defmodule SymmetryControl.Repo.Migrations.SnapshotProviderActionTargets do
  use Ecto.Migration

  def up do
    alter table(:provider_action_intents) do
      add :provider, :string
      add :account_ref, :string
      add :resource_kind, :string
      add :resource_external_ref, :string
      add :resource_lock_version, :bigint
    end

    execute("""
    UPDATE provider_action_intents AS intent
    SET provider = connection.provider,
        account_ref = connection.account_ref,
        resource_kind = resource.kind,
        resource_external_ref = resource.external_ref,
        resource_lock_version = resource.lock_version
    FROM external_connections AS connection,
         project_resources AS resource
    WHERE intent.connection_id = connection.id
      AND intent.resource_id = resource.id
    """)

    alter table(:provider_action_intents) do
      modify :provider, :string, null: false
      modify :account_ref, :string, null: false
      modify :resource_kind, :string, null: false
      modify :resource_external_ref, :string, null: false
      modify :resource_lock_version, :bigint, null: false
    end
  end

  def down do
    alter table(:provider_action_intents) do
      remove :resource_lock_version
      remove :resource_external_ref
      remove :resource_kind
      remove :account_ref
      remove :provider
    end
  end
end
