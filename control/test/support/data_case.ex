defmodule SymmetryControl.DataCase do
  @moduledoc """
  This module defines the setup for tests requiring
  access to the application's data layer.

  You may define functions here to be used as helpers in
  your tests.

  Finally, if the test case interacts with the database,
  we enable the SQL sandbox, so changes done to the database
  are reverted at the end of every test. If you are using
  PostgreSQL, you can even run database tests asynchronously
  by setting `use SymmetryControl.DataCase, async: true`, although
  this option is not recommended for other databases.
  """

  use ExUnit.CaseTemplate

  using do
    quote do
      alias SymmetryControl.Repo

      import Ecto
      import Ecto.Changeset
      import Ecto.Query
      import SymmetryControl.DataCase
    end
  end

  setup tags do
    SymmetryControl.DataCase.setup_sandbox(tags)
    :ok
  end

  @doc """
  Sets up the sandbox based on the test tags.
  """
  def setup_sandbox(tags) do
    pid =
      Ecto.Adapters.SQL.Sandbox.start_owner!(
        SymmetryControl.Repo,
        sandbox_options(tags)
      )

    on_exit(fn -> Ecto.Adapters.SQL.Sandbox.stop_owner(pid) end)
  end

  @doc false
  def sandbox_options(tags) do
    options = [shared: not tags[:async]]

    case tags[:sandbox_ownership_timeout] do
      nil ->
        options

      timeout when is_integer(timeout) and timeout > 0 and timeout <= 600_000 ->
        Keyword.put(options, :ownership_timeout, timeout)

      timeout ->
        raise ArgumentError,
              "sandbox_ownership_timeout must be a finite positive integer no greater than 600000, got: #{inspect(timeout)}"
    end
  end

  @doc """
  A helper that transforms changeset errors into a map of messages.

      assert {:error, changeset} = Accounts.create_user(%{password: "short"})
      assert "password is too short" in errors_on(changeset).password
      assert %{password: ["password is too short"]} = errors_on(changeset)

  """
  def errors_on(changeset) do
    Ecto.Changeset.traverse_errors(changeset, fn {message, opts} ->
      Regex.replace(~r"%{(\w+)}", message, fn _, key ->
        opts |> Keyword.get(String.to_existing_atom(key), key) |> to_string()
      end)
    end)
  end
end
