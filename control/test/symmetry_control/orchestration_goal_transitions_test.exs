defmodule SymmetryControl.OrchestrationGoalTransitionsTest do
  use SymmetryControl.OrchestrationCase, async: false

  alias Oban.Job
  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.GoalEvent
  alias SymmetryControl.Goals.GoalRevision
  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Command
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.RunTransition
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces.ProjectResource
  alias SymmetryControl.Workspaces.WorkItem

  @now ~U[2026-09-09 09:00:00.000000Z]

  test "legacy pause transition facade fails closed for a Goal-owned Run" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("goal-pause-fail-closed")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = goal_claim(run, runtime, @now)

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(run.id, fence, "running", %{}, Ecto.UUID.generate(), now: @now)

    assert {:error, :goal_authority_required} =
             Orchestration.transition(
               run.id,
               fence,
               "paused",
               %{"command_id" => Ecto.UUID.generate()},
               Ecto.UUID.generate(),
               now: @now
             )

    assert task.id == run.task_id
    assert %{state: "running"} = Repo.get!(Run, run.id)
  end

  test "Goal machine retries committed events and transitions but rejects changed or new writes after pause" do
    {_task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("lost-ack-authority")
    assert {:ok, run} = Orchestration.assign_one(now: @now)

    claim_request = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: Ecto.UUID.generate()
    }

    assert {:ok, claimed} = Goals.claim(run.id, claim_request, now: @now)

    fence = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: claimed.claim_id,
      lease_token: claimed.lease_token
    }

    event = %{
      event_id: Ecto.UUID.generate(),
      sequence: 1,
      kind: "message",
      payload: %{"text" => "committed before pause"},
      occurred_at: @now
    }

    assert {:ok, [_]} = Orchestration.append_events(run.id, fence, [event], now: @now)

    transition_id = Ecto.UUID.generate()

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(run.id, fence, "running", %{}, transition_id, now: @now)

    pause_goal!(goal_id)

    assert {:ok, [_]} = Orchestration.append_events(run.id, fence, [event], now: @now)

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(run.id, fence, "running", %{}, transition_id, now: @now)

    assert {:error, :idempotency_conflict} =
             Orchestration.append_events(
               run.id,
               fence,
               [%{event | payload: %{"text" => "changed replay"}}],
               now: @now
             )

    assert {:error, :ownership_lost} =
             Orchestration.append_events(
               run.id,
               fence,
               [%{event | event_id: Ecto.UUID.generate()}],
               now: @now
             )

    assert {:error, :idempotency_conflict} =
             Orchestration.transition(
               run.id,
               fence,
               "running",
               %{"changed" => true},
               transition_id,
               now: @now
             )

    assert {:error, :ownership_lost} =
             Orchestration.transition(
               run.id,
               fence,
               "waiting_for_input",
               %{},
               Ecto.UUID.generate(),
               now: @now
             )

    assert 1 ==
             Repo.aggregate(
               from(event_row in SymmetryControl.Orchestration.RunEvent,
                 where: event_row.run_id == ^run.id
               ),
               :count
             )

    assert %{state: "running"} = Repo.get!(Run, run.id)
  end

  test "Goal pause revokes nonterminal execution writes before its control worker runs" do
    {_task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("post-command-authority")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = goal_claim(run, runtime, @now)

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(run.id, fence, "running", %{}, Ecto.UUID.generate(), now: @now)

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

    assert {:error, :ownership_lost} = Orchestration.renew_lease(run.id, fence, now: @now)

    assert {:error, :ownership_lost} =
             Orchestration.append_events(
               run.id,
               fence,
               [
                 %{
                   event_id: Ecto.UUID.generate(),
                   sequence: 1,
                   kind: "message",
                   payload: %{},
                   occurred_at: @now
                 }
               ],
               now: @now
             )

    assert {:error, :ownership_lost} =
             Orchestration.transition(
               run.id,
               fence,
               "waiting_for_input",
               %{"question" => "Continue?"},
               Ecto.UUID.generate(),
               now: @now
             )

    assert {:ok, %{state: "failed"}} =
             Orchestration.transition(
               run.id,
               fence,
               "failed",
               %{"reason" => "terminal delivery remains historical"},
               Ecto.UUID.generate(),
               now: @now
             )
  end

  test "Goal pause transition rejects a pause command superseded by resume" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("stale-pause-transition")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = goal_claim(run, runtime, @now)

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(run.id, fence, "running", %{}, Ecto.UUID.generate(), now: @now)

    pause_action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, pause_action_id, 1, "pause")

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "paused", event_sequence: 1]
    )

    command = insert_goal_pause_command(task, run, pause_action_id)

    resume_action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, resume_action_id, 2, "resume")

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "active", event_sequence: 2]
    )

    assert {:error, :stale_revision} =
             Goals.apply_goal_pause_transition(
               run.id,
               fence,
               %{"command_id" => command.id},
               Ecto.UUID.generate(),
               now: @now
             )

    assert Repo.get!(Command, command.id).state == "pending"
    assert {:ok, running} = Orchestration.fetch_run(run.id)
    assert running.state == "running"
  end

  test "Goal pause transition rejects a pause command superseded by amend" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("stale-pause-amend-transition")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = goal_claim(run, runtime, @now)

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(run.id, fence, "running", %{}, Ecto.UUID.generate(), now: @now)

    pause_action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, pause_action_id, 1, "pause")

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "paused", event_sequence: 1]
    )

    command = insert_goal_pause_command(task, run, pause_action_id)

    amend_action_id = Ecto.UUID.generate()
    insert_goal_revision(goal_id, 2)
    insert_goal_event(goal_id, amend_action_id, 2, "amend", 2)

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "paused", current_revision: 2, event_sequence: 2]
    )

    assert {:error, :stale_revision} =
             Goals.apply_goal_pause_transition(
               run.id,
               fence,
               %{"command_id" => command.id},
               Ecto.UUID.generate(),
               now: @now
             )

    assert Repo.get!(Command, command.id).state == "pending"
    assert {:ok, running} = Orchestration.fetch_run(run.id)
    assert running.state == "running"
  end

  test "Goal pause transition creates once and replays after Goal advancement" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("goal-pause-replay")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = goal_claim(run, runtime, @now)

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(run.id, fence, "running", %{}, Ecto.UUID.generate(), now: @now)

    action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, action_id, 1, "pause")

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "paused", event_sequence: 1]
    )

    command = insert_goal_pause_command(task, run, action_id)
    transition_id = Ecto.UUID.generate()
    payload = %{"command_id" => command.id}

    assert {:ok, %{state: "paused"} = paused, :created, nil} =
             Goals.apply_goal_pause_transition(run.id, fence, payload, transition_id, now: @now)

    resume_action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, resume_action_id, 2, "resume")

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "active", event_sequence: 2]
    )

    assert {:ok, replayed, :replayed} =
             Goals.apply_goal_pause_transition(run.id, fence, payload, transition_id, now: @now)

    assert replayed.state == paused.state

    assert {:error, :idempotency_conflict} =
             Goals.apply_goal_pause_transition(
               run.id,
               fence,
               Map.put(payload, "generation", run.generation + 1),
               transition_id,
               now: @now
             )

    assert 1 ==
             Repo.aggregate(
               from(transition in RunTransition,
                 where:
                   transition.run_id == ^run.id and
                     transition.transition_id == ^transition_id
               ),
               :count
             )
  end

  test "daemon Goal pause route emits telemetry only for the committed creation" do
    ref =
      :telemetry_test.attach_event_handlers(self(), [
        [:symmetry_control, :orchestration, :run, :transition]
      ])

    on_exit(fn -> :telemetry.detach(ref) end)

    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("goal-pause-controller")
    machine = Repo.get!(SymmetryControl.Orchestration.Machine, runtime.machine_id)
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = goal_claim(run, runtime, @now)

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(run.id, fence, "running", %{}, Ecto.UUID.generate(), now: @now)

    Repo.update_all(
      from(stored in Run, where: stored.id == ^run.id),
      set: [lease_expires_at: DateTime.shift(DateTime.utc_now(), minute: 1)]
    )

    assert_receive {[:symmetry_control, :orchestration, :run, :transition], ^ref, %{count: 1},
                    %{run_id: run_id, state: "running"}}

    assert run_id == run.id

    action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, action_id, 1, "pause")

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "paused", event_sequence: 1]
    )

    command = insert_goal_pause_command(task, run, action_id)
    transition_id = Ecto.UUID.generate()

    body =
      fence
      |> Map.new(fn {key, value} -> {Atom.to_string(key), value} end)
      |> Map.merge(%{"state" => "paused", "payload" => %{"command_id" => command.id}})

    build_conn = fn ->
      Phoenix.ConnTest.build_conn()
      |> Plug.Conn.assign(:machine, machine)
      |> Map.put(:path_params, %{"run_id" => run.id, "transition_id" => transition_id})
      |> Map.put(:body_params, body)
    end

    first = SymmetryControlWeb.DaemonController.transition(build_conn.(), %{})
    assert first.status == 200, first.resp_body
    assert %{"state" => "paused"} = Jason.decode!(first.resp_body)

    assert_receive {[:symmetry_control, :orchestration, :run, :transition], ^ref, %{count: 1},
                    %{run_id: paused_run_id, generation: generation, state: "paused"}}

    assert paused_run_id == run.id
    assert generation == run.generation

    replay = SymmetryControlWeb.DaemonController.transition(build_conn.(), %{})
    assert replay.status == 200
    assert %{"state" => "paused"} = Jason.decode!(replay.resp_body)

    refute_receive {[:symmetry_control, :orchestration, :run, :transition], ^ref, _, _}
  end

  test "Goal pause transition rolls back with an outer transaction" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("goal-pause-rollback")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = goal_claim(run, runtime, @now)

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(run.id, fence, "running", %{}, Ecto.UUID.generate(), now: @now)

    action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, action_id, 1, "pause")

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "paused", event_sequence: 1]
    )

    command = insert_goal_pause_command(task, run, action_id)
    transition_id = Ecto.UUID.generate()

    assert {:error, :later_failure} =
             Repo.transaction(fn ->
               assert {:ok, %{state: "paused"}, :created, nil} =
                        Goals.apply_goal_pause_transition(
                          run.id,
                          fence,
                          %{"command_id" => command.id},
                          transition_id,
                          now: @now
                        )

               Repo.rollback(:later_failure)
             end)

    assert %{state: "running"} = Repo.get!(Run, run.id)

    refute Repo.exists?(
             from(transition in RunTransition,
               where: transition.run_id == ^run.id and transition.transition_id == ^transition_id
             )
           )
  end

  test "Goal pause transition rejects mismatched command and fence identity" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("goal-pause-identity")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = goal_claim(run, runtime, @now)

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(run.id, fence, "running", %{}, Ecto.UUID.generate(), now: @now)

    action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, action_id, 1, "pause")

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "paused", event_sequence: 1]
    )

    {other_task, _other_goal_id} = insert_goal_task(max_run_attempts: 2)
    other_command = insert_goal_pause_command(other_task, run, action_id)

    assert {:error, :state_conflict} =
             Goals.apply_goal_pause_transition(
               run.id,
               fence,
               %{"command_id" => other_command.id},
               Ecto.UUID.generate(),
               now: @now
             )

    Repo.delete!(other_command)
    command = insert_goal_pause_command(task, run, action_id)

    assert {:error, :ownership_lost} =
             Goals.apply_goal_pause_transition(
               run.id,
               fence,
               %{"command_id" => command.id, "run_id" => Ecto.UUID.generate()},
               Ecto.UUID.generate(),
               now: @now
             )

    assert {:error, :ownership_lost} =
             Goals.apply_goal_pause_transition(
               run.id,
               fence,
               %{"command_id" => command.id, "generation" => run.generation + 1},
               Ecto.UUID.generate(),
               now: @now
             )

    assert {:error, :ownership_lost} =
             Goals.apply_goal_pause_transition(
               run.id,
               Map.put(fence, :claim_id, Ecto.UUID.generate()),
               %{"command_id" => command.id},
               Ecto.UUID.generate(),
               now: @now
             )

    Repo.update_all(
      from(stored in Command, where: stored.id == ^command.id),
      set: [run_id: nil, generation: nil]
    )

    assert {:error, :ownership_lost} =
             Goals.apply_goal_pause_transition(
               run.id,
               fence,
               %{"command_id" => command.id},
               Ecto.UUID.generate(),
               now: @now
             )

    Repo.update_all(
      from(stored in Command, where: stored.id == ^command.id),
      set: [run_id: run.id, generation: run.generation + 1]
    )

    assert {:error, :ownership_lost} =
             Goals.apply_goal_pause_transition(
               run.id,
               fence,
               %{"command_id" => command.id},
               Ecto.UUID.generate(),
               now: @now
             )

    assert %{state: "running"} = Repo.get!(Run, run.id)
  end

  test "a draft planning Task assigns, claims, and emits one terminal settlement job by Subject resource" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2, purpose: "plan")
    repository_resource_id = task.input["subject"]["resource_id"]

    runtime =
      enroll_and_register_runtime("planning-task", repository_resource_id: repository_resource_id)

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == task.id
    assert run.runtime_id == runtime.id

    fence = goal_claim(run, runtime, @now)

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(run.id, fence, "running", %{}, Ecto.UUID.generate(), now: @now)

    assert {:ok, %{state: "completed"}} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               %{"summary" => "planning result persisted for Goal settlement"},
               Ecto.UUID.generate(),
               now: @now
             )

    assert [_job] = settlement_jobs(task.id, run.id, run.generation)
  end

  test "Goal cancellation can settle a queued planning Task without a WorkItem" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2, purpose: "plan")
    action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, action_id, 1, "cancel")

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "cancelled", event_sequence: 1]
    )

    assert {:ok, command, :created} =
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

    assert command.state == "applied"
    assert {:ok, receipt} = Goals.settle_unstarted_task(task.id, "cancelled", now: @now)
    assert receipt["settlement"] == "released_without_run"
  end

  test "final-attempt lease expiry retains terminal grace behind one delayed settlement job" do
    {task, _goal} = insert_goal_task(max_run_attempts: 1)
    runtime = enroll_and_register_runtime("final-grace")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = goal_claim(run, runtime, @now)

    expiry = DateTime.shift(@now, second: 30)
    assert %{expired_runs: 1} = Orchestration.expire(now: expiry)

    assert {:ok, %{state: "completed"}} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               %{"summary" => "late but within terminal grace"},
               uuid(2),
               now: DateTime.shift(expiry, second: 1)
             )

    assert {:ok, %{state: "completed", attempt_generation: 1}} = Orchestration.fetch_task(task.id)
    assert {:ok, %{state: "completed", failure: nil}} = Orchestration.fetch_run(run.id)
    assert [_job] = settlement_jobs(task.id, run.id, 1)
  end

  test "terminal transition writes one durable job and notifies only after the committing call" do
    {task, _goal} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("terminal")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = goal_claim(run, runtime, @now)

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(run.id, fence, "running", %{}, uuid(3), now: @now)

    transition_id = uuid(4)
    hook = fn receipt -> send(self(), {:goal_task_terminal, receipt}) end

    assert {:ok, %{state: "completed"}} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               %{"summary" => "done"},
               transition_id,
               now: @now,
               on_goal_task_terminal: hook
             )

    assert_receive {:goal_task_terminal, %{task_id: task_id, run_id: run_id, generation: generation}}

    assert task_id == task.id
    assert run_id == run.id
    assert generation == 1
    assert task.id |> settlement_jobs(run.id, 1) |> length() == 1

    assert {:ok, %{state: "completed"}} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               %{"summary" => "done"},
               transition_id,
               now: @now,
               on_goal_task_terminal: hook
             )

    refute_receive {:goal_task_terminal, _}
    assert task.id |> settlement_jobs(run.id, 1) |> length() == 1

    {rolled_back_task, _goal} = insert_goal_task(max_run_attempts: 2)
    second_item = Repo.get!(WorkItem, rolled_back_task.work_item_id)

    Repo.update_all(
      from(runtime_row in Runtime, where: runtime_row.id == ^runtime.id),
      set: [repository_resource_id: second_item.repository_resource_id]
    )

    assert {:ok, rolled_back_run} = Orchestration.assign_one(now: @now)

    rolled_back_fence =
      goal_claim(rolled_back_run, runtime, @now)

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(
               rolled_back_run.id,
               rolled_back_fence,
               "running",
               %{},
               uuid(5),
               now: @now
             )

    assert {:error, :rollback} =
             Repo.transaction(fn ->
               assert {:ok, _} =
                        Orchestration.transition(
                          rolled_back_run.id,
                          rolled_back_fence,
                          "completed",
                          %{"summary" => "rolled back"},
                          uuid(6),
                          now: @now,
                          on_goal_task_terminal: hook
                        )

               Repo.rollback(:rollback)
             end)

    refute_receive {:goal_task_terminal, _}
    assert task.id |> settlement_jobs(run.id, 1) |> length() == 1
    assert [] == settlement_jobs(rolled_back_task.id, rolled_back_run.id, 1)
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

  defp project_key do
    "P" <> Integer.to_string(rem(System.unique_integer([:positive]), 9_999_999))
  end

  defp uuid(number), do: "00000000-0000-0000-0000-" <> String.pad_leading(Integer.to_string(number), 12, "0")
end
