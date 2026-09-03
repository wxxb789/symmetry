defmodule SymmetryControl.OrchestrationTest do
  use SymmetryControl.DataCase, async: true

  alias SymmetryControl.Orchestration

  @now ~U[2026-09-02 00:00:00.000000Z]

  test "enrolls, registers, assigns, claims, runs, and completes work" do
    %{machine: machine, token: token} = enroll_machine()
    runtime = register_runtime(machine)

    assert {:ok, ^machine} = Orchestration.authenticate_machine(token)
    assert {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == task.id
    assert run.generation == 1

    fence = claim(run, runtime)

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

  test "submission rejects an idempotency key with a different request" do
    assert {:ok, _task, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)
    assert {:ok, _task, :replayed} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)

    assert {:error, :idempotency_conflict} =
             Orchestration.submit_task(Map.put(task_attrs(), :goal, "another"), "task-1",
               now: @now
             )
  end

  test "submission preserves omitted input separately from explicit empty input" do
    omitted = %{goal: "Omitted input", agent_profile: "codex", workspace: "primary"}

    assert {:ok, omitted_task, :created} =
             Orchestration.submit_task(omitted, "omitted-input", now: @now)

    assert omitted_task.input == nil
    assert {:ok, fetched_omitted} = Orchestration.fetch_task(omitted_task.id)
    assert fetched_omitted.input == nil

    assert {:error, :idempotency_conflict} =
             Orchestration.submit_task(Map.put(omitted, :input, %{}), "omitted-input", now: @now)

    assert {:ok, explicit_empty_task, :created} =
             Orchestration.submit_task(task_attrs(), "explicit-empty-input", now: @now)

    assert explicit_empty_task.input == %{}
    assert {:ok, fetched_explicit_empty} = Orchestration.fetch_task(explicit_empty_task.id)
    assert fetched_explicit_empty.input == %{}
  end

  test "same daemon instance replays epoch and a replacement increments it" do
    %{machine: machine} = enroll_machine()
    first = register_runtime(machine, daemon_instance_id: "00000000-0000-0000-0000-000000000001")

    replayed =
      register_runtime(machine, daemon_instance_id: "00000000-0000-0000-0000-000000000001")

    replacement =
      register_runtime(machine, daemon_instance_id: "00000000-0000-0000-0000-000000000002")

    assert replayed.connection_epoch == first.connection_epoch
    assert replacement.connection_epoch == first.connection_epoch + 1
  end

  test "capacity one cannot be over-assigned" do
    %{machine: machine} = enroll_machine()
    register_runtime(machine, capacity: 1)

    assert {:ok, _task, :created} =
             Orchestration.submit_task(task_attrs(goal: "one"), "task-1", now: @now)

    assert {:ok, _task, :created} =
             Orchestration.submit_task(task_attrs(goal: "two"), "task-2", now: @now)

    assert {:ok, _run} = Orchestration.assign_one(now: @now)
    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
  end

  test "claim replays one claim id and rejects a different claimant" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    {:ok, _task, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    claim_id = "00000000-0000-0000-0000-000000000011"

    request = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: claim_id
    }

    assert {:ok, first} = Orchestration.claim(run.id, request, now: @now)
    assert {:ok, replayed} = Orchestration.claim(run.id, request, now: @now)
    assert replayed.lease_token == first.lease_token

    assert {:error, :ownership_lost} =
             Orchestration.claim(run.id, request, now: DateTime.add(@now, 31, :second))

    assert {:error, :ownership_lost} =
             Orchestration.claim(
               run.id,
               %{request | claim_id: "00000000-0000-0000-0000-000000000012"},
               now: @now
             )
  end

  test "fences reject stale epoch, generation, and lease" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    {:ok, _task, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

    assert {:error, :ownership_lost} =
             Orchestration.renew_lease(run.id, %{fence | runtime_epoch: fence.runtime_epoch + 1},
               now: @now
             )

    assert {:error, :ownership_lost} =
             Orchestration.renew_lease(run.id, %{fence | generation: fence.generation + 1},
               now: @now
             )

    assert {:error, :ownership_lost} =
             Orchestration.renew_lease(
               run.id,
               %{fence | lease_token: "00000000-0000-0000-0000-000000000099"},
               now: @now
             )
  end

  test "events and transitions are replay-safe and detect changed bodies" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    {:ok, _task, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

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
  end

  test "events reject NUL in nested JSONB map keys and string values before persistence" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    {:ok, _task, :created} = Orchestration.submit_task(task_attrs(), "nul-event", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

    for event <- [
          %{
            event_id: "00000000-0000-0000-0000-000000000051",
            sequence: 1,
            kind: "progress",
            payload: %{"nested" => [%{"message" => "bad\0value"}]},
            occurred_at: @now
          },
          %{
            event_id: "00000000-0000-0000-0000-000000000052",
            sequence: 2,
            kind: "progress",
            payload: %{"bad\0key" => true},
            occurred_at: @now
          }
        ] do
      assert {:error, :invalid_request} =
               Orchestration.append_events(run.id, fence, [event], now: @now)
    end
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

    runtime = register_runtime(machine)

    assert {:ok, task, :created} =
             Orchestration.submit_task(task_attrs(), "nul-command", now: @now)

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

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
    runtime = register_runtime(machine)

    {:ok, _task, :created} =
      Orchestration.submit_task(task_attrs(), "transition-errors", now: @now)

    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

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
               "completed",
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

  test "cancellation handles queued work and serializes with terminal completion" do
    {:ok, queued, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)

    assert {:ok, cancelled, queued_command} = Orchestration.request_cancel(queued.id, now: @now)

    assert cancelled.state == "cancelled"
    assert queued_command.task_id == queued.id
    assert queued_command.run_id == nil
    assert queued_command.generation == nil
    assert queued_command.state == "applied"
    assert queued_command.applied_at == @now

    assert {:ok, ^cancelled, replayed_queued_command} =
             Orchestration.request_cancel(queued.id, now: @now)

    assert replayed_queued_command.id == queued_command.id

    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)

    {:ok, task, :created} =
      Orchestration.submit_task(task_attrs(goal: "live"), "task-2", now: @now)

    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

    {:ok, _} =
      Orchestration.transition(
        run.id,
        fence,
        "running",
        %{},
        "00000000-0000-0000-0000-000000000034",
        now: @now
      )

    assert {:ok, cancelling, command} = Orchestration.request_cancel(task.id, now: @now)
    assert cancelling.state == "cancelling"
    assert command.kind == "cancel"

    assert {:ok, acknowledged} =
             Orchestration.acknowledge_command(
               command.id,
               fence,
               "applied",
               "00000000-0000-0000-0000-000000000045",
               now: @now
             )

    assert acknowledged.acknowledgement_outcome == "applied"

    assert {:ok, replayed_acknowledgement} =
             Orchestration.acknowledge_command(
               command.id,
               fence,
               "applied",
               "00000000-0000-0000-0000-000000000045",
               now: @now
             )

    assert replayed_acknowledgement.id == command.id

    assert {:error, :invalid_transition} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               %{},
               "00000000-0000-0000-0000-000000000035",
               now: @now
             )

    assert {:ok, same, same_command} = Orchestration.request_cancel(task.id, now: @now)
    assert same.state == "cancelling"
    assert same_command.id == command.id

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "cancelled",
               %{},
               "00000000-0000-0000-0000-000000000046",
               now: @now
             )

    assert {:ok, terminal_cancelled, retained_command} =
             Orchestration.request_cancel(task.id, now: @now)

    assert terminal_cancelled.state == "cancelled"
    assert retained_command.id == command.id
  end

  test "task commands scope idempotency to task and replay before later state validation" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)

    {:ok, task, :created} =
      Orchestration.submit_task(task_attrs(), "command-idempotency", now: @now)

    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

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

  test "provide_input requires a current waiting generation" do
    {:ok, queued_task, :created} =
      Orchestration.submit_task(task_attrs(), "input-without-waiting-run", now: @now)

    assert {:error, :state_conflict} =
             Orchestration.create_command(
               queued_task.id,
               "provide_input",
               %{"answer" => "no run"},
               "input-without-waiting-run",
               now: @now
             )
  end

  test "input is bound to a waiting generation and expiry requeues without harming unrelated work" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)

    {:ok, unaffected, :created} =
      Orchestration.submit_task(task_attrs(goal: "unrelated"), "task-2",
        now: DateTime.add(@now, 1, :microsecond)
      )

    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

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

    later = DateTime.add(@now, 31, :second)
    assert %{expired_runs: 1} = Orchestration.expire(now: later)

    assert {:error, :ownership_lost} =
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

    assert {:ok, requeued} = Orchestration.fetch_task(task.id)
    assert requeued.state == "queued"
    assert {:ok, still_unaffected} = Orchestration.fetch_task(unaffected.id)
    assert still_unaffected.state == "queued"
  end

  test "completion committed first makes cancellation return the terminal task" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

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
    runtime = register_runtime(machine)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)
    {:ok, first_run} = Orchestration.assign_one(now: @now)
    first_fence = claim(first_run, runtime)

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

    later = DateTime.add(@now, 31, :second)
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

  test "expiry requeues only the expired run and preserves other live work" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, capacity: 2)

    {:ok, first_task, :created} =
      Orchestration.submit_task(task_attrs(goal: "first"), "task-1", now: @now)

    {:ok, second_task, :created} =
      Orchestration.submit_task(task_attrs(goal: "second"), "task-2", now: @now)

    {:ok, runs} = Orchestration.assign_all(now: @now)
    runs_by_task = Map.new(runs, &{&1.task_id, &1})
    first_run = Map.fetch!(runs_by_task, first_task.id)
    second_run = Map.fetch!(runs_by_task, second_task.id)
    first_fence = claim(first_run, runtime)
    second_fence = claim(second_run, runtime)
    refreshed_at = DateTime.add(@now, 29, :second)
    assert {:ok, _} = Orchestration.renew_lease(second_run.id, second_fence, now: refreshed_at)

    assert %{expired_runs: 1} = Orchestration.expire(now: DateTime.add(@now, 31, :second))
    assert {:ok, expired_task} = Orchestration.fetch_task(first_task.id)
    assert expired_task.state == "queued"
    assert {:ok, preserved_task} = Orchestration.fetch_task(second_task.id)
    assert preserved_task.state == "claimed"
    assert {:ok, preserved_run} = Orchestration.fetch_run(second_run.id)
    assert preserved_run.state == "claimed"

    assert {:error, :ownership_lost} =
             Orchestration.renew_lease(first_run.id, first_fence, now: refreshed_at)
  end

  defp enroll_machine do
    assert {:ok, enrolled} =
             Orchestration.enroll_machine(%{name: "builder"},
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret"
             )

    enrolled
  end

  defp register_runtime(machine, overrides \\ []) do
    daemon_instance_id =
      Keyword.get(overrides, :daemon_instance_id, "00000000-0000-0000-0000-000000000001")

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               daemon_instance_id,
               [
                 %{
                   runtime_key: "default",
                   name: "Local Codex",
                   capacity: Keyword.get(overrides, :capacity, 1),
                   agent_profile: "codex",
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

  defp claim(run, runtime, current \\ @now) do
    assert {:ok, claimed} =
             Orchestration.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: "00000000-0000-0000-0000-000000000010"
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
end
