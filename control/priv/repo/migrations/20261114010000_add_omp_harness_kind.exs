defmodule SymmetryControl.Repo.Migrations.AddOmpHarnessKind do
  use Ecto.Migration

  def up do
    drop constraint(:runtimes, :runtimes_harness_kind_check)

    create constraint(:runtimes, :runtimes_harness_kind_check,
             check:
               "harness_kind IS NULL OR harness_kind IN ('codex', 'claude_code', 'pi', 'opencode', 'generic', 'omp')"
           )

    drop constraint(:harness_sessions, :harness_sessions_harness_kind_check)

    create constraint(:harness_sessions, :harness_sessions_harness_kind_check,
             check: "harness_kind IN ('codex', 'claude_code', 'pi', 'opencode', 'omp')"
           )
  end

  def down do
    execute("LOCK TABLE runtimes, harness_sessions IN ACCESS EXCLUSIVE MODE")

    execute("""
    DO $$ BEGIN
      IF EXISTS (SELECT 1 FROM runtimes WHERE harness_kind = 'omp') OR
         EXISTS (SELECT 1 FROM harness_sessions WHERE harness_kind = 'omp') THEN
        RAISE EXCEPTION 'cannot remove OMP harness support while OMP identities exist';
      END IF;
    END $$;
    """)

    drop constraint(:harness_sessions, :harness_sessions_harness_kind_check)

    create constraint(:harness_sessions, :harness_sessions_harness_kind_check,
             check: "harness_kind IN ('codex', 'claude_code', 'pi', 'opencode')"
           )

    drop constraint(:runtimes, :runtimes_harness_kind_check)

    create constraint(:runtimes, :runtimes_harness_kind_check,
             check:
               "harness_kind IS NULL OR harness_kind IN ('codex', 'claude_code', 'pi', 'opencode', 'generic')"
           )
  end
end
