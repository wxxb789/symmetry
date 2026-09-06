defmodule SymmetryControl.Integrations.CommandTest do
  use ExUnit.Case, async: true

  alias SymmetryControl.Integrations.Command

  test "provider CLI commands time out" do
    assert {:error, {:command_timeout, "sh"}} =
             Command.run("sh", ["-c", "sleep 1"], timeout: 10)
  end

  test "missing provider CLI commands return an error" do
    assert {:error, {:command_unavailable, "definitely-not-installed"}} =
             Command.run("definitely-not-installed", [])
  end

  test "provider token commands can keep stderr diagnostics out of stdout" do
    assert {:ok, "runtime-token"} =
             Command.run(
               "sh",
               ["-c", "printf 'warning\\n' >&2; printf 'runtime-token\\n'"],
               stderr_to_stdout: false
             )
  end
end
