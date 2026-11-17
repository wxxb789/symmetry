defmodule SymmetryControlWeb.PortalChannel do
  @moduledoc """
  The operator invalidation topic.

  The channel is deliberately receive-only for the browser: joining subscribes
  the channel process to `SymmetryControl.Portal.Invalidations` and every
  committed change is pushed as an `invalidation` event. There is no client
  message that mutates anything, so the real-time path can never become a second
  writable control plane.
  """

  use SymmetryControlWeb, :channel

  alias SymmetryControl.Portal.Invalidations
  alias SymmetryControlWeb.PortalSocketToken

  @topic "portal:invalidations"

  @impl true
  def join(@topic, _payload, socket) do
    if socket.assigns[:portal_audience] == PortalSocketToken.audience() do
      Invalidations.subscribe()
      {:ok, %{audience: PortalSocketToken.audience()}, socket}
    else
      {:error, %{reason: "forbidden"}}
    end
  end

  @impl true
  def handle_info({:portal_invalidation, payload}, socket) do
    push(socket, "invalidation", payload)
    {:noreply, socket}
  end
end
