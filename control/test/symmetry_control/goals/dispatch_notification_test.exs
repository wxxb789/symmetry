defmodule SymmetryControl.Goals.DispatchNotificationTest do
  use ExUnit.Case, async: false

  import Ecto.Query

  alias Ecto.Adapters.SQL.Sandbox
  alias Oban.Job
  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.Workers.{GoalControlWorker, WakeupWorker}
  alias SymmetryControl.Orchestration.{Command, Machine, Run, Runtime, Scheduler, Task}
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.WorkItem

  @moduletag skip: System.get_env("SYMMETRY_GOAL_DISPATCH_E2E") != "1"

  @dispatch_timeout 5_000
  @validation_runtime_id "00000000-0000-4000-8000-000000000001"
  @validation_profile_digest "sha256:" <> String.duplicate("c", 64)

  setup_all do
    previous_orchestration = Application.fetch_env!(:symmetry_control, :orchestration)
    previous_goals = Application.fetch_env!(:symmetry_control, :goals)

    Sandbox.mode(Repo, :auto)

    Application.put_env(
      :symmetry_control,
      :orchestration,
      Keyword.merge(previous_orchestration, scheduler_enabled: true, reaper_enabled: false)
    )

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.merge(previous_goals,
        rollout_enabled: true,
        validation_profiles: validation_profiles()
      )
    )

    on_exit(fn ->
      Application.put_env(:symmetry_control, :orchestration, previous_orchestration)
      Application.put_env(:symmetry_control, :goals, previous_goals)
      Sandbox.mode(Repo, :manual)
    end)

    assert_isolated_empty_database!()
    assert is_pid(Process.whereis(Scheduler))

    :ok
  end

  test "committed draft planning wakes the supervised scheduler and publishes work_available" do
    fixture = goal_fixture()
    %{machine: machine, runtime: runtime} = runtime_fixture(fixture.repository.id)
    subscribe_to_daemon(machine.id)

    assert {:ok, receipt, :created} =
             Goals.command(
               fixture.goal.id,
               request_plan_command(fixture),
               "operator:test",
               command_opts()
             )

    task_id = receipt.response["task"]["id"]

    assert_receive {:work_available, %{runtime_id: runtime_id}}, @dispatch_timeout
    assert runtime_id == runtime.id
    assert Repo.get!(Task, task_id).state == "assigned"
    assert Repo.aggregate(from(run in Run, where: run.task_id == ^task_id), :count) == 1
  end

  test "committed manual admission wakes the supervised scheduler and publishes work_available" do
    fixture = active_goal_fixture()
    %{machine: machine, runtime: runtime} = runtime_fixture(fixture.repository.id)
    subscribe_to_daemon(machine.id)

    assert {:ok, receipt, :created} =
             command_current(fixture.goal.id, "admit_task", admission_payload(fixture.item))

    task_id = receipt.response["task"]["id"]

    assert_receive {:work_available, %{runtime_id: runtime_id}}, @dispatch_timeout
    assert runtime_id == runtime.id
    assert Repo.get!(Task, task_id).state == "assigned"
    assert Repo.aggregate(from(run in Run, where: run.task_id == ^task_id), :count) == 1
  end

  test "an outer transaction suppresses fast wake but commit retains the task and immediate wakeup" do
    fixture = goal_fixture()
    %{machine: machine} = runtime_fixture(fixture.repository.id)
    subscribe_to_daemon(machine.id)

    command = request_plan_command(fixture)

    assert {:ok, {task_id, receipt}} =
             Repo.transaction(fn ->
               assert {:ok, receipt, :created} =
                        Goals.command(fixture.goal.id, command, "operator:test", command_opts())

               task_id = receipt.response["task"]["id"]
               assert Repo.get!(Task, task_id).state == "queued"
               assert wakeup_job_count(fixture.goal.id) == 1
               refute_receive {:work_available, _}, 100
               {task_id, receipt}
             end)

    assert receipt.response["task"]["id"] == task_id
    refute_receive {:work_available, _}, 100
    assert Repo.get!(Task, task_id).state == "queued"

    job = hd(wakeup_jobs(fixture.goal.id))
    assert :ok = WakeupWorker.perform(job)

    assert_receive {:work_available, _}, @dispatch_timeout
    assert Repo.get!(Task, task_id).state == "assigned"
  end

  test "rollback leaves no Goal Task, event, wakeup, or scheduler hint" do
    fixture = goal_fixture()
    %{machine: machine} = runtime_fixture(fixture.repository.id)
    subscribe_to_daemon(machine.id)

    assert {:error, :dispatch_rollback} =
             Repo.transaction(fn ->
               assert {:ok, _receipt, :created} =
                        Goals.command(
                          fixture.goal.id,
                          request_plan_command(fixture),
                          "operator:test",
                          command_opts()
                        )

               refute_receive {:work_available, _}, 100
               Repo.rollback(:dispatch_rollback)
             end)

    refute Repo.exists?(from(task in Task, where: task.goal_id == ^fixture.goal.id))

    refute Repo.exists?(
             from(event in SymmetryControl.Goals.GoalEvent,
               where: event.goal_id == ^fixture.goal.id and event.kind == "request_plan"
             )
           )

    assert wakeup_job_count(fixture.goal.id) == 0
    refute_receive {:work_available, _}, 100
  end

  test "exact Goal command replay returns the same receipt and does not add a wakeup" do
    with_scheduler_disabled(fn ->
      fixture = goal_fixture()
      command = request_plan_command(fixture)
      opts = command_opts()

      assert {:ok, first, :created} =
               Goals.command(fixture.goal.id, command, "operator:test", opts)

      task_id = first.response["task"]["id"]
      jobs_before = wakeup_job_count(fixture.goal.id)

      assert {:ok, replayed, :replayed} =
               Goals.command(fixture.goal.id, command, "operator:test", opts)

      assert replayed == first
      assert replayed.response["task"]["id"] == task_id

      assert Repo.aggregate(from(task in Task, where: task.goal_id == ^fixture.goal.id), :count) ==
               1

      assert wakeup_job_count(fixture.goal.id) == jobs_before
    end)
  end

  test "a future same-Goal wakeup does not coalesce the immediate wakeup" do
    with_scheduler_disabled(fn ->
      fixture = active_goal_fixture()
      future = DateTime.add(now(), 60, :second)
      jobs_before = wakeup_job_count(fixture.goal.id)

      assert {:ok, _receipt, :created} =
               Goals.command(
                 fixture.goal.id,
                 command(
                   fixture.goal,
                   "admit_task",
                   admission_payload(fixture.item),
                   Ecto.UUID.generate()
                 ),
                 "operator:test",
                 Keyword.put(command_opts(), :settlement_next_wake_at, future)
               )

      jobs = wakeup_jobs(fixture.goal.id)
      assert length(jobs) == jobs_before + 2

      assert Enum.any?(jobs, fn job ->
               is_struct(job.scheduled_at, DateTime) and
                 DateTime.compare(job.scheduled_at, future) in [:eq, :gt]
             end)
    end)
  end

  test "running Goal cancellation publishes command_available only for the committed pending command" do
    fixture = admitted_task_fixture()
    %{machine: machine, runtime: runtime} = runtime_fixture(fixture.repository.id)
    {_task, run} = make_running!(fixture.task, runtime)
    subscribe_to_daemon(machine.id)

    assert {:ok, cancelled, :created} =
             command_current(fixture.goal.id, "cancel", %{reason: "Stop this run."})

    action_id = cancelled.response["control_action_id"]
    refute_receive {:command_available, _}, 100

    assert :ok =
             GoalControlWorker.perform(
               control_job(fixture.goal.id, fixture.goal.current_revision, action_id)
             )

    command_row =
      Repo.one!(
        from(command in Command,
          where: command.task_id == ^fixture.task.id,
          order_by: [desc: command.inserted_at, desc: command.id]
        )
      )

    assert command_row.state == "pending"
    assert command_row.run_id == run.id

    assert_receive {:command_available, %{runtime_id: runtime_id, command_id: command_id}},
                   @dispatch_timeout

    assert runtime_id == runtime.id
    assert command_id == command_row.id
  end

  test "an outer transaction suppresses a Goal control hint before its command is committed" do
    fixture = admitted_task_fixture()
    %{machine: machine, runtime: runtime} = runtime_fixture(fixture.repository.id)
    {_task, run} = make_running!(fixture.task, runtime)
    subscribe_to_daemon(machine.id)

    assert {:ok, cancelled, :created} =
             command_current(fixture.goal.id, "cancel", %{reason: "Stop transactionally."})

    action_id = cancelled.response["control_action_id"]

    assert {:ok, :outer_complete} =
             Repo.transaction(fn ->
               assert :ok =
                        GoalControlWorker.perform(
                          control_job(fixture.goal.id, fixture.goal.current_revision, action_id)
                        )

               refute_receive {:command_available, _}, 100
               :outer_complete
             end)

    command_row =
      Repo.one!(
        from(command in Command,
          where: command.task_id == ^fixture.task.id,
          order_by: [desc: command.inserted_at, desc: command.id]
        )
      )

    assert command_row.state == "pending"
    assert command_row.run_id == run.id
  end

  test "runless applied Goal cancellation still settles without command_available" do
    fixture = admitted_task_fixture()
    %{machine: machine} = runtime_fixture(fixture.repository.id)
    subscribe_to_daemon(machine.id)

    assert {:ok, cancelled, :created} =
             command_current(fixture.goal.id, "cancel", %{reason: "Stop before start."})

    action_id = cancelled.response["control_action_id"]

    assert :ok =
             GoalControlWorker.perform(
               control_job(fixture.goal.id, fixture.goal.current_revision, action_id)
             )

    command_row =
      Repo.one!(
        from(command in Command,
          where: command.task_id == ^fixture.task.id,
          order_by: [desc: command.inserted_at, desc: command.id]
        )
      )

    assert command_row.state == "applied"
    assert command_row.run_id == nil

    assert Repo.one!(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.task_id == ^fixture.task.id,
               select: reservation.state
             )
           ) == "released"

    refute_receive {:command_available, _}, 100
  end

  test "superseded and invalid Goal control jobs do not publish command_available" do
    fixture = admitted_task_fixture()
    %{machine: machine} = runtime_fixture(fixture.repository.id)
    subscribe_to_daemon(machine.id)

    assert {:ok, paused, :created} =
             command_current(fixture.goal.id, "pause", %{reason: "Pause before supersession."})

    action_id = paused.response["control_action_id"]

    assert :ok =
             GoalControlWorker.perform(
               control_job(fixture.goal.id, fixture.goal.current_revision + 1, action_id)
             )

    assert {:cancel, :invalid_goal_control_job} = GoalControlWorker.perform(%Job{args: %{}})
    refute Repo.exists?(from(command in Command, where: command.task_id == ^fixture.task.id))
    refute_receive {:command_available, _}, 100
  end

  defp assert_isolated_empty_database! do
    database = System.get_env("POSTGRES_DB")
    configured_database = Keyword.get(Repo.config(), :database)

    unless is_binary(database) and configured_database == database and
             Regex.match?(~r/^symmetry_dispatch_e2e_[0-9a-f]{32}$/, database) do
      flunk(
        "goal dispatch E2E requires POSTGRES_DB and Repo database " <>
          "to match symmetry_dispatch_e2e_<32 lowercase hex>"
      )
    end

    %{rows: [[current_database]]} = Repo.query!("SELECT current_database()")

    unless current_database == database do
      flunk("goal dispatch E2E connected to #{current_database}, expected #{database}")
    end

    table_names =
      Repo.query!("""
      SELECT table_name
      FROM information_schema.tables
      WHERE table_schema = 'public'
        AND table_type = 'BASE TABLE'
        AND table_name <> 'schema_migrations'
      ORDER BY table_name
      """).rows
      |> Enum.map(&List.first/1)

    nonempty_tables = Enum.filter(table_names, &table_nonempty?/1)

    unless nonempty_tables == [] do
      flunk(
        "goal dispatch E2E requires an empty database; found rows in #{inspect(nonempty_tables)}"
      )
    end
  end

  defp table_nonempty?(table_name) do
    query = "SELECT EXISTS (SELECT 1 FROM public.#{quote_identifier(table_name)} LIMIT 1)"
    %{rows: [[nonempty?]]} = Repo.query!(query)
    nonempty?
  end

  defp quote_identifier(identifier), do: "\"" <> String.replace(identifier, "\"", "\"\"") <> "\""

  defp subscribe_to_daemon(machine_id) do
    assert :ok = Phoenix.PubSub.subscribe(SymmetryControl.PubSub, "daemon:" <> machine_id)
  end

  defp command_opts do
    [now: now(), rollout_enabled: true, validation_profiles: validation_profiles()]
  end

  defp with_scheduler_disabled(fun) when is_function(fun, 0) do
    orchestration = Application.fetch_env!(:symmetry_control, :orchestration)

    Application.put_env(
      :symmetry_control,
      :orchestration,
      Keyword.put(orchestration, :scheduler_enabled, false)
    )

    try do
      fun.()
    after
      Application.put_env(:symmetry_control, :orchestration, orchestration)
    end
  end

  defp now, do: DateTime.utc_now() |> DateTime.truncate(:microsecond)

  defp request_plan_command(fixture) do
    command(fixture.goal, "request_plan", %{
      model_profile: "codex",
      repository_resource_id: fixture.repository.id,
      subject: baseline_subject(fixture.repository.id).subject,
      session_mode: "fresh",
      requested_session_id: nil
    })
  end

  defp command_current(goal_id, kind, payload, opts \\ []) do
    {:ok, goal} = Goals.fetch_goal(goal_id)

    Goals.command(
      goal_id,
      command(goal, kind, payload),
      "operator:test",
      Keyword.merge(command_opts(), opts)
    )
  end

  defp command(goal, kind, payload, mutation_id \\ Ecto.UUID.generate()) do
    %{
      schema_version: "symmetry.goal_command.v1",
      mutation_id: mutation_id,
      expected_version: goal.version,
      expected_revision: goal.current_revision,
      kind: kind,
      payload: payload
    }
  end

  defp goal_fixture(policy_overrides \\ %{}) do
    project = project_fixture()
    repository = repository_fixture(project)

    policy =
      %{
        "max_parallel_tasks" => 1,
        "max_task_admissions" => 4,
        "allowed_resource_ids" => [repository.id],
        "allowed_model_profiles" => ["codex"]
      }
      |> Map.merge(policy_overrides)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(policy), "operator:test", command_opts())

    %{project: project, repository: repository, goal: created.goal}
  end

  defp active_goal_fixture do
    with_scheduler_disabled(fn ->
      fixture = goal_fixture()

      proposal = %{
        schema_version: "symmetry.plan.v1",
        proposal_id: Ecto.UUID.generate(),
        goal_id: fixture.goal.id,
        expected_revision: 1,
        items: [
          %{
            key: "dispatch-item",
            title: "Dispatch item",
            description: "Bounded dispatch test work.",
            required: true,
            integration: true,
            repository_resource_id: fixture.repository.id,
            acceptance: check_contract(),
            depends_on_keys: [],
            model_profile: "codex",
            change_target: nil,
            baseline: baseline_subject(fixture.repository.id)
          }
        ]
      }

      assert {:ok, decision, :created} =
               command_current(fixture.goal.id, "request_decision", %{
                 kind: "plan",
                 work_item_id: nil,
                 subject_hash: nil,
                 proposal: proposal
               })

      decision_id = decision.response["decision"]["id"]
      decision_version = Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).lock_version

      assert {:ok, _resolved, :created} =
               command_current(fixture.goal.id, "resolve_decision", %{
                 decision_id: decision_id,
                 expected_decision_version: decision_version,
                 option_id: "accept"
               })

      assert {:ok, _planned, :created} =
               command_current(
                 fixture.goal.id,
                 "accept_plan",
                 %{
                   proposal: proposal,
                   proposal_hash:
                     "sha256:" <>
                       Base.encode16(SymmetryControl.RequestHash.canonical(proposal),
                         case: :lower
                       ),
                   decision_id: decision_id
                 }
               )

      item =
        Repo.one!(
          from(item in WorkItem,
            where: item.goal_id == ^fixture.goal.id,
            order_by: [asc: item.id]
          )
        )

      assert {:ok, _active, :created} =
               command_current(fixture.goal.id, "activate", %{approved_revision: 1})

      {:ok, goal} = Goals.fetch_goal(fixture.goal.id)
      Map.merge(fixture, %{goal: goal, item: item})
    end)
  end

  defp admitted_task_fixture do
    with_scheduler_disabled(fn ->
      fixture = active_goal_fixture()

      assert {:ok, admitted, :created} =
               command_current(fixture.goal.id, "admit_task", admission_payload(fixture.item))

      Map.put(fixture, :task, Repo.get!(Task, admitted.response["task"]["id"]))
    end)
  end

  defp make_running!(task, runtime) do
    current = now()
    claim_id = Ecto.UUID.generate()
    lease_token = Ecto.UUID.generate()

    Repo.update_all(
      from(row in Task, where: row.id == ^task.id),
      set: [state: "running", current_generation: 1, updated_at: current]
    )

    run =
      %Run{}
      |> Run.changeset(%{
        task_id: task.id,
        runtime_id: runtime.id,
        generation: 1,
        state: "running",
        claimed_runtime_epoch: runtime.connection_epoch,
        claim_id: claim_id,
        lease_token: lease_token,
        assigned_at: current,
        assignment_expires_at: DateTime.add(current, 60, :second),
        claimed_at: current,
        lease_expires_at: DateTime.add(current, 3_600, :second)
      })
      |> Repo.insert!()

    {Repo.get!(Task, task.id), run}
  end

  defp runtime_fixture(repository_resource_id) do
    machine =
      %Machine{}
      |> Machine.changeset(%{
        name: "dispatch-machine-#{System.unique_integer([:positive])}",
        token_digest: :crypto.strong_rand_bytes(32)
      })
      |> Repo.insert!()

    runtime =
      %Runtime{}
      |> Runtime.changeset(%{
        machine_id: machine.id,
        runtime_key: "dispatch-runtime-#{System.unique_integer([:positive])}",
        name: "Dispatch runtime",
        daemon_instance_id: Ecto.UUID.generate(),
        connection_epoch: 1,
        capacity: 1,
        agent_profile: "codex",
        workspace: "primary",
        repository_resource_id: repository_resource_id,
        capabilities: native_capabilities(),
        harness_kind: "codex",
        harness_version: "1.0.0",
        adapter_version: "1.0.0",
        adapter_protocol_version: 1,
        status: "online",
        last_heartbeat_at: now(),
        heartbeat_interval_ms: 5_000
      })
      |> Repo.insert!()

    %{machine: machine, runtime: runtime}
  end

  defp native_capabilities do
    %{
      "adapter" => %{
        "kind" => "codex",
        "native_version" => "1.0.0",
        "implementation_version" => "1.0.0",
        "protocol_version" => 1,
        "operations" => %{
          "start" => true,
          "events" => true,
          "cancel" => true,
          "pause" => "unsupported",
          "resume" => false,
          "handoff" => false,
          "guidance" => "unsupported",
          "approval_response" => false,
          "usage" => "unknown",
          "hard_cost_limit" => false
        }
      }
    }
  end

  defp admission_payload(item) do
    %{
      work_item_id: item.id,
      purpose: "implement",
      model_profile: "codex",
      session_mode: "fresh",
      requested_session_id: nil,
      validation_of_task_id: nil
    }
  end

  defp control_job(goal_id, revision, action_id) do
    %Job{args: %{"goal_id" => goal_id, "revision" => revision, "action_id" => action_id}}
  end

  defp wakeup_jobs(goal_id) do
    Repo.all(
      from(job in Oban.Job,
        where:
          job.worker == "SymmetryControl.Goals.Workers.WakeupWorker" and
            fragment("? ->> 'goal_id' = ?", job.args, ^goal_id),
        order_by: [asc: job.inserted_at, asc: job.id]
      )
    )
  end

  defp wakeup_job_count(goal_id), do: length(wakeup_jobs(goal_id))

  defp project_fixture do
    {:ok, project} =
      Workspaces.create_project(%{
        name: "Dispatch #{System.unique_integer([:positive])}",
        key: "D#{System.unique_integer([:positive])}",
        default_agent_profile: "codex",
        default_workspace: "primary"
      })

    project
  end

  defp repository_fixture(project) do
    {:ok, repository} =
      Workspaces.create_resource(project.id, %{
        kind: "repository",
        name: "Dispatch repository #{System.unique_integer([:positive])}"
      })

    repository
  end

  defp goal_attrs(execution_policy) do
    %{
      schema_version: "symmetry.goal_create.v1",
      title: "Dispatch goal #{System.unique_integer([:positive])}",
      mutation_id: Ecto.UUID.generate(),
      initial_revision: %{
        objective: "Verify durable Goal dispatch.",
        non_goals: ["No autonomous publication"],
        acceptance_contract: check_contract(),
        authority_policy: %{
          "operator_required_for_scope_change" => true,
          "operator_required_for_completion" => true,
          "publication_allowed" => false,
          "allowed_actions" => []
        },
        execution_policy:
          %{
            "automatic_execution" => false,
            "max_parallel_tasks" => 1,
            "max_task_admissions" => 4,
            "max_run_attempts_per_task" => 2,
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
          |> Map.merge(execution_policy),
        context_manifest: %{
          "byte_budget" => 32_768,
          "required_source_kinds" => ["repository"],
          "include_advisory_recall" => false
        },
        reason: "Dispatch test fixture"
      }
    }
  end

  defp check_contract do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "An independent check must pass.",
      "predicates" => [%{"id" => "check", "kind" => "check", "validator_profile" => "test"}]
    }
  end

  defp baseline_subject(resource_id) do
    %{
      kind: "subject",
      subject: %{
        "resource_id" => resource_id,
        "commit" => String.duplicate("a", 40),
        "tree_digest" => "sha256:" <> String.duplicate("b", 64)
      }
    }
  end

  defp validation_profiles do
    [
      test: [
        kind: :check,
        profile_digest: @validation_profile_digest,
        enabled: true,
        allowed_runtime_ids: [@validation_runtime_id]
      ]
    ]
  end
end
