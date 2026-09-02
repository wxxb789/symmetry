defmodule SymmetryControl.Orchestration.Reconciler do
  @moduledoc false

  use GenServer

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Scheduler

  def start_link(_opts), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)

  def run_once(opts \\ []) do
    result = Orchestration.expire(opts)
    Scheduler.wake()
    result
  end

  @impl true
  def init(:ok) do
    if enabled?(), do: send(self(), :reap)
    {:ok, %{}}
  end

  @impl true
  def handle_info(:reap, state) do
    _result = run_once()
    Process.send_after(self(), :reap, interval())
    {:noreply, state}
  end

  defp interval do
    Application.fetch_env!(:symmetry_control, :orchestration)
    |> Keyword.fetch!(:reaper_interval_ms)
  end

  defp enabled? do
    Application.fetch_env!(:symmetry_control, :orchestration)
    |> Keyword.fetch!(:reaper_enabled)
  end
end
