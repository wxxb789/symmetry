defmodule SymmetryControl.Integrations.AggregateProviderStub do
  @moduledoc false
  @behaviour SymmetryControl.Integrations.Provider

  alias SymmetryControl.Integrations.ProviderStub

  @impl true
  def authenticate(_connection), do: {:ok, :stub_auth}

  @impl true
  def validate_resource_reference(connection, kind, reference),
    do: ProviderStub.validate_resource_reference(connection, kind, reference)

  @impl true
  def check(connection, auth), do: ProviderStub.check(connection, auth)

  @impl true
  def sync_resource(%{name: "Forbidden sync"}, _resource, :stub_auth),
    do: {:error, {:http, 403, "forbidden"}}

  def sync_resource(%{name: "Unauthorized sync"}, _resource, :stub_auth),
    do: {:error, {:http, 401, "unauthorized"}}

  def sync_resource(%{name: "Provider failure sync"}, _resource, :stub_auth),
    do: {:error, {:transport, :closed}}

  def sync_resource(connection, resource, auth),
    do: ProviderStub.sync_resource(connection, resource, auth)

  @impl true
  def sync_delivery(connection, resource, work_item, auth),
    do: ProviderStub.sync_delivery(connection, resource, work_item, auth)

  @impl true
  def sync_ci(connection, resource, work_item, auth),
    do: ProviderStub.sync_ci(connection, resource, work_item, auth)

  @impl true
  def execute(_connection, _resource, _work_item, _operation, _input, _auth),
    do: {:error, :unsupported_resource}
end

