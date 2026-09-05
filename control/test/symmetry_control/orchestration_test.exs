defmodule SymmetryControl.OrchestrationTest do
  use SymmetryControl.DataCase, async: true

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Machine
  alias SymmetryControl.Orchestration.{Command, RunEvent, RunTransition}

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

  test "machine enrollment replays the exact request and rejects key reuse" do
    key = "machine-enrollment-#{System.unique_integer([:positive])}"
    attrs = %{name: "builder", machine_token: "machine-token-#{key}"}

    opts = [
      enrollment_token: "enrollment-secret",
      expected_enrollment_token: "enrollment-secret",
      now: @now
    ]

    assert {:ok, first, :created} = Orchestration.enroll_machine(attrs, key, opts)
    assert {:ok, replayed, :replayed} = Orchestration.enroll_machine(attrs, key, opts)
    assert replayed.machine.id == first.machine.id
    assert replayed.token == first.token

    assert {:error, :idempotency_conflict} =
             Orchestration.enroll_machine(%{attrs | name: "other"}, key, opts)

    assert {:error, :idempotency_conflict} =
             Orchestration.enroll_machine(%{attrs | machine_token: "other-token"}, key, opts)
  end

  test "historical machine rows without replay metadata still authenticate and register" do
    token = "historical-machine-token"

    machine =
      %Machine{}
      |> Machine.changeset(%{name: "historical", token_digest: :crypto.hash(:sha256, token)})
      |> Repo.insert!()

    assert machine.enrollment_idempotency_key == nil
    assert machine.enrollment_request_hash == nil
    assert {:ok, ^machine} = Orchestration.authenticate_machine(token)

    assert {:ok, [_runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: "historical",
                   name: "Historical Runtime",
                   capacity: 1,
                   agent_profile: "codex",
                   workspace: "primary",
                   capabilities: %{}
                 }
               ],
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

  test "claim and renewal use configured lease duration unless explicitly overridden" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, capacity: 2)

    {:ok, _task, :created} =
      Orchestration.submit_task(task_attrs(), "configured-lease", now: @now)

    {:ok, run} = Orchestration.assign_one(now: @now)

    request = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: "00000000-0000-0000-0000-000000000013"
    }

    assert {:ok, claimed} = Orchestration.claim(run.id, request, now: @now)
    assert claimed.lease_expires_at == DateTime.add(@now, 30, :second)

    fence = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: claimed.claim_id,
      lease_token: claimed.lease_token
    }

    renewed_at = DateTime.add(@now, 1, :second)
    assert {:ok, renewed} = Orchestration.renew_lease(run.id, fence, now: renewed_at)
    assert renewed.lease_expires_at == DateTime.add(renewed_at, 30, :second)

    assert {:ok, overridden} =
             Orchestration.renew_lease(run.id, fence,
               now: DateTime.add(@now, 2, :second),
               lease_duration_ms: 45_000
             )

    assert overridden.lease_expires_at == DateTime.add(@now, 47, :second)

    {:ok, _task, :created} =
      Orchestration.submit_task(task_attrs(goal: "explicit-claim-lease"), "explicit-claim-lease",
        now: @now
      )

    {:ok, override_run} = Orchestration.assign_one(now: @now)

    override_request = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: override_run.generation,
      claim_id: "00000000-0000-0000-0000-000000000014"
    }

    for invalid_duration <- [0, -1, 29_999] do
      assert {:error, :invalid_request} =
               Orchestration.claim(override_run.id, override_request,
                 now: @now,
                 lease_duration_ms: invalid_duration
               )
    end

    assert {:ok, explicitly_claimed} =
             Orchestration.claim(override_run.id, override_request,
               now: @now,
               lease_duration_ms: 30_000
             )

    assert explicitly_claimed.lease_expires_at == DateTime.add(@now, 30, :second)

    override_fence = Map.put(override_request, :lease_token, explicitly_claimed.lease_token)

    for invalid_duration <- [0, -1, 29_999] do
      assert {:error, :invalid_request} =
               Orchestration.renew_lease(override_run.id, override_fence,
                 now: @now,
                 lease_duration_ms: invalid_duration
               )
    end
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

  test "terminal transitions accept the fixed eight minute grace boundary after lease expiry" do
    for {label, current, expected} <- [
          {:before_expiry, DateTime.add(@now, 29, :second), :ok},
          {:inside_grace, DateTime.add(@now, 509, :second), :ok},
          {:grace_boundary, DateTime.add(@now, 510, :second), :ok},
          {:outside_grace, DateTime.add(@now, 511, :second), :terminal_grace_expired}
        ] do
      %{machine: machine} = enroll_machine()
      agent_profile = "terminal-grace-#{label}"
      runtime = register_runtime(machine, agent_profile: agent_profile)

      assert {:ok, _task, :created} =
               Orchestration.submit_task(
                 task_attrs(agent_profile: agent_profile),
                 "terminal-grace-#{label}",
                 now: @now
               )

      assert {:ok, run} = Orchestration.assign_one(now: @now)
      fence = claim(run, runtime)

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
                 now: DateTime.add(@now, 31, :second)
               )

      assert {:ok, %{state: ^target_state}} = Orchestration.fetch_run(run.id)
      assert {:ok, %{state: ^target_state}} = Orchestration.fetch_task(task.id)
    end
  end

  test "an expired current generation becomes the visible attempt after a late terminal transition" do
    for target_state <- ["completed", "failed", "cancelled"] do
      {task, run, runtime, fence} = terminal_grace_fixture("expired-current-#{target_state}")
      expired_at = DateTime.add(@now, 30, :second)
      terminal_at = DateTime.add(expired_at, 1, :second)
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
                 Orchestration.heartbeat(runtime.id, runtime.connection_epoch, [],
                   now: terminal_at
                 )

        assert {:ok, retry_run} = Orchestration.assign_one(now: terminal_at)
        assert retry_run.task_id == task.id
        assert retry_run.generation == run.generation + 1
      end
    end
  end

  test "terminal grace cannot overwrite an operator cancellation after expiry" do
    for target_state <- ["completed", "failed", "cancelled"] do
      {task, run, _runtime, fence} = terminal_grace_fixture("cancel-wins-#{target_state}")
      expired_at = DateTime.add(@now, 30, :second)

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
                 now: DateTime.add(expired_at, 1, :second)
               )

      assert {:ok, %{state: "expired"}} = Orchestration.fetch_run(run.id)
      assert {:ok, %{state: "cancelled"}} = Orchestration.fetch_task(task.id)
    end
  end

  test "acknowledgements honor terminal grace after reaper expiry" do
    {expired_task, expired_run, _runtime, expired_fence} = terminal_grace_fixture("expired-ack")

    assert {:ok, %{state: "waiting_for_input"}} =
             Orchestration.transition(
               expired_run.id,
               expired_fence,
               "waiting_for_input",
               %{},
               Ecto.UUID.generate(),
               now: @now
             )

    assert {:ok, expired_command, :created} =
             Orchestration.create_command(
               expired_task.id,
               "provide_input",
               %{"answer" => "yes"},
               "expired-ack-command",
               now: @now
             )

    expired_at = DateTime.add(@now, 30, :second)
    grace_boundary = DateTime.add(@now, 510, :second)
    after_grace = DateTime.add(grace_boundary, 1, :second)

    assert %{expired_runs: 1} = Orchestration.expire(now: expired_at)
    assert {:ok, %{state: "expired"}} = Orchestration.fetch_run(expired_run.id)

    expired_acknowledgement_id = Ecto.UUID.generate()

    assert {:ok, %{state: "acknowledged"}} =
             Orchestration.acknowledge_command(
               expired_command.id,
               expired_fence,
               "applied",
               expired_acknowledgement_id,
               now: grace_boundary
             )

    assert {:error, :idempotency_conflict} =
             Orchestration.acknowledge_command(
               expired_command.id,
               expired_fence,
               "failed",
               expired_acknowledgement_id,
               now: after_grace
             )

    {cancel_task, cancel_run, _runtime, cancel_fence} = terminal_grace_fixture("cancelled-ack")

    assert {:ok, cancel_command, :created} =
             Orchestration.create_command(
               cancel_task.id,
               "cancel",
               %{},
               "cancelled-ack-command",
               now: @now
             )

    assert %{expired_runs: 1} = Orchestration.expire(now: expired_at)
    assert {:ok, %{state: "cancelled"}} = Orchestration.fetch_run(cancel_run.id)

    cancel_acknowledgement_id = Ecto.UUID.generate()

    assert {:ok, %{state: "acknowledged"}} =
             Orchestration.acknowledge_command(
               cancel_command.id,
               cancel_fence,
               "applied",
               cancel_acknowledgement_id,
               now: grace_boundary
             )

    assert {:error, :idempotency_conflict} =
             Orchestration.acknowledge_command(
               cancel_command.id,
               cancel_fence,
               "applied",
               Ecto.UUID.generate(),
               now: after_grace
             )
  end

  test "a new acknowledgement after terminal grace expires" do
    {task, _run, _runtime, fence} =
      terminal_grace_fixture("unacknowledged-grace", "waiting_for_input")

    assert {:ok, command, :created} =
             Orchestration.create_command(
               task.id,
               "provide_input",
               %{"answer" => "yes"},
               "unacknowledged-grace-command",
               now: @now
             )

    assert %{expired_runs: 1} = Orchestration.expire(now: DateTime.add(@now, 30, :second))

    assert {:error, :terminal_grace_expired} =
             Orchestration.acknowledge_command(
               command.id,
               fence,
               "applied",
               Ecto.UUID.generate(),
               now: DateTime.add(@now, 511, :second)
             )
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
             re_register_runtime(runtime, Ecto.UUID.generate(), DateTime.add(@now, 1, :second))

    assert reconnected_runtime.id == runtime.id
    assert reconnected_runtime.connection_epoch == runtime.connection_epoch + 1

    assert {:error, :ownership_lost} =
             Orchestration.renew_lease(run.id, fence, now: DateTime.add(@now, 1, :second))

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
               now: DateTime.add(@now, 1, :second)
             )

    assert {:error, :ownership_lost} =
             Orchestration.transition(
               run.id,
               fence,
               "waiting_for_input",
               %{},
               Ecto.UUID.generate(),
               now: DateTime.add(@now, 1, :second)
             )

    expired_at = DateTime.add(@now, 30, :second)
    terminal_at = DateTime.add(expired_at, 1, :second)
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
               now: DateTime.add(terminal_at, 1, :second)
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
               now: DateTime.add(terminal_at, 1, :second)
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
             re_register_runtime(runtime, Ecto.UUID.generate(), DateTime.add(@now, 1, :second))

    expired_at = DateTime.add(@now, 30, :second)

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
               now: DateTime.add(expired_at, 1, :second)
             )

    assert {:error, :ownership_lost} =
             Orchestration.acknowledge_command(
               command.id,
               fence,
               "applied",
               Ecto.UUID.generate(),
               now: DateTime.add(expired_at, 1, :second)
             )

    assert {:ok, %{state: "assigned", current_generation: 2}} =
             Orchestration.fetch_task(task.id)
  end

  test "a replacement generation fences an expired generation's terminal delivery" do
    {task, run, runtime, fence} = terminal_grace_fixture("replacement-generation")
    expired_at = DateTime.add(@now, 30, :second)

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
               now: DateTime.add(expired_at, 1, :second)
             )

    assert {:ok, %{current_generation: generation}} = Orchestration.fetch_task(task.id)
    assert generation == replacement.generation
  end

  test "terminal transition replays and acknowledgement conflicts survive grace" do
    {_task, run, _runtime, fence} = terminal_grace_fixture("terminal-replay")
    inside_grace = DateTime.add(@now, 509, :second)
    outside_grace = DateTime.add(@now, 511, :second)
    transition_id = Ecto.UUID.generate()
    transition_payload = %{"summary" => "completed after expiry"}

    assert {:ok, %{state: "completed"}} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               transition_payload,
               transition_id,
               now: inside_grace
             )

    assert {:ok, %{state: "completed", result: ^transition_payload}} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               transition_payload,
               transition_id,
               now: outside_grace
             )

    assert {:error, :terminal_grace_expired} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               transition_payload,
               Ecto.UUID.generate(),
               now: outside_grace
             )

    {ack_task, ack_run, _runtime, ack_fence} = terminal_grace_fixture("ack-replay")

    assert {:ok, command, :created} =
             Orchestration.create_command(ack_task.id, "cancel", %{}, "ack-replay-command",
               now: @now
             )

    acknowledgement_id = Ecto.UUID.generate()

    assert {:ok, %{state: "acknowledged"}} =
             Orchestration.acknowledge_command(
               command.id,
               ack_fence,
               "applied",
               acknowledgement_id,
               now: inside_grace
             )

    assert {:ok, %{state: "acknowledged", acknowledgement_id: ^acknowledgement_id}} =
             Orchestration.acknowledge_command(
               command.id,
               ack_fence,
               "applied",
               acknowledgement_id,
               now: outside_grace
             )

    assert {:error, :idempotency_conflict} =
             Orchestration.acknowledge_command(
               command.id,
               ack_fence,
               "applied",
               Ecto.UUID.generate(),
               now: outside_grace
             )

    assert {:ok, %{state: "cancelling"}} = Orchestration.fetch_run(ack_run.id)
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

  test "input is bound to a waiting generation and late terminal delivery preserves unrelated work" do
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

  test "runtime read models expose machine identity, capacity reservations, and active runs" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, capacity: 5)

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

    _claimed_fence = claim(claimed_run, runtime)
    running_fence = claim(running_run, runtime)
    waiting_fence = claim(waiting_run, runtime)
    _cancelling_fence = claim(cancelling_run, runtime)

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
    runtime = register_runtime(machine)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "snapshot-waiting", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

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
      Orchestration.submit_task(task_attrs(goal: "runless command"), "snapshot-runless",
        now: @now
      )

    assert {:ok, _cancelled, applied} = Orchestration.request_cancel(queued.id, now: @now)
    assert {:ok, applied_snapshot} = Orchestration.task_snapshot(queued.id)
    assert applied_snapshot.latest_command.id == applied.id
    assert applied_snapshot.latest_command.state == "applied"
    assert applied_snapshot.latest_command.run_id == nil
    assert applied_snapshot.latest_command.generation == nil
  end

  test "separate task history collections are ascending, task-scoped, and keyset paged" do
    %{task: task, first_run: first_run, second_run: second_run} = history_fixture()
    at = DateTime.add(@now, 1, :second)

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

  test "task snapshots use the latest waiting event without mixing generations" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)

    {:ok, task, :created} =
      Orchestration.submit_task(task_attrs(), "waiting-generation", now: @now)

    {:ok, first_run} = Orchestration.assign_one(now: @now)
    first_fence = claim(first_run, runtime)

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

    later = DateTime.add(@now, 31, :second)
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

  test "timeline spans generations, uses durable insertion order, and has stable source and id ties" do
    %{task: task, first_run: first_run, second_run: second_run} = history_fixture()
    first_at = DateTime.add(@now, 1, :second)
    same_at = DateTime.add(@now, 2, :second)

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
                   agent_profile: Keyword.get(overrides, :agent_profile, "codex"),
                   workspace: "primary",
                   capabilities: %{}
                 }
               ],
               now: @now
             )

    runtime
  end

  defp re_register_runtime(runtime, daemon_instance_id, current) do
    Orchestration.register_runtimes(
      runtime.machine_id,
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

  defp terminal_grace_fixture(label, local_state \\ "running") do
    %{machine: machine} = enroll_machine()
    agent_profile = "terminal-grace-#{label}-#{System.unique_integer([:positive])}"
    runtime = register_runtime(machine, agent_profile: agent_profile)

    assert {:ok, task, :created} =
             Orchestration.submit_task(
               task_attrs(agent_profile: agent_profile),
               "terminal-grace-#{label}-#{System.unique_integer([:positive])}",
               now: @now
             )

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

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
    runtime = register_runtime(machine, capacity: 2)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "history-task", now: @now)
    {:ok, first_run} = Orchestration.assign_one(now: @now)

    assert %{expired_runs: 1} = Orchestration.expire(now: DateTime.add(@now, 31, :second))

    assert {:ok, _} =
             Orchestration.heartbeat(runtime.id, runtime.connection_epoch, [],
               now: DateTime.add(@now, 31, :second)
             )

    assert {:ok, second_run} = Orchestration.assign_one(now: DateTime.add(@now, 31, :second))

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
      occurred_at: DateTime.add(inserted_at, -1, :second)
    })
    |> Ecto.Changeset.change(inserted_at: inserted_at, updated_at: inserted_at)
    |> Repo.insert!()
  end

  defp insert_history_event_for_new_task(inserted_at) do
    %{machine: machine} = enroll_machine()
    _runtime = register_runtime(machine)

    assert {:ok, _task, :created} =
             Orchestration.submit_task(task_attrs(goal: "other history"), "other-history-task",
               now: inserted_at
             )

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
end
