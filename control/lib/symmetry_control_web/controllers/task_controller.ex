defmodule SymmetryControlWeb.TaskController do
  use SymmetryControlWeb, :controller

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Notifier, Scheduler}
  alias SymmetryControlWeb.Protocol

  @work_fields ["goal", "agent_profile", "workspace", "input"]

  def create(conn, _params) do
    case body_params(conn) do
      %{"work" => work} = body when is_map(work) and map_size(body) == 1 ->
        with {:ok, idempotency_key} <- idempotency_key(conn),
             {:ok, work} <- public_work(work),
             {:ok, task, disposition} <-
               Orchestration.submit_task(work, idempotency_key),
             {:ok, snapshot} <- Orchestration.task_snapshot(task.id) do
          Scheduler.wake()
          status = if disposition == :created, do: :created, else: :ok
          conn |> put_status(status) |> json(Protocol.task(snapshot))
        else
          {:error, reason} -> Protocol.error(conn, reason)
        end

      _ ->
        Protocol.error(conn, :invalid_request)
    end
  end

  def show(conn, _params) do
    task_id = Map.fetch!(conn.path_params, "task_id")

    case Orchestration.task_snapshot(task_id) do
      {:ok, snapshot} -> json(conn, Protocol.task(snapshot))
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def command(conn, _params) do
    task_id = Map.fetch!(conn.path_params, "task_id")

    with {:ok, idempotency_key} <- idempotency_key(conn),
         {:ok, kind, payload, opts} <- command_request(body_params(conn)),
         {:ok, command, disposition} <-
           Orchestration.create_command(task_id, kind, payload, idempotency_key, opts) do
      if disposition == :created do
        Notifier.command_available(command)
        Scheduler.wake()
      end

      status = if disposition == :created, do: :created, else: :ok
      conn |> put_status(status) |> json(Protocol.command(command))
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  defp command_request(%{"kind" => "cancel"} = body) when map_size(body) == 1,
    do: {:ok, "cancel", %{}, []}

  defp command_request(%{"kind" => "cancel", "generation" => generation} = body)
       when map_size(body) == 2 and is_integer(generation) and generation > 0,
       do: {:ok, "cancel", %{}, [expected_generation: generation]}

  defp command_request(%{"kind" => "provide_input", "payload" => payload} = body)
       when map_size(body) == 2 and is_map(payload),
       do: {:ok, "provide_input", Protocol.normalize_map(payload), []}

  defp command_request(%{"kind" => kind, "payload" => payload, "generation" => generation} = body)
       when kind in ["guidance", "pause", "resume"] and map_size(body) == 3 and
              is_map(payload) and is_integer(generation) and generation > 0,
       do: {:ok, kind, Protocol.normalize_map(payload), [expected_generation: generation]}

  defp command_request(
         %{
           "kind" => "provide_input",
           "payload" => payload,
           "generation" => generation,
           "waiting_transition_id" => waiting_id
         } = body
       )
       when map_size(body) == 4 and is_map(payload) and is_integer(generation) and generation > 0 and
              is_binary(waiting_id),
       do:
         {:ok, "provide_input", Protocol.normalize_map(payload),
          [expected_generation: generation, expected_waiting_transition_id: waiting_id]}

  defp command_request(_), do: {:error, :invalid_request}

  defp idempotency_key(conn) do
    case get_req_header(conn, "idempotency-key") do
      [key] when byte_size(key) > 0 -> {:ok, key}
      _ -> {:error, :invalid_request}
    end
  end

  defp public_work(work) do
    work = Protocol.normalize_map(work)

    if Enum.all?(Map.keys(work), &(&1 in @work_fields)),
      do: {:ok, work},
      else: {:error, :invalid_request}
  end

  defp body_params(conn) when is_map(conn.body_params), do: conn.body_params
  defp body_params(_conn), do: %{}
end
