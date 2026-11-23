defmodule SymmetryControlWeb.Plugs.PortalAuth do
  @moduledoc false

  import Phoenix.Controller
  import Plug.Conn

  alias SymmetryControlWeb.PortalSession
  alias SymmetryControlWeb.Protocol

  def init(options), do: options

  def call(conn, options) do
    if PortalSession.valid?(
         get_session(conn, :portal_operator),
         Protocol.configured_token(:operator_token)
       ) do
      conn
    else
      conn
      |> delete_session(:portal_operator)
      |> unauthenticated(Keyword.get(options, :format, :html))
    end
  end

  defp unauthenticated(conn, :json), do: conn |> Protocol.error(:unauthenticated) |> halt()

  defp unauthenticated(conn, :html) do
    requested_path =
      if conn.query_string == "" do
        conn.request_path
      else
        conn.request_path <> "?" <> conn.query_string
      end

    query = URI.encode_query(%{"return_to" => PortalSession.return_to(requested_path)})

    conn
    |> redirect(to: "/portal/login?" <> query)
    |> halt()
  end
end
