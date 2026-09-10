defmodule SymmetryControl.Repo.Migrations.AddGoalWorkItemBaselines do
  use Ecto.Migration

  def up do
    refuse_undesignated_goal_work_items!()

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
      target_repository_resource_id uuid;
      dependency_repository_resource_id uuid;
    BEGIN
      IF target_work_item_id IS NULL OR target_goal_id IS NULL THEN
        RETURN;
      END IF;

      SELECT baseline_dependency_id, repository_resource_id
      INTO target_baseline_dependency_id, target_repository_resource_id
      FROM work_items
      WHERE id = target_work_item_id
        AND goal_id = target_goal_id;

      IF target_baseline_dependency_id IS NOT NULL THEN
        IF NOT EXISTS (
             SELECT 1
             FROM work_dependencies
             WHERE goal_id = target_goal_id
               AND work_item_id = target_work_item_id
               AND depends_on_id = target_baseline_dependency_id
           ) THEN
          RAISE EXCEPTION 'goal_0006_baseline_dependency_must_be_declared_dependency';
        END IF;

        SELECT repository_resource_id
        INTO dependency_repository_resource_id
        FROM work_items
        WHERE id = target_baseline_dependency_id
          AND goal_id = target_goal_id;

        IF target_repository_resource_id IS NULL
           OR dependency_repository_resource_id IS DISTINCT FROM target_repository_resource_id THEN
          RAISE EXCEPTION 'goal_0006_baseline_dependency_resource_mismatch';
        END IF;
      END IF;
    END;
    $$
    """)

    execute("""
    CREATE FUNCTION goal_0006_validate_work_item_baseline_dependency()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
      dependent_work_item_id uuid;
      dependent_goal_id uuid;
    BEGIN
      IF TG_TABLE_NAME = 'work_items' THEN
        PERFORM goal_0006_assert_work_item_baseline_dependency(NEW.id, NEW.goal_id);

        FOR dependent_work_item_id, dependent_goal_id IN
          SELECT id, goal_id
          FROM work_items
          WHERE baseline_dependency_id = NEW.id
        LOOP
          PERFORM goal_0006_assert_work_item_baseline_dependency(
            dependent_work_item_id,
            dependent_goal_id
          );
        END LOOP;
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
    AFTER INSERT OR UPDATE OF goal_id, admitted_revision, repository_resource_id, baseline_dependency_id
    ON work_items
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION goal_0006_validate_work_item_baseline_dependency()
    """)

    execute("""
    CREATE CONSTRAINT TRIGGER work_dependencies_baseline_dependency_guard
    AFTER INSERT OR UPDATE OR DELETE ON work_dependencies
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION goal_0006_validate_work_item_baseline_dependency()
    """)

    execute("""
    CREATE FUNCTION goal_0006_assert_work_item_integration(
      target_goal_id uuid,
      target_goal_revision integer
    )
    RETURNS void
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF target_goal_id IS NULL OR target_goal_revision IS NULL THEN
        RETURN;
      END IF;

      IF EXISTS (
           SELECT 1
           FROM work_items
           WHERE goal_id = target_goal_id
             AND admitted_revision = target_goal_revision
         ) AND NOT EXISTS (
           SELECT 1
           FROM work_items
           WHERE goal_id = target_goal_id
             AND admitted_revision = target_goal_revision
             AND integration
         ) THEN
        RAISE EXCEPTION 'goal_0006_goal_work_items_require_integration';
      END IF;
    END;
    $$
    """)

    execute("""
    CREATE FUNCTION goal_0006_validate_work_item_integration()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF TG_OP IN ('UPDATE', 'DELETE') THEN
        PERFORM goal_0006_assert_work_item_integration(OLD.goal_id, OLD.admitted_revision);
      END IF;

      IF TG_OP IN ('INSERT', 'UPDATE') THEN
        PERFORM goal_0006_assert_work_item_integration(NEW.goal_id, NEW.admitted_revision);
      END IF;

      RETURN NULL;
    END;
    $$
    """)

    execute("""
    CREATE CONSTRAINT TRIGGER work_items_goal_integration_guard
    AFTER INSERT OR UPDATE OF goal_id, admitted_revision, integration OR DELETE ON work_items
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION goal_0006_validate_work_item_integration()
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

    execute("DROP TRIGGER work_items_goal_integration_guard ON work_items")
    execute("DROP FUNCTION goal_0006_validate_work_item_integration()")
    execute("DROP FUNCTION goal_0006_assert_work_item_integration(uuid, integer)")
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

  defp refuse_undesignated_goal_work_items! do
    execute("LOCK TABLE work_items IN SHARE MODE")

    execute("""
    DO $$
    BEGIN
      IF EXISTS (
        SELECT 1
        FROM work_items
        WHERE goal_id IS NOT NULL
        GROUP BY goal_id, admitted_revision
        HAVING NOT BOOL_OR(integration)
      ) THEN
        RAISE EXCEPTION
          'cannot add Goal WorkItem integration guard while an admitted revision has no integration WorkItem';
      END IF;
    END
    $$;
    """)
  end
end
