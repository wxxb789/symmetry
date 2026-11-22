Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.GoalControlInterfaceTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals.GoalControl
  alias SymmetryControl.Orchestration.Command
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.RunTransition
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash

  @now ~U[2026-09-09 00:00:00.000000Z]

  use SymmetryControl.TestFixtures,
    command_opts: [now: @now, validation_profiles: validation_profiles()]

  setup :put_goal_validation_profiles

  test "control_dispatch_plan/4 enumerates the exact descriptor for the current cancellation" do
    {goal, _item, task} = admitted_task_fixture()
    action_id = goal_control_action!(goal.id, "cancel", "Stop the queued execution.")

    assert GoalControl.control_dispatch_plan(goal.id, 1, action_id) ==
             {:ok,
              [
                %{
                  task_id: task.id,
                  kind: "cancel",
                  payload: %{},
                  idempotency_key: "goal-control:#{action_id}:cancel:#{task.id}"
                }
              ]}

    assert GoalControl.control_dispatch_plan(goal.id, 2, action_id) == {:ok, :superseded}
  end

  test "dispatch_goal_control_command/8 creates one runless cancellation and replays it exactly" do
    {goal, _item, task} = admitted_task_fixture()
    action_id = goal_control_action!(goal.id, "cancel", "Stop the queued execution.")
    idempotency_key = "goal-control:#{action_id}:cancel:#{task.id}"

    assert {:ok, command, :created} =
             GoalControl.dispatch_goal_control_command(
               goal.id,
               1,
               action_id,
               task.id,
               "cancel",
               %{},
               idempotency_key,
               now: @now
             )

    assert command.kind == "cancel"
    assert command.state == "applied"
    assert command.run_id == nil
    assert command.idempotency_key == idempotency_key
    assert Repo.get!(Task, task.id).state == "cancelled"

    assert {:ok, replayed, :replayed} =
             GoalControl.dispatch_goal_control_command(
               goal.id,
               1,
               action_id,
               task.id,
               "cancel",
               %{},
               idempotency_key,
               now: @now
             )

    assert replayed == Repo.get!(Command, command.id)

    assert Repo.aggregate(from(row in Command, where: row.task_id == ^task.id), :count) == 1

    assert {:error, :invalid_request} =
             GoalControl.dispatch_goal_control_command(
               goal.id,
               1,
               action_id,
               task.id,
               "cancel",
               %{},
               idempotency_key <> ":forged",
               now: @now
             )
  end

  test "apply_goal_pause_transition/5 pauses the run from its pending pause command" do
    {goal, _item, task, _runtime, run, fence} = claimed_goal_run_fixture()
    action_id = goal_control_action!(goal.id, "pause", "Stop the running execution.")
    transition_id = Ecto.UUID.generate()

    command =
      insert_pending_command!(task, "pause", "goal-control:#{action_id}:pause:#{task.id}",
        run_id: run.id,
        generation: run.generation
      )

    assert {:ok, paused_run, :created, nil} =
             GoalControl.apply_goal_pause_transition(
               run.id,
               fence,
               %{"command_id" => command.id},
               transition_id,
               now: @now
             )

    assert paused_run.state == "paused"
    assert Repo.get!(Run, run.id).state == "paused"
    assert Repo.get!(Task, task.id).state == "paused"
    assert Repo.get!(Command, command.id).state == "applied"

    assert Repo.get_by!(RunTransition, run_id: run.id, transition_id: transition_id).state ==
             "paused"
  end

  test "reject_pending_goal_control_commands!/2 acknowledges only goal control commands" do
    {goal, _item, task} = admitted_task_fixture()

    rejected =
      insert_pending_command!(
        task,
        "cancel",
        "goal-control:#{Ecto.UUID.generate()}:cancel:#{task.id}"
      )

    untouched = insert_pending_command!(task, "cancel", "operator:other")

    assert GoalControl.reject_pending_goal_control_commands!(goal.id, @now) == {1, nil}

    rejected = Repo.get!(Command, rejected.id)
    assert rejected.state == "acknowledged"
    assert rejected.acknowledgement_outcome == "rejected"
    assert rejected.acknowledged_at == @now
    assert Repo.get!(Command, untouched.id).state == "pending"
  end

  defp goal_control_action!(goal_id, kind, reason) do
    assert {:ok, receipt, :created} = command_current(goal_id, kind, %{reason: reason})
    receipt.response["control_action_id"]
  end

  defp insert_pending_command!(task, kind, idempotency_key, opts \\ []) do
    {request_hash, request_hash_version} = RequestHash.write(%{kind: kind, payload: %{}})

    %Command{}
    |> Command.changeset(%{
      task_id: task.id,
      run_id: Keyword.get(opts, :run_id),
      generation: Keyword.get(opts, :generation),
      kind: kind,
      payload: %{},
      idempotency_key: idempotency_key,
      request_hash: request_hash,
      request_hash_version: request_hash_version,
      state: "pending"
    })
    |> Repo.insert!()
  end
end
