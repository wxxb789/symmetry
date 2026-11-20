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

    refute WorkItem.goal_managed?(item)

    assert SymmetryControlWeb.PortalJSON.work_item(
             Repo.preload(item, [:project, :repository_resource, :ci_resource]),
             %{}
           )[:goal_managed] == false

    assert {:ok, item} =
             item
             |> WorkItem.goal_membership_changeset(%{
               goal_id: goal.id,
               admitted_revision: 1,
               acceptance_contract: %{"checks" => ["mix test"]},
               integration: true,
               baseline_subject: %{
                 "resource_id" => repository.id,
                 "commit" => String.duplicate("a", 40),
                 "tree_digest" => "sha256:" <> String.duplicate("4", 64)
               }
             })
             |> Repo.update()

    assert WorkItem.goal_managed?(item)

    assert SymmetryControlWeb.PortalJSON.work_item(
             Repo.preload(item, [:project, :repository_resource, :ci_resource]),
             %{}
           )[:goal_managed] == true

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

  test "work item creation replays a client-minted identity instead of duplicating the row" do
    project = project_fixture()

    assert {:ok, other_project} =
             Workspaces.create_project(%{
               name: "Other project",
               key: "OTHER",
               default_agent_profile: "codex",
               default_workspace: "primary"
             })

    assert {:ok, created} =
             Workspaces.create_work_item(project.id, %{
               title: "Original title",
               creation_id: "draft-42"
             })

    assert created.creation_id == "draft-42"
    assert Repo.get!(WorkItem, created.id).creation_id == "draft-42"
    count = Repo.aggregate(WorkItem, :count)

    # Replaying the identity returns the original row: the retried title is not applied, the
    # reply is neither a new creation nor an update, and nothing is written.
    assert {:replayed, replayed} =
             Workspaces.create_work_item(project.id, %{
               title: "Retried title",
               creation_id: "draft-42"
             })

    assert replayed.id == created.id
    assert replayed.title == "Original title"
    assert Repo.get!(WorkItem, created.id).title == "Original title"
    assert Repo.aggregate(WorkItem, :count) == count

    # A different identity is a genuinely new creation.
    assert {:ok, second} =
             Workspaces.create_work_item(project.id, %{
               title: "Second item",
               creation_id: "draft-43"
             })

    assert second.id != created.id
    assert Repo.aggregate(WorkItem, :count) == count + 1

    # The identity is scoped to the owning project.
    assert {:ok, cross_project} =
             Workspaces.create_work_item(other_project.id, %{
               title: "Same identity, other project",
               creation_id: "draft-42"
             })

    assert cross_project.id != created.id
    assert Repo.aggregate(WorkItem, :count) == count + 2

    # Malformed identities are refused before any write.
    for invalid <- ["", String.duplicate("a", 129), "a" <> <<0>> <> "b", 42] do
      assert {:error, :invalid_request} =
               Workspaces.create_work_item(project.id, %{
                 title: "Invalid identity",
                 creation_id: invalid
               })
    end

    assert Repo.aggregate(WorkItem, :count) == count + 2

    # The legacy call without an identity still inserts, without recording one.
    assert {:ok, legacy} = Workspaces.create_work_item(project.id, %{title: "Legacy item"})
    assert legacy.creation_id == nil
    assert Repo.aggregate(WorkItem, :count) == count + 3
  end

  test "retry binding fails closed without a loaded project" do
    item = %WorkItem{
      agent_profile: nil,
      workspace: nil,
      project: %Ecto.Association.NotLoaded{
        __field__: :project,
        __owner__: WorkItem,
        __cardinality__: :belongs_to
      }
    }

    # The project's defaults are unreachable, so the item's own blank values
    # stand alone and match no runtime: the served retry gate withholds the
    # control instead of the read raising on the missing association.
    blank = Workspaces.retry_binding(item)

    assert %{agent_profile: nil, workspace: nil, required_capabilities: %{}} = blank

    assert %{required_capabilities: %{"supervisory_control" => true}} =
             Workspaces.retry_binding(item, %{"supervisory_control" => true})

    # Power for the refutation: the registered runtime is selectable for a real
    # binding, and the blank binding with the same task identity matches no
    # runtime (fails closed).
    register_adapter_runtime()
    task_id = Ecto.UUID.generate()

    assert Orchestration.retry_runtime_eligible?(
             Map.merge(blank, %{task_id: task_id, agent_profile: "codex", workspace: "primary"})
           )

    refute Orchestration.retry_runtime_eligible?(Map.put(blank, :task_id, task_id))
  end

  test "retry binding resolves a blank binding as the project's defaults" do
    project = project_fixture()

    # A row with a blank agent binding cannot be written: the
    # `work_items_owner_fields_check` constraint requires an agent's profile and
    # workspace, so this arm is defensive and resolves the project defaults
    # instead of letting a blank binding match no runtime.
    item = %WorkItem{agent_profile: nil, workspace: nil, project: project}

    assert %{agent_profile: "codex", workspace: "primary", required_capabilities: %{}} =
             Workspaces.retry_binding(item)
  end

  # One enrolled machine with an adapter-backed runtime for the default binding,
  # advertising `start`: a binding a registered runtime can actually select.
  defp register_adapter_runtime do
    enrollment_key = Ecto.UUID.generate()

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(
               %{name: "retry-binding-machine", machine_token: "token-#{enrollment_key}"},
               enrollment_key,
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret"
             )

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               enrolled.machine.id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: "retry-binding-runtime",
                   name: "Retry binding runtime",
                   capacity: 1,
                   agent_profile: "codex",
                   workspace: "primary",
                   harness_kind: "generic",
                   harness_version: "generic-v1",
                   adapter_version: "generic-v1",
                   adapter_protocol_version: 1,
                   capabilities: %{
                     "adapter" => %{
                       "kind" => "generic",
                       "native_version" => "generic-v1",
                       "implementation_version" => "generic-v1",
                       "protocol_version" => 1,
                       "operations" => %{
                         "start" => true,
                         "events" => true,
                         "cancel" => true,
                         "resume" => false,
                         "handoff" => false,
                         "guidance" => "unsupported",
                         "pause" => "unsupported",
                         "approval_response" => false,
                         "usage" => "unknown",
                         "hard_cost_limit" => false
                       }
                     }
                   }
                 }
               ]
             )

    runtime
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
