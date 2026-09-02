defmodule SymmetryControl.Orchestration.Scheduler do
  @moduledoc false

  use GenServer

  alias SymmetryControl.Orchestration

  @name __MODULE__

  def start_link(_opts), do: GenServer.start_link(__MODULE__, :ok, name: @name)

  def wake do
    if enabled?(), do: send(@name, :wake)
    :ok
  end

  def drain(opts \\ []) do
    if enabled?(), do: GenServer.call(@name, {:drain, opts}), else: assign_all(opts)
    :ok
  end

  @impl true
  def init(:ok) do
    if enabled?(), do: send(self(), :wake)
    {:ok, %{scheduled?: false}}
  end

  @impl true
  def handle_call({:drain, opts}, _from, state) do
    assign_all(opts)
    {:reply, :ok, %{state | scheduled?: false}}
  end

  @impl true
  def handle_info(:wake, %{scheduled?: false} = state) do
    send(self(), :assign)
    {:noreply, %{state | scheduled?: true}}
  end

  def handle_info(:wake, state), do: {:noreply, state}

  def handle_info(:assign, state) do
    assign_all([])
    {:noreply, %{state | scheduled?: false}}
  end

  defp assign_all(opts) do
    options = Keyword.put_new(opts, :assignment_duration_ms, config(:assignment_duration_ms))

    case Orchestration.assign_all(options) do
      {:ok, runs} -> Enum.each(runs, &broadcast_assignment/1)
      {:error, _reason} -> :ok
    end
  end

  defp broadcast_assignment(run) do
    with {:ok, %{machine_id: machine_id, runtime_id: runtime_id}} <-
           Orchestration.assignment_target(run) do
      Phoenix.PubSub.broadcast(
        SymmetryControl.PubSub,
        "daemon:" <> machine_id,
        {:work_available, %{runtime_id: runtime_id}}
      )
    end
  end

  defp config(key),
    do: Application.fetch_env!(:symmetry_control, :orchestration) |> Keyword.fetch!(key)

  defp enabled?, do: config(:scheduler_enabled)
end
