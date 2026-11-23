defmodule SymmetryControl.Migrations.AddHistoryLookupIndexesTest do
  use SymmetryControl.MigrationCase, async: true

  alias SymmetryControl.Repo
  alias SymmetryControl.Repo.Migrations.AddHistoryLookupIndexes
  alias SymmetryControl.Repo.Migrations.AddMachineEnrollmentReplay
  alias SymmetryControl.Repo.Migrations.AddTaskCommandOwnership
  alias SymmetryControl.Repo.Migrations.CreateOrchestrationTables

  @migration_version 20_260_903_020_000

  @index_names [
    "run_events_run_id_inserted_at_id",
    "run_transitions_run_id_inserted_at_id",
    "commands_task_id_inserted_at_id",
    "run_events_waiting_for_input_run_sequence_id",
    "run_transitions_waiting_for_input_run_inserted_at_id"
  ]

  test "creates valid history and current-wait lookup indexes after a migration rerun" do
    migration_config = Map.new(AddHistoryLookupIndexes.__migration__())
    assert migration_config.disable_ddl_transaction
    refute migration_config.disable_migration_lock
    assert Application.fetch_env!(:symmetry_control, Repo)[:migration_lock] == :pg_advisory_lock

    schema = schema_name("history_lookup_indexes")
    create_schema!(schema)

    repo = start_schema_repo(schema)
    previous_dynamic_repo = Repo.put_dynamic_repo(repo)

    try do
      Ecto.Migrator.run(Repo, MigrationModules.verified_pairs!(migrations()), :up,
        all: true,
        log: false,
        migration_lock: false,
        dynamic_repo: repo
      )

      assert_indexes_valid!()

      Repo.query!("DELETE FROM schema_migrations WHERE version = $1", [@migration_version])

      Ecto.Migrator.run(Repo, MigrationModules.verified_pairs!(migrations()), :up,
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

      assert_indexes_valid!()
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

  defp assert_indexes_valid! do
    valid_indexes =
      Repo.query!("""
      SELECT index_class.relname, index_metadata.indisvalid
      FROM pg_index AS index_metadata
      JOIN pg_class AS index_class ON index_class.oid = index_metadata.indexrelid
      JOIN pg_namespace AS index_namespace ON index_namespace.oid = index_class.relnamespace
      WHERE index_namespace.nspname = current_schema()
      """)
      |> Map.fetch!(:rows)
      |> Map.new(fn [name, valid] -> {name, valid} end)

    for name <- @index_names do
      assert Map.fetch!(valid_indexes, name)
    end
  end

  defp migrations do
    [
      {20_260_902_000_000, CreateOrchestrationTables},
      {20_260_903_000_000, AddTaskCommandOwnership},
      {20_260_903_010_000, AddMachineEnrollmentReplay},
      {@migration_version, AddHistoryLookupIndexes}
    ]
  end
end
