defmodule SymmetryControl.GoalsSchemaTest do
  use ExUnit.Case, async: true

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.{ContextSnapshot, Goal, GoalDecision, GoalEvent, GoalRevision}
  alias SymmetryControl.Goals.{HarnessSession, RunEvidence, RunUsage, WorkOutcome}
  alias SymmetryControl.Orchestration.{Runtime, Task}
  alias SymmetryControl.Workspaces.WorkItem

  @id "00000000-0000-0000-0000-000000000001"
  @other_id "00000000-0000-0000-0000-000000000002"
  @digest :binary.copy(<<1>>, 32)

  test "legacy Task changesets remain valid without Goal membership" do
    changeset =
      Task.changeset(%Task{}, %{
        idempotency_key: "legacy-task",
        request_hash: @digest,
        goal: "Keep existing work running",
        agent_profile: "codex",
        workspace: "primary",
        required_capabilities: %{},
        state: "queued",
        current_generation: 0,
        attempt_generation: 1
      })

    assert changeset.valid?
    assert Ecto.Changeset.get_field(changeset, :purpose) == "implement"
    assert Ecto.Changeset.get_field(changeset, :goal_id) == nil
  end

  test "legacy Task changesets accept historical WorkItem membership without Goal ownership" do
    changeset =
      Task.changeset(
        %Task{},
        Map.merge(task_attrs(), %{work_item_id: @id})
      )

    assert changeset.valid?
    assert Ecto.Changeset.get_field(changeset, :work_item_id) == @id
    assert Ecto.Changeset.get_field(changeset, :goal_id) == nil
  end

  test "Goal Task membership fields are all present except planning WorkItem ownership" do
    partial = Task.changeset(%Task{}, Map.merge(task_attrs(), %{goal_id: @id}))
    assert "must be present for a Goal task" in errors_on(partial).work_item_id
    assert "must be present for a Goal task" in errors_on(partial).max_run_attempts

    goal_task = Task.changeset(%Task{}, Map.merge(task_attrs(), goal_task_attrs()))
    assert goal_task.valid?

    missing_producer =
      Task.changeset(
        %Task{},
        Map.merge(task_attrs(), Map.merge(goal_task_attrs(), %{purpose: "validate"}))
      )

    assert "must be present for a validation task" in errors_on(missing_producer).validation_of_task_id

    validation_task =
      Task.changeset(
        %Task{},
        Map.merge(
          task_attrs(),
          Map.merge(goal_task_attrs(), %{purpose: "validate", validation_of_task_id: @other_id})
        )
      )

    assert validation_task.valid?

    planning_task =
      Task.changeset(
        %Task{},
        Map.merge(
          task_attrs(),
          Map.merge(goal_task_attrs(), %{purpose: "plan", work_item_id: nil})
        )
      )

    assert planning_task.valid?

    planning_with_work_item =
      Task.changeset(
        %Task{},
        Map.merge(task_attrs(), Map.merge(goal_task_attrs(), %{purpose: "plan"}))
      )

    assert "must be absent for a planning task" in errors_on(planning_with_work_item).work_item_id

    nonplanning_without_work_item =
      Task.changeset(
        %Task{},
        Map.merge(
          task_attrs(),
          Map.merge(goal_task_attrs(), %{work_item_id: nil, purpose: "observe"})
        )
      )

    assert "must be present for a Goal task" in errors_on(nonplanning_without_work_item).work_item_id

    goal_less_planning_task =
      Task.changeset(%Task{}, Map.merge(task_attrs(), %{purpose: "plan"}))

    assert "requires Goal membership" in errors_on(goal_less_planning_task).purpose
  end

  test "ContextSnapshot changesets permit the Goal-scoped planning shape" do
    planning_snapshot =
      ContextSnapshot.changeset(%ContextSnapshot{}, %{
        goal_id: @id,
        goal_revision: 1,
        work_item_id: nil,
        schema_version: 1,
        content_hash: @digest,
        payload: %{}
      })

    assert planning_snapshot.valid?
  end

  test "runtime adapter metadata is optional for legacy runtimes and complete for native runtimes" do
    legacy = Runtime.changeset(%Runtime{}, runtime_attrs())
    assert legacy.valid?

    partial = Runtime.changeset(%Runtime{}, Map.merge(runtime_attrs(), %{harness_kind: "codex"}))
    assert "must be present with adapter metadata" in errors_on(partial).adapter_version

    native =
      Runtime.changeset(
        %Runtime{},
        Map.merge(runtime_attrs(), %{
          harness_kind: "codex",
          harness_version: "1.2.3",
          adapter_version: "0.1.0",
          adapter_protocol_version: 1
        })
      )

    assert native.valid?

    generic =
      Runtime.changeset(
        %Runtime{},
        Map.merge(runtime_attrs(), %{
          harness_kind: "generic",
          harness_version: "v1",
          adapter_version: "v1",
          adapter_protocol_version: 1
        })
      )

    assert generic.valid?
  end

  test "Goal WorkItem membership is all present or all absent and cannot be rewritten" do
    partial = WorkItem.goal_membership_changeset(%WorkItem{}, %{goal_id: @id})
    assert "must be present for a Goal WorkItem" in errors_on(partial).admitted_revision
    assert "must be present for a Goal WorkItem" in errors_on(partial).acceptance_contract

    goal_less_integration = WorkItem.goal_membership_changeset(%WorkItem{}, %{integration: true})
    assert "requires Goal ownership" in errors_on(goal_less_integration).integration

    admitted =
      WorkItem.goal_membership_changeset(%WorkItem{}, %{
        goal_id: @id,
        admitted_revision: 1,
        required: true,
        integration: true,
        acceptance_contract: %{"schema_version" => "symmetry.acceptance.v1"},
        baseline_subject: %{
          "resource_id" => @id,
          "commit" => String.duplicate("a", 40),
          "tree_digest" => "sha256:" <> String.duplicate("b", 64)
        }
      })

    assert admitted.valid?
    assert Ecto.Changeset.get_field(admitted, :integration)

    existing = %WorkItem{
      goal_id: @id,
      admitted_revision: 1,
      integration: true,
      acceptance_contract: %{"schema_version" => "symmetry.acceptance.v1"}
    }

    assert WorkItem.goal_membership_changeset(existing, %{}).valid?

    rewrite = WorkItem.goal_membership_changeset(existing, %{admitted_revision: 2})
    assert "cannot be changed after Goal admission" in errors_on(rewrite).admitted_revision

    integration_rewrite = WorkItem.goal_membership_changeset(existing, %{integration: false})
    assert "cannot be changed after Goal admission" in errors_on(integration_rewrite).integration
  end

  test "Goal-owned WorkItems cannot be manually moved to done" do
    changeset =
      WorkItem.move_changeset(%WorkItem{goal_id: @id, status: "review", position: 0}, %{
        status: "done",
        position: 0
      })

    assert "is derived from an accepted Goal outcome" in errors_on(changeset).status
  end

  test "append-only Goal records reject program-level mutation" do
    for {schema, immutable_changeset, change} <- [
          {%GoalRevision{}, &GoalRevision.immutable_changeset/2, %{objective: "changed"}},
          {%ContextSnapshot{}, &ContextSnapshot.immutable_changeset/2,
           %{payload: %{"changed" => true}}},
          {%RunEvidence{}, &RunEvidence.immutable_changeset/2, %{verdict: "failed"}},
          {%WorkOutcome{}, &WorkOutcome.immutable_changeset/2, %{reason: "changed"}},
          {%GoalEvent{}, &GoalEvent.immutable_changeset/2, %{kind: "changed"}},
          {%RunUsage{}, &RunUsage.immutable_changeset/2, %{cost_microusd: 1}}
        ] do
      changeset = immutable_changeset.(schema, change)
      assert changeset.valid? == false
    end
  end

  test "RunUsage rejects a self-superseding record before persistence" do
    changeset =
      RunUsage.changeset(%RunUsage{id: @id}, %{
        run_id: @other_id,
        usage_key: "turn-1",
        provider: "codex",
        model: "test",
        cost_basis: "reported",
        supersedes_id: @id
      })

    assert "cannot supersede itself" in errors_on(changeset).supersedes_id
  end

  test "GoalDecision rejects scalar option entries as an invalid changeset" do
    for options <- [[nil], ["invalid"], [1], [%{"id" => "accept"}, nil]] do
      changeset =
        GoalDecision.changeset(%GoalDecision{}, %{
          goal_id: @id,
          goal_revision: 1,
          kind: "scope",
          action_hash: @digest,
          state: "open",
          question: "Keep the scope bounded?",
          options: options
        })

      refute changeset.valid?
      assert "must contain id, label, and consequence records" in errors_on(changeset).options
    end
  end

  test "public Goal commands reject unknown envelope fields and internal outcome acceptance" do
    command = %{
      schema_version: "symmetry.goal_command.v1",
      mutation_id: @id,
      expected_version: 1,
      expected_revision: 1,
      kind: "pause",
      payload: %{reason: "Operator review"}
    }

    assert {:error, :invalid_request} =
             Goals.command(@other_id, Map.put(command, :unexpected, true), "operator:test")

    assert {:error, :invalid_request} =
             Goals.command(
               @other_id,
               %{command | kind: "accept_outcome", payload: %{}},
               "operator:test"
             )
  end

  test "Goal JSON arrays remain JSON documents and validate their contents" do
    revision =
      GoalRevision.changeset(%GoalRevision{}, %{
        goal_id: @id,
        revision: 1,
        objective: "Preserve verified authority",
        non_goals: ["No autonomous merge"],
        acceptance_contract: %{},
        authority_policy: %{},
        execution_policy: execution_policy(),
        context_manifest: %{},
        reason: "initial",
        actor_ref: "operator:test"
      })

    assert revision.valid?

    outcome =
      WorkOutcome.changeset(%WorkOutcome{}, %{
        goal_id: @id,
        goal_revision: 1,
        work_item_id: @other_id,
        producing_task_id: @id,
        producing_run_id: @other_id,
        candidate_subject: %{
          "resource_id" => @id,
          "commit" => String.duplicate("a", 40),
          "tree_digest" => "sha256:" <> String.duplicate("b", 64)
        },
        subject_hash: @digest,
        producing_result_id: @id,
        evidence_ids: [@id],
        disposition: "accepted",
        reason: "all required predicates passed"
      })

    assert outcome.valid?

    invalid_outcome =
      WorkOutcome.changeset(%WorkOutcome{}, %{
        goal_id: @id,
        goal_revision: 1,
        work_item_id: @other_id,
        producing_task_id: @id,
        producing_run_id: @other_id,
        candidate_subject: %{},
        subject_hash: @digest,
        producing_result_id: @id,
        evidence_ids: ["not-a-uuid"],
        disposition: "accepted",
        reason: "all required predicates passed"
      })

    refute invalid_outcome.valid?
  end

  test "session ownership and decision resolution retain their state invariants" do
    unavailable =
      HarnessSession.update_changeset(%HarnessSession{state: "available"}, %{state: "busy"})

    assert "must be present exactly when session is busy" in errors_on(unavailable).active_run_id

    busy =
      HarnessSession.update_changeset(
        %HarnessSession{state: "available"},
        %{state: "busy", active_run_id: @id}
      )

    assert busy.valid?

    decision = %GoalDecision{state: "resolved", resolution: %{"option_id" => "approve"}}

    rewrite =
      GoalDecision.resolve_changeset(decision, %{
        state: "resolved",
        resolution: %{"option_id" => "reject"}
      })

    assert "cannot be changed once resolved" in errors_on(rewrite).resolution
  end

  test "terminal Goals reject authority changes and future wakes while allowing event accounting" do
    terminal = %Goal{
      project_id: @id,
      title: "Keep accepted work immutable",
      state: "achieved",
      current_revision: 1,
      event_sequence: 4
    }

    resurrection = Goal.transition_changeset(terminal, %{state: "active"})
    assert "cannot be changed after Goal terminalization" in errors_on(resurrection).state

    revision_rewrite = Goal.changeset(terminal, %{current_revision: 2})

    assert "cannot be changed after Goal terminalization" in errors_on(revision_rewrite).current_revision

    project_rewrite = Goal.changeset(terminal, %{project_id: @other_id})
    assert "cannot be changed after Goal terminalization" in errors_on(project_rewrite).project_id

    accounting = Goal.changeset(terminal, %{event_sequence: 5})
    assert accounting.valid?

    terminal_wake =
      Goal.transition_changeset(%Goal{state: "active"}, %{
        state: "cancelled",
        next_wake_at: DateTime.utc_now()
      })

    assert "must be absent for a terminal Goal" in errors_on(terminal_wake).next_wake_at
  end

  test "terminal decisions reject further authority transitions" do
    resolved = %GoalDecision{state: "resolved", resolution: %{"option_id" => "approve"}}
    superseded = %GoalDecision{state: "superseded"}

    terminal_create =
      GoalDecision.changeset(%GoalDecision{}, %{
        goal_id: @id,
        goal_revision: 1,
        kind: "completion",
        action_hash: @digest,
        state: "resolved",
        question: "Accept the Goal outcome?",
        options: [%{"id" => "accept", "label" => "Accept", "consequence" => "Mark achieved"}],
        resolution: %{"option_id" => "accept"}
      })

    assert "new decisions must start open" in errors_on(terminal_create).state

    resolved_rewrite = GoalDecision.changeset(resolved, %{question: "Repurpose the decision?"})

    assert "cannot be changed after decision terminalization" in errors_on(resolved_rewrite).question

    resolved_supersede = GoalDecision.supersede_changeset(resolved)
    assert "only open decisions can be marked superseded" in errors_on(resolved_supersede).state

    superseded_resolve =
      GoalDecision.resolve_changeset(superseded, %{
        state: "resolved",
        resolution: %{"option_id" => "approve"}
      })

    assert "only open decisions can be marked resolved" in errors_on(superseded_resolve).state
  end

  test "GoalRevision enforces the exact execution policy and cost rules" do
    assert revision_changeset(execution_policy()).valid?

    missing_key = Map.delete(execution_policy(), "hard_cost_limit_required")

    assert "must contain exactly the v1 execution policy keys" in errors_on(
             revision_changeset(missing_key)
           ).execution_policy

    extra_key = Map.put(execution_policy(), "unbounded_execution", true)

    assert "must contain exactly the v1 execution policy keys" in errors_on(
             revision_changeset(extra_key)
           ).execution_policy

    automatic_without_budget = Map.put(execution_policy(), "automatic_execution", true)

    assert "automatic execution requires a total budget" in errors_on(
             revision_changeset(automatic_without_budget)
           ).execution_policy

    manual_strict =
      execution_policy()
      |> Map.put("budget_mode", "strict")
      |> Map.put("per_run_cost_limit_microusd", 0)
      |> Map.put("hard_cost_limit_required", true)

    assert revision_changeset(manual_strict).valid?

    strict_without_ceiling = Map.put(manual_strict, "per_run_cost_limit_microusd", nil)

    assert "strict execution requires a per-run ceiling and hard limit" in errors_on(
             revision_changeset(strict_without_ceiling)
           ).execution_policy

    strict_without_hard_limit = Map.put(manual_strict, "hard_cost_limit_required", false)

    assert "strict execution requires a per-run ceiling and hard limit" in errors_on(
             revision_changeset(strict_without_hard_limit)
           ).execution_policy

    negative_cost_limit = Map.put(execution_policy(), "per_run_cost_limit_microusd", -1)

    assert "must use nullable non-negative signed 64-bit cost limits" in errors_on(
             revision_changeset(negative_cost_limit)
           ).execution_policy
  end

  defp task_attrs do
    %{
      idempotency_key: "task-#{System.unique_integer([:positive])}",
      request_hash: @digest,
      goal: "Run the required checks",
      agent_profile: "codex",
      workspace: "primary",
      required_capabilities: %{},
      state: "queued",
      current_generation: 0,
      attempt_generation: 1
    }
  end

  defp goal_task_attrs do
    %{
      work_item_id: @id,
      goal_id: @id,
      goal_revision: 1,
      context_snapshot_id: @other_id,
      admission_key: "00000000-0000-0000-0000-000000000003",
      max_run_attempts: 2
    }
  end

  defp revision_changeset(execution_policy) do
    GoalRevision.changeset(%GoalRevision{}, %{
      goal_id: @id,
      revision: 1,
      objective: "Preserve verified authority",
      non_goals: ["No autonomous merge"],
      acceptance_contract: %{},
      authority_policy: %{},
      execution_policy: execution_policy,
      context_manifest: %{},
      reason: "initial",
      actor_ref: "operator:test"
    })
  end

  defp execution_policy do
    %{
      "automatic_execution" => false,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "max_run_attempts_per_task" => 1,
      "budget_limit_microusd" => nil,
      "per_run_cost_limit_microusd" => nil,
      "budget_mode" => "soft",
      "hard_cost_limit_required" => false,
      "allowed_runtime_ids" => [],
      "allowed_model_profiles" => [],
      "final_acceptance" => "operator",
      "allowed_actions" => [],
      "allowed_resource_ids" => []
    }
  end

  defp runtime_attrs do
    %{
      machine_id: @id,
      runtime_key: "default",
      name: "Local runtime",
      daemon_instance_id: @other_id,
      connection_epoch: 1,
      capacity: 1,
      agent_profile: "codex",
      workspace: "primary",
      capabilities: %{},
      status: "online",
      heartbeat_interval_ms: 5_000
    }
  end

  defp errors_on(changeset) do
    Ecto.Changeset.traverse_errors(changeset, fn {message, _opts} -> message end)
  end
end
