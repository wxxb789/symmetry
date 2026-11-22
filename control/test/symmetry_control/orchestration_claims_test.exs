defmodule SymmetryControl.OrchestrationClaimsTest do
  use SymmetryControl.OrchestrationCase, async: false

  alias SymmetryControl.Orchestration

  @now ~U[2026-09-02 00:00:00.000000Z]

  test "claim replays one claim id and rejects a different claimant" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    {:ok, _task, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    claim_id = "00000000-0000-0000-0000-000000000011"

    request = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: claim_id
    }

    assert {:ok, first} = Orchestration.claim(run.id, request, now: @now)
    assert {:ok, replayed} = Orchestration.claim(run.id, request, now: @now)
    assert replayed.lease_token == first.lease_token

    assert {:error, :ownership_lost} =
             Orchestration.claim(run.id, request, now: DateTime.add(@now, 31, :second))

    assert {:error, :ownership_lost} =
             Orchestration.claim(
               run.id,
               %{request | claim_id: "00000000-0000-0000-0000-000000000012"},
               now: @now
             )
  end

  test "claim and renewal use configured lease duration unless explicitly overridden" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now, capacity: 2)

    {:ok, _task, :created} =
      Orchestration.submit_task(task_attrs(), "configured-lease", now: @now)

    {:ok, run} = Orchestration.assign_one(now: @now)

    request = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: "00000000-0000-0000-0000-000000000013"
    }

    assert {:ok, claimed} = Orchestration.claim(run.id, request, now: @now)
    assert claimed.lease_expires_at == DateTime.add(@now, 30, :second)

    fence = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: claimed.claim_id,
      lease_token: claimed.lease_token
    }

    renewed_at = DateTime.add(@now, 1, :second)
    assert {:ok, renewed} = Orchestration.renew_lease(run.id, fence, now: renewed_at)
    assert renewed.lease_expires_at == DateTime.add(renewed_at, 30, :second)

    assert {:ok, overridden} =
             Orchestration.renew_lease(run.id, fence,
               now: DateTime.add(@now, 2, :second),
               lease_duration_ms: 45_000
             )

    assert overridden.lease_expires_at == DateTime.add(@now, 47, :second)

    {:ok, _task, :created} =
      Orchestration.submit_task(task_attrs(goal: "explicit-claim-lease"), "explicit-claim-lease",
        now: @now
      )

    {:ok, override_run} = Orchestration.assign_one(now: @now)

    override_request = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: override_run.generation,
      claim_id: "00000000-0000-0000-0000-000000000014"
    }

    for invalid_duration <- [0, -1, 29_999] do
      assert {:error, :invalid_request} =
               Orchestration.claim(override_run.id, override_request,
                 now: @now,
                 lease_duration_ms: invalid_duration
               )
    end

    assert {:ok, explicitly_claimed} =
             Orchestration.claim(override_run.id, override_request,
               now: @now,
               lease_duration_ms: 30_000
             )

    assert explicitly_claimed.lease_expires_at == DateTime.add(@now, 30, :second)

    override_fence = Map.put(override_request, :lease_token, explicitly_claimed.lease_token)

    for invalid_duration <- [0, -1, 29_999] do
      assert {:error, :invalid_request} =
               Orchestration.renew_lease(override_run.id, override_fence,
                 now: @now,
                 lease_duration_ms: invalid_duration
               )
    end
  end

  test "fences reject stale epoch, generation, and lease" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    {:ok, _task, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime, @now)

    assert {:error, :ownership_lost} =
             Orchestration.renew_lease(run.id, %{fence | runtime_epoch: fence.runtime_epoch + 1},
               now: @now
             )

    assert {:error, :ownership_lost} =
             Orchestration.renew_lease(run.id, %{fence | generation: fence.generation + 1},
               now: @now
             )

    assert {:error, :ownership_lost} =
             Orchestration.renew_lease(
               run.id,
               %{fence | lease_token: "00000000-0000-0000-0000-000000000099"},
               now: @now
             )
  end

  defp enroll_machine do
    key = Ecto.UUID.generate()
    token = "machine-token-#{key}"

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(%{name: "builder", machine_token: token}, key,
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret"
             )

    enrolled
  end

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.{Goal, HarnessSession}
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Run, Runtime, Task}
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.{Project, ProjectResource, WorkItem}

  @now ~U[2026-09-09 09:00:00.000000Z]

  test "legacy claim rejects a manually assigned unverified OMP or OmO runtime without issuing a lease" do
    for kind <- ["omp", "omo"] do
      runtime = register_unverified_omp_runtime("legacy-#{kind}-claim", kind)

      assert {:ok, task, :created} =
               Orchestration.submit_task(legacy_task_attrs(), Ecto.UUID.generate(), now: @now)

      assert task.goal_id == nil
      assert task.required_capabilities == %{}
      assert task.input == %{}
      assert task.agent_profile == runtime.agent_profile
      assert task.workspace == runtime.workspace
      run = insert_assigned_run!(task, runtime)

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

      assert %{state: "assigned", current_generation: 1} = Repo.get!(Task, task.id)

      assert %{
               state: "assigned",
               claimed_runtime_epoch: nil,
               claim_id: nil,
               lease_token: nil,
               claimed_at: nil,
               lease_expires_at: nil
             } = Repo.get!(Run, run.id)
    end
  end

  test "assignment and exact claim replay use the frozen Task runtime snapshot" do
    allowed = enroll_and_register_runtime("snapshot-allowed")
    disallowed = enroll_and_register_runtime("snapshot-disallowed")

    {task, goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        execution_policy: Map.put(execution_policy(2), "allowed_runtime_ids", [allowed.id])
      )

    repository_resource_id = Repo.get!(WorkItem, task.work_item_id).repository_resource_id

    Repo.update_all(
      from(runtime in Runtime, where: runtime.id in ^[allowed.id, disallowed.id]),
      set: [repository_resource_id: repository_resource_id]
    )

    update_live_goal_runtime_policy!(goal_id, [disallowed.id])

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.runtime_id == allowed.id

    request = %{
      runtime_id: allowed.id,
      runtime_epoch: allowed.connection_epoch,
      generation: run.generation,
      claim_id: Ecto.UUID.generate()
    }

    assert {:ok, claimed} = Goals.claim(run.id, request, now: @now)
    assert {:ok, replayed} = Goals.claim(run.id, request, now: @now)
    assert replayed.lease_token == claimed.lease_token
  end

  test "a new claim rejects an assigned runtime outside the Task snapshot" do
    allowed = enroll_and_register_runtime("snapshot-new-claim-allowed")
    disallowed = enroll_and_register_runtime("snapshot-new-claim-disallowed")

    {task, goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        execution_policy: Map.put(execution_policy(2), "allowed_runtime_ids", [allowed.id])
      )

    repository_resource_id = Repo.get!(WorkItem, task.work_item_id).repository_resource_id

    Repo.update_all(
      from(runtime in Runtime, where: runtime.id in ^[allowed.id, disallowed.id]),
      set: [repository_resource_id: repository_resource_id]
    )

    update_live_goal_runtime_policy!(goal_id, [disallowed.id])
    run = insert_assigned_run!(task, disallowed)

    assert {:error, :ownership_lost} =
             Goals.claim(
               run.id,
               %{
                 runtime_id: disallowed.id,
                 runtime_epoch: disallowed.connection_epoch,
                 generation: run.generation,
                 claim_id: Ecto.UUID.generate()
               },
               now: @now
             )
  end

  test "strict-budget Goal assignment and new claim require native hard cost limit support" do
    {task, _goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        execution_policy:
          execution_policy(2)
          |> Map.put("budget_mode", "strict")
          |> Map.put("per_run_cost_limit_microusd", 1)
          |> Map.put("hard_cost_limit_required", true)
      )

    _without_hard_limit = enroll_and_register_runtime("strict-budget-without-hard-limit")
    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)

    hard_limit_runtime =
      enroll_and_register_runtime("strict-budget-with-hard-limit", hard_cost_limit?: true)

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == task.id
    assert run.runtime_id == hard_limit_runtime.id

    Repo.update_all(
      from(runtime in Runtime, where: runtime.id == ^hard_limit_runtime.id),
      set: [
        capabilities:
          put_in(
            hard_limit_runtime.capabilities,
            ["adapter", "operations", "hard_cost_limit"],
            false
          )
      ]
    )

    assert {:error, :ownership_lost} =
             Goals.claim(
               run.id,
               %{
                 runtime_id: hard_limit_runtime.id,
                 runtime_epoch: hard_limit_runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: Ecto.UUID.generate()
               },
               now: @now
             )

    Repo.update_all(
      from(runtime in Runtime, where: runtime.id == ^hard_limit_runtime.id),
      set: [capabilities: hard_limit_runtime.capabilities]
    )

    claim_request = %{
      runtime_id: hard_limit_runtime.id,
      runtime_epoch: hard_limit_runtime.connection_epoch,
      generation: run.generation,
      claim_id: Ecto.UUID.generate()
    }

    assert {:ok, claimed} = Goals.claim(run.id, claim_request, now: @now)

    Repo.update_all(
      from(runtime in Runtime, where: runtime.id == ^hard_limit_runtime.id),
      set: [
        capabilities:
          put_in(
            hard_limit_runtime.capabilities,
            ["adapter", "operations", "hard_cost_limit"],
            false
          )
      ]
    )

    assert {:ok, replayed} = Goals.claim(run.id, claim_request, now: @now)
    assert replayed.lease_token == claimed.lease_token
  end

  test "validation binding mismatch blocks a new claim but preserves an exact claim replay" do
    {project, repository} = project_repository_fixture("validation-claim")

    assigned_runtime =
      enroll_and_register_runtime("validation-claim-assigned",
        repository_resource_id: repository.id
      )

    rejected_runtime =
      enroll_and_register_runtime("validation-claim-rejected",
        repository_resource_id: repository.id
      )

    {assigned_task, _goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        purpose: "validate",
        project_id: project.id,
        repository_id: repository.id,
        validation_bindings: [validation_binding("checks", "check", [rejected_runtime.id])]
      )

    assigned_run = insert_assigned_run!(assigned_task, assigned_runtime)

    assert {:error, :ownership_lost} =
             Goals.claim(
               assigned_run.id,
               %{
                 runtime_id: assigned_runtime.id,
                 runtime_epoch: assigned_runtime.connection_epoch,
                 generation: assigned_run.generation,
                 claim_id: Ecto.UUID.generate()
               },
               now: @now
             )

    replay_runtime =
      enroll_and_register_runtime("validation-claim-replay",
        repository_resource_id: repository.id
      )

    {_replay_task, _replay_goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        purpose: "validate",
        project_id: project.id,
        repository_id: repository.id,
        validation_bindings: [validation_binding("checks", "check", [replay_runtime.id])]
      )

    assert {:ok, replay_run} = Orchestration.assign_one(now: DateTime.add(@now, 1, :second))

    replay_request = %{
      runtime_id: replay_runtime.id,
      runtime_epoch: replay_runtime.connection_epoch,
      generation: replay_run.generation,
      claim_id: Ecto.UUID.generate()
    }

    assert {:ok, claimed} =
             Goals.claim(replay_run.id, replay_request, now: DateTime.add(@now, 1, :second))

    assert {:ok, replayed} =
             Goals.claim(replay_run.id, replay_request, now: DateTime.add(@now, 1, :second))

    assert replayed.lease_token == claimed.lease_token
  end

  test "direct Goal claim rejects an archived Project" do
    {project, repository} = project_repository_fixture("archived-project-claim")

    runtime =
      enroll_and_register_runtime("archived-project-claim", repository_resource_id: repository.id)

    {task, _goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        project_id: project.id,
        repository_id: repository.id
      )

    run = insert_assigned_run!(task, runtime)

    Repo.update_all(
      from(project_row in Project, where: project_row.id == ^project.id),
      set: [status: "archived"]
    )

    assert {:error, :state_conflict} =
             Goals.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: Ecto.UUID.generate(),
                 provider_scope: nil
               },
               now: @now
             )
  end

  test "exact Goal claim replay remains allowed after its Project is archived" do
    {project, repository} = project_repository_fixture("archived-project-replay")

    runtime =
      enroll_and_register_runtime("archived-project-replay",
        repository_resource_id: repository.id
      )

    {task, _goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        project_id: project.id,
        repository_id: repository.id
      )

    run = insert_assigned_run!(task, runtime)

    claim_request = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: Ecto.UUID.generate(),
      provider_scope: nil
    }

    assert {:ok, claimed} = Goals.claim(run.id, claim_request, now: @now)

    Repo.update_all(
      from(project_row in Project, where: project_row.id == ^project.id),
      set: [status: "archived"]
    )

    assert {:ok, replayed} = Goals.claim(run.id, claim_request, now: @now)
    assert replayed.lease_token == claimed.lease_token
  end

  test "lost claim acknowledgement replays after a runtime affinity change" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("claim-affinity-replay")
    assert {:ok, run} = Orchestration.assign_one(now: @now)

    request = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: Ecto.UUID.generate()
    }

    assert {:ok, claimed} = Goals.claim(run.id, request, now: @now)
    item = Repo.get!(WorkItem, task.work_item_id)

    assert {:ok, other_repository} =
             Workspaces.create_resource(item.project_id, %{
               kind: "repository",
               name: "Replay mismatch repository #{System.unique_integer([:positive])}"
             })

    Repo.update_all(
      from(runtime_row in Runtime, where: runtime_row.id == ^runtime.id),
      set: [repository_resource_id: other_repository.id]
    )

    assert {:ok, replayed} = Goals.claim(run.id, request, now: @now)
    assert replayed.lease_token == claimed.lease_token
    assert replayed.claim_id == claimed.claim_id
  end

  test "claim fails closed when Run identity changes after its Task lock" do
    {goal_task, _goal_id} = insert_goal_task(max_run_attempts: 2)

    assert {:ok, legacy_task, :created} =
             Orchestration.submit_task(
               %{
                 goal: "Legacy claim routing race",
                 agent_profile: "codex",
                 workspace: "primary",
                 input: %{}
               },
               Ecto.UUID.generate(),
               now: @now
             )

    runtime = enroll_and_register_runtime("legacy-claim-routing-race")
    run = insert_assigned_run!(legacy_task, runtime)

    assert {:error, :ownership_lost} =
             Orchestration.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: Ecto.UUID.generate()
               },
               now: @now,
               on_claim_task_locked: fn locked_task ->
                 assert locked_task.id == legacy_task.id

                 Repo.update_all(
                   from(run_row in Run, where: run_row.id == ^run.id),
                   set: [task_id: goal_task.id]
                 )
               end
             )

    assert %{state: "assigned", claim_id: nil} = Repo.get!(Run, run.id)
  end

  test "Goal claim rejects a Run identity change after authority routing" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    {other_task, _other_goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("goal-claim-identity-race")
    run = insert_assigned_run!(task, runtime)

    assert {:error, :ownership_lost} =
             Goals.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: Ecto.UUID.generate()
               },
               now: @now,
               on_goal_claim_routed: fn ownership ->
                 assert ownership.task_id == task.id

                 Repo.update_all(
                   from(run_row in Run, where: run_row.id == ^run.id),
                   set: [task_id: other_task.id]
                 )
               end
             )

    assert %{task_id: task_id, state: "assigned", claim_id: nil} = Repo.get!(Run, run.id)
    assert task_id == task.id
  end

  test "Goal handoff claim rejects an assigned Run on a different machine without advancing authority" do
    {source_task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    source_item = Repo.get!(WorkItem, source_task.work_item_id)

    source_runtime =
      enroll_and_register_runtime("handoff-claim-source",
        handoff?: true,
        repository_resource_id: source_item.repository_resource_id
      )

    source_run = complete_source_run!(source_task, source_runtime)
    target_task = insert_handoff_successor!(source_task, source_run)

    target_runtime =
      enroll_and_register_runtime("handoff-claim-other-machine",
        handoff?: true,
        repository_resource_id: source_item.repository_resource_id
      )

    assert target_runtime.machine_id != source_runtime.machine_id
    assert target_runtime.repository_resource_id == source_runtime.repository_resource_id
    assert target_runtime.agent_profile == source_runtime.agent_profile
    assert target_runtime.workspace == source_runtime.workspace
    assert target_runtime.capabilities == source_runtime.capabilities

    target_run = insert_assigned_run!(target_task, target_runtime)
    target_task_before = Repo.get!(Task, target_task.id)
    target_run_before = Repo.get!(Run, target_run.id)

    harness_sessions_before =
      Repo.all(
        from session in HarnessSession,
          order_by: [asc: session.id],
          select: {session.id, session.state, session.active_run_id, session.binding_id}
      )

    claim_request = %{
      runtime_id: target_runtime.id,
      runtime_epoch: target_runtime.connection_epoch,
      generation: target_run.generation,
      claim_id: Ecto.UUID.generate()
    }

    assert {:error, :ownership_lost} =
             Goals.claim(target_run.id, claim_request, now: @now)

    target_task_after = Repo.get!(Task, target_task.id)
    target_run_after = Repo.get!(Run, target_run.id)

    assert target_task_after.state == target_task_before.state
    assert target_task_after.current_generation == target_task_before.current_generation
    assert target_task_after.attempt_generation == target_task_before.attempt_generation
    assert target_run_after.state == target_run_before.state
    assert target_run_after.generation == target_run_before.generation
    assert target_run_after.claimed_runtime_epoch == target_run_before.claimed_runtime_epoch
    assert target_run_after.claim_id == target_run_before.claim_id
    assert target_run_after.lease_token == target_run_before.lease_token
    assert target_run_after.claimed_at == target_run_before.claimed_at
    assert target_run_after.lease_expires_at == target_run_before.lease_expires_at
    assert is_nil(target_run_after.harness_session_id)
    assert is_nil(target_run_after.harness_binding_id)

    assert harness_sessions_before ==
             Repo.all(
               from session in HarnessSession,
                 order_by: [asc: session.id],
                 select: {session.id, session.state, session.active_run_id, session.binding_id}
             )
  end

  test "Goal machine retries an exact committed claim after pause but rejects a new claimant" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("lost-ack-claim")
    assert {:ok, run} = Orchestration.assign_one(now: @now)

    claim_request = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: Ecto.UUID.generate()
    }

    assert {:ok, claimed} = Goals.claim(run.id, claim_request, now: @now)
    pause_goal!(goal_id)

    assert {:ok, replayed_claim} = Goals.claim(run.id, claim_request, now: @now)
    assert replayed_claim.lease_token == claimed.lease_token

    assert {:error, :ownership_lost} =
             Goals.claim(run.id, %{claim_request | claim_id: Ecto.UUID.generate()}, now: @now)

    assert task.id == run.task_id
  end

  defp insert_goal_task(opts) do
    max_run_attempts = Keyword.fetch!(opts, :max_run_attempts)
    purpose = Keyword.get(opts, :purpose, "implement")
    planning? = purpose == "plan"
    project_id = Keyword.get(opts, :project_id) || Ecto.UUID.generate()
    create_project? = is_nil(Keyword.get(opts, :project_id))
    goal_id = Ecto.UUID.generate()
    work_item_id = if planning?, do: nil, else: Ecto.UUID.generate()
    snapshot_id = Ecto.UUID.generate()
    task_id = Ecto.UUID.generate()
    admission_key = Ecto.UUID.generate()
    reservation_id = Ecto.UUID.generate()
    repository_id = Keyword.get(opts, :repository_id, Ecto.UUID.generate())
    validation_of_task_id = if purpose == "validate", do: Ecto.UUID.generate(), else: nil
    validation_bindings = Keyword.get(opts, :validation_bindings, [])
    policy = Keyword.get(opts, :execution_policy, execution_policy(max_run_attempts))
    allowed_runtime_ids = Map.get(policy, "allowed_runtime_ids", [])
    strict_budget? = Map.get(policy, "budget_mode", "soft") == "strict"
    retained_runtime = Keyword.get(opts, :retained_runtime)

    requested_session_id =
      Keyword.get(opts, :requested_session_id) || if(retained_runtime, do: Ecto.UUID.generate())

    task_inserted_at = Keyword.get(opts, :inserted_at, @now)

    task_input =
      %{
        "subject" => %{
          "resource_id" => repository_id,
          "commit" => String.duplicate("a", 40),
          "tree_digest" => "sha256:" <> String.duplicate("b", 64)
        }
      }
      |> then(fn input ->
        if requested_session_id do
          input
          |> Map.put("session_mode", "resume")
          |> Map.put("requested_session_id", requested_session_id)
        else
          input
        end
      end)

    task_input =
      Map.put(task_input, "limits", %{
        "max_turns" => 1,
        "deadline_at" => DateTime.to_iso8601(DateTime.add(@now, 15 * 60, :second)),
        "max_cost_microusd" =>
          if(strict_budget?,
            do: Integer.to_string(Map.fetch!(policy, "per_run_cost_limit_microusd"))
          )
      })

    assert {:ok, {task, goal_id}} =
             Repo.transaction(fn ->
               if create_project? do
                 Repo.query!(
                   """
                   INSERT INTO projects (
                     id, name, key, status, default_agent_profile, default_workspace, inserted_at, updated_at
                   ) VALUES ($1, $2, $3, 'active', 'codex', 'primary', $4, $4)
                   """,
                   [db_uuid(project_id), "Goal admission project", project_key(), @now]
                 )
               end

               unless Repo.exists?(
                        from(resource in ProjectResource, where: resource.id == ^repository_id)
                      ) do
                 Repo.query!(
                   """
                   INSERT INTO project_resources (
                     id, project_id, kind, name, status, sync_status, metadata, lock_version, inserted_at, updated_at
                   ) VALUES ($1, $2, 'repository', $3, 'unknown', 'unknown', '{}'::jsonb, 1, $4, $4)
                   """,
                   [
                     db_uuid(repository_id),
                     db_uuid(project_id),
                     "Goal admission repository #{System.unique_integer([:positive])}",
                     @now
                   ]
                 )
               end

               Repo.query!(
                 """
                 INSERT INTO goals (
                  id, project_id, title, state, current_revision, event_sequence, inserted_at, updated_at
                 ) VALUES ($1, $2, 'Goal admission', $3, 1, 0, $4, $4)
                 """,
                 [
                   db_uuid(goal_id),
                   db_uuid(project_id),
                   if(planning?, do: "draft", else: "active"),
                   @now
                 ]
               )

               Repo.query!(
                 """
                 INSERT INTO goal_revisions (
                   goal_id, revision, objective, non_goals, acceptance_contract, authority_policy,
                   execution_policy, context_manifest, reason, actor_ref, inserted_at
                 ) VALUES ($1, 1, 'Exercise orchestration admission', '[]'::jsonb, '{}'::jsonb,
                   '{}'::jsonb, $2::text::jsonb, '{}'::jsonb, 'initial', 'operator:test', $3)
                 """,
                 [db_uuid(goal_id), Jason.encode!(policy), @now]
               )

               unless planning? do
                 Repo.query!(
                   """
                   INSERT INTO work_items (
                      id, project_id, title, status, priority, position, assignee_type, blocked,
                       goal_id, admitted_revision, acceptance_contract, integration, repository_resource_id,
                       baseline_subject, inserted_at, updated_at
                     ) VALUES ($1, $2, 'Goal admission work item', 'ready', 'no_priority', 0, 'unassigned',
                       FALSE, $3, 1, '{}'::jsonb, TRUE, $4, $5::text::jsonb, $6, $6)
                   """,
                   [
                     db_uuid(work_item_id),
                     db_uuid(project_id),
                     db_uuid(goal_id),
                     db_uuid(repository_id),
                     Jason.encode!(%{
                       "resource_id" => repository_id,
                       "commit" => String.duplicate("a", 40),
                       "tree_digest" => "sha256:" <> String.duplicate("b", 64)
                     }),
                     @now
                   ]
                 )
               end

               if retained_runtime do
                 Repo.update_all(
                   from(runtime_row in Runtime, where: runtime_row.id == ^retained_runtime.id),
                   set: [repository_resource_id: repository_id]
                 )

                 %HarnessSession{id: requested_session_id}
                 |> HarnessSession.changeset(%{
                   machine_id: retained_runtime.machine_id,
                   runtime_id: retained_runtime.id,
                   repository_resource_id: repository_id,
                   harness_kind: retained_runtime.harness_kind,
                   harness_version: retained_runtime.harness_version,
                   adapter_version: retained_runtime.adapter_version,
                   local_handle_id: Ecto.UUID.generate(),
                   binding_id: Ecto.UUID.generate(),
                   binding_verified: Keyword.get(opts, :retained_session_binding_verified, true),
                   workspace_fingerprint: "workspace:#{retained_runtime.id}",
                   state: Keyword.get(opts, :retained_session_state, "available")
                 })
                 |> Repo.insert!()
               end

               Repo.query!(
                 """
                 INSERT INTO context_snapshots (
                   id, goal_id, goal_revision, work_item_id, schema_version, content_hash, payload, inserted_at
                 ) VALUES ($1, $2, 1, $3, 1, $4, $5::text::jsonb, $6)
                 """,
                 [
                   db_uuid(snapshot_id),
                   db_uuid(goal_id),
                   maybe_db_uuid(work_item_id),
                   :crypto.hash(:sha256, "goal-context"),
                   Jason.encode!(%{
                     "work_contract" => %{
                       "purpose" => purpose,
                       "validation_bindings" => validation_bindings
                     }
                   }),
                   @now
                 ]
               )

               if purpose == "validate" do
                 Repo.query!(
                   """
                   INSERT INTO tasks (
                     id, idempotency_key, request_hash, request_hash_version, goal, agent_profile, workspace,
                     input, required_capabilities, state, current_generation, attempt_generation, work_item_id,
                     goal_id, goal_revision, context_snapshot_id, purpose, validation_of_task_id, admission_key,
                     allowed_runtime_ids, max_run_attempts,
                     inserted_at, updated_at
                   ) VALUES ($1, $2, $3, 1, 'Goal admission producer', 'codex', 'primary', '{}'::jsonb,
                     '{}'::jsonb, 'completed', 1, 1, $4, $5, 1, $6, 'implement', NULL, $7,
                     ARRAY(SELECT value::uuid FROM jsonb_array_elements_text($8::text::jsonb) AS value), $9, $10, $10)
                   """,
                   [
                     db_uuid(validation_of_task_id),
                     "goal-admission-producer-#{System.unique_integer([:positive])}",
                     :crypto.hash(:sha256, "goal-admission-producer"),
                     maybe_db_uuid(work_item_id),
                     db_uuid(goal_id),
                     db_uuid(snapshot_id),
                     db_uuid(Ecto.UUID.generate()),
                     Jason.encode!(allowed_runtime_ids),
                     max_run_attempts,
                     @now
                   ]
                 )
               end

               Repo.query!(
                 """
                 INSERT INTO tasks (
                 id, idempotency_key, request_hash, request_hash_version, goal, agent_profile, workspace,
                 input, required_capabilities, state, current_generation, attempt_generation, work_item_id,
                 goal_id, goal_revision, context_snapshot_id, purpose, validation_of_task_id, admission_key,
                 allowed_runtime_ids, max_run_attempts, requested_session_id, inserted_at, updated_at
                 ) VALUES ($1, $2, $3, 1, 'Goal admission task', 'codex', 'primary', $4::text::jsonb,
                 '{}'::jsonb, 'queued', 0, 1, $5, $6, 1, $7, $8, $9, $10,
                 ARRAY(SELECT value::uuid FROM jsonb_array_elements_text($11::text::jsonb) AS value),
                 $12, $13, $14, $14)
                 """,
                 [
                   db_uuid(task_id),
                   "goal-admission-task-#{System.unique_integer([:positive])}",
                   :crypto.hash(:sha256, "goal-admission-task"),
                   Jason.encode!(task_input),
                   maybe_db_uuid(work_item_id),
                   db_uuid(goal_id),
                   db_uuid(snapshot_id),
                   purpose,
                   maybe_db_uuid(validation_of_task_id),
                   db_uuid(admission_key),
                   Jason.encode!(allowed_runtime_ids),
                   max_run_attempts,
                   maybe_db_uuid(requested_session_id),
                   task_inserted_at
                 ]
               )

               Repo.query!(
                 """
                 INSERT INTO goal_budget_reservations (
                   id, goal_id, goal_revision, task_id, admission_key, reserved_microusd, state,
                   lock_version, inserted_at, updated_at
                 ) VALUES ($1, $2, 1, $3, $4, 1, 'held', 1, $5, $5)
                 """,
                 [
                   db_uuid(reservation_id),
                   db_uuid(goal_id),
                   db_uuid(task_id),
                   db_uuid(admission_key),
                   @now
                 ]
               )

               {Repo.get!(Task, task_id), goal_id}
             end)

    {task, goal_id}
  end

  defp db_uuid(uuid), do: Ecto.UUID.dump!(uuid)

  defp maybe_db_uuid(nil), do: nil

  defp maybe_db_uuid(uuid), do: db_uuid(uuid)

  defp validation_binding(profile_name, kind, allowed_runtime_ids) do
    %{
      "profile_name" => profile_name,
      "kind" => kind,
      "profile_digest" => "sha256:" <> String.duplicate("a", 64),
      "allowed_runtime_ids" => allowed_runtime_ids
    }
  end

  defp project_repository_fixture(label) do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: "Goal admission #{label}",
               key: project_key()
             })

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{
               kind: "repository",
               name: "Goal admission repository #{label}"
             })

    {project, repository}
  end

  defp enroll_machine(label) do
    token = "goal-admission-#{label}-#{System.unique_integer([:positive])}"

    assert {:ok, %{machine: machine}, :created} =
             Orchestration.enroll_machine(
               %{name: "Goal admission #{label}", machine_token: token},
               Ecto.UUID.generate(),
               enrollment_token: "test-enrollment-token",
               expected_enrollment_token: "test-enrollment-token",
               now: @now
             )

    machine
  end

  defp runtime_spec(runtime_key, overrides) do
    Map.merge(
      %{
        runtime_key: runtime_key,
        name: runtime_key,
        capacity: 1,
        agent_profile: "codex",
        workspace: "primary",
        capabilities: %{}
      },
      overrides
    )
  end

  defp native_adapter(kind, native_version, implementation_version, protocol_version) do
    %{
      "kind" => kind,
      "native_version" => native_version,
      "implementation_version" => implementation_version,
      "protocol_version" => protocol_version,
      "operations" => %{
        "start" => true,
        "events" => true,
        "cancel" => true,
        "resume" => false,
        "handoff" => false,
        "guidance" => "next_turn",
        "pause" => "unsupported",
        "approval_response" => false,
        "usage" => "reported",
        "hard_cost_limit" => false
      }
    }
  end

  defp register_unverified_omp_runtime(label, kind) do
    machine = enroll_machine(label)

    {native_version, implementation_version, protocol_version} =
      case kind do
        "omo" -> {"5.1.69", "omo-rpc-v1", 1}
        "omp" -> {"18.20.0", "omp-rpc-v2", 2}
      end

    adapter =
      native_adapter(kind, native_version, implementation_version, protocol_version)
      |> Map.put("operations", %{
        "start" => false,
        "events" => false,
        "cancel" => false,
        "resume" => false,
        "handoff" => false,
        "guidance" => "unsupported",
        "pause" => "unsupported",
        "approval_response" => false,
        "usage" => "unknown",
        "hard_cost_limit" => false
      })

    specification =
      runtime_spec(label, %{
        harness_kind: kind,
        harness_version: native_version,
        adapter_version: implementation_version,
        adapter_protocol_version: protocol_version,
        capabilities: %{
          "structured_input" => true,
          "provider_access" => false,
          "interactive" => true,
          "supervisory_control" => false,
          "adapter" => adapter
        }
      })

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(machine.id, Ecto.UUID.generate(), [specification],
               now: @now
             )

    runtime
  end

  defp execution_policy(max_run_attempts) do
    %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "max_run_attempts_per_task" => max_run_attempts,
      "budget_limit_microusd" => 1_000_000,
      "per_run_cost_limit_microusd" => nil,
      "budget_mode" => "soft",
      "hard_cost_limit_required" => false,
      "allowed_runtime_ids" => [],
      "allowed_model_profiles" => ["codex"],
      "final_acceptance" => "operator",
      "allowed_actions" => [],
      "allowed_resource_ids" => []
    }
  end

  defp update_live_goal_runtime_policy!(goal_id, runtime_ids) do
    Repo.query!("ALTER TABLE goal_revisions DISABLE TRIGGER USER")

    try do
      Repo.query!(
        "UPDATE goal_revisions SET execution_policy = $1::text::jsonb WHERE goal_id = $2",
        [
          Jason.encode!(Map.put(execution_policy(2), "allowed_runtime_ids", runtime_ids)),
          db_uuid(goal_id)
        ]
      )
    after
      Repo.query!("ALTER TABLE goal_revisions ENABLE TRIGGER USER")
    end
  end

  defp enroll_and_register_runtime(label, opts \\ []) do
    machine_token = "goal-admission-machine-#{label}-#{System.unique_integer([:positive])}"

    machine = enroll_machine(label, machine_token)
    register_runtime_on_machine(label, machine.id, opts)
  end

  defp register_runtime_on_machine(label, machine_id, opts) do
    resume? = Keyword.get(opts, :resume?, false)
    handoff? = Keyword.get(opts, :handoff?, false)
    hard_cost_limit? = Keyword.get(opts, :hard_cost_limit?, false)
    capacity = Keyword.get(opts, :capacity, 1)
    harness_kind = Keyword.get(opts, :harness_kind, "codex")
    harness_version = Keyword.get(opts, :harness_version, "0.153.4")
    adapter_version = Keyword.get(opts, :adapter_version, "symmetry-codex-1")

    repository_resource_id =
      Keyword.get_lazy(opts, :repository_resource_id, fn ->
        Repo.one(
          from item in WorkItem,
            where: not is_nil(item.goal_id) and not is_nil(item.repository_resource_id),
            order_by: [desc: item.inserted_at, desc: item.id],
            limit: 1,
            select: item.repository_resource_id
        )
      end)

    adapter =
      native_adapter(harness_kind, harness_version, adapter_version, 1)
      |> put_in(["operations", "resume"], resume?)
      |> put_in(["operations", "handoff"], handoff?)
      |> put_in(["operations", "hard_cost_limit"], hard_cost_limit?)

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine_id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: "goal-admission-#{label}",
                   name: "Goal admission #{label}",
                   capacity: capacity,
                   agent_profile: "codex",
                   workspace: "primary",
                   harness_kind: harness_kind,
                   harness_version: harness_version,
                   adapter_version: adapter_version,
                   adapter_protocol_version: 1,
                   capabilities: %{
                     "structured_input" => true,
                     "interactive" => true,
                     "adapter" => adapter
                   }
                 }
                 |> then(fn specification ->
                   if is_nil(repository_resource_id),
                     do: specification,
                     else: Map.put(specification, :repository_resource_id, repository_resource_id)
                 end)
               ],
               now: @now
             )

    runtime
  end

  defp enroll_machine(label, machine_token) do
    assert {:ok, %{machine: machine}, :created} =
             Orchestration.enroll_machine(
               %{name: "Goal admission #{label}", machine_token: machine_token},
               Ecto.UUID.generate(),
               enrollment_token: "test-enrollment-token",
               expected_enrollment_token: "test-enrollment-token",
               now: @now
             )

    machine
  end

  defp complete_source_run!(task, runtime) do
    Repo.update_all(
      from(task_row in Task, where: task_row.id == ^task.id),
      set: [state: "completed", current_generation: 1]
    )

    %Run{}
    |> Run.changeset(%{
      task_id: task.id,
      runtime_id: runtime.id,
      generation: 1,
      state: "completed",
      assigned_at: @now,
      assignment_expires_at: DateTime.add(@now, 60, :second)
    })
    |> Repo.insert!()
  end

  defp insert_assigned_run!(task, runtime) do
    Repo.update_all(
      from(task_row in Task, where: task_row.id == ^task.id),
      set: [state: "assigned", current_generation: task.attempt_generation]
    )

    %Run{}
    |> Run.changeset(%{
      task_id: task.id,
      runtime_id: runtime.id,
      generation: task.attempt_generation,
      state: "assigned",
      assigned_at: @now,
      assignment_expires_at: DateTime.add(@now, 60, :second)
    })
    |> Repo.insert!()
  end

  defp insert_handoff_successor!(source_task, source_run) do
    source_task = Repo.get!(Task, source_task.id)

    input =
      source_task.input
      |> Map.put("session_mode", "handoff")
      |> Map.put("requested_session_id", nil)
      |> Map.put("handoff_source_run_id", source_run.id)

    %Task{}
    |> Task.changeset(%{
      idempotency_key: "handoff-successor:#{Ecto.UUID.generate()}",
      request_hash: :crypto.hash(:sha256, Ecto.UUID.generate()),
      request_hash_version: 2,
      goal: source_task.goal,
      agent_profile: source_task.agent_profile,
      workspace: source_task.workspace,
      input: input,
      required_capabilities: source_task.required_capabilities,
      state: "queued",
      current_generation: 0,
      attempt_generation: 1,
      work_item_id: source_task.work_item_id,
      goal_id: source_task.goal_id,
      goal_revision: source_task.goal_revision,
      context_snapshot_id: source_task.context_snapshot_id,
      purpose: source_task.purpose,
      validation_of_task_id: source_task.validation_of_task_id,
      admission_key: Ecto.UUID.generate(),
      allowed_runtime_ids: source_task.allowed_runtime_ids,
      max_run_attempts: source_task.max_run_attempts,
      requested_session_id: nil,
      handoff_source_run_id: source_run.id
    })
    |> Ecto.Changeset.change(inserted_at: @now, updated_at: @now)
    |> Repo.insert!()
  end

  defp pause_goal!(goal_id) do
    goal = Repo.get!(Goal, goal_id)

    assert {:ok, %{goal: %{state: "paused"}}, :created} =
             Goals.command(
               goal_id,
               %{
                 schema_version: "symmetry.goal_command.v1",
                 mutation_id: Ecto.UUID.generate(),
                 expected_version: goal.operator_version,
                 expected_revision: goal.current_revision,
                 kind: "pause",
                 payload: %{reason: "Stop new execution before control delivery."}
               },
               "operator:test",
               now: @now
             )
  end

  defp legacy_task_attrs do
    %{goal: "Retry legacy body", agent_profile: "codex", workspace: "primary", input: %{}}
  end

  defp project_key do
    "P" <> Integer.to_string(rem(System.unique_integer([:positive]), 9_999_999))
  end
end
