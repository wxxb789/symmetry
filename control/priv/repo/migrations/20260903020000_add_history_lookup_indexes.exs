defmodule SymmetryControl.Repo.Migrations.AddHistoryLookupIndexes do
  use Ecto.Migration

  @disable_ddl_transaction true

  def up do
    drop_if_exists index(:run_events, [:run_id, :inserted_at, :id],
                     concurrently: true,
                     name: :run_events_run_id_inserted_at_id
                   )

    create index(:run_events, [:run_id, :inserted_at, :id],
             concurrently: true,
             name: :run_events_run_id_inserted_at_id
           )

    drop_if_exists index(:run_transitions, [:run_id, :inserted_at, :id],
                     concurrently: true,
                     name: :run_transitions_run_id_inserted_at_id
                   )

    create index(:run_transitions, [:run_id, :inserted_at, :id],
             concurrently: true,
             name: :run_transitions_run_id_inserted_at_id
           )

    drop_if_exists index(:commands, [:task_id, :inserted_at, :id],
                     concurrently: true,
                     name: :commands_task_id_inserted_at_id
                   )

    create index(:commands, [:task_id, :inserted_at, :id],
             concurrently: true,
             name: :commands_task_id_inserted_at_id
           )

    drop_if_exists index(:run_events, [:run_id, {:desc, :sequence}, {:desc, :id}],
                     concurrently: true,
                     where: "kind = 'waiting_for_input'",
                     name: :run_events_waiting_for_input_run_sequence_id
                   )

    create index(:run_events, [:run_id, {:desc, :sequence}, {:desc, :id}],
             concurrently: true,
             where: "kind = 'waiting_for_input'",
             name: :run_events_waiting_for_input_run_sequence_id
           )

    drop_if_exists index(:run_transitions, [:run_id, {:desc, :inserted_at}, {:desc, :id}],
                     concurrently: true,
                     where: "state = 'waiting_for_input'",
                     name: :run_transitions_waiting_for_input_run_inserted_at_id
                   )

    create index(:run_transitions, [:run_id, {:desc, :inserted_at}, {:desc, :id}],
             concurrently: true,
             where: "state = 'waiting_for_input'",
             name: :run_transitions_waiting_for_input_run_inserted_at_id
           )
  end

  def down do
    drop_if_exists index(:run_events, [:run_id, :inserted_at, :id],
                     concurrently: true,
                     name: :run_events_run_id_inserted_at_id
                   )

    drop_if_exists index(:run_transitions, [:run_id, :inserted_at, :id],
                     concurrently: true,
                     name: :run_transitions_run_id_inserted_at_id
                   )

    drop_if_exists index(:commands, [:task_id, :inserted_at, :id],
                     concurrently: true,
                     name: :commands_task_id_inserted_at_id
                   )

    drop_if_exists index(:run_events, [:run_id, {:desc, :sequence}, {:desc, :id}],
                     concurrently: true,
                     where: "kind = 'waiting_for_input'",
                     name: :run_events_waiting_for_input_run_sequence_id
                   )

    drop_if_exists index(:run_transitions, [:run_id, {:desc, :inserted_at}, {:desc, :id}],
                     concurrently: true,
                     where: "state = 'waiting_for_input'",
                     name: :run_transitions_waiting_for_input_run_inserted_at_id
                   )
  end
end
