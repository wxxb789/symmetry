defmodule SymmetryControl.Migrations.AddWorkItemCreationIdentityMigrationTest do
  use ExUnit.Case, async: false

  alias Ecto.Adapters.SQL.Sandbox
  alias SymmetryControl.Repo
  alias SymmetryControl.Repo.Migrations.AddWorkItemCreationIdentity

  @migration_version 20_261_117_000_000
  @index_name "work_items_project_creation_id_index"

  test "the migrations directory applies end to end and records the creation identity version" do
    with_schema(fn ->
      migrate_all_up!()

      assert %{rows: [[@migration_version]]} =
               Repo.query!(
                 "SELECT version FROM schema_migrations WHERE version = $1",
                 [@migration_version]
               )
    end)
  end

  test "the creation identity index admits one row per project and identity" do
    with_schema(fn ->
      migrate_all_up!()

      project_id = insert_project!()
      other_project_id = insert_project!()

      insert_keyed_work_item!(project_id, "draft-1")

      # PostgreSQL refuses a second row carrying the same identity in the same project.
      assert_raise Postgrex.Error, ~r/#{@index_name}/, fn ->
        insert_keyed_work_item!(project_id, "draft-1")
      end

      # The scope is the owning project, so another project may reuse the identity.
      insert_keyed_work_item!(other_project_id, "draft-1")

      # Rows without an identity stay outside the partial index.
      insert_work_item!(project_id)
      insert_work_item!(project_id)

      assert %{rows: [[4]]} = Repo.query!("SELECT count(*) FROM work_items")
    end)
  end

  test "rolling the creation identity back drops the identity and its guarantee" do
    with_schema(fn ->
      migrate_all_up!()

      project_id = insert_project!()

      insert_keyed_work_item!(project_id, "draft-1")

      assert_raise Postgrex.Error, ~r/#{@index_name}/, fn ->
        insert_keyed_work_item!(project_id, "draft-1")
      end

      migrate_creation_identity_down!()

      # The migration version is gone.
      assert %{rows: []} =
               Repo.query!("SELECT version FROM schema_migrations WHERE version = $1", [
                 @migration_version
               ])

      # The column is gone.
      assert %{rows: []} =
               Repo.query!("""
               SELECT column_name FROM information_schema.columns
               WHERE table_schema = current_schema()
                 AND table_name = 'work_items'
                 AND column_name = 'creation_id'
               """)

      # The index is gone.
      assert %{rows: []} =
               Repo.query!(
                 """
                 SELECT indexname FROM pg_indexes
                 WHERE schemaname = current_schema()
                   AND tablename = 'work_items'
                   AND indexname = $1
                 """,
                 [@index_name]
               )

      # The rows that shared an identity before the rollback insert again, because the
      # identity that made them duplicates no longer exists.
      insert_work_item!(project_id)
      insert_work_item!(project_id)

      assert %{rows: [[3]]} = Repo.query!("SELECT count(*) FROM work_items")
    end)
  end

  defp with_schema(test) do
    schema = "add_work_item_creation_identity_#{System.unique_integer([:positive])}"
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

  defp migrate_creation_identity_down! do
    Ecto.Migrator.run(
      Repo,
      [{@migration_version, AddWorkItemCreationIdentity}],
      :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
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
        "creation-identity-project-#{System.unique_integer([:positive])}",
        "CI#{System.unique_integer([:positive])}" |> String.slice(0, 8)
      ]
    )

    project_id
  end

  defp insert_work_item!(project_id) do
    Repo.query!(
      """
      INSERT INTO work_items (id, project_id, title, inserted_at, updated_at)
      VALUES ($1, $2, 'Unkeyed work item', now(), now())
      """,
      [Ecto.UUID.bingenerate(), project_id]
    )
  end

  defp insert_keyed_work_item!(project_id, creation_id) do
    Repo.query!(
      """
      INSERT INTO work_items (id, project_id, title, creation_id, inserted_at, updated_at)
      VALUES ($1, $2, 'Keyed work item', $3, now(), now())
      """,
      [Ecto.UUID.bingenerate(), project_id, creation_id]
    )
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
