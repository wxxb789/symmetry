defmodule SymmetryControl.Repo.Migrations.AddSupervisoryControls do
  use Ecto.Migration

  def up do
    replace_constraints(true)

    create unique_index(:commands, [:run_id],
             where: "kind IN ('pause', 'resume') AND state IN ('pending', 'applied')",
             name: :commands_one_pending_supervisory_transition_per_run
           )
  end

  def down do
    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM commands WHERE kind IN ('guidance', 'pause', 'resume'))
         OR EXISTS (SELECT 1 FROM runs WHERE state = 'paused') THEN
        RAISE EXCEPTION 'cannot roll back supervisory control while its history exists';
      END IF;
    END
    $$;
    """)

    drop index(:commands, [:run_id], name: :commands_one_pending_supervisory_transition_per_run)
    replace_constraints(false)
  end

  defp replace_constraints(supervised?) do
    paused = if supervised?, do: ", 'paused'", else: ""
    controls = if supervised?, do: ", 'guidance', 'pause', 'resume'", else: ""

    drop constraint(:tasks, :tasks_state_check)
    drop constraint(:runs, :runs_state_check)
    drop constraint(:commands, :commands_kind_check)
    drop index(:runs, [:task_id], name: :runs_one_capacity_bearing_run_per_task)

    create constraint(:tasks, :tasks_state_check,
             check:
               "state IN ('queued', 'assigned', 'claimed', 'running', 'waiting_for_input', 'cancelling', 'completed', 'failed', 'cancelled'#{paused})"
           )

    create constraint(:runs, :runs_state_check,
             check:
               "state IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'cancelling', 'completed', 'failed', 'cancelled', 'expired'#{paused})"
           )

    create constraint(:commands, :commands_kind_check,
             check: "kind IN ('cancel', 'provide_input', 'retry'#{controls})"
           )

    create unique_index(:runs, [:task_id],
             where:
               "state IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'cancelling'#{paused})",
             name: :runs_one_capacity_bearing_run_per_task
           )
  end
end
