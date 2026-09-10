defmodule SymmetryControl.GoalsExternalWaitSchemaTest do
  use ExUnit.Case, async: true

  alias Ecto.Changeset
  alias SymmetryControl.Goals.GoalExternalWait

  @goal_id Ecto.UUID.generate()
  @work_item_id Ecto.UUID.generate()
  @task_id Ecto.UUID.generate()
  @run_id Ecto.UUID.generate()
  @receipt_event_id Ecto.UUID.generate()
  @resource_id Ecto.UUID.generate()
  @result_id Ecto.UUID.generate()
  @source_resource_id Ecto.UUID.generate()
  @subject_hash :crypto.hash(:sha256, "external-wait-subject")
  @subject %{
    "resource_id" => @source_resource_id,
    "commit" => String.duplicate("a", 40),
    "tree_digest" => "sha256:" <> String.duplicate("b", 64)
  }

  test "changeset captures an immutable unsupported source/result and starts at check sequence zero" do
    changeset = GoalExternalWait.changeset(%GoalExternalWait{}, valid_attrs())

    assert changeset.valid?
    assert Changeset.get_field(changeset, :state) == "unsupported"
    assert Changeset.get_field(changeset, :check_seq) == 0
    assert Changeset.get_field(changeset, :subject) == @subject
    assert Changeset.get_field(changeset, :receipt_event_id) == @receipt_event_id
  end

  test "the schema defaults a new wait to the fail-closed terminal state" do
    attrs = Map.delete(valid_attrs(), :state)
    changeset = GoalExternalWait.changeset(%GoalExternalWait{}, attrs)

    assert changeset.valid?
    assert Changeset.get_field(changeset, :state) == "unsupported"
  end

  test "changeset rejects malformed subject, result and unsupported schedules" do
    attrs = valid_attrs()

    refute GoalExternalWait.changeset(%GoalExternalWait{}, %{attrs | subject: %{}}).valid?

    refute GoalExternalWait.changeset(%GoalExternalWait{}, %{attrs | result: []}).valid?

    changeset =
      GoalExternalWait.changeset(
        %GoalExternalWait{},
        %{attrs | next_check_at: ~U[2026-09-09 01:10:00Z]}
      )

    assert "must be absent after the wait is terminal" in errors_on(changeset).next_check_at

    active_changeset =
      GoalExternalWait.changeset(
        %GoalExternalWait{},
        %{attrs | state: "waiting", next_check_at: ~U[2026-09-09 01:10:00Z]}
      )

    refute active_changeset.valid?
    assert "is invalid" in errors_on(active_changeset).state
  end

  test "terminal external waits reject reconciliation mutation" do
    wait = %GoalExternalWait{
      id: Ecto.UUID.generate(),
      state: "unsupported",
      check_seq: 4,
      next_check_at: nil
    }

    changeset =
      GoalExternalWait.reconcile_changeset(wait, %{
        state: "unsupported",
        next_check_at: nil,
        receipt_event_id: Ecto.UUID.generate()
      })

    refute changeset.valid?
    assert "terminal external wait is immutable" in errors_on(changeset).receipt_event_id
  end

  test "terminal waits cannot be reopened" do
    wait = %GoalExternalWait{
      id: Ecto.UUID.generate(),
      state: "satisfied",
      check_seq: 2,
      next_check_at: nil
    }

    changeset =
      GoalExternalWait.reconcile_changeset(wait, %{
        state: "waiting",
        next_check_at: ~U[2026-09-09 01:10:00Z],
        receipt_event_id: Ecto.UUID.generate()
      })

    refute changeset.valid?
    assert "cannot transition a terminal external wait" in errors_on(changeset).state
  end

  test "unsupported waits are terminal and cannot carry a schedule" do
    wait = %GoalExternalWait{
      id: Ecto.UUID.generate(),
      state: "unsupported",
      check_seq: 0,
      next_check_at: nil
    }

    changeset =
      GoalExternalWait.reconcile_changeset(wait, %{
        state: "unsupported",
        next_check_at: nil,
        receipt_event_id: Ecto.UUID.generate()
      })

    refute changeset.valid?
    assert "terminal external wait is immutable" in errors_on(changeset).receipt_event_id

    changeset =
      GoalExternalWait.reconcile_changeset(wait, %{
        state: "waiting",
        next_check_at: ~U[2026-09-09 01:10:00Z],
        receipt_event_id: Ecto.UUID.generate()
      })

    refute changeset.valid?
    assert "cannot transition a terminal external wait" in errors_on(changeset).state
  end

  test "immutable changeset rejects changes to source and target snapshots" do
    wait = struct(GoalExternalWait, valid_attrs())

    changeset =
      GoalExternalWait.immutable_changeset(wait, %{
        result: Map.put(wait.result, "summary", "rewritten"),
        external_ref: "rewritten/ref",
        subject_hash: :crypto.hash(:sha256, "different")
      })

    refute changeset.valid?
    assert "external wait history is immutable" in errors_on(changeset).result
    assert "external wait history is immutable" in errors_on(changeset).external_ref
    assert "external wait history is immutable" in errors_on(changeset).subject_hash
  end

  test "state helpers expose scan and terminal partitions" do
    refute "waiting" in GoalExternalWait.states()
    assert "unsupported" in GoalExternalWait.states()
    assert GoalExternalWait.active_states() == []
    assert "unsupported" in GoalExternalWait.terminal_states()
  end

  defp valid_attrs do
    result = %{
      "schema_version" => "symmetry.task_result.v1",
      "result_id" => @result_id,
      "kind" => "blocked",
      "summary" => "Waiting for external state",
      "subject" => @subject,
      "subject_hash" => "sha256:" <> Base.encode16(@subject_hash, case: :lower),
      "evidence_refs" => [],
      "blocker" => %{
        "kind" => "external",
        "resource_id" => @resource_id,
        "external_ref" => "acme/change/42",
        "next_check_at" => "2026-09-09T01:00:00Z"
      },
      "proposed_next_action" => nil,
      "reason" => nil,
      "diagnostics" => []
    }

    %{
      goal_id: @goal_id,
      goal_revision: 1,
      work_item_id: @work_item_id,
      task_id: @task_id,
      run_id: @run_id,
      run_generation: 1,
      source_ref: %{"kind" => "task_result", "ref" => @result_id},
      result_id: @result_id,
      result: result,
      resource_id: @resource_id,
      external_ref: "acme/change/42",
      subject: @subject,
      subject_hash: @subject_hash,
      next_check_at: nil,
      state: "unsupported",
      receipt_event_id: @receipt_event_id
    }
  end

  defp errors_on(changeset) do
    Changeset.traverse_errors(changeset, fn {message, _opts} -> message end)
  end
end
