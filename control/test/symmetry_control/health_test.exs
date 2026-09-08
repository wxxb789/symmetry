defmodule SymmetryControl.HealthTest do
  use ExUnit.Case, async: true

  alias SymmetryControl.Health

  test "returns ok when every injected check succeeds" do
    assert {:ok, %{status: "ok", checks: %{database: "ok", scheduler: "ok"}}} =
             Health.check(database: fn -> :ok end, scheduler: fn -> :ok end)
  end

  test "returns unavailable without exposing a check failure reason" do
    assert {:error, %{status: "unavailable", checks: %{database: "error", scheduler: "ok"}}} =
             Health.check(database: fn -> {:error, :down} end, scheduler: fn -> :ok end)
  end
end
