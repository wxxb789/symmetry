defmodule SymmetryControl.Goals.Recovery do
  @moduledoc false

  import Ecto.Query

  import SymmetryControl.Goals.Internal,
    only: [lock_goal!: 1, now: 1, rollback: 1, valid_uuid: 1, valid_uuid?: 1]

  alias Ecto.Query.CastError
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.GoalBudgetReservation
  alias SymmetryControl.Goals.GoalDecision
  alias SymmetryControl.Goals.GoalEvent
  alias SymmetryControl.Goals.WorkOutcome
  alias SymmetryControl.Orchestration.Command
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces.WorkItem

  @nonterminal_task_states [
    "queued",
    "assigned",
    "claimed",
    "running",
    "waiting_for_input",
    "paused",
    "cancelling"
  ]
  @cancellable_task_states [
    "queued",
    "assigned",
    "claimed",
    "running",
    "paused",
    "waiting_for_input"
  ]
  @terminal_run_states ["completed", "failed", "cancelled"]
  @terminal_settlement_grace_ms 8 * 60 * 1_000

  def recover_pending_unstarted_cancellations(goal_id) when is_binary(goal_id) do
    if valid_uuid(goal_id) == :ok do
      fn ->
        goal = lock_goal!(goal_id)

        task_ids =
          Repo.all(
            from(task in Task,
              where: task.goal_id == ^goal.id and task.state == "cancelled",
              order_by: [asc: task.id],
              lock: "FOR UPDATE",
              select: task.id
            )
          )

        Enum.filter(task_ids, &recoverable_historical_unstarted_cancellation?(goal, &1))
      end
      |> Repo.transaction()
      |> case do
        {:ok, task_ids} -> {:ok, task_ids}
        {:error, reason} -> {:error, reason}
      end
    else
      {:error, :invalid_request}
    end
  end

  def recover_pending_unstarted_cancellations(_), do: {:error, :invalid_request}

  def recoverable_unstarted_cancellation_goal_ids(opts \\ [])

  def recoverable_unstarted_cancellation_goal_ids(opts) when is_list(opts) do
    requested_goal_ids = Keyword.get(opts, :goal_ids)
    limit = Keyword.get(opts, :limit, 100)

    if not (is_integer(limit) and limit > 0) or
         not (is_nil(requested_goal_ids) or
                (is_list(requested_goal_ids) and Enum.all?(requested_goal_ids, &valid_uuid?/1))) do
      {:error, :invalid_request}
    else
      recoverable =
        from(reservation in GoalBudgetReservation,
          join: task in Task,
          on: task.id == reservation.task_id,
          left_join: run in Run,
          on: run.task_id == task.id,
          where:
            reservation.state == "held" and not is_nil(task.goal_id) and task.state == "cancelled" and
              is_nil(run.id),
          order_by: [asc: task.goal_id],
          distinct: true,
          select: task.goal_id,
          limit: ^limit
        )

      recoverable =
        if is_list(requested_goal_ids),
          do:
            where(
              recoverable,
              [reservation, task],
              reservation.goal_id in ^requested_goal_ids and task.goal_id in ^requested_goal_ids
            ),
          else: recoverable

      {:ok, Repo.all(recoverable)}
    end
  rescue
    error in [CastError] -> {:error, error}
  end

  def recoverable_unstarted_cancellation_goal_ids(_), do: {:error, :invalid_request}

  def recover_pending_terminal_settlements(opts \\ [])

  def recover_pending_terminal_settlements(opts) when is_list(opts) do
    current = now(opts)
    requested_goal_ids = Keyword.get(opts, :goal_ids)
    limit = Keyword.get(opts, :limit, 100)

    if not (is_integer(limit) and limit > 0) or
         not (is_nil(requested_goal_ids) or
                (is_list(requested_goal_ids) and Enum.all?(requested_goal_ids, &valid_uuid?/1))) do
      {:error, :invalid_request}
    else
      unsettled =
        from(reservation in GoalBudgetReservation,
          join: task in Task,
          as: :task,
          on: task.id == reservation.task_id,
          join: run in Run,
          as: :run,
          on: run.task_id == task.id and run.generation == task.current_generation,
          where:
            reservation.state in ["held", "settled", "unknown"] and not is_nil(task.goal_id) and
              task.state in ^@terminal_run_states and run.state in ^@terminal_run_states and
              fragment(
                "COALESCE(? ->> 'reason', '') <> 'lease_expired' OR (? + (? * interval '1 millisecond')) <= ?",
                run.failure,
                run.updated_at,
                ^@terminal_settlement_grace_ms,
                ^current
              ) and
              not exists(
                from(event in GoalEvent,
                  where:
                    event.goal_id == parent_as(:task).goal_id and event.kind == "task_settled" and
                      fragment(
                        "? ->> 'task_id' = CAST(? AS text)",
                        event.payload,
                        parent_as(:task).id
                      ) and
                      fragment(
                        "? ->> 'run_id' = CAST(? AS text)",
                        event.payload,
                        parent_as(:run).id
                      ) and
                      fragment(
                        "? ->> 'generation' = CAST(? AS text)",
                        event.payload,
                        parent_as(:run).generation
                      )
                )
              ),
          order_by: [asc: task.goal_id, asc: task.id, asc: run.id],
          select: %{task_id: task.id, run_id: run.id, generation: run.generation},
          limit: ^limit
        )

      unsettled =
        if is_list(requested_goal_ids),
          do:
            where(
              unsettled,
              [reservation, task],
              reservation.goal_id in ^requested_goal_ids and task.goal_id in ^requested_goal_ids
            ),
          else: unsettled

      unsettled =
        unsettled
        |> Repo.all()
        |> Enum.map(&Map.put(&1, :reconciliation, :settlement))

      awaiting_validation =
        from(task in Task,
          as: :task,
          join: goal in Goal,
          on: goal.id == task.goal_id,
          join: item in WorkItem,
          on: item.id == task.work_item_id,
          join: run in Run,
          as: :run,
          on: run.task_id == task.id and run.generation == task.current_generation,
          join: decision in GoalDecision,
          on:
            decision.goal_id == task.goal_id and decision.goal_revision == goal.current_revision and
              decision.kind == "review" and decision.state == "resolved" and
              decision.work_item_id == item.id,
          where:
            task.purpose == "validate" and task.goal_revision == goal.current_revision and
              task.state == "completed" and run.state == "completed" and
              fragment(
                "EXISTS (SELECT 1 FROM jsonb_array_elements(? -> 'predicates') AS predicate WHERE predicate ->> 'kind' = 'operator_acceptance')",
                item.acceptance_contract
              ) and
              fragment("? ->> 'option_id' IN ('accept', 'reject')", decision.resolution) and
              fragment(
                "encode(?, 'hex') = replace(? #>> '{task_result,subject_hash}', 'sha256:', '')",
                decision.subject_hash,
                run.result
              ) and
              exists(
                from(event in GoalEvent,
                  where:
                    event.goal_id == parent_as(:task).goal_id and event.kind == "task_settled" and
                      fragment(
                        "? ->> 'task_id' = CAST(? AS text)",
                        event.payload,
                        parent_as(:task).id
                      ) and
                      fragment(
                        "? ->> 'run_id' = CAST(? AS text)",
                        event.payload,
                        parent_as(:run).id
                      ) and
                      fragment(
                        "? ->> 'generation' = CAST(? AS text)",
                        event.payload,
                        parent_as(:run).generation
                      ) and
                      fragment("? ->> 'settlement' = 'awaiting_validation'", event.response)
                )
              ) and
              not exists(
                from(outcome in WorkOutcome,
                  where: outcome.validation_task_id == parent_as(:task).id
                )
              ),
          order_by: [asc: task.goal_id, asc: task.id, asc: run.id],
          select: %{task_id: task.id, run_id: run.id, generation: run.generation},
          limit: ^limit
        )

      awaiting_validation =
        if is_list(requested_goal_ids),
          do: where(awaiting_validation, [task: task], task.goal_id in ^requested_goal_ids),
          else: awaiting_validation

      awaiting_validation =
        awaiting_validation
        |> Repo.all()
        |> Enum.map(&Map.put(&1, :reconciliation, :validation_outcome))

      pending =
        (unsettled ++ awaiting_validation)
        |> Enum.uniq_by(&{&1.task_id, &1.run_id, &1.generation})
        |> Enum.sort_by(&{&1.task_id, &1.run_id})
        |> Enum.take(limit)

      {:ok, pending}
    end
  rescue
    error in [CastError] -> {:error, error}
  end

  def recover_pending_terminal_settlements(_), do: {:error, :invalid_request}

  def recover_pending_goal_control_actions(opts \\ [])

  def recover_pending_goal_control_actions(opts) when is_list(opts) do
    requested_goal_ids = Keyword.get(opts, :goal_ids)
    limit = Keyword.get(opts, :limit, 100)

    if not (is_integer(limit) and limit > 0) or
         not (is_nil(requested_goal_ids) or
                (is_list(requested_goal_ids) and Enum.all?(requested_goal_ids, &valid_uuid?/1))) do
      {:error, :invalid_request}
    else
      pending =
        from(goal in Goal,
          as: :goal,
          join: event in GoalEvent,
          as: :event,
          on:
            event.goal_id == goal.id and event.revision == goal.current_revision and
              event.kind in ["pause", "cancel", "amend"],
          join: task in Task,
          on: task.goal_id == goal.id and task.state in ^@cancellable_task_states,
          where:
            (event.kind == "cancel" and goal.state == "cancelled") or
              (event.kind == "amend" and goal.state == "paused"),
          where:
            not exists(
              from(newer in GoalEvent,
                where:
                  newer.goal_id == parent_as(:goal).id and
                    newer.revision == parent_as(:goal).current_revision and
                    newer.kind in ["pause", "cancel", "amend"] and
                    newer.sequence > parent_as(:event).sequence
              )
            ),
          order_by: [asc: goal.id],
          select: goal.id,
          distinct: true,
          limit: ^limit
        )

      pending =
        if is_list(requested_goal_ids),
          do: where(pending, [goal], goal.id in ^requested_goal_ids),
          else: pending

      pending
      |> Repo.all()
      |> Enum.reduce_while({:ok, []}, fn goal_id, {:ok, actions} ->
        case recover_pending_goal_control_action(goal_id) do
          {:ok, nil} -> {:cont, {:ok, actions}}
          {:ok, action} -> {:cont, {:ok, [action | actions]}}
          {:error, reason} -> {:halt, {:error, reason}}
        end
      end)
      |> case do
        {:ok, actions} -> {:ok, Enum.reverse(actions)}
        error -> error
      end
    end
  rescue
    error in [CastError] -> {:error, error}
  end

  def recover_pending_goal_control_actions(_), do: {:error, :invalid_request}

  defp recover_pending_goal_control_action(goal_id) do
    Repo.transaction(fn ->
      goal = lock_goal!(goal_id)

      event =
        Repo.one(
          from(event in GoalEvent,
            where:
              event.goal_id == ^goal.id and event.revision == ^goal.current_revision and
                event.kind in ["pause", "cancel", "amend"],
            order_by: [desc: event.sequence],
            limit: 1,
            lock: "FOR UPDATE"
          )
        )

      valid? =
        event &&
          ((event.kind == "cancel" and goal.state == "cancelled") or
             (event.kind in ["pause", "amend"] and goal.state == "paused"))

      if valid? and control_descriptors(goal, event.kind, event.id) != [] do
        %{goal_id: goal.id, revision: event.revision, action_id: event.id}
      end
    end)
  end

  @doc false

  def control_descriptors(goal, "pause", action_id) do
    goal.id
    |> safe_pause_task_ids()
    |> control_descriptors_for("pause", action_id)
  end

  def control_descriptors(goal, "cancel", action_id) do
    goal.id
    |> cancellable_task_ids()
    |> control_descriptors_for("cancel", action_id)
  end

  def control_descriptors(goal, "amend", action_id) do
    pausable_ids = safe_pause_task_ids(goal.id)

    control_descriptors_for(pausable_ids, "pause", action_id) ++
      (goal.id
       |> cancellable_task_ids()
       |> Enum.reject(&(&1 in pausable_ids))
       |> control_descriptors_for("cancel", action_id))
  end

  # No native adapter has a verified retained pause/resume lifecycle yet.
  # Goal pause stops future admission; amendment and cancellation use cancel.
  defp safe_pause_task_ids(_goal_id), do: []

  @doc false

  def ensure_amendment_controls_settled!(goal) do
    latest_lifecycle_action =
      Repo.one(
        from(event in GoalEvent,
          where: event.goal_id == ^goal.id and event.kind in ["pause", "resume", "cancel", "amend"],
          order_by: [desc: event.sequence],
          limit: 1
        )
      )

    if latest_lifecycle_action && latest_lifecycle_action.kind == "amend" do
      stale_work_remaining? =
        Repo.exists?(
          from(task in Task,
            where:
              task.goal_id == ^goal.id and task.goal_revision < ^goal.current_revision and
                task.state in ^@nonterminal_task_states
          )
        )

      stale_reservation_remaining? =
        Repo.exists?(
          from(reservation in GoalBudgetReservation,
            where:
              reservation.goal_id == ^goal.id and
                reservation.goal_revision < ^goal.current_revision and
                reservation.state == "held"
          )
        )

      if stale_work_remaining? or stale_reservation_remaining?, do: rollback(:state_conflict)
    end
  end

  defp cancellable_task_ids(goal_id) do
    Repo.all(
      from(task in Task,
        where: task.goal_id == ^goal_id and task.state in ^@cancellable_task_states,
        order_by: [asc: task.id],
        select: task.id
      )
    )
  end

  defp control_descriptors_for(task_ids, kind, action_id) do
    Enum.map(task_ids, fn task_id ->
      %{
        task_id: task_id,
        kind: kind,
        payload: %{},
        idempotency_key: "goal-control:#{action_id}:#{kind}:#{task_id}"
      }
    end)
  end

  @doc false

  def recoverable_historical_unstarted_cancellation?(goal, task_id) do
    if Repo.exists?(from(run in Run, where: run.task_id == ^task_id)) do
      false
    else
      reservation =
        Repo.one(
          from(reservation in GoalBudgetReservation,
            where: reservation.goal_id == ^goal.id and reservation.task_id == ^task_id,
            lock: "FOR UPDATE"
          )
        )

      not is_nil(reservation) and reservation.state == "held" and
        from(command in Command,
          where:
            command.task_id == ^task_id and command.kind == "cancel" and
              command.state == "applied" and is_nil(command.run_id),
          select: command.idempotency_key
        )
        |> Repo.all()
        |> Enum.any?(&historical_goal_cancel_command?(goal.id, task_id, &1))
    end
  end

  defp historical_goal_cancel_command?(goal_id, task_id, command_key) when is_binary(command_key) do
    case String.split(command_key, ":", parts: 4) do
      ["goal-control", action_id, "cancel", ^task_id] ->
        valid_uuid(action_id) == :ok and
          Repo.exists?(
            from(event in GoalEvent,
              where:
                event.id == ^action_id and event.goal_id == ^goal_id and
                  event.kind in ["cancel", "amend"]
            )
          )

      _ ->
        false
    end
  end

  defp historical_goal_cancel_command?(_goal_id, _task_id, _command_key), do: false
end
