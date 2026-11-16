defmodule SymmetryControlWeb.PortalSessionTest do
  use SymmetryControlWeb.ConnCase, async: false

  alias SymmetryControlWeb.PortalSession

  test "session values expire and are bound to the configured operator token" do
    session = PortalSession.issue("operator-token", now: 1_000)

    assert PortalSession.valid?(session, "operator-token", now: 1_000, max_age_seconds: 60)
    assert PortalSession.valid?(session, "operator-token", now: 1_059, max_age_seconds: 60)
    refute PortalSession.valid?(session, "operator-token", now: 1_060, max_age_seconds: 60)
    refute PortalSession.valid?(session, "rotated-token", now: 1_001, max_age_seconds: 60)
    refute PortalSession.valid?(true, "operator-token", now: 1_001, max_age_seconds: 60)
  end

  test "login renews the session and logout drops it", %{conn: conn} do
    login_page = conn |> enforce_csrf() |> get("/portal/login")

    [_, csrf_token] =
      Regex.run(~r/name="_csrf_token" value="([^"]+)"/, html_response(login_page, 200))

    authenticated =
      login_page
      |> recycle()
      |> enforce_csrf()
      |> post("/portal/login", %{
        "_csrf_token" => csrf_token,
        "operator_token" => "test-operator-token"
      })

    assert redirected_to(authenticated, 302) == "/portal"

    authenticated
    |> get_resp_header("set-cookie")
    |> Enum.each(fn cookie -> refute String.contains?(String.downcase(cookie), "max-age=") end)

    assert %{issued_at: issued_at, token_fingerprint: fingerprint} =
             get_session(authenticated, :portal_operator)

    assert is_integer(issued_at)
    assert is_binary(fingerprint)

    portal_page = authenticated |> recycle() |> get("/portal/classic")

    [_, logout_token] =
      Regex.run(~r/name="_csrf_token" value="([^"]+)"/, html_response(portal_page, 200))

    logged_out =
      portal_page
      |> recycle()
      |> enforce_csrf()
      |> post("/portal/logout", %{"_csrf_token" => logout_token})

    assert redirected_to(logged_out, 302) == "/portal/login"
    assert get_session(logged_out, :portal_operator) == nil
  end

  test "authenticated bootstrap exposes only CSRF and supports shell logout", %{conn: conn} do
    bootstrap =
      conn
      |> init_test_session(%{portal_operator: PortalSession.issue("test-operator-token")})
      |> put_req_header("accept", "application/json")
      |> get("/portal/api/session")

    assert %{"csrf_token" => logout_token} = session = json_response(bootstrap, 200)
    assert Map.keys(session) == ["csrf_token"]
    assert is_binary(logout_token) and byte_size(logout_token) > 0
    assert get_resp_header(bootstrap, "cache-control") == ["no-store"]
    refute bootstrap.resp_body =~ "test-operator-token"

    logged_out =
      bootstrap
      |> recycle()
      |> enforce_csrf()
      |> put_req_header("accept", "text/html")
      |> post("/portal/logout", %{"_csrf_token" => logout_token})

    assert redirected_to(logged_out, 302) == "/portal/login"
    assert get_session(logged_out, :portal_operator) == nil
  end

  test "expired and token-rotated sessions are rejected and cleared", %{conn: conn} do
    original = Application.fetch_env!(:symmetry_control, :orchestration)
    on_exit(fn -> Application.put_env(:symmetry_control, :orchestration, original) end)

    expired = PortalSession.issue("test-operator-token", now: 1)

    expired_response =
      conn
      |> init_test_session(%{portal_operator: expired})
      |> get("/portal/projects/project-123")

    assert redirected_to(expired_response, 302) ==
             "/portal/login?return_to=%2Fportal%2Fprojects%2Fproject-123"

    assert get_session(expired_response, :portal_operator) == nil
    assert get_resp_header(expired_response, "cache-control") == ["no-store"]

    for path <- ["/portal/api/session", "/portal/api/workspace", "/portal/api/attention"] do
      expired_api =
        conn
        |> recycle()
        |> init_test_session(%{portal_operator: expired})
        |> put_req_header("accept", "application/json")
        |> get(path)

      assert %{"error" => %{"code" => "unauthenticated"}} = json_response(expired_api, 401)
      assert get_session(expired_api, :portal_operator) == nil
      assert get_resp_header(expired_api, "cache-control") == ["no-store"]
      assert get_resp_header(expired_api, "location") == []
    end

    valid = PortalSession.issue("test-operator-token")

    Application.put_env(
      :symmetry_control,
      :orchestration,
      Keyword.put(original, :operator_token, "rotated-token")
    )

    rotated_response =
      conn
      |> recycle()
      |> init_test_session(%{portal_operator: valid})
      |> put_req_header("accept", "application/json")
      |> get("/portal/api/workspace")

    assert %{"error" => %{"code" => "unauthenticated"}} = json_response(rotated_response, 401)
    assert get_session(rotated_response, :portal_operator) == nil
  end

  test "unauthenticated deep links survive login and invalid-credential retries", %{conn: conn} do
    for path <- [
          "/portal",
          "/portal?project_id=project-123",
          "/portal/attention",
          "/portal/attention?projectId=project-123",
          "/portal/connections",
          "/portal/connections?connectionId=20000000-0000-4000-8000-000000000001",
          "/portal/projects",
          "/portal/projects/project-123",
          "/portal/projects/project-123?view=board&q=keeper",
          "/portal/projects/project-123?q=Human+decision&blocked=%221%22",
          "/portal/projects/project-123?owner=agent%3Acodex",
          "/portal/projects/project-123?q=%22123%22",
          "/portal/projects/project-123?q=a%09b",
          "/portal/work/work-123",
          "/portal/work/work-123?view=board",
          "/portal/work/work-123?projectId=project-123",
          "/portal/work/work-123?view=list&q=a%09b&owner=agent%3Acodex&blocked=1&projectId=project-123",
          "/portal/runtimes",
          "/portal/classic"
        ] do
      redirect_response = conn |> get(path)

      assert get_resp_header(redirect_response, "cache-control") == ["no-store"],
             "unauthorized deep link #{path} must not be cacheable"

      login_page = conn |> enforce_csrf() |> get(redirected_to(redirect_response, 302))
      login_html = html_response(login_page, 200)
      [_, csrf_token] = Regex.run(~r/name="_csrf_token" value="([^"]+)"/, login_html)
      return_to_field = ~s(name="return_to" value="#{escaped_attribute(path)}")
      assert login_html =~ return_to_field
      assert get_resp_header(login_page, "cache-control") == ["no-store"]

      invalid =
        login_page
        |> recycle()
        |> enforce_csrf()
        |> post("/portal/login", %{
          "_csrf_token" => csrf_token,
          "operator_token" => "wrong",
          "return_to" => path
        })

      assert html_response(invalid, 401) =~ return_to_field
      assert get_resp_header(invalid, "cache-control") == ["no-store"]

      authenticated =
        invalid
        |> recycle()
        |> enforce_csrf()
        |> post("/portal/login", %{
          "_csrf_token" => csrf_token,
          "operator_token" => "test-operator-token",
          "return_to" => path
        })

      assert redirected_to(authenticated, 302) == path
    end
  end

  test "classic login preserves validated project, run, and hash destinations", %{conn: conn} do
    query = "project_id=project-123&chat_scope=run&run_id=run-456"
    target = "/portal/classic?" <> query
    unauthenticated = conn |> get(target)
    login_location = redirected_to(unauthenticated, 302)
    login_query = login_location |> URI.parse() |> Map.fetch!(:query) |> URI.decode_query()
    assert %{"return_to" => ^target} = login_query

    return_to = target <> "#chat"
    login_page = conn |> enforce_csrf() |> get("/portal/login", %{"return_to" => return_to})
    login_html = html_response(login_page, 200)
    [_, csrf_token] = Regex.run(~r/name="_csrf_token" value="([^"]+)"/, login_html)

    assert login_html =~
             ~s(name="return_to" value="/portal/classic?project_id=project-123&amp;chat_scope=run&amp;run_id=run-456#chat")

    authenticated =
      login_page
      |> recycle()
      |> enforce_csrf()
      |> post("/portal/login", %{
        "_csrf_token" => csrf_token,
        "operator_token" => "test-operator-token",
        "return_to" => return_to
      })

    assert redirected_to(authenticated, 302) == return_to

    for view <- ["board", "chat", "activity", "resources", "connections", "health"] do
      destination = "/portal/classic#" <> view
      assert PortalSession.return_to(destination) == destination
    end
  end

  test "login accepts only explicit internal shell return paths", %{conn: conn} do
    for path <- [
          "https://example.com/portal",
          "//example.com/portal",
          "/\\example.com/portal",
          "/portal/login",
          "/portal/logout",
          "/portal/api/session",
          "/portal/assets/main.js",
          "/portal/not-a-route",
          "/portal/projects/../login",
          "/portal/projects/%2e%2e",
          "/portal/connections/extra",
          "/portal/connections?connectionId=//example.com",
          "/portal/connections?return_to=//example.com",
          "/portal/connections#not-a-view",
          "/portal/projects/project-123?return_to=//example.com",
          "/portal?return_to=//example.com",
          "/portal?project_id=//example.com",
          "/portal?project_id=project-123%0d%0alocation%3ahttps://example.com",
          "/portal?chat_scope=admin",
          "/portal?project_id=project-123#not-a-view",
          "/portal/classic?return_to=//example.com",
          "/portal/classic?project_id=//example.com",
          "/portal/classic?project_id=project-123%0d%0alocation%3ahttps://example.com",
          "/portal/classic?chat_scope=admin",
          "/portal/classic#not-a-view",
          "/portal/classic/../login",
          "/portal/projects/project-123?return_to=//example.com",
          "/portal/projects/project-123?projectId=project-123",
          "/portal/projects/project-123?q=//example.com",
          "/portal/projects/project-123?q=a%0ab",
          "/portal/projects/project-123?q=a%1fb",
          "/portal/projects/project-123?q=a%00b",
          "/portal/projects/project-123?view=board#not-a-view",
          "/portal/work",
          "/portal/work/work-123#not-a-view",
          "/portal/work/work-123?view=evil",
          "/portal/work/work-123?unknown=1",
          "/portal/work/work-123?q=//example.com",
          "/portal/work/work-123?q=project-123%0d%0alocation%3ahttps://example.com",
          "/portal/work/work-123?q=a%0ab",
          "/portal/work/work-123?q=a%1fb",
          "/portal/work/work-123?q=a%00b",
          "/portal/work/work-123?projectId=//example.com",
          "/portal/work/../login",
          "/portal/work/%2e%2e",
          "/portal/runtimes?x=1",
          "/portal/runtimes/extra",
          "/portal#chat",
          "/portal\r\nlocation:https://example.com",
          "\"><script>alert(1)</script>"
        ] do
      login_page = conn |> enforce_csrf() |> get("/portal/login", %{"return_to" => path})
      login_html = html_response(login_page, 200)
      [_, csrf_token] = Regex.run(~r/name="_csrf_token" value="([^"]+)"/, login_html)
      assert login_html =~ ~s(name="return_to" value="/portal")

      authenticated =
        login_page
        |> recycle()
        |> enforce_csrf()
        |> post("/portal/login", %{
          "_csrf_token" => csrf_token,
          "operator_token" => "test-operator-token",
          "return_to" => path
        })

      assert redirected_to(authenticated, 302) == "/portal"
    end
  end

  test "anonymous bootstrap rejects bearer credentials and logout still enforces CSRF", %{
    conn: conn
  } do
    anonymous =
      conn
      |> put_req_header("accept", "application/json")
      |> put_req_header("authorization", "Bearer test-operator-token")
      |> get("/portal/api/session")

    assert %{"error" => %{"code" => "unauthenticated"}} = json_response(anonymous, 401)
    assert get_resp_header(anonymous, "cache-control") == ["no-store"]

    assert_error_sent 403, fn ->
      conn
      |> init_test_session(%{portal_operator: PortalSession.issue("test-operator-token")})
      |> enforce_csrf()
      |> post("/portal/logout")
    end
  end

  defp enforce_csrf(conn),
    do: %{conn | private: Map.delete(conn.private, :plug_skip_csrf_protection)}

  defp escaped_attribute(path), do: String.replace(path, "&", "&amp;")
end
