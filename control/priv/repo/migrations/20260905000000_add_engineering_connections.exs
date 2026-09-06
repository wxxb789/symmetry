defmodule SymmetryControl.Repo.Migrations.AddEngineeringConnections do
  use Ecto.Migration

  def change do
    create table(:external_connections, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :provider, :string, null: false
      add :name, :string, null: false
      add :account_ref, :string, null: false
      add :auth_type, :string, null: false
      add :capabilities, {:array, :string}, null: false, default: []
      add :status, :string, null: false, default: "unknown"
      add :status_message, :text
      add :metadata, :map, null: false, default: %{}
      add :last_checked_at, :utc_datetime_usec
      add :lock_version, :integer, null: false, default: 1
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:external_connections, [:provider, :name])
    create index(:external_connections, [:provider, :status])

    create constraint(:external_connections, :external_connections_provider_check,
             check: "provider IN ('github', 'azure_devops')"
           )

    create constraint(:external_connections, :external_connections_auth_type_check,
             check:
               "(provider = 'github' AND auth_type = 'gh_cli') OR " <>
                 "(provider = 'azure_devops' AND auth_type = 'entra_id')"
           )

    create constraint(:external_connections, :external_connections_status_check,
             check: "status IN ('healthy', 'degraded', 'offline', 'unknown')"
           )

    alter table(:project_resources) do
      add :connection_id,
          references(:external_connections, type: :binary_id, on_delete: :restrict)
    end

    create index(:project_resources, [:connection_id])

    alter table(:work_items) do
      add :external_work_item_resource_id,
          references(:project_resources, type: :binary_id, on_delete: :restrict)

      add :ci_resource_id,
          references(:project_resources, type: :binary_id, on_delete: :restrict)

      add :external_provider, :string
      add :external_id, :string
      add :external_url, :text
      add :external_state, :string
      add :external_updated_at, :utc_datetime_usec
      add :external_available, :boolean, null: false, default: true
      add :external_assignee_name, :string
      add :labels, {:array, :string}, null: false, default: []
      add :external_data, :map, null: false, default: %{}
      add :external_pull_request_url, :text
      add :external_pull_request_state, :string
      add :external_ci_status, :string
      add :external_review_status, :string
      add :external_change_updated_at, :utc_datetime_usec
      add :external_ci_updated_at, :utc_datetime_usec
      add :external_change_data, :map, null: false, default: %{}
      add :external_ci_data, :map, null: false, default: %{}
    end

    create index(:work_items, [:external_work_item_resource_id])
    create index(:work_items, [:ci_resource_id])

    create unique_index(:work_items, [:external_work_item_resource_id, :external_id],
             where: "external_work_item_resource_id IS NOT NULL AND external_id IS NOT NULL",
             name: :work_items_external_identity
           )

    create constraint(:work_items, :work_items_external_provider_check,
             check: "external_provider IS NULL OR external_provider IN ('github', 'azure_devops')"
           )

    create constraint(:work_items, :work_items_external_ci_status_check,
             check:
               "external_ci_status IS NULL OR external_ci_status IN ('unknown', 'pending', 'passed', 'failed')"
           )

    create constraint(:work_items, :work_items_external_pull_request_state_check,
             check:
               "external_pull_request_state IS NULL OR external_pull_request_state IN ('unknown', 'open', 'closed', 'merged')"
           )

    create constraint(:work_items, :work_items_external_review_status_check,
             check:
               "external_review_status IS NULL OR external_review_status IN ('none', 'required', 'changes_requested', 'approved')"
           )
  end
end
