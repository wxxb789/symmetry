defmodule LinuxV2ControlClaimFixture do
  alias SymmetryControl.{Integrations, Orchestration, Repo, Workspaces}
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
  @expected_fixture_runs 44

  @cases [
    {"pi_provider_action", "pi-native", :pi,
     "Use the real symmetry_resource_sync tool for the configured GitHub repository, then create and commit one small file. Do not publish."},
    {"opencode_native_health", "opencode-native", :opencode,
     "Use the real native OpenCode profile to create and commit one small file in the B-owned clone. Do not publish."},
    {"helper_sigkill_detached_child", "pi-native", :pi,
     "Use the real symmetry_resource_sync tool, then create and commit one small file for the detached-child helper crash witness. Do not publish."},
    {"daemon_sigkill_helper_survives_detached_child", "pi-native", :pi,
     "Use the real symmetry_resource_sync tool, then create and commit one small file for the detached-child daemon crash witness. Do not publish."}
  ] ++
    Enum.map(@stage_replay_holds, fn hold ->
      {"stage_replay_#{hold}", "opencode-native", :opencode,
       "Use the real OpenCode runtime for the Linux v2 stage replay witness at #{hold}. Do not publish."}
    end) ++
    Enum.map(@ack_loss_stages, fn stage ->
      {"stage_replay_ack_loss_#{stage}", "opencode-native", :opencode,
       "Use the real OpenCode runtime for the Linux v2 lost-ACK replay witness at #{stage}. Do not publish."}
    end) ++
    [
      {"stage_replay_stale_journal_run_key_setup", "opencode-native", :opencode,
       "Use the real OpenCode runtime to seed a completed Linux v2 journal for stale RunKey denial. Do not publish."},
      {"stage_replay_stale_journal_run_key_request", "opencode-native", :opencode,
       "Use the real OpenCode runtime for the second fresh Claim in stale RunKey denial. Do not publish."},
      {"stage_replay_stale_journal_run_key_legacy_setup", "opencode-native", :opencode,
       "Use the real OpenCode runtime to seed the preserved original stale RunKey denial witness. Do not publish."},
      {"stage_replay_stale_journal_run_key_legacy_request", "opencode-native", :opencode,
       "Use the real OpenCode runtime for the preserved original stale RunKey denial Claim. Do not publish."},
      {"stage_replay_stale_journal_fence_setup", "opencode-native", :opencode,
       "Use the real OpenCode runtime to seed a completed Linux v2 journal for stale fence denial. Do not publish."},
      {"stage_replay_stale_journal_subject_setup", "opencode-native", :opencode,
       "Use the real OpenCode runtime to seed a completed Linux v2 journal for stale Subject denial. Do not publish."},
      {"stage_replay_receipt_persist_failure", "opencode-native", :opencode,
       "Use the real OpenCode runtime for the Linux v2 receipt persistence failure witness. Do not publish."},
      {"stage_replay_stale_leaf", "opencode-native", :opencode,
       "Use the real OpenCode runtime for the Linux v2 stale cgroup-leaf identity witness. Do not publish."},
      {"stage_replay_alternate_mount", "opencode-native", :opencode,
       "Use the real OpenCode runtime for the Linux v2 alternate cgroup-mount refusal witness. Do not publish."},
      {"stage_replay_migration", "opencode-native", :opencode,
       "Use the real OpenCode runtime for the Linux v2 migration denial witness. Do not publish."},
      {"stage_replay_literal_pid_reuse", "opencode-native", :opencode,
       "Use the real OpenCode runtime for the exact Linux v2 literal PID-reuse witness. Do not publish."},
      {"stage_replay_helper_exit_after_release_before_native_register", "opencode-native", :opencode,
       "Use the real OpenCode runtime for the installed helper-exit-after-RELEASE-before-native-register witness. Do not publish."},
      {"stage_replay_helper_stop_response_loss", "opencode-native", :opencode,
       "Use the real OpenCode runtime for the Linux v2 helper stop-response-loss replay witness. Do not publish."},
      {"stage_replay_helper_ack_stop_receipt_response_loss", "opencode-native", :opencode,
       "Use the real OpenCode runtime for the Linux v2 helper stop-receipt-response-loss replay witness. Do not publish."},
      {"stage_replay_helper_launcher_response_loss", "opencode-native", :opencode,
       "Use the real OpenCode runtime for the Linux v2 helper launcher-response-loss replay witness. Do not publish."},
      {"stage_replay_unprovisioned_refusal", "opencode-native", :opencode,
       "Use the real OpenCode runtime for the unprovisioned Linux owner refusal witness. Do not publish."}
    ]

  def run!("prepare", [output_path, repository_ref, repository_owner, commit, tree_digest]) do
    require_ephemeral_ci!()
    start_repo!()
    validate_subject!(repository_ref, commit, tree_digest)
    suffix = Ecto.UUID.generate() |> String.replace("-", "")

    {:ok, project} =
      Workspaces.create_project(%{
        name: "Linux v2 isolated owner witness #{suffix}",
        key: "V2" <> (String.slice(suffix, 0, 8) |> String.upcase()),
        default_agent_profile: "pi-native",
        default_workspace: "native"
      })

    {:ok, connection} =
      Integrations.create_connection(%{
        provider: "github",
        name: "linux-v2-owner-witness-#{suffix}",
        account_ref: repository_owner,
        capabilities: ["repositories"]
      })

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

    work_items =
      Map.new(@cases, fn {case_name, profile, harness, description} ->
        repository_id = if harness == :pi, do: pi_repository.id, else: opencode_repository.id

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

    unless map_size(work_items) == @expected_fixture_runs do
      raise "owner-witness fixture Run count changed: expected #{@expected_fixture_runs}, got #{map_size(work_items)}"
    end

    write_private_json!(output_path, %{
      "version" => 1,
      "project_id" => project.id,
      "repository_ref" => repository_ref,
      "subject" => %{"commit" => commit, "tree_digest" => tree_digest},
      "pi_repository_resource_id" => pi_repository.id,
      "opencode_repository_resource_id" => opencode_repository.id,
      "work_items" => work_items
    })
  end

  def run!("schedule", [fixture_path, case_name, output_path]) do
    require_ephemeral_ci!()
    start_repo!()
    fixture = read_json!(fixture_path)
    unless map_size(fixture["work_items"]) == @expected_fixture_runs do
      raise "owner-witness fixture Run count changed before assignment: expected #{@expected_fixture_runs}"
    end
    {^case_name, profile, harness, _description} =
      Enum.find(@cases, &(elem(&1, 0) == case_name)) || raise "unknown owner-witness case"
    request_case = if String.starts_with?(case_name, "stage_replay_"), do: "stage_replay", else: case_name

    item = Map.fetch!(fixture["work_items"], case_name)
    task_id = launch_one_shot_work_item!(item["work_item_id"], case_name)
    run = await_assigned_run!(task_id, 30_000)
    runtime = Repo.get!(Runtime, run.runtime_id)
    provider_access? = Map.get(runtime.capabilities || %{}, "provider_access", false)

    unless runtime.agent_profile == profile and runtime.workspace == "native" and
             runtime.repository_resource_id == item["repository_resource_id"] and
             not Map.has_key?(runtime.capabilities || %{}, "adapter") and
             provider_access? == (harness == :pi) do
      raise "generic Control claim runtime did not match the exact non-native Work profile/resource"
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

  defp start_repo! do
    {:ok, _started} = Application.ensure_all_started(:ecto_sql)

    case Repo.start_link() do
      {:ok, _pid} -> :ok
      {:error, {:already_started, _pid}} -> :ok
      {:error, reason} -> raise "could not start isolated Control fixture repository: #{inspect(reason)}"
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
    {:ok, %{task: %{task: task}}, :created} =
      Workspaces.launch_work_item(work_item_id, "linux-v2-owner-#{case_name}", notify: false)

    case Repo.update_all(from(current in Task, where: current.id == ^task.id),
           set: [max_run_attempts: 1]
         ) do
      {1, _} -> task.id
      other -> raise "could not pin one legacy Run attempt for #{case_name}: #{inspect(other)}"
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
