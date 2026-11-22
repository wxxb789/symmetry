defmodule SymmetryControl.Orchestration.Claims do
  @moduledoc false

  @minimum_lease_duration_ms 30_000

  import Ecto.Query

  import SymmetryControl.Goals.Eligibility,
    only: [
      goal_task_current?: 2,
      goal_task_repository_resource_id: 2,
      goal_work_item_owned?: 3,
      valid_uuid?: 1,
      value: 2
    ]

  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Orchestration.Machine
  alias SymmetryControl.Repo
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Workspaces.WorkItem
  alias SymmetryControl.Goals
  alias SymmetryControl.Integrations.ProviderAccess
  alias SymmetryControl.Orchestration.TaskReads
  alias SymmetryControl.Orchestration.Assignment

  @spec claim(Ecto.UUID.t(), map(), keyword()) :: {:ok, Run.t()} | {:error, atom()}
  def claim(run_id, request, opts \\ []) do
    notify? = not Repo.in_transaction?()

    case claim_with_disposition(run_id, request, opts) do
      {:ok, run, :created} ->
        if notify?, do: emit_claimed(run)
        {:ok, run}

      {:ok, run, :replayed} ->
        {:ok, run}

      error ->
        error
    end
  end

  @doc false
  @spec claim_owner(Ecto.UUID.t()) :: {:ok, :goal | :legacy} | {:error, atom()}
  def claim_owner(run_id) when is_binary(run_id) do
    if valid_uuid?(run_id) do
      case Repo.one(
             from run in Run,
               join: task in Task,
               on: task.id == run.task_id,
               where: run.id == ^run_id,
               select: %{goal_id: task.goal_id}
           ) do
        %{goal_id: nil} -> {:ok, :legacy}
        %{goal_id: goal_id} when is_binary(goal_id) -> {:ok, :goal}
        nil -> {:error, :not_found}
      end
    else
      {:error, :invalid_request}
    end
  end

  def claim_owner(_), do: {:error, :invalid_request}

  @doc false
  @spec claim_with_disposition(Ecto.UUID.t(), map(), keyword()) ::
          {:ok, Run.t(), :created | :replayed} | {:error, atom()}
  def claim_with_disposition(run_id, request, opts) when is_map(request) do
    with true <- valid_uuid?(run_id) and valid_claim_request?(request),
         {:ok, _lease_duration_ms} <- lease_duration_ms(opts),
         {:ok, :legacy} <- claim_owner(run_id) do
      result =
        Repo.transaction(fn ->
          case prepare_claim_in_transaction(run_id, request, opts, nil) do
            {:replayed, %{run: run}} ->
              run

            {:new, %{task: %Task{goal_id: nil}} = claim_context} ->
              commit_new_claim_in_transaction(claim_context, opts)

            {:new, _claim_context} ->
              rollback(:goal_authority_required)
          end
        end)
        |> case do
          {:ok, {:created, run}} -> {:created, run}
          {:ok, run} -> {:replayed, run}
          error -> error
        end

      case result do
        {:created, run} ->
          {:ok, run, :created}

        {:replayed, run} ->
          {:ok, run, :replayed}

        error ->
          error
      end
    else
      {:ok, :goal} -> {:error, :goal_authority_required}
      {:error, reason} -> {:error, reason}
      :error -> {:error, :invalid_request}
      false -> {:error, :invalid_request}
    end
  end

  def claim_with_disposition(_, _, _), do: {:error, :invalid_request}

  @doc false
  @spec prepare_claim_in_transaction(Ecto.UUID.t(), map(), keyword(), WorkItem.t() | nil) ::
          {:replayed, map()} | {:new, map()} | {:error, atom()}
  def prepare_claim_in_transaction(run_id, request, opts, work_item)
      when is_binary(run_id) and is_map(request) and is_list(opts) do
    if Repo.in_transaction?() and valid_uuid?(run_id) and valid_claim_request?(request) and
         (is_nil(work_item) or match?(%WorkItem{}, work_item)) do
      current = now(opts)

      case prepare_claim_locked(run_id, request, current, work_item, opts) do
        {:replayed, claim_context} ->
          {:replayed, claim_context}

        {:new, claim_context} ->
          {:new, Map.put(claim_context, :current, current)}
      end
    else
      {:error, :invalid_request}
    end
  end

  def prepare_claim_in_transaction(_, _, _, _), do: {:error, :invalid_request}

  @doc false
  @spec commit_new_claim_in_transaction(map(), keyword()) ::
          {:created, Run.t()} | {:error, atom()}
  def commit_new_claim_in_transaction(claim_context, opts)
      when is_map(claim_context) and is_list(opts) do
    with true <- Repo.in_transaction?(),
         %DateTime{} = current <- Map.get(claim_context, :current),
         {:ok, lease_duration_ms} <- lease_duration_ms(opts) do
      commit_new_claim!(claim_context, current, lease_duration_ms)
    else
      _ -> {:error, :invalid_request}
    end
  end

  def commit_new_claim_in_transaction(_, _), do: {:error, :invalid_request}

  def prepare_claim_locked(run_id, request, current, work_item, opts) do
    {task, run, runtime} = lock_chain(run_id, opts)

    claim_context = %{
      task: task,
      run: run,
      runtime: runtime,
      work_item: work_item,
      request_runtime_id: value(request, :runtime_id),
      request_epoch: value(request, :runtime_epoch),
      request_generation: value(request, :generation),
      request_claim_id: value(request, :claim_id)
    }

    if replayed_claim?(task, run, runtime, request, current) do
      {:replayed, claim_context}
    else
      ensure_requested_session_claim_binding!(task, runtime, work_item, run)
      {:new, claim_context}
    end
  end

  def commit_new_claim!(
        %{
          task: task,
          run: run,
          runtime: runtime,
          work_item: work_item,
          request_runtime_id: request_runtime_id,
          request_epoch: request_epoch,
          request_generation: request_generation,
          request_claim_id: request_claim_id
        },
        current,
        lease_duration_ms
      ) do
    cond do
      not is_nil(task.goal_id) and not Assignment.goal_runtime_matches?(runtime, task, work_item) ->
        rollback(:ownership_lost)

      is_nil(task.goal_id) and runtime.harness_kind in ["omp", "omo"] ->
        rollback(:ownership_lost)

      run.runtime_id != request_runtime_id or runtime.id != request_runtime_id ->
        rollback(:ownership_lost)

      runtime.connection_epoch != request_epoch or
        task.current_generation != request_generation or
          run.generation != request_generation ->
        rollback(:ownership_lost)

      not Assignment.runtime_matches_task?(runtime, task) ->
        rollback(:ownership_lost)

      run.state != "assigned" ->
        rollback(:ownership_lost)

      DateTime.compare(run.assignment_expires_at, current) != :gt ->
        rollback(:assignment_expired)

      task.state != "assigned" ->
        rollback(:ownership_lost)

      true ->
        lease_expires_at = DateTime.add(current, lease_duration_ms, :millisecond)

        run =
          run
          |> Run.changeset(%{
            state: "claimed",
            claimed_runtime_epoch: request_epoch,
            claim_id: request_claim_id,
            lease_token: Ecto.UUID.generate(),
            claimed_at: current,
            lease_expires_at: lease_expires_at
          })
          |> stamp_update(current)
          |> Repo.update!()

        task
        |> Task.changeset(%{state: "claimed"})
        |> stamp_update(current)
        |> Repo.update!()

        {:created, run}
    end
  end

  @doc false
  @spec emit_claimed(Run.t()) :: :ok
  def emit_claimed(%Run{} = run) do
    emit([:run, :claimed], %{
      task_id: run.task_id,
      run_id: run.id,
      runtime_id: run.runtime_id,
      generation: run.generation
    })
  end

  @spec renew_lease(Ecto.UUID.t(), map(), keyword()) :: {:ok, Run.t()} | {:error, atom()}
  def renew_lease(run_id, fence, opts \\ [])

  def renew_lease(run_id, fence, opts) when is_map(fence) do
    with true <- valid_uuid?(run_id) and valid_fence?(fence),
         {:ok, lease_duration_ms} <- lease_duration_ms(opts) do
      Repo.transaction(fn ->
        {task, run, runtime} = lock_execution_chain(run_id)
        current = now(opts)
        if run.state == "cancelling", do: rollback(:ownership_lost)
        ensure_fence!(task, run, runtime, fence, current)

        candidate_expiry = DateTime.add(current, lease_duration_ms, :millisecond)

        lease_expires_at =
          if DateTime.compare(run.lease_expires_at, candidate_expiry) == :lt,
            do: candidate_expiry,
            else: run.lease_expires_at

        run
        |> Run.changeset(%{lease_expires_at: lease_expires_at})
        |> stamp_update(current)
        |> Repo.update!()
      end)
    else
      _ -> {:error, :invalid_request}
    end
  end

  def renew_lease(_, _, _), do: {:error, :invalid_request}

  @doc false
  def ensure_fence!(task, run, runtime, fence, current) do
    ensure_static_fence!(task, run, runtime, fence)

    valid? =
      run.state in ["claimed", "running", "paused", "waiting_for_input", "cancelling"] and
        not is_nil(run.lease_expires_at) and
        DateTime.compare(run.lease_expires_at, current) == :gt

    unless valid?, do: rollback(:ownership_lost)
  end

  @doc false
  def ensure_static_fence!(task, run, runtime, fence) do
    valid? =
      run.runtime_id == value(fence, :runtime_id) and
        runtime.connection_epoch == value(fence, :runtime_epoch) and
        run.claimed_runtime_epoch == value(fence, :runtime_epoch) and
        run.generation == value(fence, :generation) and
        task.current_generation == value(fence, :generation) and
        run.claim_id == value(fence, :claim_id) and
        run.lease_token == value(fence, :lease_token)

    unless valid?, do: rollback(:ownership_lost)
  end

  @doc false
  def lock_chain(run_id, opts \\ []) do
    task_id =
      Repo.one(from run in Run, where: run.id == ^run_id, select: run.task_id) ||
        rollback(:not_found)

    task = lock_task(task_id)
    notify_claim_task_locked(opts, task)
    run = Repo.one!(from run in Run, where: run.id == ^run_id, lock: "FOR UPDATE")
    unless run.task_id == task.id, do: rollback(:ownership_lost)
    runtime = lock_runtime(run.runtime_id)
    {task, run, runtime}
  end

  def notify_claim_task_locked(opts, task) do
    case Keyword.get(opts, :on_claim_task_locked) do
      hook when is_function(hook, 1) -> hook.(task)
      _ -> :ok
    end
  end

  def lock_execution_chain(run_id) do
    {task, run, runtime, goal, item} = lock_goal_chain(run_id)

    if goal do
      ensure_current_goal_execution!(goal, item, task)
    end

    {task, run, runtime}
  end

  # Read ownership without a lock, then take the durable locks in the one ordering shared by
  # Goal execution paths, so a lifecycle command cannot race a legacy Task -> Run acquisition.
  @doc false
  def lock_goal_chain(run_id) do
    ownership =
      Repo.one(
        from run in Run,
          join: task in Task,
          on: task.id == run.task_id,
          where: run.id == ^run_id,
          select: %{task_id: task.id, goal_id: task.goal_id, work_item_id: task.work_item_id}
      ) || rollback(:not_found)

    case ownership.goal_id do
      nil ->
        {task, run, runtime} = lock_chain(run_id)
        {task, run, runtime, nil, nil}

      goal_id ->
        goal = lock_goal(goal_id)
        item = if ownership.work_item_id, do: lock_work_item(ownership.work_item_id)
        task = lock_task(ownership.task_id)
        run = Repo.one!(from run in Run, where: run.id == ^run_id, lock: "FOR UPDATE")
        runtime = lock_runtime(run.runtime_id)

        ensure_goal_work_item_ownership!(goal, item, task)
        {task, run, runtime, goal, item}
    end
  end

  def ensure_goal_work_item_ownership!(goal, item, task) do
    unless goal_work_item_owned?(goal, item, task),
      do: rollback(:ownership_lost)
  end

  @doc false
  def ensure_current_goal_execution!(goal, item, task) do
    ensure_goal_work_item_ownership!(goal, item, task)
    unless goal_task_current?(goal, task), do: rollback(:ownership_lost)
  end

  @doc false
  def ensure_current_goal_execution_if_present!(nil, _item, _task), do: :ok

  def ensure_current_goal_execution_if_present!(goal, item, task) do
    ensure_current_goal_execution!(goal, item, task)
  end

  def ensure_requested_session_claim_binding!(
        %Task{requested_session_id: nil},
        _runtime,
        _item,
        _run
      ),
      do: :ok

  def ensure_requested_session_claim_binding!(task, runtime, item, run) do
    repository_resource_id =
      goal_task_repository_resource_id(task, item) || rollback(:ownership_lost)

    session =
      Repo.one(
        from session in HarnessSession,
          where:
            session.id == ^task.requested_session_id and session.runtime_id == ^runtime.id and
              session.machine_id == ^runtime.machine_id and session.state == "busy" and
              session.binding_verified == true and
              session.active_run_id == ^run.id and
              session.repository_resource_id == ^repository_resource_id and
              session.harness_kind == ^runtime.harness_kind and
              session.harness_version == ^runtime.harness_version and
              session.adapter_version == ^runtime.adapter_version,
          lock: "FOR UPDATE"
      )

    unless (session && run.harness_session_id == session.id) and
             run.harness_binding_id == session.binding_id,
           do: rollback(:ownership_lost)
  end

  def replayed_claim?(task, run, runtime, request, current) do
    run.runtime_id == value(request, :runtime_id) and runtime.id == value(request, :runtime_id) and
      runtime.connection_epoch == value(request, :runtime_epoch) and
      task.current_generation == value(request, :generation) and
      run.generation == value(request, :generation) and
      run.state in ["claimed", "cancelling"] and run.claim_id == value(request, :claim_id) and
      run.claimed_runtime_epoch == value(request, :runtime_epoch) and
      not is_nil(run.lease_expires_at) and
      DateTime.compare(run.lease_expires_at, current) == :gt
  end

  def lock_goal(goal_id),
    do:
      Repo.one(from goal in Goal, where: goal.id == ^goal_id, lock: "FOR UPDATE") ||
        rollback(:not_found)

  @doc false
  def lock_work_item(work_item_id),
    do:
      Repo.one(from item in WorkItem, where: item.id == ^work_item_id, lock: "FOR UPDATE") ||
        rollback(:not_found)

  @doc false
  def lock_machine(machine_id),
    do:
      Repo.one(from machine in Machine, where: machine.id == ^machine_id, lock: "FOR UPDATE") ||
        rollback(:not_found)

  @doc false
  def lock_task(task_id),
    do:
      Repo.one(from task in Task, where: task.id == ^task_id, lock: "FOR UPDATE") ||
        rollback(:not_found)

  @doc false
  def lock_current_run(task),
    do:
      Repo.one(
        from run in Run,
          where: run.task_id == ^task.id and run.generation == ^task.current_generation,
          lock: "FOR UPDATE"
      ) || rollback(:not_found)

  @doc false
  def lock_runtime(runtime_id),
    do:
      Repo.one(from runtime in Runtime, where: runtime.id == ^runtime_id, lock: "FOR UPDATE") ||
        rollback(:not_found)

  @doc false
  def persist_insert(changeset) do
    case Repo.insert(changeset) do
      {:ok, record} -> record
      {:error, _changeset} -> rollback(:invalid_request)
    end
  end

  @doc false
  def persist_update(changeset) do
    case Repo.update(changeset) do
      {:ok, record} -> record
      {:error, _changeset} -> rollback(:invalid_request)
    end
  end

  @doc false
  def insert_ignoring_conflict(schema, changeset, conflict_target) do
    if changeset.valid? do
      row = Map.put(changeset.changes, :id, Ecto.UUID.generate())

      opts =
        if conflict_target == nil,
          do: [on_conflict: :nothing],
          else: [on_conflict: :nothing, conflict_target: conflict_target]

      case Repo.insert_all(schema, [row], opts) do
        {1, _} -> :inserted
        {0, _} -> :conflict
      end
    else
      :invalid
    end
  end

  @doc false
  def rollback(reason), do: Repo.rollback(reason)

  @doc false
  def now(opts),
    do: opts |> Keyword.get(:now, DateTime.utc_now()) |> DateTime.truncate(:microsecond)

  def lease_duration_ms(opts) do
    duration =
      Keyword.get_lazy(opts, :lease_duration_ms, fn ->
        Application.fetch_env!(:symmetry_control, :orchestration)
        |> Keyword.fetch!(:lease_duration_ms)
      end)

    if is_integer(duration) and duration >= @minimum_lease_duration_ms,
      do: {:ok, duration},
      else: :error
  end

  @doc false
  def stamp_insert(changeset, current),
    do: Ecto.Changeset.change(changeset, inserted_at: current, updated_at: current)

  @doc false
  def stamp_update(changeset, current), do: Ecto.Changeset.change(changeset, updated_at: current)

  def valid_claim_request?(request) do
    valid_uuid?(value(request, :runtime_id)) and
      is_integer(value(request, :runtime_epoch)) and value(request, :runtime_epoch) > 0 and
      is_integer(value(request, :generation)) and value(request, :generation) > 0 and
      valid_uuid?(value(request, :claim_id))
  end

  @doc false
  def valid_fence?(fence) do
    valid_uuid?(value(fence, :runtime_id)) and
      is_integer(value(fence, :runtime_epoch)) and value(fence, :runtime_epoch) > 0 and
      is_integer(value(fence, :generation)) and value(fence, :generation) > 0 and
      valid_uuid?(value(fence, :claim_id)) and valid_uuid?(value(fence, :lease_token))
  end

  @doc false
  def emit(event, metadata),
    do: :telemetry.execute([:symmetry_control, :orchestration | event], %{count: 1}, metadata)

  @doc false
  @spec claim_run(Ecto.UUID.t(), map(), keyword()) ::
          {:ok, {Run.t(), Task.t(), map()}} | {:error, atom()}
  def claim_run(run_id, request, opts \\ [])

  def claim_run(run_id, request, opts) when is_map(request) and is_list(opts) do
    with {:ok, owner} <- claim_owner(run_id) do
      owner |> claim_in_transaction(run_id, request, opts) |> claim_run_result()
    end
  end

  def claim_run(_, _, _), do: {:error, :invalid_request}

  defp claim_in_transaction(owner, run_id, request, opts) do
    Repo.transaction(fn ->
      # Scope locking precedes a new claim to preserve the established
      # Goal -> resource lock order. A replay ignores a now-stale scope result.
      provider_scope = ProviderAccess.lock_claim_scope(run_id)

      with {:ok, run, disposition} <- claim_with_owner(owner, run_id, request, opts),
           {:ok, %{task: task}} <- TaskReads.task_snapshot(run.task_id),
           {:ok, provider_access} <-
             provider_access_for_claim(provider_scope, disposition, run, task) do
        {run, task, provider_access, disposition}
      else
        {:error, reason} -> Repo.rollback(reason)
      end
    end)
  end

  defp claim_with_owner(:goal, run_id, request, opts),
    do: Goals.claim_with_disposition(run_id, request, opts)

  defp claim_with_owner(:legacy, run_id, request, opts),
    do: claim_with_disposition(run_id, request, opts)

  defp claim_run_result({:ok, {run, task, provider_access, :created}}) do
    emit_claimed(run)
    {:ok, {run, task, provider_access}}
  end

  defp claim_run_result({:ok, {run, task, provider_access, :replayed}}),
    do: {:ok, {run, task, provider_access}}

  defp claim_run_result(error), do: error

  defp provider_access_for_claim(_scope, :replayed, run, task),
    do: ProviderAccess.replay_claim_access(run, task)

  defp provider_access_for_claim({:ok, scope}, :created, run, task) do
    with {:ok, provider_access} <- ProviderAccess.issue(scope, run, task),
         {:ok, _run} <- ProviderAccess.persist_claim_access(run, provider_access) do
      {:ok, provider_access}
    end
  end

  defp provider_access_for_claim({:error, reason}, :created, _run, _task), do: {:error, reason}
end
