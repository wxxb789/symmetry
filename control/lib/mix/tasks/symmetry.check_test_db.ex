defmodule Mix.Tasks.Symmetry.CheckTestDb do
  @shortdoc "Refuses test-environment Ecto commands against browser acceptance databases"

  @moduledoc """
  Refuses to run `mix test`, `mix ecto.create`, `mix ecto.migrate` or
  `mix ecto.drop` when the test environment is configured to use a browser
  acceptance database.

  The browser acceptance harnesses (`browser/scripts/run-*.ps1`) create, migrate
  and seed their own databases. Running the test suite against one of them makes
  the committed fixture rows visible to suite-level aggregates
  (`sync_all_resources/0`, workspace and portal reads), which is the 0007d
  failure mode recorded in `docs/TODO.md`; the suite must use the repository
  default test database instead.

  `config/test.exs` appends `MIX_TEST_PARTITION` to `POSTGRES_DB`, so this guard
  evaluates the effective database name the repository configures and treats
  any suffixed variant of an acceptance database as reserved too.

  The task is the first step of the `test` alias and of the
  `ecto.create`/`ecto.migrate`/`ecto.drop` aliases in `mix.exs`, so the refusal
  happens before any storage task can touch PostgreSQL, including the create and
  migrate steps `mix test` runs itself. Outside the test environment it is a
  no-op: the harnesses own these databases from the development environment.
  """

  use Mix.Task

  # Databases created, migrated and seeded by the browser acceptance harnesses.
  # Add an entry here when a harness introduces another database.
  @acceptance_databases ~w(symmetry_0007c_test symmetry_0007d_test symmetry_0007e_test)

  @impl true
  def run(_args) do
    case check(Mix.env(), configured_database()) do
      :ok -> :ok
      {:error, message} -> Mix.raise(message)
    end
  end

  @doc """
  Checks the environment and the effective configured database.

  Returns `:ok` outside the test environment, without a configured database, or
  for a database no acceptance harness owns, and `{:error, message}` when the
  test environment would use an acceptance database.
  """
  @spec check(atom(), String.t() | nil) :: :ok | {:error, String.t()}
  def check(:test, database) when is_binary(database) do
    case Enum.find(@acceptance_databases, &String.starts_with?(database, &1)) do
      nil -> :ok
      acceptance_database -> {:error, refusal(database, acceptance_database)}
    end
  end

  def check(_env, _database), do: :ok

  defp configured_database do
    Application.get_env(:symmetry_control, SymmetryControl.Repo, [])[:database]
  end

  defp refusal(database, acceptance_database) do
    """
    refusing to use the browser acceptance database #{inspect(acceptance_database)} in the test environment.

    The effective configured database is #{inspect(database)} (config/test.exs appends
    MIX_TEST_PARTITION to POSTGRES_DB). The browser acceptance harnesses recreate and seed
    these databases, and their committed fixture rows break suite-level aggregates.
    Unset POSTGRES_DB to use the repository default (symmetry_control_test) or name a
    dedicated test database.
    """
  end
end
