defmodule SymmetryControl.Repo.Migrations.AddOmoHarnessKind do
  use Ecto.Migration

  def up do
    drop(constraint(:runtimes, :runtimes_harness_kind_check))
    drop(constraint(:harness_sessions, :harness_sessions_harness_kind_check))

    create(
      constraint(:runtimes, :runtimes_harness_kind_check,
        check:
          "harness_kind IS NULL OR harness_kind IN ('codex', 'claude_code', 'pi', 'opencode', 'generic', 'omo')"
      )
    )

    create(
      constraint(:harness_sessions, :harness_sessions_harness_kind_check,
        check: "harness_kind IN ('codex', 'claude_code', 'pi', 'opencode', 'omo')"
      )
    )
  end

  def down do
    execute("LOCK TABLE runtimes, harness_sessions IN ACCESS EXCLUSIVE MODE")

    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM runtimes WHERE harness_kind = 'omo')
         OR EXISTS (SELECT 1 FROM harness_sessions WHERE harness_kind = 'omo') THEN
        RAISE EXCEPTION 'cannot remove omo harness support while runtimes or sessions exist';
      END IF;
    END
    $$;
    """)

    drop(constraint(:runtimes, :runtimes_harness_kind_check))
    drop(constraint(:harness_sessions, :harness_sessions_harness_kind_check))

    create(
      constraint(:runtimes, :runtimes_harness_kind_check,
        check:
          "harness_kind IS NULL OR harness_kind IN ('codex', 'claude_code', 'pi', 'opencode', 'generic')"
      )
    )

    create(
      constraint(:harness_sessions, :harness_sessions_harness_kind_check,
        check: "harness_kind IN ('codex', 'claude_code', 'pi', 'opencode')"
      )
    )
  end
end
