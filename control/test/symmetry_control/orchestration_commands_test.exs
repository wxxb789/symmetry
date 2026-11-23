defmodule SymmetryControl.OrchestrationCommandsTest do
  use SymmetryControl.OrchestrationCase, async: false

  alias SymmetryControl.Orchestration

  @now ~U[2026-09-02 00:00:00.000000Z]

  test "acknowledgements honor terminal grace after reaper expiry" do
    {expired_task, expired_run, _runtime, expired_fence} = terminal_grace_fixture("expired-ack")

    assert {:ok, %{state: "waiting_for_input"}} =
             Orchestration.transition(
               expired_run.id,
               expired_fence,
               "waiting_for_input",
               %{},
               Ecto.UUID.generate(),
               now: @now
             )

    assert {:ok, expired_command, :created} =
             Orchestration.create_command(
               expired_task.id,
               "provide_input",
               %{"answer" => "yes"},
               "expired-ack-command",
               now: @now
             )

    expired_at = DateTime.add(@now, 30, :second)
    grace_boundary = DateTime.add(@now, 510, :second)
    after_grace = DateTime.add(grace_boundary, 1, :second)

    assert %{expired_runs: 1} = Orchestration.expire(now: expired_at)
    assert {:ok, %{state: "expired"}} = Orchestration.fetch_run(expired_run.id)

    expired_acknowledgement_id = Ecto.UUID.generate()

    assert {:ok, %{state: "acknowledged"}} =
             Orchestration.acknowledge_command(
               expired_command.id,
               expired_fence,
               "applied",
               expired_acknowledgement_id,
               now: grace_boundary
             )

    assert {:error, :idempotency_conflict} =
             Orchestration.acknowledge_command(
               expired_command.id,
               expired_fence,
               "failed",
               expired_acknowledgement_id,
               now: after_grace
             )

    {cancel_task, cancel_run, _runtime, cancel_fence} = terminal_grace_fixture("cancelled-ack")

    assert {:ok, cancel_command, :created} =
             Orchestration.create_command(
               cancel_task.id,
               "cancel",
               %{},
               "cancelled-ack-command",
               now: @now
             )

    assert %{expired_runs: 1} = Orchestration.expire(now: expired_at)
    assert {:ok, %{state: "cancelled"}} = Orchestration.fetch_run(cancel_run.id)

    cancel_acknowledgement_id = Ecto.UUID.generate()

    assert {:ok, %{state: "acknowledged"}} =
             Orchestration.acknowledge_command(
               cancel_command.id,
               cancel_fence,
               "applied",
               cancel_acknowledgement_id,
               now: grace_boundary
             )

    assert {:error, :idempotency_conflict} =
             Orchestration.acknowledge_command(
               cancel_command.id,
               cancel_fence,
               "applied",
               Ecto.UUID.generate(),
               now: after_grace
             )
  end

  test "a new acknowledgement after terminal grace expires" do
    {task, _run, _runtime, fence} =
      terminal_grace_fixture("unacknowledged-grace", "waiting_for_input")

    assert {:ok, command, :created} =
             Orchestration.create_command(
               task.id,
               "provide_input",
               %{"answer" => "yes"},
               "unacknowledged-grace-command",
               now: @now
             )

    assert %{expired_runs: 1} = Orchestration.expire(now: DateTime.add(@now, 30, :second))

    assert {:error, :terminal_grace_expired} =
             Orchestration.acknowledge_command(
               command.id,
               fence,
               "applied",
               Ecto.UUID.generate(),
               now: DateTime.add(@now, 511, :second)
             )
  end

  test "terminal transition replays and acknowledgement conflicts survive grace" do
    {_task, run, _runtime, fence} = terminal_grace_fixture("terminal-replay")
    inside_grace = DateTime.add(@now, 509, :second)
    outside_grace = DateTime.add(@now, 511, :second)
    transition_id = Ecto.UUID.generate()
    transition_payload = %{"summary" => "completed after expiry"}

    assert {:ok, %{state: "completed"}} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               transition_payload,
               transition_id,
               now: inside_grace
             )

    assert {:ok, %{state: "completed", result: ^transition_payload}} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               transition_payload,
               transition_id,
               now: outside_grace
             )

    assert {:error, :terminal_grace_expired} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               transition_payload,
               Ecto.UUID.generate(),
               now: outside_grace
             )

    {ack_task, ack_run, _runtime, ack_fence} = terminal_grace_fixture("ack-replay")

    assert {:ok, command, :created} =
             Orchestration.create_command(ack_task.id, "cancel", %{}, "ack-replay-command",
               now: @now
             )

    acknowledgement_id = Ecto.UUID.generate()

    assert {:ok, %{state: "acknowledged"}} =
             Orchestration.acknowledge_command(
               command.id,
               ack_fence,
               "applied",
               acknowledgement_id,
               now: inside_grace
             )

    assert {:ok, %{state: "acknowledged", acknowledgement_id: ^acknowledgement_id}} =
             Orchestration.acknowledge_command(
               command.id,
               ack_fence,
               "applied",
               acknowledgement_id,
               now: outside_grace
             )

    assert {:error, :idempotency_conflict} =
             Orchestration.acknowledge_command(
               command.id,
               ack_fence,
               "applied",
               Ecto.UUID.generate(),
               now: outside_grace
             )

    assert {:ok, %{state: "cancelling"}} = Orchestration.fetch_run(ack_run.id)
  end

  test "cancellation handles queued work and serializes with terminal completion" do
    {:ok, queued, :created} = Orchestration.submit_task(task_attrs(), "task-1", now: @now)

    assert {:ok, cancelled, queued_command} = Orchestration.request_cancel(queued.id, now: @now)

    assert cancelled.state == "cancelled"
    assert queued_command.task_id == queued.id
    assert queued_command.run_id == nil
    assert queued_command.generation == nil
    assert queued_command.state == "applied"
    assert queued_command.applied_at == @now

    assert {:ok, ^cancelled, replayed_queued_command} =
             Orchestration.request_cancel(queued.id, now: @now)

    assert replayed_queued_command.id == queued_command.id

    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)

    {:ok, task, :created} =
      Orchestration.submit_task(task_attrs(goal: "live"), "task-2", now: @now)

    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime, @now)

    {:ok, _} =
      Orchestration.transition(
        run.id,
        fence,
        "running",
        %{},
        "00000000-0000-0000-0000-000000000034",
        now: @now
      )

    assert {:ok, cancelling, command} = Orchestration.request_cancel(task.id, now: @now)
    assert cancelling.state == "cancelling"
    assert command.kind == "cancel"

    assert {:ok, acknowledged} =
             Orchestration.acknowledge_command(
               command.id,
               fence,
               "applied",
               "00000000-0000-0000-0000-000000000045",
               now: @now
             )

    assert acknowledged.acknowledgement_outcome == "applied"

    assert {:ok, replayed_acknowledgement} =
             Orchestration.acknowledge_command(
               command.id,
               fence,
               "applied",
               "00000000-0000-0000-0000-000000000045",
               now: @now
             )

    assert replayed_acknowledgement.id == command.id

    assert {:error, :invalid_transition} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               %{},
               "00000000-0000-0000-0000-000000000035",
               now: @now
             )

    assert {:ok, same, same_command} = Orchestration.request_cancel(task.id, now: @now)
    assert same.state == "cancelling"
    assert same_command.id == command.id

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "cancelled",
               %{},
               "00000000-0000-0000-0000-000000000046",
               now: @now
             )

    assert {:ok, terminal_cancelled, retained_command} =
             Orchestration.request_cancel(task.id, now: @now)

    assert terminal_cancelled.state == "cancelled"
    assert retained_command.id == command.id
  end

  test "provide_input requires a current waiting generation" do
    {:ok, queued_task, :created} =
      Orchestration.submit_task(task_attrs(), "input-without-waiting-run", now: @now)

    assert {:error, :state_conflict} =
             Orchestration.create_command(
               queued_task.id,
               "provide_input",
               %{"answer" => "no run"},
               "input-without-waiting-run",
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

  defp terminal_grace_fixture(label, local_state \\ "running") do
    %{machine: machine} = enroll_machine()
    agent_profile = "terminal-grace-#{label}-#{System.unique_integer([:positive])}"
    runtime = register_runtime(machine, @now, agent_profile: agent_profile)

    assert {:ok, task, :created} =
             Orchestration.submit_task(
               task_attrs(agent_profile: agent_profile),
               "terminal-grace-#{label}-#{System.unique_integer([:positive])}",
               now: @now
             )

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime, @now)

    case local_state do
      "claimed" ->
        :ok

      "running" ->
        assert {:ok, %{state: "running"}} =
                 Orchestration.transition(
                   run.id,
                   fence,
                   "running",
                   %{},
                   Ecto.UUID.generate(),
                   now: @now
                 )

      "waiting_for_input" ->
        assert {:ok, %{state: "running"}} =
                 Orchestration.transition(
                   run.id,
                   fence,
                   "running",
                   %{},
                   Ecto.UUID.generate(),
                   now: @now
                 )

        assert {:ok, %{state: "waiting_for_input"}} =
                 Orchestration.transition(
                   run.id,
                   fence,
                   "waiting_for_input",
                   %{},
                   Ecto.UUID.generate(),
                   now: @now
                 )
    end

    {task, run, runtime, fence}
  end

  alias SymmetryControl.Goals

  alias SymmetryControl.Goals.{
    Goal,
    GoalBudgetReservation,
    GoalEvent,
    GoalRevision,
    HarnessSession
  }

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Command, Runtime, Task}
  alias SymmetryControl.Workspaces.{Project, ProjectResource}

  # The cases below came from the other base file's region and pin its clock;
  # the shadowing is intentional, so do not move a case across this line.
  @now ~U[2026-09-09 09:00:00.000000Z]

  test "legacy command entry points cannot mutate a Goal task or its reservation" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)

    assert {:error, :goal_authority_required} =
             Orchestration.create_command(task.id, "cancel", %{}, "legacy-goal-cancel", now: @now)

    assert {:error, :goal_authority_required} = Orchestration.request_cancel(task.id, now: @now)

    assert {:error, :goal_authority_required} =
             Orchestration.provide_input(task.id, %{"answer" => "legacy"}, "legacy-goal-input",
               now: @now
             )

    assert {:error, :goal_authority_required} =
             Orchestration.retry_task(
               task.id,
               legacy_task_attrs(),
               "legacy-goal-retry",
               expected_generation: 1,
               now: @now
             )

    assert {:ok, persisted_task} = Orchestration.fetch_task(task.id)
    assert persisted_task.state == "queued"

    assert "held" ==
             Repo.one!(
               from reservation in GoalBudgetReservation,
                 where: reservation.task_id == ^task.id,
                 select: reservation.state
             )
  end

  test "legacy Goal control facade fails closed before writing a command" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    action_id = Ecto.UUID.generate()

    assert {:error, :goal_authority_required} =
             Orchestration.create_goal_control_command(
               goal_id,
               1,
               action_id,
               task.id,
               "cancel",
               %{},
               "goal-control:#{action_id}:cancel:#{task.id}",
               now: @now
             )

    refute Repo.exists?(from(command in Command, where: command.task_id == ^task.id))
  end

  test "Goal control dispatch rejects a superseded lifecycle action" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
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

    refute Repo.exists?(from(command in Command, where: command.task_id == ^task.id))
  end

  test "Goal control dispatch creates and exactly replays one command" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, action_id, 1, "cancel")

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "cancelled", event_sequence: 1]
    )

    args = [
      goal_id,
      1,
      action_id,
      task.id,
      "cancel",
      %{},
      "goal-control:#{action_id}:cancel:#{task.id}",
      [now: @now]
    ]

    assert {:ok, first, :created} = apply(Goals, :dispatch_goal_control_command, args)
    assert {:ok, replayed, :replayed} = apply(Goals, :dispatch_goal_control_command, args)
    assert replayed == first

    assert 1 ==
             Repo.aggregate(from(command in Command, where: command.task_id == ^task.id), :count)
  end

  test "Goal control replay trusts the stored command after Goal and Task advancement" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    action_id = Ecto.UUID.generate()
    insert_goal_revision(goal_id, 2)
    insert_goal_event(goal_id, action_id, 1, "amend", 2)

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "paused", current_revision: 2, event_sequence: 1]
    )

    idempotency_key = "goal-control:#{action_id}:cancel:#{task.id}"

    assert {:ok, first, :created} =
             Goals.dispatch_goal_control_command(
               goal_id,
               2,
               action_id,
               task.id,
               "cancel",
               %{},
               idempotency_key,
               now: @now
             )

    assert Repo.get!(Task, task.id).state == "cancelled"

    resume_action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, resume_action_id, 2, "resume", 2)

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "active", event_sequence: 2]
    )

    assert {:ok, replayed, :replayed} =
             Goals.dispatch_goal_control_command(
               goal_id,
               2,
               action_id,
               task.id,
               "cancel",
               %{},
               idempotency_key,
               now: @now
             )

    assert replayed == first
  end

  test "exact Goal control replay remains available after Project archival" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    goal = Repo.get!(Goal, goal_id)
    action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, action_id, 1, "cancel")

    Repo.update_all(
      from(goal_row in Goal, where: goal_row.id == ^goal_id),
      set: [state: "cancelled", event_sequence: 1]
    )

    idempotency_key = "goal-control:#{action_id}:cancel:#{task.id}"

    assert {:ok, first, :created} =
             Goals.dispatch_goal_control_command(
               goal_id,
               1,
               action_id,
               task.id,
               "cancel",
               %{},
               idempotency_key,
               now: @now
             )

    Repo.update_all(
      from(project in Project, where: project.id == ^goal.project_id),
      set: [status: "archived"]
    )

    assert {:ok, replayed, :replayed} =
             Goals.dispatch_goal_control_command(
               goal_id,
               1,
               action_id,
               task.id,
               "cancel",
               %{},
               idempotency_key,
               now: @now
             )

    assert replayed == first
  end

  test "new Goal control command rejects an archived Project" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    goal = Repo.get!(Goal, goal_id)
    action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, action_id, 1, "cancel")

    Repo.update_all(
      from(goal_row in Goal, where: goal_row.id == ^goal_id),
      set: [state: "cancelled", event_sequence: 1]
    )

    Repo.update_all(
      from(project in Project, where: project.id == ^goal.project_id),
      set: [status: "archived"]
    )

    assert {:error, :state_conflict} =
             Goals.dispatch_goal_control_command(
               goal_id,
               1,
               action_id,
               task.id,
               "cancel",
               %{},
               "goal-control:#{action_id}:cancel:#{task.id}",
               now: @now
             )

    refute Repo.exists?(from(command in Command, where: command.task_id == ^task.id))
  end

  test "Goal control command transaction rejects calls outside an enclosing transaction" do
    goal_id = Ecto.UUID.generate()
    action_id = Ecto.UUID.generate()
    task_id = Ecto.UUID.generate()
    idempotency_key = "goal-control:#{action_id}:cancel:#{task_id}"

    assert {:error, :transaction_required} =
             Orchestration.create_goal_control_command_in_transaction(
               %{goal_id: goal_id, revision: 1, action_id: action_id, kind: "cancel"},
               %{task_id: task_id, payload: %{}, idempotency_key: idempotency_key},
               now: @now
             )
  end

  test "Goal control dispatch rolls back command and Task state with its outer transaction" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, action_id, 1, "cancel")

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "cancelled", event_sequence: 1]
    )

    assert {:error, :dispatch_rollback} =
             Repo.transaction(fn ->
               assert {:ok, _command, :created} =
                        Goals.dispatch_goal_control_command(
                          goal_id,
                          1,
                          action_id,
                          task.id,
                          "cancel",
                          %{},
                          "goal-control:#{action_id}:cancel:#{task.id}",
                          now: @now
                        )

               Repo.rollback(:dispatch_rollback)
             end)

    assert Repo.get!(Task, task.id).state == "queued"
    refute Repo.exists?(from(command in Command, where: command.task_id == ^task.id))
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

  defp insert_goal_revision(goal_id, revision) do
    %GoalRevision{
      goal_id: goal_id,
      revision: revision,
      objective: "Goal admission amendment",
      non_goals: [],
      acceptance_contract: %{},
      authority_policy: %{},
      execution_policy: execution_policy(2),
      context_manifest: %{},
      reason: "test amendment",
      actor_ref: "operator:test"
    }
    |> Ecto.Changeset.change(inserted_at: @now)
    |> Repo.insert!()
  end

  defp legacy_task_attrs do
    %{goal: "Retry legacy body", agent_profile: "codex", workspace: "primary", input: %{}}
  end

  defp project_key do
    "P" <> Integer.to_string(rem(System.unique_integer([:positive]), 9_999_999))
  end
end
