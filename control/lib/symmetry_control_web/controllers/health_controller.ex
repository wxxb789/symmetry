defmodule SymmetryControlWeb.HealthController do
  use SymmetryControlWeb, :controller

  require Logger

  alias SymmetryControl.Health

  def show(conn, _params) do
    case Health.check(Health.default_checks()) do
      {:ok, body} ->
        json(conn, body)

      {:error, body} ->
        body.checks
        |> Enum.filter(fn {_name, status} -> status == "error" end)
        |> Enum.each(fn {name, _status} -> Logger.error("health check failed", check: name) end)

        conn
        |> put_status(:service_unavailable)
        |> json(body)
    end
  end
end
