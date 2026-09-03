defmodule SymmetryControl.Repo.Migrations.AddTaskCommandOwnership do
  use Ecto.Migration

  def up do
    alter table(:tasks) do
      modify :input, :map, null: true, default: nil
    end

    alter table(:commands) do
      add :task_id, references(:tasks, type: :binary_id, on_delete: :delete_all)
      add :state, :string, null: false, default: "pending"
      add :applied_at, :utc_datetime_usec
    end

    execute """
    UPDATE commands
    SET task_id = runs.task_id
    FROM runs
    WHERE commands.run_id = runs.id
    """

    execute """
    UPDATE commands
    SET state = 'acknowledged'
    WHERE acknowledged_at IS NOT NULL
    """

    execute """
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM commands WHERE task_id IS NULL) THEN
        RAISE EXCEPTION 'commands.task_id backfill failed';
      END IF;
    END
    $$;
    """

    execute "ALTER TABLE commands ALTER COLUMN task_id SET NOT NULL"
    execute "ALTER TABLE commands ALTER COLUMN run_id DROP NOT NULL"
    execute "ALTER TABLE commands ALTER COLUMN generation DROP NOT NULL"

    drop unique_index(:commands, [:idempotency_key])
    drop constraint(:commands, :commands_generation_positive)

    create unique_index(:commands, [:task_id, :idempotency_key])

    create constraint(:commands, :commands_run_generation_pair,
             check:
               "(run_id IS NULL AND generation IS NULL) OR (run_id IS NOT NULL AND generation IS NOT NULL AND generation > 0)"
           )

    create constraint(:commands, :commands_state_check,
             check: "state IN ('pending', 'applied', 'acknowledged')"
           )
  end
end
