defmodule SymmetryControl.Repo.Migrations.AddGoalWorkItemChangeTargets do
  use Ecto.Migration

  def up do
    alter table(:work_items) do
      add(:change_target, :map)
    end

    create(
      constraint(:work_items, :work_items_change_target_requires_goal_ownership,
        check: "goal_id IS NOT NULL OR change_target IS NULL"
      )
    )

    create(
      constraint(:work_items, :work_items_change_target_shape_check, check: change_target_check())
    )

    execute("""
    CREATE FUNCTION goal_0006_guard_work_item_change_target()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF NEW.goal_id IS NULL AND NEW.change_target IS NOT NULL THEN
        RAISE EXCEPTION 'goal_0006_work_item_change_target_requires_goal';
      END IF;

      IF TG_OP = 'UPDATE'
         AND OLD.goal_id IS NOT NULL
         AND NEW.change_target IS DISTINCT FROM OLD.change_target THEN
        RAISE EXCEPTION 'goal_0006_work_item_change_target_immutable';
      END IF;

      RETURN NEW;
    END;
    $$
    """)

    execute("""
    CREATE TRIGGER work_items_goal_change_target_immutable
    BEFORE INSERT OR UPDATE OF goal_id, change_target ON work_items
    FOR EACH ROW EXECUTE FUNCTION goal_0006_guard_work_item_change_target()
    """)
  end

  def down do
    execute("LOCK TABLE work_items IN SHARE MODE")

    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM work_items WHERE change_target IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot roll back Goal WorkItem change targets while targets exist';
      END IF;
    END
    $$;
    """)

    execute("DROP TRIGGER work_items_goal_change_target_immutable ON work_items")
    execute("DROP FUNCTION goal_0006_guard_work_item_change_target()")
    drop(constraint(:work_items, :work_items_change_target_shape_check))
    drop(constraint(:work_items, :work_items_change_target_requires_goal_ownership))

    alter table(:work_items) do
      remove(:change_target)
    end
  end

  defp change_target_check do
    """
    change_target IS NULL OR (
      jsonb_typeof(change_target) = 'object'
      AND (
        (
          change_target ->> 'kind' = 'branches'
          AND change_target ?& ARRAY['kind', 'source_branch', 'target_branch']
          AND change_target - 'kind' - 'source_branch' - 'target_branch' = '{}'::jsonb
          AND jsonb_typeof(change_target -> 'source_branch') = 'string'
          AND jsonb_typeof(change_target -> 'target_branch') = 'string'
          AND char_length(change_target ->> 'source_branch') BETWEEN 1 AND 255
          AND char_length(change_target ->> 'target_branch') BETWEEN 1 AND 255
          AND change_target ->> 'source_branch' !~ '[[:space:][:cntrl:]]'
          AND change_target ->> 'target_branch' !~ '[[:space:][:cntrl:]]'
          AND change_target ->> 'source_branch' <> change_target ->> 'target_branch'
        )
        OR
        (
          change_target ->> 'kind' = 'pull_request'
          AND change_target ?& ARRAY['kind', 'pull_request_url']
          AND change_target - 'kind' - 'pull_request_url' = '{}'::jsonb
          AND jsonb_typeof(change_target -> 'pull_request_url') = 'string'
          AND char_length(BTRIM(change_target ->> 'pull_request_url')) BETWEEN 1 AND 16384
          AND change_target ->> 'pull_request_url' = BTRIM(change_target ->> 'pull_request_url')
        )
      )
    )
    """
  end
end
