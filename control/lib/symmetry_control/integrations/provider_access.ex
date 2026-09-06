defmodule SymmetryControl.Integrations.ProviderAccess do
  @moduledoc false

  use GenServer

  import Ecto.Query

  alias SymmetryControl.Integrations
  alias SymmetryControl.Integrations.{ChangeAction, Connection, ProviderActionIntent}
  alias SymmetryControl.Orchestration.{Run, Runtime, Task}
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces.{Project, ProjectResource, WorkItem}

  @salt "provider-access-v1"
  @active_states ["claimed", "running", "waiting_for_input"]
  @claim_keys ["v", "run", "task", "runtime", "runtime_epoch", "generation", "claim"]
  @change_operations ["change.upsert", "change.update"]
  @all_operations ["resource.sync" | @change_operations]
  @max_recovery_jobs 4
  @default_recovery_interval_ms 5_000
  @default_dispatch_timeout_ms 60_000

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    Process.flag(:trap_exit, true)

    case Postgrex.start_link(postgrex_options()) do
      {:ok, lock_connection} ->
        send(self(), :recovery_sweep)

        {:ok,
         %{
           jobs: %{},
           recovering: MapSet.new(),
           held_locks: MapSet.new(),
           lock_connection: lock_connection
         }}

      {:error, reason} ->
        {:stop, reason}
    end
  end

  @impl true
  def handle_call({:execute, request}, from, state) do
    {:noreply, start_dispatch_job(state, from, {:request, request})}
  end

  def handle_call({:acquire_dispatch_lock, intent_id}, {worker, _tag}, state) do
    key = advisory_key(intent_id)

    if MapSet.member?(state.held_locks, key) do
      {:reply, {:error, :state_conflict}, state}
    else
      case Postgrex.query(
             state.lock_connection,
             "SELECT pg_try_advisory_lock($1, $2)",
             key
           ) do
        {:ok, %Postgrex.Result{rows: [[true]]}} ->
          jobs = Map.update!(state.jobs, worker, &Map.put(&1, :lock_key, key))

          {:reply, :ok, %{state | jobs: jobs, held_locks: MapSet.put(state.held_locks, key)}}

        {:ok, %Postgrex.Result{rows: [[false]]}} ->
          {:reply, {:error, :state_conflict}, state}

        {:error, _reason} ->
          {:reply, {:error, :provider_failure}, state}
      end
    end
  end

  def handle_call({:release_dispatch_lock, key}, {worker, _tag}, state) do
    state = release_dispatch_lock(state, worker, key)
    {:reply, :ok, state}
  end

  @impl true
  def handle_info(:recovery_sweep, state) do
    schedule_recovery_sweep()
    {:noreply, start_recovery_jobs(state)}
  end

  def handle_info(:recover_interrupted_dispatches, state) do
    {:noreply, start_recovery_jobs(state)}
  end

  def handle_info({:provider_action_complete, pid, result}, state) do
    state = release_job_lock(state, pid)

    case pop_job(state, pid) do
      {nil, _jobs} ->
        {:noreply, state}

      {%{from: from}, state} ->
        Process.unlink(pid)
        reply(from, result)
        {:noreply, state}
    end
  end

  def handle_info({:EXIT, pid, reason}, %{lock_connection: pid} = state) do
    {:stop, {:dispatch_lock_connection_lost, reason}, state}
  end

  def handle_info({:EXIT, pid, _reason}, state) do
    state = release_job_lock(state, pid)

    case pop_job(state, pid) do
      {nil, _jobs} ->
        {:noreply, state}

      {job, state} ->
        reply(job.from, {:error, :provider_failure})

        if is_nil(job.intent_id), do: send(self(), :recover_interrupted_dispatches)
        {:noreply, state}
    end
  end

  def handle_info({:dispatch_timeout, pid}, state) do
    if Map.has_key?(state.jobs, pid), do: Process.exit(pid, :kill)
    {:noreply, state}
  end

  @impl true
  def terminate(_reason, state) do
    Enum.each(state.jobs, fn {_pid, job} -> cancel_timer(job.timeout_ref) end)

    state.jobs
    |> Map.keys()
    |> Enum.map(fn pid ->
      reference = Process.monitor(pid)
      Process.exit(pid, :kill)
      {pid, reference}
    end)
    |> Enum.each(fn {pid, reference} ->
      receive do
        {:DOWN, ^reference, :process, ^pid, _reason} -> :ok
      end
    end)

    if Process.alive?(state.lock_connection),
      do: GenServer.stop(state.lock_connection, :normal, :infinity)

    :ok
  end

  defp start_dispatch_job(state, from, work) do
    intent_id = recovery_intent_id(work)

    if is_binary(intent_id) and MapSet.member?(state.recovering, intent_id) do
      state
    else
      do_start_dispatch_job(state, from, work, intent_id)
    end
  end

  defp do_start_dispatch_job(state, from, work, intent_id) do
    owner = self()

    pid =
      spawn_link(fn ->
        result = execute_dispatch_job(work)
        send(owner, {:provider_action_complete, self(), result})
      end)

    timeout_ref = Process.send_after(self(), {:dispatch_timeout, pid}, dispatch_timeout_ms())

    job = %{
      from: from,
      intent_id: intent_id,
      lock_key: nil,
      recovery?: is_binary(intent_id),
      timeout_ref: timeout_ref
    }

    %{
      state
      | jobs: Map.put(state.jobs, pid, job),
        recovering: maybe_put_recovering(state.recovering, intent_id)
    }
  end

  defp reply(nil, _result), do: :ok
  defp reply(from, result), do: GenServer.reply(from, result)

  defp start_recovery_jobs(state) do
    available = max(@max_recovery_jobs - MapSet.size(state.recovering), 0)

    if available == 0 do
      state
    else
      try do
        Repo.all(
          from intent in ProviderActionIntent,
            where: intent.state in ["accepted", "executing"],
            order_by: [asc: intent.inserted_at, asc: intent.id],
            limit: ^available,
            select: intent.id
        )
        |> Enum.reject(&MapSet.member?(state.recovering, &1))
        |> Enum.reduce(state, fn intent_id, current ->
          start_dispatch_job(current, nil, {:recover, intent_id})
        end)
      rescue
        _error -> state
      end
    end
  end

  defp pop_job(state, pid) do
    case Map.pop(state.jobs, pid) do
      {nil, jobs} ->
        {nil, %{state | jobs: jobs}}

      {job, jobs} ->
        cancel_timer(job.timeout_ref)
        recovering = maybe_delete_recovering(state.recovering, job.intent_id)
        {job, %{state | jobs: jobs, recovering: recovering}}
    end
  end

  defp release_job_lock(state, pid) do
    case state.jobs[pid] do
      %{lock_key: key} when is_list(key) -> release_dispatch_lock(state, pid, key)
      _job -> state
    end
  end

  defp release_dispatch_lock(state, worker, key) do
    case state.jobs[worker] do
      %{lock_key: ^key} = job ->
        _ = Postgrex.query(state.lock_connection, "SELECT pg_advisory_unlock($1, $2)", key)

        state
        |> put_in([:jobs, worker], %{job | lock_key: nil})
        |> Map.update!(:held_locks, &MapSet.delete(&1, key))

      _job ->
        state
    end
  end

  defp recovery_intent_id({:recover, intent_id}), do: intent_id
  defp recovery_intent_id(_work), do: nil
  defp maybe_put_recovering(recovering, nil), do: recovering
  defp maybe_put_recovering(recovering, intent_id), do: MapSet.put(recovering, intent_id)
  defp maybe_delete_recovering(recovering, nil), do: recovering
  defp maybe_delete_recovering(recovering, intent_id), do: MapSet.delete(recovering, intent_id)

  defp cancel_timer(reference) do
    Process.cancel_timer(reference, async: false, info: false)
    :ok
  end

  defp schedule_recovery_sweep do
    case integration_config(:provider_action_recovery_interval_ms, @default_recovery_interval_ms) do
      :infinity ->
        :ok

      interval when is_integer(interval) and interval > 0 ->
        Process.send_after(self(), :recovery_sweep, interval)

      _invalid ->
        Process.send_after(self(), :recovery_sweep, @default_recovery_interval_ms)
    end
  end

  defp dispatch_timeout_ms do
    case integration_config(:provider_action_timeout_ms, @default_dispatch_timeout_ms) do
      timeout when is_integer(timeout) and timeout > 0 -> timeout
      _invalid -> @default_dispatch_timeout_ms
    end
  end

  defp integration_config(key, default) do
    :symmetry_control
    |> Application.get_env(:integrations, [])
    |> Keyword.get(key, default)
  end

  @spec lock_claim_scope(Ecto.UUID.t()) :: {:ok, map() | nil} | {:error, atom()}
  def lock_claim_scope(run_id) when is_binary(run_id) do
    case claim_scope_ids(run_id) do
      {:ok, %{required?: false}} ->
        {:ok, nil}

      {:ok, scope} ->
        project = lock_record(Project, scope.project_id)

        resources =
          Repo.all(
            from resource in ProjectResource,
              where: resource.id in ^scope.resource_ids,
              order_by: [asc: resource.id],
              lock: "FOR UPDATE"
          )

        connection_ids =
          resources
          |> Enum.map(& &1.connection_id)
          |> Enum.reject(&is_nil/1)
          |> Enum.uniq()
          |> Enum.sort()

        connections =
          Repo.all(
            from connection in Connection,
              where: connection.id in ^connection_ids,
              order_by: [asc: connection.id],
              lock: "FOR UPDATE"
          )

        work_item = lock_record(WorkItem, scope.work_item_id)

        build_locked_claim_scope(scope, project, work_item, resources, connections)

      {:error, reason} ->
        {:error, reason}
    end
  end

  def lock_claim_scope(_run_id), do: {:error, :provider_access_unavailable}

  defp build_locked_claim_scope(scope, project, work_item, resources, connections) do
    resources_by_id = Map.new(resources, &{&1.id, &1})
    connections_by_id = Map.new(connections, &{&1.id, &1})

    connected =
      Enum.flat_map(scope.resource_ids, fn resource_id ->
        with %ProjectResource{} = resource <- resources_by_id[resource_id],
             connection_id when is_binary(connection_id) <- resource.connection_id,
             %Connection{} = connection <- connections_by_id[connection_id] do
          [{resource, connection}]
        else
          _missing -> []
        end
      end)

    valid? =
      match?(%Project{status: "active"}, project) and
        match?(%WorkItem{}, work_item) and
        work_item.project_id == scope.project_id and
        work_item.orchestration_task_id == scope.task_id and
        Enum.all?(scope.resource_ids, &(&1 in bound_resource_ids(work_item))) and
        length(connected) == length(scope.resource_ids) and
        Enum.all?(connected, fn {resource, connection} ->
          resource.project_id == scope.project_id and resource.provider == connection.provider
        end)

    if valid? do
      {:ok,
       %{
         task_id: scope.task_id,
         resources: connected
       }}
    else
      {:error, :provider_access_unavailable}
    end
  end

  @spec issue(map() | nil, Run.t(), Task.t()) :: {:ok, map() | nil} | {:error, atom()}
  def issue(nil, %Run{}, %Task{} = task) do
    if provider_access_required?(task),
      do: {:error, :provider_access_unavailable},
      else: {:ok, nil}
  end

  def issue(%{task_id: task_id, resources: resources}, %Run{} = run, %Task{id: task_id} = task) do
    if provider_access_required?(task) do
      with {:ok, grants} <- build_grants(resources, task),
           true <- grants != [] || {:error, :provider_access_unavailable} do
        claims = %{
          "v" => 1,
          "run" => run.id,
          "task" => task.id,
          "runtime" => run.runtime_id,
          "runtime_epoch" => run.claimed_runtime_epoch,
          "generation" => run.generation,
          "claim" => run.claim_id
        }

        {:ok,
         %{
           path: "/api/v1/provider-actions",
           token: Phoenix.Token.sign(SymmetryControlWeb.Endpoint, @salt, claims),
           grants: grants
         }}
      end
    else
      {:error, :provider_access_unavailable}
    end
  end

  def issue(_scope, %Run{}, %Task{}), do: {:error, :provider_access_unavailable}

  @spec execute(String.t(), Ecto.UUID.t(), Ecto.UUID.t(), String.t(), map()) ::
          {:ok, map()} | {:error, atom()}
  def execute(token, action_id, resource_id, operation, input)
      when is_binary(token) and is_binary(action_id) and is_binary(resource_id) and
             is_binary(operation) and is_map(input) do
    with {:ok, action_id} <- Ecto.UUID.cast(action_id),
         {:ok, resource_id} <- Ecto.UUID.cast(resource_id),
         true <- operation in @all_operations || {:error, :invalid_request},
         {:ok, claims} <- verify_token(token),
         :ok <- reject_token_content(input, token),
         {:ok, caller_input} <- normalize_caller_input(operation, input),
         request_hash <- request_hash(resource_id, operation, caller_input) do
      try do
        GenServer.call(
          __MODULE__,
          {:execute, {claims, action_id, resource_id, operation, caller_input, request_hash}},
          :infinity
        )
      catch
        :exit, _reason -> {:error, :provider_failure}
      end
    else
      :error -> {:error, :invalid_request}
      {:error, reason} -> {:error, reason}
    end
  end

  def execute(_, _, _, _, _), do: {:error, :invalid_request}

  defp verify_token(token) do
    with {:ok, claims} <-
           Phoenix.Token.verify(SymmetryControlWeb.Endpoint, @salt, token, max_age: :infinity),
         true <- valid_claims?(claims) do
      {:ok, claims}
    else
      _ -> {:error, :unauthenticated}
    end
  end

  defp valid_claims?(claims) when is_map(claims) do
    MapSet.new(Map.keys(claims)) == MapSet.new(@claim_keys) and
      claims["v"] == 1 and
      valid_uuid?(claims["run"]) and
      valid_uuid?(claims["task"]) and
      valid_uuid?(claims["runtime"]) and
      valid_uuid?(claims["claim"]) and
      is_integer(claims["runtime_epoch"]) and claims["runtime_epoch"] > 0 and
      is_integer(claims["generation"]) and claims["generation"] > 0
  end

  defp valid_claims?(_), do: false

  defp accept_intent(claims, action_id, resource_id, operation, caller_input, request_hash) do
    Repo.transaction(fn ->
      case find_intent(claims["run"], action_id) do
        %ProviderActionIntent{} = intent ->
          existing_intent_decision(intent, request_hash, claims)

        nil ->
          context = lock_new_intent_context(claims, resource_id)

          case lock_intent(claims["run"], action_id) do
            %ProviderActionIntent{} = intent ->
              existing_intent_decision(intent, request_hash, claims)

            nil ->
              accept_new_intent(
                context,
                claims,
                action_id,
                operation,
                caller_input,
                request_hash
              )
          end
      end
    end)
  end

  defp existing_intent_decision(intent, request_hash, claims) do
    if intent.request_hash != request_hash, do: Repo.rollback(:idempotency_conflict)

    case intent.state do
      "accepted" ->
        resume_accepted_intent(intent, request_hash, claims)

      "executing" ->
        recover_executing_intent(intent, request_hash, claims)

      "unknown" ->
        retry_unknown_intent(intent, request_hash, claims)

      _state ->
        intent |> lock_existing_intent(request_hash, claims) |> completed_intent_decision()
    end
  end

  defp recover_executing_intent(intent, request_hash, claims) do
    validate_intent_claims!(intent, claims)
    intent = lock_record(ProviderActionIntent, intent.id) || Repo.rollback(:not_found)
    if intent.request_hash != request_hash, do: Repo.rollback(:idempotency_conflict)
    validate_intent_claims!(intent, claims)

    case intent.state do
      "executing" -> {:recover, intent.id}
      _state -> Repo.rollback(:state_conflict)
    end
  end

  defp completed_intent_decision(intent) do
    case intent.state do
      "succeeded" -> {:replay, intent.result}
      "failed" -> {:failed, stored_failure(intent.failure)}
      "accepted" -> Repo.rollback(:state_conflict)
      "executing" -> Repo.rollback(:state_conflict)
      "unknown" -> Repo.rollback(:state_conflict)
    end
  end

  defp resume_accepted_intent(intent, request_hash, claims) do
    context = lock_intent_context(intent)
    validate_intent_claims!(intent, claims)
    validate_live_execution!(claims, context)
    validate_bound_resource!(context)
    validate_operation!(intent.operation, context.resource, context.connection, context.task)

    intent = lock_record(ProviderActionIntent, intent.id) || Repo.rollback(:not_found)
    if intent.request_hash != request_hash, do: Repo.rollback(:idempotency_conflict)

    case intent.state do
      "accepted" -> {:execute, authorized_context(context, intent)}
      _state -> completed_intent_decision(intent)
    end
  end

  defp retry_unknown_intent(intent, request_hash, claims) do
    context = lock_intent_context(intent)
    validate_intent_claims!(intent, claims)
    validate_live_execution!(claims, context)
    validate_bound_resource!(context)
    validate_operation!(intent.operation, context.resource, context.connection, context.task)

    intent = lock_record(ProviderActionIntent, intent.id) || Repo.rollback(:not_found)
    if intent.request_hash != request_hash, do: Repo.rollback(:idempotency_conflict)

    case intent.state do
      "unknown" ->
        case intent |> ProviderActionIntent.retry_changeset() |> Repo.update() do
          {:ok, accepted} -> {:execute, authorized_context(context, accepted)}
          {:error, reason} -> Repo.rollback(reason)
        end

      _state ->
        completed_intent_decision(intent)
    end
  end

  defp accept_new_intent(context, claims, action_id, operation, caller_input, request_hash) do
    validate_live_execution!(claims, context)
    validate_bound_resource!(context)

    scoped_input = scoped_input!(operation, caller_input, context)
    validate_operation!(operation, context.resource, context.connection, context.task)

    if lock_active_resource_intent(context.run.id, context.resource.id),
      do: Repo.rollback(:state_conflict)

    attrs = %{
      run_id: context.run.id,
      task_id: context.task.id,
      runtime_id: context.runtime.id,
      project_id: context.project.id,
      work_item_id: context.work_item.id,
      resource_id: context.resource.id,
      connection_id: context.connection.id,
      action_id: action_id,
      runtime_epoch: claims["runtime_epoch"],
      generation: claims["generation"],
      claim_id: claims["claim"],
      operation: operation,
      request_hash: request_hash,
      input: scoped_input,
      state: "accepted",
      provider: context.connection.provider,
      account_ref: context.connection.account_ref,
      resource_kind: context.resource.kind,
      resource_external_ref: context.resource.external_ref,
      resource_lock_version: context.resource.lock_version,
      connection_lock_version: context.connection.lock_version,
      work_item_lock_version: context.work_item.lock_version
    }

    case %ProviderActionIntent{}
         |> ProviderActionIntent.accept_changeset(attrs)
         |> Repo.insert(log: false) do
      {:ok, intent} ->
        {:execute, authorized_context(context, intent)}

      {:error, reason} ->
        if Keyword.has_key?(reason.errors, :resource_id),
          do: Repo.rollback(:state_conflict),
          else: Repo.rollback(reason)
    end
  end

  defp lock_intent_context(intent) do
    project = lock_record(Project, intent.project_id) || Repo.rollback(:ownership_lost)
    resource = lock_record(ProjectResource, intent.resource_id) || Repo.rollback(:forbidden)
    connection = lock_record(Connection, intent.connection_id) || Repo.rollback(:forbidden)
    work_item = lock_record(WorkItem, intent.work_item_id) || Repo.rollback(:ownership_lost)
    task = lock_record(Task, intent.task_id) || Repo.rollback(:ownership_lost)
    run = lock_record(Run, intent.run_id) || Repo.rollback(:ownership_lost)
    runtime = lock_record(Runtime, intent.runtime_id) || Repo.rollback(:ownership_lost)

    %{
      project: project,
      resource: resource,
      connection: connection,
      work_item: work_item,
      task: task,
      run: run,
      runtime: runtime
    }
  end

  defp lock_new_intent_context(claims, resource_id) do
    ids = locate_intent_context(claims) || Repo.rollback(:ownership_lost)
    project = lock_record(Project, ids.project_id) || Repo.rollback(:ownership_lost)
    resource = lock_record(ProjectResource, resource_id) || Repo.rollback(:forbidden)

    connection =
      if is_binary(resource.connection_id),
        do: lock_record(Connection, resource.connection_id),
        else: nil

    connection || Repo.rollback(:forbidden)
    work_item = lock_record(WorkItem, ids.work_item_id) || Repo.rollback(:ownership_lost)
    task = lock_record(Task, claims["task"]) || Repo.rollback(:ownership_lost)
    run = lock_record(Run, claims["run"]) || Repo.rollback(:ownership_lost)
    runtime = lock_record(Runtime, claims["runtime"]) || Repo.rollback(:ownership_lost)

    %{
      project: project,
      resource: resource,
      connection: connection,
      work_item: work_item,
      task: task,
      run: run,
      runtime: runtime
    }
  end

  defp locate_intent_context(claims) do
    Repo.one(
      from run in Run,
        join: task in Task,
        on: task.id == run.task_id,
        join: item in WorkItem,
        on: item.orchestration_task_id == task.id,
        where: run.id == ^claims["run"] and task.id == ^claims["task"],
        select: %{project_id: item.project_id, work_item_id: item.id}
    )
  end

  defp validate_live_execution!(claims, context) do
    valid? =
      context.project.status == "active" and
        live_execution?(claims, context.run, context.task, context.runtime)

    unless valid?, do: Repo.rollback(:ownership_lost)
  end

  defp lock_existing_intent(intent, request_hash, claims) do
    validate_intent_claims!(intent, claims)

    task = lock_record(Task, intent.task_id) || Repo.rollback(:ownership_lost)
    run = lock_record(Run, intent.run_id) || Repo.rollback(:ownership_lost)
    runtime = lock_record(Runtime, intent.runtime_id) || Repo.rollback(:ownership_lost)
    intent = lock_record(ProviderActionIntent, intent.id) || Repo.rollback(:not_found)

    if intent.request_hash != request_hash, do: Repo.rollback(:idempotency_conflict)
    validate_intent_claims!(intent, claims)

    unless live_execution?(claims, run, task, runtime), do: Repo.rollback(:ownership_lost)
    intent
  end

  defp validate_intent_claims!(intent, claims) do
    static? =
      intent.run_id == claims["run"] and
        intent.task_id == claims["task"] and
        intent.runtime_id == claims["runtime"] and
        intent.runtime_epoch == claims["runtime_epoch"] and
        intent.generation == claims["generation"] and
        intent.claim_id == claims["claim"]

    unless static?, do: Repo.rollback(:ownership_lost)
  end

  defp live_execution?(claims, run, task, runtime) do
    run.task_id == claims["task"] and
      run.runtime_id == claims["runtime"] and
      runtime.status == "online" and
      runtime.agent_profile == task.agent_profile and
      runtime.workspace == task.workspace and
      capabilities_match?(runtime.capabilities, task.required_capabilities) and
      runtime.connection_epoch == claims["runtime_epoch"] and
      run.claimed_runtime_epoch == claims["runtime_epoch"] and
      run.generation == claims["generation"] and
      task.current_generation == claims["generation"] and
      task.attempt_generation == claims["generation"] and
      run.claim_id == claims["claim"] and
      run.state in @active_states and
      task.state in @active_states and
      not is_nil(run.lease_expires_at) and
      DateTime.compare(run.lease_expires_at, DateTime.utc_now()) == :gt
  end

  defp capabilities_match?(available, required) when is_map(available) and is_map(required) do
    Enum.all?(required, fn {key, value} ->
      Map.get(available, to_string(key), Map.get(available, key)) == value
    end)
  end

  defp capabilities_match?(_available, _required), do: false

  defp validate_bound_resource!(context) do
    provider_resource_ids =
      case provider_resource_ids(context.task) do
        {:ok, resource_ids} -> resource_ids
        {:error, reason} -> Repo.rollback(reason)
      end

    valid? =
      context.work_item.project_id == context.project.id and
        context.work_item.orchestration_task_id == context.task.id and
        context.resource.project_id == context.project.id and
        context.resource.connection_id == context.connection.id and
        context.resource.id in bound_resource_ids(context.work_item) and
        context.resource.id in provider_resource_ids

    unless valid?, do: Repo.rollback(:forbidden)
  end

  defp validate_operation!(operation, resource, connection, task) do
    case grant_operations(resource, connection, task) do
      {:ok, operations} ->
        unless operation in operations, do: Repo.rollback(:forbidden)

      {:error, reason} ->
        Repo.rollback(reason)
    end
  end

  defp scoped_input!("resource.sync", %{}, _context), do: %{}

  defp scoped_input!("change.upsert", caller_input, context) do
    case change_scope(context.task) do
      {:ok, source_branch, target_branch} ->
        caller_input
        |> Map.put("source_branch", source_branch)
        |> Map.put("target_branch", target_branch)

      :error ->
        Repo.rollback(:provider_access_unavailable)
    end
  end

  defp scoped_input!("change.update", caller_input, context) do
    case authorized_update_url(context) do
      nil -> Repo.rollback(:provider_access_unavailable)
      pull_request_url -> Map.put(caller_input, "pull_request_url", pull_request_url)
    end
  end

  defp authorized_update_url(context) do
    present(task_input(context.task)["pull_request_url"]) ||
      successful_upsert_url(context.run.id, context.resource.id)
  end

  defp successful_upsert_url(run_id, resource_id) do
    Repo.one(
      from intent in ProviderActionIntent,
        where:
          intent.run_id == ^run_id and intent.resource_id == ^resource_id and
            intent.operation == "change.upsert" and intent.state == "succeeded",
        order_by: [desc: intent.completed_at, desc: intent.id],
        limit: 1,
        select: intent.result
    )
    |> case do
      %{"delivery" => %{"pull_request_url" => url}} -> present(url)
      _result -> nil
    end
  end

  defp authorized_context(context, intent) do
    Map.merge(context, %{
      intent: intent,
      connection: authorized_connection(context.connection, intent.resource_kind, intent),
      resource: authorized_resource(context.resource, intent),
      work_item: authorized_work_item(context.work_item, intent)
    })
  end

  defp authorized_connection(connection, kind, intent) do
    required =
      case {kind, intent.operation} do
        {"repository", operation} when operation in @change_operations ->
          ["repositories", "changes"]

        {"repository", _operation} ->
          ["repositories"]

        {"work_tracking", _operation} ->
          ["work_items"]

        {"ci", _operation} ->
          ["ci"]
      end

    %{
      connection
      | provider: intent.provider,
        account_ref: intent.account_ref,
        capabilities: Enum.uniq(connection.capabilities ++ required),
        lock_version: intent.connection_lock_version
    }
  end

  defp authorized_resource(resource, intent) do
    %{
      resource
      | connection_id: intent.connection_id,
        provider: intent.provider,
        kind: intent.resource_kind,
        external_ref: intent.resource_external_ref,
        lock_version: intent.resource_lock_version
    }
  end

  defp authorized_work_item(work_item, %{operation: "change.update"} = intent) do
    %{
      work_item
      | repository_resource_id: intent.resource_id,
        external_pull_request_url: intent.input["pull_request_url"],
        lock_version: intent.work_item_lock_version
    }
  end

  defp authorized_work_item(work_item, intent) do
    %{
      work_item
      | repository_resource_id: intent.resource_id,
        lock_version: intent.work_item_lock_version
    }
  end

  defp dispatch(%{intent: %{operation: "resource.sync"}} = context) do
    Integrations.execute_accepted_resource_sync(context.connection, context.resource)
  end

  defp dispatch(%{intent: %{operation: operation, input: input}} = context)
       when operation in @change_operations do
    Integrations.execute_provider_action(
      context.connection,
      context.resource,
      context.work_item,
      operation,
      input
    )
  end

  defp execute_dispatch_job(
         {:request, {claims, action_id, resource_id, operation, caller_input, request_hash}}
       ) do
    request = {claims, action_id, resource_id, operation, caller_input, request_hash}

    case accept_intent(claims, action_id, resource_id, operation, caller_input, request_hash) do
      {:ok, {:execute, context}} ->
        with_dispatch_lock(context.intent.id, fn -> claim_and_execute(context, request_hash) end)

      {:ok, {:recover, intent_id}} ->
        case with_dispatch_lock(intent_id, fn -> recover_and_retry(intent_id, request) end) do
          {:error, :state_conflict} ->
            active_dispatch_conflict(claims, intent_id, request_hash)

          result ->
            result
        end

      {:ok, {:replay, result}} ->
        {:ok, result}

      {:ok, {:failed, reason}} ->
        {:error, reason}

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp execute_dispatch_job({:recover, intent_id}) do
    with_dispatch_lock(intent_id, fn -> recover_dispatch(intent_id) end)
  end

  defp claim_and_execute(context, request_hash) do
    case claim_dispatch(context.intent.id, request_hash) do
      {:ok, {:execute, intent, dispatch_token}} ->
        execute_owned(%{context | intent: intent}, request_hash, dispatch_token)

      {:ok, {:replay, result}} ->
        {:ok, result}

      {:ok, {:failed, reason}} ->
        {:error, reason}

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp recover_and_retry(
         intent_id,
         {claims, action_id, resource_id, operation, caller_input, request_hash}
       ) do
    case recover_dispatch_context(intent_id) do
      {:ok, {:interrupted, dispatch_token}} ->
        with {:error, :provider_failure} <- mark_dispatch_unknown(intent_id, dispatch_token),
             {:ok, {:execute, context}} <-
               accept_intent(
                 claims,
                 action_id,
                 resource_id,
                 operation,
                 caller_input,
                 request_hash
               ) do
          claim_and_execute(context, request_hash)
        else
          {:ok, {:replay, result}} -> {:ok, result}
          {:ok, {:failed, reason}} -> {:error, reason}
          {:error, reason} -> {:error, reason}
        end

      {:ok, :done} ->
        replay_after_recovery(
          claims,
          action_id,
          resource_id,
          operation,
          caller_input,
          request_hash
        )

      {:ok, {:execute, context, ^request_hash, dispatch_token}} ->
        execute_owned(context, request_hash, dispatch_token)

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp replay_after_recovery(
         claims,
         action_id,
         resource_id,
         operation,
         caller_input,
         request_hash
       ) do
    case accept_intent(claims, action_id, resource_id, operation, caller_input, request_hash) do
      {:ok, {:execute, context}} -> claim_and_execute(context, request_hash)
      {:ok, {:replay, result}} -> {:ok, result}
      {:ok, {:failed, reason}} -> {:error, reason}
      {:ok, {:recover, _intent_id}} -> {:error, :state_conflict}
      {:error, reason} -> {:error, reason}
    end
  end

  defp active_dispatch_conflict(claims, intent_id, request_hash) do
    Repo.transaction(fn ->
      intent = Repo.get(ProviderActionIntent, intent_id) || Repo.rollback(:not_found)
      intent |> lock_existing_intent(request_hash, claims) |> completed_intent_decision()
    end)
    |> case do
      {:ok, {:replay, result}} -> {:ok, result}
      {:ok, {:failed, reason}} -> {:error, reason}
      {:error, reason} -> {:error, reason}
    end
  end

  defp execute_owned(context, request_hash, dispatch_token) do
    case dispatch(context) do
      {:ok, result} ->
        finalize_success(context.intent.id, request_hash, dispatch_token, result)

      {:error, reason} ->
        finalize_failure(context.intent.id, request_hash, dispatch_token, reason)
    end
  rescue
    _error -> mark_dispatch_unknown(context.intent.id, dispatch_token)
  catch
    _kind, _reason -> mark_dispatch_unknown(context.intent.id, dispatch_token)
  end

  defp recover_dispatch(intent_id) do
    case recover_dispatch_context(intent_id) do
      {:ok, {:execute, context, request_hash, dispatch_token}} ->
        execute_owned(context, request_hash, dispatch_token)

      {:ok, {:interrupted, dispatch_token}} ->
        mark_dispatch_unknown(intent_id, dispatch_token)

      {:ok, :done} ->
        :ok

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp recover_dispatch_context(intent_id) do
    Repo.transaction(fn ->
      intent = Repo.get(ProviderActionIntent, intent_id) || Repo.rollback(:not_found)

      case intent.state do
        "accepted" ->
          context = lock_intent_context(intent)
          intent = lock_record(ProviderActionIntent, intent.id) || Repo.rollback(:not_found)

          case intent.state do
            "accepted" ->
              dispatch_token = Ecto.UUID.generate()

              case intent
                   |> ProviderActionIntent.dispatch_changeset(dispatch_token)
                   |> Repo.update() do
                {:ok, executing} ->
                  {:execute, authorized_context(context, executing), executing.request_hash,
                   dispatch_token}

                {:error, reason} ->
                  Repo.rollback(reason)
              end

            "executing" ->
              {:interrupted, intent.dispatch_token}

            _state ->
              :done
          end

        "executing" ->
          {:interrupted, intent.dispatch_token}

        _state ->
          :done
      end
    end)
  end

  defp with_dispatch_lock(intent_id, fun) do
    case GenServer.call(__MODULE__, {:acquire_dispatch_lock, intent_id}, :infinity) do
      :ok ->
        key = advisory_key(intent_id)

        try do
          fun.()
        after
          GenServer.call(__MODULE__, {:release_dispatch_lock, key}, :infinity)
        end

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp advisory_key(intent_id) do
    <<first::signed-32, second::signed-32, _rest::binary>> =
      :crypto.hash(:sha256, Ecto.UUID.dump!(intent_id))

    [first, second]
  end

  defp postgrex_options do
    config = Repo.config()

    url_options =
      case config[:url] do
        url when is_binary(url) -> Ecto.Repo.Supervisor.parse_url(url)
        _url -> []
      end

    config
    |> Keyword.merge(url_options)
    |> Keyword.take([
      :hostname,
      :endpoints,
      :socket_dir,
      :socket,
      :port,
      :database,
      :username,
      :password,
      :parameters,
      :timeout,
      :connect_timeout,
      :handshake_timeout,
      :ping_timeout,
      :ssl,
      :socket_options,
      :prepare,
      :transactions,
      :types,
      :disconnect_on_error_codes,
      :idle_interval,
      :target_server_type
    ])
  end

  defp claim_dispatch(intent_id, request_hash) do
    Repo.transaction(fn ->
      intent = lock_record(ProviderActionIntent, intent_id) || Repo.rollback(:not_found)
      if intent.request_hash != request_hash, do: Repo.rollback(:idempotency_conflict)

      case intent.state do
        "accepted" ->
          dispatch_token = Ecto.UUID.generate()

          case intent
               |> ProviderActionIntent.dispatch_changeset(dispatch_token)
               |> Repo.update() do
            {:ok, executing} -> {:execute, executing, dispatch_token}
            {:error, reason} -> Repo.rollback(reason)
          end

        "succeeded" ->
          {:replay, intent.result}

        "failed" ->
          {:failed, stored_failure(intent.failure)}

        state when state in ["executing", "unknown"] ->
          Repo.rollback(:state_conflict)
      end
    end)
  end

  defp finalize_success(intent_id, request_hash, dispatch_token, result) do
    result = normalize_json(result)

    Repo.transaction(fn ->
      intent = lock_record(ProviderActionIntent, intent_id) || Repo.rollback(:not_found)
      if intent.request_hash != request_hash, do: Repo.rollback(:idempotency_conflict)

      case intent.state do
        "succeeded" ->
          intent.result

        "executing" when intent.dispatch_token == dispatch_token ->
          case intent
               |> ProviderActionIntent.complete_changeset("succeeded", result, now())
               |> Repo.update() do
            {:ok, completed} -> completed.result
            {:error, reason} -> Repo.rollback(reason)
          end

        _state ->
          Repo.rollback(:state_conflict)
      end
    end)
  end

  defp finalize_failure(intent_id, request_hash, dispatch_token, reason) do
    {code, outcome} = provider_failure(reason)

    Repo.transaction(fn ->
      intent = lock_record(ProviderActionIntent, intent_id) || Repo.rollback(:not_found)
      if intent.request_hash != request_hash, do: Repo.rollback(:idempotency_conflict)

      case intent.state do
        "succeeded" ->
          {:replayed, intent.result}

        "executing" when intent.dispatch_token == dispatch_token ->
          failure = %{"code" => Atom.to_string(code)}
          state = failure_state(intent.operation, outcome)

          case intent
               |> ProviderActionIntent.complete_changeset(state, failure, now())
               |> Repo.update() do
            {:ok, _completed} -> {:failed, code}
            {:error, update_reason} -> Repo.rollback(update_reason)
          end

        _state ->
          Repo.rollback(:state_conflict)
      end
    end)
    |> case do
      {:ok, {:replayed, result}} -> {:ok, result}
      {:ok, {:failed, error}} -> {:error, error}
      {:error, update_reason} -> {:error, update_reason}
    end
  end

  defp provider_failure({:provider_action_failure, code, outcome})
       when is_atom(code) and outcome in [:definite, :ambiguous],
       do: {code, outcome}

  defp provider_failure(reason) when is_atom(reason), do: {reason, :definite}
  defp provider_failure(_reason), do: {:provider_failure, :ambiguous}

  defp failure_state(operation, :ambiguous) when operation in @change_operations,
    do: "unknown"

  defp failure_state(_operation, _outcome), do: "failed"

  defp mark_dispatch_unknown(intent_id, dispatch_token) do
    Repo.transaction(fn ->
      intent = lock_record(ProviderActionIntent, intent_id) || Repo.rollback(:not_found)

      if intent.state == "executing" and intent.dispatch_token == dispatch_token do
        failure = %{"code" => "provider_failure"}

        case intent
             |> ProviderActionIntent.complete_changeset("unknown", failure, now())
             |> Repo.update() do
          {:ok, _intent} -> {:error, :provider_failure}
          {:error, reason} -> Repo.rollback(reason)
        end
      else
        Repo.rollback(:state_conflict)
      end
    end)
    |> case do
      {:ok, result} -> result
      {:error, reason} -> {:error, reason}
    end
  end

  defp stored_failure(%{"code" => code}) do
    case code do
      "invalid_request" -> :invalid_request
      "unauthenticated" -> :unauthenticated
      "forbidden" -> :forbidden
      "not_found" -> :not_found
      "idempotency_conflict" -> :idempotency_conflict
      "ownership_lost" -> :ownership_lost
      "stale" -> :stale
      "state_conflict" -> :state_conflict
      "not_connected" -> :not_connected
      "provider_unauthorized" -> :provider_unauthorized
      "provider_failure" -> :provider_failure
      "provider_access_unavailable" -> :provider_access_unavailable
      _code -> :provider_failure
    end
  end

  defp stored_failure(_failure), do: :provider_failure

  defp normalize_caller_input("resource.sync", input) do
    if map_size(input) == 0, do: {:ok, %{}}, else: {:error, :invalid_request}
  end

  defp normalize_caller_input(operation, input) when operation in @change_operations do
    ChangeAction.normalize_caller_input(operation, input)
  end

  defp normalize_caller_input(_operation, _input), do: {:error, :invalid_request}

  defp reject_token_content(input, token) do
    if contains_token?(input, token),
      do: {:error, :invalid_request},
      else: :ok
  end

  defp contains_token?(value, token) when is_binary(value), do: String.contains?(value, token)

  defp contains_token?(value, token) when is_map(value) do
    Enum.any?(value, fn {key, nested} ->
      contains_token?(key, token) or contains_token?(nested, token)
    end)
  end

  defp contains_token?(value, token) when is_list(value),
    do: Enum.any?(value, &contains_token?(&1, token))

  defp contains_token?(_value, _token), do: false

  defp claim_scope_ids(run_id) do
    case Repo.one(
           from run in Run,
             join: task in Task,
             on: task.id == run.task_id,
             left_join: item in WorkItem,
             on: item.orchestration_task_id == task.id,
             where: run.id == ^run_id,
             select: %{task: task, work_item: item}
         ) do
      %{task: task, work_item: item} ->
        if provider_access_required?(task) do
          with %WorkItem{} <- item,
               {:ok, resource_ids} <- provider_resource_ids(task),
               true <-
                 Enum.all?(resource_ids, &(&1 in bound_resource_ids(item))) ||
                   {:error, :provider_access_unavailable} do
            {:ok,
             %{
               required?: true,
               task_id: task.id,
               work_item_id: item.id,
               project_id: item.project_id,
               resource_ids: resource_ids
             }}
          else
            _ -> {:error, :provider_access_unavailable}
          end
        else
          {:ok, %{required?: false}}
        end

      _ ->
        {:error, :provider_access_unavailable}
    end
  end

  defp build_grants(connected, task) do
    Enum.reduce_while(connected, {:ok, []}, fn {resource, connection}, {:ok, grants} ->
      case grant_operations(resource, connection, task) do
        {:ok, operations} ->
          grant = %{
            resource_id: resource.id,
            provider: connection.provider,
            kind: resource.kind,
            operations: operations
          }

          {:cont, {:ok, [grant | grants]}}

        {:error, reason} ->
          {:halt, {:error, reason}}
      end
    end)
    |> case do
      {:ok, grants} -> {:ok, Enum.reverse(grants)}
      error -> error
    end
  end

  defp grant_operations(%ProjectResource{kind: "work_tracking"}, connection, _task) do
    require_grant(connection, "work_items", ["resource.sync"])
  end

  defp grant_operations(%ProjectResource{kind: "ci"}, connection, _task) do
    require_grant(connection, "ci", ["resource.sync"])
  end

  defp grant_operations(%ProjectResource{kind: "repository"}, connection, task) do
    cond do
      "repositories" not in connection.capabilities ->
        {:error, :provider_access_unavailable}

      "changes" in connection.capabilities ->
        case change_scope(task) do
          {:ok, _source, _target} -> {:ok, ["resource.sync" | @change_operations]}
          :error -> {:error, :provider_access_unavailable}
        end

      change_scope_configured?(task) ->
        {:error, :provider_access_unavailable}

      true ->
        {:ok, ["resource.sync"]}
    end
  end

  defp grant_operations(_resource, _connection, _task),
    do: {:error, :provider_access_unavailable}

  defp require_grant(connection, capability, operations) do
    if capability in connection.capabilities,
      do: {:ok, operations},
      else: {:error, :provider_access_unavailable}
  end

  defp change_scope(task) do
    input = task_input(task)

    with source when is_binary(source) and source != "" <- present(input["source_branch"]),
         target when is_binary(target) and target != "" <- present(input["target_branch"]) do
      {:ok, source, target}
    else
      _ -> :error
    end
  end

  defp change_scope_configured?(task) do
    input = task_input(task)
    present(input["source_branch"]) != nil or present(input["target_branch"]) != nil
  end

  defp task_input(%Task{input: input}) when is_map(input), do: input
  defp task_input(_task), do: %{}

  defp provider_access_required?(%Task{required_capabilities: capabilities})
       when is_map(capabilities),
       do: capabilities["provider_access"] == true or capabilities[:provider_access] == true

  defp provider_access_required?(_task), do: false

  defp provider_resource_ids(task) do
    case task_input(task)["provider_resource_ids"] do
      resource_ids when is_list(resource_ids) and resource_ids != [] ->
        if Enum.all?(resource_ids, &valid_uuid?/1) and
             length(resource_ids) == length(Enum.uniq(resource_ids)),
           do: {:ok, resource_ids},
           else: {:error, :provider_access_unavailable}

      _resource_ids ->
        {:error, :provider_access_unavailable}
    end
  end

  defp bound_resource_ids(work_item) do
    [
      work_item.external_work_item_resource_id,
      work_item.repository_resource_id,
      work_item.ci_resource_id
    ]
    |> Enum.reject(&is_nil/1)
    |> Enum.uniq()
  end

  defp lock_intent(run_id, action_id) do
    Repo.one(
      from intent in ProviderActionIntent,
        where: intent.run_id == ^run_id and intent.action_id == ^action_id,
        lock: "FOR UPDATE"
    )
  end

  defp find_intent(run_id, action_id) do
    Repo.get_by(ProviderActionIntent, run_id: run_id, action_id: action_id)
  end

  defp lock_active_resource_intent(run_id, resource_id) do
    Repo.one(
      from intent in ProviderActionIntent,
        where:
          intent.run_id == ^run_id and intent.resource_id == ^resource_id and
            intent.state in ["accepted", "executing", "unknown"],
        lock: "FOR UPDATE"
    )
  end

  defp lock_record(schema, id) do
    Repo.one(from record in schema, where: record.id == ^id, lock: "FOR UPDATE")
  end

  defp request_hash(resource_id, operation, input) do
    :crypto.hash(
      :sha256,
      :erlang.term_to_binary(%{
        "resource_id" => resource_id,
        "operation" => operation,
        "input" => input
      })
    )
  end

  defp normalize_json(%DateTime{} = value), do: DateTime.to_iso8601(value)

  defp normalize_json(value) when is_map(value) do
    Map.new(value, fn {key, nested} -> {to_string(key), normalize_json(nested)} end)
  end

  defp normalize_json(value) when is_list(value), do: Enum.map(value, &normalize_json/1)
  defp normalize_json(value), do: value

  defp present(value) when is_binary(value) do
    case String.trim(value) do
      "" -> nil
      trimmed -> trimmed
    end
  end

  defp present(_value), do: nil
  defp valid_uuid?(value) when is_binary(value), do: match?({:ok, _}, Ecto.UUID.cast(value))
  defp valid_uuid?(_), do: false
  defp now, do: DateTime.utc_now() |> DateTime.truncate(:microsecond)
end
