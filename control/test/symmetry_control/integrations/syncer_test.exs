defmodule SymmetryControl.Integrations.SyncerTest do
  use SymmetryControl.DataCase, async: false

  import SymmetryControl.IntegrationFixtures, only: [tracked_resource!: 1]

  alias SymmetryControl.Integrations.ProviderStub
  alias SymmetryControl.Integrations.Syncer
  alias SymmetryControl.Repo
  alias SymmetryControl.TestEnv
  alias SymmetryControl.Workspaces.ProjectResource

  setup do
    TestEnv.put!(:integration_providers,
      github: ProviderStub,
      azure_devops: ProviderStub
    )

    :ok
  end

  test "periodic synchronization refreshes all connected resources" do
    {_project, _connection, resource} =
      tracked_resource!(
        project_name: "Automated sync",
        project_key: "AUTO",
        connection_name: "Automatic GitHub",
        resource_name: "Automatic issues"
      )

    name = Module.concat(__MODULE__, "Worker#{System.unique_integer([:positive])}")

    start_supervised!({Syncer, name: name, initial_delay_ms: 0, interval_ms: to_timeout(hour: 1), notify: self()})

    assert_receive {:integration_sync_complete, %{synced: 1, failed: 0}}, 1_000
    assert Repo.get!(ProjectResource, resource.id).sync_status == "synced"
  end
end
