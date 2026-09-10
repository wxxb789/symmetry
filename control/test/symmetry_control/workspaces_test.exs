defmodule SymmetryControl.WorkspacesTest do
  use SymmetryControl.DataCase, async: false

  alias SymmetryControl.Goals.{Goal, GoalRevision}
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.{ProjectResource, WorkItem}

  test "projects aggregate independent engineering resources and prioritized work items" do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: "Symmetry",
               key: "SYM",
               description: "Agent-native engineering workspace",
               default_agent_profile: "codex",
               default_workspace: "primary"
             })

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{
               kind: "repository",
               name: "symmetry",
               provider: "github",
               url: "https://github.com/acme/symmetry",
               status: "healthy"
             })

    assert {:ok, ci} =
             Workspaces.create_resource(project.id, %{
               kind: "ci",
               name: "GitHub Actions",
               provider: "github",
               status: "degraded",
               status_message: "Required check is delayed",
               metadata: %{"message" => "Required check is delayed"}
             })

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Ship the project portal",
               description: "Build the daily engineering control surface.",
               status: "ready",
               priority: "urgent",
               assignee_type: "human",
               assignee_name: "Lina",
               repository: "acme/symmetry",
               branch: "codex/goal-02",
               ci_status: "pending",
               review_status: "required"
             })

    assert item.number > 0
    assert item.status == "ready"

    assert {:ok, snapshot} = Workspaces.workspace_snapshot(project.id)
    assert snapshot.selected_project.id == project.id
    assert Enum.map(snapshot.projects, & &1.id) == [project.id]
    assert MapSet.new(Enum.map(snapshot.resources, & &1.id)) == MapSet.new([repository.id, ci.id])
    assert Enum.map(snapshot.work_items, & &1.id) == [item.id]
  end

  test "work items validate workflow state and blocker detail" do
    project = project_fixture()

    assert {:error, resource_changeset} =
             Workspaces.create_resource(project.id, %{
               kind: "repository",
               name: "unsafe",
               url: "javascript:alert(document.cookie)"
             })

    assert "must be an http or https URL" in errors_on(resource_changeset).url

    assert {:error, changeset} =
             Workspaces.create_work_item(project.id, %{
               title: "Investigate flaky CI",
               status: "ready",
               priority: "critical",
               blocked: true,
               pull_request_url: "javascript:alert(document.cookie)"
             })

    assert "is invalid" in errors_on(changeset).priority
    assert "must be present when blocked" in errors_on(changeset).blocker
    assert "must be an http or https URL" in errors_on(changeset).pull_request_url

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Investigate flaky CI",
               status: "backlog",
               priority: "high"
             })

    assert {:ok, updated} =
             Workspaces.update_work_item(item.id, %{
               version: item.lock_version,
               assignee_type: "agent",
               assignee_name: "Codex",
               agent_profile: "codex",
               ci_status: "failed",
               review_status: "changes_requested",
               blocked: true,
               blocker: "Linux integration test is failing"
             })

    assert {:ok, updated} =
             Workspaces.move_work_item(updated.id, %{
               version: updated.lock_version,
               status: "review"
             })

    assert updated.status == "review"
    assert updated.blocked
    assert updated.blocker == "Linux integration test is failing"
  end

  test "work item repository and CI bindings require the correct kind in the same project" do
    project = project_fixture()

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{kind: "repository", name: "Repository"})

    assert {:ok, ci} =
             Workspaces.create_resource(project.id, %{kind: "ci", name: "CI"})

    assert {:ok, other_project} =
             Workspaces.create_project(%{
               name: "Other project",
               key: "OTHER",
               default_agent_profile: "codex",
               default_workspace: "primary"
             })

    assert {:ok, other_ci} =
             Workspaces.create_resource(other_project.id, %{kind: "ci", name: "Other CI"})

    assert {:error, invalid_repository} =
             Workspaces.create_work_item(project.id, %{
               title: "Wrong repository kind",
               repository_resource_id: ci.id
             })

    assert "must belong to the work item's project" in errors_on(invalid_repository).repository_resource_id

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Validate CI bindings",
               repository_resource_id: repository.id
             })

    assert {:error, invalid_ci_kind} =
             Workspaces.update_work_item(item.id, %{
               version: item.lock_version,
               ci_resource_id: repository.id
             })

    assert "must be a CI resource in the work item's project" in errors_on(invalid_ci_kind).ci_resource_id

    assert {:error, cross_project_ci} =
             Workspaces.update_work_item(item.id, %{
               version: item.lock_version,
               ci_resource_id: other_ci.id
             })

    assert "must be a CI resource in the work item's project" in errors_on(cross_project_ci).ci_resource_id
  end

  test "updates continue to reject unchanged resource bindings that became invalid" do
    project = project_fixture()

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{kind: "repository", name: "Repository"})

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Validate unchanged bindings",
               repository_resource_id: repository.id
             })

    Repo.update_all(
      from(resource in ProjectResource, where: resource.id == ^repository.id),
      set: [kind: "ci"]
    )

    assert {:error, changeset} =
             Workspaces.update_work_item(item.id, %{
               version: item.lock_version,
               title: "Still validate unchanged bindings"
             })

    assert "must belong to the work item's project" in errors_on(changeset).repository_resource_id
  end

  test "launching an agent creates one durable task and links it to the work item" do
    project = project_fixture()

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Add run summaries",
               description: "Keep raw events behind progressive disclosure.",
               status: "ready",
               priority: "high",
               assignee_type: "agent",
               agent_profile: "codex",
               repository: "acme/symmetry"
             })

    assert {:ok, first, :created} = Workspaces.launch_work_item(item.id)
    assert first.work_item.status == "in_progress"
    assert first.work_item.assignee_type == "agent"
    assert first.work_item.assignee_name == "codex"
    assert first.work_item.orchestration_task_id == first.task.task.id
    assert first.task.task.state == "queued"
    assert first.task.task.agent_profile == "codex"
    assert first.task.task.workspace == "primary"

    assert first.task.task.input == %{
             "project_id" => project.id,
             "project_key" => project.key,
             "provider_resource_ids" => [],
             "repository" => "acme/symmetry",
             "repository_resource_id" => nil,
             "work_item_id" => item.id,
             "work_item_key" => "#{project.key}-#{item.number}"
           }

    assert {:ok, second, :replayed} = Workspaces.launch_work_item(item.id)
    assert second.task.task.id == first.task.task.id

    assert {:ok, stored_task} = Orchestration.fetch_task(first.task.task.id)
    assert stored_task.id == first.task.task.id
  end

  test "goal-owned work items reject legacy mutation and control paths" do
    project = project_fixture()

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{
               kind: "repository",
               name: "Goal-owned repository"
             })

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Goal-owned work",
               status: "ready",
               assignee_type: "agent",
               agent_profile: "codex",
               repository_resource_id: repository.id
             })

    goal = goal_fixture(project)

    assert {:ok, item} =
             item
             |> WorkItem.goal_membership_changeset(%{
               goal_id: goal.id,
               admitted_revision: 1,
               acceptance_contract: %{"checks" => ["mix test"]},
               baseline_subject: %{
                 "resource_id" => repository.id,
                 "commit" => String.duplicate("a", 40),
                 "tree_digest" => "sha256:" <> String.duplicate("4", 64)
               }
             })
             |> Repo.update()

    assert {:error, :goal_authority_required} =
             Workspaces.update_work_item(item.id, %{version: item.lock_version, title: "Bypass"})

    assert {:error, :goal_authority_required} =
             Workspaces.move_work_item(item.id, %{version: item.lock_version, status: "review"})

    assert {:error, :goal_authority_required} =
             Workspaces.launch_work_item(item.id, "goal-launch")

    assert {:error, :goal_authority_required} =
             Workspaces.cancel_work_item(item.id, 0, "goal-cancel")

    assert {:error, :goal_authority_required} =
             Workspaces.provide_work_item_input(
               item.id,
               %{"decision" => "continue"},
               Ecto.UUID.generate(),
               "goal-input"
             )

    assert {:error, :goal_authority_required} =
             Workspaces.retry_work_item(item.id, 0, "goal-retry")

    stored = Repo.get!(WorkItem, item.id)
    assert stored.orchestration_task_id == nil
    assert stored.status == "ready"
    assert stored.title == "Goal-owned work"
  end

  test "missing parents and work items return not found without orphaned records" do
    assert {:error, :not_found} =
             Workspaces.create_resource(Ecto.UUID.generate(), %{
               kind: "repository",
               name: "missing"
             })

    assert {:error, :not_found} =
             Workspaces.create_work_item(Ecto.UUID.generate(), %{title: "missing"})

    assert {:error, :not_found} = Workspaces.update_work_item(Ecto.UUID.generate(), %{})
    assert {:error, :not_found} = Workspaces.launch_work_item(Ecto.UUID.generate())
  end

  defp project_fixture do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: "Symmetry",
               key: "SYM",
               default_agent_profile: "codex",
               default_workspace: "primary"
             })

    project
  end

  defp goal_fixture(project) do
    assert {:ok, goal} =
             Repo.transaction(fn ->
               goal =
                 %Goal{}
                 |> Goal.changeset(%{
                   project_id: project.id,
                   title: "Protect legacy controls",
                   state: "active",
                   current_revision: 1,
                   event_sequence: 0
                 })
                 |> Repo.insert!()

               %GoalRevision{}
               |> GoalRevision.changeset(%{
                 goal_id: goal.id,
                 revision: 1,
                 objective: "Keep Goal authority intact",
                 non_goals: [],
                 acceptance_contract: %{"checks" => ["mix test"]},
                 authority_policy: %{},
                 execution_policy: execution_policy(),
                 context_manifest: %{},
                 reason: "initial admission",
                 actor_ref: "operator:test"
               })
               |> Repo.insert!()

               goal
             end)

    goal
  end

  defp execution_policy do
    %{
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
  end
end
