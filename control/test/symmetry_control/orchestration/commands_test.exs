defmodule SymmetryControl.Orchestration.CommandsTest do
  use SymmetryControl.OrchestrationCase, async: false

  alias SymmetryControl.Goals.{ContextSnapshot, Goal, GoalRevision}
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Command, Commands, Task}
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.WorkItem

  @now ~U[2026-09-12 09:00:00.000000Z]

  test "create_command cancels queued work and replays the same idempotency key" do
    assert {:ok, task, :created} =
             Orchestration.submit_task(task_attrs(), "create-command", now: @now)

    assert {:ok, command, :created} =
             Commands.create_command(task.id, "cancel", %{}, "queued-cancel", now: @now)

    assert command.kind == "cancel"
    assert command.state == "applied"
    assert command.applied_at == @now
    assert {:ok, %{state: "cancelled"}} = Orchestration.fetch_task(task.id)

    assert {:ok, replayed, :replayed} =
             Commands.create_command(task.id, "cancel", %{}, "queued-cancel", now: @now)

    assert replayed.id == command.id
  end

  test "create_command rejects a Goal-owned task with :goal_authority_required" do
    task = goal_owned_task("command-guard")

    assert {:error, :goal_authority_required} =
             Commands.create_command(task.id, "cancel", %{}, "goal-owned-cancel", now: @now)

    assert {:ok, %{state: "queued"}} = Orchestration.fetch_task(task.id)

    refute Repo.exists?(from command in Command, where: command.task_id == ^task.id)
  end

  test "request_cancel cancels queued work and replays the applied command" do
    assert {:ok, task, :created} =
             Orchestration.submit_task(task_attrs(), "request-cancel", now: @now)

    assert {:ok, cancelled, command} = Commands.request_cancel(task.id, now: @now)
    assert cancelled.state == "cancelled"
    assert command.kind == "cancel"
    assert command.state == "applied"
    assert command.run_id == nil

    assert {:ok, same, replayed} = Commands.request_cancel(task.id, now: @now)
    assert same.state == "cancelled"
    assert replayed.id == command.id
  end

  test "request_cancel returns a terminal task without creating a command" do
    assert {:ok, task, :created} =
             Orchestration.submit_task(task_attrs(), "terminal-cancel", now: @now)

    task
    |> Ecto.Changeset.change(state: "failed")
    |> Repo.update!()

    assert {:ok, terminal, nil} = Commands.request_cancel(task.id, now: @now)
    assert terminal.state == "failed"

    refute Repo.exists?(from command in Command, where: command.task_id == ^task.id)
  end

  test "provide_input creates a pending command for the waiting generation" do
    {task, run, _fence} = waiting_task("provide-input")

    assert {:ok, command, :created} =
             Commands.provide_input(task.id, %{"choice" => "main"}, "waiting-input", now: @now)

    assert command.kind == "provide_input"
    assert command.state == "pending"
    assert command.run_id == run.id
    assert command.generation == run.generation
  end

  test "acknowledge_command applies an acknowledgement and replays the same id" do
    {task, _run, fence} = running_task("acknowledge")

    assert {:ok, command, :created} =
             Commands.create_command(task.id, "cancel", %{}, "acknowledge-cancel", now: @now)

    acknowledgement_id = Ecto.UUID.generate()

    assert {:ok, acknowledged} =
             Commands.acknowledge_command(
               command.id,
               fence,
               "applied",
               acknowledgement_id,
               now: @now
             )

    assert acknowledged.state == "acknowledged"
    assert acknowledged.acknowledged_at == @now

    assert {:ok, replayed} =
             Commands.acknowledge_command(
               command.id,
               fence,
               "applied",
               acknowledgement_id,
               now: @now
             )

    assert replayed.id == command.id
  end

  test "retry_task requeues a failed attempt with the next generation" do
    {task, run, _fence} = failed_task("retry")

    assert {:ok, retried, command, :created} =
             Commands.retry_task(task.id, task_attrs(), "retry-command", now: @now)

    assert retried.state == "queued"
    assert retried.attempt_generation == 2
    assert retried.failure == nil
    assert command.kind == "retry"
    assert command.state == "applied"
    assert command.run_id == run.id
  end

  test "retry_task replays the same idempotency key instead of retrying twice" do
    {task, _run, _fence} = failed_task("retry-replay")

    assert {:ok, _retried, command, :created} =
             Commands.retry_task(task.id, task_attrs(), "retry-replay-command", now: @now)

    assert {:ok, replayed_task, replayed, :replayed} =
             Commands.retry_task(task.id, task_attrs(), "retry-replay-command", now: @now)

    assert replayed.id == command.id
    assert replayed_task.attempt_generation == 2
  end

  defp running_task(label) do
    {machine, _token} = enroll_machine(label)
    runtime = register_runtime(machine, @now)

    assert {:ok, task, :created} =
             Orchestration.submit_task(task_attrs(), label, now: @now)

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime, @now)

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(
               run.id,
               fence,
               "running",
               %{},
               Ecto.UUID.generate(),
               now: @now
             )

    {task, run, fence}
  end

  defp waiting_task(label) do
    {task, run, fence} = running_task(label)

    assert {:ok, %{state: "waiting_for_input"}} =
             Orchestration.transition(
               run.id,
               fence,
               "waiting_for_input",
               %{},
               Ecto.UUID.generate(),
               now: @now
             )

    {task, run, fence}
  end

  defp failed_task(label) do
    {task, run, fence} = running_task(label)

    assert {:ok, %{state: "failed"}} =
             Orchestration.transition(
               run.id,
               fence,
               "failed",
               %{"reason" => "failed"},
               Ecto.UUID.generate(),
               now: @now
             )

    {task, run, fence}
  end

  defp goal_owned_task(label) do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: "Commands #{label}",
               key: project_key()
             })

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{
               kind: "repository",
               name: "Commands repository #{label}"
             })

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Commands goal work",
               status: "ready",
               assignee_type: "agent",
               agent_profile: "codex",
               repository_resource_id: repository.id
             })

    assert {:ok, goal} =
             Repo.transaction(fn ->
               goal =
                 %Goal{}
                 |> Goal.changeset(%{
                   project_id: project.id,
                   title: "Commands goal",
                   state: "active",
                   current_revision: 1,
                   event_sequence: 0
                 })
                 |> Repo.insert!()

               %GoalRevision{}
               |> GoalRevision.changeset(%{
                 goal_id: goal.id,
                 revision: 1,
                 objective: "Keep Goal commands on the Goal authority path",
                 non_goals: [],
                 acceptance_contract: %{"checks" => ["mix test"]},
                 authority_policy: %{},
                 execution_policy: goal_policy(),
                 context_manifest: %{},
                 reason: "initial admission",
                 actor_ref: "operator:test"
               })
               |> Repo.insert!()

               goal
             end)

    assert {:ok, item} =
             item
             |> WorkItem.goal_membership_changeset(%{
               goal_id: goal.id,
               admitted_revision: 1,
               acceptance_contract: %{"checks" => ["mix test"]},
               integration: true,
               baseline_subject: %{
                 "resource_id" => repository.id,
                 "commit" => String.duplicate("a", 40),
                 "tree_digest" => "sha256:" <> String.duplicate("4", 64)
               }
             })
             |> Repo.update()

    snapshot =
      %ContextSnapshot{}
      |> ContextSnapshot.changeset(%{
        goal_id: goal.id,
        goal_revision: 1,
        work_item_id: item.id,
        schema_version: 1,
        content_hash: :crypto.hash(:sha256, "commands-goal-context"),
        payload: %{"sources" => []}
      })
      |> Repo.insert!()

    %Task{}
    |> Task.changeset(%{
      idempotency_key: "commands-goal-task-#{label}",
      request_hash: :crypto.hash(:sha256, "commands-goal-task"),
      goal_id: goal.id,
      goal_revision: 1,
      work_item_id: item.id,
      context_snapshot_id: snapshot.id,
      admission_key: Ecto.UUID.generate(),
      allowed_runtime_ids: [],
      max_run_attempts: 1,
      goal: "Commands goal work",
      agent_profile: "codex",
      workspace: "primary",
      required_capabilities: %{},
      input: %{},
      state: "queued",
      current_generation: 1,
      attempt_generation: 1,
      purpose: "implement"
    })
    |> Repo.insert!()
  end

  defp goal_policy do
    %{
      "automatic_execution" => false,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "max_run_attempts_per_task" => 1,
      "budget_limit_microusd" => nil,
      "per_run_cost_limit_microusd" => nil,
      "budget_mode" => "soft",
      "hard_cost_limit_required" => false,
      "allowed_runtime_ids" => [],
      "allowed_model_profiles" => ["codex"],
      "final_acceptance" => "operator",
      "allowed_actions" => [],
      "allowed_resource_ids" => []
    }
  end

  defp project_key do
    "C" <> Integer.to_string(rem(System.unique_integer([:positive]), 9_999_999))
  end

  defp enroll_machine(label) do
    key = Ecto.UUID.generate()
    token = "commands-#{label}-#{key}"

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(%{name: "Commands #{label}", machine_token: token}, key,
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret",
               now: @now
             )

    {enrolled.machine, token}
  end
end
