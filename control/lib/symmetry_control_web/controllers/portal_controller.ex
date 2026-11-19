defmodule SymmetryControlWeb.PortalController do
  use SymmetryControlWeb, :controller

  alias SymmetryControlWeb.PortalAssets
  alias SymmetryControlWeb.PortalHTML

  def home(conn, _params), do: redirect(conn, to: "/portal")

  def index(conn, _params) do
    index_path = PortalAssets.index_path()

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
