defmodule SymmetryControl.Repo.Migrations.AddTaskAttemptIdentity do
  use Ecto.Migration

  def up do
    alter table(:tasks) do
      add :attempt_generation, :integer, null: false, default: 1
      add :waiting_transition_id, :binary_id
    end

    execute("""
    UPDATE tasks
    SET attempt_generation = CASE
      WHEN state = 'queued' THEN current_generation + 1
      ELSE GREATEST(current_generation, 1)
    END
    """)

    execute("""
    UPDATE tasks
    SET waiting_transition_id = (
      SELECT transition.transition_id
      FROM runs AS run
      JOIN run_transitions AS transition ON transition.run_id = run.id
      WHERE run.task_id = tasks.id
        AND run.generation = tasks.current_generation
        AND transition.state = 'waiting_for_input'
      ORDER BY transition.inserted_at DESC, transition.id DESC
      LIMIT 1
    )
    WHERE state = 'waiting_for_input'
    """)

    create constraint(:tasks, :tasks_attempt_generation_valid,
             check: "attempt_generation >= 1 AND attempt_generation >= current_generation"
           )

    create constraint(:tasks, :tasks_waiting_transition_matches_state,
             check:
               "(state = 'waiting_for_input' AND waiting_transition_id IS NOT NULL) OR " <>
                 "(state <> 'waiting_for_input' AND waiting_transition_id IS NULL)"
           )
  end

  def down do
    drop constraint(:tasks, :tasks_waiting_transition_matches_state)
    drop constraint(:tasks, :tasks_attempt_generation_valid)

    alter table(:tasks) do
      remove :waiting_transition_id
      remove :attempt_generation
    end
  end
end
