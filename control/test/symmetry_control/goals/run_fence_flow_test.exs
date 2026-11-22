Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.RunFenceFlowTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.RunEvidence
  alias SymmetryControl.Goals.Workers.WakeupWorker
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo

  @now ~U[2026-09-09 00:00:00.000000Z]
  @validation_runtime_id "00000000-0000-4000-8000-000000000001"
  @validation_profile_digest "sha256:" <> String.duplicate("c", 64)

  use SymmetryControl.TestFixtures,
    command_opts: [now: @now, validation_profiles: validation_profiles()]

  setup :put_goal_validation_profiles

  test "an amendment retains prior spent admissions under the Goal admission cap" do
    {goal, item, task, runtime, run, fence} = claimed_goal_run_fixture()

    Repo.update_all(from(row in Task, where: row.id == ^task.id),
      set: [state: "completed", current_generation: 1, updated_at: @now]
    )

    Repo.update_all(from(row in Run, where: row.id == ^run.id),
      set: [state: "completed", updated_at: @now]
    )

    assert {:ok, %{usage: %{cost_microusd: "42"}}, :created} =
             Goals.record_usage(runtime.machine_id, run.id, fence, usage_attrs(run.id), now: @now)

    assert Repo.one!(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.task_id == ^task.id
             )
           ).state == "settled"

    revision_contract = amended_revision_contract("Do not reset spent admissions on amendment.")

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract: revision_contract,
               reason: "Continue within the original Goal budget."
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

    [revised_item] = fetch_goal!(goal.id).work_items

    assert {:ok, _resumed, :created} =
             command_current(goal.id, "resume", %{reason: "Revised work is approved."})

    assert {:error, :admission_limit_reached} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(revised_item, %{reserved_microusd: 1}),
               rollout_enabled: true
             )
  end

  test "evidence stores observed_at in its row timestamp without polluting the payload" do
    {_goal, item, _task, runtime, run, fence} = validation_goal_run_fixture()
    observed_at = DateTime.add(@now, -60, :second)

    evidence =
      evidence_attrs(run.id, item)
      |> Map.put(:observed_at, DateTime.to_iso8601(observed_at))

    assert {:ok, %{evidence: receipt}, :created} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, evidence, now: @now)

    row = Repo.get!(SymmetryControl.Goals.RunEvidence, evidence.evidence_id)
    assert row.inserted_at == observed_at
    refute Map.has_key?(row.payload, "_observed_at")
    assert receipt.observed_at == DateTime.to_iso8601(observed_at)

    assert {:ok, %{evidence: ^receipt}, :replayed} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, evidence, now: @now)
  end

  test "deterministic final acceptance closes the Goal loop without a completion Decision" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} =
      independent_validation_attempt_fixture(check_contract(), %{
        execution_policy: %{"final_acceptance" => "deterministic"},
        authority_policy: %{"operator_required_for_completion" => false}
      })

    evidence = evidence_attrs(validation_run.id, item, candidate_subject)

    assert {:ok, %{evidence: stored_evidence}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence,
               now: @now
             )

    assert {:ok, %{"settlement" => "accepted", "reason" => "validation_passed"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    [accepted_outcome] =
      Repo.all(
        from(outcome in SymmetryControl.Goals.WorkOutcome,
          where:
            outcome.goal_id == ^goal.id and outcome.work_item_id == ^item.id and
              outcome.validation_task_id == ^validation.id and outcome.disposition == "accepted"
        )
      )

    assert accepted_outcome.decision_id == nil

    refute Repo.exists?(
             from(decision in SymmetryControl.Goals.GoalDecision,
               where:
                 decision.goal_id == ^goal.id and
                   decision.goal_revision == ^goal.current_revision and
                   decision.kind == "completion"
             )
           )

    assert {:ok, achieved, :created} =
             command_current(goal.id, "achieve", %{
               subject: evidence.subject,
               integration_work_item_id: item.id,
               evidence_ids: [stored_evidence.id],
               decision_id: nil
             })

    assert achieved.goal.state == "achieved"
  end

  test "an accepted current-revision WorkItem rejects later manual admission" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    assert {:ok, %{evidence: _stored}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence_attrs(validation_run.id, item, candidate_subject),
               now: @now
             )

    assert {:ok, %{"settlement" => "accepted"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert {:error, :already_accepted} =
             command_current(goal.id, "admit_task", admission_payload(item),
               rollout_enabled: true
             )
  end

  test "operator review rejection settles the corresponding validation as rejected once" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} =
      independent_validation_attempt_fixture(check_and_operator_acceptance_contract())

    assert {:ok, %{evidence: _stored}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence_attrs(validation_run.id, item, candidate_subject),
               now: @now
             )

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert {:ok, review, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "review",
               work_item_id: item.id,
               subject_hash: evidence_subject_hash(candidate_subject),
               question: "Reject this validated WorkItem?",
               options: [
                 %{"id" => "accept", "label" => "Accept", "consequence" => "Accept work"},
                 %{"id" => "reject", "label" => "Reject", "consequence" => "Keep work open"}
               ]
             })

    review_id = review.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(goal.id, "resolve_decision", %{
               decision_id: review_id,
               expected_decision_version:
                 Repo.get!(SymmetryControl.Goals.GoalDecision, review_id).lock_version,
               option_id: "reject"
             })

    assert :ok = WakeupWorker.perform(%Oban.Job{args: %{"goal_id" => goal.id}})
    assert :ok = WakeupWorker.perform(%Oban.Job{args: %{"goal_id" => goal.id}})

    validation_id = validation.id

    assert [
             %{
               disposition: "rejected",
               decision_id: ^review_id,
               validation_task_id: ^validation_id
             }
           ] =
             Repo.all(
               from(outcome in SymmetryControl.Goals.WorkOutcome,
                 where:
                   outcome.validation_task_id == ^validation.id and
                     outcome.disposition == "rejected",
                 select: %{
                   disposition: outcome.disposition,
                   decision_id: outcome.decision_id,
                   validation_task_id: outcome.validation_task_id
                 }
               )
             )

    assert [%{response: response}] =
             Repo.all(
               from(event in SymmetryControl.Goals.GoalEvent,
                 where:
                   event.goal_id == ^goal.id and event.kind == "validation_outcome_settled" and
                     event.payload["task_id"] == ^validation.id,
                 select: %{response: event.response}
               )
             )

    assert response["settlement"] == "rejected"
    assert {:ok, []} = Goals.recover_pending_terminal_settlements(goal_ids: [goal.id], now: @now)
  end

  test "a competing accepted Subject is historical and does not loop validation recovery" do
    {goal, item, _producer, producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} =
      independent_validation_attempt_fixture(check_and_operator_acceptance_contract())

    assert {:ok, %{evidence: _stored}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence_attrs(validation_run.id, item, candidate_subject),
               now: @now
             )

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    accepted_subject = Map.put(candidate_subject, "commit", String.duplicate("a", 40))

    insert_accepted_work_outcome!(goal, item.id, producer_run, accepted_subject)

    assert {:ok, review, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "review",
               work_item_id: item.id,
               subject_hash: evidence_subject_hash(candidate_subject),
               question: "Accept this validated WorkItem?",
               options: [%{"id" => "accept", "label" => "Accept", "consequence" => "Accept work"}]
             })

    review_id = review.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(goal.id, "resolve_decision", %{
               decision_id: review_id,
               expected_decision_version:
                 Repo.get!(SymmetryControl.Goals.GoalDecision, review_id).lock_version,
               option_id: "accept"
             })

    validation_id = validation.id

    assert {:ok, [%{task_id: ^validation_id}]} =
             Goals.recover_pending_terminal_settlements(goal_ids: [goal.id], now: @now)

    assert :ok = WakeupWorker.perform(%Oban.Job{args: %{"goal_id" => goal.id}})
    assert :ok = WakeupWorker.perform(%Oban.Job{args: %{"goal_id" => goal.id}})

    assert [%{disposition: "rejected", reason: "historical_result"}] =
             Repo.all(
               from(outcome in SymmetryControl.Goals.WorkOutcome,
                 where: outcome.validation_task_id == ^validation_id,
                 select: %{disposition: outcome.disposition, reason: outcome.reason}
               )
             )

    assert [%{response: response}] =
             Repo.all(
               from(event in SymmetryControl.Goals.GoalEvent,
                 where:
                   event.goal_id == ^goal.id and event.kind == "validation_outcome_settled" and
                     event.payload["task_id"] == ^validation_id,
                 select: %{response: event.response}
               )
             )

    assert response["settlement"] == "historical_result"
    assert {:ok, []} = Goals.recover_pending_terminal_settlements(goal_ids: [goal.id], now: @now)
  end

  test "a competing accepted Subject settles a first terminal validation as historical result" do
    {goal, item, _producer, producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} =
      independent_validation_attempt_fixture(check_and_operator_acceptance_contract())

    assert {:ok, %{evidence: _stored}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence_attrs(validation_run.id, item, candidate_subject),
               now: @now
             )

    accepted_subject = Map.put(candidate_subject, "commit", String.duplicate("a", 40))

    insert_accepted_work_outcome!(goal, item.id, producer_run, accepted_subject)

    assert {:ok, %{"settlement" => "historical_result", "next_wake_at" => nil}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert [%{disposition: "rejected", reason: "historical_result"}] =
             Repo.all(
               from(outcome in SymmetryControl.Goals.WorkOutcome,
                 where: outcome.validation_task_id == ^validation.id,
                 select: %{disposition: outcome.disposition, reason: outcome.reason}
               )
             )
  end

  test "failed validation evidence remains rejected even with scoped operator acceptance" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} =
      independent_validation_attempt_fixture(check_and_operator_acceptance_contract())

    failed_evidence =
      evidence_attrs(validation_run.id, item, candidate_subject)
      |> Map.put(:verdict, "failed")
      |> put_in([:payload, :exit_code], 1)

    assert {:ok, %{evidence: _failed}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               failed_evidence,
               now: @now
             )

    assert {:ok, %{"settlement" => "validation_failed"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert {:ok, scoped_review, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "review",
               work_item_id: item.id,
               subject_hash: evidence_subject_hash(candidate_subject),
               question: "Accept failed work?",
               options: [
                 %{
                   "id" => "accept",
                   "label" => "Accept",
                   "consequence" => "Must not override failure"
                 }
               ]
             })

    scoped_review_id = scoped_review.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(goal.id, "resolve_decision", %{
               decision_id: scoped_review_id,
               expected_decision_version:
                 Repo.get!(SymmetryControl.Goals.GoalDecision, scoped_review_id).lock_version,
               option_id: "accept"
             })

    assert Repo.aggregate(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where:
                 outcome.validation_task_id == ^validation.id and
                   outcome.disposition == "rejected"
             ),
             :count
           ) == 1

    refute Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where:
                 outcome.validation_task_id == ^validation.id and
                   outcome.disposition == "accepted"
             )
           )
  end

  test "unknown and not-applicable validation evidence remain awaiting validation" do
    for verdict <- ["unknown", "not_applicable"] do
      {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
       validation_fence, candidate_subject} = independent_validation_attempt_fixture()

      evidence =
        evidence_attrs(validation_run.id, item, candidate_subject)
        |> Map.put(:evidence_id, Ecto.UUID.generate())
        |> Map.put(:evidence_key, "check:#{verdict}")
        |> Map.put(:verdict, verdict)

      assert {:ok, %{evidence: _stored}, :created} =
               Goals.append_evidence(
                 validation_runtime.machine_id,
                 validation_run.id,
                 validation_fence,
                 evidence,
                 now: @now
               )

      assert {:ok, %{"settlement" => "awaiting_validation"}} =
               Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

      refute Repo.exists?(
               from(outcome in SymmetryControl.Goals.WorkOutcome,
                 where:
                   outcome.goal_id == ^goal.id and outcome.validation_task_id == ^validation.id
               )
             )
    end
  end

  test "failed validation evidence does not turn a self-declared failure into a rejected outcome" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    failed_evidence =
      evidence_attrs(validation_run.id, item, candidate_subject)
      |> Map.put(:evidence_id, Ecto.UUID.generate())
      |> Map.put(:evidence_key, "check:self-declared-failure")
      |> Map.put(:verdict, "failed")
      |> put_in([:payload, :exit_code], 1)

    assert {:ok, %{evidence: _failed}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               failed_evidence,
               now: @now
             )

    put_task_result!(
      validation_run,
      task_result(validation, "failed", %{"reason" => "network"})
    )

    assert {:ok, %{"settlement" => "failed", "next_wake_at" => nil}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    refute Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.goal_id == ^goal.id and outcome.disposition == "rejected"
             )
           )
  end

  test "failed validation evidence cannot reject a candidate that differs from the frozen producer Subject" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    failed_evidence =
      evidence_attrs(validation_run.id, item, candidate_subject)
      |> Map.put(:evidence_id, Ecto.UUID.generate())
      |> Map.put(:evidence_key, "check:mismatched-candidate")
      |> Map.put(:verdict, "failed")
      |> put_in([:payload, :exit_code], 1)

    assert {:ok, %{evidence: _failed}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               failed_evidence,
               now: @now
             )

    put_task_result!(
      validation_run,
      task_result(validation, "candidate_completion", %{"subject" => evidence_subject(item)})
    )

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    refute Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.goal_id == ^goal.id and outcome.disposition == "rejected"
             )
           )
  end

  test "paused current revision records trusted validation rejection without waking execution" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    failed_evidence =
      evidence_attrs(validation_run.id, item, candidate_subject)
      |> Map.put(:evidence_id, Ecto.UUID.generate())
      |> Map.put(:evidence_key, "check:paused-failure")
      |> Map.put(:verdict, "failed")
      |> put_in([:payload, :exit_code], 1)

    assert {:ok, %{evidence: _failed}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               failed_evidence,
               now: @now
             )

    assert {:ok, _paused, :created} =
             command_current(goal.id, "pause", %{reason: "Pause before validation settlement."})

    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, %{"settlement" => "historical_result", "next_wake_at" => nil}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert Repo.aggregate(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.goal_id == ^goal.id and outcome.disposition == "rejected"
             ),
             :count
           ) == 1

    assert wakeup_job_count(goal.id) == 0
  end

  test "paused current revision records accepted validation without waking execution" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    evidence = evidence_attrs(validation_run.id, item, candidate_subject)

    assert {:ok, %{evidence: _stored}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence,
               now: @now
             )

    assert {:ok, _paused, :created} =
             command_current(goal.id, "pause", %{reason: "Pause before validation settlement."})

    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, %{"settlement" => "accepted", "next_wake_at" => nil}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where:
                 outcome.goal_id == ^goal.id and outcome.validation_task_id == ^validation.id and
                   outcome.disposition == "accepted"
             )
           )

    assert wakeup_job_count(goal.id) == 0
  end

  test "superseded validation failure remains historical and cannot reject the new revision" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    failed_evidence =
      evidence_attrs(validation_run.id, item, candidate_subject)
      |> Map.put(:evidence_id, Ecto.UUID.generate())
      |> Map.put(:evidence_key, "check:superseded-failure")
      |> Map.put(:verdict, "failed")
      |> put_in([:payload, :exit_code], 1)

    assert {:ok, %{evidence: _failed}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               failed_evidence,
               now: @now
             )

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract: amended_revision_contract("A new approved revision."),
               reason: "Supersede the failed validation."
             })

    assert amended.goal.current_revision == goal.current_revision + 1

    assert {:ok, %{"settlement" => "historical_result"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    refute Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.goal_id == ^goal.id and outcome.disposition == "rejected"
             )
           )
  end

  test "admission freezes validation bindings and evidence ignores later registry changes" do
    {_goal, item, task, runtime, run, fence} = validation_goal_run_fixture()
    snapshot = Repo.get!(SymmetryControl.Goals.ContextSnapshot, task.context_snapshot_id)

    assert snapshot.payload["work_contract"]["validation_bindings"] == [
             %{
               "profile_name" => "test",
               "kind" => "check",
               "profile_digest" => @validation_profile_digest,
               "allowed_runtime_ids" => [@validation_runtime_id]
             }
           ]

    changed_profiles = [
      test: [
        kind: :check,
        profile_digest: "sha256:" <> String.duplicate("f", 64),
        enabled: false,
        allowed_runtime_ids: [Ecto.UUID.generate()]
      ]
    ]

    evidence = evidence_attrs(run.id, item)
    previous_goals = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(previous_goals, :validation_profiles, changed_profiles)
    )

    try do
      assert {:ok, %{evidence: stored}, :created} =
               Goals.append_evidence(runtime.machine_id, run.id, fence, evidence, now: @now)

      assert stored.id == evidence.evidence_id
    after
      Application.put_env(:symmetry_control, :goals, previous_goals)
    end

    assert Repo.get!(SymmetryControl.Goals.ContextSnapshot, snapshot.id).payload ==
             snapshot.payload
  end

  test "check evidence rejects profile spoofing, digest mismatch, and unbound runtimes" do
    {_goal, item, _task, runtime, run, fence} = validation_goal_run_fixture()
    evidence = evidence_attrs(run.id, item)

    assert {:error, :invalid_evidence_identity} =
             Goals.append_evidence(
               runtime.machine_id,
               run.id,
               fence,
               evidence
               |> Map.put(:validator_profile, "spoofed")
               |> put_in([:source_ref, :validator_profile], "spoofed"),
               now: @now
             )

    assert {:error, :invalid_evidence_identity} =
             Goals.append_evidence(
               runtime.machine_id,
               run.id,
               fence,
               evidence
               |> Map.put(:evidence_id, Ecto.UUID.generate())
               |> Map.put(:evidence_key, "check:wrong-digest")
               |> put_in([:payload, :profile_digest], "sha256:" <> String.duplicate("f", 64)),
               now: @now
             )

    outsider_runtime_id = Ecto.UUID.generate()

    {_other_goal, other_item, _other_task, outsider, outsider_run, outsider_fence} =
      validation_goal_run_fixture(check_contract(), outsider_runtime_id)

    assert {:error, :invalid_evidence_identity} =
             Goals.append_evidence(
               outsider.machine_id,
               outsider_run.id,
               outsider_fence,
               evidence_attrs(outsider_run.id, other_item),
               now: @now
             )
  end

  test "review evidence requires the snapshot-bound profile digest and review task identity" do
    {_goal, item, task, runtime, run, fence} = validation_goal_run_fixture(review_contract())
    evidence = review_evidence_attrs(run.id, item, task.id)

    assert {:ok, %{evidence: stored}, :created} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, evidence, now: @now)

    assert stored.id == evidence.evidence_id

    assert {:error, :invalid_evidence_identity} =
             Goals.append_evidence(
               runtime.machine_id,
               run.id,
               fence,
               evidence
               |> Map.put(:evidence_id, Ecto.UUID.generate())
               |> Map.put(:evidence_key, "review:wrong-digest")
               |> Map.put(:source_revision, "sha256:" <> String.duplicate("f", 64)),
               now: @now
             )
  end

  test "historical evidence is immutable by key and accepts only the owning machine fence" do
    {_goal, item, _task, runtime, run, fence} = claimed_goal_run_fixture()
    evidence = observation_evidence_attrs(run.id, item)

    assert {:ok, %{evidence: stored}, :created} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, evidence, now: @now)

    assert stored.evidence_key == "observation:remote"
    assert stored.subject_hash == evidence.subject_hash

    Repo.update_all(from(row in Task, where: row.id == ^run.task_id),
      set: [current_generation: 2, attempt_generation: 2]
    )

    assert {:ok, %{evidence: ^stored}, :replayed} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, evidence, now: @now)

    assert {:error, :idempotency_conflict} =
             Goals.append_evidence(
               runtime.machine_id,
               run.id,
               fence,
               %{evidence | verdict: "failed"},
               now: @now
             )

    assert {:error, :ownership_lost} =
             Goals.append_evidence(Ecto.UUID.generate(), run.id, fence, evidence, now: @now)

    for key <- [:raw_transcript, :api_key, :private_key, :session_filename, :local_handle] do
      assert {:error, :invalid_request} =
               Goals.append_evidence(
                 runtime.machine_id,
                 run.id,
                 fence,
                 %{evidence | payload: Map.put(evidence.payload, key, "private")},
                 now: @now
               )
    end

    assert {:error, :invalid_evidence_identity} =
             Goals.append_evidence(
               runtime.machine_id,
               run.id,
               fence,
               evidence_attrs(run.id, item),
               now: @now
             )
  end

  test "a fenced historical observation can arrive after an amendment but cannot restore current authority" do
    {goal, item, _task, runtime, run, fence} = claimed_goal_run_fixture()
    evidence = observation_evidence_attrs(run.id, item)

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract: amended_revision_contract("Preserve historical evidence."),
               reason: "The original execution is now historical."
             })

    assert amended.goal.current_revision == 2

    assert {:ok, %{evidence: stored}, :created} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, evidence, now: @now)

    assert {:ok, %{evidence: ^stored}, :replayed} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, evidence, now: @now)

    assert {:error, :ownership_lost} = Goals.fetch_run_context(runtime.machine_id, run.id, fence)
    assert Repo.aggregate(SymmetryControl.Goals.WorkOutcome, :count) == 0
  end

  test "late usage remains fenced to the original machine and replays by exact usage key" do
    {goal, _item, task, runtime, run, fence} = claimed_goal_run_fixture()

    Repo.update_all(from(row in Task, where: row.id == ^task.id),
      set: [state: "completed", current_generation: 2, attempt_generation: 2]
    )

    Repo.update_all(from(row in Run, where: row.id == ^run.id), set: [state: "completed"])
    usage = usage_attrs(run.id)
    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, %{usage: stored}, :created} =
             Goals.record_usage(runtime.machine_id, run.id, fence, usage, now: @now)

    assert stored.cost_microusd == "42"
    assert Repo.get!(SymmetryControl.Goals.RunUsage, usage.usage_id).inserted_at == @now

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "settled"

    assert wakeup_job_count(goal.id) == 1
    after_created = Repo.get!(SymmetryControl.Goals.Goal, goal.id)
    created_next_wake_at = after_created.next_wake_at
    created_lock_version = after_created.lock_version

    assert {:ok, %{usage: ^stored}, :replayed} =
             Goals.record_usage(runtime.machine_id, run.id, fence, usage, now: @now)

    assert wakeup_job_count(goal.id) == 1

    assert %{next_wake_at: ^created_next_wake_at, lock_version: ^created_lock_version} =
             Repo.get!(SymmetryControl.Goals.Goal, goal.id)

    assert {:error, :idempotency_conflict} =
             Goals.record_usage(
               runtime.machine_id,
               run.id,
               fence,
               %{usage | observed_at: DateTime.to_iso8601(DateTime.add(@now, 1, :second))},
               now: @now
             )

    assert {:error, :idempotency_conflict} =
             Goals.record_usage(runtime.machine_id, run.id, fence, %{usage | cost_microusd: "43"},
               now: @now
             )

    assert {:error, :ownership_lost} =
             Goals.record_usage(Ecto.UUID.generate(), run.id, fence, usage, now: @now)

    assert {:error, :invalid_request} =
             Goals.record_usage(
               runtime.machine_id,
               run.id,
               fence,
               %{usage | usage_key: "too-large", cost_microusd: "9223372036854775808"},
               now: @now
             )

    assert {:ok, goal_projection} = Goals.fetch_goal(goal.id)
    assert goal_projection.accounting.usage_records == 1
  end

  test "usage corrections settle a terminal Task only when every effective leaf is known" do
    {goal, _item, task, runtime, run, fence} = claimed_goal_run_fixture()

    Repo.update_all(from(row in Task, where: row.id == ^task.id),
      set: [state: "completed", updated_at: @now]
    )

    Repo.update_all(from(row in Run, where: row.id == ^run.id),
      set: [state: "completed", updated_at: @now]
    )

    unknown_usage = %{usage_attrs(run.id) | cost_microusd: nil, cost_basis: "unknown"}

    assert {:ok, %{usage: _}, :created} =
             Goals.record_usage(runtime.machine_id, run.id, fence, unknown_usage, now: @now)

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "unknown"

    delete_goal_wakeup_jobs(goal.id)

    correction = %{
      usage_attrs(run.id)
      | usage_key: "turn-1-corrected",
        supersedes_id: unknown_usage.usage_id
    }

    assert {:ok, %{usage: _}, :created} =
             Goals.record_usage(runtime.machine_id, run.id, fence, correction, now: @now)

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "settled"

    assert wakeup_job_count(goal.id) == 1
    after_correction = Repo.get!(SymmetryControl.Goals.Goal, goal.id)
    correction_next_wake_at = after_correction.next_wake_at
    correction_lock_version = after_correction.lock_version

    assert {:ok, %{usage: _}, :replayed} =
             Goals.record_usage(runtime.machine_id, run.id, fence, correction, now: @now)

    assert wakeup_job_count(goal.id) == 1

    assert %{next_wake_at: ^correction_next_wake_at, lock_version: ^correction_lock_version} =
             Repo.get!(SymmetryControl.Goals.Goal, goal.id)
  end

  test "new active usage wakes a Goal even when its reservation remains held" do
    {goal, _item, task, runtime, run, fence} = claimed_goal_run_fixture()
    usage = usage_attrs(run.id)
    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, %{usage: stored}, :created} =
             Goals.record_usage(runtime.machine_id, run.id, fence, usage, now: @now)

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "held"

    assert wakeup_job_count(goal.id) == 1
    after_created = Repo.get!(SymmetryControl.Goals.Goal, goal.id)
    created_next_wake_at = after_created.next_wake_at
    created_lock_version = after_created.lock_version

    assert {:ok, %{usage: ^stored}, :replayed} =
             Goals.record_usage(runtime.machine_id, run.id, fence, usage, now: @now)

    assert wakeup_job_count(goal.id) == 1

    assert %{next_wake_at: ^created_next_wake_at, lock_version: ^created_lock_version} =
             Repo.get!(SymmetryControl.Goals.Goal, goal.id)
  end

  test "known usage from an earlier Run cannot settle another terminal Run without usage" do
    {goal, item, task, runtime, first_run, first_fence} =
      claimed_goal_run_fixture(check_contract(), nil, %{
        execution_policy: %{
          "budget_mode" => "strict",
          "per_run_cost_limit_microusd" => 1,
          "hard_cost_limit_required" => true
        }
      })

    Repo.update_all(from(row in Task, where: row.id == ^task.id),
      set: [state: "completed", current_generation: 2, attempt_generation: 2, updated_at: @now]
    )

    Repo.update_all(from(row in Run, where: row.id == ^first_run.id),
      set: [state: "completed", updated_at: @now]
    )

    %Run{}
    |> Run.changeset(%{
      task_id: task.id,
      runtime_id: runtime.id,
      generation: 2,
      state: "completed",
      assigned_at: @now,
      assignment_expires_at: DateTime.add(@now, 60, :second)
    })
    |> Repo.insert!()

    assert {:ok, %{usage: _}, :created} =
             Goals.record_usage(
               runtime.machine_id,
               first_run.id,
               first_fence,
               usage_attrs(first_run.id),
               now: @now
             )

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "unknown"

    assert {:error, :budget_unknown} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{reserved_microusd: 1}),
               rollout_enabled: true
             )
  end

  test "terminal unknown accounting is recovered before a missing settlement receipt" do
    {_goal, _item, task, runtime, run, fence} = claimed_goal_run_fixture()

    Repo.update_all(from(row in Task, where: row.id == ^task.id),
      set: [state: "completed", updated_at: @now]
    )

    Repo.update_all(from(row in Run, where: row.id == ^run.id),
      set: [state: "completed", updated_at: @now]
    )

    unknown_usage = %{usage_attrs(run.id) | cost_microusd: nil, cost_basis: "unknown"}

    assert {:ok, %{usage: _}, :created} =
             Goals.record_usage(runtime.machine_id, run.id, fence, unknown_usage, now: @now)

    assert {:ok, [%{task_id: task_id, run_id: run_id, generation: 1}]} =
             Goals.recover_pending_terminal_settlements(now: DateTime.add(@now, 8 * 60, :second))

    assert task_id == task.id
    assert run_id == run.id
  end

  test "usage cannot supersede itself or make a held reservation disappear" do
    {_goal, _item, task, runtime, run, fence} = claimed_goal_run_fixture()
    usage = usage_attrs(run.id)

    assert {:error, :invalid_request} =
             Goals.record_usage(
               runtime.machine_id,
               run.id,
               fence,
               %{usage | supersedes_id: usage.usage_id},
               now: @now
             )

    refute Repo.exists?(from(row in SymmetryControl.Goals.RunUsage, where: row.run_id == ^run.id))

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "held"
  end

  test "artifact and observation evidence bind canonical receipt identity" do
    {_goal, item, _task, runtime, run, fence} = claimed_goal_run_fixture()

    artifact = artifact_evidence_attrs(run.id, item)

    assert {:error, :invalid_evidence_identity} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, artifact, now: @now)

    assert {:error, {:evidence_identity_mismatch, :artifact_path}} =
             Goals.append_evidence(
               runtime.machine_id,
               run.id,
               fence,
               put_in(artifact, [:source_ref, :path], "wrong-proof.txt"),
               now: @now
             )

    observation = observation_evidence_attrs(run.id, item)

    assert {:ok, %{evidence: %{kind: "observation"}}, :created} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, observation, now: @now)
  end

  test "legacy evidence replays before predicate validation and batch strictness" do
    {_goal, item, _task, runtime, run, fence} = claimed_goal_run_fixture()
    legacy_path = String.duplicate("e\u0301", 513)

    legacy_artifact =
      artifact_evidence_attrs(run.id, item)
      |> put_in([:source_ref, :path], legacy_path)
      |> put_in([:payload, :path], legacy_path)

    persist_run_evidence!(legacy_artifact)

    evidence_before = Repo.aggregate(RunEvidence, :count)

    assert {:ok, %{evidence: %{evidence_key: "artifact:proof"}}, :replayed} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, legacy_artifact, now: @now)

    assert Repo.aggregate(RunEvidence, :count) == evidence_before

    changed_subject =
      Map.put(legacy_artifact.subject, "commit", String.duplicate("b", 40))

    assert {:error, :idempotency_conflict} =
             Goals.append_evidence(
               runtime.machine_id,
               run.id,
               fence,
               %{legacy_artifact | subject: changed_subject},
               now: @now
             )

    batch = %{
      schema_version: "symmetry.evidence_batch.v1",
      run_id: run.id,
      items: [legacy_artifact]
    }

    assert {:ok, %{evidence_batch: %{receipts: [%{disposition: "replayed"}]}}, :replayed} =
             Goals.append_evidence_batch(runtime.machine_id, run.id, fence, batch, now: @now)

    new_invalid =
      legacy_artifact
      |> Map.put(:evidence_id, Ecto.UUID.generate())
      |> Map.put(:evidence_key, "artifact:new-invalid")

    assert {:error, {:invalid_contract, _}} =
             Goals.append_evidence_batch(
               runtime.machine_id,
               run.id,
               fence,
               %{batch | items: [legacy_artifact, new_invalid]},
               now: @now
             )

    assert Repo.aggregate(RunEvidence, :count) == evidence_before
  end

  test "evidence batch treats a cross-run primary-key collision as an indexed conflict" do
    {_goal_a, item_a, _task_a, runtime_a, run_a, fence_a} = claimed_goal_run_fixture()
    {_goal_b, item_b, _task_b, runtime_b, run_b, fence_b} = claimed_goal_run_fixture()

    evidence_a = observation_evidence_attrs(run_a.id, item_a)

    assert {:ok, _receipt, :created} =
             Goals.append_evidence(runtime_a.machine_id, run_a.id, fence_a, evidence_a, now: @now)

    evidence_b =
      observation_evidence_attrs(run_b.id, item_b)
      |> Map.put(:evidence_id, evidence_a.evidence_id)
      |> Map.put(:evidence_key, "observation:cross-run-primary-key")
      |> put_in([:source_ref, :external_ref], "status:cross-run")
      |> put_in([:payload, :external_ref], "status:cross-run")

    batch = %{
      schema_version: "symmetry.evidence_batch.v1",
      run_id: run_b.id,
      items: [evidence_b]
    }

    assert {:error,
            {:idempotency_conflict,
             %{
               details: %{items: [%{index: 0, evidence_key: "observation:cross-run-primary-key"}]}
             }}} =
             Goals.append_evidence_batch(
               runtime_b.machine_id,
               run_b.id,
               fence_b,
               batch,
               now: @now
             )

    refute Repo.exists?(from row in RunEvidence, where: row.run_id == ^run_b.id)
  end

  test "deterministic validation fetches claimed context without a retained harness session" do
    {_goal, _item, _task, runtime, run, fence} = validation_goal_run_fixture()

    assert {:ok, context} = Goals.fetch_run_context(runtime.machine_id, run.id, fence)
    assert context.run_id == run.id
    assert context.session_id == nil
    assert context.context["schema_version"] == "symmetry.context_snapshot.v1"
  end

  defp validation_goal_run_fixture(
         acceptance_contract \\ check_contract(),
         runtime_id \\ @validation_runtime_id
       ) do
    {goal, item, producer} = admitted_task_fixture("primary", acceptance_contract)
    producer_runtime = runtime_fixture()
    {producer_run, _producer_fence} = completed_goal_run_fixture(producer, producer_runtime)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(producer.id, producer_run.id, producer_run.generation, now: @now)

    assert {:ok, admission, :created} =
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

    task = Repo.get!(Task, admission.response["task"]["id"])
    runtime = runtime_fixture(runtime_id)
    {run, fence} = running_goal_run_fixture(task, runtime)

    {goal, item, task, runtime, run, fence}
  end

  defp review_evidence_attrs(run_id, item, review_task_id) do
    subject = evidence_subject(item)
    subject_hash = evidence_subject_hash(subject)

    %{
      schema_version: "symmetry.evidence.v1",
      evidence_id: Ecto.UUID.generate(),
      run_id: run_id,
      evidence_key: "review:independent",
      kind: "review",
      subject: subject,
      subject_hash: subject_hash,
      source_ref: %{
        kind: "review",
        ref: "independent-review",
        review_task_id: review_task_id,
        subject_hash: subject_hash
      },
      source_revision: @validation_profile_digest,
      validator_profile: "test-review",
      verdict: "passed",
      payload: %{
        predicate_id: "review",
        subject: subject,
        subject_hash: subject_hash,
        review_task_id: review_task_id,
        findings: [],
        verdict: "passed"
      },
      observed_at: DateTime.to_iso8601(@now)
    }
  end

  defp usage_attrs(run_id) do
    %{
      schema_version: "symmetry.usage.v1",
      usage_id: Ecto.UUID.generate(),
      run_id: run_id,
      usage_key: "turn-1",
      provider: "codex",
      model: "codex-test",
      input_tokens: 10,
      output_tokens: 20,
      cached_input_tokens: 0,
      cost_microusd: "42",
      cost_basis: "reported",
      price_version: nil,
      supersedes_id: nil,
      observed_at: DateTime.to_iso8601(@now)
    }
  end

  defp artifact_evidence_attrs(run_id, item) do
    subject = evidence_subject(item)
    subject_hash = evidence_subject_hash(subject)

    %{
      schema_version: "symmetry.evidence.v1",
      evidence_id: Ecto.UUID.generate(),
      run_id: run_id,
      evidence_key: "artifact:proof",
      kind: "artifact",
      subject: subject,
      subject_hash: subject_hash,
      source_ref: %{
        kind: "artifact",
        ref: "proof-file",
        resource_id: item_repository_resource_id(item),
        commit: subject["commit"],
        path: "proof.txt",
        subject_hash: subject_hash
      },
      source_revision: "artifact-v1",
      validator_profile: "artifact",
      verdict: "passed",
      payload: %{
        predicate_id: "artifact",
        subject: subject,
        subject_hash: subject_hash,
        resource_id: item_repository_resource_id(item),
        commit: subject["commit"],
        path: "proof.txt",
        content_digest: "sha256:" <> String.duplicate("c", 64)
      },
      observed_at: DateTime.to_iso8601(@now)
    }
  end

  defp observation_evidence_attrs(run_id, item) do
    subject = evidence_subject(item)
    subject_hash = evidence_subject_hash(subject)

    %{
      schema_version: "symmetry.evidence.v1",
      evidence_id: Ecto.UUID.generate(),
      run_id: run_id,
      evidence_key: "observation:remote",
      kind: "observation",
      subject: subject,
      subject_hash: subject_hash,
      source_ref: %{
        kind: "observation",
        ref: "remote-status",
        external_ref: "status:123",
        subject_hash: subject_hash
      },
      source_revision: "observation-v1",
      validator_profile: nil,
      verdict: "passed",
      payload: %{
        predicate_id: "observation",
        subject: subject,
        subject_hash: subject_hash,
        external_ref: "status:123",
        observed_at: DateTime.to_iso8601(@now),
        note: "The external status is available."
      },
      observed_at: DateTime.to_iso8601(@now)
    }
  end

  defp persist_run_evidence!(evidence) do
    %RunEvidence{id: evidence.evidence_id}
    |> RunEvidence.changeset(%{
      run_id: evidence.run_id,
      evidence_key: evidence.evidence_key,
      kind: evidence.kind,
      subject_hash: SymmetryControl.RequestHash.canonical(evidence.subject),
      source_ref: evidence.source_ref,
      source_revision: evidence.source_revision,
      validator_profile: evidence.validator_profile,
      verdict: evidence.verdict,
      payload: evidence.payload
    })
    |> Ecto.Changeset.put_change(:inserted_at, @now)
    |> Repo.insert!()
  end

  defp review_contract do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "An independent review must pass.",
      "predicates" => [
        %{"id" => "review", "kind" => "review", "reviewer_profile" => "test-review"}
      ]
    }
  end
end
