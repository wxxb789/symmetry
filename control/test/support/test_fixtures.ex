defmodule SymmetryControl.TestFixtures do
  @moduledoc """
  Data builders shared by the control suites.

  Each builder below replaces a helper that had been copy-pasted into every
  suite that needed it (5-11 copies per name). The values stay identical to the
  copies': every knob is an explicit option at the call site, and the
  documented defaults are the deterministic fixture values the copies already
  pinned. Suite-specific values (the suite clock, the schema base, a fresh
  daemon or claim id) are arguments at the call site, never hidden defaults.
  """

  import ExUnit.Assertions

  alias SymmetryControl.Goals
  alias SymmetryControl.Orchestration

  # Deterministic fixture ids: the daemon instance and claim id the first
  # orchestration suites pinned, matched by `uuid(1)`/`uuid(10)` in the review
  # and retry suites. A suite that needs a fresh id passes one explicitly; a
  # fresh id is the deliberate choice in the tests that use it (replay and
  # duplicate-claim semantics), so it never arrives by default.
  @daemon_instance_id "00000000-0000-0000-0000-000000000001"
  @claim_id "00000000-0000-0000-0000-000000000010"

  @task_base %{goal: "Run tests", agent_profile: "codex", workspace: "primary", input: %{}}
  @provider_task_base Map.put(@task_base, :goal, "Use provider")

  @doc """
  Registers one runtime for `machine` at `now` and returns it.

  Defaults are the plain codex runtime the converted suites registered:

    * `:runtime_key` / `:name` — `"default"` / `"Local Codex"`
    * `:capacity` / `:agent_profile` / `:workspace` / `:capabilities` —
      `1` / `"codex"` / `"primary"` / `%{}`
    * `:daemon_instance_id` — `#{@daemon_instance_id}`; pass `Ecto.UUID.generate()`
      when the registration must carry a fresh daemon instance
    * `:extra` — a map merged over the registration spec, for
      `heartbeat_interval_ms`, `harness_*`, `adapter_*` and
      `repository_resource_id` fields
  """
  def register_runtime(machine, now, opts \\ []) do
    spec =
      %{
        runtime_key: Keyword.get(opts, :runtime_key, "default"),
        name: Keyword.get(opts, :name, "Local Codex"),
        capacity: Keyword.get(opts, :capacity, 1),
        agent_profile: Keyword.get(opts, :agent_profile, "codex"),
        workspace: Keyword.get(opts, :workspace, "primary"),
        capabilities: Keyword.get(opts, :capabilities, %{})
      }
      |> Map.merge(Keyword.get(opts, :extra, %{}))

    daemon_instance_id = Keyword.get(opts, :daemon_instance_id, @daemon_instance_id)

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(machine.id, daemon_instance_id, [spec], now: now)

    runtime
  end

  @doc """
  Claims `run` for `runtime` at `now` and returns the fence map the suites
  pass to transitions, settlements and renewals.

  Options:

    * `:claim_id` — the claim's idempotency key, default `#{@claim_id}`;
      pass `Ecto.UUID.generate()` when the claim must be a fresh delivery
    * `:via` — the entry point, `SymmetryControl.Orchestration` (default) or
      `SymmetryControl.Goals`
  """
  def claim(run, runtime, now, opts \\ []) do
    claim_id = Keyword.get(opts, :claim_id, @claim_id)
    via = Keyword.get(opts, :via, Orchestration)

    assert {:ok, claimed} =
             via.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: claim_id
               },
               now: now
             )

    %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: claimed.claim_id,
      lease_token: claimed.lease_token
    }
  end

  @doc "Task attributes over the default `\"Run tests\"` goal, with overrides merged in."
  def task_attrs(overrides \\ []) do
    Map.merge(@task_base, Map.new(overrides))
  end

  @doc "Task attributes over the capability suite's `\"Use provider\"` goal."
  def provider_task_attrs(overrides) do
    Map.merge(@provider_task_base, Map.new(overrides))
  end

  @doc """
  The canonical `symmetry.goal_command.v1` map for `goal`, with `payload`
  canonicalised for `"request_decision"` exactly as the goals suite pinned it
  (every other kind passes the payload through).
  """
  def goal_command(goal, kind, payload, mutation_id \\ Ecto.UUID.generate()) do
    %{
      schema_version: "symmetry.goal_command.v1",
      mutation_id: mutation_id,
      expected_version: goal.version,
      expected_revision: goal.current_revision,
      kind: kind,
      payload: canonical_command_payload(kind, payload)
    }
  end

  @doc """
  Submits the current revision of `goal_id` as `"operator:test"`.

  `command_opts` are the suite's own command options (its clock, rollout gate
  and validation profiles); `opts` are merged over them for the individual
  call, so a caller's `:now`, `:rollout_enabled` or `:validation_profiles`
  always wins.
  """
  def command_current(goal_id, kind, payload, command_opts, opts \\ []) do
    assert {:ok, goal} = Goals.fetch_goal(goal_id)

    Goals.command(
      goal_id,
      goal_command(goal, kind, payload),
      "operator:test",
      Keyword.merge(command_opts, opts)
    )
  end

  defp canonical_command_payload("request_decision", payload) do
    case Map.get(payload, :kind, Map.get(payload, "kind")) do
      "plan" ->
        %{
          kind: "plan",
          work_item_id: nil,
          subject_hash: nil,
          proposal: Map.get(payload, :proposal, Map.get(payload, "proposal"))
        }

      "scope" ->
        %{
          kind: "scope",
          work_item_id: Map.get(payload, :work_item_id, Map.get(payload, "work_item_id")),
          subject_hash: nil,
          proposal: %{
            kind:
              Map.get(payload, :dependency_operation, Map.get(payload, "dependency_operation")),
            depends_on_id: Map.get(payload, :depends_on_id, Map.get(payload, "depends_on_id"))
          }
        }

      "review" ->
        %{
          kind: "review",
          work_item_id: Map.get(payload, :work_item_id, Map.get(payload, "work_item_id")),
          subject_hash: Map.get(payload, :subject_hash, Map.get(payload, "subject_hash")),
          proposal: nil
        }

      "completion" ->
        %{
          kind: "completion",
          work_item_id: Map.get(payload, :work_item_id, Map.get(payload, "work_item_id")),
          subject_hash: Map.get(payload, :subject_hash, Map.get(payload, "subject_hash")),
          proposal: nil
        }

      decision_kind ->
        %{kind: decision_kind, work_item_id: nil, subject_hash: nil, proposal: nil}
    end
  end

  defp canonical_command_payload(_kind, payload), do: payload
end
