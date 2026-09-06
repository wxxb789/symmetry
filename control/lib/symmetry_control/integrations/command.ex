defmodule SymmetryControl.Integrations.Command do
  @moduledoc false
  @timeout 15_000

  def run(executable, arguments, options \\ []) do
    {timeout, options} = Keyword.pop(options, :timeout, @timeout)

    task =
      Task.async(fn ->
        try do
          System.cmd(executable, arguments, Keyword.merge([stderr_to_stdout: true], options))
        rescue
          ErlangError -> {:command_unavailable, executable}
        end
      end)

    case Task.yield(task, timeout) || Task.shutdown(task, :brutal_kill) do
      {:ok, {:command_unavailable, ^executable}} -> {:error, {:command_unavailable, executable}}
      {:ok, {output, 0}} -> {:ok, String.trim(output)}
      {:ok, {_output, status}} when is_integer(status) -> {:error, {:command_failed, executable}}
      {:exit, _reason} -> {:error, {:command_unavailable, executable}}
      nil -> {:error, {:command_timeout, executable}}
    end
  rescue
    ErlangError -> {:error, {:command_unavailable, executable}}
  end
end
