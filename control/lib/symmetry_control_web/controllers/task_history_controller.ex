defmodule SymmetryControlWeb.TaskHistoryController do
  use SymmetryControlWeb, :controller

  alias SymmetryControl.Orchestration
  alias SymmetryControlWeb.Protocol

  def events(conn, _params), do: collection(conn, "events", &Orchestration.list_task_events/2)

  def transitions(conn, _params),
    do: collection(conn, "transitions", &Orchestration.list_task_transitions/2)

  def commands(conn, _params),
    do: collection(conn, "commands", &Orchestration.list_task_commands/2)

  def timeline(conn, _params) do
    task_id = Map.fetch!(conn.path_params, "task_id")

    with {:ok, opts} <-
           Protocol.history_options(
             fetch_query_params(conn).query_params,
             task_id,
             "timeline",
             "before"
           ),
         {:ok, page} <- Orchestration.task_timeline(task_id, opts) do
      json(conn, Protocol.timeline_page(task_id, page))
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  defp collection(conn, surface, query) do
    task_id = Map.fetch!(conn.path_params, "task_id")

    with {:ok, opts} <-
           Protocol.history_options(
             fetch_query_params(conn).query_params,
             task_id,
             surface,
             "after"
           ),
         {:ok, page} <- query.(task_id, opts) do
      json(conn, Protocol.history_page(task_id, surface, page))
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end
end
