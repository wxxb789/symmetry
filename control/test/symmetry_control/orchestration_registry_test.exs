defmodule SymmetryControl.OrchestrationRegistryTest do
  use SymmetryControl.OrchestrationCase, async: false

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Machine

  @now ~U[2026-09-02 00:00:00.000000Z]

  test "machine enrollment replays the exact request and rejects key reuse" do
    key = "machine-enrollment-#{System.unique_integer([:positive])}"
    attrs = %{name: "builder", machine_token: "machine-token-#{key}"}

    opts = [
      enrollment_token: "enrollment-secret",
      expected_enrollment_token: "enrollment-secret",
      now: @now
    ]

    assert {:ok, first, :created} = Orchestration.enroll_machine(attrs, key, opts)
    assert {:ok, replayed, :replayed} = Orchestration.enroll_machine(attrs, key, opts)
    assert replayed.machine.id == first.machine.id
    assert replayed.token == first.token

    assert {:error, :idempotency_conflict} =
             Orchestration.enroll_machine(%{attrs | name: "other"}, key, opts)

    assert {:error, :idempotency_conflict} =
             Orchestration.enroll_machine(%{attrs | machine_token: "other-token"}, key, opts)
  end

  test "historical machine rows without replay metadata still authenticate and register" do
    token = "historical-machine-token"

    machine =
      %Machine{}
      |> Machine.changeset(%{name: "historical", token_digest: :crypto.hash(:sha256, token)})
      |> Repo.insert!()

    assert machine.enrollment_idempotency_key == nil
    assert machine.enrollment_request_hash == nil
    assert {:ok, ^machine} = Orchestration.authenticate_machine(token)

    assert {:ok, [_runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: "historical",
                   name: "Historical Runtime",
                   capacity: 1,
                   agent_profile: "codex",
                   workspace: "primary",
                   capabilities: %{}
                 }
               ],
               now: @now
             )
  end

  test "same daemon instance replays epoch and a replacement increments it" do
    %{machine: machine} = enroll_machine()

    first =
      register_runtime(machine, @now, daemon_instance_id: "00000000-0000-0000-0000-000000000001")

    replayed =
      register_runtime(machine, @now, daemon_instance_id: "00000000-0000-0000-0000-000000000001")

    replacement =
      register_runtime(machine, @now, daemon_instance_id: "00000000-0000-0000-0000-000000000002")

    assert replayed.connection_epoch == first.connection_epoch
    assert replacement.connection_epoch == first.connection_epoch + 1
  end

  test "runtime registration validates capabilities with the canonical schema" do
    %{machine: machine} = enroll_machine()

    assert {:error, :invalid_request} =
             Orchestration.register_runtimes(
               machine.id,
               "00000000-0000-0000-0000-000000000064",
               [
                 %{
                   runtime_key: "unknown-capability",
                   name: "Unknown capability",
                   capacity: 1,
                   agent_profile: "codex",
                   workspace: "primary",
                   capabilities: %{
                     "structured_input" => true,
                     "provider_access" => true,
                     "future_capability" => true
                   }
                 }
               ],
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
  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Run, Runtime, Task}
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.{ProjectResource, WorkItem}

  @now ~U[2026-09-09 09:00:00.000000Z]

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

  test "non-OMP runtimes retain legacy scheduling and claim with empty task requirements" do
    for kind <- [nil, "generic", "codex", "claude_code", "pi", "opencode"] do
      label = "legacy-compatible-#{kind || "untyped"}"
      machine = enroll_machine(label)

      specification =
        if is_nil(kind) do
          runtime_spec(label)
        else
          runtime_spec(label, %{
            harness_kind: kind,
            harness_version: "legacy-v1",
            adapter_version: "symmetry-legacy-1",
            adapter_protocol_version: 1
          })
        end

      assert {:ok, [runtime]} =
               Orchestration.register_runtimes(
                 machine.id,
                 Ecto.UUID.generate(),
                 [specification],
                 now: @now
               )

      assert {:ok, task, :created} =
               Orchestration.submit_task(legacy_task_attrs(), Ecto.UUID.generate(), now: @now)

      assert {:ok, run} = Orchestration.assign_one(now: @now)
      assert run.task_id == task.id
      assert run.runtime_id == runtime.id

      assert {:ok, claimed} =
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

      assert claimed.state == "claimed"
      assert is_binary(claimed.lease_token)
    end
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

  test "omo metadata persists and native assignment still requires adapter capabilities" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    repository_resource_id = Repo.get!(WorkItem, task.work_item_id).repository_resource_id
    machine = enroll_machine("omo-runtime")
    daemon_instance_id = Ecto.UUID.generate()

    specification =
      runtime_spec("omo-runtime", %{
        repository_resource_id: repository_resource_id,
        harness_kind: "omo",
        harness_version: "5.1.69",
        adapter_version: "omo-rpc-v1",
        adapter_protocol_version: 1
      })

    assert {:ok, [metadata_only_runtime]} =
             Orchestration.register_runtimes(machine.id, daemon_instance_id, [specification],
               now: @now
             )

    persisted_runtime = Repo.get!(Runtime, metadata_only_runtime.id)
    assert persisted_runtime.harness_kind == "omo"
    assert persisted_runtime.harness_version == "5.1.69"
    assert persisted_runtime.adapter_version == "omo-rpc-v1"
    assert persisted_runtime.adapter_protocol_version == 1
    assert persisted_runtime.capabilities == %{}
    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)

    specification =
      Map.put(specification, :capabilities, %{
        "structured_input" => true,
        "interactive" => true,
        "adapter" => native_adapter("omo", "5.1.69", "omo-rpc-v1", 1)
      })

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(machine.id, daemon_instance_id, [specification],
               now: @now
             )

    assert runtime.id == metadata_only_runtime.id
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == task.id
    assert run.runtime_id == runtime.id
    fence = goal_claim(run, runtime, @now)

    session_attrs = %{
      local_handle_id: Ecto.UUID.generate(),
      harness_kind: "omo",
      harness_version: "5.1.69",
      adapter_version: "omo-rpc-v1",
      workspace_fingerprint: "workspace:#{runtime.id}",
      workspace: "primary",
      repository_resource_id: repository_resource_id
    }

    assert {:error, :invalid_request} =
             Goals.attach_harness_session(
               machine.id,
               run.id,
               fence,
               Map.put(session_attrs, :harness_kind, "unknown"),
               now: @now
             )

    assert {:ok, %{session: %{id: session_id}}, :created} =
             Goals.attach_harness_session(machine.id, run.id, fence, session_attrs, now: @now)

    session = Repo.get!(HarnessSession, session_id)
    assert session.harness_kind == "omo"
    assert session.harness_version == "5.1.69"
    assert session.adapter_version == "omo-rpc-v1"
    assert session.runtime_id == runtime.id
    assert session.state == "busy"
    assert session.active_run_id == run.id
    assert session.binding_verified
    assert Repo.get!(Run, run.id).harness_session_id == session.id
  end

  test "runtime registration rejects incomplete or incompatible adapter declarations" do
    machine = enroll_machine("invalid-adapter")

    cases = [
      runtime_spec("partial", %{harness_kind: "codex"}),
      runtime_spec("unknown-kind", %{
        harness_kind: "unsupported_harness",
        harness_version: "18.20.0",
        adapter_version: "symmetry-omp-1",
        adapter_protocol_version: 1,
        capabilities: %{
          "adapter" => native_adapter("unsupported_harness", "18.20.0", "symmetry-omp-1", 1)
        }
      }),
      runtime_spec("omp-is-not-pi", %{
        harness_kind: "omp",
        harness_version: "18.20.0",
        adapter_version: "symmetry-omp-1",
        adapter_protocol_version: 1,
        capabilities: %{"adapter" => native_adapter("pi", "18.20.0", "symmetry-omp-1", 1)}
      }),
      runtime_spec("omo-is-not-pi", %{
        harness_kind: "omo",
        harness_version: "5.1.69",
        adapter_version: "omo-rpc-v1",
        adapter_protocol_version: 1,
        capabilities: %{
          "adapter" => native_adapter("pi", "5.1.69", "omo-rpc-v1", 1)
        }
      }),
      runtime_spec("unknown-omo-kind", %{
        harness_kind: "unknown",
        harness_version: "5.1.69",
        adapter_version: "omo-rpc-v1",
        adapter_protocol_version: 1
      }),
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
      runtime_spec("cancel-without-events", %{
        harness_kind: "codex",
        harness_version: "0.153.4",
        adapter_version: "symmetry-codex-1",
        adapter_protocol_version: 1,
        capabilities: %{
          "adapter" =>
            put_in(
              native_adapter("codex", "0.153.4", "symmetry-codex-1", 1),
              ["operations", "events"],
              false
            )
        }
      }),
      runtime_spec("approval-without-events", %{
        harness_kind: "codex",
        harness_version: "0.153.4",
        adapter_version: "symmetry-codex-1",
        adapter_protocol_version: 1,
        capabilities: %{
          "adapter" =>
            native_adapter("codex", "0.153.4", "symmetry-codex-1", 1)
            |> put_in(["operations", "events"], false)
            |> put_in(["operations", "approval_response"], true)
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

  test "runtime re-registration permits a same-affinity refresh but rejects a change during a Goal run" do
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2)
    runtime = enroll_and_register_runtime("active-affinity-registration")
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
    runtime = enroll_and_register_runtime("retained-affinity-registration", resume?: true)
    {task, _goal_id} = insert_goal_task(max_run_attempts: 2, retained_runtime: runtime)
    session = Repo.get!(HarnessSession, task.requested_session_id)
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
        "handoff" => false,
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

  defp legacy_task_attrs do
    %{goal: "Retry legacy body", agent_profile: "codex", workspace: "primary", input: %{}}
  end

  defp project_key do
    "P" <> Integer.to_string(rem(System.unique_integer([:positive]), 9_999_999))
  end
end
