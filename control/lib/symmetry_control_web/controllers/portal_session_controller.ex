defmodule SymmetryControlWeb.PortalSessionController do
  use SymmetryControlWeb, :controller

  alias SymmetryControlWeb.{PortalHTML, PortalSession, Protocol}

  def new(conn, params), do: render_login(conn, :ok, nil, params["return_to"])

  def show(conn, _params) do
    json(conn, %{csrf_token: Plug.CSRFProtection.get_csrf_token()})
  end

  def create(conn, %{"operator_token" => token} = params) when is_binary(token) do
    expected = Protocol.configured_token(:operator_token)

    if Protocol.secure_compare(token, expected) do
      conn
      |> configure_session(renew: true)
      |> put_session(:portal_operator, PortalSession.issue(expected))
      |> redirect(to: PortalSession.return_to(params["return_to"]))
    else
      render_login(conn, :unauthorized, "Operator token is invalid", params["return_to"])
    end
  end

  def create(conn, params),
    do:
      render_login(conn, :unprocessable_entity, "Operator token is required", params["return_to"])

  def delete(conn, _params) do
    conn
    |> delete_session(:portal_operator)
    |> configure_session(drop: true)
    |> redirect(to: "/portal/login")
  end

  defp render_login(conn, status, error, return_to) do
    html =
      PortalHTML.login(%{
        csrf_token: Plug.CSRFProtection.get_csrf_token(),
        error: error,
        return_to: PortalSession.return_to(return_to)
      })

    conn
    |> put_resp_content_type("text/html")
    |> send_resp(Plug.Conn.Status.code(status), html)
  end
end
