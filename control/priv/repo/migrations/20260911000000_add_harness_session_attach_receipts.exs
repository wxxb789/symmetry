defmodule SymmetryControl.Repo.Migrations.AddHarnessSessionAttachReceipts do
  use Ecto.Migration

  def up do
    create table(:harness_session_attach_receipts, primary_key: false) do
      add :id, :binary_id, primary_key: true

      add :session_id,
          references(:harness_sessions,
            type: :binary_id,
            on_delete: :restrict,
            name: :harness_session_attach_receipts_session_id_fkey
          ),
          null: false

      add :run_id,
          references(:runs,
            type: :binary_id,
            on_delete: :restrict,
            name: :harness_session_attach_receipts_run_id_fkey
          ),
          null: false

      add :runtime_id,
          references(:runtimes,
            type: :binary_id,
            on_delete: :restrict,
            name: :harness_session_attach_receipts_runtime_id_fkey
          ),
          null: false

      add :machine_id, :binary_id, null: false
      add :runtime_epoch, :bigint, null: false
      add :generation, :bigint, null: false
      add :claim_id, :binary_id, null: false
      add :lease_token, :binary_id, null: false
      add :request_hash, :binary, null: false
      add :response, :map, null: false
      add :inserted_at, :utc_datetime_usec, null: false
    end

    create unique_index(:harness_session_attach_receipts, [:run_id],
             name: :harness_session_attach_receipts_run_id_index
           )

    create index(:harness_session_attach_receipts, [:session_id])

    create constraint(
             :harness_session_attach_receipts,
             :harness_session_attach_receipts_epoch_check,
             check: "runtime_epoch > 0"
           )

    create constraint(
             :harness_session_attach_receipts,
             :harness_session_attach_receipts_generation_check,
             check: "generation > 0"
           )

    create constraint(
             :harness_session_attach_receipts,
             :harness_session_attach_receipts_request_hash_size,
             check: "octet_length(request_hash) = 32"
           )

    create constraint(
             :harness_session_attach_receipts,
             :harness_session_attach_receipts_response_object,
             check: "jsonb_typeof(response) = 'object'"
           )

    create_attach_receipt_guard!()
  end

  def down do
    execute("LOCK TABLE harness_session_attach_receipts IN SHARE MODE")

    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM harness_session_attach_receipts) THEN
        RAISE EXCEPTION
          'cannot roll back harness session attach receipts while durable receipt history exists';
      END IF;
    END
    $$;
    """)

    execute(
      "DROP TRIGGER harness_session_attach_receipts_identity_guard ON harness_session_attach_receipts"
    )

    execute("DROP FUNCTION goal_0006_validate_harness_session_attach_receipt()")

    drop index(:harness_session_attach_receipts, [:session_id])

    drop index(:harness_session_attach_receipts, [:run_id],
           name: :harness_session_attach_receipts_run_id_index
         )

    drop table(:harness_session_attach_receipts)
  end

  defp create_attach_receipt_guard! do
    execute("""
    CREATE FUNCTION goal_0006_validate_harness_session_attach_receipt()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
      session_machine_id uuid;
      session_runtime_id uuid;
      session_binding_id uuid;
      session_binding_verified boolean;
      session_local_handle_id uuid;
      session_repository_resource_id uuid;
      session_harness_kind text;
      session_harness_version text;
      session_adapter_version text;
      session_workspace_fingerprint text;
      session_state text;
      session_active_run_id uuid;
      run_runtime_id uuid;
      run_task_id uuid;
      run_session_id uuid;
      run_binding_id uuid;
      run_generation bigint;
      run_runtime_epoch bigint;
      run_claim_id uuid;
      run_lease_token uuid;
      task_workspace text;
      response_session jsonb;
    BEGIN
      IF TG_OP <> 'INSERT' THEN
        RAISE EXCEPTION 'goal_0006_harness_session_attach_receipt_immutable';
      END IF;

      SELECT machine_id, runtime_id, binding_id, binding_verified, local_handle_id,
             repository_resource_id, harness_kind, harness_version, adapter_version,
             workspace_fingerprint, state, active_run_id
      INTO session_machine_id, session_runtime_id, session_binding_id, session_binding_verified,
           session_local_handle_id, session_repository_resource_id, session_harness_kind,
           session_harness_version, session_adapter_version, session_workspace_fingerprint,
           session_state, session_active_run_id
      FROM harness_sessions
      WHERE id = NEW.session_id
      FOR KEY SHARE;

      IF NOT FOUND THEN
        RAISE EXCEPTION 'goal_0006_harness_session_attach_receipt_identity';
      END IF;

      SELECT runtime_id, task_id, harness_session_id, harness_binding_id, generation,
             claimed_runtime_epoch, claim_id, lease_token
      INTO run_runtime_id, run_task_id, run_session_id, run_binding_id, run_generation,
           run_runtime_epoch, run_claim_id, run_lease_token
      FROM runs
      WHERE id = NEW.run_id
      FOR KEY SHARE;

      IF NOT FOUND
         OR session_machine_id IS DISTINCT FROM NEW.machine_id
         OR session_runtime_id IS DISTINCT FROM NEW.runtime_id
         OR run_runtime_id IS DISTINCT FROM NEW.runtime_id
         OR run_session_id IS DISTINCT FROM NEW.session_id
         OR run_binding_id IS DISTINCT FROM session_binding_id
         OR session_binding_verified IS DISTINCT FROM TRUE
         OR session_state IS DISTINCT FROM 'busy'
         OR session_active_run_id IS DISTINCT FROM NEW.run_id
         OR run_generation IS DISTINCT FROM NEW.generation
         OR run_runtime_epoch IS DISTINCT FROM NEW.runtime_epoch
         OR run_claim_id IS DISTINCT FROM NEW.claim_id
         OR run_lease_token IS DISTINCT FROM NEW.lease_token THEN
        RAISE EXCEPTION 'goal_0006_harness_session_attach_receipt_identity';
      END IF;

      SELECT workspace
      INTO task_workspace
      FROM tasks
      WHERE id = run_task_id
      FOR KEY SHARE;

      IF NOT FOUND THEN
        RAISE EXCEPTION 'goal_0006_harness_session_attach_receipt_identity';
      END IF;

      response_session := NEW.response -> 'session';

      IF jsonb_typeof(response_session) IS DISTINCT FROM 'object'
         OR response_session ->> 'attachment_receipt_id' IS DISTINCT FROM NEW.id::text
         OR response_session ->> 'run_id' IS DISTINCT FROM NEW.run_id::text
         OR response_session ->> 'session_id' IS DISTINCT FROM NEW.session_id::text
         OR response_session ->> 'id' IS DISTINCT FROM NEW.session_id::text
         OR response_session ->> 'runtime_id' IS DISTINCT FROM NEW.runtime_id::text
         OR response_session ->> 'machine_id' IS DISTINCT FROM NEW.machine_id::text
         OR response_session ->> 'repository_resource_id' IS DISTINCT FROM session_repository_resource_id::text
         OR response_session ->> 'active_run_id' IS DISTINCT FROM NEW.run_id::text
         OR response_session ->> 'local_handle_id' IS DISTINCT FROM session_local_handle_id::text
         OR response_session ->> 'binding_id' IS DISTINCT FROM session_binding_id::text
         OR response_session ->> 'harness_kind' IS DISTINCT FROM session_harness_kind
         OR response_session ->> 'harness_version' IS DISTINCT FROM session_harness_version
         OR response_session ->> 'adapter_version' IS DISTINCT FROM session_adapter_version
         OR response_session ->> 'workspace_fingerprint' IS DISTINCT FROM session_workspace_fingerprint
         OR response_session ->> 'workspace' IS DISTINCT FROM task_workspace
         OR response_session ->> 'state' IS DISTINCT FROM 'busy' THEN
        RAISE EXCEPTION 'goal_0006_harness_session_attach_receipt_response';
      END IF;

      RETURN NEW;
    END;
    $$;
    """)

    execute("""
    CREATE TRIGGER harness_session_attach_receipts_identity_guard
    BEFORE INSERT OR UPDATE OR DELETE ON harness_session_attach_receipts
    FOR EACH ROW EXECUTE FUNCTION goal_0006_validate_harness_session_attach_receipt()
    """)
  end
end
