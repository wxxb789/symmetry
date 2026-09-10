defmodule SymmetryControl.Repo.Migrations.AddGoal0006ControlPlane do
  use Ecto.Migration

  @history_tables [
    "goals",
    "goal_revisions",
    "work_dependencies",
    "context_snapshots",
    "harness_sessions",
    "run_evidence",
    "work_outcomes",
    "goal_decisions",
    "goal_events",
    "goal_budget_reservations",
    "run_usage"
  ]

  def up do
    add_existing_columns()
    create_json_guards()
    create_goals()
    create_goal_revisions()
    add_goal_ownership_constraints()
    create_context_snapshots()
    add_task_context_constraint()
    create_harness_sessions()
    add_session_constraints()
    create_work_dependencies()
    create_run_evidence()
    create_goal_decisions()
    create_work_outcomes()
    create_goal_events()
    create_budget_and_usage()
    create_immutability_guards()
  end

  def down do
    refuse_destructive_rollback!()

    drop_immutability_guards()
    drop table(:run_usage)
    drop table(:goal_budget_reservations)
    drop table(:goal_events)
    drop table(:work_outcomes)
    drop table(:goal_decisions)
    drop table(:run_evidence)
    drop table(:work_dependencies)

    drop constraint(:tasks, :tasks_context_snapshot_ownership_fkey)
    drop table(:context_snapshots)

    drop constraint(:tasks, :tasks_requested_session_id_fkey)
    drop constraint(:harness_sessions, :harness_sessions_active_run_id_fkey)
    drop constraint(:runs, :runs_harness_session_id_fkey)
    drop table(:harness_sessions)

    drop_task_constraints()
    drop_work_item_constraints()

    drop constraint(:goals, :goals_current_revision_fkey)
    drop table(:goal_revisions)
    drop table(:goals)
    drop_json_guards()

    alter table(:runs) do
      remove :harness_session_id
    end

    alter table(:tasks) do
      remove :requested_session_id
      remove :max_run_attempts
      remove :admission_key
      remove :validation_of_task_id
      remove :purpose
      remove :context_snapshot_id
      remove :goal_revision
      remove :goal_id
      remove :work_item_id
    end

    alter table(:work_items) do
      remove :acceptance_contract
      remove :required
      remove :admitted_revision
      remove :goal_id
    end

    drop constraint(:runtimes, :runtimes_harness_kind_check)
    drop constraint(:runtimes, :runtimes_adapter_protocol_version_positive)
    drop index(:runtimes, [:id, :machine_id], name: :runtimes_id_machine_id_key)

    alter table(:runtimes) do
      remove :adapter_protocol_version
      remove :adapter_version
      remove :harness_version
      remove :harness_kind
    end
  end

  defp add_existing_columns do
    alter table(:runtimes) do
      add :harness_kind, :text
      add :harness_version, :text
      add :adapter_version, :text
      add :adapter_protocol_version, :integer
    end

    create constraint(:runtimes, :runtimes_harness_kind_check,
             check:
               "harness_kind IS NULL OR harness_kind IN ('codex', 'claude_code', 'pi', 'opencode', 'generic')"
           )

    create constraint(:runtimes, :runtimes_adapter_protocol_version_positive,
             check: "adapter_protocol_version IS NULL OR adapter_protocol_version > 0"
           )

    create unique_index(:runtimes, [:id, :machine_id], name: :runtimes_id_machine_id_key)

    alter table(:work_items) do
      add :goal_id, :binary_id
      add :admitted_revision, :integer
      add :required, :boolean, null: false, default: true
      add :acceptance_contract, :map
    end

    alter table(:tasks) do
      add :work_item_id, :binary_id
      add :goal_id, :binary_id
      add :goal_revision, :integer
      add :context_snapshot_id, :binary_id
      add :purpose, :text, null: false, default: "implement"
      add :validation_of_task_id, :binary_id
      add :admission_key, :binary_id
      add :max_run_attempts, :bigint
      add :requested_session_id, :binary_id
    end

    # A current pointer is unambiguous only when exactly one WorkItem names it.
    execute("""
    UPDATE tasks AS task
    SET work_item_id = membership.work_item_id
    FROM (
      SELECT task_id, work_item_id
      FROM (
        SELECT
          orchestration_task_id AS task_id,
          id AS work_item_id,
          COUNT(*) OVER (PARTITION BY orchestration_task_id) AS membership_count
        FROM work_items
        WHERE orchestration_task_id IS NOT NULL
      ) AS candidates
      WHERE membership_count = 1
    ) AS membership
    WHERE task.id = membership.task_id
      AND task.work_item_id IS NULL
    """)

    alter table(:runs) do
      add :harness_session_id, :binary_id
    end
  end

  defp create_goals do
    create table(:goals, primary_key: false) do
      add :id, :binary_id, primary_key: true

      add :project_id,
          references(:projects,
            type: :binary_id,
            on_delete: :restrict,
            name: :goals_project_id_fkey
          ),
          null: false

      add :title, :text, null: false
      add :state, :text, null: false, default: "draft"
      add :current_revision, :integer, null: false
      add :event_sequence, :bigint, null: false, default: 0
      add :next_wake_at, :utc_datetime_usec
      add :lock_version, :bigint, null: false, default: 1
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:goals, [:id, :project_id], name: :goals_id_project_id_key)
    create index(:goals, [:state, :next_wake_at])

    create constraint(:goals, :goals_title_present, check: "NULLIF(BTRIM(title), '') IS NOT NULL")

    create constraint(:goals, :goals_state_check,
             check: "state IN ('draft', 'active', 'paused', 'achieved', 'cancelled')"
           )

    create constraint(:goals, :goals_current_revision_positive, check: "current_revision > 0")
    create constraint(:goals, :goals_event_sequence_nonnegative, check: "event_sequence >= 0")
    create constraint(:goals, :goals_lock_version_positive, check: "lock_version > 0")
  end

  defp create_goal_revisions do
    create table(:goal_revisions, primary_key: false) do
      add :goal_id,
          references(:goals,
            type: :binary_id,
            on_delete: :restrict,
            name: :goal_revisions_goal_id_fkey
          ),
          primary_key: true

      add :revision, :integer, primary_key: true
      add :objective, :text, null: false
      add :non_goals, :map, null: false
      add :acceptance_contract, :map, null: false
      add :authority_policy, :map, null: false
      add :execution_policy, :map, null: false
      add :context_manifest, :map, null: false
      add :reason, :text, null: false
      add :actor_ref, :text, null: false
      add :inserted_at, :utc_datetime_usec, null: false
    end

    create constraint(:goal_revisions, :goal_revisions_revision_positive, check: "revision > 0")

    create constraint(:goal_revisions, :goal_revisions_objective_present,
             check: "NULLIF(BTRIM(objective), '') IS NOT NULL"
           )

    create constraint(:goal_revisions, :goal_revisions_non_goals_array,
             check: "goal_0006_jsonb_nonblank_string_array(non_goals)"
           )

    create constraint(:goal_revisions, :goal_revisions_contracts_are_objects,
             check:
               "jsonb_typeof(acceptance_contract) = 'object' AND jsonb_typeof(authority_policy) = 'object' AND jsonb_typeof(execution_policy) = 'object' AND jsonb_typeof(context_manifest) = 'object'"
           )

    create constraint(:goal_revisions, :goal_revisions_execution_policy_v1,
             check: execution_policy_check()
           )

    execute("""
    ALTER TABLE goals
    ADD CONSTRAINT goals_current_revision_fkey
    FOREIGN KEY (id, current_revision)
    REFERENCES goal_revisions (goal_id, revision)
    DEFERRABLE INITIALLY DEFERRED
    """)
  end

  defp add_goal_ownership_constraints do
    create constraint(:work_items, :work_items_goal_fields_all_or_none,
             check:
               "(goal_id IS NULL AND admitted_revision IS NULL AND acceptance_contract IS NULL) OR (goal_id IS NOT NULL AND admitted_revision IS NOT NULL AND acceptance_contract IS NOT NULL)"
           )

    create constraint(:work_items, :work_items_admitted_revision_positive,
             check: "admitted_revision IS NULL OR admitted_revision > 0"
           )

    create unique_index(:work_items, [:id, :goal_id], name: :work_items_id_goal_id_key)

    execute("""
    ALTER TABLE work_items
    ADD CONSTRAINT work_items_goal_project_ownership_fkey
    FOREIGN KEY (goal_id, project_id)
    REFERENCES goals (id, project_id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE work_items
    ADD CONSTRAINT work_items_goal_revision_fkey
    FOREIGN KEY (goal_id, admitted_revision)
    REFERENCES goal_revisions (goal_id, revision)
    ON DELETE RESTRICT
    """)

    create constraint(:tasks, :tasks_goal_fields_all_or_none, check: task_goal_fields_check())

    create constraint(:tasks, :tasks_goal_revision_positive,
             check: "goal_revision IS NULL OR goal_revision > 0"
           )

    create constraint(:tasks, :tasks_max_run_attempts_positive,
             check: "max_run_attempts IS NULL OR max_run_attempts BETWEEN 1 AND 9007199254740991"
           )

    create constraint(:tasks, :tasks_validation_not_self,
             check: "validation_of_task_id IS NULL OR validation_of_task_id <> id"
           )

    create constraint(:tasks, :tasks_purpose_check,
             check: "purpose IN ('implement', 'validate', 'plan', 'observe', 'chat')"
           )

    create unique_index(:tasks, [:goal_id, :admission_key],
             name: :tasks_goal_id_admission_key_key
           )

    create unique_index(:tasks, [:id, :work_item_id, :goal_id, :goal_revision],
             name: :tasks_id_goal_identity_key
           )

    create unique_index(:tasks, [:id, :goal_id, :goal_revision],
             name: :tasks_id_goal_revision_key
           )

    execute("""
    ALTER TABLE tasks
    ADD CONSTRAINT tasks_work_item_id_fkey
    FOREIGN KEY (work_item_id)
    REFERENCES work_items (id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE tasks
    ADD CONSTRAINT tasks_goal_work_item_membership_fkey
    FOREIGN KEY (work_item_id, goal_id)
    REFERENCES work_items (id, goal_id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE tasks
    ADD CONSTRAINT tasks_goal_revision_fkey
    FOREIGN KEY (goal_id, goal_revision)
    REFERENCES goal_revisions (goal_id, revision)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE tasks
    ADD CONSTRAINT tasks_validation_task_identity_fkey
    FOREIGN KEY (validation_of_task_id, work_item_id, goal_id, goal_revision)
    REFERENCES tasks (id, work_item_id, goal_id, goal_revision)
    ON DELETE RESTRICT
    """)

    execute("""
    CREATE FUNCTION goal_0006_reject_invalid_validation_task_producer()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
      producer_purpose text;
    BEGIN
      IF NEW.purpose = 'validate' THEN
        IF NEW.validation_of_task_id = NEW.id THEN
          RAISE EXCEPTION 'goal_0006_validation_task_must_not_validate_itself';
        END IF;

        SELECT purpose
        INTO producer_purpose
        FROM tasks
        WHERE id = NEW.validation_of_task_id;

        IF producer_purpose IS NULL OR producer_purpose = 'validate' THEN
          RAISE EXCEPTION 'goal_0006_validation_task_requires_nonvalidation_producer';
        END IF;

        IF EXISTS (
          SELECT 1
          FROM tasks AS validation
          WHERE validation.validation_of_task_id = NEW.id
            AND validation.purpose = 'validate'
        ) THEN
          RAISE EXCEPTION 'goal_0006_validation_task_cannot_produce_validation';
        END IF;
      END IF;

      RETURN NEW;
    END;
    $$
    """)

    execute("""
    CREATE TRIGGER tasks_validation_task_producer_guard
    BEFORE INSERT OR UPDATE OF purpose, validation_of_task_id ON tasks
    FOR EACH ROW EXECUTE FUNCTION goal_0006_reject_invalid_validation_task_producer()
    """)

    create unique_index(:tasks, [:work_item_id],
             where:
               "goal_id IS NOT NULL AND state IN ('queued', 'assigned', 'claimed', 'running', 'waiting_for_input', 'paused', 'cancelling')",
             name: :tasks_one_active_goal_task_per_work_item
           )

    create unique_index(:runs, [:id, :task_id], name: :runs_id_task_id_key)
  end

  defp create_context_snapshots do
    create table(:context_snapshots, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :goal_id, :binary_id, null: false
      add :goal_revision, :integer, null: false
      add :work_item_id, :binary_id, null: false
      add :schema_version, :integer, null: false
      add :content_hash, :binary, null: false
      add :payload, :map, null: false
      add :inserted_at, :utc_datetime_usec, null: false
    end

    create unique_index(:context_snapshots, [:id, :goal_id],
             name: :context_snapshots_id_goal_id_key
           )

    create unique_index(:context_snapshots, [:id, :goal_id, :goal_revision, :work_item_id],
             name: :context_snapshots_identity_key
           )

    create unique_index(
             :context_snapshots,
             [:goal_id, :goal_revision, :work_item_id, :content_hash],
             name: :context_snapshots_reusable_content_key
           )

    execute("""
    ALTER TABLE context_snapshots
    ADD CONSTRAINT context_snapshots_goal_revision_fkey
    FOREIGN KEY (goal_id, goal_revision)
    REFERENCES goal_revisions (goal_id, revision)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE context_snapshots
    ADD CONSTRAINT context_snapshots_work_item_ownership_fkey
    FOREIGN KEY (work_item_id, goal_id)
    REFERENCES work_items (id, goal_id)
    ON DELETE RESTRICT
    """)

    create constraint(:context_snapshots, :context_snapshots_schema_version_positive,
             check: "schema_version > 0"
           )

    create constraint(:context_snapshots, :context_snapshots_goal_revision_positive,
             check: "goal_revision > 0"
           )

    create constraint(:context_snapshots, :context_snapshots_content_hash_size,
             check: "octet_length(content_hash) = 32"
           )

    create constraint(:context_snapshots, :context_snapshots_payload_object,
             check: "jsonb_typeof(payload) = 'object'"
           )
  end

  defp add_task_context_constraint do
    execute("""
    ALTER TABLE tasks
    ADD CONSTRAINT tasks_context_snapshot_ownership_fkey
    FOREIGN KEY (context_snapshot_id, goal_id, goal_revision, work_item_id)
    REFERENCES context_snapshots (id, goal_id, goal_revision, work_item_id)
    ON DELETE RESTRICT
    """)
  end

  defp create_harness_sessions do
    create table(:harness_sessions, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :machine_id, :binary_id, null: false
      add :runtime_id, :binary_id, null: false
      add :harness_kind, :text, null: false
      add :harness_version, :text, null: false
      add :adapter_version, :text, null: false
      add :local_handle_id, :binary_id, null: false
      add :repository_resource_id, :binary_id, null: false
      add :workspace_fingerprint, :text, null: false
      add :state, :text, null: false, default: "available"
      add :active_run_id, :binary_id
      add :lock_version, :bigint, null: false, default: 1
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:harness_sessions, [:machine_id, :local_handle_id],
             name: :harness_sessions_machine_id_local_handle_id_key
           )

    create unique_index(:harness_sessions, [:active_run_id],
             name: :harness_sessions_active_run_id_key
           )

    execute("""
    ALTER TABLE harness_sessions
    ADD CONSTRAINT harness_sessions_machine_id_fkey
    FOREIGN KEY (machine_id)
    REFERENCES machines (id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE harness_sessions
    ADD CONSTRAINT harness_sessions_runtime_id_fkey
    FOREIGN KEY (runtime_id)
    REFERENCES runtimes (id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE harness_sessions
    ADD CONSTRAINT harness_sessions_runtime_machine_ownership_fkey
    FOREIGN KEY (runtime_id, machine_id)
    REFERENCES runtimes (id, machine_id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE harness_sessions
    ADD CONSTRAINT harness_sessions_repository_resource_id_fkey
    FOREIGN KEY (repository_resource_id)
    REFERENCES project_resources (id)
    ON DELETE RESTRICT
    """)

    create constraint(:harness_sessions, :harness_sessions_harness_kind_check,
             check: "harness_kind IN ('codex', 'claude_code', 'pi', 'opencode')"
           )

    create constraint(:harness_sessions, :harness_sessions_state_check,
             check: "state IN ('available', 'busy', 'unavailable', 'closed')"
           )

    create constraint(:harness_sessions, :harness_sessions_busy_active_run_check,
             check: "(state = 'busy') = (active_run_id IS NOT NULL)"
           )

    create constraint(:harness_sessions, :harness_sessions_lock_version_positive,
             check: "lock_version > 0"
           )
  end

  defp add_session_constraints do
    execute("""
    ALTER TABLE runs
    ADD CONSTRAINT runs_harness_session_id_fkey
    FOREIGN KEY (harness_session_id)
    REFERENCES harness_sessions (id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE harness_sessions
    ADD CONSTRAINT harness_sessions_active_run_id_fkey
    FOREIGN KEY (active_run_id)
    REFERENCES runs (id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE tasks
    ADD CONSTRAINT tasks_requested_session_id_fkey
    FOREIGN KEY (requested_session_id)
    REFERENCES harness_sessions (id)
    ON DELETE RESTRICT
    """)
  end

  defp create_work_dependencies do
    create table(:work_dependencies, primary_key: false) do
      add :goal_id, :binary_id, null: false
      add :work_item_id, :binary_id, primary_key: true
      add :depends_on_id, :binary_id, primary_key: true
      add :inserted_at, :utc_datetime_usec, null: false
    end

    create index(:work_dependencies, [:depends_on_id, :work_item_id])

    execute("""
    ALTER TABLE work_dependencies
    ADD CONSTRAINT work_dependencies_goal_id_fkey
    FOREIGN KEY (goal_id)
    REFERENCES goals (id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE work_dependencies
    ADD CONSTRAINT work_dependencies_work_item_ownership_fkey
    FOREIGN KEY (work_item_id, goal_id)
    REFERENCES work_items (id, goal_id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE work_dependencies
    ADD CONSTRAINT work_dependencies_depends_on_ownership_fkey
    FOREIGN KEY (depends_on_id, goal_id)
    REFERENCES work_items (id, goal_id)
    ON DELETE RESTRICT
    """)

    create constraint(:work_dependencies, :work_dependencies_distinct_items,
             check: "work_item_id <> depends_on_id"
           )
  end

  defp create_run_evidence do
    create table(:run_evidence, primary_key: false) do
      add :id, :binary_id, primary_key: true

      add :run_id,
          references(:runs,
            type: :binary_id,
            on_delete: :restrict,
            name: :run_evidence_run_id_fkey
          ),
          null: false

      add :evidence_key, :text, null: false
      add :kind, :text, null: false
      add :subject_hash, :binary, null: false
      add :source_ref, :map, null: false
      add :source_revision, :text, null: false
      add :validator_profile, :text
      add :verdict, :text, null: false
      add :payload, :map, null: false
      add :inserted_at, :utc_datetime_usec, null: false
    end

    create unique_index(:run_evidence, [:run_id, :evidence_key])
    create index(:run_evidence, [:subject_hash])

    create constraint(:run_evidence, :run_evidence_key_present,
             check: "NULLIF(BTRIM(evidence_key), '') IS NOT NULL"
           )

    create constraint(:run_evidence, :run_evidence_kind_check,
             check: "kind IN ('check', 'artifact', 'review', 'observation')"
           )

    create constraint(:run_evidence, :run_evidence_subject_hash_size,
             check: "octet_length(subject_hash) = 32"
           )

    create constraint(:run_evidence, :run_evidence_verdict_check,
             check: "verdict IN ('passed', 'failed', 'unknown', 'not_applicable')"
           )

    create constraint(:run_evidence, :run_evidence_payloads_are_objects,
             check: "jsonb_typeof(source_ref) = 'object' AND jsonb_typeof(payload) = 'object'"
           )
  end

  defp create_goal_decisions do
    create table(:goal_decisions, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :goal_id, :binary_id, null: false
      add :goal_revision, :integer, null: false
      add :work_item_id, :binary_id
      add :kind, :text, null: false
      add :action_hash, :binary, null: false
      add :subject_hash, :binary
      add :state, :text, null: false, default: "open"
      add :question, :text, null: false
      add :options, :map, null: false
      add :resolution, :map
      add :actor_ref, :text
      add :expires_at, :utc_datetime_usec
      add :lock_version, :bigint, null: false, default: 1
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:goal_decisions, [:goal_id, :goal_revision, :action_hash])

    create unique_index(:goal_decisions, [:id, :goal_id, :goal_revision],
             name: :goal_decisions_identity_key
           )

    execute("""
    ALTER TABLE goal_decisions
    ADD CONSTRAINT goal_decisions_goal_revision_fkey
    FOREIGN KEY (goal_id, goal_revision)
    REFERENCES goal_revisions (goal_id, revision)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE goal_decisions
    ADD CONSTRAINT goal_decisions_work_item_ownership_fkey
    FOREIGN KEY (work_item_id, goal_id)
    REFERENCES work_items (id, goal_id)
    ON DELETE RESTRICT
    """)

    create constraint(:goal_decisions, :goal_decisions_kind_check,
             check:
               "kind IN ('plan', 'scope', 'review', 'budget', 'external_action', 'completion')"
           )

    create constraint(:goal_decisions, :goal_decisions_goal_revision_positive,
             check: "goal_revision > 0"
           )

    create constraint(:goal_decisions, :goal_decisions_state_check,
             check: "state IN ('open', 'resolved', 'superseded')"
           )

    create constraint(:goal_decisions, :goal_decisions_action_hash_size,
             check: "octet_length(action_hash) = 32"
           )

    create constraint(:goal_decisions, :goal_decisions_subject_hash_size,
             check: "subject_hash IS NULL OR octet_length(subject_hash) = 32"
           )

    create constraint(:goal_decisions, :goal_decisions_state_resolution_check,
             check:
               "(state = 'open' AND resolution IS NULL) OR (state = 'resolved' AND resolution IS NOT NULL) OR (state = 'superseded' AND resolution IS NULL)"
           )

    create constraint(:goal_decisions, :goal_decisions_question_present,
             check: "NULLIF(BTRIM(question), '') IS NOT NULL"
           )

    create constraint(:goal_decisions, :goal_decisions_options_array,
             check: "goal_0006_jsonb_decision_options(options)"
           )

    create constraint(:goal_decisions, :goal_decisions_lock_version_positive,
             check: "lock_version > 0"
           )
  end

  defp create_work_outcomes do
    create table(:work_outcomes, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :goal_id, :binary_id, null: false
      add :goal_revision, :integer, null: false
      add :work_item_id, :binary_id, null: false
      add :producing_task_id, :binary_id, null: false
      add :producing_run_id, :binary_id, null: false
      add :validation_task_id, :binary_id
      add :candidate_subject, :map, null: false
      add :subject_hash, :binary, null: false
      add :producing_result_id, :binary_id, null: false
      add :evidence_ids, :map, null: false
      add :disposition, :text, null: false
      add :reason, :text, null: false
      add :decision_id, :binary_id
      add :inserted_at, :utc_datetime_usec, null: false
    end

    create unique_index(:work_outcomes, [:work_item_id, :goal_revision],
             where: "disposition = 'accepted'",
             name: :work_outcomes_one_accepted_per_revision
           )

    execute("""
    ALTER TABLE work_outcomes
    ADD CONSTRAINT work_outcomes_goal_revision_fkey
    FOREIGN KEY (goal_id, goal_revision)
    REFERENCES goal_revisions (goal_id, revision)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE work_outcomes
    ADD CONSTRAINT work_outcomes_work_item_ownership_fkey
    FOREIGN KEY (work_item_id, goal_id)
    REFERENCES work_items (id, goal_id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE work_outcomes
    ADD CONSTRAINT work_outcomes_producing_task_identity_fkey
    FOREIGN KEY (producing_task_id, work_item_id, goal_id, goal_revision)
    REFERENCES tasks (id, work_item_id, goal_id, goal_revision)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE work_outcomes
    ADD CONSTRAINT work_outcomes_validation_task_identity_fkey
    FOREIGN KEY (validation_task_id, work_item_id, goal_id, goal_revision)
    REFERENCES tasks (id, work_item_id, goal_id, goal_revision)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE work_outcomes
    ADD CONSTRAINT work_outcomes_producing_run_task_fkey
    FOREIGN KEY (producing_run_id, producing_task_id)
    REFERENCES runs (id, task_id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE work_outcomes
    ADD CONSTRAINT work_outcomes_decision_identity_fkey
    FOREIGN KEY (decision_id, goal_id, goal_revision)
    REFERENCES goal_decisions (id, goal_id, goal_revision)
    ON DELETE RESTRICT
    """)

    create constraint(:work_outcomes, :work_outcomes_subject_hash_size,
             check: "octet_length(subject_hash) = 32"
           )

    create constraint(:work_outcomes, :work_outcomes_candidate_subject_is_object,
             check: "jsonb_typeof(candidate_subject) = 'object'"
           )

    create constraint(:work_outcomes, :work_outcomes_evidence_ids_array,
             check: "goal_0006_jsonb_uuid_array(evidence_ids)"
           )

    create constraint(:work_outcomes, :work_outcomes_disposition_check,
             check: "disposition IN ('accepted', 'rejected')"
           )

    create constraint(:work_outcomes, :work_outcomes_reason_present,
             check: "NULLIF(BTRIM(reason), '') IS NOT NULL"
           )
  end

  defp create_goal_events do
    create table(:goal_events, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :goal_id, :binary_id, null: false
      add :sequence, :bigint, null: false
      add :kind, :text, null: false
      add :actor_ref, :text, null: false
      add :request_hash, :binary, null: false
      add :request_hash_version, :integer, null: false
      add :revision, :integer, null: false
      add :payload, :map, null: false
      add :response, :map, null: false
      add :inserted_at, :utc_datetime_usec, null: false
    end

    create unique_index(:goal_events, [:goal_id, :sequence])

    execute("""
    ALTER TABLE goal_events
    ADD CONSTRAINT goal_events_goal_revision_fkey
    FOREIGN KEY (goal_id, revision)
    REFERENCES goal_revisions (goal_id, revision)
    ON DELETE RESTRICT
    """)

    create constraint(:goal_events, :goal_events_sequence_positive, check: "sequence > 0")

    create constraint(:goal_events, :goal_events_kind_present,
             check: "NULLIF(BTRIM(kind), '') IS NOT NULL"
           )

    create constraint(:goal_events, :goal_events_request_hash_size,
             check: "octet_length(request_hash) = 32"
           )

    create constraint(:goal_events, :goal_events_request_hash_version_positive,
             check: "request_hash_version > 0"
           )

    create constraint(:goal_events, :goal_events_revision_positive, check: "revision > 0")

    create constraint(:goal_events, :goal_events_payloads_are_objects,
             check: "jsonb_typeof(payload) = 'object' AND jsonb_typeof(response) = 'object'"
           )
  end

  defp create_budget_and_usage do
    create table(:goal_budget_reservations, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :goal_id, :binary_id, null: false
      add :goal_revision, :integer, null: false
      add :task_id, :binary_id, null: false
      add :admission_key, :binary_id, null: false
      add :reserved_microusd, :bigint
      add :state, :text, null: false, default: "held"
      add :lock_version, :bigint, null: false, default: 1
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:goal_budget_reservations, [:task_id])
    create unique_index(:goal_budget_reservations, [:goal_id, :admission_key])

    execute("""
    ALTER TABLE goal_budget_reservations
    ADD CONSTRAINT goal_budget_reservations_goal_revision_fkey
    FOREIGN KEY (goal_id, goal_revision)
    REFERENCES goal_revisions (goal_id, revision)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE goal_budget_reservations
    ADD CONSTRAINT goal_budget_reservations_task_identity_fkey
    FOREIGN KEY (task_id, goal_id, goal_revision)
    REFERENCES tasks (id, goal_id, goal_revision)
    ON DELETE RESTRICT
    """)

    create constraint(:goal_budget_reservations, :goal_budget_reservations_amount_nonnegative,
             check: "reserved_microusd IS NULL OR reserved_microusd >= 0"
           )

    create constraint(:goal_budget_reservations, :goal_budget_reservations_goal_revision_positive,
             check: "goal_revision > 0"
           )

    create constraint(:goal_budget_reservations, :goal_budget_reservations_state_check,
             check: "state IN ('held', 'settled', 'released', 'unknown')"
           )

    create constraint(:goal_budget_reservations, :goal_budget_reservations_lock_version_positive,
             check: "lock_version > 0"
           )

    create table(:run_usage, primary_key: false) do
      add :id, :binary_id, primary_key: true

      add :run_id,
          references(:runs, type: :binary_id, on_delete: :restrict, name: :run_usage_run_id_fkey),
          null: false

      add :usage_key, :text, null: false
      add :provider, :text, null: false
      add :model, :text, null: false
      add :input_tokens, :bigint
      add :output_tokens, :bigint
      add :cached_input_tokens, :bigint
      add :cost_microusd, :bigint
      add :cost_basis, :text, null: false
      add :price_version, :text
      add :supersedes_id, :binary_id
      add :inserted_at, :utc_datetime_usec, null: false
    end

    create unique_index(:run_usage, [:run_id, :usage_key])
    create unique_index(:run_usage, [:supersedes_id], name: :run_usage_supersedes_id_key)
    create unique_index(:run_usage, [:id, :run_id], name: :run_usage_id_run_id_key)

    execute("""
    ALTER TABLE run_usage
    ADD CONSTRAINT run_usage_supersedes_id_fkey
    FOREIGN KEY (supersedes_id)
    REFERENCES run_usage (id)
    ON DELETE RESTRICT
    """)

    execute("""
    ALTER TABLE run_usage
    ADD CONSTRAINT run_usage_supersedes_same_run_fkey
    FOREIGN KEY (supersedes_id, run_id)
    REFERENCES run_usage (id, run_id)
    ON DELETE RESTRICT
    """)

    create constraint(:run_usage, :run_usage_key_present,
             check: "NULLIF(BTRIM(usage_key), '') IS NOT NULL"
           )

    create constraint(:run_usage, :run_usage_count_nonnegative,
             check:
               "(input_tokens IS NULL OR input_tokens >= 0) AND (output_tokens IS NULL OR output_tokens >= 0) AND (cached_input_tokens IS NULL OR cached_input_tokens >= 0) AND (cost_microusd IS NULL OR cost_microusd >= 0)"
           )

    create constraint(:run_usage, :run_usage_cost_basis_check,
             check:
               "(cost_basis = 'unknown' AND cost_microusd IS NULL) OR " <>
                 "(cost_basis IN ('reported', 'estimated') AND cost_microusd IS NOT NULL)"
           )

    create constraint(:run_usage, :run_usage_supersedes_not_self,
             check: "supersedes_id IS NULL OR supersedes_id <> id"
           )
  end

  defp create_immutability_guards do
    execute("""
    CREATE FUNCTION goal_0006_reject_immutable_history_mutation()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      RAISE EXCEPTION 'goal_0006_immutable_history: % rows cannot be changed', TG_TABLE_NAME;
    END;
    $$
    """)

    for table <- [
          :goal_revisions,
          :context_snapshots,
          :run_evidence,
          :work_outcomes,
          :goal_events,
          :run_usage
        ] do
      execute("""
      CREATE TRIGGER #{table}_immutable_history
      BEFORE UPDATE OR DELETE ON #{table}
      FOR EACH ROW EXECUTE FUNCTION goal_0006_reject_immutable_history_mutation()
      """)
    end

    execute("""
    CREATE FUNCTION goal_0006_reject_resolved_decision_mutation()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF TG_OP = 'DELETE' THEN
        IF OLD.resolution IS NOT NULL THEN
          RAISE EXCEPTION 'goal_0006_resolved_decision_immutable';
        END IF;

        RETURN OLD;
      END IF;

      IF TG_OP = 'UPDATE' AND OLD.resolution IS NOT NULL AND (
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
        RAISE EXCEPTION 'goal_0006_resolved_decision_immutable';
      END IF;

      RETURN NEW;
    END;
    $$
    """)

    execute("""
    CREATE TRIGGER goal_decisions_resolved_immutable
    BEFORE UPDATE OR DELETE ON goal_decisions
    FOR EACH ROW EXECUTE FUNCTION goal_0006_reject_resolved_decision_mutation()
    """)
  end

  defp drop_immutability_guards do
    for table <- [
          :goal_revisions,
          :context_snapshots,
          :run_evidence,
          :work_outcomes,
          :goal_events,
          :run_usage
        ] do
      execute("DROP TRIGGER #{table}_immutable_history ON #{table}")
    end

    execute("DROP TRIGGER goal_decisions_resolved_immutable ON goal_decisions")
    execute("DROP FUNCTION goal_0006_reject_resolved_decision_mutation()")
    execute("DROP FUNCTION goal_0006_reject_immutable_history_mutation()")
  end

  defp create_json_guards do
    execute("""
    CREATE FUNCTION goal_0006_jsonb_nonblank_string_array(value jsonb)
    RETURNS boolean
    LANGUAGE sql
    IMMUTABLE
    STRICT
    AS $$
      SELECT jsonb_typeof(value) = 'array'
        AND NOT EXISTS (
          SELECT 1
          FROM jsonb_array_elements(value) AS item
          WHERE jsonb_typeof(item) <> 'string'
             OR NULLIF(BTRIM(item #>> '{}'), '') IS NULL
        )
    $$
    """)

    execute("""
    CREATE FUNCTION goal_0006_jsonb_uuid_array(value jsonb)
    RETURNS boolean
    LANGUAGE sql
    IMMUTABLE
    STRICT
    AS $$
      SELECT jsonb_typeof(value) = 'array'
        AND NOT EXISTS (
          SELECT 1
           FROM jsonb_array_elements(value) AS item
           WHERE jsonb_typeof(item) <> 'string'
              OR NOT (item #>> '{}') ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'
         )
    $$
    """)

    execute("""
    CREATE FUNCTION goal_0006_jsonb_decision_options(value jsonb)
    RETURNS boolean
    LANGUAGE sql
    IMMUTABLE
    STRICT
    AS $$
      SELECT jsonb_typeof(value) = 'array'
        AND jsonb_array_length(value) > 0
        AND NOT EXISTS (
          SELECT 1
          FROM jsonb_array_elements(value) AS item
          WHERE jsonb_typeof(item) <> 'object'
             OR NOT (item ?& ARRAY['id', 'label', 'consequence'])
             OR jsonb_typeof(item -> 'id') <> 'string'
             OR jsonb_typeof(item -> 'label') <> 'string'
             OR jsonb_typeof(item -> 'consequence') <> 'string'
             OR NULLIF(BTRIM(item ->> 'id'), '') IS NULL
             OR NULLIF(BTRIM(item ->> 'label'), '') IS NULL
             OR NULLIF(BTRIM(item ->> 'consequence'), '') IS NULL
        )
        AND (
          SELECT COUNT(DISTINCT item ->> 'id')
          FROM jsonb_array_elements(value) AS item
        ) = jsonb_array_length(value)
    $$
    """)
  end

  defp drop_json_guards do
    execute("DROP FUNCTION goal_0006_jsonb_decision_options(jsonb)")
    execute("DROP FUNCTION goal_0006_jsonb_uuid_array(jsonb)")
    execute("DROP FUNCTION goal_0006_jsonb_nonblank_string_array(jsonb)")
  end

  defp drop_task_constraints do
    execute("DROP TRIGGER tasks_validation_task_producer_guard ON tasks")
    execute("DROP FUNCTION goal_0006_reject_invalid_validation_task_producer()")
    drop index(:tasks, [:work_item_id], name: :tasks_one_active_goal_task_per_work_item)
    drop constraint(:tasks, :tasks_validation_task_identity_fkey)
    drop constraint(:tasks, :tasks_goal_revision_fkey)
    drop constraint(:tasks, :tasks_goal_work_item_membership_fkey)
    drop constraint(:tasks, :tasks_work_item_id_fkey)

    drop index(:tasks, [:id, :work_item_id, :goal_id, :goal_revision],
           name: :tasks_id_goal_identity_key
         )

    drop index(:tasks, [:id, :goal_id, :goal_revision], name: :tasks_id_goal_revision_key)
    drop index(:tasks, [:goal_id, :admission_key], name: :tasks_goal_id_admission_key_key)
    drop constraint(:tasks, :tasks_purpose_check)
    drop constraint(:tasks, :tasks_max_run_attempts_positive)
    drop constraint(:tasks, :tasks_validation_not_self)
    drop constraint(:tasks, :tasks_goal_revision_positive)
    drop constraint(:tasks, :tasks_goal_fields_all_or_none)
    drop index(:runs, [:id, :task_id], name: :runs_id_task_id_key)
  end

  defp drop_work_item_constraints do
    drop constraint(:work_items, :work_items_goal_revision_fkey)
    drop constraint(:work_items, :work_items_goal_project_ownership_fkey)
    drop index(:work_items, [:id, :goal_id], name: :work_items_id_goal_id_key)
    drop constraint(:work_items, :work_items_admitted_revision_positive)
    drop constraint(:work_items, :work_items_goal_fields_all_or_none)
  end

  defp refuse_destructive_rollback! do
    predicates =
      Enum.map_join(@history_tables, "\n         OR ", fn table ->
        "EXISTS (SELECT 1 FROM #{table})"
      end)

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
      project_resources,
      runtimes
    IN SHARE MODE
    """)

    # A backfilled legacy membership is safe to drop only while the unique
    # current pointer still reconstructs the same relationship.
    execute("""
    DO $$
    BEGIN
      IF #{predicates}
         OR EXISTS (SELECT 1 FROM work_items WHERE goal_id IS NOT NULL OR admitted_revision IS NOT NULL OR acceptance_contract IS NOT NULL)
         OR EXISTS (
           SELECT 1
           FROM tasks AS task
           WHERE task.goal_id IS NOT NULL
              OR task.goal_revision IS NOT NULL
              OR task.context_snapshot_id IS NOT NULL
              OR task.admission_key IS NOT NULL
              OR task.max_run_attempts IS NOT NULL
              OR task.validation_of_task_id IS NOT NULL
              OR task.requested_session_id IS NOT NULL
              OR (
                task.work_item_id IS NOT NULL
                AND NOT EXISTS (
                  SELECT 1
                  FROM work_items AS current_item
                  WHERE current_item.id = task.work_item_id
                    AND current_item.orchestration_task_id = task.id
                    AND (
                      SELECT COUNT(*)
                      FROM work_items AS pointed_item
                      WHERE pointed_item.orchestration_task_id = task.id
                    ) = 1
                )
              )
         )
         OR EXISTS (SELECT 1 FROM runs WHERE harness_session_id IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot roll back Goal 0006 while Goal history exists';
      END IF;
    END
    $$;
    """)
  end

  defp task_goal_fields_check do
    """
    (
      goal_id IS NULL
      AND goal_revision IS NULL
      AND context_snapshot_id IS NULL
      AND admission_key IS NULL
      AND max_run_attempts IS NULL
      AND validation_of_task_id IS NULL
      AND purpose <> 'validate'
    ) OR (
      goal_id IS NOT NULL
      AND goal_revision IS NOT NULL
      AND work_item_id IS NOT NULL
      AND context_snapshot_id IS NOT NULL
      AND admission_key IS NOT NULL
      AND max_run_attempts IS NOT NULL
      AND ((purpose = 'validate' AND validation_of_task_id IS NOT NULL) OR (purpose <> 'validate' AND validation_of_task_id IS NULL))
    )
    """
  end

  defp execution_policy_check do
    """
    COALESCE(
    execution_policy ?& ARRAY[
      'automatic_execution',
      'max_parallel_tasks',
      'max_task_admissions',
      'max_run_attempts_per_task',
      'budget_limit_microusd',
      'budget_mode',
      'allowed_runtime_ids',
      'allowed_model_profiles',
      'final_acceptance',
      'allowed_actions',
      'allowed_resource_ids'
    ]
    AND jsonb_typeof(execution_policy -> 'automatic_execution') = 'boolean'
    AND jsonb_typeof(execution_policy -> 'max_parallel_tasks') = 'number'
    AND (execution_policy ->> 'max_parallel_tasks')::numeric BETWEEN 1 AND 9007199254740991
    AND (execution_policy ->> 'max_parallel_tasks')::numeric = TRUNC((execution_policy ->> 'max_parallel_tasks')::numeric)
    AND jsonb_typeof(execution_policy -> 'max_task_admissions') = 'number'
    AND (execution_policy ->> 'max_task_admissions')::numeric BETWEEN 1 AND 9007199254740991
    AND (execution_policy ->> 'max_task_admissions')::numeric = TRUNC((execution_policy ->> 'max_task_admissions')::numeric)
    AND jsonb_typeof(execution_policy -> 'max_run_attempts_per_task') = 'number'
    AND (execution_policy ->> 'max_run_attempts_per_task')::numeric BETWEEN 1 AND 9007199254740991
    AND (execution_policy ->> 'max_run_attempts_per_task')::numeric = TRUNC((execution_policy ->> 'max_run_attempts_per_task')::numeric)
    AND (execution_policy -> 'budget_limit_microusd' = 'null'::jsonb OR
         (jsonb_typeof(execution_policy -> 'budget_limit_microusd') = 'number' AND
          (execution_policy ->> 'budget_limit_microusd')::numeric BETWEEN 0 AND 9223372036854775807 AND
          (execution_policy ->> 'budget_limit_microusd')::numeric = TRUNC((execution_policy ->> 'budget_limit_microusd')::numeric)))
    AND execution_policy ->> 'budget_mode' IN ('soft', 'strict')
    AND goal_0006_jsonb_uuid_array(execution_policy -> 'allowed_runtime_ids')
    AND goal_0006_jsonb_nonblank_string_array(execution_policy -> 'allowed_model_profiles')
    AND execution_policy ->> 'final_acceptance' IN ('operator', 'deterministic')
    AND goal_0006_jsonb_nonblank_string_array(execution_policy -> 'allowed_actions')
    AND goal_0006_jsonb_uuid_array(execution_policy -> 'allowed_resource_ids'), FALSE)
    """
  end
end
