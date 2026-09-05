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

    portal_page = authenticated |> recycle() |> get("/portal")

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

  test "expired and token-rotated sessions are rejected and cleared", %{conn: conn} do
    original = Application.fetch_env!(:symmetry_control, :orchestration)
    on_exit(fn -> Application.put_env(:symmetry_control, :orchestration, original) end)

    expired = PortalSession.issue("test-operator-token", now: 1)

    expired_response =
      conn
      |> init_test_session(%{portal_operator: expired})
      |> get("/portal")

    assert redirected_to(expired_response, 302) == "/portal/login"
    assert get_session(expired_response, :portal_operator) == nil

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

  defp enforce_csrf(conn),
    do: %{conn | private: Map.delete(conn.private, :plug_skip_csrf_protection)}
end
