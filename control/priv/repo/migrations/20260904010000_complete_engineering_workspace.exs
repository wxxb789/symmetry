defmodule SymmetryControl.Repo.Migrations.CompleteEngineeringWorkspace do
  use Ecto.Migration

  def change do
    alter table(:projects) do
      add :lock_version, :integer, null: false, default: 1
    end

    alter table(:project_resources) do
      add :sync_status, :string, null: false, default: "unknown"
      add :status_message, :text
      add :last_checked_at, :utc_datetime_usec
      add :lock_version, :integer, null: false, default: 1
    end

    create index(:project_resources, [:project_id, :sync_status])

    create constraint(:project_resources, :project_resources_sync_status_check,
             check: "sync_status IN ('unknown', 'syncing', 'synced', 'stale', 'failed')"
           )

    alter table(:work_items) do
      add :lock_version, :integer, null: false, default: 1
    end
  end
end
