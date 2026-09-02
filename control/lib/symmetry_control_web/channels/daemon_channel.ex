defmodule SymmetryControlWeb.DaemonChannel do
  use SymmetryControlWeb, :channel

  @impl true
  def join("daemon:" <> machine_id, _payload, socket) do
    if socket.assigns.machine.id == machine_id do
      {:ok, socket}
    else
      {:error, %{reason: "forbidden"}}
    end
  end

  @impl true
  def handle_info({:work_available, payload}, socket),
    do: push_hint("work_available", payload, socket)

  def handle_info({:command_available, payload}, socket),
    do: push_hint("command_available", payload, socket)

  def handle_info({:reconcile_required, payload}, socket),
    do: push_hint("reconcile_required", payload, socket)

  defp push_hint(type, payload, socket) do
    push(socket, type, Map.put(payload, :type, type))
    {:noreply, socket}
  end
end
