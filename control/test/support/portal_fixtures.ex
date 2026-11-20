defmodule SymmetryControlWeb.PortalFixtures do
  @moduledoc """
  The contract fixtures the portal controller tests share.

  The project/connection builders drive the real endpoints through
  `SymmetryControlWeb.TestConn`; the goal contract fragments are the literals
  every Goal envelope builder repeats. A file whose fixture needs a different
  value keeps one local adapter that binds that value and calls in here, so the
  request mechanics and the shared contract text live in exactly one place.
  """
  import Phoenix.ConnTest, only: [json_response: 2, post: 3]

  alias SymmetryControl.Workspaces
  alias SymmetryControlWeb.TestConn

  @endpoint SymmetryControlWeb.Endpoint

  @doc "Creates a project through `POST /portal/api/projects` and returns the 201 body's project."
  def create_project(key, name) do
    %{"project" => project} =
      TestConn.api_conn()
      |> post("/portal/api/projects", %{
        "name" => name,
        "key" => key,
        "default_agent_profile" => "codex",
        "default_workspace" => "primary"
      })
      |> json_response(201)

    project
  end

  @doc """
  Creates a project through `Workspaces.create_project/1` with a fresh unique
  suffix: `name_prefix` and `key_prefix` make one test file's projects distinct
  without a second copy of the insert mechanics.
  """
  def create_context_project(name_prefix, key_prefix) do
    {:ok, project} =
      Workspaces.create_project(%{
        "name" => "#{name_prefix} #{System.unique_integer([:positive])}",
        "key" => "#{key_prefix}#{System.unique_integer([:positive])}",
        "default_agent_profile" => "codex",
        "default_workspace" => "primary"
      })

    project
  end

  @doc "Creates a connection through `POST /portal/api/connections` and returns the 201 body's connection."
  def create_connection(provider, name, account_ref, capabilities) do
    %{"connection" => connection} =
      TestConn.api_conn()
      |> post("/portal/api/connections", %{
        "provider" => provider,
        "name" => name,
        "account_ref" => account_ref,
        "capabilities" => capabilities
      })
      |> json_response(201)

    connection
  end

  @doc "The complete Goal create envelope; `acceptance_description` selects the file's contract text."
  def goal_create_payload(title, objective, acceptance_description) do
    %{
      "schema_version" => "symmetry.goal_create.v1",
      "mutation_id" => Ecto.UUID.generate(),
      "title" => title,
      "initial_revision" => %{
        "objective" => objective,
        "non_goals" => [],
        "acceptance_contract" => acceptance_contract(acceptance_description),
        "authority_policy" => authority_policy(),
        "execution_policy" => execution_policy(),
        "context_manifest" => context_manifest(),
        "reason" => "Initial approved Goal contract"
      }
    }
  end

  @doc "The acceptance contract shell around one operator-verification description."
  def acceptance_contract(description) do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => description,
      "predicates" => [%{"id" => "operator", "kind" => "operator_acceptance"}]
    }
  end

  @doc "The authority policy every Goal contract fixture carries."
  def authority_policy do
    %{
      "operator_required_for_scope_change" => true,
      "operator_required_for_completion" => true,
      "publication_allowed" => false,
      "allowed_actions" => []
    }
  end

  @doc "The execution policy every Goal contract fixture carries."
  def execution_policy do
    %{
      "automatic_execution" => false,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 2,
      "max_run_attempts_per_task" => 2,
      "budget_limit_microusd" => nil,
      "per_run_cost_limit_microusd" => nil,
      "budget_mode" => "soft",
      "hard_cost_limit_required" => false,
      "allowed_runtime_ids" => [],
      "allowed_model_profiles" => [],
      "final_acceptance" => "operator",
      "allowed_actions" => [],
      "allowed_resource_ids" => []
    }
  end

  @doc "The context manifest every Goal contract fixture carries."
  def context_manifest do
    %{
      "byte_budget" => 32_768,
      "required_source_kinds" => ["repository"],
      "include_advisory_recall" => false
    }
  end
end
