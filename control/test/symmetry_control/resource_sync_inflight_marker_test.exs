defmodule SymmetryControl.Integrations.InflightMarkerProvider do
  @behaviour SymmetryControl.Integrations.Provider

  alias SymmetryControl.Integrations.ProviderStub
  alias SymmetryControl.Repo

  @impl true
  def authenticate(_connection) do
    send(controller(), {:provider_authenticating, self(), Repo.in_transaction?()})

    receive do
      :authenticate -> {:ok, :stub_auth}
    after
      5_000 -> {:error, {:transport, :test_timeout}}
    end
  end

  @impl true
  def sync_resource(_connection, resource, :stub_auth) do
    send(controller(), {:provider_waiting, self(), resource, Repo.in_transaction?()})

    receive do
      {:provider_result, result} -> result
    after
      5_000 -> {:error, {:transport, :test_timeout}}
    end
  end

  @impl true
  defdelegate validate_resource_reference(connection, kind, reference), to: ProviderStub
  @impl true
  defdelegate check(connection, auth), to: ProviderStub
  @impl true
  defdelegate sync_delivery(connection, resource, item, auth), to: ProviderStub
  @impl true
  defdelegate sync_ci(connection, resource, item, auth), to: ProviderStub
  @impl true
  defdelegate execute(connection, resource, item, operation, input, auth), to: ProviderStub

  defp controller do
    Application.fetch_env!(:symmetry_control, :inflight_marker_test_controller)
  end
end

