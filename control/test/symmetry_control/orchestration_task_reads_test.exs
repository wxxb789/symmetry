defmodule SymmetryControl.OrchestrationTaskReadsTest do
  use SymmetryControl.OrchestrationCase, async: false

  alias Oban.Job
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Command
  alias SymmetryControl.Orchestration.RunEvent
  alias SymmetryControl.Orchestration.RunTransition
  alias SymmetryControl.RequestHash

  @now ~U[2026-09-02 00:00:00.000000Z]

  test "submission rejects an idempotency key with a different request" do
    assert {:ok, _task, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)
    assert {:ok, _task, :replayed} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)

    assert {:error, :idempotency_conflict} =
             Orchestration.submit_task(Map.put(task_attrs(), :goal, "another"), "task-1", now: @now)
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

  test "lease renewal never shortens an already longer durable lease" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now, capacity: 2)

    assert {:ok, _task, :created} =
             Orchestration.submit_task(task_attrs(goal: "monotonic-lease"), "monotonic-lease", now: @now)

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime, @now)
    existing_expiry = DateTime.shift(@now, second: 90)

    assert {:ok, loaded_run} = Orchestration.fetch_run(run.id)

    loaded_run
    |> Ecto.Changeset.change(lease_expires_at: existing_expiry)
    |> Repo.update!()

    assert {:ok, renewed} =
             Orchestration.renew_lease(run.id, fence,
               now: DateTime.shift(@now, second: 1),
               lease_duration_ms: 30_000
             )

    assert renewed.lease_expires_at == existing_expiry
  end

  test "claimed and waiting runs can deliver terminal results during grace before reaping" do
    for {local_state, target_state} <- [
          {"claimed", "completed"},
          {"claimed", "failed"},
          {"waiting_for_input", "completed"},
          {"waiting_for_input", "failed"}
        ] do
      {task, run, _runtime, fence} =
        terminal_grace_fixture("#{local_state}-#{target_state}", local_state)

      assert {:ok, %{state: ^target_state}} =
               Orchestration.transition(
                 run.id,
                 fence,
                 target_state,
                 %{"source_state" => local_state},
                 Ecto.UUID.generate(),
                 now: DateTime.shift(@now, second: 31)
               )

      assert {:ok, %{state: ^target_state}} = Orchestration.fetch_run(run.id)
      assert {:ok, %{state: ^target_state}} = Orchestration.fetch_task(task.id)
    end
  end

  test "an expired current generation becomes the visible attempt after a late terminal transition" do
    for target_state <- ["completed", "failed", "cancelled"] do
      {task, run, runtime, fence} = terminal_grace_fixture("expired-current-#{target_state}")
      expired_at = DateTime.shift(@now, second: 30)
      terminal_at = DateTime.shift(expired_at, second: 1)
      payload = %{"summary" => "late #{target_state} delivery"}

      assert %{expired_runs: 1} = Orchestration.expire(now: expired_at)
      assert {:ok, %{state: "expired"}} = Orchestration.fetch_run(run.id)

      assert {:ok, %{state: "queued", current_generation: 1, attempt_generation: 2}} =
               Orchestration.fetch_task(task.id)

      assert {:ok, %{state: ^target_state}} =
               Orchestration.transition(
                 run.id,
                 fence,
                 target_state,
                 payload,
                 Ecto.UUID.generate(),
                 now: terminal_at
               )

      assert {:ok, terminal_task} = Orchestration.fetch_task(task.id)
      assert terminal_task.state == target_state
      assert terminal_task.current_generation == run.generation
      assert terminal_task.attempt_generation == run.generation

      assert {:ok, %{task: snapshot_task, run: snapshot_run}} =
               Orchestration.task_snapshot(task.id)

      assert snapshot_task.id == task.id
      assert snapshot_run.id == run.id
      assert snapshot_run.state == target_state
      assert snapshot_run.generation == run.generation

      case target_state do
        "completed" ->
          assert snapshot_task.result == payload
          assert snapshot_task.failure == nil
          assert snapshot_run.result == payload
          assert snapshot_run.failure == nil

        "failed" ->
          assert snapshot_task.result == nil
          assert snapshot_task.failure == payload
          assert snapshot_run.result == nil
          assert snapshot_run.failure == payload

        "cancelled" ->
          assert snapshot_task.result == nil
          assert snapshot_task.failure == nil
          assert snapshot_run.result == nil
          assert snapshot_run.failure == nil
      end

      if target_state in ["failed", "cancelled"] do
        retry_attrs = %{
          goal: task.goal,
          agent_profile: task.agent_profile,
          workspace: task.workspace,
          input: task.input
        }

        assert {:ok, retried_task, _command, :created} =
                 Orchestration.retry_task(
                   task.id,
                   retry_attrs,
                   "late-terminal-retry-#{target_state}",
                   expected_generation: run.generation,
                   now: terminal_at
                 )

        assert retried_task.current_generation == run.generation
        assert retried_task.attempt_generation == run.generation + 1

        assert {:ok, _snapshot} =
                 Orchestration.heartbeat(runtime.id, runtime.connection_epoch, [], now: terminal_at)

        assert {:ok, retry_run} = Orchestration.assign_one(now: terminal_at)
        assert retry_run.task_id == task.id
        assert retry_run.generation == run.generation + 1
      end
    end
  end

  test "terminal grace cannot overwrite an operator cancellation after expiry" do
    for target_state <- ["completed", "failed", "cancelled"] do
      {task, run, _runtime, fence} = terminal_grace_fixture("cancel-wins-#{target_state}")
      expired_at = DateTime.shift(@now, second: 30)

      assert %{expired_runs: 1} = Orchestration.expire(now: expired_at)

      assert {:ok, %{state: "applied"}, :created} =
               Orchestration.create_command(
                 task.id,
                 "cancel",
                 %{},
                 "cancel-wins-#{target_state}",
                 now: expired_at
               )

      assert {:ok, %{state: "cancelled", current_generation: 1}} =
               Orchestration.fetch_task(task.id)

      assert {:error, :ownership_lost} =
               Orchestration.transition(
                 run.id,
                 fence,
                 target_state,
                 %{"summary" => "stale terminal delivery"},
                 Ecto.UUID.generate(),
                 now: DateTime.shift(expired_at, second: 1)
               )

      assert {:ok, %{state: "expired"}} = Orchestration.fetch_run(run.id)
      assert {:ok, %{state: "cancelled"}} = Orchestration.fetch_task(task.id)
    end
  end

  test "expiry requeues only the expired run and preserves other live work" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now, capacity: 2)

    {:ok, first_task, :created} =
      Orchestration.submit_task(task_attrs(goal: "first"), "task-1", now: @now)

    {:ok, second_task, :created} =
      Orchestration.submit_task(task_attrs(goal: "second"), "task-2", now: @now)

    {:ok, runs} = Orchestration.assign_all(now: @now)
    runs_by_task = Map.new(runs, &{&1.task_id, &1})
    first_run = Map.fetch!(runs_by_task, first_task.id)
    second_run = Map.fetch!(runs_by_task, second_task.id)
    first_fence = claim(first_run, runtime, @now)
    second_fence = claim(second_run, runtime, @now)
    refreshed_at = DateTime.shift(@now, second: 29)
    assert {:ok, _} = Orchestration.renew_lease(second_run.id, second_fence, now: refreshed_at)

    assert %{expired_runs: 1} = Orchestration.expire(now: DateTime.shift(@now, second: 31))
    assert {:ok, expired_task} = Orchestration.fetch_task(first_task.id)
    assert expired_task.state == "queued"
    assert {:ok, preserved_task} = Orchestration.fetch_task(second_task.id)
    assert preserved_task.state == "claimed"
    assert {:ok, preserved_run} = Orchestration.fetch_run(second_run.id)
    assert preserved_run.state == "claimed"

    assert {:error, :ownership_lost} =
             Orchestration.renew_lease(first_run.id, first_fence, now: refreshed_at)
  end

  test "runtime read models expose machine identity, capacity reservations, and active runs" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now, capacity: 5)

    tasks =
      for number <- 1..5 do
        assert {:ok, task, :created} =
                 Orchestration.submit_task(
                   task_attrs(goal: "runtime-#{number}"),
                   "runtime-#{number}",
                   now: DateTime.add(@now, number, :microsecond)
                 )

        task
      end

    assert {:ok, runs} = Orchestration.assign_all(now: DateTime.add(@now, 6, :microsecond))
    runs_by_task = Map.new(runs, &{&1.task_id, &1})
    [assigned_task, claimed_task, running_task, waiting_task, cancelling_task] = tasks

    claimed_run = Map.fetch!(runs_by_task, claimed_task.id)
    running_run = Map.fetch!(runs_by_task, running_task.id)
    waiting_run = Map.fetch!(runs_by_task, waiting_task.id)
    cancelling_run = Map.fetch!(runs_by_task, cancelling_task.id)

    _claimed_fence = claim(claimed_run, runtime, @now)
    running_fence = claim(running_run, runtime, @now)
    waiting_fence = claim(waiting_run, runtime, @now)
    _cancelling_fence = claim(cancelling_run, runtime, @now)

    assert {:ok, _} =
             Orchestration.transition(
               running_run.id,
               running_fence,
               "running",
               %{},
               "00000000-0000-0000-0000-000000000101",
               now: @now
             )

    assert {:ok, _} =
             Orchestration.transition(
               waiting_run.id,
               waiting_fence,
               "running",
               %{},
               "00000000-0000-0000-0000-000000000102",
               now: @now
             )

    assert {:ok, _} =
             Orchestration.transition(
               waiting_run.id,
               waiting_fence,
               "waiting_for_input",
               %{"question" => "Continue?"},
               "00000000-0000-0000-0000-000000000103",
               now: @now
             )

    assert {:ok, _task, _command} = Orchestration.request_cancel(cancelling_task.id, now: @now)

    assert {:ok, [snapshot]} = Orchestration.runtime_snapshots()

    assert snapshot.machine_id == machine.id
    assert snapshot.machine_name == machine.name
    assert snapshot.runtime_id == runtime.id
    assert snapshot.runtime_key == runtime.runtime_key
    assert snapshot.runtime_name == runtime.name
    assert snapshot.status == "online"
    assert snapshot.last_heartbeat_at == @now
    assert snapshot.connection_epoch == runtime.connection_epoch
    assert snapshot.capacity == 5
    assert snapshot.reserved_capacity == 5

    assert Enum.map(snapshot.active_runs, &{&1.inserted_at, &1.run_id}) ==
             Enum.sort(Enum.map(snapshot.active_runs, &{&1.inserted_at, &1.run_id}))

    assert Map.new(snapshot.active_runs, &{&1.task_id, &1.state}) == %{
             assigned_task.id => "assigned",
             claimed_task.id => "claimed",
             running_task.id => "running",
             waiting_task.id => "waiting_for_input",
             cancelling_task.id => "cancelling"
           }

    assert {:ok, ^snapshot} = Orchestration.runtime_snapshot(runtime.id)

    assert {:error, :not_found} =
             Orchestration.runtime_snapshot("00000000-0000-0000-0000-000000000199")
  end

  test "task snapshots expose the current waiting question and latest commands across command states" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "snapshot-waiting", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime, @now)

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "running",
               %{},
               "00000000-0000-0000-0000-000000000111",
               now: @now
             )

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "waiting_for_input",
               %{"question" => "What should happen next?", "choices" => ["continue"]},
               "00000000-0000-0000-0000-000000000112",
               now: @now
             )

    assert {:ok, fallback_snapshot} = Orchestration.task_snapshot(task.id)
    assert fallback_snapshot.waiting.transition_id == "00000000-0000-0000-0000-000000000112"
    assert fallback_snapshot.waiting.question == "What should happen next?"
    assert fallback_snapshot.waiting.recorded_at == @now

    batch_recorded_at = DateTime.add(@now, 1, :microsecond)

    assert {:ok, [_, _]} =
             Orchestration.append_events(
               run.id,
               fence,
               [
                 %{
                   event_id: "00000000-0000-0000-0000-000000000116",
                   sequence: 1,
                   kind: "waiting_for_input",
                   payload: %{"question" => "Which branch?", "choices" => ["main", "release"]},
                   occurred_at: batch_recorded_at
                 },
                 %{
                   event_id: "00000000-0000-0000-0000-000000000117",
                   sequence: 2,
                   kind: "waiting_for_input",
                   payload: %{"question" => "Which environment?", "choices" => ["staging"]},
                   occurred_at: DateTime.add(batch_recorded_at, 1, :microsecond)
                 }
               ],
               now: batch_recorded_at
             )

    assert {:ok, pending, :created} =
             Orchestration.create_command(
               task.id,
               "provide_input",
               %{"answer" => "continue"},
               "snapshot-pending",
               now: @now
             )

    assert {:ok, snapshot} = Orchestration.task_snapshot(task.id)
    assert snapshot.run.id == run.id
    assert snapshot.waiting.transition_id == "00000000-0000-0000-0000-000000000112"
    assert snapshot.waiting.question == "Which environment?"

    assert snapshot.waiting.payload == %{
             "question" => "Which environment?",
             "choices" => ["staging"]
           }

    assert snapshot.waiting.run_id == run.id
    assert snapshot.waiting.generation == run.generation
    assert snapshot.waiting.recorded_at == batch_recorded_at
    assert snapshot.latest_command.id == pending.id
    assert snapshot.latest_command.state == "pending"

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "running",
               %{},
               "00000000-0000-0000-0000-000000000113",
               now: DateTime.add(@now, 3, :microsecond)
             )

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "waiting_for_input",
               %{"question" => "Which option now?"},
               "00000000-0000-0000-0000-000000000115",
               now: DateTime.add(@now, 4, :microsecond)
             )

    assert {:ok, [_]} =
             Orchestration.append_events(
               run.id,
               fence,
               [
                 %{
                   event_id: "00000000-0000-0000-0000-000000000118",
                   sequence: 3,
                   kind: "waiting_for_input",
                   payload: %{"question" => "Which option now?"},
                   occurred_at: DateTime.add(@now, 5, :microsecond)
                 }
               ],
               now: DateTime.add(@now, 5, :microsecond)
             )

    assert {:ok, refreshed_waiting_snapshot} = Orchestration.task_snapshot(task.id)
    assert refreshed_waiting_snapshot.waiting.question == "Which option now?"

    assert refreshed_waiting_snapshot.waiting.transition_id ==
             "00000000-0000-0000-0000-000000000115"

    assert refreshed_waiting_snapshot.waiting.recorded_at == DateTime.add(@now, 5, :microsecond)
    assert refreshed_waiting_snapshot.latest_command.id == pending.id

    assert {:ok, _task, cancel} =
             Orchestration.request_cancel(task.id, now: DateTime.add(@now, 6, :microsecond))

    assert {:ok, acknowledged} =
             Orchestration.acknowledge_command(
               cancel.id,
               fence,
               "applied",
               "00000000-0000-0000-0000-000000000114",
               now: DateTime.add(@now, 7, :microsecond)
             )

    assert {:ok, acknowledged_snapshot} = Orchestration.task_snapshot(task.id)
    assert acknowledged_snapshot.waiting == nil
    assert acknowledged_snapshot.latest_command.id == acknowledged.id
    assert acknowledged_snapshot.latest_command.state == "acknowledged"

    {:ok, queued, :created} =
      Orchestration.submit_task(task_attrs(goal: "runless command"), "snapshot-runless", now: @now)

    assert {:ok, _cancelled, applied} = Orchestration.request_cancel(queued.id, now: @now)
    assert {:ok, applied_snapshot} = Orchestration.task_snapshot(queued.id)
    assert applied_snapshot.latest_command.id == applied.id
    assert applied_snapshot.latest_command.state == "applied"
    assert applied_snapshot.latest_command.run_id == nil
    assert applied_snapshot.latest_command.generation == nil
  end

  test "separate task history collections are ascending, task-scoped, and keyset paged" do
    %{task: task, first_run: first_run, second_run: second_run} = history_fixture()
    at = DateTime.shift(@now, second: 1)

    first = insert_history_event(first_run, "00000000-0000-0000-0000-000000000121", 1, at)

    second =
      insert_history_event(
        first_run,
        "00000000-0000-0000-0000-000000000122",
        2,
        DateTime.add(at, 1, :microsecond)
      )

    third =
      insert_history_event(
        second_run,
        "00000000-0000-0000-0000-000000000123",
        3,
        DateTime.add(at, 2, :microsecond)
      )

    _other = insert_history_event_for_new_task(at)

    assert {:ok, first_page} = Orchestration.list_task_events(task.id, limit: 2)
    assert Enum.map(first_page.entries, & &1.id) == [first.id, second.id]
    assert first_page.next_after == %{inserted_at: second.inserted_at, id: second.id}

    assert {:ok, second_page} =
             Orchestration.list_task_events(task.id, after: first_page.next_after, limit: 2)

    assert Enum.map(second_page.entries, & &1.id) == [third.id]
    assert second_page.next_after == nil

    assert Enum.map(first_page.entries ++ second_page.entries, & &1.id) == [
             first.id,
             second.id,
             third.id
           ]

    first_transition =
      insert_history_transition(
        first_run,
        "00000000-0000-0000-0000-000000000124",
        "running",
        DateTime.add(at, 3, :microsecond)
      )

    second_transition =
      insert_history_transition(
        second_run,
        "00000000-0000-0000-0000-000000000125",
        "waiting_for_input",
        DateTime.add(at, 4, :microsecond)
      )

    third_transition =
      insert_history_transition(
        first_run,
        "00000000-0000-0000-0000-000000000126",
        "running",
        DateTime.add(at, 5, :microsecond)
      )

    assert {:ok, first_transition_page} = Orchestration.list_task_transitions(task.id, limit: 2)

    assert Enum.map(first_transition_page.entries, & &1.id) == [
             first_transition.id,
             second_transition.id
           ]

    assert first_transition_page.next_after == %{
             inserted_at: second_transition.inserted_at,
             id: second_transition.id
           }

    assert {:ok, second_transition_page} =
             Orchestration.list_task_transitions(task.id,
               after: first_transition_page.next_after,
               limit: 2
             )

    assert Enum.map(second_transition_page.entries, & &1.id) == [third_transition.id]
    assert second_transition_page.next_after == nil

    assert Enum.map(first_transition_page.entries ++ second_transition_page.entries, & &1.id) == [
             first_transition.id,
             second_transition.id,
             third_transition.id
           ]

    first_command =
      insert_history_command(
        task,
        second_run,
        "00000000-0000-0000-0000-000000000127",
        "pending",
        DateTime.add(at, 6, :microsecond)
      )

    second_command =
      insert_history_command(
        task,
        first_run,
        "00000000-0000-0000-0000-000000000128",
        "pending",
        DateTime.add(at, 7, :microsecond)
      )

    third_command =
      insert_history_command(
        task,
        second_run,
        "00000000-0000-0000-0000-000000000129",
        "pending",
        DateTime.add(at, 8, :microsecond),
        "provide_input"
      )

    assert {:ok, first_command_page} = Orchestration.list_task_commands(task.id, limit: 2)

    assert Enum.map(first_command_page.entries, & &1.id) == [
             first_command.id,
             second_command.id
           ]

    assert first_command_page.next_after == %{
             inserted_at: second_command.inserted_at,
             id: second_command.id
           }

    assert {:ok, second_command_page} =
             Orchestration.list_task_commands(task.id,
               after: first_command_page.next_after,
               limit: 2
             )

    assert Enum.map(second_command_page.entries, & &1.id) == [third_command.id]
    assert second_command_page.next_after == nil

    assert Enum.map(first_command_page.entries ++ second_command_page.entries, & &1.id) == [
             first_command.id,
             second_command.id,
             third_command.id
           ]

    assert {:error, :not_found} =
             Orchestration.list_task_commands("00000000-0000-0000-0000-000000000129")
  end

  test "timeline spans generations, uses durable insertion order, and has stable source and id ties" do
    %{task: task, first_run: first_run, second_run: second_run} = history_fixture()
    first_at = DateTime.shift(@now, second: 1)
    same_at = DateTime.shift(@now, second: 2)

    oldest_event =
      insert_history_event(first_run, "00000000-0000-0000-0000-000000000131", 1, first_at)

    event = insert_history_event(second_run, "00000000-0000-0000-0000-000000000132", 2, same_at)

    transition =
      insert_history_transition(
        first_run,
        "00000000-0000-0000-0000-000000000133",
        "running",
        same_at
      )

    command =
      insert_history_command(
        task,
        second_run,
        "00000000-0000-0000-0000-000000000134",
        "pending",
        same_at
      )

    assert {:ok, first_page} = Orchestration.task_timeline(task.id, limit: 2)

    assert Enum.map(first_page.entries, &{&1.source, &1.id}) == [
             {"event", event.id},
             {"transition", transition.id}
           ]

    assert first_page.next_before == %{
             inserted_at: transition.inserted_at,
             source: "transition",
             id: transition.id
           }

    assert {:ok, second_page} =
             Orchestration.task_timeline(task.id, before: first_page.next_before, limit: 2)

    assert Enum.map(second_page.entries, &{&1.source, &1.id}) == [
             {"command", command.id},
             {"event", oldest_event.id}
           ]

    assert second_page.next_before == nil

    assert Enum.map(first_page.entries ++ second_page.entries, & &1.id) == [
             event.id,
             transition.id,
             command.id,
             oldest_event.id
           ]
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

  defp history_fixture do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now, capacity: 2)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "history-task", now: @now)
    {:ok, first_run} = Orchestration.assign_one(now: @now)

    assert %{expired_runs: 1} = Orchestration.expire(now: DateTime.shift(@now, second: 31))

    assert {:ok, _} =
             Orchestration.heartbeat(runtime.id, runtime.connection_epoch, [], now: DateTime.shift(@now, second: 31))

    assert {:ok, second_run} = Orchestration.assign_one(now: DateTime.shift(@now, second: 31))

    %{task: task, runtime: runtime, first_run: first_run, second_run: second_run}
  end

  defp insert_history_event(run, event_id, sequence, inserted_at) do
    %RunEvent{}
    |> RunEvent.changeset(%{
      run_id: run.id,
      event_id: event_id,
      request_hash: :crypto.hash(:sha256, event_id),
      sequence: sequence,
      kind: "progress",
      payload: %{"sequence" => sequence},
      occurred_at: DateTime.shift(inserted_at, second: -1)
    })
    |> Ecto.Changeset.change(inserted_at: inserted_at, updated_at: inserted_at)
    |> Repo.insert!()
  end

  defp insert_history_event_for_new_task(inserted_at) do
    %{machine: machine} = enroll_machine()
    _runtime = register_runtime(machine, @now)

    assert {:ok, _task, :created} =
             Orchestration.submit_task(task_attrs(goal: "other history"), "other-history-task", now: inserted_at)

    assert {:ok, run} = Orchestration.assign_one(now: inserted_at)
    insert_history_event(run, "00000000-0000-0000-0000-000000000128", 1, inserted_at)
  end

  defp insert_history_transition(run, transition_id, state, inserted_at) do
    %RunTransition{}
    |> RunTransition.changeset(%{
      run_id: run.id,
      transition_id: transition_id,
      request_hash: :crypto.hash(:sha256, transition_id),
      state: state,
      payload: %{"state" => state}
    })
    |> Ecto.Changeset.change(inserted_at: inserted_at, updated_at: inserted_at)
    |> Repo.insert!()
  end

  defp insert_history_command(task, run, idempotency_key, state, inserted_at, kind \\ "cancel") do
    %Command{}
    |> Command.changeset(%{
      task_id: task.id,
      run_id: run.id,
      generation: run.generation,
      kind: kind,
      payload: %{},
      idempotency_key: idempotency_key,
      request_hash: :crypto.hash(:sha256, idempotency_key),
      state: state
    })
    |> Ecto.Changeset.change(inserted_at: inserted_at, updated_at: inserted_at)
    |> Repo.insert!()
  end

  # The cases below came from the other base file's region and pin its clock;
  # the shadowing is intentional, so do not move a case across this line.
  @now ~U[2026-09-09 09:00:00.000000Z]

  test "legacy submit_task preserves its request hash and creates no Goal settlement" do
    attrs = %{
      goal: "Legacy task",
      agent_profile: "codex",
      workspace: "primary",
      input: %{}
    }

    assert {:ok, task, :created} =
             Orchestration.submit_task(attrs, Ecto.UUID.generate(), now: @now)

    assert task.goal_id == nil
    assert task.request_hash == RequestHash.legacy(Map.put(attrs, :required_capabilities, %{}))
    assert task.request_hash_version == 1

    assert [] ==
             Repo.all(
               from job in Job,
                 where:
                   job.worker == "SymmetryControl.Goals.Workers.SettleTaskWorker" and
                     fragment("? ->> 'task_id' = ?", job.args, ^task.id)
             )
  end
end
