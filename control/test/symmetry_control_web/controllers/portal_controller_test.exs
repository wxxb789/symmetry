defmodule SymmetryControlWeb.PortalControllerTest do
  use SymmetryControlWeb.ConnCase, async: false

  alias SymmetryControlWeb.PortalSession

  @operator_token "test-operator-token"

  test "the portal requires an operator session and supports login and logout", %{conn: conn} do
    assert conn
           |> get("/")
           |> redirected_to(302) == "/portal"

    assert conn
           |> get("/portal")
           |> redirected_to(302) == "/portal/login"

    assert %{"error" => %{"code" => "unauthenticated"}} =
             conn
             |> put_req_header("accept", "application/json")
             |> get("/portal/api/workspace")
             |> json_response(401)

    invalid =
      conn
      |> skip_csrf()
      |> post("/portal/login", %{"operator_token" => "wrong"})

    assert html_response(invalid, 401) =~ "Operator token is invalid"

    authenticated =
      conn
      |> recycle()
      |> skip_csrf()
      |> post("/portal/login", %{"operator_token" => @operator_token})

    assert redirected_to(authenticated, 302) == "/portal"
    assert get_session(authenticated, :portal_operator)

    page = authenticated |> recycle() |> get("/portal")
    body = html_response(page, 200)
    assert body =~ "Symmetry"
    assert body =~ "Engineering workspace"
    assert body =~ "id=\"kanban-board\""
    assert body =~ "/assets/portal.css"
    assert body =~ "/assets/portal.js"

    logged_out = page |> recycle() |> skip_csrf() |> post("/portal/logout")
    assert redirected_to(logged_out, 302) == "/portal/login"
    refute get_session(logged_out, :portal_operator)
  end

  test "portal source assets are served from the release priv directory", %{conn: conn} do
    assert conn |> get("/assets/portal.css") |> response(200) =~ "--surface"
    assert conn |> recycle() |> get("/assets/portal.js") |> response(200) =~ "loadWorkspace"
  end

  test "portal APIs manage projects, resources, work items, and Kanban state", %{conn: _conn} do
    assert %{"project" => project} =
             api_conn()
             |> post("/portal/api/projects", %{
               "name" => "Symmetry",
               "key" => "SYM",
               "description" => "Daily engineering control surface",
               "default_agent_profile" => "codex",
               "default_workspace" => "primary"
             })
             |> json_response(201)

    assert %{"resource" => repository} =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/resources", %{
               "kind" => "repository",
               "name" => "symmetry",
               "provider" => "github",
               "url" => "https://github.com/acme/symmetry",
               "status" => "healthy"
             })
             |> json_response(201)

    assert %{"work_item" => item} =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/work-items", %{
               "title" => "Build project portal",
               "description" => "Create the coherent Goal 2 workflow.",
               "status" => "ready",
               "priority" => "urgent",
               "assignee_type" => "human",
               "assignee_name" => "Lina",
               "repository" => "acme/symmetry",
               "ci_status" => "pending",
               "review_status" => "required"
             })
             |> json_response(201)

    assert item["key"] =~ "SYM-"

    assert %{"work_item" => edited} =
             api_conn()
             |> patch("/portal/api/work-items/#{item["id"]}", %{
               "version" => item["version"],
               "blocked" => true,
               "blocker" => "Waiting for architecture review",
               "ci_status" => "passed",
               "pull_request_url" => "https://github.com/acme/symmetry/pull/42"
             })
             |> json_response(200)

    assert %{"work_item" => moved} =
             api_conn()
             |> patch("/portal/api/work-items/#{item["id"]}/move", %{
               "version" => edited["version"],
               "status" => "review"
             })
             |> json_response(200)

    assert moved["status"] == "review"
    assert moved["blocked"]
    assert moved["ci_status"] == "passed"

    assert %{
             "selected_project_id" => selected_project_id,
             "projects" => [workspace_project],
             "runtimes" => [],
             "health" => health
           } =
             api_conn()
             |> get("/portal/api/workspace?project_id=#{project["id"]}")
             |> json_response(200)

    assert selected_project_id == project["id"]
    assert Enum.map(workspace_project["resources"], & &1["id"]) == [repository["id"]]
    assert Enum.map(workspace_project["work_items"], & &1["id"]) == [item["id"]]
    assert health["connections"] == "healthy"
    assert health["runtimes"] == "offline"

    assert %{"resource" => updated_resource} =
             api_conn()
             |> patch("/portal/api/resources/#{repository["id"]}", %{
               "version" => repository["version"],
               "status" => "degraded",
               "sync_status" => "failed",
               "status_message" => "Webhook delivery is delayed",
               "metadata" => %{"message" => "Webhook delivery is delayed"}
             })
             |> json_response(200)

    assert updated_resource["status"] == "degraded"

    assert %{
             "health" => %{
               "connections" => "degraded",
               "synchronization" => "attention"
             }
           } =
             api_conn()
             |> get("/portal/api/workspace?project_id=#{project["id"]}")
             |> json_response(200)
  end

  test "an authenticated empty workspace returns a stable onboarding state", %{conn: _conn} do
    assert %{
             "selected_project_id" => nil,
             "projects" => [],
             "runtimes" => [],
             "health" => %{
               "connections" => "unknown",
               "runtimes" => "offline",
               "executions" => "idle",
               "synchronization" => "unknown"
             }
           } =
             api_conn()
             |> get("/portal/api/workspace")
             |> json_response(200)
  end

  test "agent launch is idempotent and detail separates outcomes from raw execution data", %{
    conn: _conn
  } do
    project = create_project()
    item = create_work_item(project["id"])

    assert %{
             "disposition" => "created",
             "work_item" => launched,
             "execution" => execution
           } =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/run", %{"action_id" => "start-test"})
             |> json_response(202)

    assert launched["status"] == "in_progress"
    assert launched["assignee"]["type"] == "agent"
    assert execution["state"] == "queued"

    assert %{"disposition" => "replayed", "execution" => replayed} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/run", %{"action_id" => "start-test"})
             |> json_response(200)

    assert replayed["task_id"] == execution["task_id"]

    assert %{
             "work_item" => detail_item,
             "outcome" => outcome,
             "timeline" => [],
             "raw" => %{"task" => raw_task}
           } =
             api_conn()
             |> get("/portal/api/work-items/#{item["id"]}")
             |> json_response(200)

    assert detail_item["id"] == item["id"]
    assert outcome["phase"] == "queued"
    assert outcome["goal"] == item["title"] <> "\n\nShow useful results before raw trace data."
    assert raw_task["task_id"] == execution["task_id"]
  end

  test "portal APIs return field errors for invalid work instead of creating partial state", %{
    conn: _conn
  } do
    project = create_project()

    assert %{
             "error" => %{
               "code" => "validation_failed",
               "fields" => %{"blocker" => ["must be present when blocked"]}
             }
           } =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/work-items", %{
               "title" => "Blocked without context",
               "blocked" => true
             })
             |> json_response(422)
  end

  test "portal cancellation uses the durable task command path", %{conn: _conn} do
    project = create_project()
    item = create_work_item(project["id"])

    assert %{"execution" => %{"state" => "queued"}} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/run", %{"action_id" => "start-test"})
             |> json_response(202)

    assert %{"command" => %{"kind" => "cancel", "state" => "applied"}} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/cancel", %{
               "action_id" => "cancel-test-action",
               "generation" => 1
             })
             |> json_response(202)

    assert %{"raw" => %{"task" => %{"state" => "cancelled"}}} =
             api_conn()
             |> get("/portal/api/work-items/#{item["id"]}")
             |> json_response(200)
  end

  defp create_project do
    %{"project" => project} =
      api_conn()
      |> post("/portal/api/projects", %{
        "name" => "Symmetry",
        "key" => "SYM",
        "default_agent_profile" => "codex",
        "default_workspace" => "primary"
      })
      |> json_response(201)

    project
  end

  defp create_work_item(project_id) do
    %{"work_item" => item} =
      api_conn()
      |> post("/portal/api/projects/#{project_id}/work-items", %{
        "title" => "Summarize agent outcomes",
        "description" => "Show useful results before raw trace data.",
        "status" => "ready",
        "priority" => "high",
        "assignee_type" => "agent",
        "agent_profile" => "codex",
        "repository" => "acme/symmetry"
      })
      |> json_response(201)

    item
  end

  defp api_conn do
    Phoenix.ConnTest.build_conn()
    |> init_test_session(%{portal_operator: PortalSession.issue(@operator_token)})
    |> skip_csrf()
    |> put_req_header("accept", "application/json")
  end

  defp skip_csrf(conn), do: put_private(conn, :plug_skip_csrf_protection, true)
end
