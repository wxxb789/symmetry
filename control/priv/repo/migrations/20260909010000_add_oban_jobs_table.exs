defmodule SymmetryControl.Repo.Migrations.AddObanJobsTable do
  use Ecto.Migration

  def up, do: Oban.Migrations.up(unlogged: false)

  def down do
    execute("LOCK TABLE public.oban_jobs IN SHARE MODE")

    execute("""
    DO $$
    BEGIN
      IF EXISTS (
        SELECT 1
        FROM public.oban_jobs
        WHERE worker LIKE 'SymmetryControl.Goals.Workers.%'
          AND state IN ('available', 'scheduled', 'executing', 'retryable', 'suspended')
      ) THEN
        RAISE EXCEPTION 'cannot roll back Oban jobs while queued Goal wakeups exist';
      END IF;
    END
    $$;
    """)

    Oban.Migrations.down(version: 1)
  end
end