defmodule SymmetryControl.ResourceSyncInflightMarkerTest do
  use SymmetryControl.DataCase, async: false

  alias Ecto.Adapters.SQL.Sandbox
  alias SymmetryControl.Integrations
  alias SymmetryControl.Integrations.{Connection, InflightMarkerProvider, Syncer}
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.{Project, ProjectResource}

  setup do
    keys = [:integration_providers, :inflight_marker_test_controller]
    previous = Enum.map(keys, &{&1, Application.fetch_env(:symmetry_control, &1)})

    Application.put_env(:symmetry_control, :integration_providers, github: InflightMarkerProvider)

    Application.put_env(:symmetry_control, :inflight_marker_test_controller, self())

    on_exit(fn ->
      for {key, value} <- previous do
        case value do
          {:ok, original} -> Application.put_env(:symmetry_control, key, original)
          :error -> Application.delete_env(:symmetry_control, key)
        end
      end
    end)

    :ok
  end

  test "a synchronization commits a syncing marker before the provider call and finalizes once" do
    Sandbox.unboxed_run(Repo, fn ->
      {_project, resource} = fixture()
      task = sync_task(resource)
      marker = await_provider(task.pid, resource)

      assert Map.take(marker, [:status, :status_message, :metadata, :last_synced_at]) ==
               Map.take(resource, [:status, :status_message, :metadata, :last_synced_at])

      send(task.pid, {:provider_result, success("fresh")})
      assert {:ok, synced} = Task.await(task)
      assert synced.sync_status == "synced"
      assert synced.status == "healthy"
      assert synced.status_message == nil
      assert synced.metadata == %{"previous" => true, "result" => "fresh"}
      assert DateTime.compare(synced.last_checked_at, marker.last_checked_at) in [:eq, :gt]
      assert synced.last_synced_at == synced.last_checked_at
      assert synced.lock_version == resource.lock_version + 1
      assert Repo.get!(ProjectResource, resource.id) == synced
    end)
  end

  test "concurrent synchronizations produce a single winner and a stale loser that cannot overwrite it" do
    Sandbox.unboxed_run(Repo, fn ->
      for losing_result <- [success("loser"), {:error, {:http, 503, "unavailable"}}],
          archive? <- [false, true] do
        {project, resource} = fixture()
        loser = sync_task(resource)
        await_provider(loser.pid, resource)

        winner = sync_task(resource)
        await_provider(winner.pid, resource)
        send(winner.pid, {:provider_result, success("winner")})
        assert {:ok, synced} = Task.await(winner)
        assert synced.metadata["result"] == "winner"

        if archive? do
          assert {:ok, _archived} =
                   Workspaces.update_project(project.id, %{
                     version: project.lock_version,
                     status: "archived"
                   })
        end

        send(loser.pid, {:provider_result, losing_result})
        assert {:error, :stale} = Task.await(loser)

        stored = Repo.get!(ProjectResource, resource.id)
        assert stored == synced
        assert stored.sync_status == "synced"
        assert stored.status == "healthy"
        assert stored.status_message == nil
      end
    end)
  end

  test "a failed provider call replaces the in-flight marker with failed synchronization" do
    Sandbox.unboxed_run(Repo, fn ->
      {_project, resource} = fixture()
      task = sync_task(resource)
      marker = await_provider(task.pid, resource)

      send(task.pid, {:provider_result, {:error, {:http, 503, "unavailable"}}})
      assert {:error, :provider_failure, failed} = Task.await(task)
      assert failed.sync_status == "failed"
      assert failed.status == "degraded"
      assert failed.status_message == "Provider returned HTTP 503"
      assert failed.metadata == resource.metadata
      assert failed.last_synced_at == resource.last_synced_at
      assert DateTime.compare(failed.last_checked_at, marker.last_checked_at) in [:eq, :gt]
      assert failed.lock_version == resource.lock_version + 1
      assert Repo.get!(ProjectResource, resource.id) == failed
    end)
  end

  test "an archived project rolls the in-flight marker back to the resource's previous state" do
    Sandbox.unboxed_run(Repo, fn ->
      for result <- [success("refused"), {:error, {:http, 503, "unavailable"}}] do
        {project, resource} = fixture()
        task = sync_task(resource)
        await_provider(task.pid, resource)

        assert {:ok, _archived} =
                 Workspaces.update_project(project.id, %{
                   version: project.lock_version,
                   status: "archived"
                 })

        send(task.pid, {:provider_result, result})
        assert {:error, :state_conflict} = Task.await(task)
        assert Repo.get!(ProjectResource, resource.id) == resource
      end
    end)
  end

  test "the periodic syncer notifies completion once after the in-flight marker finalizes" do
    {_project, resource} = fixture()
    name = Module.concat(__MODULE__, "Worker#{System.unique_integer([:positive])}")

    syncer =
      start_supervised!(
        {Syncer, name: name, initial_delay_ms: 0, interval_ms: :timer.hours(1), notify: self()}
      )

    await_provider(syncer, resource)
    refute_received {:integration_sync_complete, _summary}

    send(syncer, {:provider_result, success("periodic")})

    assert_receive {:integration_sync_complete, %{synced: 1, failed: 0, results: results}}, 5_000
    assert [{resource_id, {:ok, synced}}] = results
    assert resource_id == resource.id
    assert synced.sync_status == "synced"
    assert Repo.get!(ProjectResource, resource.id) == synced

    # The server has finished the entire callback before duplicate notification is checked.
    :sys.get_state(syncer)
    refute_received {:integration_sync_complete, _summary}
  end

  defp await_provider(pid, resource) do
    assert_receive {:provider_authenticating, ^pid, false}, 5_000
    marker = Repo.get!(ProjectResource, resource.id)
    assert marker.sync_status == "syncing"
    assert DateTime.compare(marker.last_checked_at, resource.last_checked_at) == :gt

    send(pid, :authenticate)
    assert_receive {:provider_waiting, ^pid, snapshot, false}, 5_000
    assert snapshot.sync_status == "syncing"
    assert snapshot.last_checked_at == marker.last_checked_at
    marker
  end

  defp sync_task(resource) do
    task =
      Task.async(fn ->
        Sandbox.unboxed_run(Repo, fn -> Integrations.sync_resource(resource.id) end)
      end)

    on_exit(fn ->
      if Process.alive?(task.pid), do: Process.exit(task.pid, :kill)
    end)

    task
  end

  defp success(value) do
    {:ok, %{resource: %{metadata: %{"result" => value}}, work_items: []}}
  end

  defp fixture do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: "In-flight marker",
               key: "S" <> String.slice(Ecto.UUID.generate(), 0, 6),
               default_agent_profile: "codex",
               default_workspace: "primary"
             })

    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: "Marker " <> Ecto.UUID.generate(),
               account_ref: "acme",
               capabilities: ["work_items"]
             })

    on_exit(fn ->
      Sandbox.unboxed_run(Repo, fn ->
        Repo.delete_all(from row in Project, where: row.id == ^project.id)
        Repo.delete_all(from row in Connection, where: row.id == ^connection.id)
      end)
    end)

    assert {:ok, resource} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Marker issues",
               external_ref: "acme/symmetry"
             })

    resource =
      resource
      |> ProjectResource.sync_changeset(%{
        status: "degraded",
        sync_status: "failed",
        status_message: "Previous synchronization failed",
        metadata: %{"previous" => true},
        last_checked_at: ~U[2026-01-01 00:00:00.000000Z],
        last_synced_at: ~U[2025-12-31 00:00:00.000000Z]
      })
      |> Repo.update!()

    {project, resource}
  end
end
