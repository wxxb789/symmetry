defmodule SymmetryControl.Repo.Migrations.CreateProviderActionIntents do
  use Ecto.Migration

  def change do
    create table(:provider_action_intents, primary_key: false) do
      add :id, :binary_id, primary_key: true
      add :run_id, references(:runs, type: :binary_id, on_delete: :delete_all), null: false
      add :task_id, references(:tasks, type: :binary_id, on_delete: :delete_all), null: false
      add :runtime_id, references(:runtimes, type: :binary_id, on_delete: :restrict), null: false
      add :project_id, references(:projects, type: :binary_id, on_delete: :restrict), null: false

      add :work_item_id, references(:work_items, type: :binary_id, on_delete: :restrict),
        null: false

      add :resource_id, references(:project_resources, type: :binary_id, on_delete: :restrict),
        null: false

      add :connection_id,
          references(:external_connections, type: :binary_id, on_delete: :restrict),
          null: false

      add :action_id, :uuid, null: false
      add :runtime_epoch, :bigint, null: false
      add :generation, :bigint, null: false
      add :claim_id, :uuid, null: false
      add :operation, :string, null: false
      add :request_hash, :binary, null: false
      add :input, :map, null: false, default: %{}
      add :state, :string, null: false, default: "accepted"
      add :result, :map
      add :failure, :map
      add :connection_lock_version, :bigint, null: false
      add :work_item_lock_version, :bigint, null: false
      add :completed_at, :utc_datetime_usec
      timestamps(type: :utc_datetime_usec)
    end

    create unique_index(:provider_action_intents, [:run_id, :action_id])

    create unique_index(:provider_action_intents, [:run_id, :resource_id],
             name: :provider_action_intents_active_resource,
             where: "state IN ('accepted', 'unknown')"
           )

    create index(:provider_action_intents, [:connection_id])
    create index(:provider_action_intents, [:resource_id])
    create index(:provider_action_intents, [:work_item_id])

    create constraint(:provider_action_intents, :provider_action_intents_request_hash_size,
             check: "octet_length(request_hash) = 32"
           )

    create constraint(:provider_action_intents, :provider_action_intents_operation_check,
             check: "operation IN ('resource.sync', 'change.upsert', 'change.update')"
           )

    create constraint(:provider_action_intents, :provider_action_intents_state_check,
             check: "state IN ('accepted', 'succeeded', 'failed', 'unknown')"
           )

    create constraint(:provider_action_intents, :provider_action_intents_outcome_check,
             check: """
             (state = 'accepted' AND completed_at IS NULL AND result IS NULL AND failure IS NULL) OR
             (state = 'succeeded' AND completed_at IS NOT NULL AND result IS NOT NULL AND failure IS NULL) OR
             (state = 'failed' AND completed_at IS NOT NULL AND result IS NULL AND failure IS NOT NULL) OR
             (state = 'unknown' AND completed_at IS NOT NULL AND result IS NULL AND failure IS NOT NULL)
             """
           )
  end
end
