Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.SettlementInterfaceTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.GoalBudgetReservation
  alias SymmetryControl.Goals.GoalEvent
  alias SymmetryControl.Goals.GoalExternalWait
  alias SymmetryControl.Goals.Settlement
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces.WorkItem

  @now ~U[2026-09-09 00:00:00.000000Z]

  setup :put_goal_validation_profiles

  test "settle_task/4 settles a completed candidate Run as awaiting validation" do
    {_goal, _item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    assert {:ok, receipt} = Settlement.settle_task(task.id, run.id, 1, now: @now)
    assert receipt["settlement"] == "awaiting_validation"
    assert receipt["result_kind"] == "candidate_completion"
    assert receipt["result_id"] == run.result["task_result"]["result_id"]
    assert receipt["task_id"] == task.id
    assert receipt["run_id"] == run.id
    assert receipt["generation"] == 1
    assert receipt["next_wake_at"] == DateTime.to_iso8601(@now)

    assert {:ok, ^receipt} = Settlement.settle_task(task.id, run.id, 1, now: @now)
  end

  test "settle_unstarted_task/3 settles a committed runless cancellation once" do
    {goal, _item, task} = admitted_task_fixture()
    {:ok, current} = Goals.fetch_goal(goal.id)

    assert {:ok, cancelled, :created} =
             Goals.command(
               goal.id,
               goal_command(current, "cancel", %{reason: "Stop before a Run starts."}),
               "operator:test",
               now: @now
             )

    action_id = cancelled.response["control_action_id"]

    assert {:ok, command, :created} =
             Goals.dispatch_goal_control_command(
               goal.id,
               current.current_revision,
               action_id,
               task.id,
               "cancel",
               %{},
               "goal-control:#{action_id}:cancel:#{task.id}",
               now: @now
             )

    assert command.state == "applied"
    assert command.run_id == nil

    assert {:ok, %{"settlement" => "released_without_run"} = receipt} =
             Settlement.settle_unstarted_task(task.id, "cancelled", now: @now)

    assert receipt["task_id"] == task.id
    assert receipt["reason"] == "cancelled"

    assert {:ok, replayed} = Settlement.settle_unstarted_task(task.id, "cancelled", now: @now)
    assert Map.delete(replayed, "event_sequence") == Map.delete(receipt, "event_sequence")
    assert replayed["settlement"] == "released_without_run"

    assert Repo.aggregate(
             from(event in GoalEvent,
               where: event.goal_id == ^goal.id and event.kind == "task_unstarted_settled"
             ),
             :count
           ) == 1

    assert Repo.one!(from(row in GoalBudgetReservation, where: row.task_id == ^task.id)).state ==
             "released"

    assert {:error, :invalid_request} =
             Settlement.settle_unstarted_task(task.id, "failed", now: @now)
  end

  test "terminal_next_wake_at/4 keeps the earliest future wake" do
    hour_later = DateTime.shift(@now, hour: 1)
    goal = %Goal{state: "active", current_revision: 1, next_wake_at: hour_later}
    task = %Task{goal_revision: 1}

    assert Settlement.terminal_next_wake_at(goal, task, %{wake_at: :immediate}, now: @now) ==
             @now

    assert Settlement.terminal_next_wake_at(
             goal,
             task,
             %{wake_at: DateTime.shift(@now, hour: 2)},
             now: @now
           ) == hour_later

    stale_task = %Task{goal_revision: 0}

    terminal_wake =
      Settlement.terminal_next_wake_at(goal, stale_task, %{wake_at: :immediate}, now: @now)

    assert terminal_wake == hour_later

    assert Settlement.terminal_next_wake_at(
             %Goal{state: "active", current_revision: 1, next_wake_at: nil},
             stale_task,
             %{wake_at: :immediate},
             now: @now
           ) == nil
  end

  test "terminal_settlement!/7 reports a failed Run's semantic reason" do
    {goal, item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    Repo.update_all(
      from(row in Run, where: row.id == ^run.id),
      set: [state: "failed", failure: %{"reason" => "quota", "summary" => "Quota exhausted."}]
    )

    failed_run = Repo.get!(Run, run.id)

    assert Settlement.terminal_settlement!(goal, item, task, failed_run, nil, nil, now: @now) ==
             %{
               settlement: "failed",
               result_id: nil,
               result_kind: nil,
               reason: "quota",
               blocker: nil,
               decision_id: nil,
               proposed_next_action: nil,
               subject_hash: nil,
               evidence_refs: [],
               wake_at: nil
             }
  end

  test "settlement_task_owned_by_goal?/3 requires goal, item and task to agree" do
    {goal, item, task} = admitted_task_fixture()
    goal = Repo.get!(Goal, goal.id)
    item = Repo.get!(WorkItem, item.id)
    task = Repo.get!(Task, task.id)
    foreign = Ecto.UUID.generate()

    assert Settlement.settlement_task_owned_by_goal?(task, goal, item)
    refute Settlement.settlement_task_owned_by_goal?(task, goal, nil)
    refute Settlement.settlement_task_owned_by_goal?(task, %{goal | id: foreign}, item)
    refute Settlement.settlement_task_owned_by_goal?(task, goal, %{item | goal_id: foreign})
    refute Settlement.settlement_task_owned_by_goal?(task, goal, %{item | id: foreign})

    plan_task = %Task{
      goal_id: goal.id,
      purpose: "plan",
      work_item_id: nil,
      validation_of_task_id: nil
    }

    assert Settlement.settlement_task_owned_by_goal?(plan_task, goal, nil)
    refute Settlement.settlement_task_owned_by_goal?(plan_task, %{goal | id: foreign}, nil)
  end

  test "external_wait_identity/1 freezes the wait, task, run, result and check sequence" do
    wait = %GoalExternalWait{
      id: "wait-1",
      task_id: "task-1",
      run_id: "run-1",
      result_id: "result-1",
      check_seq: 3
    }

    assert Settlement.external_wait_identity(wait) == "external:wait-1:task-1:run-1:result-1:3"
  end

  test "external_wait_wake_at/2 schedules no wake" do
    goal = %Goal{
      state: "active",
      current_revision: 1,
      next_wake_at: DateTime.shift(@now, minute: 1)
    }

    assert Settlement.external_wait_wake_at(goal, @now) == nil
  end

  test "lock_settlement_tasks!/2 locks the validation task and its producer" do
    {_goal, _item, producer, _producer_run, validation, _validation_run, _runtime, _fence, _subject} =
      independent_validation_attempt_fixture()

    assert {:ok, {locked, producing}} =
             Repo.transaction(fn ->
               Settlement.lock_settlement_tasks!(validation.id, producer.id)
             end)

    assert locked.id == validation.id
    assert locked.validation_of_task_id == producer.id
    assert producing.id == producer.id

    assert {:error, :not_found} =
             Repo.transaction(fn ->
               Settlement.lock_settlement_tasks!(Ecto.UUID.generate(), nil)
             end)
  end

  test "lock_settlement_runs!/3 locks the run and its producing run" do
    {_goal, _item, producer, producer_run, validation, validation_run, _runtime, _, _} =
      independent_validation_attempt_fixture()

    producer = Repo.get!(Task, producer.id)

    assert {:ok, {locked_run, producing_run}} =
             Repo.transaction(fn ->
               Settlement.lock_settlement_runs!(validation_run.id, validation.id, producer)
             end)

    assert locked_run.id == validation_run.id
    assert producing_run.id == producer_run.id

    assert {:ok, {nil, nil}} =
             Repo.transaction(fn ->
               Settlement.lock_settlement_runs!(validation_run.id, producer.id, producer)
             end)
  end
end
