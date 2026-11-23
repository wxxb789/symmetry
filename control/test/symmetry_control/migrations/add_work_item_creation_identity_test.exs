defmodule SymmetryControl.Migrations.AddWorkItemCreationIdentityMigrationTest do
  use SymmetryControl.MigrationCase, async: true

  alias SymmetryControl.Repo
  alias SymmetryControl.Repo.Migrations.AddWorkItemCreationIdentity

  @migration_version 20_261_117_000_000
  @index_name "work_items_project_creation_id_index"

  test "the migrations directory applies end to end and records the creation identity version" do
    with_schema("add_work_item_creation_identity", fn ->
      migrate_all_up!()

      assert %{rows: [[@migration_version]]} =
               Repo.query!(
                 "SELECT version FROM schema_migrations WHERE version = $1",
                 [@migration_version]
               )
    end)
  end

  test "the creation identity index admits one row per project and identity" do
    with_schema("add_work_item_creation_identity", fn ->
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
    with_schema("add_work_item_creation_identity", fn ->
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

  defp migrate_creation_identity_down! do
    Ecto.Migrator.run(
      Repo,
      MigrationModules.verified_pairs!([{@migration_version, AddWorkItemCreationIdentity}]),
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
        String.slice("CI#{System.unique_integer([:positive])}", 0, 8)
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
end
