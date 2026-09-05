defmodule SymmetryControl.Repo.Migrations.CreateEngineeringWorkspace do
  use Ecto.Migration

  def change do
    create table(:projects, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :name, :string, null: false
      add :key, :string, null: false
      add :description, :text
      add :status, :string, null: false, default: "active"
      add :default_agent_profile, :string, null: false, default: "default"
      add :default_workspace, :string, null: false, default: "primary"
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:projects, [:key])

    create constraint(:projects, :projects_status_check,
             check: "status IN ('active', 'archived')"
           )

    create table(:project_resources, primary_key: false) do
      add :id, :binary_id, primary_key: true

      add :project_id, references(:projects, type: :binary_id, on_delete: :delete_all),
        null: false

      add :kind, :string, null: false
      add :name, :string, null: false
      add :provider, :string
      add :external_ref, :string
      add :url, :text
      add :status, :string, null: false, default: "unknown"
      add :metadata, :map, null: false, default: %{}
      add :last_synced_at, :utc_datetime_usec
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:project_resources, [:project_id, :kind, :name])
    create index(:project_resources, [:project_id, :status])

    create constraint(:project_resources, :project_resources_kind_check,
             check:
               "kind IN ('repository', 'work_tracking', 'ci', 'agent', 'runtime', 'connection')"
           )

    create constraint(:project_resources, :project_resources_status_check,
             check: "status IN ('healthy', 'degraded', 'offline', 'unknown')"
           )

    create table(:work_items, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :number, :bigserial, null: false

      add :project_id, references(:projects, type: :binary_id, on_delete: :delete_all),
        null: false

      add :orchestration_task_id,
          references(:tasks, type: :binary_id, on_delete: :nilify_all)

      add :title, :string, null: false
      add :description, :text
      add :status, :string, null: false, default: "backlog"
      add :priority, :string, null: false, default: "no_priority"
      add :position, :integer, null: false, default: 0
      add :assignee_type, :string, null: false, default: "unassigned"
      add :assignee_name, :string
      add :agent_profile, :string
      add :workspace, :string
      add :blocked, :boolean, null: false, default: false
      add :blocker, :text
      add :repository, :string
      add :branch, :string
      add :pull_request_url, :text
      add :ci_status, :string, null: false, default: "unknown"
      add :review_status, :string, null: false, default: "none"
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:work_items, [:number])
    create index(:work_items, [:project_id, :status, :priority, :position])
    create index(:work_items, [:orchestration_task_id])

    create constraint(:work_items, :work_items_status_check,
             check: "status IN ('backlog', 'ready', 'in_progress', 'review', 'done')"
           )

    create constraint(:work_items, :work_items_priority_check,
             check: "priority IN ('urgent', 'high', 'medium', 'low', 'no_priority')"
           )

    create constraint(:work_items, :work_items_assignee_type_check,
             check: "assignee_type IN ('unassigned', 'human', 'agent')"
           )

    create constraint(:work_items, :work_items_ci_status_check,
             check: "ci_status IN ('unknown', 'pending', 'passed', 'failed')"
           )

    create constraint(:work_items, :work_items_review_status_check,
             check: "review_status IN ('none', 'required', 'changes_requested', 'approved')"
           )

    create constraint(:work_items, :work_items_blocker_check,
             check: "blocked = FALSE OR NULLIF(BTRIM(blocker), '') IS NOT NULL"
           )
  end
end
