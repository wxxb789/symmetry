defmodule SymmetryControl.Migrations.AddGoalOperatorVersionMigrationTest do
  use SymmetryControl.MigrationCase, async: true

  alias SymmetryControl.Repo
  alias SymmetryControl.Repo.Migrations.AddGoalOperatorVersion

  @migration_version 20_261_121_000_000

  test "backfills existing Goals from lock_version and fences the operator version positive" do
    with_schema("add_goal_operator_version", fn ->
      migrate_all_up!()
      migrate_operator_version_down!()

      # True pre-migration state: the column the migration adds is not there, so
      # every fixture row below is a Goal row this migration has to backfill.
      assert %{rows: []} = operator_version_column()

      project_id = insert_project!()
      advanced_goal_id = insert_backfillable_goal!(project_id, 7)
      fresh_goal_id = insert_backfillable_goal!(project_id, 1)

      migrate_operator_version_up!()

      assert %{rows: [[@migration_version]]} =
               Repo.query!("SELECT version FROM schema_migrations WHERE version = $1", [
                 @migration_version
               ])

      assert %{rows: [["operator_version", "NO"]]} = operator_version_column()

      # The backfill takes the last version a client could have read, so no served
      # value moves backwards and lock_version itself is left where it was.
      assert %{rows: [[operator_version, lock_version]]} =
               Repo.query!("SELECT operator_version, lock_version FROM goals WHERE id = $1", [
                 advanced_goal_id
               ])

      assert operator_version == 7
      assert lock_version == 7

      assert %{rows: [[1, 1]]} =
               Repo.query!("SELECT operator_version, lock_version FROM goals WHERE id = $1", [
                 fresh_goal_id
               ])

      # A Goal created after the migration starts at 1.
      created_goal_id = insert_backfillable_goal!(project_id, 1)

      assert %{rows: [[1]]} =
               Repo.query!("SELECT operator_version FROM goals WHERE id = $1", [created_goal_id])

      # PostgreSQL refuses a non-positive operator version on both write paths.
      assert_raise Postgrex.Error, ~r/goals_operator_version_positive/, fn ->
        Repo.query!("UPDATE goals SET operator_version = 0 WHERE id = $1", [advanced_goal_id])
      end

      assert_raise Postgrex.Error, ~r/goals_operator_version_positive/, fn ->
        insert_goal_with_operator_version!(project_id, -1)
      end
    end)
  end

  test "rolling the operator version back drops the column and its guarantee" do
    with_schema("add_goal_operator_version", fn ->
      migrate_all_up!()
      migrate_operator_version_down!()

      project_id = insert_project!()
      goal_id = insert_backfillable_goal!(project_id, 4)

      migrate_operator_version_up!()

      assert %{rows: [[4]]} =
               Repo.query!("SELECT operator_version FROM goals WHERE id = $1", [goal_id])

      migrate_operator_version_down!()

      # The migration version is gone.
      assert %{rows: []} =
               Repo.query!("SELECT version FROM schema_migrations WHERE version = $1", [
                 @migration_version
               ])

      # The column is gone.
      assert %{rows: []} = operator_version_column()

      # The constraint is gone.
      assert %{rows: []} = operator_version_constraint()

      # The Goal row survives the rollback with its row-level lock untouched.
      assert %{rows: [[4]]} =
               Repo.query!("SELECT lock_version FROM goals WHERE id = $1", [goal_id])
    end)
  end

  defp migrate_operator_version_up! do
    Ecto.Migrator.run(Repo, [{@migration_version, AddGoalOperatorVersion}], :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_operator_version_down! do
    Ecto.Migrator.run(Repo, [{@migration_version, AddGoalOperatorVersion}], :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp operator_version_column do
    Repo.query!("""
    SELECT column_name, is_nullable FROM information_schema.columns
    WHERE table_schema = current_schema()
      AND table_name = 'goals'
      AND column_name = 'operator_version'
    """)
  end

  defp operator_version_constraint do
    Repo.query!("""
    SELECT conname FROM pg_constraint
    WHERE conrelid = 'goals'::regclass
      AND conname = 'goals_operator_version_positive'
    """)
  end

  defp insert_project! do
    project_id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO projects (
        id, name, key, status, default_agent_profile, default_workspace, inserted_at, updated_at
      )
      VALUES ($1, $2, $3, 'active', 'default', 'primary', now(), now())
      """,
      [
        project_id,
        "operator-version-project-#{System.unique_integer([:positive])}",
        "OV#{System.unique_integer([:positive])}" |> String.slice(0, 8)
      ]
    )

    project_id
  end

  defp insert_backfillable_goal!(project_id, lock_version) do
    goal_id = Ecto.UUID.bingenerate()

    Repo.transaction(fn ->
      Repo.query!(
        """
        INSERT INTO goals (
          id, project_id, title, state, current_revision, lock_version, inserted_at, updated_at
        )
        VALUES ($1, $2, 'Operator version migration Goal', 'draft', 1, $3, now(), now())
        """,
        [goal_id, project_id, lock_version]
      )

      insert_goal_revision!(goal_id)
    end)

    goal_id
  end

  defp insert_goal_with_operator_version!(project_id, operator_version) do
    goal_id = Ecto.UUID.bingenerate()

    Repo.transaction(fn ->
      Repo.query!(
        """
        INSERT INTO goals (
          id, project_id, title, state, current_revision, lock_version, operator_version,
          inserted_at, updated_at
        )
        VALUES ($1, $2, 'Operator version migration Goal', 'draft', 1, 1, $3, now(), now())
        """,
        [goal_id, project_id, operator_version]
      )

      insert_goal_revision!(goal_id)
    end)
  end

  defp insert_goal_revision!(goal_id) do
    Repo.query!(
      """
      INSERT INTO goal_revisions (
        goal_id, revision, objective, non_goals, acceptance_contract, authority_policy,
        execution_policy, context_manifest, reason, actor_ref, inserted_at
      )
      VALUES ($1, 1, 'Verify the operator version migration', '[]'::jsonb, '{}'::jsonb, '{}'::jsonb,
              $2::text::jsonb, '{}'::jsonb, 'initial', 'operator:test', now())
      """,
      [goal_id, execution_policy_json()]
    )
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
end
