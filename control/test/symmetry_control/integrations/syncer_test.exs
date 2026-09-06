defmodule SymmetryControl.Integrations.SyncerTest do
  use SymmetryControl.DataCase, async: false

  alias SymmetryControl.Integrations
  alias SymmetryControl.Integrations.Syncer
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.ProjectResource

  setup do
    previous = Application.get_env(:symmetry_control, :integration_providers)

    Application.put_env(:symmetry_control, :integration_providers,
      github: SymmetryControl.Integrations.ProviderStub,
      azure_devops: SymmetryControl.Integrations.ProviderStub
    )

    on_exit(fn ->
      if previous do
        Application.put_env(:symmetry_control, :integration_providers, previous)
      else
        Application.delete_env(:symmetry_control, :integration_providers)
      end
    end)

    :ok
  end

  test "periodic synchronization refreshes all connected resources" do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: "Automated sync",
               key: "AUTO",
               default_agent_profile: "codex",
               default_workspace: "primary"
             })

    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: "Automatic GitHub",
               account_ref: "acme",
               auth_type: "gh_cli",
               capabilities: ["work_items"]
             })

    assert {:ok, resource} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Automatic issues",
               external_ref: "acme/symmetry"
             })

    name = Module.concat(__MODULE__, "Worker#{System.unique_integer([:positive])}")

    start_supervised!(
      {Syncer, name: name, initial_delay_ms: 0, interval_ms: :timer.hours(1), notify: self()}
    )

    assert_receive {:integration_sync_complete, %{synced: 1, failed: 0}}, 1_000
    assert Repo.get!(ProjectResource, resource.id).sync_status == "synced"
  end
end
