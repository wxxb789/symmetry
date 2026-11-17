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
  alias SymmetryControl.Portal.Invalidations

  @impl Oban.Worker
  def perform(%Job{} = job), do: settle(job.args)

  @impl Oban.Worker
  def timeout(_job), do: :timer.seconds(30)

  defp settle(%{"task_id" => task_id, "run_id" => run_id, "generation" => generation})
       when is_binary(task_id) and is_binary(run_id) and is_integer(generation) and generation > 0 do
    case Goals.settle_task(task_id, run_id, generation) do
      # A committed settlement changes the goal's accepted progress and the run's
      # execution truth; the durable goal event log it appended is fetched by the
      # connected browsers after this notification, and a missed notification is
      # recovered by their reconnect/poll refetch.
      {:ok, {:ignored, _locator}} ->
        :ok

      {:ok, _result} ->
        Invalidations.publish(%{kind: "run", id: run_id})
        Invalidations.publish(%{kind: "goal"})
        :ok

      {:error, :stale_run} ->
        :ok

      {:error, :not_found} ->
        :ok

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp settle(_args), do: {:cancel, :invalid_settlement_job}
end
