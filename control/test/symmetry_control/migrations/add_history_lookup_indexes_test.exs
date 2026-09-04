defmodule SymmetryControl.Migrations.AddHistoryLookupIndexesTest do
  use ExUnit.Case, async: false

  alias Ecto.Adapters.SQL.Sandbox
  alias SymmetryControl.Repo
  alias SymmetryControl.Repo.Migrations.AddHistoryLookupIndexes
  alias SymmetryControl.Repo.Migrations.AddMachineEnrollmentReplay
  alias SymmetryControl.Repo.Migrations.AddTaskCommandOwnership
  alias SymmetryControl.Repo.Migrations.CreateOrchestrationTables

  test "creates history and current-wait lookup indexes" do
    load_migration_modules!()

    migration_config = Map.new(AddHistoryLookupIndexes.__migration__())
    assert migration_config.disable_ddl_transaction
    assert migration_config.disable_migration_lock

    schema = "history_lookup_indexes_#{System.unique_integer([:positive])}"
    create_schema!(schema)

    repo = start_schema_repo(schema)
    previous_dynamic_repo = Repo.put_dynamic_repo(repo)

    try do
      Ecto.Migrator.run(Repo, migrations(), :up,
        all: true,
        log: false,
        migration_lock: false,
        dynamic_repo: repo
      )

      Repo.query!("DELETE FROM schema_migrations WHERE version = $1", [20_260_903_020_000])

      Ecto.Migrator.run(Repo, migrations(), :up,
        all: true,
        log: false,
        migration_lock: false,
        dynamic_repo: repo
      )

      indexes = indexes_by_name()

      assert_index!(indexes, "run_events_run_id_inserted_at_id", "(run_id, inserted_at, id)")

      assert_index!(
        indexes,
        "run_transitions_run_id_inserted_at_id",
        "(run_id, inserted_at, id)"
      )

      assert_index!(indexes, "commands_task_id_inserted_at_id", "(task_id, inserted_at, id)")

      assert_index!(
        indexes,
        "run_events_waiting_for_input_run_sequence_id",
        "(run_id, sequence DESC, id DESC)",
        ~r/kind.*waiting_for_input/
      )

      assert_index!(
        indexes,
        "run_transitions_waiting_for_input_run_inserted_at_id",
        "(run_id, inserted_at DESC, id DESC)",
        ~r/state.*waiting_for_input/
      )
    after
      Repo.put_dynamic_repo(previous_dynamic_repo)
      GenServer.stop(repo)
      drop_schema!(schema)
    end
  end

  defp indexes_by_name do
    %{rows: rows} =
      Repo.query!(
        "SELECT indexname, indexdef FROM pg_indexes WHERE schemaname = current_schema()"
      )

    Map.new(rows, fn [name, definition] -> {name, String.replace(definition, "\"", "")} end)
  end

  defp assert_index!(indexes, name, keys, predicate \\ nil) do
    definition = Map.fetch!(indexes, name)

    assert definition =~ keys

    if predicate do
      assert definition =~ predicate
    end
  end

  defp migrations do
    [
      {20_260_902_000_000, CreateOrchestrationTables},
      {20_260_903_000_000, AddTaskCommandOwnership},
      {20_260_903_010_000, AddMachineEnrollmentReplay},
      {20_260_903_020_000, AddHistoryLookupIndexes}
    ]
  end

  defp start_schema_repo(schema) do
    config =
      Application.fetch_env!(:symmetry_control, Repo)
      |> Keyword.merge(name: nil, pool: DBConnection.ConnectionPool, pool_size: 1)
      |> Keyword.put(:parameters, search_path: schema)

    {:ok, repo} = Repo.start_link(config)
    repo
  end

  defp load_migration_modules! do
    migrations = [
      {CreateOrchestrationTables, "20260902000000_create_orchestration_tables.exs"},
      {AddTaskCommandOwnership, "20260903000000_add_task_command_ownership.exs"},
      {AddMachineEnrollmentReplay, "20260903010000_add_machine_enrollment_replay.exs"},
      {AddHistoryLookupIndexes, "20260903020000_add_history_lookup_indexes.exs"}
    ]

    Enum.each(migrations, fn {module, filename} ->
      unless function_exported?(module, :__migration__, 0) do
        filename
        |> then(&Path.expand("../../../priv/repo/migrations/#{&1}", __DIR__))
        |> Code.compile_file()
      end
    end)
  end

  defp create_schema!(schema) do
    Sandbox.unboxed_run(Repo, fn ->
      Repo.query!("CREATE SCHEMA #{schema}")
    end)
  end

  defp drop_schema!(schema) do
    Sandbox.unboxed_run(Repo, fn ->
      Repo.query!("DROP SCHEMA IF EXISTS #{schema} CASCADE")
    end)
  end
end
