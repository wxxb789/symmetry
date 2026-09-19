defmodule SymmetryControl.Repo.Migrations.AddRunProviderAccessSnapshots do
  use Ecto.Migration

  def up do
    # Quiesce claim transactions before checking or backfilling snapshots. A SHARE lock
    # can coexist with SELECT FOR UPDATE row locks and deadlock when this UPDATE
    # promotes the relation lock. The order matches the claim path's Task -> Run order.
    execute("LOCK TABLE tasks, runs IN ACCESS EXCLUSIVE MODE")

    execute("""
    DO $$
    BEGIN
      IF EXISTS (
        SELECT 1
        FROM runs AS run
        JOIN tasks AS task ON task.id = run.task_id
        WHERE run.state IN ('claimed', 'running', 'waiting_for_input', 'paused', 'cancelling')
          AND COALESCE(task.required_capabilities ->> 'provider_access', 'false') = 'true'
      ) THEN
        RAISE EXCEPTION
          'cannot add run provider access snapshots while active provider-required claims exist';
      END IF;
    END
    $$;
    """)

    alter table(:runs) do
      add :provider_access_snapshot, :map
    end

    # Store only the credential-free capability receipt. Broker credentials and
    # signed tokens remain reconstructable local effects, never durable state.
    execute("""
    CREATE FUNCTION goal_0006_provider_access_snapshot_valid(value jsonb)
    RETURNS boolean
    LANGUAGE sql
    IMMUTABLE
    STRICT
    AS $$
      SELECT value = '{"v":1,"kind":"none"}'::jsonb OR
      CASE
        WHEN jsonb_typeof(value) = 'object' THEN
          value ?& ARRAY['v', 'kind', 'grants']
          AND value - 'v' - 'kind' - 'grants' = '{}'::jsonb
          AND jsonb_typeof(value -> 'v') = 'number'
          AND value ->> 'v' = '1'
          AND jsonb_typeof(value -> 'kind') = 'string'
          AND value ->> 'kind' = 'granted'
          AND CASE
            WHEN jsonb_typeof(value -> 'grants') = 'array' THEN
              jsonb_array_length(value -> 'grants') > 0
              AND NOT EXISTS (
                SELECT 1
                FROM jsonb_array_elements(value -> 'grants') AS grant_value
                WHERE CASE
                  WHEN jsonb_typeof(grant_value) = 'object' THEN
                    NOT (grant_value ?& ARRAY['resource_id', 'provider', 'kind', 'operations'])
                    OR grant_value - 'resource_id' - 'provider' - 'kind' - 'operations' <> '{}'::jsonb
                    OR jsonb_typeof(grant_value -> 'resource_id') <> 'string'
                    OR grant_value ->> 'resource_id' !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
                    OR jsonb_typeof(grant_value -> 'provider') <> 'string'
                    OR grant_value ->> 'provider' NOT IN ('github', 'azure_devops')
                    OR jsonb_typeof(grant_value -> 'kind') <> 'string'
                    OR grant_value ->> 'kind' NOT IN ('repository', 'work_tracking', 'ci')
                    OR CASE
                      WHEN jsonb_typeof(grant_value -> 'operations') = 'array' THEN
                        jsonb_array_length(grant_value -> 'operations') = 0
                        OR EXISTS (
                          SELECT 1
                          FROM jsonb_array_elements(grant_value -> 'operations') AS operation_value
                          WHERE jsonb_typeof(operation_value) <> 'string'
                             OR operation_value #>> '{}' NOT IN ('resource.sync', 'change.upsert', 'change.update')
                        )
                      ELSE
                        TRUE
                    END
                  ELSE
                    TRUE
                END
              )
            ELSE
              FALSE
          END
        ELSE
          FALSE
      END
    $$
    """)

    create constraint(:runs, :runs_provider_access_snapshot_shape_check,
             check: "goal_0006_provider_access_snapshot_valid(provider_access_snapshot)"
           )

    execute("""
    CREATE FUNCTION goal_0006_freeze_provider_access_snapshot()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF OLD.provider_access_snapshot IS NOT NULL
         AND NEW.provider_access_snapshot IS DISTINCT FROM OLD.provider_access_snapshot THEN
        RAISE EXCEPTION 'goal_0006_provider_access_snapshot_immutable';
      END IF;

      RETURN NEW;
    END;
    $$
    """)

    execute("""
    CREATE TRIGGER runs_provider_access_snapshot_immutable
    BEFORE UPDATE OF provider_access_snapshot ON runs
    FOR EACH ROW EXECUTE FUNCTION goal_0006_freeze_provider_access_snapshot()
    """)

    execute("""
    UPDATE runs AS run
    SET provider_access_snapshot = '{"v":1,"kind":"none"}'::jsonb
    FROM tasks
    WHERE tasks.id = run.task_id
      AND run.provider_access_snapshot IS NULL
       AND run.state IN ('claimed', 'running', 'waiting_for_input', 'paused', 'cancelling')
      AND COALESCE(tasks.required_capabilities->>'provider_access', 'false') <> 'true'
    """)
  end

  def down do
    execute("LOCK TABLE tasks, runs IN ACCESS EXCLUSIVE MODE")

    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM runs WHERE provider_access_snapshot IS NOT NULL) THEN
        RAISE EXCEPTION
          'cannot roll back run provider access snapshots while replay snapshots exist';
      END IF;
    END
    $$;
    """)

    execute("DROP TRIGGER runs_provider_access_snapshot_immutable ON runs")
    execute("DROP FUNCTION goal_0006_freeze_provider_access_snapshot()")
    drop constraint(:runs, :runs_provider_access_snapshot_shape_check)
    execute("DROP FUNCTION goal_0006_provider_access_snapshot_valid(jsonb)")

    alter table(:runs) do
      remove :provider_access_snapshot
    end
  end
end
