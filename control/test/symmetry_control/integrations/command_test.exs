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
      {:win32, _} -> {"pwsh", ["-NoProfile", "-Command", windows_script]}
      _ -> {"sh", ["-c", unix_script]}
    end
  end
end
