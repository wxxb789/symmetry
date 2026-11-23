defmodule SymmetryControl.OrchestrationAssignmentTest do
  use SymmetryControl.OrchestrationCase, async: false

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.GoalEvent
  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Command
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.ProjectResource
  alias SymmetryControl.Workspaces.WorkItem

  @now ~U[2026-09-02 00:00:00.000000Z]

  test "capacity one cannot be over-assigned" do
    %{machine: machine} = enroll_machine()
    register_runtime(machine, @now, capacity: 1)

    assert {:ok, _task, :created} =
             Orchestration.submit_task(task_attrs(goal: "one"), "task-1", now: @now)

    assert {:ok, _task, :created} =
             Orchestration.submit_task(task_attrs(goal: "two"), "task-2", now: @now)

    assert {:ok, _run} = Orchestration.assign_one(now: @now)
    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
  end

  test "events reject NUL in nested JSONB map keys and string values before persistence" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    {:ok, _task, :created} = Orchestration.submit_task(task_attrs(), "nul-event", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime, @now)

    for event <- [
          %{
            event_id: "00000000-0000-0000-0000-000000000051",
            sequence: 1,
            kind: "progress",
            payload: %{"nested" => [%{"message" => "bad\0value"}]},
            occurred_at: @now
          },
          %{
            event_id: "00000000-0000-0000-0000-000000000052",
            sequence: 2,
            kind: "progress",
            payload: %{"bad\0key" => true},
            occurred_at: @now
          }
        ] do
      assert {:error, :invalid_request} =
               Orchestration.append_events(run.id, fence, [event], now: @now)
    end
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

  # The cases below came from the other base file's region and pin its clock;
  # the shadowing is intentional, so do not move a case across this line.
  @now ~U[2026-09-09 09:00:00.000000Z]

  test "legacy scheduling cannot select an unverified OMP or OmO runtime with empty task requirements" do
    for kind <- ["omp", "omo"] do
      runtime = register_unverified_omp_runtime("legacy-#{kind}-scheduling", kind)

      assert {:ok, task, :created} =
               Orchestration.submit_task(legacy_task_attrs(), Ecto.UUID.generate(), now: @now)

      assert task.goal_id == nil
      assert task.required_capabilities == %{}
      assert task.input == %{}
      assert task.agent_profile == runtime.agent_profile
      assert task.workspace == runtime.workspace

      assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
      assert %{state: "queued", current_generation: 0} = Repo.get!(Task, task.id)
      refute Repo.exists?(from run in Run, where: run.task_id == ^task.id)
    end
  end

  test "paused Goal tasks are not assigned until the Goal resumes, without blocking legacy tasks" do
    {goal_task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("goal-pause")

    Repo.update_all(from(goal in Goal, where: goal.id == ^goal_id), set: [state: "paused"])

    assert {:ok, legacy_task, :created} =
             Orchestration.submit_task(legacy_task_attrs(), "legacy-while-goal-paused", now: @now)

    assert {:ok, legacy_run} = Orchestration.assign_one(now: @now)
    assert legacy_run.task_id == legacy_task.id

    resumed_at = DateTime.shift(@now, second: 31)
    assert %{expired_runs: 1} = Orchestration.expire(now: DateTime.shift(@now, second: 30))

    assert {:ok, _snapshot} =
             Orchestration.heartbeat(runtime.id, runtime.connection_epoch, [], now: resumed_at)

    Repo.update_all(from(goal in Goal, where: goal.id == ^goal_id), set: [state: "active"])

    assert {:ok, resumed_run} = Orchestration.assign_one(now: resumed_at)
    assert resumed_run.task_id == goal_task.id
  end

  test "Goal assignment and claim require a current native runtime allowed by the Goal" do
    generic_machine = enroll_machine("goal-generic")

    assert {:ok, [generic_runtime]} =
             Orchestration.register_runtimes(
               generic_machine.id,
               Ecto.UUID.generate(),
               [
                 runtime_spec("goal-generic", %{
                   harness_kind: "generic",
                   harness_version: "generic-v1",
                   adapter_version: "symmetry-generic-1",
                   adapter_protocol_version: 1,
                   capabilities: %{
                     "structured_input" => true,
                     "interactive" => true,
                     "supervisory_control" => true,
                     "adapter" => generic_adapter()
                   }
                 })
               ],
               now: @now
             )

    disallowed = enroll_and_register_runtime("goal-disallowed")
    allowed = enroll_and_register_runtime("goal-allowed")

    {task, goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        execution_policy: Map.put(execution_policy(2), "allowed_runtime_ids", [allowed.id])
      )

    repository_resource_id = Repo.get!(WorkItem, task.work_item_id).repository_resource_id

    Repo.update_all(
      from(runtime in Runtime, where: runtime.id == ^generic_runtime.id),
      set: [repository_resource_id: repository_resource_id]
    )

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)

    Repo.update_all(
      from(runtime in Runtime, where: runtime.id in ^[allowed.id, disallowed.id]),
      set: [repository_resource_id: repository_resource_id]
    )

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == task.id
    assert run.runtime_id == allowed.id

    Repo.update_all(from(goal in Goal, where: goal.id == ^goal_id), set: [state: "paused"])

    assert {:error, :ownership_lost} =
             Goals.claim(
               run.id,
               %{
                 runtime_id: allowed.id,
                 runtime_epoch: allowed.connection_epoch,
                 generation: run.generation,
                 claim_id: Ecto.UUID.generate()
               },
               now: @now
             )

    assert {:ok, assigned} = Orchestration.fetch_run(run.id)
    assert assigned.state == "assigned"
  end

  test "OMP registration assigns and claims Goal work with its own fenced session identity" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)

    runtime =
      enroll_and_register_runtime("omp-native",
        harness_kind: "omp",
        harness_version: "18.20.0",
        adapter_version: "symmetry-omp-1"
      )

    persisted_runtime = Repo.get!(Runtime, runtime.id)
    assert persisted_runtime.harness_kind == "omp"
    assert persisted_runtime.harness_version == "18.20.0"
    assert persisted_runtime.capabilities["adapter"]["kind"] == "omp"

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == task.id
    assert run.runtime_id == runtime.id
    fence = goal_claim(run, runtime, @now)

    attrs = %{
      local_handle_id: Ecto.UUID.generate(),
      harness_kind: "omp",
      harness_version: "18.20.0",
      adapter_version: "symmetry-omp-1",
      workspace_fingerprint: "omp-workspace",
      workspace: task.workspace,
      repository_resource_id: runtime.repository_resource_id
    }

    assert {:error, :invalid_request} =
             Goals.attach_harness_session(
               runtime.machine_id,
               run.id,
               fence,
               %{attrs | harness_kind: "unsupported_harness"},
               now: @now
             )

    for changed <- [
          %{harness_kind: "pi"},
          %{harness_version: "18.20.1"},
          %{adapter_version: "symmetry-omp-2"}
        ] do
      assert {:error, :ownership_lost} =
               Goals.attach_harness_session(
                 runtime.machine_id,
                 run.id,
                 fence,
                 Map.merge(attrs, changed),
                 now: @now
               )
    end

    assert %{harness_session_id: nil, harness_binding_id: nil} = Repo.get!(Run, run.id)

    assert {:ok, %{session: session}, :created} =
             Goals.attach_harness_session(runtime.machine_id, run.id, fence, attrs, now: @now)

    assert session.harness_kind == "omp"
    assert session.harness_version == "18.20.0"
    assert session.adapter_version == "symmetry-omp-1"
    assert session.runtime_id == runtime.id
    assert session.run_id == run.id

    persisted_session = Repo.get!(HarnessSession, session.id)
    assert persisted_session.harness_kind == "omp"
    assert persisted_session.active_run_id == run.id
    assert persisted_session.binding_verified
    assert Repo.get!(Run, run.id).harness_binding_id == session.binding_id

    assert {:ok, %{session: ^session}, :replayed} =
             Goals.attach_harness_session(runtime.machine_id, run.id, fence, attrs, now: @now)
  end

  test "Goal assignment deterministically chooses one eligible native runtime" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    first = enroll_and_register_runtime("goal-runtime-first")
    second = enroll_and_register_runtime("goal-runtime-second")
    expected_runtime = Enum.min_by([first, second], & &1.id)

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == task.id
    assert run.runtime_id == expected_runtime.id
  end

  test "Goal assignment requires the runtime repository affinity" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    item = Repo.get!(WorkItem, task.work_item_id)

    assert {:ok, other_repository} =
             Workspaces.create_resource(item.project_id, %{
               kind: "repository",
               name: "Other runtime repository #{System.unique_integer([:positive])}"
             })

    _mismatched =
      enroll_and_register_runtime("goal-resource-mismatch",
        repository_resource_id: other_repository.id
      )

    matching = enroll_and_register_runtime("goal-resource-match")

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.runtime_id == matching.id
  end

  test "Goal claim rechecks repository affinity after assignment" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("claim-resource-affinity")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    item = Repo.get!(WorkItem, task.work_item_id)

    assert {:ok, other_repository} =
             Workspaces.create_resource(item.project_id, %{
               kind: "repository",
               name: "Claim mismatch repository #{System.unique_integer([:positive])}"
             })

    Repo.update_all(
      from(runtime_row in Runtime, where: runtime_row.id == ^runtime.id),
      set: [repository_resource_id: other_repository.id]
    )

    assert {:error, :ownership_lost} =
             Goals.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: Ecto.UUID.generate()
               },
               now: @now
             )
  end

  test "validation assignment requires every frozen binding to allow the runtime" do
    {project, repository} = project_repository_fixture("validation-assignment")
    first = enroll_and_register_runtime("validation-first", repository_resource_id: repository.id)

    second =
      enroll_and_register_runtime("validation-second", repository_resource_id: repository.id)

    outside =
      enroll_and_register_runtime("validation-outside", repository_resource_id: repository.id)

    {task, _goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        purpose: "validate",
        project_id: project.id,
        repository_id: repository.id,
        validation_bindings: [
          validation_binding("checks", "check", [first.id, second.id]),
          validation_binding("review", "review", [second.id])
        ]
      )

    assert task.purpose == "validate"
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.runtime_id == second.id
    refute run.runtime_id == outside.id
  end

  test "validation assignment fails closed when frozen bindings have no common runtime" do
    {project, repository} = project_repository_fixture("validation-no-common-runtime")

    first =
      enroll_and_register_runtime("validation-empty-first", repository_resource_id: repository.id)

    second =
      enroll_and_register_runtime("validation-empty-second",
        repository_resource_id: repository.id
      )

    {task, _goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        purpose: "validate",
        project_id: project.id,
        repository_id: repository.id,
        validation_bindings: [
          validation_binding("checks", "check", [first.id]),
          validation_binding("review", "review", [second.id])
        ]
      )

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
    assert %{state: "queued"} = Repo.get!(Task, task.id)
  end

  test "empty validation bindings add no runtime restriction while malformed bindings fail closed" do
    {unrestricted_task, _goal_id} =
      insert_goal_task(max_run_attempts: 2, purpose: "validate", validation_bindings: [])

    unrestricted_item = Repo.get!(WorkItem, unrestricted_task.work_item_id)

    unrestricted_runtime =
      enroll_and_register_runtime("validation-unrestricted",
        repository_resource_id: unrestricted_item.repository_resource_id
      )

    assert {:ok, unrestricted_run} = Orchestration.assign_one(now: @now)
    assert unrestricted_run.runtime_id == unrestricted_runtime.id

    {task, _goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        purpose: "validate",
        validation_bindings: [validation_binding("checks", "check", [])]
      )

    item = Repo.get!(WorkItem, task.work_item_id)

    _runtime =
      enroll_and_register_runtime("validation-malformed",
        repository_resource_id: item.repository_resource_id
      )

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)

    {malformed_task, _goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        purpose: "validate",
        project_id: item.project_id,
        repository_id: item.repository_resource_id,
        validation_bindings: [%{"profile_name" => "checks", "kind" => "check"}]
      )

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
    assert %{state: "queued"} = Repo.get!(Task, malformed_task.id)

    {invalid_runtime_task, _goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        purpose: "validate",
        project_id: item.project_id,
        repository_id: item.repository_resource_id,
        validation_bindings: [validation_binding("checks", "check", ["not-a-runtime-id"])]
      )

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
    assert %{state: "queued"} = Repo.get!(Task, invalid_runtime_task.id)
  end

  test "an unschedulable validation task does not starve a later eligible Goal task" do
    {blocked_task, _goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        purpose: "validate",
        validation_bindings: [validation_binding("checks", "check", [])]
      )

    blocked_item = Repo.get!(WorkItem, blocked_task.work_item_id)

    _blocked_runtime =
      enroll_and_register_runtime("validation-starved",
        repository_resource_id: blocked_item.repository_resource_id
      )

    Repo.update_all(
      from(task_row in Task, where: task_row.id == ^blocked_task.id),
      set: [inserted_at: DateTime.shift(@now, second: -1)]
    )

    {ready_task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    ready_item = Repo.get!(WorkItem, ready_task.work_item_id)

    ready_runtime =
      enroll_and_register_runtime("validation-ready",
        repository_resource_id: ready_item.repository_resource_id
      )

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == ready_task.id
    assert run.runtime_id == ready_runtime.id
  end

  test "keyset scanning advances beyond a full page of unschedulable Goal tasks" do
    Enum.each(1..32, fn _ ->
      {task, _goal_id} =
        insert_goal_task(
          max_run_attempts: 2,
          purpose: "validate",
          validation_bindings: [validation_binding("checks", "check", [])]
        )

      Repo.update_all(
        from(task_row in Task, where: task_row.id == ^task.id),
        set: [inserted_at: DateTime.shift(@now, second: -1)]
      )
    end)

    {ready_task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    ready_item = Repo.get!(WorkItem, ready_task.work_item_id)

    ready_runtime =
      enroll_and_register_runtime("validation-page-ready",
        repository_resource_id: ready_item.repository_resource_id
      )

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == ready_task.id
    assert run.runtime_id == ready_runtime.id
  end

  test "legacy Orchestration claim entry fails closed for a Goal-owned Run" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("goal-claim-fail-closed")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == task.id

    assert {:error, :goal_authority_required} =
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

    assert %{state: "assigned"} = Repo.get!(Task, task.id)
    assert %{state: "assigned", claim_id: nil} = Repo.get!(Run, run.id)
  end

  test "Goal resume durably rejects pending controls from the superseded pause action" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("revoke-superseded-pause")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    _fence = goal_claim(run, runtime, @now)

    pause_action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, pause_action_id, 1, "pause")

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "paused", event_sequence: 1]
    )

    command = insert_goal_pause_command(task, run, pause_action_id)
    goal = Repo.get!(Goal, goal_id)

    resume = %{
      schema_version: "symmetry.goal_command.v1",
      mutation_id: Ecto.UUID.generate(),
      expected_version: goal.operator_version,
      expected_revision: goal.current_revision,
      kind: "resume",
      payload: %{reason: "resume after pause"}
    }

    assert {:ok, _receipt, :created} =
             Goals.command(goal_id, resume, "operator:test", now: @now)

    assert %{state: "acknowledged", acknowledgement_outcome: "rejected"} =
             Repo.get!(Command, command.id)
  end

  test "Goal resume assignment is pinned to its retained compatible runtime" do
    _other = enroll_and_register_runtime("resume-other")
    retained_runtime = enroll_and_register_runtime("resume-retained", resume?: true)
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2, retained_runtime: retained_runtime)
    retained_session = Repo.get!(HarnessSession, task.requested_session_id)

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == task.id
    assert run.runtime_id == retained_runtime.id
    assert run.harness_session_id == retained_session.id
    assert retained_session.runtime_id == retained_runtime.id

    reserved_session = Repo.get!(HarnessSession, retained_session.id)
    assert run.harness_binding_id == reserved_session.binding_id

    fence = goal_claim(run, retained_runtime, @now)

    assert {:ok, %{session: %{id: session_id, state: "busy"}}, :created} =
             Goals.attach_harness_session(
               retained_runtime.machine_id,
               run.id,
               fence,
               %{
                 local_handle_id: retained_session.local_handle_id,
                 binding_id: run.harness_binding_id,
                 harness_kind: retained_session.harness_kind,
                 harness_version: retained_session.harness_version,
                 adapter_version: retained_session.adapter_version,
                 workspace_fingerprint: retained_session.workspace_fingerprint,
                 workspace: "primary",
                 repository_resource_id: retained_session.repository_resource_id
               },
               now: @now
             )

    assert session_id == retained_session.id

    assert {:ok, %{state: "failed"}} =
             Orchestration.transition(
               run.id,
               fence,
               "failed",
               %{"reason" => "terminal session state is not a stop receipt"},
               Ecto.UUID.generate(),
               now: @now
             )

    assert %{state: "unavailable", active_run_id: nil} = Repo.get!(HarnessSession, session_id)
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
    validation_of_task_id = if purpose == "validate", do: Ecto.UUID.generate()
    validation_bindings = Keyword.get(opts, :validation_bindings, [])
    policy = Keyword.get(opts, :execution_policy, execution_policy(max_run_attempts))
    allowed_runtime_ids = Map.get(policy, "allowed_runtime_ids", [])
    strict_budget? = Map.get(policy, "budget_mode", "soft") == "strict"
    retained_runtime = Keyword.get(opts, :retained_runtime)

    requested_session_id =
      Keyword.get(opts, :requested_session_id) || if(retained_runtime, do: Ecto.UUID.generate())

    task_inserted_at = Keyword.get(opts, :inserted_at, @now)

    task_input =
      then(
        %{
          "subject" => %{
            "resource_id" => repository_id,
            "commit" => String.duplicate("a", 40),
            "tree_digest" => "sha256:" <> String.duplicate("b", 64)
          }
        },
        fn input ->
          if requested_session_id do
            input
            |> Map.put("session_mode", "resume")
            |> Map.put("requested_session_id", requested_session_id)
          else
            input
          end
        end
      )

    task_input =
      Map.put(task_input, "limits", %{
        "max_turns" => 1,
        "deadline_at" => DateTime.to_iso8601(DateTime.shift(@now, minute: 15)),
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

               if !Repo.exists?(from(resource in ProjectResource, where: resource.id == ^repository_id)) do
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

               if !planning? do
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
      kind
      |> native_adapter(native_version, implementation_version, protocol_version)
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
             Orchestration.register_runtimes(machine.id, Ecto.UUID.generate(), [specification], now: @now)

    runtime
  end

  defp generic_adapter do
    %{
      "kind" => "generic",
      "native_version" => "generic-v1",
      "implementation_version" => "symmetry-generic-1",
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
      harness_kind
      |> native_adapter(harness_version, adapter_version, 1)
      |> put_in(["operations", "resume"], resume?)
      |> put_in(["operations", "handoff"], handoff?)
      |> put_in(["operations", "hard_cost_limit"], hard_cost_limit?)

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine_id,
               Ecto.UUID.generate(),
               [
                 then(
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
                   },
                   fn specification ->
                     if is_nil(repository_resource_id),
                       do: specification,
                       else: Map.put(specification, :repository_resource_id, repository_resource_id)
                   end
                 )
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

  defp insert_goal_pause_command(task, run, action_id) do
    {request_hash, request_hash_version} = RequestHash.write(%{kind: "pause", payload: %{}})

    Repo.insert!(%Command{
      task_id: task.id,
      run_id: run.id,
      generation: run.generation,
      kind: "pause",
      payload: %{},
      idempotency_key: "goal-control:#{action_id}:pause:#{task.id}",
      request_hash: request_hash,
      request_hash_version: request_hash_version,
      state: "pending"
    })
  end

  defp legacy_task_attrs do
    %{goal: "Retry legacy body", agent_profile: "codex", workspace: "primary", input: %{}}
  end

  defp project_key do
    "P" <> Integer.to_string(rem(System.unique_integer([:positive]), 9_999_999))
  end
end
