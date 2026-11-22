defmodule SymmetryControl.Orchestration.Assignment do
  @moduledoc false

  @capacity_bearing_states [
    "assigned",
    "claimed",
    "running",
    "paused",
    "waiting_for_input",
    "cancelling"
  ]
  import Ecto.Query

  import SymmetryControl.Goals.Eligibility,
    only: [
      goal_subject_matches_resource?: 2,
      goal_task_assignable?: 2,
      goal_task_repository_resource_id: 2,
      goal_work_item_owned?: 3,
      handoff_source_machine_matches?: 2,
      next_goal_task_candidate_page: 2,
      runtime_allowed_for_task?: 2,
      runtime_matches_goal_resource?: 3,
      task_allowed_runtime_ids: 1,
      valid_uuid?: 1,
      validation_runtime_allowed?: 2,
      validation_runtime_ids_for_task: 1,
      value: 2,
      value: 3
    ]

  import SymmetryControl.Orchestration.Claims,
    only: [
      emit: 2,
      now: 1,
      rollback: 1,
      stamp_insert: 2,
      stamp_update: 2
    ]

  import SymmetryControl.Orchestration.Registry,
    only: [
      adapter_matches_runtime_metadata?: 2,
      legacy_supervisory_runtime?: 1,
      native_harness_kinds: 0,
      valid_adapter_operations?: 1,
      valid_runtime_metadata?: 1
    ]

  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Workspaces.Project
  alias SymmetryControl.Repo
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Workspaces.WorkItem

  @spec assignment_target(Run.t()) ::
          {:ok, %{machine_id: Ecto.UUID.t(), runtime_id: Ecto.UUID.t()}} | {:error, :not_found}
  def assignment_target(%Run{id: run_id}), do: assignment_target(run_id)

  def assignment_target(run_id) when is_binary(run_id) do
    if valid_uuid?(run_id) do
      case Repo.one(
             from run in Run,
               join: runtime in Runtime,
               on: runtime.id == run.runtime_id,
               where: run.id == ^run_id,
               select: %{machine_id: runtime.machine_id, runtime_id: run.runtime_id}
           ) do
        nil -> {:error, :not_found}
        target -> {:ok, target}
      end
    else
      {:error, :not_found}
    end
  end

  def assignment_target(_), do: {:error, :not_found}

  @spec assign_one(keyword()) :: {:ok, Run.t()} | {:error, :no_assignment}
  def assign_one(opts \\ []) do
    current = now(opts)
    assignment_duration_ms = Keyword.get(opts, :assignment_duration_ms, 30_000)

    result =
      case assign_one_goal(current, assignment_duration_ms, opts) do
        {:error, :no_assignment} -> assign_one_legacy(current, assignment_duration_ms, opts)
        result -> result
      end

    case result do
      {:ok, run} ->
        emit([:run, :assigned], %{
          task_id: run.task_id,
          run_id: run.id,
          runtime_id: run.runtime_id,
          generation: run.generation
        })

        result

      _ ->
        result
    end
  end

  # Goal candidates are scanned by a stable keyset, one transaction per candidate, so an
  # unavailable or malformed frozen validation binding cannot retain locks or starve a later task.
  defp assign_one_goal(current, assignment_duration_ms, opts),
    do: assign_one_goal(current, assignment_duration_ms, opts, nil)

  defp assign_one_goal(current, assignment_duration_ms, opts, cursor) do
    case next_goal_task_candidate_page(current, cursor) do
      [] ->
        {:error, :no_assignment}

      candidates ->
        case assign_goal_candidate_page(candidates, current, assignment_duration_ms, opts) do
          {:ok, run} -> {:ok, run}
          :skip -> assign_one_goal(current, assignment_duration_ms, opts, List.last(candidates))
          error -> error
        end
    end
  end

  defp assign_goal_candidate_page([], _current, _assignment_duration_ms, _opts), do: :skip

  defp assign_goal_candidate_page([candidate | remaining], current, assignment_duration_ms, opts) do
    case try_assign_goal_candidate(candidate, current, assignment_duration_ms, opts) do
      {:ok, run} -> {:ok, run}
      :skip -> assign_goal_candidate_page(remaining, current, assignment_duration_ms, opts)
      error -> error
    end
  end

  defp try_assign_goal_candidate(candidate, _current, assignment_duration_ms, opts) do
    Repo.transaction(fn ->
      with {:ok, _project, goal, item, task} <-
             try_lock_goal_assignment_chain(candidate),
           current <- now(opts),
           true <- goal_task_assignable?(goal, task) and goal_work_item_owned?(goal, item, task),
           %Runtime{} = runtime <- next_goal_runtime(task, item, current),
           true <- runtime_has_capacity?(runtime) do
        {:assigned, assign_task_to_runtime!(task, runtime, item, current, assignment_duration_ms)}
      else
        _ -> :skip
      end
    end)
    |> case do
      {:ok, {:assigned, run}} -> {:ok, run}
      {:ok, :skip} -> :skip
      {:error, reason} when reason in [:no_assignment, :ownership_lost, :stale_revision] -> :skip
      {:error, reason} -> {:error, reason}
    end
  end

  defp try_lock_goal_assignment_chain(candidate) do
    with %Project{} = project <- try_lock_goal_project(candidate.goal_id),
         true <- project.status == "active",
         %Goal{} = goal <- try_lock_goal(candidate.goal_id),
         item <- try_lock_goal_task_item(candidate.work_item_id),
         true <- is_nil(item) or match?(%WorkItem{}, item),
         %Task{} = task <- try_lock_task(candidate.task_id) do
      {:ok, project, goal, item, task}
    else
      _ -> :skip
    end
  end

  defp try_lock_goal_project(goal_id) do
    with project_id when is_binary(project_id) <-
           Repo.one(from(goal in Goal, where: goal.id == ^goal_id, select: goal.project_id)) do
      Repo.one(
        from(project in Project,
          where: project.id == ^project_id,
          lock: "FOR SHARE SKIP LOCKED"
        )
      )
    end
  end

  defp try_lock_goal(goal_id),
    do: Repo.one(from goal in Goal, where: goal.id == ^goal_id, lock: "FOR UPDATE SKIP LOCKED")

  defp try_lock_work_item(work_item_id),
    do:
      Repo.one(
        from item in WorkItem, where: item.id == ^work_item_id, lock: "FOR UPDATE SKIP LOCKED"
      )

  defp try_lock_goal_task_item(nil), do: nil
  defp try_lock_goal_task_item(work_item_id), do: try_lock_work_item(work_item_id)

  defp try_lock_task(task_id),
    do: Repo.one(from task in Task, where: task.id == ^task_id, lock: "FOR UPDATE SKIP LOCKED")

  defp assign_one_legacy(current, assignment_duration_ms, opts) do
    Repo.transaction(fn ->
      case next_assignable_legacy_task_and_runtime(current) do
        nil ->
          rollback(:no_assignment)

        {task, runtime} ->
          current = now(opts)

          if runtime_available_at?(runtime, current) do
            assign_task_to_runtime!(task, runtime, nil, current, assignment_duration_ms)
          else
            rollback(:no_assignment)
          end
      end
    end)
  end

  defp assign_task_to_runtime!(task, runtime, item, current, assignment_duration_ms) do
    generation = task.attempt_generation

    task =
      task
      |> Task.changeset(%{
        state: "assigned",
        current_generation: generation,
        waiting_transition_id: nil
      })
      |> stamp_update(current)
      |> Repo.update!()

    run =
      %Run{}
      |> Run.changeset(%{
        task_id: task.id,
        runtime_id: runtime.id,
        generation: generation,
        state: "assigned",
        assigned_at: current,
        assignment_expires_at: DateTime.add(current, assignment_duration_ms, :millisecond)
      })
      |> stamp_insert(current)
      |> Repo.insert!()

    reserve_requested_session!(task, runtime, item, run, current)
  end

  def runtime_has_capacity?(runtime) do
    Repo.aggregate(
      from(run in Run,
        where: run.runtime_id == ^runtime.id and run.state in ^@capacity_bearing_states
      ),
      :count
    ) < runtime.capacity
  end

  defp runtime_available_at?(runtime, current) do
    runtime.status == "online" and
      not is_nil(runtime.last_heartbeat_at) and
      DateTime.compare(
        runtime.last_heartbeat_at,
        DateTime.add(current, -3 * runtime.heartbeat_interval_ms, :millisecond)
      ) == :gt
  end

  defp next_assignable_legacy_task_and_runtime(current) do
    Repo.one(
      from task in Task,
        join: runtime in Runtime,
        on: runtime.status == "online",
        where: ^runtime_matches_binding(),
        where: task.state == "queued" and is_nil(task.goal_id),
        where:
          fragment(
            "? > CAST(? AS timestamp) - (? * INTERVAL '3 milliseconds')",
            runtime.last_heartbeat_at,
            ^current,
            runtime.heartbeat_interval_ms
          ),
        where:
          fragment(
            "(SELECT count(*) FROM runs AS active_run WHERE active_run.runtime_id = ? AND active_run.state = ANY(CAST(? AS text[]))) < ?",
            runtime.id,
            ^@capacity_bearing_states,
            runtime.capacity
          ),
        order_by: [asc: task.inserted_at, asc: runtime.inserted_at],
        limit: 1,
        lock: "FOR UPDATE SKIP LOCKED",
        select: {task, runtime}
    )
  end

  # The non-transient conditions that make a runtime eligible for a goal-less task; runtime
  # status, heartbeat freshness and free capacity are added by the caller. The retry projection
  # shares exactly these conditions against the binding the retried generation is re-admitted with.
  @doc false
  def runtime_matches_binding do
    dynamic(
      [binding, runtime],
      runtime.agent_profile == binding.agent_profile and
        runtime.workspace == binding.workspace and
        fragment("? @> ?", runtime.capabilities, binding.required_capabilities) and
        (fragment(
           "?->>'supervisory_control' IS DISTINCT FROM 'true'",
           binding.required_capabilities
         ) or
           is_nil(runtime.harness_kind) or runtime.harness_kind == "generic") and
        (is_nil(runtime.harness_kind) or runtime.harness_kind not in ["omp", "omo"])
    )
  end

  defp next_goal_runtime(task, item, current) do
    case {goal_task_repository_resource_id(task, item), task_allowed_runtime_ids(task),
          validation_runtime_ids_for_task(task), native_goal_session_requirement(task)} do
      {nil, _, _, _} ->
        nil

      {_, :invalid, _, _} ->
        nil

      {_, _, :invalid, _} ->
        nil

      {_, _, _, :invalid} ->
        nil

      {repository_resource_id, {:ok, allowed_runtime_ids}, {:ok, validation_runtime_ids},
       session_requirement} ->
        adapter_requirement = native_goal_adapter_requirement()

        strict_budget_requirement = strict_budget_capability_requirement(task)

        query =
          from runtime in Runtime,
            where:
              runtime.status == "online" and runtime.agent_profile == ^task.agent_profile and
                runtime.workspace == ^task.workspace and
                runtime.repository_resource_id == ^repository_resource_id and
                runtime.harness_kind in ^native_harness_kinds(),
            where:
              fragment("? @> ?", runtime.capabilities, type(^task.required_capabilities, :map)),
            where: fragment("? @> ?", runtime.capabilities, type(^adapter_requirement, :map)),
            where: fragment("? @> ?", runtime.capabilities, type(^session_requirement, :map)),
            where:
              fragment("? @> ?", runtime.capabilities, type(^strict_budget_requirement, :map)),
            where:
              fragment(
                "? > CAST(? AS timestamp) - (? * INTERVAL '3 milliseconds')",
                runtime.last_heartbeat_at,
                ^current,
                runtime.heartbeat_interval_ms
              ),
            where:
              fragment(
                "(SELECT count(*) FROM runs AS active_run WHERE active_run.runtime_id = ? AND active_run.state = ANY(CAST(? AS text[]))) < ?",
                runtime.id,
                ^@capacity_bearing_states,
                runtime.capacity
              ),
            order_by: [asc: runtime.inserted_at, asc: runtime.id],
            limit: 1,
            lock: "FOR UPDATE SKIP LOCKED"

        query =
          if allowed_runtime_ids == [],
            do: query,
            else: where(query, [runtime], runtime.id in ^allowed_runtime_ids)

        query =
          if is_nil(validation_runtime_ids),
            do: query,
            else: where(query, [runtime], runtime.id in ^validation_runtime_ids)

        query = restrict_to_requested_session(query, task, item)
        query = restrict_to_handoff_source_machine(query, task)

        case Repo.one(query) do
          nil ->
            nil

          runtime ->
            if goal_runtime_matches?(runtime, task, item), do: runtime, else: nil
        end
    end
  end

  # A retained native session is machine-local and must not fall back to another
  # runtime merely because it matches the task profile.
  defp restrict_to_requested_session(query, %Task{requested_session_id: nil}, _item), do: query

  defp restrict_to_requested_session(query, %Task{} = task, item) do
    repository_resource_id =
      goal_task_repository_resource_id(task, item) || rollback(:ownership_lost)

    retained_runtime =
      from session in HarnessSession,
        join: runtime in Runtime,
        on: runtime.id == session.runtime_id,
        where:
          session.id == ^task.requested_session_id and session.state == "available" and
            session.binding_verified == true and is_nil(session.active_run_id) and
            session.repository_resource_id == ^repository_resource_id and
            session.machine_id == runtime.machine_id and
            session.harness_kind == runtime.harness_kind and
            session.harness_version == runtime.harness_version and
            session.adapter_version == runtime.adapter_version,
        select: session.runtime_id

    from runtime in query,
      where: runtime.id == subquery(retained_runtime)
  end

  # Handoff starts a fresh native session whose verified artifact belongs to the source machine;
  # another machine would turn a local artifact pointer into a cross-machine transfer.
  defp restrict_to_handoff_source_machine(query, %Task{handoff_source_run_id: nil}), do: query

  defp restrict_to_handoff_source_machine(query, %Task{} = task) do
    source_machine_id =
      Repo.one(
        from(source_run in Run,
          join: source_runtime in Runtime,
          on: source_runtime.id == source_run.runtime_id,
          where: source_run.id == ^task.handoff_source_run_id,
          select: source_runtime.machine_id
        )
      )

    if is_binary(source_machine_id),
      do: where(query, [runtime], runtime.machine_id == ^source_machine_id),
      else: where(query, [runtime], false)
  end

  # The scheduler owns each retained attachment identity. It is persisted with
  # the Run before dispatch so a daemon cannot choose or reuse a stale binding.
  defp reserve_requested_session!(
         %Task{requested_session_id: nil},
         _runtime,
         _item,
         run,
         _current
       ),
       do: run

  defp reserve_requested_session!(task, runtime, item, run, current) do
    repository_resource_id =
      goal_task_repository_resource_id(task, item) || rollback(:ownership_lost)

    session =
      Repo.one(
        from session in HarnessSession,
          where:
            session.id == ^task.requested_session_id and session.runtime_id == ^runtime.id and
              session.machine_id == ^runtime.machine_id and session.binding_verified == true and
              session.state == "available" and
              is_nil(session.active_run_id) and
              session.repository_resource_id == ^repository_resource_id and
              session.harness_kind == ^runtime.harness_kind and
              session.harness_version == ^runtime.harness_version and
              session.adapter_version == ^runtime.adapter_version,
          lock: "FOR UPDATE"
      ) || rollback(:no_assignment)

    binding_id = Ecto.UUID.generate()

    session
    |> HarnessSession.update_changeset(%{
      state: "busy",
      active_run_id: run.id,
      binding_id: binding_id
    })
    |> stamp_update(current)
    |> Repo.update!()

    run
    |> Ecto.Changeset.change(harness_session_id: session.id, harness_binding_id: binding_id)
    |> stamp_update(current)
    |> Repo.update!()
  end

  @doc false
  def release_reserved_session!(%Run{harness_session_id: nil}, _current), do: :ok

  def release_reserved_session!(run, current) do
    session =
      Repo.one(
        from session in HarnessSession,
          where: session.id == ^run.harness_session_id,
          lock: "FOR UPDATE"
      )

    if session && session.state == "busy" && session.active_run_id == run.id &&
         session.binding_id == run.harness_binding_id do
      session
      |> HarnessSession.update_changeset(%{state: "available", active_run_id: nil})
      |> stamp_update(current)
      |> Repo.update!()
    end
  end

  @doc false
  def mark_reserved_session_unavailable!(%Run{harness_session_id: nil}, _current), do: :ok

  def mark_reserved_session_unavailable!(run, current) do
    session =
      Repo.one(
        from session in HarnessSession,
          where: session.id == ^run.harness_session_id,
          lock: "FOR UPDATE"
      )

    if session && session.state == "busy" && session.active_run_id == run.id &&
         session.binding_id == run.harness_binding_id do
      session
      |> HarnessSession.update_changeset(%{state: "unavailable", active_run_id: nil})
      |> stamp_update(current)
      |> Repo.update!()
    end
  end

  @doc false
  def goal_runtime_matches?(runtime, task, item) do
    runtime_matches_task?(runtime, task) and
      goal_subject_matches_resource?(task, goal_task_repository_resource_id(task, item)) and
      runtime_matches_goal_resource?(
        runtime,
        task.goal_id,
        goal_task_repository_resource_id(task, item)
      ) and
      runtime_allowed_for_task?(runtime, task) and
      validation_runtime_allowed?(runtime, task) and
      handoff_source_machine_matches?(runtime, task) and
      native_goal_adapter_capable?(runtime, task)
  end

  defp native_goal_adapter_requirement do
    %{
      "adapter" => %{
        "operations" => %{
          "start" => true,
          "events" => true,
          "cancel" => true,
          "pause" => "unsupported"
        }
      }
    }
  end

  defp native_goal_resume_requirement do
    %{
      "adapter" => %{
        "operations" => %{
          "resume" => true
        }
      }
    }
  end

  defp native_goal_handoff_requirement do
    %{
      "adapter" => %{
        "operations" => %{
          "handoff" => true
        }
      }
    }
  end

  defp native_goal_session_requirement(task) do
    case value(task.input || %{}, :session_mode, "fresh") do
      "fresh" -> %{}
      "resume" -> native_goal_resume_requirement()
      "handoff" -> native_goal_handoff_requirement()
      _ -> :invalid
    end
  end

  defp native_goal_adapter_capable?(runtime, task) do
    adapter = value(runtime.capabilities, :adapter)
    operations = value(adapter, :operations)
    session_mode = value(task.input || %{}, :session_mode, "fresh")

    valid_runtime_metadata?(runtime) and is_map(adapter) and
      adapter_matches_runtime_metadata?(adapter, runtime) and
      valid_adapter_operations?(operations) and
      value(operations, :start) == true and value(operations, :events) == true and
      value(operations, :cancel) == true and value(operations, :pause) == "unsupported" and
      value(runtime.capabilities, :supervisory_control, false) != true and
      (not strict_budget_task?(task) or value(operations, :hard_cost_limit) == true) and
      (session_mode != "resume" or value(operations, :resume) == true) and
      (session_mode != "handoff" or value(operations, :handoff) == true)
  end

  defp strict_budget_capability_requirement(task) do
    if strict_budget_task?(task) do
      %{"adapter" => %{"operations" => %{"hard_cost_limit" => true}}}
    else
      %{}
    end
  end

  defp strict_budget_task?(%Task{} = task) do
    task.input
    |> value(:limits, %{})
    |> value(:max_cost_microusd)
    |> is_nil()
    |> Kernel.not()
  end

  defp strict_budget_task?(_task), do: false

  @spec assign_all(keyword()) :: {:ok, [Run.t()]}
  def assign_all(opts \\ []) do
    assign_all([], opts)
  end

  def assign_all(runs, opts) do
    case assign_one(opts) do
      {:ok, run} -> assign_all([run | runs], opts)
      {:error, :no_assignment} -> {:ok, Enum.reverse(runs)}
    end
  end

  @doc false
  def runtime_matches_task?(runtime, task) do
    runtime.status == "online" and runtime.agent_profile == task.agent_profile and
      runtime.workspace == task.workspace and
      (value(task.required_capabilities, :supervisory_control, false) != true or
         legacy_supervisory_runtime?(runtime)) and
      Repo.exists?(
        from candidate in Runtime,
          where: candidate.id == ^runtime.id,
          where:
            fragment(
              "? @> ?",
              candidate.capabilities,
              type(^task.required_capabilities, :map)
            )
      )
  end
end
