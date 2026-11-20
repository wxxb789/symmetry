defmodule SymmetryControl.TestEnv do
  @moduledoc """
  Application-env override for one test: the previous entry (or its absence)
  is restored exactly when the test exits.
  """

  @app :symmetry_control

  @doc "Sets `:symmetry_control` env `key` to `value`, restoring the previous state on test exit."
  def put!(key, value) do
    previous = Application.fetch_env(@app, key)
    Application.put_env(@app, key, value)

    ExUnit.Callbacks.on_exit(fn ->
      case previous do
        {:ok, previous_value} -> Application.put_env(@app, key, previous_value)
        :error -> Application.delete_env(@app, key)
      end
    end)

    :ok
  end
end
