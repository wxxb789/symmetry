defmodule SymmetryControl.Integrations.Syncer do
  @moduledoc false

  use GenServer

  alias SymmetryControl.Integrations
  alias SymmetryControl.Portal.Invalidations

  def start_link(options \\ []) do
    GenServer.start_link(__MODULE__, options, name: Keyword.get(options, :name, __MODULE__))
  end

  @impl true
  def init(options) do
    config = Application.get_env(:symmetry_control, :integrations, [])

    state = %{
      interval_ms: Keyword.get(options, :interval_ms, config[:sync_interval_ms] || 300_000),
      notify: Keyword.get(options, :notify)
    }

    Process.send_after(self(), :sync, Keyword.get(options, :initial_delay_ms, 5_000))
    {:ok, state}
  end

  @impl true
  def handle_info(:sync, state) do
    summary = Integrations.sync_all_resources()
    # A sync writes the resource's served health/sync state (and its bound work
    # items on a successful provider read), so the connected operator browsers
    # refetch those reads; an empty sweep publishes nothing.
    if summary.synced + summary.failed > 0, do: Invalidations.publish(%{kind: "resource"})
    if state.notify, do: send(state.notify, {:integration_sync_complete, summary})
    Process.send_after(self(), :sync, state.interval_ms)
    {:noreply, state}
  end
end
