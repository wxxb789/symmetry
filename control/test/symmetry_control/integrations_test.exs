defmodule SymmetryControl.IntegrationsTest do
  use SymmetryControl.DataCase, async: false

  import Ecto.Query

  import SymmetryControl.IntegrationFixtures,
    only: [
      assert_sync_waiting_on_work_item: 3,
      provider_snapshot: 5,
      provider_work_item: 1,
      sync_lock_order_task: 4,
      use_blocking_connection_check_provider: 0,
      use_credential_leak_provider: 0,
      work_item_row_locked?: 1
    ]

  alias Ecto.Adapters.SQL.Sandbox
  alias SymmetryControl.Integrations
  alias SymmetryControl.Integrations.Connection
  alias SymmetryControl.Integrations.CredentialLeakProviderStub
  alias SymmetryControl.Integrations.ProviderStub
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.Project
  alias SymmetryControl.Workspaces.ProjectResource
  alias SymmetryControl.Workspaces.WorkItem

  setup do
    previous = Application.get_env(:symmetry_control, :integration_providers)

    Application.put_env(:symmetry_control, :integration_providers,
      github: ProviderStub,
      azure_devops: ProviderStub
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
      github: ProviderStub,
      azure_devops: ProviderStub
    )

    assert {:ok, healthy} = Integrations.check_connection(connection.id)
    assert healthy.status == "healthy"
    assert healthy.metadata["actor"] == "github-actor"

    send(provider_pid, :continue_failure)
    assert {:error, :stale} = Task.await(stale_check)

    stored = Repo.get!(Connection, connection.id)
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

    stored = Repo.get!(Connection, connection.id)
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
        [Ecto.UUID.dump!(Ecto.UUID.generate())]
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
               where: item.external_work_item_resource_id == ^tracker.id and item.external_id == "101"
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
      "github"
      |> provider_snapshot(
        ~U[2026-09-06 09:00:00.000000Z],
        "Old duplicate",
        "open",
        %{"number" => 101}
      )
      |> Map.fetch!(:work_items)
      |> hd()

    fresh =
      "github"
      |> provider_snapshot(
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
      "github"
      |> provider_snapshot(
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

    assert {:error, :capability_not_granted} =
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

  test "a pre-dispatch capability refusal is classified definite for the stored outcome" do
    project = project_fixture()

    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: "Repositories without changes",
               account_ref: "acme",
               auth_type: "gh_cli",
               capabilities: ["repositories"]
             })

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "repository",
               name: "Changes outside scope",
               external_ref: "acme/symmetry"
             })

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Refused change",
               repository_resource_id: repository.id,
               pull_request_url: "https://github.com/acme/symmetry/pull/42"
             })

    # No provider call happens: `require_capabilities/2` refuses between the
    # shared dispatch guards and `provider.authenticate/1`, so the outcome must
    # definite (stored `failed` and replayed) like every other pre-dispatch
    # refusal.
    assert {:error,
            {:provider_action_failure, :capability_not_granted, :definite, "Connection capability is not granted"}} =
             Integrations.execute_unprojected_provider_action(
               connection,
               repository,
               item,
               "change.upsert",
               %{}
             )
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

    unchanged_resource = Repo.get!(ProjectResource, synced.id)
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
    assert Repo.get!(ProjectResource, resource.id).status == "unknown"
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

    unchanged = Repo.get!(ProjectResource, resource.id)
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

    unchanged = Repo.get!(ProjectResource, resource.id)
    assert unchanged.connection_id == rebound.connection_id
    assert unchanged.external_ref == rebound.external_ref
    assert unchanged.status == "unknown"
    assert unchanged.sync_status == "unknown"
    assert unchanged.status_message == nil
  end

  test "a resource deleted before the sync lock stays stale instead of invalid" do
    project = project_fixture()
    connection = connection_fixture("github", "gh_cli")

    assert {:ok, resource} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Delete before lock",
               external_ref: "acme/symmetry"
             })

    ref = make_ref()
    handler_id = {__MODULE__, ref}

    :ok =
      :telemetry.attach(
        handler_id,
        [:symmetry_control, :repo, :query],
        fn _event, _measurements, metadata, _config ->
          if String.contains?(metadata.query, "\"projects\"") and
               String.contains?(metadata.query, "FOR SHARE") do
            {deleted, _} =
              Repo.delete_all(
                from current in ProjectResource,
                  where: current.id == ^resource.id
              )

            send(self(), {:resource_deleted_before_lock, ref, deleted})
          end
        end,
        nil
      )

    try do
      # `mark_resource_syncing/1` re-reads the resource under the project lock (FOR UPDATE); with the
      # row already deleted that read is `nil`: the sync must answer `:stale` (409), not `:invalid_request` (400).
      assert {:error, :stale} = Integrations.sync_resource(resource.id)
    after
      :telemetry.detach(handler_id)
    end

    assert_received {:resource_deleted_before_lock, ^ref, 1}
    assert Repo.get(ProjectResource, resource.id)
  end

  test "a resource deleted during the provider call stays stale instead of invalid" do
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
               name: "Delete during provider call",
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
    # The send follows the sync lock transaction and provider auth: wait the 5 000 ms window
    # the stub holds :continue for.
    assert_receive {:integration_provider_waiting, provider_pid}, 5_000

    assert {1, _} =
             Repo.delete_all(
               from current in ProjectResource,
                 where: current.id == ^resource.id
             )

    send(provider_pid, :continue)

    # `record_resource_failure/3` re-reads the row under FOR UPDATE: a missing row must stay
    # `:stale`, the verdict the base lock helper gave it.
    assert {:error, :stale} = Task.await(sync_task)
  end

  test "reversed import and delivery updates lock WorkItems in UUID order without deadlocking" do
    use_credential_leak_provider()

    Sandbox.unboxed_run(Repo, fn ->
      project = project_fixture()
      connection = connection_fixture("github", "gh_cli")

      on_exit(fn ->
        Sandbox.unboxed_run(Repo, fn ->
          Repo.delete_all(
            from project_row in Project,
              where: project_row.id == ^project.id
          )

          Repo.delete_all(
            from connection_row in Connection,
              where: connection_row.id == ^connection.id
          )
        end)
      end)

      assert {:ok, repository} =
               Workspaces.create_resource(project.id, %{
                 connection_id: connection.id,
                 kind: "repository",
                 name: "Lock order repository",
                 external_ref: "acme/symmetry"
               })

      assert {:ok, tracker} =
               Workspaces.create_resource(project.id, %{
                 connection_id: connection.id,
                 kind: "work_tracking",
                 name: "Lock order tracker",
                 external_ref: "acme/symmetry"
               })

      provider_items =
        Enum.map(["101", "102"], fn external_id ->
          %{} |> provider_work_item() |> Map.put(:external_id, external_id)
        end)

      CredentialLeakProviderStub.respond_with(:sync_resource, %{
        resource: %{metadata: %{}},
        work_items: provider_items
      })

      assert {:ok, _tracker} = Integrations.sync_resource(tracker.id)

      items =
        from(item in WorkItem,
          where: item.external_work_item_resource_id == ^tracker.id,
          order_by: [asc: item.id]
        )
        |> Repo.all()
        |> Enum.map(fn item ->
          assert {:ok, updated} =
                   Workspaces.update_work_item(item.id, %{
                     version: item.lock_version,
                     pull_request_url: "https://github.com/acme/symmetry/pull/" <> item.external_id
                   })

          updated
        end)

      [lower, higher] = items

      # Import writes ascending UUIDs; delivery collection writes the same rows in reverse.
      provider_items =
        Enum.map(items, fn item ->
          %{}
          |> provider_work_item()
          |> Map.merge(%{
            external_id: item.external_id,
            title: "Updated " <> item.external_id,
            external_updated_at: ~U[2026-09-06 09:30:00.000000Z]
          })
        end)

      owner = self()
      ref = make_ref()
      delivery = sync_lock_order_task(repository, [], owner, ref)
      importer = sync_lock_order_task(tracker, provider_items, owner, ref)
      delivery_pid = delivery.pid
      importer_pid = importer.pid
      handler_id = {__MODULE__, ref}

      :ok =
        :telemetry.attach(
          handler_id,
          [:symmetry_control, :repo, :query],
          fn _event, _measurements, metadata, _config ->
            if self() == delivery_pid and
                 String.starts_with?(metadata.query, "UPDATE \"work_items\"") and
                 String.contains?(metadata.query, "\"external_pull_request_url\"") do
              send(owner, {:delivery_first_update, ref, metadata.params})

              receive do
                {:continue_delivery, ^ref} -> :ok
              after
                15_000 -> :ok
              end
            end
          end,
          nil
        )

      try do
        assert_receive {:sync_backend, ^ref, ^delivery_pid, delivery_backend}, 5_000
        assert_receive {:sync_backend, ^ref, ^importer_pid, importer_backend}, 5_000
        send(delivery_pid, {:start_sync, ref})
        assert_receive {:delivery_first_update, ^ref, params}, 5_000
        :telemetry.detach(handler_id)
        assert Ecto.UUID.dump!(higher.id) in params

        send(importer_pid, {:start_sync, ref})

        assert_sync_waiting_on_work_item(
          importer_backend,
          delivery_backend,
          System.monotonic_time(:millisecond) + 5_000
        )

        # Without the ordered prelock, import now holds lower while delivery holds higher.
        # The direction witness is the sibling case below, "the work-item prelock
        # waits in ascending UUID order while a delivery holds the higher row".
        send(delivery_pid, {:continue_delivery, ref})
        assert {:ok, _repository} = Task.await(delivery, 10_000)
        assert {:ok, _tracker} = Task.await(importer, 10_000)

        for item <- [lower, higher] do
          stored = Repo.get!(WorkItem, item.id)
          assert stored.title == "Updated " <> item.external_id
          assert stored.external_updated_at == ~U[2026-09-06 09:30:00.000000Z]
          assert stored.external_available == true
          assert stored.external_pull_request_url == item.pull_request_url
          assert stored.external_review_status == "approved"
          assert stored.external_ci_status == "passed"
        end
      after
        :telemetry.detach(handler_id)
        send(delivery_pid, {:continue_delivery, ref})
        Task.shutdown(delivery, :brutal_kill)
        Task.shutdown(importer, :brutal_kill)
      end
    end)
  end

  test "the work-item prelock waits in ascending UUID order while a delivery holds the higher row" do
    use_credential_leak_provider()

    Sandbox.unboxed_run(Repo, fn ->
      project = project_fixture()
      connection = connection_fixture("github", "gh_cli")

      on_exit(fn ->
        Sandbox.unboxed_run(Repo, fn ->
          Repo.delete_all(
            from project_row in Project,
              where: project_row.id == ^project.id
          )

          Repo.delete_all(
            from connection_row in Connection,
              where: connection_row.id == ^connection.id
          )
        end)
      end)

      assert {:ok, repository} =
               Workspaces.create_resource(project.id, %{
                 connection_id: connection.id,
                 kind: "repository",
                 name: "Lock order witness repository",
                 external_ref: "acme/symmetry"
               })

      assert {:ok, tracker} =
               Workspaces.create_resource(project.id, %{
                 connection_id: connection.id,
                 kind: "work_tracking",
                 name: "Lock order witness tracker",
                 external_ref: "acme/symmetry"
               })

      provider_items =
        Enum.map(["101", "102"], fn external_id ->
          %{} |> provider_work_item() |> Map.put(:external_id, external_id)
        end)

      CredentialLeakProviderStub.respond_with(:sync_resource, %{
        resource: %{metadata: %{}},
        work_items: provider_items
      })

      assert {:ok, _tracker} = Integrations.sync_resource(tracker.id)

      items =
        Repo.all(
          from item in WorkItem,
            where: item.external_work_item_resource_id == ^tracker.id,
            order_by: [asc: item.id]
        )

      [lower, higher] = items

      # Only the higher row is deliverable, so the delivery transaction holds a
      # single work-item row while paused on its first UPDATE: the ascending
      # prelock must have locked the lower row before it waits for that row.
      assert {:ok, higher} =
               Workspaces.update_work_item(higher.id, %{
                 version: higher.lock_version,
                 pull_request_url: "https://github.com/acme/symmetry/pull/" <> higher.external_id
               })

      provider_items =
        Enum.map(items, fn item ->
          %{}
          |> provider_work_item()
          |> Map.merge(%{
            external_id: item.external_id,
            title: "Updated " <> item.external_id,
            external_updated_at: ~U[2026-09-06 09:30:00.000000Z]
          })
        end)

      owner = self()
      ref = make_ref()
      delivery = sync_lock_order_task(repository, [], owner, ref)
      importer = sync_lock_order_task(tracker, provider_items, owner, ref)
      delivery_pid = delivery.pid
      importer_pid = importer.pid
      handler_id = {__MODULE__, ref}

      :ok =
        :telemetry.attach(
          handler_id,
          [:symmetry_control, :repo, :query],
          fn _event, _measurements, metadata, _config ->
            if self() == delivery_pid and
                 String.starts_with?(metadata.query, "UPDATE \"work_items\"") and
                 String.contains?(metadata.query, "\"external_pull_request_url\"") do
              send(owner, {:delivery_first_update, ref, metadata.params})

              receive do
                {:continue_delivery, ^ref} -> :ok
              after
                15_000 -> :ok
              end
            end
          end,
          nil
        )

      try do
        assert_receive {:sync_backend, ^ref, ^delivery_pid, delivery_backend}, 5_000
        assert_receive {:sync_backend, ^ref, ^importer_pid, importer_backend}, 5_000
        send(delivery_pid, {:start_sync, ref})
        assert_receive {:delivery_first_update, ^ref, params}, 5_000
        :telemetry.detach(handler_id)
        assert Ecto.UUID.dump!(higher.id) in params

        send(importer_pid, {:start_sync, ref})

        assert_sync_waiting_on_work_item(
          importer_backend,
          delivery_backend,
          System.monotonic_time(:millisecond) + 5_000
        )

        # The witness that pins the direction: in ascending order the import's
        # prelock statement already holds the lower row while it waits for the
        # delivery-held higher row. A descending prelock would block on the
        # higher row first and hold nothing.
        assert work_item_row_locked?(lower.id),
               "the import did not hold the lower work item while blocked on the higher one"

        send(delivery_pid, {:continue_delivery, ref})
        assert {:ok, _repository} = Task.await(delivery, 10_000)
        assert {:ok, _tracker} = Task.await(importer, 10_000)

        refute work_item_row_locked?(lower.id)

        higher_stored = Repo.get!(WorkItem, higher.id)
        assert higher_stored.title == "Updated " <> higher.external_id
        assert higher_stored.external_available == true
        assert higher_stored.external_pull_request_url == higher.pull_request_url
        assert higher_stored.external_review_status == "approved"
        assert higher_stored.external_ci_status == "passed"

        lower_stored = Repo.get!(WorkItem, lower.id)
        assert lower_stored.title == "Updated " <> lower.external_id
        assert lower_stored.external_available == true
        assert lower_stored.external_pull_request_url == nil
      after
        :telemetry.detach(handler_id)
        send(delivery_pid, {:continue_delivery, ref})
        Task.shutdown(delivery, :brutal_kill)
        Task.shutdown(importer, :brutal_kill)
      end
    end)
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
    resource = Repo.get!(ProjectResource, resource.id)

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

    connection = Repo.get!(Connection, connection.id)

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

  test "connection capability changes clear archived delivery projections and preserve resource sync state" do
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
    assert delivered.external_pull_request_url == "https://github.com/acme/symmetry/pull/42"
    assert delivered.external_ci_status == "passed"
    assert delivered.external_review_status == "approved"

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

    archived_repository = Repo.get!(ProjectResource, repository.id)
    archived_item = Repo.get!(WorkItem, item.id)

    assert archived_repository.lock_version == synced_repository.lock_version
    assert archived_repository.status == synced_repository.status
    assert archived_repository.sync_status == synced_repository.sync_status
    assert archived_repository.status_message == synced_repository.status_message
    assert archived_repository.metadata == synced_repository.metadata
    assert archived_repository.last_checked_at == synced_repository.last_checked_at
    assert archived_repository.last_synced_at == synced_repository.last_synced_at
    assert archived_item.lock_version > delivered.lock_version
    assert archived_item.external_pull_request_url == nil
    assert archived_item.external_pull_request_state == nil
    assert archived_item.external_review_status == nil
    assert archived_item.external_change_updated_at == nil
    assert archived_item.external_change_data == %{}
    assert archived_item.external_ci_status == nil
    assert archived_item.external_ci_updated_at == nil
    assert archived_item.external_ci_data == %{}
  end

  test "unprojected provider actions preserve sanitized failures when projected" do
    use_credential_leak_provider()
    project = project_fixture()
    connection = connection_fixture("github", "gh_cli")

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "repository",
               name: "Unprojected action failure",
               external_ref: "acme/symmetry"
             })

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Keep sanitized provider failure",
               repository_resource_id: repository.id,
               pull_request_url: "https://github.com/acme/symmetry/pull/42"
             })

    for {provider_result, message} <- [
          {{:ok, %{}}, "Provider response is invalid"},
          {{:error, {:transport, :forced_failure}}, "Provider transport failed"}
        ] do
      CredentialLeakProviderStub.respond_with(:execute, provider_result)

      assert {:error, failure = {:provider_action_failure, :provider_failure, :ambiguous, ^message}} =
               Integrations.execute_unprojected_provider_action(
                 connection,
                 repository,
                 item,
                 "change.upsert",
                 %{}
               )

      assert :ok =
               Integrations.project_provider_action_failure(
                 Repo.get!(ProjectResource, repository.id),
                 failure
               )

      assert Repo.get!(ProjectResource, repository.id).status_message ==
               message
    end
  end

  test "the callerless projected provider-action variant stays out of the module surface" do
    # The projected variant (`execute_provider_action/5` and the private
    # `execute_authenticated_provider_action/7` -> `persist_provider_delivery/5`
    # chain behind it) had no referent anywhere in the repository and was deleted
    # on branch `fix/provider-action-chain-removal`; the live path is the sibling
    # `execute_unprojected_provider_action/6` the broker calls; `/5` is its delegate. A public `def`
    # hides a missing referent from the compiler, so the surface is pinned here:
    # reviving a second variant must be a deliberate act, not an unnoticed re-add.
    # `function_exported?/3` reads the loaded module: load it first so the run
    # does not depend on another test having touched Integrations already.
    Code.ensure_loaded!(Integrations)
    refute function_exported?(Integrations, :execute_provider_action, 5)
    assert function_exported?(Integrations, :execute_unprojected_provider_action, 5)
    assert function_exported?(Integrations, :execute_unprojected_provider_action, 6)
  end

  test "delete_connection refuses a non-integer expected version as an invalid request" do
    connection = connection_fixture("github", "gh_cli")

    # One version rule for both portal contexts: a malformed expected version answers
    # `:invalid_request` (not `:stale`) — pinned so the shared version clause cannot drift.
    assert {:error, :invalid_request} = Integrations.delete_connection(connection.id, "1")
    assert {:error, :invalid_request} = Integrations.delete_connection(connection.id, nil)

    assert {:ok, deleted} = Integrations.delete_connection(connection.id, connection.lock_version)
    assert deleted.id == connection.id
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
