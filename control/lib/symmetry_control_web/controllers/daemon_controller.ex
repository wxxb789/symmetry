defmodule SymmetryControlWeb.DaemonController do
  use SymmetryControlWeb, :controller

  alias SymmetryControl.Integrations.ProviderAccess
  alias SymmetryControl.Goals
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
          |> Map.put("heartbeat_interval_ms", configured_heartbeat)
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
    if ProviderAccess.exact_claim_replay?(run_id, request) do
      replay_claim_with_provider_access(run_id, request)
    else
      create_claim_with_provider_access(run_id, request)
    end
  end

  defp replay_claim_with_provider_access(run_id, request) do
    Repo.transaction(fn ->
      with {:ok, run, :replayed} <-
             Orchestration.claim_with_disposition(run_id, request,
               lease_duration_ms: config(:lease_duration_ms)
             ),
           {:ok, %{task: task}} <- Orchestration.task_snapshot(run.task_id),
           {:ok, provider_access} <- ProviderAccess.replay_claim_access(run, task) do
        {run, task, provider_access, :replayed}
      else
        {:ok, _run, :created} -> Repo.rollback(:ownership_lost)
        {:error, reason} -> Repo.rollback(reason)
      end
    end)
    |> claim_provider_access_result()
  end

  defp create_claim_with_provider_access(run_id, request) do
    Repo.transaction(fn ->
      # Scope locking precedes a new claim to preserve Goal -> Project -> resource
      # lock order. A replay intentionally ignores a now-stale scope result.
      provider_scope = ProviderAccess.lock_claim_scope(run_id)

      with {:ok, run, disposition} <-
             Orchestration.claim_with_disposition(run_id, request,
               lease_duration_ms: config(:lease_duration_ms)
             ),
           {:ok, %{task: task}} <- Orchestration.task_snapshot(run.task_id),
           {:ok, provider_access} <-
             provider_access_for_claim(provider_scope, disposition, run, task) do
        {run, task, provider_access, disposition}
      else
        {:error, reason} -> Repo.rollback(reason)
      end
    end)
    |> claim_provider_access_result()
  end

  defp claim_provider_access_result(result) do
    case result do
      {:ok, {run, task, provider_access, :created}} ->
        Orchestration.emit_claimed(run)
        {:ok, {run, task, provider_access}}

      {:ok, {run, task, provider_access, :replayed}} ->
        {:ok, {run, task, provider_access}}

      error ->
        error
    end
  end

  defp provider_access_for_claim(_scope, :replayed, run, task),
    do: ProviderAccess.replay_claim_access(run, task)

  defp provider_access_for_claim({:ok, scope}, :created, run, task) do
    with {:ok, provider_access} <- ProviderAccess.issue(scope, run, task),
         {:ok, _run} <- ProviderAccess.persist_claim_access(run, provider_access) do
      {:ok, provider_access}
    end
  end

  defp provider_access_for_claim({:error, reason}, :created, _run, _task), do: {:error, reason}

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

  def attach_harness_session(conn, _params) do
    run_id = path_param(conn, "run_id")

    with :ok <- owns_run(conn, run_id),
         {:ok, fence, attrs} <- fenced_body(body_params(conn), run_id, :absent),
         {:ok, receipt, disposition} <-
           Goals.attach_harness_session(conn.assigns.machine.id, run_id, fence, attrs) do
      receipt(conn, receipt, disposition)
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def append_evidence(conn, _params) do
    run_id = path_param(conn, "run_id")

    with :ok <- owns_run(conn, run_id),
         {:ok, fence, evidence} <- fenced_body(body_params(conn), run_id, :required),
         {:ok, receipt, disposition} <-
           Goals.append_evidence(conn.assigns.machine.id, run_id, fence, evidence) do
      receipt(conn, receipt, disposition)
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def record_usage(conn, _params) do
    run_id = path_param(conn, "run_id")

    with :ok <- owns_run(conn, run_id),
         {:ok, fence, usage} <- fenced_body(body_params(conn), run_id, :required),
         {:ok, receipt, disposition} <-
           Goals.record_usage(conn.assigns.machine.id, run_id, fence, usage) do
      receipt(conn, receipt, disposition)
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def fetch_run_context(conn, _params) do
    run_id = path_param(conn, "run_id")

    with :ok <- owns_run(conn, run_id),
         {:ok, fence} <- query_fence(conn),
         {:ok, context} <- Goals.fetch_run_context(conn.assigns.machine.id, run_id, fence) do
      json(conn, context)
    else
      {:error, reason} -> Protocol.error(conn, reason)
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

  @fence_fields ["runtime_id", "runtime_epoch", "generation", "claim_id", "lease_token"]

  defp fenced_body(body, run_id, run_id_mode) when is_map(body) do
    with {:ok, fence} <- fence(body),
         false <- Map.has_key?(body, "machine_id"),
         :ok <- run_id_matches?(body, run_id, run_id_mode) do
      {:ok, fence, body |> Map.drop(@fence_fields) |> Protocol.normalize_map()}
    else
      true -> {:error, :invalid_request}
      {:error, reason} -> {:error, reason}
    end
  end

  defp query_fence(conn) do
    query = fetch_query_params(conn).query_params

    with {:ok, fence} <- query_fence_values(query),
         false <- Map.has_key?(query, "machine_id"),
         true <- MapSet.subset?(MapSet.new(Map.keys(query)), MapSet.new(@fence_fields)) do
      {:ok, fence}
    else
      false -> {:error, :invalid_request}
      {:error, reason} -> {:error, reason}
    end
  end

  defp fence(params) do
    with {:ok, runtime_id} <- nonempty_string(params, "runtime_id"),
         {:ok, runtime_epoch} <- positive_integer(params, "runtime_epoch"),
         {:ok, generation} <- positive_integer(params, "generation"),
         {:ok, claim_id} <- nonempty_string(params, "claim_id"),
         {:ok, lease_token} <- nonempty_string(params, "lease_token") do
      {:ok,
       %{
         "runtime_id" => runtime_id,
         "runtime_epoch" => runtime_epoch,
         "generation" => generation,
         "claim_id" => claim_id,
         "lease_token" => lease_token
       }}
    end
  end

  defp query_fence_values(params) do
    with {:ok, runtime_id} <- nonempty_string(params, "runtime_id"),
         {:ok, runtime_epoch} <- query_positive_integer(params, "runtime_epoch"),
         {:ok, generation} <- query_positive_integer(params, "generation"),
         {:ok, claim_id} <- nonempty_string(params, "claim_id"),
         {:ok, lease_token} <- nonempty_string(params, "lease_token") do
      {:ok,
       %{
         "runtime_id" => runtime_id,
         "runtime_epoch" => runtime_epoch,
         "generation" => generation,
         "claim_id" => claim_id,
         "lease_token" => lease_token
       }}
    end
  end

  defp run_id_matches?(body, _run_id, :absent) do
    if Map.has_key?(body, "run_id"), do: {:error, :invalid_request}, else: :ok
  end

  defp run_id_matches?(%{"run_id" => run_id}, run_id, :required), do: :ok
  defp run_id_matches?(_body, _run_id, :required), do: {:error, :invalid_request}

  defp nonempty_string(params, key) do
    case Map.get(params, key) do
      value when is_binary(value) and byte_size(value) > 0 -> {:ok, value}
      _ -> {:error, :invalid_request}
    end
  end

  defp positive_integer(params, key) do
    case Map.get(params, key) do
      value when is_integer(value) and value > 0 -> {:ok, value}
      _ -> {:error, :invalid_request}
    end
  end

  defp query_positive_integer(params, key) do
    case Map.get(params, key) do
      value when is_binary(value) ->
        case Integer.parse(value) do
          {parsed, ""} when parsed > 0 -> {:ok, parsed}
          _ -> {:error, :invalid_request}
        end

      _ ->
        {:error, :invalid_request}
    end
  end

  defp receipt(conn, response, :created), do: conn |> put_status(:created) |> json(response)
  defp receipt(conn, response, :replayed), do: json(conn, response)

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
