Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.ValidationOutcomeInterfaceTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.GoalDecision
  alias SymmetryControl.Goals.ValidationOutcome
  alias SymmetryControl.Goals.WorkOutcome
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces.WorkItem

  @now ~U[2026-09-09 00:00:00.000000Z]

  setup :put_goal_validation_profiles

  test "reconcile_validation_outcome/4 settles a resolved operator rejection as rejected" do
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

    review_goal = fetch_goal!(goal.id)

    assert {:ok, review, :created} =
             Goals.command(
               goal.id,
               goal_command(review_goal, "request_decision", %{
                 kind: "review",
                 work_item_id: item.id,
                 subject_hash: evidence_subject_hash(candidate_subject)
               }),
               "operator:test",
               now: @now
             )

    review_id = review.response["decision"]["id"]
    resolve_goal = fetch_goal!(goal.id)

    assert {:ok, _resolved, :created} =
             Goals.command(
               goal.id,
               goal_command(resolve_goal, "resolve_decision", %{
                 decision_id: review_id,
                 expected_decision_version: Repo.get!(GoalDecision, review_id).lock_version,
                 option_id: "reject"
               }),
               "operator:test",
               now: @now
             )

    assert {:ok, response} =
             ValidationOutcome.reconcile_validation_outcome(
               validation.id,
               validation_run.id,
               1,
               now: @now
             )

    assert response["settlement"] == "rejected"
    assert response["reason"] == "validation_failed"
    assert response["task_id"] == validation.id
    assert response["run_id"] == validation_run.id
    assert response["generation"] == 1
    assert response["next_wake_at"] == nil

    validation_id = validation.id

    assert [
             %{
               disposition: "rejected",
               decision_id: ^review_id,
               validation_task_id: ^validation_id
             }
           ] =
             Repo.all(
               from(outcome in WorkOutcome,
                 where: outcome.validation_task_id == ^validation.id,
                 select: %{
                   disposition: outcome.disposition,
                   decision_id: outcome.decision_id,
                   validation_task_id: outcome.validation_task_id
                 }
               )
             )

    assert {:ok, ^response} =
             ValidationOutcome.reconcile_validation_outcome(
               validation.id,
               validation_run.id,
               1,
               now: @now
             )
  end

  test "derive_validation_outcome!/10 inserts the accepted outcome bound to the candidate run" do
    {goal, item, producer, producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} =
      independent_validation_attempt_fixture()

    evidence = evidence_attrs(validation_run.id, item, candidate_subject)

    assert {:ok, %{evidence: _stored}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence,
               now: @now
             )

    goal = Repo.get!(Goal, goal.id)
    item = Repo.get!(WorkItem, item.id)
    producer = Repo.get!(Task, producer.id)
    validation = Repo.get!(Task, validation.id)
    subject_hash = RequestHash.canonical(candidate_subject)

    outcome =
      ValidationOutcome.derive_validation_outcome!(
        goal,
        item,
        validation,
        validation_run,
        producer,
        producer_run,
        "candidate_completion",
        candidate_subject,
        subject_hash,
        now: @now
      )

    assert outcome.disposition == "accepted"
    assert outcome.reason == "validation_passed"
    assert outcome.decision_id == nil
    assert outcome.evidence_ids == [evidence.evidence_id]
    assert outcome.candidate_subject == candidate_subject
    assert outcome.subject_hash == subject_hash
    assert outcome.goal_revision == goal.current_revision
    assert outcome.work_item_id == item.id
    assert outcome.producing_task_id == producer.id
    assert outcome.producing_run_id == producer_run.id
    assert outcome.producing_result_id == producer_run.result["task_result"]["result_id"]
    assert outcome.validation_task_id == validation.id

    assert ValidationOutcome.derive_validation_outcome!(
             goal,
             item,
             validation,
             validation_run,
             producer,
             producer_run,
             "progress",
             candidate_subject,
             subject_hash,
             now: @now
           ) == nil
  end

  test "reconcile_validation_outcome/4 refuses an attempt that never settled" do
    {_goal, _item, _producer, _producer_run, validation, validation_run, _validation_runtime,
     _validation_fence, _candidate_subject} = independent_validation_attempt_fixture()

    assert {:error, :validation_reconciliation_not_pending} =
             ValidationOutcome.reconcile_validation_outcome(
               validation.id,
               validation_run.id,
               1,
               now: @now
             )
  end

  test "reconcile_validation_outcome/4 refuses an attempt settled without awaiting validation" do
    {_goal, _item, _producer, _producer_run, validation, validation_run, _validation_runtime,
     _validation_fence, _candidate_subject} = independent_validation_attempt_fixture()

    put_run_task_result!(
      validation_run,
      task_result(validation, "failed", %{"reason" => "network"})
    )

    assert {:ok, %{"settlement" => "failed"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert {:error, :validation_reconciliation_not_pending} =
             ValidationOutcome.reconcile_validation_outcome(
               validation.id,
               validation_run.id,
               1,
               now: @now
             )
  end

  test "reconcile_validation_outcome/4 fails closed when the late receipt outlives its WorkOutcome" do
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

    review_goal = fetch_goal!(goal.id)

    assert {:ok, review, :created} =
             Goals.command(
               goal.id,
               goal_command(review_goal, "request_decision", %{
                 kind: "review",
                 work_item_id: item.id,
                 subject_hash: evidence_subject_hash(candidate_subject)
               }),
               "operator:test",
               now: @now
             )

    review_id = review.response["decision"]["id"]
    resolve_goal = fetch_goal!(goal.id)

    assert {:ok, _resolved, :created} =
             Goals.command(
               goal.id,
               goal_command(resolve_goal, "resolve_decision", %{
                 decision_id: review_id,
                 expected_decision_version: Repo.get!(GoalDecision, review_id).lock_version,
                 option_id: "reject"
               }),
               "operator:test",
               now: @now
             )

    assert {:ok, %{"settlement" => "rejected"}} =
             ValidationOutcome.reconcile_validation_outcome(
               validation.id,
               validation_run.id,
               1,
               now: @now
             )

    Repo.query!("ALTER TABLE work_outcomes DISABLE TRIGGER work_outcomes_immutable_history")

    try do
      Repo.delete_all(from(row in WorkOutcome, where: row.validation_task_id == ^validation.id))
    after
      Repo.query!("ALTER TABLE work_outcomes ENABLE TRIGGER work_outcomes_immutable_history")
    end

    assert {:error, :internal_error} =
             ValidationOutcome.reconcile_validation_outcome(
               validation.id,
               validation_run.id,
               1,
               now: @now
             )
  end
end
