defmodule SymmetryControl.Integrations.ProviderAccess do
  @moduledoc false

  use GenServer

  import Ecto.Query

  alias SymmetryControl.Goals.{Goal, GoalRevision}
  alias SymmetryControl.Integrations
  alias SymmetryControl.Integrations.{ChangeAction, Connection, ProviderActionIntent}
  alias SymmetryControl.Orchestration.{Run, Runtime, Task}
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
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
    recovery_started_at = now()

    if available == 0 do
      state
    else
      try do
        Repo.all(
          from intent in ProviderActionIntent,
            join: task in Task,
            on: task.id == intent.task_id,
            join: run in Run,
            on: run.id == intent.run_id,
            left_join: goal in Goal,
            on: goal.id == task.goal_id,
            where:
              (intent.state in ["accepted", "executing"] and
                 (is_nil(task.goal_id) or goal.state != "paused" or
                    is_nil(run.lease_expires_at) or
                    run.lease_expires_at <= ^recovery_started_at)) or
                (intent.state == "unknown" and not is_nil(task.goal_id) and
                   fragment("COALESCE(?->>'readback_observed_at', '') = ''", intent.failure)),
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

      {:ok, %{goal_id: goal_id} = scope} when is_binary(goal_id) ->
        lock_goal_claim_scope(scope)

      {:ok, scope} ->
        lock_legacy_claim_scope(scope)

      {:error, reason} ->
        {:error, reason}
    end
  end

  def lock_claim_scope(_run_id), do: {:error, :provider_access_unavailable}

  @doc false
  @spec exact_claim_replay?(Ecto.UUID.t(), map()) :: boolean()
  def exact_claim_replay?(run_id, request) when is_binary(run_id) and is_map(request) do
    case Repo.one(
           from run in Run,
             join: task in Task,
             on: task.id == run.task_id,
             join: runtime in Runtime,
             on: runtime.id == run.runtime_id,
             where: run.id == ^run_id,
             select: %{run: run, task: task, runtime: runtime}
         ) do
      %{run: run, task: task, runtime: runtime} ->
        run.runtime_id == claim_value(request, "runtime_id") and
          runtime.id == claim_value(request, "runtime_id") and
          runtime.connection_epoch == claim_value(request, "runtime_epoch") and
          task.current_generation == claim_value(request, "generation") and
          run.generation == claim_value(request, "generation") and
          run.state in ["claimed", "cancelling"] and
          run.claim_id == claim_value(request, "claim_id") and
          run.claimed_runtime_epoch == claim_value(request, "runtime_epoch") and
          unexpired_claim_lease?(run.lease_expires_at)

      _missing ->
        false
    end
  end

  def exact_claim_replay?(_run_id, _request), do: false

  defp unexpired_claim_lease?(%DateTime{} = expiry),
    do: DateTime.compare(expiry, DateTime.utc_now()) == :gt

  defp unexpired_claim_lease?(_expiry), do: false

  defp claim_value(request, "runtime_id"),
    do: Map.get(request, "runtime_id", Map.get(request, :runtime_id))

  defp claim_value(request, "runtime_epoch"),
    do: Map.get(request, "runtime_epoch", Map.get(request, :runtime_epoch))

  defp claim_value(request, "generation"),
    do: Map.get(request, "generation", Map.get(request, :generation))

  defp claim_value(request, "claim_id"),
    do: Map.get(request, "claim_id", Map.get(request, :claim_id))

  defp lock_legacy_claim_scope(scope) do
    project = lock_record(Project, scope.project_id)
    {resources, connections} = lock_claim_resources(scope.resource_ids)
    work_item = lock_record(WorkItem, scope.work_item_id)
    build_locked_claim_scope(scope, project, work_item, resources, connections)
  end

  # A Goal-aware claim is new authority. Read its Project identity before
  # locking, then keep the documented Project -> Goal order through the
  # remaining provider scope. Legacy claims retain their original order.
  defp lock_goal_claim_scope(scope) do
    with project_id when is_binary(project_id) <- scope.project_id,
         %Project{} = project <- lock_project_for_goal(project_id),
         %Goal{} = goal <- lock_record(Goal, scope.goal_id),
         {resources, connections} <- lock_claim_resources(scope.resource_ids),
         %WorkItem{} = work_item <- lock_record(WorkItem, scope.work_item_id),
         %Task{} = task <- lock_record(Task, scope.task_id),
         %GoalRevision{} = revision <- lock_goal_revision(goal),
         true <- goal_claim_scope_links?(scope, project, goal, revision, work_item, task),
         {:ok, claim_scope} <-
           build_locked_claim_scope(scope, project, work_item, resources, connections) do
      {:ok,
       Map.merge(claim_scope, %{
         goal: goal,
         revision: revision,
         work_item: work_item,
         task: task
       })}
    else
      _missing -> {:error, :provider_access_unavailable}
    end
  end

  defp goal_claim_scope_links?(scope, project, goal, revision, work_item, task) do
    project.id == scope.project_id and
      goal.project_id == project.id and
      work_item.id == scope.work_item_id and
      work_item.project_id == project.id and
      work_item.goal_id == goal.id and
      task.id == scope.task_id and
      goal_task_membership_current?(goal, revision, task, work_item)
  end

  defp lock_claim_resources(resource_ids) do
    resources =
      Repo.all(
        from resource in ProjectResource,
          where: resource.id in ^resource_ids,
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

    {resources, connections}
  end

  defp validate_goal_claim_authority(goal, revision, task, work_item, claim_scope) do
    allowed_operations =
      Map.new(claim_scope.resources, fn {resource, connection} ->
        {resource.id, goal_allowed_operations(revision, resource, connection, task)}
      end)

    valid? =
      goal.state == "active" and
        goal.current_revision == revision.revision and
        goal.project_id == work_item.project_id and
        goal_task_membership_current?(goal, revision, task, work_item) and
        Enum.all?(claim_scope.resources, fn {resource, _connection} ->
          goal_resource_allowed?(revision, resource.id) and
            Map.get(allowed_operations, resource.id, []) != []
        end)

    if valid?, do: {:ok, allowed_operations}, else: {:error, :provider_access_unavailable}
  end

  defp validate_goal_provider_authority!(context, operation) do
    unless goal_provider_authorized?(context, operation),
      do: Repo.rollback(:provider_access_unavailable)
  end

  defp goal_provider_authorized?(%{goal: %Goal{} = goal} = context, operation) do
    goal.state == "active" and
      context.project.status == "active" and
      goal.current_revision == context.revision.revision and
      goal.project_id == context.project.id and
      goal_task_membership_current?(goal, context.revision, context.task, context.work_item) and
      goal_resource_allowed?(context.revision, context.resource.id) and
      operation in goal_allowed_operations(
        context.revision,
        context.resource,
        context.connection,
        context.task
      )
  end

  defp goal_provider_authorized?(_context, _operation), do: true

  defp unstarted_goal_recovery_status(
         %ProviderActionIntent{} = intent,
         %{goal: %Goal{} = goal} = context,
         operation
       ) do
    cond do
      goal_provider_authorized?(context, operation) and
          persisted_intent_execution_live?(intent, context) ->
        :authorized

      not persisted_intent_execution_live?(intent, context) ->
        :ownership_lost

      goal.state == "paused" and
        context.project.status == "active" and
        goal.current_revision == context.revision.revision and
        goal.project_id == context.project.id and
        goal_task_membership_current?(goal, context.revision, context.task, context.work_item) and
        goal_resource_allowed?(context.revision, context.resource.id) and
          operation in goal_allowed_operations(
            context.revision,
            context.resource,
            context.connection,
            context.task
          ) ->
        :paused

      true ->
        :revoked
    end
  end

  defp unstarted_goal_recovery_status(_intent, _context, _operation), do: :authorized

  defp persisted_intent_execution_live?(%ProviderActionIntent{} = intent, context) do
    live_execution?(intent_execution_claims(intent), context.run, context.task, context.runtime)
  end

  defp intent_execution_claims(%ProviderActionIntent{} = intent) do
    %{
      "run" => intent.run_id,
      "task" => intent.task_id,
      "runtime" => intent.runtime_id,
      "runtime_epoch" => intent.runtime_epoch,
      "generation" => intent.generation,
      "claim" => intent.claim_id
    }
  end

  # A cancelled or superseded Goal may no longer authorize a new effect, but a
  # previously dispatched unknown intent still needs a bounded readback of its
  # immutable original target.
  defp validate_goal_readback_target!(intent, context) do
    unless goal_readback_target_valid?(intent, context), do: Repo.rollback(:ownership_lost)
  end

  defp goal_readback_target_valid?(intent, context) do
    context.goal.project_id == intent.project_id and
      context.task.id == intent.task_id and
      context.task.goal_id == context.goal.id and
      context.work_item.id == intent.work_item_id and
      context.work_item.goal_id == context.goal.id and
      context.run.id == intent.run_id and
      context.run.task_id == intent.task_id and
      context.run.runtime_id == intent.runtime_id and
      context.run.generation == intent.generation and
      context.runtime.id == intent.runtime_id and
      context.resource.id == intent.resource_id and
      context.connection.id == intent.connection_id
  end

  defp validate_public_goal_execution_binding!(context) do
    current? =
      context.task.goal_id == context.goal.id and
        context.task.goal_revision == context.goal.current_revision and
        context.work_item.goal_id == context.goal.id and
        context.work_item.admitted_revision == context.goal.current_revision

    unless current?, do: Repo.rollback(:ownership_lost)
  end

  defp immutable_readback_context(context, intent) do
    Map.merge(context, %{
      intent: intent,
      connection: immutable_readback_connection(context.connection, intent),
      resource: authorized_resource(context.resource, intent)
    })
  end

  defp immutable_readback_connection(connection, intent) do
    %{
      connection
      | provider: intent.provider,
        account_ref: intent.account_ref,
        capabilities: immutable_readback_capabilities(intent)
    }
  end

  defp immutable_readback_capabilities(%{resource_kind: "repository", operation: operation})
       when operation in @change_operations,
       do: ["repositories", "changes"]

  defp immutable_readback_capabilities(%{resource_kind: "repository"}), do: ["repositories"]
  defp immutable_readback_capabilities(%{resource_kind: "work_tracking"}), do: ["work_items"]
  defp immutable_readback_capabilities(%{resource_kind: "ci"}), do: ["ci"]

  defp goal_task_membership_current?(goal, revision, task, work_item) do
    task_work_item_membership(task) == :durable and
      task.goal_id == goal.id and
      task.goal_revision == revision.revision and
      task.work_item_id == work_item.id and
      work_item.goal_id == goal.id and
      work_item.admitted_revision == revision.revision
  end

  defp goal_allowed_operations(revision, resource, connection, task) do
    with {:ok, operations} <- grant_operations(resource, connection, task),
         {:ok, scope} <- frozen_goal_provider_scope(task),
         {:ok, frozen_operations} <- frozen_goal_provider_operations(scope, resource.id) do
      operations
      |> Enum.filter(&goal_action_allowed?(revision, &1))
      |> Enum.filter(&(&1 in frozen_operations))
    else
      _reason -> []
    end
  end

  defp goal_action_allowed?(revision, operation) do
    operation in goal_allowed_actions(revision.authority_policy) and
      operation in goal_allowed_actions(revision.execution_policy)
  end

  defp goal_allowed_actions(policy) when is_map(policy) do
    case Map.get(policy, "allowed_actions", Map.get(policy, :allowed_actions, [])) do
      actions when is_list(actions) -> actions
      _invalid -> []
    end
  end

  defp goal_allowed_actions(_policy), do: []

  defp goal_resource_allowed?(revision, resource_id) do
    case goal_allowed_resource_ids(revision) do
      [] -> true
      resource_ids when is_list(resource_ids) -> resource_id in resource_ids
      _invalid -> false
    end
  end

  defp goal_allowed_resource_ids(revision) do
    policy = revision.execution_policy || %{}

    case Map.get(policy, "allowed_resource_ids", Map.get(policy, :allowed_resource_ids, [])) do
      resource_ids when is_list(resource_ids) -> resource_ids
      _invalid -> nil
    end
  end

  defp lock_goal_revision(goal) do
    Repo.one(
      from revision in GoalRevision,
        where: revision.goal_id == ^goal.id and revision.revision == ^goal.current_revision
    )
  end

  defp goal_project_id(goal_id) do
    Repo.one(from goal in Goal, where: goal.id == ^goal_id, select: goal.project_id)
  end

  defp intent_goal_id(intent) do
    Repo.one(from task in Task, where: task.id == ^intent.task_id, select: task.goal_id)
  end

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
        work_item_belongs_to_task?(work_item, scope) and
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

  def issue(
        %{task_id: task_id, resources: resources} = scope,
        %Run{} = run,
        %Task{id: task_id} = task
      ) do
    if provider_access_required?(task) do
      with {:ok, allowed_operations} <- claim_scope_operations(scope, task),
           {:ok, grants} <- build_grants(resources, task, allowed_operations),
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

        {:ok, signed_access(claims, grants)}
      end
    else
      {:error, :provider_access_unavailable}
    end
  end

  def issue(_scope, %Run{}, %Task{}), do: {:error, :provider_access_unavailable}

  defp claim_scope_operations(
         %{goal: goal, revision: revision, work_item: work_item, resources: resources},
         task
       ) do
    validate_goal_claim_authority(goal, revision, task, work_item, %{resources: resources})
  end

  defp claim_scope_operations(_scope, _task), do: {:ok, nil}

  @spec persist_claim_access(Run.t(), map() | nil) :: {:ok, Run.t()} | {:error, atom()}
  def persist_claim_access(%Run{} = run, provider_access)
      when provider_access in [nil] or is_map(provider_access) do
    snapshot = claim_access_snapshot(provider_access)

    {updated, _rows} =
      from(current in Run,
        where:
          current.id == ^run.id and current.task_id == ^run.task_id and
            current.runtime_id == ^run.runtime_id and
            current.generation == ^run.generation and current.claim_id == ^run.claim_id and
            current.claimed_runtime_epoch == ^run.claimed_runtime_epoch and
            is_nil(current.provider_access_snapshot)
      )
      |> Repo.update_all(set: [provider_access_snapshot: snapshot, updated_at: now()])

    if updated == 1,
      do: {:ok, %{run | provider_access_snapshot: snapshot}},
      else: {:error, :ownership_lost}
  end

  @spec replay_claim_access(Run.t(), Task.t()) :: {:ok, map() | nil} | {:error, atom()}
  def replay_claim_access(%Run{task_id: task_id} = run, %Task{id: task_id}) do
    with {:ok, snapshot} <- parse_claim_access_snapshot(run.provider_access_snapshot) do
      claims = %{
        "v" => 1,
        "run" => run.id,
        "task" => run.task_id,
        "runtime" => run.runtime_id,
        "runtime_epoch" => run.claimed_runtime_epoch,
        "generation" => run.generation,
        "claim" => run.claim_id
      }

      case snapshot do
        :none -> {:ok, nil}
        {:granted, grants} -> {:ok, signed_access(claims, grants)}
      end
    end
  end

  def replay_claim_access(%Run{}, %Task{}), do: {:error, :ownership_lost}

  defp signed_access(claims, grants) do
    %{
      path: "/api/v1/provider-actions",
      token: Phoenix.Token.sign(SymmetryControlWeb.Endpoint, @salt, claims),
      grants: grants
    }
  end

  # The persisted snapshot deliberately contains only the capability surface.
  # Provider credentials and the signed broker token are reconstructed locally.
  defp claim_access_snapshot(nil), do: %{"v" => 1, "kind" => "none"}

  defp claim_access_snapshot(%{grants: grants}) when is_list(grants) do
    %{
      "v" => 1,
      "kind" => "granted",
      "grants" => Enum.map(grants, &normalize_json/1)
    }
  end

  defp parse_claim_access_snapshot(%{"v" => 1, "kind" => "none"}), do: {:ok, :none}

  defp parse_claim_access_snapshot(%{"v" => 1, "kind" => "granted", "grants" => grants})
       when is_list(grants) do
    if Enum.all?(grants, &valid_snapshot_grant?/1),
      do: {:ok, {:granted, grants}},
      else: {:error, :ownership_lost}
  end

  defp parse_claim_access_snapshot(_snapshot), do: {:error, :ownership_lost}

  defp valid_snapshot_grant?(%{
         "resource_id" => resource_id,
         "provider" => provider,
         "kind" => kind,
         "operations" => operations
       }) do
    valid_uuid?(resource_id) and provider in ["github", "azure_devops"] and
      kind in ["repository", "work_tracking", "ci"] and is_list(operations) and
      operations != [] and Enum.all?(operations, &(&1 in @all_operations))
  end

  defp valid_snapshot_grant?(_grant), do: false

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
         request_body <- request_body(resource_id, operation, caller_input),
         request_hash <- RequestHash.write(request_body) do
      try do
        GenServer.call(
          __MODULE__,
          {:execute,
           {claims, action_id, resource_id, operation, caller_input, request_hash, request_body}},
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

  defp accept_intent(
         claims,
         action_id,
         resource_id,
         operation,
         caller_input,
         request_hash,
         request_body
       ) do
    Repo.transaction(fn ->
      case find_intent(claims["run"], action_id) do
        %ProviderActionIntent{} = intent ->
          existing_intent_decision(intent, request_hash, request_body, claims)

        nil ->
          context = lock_new_intent_context(claims, resource_id)

          case lock_intent(claims["run"], action_id) do
            %ProviderActionIntent{} = intent ->
              existing_intent_decision(intent, request_hash, request_body, claims)

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

  defp existing_intent_decision(intent, request_hash, request_body, claims) do
    unless RequestHash.matches?(intent.request_hash, intent.request_hash_version, request_body),
      do: Repo.rollback(:idempotency_conflict)

    case intent.state do
      "accepted" ->
        resume_accepted_intent(intent, request_hash, request_body, claims)

      "executing" ->
        recover_executing_intent(intent, request_hash, request_body, claims)

      "unknown" ->
        retry_unknown_intent(intent, request_hash, request_body, claims)

      _state ->
        intent |> lock_existing_intent(request_body, claims) |> completed_intent_decision()
    end
  end

  defp recover_executing_intent(intent, _request_hash, request_body, claims) do
    context = lock_intent_context(intent)
    validate_intent_claims!(intent, claims)
    intent = lock_record(ProviderActionIntent, intent.id) || Repo.rollback(:not_found)

    unless RequestHash.matches?(intent.request_hash, intent.request_hash_version, request_body),
      do: Repo.rollback(:idempotency_conflict)

    validate_intent_claims!(intent, claims)

    if goal_managed_intent?(intent) do
      validate_public_goal_execution_binding!(context)
      validate_live_execution!(claims, context)
      validate_bound_resource!(context)
      validate_operation!(intent.operation, context.resource, context.connection, context.task)
      validate_goal_provider_authority!(context, intent.operation)
    end

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

  defp resume_accepted_intent(intent, _request_hash, request_body, claims) do
    context = lock_intent_context(intent)
    validate_intent_claims!(intent, claims)

    intent = lock_record(ProviderActionIntent, intent.id) || Repo.rollback(:not_found)

    unless RequestHash.matches?(intent.request_hash, intent.request_hash_version, request_body),
      do: Repo.rollback(:idempotency_conflict)

    case intent.state do
      "accepted" ->
        if goal_managed_intent?(intent) and not persisted_intent_execution_live?(intent, context) do
          fail_unstarted_goal_intent(intent, :ownership_lost)
          {:failed, :ownership_lost}
        else
          validate_live_execution!(claims, context)
          validate_bound_resource!(context)

          validate_operation!(
            intent.operation,
            context.resource,
            context.connection,
            context.task
          )

          validate_goal_provider_authority!(context, intent.operation)
          {:execute, authorized_context(context, intent)}
        end

      _state ->
        completed_intent_decision(intent)
    end
  end

  defp retry_unknown_intent(intent, _request_hash, request_body, claims) do
    context = lock_intent_context(intent)
    validate_intent_claims!(intent, claims)

    intent = lock_record(ProviderActionIntent, intent.id) || Repo.rollback(:not_found)

    unless RequestHash.matches?(intent.request_hash, intent.request_hash_version, request_body),
      do: Repo.rollback(:idempotency_conflict)

    if goal_managed_intent?(intent) do
      validate_goal_readback_target!(intent, context)
      validate_public_goal_execution_binding!(context)
      validate_live_execution!(claims, context)
      validate_bound_resource!(context)

      validate_operation!(
        intent.operation,
        context.resource,
        context.connection,
        context.task
      )

      validate_goal_provider_authority!(context, intent.operation)

      case intent.state do
        "unknown" -> {:readback, immutable_readback_context(context, intent)}
        _state -> completed_intent_decision(intent)
      end
    else
      validate_live_execution!(claims, context)
      validate_bound_resource!(context)
      validate_operation!(intent.operation, context.resource, context.connection, context.task)
      validate_goal_provider_authority!(context, intent.operation)

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
  end

  defp accept_new_intent(
         context,
         claims,
         action_id,
         operation,
         caller_input,
         {request_hash, request_hash_version}
       ) do
    validate_live_execution!(claims, context)
    validate_bound_resource!(context)

    scoped_input = scoped_input!(operation, caller_input, context)
    validate_operation!(operation, context.resource, context.connection, context.task)
    validate_goal_provider_authority!(context, operation)

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
      request_hash_version: request_hash_version,
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
    case intent_goal_id(intent) do
      goal_id when is_binary(goal_id) -> lock_goal_intent_context(intent, goal_id)
      _legacy -> lock_legacy_intent_context(intent)
    end
  end

  defp lock_legacy_intent_context(intent) do
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

    case ids do
      %{goal_id: goal_id} when is_binary(goal_id) ->
        lock_goal_new_intent_context(ids, claims, resource_id)

      _legacy ->
        lock_legacy_new_intent_context(ids, claims, resource_id)
    end
  end

  defp lock_legacy_new_intent_context(ids, claims, resource_id) do
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

  # A preflight read determines whether the immutable Task membership is Goal
  # managed. The authoritative rows are then locked in the documented order.
  defp lock_goal_intent_context(intent, goal_id) do
    project_id = goal_project_id(goal_id) || Repo.rollback(:ownership_lost)
    project = lock_project_for_goal(project_id) || Repo.rollback(:ownership_lost)
    goal = lock_record(Goal, goal_id) || Repo.rollback(:ownership_lost)
    resource = lock_record(ProjectResource, intent.resource_id) || Repo.rollback(:forbidden)
    connection = lock_record(Connection, intent.connection_id) || Repo.rollback(:forbidden)
    work_item = lock_goal_work_item(intent.work_item_id)
    task = lock_record(Task, intent.task_id) || Repo.rollback(:ownership_lost)
    run = lock_record(Run, intent.run_id) || Repo.rollback(:ownership_lost)
    runtime = lock_record(Runtime, intent.runtime_id) || Repo.rollback(:ownership_lost)
    revision = lock_goal_revision(goal) || Repo.rollback(:ownership_lost)

    context = %{
      goal: goal,
      revision: revision,
      project: project,
      resource: resource,
      connection: connection,
      work_item: work_item,
      task: task,
      run: run,
      runtime: runtime
    }

    validate_goal_intent_context_links!(context, intent, project_id)
    context
  end

  defp lock_goal_new_intent_context(ids, claims, resource_id) do
    project_id = ids.project_id || Repo.rollback(:ownership_lost)
    project = lock_project_for_goal(project_id) || Repo.rollback(:ownership_lost)
    goal = lock_record(Goal, ids.goal_id) || Repo.rollback(:ownership_lost)
    resource = lock_record(ProjectResource, resource_id) || Repo.rollback(:forbidden)

    connection =
      if is_binary(resource.connection_id),
        do: lock_record(Connection, resource.connection_id),
        else: nil

    connection || Repo.rollback(:forbidden)
    work_item = lock_goal_work_item(ids.work_item_id)
    task = lock_record(Task, claims["task"]) || Repo.rollback(:ownership_lost)
    run = lock_record(Run, claims["run"]) || Repo.rollback(:ownership_lost)
    runtime = lock_record(Runtime, claims["runtime"]) || Repo.rollback(:ownership_lost)
    revision = lock_goal_revision(goal) || Repo.rollback(:ownership_lost)

    context = %{
      goal: goal,
      revision: revision,
      project: project,
      resource: resource,
      connection: connection,
      work_item: work_item,
      task: task,
      run: run,
      runtime: runtime
    }

    validate_goal_new_intent_context_links!(context, ids, claims, project_id)
    context
  end

  defp validate_goal_intent_context_links!(context, intent, project_id) do
    valid? =
      goal_context_links?(context, project_id) and
        context.project.id == intent.project_id and
        context.work_item.id == intent.work_item_id and
        context.task.id == intent.task_id and
        context.run.id == intent.run_id and
        context.runtime.id == intent.runtime_id and
        context.resource.id == intent.resource_id and
        context.connection.id == intent.connection_id

    unless valid?, do: Repo.rollback(:ownership_lost)
  end

  defp validate_goal_new_intent_context_links!(context, ids, claims, project_id) do
    valid? =
      goal_context_links?(context, project_id) and
        context.project.id == ids.project_id and
        context.work_item.id == ids.work_item_id and
        context.task.id == claims["task"] and
        context.run.id == claims["run"] and
        context.runtime.id == claims["runtime"]

    unless valid?, do: Repo.rollback(:ownership_lost)
  end

  defp goal_context_links?(context, project_id) do
    context.project.id == project_id and
      context.goal.project_id == context.project.id and
      context.resource.project_id == context.project.id and
      context.resource.connection_id == context.connection.id and
      context.work_item.project_id == context.project.id and
      context.work_item.goal_id == context.goal.id and
      context.task.goal_id == context.goal.id and
      context.task.work_item_id == context.work_item.id and
      context.run.task_id == context.task.id and
      context.run.runtime_id == context.runtime.id
  end

  defp locate_intent_context(claims) do
    task =
      Repo.one(
        from run in Run,
          join: task in Task,
          on: task.id == run.task_id,
          where: run.id == ^claims["run"] and task.id == ^claims["task"],
          select: task
      )

    case task do
      %Task{goal_id: goal_id} = task when is_binary(goal_id) ->
        %{
          goal_id: goal_id,
          project_id: goal_project_id(goal_id),
          work_item_id: Map.get(task, :work_item_id)
        }

      %Task{} ->
        case task_work_item(task) do
          %WorkItem{} = work_item ->
            %{project_id: work_item.project_id, work_item_id: work_item.id}

          _missing ->
            nil
        end

      _missing ->
        nil
    end
  end

  defp lock_goal_work_item(work_item_id) when is_binary(work_item_id),
    do: lock_record(WorkItem, work_item_id) || Repo.rollback(:ownership_lost)

  defp lock_goal_work_item(_work_item_id), do: Repo.rollback(:ownership_lost)

  defp validate_live_execution!(claims, context) do
    valid? =
      context.project.status == "active" and
        live_execution?(claims, context.run, context.task, context.runtime)

    unless valid?, do: Repo.rollback(:ownership_lost)
  end

  defp validate_persisted_intent_execution!(intent, context) do
    claims = %{
      "run" => intent.run_id,
      "task" => intent.task_id,
      "runtime" => intent.runtime_id,
      "runtime_epoch" => intent.runtime_epoch,
      "generation" => intent.generation,
      "claim" => intent.claim_id
    }

    validate_live_execution!(claims, context)
  end

  defp goal_managed_intent?(%ProviderActionIntent{} = intent) do
    is_binary(intent_goal_id(intent))
  end

  defp goal_managed_intent?(_intent), do: false

  defp lock_existing_intent(intent, request_body, claims) do
    validate_intent_claims!(intent, claims)

    task = lock_record(Task, intent.task_id) || Repo.rollback(:ownership_lost)
    run = lock_record(Run, intent.run_id) || Repo.rollback(:ownership_lost)
    runtime = lock_record(Runtime, intent.runtime_id) || Repo.rollback(:ownership_lost)
    intent = lock_record(ProviderActionIntent, intent.id) || Repo.rollback(:not_found)

    unless RequestHash.matches?(intent.request_hash, intent.request_hash_version, request_body),
      do: Repo.rollback(:idempotency_conflict)

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
        work_item_belongs_to_task?(context.work_item, context.task) and
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
    if goal_managed_task?(context.task) do
      case frozen_goal_provider_scope(context.task) do
        {:ok, %{change_target: %{"kind" => "pull_request", "pull_request_url" => url}}} ->
          present(url)

        {:ok, %{change_target: %{"kind" => "branches"}}} ->
          successful_upsert_url(context.run.id, context.resource.id)

        _scope ->
          nil
      end
    else
      present(task_input(context.task)["pull_request_url"]) ||
        successful_upsert_url(context.run.id, context.resource.id)
    end
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
    Integrations.execute_accepted_resource_sync_unprojected(context.connection, context.resource)
  end

  defp dispatch(%{intent: %{operation: operation, input: input}} = context)
       when operation in @change_operations do
    Integrations.execute_unprojected_provider_action(
      context.connection,
      context.resource,
      context.work_item,
      operation,
      input
    )
  end

  defp execute_dispatch_job(
         {:request,
          {claims, action_id, resource_id, operation, caller_input, request_hash, request_body}}
       ) do
    request =
      {claims, action_id, resource_id, operation, caller_input, request_hash, request_body}

    case accept_intent(
           claims,
           action_id,
           resource_id,
           operation,
           caller_input,
           request_hash,
           request_body
         ) do
      {:ok, {:execute, context}} ->
        with_dispatch_lock(context.intent.id, fn ->
          claim_and_execute(context, context.intent.request_hash)
        end)

      {:ok, {:readback, context}} ->
        with_dispatch_lock(context.intent.id, fn -> reconcile_unknown_intent(context) end)

      {:ok, {:recover, intent_id}} ->
        case with_dispatch_lock(intent_id, fn -> recover_and_retry(intent_id, request) end) do
          {:error, :state_conflict} ->
            active_dispatch_conflict(claims, intent_id, request_body)

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
      {:ok, {:execute, %ProviderActionIntent{} = intent, dispatch_token}} ->
        execute_owned(%{context | intent: intent}, request_hash, dispatch_token)

      {:ok, {:execute, authorized, dispatch_token}} ->
        execute_owned(authorized, request_hash, dispatch_token)

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
         {claims, action_id, resource_id, operation, caller_input, request_hash, request_body}
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
                 request_hash,
                 request_body
               ) do
          claim_and_execute(context, context.intent.request_hash)
        else
          {:ok, {:replay, result}} -> {:ok, result}
          {:ok, {:failed, reason}} -> {:error, reason}
          {:ok, {:readback, context}} -> reconcile_unknown_intent(context)
          {:error, reason} -> {:error, reason}
        end

      {:ok, :done} ->
        replay_after_recovery(
          claims,
          action_id,
          resource_id,
          operation,
          caller_input,
          request_hash,
          request_body
        )

      {:ok, {:execute, context, _stored_hash, dispatch_token}} ->
        execute_owned(context, context.intent.request_hash, dispatch_token)

      {:ok, {:readback, context}} ->
        reconcile_unknown_intent(context)

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
         request_hash,
         request_body
       ) do
    case accept_intent(
           claims,
           action_id,
           resource_id,
           operation,
           caller_input,
           request_hash,
           request_body
         ) do
      {:ok, {:execute, context}} -> claim_and_execute(context, context.intent.request_hash)
      {:ok, {:replay, result}} -> {:ok, result}
      {:ok, {:failed, reason}} -> {:error, reason}
      {:ok, {:recover, _intent_id}} -> {:error, :state_conflict}
      {:ok, {:readback, context}} -> reconcile_unknown_intent(context)
      {:error, reason} -> {:error, reason}
    end
  end

  defp active_dispatch_conflict(claims, intent_id, request_body) do
    Repo.transaction(fn ->
      intent = Repo.get(ProviderActionIntent, intent_id) || Repo.rollback(:not_found)
      intent |> lock_existing_intent(request_body, claims) |> completed_intent_decision()
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
        finalize_success(context, request_hash, dispatch_token, result)

      {:error, reason} ->
        finalize_failure(context, request_hash, dispatch_token, reason)
    end
  rescue
    _error -> mark_dispatch_unknown(context.intent.id, dispatch_token)
  catch
    _kind, _reason -> mark_dispatch_unknown(context.intent.id, dispatch_token)
  end

  defp reconcile_unknown_intent(context) do
    case Integrations.execute_accepted_resource_sync_unprojected(
           context.connection,
           context.resource
         ) do
      {:ok, observation} ->
        # Generic resource sync does not bind an observation to the original
        # change action, input, branches, or pull request. It is therefore
        # useful diagnostic evidence but cannot settle or retry an unknown action.
        with :ok <- mark_readback_observed(context.intent.id, :unconfirmed) do
          readback = Integrations.unprojected_resource_sync_result(context.resource, observation)

          {:ok,
           %{
             operation: context.intent.operation,
             outcome: "unknown",
             readback_status: "unconfirmed",
             projected: false,
             readback: readback
           }}
        end

      {:error, reason} ->
        _ = mark_readback_observed(context.intent.id, :unconfirmed, reason)
        {:error, reason}
    end
  end

  defp mark_readback_observed(intent_id, outcome, reason \\ nil) do
    Repo.transaction(fn ->
      intent = lock_record(ProviderActionIntent, intent_id) || Repo.rollback(:not_found)

      if intent.state == "unknown" do
        failure =
          if(is_map(intent.failure), do: intent.failure, else: %{})
          |> Map.merge(%{
            "readback_observed_at" => DateTime.to_iso8601(now()),
            "readback_status" => Atom.to_string(outcome)
          })
          |> maybe_put_readback_error(reason)

        case intent
             |> ProviderActionIntent.complete_changeset("unknown", failure, now())
             |> Repo.update() do
          {:ok, _observed} -> :ok
          {:error, reason} -> Repo.rollback(reason)
        end
      else
        Repo.rollback(:state_conflict)
      end
    end)
    |> case do
      {:ok, :ok} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  defp maybe_put_readback_error(failure, nil), do: failure

  defp maybe_put_readback_error(failure, reason) when is_atom(reason),
    do: Map.put(failure, "readback_error", Atom.to_string(reason))

  defp maybe_put_readback_error(failure, _reason),
    do: Map.put(failure, "readback_error", "provider_failure")

  defp fail_unstarted_goal_intent(intent, reason) do
    case intent
         |> ProviderActionIntent.complete_changeset(
           "failed",
           %{"code" => Atom.to_string(reason)},
           now()
         )
         |> Repo.update() do
      {:ok, _failed} -> :ok
      {:error, reason} -> Repo.rollback(reason)
    end
  end

  defp recover_dispatch(intent_id) do
    case recover_dispatch_context(intent_id) do
      {:ok, {:execute, context, request_hash, dispatch_token}} ->
        execute_owned(context, request_hash, dispatch_token)

      {:ok, {:interrupted, dispatch_token}} ->
        mark_dispatch_unknown(intent_id, dispatch_token)

      {:ok, {:readback, context}} ->
        reconcile_unknown_intent(context)

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

          case unstarted_goal_recovery_status(intent, context, intent.operation) do
            :paused ->
              :done

            :ownership_lost ->
              fail_unstarted_goal_intent(intent, :ownership_lost)
              :done

            :revoked ->
              fail_unstarted_goal_intent(intent, :provider_access_unavailable)
              :done

            :authorized ->
              if goal_managed_intent?(intent) do
                validate_persisted_intent_execution!(intent, context)
                validate_bound_resource!(context)

                validate_operation!(
                  intent.operation,
                  context.resource,
                  context.connection,
                  context.task
                )

                validate_goal_provider_authority!(context, intent.operation)
              end

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
          end

        "executing" ->
          {:interrupted, intent.dispatch_token}

        "unknown" ->
          if goal_managed_intent?(intent) do
            context = lock_intent_context(intent)
            intent = lock_record(ProviderActionIntent, intent.id) || Repo.rollback(:not_found)

            case intent.state do
              "unknown" ->
                validate_goal_readback_target!(intent, context)
                {:readback, immutable_readback_context(context, intent)}

              _state ->
                :done
            end
          else
            :done
          end

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
    intent = Repo.get(ProviderActionIntent, intent_id)

    if goal_managed_intent?(intent) do
      claim_goal_dispatch(intent, request_hash)
    else
      claim_legacy_dispatch(intent_id, request_hash)
    end
  end

  defp claim_goal_dispatch(%ProviderActionIntent{} = intent, request_hash) do
    Repo.transaction(fn ->
      context = lock_intent_context(intent)
      intent = lock_record(ProviderActionIntent, intent.id) || Repo.rollback(:not_found)
      if intent.request_hash != request_hash, do: Repo.rollback(:idempotency_conflict)

      validate_persisted_intent_execution!(intent, context)
      validate_bound_resource!(context)
      validate_operation!(intent.operation, context.resource, context.connection, context.task)
      validate_goal_provider_authority!(context, intent.operation)

      case intent.state do
        "accepted" ->
          dispatch_token = Ecto.UUID.generate()

          case intent
               |> ProviderActionIntent.dispatch_changeset(dispatch_token)
               |> Repo.update() do
            {:ok, executing} -> {:execute, authorized_context(context, executing), dispatch_token}
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

  defp claim_legacy_dispatch(intent_id, request_hash) do
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

  defp finalize_success(context, request_hash, dispatch_token, result) do
    Repo.transaction(fn ->
      intent = Repo.get(ProviderActionIntent, context.intent.id) || Repo.rollback(:not_found)
      current = lock_intent_context(intent)
      intent = lock_record(ProviderActionIntent, intent.id) || Repo.rollback(:not_found)
      if intent.request_hash != request_hash, do: Repo.rollback(:idempotency_conflict)

      case intent.state do
        "succeeded" ->
          intent.result

        "executing" when intent.dispatch_token == dispatch_token ->
          outcome =
            project_network_success(intent, current, result)
            |> normalize_json()

          case intent
               |> ProviderActionIntent.complete_changeset("succeeded", outcome, now())
               |> Repo.update() do
            {:ok, completed} -> completed.result
            {:error, reason} -> Repo.rollback(reason)
          end

        _state ->
          Repo.rollback(:state_conflict)
      end
    end)
  end

  defp finalize_failure(context, request_hash, dispatch_token, reason) do
    {code, outcome} = provider_failure(reason)

    Repo.transaction(fn ->
      intent = Repo.get(ProviderActionIntent, context.intent.id) || Repo.rollback(:not_found)
      current = lock_intent_context(intent)
      intent = lock_record(ProviderActionIntent, intent.id) || Repo.rollback(:not_found)
      if intent.request_hash != request_hash, do: Repo.rollback(:idempotency_conflict)

      case intent.state do
        "succeeded" ->
          {:replayed, intent.result}

        "executing" when intent.dispatch_token == dispatch_token ->
          if projection_authorized?(intent, current),
            do:
              Integrations.project_provider_action_failure(
                authorized_context(current, intent).resource,
                reason
              )

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

  defp project_network_success(intent, current, result) do
    if projection_authorized?(intent, current) do
      current = authorized_context(current, intent)

      case intent.operation do
        "resource.sync" ->
          {:ok, projected} =
            Integrations.project_accepted_resource_sync(
              current.connection,
              current.resource,
              result
            )

          projected

        operation when operation in @change_operations ->
          case Integrations.project_provider_action_delivery(
                 current.connection,
                 current.resource,
                 current.work_item,
                 operation,
                 result
               ) do
            {:ok, projected} ->
              projected

            {:error, _reason} ->
              Integrations.unprojected_provider_action_result(
                current.resource,
                current.work_item,
                operation,
                result
              )
          end
      end
    else
      current = authorized_context(current, intent)

      case intent.operation do
        "resource.sync" ->
          Integrations.unprojected_resource_sync_result(current.resource, result)

        operation when operation in @change_operations ->
          Integrations.unprojected_provider_action_result(
            current.resource,
            current.work_item,
            operation,
            result
          )
      end
    end
  end

  # The provider call has already occurred. Projection is allowed only while
  # the original execution fence and the current Goal authority still agree.
  defp projection_authorized?(intent, context) do
    persisted_intent_execution_live?(intent, context) and
      bound_resource?(context) and
      operation_allowed?(intent.operation, context.resource, context.connection, context.task) and
      (not Map.has_key?(context, :goal) or
         (goal_readback_target_valid?(intent, context) and
            goal_provider_authorized?(context, intent.operation)))
  end

  defp bound_resource?(context) do
    with {:ok, provider_resource_ids} <- provider_resource_ids(context.task) do
      context.work_item.project_id == context.project.id and
        work_item_belongs_to_task?(context.work_item, context.task) and
        context.resource.project_id == context.project.id and
        context.resource.connection_id == context.connection.id and
        context.resource.id in bound_resource_ids(context.work_item) and
        context.resource.id in provider_resource_ids
    else
      _reason -> false
    end
  end

  defp operation_allowed?(operation, resource, connection, task) do
    case grant_operations(resource, connection, task) do
      {:ok, operations} -> operation in operations
      {:error, _reason} -> false
    end
  end

  defp provider_failure({:provider_action_failure, code, outcome})
       when is_atom(code) and outcome in [:definite, :ambiguous],
       do: {code, outcome}

  defp provider_failure({:http, status, _body}) when status in 400..499,
    do: {:provider_failure, :definite}

  defp provider_failure(:invalid_provider_success), do: {:provider_failure, :ambiguous}
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
             where: run.id == ^run_id,
             select: task
         ) do
      %Task{} = task ->
        if provider_access_required?(task) do
          with %WorkItem{} = item <- task_work_item(task),
               {:ok, resource_ids} <- provider_resource_ids(task),
               true <-
                 Enum.all?(resource_ids, &(&1 in bound_resource_ids(item))) ||
                   {:error, :provider_access_unavailable} do
            {:ok,
             %{
               required?: true,
               task_id: task.id,
               work_item_id: item.id,
               membership: task_work_item_membership(task),
               goal_id: Map.get(task, :goal_id),
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

  # Goal tasks retain durable WorkItem membership. Goal-less tasks continue to
  # use the v1 current-task pointer until their data is migrated.
  defp task_work_item(%Task{} = task) do
    case task_work_item_membership(task) do
      :durable ->
        case Repo.get(WorkItem, Map.get(task, :work_item_id)) do
          %WorkItem{} = work_item ->
            if goal_owned_work_item?(work_item), do: work_item, else: nil

          _missing ->
            nil
        end

      :legacy ->
        Repo.get_by(WorkItem, orchestration_task_id: task.id)
    end
  end

  defp task_work_item_membership(task) do
    if is_binary(Map.get(task, :work_item_id)) and
         is_binary(Map.get(task, :goal_id)) and
         is_integer(Map.get(task, :goal_revision)) and
         is_binary(Map.get(task, :context_snapshot_id)) and
         is_binary(Map.get(task, :admission_key)) and
         is_integer(Map.get(task, :max_run_attempts)),
       do: :durable,
       else: :legacy
  end

  defp work_item_belongs_to_task?(
         work_item,
         %{task_id: task_id, work_item_id: work_item_id, membership: membership}
       ) do
    case membership do
      :durable -> work_item.id == work_item_id and goal_owned_work_item?(work_item)
      :legacy -> work_item.orchestration_task_id == task_id
    end
  end

  defp work_item_belongs_to_task?(work_item, %Task{} = task) do
    case task_work_item_membership(task) do
      :durable ->
        work_item.id == Map.get(task, :work_item_id) and goal_owned_work_item?(work_item)

      :legacy ->
        work_item.orchestration_task_id == task.id
    end
  end

  defp goal_owned_work_item?(work_item) do
    is_binary(Map.get(work_item, :goal_id)) and
      is_integer(Map.get(work_item, :admitted_revision)) and
      is_map(Map.get(work_item, :acceptance_contract))
  end

  defp build_grants(connected, task, allowed_operations) do
    Enum.reduce_while(connected, {:ok, []}, fn {resource, connection}, {:ok, grants} ->
      case grant_operations(resource, connection, task) do
        {:ok, operations} ->
          operations = restrict_operations(operations, resource.id, allowed_operations)

          if operations == [] do
            {:halt, {:error, :provider_access_unavailable}}
          else
            grant = %{
              resource_id: resource.id,
              provider: connection.provider,
              kind: resource.kind,
              operations: operations
            }

            {:cont, {:ok, [grant | grants]}}
          end

        {:error, reason} ->
          {:halt, {:error, reason}}
      end
    end)
    |> case do
      {:ok, grants} -> {:ok, Enum.reverse(grants)}
      error -> error
    end
  end

  defp restrict_operations(operations, _resource_id, nil), do: operations

  defp restrict_operations(operations, resource_id, allowed_operations)
       when is_map(allowed_operations),
       do: Enum.filter(operations, &(&1 in Map.get(allowed_operations, resource_id, [])))

  defp grant_operations(%ProjectResource{kind: "work_tracking"}, connection, _task) do
    require_grant(connection, "work_items", ["resource.sync"])
  end

  defp grant_operations(%ProjectResource{kind: "ci"}, connection, _task) do
    require_grant(connection, "ci", ["resource.sync"])
  end

  defp grant_operations(%ProjectResource{kind: "repository"} = resource, connection, task) do
    cond do
      "repositories" not in connection.capabilities ->
        {:error, :provider_access_unavailable}

      goal_managed_task?(task) ->
        case frozen_goal_provider_scope(task) do
          {:ok, scope} ->
            with {:ok, frozen_operations} <- frozen_goal_provider_operations(scope, resource.id),
                 true <-
                   "changes" in connection.capabilities or
                     Enum.all?(frozen_operations, &(&1 == "resource.sync")) do
              {:ok,
               if("changes" in connection.capabilities,
                 do: ["resource.sync" | @change_operations],
                 else: ["resource.sync"]
               )}
            else
              _invalid -> {:error, :provider_access_unavailable}
            end

          {:error, reason} ->
            {:error, reason}
        end

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
    if goal_managed_task?(task),
      do: frozen_goal_change_scope(task),
      else: legacy_change_scope(task)
  end

  defp change_scope_configured?(task) do
    if goal_managed_task?(task) do
      case frozen_goal_provider_scope(task) do
        {:ok, scope} ->
          Enum.any?(scope.resource_ids, fn resource_id ->
            {:ok, operations} = frozen_goal_provider_operations(scope, resource_id)
            Enum.any?(operations, &(&1 in @change_operations))
          end)

        {:error, _reason} ->
          true
      end
    else
      legacy_change_scope_configured?(task)
    end
  end

  defp legacy_change_scope(task) do
    input = task_input(task)

    with source when is_binary(source) and source != "" <- present(input["source_branch"]),
         target when is_binary(target) and target != "" <- present(input["target_branch"]) do
      {:ok, source, target}
    else
      _ -> :error
    end
  end

  defp legacy_change_scope_configured?(task) do
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
    if goal_managed_task?(task) do
      with {:ok, scope} <- frozen_goal_provider_scope(task), do: {:ok, scope.resource_ids}
    else
      legacy_provider_resource_ids(task)
    end
  end

  defp legacy_provider_resource_ids(task) do
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

  defp goal_managed_task?(%Task{goal_id: goal_id}) when is_binary(goal_id), do: true
  defp goal_managed_task?(_task), do: false

  defp frozen_goal_provider_scope(task) do
    case task_input(task)["provider_scope"] do
      %{
        "resource_ids" => resource_ids,
        "operations_by_resource" => operations_by_resource,
        "change_target" => change_target
      }
      when is_list(resource_ids) and is_map(operations_by_resource) ->
        with true <- resource_ids != [] and Enum.all?(resource_ids, &valid_uuid?/1),
             true <- length(resource_ids) == length(Enum.uniq(resource_ids)),
             true <-
               MapSet.equal?(
                 MapSet.new(resource_ids),
                 MapSet.new(Map.keys(operations_by_resource))
               ),
             {:ok, operations_by_resource} <-
               normalize_frozen_goal_provider_operations(resource_ids, operations_by_resource),
             {:ok, change_target} <-
               validate_frozen_goal_change_target(operations_by_resource, change_target) do
          {:ok,
           %{
             resource_ids: resource_ids,
             operations_by_resource: operations_by_resource,
             change_target: change_target
           }}
        else
          _invalid -> {:error, :provider_access_unavailable}
        end

      _invalid ->
        {:error, :provider_access_unavailable}
    end
  end

  defp normalize_frozen_goal_provider_operations(resource_ids, operations_by_resource) do
    resource_ids
    |> Enum.reduce_while({:ok, %{}}, fn resource_id, {:ok, normalized} ->
      case Map.fetch(operations_by_resource, resource_id) do
        {:ok, operations} when is_list(operations) and operations != [] ->
          if length(operations) == length(Enum.uniq(operations)) and
               Enum.all?(operations, &(&1 in @all_operations)) do
            {:cont, {:ok, Map.put(normalized, resource_id, operations)}}
          else
            {:halt, {:error, :provider_access_unavailable}}
          end

        _invalid ->
          {:halt, {:error, :provider_access_unavailable}}
      end
    end)
  end

  defp validate_frozen_goal_change_target(operations_by_resource, change_target)
       when is_map(operations_by_resource) do
    operations =
      operations_by_resource
      |> Map.values()
      |> List.flatten()
      |> Enum.uniq()

    validate_frozen_goal_change_target(operations, change_target)
  end

  defp validate_frozen_goal_change_target(["resource.sync"], nil), do: {:ok, nil}

  defp validate_frozen_goal_change_target(
         operations,
         %{"kind" => "branches", "source_branch" => source, "target_branch" => target}
       )
       when is_binary(source) and is_binary(target) do
    if "change.upsert" in operations and Enum.all?(operations, &(&1 in @all_operations)) do
      with {:ok, source} <- frozen_goal_branch(source),
           {:ok, target} <- frozen_goal_branch(target),
           true <- source != target do
        {:ok, %{"kind" => "branches", "source_branch" => source, "target_branch" => target}}
      else
        _invalid -> {:error, :provider_access_unavailable}
      end
    else
      {:error, :provider_access_unavailable}
    end
  end

  defp validate_frozen_goal_change_target(
         operations,
         %{"kind" => "pull_request", "pull_request_url" => url}
       )
       when is_binary(url) do
    if "change.update" in operations and "change.upsert" not in operations and
         Enum.all?(operations, &(&1 in @all_operations)) do
      if exact_present?(url) do
        {:ok, %{"kind" => "pull_request", "pull_request_url" => url}}
      else
        {:error, :provider_access_unavailable}
      end
    else
      {:error, :provider_access_unavailable}
    end
  end

  defp validate_frozen_goal_change_target(_operations, _change_target),
    do: {:error, :provider_access_unavailable}

  defp frozen_goal_branch(value) do
    case ChangeAction.normalize_branch(value) do
      {:ok, ^value} -> {:ok, value}
      _invalid -> {:error, :provider_access_unavailable}
    end
  end

  defp exact_present?(value), do: value != "" and value == String.trim(value)

  defp frozen_goal_provider_operations(scope, resource_id) do
    case Map.fetch(scope.operations_by_resource, resource_id) do
      {:ok, operations} -> {:ok, operations}
      :error -> {:error, :provider_access_unavailable}
    end
  end

  defp frozen_goal_change_scope(task) do
    with {:ok,
          %{
            change_target: %{
              "kind" => "branches",
              "source_branch" => source,
              "target_branch" => target
            }
          }} <- frozen_goal_provider_scope(task) do
      {:ok, source, target}
    else
      _invalid -> :error
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

  defp lock_project_for_goal(project_id) do
    Repo.one(from project in Project, where: project.id == ^project_id, lock: "FOR SHARE")
  end

  defp lock_record(schema, id) do
    Repo.one(from record in schema, where: record.id == ^id, lock: "FOR UPDATE")
  end

  defp request_body(resource_id, operation, input),
    do: %{"resource_id" => resource_id, "operation" => operation, "input" => input}

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
