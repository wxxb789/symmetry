defmodule SymmetryControl.Repo.Migrations.AddTaskRuntimePolicySnapshot do
  use Ecto.Migration

  def up do
    execute("LOCK TABLE tasks IN ACCESS EXCLUSIVE MODE")

    alter table(:tasks) do
      add :allowed_runtime_ids, {:array, :binary_id}, null: true
    end

    execute("""
    DO $$
    BEGIN
      IF EXISTS (
        SELECT 1
        FROM tasks AS task
        LEFT JOIN goal_revisions AS revision
          ON revision.goal_id = task.goal_id
         AND revision.revision = task.goal_revision
        WHERE task.goal_id IS NOT NULL
          AND (
            revision.goal_id IS NULL
            OR jsonb_typeof(revision.execution_policy -> 'allowed_runtime_ids') IS DISTINCT FROM 'array'
        OR (CASE
              WHEN jsonb_typeof(revision.execution_policy -> 'allowed_runtime_ids') = 'array'
                THEN jsonb_array_length(revision.execution_policy -> 'allowed_runtime_ids')
              ELSE -1
            END) > 256
            OR EXISTS (
                 SELECT 1
                 FROM jsonb_array_elements_text(
                   CASE
                     WHEN jsonb_typeof(revision.execution_policy -> 'allowed_runtime_ids') = 'array'
                       THEN revision.execution_policy -> 'allowed_runtime_ids'
                     ELSE '[]'::jsonb
                   END
                 ) AS member(value)
                 WHERE member.value !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
               )
            OR (
                 SELECT COUNT(DISTINCT member.value)
                 FROM jsonb_array_elements_text(
                   CASE
                     WHEN jsonb_typeof(revision.execution_policy -> 'allowed_runtime_ids') = 'array'
                       THEN revision.execution_policy -> 'allowed_runtime_ids'
                     ELSE '[]'::jsonb
                   END
                 ) AS member(value)
           ) <> (CASE
                   WHEN jsonb_typeof(revision.execution_policy -> 'allowed_runtime_ids') = 'array'
                     THEN jsonb_array_length(revision.execution_policy -> 'allowed_runtime_ids')
                   ELSE -1
                 END)
          )
      ) THEN
        RAISE EXCEPTION 'goal_0006_task_runtime_policy_source_invalid';
      END IF;
    END
    $$;
    """)

    execute("""
    UPDATE tasks AS task
    SET allowed_runtime_ids = ARRAY(
      SELECT member.value::uuid
      FROM jsonb_array_elements_text(revision.execution_policy -> 'allowed_runtime_ids')
        WITH ORDINALITY AS member(value, ordinality)
      ORDER BY member.ordinality
    )::uuid[]
    FROM goal_revisions AS revision
    WHERE revision.goal_id = task.goal_id
      AND revision.revision = task.goal_revision
      AND task.goal_id IS NOT NULL
    """)

    create constraint(:tasks, :tasks_allowed_runtime_ids_membership_check,
             check:
               "(goal_id IS NULL AND allowed_runtime_ids IS NULL) OR (goal_id IS NOT NULL AND allowed_runtime_ids IS NOT NULL)"
           )

    create constraint(:tasks, :tasks_allowed_runtime_ids_dimensions_check,
             check:
               "array_ndims(allowed_runtime_ids) IS NULL OR array_ndims(allowed_runtime_ids) = 1"
           )

    execute("""
    CREATE FUNCTION goal_0006_validate_task_runtime_policy()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
      source_policy jsonb;
      source_runtime_ids uuid[];
      source_count integer;
      snapshot_count integer;
    BEGIN
      IF TG_OP = 'UPDATE' AND (
           NEW.goal_id IS DISTINCT FROM OLD.goal_id
        OR NEW.goal_revision IS DISTINCT FROM OLD.goal_revision
      ) THEN
        RAISE EXCEPTION 'goal_0006_task_runtime_policy_membership_immutable';
      END IF;

      IF NEW.goal_id IS NULL THEN
        IF NEW.allowed_runtime_ids IS NOT NULL THEN
          RAISE EXCEPTION 'goal_0006_task_runtime_policy_requires_goal';
        END IF;

        RETURN NEW;
      END IF;

      IF NEW.allowed_runtime_ids IS NULL THEN
        RAISE EXCEPTION 'goal_0006_task_runtime_policy_required';
      END IF;

      IF TG_OP = 'UPDATE' AND
         NEW.allowed_runtime_ids IS DISTINCT FROM OLD.allowed_runtime_ids THEN
        RAISE EXCEPTION 'goal_0006_task_runtime_policy_snapshot_immutable';
      END IF;

      SELECT revision.execution_policy -> 'allowed_runtime_ids'
      INTO source_policy
      FROM goal_revisions AS revision
      WHERE revision.goal_id = NEW.goal_id
        AND revision.revision = NEW.goal_revision;

      IF source_policy IS NULL
         OR jsonb_typeof(source_policy) IS DISTINCT FROM 'array'
         OR (CASE
               WHEN jsonb_typeof(source_policy) = 'array' THEN jsonb_array_length(source_policy)
               ELSE -1
             END) > 256
         OR EXISTS (
              SELECT 1
              FROM jsonb_array_elements_text(
                CASE WHEN jsonb_typeof(source_policy) = 'array'
                  THEN source_policy
                  ELSE '[]'::jsonb
                END
              ) AS member(value)
              WHERE member.value !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
            )
         OR (
              SELECT COUNT(DISTINCT member.value)
              FROM jsonb_array_elements_text(
                CASE WHEN jsonb_typeof(source_policy) = 'array'
                  THEN source_policy
                  ELSE '[]'::jsonb
                END
              ) AS member(value)
            ) <> (CASE
                    WHEN jsonb_typeof(source_policy) = 'array'
                      THEN jsonb_array_length(source_policy)
                    ELSE -1
                  END) THEN
        RAISE EXCEPTION 'goal_0006_task_runtime_policy_source_invalid';
      END IF;

      SELECT ARRAY(
               SELECT member.value::uuid
               FROM jsonb_array_elements_text(source_policy) AS member(value)
             )::uuid[]
      INTO source_runtime_ids;

      source_count := cardinality(source_runtime_ids);
      snapshot_count := cardinality(NEW.allowed_runtime_ids);

      IF source_count <> snapshot_count
         OR (
              SELECT COUNT(DISTINCT snapshot.runtime_id)
              FROM unnest(NEW.allowed_runtime_ids) AS snapshot(runtime_id)
            ) <> snapshot_count
         OR NOT (source_runtime_ids <@ NEW.allowed_runtime_ids)
         OR NOT (NEW.allowed_runtime_ids <@ source_runtime_ids) THEN
        RAISE EXCEPTION 'goal_0006_task_runtime_policy_source_mismatch';
      END IF;

      RETURN NEW;
    END;
    $$;
    """)

    execute("""
    CREATE TRIGGER tasks_runtime_policy_snapshot_guard
    BEFORE INSERT OR UPDATE OF goal_id, goal_revision, allowed_runtime_ids
    ON tasks
    FOR EACH ROW EXECUTE FUNCTION goal_0006_validate_task_runtime_policy()
    """)
  end

  def down do
    execute("LOCK TABLE tasks IN ACCESS EXCLUSIVE MODE")

    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM tasks WHERE allowed_runtime_ids IS NOT NULL) THEN
        RAISE EXCEPTION
          'cannot roll back task runtime policy snapshots while Goal task history exists';
      END IF;
    END
    $$;
    """)

    execute("DROP TRIGGER tasks_runtime_policy_snapshot_guard ON tasks")
    execute("DROP FUNCTION goal_0006_validate_task_runtime_policy()")
    drop constraint(:tasks, :tasks_allowed_runtime_ids_dimensions_check)
    drop constraint(:tasks, :tasks_allowed_runtime_ids_membership_check)

    alter table(:tasks) do
      remove :allowed_runtime_ids
    end
  end
end
