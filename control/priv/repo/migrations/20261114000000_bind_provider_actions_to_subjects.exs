defmodule SymmetryControl.Repo.Migrations.BindProviderActionsToSubjects do
  use Ecto.Migration

  def up do
    alter table(:provider_action_intents) do
      add :action_binding, :map
    end

    execute("""
    CREATE FUNCTION freeze_provider_action_binding() RETURNS trigger
    LANGUAGE plpgsql AS $$
    BEGIN
      IF NEW.action_binding IS DISTINCT FROM OLD.action_binding THEN
        RAISE EXCEPTION 'provider_action_binding_immutable';
      END IF;
      IF OLD.action_binding IS NOT NULL AND
         (to_jsonb(NEW) - ARRAY['state', 'dispatch_token', 'result', 'failure', 'completed_at', 'updated_at'])
         IS DISTINCT FROM
         (to_jsonb(OLD) - ARRAY['state', 'dispatch_token', 'result', 'failure', 'completed_at', 'updated_at']) THEN
        RAISE EXCEPTION 'provider_action_identity_immutable';
      END IF;
      IF OLD.action_binding IS NOT NULL AND OLD.state IN ('succeeded', 'failed') AND
         (NEW.state, NEW.result, NEW.failure, NEW.completed_at)
         IS DISTINCT FROM (OLD.state, OLD.result, OLD.failure, OLD.completed_at) THEN
        RAISE EXCEPTION 'provider_action_outcome_immutable';
      END IF;
      RETURN NEW;
    END;
    $$
    """)

    execute("""
    CREATE TRIGGER provider_action_binding_immutable
    BEFORE UPDATE ON provider_action_intents
    FOR EACH ROW EXECUTE FUNCTION freeze_provider_action_binding()
    """)
  end

  def down do
    execute("LOCK TABLE provider_action_intents IN ACCESS EXCLUSIVE MODE")

    execute("""
    DO $$ BEGIN
      IF EXISTS (SELECT 1 FROM provider_action_intents WHERE action_binding IS NOT NULL) THEN
        RAISE EXCEPTION 'cannot discard provider action Subject bindings';
      END IF;
    END $$;
    """)

    execute("DROP TRIGGER provider_action_binding_immutable ON provider_action_intents")
    execute("DROP FUNCTION freeze_provider_action_binding()")

    alter table(:provider_action_intents) do
      remove :action_binding
    end
  end
end
