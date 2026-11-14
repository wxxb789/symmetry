defmodule SymmetryControl.Repo.Migrations.AddOmoHarnessKind do
  use Ecto.Migration

  def up do
    drop(constraint(:runtimes, :runtimes_harness_kind_check))
    drop(constraint(:harness_sessions, :harness_sessions_harness_kind_check))

    create(
      constraint(:runtimes, :runtimes_harness_kind_check,
        check:
          "harness_kind IS NULL OR harness_kind IN ('codex', 'claude_code', 'pi', 'opencode', 'generic', 'omp', 'omo')"
      )
    )

    create(
      constraint(:harness_sessions, :harness_sessions_harness_kind_check,
        check: "harness_kind IN ('codex', 'claude_code', 'pi', 'opencode', 'omp', 'omo')"
      )
    )
  end

  def down do
    execute("""
    DO $$
    BEGIN
      IF EXISTS (SELECT 1 FROM runtimes WHERE harness_kind = 'omo')
         OR EXISTS (SELECT 1 FROM harness_sessions WHERE harness_kind = 'omo')
         OR EXISTS (SELECT 1 FROM runtimes WHERE harness_kind = 'omp')
         OR EXISTS (SELECT 1 FROM harness_sessions WHERE harness_kind = 'omp') THEN
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
          "harness_kind IS NULL OR harness_kind IN ('codex', 'claude_code', 'pi', 'opencode', 'generic', 'omp')"
      )
    )

    create(
      constraint(:harness_sessions, :harness_sessions_harness_kind_check,
        check: "harness_kind IN ('codex', 'claude_code', 'pi', 'opencode', 'omp')"
      )
    )
  end
end
