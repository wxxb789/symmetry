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
    assert command.task_id == task.id
    assert command.run_id == run.id
    assert command.generation == run.generation
    assert command.state == "pending"
    assert {:error, :ownership_lost} = Orchestration.renew_lease(run.id, fence, now: claimed_at)

    assert %{expired_runs: 0} = Orchestration.expire(now: DateTime.add(@now, 31, :second))
    assert {:ok, still_cancelling} = Orchestration.fetch_run(run.id)
    assert still_cancelling.state == "cancelling"

    assert %{expired_runs: 1} = Orchestration.expire(now: DateTime.add(@now, 51, :second))
    assert {:ok, cancelled_run} = Orchestration.fetch_run(run.id)
    assert cancelled_run.state == "cancelled"
    assert {:ok, cancelled_task} = Orchestration.fetch_task(task.id)
    assert cancelled_task.state == "cancelled"

    for {target_state, transition_id} <- [
          {"completed", uuid(200)},
          {"failed", uuid(201)},
          {"cancelled", uuid(202)}
        ] do
      assert {:error, :ownership_lost} =
               Orchestration.transition(
                 run.id,
                 fence,
                 target_state,
                 %{},
                 transition_id,
                 now: DateTime.add(@now, 51, :second)
               )
    end

    assert {:ok, %{commands: []}} =
             Orchestration.work_snapshot(runtime.id, runtime.connection_epoch,
               now: DateTime.add(@now, 51, :second)
             )
  end

  test "assigned cancellation is applied by control and rejects late claim" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "assigned-cancel", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)

    assert {:ok, cancelled_task, command} = Orchestration.request_cancel(task.id, now: @now)

    assert cancelled_task.state == "cancelled"
    assert command.task_id == task.id
    assert command.run_id == run.id
    assert command.generation == run.generation
    assert command.state == "applied"
    assert command.applied_at == @now
    assert {:ok, cancelled_run} = Orchestration.fetch_run(run.id)
    assert cancelled_run.state == "cancelled"

    assert {:ok, ^cancelled_task, replayed_command} =
             Orchestration.request_cancel(task.id, now: @now)

    assert replayed_command.id == command.id

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

  test "failed input acknowledgement stops redelivery and permits a replacement input" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "input-failed-ack", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)
    transition(run.id, fence, "running", 65)
    transition(run.id, fence, "waiting_for_input", 66)

    {:ok, first, :created} =
      Orchestration.provide_input(task.id, %{"answer" => "first"}, "input-failed-first",
        now: @now
      )

    assert {:ok, _} =
             Orchestration.acknowledge_command(first.id, fence, "failed", uuid(67), now: @now)

    assert {:ok, %{commands: []}} =
             Orchestration.work_snapshot(runtime.id, runtime.connection_epoch, now: @now)

    assert {:error, :idempotency_conflict} =
             Orchestration.acknowledge_command(first.id, fence, "failed", uuid(68), now: @now)

    assert {:ok, %{commands: []}} =
             Orchestration.work_snapshot(runtime.id, runtime.connection_epoch, now: @now)

    assert {:ok, replacement, :created} =
             Orchestration.provide_input(
               task.id,
               %{"answer" => "replacement"},
               "input-failed-replacement",
               now: @now
             )

    assert replacement.id != first.id
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

  test "an input idempotency key is scoped to its task" do
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

    assert {:ok, first_command, :created} =
             Orchestration.provide_input(first_task.id, %{"answer" => "same"}, "shared-input-key",
               now: @now
             )

    assert {:ok, second_command, :created} =
             Orchestration.provide_input(
               second_task.id,
               %{"answer" => "same"},
               "shared-input-key",
               now: @now
             )

    assert first_command.id != second_command.id
    assert first_command.task_id == first_task.id
    assert second_command.task_id == second_task.id
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

  test "completed run accepts a late fenced input acknowledgement without changing lifecycle" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "late-input-ack", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)
    transition(run.id, fence, "running", 101)
    transition(run.id, fence, "waiting_for_input", 102)

    {:ok, command, :created} =
      Orchestration.provide_input(task.id, %{"answer" => "yes"}, "late-input-command", now: @now)

    transition(run.id, fence, "running", 103)

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               %{"summary" => "done"},
               uuid(104),
               now: @now
             )

    assert {:ok, acknowledged} =
             Orchestration.acknowledge_command(command.id, fence, "applied", uuid(105), now: @now)

    assert acknowledged.acknowledgement_outcome == "applied"
    assert {:ok, completed} = Orchestration.fetch_run(run.id)
    assert completed.state == "completed"
    assert {:ok, completed_task} = Orchestration.fetch_task(task.id)
    assert completed_task.state == "completed"
  end

  test "cancelled run accepts a late fenced cancel acknowledgement without changing lifecycle" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "late-cancel-ack", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)
    {:ok, _cancelling, command} = Orchestration.request_cancel(task.id, now: @now)
    transition(run.id, fence, "cancelled", 111)

    assert {:ok, acknowledged} =
             Orchestration.acknowledge_command(command.id, fence, "applied", uuid(112), now: @now)

    assert acknowledged.acknowledgement_outcome == "applied"
    assert {:ok, cancelled} = Orchestration.fetch_run(run.id)
    assert cancelled.state == "cancelled"
    assert {:ok, cancelled_task} = Orchestration.fetch_task(task.id)
    assert cancelled_task.state == "cancelled"
  end

  test "terminal acknowledgement rejects stale epoch and generation" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)

    {:ok, task, :created} =
      Orchestration.submit_task(task_attrs(), "terminal-stale-ack", now: @now)

    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)
    transition(run.id, fence, "running", 121)
    transition(run.id, fence, "waiting_for_input", 122)

    {:ok, command, :created} =
      Orchestration.provide_input(task.id, %{"answer" => "yes"}, "terminal-stale-command",
        now: @now
      )

    transition(run.id, fence, "running", 123)
    transition(run.id, fence, "completed", 124)

    assert {:error, :ownership_lost} =
             Orchestration.acknowledge_command(
               command.id,
               %{fence | runtime_epoch: fence.runtime_epoch + 1},
               "applied",
               uuid(125),
               now: @now
             )

    assert {:error, :ownership_lost} =
             Orchestration.acknowledge_command(
               command.id,
               %{fence | generation: fence.generation + 1},
               "applied",
               uuid(126),
               now: @now
             )
  end

  test "a cancelling run replays its original unexpired claim without reopening lifecycle" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)

    {:ok, task, :created} =
      Orchestration.submit_task(task_attrs(), "cancelling-claim-replay", now: @now)

    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

    assert {:ok, cancelling, _command} = Orchestration.request_cancel(task.id, now: @now)
    assert cancelling.state == "cancelling"

    assert {:ok, replayed} =
             Orchestration.claim(
               run.id,
               %{
                 runtime_id: fence.runtime_id,
                 runtime_epoch: fence.runtime_epoch,
                 generation: fence.generation,
                 claim_id: fence.claim_id
               },
               now: @now
             )

    assert replayed.lease_token == fence.lease_token
    assert replayed.state == "cancelling"

    assert {:error, :ownership_lost} =
             Orchestration.claim(
               run.id,
               %{
                 runtime_id: fence.runtime_id,
                 runtime_epoch: fence.runtime_epoch,
                 generation: fence.generation,
                 claim_id: uuid(131)
               },
               now: @now
             )
  end

  test "a waiting episode permits one unacknowledged input command" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)

    {:ok, task, :created} =
      Orchestration.submit_task(task_attrs(), "single-input-episode", now: @now)

    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)
    transition(run.id, fence, "running", 141)
    transition(run.id, fence, "waiting_for_input", 142)

    assert {:ok, first, :created} =
             Orchestration.provide_input(task.id, %{"choice" => "main"}, "input-first", now: @now)

    assert {:ok, replayed, :replayed} =
             Orchestration.provide_input(task.id, %{"choice" => "main"}, "input-first", now: @now)

    assert replayed.id == first.id

    assert {:error, :state_conflict} =
             Orchestration.provide_input(task.id, %{"choice" => "release"}, "input-second",
               now: @now
             )

    assert {:error, :state_conflict} =
             Orchestration.acknowledge_command(first.id, fence, "applied", uuid(143), now: @now)

    assert {:error, :state_conflict} =
             Orchestration.provide_input(task.id, %{"choice" => "release"}, "input-second",
               now: @now
             )

    transition(run.id, fence, "running", 144)

    assert {:ok, _} =
             Orchestration.acknowledge_command(first.id, fence, "applied", uuid(143), now: @now)

    transition(run.id, fence, "waiting_for_input", 145)

    assert {:ok, _second, :created} =
             Orchestration.provide_input(task.id, %{"choice" => "release"}, "input-second",
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

      assert Enum.map(inputs, fn {:ok, _command, disposition} -> disposition end) |> Enum.sort() ==
               [:created, :replayed]

      assert Enum.map(inputs, fn {:ok, command, _} -> command.id end) |> Enum.uniq() |> length() ==
               1

      Repo.delete!(task)
      Repo.delete!(machine)
    end)
  end

  test "provide_input cannot bind to a replacement generation after lease expiry" do
    Sandbox.unboxed_run(Repo, fn ->
      %{machine: machine} = enroll_machine()
      runtime = register_runtime(machine)

      {:ok, task, :created} =
        Orchestration.submit_task(task_attrs(), "input-generation-race", now: @now)

      {:ok, run} = Orchestration.assign_one(now: @now)
      fence = claim(run, runtime)
      transition(run.id, fence, "running", 151)
      transition(run.id, fence, "waiting_for_input", 152)
      expired_at = DateTime.add(@now, 31, :second)
      parent = self()

      assert {:ok, {input_task, replacement_run}} =
               Repo.transaction(fn ->
                 Repo.one!(
                   from task_row in SymmetryControl.Orchestration.Task,
                     where: task_row.id == ^task.id,
                     lock: "FOR UPDATE"
                 )

                 input_task =
                   Task.async(fn ->
                     Sandbox.unboxed_run(Repo, fn ->
                       send(parent, :provide_input_started)

                       Orchestration.provide_input(
                         task.id,
                         %{"answer" => "stale"},
                         "input-generation-race",
                         now: expired_at
                       )
                     end)
                   end)

                 assert_receive :provide_input_started, 1_000
                 assert %{expired_runs: 1} = Orchestration.expire(now: expired_at)

                 assert {:ok, _snapshot} =
                          Orchestration.heartbeat(
                            runtime.id,
                            runtime.connection_epoch,
                            [],
                            now: expired_at
                          )

                 assert {:ok, replacement_run} = Orchestration.assign_one(now: expired_at)
                 {input_task, replacement_run}
               end)

      assert {:error, :state_conflict} = Task.await(input_task, 5_000)

      refute Repo.exists?(
               from command in SymmetryControl.Orchestration.Command,
                 where: command.task_id == ^task.id and command.kind == "provide_input"
             )

      refute Repo.exists?(
               from command in SymmetryControl.Orchestration.Command,
                 where: command.run_id == ^replacement_run.id and command.kind == "provide_input"
             )

      Repo.delete!(task)
      Repo.delete!(machine)
    end)
  end

  test "terminal grace delivery and replacement assignment serialize on the current generation" do
    Sandbox.unboxed_run(Repo, fn ->
      %{machine: machine} = enroll_machine()
      suffix = System.unique_integer([:positive])
      agent_profile = "terminal-race-#{suffix}"

      assert {:ok, [runtime]} =
               Orchestration.register_runtimes(
                 machine.id,
                 Ecto.UUID.generate(),
                 [runtime_attrs(agent_profile: agent_profile)],
                 now: @now
               )

      assert {:ok, task, :created} =
               Orchestration.submit_task(
                 task_attrs(agent_profile: agent_profile),
                 "terminal-race-#{suffix}",
                 now: @now
               )

      assert {:ok, run} = Orchestration.assign_one(now: @now)
      fence = claim(run, runtime)
      transition(run.id, fence, "running", 251)
      expired_at = DateTime.add(@now, 30, :second)
      terminal_at = DateTime.add(expired_at, 1, :second)

      assert %{expired_runs: 1} = Orchestration.expire(now: expired_at)

      assert {:ok, _snapshot} =
               Orchestration.heartbeat(runtime.id, runtime.connection_epoch, [], now: expired_at)

      [terminal_result, assignment_result] =
        concurrently(2, fn
          1 ->
            Orchestration.transition(
              run.id,
              fence,
              "completed",
              %{"summary" => "race winner"},
              Ecto.UUID.generate(),
              now: terminal_at
            )

          2 ->
            Orchestration.assign_one(now: terminal_at)
        end)

      case {terminal_result, assignment_result} do
        {{:ok, %{state: "completed"}}, {:error, :no_assignment}} ->
          assert {:ok, %{state: "completed", current_generation: 1}} =
                   Orchestration.fetch_task(task.id)

        {{:error, :ownership_lost}, {:ok, %{generation: 2}}} ->
          assert {:ok, %{state: "assigned", current_generation: 2}} =
                   Orchestration.fetch_task(task.id)

        other ->
          flunk("unexpected terminal/replacement race result: #{inspect(other)}")
      end

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
