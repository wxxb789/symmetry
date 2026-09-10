defmodule SymmetryControl.Repo.Migrations.AddGoalTerminalGuardsAndExecutionPolicy do
  use Ecto.Migration

  @execution_policy_keys [
    "automatic_execution",
    "max_parallel_tasks",
    "max_task_admissions",
    "max_run_attempts_per_task",
    "budget_limit_microusd",
    "per_run_cost_limit_microusd",
    "budget_mode",
    "hard_cost_limit_required",
    "allowed_runtime_ids",
    "allowed_model_profiles",
    "final_acceptance",
    "allowed_actions",
    "allowed_resource_ids"
  ]

  def up do
    refuse_incompatible_policy_upgrade!()
    normalize_existing_policies!()

    drop(constraint(:goal_revisions, :goal_revisions_execution_policy_v1))

    create constraint(:goal_revisions, :goal_revisions_execution_policy_v1,
             check: execution_policy_check()
           )

    execute("UPDATE goals SET next_wake_at = NULL WHERE state IN ('achieved', 'cancelled')")

    create constraint(:goals, :goals_terminal_next_wake_at_null,
             check: "state NOT IN ('achieved', 'cancelled') OR next_wake_at IS NULL"
           )

    execute("""
    CREATE FUNCTION goal_0006_guard_terminal_goal()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF OLD.state IN ('achieved', 'cancelled') AND (
           NEW.state IS DISTINCT FROM OLD.state
        OR NEW.current_revision IS DISTINCT FROM OLD.current_revision
        OR NEW.project_id IS DISTINCT FROM OLD.project_id
      ) THEN
        RAISE EXCEPTION 'goal_0006_terminal_goal_immutable';
      END IF;

      RETURN NEW;
    END;
    $$
    """)

    execute("""
    CREATE TRIGGER goals_terminal_immutability_guard
    BEFORE UPDATE OF state, current_revision, project_id ON goals
    FOR EACH ROW EXECUTE FUNCTION goal_0006_guard_terminal_goal()
    """)

    execute("""
    CREATE FUNCTION goal_0006_guard_terminal_decision()
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
    $$
    """)

    execute("""
    CREATE TRIGGER goal_decisions_terminal_authority_guard
    BEFORE INSERT OR UPDATE OR DELETE ON goal_decisions
    FOR EACH ROW EXECUTE FUNCTION goal_0006_guard_terminal_decision()
    """)
  end

  def down do
    refuse_rollback!()

    execute("DROP TRIGGER goal_decisions_terminal_authority_guard ON goal_decisions")
    execute("DROP FUNCTION goal_0006_guard_terminal_decision()")
    execute("DROP TRIGGER goals_terminal_immutability_guard ON goals")
    execute("DROP FUNCTION goal_0006_guard_terminal_goal()")
    drop(constraint(:goals, :goals_terminal_next_wake_at_null))

    drop(constraint(:goal_revisions, :goal_revisions_execution_policy_v1))

    create constraint(:goal_revisions, :goal_revisions_execution_policy_v1,
             check: legacy_execution_policy_check()
           )
  end

  defp refuse_incompatible_policy_upgrade! do
    expected_keys = sql_text_array(@execution_policy_keys)

    execute("LOCK TABLE goals, goal_revisions, goal_decisions IN SHARE MODE")

    execute("""
    DO $$
    BEGIN
      IF EXISTS (
        SELECT 1
        FROM goal_revisions
        WHERE execution_policy - #{expected_keys} <> '{}'::jsonb
      ) THEN
        RAISE EXCEPTION 'cannot add exact execution policy guard while revisions contain unknown keys';
      END IF;

      IF EXISTS (
        SELECT 1
        FROM goal_revisions
        WHERE execution_policy -> 'automatic_execution' = 'true'::jsonb
          AND execution_policy -> 'budget_limit_microusd' = 'null'::jsonb
      ) THEN
        RAISE EXCEPTION 'cannot require automatic Goal budget while existing automatic revisions have no total budget';
      END IF;

      IF EXISTS (
        SELECT 1
        FROM goal_revisions
        WHERE execution_policy ->> 'budget_mode' = 'strict'
          AND (
            execution_policy -> 'per_run_cost_limit_microusd' IS NULL
            OR execution_policy -> 'per_run_cost_limit_microusd' = 'null'::jsonb
            OR execution_policy -> 'hard_cost_limit_required' IS DISTINCT FROM 'true'::jsonb
          )
      ) THEN
        RAISE EXCEPTION 'cannot require strict Goal cost ceiling while existing strict revisions are not enforceable';
      END IF;
    END
    $$;
    """)
  end

  defp normalize_existing_policies! do
    # The base Goal migration freezes revision history. This one-time, additive
    # normalization must run before the stronger exact-key constraint is added.
    execute("ALTER TABLE goal_revisions DISABLE TRIGGER goal_revisions_immutable_history")

    try do
      execute("""
      UPDATE goal_revisions
      SET execution_policy = execution_policy || jsonb_build_object(
        'per_run_cost_limit_microusd',
        COALESCE(execution_policy -> 'per_run_cost_limit_microusd', 'null'::jsonb),
        'hard_cost_limit_required',
        COALESCE(execution_policy -> 'hard_cost_limit_required', 'false'::jsonb)
      )
      WHERE NOT execution_policy ?& ARRAY['per_run_cost_limit_microusd', 'hard_cost_limit_required']
      """)
    after
      execute("ALTER TABLE goal_revisions ENABLE TRIGGER goal_revisions_immutable_history")
    end
  end

  defp refuse_rollback! do
    execute("LOCK TABLE goals, goal_revisions, goal_decisions IN SHARE MODE")

    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM goals WHERE state IN ('achieved', 'cancelled'))
         OR EXISTS (SELECT 1 FROM goal_decisions WHERE state IN ('resolved', 'superseded'))
         OR EXISTS (
           SELECT 1
           FROM goal_revisions
           WHERE execution_policy -> 'per_run_cost_limit_microusd' IS DISTINCT FROM 'null'::jsonb
              OR execution_policy -> 'hard_cost_limit_required' IS DISTINCT FROM 'false'::jsonb
         ) THEN
        RAISE EXCEPTION 'cannot roll back terminal Goal guards and execution policy while protected history exists';
      END IF;
    END
    $$;
    """)
  end

  defp execution_policy_check do
    """
    COALESCE(
      execution_policy ?& #{sql_text_array(@execution_policy_keys)}
      AND execution_policy - #{sql_text_array(@execution_policy_keys)} = '{}'::jsonb
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
      AND #{nullable_microusd_check("budget_limit_microusd")}
      AND #{nullable_microusd_check("per_run_cost_limit_microusd")}
      AND jsonb_typeof(execution_policy -> 'budget_mode') = 'string'
      AND execution_policy ->> 'budget_mode' IN ('soft', 'strict')
      AND jsonb_typeof(execution_policy -> 'hard_cost_limit_required') = 'boolean'
      AND goal_0006_jsonb_uuid_array(execution_policy -> 'allowed_runtime_ids')
      AND goal_0006_jsonb_nonblank_string_array(execution_policy -> 'allowed_model_profiles')
      AND jsonb_typeof(execution_policy -> 'final_acceptance') = 'string'
      AND execution_policy ->> 'final_acceptance' IN ('operator', 'deterministic')
      AND goal_0006_jsonb_nonblank_string_array(execution_policy -> 'allowed_actions')
      AND goal_0006_jsonb_uuid_array(execution_policy -> 'allowed_resource_ids')
      AND (
        execution_policy -> 'automatic_execution' = 'false'::jsonb
        OR execution_policy -> 'budget_limit_microusd' <> 'null'::jsonb
      )
      AND (
        execution_policy ->> 'budget_mode' <> 'strict'
        OR (
          execution_policy -> 'per_run_cost_limit_microusd' <> 'null'::jsonb
          AND execution_policy -> 'hard_cost_limit_required' = 'true'::jsonb
        )
      ),
      FALSE
    )
    """
  end

  defp legacy_execution_policy_check do
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
    AND #{nullable_microusd_check("budget_limit_microusd")}
    AND execution_policy ->> 'budget_mode' IN ('soft', 'strict')
    AND goal_0006_jsonb_uuid_array(execution_policy -> 'allowed_runtime_ids')
    AND goal_0006_jsonb_nonblank_string_array(execution_policy -> 'allowed_model_profiles')
    AND execution_policy ->> 'final_acceptance' IN ('operator', 'deterministic')
    AND goal_0006_jsonb_nonblank_string_array(execution_policy -> 'allowed_actions')
    AND goal_0006_jsonb_uuid_array(execution_policy -> 'allowed_resource_ids'), FALSE)
    """
  end

  defp nullable_microusd_check(key) do
    """
    (execution_policy -> '#{key}' = 'null'::jsonb OR
     (jsonb_typeof(execution_policy -> '#{key}') = 'number' AND
      (execution_policy ->> '#{key}')::numeric BETWEEN 0 AND 9223372036854775807 AND
      (execution_policy ->> '#{key}')::numeric = TRUNC((execution_policy ->> '#{key}')::numeric)))
    """
  end

  defp sql_text_array(values) do
    "ARRAY[" <> Enum.map_join(values, ", ", &"'#{&1}'") <> "]"
  end
end
