defmodule SymmetryControlWeb.RuntimeController do
  use SymmetryControlWeb, :controller

  alias SymmetryControl.Orchestration
  alias SymmetryControlWeb.Protocol

  def index(conn, _params) do
    {:ok, snapshots} = Orchestration.runtime_snapshots()
    json(conn, %{runtimes: Enum.map(snapshots, &Protocol.runtime/1)})
  end

  def show(conn, _params) do
    runtime_id = Map.fetch!(conn.path_params, "runtime_id")

    case Orchestration.runtime_snapshot(runtime_id) do
      {:ok, snapshot} -> json(conn, Protocol.runtime(snapshot))
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end
end
