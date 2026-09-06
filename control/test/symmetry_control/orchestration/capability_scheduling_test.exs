defmodule SymmetryControl.Orchestration.CapabilitySchedulingTest do
  use SymmetryControl.DataCase, async: false

  alias SymmetryControl.Integrations
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces

  @now ~U[2026-09-06 09:00:00.000000Z]

  test "connected work snapshots immutable change scope and refreshes it on retry" do
    project = project_fixture()

    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: "Scheduling GitHub",
               account_ref: "acme",
               auth_type: "gh_cli",
               capabilities: ["repositories", "changes"]
             })

    assert {:ok, connected_repository} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "repository",
               name: "Connected repository",
               external_ref: "acme/symmetry"
             })

    connected_repository =
      connected_repository
      |> Ecto.Changeset.change(metadata: %{"default_branch" => "refs/heads/main"})
      |> Repo.update!()

    assert {:ok, manual_repository} =
             Workspaces.create_resource(project.id, %{
               kind: "repository",
               name: "Manual repository"
             })

    connected =
      work_item_fixture(project, "Connected work", %{
        repository_resource_id: connected_repository.id,
        branch: "refs/heads/codex/provider-scope",
        pull_request_url: "https://github.com/acme/symmetry/pull/42"
      })

    connected =
      connected
      |> Ecto.Changeset.change(
        external_pull_request_url: "https://github.com/acme/symmetry/pull/41"
      )
      |> Repo.update!()

    manual =
      work_item_fixture(project, "Manual work", %{
        repository_resource_id: manual_repository.id
      })

    native = work_item_fixture(project, "Native work")

    assert {:ok, connected_launch, :created} = Workspaces.launch_work_item(connected.id)
    assert {:ok, manual_launch, :created} = Workspaces.launch_work_item(manual.id)
    assert {:ok, native_launch, :created} = Workspaces.launch_work_item(native.id)

    assert connected_launch.task.task.required_capabilities == %{"provider_access" => true}

    assert connected_launch.task.task.input["provider_resource_ids"] == [
             connected_repository.id
           ]

    assert connected_launch.task.task.input["source_branch"] == "codex/provider-scope"
    assert connected_launch.task.task.input["target_branch"] == "main"

    assert connected_launch.task.task.input["pull_request_url"] ==
             "https://github.com/acme/symmetry/pull/41"

    assert manual_launch.task.task.required_capabilities == %{}
    assert native_launch.task.task.required_capabilities == %{}
    assert manual_launch.task.task.input["provider_resource_ids"] == []
    assert native_launch.task.task.input["provider_resource_ids"] == []

    assert {:error, :state_conflict} =
             Workspaces.update_work_item(connected.id, %{
               version: connected_launch.work_item.lock_version,
               branch: "codex/other"
             })

    assert {:error, :state_conflict} =
             Workspaces.update_work_item(connected.id, %{
               version: connected_launch.work_item.lock_version,
               pull_request_url: "https://github.com/acme/symmetry/pull/43"
             })

    assert {:ok, _cancelled, :created} =
             Workspaces.cancel_work_item(connected.id, 1, "cancel-connected")

    assert {:ok, updated} =
             Workspaces.update_work_item(connected.id, %{
               version: connected_launch.work_item.lock_version,
               branch: "refs/heads/codex/provider-scope-v2",
               pull_request_url: "https://github.com/acme/symmetry/pull/43"
             })

    updated =
      updated
      |> Ecto.Changeset.change(external_pull_request_url: nil)
      |> Repo.update!()

    connected_repository
    |> Ecto.Changeset.change(metadata: %{"default_branch" => "refs/heads/develop"})
    |> Repo.update!()

    assert {:ok, retried, :created} =
             Workspaces.retry_work_item(updated.id, 1, "retry-connected")

    assert retried.task.task.required_capabilities == %{"provider_access" => true}
    assert retried.task.task.input["provider_resource_ids"] == [connected_repository.id]
    assert retried.task.task.input["source_branch"] == "codex/provider-scope-v2"
    assert retried.task.task.input["target_branch"] == "develop"

    assert retried.task.task.input["pull_request_url"] ==
             "https://github.com/acme/symmetry/pull/43"
  end

  test "provider resource snapshot excludes manual bindings and survives disconnect" do
    project = project_fixture()

    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: "Mixed resources",
               account_ref: "acme",
               auth_type: "gh_cli",
               capabilities: ["work_items", "ci"]
             })

    assert {:ok, tracker} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: "Connected tracker",
               external_ref: "acme/symmetry"
             })

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{
               kind: "repository",
               name: "Manual repository"
             })

    assert {:ok, ci} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "ci",
               name: "Connected CI",
               external_ref: "acme/symmetry"
             })

    item =
      work_item_fixture(project, "Mixed resources", %{
        repository_resource_id: repository.id,
        ci_resource_id: ci.id
      })

    item =
      item
      |> Ecto.Changeset.change(
        external_work_item_resource_id: tracker.id,
        external_provider: "github",
        external_id: "101"
      )
      |> Repo.update!()

    assert {:ok, launched, :created} = Workspaces.launch_work_item(item.id)

    assert launched.task.task.input["provider_resource_ids"] == [tracker.id, ci.id]

    tracker
    |> Ecto.Changeset.change(connection_id: nil)
    |> Repo.update!()

    assert {:ok, stored_task} = Orchestration.fetch_task(launched.task.task.id)
    assert stored_task.input["provider_resource_ids"] == [tracker.id, ci.id]
  end

  test "connected change-capable work cannot launch without server-owned branch scope" do
    project = project_fixture()

    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: "Scoped GitHub",
               account_ref: "acme",
               auth_type: "gh_cli",
               capabilities: ["repositories", "changes"]
             })

    assert {:ok, repository_with_target} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "repository",
               name: "Repository with target",
               external_ref: "acme/symmetry"
             })

    repository_with_target =
      repository_with_target
      |> Ecto.Changeset.change(metadata: %{"default_branch" => "main"})
      |> Repo.update!()

    missing_source =
      work_item_fixture(project, "Missing source", %{
        repository_resource_id: repository_with_target.id
      })

    retry_without_target =
      work_item_fixture(project, "Retry without target", %{
        repository_resource_id: repository_with_target.id,
        branch: "codex/retry-without-target"
      })

    assert {:error, :state_conflict} = Workspaces.launch_work_item(missing_source.id)
    assert {:ok, _launched, :created} = Workspaces.launch_work_item(retry_without_target.id)

    assert {:ok, _cancelled, :created} =
             Workspaces.cancel_work_item(retry_without_target.id, 1, "cancel-before-scope-loss")

    repository_with_target
    |> Ecto.Changeset.change(metadata: %{})
    |> Repo.update!()

    assert {:error, :state_conflict} =
             Workspaces.retry_work_item(retry_without_target.id, 1, "retry-after-scope-loss")

    assert {:ok, repository_without_target} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "repository",
               name: "Repository without target",
               external_ref: "acme/other"
             })

    missing_target =
      work_item_fixture(project, "Missing target", %{
        repository_resource_id: repository_without_target.id,
        branch: "codex/missing-target"
      })

    assert {:error, :state_conflict} = Workspaces.launch_work_item(missing_target.id)

    assert {:ok, missing_source} = Workspaces.fetch_work_item(missing_source.id)
    assert {:ok, missing_target} = Workspaces.fetch_work_item(missing_target.id)
    assert missing_source.orchestration_task_id == nil
    assert missing_target.orchestration_task_id == nil
  end

  test "scheduler skips an incompatible runtime and assigns a compatible runtime" do
    %{machine: incompatible_machine} = enroll_machine("incompatible-machine")
    %{machine: compatible_machine} = enroll_machine("compatible-machine")

    _incompatible =
      register_runtime(incompatible_machine, "incompatible", %{}, @now)

    assert {:ok, task, :created} =
             Orchestration.submit_task(
               task_attrs(required_capabilities: %{"provider_access" => true}),
               "provider-task",
               now: @now
             )

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)

    compatible =
      register_runtime(
        compatible_machine,
        "compatible",
        %{"structured_input" => true, "provider_access" => true, "shell" => true},
        DateTime.add(@now, 1, :microsecond)
      )

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == task.id
    assert run.runtime_id == compatible.id
  end

  test "claim rejects a runtime that became incompatible after assignment" do
    for {label, replacement} <- [
          {"profile", [agent_profile: "other"]},
          {"workspace", [workspace: "other"]},
          {"capabilities", [capabilities: %{"structured_input" => true}]}
        ] do
      %{machine: machine} = enroll_machine("claim-#{label}")
      daemon_instance_id = Ecto.UUID.generate()

      runtime =
        register_runtime(
          machine,
          "claim-#{label}",
          %{"structured_input" => true, "provider_access" => true},
          @now,
          daemon_instance_id: daemon_instance_id
        )

      assert {:ok, task, :created} =
               Orchestration.submit_task(
                 task_attrs(required_capabilities: %{"provider_access" => true}),
                 "claim-task-#{label}",
                 now: @now
               )

      assert {:ok, run} = Orchestration.assign_one(now: @now)
      assert run.runtime_id == runtime.id

      replacement_runtime =
        register_runtime(
          machine,
          runtime.runtime_key,
          Keyword.get(
            replacement,
            :capabilities,
            %{"structured_input" => true, "provider_access" => true}
          ),
          DateTime.add(@now, 1, :microsecond),
          daemon_instance_id: daemon_instance_id,
          agent_profile: Keyword.get(replacement, :agent_profile, "codex"),
          workspace: Keyword.get(replacement, :workspace, "primary")
        )

      assert replacement_runtime.id == runtime.id
      assert replacement_runtime.connection_epoch == runtime.connection_epoch

      assert {:error, :ownership_lost} =
               Orchestration.claim(
                 run.id,
                 %{
                   runtime_id: runtime.id,
                   runtime_epoch: runtime.connection_epoch,
                   generation: run.generation,
                   claim_id: Ecto.UUID.generate()
                 },
                 now: @now
               )

      assert {:ok, %{state: "assigned"}} = Orchestration.fetch_task(task.id)

      assert {:ok, %{state: "assigned", claim_id: nil, lease_token: nil}} =
               Orchestration.fetch_run(run.id)
    end
  end

  test "claim rejects a runtime that goes offline after assignment" do
    %{machine: machine} = enroll_machine("offline-claim")
    runtime = register_runtime(machine, "offline-claim", %{}, @now)

    assert {:ok, task, :created} =
             Orchestration.submit_task(
               task_attrs(required_capabilities: %{}),
               "offline-claim-task",
               now: @now
             )

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.runtime_id == runtime.id

    runtime
    |> Ecto.Changeset.change(status: "offline")
    |> Repo.update!()

    assert {:error, :ownership_lost} =
             Orchestration.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: Ecto.UUID.generate()
               },
               now: @now
             )

    assert {:ok, %{state: "assigned"}} = Orchestration.fetch_task(task.id)

    assert {:ok, %{state: "assigned", claim_id: nil, lease_token: nil}} =
             Orchestration.fetch_run(run.id)
  end

  test "required capabilities participate in submission and retry idempotency" do
    requirements = %{"provider_access" => true}
    attrs = task_attrs(required_capabilities: requirements)

    assert {:ok, task, :created} =
             Orchestration.submit_task(attrs, "retry-provider-task", now: @now)

    assert {:error, :idempotency_conflict} =
             Orchestration.submit_task(
               task_attrs(required_capabilities: %{}),
               "retry-provider-task",
               now: @now
             )

    assert {:ok, %{state: "cancelled"}, _command} =
             Orchestration.request_cancel(task.id, now: @now)

    assert {:ok, retried, _command, :created} =
             Orchestration.retry_task(task.id, attrs, "retry-provider-action", now: @now)

    assert retried.required_capabilities == requirements

    assert {:error, :idempotency_conflict} =
             Orchestration.retry_task(
               task.id,
               task_attrs(required_capabilities: %{}),
               "retry-provider-action",
               now: @now
             )
  end

  defp project_fixture do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: "Capability scheduling",
               key: "CAP",
               default_agent_profile: "codex",
               default_workspace: "primary"
             })

    project
  end

  defp work_item_fixture(project, title, overrides \\ %{}) do
    attrs =
      Map.merge(
        %{
          title: title,
          status: "ready",
          priority: "medium",
          assignee_type: "agent",
          agent_profile: "codex"
        },
        overrides
      )

    assert {:ok, work_item} = Workspaces.create_work_item(project.id, attrs)
    work_item
  end

  defp enroll_machine(name) do
    key = Ecto.UUID.generate()

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(
               %{name: name, machine_token: "token-#{key}"},
               key,
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret",
               now: @now
             )

    enrolled
  end

  defp register_runtime(machine, runtime_key, capabilities, now, opts \\ []) do
    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               Keyword.get_lazy(opts, :daemon_instance_id, &Ecto.UUID.generate/0),
               [
                 %{
                   runtime_key: runtime_key,
                   name: runtime_key,
                   capacity: 1,
                   agent_profile: Keyword.get(opts, :agent_profile, "codex"),
                   workspace: Keyword.get(opts, :workspace, "primary"),
                   capabilities: capabilities,
                   heartbeat_interval_ms: 60_000
                 }
               ],
               now: now
             )

    runtime
  end

  defp task_attrs(overrides) do
    Map.merge(
      %{goal: "Use provider", agent_profile: "codex", workspace: "primary", input: %{}},
      Map.new(overrides)
    )
  end
end
