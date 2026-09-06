defmodule SymmetryControl.Repo.Migrations.AddProviderActionDispatchOwnership do
  use Ecto.Migration

  def up do
    alter table(:provider_action_intents) do
      add :dispatch_token, :uuid
    end

    drop_if_exists index(:provider_action_intents, [:run_id, :resource_id],
                     name: :provider_action_intents_active_resource
                   )

    drop constraint(:provider_action_intents, :provider_action_intents_state_check)
    drop constraint(:provider_action_intents, :provider_action_intents_outcome_check)

    create unique_index(:provider_action_intents, [:run_id, :resource_id],
             name: :provider_action_intents_active_resource,
             where: "state IN ('accepted', 'executing', 'unknown')"
           )

    create constraint(:provider_action_intents, :provider_action_intents_state_check,
             check: "state IN ('accepted', 'executing', 'succeeded', 'failed', 'unknown')"
           )

    create constraint(:provider_action_intents, :provider_action_intents_outcome_check,
             check: """
             (state = 'accepted' AND dispatch_token IS NULL AND completed_at IS NULL AND result IS NULL AND failure IS NULL) OR
             (state = 'executing' AND dispatch_token IS NOT NULL AND completed_at IS NULL AND result IS NULL AND failure IS NULL) OR
             (state = 'succeeded' AND dispatch_token IS NULL AND completed_at IS NOT NULL AND result IS NOT NULL AND failure IS NULL) OR
             (state = 'failed' AND dispatch_token IS NULL AND completed_at IS NOT NULL AND result IS NULL AND failure IS NOT NULL) OR
             (state = 'unknown' AND dispatch_token IS NULL AND completed_at IS NOT NULL AND result IS NULL AND failure IS NOT NULL)
             """
           )
  end

  def down do
    execute("""
    UPDATE provider_action_intents
    SET state = 'unknown',
        dispatch_token = NULL,
        result = NULL,
        failure = '{"code":"provider_failure"}'::jsonb,
        completed_at = COALESCE(completed_at, NOW())
    WHERE state = 'executing'
    """)

    drop_if_exists index(:provider_action_intents, [:run_id, :resource_id],
                     name: :provider_action_intents_active_resource
                   )

    drop constraint(:provider_action_intents, :provider_action_intents_state_check)
    drop constraint(:provider_action_intents, :provider_action_intents_outcome_check)

    create unique_index(:provider_action_intents, [:run_id, :resource_id],
             name: :provider_action_intents_active_resource,
             where: "state IN ('accepted', 'unknown')"
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

    alter table(:provider_action_intents) do
      remove :dispatch_token
    end
  end
end