defmodule SymmetryControlWeb.PortalIntegrationsTest do
  use SymmetryControlWeb.ConnCase, async: false

  alias SymmetryControlWeb.PortalSession

  @operator_token "test-operator-token"

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

  test "changes capability requires repository access on create and update" do
    assert %{
             "error" => %{
               "code" => "validation_failed",
               "fields" => %{"capabilities" => ["changes requires repositories"]}
             }
           } =
             api_conn()
             |> post("/portal/api/connections", %{
               "provider" => "github",
               "name" => "Invalid changes only",
               "account_ref" => "acme",
               "capabilities" => ["changes"]
             })
             |> json_response(422)

    assert %{"connection" => connection} =
             api_conn()
             |> post("/portal/api/connections", %{
               "provider" => "github",
               "name" => "Valid changes",
               "account_ref" => "acme",
               "capabilities" => ["repositories", "changes"]
             })
             |> json_response(201)

    assert %{
             "error" => %{
               "code" => "validation_failed",
               "fields" => %{"capabilities" => ["changes requires repositories"]}
             }
           } =
             api_conn()
             |> patch("/portal/api/connections/#{connection["id"]}", %{
               "version" => connection["version"],
               "capabilities" => ["changes"]
             })
             |> json_response(422)
  end

  test "portal connects a provider, binds resources, syncs work, and never returns credentials" do
    assert %{"connection" => connection} =
             api_conn()
             |> post("/portal/api/connections", %{
               "provider" => "github",
               "name" => "GitHub engineering",
               "account_ref" => "acme",
               "auth_type" => "gh_cli",
               "capabilities" => ["repositories", "work_items", "changes", "ci"]
             })
             |> json_response(201)

    refute Map.has_key?(connection, "credential")
    refute Map.has_key?(connection, "credential_ciphertext")
    assert connection["auth_type"] == "gh_cli"

    assert %{"error" => %{"code" => "invalid_request"}} =
             api_conn()
             |> post("/portal/api/connections", %{
               "provider" => "github",
               "name" => "Unsafe secret",
               "account_ref" => "acme",
               "auth_type" => "gh_cli",
               "credential" => "must-not-be-accepted",
               "capabilities" => ["work_items"]
             })
             |> json_response(400)

    assert %{"connection" => checked} =
             api_conn()
             |> post("/portal/api/connections/#{connection["id"]}/check")
             |> json_response(200)

    assert checked["status"] == "healthy"

    project = create_project()

    assert %{"resource" => repository} =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/resources", %{
               "connection_id" => connection["id"],
               "kind" => "repository",
               "name" => "symmetry",
               "external_ref" => "acme/symmetry"
             })
             |> json_response(201)

    assert %{"resource" => tracker} =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/resources", %{
               "connection_id" => connection["id"],
               "kind" => "work_tracking",
               "name" => "GitHub Issues",
               "external_ref" => "acme/symmetry"
             })
             |> json_response(201)

    assert %{"resource" => synced} =
             api_conn()
             |> post("/portal/api/resources/#{tracker["id"]}/sync")
             |> json_response(200)

    assert synced["sync_status"] == "synced"

    assert %{"resource" => _repository_scope} =
             api_conn()
             |> post("/portal/api/resources/#{repository["id"]}/sync")
             |> json_response(200)

    assert %{"projects" => [workspace], "connections" => [listed_connection]} =
             api_conn()
             |> get("/portal/api/workspace?project_id=#{project["id"]}")
             |> json_response(200)

    refute Map.has_key?(listed_connection, "credential_ciphertext")

    [item] = workspace["work_items"]
    assert item["external"]["provider"] == "github"
    assert item["external"]["id"] == "101"
    assert item["external"]["state"] == "open"
    assert item["external"]["available"] == true
    assert item["external"]["assignee"] == "Ada Lovelace"
    assert item["external"]["labels"] == ["agent-ready", "priority:high"]
    assert "priority" in item["external"]["provider_owned_fields"]
    assert item["repository_resource_id"] == repository["id"]

    assert %{"error" => %{"code" => "provider_owned"}} =
             api_conn()
             |> patch("/portal/api/work-items/#{item["id"]}", %{
               "version" => item["version"],
               "title" => "Local overwrite"
             })
             |> json_response(409)

    assert %{"error" => %{"code" => "provider_owned"}} =
             api_conn()
             |> patch("/portal/api/work-items/#{item["id"]}", %{
               "version" => item["version"],
               "priority" => "low"
             })
             |> json_response(409)

    assert %{"work_item" => assigned} =
             api_conn()
             |> patch("/portal/api/work-items/#{item["id"]}", %{
               "version" => item["version"],
               "assignee_type" => "agent",
               "assignee_name" => "Codex",
               "agent_profile" => "codex",
               "workspace" => "primary",
               "branch" => "codex/external-101",
               "pull_request_url" => "https://github.com/acme/symmetry/pull/42"
             })
             |> json_response(200)

    assert %{"resource" => _synced_repository} =
             api_conn()
             |> post("/portal/api/resources/#{repository["id"]}/sync")
             |> json_response(200)

    assert %{"projects" => [refreshed]} =
             api_conn()
             |> get("/portal/api/workspace?project_id=#{project["id"]}")
             |> json_response(200)

    delivered = Enum.find(refreshed["work_items"], &(&1["id"] == assigned["id"]))
    assert delivered["delivery"]["pull_request"]["source"] == "provider"
    assert delivered["ci_status"] == "passed"
    assert delivered["review_status"] == "approved"

    assert %{"execution" => %{"state" => "queued"}} =
             api_conn()
             |> post("/portal/api/work-items/#{assigned["id"]}/run", %{
               "action_id" => "github-connected-run"
             })
             |> json_response(202)
  end

  test "Azure Boards work can use an Azure Repo and enter the agent workflow" do
    assert %{"connection" => connection} =
             api_conn()
             |> post("/portal/api/connections", %{
               "provider" => "azure_devops",
               "name" => "Azure engineering",
               "account_ref" => "acme",
               "auth_type" => "entra_id",
               "capabilities" => ["repositories", "work_items", "changes", "ci"]
             })
             |> json_response(201)

    project = create_project("AZDO")

    assert %{"resource" => repository} =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/resources", %{
               "connection_id" => connection["id"],
               "kind" => "repository",
               "name" => "Azure Repo",
               "external_ref" => "Platform/symmetry"
             })
             |> json_response(201)

    assert %{"resource" => tracker} =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/resources", %{
               "connection_id" => connection["id"],
               "kind" => "work_tracking",
               "name" => "Azure Boards",
               "external_ref" => "Platform"
             })
             |> json_response(201)

    assert %{"resource" => %{"sync_status" => "synced"}} =
             api_conn()
             |> post("/portal/api/resources/#{tracker["id"]}/sync")
             |> json_response(200)

    assert %{"resource" => %{"sync_status" => "synced"}} =
             api_conn()
             |> post("/portal/api/resources/#{repository["id"]}/sync")
             |> json_response(200)

    assert %{"projects" => [workspace]} =
             api_conn()
             |> get("/portal/api/workspace?project_id=#{project["id"]}")
             |> json_response(200)

    [item] = workspace["work_items"]
    assert item["external"]["provider"] == "azure_devops"

    assert %{"work_item" => assigned} =
             api_conn()
             |> patch("/portal/api/work-items/#{item["id"]}", %{
               "version" => item["version"],
               "assignee_type" => "agent",
               "assignee_name" => "Codex",
               "agent_profile" => "codex",
               "workspace" => "primary",
               "repository_resource_id" => repository["id"],
               "branch" => "codex/external-101"
             })
             |> json_response(200)

    assert %{"execution" => %{"state" => "queued"}, "work_item" => launched} =
             api_conn()
             |> post("/portal/api/work-items/#{assigned["id"]}/run", %{
               "action_id" => "azure-connected-run"
             })
             |> json_response(202)

    assert launched["external"]["id"] == "101"
  end

  test "connection failures remain visible and in-use connections cannot be deleted" do
    assert %{"connection" => connection} =
             api_conn()
             |> post("/portal/api/connections", %{
               "provider" => "azure_devops",
               "name" => "Forbidden",
               "account_ref" => "acme",
               "auth_type" => "entra_id",
               "capabilities" => ["work_items"]
             })
             |> json_response(201)

    assert %{"error" => %{"code" => "forbidden"}, "connection" => degraded} =
             api_conn()
             |> post("/portal/api/connections/#{connection["id"]}/check")
             |> json_response(403)

    assert degraded["status"] == "degraded"
    assert degraded["status_message"] =~ "permission"

    project = create_project()

    assert %{"resource" => _resource} =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/resources", %{
               "connection_id" => connection["id"],
               "kind" => "work_tracking",
               "name" => "Azure Boards",
               "external_ref" => "Platform"
             })
             |> json_response(201)

    assert %{"health" => %{"connections" => "degraded"}} =
             api_conn()
             |> get("/portal/api/workspace?project_id=#{project["id"]}")
             |> json_response(200)

    assert %{"error" => %{"code" => "validation_failed"}} =
             api_conn()
             |> delete("/portal/api/connections/#{connection["id"]}", %{
               "version" => degraded["version"]
             })
             |> json_response(422)
  end

  test "provider authentication failures do not masquerade as an expired portal session" do
    assert %{"connection" => connection} =
             api_conn()
             |> post("/portal/api/connections", %{
               "provider" => "github",
               "name" => "Unauthorized",
               "account_ref" => "acme",
               "capabilities" => ["work_items"]
             })
             |> json_response(201)

    assert %{
             "error" => %{"code" => "provider_unauthorized"},
             "connection" => %{"status" => "degraded"}
           } =
             api_conn()
             |> post("/portal/api/connections/#{connection["id"]}/check")
             |> json_response(502)
  end

  test "project sync preserves stale resource conflicts" do
    Application.put_env(:symmetry_control, :integration_provider_test_controller, self())

    on_exit(fn ->
      Application.delete_env(:symmetry_control, :integration_provider_test_controller)
    end)

    connection = create_connection("Fail after archive")
    replacement = create_connection("Replacement connection")
    project = create_project("STALE")
    resource = create_tracker(project, connection, "Stale binding")

    sync_task =
      Task.async(fn ->
        api_conn()
        |> post("/portal/api/projects/#{project["id"]}/sync")
      end)

    assert_receive {:integration_provider_waiting, provider_pid}

    assert %{"resource" => _rebound} =
             api_conn()
             |> patch("/portal/api/resources/#{resource["id"]}", %{
               "version" => resource["version"],
               "connection_id" => replacement["id"],
               "external_ref" => "acme/rebound"
             })
             |> json_response(200)

    send(provider_pid, :continue)

    assert %{
             "error" => %{
               "code" => "stale",
               "causes" => [
                 %{"code" => "stale", "count" => 1, "http_status" => 409}
               ]
             },
             "results" => [
               %{
                 "resource_id" => resource_id,
                 "status" => "failed",
                 "reason" => "stale",
                 "error" => %{"code" => "stale", "http_status" => 409}
               }
             ]
           } =
             sync_task
             |> Task.await()
             |> json_response(409)

    assert resource_id == resource["id"]
  end

  test "project sync preserves state conflicts raised during aggregate synchronization" do
    Application.put_env(:symmetry_control, :integration_provider_test_controller, self())

    on_exit(fn ->
      Application.delete_env(:symmetry_control, :integration_provider_test_controller)
    end)

    connection = create_connection("Fail after archive")
    project = create_project("STATE")
    resource = create_tracker(project, connection, "Archive race")

    sync_task =
      Task.async(fn ->
        api_conn()
        |> post("/portal/api/projects/#{project["id"]}/sync")
      end)

    assert_receive {:integration_provider_waiting, provider_pid}

    assert %{"project" => %{"status" => "archived"}} =
             api_conn()
             |> patch("/portal/api/projects/#{project["id"]}", %{
               "version" => project["version"],
               "status" => "archived"
             })
             |> json_response(200)

    send(provider_pid, :continue)

    assert %{
             "error" => %{
               "code" => "state_conflict",
               "causes" => [
                 %{"code" => "state_conflict", "count" => 1, "http_status" => 409}
               ]
             },
             "results" => [
               %{
                 "resource_id" => resource_id,
                 "status" => "failed",
                 "reason" => "state_conflict",
                 "error" => %{"code" => "state_conflict", "http_status" => 409}
               }
             ]
           } =
             sync_task
             |> Task.await()
             |> json_response(409)

    assert resource_id == resource["id"]
  end

  test "project sync reports mixed provider failures without collapsing their classifications" do
    Application.put_env(:symmetry_control, :integration_providers,
      github: SymmetryControl.Integrations.AggregateProviderStub,
      azure_devops: SymmetryControl.Integrations.AggregateProviderStub
    )

    project = create_project("MIXFAIL")

    resources =
      [
        {"Healthy sync", "Healthy issues"},
        {"Forbidden sync", "Forbidden issues"},
        {"Unauthorized sync", "Unauthorized issues"},
        {"Provider failure sync", "Unavailable issues"}
      ]
      |> Enum.map(fn {connection_name, resource_name} ->
        connection = create_connection(connection_name)
        create_tracker(project, connection, resource_name)
      end)

    assert %{
             "error" => %{
               "code" => "multiple_failures",
               "causes" => causes
             },
             "results" => results
           } =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/sync")
             |> json_response(422)

    assert Enum.map(causes, &Map.take(&1, ["code", "count", "http_status"])) == [
             %{"code" => "forbidden", "count" => 1, "http_status" => 403},
             %{"code" => "provider_failure", "count" => 1, "http_status" => 502},
             %{"code" => "provider_unauthorized", "count" => 1, "http_status" => 502}
           ]

    results_by_id = Map.new(results, &{&1["resource_id"], &1})
    [healthy, forbidden, unauthorized, provider_failure] = resources

    assert results_by_id[healthy["id"]] == %{
             "resource_id" => healthy["id"],
             "status" => "synced"
           }

    assert get_in(results_by_id, [forbidden["id"], "error", "code"]) == "forbidden"

    assert get_in(results_by_id, [unauthorized["id"], "error", "code"]) ==
             "provider_unauthorized"

    assert get_in(results_by_id, [provider_failure["id"], "error", "code"]) ==
             "provider_failure"
  end

  test "Azure Boards work can use GitHub changes and Azure Pipelines CI" do
    assert %{"connection" => azure} =
             api_conn()
             |> post("/portal/api/connections", %{
               "provider" => "azure_devops",
               "name" => "Azure work and CI",
               "account_ref" => "acme",
               "capabilities" => ["work_items", "ci"]
             })
             |> json_response(201)

    assert %{"connection" => github} =
             api_conn()
             |> post("/portal/api/connections", %{
               "provider" => "github",
               "name" => "GitHub changes",
               "account_ref" => "acme",
               "capabilities" => ["repositories", "changes"]
             })
             |> json_response(201)

    project = create_project("MIXED")

    assert %{"resource" => repository} =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/resources", %{
               "connection_id" => github["id"],
               "kind" => "repository",
               "name" => "GitHub repository",
               "external_ref" => "acme/symmetry"
             })
             |> json_response(201)

    assert %{"resource" => tracker} =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/resources", %{
               "connection_id" => azure["id"],
               "kind" => "work_tracking",
               "name" => "Azure Boards",
               "external_ref" => "Platform"
             })
             |> json_response(201)

    assert %{"resource" => ci} =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/resources", %{
               "connection_id" => azure["id"],
               "kind" => "ci",
               "name" => "Azure Pipelines",
               "external_ref" => "Platform/pipeline/42"
             })
             |> json_response(201)

    assert %{"resource" => _tracker} =
             api_conn()
             |> post("/portal/api/resources/#{tracker["id"]}/sync")
             |> json_response(200)

    assert %{"resource" => _repository_scope} =
             api_conn()
             |> post("/portal/api/resources/#{repository["id"]}/sync")
             |> json_response(200)

    assert %{"projects" => [workspace]} =
             api_conn()
             |> get("/portal/api/workspace?project_id=#{project["id"]}")
             |> json_response(200)

    [item] = workspace["work_items"]

    assert %{"work_item" => assigned} =
             api_conn()
             |> patch("/portal/api/work-items/#{item["id"]}", %{
               "version" => item["version"],
               "assignee_type" => "agent",
               "assignee_name" => "Codex",
               "agent_profile" => "codex",
               "workspace" => "primary",
               "repository_resource_id" => repository["id"],
               "ci_resource_id" => ci["id"],
               "branch" => "codex/external-101",
               "pull_request_url" => "https://github.com/acme/symmetry/pull/42"
             })
             |> json_response(200)

    assert %{"resource" => _repository} =
             api_conn()
             |> post("/portal/api/resources/#{repository["id"]}/sync")
             |> json_response(200)

    assert %{"resource" => _ci} =
             api_conn()
             |> post("/portal/api/resources/#{ci["id"]}/sync")
             |> json_response(200)

    assert %{"projects" => [refreshed]} =
             api_conn()
             |> get("/portal/api/workspace?project_id=#{project["id"]}")
             |> json_response(200)

    delivered = Enum.find(refreshed["work_items"], &(&1["id"] == assigned["id"]))
    assert delivered["external"]["provider"] == "azure_devops"
    assert delivered["delivery"]["pull_request"]["provider"] == "github"
    assert delivered["delivery"]["review"]["provider"] == "github"
    assert delivered["delivery"]["ci"]["provider"] == "azure_devops"
    assert delivered["delivery"]["pull_request"]["status"] == "open"
    assert delivered["ci_status"] == "passed"

    assert %{"execution" => %{"state" => "queued"}} =
             api_conn()
             |> post("/portal/api/work-items/#{assigned["id"]}/run", %{
               "action_id" => "mixed-provider-run"
             })
             |> json_response(202)
  end

  defp create_project(key \\ "CONN") do
    %{"project" => project} =
      api_conn()
      |> post("/portal/api/projects", %{
        "name" => "Connected engineering",
        "key" => key,
        "default_agent_profile" => "codex",
        "default_workspace" => "primary"
      })
      |> json_response(201)

    project
  end

  defp create_connection(name) do
    %{"connection" => connection} =
      api_conn()
      |> post("/portal/api/connections", %{
        "provider" => "github",
        "name" => name,
        "account_ref" => "acme",
        "capabilities" => ["work_items"]
      })
      |> json_response(201)

    connection
  end

  defp create_tracker(project, connection, name) do
    %{"resource" => resource} =
      api_conn()
      |> post("/portal/api/projects/#{project["id"]}/resources", %{
        "connection_id" => connection["id"],
        "kind" => "work_tracking",
        "name" => name,
        "external_ref" => "acme/symmetry"
      })
      |> json_response(201)

    resource
  end

  defp api_conn do
    Phoenix.ConnTest.build_conn()
    |> init_test_session(%{portal_operator: PortalSession.issue(@operator_token)})
    |> skip_csrf()
    |> put_req_header("accept", "application/json")
  end

  defp skip_csrf(conn), do: put_private(conn, :plug_skip_csrf_protection, true)
end
