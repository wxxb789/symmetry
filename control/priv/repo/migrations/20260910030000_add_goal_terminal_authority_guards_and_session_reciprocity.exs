defmodule SymmetryControl.Repo.Migrations.AddGoalTerminalAuthorityGuardsAndSessionReciprocity do
  use Ecto.Migration

  def up do
    lock_guarded_tables!()
    refuse_inconsistent_session_run_relationships!()

    execute("""
    CREATE FUNCTION goal_0006_guard_terminal_goal_revision_parent()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
      parent_state text;
    BEGIN
      SELECT state
      INTO parent_state
      FROM goals
      WHERE id = NEW.goal_id
      FOR SHARE;

      IF parent_state IN ('achieved', 'cancelled') THEN
        RAISE EXCEPTION 'goal_0006_terminal_goal_authority';
      END IF;

      RETURN NEW;
    END;
    $$;
    """)

    execute("""
    CREATE TRIGGER goal_revisions_terminal_parent_authority_guard
    BEFORE INSERT ON goal_revisions
    FOR EACH ROW EXECUTE FUNCTION goal_0006_guard_terminal_goal_revision_parent()
    """)

    execute("DROP TRIGGER goal_decisions_terminal_authority_guard ON goal_decisions")

    execute("""
    CREATE OR REPLACE FUNCTION goal_0006_guard_terminal_decision()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
      parent_state text;
    BEGIN
      IF TG_OP = 'INSERT' THEN
        SELECT state
        INTO parent_state
        FROM goals
        WHERE id = NEW.goal_id
        FOR SHARE;

        IF parent_state IN ('achieved', 'cancelled') THEN
          RAISE EXCEPTION 'goal_0006_terminal_goal_authority';
        END IF;
      ELSIF TG_OP = 'DELETE' THEN
        SELECT state
        INTO parent_state
        FROM goals
        WHERE id = OLD.goal_id
        FOR SHARE;

        IF parent_state IN ('achieved', 'cancelled') THEN
          RAISE EXCEPTION 'goal_0006_terminal_goal_authority';
        END IF;
      ELSE
        FOR parent_state IN
              SELECT state
              FROM goals
              WHERE id = ANY(ARRAY[OLD.goal_id, NEW.goal_id])
              ORDER BY id
              FOR SHARE
            LOOP
          IF parent_state IN ('achieved', 'cancelled') THEN
            RAISE EXCEPTION 'goal_0006_terminal_goal_authority';
          END IF;
        END LOOP;
      END IF;

      IF TG_OP = 'INSERT' THEN
        IF NEW.state <> 'open' THEN
          RAISE EXCEPTION 'goal_0006_decision_transition_invalid';
        END IF;

        RETURN NEW;
      END IF;

      IF TG_OP = 'DELETE' THEN
        IF OLD.state IN ('resolved', 'superseded') THEN
          RAISE EXCEPTION 'goal_0006_terminal_decision_immutable';
        END IF;

        RETURN OLD;
      END IF;

      IF OLD.state = 'open' THEN
        IF NEW.state NOT IN ('open', 'resolved', 'superseded') THEN
          RAISE EXCEPTION 'goal_0006_decision_transition_invalid';
        END IF;

        RETURN NEW;
      END IF;

      IF OLD.state IN ('resolved', 'superseded') AND (
           NEW.goal_id IS DISTINCT FROM OLD.goal_id
        OR NEW.goal_revision IS DISTINCT FROM OLD.goal_revision
        OR NEW.work_item_id IS DISTINCT FROM OLD.work_item_id
        OR NEW.kind IS DISTINCT FROM OLD.kind
        OR NEW.action_hash IS DISTINCT FROM OLD.action_hash
        OR NEW.subject_hash IS DISTINCT FROM OLD.subject_hash
        OR NEW.state IS DISTINCT FROM OLD.state
        OR NEW.question IS DISTINCT FROM OLD.question
        OR NEW.options IS DISTINCT FROM OLD.options
        OR NEW.resolution IS DISTINCT FROM OLD.resolution
        OR NEW.actor_ref IS DISTINCT FROM OLD.actor_ref
        OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
      ) THEN
        RAISE EXCEPTION 'goal_0006_terminal_decision_immutable';
      END IF;

      RETURN NEW;
    END;
    $$;
    """)

    execute("""
    CREATE TRIGGER goal_decisions_parent_terminal_authority_guard
    BEFORE INSERT OR UPDATE OR DELETE ON goal_decisions
    FOR EACH ROW EXECUTE FUNCTION goal_0006_guard_terminal_decision()
    """)

    execute("""
    CREATE OR REPLACE FUNCTION goal_0006_validate_active_session_run_identity()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
      current_session_id uuid;
      current_session_state text;
      current_active_run_id uuid;
      current_run_id uuid;
      current_run_state text;
      current_harness_session_id uuid;
      linked_harness_session_id uuid;
      linked_run_state text;
    BEGIN
      IF TG_TABLE_NAME = 'harness_sessions' THEN
        SELECT id, state, active_run_id
        INTO current_session_id, current_session_state, current_active_run_id
        FROM harness_sessions
        WHERE id = NEW.id;

        IF NOT FOUND THEN
          RETURN NULL;
        END IF;

        IF current_active_run_id IS NOT NULL THEN
          SELECT harness_session_id, state
          INTO linked_harness_session_id, linked_run_state
          FROM runs
          WHERE id = current_active_run_id;

          IF NOT FOUND
             OR current_session_state IS DISTINCT FROM 'busy'
             OR linked_harness_session_id IS DISTINCT FROM current_session_id
             OR linked_run_state NOT IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'paused', 'cancelling') THEN
            RAISE EXCEPTION 'goal_0006_active_session_run_identity';
          END IF;
        END IF;

        IF EXISTS (
          SELECT 1
          FROM runs
          WHERE harness_session_id = current_session_id
            AND state IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'paused', 'cancelling')
            AND id IS DISTINCT FROM current_active_run_id
        ) THEN
          RAISE EXCEPTION 'goal_0006_active_session_run_identity';
        END IF;
      ELSE
        SELECT id, state, harness_session_id
        INTO current_run_id, current_run_state, current_harness_session_id
        FROM runs
        WHERE id = NEW.id;

        IF NOT FOUND THEN
          RETURN NULL;
        END IF;

        IF current_run_state IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'paused', 'cancelling')
           AND current_harness_session_id IS NOT NULL THEN
          SELECT active_run_id, state
          INTO current_active_run_id, current_session_state
          FROM harness_sessions
          WHERE id = current_harness_session_id;

          IF NOT FOUND
             OR current_active_run_id IS DISTINCT FROM current_run_id
             OR current_session_state IS DISTINCT FROM 'busy' THEN
            RAISE EXCEPTION 'goal_0006_active_session_run_identity';
          END IF;
        ELSIF EXISTS (
          SELECT 1
          FROM harness_sessions
          WHERE active_run_id = current_run_id
        ) THEN
          RAISE EXCEPTION 'goal_0006_active_session_run_identity';
        END IF;

        IF EXISTS (
          SELECT 1
          FROM harness_sessions
          WHERE active_run_id = current_run_id
            AND id IS DISTINCT FROM current_harness_session_id
        ) THEN
          RAISE EXCEPTION 'goal_0006_active_session_run_identity';
        END IF;
      END IF;

      RETURN NULL;
    END;
    $$;
    """)
  end

  def down do
    refuse_rollback!()

    execute("DROP TRIGGER goal_revisions_terminal_parent_authority_guard ON goal_revisions")
    execute("DROP FUNCTION goal_0006_guard_terminal_goal_revision_parent()")

    execute("DROP TRIGGER goal_decisions_parent_terminal_authority_guard ON goal_decisions")
    restore_terminal_decision_guard!()

    execute("""
    CREATE TRIGGER goal_decisions_terminal_authority_guard
    BEFORE INSERT OR UPDATE OR DELETE ON goal_decisions
    FOR EACH ROW EXECUTE FUNCTION goal_0006_guard_terminal_decision()
    """)

    restore_session_run_identity_guard!()
  end

  defp refuse_inconsistent_session_run_relationships! do
    execute("""
    DO $$
    BEGIN
      IF EXISTS (
        SELECT 1
        FROM harness_sessions AS session
        LEFT JOIN runs AS active_run ON active_run.id = session.active_run_id
        WHERE (
          session.active_run_id IS NOT NULL
          AND (
            session.state IS DISTINCT FROM 'busy'
            OR active_run.id IS NULL
            OR active_run.harness_session_id IS DISTINCT FROM session.id
            OR active_run.state NOT IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'paused', 'cancelling')
          )
        )
        OR (
          session.active_run_id IS NULL
          AND EXISTS (
            SELECT 1
            FROM runs
            WHERE harness_session_id = session.id
              AND state IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'paused', 'cancelling')
          )
        )
      )
      OR EXISTS (
        SELECT 1
        FROM runs AS run
        WHERE (
          run.harness_session_id IS NOT NULL
          AND run.state IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'paused', 'cancelling')
          AND NOT EXISTS (
            SELECT 1
            FROM harness_sessions AS session
            WHERE session.id = run.harness_session_id
              AND session.state = 'busy'
              AND session.active_run_id = run.id
          )
        )
        OR (
          run.state NOT IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'paused', 'cancelling')
          AND EXISTS (
            SELECT 1
            FROM harness_sessions
            WHERE active_run_id = run.id
          )
        ) THEN
        RAISE EXCEPTION
          'cannot add Goal final session/run reciprocity guard while existing session/run relationships are inconsistent';
      END IF;
    END
    $$;
    """)
  end

  defp refuse_rollback! do
    lock_guarded_tables!()

    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM goals WHERE state IN ('achieved', 'cancelled'))
         OR EXISTS (
           SELECT 1
           FROM harness_sessions
           WHERE active_run_id IS NOT NULL
         )
         OR EXISTS (
           SELECT 1
           FROM runs
           WHERE harness_session_id IS NOT NULL
             AND state IN ('assigned', 'claimed', 'running', 'waiting_for_input', 'paused', 'cancelling')
         ) THEN
        RAISE EXCEPTION
          'cannot roll back terminal Goal authority and session reciprocity guards while protected Goal or session history exists';
      END IF;
    END
    $$;
    """)
  end

  defp lock_guarded_tables! do
    execute("""
    LOCK TABLE goals, goal_revisions, goal_decisions, runs, harness_sessions
    IN SHARE MODE
    """)
  end

  defp restore_terminal_decision_guard! do
    execute("""
    CREATE OR REPLACE FUNCTION goal_0006_guard_terminal_decision()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF TG_OP = 'INSERT' THEN
        IF NEW.state <> 'open' THEN
          RAISE EXCEPTION 'goal_0006_decision_transition_invalid';
        END IF;

        RETURN NEW;
      END IF;

      IF TG_OP = 'DELETE' THEN
        IF OLD.state IN ('resolved', 'superseded') THEN
          RAISE EXCEPTION 'goal_0006_terminal_decision_immutable';
        END IF;

        RETURN OLD;
      END IF;

      IF OLD.state = 'open' THEN
        IF NEW.state NOT IN ('open', 'resolved', 'superseded') THEN
          RAISE EXCEPTION 'goal_0006_decision_transition_invalid';
        END IF;

        RETURN NEW;
      END IF;

      IF OLD.state IN ('resolved', 'superseded') AND (
           NEW.goal_id IS DISTINCT FROM OLD.goal_id
        OR NEW.goal_revision IS DISTINCT FROM OLD.goal_revision
        OR NEW.work_item_id IS DISTINCT FROM OLD.work_item_id
        OR NEW.kind IS DISTINCT FROM OLD.kind
        OR NEW.action_hash IS DISTINCT FROM OLD.action_hash
        OR NEW.subject_hash IS DISTINCT FROM OLD.subject_hash
        OR NEW.state IS DISTINCT FROM OLD.state
        OR NEW.question IS DISTINCT FROM OLD.question
        OR NEW.options IS DISTINCT FROM OLD.options
        OR NEW.resolution IS DISTINCT FROM OLD.resolution
        OR NEW.actor_ref IS DISTINCT FROM OLD.actor_ref
        OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
      ) THEN
        RAISE EXCEPTION 'goal_0006_terminal_decision_immutable';
      END IF;

      RETURN NEW;
    END;
    $$;
    """)
  end

  defp restore_session_run_identity_guard! do
    execute("""
    CREATE OR REPLACE FUNCTION goal_0006_validate_active_session_run_identity()
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
    $$;
    """)
  end
end
