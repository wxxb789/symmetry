defmodule SymmetryControl.Repo.Migrations.AddRetryCommands do
  use Ecto.Migration

  def up do
    drop constraint(:commands, :commands_kind_check)

    create constraint(:commands, :commands_kind_check,
             check: "kind IN ('cancel', 'provide_input', 'retry')"
           )
  end

  def down do
    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM commands WHERE kind = 'retry') THEN
        RAISE EXCEPTION
          'cannot roll back retry command support while retry history exists';
      END IF;
    END
    $$;
    """)

    drop constraint(:commands, :commands_kind_check)

    create constraint(:commands, :commands_kind_check,
             check: "kind IN ('cancel', 'provide_input')"
           )
  end
end
