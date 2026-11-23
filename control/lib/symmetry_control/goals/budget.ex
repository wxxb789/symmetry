defmodule SymmetryControl.Goals.Budget do
  @moduledoc false

  import Ecto.Query

  import SymmetryControl.Goals.Internal,
    only: [now: 1, only_known_keys?: 2, policy_integer: 3, rollback: 1, stamp_update: 2, value: 3]

  alias SymmetryControl.Goals.GoalBudgetReservation
  alias SymmetryControl.Goals.RunUsage
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo

  @nonterminal_task_states [
    "queued",
    "assigned",
    "claimed",
    "running",
    "waiting_for_input",
    "paused",
    "cancelling"
  ]
  @terminal_run_states ["completed", "failed", "cancelled"]
  @accounting_terminal_run_states @terminal_run_states ++ ["expired"]
  @default_max_run_attempts 2
  @max_microusd 9_223_372_036_854_775_807

  # Reservation state projects the whole Task accounting history: a terminal retry cannot
  # settle on an earlier Run while another Run is unreported or unknown.
  @doc false
  def reconcile_reservation!(goal, task, opts) do
    reservation =
      Repo.one(
        from(reservation in GoalBudgetReservation,
          where: reservation.goal_id == ^goal.id and reservation.task_id == ^task.id,
          lock: "FOR UPDATE"
        )
      )

    if reservation do
      accounting = task_accounting(task, reservation)

      if reservation.state == accounting.reservation_state do
        false
      else
        reservation
        |> GoalBudgetReservation.settle_changeset(accounting.reservation_state)
        |> stamp_update(now(opts))
        |> Repo.update!()

        true
      end
    else
      false
    end
  end

  defp ensure_budget!(goal, revision, reserved_microusd) do
    case budget_status(goal, revision, reserved_microusd) do
      :ok -> :ok
      {:error, reason} -> rollback(reason)
    end
  end

  @doc false

  def server_reservation_amount(revision) do
    case value(revision.execution_policy || %{}, "budget_mode", "soft") do
      "soft" ->
        {:ok, nil}

      "strict" ->
        ceiling =
          policy_nonnegative_integer(revision.execution_policy, "per_run_cost_limit_microusd")

        if is_nil(ceiling) or
             value(revision.execution_policy, "hard_cost_limit_required", false) != true do
          {:error, :strict_budget_requires_reservation}
        else
          {:ok,
           multiply_microusd!(
             ceiling,
             policy_integer(
               revision.execution_policy,
               "max_run_attempts_per_task",
               @default_max_run_attempts
             )
           )}
        end

      _ ->
        {:error, :invalid_request}
    end
  end

  @doc false

  def budget_status(goal, revision, reserved_microusd) do
    limit = policy_nonnegative_integer(revision.execution_policy, "budget_limit_microusd")
    mode = value(revision.execution_policy || %{}, "budget_mode", "soft")

    reservations =
      Repo.all(
        from(reservation in GoalBudgetReservation,
          where: reservation.goal_id == ^goal.id,
          lock: "FOR UPDATE"
        )
      )

    reservations_by_task = Map.new(reservations, &{&1.task_id, &1})

    accounting =
      from(task in Task, where: task.goal_id == ^goal.id)
      |> Repo.all()
      |> Enum.map(fn task -> task_accounting(task, Map.get(reservations_by_task, task.id)) end)

    unknown? = Enum.any?(accounting, & &1.unknown?)

    cond do
      mode == "strict" and unknown? ->
        {:error, :budget_unknown}

      mode == "strict" and is_nil(reserved_microusd) ->
        {:error, :strict_budget_requires_reservation}

      limit ->
        committed =
          Enum.reduce(accounting, 0, fn task, total ->
            total + task.known_cost_microusd + task.remaining_liability_microusd
          end)

        if committed + (reserved_microusd || 0) > limit,
          do: {:error, :budget_exhausted},
          else: :ok

      true ->
        :ok
    end
  end

  @doc false

  def ensure_resume_budget!(goal, revision) do
    if value(revision.execution_policy || %{}, "budget_mode", "soft") == "strict" do
      ensure_budget!(goal, revision, 0)
    end
  end

  defp task_accounting(task, reservation) do
    runs =
      Repo.all(
        from(run in Run,
          where: run.task_id == ^task.id,
          select: %{id: run.id, state: run.state}
        )
      )

    effective_usage_by_run =
      from(usage in RunUsage,
        join: run in Run,
        on: run.id == usage.run_id,
        where: run.task_id == ^task.id,
        select: {run.id, usage.id, usage.supersedes_id, usage.cost_microusd, usage.cost_basis}
      )
      |> Repo.all()
      |> Enum.group_by(&elem(&1, 0), fn {_run_id, id, supersedes_id, cost, basis} ->
        {id, supersedes_id, cost, basis}
      end)
      |> Map.new(fn {run_id, rows} -> {run_id, latest_usage_rows(rows)} end)

    leaves = effective_usage_by_run |> Map.values() |> List.flatten()

    known_cost_microusd =
      leaves
      |> Enum.map(&elem(&1, 2))
      |> Enum.reject(&is_nil/1)
      |> Enum.sum()

    unknown_leaf? =
      Enum.any?(leaves, fn {_id, _supersedes_id, cost, basis} ->
        is_nil(cost) or basis == "unknown"
      end)

    estimated_leaf? =
      Enum.any?(leaves, fn {_id, _supersedes_id, _cost, basis} -> basis == "estimated" end)

    missing_terminal_usage? =
      Enum.any?(runs, fn run ->
        run.state in @accounting_terminal_run_states and
          Map.get(effective_usage_by_run, run.id, []) == []
      end)

    missing_task_usage? =
      runs != [] and Enum.any?(runs, &(Map.get(effective_usage_by_run, &1.id, []) == []))

    terminal? = task.state in @terminal_run_states

    reservation_state =
      cond do
        task.state in @nonterminal_task_states ->
          "held"

        terminal? and runs == [] ->
          "released"

        terminal? and (unknown_leaf? or estimated_leaf? or missing_task_usage?) ->
          "unknown"

        terminal? ->
          "settled"

        true ->
          "unknown"
      end

    unknown? =
      unknown_leaf? or
        estimated_leaf? or
        missing_terminal_usage? or
        (terminal? and missing_task_usage?) or
        (not terminal? and (is_nil(reservation) or is_nil(reservation.reserved_microusd)))

    remaining_liability_microusd =
      if reservation && reservation_state in ["held", "unknown"] do
        max((reservation.reserved_microusd || 0) - known_cost_microusd, 0)
      else
        0
      end

    %{
      known_cost_microusd: known_cost_microusd,
      remaining_liability_microusd: remaining_liability_microusd,
      reservation_state: reservation_state,
      unknown?: unknown?
    }
  end

  defp latest_usage_rows(rows) do
    usage_ids = MapSet.new(rows, &elem(&1, 0))

    superseded_ids =
      rows
      |> Enum.flat_map(fn {_id, supersedes_id, _cost, _basis} ->
        if is_nil(supersedes_id) or not MapSet.member?(usage_ids, supersedes_id),
          do: [],
          else: [supersedes_id]
      end)
      |> MapSet.new()

    Enum.reject(rows, &MapSet.member?(superseded_ids, elem(&1, 0)))
  end

  @doc false

  def admission_limits!(payload, opts) do
    supplied = value(payload, :limits, %{})

    if !(is_map(supplied) and only_known_keys?(supplied, ["max_turns", "deadline_at"])),
      do: rollback(:invalid_request)

    max_turns = value(supplied, :max_turns, 1)
    deadline_at = value(supplied, :deadline_at, DateTime.shift(now(opts), minute: 15))

    if !(is_integer(max_turns) and max_turns > 0 and max_turns <= 9_007_199_254_740_991),
      do: rollback(:invalid_request)

    deadline_at =
      case deadline_at do
        %DateTime{} = datetime ->
          datetime

        value when is_binary(value) ->
          case DateTime.from_iso8601(value) do
            {:ok, datetime, _offset} -> datetime
            _ -> rollback(:invalid_request)
          end

        _ ->
          rollback(:invalid_request)
      end

    if DateTime.compare(deadline_at, now(opts)) != :gt, do: rollback(:invalid_request)
    %{"max_turns" => max_turns, "deadline_at" => DateTime.to_iso8601(deadline_at)}
  end

  @doc false

  # Cost ceilings and reservations are approved revision policy, never an
  # operator-command parameter. A strict admission reserves every permitted
  # infrastructure attempt before it can be queued.
  def server_admission_budget!(goal, revision, limits) do
    case value(revision.execution_policy || %{}, "budget_mode", "soft") do
      "soft" ->
        ensure_budget!(goal, revision, nil)
        {Map.put(limits, "max_cost_microusd", nil), nil, %{}}

      "strict" ->
        ceiling =
          policy_nonnegative_integer(revision.execution_policy, "per_run_cost_limit_microusd") ||
            rollback(:strict_budget_requires_reservation)

        if value(revision.execution_policy, "hard_cost_limit_required", false) != true do
          rollback(:strict_budget_requires_reservation)
        end

        {:ok, reservation_amount} = server_reservation_amount(revision)

        ensure_budget!(goal, revision, reservation_amount)

        {
          Map.put(limits, "max_cost_microusd", Integer.to_string(ceiling)),
          reservation_amount,
          %{"adapter" => %{"operations" => %{"hard_cost_limit" => true}}}
        }

      _ ->
        rollback(:invalid_request)
    end
  end

  defp multiply_microusd!(left, right) when is_integer(left) and left >= 0 and is_integer(right) and right > 0 do
    if left == 0 or right <= div(@max_microusd, left),
      do: left * right,
      else: rollback(:invalid_request)
  end

  defp multiply_microusd!(_left, _right), do: rollback(:invalid_request)

  defp policy_nonnegative_integer(policy, key, default \\ nil) do
    case value(policy || %{}, key, default) do
      value when is_integer(value) and value >= 0 and value <= @max_microusd -> value
      nil -> nil
      _ -> rollback(:invalid_request)
    end
  end
end
