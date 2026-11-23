defmodule SymmetryControl.TestFixtures do
  @moduledoc false
  import ExUnit.Assertions

  alias SymmetryControl.Goals
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Workspaces

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

  @daemon_instance_id "00000000-0000-0000-0000-000000000001"
  @claim_id "00000000-0000-0000-0000-000000000010"

  @task_base %{goal: "Run tests", agent_profile: "codex", workspace: "primary", input: %{}}
  @provider_task_base Map.put(@task_base, :goal, "Use provider")

  def uuid, do: Ecto.UUID.generate()

  def register_runtime(machine, now, opts \\ []) do
    spec =
      Map.merge(
        %{
          runtime_key: Keyword.get(opts, :runtime_key, "default"),
          name: Keyword.get(opts, :name, "Local Codex"),
          capacity: Keyword.get(opts, :capacity, 1),
          agent_profile: Keyword.get(opts, :agent_profile, "codex"),
          workspace: Keyword.get(opts, :workspace, "primary"),
          capabilities: Keyword.get(opts, :capabilities, %{})
        },
        Keyword.get(opts, :extra, %{})
      )

    daemon_instance_id = Keyword.get(opts, :daemon_instance_id, @daemon_instance_id)

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(machine.id, daemon_instance_id, [spec], now: now)

    runtime
  end

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
  Claims through `Goals` with a fresh claim id per call — `claim/4`'s fixed default is the
  idempotency key the replay/duplicate-claim tests rely on.
  """
  def goal_claim(run, runtime, now, opts \\ []) do
    claim(run, runtime, now, Keyword.merge([claim_id: Ecto.UUID.generate(), via: Goals], opts))
  end

  def task_attrs(overrides \\ []) do
    Map.merge(@task_base, Map.new(overrides))
  end

  def provider_task_attrs(overrides) do
    Map.merge(@provider_task_base, Map.new(overrides))
  end

  def subject(repository_id) do
    %{
      "resource_id" => repository_id,
      "commit" => String.duplicate("a", 40),
      "tree_digest" => "sha256:" <> String.duplicate("4", 64)
    }
  end

  def check_contract do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "An independent check must pass.",
      "predicates" => [%{"id" => "check", "kind" => "check", "validator_profile" => "test"}]
    }
  end

  @doc """
  One enabled `:test` check whose `allowed_runtime_ids` is a fresh id: no registered runtime is
  admitted, so admission sites use their own `validation_profiles(runtime_id)`.
  """
  def validation_profiles do
    [
      test: [
        kind: :check,
        profile_digest: "sha256:" <> String.duplicate("c", 64),
        enabled: true,
        allowed_runtime_ids: [Ecto.UUID.generate()]
      ]
    ]
  end

  def repository_fixture(project) do
    {:ok, repository} =
      Workspaces.create_resource(project.id, %{
        kind: "repository",
        name: "Repository #{System.unique_integer([:positive])}"
      })

    repository
  end

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

  def unix_signal(:term), do: "-TERM"
  def unix_signal(:kill), do: "-KILL"

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
            kind: Map.get(payload, :dependency_operation, Map.get(payload, "dependency_operation")),
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
