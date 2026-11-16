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
           |> redirected_to(302) == "/portal/login?return_to=%2Fportal"

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
    assert body =~ ~s(<div id="root")
    assert body =~ "/portal/assets/"
    refute body =~ @operator_token
    assert get_resp_header(page, "cache-control") == ["no-store"]

    logged_out = page |> recycle() |> skip_csrf() |> post("/portal/logout")
    assert redirected_to(logged_out, 302) == "/portal/login"
    refute get_session(logged_out, :portal_operator)
  end

  test "every explicit shell deep link serves the actual built entry with request CSRF" do
    index = File.read!(portal_index_path())
    assert index =~ ~s(<meta name="csrf-token" content="__SYMMETRY_CSRF__")

    for path <- [
          "/portal",
          "/portal/attention",
          "/portal/connections",
          "/portal/projects",
          "/portal/projects/project-123",
          "/portal/work/work-123"
        ] do
      page = api_conn() |> put_req_header("accept", "text/html") |> get(path)
      body = html_response(page, 200)
      [_, csrf_token] = Regex.run(~r/<meta name="csrf-token" content="([^"]+)"/, body)

      assert csrf_token != "__SYMMETRY_CSRF__"
      assert byte_size(csrf_token) > 0
      assert body == String.replace(index, "__SYMMETRY_CSRF__", csrf_token)
      assert get_resp_header(page, "cache-control") == ["no-store"]
    end
  end

  test "the authenticated classic document retains existing workspace flows without loading React" do
    page =
      api_conn()
      |> put_req_header("accept", "text/html")
      |> get("/portal/classic?project_id=project-123")

    body = html_response(page, 200)
    assert body =~ "Symmetry"
    assert body =~ "Engineering workspace"
    assert body =~ "id=\"kanban-board\""
    assert body =~ "id=\"chat-form\""
    assert body =~ "id=\"resource-dialog\""
    assert body =~ "/assets/portal.css"
    assert body =~ "/assets/portal.js"
    assert body =~ ~s(href="/portal/classic" aria-label="Symmetry workspace")
    assert body =~ ~s(href="/portal" aria-label="Workspace shell")
    assert get_resp_header(page, "cache-control") == ["no-store"]
    refute body =~ "/portal/assets/"
    refute body =~ ~s(<div id="root")
  end

  test "classic source assets remain served from the release priv directory", %{conn: conn} do
    assert conn |> get("/assets/portal.css") |> response(200) =~ "--surface"
    assert conn |> get("/assets/portal.js") |> response(200) =~ "loadWorkspace"
  end

  test "built hashed assets are public and immutable while login CSS remains available", %{
    conn: conn
  } do
    index = File.read!(portal_index_path())

    assets =
      ~r/(?:src|href)="(\/portal\/assets\/[^"?]+\.(?:js|css))"/
      |> Regex.scan(index, capture: :all_but_first)
      |> List.flatten()

    assert Enum.any?(assets, &String.ends_with?(&1, ".js"))
    assert Enum.any?(assets, &String.ends_with?(&1, ".css"))

    for asset <- assets do
      assert Path.basename(asset) =~ ~r/-[A-Za-z0-9_-]+\.(?:js|css)\z/

      for suffix <- ["", "?vsn=release"] do
        served = conn |> get(asset <> suffix)
        assert response(served, 200) != ""

        assert get_resp_header(served, "cache-control") == [
                 "public, max-age=31536000, immutable"
               ]

        refute get_resp_header(served, "content-type") == ["text/html; charset=utf-8"]
      end
    end

    assert conn |> get("/assets/portal.css") |> response(200) =~ ".login-panel"
  end

  test "unknown API, asset, and shell paths remain real 404s instead of SPA HTML" do
    for path <- [
          "/portal/api/not-a-route",
          "/portal/api/projects/not-a-route",
          "/portal/assets/missing-123.js",
          "/portal/assets/index.html",
          "/portal/not-a-route",
          "/portal/projects/project-123/not-a-route",
          "/portal/work",
          "/portal/work/work-123/not-a-route",
          "/portal/classic/not-a-route",
          "/portal/index.html"
        ] do
      assert %{"error" => %{"code" => "not_found"}} =
               api_conn() |> get(path) |> json_response(404)
    end
  end

  test "a missing built entry returns an explicit uncacheable 503, not a fallback workspace" do
    index_path = portal_index_path()
    unavailable_path = index_path <> ".unavailable-#{System.unique_integer([:positive])}"
    File.rename!(index_path, unavailable_path)

    try do
      unavailable =
        api_conn() |> put_req_header("accept", "text/html") |> get("/portal/attention")

      assert response(unavailable, 503) =~ "Workspace assets are unavailable"
      assert get_resp_header(unavailable, "cache-control") == ["no-store"]
      refute unavailable.resp_body =~ "<html"

      classic = api_conn() |> put_req_header("accept", "text/html") |> get("/portal/classic")
      assert html_response(classic, 200) =~ "id=\"kanban-board\""
    after
      File.rename!(unavailable_path, index_path)
    end
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

  defp portal_index_path,
    do: Application.app_dir(:symmetry_control, "priv/static/portal/index.html")

  defp skip_csrf(conn), do: put_private(conn, :plug_skip_csrf_protection, true)
end
