defmodule SymmetryControl.Repo.Migrations.AddGoalIdentityGuards do
  use Ecto.Migration

  def up do
    execute("ALTER TABLE tasks DROP CONSTRAINT tasks_work_item_id_fkey")

    execute("""
    ALTER TABLE tasks
    ADD CONSTRAINT tasks_work_item_id_fkey
    FOREIGN KEY (work_item_id)
    REFERENCES work_items (id)
    ON DELETE SET NULL
    """)

    create unique_index(:project_resources, [:id, :project_id],
             name: :project_resources_id_project_id_key
           )

    execute("""
    ALTER TABLE work_items
    ADD CONSTRAINT work_items_repository_resource_project_fkey
    FOREIGN KEY (repository_resource_id, project_id)
    REFERENCES project_resources (id, project_id)
    ON DELETE RESTRICT
    """)

    execute("""
    CREATE FUNCTION goal_0006_validate_work_item_repository_resource()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
      resource_project_id uuid;
      resource_kind text;
    BEGIN
      IF NEW.repository_resource_id IS NULL THEN
        RETURN NEW;
      END IF;

      SELECT project_id, kind
      INTO resource_project_id, resource_kind
      FROM project_resources
      WHERE id = NEW.repository_resource_id;

      IF resource_project_id IS DISTINCT FROM NEW.project_id THEN
        RAISE EXCEPTION 'goal_0006_work_item_repository_resource_project';
      END IF;

      IF resource_kind IS DISTINCT FROM 'repository' THEN
        RAISE EXCEPTION 'goal_0006_work_item_repository_resource_kind';
      END IF;

      RETURN NEW;
    END;
    $$
    """)

    execute("""
    CREATE TRIGGER work_items_repository_resource_guard
    BEFORE INSERT OR UPDATE OF project_id, repository_resource_id ON work_items
    FOR EACH ROW EXECUTE FUNCTION goal_0006_validate_work_item_repository_resource()
    """)

    execute("""
    CREATE FUNCTION goal_0006_retain_goal_work_item()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF OLD.goal_id IS NOT NULL THEN
        RAISE EXCEPTION 'goal_0006_goal_work_item_retained';
      END IF;

      RETURN OLD;
    END;
    $$
    """)

    execute("""
    CREATE TRIGGER work_items_goal_retention_guard
    BEFORE DELETE ON work_items
    FOR EACH ROW EXECUTE FUNCTION goal_0006_retain_goal_work_item()
    """)

    execute("""
    CREATE FUNCTION goal_0006_freeze_goal_work_item_identity()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF OLD.goal_id IS NOT NULL AND (
           NEW.project_id IS DISTINCT FROM OLD.project_id
        OR NEW.goal_id IS DISTINCT FROM OLD.goal_id
        OR NEW.admitted_revision IS DISTINCT FROM OLD.admitted_revision
        OR NEW.required IS DISTINCT FROM OLD.required
        OR NEW.integration IS DISTINCT FROM OLD.integration
        OR NEW.acceptance_contract IS DISTINCT FROM OLD.acceptance_contract
        OR NEW.repository_resource_id IS DISTINCT FROM OLD.repository_resource_id
        OR NEW.baseline_subject IS DISTINCT FROM OLD.baseline_subject
        OR NEW.baseline_dependency_id IS DISTINCT FROM OLD.baseline_dependency_id
      ) THEN
        RAISE EXCEPTION 'goal_0006_goal_work_item_identity_immutable';
      END IF;

      RETURN NEW;
    END;
    $$
    """)

    execute("""
    CREATE TRIGGER work_items_goal_identity_guard
    BEFORE UPDATE OF project_id, goal_id, admitted_revision, required, integration,
      acceptance_contract, repository_resource_id, baseline_subject, baseline_dependency_id
    ON work_items
    FOR EACH ROW EXECUTE FUNCTION goal_0006_freeze_goal_work_item_identity()
    """)

    execute("""
    CREATE FUNCTION goal_0006_freeze_goal_task_identity()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF OLD.goal_id IS NOT NULL AND (
           NEW.idempotency_key IS DISTINCT FROM OLD.idempotency_key
        OR NEW.request_hash IS DISTINCT FROM OLD.request_hash
        OR NEW.request_hash_version IS DISTINCT FROM OLD.request_hash_version
        OR NEW.work_item_id IS DISTINCT FROM OLD.work_item_id
        OR NEW.goal_id IS DISTINCT FROM OLD.goal_id
        OR NEW.goal_revision IS DISTINCT FROM OLD.goal_revision
        OR NEW.context_snapshot_id IS DISTINCT FROM OLD.context_snapshot_id
        OR NEW.goal IS DISTINCT FROM OLD.goal
        OR NEW.agent_profile IS DISTINCT FROM OLD.agent_profile
         OR NEW.workspace IS DISTINCT FROM OLD.workspace
         OR NEW.input IS DISTINCT FROM OLD.input
         OR NEW.required_capabilities IS DISTINCT FROM OLD.required_capabilities
         OR NEW.purpose IS DISTINCT FROM OLD.purpose
         OR NEW.validation_of_task_id IS DISTINCT FROM OLD.validation_of_task_id
         OR NEW.admission_key IS DISTINCT FROM OLD.admission_key
         OR NEW.max_run_attempts IS DISTINCT FROM OLD.max_run_attempts
         OR NEW.requested_session_id IS DISTINCT FROM OLD.requested_session_id
      ) THEN
        RAISE EXCEPTION 'goal_0006_goal_task_identity_immutable';
      END IF;

      RETURN NEW;
    END;
    $$
    """)

    execute("""
    CREATE TRIGGER tasks_goal_identity_guard
    BEFORE UPDATE OF idempotency_key, request_hash, request_hash_version, work_item_id,
      goal_id, goal_revision, context_snapshot_id, goal, agent_profile, workspace,
      input, required_capabilities, purpose, validation_of_task_id, admission_key,
      max_run_attempts, requested_session_id
    ON tasks
    FOR EACH ROW EXECUTE FUNCTION goal_0006_freeze_goal_task_identity()
    """)

    execute("""
    DO $$
    BEGIN
      IF EXISTS (
        SELECT 1
        FROM runs
        WHERE harness_session_id IS NOT NULL
          AND state IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'paused', 'cancelling')
        GROUP BY harness_session_id
        HAVING COUNT(*) > 1
      ) THEN
        RAISE EXCEPTION 'cannot add Goal session identity guard while duplicate active session runs exist';
      END IF;
    END
    $$;
    """)

    create unique_index(:runs, [:harness_session_id],
             where:
               "harness_session_id IS NOT NULL AND state IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'paused', 'cancelling')",
             name: :runs_one_active_harness_session
           )

    execute("""
    CREATE FUNCTION goal_0006_freeze_goal_resource_identity()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF EXISTS (
        SELECT 1
        FROM work_items
        WHERE goal_id IS NOT NULL
          AND (
            repository_resource_id = OLD.id
            OR ci_resource_id = OLD.id
            OR external_work_item_resource_id = OLD.id
          )
      ) AND (
           NEW.project_id IS DISTINCT FROM OLD.project_id
        OR NEW.kind IS DISTINCT FROM OLD.kind
        OR NEW.provider IS DISTINCT FROM OLD.provider
        OR NEW.external_ref IS DISTINCT FROM OLD.external_ref
        OR NEW.connection_id IS DISTINCT FROM OLD.connection_id
      ) THEN
        RAISE EXCEPTION 'goal_0006_goal_resource_identity_immutable';
      END IF;

      RETURN NEW;
    END;
    $$
    """)

    execute("""
    CREATE TRIGGER project_resources_goal_identity_guard
    BEFORE UPDATE OF project_id, kind, provider, external_ref, connection_id
    ON project_resources
    FOR EACH ROW EXECUTE FUNCTION goal_0006_freeze_goal_resource_identity()
    """)

    execute("""
    CREATE FUNCTION goal_0006_validate_active_session_run_identity()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
      linked_session_id uuid;
      linked_run_id uuid;
      linked_state text;
    BEGIN
      IF TG_TABLE_NAME = 'harness_sessions' THEN
        IF NEW.active_run_id IS NOT NULL THEN
          SELECT harness_session_id, state
          INTO linked_session_id, linked_state
          FROM runs
          WHERE id = NEW.active_run_id;

          IF linked_session_id IS DISTINCT FROM NEW.id
             OR linked_state IS NULL
             OR linked_state NOT IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'paused', 'cancelling') THEN
            RAISE EXCEPTION 'goal_0006_active_session_run_identity';
          END IF;

          IF EXISTS (
            SELECT 1
            FROM runs
            WHERE harness_session_id = NEW.id
              AND id IS DISTINCT FROM NEW.active_run_id
              AND state IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'paused', 'cancelling')
          ) THEN
            RAISE EXCEPTION 'goal_0006_active_session_run_identity';
          END IF;
        ELSE
          IF EXISTS (
            SELECT 1
            FROM runs
            WHERE harness_session_id = NEW.id
              AND state IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'paused', 'cancelling')
          ) THEN
            RAISE EXCEPTION 'goal_0006_active_session_run_identity';
          END IF;
        END IF;
      ELSE
        IF NEW.harness_session_id IS NOT NULL
           AND NEW.state IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'paused', 'cancelling') THEN
          SELECT active_run_id, state
          INTO linked_run_id, linked_state
          FROM harness_sessions
          WHERE id = NEW.harness_session_id;

          IF linked_run_id IS DISTINCT FROM NEW.id OR linked_state <> 'busy' THEN
            RAISE EXCEPTION 'goal_0006_active_session_run_identity';
          END IF;
        END IF;
      END IF;

      RETURN NULL;
    END;
    $$
    """)

    execute("""
    CREATE CONSTRAINT TRIGGER harness_sessions_active_run_identity_guard
    AFTER INSERT OR UPDATE OF active_run_id, state ON harness_sessions
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION goal_0006_validate_active_session_run_identity()
    """)

    execute("""
    CREATE CONSTRAINT TRIGGER runs_active_session_identity_guard
    AFTER INSERT OR UPDATE OF harness_session_id, state ON runs
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW EXECUTE FUNCTION goal_0006_validate_active_session_run_identity()
    """)
  end

  def down do
    refuse_identity_rollback!()

    execute("DROP TRIGGER runs_active_session_identity_guard ON runs")
    execute("DROP TRIGGER harness_sessions_active_run_identity_guard ON harness_sessions")
    execute("DROP FUNCTION goal_0006_validate_active_session_run_identity()")

    execute("DROP TRIGGER project_resources_goal_identity_guard ON project_resources")
    execute("DROP FUNCTION goal_0006_freeze_goal_resource_identity()")

    execute("DROP TRIGGER tasks_goal_identity_guard ON tasks")
    execute("DROP FUNCTION goal_0006_freeze_goal_task_identity()")

    drop index(:runs, [:harness_session_id], name: :runs_one_active_harness_session)

    execute("DROP TRIGGER work_items_goal_identity_guard ON work_items")
    execute("DROP FUNCTION goal_0006_freeze_goal_work_item_identity()")

    execute("DROP TRIGGER work_items_goal_retention_guard ON work_items")
    execute("DROP FUNCTION goal_0006_retain_goal_work_item()")

    execute("DROP TRIGGER work_items_repository_resource_guard ON work_items")
    execute("DROP FUNCTION goal_0006_validate_work_item_repository_resource()")

    drop constraint(:work_items, :work_items_repository_resource_project_fkey)
    drop index(:project_resources, [:id, :project_id], name: :project_resources_id_project_id_key)

    execute("ALTER TABLE tasks DROP CONSTRAINT tasks_work_item_id_fkey")

    execute("""
    ALTER TABLE tasks
    ADD CONSTRAINT tasks_work_item_id_fkey
    FOREIGN KEY (work_item_id)
    REFERENCES work_items (id)
    ON DELETE RESTRICT
    """)
  end

  defp refuse_identity_rollback! do
    execute("""
    LOCK TABLE
      goals,
      goal_revisions,
      goal_events,
      goal_decisions,
      goal_budget_reservations,
      context_snapshots,
      work_dependencies,
      work_items,
      tasks,
      runs,
      harness_sessions,
      run_evidence,
      work_outcomes,
      run_usage,
      project_resources
    IN SHARE MODE
    """)

    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM goals)
         OR EXISTS (SELECT 1 FROM goal_revisions)
         OR EXISTS (SELECT 1 FROM work_dependencies)
         OR EXISTS (SELECT 1 FROM context_snapshots)
         OR EXISTS (SELECT 1 FROM work_outcomes)
         OR EXISTS (SELECT 1 FROM goal_decisions)
         OR EXISTS (SELECT 1 FROM goal_events)
         OR EXISTS (SELECT 1 FROM goal_budget_reservations)
         OR EXISTS (
           SELECT 1
           FROM run_evidence AS evidence
           JOIN runs AS evidence_run ON evidence_run.id = evidence.run_id
           JOIN tasks AS evidence_task ON evidence_task.id = evidence_run.task_id
           WHERE evidence_task.goal_id IS NOT NULL
         )
         OR EXISTS (
           SELECT 1
           FROM run_usage AS usage
           JOIN runs AS usage_run ON usage_run.id = usage.run_id
           JOIN tasks AS usage_task ON usage_task.id = usage_run.task_id
           WHERE usage_task.goal_id IS NOT NULL
         )
         OR EXISTS (SELECT 1 FROM run_usage)
         OR EXISTS (
           SELECT 1
           FROM work_items
           WHERE goal_id IS NOT NULL
              OR admitted_revision IS NOT NULL
              OR acceptance_contract IS NOT NULL
         )
         OR EXISTS (
           SELECT 1
           FROM tasks
           WHERE goal_id IS NOT NULL
              OR goal_revision IS NOT NULL
              OR context_snapshot_id IS NOT NULL
              OR admission_key IS NOT NULL
              OR max_run_attempts IS NOT NULL
              OR validation_of_task_id IS NOT NULL
              OR requested_session_id IS NOT NULL
              OR (
                work_item_id IS NOT NULL
                AND NOT EXISTS (
                  SELECT 1
                  FROM work_items AS current_item
                  WHERE current_item.id = tasks.work_item_id
                    AND current_item.orchestration_task_id = tasks.id
                    AND (
                      SELECT COUNT(*)
                      FROM work_items AS pointed_item
                      WHERE pointed_item.orchestration_task_id = tasks.id
                    ) = 1
                )
              )
         )
         OR EXISTS (
           SELECT 1
           FROM runs AS goal_run
           JOIN tasks AS goal_task ON goal_task.id = goal_run.task_id
           WHERE goal_task.goal_id IS NOT NULL
             AND goal_run.harness_session_id IS NOT NULL
         ) THEN
        RAISE EXCEPTION 'cannot roll back Goal identity guards while Goal history exists';
      END IF;
    END
    $$;
    """)
  end
end
