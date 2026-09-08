defmodule SymmetryControl.Repo.Migrations.VersionRequestHashes do
  use Ecto.Migration

  @hash_version_tables [
    :tasks,
    :run_events,
    :run_transitions,
    :chat_actions,
    :provider_action_intents
  ]

  def up do
    Enum.each(@hash_version_tables, fn table ->
      alter table(table) do
        add :request_hash_version, :integer, null: false, default: 1
      end

      create constraint(table, request_hash_version_constraint(table),
               check: "request_hash_version IN (1, 2)"
             )
    end)

    alter table(:machines) do
      add :enrollment_request_hash_version, :integer, null: false, default: 1
    end

    create constraint(:machines, :machines_enrollment_request_hash_version_check,
             check: "enrollment_request_hash_version IN (1, 2)"
           )

    drop constraint(:commands, :commands_request_hash_version_check)

    create constraint(:commands, :commands_request_hash_version_check,
             check: "request_hash_version IN (1, 2, 3)"
           )
  end

  def down do
    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM tasks WHERE request_hash_version = 2)
         OR EXISTS (SELECT 1 FROM run_events WHERE request_hash_version = 2)
         OR EXISTS (SELECT 1 FROM run_transitions WHERE request_hash_version = 2)
         OR EXISTS (SELECT 1 FROM chat_actions WHERE request_hash_version = 2)
         OR EXISTS (SELECT 1 FROM provider_action_intents WHERE request_hash_version = 2)
         OR EXISTS (SELECT 1 FROM machines WHERE enrollment_request_hash_version = 2)
         OR EXISTS (SELECT 1 FROM commands WHERE request_hash_version = 3) THEN
        RAISE EXCEPTION 'cannot remove request hash versions while canonical hash history exists';
      END IF;
    END
    $$;
    """)

    drop constraint(:commands, :commands_request_hash_version_check)

    create constraint(:commands, :commands_request_hash_version_check,
             check: "request_hash_version IN (1, 2)"
           )

    drop constraint(:machines, :machines_enrollment_request_hash_version_check)

    alter table(:machines) do
      remove :enrollment_request_hash_version
    end

    Enum.each(@hash_version_tables, fn table ->
      drop constraint(table, request_hash_version_constraint(table))

      alter table(table) do
        remove :request_hash_version
      end
    end)
  end

  defp request_hash_version_constraint(table),
    do: String.to_atom("#{table}_request_hash_version_check")
end
