defmodule SymmetryControl.Orchestration.RegistryTest do
  use SymmetryControl.OrchestrationCase, async: false

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Registry
  alias SymmetryControl.Orchestration.Runtime

  @now ~U[2026-09-12 09:00:00.000000Z]

  test "authenticate_machine resolves a fresh enrollment token and rejects unknown tokens" do
    {machine, token} = enroll_machine("authenticate")

    assert {:ok, authenticated} = Registry.authenticate_machine(token)
    assert authenticated.id == machine.id

    assert {:error, :unauthenticated} =
             Registry.authenticate_machine("machine-token-#{Ecto.UUID.generate()}")

    assert {:error, :unauthenticated} = Registry.authenticate_machine(nil)
  end

  test "machine_owns_run? is true only for the machine that owns the run" do
    {owner, _owner_token} = enroll_machine("run-owner")
    {outsider, _outsider_token} = enroll_machine("run-outsider")
    register_runtime(owner, @now)

    assert {:ok, _task, :created} =
             Orchestration.submit_task(task_attrs(), "machine-owned-run", now: @now)

    assert {:ok, run} = Orchestration.assign_one(now: @now)

    assert Registry.machine_owns_run?(owner.id, run.id)
    refute Registry.machine_owns_run?(outsider.id, run.id)
    refute Registry.machine_owns_run?(owner.id, Ecto.UUID.generate())
  end

  test "valid_runtime_metadata? accepts complete native metadata and rejects a mutated copy" do
    metadata = %{
      harness_kind: "codex",
      harness_version: "0.153.4",
      adapter_version: "symmetry-codex-1",
      adapter_protocol_version: 1
    }

    assert Registry.valid_runtime_metadata?(metadata)
    refute Registry.valid_runtime_metadata?(%{metadata | harness_kind: "unknown"})
    refute Registry.valid_runtime_metadata?(%{metadata | adapter_protocol_version: 0})
  end

  test "adapter_matches_runtime_metadata? requires exact keys and matching versions" do
    metadata = %{
      harness_kind: "codex",
      harness_version: "0.153.4",
      adapter_version: "symmetry-codex-1",
      adapter_protocol_version: 1
    }

    adapter = %{
      "kind" => "codex",
      "native_version" => "0.153.4",
      "implementation_version" => "symmetry-codex-1",
      "protocol_version" => 1,
      "operations" => adapter_operations()
    }

    assert Registry.adapter_matches_runtime_metadata?(adapter, metadata)
    refute Registry.adapter_matches_runtime_metadata?(Map.put(adapter, "extra", true), metadata)

    refute Registry.adapter_matches_runtime_metadata?(
             Map.put(adapter, "protocol_version", 2),
             metadata
           )
  end

  test "valid_adapter_operations? accepts a coherent set and rejects incoherent ones" do
    assert Registry.valid_adapter_operations?(adapter_operations())
    refute Registry.valid_adapter_operations?(Map.put(adapter_operations(), "events", false))

    refute Registry.valid_adapter_operations?(
             Map.put(adapter_operations(), "guidance", "sometimes")
           )
  end

  test "legacy_supervisory_runtime? is true for absent and generic harness kinds only" do
    assert Registry.legacy_supervisory_runtime?(%Runtime{harness_kind: nil})
    assert Registry.legacy_supervisory_runtime?(%Runtime{harness_kind: "generic"})
    refute Registry.legacy_supervisory_runtime?(%Runtime{harness_kind: "codex"})
  end

  test "jsonb_compatible? accepts nested JSON values and rejects a tuple" do
    assert Registry.jsonb_compatible?(%{
             "nested" => [1, "two", %{"three" => true}],
             "empty" => nil
           })

    refute Registry.jsonb_compatible?({:tuple, 1})
  end

  test "heartbeat rejects a stale runtime epoch with :ownership_lost" do
    {machine, _token} = enroll_machine("heartbeat")
    runtime = register_runtime(machine, @now)

    assert {:ok, snapshot} =
             Registry.heartbeat(runtime.id, runtime.connection_epoch, [], now: @now)

    assert snapshot.server_time == @now
    assert snapshot.assignments == []
    assert snapshot.commands == []

    assert {:error, :ownership_lost} =
             Registry.heartbeat(runtime.id, runtime.connection_epoch + 1, [], now: @now)
  end

  defp enroll_machine(name) do
    key = Ecto.UUID.generate()
    token = "machine-token-#{key}"

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(%{name: name, machine_token: token}, key,
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret",
               now: @now
             )

    {enrolled.machine, token}
  end

  defp adapter_operations do
    %{
      "start" => true,
      "events" => true,
      "cancel" => true,
      "resume" => false,
      "handoff" => false,
      "guidance" => "next_turn",
      "pause" => "unsupported",
      "approval_response" => false,
      "usage" => "reported",
      "hard_cost_limit" => false
    }
  end
end
