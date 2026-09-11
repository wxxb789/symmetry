defmodule SymmetryControl.Goals.Workers.WakeupWorker do
  @moduledoc false

  use Oban.Worker,
    queue: :goal_wakeup,
    max_attempts: 5,
    unique: [
      period: {10, :minutes},
      fields: [:worker, :args],
      keys: [:goal_id],
      states: :incomplete
    ]

  alias Oban.Job
  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.Workers.GoalControlWorker
  alias SymmetryControl.Orchestration.Scheduler
  alias SymmetryControl.Repo

  @impl Oban.Worker
  def perform(%Job{} = job) do
    case reconcile(job.args) do
      :ok ->
        unless Repo.in_transaction?(), do: Scheduler.wake()
        :ok

      result ->
        result
    end
  end

  @impl Oban.Worker
  def timeout(_job), do: :timer.seconds(30)

  defp reconcile(args) when is_map(args) and map_size(args) == 0 do
    reconcile_all([])
  end

  defp reconcile(%{"goal_id" => goal_id}) when is_binary(goal_id) do
    reconcile_all(goal_ids: [goal_id])
  end

  defp reconcile(_args), do: {:cancel, :invalid_wakeup_job}

  defp reconcile_due_goals(opts) do
    case Goals.reconcile_due_goals(opts) do
      {:ok, _result} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  defp reconcile_all(opts) do
    with :ok <- recover_terminal_settlements(opts),
         :ok <- recover_unstarted_cancellations(opts),
         :ok <- recover_goal_controls(opts) do
      reconcile_due_goals(opts)
    end
  end

  defp recover_terminal_settlements(opts) do
    case Goals.recover_pending_terminal_settlements(opts) do
      {:ok, settlements} when is_list(settlements) ->
        Enum.reduce_while(settlements, :ok, fn settlement, :ok ->
          case Goals.settle_task(settlement.task_id, settlement.run_id, settlement.generation) do
            {:ok, _receipt} -> {:cont, :ok}
            {:error, :stale_run} -> {:cont, :ok}
            {:error, :not_found} -> {:cont, :ok}
            {:error, reason} -> {:halt, {:error, reason}}
          end
        end)

      {:error, reason} ->
        {:error, reason}

      result ->
        {:error, {:unexpected_terminal_settlement_recovery_result, result}}
    end
  end

  defp recover_unstarted_cancellations(opts) do
    with {:ok, goal_ids} <- Goals.recoverable_unstarted_cancellation_goal_ids(opts) do
      Enum.reduce_while(goal_ids, :ok, fn goal_id, :ok ->
        case Goals.recover_pending_unstarted_cancellations(goal_id) do
          {:ok, task_ids} ->
            settle_unstarted_tasks(task_ids)

          {:error, :not_found} ->
            {:cont, :ok}

          {:error, reason} ->
            {:halt, {:error, reason}}
        end
      end)
    else
      {:error, reason} -> {:error, reason}
    end
  end

  defp settle_unstarted_tasks(task_ids) do
    Enum.reduce_while(task_ids, :ok, fn task_id, :ok ->
      case Goals.settle_unstarted_task(task_id, "cancelled") do
        {:ok, _receipt} -> {:cont, :ok}
        {:error, :invalid_unstarted_settlement} -> {:cont, :ok}
        {:error, :not_found} -> {:cont, :ok}
        {:error, reason} -> {:halt, {:error, reason}}
      end
    end)
  end

  defp recover_goal_controls(opts) do
    case Goals.recover_pending_goal_control_actions(opts) do
      {:ok, actions} when is_list(actions) ->
        Enum.reduce_while(actions, :ok, fn action, :ok ->
          action
          |> Map.new(fn {key, value} -> {to_string(key), value} end)
          |> GoalControlWorker.new()
          |> Oban.insert()
          |> case do
            {:ok, _job} -> {:cont, :ok}
            {:error, reason} -> {:halt, {:error, reason}}
          end
        end)

      {:error, reason} ->
        {:error, reason}

      result ->
        {:error, {:unexpected_goal_control_recovery_result, result}}
    end
  end
end
