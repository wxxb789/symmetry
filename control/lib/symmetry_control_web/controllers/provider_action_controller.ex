defmodule SymmetryControlWeb.ProviderActionController do
  use SymmetryControlWeb, :controller

  alias SymmetryControl.Integrations.ProviderAccess
  alias SymmetryControlWeb.Protocol

  def create(conn, _params) do
    with {:ok, token} <- Protocol.machine_token(conn),
         {:ok, action_id, resource_id, operation, input} <- request(conn.body_params),
         {:ok, result} <- ProviderAccess.execute(token, action_id, resource_id, operation, input) do
      json(conn, result)
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  defp request(
         %{"action_id" => action_id, "resource_id" => resource_id, "operation" => operation} =
           body
       )
       when is_binary(action_id) and is_binary(resource_id) and is_binary(operation) do
    case Map.get(body, "input", %{}) do
      input when is_map(input) -> {:ok, action_id, resource_id, operation, input}
      _input -> {:error, :invalid_request}
    end
  end

  defp request(_body), do: {:error, :invalid_request}
end
