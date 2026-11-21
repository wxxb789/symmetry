defmodule SymmetryControl.Migrations.AddOmoHarnessKindMigrationTest do
  use SymmetryControl.MigrationCase, async: false

  alias SymmetryControl.Repo
  alias SymmetryControl.Repo.Migrations.AddOmoHarnessKind

  @omp_harness_kind_migration_version 20_261_114_010_000
  @omo_harness_kind_migration_version 20_261_114_020_000

  test "the migrations directory applies end to end without a duplicated version" do
    with_schema("add_omo_harness_kind_migration", fn ->
      migrate_all_up!()

      assert %{
               rows: [
                 [@omp_harness_kind_migration_version],
                 [@omo_harness_kind_migration_version]
               ]
             } =
               Repo.query!(
                 "SELECT version FROM schema_migrations WHERE version IN ($1, $2) ORDER BY version",
                 [@omp_harness_kind_migration_version, @omo_harness_kind_migration_version]
               )
    end)
  end

  test "adds the OmO kinds without regressing the OMP kinds" do
    with_schema("add_omo_harness_kind_migration", fn ->
      migrate_all_up!()

      # The OMP kinds remain admissible after the OmO migration rebuilds both constraints.
      %{runtime_id: runtime_id, session_id: session_id} = insert_omp_harness_rows!()

      assert %{rows: [["omp", "18.20.0", "symmetry-omo-1"]]} =
               Repo.query!(
                 "SELECT harness_kind, harness_version, adapter_version FROM runtimes WHERE id = $1",
                 [runtime_id]
               )

      assert %{rows: [["omp"]]} =
               Repo.query!(
                 "SELECT harness_kind FROM harness_sessions WHERE id = $1",
                 [session_id]
               )

      # The OmO kinds are admissible too, and unknown kinds remain refused.
      for {table, id} <- [{"runtimes", runtime_id}, {"harness_sessions", session_id}] do
        Repo.query!("UPDATE #{table} SET harness_kind = 'omo' WHERE id = $1", [id])

        assert_raise Postgrex.Error, ~r/#{table}_harness_kind_check/, fn ->
          Repo.query!("UPDATE #{table} SET harness_kind = 'unsupported_harness' WHERE id = $1", [
            id
          ])
        end
      end
    end)
  end

  test "rolling the OmO kind back refuses retained OmO rows and preserves the OMP kinds" do
    with_schema("add_omo_harness_kind_migration", fn ->
      migrate_all_up!()

      %{runtime_id: runtime_id, session_id: session_id} = insert_omo_harness_rows!()

      # The down guard must refuse while an OmO row exists.
      assert_raise Postgrex.Error,
                   ~r/cannot remove omo harness support while runtimes or sessions exist/i,
                   fn ->
                     migrate_omo_down!()
                   end

      # The down guard must also refuse while only an OMP row exists.
      for {table, id} <- [{"runtimes", runtime_id}, {"harness_sessions", session_id}] do
        Repo.query!("UPDATE #{table} SET harness_kind = 'omp' WHERE id = $1", [id])
      end

      assert_raise Postgrex.Error,
                   ~r/cannot remove omo harness support while runtimes or sessions exist/i,
                   fn ->
                     migrate_omo_down!()
                   end

      # After a clean down, the post-OMP state remains: OMP rows stay admissible, OmO does not.
      for {table, id} <- [{"runtimes", runtime_id}, {"harness_sessions", session_id}] do
        Repo.query!("UPDATE #{table} SET harness_kind = 'codex' WHERE id = $1", [id])
      end

      migrate_omo_down!()

      for {table, id} <- [{"runtimes", runtime_id}, {"harness_sessions", session_id}] do
        Repo.query!("UPDATE #{table} SET harness_kind = 'omp' WHERE id = $1", [id])

        assert_raise Postgrex.Error, ~r/#{table}_harness_kind_check/, fn ->
          Repo.query!("UPDATE #{table} SET harness_kind = 'omo' WHERE id = $1", [id])
        end
      end
    end)
  end

  defp migrate_all_up! do
    Ecto.Migrator.run(Repo, migrations_path(), :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_omo_down! do
    Ecto.Migrator.run(
      Repo,
      [{@omo_harness_kind_migration_version, AddOmoHarnessKind}],
      :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp insert_omp_harness_rows! do
    insert_harness_rows!("omp")
  end

  defp insert_omo_harness_rows! do
    insert_harness_rows!("omo")
  end

  defp insert_harness_rows!(kind) do
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
        "omo-kind-project-#{System.unique_integer([:positive])}",
        "OK#{System.unique_integer([:positive])}" |> String.slice(0, 8)
      ]
    )

    resource_id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO project_resources (
        id, project_id, kind, name, status, sync_status, metadata, lock_version,
        inserted_at, updated_at
      )
      VALUES ($1, $2, 'repository', $3, 'unknown', 'unknown', '{}'::jsonb, 1, now(), now())
      """,
      [resource_id, project_id, "omo-kind-repository-#{System.unique_integer([:positive])}"]
    )

    machine_id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO machines (id, name, token_digest, inserted_at, updated_at)
      VALUES ($1, $2, $3, now(), now())
      """,
      [machine_id, "omo-kind-machine-#{System.unique_integer([:positive])}", machine_id]
    )

    runtime_id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO runtimes (
        id, machine_id, runtime_key, name, daemon_instance_id, connection_epoch, capacity,
        agent_profile, workspace, status, heartbeat_interval_ms, repository_resource_id,
        harness_kind, harness_version, adapter_version, adapter_protocol_version,
        inserted_at, updated_at
      )
      VALUES ($1, $2, $3, $3, $4, 1, 1, 'default', 'primary', 'online', 5000, $5, $6,
              '18.20.0', 'symmetry-omo-1', 1, now(), now())
      """,
      [
        runtime_id,
        machine_id,
        "omo-kind-runtime-#{System.unique_integer([:positive])}",
        Ecto.UUID.bingenerate(),
        resource_id,
        kind
      ]
    )

    session_id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO harness_sessions (
        id, machine_id, runtime_id, repository_resource_id, harness_kind, harness_version,
        adapter_version, local_handle_id, binding_id, workspace_fingerprint, state,
        inserted_at, updated_at
      )
      VALUES ($1, $2, $3, $4, $5, '18.20.0', 'symmetry-omo-1', $6, $7, 'fingerprint',
              'available', now(), now())
      """,
      [
        session_id,
        machine_id,
        runtime_id,
        resource_id,
        kind,
        Ecto.UUID.bingenerate(),
        Ecto.UUID.bingenerate()
      ]
    )

    %{runtime_id: runtime_id, session_id: session_id}
  end

  defp migrations_path do
    Path.expand("../../../priv/repo/migrations", __DIR__)
  end
end
