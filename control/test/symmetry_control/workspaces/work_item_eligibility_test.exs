defmodule SymmetryControl.Workspaces.WorkItemEligibilityTest do
  @moduledoc false
  use ExUnit.Case, async: true

  alias SymmetryControl.Workspaces.Project
  alias SymmetryControl.Workspaces.WorkItem
  alias SymmetryControl.Workspaces.WorkItemEligibility

  @project_id "4f3a3f38-9a3a-4c9a-8a3a-4f3a3f38d0a1"
  @work_item_id "4f3a3f38-9a3a-4c9a-8a3a-4f3a3f38d0a2"
  @task_id "4f3a3f38-9a3a-4c9a-8a3a-4f3a3f38d0a3"
  @goal_id "4f3a3f38-9a3a-4c9a-8a3a-4f3a3f38d0a4"
  @resource_id "4f3a3f38-9a3a-4c9a-8a3a-4f3a3f38d0a5"

  @rows [
    {false, false, :human, :backlog, :archived, false, :s, false, :ok, true, :s, false},
    {false, false, :human, :backlog, :archived, true, :s, false, :ok, true, :s, false},
    {false, false, :human, :backlog, :active, false, :s, false, :ok, true, :s, false},
    {false, false, :human, :backlog, :active, true, :s, false, :ok, true, :s, false},
    {false, false, :human, :ready, :archived, false, :s, false, :ok, true, :s, false},
    {false, false, :human, :ready, :archived, true, :s, false, :ok, true, :s, false},
    {false, false, :human, :ready, :active, false, :s, false, :ok, true, :s, false},
    {false, false, :human, :ready, :active, true, :s, false, :ok, true, :s, false},
    {false, false, :human, :in_progress, :archived, false, :s, false, :ok, true, :s, false},
    {false, false, :human, :in_progress, :archived, true, :s, false, :ok, true, :s, false},
    {false, false, :human, :in_progress, :active, false, :s, false, :ok, true, :s, false},
    {false, false, :human, :in_progress, :active, true, :s, false, :ok, true, :s, false},
    {false, false, :agent, :backlog, :archived, false, :s, false, :ok, true, :s, false},
    {false, false, :agent, :backlog, :archived, true, :s, false, :ok, true, :s, false},
    {false, false, :agent, :backlog, :active, false, :s, false, :ok, true, :s, false},
    {false, false, :agent, :backlog, :active, true, :ok, true, :ok, true, :ok, true},
    {false, false, :agent, :ready, :archived, false, :s, false, :ok, true, :s, false},
    {false, false, :agent, :ready, :archived, true, :s, false, :ok, true, :s, false},
    {false, false, :agent, :ready, :active, false, :s, false, :ok, true, :s, false},
    {false, false, :agent, :ready, :active, true, :ok, true, :ok, true, :ok, true},
    {false, false, :agent, :in_progress, :archived, false, :s, false, :ok, true, :s, false},
    {false, false, :agent, :in_progress, :archived, true, :s, false, :ok, true, :s, false},
    {false, false, :agent, :in_progress, :active, false, :s, false, :ok, true, :s, false},
    {false, false, :agent, :in_progress, :active, true, :s, false, :ok, true, :ok, true},
    {false, true, :human, :backlog, :archived, false, :s, false, :ok, true, :s, false},
    {false, true, :human, :backlog, :archived, true, :s, false, :ok, true, :s, false},
    {false, true, :human, :backlog, :active, false, :ok, false, :ok, true, :s, false},
    {false, true, :human, :backlog, :active, true, :ok, false, :ok, true, :s, false},
    {false, true, :human, :ready, :archived, false, :s, false, :ok, true, :s, false},
    {false, true, :human, :ready, :archived, true, :s, false, :ok, true, :s, false},
    {false, true, :human, :ready, :active, false, :ok, false, :ok, true, :s, false},
    {false, true, :human, :ready, :active, true, :ok, false, :ok, true, :s, false},
    {false, true, :human, :in_progress, :archived, false, :s, false, :ok, true, :s, false},
    {false, true, :human, :in_progress, :archived, true, :s, false, :ok, true, :s, false},
    {false, true, :human, :in_progress, :active, false, :ok, false, :ok, true, :s, false},
    {false, true, :human, :in_progress, :active, true, :ok, false, :ok, true, :s, false},
    {false, true, :agent, :backlog, :archived, false, :s, false, :ok, true, :s, false},
    {false, true, :agent, :backlog, :archived, true, :s, false, :ok, true, :s, false},
    {false, true, :agent, :backlog, :active, false, :ok, false, :ok, true, :s, false},
    {false, true, :agent, :backlog, :active, true, :ok, false, :ok, true, :ok, true},
    {false, true, :agent, :ready, :archived, false, :s, false, :ok, true, :s, false},
    {false, true, :agent, :ready, :archived, true, :s, false, :ok, true, :s, false},
    {false, true, :agent, :ready, :active, false, :ok, false, :ok, true, :s, false},
    {false, true, :agent, :ready, :active, true, :ok, false, :ok, true, :ok, true},
    {false, true, :agent, :in_progress, :archived, false, :s, false, :ok, true, :s, false},
    {false, true, :agent, :in_progress, :archived, true, :s, false, :ok, true, :s, false},
    {false, true, :agent, :in_progress, :active, false, :ok, false, :ok, true, :s, false},
    {false, true, :agent, :in_progress, :active, true, :ok, false, :ok, true, :ok, true},
    {true, false, :human, :backlog, :archived, false, :g, false, :g, false, :g, false},
    {true, false, :human, :backlog, :archived, true, :g, false, :g, false, :g, false},
    {true, false, :human, :backlog, :active, false, :g, false, :g, false, :g, false},
    {true, false, :human, :backlog, :active, true, :g, false, :g, false, :g, false},
    {true, false, :human, :ready, :archived, false, :g, false, :g, false, :g, false},
    {true, false, :human, :ready, :archived, true, :g, false, :g, false, :g, false},
    {true, false, :human, :ready, :active, false, :g, false, :g, false, :g, false},
    {true, false, :human, :ready, :active, true, :g, false, :g, false, :g, false},
    {true, false, :human, :in_progress, :archived, false, :g, false, :g, false, :g, false},
    {true, false, :human, :in_progress, :archived, true, :g, false, :g, false, :g, false},
    {true, false, :human, :in_progress, :active, false, :g, false, :g, false, :g, false},
    {true, false, :human, :in_progress, :active, true, :g, false, :g, false, :g, false},
    {true, false, :agent, :backlog, :archived, false, :g, false, :g, false, :g, false},
    {true, false, :agent, :backlog, :archived, true, :g, false, :g, false, :g, false},
    {true, false, :agent, :backlog, :active, false, :g, false, :g, false, :g, false},
    {true, false, :agent, :backlog, :active, true, :g, false, :g, false, :g, true},
    {true, false, :agent, :ready, :archived, false, :g, false, :g, false, :g, false},
    {true, false, :agent, :ready, :archived, true, :g, false, :g, false, :g, false},
    {true, false, :agent, :ready, :active, false, :g, false, :g, false, :g, false},
    {true, false, :agent, :ready, :active, true, :g, false, :g, false, :g, true},
    {true, false, :agent, :in_progress, :archived, false, :g, false, :g, false, :g, false},
    {true, false, :agent, :in_progress, :archived, true, :g, false, :g, false, :g, false},
    {true, false, :agent, :in_progress, :active, false, :g, false, :g, false, :g, false},
    {true, false, :agent, :in_progress, :active, true, :g, false, :g, false, :g, true},
    {true, true, :human, :backlog, :archived, false, :g, false, :g, false, :g, false},
    {true, true, :human, :backlog, :archived, true, :g, false, :g, false, :g, false},
    {true, true, :human, :backlog, :active, false, :g, false, :g, false, :g, false},
    {true, true, :human, :backlog, :active, true, :g, false, :g, false, :g, false},
    {true, true, :human, :ready, :archived, false, :g, false, :g, false, :g, false},
    {true, true, :human, :ready, :archived, true, :g, false, :g, false, :g, false},
    {true, true, :human, :ready, :active, false, :g, false, :g, false, :g, false},
    {true, true, :human, :ready, :active, true, :g, false, :g, false, :g, false},
    {true, true, :human, :in_progress, :archived, false, :g, false, :g, false, :g, false},
    {true, true, :human, :in_progress, :archived, true, :g, false, :g, false, :g, false},
    {true, true, :human, :in_progress, :active, false, :g, false, :g, false, :g, false},
    {true, true, :human, :in_progress, :active, true, :g, false, :g, false, :g, false},
    {true, true, :agent, :backlog, :archived, false, :g, false, :g, false, :g, false},
    {true, true, :agent, :backlog, :archived, true, :g, false, :g, false, :g, false},
    {true, true, :agent, :backlog, :active, false, :g, false, :g, false, :g, false},
    {true, true, :agent, :backlog, :active, true, :g, false, :g, false, :g, true},
    {true, true, :agent, :ready, :archived, false, :g, false, :g, false, :g, false},
    {true, true, :agent, :ready, :archived, true, :g, false, :g, false, :g, false},
    {true, true, :agent, :ready, :active, false, :g, false, :g, false, :g, false},
    {true, true, :agent, :ready, :active, true, :g, false, :g, false, :g, true},
    {true, true, :agent, :in_progress, :archived, false, :g, false, :g, false, :g, false},
    {true, true, :agent, :in_progress, :archived, true, :g, false, :g, false, :g, false},
    {true, true, :agent, :in_progress, :active, false, :g, false, :g, false, :g, false},
    {true, true, :agent, :in_progress, :active, true, :g, false, :g, false, :g, true}
  ]

  test "every input combination answers the baseline verdict and projection boolean" do
    Enum.each(@rows, &assert_row/1)
  end

  test "an unloaded project association is not an active project" do
    work_item = work_item(false, false, :agent, :backlog, true, unloaded_project())

    assert WorkItemEligibility.start(work_item) == {:error, :state_conflict}
    refute WorkItemEligibility.start_allowed?(work_item)
    assert WorkItemEligibility.retry_gate(work_item) == {:error, :state_conflict}
    refute WorkItemEligibility.retry_allowed?(work_item)
  end

  test "an item without an external work binding is available whatever the flag carries" do
    work_item = work_item(false, false, :agent, :backlog, true, project(:active))
    work_item = %{work_item | external_available: false}

    assert WorkItemEligibility.start(work_item) == :ok
    assert WorkItemEligibility.start_allowed?(work_item)
    assert WorkItemEligibility.retry_allowed?(work_item)
  end

  test "a bound external work resource is available when its own flag allows it" do
    work_item = work_item(false, false, :agent, :backlog, false, project(:active))
    work_item = %{work_item | external_available: true}

    assert WorkItemEligibility.start(work_item) == :ok
    assert WorkItemEligibility.start_allowed?(work_item)
    assert WorkItemEligibility.retry_gate(work_item) == :ok
    assert WorkItemEligibility.retry_allowed?(work_item)
  end

  defp assert_row(
         {goal, launched, assignee, status, project_status, external, start, can_start, cancel, can_cancel, retry,
          can_retry}
       ) do
    work_item =
      work_item(goal, launched, assignee, status, external, project(project_status))

    label = "row #{inspect({goal, launched, assignee, status, project_status, external})}"

    assert WorkItemEligibility.start(work_item) == verdict(start), label
    assert WorkItemEligibility.start_allowed?(work_item) == can_start, label
    assert WorkItemEligibility.cancel_gate(work_item) == verdict(cancel), label
    assert WorkItemEligibility.cancel_allowed?(work_item) == can_cancel, label
    assert WorkItemEligibility.retry_gate(work_item) == verdict(retry), label
    assert WorkItemEligibility.retry_allowed?(work_item) == can_retry, label
  end

  defp verdict(:ok), do: :ok
  defp verdict(:s), do: {:error, :state_conflict}
  defp verdict(:g), do: {:error, :goal_authority_required}

  defp project(:active), do: %Project{id: @project_id, status: "active"}
  defp project(:archived), do: %Project{id: @project_id, status: "archived"}

  defp unloaded_project do
    %Ecto.Association.NotLoaded{__field__: :project, __owner__: WorkItem, __cardinality__: :one}
  end

  defp work_item(goal, launched, assignee, status, external, project) do
    %WorkItem{
      id: @work_item_id,
      project_id: @project_id,
      number: 1,
      title: "eligibility row",
      status: Atom.to_string(status),
      priority: "no_priority",
      assignee_type: Atom.to_string(assignee),
      agent_profile: if(assignee == :agent, do: "codex"),
      workspace: "primary",
      blocked: false,
      orchestration_task_id: if(launched, do: @task_id),
      goal_id: if(goal, do: @goal_id),
      admitted_revision: if(goal, do: 1),
      acceptance_contract: if(goal, do: %{"checks" => ["mix test"]}),
      external_work_item_resource_id: if(external, do: nil, else: @resource_id),
      external_available: external,
      project: project
    }
  end
end
