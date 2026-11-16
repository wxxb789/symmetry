defmodule SymmetryControl.Repo.Migrations.AddWorkItemCreationIdentity do
  use Ecto.Migration

  # The client mints the creation identity and replays it verbatim on retry; the partial index
  # is the database-level guarantee that one identity cannot create two rows. The scope is the
  # owning project because the portal's actor is the deployment's single operator.
  def up do
    alter table(:work_items) do
      add :creation_id, :string
    end

    create unique_index(:work_items, [:project_id, :creation_id],
             name: :work_items_project_creation_id_index,
             where: "creation_id IS NOT NULL"
           )
  end

  def down do
    drop index(:work_items, [:project_id, :creation_id],
           name: :work_items_project_creation_id_index
         )

    alter table(:work_items) do
      remove :creation_id
    end
  end
end
