Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.SettlementTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.GoalExternalWait
  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Goals.Workers.GoalControlWorker
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo

  @now ~U[2026-09-09 00:00:00.000000Z]
  @validation_runtime_id "00000000-0000-4000-8000-000000000001"

  use SymmetryControl.TestFixtures,
    command_opts: [now: @now, validation_profiles: validation_profiles()]

  setup :put_goal_validation_profiles

  test "plan_proposed from a WorkItem Task settles as a stable invalid receipt" do
    {goal, item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    proposal =
      plan_proposal(goal.id, [
        %{
          key: "misplaced-plan",
          title: "Misplaced plan result",
          description: "A WorkItem Task must not create planning authority.",
          required: true,
          integration: true,
          repository_resource_id: item_repository_resource_id(item),
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: baseline_subject(item_repository_resource_id(item))
        }
      ])

    put_run_task_result!(run, task_result(task, "plan_proposed", %{proposal: proposal}))

    assert {:ok, receipt} = Goals.settle_task(task.id, run.id, 1, now: @now)
    assert receipt["settlement"] == "invalid_task_result"
    assert receipt["reason"] == "invalid_task_result"

    assert {:ok, replayed} = Goals.settle_task(task.id, run.id, 1, now: @now)
    assert replayed == receipt
  end

  test "an amendment releases a rejected completion Subject for the revised revision" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} =
      independent_validation_attempt_fixture(check_contract(), %{
        acceptance_contract: check_and_operator_acceptance_contract()
      })

    {evidence, _stored_evidence} =
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

    resource_id = item_repository_resource_id(item)

    revision_contract =
      goal_attrs()
      |> Map.fetch!(:initial_revision)
      |> Map.drop([:reason])
      |> Map.put(:objective, "Re-admit the rejected Subject under a revised revision.")
      |> Map.put(:acceptance_contract, check_and_operator_acceptance_contract())
      |> Map.update!(:execution_policy, fn policy ->
        Map.merge(policy, %{
          "max_task_admissions" => 8,
          "budget_limit_microusd" => "42",
          "allowed_resource_ids" => [resource_id],
          "allowed_model_profiles" => ["codex"]
        })
      end)

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract: revision_contract,
               reason: "The prior rejection is bound to the prior revision."
             })

    assert amended.goal.state == "paused"
    assert amended.goal.current_revision == 2

    proposal = %{
      schema_version: "symmetry.plan.v1",
      proposal_id: Ecto.UUID.generate(),
      goal_id: amended.goal.id,
      expected_revision: amended.goal.current_revision,
      items: [
        %{
          key: "revised-work",
          title: "Revised work",
          description: "Re-admits the same Subject under revision 2.",
          required: true,
          integration: true,
          repository_resource_id: resource_id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          change_target: nil,
          baseline: baseline_subject(resource_id)
        }
      ]
    }

    accept_plan!(goal.id, proposal)

    [revised_item] = fetch_goal!(goal.id).work_items

    assert {:ok, _resumed, :created} =
             command_current(goal.id, "resume", %{reason: "Revised work is approved."})

    assert {:ok, admitted, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(revised_item),
               rollout_enabled: true
             )

    revised_producer = Repo.get!(Task, admitted.response["task"]["id"])

    {revised_producer_run, _revised_fence} =
      completed_goal_run_fixture(revised_producer, runtime_fixture(), candidate_subject)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(revised_producer.id, revised_producer_run.id, 1, now: @now)

    assert {:ok, revised_validation, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(revised_item, %{
                 purpose: "validate",
                 validation_of_task_id: revised_producer.id
               }),
               rollout_enabled: true
             )

    revised_validation_task = Repo.get!(Task, revised_validation.response["task"]["id"])
    revised_validation_runtime = runtime_fixture(@validation_runtime_id)

    {revised_validation_run, revised_validation_fence} =
      completed_goal_run_fixture(
        revised_validation_task,
        revised_validation_runtime,
        candidate_subject
      )

    revised_evidence = evidence_attrs(revised_validation_run.id, revised_item, candidate_subject)

    assert {:ok, %{evidence: revised_stored_evidence}, :created} =
             Goals.append_evidence(
               revised_validation_runtime.machine_id,
               revised_validation_run.id,
               revised_validation_fence,
               revised_evidence,
               now: @now
             )

    assert {:ok, %{"settlement" => "accepted"}} =
             Goals.settle_task(
               revised_validation_task.id,
               revised_validation_run.id,
               1,
               now: @now
             )

    assert {:ok, accept_request, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "completion",
               subject_hash: revised_evidence.subject_hash
             })

    accept_decision_id = accept_request.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(goal.id, "resolve_decision", %{
               decision_id: accept_decision_id,
               expected_decision_version:
                 Repo.get!(SymmetryControl.Goals.GoalDecision, accept_decision_id).lock_version,
               option_id: "accept"
             })

    projection = fetch_goal!(goal.id)

    assert projection.completion.ready?
    assert "achieve" in projection.allowed_actions

    assert {:ok, achieved, :created} =
             Goals.command(
               goal.id,
               goal_command(fetch_goal!(goal.id), "achieve", %{
                 subject: revised_evidence.subject,
                 evidence_ids: [revised_stored_evidence.id],
                 decision_id: nil
               }),
               "operator:test",
               now: @now,
               validation_profiles: validation_profiles()
             )

    assert achieved.goal.state == "achieved"
  end

  test "incomplete validation closes its evidence set without accepting work" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    refute Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.goal_id == ^goal.id and outcome.validation_task_id == ^validation.id
             )
           )

    late_evidence =
      evidence_attrs(validation_run.id, item, candidate_subject)
      |> Map.put(:evidence_id, Ecto.UUID.generate())
      |> Map.put(:evidence_key, "check:late-after-incomplete")

    assert {:error, :validation_evidence_closed} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               late_evidence,
               now: @now
             )
  end

  test "automatic reconciliation does not admit an accepted current-revision WorkItem" do
    policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 4,
      "allowed_actions" => ["validate"],
      "allowed_model_profiles" => ["codex"],
      "allowed_runtime_ids" => [],
      "budget_limit_microusd" => 1_000_000
    }

    {goal, item, producer} =
      admitted_task_fixture("primary", check_contract(), %{execution_policy: policy})

    candidate_subject = candidate_subject(item_repository_resource_id(item))

    {producer_run, _producer_fence} =
      completed_goal_run_fixture(producer, runtime_fixture(), candidate_subject)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(producer.id, producer_run.id, 1, now: @now)

    insert_accepted_work_outcome!(goal, item.id, producer_run, candidate_subject)

    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :noop}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 1
  end

  test "failed Runs preserve safe normalized semantic reasons without deriving outcomes or retries" do
    for {shape, expected_reason} <- [
          {:daemon_terminal_payload, "unknown_outcome"},
          {:persisted_failure, "lease_expired"},
          {:persisted_failure, "attempt_limit"},
          {:persisted_failure, "supervised_worker_lost"},
          {:existing_task_result, "network"}
        ] do
      {goal, _item, task, _runtime, run, _fence} = claimed_goal_run_fixture()

      result =
        case shape do
          :daemon_terminal_payload ->
            %{
              "summary" => "Native terminal framing failed.",
              "reason" => "unknown_outcome",
              "error" => "close failed"
            }

          :persisted_failure ->
            %{"summary" => "A terminal daemon failure occurred.", "reason" => expected_reason}

          :existing_task_result ->
            %{"task_result" => task_result(task, "failed", %{"reason" => "network"})}
        end

      Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "failed"])

      Repo.update_all(
        from(row in Run, where: row.id == ^run.id),
        set:
          if(shape in [:daemon_terminal_payload, :persisted_failure],
            do: [state: "failed", result: nil, failure: result],
            else: [state: "failed", result: result, failure: nil]
          )
      )

      assert {:ok, %{"settlement" => "failed", "reason" => ^expected_reason}} =
               Goals.settle_task(task.id, run.id, 1, now: @now)

      refute Repo.exists?(
               from(outcome in SymmetryControl.Goals.WorkOutcome,
                 where: outcome.goal_id == ^goal.id
               )
             )

      assert Repo.aggregate(from(candidate in Run, where: candidate.task_id == ^task.id), :count) ==
               1
    end

    for invalid_result <- [
          nil,
          %{"task_result" => %{"reason" => "unknown_outcome"}},
          %{"summary" => "Untrusted", "reason" => "outside_whitelist"},
          %{"reason" => "unknown_outcome"}
        ] do
      {invalid_goal, _invalid_item, invalid_task, _invalid_runtime, invalid_run, _invalid_fence} =
        claimed_goal_run_fixture()

      Repo.update_all(from(row in Task, where: row.id == ^invalid_task.id),
        set: [state: "failed"]
      )

      Repo.update_all(
        from(row in Run, where: row.id == ^invalid_run.id),
        set: [state: "failed", result: nil, failure: invalid_result]
      )

      assert {:ok, %{"settlement" => "failed", "reason" => "process_failure"}} =
               Goals.settle_task(invalid_task.id, invalid_run.id, 1, now: @now)

      refute Repo.exists?(
               from(outcome in SymmetryControl.Goals.WorkOutcome,
                 where: outcome.goal_id == ^invalid_goal.id
               )
             )
    end
  end

  test "invalid blockers settle durably without scheduling a retry loop" do
    {goal, _item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    put_run_task_result!(
      run,
      task_result(task, "blocked", %{
        "blocker" => %{"kind" => "decision", "decision_id" => Ecto.UUID.generate()}
      })
    )

    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, receipt} = Goals.settle_task(task.id, run.id, 1, now: @now)
    assert receipt["settlement"] == "invalid_task_result"
    assert receipt["reason"] == "invalid_blocker"
    assert receipt["next_wake_at"] == nil
    assert wakeup_job_count(goal.id) == 0

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "unknown"
  end

  test "repair and replan results are retained as proposals without admitting work" do
    {goal, item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)
    tasks_before = Repo.aggregate(Task, :count)

    repair = %{
      "kind" => "repair",
      "work_item_id" => item.id,
      "reason" => "Fix the failing check."
    }

    put_run_task_result!(
      run,
      task_result(task, "repair_required", %{
        "proposed_next_action" => repair
      })
    )

    assert {:ok, repair_receipt} = Goals.settle_task(task.id, run.id, 1, now: @now)
    assert repair_receipt["settlement"] == "repair_required"
    assert repair_receipt["proposed_next_action"] == repair
    assert repair_receipt["proposed_next_action_status"] == "proposal_only"
    assert repair_receipt["next_wake_at"] == DateTime.to_iso8601(@now)
    assert Repo.aggregate(Task, :count) == tasks_before

    assert {:ok, admitted, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item),
               rollout_enabled: true
             )

    replan_task = Repo.get!(Task, admitted.response["task"]["id"])
    {replan_run, _fence} = completed_goal_run_fixture(replan_task, runtime)
    replan = %{"kind" => "replan", "reason" => "The approved plan needs a scope decision."}

    put_run_task_result!(
      replan_run,
      task_result(replan_task, "replan_required", %{
        "proposed_next_action" => replan
      })
    )

    assert {:ok, replan_receipt} =
             Goals.settle_task(replan_task.id, replan_run.id, 1, now: @now)

    assert replan_receipt["settlement"] == "replan_required"
    assert replan_receipt["proposed_next_action"] == replan
    assert replan_receipt["proposed_next_action_status"] == "proposal_only"
    assert replan_receipt["next_wake_at"] == nil
    assert Repo.aggregate(Task, :count) == tasks_before + 1
  end

  test "duplicate progress without a new Subject or verified evidence stops at no_verified_progress" do
    {goal, item, first_task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {first_run, _fence} = completed_goal_run_fixture(first_task, runtime)
    first_progress = task_result(first_task, "progress")

    put_run_task_result!(first_run, first_progress)
    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, %{"settlement" => "progress", "next_wake_at" => next_wake_at}} =
             Goals.settle_task(first_task.id, first_run.id, 1, now: @now)

    assert next_wake_at == DateTime.to_iso8601(@now)

    assert {:ok, admitted, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item),
               rollout_enabled: true
             )

    second_task = Repo.get!(Task, admitted.response["task"]["id"])
    {second_run, _fence} = completed_goal_run_fixture(second_task, runtime)
    put_run_task_result!(second_run, task_result(second_task, "progress"))
    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, receipt} = Goals.settle_task(second_task.id, second_run.id, 1, now: @now)
    assert receipt["settlement"] == "no_verified_progress"
    assert receipt["next_wake_at"] == nil
    assert wakeup_job_count(goal.id) == 0
  end

  test "admission defaults off and terminal usage gaps remain unknown without a caller reservation" do
    {goal, item, task} = admitted_task_fixture()

    assert {:error, :goal_admission_disabled} =
             command_current(goal.id, "admit_task", admission_payload(item))

    Repo.update_all(from(row in Task, where: row.id == ^task.id),
      set: [state: "completed", current_generation: 1, updated_at: @now]
    )

    runtime = runtime_fixture()

    run =
      %Run{}
      |> Run.changeset(%{
        task_id: task.id,
        runtime_id: runtime.id,
        generation: 1,
        state: "completed",
        assigned_at: @now,
        assignment_expires_at: DateTime.add(@now, 60, :second)
      })
      |> Repo.insert!()

    assert {:ok, %{"settlement" => "missing_result"}} =
             Goals.settle_task(task.id, run.id, 1, now: @now)

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "unknown"

    assert {:ok, admitted, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item),
               rollout_enabled: true
             )

    assert Repo.one!(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.task_id == ^admitted.response["task"]["id"],
               select: reservation.reserved_microusd
             )
           ) == nil
  end

  test "automatic execution admits one independent validation Task from a settled candidate" do
    producer_runtime = runtime_fixture()

    {goal, item, producer} =
      admitted_task_fixture("primary", check_contract(), %{
        execution_policy: %{
          "automatic_execution" => true,
          "max_parallel_tasks" => 1,
          "max_task_admissions" => 2,
          "budget_limit_microusd" => 1_000_000,
          "allowed_actions" => ["implement", "validate"]
        }
      })

    Repo.update_all(
      from(runtime in Runtime, where: runtime.id == ^producer_runtime.id),
      set: [
        repository_resource_id: item_repository_resource_id(item),
        capabilities: %{"adapter" => %{"operations" => %{"start" => true}}}
      ]
    )

    candidate_subject = candidate_subject(item_repository_resource_id(item))

    {producer_run, _fence} =
      completed_goal_run_fixture(producer, producer_runtime, candidate_subject)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(producer.id, producer_run.id, 1, now: @now)

    assert {:ok, reconciliations} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert [
             %{
               goal_id: goal_id,
               disposition: :admitted,
               task_id: validation_task_id,
               work_item_id: work_item_id,
               purpose: "validate"
             }
           ] = Enum.filter(reconciliations, &(&1.disposition == :admitted))

    assert goal_id == goal.id
    assert work_item_id == item.id

    validation = Repo.get!(Task, validation_task_id)
    assert validation.validation_of_task_id == producer.id
    assert validation.input["subject"] == candidate_subject

    assert {:ok, refreshed} = Goals.fetch_goal(goal.id)
    assert refreshed.next_wake_at == nil

    Repo.update_all(from(row in SymmetryControl.Goals.Goal, where: row.id == ^goal.id),
      set: [next_wake_at: @now]
    )

    assert {:ok, [%{goal_id: ^goal_id, disposition: :noop}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Repo.aggregate(
             from(task in Task, where: task.goal_id == ^goal.id and task.purpose == "validate"),
             :count
           ) == 1
  end

  test "automatic progress continuation binds the current terminal result Subject and result identity" do
    {goal, item, task} =
      admitted_task_fixture("primary", check_contract(), %{
        execution_policy: %{
          "automatic_execution" => true,
          "max_task_admissions" => 2,
          "budget_limit_microusd" => 1_000_000,
          "allowed_actions" => ["implement"]
        }
      })

    runtime = runtime_fixture()

    progressed_subject = candidate_subject(item_repository_resource_id(item))

    {run, _fence} = completed_goal_run_fixture(task, runtime, progressed_subject)
    progress_result = task_result(task, "progress", %{"subject" => progressed_subject})
    put_run_task_result!(run, progress_result)

    assert {:ok, %{"settlement" => "progress"}} =
             Goals.settle_task(task.id, run.id, 1, now: @now)

    set_goal_due!(goal.id)

    assert {:ok, reconciliations} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert [%{disposition: :admitted, task_id: continuation_task_id, purpose: "implement"}] =
             Enum.filter(reconciliations, &(&1.disposition == :admitted))

    continuation = Repo.get!(Task, continuation_task_id)
    assert continuation.id != task.id
    assert continuation.input["subject"] == progressed_subject

    event =
      Repo.one!(
        from(event in SymmetryControl.Goals.GoalEvent,
          where: event.goal_id == ^goal.id and event.kind == "automatic_task_admitted",
          order_by: [desc: event.sequence],
          limit: 1
        )
      )

    assert event.payload["source_task_id"] == task.id
    assert event.payload["source_result_id"] == progress_result["result_id"]
    assert event.response["source_identity"] =~ progress_result["result_id"]
  end

  test "automatic reconciliation quiesces after an unattended terminal failure" do
    {goal, item, task} =
      admitted_task_fixture(
        "primary",
        check_contract(),
        %{
          execution_policy: %{
            "automatic_execution" => true,
            "max_task_admissions" => 2,
            "max_parallel_tasks" => 1,
            "budget_limit_microusd" => 1_000_000,
            "allowed_actions" => ["implement"]
          }
        }
      )

    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "failed"])

    Repo.update_all(
      from(row in Run, where: row.id == ^run.id),
      set: [state: "failed", result: nil, failure: %{"reason" => "network"}]
    )

    assert {:ok, %{"settlement" => "failed"}} =
             Goals.settle_task(task.id, run.id, run.generation, now: @now)

    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :noop}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Repo.get!(SymmetryControl.Goals.Goal, goal.id).next_wake_at == nil
    assert {:ok, projection} = Goals.fetch_goal(goal.id)
    assert projection.settlement_attention != []
    assert item.id == hd(projection.settlement_attention).work_item_id
  end

  test "handoff creates a fresh Task from one current settled source Run" do
    {goal, item, source_task} =
      admitted_task_fixture("primary", check_contract(), %{
        execution_policy: %{"max_task_admissions" => 3}
      })

    source_runtime = runtime_fixture()
    {source_run, _source_fence} = completed_goal_run_fixture(source_task, source_runtime)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(source_task.id, source_run.id, source_run.generation, now: @now)

    assert {:ok, admission, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 session_mode: "handoff",
                 handoff_source_run_id: source_run.id
               }),
               rollout_enabled: true
             )

    target = Repo.get!(Task, admission.response["task"]["id"])
    snapshot = Repo.get!(SymmetryControl.Goals.ContextSnapshot, target.context_snapshot_id)

    assert target.id != source_task.id
    assert target.handoff_source_run_id == source_run.id
    assert target.requested_session_id == nil
    assert target.input["session_mode"] == "handoff"
    assert target.input["handoff_source_run_id"] == source_run.id
    assert target.input["subject"] == source_run.result["task_result"]["subject"]

    assert Enum.any?(snapshot.payload["sources"], fn source ->
             source["source_kind"] == "handoff_source" and
               source["source_revision"] == "run:" <> source_run.id and
               source["content"]["value"] == "run:" <> source_run.id and
               source["trust"] == "advisory" and source["required"] == true
           end)

    assert Repo.aggregate(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.task_id == ^target.id
             ),
             :count
           ) == 1

    assert Repo.get!(SymmetryControl.Goals.Goal, goal.id).state == "active"

    refute Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where:
                 outcome.goal_id == ^goal.id and outcome.work_item_id == ^item.id and
                   outcome.disposition == "accepted"
             )
           )

    complete_task_and_release_reservation!(goal, target)

    assert {:error, :handoff_source_consumed} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 session_mode: "handoff",
                 handoff_source_run_id: source_run.id
               }),
               rollout_enabled: true
             )
  end

  test "handoff rejects an earlier settled source instead of rolling work back from the progression tip" do
    {goal, item, first_task} =
      admitted_task_fixture("primary", check_contract(), %{
        execution_policy: %{"max_task_admissions" => 4}
      })

    runtime = runtime_fixture()
    {first_run, _first_fence} = completed_goal_run_fixture(first_task, runtime)
    replace_terminal_result!(first_task, first_run, task_result(first_task, "progress"))

    assert {:ok, %{"settlement" => "progress"}} =
             Goals.settle_task(first_task.id, first_run.id, first_run.generation, now: @now)

    assert {:ok, second_admission, :created} =
             command_current(goal.id, "admit_task", admission_payload(item),
               rollout_enabled: true
             )

    second_task = Repo.get!(Task, second_admission.response["task"]["id"])

    Repo.update_all(from(task in Task, where: task.id == ^second_task.id),
      set: [inserted_at: DateTime.add(@now, 1, :second)]
    )

    progressed_subject = %{
      "resource_id" => item_repository_resource_id(item),
      "commit" => String.duplicate("e", 40),
      "tree_digest" => "sha256:" <> String.duplicate("f", 64)
    }

    {second_run, _second_fence} =
      completed_goal_run_fixture(second_task, runtime, progressed_subject)

    replace_terminal_result!(
      second_task,
      second_run,
      task_result(second_task, "progress", %{"subject" => progressed_subject})
    )

    assert {:ok, %{"settlement" => "progress"}} =
             Goals.settle_task(second_task.id, second_run.id, second_run.generation, now: @now)

    assert {:error, :handoff_source_stale} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 session_mode: "handoff",
                 handoff_source_run_id: first_run.id
               }),
               rollout_enabled: true
             )
  end

  test "validation handoff binds the exact settled source producer" do
    {goal, item, source_task} =
      admitted_task_fixture("primary", check_contract(), %{
        execution_policy: %{"max_task_admissions" => 3}
      })

    source_runtime = runtime_fixture()
    {source_run, _source_fence} = completed_goal_run_fixture(source_task, source_runtime)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(source_task.id, source_run.id, source_run.generation, now: @now)

    assert {:ok, admission, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 purpose: "validate",
                 session_mode: "handoff",
                 handoff_source_run_id: source_run.id,
                 validation_of_task_id: source_task.id
               }),
               rollout_enabled: true
             )

    target = Repo.get!(Task, admission.response["task"]["id"])
    assert target.validation_of_task_id == source_task.id
    assert target.handoff_source_run_id == source_run.id
    assert target.input["subject"] == source_run.result["task_result"]["subject"]
  end

  test "handoff session attach replays only its own fresh native handle" do
    {goal, item, source_task} =
      admitted_task_fixture("primary", check_contract(), %{
        execution_policy: %{"max_task_admissions" => 3}
      })

    source_runtime = runtime_fixture()
    {source_run, _source_fence} = completed_goal_run_fixture(source_task, source_runtime)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(source_task.id, source_run.id, source_run.generation, now: @now)

    assert {:ok, admission, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 session_mode: "handoff",
                 handoff_source_run_id: source_run.id
               }),
               rollout_enabled: true
             )

    target = Repo.get!(Task, admission.response["task"]["id"])
    {target_run, target_fence} = running_goal_run_fixture(target, source_runtime)
    attrs = session_attrs(item, "workspace-handoff-target")

    assert {:error, :invalid_request} =
             Goals.attach_harness_session(
               source_runtime.machine_id,
               target_run.id,
               target_fence,
               Map.put(attrs, :binding_id, Ecto.UUID.generate()),
               now: @now
             )

    assert {:ok, %{session: %{run_id: run_id, local_handle_id: local_handle_id}}, :created} =
             Goals.attach_harness_session(
               source_runtime.machine_id,
               target_run.id,
               target_fence,
               attrs,
               now: @now
             )

    assert run_id == target_run.id
    assert local_handle_id == attrs.local_handle_id

    assert {:ok, %{session: %{id: session_id}}, :replayed} =
             Goals.attach_harness_session(
               source_runtime.machine_id,
               target_run.id,
               target_fence,
               attrs,
               now: @now
             )

    assert Repo.aggregate(
             from(session in HarnessSession, where: session.id == ^session_id),
             :count
           ) == 1
  end

  test "handoff session attach rejects a handle already owned by another Run" do
    {goal, item, source_task} =
      admitted_task_fixture("primary", check_contract(), %{
        execution_policy: %{"max_task_admissions" => 3}
      })

    source_runtime = runtime_fixture()
    {source_run, _source_fence} = completed_goal_run_fixture(source_task, source_runtime)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(source_task.id, source_run.id, source_run.generation, now: @now)

    assert {:ok, admission, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 session_mode: "handoff",
                 handoff_source_run_id: source_run.id
               }),
               rollout_enabled: true
             )

    target = Repo.get!(Task, admission.response["task"]["id"])
    {target_run, target_fence} = running_goal_run_fixture(target, source_runtime)
    attrs = session_attrs(item, "workspace-handoff-conflict")
    available_session_fixture(source_runtime, item, attrs)

    assert {:error, :handoff_session_reuse} =
             Goals.attach_harness_session(
               source_runtime.machine_id,
               target_run.id,
               target_fence,
               attrs,
               now: @now
             )

    assert Repo.get!(Run, target_run.id).harness_session_id == nil
  end

  test "terminal unknown cost does not block a soft-budget amendment resume" do
    {goal, _item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(task.id, run.id, run.generation, now: @now)

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "unknown"

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract:
                 amended_revision_contract("Resume after unknown terminal cost."),
               reason: "A terminal usage receipt is still pending."
             })

    assert {:ok, resumed, :created} =
             command_current(goal.id, "resume", %{reason: "Continue under the amended policy."})

    assert amended.goal.current_revision == 2
    assert resumed.goal.state == "active"
  end

  test "strict mode without a configured budget limit still blocks terminal unknown cost" do
    {goal, _item, task} =
      admitted_task_fixture("primary", check_contract(), %{
        execution_policy: %{
          "budget_mode" => "strict",
          "budget_limit_microusd" => nil,
          "per_run_cost_limit_microusd" => 1,
          "hard_cost_limit_required" => true
        }
      })

    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(task.id, run.id, run.generation, now: @now)

    assert {:ok, _paused, :created} =
             command_current(goal.id, "pause", %{reason: "Review unknown accounting."})

    assert {:error, :budget_unknown} =
             command_current(goal.id, "resume", %{reason: "Do not bypass strict accounting."})
  end

  test "runless cancellation settlement requires a canonical Goal control cancellation" do
    enable_rollout!()

    {_goal, _item, task} = admitted_task_fixture()
    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "cancelled"])

    assert {:error, :invalid_unstarted_settlement} =
             Goals.settle_unstarted_task(task.id, "cancelled", now: @now)

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "held"

    {controlled_goal, _controlled_item, controlled_task} = admitted_task_fixture()

    assert {:ok, cancelled, :created} =
             command_current(controlled_goal.id, "cancel", %{reason: "Stop before a Run starts."})

    assert :ok =
             GoalControlWorker.perform(%Oban.Job{
               args: %{
                 "goal_id" => controlled_goal.id,
                 "revision" => cancelled.goal.current_revision,
                 "action_id" => cancelled.response["control_action_id"]
               }
             })

    assert {:ok, %{"settlement" => "released_without_run"} = receipt} =
             Goals.settle_unstarted_task(controlled_task.id, "cancelled", now: @now)

    assert {:ok, ^receipt} =
             Goals.settle_unstarted_task(controlled_task.id, "cancelled", now: @now)

    settlement_event =
      Repo.one!(
        from(event in SymmetryControl.Goals.GoalEvent,
          where:
            event.goal_id == ^controlled_goal.id and
              event.kind == "task_unstarted_settled" and
              fragment("? ->> 'task_id' = ?", event.payload, ^controlled_task.id)
        )
      )

    assert settlement_event.id ==
             frozen_mutation_id("unstarted-settlement", [controlled_task.id, "cancelled"])

    assert Repo.aggregate(from(run in Run, where: run.task_id == ^controlled_task.id), :count) ==
             0

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^controlled_task.id
             )
           ).state == "released"

    assert {:error, :invalid_request} =
             Goals.settle_unstarted_task(controlled_task.id, "failed", now: @now)

    assert {:ok, projection} = Goals.fetch_goal(controlled_goal.id)
    assert projection.execution.run_count == 0
  end

  test "historical task settlement retains the producing Task revision in its audit receipt" do
    {goal, _item, task, _runtime, run, _fence} = claimed_goal_run_fixture()

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract:
                 amended_revision_contract("Record historical settlement accurately."),
               reason: "The active work is now historical."
             })

    assert amended.goal.current_revision == 2

    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "completed"])
    Repo.update_all(from(row in Run, where: row.id == ^run.id), set: [state: "completed"])

    assert {:ok, %{"goal_revision" => 1, "settlement" => "missing_result"}} =
             Goals.settle_task(task.id, run.id, run.generation, now: @now)

    event =
      Repo.one!(
        from(event in SymmetryControl.Goals.GoalEvent,
          where: event.goal_id == ^goal.id and event.kind == "task_settled",
          order_by: [desc: event.sequence],
          limit: 1
        )
      )

    assert event.revision == 1
    assert event.payload["goal_revision"] == 1
    assert event.response["goal_revision"] == 1
    assert event.response["_receipt_v1"]["event"]["revision"] == 1
  end

  test "an external blocker is retained as unsupported history without a model observation" do
    policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 5,
      "max_run_attempts_per_task" => 2,
      "budget_limit_microusd" => 1_000_000,
      "per_run_cost_limit_microusd" => nil,
      "budget_mode" => "soft",
      "hard_cost_limit_required" => false,
      "allowed_runtime_ids" => [],
      "allowed_model_profiles" => ["codex"],
      "final_acceptance" => "operator",
      "allowed_actions" => ["implement", "observe"],
      "allowed_resource_ids" => []
    }

    {goal, [item]} = goal_with_items_fixture(policy)

    assert {:ok, admission, :created} =
             command_current(goal.id, "admit_task", admission_payload(item),
               rollout_enabled: true
             )

    source_task = Repo.get!(Task, admission.response["task"]["id"])
    runtime = runtime_fixture()
    {source_run, _source_fence} = completed_goal_run_fixture(source_task, runtime)
    requested_check_at = DateTime.add(@now, 60, :second)

    external_blocker = %{
      "kind" => "external",
      "resource_id" => item_repository_resource_id(item),
      "external_ref" => "ci://build/unsupported",
      "next_check_at" => DateTime.to_iso8601(requested_check_at)
    }

    blocked_result =
      task_result(source_task, "blocked", %{
        "blocker" => external_blocker,
        "proposed_next_action" => %{
          "kind" => "observe",
          "resource_id" => item_repository_resource_id(item),
          "external_ref" => external_blocker["external_ref"]
        }
      })

    put_run_task_result!(source_run, blocked_result)

    assert {:ok, receipt} = Goals.settle_task(source_task.id, source_run.id, 1, now: @now)
    assert receipt["settlement"] == "blocked"
    assert receipt["reason"] == "unsupported_external_check"
    assert receipt["next_wake_at"] == nil
    assert receipt["proposed_next_action"] == nil

    wait = Repo.get_by!(GoalExternalWait, goal_id: goal.id, work_item_id: item.id)
    assert wait.state == "unsupported"
    assert wait.next_check_at == nil
    assert wait.check_seq == 0
    assert wait.result == blocked_result
    assert wait.subject == source_task.input["subject"]
    assert wait.resource_id == item_repository_resource_id(item)
    assert wait.external_ref == external_blocker["external_ref"]
    assert wait.source_ref["task_id"] == source_task.id
    assert wait.source_ref["run_id"] == source_run.id
    assert wait.source_ref["generation"] == source_run.generation
    assert wait.source_ref["result_id"] == blocked_result["result_id"]
    assert wait.receipt_event_id == receipt["mutation_id"]

    assert {:ok, projection} = Goals.fetch_goal(goal.id)
    assert projection.next_wake_at == nil
    assert "unsupported_external_check" in projection.blocker_reasons
    refute "waiting_external" in projection.blocker_reasons

    assert [
             %{id: wait_id, state: "unsupported", reason: "unsupported_external_check"} =
               projected_wait
           ] =
             projection.external_waits

    assert wait_id == wait.id
    assert projected_wait.subject == wait.subject
    assert projected_wait.source_ref == wait.source_ref
    assert projected_wait.resource_id == wait.resource_id
    assert projected_wait.external_ref == wait.external_ref

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 1

    assert Repo.aggregate(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.goal_id == ^goal.id
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

    assert Repo.get!(GoalExternalWait, wait.id).state == "unsupported"

    assert Repo.get!(SymmetryControl.Goals.Goal, goal.id).next_wake_at == nil

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 1

    assert {:error, :unsupported_external_check} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{purpose: "observe"}),
               rollout_enabled: true
             )
  end

  test "automatic repair creates a fresh implementation Task with immutable source identity" do
    policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 2,
      "max_run_attempts_per_task" => 2,
      "budget_limit_microusd" => 1_000_000,
      "per_run_cost_limit_microusd" => nil,
      "budget_mode" => "soft",
      "hard_cost_limit_required" => false,
      "allowed_runtime_ids" => [],
      "allowed_model_profiles" => ["codex"],
      "final_acceptance" => "operator",
      "allowed_actions" => ["implement"],
      "allowed_resource_ids" => []
    }

    {goal, [item]} = goal_with_items_fixture(policy)

    assert {:ok, admission, :created} =
             command_current(goal.id, "admit_task", admission_payload(item),
               rollout_enabled: true
             )

    source_task = Repo.get!(Task, admission.response["task"]["id"])
    runtime = runtime_fixture()
    {source_run, _source_fence} = completed_goal_run_fixture(source_task, runtime)

    repair_action = %{
      "kind" => "repair",
      "work_item_id" => item.id,
      "reason" => "Fix the failed check."
    }

    repair_result =
      task_result(source_task, "repair_required", %{
        "proposed_next_action" => repair_action
      })

    put_run_task_result!(source_run, repair_result)

    assert {:ok, repair_receipt} =
             Goals.settle_task(source_task.id, source_run.id, 1, now: @now)

    assert repair_receipt["settlement"] == "repair_required"
    assert repair_receipt["next_wake_at"] == DateTime.to_iso8601(@now)

    assert {:ok, refreshed} = Goals.fetch_goal(goal.id)
    assert DateTime.compare(refreshed.next_wake_at, @now) == :eq

    assert {:ok, reconciliations} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert [%{disposition: :admitted, purpose: "implement", task_id: repair_task_id}] =
             Enum.filter(reconciliations, &(&1.disposition == :admitted))

    repair_task = Repo.get!(Task, repair_task_id)
    assert repair_task.id != source_task.id
    assert repair_task.purpose == "implement"
    assert repair_task.input["subject"] == source_task.input["subject"]
    assert repair_task.input["limits"]["max_turns"] == 1

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 2

    event =
      Repo.one!(
        from(event in SymmetryControl.Goals.GoalEvent,
          where:
            event.goal_id == ^goal.id and event.kind == "automatic_task_admitted" and
              fragment("? ->> 'task_id' = ?", event.payload, ^repair_task.id)
        )
      )

    assert event.payload["source_task_id"] == source_task.id
    assert event.payload["source_result_id"] == repair_result["result_id"]
    assert event.response["source_identity"] =~ repair_result["result_id"]

    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :noop}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 2
  end

  defp replace_terminal_result!(task, run, task_result) do
    result = %{"task_result" => task_result}

    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [result: result])
    Repo.update_all(from(row in Run, where: row.id == ^run.id), set: [result: result])
  end
end
