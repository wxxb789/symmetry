defmodule SymmetryControl.Integrations.CommandTest do
  use ExUnit.Case, async: true

  alias SymmetryControl.Integrations.Command

  test "provider CLI commands time out" do
    {executable, arguments} = shell_command("sleep 1", "Start-Sleep -Seconds 1")

    assert {:error, {:command_timeout, ^executable}} =
             Command.run(executable, arguments, timeout: 10)
  end

  test "missing provider CLI commands return an error" do
    assert {:error, {:command_unavailable, "definitely-not-installed"}} =
             Command.run("definitely-not-installed", [])
  end

  test "provider token commands can keep stderr diagnostics out of stdout" do
    {executable, arguments} =
      shell_command(
        "printf 'warning\\n' >&2; printf 'runtime-token\\n'",
        "[Console]::Error.WriteLine('warning'); [Console]::Out.WriteLine('runtime-token')"
      )

    assert {:ok, "runtime-token"} =
             Command.run(
               executable,
               arguments,
               stderr_to_stdout: false
             )
  end

  defp shell_command(unix_script, windows_script) do
    case :os.type() do
      {:win32, _} -> {windows_shell(), ["-NoProfile", "-Command", windows_script]}
      _ -> {"sh", ["-c", unix_script]}
    end
  end

  # A Microsoft Store PowerShell 7 is exposed on PATH only as a zero-byte
  # app-execution alias, which Erlang cannot resolve (`File.stat/1` reports
  # `:eio` for the reparse point), so `System.find_executable("pwsh")` alone
  # understates a host whose shell runs `pwsh` fine. Prefer a resolvable
  # PowerShell 7 and otherwise use the Windows PowerShell 5.1 that ships with
  # Windows, which exercises the same command semantics; if neither resolves,
  # name the prerequisite instead of reporting an unexplained
  # `command_unavailable`.
  defp windows_shell do
    System.find_executable("pwsh") || System.find_executable("powershell.exe") ||
      raise "CommandTest requires a shell on Windows: neither \"pwsh\" nor \"powershell.exe\" resolved via System.find_executable/1"
  end
end
