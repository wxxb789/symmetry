defmodule SymmetryControl.Goals.Eligibility do
  @moduledoc false

  @goal_assignment_scan_page_size 32
  @sha256 ~r/\Asha256:[0-9a-f]{64}\z/
  @validation_profile_name ~r/\A[A-Za-z0-9][A-Za-z0-9._:-]{0,127}\z/

  import Ecto.Query

  alias SymmetryControl.Goals.ContextSnapshot
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Workspaces.ProjectResource
  alias SymmetryControl.Repo
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Workspaces.WorkItem

  @doc false
  def candidate_task_query(current, cursor) do
    query =
      from task in Task,
        join: goal in Goal,
        on: goal.id == task.goal_id,
        left_join: item in WorkItem,
        on: item.id == task.work_item_id,
        where: task.state == "queued" and not is_nil(task.goal_id),
        where: task.inserted_at <= ^current,
        where:
          task.goal_revision == goal.current_revision and
            ((goal.state == "draft" and task.purpose == "plan" and is_nil(task.work_item_id)) or
               (goal.state == "active" and task.purpose != "plan" and
                  not is_nil(task.work_item_id))),
        where: task.attempt_generation <= task.max_run_attempts,
        order_by: [asc: task.inserted_at, asc: task.id],
        limit: ^@goal_assignment_scan_page_size,
        select: %{
          goal_id: task.goal_id,
          task_id: task.id,
          work_item_id: task.work_item_id,
          inserted_at: task.inserted_at
        }

    query =
      case cursor do
        nil ->
          query

        %{inserted_at: inserted_at, task_id: task_id} ->
          where(
            query,
            [task],
            task.inserted_at > ^inserted_at or
              (task.inserted_at == ^inserted_at and task.id > ^task_id)
          )
      end

    query
  end

  @doc false
  def next_goal_task_candidate_page(current, cursor),
    do: Repo.all(candidate_task_query(current, cursor))

  @doc false
  def goal_task_current?(goal, task) do
    task.goal_id == goal.id and task.goal_revision == goal.current_revision and
      ((goal.state == "draft" and planning_task?(task)) or
         (goal.state == "active" and task.purpose != "plan"))
  end

  def planning_task?(%Task{purpose: "plan", work_item_id: nil, validation_of_task_id: nil}),
    do: true

  def planning_task?(_task), do: false

  @doc false
  def goal_task_repository_resource_id(%Task{} = task, nil) do
    if planning_task?(task) do
      task.input
      |> value(:subject)
      |> value(:resource_id)
      |> case do
        resource_id when is_binary(resource_id) ->
          if valid_uuid?(resource_id), do: resource_id, else: nil

        _ ->
          nil
      end
    end
  end

  def goal_task_repository_resource_id(_task, %WorkItem{} = item),
    do: item.repository_resource_id

  def goal_task_repository_resource_id(_task, _item), do: nil

  @doc false
  def goal_work_item_owned?(goal, nil, task) do
    planning_task?(task) and task.goal_id == goal.id
  end

  def goal_work_item_owned?(goal, item, task) do
    task.goal_id == goal.id and task.work_item_id == item.id and item.goal_id == goal.id and
      item.admitted_revision == task.goal_revision
  end

  @doc false
  def goal_task_assignable?(goal, task) do
    goal_task_current?(goal, task) and task.state == "queued" and
      task.current_generation < task.attempt_generation and
      task.attempt_generation <= task.max_run_attempts
  end

  # Validation profile authorization is frozen into the admitted ContextSnapshot.
  # Never consult the live profile registry while assigning or claiming a run.
  @doc false
  def validation_runtime_allowed?(runtime, task) do
    case validation_runtime_ids_for_task(task) do
      {:ok, nil} -> true
      {:ok, runtime_ids} -> runtime.id in runtime_ids
      :invalid -> false
    end
  end

  @doc false
  def validation_runtime_ids_for_task(%Task{purpose: "validate"} = task) do
    snapshot =
      Repo.one(
        from snapshot in ContextSnapshot,
          where:
            snapshot.id == ^task.context_snapshot_id and snapshot.goal_id == ^task.goal_id and
              snapshot.goal_revision == ^task.goal_revision and
              snapshot.work_item_id == ^task.work_item_id
      )

    with %ContextSnapshot{} = snapshot <- snapshot,
         work_contract when is_map(work_contract) <-
           value(snapshot.payload || %{}, :work_contract),
         bindings when is_list(bindings) <-
           value(work_contract, :validation_bindings),
         {:ok, runtime_ids} <- validation_binding_runtime_ids(bindings) do
      {:ok, runtime_ids}
    else
      _ -> :invalid
    end
  end

  def validation_runtime_ids_for_task(_task), do: {:ok, nil}

  def validation_binding_runtime_ids([]), do: {:ok, nil}

  def validation_binding_runtime_ids(bindings),
    do: intersect_validation_binding_runtime_ids(bindings)

  def intersect_validation_binding_runtime_ids(bindings) do
    with true <- Enum.all?(bindings, &valid_validation_binding?/1),
         identities <- Enum.map(bindings, &{value(&1, :profile_name), value(&1, :kind)}),
         true <- length(identities) == MapSet.size(MapSet.new(identities)) do
      bindings
      |> Enum.map(&(value(&1, :allowed_runtime_ids) |> MapSet.new()))
      |> Enum.reduce_while(nil, fn runtime_ids, intersection ->
        intersection =
          if is_nil(intersection),
            do: runtime_ids,
            else: MapSet.intersection(intersection, runtime_ids)

        if MapSet.size(intersection) == 0,
          do: {:halt, :invalid},
          else: {:cont, intersection}
      end)
      |> case do
        :invalid -> :invalid
        nil -> :invalid
        runtime_ids -> {:ok, MapSet.to_list(runtime_ids)}
      end
    else
      _ -> :invalid
    end
  end

  def valid_validation_binding?(binding) when is_map(binding) do
    runtime_ids = value(binding, :allowed_runtime_ids)

    exact_map_keys?(binding, ["profile_name", "kind", "profile_digest", "allowed_runtime_ids"]) and
      is_binary(value(binding, :profile_name)) and
      Regex.match?(@validation_profile_name, value(binding, :profile_name)) and
      value(binding, :kind) in ["check", "review"] and
      is_binary(value(binding, :profile_digest)) and
      Regex.match?(@sha256, value(binding, :profile_digest)) and
      is_list(runtime_ids) and runtime_ids != [] and
      Enum.all?(runtime_ids, &canonical_runtime_uuid?/1) and
      length(runtime_ids) == MapSet.size(MapSet.new(runtime_ids))
  end

  def valid_validation_binding?(_binding), do: false

  def canonical_runtime_uuid?(value) when is_binary(value) do
    match?({:ok, ^value}, Ecto.UUID.cast(value))
  end

  def canonical_runtime_uuid?(_value), do: false

  @doc false
  def goal_subject_matches_resource?(%Task{} = task, repository_resource_id)
      when is_binary(repository_resource_id) do
    subject = value(task.input || %{}, :subject)
    is_map(subject) and value(subject, :resource_id) == repository_resource_id
  end

  def goal_subject_matches_resource?(_task, _repository_resource_id), do: false

  @doc false
  def runtime_matches_goal_resource?(runtime, goal_id, repository_resource_id)
      when is_binary(goal_id) and is_binary(repository_resource_id) do
    runtime.repository_resource_id == repository_resource_id and
      Repo.exists?(
        from repository in ProjectResource,
          join: goal in Goal,
          on: goal.project_id == repository.project_id,
          where:
            repository.id == ^repository_resource_id and repository.kind == "repository" and
              goal.id == ^goal_id
      )
  end

  def runtime_matches_goal_resource?(_runtime, _goal_id, _repository_resource_id), do: false

  @doc false
  def runtime_allowed_for_task?(runtime, task) do
    case task_allowed_runtime_ids(task) do
      {:ok, []} -> true
      {:ok, allowed_runtime_ids} -> runtime.id in allowed_runtime_ids
      :invalid -> false
    end
  end

  @doc false
  def task_allowed_runtime_ids(%Task{goal_id: goal_id, allowed_runtime_ids: runtime_ids})
      when not is_nil(goal_id) do
    cond do
      not is_list(runtime_ids) ->
        :invalid

      length(runtime_ids) > 256 ->
        :invalid

      length(runtime_ids) != length(Enum.uniq(runtime_ids)) ->
        :invalid

      Enum.all?(runtime_ids, &canonical_runtime_uuid?/1) ->
        {:ok, runtime_ids}

      true ->
        :invalid
    end
  end

  def task_allowed_runtime_ids(_task), do: {:ok, nil}

  @doc false
  def handoff_source_machine_matches?(_runtime, %Task{handoff_source_run_id: nil}), do: true

  def handoff_source_machine_matches?(runtime, %Task{} = task) do
    Repo.exists?(
      from(source_run in Run,
        join: source_runtime in Runtime,
        on: source_runtime.id == source_run.runtime_id,
        where:
          source_run.id == ^task.handoff_source_run_id and
            source_runtime.machine_id == ^runtime.machine_id
      )
    )
  end

  @doc false
  def exact_map_keys?(map, expected_keys) do
    normalized_keys =
      map
      |> Map.keys()
      |> Enum.map(fn
        key when is_binary(key) -> key
        key when is_atom(key) -> Atom.to_string(key)
        _key -> nil
      end)

    Enum.sort(normalized_keys) == Enum.sort(expected_keys)
  end

  @doc false
  def valid_uuid?(value) when is_binary(value), do: match?({:ok, _}, Ecto.UUID.cast(value))
  def valid_uuid?(_), do: false

  @doc false
  def value(map, key, default \\ nil)

  def value(map, key, default) when is_map(map),
    do: Map.get(map, key, Map.get(map, Atom.to_string(key), default))

  def value(_, _key, default), do: default
end
