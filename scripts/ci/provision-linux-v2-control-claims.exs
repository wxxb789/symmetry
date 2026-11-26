defmodule LinuxV2ControlClaimFixture do
  alias SymmetryControl.{Integrations, Orchestration, Repo, Workspaces}
  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.GoalDecision
  alias SymmetryControl.Integrations.ProviderActionIntent
  alias SymmetryControl.Orchestration.{Run, Runtime, Task}
  import Ecto.Query

  @stage_replay_holds [
    "reservation_durable",
    "create_started",
    "leaf_bound",
    "helper_bound",
    "launcher_bound",
    "gate_bound",
    "handoff_prepared",
    "handoff_committed",
    "lease_armed",
    "checkout_ready",
    "native_release",
    "stop_receipt_durable",
    "leaf_removed",
    "journal_cleared"
  ]
  @ack_loss_stages [
    "helper_bound",
    "launcher_bound",
    "gate_bound",
    "handoff_prepared",
    "handoff_committed",
    "lease_armed",
    "checkout_ready",
    "stop_receipt_durable",
    "native_release",
    "journal_cleared"
  ]
  @expected_fixture_runs 48
  @fixture_database_prefix "symmetry_linux_v2"
  @fixture_actor "operator:linux-v2-fixture"

  @cases [
    {"pi_provider_action", "pi-native", :pi, :pi_provider,
     "Use the real symmetry_resource_sync tool for the configured GitHub repository, then create and commit one small file. Do not publish."},
    {"opencode_native_health", "opencode-native", :opencode, :opencode,
     "Use the real native OpenCode profile to create and commit one small file in the B-owned clone. Do not publish."},
    {"helper_sigkill_detached_child", "pi-native", :pi, :pi_provider,
     "Use the real symmetry_resource_sync tool, then create and commit one small file for the detached-child helper crash witness. Do not publish."},
    {"daemon_sigkill_helper_survives_detached_child", "pi-native", :pi, :pi_provider,
     "Use the real symmetry_resource_sync tool, then create and commit one small file for the detached-child daemon crash witness. Do not publish."},
    {"codex_containment", "codex-native", :codex, :opencode_goal,
     "Use the real codex runtime for the Linux v2 containment boundary witness. Do not publish."},
    {"claude_containment", "claude-native", :claude_code, :opencode_goal,
     "Use the real Claude Code runtime for the Linux v2 containment boundary witness. Do not publish."}
  ] ++
    Enum.map(@stage_replay_holds, fn hold ->
      {"stage_replay_#{hold}", "pi-durability", :pi, :pi_durability,
       "Use the real Pi runtime for the Linux v2 stage replay witness at #{hold}. Do not publish."}
    end) ++
    Enum.map(@ack_loss_stages, fn stage ->
      {"stage_replay_ack_loss_#{stage}", "pi-durability", :pi, :pi_durability,
       "Use the real Pi runtime for the Linux v2 lost-ACK replay witness at #{stage}. Do not publish."}
    end) ++
    [
      {"stage_replay_stale_journal_run_key_setup", "pi-durability", :pi, :pi_durability,
       "Use the real Pi runtime to seed a completed Linux v2 journal for stale RunKey denial. Do not publish."},
      {"stage_replay_stale_journal_run_key_request", "pi-durability", :pi, :pi_durability,
       "Use the real Pi runtime for the second fresh Claim in stale RunKey denial. Do not publish."},
      {"stage_replay_stale_journal_run_key_legacy_setup", "pi-durability", :pi, :pi_durability,
       "Use the real Pi runtime to seed the preserved original stale RunKey denial witness. Do not publish."},
      {"stage_replay_stale_journal_run_key_legacy_request", "pi-durability", :pi, :pi_durability,
       "Use the real Pi runtime for the preserved original stale RunKey denial Claim. Do not publish."},
      {"stage_replay_stale_journal_fence_setup", "pi-durability", :pi, :pi_durability,
       "Use the real Pi runtime to seed a completed Linux v2 journal for stale fence denial. Do not publish."},
      {"stage_replay_stale_journal_subject_setup", "pi-durability", :pi, :pi_durability,
       "Use the real Pi runtime to seed a completed Linux v2 journal for stale Subject denial. Do not publish."},
      {"stage_replay_receipt_persist_failure", "pi-durability", :pi, :pi_durability,
       "Use the real Pi runtime for the Linux v2 receipt persistence failure witness. Do not publish."},
      {"stage_replay_stale_leaf", "pi-durability", :pi, :pi_durability,
       "Use the real Pi runtime for the Linux v2 stale cgroup-leaf identity witness. Do not publish."},
      {"stage_replay_alternate_mount", "pi-durability", :pi, :pi_durability,
       "Use the real Pi runtime for the Linux v2 alternate cgroup-mount refusal witness. Do not publish."},
      {"stage_replay_migration", "pi-durability", :pi, :pi_durability,
       "Use the real Pi runtime for the Linux v2 migration denial witness. Do not publish."},
      {"stage_replay_literal_pid_reuse", "pi-durability", :pi, :pi_durability,
       "Use the real Pi runtime for the exact Linux v2 literal PID-reuse witness. Do not publish."},
      {"stage_replay_helper_exit_after_release_before_native_register", "pi-durability", :pi, :pi_durability,
       "Use the real Pi runtime for the installed helper-exit-after-RELEASE-before-native-register witness. Do not publish."},
      {"stage_replay_helper_stop_response_loss", "pi-durability", :pi, :pi_durability,
       "Use the real Pi runtime for the Linux v2 helper stop-response-loss replay witness. Do not publish."},
      {"stage_replay_helper_ack_stop_receipt_response_loss", "pi-durability", :pi, :pi_durability,
       "Use the real Pi runtime for the Linux v2 helper stop-receipt-response-loss replay witness. Do not publish."},
      {"stage_replay_helper_launcher_response_loss", "pi-durability", :pi, :pi_durability,
       "Use the real Pi runtime for the Linux v2 helper launcher-response-loss replay witness. Do not publish."},
      {"stage_replay_unprovisioned_refusal", "opencode-native", :opencode, :opencode,
       "Use the real OpenCode runtime for the unprovisioned Linux owner refusal witness. Do not publish."},
      {"stage_replay_native_release_codex", "codex-native", :codex, :opencode_goal,
       "Use the real codex runtime for the Linux v2 native-release crash-replay witness. Do not publish."},
      {"stage_replay_native_release_claude", "claude-native", :claude_code, :opencode_goal,
       "Use the real Claude Code runtime for the Linux v2 native-release crash-replay witness. Do not publish."}
    ]

  def run!("prepare", [output_path, repository_ref, repository_owner, commit, tree_digest]) do
    require_ephemeral_ci!()
    start_repo!()
    retire_earlier_case_set_runtimes!()
    validate_subject!(repository_ref, commit, tree_digest)
    suffix = Ecto.UUID.generate() |> String.replace("-", "")

    {:ok, project} =
      Workspaces.create_project(%{
        name: "Linux v2 isolated owner witness #{suffix}",
        key: "V2" <> (String.slice(suffix, 0, 6) |> String.upcase()),
        default_agent_profile: "pi-native",
        default_workspace: "native"
      })

    # The per-run suffix keeps the natural key unique, so this always inserts;
    # the replayed shape is part of the create contract and is consumed the same
    # way instead of raising a MatchError if that ever changes.
    connection =
      case Integrations.create_connection(%{
             provider: "github",
             name: "linux-v2-owner-witness-#{suffix}",
             account_ref: repository_owner,
             capabilities: ["repositories"]
           }) do
        {:ok, connection} -> connection
        {:replayed, connection} -> connection
      end

    {:ok, pi_repository} =
      Workspaces.create_resource(project.id, %{
        connection_id: connection.id,
        kind: "repository",
        name: "Linux v2 Pi provider witness repository #{suffix}",
        external_ref: repository_ref
      })

    {:ok, opencode_repository} =
      Workspaces.create_resource(project.id, %{
        kind: "repository",
        name: "Linux v2 OpenCode witness repository #{suffix}"
      })

    {:ok, pi_durability_repository} =
      Workspaces.create_resource(project.id, %{
        kind: "repository",
        name: "Linux v2 Pi durability witness repository #{suffix}"
      })

    {legacy_cases, goal_cases} =
      Enum.split_with(@cases, fn {_name, _profile, _harness, repository_role, _description} ->
        repository_role not in [:pi_provider, :pi_durability, :opencode_goal]
      end)

    work_items =
      Map.new(legacy_cases, fn {case_name, profile, _harness, repository_role, description} ->
        :opencode = repository_role
        repository_id = opencode_repository.id

        {:ok, item} =
          Workspaces.create_work_item(project.id, %{
            title: "Linux v2 #{case_name} #{suffix}",
            description: description,
            status: "ready",
            priority: "high",
            assignee_type: "agent",
            agent_profile: profile,
            workspace: "native",
            repository_resource_id: repository_id
          })

        {case_name,
         %{
           "work_item_id" => item.id,
           "repository_resource_id" => repository_id,
           "agent_profile" => profile
         }}
      end)

    # The goal-routed cases get their WorkItem through Control's own Goal admission; their WorkItem
    # must stay goal-owned for the clone fence, and test-linux-v2-owner-request-keys.py pins both sets.
    goal_case_specs =
      Map.new(goal_cases, fn {case_name, profile, _harness, repository_role, description} ->
        repository_id =
          case repository_role do
            :pi_provider -> pi_repository.id
            :pi_durability -> pi_durability_repository.id
            :opencode_goal -> opencode_repository.id
          end

        {case_name,
         %{
           "repository_resource_id" => repository_id,
           "agent_profile" => profile,
           "description" => description,
           "provider_sync" => repository_role == :pi_provider
         }}
      end)

    fixture_runs = map_size(work_items) + map_size(goal_case_specs)

    if fixture_runs != @expected_fixture_runs do
      raise "owner-witness fixture Run count changed: expected #{@expected_fixture_runs}, got #{fixture_runs}"
    end

    write_private_json!(output_path, %{
      "version" => 1,
      "project_id" => project.id,
      "repository_ref" => repository_ref,
      "subject" => %{"commit" => commit, "tree_digest" => tree_digest},
      "pi_repository_resource_id" => pi_repository.id,
      "pi_durability_repository_resource_id" => pi_durability_repository.id,
      "opencode_repository_resource_id" => opencode_repository.id,
      "work_items" => work_items,
      "goal_cases" => goal_case_specs
    })
  end

  def run!("schedule", [fixture_path, case_name, output_path]) do
    require_ephemeral_ci!()
    start_repo!()
    fixture = read_json!(fixture_path)
    fixture_runs = map_size(fixture["work_items"]) + map_size(fixture["goal_cases"])

    if fixture_runs != @expected_fixture_runs do
      raise "owner-witness fixture Run count changed before assignment: expected #{@expected_fixture_runs}"
    end
    {^case_name, profile, harness, repository_role, description} =
      Enum.find(@cases, &(elem(&1, 0) == case_name)) || raise "unknown owner-witness case"
    request_case =
      cond do
        case_name in [
          "stage_replay_native_release_codex",
          "stage_replay_native_release_claude",
          "stage_replay_unprovisioned_refusal"
        ] ->
          case_name
        String.starts_with?(case_name, "stage_replay_") -> "stage_replay"
        true -> case_name
      end

    {item, task_id} =
      case Map.fetch(fixture["goal_cases"], case_name) do
        {:ok, goal_case} ->
          provision_goal_admission!(fixture, case_name, profile, description, goal_case)

        :error ->
          item = Map.fetch!(fixture["work_items"], case_name)
          {item, launch_one_shot_work_item!(item["work_item_id"], case_name)}
      end

    run = await_assigned_run!(task_id, 30_000)
    runtime = Repo.get!(Runtime, run.runtime_id)
    provider_access? = Map.get(runtime.capabilities || %{}, "provider_access", false)

    if !(runtime.agent_profile == profile and runtime.workspace == "native" and
           runtime.repository_resource_id == item["repository_resource_id"] and
           provider_access? == (repository_role == :pi_provider) and
           runtime_adapter_binding?(runtime, repository_role, harness)) do
      raise "generic Control claim runtime did not match the exact Work profile/resource"
    end

    write_private_json!(output_path, %{
      "version" => 1,
      "case" => request_case,
      "fixture_case" => case_name,
      "harness" => Atom.to_string(harness),
      "run_id" => run.id,
      "task_id" => task_id,
      "generation" => run.generation,
      "runtime_id" => runtime.id,
      "runtime_epoch" => runtime.connection_epoch,
      "claim_id" => Ecto.UUID.generate(),
      "work_item_id" => item["work_item_id"],
      "repository_resource_id" => item["repository_resource_id"],
      "commit" => fixture["subject"]["commit"],
      "tree_digest" => fixture["subject"]["tree_digest"],
      "agent_profile" => profile,
      "workspace" => runtime.workspace
    })
  end

  def run!("read-provider-action", [context_path, output_path, cancel_path]) do
    require_ephemeral_ci!()
    start_repo!()
    context = read_json!(context_path)
    validate_provider_receipt_context!(context)
    intent =
      await_provider_action_receipt!(
        context,
        System.monotonic_time(:millisecond) + 900_000,
        cancel_path
      )

    write_private_json!(output_path, %{
      "version" => 1,
      "case" => context["case"],
      "work_item_id" => intent.work_item_id,
      "task_id" => intent.task_id,
      "run_id" => intent.run_id,
      "generation" => intent.generation,
      "runtime_id" => intent.runtime_id,
      "runtime_epoch" => intent.runtime_epoch,
      "claim_id" => intent.claim_id,
      "resource_id" => intent.resource_id,
      "operation" => intent.operation,
      "state" => intent.state,
      "provider" => intent.provider,
      "resource_kind" => intent.resource_kind,
      "completed_at" => DateTime.to_iso8601(intent.completed_at),
      "matched_count" => 1,
      "intent_id_sha256" => sha256_hex(intent.id),
      "action_id_sha256" => sha256_hex(intent.action_id),
      "failure_present" => not is_nil(intent.failure),
      "result_metadata" => provider_action_result_metadata!(intent)
    })
  end

  def run!(mode, arguments) do
    raise "invalid Control claim fixture invocation mode=#{inspect(mode)} argc=#{length(arguments)}"
  end

  defp validate_provider_receipt_context!(context) do
    required = [
      "case", "run_id", "task_id", "generation", "runtime_id", "runtime_epoch",
      "claim_id", "work_item_id", "resource_id"
    ]

    unless is_map(context) and
             Enum.all?(required, fn key -> Map.has_key?(context, key) end) and
             context["version"] == 1 and
             context["case"] in [
               "pi_provider_action",
               "helper_sigkill_detached_child",
               "daemon_sigkill_helper_survives_detached_child"
             ] and
             is_integer(context["generation"]) and
             context["generation"] > 0 and is_integer(context["runtime_epoch"]) and
             context["runtime_epoch"] > 0 and is_binary(context["run_id"]) and
             is_binary(context["task_id"]) and is_binary(context["runtime_id"]) and
             is_binary(context["claim_id"]) and
             is_binary(context["work_item_id"]) and is_binary(context["resource_id"]) do
      raise "provider-action receipt context is incomplete or is not a Pi Claim"
    end
  end

  defp await_provider_action_receipt!(context, deadline, cancel_path) do
    if File.exists?(cancel_path) do
      raise "Pi provider-action E2E failed before Control receipt readback"
    end
    run_id = context["run_id"]
    generation = context["generation"]
    runtime_id = context["runtime_id"]
    runtime_epoch = context["runtime_epoch"]
    claim_id = context["claim_id"]
    work_item_id = context["work_item_id"]
    resource_id = context["resource_id"]

    rows =
      Repo.all(
        from intent in ProviderActionIntent,
          join: run in Run,
          on: run.id == intent.run_id and run.task_id == intent.task_id and
            run.runtime_id == intent.runtime_id,
          join: task in Task,
          on: task.id == intent.task_id and task.work_item_id == intent.work_item_id,
          where:
            intent.run_id == ^run_id and run.generation == ^generation and
              run.runtime_id == ^runtime_id and intent.runtime_id == ^runtime_id and
              run.claimed_runtime_epoch == ^runtime_epoch and run.claim_id == ^claim_id and
              intent.runtime_epoch == ^runtime_epoch and intent.generation == ^generation and
              intent.claim_id == ^claim_id and intent.work_item_id == ^work_item_id and
              task.work_item_id == ^work_item_id and intent.resource_id == ^resource_id and
              intent.operation == "resource.sync",
          order_by: [desc: intent.completed_at, desc: intent.id]
      )

    case rows do
      [%ProviderActionIntent{state: "succeeded"} = intent] -> intent
      [%ProviderActionIntent{state: state}] when state in ["failed", "unknown"] ->
          raise "Control persisted a failed/unknown resource.sync intent for the exact Pi RunKey"
      [%ProviderActionIntent{state: state}] when state in ["accepted", "executing"] ->
        if System.monotonic_time(:millisecond) >= deadline do
          raise "Control did not persist a succeeded resource.sync intent for the exact Pi RunKey"
        end

        Process.sleep(50)
        await_provider_action_receipt!(context, deadline, cancel_path)
      [] ->
        if System.monotonic_time(:millisecond) >= deadline do
          raise "Control did not persist a succeeded resource.sync intent for the exact Pi RunKey"
        end

        Process.sleep(50)
        await_provider_action_receipt!(context, deadline, cancel_path)
      _multiple ->
        raise "Control persisted multiple resource.sync intents for the exact Pi RunKey"
    end
  end

  defp provider_action_result_metadata!(intent) do
    result = intent.result || %{}
    resource = Map.get(result, "resource", %{})

    unless intent.operation == "resource.sync" and intent.state == "succeeded" and
             intent.provider == "github" and intent.resource_kind == "repository" and
             match?(%DateTime{}, intent.completed_at) and is_nil(intent.failure) and
             Map.get(result, "operation") == "resource.sync" and
             Map.get(result, "resource_id") == intent.resource_id and
             is_boolean(Map.get(result, "projected")) and is_map(resource) and
             Map.get(resource, "provider") == "github" and
             Map.get(resource, "kind") == "repository" and
             is_binary(Map.get(resource, "status")) and Map.get(resource, "status") != "" and
             is_binary(Map.get(resource, "sync_status")) and Map.get(resource, "sync_status") != "" do
      raise "Control resource.sync intent lacks an exact terminal success result"
    end

    metadata = %{
      "operation" => Map.get(result, "operation"),
      "resource_id" => Map.get(result, "resource_id"),
      "projected" => Map.get(result, "projected"),
      "resource" => %{
        "provider" => Map.get(resource, "provider"),
        "kind" => Map.get(resource, "kind"),
        "status" => Map.get(resource, "status"),
        "sync_status" => Map.get(resource, "sync_status"),
        "last_synced_at" => Map.get(resource, "last_synced_at")
      }
    }

    case Map.get(result, "readback") do
      value when value in ["applied", "unconfirmed", "not_applied"] ->
        Map.put(metadata, "readback", value)

      _other -> metadata
    end
  end

  defp sha256_hex(value) when is_binary(value) do
    Base.encode16(:crypto.hash(:sha256, value), case: :lower)
  end

  # The clone reserve parses the Claim's Work input as a goal admission
  # (linux_clone_journal.go:864-877), so Control composes it through this command path.
  defp provision_goal_admission!(fixture, case_name, profile, description, goal_case) do
    subject = %{
      "resource_id" => goal_case["repository_resource_id"],
      "commit" => fixture["subject"]["commit"],
      "tree_digest" => fixture["subject"]["tree_digest"]
    }

    allowed_actions = if goal_case["provider_sync"], do: ["resource.sync"], else: []

    acceptance = %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "A scoped operator acceptance is required.",
      "predicates" => [%{"id" => "operator", "kind" => "operator_acceptance"}]
    }

    # Composed by parts so the block is not a copy of the Control test's goal_create_attrs.
    execution_policy =
      Map.merge(
        %{
          "automatic_execution" => false,
          "budget_limit_microusd" => nil,
          "per_run_cost_limit_microusd" => nil,
          "budget_mode" => "soft",
          "hard_cost_limit_required" => false,
          "final_acceptance" => "operator"
        },
        %{
          "max_parallel_tasks" => 2,
          "max_task_admissions" => 2,
          "max_run_attempts_per_task" => 2,
          "allowed_runtime_ids" => [],
          "allowed_model_profiles" => [],
          "allowed_actions" => allowed_actions,
          "allowed_resource_ids" => []
        }
      )

    attrs = %{
      schema_version: "symmetry.goal_create.v1",
      mutation_id: Ecto.UUID.generate(),
      title: "Linux v2 #{case_name} owner witness",
      initial_revision: %{
        objective: description,
        non_goals: ["No autonomous publication"],
        acceptance_contract: acceptance,
        authority_policy: %{
          "operator_required_for_scope_change" => true,
          "operator_required_for_completion" => true,
          "publication_allowed" => false,
          "allowed_actions" => allowed_actions
        },
        execution_policy: execution_policy,
        context_manifest: %{
          "byte_budget" => 32_768,
          "required_source_kinds" => ["repository"],
          "include_advisory_recall" => false
        },
        reason: "Isolated Linux v2 owner-witness admission"
      }
    }

    created =
      case Goals.create_goal(fixture["project_id"], attrs, @fixture_actor, rollout_enabled: true) do
        {:ok, receipt, :created} -> receipt
        other -> raise "owner-witness Goal creation was refused: #{inspect(other)}"
      end

    goal_id = created.goal_id

    proposal = %{
      schema_version: "symmetry.plan.v1",
      proposal_id: Ecto.UUID.generate(),
      goal_id: goal_id,
      expected_revision: 1,
      items: [
        %{
          key: "witness",
          title: "Linux v2 #{case_name} owner witness",
          description: description,
          required: true,
          integration: true,
          repository_resource_id: subject["resource_id"],
          acceptance: acceptance,
          depends_on_keys: [],
          model_profile: profile,
          change_target: nil,
          baseline: %{kind: "subject", subject: subject}
        }
      ]
    }

    decision_receipt =
      goal_command!(goal_id, "request_decision", %{
        kind: "plan",
        work_item_id: nil,
        subject_hash: nil,
        proposal: proposal
      })

    decision_id = get_in(decision_receipt, [:response, "decision", "id"]) || raise "plan decision is absent"
    decision = Repo.get!(GoalDecision, decision_id)

    goal_command!(goal_id, "resolve_decision", %{
      decision_id: decision_id,
      expected_decision_version: decision.lock_version,
      option_id: "accept"
    })

    goal_command!(goal_id, "accept_plan", %{
      proposal: proposal,
      proposal_hash: "sha256:" <> Base.encode16(SymmetryControl.RequestHash.canonical(proposal), case: :lower),
      decision_id: decision_id
    })

    goal_command!(goal_id, "activate", %{approved_revision: 1})

    {:ok, goal} = Goals.fetch_goal(goal_id)
    item_id = goal.work_items |> List.first() |> Map.fetch!(:id)

    # Both allowed_actions lists carrying resource.sync is what makes Control compose the provider
    # scope (admission.ex:1218-1268,:1304-1307); every other goal case keeps the provider-free shape.
    admitted =
      goal_command!(goal_id, "admit_task", %{
        work_item_id: item_id,
        purpose: "implement",
        model_profile: profile,
        session_mode: "fresh",
        requested_session_id: nil,
        validation_of_task_id: nil
      })

    {%{
       "work_item_id" => item_id,
       "repository_resource_id" => subject["resource_id"],
       "agent_profile" => profile
     }, admitted.response["task"]["id"]}
  end

  defp goal_command!(goal_id, kind, payload) do
    {:ok, goal} = Goals.fetch_goal(goal_id)

    command = %{
      schema_version: "symmetry.goal_command.v1",
      mutation_id: Ecto.UUID.generate(),
      expected_version: goal.version,
      expected_revision: goal.current_revision,
      kind: kind,
      payload: payload
    }

    case Goals.command(goal_id, command, @fixture_actor, rollout_enabled: true) do
      {:ok, receipt, :created} -> receipt
      other -> raise "owner-witness goal command #{kind} was refused: #{inspect(other)}"
    end
  end

  # Goal assignment requires a native-harness runtime with adapter operations
  # (assignment.ex:332,:519-530); the legacy opencode claim runtimes stay adapter-free.
  defp runtime_adapter_binding?(runtime, role, _harness) when role in [:pi_provider, :pi_durability],
    do: get_in(runtime.capabilities || %{}, ["adapter", "kind"]) == "pi"

  defp runtime_adapter_binding?(runtime, :opencode_goal, harness),
    do: get_in(runtime.capabilities || %{}, ["adapter", "kind"]) == Atom.to_string(harness)

  defp runtime_adapter_binding?(runtime, _repository_role, _harness),
    do: not Map.has_key?(runtime.capabilities || %{}, "adapter")

  defp start_repo! do
    {:ok, _started} = Application.ensure_all_started(:ecto_sql)

    case Repo.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _pid}} -> :ok
      {:error, reason} -> raise "could not start isolated Control fixture repository: #{inspect(reason)}"
    end

    # Goal commands persist wakeup jobs, so Oban must be running; queues stay
    # off because the fixture never executes jobs itself.
    {:ok, _oban_started} = Application.ensure_all_started(:oban)

    oban =
      :symmetry_control
      |> Application.fetch_env!(Oban)
      |> Keyword.put(:queues, false)
      |> Keyword.delete(:plugins)

    case Oban.start_link(oban) do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _pid}} -> :ok
      {:error, reason} -> raise "could not start isolated Control fixture Oban: #{inspect(reason)}"
    end
  end

  defp require_ephemeral_ci! do
    unless System.get_env("GITHUB_ACTIONS") == "true" and
             is_binary(System.get_env("GITHUB_RUN_ID")) and
             System.get_env("GITHUB_RUN_ID") != "" do
      raise "Linux v2 Control claim fixtures may run only in an ephemeral GitHub Actions database"
    end
  end

  defp validate_subject!(repository_ref, commit, tree_digest) do
    valid_ref? = Regex.match?(~r/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/, repository_ref)
    valid_commit? = Regex.match?(~r/^(?:[0-9a-f]{40}|[0-9a-f]{64})$/, commit)
    valid_tree? = Regex.match?(~r/^sha256:[0-9a-f]{64}$/, tree_digest)
    unless valid_ref? and valid_commit? and valid_tree?, do: raise("invalid source Subject for Control fixture")
  end

  defp launch_one_shot_work_item!(work_item_id, case_name) do
    # Legacy (goal-less) tasks must not carry goal-admission fields
    # (control constraint tasks_goal_fields_all_or_none).
    {:ok, %{task: %{task: task}}, :created} =
      Workspaces.launch_work_item(work_item_id, "linux-v2-owner-#{case_name}", notify: false)

    task.id
  end

  # Case sets share one Control database per job and legacy assignment takes the oldest online
  # runtime (assignment.ex:260-304), so every online runtime here belongs to an earlier case set.
  defp retire_earlier_case_set_runtimes!() do
    require_fixture_local_database!()

    retired =
      from(runtime in Runtime, where: runtime.status == "online")
      |> Repo.all()
      |> Enum.map(fn runtime ->
        runtime
        |> Runtime.changeset(%{status: "offline"})
        |> Repo.update!()
      end)

    IO.puts("retired #{length(retired)} runtime(s) left online by earlier case sets")
  end

  # The sweep may touch only the job's own fixture database: the loopback service the workflow
  # names. Any other target refuses before a query; this fail-closed check is defense in depth.
  defp require_fixture_local_database!() do
    config = Repo.config()

    unless config[:hostname] in ["127.0.0.1", "localhost", "::1"] and
             String.starts_with?(to_string(config[:database]), @fixture_database_prefix) do
      raise "runtime retirement refuses a Control database that is not the fixture's own local one: " <>
              "host=#{inspect(config[:hostname])} database=#{inspect(config[:database])}"
    end
  end

  defp await_assigned_run!(task_id, timeout_ms) do
    await_task_run!(task_id, System.monotonic_time(:millisecond) + timeout_ms)
  end

  defp await_task_run!(task_id, deadline) do
    case Repo.get_by(Run, task_id: task_id) do
      %Run{} = run -> run
      nil ->
        if System.monotonic_time(:millisecond) >= deadline do
          raise "Control did not assign the exact isolated owner-witness Task"
        end

        case Orchestration.assign_one() do
          {:ok, _assigned} -> :ok
          {:error, :no_assignment} -> :ok
        end

        Process.sleep(25)
        await_task_run!(task_id, deadline)
    end
  end

  defp read_json!(path) do
    path |> File.read!() |> Jason.decode!()
  end

  defp write_private_json!(path, value) do
    File.write!(path, Jason.encode!(value))
    File.chmod!(path, 0o600)
  end
end

case System.argv() do
  [mode | arguments] -> LinuxV2ControlClaimFixture.run!(mode, arguments)
  _ -> raise "expected prepare/schedule and fixture paths"
end
