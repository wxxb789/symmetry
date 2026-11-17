defmodule SymmetryControl.ExUnitJsonFormatter do
  @moduledoc """
  Writes one JSON line per ExUnit event to `SYMMETRY_EXUNIT_EVENTS_FILE`.

  Gated CI steps register this formatter next to `ExUnit.CLIFormatter` and check
  the stream with `scripts/ci/assert-exunit-json.py`, which requires every named
  test to appear exactly once in state `passed`. Scraping the human-readable
  summary cannot do that: a skipped-only run, a decoy `Result:` line or a
  renamed test all read as success there.

  Emitted lines, one per event, and no other output:

    * `suite_started` - the include/exclude filters the run actually selected
      and the seed, so a tag that no longer matches is visible in evidence
    * `test_finished` - module, name, state (`passed`, `skipped`, `excluded`,
      `failed`, `invalid` or `unknown`) and duration in microseconds
    * `suite_finished` - the run measurements and this formatter's own counts

  The file is opened with `:sync`, so every event is on disk before the run
  continues and a VM that dies after the last test still leaves a complete
  stream for the gate.
  """

  use GenServer

  @events_file_env "SYMMETRY_EXUNIT_EVENTS_FILE"

  @impl GenServer
  def init(opts) do
    path = events_file!(System.get_env(@events_file_env))

    {:ok,
     %{
       io: File.open!(path, [:write, :sync, :binary]),
       include: filters(opts[:include]),
       exclude: filters(opts[:exclude]),
       seed: opts[:seed],
       counts: %{total: 0, passed: 0, failed: 0, skipped: 0, excluded: 0, invalid: 0}
     }}
  end

  @impl GenServer
  def handle_cast({:suite_started, _opts}, state) do
    state =
      emit(state, %{
        event: "suite_started",
        include: state.include,
        exclude: state.exclude,
        seed: state.seed
      })

    {:noreply, state}
  end

  def handle_cast({:test_finished, %ExUnit.Test{} = test}, state) do
    name = state_name(test.state)

    state =
      emit(state, %{
        event: "test_finished",
        module: inspect(test.module),
        name: Atom.to_string(test.name),
        state: name,
        duration_us: test.time || 0
      })

    {:noreply, count(state, name)}
  end

  def handle_cast({:suite_finished, times}, state) do
    state =
      emit(state, %{
        event: "suite_finished",
        run_us: times[:run],
        async_us: times[:async],
        load_us: times[:load],
        counts: state.counts
      })

    {:noreply, state}
  end

  def handle_cast(_event, state), do: {:noreply, state}

  defp events_file!(path) when is_binary(path) and path != "", do: path

  defp events_file!(path) do
    raise ArgumentError,
          "#{@events_file_env} must name the ExUnit JSON event stream to write, got: #{inspect(path)}"
  end

  defp emit(state, payload) do
    IO.write(state.io, [Jason.encode!(payload), "\n"])
    state
  end

  defp count(state, name) do
    counts =
      state.counts
      |> Map.update!(:total, &(&1 + 1))
      |> Map.update!(count_key(name), &(&1 + 1))

    %{state | counts: counts}
  end

  defp count_key("passed"), do: :passed
  defp count_key("failed"), do: :failed
  defp count_key("skipped"), do: :skipped
  defp count_key("excluded"), do: :excluded
  defp count_key(_other), do: :invalid

  defp state_name(nil), do: "passed"
  defp state_name({:failed, _failures}), do: "failed"
  defp state_name({:skipped, _reason}), do: "skipped"
  defp state_name({:excluded, _reason}), do: "excluded"
  defp state_name({:invalid, _module}), do: "invalid"
  defp state_name(_other), do: "unknown"

  defp filters(nil), do: []

  defp filters(list) when is_list(list), do: Enum.map(list, &filter/1)

  defp filter(key) when is_atom(key), do: Atom.to_string(key)
  defp filter({key, value}) when is_atom(key), do: "#{key}:#{filter_value(value)}"
  defp filter(other), do: inspect(other)

  defp filter_value(value) when is_binary(value), do: value
  defp filter_value(value) when is_atom(value), do: Atom.to_string(value)
  defp filter_value(value), do: inspect(value)
end
