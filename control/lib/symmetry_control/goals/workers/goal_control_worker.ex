defmodule SymmetryControl.Goals.Workers.GoalControlWorker do
  @moduledoc false

  use Oban.Worker,
    queue: :goal_control,
    max_attempts: 5,
    unique: [
      period: {10, :minutes},
      fields: [:worker, :args],
      keys: [:goal_id, :revision, :action_id],
      states: :incomplete
    ]

  alias Oban.Job
  alias SymmetryControl.{Goals, Orchestration, Repo}
  alias SymmetryControl.Orchestration.Notifier

  @impl Oban.Worker
  def perform(%Job{} = job), do: dispatch(job.args)

  @impl Oban.Worker
  def timeout(_job), do: :timer.seconds(30)

  defp dispatch(%{"goal_id" => goal_id, "revision" => revision, "action_id" => action_id})
       when is_binary(goal_id) and is_integer(revision) and revision > 0 and is_binary(action_id) do
    with :ok <- recover_unstarted_cancellations(goal_id) do
      case Goals.control_dispatch_plan(goal_id, revision, action_id) do
        {:ok, :superseded} ->
          :ok

        {:ok, descriptors} when is_list(descriptors) ->
          dispatch_commands(goal_id, revision, action_id, descriptors)

        {:error, reason} ->
          {:error, reason}
      end
    end
  end

  defp dispatch(_args), do: {:cancel, :invalid_goal_control_job}

  defp dispatch_commands(goal_id, revision, action_id, descriptors) do
    Enum.reduce_while(descriptors, :ok, fn descriptor, :ok ->
      case dispatch_command(goal_id, revision, action_id, descriptor) do
        :ok -> {:cont, :ok}
        {:error, reason} -> {:halt, {:error, reason}}
      end
    end)
  end

  defp recover_unstarted_cancellations(goal_id) do
    case Goals.recover_pending_unstarted_cancellations(goal_id) do
      {:ok, recoverable_task_ids} -> settle_recoverable_tasks(recoverable_task_ids)
      {:error, reason} -> {:error, reason}
    end
  end

  defp dispatch_command(goal_id, revision, action_id, %{
         task_id: task_id,
         kind: kind,
         payload: payload,
         idempotency_key: idempotency_key
       })
       when is_binary(goal_id) and is_integer(revision) and revision > 0 and is_binary(action_id) and
              is_binary(task_id) and kind in ["pause", "cancel"] and is_map(payload) and
              payload == %{} and is_binary(idempotency_key) and byte_size(idempotency_key) > 0 do
    case Orchestration.create_goal_control_command(
           goal_id,
           revision,
           action_id,
           task_id,
           kind,
           payload,
           idempotency_key
         ) do
      {:ok, %{kind: "cancel", state: "applied", run_id: nil}, _disposition} ->
        settle_unstarted_task(task_id)

      {:ok, command, _disposition} ->
        unless Repo.in_transaction?(), do: Notifier.command_available(command)
        :ok

      {:error, :stale_revision} ->
        :ok

      {:error, reason} ->
        {:error, {:control_command_failed, task_id, kind, reason}}
    end
  end

  defp dispatch_command(_goal_id, _revision, _action_id, _descriptor),
    do: {:error, :invalid_goal_control_descriptor}

  defp settle_recoverable_tasks(task_ids) when is_list(task_ids) do
    Enum.reduce_while(task_ids, :ok, fn task_id, :ok ->
      case settle_unstarted_task(task_id) do
        :ok ->
          {:cont, :ok}

        {:error, reason} ->
          {:halt, {:error, {:unstarted_task_settlement_failed, task_id, reason}}}
      end
    end)
  end

  defp settle_recoverable_tasks(:superseded), do: :ok

  defp settle_unstarted_task(task_id) do
    case Goals.settle_unstarted_task(task_id, "cancelled") do
      {:ok, _result} -> :ok
      {:error, reason} -> {:error, {:unstarted_task_settlement_failed, task_id, reason}}
    end
  end
end
