defmodule SymmetryControl.Migrations.AddObanJobsTableTest do
  use SymmetryControl.MigrationCase, async: false

  alias SymmetryControl.Repo
  alias SymmetryControl.Repo.Migrations.AddEngineeringConnections
  alias SymmetryControl.Repo.Migrations.AddGoal0006ControlPlane
  alias SymmetryControl.Repo.Migrations.AddHistoryLookupIndexes
  alias SymmetryControl.Repo.Migrations.AddMachineEnrollmentReplay
  alias SymmetryControl.Repo.Migrations.AddObanJobsTable
  alias SymmetryControl.Repo.Migrations.AddProviderActionDispatchOwnership
  alias SymmetryControl.Repo.Migrations.AddRetryCommands
  alias SymmetryControl.Repo.Migrations.AddSupervisoryControls
  alias SymmetryControl.Repo.Migrations.AddTaskAttemptIdentity
  alias SymmetryControl.Repo.Migrations.AddTaskCommandOwnership
  alias SymmetryControl.Repo.Migrations.AddTaskRequiredCapabilities
  alias SymmetryControl.Repo.Migrations.BindWorkItemsToResources
  alias SymmetryControl.Repo.Migrations.CompleteEngineeringWorkspace
  alias SymmetryControl.Repo.Migrations.CreateChat
  alias SymmetryControl.Repo.Migrations.CreateEngineeringWorkspace
  alias SymmetryControl.Repo.Migrations.CreateOrchestrationTables
  alias SymmetryControl.Repo.Migrations.CreateProviderActionIntents
  alias SymmetryControl.Repo.Migrations.EnforceConnectedResourceIdentity
  alias SymmetryControl.Repo.Migrations.EnforceWorkItemCoherence
  alias SymmetryControl.Repo.Migrations.SnapshotProviderActionTargets
  alias SymmetryControl.Repo.Migrations.VersionCommandRequestHashes
  alias SymmetryControl.Repo.Migrations.VersionRequestHashes

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
    Ecto.Migrator.run(Repo, base_migrations(), :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_goal_up! do
    Ecto.Migrator.run(Repo, [{@migration_version, AddGoal0006ControlPlane}], :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_oban_up! do
    Ecto.Migrator.run(Repo, [{@oban_migration_version, AddObanJobsTable}], :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_oban_down! do
    Ecto.Migrator.run(Repo, [{@oban_migration_version, AddObanJobsTable}], :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp base_migrations do
    [
      {20_260_902_000_000, CreateOrchestrationTables},
      {20_260_903_000_000, AddTaskCommandOwnership},
      {20_260_903_010_000, AddMachineEnrollmentReplay},
      {20_260_903_020_000, AddHistoryLookupIndexes},
      {20_260_904_000_000, CreateEngineeringWorkspace},
      {20_260_904_010_000, CompleteEngineeringWorkspace},
      {20_260_904_020_000, BindWorkItemsToResources},
      {20_260_904_030_000, AddRetryCommands},
      {20_260_904_040_000, AddTaskAttemptIdentity},
      {20_260_904_050_000, EnforceWorkItemCoherence},
      {20_260_905_000_000, AddEngineeringConnections},
      {20_260_906_000_000, AddTaskRequiredCapabilities},
      {20_260_906_010_000, CreateProviderActionIntents},
      {20_260_906_020_000, AddProviderActionDispatchOwnership},
      {20_260_906_030_000, EnforceConnectedResourceIdentity},
      {20_260_906_040_000, SnapshotProviderActionTargets},
      {20_260_906_050_000, AddSupervisoryControls},
      {20_260_906_060_000, CreateChat},
      {20_260_906_070_000, VersionCommandRequestHashes},
      {20_260_907_000_000, VersionRequestHashes}
    ]
  end
end
