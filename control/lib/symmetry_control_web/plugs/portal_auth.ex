defmodule SymmetryControlWeb.Plugs.PortalAuth do
  @moduledoc false

  import Plug.Conn
  import Phoenix.Controller

  alias SymmetryControlWeb.Protocol
  alias SymmetryControlWeb.PortalSession

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
    conn
    |> redirect(to: "/portal/login")
    |> halt()
  end
end
