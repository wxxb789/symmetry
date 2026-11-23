defmodule SymmetryControl.Integrations.IdentityLossProviderStub do
  @moduledoc false
  @behaviour SymmetryControl.Integrations.Provider

  alias SymmetryControl.Integrations.ProviderStub

  def respond_with(response), do: Process.put({__MODULE__, :response}, response)

  @impl true
  defdelegate authenticate(connection), to: ProviderStub

  @impl true
  defdelegate validate_resource_reference(connection, kind, reference), to: ProviderStub

  @impl true
  defdelegate check(connection, auth), to: ProviderStub

  @impl true
  def sync_resource(_connection, _resource, _auth) do
    case Process.get({__MODULE__, :response}, {:error, {:http, 404, "not found"}}) do
      {:blocked, owner} ->
        send(owner, {:identity_loss_waiting, self()})

        receive do
          :continue -> {:error, {:http, 404, "not found"}}
        after
          5_000 -> {:error, {:transport, :test_timeout}}
        end

      response ->
        response
    end
  end

  @impl true
  defdelegate sync_delivery(connection, resource, work_item, auth), to: ProviderStub

  @impl true
  defdelegate sync_ci(connection, resource, work_item, auth), to: ProviderStub

  @impl true
  defdelegate execute(connection, resource, work_item, operation, input, auth), to: ProviderStub
end

defmodule SymmetryControl.ResourceSyncIdentityLossTest do
  use SymmetryControl.DataCase, async: false

  import SymmetryControl.IntegrationFixtures, only: [tracked_resource!: 1]

  alias SymmetryControl.Integrations
  alias SymmetryControl.Integrations.IdentityLossProviderStub
  alias SymmetryControl.TestEnv
  alias SymmetryControl.Workspaces.ProjectResource
  alias SymmetryControl.Workspaces.ReadModel

  @checked_at ~U[2026-09-04 10:00:00.000000Z]

  setup do
    TestEnv.put!(:integration_providers, github: IdentityLossProviderStub)

    :ok
  end

  test "a not-found provider response persists stale synchronization with the not-found message" do
    {_project, resource} = resource_fixture()

    assert_failure_paths(resource, :not_found, "stale", "Provider resource was not found")

    owner = self()

    losing_sync =
      Task.async(fn ->
        IdentityLossProviderStub.respond_with({:blocked, owner})
        Integrations.sync_resource(resource.id)
      end)

    assert_receive {:identity_loss_waiting, provider_pid}

    winner =
      ProjectResource
      |> Repo.get!(resource.id)
      |> ProjectResource.sync_changeset(%{
        status: "healthy",
        sync_status: "synced",
        status_message: nil,
        metadata: %{"winner" => true},
        last_checked_at: DateTime.utc_now()
      })
      |> Repo.update!()

    send(provider_pid, :continue)
    assert {:error, :stale} = Task.await(losing_sync)
    assert Repo.get!(ProjectResource, resource.id) == winner
  end

  test "a transient provider failure keeps persisting failed synchronization" do
    {_project, resource} = resource_fixture()

    for {reason, code, message} <- [
          {{:transport, :timeout}, :provider_failure, "Provider transport failed"},
          {{:http, 401, "unauthorized"}, :provider_unauthorized, "Provider authentication failed"},
          {{:http, 403, "forbidden"}, :forbidden, "Provider permission denied"},
          {{:http, 503, "unavailable"}, :provider_failure, "Provider returned HTTP 503"},
          {:capability_not_granted, :capability_not_granted, "Connection capability is not granted"},
          {:invalid_provider_response, :provider_failure, "Provider response is invalid"},
          {:not_connected, :not_connected, "Resource has no external connection"}
        ] do
      IdentityLossProviderStub.respond_with({:error, reason})
      assert_failure_paths(resource, code, "failed", message)
    end
  end

  test "a stale synchronization is reported as attention by the workspace read model" do
    {project, resource} = resource_fixture()
    {other_project, _other_resource} = resource_fixture()

    assert {:ok, before_sync} = ReadModel.workspace(project.id)
    assert before_sync.health.synchronization == "synced"

    assert {:error, :not_found, _resource} = Integrations.sync_resource(resource.id)
    assert Repo.get!(ProjectResource, resource.id).sync_status == "stale"

    assert {:ok, workspace} = ReadModel.workspace(project.id)
    assert workspace.health.synchronization == "attention"
    assert {:ok, other_workspace} = ReadModel.workspace(other_project.id)
    assert other_workspace.health.synchronization == "synced"
  end

  defp assert_failure_paths(resource, code, sync_status, message) do
    assert {:error, ^code, manual} = Integrations.sync_resource(resource.id)
    assert_persisted_failure(resource, manual, sync_status, message)

    assert %{results: results} = Integrations.sync_all_resources()
    assert {_, {:error, ^code, periodic}} = List.keyfind(results, resource.id, 0)
    assert_persisted_failure(resource, periodic, sync_status, message)
    assert periodic.lock_version == manual.lock_version + 1
    assert DateTime.compare(periodic.last_checked_at, manual.last_checked_at) in [:eq, :gt]
  end

  defp assert_persisted_failure(resource, result, sync_status, message) do
    persisted = Repo.get!(ProjectResource, resource.id)
    assert persisted == result
    assert persisted.status == "degraded"
    assert persisted.sync_status == sync_status
    assert persisted.status_message == message
    assert DateTime.after?(persisted.last_checked_at, @checked_at)
    assert persisted.last_synced_at == resource.last_synced_at
    assert persisted.metadata == resource.metadata
    assert persisted.external_ref == resource.external_ref
    assert persisted.connection_id == resource.connection_id
  end

  defp resource_fixture do
    {project, _connection, resource} =
      tracked_resource!(
        project_name: "Identity loss",
        project_key: "S" <> String.slice(Ecto.UUID.generate(), 0, 6),
        connection_name: "Identity loss " <> Ecto.UUID.generate(),
        resource_name: "Issues"
      )

    resource =
      resource
      |> ProjectResource.sync_changeset(%{
        status: "healthy",
        sync_status: "synced",
        metadata: %{"provider_snapshot" => "retained"},
        last_checked_at: @checked_at,
        last_synced_at: @checked_at
      })
      |> Repo.update!()

    {project, resource}
  end
end
