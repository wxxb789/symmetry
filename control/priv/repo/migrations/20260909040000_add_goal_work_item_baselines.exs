defmodule SymmetryControl.Repo.Migrations.AddGoalWorkItemBaselines do
  use Ecto.Migration

  def up do
    alter table(:work_items) do
      add :baseline_subject, :map
      add :baseline_dependency_id, :binary_id
    end

    create unique_index(:work_items, [:id, :goal_id, :admitted_revision],
             name: :work_items_id_goal_revision_key
           )

    create index(:work_items, [:baseline_dependency_id])

    create constraint(:work_items, :work_items_baseline_fields_check,
             check: """
             (
               goal_id IS NULL
               AND admitted_revision IS NULL
               AND baseline_subject IS NULL
               AND baseline_dependency_id IS NULL
             ) OR (
               goal_id IS NOT NULL
               AND admitted_revision IS NOT NULL
               AND (
                 (baseline_subject IS NULL AND baseline_dependency_id IS NULL)
                 OR
                 (baseline_subject IS NOT NULL AND baseline_dependency_id IS NULL)
                 OR
                 (baseline_subject IS NULL AND baseline_dependency_id IS NOT NULL)
               )
             )
             """
           )

    create constraint(:work_items, :work_items_baseline_subject_shape_check,
             check: """
             baseline_subject IS NULL OR (
               jsonb_typeof(baseline_subject) = 'object'
               AND baseline_subject ?& ARRAY['resource_id', 'commit', 'tree_digest']
               AND baseline_subject - 'resource_id' - 'commit' - 'tree_digest' = '{}'::jsonb
               AND repository_resource_id IS NOT NULL
               AND jsonb_typeof(baseline_subject -> 'resource_id') = 'string'
               AND jsonb_typeof(baseline_subject -> 'commit') = 'string'
               AND jsonb_typeof(baseline_subject -> 'tree_digest') = 'string'
               AND baseline_subject ->> 'resource_id' IS NOT NULL
               AND baseline_subject ->> 'commit' IS NOT NULL
               AND baseline_subject ->> 'tree_digest' IS NOT NULL
               AND baseline_subject ->> 'resource_id' = repository_resource_id::text
               AND baseline_subject ->> 'commit' ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
               AND baseline_subject ->> 'tree_digest' ~ '^sha256:[0-9a-f]{64}$'
             )
             """
           )

    execute("""
    ALTER TABLE work_items
    ADD CONSTRAINT work_items_baseline_dependency_identity_fkey
    FOREIGN KEY (baseline_dependency_id, goal_id, admitted_revision)
    REFERENCES work_items (id, goal_id, admitted_revision)
    ON DELETE RESTRICT
    DEFERRABLE INITIALLY DEFERRED
    """)

    execute("""
    CREATE FUNCTION goal_0006_guard_work_item_baseline_assignment()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF TG_OP = 'INSERT' THEN
        IF NEW.goal_id IS NOT NULL
           AND NEW.baseline_subject IS NULL
           AND NEW.baseline_dependency_id IS NULL THEN
          RAISE EXCEPTION 'goal_0006_goal_work_item_requires_baseline';
        END IF;
      ELSIF TG_OP = 'UPDATE' THEN
        IF OLD.goal_id IS NULL
           AND NEW.goal_id IS NOT NULL
           AND NEW.baseline_subject IS NULL
           AND NEW.baseline_dependency_id IS NULL THEN
          RAISE EXCEPTION 'goal_0006_goal_work_item_requires_baseline';
        END IF;

        IF OLD.goal_id IS NOT NULL AND (
             NEW.baseline_subject IS DISTINCT FROM OLD.baseline_subject
          OR NEW.baseline_dependency_id IS DISTINCT FROM OLD.baseline_dependency_id
        ) THEN
          RAISE EXCEPTION 'goal_0006_work_item_baseline_immutable';
        END IF;
      END IF;

      RETURN NEW;
    END;
    $$
    """)

    execute("""
    CREATE TRIGGER work_items_baseline_assignment_guard
    BEFORE INSERT OR UPDATE OF goal_id, baseline_subject, baseline_dependency_id ON work_items
    FOR EACH ROW EXECUTE FUNCTION goal_0006_guard_work_item_baseline_assignment()
    """)

    execute("""
    CREATE FUNCTION goal_0006_assert_work_item_baseline_dependency(
      target_work_item_id uuid,
      target_goal_id uuid
    )
    RETURNS void
    LANGUAGE plpgsql
    AS $$
    DECLARE
      target_baseline_dependency_id uuid;
    BEGIN
      IF target_work_item_id IS NULL OR target_goal_id IS NULL THEN
        RETURN;
      END IF;

      SELECT baseline_dependency_id
      INTO target_baseline_dependency_id
      FROM work_items
      WHERE id = target_work_item_id
        AND goal_id = target_goal_id;

      IF target_baseline_dependency_id IS NOT NULL
         AND NOT EXISTS (
           SELECT 1
           FROM work_dependencies
           WHERE goal_id = target_goal_id
             AND work_item_id = target_work_item_id
             AND depends_on_id = target_baseline_dependency_id
         ) THEN
        RAISE EXCEPTION 'goal_0006_baseline_dependency_must_be_declared_dependency';
      END IF;
    END;
    $$
    """)

    execute("""
    CREATE FUNCTION goal_0006_validate_work_item_baseline_dependency()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF TG_TABLE_NAME = 'work_items' THEN
        PERFORM goal_0006_assert_work_item_baseline_dependency(NEW.id, NEW.goal_id);
      ELSE
        IF TG_OP IN ('UPDATE', 'DELETE') THEN
          PERFORM goal_0006_assert_work_item_baseline_dependency(
            OLD.work_item_id,
            OLD.goal_id
          );
        END IF;

        IF TG_OP IN ('INSERT', 'UPDATE') THEN
          PERFORM goal_0006_assert_work_item_baseline_dependency(
            NEW.work_item_id,
            NEW.goal_id
          );
        END IF;
      END IF;

      RETURN NULL;
    END;
    $$
    """)

    execute("""
    CREATE CONSTRAINT TRIGGER work_items_baseline_dependency_guard
    AFTER INSERT OR UPDATE OF goal_id, admitted_revision, baseline_dependency_id ON work_items
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION goal_0006_validate_work_item_baseline_dependency()
    """)

    execute("""
    CREATE CONSTRAINT TRIGGER work_dependencies_baseline_dependency_guard
    AFTER INSERT OR UPDATE OR DELETE ON work_dependencies
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION goal_0006_validate_work_item_baseline_dependency()
    """)
  end

  def down do
    execute("""
    LOCK TABLE work_dependencies, work_items IN SHARE MODE
    """)

    execute("""
    DO $$
    BEGIN
      IF EXISTS (
        SELECT 1
        FROM work_items
        WHERE baseline_subject IS NOT NULL OR baseline_dependency_id IS NOT NULL
      ) THEN
        RAISE EXCEPTION
          'cannot roll back Goal WorkItem baselines while baseline sources exist';
      END IF;
    END
    $$;
    """)

    execute("DROP TRIGGER work_dependencies_baseline_dependency_guard ON work_dependencies")
    execute("DROP TRIGGER work_items_baseline_dependency_guard ON work_items")
    execute("DROP TRIGGER work_items_baseline_assignment_guard ON work_items")
    execute("DROP FUNCTION goal_0006_validate_work_item_baseline_dependency()")
    execute("DROP FUNCTION goal_0006_assert_work_item_baseline_dependency(uuid, uuid)")
    execute("DROP FUNCTION goal_0006_guard_work_item_baseline_assignment()")
    drop constraint(:work_items, :work_items_baseline_dependency_identity_fkey)
    drop constraint(:work_items, :work_items_baseline_subject_shape_check)
    drop constraint(:work_items, :work_items_baseline_fields_check)
    drop index(:work_items, [:baseline_dependency_id])

    drop index(:work_items, [:id, :goal_id, :admitted_revision],
           name: :work_items_id_goal_revision_key
         )

    alter table(:work_items) do
      remove :baseline_dependency_id
      remove :baseline_subject
    end
  end
end
