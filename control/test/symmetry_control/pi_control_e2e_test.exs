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
    GoalEvent,
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
  # Keep this skip literal. ExUnit prepares module filters before test setup,
  # while a dynamic environment-based skip can be frozen in cached BEAM
  # metadata. Running this module requires both the environment opt-in and an
  # explicit `--include skip:true` filter.
  @moduletag skip: true

  @pi_version "0.85.1"
  @pi_provider "symmetry-control-loopback"
  @pi_model "gpt-5.6-terra"
  @pi_api "openai-responses"
  @pi_api_key "symmetry-control-loopback-test-key"
  @pi_executable_sha256_by_platform %{
    {{:win32, :nt}, "x86_64-pc-windows"} =>
      "2d4d351da30bfe23a473032e66a571b238763565aa93754e74f4a939de13f195",
    {{:unix, :linux}, "x86_64-pc-linux-gnu"} =>
      "443bd83f30e4dbc7bac2eed9c6aa2461b9a15016fd555f48c92a0591d028c403"
  }
  @witness_adapter_version "symmetry-test:pi-control-loopback-witness-v1"
  @artifact_path "pi-control-e2e-artifact.txt"
  @artifact_content "real Pi Control E2E artifact\n"
  @resume_artifact_path "pi-control-e2e-resumed-artifact.txt"
  @journal_read_limit 4_194_304
  @daemon_stop_timeout_ms 10_000
  @daemon_kill_timeout_ms 5_000
  @daemon_port_timeout_ms 5_000
  @daemon_identity_reprobe_interval_ms 25
  @daemon_root_cleanup_timeout_ms 5_000
  @daemon_startup_identity_timeout_ms 5_000
  @daemon_startup_identity_initial_backoff_ms 10
  @daemon_startup_identity_max_backoff_ms 100
  @daemon_startup_drain_timeout_ms 250
  @daemon_startup_retry_timeout_ms 15_000
  @daemon_startup_retry_initial_backoff_ms 25
  @daemon_startup_retry_max_backoff_ms 100
  @crash_recovery_witness_timeout_ms 30_000

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
    unless System.get_env("SYMMETRY_PI_CONTROL_E2E") == "1" do
      flunk(
        "SYMMETRY_PI_CONTROL_E2E=1 is required when explicitly including the Pi Control E2E module"
      )
    end

    platform = assert_supported_platform!()
    executable = required_pi_executable!(platform)
    assert_pi_version!(executable)
    acceptance_witness? = Map.get(context, :pi_control_acceptance, false)

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
    root_cleanup_state = :atomics.new(1, signed: false)

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

    # Keep an explicit pre-start marker so a missing file is never treated as
    # proof that no native child was started.
    File.write!(process_marker_path, "not_started\n")

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
             expected_responses: if(acceptance_witness?, do: 3, else: 2),
             gated_responses: if(acceptance_witness?, do: [3], else: [2]),
             tool_calls: gateway_tool_calls(acceptance_witness?),
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
             artifact_content: @artifact_content,
             acceptance_witness: acceptance_witness?
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
    fixture = create_goal_fixture!(repository_path, profile, workspace, acceptance_witness?)

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
      repository_resource_id: fixture.repository_resource_id,
      tools: daemon_tools(acceptance_witness?)
    })

    File.write!(process_marker_path, "starting\n")

    daemon_process =
      start_daemon!(context.daemon, config_path, process_marker_path, root, root_cleanup_state)

    on_exit(fn ->
      stop_daemon!(daemon_process)
      cleanup_root_if_proven!(root, root_cleanup_state)
    end)

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
     root: root,
     state_dir: state_dir,
     process_marker_path: process_marker_path,
     root_cleanup_state: root_cleanup_state,
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

  @tag :pi_control_crash_recovery
  test "hard daemon crash recovers an in-flight Pi run without replaying effects", context do
    %{fixture: fixture, gateway_state: gateway_state} = context
    runtime = Repo.get_by!(Runtime, runtime_key: fixture.runtime_key)

    assigned = assign_goal_task!(context.daemon_port, fixture.task.id)
    assert assigned.task_id == fixture.task.id
    assert assigned.runtime_id == runtime.id
    run_key = {assigned.id, assigned.generation}

    # The first tool call has completed and the final native response is now
    # blocked in the loopback gateway. Killing the daemon at this boundary leaves
    # a real in-flight child plus a durable journal, without claiming a result.
    await_gateway_waiter!(
      context.daemon_port,
      gateway_state,
      2,
      "in-flight Pi final response waiter"
    )

    process_marker =
      await_process_marker!(
        context.process_marker_path,
        run_key,
        "in-flight native Pi process identity"
      )

    workspace_path =
      Path.join([
        context.workspace_root,
        "binding-pi-control-e2e",
        "run-#{assigned.id}",
        "generation-#{assigned.generation}"
      ])

    assert process_marker["pid"] > 0
    assert is_binary(process_marker["process_identity"])
    assert File.read!(Path.join(workspace_path, @artifact_path)) == @artifact_content

    first_run = Repo.get_by!(Run, id: assigned.id)
    first_fence = run_fence(first_run)
    first_runtime_epoch = runtime.connection_epoch

    assert first_run.state in [
             "assigned",
             "claimed",
             "running",
             "waiting_for_input",
             "paused",
             "cancelling"
           ]

    assert is_nil(first_run.result)
    assert is_nil(first_run.failure)

    refute Repo.exists?(
             from transition in RunTransition,
               where:
                 transition.run_id == ^first_run.id and
                   transition.state in ["completed", "failed", "cancelled"]
           )

    assert Repo.aggregate(
             from(receipt in HarnessSessionStopReceipt,
               where: receipt.run_id == ^first_run.id
             ),
             :count
           ) == 0

    assert Repo.aggregate(
             from(usage in SymmetryControl.Goals.RunUsage,
               where: usage.run_id == ^first_run.id
             ),
             :count
           ) == 0

    refute Repo.exists?(
             from event in GoalEvent,
               where: event.goal_id == ^fixture.goal.id and event.kind == "task_settled"
           )

    assert_native_process_owned!(%{
      pid: process_marker["pid"],
      identity: process_marker["process_identity"]
    })

    gateway_before_crash = Agent.get(gateway_state, & &1)
    assert gateway_before_crash.responses == 2
    assert length(gateway_before_crash.requests) == 2
    assert gateway_before_crash.errors == []

    {:ok, first_journal} = journal_for_run(context.state_dir, run_key)
    assert first_journal["pid"] == process_marker["pid"]
    assert first_journal["process_identity"] == process_marker["process_identity"]
    assert normalize_path(first_journal["workspace_path"]) == normalize_path(workspace_path)

    crash_daemon!(context.daemon_process)
    # Release the now-dead HTTP waiter so the synthetic gateway does not retain a
    # blocked request until its own timeout. No new request is expected after this.
    release_gateway_response!(gateway_state, 2)

    second_daemon =
      start_daemon!(
        context.daemon,
        context.config_path,
        context.process_marker_path,
        context.root,
        context.root_cleanup_state
      )

    on_exit(fn -> stop_daemon!(second_daemon) end)

    restarted_runtime =
      await!(second_daemon.port, "same runtime after hard daemon crash", fn ->
        case Repo.get_by(Runtime, runtime_key: fixture.runtime_key) do
          %Runtime{id: id, status: "online", connection_epoch: epoch} = current
          when id == runtime.id and epoch > first_runtime_epoch ->
            {:ok, current}

          _ ->
            :retry
        end
      end)

    recovery_deadline =
      System.monotonic_time(:millisecond) + @crash_recovery_witness_timeout_ms

    recovery_outcome =
      await!(
        second_daemon.port,
        "hard-crash durable unknown-outcome terminal witness",
        fn ->
          case hard_crash_recovery_observation(
                 second_daemon,
                 context.state_dir,
                 run_key,
                 fixture.task.id,
                 fixture.goal.id,
                 runtime.id,
                 workspace_path
               ) do
            {:terminal, task} ->
              {:ok, {:terminal, task}}

            {:wait, diagnostic} ->
              if System.monotonic_time(:millisecond) >= recovery_deadline do
                flunk("hard-crash recovery witness did not settle: #{inspect(diagnostic)}")
              else
                :retry
              end

            {:error, reason} ->
              flunk("hard-crash recovery produced an invalid outcome: #{reason}")

            :retry ->
              :retry
          end
        end,
        @crash_recovery_witness_timeout_ms + 100
      )

    {:terminal, recovered_task} = recovery_outcome
    recovered_run = Repo.get_by!(Run, id: assigned.id)
    assert recovered_run.id == assigned.id
    assert File.dir?(workspace_path)
    assert File.read!(Path.join(workspace_path, @artifact_path)) == @artifact_content

    assert recovered_task.state == "failed"
    assert recovered_task.failure["reason"] == "unknown_outcome"
    assert recovered_run.state == "failed"
    assert recovered_run.failure["reason"] == "unknown_outcome"
    assert is_nil(recovered_run.result)

    transitions =
      Repo.all(
        from transition in RunTransition,
          where: transition.run_id == ^recovered_run.id and transition.state == "failed"
      )

    assert length(transitions) == 1
    assert hd(transitions).payload["reason"] == "unknown_outcome"

    recovered_journal =
      case journal_for_run(context.state_dir, run_key) do
        {:ok, journal} ->
          if retained_terminal_crash_journal?(journal, workspace_path),
            do: {:ok, journal},
            else: :missing

        :missing ->
          :missing
      end

    usage_query =
      from usage in SymmetryControl.Goals.RunUsage, where: usage.run_id == ^recovered_run.id

    assert Repo.aggregate(usage_query, :count) == 1
    usage = Repo.one!(usage_query)
    assert usage.provider == "unknown"
    assert usage.model == "unknown"
    assert usage.cost_basis == "unknown"

    session = Repo.get!(HarnessSession, recovered_run.harness_session_id)
    assert session.binding_verified
    assert session.binding_id == recovered_run.harness_binding_id

    session_journal = goal_session_journal_for_goal!(context.state_dir, fixture.goal.id)

    case recovered_journal do
      {:ok, journal} ->
        assert journal["pid"] == process_marker["pid"]
        assert journal["process_identity"] == process_marker["process_identity"]
        assert normalize_path(journal["workspace_path"]) == normalize_path(workspace_path)
        assert journal["local_state"] in ["terminal_pending", "cleanup_pending", "stale"]

        assert journal["workspace_recovery_required"] == true or
                 journal["retain_workspace"] == true

        case native_process_stop_status(journal["pid"], journal["process_identity"]) do
          :stopped ->
            {attach_receipt, stop_receipt} =
              await!(second_daemon.port, "single recovered session stop receipt", fn ->
                attach =
                  Repo.get_by(HarnessSessionAttachReceipt,
                    run_id: recovered_run.id,
                    session_id: recovered_run.harness_session_id,
                    generation: recovered_run.generation
                  )

                stop =
                  Repo.get_by(HarnessSessionStopReceipt,
                    run_id: recovered_run.id,
                    session_id: recovered_run.harness_session_id,
                    binding_id: recovered_run.harness_binding_id
                  )

                if attach && stop, do: {:ok, {attach, stop}}, else: :retry
              end)

            session = Repo.get!(HarnessSession, recovered_run.harness_session_id)
            session_journal = goal_session_journal_for_goal!(context.state_dir, fixture.goal.id)

            assert_crash_recovery_stop_receipt!(
              recovered_run,
              session,
              session_journal,
              attach_receipt,
              stop_receipt
            )

          :unproven ->
            assert Repo.aggregate(
                     from(receipt in HarnessSessionStopReceipt,
                       where: receipt.run_id == ^recovered_run.id
                     ),
                     :count
                   ) == 0

            assert session.state == "unavailable"
            assert session_journal["launch_state"] == "uncertain"
            assert session_journal["session_state"] == "unavailable"
            assert session_journal["recovery_required"] == true
            assert session_journal["uncertain_reason"] == "native Goal session stop is unproven"
            assert is_nil(session_journal["stop_certificate"])

            assert {:ok, marker_contents} = File.read(context.process_marker_path)
            assert {:ok, ^process_marker} = Jason.decode(marker_contents)

            marker_snapshot =
              preserve_native_process_marker!(context.process_marker_path, context.root)

            cleanup_daemon = Map.put(second_daemon, :process_marker_path, marker_snapshot)
            stop_daemon!(cleanup_daemon)
            restore_native_process_marker!(marker_snapshot, context.process_marker_path)

          :live ->
            flunk("hard-crash recovery terminalized while the native Pi process remained live")

          {:unsafe, reason} ->
            flunk("hard-crash recovery terminalized with an unsafe native Pi process: #{reason}")
        end

      :missing ->
        attach_receipt =
          Repo.get_by!(HarnessSessionAttachReceipt,
            run_id: recovered_run.id,
            session_id: recovered_run.harness_session_id,
            generation: recovered_run.generation
          )

        stop_receipt =
          Repo.get_by!(HarnessSessionStopReceipt,
            run_id: recovered_run.id,
            session_id: recovered_run.harness_session_id,
            binding_id: recovered_run.harness_binding_id
          )

        assert_crash_recovery_stop_receipt!(
          recovered_run,
          session,
          session_journal,
          attach_receipt,
          stop_receipt
        )
    end

    await_empty_goal_outbox!(second_daemon.port, context.state_dir, run_key)

    # The Run and Task terminal records above preserve `unknown_outcome`; the
    # settlement API maps recovery metadata in the failure payload to its
    # generic `process_failure` reason.
    assert {:ok, %{"settlement" => "failed", "reason" => "process_failure"}} =
             Goals.settle_task(recovered_task.id, recovered_run.id, recovered_run.generation)

    assert Repo.aggregate(
             from(event in GoalEvent,
               where: event.goal_id == ^fixture.goal.id and event.kind == "task_settled"
             ),
             :count
           ) == 1

    stale_fence = Map.put(first_fence, :runtime_epoch, restarted_runtime.connection_epoch)

    assert {:error, :ownership_lost} =
             Goals.mark_harness_session_stopped(
               runtime.machine_id,
               recovered_run.id,
               stale_fence,
               %{
                 session_id: recovered_run.harness_session_id,
                 local_handle_id: session.local_handle_id,
                 binding_id: recovered_run.harness_binding_id
               }
             )

    gateway = Agent.get(gateway_state, & &1)
    assert gateway.responses == gateway_before_crash.responses
    assert gateway.requests == gateway_before_crash.requests
    assert gateway.errors == []
  end

  @tag :pi_control_acceptance
  test "real Pi committed candidate is independently validated and achieves the Goal", context do
    %{fixture: fixture, gateway_state: gateway_state} = context
    runtime = Repo.get_by!(Runtime, runtime_key: fixture.runtime_key)

    assigned = assign_goal_task!(context.daemon_port, fixture.task.id)
    assert assigned.task_id == fixture.task.id
    assert assigned.runtime_id == runtime.id

    run_key = {assigned.id, assigned.generation}

    process_marker =
      await_process_marker!(
        context.process_marker_path,
        run_key,
        "native Pi acceptance process identity"
      )

    workspace_path =
      Path.join([
        context.workspace_root,
        "binding-pi-control-e2e",
        "run-#{assigned.id}",
        "generation-#{assigned.generation}"
      ])

    assert process_marker["pid"] > 0
    assert File.dir?(workspace_path)

    await_gateway_waiter!(
      context.daemon_port,
      gateway_state,
      3,
      "native Pi candidate final response waiter"
    )

    candidate_subject =
      await_candidate_subject!(
        context.daemon_port,
        workspace_path,
        fixture.repository_resource_id,
        fixture.subject
      )

    assert candidate_subject["commit"] != fixture.subject["commit"]
    assert File.read!(Path.join(workspace_path, @artifact_path)) == @artifact_content

    Agent.update(gateway_state, fn state ->
      %{state | subject: candidate_subject, subject_hash: subject_hash!(candidate_subject)}
    end)

    release_gateway_response!(gateway_state, 3)

    completed_task =
      await!(context.daemon_port, "completed native candidate Task", fn ->
        case Repo.get(Task, fixture.task.id) do
          %Task{state: "completed"} = task ->
            {:ok, task}

          %Task{state: "failed", failure: failure} ->
            flunk("Pi acceptance producer failed: #{inspect(failure)}")

          _ ->
            :retry
        end
      end)

    completed_run = Repo.get_by!(Run, id: assigned.id)
    assert completed_run.state == "completed"
    assert completed_run.result["task_result"]["kind"] == "candidate_completion"
    assert completed_run.result["task_result"]["subject"] == candidate_subject
    assert completed_run.result["task_result"]["subject_hash"] == subject_hash!(candidate_subject)

    await!(context.daemon_port, "native candidate session stop receipt", fn ->
      if Repo.get_by(HarnessSessionStopReceipt, run_id: completed_run.id), do: :ok, else: :retry
    end)

    await_empty_goal_outbox!(context.daemon_port, context.state_dir, run_key)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(completed_task.id, completed_run.id, completed_run.generation)

    assert Repo.get!(Goal, fixture.goal.id).state == "active"

    refute Repo.exists?(
             from outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.work_item_id == ^fixture.item.id
           )

    wrong_subject = Map.put(fixture.subject, "commit", String.duplicate("0", 40))

    assert {:error, {:invalid_contract, _}} =
             command_current(fixture.goal.id, "admit_task", %{
               work_item_id: fixture.item.id,
               purpose: "validate",
               model_profile: context.profile,
               session_mode: "fresh",
               requested_session_id: nil,
               validation_of_task_id: completed_task.id,
               subject: wrong_subject
             })

    assert {:ok, validation_admission, :created} =
             command_current(fixture.goal.id, "admit_task", %{
               work_item_id: fixture.item.id,
               purpose: "validate",
               model_profile: context.profile,
               session_mode: "fresh",
               requested_session_id: nil,
               validation_of_task_id: completed_task.id
             })

    validation_task = Repo.get!(Task, validation_admission.response["task"]["id"])
    assert validation_task.input["subject"] == candidate_subject

    validation_run = assign_goal_task!(context.daemon_port, validation_task.id)
    validation_run_id = validation_run.id

    validation_task =
      await!(context.daemon_port, "completed deterministic validation Task", fn ->
        case Repo.get(Task, validation_task.id) do
          %Task{state: "completed"} = task ->
            {:ok, task}

          %Task{state: "failed", failure: failure} ->
            flunk("deterministic validation failed: #{inspect(failure)}")

          _ ->
            :retry
        end
      end)

    validation_run = Repo.get!(Run, validation_run_id)
    assert validation_run.state == "completed"
    assert validation_run.result["task_result"]["kind"] == "candidate_completion"
    assert validation_run.result["task_result"]["subject"] == candidate_subject

    validation_evidence =
      await!(context.daemon_port, "persisted deterministic artifact evidence", fn ->
        case Repo.one(
               from evidence in SymmetryControl.Goals.RunEvidence,
                 where: evidence.run_id == ^validation_run.id
             ) do
          %SymmetryControl.Goals.RunEvidence{} = row -> {:ok, row}
          _ -> :retry
        end
      end)

    validation_runtime = Repo.get!(Runtime, validation_run.runtime_id)

    assert {:error, :invalid_evidence_identity} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               run_fence(validation_run),
               wrong_artifact_evidence(
                 validation_run.id,
                 fixture.repository_resource_id,
                 fixture.subject
               )
             )

    assert {:ok, %{"settlement" => "accepted", "reason" => "validation_passed"}} =
             Goals.settle_task(validation_task.id, validation_run.id, validation_run.generation)

    await_empty_goal_outbox!(
      context.daemon_port,
      context.state_dir,
      {validation_run.id, validation_run.generation}
    )

    subject_hash = subject_hash!(candidate_subject)

    assert {:ok, completion_request, :created} =
             command_current(fixture.goal.id, "request_decision", %{
               kind: "completion",
               work_item_id: nil,
               subject_hash: subject_hash,
               proposal: nil
             })

    decision_id = completion_request.response["decision"]["id"]
    decision_version = Repo.get!(GoalDecision, decision_id).lock_version

    assert {:ok, _resolved, :created} =
             command_current(fixture.goal.id, "resolve_decision", %{
               decision_id: decision_id,
               expected_decision_version: decision_version,
               option_id: "accept"
             })

    assert {:ok, achieved, :created} =
             command_current(fixture.goal.id, "achieve", %{
               subject: candidate_subject,
               integration_work_item_id: fixture.item.id,
               evidence_ids: [validation_evidence.id],
               decision_id: decision_id
             })

    assert achieved.goal.state == "achieved"

    assert Repo.exists?(
             from outcome in SymmetryControl.Goals.WorkOutcome,
               where:
                 outcome.goal_id == ^fixture.goal.id and
                   outcome.work_item_id == ^fixture.item.id and
                   outcome.validation_task_id == ^validation_task.id and
                   outcome.disposition == "accepted" and
                   outcome.subject_hash ==
                     ^SymmetryControl.RequestHash.canonical(candidate_subject)
           )

    gateway = Agent.get(gateway_state, & &1)
    assert gateway.errors == []
    assert gateway.responses == 3
    assert Enum.count(gateway.requests) == 3
  end

  @tag :pi_control_handoff
  @tag :pi_control_acceptance
  test "real Pi fresh handoff preserves Control lineage and starts one new native session",
       context do
    %{fixture: fixture, gateway_state: gateway_state} = context
    runtime = Repo.get_by!(Runtime, runtime_key: fixture.runtime_key)
    handoff_artifact_path = "pi-control-e2e-handoff-artifact.txt"
    handoff_artifact_content = "real Pi Control E2E handoff artifact\n"

    handoff_commit_command =
      if match?({:win32, _}, :os.type()) do
        "$ErrorActionPreference='Stop'; git add -- #{handoff_artifact_path}; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; git -c user.name='Symmetry Pi Control E2E' -c user.email='symmetry-pi-control-e2e@example.invalid' commit --quiet -m 'Pi Control E2E handoff candidate'; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; git rev-parse HEAD"
      else
        "set -eu; git add -- #{handoff_artifact_path}; git -c user.name='Symmetry Pi Control E2E' -c user.email='symmetry-pi-control-e2e@example.invalid' commit --quiet -m 'Pi Control E2E handoff candidate'; git rev-parse HEAD"
      end

    # One real source turn (write + commit + final) is followed by one fresh
    # handoff turn (write + commit + final). The target tool calls use explicit
    # arguments so the gateway never interprets the fresh handoff as a resume.
    Agent.update(gateway_state, fn state ->
      %{
        state
        | expected_responses: 6,
          gated_responses: [3, 6],
          tool_calls: %{
            1 => %{
              name: "write",
              call_id: "call_pi_handoff_source_write",
              item_id: "fc_pi_handoff_source_write",
              arguments: %{path: @artifact_path, content: @artifact_content}
            },
            2 => %{
              name: candidate_commit_tool(),
              call_id: "call_pi_handoff_source_commit",
              item_id: "fc_pi_handoff_source_commit",
              arguments: %{command: candidate_commit_command(), timeout: 30}
            },
            4 => %{
              name: "write",
              call_id: "call_pi_handoff_target_write",
              item_id: "fc_pi_handoff_target_write",
              arguments: %{path: handoff_artifact_path, content: handoff_artifact_content}
            },
            5 => %{
              name: candidate_commit_tool(),
              call_id: "call_pi_handoff_target_commit",
              item_id: "fc_pi_handoff_target_commit",
              arguments: %{command: handoff_commit_command, timeout: 30}
            }
          }
      }
    end)

    source_assigned = assign_goal_task!(context.daemon_port, fixture.task.id)
    assert source_assigned.task_id == fixture.task.id
    assert source_assigned.runtime_id == runtime.id
    source_run_key = {source_assigned.id, source_assigned.generation}

    await_gateway_waiter!(
      context.daemon_port,
      gateway_state,
      3,
      "native Pi handoff source final response waiter"
    )

    source_process_marker =
      await_process_marker!(
        context.process_marker_path,
        source_run_key,
        "native Pi handoff source process identity"
      )

    source_workspace_path =
      Path.join([
        context.workspace_root,
        "binding-pi-control-e2e",
        "run-#{source_assigned.id}",
        "generation-#{source_assigned.generation}"
      ])

    assert source_process_marker["pid"] > 0
    assert File.dir?(source_workspace_path)

    source_subject =
      await_candidate_subject!(
        context.daemon_port,
        source_workspace_path,
        fixture.repository_resource_id,
        fixture.subject
      )

    assert source_subject["commit"] != fixture.subject["commit"]
    assert File.read!(Path.join(source_workspace_path, @artifact_path)) == @artifact_content

    Agent.update(gateway_state, fn state ->
      %{state | subject: source_subject, subject_hash: subject_hash!(source_subject)}
    end)

    release_gateway_response!(gateway_state, 3)

    source_task =
      await!(context.daemon_port, "completed native Pi handoff source Task", fn ->
        case Repo.get(Task, fixture.task.id) do
          %Task{state: "completed"} = task ->
            {:ok, task}

          %Task{state: "failed", failure: failure} ->
            flunk("Pi handoff source task failed: #{inspect(failure)}")

          _ ->
            :retry
        end
      end)

    source_run = Repo.get_by!(Run, id: source_assigned.id)
    assert source_run.state == "completed"
    assert source_run.result["task_result"]["kind"] == "candidate_completion"
    assert source_run.result["task_result"]["subject"] == source_subject
    assert source_run.result["task_result"]["subject_hash"] == subject_hash!(source_subject)
    assert source_run.result["task_result"]["evidence_refs"] == []
    assert source_run.provider_access_snapshot == %{"v" => 1, "kind" => "none"}

    {source_attach_receipt, source_stop_receipt} =
      await!(context.daemon_port, "native Pi handoff source attach and stop receipts", fn ->
        attach = Repo.get_by(HarnessSessionAttachReceipt, run_id: source_run.id)
        stop = Repo.get_by(HarnessSessionStopReceipt, run_id: source_run.id)
        if attach && stop, do: {:ok, {attach, stop}}, else: :retry
      end)

    assert source_attach_receipt.runtime_id == source_run.runtime_id
    assert source_attach_receipt.runtime_epoch == source_run.claimed_runtime_epoch
    assert source_attach_receipt.generation == source_run.generation
    assert source_attach_receipt.claim_id == source_run.claim_id
    assert source_attach_receipt.lease_token == source_run.lease_token
    assert source_stop_receipt.run_id == source_run.id
    assert source_stop_receipt.session_id == source_run.harness_session_id
    assert source_stop_receipt.binding_id == source_run.harness_binding_id

    await_empty_goal_outbox!(context.daemon_port, context.state_dir, source_run_key)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(source_task.id, source_run.id, source_run.generation)

    source_context =
      Repo.get!(SymmetryControl.Goals.ContextSnapshot, source_task.context_snapshot_id)

    assert {:ok, handoff_admission, :created} =
             command_current(fixture.goal.id, "admit_task", %{
               work_item_id: fixture.item.id,
               purpose: "implement",
               model_profile: context.profile,
               session_mode: "handoff",
               requested_session_id: nil,
               handoff_source_run_id: source_run.id,
               validation_of_task_id: nil
             })

    target_task = Repo.get!(Task, handoff_admission.response["task"]["id"])
    assert target_task.id != source_task.id
    assert target_task.handoff_source_run_id == source_run.id
    assert target_task.requested_session_id == nil
    assert target_task.input["session_mode"] == "handoff"
    assert target_task.input["handoff_source_run_id"] == source_run.id
    assert target_task.input["subject"] == source_subject

    target_context =
      Repo.get!(SymmetryControl.Goals.ContextSnapshot, target_task.context_snapshot_id)

    assert target_context.payload["validated_evidence"] ==
             source_context.payload["validated_evidence"]

    handoff_source =
      Enum.find(target_context.payload["sources"], fn source ->
        source["source_kind"] == "handoff_source"
      end)

    assert is_map(handoff_source)
    assert handoff_source["resource_id"] == fixture.repository_resource_id
    assert handoff_source["source_revision"] == "run:#{source_run.id}"
    assert handoff_source["content"]["value"] == "run:#{source_run.id}"
    assert handoff_source["trust"] == "advisory"
    assert handoff_source["required"] == true
    assert is_binary(handoff_source["content_hash"])

    target_assigned = assign_goal_task!(context.daemon_port, target_task.id)
    assert target_assigned.task_id == target_task.id
    assert target_assigned.runtime_id == runtime.id
    refute target_assigned.id == source_run.id

    # A completed source and one claimed target leave no second assignment for
    # the scheduler. The final request-count assertion below also proves no
    # replay opened another native target process.
    assert {:error, :no_assignment} = Orchestration.assign_one()

    target_run_key = {target_assigned.id, target_assigned.generation}

    await_gateway_waiter!(
      context.daemon_port,
      gateway_state,
      6,
      "native Pi handoff target final response waiter"
    )

    target_process_marker =
      await_process_marker!(
        context.process_marker_path,
        target_run_key,
        "native Pi handoff target process identity"
      )

    target_workspace_path =
      Path.join([
        context.workspace_root,
        "binding-pi-control-e2e",
        "run-#{target_assigned.id}",
        "generation-#{target_assigned.generation}"
      ])

    assert target_process_marker["pid"] > 0
    assert File.dir?(target_workspace_path)
    refute normalize_path(target_workspace_path) == normalize_path(source_workspace_path)

    target_subject =
      await_candidate_subject!(
        context.daemon_port,
        target_workspace_path,
        fixture.repository_resource_id,
        source_subject
      )

    assert target_subject["commit"] != source_subject["commit"]

    assert File.read!(Path.join(target_workspace_path, @artifact_path))
           |> String.replace("\r\n", "\n") == @artifact_content

    assert File.read!(Path.join(target_workspace_path, handoff_artifact_path)) ==
             handoff_artifact_content

    Agent.update(gateway_state, fn state ->
      %{state | subject: target_subject, subject_hash: subject_hash!(target_subject)}
    end)

    target_request =
      gateway_state
      |> Agent.get(&Enum.at(&1.requests, 3))
      |> Jason.encode!()

    assert String.contains?(target_request, target_context.payload["content_hash"])
    assert String.contains?(target_request, "run:#{source_run.id}")
    refute String.contains?(target_request, source_run.harness_session_id)

    release_gateway_response!(gateway_state, 6)

    target_task =
      await!(context.daemon_port, "completed native Pi handoff target Task", fn ->
        case Repo.get(Task, target_task.id) do
          %Task{state: "completed"} = task ->
            {:ok, task}

          %Task{state: "failed", failure: failure} ->
            flunk("Pi handoff target task failed: #{inspect(failure)}")

          _ ->
            :retry
        end
      end)

    target_run = Repo.get_by!(Run, id: target_assigned.id)
    assert target_run.state == "completed"
    assert target_run.result["task_result"]["kind"] == "candidate_completion"
    assert target_run.result["task_result"]["subject"] == target_subject
    assert target_run.result["task_result"]["subject_hash"] == subject_hash!(target_subject)
    assert target_run.result["task_result"]["evidence_refs"] == []
    assert target_run.provider_access_snapshot == %{"v" => 1, "kind" => "none"}
    refute target_run.harness_session_id == source_run.harness_session_id
    refute target_run.harness_binding_id == source_run.harness_binding_id

    {target_attach_receipt, target_stop_receipt} =
      await!(context.daemon_port, "native Pi handoff target attach and stop receipts", fn ->
        attach = Repo.get_by(HarnessSessionAttachReceipt, run_id: target_run.id)
        stop = Repo.get_by(HarnessSessionStopReceipt, run_id: target_run.id)
        if attach && stop, do: {:ok, {attach, stop}}, else: :retry
      end)

    assert target_attach_receipt.runtime_id == target_run.runtime_id
    assert target_attach_receipt.runtime_epoch == target_run.claimed_runtime_epoch
    assert target_attach_receipt.generation == target_run.generation
    assert target_attach_receipt.claim_id == target_run.claim_id
    assert target_attach_receipt.lease_token == target_run.lease_token
    assert target_attach_receipt.session_id == target_run.harness_session_id
    assert target_stop_receipt.run_id == target_run.id
    assert target_stop_receipt.session_id == target_run.harness_session_id
    assert target_stop_receipt.binding_id == target_run.harness_binding_id
    refute target_attach_receipt.session_id == source_attach_receipt.session_id
    refute target_stop_receipt.binding_id == source_stop_receipt.binding_id

    source_session = Repo.get!(HarnessSession, source_run.harness_session_id)
    target_session = Repo.get!(HarnessSession, target_run.harness_session_id)
    assert source_session.state == "available"
    assert target_session.state == "available"
    assert is_nil(source_session.active_run_id)
    assert is_nil(target_session.active_run_id)
    refute source_session.local_handle_id == target_session.local_handle_id

    await_empty_goal_outbox!(context.daemon_port, context.state_dir, target_run_key)

    session_journals =
      context.state_dir
      |> Path.join("sessions")
      |> File.ls!()
      |> Enum.filter(&(String.starts_with?(&1, "session-") and String.ends_with?(&1, ".json")))
      |> Enum.map(fn filename ->
        {:ok, contents} =
          read_bounded_file(Path.join(Path.join(context.state_dir, "sessions"), filename))

        Jason.decode!(contents)
      end)
      |> Enum.filter(&(&1["goal_id"] == fixture.goal.id))

    assert length(session_journals) == 2
    source_journal = Enum.find(session_journals, &(&1["run_id"] == source_run.id))
    target_journal = Enum.find(session_journals, &(&1["run_id"] == target_run.id))
    assert is_map(source_journal)
    assert is_map(target_journal)
    assert source_journal["session_mode"] == "fresh"
    assert source_journal["stop_certificate"]["run_id"] == source_run.id
    assert target_journal["session_mode"] == "handoff"
    assert target_journal["handoff_source_run_id"] == source_run.id
    assert target_journal["control_session_id"] == target_run.harness_session_id
    assert target_journal["stop_certificate"]["run_id"] == target_run.id
    refute target_journal["native_session_id"] == source_journal["native_session_id"]
    refute target_journal["local_handle_id"] == source_journal["local_handle_id"]

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(target_task.id, target_run.id, target_run.generation)

    gateway = Agent.get(gateway_state, & &1)
    assert gateway.errors == []
    assert gateway.responses == 6
    assert length(gateway.requests) == 6
    assert length(gateway.authorizations) == 6
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

    File.write!(context.process_marker_path, "starting\n")

    second_daemon =
      start_daemon!(
        context.daemon,
        context.config_path,
        context.process_marker_path,
        context.root,
        context.root_cleanup_state
      )

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

  defp create_goal_fixture!(repository_path, profile, workspace, acceptance_witness?) do
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
    acceptance = acceptance_contract(resource.id, acceptance_witness?)

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
          description:
            if(acceptance_witness?,
              do: "Use the configured real Pi adapter to create and commit one artifact.",
              else: "Use the configured real Pi adapter to create one untracked artifact."
            ),
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

  defp acceptance_contract(repository_resource_id, acceptance_witness?) do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" =>
        if(acceptance_witness?,
          do: "One committed artifact must exist in the daemon-owned worktree.",
          else: "One untracked artifact must exist in the daemon-owned worktree."
        ),
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

  defp await_candidate_subject!(daemon_port, workspace_path, resource_id, baseline_subject) do
    await!(daemon_port, "committed candidate Subject", fn ->
      case System.cmd(
             "git",
             ["-C", workspace_path, "rev-parse", "HEAD"],
             system_cmd_options(stderr_to_stdout: true)
           ) do
        {output, 0} ->
          commit = String.trim(output)

          if commit == baseline_subject["commit"] do
            :retry
          else
            try do
              subject = %{
                "resource_id" => resource_id,
                "commit" => commit,
                "tree_digest" => git_tree_digest!(workspace_path, commit)
              }

              {:ok, subject}
            rescue
              _ -> :retry
            end
          end

        _ ->
          :retry
      end
    end)
  end

  defp run_fence(run) do
    %{
      runtime_id: run.runtime_id,
      runtime_epoch: run.claimed_runtime_epoch,
      generation: run.generation,
      claim_id: run.claim_id,
      lease_token: run.lease_token
    }
  end

  defp hard_crash_recovery_observation(
         daemon,
         state_dir,
         {run_id, _generation} = key,
         task_id,
         goal_id,
         runtime_id,
         workspace_path
       ) do
    case Repo.get(Task, task_id) do
      %Task{state: "failed", failure: %{"reason" => "unknown_outcome"}} = task ->
        run = Repo.get_by!(Run, id: run_id)

        if terminal_crash_recovery_barrier?(state_dir, key, run, task, workspace_path, goal_id) do
          {:terminal, task}
        else
          {:wait,
           crash_recovery_diagnostic(
             daemon,
             state_dir,
             key,
             run,
             task,
             runtime_id,
             workspace_path,
             goal_id
           )}
        end

      %Task{state: "failed", failure: failure} ->
        {:error, "unexpected failure #{inspect(failure)}"}

      %Task{} = task ->
        run = Repo.get_by!(Run, id: run_id)

        {:wait,
         crash_recovery_diagnostic(
           daemon,
           state_dir,
           key,
           run,
           task,
           runtime_id,
           workspace_path,
           goal_id
         )}

      _ ->
        :retry
    end
  end

  defp terminal_crash_recovery_barrier?(
         state_dir,
         key,
         run,
         task,
         workspace_path,
         goal_id
       ) do
    with true <- run.state == "failed",
         true <- task.state == "failed",
         true <- Map.get(task.failure || %{}, "reason") == "unknown_outcome",
         true <-
           Repo.exists?(
             from transition in RunTransition,
               where:
                 transition.run_id == ^run.id and transition.state == "failed" and
                   fragment("? ->> 'reason' = ?", transition.payload, "unknown_outcome")
           ),
         true <-
           Repo.aggregate(
             from(usage in SymmetryControl.Goals.RunUsage, where: usage.run_id == ^run.id),
             :count
           ) == 1,
         session_journal <- goal_session_journal_for_goal!(state_dir, goal_id),
         true <-
           terminal_crash_recovery_persistence_barrier?(
             state_dir,
             key,
             run,
             session_journal,
             workspace_path
           ) do
      true
    else
      _ -> false
    end
  end

  defp terminal_crash_recovery_persistence_barrier?(
         state_dir,
         key,
         run,
         session_journal,
         workspace_path
       ) do
    case journal_for_run(state_dir, key) do
      {:ok, journal} ->
        retained_terminal_crash_journal?(journal, workspace_path) or
          terminal_crash_recovery_stop_receipt_barrier?(run, session_journal)

      :missing ->
        terminal_crash_recovery_stop_receipt_barrier?(run, session_journal)
    end
  end

  defp retained_terminal_crash_journal?(journal, workspace_path) when is_map(journal) do
    is_integer(journal["pid"]) and journal["pid"] > 0 and
      is_binary(journal["process_identity"]) and journal["process_identity"] != "" and
      journal["local_state"] in ["terminal_pending", "cleanup_pending", "stale"] and
      normalize_path(journal["workspace_path"]) == normalize_path(workspace_path) and
      (journal["workspace_recovery_required"] == true or journal["retain_workspace"] == true)
  end

  defp terminal_crash_recovery_stop_receipt_barrier?(run, session_journal) do
    session_id = run.harness_session_id
    binding_id = run.harness_binding_id
    run_id = run.id
    generation = run.generation

    with true <- session_journal["session_state"] == "available",
         %HarnessSession{
           state: "available",
           active_run_id: nil,
           binding_verified: true,
           local_handle_id: local_handle_id,
           binding_id: ^binding_id
         } <- Repo.get(HarnessSession, session_id),
         true <- is_binary(local_handle_id) and local_handle_id != "",
         %HarnessSessionStopReceipt{
           id: receipt_id,
           run_id: ^run_id,
           session_id: ^session_id,
           binding_id: ^binding_id
         } <-
           Repo.get_by(HarnessSessionStopReceipt,
             run_id: run_id,
             session_id: session_id,
             binding_id: binding_id
           ),
         %{
           "run_id" => ^run_id,
           "generation" => ^generation,
           "session_id" => ^session_id,
           "local_handle_id" => ^local_handle_id,
           "binding_id" => ^binding_id,
           "receipt_id" => ^receipt_id
         } <- session_journal["stop_certificate"],
         true <- session_journal["control_session_id"] == session_id,
         true <- session_journal["local_handle_id"] == local_handle_id do
      true
    else
      _ -> false
    end
  end

  defp assert_crash_recovery_stop_receipt!(
         run,
         session,
         session_journal,
         attach_receipt,
         stop_receipt
       ) do
    session_id = run.harness_session_id
    binding_id = run.harness_binding_id

    assert attach_receipt.run_id == run.id
    assert attach_receipt.session_id == session_id
    assert attach_receipt.generation == run.generation
    assert attach_receipt.response["session"]["session_id"] == session_id
    assert attach_receipt.response["session"]["binding_id"] == binding_id

    assert stop_receipt.run_id == run.id
    assert stop_receipt.session_id == session_id
    assert stop_receipt.binding_id == binding_id

    stopped = stop_receipt.response["session_stopped"]
    assert stopped["receipt_id"] == stop_receipt.id
    assert stopped["run_id"] == run.id
    assert stopped["session_id"] == session_id
    assert stopped["local_handle_id"] == session.local_handle_id
    assert stopped["binding_id"] == binding_id
    assert stopped["state"] == "available"
    assert is_nil(stopped["active_run_id"])

    assert session.state == "available"
    assert is_nil(session.active_run_id)
    assert session.binding_verified
    assert session.binding_id == binding_id
    assert session_journal["session_state"] == "available"
    assert session_journal["control_session_id"] == session_id
    assert session_journal["local_handle_id"] == session.local_handle_id

    certificate = session_journal["stop_certificate"]
    assert certificate["run_id"] == run.id
    assert certificate["generation"] == run.generation
    assert certificate["session_id"] == session_id
    assert certificate["local_handle_id"] == session.local_handle_id
    assert certificate["binding_id"] == binding_id
    assert certificate["receipt_id"] == stop_receipt.id
    assert is_binary(certificate["delivery_digest"])
    assert certificate["delivery_digest"] != ""
  end

  defp crash_recovery_diagnostic(
         daemon,
         state_dir,
         key,
         run,
         task,
         runtime_id,
         workspace_path,
         goal_id
       ) do
    journal =
      case journal_for_run(state_dir, key) do
        {:ok, value} -> value
        :missing -> :missing
      end

    session_journal = goal_session_journal_for_goal!(state_dir, goal_id)
    runtime = Repo.get(Runtime, runtime_id)

    process_state =
      case journal do
        %{"pid" => pid, "process_identity" => identity} ->
          native_process_stop_status(pid, identity)

        _ ->
          :missing
      end

    journal_presence =
      case journal_for_run(state_dir, key) do
        {:ok, value} -> {:present, Map.keys(value)}
        :missing -> :missing
      end

    retained_journal? =
      is_map(journal) and retained_terminal_crash_journal?(journal, workspace_path)

    stop_receipt_barrier? = terminal_crash_recovery_stop_receipt_barrier?(run, session_journal)

    %{
      daemon_state: daemon_state(daemon.port, daemon.os_pid, daemon.process_identity),
      runtime_status: runtime && runtime.status,
      task_state: task.state,
      task_failure: task.failure,
      run_state: run.state,
      run_result_present?: not is_nil(run.result),
      journal_local_state: value_from_map(journal, "local_state"),
      journal_process_state: process_state,
      journal_presence: journal_presence,
      retained_journal?: retained_journal?,
      stop_receipt_barrier?: stop_receipt_barrier?,
      persistence_barrier?: retained_journal? or stop_receipt_barrier?,
      journal_workspace_recovery_required: value_from_map(journal, "workspace_recovery_required"),
      journal_retain_workspace: value_from_map(journal, "retain_workspace"),
      journal_workspace_matches?:
        value_from_map(journal, "workspace_path") == normalize_path(workspace_path),
      session_launch_state: session_journal["launch_state"],
      session_state: session_journal["session_state"],
      session_recovery_required: session_journal["recovery_required"],
      session_uncertain_reason: session_journal["uncertain_reason"],
      stop_receipts:
        Repo.aggregate(
          from(receipt in HarnessSessionStopReceipt, where: receipt.run_id == ^run.id),
          :count
        ),
      usage_rows:
        Repo.aggregate(
          from(usage in SymmetryControl.Goals.RunUsage, where: usage.run_id == ^run.id),
          :count
        ),
      terminal_transitions:
        Repo.aggregate(
          from(transition in RunTransition,
            where:
              transition.run_id == ^run.id and
                transition.state in ["completed", "failed", "cancelled"]
          ),
          :count
        )
    }
  end

  defp value_from_map(value, key) when is_map(value), do: Map.get(value, key)
  defp value_from_map(_value, _key), do: nil

  defp wrong_artifact_evidence(run_id, resource_id, subject) do
    subject_hash = subject_hash!(subject)

    %{
      schema_version: "symmetry.evidence.v1",
      evidence_id: Ecto.UUID.generate(),
      run_id: run_id,
      evidence_key: "artifact:wrong-subject",
      kind: "artifact",
      subject: subject,
      subject_hash: subject_hash,
      source_ref: %{
        kind: "artifact",
        ref: "wrong-subject",
        resource_id: resource_id,
        commit: subject["commit"],
        path: @artifact_path,
        subject_hash: subject_hash
      },
      source_revision: "commit:" <> subject["commit"],
      validator_profile: "artifact",
      verdict: "passed",
      payload: %{
        predicate_id: "artifact",
        subject: subject,
        subject_hash: subject_hash,
        resource_id: resource_id,
        commit: subject["commit"],
        path: @artifact_path,
        content_digest: "sha256:" <> String.duplicate("0", 64)
      },
      observed_at: DateTime.to_iso8601(DateTime.utc_now())
    }
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
            Enum.join(Map.get(values, :tools, ["write"]), ",")
          ],
          input_mode: "json",
          provider_access: false,
          interactive: false,
          supervisory_control: false,
          event_format: "raw",
          env_allowlist: pi_e2e_environment_allowlist()
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

  defp daemon_tools(false), do: ["write"]

  defp daemon_tools(true) do
    if match?({:win32, _}, :os.type()), do: ["write", "powershell"], else: ["write", "bash"]
  end

  defp pi_e2e_environment_allowlist do
    base = ["PI_CODING_AGENT_DIR"]

    if match?({:win32, _}, :os.type()) do
      base ++ ["USERPROFILE", "APPDATA", "LOCALAPPDATA"]
    else
      base ++ ["XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME"]
    end
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

  defp gateway_tool_calls(false), do: %{1 => %{path: @artifact_path, content: @artifact_content}}

  defp gateway_tool_calls(true) do
    %{
      1 => %{name: "write", path: @artifact_path, content: @artifact_content},
      2 => %{
        name: candidate_commit_tool(),
        call_id: "call_pi_control_acceptance_commit",
        item_id: "fc_pi_control_acceptance_commit",
        arguments: %{command: candidate_commit_command(), timeout: 30}
      }
    }
  end

  defp candidate_commit_tool do
    if match?({:win32, _}, :os.type()), do: "powershell", else: "bash"
  end

  defp candidate_commit_command do
    if match?({:win32, _}, :os.type()) do
      "$ErrorActionPreference='Stop'; git add -- #{@artifact_path}; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; git -c user.name='Symmetry Pi Control E2E' -c user.email='symmetry-pi-control-e2e@example.invalid' commit --quiet -m 'Pi Control E2E candidate'; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }; git rev-parse HEAD"
    else
      "set -eu; git add -- #{@artifact_path}; git -c user.name='Symmetry Pi Control E2E' -c user.email='symmetry-pi-control-e2e@example.invalid' commit --quiet -m 'Pi Control E2E candidate'; git rev-parse HEAD"
    end
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

    {name, call_id, item_id, arguments} =
      case tool_call do
        %{name: name, arguments: arguments} = spec ->
          {name, Map.get(spec, :call_id), Map.get(spec, :item_id), Jason.encode!(arguments)}

        %{path: path, content: content} ->
          {call_id, item_id, content} =
            if response_number == 1 do
              marker = state.opaque_marker || "regular"

              {state.first_call_id || "call_pi_control_e2e_#{marker}",
               state.first_item_id || "fc_pi_control_e2e_#{marker}", content}
            else
              {"call_pi_control_e2e_resume", "fc_pi_control_e2e_resume",
               state.resume_marker <> "\n"}
            end

          {"write", call_id, item_id, Jason.encode!(%{"path" => path, "content" => content})}
      end

    item = %{
      "id" => item_id,
      "type" => "function_call",
      "status" => "completed",
      "call_id" => call_id,
      "name" => name,
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
    result_kind = if state.acceptance_witness, do: "candidate_completion", else: "progress"

    result_summary =
      if state.acceptance_witness,
        do: "real Pi created and committed one repository artifact",
        else: "real Pi created one untracked repository artifact"

    result = %{
      "schema_version" => "symmetry.task_result.v1",
      "result_id" => Ecto.UUID.generate(),
      "kind" => result_kind,
      "summary" => result_summary,
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
    case System.cmd("git", ["-C", directory | args], system_cmd_options(stderr_to_stdout: true)) do
      {_output, 0} -> :ok
      {output, status} -> raise "git #{Enum.join(args, " ")} failed (#{status}): #{output}"
    end
  end

  defp git_output!(directory, args) do
    case System.cmd("git", ["-C", directory | args], system_cmd_options(stderr_to_stdout: true)) do
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
           system_cmd_options(cd: daemon_dir, stderr_to_stdout: true)
         ) do
      {_output, 0} -> path
      {output, status} -> raise "go build test-only Pi witness failed (#{status}):\n#{output}"
    end
  end

  defp start_daemon!(executable, config_path, process_marker_path, root, root_cleanup_state) do
    start_daemon_with_retry!(
      executable,
      config_path,
      root,
      root_cleanup_state,
      System.monotonic_time(:millisecond) + @daemon_startup_retry_timeout_ms,
      @daemon_startup_retry_initial_backoff_ms
    )
    |> Map.put(:process_marker_path, process_marker_path)
    |> Map.put(:root_cleanup_state, root_cleanup_state)
  end

  defp start_daemon_with_retry!(
         executable,
         config_path,
         root,
         root_cleanup_state,
         deadline,
         backoff_ms
       ) do
    start_daemon_attempt!(executable, config_path, root, root_cleanup_state)
  rescue
    exception in RuntimeError ->
      message = Exception.message(exception)
      remaining = deadline - System.monotonic_time(:millisecond)

      if remaining > 0 and String.contains?(message, "state directory is already in use") do
        await_startup_retry_backoff!(min(backoff_ms, remaining))

        start_daemon_with_retry!(
          executable,
          config_path,
          root,
          root_cleanup_state,
          deadline,
          min(backoff_ms * 2, @daemon_startup_retry_max_backoff_ms)
        )
      else
        reraise exception, __STACKTRACE__
      end
  end

  defp await_startup_retry_backoff!(delay_ms) when delay_ms > 0 do
    token = make_ref()
    Process.send_after(self(), {:pi_control_startup_retry, token}, delay_ms)

    receive do
      {:pi_control_startup_retry, ^token} ->
        :ok
    end
  end

  defp start_daemon_attempt!(executable, config_path, root, root_cleanup_state) do
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
          process_identity:
            await_startup_process_identity!(
              port,
              os_pid,
              spawn_executable,
              root,
              root_cleanup_state
            ),
          stop_state: :atomics.new(1, signed: false)
        }

      other ->
        startup_port_failure!(
          port,
          root,
          root_cleanup_state,
          "test-only Pi witness daemon did not expose an OS process ID: #{inspect(other)}"
        )
    end
  end

  defp cleanup_root_if_proven!(root, root_cleanup_state) do
    if :atomics.get(root_cleanup_state, 1) == 0 do
      await_root_removed!(
        root,
        System.monotonic_time(:millisecond) + @daemon_root_cleanup_timeout_ms
      )
    end
  end

  defp await_root_removed!(root, deadline) do
    case File.rm_rf(root) do
      {:ok, _removed} ->
        if File.exists?(root) do
          retry_root_removal!(root, deadline, "root still exists after File.rm_rf")
        else
          :ok
        end

      {:error, reason, path} ->
        retry_root_removal!(root, deadline, "#{inspect(reason)} at #{path}")
    end
  rescue
    exception ->
      retry_root_removal!(root, deadline, Exception.message(exception))
  end

  defp retry_root_removal!(root, deadline, last_error) do
    if System.monotonic_time(:millisecond) >= deadline do
      raise "could not remove Pi Control E2E root #{root} after bounded cleanup: #{sanitize_cleanup_reason(last_error)}"
    else
      Process.sleep(min(50, max(deadline - System.monotonic_time(:millisecond), 1)))
      await_root_removed!(root, deadline)
    end
  end

  defp daemon_command(executable, config_path) do
    # Launch the witness itself as the owned Port process. A `setsid --wait`
    # wrapper can exit while its child survives and retains the state lock, so
    # daemon teardown must own the actual process directly.
    {executable, ["-config", config_path]}
  end

  defp stop_daemon!(
         %{os_pid: os_pid, stop_state: stop_state, root_cleanup_state: root_cleanup_state} =
           daemon
       ) do
    case :atomics.compare_exchange(stop_state, 1, 0, 1) do
      :ok ->
        try do
          stop_daemon_once!(daemon)
          :atomics.put(stop_state, 1, 2)
          :ok
        rescue
          exception ->
            :atomics.put(stop_state, 1, 0)
            :atomics.put(root_cleanup_state, 1, 1)
            reraise exception, __STACKTRACE__
        catch
          kind, reason ->
            :atomics.put(stop_state, 1, 0)
            :atomics.put(root_cleanup_state, 1, 1)
            :erlang.raise(kind, reason, __STACKTRACE__)
        end

      1 ->
        try do
          await_daemon_stopped!(daemon)
        rescue
          exception ->
            :atomics.put(root_cleanup_state, 1, 1)
            reraise exception, __STACKTRACE__
        catch
          kind, reason ->
            :atomics.put(root_cleanup_state, 1, 1)
            :erlang.raise(kind, reason, __STACKTRACE__)
        end

      2 ->
        :ok

      state ->
        raise "invalid Pi witness stop state #{inspect(state)} for OS PID #{os_pid}"
    end
  end

  defp stop_daemon_once!(daemon) do
    stop_result =
      capture_stop_result(fn ->
        stop_daemon_process!(daemon)
      end)

    cleanup_result =
      capture_stop_result(fn ->
        cleanup_native_process_after_daemon_stop!(daemon)
      end)

    finish_stop_results!(stop_result, cleanup_result)
  end

  defp stop_daemon_process!(%{
         port: port,
         os_pid: os_pid,
         process_identity: process_identity
       }) do
    case daemon_state(port, os_pid, process_identity) do
      :stopped ->
        :ok

      :owned ->
        send_daemon_signal!(port, os_pid, process_identity, :term)
        await_process_exit_or_escalate!(port, os_pid, process_identity)

      {:error, reason} ->
        raise "refusing to terminate Pi witness OS PID #{os_pid}: #{reason}"
    end

    await_port_closed_or_raise!(port, os_pid)
  end

  defp await_daemon_stopped!(daemon) do
    %{port: port, os_pid: os_pid, process_identity: process_identity, stop_state: stop_state} =
      daemon

    stop_result =
      capture_stop_result(fn ->
        case daemon_state(port, os_pid, process_identity) do
          :stopped ->
            await_port_closed_or_raise!(port, os_pid)

          :owned ->
            await_process_exit_or_escalate!(port, os_pid, process_identity)
            await_port_closed_or_raise!(port, os_pid)

          {:error, reason} ->
            raise "Pi witness stop is still in progress but ownership is no longer safe for OS PID #{os_pid}: #{reason}"
        end
      end)

    cleanup_result =
      capture_stop_result(fn ->
        cleanup_native_process_after_daemon_stop!(daemon)
      end)

    case {stop_result, cleanup_result} do
      {:ok, :ok} ->
        :atomics.put(stop_state, 1, 2)
        :ok

      _ ->
        :atomics.put(stop_state, 1, 0)
        finish_stop_results!(stop_result, cleanup_result)
    end
  end

  defp cleanup_native_process_after_daemon_stop!(%{
         os_pid: os_pid,
         process_marker_path: process_marker_path
       }) do
    if os_process_alive?(os_pid) do
      raise "skipping native Pi process cleanup while Pi witness OS PID #{os_pid} remains alive"
    end

    cleanup_native_process_from_marker!(process_marker_path)
  end

  defp capture_stop_result(fun) do
    fun.()
    :ok
  rescue
    exception ->
      {:error, exception, __STACKTRACE__}
  end

  defp finish_stop_results!(:ok, :ok), do: :ok

  defp finish_stop_results!({:error, exception, stacktrace}, :ok),
    do: reraise(exception, stacktrace)

  defp finish_stop_results!(:ok, {:error, exception, stacktrace}),
    do: reraise(exception, stacktrace)

  defp finish_stop_results!(
         {:error, stop_exception, _stop_stacktrace},
         {:error, cleanup_exception, _cleanup_stacktrace}
       ) do
    raise "Pi witness stop failed: #{Exception.message(stop_exception)}; native process cleanup failed: #{Exception.message(cleanup_exception)}"
  end

  defp daemon_state(port, os_pid, process_identity) do
    case daemon_port_os_pid(port) do
      :closed ->
        daemon_state_after_port_closed(os_pid, process_identity)

      {:ok, ^os_pid} ->
        case recorded_daemon_state(os_pid, process_identity) do
          :stopped ->
            :stopped

          :owned ->
            :owned

          {:error, {:identity_changed, actual}} ->
            {:error,
             "the Port still refers to the recorded PID but its process identity changed to #{inspect(actual)}"}

          {:error, {:identity_unavailable, reason}} ->
            {:error,
             "the Port still refers to the recorded PID but its process identity was not readable: #{sanitize_cleanup_reason(reason)}"}
        end

      {:ok, actual_pid} ->
        {:error, "the Port is owned by OS PID #{actual_pid}, not recorded PID #{os_pid}"}

      {:error, reason} ->
        {:error, "could not inspect Port ownership: #{reason}"}
    end
  end

  defp daemon_state_after_port_closed(os_pid, process_identity) do
    await_recorded_daemon_state(
      os_pid,
      process_identity,
      System.monotonic_time(:millisecond) + @daemon_port_timeout_ms,
      "identity query has not completed after the Port closed"
    )
    |> case do
      :stopped ->
        :stopped

      :owned ->
        :owned

      {:error, {:identity_changed, actual}} ->
        {:error,
         "the Port closed while the recorded process identity changed to #{inspect(actual)}"}

      {:error, {:identity_unavailable, reason}} ->
        {:error,
         "the Port closed while the recorded process identity was not verified: #{sanitize_cleanup_reason(reason)}"}
    end
  end

  defp recorded_daemon_state(os_pid, process_identity) do
    if not os_process_alive?(os_pid) do
      :stopped
    else
      case process_identity(os_pid, process_identity.executable) do
        {:ok, ^process_identity} ->
          :owned

        {:ok, actual} ->
          {:error, {:identity_changed, actual}}

        {:error, reason} ->
          {:error, {:identity_unavailable, reason}}
      end
    end
  end

  defp await_recorded_daemon_state(os_pid, process_identity, deadline, last_reason) do
    case recorded_daemon_state(os_pid, process_identity) do
      :stopped ->
        :stopped

      :owned ->
        :owned

      {:error, {:identity_changed, _actual} = reason} ->
        {:error, reason}

      {:error, {:identity_unavailable, reason}} ->
        remaining = deadline - System.monotonic_time(:millisecond)

        if remaining <= 0 do
          {:error, {:identity_unavailable, "#{last_reason}: #{inspect(reason)}"}}
        else
          Process.sleep(min(@daemon_identity_reprobe_interval_ms, remaining))

          await_recorded_daemon_state(
            os_pid,
            process_identity,
            deadline,
            "#{last_reason}: #{inspect(reason)}"
          )
        end
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

  defp terminate_daemon!(os_pid, signal), do: terminate_process!(os_pid, signal, false)

  defp terminate_native_process_group!(os_pid, signal),
    do: terminate_process!(os_pid, signal, true)

  defp terminate_process!(os_pid, signal, group?) do
    result =
      case :os.type() do
        {:win32, _} ->
          taskkill_args =
            ["/PID", Integer.to_string(os_pid)] ++
              if(group?, do: ["/T"], else: []) ++ ["/F"]

          System.cmd(
            "taskkill",
            taskkill_args,
            system_cmd_options(stderr_to_stdout: true)
          )

        _ ->
          target = if group?, do: "-#{os_pid}", else: Integer.to_string(os_pid)

          System.cmd(
            "kill",
            [unix_signal(signal), target],
            system_cmd_options(stderr_to_stdout: true)
          )
      end

    case result do
      {_output, 0} ->
        :ok

      {output, status} ->
        raise "failed to terminate Pi witness OS PID #{os_pid} with #{signal} (status #{status}): #{String.trim(output)}"
    end
  end

  defp send_daemon_signal!(port, os_pid, process_identity, signal) do
    case daemon_state(port, os_pid, process_identity) do
      :stopped ->
        :ok

      :owned ->
        try do
          terminate_daemon!(os_pid, signal)
        rescue
          exception ->
            case await_recorded_daemon_state(
                   os_pid,
                   process_identity,
                   System.monotonic_time(:millisecond) + @daemon_port_timeout_ms,
                   "daemon signal #{signal} failed"
                 ) do
              :stopped ->
                :ok

              :owned ->
                reraise exception, __STACKTRACE__

              {:error, {:identity_changed, actual}} ->
                raise "refusing to retry Pi witness OS PID #{os_pid}: its process identity changed to #{inspect(actual)}"

              {:error, {:identity_unavailable, reason}} ->
                raise "refusing to retry Pi witness OS PID #{os_pid}: could not prove its recorded identity after signal failure: #{sanitize_cleanup_reason(reason)}"
            end
        end

      {:error, reason} ->
        raise "refusing to signal Pi witness OS PID #{os_pid}: #{reason}"
    end
  end

  defp crash_daemon!(%{port: port, os_pid: os_pid, process_identity: process_identity}) do
    send_daemon_signal!(port, os_pid, process_identity, :kill)
    await_process_exit_or_escalate!(port, os_pid, process_identity)
    await_port_closed_or_raise!(port, os_pid)
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
                send_daemon_signal!(port, os_pid, process_identity, :kill)

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
            :ok

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

  defp await_startup_process_identity!(port, os_pid, executable, root, root_cleanup_state) do
    deadline = System.monotonic_time(:millisecond) + @daemon_startup_identity_timeout_ms

    await_startup_process_identity!(
      port,
      os_pid,
      executable,
      root,
      root_cleanup_state,
      deadline,
      @daemon_startup_identity_initial_backoff_ms,
      "identity query has not completed"
    )
  end

  defp await_startup_process_identity!(
         port,
         os_pid,
         executable,
         root,
         root_cleanup_state,
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
              root,
              root_cleanup_state,
              deadline,
              backoff_ms,
              inspect(reason)
            )
        end

      {:ok, actual_pid} ->
        startup_identity_failure!(
          port,
          os_pid,
          executable,
          root,
          root_cleanup_state,
          "Port reported OS PID #{actual_pid} instead of recorded PID #{os_pid}"
        )

      :closed ->
        startup_identity_failure!(
          port,
          os_pid,
          executable,
          root,
          root_cleanup_state,
          "Port closed before process identity was established; last identity result: #{last_reason}"
        )

      {:error, reason} ->
        startup_identity_failure!(
          port,
          os_pid,
          executable,
          root,
          root_cleanup_state,
          "could not inspect Port ownership: #{reason}; last identity result: #{last_reason}"
        )
    end
  end

  defp retry_startup_process_identity!(
         port,
         os_pid,
         executable,
         root,
         root_cleanup_state,
         deadline,
         backoff_ms,
         last_reason
       ) do
    remaining = deadline - System.monotonic_time(:millisecond)

    if remaining <= 0 do
      startup_identity_failure!(
        port,
        os_pid,
        executable,
        root,
        root_cleanup_state,
        "identity query did not become readable before the #{@daemon_startup_identity_timeout_ms}ms startup deadline: #{last_reason}"
      )
    else
      Process.sleep(min(backoff_ms, remaining))

      await_startup_process_identity!(
        port,
        os_pid,
        executable,
        root,
        root_cleanup_state,
        deadline,
        min(backoff_ms * 2, @daemon_startup_identity_max_backoff_ms),
        last_reason
      )
    end
  end

  defp startup_identity_failure!(
         port,
         os_pid,
         executable,
         root,
         root_cleanup_state,
         reason
       ) do
    initial_output = drain_port_output(port)

    cleanup_result =
      cleanup_failed_startup_process!(port, os_pid, executable)

    output = initial_output <> drain_port_output(port)

    case cleanup_result do
      :ok ->
        cleanup_root_if_proven!(root, root_cleanup_state)

        raise "could not establish Pi witness process identity for OS PID #{os_pid}: #{reason}; daemon output: #{String.trim(output)}"

      {:error, cleanup_reason} ->
        :atomics.put(root_cleanup_state, 1, 1)

        raise "could not establish Pi witness process identity for OS PID #{os_pid}: #{reason}; startup cleanup failed: #{cleanup_reason}; daemon output: #{String.trim(output)}"
    end
  end

  defp startup_port_failure!(port, _root, root_cleanup_state, reason) do
    initial_output = drain_port_output(port)
    close_result = close_startup_port(port)
    output = initial_output <> drain_port_output(port)

    case close_result do
      :ok ->
        :atomics.put(root_cleanup_state, 1, 1)

        raise "could not establish Pi witness OS process ownership: #{reason}; daemon output: #{String.trim(output)}"

      {:error, close_reason} ->
        :atomics.put(root_cleanup_state, 1, 1)

        raise "could not establish Pi witness OS process ownership: #{reason}; startup Port cleanup failed: #{close_reason}; daemon output: #{String.trim(output)}"
    end
  end

  defp cleanup_failed_startup_process!(port, os_pid, executable) do
    ownership = startup_cleanup_ownership(port, os_pid, executable)

    case ownership do
      {:owned, process_identity} ->
        cleanup_owned_startup_process!(port, os_pid, process_identity)

      {:unverified, reason} ->
        cleanup_unverified_startup_process!(port, os_pid, reason)
    end
  rescue
    exception ->
      close_result = close_startup_port(port)

      combine_startup_cleanup_results(
        {:error, sanitize_cleanup_reason(Exception.message(exception))},
        close_result
      )
  catch
    kind, reason ->
      close_result = close_startup_port(port)

      combine_startup_cleanup_results(
        {:error, sanitize_cleanup_reason("#{kind}: #{inspect(reason)}")},
        close_result
      )
  end

  defp startup_cleanup_ownership(port, os_pid, executable) do
    case daemon_port_os_pid(port) do
      {:ok, ^os_pid} ->
        case process_identity(os_pid, executable) do
          {:ok, process_identity} ->
            {:owned, process_identity}

          {:error, reason} ->
            {:unverified,
             "the Port still owns OS PID #{os_pid}, but its process identity was not readable: #{sanitize_cleanup_reason(inspect(reason))}"}
        end

      :closed ->
        {:unverified,
         "the Port closed before startup cleanup could verify ownership of OS PID #{os_pid}"}

      {:ok, actual_pid} ->
        {:unverified,
         "the Port reports OS PID #{actual_pid} instead of recorded PID #{os_pid}; refusing numeric PID termination"}

      {:error, reason} ->
        {:unverified,
         "could not inspect Port ownership for OS PID #{os_pid}: #{sanitize_cleanup_reason(reason)}"}
    end
  end

  defp cleanup_owned_startup_process!(port, os_pid, process_identity) do
    process_result =
      capture_startup_cleanup_result(fn ->
        case daemon_state(port, os_pid, process_identity) do
          :stopped ->
            :ok

          :owned ->
            send_daemon_signal!(port, os_pid, process_identity, :term)
            await_process_exit_or_escalate!(port, os_pid, process_identity)

          {:error, reason} ->
            raise "refusing to terminate Pi witness OS PID #{os_pid}: #{reason}"
        end
      end)

    close_result = close_startup_port(port)
    combine_startup_cleanup_results(process_result, close_result)
  end

  defp cleanup_unverified_startup_process!(port, os_pid, reason) do
    close_result = close_startup_port(port)

    process_result =
      capture_startup_cleanup_result(fn ->
        case await_process_exit(
               os_pid,
               System.monotonic_time(:millisecond) + @daemon_stop_timeout_ms
             ) do
          :ok ->
            :ok

          {:error, :timeout} ->
            raise "identity was not established for OS PID #{os_pid}; Port was closed, but the process remains alive, so refusing numeric PID termination (#{reason})"
        end
      end)

    combine_startup_cleanup_results(close_result, process_result)
  end

  defp close_startup_port(port) do
    capture_startup_cleanup_result(fn ->
      case force_close_port(port) do
        :ok ->
          :ok

        {:error, :timeout} ->
          raise "Pi witness Port did not close within the bounded startup cleanup interval"
      end
    end)
  end

  defp capture_startup_cleanup_result(fun) do
    fun.()
    :ok
  rescue
    exception ->
      {:error, sanitize_cleanup_reason(Exception.message(exception))}
  catch
    kind, reason ->
      {:error, sanitize_cleanup_reason("#{kind}: #{inspect(reason)}")}
  end

  defp combine_startup_cleanup_results(:ok, :ok), do: :ok

  defp combine_startup_cleanup_results({:error, reason}, :ok), do: {:error, reason}

  defp combine_startup_cleanup_results(:ok, {:error, reason}), do: {:error, reason}

  defp combine_startup_cleanup_results({:error, first}, {:error, second}),
    do: {:error, "#{first}; additionally: #{second}"}

  defp sanitize_cleanup_reason(reason) when is_binary(reason) do
    reason
    |> String.trim()
    |> String.replace(~r/\s+/, " ")
    |> String.slice(0, 512)
  end

  defp sanitize_cleanup_reason(reason), do: reason |> inspect() |> sanitize_cleanup_reason()

  defp cleanup_native_process_from_marker!(path) do
    case read_native_process_marker(path) do
      :not_started ->
        :ok

      {:ok, marker} ->
        cleanup_native_process!(marker)

      {:error, reason} ->
        raise "refusing native Pi process cleanup from #{path}: #{reason}"
    end
  end

  defp read_native_process_marker(path) do
    case read_bounded_file(path) do
      {:ok, contents} ->
        if String.trim(contents) == "not_started" do
          :not_started
        else
          if String.trim(contents) == "starting" do
            {:error, "process marker is still in starting state"}
          else
            case Jason.decode(contents) do
              {:ok, document} -> validate_native_process_marker(document)
              {:error, reason} -> {:error, "decode process marker: #{inspect(reason)}"}
            end
          end
        end

      {:error, :enoent} ->
        {:error, "process marker is missing"}

      {:error, reason} ->
        {:error, "read process marker: #{inspect(reason)}"}
    end
  end

  defp preserve_native_process_marker!(source_path, root) do
    snapshot_path = Path.join(root, "process-marker-stop-snapshot.json")

    case File.read(source_path) do
      {:ok, contents} ->
        if String.trim(contents) in ["", "not_started", "starting"] do
          raise "cannot preserve a non-terminal native process marker from #{source_path}"
        end

        File.write!(snapshot_path, contents)
        snapshot_path

      {:error, reason} ->
        raise "cannot preserve native process marker #{source_path}: #{inspect(reason)}"
    end
  end

  defp restore_native_process_marker!(snapshot_path, target_path) do
    case File.read(snapshot_path) do
      {:ok, contents} ->
        File.write!(target_path, contents)

      {:error, reason} ->
        raise "cannot restore native process marker #{snapshot_path}: #{inspect(reason)}"
    end
  end

  defp validate_native_process_marker(%{"pid" => pid, "process_identity" => identity})
       when is_integer(pid) and pid > 0 and is_binary(identity) do
    cond do
      identity == "" ->
        {:error, "process marker has an empty process identity"}

      not valid_native_process_identity?(identity) ->
        {:error, "process marker has an invalid identity for #{inspect(:os.type())}"}

      true ->
        {:ok, %{pid: pid, identity: identity}}
    end
  end

  defp validate_native_process_marker(_document) do
    {:error, "process marker must contain a positive PID and non-empty process identity"}
  end

  defp valid_native_process_identity?(identity) do
    case :os.type() do
      {:win32, _} -> Regex.match?(~r/^windows:\d+:[0-9a-f]{16}$/, identity)
      {:unix, :linux} -> Regex.match?(~r/^linux:[^:\s]+:\d+:\d+$/, identity)
      _ -> false
    end
  end

  defp cleanup_native_process!(%{pid: os_pid, identity: expected_identity} = marker) do
    case native_process_state(os_pid, expected_identity) do
      :stopped ->
        :ok

      :owned ->
        send_native_process_signal!(marker, :term)
        await_native_process_exit_or_escalate!(marker)
        assert_native_process_stopped!(marker)

      {:error, reason} ->
        raise "refusing to terminate native Pi OS PID #{os_pid}: #{reason}"
    end
  end

  defp native_process_state(os_pid, expected_identity) do
    if not os_process_alive?(os_pid) do
      :stopped
    else
      case native_process_identity(os_pid) do
        {:ok, ^expected_identity} ->
          :owned

        {:ok, actual_identity} ->
          {:error,
           "the recorded process identity changed to #{inspect(actual_identity)} (expected #{inspect(expected_identity)})"}

        {:error, reason} ->
          if os_process_alive?(os_pid) do
            {:error, "could not read the recorded process identity: #{reason}"}
          else
            :stopped
          end
      end
    end
  end

  defp native_process_stop_status(os_pid, expected_identity) do
    case native_process_state(os_pid, expected_identity) do
      :stopped ->
        :stopped

      :owned ->
        :live

      {:error, reason} when is_binary(reason) ->
        if String.starts_with?(reason, "could not read the recorded process identity:") do
          :unproven
        else
          {:unsafe, reason}
        end
    end
  end

  defp assert_native_process_owned!(%{pid: os_pid, identity: expected_identity}) do
    if os_process_alive?(os_pid) do
      assert_native_process_identity_matches!(os_pid, expected_identity)
    else
      flunk("native Pi OS PID #{os_pid} stopped before the hard-crash boundary")
    end
  end

  defp assert_native_process_identity_matches!(os_pid, expected_identity) do
    case native_process_identity(os_pid) do
      {:ok, ^expected_identity} ->
        :ok

      {:ok, actual_identity} ->
        flunk(
          "native Pi OS PID #{os_pid} creation identity changed to #{inspect(actual_identity)} (expected #{inspect(expected_identity)})"
        )

      {:error, reason} ->
        flunk(
          "could not prove native Pi OS PID #{os_pid} creation identity: #{sanitize_cleanup_reason(reason)}"
        )
    end
  end

  defp send_native_process_signal!(%{pid: os_pid, identity: expected_identity}, signal) do
    case native_process_state(os_pid, expected_identity) do
      :stopped ->
        :ok

      :owned ->
        try do
          # Native Pi runs own a process group on Unix; Windows uses
          # taskkill /T /F. Both are gated by the marker identity.
          terminate_native_process_group!(os_pid, signal)
        rescue
          exception ->
            case native_process_state(os_pid, expected_identity) do
              :stopped ->
                :ok

              :owned ->
                reraise exception, __STACKTRACE__

              {:error, reason} ->
                raise "refusing to retry native Pi OS PID #{os_pid}: #{reason}"
            end
        end

      {:error, reason} ->
        raise "refusing to signal native Pi OS PID #{os_pid}: #{reason}"
    end
  end

  defp await_native_process_exit_or_escalate!(
         %{pid: os_pid, identity: expected_identity} = marker
       ) do
    first_deadline = System.monotonic_time(:millisecond) + @daemon_stop_timeout_ms

    case await_process_exit(os_pid, first_deadline) do
      :ok ->
        :ok

      {:error, :timeout} ->
        case native_process_state(os_pid, expected_identity) do
          :stopped ->
            :ok

          :owned ->
            send_native_process_signal!(marker, :kill)

            kill_deadline =
              System.monotonic_time(:millisecond) + @daemon_kill_timeout_ms

            case await_process_exit(os_pid, kill_deadline) do
              :ok ->
                :ok

              {:error, :timeout} ->
                raise "native Pi OS PID #{os_pid} did not terminate after bounded kill escalation"
            end

          {:error, reason} ->
            raise "refusing native Pi kill escalation for OS PID #{os_pid}: #{reason}"
        end
    end
  end

  defp assert_native_process_stopped!(%{pid: os_pid, identity: expected_identity}) do
    case native_process_state(os_pid, expected_identity) do
      :stopped ->
        :ok

      :owned ->
        raise "native Pi OS PID #{os_pid} remained alive after bounded cleanup"

      {:error, reason} ->
        raise "could not prove native Pi OS PID #{os_pid} stopped: #{reason}"
    end
  end

  # The Go process marker uses platform.ProcessIdentity's wire format. It is
  # intentionally distinct from the Elixir daemon ownership representation.
  defp native_process_identity(os_pid) do
    case :os.type() do
      {:win32, _} -> windows_native_process_identity(os_pid)
      {:unix, :linux} -> linux_native_process_identity(os_pid)
      _ -> {:error, "unsupported native process identity platform"}
    end
  rescue
    exception -> {:error, Exception.message(exception)}
  end

  defp linux_native_process_identity(os_pid) do
    with {:ok, boot_id} <- File.read("/proc/sys/kernel/random/boot_id"),
         {:ok, stat} <- File.read("/proc/#{os_pid}/stat"),
         {:ok, start_time} <- unix_process_start_time(stat) do
      boot_id = String.trim(boot_id)

      if boot_id == "" do
        {:error, "Linux boot ID is empty"}
      else
        {:ok, "linux:#{boot_id}:#{os_pid}:#{start_time}"}
      end
    else
      {:error, reason} -> {:error, inspect(reason)}
    end
  end

  defp windows_native_process_identity(os_pid) do
    script =
      ~s"""
      $ErrorActionPreference = 'Stop'
      $ProgressPreference = 'SilentlyContinue'
      try {
          $process = [System.Diagnostics.Process]::GetProcessById([int]#{os_pid})
          $creation = $process.StartTime.ToUniversalTime().ToFileTimeUtc()
          Write-Output ("windows:#{os_pid}:{0:x16}" -f [uint64]$creation)
      }
      catch {
          Write-Error $_
          exit 2
      }
      """

    case System.cmd(
           "powershell",
           ["-NoProfile", "-NonInteractive", "-Command", script],
           system_cmd_options(stderr_to_stdout: true)
         ) do
      {output, 0} ->
        identity = String.trim(output)

        if valid_native_process_identity?(identity) do
          {:ok, identity}
        else
          {:error, "PowerShell returned invalid process identity #{inspect(identity)}"}
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

    case System.cmd(
           "powershell",
           ["-NoProfile", "-NonInteractive", "-Command", script],
           system_cmd_options(stderr_to_stdout: true)
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
               system_cmd_options(stderr_to_stdout: true)
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
    case System.cmd(
           "kill",
           ["-0", Integer.to_string(os_pid)],
           system_cmd_options(stderr_to_stdout: true)
         ) do
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

  defp required_pi_executable!(platform) do
    value = String.trim(System.get_env("SYMMETRY_PI_CONTROL_E2E_EXECUTABLE") || "")

    if value == "",
      do: flunk("SYMMETRY_PI_CONTROL_E2E_EXECUTABLE must name the real Pi 0.85.1 executable")

    unless Path.type(value) == :absolute, do: flunk("Pi executable path must be absolute")
    if not File.regular?(value), do: flunk("Pi executable path must name a regular file")
    assert_pi_executable_sha256!(value, platform)
    value
  end

  defp assert_pi_executable_sha256!(path, platform) do
    digest =
      case File.open(path, [:read, :binary, :raw]) do
        {:ok, io_device} ->
          try do
            io_device
            |> hash_file_chunks!(:crypto.hash_init(:sha256))
            |> :crypto.hash_final()
            |> Base.encode16(case: :lower)
          after
            File.close(io_device)
          end

        {:error, reason} ->
          flunk("could not open Pi executable for SHA-256 verification: #{inspect(reason)}")
      end

    expected_digest = Map.fetch!(@pi_executable_sha256_by_platform, platform)

    if digest != expected_digest do
      flunk("Pi executable SHA-256 did not equal #{expected_digest}; got #{digest}")
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
    case System.cmd(executable, ["--version"], system_cmd_options(stderr_to_stdout: true)) do
      {output, 0} ->
        if String.trim(output) != @pi_version,
          do: flunk("Pi version did not equal #{@pi_version}")

      {output, status} ->
        flunk("Pi --version failed (#{status}): #{output}")
    end
  end

  defp system_cmd_options(options) do
    case :os.type() do
      {:win32, _} ->
        # Elixir 1.20's System.cmd/3 has no public `hide: true` keyword. It
        # always supplies the underlying Port.open `:hide` option, so keep
        # every Windows command on this explicit synchronous stdio path.
        Keyword.put(options, :use_stdio, true)

      _ ->
        options
    end
  end

  defp assert_supported_platform! do
    platform = {:os.type(), to_string(:erlang.system_info(:system_architecture))}

    if Map.has_key?(@pi_executable_sha256_by_platform, platform) do
      platform
    else
      flunk(
        "Pi Control E2E requires an exact supported OS and architecture (Windows amd64 or Linux amd64); got #{inspect(platform)}"
      )
    end
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
