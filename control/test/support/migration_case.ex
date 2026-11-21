defmodule SymmetryControl.MigrationCase do
  @moduledoc """
  The isolated-schema harness of the migration tests: every test migrates
  into a private schema through its own Repo, then the schema is dropped,
  so partially-migrated states never touch the shared test schema.
  """

  use ExUnit.CaseTemplate

  alias Ecto.Adapters.SQL.Sandbox
  alias SymmetryControl.Repo

  using do
    quote do
      import SymmetryControl.MigrationCase,
        only: [
          with_schema: 2,
          start_schema_repo: 1,
          create_schema!: 1,
          drop_schema!: 1,
          migrate_all_up!: 0
        ]
    end
  end

  @doc """
  Runs `test` against a private schema whose name is `schema_base` plus a
  unique suffix.

  The schema is created first, a dedicated single-connection Repo starts
  against it, and the dynamic repo points there for the duration of `test`.
  The dynamic repo is restored, the Repo stopped and the schema dropped
  afterwards; a failure while starting the Repo still drops the schema, and a
  failure while pointing the dynamic repo at it still stops the Repo.

  Tests that need to observe pre-migration state call `create_schema!/1`,
  `start_schema_repo/1` and `drop_schema!/1` directly instead.
  """
  def with_schema(schema_base, test) do
    schema = "#{schema_base}_#{System.unique_integer([:positive])}"
    create_schema!(schema)

    try do
      repo = start_schema_repo(schema)

      try do
        previous_dynamic_repo = Repo.put_dynamic_repo(repo)

        try do
          test.()
        after
          Repo.put_dynamic_repo(previous_dynamic_repo)
        end
      after
        GenServer.stop(repo)
      end
    after
      drop_schema!(schema)
    end
  end

  @doc "Starts a dedicated single-connection Repo whose search path is `schema`."
  def start_schema_repo(schema) do
    config =
      Application.fetch_env!(:symmetry_control, Repo)
      |> Keyword.merge(name: nil, pool: DBConnection.ConnectionPool, pool_size: 1)
      |> Keyword.put(:parameters, search_path: schema)

    {:ok, repo} = Repo.start_link(config)
    repo
  end

  @doc "Creates `schema` through the shared Repo outside the sandbox."
  def create_schema!(schema) do
    Sandbox.unboxed_run(Repo, fn -> Repo.query!("CREATE SCHEMA #{schema}") end)
  end

  @doc "Drops `schema` and everything in it."
  def drop_schema!(schema) do
    Sandbox.unboxed_run(Repo, fn -> Repo.query!("DROP SCHEMA IF EXISTS #{schema} CASCADE") end)
  end

  @doc """
  The repository's migration directory.

  The support tree sits two levels below `control/`, so this resolves to
  `control/priv/repo/migrations` — the same directory the suites that carried
  this helper locally resolved from `test/symmetry_control/migrations`.
  """
  def migrations_path do
    Path.expand("../../priv/repo/migrations", __DIR__)
  end

  @doc """
  Runs every migration in `migrations_path/0` up through the Repo's dynamic repo.

  The dynamic repo is the private-schema repo a suite started with
  `start_schema_repo/1` (or `with_schema/2`), so the migrations land in that
  private schema and never in the shared test schema.
  """
  def migrate_all_up! do
    Ecto.Migrator.run(Repo, migrations_path(), :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end
end
