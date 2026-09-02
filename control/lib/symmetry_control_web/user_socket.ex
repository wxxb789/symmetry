defmodule SymmetryControlWeb.UserSocket do
  use Phoenix.Socket

  alias SymmetryControl.Orchestration
  channel "daemon:*", SymmetryControlWeb.DaemonChannel

  @impl true
  def connect(_params, socket, connect_info) do
    with {:ok, token} <- bearer(connect_info),
         {:ok, machine} <- Orchestration.authenticate_machine(token) do
      {:ok, assign(socket, :machine, machine)}
    else
      _ -> :error
    end
  end

  @impl true
  def id(socket), do: "machine:" <> socket.assigns.machine.id

  defp bearer(connect_info) do
    headers = Map.get(connect_info, :x_headers, Map.get(connect_info, "x_headers", []))

    case Enum.find_value(headers, fn
           {name, value} when is_binary(name) and is_binary(value) ->
             if String.downcase(name) == "x-symmetry-token", do: value

           _ ->
             nil
         end) do
      token when is_binary(token) and byte_size(token) > 0 -> {:ok, token}
      _ -> {:error, :unauthenticated}
    end
  end
end
