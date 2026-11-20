# 0007c planning-fixture: applies the state this increment must present as
# unwritable, using Control's own changesets on real database rows.
#
#   --goal-managed <work_item_id> <repository_resource_id>
#     Inserts a Goal row and applies WorkItem.goal_membership_changeset/2 with
#     the all-or-nothing membership (goal_id, admitted_revision,
#     acceptance_contract) plus the repository baseline the first admission
#     requires. This is the ownership state the legacy boundary refuses writes
#     for, written with the same changesets and database constraints the Goal
#     admission path uses; the Goal plan/approve flow itself belongs to 0007h
#     and is not exercised here.
#
#   --provider-owned <work_item_id> <resource_id> <provider> <external_id>
#     Applies WorkItem.provider_changeset/2 with an external work-tracker
#     binding, which is what the provider-sync path writes; the served row then
#     carries the tracker's provider_owned_fields and Control refuses edits to
#     its provider-owned fields.
#
# Every other interaction of the browser suite under test is real HTTP against
# the running control plane and the real PostgreSQL database.
defmodule PortalPlanningFixture do
  @moduledoc false

  alias SymmetryControl.Goals.{Goal, GoalRevision}
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces.{ProjectResource, WorkItem}

  @usage "usage: --goal-managed <work_item_id> <repository_resource_id> | " <>
           "--provider-owned <work_item_id> <resource_id> <provider> <external_id>"

  # The v1 execution policy shape the Goal revision contracts require. Automatic
  # execution stays off: this fixture freezes ownership state, it does not ask
  # Control to schedule anything.
  @execution_policy %{
    "automatic_execution" => false,
    "max_parallel_tasks" => 1,
    "max_task_admissions" => 1,
    "max_run_attempts_per_task" => 1,
    "budget_limit_microusd" => nil,
    "per_run_cost_limit_microusd" => nil,
    "budget_mode" => "soft",
    "hard_cost_limit_required" => false,
    "allowed_runtime_ids" => [],
    "allowed_model_profiles" => [],
    "final_acceptance" => "operator",
    "allowed_actions" => [],
    "allowed_resource_ids" => []
  }

  def main(["--goal-managed", work_item_id, repository_resource_id]) do
    ensure_started()

    work_item = fetch_work_item(work_item_id)
    repository_resource = fetch_repository_resource(repository_resource_id)

    # goals.current_revision has a DEFERRABLE FK into goal_revisions, so the
    # goal row, its revision and the membership write commit as one transaction
    # - the same shape the Goal admission path uses.
    {:ok, goal} =
      Repo.transaction(fn ->
        goal =
          %Goal{}
          |> Goal.changeset(%{
            project_id: work_item.project_id,
            title: "0007c fixture goal",
            state: "active",
            current_revision: 1
          })
          |> Repo.insert!()

        %GoalRevision{}
        |> GoalRevision.changeset(%{
          goal_id: goal.id,
          revision: 1,
          objective: "Freeze the ownership state the 0007c browser suite presents as unwritable",
          non_goals: [],
          acceptance_contract: %{"checks" => ["mix test"]},
          authority_policy: %{},
          execution_policy: @execution_policy,
          context_manifest: %{},
          reason: "0007c planning fixture",
          actor_ref: "0007c-planning-fixture"
        })
        |> Repo.insert!()

        # The admission baseline is frozen from the item's repository resource,
        # so the repository binding is written first: the database rejects a
        # baseline change after admission.
        work_item
        |> WorkItem.changeset(%{repository_resource_id: repository_resource.id})
        |> Repo.update!()
        |> WorkItem.goal_membership_changeset(%{
          goal_id: goal.id,
          admitted_revision: 1,
          # The database asserts that every admitted (goal, revision) pair has
          # an integrated work item; this fixture item is that item.
          integration: true,
          acceptance_contract: %{"checks" => ["mix test"]},
          baseline_subject: %{
            "resource_id" => repository_resource.id,
            "commit" => String.duplicate("a", 40),
            "tree_digest" => "sha256:" <> String.duplicate("b", 64)
          }
        })
        |> Repo.update!()

        goal
      end)

    IO.puts("goal-managed #{work_item.id} goal #{goal.id}")
  end

  def main(["--provider-owned", work_item_id, resource_id, provider, external_id]) do
    ensure_started()

    work_item = fetch_work_item(work_item_id)

    work_item
    |> WorkItem.provider_changeset(%{
      external_work_item_resource_id: resource_id,
      external_provider: provider,
      external_id: external_id,
      external_url: "https://example.invalid/tracker/#{external_id}",
      external_state: "open",
      external_updated_at: DateTime.utc_now(),
      external_available: true,
      labels: []
    })
    |> Repo.update!()

    IO.puts("provider-owned #{work_item.id} #{provider}/#{external_id}")
  end

  def main(_argv) do
    IO.puts(:stderr, @usage)
    System.halt(2)
  end

  defp fetch_work_item(work_item_id) do
    case Ecto.UUID.cast(work_item_id) do
      {:ok, id} -> Repo.get!(WorkItem, id)
      :error -> raise ArgumentError, "not a UUID: #{work_item_id}"
    end
  end

  defp fetch_repository_resource(resource_id) do
    resource =
      case Ecto.UUID.cast(resource_id) do
        {:ok, id} -> Repo.get!(ProjectResource, id)
        :error -> raise ArgumentError, "not a UUID: #{resource_id}"
      end

    if resource.kind == "repository" do
      resource
    else
      raise ArgumentError,
            "baseline resource #{resource.id} is a #{resource.kind}, not a repository"
    end
  end

  defp ensure_started do
    {:ok, _applications} = Application.ensure_all_started(:symmetry_control)
    :ok
  end
end

PortalPlanningFixture.main(System.argv())
