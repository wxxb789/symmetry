defmodule SymmetryControlWeb.DaemonController do
  use SymmetryControlWeb, :controller

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Scheduler
  alias SymmetryControlWeb.Protocol

  def enroll(conn, %{"machine" => machine}) when is_map(machine) do
    case Orchestration.enroll_machine(Protocol.normalize_map(machine),
           enrollment_token: conn.assigns.enrollment_token,
           expected_enrollment_token: Protocol.configured_token(:enrollment_token)
         ) do
      {:ok, %{machine: enrolled, token: token}} ->
        conn |> put_status(:created) |> json(%{machine_id: enrolled.id, machine_token: token})

      {:error, reason} ->
        Protocol.error(conn, reason)
    end
  end

  def enroll(conn, _params), do: Protocol.error(conn, :invalid_request)

  def register_session(conn, %{"daemon_instance_id" => daemon_instance_id, "runtimes" => runtimes})
      when is_list(runtimes) do
    if Enum.all?(runtimes, &is_map/1) do
      configured_heartbeat = config(:heartbeat_interval_ms)

      specifications =
        Enum.map(runtimes, fn runtime ->
          runtime
          |> Protocol.normalize_map()
          |> Map.put_new("heartbeat_interval_ms", configured_heartbeat)
        end)

      case Orchestration.register_runtimes(
             conn.assigns.machine.id,
             daemon_instance_id,
             specifications
           ) do
        {:ok, registered} ->
          Scheduler.wake()
          conn |> put_status(:created) |> json(Protocol.session(registered))

        {:error, reason} ->
          Protocol.error(conn, reason)
      end
    else
      Protocol.error(conn, :invalid_request)
    end
  end

  def register_session(conn, _params), do: Protocol.error(conn, :invalid_request)

  def heartbeat(conn, %{"runtime_id" => runtime_id, "runtime_epoch" => runtime_epoch} = params) do
    with :ok <- owns_runtime(conn, runtime_id),
         {:ok, snapshot} <-
           Orchestration.heartbeat(runtime_id, runtime_epoch, Map.get(params, "active_runs", [])) do
      Scheduler.wake()
      json(conn, Protocol.snapshot(snapshot))
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def heartbeat(conn, _params), do: Protocol.error(conn, :invalid_request)

  def work(conn, %{"runtime_id" => runtime_id, "runtime_epoch" => runtime_epoch}) do
    with :ok <- owns_runtime(conn, runtime_id),
         {:ok, epoch} <- integer(runtime_epoch),
         {:ok, snapshot} <- Orchestration.work_snapshot(runtime_id, epoch) do
      json(conn, Protocol.snapshot(snapshot))
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def work(conn, _params), do: Protocol.error(conn, :invalid_request)

  def claim(conn, %{"run_id" => run_id} = params) do
    with :ok <- owns_run(conn, run_id),
         {:ok, run} <-
           Orchestration.claim(run_id, Protocol.normalize_map(params),
             lease_duration_ms: config(:lease_duration_ms)
           ),
         {:ok, %{task: task}} <- Orchestration.task_snapshot(run.task_id) do
      json(conn, Protocol.claimed_run(run, task))
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def heartbeat_run(conn, %{"run_id" => run_id} = params) do
    fence = Protocol.normalize_map(params)

    with :ok <- owns_run(conn, run_id),
         {:ok, run} <-
           Orchestration.renew_lease(run_id, fence, lease_duration_ms: config(:lease_duration_ms)),
         {:ok, snapshot} <-
           Orchestration.work_snapshot(run.runtime_id, Map.fetch!(fence, "runtime_epoch")) do
      json(conn, %{
        lease_expires_at: DateTime.to_iso8601(run.lease_expires_at),
        commands: Enum.map(snapshot.commands, &Protocol.command/1)
      })
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def append_events(conn, %{"run_id" => run_id, "events" => events} = params) do
    with :ok <- owns_run(conn, run_id),
         {:ok, events} <- Protocol.parse_event_times(Protocol.normalize_map(events)),
         {:ok, stored} <-
           Orchestration.append_events(run_id, Protocol.normalize_map(params), events) do
      conn |> put_status(:created) |> json(%{events: Enum.map(stored, &event/1)})
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def append_events(conn, _params), do: Protocol.error(conn, :invalid_request)

  def transition(
        conn,
        %{"run_id" => run_id, "state" => target, "transition_id" => transition_id} = params
      ) do
    payload = params |> Map.get("payload", %{}) |> Protocol.normalize_map()
    fence = Protocol.normalize_map(params)

    with :ok <- owns_run(conn, run_id),
         {:ok, run} <- Orchestration.transition(run_id, fence, target, payload, transition_id) do
      if target in ["completed", "failed", "cancelled"], do: Scheduler.wake()
      json(conn, Protocol.run(run))
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def transition(conn, _params), do: Protocol.error(conn, :invalid_request)

  def reconcile(conn, %{"runtime_id" => runtime_id, "runtime_epoch" => runtime_epoch} = params) do
    with :ok <- owns_runtime(conn, runtime_id),
         {:ok, snapshot} <-
           Orchestration.reconcile(runtime_id, runtime_epoch, Map.get(params, "runs", [])) do
      json(conn, Protocol.reconcile(snapshot))
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def reconcile(conn, _params), do: Protocol.error(conn, :invalid_request)

  def acknowledge_command(
        conn,
        %{"command_id" => command_id, "outcome" => outcome, "ack_id" => acknowledgement_id} =
          params
      ) do
    if Map.has_key?(params, "acknowledgement_id") do
      Protocol.error(conn, :invalid_request)
    else
      with :ok <- owns_command(conn, command_id),
           {:ok, command} <-
             Orchestration.acknowledge_command(
               command_id,
               Protocol.normalize_map(params),
               outcome,
               acknowledgement_id
             ) do
        json(conn, Protocol.command(command))
      else
        {:error, reason} -> Protocol.error(conn, reason)
      end
    end
  end

  def acknowledge_command(conn, _params), do: Protocol.error(conn, :invalid_request)

  defp event(event),
    do: %{
      event_id: event.event_id,
      sequence: event.sequence,
      kind: event.kind,
      payload: event.payload,
      occurred_at: DateTime.to_iso8601(event.occurred_at)
    }

  defp owns_runtime(conn, runtime_id) do
    with {:ok, runtime} <- Orchestration.fetch_runtime(runtime_id) do
      if runtime.machine_id == conn.assigns.machine.id, do: :ok, else: {:error, :forbidden}
    end
  end

  defp owns_run(conn, run_id) do
    with {:ok, _run} <- Orchestration.fetch_run(run_id) do
      if Orchestration.machine_owns_run?(conn.assigns.machine.id, run_id),
        do: :ok,
        else: {:error, :forbidden}
    end
  end

  defp owns_command(conn, command_id) do
    with {:ok, _command} <- Orchestration.fetch_command(command_id) do
      if Orchestration.machine_owns_command?(conn.assigns.machine.id, command_id),
        do: :ok,
        else: {:error, :forbidden}
    end
  end

  defp integer(value) when is_integer(value) and value > 0, do: {:ok, value}

  defp integer(value) when is_binary(value) do
    case Integer.parse(value) do
      {parsed, ""} when parsed > 0 -> {:ok, parsed}
      _ -> {:error, :invalid_request}
    end
  end

  defp integer(_), do: {:error, :invalid_request}

  defp config(key),
    do: Application.fetch_env!(:symmetry_control, :orchestration) |> Keyword.fetch!(key)
end
