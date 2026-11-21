defmodule SymmetryControl.Migrations.VersionRequestHashesTest do
  use SymmetryControl.MigrationCase, async: false

  alias SymmetryControl.Repo
  alias SymmetryControl.Repo.Migrations.CreateOrchestrationTables
  alias SymmetryControl.Repo.Migrations.VersionCommandRequestHashes
  alias SymmetryControl.Repo.Migrations.VersionRequestHashes

  @version 20_260_907_000_000

  test "rolls back canonical hash version support when all history is legacy" do
    load_migration_modules!()

    with_schema("version_request_hashes", fn ->
      migrate_up!()

      Repo.query!(
        """
        INSERT INTO machines (id, name, token_digest, inserted_at, updated_at)
        VALUES ($1, 'legacy-history', $2, now(), now())
        """,
        [Ecto.UUID.generate() |> Ecto.UUID.dump!(), <<1>>]
      )

      assert %{rows: [[1]]} =
               Repo.query!(
                 "SELECT enrollment_request_hash_version FROM machines WHERE name = 'legacy-history'"
               )

      migrate_down!()

      assert %{rows: []} =
               Repo.query!("SELECT version FROM schema_migrations WHERE version = $1", [@version])
    end)
  end

  test "refuses rollback when canonical hash history exists" do
    load_migration_modules!()

    with_schema("version_request_hashes", fn ->
      migrate_up!()

      Repo.query!(
        """
        INSERT INTO machines (id, name, token_digest, enrollment_request_hash_version, inserted_at, updated_at)
        VALUES ($1, 'canonical-history', $2, 2, now(), now())
        """,
        [Ecto.UUID.generate() |> Ecto.UUID.dump!(), <<1>>]
      )

      assert_raise Postgrex.Error, ~r/cannot remove request hash versions/i, &migrate_down!/0
    end)
  end

  defp migrate_up! do
    Ecto.Migrator.run(Repo, base_migrations(), :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )

    Repo.query!("CREATE TABLE chat_actions (id uuid PRIMARY KEY, request_hash bytea)")
    Repo.query!("CREATE TABLE provider_action_intents (id uuid PRIMARY KEY, request_hash bytea)")

    Ecto.Migrator.run(Repo, [{@version, VersionRequestHashes}], :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_down! do
    Ecto.Migrator.run(Repo, base_migrations() ++ [{@version, VersionRequestHashes}], :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp base_migrations do
    [
      {20_260_902_000_000, CreateOrchestrationTables},
      {20_260_906_070_000, VersionCommandRequestHashes}
    ]
  end

  defp load_migration_modules! do
    migrations = [
      {CreateOrchestrationTables, "20260902000000_create_orchestration_tables.exs"},
      {VersionCommandRequestHashes, "20260906070000_version_command_request_hashes.exs"},
      {VersionRequestHashes, "20260907000000_version_request_hashes.exs"}
    ]

    Enum.each(migrations, fn {module, filename} ->
      unless function_exported?(module, :__migration__, 0) do
        filename
        |> then(&Path.expand("../../../priv/repo/migrations/#{&1}", __DIR__))
        |> Code.compile_file()
      end
    end)
  end
end
