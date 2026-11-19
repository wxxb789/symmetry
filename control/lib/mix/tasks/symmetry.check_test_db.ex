defmodule Mix.Tasks.Symmetry.CheckTestDb do
  @shortdoc "Refuses test-environment Ecto commands against browser acceptance databases"

  @moduledoc """
  Refuses to run `mix test`, `mix ecto.create`, `mix ecto.migrate`,
  `mix ecto.drop`, `mix ecto.rollback` or `mix ecto.load` when the test
  environment is configured to use a browser acceptance database.

  The browser acceptance harnesses (`browser/scripts/run-*.ps1`) create, migrate
  and seed their own databases. Running the test suite against one of them makes
  the committed fixture rows visible to suite-level aggregates
  (`sync_all_resources/0`, workspace and portal reads), which is the 0007d
  failure mode recorded in `docs/archive/CLOSED-TODO.md`; the suite must use the
  repository default test database instead.

  `config/test.exs` appends `MIX_TEST_PARTITION` to `POSTGRES_DB`, so this guard
  evaluates the effective database name the repository configures. It reserves
  whole harness database families, because the planning harness takes the
  database name as a `-DbName` parameter: every name beginning with
  `symmetry_0007c_`, `symmetry_0007d_` or `symmetry_0007e_` is refused,
  whatever suffix follows.

  The task is the first step of the `test` alias and of the
  `ecto.create`/`ecto.migrate`/`ecto.drop`/`ecto.rollback`/`ecto.load` aliases in
  `mix.exs`, so the refusal happens before any storage task can touch
  PostgreSQL, including the create and migrate steps `mix test` runs itself.
  Outside the test environment it is a no-op: the harnesses own these databases
  from the development environment.
  """

  use Mix.Task

  # Database families created, migrated and seeded by the browser acceptance
  # harnesses. Add an entry here when a harness introduces another family.
  @acceptance_database_prefixes ~w(symmetry_0007c_ symmetry_0007d_ symmetry_0007e_)

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
  test environment would use an acceptance database family.
  """
  @spec check(atom(), String.t() | nil) :: :ok | {:error, String.t()}
  def check(:test, database) when is_binary(database) do
    case Enum.find(@acceptance_database_prefixes, &String.starts_with?(database, &1)) do
      nil -> :ok
      _prefix -> {:error, refusal(database)}
    end
  end

  def check(_env, _database), do: :ok

  defp configured_database do
    Application.get_env(:symmetry_control, SymmetryControl.Repo, [])[:database]
  end

  defp refusal(database) do
    """
    refusing to use the browser acceptance database #{inspect(database)} in the test environment.

    The browser acceptance harnesses recreate and seed every database named for their
    families (#{Enum.join(@acceptance_database_prefixes, ", ")}, any suffix), and their
    committed fixture rows break suite-level aggregates. config/test.exs appends
    MIX_TEST_PARTITION to POSTGRES_DB, so the effective configured name is the one above.
    Unset POSTGRES_DB to use the repository default (symmetry_control_test) or name a
    dedicated test database.
    """
  end
end
