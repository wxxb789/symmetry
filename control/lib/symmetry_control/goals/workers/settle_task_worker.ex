defmodule SymmetryControl.Goals.Workers.SettleTaskWorker do
  @moduledoc false

  use Oban.Worker,
    queue: :goal_settlement,
    max_attempts: 5,
    unique: [
      period: {10, :minutes},
      fields: [:worker, :args],
      keys: [:task_id, :run_id, :generation],
      states: :incomplete
    ]

  alias Oban.Job
  alias SymmetryControl.Goals

  @impl Oban.Worker
  def perform(%Job{} = job), do: settle(job.args)

  @impl Oban.Worker
  def timeout(_job), do: :timer.seconds(30)

  defp settle(%{"task_id" => task_id, "run_id" => run_id, "generation" => generation})
       when is_binary(task_id) and is_binary(run_id) and is_integer(generation) and generation > 0 do
    case Goals.settle_task(task_id, run_id, generation) do
      {:ok, _result} -> :ok
      {:error, :stale_run} -> :ok
      {:error, :not_found} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  defp settle(_args), do: {:cancel, :invalid_settlement_job}
end
