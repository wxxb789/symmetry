defmodule SymmetryControl.OrchestrationGoalAdmissionTest do
  use SymmetryControl.DataCase, async: false

  alias Oban.Job
  alias SymmetryControl.{Goals, RequestHash}

  alias SymmetryControl.Goals.{
    Goal,
    GoalBudgetReservation,
    GoalEvent,
    HarnessSession
  }

  alias SymmetryControl.Goals.Workers.SettleTaskWorker
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Command, Run, Runtime, Task}
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.{ProjectResource, WorkItem}

  @now ~U[2026-09-09 09:00:00.000000Z]

  test "legacy submit_task preserves its request hash and creates no Goal settlement" do
    attrs = %{
      goal: "Legacy task",
      agent_profile: "codex",
      workspace: "primary",
      input: %{}
    }

    assert {:ok, task, :created} =
             Orchestration.submit_task(attrs, Ecto.UUID.generate(), now: @now)

    assert task.goal_id == nil
    assert task.request_hash == RequestHash.legacy(Map.put(attrs, :required_capabilities, %{}))
    assert task.request_hash_version == 1
    assert [] == Repo.all(Job)
  end

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

  test "legacy runtime registration replays without adapter metadata" do
    machine = enroll_machine("legacy-runtime")
    specification = runtime_spec("legacy-runtime")
    daemon_instance_id = Ecto.UUID.generate()

    assert {:ok, [first]} =
             Orchestration.register_runtimes(machine.id, daemon_instance_id, [specification],
               now: @now
             )

    assert first.harness_kind == nil
    assert first.harness_version == nil
    assert first.adapter_version == nil
    assert first.adapter_protocol_version == nil

    assert {:ok, [replayed]} =
             Orchestration.register_runtimes(machine.id, daemon_instance_id, [specification],
               now: @now
             )

    assert replayed.id == first.id
    assert replayed.connection_epoch == first.connection_epoch
    assert replayed.harness_kind == nil
  end

  test "disabled Goal rollout accepts legacy registration without contracts but rejects adapter metadata" do
    previous_contracts = Application.fetch_env!(:symmetry_control, :contracts)
    previous_goals = Application.fetch_env!(:symmetry_control, :goals)

    missing_contracts_dir =
      Path.join(
        System.tmp_dir!(),
        "symmetry-contracts-missing-#{System.unique_integer([:positive])}"
      )

    Application.put_env(:symmetry_control, :contracts, directory: missing_contracts_dir)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(previous_goals, :rollout_enabled, false)
    )

    on_exit(fn ->
      Application.put_env(:symmetry_control, :contracts, previous_contracts)
      Application.put_env(:symmetry_control, :goals, previous_goals)
    end)

    legacy_machine = enroll_machine("contractless-legacy-runtime")

    assert {:ok, [legacy_runtime]} =
             Orchestration.register_runtimes(
               legacy_machine.id,
               Ecto.UUID.generate(),
               [runtime_spec("contractless-legacy-runtime")],
               now: @now
             )

    assert legacy_runtime.capabilities == %{}

    unknown_capability_machine = enroll_machine("contractless-unknown-capability")

    assert {:error, :invalid_request} =
             Orchestration.register_runtimes(
               unknown_capability_machine.id,
               Ecto.UUID.generate(),
               [
                 runtime_spec("contractless-unknown-capability", %{
                   capabilities: %{"future_capability" => true}
                 })
               ],
               now: @now
             )

    native_machine = enroll_machine("contractless-native-runtime")

    native_specification =
      runtime_spec("contractless-native-runtime", %{
        harness_kind: "codex",
        harness_version: "0.153.4",
        adapter_version: "symmetry-codex-1",
        adapter_protocol_version: 1,
        capabilities: %{
          "structured_input" => true,
          "interactive" => true,
          "adapter" => native_adapter("codex", "0.153.4", "symmetry-codex-1", 1)
        }
      })

    assert {:error, :invalid_request} =
             Orchestration.register_runtimes(
               native_machine.id,
               Ecto.UUID.generate(),
               [native_specification],
               now: @now
             )
  end

  test "legacy generic runtime accepts supervisory controls alongside metadata" do
    machine = enroll_machine("generic-supervisory")
    daemon_instance_id = Ecto.UUID.generate()

    specification =
      runtime_spec("generic-supervisory", %{
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

    assert {:ok, [registered]} =
             Orchestration.register_runtimes(machine.id, daemon_instance_id, [specification],
               now: @now
             )

    assert {:ok, [replayed]} =
             Orchestration.register_runtimes(machine.id, daemon_instance_id, [specification],
               now: @now
             )

    assert replayed.id == registered.id
    assert replayed.connection_epoch == registered.connection_epoch
    assert replayed.capabilities["supervisory_control"] == true
  end

  test "generic and native registrations persist additive adapter metadata" do
    generic_machine = enroll_machine("generic-runtime")

    generic =
      runtime_spec("generic-runtime", %{
        harness_kind: "generic",
        harness_version: "generic-v1",
        adapter_version: "symmetry-generic-1",
        adapter_protocol_version: 1
      })

    assert {:ok, [generic_runtime]} =
             Orchestration.register_runtimes(
               generic_machine.id,
               Ecto.UUID.generate(),
               [generic],
               now: @now
             )

    assert generic_runtime.harness_kind == "generic"
    assert generic_runtime.harness_version == "generic-v1"
    assert generic_runtime.adapter_version == "symmetry-generic-1"
    assert generic_runtime.adapter_protocol_version == 1
    assert generic_runtime.capabilities == %{}

    generic_runtime = Repo.get!(Runtime, generic_runtime.id)
    assert generic_runtime.harness_kind == "generic"
    assert generic_runtime.harness_version == "generic-v1"
    assert generic_runtime.adapter_version == "symmetry-generic-1"
    assert generic_runtime.adapter_protocol_version == 1

    metadata_only_machine = enroll_machine("native-metadata-only")

    metadata_only_native =
      runtime_spec("native-metadata-only", %{
        harness_kind: "pi",
        harness_version: "0.51.0",
        adapter_version: "symmetry-pi-1",
        adapter_protocol_version: 1
      })

    assert {:ok, [metadata_only_runtime]} =
             Orchestration.register_runtimes(
               metadata_only_machine.id,
               Ecto.UUID.generate(),
               [metadata_only_native],
               now: @now
             )

    assert metadata_only_runtime.harness_kind == "pi"
    assert metadata_only_runtime.harness_version == "0.51.0"
    assert metadata_only_runtime.capabilities == %{}

    native_machine = enroll_machine("native-runtime")
    adapter = native_adapter("codex", "0.153.4", "symmetry-codex-1", 1)

    native =
      runtime_spec("native-runtime", %{
        harness_kind: "codex",
        harness_version: "0.153.4",
        adapter_version: "symmetry-codex-1",
        adapter_protocol_version: 1,
        capabilities: %{
          "structured_input" => true,
          "interactive" => true,
          "adapter" => adapter
        }
      })

    assert {:ok, [native_runtime]} =
             Orchestration.register_runtimes(
               native_machine.id,
               Ecto.UUID.generate(),
               [native],
               now: @now
             )

    assert native_runtime.harness_kind == "codex"
    assert native_runtime.harness_version == "0.153.4"
    assert native_runtime.adapter_version == "symmetry-codex-1"
    assert native_runtime.adapter_protocol_version == 1
    assert native_runtime.capabilities["adapter"] == adapter

    native_runtime = Repo.get!(Runtime, native_runtime.id)
    assert native_runtime.harness_kind == "codex"
    assert native_runtime.harness_version == "0.153.4"
    assert native_runtime.adapter_version == "symmetry-codex-1"
    assert native_runtime.adapter_protocol_version == 1
    assert native_runtime.capabilities["adapter"] == adapter
  end

  test "runtime registration rejects incomplete or incompatible adapter declarations" do
    machine = enroll_machine("invalid-adapter")

    cases = [
      runtime_spec("partial", %{harness_kind: "codex"}),
      runtime_spec("unbound-adapter", %{
        capabilities: %{"adapter" => native_adapter("codex", "0.153.4", "symmetry-codex-1", 1)}
      }),
      runtime_spec("mismatched-kind", %{
        harness_kind: "codex",
        harness_version: "0.153.4",
        adapter_version: "symmetry-codex-1",
        adapter_protocol_version: 1,
        capabilities: %{"adapter" => native_adapter("pi", "0.153.4", "symmetry-codex-1", 1)}
      }),
      runtime_spec("unknown-operation", %{
        harness_kind: "codex",
        harness_version: "0.153.4",
        adapter_version: "symmetry-codex-1",
        adapter_protocol_version: 1,
        capabilities: %{
          "adapter" =>
            put_in(
              native_adapter("codex", "0.153.4", "symmetry-codex-1", 1),
              ["operations", "guidance"],
              "unsupported-value"
            )
        }
      }),
      runtime_spec("extra-operation", %{
        harness_kind: "codex",
        harness_version: "0.153.4",
        adapter_version: "symmetry-codex-1",
        adapter_protocol_version: 1,
        capabilities: %{
          "adapter" =>
            update_in(
              native_adapter("codex", "0.153.4", "symmetry-codex-1", 1),
              ["operations"],
              &Map.put(&1, "future_operation", false)
            )
        }
      }),
      runtime_spec("extra-adapter-field", %{
        harness_kind: "codex",
        harness_version: "0.153.4",
        adapter_version: "symmetry-codex-1",
        adapter_protocol_version: 1,
        capabilities: %{
          "adapter" =>
            Map.put(
              native_adapter("codex", "0.153.4", "symmetry-codex-1", 1),
              "future_field",
              false
            )
        }
      }),
      runtime_spec("incompatible-supervision", %{
        harness_kind: "codex",
        harness_version: "0.153.4",
        adapter_version: "symmetry-codex-1",
        adapter_protocol_version: 1,
        capabilities: %{
          "structured_input" => true,
          "interactive" => true,
          "supervisory_control" => true,
          "adapter" =>
            put_in(
              native_adapter("codex", "0.153.4", "symmetry-codex-1", 1),
              ["operations", "pause"],
              "unsupported"
            )
        }
      }),
      runtime_spec("generic-overclaim", %{
        harness_kind: "generic",
        harness_version: "generic-v1",
        adapter_version: "symmetry-generic-1",
        adapter_protocol_version: 1,
        capabilities: %{
          "adapter" =>
            put_in(
              native_adapter("generic", "generic-v1", "symmetry-generic-1", 1),
              ["operations", "resume"],
              true
            )
        }
      })
    ]

    for specification <- cases do
      assert {:error, :invalid_request} =
               Orchestration.register_runtimes(
                 machine.id,
                 Ecto.UUID.generate(),
                 [specification],
                 now: @now
               )
    end
  end

  test "native registrations cannot advertise an unimplemented safe pause" do
    machine = enroll_machine("safe-pause")

    adapter =
      native_adapter("codex", "0.153.4", "symmetry-codex-1", 1)
      |> put_in(["operations", "resume"], true)
      |> put_in(["operations", "pause"], "safe_boundary")

    specification =
      runtime_spec("safe-pause", %{
        harness_kind: "codex",
        harness_version: "0.153.4",
        adapter_version: "symmetry-codex-1",
        adapter_protocol_version: 1,
        capabilities: %{"adapter" => adapter}
      })

    assert {:error, :invalid_request} =
             Orchestration.register_runtimes(machine.id, Ecto.UUID.generate(), [specification],
               now: @now
             )
  end

  test "paused Goal tasks are not assigned until the Goal resumes, without blocking legacy tasks" do
    {goal_task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = register_runtime("goal-pause")

    Repo.update_all(from(goal in Goal, where: goal.id == ^goal_id), set: [state: "paused"])

    assert {:ok, legacy_task, :created} =
             Orchestration.submit_task(legacy_task_attrs(), "legacy-while-goal-paused", now: @now)

    assert {:ok, legacy_run} = Orchestration.assign_one(now: @now)
    assert legacy_run.task_id == legacy_task.id

    resumed_at = DateTime.add(@now, 31, :second)
    assert %{expired_runs: 1} = Orchestration.expire(now: DateTime.add(@now, 30, :second))

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

    disallowed = register_runtime("goal-disallowed")
    allowed = register_runtime("goal-allowed")

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
             Orchestration.claim(
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

  test "Goal assignment deterministically chooses one eligible native runtime" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    first = register_runtime("goal-runtime-first")
    second = register_runtime("goal-runtime-second")
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
      register_runtime("goal-resource-mismatch", repository_resource_id: other_repository.id)

    matching = register_runtime("goal-resource-match")

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.runtime_id == matching.id
  end

  test "strict-budget Goal assignment and new claim require native hard cost limit support" do
    {task, _goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        execution_policy: Map.put(execution_policy(2), "budget_mode", "strict")
      )

    _without_hard_limit = register_runtime("strict-budget-without-hard-limit")
    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)

    hard_limit_runtime = register_runtime("strict-budget-with-hard-limit", hard_cost_limit?: true)

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
             Orchestration.claim(
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

    assert {:ok, claimed} = Orchestration.claim(run.id, claim_request, now: @now)

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

    assert {:ok, replayed} = Orchestration.claim(run.id, claim_request, now: @now)
    assert replayed.lease_token == claimed.lease_token
  end

  test "runtime re-registration permits a same-affinity refresh but rejects a change during a Goal run" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = register_runtime("active-affinity-registration")
    assert {:ok, _run} = Orchestration.assign_one(now: @now)
    item = Repo.get!(WorkItem, task.work_item_id)

    assert {:ok, [refreshed]} =
             Orchestration.register_runtimes(
               runtime.machine_id,
               runtime.daemon_instance_id,
               [runtime_registration_spec(runtime)],
               now: DateTime.add(@now, 1, :second)
             )

    assert refreshed.repository_resource_id == item.repository_resource_id

    assert {:ok, other_repository} =
             Workspaces.create_resource(item.project_id, %{
               kind: "repository",
               name: "Active runtime replacement #{System.unique_integer([:positive])}"
             })

    assert {:error, :state_conflict} =
             Orchestration.register_runtimes(
               runtime.machine_id,
               runtime.daemon_instance_id,
               [
                 runtime_registration_spec(runtime, %{repository_resource_id: other_repository.id})
               ],
               now: DateTime.add(@now, 2, :second)
             )

    assert Repo.get!(Runtime, runtime.id).repository_resource_id == item.repository_resource_id
  end

  test "runtime re-registration rejects an affinity change while a harness session is retained" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = register_runtime("retained-affinity-registration", resume?: true)
    {_task, session} = bind_retained_session(task, runtime)
    runtime = Repo.get!(Runtime, runtime.id)
    item = Repo.get!(WorkItem, task.work_item_id)

    assert {:ok, other_repository} =
             Workspaces.create_resource(item.project_id, %{
               kind: "repository",
               name: "Retained runtime replacement #{System.unique_integer([:positive])}"
             })

    assert {:error, :state_conflict} =
             Orchestration.register_runtimes(
               runtime.machine_id,
               runtime.daemon_instance_id,
               [
                 runtime_registration_spec(runtime, %{repository_resource_id: other_repository.id})
               ],
               now: @now
             )

    assert Repo.get!(Runtime, runtime.id).repository_resource_id == session.repository_resource_id
    assert session.repository_resource_id == Repo.get!(WorkItem, item.id).repository_resource_id
  end

  test "pre-affinity native runtimes remain registerable but are ineligible for Goal work" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    machine = enroll_machine("unbound-native")

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               Ecto.UUID.generate(),
               [
                 runtime_spec("unbound-native", %{
                   harness_kind: "codex",
                   harness_version: "0.153.4",
                   adapter_version: "symmetry-codex-1",
                   adapter_protocol_version: 1,
                   capabilities: %{
                     "structured_input" => true,
                     "interactive" => true,
                     "adapter" => native_adapter("codex", "0.153.4", "symmetry-codex-1", 1)
                   }
                 })
               ],
               now: @now
             )

    assert runtime.repository_resource_id == nil
    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
    assert %{state: "queued"} = Repo.get!(Task, task.id)

    assert {:ok, [explicitly_unbound]} =
             Orchestration.register_runtimes(
               machine.id,
               Ecto.UUID.generate(),
               [
                 runtime_spec("explicitly-unbound-native", %{
                   repository_resource_id: nil,
                   harness_kind: "codex",
                   harness_version: "0.153.4",
                   adapter_version: "symmetry-codex-1",
                   adapter_protocol_version: 1,
                   capabilities: %{
                     "structured_input" => true,
                     "interactive" => true,
                     "adapter" => native_adapter("codex", "0.153.4", "symmetry-codex-1", 1)
                   }
                 })
               ],
               now: @now
             )

    assert explicitly_unbound.repository_resource_id == nil
    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
  end

  test "Goal claim rechecks repository affinity after assignment" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = register_runtime("claim-resource-affinity")
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
  end

  test "validation assignment requires every frozen binding to allow the runtime" do
    {task, _goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        purpose: "validate",
        validation_bindings: [
          validation_binding("checks", "check", []),
          validation_binding("review", "review", [])
        ]
      )

    first = register_runtime("validation-first")
    second = register_runtime("validation-second")
    outside = register_runtime("validation-outside")

    task =
      replace_validation_bindings!(task, [
        validation_binding("checks", "check", [first.id, second.id]),
        validation_binding("review", "review", [second.id])
      ])

    assert task.purpose == "validate"
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.runtime_id == second.id
    refute run.runtime_id == outside.id
  end

  test "validation assignment fails closed when frozen bindings have no common runtime" do
    {task, _goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        purpose: "validate",
        validation_bindings: [
          validation_binding("checks", "check", []),
          validation_binding("review", "review", [])
        ]
      )

    first = register_runtime("validation-empty-first")
    second = register_runtime("validation-empty-second")

    replace_validation_bindings!(task, [
      validation_binding("checks", "check", [first.id]),
      validation_binding("review", "review", [second.id])
    ])

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
    assert %{state: "queued"} = Repo.get!(Task, task.id)
  end

  test "empty validation bindings add no runtime restriction while malformed bindings fail closed" do
    {unrestricted_task, _goal_id} =
      insert_goal_task(max_run_attempts: 2, purpose: "validate", validation_bindings: [])

    unrestricted_item = Repo.get!(WorkItem, unrestricted_task.work_item_id)

    unrestricted_runtime =
      register_runtime("validation-unrestricted",
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
      register_runtime("validation-malformed",
        repository_resource_id: item.repository_resource_id
      )

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)

    task = replace_validation_bindings!(task, [%{"profile_name" => "checks", "kind" => "check"}])
    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)

    replace_validation_bindings!(task, [
      validation_binding("checks", "check", ["not-a-runtime-id"])
    ])

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
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
      register_runtime("validation-starved",
        repository_resource_id: blocked_item.repository_resource_id
      )

    Repo.update_all(
      from(task_row in Task, where: task_row.id == ^blocked_task.id),
      set: [inserted_at: DateTime.add(@now, -1, :second)]
    )

    {ready_task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    ready_item = Repo.get!(WorkItem, ready_task.work_item_id)

    ready_runtime =
      register_runtime("validation-ready",
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
        set: [inserted_at: DateTime.add(@now, -1, :second)]
      )
    end)

    {ready_task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    ready_item = Repo.get!(WorkItem, ready_task.work_item_id)

    ready_runtime =
      register_runtime("validation-page-ready",
        repository_resource_id: ready_item.repository_resource_id
      )

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == ready_task.id
    assert run.runtime_id == ready_runtime.id
  end

  test "validation binding replacement blocks a new claim but preserves an exact claim replay" do
    {assigned_task, _goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        purpose: "validate",
        validation_bindings: [validation_binding("checks", "check", [])]
      )

    assigned_runtime = register_runtime("validation-claim-assigned")
    rejected_runtime = register_runtime("validation-claim-rejected")

    replace_validation_bindings!(assigned_task, [
      validation_binding("checks", "check", [assigned_runtime.id])
    ])

    assert {:ok, assigned_run} = Orchestration.assign_one(now: @now)

    replace_validation_bindings!(Repo.get!(Task, assigned_task.id), [
      validation_binding("checks", "check", [rejected_runtime.id])
    ])

    assert {:error, :ownership_lost} =
             Orchestration.claim(
               assigned_run.id,
               %{
                 runtime_id: assigned_runtime.id,
                 runtime_epoch: assigned_runtime.connection_epoch,
                 generation: assigned_run.generation,
                 claim_id: Ecto.UUID.generate()
               },
               now: @now
             )

    {replay_task, _replay_goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        purpose: "validate",
        validation_bindings: [validation_binding("checks", "check", [])]
      )

    replay_item = Repo.get!(WorkItem, replay_task.work_item_id)

    replay_runtime =
      register_runtime("validation-claim-replay",
        repository_resource_id: replay_item.repository_resource_id
      )

    replacement_runtime =
      register_runtime("validation-claim-replacement",
        repository_resource_id: replay_item.repository_resource_id
      )

    replace_validation_bindings!(replay_task, [
      validation_binding("checks", "check", [replay_runtime.id])
    ])

    assert {:ok, replay_run} = Orchestration.assign_one(now: DateTime.add(@now, 1, :second))

    replay_request = %{
      runtime_id: replay_runtime.id,
      runtime_epoch: replay_runtime.connection_epoch,
      generation: replay_run.generation,
      claim_id: Ecto.UUID.generate()
    }

    assert {:ok, claimed} =
             Orchestration.claim(replay_run.id, replay_request,
               now: DateTime.add(@now, 1, :second)
             )

    replace_validation_bindings!(Repo.get!(Task, replay_task.id), [
      validation_binding("checks", "check", [replacement_runtime.id])
    ])

    assert {:ok, replayed} =
             Orchestration.claim(replay_run.id, replay_request,
               now: DateTime.add(@now, 1, :second)
             )

    assert replayed.lease_token == claimed.lease_token
  end

  test "lost claim acknowledgement replays after a runtime affinity change" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = register_runtime("claim-affinity-replay")
    assert {:ok, run} = Orchestration.assign_one(now: @now)

    request = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: Ecto.UUID.generate()
    }

    assert {:ok, claimed} = Orchestration.claim(run.id, request, now: @now)
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

    assert {:ok, replayed} = Orchestration.claim(run.id, request, now: @now)
    assert replayed.lease_token == claimed.lease_token
    assert replayed.claim_id == claimed.claim_id
  end

  test "runtime registration accepts only existing repository resources" do
    machine = enroll_machine("runtime-resource-validation")

    assert {:ok, project} =
             Workspaces.create_project(%{name: "Runtime resource project", key: project_key()})

    assert {:ok, ci_resource} =
             Workspaces.create_resource(project.id, %{
               kind: "ci",
               name: "Runtime CI resource #{System.unique_integer([:positive])}"
             })

    for repository_resource_id <- [Ecto.UUID.generate(), ci_resource.id, 1, true, %{}] do
      assert {:error, :invalid_request} =
               Orchestration.register_runtimes(
                 machine.id,
                 Ecto.UUID.generate(),
                 [
                   runtime_spec("invalid-resource-#{System.unique_integer([:positive])}", %{
                     repository_resource_id: repository_resource_id
                   })
                 ],
                 now: @now
               )
    end
  end

  test "Goal resume assignment is pinned to its retained compatible runtime" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    _other = register_runtime("resume-other")
    retained_runtime = register_runtime("resume-retained", resume?: true)
    {_task, retained_session} = bind_retained_session(task, retained_runtime)

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == task.id
    assert run.runtime_id == retained_runtime.id
    assert run.harness_session_id == retained_session.id
    assert retained_session.runtime_id == retained_runtime.id

    fence = claim(run, retained_runtime)

    assert {:ok, %{session: %{id: session_id, state: "busy"}}, :replayed} =
             Goals.attach_harness_session(
               retained_runtime.machine_id,
               run.id,
               fence,
               %{
                 local_handle_id: retained_session.local_handle_id,
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

  test "Goal resume with no viable retained runtime remains queued without fallback" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    _other = register_runtime("resume-fallback")
    retained_runtime = register_runtime("resume-unavailable", resume?: true)

    {_task, retained_session} =
      bind_retained_session(task, retained_runtime, state: "unavailable")

    assert retained_session.state == "unavailable"
    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
    assert %{state: "queued", current_generation: 0} = Repo.get!(Task, task.id)
    assert 0 == Repo.aggregate(from(run in Run, where: run.task_id == ^task.id), :count)
  end

  test "only one queued Goal task reserves a retained session before either daemon attaches" do
    {first_task, _first_goal_id} = insert_goal_task(max_run_attempts: 2)
    retained_runtime = register_runtime("exclusive-retained", resume?: true, capacity: 2)
    {_first_task, session} = bind_retained_session(first_task, retained_runtime)
    first_item = Repo.get!(WorkItem, first_task.work_item_id)

    {second_task, _second_goal_id} =
      insert_goal_task(
        max_run_attempts: 2,
        project_id: first_item.project_id,
        repository_id: first_item.repository_resource_id
      )

    second_item = Repo.get!(WorkItem, second_task.work_item_id)

    second_input =
      second_task.input
      |> Map.put("session_mode", "resume")
      |> Map.put("requested_session_id", session.id)

    Repo.update_all(
      from(item in WorkItem, where: item.id == ^second_item.id),
      set: [repository_resource_id: first_item.repository_resource_id]
    )

    Repo.update_all(
      from(task in Task, where: task.id == ^second_task.id),
      set: [
        input: second_input,
        requested_session_id: session.id,
        inserted_at: DateTime.add(@now, 1, :second)
      ]
    )

    assert {:ok, first_run} = Orchestration.assign_one(now: @now)
    assert first_run.task_id == first_task.id
    assert first_run.harness_session_id == session.id
    assert %{state: "busy", active_run_id: active_run_id} = Repo.get!(HarnessSession, session.id)
    assert active_run_id == first_run.id

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
    assert %{state: "queued", current_generation: 0} = Repo.get!(Task, second_task.id)
    assert 0 == Repo.aggregate(from(run in Run, where: run.task_id == ^second_task.id), :count)

    assert %{expired_runs: 1} = Orchestration.expire(now: DateTime.add(@now, 31, :second))
    assert %{state: "available", active_run_id: nil} = Repo.get!(HarnessSession, session.id)
  end

  test "lease expiry and a late terminal receipt keep a claimed retained session unavailable" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    retained_runtime = register_runtime("retained-expiry", resume?: true)
    {_task, session} = bind_retained_session(task, retained_runtime)
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, retained_runtime)
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
  end

  test "Goal machine retries an exact committed claim after pause but rejects a new claimant" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = register_runtime("lost-ack-claim")
    assert {:ok, run} = Orchestration.assign_one(now: @now)

    claim_request = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: Ecto.UUID.generate()
    }

    assert {:ok, claimed} = Orchestration.claim(run.id, claim_request, now: @now)
    pause_goal!(goal_id)

    assert {:ok, replayed_claim} = Orchestration.claim(run.id, claim_request, now: @now)
    assert replayed_claim.lease_token == claimed.lease_token

    assert {:error, :ownership_lost} =
             Orchestration.claim(run.id, %{claim_request | claim_id: Ecto.UUID.generate()},
               now: @now
             )

    assert task.id == run.task_id
  end

  test "Goal machine retries committed events and transitions but rejects changed or new writes after pause" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = register_runtime("lost-ack-authority")
    assert {:ok, run} = Orchestration.assign_one(now: @now)

    claim_request = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: Ecto.UUID.generate()
    }

    assert {:ok, claimed} = Orchestration.claim(run.id, claim_request, now: @now)

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
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = register_runtime("post-command-authority")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(run.id, fence, "running", %{}, Ecto.UUID.generate(),
               now: @now
             )

    goal = Repo.get!(Goal, goal_id)

    assert {:ok, %{goal: %{state: "paused"}}, :created} =
             Goals.command(
               goal_id,
               %{
                 schema_version: "symmetry.goal_command.v1",
                 mutation_id: Ecto.UUID.generate(),
                 expected_version: goal.lock_version,
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

  test "Goal assignment rejects a queued task whose current attempt is already consumed" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    _runtime = register_runtime("goal-current-attempt")

    Repo.update_all(
      from(task_row in Task, where: task_row.id == ^task.id),
      set: [current_generation: task.attempt_generation]
    )

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
  end

  test "Goal control command rejects a superseded lifecycle action" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = register_runtime("stale-control")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

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

    assert {:error, :stale_revision} =
             Orchestration.create_goal_control_command(
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

  test "Goal pause transition rejects a pause command superseded by resume" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = register_runtime("stale-pause-transition")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

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

    command = insert_goal_pause_command(task, run, pause_action_id)

    resume_action_id = Ecto.UUID.generate()
    insert_goal_event(goal_id, resume_action_id, 2, "resume")

    Repo.update_all(
      from(goal in Goal, where: goal.id == ^goal_id),
      set: [state: "active", event_sequence: 2]
    )

    assert {:error, :stale_revision} =
             Orchestration.transition(
               run.id,
               fence,
               "paused",
               %{"command_id" => command.id},
               Ecto.UUID.generate(),
               now: @now
             )

    assert Repo.get!(Command, command.id).state == "pending"
    assert {:ok, running} = Orchestration.fetch_run(run.id)
    assert running.state == "running"
  end

  test "Goal pause does not create an unimplemented native pause descriptor" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = register_runtime("goal-pause-no-native")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

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

  test "a draft planning Task assigns, claims, and emits one terminal settlement job by Subject resource" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2, purpose: "plan")
    repository_resource_id = task.input["subject"]["resource_id"]
    runtime = register_runtime("planning-task", repository_resource_id: repository_resource_id)

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == task.id
    assert run.runtime_id == runtime.id

    fence = claim(run, runtime)

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(run.id, fence, "running", %{}, Ecto.UUID.generate(),
               now: @now
             )

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

  test "a planning Task cannot start after its draft Goal becomes active" do
    {task, goal_id} = insert_goal_task(max_run_attempts: 2, purpose: "plan")
    repository_resource_id = task.input["subject"]["resource_id"]

    _runtime =
      register_runtime("planning-task-active", repository_resource_id: repository_resource_id)

    Repo.update_all(from(goal in Goal, where: goal.id == ^goal_id), set: [state: "active"])

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)
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

    assert command.state == "applied"
    assert {:ok, receipt} = Goals.settle_unstarted_task(task.id, "cancelled", now: @now)
    assert receipt["settlement"] == "released_without_run"
  end

  test "lease expiry consumes the admitted final run attempt and persists one settlement job" do
    {task, _goal} = insert_goal_task(max_run_attempts: 1)
    runtime = register_runtime("expiry")

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == task.id
    _fence = claim(run, runtime)

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
    runtime = register_runtime("retry")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

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

  test "final-attempt lease expiry retains terminal grace behind one delayed settlement job" do
    {task, _goal} = insert_goal_task(max_run_attempts: 1)
    runtime = register_runtime("final-grace")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

    expiry = DateTime.add(@now, 30, :second)
    assert %{expired_runs: 1} = Orchestration.expire(now: expiry)

    assert {:ok, %{state: "completed"}} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               %{"summary" => "late but within terminal grace"},
               uuid(2),
               now: DateTime.add(expiry, 1, :second)
             )

    assert {:ok, %{state: "completed", attempt_generation: 1}} = Orchestration.fetch_task(task.id)
    assert {:ok, %{state: "completed", failure: nil}} = Orchestration.fetch_run(run.id)
    assert [_job] = settlement_jobs(task.id, run.id, 1)
  end

  test "stale delivery after automatic retry cannot create another settlement job" do
    {task, _goal} = insert_goal_task(max_run_attempts: 2)
    runtime = register_runtime("stale")
    assert {:ok, first_run} = Orchestration.assign_one(now: @now)
    fence = claim(first_run, runtime)

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

  test "terminal transition writes one durable job and notifies only after the committing call" do
    {task, _goal} = insert_goal_task(max_run_attempts: 2)
    runtime = register_runtime("terminal")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

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

    assert_receive {:goal_task_terminal,
                    %{task_id: task_id, run_id: run_id, generation: generation}}

    assert task_id == task.id
    assert run_id == run.id
    assert generation == 1
    assert settlement_jobs(task.id, run.id, 1) |> length() == 1

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
    assert settlement_jobs(task.id, run.id, 1) |> length() == 1

    {rolled_back_task, _goal} = insert_goal_task(max_run_attempts: 2)
    second_item = Repo.get!(WorkItem, rolled_back_task.work_item_id)

    Repo.update_all(
      from(runtime_row in Runtime, where: runtime_row.id == ^runtime.id),
      set: [repository_resource_id: second_item.repository_resource_id]
    )

    assert {:ok, rolled_back_run} = Orchestration.assign_one(now: @now)
    rolled_back_fence = claim(rolled_back_run, runtime)

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
    assert settlement_jobs(task.id, run.id, 1) |> length() == 1
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
    validation_of_task_id = if purpose == "validate", do: Ecto.UUID.generate(), else: nil
    validation_bindings = Keyword.get(opts, :validation_bindings, [])
    policy = Keyword.get(opts, :execution_policy, execution_policy(max_run_attempts))

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
                     goal_id, goal_revision, context_snapshot_id, purpose, validation_of_task_id, admission_key, max_run_attempts,
                     inserted_at, updated_at
                   ) VALUES ($1, $2, $3, 1, 'Goal admission producer', 'codex', 'primary', '{}'::jsonb,
                     '{}'::jsonb, 'completed', 1, 1, $4, $5, 1, $6, 'implement', NULL, $7, $8, $9, $9)
                   """,
                   [
                     db_uuid(validation_of_task_id),
                     "goal-admission-producer-#{System.unique_integer([:positive])}",
                     :crypto.hash(:sha256, "goal-admission-producer"),
                     maybe_db_uuid(work_item_id),
                     db_uuid(goal_id),
                     db_uuid(snapshot_id),
                     db_uuid(Ecto.UUID.generate()),
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
                   goal_id, goal_revision, context_snapshot_id, purpose, validation_of_task_id, admission_key, max_run_attempts,
                   inserted_at, updated_at
                 ) VALUES ($1, $2, $3, 1, 'Goal admission task', 'codex', 'primary', '{}'::jsonb,
                   '{}'::jsonb, 'queued', 0, 1, $4, $5, 1, $6, $7, $8, $9, $10, $11, $11)
                 """,
                 [
                   db_uuid(task_id),
                   "goal-admission-task-#{System.unique_integer([:positive])}",
                   :crypto.hash(:sha256, "goal-admission-task"),
                   db_uuid(work_item_id),
                   db_uuid(goal_id),
                   db_uuid(snapshot_id),
                   purpose,
                   maybe_db_uuid(validation_of_task_id),
                   db_uuid(admission_key),
                   max_run_attempts,
                   @now
                 ]
               )

               Repo.query!(
                 "UPDATE tasks SET input = $1::text::jsonb WHERE id = $2",
                 [
                   Jason.encode!(%{
                     "subject" => %{
                       "resource_id" => repository_id,
                       "commit" => String.duplicate("a", 40),
                       "tree_digest" => "sha256:" <> String.duplicate("b", 64)
                     }
                   }),
                   db_uuid(task_id)
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

  defp replace_validation_bindings!(task, bindings) do
    snapshot_id = Ecto.UUID.generate()

    Repo.query!(
      """
      INSERT INTO context_snapshots (
        id, goal_id, goal_revision, work_item_id, schema_version, content_hash, payload, inserted_at
       ) VALUES ($1, $2, $3, $4, 1, $5, $6::text::jsonb, $7)
      """,
      [
        db_uuid(snapshot_id),
        db_uuid(task.goal_id),
        task.goal_revision,
        db_uuid(task.work_item_id),
        :crypto.hash(:sha256, "validation-bindings-#{snapshot_id}"),
        Jason.encode!(%{"work_contract" => %{"validation_bindings" => bindings}}),
        @now
      ]
    )

    Repo.update_all(
      from(task_row in Task, where: task_row.id == ^task.id),
      set: [context_snapshot_id: snapshot_id]
    )

    Repo.get!(Task, task.id)
  end

  defp validation_binding(profile_name, kind, allowed_runtime_ids) do
    %{
      "profile_name" => profile_name,
      "kind" => kind,
      "profile_digest" => "sha256:" <> String.duplicate("a", 64),
      "allowed_runtime_ids" => allowed_runtime_ids
    }
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

  defp runtime_spec(runtime_key, overrides \\ %{}) do
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
        "guidance" => "next_turn",
        "pause" => "unsupported",
        "approval_response" => false,
        "usage" => "reported",
        "hard_cost_limit" => false
      }
    }
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
      "budget_limit_microusd" => nil,
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

  defp register_runtime(label, opts \\ []) do
    machine_token = "goal-admission-machine-#{label}-#{System.unique_integer([:positive])}"
    resume? = Keyword.get(opts, :resume?, false)
    hard_cost_limit? = Keyword.get(opts, :hard_cost_limit?, false)
    capacity = Keyword.get(opts, :capacity, 1)

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
      native_adapter("codex", "0.153.4", "symmetry-codex-1", 1)
      |> put_in(["operations", "resume"], resume?)
      |> put_in(["operations", "hard_cost_limit"], hard_cost_limit?)

    assert {:ok, %{machine: machine}, :created} =
             Orchestration.enroll_machine(
               %{name: "Goal admission #{label}", machine_token: machine_token},
               Ecto.UUID.generate(),
               enrollment_token: "test-enrollment-token",
               expected_enrollment_token: "test-enrollment-token",
               now: @now
             )

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: "goal-admission-#{label}",
                   name: "Goal admission #{label}",
                   capacity: capacity,
                   agent_profile: "codex",
                   workspace: "primary",
                   harness_kind: "codex",
                   harness_version: "0.153.4",
                   adapter_version: "symmetry-codex-1",
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

  defp runtime_registration_spec(runtime, overrides \\ %{}) do
    Map.merge(
      %{
        runtime_key: runtime.runtime_key,
        name: runtime.name,
        capacity: runtime.capacity,
        agent_profile: runtime.agent_profile,
        workspace: runtime.workspace,
        repository_resource_id: runtime.repository_resource_id,
        capabilities: runtime.capabilities,
        harness_kind: runtime.harness_kind,
        harness_version: runtime.harness_version,
        adapter_version: runtime.adapter_version,
        adapter_protocol_version: runtime.adapter_protocol_version,
        heartbeat_interval_ms: runtime.heartbeat_interval_ms
      },
      overrides
    )
  end

  defp bind_retained_session(task, runtime, opts \\ []) do
    item = Repo.get!(WorkItem, task.work_item_id)
    repository_id = item.repository_resource_id

    Repo.update_all(
      from(runtime_row in Runtime, where: runtime_row.id == ^runtime.id),
      set: [repository_resource_id: repository_id]
    )

    session =
      %HarnessSession{}
      |> HarnessSession.changeset(%{
        machine_id: runtime.machine_id,
        runtime_id: runtime.id,
        repository_resource_id: repository_id,
        harness_kind: runtime.harness_kind,
        harness_version: runtime.harness_version,
        adapter_version: runtime.adapter_version,
        local_handle_id: Ecto.UUID.generate(),
        workspace_fingerprint: "workspace:#{runtime.id}",
        state: Keyword.get(opts, :state, "available")
      })
      |> Repo.insert!()

    input =
      task.input
      |> put_in(["subject", "resource_id"], repository_id)
      |> Map.put("session_mode", "resume")
      |> Map.put("requested_session_id", session.id)

    Repo.update_all(
      from(task_row in Task, where: task_row.id == ^task.id),
      set: [input: input, requested_session_id: session.id]
    )

    {Repo.get!(Task, task.id), session}
  end

  defp pause_goal!(goal_id) do
    goal = Repo.get!(Goal, goal_id)

    assert {:ok, %{goal: %{state: "paused"}}, :created} =
             Goals.command(
               goal_id,
               %{
                 schema_version: "symmetry.goal_command.v1",
                 mutation_id: Ecto.UUID.generate(),
                 expected_version: goal.lock_version,
                 expected_revision: goal.current_revision,
                 kind: "pause",
                 payload: %{reason: "Stop new execution before control delivery."}
               },
               "operator:test",
               now: @now
             )
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
               now: @now
             )

    %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: claimed.claim_id,
      lease_token: claimed.lease_token
    }
  end

  defp settlement_jobs(task_id, run_id, generation) do
    Repo.all(
      from job in Job,
        where:
          job.worker == "SymmetryControl.Goals.Workers.SettleTaskWorker" and
            job.args == ^%{"task_id" => task_id, "run_id" => run_id, "generation" => generation}
    )
  end

  defp insert_goal_event(goal_id, event_id, sequence, kind) do
    Repo.insert!(%GoalEvent{
      id: event_id,
      goal_id: goal_id,
      sequence: sequence,
      kind: kind,
      actor_ref: "operator:test",
      request_hash: :crypto.hash(:sha256, "#{event_id}:#{kind}"),
      request_hash_version: 2,
      revision: 1,
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

  defp uuid(number),
    do: "00000000-0000-0000-0000-" <> String.pad_leading(Integer.to_string(number), 12, "0")
end
