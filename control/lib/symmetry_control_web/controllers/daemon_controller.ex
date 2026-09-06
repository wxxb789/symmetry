defmodule SymmetryControlWeb.DaemonController do
  use SymmetryControlWeb, :controller

  alias SymmetryControl.Integrations.ProviderAccess
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Scheduler
  alias SymmetryControl.Repo
  alias SymmetryControlWeb.Protocol

  def enroll(conn, _params) do
    with {:ok, idempotency_key} <- idempotency_key(conn),
         %{"machine" => machine, "machine_token" => machine_token}
         when is_map(machine) and is_binary(machine_token) <- body_params(conn) do
      attrs = machine |> Protocol.normalize_map() |> Map.put("machine_token", machine_token)

      case Orchestration.enroll_machine(attrs, idempotency_key,
             enrollment_token: conn.assigns.enrollment_token,
             expected_enrollment_token: Protocol.configured_token(:enrollment_token)
           ) do
        {:ok, %{machine: enrolled, token: token}, disposition} ->
          conn
          |> put_status(if(disposition == :created, do: :created, else: :ok))
          |> json(%{machine_id: enrolled.id, machine_token: token})

        {:error, reason} ->
          Protocol.error(conn, reason)
      end
    else
      _ ->
        Protocol.error(conn, :invalid_request)
    end
  end

  defp idempotency_key(conn) do
    case get_req_header(conn, "idempotency-key") do
      [key] when byte_size(key) > 0 -> {:ok, key}
      _ -> {:error, :invalid_request}
    end
  end

  def register_session(conn, _params) do
    machine_id = path_param(conn, "machine_id")
    daemon_instance_id = path_param(conn, "daemon_instance_id")
    body = body_params(conn)

    with %{"runtimes" => runtimes} when is_list(runtimes) <- body,
         true <- Enum.all?(runtimes, &is_map/1) do
      configured_heartbeat = config(:heartbeat_interval_ms)

      specifications =
        body
        |> mutation_params(["machine_id", "daemon_instance_id"])
        |> Map.fetch!("runtimes")
        |> Enum.map(fn runtime ->
          runtime
          |> Protocol.normalize_map()
          |> Map.put_new("heartbeat_interval_ms", configured_heartbeat)
        end)

      with :ok <- owns_machine(conn, machine_id),
           {:ok, registered} <-
             Orchestration.register_runtimes(machine_id, daemon_instance_id, specifications) do
        Scheduler.wake()
        json(conn, Protocol.session(registered))
      else
        {:error, reason} -> Protocol.error(conn, reason)
      end
    else
      _ -> Protocol.error(conn, :invalid_request)
    end
  end

  def heartbeat(conn, _params) do
    runtime_id = path_param(conn, "runtime_id")
    body = body_params(conn)
    request = mutation_params(body, ["runtime_id"])

    with {:ok, runtime_epoch} <- body_value(body, "runtime_epoch"),
         :ok <- owns_runtime(conn, runtime_id),
         {:ok, snapshot} <-
           Orchestration.heartbeat(runtime_id, runtime_epoch, Map.get(request, "active_runs", [])) do
      Scheduler.wake()
      json(conn, Protocol.snapshot(snapshot))
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def work(conn, _params) do
    runtime_id = path_param(conn, "runtime_id")

    with {:ok, runtime_epoch} <- query_value(conn, "runtime_epoch"),
         :ok <- owns_runtime(conn, runtime_id),
         {:ok, epoch} <- integer(runtime_epoch),
         {:ok, snapshot} <- Orchestration.work_snapshot(runtime_id, epoch) do
      json(conn, Protocol.snapshot(snapshot))
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def claim(conn, _params) do
    run_id = path_param(conn, "run_id")
    claim_id = path_param(conn, "claim_id")

    request =
      conn
      |> mutation_params(["run_id", "claim_id"])
      |> Map.put("claim_id", claim_id)

    with :ok <- owns_run(conn, run_id),
         {:ok, {run, task, provider_access}} <- claim_with_provider_access(run_id, request) do
      json(conn, Protocol.claimed_run(run, task, provider_access))
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  defp claim_with_provider_access(run_id, request) do
    Repo.transaction(fn ->
      with {:ok, provider_scope} <- ProviderAccess.lock_claim_scope(run_id),
           {:ok, run} <-
             Orchestration.claim(run_id, request, lease_duration_ms: config(:lease_duration_ms)),
           {:ok, %{task: task}} <- Orchestration.task_snapshot(run.task_id),
           {:ok, provider_access} <- ProviderAccess.issue(provider_scope, run, task) do
        {run, task, provider_access}
      else
        {:error, reason} -> Repo.rollback(reason)
      end
    end)
  end

  def heartbeat_run(conn, _params) do
    run_id = path_param(conn, "run_id")
    fence = conn |> mutation_params(["run_id"]) |> Protocol.normalize_map()

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

  def append_events(conn, _params) do
    run_id = path_param(conn, "run_id")
    body = body_params(conn)
    request = mutation_params(body, ["run_id"]) |> Protocol.normalize_map()
    fence = Map.delete(request, "events")

    with {:ok, events} <- body_value(body, "events"),
         :ok <- owns_run(conn, run_id),
         {:ok, events} <- Protocol.parse_event_times(Protocol.normalize_map(events)),
         {:ok, _stored} <- Orchestration.append_events(run_id, fence, events) do
      send_resp(conn, :no_content, "")
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def transition(conn, _params) do
    run_id = path_param(conn, "run_id")
    transition_id = path_param(conn, "transition_id")
    body = body_params(conn)

    request =
      mutation_params(body, ["run_id", "transition_id"]) |> Protocol.normalize_map()

    payload = Map.get(request, "payload", %{})

    with {:ok, target} <- body_value(body, "state"),
         :ok <- owns_run(conn, run_id),
         {:ok, run} <- Orchestration.transition(run_id, request, target, payload, transition_id) do
      if target in ["completed", "failed", "cancelled"], do: Scheduler.wake()
      json(conn, Protocol.run(run))
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def reconcile(conn, _params) do
    runtime_id = path_param(conn, "runtime_id")
    body = body_params(conn)
    request = mutation_params(body, ["runtime_id"])

    with {:ok, runtime_epoch} <- body_value(body, "runtime_epoch"),
         :ok <- owns_runtime(conn, runtime_id),
         {:ok, snapshot} <-
           Orchestration.reconcile(runtime_id, runtime_epoch, Map.get(request, "runs", [])) do
      json(conn, Protocol.reconcile(snapshot))
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def acknowledge_command(conn, _params) do
    command_id = path_param(conn, "command_id")
    acknowledgement_id = path_param(conn, "ack_id")
    body = body_params(conn)

    if Map.has_key?(body, "acknowledgement_id") do
      Protocol.error(conn, :invalid_request)
    else
      fence =
        body
        |> Map.drop(["command_id", "ack_id", "run_id"])
        |> Protocol.normalize_map()

      with {:ok, run_id, outcome} <- acknowledgement_body(body),
           :ok <- owns_command(conn, command_id),
           {:ok, command} <- Orchestration.fetch_command(command_id),
           :ok <- command_run_matches?(command, run_id),
           {:ok, command} <-
             Orchestration.acknowledge_command(command_id, fence, outcome, acknowledgement_id) do
        json(conn, Protocol.command(command))
      else
        {:error, reason} -> Protocol.error(conn, reason)
      end
    end
  end

  defp owns_machine(conn, machine_id) do
    if machine_id == conn.assigns.machine.id, do: :ok, else: {:error, :forbidden}
  end

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

  defp command_run_matches?(%{run_id: run_id}, run_id) when is_binary(run_id), do: :ok
  defp command_run_matches?(_command, _run_id), do: {:error, :invalid_request}

  defp acknowledgement_body(%{"run_id" => run_id, "outcome" => outcome})
       when is_binary(run_id) and is_binary(outcome),
       do: {:ok, run_id, outcome}

  defp acknowledgement_body(_body), do: {:error, :invalid_request}

  defp mutation_params(conn, path_keys) when is_struct(conn, Plug.Conn),
    do: conn |> body_params() |> Map.drop(path_keys)

  defp mutation_params(params, path_keys) when is_map(params), do: Map.drop(params, path_keys)

  defp body_params(conn) when is_map(conn.body_params), do: conn.body_params
  defp body_params(_conn), do: %{}

  defp body_value(body, key) do
    case Map.fetch(body, key) do
      {:ok, value} -> {:ok, value}
      :error -> {:error, :invalid_request}
    end
  end

  defp query_value(conn, key) do
    case Map.fetch(fetch_query_params(conn).query_params, key) do
      {:ok, value} -> {:ok, value}
      :error -> {:error, :invalid_request}
    end
  end

  defp path_param(conn, key), do: Map.fetch!(conn.path_params, key)

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
