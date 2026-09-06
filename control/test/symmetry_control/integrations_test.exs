defmodule SymmetryControl.Integrations.CredentialLeakProviderStub do
  @moduledoc false
  @behaviour SymmetryControl.Integrations.Provider

  alias SymmetryControl.Integrations.ProviderStub

  def respond_with(kind, value), do: Process.put({__MODULE__, kind}, value)

  @impl true
  def authenticate(connection), do: ProviderStub.authenticate(connection)

  @impl true
  def validate_resource_reference(connection, kind, reference),
    do: ProviderStub.validate_resource_reference(connection, kind, reference)

  @impl true
  def check(%{name: "Blocking failure"}, _auth) do
    controller =
      Application.fetch_env!(:symmetry_control, :integration_provider_test_controller)

    send(controller, {:integration_connection_check_waiting, self()})

    receive do
      :continue_failure -> {:error, {:transport, :forced_failure}}
    after
      5_000 -> {:error, {:transport, :test_timeout}}
    end
  end

  def check(connection, auth) do
    case Process.get({__MODULE__, :check}) do
      nil -> ProviderStub.check(connection, auth)
      metadata -> {:ok, metadata}
    end
  end

  @impl true
  def sync_resource(connection, resource, auth) do
    case Process.get({__MODULE__, :sync_resource}) do
      nil -> ProviderStub.sync_resource(connection, resource, auth)
      result -> {:ok, result}
    end
  end

  @impl true
  def sync_delivery(connection, resource, work_item, auth) do
    case Process.get({__MODULE__, :sync_delivery}) do
      nil -> ProviderStub.sync_delivery(connection, resource, work_item, auth)
      delivery -> {:ok, delivery}
    end
  end

  @impl true
  def sync_ci(connection, resource, work_item, auth),
    do: ProviderStub.sync_ci(connection, resource, work_item, auth)

  @impl true
  def execute(connection, resource, work_item, operation, input, auth),
    do: ProviderStub.execute(connection, resource, work_item, operation, input, auth)
end

