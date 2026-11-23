defmodule SymmetryControl.MigrationCase do
  @moduledoc """
  Isolated-schema harness of the migration tests: every test migrates through its own Repo into a
  private schema, so partially-migrated states never touch the shared test schema.
  """

  use ExUnit.CaseTemplate

  import ExUnit.Callbacks, only: [on_exit: 1]

  alias SymmetryControl.MigrationModules
  alias SymmetryControl.Repo

  using do
    quote do
      alias SymmetryControl.MigrationModules

      import SymmetryControl.MigrationCase,
        only: [
          with_schema: 2,
          schema_name: 1,
          start_schema_repo: 1,
          create_schema!: 1,
          drop_schema!: 1,
          migrate_all_up!: 0,
          run_private_query!: 2
        ]
    end
  end

  @doc """
  Runs `test` against a unique private schema. No teardown call here: ExUnit kills the test process
  before `on_exit`, so a stop could only mask the test's own error.
  """
  def with_schema(schema_base, test) do
    schema = schema_name(schema_base)

    create_schema!(schema)
    repo = start_schema_repo(schema)

    previous_dynamic_repo = Repo.put_dynamic_repo(repo)

    try do
      test.()
    after
      Repo.put_dynamic_repo(previous_dynamic_repo)
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
  Registers the `on_exit` drop before the DDL: ExUnit's kill path runs it even when it skips every
  `after` block, so a timed-out test cannot leak its schema.
  """
  def create_schema!(schema) do
    on_exit(fn -> drop_schema!(schema) end)
    run_private_query!("CREATE SCHEMA #{schema}", [])
  end

  @doc "Drops `schema` and everything in it, outside the shared sandbox pool."
  def drop_schema!(schema) do
    run_private_query!("DROP SCHEMA IF EXISTS #{schema} CASCADE", [])
  end

  @doc """
  Runs `statement` on a fresh private Repo with a bounded `:normal` stop: the shape `on_exit` cleanup
  needs, since the suite Repo has no sandbox ownership and the dynamic Repo is already terminating.
  """
  def run_private_query!(statement, params) do
    {:ok, repo} = Repo.start_link(private_repo_config())

    try do
      Ecto.Adapters.SQL.query!(repo, statement, params)
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
