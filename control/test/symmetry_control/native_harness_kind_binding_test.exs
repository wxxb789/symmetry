defmodule SymmetryControl.NativeHarnessKindBindingTest do
  @moduledoc """
  Binds the native harness vocabulary to the restatements that accept a runtime
  registration.

  `SymmetryControl.Orchestration.native_harness_kind?/1` decides native Goal
  admission and the Chat requirement derivation, but the vocabulary is restated
  in the Runtime and HarnessSession schema inclusions and in the
  `runtimes_harness_kind_check` and `harness_sessions_harness_kind_check`
  database constraints. A kind added to `native_harness_kinds/0` alone would be
  selected by admission and then rejected at registration, so this case holds
  both restatements to the list: every admitted kind must pass both changesets
  and appear in both live constraint definitions.
  """

  use SymmetryControl.DataCase, async: false

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

  # The live constraint definition, not the migration source: a later migration
  # that rebuilds the constraint is what registration is checked against.
  defp constraint_kinds!(name) do
    assert %{rows: [[definition]]} =
             Repo.query!(
               "SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1",
               [name]
             )

    ~r/'([^']+)'/
    |> Regex.scan(definition, capture: :all_but_first)
    |> List.flatten()
    |> MapSet.new()
  end
end
