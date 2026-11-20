defmodule SymmetryControl.IntegrationFixtures do
  @moduledoc """
  Durable fixtures for the integration sync tests: a fresh project, its
  GitHub connection and a bound work-tracking resource.
  """
  import ExUnit.Assertions

  alias SymmetryControl.Integrations
  alias SymmetryControl.Workspaces

  @doc """
  Creates project/connection/resource; `opts` requires `:project_name`,
  `:project_key`, `:connection_name`, `:resource_name`. Returns
  `{project, connection, resource}`.
  """
  def tracked_resource!(opts) do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: Keyword.fetch!(opts, :project_name),
               key: Keyword.fetch!(opts, :project_key),
               default_agent_profile: "codex",
               default_workspace: "primary"
             })

    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: Keyword.fetch!(opts, :connection_name),
               account_ref: "acme",
               auth_type: "gh_cli",
               capabilities: ["work_items"]
             })

    assert {:ok, resource} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: Keyword.fetch!(opts, :resource_name),
               external_ref: "acme/symmetry"
             })

    {project, connection, resource}
  end
end
