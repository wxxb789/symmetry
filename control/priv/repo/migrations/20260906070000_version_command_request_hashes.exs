defmodule SymmetryControl.Repo.Migrations.VersionCommandRequestHashes do
  use Ecto.Migration

  def up do
    alter table(:commands) do
      # Old application instances continue writing the legacy algorithm during rollout.
      add :request_hash_version, :integer, null: false, default: 1
    end

    create constraint(:commands, :commands_request_hash_version_check,
             check: "request_hash_version IN (1, 2)"
           )
  end

  def down do
    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM commands WHERE request_hash_version = 2) THEN
        RAISE EXCEPTION 'cannot remove command hash versions while context-bound history exists';
      END IF;
    END
    $$;
    """)

    drop constraint(:commands, :commands_request_hash_version_check)
    alter table(:commands), do: remove(:request_hash_version)
  end
end
