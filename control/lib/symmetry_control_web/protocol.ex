defmodule SymmetryControlWeb.Protocol do
  @moduledoc false

  import Plug.Conn
  import Phoenix.Controller, only: [json: 2]

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Command, Run, Runtime, Task}

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

  def task(%{task: %Task{} = task, run: run}) do
    %{
      task_id: task.id,
      state: task.state,
      run_id: run && run.id,
      generation: task.current_generation,
      work: work(task),
      result: task.result,
      failure: task.failure
    }
  end

  def task(%Task{} = task), do: task(%{task: task, run: nil})

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
end
