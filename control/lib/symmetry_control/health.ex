defmodule SymmetryControl.Health do
  @moduledoc false

  alias SymmetryControl.Repo

  @spec check([{atom(), (-> :ok | {:error, term()})}]) :: {:ok, map()} | {:error, map()}
  def check(checks) do
    statuses =
      Map.new(checks, fn {name, check} ->
        {name, if(run_check(check) == :ok, do: "ok", else: "error")}
      end)

    body = %{
      status:
        if(Enum.all?(statuses, fn {_name, status} -> status == "ok" end),
          do: "ok",
          else: "unavailable"
        ),
      checks: statuses
    }

    if body.status == "ok", do: {:ok, body}, else: {:error, body}
  end

  @spec default_checks() :: [{atom(), (-> :ok | {:error, term()})}]
  def default_checks do
    [
      database: &database/0,
      scheduler: fn -> registered?(SymmetryControl.Orchestration.Scheduler) end,
      reaper: fn -> registered?(SymmetryControl.Orchestration.Reconciler) end,
      provider_access: fn -> registered?(SymmetryControl.Integrations.ProviderAccess) end
    ]
  end

  defp database do
    case Ecto.Adapters.SQL.query(Repo, "SELECT 1", [], timeout: 2_000) do
      {:ok, _result} -> :ok
      {:error, reason} -> {:error, reason}
    end
  rescue
    error in DBConnection.ConnectionError -> {:error, error}
  end

  defp run_check(check) do
    try do
      check.()
    rescue
      _error -> :error
    catch
      _kind, _reason -> :error
    end
  end

  defp registered?(name), do: if(is_pid(Process.whereis(name)), do: :ok, else: {:error, :down})
end
