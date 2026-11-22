defmodule SymmetryControl.Goals.GoalControl do
  @moduledoc false

  import Ecto.Query

  import SymmetryControl.Goals.Internal,
    only: [
      ensure_project_active!: 1,
      lock_goal!: 1,
      lock_project!: 1,
      lock_run!: 1,
      lock_task!: 1,
      lock_work_item!: 1,
      rollback: 1,
      valid_uuid: 1,
      valid_uuid?: 1,
      value: 2
    ]

  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.GoalEvent
  alias SymmetryControl.Goals.Recovery
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Command
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.RunTransition
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo

  def control_dispatch_plan(goal_id, revision, action_id, opts \\ [])

  def control_dispatch_plan(goal_id, revision, action_id, _opts)
      when is_binary(goal_id) and is_integer(revision) and revision > 0 and is_binary(action_id) do
    with :ok <- valid_uuid(goal_id), :ok <- valid_uuid(action_id) do
      Repo.transaction(fn ->
        project_id =
          Repo.one(from(goal in Goal, where: goal.id == ^goal_id, select: goal.project_id)) ||
            rollback(:not_found)

        project = lock_project!(project_id)
        ensure_project_active!(project)
        goal = lock_goal!(goal_id)

        case lock_current_goal_control_action(goal, revision, action_id, nil) do
          {:ok, event} -> Recovery.control_descriptors(goal, event.kind, action_id)
          :superseded -> :superseded
        end
      end)
      |> case do
        {:ok, :superseded} -> {:ok, :superseded}
        {:ok, descriptors} -> {:ok, descriptors}
        {:error, reason} -> {:error, reason}
      end
    end
  end

  def control_dispatch_plan(_, _, _, _), do: {:error, :invalid_request}

  def dispatch_goal_control_command(
        goal_id,
        revision,
        action_id,
        task_id,
        kind,
        payload,
        idempotency_key,
        opts \\ []
      )

  def dispatch_goal_control_command(
        goal_id,
        revision,
        action_id,
        task_id,
        kind,
        payload,
        idempotency_key,
        opts
      )
      when is_binary(goal_id) and is_integer(revision) and revision > 0 and is_binary(action_id) and
             is_binary(task_id) and is_binary(kind) and is_map(payload) and
             is_binary(idempotency_key) and byte_size(idempotency_key) > 0 and is_list(opts) do
    with :ok <- valid_uuid(goal_id),
         :ok <- valid_uuid(action_id),
         :ok <- valid_uuid(task_id),
         true <- kind in ["pause", "cancel"],
         true <- valid_control_command_payload?(kind, payload),
         true <- idempotency_key == goal_control_idempotency_key(action_id, kind, task_id) do
      Repo.transaction(fn ->
        if goal_control_command_exists?(task_id, idempotency_key) do
          goal = Repo.get(Goal, goal_id) || rollback(:not_found)

          dispatch_authorized_goal_control_command!(
            goal,
            revision,
            action_id,
            task_id,
            kind,
            payload,
            idempotency_key,
            opts
          )
        else
          project_id =
            Repo.one(from(goal in Goal, where: goal.id == ^goal_id, select: goal.project_id)) ||
              rollback(:not_found)

          project = lock_project!(project_id)
          ensure_project_active!(project)
          goal = lock_goal!(goal_id)

          case lock_current_goal_control_action(goal, revision, action_id, kind) do
            :superseded ->
              :superseded

            {:ok, event} ->
              if control_descriptor_authorized?(
                   goal,
                   event,
                   task_id,
                   kind,
                   payload,
                   idempotency_key
                 ) do
                dispatch_authorized_goal_control_command!(
                  goal,
                  event.revision,
                  event.id,
                  task_id,
                  kind,
                  payload,
                  idempotency_key,
                  opts
                )
              else
                :superseded
              end
          end
        end
      end)
      |> case do
        {:ok, :superseded} -> {:ok, :superseded}
        {:ok, {command, disposition}} -> {:ok, command, disposition}
        {:error, reason} -> {:error, reason}
      end
    else
      _ -> {:error, :invalid_request}
    end
  end

  def dispatch_goal_control_command(_, _, _, _, _, _, _, _), do: {:error, :invalid_request}

  defp dispatch_authorized_goal_control_command!(
         goal,
         revision,
         action_id,
         task_id,
         kind,
         payload,
         idempotency_key,
         opts
       ) do
    authorization = %{
      goal_id: goal.id,
      revision: revision,
      action_id: action_id,
      kind: kind
    }

    envelope = %{
      task_id: task_id,
      payload: payload,
      idempotency_key: idempotency_key,
      run_id: Keyword.get(opts, :run_id),
      generation: Keyword.get(opts, :generation)
    }

    case Orchestration.create_goal_control_command_in_transaction(authorization, envelope, opts) do
      {command, disposition, _receipt} ->
        {command, disposition}

      {:error, reason} ->
        rollback(reason)
    end
  end

  def apply_goal_pause_transition(run_id, fence, payload, transition_id, opts \\ [])

  def apply_goal_pause_transition(run_id, fence, payload, transition_id, opts)
      when is_binary(run_id) and is_map(fence) and is_map(payload) and
             is_binary(transition_id) and is_list(opts) do
    if not (valid_uuid?(run_id) and valid_uuid?(transition_id)) do
      {:error, :invalid_request}
    else
      Repo.transaction(fn ->
        ownership =
          Repo.one(
            from(run in Run,
              join: task in Task,
              on: task.id == run.task_id,
              left_join: goal in Goal,
              on: goal.id == task.goal_id,
              where: run.id == ^run_id,
              select: %{
                task_id: task.id,
                goal_id: task.goal_id,
                project_id: goal.project_id,
                work_item_id: task.work_item_id
              }
            )
          ) || rollback(:not_found)

        if is_nil(ownership.goal_id), do: rollback(:goal_authority_required)

        lock_project!(ownership.project_id)
        goal = lock_goal!(ownership.goal_id)
        item = if ownership.work_item_id, do: lock_work_item!(ownership.work_item_id)
        task = lock_task!(ownership.task_id)
        run = lock_run!(run_id)

        ensure_goal_pause_task_ownership!(goal, item, task, run)

        transition_exists? =
          Repo.exists?(
            from(transition in RunTransition,
              where: transition.run_id == ^run.id and transition.transition_id == ^transition_id
            )
          )

        unless transition_exists? do
          command_id = value(payload, :command_id)
          unless valid_uuid?(command_id), do: rollback(:invalid_request)

          command = Repo.get(Command, command_id) || rollback(:state_conflict)

          if command.task_id != task.id or command.kind != "pause",
            do: rollback(:state_conflict)

          action_id = goal_pause_action_id!(command, task.id)

          command =
            Repo.one(
              from(command_row in Command,
                where: command_row.id == ^command.id,
                lock: "FOR UPDATE"
              )
            ) || rollback(:state_conflict)

          if command.run_id != run.id or command.generation != run.generation,
            do: rollback(:ownership_lost)

          if value(payload, :run_id) not in [nil, run.id] or
               value(payload, :generation) not in [nil, run.generation],
             do: rollback(:ownership_lost)

          unless task.goal_revision == goal.current_revision,
            do: rollback(:stale_revision)

          case lock_current_goal_control_action(goal, goal.current_revision, action_id, "pause") do
            {:ok, _event} -> :ok
            :superseded -> rollback(:stale_revision)
          end
        end

        case Orchestration.apply_goal_pause_transition_in_transaction(
               task,
               run,
               fence,
               payload,
               transition_id,
               opts
             ) do
          {:created, {updated_run, receipt}} -> {:created, {updated_run, receipt}}
          {:replayed, updated_run} -> {:replayed, updated_run}
          {:error, reason} -> rollback(reason)
        end
      end)
      |> case do
        {:ok, {:created, {run, receipt}}} -> {:ok, run, :created, receipt}
        {:ok, {:replayed, run}} -> {:ok, run, :replayed}
        {:error, reason} -> {:error, reason}
      end
    end
  end

  def apply_goal_pause_transition(_, _, _, _, _), do: {:error, :invalid_request}

  defp lock_current_goal_control_action(goal, revision, action_id, command_kind) do
    action =
      Repo.one(
        from(event in GoalEvent,
          where: event.id == ^action_id and event.goal_id == ^goal.id,
          lock: "FOR UPDATE"
        )
      )

    latest_lifecycle_action =
      Repo.one(
        from(event in GoalEvent,
          where:
            event.goal_id == ^goal.id and event.kind in ["pause", "resume", "cancel", "amend"],
          order_by: [desc: event.sequence],
          limit: 1,
          lock: "FOR UPDATE"
        )
      )

    current_lifecycle? =
      case {action && action.kind, command_kind, goal.state} do
        {"pause", nil, "paused"} -> true
        {"cancel", nil, "cancelled"} -> true
        {"amend", nil, "paused"} -> true
        {"pause", "pause", "paused"} -> true
        {"cancel", "cancel", "cancelled"} -> true
        {"amend", kind, "paused"} when kind in ["pause", "cancel"] -> true
        _ -> false
      end

    if not is_nil(action) and not is_nil(latest_lifecycle_action) and
         action.revision == revision and goal.current_revision == revision and
         latest_lifecycle_action.id == action_id and
         action.kind in ["pause", "cancel", "amend"] and current_lifecycle? do
      {:ok, action}
    else
      :superseded
    end
  end

  defp control_descriptor_authorized?(goal, event, task_id, kind, payload, idempotency_key) do
    descriptor = %{
      task_id: task_id,
      kind: kind,
      payload: payload,
      idempotency_key: idempotency_key
    }

    Enum.any?(Recovery.control_descriptors(goal, event.kind, event.id), &(&1 == descriptor)) or
      Repo.exists?(
        from(command in Command,
          where:
            command.task_id == ^task_id and command.kind == ^kind and
              command.idempotency_key == ^idempotency_key
        )
      )
  end

  defp valid_control_command_payload?(kind, payload),
    do: kind in ["pause", "cancel"] and payload == %{}

  defp goal_control_command_exists?(task_id, idempotency_key) do
    Repo.exists?(
      from(command in Command,
        where: command.task_id == ^task_id and command.idempotency_key == ^idempotency_key
      )
    )
  end

  defp goal_pause_action_id!(%Command{idempotency_key: idempotency_key}, task_id)
       when is_binary(idempotency_key) do
    case String.split(idempotency_key, ":", parts: 4) do
      ["goal-control", action_id, "pause", ^task_id] ->
        if valid_uuid?(action_id), do: action_id, else: rollback(:goal_authority_required)

      _ ->
        rollback(:goal_authority_required)
    end
  end

  defp goal_pause_action_id!(_command, _task_id), do: rollback(:goal_authority_required)

  defp ensure_goal_pause_task_ownership!(goal, item, task, run) do
    owned? =
      task.goal_id == goal.id and
        run.task_id == task.id and
        goal_work_item_owned?(goal, item, task) and
        task.current_generation == run.generation

    unless owned?, do: rollback(:ownership_lost)
  end

  defp goal_work_item_owned?(goal, nil, %Task{work_item_id: nil} = task),
    do: task.goal_id == goal.id and task.purpose == "plan"

  defp goal_work_item_owned?(goal, item, task) do
    not is_nil(item) and task.work_item_id == item.id and item.goal_id == goal.id and
      task.goal_id == goal.id and item.admitted_revision == task.goal_revision
  end

  defp goal_control_idempotency_key(action_id, kind, task_id),
    do: "goal-control:#{action_id}:#{kind}:#{task_id}"

  @doc false

  def reject_pending_goal_control_commands!(goal_id, current) do
    Repo.update_all(
      from(command in Command,
        join: task in Task,
        on: task.id == command.task_id,
        where:
          task.goal_id == ^goal_id and command.kind in ["pause", "cancel"] and
            command.state == "pending" and like(command.idempotency_key, "goal-control:%")
      ),
      set: [
        state: "acknowledged",
        acknowledgement_outcome: "rejected",
        acknowledged_at: current,
        updated_at: current
      ]
    )
  end
end
