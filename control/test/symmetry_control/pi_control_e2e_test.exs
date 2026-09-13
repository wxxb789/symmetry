defmodule SymmetryControl.PiControlE2ETest do
  @moduledoc """
  Opt-in real Pi/Control/daemon integration evidence.

  This test uses an explicitly named test admission witness. The witness gives
  the test child a fixed, scoped capability projection while delegating native
  execution to the real Pi 0.85.1 adapter. The loopback upstream is synthetic,
  noncredentialed, and configured through a numeric loopback address. This test
  does not prove firewall or network-namespace isolation and makes no
  production capability, provider accounting, or Goal-completion claim.
  """

  use SymmetryControlWeb.ConnCase, async: false

  import Ecto.Query

  alias SymmetryControl.Goals

  alias SymmetryControl.Goals.{
    Goal,
    GoalDecision,
    HarnessSession,
    HarnessSessionAttachReceipt,
    HarnessSessionStopReceipt
  }

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Run, RunEvent, RunTransition, Runtime, Task}
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces

  @moduletag :pi_control_e2e
  @moduletag timeout: 180_000
  # Keep the owner alive through the test ceiling and bounded daemon teardown;
  # the finite value still detects a stalled cleanup instead of masking it.
  @moduletag sandbox_ownership_timeout: 240_000
  @moduletag skip: System.get_env("SYMMETRY_PI_CONTROL_E2E") != "1"

  @pi_version "0.85.1"
  @pi_provider "symmetry-control-loopback"
  @pi_model "gpt-5.6-terra"
  @pi_api "openai-responses"
  @pi_api_key "symmetry-control-loopback-test-key"
  @pi_executable_sha256 "2D4D351DA30BFE23A473032E66A571B238763565AA93754E74F4A939DE13F195"
  @witness_adapter_version "symmetry-test:pi-control-loopback-witness-v1"
  @artifact_path "pi-control-e2e-artifact.txt"
  @artifact_content "real Pi Control E2E artifact\n"
  @resume_artifact_path "pi-control-e2e-resumed-artifact.txt"
  @journal_read_limit 4_194_304
  @daemon_stop_timeout_ms 10_000
  @daemon_kill_timeout_ms 5_000
  @daemon_port_timeout_ms 5_000
  @daemon_startup_identity_timeout_ms 5_000
  @daemon_startup_identity_initial_backoff_ms 10
  @daemon_startup_identity_max_backoff_ms 100
  @daemon_startup_drain_timeout_ms 250
  @daemon_startup_retry_timeout_ms 15_000
  @daemon_startup_retry_initial_backoff_ms 25
  @daemon_startup_retry_max_backoff_ms 100

  setup_all do
    daemon_dir = Path.expand("../../../daemon", __DIR__)

    build_dir =
      Path.join(System.tmp_dir!(), "symmetry-pi-control-e2e-#{Ecto.UUID.generate()}")

    File.rm_rf!(build_dir)
    File.mkdir_p!(build_dir)
    on_exit(fn -> File.rm_rf!(build_dir) end)

    extension = if match?({:win32, _}, :os.type()), do: ".exe", else: ""

    daemon =
      build_witness_binary!(daemon_dir, build_dir, "symmetry-pi-control-witness#{extension}")

    {:ok, daemon: daemon}
  end

  setup context do
    assert_supported_platform!()
    executable = required_pi_executable!()
    assert_pi_version!(executable)

    previous_orchestration = Application.fetch_env!(:symmetry_control, :orchestration)

    Application.put_env(
      :symmetry_control,
      :orchestration,
      Keyword.merge(previous_orchestration,
        heartbeat_interval_ms: 100,
        poll_interval_ms: 100,
        lease_duration_ms: 30_000
      )
    )

    on_exit(fn ->
      Application.put_env(:symmetry_control, :orchestration, previous_orchestration)
    end)

    root =
      Path.join(System.tmp_dir!(), "symmetry-pi-control-run-#{Ecto.UUID.generate()}")

    state_dir = Path.join(root, "state")
    process_marker_path = Path.join(root, "process-marker.json")
    repository_path = Path.join(root, "repository")
    workspace_root = Path.join(root, "worktrees")
    agent_dir = Path.join(root, "agent")
    home_dir = Path.join(root, "home")
    cache_dir = Path.join(root, "cache")
    data_dir = Path.join(root, "data")
    temp_dir = Path.join(root, "tmp")
    appdata_dir = Path.join(root, "appdata")
    localappdata_dir = Path.join(root, "localappdata")

    Enum.each(
      [
        state_dir,
        repository_path,
        workspace_root,
        agent_dir,
        home_dir,
        cache_dir,
        data_dir,
        temp_dir,
        appdata_dir,
        localappdata_dir
      ],
      &File.mkdir_p!/1
    )

    on_exit(fn -> File.rm_rf!(root) end)
    put_env_with_restore("SYMMETRY_PI_CONTROL_E2E_PROCESS_RECORD", process_marker_path)

    gateway_state =
      start_supervised!(
        {Agent,
         fn ->
           %{
             responses: 0,
             requests: [],
             authorizations: [],
             errors: [],
             expected_responses: 2,
             gated_responses: [2],
             tool_calls: %{1 => %{path: @artifact_path, content: @artifact_content}},
             opaque_marker: nil,
             resume_marker: nil,
             first_call_id: nil,
             first_item_id: nil,
             first_assistant_id: nil,
             waiters: %{},
             final_waiter: nil,
             subject: nil,
             subject_hash: nil,
             artifact_path: @artifact_path,
             artifact_content: @artifact_content
           }
         end},
        id: {:pi_control_gateway_state, Ecto.UUID.generate()}
      )

    gateway_listener =
      start_supervised!(
        {Bandit,
         plug: {&loopback_gateway_plug/2, gateway_state},
         scheme: :http,
         ip: {127, 0, 0, 1},
         port: 0,
         startup_log: false},
        id: {:pi_control_gateway, Ecto.UUID.generate()}
      )

    {:ok, {{127, 0, 0, 1}, gateway_port}} = ThousandIsland.listener_info(gateway_listener)
    gateway_url = "http://127.0.0.1:#{gateway_port}/v1"

    write_loopback_models!(agent_dir, gateway_url)

    isolate_child_environment!(
      root,
      agent_dir,
      home_dir,
      cache_dir,
      data_dir,
      temp_dir,
      appdata_dir,
      localappdata_dir
    )

    profile = "pi-control-e2e"
    workspace = "pi-control-e2e"
    fixture = create_goal_fixture!(repository_path, profile, workspace)

    Agent.update(gateway_state, fn state ->
      %{state | subject: fixture.subject, subject_hash: subject_hash!(fixture.subject)}
    end)

    control_listener =
      start_supervised!(
        {Bandit,
         plug: SymmetryControlWeb.Endpoint,
         scheme: :http,
         ip: {127, 0, 0, 1},
         port: 0,
         startup_log: false},
        id: {:pi_control_endpoint, Ecto.UUID.generate()}
      )

    {:ok, {{127, 0, 0, 1}, control_port}} = ThousandIsland.listener_info(control_listener)

    config_path = Path.join(root, "daemon.json")

    write_daemon_config!(config_path, %{
      control_port: control_port,
      state_dir: state_dir,
      profile: profile,
      workspace: workspace,
      executable: executable,
      repository_path: repository_path,
      workspace_root: workspace_root,
      commit: fixture.subject["commit"],
      repository_resource_id: fixture.repository_resource_id
    })

    daemon_process = start_daemon!(context.daemon, config_path)
    on_exit(fn -> stop_daemon!(daemon_process) end)

    await!(daemon_process.port, "Pi witness runtime registration", fn ->
      case Repo.get_by(Runtime, runtime_key: fixture.runtime_key) do
        %Runtime{
          status: "online",
          harness_kind: "pi",
          harness_version: @pi_version,
          adapter_version: @witness_adapter_version,
          capabilities: %{
            "adapter" => %{
              "operations" => %{
                "start" => true,
                "events" => true,
                "cancel" => true,
                "resume" => true,
                "pause" => "unsupported",
                "usage" => "unknown"
              }
            }
          }
        } ->
          :ok

        _ ->
          :retry
      end
    end)

    {:ok,
     daemon_process: daemon_process,
     daemon_port: daemon_process.port,
     daemon_os_pid: daemon_process.os_pid,
     config_path: config_path,
     state_dir: state_dir,
     process_marker_path: process_marker_path,
     repository_path: repository_path,
     workspace_root: workspace_root,
     gateway_state: gateway_state,
     fixture: fixture,
     daemon: context.daemon,
     profile: profile,
     workspace: workspace}
  end

  test "real Pi repository work crosses Control receipts without accepting the Goal", context do
    IO.puts(
      "PI CONTROL E2E: test admission witness + synthetic numeric-loopback upstream; no credential forwarding, production capability/provider/accounting, firewall, network-namespace, native opaque-token continuity, or Goal completion claim"
    )

    %{fixture: fixture, gateway_state: gateway_state} = context
    runtime = Repo.get_by!(Runtime, runtime_key: fixture.runtime_key)

    assigned = assign_goal_task!(context.daemon_port, fixture.task.id)
    assert assigned.task_id == fixture.task.id
    assert assigned.runtime_id == runtime.id
    assert assigned.generation == fixture.task.attempt_generation

    run_key = {assigned.id, assigned.generation}

    # Hold the synthetic provider's final response until the native process and
    # gateway waiter are both observable. This creates a stable proof point
    # before the final response can trigger terminal journal cleanup.
    await!(context.daemon_port, "native Pi final response waiter", fn ->
      case Agent.get(gateway_state, & &1.final_waiter) do
        pid when is_pid(pid) -> :ok
        _ -> :retry
      end
    end)

    process_marker =
      await_process_marker!(
        context.process_marker_path,
        run_key,
        "native process identity and isolated worktree"
      )

    workspace_path =
      Path.join([
        context.workspace_root,
        "binding-pi-control-e2e",
        "run-#{assigned.id}",
        "generation-#{assigned.generation}"
      ])

    assert process_marker["pid"] > 0

    assert is_binary(process_marker["process_identity"]) and
             process_marker["process_identity"] != ""

    assert is_binary(process_marker["started_at"]) and process_marker["started_at"] != ""
    assert File.dir?(workspace_path)
    refute File.exists?(Path.join(context.repository_path, @artifact_path))

    claimed_run = Repo.get_by!(Run, id: assigned.id)
    assert claimed_run.state in ["claimed", "running"]

    {:ok, journal} = journal_for_run(context.state_dir, run_key)
    assert journal["run_id"] == assigned.id
    assert journal["generation"] == assigned.generation
    assert journal["runtime_id"] == runtime.id
    assert journal["claimed_runtime_epoch"] == claimed_run.claimed_runtime_epoch
    assert journal["claim_id"] == claimed_run.claim_id
    assert journal["lease_token"] == claimed_run.lease_token
    assert journal["pid"] == process_marker["pid"]
    assert journal["process_identity"] == process_marker["process_identity"]
    assert normalize_path(journal["workspace_path"]) == normalize_path(workspace_path)
    assert File.dir?(journal["workspace_path"])

    release_gateway_final!(gateway_state)

    completed_task =
      await!(context.daemon_port, "completed Goal task", fn ->
        case Repo.get(Task, fixture.task.id) do
          %Task{state: "completed"} = task ->
            {:ok, task}

          %Task{state: "failed", failure: failure} ->
            flunk("Pi Control E2E task failed: #{inspect(failure)}")

          _ ->
            :retry
        end
      end)

    completed_run = Repo.get_by!(Run, id: assigned.id)
    assert completed_run.state == "completed"
    assert completed_run.generation == assigned.generation
    assert completed_run.claimed_runtime_epoch > 0
    assert completed_run.claim_id
    assert completed_run.lease_token
    assert completed_run.harness_session_id
    assert completed_run.harness_binding_id
    assert completed_run.provider_access_snapshot == %{"v" => 1, "kind" => "none"}

    result = completed_run.result["task_result"]
    assert result["schema_version"] == "symmetry.task_result.v1"
    assert result["kind"] == "progress"
    assert result["subject"] == fixture.subject
    assert result["subject_hash"] == subject_hash!(fixture.subject)
    assert result["evidence_refs"] == []
    assert result["reason"] == nil

    artifact = Path.join(workspace_path, @artifact_path)
    assert File.read!(artifact) == @artifact_content

    assert git_status!(workspace_path)
           |> String.split("\n", trim: true)
           |> Enum.sort() ==
             ["?? .symmetry-workspace.json", "?? #{@artifact_path}"]

    assert Repo.aggregate(
             from(event in RunEvent, where: event.run_id == ^completed_run.id),
             :count
           ) > 0

    assert Repo.exists?(
             from event in RunEvent,
               where: event.run_id == ^completed_run.id and event.kind == "native_frame"
           )

    assert Repo.exists?(
             from transition in RunTransition,
               where: transition.run_id == ^completed_run.id and transition.state == "completed"
           )

    {attach_receipt, stop_receipt} =
      await!(context.daemon_port, "session attach and stop receipts", fn ->
        attach = Repo.get_by(HarnessSessionAttachReceipt, run_id: completed_run.id)
        stop = Repo.get_by(HarnessSessionStopReceipt, run_id: completed_run.id)
        if attach && stop, do: {:ok, {attach, stop}}, else: :retry
      end)

    assert attach_receipt.session_id == completed_run.harness_session_id
    assert attach_receipt.response["session"]["binding_id"] == completed_run.harness_binding_id
    assert stop_receipt.session_id == completed_run.harness_session_id
    assert stop_receipt.binding_id == completed_run.harness_binding_id

    session = Repo.get!(HarnessSession, completed_run.harness_session_id)
    assert session.state == "available"
    assert is_nil(session.active_run_id)
    assert session.harness_kind == "pi"
    assert session.harness_version == @pi_version
    assert session.adapter_version == @witness_adapter_version
    assert session.binding_verified
    assert session.local_handle_id
    assert session.binding_id == completed_run.harness_binding_id

    usage_query =
      from usage in SymmetryControl.Goals.RunUsage,
        where: usage.run_id == ^completed_run.id

    run_usage_count = Repo.aggregate(usage_query, :count)

    assert run_usage_count == 1
    usage = Repo.one!(usage_query)
    assert usage.provider == "unknown"
    assert usage.model == "unknown"
    assert usage.cost_basis == "unknown"
    assert is_nil(usage.input_tokens)
    assert is_nil(usage.output_tokens)

    settlement =
      await!(context.daemon_port, "non-accepting Goal settlement", fn ->
        case Goals.settle_task(completed_task.id, completed_run.id, completed_run.generation) do
          {:ok, %{"settlement" => settlement} = response}
          when settlement in ["progress", "no_verified_progress"] ->
            {:ok, response}

          {:error, :stale_run} ->
            :retry

          other ->
            flunk("unexpected Goal settlement: #{inspect(other)}")
        end
      end)

    assert settlement["settlement"] in ["progress", "no_verified_progress"]
    refute settlement["settlement"] in ["accepted", "awaiting_validation"]
    assert Repo.get!(Goal, fixture.goal.id).state == "active"

    refute Repo.exists?(
             from outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.work_item_id == ^fixture.item.id
           )

    await_empty_goal_outbox!(
      context.daemon_port,
      context.state_dir,
      run_key
    )

    gateway = Agent.get(gateway_state, & &1)
    assert gateway.errors == []
    assert gateway.responses == 2
    assert Enum.count(gateway.authorizations) > 0
    assert Enum.all?(gateway.authorizations, &(&1 == "Bearer #{@pi_api_key}"))
    assert Enum.count(gateway.requests) == 2
    assert Enum.all?(gateway.requests, &is_map/1)
  end

  @tag :pi_control_resume
  test "real Pi retained resume crosses a fresh daemon with fenced Control lineage", context do
    %{fixture: fixture, gateway_state: gateway_state} = context
    opaque_marker = "opaque-" <> String.replace(Ecto.UUID.generate(), "-", "")
    first_call_id = "call_pi_control_e2e_#{opaque_marker}"
    first_item_id = "fc_pi_control_e2e_#{opaque_marker}"
    first_assistant_id = "msg_pi_control_e2e_#{opaque_marker}"

    Agent.update(gateway_state, fn state ->
      %{
        state
        | expected_responses: 4,
          gated_responses: [2, 4],
          tool_calls: %{
            1 => %{path: @artifact_path, content: @artifact_content},
            3 => %{path: @resume_artifact_path, content: nil}
          },
          opaque_marker: opaque_marker,
          first_call_id: first_call_id,
          first_item_id: first_item_id,
          first_assistant_id: first_assistant_id
      }
    end)

    runtime = Repo.get_by!(Runtime, runtime_key: fixture.runtime_key)
    assigned = assign_goal_task!(context.daemon_port, fixture.task.id)
    assert assigned.task_id == fixture.task.id
    assert assigned.runtime_id == runtime.id
    assert assigned.generation == fixture.task.attempt_generation
    run_key = {assigned.id, assigned.generation}

    await_gateway_waiter!(context.daemon_port, gateway_state, 2, "first Pi final response waiter")

    first_process_marker =
      await_process_marker!(
        context.process_marker_path,
        run_key,
        "first native Pi process identity"
      )

    first_workspace_path =
      Path.join([
        context.workspace_root,
        "binding-pi-control-e2e",
        "run-#{assigned.id}",
        "generation-#{assigned.generation}"
      ])

    first_claimed_run = Repo.get_by!(Run, id: assigned.id)
    assert first_claimed_run.state in ["claimed", "running"]
    assert first_claimed_run.harness_session_id
    assert first_claimed_run.harness_binding_id
    assert first_process_marker["pid"] > 0
    assert is_binary(first_process_marker["process_identity"])
    assert File.dir?(first_workspace_path)

    {:ok, first_journal} = journal_for_run(context.state_dir, run_key)
    assert first_journal["run_id"] == assigned.id
    assert first_journal["generation"] == assigned.generation
    assert first_journal["runtime_id"] == runtime.id
    assert first_journal["claimed_runtime_epoch"] == first_claimed_run.claimed_runtime_epoch
    assert first_journal["claim_id"] == first_claimed_run.claim_id
    assert first_journal["lease_token"] == first_claimed_run.lease_token
    assert first_journal["pid"] == first_process_marker["pid"]
    assert first_journal["process_identity"] == first_process_marker["process_identity"]

    assert normalize_path(first_journal["workspace_path"]) ==
             normalize_path(first_workspace_path)

    release_gateway_response!(gateway_state, 2)

    first_task =
      await!(context.daemon_port, "first completed Goal task", fn ->
        case Repo.get(Task, fixture.task.id) do
          %Task{state: "completed"} = task ->
            {:ok, task}

          %Task{state: "failed", failure: failure} ->
            flunk("first Pi resume task failed: #{inspect(failure)}")

          _ ->
            :retry
        end
      end)

    first_run = Repo.get_by!(Run, id: assigned.id)
    assert first_run.state == "completed"
    assert first_run.harness_session_id
    assert first_run.harness_binding_id
    assert first_run.claim_id
    assert first_run.lease_token
    assert first_run.provider_access_snapshot == %{"v" => 1, "kind" => "none"}
    assert first_run.result["task_result"]["subject"] == fixture.subject
    assert first_run.result["task_result"]["subject_hash"] == subject_hash!(fixture.subject)

    assert File.read!(Path.join(first_workspace_path, @artifact_path)) == @artifact_content

    assert git_status!(first_workspace_path) |> String.split("\n", trim: true) |> Enum.sort() ==
             ["?? .symmetry-workspace.json", "?? #{@artifact_path}"]

    assert Repo.exists?(
             from event in RunEvent,
               where: event.run_id == ^first_run.id and event.kind == "native_frame"
           )

    assert Repo.exists?(
             from transition in RunTransition,
               where: transition.run_id == ^first_run.id and transition.state == "completed"
           )

    {first_attach_receipt, first_stop_receipt} =
      await!(context.daemon_port, "first session attach and stop receipts", fn ->
        attach = Repo.get_by(HarnessSessionAttachReceipt, run_id: first_run.id)
        stop = Repo.get_by(HarnessSessionStopReceipt, run_id: first_run.id)
        if attach && stop, do: {:ok, {attach, stop}}, else: :retry
      end)

    assert first_attach_receipt.session_id == first_run.harness_session_id
    assert first_attach_receipt.response["session"]["binding_id"] == first_run.harness_binding_id
    assert first_stop_receipt.session_id == first_run.harness_session_id
    assert first_stop_receipt.binding_id == first_run.harness_binding_id

    first_session = Repo.get!(HarnessSession, first_run.harness_session_id)
    assert first_session.state == "available"
    assert is_nil(first_session.active_run_id)
    assert first_session.binding_id == first_run.harness_binding_id

    first_usage_query =
      from usage in SymmetryControl.Goals.RunUsage, where: usage.run_id == ^first_run.id

    assert Repo.aggregate(first_usage_query, :count) == 1
    first_usage = Repo.one!(first_usage_query)
    assert first_usage.provider == "unknown"
    assert first_usage.model == "unknown"
    assert first_usage.cost_basis == "unknown"
    assert is_nil(first_usage.input_tokens)
    assert is_nil(first_usage.output_tokens)

    first_settlement =
      await!(context.daemon_port, "first non-accepting Goal settlement", fn ->
        case Goals.settle_task(first_task.id, first_run.id, first_run.generation) do
          {:ok, %{"settlement" => settlement} = response}
          when settlement in ["progress", "no_verified_progress"] ->
            {:ok, response}

          {:error, :stale_run} ->
            :retry

          other ->
            flunk("unexpected first Goal settlement: #{inspect(other)}")
        end
      end)

    assert first_settlement["settlement"] in ["progress", "no_verified_progress"]
    refute first_settlement["settlement"] in ["accepted", "awaiting_validation"]
    assert Repo.get!(Goal, fixture.goal.id).state == "active"

    refute Repo.exists?(
             from outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.work_item_id == ^fixture.item.id
           )

    await_empty_goal_outbox!(context.daemon_port, context.state_dir, run_key)
    first_session_journal = goal_session_journal_for_goal!(context.state_dir, fixture.goal.id)
    assert first_session_journal["session_state"] == "available"
    assert first_session_journal["session_mode"] == "fresh"

    assert is_binary(first_session_journal["native_session_id"]) and
             first_session_journal["native_session_id"] != ""

    assert is_binary(first_session_journal["native_session_filename"]) and
             first_session_journal["native_session_filename"] != ""

    assert is_binary(first_session_journal["local_handle_id"]) and
             first_session_journal["local_handle_id"] != ""

    first_native_session_id = first_session_journal["native_session_id"]
    first_native_session_filename = first_session_journal["native_session_filename"]
    first_local_handle_id = first_session_journal["local_handle_id"]
    assert first_session_journal["stop_certificate"]["run_id"] == first_run.id
    assert first_session_journal["stop_certificate"]["binding_id"] == first_run.harness_binding_id
    assert first_session_journal["control_session_id"] == first_run.harness_session_id
    assert first_session_journal["control_attachment_receipt_id"] == first_attach_receipt.id

    assert first_session_journal["control_attachment_lineage"]["original"]["run_id"] ==
             first_run.id

    assert first_session_journal["control_attachment_lineage"]["original"]["generation"] ==
             first_run.generation

    assert first_session_journal["control_attachment_lineage"]["original"]["session_id"] ==
             first_run.harness_session_id

    assert first_session_journal["control_attachment_lineage"]["original"]["binding_id"] ==
             first_run.harness_binding_id

    assert first_session_journal["control_attachment_lineage"]["original"]["receipt_id"] ==
             first_attach_receipt.id

    assert first_session_journal["control_attachment_lineage"]["current"]["run_id"] ==
             first_run.id

    assert first_session_journal["control_attachment_lineage"]["current"]["receipt_id"] ==
             first_attach_receipt.id

    stop_daemon!(context.daemon_process)

    first_runtime = Repo.get!(Runtime, runtime.id)
    first_epoch = first_runtime.connection_epoch

    second_daemon = start_daemon!(context.daemon, context.config_path)
    on_exit(fn -> stop_daemon!(second_daemon) end)
    runtime_id = runtime.id

    restarted_runtime =
      await!(second_daemon.port, "same runtime after daemon restart", fn ->
        case Repo.get_by(Runtime, runtime_key: fixture.runtime_key) do
          %Runtime{id: ^runtime_id, status: "online", connection_epoch: epoch} = current
          when epoch > first_epoch and runtime_id == runtime.id ->
            {:ok, current}

          _ ->
            :retry
        end
      end)

    assert restarted_runtime.id == first_runtime.id
    assert restarted_runtime.connection_epoch > first_epoch
    assert restarted_runtime.capabilities["adapter"]["operations"]["resume"] == true

    assert {:ok, second_admission, :created} =
             command_current(fixture.goal.id, "admit_task", %{
               work_item_id: fixture.item.id,
               purpose: "implement",
               model_profile: context.profile,
               session_mode: "resume",
               requested_session_id: first_run.harness_session_id,
               validation_of_task_id: nil
             })

    second_task = Repo.get!(Task, second_admission.response["task"]["id"])
    assert second_task.work_item_id == fixture.item.id
    assert second_task.requested_session_id == first_run.harness_session_id
    assert second_task.input["session_mode"] == "resume"
    assert second_task.input["requested_session_id"] == first_run.harness_session_id
    assert second_task.input["subject"] == fixture.subject

    second_assigned = assign_goal_task!(second_daemon.port, second_task.id)
    assert second_assigned.task_id == second_task.id
    assert second_assigned.runtime_id == restarted_runtime.id
    assert second_assigned.generation == second_task.attempt_generation
    refute second_assigned.id == assigned.id
    second_run_key = {second_assigned.id, second_assigned.generation}

    await!(second_daemon.port, "resumed Pi final response waiter", fn ->
      case Repo.get(Task, second_task.id) do
        %Task{state: "failed", failure: failure} ->
          failed_run = Repo.get_by(Run, task_id: second_task.id)
          late_usage_files = Path.join(context.state_dir, "goal-usage") |> File.ls()

          flunk(
            "second Pi resume task failed before gateway response 4: #{inspect(failure)} " <>
              "first_run=#{first_run.id}/#{first_run.generation} " <>
              "second_run=#{inspect(failed_run && {failed_run.id, failed_run.generation})} " <>
              "late_usage_files=#{inspect(late_usage_files)}"
          )

        _ ->
          case Agent.get(gateway_state, &Map.get(&1.waiters, 4)) do
            pid when is_pid(pid) -> :ok
            _ -> :retry
          end
      end
    end)

    second_process_marker =
      await_process_marker!(
        context.process_marker_path,
        second_run_key,
        "resumed native Pi process identity"
      )

    assert second_process_marker["pid"] > 0
    assert is_binary(second_process_marker["process_identity"])

    second_claimed_run = Repo.get_by!(Run, id: second_assigned.id)
    assert second_claimed_run.state in ["claimed", "running"]
    assert second_claimed_run.runtime_id == restarted_runtime.id
    assert second_claimed_run.harness_session_id == first_run.harness_session_id
    refute second_claimed_run.harness_binding_id == first_run.harness_binding_id
    refute second_claimed_run.claim_id == first_run.claim_id
    refute second_claimed_run.lease_token == first_run.lease_token

    {:ok, second_journal} = journal_for_run(context.state_dir, second_run_key)
    assert second_journal["run_id"] == second_assigned.id
    assert second_journal["generation"] == second_assigned.generation
    assert second_journal["runtime_id"] == restarted_runtime.id
    assert second_journal["claimed_runtime_epoch"] == second_claimed_run.claimed_runtime_epoch
    assert second_journal["claim_id"] == second_claimed_run.claim_id
    assert second_journal["lease_token"] == second_claimed_run.lease_token
    assert second_journal["pid"] == second_process_marker["pid"]
    assert second_journal["process_identity"] == second_process_marker["process_identity"]

    assert normalize_path(second_journal["workspace_path"]) ==
             normalize_path(first_workspace_path)

    assert Agent.get(gateway_state, & &1.resume_marker) == opaque_marker

    first_fence = %{
      runtime_id: first_run.runtime_id,
      runtime_epoch: first_run.claimed_runtime_epoch,
      generation: first_run.generation,
      claim_id: first_run.claim_id,
      lease_token: first_run.lease_token
    }

    first_stop_response = first_stop_receipt.response

    assert {:ok, ^first_stop_response, :replayed} =
             Goals.mark_harness_session_stopped(
               runtime.machine_id,
               first_run.id,
               first_fence,
               %{
                 session_id: first_run.harness_session_id,
                 local_handle_id: first_session.local_handle_id,
                 binding_id: first_run.harness_binding_id
               }
             )

    second_run_id = second_claimed_run.id

    assert %{state: "busy", active_run_id: ^second_run_id, binding_id: second_binding_id} =
             Repo.get!(HarnessSession, first_run.harness_session_id)

    assert second_run_id == second_claimed_run.id
    assert second_binding_id == second_claimed_run.harness_binding_id

    release_gateway_response!(gateway_state, 4)

    second_completed_task =
      await!(second_daemon.port, "second completed Goal task", fn ->
        case Repo.get(Task, second_task.id) do
          %Task{state: "completed"} = task ->
            {:ok, task}

          %Task{state: "failed", failure: failure} ->
            flunk("second Pi resume task failed: #{inspect(failure)}")

          _ ->
            :retry
        end
      end)

    second_run = Repo.get_by!(Run, id: second_assigned.id)
    assert second_run.state == "completed"
    assert second_run.harness_session_id == first_run.harness_session_id
    assert second_run.harness_binding_id == second_claimed_run.harness_binding_id
    assert second_run.result["task_result"]["subject"] == fixture.subject
    assert second_run.result["task_result"]["subject_hash"] == subject_hash!(fixture.subject)

    assert File.read!(Path.join(first_workspace_path, @artifact_path)) == @artifact_content

    assert File.read!(Path.join(first_workspace_path, @resume_artifact_path)) ==
             opaque_marker <> "\n"

    assert git_status!(first_workspace_path) |> String.split("\n", trim: true) |> Enum.sort() ==
             [
               "?? .symmetry-workspace.json",
               "?? #{@artifact_path}",
               "?? #{@resume_artifact_path}"
             ]

    {second_attach_receipt, second_stop_receipt} =
      await!(second_daemon.port, "second session attach and stop receipts", fn ->
        attach = Repo.get_by(HarnessSessionAttachReceipt, run_id: second_run.id)
        stop = Repo.get_by(HarnessSessionStopReceipt, run_id: second_run.id)
        if attach && stop, do: {:ok, {attach, stop}}, else: :retry
      end)

    assert second_attach_receipt.session_id == first_run.harness_session_id

    assert second_attach_receipt.response["session"]["binding_id"] ==
             second_run.harness_binding_id

    assert second_stop_receipt.session_id == first_run.harness_session_id
    assert second_stop_receipt.binding_id == second_run.harness_binding_id
    refute second_stop_receipt.binding_id == first_stop_receipt.binding_id

    second_session = Repo.get!(HarnessSession, first_run.harness_session_id)
    assert second_session.state == "available"
    assert is_nil(second_session.active_run_id)
    assert second_session.binding_id == second_run.harness_binding_id

    second_usage_query =
      from usage in SymmetryControl.Goals.RunUsage, where: usage.run_id == ^second_run.id

    assert Repo.aggregate(second_usage_query, :count) == 1
    second_usage = Repo.one!(second_usage_query)
    assert second_usage.provider == "unknown"
    assert second_usage.model == "unknown"
    assert second_usage.cost_basis == "unknown"
    assert is_nil(second_usage.input_tokens)
    assert is_nil(second_usage.output_tokens)

    second_settlement =
      await!(second_daemon.port, "second non-accepting Goal settlement", fn ->
        case Goals.settle_task(second_completed_task.id, second_run.id, second_run.generation) do
          {:ok, %{"settlement" => settlement} = response}
          when settlement in ["progress", "no_verified_progress"] ->
            {:ok, response}

          {:error, :stale_run} ->
            :retry

          other ->
            flunk("unexpected second Goal settlement: #{inspect(other)}")
        end
      end)

    assert second_settlement["settlement"] in ["progress", "no_verified_progress"]
    refute second_settlement["settlement"] in ["accepted", "awaiting_validation"]
    assert Repo.get!(Goal, fixture.goal.id).state == "active"

    refute Repo.exists?(
             from outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.work_item_id == ^fixture.item.id
           )

    await_empty_goal_outbox!(second_daemon.port, context.state_dir, second_run_key)
    second_session_journal = goal_session_journal_for_goal!(context.state_dir, fixture.goal.id)
    assert second_session_journal["session_state"] == "available"
    assert second_session_journal["session_mode"] == "resume"
    assert second_session_journal["native_session_id"] == first_native_session_id

    assert normalize_path(second_session_journal["native_session_filename"]) ==
             normalize_path(first_native_session_filename)

    assert second_session_journal["local_handle_id"] == first_local_handle_id
    assert second_session_journal["control_session_id"] == first_run.harness_session_id
    assert second_session_journal["control_attachment_receipt_id"] == second_attach_receipt.id
    assert second_session_journal["stop_certificate"]["run_id"] == second_run.id

    assert second_session_journal["stop_certificate"]["binding_id"] ==
             second_run.harness_binding_id

    assert second_session_journal["control_attachment_lineage"]["original"]["run_id"] ==
             first_run.id

    assert second_session_journal["control_attachment_lineage"]["original"]["generation"] ==
             first_run.generation

    assert second_session_journal["control_attachment_lineage"]["original"]["binding_id"] ==
             first_run.harness_binding_id

    assert second_session_journal["control_attachment_lineage"]["original"]["session_id"] ==
             first_run.harness_session_id

    assert second_session_journal["control_attachment_lineage"]["original"]["receipt_id"] ==
             first_attach_receipt.id

    assert second_session_journal["control_attachment_lineage"]["current"]["run_id"] ==
             second_run.id

    assert second_session_journal["control_attachment_lineage"]["current"]["generation"] ==
             second_run.generation

    assert second_session_journal["control_attachment_lineage"]["current"]["binding_id"] ==
             second_run.harness_binding_id

    assert second_session_journal["control_attachment_lineage"]["current"]["session_id"] ==
             second_run.harness_session_id

    assert second_session_journal["control_attachment_lineage"]["current"]["receipt_id"] ==
             second_attach_receipt.id

    consumed = get_in(second_session_journal, ["consumed_stop_certificates", "certificates"])

    assert Enum.any?(consumed || [], fn certificate ->
             certificate["run_id"] == first_run.id and
               certificate["generation"] == first_run.generation and
               certificate["session_id"] == first_run.harness_session_id and
               certificate["local_handle_id"] == first_local_handle_id and
               certificate["binding_id"] == first_run.harness_binding_id and
               certificate["receipt_id"] == first_stop_receipt.id
           end)

    gateway = Agent.get(gateway_state, & &1)
    assert gateway.errors == []
    assert gateway.responses == 4
    assert gateway.resume_marker == opaque_marker
    assert Enum.count(gateway.requests) == 4
    assert Enum.all?(gateway.requests, &is_map/1)
    assert Enum.count(gateway.authorizations) == 4
    assert Enum.all?(gateway.authorizations, &(&1 == "Bearer #{@pi_api_key}"))
  end

  defp create_goal_fixture!(repository_path, profile, workspace) do
    run_git!(repository_path, ["init", "--quiet"])
    run_git!(repository_path, ["config", "user.email", "symmetry-pi-control-e2e@example.invalid"])
    run_git!(repository_path, ["config", "user.name", "Symmetry Pi Control E2E"])
    File.write!(Path.join(repository_path, "README.md"), "# Pi Control E2E\n")
    run_git!(repository_path, ["add", "README.md"])
    run_git!(repository_path, ["commit", "--quiet", "-m", "initial Pi Control E2E subject"])

    commit = git_output!(repository_path, ["rev-parse", "HEAD"]) |> String.trim()

    subject = %{
      "resource_id" => nil,
      "commit" => commit,
      "tree_digest" => git_tree_digest!(repository_path, commit)
    }

    suffix = String.replace(Ecto.UUID.generate(), "-", "")

    {:ok, project} =
      Workspaces.create_project(%{
        name: "Pi Control E2E #{suffix}",
        key: "P#{String.slice(suffix, 0, 7)}",
        default_agent_profile: profile,
        default_workspace: workspace
      })

    {:ok, resource} =
      Workspaces.create_resource(project.id, %{
        kind: "repository",
        name: "Pi Control E2E repository #{suffix}"
      })

    subject = Map.put(subject, "resource_id", resource.id)
    acceptance = acceptance_contract(resource.id)

    {:ok, created, :created} =
      Goals.create_goal(
        project.id,
        goal_attrs(resource.id, profile, acceptance),
        "operator:pi_control_e2e",
        validation_profiles: []
      )

    goal_id = created.goal.id

    proposal = %{
      schema_version: "symmetry.plan.v1",
      proposal_id: Ecto.UUID.generate(),
      goal_id: goal_id,
      expected_revision: 1,
      items: [
        %{
          key: "implement",
          title: "Perform one real Pi repository write",
          description: "Use the configured real Pi adapter to create one untracked artifact.",
          required: true,
          integration: true,
          repository_resource_id: resource.id,
          acceptance: acceptance,
          depends_on_keys: [],
          model_profile: profile,
          change_target: nil,
          baseline: %{kind: "subject", subject: subject}
        }
      ]
    }

    assert {:ok, decision, :created} =
             command_current(goal_id, "request_decision", %{
               kind: "plan",
               work_item_id: nil,
               subject_hash: nil,
               proposal: proposal
             })

    decision_id = decision.response["decision"]["id"]
    decision_version = Repo.get!(GoalDecision, decision_id).lock_version

    assert {:ok, _resolved, :created} =
             command_current(goal_id, "resolve_decision", %{
               decision_id: decision_id,
               expected_decision_version: decision_version,
               option_id: "accept"
             })

    proposal_hash =
      "sha256:" <> Base.encode16(SymmetryControl.RequestHash.canonical(proposal), case: :lower)

    assert {:ok, _accepted, :created} =
             command_current(goal_id, "accept_plan", %{
               proposal: proposal,
               proposal_hash: proposal_hash,
               decision_id: decision_id
             })

    assert {:ok, _active, :created} =
             command_current(goal_id, "activate", %{approved_revision: 1})

    assert {:ok, %{work_items: [item]}} = Goals.fetch_goal(goal_id)

    assert {:ok, admission, :created} =
             command_current(goal_id, "admit_task", %{
               work_item_id: item.id,
               purpose: "implement",
               model_profile: profile,
               session_mode: "fresh",
               requested_session_id: nil,
               validation_of_task_id: nil
             })

    task = Repo.get!(Task, admission.response["task"]["id"])

    %{
      goal: Repo.get!(Goal, goal_id),
      item: item,
      task: task,
      subject: subject,
      repository_resource_id: resource.id,
      runtime_key: "pi-control-e2e-runtime-#{resource.id}"
    }
  end

  defp goal_attrs(repository_resource_id, profile, acceptance) do
    %{
      schema_version: "symmetry.goal_create.v1",
      title: "Pi Control E2E #{System.unique_integer([:positive])}",
      mutation_id: Ecto.UUID.generate(),
      initial_revision: %{
        objective: "Perform one bounded real Pi repository write through Control.",
        non_goals: [],
        acceptance_contract: acceptance,
        authority_policy: %{
          "operator_required_for_scope_change" => true,
          "operator_required_for_completion" => true,
          "publication_allowed" => false,
          "allowed_actions" => []
        },
        execution_policy: %{
          "automatic_execution" => false,
          "max_parallel_tasks" => 1,
          "max_task_admissions" => 2,
          "max_run_attempts_per_task" => 1,
          "budget_limit_microusd" => nil,
          "per_run_cost_limit_microusd" => nil,
          "budget_mode" => "soft",
          "hard_cost_limit_required" => false,
          "allowed_runtime_ids" => [],
          "allowed_resource_ids" => [repository_resource_id],
          "allowed_model_profiles" => [profile],
          "final_acceptance" => "operator",
          "allowed_actions" => []
        },
        context_manifest: %{
          "byte_budget" => 32_768,
          "required_source_kinds" => ["approved_goal", "work_contract", "repository_subject"],
          "include_advisory_recall" => false
        },
        reason: "Real Pi Control E2E contract"
      }
    }
  end

  defp acceptance_contract(repository_resource_id) do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "One untracked artifact must exist in the daemon-owned worktree.",
      "predicates" => [
        %{
          "id" => "artifact",
          "kind" => "artifact",
          "resource_id" => repository_resource_id,
          "path" => @artifact_path
        }
      ]
    }
  end

  defp command_current(goal_id, kind, payload) do
    goal = Repo.get!(Goal, goal_id)

    Goals.command(
      goal_id,
      %{
        schema_version: "symmetry.goal_command.v1",
        mutation_id: Ecto.UUID.generate(),
        expected_version: goal.lock_version,
        expected_revision: goal.current_revision,
        kind: kind,
        payload: payload
      },
      "operator:pi_control_e2e",
      rollout_enabled: true
    )
  end

  defp assign_goal_task!(daemon_port, task_id) do
    await!(daemon_port, "scheduler assignment", fn ->
      case Orchestration.assign_one() do
        {:ok, %Run{task_id: ^task_id} = run} -> {:ok, run}
        {:ok, _other} -> :retry
        {:error, :no_assignment} -> :retry
      end
    end)
  end

  defp write_daemon_config!(path, values) do
    config = %{
      control_plane_url: "http://127.0.0.1:#{values.control_port}",
      allow_insecure_http: true,
      state_dir: values.state_dir,
      machine_name: "pi-control-e2e-#{Ecto.UUID.generate()}",
      agent_profiles: %{
        values.profile => %{
          command: values.executable,
          args: [
            "--no-extensions",
            "--no-skills",
            "--no-prompt-templates",
            "--no-themes",
            "--no-context-files",
            "--no-approve",
            "--provider",
            @pi_provider,
            "--model",
            @pi_model,
            "--tools",
            "write"
          ],
          input_mode: "json",
          provider_access: false,
          interactive: false,
          supervisory_control: false,
          event_format: "raw",
          env_allowlist: ["PI_CODING_AGENT_DIR"]
        }
      },
      workspaces: %{
        values.workspace => %{
          policy: "git_worktree",
          repository: values.repository_path,
          root: values.workspace_root,
          ref: values.commit,
          cleanup: "never"
        }
      },
      runtime: %{
        runtime_key: "pi-control-e2e-runtime-#{values.repository_resource_id}",
        name: "Pi Control E2E witness",
        capacity: 1,
        agent_profile: values.profile,
        workspace: values.workspace,
        repository_resource_id: values.repository_resource_id,
        harness_kind: "pi",
        harness_version: @pi_version,
        adapter_version: @witness_adapter_version,
        adapter_protocol_version: 1
      }
    }

    File.write!(path, Jason.encode!(config))
  end

  defp write_loopback_models!(agent_dir, base_url) do
    models = %{
      "providers" => %{
        @pi_provider => %{
          "baseUrl" => base_url,
          "api" => @pi_api,
          "apiKey" => @pi_api_key,
          "models" => [
            %{
              "id" => @pi_model,
              "reasoning" => false,
              "contextWindow" => 32_768,
              "maxTokens" => 4_096
            }
          ]
        }
      }
    }

    File.write!(Path.join(agent_dir, "models.json"), Jason.encode!(models))
  end

  defp isolate_child_environment!(
         root,
         agent_dir,
         home_dir,
         cache_dir,
         data_dir,
         temp_dir,
         appdata_dir,
         localappdata_dir
       ) do
    values = %{
      "PI_CODING_AGENT_DIR" => agent_dir,
      "HOME" => home_dir,
      "TEMP" => temp_dir,
      "TMP" => temp_dir
    }

    values =
      if match?({:win32, _}, :os.type()) do
        Map.merge(values, %{
          "USERPROFILE" => home_dir,
          "APPDATA" => appdata_dir,
          "LOCALAPPDATA" => localappdata_dir
        })
      else
        Map.merge(values, %{
          "XDG_CONFIG_HOME" => agent_dir,
          "XDG_CACHE_HOME" => cache_dir,
          "XDG_DATA_HOME" => data_dir
        })
      end

    Enum.each(values, fn {key, value} -> put_env_with_restore(key, value) end)
    File.write!(Path.join(root, "environment-banner.txt"), "test-only Pi loopback environment\n")
  end

  defp put_env_with_restore(key, value) do
    previous = System.get_env(key)
    System.put_env(key, value)

    on_exit(fn ->
      if previous, do: System.put_env(key, previous), else: System.delete_env(key)
    end)
  end

  defp loopback_gateway_plug(conn, gateway_state) do
    record_gateway_authorization!(gateway_state, conn)

    cond do
      conn.method == "GET" and conn.request_path == "/v1/models" ->
        json_response(conn, 200, %{
          "object" => "list",
          "data" => [%{"id" => @pi_model, "object" => "model", "owned_by" => "symmetry-test"}]
        })

      conn.method == "POST" and conn.request_path == "/v1/responses" ->
        handle_loopback_response(conn, gateway_state)

      true ->
        record_gateway_error(
          gateway_state,
          "unexpected route #{conn.method} #{conn.request_path}"
        )

        json_response(conn, 404, %{"error" => %{"message" => "not found"}})
    end
  end

  defp handle_loopback_response(conn, gateway_state) do
    with {:ok, body, conn} <- read_request_body(conn),
         {:ok, document} <- Jason.decode(body),
         true <- document["model"] == @pi_model,
         true <- document["stream"] == true do
      waiter = self()

      response_number =
        Agent.get_and_update(gateway_state, fn state ->
          number = state.responses + 1
          gated? = number in state.gated_responses

          state = %{
            state
            | responses: number,
              requests: state.requests ++ [document],
              waiters:
                if(gated?, do: Map.put(state.waiters, number, waiter), else: state.waiters),
              final_waiter: if(gated?, do: waiter, else: state.final_waiter)
          }

          {number, state}
        end)

      state = Agent.get(gateway_state, & &1)

      cond do
        Map.has_key?(state.tool_calls, response_number) ->
          case response_number do
            3 ->
              case resume_history_marker(document, state) do
                {:ok, marker} ->
                  Agent.update(gateway_state, &Map.put(&1, :resume_marker, marker))
                  send_sse(conn, tool_call_events(gateway_state, response_number))

                {:error, reason} ->
                  record_gateway_error(gateway_state, reason)
                  json_response(conn, 400, %{"error" => %{"message" => reason}})
              end

            _ ->
              send_sse(conn, tool_call_events(gateway_state, response_number))
          end

        response_number in state.gated_responses ->
          await_gateway_release(conn, gateway_state, response_number)

        response_number > state.expected_responses ->
          record_gateway_error(
            gateway_state,
            "unexpected Responses request count #{response_number}"
          )

          json_response(conn, 409, %{"error" => %{"message" => "unexpected request count"}})

        true ->
          record_gateway_error(
            gateway_state,
            "unexpected Responses request shape at count #{response_number}"
          )

          json_response(conn, 409, %{"error" => %{"message" => "unexpected request shape"}})
      end
    else
      _ ->
        record_gateway_error(gateway_state, "invalid Responses request")
        json_response(conn, 400, %{"error" => %{"message" => "invalid request"}})
    end
  end

  defp await_gateway_release(conn, gateway_state, response_number) do
    receive do
      {:release_response, ^response_number} ->
        send_sse(conn, final_text_events(gateway_state, response_number))
    after
      30_000 ->
        record_gateway_error(
          gateway_state,
          "timed out waiting for the test to release response #{response_number}"
        )

        json_response(conn, 504, %{"error" => %{"message" => "test release timeout"}})
    end
  end

  defp tool_call_events(gateway_state, response_number) do
    state = Agent.get(gateway_state, & &1)
    tool_call = Map.fetch!(state.tool_calls, response_number)

    {call_id, item_id, content} =
      if response_number == 1 do
        marker = state.opaque_marker || "regular"

        {state.first_call_id || "call_pi_control_e2e_#{marker}",
         state.first_item_id || "fc_pi_control_e2e_#{marker}", tool_call.content}
      else
        {"call_pi_control_e2e_resume", "fc_pi_control_e2e_resume", state.resume_marker <> "\n"}
      end

    arguments =
      Jason.encode!(%{"path" => tool_call.path, "content" => content})

    item = %{
      "id" => item_id,
      "type" => "function_call",
      "status" => "completed",
      "call_id" => call_id,
      "name" => "write",
      "arguments" => arguments
    }

    added_item = Map.put(item, "status", "in_progress")

    response = response_document("resp_pi_control_tool", @pi_model, [item], "completed")

    [
      {"response.created",
       %{
         "type" => "response.created",
         "sequence_number" => 1,
         "response" => Map.put(response, "status", "in_progress")
       }},
      {"response.output_item.added",
       %{
         "type" => "response.output_item.added",
         "sequence_number" => 2,
         "output_index" => 0,
         "item" => added_item
       }},
      {"response.function_call_arguments.delta",
       %{
         "type" => "response.function_call_arguments.delta",
         "sequence_number" => 3,
         "item_id" => item["id"],
         "output_index" => 0,
         "delta" => arguments
       }},
      {"response.function_call_arguments.done",
       %{
         "type" => "response.function_call_arguments.done",
         "sequence_number" => 4,
         "item_id" => item["id"],
         "output_index" => 0,
         "arguments" => arguments
       }},
      {"response.output_item.done",
       %{
         "type" => "response.output_item.done",
         "sequence_number" => 5,
         "output_index" => 0,
         "item" => item
       }},
      {"response.completed",
       %{"type" => "response.completed", "sequence_number" => 6, "response" => response}}
    ]
  end

  defp resume_history_marker(document, state) do
    input = document["input"]

    with true <- is_list(input),
         %{"id" => item_id, "call_id" => call_id} = _call <-
           Enum.find(input, fn item ->
             is_map(item) and item["type"] == "function_call" and
               item["call_id"] == state.first_call_id
           end),
         %{"call_id" => ^call_id} <-
           Enum.find(input, fn item ->
             is_map(item) and item["type"] == "function_call_output" and
               item["call_id"] == call_id
           end),
         %{"id" => assistant_id, "role" => "assistant"} <-
           Enum.find(input, fn item ->
             is_map(item) and item["role"] == "assistant" and
               item["id"] == state.first_assistant_id
           end),
         true <- item_id == state.first_item_id,
         true <- assistant_id == state.first_assistant_id,
         true <-
           Enum.all?(input, fn item ->
             not (is_map(item) and item["role"] in ["system", "developer", "user"] and
                    String.contains?(message_text(item), state.opaque_marker))
           end) do
      {:ok, state.opaque_marker}
    else
      false ->
        {:error,
         "resume history omitted the first native identity or leaked it into user/context text"}

      nil ->
        {:error, "resume history omitted the first native identity"}

      _ ->
        {:error, "resume history did not preserve the first native function-call exchange"}
    end
  end

  defp message_text(item) do
    case item["content"] do
      content when is_list(content) ->
        Enum.map_join(content, "", fn part ->
          if is_map(part), do: part["text"] || "", else: ""
        end)

      _ ->
        ""
    end
  end

  defp final_text_events(gateway_state, response_number) do
    state = Agent.get(gateway_state, & &1)

    result = %{
      "schema_version" => "symmetry.task_result.v1",
      "result_id" => Ecto.UUID.generate(),
      "kind" => "progress",
      "summary" => "real Pi created one untracked repository artifact",
      "subject" => state.subject,
      "subject_hash" => state.subject_hash,
      "evidence_refs" => [],
      "blocker" => nil,
      "proposed_next_action" => nil,
      "proposal" => nil,
      "reason" => nil,
      "diagnostics" => []
    }

    text = Jason.encode!(result)
    content = %{"type" => "output_text", "text" => text, "annotations" => []}

    item = %{
      "id" =>
        if(response_number == 2,
          do: state.first_assistant_id || "msg_pi_control_e2e",
          else: "msg_pi_control_e2e_resume"
        ),
      "type" => "message",
      "status" => "completed",
      "role" => "assistant",
      "content" => [content]
    }

    response = response_document("resp_pi_control_final", @pi_model, [item], "completed")
    added_item = Map.put(item, "status", "in_progress")

    [
      {"response.created",
       %{
         "type" => "response.created",
         "sequence_number" => 1,
         "response" => Map.put(response, "status", "in_progress")
       }},
      {"response.output_item.added",
       %{
         "type" => "response.output_item.added",
         "sequence_number" => 2,
         "output_index" => 0,
         "item" => added_item
       }},
      {"response.content_part.added",
       %{
         "type" => "response.content_part.added",
         "sequence_number" => 3,
         "output_index" => 0,
         "content_index" => 0,
         "item_id" => item["id"],
         "part" => content
       }},
      {"response.output_text.delta",
       %{
         "type" => "response.output_text.delta",
         "sequence_number" => 4,
         "output_index" => 0,
         "content_index" => 0,
         "item_id" => item["id"],
         "delta" => text
       }},
      {"response.output_text.done",
       %{
         "type" => "response.output_text.done",
         "sequence_number" => 5,
         "output_index" => 0,
         "content_index" => 0,
         "item_id" => item["id"],
         "text" => text
       }},
      {"response.content_part.done",
       %{
         "type" => "response.content_part.done",
         "sequence_number" => 6,
         "output_index" => 0,
         "content_index" => 0,
         "item_id" => item["id"],
         "part" => content
       }},
      {"response.output_item.done",
       %{
         "type" => "response.output_item.done",
         "sequence_number" => 7,
         "output_index" => 0,
         "item" => item
       }},
      {"response.completed",
       %{"type" => "response.completed", "sequence_number" => 8, "response" => response}}
    ]
  end

  defp response_document(id, model, output, status) do
    %{
      "id" => id,
      "object" => "response",
      "created_at" => System.system_time(:second),
      "status" => status,
      "model" => model,
      "output" => output,
      "usage" => %{"input_tokens" => 0, "output_tokens" => 0, "total_tokens" => 0}
    }
  end

  defp send_sse(conn, events) do
    conn =
      conn
      |> Plug.Conn.put_resp_header("cache-control", "no-cache")
      |> Plug.Conn.put_resp_content_type("text/event-stream")
      |> Plug.Conn.send_chunked(200)

    Enum.reduce(events, conn, fn {event_name, payload}, connection ->
      frame = "event: #{event_name}\ndata: #{Jason.encode!(payload)}\n\n"
      {:ok, connection} = Plug.Conn.chunk(connection, frame)
      connection
    end)
    |> then(fn connection ->
      {:ok, connection} = Plug.Conn.chunk(connection, "data: [DONE]\n\n")
      connection
    end)
  end

  defp json_response(conn, status, payload) do
    conn
    |> Plug.Conn.put_resp_content_type("application/json")
    |> Plug.Conn.send_resp(status, Jason.encode!(payload))
  end

  defp read_request_body(conn, acc \\ <<>>) do
    case Plug.Conn.read_body(conn, length: 8_000_000) do
      {:ok, body, conn} -> {:ok, acc <> body, conn}
      {:more, body, conn} -> read_request_body(conn, acc <> body)
      {:error, reason} -> {:error, reason}
    end
  end

  defp record_gateway_error(gateway_state, message) do
    Agent.update(gateway_state, &Map.update!(&1, :errors, fn errors -> errors ++ [message] end))
  end

  defp record_gateway_authorization!(gateway_state, conn) do
    case Plug.Conn.get_req_header(conn, "authorization") do
      [authorization | _] ->
        Agent.update(
          gateway_state,
          &Map.update!(&1, :authorizations, fn values -> values ++ [authorization] end)
        )

      _ ->
        :ok
    end
  end

  defp release_gateway_final!(gateway_state) do
    response_number = Agent.get(gateway_state, & &1.responses)
    release_gateway_response!(gateway_state, response_number)
  end

  defp release_gateway_response!(gateway_state, response_number) do
    case Agent.get(gateway_state, &Map.get(&1.waiters, response_number)) do
      pid when is_pid(pid) -> send(pid, {:release_response, response_number})
      _ -> flunk("loopback gateway did not expose a waiter for response #{response_number}")
    end
  end

  defp await_gateway_waiter!(daemon_port, gateway_state, response_number, description) do
    await!(daemon_port, description, fn ->
      case Agent.get(gateway_state, &Map.get(&1.waiters, response_number)) do
        pid when is_pid(pid) -> :ok
        _ -> :retry
      end
    end)
  end

  defp await_empty_goal_outbox!(daemon_port, state_dir, run_key) do
    await!(daemon_port, "empty local Goal outbox", fn ->
      case journal_for_run(state_dir, run_key) do
        {:ok, journal} ->
          if Enum.empty?(journal["pending_goal_deliveries"] || []) and
               Enum.empty?(journal["pending_transitions"] || []) and
               Enum.empty?(journal["pending_events"] || []) do
            :ok
          else
            :retry
          end

        :missing ->
          :ok
      end
    end)
  end

  defp await!(daemon_port, description, assertion, timeout \\ 120_000) do
    deadline = System.monotonic_time(:millisecond) + timeout
    await_loop!(daemon_port, description, assertion, deadline, <<>>)
  end

  defp await_loop!(daemon_port, description, assertion, deadline, output) do
    case assertion.() do
      :ok ->
        :ok

      {:ok, value} ->
        value

      :retry ->
        remaining = deadline - System.monotonic_time(:millisecond)

        if remaining <= 0 do
          flunk("timed out waiting for #{description}; daemon output: #{output}")
        end

        receive do
          {^daemon_port, {:data, data}} ->
            await_loop!(daemon_port, description, assertion, deadline, output <> data)

          {^daemon_port, {:exit_status, status}} ->
            flunk(
              "daemon exited with status #{status} while waiting for #{description}: #{output}"
            )
        after
          min(100, remaining) ->
            await_loop!(daemon_port, description, assertion, deadline, output)
        end
    end
  end

  defp await_process_marker!(path, {run_id, generation}, description, timeout \\ 120_000) do
    await!(
      nil,
      description,
      fn ->
        case File.read(path) do
          {:ok, contents} ->
            case Jason.decode(contents) do
              {:ok, %{"run_id" => ^run_id, "generation" => ^generation} = marker} ->
                {:ok, marker}

              _ ->
                :retry
            end

          {:error, _reason} ->
            :retry
        end
      end,
      timeout
    )
  end

  defp journal_for_run(state_dir, {run_id, generation}) do
    digest =
      :crypto.hash(:sha256, run_id <> <<0>> <> Integer.to_string(generation))
      |> Base.encode16(case: :lower)

    path = Path.join([state_dir, "runs", "journal-#{digest}.json"])

    case read_bounded_file(path) do
      {:ok, contents} ->
        case Jason.decode(contents) do
          {:ok, %{"run_id" => ^run_id, "generation" => ^generation} = journal} ->
            {:ok, journal}

          _ ->
            :missing
        end

      {:error, _reason} ->
        :missing
    end
  end

  defp goal_session_journal_for_goal!(state_dir, goal_id) do
    sessions_dir = Path.join(state_dir, "sessions")

    journal =
      sessions_dir
      |> File.ls!()
      |> Enum.filter(&(String.starts_with?(&1, "session-") and String.ends_with?(&1, ".json")))
      |> Enum.find_value(fn filename ->
        case read_bounded_file(Path.join(sessions_dir, filename)) do
          {:ok, contents} ->
            case Jason.decode(contents) do
              {:ok, %{"goal_id" => ^goal_id} = value} -> value
              _ -> nil
            end

          {:error, _reason} ->
            nil
        end
      end)

    journal || flunk("no daemon-local Goal session journal found for Goal #{goal_id}")
  end

  defp read_bounded_file(path) do
    case File.open(path, [:read, :binary, :raw]) do
      {:ok, io_device} ->
        try do
          case :file.read(io_device, @journal_read_limit) do
            {:ok, contents} -> {:ok, contents}
            :eof -> {:ok, <<>>}
            {:error, reason} -> {:error, reason}
          end
        after
          File.close(io_device)
        end

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp git_status!(directory),
    do: git_output!(directory, ["status", "--porcelain=v1", "--untracked-files=all"])

  defp git_tree_digest!(repository_path, commit) do
    entries =
      repository_path
      |> git_output_bytes!(["ls-tree", "--full-tree", "-r", "-z", commit])
      |> :binary.split(<<0>>, [:global])
      |> Enum.reject(&(&1 == <<>>))
      |> Enum.map(fn record ->
        [header, path] = :binary.split(record, <<9>>, [:global])
        [mode, type, object_id] = :binary.split(header, <<32>>, [:global])
        {path, mode, type, object_id}
      end)
      |> Enum.sort_by(&elem(&1, 0))

    manifest =
      entries
      |> Enum.flat_map(fn {path, mode, type, object_id} ->
        blob_digest =
          repository_path |> git_output_bytes!(["cat-file", "blob", object_id]) |> digest!()

        [mode, <<0>>, type, <<0>>, path, <<0>>, blob_digest, <<0>>]
      end)
      |> IO.iodata_to_binary()

    digest!(manifest)
  end

  defp subject_hash!(subject),
    do: "sha256:" <> Base.encode16(SymmetryControl.RequestHash.canonical(subject), case: :lower)

  defp digest!(content),
    do: "sha256:" <> Base.encode16(:crypto.hash(:sha256, content), case: :lower)

  defp normalize_path(path) do
    case :os.type() do
      {:win32, _} -> String.replace(path, "/", "\\")
      _ -> Path.expand(path)
    end
  end

  defp run_git!(directory, args) do
    case System.cmd("git", ["-C", directory | args], stderr_to_stdout: true) do
      {_output, 0} -> :ok
      {output, status} -> raise "git #{Enum.join(args, " ")} failed (#{status}): #{output}"
    end
  end

  defp git_output!(directory, args) do
    case System.cmd("git", ["-C", directory | args], stderr_to_stdout: true) do
      {output, 0} -> output
      {output, status} -> raise "git #{Enum.join(args, " ")} failed (#{status}): #{output}"
    end
  end

  defp git_output_bytes!(directory, args), do: git_output!(directory, args)

  defp build_witness_binary!(daemon_dir, build_dir, name) do
    path = Path.join(build_dir, name)

    case System.cmd(
           "go",
           ["build", "-tags", "symmetry_pi_control_e2e", "-o", path, "./e2e/pi-control-witness"],
           cd: daemon_dir,
           stderr_to_stdout: true
         ) do
      {_output, 0} -> path
      {output, status} -> raise "go build test-only Pi witness failed (#{status}):\n#{output}"
    end
  end

  defp start_daemon!(executable, config_path) do
    start_daemon_with_retry!(
      executable,
      config_path,
      System.monotonic_time(:millisecond) + @daemon_startup_retry_timeout_ms,
      @daemon_startup_retry_initial_backoff_ms
    )
  end

  defp start_daemon_with_retry!(executable, config_path, deadline, backoff_ms) do
    start_daemon_attempt!(executable, config_path)
  rescue
    exception in RuntimeError ->
      message = Exception.message(exception)
      remaining = deadline - System.monotonic_time(:millisecond)

      if remaining > 0 and String.contains?(message, "state directory is already in use") do
        Process.sleep(min(backoff_ms, remaining))

        start_daemon_with_retry!(
          executable,
          config_path,
          deadline,
          min(backoff_ms * 2, @daemon_startup_retry_max_backoff_ms)
        )
      else
        reraise exception, __STACKTRACE__
      end
  end

  defp start_daemon_attempt!(executable, config_path) do
    {spawn_executable, arguments} = daemon_command(executable, config_path)

    port_options = [
      :binary,
      :exit_status,
      :stderr_to_stdout,
      args: arguments,
      env: [
        {~c"SYMMETRY_ENROLLMENT_TOKEN", ~c"test-enrollment-token"},
        {~c"SYMMETRY_PI_CONTROL_E2E", ~c"1"}
      ]
    ]

    port_options =
      if match?({:win32, _}, :os.type()) do
        # OTP's :hide option suppresses a new Windows console and is ignored on Unix.
        [:hide | port_options]
      else
        port_options
      end

    port =
      Port.open(
        {:spawn_executable, String.to_charlist(spawn_executable)},
        port_options
      )

    case Port.info(port, :os_pid) do
      {:os_pid, os_pid} when is_integer(os_pid) and os_pid > 0 ->
        %{
          port: port,
          os_pid: os_pid,
          process_identity: await_startup_process_identity!(port, os_pid, spawn_executable),
          stop_state: :atomics.new(1, signed: false)
        }

      _ ->
        raise "test-only Pi witness daemon did not expose an OS process ID"
    end
  end

  defp daemon_command(executable, config_path) do
    case :os.type() do
      {:win32, _} ->
        {executable, ["-config", config_path]}

      _ ->
        setsid =
          System.find_executable("setsid") || raise "Pi Control E2E requires setsid on Unix"

        {setsid, ["--wait", executable, "-config", config_path]}
    end
  end

  defp stop_daemon!(%{os_pid: os_pid, stop_state: stop_state} = daemon) do
    case :atomics.compare_exchange(stop_state, 1, 0, 1) do
      :ok ->
        try do
          stop_daemon_once!(daemon)
          :atomics.put(stop_state, 1, 2)
          :ok
        rescue
          exception ->
            :atomics.put(stop_state, 1, 0)
            reraise exception, __STACKTRACE__
        catch
          kind, reason ->
            :atomics.put(stop_state, 1, 0)
            :erlang.raise(kind, reason, __STACKTRACE__)
        end

      1 ->
        await_daemon_stopped!(daemon)

      2 ->
        :ok

      state ->
        raise "invalid Pi witness stop state #{inspect(state)} for OS PID #{os_pid}"
    end
  end

  defp stop_daemon_once!(%{port: port, os_pid: os_pid, process_identity: process_identity}) do
    case daemon_state(port, os_pid, process_identity) do
      :stopped ->
        :ok

      :owned ->
        terminate_daemon!(os_pid, :term)
        await_process_exit_or_escalate!(port, os_pid, process_identity)

      {:error, reason} ->
        raise "refusing to terminate Pi witness OS PID #{os_pid}: #{reason}"
    end

    await_port_closed_or_raise!(port, os_pid)
  end

  defp await_daemon_stopped!(%{
         port: port,
         os_pid: os_pid,
         process_identity: process_identity,
         stop_state: stop_state
       }) do
    case daemon_state(port, os_pid, process_identity) do
      :stopped ->
        await_port_closed_or_raise!(port, os_pid)
        :atomics.put(stop_state, 1, 2)
        :ok

      :owned ->
        await_process_exit_or_escalate!(port, os_pid, process_identity)
        await_port_closed_or_raise!(port, os_pid)
        :atomics.put(stop_state, 1, 2)
        :ok

      {:error, reason} ->
        raise "Pi witness stop is still in progress but ownership is no longer safe for OS PID #{os_pid}: #{reason}"
    end
  end

  defp daemon_state(port, os_pid, process_identity) do
    case daemon_port_os_pid(port) do
      :closed ->
        cond do
          not os_process_alive?(os_pid) ->
            :stopped

          process_identity_matches?(os_pid, process_identity) ->
            :owned

          true ->
            {:error, "the Port closed while the recorded process identity was not verified"}
        end

      {:ok, ^os_pid} ->
        cond do
          not os_process_alive?(os_pid) ->
            :stopped

          process_identity_matches?(os_pid, process_identity) ->
            :owned

          true ->
            {:error, "the Port still refers to the recorded PID but its process identity changed"}
        end

      {:ok, actual_pid} ->
        {:error, "the Port is owned by OS PID #{actual_pid}, not recorded PID #{os_pid}"}

      {:error, reason} ->
        {:error, "could not inspect Port ownership: #{reason}"}
    end
  end

  defp daemon_port_os_pid(port) do
    case Port.info(port, :os_pid) do
      {:os_pid, os_pid} when is_integer(os_pid) and os_pid > 0 -> {:ok, os_pid}
      nil -> :closed
      other -> {:error, inspect(other)}
    end
  rescue
    ArgumentError -> :closed
  end

  defp terminate_daemon!(os_pid, signal) do
    result =
      case :os.type() do
        {:win32, _} ->
          System.cmd("taskkill", ["/PID", Integer.to_string(os_pid), "/T", "/F"],
            stderr_to_stdout: true
          )

        _ ->
          System.cmd("kill", [unix_signal(signal), "-#{os_pid}"], stderr_to_stdout: true)
      end

    case result do
      {_output, 0} ->
        :ok

      {output, status} ->
        raise "failed to terminate Pi witness OS PID #{os_pid} with #{signal} (status #{status}): #{String.trim(output)}"
    end
  end

  defp await_process_exit_or_escalate!(port, os_pid, process_identity) do
    first_deadline = System.monotonic_time(:millisecond) + @daemon_stop_timeout_ms

    case await_process_exit(os_pid, first_deadline) do
      :ok ->
        :ok

      {:error, :timeout} ->
        case :os.type() do
          {:win32, _} ->
            raise "Pi witness OS PID #{os_pid} did not terminate within #{@daemon_stop_timeout_ms}ms"

          _ ->
            case daemon_state(port, os_pid, process_identity) do
              :stopped ->
                :ok

              :owned ->
                terminate_daemon!(os_pid, :kill)

                kill_deadline =
                  System.monotonic_time(:millisecond) + @daemon_kill_timeout_ms

                case await_process_exit(os_pid, kill_deadline) do
                  :ok ->
                    :ok

                  {:error, :timeout} ->
                    raise "Pi witness OS PID #{os_pid} did not terminate after bounded kill escalation"
                end

              {:error, reason} ->
                raise "refusing kill escalation for Pi witness OS PID #{os_pid}: #{reason}"
            end
        end
    end
  end

  defp await_process_exit(os_pid, deadline) do
    cond do
      not os_process_alive?(os_pid) ->
        :ok

      System.monotonic_time(:millisecond) >= deadline ->
        {:error, :timeout}

      true ->
        Process.sleep(25)
        await_process_exit(os_pid, deadline)
    end
  end

  defp await_port_closed_or_raise!(port, os_pid) do
    deadline = System.monotonic_time(:millisecond) + @daemon_port_timeout_ms

    case await_port_closed(port, deadline) do
      :ok ->
        :ok

      {:error, :timeout} ->
        case force_close_port(port) do
          :ok ->
            raise "Pi witness Port for OS PID #{os_pid} did not close within #{@daemon_port_timeout_ms}ms after the OS process stopped"

          {:error, :timeout} ->
            raise "Pi witness Port for OS PID #{os_pid} did not close within #{@daemon_port_timeout_ms}ms and remained open after force close"
        end
    end
  end

  defp await_port_closed(port, deadline) do
    cond do
      not port_open?(port) ->
        :ok

      System.monotonic_time(:millisecond) >= deadline ->
        {:error, :timeout}

      true ->
        receive do
          {^port, _message} -> await_port_closed(port, deadline)
        after
          min(100, max(deadline - System.monotonic_time(:millisecond), 0)) ->
            await_port_closed(port, deadline)
        end
    end
  end

  defp port_open?(port) do
    not is_nil(Port.info(port))
  rescue
    ArgumentError -> false
  end

  defp force_close_port(port) do
    if port_open?(port) do
      try do
        Port.close(port)
      rescue
        ArgumentError -> :ok
      end
    end

    await_port_closed(port, System.monotonic_time(:millisecond) + 1_000)
  end

  defp await_startup_process_identity!(port, os_pid, executable) do
    deadline = System.monotonic_time(:millisecond) + @daemon_startup_identity_timeout_ms

    await_startup_process_identity!(
      port,
      os_pid,
      executable,
      deadline,
      @daemon_startup_identity_initial_backoff_ms,
      "identity query has not completed"
    )
  end

  defp await_startup_process_identity!(
         port,
         os_pid,
         executable,
         deadline,
         backoff_ms,
         last_reason
       ) do
    case daemon_port_os_pid(port) do
      {:ok, ^os_pid} ->
        case process_identity(os_pid, executable) do
          {:ok, identity} ->
            identity

          {:error, reason} ->
            retry_startup_process_identity!(
              port,
              os_pid,
              executable,
              deadline,
              backoff_ms,
              inspect(reason)
            )
        end

      {:ok, actual_pid} ->
        startup_identity_failure!(
          port,
          os_pid,
          "Port reported OS PID #{actual_pid} instead of recorded PID #{os_pid}"
        )

      :closed ->
        startup_identity_failure!(
          port,
          os_pid,
          "Port closed before process identity was established; last identity result: #{last_reason}"
        )

      {:error, reason} ->
        startup_identity_failure!(
          port,
          os_pid,
          "could not inspect Port ownership: #{reason}; last identity result: #{last_reason}"
        )
    end
  end

  defp retry_startup_process_identity!(
         port,
         os_pid,
         executable,
         deadline,
         backoff_ms,
         last_reason
       ) do
    remaining = deadline - System.monotonic_time(:millisecond)

    if remaining <= 0 do
      startup_identity_failure!(
        port,
        os_pid,
        "identity query did not become readable before the #{@daemon_startup_identity_timeout_ms}ms startup deadline: #{last_reason}"
      )
    else
      Process.sleep(min(backoff_ms, remaining))

      await_startup_process_identity!(
        port,
        os_pid,
        executable,
        deadline,
        min(backoff_ms * 2, @daemon_startup_identity_max_backoff_ms),
        last_reason
      )
    end
  end

  defp startup_identity_failure!(port, os_pid, reason) do
    initial_output = drain_port_output(port)
    cleanup_failed_startup_process!(port, os_pid)
    output = initial_output <> drain_port_output(port)

    raise "could not establish Pi witness process identity for OS PID #{os_pid}: #{reason}; daemon output: #{String.trim(output)}"
  end

  defp cleanup_failed_startup_process!(port, os_pid) do
    case daemon_port_os_pid(port) do
      {:ok, ^os_pid} ->
        try do
          terminate_daemon!(os_pid, :term)
        rescue
          _ -> :ok
        end

        _ = await_process_exit(os_pid, System.monotonic_time(:millisecond) + 1_000)
        _ = force_close_port(port)

      _ ->
        :ok
    end
  end

  defp drain_port_output(port) do
    drain_port_output(
      port,
      System.monotonic_time(:millisecond) + @daemon_startup_drain_timeout_ms,
      []
    )
  end

  defp drain_port_output(port, deadline, chunks) do
    cond do
      System.monotonic_time(:millisecond) >= deadline ->
        IO.iodata_to_binary(Enum.reverse(chunks))

      true ->
        receive do
          {^port, {:data, data}} ->
            drain_port_output(port, deadline, [data | chunks])

          {^port, {:exit_status, status}} ->
            drain_port_output(port, deadline, ["[exit_status=#{status}]" | chunks])
        after
          min(25, max(deadline - System.monotonic_time(:millisecond), 0)) ->
            if port_open?(port) do
              drain_port_output(port, deadline, chunks)
            else
              IO.iodata_to_binary(Enum.reverse(chunks))
            end
        end
    end
  end

  defp process_identity_matches?(os_pid, expected) do
    case process_identity(os_pid, expected.executable) do
      {:ok, actual} -> actual == expected
      {:error, _reason} -> false
    end
  end

  defp process_identity(os_pid, executable) do
    case :os.type() do
      {:win32, _} -> windows_process_identity(os_pid, executable)
      _ -> unix_process_identity(os_pid)
    end
  rescue
    exception -> {:error, Exception.message(exception)}
  end

  defp windows_process_identity(os_pid, executable) do
    script =
      "$process = Get-CimInstance Win32_Process -Filter 'ProcessId = #{os_pid}'; " <>
        "if ($null -eq $process) { exit 2 }; " <>
        "$process | Select-Object CreationDate,ExecutablePath | ConvertTo-Json -Compress"

    # This direct System.cmd invocation uses the test runner's inherited console
    # rather than cmd.exe; -NoProfile/-NonInteractive prevents shell startup or
    # prompts, so it does not create a new visible console window. The daemon
    # itself is launched through an OTP Port with :hide above.
    case System.cmd(
           "powershell",
           ["-NoProfile", "-NonInteractive", "-Command", script],
           stderr_to_stdout: true
         ) do
      {output, 0} ->
        case Jason.decode(String.trim(output)) do
          {:ok, %{"CreationDate" => creation, "ExecutablePath" => actual_path}}
          when is_binary(creation) and is_binary(actual_path) ->
            expected_path = normalize_process_path(executable)
            actual_path = normalize_process_path(actual_path)

            if actual_path == expected_path do
              {:ok, %{value: "windows:#{creation}", executable: expected_path}}
            else
              {:error, "process executable changed to #{actual_path}"}
            end

          {:ok, value} ->
            {:error, "unexpected Windows process identity: #{inspect(value)}"}

          {:error, reason} ->
            {:error, "decode Windows process identity: #{inspect(reason)}"}
        end

      {output, 2} ->
        {:error,
         {:not_ready,
          "PowerShell process identity query has not observed OS PID #{os_pid}: #{String.trim(output)}"}}

      {output, status} ->
        {:error,
         "PowerShell process identity query failed (status #{status}): #{String.trim(output)}"}
    end
  end

  defp unix_process_identity(os_pid) do
    with {:ok, boot_id} <- File.read("/proc/sys/kernel/random/boot_id"),
         {:ok, stat} <- File.read("/proc/#{os_pid}/stat"),
         {:ok, start_time} <- unix_process_start_time(stat) do
      {:ok,
       %{
         value: "unix:#{String.trim(boot_id)}:#{os_pid}:#{start_time}",
         executable: nil
       }}
    else
      {:error, reason} -> {:error, inspect(reason)}
    end
  end

  defp unix_process_start_time(stat) do
    case Regex.run(~r/^\d+ \(.*\) (.+)$/s, stat, capture: :all_but_first) do
      [fields] ->
        case fields |> String.split() |> Enum.at(19) do
          start_time when is_binary(start_time) ->
            case Integer.parse(start_time) do
              {_, ""} -> {:ok, start_time}
              _ -> {:error, "invalid Unix process start time"}
            end

          _ ->
            {:error, "missing Unix process start time"}
        end

      _ ->
        {:error, "malformed Unix process stat"}
    end
  end

  defp normalize_process_path(path) do
    path = Path.expand(path)

    case :os.type() do
      {:win32, _} -> path |> String.replace("/", "\\") |> String.downcase()
      _ -> path
    end
  end

  defp os_process_alive?(os_pid) do
    case :os.type() do
      {:win32, _} ->
        case System.cmd(
               "tasklist",
               ["/FI", "PID eq #{os_pid}", "/FO", "CSV", "/NH"],
               stderr_to_stdout: true
             ) do
          {output, 0} ->
            output
            |> String.split(["\r\n", "\n"], trim: true)
            |> Enum.any?(&Regex.match?(~r/^"[^"]*","#{os_pid}",/, &1))

          {output, status} ->
            raise "could not inspect Windows OS PID #{os_pid} (tasklist status #{status}): #{String.trim(output)}"
        end

      _ ->
        if File.dir?("/proc"),
          do: File.dir?("/proc/#{os_pid}"),
          else: unix_process_alive?(os_pid)
    end
  end

  defp unix_process_alive?(os_pid) do
    case System.cmd("kill", ["-0", Integer.to_string(os_pid)], stderr_to_stdout: true) do
      {_output, 0} ->
        true

      {_output, 1} ->
        false

      {output, status} ->
        raise "could not inspect Unix OS PID #{os_pid} (kill -0 status #{status}): #{String.trim(output)}"
    end
  end

  defp unix_signal(:term), do: "-TERM"
  defp unix_signal(:kill), do: "-KILL"

  defp required_pi_executable! do
    value = String.trim(System.get_env("SYMMETRY_PI_CONTROL_E2E_EXECUTABLE") || "")

    if value == "",
      do: flunk("SYMMETRY_PI_CONTROL_E2E_EXECUTABLE must name the real Pi 0.85.1 executable")

    unless Path.type(value) == :absolute, do: flunk("Pi executable path must be absolute")
    if not File.regular?(value), do: flunk("Pi executable path must name a regular file")
    assert_pi_executable_sha256!(value)
    value
  end

  defp assert_pi_executable_sha256!(path) do
    digest =
      case File.open(path, [:read, :binary, :raw]) do
        {:ok, io_device} ->
          try do
            io_device
            |> hash_file_chunks!(:crypto.hash_init(:sha256))
            |> :crypto.hash_final()
            |> Base.encode16(case: :upper)
          after
            File.close(io_device)
          end

        {:error, reason} ->
          flunk("could not open Pi executable for SHA-256 verification: #{inspect(reason)}")
      end

    if digest != @pi_executable_sha256 do
      flunk("Pi executable SHA-256 did not equal #{@pi_executable_sha256}; got #{digest}")
    end
  end

  defp hash_file_chunks!(io_device, context) do
    case IO.binread(io_device, 64 * 1024) do
      :eof ->
        context

      {:error, reason} ->
        flunk("could not read Pi executable for SHA-256 verification: #{inspect(reason)}")

      chunk when is_binary(chunk) ->
        hash_file_chunks!(io_device, :crypto.hash_update(context, chunk))
    end
  end

  defp assert_pi_version!(executable) do
    case System.cmd(executable, ["--version"], stderr_to_stdout: true) do
      {output, 0} ->
        if String.trim(output) != @pi_version,
          do: flunk("Pi version did not equal #{@pi_version}")

      {output, status} ->
        flunk("Pi --version failed (#{status}): #{output}")
    end
  end

  defp assert_supported_platform! do
    unless match?({:win32, _}, :os.type()),
      do: flunk("Pi Control E2E requires the verified Windows Pi 0.85.1 binary")
  end
end

defmodule SymmetryControl.DataCaseSandboxOptionsTest do
  use ExUnit.Case, async: true

  test "preserves defaults and accepts a bounded module ownership timeout" do
    assert SymmetryControl.DataCase.sandbox_options(%{async: false}) == [shared: true]
    assert SymmetryControl.DataCase.sandbox_options(%{async: true}) == [shared: false]

    assert SymmetryControl.DataCase.sandbox_options(%{
             async: false,
             sandbox_ownership_timeout: 240_000
           })
           |> Enum.sort() == [ownership_timeout: 240_000, shared: true]
  end

  test "rejects unbounded and invalid module ownership timeouts" do
    for timeout <- [:infinity, 0, -1, 600_001, "240000"] do
      assert_raise ArgumentError, ~r/sandbox_ownership_timeout/, fn ->
        SymmetryControl.DataCase.sandbox_options(%{
          async: false,
          sandbox_ownership_timeout: timeout
        })
      end
    end
  end
end
