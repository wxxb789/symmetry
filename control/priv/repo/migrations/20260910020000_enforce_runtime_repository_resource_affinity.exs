defmodule SymmetryControl.Repo.Migrations.EnforceRuntimeRepositoryResourceAffinity do
  use Ecto.Migration

  def up do
    refuse_non_repository_runtime_bindings!()

    execute("""
    CREATE FUNCTION validate_runtime_repository_resource()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    DECLARE
      resource_kind text;
    BEGIN
      IF NEW.repository_resource_id IS NULL THEN
        RETURN NEW;
      END IF;

      SELECT kind
      INTO resource_kind
      FROM project_resources
      WHERE id = NEW.repository_resource_id
      FOR SHARE;

      IF NOT FOUND OR resource_kind IS DISTINCT FROM 'repository' THEN
        RAISE EXCEPTION 'runtime_repository_resource_requires_repository';
      END IF;

      RETURN NEW;
    END;
    $$;
    """)

    execute("""
    CREATE TRIGGER runtimes_repository_resource_guard
    BEFORE INSERT OR UPDATE OF repository_resource_id ON runtimes
    FOR EACH ROW EXECUTE FUNCTION validate_runtime_repository_resource()
    """)

    execute("""
    CREATE FUNCTION freeze_runtime_repository_resource_identity()
    RETURNS trigger
    LANGUAGE plpgsql
    AS $$
    BEGIN
      IF EXISTS (
        SELECT 1
        FROM runtimes
        WHERE repository_resource_id = OLD.id
      ) AND (
           NEW.project_id IS DISTINCT FROM OLD.project_id
        OR NEW.kind IS DISTINCT FROM OLD.kind
        OR NEW.provider IS DISTINCT FROM OLD.provider
        OR NEW.external_ref IS DISTINCT FROM OLD.external_ref
        OR NEW.connection_id IS DISTINCT FROM OLD.connection_id
      ) THEN
        RAISE EXCEPTION 'runtime_repository_resource_identity_immutable';
      END IF;

      RETURN NEW;
    END;
    $$;
    """)

    execute("""
    CREATE TRIGGER project_resources_runtime_repository_identity_guard
    BEFORE UPDATE OF project_id, kind, provider, external_ref, connection_id
    ON project_resources
    FOR EACH ROW EXECUTE FUNCTION freeze_runtime_repository_resource_identity()
    """)
  end

  def down do
    execute("LOCK TABLE project_resources, runtimes IN SHARE MODE")

    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM runtimes WHERE repository_resource_id IS NOT NULL) THEN
        RAISE EXCEPTION
          'cannot roll back runtime repository resource affinity guards while runtime bindings exist';
      END IF;
    END
    $$;
    """)

    execute(
      "DROP TRIGGER project_resources_runtime_repository_identity_guard ON project_resources"
    )

    execute("DROP FUNCTION freeze_runtime_repository_resource_identity()")

    execute("DROP TRIGGER runtimes_repository_resource_guard ON runtimes")
    execute("DROP FUNCTION validate_runtime_repository_resource()")
  end

  defp refuse_non_repository_runtime_bindings! do
    execute("LOCK TABLE project_resources, runtimes IN SHARE MODE")

    execute("""
    DO $$
    BEGIN
      IF EXISTS (
        SELECT 1
        FROM runtimes
        JOIN project_resources ON project_resources.id = runtimes.repository_resource_id
        WHERE project_resources.kind IS DISTINCT FROM 'repository'
      ) THEN
        RAISE EXCEPTION
          'cannot add runtime repository resource affinity guards while non-repository runtime bindings exist';
      END IF;
    END
    $$;
    """)
  end
end
