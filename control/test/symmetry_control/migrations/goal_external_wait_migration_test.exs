defmodule SymmetryControl.Migrations.GoalExternalWaitMigrationTest do
  use ExUnit.Case, async: false

  alias Ecto.Adapters.SQL.Sandbox
  alias SymmetryControl.Repo
  alias SymmetryControl.Repo.Migrations.AddGoalExternalWaits
  alias SymmetryControl.Repo.Migrations.AddGoalExternalWaitUnsupportedState

  @migration_version 20_260_909_060_000
  @unsupported_migration_version 20_260_909_100_000

  test "creates the external wait table, identity fences and reconciliation indexes" do
    with_schema(fn ->
      migrate_all_up!()

      assert %{rows: [[@migration_version]]} =
               Repo.query!("SELECT version FROM schema_migrations WHERE version = $1", [
                 @migration_version
               ])

      assert %{rows: rows} =
               Repo.query!("""
               SELECT column_name, is_nullable
               FROM information_schema.columns
               WHERE table_schema = current_schema()
                 AND table_name = 'goal_external_waits'
               ORDER BY ordinal_position
               """)

      assert ["goal_id", "NO"] in rows
      assert ["goal_revision", "NO"] in rows
      assert ["work_item_id", "NO"] in rows
      assert ["task_id", "NO"] in rows
      assert ["run_id", "NO"] in rows
      assert ["run_generation", "NO"] in rows
      assert ["source_ref", "NO"] in rows
      assert ["result_id", "NO"] in rows
      assert ["result", "NO"] in rows
      assert ["resource_id", "NO"] in rows
      assert ["external_ref", "NO"] in rows
      assert ["subject", "NO"] in rows
      assert ["subject_hash", "NO"] in rows
      assert ["next_check_at", "YES"] in rows
      assert ["state", "NO"] in rows
      assert ["check_seq", "NO"] in rows
      assert ["receipt_event_id", "YES"] in rows

      assert %{rows: [[1]]} =
               Repo.query!("""
               SELECT COUNT(*)
               FROM pg_indexes
               WHERE schemaname = current_schema()
                 AND tablename = 'goal_external_waits'
                 AND indexname = 'goal_external_waits_current_work_item_key'
               """)

      assert %{rows: [[1]]} =
               Repo.query!("""
               SELECT COUNT(*)
               FROM pg_trigger
               WHERE tgrelid = 'goal_external_waits'::regclass
                 AND tgname = 'goal_external_waits_history_guard'
               """)

      assert %{rows: [[1]]} =
               Repo.query!("""
               SELECT COUNT(*)
               FROM pg_constraint
               WHERE conrelid = 'goal_external_waits'::regclass
                 AND conname = 'goal_external_waits_run_identity_fkey'
               """)
    end)
  end

  test "the migration down removes only the new external-wait structures" do
    with_schema(fn ->
      migrate_all_up!()

      Ecto.Migrator.run(Repo, [{@migration_version, AddGoalExternalWaits}], :down,
        step: 1,
        log: false,
        migration_lock: false,
        dynamic_repo: Repo.get_dynamic_repo()
      )

      assert %{rows: []} =
               Repo.query!("""
               SELECT 1
               FROM information_schema.tables
               WHERE table_schema = current_schema()
                 AND table_name = 'goal_external_waits'
               """)

      assert %{rows: []} =
               Repo.query!("""
               SELECT 1
               FROM pg_indexes
               WHERE schemaname = current_schema()
                 AND indexname IN ('runs_id_task_id_generation_key', 'goal_events_id_goal_id_key')
               """)
    end)
  end

  test "the unsupported-state migration makes unsupported terminal and reversible without history" do
    with_schema(fn ->
      migrate_all_up!()

      assert %{rows: [[definition]]} =
               Repo.query!("""
               SELECT pg_get_constraintdef(oid)
               FROM pg_constraint
               WHERE conrelid = 'goal_external_waits'::regclass
                 AND conname = 'goal_external_waits_state_check'
               """)

      assert definition =~ "unsupported"

      assert %{rows: [[default]]} =
               Repo.query!("""
               SELECT column_default
               FROM information_schema.columns
               WHERE table_schema = current_schema()
                 AND table_name = 'goal_external_waits' AND column_name = 'state'
               """)

      assert default =~ "unsupported"

      Ecto.Migrator.run(
        Repo,
        [{@unsupported_migration_version, AddGoalExternalWaitUnsupportedState}],
        :down,
        step: 1,
        log: false,
        migration_lock: false,
        dynamic_repo: Repo.get_dynamic_repo()
      )

      assert %{rows: [[prior_definition]]} =
               Repo.query!("""
               SELECT pg_get_constraintdef(oid)
               FROM pg_constraint
               WHERE conrelid = 'goal_external_waits'::regclass
                 AND conname = 'goal_external_waits_state_check'
               """)

      refute prior_definition =~ "unsupported"

      assert %{rows: [[prior_default]]} =
               Repo.query!("""
               SELECT column_default
               FROM information_schema.columns
               WHERE table_schema = current_schema()
                 AND table_name = 'goal_external_waits' AND column_name = 'state'
               """)

      assert prior_default =~ "waiting"

      Ecto.Migrator.run(
        Repo,
        [{@unsupported_migration_version, AddGoalExternalWaitUnsupportedState}],
        :up,
        step: 1,
        log: false,
        migration_lock: false,
        dynamic_repo: Repo.get_dynamic_repo()
      )
    end)
  end

  test "preserves an inserted terminal receipt and rejects every later wait mutation" do
    with_schema(fn ->
      migrate_all_up!()

      %{wait_id: wait_id, goal_id: goal_id, receipt_event_id: receipt_event_id} =
        insert_terminal_wait_fixture!()

      replacement_receipt_id = insert_goal_event!(goal_id, 2)

      assert_raise Postgrex.Error, ~r/goal_0006_external_wait_receipt_required/i, fn ->
        Repo.query!(
          """
          INSERT INTO goal_external_waits (
            id, goal_id, goal_revision, work_item_id, task_id, run_id, run_generation, source_ref,
            result_id, result, resource_id, external_ref, subject, subject_hash, next_check_at, state,
            check_seq, receipt_event_id, inserted_at, updated_at
          )
          SELECT $1, goal_id, goal_revision, work_item_id, task_id, run_id, run_generation, source_ref,
                 result_id, result, resource_id, external_ref, subject, subject_hash, next_check_at, state,
                 check_seq, NULL, now(), now()
          FROM goal_external_waits
          WHERE id = $2
          """,
          [Ecto.UUID.bingenerate(), wait_id]
        )
      end

      for statement <- [
            "UPDATE goal_external_waits SET receipt_event_id = $1 WHERE id = $2",
            "UPDATE goal_external_waits SET check_seq = check_seq + 1 WHERE id = $1",
            "UPDATE goal_external_waits SET next_check_at = now() WHERE id = $1"
          ] do
        assert_raise Postgrex.Error, ~r/goal_0006_external_wait_terminal_immutable/i, fn ->
          parameters =
            if String.contains?(statement, "receipt_event_id"),
              do: [replacement_receipt_id, wait_id],
              else: [wait_id]

          Repo.query!(statement, parameters)
        end
      end

      assert_raise Postgrex.Error, ~r/goal_0006_external_wait_history_immutable/i, fn ->
        Repo.query!("DELETE FROM goal_external_waits WHERE id = $1", [wait_id])
      end

      assert %{rows: [[^receipt_event_id, 0, nil]]} =
               Repo.query!(
                 "SELECT receipt_event_id, check_seq, next_check_at FROM goal_external_waits WHERE id = $1",
                 [wait_id]
               )
    end)
  end

  defp with_schema(test) do
    schema = "goal_external_wait_migration_#{System.unique_integer([:positive])}"
    create_schema!(schema)
    repo = start_schema_repo(schema)
    previous_dynamic_repo = Repo.put_dynamic_repo(repo)

    try do
      test.()
    after
      Repo.put_dynamic_repo(previous_dynamic_repo)
      GenServer.stop(repo)
      drop_schema!(schema)
    end
  end

  defp migrate_all_up! do
    Ecto.Migrator.run(Repo, migrations_path(), :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp insert_terminal_wait_fixture! do
    project_id = Ecto.UUID.bingenerate()
    repository_id = Ecto.UUID.bingenerate()
    goal_id = Ecto.UUID.bingenerate()
    work_item_id = Ecto.UUID.bingenerate()
    context_snapshot_id = Ecto.UUID.bingenerate()
    machine_id = Ecto.UUID.bingenerate()
    runtime_id = Ecto.UUID.bingenerate()
    task_id = Ecto.UUID.bingenerate()
    run_id = Ecto.UUID.bingenerate()
    result_id = Ecto.UUID.bingenerate()
    wait_id = Ecto.UUID.bingenerate()
    subject_hash = :crypto.hash(:sha256, "external-wait-migration-subject")

    subject = %{
      "resource_id" => Ecto.UUID.cast!(repository_id),
      "commit" => String.duplicate("a", 40),
      "tree_digest" => "sha256:" <> String.duplicate("b", 64)
    }

    result = %{
      "result_id" => Ecto.UUID.cast!(result_id),
      "subject" => subject,
      "subject_hash" => "sha256:" <> Base.encode16(subject_hash, case: :lower)
    }

    Repo.query!(
      """
      INSERT INTO projects (
        id, name, key, status, default_agent_profile, default_workspace, inserted_at, updated_at
      )
      VALUES ($1, 'external wait migration project', 'EWMT', 'active', 'default', 'primary', now(), now())
      """,
      [project_id]
    )

    Repo.query!(
      """
      INSERT INTO project_resources (
        id, project_id, kind, name, status, sync_status, metadata, lock_version, inserted_at, updated_at
      )
      VALUES ($1, $2, 'repository', 'external wait repository', 'unknown', 'unknown', '{}'::jsonb, 1,
              now(), now())
      """,
      [repository_id, project_id]
    )

    Repo.transaction(fn ->
      Repo.query!(
        """
        INSERT INTO goals (id, project_id, title, state, current_revision, inserted_at, updated_at)
        VALUES ($1, $2, 'External wait migration Goal', 'draft', 1, now(), now())
        """,
        [goal_id, project_id]
      )

      Repo.query!(
        """
        INSERT INTO goal_revisions (
          goal_id, revision, objective, non_goals, acceptance_contract, authority_policy,
          execution_policy, context_manifest, reason, actor_ref, inserted_at
        )
        VALUES ($1, 1, 'Verify external wait history', '[]'::jsonb, '{}'::jsonb, '{}'::jsonb,
                $2::text::jsonb, '{}'::jsonb, 'initial', 'operator:test', now())
        """,
        [goal_id, execution_policy_json()]
      )
    end)

    Repo.query!(
      """
      INSERT INTO work_items (
        id, project_id, repository_resource_id, title, status, priority, position, assignee_type,
        blocked, ci_status, review_status, goal_id, admitted_revision, acceptance_contract,
        baseline_subject, integration, inserted_at, updated_at
      )
      VALUES ($1, $2, $3, 'External wait WorkItem', 'backlog', 'no_priority', 0, 'unassigned',
              FALSE, NULL, NULL, $4, 1, '{}'::jsonb, $5::text::jsonb, TRUE, now(), now())
      """,
      [work_item_id, project_id, repository_id, goal_id, Jason.encode!(subject)]
    )

    Repo.query!(
      """
      INSERT INTO context_snapshots (
        id, goal_id, goal_revision, work_item_id, schema_version, content_hash, payload, inserted_at
      )
      VALUES ($1, $2, 1, $3, 1, $4, '{}'::jsonb, now())
      """,
      [context_snapshot_id, goal_id, work_item_id, :crypto.hash(:sha256, "external-wait-context")]
    )

    Repo.query!(
      """
      INSERT INTO machines (id, name, token_digest, inserted_at, updated_at)
      VALUES ($1, 'external-wait-machine', $2, now(), now())
      """,
      [machine_id, :crypto.hash(:sha256, "external-wait-machine")]
    )

    Repo.query!(
      """
      INSERT INTO runtimes (
        id, machine_id, runtime_key, name, daemon_instance_id, connection_epoch, capacity,
        agent_profile, workspace, capabilities, status, heartbeat_interval_ms, repository_resource_id,
        inserted_at, updated_at
      )
      VALUES ($1, $2, 'external-wait-runtime', 'external-wait-runtime', $3, 1, 1,
              'default', 'primary', '{}'::jsonb, 'online', 5000, $4, now(), now())
      """,
      [runtime_id, machine_id, Ecto.UUID.bingenerate(), repository_id]
    )

    Repo.query!(
      """
      INSERT INTO tasks (
        id, idempotency_key, request_hash, goal, agent_profile, workspace, input,
        required_capabilities, state, current_generation, attempt_generation, work_item_id,
        goal_id, goal_revision, context_snapshot_id, purpose, validation_of_task_id, admission_key,
        max_run_attempts, inserted_at, updated_at
      )
      VALUES ($1, 'external-wait-task', $2, 'External wait task', 'default', 'primary',
              '{}'::jsonb, '{}'::jsonb, 'completed', 1, 1, $3, $4, 1, $5, 'implement', NULL,
              $6, 2, now(), now())
      """,
      [
        task_id,
        :crypto.hash(:sha256, "external-wait-task"),
        work_item_id,
        goal_id,
        context_snapshot_id,
        Ecto.UUID.bingenerate()
      ]
    )

    Repo.query!(
      """
      INSERT INTO runs (
        id, task_id, runtime_id, generation, state, assigned_at, assignment_expires_at,
        result, inserted_at, updated_at
      )
      VALUES ($1, $2, $3, 1, 'completed', now(), now(), $4::text::jsonb, now(), now())
      """,
      [run_id, task_id, runtime_id, Jason.encode!(%{"task_result" => result})]
    )

    receipt_event_id = insert_goal_event!(goal_id, 1)

    Repo.query!(
      """
      INSERT INTO goal_external_waits (
        id, goal_id, goal_revision, work_item_id, task_id, run_id, run_generation, source_ref,
        result_id, result, resource_id, external_ref, subject, subject_hash, next_check_at, state,
        check_seq, receipt_event_id, inserted_at, updated_at
      )
      VALUES ($1, $2, 1, $3, $4, $5, 1, $6::text::jsonb, $7, $8::text::jsonb, $9,
              'external://receipt', $10::text::jsonb, $11, NULL, 'unsupported', 0, $12, now(), now())
      """,
      [
        wait_id,
        goal_id,
        work_item_id,
        task_id,
        run_id,
        Jason.encode!(%{
          "kind" => "task_result",
          "result_id" => Ecto.UUID.cast!(result_id)
        }),
        result_id,
        Jason.encode!(result),
        repository_id,
        Jason.encode!(subject),
        subject_hash,
        receipt_event_id
      ]
    )

    %{goal_id: goal_id, wait_id: wait_id, receipt_event_id: receipt_event_id}
  end

  defp insert_goal_event!(goal_id, sequence) do
    event_id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO goal_events (
        id, goal_id, sequence, kind, actor_ref, request_hash, request_hash_version, revision,
        payload, response, inserted_at
      )
      VALUES ($1, $2, $3, 'external_wait_receipt', 'server:integration', $4, 1, 1,
              '{}'::jsonb, '{}'::jsonb, now())
      """,
      [event_id, goal_id, sequence, :crypto.hash(:sha256, "external-wait-event-#{sequence}")]
    )

    event_id
  end

  defp execution_policy_json do
    Jason.encode!(%{
      "automatic_execution" => false,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "max_run_attempts_per_task" => 2,
      "budget_limit_microusd" => nil,
      "per_run_cost_limit_microusd" => nil,
      "budget_mode" => "soft",
      "hard_cost_limit_required" => false,
      "allowed_runtime_ids" => [],
      "allowed_model_profiles" => [],
      "final_acceptance" => "operator",
      "allowed_actions" => [],
      "allowed_resource_ids" => []
    })
  end

  defp migrations_path do
    Path.expand("../../../priv/repo/migrations", __DIR__)
  end

  defp start_schema_repo(schema) do
    config =
      Application.fetch_env!(:symmetry_control, Repo)
      |> Keyword.merge(name: nil, pool: DBConnection.ConnectionPool, pool_size: 1)
      |> Keyword.put(:parameters, search_path: schema)

    {:ok, repo} = Repo.start_link(config)
    repo
  end

  defp create_schema!(schema) do
    Sandbox.unboxed_run(Repo, fn -> Repo.query!("CREATE SCHEMA #{schema}") end)
  end

  defp drop_schema!(schema) do
    Sandbox.unboxed_run(Repo, fn -> Repo.query!("DROP SCHEMA IF EXISTS #{schema} CASCADE") end)
  end
end
