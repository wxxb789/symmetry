defmodule Mix.Tasks.Symmetry.CheckTestDb do
  @shortdoc "Refuses test-environment Ecto commands against browser acceptance databases"
  @moduledoc """
  Refuses to run `mix test`, `mix ecto.create`, `mix ecto.migrate`,
  `mix ecto.migrations`, `mix ecto.drop`, `mix ecto.rollback` or `mix ecto.load`
  when the test environment is configured to use a browser acceptance database.

  The browser acceptance harnesses (`browser/scripts/run-*.ps1`) own the
  databases they configure: the compose harness seeds and fingerprints
  `symmetry_dev`, and the planning and fixture
  harnesses recreate and seed the databases named for their families. Running
  the test suite against one of them makes the committed fixture rows visible to
  suite-level aggregates (`sync_all_resources/0`, workspace and portal reads),
  which is the 0007d failure mode recorded in `docs/archive/CLOSED-TODO.md`; the
  suite must use the repository default or another dedicated test database,
  never a name a harness owns.

  `config/test.exs` appends `MIX_TEST_PARTITION` to `POSTGRES_DB`, so this guard
  evaluates the effective database name the repository configures. Whole
  families are reserved, because a harness pointed at any name in one recreates
  and seeds it: every name beginning with
  `symmetry_0007c_`, `symmetry_0007d_`, `symmetry_0007e_` is refused, whatever
  suffix follows. The compose entry `symmetry_dev`
  is refused as an exact name: the compose harness never partition-suffixes the
  database it seeds and fingerprints, so a suffixed variant belongs to no
  harness. An arbitrary `-DbName` override cannot be covered by a static list;
  `symmetry_check_test_db_test.exs` keeps the list anchored to the values the
  harness scripts assign.

  The task is the first step of the `test` alias and of the
  `ecto.create`/`ecto.migrate`/`ecto.migrations`/`ecto.drop`/`ecto.rollback`/`ecto.load`
  aliases in `mix.exs`, so the refusal happens before any storage task can touch
  PostgreSQL, including the create and migrate steps `mix test` runs itself.
  Outside the test environment it is a no-op: the harnesses own these databases
  from the development environment.
  """

  use Mix.Task

  # The compose acceptance database, seeded and row-fingerprinted by
  # `browser/scripts/run-compose-daemon-tests.ps1`.
  @acceptance_databases ~w(symmetry_dev)

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
  test environment would use an acceptance database.
  """
  @spec check(atom(), String.t() | nil) :: :ok | {:error, String.t()}
  def check(:test, database) when is_binary(database) do
    if acceptance_database?(database), do: {:error, refusal(database)}, else: :ok
  end

  def check(_env, _database), do: :ok

  defp configured_database do
    Application.get_env(:symmetry_control, SymmetryControl.Repo, [])[:database]
  end

  defp acceptance_database?(database) do
    database in @acceptance_databases or
      Enum.any?(@acceptance_database_prefixes, &String.starts_with?(database, &1))
  end

  defp refusal(database) do
    """
    refusing to use the browser acceptance database #{inspect(database)} in the test environment.

    The browser acceptance harnesses own the databases they configure. The compose
    harness seeds and fingerprints #{Enum.join(@acceptance_databases, ", ")}; the
    planning and fixture harnesses recreate and seed the databases named for their
    families. Each family is reserved as a whole, any suffix included
    (#{Enum.join(@acceptance_database_prefixes, ", ")}). Their committed fixture rows
    break suite-level aggregates. config/test.exs appends MIX_TEST_PARTITION to
    POSTGRES_DB, so the effective configured name is the one above. Unset POSTGRES_DB
    to use the repository default (symmetry_control_test) or another dedicated test
    database, never a name a harness owns.
    """
  end
end
