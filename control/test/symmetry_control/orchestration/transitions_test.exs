defmodule SymmetryControl.Orchestration.TransitionsTest do
  use SymmetryControl.OrchestrationCase, async: false

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Run, RunEvent, RunTransition, Task, Transitions}

  @now ~U[2026-09-09 09:00:00.000000Z]

  test "transition creates a transition and replays the same id without rewriting state" do
    {run, fence} = claimed_legacy_run("transition-replay")
    transition_id = Ecto.UUID.generate()
    other_id = Ecto.UUID.generate()

    assert {:ok, %{state: "running"}} =
             Transitions.transition(run.id, fence, "running", %{}, transition_id, now: @now)

    assert {:ok, %{state: "completed"}} =
             Transitions.transition(run.id, fence, "completed", %{}, other_id, now: @now)

    assert {:ok, replayed} =
             Transitions.transition(run.id, fence, "running", %{}, transition_id, now: @now)

    assert replayed.id == run.id
    assert replayed.state == "running"
    assert %{state: "completed"} = Repo.get!(Run, run.id)

    transitions = from(transition in RunTransition, where: transition.run_id == ^run.id)
    assert 2 == Repo.aggregate(transitions, :count)
  end

  test "transition rejects the same transition id with a changed payload" do
    {run, fence} = claimed_legacy_run("transition-conflict")
    transition_id = Ecto.UUID.generate()

    assert {:ok, %{state: "running"}} =
             Transitions.transition(run.id, fence, "running", %{}, transition_id, now: @now)

    assert {:error, :idempotency_conflict} =
             Transitions.transition(
               run.id,
               fence,
               "running",
               %{"note" => "changed"},
               transition_id,
               now: @now
             )

    assert %{state: "running"} = Repo.get!(Run, run.id)

    transitions = from(transition in RunTransition, where: transition.run_id == ^run.id)
    assert 1 == Repo.aggregate(transitions, :count)
  end

  test "transition refuses paused on a goal-owned run without goal authority" do
    task = goal_plan_task!("goal-pause")
    runtime = register_runtime(enroll_machine("goal-pause"), @now)
    run = insert_assigned_run!(task, runtime)

    fence = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: Ecto.UUID.generate(),
      lease_token: Ecto.UUID.generate()
    }

    assert {:error, :goal_authority_required} =
             Transitions.transition(run.id, fence, "paused", %{}, Ecto.UUID.generate(), now: @now)

    assert %{state: "assigned", claim_id: nil} = Repo.get!(Run, run.id)
  end

  test "append_events appends a batch and replays the same event id" do
    {run, fence} = claimed_legacy_run("append-replay")

    event = %{
      event_id: Ecto.UUID.generate(),
      sequence: 1,
      kind: "progress",
      payload: %{"message" => "first"},
      occurred_at: @now
    }

    assert {:ok, [stored]} = Transitions.append_events(run.id, fence, [event], now: @now)
    assert stored.event_id == event.event_id

    assert {:ok, [replayed]} = Transitions.append_events(run.id, fence, [event], now: @now)
    assert replayed.id == stored.id
    assert replayed.request_hash == stored.request_hash

    assert 1 == Repo.aggregate(RunEvent, :count)
  end

  test "append_events rejects a changed payload for an existing event id" do
    {run, fence} = claimed_legacy_run("append-conflict")
    event_id = Ecto.UUID.generate()

    event = %{
      event_id: event_id,
      sequence: 1,
      kind: "progress",
      payload: %{"message" => "first"},
      occurred_at: @now
    }

    assert {:ok, [_]} = Transitions.append_events(run.id, fence, [event], now: @now)

    assert {:error, :idempotency_conflict} =
             Transitions.append_events(
               run.id,
               fence,
               [%{event | payload: %{"message" => "changed"}}],
               now: @now
             )

    assert 1 == Repo.aggregate(RunEvent, :count)
  end

  test "append_events rejects an event with a non-uuid id before touching the run" do
    {run, fence} = claimed_legacy_run("append-invalid")

    event = %{
      event_id: "not-a-uuid",
      sequence: 1,
      kind: "progress",
      payload: %{},
      occurred_at: @now
    }

    assert {:error, :invalid_request} =
             Transitions.append_events(run.id, fence, [event], now: @now)

    refute Repo.exists?(from(run_event in RunEvent, where: run_event.run_id == ^run.id))
  end

  test "expire expires an assignment past its deadline and reports both counts" do
    {task, run, _runtime} = assigned_legacy_run("expire-assignment")

    assert %{expired_runs: 1, offline_runtimes: 1} =
             Transitions.expire(now: DateTime.add(@now, 31, :second))

    assert %{state: "expired"} = Repo.get!(Run, run.id)
    assert %{state: "queued", attempt_generation: 2} = Repo.get!(Task, task.id)
  end

  test "expire reports zero counts while the assignment is still live" do
    {_task, run, _runtime} = assigned_legacy_run("expire-live")

    assert %{expired_runs: 0, offline_runtimes: 0} = Transitions.expire(now: @now)
    assert %{state: "assigned"} = Repo.get!(Run, run.id)
  end

  test "reconcile rejects malformed journals before touching runtime state" do
    runtime_id = Ecto.UUID.generate()

    assert {:error, :invalid_request} =
             Transitions.reconcile(runtime_id, 1, [%{run_id: "not-a-uuid", generation: 1}])

    assert {:error, :invalid_request} = Transitions.reconcile(runtime_id, 1, [:not_a_journal])
  end

  defp enroll_machine(label) do
    key = Ecto.UUID.generate()
    token = "transitions-#{label}-#{key}"

    assert {:ok, %{machine: machine}, :created} =
             Orchestration.enroll_machine(
               %{name: "transitions #{label}", machine_token: token},
               key,
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret"
             )

    machine
  end

  defp claimed_legacy_run(label) do
    runtime = register_runtime(enroll_machine(label), @now)

    assert {:ok, _task, :created} =
             Orchestration.submit_task(
               task_attrs(goal: "transitions #{label}"),
               "transitions-#{label}",
               now: @now
             )

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    {run, claim(run, runtime, @now)}
  end

  defp assigned_legacy_run(label) do
    runtime = register_runtime(enroll_machine(label), @now)

    assert {:ok, task, :created} =
             Orchestration.submit_task(
               task_attrs(goal: "expire #{label}"),
               "expire-#{label}",
               now: @now
             )

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    {task, run, runtime}
  end

  defp goal_plan_task!(label) do
    project_id = Ecto.UUID.generate()
    goal_id = Ecto.UUID.generate()
    snapshot_id = Ecto.UUID.generate()
    task_id = Ecto.UUID.generate()
    idempotency_key = "goal-plan-#{label}-#{System.unique_integer([:positive])}"

    {:ok, task} =
      Repo.transaction(fn ->
        Repo.query!(
          """
          INSERT INTO projects (
            id, name, key, status, default_agent_profile, default_workspace, inserted_at, updated_at
          ) VALUES ($1, $2, $3, 'active', 'codex', 'primary', $4, $4)
          """,
          [db_uuid(project_id), "Transitions goal project #{label}", project_key(), @now]
        )

        Repo.query!(
          """
          INSERT INTO goals (
            id, project_id, title, state, current_revision, event_sequence, inserted_at, updated_at
          ) VALUES ($1, $2, $3, 'draft', 1, 0, $4, $4)
          """,
          [db_uuid(goal_id), db_uuid(project_id), "Transitions goal #{label}", @now]
        )

        Repo.query!(
          """
          INSERT INTO goal_revisions (
            goal_id, revision, objective, non_goals, acceptance_contract, authority_policy,
            execution_policy, context_manifest, reason, actor_ref, inserted_at
          ) VALUES ($1, 1, $2, '[]'::jsonb, '{}'::jsonb, '{}'::jsonb, $3::text::jsonb,
            '{}'::jsonb, 'initial', 'operator:test', $4)
          """,
          [
            db_uuid(goal_id),
            "Transitions goal plan #{label}",
            Jason.encode!(goal_execution_policy()),
            @now
          ]
        )

        Repo.query!(
          """
          INSERT INTO context_snapshots (
            id, goal_id, goal_revision, work_item_id, schema_version, content_hash, payload,
            inserted_at
          ) VALUES ($1, $2, 1, NULL, 1, $3, $4::text::jsonb, $5)
          """,
          [
            db_uuid(snapshot_id),
            db_uuid(goal_id),
            :crypto.hash(:sha256, idempotency_key),
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
          ) VALUES ($1, $2, $3, 1, $4, 'codex', 'primary', '{}'::jsonb,
            '{}'::jsonb, 'queued', 0, 1, NULL, $5, 1, $6, 'plan', NULL, $7,
            ARRAY[]::uuid[], 1, $8, $8)
          """,
          [
            db_uuid(task_id),
            idempotency_key,
            :crypto.hash(:sha256, idempotency_key),
            "Transitions goal plan task #{label}",
            db_uuid(goal_id),
            db_uuid(snapshot_id),
            db_uuid(Ecto.UUID.generate()),
            @now
          ]
        )

        Repo.get!(Task, task_id)
      end)

    task
  end

  defp insert_assigned_run!(task, runtime) do
    %Run{}
    |> Run.changeset(%{
      task_id: task.id,
      runtime_id: runtime.id,
      generation: 1,
      state: "assigned",
      assigned_at: @now,
      assignment_expires_at: DateTime.add(@now, 60, :second)
    })
    |> Repo.insert!()
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

  defp project_key do
    "P" <> Integer.to_string(rem(System.unique_integer([:positive]), 9_999_999))
  end

  defp db_uuid(uuid), do: Ecto.UUID.dump!(uuid)
end
