defmodule SymmetryControl.OrchestrationRetryTest do
  use SymmetryControl.DataCase, async: false

  alias SymmetryControl.Orchestration

  @now ~U[2026-09-04 09:00:00.000000Z]

  test "failed work retries idempotently on the same task with refreshed intent" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)

    assert {:ok, task, :created} =
             Orchestration.submit_task(task_attrs(), "retry-task", now: @now)

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)
    transition(run.id, fence, "running", 1)

    assert {:ok, _failed} =
             Orchestration.transition(
               run.id,
               fence,
               "failed",
               %{"stage" => "tests", "error" => "first attempt failed"},
               uuid(2),
               now: @now
             )

    replacement = %{
      goal: "Run the corrected tests",
      agent_profile: "codex",
      workspace: "primary",
      input: %{"work_item_id" => "item-1", "revision" => 2}
    }

    assert {:ok, retried, command, :created} =
             Orchestration.retry_task(task.id, replacement, "retry-action-1", now: @now)

    assert retried.id == task.id
    assert retried.state == "queued"
    assert retried.current_generation == 1
    assert retried.attempt_generation == 2
    assert retried.goal == replacement.goal
    assert retried.input == replacement.input
    assert retried.result == nil
    assert retried.failure == nil
    assert command.kind == "retry"
    assert command.state == "applied"
    assert command.run_id == run.id
    assert command.generation == 1

    assert {:ok, replayed_task, replayed_command, :replayed} =
             Orchestration.retry_task(task.id, replacement, "retry-action-1", now: @now)

    assert replayed_task.id == task.id
    assert replayed_command.id == command.id

    assert {:error, :idempotency_conflict} =
             Orchestration.retry_task(
               task.id,
               Map.put(replacement, :goal, "Different retry"),
               "retry-action-1",
               now: @now
             )

    assert {:ok, second_run} = Orchestration.assign_one(now: @now)
    assert second_run.task_id == task.id
    assert second_run.generation == 2

    assert {:ok, assigned} = Orchestration.fetch_task(task.id)
    assert assigned.current_generation == 2
    assert assigned.attempt_generation == 2

    assert {:error, :state_conflict} =
             Orchestration.retry_task(task.id, replacement, "retry-action-2", now: @now)
  end

  test "cancelled queued work can retry once and completed work cannot retry" do
    assert {:ok, cancelled_task, :created} =
             Orchestration.submit_task(task_attrs(), "cancelled-retry-task", now: @now)

    assert {:ok, %{state: "cancelled"}, _command} =
             Orchestration.request_cancel(cancelled_task.id, now: @now)

    assert {:ok, queued, command, :created} =
             Orchestration.retry_task(
               cancelled_task.id,
               task_attrs(goal: "Try after cancellation"),
               "cancelled-retry-action",
               now: @now
             )

    assert queued.state == "queued"
    assert queued.current_generation == 0
    assert queued.attempt_generation == 2
    assert command.run_id == nil
    assert command.generation == nil

    assert {:ok, _command, :created} =
             Orchestration.create_command(
               queued.id,
               "cancel",
               %{},
               "cancel-second-attempt",
               expected_generation: 2,
               now: @now
             )

    assert {:ok, cancelled_again} = Orchestration.fetch_task(queued.id)
    assert cancelled_again.state == "cancelled"
    assert cancelled_again.attempt_generation == 2

    assert {:ok, third_attempt, third_retry_command, :created} =
             Orchestration.retry_task(
               queued.id,
               task_attrs(goal: "Try after queued cancellation"),
               "retry-after-queued-cancel",
               expected_generation: 2,
               now: @now
             )

    assert third_attempt.current_generation == 0
    assert third_attempt.attempt_generation == 3
    assert third_retry_command.run_id == nil
    assert third_retry_command.generation == nil

    %{machine: machine} = enroll_machine("completed-machine")
    runtime = register_runtime(machine, "completed-runtime")

    assert {:ok, completed_task, :created} =
             Orchestration.submit_task(
               task_attrs(agent_profile: "completed-profile"),
               "completed-retry-task",
               now: @now
             )

    assert {:ok, completed_run} = Orchestration.assign_one(now: @now)
    completed_fence = claim(completed_run, runtime)
    transition(completed_run.id, completed_fence, "running", 3)
    transition(completed_run.id, completed_fence, "completed", 4)

    assert {:error, :state_conflict} =
             Orchestration.retry_task(
               completed_task.id,
               task_attrs(agent_profile: "completed-profile"),
               "completed-retry-action",
               now: @now
             )
  end

  defp enroll_machine(name \\ "retry-machine") do
    key = Ecto.UUID.generate()

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(
               %{name: name, machine_token: "token-#{key}"},
               key,
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret",
               now: @now
             )

    enrolled
  end

  defp register_runtime(machine, runtime_key \\ "retry-runtime") do
    agent_profile = if runtime_key == "completed-runtime", do: "completed-profile", else: "codex"

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: runtime_key,
                   name: runtime_key,
                   capacity: 1,
                   agent_profile: agent_profile,
                   workspace: "primary",
                   capabilities: %{}
                 }
               ],
               now: @now
             )

    runtime
  end

  defp task_attrs(overrides \\ []) do
    Map.merge(
      %{goal: "Run tests", agent_profile: "codex", workspace: "primary", input: %{}},
      Map.new(overrides)
    )
  end

  defp claim(run, runtime) do
    assert {:ok, claimed} =
             Orchestration.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: uuid(10)
               },
               now: @now
             )

    %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: claimed.claim_id,
      lease_token: claimed.lease_token
    }
  end

  defp transition(run_id, fence, state, number) do
    assert {:ok, _run} =
             Orchestration.transition(run_id, fence, state, %{}, uuid(number), now: @now)
  end

  defp uuid(number),
    do: "00000000-0000-0000-0000-" <> String.pad_leading(Integer.to_string(number), 12, "0")
end
