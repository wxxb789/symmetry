defmodule SymmetryControl.Repo.Migrations.AddPlanningTaskIdentityGuards do
  use Ecto.Migration

  @nonterminal_states ~w(queued assigned claimed running waiting_for_input paused cancelling)

  def up do
    refuse_legacy_planning_task_shape!()

    alter table(:context_snapshots) do
      modify :work_item_id, :binary_id, null: true
    end

    drop(constraint(:tasks, :tasks_goal_fields_all_or_none))
    drop(constraint(:tasks, :tasks_context_snapshot_ownership_fkey))

    create constraint(:tasks, :tasks_goal_fields_all_or_none, check: task_goal_fields_check())

    create unique_index(:context_snapshots, [:id, :goal_id, :goal_revision],
             name: :context_snapshots_id_goal_revision_key
           )

    execute("""
    ALTER TABLE tasks
    ADD CONSTRAINT tasks_context_snapshot_goal_revision_fkey
    FOREIGN KEY (context_snapshot_id, goal_id, goal_revision)
    REFERENCES context_snapshots (id, goal_id, goal_revision)
    MATCH FULL
    ON DELETE RESTRICT
    """)

    create unique_index(:tasks, [:goal_id],
             where: "purpose = 'plan' AND goal_id IS NOT NULL AND state IN (#{quoted_states()})",
             name: :tasks_one_active_plan_task_per_goal
           )

    create_task_context_snapshot_guard!()
    create_planning_context_snapshot_guard!()
  end

  def down do
    refuse_planning_history_rollback!()

    execute("DROP TRIGGER tasks_planning_context_snapshot_guard ON tasks")
    execute("DROP TRIGGER context_snapshots_planning_task_guard ON context_snapshots")
    execute("DROP TRIGGER tasks_context_snapshot_ownership_guard ON tasks")
    execute("DROP FUNCTION goal_0006_require_planning_context_snapshot_task()")
    execute("DROP FUNCTION goal_0006_require_planning_context_task()")
    execute("DROP FUNCTION goal_0006_validate_task_context_snapshot_ownership()")

    drop index(:tasks, [:goal_id], name: :tasks_one_active_plan_task_per_goal)
    drop constraint(:tasks, :tasks_context_snapshot_goal_revision_fkey)

    drop index(:context_snapshots, [:id, :goal_id, :goal_revision],
           name: :context_snapshots_id_goal_revision_key
         )

    drop constraint(:tasks, :tasks_goal_fields_all_or_none)

    alter table(:context_snapshots) do
      modify :work_item_id, :binary_id, null: false
    end

    create constraint(:tasks, :tasks_goal_fields_all_or_none,
             check: legacy_task_goal_fields_check()
           )

    execute("""
    ALTER TABLE tasks
    ADD CONSTRAINT tasks_context_snapshot_ownership_fkey
    FOREIGN KEY (context_snapshot_id, goal_id, goal_revision, work_item_id)
    REFERENCES context_snapshots (id, goal_id, goal_revision, work_item_id)
    ON DELETE RESTRICT
    """)
  end

  defp refuse_legacy_planning_task_shape! do
    execute("""
    DO $$
    BEGIN
      IF EXISTS (
        SELECT 1
        FROM tasks
        WHERE purpose = 'plan'
          AND (goal_id IS NULL OR work_item_id IS NOT NULL)
      ) THEN
        RAISE EXCEPTION 'cannot add planning task identity guards while existing planning tasks do not meet Goal-scoped ownership';
      END IF;
    END
    $$;
    """)

    execute("""
    CREATE FUNCTION goal_0006_require_planning_context_snapshot_task()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
      checked_snapshot_id uuid;
    BEGIN
      checked_snapshot_id := CASE WHEN TG_OP = 'DELETE' THEN OLD.id ELSE NEW.id END;

      IF EXISTS (
        SELECT 1
        FROM context_snapshots AS snapshot
        WHERE snapshot.id = checked_snapshot_id
          AND snapshot.work_item_id IS NULL
      ) AND NOT EXISTS (
        SELECT 1
        FROM tasks AS task
        JOIN context_snapshots AS snapshot ON snapshot.id = task.context_snapshot_id
        WHERE task.context_snapshot_id = checked_snapshot_id
          AND task.goal_id = snapshot.goal_id
          AND task.goal_revision = snapshot.goal_revision
          AND task.purpose = 'plan'
          AND task.work_item_id IS NULL
          AND task.validation_of_task_id IS NULL
      ) THEN
        RAISE EXCEPTION 'goal_0006_planning_context_snapshot_requires_plan_task';
      END IF;

      RETURN NULL;
    END;
    $$;
    """)
  end

  defp refuse_planning_history_rollback! do
    execute("LOCK TABLE context_snapshots, tasks IN SHARE MODE")

    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM context_snapshots WHERE work_item_id IS NULL)
         OR EXISTS (SELECT 1 FROM tasks WHERE goal_id IS NOT NULL AND purpose = 'plan') THEN
        RAISE EXCEPTION 'cannot roll back planning task identity guards while planning history exists';
      END IF;
    END
    $$;
    """)
  end

  defp create_task_context_snapshot_guard! do
    execute("""
    CREATE FUNCTION goal_0006_validate_task_context_snapshot_ownership()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
      snapshot_work_item_id uuid;
    BEGIN
      IF NEW.goal_id IS NULL THEN
        RETURN NEW;
      END IF;

      SELECT work_item_id
      INTO snapshot_work_item_id
      FROM context_snapshots
      WHERE id = NEW.context_snapshot_id
        AND goal_id = NEW.goal_id
        AND goal_revision = NEW.goal_revision;

      IF NOT FOUND THEN
        RAISE EXCEPTION 'goal_0006_task_context_snapshot_goal_revision_mismatch';
      END IF;

      IF NEW.purpose = 'plan' THEN
        IF snapshot_work_item_id IS NOT NULL THEN
          RAISE EXCEPTION 'goal_0006_planning_task_requires_goal_scoped_context_snapshot';
        END IF;
      ELSIF snapshot_work_item_id IS DISTINCT FROM NEW.work_item_id THEN
        RAISE EXCEPTION 'goal_0006_task_context_snapshot_work_item_mismatch';
      END IF;

      RETURN NEW;
    END;
    $$;
    """)

    execute("""
    CREATE TRIGGER tasks_context_snapshot_ownership_guard
    BEFORE INSERT OR UPDATE OF goal_id, goal_revision, work_item_id, context_snapshot_id, purpose
    ON tasks
    FOR EACH ROW EXECUTE FUNCTION goal_0006_validate_task_context_snapshot_ownership()
    """)
  end

  defp create_planning_context_snapshot_guard! do
    execute("""
    CREATE FUNCTION goal_0006_require_planning_context_task()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
      checked_snapshot_id uuid;
    BEGIN
      checked_snapshot_id := CASE
        WHEN TG_OP = 'DELETE' THEN OLD.context_snapshot_id
        ELSE NEW.context_snapshot_id
      END;

      IF checked_snapshot_id IS NULL THEN
        RETURN NULL;
      END IF;

      IF EXISTS (
        SELECT 1
        FROM context_snapshots AS snapshot
        WHERE snapshot.id = checked_snapshot_id
          AND snapshot.work_item_id IS NULL
      ) AND NOT EXISTS (
        SELECT 1
        FROM tasks AS task
        JOIN context_snapshots AS snapshot ON snapshot.id = task.context_snapshot_id
        WHERE task.context_snapshot_id = checked_snapshot_id
          AND task.goal_id = snapshot.goal_id
          AND task.goal_revision = snapshot.goal_revision
          AND task.purpose = 'plan'
          AND task.work_item_id IS NULL
          AND task.validation_of_task_id IS NULL
      ) THEN
        RAISE EXCEPTION 'goal_0006_planning_context_snapshot_requires_plan_task';
      END IF;

      RETURN NULL;
    END;
    $$;
    """)

    execute("""
    CREATE CONSTRAINT TRIGGER context_snapshots_planning_task_guard
    AFTER INSERT OR UPDATE ON context_snapshots
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION goal_0006_require_planning_context_snapshot_task()
    """)

    execute("""
    CREATE CONSTRAINT TRIGGER tasks_planning_context_snapshot_guard
    AFTER INSERT OR UPDATE OR DELETE ON tasks
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION goal_0006_require_planning_context_task()
    """)
  end

  defp task_goal_fields_check do
    """
    (
      goal_id IS NULL
      AND goal_revision IS NULL
      AND context_snapshot_id IS NULL
      AND admission_key IS NULL
      AND max_run_attempts IS NULL
      AND validation_of_task_id IS NULL
      AND purpose NOT IN ('validate', 'plan')
    ) OR (
      goal_id IS NOT NULL
      AND goal_revision IS NOT NULL
      AND context_snapshot_id IS NOT NULL
      AND admission_key IS NOT NULL
      AND max_run_attempts IS NOT NULL
      AND (
        (purpose = 'plan' AND work_item_id IS NULL AND validation_of_task_id IS NULL)
        OR (purpose = 'validate' AND work_item_id IS NOT NULL AND validation_of_task_id IS NOT NULL)
        OR (purpose NOT IN ('plan', 'validate') AND work_item_id IS NOT NULL AND validation_of_task_id IS NULL)
      )
    )
    """
  end

  defp legacy_task_goal_fields_check do
    """
    (
      goal_id IS NULL
      AND goal_revision IS NULL
      AND context_snapshot_id IS NULL
      AND admission_key IS NULL
      AND max_run_attempts IS NULL
      AND validation_of_task_id IS NULL
      AND purpose NOT IN ('validate', 'plan')
    ) OR (
      goal_id IS NOT NULL
      AND goal_revision IS NOT NULL
      AND work_item_id IS NOT NULL
      AND context_snapshot_id IS NOT NULL
      AND admission_key IS NOT NULL
      AND max_run_attempts IS NOT NULL
      AND ((purpose = 'validate' AND validation_of_task_id IS NOT NULL) OR (purpose <> 'validate' AND validation_of_task_id IS NULL))
    )
    """
  end

  defp quoted_states do
    @nonterminal_states
    |> Enum.map_join(", ", &"'#{&1}'")
  end
end
