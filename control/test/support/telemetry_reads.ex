defmodule SymmetryControl.TelemetryReads do
  @moduledoc """
  Reads the `[:symmetry_control, :repo, :query]` telemetry a test attaches to
  prove a served read keeps every query on the calling process.

  Each caller owns its attach block (handler id, filter forms, message tag); this
  module owns the two shared pieces: the predicate that selects the queries
  belonging to the read under test, and the drain that collects the
  `{tag, pid, query}` messages the handler sent, newest first.
  """

  @doc """
  Whether a query's `metadata.params` reference one of the `forms` under test.

  A form may appear as a direct parameter, inside a list parameter, or as a
  dumped binary id; a non-binary value never matches.
  """
  @spec read?(term(), [term()]) :: boolean()
  def read?(params, forms) do
    params
    |> List.wrap()
    |> Enum.any?(fn
      value when is_binary(value) -> value in forms
      values when is_list(values) -> Enum.any?(values, &(&1 in forms))
      _value -> false
    end)
  end

  @doc """
  Drains the messages the attached handler sent for `tag`.

  The handler runs with the query, so every message is already queued when the
  read under test returns: an empty timeout is a drain, never a wait.
  """
  @spec drain(atom()) :: [{pid(), String.t()}]
  def drain(tag), do: drain(tag, [])

  defp drain(tag, reads) do
    receive do
      {^tag, pid, query} -> drain(tag, [{pid, query} | reads])
    after
      0 -> reads
    end
  end
end
