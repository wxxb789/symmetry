defmodule SymmetryControl.OrchestrationReviewTest do
  use SymmetryControl.DataCase, async: false

  alias Ecto.Adapters.SQL.Sandbox
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Repo

  @now ~U[2026-09-02 00:00:00.000000Z]

  test "cancelling expires by lease into cancelled and cannot renew" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "cancel-expiry", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    claimed_at = DateTime.add(@now, 20, :second)
    fence = claim(run, runtime, claimed_at)

    assert {:ok, cancelling, command} = Orchestration.request_cancel(task.id, now: claimed_at)
    assert cancelling.state == "cancelling"
    assert command.kind == "cancel"
    assert {:error, :ownership_lost} = Orchestration.renew_lease(run.id, fence, now: claimed_at)

    assert %{expired_runs: 0} = Orchestration.expire(now: DateTime.add(@now, 31, :second))
    assert {:ok, still_cancelling} = Orchestration.fetch_run(run.id)
    assert still_cancelling.state == "cancelling"

    assert %{expired_runs: 1} = Orchestration.expire(now: DateTime.add(@now, 51, :second))
    assert {:ok, cancelled_run} = Orchestration.fetch_run(run.id)
    assert cancelled_run.state == "cancelled"
    assert {:ok, cancelled_task} = Orchestration.fetch_task(task.id)
    assert cancelled_task.state == "cancelled"

    assert {:ok, %{commands: []}} =
             Orchestration.work_snapshot(runtime.id, runtime.connection_epoch,
               now: DateTime.add(@now, 51, :second)
             )
  end

  test "assigned cancellation is terminal without a command and rejects late claim" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "assigned-cancel", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)

    assert {:ok, cancelled_task, nil} = Orchestration.request_cancel(task.id, now: @now)
    assert cancelled_task.state == "cancelled"
    assert {:ok, cancelled_run} = Orchestration.fetch_run(run.id)
    assert cancelled_run.state == "cancelled"

    assert {:error, :ownership_lost} =
             Orchestration.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: uuid(51)
               },
               now: @now
             )

    assert {:ok, %{commands: []}} =
             Orchestration.work_snapshot(runtime.id, runtime.connection_epoch, now: @now)
  end

  test "snapshot retains input until both acknowledgement and resume, then hides it" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "input-cleanup", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)
    transition(run.id, fence, "running", 61)
    transition(run.id, fence, "waiting_for_input", 62)

    {:ok, command, :created} =
      Orchestration.provide_input(task.id, %{"answer" => "yes"}, "input-cleanup-1", now: @now)

    assert {:ok, %{commands: [%{id: command_id}]}} =
             Orchestration.work_snapshot(runtime.id, runtime.connection_epoch, now: @now)

    assert command_id == command.id
    transition(run.id, fence, "running", 63)

    assert {:ok, %{commands: [%{id: ^command_id}]}} =
             Orchestration.work_snapshot(runtime.id, runtime.connection_epoch, now: @now)

    assert {:ok, _} =
             Orchestration.acknowledge_command(command.id, fence, "applied", uuid(64), now: @now)

    assert {:error, :idempotency_conflict} =
             Orchestration.acknowledge_command(command.id, fence, "failed", uuid(64), now: @now)

    assert {:ok, %{commands: []}} =
             Orchestration.work_snapshot(runtime.id, runtime.connection_epoch, now: @now)
  end

  test "transition replay returns its original state and event replay rejects body changes" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    {:ok, _task, :created} = Orchestration.submit_task(task_attrs(), "replay-body", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)
    transition(run.id, fence, "running", 71)

    assert {:ok, waiting} =
             Orchestration.transition(run.id, fence, "waiting_for_input", %{}, uuid(72),
               now: @now
             )

    assert waiting.state == "waiting_for_input"
    transition(run.id, fence, "running", 73)

    assert {:ok, replayed} =
             Orchestration.transition(run.id, fence, "waiting_for_input", %{}, uuid(72),
               now: @now
             )

    assert replayed.state == "waiting_for_input"
    assert replayed.result == nil
    assert replayed.failure == nil

    event = %{
      event_id: uuid(74),
      sequence: 1,
      kind: "progress",
      payload: %{"message" => "one"},
      occurred_at: @now
    }

    assert {:ok, [_]} = Orchestration.append_events(run.id, fence, [event], now: @now)

    assert {:error, :idempotency_conflict} =
             Orchestration.append_events(
               run.id,
               fence,
               [%{event | payload: %{"message" => "two"}}],
               now: @now
             )
  end

  test "a queued task without a matching runtime does not starve a later matching task" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)

    {:ok, unmatched, :created} =
      Orchestration.submit_task(task_attrs(agent_profile: "other"), "unmatched", now: @now)

    {:ok, matching, :created} =
      Orchestration.submit_task(task_attrs(), "matching",
        now: DateTime.add(@now, 1, :microsecond)
      )

    assert {:ok, assigned} = Orchestration.assign_one(now: @now)
    assert assigned.task_id == matching.id
    assert assigned.runtime_id == runtime.id
    assert {:ok, still_queued} = Orchestration.fetch_task(unmatched.id)
    assert still_queued.state == "queued"
  end

  test "an input idempotency key cannot replay a command from another task" do
    %{machine: machine} = enroll_machine()

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               uuid(82),
               [runtime_attrs(capacity: 2)],
               now: @now
             )

    {:ok, first_task, :created} =
      Orchestration.submit_task(task_attrs(goal: "first"), "input-first", now: @now)

    {:ok, second_task, :created} =
      Orchestration.submit_task(task_attrs(goal: "second"), "input-second", now: @now)

    {:ok, runs} = Orchestration.assign_all(now: @now)
    runs_by_task = Map.new(runs, &{&1.task_id, &1})

    for task <- [first_task, second_task] do
      run = Map.fetch!(runs_by_task, task.id)
      fence = claim(run, runtime)
      transition(run.id, fence, "running", System.unique_integer([:positive]))
      transition(run.id, fence, "waiting_for_input", System.unique_integer([:positive]))
    end

    assert {:ok, _command, :created} =
             Orchestration.provide_input(first_task.id, %{"answer" => "same"}, "shared-input-key",
               now: @now
             )

    assert {:error, :idempotency_conflict} =
             Orchestration.provide_input(
               second_task.id,
               %{"answer" => "same"},
               "shared-input-key",
               now: @now
             )
  end

  test "invalid IDs return protocol errors instead of database exceptions" do
    assert {:error, :invalid_request} = Orchestration.claim("not-a-uuid", %{}, now: @now)
    assert {:error, :invalid_request} = Orchestration.work_snapshot("not-a-uuid", 1, now: @now)

    assert {:error, :invalid_request} =
             Orchestration.acknowledge_command("not-a-uuid", %{}, "applied", uuid(81), now: @now)
  end

  test "heartbeat and reconcile reject malformed active run payloads" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)

    assert {:error, :invalid_request} =
             Orchestration.heartbeat(runtime.id, runtime.connection_epoch, :not_a_list, now: @now)

    assert {:error, :invalid_request} =
             Orchestration.reconcile(
               runtime.id,
               runtime.connection_epoch,
               [%{run_id: "not-a-uuid"}],
               now: @now
             )
  end

  test "concurrent first task submission and runtime registration are idempotent" do
    Sandbox.unboxed_run(Repo, fn ->
      %{machine: machine} = enroll_machine()
      suffix = System.unique_integer([:positive])
      agent_profile = "concurrent-#{suffix}"
      task_key = "concurrent-task-#{suffix}"
      runtime_key = "concurrent-runtime-#{suffix}"
      concurrent_now = DateTime.add(@now, 1, :hour)

      submitted =
        concurrently(2, fn _ ->
          Orchestration.submit_task(task_attrs(agent_profile: agent_profile), task_key,
            now: concurrent_now
          )
        end)

      assert Enum.count(submitted, &match?({:ok, _, _}, &1)) == 2
      assert Enum.map(submitted, fn {:ok, task, _} -> task.id end) |> Enum.uniq() |> length() == 1

      registered =
        concurrently(2, fn _ ->
          Orchestration.register_runtimes(
            machine.id,
            uuid(91),
            [runtime_attrs(runtime_key: runtime_key, agent_profile: agent_profile)],
            now: concurrent_now
          )
        end)

      assert Enum.all?(registered, &match?({:ok, [_]}, &1))

      assert Enum.map(registered, fn {:ok, [runtime]} -> runtime.id end)
             |> Enum.uniq()
             |> length() == 1

      assert Enum.map(registered, fn {:ok, [runtime]} -> runtime.connection_epoch end)
             |> Enum.uniq() == [1]

      {:ok, task, _} = hd(submitted)
      {:ok, [runtime]} = hd(registered)
      {:ok, run} = Orchestration.assign_one(now: concurrent_now)
      fence = claim(run, runtime, concurrent_now)
      transition(run.id, fence, "running", 92, concurrent_now)
      transition(run.id, fence, "waiting_for_input", 93, concurrent_now)

      inputs =
        concurrently(2, fn _ ->
          Orchestration.provide_input(
            task.id,
            %{"answer" => "same"},
            "concurrent-input-#{suffix}",
            now: concurrent_now
          )
        end)

      assert Enum.count(inputs, &match?({:ok, _, _}, &1)) == 2

      assert Enum.map(inputs, fn {:ok, command, _} -> command.id end) |> Enum.uniq() |> length() ==
               1

      Repo.delete!(task)
      Repo.delete!(machine)
    end)
  end

  defp enroll_machine do
    assert {:ok, enrolled} =
             Orchestration.enroll_machine(%{name: "builder"},
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret",
               now: @now
             )

    enrolled
  end

  defp register_runtime(machine) do
    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(machine.id, uuid(1), [runtime_attrs()], now: @now)

    runtime
  end

  defp runtime_attrs(overrides \\ []) do
    Map.merge(
      %{
        runtime_key: "default",
        name: "Local Codex",
        capacity: 1,
        agent_profile: "codex",
        workspace: "primary",
        capabilities: %{}
      },
      Map.new(overrides)
    )
  end

  defp task_attrs(overrides \\ []) do
    Map.merge(
      %{goal: "Run tests", agent_profile: "codex", workspace: "primary", input: %{}},
      Map.new(overrides)
    )
  end

  defp claim(run, runtime, current \\ @now) do
    assert {:ok, claimed} =
             Orchestration.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: uuid(10)
               },
               now: current
             )

    %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: claimed.claim_id,
      lease_token: claimed.lease_token
    }
  end

  defp transition(run_id, fence, state, number, current \\ @now) do
    assert {:ok, _} =
             Orchestration.transition(run_id, fence, state, %{}, uuid(number), now: current)
  end

  defp concurrently(count, fun) do
    parent = self()

    tasks =
      for index <- 1..count do
        Task.async(fn ->
          Sandbox.unboxed_run(Repo, fn ->
            send(parent, {:ready, self()})
            receive do: (:go -> fun.(index))
          end)
        end)
      end

    for _ <- 1..count, do: assert_receive({:ready, _}, 1_000)
    Enum.each(tasks, &send(&1.pid, :go))
    Enum.map(tasks, &Task.await(&1, 5_000))
  end

  defp uuid(number),
    do: "00000000-0000-0000-0000-" <> String.pad_leading(Integer.to_string(number), 12, "0")
end
