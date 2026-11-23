defmodule SymmetryControl.OrchestrationTransitionsTest do
  use SymmetryControl.OrchestrationCase, async: false

  alias SymmetryControl.Orchestration

  @now ~U[2026-09-02 00:00:00.000000Z]

  test "enrolls, registers, assigns, claims, runs, and completes work" do
    %{machine: machine, token: token} = enroll_machine()
    runtime = register_runtime(machine, @now)

    assert {:ok, ^machine} = Orchestration.authenticate_machine(token)
    assert {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == task.id
    assert run.generation == 1

    fence = claim(run, runtime, @now)

    assert {:ok, _run} =
             Orchestration.transition(
               run.id,
               fence,
               "running",
               %{},
               "00000000-0000-0000-0000-000000000031",
               now: @now
             )

    assert {:ok, completed} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               %{"summary" => "done"},
               "00000000-0000-0000-0000-000000000032",
               now: @now
             )

    assert completed.state == "completed"
  end

  test "terminal transitions accept the fixed eight minute grace boundary after lease expiry" do
    for {label, current, expected} <- [
          {:before_expiry, DateTime.shift(@now, second: 29), :ok},
          {:inside_grace, DateTime.shift(@now, second: 509), :ok},
          {:grace_boundary, DateTime.shift(@now, second: 510), :ok},
          {:outside_grace, DateTime.shift(@now, second: 511), :terminal_grace_expired}
        ] do
      %{machine: machine} = enroll_machine()
      agent_profile = "terminal-grace-#{label}"
      runtime = register_runtime(machine, @now, agent_profile: agent_profile)

      assert {:ok, _task, :created} =
               Orchestration.submit_task(
                 task_attrs(agent_profile: agent_profile),
                 "terminal-grace-#{label}",
                 now: @now
               )

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

      case expected do
        :ok ->
          assert {:ok, %{state: "completed"}} =
                   Orchestration.transition(
                     run.id,
                     fence,
                     "completed",
                     %{"label" => Atom.to_string(label)},
                     Ecto.UUID.generate(),
                     now: current
                   )

        :terminal_grace_expired ->
          assert {:error, :terminal_grace_expired} =
                   Orchestration.transition(
                     run.id,
                     fence,
                     "completed",
                     %{},
                     Ecto.UUID.generate(),
                     now: current
                   )
      end
    end
  end

  test "terminal operations retain the claimed epoch across runtime re-registration" do
    {task, run, runtime, fence} = terminal_grace_fixture("re-registration")

    assert {:ok, %{state: "waiting_for_input"}} =
             Orchestration.transition(
               run.id,
               fence,
               "waiting_for_input",
               %{},
               Ecto.UUID.generate(),
               now: @now
             )

    assert {:ok, command, :created} =
             Orchestration.create_command(
               task.id,
               "provide_input",
               %{"answer" => "yes"},
               "re-registration-command",
               now: @now
             )

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(
               run.id,
               fence,
               "running",
               %{},
               Ecto.UUID.generate(),
               now: @now
             )

    assert {:ok, reconnected_runtime} =
             re_register_runtime(runtime, Ecto.UUID.generate(), DateTime.shift(@now, second: 1))

    assert reconnected_runtime.id == runtime.id
    assert reconnected_runtime.connection_epoch == runtime.connection_epoch + 1

    assert {:error, :ownership_lost} =
             Orchestration.renew_lease(run.id, fence, now: DateTime.shift(@now, second: 1))

    assert {:error, :ownership_lost} =
             Orchestration.append_events(
               run.id,
               fence,
               [
                 %{
                   event_id: Ecto.UUID.generate(),
                   sequence: 1,
                   kind: "progress",
                   payload: %{},
                   occurred_at: @now
                 }
               ],
               now: DateTime.shift(@now, second: 1)
             )

    assert {:error, :ownership_lost} =
             Orchestration.transition(
               run.id,
               fence,
               "waiting_for_input",
               %{},
               Ecto.UUID.generate(),
               now: DateTime.shift(@now, second: 1)
             )

    expired_at = DateTime.shift(@now, second: 30)
    terminal_at = DateTime.shift(expired_at, second: 1)
    transition_id = Ecto.UUID.generate()

    assert %{expired_runs: 1} = Orchestration.expire(now: expired_at)

    assert {:ok, %{state: "completed"}} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               %{"summary" => "terminal after re-registration"},
               transition_id,
               now: terminal_at
             )

    assert {:ok, %{state: "completed"}} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               %{"summary" => "terminal after re-registration"},
               transition_id,
               now: DateTime.shift(terminal_at, second: 1)
             )

    acknowledgement_id = Ecto.UUID.generate()

    assert {:ok, %{state: "acknowledged"}} =
             Orchestration.acknowledge_command(
               command.id,
               fence,
               "applied",
               acknowledgement_id,
               now: terminal_at
             )

    assert {:ok, %{state: "acknowledged", acknowledgement_id: ^acknowledgement_id}} =
             Orchestration.acknowledge_command(
               command.id,
               fence,
               "applied",
               acknowledgement_id,
               now: DateTime.shift(terminal_at, second: 1)
             )

    assert {:ok, %{state: "completed", current_generation: 1}} =
             Orchestration.fetch_task(task.id)
  end

  test "a replacement generation fences terminal operations after runtime re-registration" do
    {task, run, runtime, fence} = terminal_grace_fixture("re-registration-replacement")

    assert {:ok, %{state: "waiting_for_input"}} =
             Orchestration.transition(
               run.id,
               fence,
               "waiting_for_input",
               %{},
               Ecto.UUID.generate(),
               now: @now
             )

    assert {:ok, command, :created} =
             Orchestration.create_command(
               task.id,
               "provide_input",
               %{"answer" => "yes"},
               "re-registration-replacement-command",
               now: @now
             )

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(
               run.id,
               fence,
               "running",
               %{},
               Ecto.UUID.generate(),
               now: @now
             )

    assert {:ok, reconnected_runtime} =
             re_register_runtime(runtime, Ecto.UUID.generate(), DateTime.shift(@now, second: 1))

    expired_at = DateTime.shift(@now, second: 30)

    assert %{expired_runs: 1} = Orchestration.expire(now: expired_at)

    assert {:ok, _snapshot} =
             Orchestration.heartbeat(
               reconnected_runtime.id,
               reconnected_runtime.connection_epoch,
               [],
               now: expired_at
             )

    assert {:ok, %{generation: 2}} = Orchestration.assign_one(now: expired_at)

    assert {:error, :ownership_lost} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               %{},
               Ecto.UUID.generate(),
               now: DateTime.shift(expired_at, second: 1)
             )

    assert {:error, :ownership_lost} =
             Orchestration.acknowledge_command(
               command.id,
               fence,
               "applied",
               Ecto.UUID.generate(),
               now: DateTime.shift(expired_at, second: 1)
             )

    assert {:ok, %{state: "assigned", current_generation: 2}} =
             Orchestration.fetch_task(task.id)
  end

  test "a replacement generation fences an expired generation's terminal delivery" do
    {task, run, runtime, fence} = terminal_grace_fixture("replacement-generation")
    expired_at = DateTime.shift(@now, second: 30)

    assert %{expired_runs: 1} = Orchestration.expire(now: expired_at)

    assert {:ok, _snapshot} =
             Orchestration.heartbeat(runtime.id, runtime.connection_epoch, [], now: expired_at)

    assert {:ok, replacement} = Orchestration.assign_one(now: expired_at)
    assert replacement.generation == run.generation + 1

    assert {:error, :ownership_lost} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               %{},
               Ecto.UUID.generate(),
               now: DateTime.shift(expired_at, second: 1)
             )

    assert {:ok, %{current_generation: generation}} = Orchestration.fetch_task(task.id)
    assert generation == replacement.generation
  end

  test "events and transitions are replay-safe and detect changed bodies" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    {:ok, _task, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime, @now)

    event = %{
      event_id: "00000000-0000-0000-0000-000000000021",
      sequence: 1,
      kind: "progress",
      payload: %{},
      occurred_at: @now
    }

    assert {:ok, [_]} = Orchestration.append_events(run.id, fence, [event], now: @now)
    assert {:ok, [_]} = Orchestration.append_events(run.id, fence, [event], now: @now)

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "running",
               %{},
               "00000000-0000-0000-0000-000000000033",
               now: @now
             )

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "running",
               %{},
               "00000000-0000-0000-0000-000000000033",
               now: @now
             )

    assert {:error, :idempotency_conflict} =
             Orchestration.transition(
               run.id,
               fence,
               "waiting_for_input",
               %{},
               "00000000-0000-0000-0000-000000000033",
               now: @now
             )

    batch_event = %{
      event_id: "00000000-0000-0000-0000-000000000034",
      sequence: 2,
      kind: "progress",
      payload: %{"message" => "batch"},
      occurred_at: @now
    }

    assert {:error, :idempotency_conflict} =
             Orchestration.append_events(
               run.id,
               fence,
               [batch_event, %{batch_event | payload: %{"message" => "changed"}}],
               now: @now
             )

    assert {:ok, [first_batch_event, second_batch_event]} =
             Orchestration.append_events(run.id, fence, [batch_event, batch_event], now: @now)

    assert first_batch_event.id == second_batch_event.id

    assert {:ok, [replayed_batch_event]} =
             Orchestration.append_events(run.id, fence, [batch_event], now: @now)

    assert replayed_batch_event.id == first_batch_event.id
  end

  test "fenced execution mutations serialize behind a concurrent run lock" do
    with_dedicated_orchestration_repo(fn ->
      {:ok, %{machine: machine, runtime: runtime, task: task, run: run, fence: fence}} =
        Repo.transaction(fn ->
          %{machine: machine} = enroll_machine()
          runtime = register_runtime(machine, @now)

          {:ok, task, :created} =
            Orchestration.submit_task(
              task_attrs(),
              "locked-run-#{Ecto.UUID.generate()}",
              now: @now
            )

          {:ok, run} = Orchestration.assign_one(now: @now)
          fence = claim(run, runtime, @now)

          %{machine: machine, runtime: runtime, task: task, run: run, fence: fence}
        end)

      fixture = %{
        machine_id: machine.id,
        runtime_id: runtime.id,
        task_id: task.id,
        run_id: run.id
      }

      try do
        event = %{
          event_id: Ecto.UUID.generate(),
          sequence: 1,
          kind: "progress",
          payload: %{},
          occurred_at: @now
        }

        assert_run_lock_timeout(run.id, fn ->
          Orchestration.append_events(run.id, fence, [event], now: @now)
        end)

        assert {:ok, [_]} = Orchestration.append_events(run.id, fence, [event], now: @now)

        transition_id = Ecto.UUID.generate()

        assert_run_lock_timeout(run.id, fn ->
          Orchestration.transition(
            run.id,
            fence,
            "running",
            %{},
            transition_id,
            now: @now
          )
        end)

        assert {:ok, %{state: "running"}} =
                 Orchestration.transition(run.id, fence, "running", %{}, transition_id, now: @now)

        assert {:ok, %{state: "waiting_for_input"}} =
                 Orchestration.transition(
                   run.id,
                   fence,
                   "waiting_for_input",
                   %{},
                   Ecto.UUID.generate(),
                   now: @now
                 )

        assert {:ok, command, :created} =
                 Orchestration.create_command(
                   task.id,
                   "provide_input",
                   %{"answer" => "yes"},
                   "locked-run-input-#{Ecto.UUID.generate()}",
                   now: @now
                 )

        assert {:ok, %{state: "running"}} =
                 Orchestration.transition(
                   run.id,
                   fence,
                   "running",
                   %{},
                   Ecto.UUID.generate(),
                   now: @now
                 )

        acknowledgement_id = Ecto.UUID.generate()

        assert_run_lock_timeout(run.id, fn ->
          Orchestration.acknowledge_command(
            command.id,
            fence,
            "applied",
            acknowledgement_id,
            now: @now
          )
        end)

        assert {:ok, %{state: "acknowledged"}} =
                 Orchestration.acknowledge_command(
                   command.id,
                   fence,
                   "applied",
                   acknowledgement_id,
                   now: @now
                 )
      after
        cleanup_dedicated_orchestration_fixture(fixture)
      end
    end)
  end

  test "JSONB-bound runtime, task, command, and transition maps reject NUL before persistence" do
    %{machine: machine} = enroll_machine()

    assert {:error, :invalid_request} =
             Orchestration.register_runtimes(
               machine.id,
               "00000000-0000-0000-0000-000000000061",
               [
                 %{
                   runtime_key: "nul-runtime",
                   name: "NUL runtime",
                   capacity: 1,
                   agent_profile: "codex",
                   workspace: "primary",
                   capabilities: %{"bad" => "value\0"}
                 }
               ],
               now: @now
             )

    assert Repo.aggregate(SymmetryControl.Orchestration.Runtime, :count) == 0

    assert {:error, :invalid_request} =
             Orchestration.submit_task(
               task_attrs(input: %{"bad" => "value\0"}),
               "nul-task-input",
               now: @now
             )

    assert Repo.aggregate(SymmetryControl.Orchestration.Task, :count) == 0

    runtime = register_runtime(machine, @now)

    assert {:ok, task, :created} =
             Orchestration.submit_task(task_attrs(), "nul-command", now: @now)

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime, @now)

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "running",
               %{},
               "00000000-0000-0000-0000-000000000062",
               now: @now
             )

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "waiting_for_input",
               %{},
               "00000000-0000-0000-0000-000000000063",
               now: @now
             )

    assert {:error, :invalid_request} =
             Orchestration.create_command(
               task.id,
               "provide_input",
               %{"bad" => "value\0"},
               "nul-command-payload",
               now: @now
             )

    assert Repo.aggregate(SymmetryControl.Orchestration.Command, :count) == 0

    assert {:error, :invalid_request} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               %{"bad" => "value\0"},
               "00000000-0000-0000-0000-000000000064",
               now: @now
             )

    assert {:ok, current_run} = Orchestration.fetch_run(run.id)
    assert current_run.state == "waiting_for_input"
    assert Repo.aggregate(SymmetryControl.Orchestration.RunTransition, :count) == 2
  end

  test "unknown targets and illegal lifecycle edges are invalid transitions" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)

    {:ok, _task, :created} =
      Orchestration.submit_task(task_attrs(), "transition-errors", now: @now)

    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime, @now)

    assert {:error, :invalid_transition} =
             Orchestration.transition(
               run.id,
               fence,
               "unknown",
               %{},
               "00000000-0000-0000-0000-000000000047",
               now: @now
             )

    assert {:error, :invalid_transition} =
             Orchestration.transition(
               run.id,
               fence,
               "waiting_for_input",
               %{},
               "00000000-0000-0000-0000-000000000048",
               now: @now
             )

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "running",
               %{},
               "00000000-0000-0000-0000-000000000049",
               now: @now
             )

    assert {:error, :state_conflict} =
             Orchestration.transition(
               run.id,
               fence,
               "running",
               %{},
               "00000000-0000-0000-0000-000000000050",
               now: @now
             )
  end

  test "task commands scope idempotency to task and replay before later state validation" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)

    {:ok, task, :created} =
      Orchestration.submit_task(task_attrs(), "command-idempotency", now: @now)

    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime, @now)

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "running",
               %{},
               "00000000-0000-0000-0000-000000000065",
               now: @now
             )

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "waiting_for_input",
               %{},
               "00000000-0000-0000-0000-000000000066",
               now: @now
             )

    assert {:ok, command, :created} =
             Orchestration.create_command(
               task.id,
               "provide_input",
               %{"answer" => "first"},
               "same-command-key",
               now: @now
             )

    assert command.task_id == task.id
    assert command.run_id == run.id
    assert command.generation == run.generation
    assert command.state == "pending"

    assert {:error, :idempotency_conflict} =
             Orchestration.create_command(
               task.id,
               "provide_input",
               %{"answer" => "second"},
               "same-command-key",
               now: @now
             )

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "running",
               %{},
               "00000000-0000-0000-0000-000000000067",
               now: @now
             )

    assert {:ok, replayed, :replayed} =
             Orchestration.create_command(
               task.id,
               "provide_input",
               %{"answer" => "first"},
               "same-command-key",
               now: @now
             )

    assert replayed.id == command.id
  end

  test "input is bound to a waiting generation and late terminal delivery preserves unrelated work" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)

    {:ok, unaffected, :created} =
      Orchestration.submit_task(task_attrs(goal: "unrelated"), "task-2", now: DateTime.add(@now, 1, :microsecond))

    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime, @now)

    {:ok, _} =
      Orchestration.transition(
        run.id,
        fence,
        "running",
        %{},
        "00000000-0000-0000-0000-000000000036",
        now: @now
      )

    {:ok, _} =
      Orchestration.transition(
        run.id,
        fence,
        "waiting_for_input",
        %{},
        "00000000-0000-0000-0000-000000000037",
        now: @now
      )

    assert {:ok, command, :created} =
             Orchestration.provide_input(task.id, %{"choice" => "main"}, "input-1", now: @now)

    assert command.run_id == run.id

    later = DateTime.shift(@now, second: 31)
    assert %{expired_runs: 1} = Orchestration.expire(now: later)

    assert {:ok, %{state: "completed"}} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               %{},
               "00000000-0000-0000-0000-000000000038",
               now: later
             )

    assert {:error, :state_conflict} =
             Orchestration.provide_input(task.id, %{}, "input-2", now: later)

    assert {:ok, completed} = Orchestration.fetch_task(task.id)
    assert completed.state == "completed"
    assert {:ok, still_unaffected} = Orchestration.fetch_task(unaffected.id)
    assert still_unaffected.state == "queued"
  end

  test "completion committed first makes cancellation return the terminal task" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime, @now)

    {:ok, _} =
      Orchestration.transition(
        run.id,
        fence,
        "running",
        %{},
        "00000000-0000-0000-0000-000000000039",
        now: @now
      )

    {:ok, _} =
      Orchestration.transition(
        run.id,
        fence,
        "completed",
        %{"summary" => "completed first"},
        "00000000-0000-0000-0000-000000000040",
        now: @now
      )

    assert {:ok, completed, nil} = Orchestration.request_cancel(task.id, now: @now)
    assert completed.state == "completed"
  end

  test "input commands cannot cross a requeued generation" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)
    {:ok, first_run} = Orchestration.assign_one(now: @now)
    first_fence = claim(first_run, runtime, @now)

    {:ok, _} =
      Orchestration.transition(
        first_run.id,
        first_fence,
        "running",
        %{},
        "00000000-0000-0000-0000-000000000041",
        now: @now
      )

    {:ok, _} =
      Orchestration.transition(
        first_run.id,
        first_fence,
        "waiting_for_input",
        %{},
        "00000000-0000-0000-0000-000000000042",
        now: @now
      )

    {:ok, old_command, :created} =
      Orchestration.provide_input(task.id, %{"choice" => "old"}, "input-1", now: @now)

    later = DateTime.shift(@now, second: 31)
    assert %{expired_runs: 1} = Orchestration.expire(now: later)

    assert {:ok, _snapshot} =
             Orchestration.heartbeat(runtime.id, runtime.connection_epoch, [], now: later)

    assert {:ok, second_run} = Orchestration.assign_one(now: later)
    assert second_run.generation == first_run.generation + 1

    assert {:ok, %{commands: []}} =
             Orchestration.work_snapshot(runtime.id, runtime.connection_epoch, now: later)

    second_fence = claim(second_run, runtime, later)

    {:ok, _} =
      Orchestration.transition(
        second_run.id,
        second_fence,
        "running",
        %{},
        "00000000-0000-0000-0000-000000000043",
        now: later
      )

    {:ok, _} =
      Orchestration.transition(
        second_run.id,
        second_fence,
        "waiting_for_input",
        %{},
        "00000000-0000-0000-0000-000000000044",
        now: later
      )

    assert {:ok, new_command, :created} =
             Orchestration.provide_input(task.id, %{"choice" => "new"}, "input-2", now: later)

    assert new_command.run_id == second_run.id
    assert old_command.run_id != new_command.run_id
  end

  test "task snapshots use the latest waiting event without mixing generations" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)

    {:ok, task, :created} =
      Orchestration.submit_task(task_attrs(), "waiting-generation", now: @now)

    {:ok, first_run} = Orchestration.assign_one(now: @now)
    first_fence = claim(first_run, runtime, @now)

    assert {:ok, _} =
             Orchestration.transition(
               first_run.id,
               first_fence,
               "running",
               %{},
               "00000000-0000-0000-0000-000000000141",
               now: @now
             )

    assert {:ok, _} =
             Orchestration.transition(
               first_run.id,
               first_fence,
               "waiting_for_input",
               %{"question" => "stale transition"},
               "00000000-0000-0000-0000-000000000142",
               now: @now
             )

    assert {:ok, [_]} =
             Orchestration.append_events(
               first_run.id,
               first_fence,
               [
                 %{
                   event_id: "00000000-0000-0000-0000-000000000143",
                   sequence: 1,
                   kind: "waiting_for_input",
                   payload: %{"question" => "stale event"},
                   occurred_at: @now
                 }
               ],
               now: @now
             )

    later = DateTime.shift(@now, second: 31)
    assert %{expired_runs: 1} = Orchestration.expire(now: later)

    assert {:ok, _} =
             Orchestration.heartbeat(runtime.id, runtime.connection_epoch, [], now: later)

    assert {:ok, second_run} = Orchestration.assign_one(now: later)
    second_fence = claim(second_run, runtime, later)

    assert {:ok, _} =
             Orchestration.transition(
               second_run.id,
               second_fence,
               "running",
               %{},
               "00000000-0000-0000-0000-000000000144",
               now: later
             )

    assert {:ok, _} =
             Orchestration.transition(
               second_run.id,
               second_fence,
               "waiting_for_input",
               %{"question" => "current transition"},
               "00000000-0000-0000-0000-000000000145",
               now: later
             )

    current_event_time = DateTime.add(later, 1, :microsecond)

    assert {:ok, [_]} =
             Orchestration.append_events(
               second_run.id,
               second_fence,
               [
                 %{
                   event_id: "00000000-0000-0000-0000-000000000146",
                   sequence: 1,
                   kind: "waiting_for_input",
                   payload: %{"question" => "current event"},
                   occurred_at: current_event_time
                 }
               ],
               now: current_event_time
             )

    assert {:ok, snapshot} = Orchestration.task_snapshot(task.id)
    assert snapshot.run.id == second_run.id
    assert snapshot.waiting.run_id == second_run.id
    assert snapshot.waiting.generation == second_run.generation
    assert snapshot.waiting.transition_id == "00000000-0000-0000-0000-000000000145"
    assert snapshot.waiting.question == "current event"
    assert snapshot.waiting.payload == %{"question" => "current event"}
    assert snapshot.waiting.recorded_at == current_event_time
  end

  defp enroll_machine do
    key = Ecto.UUID.generate()
    token = "machine-token-#{key}"

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(%{name: "builder", machine_token: token}, key,
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret"
             )

    enrolled
  end

  defp re_register_runtime(runtime, daemon_instance_id, current) do
    runtime.machine_id
    |> Orchestration.register_runtimes(
      daemon_instance_id,
      [
        %{
          runtime_key: runtime.runtime_key,
          name: runtime.name,
          capacity: runtime.capacity,
          agent_profile: runtime.agent_profile,
          workspace: runtime.workspace,
          capabilities: runtime.capabilities,
          heartbeat_interval_ms: runtime.heartbeat_interval_ms
        }
      ],
      now: current
    )
    |> case do
      {:ok, [reconnected]} -> {:ok, reconnected}
      error -> error
    end
  end

  defp terminal_grace_fixture(label, local_state \\ "running") do
    %{machine: machine} = enroll_machine()
    agent_profile = "terminal-grace-#{label}-#{System.unique_integer([:positive])}"
    runtime = register_runtime(machine, @now, agent_profile: agent_profile)

    assert {:ok, task, :created} =
             Orchestration.submit_task(
               task_attrs(agent_profile: agent_profile),
               "terminal-grace-#{label}-#{System.unique_integer([:positive])}",
               now: @now
             )

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime, @now)

    case local_state do
      "claimed" ->
        :ok

      "running" ->
        assert {:ok, %{state: "running"}} =
                 Orchestration.transition(
                   run.id,
                   fence,
                   "running",
                   %{},
                   Ecto.UUID.generate(),
                   now: @now
                 )

      "waiting_for_input" ->
        assert {:ok, %{state: "running"}} =
                 Orchestration.transition(
                   run.id,
                   fence,
                   "running",
                   %{},
                   Ecto.UUID.generate(),
                   now: @now
                 )

        assert {:ok, %{state: "waiting_for_input"}} =
                 Orchestration.transition(
                   run.id,
                   fence,
                   "waiting_for_input",
                   %{},
                   Ecto.UUID.generate(),
                   now: @now
                 )
    end

    {task, run, runtime, fence}
  end

  defp with_dedicated_orchestration_repo(test) do
    config =
      :symmetry_control
      |> Application.fetch_env!(Repo)
      |> Keyword.merge(name: nil, pool: DBConnection.ConnectionPool, pool_size: 3)

    {:ok, repo} = Repo.start_link(config)
    previous_dynamic_repo = Repo.put_dynamic_repo(repo)

    try do
      test.()
    after
      Repo.put_dynamic_repo(previous_dynamic_repo)
      GenServer.stop(repo)
    end
  end

  defp cleanup_dedicated_orchestration_fixture(fixture) do
    run_id = Ecto.UUID.dump!(fixture.run_id)
    task_id = Ecto.UUID.dump!(fixture.task_id)
    runtime_id = Ecto.UUID.dump!(fixture.runtime_id)
    machine_id = Ecto.UUID.dump!(fixture.machine_id)

    Repo.query!("DELETE FROM run_events WHERE run_id = $1", [run_id])
    Repo.query!("DELETE FROM run_transitions WHERE run_id = $1", [run_id])

    Repo.query!(
      "DELETE FROM commands WHERE run_id = $1 OR task_id = $2",
      [run_id, task_id]
    )

    Repo.query!("DELETE FROM runs WHERE id = $1", [run_id])
    Repo.query!("DELETE FROM tasks WHERE id = $1", [task_id])
    Repo.query!("DELETE FROM runtimes WHERE id = $1", [runtime_id])
    Repo.query!("DELETE FROM machines WHERE id = $1", [machine_id])
  end

  defp with_dynamic_repo(repo, fun) do
    previous_dynamic_repo = Repo.put_dynamic_repo(repo)

    try do
      fun.()
    after
      Repo.put_dynamic_repo(previous_dynamic_repo)
    end
  end

  defp assert_run_lock_timeout(run_id, operation) do
    parent = self()
    repo = Repo.get_dynamic_repo()

    lock_holder =
      Task.async(fn ->
        task_boundary(fn ->
          with_dynamic_repo(repo, fn ->
            Repo.transaction(fn ->
              Repo.one!(
                from run in SymmetryControl.Orchestration.Run,
                  where: run.id == ^run_id,
                  lock: "FOR UPDATE"
              )

              send(parent, {:run_lock_held, self()})

              receive do
                :release_run_lock -> :ok
              end
            end)
          end)
        end)
      end)

    requester =
      Task.async(fn ->
        task_boundary(fn ->
          receive do
            :start_run_lock_request ->
              with_dynamic_repo(repo, fn ->
                try do
                  Repo.transaction(fn ->
                    Repo.query!("SET LOCAL lock_timeout = '1s'")
                    send(parent, :run_lock_request_started)
                    operation.()
                  end)
                rescue
                  error in Postgrex.Error -> {:raised, error}
                end
              end)
          after
            15_000 -> {:task_timeout, :request_gate}
          end
        end)
      end)

    lock_holder_pid = lock_holder.pid
    state_key = make_ref()
    Process.put(state_key, %{holder_done: false, requester_done: false})

    try do
      assert_receive {:run_lock_held, ^lock_holder_pid}, 15_000
      send(requester.pid, :start_run_lock_request)
      assert_receive :run_lock_request_started, 15_000

      requester_result = Task.yield(requester, 15_000)
      if match?({:ok, _}, requester_result), do: mark_task_done(state_key, :requester_done)

      assert {:ok, {:raised, %Postgrex.Error{postgres: %{code: :lock_not_available}}}} =
               requester_result

      send(lock_holder.pid, :release_run_lock)
      holder_result = Task.yield(lock_holder, 15_000)
      if match?({:ok, _}, holder_result), do: mark_task_done(state_key, :holder_done)

      assert {:ok, {:ok, :ok}} = holder_result
    after
      state = Process.get(state_key, %{holder_done: false, requester_done: false})

      if !state.requester_done do
        drain_task(requester)
      end

      if !state.holder_done do
        send(lock_holder.pid, :release_run_lock)

        case drain_task(lock_holder) do
          {:completed, result} ->
            assert {:ok, :ok} = result

          {:terminated, _reason} ->
            :ok

          _unfinished ->
            :ok
        end
      end

      Process.delete(state_key)
    end
  end

  defp mark_task_done(state_key, task_key) do
    state = Process.get(state_key)
    Process.put(state_key, Map.put(state, task_key, true))
  end

  defp task_boundary(fun) do
    fun.()
  rescue
    error -> {:task_error, error}
  catch
    kind, reason -> {:task_exit, kind, reason}
  end

  defp drain_task(task) do
    case Task.yield(task, 1_000) do
      {:ok, result} ->
        {:completed, result}

      {:exit, reason} ->
        {:terminated, reason}

      nil ->
        case Task.shutdown(task, :brutal_kill) do
          {:ok, result} -> {:completed, result}
          _ -> :unfinished
        end
    end
  catch
    :exit, _reason -> :unfinished
  end
end
