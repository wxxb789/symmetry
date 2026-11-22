Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.RecoveryTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Goals.Workers.WakeupWorker
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo

  @now ~U[2026-09-09 00:00:00.000000Z]
  @validation_runtime_id "00000000-0000-4000-8000-000000000001"

  use SymmetryControl.TestFixtures,
    command_opts: [now: @now, validation_profiles: validation_profiles()]

  setup :put_goal_validation_profiles

  test "an amended Goal accepts a new revision plan without rewriting historical work" do
    {goal, item, _task} = admitted_task_fixture()

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract:
                 amended_revision_contract("A revised plan requires new admitted work."),
               reason: "The prior revision is retained as history."
             })

    proposal = %{
      schema_version: "symmetry.plan.v1",
      proposal_id: Ecto.UUID.generate(),
      goal_id: amended.goal.id,
      expected_revision: amended.goal.current_revision,
      items: [
        %{
          key: "revised-work",
          title: "Revised work",
          description: "Admitted under the revised Goal contract.",
          required: true,
          integration: true,
          repository_resource_id: item_repository_resource_id(item),
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          change_target: nil,
          baseline: baseline_subject(item_repository_resource_id(item))
        }
      ]
    }

    assert {:ok, decision, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "plan",
               question: "Accept the revised plan?",
               options: [
                 %{"id" => "accept", "label" => "Accept", "consequence" => "Admit revised work"}
               ],
               proposal: proposal
             })

    decision_id = decision.response["decision"]["id"]
    decision_version = Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).lock_version

    assert {:ok, _resolved, :created} =
             command_current(goal.id, "resolve_decision", %{
               decision_id: decision_id,
               expected_decision_version: decision_version,
               option_id: "accept"
             })

    assert {:ok, _planned, :created} =
             command_current(
               goal.id,
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

    current_goal = fetch_goal!(goal.id)
    [revised_item] = current_goal.work_items
    assert revised_item.id != item.id
    assert revised_item.admitted_revision == current_goal.current_revision
    assert Repo.get!(SymmetryControl.Workspaces.WorkItem, revised_item.id).integration
    assert Enum.any?(current_goal.history.work_items, &(&1.id == item.id))
  end

  test "operator acceptance requires a current scoped review and wakes closed validation evaluation once" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} =
      independent_validation_attempt_fixture(check_and_operator_acceptance_contract())

    evidence = evidence_attrs(validation_run.id, item, candidate_subject)

    assert {:ok, %{evidence: _stored}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence,
               now: @now
             )

    assert {:ok, original_settlement_receipt} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert original_settlement_receipt["settlement"] == "awaiting_validation"

    subject_hash = evidence_subject_hash(candidate_subject)

    assert {:ok, completion, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "completion",
               subject_hash: subject_hash
             })

    completion_id = completion.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(
               goal.id,
               "resolve_decision",
               %{
                 decision_id: completion_id,
                 expected_decision_version:
                   Repo.get!(SymmetryControl.Goals.GoalDecision, completion_id).lock_version,
                 option_id: "accept"
               },
               now: DateTime.add(@now, 1, :second)
             )

    assert {:ok, ^original_settlement_receipt} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert {:error, {:invalid_contract, _}} =
             command_current(goal.id, "request_decision", %{
               kind: "review",
               subject_hash: subject_hash,
               question: "Accept without an item?",
               options: [
                 %{"id" => "accept", "label" => "Accept", "consequence" => "Incorrect scope"}
               ]
             })

    assert {:ok, ^original_settlement_receipt} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert {:ok, wrong_subject_review, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "review",
               work_item_id: item.id,
               subject_hash:
                 evidence_subject_hash(%{
                   candidate_subject
                   | "commit" => String.duplicate("e", 40)
                 }),
               question: "Accept the wrong subject?",
               options: [
                 %{"id" => "accept", "label" => "Accept", "consequence" => "Incorrect subject"}
               ]
             })

    wrong_subject_review_id = wrong_subject_review.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(
               goal.id,
               "resolve_decision",
               %{
                 decision_id: wrong_subject_review_id,
                 expected_decision_version:
                   Repo.get!(SymmetryControl.Goals.GoalDecision, wrong_subject_review_id).lock_version,
                 option_id: "accept"
               },
               now: DateTime.add(@now, 3, :second)
             )

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    refute Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where:
                 outcome.validation_task_id == ^validation.id and
                   outcome.disposition == "accepted"
             )
           )

    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, scoped_review, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "review",
               work_item_id: item.id,
               subject_hash: subject_hash,
               question: "Accept this validated WorkItem?",
               options: [%{"id" => "accept", "label" => "Accept", "consequence" => "Accept work"}]
             })

    scoped_review_id = scoped_review.response["decision"]["id"]
    assert {:ok, current_goal} = Goals.fetch_goal(goal.id)

    resolve_command =
      goal_command(current_goal, "resolve_decision", %{
        decision_id: scoped_review_id,
        expected_decision_version:
          Repo.get!(SymmetryControl.Goals.GoalDecision, scoped_review_id).lock_version,
        option_id: "accept"
      })

    assert {:ok, _resolved, :created} =
             Goals.command(
               goal.id,
               resolve_command,
               "operator:test",
               now: DateTime.add(@now, 4, :second),
               validation_profiles: validation_profiles()
             )

    assert wakeup_job_count(goal.id) == 1

    assert {:ok, _replayed, :replayed} =
             Goals.command(
               goal.id,
               resolve_command,
               "operator:test",
               now: DateTime.add(@now, 4, :second),
               validation_profiles: validation_profiles()
             )

    enable_rollout!()

    assert :ok = WakeupWorker.perform(%Oban.Job{args: %{"goal_id" => goal.id}})
    assert :ok = WakeupWorker.perform(%Oban.Job{args: %{"goal_id" => goal.id}})

    assert [late_outcome_event] =
             Repo.all(
               from(event in SymmetryControl.Goals.GoalEvent,
                 where:
                   event.goal_id == ^goal.id and event.kind == "validation_outcome_settled" and
                     event.payload["task_id"] == ^validation.id,
                 order_by: [asc: event.sequence]
               )
             )

    assert late_outcome_event.response["settlement"] == "accepted"

    assert {:ok, late_receipt} =
             Goals.reconcile_validation_outcome(validation.id, validation_run.id, 1, now: @now)

    assert late_receipt["settlement"] == "accepted"
    assert late_receipt["event_sequence"] == late_outcome_event.sequence
    refute late_receipt["mutation_id"] == original_settlement_receipt["mutation_id"]

    # Durable idempotency keys (settlement events, admission keys): freeze the delivered
    # bytes so a formatter change fails here instead of silently rewriting stored keys.
    assert original_settlement_receipt["mutation_id"] ==
             frozen_mutation_id("settlement", [validation.id, validation_run.id, "1"])

    assert late_receipt["mutation_id"] ==
             frozen_mutation_id("late-validation-outcome", [validation.id, validation_run.id, "1"])

    assert {:ok, original_replay} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert original_replay == original_settlement_receipt

    assert {:ok, late_replay} =
             Goals.reconcile_validation_outcome(validation.id, validation_run.id, 1, now: @now)

    assert late_replay == late_receipt
    assert wakeup_job_count(goal.id) == 1

    assert {:ok, projection} = Goals.fetch_goal(goal.id)
    assert Enum.any?(projection.accepted_outcomes, &(&1.work_item_id == item.id))

    assert [%{decision_id: ^scoped_review_id}] =
             Repo.all(
               from(outcome in SymmetryControl.Goals.WorkOutcome,
                 where:
                   outcome.validation_task_id == ^validation.id and
                     outcome.disposition == "accepted",
                 select: %{decision_id: outcome.decision_id}
               )
             )
  end

  test "failed independent validation records one immutable rejection and cannot be bypassed by passed evidence from that attempt" do
    {goal, item, producer, producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    failed_evidence =
      evidence_attrs(validation_run.id, item, candidate_subject)
      |> Map.put(:evidence_id, Ecto.UUID.generate())
      |> Map.put(:evidence_key, "check:failed")
      |> Map.put(:verdict, "failed")
      |> put_in([:payload, :exit_code], 1)

    assert {:ok, %{evidence: failed}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               failed_evidence,
               now: @now
             )

    passed_evidence =
      evidence_attrs(validation_run.id, item, candidate_subject)
      |> Map.put(:evidence_id, Ecto.UUID.generate())
      |> Map.put(:evidence_key, "check:passed")

    assert {:ok, %{evidence: passed}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               passed_evidence,
               now: @now
             )

    assert {:ok, receipt} = Goals.settle_task(validation.id, validation_run.id, 1, now: @now)
    assert receipt["settlement"] == "validation_failed"
    assert receipt["reason"] == "validation_failed"
    assert receipt["next_wake_at"] == nil

    [rejected] =
      Repo.all(
        from(outcome in SymmetryControl.Goals.WorkOutcome,
          where:
            outcome.goal_id == ^goal.id and outcome.work_item_id == ^item.id and
              outcome.disposition == "rejected"
        )
      )

    assert rejected.goal_revision == goal.current_revision
    assert rejected.producing_task_id == producer.id
    assert rejected.producing_run_id == producer_run.id
    assert rejected.validation_task_id == validation.id
    assert rejected.candidate_subject == candidate_subject
    assert rejected.subject_hash == SymmetryControl.RequestHash.canonical(candidate_subject)
    assert rejected.producing_result_id == producer_run.result["task_result"]["result_id"]
    assert rejected.evidence_ids == [failed.id]
    assert rejected.reason == "validation_failed"

    subject_hash = evidence_subject_hash(candidate_subject)

    assert {:error, :outcome_derived} =
             Goals.accept_outcome(goal.id, %{
               work_item_id: item.id,
               producing_task_id: producer.id,
               producing_run_id: producer_run.id,
               validation_task_id: validation.id,
               subject_hash: String.replace_prefix(subject_hash, "sha256:", ""),
               evidence_ids: [passed.id],
               reason: "A passed subset cannot hide a required failed check."
             })

    assert {:ok, ^receipt} = Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert Repo.aggregate(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where:
                 outcome.validation_task_id == ^validation.id and
                   outcome.disposition == "rejected"
             ),
             :count
           ) == 1

    assert {:ok, retry_admission, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 purpose: "validate",
                 validation_of_task_id: producer.id
               }),
               rollout_enabled: true
             )

    retry_validation = Repo.get!(Task, retry_admission.response["task"]["id"])
    retry_runtime = runtime_fixture(@validation_runtime_id)

    {retry_run, retry_fence} =
      completed_goal_run_fixture(retry_validation, retry_runtime, candidate_subject)

    retry_evidence =
      evidence_attrs(retry_run.id, item, candidate_subject)
      |> Map.put(:evidence_key, "check:retry-passed")

    assert {:ok, %{evidence: retry_passed}, :created} =
             Goals.append_evidence(
               retry_runtime.machine_id,
               retry_run.id,
               retry_fence,
               retry_evidence,
               now: @now
             )

    assert {:ok, %{"settlement" => "accepted", "reason" => "validation_passed"}} =
             Goals.settle_task(retry_validation.id, retry_run.id, 1, now: @now)

    assert Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where:
                 outcome.goal_id == ^goal.id and outcome.work_item_id == ^item.id and
                   outcome.validation_task_id == ^retry_validation.id and
                   outcome.disposition == "accepted" and
                   outcome.evidence_ids == ^[retry_passed.id]
             )
           )
  end

  test "terminal settlement recovery respects the lease-expiry grace boundary" do
    {_goal, _item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "failed"])

    Repo.update_all(
      from(row in Run, where: row.id == ^run.id),
      set: [state: "failed", failure: %{"reason" => "lease_expired"}, updated_at: @now]
    )

    assert {:ok, []} = Goals.recover_pending_terminal_settlements(now: @now)

    assert {:ok, [%{task_id: task_id, run_id: run_id, generation: 1}]} =
             Goals.recover_pending_terminal_settlements(now: DateTime.add(@now, 8 * 60, :second))

    assert task_id == task.id
    assert run_id == run.id
  end

  test "OMP resume admission requires the retained kind and versions to match exactly" do
    {goal, item, nil} = admitted_task_fixture("primary", check_contract(), %{}, false)
    runtime = runtime_fixture()

    metadata = %{
      harness_kind: "omp",
      harness_version: "18.20.0",
      adapter_version: "symmetry-omp-1",
      capabilities: %{"adapter" => %{"operations" => %{"resume" => true}}}
    }

    Repo.update_all(from(row in Runtime, where: row.id == ^runtime.id),
      set: Map.to_list(metadata)
    )

    runtime = Repo.get!(Runtime, runtime.id)

    attrs =
      session_attrs(item, "omp-retained-workspace")
      |> Map.merge(Map.take(metadata, [:harness_kind, :harness_version, :adapter_version]))

    retained = available_session_fixture(runtime, item, attrs)

    counts_before = %{
      snapshots: Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count),
      tasks: Repo.aggregate(Task, :count),
      reservations: Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count),
      events: Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count)
    }

    for changed <- [
          %{harness_kind: "pi"},
          %{harness_version: "18.20.1"},
          %{adapter_version: "symmetry-omp-2"}
        ] do
      Repo.update_all(from(row in Runtime, where: row.id == ^runtime.id),
        set: Map.to_list(Map.merge(metadata, changed))
      )

      assert {:error, :requested_session_unavailable} =
               command_current(
                 goal.id,
                 "admit_task",
                 admission_payload(item, %{
                   session_mode: "resume",
                   requested_session_id: retained.id
                 }),
                 rollout_enabled: true
               )

      assert counts_before ==
               %{
                 snapshots: Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count),
                 tasks: Repo.aggregate(Task, :count),
                 reservations:
                   Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count),
                 events: Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count)
               }
    end

    Repo.update_all(from(row in Runtime, where: row.id == ^runtime.id),
      set: Map.to_list(metadata)
    )

    assert {:ok, admission, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 session_mode: "resume",
                 requested_session_id: retained.id
               }),
               rollout_enabled: true
             )

    task = Repo.get!(Task, admission.response["task"]["id"])
    assert task.requested_session_id == retained.id
    assert task.input["session_mode"] == "resume"

    assert %{harness_kind: "omp", state: "available", active_run_id: nil} =
             Repo.get!(HarnessSession, retained.id)
  end
end