defmodule SymmetryControl.IntegrationsTest do
  use SymmetryControl.DataCase, async: false

  import Ecto.Query

  alias SymmetryControl.Integrations
  alias SymmetryControl.Integrations.CredentialLeakProviderStub
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.WorkItem

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

  test "an older connection failure cannot overwrite a newer successful check" do
    use_blocking_connection_check_provider()

    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: "Blocking failure",
               account_ref: "acme",
               capabilities: ["work_items"]
             })

    stale_check = Task.async(fn -> Integrations.check_connection(connection.id) end)
    assert_receive {:integration_connection_check_waiting, provider_pid}

    Application.put_env(:symmetry_control, :integration_providers,
      github: SymmetryControl.Integrations.ProviderStub,
      azure_devops: SymmetryControl.Integrations.ProviderStub
    )

    assert {:ok, healthy} = Integrations.check_connection(connection.id)
    assert healthy.status == "healthy"
    assert healthy.metadata["actor"] == "github-actor"

    send(provider_pid, :continue_failure)
    assert {:error, :stale} = Task.await(stale_check)

    stored = Repo.get!(SymmetryControl.Integrations.Connection, connection.id)
    assert stored.lock_version == healthy.lock_version
    assert stored.status == "healthy"
    assert stored.status_message == nil
    assert stored.metadata == healthy.metadata
    assert stored.last_checked_at == healthy.last_checked_at
  end

  test "an older connection failure cannot overwrite a newer configuration" do
    use_blocking_connection_check_provider()

    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: "Blocking failure",
               account_ref: "acme",
               capabilities: ["work_items"]
             })

    stale_check = Task.async(fn -> Integrations.check_connection(connection.id) end)
    assert_receive {:integration_connection_check_waiting, provider_pid}

    assert {:ok, renamed} =
             Integrations.update_connection(connection.id, %{
               version: connection.lock_version,
               name: "Renamed while checking"
             })

    send(provider_pid, :continue_failure)
    assert {:error, :stale} = Task.await(stale_check)

    stored = Repo.get!(SymmetryControl.Integrations.Connection, connection.id)
    assert stored.lock_version == renamed.lock_version
    assert stored.name == "Renamed while checking"
    assert stored.status == "unknown"
    assert stored.status_message == nil
    assert stored.metadata == %{}
    assert stored.last_checked_at == nil
  end

  test "provider credential keys are normalized exactly before connection metadata is stored" do
    use_credential_leak_provider()

    secret_keys = [
      "accessToken",
      "CLIENT-SECRET",
      "refresh_token",
      "api-key",
      "Authorization",
      "PaSsWoRd",
      "pat",
      "credential",
      "TOKEN"
    ]

    Enum.with_index(secret_keys, fn key, index ->
      attrs = %{
        provider: "github",
        name: "Unsafe request #{index}",
        account_ref: "acme",
        capabilities: ["work_items"]
      }

      assert {:error, :invalid_request} =
               attrs
               |> Map.put(key, "must-not-be-stored")
               |> Integrations.create_connection()

      assert {:ok, connection} =
               Integrations.create_connection(%{
                 provider: "github",
                 name: "Unsafe check #{index}",
                 account_ref: "acme",
                 capabilities: ["work_items"]
               })

      CredentialLeakProviderStub.respond_with(:check, %{
        "nested" => [%{key => "must-not-be-stored"}]
      })

      assert {:error, :provider_failure, degraded} =
               Integrations.check_connection(connection.id)

      assert degraded.status == "degraded"
      assert degraded.metadata == %{}
      assert degraded.status_message == "Provider response is invalid"
    end)

    benign = %{
      "token_count" => 3,
      "access_token_count" => 2,
      "password_hint" => "managed externally",
      "authorization_url" => "https://github.com/login/device"
    }

    assert {:ok, connection} =
             Integrations.create_connection(
               Map.merge(
                 %{
                   "provider" => "github",
                   "name" => "Benign token metadata",
                   "account_ref" => "acme",
                   "capabilities" => ["work_items"]
                 },
                 benign
               )
             )

    CredentialLeakProviderStub.respond_with(:check, %{"nested" => benign})
    assert {:ok, checked} = Integrations.check_connection(connection.id)
    assert checked.metadata == %{"nested" => benign}
  end

  test "resource and work-item provider data reject credentials before any provider payload is stored" do
    use_credential_leak_provider()

    project = project_fixture()
    connection = connection_fixture("github", "gh_cli")

    assert {:ok, tracker} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Credential-safe issues",
               external_ref: "acme/symmetry"
             })

    CredentialLeakProviderStub.respond_with(:sync_resource, %{
      resource: %{
        metadata: %{"nested" => [%{"clientSecret" => "must-not-be-stored"}]}
      },
      work_items: []
    })

    assert {:error, :provider_failure, degraded} = Integrations.sync_resource(tracker.id)
    assert degraded.status_message == "Provider response is invalid"
    assert degraded.metadata == %{}
    assert Repo.aggregate(WorkItem, :count) == 0

    CredentialLeakProviderStub.respond_with(:sync_resource, %{
      resource: %{metadata: %{"token_count" => 1}},
      work_items: [provider_work_item(%{"nested" => [%{"refresh-token" => "secret"}]})]
    })

    assert {:error, :provider_failure, degraded} = Integrations.sync_resource(tracker.id)
    assert degraded.metadata == %{}
    assert Repo.aggregate(WorkItem, :count) == 0

    benign_provider_data = %{
      "token_count" => 4,
      "access_token_count" => 2,
      "api_key_id" => "key-1"
    }

    CredentialLeakProviderStub.respond_with(:sync_resource, %{
      resource: %{metadata: %{"token_count" => 1}},
      work_items: [provider_work_item(benign_provider_data)]
    })

    assert {:ok, synced} = Integrations.sync_resource(tracker.id)
    assert synced.metadata == %{"token_count" => 1}

    stored_item = Repo.get_by!(WorkItem, external_work_item_resource_id: tracker.id)
    assert stored_item.external_data == benign_provider_data
  end

  test "delivery provider data rejects credentials before resource or work-item updates are stored" do
    use_credential_leak_provider()

    project = project_fixture()
    connection = connection_fixture("github", "gh_cli")

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "repository",
               name: "Credential-safe repository",
               external_ref: "acme/symmetry"
             })

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Reject delivery credentials",
               repository_resource_id: repository.id,
               pull_request_url: "https://github.com/acme/symmetry/pull/42"
             })

    CredentialLeakProviderStub.respond_with(:sync_resource, %{
      resource: %{metadata: %{"token_count" => 1}},
      work_items: []
    })

    CredentialLeakProviderStub.respond_with(:sync_delivery, %{
      pull_request_url: item.pull_request_url,
      pull_request_state: "open",
      review_status: "approved",
      updated_at: ~U[2026-09-05 09:35:00.000000Z],
      provider_data: %{"outer" => [%{"apiKey" => "must-not-be-stored"}]}
    })

    assert {:error, :provider_failure, degraded} = Integrations.sync_resource(repository.id)
    assert degraded.status_message == "Provider response is invalid"
    assert degraded.metadata == %{}

    stored_item = Repo.get!(WorkItem, item.id)
    assert stored_item.external_pull_request_url == nil
    assert stored_item.external_review_status == nil
    assert stored_item.external_change_data == %{}

    CredentialLeakProviderStub.respond_with(:sync_delivery, %{
      pull_request_url: item.pull_request_url,
      pull_request_state: "open",
      review_status: "approved",
      updated_at: ~U[2026-09-05 09:35:00.000000Z],
      provider_data: %{"token_count" => 1, "password_hint" => "managed externally"}
    })

    assert {:ok, synced} = Integrations.sync_resource(repository.id)
    assert synced.metadata == %{"token_count" => 1}

    delivered = Repo.get!(WorkItem, item.id)
    assert delivered.external_pull_request_url == item.pull_request_url
    assert delivered.external_review_status == "approved"

    assert delivered.external_change_data == %{
             "token_count" => 1,
             "password_hint" => "managed externally"
           }
  end

  test "connections reference CLI authentication and expose diagnosable health without accepting secrets" do
    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: "GitHub engineering",
               account_ref: "acme",
               auth_type: "gh_cli",
               capabilities: ["repositories", "work_items", "changes", "ci"]
             })

    refute Map.has_key?(Map.from_struct(connection), :credential_ciphertext)

    assert {:error, :invalid_request} =
             Integrations.create_connection(%{
               provider: "github",
               name: "Unsafe GitHub",
               account_ref: "acme",
               auth_type: "gh_cli",
               credential: "must-not-be-stored",
               capabilities: ["work_items"]
             })

    assert {:ok, checked} = Integrations.check_connection(connection.id)
    assert checked.status == "healthy"
    assert checked.status_message == nil
    assert checked.last_checked_at
    assert checked.metadata["actor"] == "github-actor"

    assert {:ok, forbidden} =
             Integrations.create_connection(%{
               provider: "azure_devops",
               name: "Forbidden",
               account_ref: "acme",
               auth_type: "entra_id",
               capabilities: ["work_items"]
             })

    assert {:error, :forbidden, degraded} = Integrations.check_connection(forbidden.id)
    assert degraded.status == "degraded"
    assert degraded.status_message =~ "permission"
    assert degraded.last_checked_at
  end

  test "invalid provider payloads degrade health instead of crashing synchronization" do
    project = project_fixture()

    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: "Malformed",
               account_ref: "acme",
               capabilities: ["work_items"]
             })

    assert {:ok, resource} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Malformed issues",
               external_ref: "acme/symmetry"
             })

    assert {:error, :provider_failure, degraded} = Integrations.sync_resource(resource.id)
    assert degraded.status == "degraded"
    assert degraded.sync_status == "failed"
    assert degraded.status_message == "Provider response failed validation"
  end

  test "database rejects provider and authentication type mismatches" do
    assert_raise Postgrex.Error, ~r/external_connections_auth_type_check/, fn ->
      Repo.query!(
        """
        INSERT INTO external_connections (
          id, provider, name, account_ref, auth_type, capabilities, status, metadata,
          lock_version, inserted_at, updated_at
        ) VALUES (
          $1, 'github', 'Invalid auth pair', 'acme', 'entra_id', ARRAY['work_items'],
          'unknown', '{}'::jsonb, 1, NOW(), NOW()
        )
        """,
        [Ecto.UUID.generate() |> Ecto.UUID.dump!()]
      )
    end
  end

  test "provider resources import external work and keep provider and Symmetry ownership separate" do
    project = project_fixture()
    connection = connection_fixture("github", "gh_cli")

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "repository",
               name: "symmetry",
               external_ref: "acme/symmetry"
             })

    assert repository.provider == "github"
    assert repository.status == "unknown"
    assert repository.sync_status == "unknown"

    assert {:ok, tracker} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "GitHub Issues",
               external_ref: "acme/symmetry"
             })

    assert {:ok, synced_tracker} = Integrations.sync_resource(tracker.id)
    assert synced_tracker.status == "healthy"
    assert synced_tracker.sync_status == "synced"
    assert synced_tracker.last_checked_at
    assert synced_tracker.last_synced_at

    assert {:ok, _resynced_tracker} = Integrations.sync_resource(tracker.id)
    assert {:ok, _repository_scope} = Integrations.sync_resource(repository.id)

    assert Repo.aggregate(
             from(item in WorkItem,
               where:
                 item.external_work_item_resource_id == ^tracker.id and item.external_id == "101"
             ),
             :count
           ) == 1

    item = Repo.get_by!(WorkItem, external_work_item_resource_id: tracker.id, external_id: "101")
    assert item.title == "External work stays authoritative"
    assert item.external_provider == "github"
    assert item.external_state == "open"
    assert item.external_assignee_name == "Ada Lovelace"
    assert item.labels == ["agent-ready", "priority:high"]
    assert item.repository_resource_id == repository.id

    assert {:error, :provider_owned} =
             Workspaces.update_work_item(item.id, %{
               version: item.lock_version,
               title: "Local title must not win"
             })

    assert {:error, :provider_owned} =
             Workspaces.update_work_item(item.id, %{
               version: item.lock_version,
               priority: "low"
             })

    assert {:ok, assigned} =
             Workspaces.update_work_item(item.id, %{
               version: item.lock_version,
               assignee_type: "agent",
               assignee_name: "Codex",
               agent_profile: "codex",
               workspace: "primary",
               branch: "codex/external-101",
               pull_request_url: "https://github.com/acme/symmetry/pull/42"
             })

    assert assigned.assignee_type == "agent"
    assert assigned.external_assignee_name == "Ada Lovelace"

    assert {:ok, _repository} = Integrations.sync_resource(repository.id)

    delivered = Repo.get!(WorkItem, assigned.id)
    assert delivered.external_pull_request_url == "https://github.com/acme/symmetry/pull/42"
    assert delivered.external_pull_request_state == "open"
    assert delivered.external_ci_status == "passed"
    assert delivered.external_review_status == "approved"
    assert delivered.external_change_data == %{"head_sha" => "abc123"}

    assert {:ok, corrected} =
             Workspaces.update_work_item(delivered.id, %{
               version: delivered.lock_version,
               pull_request_url: "https://github.com/acme/symmetry/pull/43"
             })

    assert corrected.external_pull_request_url == nil
    assert corrected.external_review_status == nil
    assert corrected.external_ci_status == nil
    assert corrected.external_change_data == %{}
    assert corrected.external_ci_data == %{}

    assert {:ok, launched, :created} = Workspaces.launch_work_item(corrected.id)

    assert launched.task.task.input["external_work_item"] == %{
             "provider" => "github",
             "id" => "101",
             "url" => "https://github.com/acme/symmetry/items/101",
             "state" => "open",
             "resource_id" => tracker.id
           }

    refute Map.has_key?(launched.task.task.input, "credential")
  end

  test "an older GitHub work-item snapshot cannot overwrite newer provider fields" do
    use_credential_leak_provider()
    project = project_fixture()
    connection = connection_fixture("github", "gh_cli")

    assert {:ok, tracker} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "GitHub freshness",
               external_ref: "acme/symmetry"
             })

    CredentialLeakProviderStub.respond_with(
      :sync_resource,
      provider_snapshot(
        "github",
        ~U[2026-09-06 10:00:00.000000Z],
        "New title",
        "closed",
        %{"number" => 101, "marker" => "new"}
      )
    )

    assert {:ok, _resource} = Integrations.sync_resource(tracker.id)

    CredentialLeakProviderStub.respond_with(
      :sync_resource,
      provider_snapshot(
        "github",
        ~U[2026-09-06 09:00:00.000000Z],
        "Old title",
        "open",
        %{"number" => 101, "marker" => "old"}
      )
    )

    assert {:ok, _resource} = Integrations.sync_resource(tracker.id)

    stored =
      Repo.get_by!(WorkItem, external_work_item_resource_id: tracker.id, external_id: "101")

    assert stored.title == "New title"
    assert stored.description == "New title description"
    assert stored.external_state == "closed"
    assert stored.external_updated_at == ~U[2026-09-06 10:00:00.000000Z]
    assert stored.external_assignee_name == "New title owner"
    assert stored.labels == ["new title"]
    assert stored.priority == "high"
    assert stored.external_data == %{"number" => 101, "marker" => "new"}

    CredentialLeakProviderStub.respond_with(
      :sync_resource,
      provider_snapshot(
        "github",
        ~U[2026-09-06 10:00:00.000000Z],
        "Conflicting same-second title",
        "open",
        %{"number" => 101, "marker" => "conflict"}
      )
    )

    assert {:ok, _resource} = Integrations.sync_resource(tracker.id)

    same_second =
      Repo.get_by!(WorkItem, external_work_item_resource_id: tracker.id, external_id: "101")

    assert same_second.title == "Conflicting same-second title"
    assert same_second.external_state == "open"
    assert same_second.external_updated_at == ~U[2026-09-06 10:00:00.000000Z]
    assert same_second.external_data == %{"number" => 101, "marker" => "conflict"}
  end

  test "Azure work-item revision orders snapshots ahead of changed time" do
    use_credential_leak_provider()
    project = project_fixture()
    connection = connection_fixture("azure_devops", "entra_id")

    assert {:ok, tracker} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Azure freshness",
               external_ref: "symmetry/items"
             })

    invalid_initial =
      provider_snapshot(
        "azure_devops",
        ~U[2026-09-06 08:00:00.000000Z],
        "Invalid initial revision",
        "New",
        %{"revision" => "1"}
      )

    CredentialLeakProviderStub.respond_with(:sync_resource, invalid_initial)
    assert {:error, :provider_failure, _resource} = Integrations.sync_resource(tracker.id)
    refute Repo.get_by(WorkItem, external_work_item_resource_id: tracker.id, external_id: "101")

    CredentialLeakProviderStub.respond_with(
      :sync_resource,
      provider_snapshot(
        "azure_devops",
        ~U[2026-09-06 10:00:00.000000Z],
        "Revision 8",
        "Active",
        %{"revision" => 8}
      )
    )

    assert {:ok, _resource} = Integrations.sync_resource(tracker.id)

    CredentialLeakProviderStub.respond_with(
      :sync_resource,
      provider_snapshot(
        "azure_devops",
        ~U[2026-09-06 11:00:00.000000Z],
        "Revision 7",
        "Closed",
        %{"revision" => 7}
      )
    )

    assert {:ok, _resource} = Integrations.sync_resource(tracker.id)

    stored =
      Repo.get_by!(WorkItem, external_work_item_resource_id: tracker.id, external_id: "101")

    assert stored.title == "Revision 8"
    assert stored.external_data == %{"revision" => 8}
    assert stored.external_updated_at == ~U[2026-09-06 10:00:00.000000Z]

    CredentialLeakProviderStub.respond_with(
      :sync_resource,
      provider_snapshot(
        "azure_devops",
        ~U[2026-09-06 09:00:00.000000Z],
        "Revision 9",
        "Resolved",
        %{"revision" => 9}
      )
    )

    assert {:ok, _resource} = Integrations.sync_resource(tracker.id)

    stored =
      Repo.get_by!(WorkItem, external_work_item_resource_id: tracker.id, external_id: "101")

    assert stored.title == "Revision 9"
    assert stored.external_state == "Resolved"
    assert stored.external_data == %{"revision" => 9}
    assert stored.external_updated_at == ~U[2026-09-06 09:00:00.000000Z]

    invalid =
      provider_snapshot(
        "azure_devops",
        ~U[2026-09-06 12:00:00.000000Z],
        "Missing revision",
        "Closed",
        %{}
      )

    CredentialLeakProviderStub.respond_with(:sync_resource, invalid)
    assert {:error, :provider_failure, _resource} = Integrations.sync_resource(tracker.id)

    unchanged =
      Repo.get_by!(WorkItem, external_work_item_resource_id: tracker.id, external_id: "101")

    assert unchanged.title == "Revision 9"
    assert unchanged.external_data == %{"revision" => 9}
  end

  test "duplicate provider work items select the freshest item independent of page order" do
    use_credential_leak_provider()
    project = project_fixture()
    connection = connection_fixture("github", "gh_cli")

    old =
      provider_snapshot(
        "github",
        ~U[2026-09-06 09:00:00.000000Z],
        "Old duplicate",
        "open",
        %{"number" => 101}
      )
      |> Map.fetch!(:work_items)
      |> hd()

    fresh =
      provider_snapshot(
        "github",
        ~U[2026-09-06 10:00:00.000000Z],
        "Fresh duplicate",
        "closed",
        %{"number" => 101}
      )
      |> Map.fetch!(:work_items)
      |> hd()

    for {name, work_items} <- [
          {"Old first", [old, fresh]},
          {"Fresh first", [fresh, old]}
        ] do
      assert {:ok, tracker} =
               Workspaces.create_resource(project.id, %{
                 connection_id: connection.id,
                 kind: "work_tracking",
                 name: name,
                 external_ref: "acme/#{String.replace(name, " ", "-")}"
               })

      CredentialLeakProviderStub.respond_with(:sync_resource, %{
        resource: %{metadata: %{}},
        work_items: work_items
      })

      assert {:ok, _resource} = Integrations.sync_resource(tracker.id)

      stored =
        Repo.get_by!(WorkItem, external_work_item_resource_id: tracker.id, external_id: "101")

      assert stored.title == "Fresh duplicate"
      assert stored.external_updated_at == ~U[2026-09-06 10:00:00.000000Z]
    end
  end

  test "a malformed duplicate rolls back the complete provider snapshot" do
    use_credential_leak_provider()
    project = project_fixture()
    connection = connection_fixture("github", "gh_cli")

    assert {:ok, tracker} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Malformed duplicate",
               external_ref: "acme/malformed-duplicate"
             })

    valid =
      provider_snapshot(
        "github",
        ~U[2026-09-06 09:00:00.000000Z],
        "Valid duplicate",
        "open",
        %{"number" => 101}
      )
      |> Map.fetch!(:work_items)
      |> hd()

    malformed =
      valid
      |> Map.put(:external_updated_at, ~U[2026-09-06 10:00:00.000000Z])
      |> Map.put(:external_url, "not-a-url")

    CredentialLeakProviderStub.respond_with(:sync_resource, %{
      resource: %{metadata: %{}},
      work_items: [valid, malformed]
    })

    assert {:error, :provider_failure, _resource} = Integrations.sync_resource(tracker.id)
    refute Repo.get_by(WorkItem, external_work_item_resource_id: tracker.id, external_id: "101")
  end

  test "malformed delivery metadata degrades the resource without persisting partial state" do
    project = project_fixture()

    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: "Malformed delivery",
               account_ref: "acme",
               capabilities: ["repositories", "changes"]
             })

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "repository",
               name: "Malformed repository",
               external_ref: "acme/symmetry"
             })

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Reject malformed delivery metadata",
               repository_resource_id: repository.id,
               pull_request_url: "https://github.com/acme/symmetry/pull/42"
             })

    assert {:error, :provider_failure, degraded} = Integrations.sync_resource(repository.id)
    assert degraded.sync_status == "failed"
    assert degraded.status_message == "Provider response is invalid"

    stored = Repo.get!(WorkItem, item.id)
    assert stored.external_pull_request_url == nil
    assert stored.external_change_data == %{}
  end

  test "a project can bind GitHub and Azure DevOps resources independently" do
    project = project_fixture()
    github = connection_fixture("github", "gh_cli")
    azure = connection_fixture("azure_devops", "entra_id")

    assert {:ok, github_repository} =
             Workspaces.create_resource(project.id, %{
               connection_id: github.id,
               kind: "repository",
               name: "GitHub code",
               external_ref: "acme/symmetry"
             })

    assert {:ok, azure_tracker} =
             Workspaces.create_resource(project.id, %{
               connection_id: azure.id,
               kind: "work_tracking",
               name: "Azure Boards",
               external_ref: "Platform"
             })

    assert github_repository.provider == "github"
    assert azure_tracker.provider == "azure_devops"
    assert github_repository.connection_id != azure_tracker.connection_id
  end

  test "existing external work binds to a repository attached after its first sync" do
    project = project_fixture()
    connection = connection_fixture("github", "gh_cli")

    assert {:ok, tracker} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Issues first",
               external_ref: "AcMe/SyMmEtRy"
             })

    assert {:ok, _tracker} = Integrations.sync_resource(tracker.id)
    item = Repo.get_by!(WorkItem, external_work_item_resource_id: tracker.id)
    assert item.repository_resource_id == nil

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "repository",
               name: "Repository later",
               external_ref: "acme/symmetry"
             })

    assert {:ok, _tracker} = Integrations.sync_resource(tracker.id)
    assert Repo.get!(WorkItem, item.id).repository_resource_id == repository.id
  end

  test "missing external work is retained as unavailable and cannot start until it returns" do
    project = project_fixture()
    connection = connection_fixture("github", "gh_cli")

    assert {:ok, tracker} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Availability tracker",
               external_ref: "acme/symmetry"
             })

    assert {:ok, _tracker} = Integrations.sync_resource(tracker.id)
    item = Repo.get_by!(WorkItem, external_work_item_resource_id: tracker.id)

    assert {:ok, assigned} =
             Workspaces.update_work_item(item.id, %{
               version: item.lock_version,
               assignee_type: "agent",
               assignee_name: "Codex",
               agent_profile: "codex",
               workspace: "primary"
             })

    assert {:ok, empty_connection} =
             Integrations.update_connection(connection.id, %{
               version: connection.lock_version,
               name: "Empty work items"
             })

    assert {:ok, _tracker} = Integrations.sync_resource(tracker.id)
    unavailable = Repo.get!(WorkItem, assigned.id)
    assert unavailable.external_available == false
    assert unavailable.title == "External work stays authoritative"
    assert unavailable.external_state == "open"
    assert {:error, :state_conflict} = Workspaces.launch_work_item(unavailable.id)

    assert {:ok, _restored_connection} =
             Integrations.update_connection(empty_connection.id, %{
               version: empty_connection.lock_version,
               name: "Restored work items"
             })

    assert {:ok, _tracker} = Integrations.sync_resource(tracker.id)
    assert Repo.get!(WorkItem, assigned.id).external_available == true
  end

  test "resource bindings cannot exceed a connection's declared capabilities" do
    project = project_fixture()

    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: "Issues only",
               account_ref: "acme",
               auth_type: "gh_cli",
               capabilities: ["work_items"]
             })

    assert {:error, :forbidden} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "repository",
               name: "Repository outside scope",
               external_ref: "acme/symmetry"
             })

    assert {:error, :forbidden} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Cross-account issues",
               external_ref: "other/symmetry"
             })
  end

  test "renaming a connection with unchanged capabilities preserves health" do
    project = project_fixture()
    connection = connection_fixture("github", "gh_cli")
    assert {:ok, checked} = Integrations.check_connection(connection.id)

    assert {:ok, resource} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Healthy tracker",
               external_ref: "acme/symmetry"
             })

    assert {:ok, synced} = Integrations.sync_resource(resource.id)

    assert {:ok, renamed} =
             Integrations.update_connection(checked.id, %{
               version: checked.lock_version,
               name: "Renamed connection",
               capabilities: checked.capabilities
             })

    assert renamed.status == "healthy"
    assert renamed.last_checked_at == checked.last_checked_at

    unchanged_resource = Repo.get!(SymmetryControl.Workspaces.ProjectResource, synced.id)
    assert unchanged_resource.status == "healthy"
    assert unchanged_resource.sync_status == "synced"
    assert unchanged_resource.last_synced_at == synced.last_synced_at
  end

  test "changing a connected resource kind clears provider-specific state" do
    project = project_fixture()
    connection = connection_fixture("github", "gh_cli")

    assert {:ok, resource} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "repository",
               name: "Mutable resource",
               external_ref: "acme/symmetry"
             })

    assert {:ok, synced} = Integrations.sync_resource(resource.id)
    assert synced.status == "healthy"
    assert synced.metadata != %{}

    assert {:ok, changed} =
             Workspaces.update_resource(synced.id, %{
               version: synced.lock_version,
               kind: "ci"
             })

    assert changed.kind == "ci"
    assert changed.url == nil
    assert changed.status == "unknown"
    assert changed.sync_status == "unknown"
    assert changed.metadata == %{}
    assert changed.last_checked_at == nil
    assert changed.last_synced_at == nil
  end

  test "archived projects cannot be synchronized manually or by the periodic syncer" do
    project = project_fixture()
    connection = connection_fixture("github", "gh_cli")

    assert {:ok, resource} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Archived issues",
               external_ref: "acme/symmetry"
             })

    assert {:ok, _archived} =
             Workspaces.update_project(project.id, %{
               version: project.lock_version,
               status: "archived"
             })

    assert {:error, :state_conflict} = Integrations.sync_project(project.id)
    assert {:error, :state_conflict} = Integrations.sync_resource(resource.id)
    assert %{synced: 0, failed: 0} = Integrations.sync_all_resources()
    assert Repo.get!(SymmetryControl.Workspaces.ProjectResource, resource.id).status == "unknown"
  end

  test "a failed provider call cannot update resource health after the project is archived" do
    project = project_fixture()

    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: "Fail after archive",
               account_ref: "acme",
               capabilities: ["work_items"]
             })

    assert {:ok, resource} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Archive race",
               external_ref: "acme/symmetry"
             })

    Application.put_env(
      :symmetry_control,
      :integration_provider_test_controller,
      self()
    )

    on_exit(fn ->
      Application.delete_env(:symmetry_control, :integration_provider_test_controller)
    end)

    sync_task = Task.async(fn -> Integrations.sync_resource(resource.id) end)
    assert_receive {:integration_provider_waiting, provider_pid}

    assert {:ok, _archived} =
             Workspaces.update_project(project.id, %{
               version: project.lock_version,
               status: "archived"
             })

    send(provider_pid, :continue)
    assert {:error, :state_conflict} = Task.await(sync_task)

    unchanged = Repo.get!(SymmetryControl.Workspaces.ProjectResource, resource.id)
    assert unchanged.status == "unknown"
    assert unchanged.sync_status == "unknown"
    assert unchanged.status_message == nil
  end

  test "a failed provider call cannot mark a replacement resource binding as failed" do
    project = project_fixture()

    assert {:ok, old_connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: "Fail after archive",
               account_ref: "acme",
               capabilities: ["work_items"]
             })

    new_connection = connection_fixture("github", "gh_cli")

    assert {:ok, resource} =
             Workspaces.create_resource(project.id, %{
               connection_id: old_connection.id,
               kind: "work_tracking",
               name: "Rebinding race",
               external_ref: "acme/symmetry"
             })

    Application.put_env(
      :symmetry_control,
      :integration_provider_test_controller,
      self()
    )

    on_exit(fn ->
      Application.delete_env(:symmetry_control, :integration_provider_test_controller)
    end)

    sync_task = Task.async(fn -> Integrations.sync_resource(resource.id) end)
    assert_receive {:integration_provider_waiting, provider_pid}

    assert {:ok, rebound} =
             Workspaces.update_resource(resource.id, %{
               version: resource.lock_version,
               connection_id: new_connection.id,
               external_ref: "acme/rebound"
             })

    send(provider_pid, :continue)
    assert {:error, :stale} = Task.await(sync_task)

    unchanged = Repo.get!(SymmetryControl.Workspaces.ProjectResource, resource.id)
    assert unchanged.connection_id == rebound.connection_id
    assert unchanged.external_ref == rebound.external_ref
    assert unchanged.status == "unknown"
    assert unchanged.sync_status == "unknown"
    assert unchanged.status_message == nil
  end

  test "delivery sync cannot restore provider data after the work item binding changes" do
    project = project_fixture()

    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: "Wait for delivery",
               account_ref: "acme",
               capabilities: ["repositories", "work_items", "changes"]
             })

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "repository",
               name: "Delivery race repository",
               external_ref: "acme/symmetry"
             })

    assert {:ok, tracker} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Delivery race tracker",
               external_ref: "acme/symmetry"
             })

    assert {:ok, _tracker} = Integrations.sync_resource(tracker.id)
    item = Repo.get_by!(WorkItem, external_work_item_resource_id: tracker.id)

    assert {:ok, item} =
             Workspaces.update_work_item(item.id, %{
               version: item.lock_version,
               pull_request_url: "https://github.com/acme/symmetry/pull/42"
             })

    Application.put_env(
      :symmetry_control,
      :integration_provider_test_controller,
      self()
    )

    on_exit(fn ->
      Application.delete_env(:symmetry_control, :integration_provider_test_controller)
    end)

    sync_task = Task.async(fn -> Integrations.sync_resource(repository.id) end)
    assert_receive {:integration_delivery_waiting, provider_pid}

    assert {:ok, corrected} =
             Workspaces.update_work_item(item.id, %{
               version: item.lock_version,
               pull_request_url: "https://github.com/acme/symmetry/pull/43"
             })

    send(provider_pid, :continue)
    assert {:error, :stale} = Task.await(sync_task)

    stored = Repo.get!(WorkItem, item.id)
    assert stored.pull_request_url == corrected.pull_request_url
    assert stored.external_pull_request_url == nil
    assert stored.external_review_status == nil
    assert stored.external_change_data == %{}
  end

  test "bound connection and resource identities cannot be silently rebound" do
    project = project_fixture()
    connection = connection_fixture("github", "gh_cli")

    assert {:ok, resource} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Identity-bound issues",
               external_ref: "acme/symmetry"
             })

    assert {:error, :state_conflict} =
             Integrations.update_connection(connection.id, %{
               version: connection.lock_version,
               account_ref: "different-account"
             })

    assert {:ok, _synced} = Integrations.sync_resource(resource.id)
    resource = Repo.get!(SymmetryControl.Workspaces.ProjectResource, resource.id)

    assert {:error, :state_conflict} =
             Workspaces.update_resource(resource.id, %{
               version: resource.lock_version,
               external_ref: "acme/other"
             })
  end

  test "removing delivery capabilities clears provider authority" do
    project = project_fixture()
    connection = connection_fixture("github", "gh_cli")

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "repository",
               name: "Delivery repository",
               external_ref: "acme/symmetry"
             })

    assert {:ok, tracker} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Delivery issues",
               external_ref: "acme/symmetry"
             })

    assert {:ok, _tracker} = Integrations.sync_resource(tracker.id)
    item = Repo.get_by!(WorkItem, external_work_item_resource_id: tracker.id)

    assert {:ok, _item} =
             Workspaces.update_work_item(item.id, %{
               version: item.lock_version,
               pull_request_url: "https://github.com/acme/symmetry/pull/42"
             })

    assert {:ok, _repository} = Integrations.sync_resource(repository.id)
    delivered = Repo.get!(WorkItem, item.id)
    assert delivered.external_ci_status == "passed"
    assert delivered.external_review_status == "approved"

    connection = Repo.get!(SymmetryControl.Integrations.Connection, connection.id)

    assert {:ok, without_ci} =
             Integrations.update_connection(connection.id, %{
               version: connection.lock_version,
               capabilities: ["repositories", "work_items", "changes"]
             })

    delivered = Repo.get!(WorkItem, item.id)
    assert delivered.external_ci_status == nil
    assert delivered.external_review_status == "approved"

    assert {:ok, _without_changes} =
             Integrations.update_connection(without_ci.id, %{
               version: without_ci.lock_version,
               capabilities: ["repositories", "work_items"]
             })

    delivered = Repo.get!(WorkItem, item.id)
    assert delivered.external_pull_request_url == nil
    assert delivered.external_review_status == nil
  end

  test "connection capability changes preserve archived project snapshots" do
    project = project_fixture()
    connection = connection_fixture("github", "gh_cli")

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "repository",
               name: "Archived repository",
               external_ref: "acme/symmetry"
             })

    assert {:ok, tracker} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Archived tracker",
               external_ref: "acme/symmetry"
             })

    assert {:ok, _tracker} = Integrations.sync_resource(tracker.id)
    item = Repo.get_by!(WorkItem, external_work_item_resource_id: tracker.id)

    assert {:ok, _item} =
             Workspaces.update_work_item(item.id, %{
               version: item.lock_version,
               pull_request_url: "https://github.com/acme/symmetry/pull/42"
             })

    assert {:ok, synced_repository} = Integrations.sync_resource(repository.id)
    delivered = Repo.get!(WorkItem, item.id)

    assert {:ok, _archived} =
             Workspaces.update_project(project.id, %{
               version: project.lock_version,
               status: "archived"
             })

    assert {:ok, _updated_connection} =
             Integrations.update_connection(connection.id, %{
               version: connection.lock_version,
               capabilities: ["repositories", "work_items"]
             })

    archived_repository = Repo.get!(SymmetryControl.Workspaces.ProjectResource, repository.id)
    archived_item = Repo.get!(WorkItem, item.id)

    assert archived_repository.lock_version == synced_repository.lock_version
    assert archived_repository.status == synced_repository.status
    assert archived_repository.sync_status == synced_repository.sync_status
    assert archived_item.lock_version == delivered.lock_version
    assert archived_item.external_pull_request_url == delivered.external_pull_request_url
    assert archived_item.external_ci_status == delivered.external_ci_status
    assert archived_item.external_review_status == delivered.external_review_status
  end

  defp use_credential_leak_provider do
    Application.put_env(:symmetry_control, :integration_providers,
      github: CredentialLeakProviderStub,
      azure_devops: CredentialLeakProviderStub
    )
  end

  defp use_blocking_connection_check_provider do
    use_credential_leak_provider()
    previous = Application.get_env(:symmetry_control, :integration_provider_test_controller)
    Application.put_env(:symmetry_control, :integration_provider_test_controller, self())

    on_exit(fn ->
      if previous do
        Application.put_env(:symmetry_control, :integration_provider_test_controller, previous)
      else
        Application.delete_env(:symmetry_control, :integration_provider_test_controller)
      end
    end)
  end

  defp provider_work_item(provider_data) do
    %{
      external_id: Ecto.UUID.generate(),
      external_url: "https://github.com/acme/symmetry/issues/credential-check",
      external_state: "open",
      external_updated_at: ~U[2026-09-05 09:30:00.000000Z],
      title: "Provider credential validation",
      description: "Provider data must remain credential-free.",
      labels: ["security"],
      external_assignee_name: nil,
      status: "ready",
      priority: "high",
      provider_data: provider_data
    }
  end

  defp provider_snapshot(provider, updated_at, title, external_state, provider_data) do
    base_url =
      case provider do
        "github" -> "https://github.com/acme/symmetry/issues/101"
        "azure_devops" -> "https://dev.azure.com/acme/symmetry/_workitems/edit/101"
      end

    %{
      resource: %{metadata: %{}},
      work_items: [
        %{
          external_id: "101",
          external_url: base_url,
          external_state: external_state,
          external_updated_at: updated_at,
          title: title,
          description: title <> " description",
          labels: [String.downcase(title)],
          external_assignee_name: title <> " owner",
          status: "ready",
          priority: "high",
          provider_data: provider_data
        }
      ]
    }
  end

  defp connection_fixture(provider, auth_type) do
    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: provider,
               name: provider <> "-" <> Ecto.UUID.generate(),
               account_ref: "acme",
               auth_type: auth_type,
               capabilities: ["repositories", "work_items", "changes", "ci"]
             })

    connection
  end

  defp project_fixture do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: "Symmetry",
               key: "S" <> String.slice(Ecto.UUID.generate(), 0, 6),
               default_agent_profile: "codex",
               default_workspace: "primary"
             })

    project
  end
end
