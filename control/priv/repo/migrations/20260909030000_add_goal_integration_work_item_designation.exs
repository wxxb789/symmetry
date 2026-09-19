defmodule SymmetryControl.Repo.Migrations.AddGoalIntegrationWorkItemDesignation do
  use Ecto.Migration

  def up do
    alter table(:work_items) do
      add :integration, :boolean, null: false, default: false
    end

    create constraint(:work_items, :work_items_integration_requires_goal_ownership,
             check: "NOT integration OR goal_id IS NOT NULL"
           )

    execute("""
    CREATE FUNCTION goal_0006_reject_goal_work_item_integration_mutation()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF OLD.goal_id IS NOT NULL AND NEW.integration IS DISTINCT FROM OLD.integration THEN
        RAISE EXCEPTION 'goal_0006_goal_work_item_integration_immutable';
      END IF;

      RETURN NEW;
    END;
    $$
    """)

    execute("""
    CREATE TRIGGER work_items_goal_integration_immutable
    BEFORE UPDATE ON work_items
    FOR EACH ROW EXECUTE FUNCTION goal_0006_reject_goal_work_item_integration_mutation()
    """)
  end

  def down do
    execute("LOCK TABLE work_items IN SHARE MODE")

    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM work_items WHERE integration) THEN
        RAISE EXCEPTION 'cannot roll back integration WorkItem designation while designated rows exist';
      END IF;
    END
    $$;
    """)

    execute("DROP TRIGGER work_items_goal_integration_immutable ON work_items")
    execute("DROP FUNCTION goal_0006_reject_goal_work_item_integration_mutation()")
    drop constraint(:work_items, :work_items_integration_requires_goal_ownership)

    alter table(:work_items) do
      remove :integration
    end
  end
end
