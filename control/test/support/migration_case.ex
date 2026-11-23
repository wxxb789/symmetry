defmodule SymmetryControl.MigrationCase do
  @moduledoc """
  The isolated-schema harness of the migration tests: every test migrates
  into a private schema through its own Repo, then the schema is dropped,
  so partially-migrated states never touch the shared test schema.
  """

  use ExUnit.CaseTemplate

  import ExUnit.Callbacks, only: [on_exit: 1]

  alias SymmetryControl.MigrationModules
  alias SymmetryControl.Repo

  using do
    quote do
      import SymmetryControl.MigrationCase,
        only: [
          with_schema: 2,
          schema_name: 1,
          start_schema_repo: 1,
          create_schema!: 1,
          drop_schema!: 1,
          migrate_all_up!: 0
        ]
    end
  end

  @doc """
  Runs `test` against `schema_base` plus a unique suffix, in the dynamic repo's private schema.
  The order create, start, `put_dynamic_repo`, restore/stop/drop is load-bearing; to observe
  pre-migration state call `schema_name/1`, `create_schema!/1`, `start_schema_repo/1` and
  `drop_schema!/1` directly instead.
  """
  def with_schema(schema_base, test) do
    schema = schema_name(schema_base)

    try do
      create_schema!(schema)
      repo = start_schema_repo(schema)

      try do
        previous_dynamic_repo = Repo.put_dynamic_repo(repo)

        try do
          test.()
        after
          Repo.put_dynamic_repo(previous_dynamic_repo)
        end
      after
        GenServer.stop(repo, :normal, 30_000)
      end
    after
      drop_schema!(schema)
    end
  end

  @doc "The private schema name for `schema_base`: unique per VM, test and wall-clock instant."
  def schema_name(schema_base) do
    unless is_binary(schema_base) and Regex.match?(~r/\A[a-z_][a-z0-9_]*\z/, schema_base) do
      raise ArgumentError, "schema base #{inspect(schema_base)} is not a lowercase SQL identifier"
    end

    name =
      "#{schema_base}_#{System.unique_integer([:positive])}_#{System.system_time(:microsecond)}"

    # PostgreSQL truncates identifiers past NAMEDATALEN - 1 = 63 bytes silently.
    if byte_size(name) > 63 do
      raise ArgumentError, "schema name #{name} exceeds PostgreSQL's 63-byte identifier limit"
    end

    name
  end

  @doc "Starts a dedicated single-connection Repo whose search path is `schema`."
  def start_schema_repo(schema) do
    config =
      private_repo_config()
      |> Keyword.put(:parameters, search_path: schema)

    {:ok, repo} = Repo.start_link(config)
    repo
  end

  @doc """
  Registers an ExUnit `on_exit` drop for the paths no `after` block can reach: the runner process
  runs it even after ExUnit killed the test process for a timeout.
  """
  def create_schema!(schema) do
    on_exit(fn -> drop_schema!(schema) end)
    run_schema_ddl!("CREATE SCHEMA #{schema}")
  end

  @doc "Drops `schema` and everything in it, outside the shared sandbox pool."
  def drop_schema!(schema) do
    run_schema_ddl!("DROP SCHEMA IF EXISTS #{schema} CASCADE")
  end

  defp run_schema_ddl!(statement) do
    {:ok, repo} = Repo.start_link(private_repo_config())

    try do
      Ecto.Adapters.SQL.query!(repo, statement)
    after
      GenServer.stop(repo, :normal, 30_000)
    end
  end

  defp private_repo_config do
    Application.fetch_env!(:symmetry_control, Repo)
    |> Keyword.merge(
      name: nil,
      pool: DBConnection.ConnectionPool,
      pool_size: 1,
      migration_lock: false,
      # Saturated-host connects: the 4 s default queue drop ("request was dropped from queue after
      # 4000ms") and Postgrex's 15 s request budget both fired (W3-17 plan F-1/F-4/F-5).
      timeout: 30_000,
      queue_target: 5_000,
      queue_interval: 10_000
    )
  end

  @doc "Runs every boot-compiled migration into the dynamic repo's private schema."
  def migrate_all_up! do
    Ecto.Migrator.run(Repo, MigrationModules.migrations(), :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end
end
