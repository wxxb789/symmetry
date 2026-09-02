defmodule SymmetryControlWeb.TaskController do
  use SymmetryControlWeb, :controller

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Scheduler
  alias SymmetryControlWeb.Protocol

  def create(conn, %{"work" => work} = params) when is_map(work) do
    if map_size(params) == 1 do
      with {:ok, idempotency_key} <- idempotency_key(conn),
           {:ok, task, disposition} <-
             Orchestration.submit_task(Protocol.normalize_map(work), idempotency_key),
           {:ok, snapshot} <- Orchestration.task_snapshot(task.id) do
        Scheduler.wake()
        status = if disposition == :created, do: :created, else: :ok
        conn |> put_status(status) |> json(Protocol.task(snapshot))
      else
        {:error, reason} -> Protocol.error(conn, reason)
      end
    else
      Protocol.error(conn, :invalid_request)
    end
  end

  def create(conn, _params), do: Protocol.error(conn, :invalid_request)

  def show(conn, %{"task_id" => task_id}) do
    case Orchestration.task_snapshot(task_id) do
      {:ok, snapshot} -> json(conn, Protocol.task(snapshot))
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def cancel(conn, %{"task_id" => task_id}) do
    case Orchestration.request_cancel(task_id) do
      {:ok, task, command} ->
        if command, do: broadcast_command(command)
        Scheduler.wake()

        case Orchestration.task_snapshot(task.id) do
          {:ok, snapshot} ->
            json(
              conn,
              Protocol.task(snapshot) |> Map.put(:command, command && Protocol.command(command))
            )

          {:error, reason} ->
            Protocol.error(conn, reason)
        end

      {:error, reason} ->
        Protocol.error(conn, reason)
    end
  end

  def input(conn, %{"task_id" => task_id} = params) do
    with {:ok, idempotency_key} <- idempotency_key(conn),
         {:ok, command, _disposition} <-
           Orchestration.provide_input(
             task_id,
             Protocol.normalize_map(Map.get(params, "input", %{})),
             idempotency_key
           ) do
      broadcast_command(command)

      case Orchestration.task_snapshot(task_id) do
        {:ok, snapshot} ->
          conn
          |> put_status(:created)
          |> json(Protocol.task(snapshot) |> Map.put(:command, Protocol.command(command)))

        {:error, reason} ->
          Protocol.error(conn, reason)
      end
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  defp idempotency_key(conn) do
    case get_req_header(conn, "idempotency-key") do
      [key] when byte_size(key) > 0 -> {:ok, key}
      _ -> {:error, :invalid_request}
    end
  end

  defp broadcast_command(command) do
    with {:ok, %{machine_id: machine_id, runtime_id: runtime_id}} <-
           Orchestration.assignment_target(command.run_id) do
      Phoenix.PubSub.broadcast(
        SymmetryControl.PubSub,
        "daemon:" <> machine_id,
        {:command_available, %{runtime_id: runtime_id, command_id: command.id}}
      )
    end
  end
end
