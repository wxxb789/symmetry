defmodule SymmetryControl.WorkspacesCompletionTest do
  use SymmetryControl.DataCase, async: false

  alias Ecto.Adapters.SQL.Sandbox
  alias SymmetryControl.{Orchestration, Workspaces}
  alias SymmetryControl.Workspaces.ProjectResource

  test "project settings use optimistic versions and archiving preserves project data" do
    project = project_fixture()

    assert {:ok, updated} =
             Workspaces.update_project(project.id, %{
               version: project.lock_version,
               name: "Symmetry Platform",
               description: "Daily engineering workspace",
               default_agent_profile: "codex",
               default_workspace: "repository"
             })

    assert updated.name == "Symmetry Platform"
    assert updated.key == "SYM"
    assert updated.lock_version == project.lock_version + 1

    assert {:error, :stale} =
             Workspaces.update_project(project.id, %{
               version: project.lock_version,
               name: "Stale name"
             })

    assert {:error, changeset} =
             Workspaces.update_project(updated.id, %{
               version: updated.lock_version,
               key: "OTHER"
             })

    assert "cannot be changed" in errors_on(changeset).key

    assert {:ok, resource} =
             Workspaces.create_resource(project.id, %{
               kind: "repository",
               name: "symmetry",
               status: "healthy",
               sync_status: "synced"
             })

    assert {:ok, item} = Workspaces.create_work_item(project.id, %{title: "Keep history"})

    assert {:ok, archived} =
             Workspaces.update_project(updated.id, %{
               version: updated.lock_version,
               status: "archived"
             })

    assert archived.status == "archived"
    assert {:ok, snapshot} = Workspaces.workspace_snapshot(archived.id)
    assert snapshot.selected_project.status == "archived"
    assert Enum.map(snapshot.resources, & &1.id) == [resource.id]
    assert Enum.map(snapshot.work_items, & &1.id) == [item.id]

    assert {:ok, restored} =
             Workspaces.update_project(archived.id, %{
               version: archived.lock_version,
               status: "active"
             })

    assert restored.status == "active"
  end

  test "resources separate connection and synchronization state and detach safely" do
    project = project_fixture()
    checked_at = ~U[2026-09-04 08:00:00.000000Z]
    synced_at = ~U[2026-09-04 07:55:00.000000Z]

    assert {:error, changeset} =
             Workspaces.create_resource(project.id, %{
               kind: "ci",
               name: "CI",
               status: "healthy",
               sync_status: "failed"
             })

    assert "must be present when health or synchronization needs attention" in errors_on(
             changeset
           ).status_message

    assert {:ok, resource} =
             Workspaces.create_resource(project.id, %{
               kind: "ci",
               name: "CI",
               provider: "github",
               status: "healthy",
               sync_status: "stale",
               status_message: "Webhook has not delivered recently",
               last_checked_at: checked_at,
               last_synced_at: synced_at
             })

    assert resource.status == "healthy"
    assert resource.sync_status == "stale"
    assert resource.last_checked_at == checked_at

    assert {:ok, updated} =
             Workspaces.update_resource(resource.id, %{
               version: resource.lock_version,
               sync_status: "synced",
               status_message: "",
               last_synced_at: checked_at
             })

    assert updated.sync_status == "synced"
    assert updated.status_message == nil

    assert {:error, :stale} =
             Workspaces.update_resource(resource.id, %{
               version: resource.lock_version,
               name: "Stale edit"
             })

    assert {:error, :stale} = Workspaces.delete_resource(resource.id, resource.lock_version)
    assert {:ok, deleted} = Workspaces.delete_resource(resource.id, updated.lock_version)
    assert deleted.id == resource.id

    assert {:error, :not_found} =
             Workspaces.update_resource(resource.id, %{version: updated.lock_version})
  end

  test "agent and runtime resources require registered identities" do
    project = project_fixture()
    %{machine: machine} = enroll_machine("resource-identity-machine")
    runtime = register_runtime(machine, "resource-identity-runtime", "codex")

    for attrs <- [
          %{kind: "runtime", name: "Missing runtime"},
          %{kind: "runtime", name: "Malformed runtime", external_ref: "not-a-uuid"},
          %{kind: "runtime", name: "Unknown runtime", external_ref: Ecto.UUID.generate()},
          %{kind: "agent", name: "Missing agent"},
          %{kind: "agent", name: "Unknown agent", external_ref: "unknown-profile"}
        ] do
      assert {:error, changeset} =
               Workspaces.create_resource(
                 project.id,
                 Map.merge(attrs, %{status: "healthy", sync_status: "synced"})
               )

      assert errors_on(changeset).external_ref != []
    end

    assert {:ok, runtime_resource} =
             Workspaces.create_resource(project.id, %{
               kind: "runtime",
               name: "Registered runtime",
               external_ref: runtime.id,
               status: "healthy",
               sync_status: "synced"
             })

    assert {:ok, agent_resource} =
             Workspaces.create_resource(project.id, %{
               kind: "agent",
               name: "Registered agent",
               external_ref: runtime.agent_profile,
               status: "healthy",
               sync_status: "synced"
             })

    assert agent_resource.external_ref == "codex"

    assert {:error, invalid_update} =
             Workspaces.update_resource(runtime_resource.id, %{
               version: runtime_resource.lock_version,
               external_ref: Ecto.UUID.generate()
             })

    assert errors_on(invalid_update).external_ref != []
    assert Repo.get!(ProjectResource, runtime_resource.id).external_ref == runtime.id
  end

  test "work item ownership and blocker fields normalize to coherent states" do
    project = project_fixture()

    assert {:error, human_changeset} =
             Workspaces.create_work_item(project.id, %{
               title: "Human work",
               assignee_type: "human"
             })

    assert "must be present for a human assignee" in errors_on(human_changeset).assignee_name

    assert {:ok, agent_item} =
             Workspaces.create_work_item(project.id, %{
               title: "Agent work",
               assignee_type: "agent",
               assignee_name: "",
               blocked: true,
               blocker: "Waiting for credentials"
             })

    assert agent_item.agent_profile == project.default_agent_profile
    assert agent_item.assignee_name == project.default_agent_profile
    assert agent_item.workspace == project.default_workspace

    assert {:ok, human} =
             Workspaces.update_work_item(agent_item.id, %{
               version: agent_item.lock_version,
               assignee_type: "human",
               assignee_name: "Lina"
             })

    assert human.assignee_type == "human"
    assert human.assignee_name == "Lina"
    assert human.agent_profile == nil
    assert human.workspace == nil

    assert {:ok, cleared} =
             Workspaces.update_work_item(human.id, %{
               version: human.lock_version,
               assignee_type: "unassigned",
               blocked: false
             })

    assert cleared.assignee_type == "unassigned"
    assert cleared.assignee_name == nil
    assert cleared.agent_profile == nil
    assert cleared.workspace == nil
    assert cleared.blocker == nil

    assert {:ok, unblocked} =
             Workspaces.create_work_item(project.id, %{
               title: "Unblocked work",
               blocked: false,
               blocker: "must not survive"
             })

    assert unblocked.blocker == nil

    assert {:error, :stale} =
             Workspaces.update_work_item(agent_item.id, %{
               version: agent_item.lock_version,
               title: "Stale title"
             })
  end

  test "Kanban moves persist exact order within and across columns" do
    project = project_fixture()
    first = work_item_fixture(project, "First", "ready")
    second = work_item_fixture(project, "Second", "ready")
    _third = work_item_fixture(project, "Third", "ready")
    review = work_item_fixture(project, "Review", "review")

    assert {:ok, moved_second} =
             Workspaces.move_work_item(second.id, %{
               version: second.lock_version,
               status: "ready",
               before_id: first.id
             })

    assert moved_second.status == "ready"
    assert titles_in(project.id, "ready") == ["Second", "First", "Third"]

    first = fetch_item(first.id)

    assert {:ok, moved_first} =
             Workspaces.move_work_item(first.id, %{
               version: first.lock_version,
               status: "review",
               before_id: review.id
             })

    assert moved_first.status == "review"
    assert titles_in(project.id, "ready") == ["Second", "Third"]
    assert titles_in(project.id, "review") == ["First", "Review"]

    assert {:error, :stale} =
             Workspaces.move_work_item(first.id, %{
               version: first.lock_version,
               status: "done"
             })

    other_project = project_fixture("OTHER")
    other_item = work_item_fixture(other_project, "Elsewhere", "done")

    assert {:error, :invalid_request} =
             Workspaces.move_work_item(moved_first.id, %{
               version: moved_first.lock_version,
               status: "done",
               before_id: other_item.id
             })

    assert titles_in(project.id, "review") == ["First", "Review"]
  end

  test "repository bindings stay within a project and prevent destructive detach" do
    project = project_fixture()
    other_project = project_fixture("OUT")

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{
               kind: "repository",
               name: "symmetry",
               status: "healthy",
               sync_status: "synced"
             })

    assert {:ok, other_repository} =
             Workspaces.create_resource(other_project.id, %{
               kind: "repository",
               name: "other",
               status: "healthy",
               sync_status: "synced"
             })

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Bound work",
               repository_resource_id: repository.id
             })

    assert item.repository_resource_id == repository.id

    assert {:error, :state_conflict} =
             Workspaces.delete_resource(repository.id, repository.lock_version)

    assert {:error, :state_conflict} =
             Workspaces.update_resource(repository.id, %{
               version: repository.lock_version,
               kind: "ci"
             })

    assert Repo.get!(ProjectResource, repository.id).kind == "repository"

    assert {:error, cross_project_changeset} =
             Workspaces.update_work_item(item.id, %{
               version: item.lock_version,
               repository_resource_id: other_repository.id
             })

    assert "must belong to the work item's project" in errors_on(cross_project_changeset).repository_resource_id

    assert {:ok, cleared} =
             Workspaces.update_work_item(item.id, %{
               version: item.lock_version,
               repository_resource_id: nil
             })

    assert cleared.repository_resource_id == nil
    assert {:ok, _deleted} = Workspaces.delete_resource(repository.id, repository.lock_version)
  end

  test "active execution freezes intent fields and prevents project archive" do
    project = project_fixture()

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Immutable active intent",
               description: "Original goal",
               status: "ready",
               assignee_type: "agent"
             })

    assert {:ok, launched, :created} = Workspaces.launch_work_item(item.id)

    assert {:error, :state_conflict} =
             Workspaces.update_work_item(item.id, %{
               version: launched.work_item.lock_version,
               title: "Different goal"
             })

    assert {:ok, delivery_update} =
             Workspaces.update_work_item(item.id, %{
               version: launched.work_item.lock_version,
               ci_status: "pending"
             })

    assert delivery_update.ci_status == "pending"

    assert {:error, :state_conflict} =
             Workspaces.update_project(project.id, %{
               version: project.lock_version,
               status: "archived"
             })

    assert {:ok, _task, _command} =
             Orchestration.request_cancel(launched.task.task.id)

    item = fetch_item(item.id)

    assert {:ok, changed} =
             Workspaces.update_work_item(item.id, %{
               version: item.lock_version,
               title: "Retry goal"
             })

    assert changed.title == "Retry goal"

    assert {:ok, archived} =
             Workspaces.update_project(project.id, %{
               version: project.lock_version,
               status: "archived"
             })

    assert archived.status == "archived"
  end

  test "failed work cannot retry after reassignment away from an agent" do
    project = project_fixture()

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Reassigned failure",
               status: "ready",
               assignee_type: "agent"
             })

    assert {:ok, launched, :created} = Workspaces.launch_work_item(item.id)
    %{machine: machine} = enroll_machine("retry-policy-machine")
    runtime = register_runtime(machine, "retry-policy-runtime", project.default_agent_profile)
    assert {:ok, run} = Orchestration.assign_one(now: ~U[2026-09-04 08:00:00.000000Z])
    fence = claim(run, runtime)

    assert {:ok, _running} =
             Orchestration.transition(
               run.id,
               fence,
               "running",
               %{},
               Ecto.UUID.generate(),
               now: ~U[2026-09-04 08:00:01.000000Z]
             )

    assert {:ok, _failed} =
             Orchestration.transition(
               run.id,
               fence,
               "failed",
               %{"reason" => "tests failed"},
               Ecto.UUID.generate(),
               now: ~U[2026-09-04 08:00:02.000000Z]
             )

    failed_item = fetch_item(item.id)

    assert {:ok, human_item} =
             Workspaces.update_work_item(failed_item.id, %{
               version: failed_item.lock_version,
               assignee_type: "human",
               assignee_name: "Lina"
             })

    assert {:error, :state_conflict} =
             Workspaces.retry_work_item(human_item.id, run.generation, "retry-after-human")

    persisted = fetch_item(item.id)
    assert persisted.assignee_type == "human"
    assert persisted.assignee_name == "Lina"
    assert persisted.status == "in_progress"

    assert {:ok, %{task: task}} = Orchestration.task_snapshot(launched.task.task.id)
    assert task.state == "failed"
    assert task.attempt_generation == run.generation
  end

  test "project archive and work-item launch serialize on the project lock" do
    key = unique_key("R")

    {project, item} =
      Sandbox.unboxed_run(Repo, fn ->
        project = project_fixture(key)

        assert {:ok, item} =
                 Workspaces.create_work_item(project.id, %{
                   title: "Archive race",
                   status: "ready",
                   assignee_type: "agent"
                 })

        {project, item}
      end)

    on_exit(fn -> cleanup_unboxed_project(project.id) end)

    results =
      concurrently([
        fn ->
          Workspaces.update_project(project.id, %{
            version: project.lock_version,
            status: "archived"
          })
        end,
        fn -> Workspaces.launch_work_item(item.id, "archive-race-start") end
      ])

    successes =
      Enum.count(results, fn
        {:ok, _project} -> true
        {:ok, _snapshot, _disposition} -> true
        _other -> false
      end)

    assert successes == 1
    assert Enum.count(results, &match?({:error, :state_conflict}, &1)) == 1

    Sandbox.unboxed_run(Repo, fn ->
      current_project = Repo.get!(SymmetryControl.Workspaces.Project, project.id)
      current_item = Repo.get!(SymmetryControl.Workspaces.WorkItem, item.id)

      refute current_project.status == "archived" and
               is_binary(current_item.orchestration_task_id)
    end)
  end

  test "concurrent create and move preserve exact unique Kanban positions" do
    key = unique_key("K")

    {project, first, second} =
      Sandbox.unboxed_run(Repo, fn ->
        project = project_fixture(key)
        first = work_item_fixture(project, "Concurrent first", "ready")
        second = work_item_fixture(project, "Concurrent second", "ready")
        {project, first, second}
      end)

    on_exit(fn -> cleanup_unboxed_project(project.id) end)

    results =
      concurrently([
        fn ->
          Workspaces.create_work_item(project.id, %{
            title: "Concurrent third",
            status: "ready",
            priority: "medium"
          })
        end,
        fn ->
          Workspaces.move_work_item(second.id, %{
            version: second.lock_version,
            status: "ready",
            before_id: first.id
          })
        end
      ])

    assert Enum.all?(results, &match?({:ok, _}, &1))

    Sandbox.unboxed_run(Repo, fn ->
      items =
        Repo.all(
          from item in SymmetryControl.Workspaces.WorkItem,
            where: item.project_id == ^project.id and item.status == "ready",
            order_by: [asc: item.position]
        )

      assert Enum.map(items, & &1.position) == [0, 1, 2]

      assert Enum.map(items, & &1.title) == [
               "Concurrent second",
               "Concurrent first",
               "Concurrent third"
             ]
    end)
  end

  defp project_fixture(key \\ "SYM") do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: "Project #{key}",
               key: key,
               default_agent_profile: "codex",
               default_workspace: "primary"
             })

    project
  end

  defp work_item_fixture(project, title, status) do
    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: title,
               status: status,
               priority: "medium"
             })

    item
  end

  defp enroll_machine(name) do
    key = Ecto.UUID.generate()

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(
               %{name: name, machine_token: "token-#{key}"},
               key,
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret",
               now: ~U[2026-09-04 08:00:00.000000Z]
             )

    enrolled
  end

  defp register_runtime(machine, runtime_key, agent_profile) do
    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: runtime_key,
                   name: runtime_key,
                   capacity: 1,
                   agent_profile: agent_profile,
                   workspace: "primary",
                   capabilities: %{}
                 }
               ],
               now: ~U[2026-09-04 08:00:00.000000Z]
             )

    runtime
  end

  defp claim(run, runtime) do
    claim_id = Ecto.UUID.generate()

    assert {:ok, claimed} =
             Orchestration.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: claim_id
               },
               now: ~U[2026-09-04 08:00:00.000000Z]
             )

    %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: claimed.claim_id,
      lease_token: claimed.lease_token
    }
  end

  defp fetch_item(id) do
    assert {:ok, item} = Workspaces.fetch_work_item(id)
    item
  end

  defp concurrently(functions) do
    parent = self()

    tasks =
      Enum.map(functions, fn function ->
        Task.async(fn ->
          Sandbox.unboxed_run(Repo, fn ->
            send(parent, {:ready, self()})
            receive do: (:go -> function.())
          end)
        end)
      end)

    for _function <- functions, do: assert_receive({:ready, _pid}, 1_000)
    Enum.each(tasks, &send(&1.pid, :go))
    Enum.map(tasks, &Task.await(&1, 5_000))
  end

  defp cleanup_unboxed_project(project_id) do
    Sandbox.unboxed_run(Repo, fn ->
      task_ids =
        Repo.all(
          from item in SymmetryControl.Workspaces.WorkItem,
            where: item.project_id == ^project_id and not is_nil(item.orchestration_task_id),
            select: item.orchestration_task_id
        )

      Repo.delete_all(
        from project in SymmetryControl.Workspaces.Project, where: project.id == ^project_id
      )

      Repo.delete_all(
        from task in SymmetryControl.Orchestration.Task, where: task.id in ^task_ids
      )
    end)
  end

  defp unique_key(prefix) do
    suffix = System.unique_integer([:positive]) |> rem(10_000_000) |> Integer.to_string()
    prefix <> String.pad_leading(suffix, 7, "0")
  end

  defp titles_in(project_id, status) do
    assert {:ok, snapshot} = Workspaces.workspace_snapshot(project_id)

    snapshot.work_items
    |> Enum.filter(&(&1.status == status))
    |> Enum.map(& &1.title)
  end
end
