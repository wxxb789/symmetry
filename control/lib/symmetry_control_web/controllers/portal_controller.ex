defmodule SymmetryControlWeb.PortalController do
  use SymmetryControlWeb, :controller

  alias SymmetryControlWeb.PortalHTML

  def home(conn, _params), do: redirect(conn, to: "/portal")

  def index(conn, _params) do
    html = PortalHTML.index(%{csrf_token: Plug.CSRFProtection.get_csrf_token()})

    conn
    |> put_resp_content_type("text/html")
    |> send_resp(200, html)
  end
end
