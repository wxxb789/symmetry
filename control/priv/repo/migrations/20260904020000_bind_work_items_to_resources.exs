defmodule SymmetryControl.Repo.Migrations.BindWorkItemsToResources do
  use Ecto.Migration

  def up do
    alter table(:work_items) do
      add :repository_resource_id,
          references(:project_resources, type: :binary_id, on_delete: :restrict)

      modify :ci_status, :string, null: true, default: nil
      modify :review_status, :string, null: true, default: nil
    end

    create index(:work_items, [:repository_resource_id])

    execute("UPDATE work_items SET ci_status = NULL WHERE ci_status = 'unknown'")
    execute("UPDATE work_items SET review_status = NULL WHERE review_status = 'none'")
  end

  def down do
    execute("UPDATE work_items SET ci_status = 'unknown' WHERE ci_status IS NULL")
    execute("UPDATE work_items SET review_status = 'none' WHERE review_status IS NULL")

    drop index(:work_items, [:repository_resource_id])

    alter table(:work_items) do
      remove :repository_resource_id
      modify :ci_status, :string, null: false, default: "unknown"
      modify :review_status, :string, null: false, default: "none"
    end
  end
end
