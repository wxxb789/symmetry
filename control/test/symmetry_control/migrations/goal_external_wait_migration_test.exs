defmodule SymmetryControl.Migrations.GoalExternalWaitMigrationTest do
  use ExUnit.Case, async: false

  alias Ecto.Adapters.SQL.Sandbox
  alias SymmetryControl.Repo
  alias SymmetryControl.Repo.Migrations.AddGoalExternalWaits
  alias SymmetryControl.Repo.Migrations.AddGoalExternalWaitUnsupportedState

  @migration_version 20_260_909_060_000
  @unsupported_migration_version 20_260_909_100_000

  test "creates the external wait table, identity fences and reconciliation indexes" do
    with_schema(fn ->
      migrate_all_up!()

      assert %{rows: [[@migration_version]]} =
               Repo.query!("SELECT version FROM schema_migrations WHERE version = $1", [
                 @migration_version
               ])

      assert %{rows: rows} =
               Repo.query!("""
               SELECT column_name, is_nullable
               FROM information_schema.columns
               WHERE table_name = 'goal_external_waits'
               ORDER BY ordinal_position
               """)

      assert {"goal_id", "NO"} in rows
      assert {"goal_revision", "NO"} in rows
      assert {"work_item_id", "NO"} in rows
      assert {"task_id", "NO"} in rows
      assert {"run_id", "NO"} in rows
      assert {"run_generation", "NO"} in rows
      assert {"source_ref", "NO"} in rows
      assert {"result_id", "NO"} in rows
      assert {"result", "NO"} in rows
      assert {"resource_id", "NO"} in rows
      assert {"external_ref", "NO"} in rows
      assert {"subject", "NO"} in rows
      assert {"subject_hash", "NO"} in rows
      assert {"next_check_at", "YES"} in rows
      assert {"state", "NO"} in rows
      assert {"check_seq", "NO"} in rows
      assert {"receipt_event_id", "YES"} in rows

      assert %{rows: [[1]]} =
               Repo.query!("""
               SELECT COUNT(*)
               FROM pg_indexes
               WHERE tablename = 'goal_external_waits'
                 AND indexname = 'goal_external_waits_current_work_item_key'
               """)

      assert %{rows: [[1]]} =
               Repo.query!("""
               SELECT COUNT(*)
               FROM pg_trigger
               WHERE tgrelid = 'goal_external_waits'::regclass
                 AND tgname = 'goal_external_waits_history_guard'
               """)

      assert %{rows: [[1]]} =
               Repo.query!("""
               SELECT COUNT(*)
               FROM pg_constraint
               WHERE conrelid = 'goal_external_waits'::regclass
                 AND conname = 'goal_external_waits_run_identity_fkey'
               """)
    end)
  end

  test "the migration down removes only the new external-wait structures" do
    with_schema(fn ->
      migrate_all_up!()

      Ecto.Migrator.run(Repo, [{@migration_version, AddGoalExternalWaits}], :down,
        step: 1,
        log: false,
        migration_lock: false,
        dynamic_repo: Repo.get_dynamic_repo()
      )

      assert %{rows: []} =
               Repo.query!("""
               SELECT 1
               FROM information_schema.tables
               WHERE table_name = 'goal_external_waits'
               """)

      assert %{rows: []} =
               Repo.query!("""
               SELECT 1
               FROM pg_indexes
               WHERE indexname IN ('runs_id_task_id_generation_key', 'goal_events_id_goal_id_key')
               """)
    end)
  end

  test "the unsupported-state migration makes unsupported terminal and reversible without history" do
    with_schema(fn ->
      migrate_all_up!()

      assert %{rows: [[definition]]} =
               Repo.query!("""
               SELECT pg_get_constraintdef(oid)
               FROM pg_constraint
               WHERE conrelid = 'goal_external_waits'::regclass
                 AND conname = 'goal_external_waits_state_check'
               """)

      assert definition =~ "unsupported"

      assert %{rows: [[default]]} =
               Repo.query!("""
               SELECT column_default
               FROM information_schema.columns
               WHERE table_name = 'goal_external_waits' AND column_name = 'state'
               """)

      assert default =~ "unsupported"

      Ecto.Migrator.run(
        Repo,
        [{@unsupported_migration_version, AddGoalExternalWaitUnsupportedState}],
        :down,
        step: 1,
        log: false,
        migration_lock: false,
        dynamic_repo: Repo.get_dynamic_repo()
      )

      assert %{rows: [[prior_definition]]} =
               Repo.query!("""
               SELECT pg_get_constraintdef(oid)
               FROM pg_constraint
               WHERE conrelid = 'goal_external_waits'::regclass
                 AND conname = 'goal_external_waits_state_check'
               """)

      refute prior_definition =~ "unsupported"

      assert %{rows: [[prior_default]]} =
               Repo.query!("""
               SELECT column_default
               FROM information_schema.columns
               WHERE table_name = 'goal_external_waits' AND column_name = 'state'
               """)

      assert prior_default =~ "waiting"

      Ecto.Migrator.run(
        Repo,
        [{@unsupported_migration_version, AddGoalExternalWaitUnsupportedState}],
        :up,
        step: 1,
        log: false,
        migration_lock: false,
        dynamic_repo: Repo.get_dynamic_repo()
      )
    end)
  end

  defp with_schema(test) do
    schema = "goal_external_wait_migration_#{System.unique_integer([:positive])}"
    create_schema!(schema)
    repo = start_schema_repo(schema)
    previous_dynamic_repo = Repo.put_dynamic_repo(repo)

    try do
      test.()
    after
      Repo.put_dynamic_repo(previous_dynamic_repo)
      GenServer.stop(repo)
      drop_schema!(schema)
    end
  end

  defp migrate_all_up! do
    Ecto.Migrator.run(Repo, migrations_path(), :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrations_path do
    Path.expand("../../../priv/repo/migrations", __DIR__)
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
end
