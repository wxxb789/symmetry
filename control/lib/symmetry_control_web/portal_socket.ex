defmodule SymmetryControlWeb.PortalSocket do
  @moduledoc """
  The operator invalidation socket.

  It is a separate mount from the daemon socket (`/socket`): Phoenix compiles one
  dispatch clause per socket path, so two sockets cannot share a path. The only
  accepted credential is the short-lived audience-scoped token minted by the
  authenticated `GET /portal/api/session`, sent by the browser client as the
  `base64url.bearer.phx.` WebSocket subprotocol (`auth_token: true`), so it never
  appears in a URL or an access log. There is no operator bearer token on this
  path and no unauthenticated join.
  """

  use Phoenix.Socket

  alias SymmetryControlWeb.PortalSocketToken

  channel "portal:invalidations", SymmetryControlWeb.PortalChannel

  @impl true
  def connect(_params, socket, %{auth_token: token}) when is_binary(token) do
    case PortalSocketToken.verify(token) do
      {:ok, audience} -> {:ok, assign(socket, :portal_audience, audience)}
      {:error, _reason} -> :error
    end
  end

  def connect(_params, _socket, _connect_info), do: :error

  @impl true
  def id(socket), do: "portal:" <> socket.assigns.portal_audience
end
