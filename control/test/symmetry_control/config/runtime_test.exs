defmodule SymmetryControl.Config.RuntimeTest do
  use ExUnit.Case, async: false

  @runtime_config Path.expand("../../../config/runtime.exs", __DIR__)
  @token_variables ["SYMMETRY_ENROLLMENT_TOKEN", "SYMMETRY_OPERATOR_TOKEN"]

  setup do
    previous = Map.new(@token_variables, &{&1, System.get_env(&1)})

    on_exit(fn ->
      Enum.each(previous, fn
        {variable, nil} -> System.delete_env(variable)
        {variable, value} -> System.put_env(variable, value)
      end)
    end)
  end

  test "development runtime config rejects identical enrollment and operator tokens" do
    System.put_env("SYMMETRY_ENROLLMENT_TOKEN", "same-token")
    System.put_env("SYMMETRY_OPERATOR_TOKEN", "same-token")

    assert_raise RuntimeError,
                 "SYMMETRY_ENROLLMENT_TOKEN and SYMMETRY_OPERATOR_TOKEN must differ",
                 fn ->
                   Config.Reader.read!(@runtime_config, env: :dev)
                 end
  end
end
