defmodule SymmetryControl.Migrations.AddObanJobsTableTest do
  # Deliberately `async: false`: the refusal test takes `LOCK TABLE public.oban_jobs IN SHARE MODE`,
  # which queues behind an async Oban producer (W3-17 plan finding F-8); do not flip with the rest.
  use SymmetryControl.MigrationCase, async: false

  alias SymmetryControl.Repo
  alias SymmetryControl.Repo.Migrations.AddGoal0006ControlPlane
  alias SymmetryControl.Repo.Migrations.AddObanJobsTable

  @migration_version 20_260_909_000_000
  @oban_migration_version 20_260_909_010_000

  test "applies the Goal control plane before its Oban jobs dependency" do
    with_base_schema(fn ->
      migrate_goal_up!()
      migrate_oban_up!()

      assert %{rows: [["public.oban_jobs"]]} =
               Repo.query!("SELECT to_regclass('public.oban_jobs')::text")

      assert %{rows: [[@migration_version], [@oban_migration_version]]} =
               Repo.query!(
                 "SELECT version FROM schema_migrations WHERE version IN ($1, $2) ORDER BY version",
                 [@migration_version, @oban_migration_version]
               )
    end)
  end

  test "refuses Oban rollback while an incomplete Goal worker remains queued" do
    with_base_schema(fn ->
      migrate_goal_up!()

      assert %{rows: [[oban_jobs_before]]} =
               Repo.query!("SELECT to_regclass('public.oban_jobs')::text")

      migrate_oban_up!()

      job_marker = "goal-0006-oban-#{System.unique_integer([:positive])}"

      assert %{rows: [[job_id]]} =
               Repo.query!(
                 """
                 INSERT INTO public.oban_jobs (state, queue, worker, args)
                 VALUES (
                   'available',
                   'goal_wakeup',
                   'SymmetryControl.Goals.Workers.WakeupWorker',
                   $1::text::jsonb
                 )
                 RETURNING id
                 """,
                 [Jason.encode!(%{"goal_0006_test_marker" => job_marker})]
               )

      on_exit(fn ->
        run_private_query!(
          "DELETE FROM public.oban_jobs WHERE id = $1 AND args ->> 'goal_0006_test_marker' = $2",
          [job_id, job_marker]
        )
      end)

      assert_raise Postgrex.Error,
                   ~r/cannot roll back Oban jobs while queued Goal wakeups exist/i,
                   &migrate_oban_down!/0

      assert %{rows: [["public.oban_jobs"]]} =
               Repo.query!("SELECT to_regclass('public.oban_jobs')::text")

      Repo.query!(
        "DELETE FROM public.oban_jobs WHERE id = $1 AND args ->> 'goal_0006_test_marker' = $2",
        [job_id, job_marker]
      )

      if is_nil(oban_jobs_before) do
        migrate_oban_down!()
        assert %{rows: [[nil]]} = Repo.query!("SELECT to_regclass('public.oban_jobs')::text")
      else
        assert %{rows: [["public.oban_jobs"]]} =
                 Repo.query!("SELECT to_regclass('public.oban_jobs')::text")
      end
    end)
  end

  defp with_base_schema(test) do
    with_schema("goal_0006_migration", fn ->
      migrate_base_up!()
      test.()
    end)
  end

  defp migrate_base_up! do
    Ecto.Migrator.run(
      Repo,
      MigrationModules.verified_pairs!(MigrationModules.base_migrations()),
      :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_goal_up! do
    Ecto.Migrator.run(
      Repo,
      MigrationModules.verified_pairs!([{@migration_version, AddGoal0006ControlPlane}]),
      :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_oban_up! do
    Ecto.Migrator.run(
      Repo,
      MigrationModules.verified_pairs!([{@oban_migration_version, AddObanJobsTable}]),
      :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_oban_down! do
    Ecto.Migrator.run(
      Repo,
      MigrationModules.verified_pairs!([{@oban_migration_version, AddObanJobsTable}]),
      :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end
end
