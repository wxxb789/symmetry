defmodule SymmetryControl.Repo.Migrations.AddHarnessSessionStopReceipts do
  use Ecto.Migration

  def up do
    alter table(:harness_sessions) do
      add :binding_id, :binary_id
    end

    # Existing sessions have no daemon-held attachment identity. Give them a
    # deterministic opaque value so this migration neither assumes an installed
    # UUID extension nor makes an old retained session resumable by accident.
    execute("""
    UPDATE harness_sessions
    SET binding_id = (
      '00000000-0000-4000-8000-' || substring(md5(id::text), 1, 12)
    )::uuid
    WHERE binding_id IS NULL
    """)

    alter table(:harness_sessions) do
      modify :binding_id, :binary_id, null: false
    end

    alter table(:runs) do
      add :harness_binding_id, :binary_id
    end

    execute("""
    UPDATE runs AS run
    SET harness_binding_id = session.binding_id
    FROM harness_sessions AS session
    WHERE run.harness_session_id = session.id
      AND run.harness_binding_id IS NULL
    """)

    create constraint(:runs, :runs_harness_attachment_identity_check,
             check: "(harness_session_id IS NULL) = (harness_binding_id IS NULL)"
           )

    create_attachment_identity_guards!()

    create table(:harness_session_stop_receipts, primary_key: false) do
      add :id, :binary_id, primary_key: true

      add :session_id,
          references(:harness_sessions,
            type: :binary_id,
            on_delete: :restrict,
            name: :harness_session_stop_receipts_session_id_fkey
          ),
          null: false

      add :run_id,
          references(:runs,
            type: :binary_id,
            on_delete: :restrict,
            name: :harness_session_stop_receipts_run_id_fkey
          ),
          null: false

      add :machine_id,
          references(:machines,
            type: :binary_id,
            on_delete: :restrict,
            name: :harness_session_stop_receipts_machine_id_fkey
          ),
          null: false

      add :binding_id, :binary_id, null: false
      add :request_hash, :binary, null: false
      add :response, :map, null: false
      add :inserted_at, :utc_datetime_usec, null: false
    end

    create unique_index(:harness_session_stop_receipts, [:session_id, :binding_id],
             name: :harness_session_stop_receipts_session_id_binding_id_key
           )

    create index(:harness_session_stop_receipts, [:run_id])

    create constraint(
             :harness_session_stop_receipts,
             :harness_session_stop_receipts_request_hash_size,
             check: "octet_length(request_hash) = 32"
           )

    create constraint(
             :harness_session_stop_receipts,
             :harness_session_stop_receipts_response_object,
             check: "jsonb_typeof(response) = 'object'"
           )

    create_stop_receipt_guard!()
  end

  def down do
    refuse_receipt_history_rollback!()

    execute("DROP TRIGGER runs_harness_attachment_binding_guard ON runs")
    execute("DROP FUNCTION goal_0006_freeze_run_harness_attachment_binding()")
    execute("DROP TRIGGER harness_sessions_binding_rotation_guard ON harness_sessions")
    execute("DROP FUNCTION goal_0006_validate_harness_session_binding_rotation()")

    execute(
      "DROP TRIGGER harness_session_stop_receipts_identity_guard ON harness_session_stop_receipts"
    )

    execute("DROP FUNCTION goal_0006_validate_harness_session_stop_receipt()")

    drop index(:harness_session_stop_receipts, [:run_id])

    drop index(:harness_session_stop_receipts, [:session_id, :binding_id],
           name: :harness_session_stop_receipts_session_id_binding_id_key
         )

    drop table(:harness_session_stop_receipts)

    drop constraint(:runs, :runs_harness_attachment_identity_check)

    alter table(:runs) do
      remove :harness_binding_id
    end

    alter table(:harness_sessions) do
      remove :binding_id
    end
  end

  defp refuse_receipt_history_rollback! do
    execute("""
    LOCK TABLE harness_sessions, harness_session_stop_receipts IN SHARE MODE;

    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM harness_session_stop_receipts)
         OR EXISTS (SELECT 1 FROM harness_sessions WHERE state <> 'closed') THEN
        RAISE EXCEPTION
          'cannot roll back harness session stop receipts while durable session state exists';
      END IF;
    END
    $$;
    """)
  end

  defp create_attachment_identity_guards! do
    execute("""
    CREATE FUNCTION goal_0006_validate_harness_session_binding_rotation()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF NEW.binding_id IS DISTINCT FROM OLD.binding_id
         AND NOT (
           OLD.state = 'available'
           AND OLD.active_run_id IS NULL
           AND NEW.state = 'busy'
           AND NEW.active_run_id IS NOT NULL
         ) THEN
        RAISE EXCEPTION 'goal_0006_harness_session_binding_rotation_invalid';
      END IF;

      RETURN NEW;
    END;
    $$;
    """)

    execute("""
    CREATE TRIGGER harness_sessions_binding_rotation_guard
    BEFORE UPDATE OF binding_id ON harness_sessions
    FOR EACH ROW EXECUTE FUNCTION goal_0006_validate_harness_session_binding_rotation()
    """)

    execute("""
    CREATE FUNCTION goal_0006_freeze_run_harness_attachment_binding()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF OLD.harness_session_id IS NOT NULL
         AND (
           NEW.harness_session_id IS DISTINCT FROM OLD.harness_session_id
           OR NEW.harness_binding_id IS DISTINCT FROM OLD.harness_binding_id
         ) THEN
        RAISE EXCEPTION 'goal_0006_run_harness_attachment_binding_immutable';
      END IF;

      RETURN NEW;
    END;
    $$;
    """)

    execute("""
    CREATE TRIGGER runs_harness_attachment_binding_guard
    BEFORE UPDATE OF harness_session_id, harness_binding_id ON runs
    FOR EACH ROW EXECUTE FUNCTION goal_0006_freeze_run_harness_attachment_binding()
    """)
  end

  defp create_stop_receipt_guard! do
    execute("""
    CREATE FUNCTION goal_0006_validate_harness_session_stop_receipt()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
      session_machine_id uuid;
      session_binding_id uuid;
      session_state text;
      session_active_run_id uuid;
      run_session_id uuid;
      run_binding_id uuid;
      run_state text;
    BEGIN
      IF TG_OP = 'UPDATE' THEN
        RAISE EXCEPTION 'goal_0006_harness_session_stop_receipt_immutable';
      END IF;

      SELECT machine_id, binding_id, state, active_run_id
      INTO session_machine_id, session_binding_id, session_state, session_active_run_id
      FROM harness_sessions
      WHERE id = NEW.session_id
      FOR KEY SHARE;

      SELECT harness_session_id, harness_binding_id, state
      INTO run_session_id, run_binding_id, run_state
      FROM runs
      WHERE id = NEW.run_id
      FOR KEY SHARE;

      IF NOT FOUND
         OR session_machine_id IS DISTINCT FROM NEW.machine_id
         OR session_binding_id IS DISTINCT FROM NEW.binding_id
         OR session_state IS DISTINCT FROM 'unavailable'
         OR session_active_run_id IS NOT NULL
         OR run_session_id IS DISTINCT FROM NEW.session_id
         OR run_binding_id IS DISTINCT FROM NEW.binding_id
         OR run_state NOT IN ('completed', 'failed', 'cancelled', 'expired') THEN
        RAISE EXCEPTION 'goal_0006_harness_session_stop_receipt_identity';
      END IF;

      RETURN NEW;
    END;
    $$;
    """)

    execute("""
    CREATE TRIGGER harness_session_stop_receipts_identity_guard
    BEFORE INSERT OR UPDATE ON harness_session_stop_receipts
    FOR EACH ROW EXECUTE FUNCTION goal_0006_validate_harness_session_stop_receipt()
    """)
  end
end
