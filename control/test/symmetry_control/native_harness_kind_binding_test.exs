defmodule SymmetryControl.NativeHarnessKindBindingTest do
  @moduledoc """
  Binds the native harness vocabulary to every gate that accepts a runtime
  registration or a harness-session attach.

  `SymmetryControl.Orchestration.native_harness_kind?/1` decides native Goal
  admission and the Chat requirement derivation, but the vocabulary is restated
  in the Runtime and HarnessSession schema inclusions and in the
  `runtimes_harness_kind_check` and `harness_sessions_harness_kind_check`
  database constraints. A kind added to `native_harness_kinds/0` alone would be
  selected by admission and then rejected at registration, so this case holds
  both restatements to the list: every admitted kind must pass both changesets
  and appear in both live constraint definitions. The runtime-metadata and
  session-attribute gates read the single source themselves, and the probes
  below hold them to it: every admitted kind must register a runtime through
  `Orchestration.register_runtimes/4` and clear `Goals.attach_harness_session/5`'s
  attribute gate, while a kind outside the vocabulary is refused by both.
  """

  use SymmetryControl.DataCase, async: true

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Runtime

  @runtime_constraint "runtimes_harness_kind_check"
  @harness_session_constraint "harness_sessions_harness_kind_check"

  test "every kind the native predicate admits passes the schema inclusions and the check constraints" do
    kinds = Orchestration.native_harness_kinds()

    # Non-vacuity: an empty list would make every assertion below vacuous.
    assert Enum.any?(kinds)

    runtime_kinds = constraint_kinds!(@runtime_constraint)
    harness_session_kinds = constraint_kinds!(@harness_session_constraint)

    for kind <- kinds do
      refute Keyword.has_key?(
               Runtime.changeset(%Runtime{}, %{harness_kind: kind}).errors,
               :harness_kind
             ),
             "#{@runtime_constraint} rejects the native kind #{inspect(kind)}"

      refute Keyword.has_key?(
               HarnessSession.changeset(%HarnessSession{}, %{harness_kind: kind}).errors,
               :harness_kind
             ),
             "#{@harness_session_constraint} rejects the native kind #{inspect(kind)}"

      assert MapSet.member?(runtime_kinds, kind),
             "the runtimes check constraint does not admit the native kind #{inspect(kind)}"

      assert MapSet.member?(harness_session_kinds, kind),
             "the harness_sessions check constraint does not admit the native kind #{inspect(kind)}"
    end
  end

  test "every kind the native predicate admits registers a runtime through the public call" do
    for kind <- Orchestration.native_harness_kinds() do
      %{machine: machine} = enroll_machine!("native #{kind}")

      assert {:ok, [runtime]} =
               Orchestration.register_runtimes(
                 machine.id,
                 Ecto.UUID.generate(),
                 [runtime_specification(kind)]
               )

      assert runtime.harness_kind == kind
    end

    # The runtime-metadata gate also keeps the legacy "generic" kind; the
    # rewrite to the single source must not drop that branch.
    %{machine: generic_machine} = enroll_machine!("generic")

    assert {:ok, [%Runtime{harness_kind: "generic"}]} =
             Orchestration.register_runtimes(
               generic_machine.id,
               Ecto.UUID.generate(),
               [runtime_specification("generic")]
             )

    # A kind outside the vocabulary is refused at registration rather than
    # silently accepted by a list that drifted.
    %{machine: foreign_machine} = enroll_machine!("foreign")

    assert {:error, :invalid_request} =
             Orchestration.register_runtimes(
               foreign_machine.id,
               Ecto.UUID.generate(),
               [runtime_specification("foreign_harness")]
             )
  end

  test "every kind the native predicate admits clears the harness-session attach gate" do
    for kind <- Orchestration.native_harness_kinds() do
      # The machine and run do not exist, so an admitted kind must be refused
      # past the attribute gate (:not_found), never at it (:invalid_request).
      assert Goals.attach_harness_session(
               Ecto.UUID.generate(),
               Ecto.UUID.generate(),
               fence(),
               session_attrs(kind)
             ) == {:error, :not_found}
    end

    assert Goals.attach_harness_session(
             Ecto.UUID.generate(),
             Ecto.UUID.generate(),
             fence(),
             session_attrs("foreign_harness")
           ) == {:error, :invalid_request}
  end

  defp runtime_specification(kind) do
    %{
      runtime_key: "native-binding-#{kind}",
      name: "Native binding #{kind}",
      capacity: 1,
      agent_profile: kind,
      workspace: "primary",
      harness_kind: kind,
      harness_version: "0.153.4",
      adapter_version: "symmetry-#{kind}-1",
      adapter_protocol_version: 1,
      capabilities: runtime_capabilities(kind),
      heartbeat_interval_ms: 60_000
    }
  end

  defp runtime_capabilities(kind) do
    %{
      "interactive" => false,
      "adapter" => %{
        "kind" => kind,
        "native_version" => "0.153.4",
        "implementation_version" => "symmetry-#{kind}-1",
        "protocol_version" => 1,
        "operations" => adapter_operations(kind)
      }
    }
  end

  defp adapter_operations("generic") do
    %{
      "start" => true,
      "events" => true,
      "cancel" => true,
      "resume" => false,
      "handoff" => false,
      "guidance" => "unsupported",
      "pause" => "unsupported",
      "approval_response" => false,
      "usage" => "unknown",
      "hard_cost_limit" => false
    }
  end

  defp adapter_operations(_kind) do
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

  defp session_attrs(kind) do
    %{
      "local_handle_id" => Ecto.UUID.generate(),
      "harness_kind" => kind,
      "harness_version" => "0.153.4",
      "adapter_version" => "symmetry-#{kind}-1",
      "workspace_fingerprint" => "sha256:#{String.duplicate("a", 64)}",
      "workspace" => "primary"
    }
  end

  defp fence do
    %{
      runtime_id: Ecto.UUID.generate(),
      runtime_epoch: 1,
      generation: 1,
      claim_id: Ecto.UUID.generate(),
      lease_token: Ecto.UUID.generate()
    }
  end

  defp enroll_machine!(name) do
    key = Ecto.UUID.generate()
    token = "native-binding-#{key}"

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(
               %{name: name, machine_token: token},
               key,
               enrollment_token: "test-enrollment-token",
               expected_enrollment_token: "test-enrollment-token"
             )

    enrolled
  end

  # The live constraint definition, not the migration source: a later migration
  # that rebuilds the constraint is what registration is checked against.
  # `connamespace = current_schema()` keeps a concurrent migration test's private
  # schema, which carries the same constraint names, out of the result.
  defp constraint_kinds!(name) do
    assert %{rows: [[definition]]} =
             Repo.query!(
               """
               SELECT pg_get_constraintdef(oid) FROM pg_constraint
               WHERE conname = $1 AND connamespace = current_schema()::regnamespace
               """,
               [name]
             )

    ~r/'([^']+)'/
    |> Regex.scan(definition, capture: :all_but_first)
    |> List.flatten()
    |> MapSet.new()
  end
end
