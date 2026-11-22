defmodule SymmetryControl.Orchestration.ClaimsTest do
  use SymmetryControl.OrchestrationCase, async: false

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Claims
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Orchestration.{Run, Runtime, Task}
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces
  @now ~U[2026-09-09 09:00:00.000000Z]

  test "claim_run claims a legacy run without provider access" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    assert {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "claims-1", now: @now)
    assert {:ok, run} = Orchestration.assign_one(now: @now)

    attach_claimed_events()

    assert {:ok, {claimed, claimed_task, provider_access}} =
             Claims.claim_run(run.id, claim_request(run, runtime), now: @now)

    assert claimed.id == run.id
    assert claimed.state == "claimed"
    assert claimed.lease_token
    assert claimed_task.id == task.id
    assert provider_access == nil
    assert Repo.get!(Task, task.id).state == "claimed"
    assert_receive :claimed_event
    refute_receive :claimed_event, 20
  end

  test "claim_run replays the same claim id without re-emitting the claimed event" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    assert {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "claims-2", now: @now)
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    request = claim_request(run, runtime)

    assert {:ok, {created, _task, _access}} = Claims.claim_run(run.id, request, now: @now)

    attach_claimed_events()

    assert {:ok, {replayed, replayed_task, _access}} =
             Claims.claim_run(run.id, request, now: @now)

    assert replayed.id == created.id
    assert replayed.lease_token == created.lease_token
    assert replayed_task.id == task.id
    refute_receive :claimed_event, 20
  end

  test "claim_run rejects a malformed request before claiming" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    assert {:ok, _task, :created} = Orchestration.submit_task(task_attrs(), "claims-3", now: @now)
    assert {:ok, run} = Orchestration.assign_one(now: @now)

    assert {:error, :invalid_request} = Claims.claim_run(run.id, %{}, now: @now)

    assert {:error, :not_found} =
             Claims.claim_run(Ecto.UUID.generate(), claim_request(run, runtime))

    assert Repo.get!(Run, run.id).state == "assigned"
  end

  test "claim_run claims a goal-owned plan run through Goal authority" do
    %{machine: machine} = enroll_machine()
    {goal_id, task} = insert_goal_plan_task()
    runtime = register_goal_runtime(machine, task)
    run = insert_assigned_run!(task, runtime)

    assert {:ok, {claimed, claimed_task, nil}} =
             Claims.claim_run(run.id, claim_request(run, runtime), now: @now)

    assert claimed.state == "claimed"
    assert claimed_task.id == task.id
    assert Repo.get!(Task, task.id).state == "claimed"

    assert {:error, :goal_authority_required} =
             Claims.claim_with_disposition(run.id, claim_request(run, runtime), now: @now)

    assert Repo.get!(Task, task.id).goal_id == goal_id
  end

  test "claim_run refuses a goal claim whose runtime lost repository affinity" do
    %{machine: machine} = enroll_machine()
    {goal_id, task} = insert_goal_plan_task()
    runtime = register_goal_runtime(machine, task)
    run = insert_assigned_run!(task, runtime)

    project_id =
      Repo.one!(from goal in Goal, where: goal.id == ^goal_id, select: goal.project_id)

    assert {:ok, other_repository} =
             Workspaces.create_resource(project_id, %{
               kind: "repository",
               name: "Claims other repository #{System.unique_integer([:positive])}"
             })

    Repo.update_all(
      from(runtime_row in Runtime, where: runtime_row.id == ^runtime.id),
      set: [repository_resource_id: other_repository.id]
    )

    assert {:error, :ownership_lost} =
             Claims.claim_run(run.id, claim_request(run, runtime), now: @now)

    assert Repo.get!(Run, run.id).state == "assigned"
    assert Repo.get!(Task, task.id).state == "assigned"
  end

  defp claim_request(run, runtime) do
    %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: Ecto.UUID.generate()
    }
  end

  defp attach_claimed_events do
    handler = "claims-test-#{System.unique_integer([:positive])}"
    test = self()

    :telemetry.attach(
      handler,
      [:symmetry_control, :orchestration, :run, :claimed],
      fn _event, _measurements, _metadata, pid -> send(pid, :claimed_event) end,
      test
    )

    on_exit(fn -> :telemetry.detach(handler) end)
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

  defp insert_goal_plan_task do
    %{id: project_id} = insert_project()
    repository_id = insert_repository(project_id)
    goal_id = Ecto.UUID.generate()
    snapshot_id = Ecto.UUID.generate()
    task_id = Ecto.UUID.generate()
    admission_key = Ecto.UUID.generate()

    Repo.query!(
      """
      INSERT INTO goals (
        id, project_id, title, state, current_revision, event_sequence, inserted_at, updated_at
      ) VALUES ($1, $2, 'Claims goal', 'draft', 1, 0, $3, $3)
      """,
      [db_uuid(goal_id), db_uuid(project_id), @now]
    )

    Repo.query!(
      """
      INSERT INTO goal_revisions (
        goal_id, revision, objective, non_goals, acceptance_contract, authority_policy,
        execution_policy, context_manifest, reason, actor_ref, inserted_at
      ) VALUES ($1, 1, 'Exercise the claim seam', '[]'::jsonb, '{}'::jsonb, '{}'::jsonb,
        $3::text::jsonb, '{}'::jsonb, 'initial', 'operator:test', $2)
      """,
      [db_uuid(goal_id), @now, Jason.encode!(goal_execution_policy())]
    )

    Repo.query!(
      """
      INSERT INTO context_snapshots (
        id, goal_id, goal_revision, work_item_id, schema_version, content_hash, payload, inserted_at
      ) VALUES ($1, $2, 1, NULL, 1, $3, $4::text::jsonb, $5)
      """,
      [
        db_uuid(snapshot_id),
        db_uuid(goal_id),
        :crypto.hash(:sha256, "claims-context"),
        Jason.encode!(%{"work_contract" => %{"purpose" => "plan"}}),
        @now
      ]
    )

    Repo.query!(
      """
      INSERT INTO tasks (
        id, idempotency_key, request_hash, request_hash_version, goal, agent_profile, workspace,
        input, required_capabilities, state, current_generation, attempt_generation, work_item_id,
        goal_id, goal_revision, context_snapshot_id, purpose, validation_of_task_id, admission_key,
        allowed_runtime_ids, max_run_attempts, inserted_at, updated_at
      ) VALUES ($1, $2, $3, 1, 'Claims plan task', 'codex', 'primary', $4::text::jsonb,
        '{}'::jsonb, 'queued', 0, 1, NULL, $5, 1, $6, 'plan', NULL, $7,
        ARRAY[]::uuid[], 1, $8, $8)
      """,
      [
        db_uuid(task_id),
        "claims-plan-task-#{System.unique_integer([:positive])}",
        :crypto.hash(:sha256, "claims-plan-task"),
        Jason.encode!(%{"subject" => %{"resource_id" => repository_id}}),
        db_uuid(goal_id),
        db_uuid(snapshot_id),
        db_uuid(admission_key),
        @now
      ]
    )

    {goal_id, Repo.get!(Task, task_id)}
  end

  defp insert_project do
    project_id = Ecto.UUID.generate()

    Repo.query!(
      """
      INSERT INTO projects (
        id, name, key, status, default_agent_profile, default_workspace, inserted_at, updated_at
      ) VALUES ($1, 'Claims project', $2, 'active', 'codex', 'primary', $3, $3)
      """,
      [db_uuid(project_id), project_key(), @now]
    )

    %{id: project_id}
  end

  defp insert_repository(project_id) do
    repository_id = Ecto.UUID.generate()

    Repo.query!(
      """
      INSERT INTO project_resources (
        id, project_id, kind, name, status, sync_status, metadata, lock_version, inserted_at, updated_at
      ) VALUES ($1, $2, 'repository', $3, 'unknown', 'unknown', '{}'::jsonb, 1, $4, $4)
      """,
      [
        db_uuid(repository_id),
        db_uuid(project_id),
        "Claims repository #{System.unique_integer([:positive])}",
        @now
      ]
    )

    repository_id
  end

  defp register_goal_runtime(machine, task) do
    adapter = native_adapter("codex", "0.153.4", "symmetry-codex-1", 1)

    specification =
      runtime_spec("claims-runtime",
        harness_kind: "codex",
        harness_version: "0.153.4",
        adapter_version: "symmetry-codex-1",
        adapter_protocol_version: 1,
        capabilities: %{"adapter" => adapter}
      )

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(machine.id, Ecto.UUID.generate(), [specification],
               now: @now
             )

    repository_id = task.input |> Map.fetch!("subject") |> Map.fetch!("resource_id")

    Repo.update_all(
      from(runtime_row in Runtime, where: runtime_row.id == ^runtime.id),
      set: [repository_resource_id: repository_id]
    )

    Repo.get!(Runtime, runtime.id)
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
      Map.new(overrides)
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

  defp project_key do
    "claims-#{System.unique_integer([:positive])}"
  end

  defp goal_execution_policy do
    %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "max_run_attempts_per_task" => 1,
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

  defp db_uuid(uuid), do: Ecto.UUID.dump!(uuid)
end
