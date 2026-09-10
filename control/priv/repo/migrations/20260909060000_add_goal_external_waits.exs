defmodule SymmetryControl.Repo.Migrations.AddGoalExternalWaits do
  use Ecto.Migration

  def up do
    create(
      unique_index(:runs, [:id, :task_id, :generation], name: :runs_id_task_id_generation_key)
    )

    create(unique_index(:goal_events, [:id, :goal_id], name: :goal_events_id_goal_id_key))

    create table(:goal_external_waits, primary_key: false) do
      add(:id, :binary_id, primary_key: true)
      add(:goal_id, :binary_id, null: false)
      add(:goal_revision, :integer, null: false)
      add(:work_item_id, :binary_id, null: false)
      add(:task_id, :binary_id, null: false)
      add(:run_id, :binary_id, null: false)
      add(:run_generation, :integer, null: false)
      add(:source_ref, :map, null: false)
      add(:result_id, :binary_id, null: false)
      add(:result, :map, null: false)
      add(:resource_id, :binary_id, null: false)
      add(:external_ref, :text, null: false)
      add(:subject, :map, null: false)
      add(:subject_hash, :binary, null: false)
      add(:next_check_at, :utc_datetime_usec)
      add(:state, :text, null: false, default: "waiting")
      add(:check_seq, :bigint, null: false, default: 0)
      add(:receipt_event_id, :binary_id)
      timestamps(type: :utc_datetime_usec)
    end

    create(
      index(:goal_external_waits, [:goal_id, :next_check_at],
        where: "state IN ('waiting', 'checking', 'unknown')",
        name: :goal_external_waits_due_goal_key
      )
    )

    create(
      index(:goal_external_waits, [:state, :next_check_at],
        where: "state IN ('waiting', 'checking', 'unknown')",
        name: :goal_external_waits_due_state_key
      )
    )

    create(index(:goal_external_waits, [:task_id, :run_id], name: :goal_external_waits_owner_key))

    create(
      index(:goal_external_waits, [:receipt_event_id],
        where: "receipt_event_id IS NOT NULL",
        name: :goal_external_waits_receipt_event_key
      )
    )

    create(
      unique_index(
        :goal_external_waits,
        [:goal_id, :goal_revision, :task_id, :run_id, :result_id],
        name: :goal_external_waits_source_identity_key
      )
    )

    create(
      unique_index(:goal_external_waits, [:goal_id, :goal_revision, :work_item_id],
        where: "state IN ('waiting', 'checking', 'unknown')",
        name: :goal_external_waits_current_work_item_key
      )
    )

    execute("""
    ALTER TABLE goal_external_waits
    ADD CONSTRAINT goal_external_waits_goal_id_fkey
    FOREIGN KEY (goal_id)
    REFERENCES goals (id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE goal_external_waits
    ADD CONSTRAINT goal_external_waits_goal_revision_fkey
    FOREIGN KEY (goal_id, goal_revision)
    REFERENCES goal_revisions (goal_id, revision)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE goal_external_waits
    ADD CONSTRAINT goal_external_waits_work_item_id_fkey
    FOREIGN KEY (work_item_id)
    REFERENCES work_items (id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE goal_external_waits
    ADD CONSTRAINT goal_external_waits_work_item_identity_fkey
    FOREIGN KEY (work_item_id, goal_id)
    REFERENCES work_items (id, goal_id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE goal_external_waits
    ADD CONSTRAINT goal_external_waits_task_id_fkey
    FOREIGN KEY (task_id)
    REFERENCES tasks (id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE goal_external_waits
    ADD CONSTRAINT goal_external_waits_task_identity_fkey
    FOREIGN KEY (task_id, work_item_id, goal_id, goal_revision)
    REFERENCES tasks (id, work_item_id, goal_id, goal_revision)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE goal_external_waits
    ADD CONSTRAINT goal_external_waits_run_id_fkey
    FOREIGN KEY (run_id)
    REFERENCES runs (id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE goal_external_waits
    ADD CONSTRAINT goal_external_waits_run_identity_fkey
    FOREIGN KEY (run_id, task_id, run_generation)
    REFERENCES runs (id, task_id, generation)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE goal_external_waits
    ADD CONSTRAINT goal_external_waits_resource_id_fkey
    FOREIGN KEY (resource_id)
    REFERENCES project_resources (id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE goal_external_waits
    ADD CONSTRAINT goal_external_waits_receipt_event_id_fkey
    FOREIGN KEY (receipt_event_id)
    REFERENCES goal_events (id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE goal_external_waits
    ADD CONSTRAINT goal_external_waits_receipt_event_identity_fkey
    FOREIGN KEY (receipt_event_id, goal_id)
    REFERENCES goal_events (id, goal_id)
    ON DELETE RESTRICT
    """)

    create(
      constraint(:goal_external_waits, :goal_external_waits_goal_revision_positive,
        check: "goal_revision > 0"
      )
    )

    create(
      constraint(:goal_external_waits, :goal_external_waits_run_generation_positive,
        check: "run_generation > 0"
      )
    )

    create(
      constraint(:goal_external_waits, :goal_external_waits_state_check,
        check: "state IN ('waiting', 'checking', 'unknown', 'satisfied', 'failed', 'cancelled')"
      )
    )

    create(
      constraint(:goal_external_waits, :goal_external_waits_next_check_state_check,
        check:
          "((state IN ('waiting', 'checking', 'unknown') AND next_check_at IS NOT NULL) OR (state IN ('satisfied', 'failed', 'cancelled') AND next_check_at IS NULL))"
      )
    )

    create(
      constraint(:goal_external_waits, :goal_external_waits_source_ref_object,
        check: "jsonb_typeof(source_ref) = 'object'"
      )
    )

    create(
      constraint(:goal_external_waits, :goal_external_waits_result_object,
        check: "jsonb_typeof(result) = 'object'"
      )
    )

    create(
      constraint(:goal_external_waits, :goal_external_waits_subject_object,
        check: subject_check()
      )
    )

    create(
      constraint(:goal_external_waits, :goal_external_waits_subject_hash_size,
        check: "octet_length(subject_hash) = 32"
      )
    )

    create(
      constraint(:goal_external_waits, :goal_external_waits_external_ref_present,
        check: "NULLIF(BTRIM(external_ref), '') IS NOT NULL"
      )
    )

    create(
      constraint(:goal_external_waits, :goal_external_waits_result_identity_check,
        check:
          "result ->> 'result_id' = result_id::text AND result -> 'subject' = subject AND result ->> 'subject_hash' = ('sha256:' || encode(subject_hash, 'hex'))"
      )
    )

    create(
      constraint(:goal_external_waits, :goal_external_waits_check_seq_nonnegative,
        check: "check_seq >= 0"
      )
    )

    execute("""
    CREATE FUNCTION goal_0006_validate_external_wait_identity()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
      goal_project_id uuid;
      item_goal_id uuid;
      item_revision integer;
      item_repository_resource_id uuid;
      task_goal_id uuid;
      task_revision integer;
      task_work_item_id uuid;
      task_generation integer;
      task_state text;
      run_task_id uuid;
      run_generation integer;
      run_state text;
      resource_project_id uuid;
      run_task_result jsonb;
    BEGIN
      SELECT project_id
      INTO goal_project_id
      FROM goals
      WHERE id = NEW.goal_id;

      SELECT goal_id, admitted_revision, repository_resource_id
      INTO item_goal_id, item_revision, item_repository_resource_id
      FROM work_items
      WHERE id = NEW.work_item_id;

      SELECT goal_id, goal_revision, work_item_id, current_generation, state
      INTO task_goal_id, task_revision, task_work_item_id, task_generation, task_state
      FROM tasks
      WHERE id = NEW.task_id;

      SELECT task_id, generation, state, result
      INTO run_task_id, run_generation, run_state, run_task_result
      FROM runs
      WHERE id = NEW.run_id;

      SELECT project_id
      INTO resource_project_id
      FROM project_resources
      WHERE id = NEW.resource_id;

      IF item_goal_id IS DISTINCT FROM NEW.goal_id
         OR item_revision IS DISTINCT FROM NEW.goal_revision
         OR task_goal_id IS DISTINCT FROM NEW.goal_id
         OR task_revision IS DISTINCT FROM NEW.goal_revision
         OR task_work_item_id IS DISTINCT FROM NEW.work_item_id
         OR run_task_id IS DISTINCT FROM NEW.task_id
         OR run_generation IS DISTINCT FROM NEW.run_generation
         OR task_generation IS DISTINCT FROM NEW.run_generation
         OR task_state IS DISTINCT FROM 'completed'
         OR run_state IS DISTINCT FROM 'completed'
         OR goal_project_id IS DISTINCT FROM resource_project_id
         OR item_repository_resource_id IS NULL
         OR NEW.subject ->> 'resource_id' IS DISTINCT FROM item_repository_resource_id::text
         OR run_task_result -> 'task_result' IS DISTINCT FROM NEW.result THEN
        RAISE EXCEPTION 'goal_0006_external_wait_owner_identity';
      END IF;

      RETURN NEW;
    END;
    $$
    """)

    execute("""
    CREATE TRIGGER goal_external_waits_identity_guard
    BEFORE INSERT OR UPDATE OF goal_id, goal_revision, work_item_id, task_id, run_id,
      run_generation, source_ref, result_id, result, resource_id, external_ref,
      subject, subject_hash
    ON goal_external_waits
    FOR EACH ROW EXECUTE FUNCTION goal_0006_validate_external_wait_identity()
    """)

    execute("""
    CREATE FUNCTION goal_0006_guard_external_wait_history()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'goal_0006_external_wait_history_immutable';
      END IF;

      IF OLD.goal_id IS DISTINCT FROM NEW.goal_id
         OR OLD.goal_revision IS DISTINCT FROM NEW.goal_revision
         OR OLD.work_item_id IS DISTINCT FROM NEW.work_item_id
         OR OLD.task_id IS DISTINCT FROM NEW.task_id
         OR OLD.run_id IS DISTINCT FROM NEW.run_id
         OR OLD.run_generation IS DISTINCT FROM NEW.run_generation
         OR OLD.source_ref IS DISTINCT FROM NEW.source_ref
         OR OLD.result_id IS DISTINCT FROM NEW.result_id
         OR OLD.result IS DISTINCT FROM NEW.result
         OR OLD.resource_id IS DISTINCT FROM NEW.resource_id
         OR OLD.external_ref IS DISTINCT FROM NEW.external_ref
         OR OLD.subject IS DISTINCT FROM NEW.subject
         OR OLD.subject_hash IS DISTINCT FROM NEW.subject_hash THEN
        RAISE EXCEPTION 'goal_0006_external_wait_identity_immutable';
      END IF;

      IF OLD.state IN ('satisfied', 'failed', 'cancelled')
         AND NEW.state IS DISTINCT FROM OLD.state THEN
        RAISE EXCEPTION 'goal_0006_external_wait_terminal_immutable';
      END IF;

      IF NEW.check_seq < OLD.check_seq OR NEW.check_seq > OLD.check_seq + 1 THEN
        RAISE EXCEPTION 'goal_0006_external_wait_check_seq_conflict';
      END IF;

      IF NEW.check_seq = OLD.check_seq
         AND (NEW.next_check_at IS DISTINCT FROM OLD.next_check_at
              OR NEW.state IS DISTINCT FROM OLD.state
              OR NEW.receipt_event_id IS DISTINCT FROM OLD.receipt_event_id) THEN
        RAISE EXCEPTION 'goal_0006_external_wait_check_seq_required';
      END IF;

      IF NEW.check_seq = OLD.check_seq + 1
         AND (NEW.receipt_event_id IS NULL
              OR NEW.receipt_event_id IS NOT DISTINCT FROM OLD.receipt_event_id) THEN
        RAISE EXCEPTION 'goal_0006_external_wait_receipt_required';
      END IF;

      RETURN NEW;
    END;
    $$
    """)

    execute("""
    CREATE TRIGGER goal_external_waits_history_guard
    BEFORE UPDATE OR DELETE ON goal_external_waits
    FOR EACH ROW EXECUTE FUNCTION goal_0006_guard_external_wait_history()
    """)

    execute("""
    CREATE FUNCTION goal_0006_freeze_external_wait_resource_identity()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF EXISTS (
        SELECT 1
        FROM goal_external_waits
        WHERE resource_id = OLD.id
      ) AND (
           NEW.project_id IS DISTINCT FROM OLD.project_id
        OR NEW.kind IS DISTINCT FROM OLD.kind
        OR NEW.provider IS DISTINCT FROM OLD.provider
        OR NEW.external_ref IS DISTINCT FROM OLD.external_ref
        OR NEW.connection_id IS DISTINCT FROM OLD.connection_id
      ) THEN
        RAISE EXCEPTION 'goal_0006_external_wait_resource_identity_immutable';
      END IF;

      RETURN NEW;
    END;
    $$
    """)

    execute("""
    CREATE TRIGGER project_resources_external_wait_identity_guard
    BEFORE UPDATE OF project_id, kind, provider, external_ref, connection_id
    ON project_resources
    FOR EACH ROW EXECUTE FUNCTION goal_0006_freeze_external_wait_resource_identity()
    """)
  end

  def down do
    execute("""
    LOCK TABLE
      goals,
      goal_revisions,
      goal_events,
      work_items,
      tasks,
      runs,
      project_resources,
      goal_external_waits
    IN SHARE MODE
    """)

    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM goal_external_waits) THEN
        RAISE EXCEPTION 'cannot roll back Goal external waits while history exists';
      END IF;
    END
    $$;
    """)

    execute("DROP TRIGGER project_resources_external_wait_identity_guard ON project_resources")
    execute("DROP FUNCTION goal_0006_freeze_external_wait_resource_identity()")
    execute("DROP TRIGGER goal_external_waits_history_guard ON goal_external_waits")
    execute("DROP FUNCTION goal_0006_guard_external_wait_history()")
    execute("DROP TRIGGER goal_external_waits_identity_guard ON goal_external_waits")
    execute("DROP FUNCTION goal_0006_validate_external_wait_identity()")

    drop(table(:goal_external_waits))
    drop(index(:goal_events, [:id, :goal_id], name: :goal_events_id_goal_id_key))
    drop(index(:runs, [:id, :task_id, :generation], name: :runs_id_task_id_generation_key))
  end

  defp subject_check do
    """
    jsonb_typeof(subject) = 'object'
    AND subject ?& ARRAY['resource_id', 'commit', 'tree_digest']
    AND subject - 'resource_id' - 'commit' - 'tree_digest' = '{}'::jsonb
    AND jsonb_typeof(subject -> 'resource_id') = 'string'
    AND jsonb_typeof(subject -> 'commit') = 'string'
    AND jsonb_typeof(subject -> 'tree_digest') = 'string'
    AND NULLIF(BTRIM(subject ->> 'resource_id'), '') IS NOT NULL
    AND NULLIF(BTRIM(subject ->> 'commit'), '') IS NOT NULL
    AND NULLIF(BTRIM(subject ->> 'tree_digest'), '') IS NOT NULL
    AND subject ->> 'resource_id' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
    AND subject ->> 'commit' ~ '^(?:[0-9a-f]{40}|[0-9a-f]{64})$'
    AND subject ->> 'tree_digest' ~ '^sha256:[0-9a-f]{64}$'
    """
  end
end
