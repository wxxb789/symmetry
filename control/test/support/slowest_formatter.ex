defmodule SymmetryControl.SlowestFormatter do
  @moduledoc """
  Prints the slowest tests at suite end, next to `ExUnit.CLIFormatter`.

  The control job's `mix test` step registers this formatter as a second
  `--formatter` so per-test duration telemetry reaches the CI log without
  ExUnit's own `--slowest` flag. That flag is forbidden here: it forces trace
  mode (`ExUnit.configure/1` sets `:trace` whenever `:slowest` is positive),
  which drops `:max_cases` to `1` (the whole suite serialized) and makes every
  test timeout `:infinity`. This formatter reads the same `test_finished`
  events, so it adds the table and changes nothing about how the tests run;
  `ExUnit.CLIFormatter` stays listed and keeps the run's normal output and
  exit status.

  Only tests that ran count (passed or failed, like the CLI formatter's own
  slowest list); a run with no finished tests prints nothing.
  """

  use GenServer

  @slowest_tests 20

  @impl GenServer
  def init(_opts), do: {:ok, []}

  @impl GenServer
  def handle_cast({:test_finished, %ExUnit.Test{state: nil} = test}, tests),
    do: {:noreply, [test | tests]}

  def handle_cast({:test_finished, %ExUnit.Test{state: {:failed, _failures}} = test}, tests),
    do: {:noreply, [test | tests]}

  def handle_cast({:suite_finished, _times}, tests) do
    print_slowest(tests)
    {:noreply, tests}
  end

  def handle_cast(_event, tests), do: {:noreply, tests}

  defp print_slowest(tests) do
    slowest = tests |> Enum.sort_by(& &1.time, :desc) |> Enum.take(@slowest_tests)

    if slowest != [] do
      IO.puts("\nTop #{length(slowest)} slowest tests:")

      Enum.each(slowest, fn test -> IO.puts(format_row(test)) end)
    end
  end

  defp format_row(%ExUnit.Test{time: time, module: module, name: name, tags: tags}) do
    location =
      case {tags[:file], tags[:line]} do
        {file, line} when is_binary(file) and is_integer(line) ->
          " [#{Path.relative_to_cwd(file)}:#{line}]"

        _missing ->
          ""
      end

    "  * #{Atom.to_string(name)} (#{inspect(module)}) (#{format_ms(time)}ms)#{location}"
  end

  defp format_ms(microseconds) do
    milliseconds = div(microseconds, 100)
    "#{div(milliseconds, 10)}.#{rem(milliseconds, 10)}"
  end
end
