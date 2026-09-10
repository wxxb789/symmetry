defmodule SymmetryControl.Repo.Migrations.AddGoalExternalWaitUnsupportedState do
  use Ecto.Migration

  def up do
    drop(constraint(:goal_external_waits, :goal_external_waits_next_check_state_check))
    drop(constraint(:goal_external_waits, :goal_external_waits_state_check))

    replace_history_guard!(["satisfied", "failed", "cancelled", "unsupported"], true)

    # Older rows were created before a server-owned Integration checker existed.
    # Preserve their immutable source records, but stop their model-driven polling.
    execute("ALTER TABLE goal_external_waits DISABLE TRIGGER goal_external_waits_history_guard")

    execute("""
    UPDATE goal_external_waits
    SET state = 'unsupported', next_check_at = NULL
    WHERE state IN ('waiting', 'checking', 'unknown')
    """)

    execute("ALTER TABLE goal_external_waits ENABLE TRIGGER goal_external_waits_history_guard")

    execute("ALTER TABLE goal_external_waits ALTER COLUMN state SET DEFAULT 'unsupported'")

    create(
      constraint(:goal_external_waits, :goal_external_waits_state_check,
        check: "state IN ('satisfied', 'failed', 'cancelled', 'unsupported')"
      )
    )

    create(
      constraint(:goal_external_waits, :goal_external_waits_next_check_state_check,
        check:
          "state IN ('satisfied', 'failed', 'cancelled', 'unsupported') AND next_check_at IS NULL"
      )
    )
  end

  def down do
    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM goal_external_waits WHERE state = 'unsupported') THEN
        RAISE EXCEPTION 'cannot roll back unsupported external waits while history exists';
      END IF;
    END
    $$;
    """)

    drop(constraint(:goal_external_waits, :goal_external_waits_next_check_state_check))
    drop(constraint(:goal_external_waits, :goal_external_waits_state_check))

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

    execute("ALTER TABLE goal_external_waits ALTER COLUMN state SET DEFAULT 'waiting'")

    replace_history_guard!(["satisfied", "failed", "cancelled"], false)
  end

  defp replace_history_guard!(terminal_states, require_receipt_on_insert?) do
    terminal_states = terminal_states |> Enum.map(&"'#{&1}'") |> Enum.join(", ")

    insert_guard =
      if require_receipt_on_insert? do
        """
        IF NEW.receipt_event_id IS NULL THEN
          RAISE EXCEPTION 'goal_0006_external_wait_receipt_required';
        END IF;
        """
      else
        ""
      end

    execute("""
    CREATE OR REPLACE FUNCTION goal_0006_guard_external_wait_history()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF TG_OP = 'INSERT' THEN
        #{insert_guard}
        RETURN NEW;
      END IF;

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

      IF OLD.state IN (#{terminal_states}) AND (
           NEW.state IS DISTINCT FROM OLD.state
        OR NEW.next_check_at IS DISTINCT FROM OLD.next_check_at
        OR NEW.check_seq IS DISTINCT FROM OLD.check_seq
        OR NEW.receipt_event_id IS DISTINCT FROM OLD.receipt_event_id
      ) THEN
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
  end
end
