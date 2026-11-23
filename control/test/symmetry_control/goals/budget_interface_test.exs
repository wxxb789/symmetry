Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.BudgetInterfaceTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals.Budget
  alias SymmetryControl.Goals.GoalBudgetReservation
  alias SymmetryControl.Goals.Internal
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo

  @now ~U[2026-09-09 00:00:00.000000Z]

  setup :put_goal_validation_profiles

  defp strict_execution_policy do
    %{
      "budget_mode" => "strict",
      "budget_limit_microusd" => 1_000,
      "per_run_cost_limit_microusd" => 10,
      "hard_cost_limit_required" => true
    }
  end

  test "budget_status/3 counts a held strict reservation against the total limit" do
    {goal, _item, task} =
      admitted_task_fixture("primary", check_contract(), %{
        execution_policy: strict_execution_policy()
      })

    assert Repo.one!(from(reservation in GoalBudgetReservation, where: reservation.task_id == ^task.id)).reserved_microusd ==
             20

    revision = Internal.current_revision!(goal)

    assert Budget.budget_status(goal, revision, 980) == :ok
    assert Budget.budget_status(goal, revision, 981) == {:error, :budget_exhausted}
  end

  test "admission_limits!/2 defaults one turn to the bounded fifteen minute window" do
    assert Budget.admission_limits!(%{}, now: @now) == %{
             "max_turns" => 1,
             "deadline_at" => "2026-09-09T00:15:00.000000Z"
           }

    assert {:error, :invalid_request} =
             Repo.transaction(fn ->
               Budget.admission_limits!(%{limits: %{"max_turns" => 0}}, now: @now)
             end)
  end

  test "server_reservation_amount/1 reserves every strict attempt and leaves soft mode unreserved" do
    {goal, _items} = goal_with_items_fixture(%{})
    revision = Internal.current_revision!(goal)

    assert Budget.server_reservation_amount(revision) == {:ok, nil}

    strict_policy = %{
      "budget_mode" => "strict",
      "per_run_cost_limit_microusd" => 10,
      "hard_cost_limit_required" => true
    }

    strict_revision = %{
      revision
      | execution_policy: Map.merge(revision.execution_policy, strict_policy)
    }

    assert Budget.server_reservation_amount(strict_revision) == {:ok, 20}

    unguarded_revision = %{
      revision
      | execution_policy:
          Map.merge(
            revision.execution_policy,
            Map.delete(strict_policy, "hard_cost_limit_required")
          )
    }

    assert Budget.server_reservation_amount(unguarded_revision) ==
             {:error, :strict_budget_requires_reservation}
  end

  test "server_admission_budget!/3 attaches cost limits only to the strict revision" do
    limits = %{"max_turns" => 1, "deadline_at" => "2026-09-09T00:15:00.000000Z"}

    {soft_goal, _items} = goal_with_items_fixture(%{})
    soft_revision = Internal.current_revision!(soft_goal)

    assert Budget.server_admission_budget!(soft_goal, soft_revision, limits) ==
             {%{
                "max_turns" => 1,
                "deadline_at" => "2026-09-09T00:15:00.000000Z",
                "max_cost_microusd" => nil
              }, nil, %{}}

    {strict_goal, _items} = goal_with_items_fixture(strict_execution_policy())

    strict_revision = Internal.current_revision!(strict_goal)

    assert Budget.server_admission_budget!(strict_goal, strict_revision, limits) ==
             {%{
                "max_turns" => 1,
                "deadline_at" => "2026-09-09T00:15:00.000000Z",
                "max_cost_microusd" => "10"
              }, 20, %{"adapter" => %{"operations" => %{"hard_cost_limit" => true}}}}
  end

  test "ensure_resume_budget!/2 checks only the strict revision and returns :ok" do
    {soft_goal, _items} = goal_with_items_fixture(%{})
    soft_revision = Internal.current_revision!(soft_goal)

    assert Budget.ensure_resume_budget!(soft_goal, soft_revision) == nil

    {strict_goal, _items} = goal_with_items_fixture(strict_execution_policy())

    strict_revision = Internal.current_revision!(strict_goal)

    assert Budget.ensure_resume_budget!(strict_goal, strict_revision) == :ok
  end

  test "reconcile_reservation!/3 releases a terminal task reservation exactly once" do
    {goal, _item, task} = admitted_task_fixture()

    assert Budget.reconcile_reservation!(goal, task, now: @now) == false

    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "completed"])
    completed = Repo.get!(Task, task.id)

    assert Budget.reconcile_reservation!(goal, completed, now: @now) == true

    assert Repo.one!(from(reservation in GoalBudgetReservation, where: reservation.task_id == ^task.id)).state ==
             "released"

    assert Budget.reconcile_reservation!(goal, completed, now: @now) == false
  end
end
