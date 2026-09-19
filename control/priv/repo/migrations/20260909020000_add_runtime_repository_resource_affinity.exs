defmodule SymmetryControl.Repo.Migrations.AddRuntimeRepositoryResourceAffinity do
  use Ecto.Migration

  # This stays nullable so existing legacy and pre-affinity native rows remain
  # valid. Goal scheduling treats a missing binding as ineligible.
  def up do
    alter table(:runtimes) do
      add :repository_resource_id,
          references(:project_resources, type: :binary_id, on_delete: :restrict)
    end

    create index(:runtimes, [:repository_resource_id])
  end

  def down do
    execute("LOCK TABLE runtimes IN SHARE MODE")

    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM runtimes WHERE repository_resource_id IS NOT NULL) THEN
        RAISE EXCEPTION
          'cannot roll back runtime repository affinity while active runtime repository bindings exist';
      END IF;
    END
    $$;
    """)

    drop index(:runtimes, [:repository_resource_id])

    alter table(:runtimes) do
      remove :repository_resource_id
    end
  end
end
