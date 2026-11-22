Code.require_file("goals/goals_test_support.exs", __DIR__)

defmodule SymmetryControl.GoalsTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Goals.ReadModel
  alias SymmetryControl.Goals.Workers.SettleTaskWorker
  alias SymmetryControl.Goals.Workers.WakeupWorker
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.TelemetryReads
  alias SymmetryControl.Workspaces.WorkItem

  @now ~U[2026-09-09 00:00:00.000000Z]
  @validation_runtime_id "00000000-0000-4000-8000-000000000001"
  @validation_profile_digest "sha256:" <> String.duplicate("c", 64)

  use SymmetryControl.TestFixtures,
    command_opts: [now: @now, validation_profiles: validation_profiles()]

  setup :put_goal_validation_profiles

  test "Goal plan and admitted Task preserve the project workspace and agent assignment" do
    {_goal, item, task} = admitted_task_fixture("goal-workspace")

    assert item.assignee_type == "agent"
    assert item.assignee_name == "codex"
    assert item.agent_profile == "codex"
    assert item.workspace == "goal-workspace"
    assert task.workspace == "goal-workspace"
  end

  test "plan acceptance rejects change targets without a reachable provider connection" do
    unconnected_project = project_fixture()
    unconnected_repository = repository_fixture(unconnected_project)

    assert_branch_target_rejected!(unconnected_project, unconnected_repository)

    limited_project = project_fixture()

    limited_repository =
      connected_github_repository_fixture(limited_project, "acme/symmetry", ["repositories"])

    assert_branch_target_rejected!(limited_project, limited_repository)
  end

  test "a completed Run is not accepted work and a producer cannot validate itself" do
    {goal, item, task} = admitted_task_fixture()
    now = @now

    Repo.update_all(from(task in Task, where: task.id == ^task.id),
      set: [state: "completed", updated_at: now]
    )

    runtime = runtime_fixture()

    run =
      %Run{}
      |> Run.changeset(%{
        task_id: task.id,
        runtime_id: runtime.id,
        generation: 1,
        state: "completed",
        assigned_at: now,
        assignment_expires_at: DateTime.add(now, 60, :second)
      })
      |> Repo.insert!()

    assert {:ok, projection} = Goals.fetch_goal(goal.id)
    refute Enum.any?(projection.accepted_outcomes, &(&1.work_item_id == item.id))

    assert {:error, :outcome_derived} =
             Goals.accept_outcome(goal.id, %{
               work_item_id: item.id,
               producing_task_id: task.id,
               producing_run_id: run.id,
               subject_hash: Base.encode16(:crypto.hash(:sha256, "candidate"), case: :lower),
               evidence_ids: [],
               reason: "A producer result alone is not acceptance"
             })

    assert {:error, {:invalid_contract, _}} = command_current(goal.id, "achieve", %{})
  end

  test "terminal producer, validation evidence, accepted outcome, and achieve close the Goal loop" do
    setup = goal_loop_validation_setup()

    accepted_outcome = goal_loop_accept_validation(setup)
    decision_id = goal_loop_resolve_achieve_decisions(setup)

    goal_loop_achieve_and_replay(setup, accepted_outcome, decision_id)
  end

  test "attention attributes a database cast failure to the cursor only" do
    # An uncast project_id is a caller fault, not a forged cursor: without a
    # cursor the cast error surfaces instead of being answered as invalid_cursor.
    assert_raise Ecto.Query.CastError, fn ->
      Goals.attention(project_id: "not-a-uuid")
    end

    forged_cursor =
      %{"updated_at" => "2026-11-16T10:00:00Z", "id" => "not-a-uuid"}
      |> Jason.encode!()
      |> Base.url_encode64(padding: false)

    assert {:error, :invalid_cursor} = Goals.attention(cursor: forged_cursor)
  end

  test "a rejected completion Subject refuses achieve for the current revision" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} =
      independent_validation_attempt_fixture()

    {evidence, stored_evidence} =
      append_validation_evidence_and_settle_accepted!(
        validation,
        validation_run,
        validation_runtime,
        validation_fence,
        item,
        candidate_subject
      )

    assert {:ok, completion_request, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "completion",
               subject_hash: evidence.subject_hash
             })

    rejected_decision_id = completion_request.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(goal.id, "resolve_decision", %{
               decision_id: rejected_decision_id,
               expected_decision_version:
                 Repo.get!(SymmetryControl.Goals.GoalDecision, rejected_decision_id).lock_version,
               option_id: "reject"
             })

    projection = fetch_goal!(goal.id)

    refute projection.completion.ready?
    refute "achieve" in projection.allowed_actions

    assert projection.blocker_reasons == []

    assert {:ok, %{entries: entries}} = ReadModel.attention(limit: 5)
    refute Enum.any?(entries, &(&1.goal_id == goal.id))

    achieve_payload = %{
      subject: evidence.subject,
      integration_work_item_id: item.id,
      evidence_ids: [stored_evidence.id]
    }

    assert {:error, :invalid_decision} =
             command_current(
               goal.id,
               "achieve",
               Map.put(achieve_payload, :decision_id, rejected_decision_id)
             )

    assert {:error, :invalid_decision} =
             command_current(goal.id, "achieve", Map.put(achieve_payload, :decision_id, nil))
  end

  test "a conflicting stored optional integration marker refuses achieve for a single-required plan" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} =
      independent_validation_attempt_fixture(check_contract(), %{plan_integration: false})

    {evidence, stored_evidence} =
      append_validation_evidence_and_settle_accepted!(
        validation,
        validation_run,
        validation_runtime,
        validation_fence,
        item,
        candidate_subject
      )

    conflicting =
      optional_goal_work_item!(goal, item, "Conflicting integration candidate", true)

    assert {:ok, conflicting_admission, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(conflicting, %{reserved_microusd: 42}),
               rollout_enabled: true
             )

    conflicting_producer = Repo.get!(Task, conflicting_admission.response["task"]["id"])

    {conflicting_run, _conflicting_fence} =
      completed_goal_run_fixture(conflicting_producer, runtime_fixture(), candidate_subject)

    # A storage-marker implementation would accept this conflicting item as the
    # integration role and find this accepted outcome, turning the achieve below
    # into a success instead of the deterministic contract refusal.
    insert_accepted_work_outcome!(goal, conflicting.id, conflicting_run, candidate_subject, [
      stored_evidence.id
    ])

    projection = fetch_goal!(goal.id)

    refute projection.completion.ready?
    refute "achieve" in projection.allowed_actions

    assert {:error, :invalid_plan} =
             command_current(goal.id, "achieve", %{
               subject: evidence.subject,
               evidence_ids: [stored_evidence.id],
               decision_id: nil
             })
  end

  test "a multi-required plan keeps its explicit integration marker and payload obligations" do
    {goal, projection, integration_item} = multi_required_goal_fixture!()

    other_item = Enum.find(projection.work_items, &(&1.id != integration_item.id))
    refute other_item.integration

    subject = candidate_subject(item_repository_resource_id(integration_item))

    assert {:error, :invalid_request} =
             command_current(goal.id, "achieve", %{
               subject: subject,
               evidence_ids: [],
               decision_id: nil
             })

    assert {:error, :integration_outcome_required} =
             command_current(goal.id, "achieve", %{
               subject: subject,
               integration_work_item_id: other_item.id,
               evidence_ids: [],
               decision_id: nil
             })
  end

  test "a multi-required plan refuses an omitted integration payload as an invalid request" do
    {goal, projection, integration_item} = multi_required_goal_fixture!()

    assert projection.state == "active"

    subject = candidate_subject(item_repository_resource_id(integration_item))

    assert {:error, :invalid_request} =
             command_current(goal.id, "achieve", %{
               subject: subject,
               evidence_ids: [],
               decision_id: nil
             })

    assert fetch_goal!(goal.id).state == "active"
  end

  test "accept_outcome is retired and cannot bypass terminal settlement" do
    {goal, item, _task} = admitted_task_fixture()

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract: amended_revision_contract("Revised acceptance subject."),
               reason: "Scope changed"
             })

    assert amended.goal.current_revision == goal.current_revision + 1

    assert {:error, :outcome_derived} =
             Goals.accept_outcome(goal.id, %{
               work_item_id: item.id,
               producing_task_id: Ecto.UUID.generate(),
               producing_run_id: Ecto.UUID.generate(),
               validation_task_id: Ecto.UUID.generate(),
               subject_hash: String.duplicate("f", 64),
               evidence_ids: [],
               reason: "A superseded revision cannot be accepted."
             })
  end

  test "the global wakeup recovers a terminal settlement for a paused Goal exactly once" do
    {goal, _item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)
    previous_goals = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(previous_goals, :rollout_enabled, true)
    )

    on_exit(fn -> Application.put_env(:symmetry_control, :goals, previous_goals) end)

    assert {:ok, _paused, :created} =
             command_current(goal.id, "pause", %{reason: "Pause before recovery."})

    assert :ok = WakeupWorker.perform(%Oban.Job{args: %{}})

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "unknown"

    assert Repo.aggregate(
             from(event in SymmetryControl.Goals.GoalEvent,
               where:
                 event.goal_id == ^goal.id and event.kind == "task_settled" and
                   event.payload["run_id"] == ^run.id
             ),
             :count
           ) == 1

    assert :ok = WakeupWorker.perform(%Oban.Job{args: %{}})

    assert Repo.aggregate(
             from(event in SymmetryControl.Goals.GoalEvent,
               where:
                 event.goal_id == ^goal.id and event.kind == "task_settled" and
                   event.payload["run_id"] == ^run.id
             ),
             :count
           ) == 1
  end

  test "disabled rollout settles existing terminal work but rejects a new admission" do
    {goal, item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)
    previous_goals = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(previous_goals, :rollout_enabled, false)
    )

    on_exit(fn -> Application.put_env(:symmetry_control, :goals, previous_goals) end)

    assert :ok =
             SettleTaskWorker.perform(%Oban.Job{
               args: %{"task_id" => task.id, "run_id" => run.id, "generation" => run.generation}
             })

    assert Repo.exists?(
             from(event in SymmetryControl.Goals.GoalEvent,
               where:
                 event.goal_id == ^goal.id and event.kind == "task_settled" and
                   fragment("? ->> 'task_id' = ?", event.payload, ^task.id)
             )
           )

    assert {:error, :goal_admission_disabled} =
             command_current(goal.id, "admit_task", admission_payload(item))
  end

  test "a Goal projection read keeps every query on the calling process" do
    {goal, _item, _task} = admitted_task_fixture()

    reader = self()
    goal_forms = [goal.id, Ecto.UUID.dump!(goal.id)]
    handler_id = {__MODULE__, :goal_read_processes, goal.id}

    :telemetry.attach(
      handler_id,
      [:symmetry_control, :repo, :query],
      fn _event, _measurements, metadata, %{reader: reader, forms: forms} ->
        if TelemetryReads.read?(metadata.params, forms),
          do: send(reader, {:goal_read_query, self(), to_string(metadata.query)})
      end,
      %{reader: reader, forms: goal_forms}
    )

    on_exit(fn -> :telemetry.detach(handler_id) end)

    assert {:ok, _projection} = Goals.fetch_goal(goal.id)

    reads = TelemetryReads.drain(:goal_read_query)
    queries = Enum.map(reads, fn {_pid, query} -> query end)
    processes = reads |> Enum.map(&elem(&1, 0)) |> Enum.uniq() |> Enum.sort()

    # Two preloads must issue fetch queries for this test to have power: Ecto
    # only preloads in parallel from two preloads with fetch work (an unloaded
    # has_many issues its query even with zero rows), and every parallel preload
    # would queue for the sandbox connection the test process owns (the recorded
    # intermittent checkout-timeout failure).
    assert Enum.any?(queries, &String.contains?(&1, "\"goal_revisions\""))
    assert Enum.any?(queries, &String.contains?(&1, "\"work_items\""))

    assert processes == [reader],
           "expected every Goal projection read query on #{inspect(reader)}, got #{inspect(processes)}"
  end

  test "automatic read model reports a missing explicit baseline blocker" do
    projection =
      ReadModel.project(
        %{id: "goal-baseline", state: "active", current_revision: 1},
        %{
          revisions: [
            %{
              revision: 1,
              execution_policy: %{"automatic_execution" => true}
            }
          ],
          work_items: [
            %{
              id: "unstarted-item",
              required: true,
              admitted_revision: 1,
              repository_resource_id: "repository-1"
            }
          ],
          dependencies: [],
          outcomes: [],
          decisions: [],
          tasks: [],
          runs: [],
          runtimes: []
        }
      )

    assert [%{reason: "automatic_baseline_missing"}] =
             Enum.filter(projection.blockers, &(&1.reason == "automatic_baseline_missing"))
  end

  test "advisory recall copies only bounded immutable same-resource snapshot pointers" do
    {goal, item, task} =
      admitted_task_fixture("primary", check_contract(), %{
        context_manifest: %{
          "byte_budget" => 32_768,
          "required_source_kinds" => ["repository"],
          "include_advisory_recall" => true
        }
      })

    previous_snapshot = Repo.get!(SymmetryControl.Goals.ContextSnapshot, task.context_snapshot_id)
    complete_task_and_release_reservation!(goal, task)

    assert {:ok, admission, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item),
               rollout_enabled: true
             )

    snapshot =
      Repo.get!(
        SymmetryControl.Goals.ContextSnapshot,
        Repo.get!(Task, admission.response["task"]["id"]).context_snapshot_id
      )

    assert [recall | _] = snapshot.payload["advisory_recall"]

    assert recall["content"] == %{
             "kind" => "pointer",
             "value" => "context_snapshot:" <> previous_snapshot.id
           }

    assert recall["source_revision"] == "snapshot:" <> previous_snapshot.id

    assert recall["content_hash"] ==
             "sha256:" <> Base.encode16(previous_snapshot.content_hash, case: :lower)

    assert recall["trust"] == "advisory"
    assert recall["required"] == false
    assert recall["stale"] == false
    assert snapshot.payload["size"]["optional_bytes"] > 0
    assert snapshot.payload["size"]["total_bytes"] <= snapshot.payload["size"]["byte_budget"]
  end

  test "resume admission without its requested session never creates a fresh session" do
    {goal, item, nil} = admitted_task_fixture("primary", check_contract(), %{}, false)

    assert {:error, :requested_session_not_found} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 session_mode: "resume",
                 requested_session_id: Ecto.UUID.generate()
               }),
               rollout_enabled: true
             )

    assert Repo.aggregate(from(session in HarnessSession), :count) == 0
  end

  test "resume requires a retained session" do
    {goal, item, task} = admitted_task_fixture()
    complete_task_and_release_reservation!(goal, task)

    assert {:error, {:invalid_contract, _}} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{session_mode: "resume", reserved_microusd: 1}),
               rollout_enabled: true
             )
  end

  test "handoff rejects a source Run without a current canonical settlement before writing admission records" do
    {goal, item, source_task} = admitted_task_fixture()
    source_runtime = runtime_fixture()
    {source_run, _source_fence} = completed_goal_run_fixture(source_task, source_runtime)
    counts_before = handoff_admission_counts(goal.id)

    assert {:error, :handoff_source_ineligible} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 session_mode: "handoff",
                 handoff_source_run_id: source_run.id
               }),
               rollout_enabled: true
             )

    assert handoff_admission_counts(goal.id) == counts_before
  end

  test "handoff cannot bypass the unsupported external observation contract" do
    {goal, item, _task} = admitted_task_fixture()
    counts_before = handoff_admission_counts(goal.id)

    assert {:error, {:invalid_contract, _}} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 purpose: "observe",
                 session_mode: "handoff",
                 handoff_source_run_id: Ecto.UUID.generate()
               }),
               rollout_enabled: true
             )

    assert handoff_admission_counts(goal.id) == counts_before
  end

  test "resume admission rejects an unavailable retained session before writing admission records" do
    {goal, item, task} = admitted_task_fixture()
    complete_task_and_release_reservation!(goal, task)
    runtime = runtime_fixture()
    retained = available_session_fixture(runtime, item, session_attrs(item, "workspace-retained"))

    counts_before = %{
      snapshots: Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count),
      tasks: Repo.aggregate(Task, :count),
      reservations: Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count),
      events: Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count)
    }

    assert {:error, :requested_session_unavailable} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 session_mode: "resume",
                 requested_session_id: retained.id,
                 admission_key: Ecto.UUID.generate(),
                 reserved_microusd: 1
               }),
               rollout_enabled: true
             )

    assert counts_before ==
             %{
               snapshots: Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count),
               tasks: Repo.aggregate(Task, :count),
               reservations: Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count),
               events: Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count)
             }
  end

  test "resume admission rejects an unverified retained session before writing admission records" do
    {goal, item, task} = admitted_task_fixture()
    complete_task_and_release_reservation!(goal, task)
    runtime = runtime_fixture()

    Repo.update_all(
      from(row in Runtime, where: row.id == ^runtime.id),
      set: [capabilities: %{"adapter" => %{"operations" => %{"resume" => true}}}]
    )

    retained =
      available_session_fixture(
        runtime,
        item,
        session_attrs(item, "workspace-unverified"),
        binding_verified: false
      )

    counts_before = %{
      snapshots: Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count),
      tasks: Repo.aggregate(Task, :count),
      reservations: Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count),
      events: Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count)
    }

    assert {:error, :requested_session_unavailable} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 session_mode: "resume",
                 requested_session_id: retained.id,
                 admission_key: Ecto.UUID.generate(),
                 reserved_microusd: 1
               }),
               rollout_enabled: true
             )

    assert counts_before ==
             %{
               snapshots: Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count),
               tasks: Repo.aggregate(Task, :count),
               reservations: Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count),
               events: Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count)
             }

    assert %{binding_verified: false, state: "available", active_run_id: nil} =
             Repo.get!(HarnessSession, retained.id)
  end

  test "admission rejects a validation profile with no runtime allowed by the revision policy" do
    policy_runtime_id = Ecto.UUID.generate()
    validation_runtime_id = Ecto.UUID.generate()

    {goal, item, producer} =
      admitted_task_fixture(
        "primary",
        check_contract(),
        %{execution_policy: %{"allowed_runtime_ids" => [policy_runtime_id]}},
        true,
        validation_profiles([policy_runtime_id], [policy_runtime_id])
      )

    complete_task_and_release_reservation!(goal, producer)
    producer_runtime = runtime_fixture()
    {_producer_run, _producer_fence} = completed_goal_run_fixture(producer, producer_runtime)

    snapshot_count = Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count)

    assert {:error, :validation_runtime_not_allowed} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 purpose: "validate",
                 validation_of_task_id: producer.id,
                 admission_key: Ecto.UUID.generate(),
                 reserved_microusd: 0
               }),
               rollout_enabled: true,
               validation_profiles:
                 validation_profiles([validation_runtime_id], [validation_runtime_id])
             )

    assert Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count) == snapshot_count
  end

  test "validation admission freezes the common runtime intersection for every required profile" do
    {goal, item, producer} = admitted_task_fixture("primary", check_and_review_contract())
    producer_runtime = runtime_fixture()
    {_producer_run, _producer_fence} = completed_goal_run_fixture(producer, producer_runtime)

    common_runtime_id = @validation_runtime_id
    check_only_runtime_id = Ecto.UUID.generate()
    review_only_runtime_id = Ecto.UUID.generate()

    assert {:ok, admission, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 purpose: "validate",
                 validation_of_task_id: producer.id,
                 reserved_microusd: 0
               }),
               rollout_enabled: true,
               validation_profiles:
                 validation_profiles(
                   [common_runtime_id, check_only_runtime_id],
                   [common_runtime_id, review_only_runtime_id]
                 )
             )

    validation_task = Repo.get!(Task, admission.response["task"]["id"])

    snapshot =
      Repo.get!(SymmetryControl.Goals.ContextSnapshot, validation_task.context_snapshot_id)

    assert Enum.map(
             snapshot.payload["work_contract"]["validation_bindings"],
             & &1["allowed_runtime_ids"]
           ) == [
             [common_runtime_id],
             [common_runtime_id]
           ]

    {empty_goal, empty_item, empty_producer} =
      admitted_task_fixture("primary", check_and_review_contract())

    empty_runtime = runtime_fixture()
    {_empty_run, _empty_fence} = completed_goal_run_fixture(empty_producer, empty_runtime)
    snapshot_count = Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count)

    assert {:error, :validation_runtime_not_allowed} =
             command_current(
               empty_goal.id,
               "admit_task",
               admission_payload(empty_item, %{
                 purpose: "validate",
                 validation_of_task_id: empty_producer.id,
                 reserved_microusd: 0
               }),
               rollout_enabled: true,
               validation_profiles:
                 validation_profiles([Ecto.UUID.generate()], [Ecto.UUID.generate()])
             )

    assert Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count) == snapshot_count
  end

  test "an expired earlier Run without usage blocks strict resume while its Task retries" do
    {goal, _item, task, _runtime, run, _fence} =
      claimed_goal_run_fixture(check_contract(), nil, %{
        execution_policy: %{
          "budget_mode" => "strict",
          "per_run_cost_limit_microusd" => 1,
          "hard_cost_limit_required" => true
        }
      })

    Repo.update_all(from(row in Task, where: row.id == ^task.id),
      set: [state: "queued", attempt_generation: 2, updated_at: @now]
    )

    Repo.update_all(from(row in Run, where: row.id == ^run.id),
      set: [state: "expired", updated_at: @now]
    )

    assert {:ok, _paused, :created} =
             command_current(goal.id, "pause", %{reason: "Review an expired retry."})

    assert {:error, :budget_unknown} =
             command_current(goal.id, "resume", %{reason: "Account for the expired execution."})
  end

  test "context manifest rejects missing sources and mandatory budget overflow before insertion" do
    {missing_goal, missing_item, _missing_task} =
      admitted_task_fixture(
        "primary",
        check_contract(),
        %{
          context_manifest: %{
            "byte_budget" => 32_768,
            "required_source_kinds" => ["unavailable"],
            "include_advisory_recall" => false
          }
        },
        false
      )

    snapshot_count = Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count)

    assert {:error, :required_context_source_missing} =
             command_current(
               missing_goal.id,
               "admit_task",
               admission_payload(missing_item, %{reserved_microusd: 1}),
               rollout_enabled: true
             )

    assert Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count) == snapshot_count

    {budget_goal, budget_item, _budget_task} =
      admitted_task_fixture(
        "primary",
        check_contract(),
        %{
          context_manifest: %{
            "byte_budget" => 1,
            "required_source_kinds" => ["repository"],
            "include_advisory_recall" => false
          }
        },
        false
      )

    snapshot_count = Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count)

    assert {:error, :context_budget_exceeded} =
             command_current(
               budget_goal.id,
               "admit_task",
               admission_payload(budget_item, %{reserved_microusd: 1}),
               rollout_enabled: true
             )

    assert Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count) == snapshot_count
  end

  test "admission rejects a caller-defined Subject before a Task row is written" do
    {goal, item, task} = admitted_task_fixture()
    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "completed"])

    assert {:error, {:invalid_contract, _}} =
             command_current(
               goal.id,
               "admit_task",
               Map.put(admission_payload(item), :subject, %{
                 resource_id: item_repository_resource_id(item)
               }),
               rollout_enabled: true
             )
  end

  test "a resumed Goal makes an old pause control action superseded" do
    {goal, _item, _task} = admitted_task_fixture()

    assert {:ok, paused, :created} =
             command_current(goal.id, "pause", %{reason: "operator review"})

    action_id = paused.response["control_action_id"]

    assert Repo.exists?(
             from(job in Oban.Job,
               where: job.worker == "SymmetryControl.Goals.Workers.GoalControlWorker",
               where: fragment("? ->> 'action_id' = ?", job.args, ^action_id)
             )
           )

    assert {:ok, []} =
             Goals.control_dispatch_plan(goal.id, paused.goal.current_revision, action_id)

    assert {:ok, _resumed, :created} = command_current(goal.id, "resume", %{reason: "approved"})

    assert {:ok, :superseded} =
             Goals.control_dispatch_plan(goal.id, paused.goal.current_revision, action_id)
  end

  test "cancel dispatch excludes a Task already cancelling" do
    {goal, _item, task} = admitted_task_fixture()
    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "cancelling"])

    assert {:ok, cancelled, :created} = command_current(goal.id, "cancel", %{reason: "stop"})

    assert {:ok, []} =
             Goals.control_dispatch_plan(
               goal.id,
               cancelled.goal.current_revision,
               cancelled.response["control_action_id"]
             )
  end

  test "amend cancels runs when native safe pause is unavailable" do
    {safe_goal, _safe_item, safe_task, safe_runtime, _safe_run, _safe_fence} =
      claimed_goal_run_fixture()

    Repo.update_all(
      from(row in Runtime, where: row.id == ^safe_runtime.id),
      set: [capabilities: %{"adapter" => %{"operations" => %{"pause" => "safe_boundary"}}}]
    )

    assert {:ok, safe_amendment, :created} =
             command_current(safe_goal.id, "amend", %{
               revision_contract: amended_revision_contract("Pause supported work."),
               reason: "Scope was clarified"
             })

    assert {:ok, descriptors} =
             Goals.control_dispatch_plan(
               safe_goal.id,
               safe_amendment.goal.current_revision,
               safe_amendment.response["control_action_id"]
             )

    safe_task_id = safe_task.id
    assert [%{task_id: ^safe_task_id, kind: "cancel", payload: %{}}] = descriptors

    assert String.contains?(hd(descriptors).idempotency_key, ":cancel:")

    {cancel_goal, _cancel_item, cancel_task, cancel_runtime, _cancel_run, _cancel_fence} =
      claimed_goal_run_fixture()

    Repo.update_all(
      from(row in Runtime, where: row.id == ^cancel_runtime.id),
      set: [capabilities: %{"adapter" => %{"operations" => %{"pause" => "unsupported"}}}]
    )

    assert {:ok, cancel_amendment, :created} =
             command_current(cancel_goal.id, "amend", %{
               revision_contract: amended_revision_contract("Pause unsupported work."),
               reason: "Scope was clarified"
             })

    cancel_task_id = cancel_task.id

    assert {:ok, [%{task_id: ^cancel_task_id, kind: "cancel", payload: %{}}]} =
             Goals.control_dispatch_plan(
               cancel_goal.id,
               cancel_amendment.goal.current_revision,
               cancel_amendment.response["control_action_id"]
             )
  end

  defp goal_loop_validation_setup do
    {goal, item, producer} =
      admitted_task_fixture("primary", check_contract(), %{
        execution_policy: %{"max_task_admissions" => 3}
      })

    producer_runtime = runtime_fixture(@validation_runtime_id)

    candidate_subject = candidate_subject(item_repository_resource_id(item))

    {producer_run, _producer_fence} =
      completed_goal_run_fixture(producer, producer_runtime, candidate_subject)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(producer.id, producer_run.id, 1, now: @now)

    assert {:error, {:invalid_contract, _}} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 purpose: "validate",
                 validation_of_task_id: producer.id,
                 reserved_microusd: 0
               })
               |> Map.put(:subject, candidate_subject),
               rollout_enabled: true
             )

    assert {:ok, validation_receipt, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 purpose: "validate",
                 validation_of_task_id: producer.id,
                 reserved_microusd: 0
               }),
               rollout_enabled: true
             )

    validation = Repo.get!(Task, validation_receipt.response["task"]["id"])
    assert validation.input["subject"] == candidate_subject
    validation_runtime = runtime_fixture(@validation_runtime_id)

    {validation_run, validation_fence} =
      completed_goal_run_fixture(validation, validation_runtime)

    evidence = evidence_attrs(validation_run.id, item, candidate_subject)

    assert {:ok, %{evidence: stored_evidence}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence,
               now: @now
             )

    assert stored_evidence.subject_hash == evidence.subject_hash

    old_subject_evidence = evidence_attrs(validation_run.id, item)

    assert {:error, :invalid_evidence_identity} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               Map.put(old_subject_evidence, :evidence_key, "check:initial-subject"),
               now: @now
             )

    %{
      goal: goal,
      item: item,
      producer: producer,
      producer_run: producer_run,
      candidate_subject: candidate_subject,
      validation: validation,
      validation_run: validation_run,
      validation_fence: validation_fence,
      validation_runtime: validation_runtime,
      evidence: evidence,
      stored_evidence: stored_evidence
    }
  end

  defp goal_loop_accept_validation(%{
         goal: goal,
         item: item,
         producer: producer,
         producer_run: producer_run,
         candidate_subject: candidate_subject,
         validation: validation,
         validation_run: validation_run,
         validation_fence: validation_fence,
         validation_runtime: validation_runtime,
         evidence: evidence,
         stored_evidence: stored_evidence
       }) do
    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, %{"settlement" => "accepted", "reason" => "validation_passed"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert wakeup_job_count(goal.id) == 1

    assert {:ok, %{evidence: ^stored_evidence}, :replayed} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence,
               now: @now
             )

    assert {:error, :validation_evidence_closed} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence
               |> Map.put(:evidence_id, Ecto.UUID.generate())
               |> Map.put(:evidence_key, "check:late"),
               now: @now
             )

    assert {:error, :already_accepted} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 purpose: "validate",
                 validation_of_task_id: validation.id,
                 reserved_microusd: 0
               }),
               rollout_enabled: true
             )

    subject_hash = String.replace_prefix(evidence.subject_hash, "sha256:", "")

    [accepted_outcome] =
      Repo.all(
        from(outcome in SymmetryControl.Goals.WorkOutcome,
          where:
            outcome.goal_id == ^goal.id and outcome.work_item_id == ^item.id and
              outcome.validation_task_id == ^validation.id and outcome.disposition == "accepted"
        )
      )

    assert accepted_outcome.candidate_subject == candidate_subject
    assert accepted_outcome.producing_result_id == producer_run.result["task_result"]["result_id"]

    assert {:error, :outcome_derived} =
             Goals.accept_outcome(goal.id, %{
               work_item_id: item.id,
               producing_task_id: producer.id,
               producing_run_id: producer_run.id,
               validation_task_id: validation.id,
               subject_hash: subject_hash,
               evidence_ids: [stored_evidence.id],
               reason: "A validator outcome is derived during settlement."
             })

    accepted_outcome
  end

  defp goal_loop_resolve_achieve_decisions(%{
         goal: goal,
         item: item,
         candidate_subject: candidate_subject,
         validation_run: validation_run,
         evidence: evidence,
         stored_evidence: stored_evidence
       }) do
    pre_achieve = fetch_goal!(goal.id)

    assert pre_achieve.completion.ready?
    assert "achieve" in pre_achieve.allowed_actions
    refute "final_acceptance_required" in pre_achieve.blocker_reasons

    assert {:error, {:invalid_contract, _}} =
             command_current(goal.id, "request_decision", %{
               kind: "completion",
               work_item_id: item.id,
               subject_hash: evidence.subject_hash,
               question: "Accept one work item?",
               options: [
                 %{
                   "id" => "accept",
                   "label" => "Accept",
                   "consequence" => "Mark the Goal achieved"
                 }
               ]
             })

    assert {:ok, completion_request, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "completion",
               work_item_id: nil,
               subject_hash: evidence.subject_hash,
               proposal: nil
             })

    decision_id = completion_request.response["decision"]["id"]
    decision_version = Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).lock_version

    assert {:ok, _resolved, :created} =
             command_current(goal.id, "resolve_decision", %{
               decision_id: decision_id,
               expected_decision_version: decision_version,
               option_id: "accept"
             })

    weaker_evidence =
      %SymmetryControl.Goals.RunEvidence{}
      |> SymmetryControl.Goals.RunEvidence.changeset(%{
        run_id: validation_run.id,
        evidence_key: "check:weaker-profile",
        kind: "check",
        subject_hash: SymmetryControl.RequestHash.canonical(evidence.subject),
        source_ref: Map.put(evidence.source_ref, "validator_profile", "weaker"),
        source_revision: evidence.source_revision,
        validator_profile: "weaker",
        verdict: "passed",
        payload: evidence.payload
      })
      |> Repo.insert!()

    assert {:error, :missing_evidence} =
             command_current(goal.id, "achieve", %{
               subject: evidence.subject,
               integration_work_item_id: item.id,
               evidence_ids: [weaker_evidence.id],
               decision_id: decision_id
             })

    assert {:ok, review_request, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "review",
               work_item_id: item.id,
               subject_hash: evidence.subject_hash,
               question: "Accept one WorkItem?",
               options: [
                 %{
                   "id" => "accept",
                   "label" => "Accept",
                   "consequence" => "Accept the validated WorkItem"
                 }
               ]
             })

    review_decision_id = review_request.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(goal.id, "resolve_decision", %{
               decision_id: review_decision_id,
               expected_decision_version:
                 Repo.get!(SymmetryControl.Goals.GoalDecision, review_decision_id).lock_version,
               option_id: "accept"
             })

    assert {:error, :invalid_decision} =
             command_current(goal.id, "achieve", %{
               subject: evidence.subject,
               integration_work_item_id: item.id,
               evidence_ids: [stored_evidence.id],
               decision_id: review_decision_id
             })

    assert {:error, :integration_outcome_required} =
             command_current(goal.id, "achieve", %{
               subject: evidence.subject,
               integration_work_item_id: Ecto.UUID.generate(),
               evidence_ids: [stored_evidence.id],
               decision_id: decision_id
             })

    optional_dependency = optional_goal_work_item!(goal, item, "Optional prerequisite")
    optional_follow_up = optional_goal_work_item!(goal, item, "Optional follow-up")

    %SymmetryControl.Goals.WorkDependency{}
    |> SymmetryControl.Goals.WorkDependency.changeset(%{
      goal_id: goal.id,
      work_item_id: optional_follow_up.id,
      depends_on_id: optional_dependency.id
    })
    |> Repo.insert!()

    assert {:ok, optional_admission, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(optional_dependency),
               rollout_enabled: true
             )

    optional_producer = Repo.get!(Task, optional_admission.response["task"]["id"])

    {optional_run, _optional_fence} =
      completed_goal_run_fixture(optional_producer, runtime_fixture(), candidate_subject)

    insert_accepted_work_outcome!(goal, optional_dependency.id, optional_run, candidate_subject)

    assert {:error, :integration_outcome_required} =
             command_current(goal.id, "achieve", %{
               subject: evidence.subject,
               integration_work_item_id: optional_dependency.id,
               evidence_ids: [stored_evidence.id],
               decision_id: decision_id
             })

    decision_id
  end

  defp goal_loop_achieve_and_replay(
         %{
           goal: goal,
           item: item,
           evidence: evidence,
           stored_evidence: stored_evidence
         },
         accepted_outcome,
         decision_id
       ) do
    achieve_payload = %{
      subject: evidence.subject,
      integration_work_item_id: item.id,
      evidence_ids: [stored_evidence.id],
      decision_id: decision_id
    }

    achieve_goal = fetch_goal!(goal.id)
    achieve_command = goal_command(achieve_goal, "achieve", achieve_payload)

    assert {:ok, achieved, :created} =
             Goals.command(goal.id, achieve_command, "operator:test", now: @now)

    assert achieved.goal.state == "achieved"

    assert Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.id == ^accepted_outcome.id
             )
           )

    achieve_event_count =
      Repo.aggregate(
        from(event in SymmetryControl.Goals.GoalEvent,
          where: event.goal_id == ^goal.id and event.kind == "achieve"
        ),
        :count
      )

    assert achieve_event_count == 1

    assert {:ok, replayed, :replayed} =
             Goals.command(goal.id, achieve_command, "operator:test", now: @now)

    assert replayed == achieved

    assert Repo.aggregate(
             from(event in SymmetryControl.Goals.GoalEvent,
               where: event.goal_id == ^goal.id and event.kind == "achieve"
             ),
             :count
           ) == achieve_event_count

    conflicting_achieve_command =
      put_in(achieve_command, [:payload, :evidence_ids], [])

    assert {:error, :idempotency_conflict} =
             Goals.command(goal.id, conflicting_achieve_command, "operator:test", now: @now)

    assert Repo.aggregate(
             from(event in SymmetryControl.Goals.GoalEvent,
               where: event.goal_id == ^goal.id and event.kind == "achieve"
             ),
             :count
           ) == achieve_event_count
  end

  defp multi_required_goal_fixture! do
    {goal, _items} = goal_with_items_fixture(%{}, 2)

    projection = fetch_goal!(goal.id)

    assert projection.completion.required_items == 2
    assert [integration_item] = Enum.filter(projection.work_items, & &1.integration)

    {goal, projection, integration_item}
  end

  defp optional_goal_work_item!(goal, source_item, title, integration? \\ false) do
    source_item = Repo.get!(WorkItem, source_item.id)

    insert_goal_work_item!(
      %{
        project_id: goal.project_id,
        repository_resource_id: source_item.repository_resource_id,
        title: title,
        description: "Optional work outside the completion dependency closure.",
        status: "backlog",
        priority: "no_priority",
        position: 0,
        assignee_type: "agent",
        assignee_name: source_item.agent_profile,
        agent_profile: source_item.agent_profile,
        workspace: source_item.workspace,
        blocked: false
      },
      %{
        goal_id: goal.id,
        admitted_revision: goal.current_revision,
        required: false,
        integration: integration?,
        acceptance_contract: source_item.acceptance_contract,
        baseline_subject: source_item.baseline_subject,
        baseline_dependency_id: nil,
        change_target: nil
      }
    )
  end

  defp handoff_admission_counts(goal_id) do
    %{
      snapshots: Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count),
      tasks: Repo.aggregate(Task, :count),
      reservations: Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count),
      events: Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count),
      wakeups: wakeup_job_count(goal_id)
    }
  end

  defp check_and_review_contract do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "An independent check and review must pass.",
      "predicates" => [
        %{"id" => "check", "kind" => "check", "validator_profile" => "test"},
        %{"id" => "review", "kind" => "review", "reviewer_profile" => "test-review"}
      ]
    }
  end

  defp assert_branch_target_rejected!(project, repository) do
    attrs =
      goal_attrs(nil, %{
        "allowed_actions" => ["change.upsert"],
        "allowed_resource_ids" => [repository.id]
      })
      |> put_in([:initial_revision, :authority_policy, "allowed_actions"], ["change.upsert"])

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, attrs, "operator:test", now: @now)

    proposal =
      plan_proposal(created.goal.id, [
        %{
          key: "implementation",
          title: "Create the approved provider change",
          description: "Use only the approved repository target.",
          required: true,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          change_target: %{
            kind: "branches",
            source_branch: "codex/goal-0006",
            target_branch: "main"
          },
          baseline: baseline_subject(repository.id)
        }
      ])

    assert {:ok, decision, :created} =
             command_current(created.goal.id, "request_decision", %{
               kind: "plan",
               question: "Accept this provider plan?",
               options: [
                 %{"id" => "accept", "label" => "Accept", "consequence" => "Admit the plan"}
               ],
               proposal: proposal
             })

    decision_id = decision.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(created.goal.id, "resolve_decision", %{
               decision_id: decision_id,
               expected_decision_version:
                 Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).lock_version,
               option_id: "accept"
             })

    assert {:error, :unsupported_capability} =
             command_current(
               created.goal.id,
               "accept_plan",
               %{
                 proposal: proposal,
                 proposal_hash:
                   "sha256:" <>
                     Base.encode16(SymmetryControl.RequestHash.canonical(proposal), case: :lower),
                 decision_id: decision_id
               },
               rollout_enabled: true
             )
  end
end
