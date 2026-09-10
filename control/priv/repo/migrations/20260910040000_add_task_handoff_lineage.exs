defmodule SymmetryControl.Repo.Migrations.AddTaskHandoffLineage do
  use Ecto.Migration

  def up do
    alter table(:tasks) do
      add :handoff_source_run_id, :binary_id
    end

    execute("""
    ALTER TABLE tasks
    ADD CONSTRAINT tasks_handoff_source_run_id_fkey
    FOREIGN KEY (handoff_source_run_id)
    REFERENCES runs (id)
    ON DELETE RESTRICT
    """)

    create unique_index(:tasks, [:handoff_source_run_id],
             where: "handoff_source_run_id IS NOT NULL",
             name: :tasks_handoff_source_run_id_key
           )

    refuse_existing_handoff_tasks_without_lineage!()
    create_handoff_lineage_guard!()
    create_source_terminal_guards!()
  end

  def down do
    refuse_handoff_lineage_rollback!()

    execute("DROP TRIGGER tasks_handoff_lineage_guard ON tasks")
    execute("DROP FUNCTION goal_0006_validate_task_handoff_lineage()")
    execute("DROP TRIGGER runs_handoff_source_terminal_guard ON runs")
    execute("DROP FUNCTION goal_0006_freeze_handoff_source_run()")
    execute("DROP TRIGGER tasks_handoff_source_terminal_guard ON tasks")
    execute("DROP FUNCTION goal_0006_freeze_handoff_source_task()")
    drop index(:tasks, [:handoff_source_run_id], name: :tasks_handoff_source_run_id_key)
    drop constraint(:tasks, :tasks_handoff_source_run_id_fkey)

    alter table(:tasks) do
      remove :handoff_source_run_id
    end
  end

  defp refuse_existing_handoff_tasks_without_lineage! do
    execute("""
    DO $$
    BEGIN
      IF EXISTS (
        SELECT 1
        FROM tasks
        WHERE input ->> 'session_mode' = 'handoff'
      ) THEN
        RAISE EXCEPTION
          'cannot add task handoff lineage while existing handoff tasks lack a source run';
      END IF;
    END
    $$;
    """)
  end

  defp create_handoff_lineage_guard! do
    execute("""
    CREATE FUNCTION goal_0006_validate_task_handoff_lineage()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
      session_mode text;
      current_revision integer;
      source_task_id uuid;
      source_goal_id uuid;
      source_goal_revision integer;
      source_work_item_id uuid;
      source_task_purpose text;
      source_task_state text;
      source_run_state text;
      checked_source_task_id uuid;
      input_handoff_source_run_id text;
    BEGIN
      IF TG_OP = 'UPDATE'
         AND OLD.handoff_source_run_id IS DISTINCT FROM NEW.handoff_source_run_id THEN
        RAISE EXCEPTION 'goal_0006_handoff_lineage_immutable';
      END IF;

      session_mode := COALESCE(NEW.input ->> 'session_mode', 'fresh');
      input_handoff_source_run_id := NEW.input ->> 'handoff_source_run_id';

      IF session_mode <> 'handoff' THEN
        IF NEW.handoff_source_run_id IS NOT NULL
           OR NEW.input ? 'handoff_source_run_id' THEN
          RAISE EXCEPTION 'goal_0006_handoff_source_requires_handoff_mode';
        END IF;

        RETURN NEW;
      END IF;

      IF NEW.handoff_source_run_id IS NULL THEN
        RAISE EXCEPTION 'goal_0006_handoff_source_run_required';
      END IF;

      IF NOT NEW.input ? 'handoff_source_run_id'
         OR input_handoff_source_run_id IS DISTINCT FROM NEW.handoff_source_run_id::text THEN
        RAISE EXCEPTION 'goal_0006_handoff_source_run_mismatch';
      END IF;

      IF NEW.requested_session_id IS NOT NULL THEN
        RAISE EXCEPTION 'goal_0006_handoff_cannot_request_native_session';
      END IF;

      IF NEW.goal_id IS NULL OR NEW.goal_revision IS NULL THEN
        RAISE EXCEPTION 'goal_0006_handoff_requires_goal_task';
      END IF;

      IF NEW.purpose = 'plan' OR NEW.work_item_id IS NULL THEN
        RAISE EXCEPTION 'goal_0006_handoff_plan_unsupported';
      END IF;

      SELECT goal.current_revision
      INTO current_revision
      FROM goals AS goal
      WHERE goal.id = NEW.goal_id
      FOR UPDATE;

      IF NOT FOUND OR current_revision IS DISTINCT FROM NEW.goal_revision THEN
        RAISE EXCEPTION 'goal_0006_handoff_requires_current_goal_revision';
      END IF;

      IF NEW.work_item_id IS NOT NULL THEN
        PERFORM 1
        FROM work_items AS target_item
        WHERE target_item.id = NEW.work_item_id
          AND target_item.goal_id = NEW.goal_id
        FOR UPDATE;

        IF NOT FOUND THEN
          RAISE EXCEPTION 'goal_0006_handoff_target_work_item_ineligible';
        END IF;
      END IF;

      SELECT task_id
      INTO source_task_id
      FROM runs
      WHERE id = NEW.handoff_source_run_id;

      IF NOT FOUND OR source_task_id = NEW.id THEN
        RAISE EXCEPTION 'goal_0006_handoff_source_run_ineligible';
      END IF;

      SELECT
        source_task.goal_id,
        source_task.goal_revision,
        source_task.work_item_id,
        source_task.purpose,
        source_task.state
      INTO
        source_goal_id,
        source_goal_revision,
        source_work_item_id,
        source_task_purpose,
        source_task_state
      FROM tasks AS source_task
      WHERE source_task.id = source_task_id
      FOR UPDATE;

      IF NOT FOUND THEN
        RAISE EXCEPTION 'goal_0006_handoff_source_run_ineligible';
      END IF;

      SELECT task_id, state
      INTO checked_source_task_id, source_run_state
      FROM runs AS source_run
      WHERE source_run.id = NEW.handoff_source_run_id
      FOR UPDATE;

      IF NOT FOUND OR checked_source_task_id IS DISTINCT FROM source_task_id THEN
        RAISE EXCEPTION 'goal_0006_handoff_source_run_ineligible';
      END IF;

      IF source_run_state <> 'completed' OR source_task_state <> 'completed' THEN
        RAISE EXCEPTION 'goal_0006_handoff_source_run_ineligible';
      END IF;

      IF source_task_purpose = 'plan' OR source_work_item_id IS NULL THEN
        RAISE EXCEPTION 'goal_0006_handoff_source_run_ineligible';
      END IF;

      IF source_goal_id IS DISTINCT FROM NEW.goal_id THEN
        RAISE EXCEPTION 'goal_0006_handoff_source_goal_mismatch';
      END IF;

      IF source_goal_revision IS DISTINCT FROM NEW.goal_revision THEN
        RAISE EXCEPTION 'goal_0006_handoff_source_revision_mismatch';
      END IF;

      IF source_work_item_id IS DISTINCT FROM NEW.work_item_id THEN
        RAISE EXCEPTION 'goal_0006_handoff_source_work_item_mismatch';
      END IF;

      RETURN NEW;
    END;
    $$;
    """)

    execute("""
    CREATE TRIGGER tasks_handoff_lineage_guard
    BEFORE INSERT OR UPDATE OF goal_id, goal_revision, work_item_id, input,
      requested_session_id, handoff_source_run_id
    ON tasks
    FOR EACH ROW EXECUTE FUNCTION goal_0006_validate_task_handoff_lineage()
    """)
  end

  defp refuse_handoff_lineage_rollback! do
    execute("""
    LOCK TABLE tasks IN SHARE MODE;

    DO $$
    BEGIN
      IF EXISTS (
        SELECT 1
        FROM tasks
        WHERE handoff_source_run_id IS NOT NULL
      ) THEN
        RAISE EXCEPTION
          'cannot roll back task handoff lineage while source provenance exists';
      END IF;
    END
    $$;
    """)
  end

  defp create_source_terminal_guards! do
    execute("""
    CREATE FUNCTION goal_0006_freeze_handoff_source_run()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF EXISTS (
        SELECT 1
        FROM tasks AS target_task
        WHERE target_task.handoff_source_run_id = OLD.id
      ) AND (
        NEW.task_id IS DISTINCT FROM OLD.task_id
        OR NEW.generation IS DISTINCT FROM OLD.generation
        OR NEW.result IS DISTINCT FROM OLD.result
        OR NEW.state IS DISTINCT FROM 'completed'
      ) THEN
        RAISE EXCEPTION 'goal_0006_handoff_source_run_immutable';
      END IF;

      RETURN NEW;
    END;
    $$;
    """)

    execute("""
    CREATE TRIGGER runs_handoff_source_terminal_guard
    BEFORE UPDATE OF task_id, generation, result, state ON runs
    FOR EACH ROW EXECUTE FUNCTION goal_0006_freeze_handoff_source_run()
    """)

    execute("""
    CREATE FUNCTION goal_0006_freeze_handoff_source_task()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF EXISTS (
        SELECT 1
        FROM runs AS source_run
        JOIN tasks AS target_task ON target_task.handoff_source_run_id = source_run.id
        WHERE source_run.task_id = OLD.id
      ) AND (
        NEW.goal_id IS DISTINCT FROM OLD.goal_id
        OR NEW.goal_revision IS DISTINCT FROM OLD.goal_revision
        OR NEW.work_item_id IS DISTINCT FROM OLD.work_item_id
        OR NEW.purpose IS DISTINCT FROM OLD.purpose
        OR NEW.current_generation IS DISTINCT FROM OLD.current_generation
        OR NEW.result IS DISTINCT FROM OLD.result
        OR NEW.state IS DISTINCT FROM 'completed'
      ) THEN
        RAISE EXCEPTION 'goal_0006_handoff_source_task_immutable';
      END IF;

      RETURN NEW;
    END;
    $$;
    """)

    execute("""
    CREATE TRIGGER tasks_handoff_source_terminal_guard
    BEFORE UPDATE OF goal_id, goal_revision, work_item_id, purpose, current_generation, result, state ON tasks
    FOR EACH ROW EXECUTE FUNCTION goal_0006_freeze_handoff_source_task()
    """)
  end
end
