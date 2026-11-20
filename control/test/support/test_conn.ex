defmodule SymmetryControlWeb.TestConn do
  @moduledoc """
  The controller tests' shared connection helpers.

  One authenticated operator session, a JSON `accept` header and disabled CSRF
  are the mechanics every portal read and write starts from; the protocol error
  assertion is the shape every refused command test pins.
  """
  import ExUnit.Assertions
  import Phoenix.ConnTest, only: [init_test_session: 2, json_response: 2]
  import Plug.Conn, only: [put_private: 3, put_req_header: 3]

  alias SymmetryControlWeb.PortalSession

  @operator_token "test-operator-token"

  @doc "The operator token every portal session and operator bearer header carries."
  def operator_token, do: @operator_token

  @doc "A portal JSON connection with an authenticated operator session and CSRF disabled."
  def api_conn do
    Phoenix.ConnTest.build_conn()
    |> init_test_session(%{portal_operator: PortalSession.issue(@operator_token)})
    |> put_private(:plug_skip_csrf_protection, true)
    |> put_req_header("accept", "application/json")
  end

  @doc "Adds the JSON `accept` header and the operator bearer token to `conn`."
  def operator_conn(conn) do
    conn
    |> put_req_header("accept", "application/json")
    |> put_req_header("authorization", "Bearer #{@operator_token}")
  end

  @doc "Asserts the staged error body's protocol `code` for `status`."
  def assert_error(conn, status, code) do
    assert %{"error" => %{"code" => ^code}} = json_response(conn, status)
  end
end
