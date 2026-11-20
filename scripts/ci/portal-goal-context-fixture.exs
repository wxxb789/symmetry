# 0007h goal-context fixture: reports the ContextSnapshot a Goal's admitted
# planning task was given, so a browser case can read the exact served snapshot
# instead of guessing an id.
#
#   --snapshot-id <goal_id>
#     Prints `snapshot_id=<uuid>` for the newest `context_snapshots.id` bound to
#     that Goal, or `snapshot_id=none` when the Goal has no snapshot. The marker
#     keeps the answer distinct from the application's own log lines on the same
#     stdout. Only the `request_plan`/`admit_task` admission path writes
#     snapshots (Goals.admit_plan_task!/7), so the caller must admit a planning
#     task first; this fixture only reads.
#
# The fixture starts the application only to reach the database the running
# control plane serves and writes nothing. Every other read in the browser
# suite stays real HTTP.
defmodule PortalGoalContextFixture do
  @moduledoc false

  alias SymmetryControl.Goals.ContextSnapshot
  alias SymmetryControl.Repo

  import Ecto.Query

  @usage "usage: --snapshot-id <goal_id>"

  def main(["--snapshot-id", goal_id]) do
    ensure_started()

    case Ecto.UUID.cast(goal_id) do
      {:ok, id} ->
        snapshot =
          Repo.one(
            from(snapshot in ContextSnapshot,
              where: snapshot.goal_id == ^id,
              order_by: [desc: snapshot.inserted_at, desc: snapshot.id],
              limit: 1
            ),
            log: false
          )

        IO.puts("snapshot_id=#{if snapshot, do: snapshot.id, else: "none"}")

      :error ->
        raise ArgumentError, "not a UUID: #{goal_id}"
    end
  end

  def main(_argv) do
    IO.puts(:stderr, @usage)
    System.halt(2)
  end

  defp ensure_started do
    # The development logger prints every SQL statement at debug level and its
    # handler writes asynchronously, so a boot line can land on stdout after
    # this script's own answer. Lowering the level before the application
    # starts keeps those lines from being emitted at all, and the answer's own
    # `snapshot_id=` marker keeps the browser-side helper from reading any log
    # line as the answer.
    Logger.configure(level: :warning)

    {:ok, _applications} = Application.ensure_all_started(:symmetry_control)
    :ok
  end
end

PortalGoalContextFixture.main(System.argv())
