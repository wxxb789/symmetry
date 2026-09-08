defmodule SymmetryControl.OrchestrationRequestHashTest do
  use SymmetryControl.DataCase, async: false

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{RunEvent, RunTransition, Task}
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash

  setup do
    previous = Application.fetch_env!(:symmetry_control, :orchestration)

    on_exit(fn -> Application.put_env(:symmetry_control, :orchestration, previous) end)
    :ok
  end

  test "default writes preserve legacy replay compatibility and force Machine and Task insert_all versions" do
    use_hash_write_mode(:legacy)
    machine_token = Ecto.UUID.generate()
    machine_key = Ecto.UUID.generate()
    task_key = Ecto.UUID.generate()

    assert {:ok, %{machine: machine}, :created} =
             Orchestration.enroll_machine(
               %{name: "legacy-write-machine", machine_token: machine_token},
               machine_key,
               enrollment_token: "test-enrollment-token",
               expected_enrollment_token: "test-enrollment-token"
             )

    assert machine.enrollment_request_hash_version == 1

    assert machine.enrollment_request_hash ==
             RequestHash.legacy(%{name: "legacy-write-machine", machine_token: machine_token})

    attrs = %{
      goal: "Legacy-compatible task",
      agent_profile: "codex",
      workspace: "primary",
      input: %{}
    }

    assert {:ok, task, :created} = Orchestration.submit_task(attrs, task_key)
    assert task.request_hash_version == 1
    assert task.request_hash == RequestHash.legacy(Map.put(attrs, :required_capabilities, %{}))

    assert {:ok, ^task, :replayed} = Orchestration.submit_task(attrs, task_key)
  end

  test "canonical cutover writes version 2 standard hashes and version 3 command hashes" do
    use_hash_write_mode(:canonical)
    machine_token = Ecto.UUID.generate()

    assert {:ok, %{machine: machine}, :created} =
             Orchestration.enroll_machine(
               %{name: "canonical-write-machine", machine_token: machine_token},
               Ecto.UUID.generate(),
               enrollment_token: "test-enrollment-token",
               expected_enrollment_token: "test-enrollment-token"
             )

    assert machine.enrollment_request_hash_version == 2

    attrs = %{goal: "Canonical task", agent_profile: "codex", workspace: "primary", input: %{}}

    assert {:ok, task, :created} = Orchestration.submit_task(attrs, Ecto.UUID.generate())
    assert task.request_hash_version == 2

    assert {:ok, _cancelled_task, command} = Orchestration.request_cancel(task.id)
    assert command.request_hash_version == 3
    assert command.request_hash == RequestHash.canonical(%{kind: "cancel", payload: %{}})

    assert {:ok, _cancelled_task, replayed_command} = Orchestration.request_cancel(task.id)
    assert replayed_command.id == command.id
  end

  test "replays a legacy task hash and rejects a different task body" do
    key = Ecto.UUID.generate()
    attrs = %{goal: "Legacy task", agent_profile: "codex", workspace: "primary", input: %{}}

    %Task{}
    |> Task.changeset(%{
      idempotency_key: key,
      request_hash: RequestHash.legacy(Map.put(attrs, :required_capabilities, %{})),
      request_hash_version: 1,
      goal: attrs.goal,
      agent_profile: attrs.agent_profile,
      workspace: attrs.workspace,
      input: attrs.input,
      required_capabilities: %{},
      state: "queued",
      current_generation: 0,
      attempt_generation: 1
    })
    |> Repo.insert!()

    assert {:ok, %{idempotency_key: ^key}, :replayed} = Orchestration.submit_task(attrs, key)

    assert {:error, :idempotency_conflict} =
             Orchestration.submit_task(%{attrs | goal: "Different task"}, key)
  end

  test "rejects task inputs that cannot be canonically hashed" do
    attrs = %{goal: "Canonical task", agent_profile: "codex", workspace: "primary"}

    for input <- [%{due_date: ~D[2026-09-07]}, %{1 => "unsupported key"}] do
      assert {:error, :invalid_request} =
               Orchestration.submit_task(Map.put(attrs, :input, input), Ecto.UUID.generate())
    end
  end

  test "replays legacy event and transition hashes" do
    {run, fence} = claimed_run()
    occurred_at = ~U[2026-09-07 00:00:00.000000Z]

    event = %{
      event_id: Ecto.UUID.generate(),
      sequence: 1,
      kind: "agent_event",
      payload: %{"value" => "legacy"},
      occurred_at: occurred_at
    }

    assert {:ok, [_]} = Orchestration.append_events(run.id, fence, [event], now: occurred_at)

    event_body = %{
      sequence: event.sequence,
      kind: event.kind,
      payload: event.payload,
      occurred_at: event.occurred_at
    }

    stored_event = Repo.get_by!(RunEvent, run_id: run.id, event_id: event.event_id)
    assert stored_event.request_hash_version == 1
    assert stored_event.request_hash == RequestHash.legacy(event_body)

    stored_event
    |> Ecto.Changeset.change(
      request_hash: RequestHash.legacy(event_body),
      request_hash_version: 1
    )
    |> Repo.update!()

    assert {:ok, [_]} = Orchestration.append_events(run.id, fence, [event], now: occurred_at)

    transition_id = Ecto.UUID.generate()

    assert {:ok, _} =
             Orchestration.transition(run.id, fence, "running", %{}, transition_id,
               now: occurred_at
             )

    stored_transition = Repo.get_by!(RunTransition, run_id: run.id, transition_id: transition_id)
    assert stored_transition.request_hash_version == 1
    assert stored_transition.request_hash == RequestHash.legacy(%{state: "running", payload: %{}})

    stored_transition
    |> Ecto.Changeset.change(
      request_hash: RequestHash.legacy(%{state: "running", payload: %{}}),
      request_hash_version: 1
    )
    |> Repo.update!()

    assert {:ok, _} =
             Orchestration.transition(run.id, fence, "running", %{}, transition_id,
               now: occurred_at
             )
  end

  defp claimed_run do
    machine_token = Ecto.UUID.generate()

    assert {:ok, %{machine: machine}, :created} =
             Orchestration.enroll_machine(
               %{name: "hash-machine-#{machine_token}", machine_token: machine_token},
               machine_token,
               enrollment_token: "test-enrollment-token",
               expected_enrollment_token: "test-enrollment-token"
             )

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: "hash-runtime-#{machine_token}",
                   name: "Hash Runtime",
                   capacity: 1,
                   agent_profile: "codex",
                   workspace: "primary",
                   capabilities: %{}
                 }
               ],
               now: ~U[2026-09-07 00:00:00.000000Z]
             )

    assert {:ok, _task, :created} =
             Orchestration.submit_task(
               %{goal: "Legacy replay", agent_profile: "codex", workspace: "primary", input: %{}},
               Ecto.UUID.generate(),
               now: ~U[2026-09-07 00:00:00.000000Z]
             )

    assert {:ok, run} = Orchestration.assign_one(now: ~U[2026-09-07 00:00:00.000000Z])
    claim_id = Ecto.UUID.generate()

    assert {:ok, claimed} =
             Orchestration.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: claim_id
               },
               now: ~U[2026-09-07 00:00:00.000000Z]
             )

    {run,
     %{
       runtime_id: runtime.id,
       runtime_epoch: runtime.connection_epoch,
       generation: run.generation,
       claim_id: claim_id,
       lease_token: claimed.lease_token
     }}
  end

  defp use_hash_write_mode(mode) do
    config = Application.fetch_env!(:symmetry_control, :orchestration)

    Application.put_env(
      :symmetry_control,
      :orchestration,
      Keyword.put(config, :request_hash_write_mode, mode)
    )
  end
end
