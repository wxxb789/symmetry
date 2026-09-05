defmodule SymmetryControl.WorkspacesTest do
  use SymmetryControl.DataCase, async: false

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Workspaces

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
end
