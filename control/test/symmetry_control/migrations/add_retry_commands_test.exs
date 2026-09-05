defmodule SymmetryControl.Migrations.AddRetryCommandsTest do
  use ExUnit.Case, async: false

  alias Ecto.Adapters.SQL.Sandbox
  alias SymmetryControl.Repo
  alias SymmetryControl.Repo.Migrations.AddRetryCommands
  alias SymmetryControl.Repo.Migrations.AddTaskCommandOwnership
  alias SymmetryControl.Repo.Migrations.CreateOrchestrationTables

  @retry_version 20_260_904_030_000

  test "rolls back retry support when no retry history exists" do
    with_schema(fn ->
      task_id = insert_task!()
      insert_command!(task_id, "cancel", "cancel-command")
      insert_command!(task_id, "provide_input", "input-command")

      migrate_down!()

      assert %{rows: [["cancel"], ["provide_input"]]} =
               Repo.query!("SELECT kind FROM commands ORDER BY kind")

      assert_raise Postgrex.Error, ~r/commands_kind_check/i, fn ->
        insert_command!(task_id, "retry", "retry-after-down")
      end

      assert %{rows: []} =
               Repo.query!("SELECT version FROM schema_migrations WHERE version = $1", [
                 @retry_version
               ])
    end)
  end

  test "refuses rollback and preserves durable retry history" do
    with_schema(fn ->
      task_id = insert_task!()
      retry_id = insert_command!(task_id, "retry", "durable-retry")

      assert_raise Postgrex.Error,
                   ~r/cannot roll back retry command support while retry history exists/i,
                   &migrate_down!/0

      assert %{rows: [[^retry_id, "retry"]]} =
               Repo.query!("SELECT id, kind FROM commands WHERE id = $1", [retry_id])

      assert %{rows: [[definition]]} =
               Repo.query!("""
               SELECT pg_get_constraintdef(constraint_row.oid)
               FROM pg_constraint AS constraint_row
               JOIN pg_class AS relation ON relation.oid = constraint_row.conrelid
               JOIN pg_namespace AS namespace ON namespace.oid = relation.relnamespace
               WHERE namespace.nspname = current_schema()
                 AND relation.relname = 'commands'
                 AND constraint_row.conname = 'commands_kind_check'
               """)

      assert definition =~ "retry"

      assert %{rows: [[@retry_version]]} =
               Repo.query!("SELECT version FROM schema_migrations WHERE version = $1", [
                 @retry_version
               ])

      assert [] == migrate_up!()

      assert %{rows: [[^retry_id]]} =
               Repo.query!("SELECT id FROM commands WHERE id = $1", [retry_id])
    end)
  end

  defp with_schema(test) do
    load_migration_modules!()
    schema = "goal2_retry_#{System.unique_integer([:positive])}"
    create_schema!(schema)
    repo = start_schema_repo(schema)
    previous_dynamic_repo = Repo.put_dynamic_repo(repo)

    try do
      migrate_up!()
      test.()
    after
      Repo.put_dynamic_repo(previous_dynamic_repo)
      GenServer.stop(repo)
      drop_schema!(schema)
    end
  end

  defp migrate_up! do
    Ecto.Migrator.run(Repo, migrations(), :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_down! do
    Ecto.Migrator.run(Repo, migrations(), :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp insert_task! do
    task_id = uuid()

    Repo.query!(
      """
      INSERT INTO tasks (
        id, idempotency_key, request_hash, goal, agent_profile, workspace, input, state,
        current_generation, inserted_at, updated_at
      )
      VALUES ($1, $2, $3, 'Retry migration test', 'codex', 'primary', '{}'::jsonb,
              'cancelled', 0, now(), now())
      """,
      [task_id, "task-#{System.unique_integer([:positive])}", <<1>>]
    )

    task_id
  end

  defp insert_command!(task_id, kind, idempotency_key) do
    command_id = uuid()

    Repo.query!(
      """
      INSERT INTO commands (
        id, task_id, run_id, generation, kind, payload, idempotency_key, request_hash, state,
        applied_at, inserted_at, updated_at
      )
      VALUES ($1, $2, NULL, NULL, $3, '{}'::jsonb, $4, $5, 'applied', now(), now(), now())
      """,
      [command_id, task_id, kind, idempotency_key, <<2>>]
    )

    command_id
  end

  defp migrations do
    [
      {20_260_902_000_000, CreateOrchestrationTables},
      {20_260_903_000_000, AddTaskCommandOwnership},
      {@retry_version, AddRetryCommands}
    ]
  end

  defp load_migration_modules! do
    migrations = [
      {CreateOrchestrationTables, "20260902000000_create_orchestration_tables.exs"},
      {AddTaskCommandOwnership, "20260903000000_add_task_command_ownership.exs"},
      {AddRetryCommands, "20260904030000_add_retry_commands.exs"}
    ]

    Enum.each(migrations, fn {module, filename} ->
      unless function_exported?(module, :__migration__, 0) do
        filename
        |> then(&Path.expand("../../../priv/repo/migrations/#{&1}", __DIR__))
        |> Code.compile_file()
      end
    end)
  end

  defp start_schema_repo(schema) do
    config =
      Application.fetch_env!(:symmetry_control, Repo)
      |> Keyword.merge(name: nil, pool: DBConnection.ConnectionPool, pool_size: 1)
      |> Keyword.put(:parameters, search_path: schema)

    {:ok, repo} = Repo.start_link(config)
    repo
  end

  defp create_schema!(schema) do
    Sandbox.unboxed_run(Repo, fn -> Repo.query!("CREATE SCHEMA #{schema}") end)
  end

  defp drop_schema!(schema) do
    Sandbox.unboxed_run(Repo, fn -> Repo.query!("DROP SCHEMA IF EXISTS #{schema} CASCADE") end)
  end

  defp uuid, do: Ecto.UUID.bingenerate()
end
