defmodule SymmetryControlWeb.Protocol do
  @moduledoc false

  import Plug.Conn
  import Phoenix.Controller, only: [json: 2]

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Command, Run, Runtime, Task}

  @history_cursor_salt "task-history-v1"
  @history_cursor_max_age_seconds 86_400
  @default_history_limit 100
  @max_history_limit 500
  @timeline_sources ["event", "transition", "command"]

  @error_statuses %{
    invalid_request: {400, "invalid_request", "request payload is invalid"},
    unauthenticated: {401, "unauthenticated", "credential is missing or invalid"},
    forbidden: {403, "forbidden", "credential does not own this resource"},
    not_found: {404, "not_found", "resource was not found"},
    capacity_exhausted: {409, "capacity_exhausted", "runtime capacity is exhausted"},
    idempotency_conflict:
      {409, "idempotency_conflict", "idempotency key was reused with different input"},
    ownership_lost: {409, "ownership_lost", "execution lease is no longer authoritative"},
    state_conflict: {409, "state_conflict", "state has already advanced"},
    assignment_expired: {410, "assignment_expired", "assignment has expired"},
    invalid_transition: {422, "invalid_transition", "state transition is invalid"}
  }

  def error(conn, reason) do
    {status, code, message} = Map.get(@error_statuses, reason, @error_statuses.invalid_request)
    conn |> put_status(status) |> json(%{error: %{code: code, message: message}})
  end

  def machine_token(conn) do
    with [header] <- get_req_header(conn, "authorization"),
         "Bearer " <> token <- header,
         true <- byte_size(token) > 0 do
      {:ok, token}
    else
      _ -> {:error, :unauthenticated}
    end
  end

  def configured_token(kind),
    do: Application.fetch_env!(:symmetry_control, :orchestration) |> Keyword.fetch!(kind)

  def credential_class(token) when is_binary(token) do
    cond do
      secure_compare(token, configured_token(:enrollment_token)) ->
        :enrollment

      secure_compare(token, configured_token(:operator_token)) ->
        :operator

      true ->
        case Orchestration.authenticate_machine(token) do
          {:ok, machine} -> {:machine, machine}
          {:error, :unauthenticated} -> :unknown
        end
    end
  end

  def secure_compare(left, right)
      when is_binary(left) and is_binary(right) and byte_size(left) == byte_size(right),
      do: Plug.Crypto.secure_compare(left, right)

  def secure_compare(_, _), do: false

  def session(runtime_specs) do
    config = Application.fetch_env!(:symmetry_control, :orchestration)

    %{
      runtimes: Enum.map(runtime_specs, &runtime_registration/1),
      heartbeat_interval_ms: Keyword.fetch!(config, :heartbeat_interval_ms),
      poll_interval_ms: Keyword.fetch!(config, :poll_interval_ms),
      lease_duration_ms: Keyword.fetch!(config, :lease_duration_ms),
      websocket_path: "/socket/websocket?vsn=2.0.0"
    }
  end

  def snapshot(snapshot) do
    %{
      assignments: Enum.map(snapshot.assignments, &assignment/1),
      commands: Enum.map(snapshot.commands, &command/1),
      server_time: iso8601(snapshot.server_time)
    }
  end

  def reconcile(snapshot) do
    snapshot
    |> snapshot()
    |> Map.put(:decisions, Enum.map(snapshot.decisions, &decision/1))
  end

  def claimed_run(%Run{} = run, %Task{} = task) do
    %{
      run_id: run.id,
      task_id: task.id,
      generation: run.generation,
      claim_id: run.claim_id,
      lease_token: run.lease_token,
      lease_expires_at: iso8601(run.lease_expires_at),
      work: work(task)
    }
  end

  def run(%Run{} = run) do
    %{
      run_id: run.id,
      task_id: run.task_id,
      runtime_id: run.runtime_id,
      generation: run.generation,
      state: run.state,
      claim_id: run.claim_id,
      lease_token: run.lease_token,
      lease_expires_at: iso8601(run.lease_expires_at),
      result: run.result,
      failure: run.failure
    }
  end

  def command(%Command{} = command) do
    %{
      command_id: command.id,
      task_id: command.task_id,
      run_id: command.run_id,
      generation: command.generation,
      kind: command.kind,
      payload: command.payload,
      state: command.state,
      issued_at: iso8601(command.inserted_at),
      applied_at: iso8601(command.applied_at),
      acknowledgement_id: command.acknowledgement_id,
      acknowledgement_outcome: command.acknowledgement_outcome,
      acknowledged_at: iso8601(command.acknowledged_at)
    }
  end

  def command(command) when is_map(command) do
    %{
      command_id: Map.fetch!(command, :id),
      task_id: Map.fetch!(command, :task_id),
      run_id: Map.fetch!(command, :run_id),
      generation: Map.fetch!(command, :generation),
      kind: Map.fetch!(command, :kind),
      payload: Map.fetch!(command, :payload),
      state: Map.fetch!(command, :state),
      issued_at: iso8601(Map.fetch!(command, :inserted_at)),
      applied_at: iso8601(Map.fetch!(command, :applied_at)),
      acknowledgement_id: Map.fetch!(command, :acknowledgement_id),
      acknowledgement_outcome: Map.fetch!(command, :acknowledgement_outcome),
      acknowledged_at: iso8601(Map.fetch!(command, :acknowledged_at))
    }
  end

  def task(%{task: %Task{} = task, run: run} = snapshot) do
    %{
      task_id: task.id,
      state: task.state,
      run_id: run && run.id,
      generation: task.current_generation,
      work: work(task),
      result: task.result,
      failure: task.failure,
      waiting: waiting(Map.get(snapshot, :waiting)),
      latest_command: latest_command(Map.get(snapshot, :latest_command))
    }
  end

  def task(%Task{} = task), do: task(%{task: task, run: nil})

  def runtime(snapshot) when is_map(snapshot) do
    %{
      machine_id: Map.fetch!(snapshot, :machine_id),
      machine_name: Map.fetch!(snapshot, :machine_name),
      runtime_id: Map.fetch!(snapshot, :runtime_id),
      runtime_key: Map.fetch!(snapshot, :runtime_key),
      runtime_name: Map.fetch!(snapshot, :runtime_name),
      status: Map.fetch!(snapshot, :status),
      last_heartbeat_at: iso8601(Map.fetch!(snapshot, :last_heartbeat_at)),
      connection_epoch: Map.fetch!(snapshot, :connection_epoch),
      capacity: Map.fetch!(snapshot, :capacity),
      reserved_capacity: Map.fetch!(snapshot, :reserved_capacity),
      active_runs: Enum.map(Map.fetch!(snapshot, :active_runs), &active_run/1)
    }
  end

  def history_options(params, task_id, surface, direction)
      when is_map(params) and is_binary(task_id) and is_binary(surface) and
             direction in ["after", "before"] do
    cursor_key = direction
    opposite_key = if direction == "after", do: "before", else: "after"

    with false <- Map.has_key?(params, opposite_key),
         {:ok, limit} <- history_limit(Map.get(params, "limit")),
         {:ok, cursor} <-
           decode_history_cursor(Map.get(params, cursor_key), task_id, surface, direction) do
      {:ok, history_options(direction, limit, cursor)}
    else
      _ -> {:error, :invalid_request}
    end
  end

  def history_options(_, _, _, _), do: {:error, :invalid_request}

  def history_page(task_id, "events", %{entries: entries, next_after: next_after}),
    do: %{
      events: Enum.map(entries, &event/1),
      next_after: encode_history_cursor(task_id, "events", "after", next_after)
    }

  def history_page(task_id, "transitions", %{entries: entries, next_after: next_after}),
    do: %{
      transitions: Enum.map(entries, &transition/1),
      next_after: encode_history_cursor(task_id, "transitions", "after", next_after)
    }

  def history_page(task_id, "commands", %{entries: entries, next_after: next_after}),
    do: %{
      commands: Enum.map(entries, &command/1),
      next_after: encode_history_cursor(task_id, "commands", "after", next_after)
    }

  def timeline_page(task_id, %{entries: entries, next_before: next_before}) do
    %{
      items: Enum.map(entries, &timeline_item(task_id, &1)),
      next_before: encode_history_cursor(task_id, "timeline", "before", next_before)
    }
  end

  def normalize_map(value) when is_map(value) do
    Map.new(value, fn {key, item} -> {to_string(key), normalize_map(item)} end)
  end

  def normalize_map(value) when is_list(value), do: Enum.map(value, &normalize_map/1)
  def normalize_map(value), do: value

  def parse_event_times(events) when is_list(events) do
    Enum.reduce_while(events, {:ok, []}, fn
      event, {:ok, parsed} when is_map(event) ->
        case Map.fetch(event, "occurred_at") do
          {:ok, value} when is_binary(value) ->
            case DateTime.from_iso8601(value) do
              {:ok, datetime, _offset} ->
                {:cont, {:ok, [Map.put(event, "occurred_at", datetime) | parsed]}}

              _ ->
                {:halt, {:error, :invalid_request}}
            end

          _ ->
            {:halt, {:error, :invalid_request}}
        end

      _, _ ->
        {:halt, {:error, :invalid_request}}
    end)
    |> case do
      {:ok, parsed} -> {:ok, Enum.reverse(parsed)}
      error -> error
    end
  end

  def parse_event_times(_), do: {:error, :invalid_request}

  defp runtime_registration(%Runtime{} = runtime),
    do: %{
      runtime_key: runtime.runtime_key,
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch
    }

  defp assignment(%{run: %Run{} = run, task: %Task{} = task}) do
    %{
      run_id: run.id,
      task_id: task.id,
      generation: run.generation,
      assignment_expires_at: iso8601(run.assignment_expires_at),
      work: work(task)
    }
  end

  defp work(%Task{} = task),
    do: %{
      goal: task.goal,
      agent_profile: task.agent_profile,
      workspace: task.workspace,
      input: task.input
    }

  defp decision(decision) do
    %{
      run_id: decision.run_id,
      generation: decision.generation,
      decision: decision.decision,
      lease_expires_at: iso8601(decision.lease_expires_at)
    }
  end

  defp iso8601(nil), do: nil
  defp iso8601(%DateTime{} = value), do: DateTime.to_iso8601(value)

  defp waiting(nil), do: nil

  defp waiting(waiting) do
    %{
      run_id: Map.fetch!(waiting, :run_id),
      generation: Map.fetch!(waiting, :generation),
      transition_id: Map.fetch!(waiting, :transition_id),
      question: Map.fetch!(waiting, :question),
      payload: Map.fetch!(waiting, :payload),
      recorded_at: iso8601(Map.fetch!(waiting, :inserted_at))
    }
  end

  defp latest_command(nil), do: nil
  defp latest_command(%Command{} = command), do: command(command)
  defp latest_command(command) when is_map(command), do: command(command)

  defp active_run(run) do
    %{
      run_id: Map.fetch!(run, :run_id),
      task_id: Map.fetch!(run, :task_id),
      generation: Map.fetch!(run, :generation),
      state: Map.fetch!(run, :state),
      recorded_at: iso8601(Map.fetch!(run, :inserted_at))
    }
  end

  defp history_options("after", limit, nil), do: [limit: limit]
  defp history_options("after", limit, cursor), do: [limit: limit, after: cursor]
  defp history_options("before", limit, nil), do: [limit: limit]
  defp history_options("before", limit, cursor), do: [limit: limit, before: cursor]

  defp event(entry) do
    %{
      run_id: Map.fetch!(entry, :run_id),
      generation: Map.fetch!(entry, :generation),
      event_id: Map.fetch!(entry, :event_id),
      sequence: Map.fetch!(entry, :sequence),
      kind: Map.fetch!(entry, :kind),
      payload: Map.fetch!(entry, :payload),
      occurred_at: iso8601(Map.fetch!(entry, :occurred_at)),
      recorded_at: iso8601(Map.fetch!(entry, :inserted_at))
    }
  end

  defp transition(entry) do
    %{
      run_id: Map.fetch!(entry, :run_id),
      generation: Map.fetch!(entry, :generation),
      transition_id: Map.fetch!(entry, :transition_id),
      state: Map.fetch!(entry, :state),
      payload: Map.fetch!(entry, :payload),
      recorded_at: iso8601(Map.fetch!(entry, :inserted_at))
    }
  end

  defp timeline_item(task_id, entry) do
    %{
      source: Map.fetch!(entry, :source),
      run_id: Map.fetch!(entry, :run_id),
      generation: Map.fetch!(entry, :generation),
      recorded_at: iso8601(Map.fetch!(entry, :inserted_at)),
      data: timeline_data(task_id, entry)
    }
  end

  defp timeline_data(_task_id, %{source: "event"} = entry),
    do: Map.drop(event(entry), [:run_id, :generation, :recorded_at])

  defp timeline_data(_task_id, %{source: "transition"} = entry),
    do: Map.drop(transition(entry), [:run_id, :generation, :recorded_at])

  defp timeline_data(task_id, %{source: "command"} = entry),
    do: entry |> Map.put(:task_id, task_id) |> command() |> Map.drop([:run_id, :generation])

  defp history_limit(nil), do: {:ok, @default_history_limit}

  defp history_limit(value) when is_binary(value) do
    case Integer.parse(value) do
      {limit, ""} when limit > 0 and limit <= @max_history_limit -> {:ok, limit}
      _ -> {:error, :invalid_request}
    end
  end

  defp history_limit(_), do: {:error, :invalid_request}

  defp encode_history_cursor(_task_id, _surface, _direction, nil), do: nil

  defp encode_history_cursor(task_id, surface, direction, position) do
    payload = %{
      "v" => 1,
      "task_id" => task_id,
      "surface" => surface,
      "direction" => direction,
      "inserted_at" => iso8601(Map.fetch!(position, :inserted_at)),
      "id" => Map.fetch!(position, :id)
    }

    payload =
      case Map.fetch(position, :source) do
        {:ok, source} -> Map.put(payload, "source", source)
        :error -> payload
      end

    Phoenix.Token.sign(SymmetryControlWeb.Endpoint, @history_cursor_salt, payload)
  end

  defp decode_history_cursor(nil, _task_id, _surface, _direction), do: {:ok, nil}

  defp decode_history_cursor(token, task_id, surface, direction) when is_binary(token) do
    with {:ok, payload} <-
           Phoenix.Token.verify(SymmetryControlWeb.Endpoint, @history_cursor_salt, token,
             max_age: @history_cursor_max_age_seconds
           ),
         {:ok, position} <- cursor_position(payload, task_id, surface, direction) do
      {:ok, position}
    else
      _ -> {:error, :invalid_request}
    end
  end

  defp decode_history_cursor(_, _, _, _), do: {:error, :invalid_request}

  defp cursor_position(payload, task_id, surface, direction) when is_map(payload) do
    expected_keys =
      ["v", "task_id", "surface", "direction", "inserted_at", "id"] ++
        if(surface == "timeline", do: ["source"], else: [])

    with true <- MapSet.new(Map.keys(payload)) == MapSet.new(expected_keys),
         %{
           "v" => 1,
           "task_id" => ^task_id,
           "surface" => ^surface,
           "direction" => ^direction,
           "inserted_at" => inserted_at,
           "id" => id
         } <- payload,
         true <- is_binary(inserted_at) and is_binary(id),
         {:ok, datetime, _offset} <- DateTime.from_iso8601(inserted_at),
         {:ok, _} <- Ecto.UUID.cast(id) do
      cursor_source(payload, surface, datetime, id)
    else
      _ -> {:error, :invalid_request}
    end
  end

  defp cursor_position(_, _, _, _), do: {:error, :invalid_request}

  defp cursor_source(_payload, surface, inserted_at, id) when surface != "timeline",
    do: {:ok, %{inserted_at: inserted_at, id: id}}

  defp cursor_source(%{"source" => source}, "timeline", inserted_at, id)
       when source in @timeline_sources,
       do: {:ok, %{inserted_at: inserted_at, source: source, id: id}}

  defp cursor_source(_, _, _, _), do: {:error, :invalid_request}
end
