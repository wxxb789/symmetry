defmodule SymmetryControl.OrchestrationGoalAssignmentTest do
  use SymmetryControl.OrchestrationCase, async: false

  alias Oban.Job
  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.ReadModel
  alias SymmetryControl.Goals.{Goal, GoalBudgetReservation, GoalEvent, HarnessSession}
  alias SymmetryControl.Goals.Workers.SettleTaskWorker
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Command, Run, Runtime, Task}
  alias SymmetryControl.Workspaces.{ProjectResource, WorkItem}

  @now ~U[2026-09-09 09:00:00.000000Z]

  test "OMP resume assignment requires the exact retained kind and versions" do
    retained_runtime =
      enroll_and_register_runtime("omp-retained",
        harness_kind: "omp",
        harness_version: "18.20.0",
        adapter_version: "symmetry-omp-1",
        resume?: true
      )

    {task, _goal_id} = insert_goal_task(max_run_attempts: 2, retained_runtime: retained_runtime)
    retained_runtime = Repo.get!(Runtime, retained_runtime.id)
    retained_session = Repo.get!(HarnessSession, task.requested_session_id)
    original_binding_id = retained_session.binding_id

    retained_identity = [
      harness_kind: retained_session.harness_kind,
      harness_version: retained_session.harness_version,
      adapter_version: retained_session.adapter_version
    ]

    for changed <- [
          [harness_kind: "pi"],
          [harness_version: "18.20.1"],
          [adapter_version: "symmetry-omp-2"]
        ] do
      Repo.update_all(from(session in HarnessSession, where: session.id == ^retained_session.id),
        set: retained_identity
      )

      Repo.update_all(from(session in HarnessSession, where: session.id == ^retained_session.id),
        set: changed
      )

      assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
      assert %{state: "queued", current_generation: 0} = Repo.get!(Task, task.id)
      assert Repo.get!(Runtime, retained_runtime.id) == retained_runtime
      assert 0 == Repo.aggregate(from(run in Run, where: run.task_id == ^task.id), :count)

      assert %{
               state: "available",
               active_run_id: nil,
               binding_id: ^original_binding_id,
               binding_verified: true
             } = Repo.get!(HarnessSession, retained_session.id)
    end

    Repo.update_all(from(session in HarnessSession, where: session.id == ^retained_session.id),
      set: retained_identity
    )

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.runtime_id == retained_runtime.id
    assert run.harness_session_id == retained_session.id
    refute run.harness_binding_id == retained_session.binding_id
    assert Repo.get!(HarnessSession, retained_session.id).binding_id == run.harness_binding_id

    fence = goal_claim(run, retained_runtime, @now)

    assert {:ok, %{session: %{id: session_id, harness_kind: "omp"}}, :created} =
             Goals.attach_harness_session(
               retained_runtime.machine_id,
               run.id,
               fence,
               %{
                 local_handle_id: retained_session.local_handle_id,
                 binding_id: run.harness_binding_id,
                 harness_kind: "omp",
                 harness_version: "18.20.0",
                 adapter_version: "symmetry-omp-1",
                 workspace_fingerprint: retained_session.workspace_fingerprint,
                 workspace: task.workspace,
                 repository_resource_id: retained_session.repository_resource_id
               },
               now: @now
             )

    assert session_id == retained_session.id
  end

  test "Goal resume with no viable retained runtime remains queued without fallback" do
    _other = enroll_and_register_runtime("resume-fallback")
    retained_runtime = enroll_and_register_runtime("resume-unavailable", resume?: true)

    {task, _goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        retained_runtime: retained_runtime,
        retained_session_state: "unavailable"
      )

    retained_session = Repo.get!(HarnessSession, task.requested_session_id)

    assert retained_session.state == "unavailable"
    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
    assert %{state: "queued", current_generation: 0} = Repo.get!(Task, task.id)
    assert 0 == Repo.aggregate(from(run in Run, where: run.task_id == ^task.id), :count)
  end

  test "Goal resume ignores a legacy session without verified binding provenance" do
    retained_runtime = enroll_and_register_runtime("resume-unverified", resume?: true)

    {task, _goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        retained_runtime: retained_runtime,
        retained_session_binding_verified: false
      )

    assert %{binding_verified: false, state: "available"} =
             Repo.get!(HarnessSession, task.requested_session_id)

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
    assert %{state: "queued", current_generation: 0} = Repo.get!(Task, task.id)
    assert 0 == Repo.aggregate(from(run in Run, where: run.task_id == ^task.id), :count)
  end

  test "offline retained runtime does not assign a resumed Goal task or fall back" do
    retained_runtime = enroll_and_register_runtime("resume-offline-retained", resume?: true)
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2, retained_runtime: retained_runtime)
    retained_session = Repo.get!(HarnessSession, task.requested_session_id)
    item = Repo.get!(WorkItem, task.work_item_id)

    fallback_runtime =
      enroll_and_register_runtime("resume-offline-fallback",
        resume?: true,
        repository_resource_id: item.repository_resource_id
      )

    original_binding_id = retained_session.binding_id

    Repo.update_all(
      from(runtime in Runtime, where: runtime.id == ^retained_runtime.id),
      set: [status: "offline"]
    )

    assert Repo.get!(Runtime, fallback_runtime.id).status == "online"
    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
    assert %{state: "queued", current_generation: 0} = Repo.get!(Task, task.id)
    assert 0 == Repo.aggregate(from(run in Run, where: run.task_id == ^task.id), :count)

    assert %{
             state: "available",
             active_run_id: nil,
             binding_id: ^original_binding_id,
             binding_verified: true
           } = Repo.get!(HarnessSession, retained_session.id)
  end

  test "Goal handoff selects a same-machine runtime with handoff support without reserving a session" do
    {source_task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    source_item = Repo.get!(WorkItem, source_task.work_item_id)

    unsupported_source_runtime =
      enroll_and_register_runtime("handoff-source",
        handoff?: false,
        repository_resource_id: source_item.repository_resource_id
      )

    source_run = complete_source_run!(source_task, unsupported_source_runtime)
    target_task = insert_handoff_successor!(source_task, source_run)

    _same_machine_without_handoff =
      register_runtime_on_machine(
        "handoff-without-support",
        unsupported_source_runtime.machine_id,
        handoff?: false,
        repository_resource_id: source_item.repository_resource_id
      )

    eligible_runtime =
      register_runtime_on_machine("handoff-eligible", unsupported_source_runtime.machine_id,
        handoff?: true,
        repository_resource_id: source_item.repository_resource_id
      )

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == target_task.id
    assert run.runtime_id == eligible_runtime.id
    assert run.harness_session_id == nil
  end

  test "Goal handoff remains queued when only a different machine advertises handoff" do
    {source_task, goal_id} = insert_goal_task(max_run_attempts: 2)
    source_item = Repo.get!(WorkItem, source_task.work_item_id)
    source_runtime = enroll_and_register_runtime("handoff-local-source", handoff?: false)
    source_run = complete_source_run!(source_task, source_runtime)
    target_task = insert_handoff_successor!(source_task, source_run)

    Repo.update_all(
      from(task in Task, where: task.id == ^target_task.id),
      set: [inserted_at: DateTime.add(@now, 1, :second)]
    )

    _different_machine = enroll_and_register_runtime("handoff-other-machine", handoff?: true)

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
    assert %{state: "queued", current_generation: 0} = Repo.get!(Task, target_task.id)
    assert 0 == Repo.aggregate(from(run in Run, where: run.task_id == ^target_task.id), :count)

    assert {:ok, queued_projection} = ReadModel.fetch(goal_id)

    queued_execution =
      queued_projection.work_items
      |> Enum.find(&(&1.id == target_task.work_item_id))
      |> Map.fetch!(:execution)

    assert queued_execution.state == "queued"
    refute queued_execution.terminal?

    assert queued_execution.pending_assignment == %{
             repository_resource_id: source_item.repository_resource_id,
             agent_profile: source_task.agent_profile,
             workspace: source_task.workspace,
             requested_session_id: nil,
             handoff_source_run_id: source_run.id,
             machine_affinity: "handoff_source_machine"
           }

    eligible_runtime =
      register_runtime_on_machine(
        "handoff-local-recovery",
        source_runtime.machine_id,
        handoff?: true,
        repository_resource_id: source_item.repository_resource_id
      )

    assert {:ok, assigned_run} = Orchestration.assign_one(now: DateTime.add(@now, 1, :second))
    assert assigned_run.task_id == target_task.id
    assert assigned_run.runtime_id == eligible_runtime.id

    assert {:ok, assigned_projection} = ReadModel.fetch(goal_id)

    assigned_execution =
      assigned_projection.work_items
      |> Enum.find(&(&1.id == target_task.work_item_id))
      |> Map.fetch!(:execution)

    assert assigned_execution.state == "assigned"
    refute assigned_execution.terminal?
    assert assigned_execution.pending_assignment == nil
  end

  test "retained reservation rotates the binding before dispatch and an old stop replay cannot release it" do
    retained_runtime =
      enroll_and_register_runtime("exclusive-retained", resume?: true, capacity: 2)

    {first_task, _first_goal_id} =
      insert_goal_task(max_run_attempts: 2, retained_runtime: retained_runtime)

    session = Repo.get!(HarnessSession, first_task.requested_session_id)
    original_binding_id = session.binding_id
    first_item = Repo.get!(WorkItem, first_task.work_item_id)

    {second_task, _second_goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        project_id: first_item.project_id,
        repository_id: first_item.repository_resource_id,
        requested_session_id: session.id,
        inserted_at: DateTime.add(@now, 1, :second)
      )

    assert {:ok, first_run} = Orchestration.assign_one(now: @now)
    assert first_run.task_id == first_task.id
    assert first_run.harness_session_id == session.id
    assert is_binary(first_run.harness_binding_id)
    refute first_run.harness_binding_id == original_binding_id

    assert %{state: "busy", active_run_id: active_run_id, binding_id: first_binding_id} =
             Repo.get!(HarnessSession, session.id)

    assert active_run_id == first_run.id
    assert first_binding_id == first_run.harness_binding_id

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
    assert %{state: "queued", current_generation: 0} = Repo.get!(Task, second_task.id)
    assert 0 == Repo.aggregate(from(run in Run, where: run.task_id == ^second_task.id), :count)

    first_fence =
      goal_claim(first_run, retained_runtime, @now)

    assert {:ok, %{session: first_attachment}, :created} =
             Goals.attach_harness_session(
               retained_runtime.machine_id,
               first_run.id,
               first_fence,
               %{
                 local_handle_id: session.local_handle_id,
                 binding_id: first_binding_id,
                 harness_kind: session.harness_kind,
                 harness_version: session.harness_version,
                 adapter_version: session.adapter_version,
                 workspace_fingerprint: session.workspace_fingerprint,
                 workspace: "primary",
                 repository_resource_id: session.repository_resource_id
               },
               now: @now
             )

    assert {:ok, %{state: "failed"}} =
             Orchestration.transition(
               first_run.id,
               first_fence,
               "failed",
               %{"reason" => "native turn stopped before the next retained reservation"},
               Ecto.UUID.generate(),
               now: @now
             )

    assert %{state: "unavailable", active_run_id: nil, binding_id: ^first_binding_id} =
             Repo.get!(HarnessSession, session.id)

    stop = %{
      session_id: session.id,
      local_handle_id: session.local_handle_id,
      binding_id: first_binding_id
    }

    assert {:ok, stop_receipt, :created} =
             Goals.mark_harness_session_stopped(
               retained_runtime.machine_id,
               first_run.id,
               first_fence,
               stop,
               now: @now
             )

    assert %{state: "available", active_run_id: nil} = Repo.get!(HarnessSession, session.id)

    assert {:ok, second_run} = Orchestration.assign_one(now: DateTime.add(@now, 1, :second))
    assert second_run.task_id == second_task.id
    refute second_run.harness_binding_id == first_binding_id

    assert %{state: "busy", active_run_id: active_run_id, binding_id: second_binding_id} =
             Repo.get!(HarnessSession, session.id)

    assert active_run_id == second_run.id
    assert second_binding_id == second_run.harness_binding_id

    assert {:ok, ^stop_receipt, :replayed} =
             Goals.mark_harness_session_stopped(
               retained_runtime.machine_id,
               first_run.id,
               first_fence,
               stop,
               now: DateTime.add(@now, 1, :second)
             )

    assert %{state: "busy", active_run_id: ^active_run_id, binding_id: ^second_binding_id} =
             Repo.get!(HarnessSession, session.id)

    assert {:ok, %{session: ^first_attachment}} =
             Goals.fetch_harness_session_attachment(
               retained_runtime.machine_id,
               first_run.id,
               first_fence
             )

    assert first_attachment.binding_id == first_binding_id
    assert first_attachment.state == "busy"
  end

  test "lease expiry keeps a retained session unavailable until a fenced stop receipt arrives" do
    retained_runtime = enroll_and_register_runtime("retained-expiry", resume?: true)
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2, retained_runtime: retained_runtime)
    session = Repo.get!(HarnessSession, task.requested_session_id)
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = goal_claim(run, retained_runtime, @now)
    expired_at = DateTime.add(@now, 30, :second)

    assert %{expired_runs: 1} = Orchestration.expire(now: expired_at)
    assert %{state: "unavailable", active_run_id: nil} = Repo.get!(HarnessSession, session.id)

    assert {:error, :no_assignment} =
             Orchestration.assign_one(now: DateTime.add(expired_at, 1, :second))

    assert {:ok, %{state: "failed"}} =
             Orchestration.transition(
               run.id,
               fence,
               "failed",
               %{"reason" => "terminal receipt without durable session stop"},
               Ecto.UUID.generate(),
               now: DateTime.add(expired_at, 1, :second)
             )

    assert %{state: "unavailable", active_run_id: nil} = Repo.get!(HarnessSession, session.id)

    assert {:ok, %{"session_stopped" => %{"state" => "available"}}, :created} =
             Goals.mark_harness_session_stopped(
               retained_runtime.machine_id,
               run.id,
               fence,
               %{
                 session_id: session.id,
                 local_handle_id: session.local_handle_id,
                 binding_id: run.harness_binding_id
               },
               now: DateTime.add(expired_at, 1, :second)
             )

    assert %{state: "available", active_run_id: nil} = Repo.get!(HarnessSession, session.id)
  end

  test "Goal assignment rejects a queued task whose current attempt is already consumed" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    _runtime = enroll_and_register_runtime("goal-current-attempt")

    Repo.update_all(
      from(task_row in Task, where: task_row.id == ^task.id),
      set: [current_generation: task.attempt_generation]
    )

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
  end

  test "Goal control command rejects a superseded lifecycle action" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("stale-control")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = goal_claim(run, runtime, @now)

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(run.id, fence, "running", %{}, Ecto.UUID.generate(),
               now: @now
             )

    pause_action_id = Ecto.UUID.generate()

    insert_goal_event(goal_id, pause_action_id, 1, "pause")

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "paused", event_sequence: 1]
    )

    resume_action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, resume_action_id, 2, "resume")

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "active", event_sequence: 2]
    )

    assert {:ok, :superseded} =
             Goals.dispatch_goal_control_command(
               goal_id,
               1,
               pause_action_id,
               task.id,
               "pause",
               %{},
               "goal-control:#{pause_action_id}:pause:#{task.id}",
               now: @now
             )

    assert 0 ==
             Repo.aggregate(from(command in Command, where: command.task_id == ^task.id), :count)
  end

  test "Goal pause does not create an unimplemented native pause descriptor" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("goal-pause-no-native")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = goal_claim(run, runtime, @now)

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(run.id, fence, "running", %{}, Ecto.UUID.generate(),
               now: @now
             )

    action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, action_id, 1, "pause")

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "paused", event_sequence: 1]
    )

    assert {:ok, []} = Goals.control_dispatch_plan(goal_id, 1, action_id)
    assert {:ok, persisted_run} = Orchestration.fetch_run(run.id)
    assert persisted_run.state == "running"
    assert task.id == run.task_id
  end

  test "a planning Task cannot start after its draft Goal becomes active" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2, purpose: "plan")
    repository_resource_id = task.input["subject"]["resource_id"]

    _runtime =
      enroll_and_register_runtime("planning-task-active",
        repository_resource_id: repository_resource_id
      )

    Repo.update_all(from(goal in Goal, where: goal.id == ^goal_id), set: [state: "active"])

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
  end

  test "lease expiry consumes the admitted final run attempt and persists one settlement job" do
    {task, _goal} = insert_goal_task(max_run_attempts: 1)
    runtime = enroll_and_register_runtime("expiry")

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == task.id
    _fence = goal_claim(run, runtime, @now)

    assert %{expired_runs: 1} = Orchestration.expire(now: DateTime.add(@now, 30, :second))

    assert {:ok, exhausted} = Orchestration.fetch_task(task.id)
    assert exhausted.state == "failed"
    assert exhausted.attempt_generation == 1

    assert {:error, :no_assignment} =
             Orchestration.assign_one(now: DateTime.add(@now, 31, :second))

    assert [job] = settlement_jobs(task.id, run.id, run.generation)
    assert job.args == %{"task_id" => task.id, "run_id" => run.id, "generation" => run.generation}
    assert job.scheduled_at == DateTime.add(@now, 510, :second)

    assert {:ok, fetched_run} = Orchestration.fetch_run(run.id)
    assert fetched_run.state == "failed"
    assert fetched_run.failure["reason"] == "lease_expired"
    assert runtime.id != nil

    enable_goal_rollout()
    assert :ok = SettleTaskWorker.perform(job)

    assert "unknown" ==
             Repo.one!(
               from reservation in GoalBudgetReservation,
                 where: reservation.task_id == ^task.id,
                 select: reservation.state
             )
  end

  test "manual retry cannot create a run beyond the admitted attempt limit" do
    {task, _goal} = insert_goal_task(max_run_attempts: 1)
    runtime = enroll_and_register_runtime("retry")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = goal_claim(run, runtime, @now)

    assert {:ok, %{state: "failed"}} =
             Orchestration.transition(run.id, fence, "failed", %{"stage" => "test"}, uuid(1),
               now: @now
             )

    assert {:error, :goal_authority_required} =
             Orchestration.retry_task(
               task.id,
               legacy_task_attrs(),
               "retry-attempt-limit",
               expected_generation: 1,
               now: @now
             )

    assert {:ok, exhausted} = Orchestration.fetch_task(task.id)
    assert exhausted.state == "failed"

    assert Repo.aggregate(from(command in Command, where: command.task_id == ^task.id), :count) ==
             0

    assert settlement_jobs(task.id, run.id, 1) |> length() == 1
  end

  test "stale delivery after automatic retry cannot create another settlement job" do
    {task, _goal} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("stale")
    assert {:ok, first_run} = Orchestration.assign_one(now: @now)
    fence = goal_claim(first_run, runtime, @now)

    expiry = DateTime.add(@now, 30, :second)
    assert %{expired_runs: 1} = Orchestration.expire(now: expiry)

    assert {:ok, _snapshot} =
             Orchestration.heartbeat(runtime.id, runtime.connection_epoch, [], now: expiry)

    assert {:ok, replacement} = Orchestration.assign_one(now: expiry)
    assert replacement.generation == 2

    assert {:error, :ownership_lost} =
             Orchestration.transition(
               first_run.id,
               fence,
               "completed",
               %{"summary" => "stale"},
               uuid(2),
               now: DateTime.add(expiry, 1, :second)
             )

    assert [] == settlement_jobs(task.id, first_run.id, 1)
    assert [] == settlement_jobs(task.id, replacement.id, 2)
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

  defp enable_goal_rollout do
    previous = Application.fetch_env!(:symmetry_control, :goals)
    Application.put_env(:symmetry_control, :goals, Keyword.put(previous, :rollout_enabled, true))
    on_exit(fn -> Application.put_env(:symmetry_control, :goals, previous) end)
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

  defp settlement_jobs(task_id, run_id, generation) do
    Repo.all(
      from job in Job,
        where:
          job.worker == "SymmetryControl.Goals.Workers.SettleTaskWorker" and
            job.args == ^%{"task_id" => task_id, "run_id" => run_id, "generation" => generation}
    )
  end

  defp insert_goal_event(goal_id, event_id, sequence, kind, revision \\ 1) do
    Repo.insert!(%GoalEvent{
      id: event_id,
      goal_id: goal_id,
      sequence: sequence,
      kind: kind,
      actor_ref: "operator:test",
      request_hash: :crypto.hash(:sha256, "#{event_id}:#{kind}"),
      request_hash_version: 2,
      revision: revision,
      payload: %{},
      response: %{}
    })
  end

  defp legacy_task_attrs do
    %{goal: "Retry legacy body", agent_profile: "codex", workspace: "primary", input: %{}}
  end

  defp project_key do
    "P" <> Integer.to_string(rem(System.unique_integer([:positive]), 9_999_999))
  end

  defp uuid(number),
    do: "00000000-0000-0000-0000-" <> String.pad_leading(Integer.to_string(number), 12, "0")
end
