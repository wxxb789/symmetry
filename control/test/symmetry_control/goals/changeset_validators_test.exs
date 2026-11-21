defmodule SymmetryControl.Goals.ChangesetValidatorsTest do
  @moduledoc """
  Byte-exact pins for the validation messages the goal schema modules produce.

  Written before rf-w2-05 extracts the shared validators
  (`SymmetryControl.Goals.ChangesetValidators`): every message asserted here must
  be byte-identical on the base revision and after the extraction, and the
  accept/reject outcome per drive must not change. Each drive calls the schema
  module's own public changeset function (`changeset/2`, `resolve_changeset/2` or
  `immutable_changeset/2`); where `cast/3` rejects the malformed value first (the
  `:map` fields), the drive pre-seeds the change on the changeset so the pin
  records the validator's own message rather than Ecto's `"is invalid"`.
  """
  use ExUnit.Case, async: true

  alias Ecto.Changeset

  alias SymmetryControl.Goals.{
    ContextSnapshot,
    GoalEvent,
    GoalExternalWait,
    GoalDecision,
    GoalRevision,
    HarnessSessionAttachReceipt,
    HarnessSessionStopReceipt,
    RunEvidence,
    RunUsage,
    WorkDependency,
    WorkOutcome
  }

  @uuid "00000000-0000-0000-0000-000000000001"
  @digest :binary.copy(<<1>>, 32)

  # Ecto stores errors most-recent-first, so `Keyword.get_values/2` order is the
  # order the assertion below pins.
  defp messages(changeset, field) do
    changeset.errors
    |> Keyword.get_values(field)
    |> Enum.map(&elem(&1, 0))
  end

  # A field-pinned validator only runs when the value reaches `changes`. The
  # `:map`-typed fields reject non-maps in `cast/3` itself, so the drives below
  # put the malformed value on a changeset base first: that is the only way the
  # validator's own message (rather than Ecto's "is invalid") can fire, and it is
  # the path a caller that pre-seeds the changeset exercises.
  defp seeded(struct, field, value) do
    struct |> Changeset.change() |> Changeset.put_change(field, value)
  end

  defp digest_messages do
    [
      {"ContextSnapshot.content_hash",
       fn -> ContextSnapshot.changeset(%ContextSnapshot{}, %{content_hash: "short"}) end,
       :content_hash, "must be a 32-byte digest"},
      {"RunEvidence.subject_hash",
       fn -> RunEvidence.changeset(%RunEvidence{}, %{subject_hash: "short"}) end, :subject_hash,
       "must be a 32-byte digest"},
      {"WorkOutcome.subject_hash",
       fn -> WorkOutcome.changeset(%WorkOutcome{}, %{subject_hash: "short"}) end, :subject_hash,
       "must be a 32-byte digest"},
      {"GoalDecision.action_hash",
       fn -> GoalDecision.changeset(%GoalDecision{}, %{action_hash: "short"}) end, :action_hash,
       "must be a 32-byte digest"},
      {"GoalEvent.request_hash",
       fn -> GoalEvent.changeset(%GoalEvent{}, %{request_hash: "short"}) end, :request_hash,
       "must be a 32-byte digest"},
      {"GoalExternalWait.subject_hash",
       fn -> GoalExternalWait.changeset(%GoalExternalWait{}, %{subject_hash: "short"}) end,
       :subject_hash, "must be a 32-byte digest"},
      {"HarnessSessionStopReceipt.request_hash",
       fn ->
         HarnessSessionStopReceipt.changeset(%HarnessSessionStopReceipt{}, %{
           request_hash: "short"
         })
       end, :request_hash, "must be a 32-byte digest"},
      {"HarnessSessionAttachReceipt.request_hash",
       fn ->
         HarnessSessionAttachReceipt.changeset(%HarnessSessionAttachReceipt{}, %{
           request_hash: "short"
         })
       end, :request_hash, "must be a 32-byte digest"},
      {"GoalDecision.subject_hash (nil-tolerant digest validator)",
       fn -> GoalDecision.changeset(%GoalDecision{}, %{subject_hash: "short"}) end, :subject_hash,
       "must be a 32-byte digest"}
    ]
  end

  test "every digest field keeps its byte-identical message and rejection" do
    for {label, build, field, expected} <- digest_messages() do
      changeset = build.()
      refute changeset.valid?, label
      assert messages(changeset, field) == [expected], label
    end
  end

  test "a valid 32-byte digest is still accepted where the digest is required" do
    accept = fn changeset, field ->
      assert messages(changeset, field) == []
    end

    accept.(
      ContextSnapshot.changeset(%ContextSnapshot{}, %{content_hash: @digest}),
      :content_hash
    )

    accept.(WorkOutcome.changeset(%WorkOutcome{}, %{subject_hash: @digest}), :subject_hash)
  end

  defp map_messages do
    [
      {"GoalRevision.acceptance_contract",
       fn ->
         GoalRevision.changeset(seeded(%GoalRevision{}, :acceptance_contract, :not_a_map), %{})
       end, :acceptance_contract, ["must be an object"]},
      {"GoalRevision.authority_policy",
       fn ->
         GoalRevision.changeset(seeded(%GoalRevision{}, :authority_policy, :not_a_map), %{})
       end, :authority_policy, ["must be an object"]},
      {"GoalRevision.execution_policy (shared validator plus the policy validator)",
       fn ->
         GoalRevision.changeset(seeded(%GoalRevision{}, :execution_policy, :not_a_map), %{})
       end, :execution_policy, ["must be an object", "must be an object"]},
      {"GoalRevision.context_manifest",
       fn ->
         GoalRevision.changeset(seeded(%GoalRevision{}, :context_manifest, :not_a_map), %{})
       end, :context_manifest, ["must be an object"]},
      {"ContextSnapshot.payload",
       fn -> ContextSnapshot.changeset(seeded(%ContextSnapshot{}, :payload, :not_a_map), %{}) end,
       :payload, ["must be an object"]},
      {"RunEvidence.source_ref",
       fn -> RunEvidence.changeset(seeded(%RunEvidence{}, :source_ref, :not_a_map), %{}) end,
       :source_ref, ["must be an object"]},
      {"RunEvidence.payload",
       fn -> RunEvidence.changeset(seeded(%RunEvidence{}, :payload, :not_a_map), %{}) end,
       :payload, ["must be an object"]},
      {"GoalEvent.payload",
       fn -> GoalEvent.changeset(seeded(%GoalEvent{}, :payload, :not_a_map), %{}) end, :payload,
       ["must be an object"]},
      {"GoalEvent.response",
       fn -> GoalEvent.changeset(seeded(%GoalEvent{}, :response, :not_a_map), %{}) end, :response,
       ["must be an object"]},
      {"HarnessSessionStopReceipt.response",
       fn ->
         HarnessSessionStopReceipt.changeset(
           seeded(%HarnessSessionStopReceipt{}, :response, :not_a_map),
           %{}
         )
       end, :response, ["must be an object"]},
      {"HarnessSessionAttachReceipt.response",
       fn ->
         HarnessSessionAttachReceipt.changeset(
           seeded(%HarnessSessionAttachReceipt{}, :response, :not_a_map),
           %{}
         )
       end, :response, ["must be an object"]},
      {"WorkOutcome.candidate_subject (inline call-site validator)",
       fn -> WorkOutcome.changeset(%WorkOutcome{}, %{candidate_subject: []}) end,
       :candidate_subject, ["must be an object"]},
      {"GoalDecision.resolution (inline call-site validator)",
       fn ->
         GoalDecision.resolve_changeset(
           seeded(%GoalDecision{state: "open"}, :resolution, :not_a_map),
           %{state: "resolved"}
         )
       end, :resolution, ["must be an object"]},
      {"GoalExternalWait.source_ref (single-copy helper)",
       fn ->
         GoalExternalWait.changeset(seeded(%GoalExternalWait{}, :source_ref, :not_a_map), %{})
       end, :source_ref, ["must be an object"]},
      {"GoalExternalWait.result (single-copy helper)",
       fn ->
         GoalExternalWait.changeset(seeded(%GoalExternalWait{}, :result, :not_a_map), %{})
       end, :result, ["must be an object"]}
    ]
  end

  test "every object field keeps its byte-identical message and rejection" do
    for {label, build, field, expected} <- map_messages() do
      changeset = build.()
      refute changeset.valid?, label
      assert messages(changeset, field) == expected, label
    end
  end

  test "a malformed object field reaching cast/3 keeps Ecto's own error, not the validator's" do
    changeset = ContextSnapshot.changeset(%ContextSnapshot{}, %{payload: :not_a_map})
    refute changeset.valid?
    assert messages(changeset, :payload) == ["is invalid"]
  end

  defp immutability_messages do
    [
      {"goal_revision",
       fn -> GoalRevision.immutable_changeset(%GoalRevision{}, %{objective: "x"}) end, :objective,
       "goal_revision is immutable"},
      {"work dependency",
       fn -> WorkDependency.immutable_changeset(%WorkDependency{}, %{goal_id: @uuid}) end,
       :goal_id, "work dependency is immutable"},
      {"context snapshot",
       fn -> ContextSnapshot.immutable_changeset(%ContextSnapshot{}, %{schema_version: 2}) end,
       :schema_version, "context snapshot is immutable"},
      {"run evidence",
       fn -> RunEvidence.immutable_changeset(%RunEvidence{}, %{evidence_key: "key"}) end,
       :evidence_key, "run evidence is immutable"},
      {"work outcome",
       fn -> WorkOutcome.immutable_changeset(%WorkOutcome{}, %{reason: "why"}) end, :reason,
       "work outcome is immutable"},
      {"goal event", fn -> GoalEvent.immutable_changeset(%GoalEvent{}, %{kind: "kind"}) end,
       :kind, "goal event is immutable"},
      {"run usage", fn -> RunUsage.immutable_changeset(%RunUsage{}, %{provider: "provider"}) end,
       :provider, "run usage is immutable"},
      {"external wait history",
       fn ->
         GoalExternalWait.immutable_changeset(%GoalExternalWait{}, %{external_ref: "ref"})
       end, :external_ref, "external wait history is immutable"}
    ]
  end

  test "every immutability guard keeps its byte-identical message and rejection" do
    for {label, build, field, expected} <- immutability_messages() do
      changeset = build.()
      refute changeset.valid?, label
      assert messages(changeset, field) == [expected], label
    end
  end

  test "immutability guards stay silent when nothing changed" do
    assert GoalEvent.immutable_changeset(%GoalEvent{}, %{}).valid?
    assert GoalRevision.immutable_changeset(%GoalRevision{}, %{}).valid?
    assert WorkDependency.immutable_changeset(%WorkDependency{}, %{}).valid?
    assert ContextSnapshot.immutable_changeset(%ContextSnapshot{}, %{}).valid?
    assert RunEvidence.immutable_changeset(%RunEvidence{}, %{}).valid?
    assert WorkOutcome.immutable_changeset(%WorkOutcome{}, %{}).valid?
    assert RunUsage.immutable_changeset(%RunUsage{}, %{}).valid?
    assert GoalExternalWait.immutable_changeset(%GoalExternalWait{}, %{}).valid?
  end
end
