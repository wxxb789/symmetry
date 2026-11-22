Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.ReconciliationInterfaceTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.Reconciliation
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces.WorkItem

  @now ~U[2026-09-09 00:00:00.000000Z]

  setup :put_goal_validation_profiles

  test "reconcile_due_goals/1 reconciles only the requested goal ids" do
    {first, _item, _task} = admitted_task_fixture()
    {_second, _second_item, _second_task} = admitted_task_fixture()
    set_goal_due!(first.id)

    assert Reconciliation.reconcile_due_goals(goal_ids: [first.id], now: @now) ==
             {:ok, [%{goal_id: first.id, revision: 1, disposition: :noop}]}

    assert {:error, :invalid_request} =
             Reconciliation.reconcile_due_goals(goal_ids: ["not-a-uuid"], now: @now)
  end

  test "accepted_work_item?/2 recognizes only current-revision accepted outcomes" do
    {goal, item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    insert_accepted_work_outcome!(
      goal,
      item.id,
      run,
      candidate_subject(item_repository_resource_id(item))
    )

    goal = Repo.get!(Goal, goal.id)

    assert Reconciliation.accepted_work_item?(goal, item.id) == true
    assert Reconciliation.accepted_work_item?(goal, Ecto.UUID.generate()) == false
  end

  test "automatic_admission_mutation_id/4 freezes goal, revision, item, purpose and source identity" do
    goal = %Goal{id: Ecto.UUID.generate(), current_revision: 3}
    item = %WorkItem{id: Ecto.UUID.generate()}
    source_identity = "progress:#{Ecto.UUID.generate()}"

    assert Reconciliation.automatic_admission_mutation_id(goal, item, "validate", source_identity) ==
             frozen_mutation_id("automatic-admission", [
               goal.id,
               "3",
               item.id,
               "validate",
               source_identity
             ])
  end

  test "automatic_baseline_subject/2 returns the admitted baseline subject and its frozen identity" do
    {goal, item, _task} = admitted_task_fixture()
    item = Repo.get!(WorkItem, item.id)

    expected = %{
      "resource_id" => item.repository_resource_id,
      "commit" => String.duplicate("a", 40),
      "tree_digest" => "sha256:" <> String.duplicate("b", 64)
    }

    assert {:ok, ^expected, identity} = Reconciliation.automatic_baseline_subject(goal, item)

    assert identity ==
             "baseline:subject:" <>
               Base.encode16(RequestHash.canonical(expected), case: :lower)

    assert Reconciliation.automatic_baseline_subject(
             goal,
             %{item | baseline_subject: nil, baseline_dependency_id: nil}
           ) == :none
  end

  test "automatic_progress_subject/4 binds the settled progress run identity" do
    {goal, item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)
    put_task_result!(run, task_result(task, "progress"))

    assert {:ok, %{"settlement" => "progress"}} =
             Goals.settle_task(task.id, run.id, 1, now: @now)

    goal = Repo.get!(Goal, goal.id)
    item = Repo.get!(WorkItem, item.id)
    source_task = Repo.get!(Task, task.id)
    progress_run = Repo.get!(Run, run.id)

    assert {:ok, subject, identity} =
             Reconciliation.automatic_progress_subject(goal, item, source_task, now: @now)

    assert subject == Map.fetch!(source_task.input, "subject")

    assert identity ==
             "progress:" <>
               source_task.id <>
               ":" <>
               progress_run.id <>
               ":1:" <>
               progress_run.result["task_result"]["result_id"]
  end
end
