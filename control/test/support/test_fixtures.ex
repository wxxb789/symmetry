defmodule SymmetryControl.TestFixtures do
  @moduledoc """
  Data builders shared by the control suites.

  Each builder below replaces a helper that had been copy-pasted into every
  suite that needed it (5-11 copies per name). The values stay identical to the
  copies': a knob the converted call sites set differently from the documented
  default is an explicit option, and the defaults are the deterministic fixture
  values most of those call sites rely on. Suite-specific values (the suite
  clock, the schema base, a fresh daemon or claim id) are arguments at the call
  site, never hidden defaults.
  """

  import ExUnit.Assertions

  alias SymmetryControl.Goals
  alias SymmetryControl.Orchestration

  @doc """
  Injects the suite's command surface: `command_opts/0` returns the suite's
  default command options and `command_current/3,4` submits the current
  revision of a goal with them.

  A suite whose command call sites share one option list declares it once:

      use SymmetryControl.TestFixtures,
        command_opts: [now: @now, validation_profiles: validation_profiles()]

  `command_opts/0` rebuilds the list on every call, so lazily-generated values
  (`Ecto.UUID.generate()`, `DateTime.utc_now/0`) stay per-call. A call's `opts`
  are merged over the defaults, so a caller's `:now`, `:rollout_enabled` or
  `:validation_profiles` always wins — the same precedence the converted
  suites' local wrappers had. A suite that passes no `:command_opts` injects
  nothing.
  """
  defmacro __using__(opts) do
    case Keyword.fetch(opts, :command_opts) do
      {:ok, command_opts} ->
        quote do
          defp command_opts, do: unquote(command_opts)

          defp command_current(goal_id, kind, payload, opts \\ []) do
            assert {:ok, goal} = SymmetryControl.Goals.fetch_goal(goal_id)

            SymmetryControl.Goals.command(
              goal_id,
              SymmetryControl.TestFixtures.goal_command(goal, kind, payload),
              "operator:test",
              Keyword.merge(command_opts(), opts)
            )
          end
        end

      :error ->
        nil
    end
  end

  # Deterministic fixture ids: the daemon instance the review suite pinned
  # (`uuid(1)` there) and the claim id the review and retry suites pinned
  # (`uuid(10)` in both). A suite that needs a fresh id passes one explicitly; a
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

  @doc """
  Claims `run` for `runtime` at `now` through `Goals` — the goal-admission
  suites' claim entry point — and returns the fence map the transitions take.

  The claim id is generated per call: those suites pin a fresh id deliberately
  (replay and duplicate-claim semantics), so this does not take `claim/4`'s
  deterministic default. `opts` are merged over the `via: Goals` default.
  """
  def goal_claim(run, runtime, now, opts \\ []) do
    claim(run, runtime, now, Keyword.merge([claim_id: Ecto.UUID.generate(), via: Goals], opts))
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
