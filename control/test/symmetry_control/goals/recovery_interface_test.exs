Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.RecoveryInterfaceTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.Recovery
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo

  @now ~U[2026-09-09 00:00:00.000000Z]

  setup :put_goal_validation_profiles

  test "recover_pending_unstarted_cancellations/1 returns held cancelled tasks before settlement" do
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

    assert command.run_id == nil

    assert {:ok, [task_id]} = Recovery.recover_pending_unstarted_cancellations(goal.id)
    assert task_id == task.id

    assert {:error, :invalid_request} = Recovery.recover_pending_unstarted_cancellations("nope")
  end

  test "recoverable_unstarted_cancellation_goal_ids/1 lists goals with held runless cancellations" do
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

    assert {:ok, _command, :created} =
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

    assert {:ok, [goal_id]} =
             Recovery.recoverable_unstarted_cancellation_goal_ids(goal_ids: [goal.id])

    assert goal_id == goal.id

    assert {:ok, []} =
             Recovery.recoverable_unstarted_cancellation_goal_ids(goal_ids: [Ecto.UUID.generate()])

    assert {:error, :invalid_request} =
             Recovery.recoverable_unstarted_cancellation_goal_ids(limit: 0)
  end

  test "recover_pending_terminal_settlements/1 respects the lease-expiry grace boundary" do
    {_goal, _item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "failed"])

    Repo.update_all(
      from(row in Run, where: row.id == ^run.id),
      set: [state: "failed", failure: %{"reason" => "lease_expired"}, updated_at: @now]
    )

    assert {:ok, []} = Recovery.recover_pending_terminal_settlements(now: @now)

    assert Recovery.recover_pending_terminal_settlements(now: DateTime.shift(@now, minute: 8)) ==
             {:ok, [%{task_id: task.id, run_id: run.id, generation: 1, reconciliation: :settlement}]}
  end

  test "recover_pending_goal_control_actions/1 returns the latest pending control action" do
    {goal, _item, _task} = admitted_task_fixture()
    {:ok, current} = Goals.fetch_goal(goal.id)

    assert {:ok, cancelled, :created} =
             Goals.command(
               goal.id,
               goal_command(current, "cancel", %{reason: "This action requires recovery."}),
               "operator:test",
               now: @now
             )

    action_id = cancelled.response["control_action_id"]

    assert {:ok, [action]} =
             Recovery.recover_pending_goal_control_actions(goal_ids: [goal.id], limit: 100)

    assert action == %{goal_id: goal.id, revision: 1, action_id: action_id}

    assert {:error, :invalid_request} = Recovery.recover_pending_goal_control_actions(limit: 0)
  end

  test "control_descriptors/3 maps cancellable tasks to cancel descriptors" do
    {goal, _item, task} = admitted_task_fixture()
    goal = Repo.get!(Goal, goal.id)
    action_id = Ecto.UUID.generate()

    assert Recovery.control_descriptors(goal, "cancel", action_id) == [
             %{
               task_id: task.id,
               kind: "cancel",
               payload: %{},
               idempotency_key: "goal-control:#{action_id}:cancel:#{task.id}"
             }
           ]

    assert Recovery.control_descriptors(goal, "pause", action_id) == []
  end

  test "ensure_amendment_controls_settled!/1 rolls back while stale amendment work remains" do
    {goal, _item, task} = admitted_task_fixture()
    {:ok, current} = Goals.fetch_goal(goal.id)

    assert {:ok, _amended, :created} =
             Goals.command(
               goal.id,
               goal_command(current, "amend", %{
                 revision_contract: amended_revision_contract("Stale work must settle before resume."),
                 reason: "Scope changed."
               }),
               "operator:test",
               now: @now
             )

    amended_goal = Repo.get!(Goal, goal.id)
    assert amended_goal.current_revision == 2

    assert {:error, :state_conflict} =
             Repo.transaction(fn -> Recovery.ensure_amendment_controls_settled!(amended_goal) end)

    complete_task_and_release_reservation!(goal, task)

    assert {:ok, nil} =
             Repo.transaction(fn -> Recovery.ensure_amendment_controls_settled!(amended_goal) end)
  end

  test "recoverable_historical_unstarted_cancellation?/2 requires an applied runless cancel" do
    {goal, _item, task} = admitted_task_fixture()
    goal = Repo.get!(Goal, goal.id)
    {:ok, current} = Goals.fetch_goal(goal.id)

    assert Recovery.recoverable_historical_unstarted_cancellation?(goal, task.id) == false

    assert {:ok, cancelled, :created} =
             Goals.command(
               goal.id,
               goal_command(current, "cancel", %{reason: "Stop before a Run starts."}),
               "operator:test",
               now: @now
             )

    action_id = cancelled.response["control_action_id"]

    assert Recovery.recoverable_historical_unstarted_cancellation?(goal, task.id) == false

    assert {:ok, _command, :created} =
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

    assert Recovery.recoverable_historical_unstarted_cancellation?(goal, task.id) == true

    assert Recovery.recoverable_historical_unstarted_cancellation?(goal, Ecto.UUID.generate()) ==
             false
  end
end
