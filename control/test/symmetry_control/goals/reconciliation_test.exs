Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.ReconciliationTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces.WorkItem

  @now ~U[2026-09-09 00:00:00.000000Z]
  @validation_runtime_id "00000000-0000-4000-8000-000000000001"
  @validation_profile_digest "sha256:" <> String.duplicate("c", 64)

  use SymmetryControl.TestFixtures,
    command_opts: [now: @now, validation_profiles: validation_profiles()]

  setup :put_goal_validation_profiles

  test "an internal reconcile advance does not refuse an operator command, while another operator's advance still does" do
    {goal, _item, _task} = admitted_task_fixture()

    # The operator's read: the version every command must carry, taken before the
    # reconcile runs. The row's own lock_version is not that version.
    before = fetch_goal!(goal.id)
    lock_version_before = Repo.get!(SymmetryControl.Goals.Goal, goal.id).lock_version

    assert {:ok, [%{disposition: :noop}]} =
             Goals.reconcile_due_goals(goal_ids: [goal.id], now: @now)

    # The due reconcile advanced the row through the optimistic-lock path ...
    assert Repo.get!(SymmetryControl.Goals.Goal, goal.id).lock_version > lock_version_before
    # ... and left the command precondition where the operator read it.
    assert fetch_goal!(goal.id).version == before.version

    assert {:ok, receipt, :created} =
             Goals.command(
               goal.id,
               goal_command(before, "pause", %{reason: "Operator review pause"}),
               "operator:test",
               now: @now
             )

    assert receipt.goal.state == "paused"
    assert receipt.goal.version == before.version + 1
    assert fetch_goal!(goal.id).version == before.version + 1

    # Another operator's accepted command is what a stale version still refuses:
    # the same pre-reconcile version no longer matches after the pause advanced
    # it. Both commands carry actor `operator:test`, so the refusal is the token
    # the pause advanced, not a different actor.
    assert {:error, {:stale, %{current_revision: 1, current_version: current_version}}} =
             Goals.command(
               goal.id,
               goal_command(before, "resume", %{reason: "Operator review complete"}),
               "operator:test",
               now: @now
             )

    assert current_version == receipt.goal.version
    assert fetch_goal!(goal.id).state == "paused"
  end

  test "internal append events never advance the operator version the operator read" do
    # Four append-path internal writers, each driven with a read taken before it
    # runs. The `operator_advance` option is passed on purpose: the advance
    # authority is not readable from a caller's options, so even that call must
    # leave the token where the operator's read took it.
    blocker_policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 2,
      "max_task_admissions" => 1,
      "budget_limit_microusd" => 1_000_000,
      "allowed_actions" => ["implement"]
    }

    {blocked_goal, [first_item, _second_item]} = goal_with_items_fixture(blocker_policy, 2)

    assert {:ok, _admitted, :created} =
             command_current(blocked_goal.id, "admit_task", admission_payload(first_item),
               rollout_enabled: true
             )

    set_goal_due!(blocked_goal.id)
    blocked_before = fetch_goal!(blocked_goal.id)
    blocked_lock_before = Repo.get!(SymmetryControl.Goals.Goal, blocked_goal.id).lock_version

    assert {:ok, [%{disposition: :blocked, reason: "admission_limit_reached"}]} =
             Goals.reconcile_due_goals(
               goal_ids: [blocked_goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles(),
               operator_advance: true
             )

    assert_internal_append_leaves_the_operator_version!(
      blocked_goal,
      blocked_before,
      blocked_lock_before,
      "automatic_admission_blocked"
    )

    # The rollout-off deferral append (`defer_automatic_reconciliation!`), the
    # writer the review named.
    deferred_policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "budget_limit_microusd" => 1_000_000,
      "allowed_actions" => ["implement"]
    }

    {deferred_goal, [_item]} = goal_with_items_fixture(deferred_policy)
    set_goal_due!(deferred_goal.id)
    deferred_before = fetch_goal!(deferred_goal.id)
    deferred_lock_before = Repo.get!(SymmetryControl.Goals.Goal, deferred_goal.id).lock_version

    assert {:ok, [%{disposition: :deferred, reason: "goal_admission_disabled"}]} =
             Goals.reconcile_due_goals(
               goal_ids: [deferred_goal.id],
               now: @now,
               rollout_enabled: false,
               operator_advance: true
             )

    assert_internal_append_leaves_the_operator_version!(
      deferred_goal,
      deferred_before,
      deferred_lock_before,
      "automatic_reconciliation_deferred"
    )

    # The wake path's automatic admission append (`attempt_automatic_admission!`):
    # an automatic goal with admission capacity admits its root item on reconcile.
    admission_policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "budget_limit_microusd" => 1_000_000,
      "allowed_actions" => ["implement"]
    }

    {admission_goal, [_item]} = goal_with_items_fixture(admission_policy)
    set_goal_due!(admission_goal.id)
    admission_before = fetch_goal!(admission_goal.id)
    admission_lock_before = Repo.get!(SymmetryControl.Goals.Goal, admission_goal.id).lock_version

    assert {:ok, reconciliations} =
             Goals.reconcile_due_goals(
               goal_ids: [admission_goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles(),
               operator_advance: true
             )

    assert [%{disposition: :admitted, purpose: "implement"}] =
             Enum.filter(reconciliations, &(&1.disposition == :admitted))

    assert_internal_append_leaves_the_operator_version!(
      admission_goal,
      admission_before,
      admission_lock_before,
      "automatic_task_admitted"
    )

    # The settlement append (`settle_task/4`), driven by a terminal candidate
    # completion for the goal's admitted task.
    {settlement_goal, _item, settled_task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {settlement_run, _fence} = completed_goal_run_fixture(settled_task, runtime)

    settlement_lock_before =
      Repo.get!(SymmetryControl.Goals.Goal, settlement_goal.id).lock_version

    settlement_before = fetch_goal!(settlement_goal.id)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(settled_task.id, settlement_run.id, settlement_run.generation,
               now: @now
             )

    assert_internal_append_leaves_the_operator_version!(
      settlement_goal,
      settlement_before,
      settlement_lock_before,
      "task_settled"
    )
  end

  test "terminal settlement persists one replay-safe wakeup job" do
    {goal, _item, task} = admitted_task_fixture()

    assert {:ok, [%{disposition: :noop}]} =
             Goals.reconcile_due_goals(goal_ids: [goal.id], now: @now)

    Repo.delete_all(
      from(job in Oban.Job,
        where:
          job.worker == "SymmetryControl.Goals.Workers.WakeupWorker" and
            fragment("? ->> 'goal_id' = ?", job.args, ^goal.id)
      )
    )

    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    assert {:ok, first} = Goals.settle_task(task.id, run.id, 1, now: @now)
    assert wakeup_job_count(goal.id) == 1

    assert {:ok, ^first} = Goals.settle_task(task.id, run.id, 1, now: @now)
    assert wakeup_job_count(goal.id) == 1
  end

  test "due reconciliation honors a requested Goal id set" do
    {first, _first_item, _first_task} = admitted_task_fixture()
    {_second, _second_item, _second_task} = admitted_task_fixture()

    assert {:ok, [%{goal_id: goal_id}]} =
             Goals.reconcile_due_goals(goal_ids: [first.id], now: @now)

    assert goal_id == first.id
  end

  test "automatic execution disabled clears a due wake without producing a perpetual audit loop" do
    {goal, _item, _task} = admitted_task_fixture()

    assert {:ok, [%{goal_id: goal_id, disposition: :noop}]} =
             Goals.reconcile_due_goals(goal_ids: [goal.id], now: @now)

    assert goal_id == goal.id
    assert {:ok, refreshed} = Goals.fetch_goal(goal.id)
    assert refreshed.next_wake_at == nil
    assert {:ok, []} = Goals.reconcile_due_goals(goal_ids: [goal.id], now: @now)
  end

  test "automatic reconciliation defers a disabled rollout instead of clearing its recovery wake" do
    policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "budget_limit_microusd" => 1_000_000,
      "allowed_actions" => ["implement"]
    }

    {goal, [_item]} = goal_with_items_fixture(policy)
    set_goal_due!(goal.id)

    assert {:ok,
            [
              %{
                disposition: :deferred,
                reason: "goal_admission_disabled",
                next_wake_at: next_wake_at
              }
            ]} = Goals.reconcile_due_goals(goal_ids: [goal.id], now: @now, rollout_enabled: false)

    assert next_wake_at == DateTime.add(@now, 60, :second)
    assert Repo.get!(SymmetryControl.Goals.Goal, goal.id).next_wake_at == next_wake_at
    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 0
  end

  test "automatic reconciliation records a durable admission limit blocker for unaccepted work" do
    policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 2,
      "max_task_admissions" => 1,
      "budget_limit_microusd" => 1_000_000,
      "allowed_actions" => ["implement"]
    }

    {goal, [first_item, _second_item]} = goal_with_items_fixture(policy, 2)

    assert {:ok, _admitted, :created} =
             command_current(goal.id, "admit_task", admission_payload(first_item),
               rollout_enabled: true
             )

    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :blocked, reason: "admission_limit_reached"}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    event =
      Repo.one!(
        from(event in SymmetryControl.Goals.GoalEvent,
          where:
            event.goal_id == ^goal.id and event.kind == "automatic_admission_blocked" and
              fragment("? ->> 'reason' = 'admission_limit_reached'", event.payload)
        )
      )

    assert event.payload["candidate_identity"] == "goal:admission_limit_reached"
    assert Repo.get!(SymmetryControl.Goals.Goal, goal.id).next_wake_at == nil

    assert {:ok, projection} = Goals.fetch_goal(goal.id)
    assert "automatic_admission_blocked" in projection.blocker_reasons
  end

  test "automatic reconciliation records a business admission blocker, skips its candidate, and admits another" do
    policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 2,
      "max_task_admissions" => 2,
      "budget_limit_microusd" => 1_000_000,
      "allowed_actions" => ["implement"]
    }

    {goal, items} = goal_with_items_fixture(policy, 2)
    [blocked_item | remaining_items] = Enum.sort_by(items, & &1.id)
    [admitted_item] = remaining_items

    Repo.update_all(
      from(item in WorkItem, where: item.id == ^blocked_item.id),
      set: [agent_profile: "unapproved"]
    )

    set_goal_due!(goal.id)

    assert {:ok, reconciliations} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Enum.any?(reconciliations, fn reconciliation ->
             reconciliation.disposition == :blocked and
               reconciliation.work_item_id == blocked_item.id and
               reconciliation.purpose == "implement" and
               reconciliation.reason == "model_not_allowed"
           end)

    assert Enum.any?(reconciliations, fn reconciliation ->
             reconciliation.disposition == :admitted and
               reconciliation.work_item_id == admitted_item.id and
               reconciliation.purpose == "implement"
           end)

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 1

    assert Repo.aggregate(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.goal_id == ^goal.id
             ),
             :count
           ) == 1

    assert Repo.aggregate(
             from(snapshot in SymmetryControl.Goals.ContextSnapshot,
               where: snapshot.goal_id == ^goal.id
             ),
             :count
           ) == 1

    blocked_event =
      Repo.one!(
        from(event in SymmetryControl.Goals.GoalEvent,
          where: event.goal_id == ^goal.id and event.kind == "automatic_admission_blocked"
        )
      )

    assert blocked_event.revision == goal.current_revision
    assert blocked_event.payload["work_item_id"] == blocked_item.id
    assert blocked_event.payload["reason"] == "model_not_allowed"

    assert blocked_event.response["candidate_identity"] ==
             blocked_event.payload["candidate_identity"]

    assert blocked_event.id ==
             frozen_mutation_id("automatic-admission-blocked", [
               goal.id,
               Integer.to_string(goal.current_revision),
               blocked_event.payload["candidate_identity"]
             ])

    assert blocked_event.payload["admission_key"] ==
             frozen_mutation_id("automatic-admission", [
               goal.id,
               Integer.to_string(goal.current_revision),
               blocked_item.id,
               "implement",
               blocked_event.payload["source_identity"]
             ])

    assert {:ok, projection} = Goals.fetch_goal(goal.id)

    blocker = Enum.find(projection.blockers, &(&1.reason == "automatic_admission_blocked"))

    assert blocker.details == [
             %{
               event_id: blocked_event.id,
               work_item_id: blocked_item.id,
               purpose: "implement",
               candidate_identity: blocked_event.payload["candidate_identity"],
               source_identity: blocked_event.payload["source_identity"],
               reason: "model_not_allowed"
             }
           ]

    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :noop}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 1

    assert Repo.aggregate(
             from(event in SymmetryControl.Goals.GoalEvent,
               where: event.goal_id == ^goal.id and event.kind == "automatic_admission_blocked"
             ),
             :count
           ) == 1

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract: amended_revision_contract("Supersede the blocked candidate."),
               reason: "Revise the automatic admission policy."
             })

    assert amended.goal.current_revision == 2
    assert {:ok, amended_projection} = Goals.fetch_goal(goal.id)
    refute "automatic_admission_blocked" in amended_projection.blocker_reasons
  end

  test "automatic context admission rejection commits only its blocker receipt" do
    {goal, item, nil} =
      admitted_task_fixture(
        "primary",
        check_contract(),
        %{
          execution_policy: %{
            "automatic_execution" => true,
            "max_task_admissions" => 1,
            "max_parallel_tasks" => 1,
            "budget_limit_microusd" => 1_000_000,
            "allowed_actions" => ["implement"]
          },
          context_manifest: %{
            "byte_budget" => 1,
            "required_source_kinds" => ["repository"],
            "include_advisory_recall" => false
          }
        },
        false
      )

    set_goal_due!(goal.id)

    assert {:ok,
            [
              %{
                disposition: :blocked,
                work_item_id: work_item_id,
                purpose: "implement",
                reason: "context_budget_exceeded"
              }
            ]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert work_item_id == item.id
    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 0

    assert Repo.aggregate(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.goal_id == ^goal.id
             ),
             :count
           ) == 0

    assert Repo.aggregate(
             from(snapshot in SymmetryControl.Goals.ContextSnapshot,
               where: snapshot.goal_id == ^goal.id
             ),
             :count
           ) == 0

    assert Repo.aggregate(
             from(usage in SymmetryControl.Goals.RunUsage,
               join: run in Run,
               on: run.id == usage.run_id,
               join: task in Task,
               on: task.id == run.task_id,
               where: task.goal_id == ^goal.id
             ),
             :count
           ) == 0

    assert Repo.aggregate(
             from(event in SymmetryControl.Goals.GoalEvent,
               where: event.goal_id == ^goal.id and event.kind == "automatic_admission_blocked"
             ),
             :count
           ) == 1

    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :noop}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )
  end

  test "automatic reconciliation defers a mutable validation profile configuration and retries after repair" do
    policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "budget_limit_microusd" => 1_000_000,
      "allowed_actions" => ["implement"]
    }

    {goal, [item]} = goal_with_items_fixture(policy)
    set_goal_due!(goal.id)

    work_item_id = item.id

    assert {:ok,
            [
              %{
                disposition: :deferred,
                reason: "invalid_validation_profile",
                next_wake_at: next_wake_at
              }
            ]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: []
             )

    assert next_wake_at == DateTime.add(@now, 60, :second)
    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 0

    assert Repo.aggregate(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.goal_id == ^goal.id
             ),
             :count
           ) == 0

    assert Repo.aggregate(
             from(snapshot in SymmetryControl.Goals.ContextSnapshot,
               where: snapshot.goal_id == ^goal.id
             ),
             :count
           ) == 0

    refute Repo.exists?(
             from(event in SymmetryControl.Goals.GoalEvent,
               where: event.goal_id == ^goal.id and event.kind == "automatic_admission_blocked"
             )
           )

    assert {:ok, reconciliations} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: next_wake_at,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert [%{disposition: :admitted, work_item_id: ^work_item_id}] =
             Enum.filter(reconciliations, &(&1.disposition == :admitted))
  end

  test "automatic deferred receipts distinguish changing reasons for one candidate" do
    policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "budget_limit_microusd" => 1_000_000,
      "allowed_actions" => ["implement"],
      "allowed_runtime_ids" => [Ecto.UUID.generate()]
    }

    {goal, [_item]} = goal_with_items_fixture(policy)
    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :deferred, reason: "validation_runtime_not_allowed"}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :deferred, reason: "invalid_validation_profile"}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: []
             )

    deferred_events =
      Repo.all(
        from(event in SymmetryControl.Goals.GoalEvent,
          where: event.goal_id == ^goal.id and event.kind == "automatic_reconciliation_deferred",
          order_by: [asc: event.sequence]
        )
      )

    assert Enum.map(deferred_events, & &1.response["reason"]) == [
             "validation_runtime_not_allowed",
             "invalid_validation_profile"
           ]

    assert Enum.map(deferred_events, & &1.id) ==
             Enum.map(deferred_events, fn event ->
               frozen_mutation_id("automatic-reconciliation-deferred", [
                 goal.id,
                 Integer.to_string(goal.current_revision),
                 event.payload["deferred_identity"],
                 event.response["reason"]
               ])
             end)
  end

  test "automatic execution derives one dependency baseline from its accepted current outcome" do
    project = project_fixture()
    repository = repository_fixture(project)
    source_runtime = runtime_fixture()
    validation_runtime = runtime_fixture(@validation_runtime_id)

    automatic_policy = %{
      "automatic_execution" => true,
      "max_task_admissions" => 6,
      "allowed_runtime_ids" => [source_runtime.id, validation_runtime.id],
      "allowed_actions" => ["implement", "validate"],
      "allowed_resource_ids" => [repository.id],
      "budget_limit_microusd" => 1_000_000
    }

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(nil, automatic_policy), "operator:test",
               now: @now
             )

    proposal =
      plan_proposal(created.goal.id, [
        %{
          key: "source",
          title: "Source work",
          description: "Produce the dependency Subject.",
          required: true,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: baseline_subject(repository.id)
        },
        %{
          key: "dependent",
          title: "Dependent work",
          description: "Use only the accepted source Subject.",
          required: true,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: ["source"],
          model_profile: "codex",
          baseline: %{kind: "dependency", key: "source"}
        }
      ])

    _planned = accept_plan!(created.goal.id, proposal)
    current_goal = fetch_goal!(created.goal.id)
    source_item = Enum.find(current_goal.work_items, &(&1.title == "Source work"))
    dependent_item = Enum.find(current_goal.work_items, &(&1.title == "Dependent work"))

    candidate_subject = candidate_subject(repository.id)

    configure_automatic_runtimes!(dependent_item, [source_runtime, validation_runtime])

    assert {:ok, _active, :created} =
             command_current(created.goal.id, "activate", %{approved_revision: 1})

    assert {:ok, producer_admission, :created} =
             command_current(
               created.goal.id,
               "admit_task",
               admission_payload(source_item, %{reserved_microusd: 0}),
               rollout_enabled: true
             )

    producer = Repo.get!(Task, producer_admission.response["task"]["id"])

    {producer_run, _producer_fence} =
      completed_goal_run_fixture(producer, source_runtime, candidate_subject)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(producer.id, producer_run.id, 1, now: @now)

    assert {:ok, validation_admission, :created} =
             command_current(
               created.goal.id,
               "admit_task",
               admission_payload(source_item, %{
                 purpose: "validate",
                 validation_of_task_id: producer.id,
                 reserved_microusd: 0
               }),
               rollout_enabled: true
             )

    validation = Repo.get!(Task, validation_admission.response["task"]["id"])

    {validation_run, validation_fence} =
      completed_goal_run_fixture(validation, validation_runtime, candidate_subject)

    assert {:ok, %{evidence: _stored}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence_attrs(validation_run.id, source_item, candidate_subject),
               now: @now
             )

    assert {:ok, %{"settlement" => "accepted"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    set_goal_due!(created.goal.id)

    assert {:ok, [%{disposition: :admitted, work_item_id: dependent_id}]} =
             Goals.reconcile_due_goals(
               goal_ids: [created.goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert dependent_id == dependent_item.id
    dependent_task = Repo.get_by!(Task, goal_id: created.goal.id, work_item_id: dependent_item.id)
    assert dependent_task.input["subject"] == candidate_subject

    set_goal_due!(created.goal.id)

    assert {:ok, [%{disposition: :deferred, reason: "admission_capacity_unavailable"}]} =
             Goals.reconcile_due_goals(
               goal_ids: [created.goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Repo.aggregate(
             from(task in Task,
               where: task.goal_id == ^created.goal.id and task.work_item_id == ^dependent_item.id
             ),
             :count
           ) == 1
  end

  test "strict automatic and manual admissions derive the operator-approved per-run ceiling" do
    project = project_fixture()

    incomplete_strict_policy = %{
      "automatic_execution" => false,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "max_run_attempts_per_task" => 2,
      "budget_limit_microusd" => 239,
      "per_run_cost_limit_microusd" => nil,
      "budget_mode" => "strict",
      "hard_cost_limit_required" => false,
      "allowed_runtime_ids" => [],
      "allowed_model_profiles" => ["codex"],
      "final_acceptance" => "operator",
      "allowed_actions" => ["implement"],
      "allowed_resource_ids" => []
    }

    assert {:error, _reason} =
             Goals.create_goal(
               project.id,
               goal_attrs(nil, incomplete_strict_policy),
               "operator:test",
               now: @now
             )

    strict_policy = %{
      "automatic_execution" => false,
      "max_parallel_tasks" => 2,
      "max_task_admissions" => 2,
      "max_run_attempts_per_task" => 2,
      "budget_limit_microusd" => 120,
      "per_run_cost_limit_microusd" => 60,
      "budget_mode" => "strict",
      "hard_cost_limit_required" => true,
      "allowed_runtime_ids" => [],
      "allowed_model_profiles" => ["codex"],
      "final_acceptance" => "operator",
      "allowed_actions" => ["implement"],
      "allowed_resource_ids" => []
    }

    {goal, [first_item, second_item]} = goal_with_items_fixture(strict_policy, 2)

    assert {:error, {:invalid_contract, _}} =
             command_current(
               goal.id,
               "admit_task",
               Map.put(admission_payload(first_item), :reserved_microusd, 60),
               rollout_enabled: true
             )

    assert {:ok, first_admission, :created} =
             command_current(goal.id, "admit_task", admission_payload(first_item),
               rollout_enabled: true
             )

    first_task = Repo.get!(Task, first_admission.response["task"]["id"])

    # The strict-budget admission keeps its nested adapter requirement as
    # Control-side scheduling data, while the daemon's `required_capabilities`
    # wire type is a boolean map: the served task document must carry only
    # boolean members — here none — and never the nested adapter member.
    assert first_task.required_capabilities["adapter"]["operations"]["hard_cost_limit"] == true

    served_work = SymmetryControlWeb.Protocol.task(first_task).work

    assert Enum.all?(Map.get(served_work, :required_capabilities, %{}), fn {_key, value} ->
             is_boolean(value)
           end)

    first_reservation =
      Repo.one!(
        from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
          where: reservation.task_id == ^first_task.id
        )
      )

    assert first_reservation.reserved_microusd == 120

    assert first_task.input["limits"]["max_turns"] == 1

    assert first_task.input["limits"]["deadline_at"] ==
             DateTime.to_iso8601(DateTime.add(@now, 15 * 60, :second))

    assert {:error, :budget_exhausted} =
             command_current(goal.id, "admit_task", admission_payload(second_item),
               rollout_enabled: true
             )

    automatic_policy = Map.merge(strict_policy, %{"automatic_execution" => true})

    {automatic_goal, [automatic_item, automatic_second_item]} =
      goal_with_items_fixture(automatic_policy, 2)

    set_goal_due!(automatic_goal.id)

    assert {:ok, reconciliations} =
             Goals.reconcile_due_goals(
               goal_ids: [automatic_goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert [%{disposition: :admitted, purpose: "implement", task_id: task_id}] =
             Enum.filter(reconciliations, &(&1.disposition == :admitted))

    assert [] = Enum.filter(reconciliations, &(&1.disposition == :blocked))

    automatic_task = Repo.get!(Task, task_id)

    assert automatic_task.work_item_id == automatic_item.id
    assert automatic_task.input["limits"]["max_turns"] == 1
    assert automatic_task.work_item_id != automatic_second_item.id

    assert Repo.aggregate(
             from(task in Task, where: task.goal_id == ^automatic_goal.id),
             :count
           ) == 1

    assert Repo.one!(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.task_id == ^automatic_task.id
             )
           ).reserved_microusd == 120
  end

  defp assert_internal_append_leaves_the_operator_version!(
         goal,
         before,
         lock_version_before,
         kind
       ) do
    event =
      Repo.one!(
        from(event in SymmetryControl.Goals.GoalEvent,
          where: event.goal_id == ^goal.id and event.kind == ^kind,
          order_by: [desc: event.sequence],
          limit: 1
        )
      )

    # The internal append wrote the row through the optimistic lock ...
    assert Repo.get!(SymmetryControl.Goals.Goal, goal.id).lock_version > lock_version_before
    # ... and left the command precondition where the operator's read took it,
    # in the served goal and in the event's own stored receipt.
    assert fetch_goal!(goal.id).version == before.version
    assert event.response["_receipt_v1"]["goal"]["version"] == before.version

    # A command built from that pre-append read is still accepted.
    assert {:ok, receipt, :created} =
             Goals.command(
               goal.id,
               goal_command(before, "pause", %{reason: "Operator review pause"}),
               "operator:test",
               now: @now
             )

    assert receipt.goal.version == before.version + 1
    assert fetch_goal!(goal.id).version == before.version + 1
  end
end
