Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.AdmissionInterfaceTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.Admission
  alias SymmetryControl.Goals.ContextSnapshot
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.GoalBudgetReservation
  alias SymmetryControl.Goals.RunEvidence
  alias SymmetryControl.Goals.WorkDependency
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces.WorkItem

  @now ~U[2026-09-09 00:00:00.000000Z]

  setup :put_goal_validation_profiles

  defp task_admission_payload(item, admission_key) do
    %{
      work_item_id: item.id,
      purpose: "implement",
      model_profile: "codex",
      session_mode: "fresh",
      requested_session_id: nil,
      validation_of_task_id: nil,
      admission_key: admission_key,
      subject: candidate_subject(item_repository_resource_id(item))
    }
  end

  test "admit_plan_items!/3 pins the admitted revision, membership and baseline" do
    project = project_fixture()
    repository = repository_fixture(project)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

    goal = Repo.get!(Goal, created.goal.id)

    proposal =
      plan_proposal(goal.id, [
        %{
          key: "implement",
          title: "Implement the bounded change",
          description: "Use the approved repository only.",
          required: true,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: baseline_subject(repository.id)
        }
      ])

    assert {:ok, [%WorkItem{} = planned]} =
             Repo.transaction(fn ->
               Admission.admit_plan_items!(goal, proposal.items, now: @now)
             end)

    assert planned.goal_id == goal.id
    assert planned.admitted_revision == 1
    assert planned.required == true
    assert planned.integration == true
    assert planned.baseline_subject == baseline_subject(repository.id).subject
  end

  test "admit_task!/6 records the queued task, snapshot, reservation and item binding" do
    {goal, [item]} = goal_with_items_fixture(%{})
    admission_key = Ecto.UUID.generate()
    subject = candidate_subject(item_repository_resource_id(item))

    payload = task_admission_payload(item, admission_key)

    # admit_task!/6 is private; its only public admission wrapper runs it.
    assert {:ok, task} =
             Repo.transaction(fn ->
               Admission.admit_authorized_task!(goal, item, "implement", "codex", payload,
                 now: @now
               )
             end)

    assert task.state == "queued"
    assert task.purpose == "implement"
    assert task.goal_id == goal.id
    assert task.goal_revision == 1
    assert task.work_item_id == item.id
    assert task.admission_key == admission_key
    assert task.input["subject"] == subject
    assert task.input["work_item_id"] == item.id

    assert task.input["limits"] == %{
             "max_turns" => 1,
             "deadline_at" => "2026-09-09T00:15:00.000000Z",
             "max_cost_microusd" => nil
           }

    assert Repo.get!(WorkItem, item.id).orchestration_task_id == task.id

    snapshot = Repo.get!(ContextSnapshot, task.context_snapshot_id)
    assert snapshot.goal_id == goal.id
    assert snapshot.goal_revision == 1
    assert snapshot.work_item_id == item.id

    reservation =
      Repo.one!(from(row in GoalBudgetReservation, where: row.task_id == ^task.id))

    assert reservation.state == "held"
    assert reservation.admission_key == admission_key
    assert reservation.reserved_microusd == nil
  end

  test "admit_plan_task!/7 creates the runless planning task with its own snapshot and reservation" do
    project = project_fixture()
    repository = repository_fixture(project)

    assert {:ok, created, :created} =
             Goals.create_goal(
               project.id,
               goal_attrs(nil, %{"max_task_admissions" => 2}),
               "operator:test",
               now: @now
             )

    goal = Repo.get!(Goal, created.goal.id)

    subject = baseline_subject(repository.id).subject
    admission_key = Ecto.UUID.generate()

    assert {:ok, task} =
             Repo.transaction(fn ->
               Admission.admit_plan_task!(
                 goal,
                 repository,
                 "codex",
                 subject,
                 %{session_mode: "fresh", requested_session_id: nil},
                 admission_key,
                 now: @now
               )
             end)

    assert task.purpose == "plan"
    assert task.work_item_id == nil
    assert task.state == "queued"
    assert task.admission_key == admission_key
    assert task.input["purpose"] == "plan"

    snapshot = Repo.get!(ContextSnapshot, task.context_snapshot_id)
    assert snapshot.work_item_id == nil
    assert snapshot.payload["work_item_id"] == nil

    assert Repo.aggregate(WorkItem, :count) == 0

    assert Repo.one!(
             from(reservation in GoalBudgetReservation, where: reservation.task_id == ^task.id)
           ).state == "held"
  end

  test "admit_authorized_task!/6 rechecks item admissibility before one admission" do
    {goal, [item]} = goal_with_items_fixture(%{})
    payload = task_admission_payload(item, Ecto.UUID.generate())

    assert {:error, :stale_revision} =
             Repo.transaction(fn ->
               Admission.admit_authorized_task!(
                 goal,
                 %{item | admitted_revision: 0},
                 "implement",
                 "codex",
                 payload,
                 now: @now
               )
             end)

    assert {:ok, task} =
             Repo.transaction(fn ->
               Admission.admit_authorized_task!(goal, item, "implement", "codex", payload,
                 now: @now
               )
             end)

    assert task.work_item_id == item.id
    assert task.state == "queued"
    assert task.goal_revision == goal.current_revision

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 1
  end

  test "ensure_admissible_item!/2 rejects an item admitted under another revision" do
    {goal, [item]} = goal_with_items_fixture(%{})

    assert {:ok, nil} =
             Repo.transaction(fn -> Admission.ensure_admissible_item!(goal, item) end)

    assert {:error, :stale_revision} =
             Repo.transaction(fn ->
               Admission.ensure_admissible_item!(goal, %{item | admitted_revision: 0})
             end)
  end

  test "ensure_unaccepted_item!/2 rejects an item with an accepted outcome" do
    {goal, item, task} = admitted_task_fixture()

    assert {:ok, nil} =
             Repo.transaction(fn -> Admission.ensure_unaccepted_item!(goal, item) end)

    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    insert_accepted_work_outcome!(
      goal,
      item.id,
      run,
      candidate_subject(item_repository_resource_id(item))
    )

    assert {:error, :already_accepted} =
             Repo.transaction(fn -> Admission.ensure_unaccepted_item!(goal, item) end)
  end

  test "ensure_achievable!/2 binds the accepted integration outcome to the frozen subject" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    {_evidence, stored_evidence} =
      append_validation_evidence_and_settle_accepted!(
        validation,
        validation_run,
        validation_runtime,
        validation_fence,
        item,
        candidate_subject
      )

    assert {:ok, :ok} =
             Repo.transaction(fn ->
               Admission.ensure_achievable!(goal, %{
                 subject: candidate_subject,
                 evidence_ids: [stored_evidence.id]
               })
             end)

    substituted_subject = Map.put(candidate_subject, "commit", String.duplicate("e", 40))

    assert {:error, :integration_outcome_required} =
             Repo.transaction(fn ->
               Admission.ensure_achievable!(goal, %{
                 subject: substituted_subject,
                 evidence_ids: [stored_evidence.id]
               })
             end)

    assert {:error, :missing_evidence} =
             Repo.transaction(fn ->
               Admission.ensure_achievable!(goal, %{subject: candidate_subject, evidence_ids: []})
             end)
  end

  test "goal_predicate_matches_evidence?/2 matches evidence rows against the predicate it belongs to" do
    {_goal, item, _producer, _producer_run, _validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    assert {:ok, %{evidence: receipt}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence_attrs(validation_run.id, item, candidate_subject),
               now: @now
             )

    evidence = Repo.get!(RunEvidence, receipt.id)

    assert Admission.goal_predicate_matches_evidence?(
             %{"id" => "check", "kind" => "check", "validator_profile" => "test"},
             evidence
           ) == true

    assert Admission.goal_predicate_matches_evidence?(
             %{"id" => "check", "kind" => "check", "validator_profile" => "test-review"},
             evidence
           ) == false

    review_evidence = %{
      evidence
      | kind: "review",
        validator_profile: "test",
        payload: Map.put(evidence.payload, "verdict", evidence.verdict)
    }

    assert Admission.goal_predicate_matches_evidence?(
             %{"id" => "review", "kind" => "review", "reviewer_profile" => "test"},
             review_evidence
           ) == true

    assert Admission.goal_predicate_matches_evidence?(
             %{"id" => "review", "kind" => "review", "reviewer_profile" => "test-review"},
             review_evidence
           ) == false

    assert Admission.goal_predicate_matches_evidence?(
             %{"id" => "review", "kind" => "review", "reviewer_profile" => "test"},
             evidence
           ) == false
  end

  test "creates_cycle?/3 detects only the reversing edge in an admitted DAG" do
    {goal, [first, second]} = goal_with_items_fixture(%{}, 2)

    %WorkDependency{}
    |> WorkDependency.changeset(%{
      goal_id: goal.id,
      work_item_id: second.id,
      depends_on_id: first.id
    })
    |> Ecto.Changeset.put_change(:inserted_at, @now)
    |> Repo.insert!()

    assert Admission.creates_cycle?(goal.id, first.id, second.id) == true
    assert Admission.creates_cycle?(goal.id, second.id, first.id) == false
  end

  test "manual_admission_payload!/6 binds the command mutation and the plan baseline subject" do
    {goal, [item]} = goal_with_items_fixture(%{})
    mutation_id = Ecto.UUID.generate()

    payload = %{
      work_item_id: item.id,
      purpose: "implement",
      model_profile: "codex",
      session_mode: "fresh",
      requested_session_id: nil,
      validation_of_task_id: nil
    }

    assert {:error, :invalid_request} =
             Repo.transaction(fn ->
               Admission.manual_admission_payload!(
                 goal,
                 item,
                 "implement",
                 Map.put(payload, :limits, %{"max_turns" => 2}),
                 mutation_id,
                 now: @now
               )
             end)

    assert {:ok, server_payload} =
             Repo.transaction(fn ->
               Admission.manual_admission_payload!(goal, item, "implement", payload, mutation_id,
                 now: @now
               )
             end)

    assert server_payload.admission_key == mutation_id
    assert server_payload.subject == item.baseline_subject
  end

  test "resource_allowed?/2 treats an empty allow-list as all resources" do
    resource_id = Ecto.UUID.generate()
    other_id = Ecto.UUID.generate()

    assert Admission.resource_allowed?(%{"allowed_resource_ids" => []}, resource_id) == true

    assert Admission.resource_allowed?(%{"allowed_resource_ids" => [resource_id]}, resource_id) ==
             true

    assert Admission.resource_allowed?(%{"allowed_resource_ids" => [other_id]}, resource_id) ==
             false
  end
end
