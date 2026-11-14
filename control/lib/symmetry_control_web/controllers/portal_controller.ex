defmodule SymmetryControlWeb.PortalController do
  use SymmetryControlWeb, :controller

  alias SymmetryControlWeb.PortalHTML

  def home(conn, _params), do: redirect(conn, to: "/portal")

  def classic(conn, _params) do
    html = PortalHTML.index(%{csrf_token: Plug.CSRFProtection.get_csrf_token()})

    conn
    |> put_resp_content_type("text/html")
    |> send_resp(200, html)
  end

  def index(conn, _params) do
    index_path = Application.app_dir(:symmetry_control, "priv/static/portal/index.html")

    case File.read(index_path) do
      {:ok, index_html} ->
        html = PortalHTML.shell(index_html, Plug.CSRFProtection.get_csrf_token())

        conn
        |> put_resp_content_type("text/html")
        |> send_resp(200, html)

      {:error, _reason} ->
        conn
        |> put_resp_content_type("text/plain")
        |> send_resp(
          503,
          "Workspace assets are unavailable. Build and deploy the frontend assets."
        )
    end
  end
end
