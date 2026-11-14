# Invoked only by credentialed-provider-witness.py; see its --help for prerequisites.
# This is an opt-in real-provider acceptance witness, not an ExUnit/mock test.

defmodule CredentialedProviderWitness.Failure do
  defexception [:code, message: "witness check failed"]
end

defmodule CredentialedProviderWitness do
  import Ecto.Query
  alias SymmetryControl.{Goals, Integrations, Orchestration, Repo, Workspaces}
  alias SymmetryControl.Goals.{ContextSnapshot, Goal, GoalRevision}
  alias SymmetryControl.Integrations.{ProviderAccess, ProviderActionIntent}
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Task, as: ControlTask
  alias SymmetryControl.Workspaces.ProjectResource
  alias SymmetryControl.Workspaces.WorkItem

  @fixture "wxxb789/symmetry-0006h-provider-fixture"
  @repository_id 1_457_073_269
  @node_id "R_kgDOVtksdQ"
  @secret ~r/(?:gh[pousr]_[A-Za-z0-9_]{8,}|github_pat_[A-Za-z0-9_]+|Bearer\s+[A-Za-z0-9._~+\/-]{12,})/i

  def run!(phase, directory) do
    ensure!(
      System.get_env("SYMMETRY_CREDENTIALED_PROVIDER_WITNESS") == "approved-fixture-only",
      "opt_in_required"
    )

    ensure!(Mix.env() == :test, "test_environment_required")
    # Prevent arbitrary dependency/driver failures from logging raw request context.
    :logger.remove_handler(:default)
    config = read!(Path.join(directory, "input.json"))
    validate_config!(config)
    repo = Repo.config()

    ensure!(
      repo[:port] == 55436 and repo[:database] == "symmetry_0006h_test" and
        repo[:hostname] in ["localhost", "127.0.0.1"],
      "isolated_database_required"
    )

    Application.put_env(
      :symmetry_control,
      Repo,
      repo |> Keyword.put(:pool, DBConnection.ConnectionPool) |> Keyword.put(:pool_size, 8)
    )

    Application.put_env(
      :symmetry_control,
      :integration_http_client,
      CredentialedProviderWitness.HTTP
    )

    Application.put_env(:symmetry_control, :integration_providers,
      github: SymmetryControl.Integrations.Providers.GitHub
    )

    Application.put_env(
      :symmetry_control,
      :integration_auth_provider,
      SymmetryControl.Integrations.Auth
    )

    Application.put_env(
      :symmetry_control,
      :integration_command_runner,
      SymmetryControl.Integrations.Command
    )

    Application.put_env(:symmetry_control, :integrations,
      provider_action_recovery_interval_ms: 500,
      provider_action_timeout_ms: 30_000
    )

    Application.put_env(
      :symmetry_control,
      :orchestration,
      Application.get_env(:symmetry_control, :orchestration, [])
      |> Keyword.put(:lease_duration_ms, 900_000)
    )

    {:ok, _} = Application.ensure_all_started(:ecto_sql)
    {:ok, _} = Application.ensure_all_started(:postgrex)
    {:ok, _} = Application.ensure_all_started(:phoenix)
    {:ok, _} = Application.ensure_all_started(:inets)
    {:ok, _} = Application.ensure_all_started(:ssl)
    {:ok, _} = Repo.start_link()
    {:ok, _} =
      Supervisor.start_link([{Phoenix.PubSub, name: SymmetryControl.PubSub}],
        strategy: :one_for_one
      )

    {:ok, _} = SymmetryControlWeb.Endpoint.start_link()
    controller = self()

    {:ok, _} =
      Agent.start_link(
        fn ->
          %{
            directory: directory,
            config: config,
            phase: phase,
            controller: controller,
            context: nil,
            authorizations: %{},
            hold: nil
          }
        end,
        name: __MODULE__.State
      )

    preflight_invocation!(phase, config)
    if phase == "recover", do: recovery_before_start!()
    {:ok, _} = ProviderAccess.start_link()

    case phase do
      "setup" ->
        setup!(config)

      "timeout" ->
        held_update!("timeout")

      "crash" ->
        held_update!("crash")

      "recover" ->
        recover!()
        verify_read_only_probe!(phase)

      "finish" ->
        verify_read_only_probe!(phase)
        finish!()

      _ ->
        fail!("invalid_phase")
    end

    snapshot!(phase)

    write!("phase-#{phase}.json", %{
      status: "passed",
      phase: phase,
      surface: "Control broker standalone Linux BEAM",
      pid: System.pid(),
      binding: receipt_binding()
    })
  end

  defp validate_config!(config) do
    ensure!(
      Map.get(config, "repository", @fixture) == @fixture and
        Map.get(config, "repository_id", @repository_id) == @repository_id and
        Map.get(config, "repository_node_id", @node_id) == @node_id,
      "fixture_identity_mismatch"
    )

    ensure!(
      is_binary(config["namespace"]) and
        Regex.match?(~r/\A[A-Za-z0-9][A-Za-z0-9_.-]{7,79}\z/, config["namespace"]),
      "invalid_namespace"
    )

    ensure!(match?({:ok, _}, Ecto.UUID.cast(config["resource_id"])), "invalid_resource_id")
    ensure!(config["base_branch"] == "main", "invalid_base")

    Enum.each(["source_branch", "failure_branch"], fn key ->
      ensure!(
        is_binary(config[key]) and String.starts_with?(config[key], config["namespace"] <> "-") and
          Regex.match?(~r/\A[A-Za-z0-9_.\/-]+\z/, config[key]),
        "invalid_branch"
      )
    end)

    Enum.each(["success_subject", "failure_subject"], fn key ->
      subject = config[key]

      ensure!(
        is_map(subject) and
          Enum.sort(Map.keys(subject)) == ["commit", "resource_id", "tree_digest"] and
          subject["resource_id"] == config["resource_id"] and
          Regex.match?(~r/\A[0-9a-f]{40}\z/, subject["commit"]) and
          Regex.match?(~r/\Asha256:[0-9a-f]{64}\z/, subject["tree_digest"]),
        "invalid_subject"
      )
    end)

    ensure!(
      config["success_subject"]["commit"] != config["failure_subject"]["commit"],
      "subjects_must_differ"
    )
  end

  defp setup!(config) do
    ensure!(not File.exists?(path("state.json")), "existing_state_refused")
    ensure!(is_nil(Repo.get(ProjectResource, config["resource_id"])), "existing_resource_refused")

    {:ok, project} =
      Workspaces.create_project(%{
        name: config["namespace"],
        key: "W" <> String.slice(String.replace(Ecto.UUID.generate(), "-", ""), 0, 7),
        default_agent_profile: "codex",
        default_workspace: "primary"
      })

    {:ok, connection} =
      Integrations.create_connection(%{
        provider: "github",
        name: config["namespace"],
        account_ref: "wxxb789",
        capabilities: ["repositories", "changes"]
      })

    resource =
      %ProjectResource{id: config["resource_id"]}
      |> ProjectResource.changeset(%{
        project_id: project.id,
        connection_id: connection.id,
        provider: "github",
        kind: "repository",
        name: config["namespace"],
        external_ref: @fixture
      })
      |> Repo.insert!()

    set_context(%{resource_id: resource.id, connection_id: connection.id, project_id: project.id})
    {:ok, headers} = SymmetryControl.Integrations.Auth.github_headers(connection)
    {:ok, identity} = SymmetryControl.Integrations.Providers.GitHub.check(connection, headers)
    ensure!(identity["actor"] == "wxxb789", "provider_identity_mismatch")
    {:ok, resource} = Integrations.sync_resource(resource.id)

    ensure!(
      resource.metadata["repository_id"] == @repository_id and
        resource.metadata["repository_node_id"] == @node_id,
      "provider_repository_identity_mismatch"
    )

    preflight_subject!(headers, config["source_branch"], config["success_subject"])
    preflight_subject!(headers, config["failure_branch"], config["failure_subject"])
    preflight_subject!(headers, "main", config["failure_subject"])

    success =
      scoped_claim!(
        project,
        resource,
        config["success_subject"],
        config["source_branch"],
        "success"
      )

    failure =
      scoped_claim!(
        project,
        resource,
        config["failure_subject"],
        config["failure_branch"],
        "failure"
      )

    state = %{
      project_id: project.id,
      connection_id: connection.id,
      resource_id: resource.id,
      success: success,
      failure: failure,
      actions: Map.new(["success", "failure", "timeout", "crash"], &{&1, Ecto.UUID.generate()})
    }

    write!("state.json", state)
    authorize_state!(config, read!(path("state.json")))
    success = state_context("success")
    input = input("success")
    result = execute!(success, "success", "change.upsert", input)
    ensure!(match?({:ok, _}, result), "upsert_not_succeeded")
    ensure!(intent("success").state == "succeeded", "upsert_not_durable")
    mutations = mutation_count()
    ensure!(mutations == 1, "upsert_requires_one_real_mutation")
    requests = length(audits())

    ensure!(
      execute!(success, "success", "change.upsert", input) == result,
      "success_replay_mismatch"
    )

    ensure!(
      execute!(success, "success", "change.upsert", Map.put(input, "title", "conflict")) ==
        {:error, :idempotency_conflict},
      "changed_payload_not_conflicting"
    )

    ensure!(
      mutation_count() == mutations and length(audits()) == requests,
      "success_replay_redispatched"
    )

    failure = state_context("failure")
    failed = execute!(failure, "failure", "change.upsert", input("failure"))

    ensure!(
      match?({:error, _}, failed) and intent("failure").state == "failed",
      "failure_not_definitive"
    )

    failure_requests =
      audits() |> Enum.filter(&(&1["action_id"] == action("failure") and &1["method"] == "POST"))

    ensure!(
      length(failure_requests) == 1 and hd(failure_requests)["provider_status"] == 422,
      "real_provider_422_required"
    )

    mutations = mutation_count()
    requests = length(audits())

    ensure!(
      execute!(failure, "failure", "change.upsert", input("failure")) == failed,
      "failure_replay_mismatch"
    )

    ensure!(
      mutation_count() == mutations and length(audits()) == requests,
      "failure_replay_redispatched"
    )

    write!("setup-observations.json", %{
      binding: receipt_binding(),
      identity: identity,
      success: result_value(result),
      failure: result_value(failed),
      success_action_id: action("success"),
      failure_action_id: action("failure"),
      mutation_count: mutations
    })
  end

  defp preflight_subject!(headers, branch, subject) do
    ref = URI.encode(branch, &URI.char_unreserved?/1)

    {:ok, 200, _, body} =
      __MODULE__.HTTP.request(
        :get,
        "https://api.github.com/repos/#{@fixture}/git/ref/heads/#{ref}",
        headers
      )

    ensure!(
      get_in(body, ["object", "sha"]) == subject["commit"],
      "provider_subject_commit_mismatch"
    )

    {:ok, 200, _, commit} =
      __MODULE__.HTTP.request(
        :get,
        "https://api.github.com/repos/#{@fixture}/git/commits/#{subject["commit"]}",
        headers
      )

    # The operator's complete Subject is authoritative for tree_digest; record the
    # actual Git tree OID as a separate observation, never relabel it SHA-256.
    ensure!(is_binary(get_in(commit, ["tree", "sha"])), "provider_tree_identity_missing")

    write!("subject-#{branch |> String.replace("/", "_")}.json", %{
      subject: subject,
      subject_hash: subject_hash(subject),
      provider_commit_sha: commit["sha"],
      provider_git_tree_oid: get_in(commit, ["tree", "sha"]),
      tree_digest_source:
        "complete Subject supplied by parent fixture preparation; this runner does not independently recompute tree_digest",
      subject_preparation_evidence: transport_context().config["subject_preparation_evidence"]
    })
  end

  defp scoped_claim!(project, resource, subject, branch, label) do
    actions = ["change.upsert", "change.update"]
    target = %{"kind" => "branches", "source_branch" => branch, "target_branch" => "main"}

    {:ok, goal} =
      Repo.transaction(fn ->
        goal =
          %Goal{}
          |> Goal.changeset(%{
            project_id: project.id,
            title: "Credentialed provider #{label}",
            state: "active",
            current_revision: 1,
            event_sequence: 0
          })
          |> Repo.insert!()

        %GoalRevision{}
        |> GoalRevision.changeset(%{
          goal_id: goal.id,
          revision: 1,
          objective: "Witness approved fixture provider effect",
          non_goals: [],
          acceptance_contract: %{"checks" => ["credentialed-provider-witness"]},
          authority_policy: %{
            "operator_required_for_scope_change" => true,
            "operator_required_for_completion" => true,
            "publication_allowed" => false,
            "allowed_actions" => actions
          },
          execution_policy: policy(resource.id, actions),
          context_manifest: %{},
          reason: "operator-approved isolated acceptance fixture",
          actor_ref: "operator:wxxb789"
        })
        |> Repo.insert!()

        goal
      end)

    {:ok, item} =
      Workspaces.create_work_item(project.id, %{
        title: "Credentialed provider #{label}",
        status: "ready",
        priority: "high",
        assignee_type: "agent",
        agent_profile: "codex",
        workspace: "primary",
        repository_resource_id: resource.id,
        branch: branch
      })

    item =
      item
      |> WorkItem.goal_membership_changeset(%{
        goal_id: goal.id,
        admitted_revision: 1,
        acceptance_contract: %{"checks" => ["credentialed-provider-witness"]},
        integration: true,
        baseline_subject: subject,
        change_target: target
      })
      |> Repo.update!()

    snapshot =
      %ContextSnapshot{}
      |> ContextSnapshot.changeset(%{
        goal_id: goal.id,
        goal_revision: 1,
        work_item_id: item.id,
        schema_version: 1,
        content_hash: :crypto.hash(:sha256, Jason.encode!(subject)),
        payload: %{"subject" => subject}
      })
      |> Repo.insert!()

    {:ok, %{machine: machine}, :created} =
      Orchestration.enroll_machine(
        %{name: "Control broker fixture; no daemon", machine_token: Ecto.UUID.generate()},
        Ecto.UUID.generate(),
        enrollment_token: "local-witness",
        expected_enrollment_token: "local-witness"
      )

    {:ok, [runtime]} =
      Orchestration.register_runtimes(machine.id, Ecto.UUID.generate(), [
        %{
          runtime_key: label,
          name: "Control broker fixture; no native harness",
          capacity: 1,
          agent_profile: "codex",
          workspace: "primary",
          repository_resource_id: resource.id,
          capabilities: capabilities(),
          harness_kind: "codex",
          harness_version: "0.153.4",
          adapter_version: "symmetry-codex-1",
          adapter_protocol_version: 1
        }
      ])

    task =
      %ControlTask{}
      |> ControlTask.changeset(%{
        idempotency_key: Ecto.UUID.generate(),
        request_hash: :crypto.hash(:sha256, Ecto.UUID.generate()),
        request_hash_version: 1,
        work_item_id: item.id,
        goal_id: goal.id,
        goal_revision: 1,
        context_snapshot_id: snapshot.id,
        goal: "Witness scoped real provider operation",
        agent_profile: "codex",
        workspace: "primary",
        input: %{
          "subject" => subject,
          "provider_resource_ids" => [resource.id],
          "provider_scope" => %{
            "resource_ids" => [resource.id],
            "operations_by_resource" => %{resource.id => actions},
            "change_target" => target
          }
        },
        required_capabilities: %{"provider_access" => true},
        state: "assigned",
        current_generation: 1,
        attempt_generation: 1,
        purpose: "implement",
        admission_key: Ecto.UUID.generate(),
        allowed_runtime_ids: [],
        max_run_attempts: 1
      })
      |> Repo.insert!()

    now = DateTime.utc_now()

    run =
      %Run{}
      |> Run.changeset(%{
        task_id: task.id,
        runtime_id: runtime.id,
        generation: 1,
        state: "assigned",
        assigned_at: now,
        assignment_expires_at: DateTime.add(now, 60, :second)
      })
      |> Repo.insert!()

    {:ok, run} =
      Goals.claim(
        run.id,
        %{
          "runtime_id" => runtime.id,
          "runtime_epoch" => 1,
          "generation" => 1,
          "claim_id" => Ecto.UUID.generate()
        },
        lease_duration_ms: 900_000
      )

    # Claim through normal provider-scope lock/issue/persist, as the controller does.
    if is_nil(run.provider_access_snapshot) do
      {:ok, run} =
        Repo.transaction(fn ->
          {:ok, scope} = ProviderAccess.lock_claim_scope(run.id)
          {:ok, access} = ProviderAccess.issue(scope, run, task)
          {:ok, stored} = ProviderAccess.persist_claim_access(run, access)
          stored
        end)

      ensure!(not is_nil(run.provider_access_snapshot), "claim_access_not_persisted")
    end

    %{
      project_id: project.id,
      goal_id: goal.id,
      task_id: task.id,
      run_id: run.id,
      work_item_id: item.id,
      runtime_id: runtime.id,
      subject: subject,
      subject_hash: subject_hash(subject)
    }
  end

  defp held_update!(label) do
    context = state_context("success")

    ensure!(
      is_nil(
        Repo.get_by(ProviderActionIntent, run_id: context["run_id"], action_id: action(label))
      ),
      "existing_mutation_refused"
    )

    before = mutation_count()

    set_context(
      Map.merge(context, %{"action_id" => action(label), "operation" => "change.update"})
    )

    Agent.update(__MODULE__.State, &%{&1 | hold: label})
    requester = Task.async(fn -> execute!(context, label, "change.update", input(label)) end)

    worker =
      receive do
        {:provider_acknowledged, worker, ^label} -> worker
      after
        25_000 -> fail!("provider_ack_barrier_timeout")
      end

    reference = Process.monitor(worker)
    ensure!(intent(label).state == "executing", "ack_not_before_finalization")
    ensure!(mutation_count() == before + 1, "held_action_mutation_count")

    write!("barrier-#{label}.json", %{
      binding: receipt_binding(),
      action_id: action(label),
      operation: "change.update",
      state: "executing",
      provider_acknowledged: true,
      response_delivered_to_broker: false,
      beam_pid: System.pid(),
      worker: inspect(worker),
      evidence_class:
        if(label == "crash",
          do: "whole BEAM halt after real provider acknowledgement",
          else: "broker dispatch deadline after withheld real provider response"
        )
    })

    snapshot!("#{label}-executing")
    if label == "crash", do: System.halt(86)

    reader_options =
      Keyword.take(Repo.config(), [
        :hostname,
        :port,
        :username,
        :password,
        :database,
        :ssl,
        :ssl_opts
      ])

    # This independent Postgrex connection cannot borrow the Repo/Sandbox session.
    # Its idle fixture transaction starts only after the real PATCH was ACKed.
    {:ok, reader} = Postgrex.start_link(reader_options)

    try do
      DBConnection.run(
        reader,
        fn connection ->
          try do
            Postgrex.query!(connection, "BEGIN", [])
            project_id = Ecto.UUID.dump!(context["project_id"])

            locked =
              Postgrex.query!(
                connection,
                "SELECT id FROM projects WHERE id = $1::uuid FOR SHARE",
                [project_id]
              )

            ensure!(locked.rows == [[project_id]], "independent_project_share_not_acquired")

            ensure!(
              Process.alive?(worker) and intent(label).state == "executing",
              "independent_reader_not_before_timeout"
            )

            %{rows: [[backend_pid]]} = Postgrex.query!(connection, "SELECT pg_backend_pid()", [])

            # No synthetic timeout message: the production broker's configured timer kills
            # the held dispatch worker. Its ordinary recovery sweeps mark/read back it.
            receive do
              {:DOWN, ^reference, :process, ^worker, :killed} -> :ok
            after
              35_000 -> fail!("production_dispatch_timer_not_observed")
            end

            ensure!(
              Task.await(requester, 5_000) == {:error, :provider_failure},
              "timeout_request_outcome_mismatch"
            )

            await_state!(label, "unknown", 5_000)
            snapshot!("timeout-unknown")
            await_state!(label, "succeeded", 30_000)
            completed = intent(label)

            ensure!(
              completed.state == "succeeded" and
                completed.result["readback_status"] == "confirmed",
              "timeout_exact_readback_missing"
            )

            ensure!(completed.result["projected"] == true, "reader_guarded_projection_missing")

            count = mutation_count()
            requests = length(audits())
            stored_result = intent(label).result

            ensure!(
              execute!(context, label, "change.update", input(label)) == {:ok, stored_result},
              "timeout_replay_failed"
            )

            ensure!(
              mutation_count() == count and count == before + 1 and length(audits()) == requests,
              "timeout_readback_redispatched"
            )

            write!("timeout-observations.json", %{
              binding: receipt_binding(),
              action_id: action(label),
              executing_then_unknown_then_succeeded: true,
              independent_project_share_held: true,
              projection_completed_before_reader_release: true,
              project_id: context["project_id"],
              independent_reader_backend_pid: backend_pid,
              status_while_reader_held: %{
                state: completed.state,
                readback_status: completed.result["readback_status"],
                projected: completed.result["projected"]
              },
              project_lock_compatibility:
                "Independent FOR SHARE reader remains held through confirmed projection; the old exclusive FOR UPDATE upgrade would wait for this reader to release",
              broker_timeout_ms: 30_000,
              upstream_timeout_observed: false,
              fault: "real provider acknowledged; response withheld locally",
              mutation_delta: 1
            })
          after
            Postgrex.query(connection, "ROLLBACK", [])
          end
        end,
        timeout: 90_000
      )
    after
      if Process.alive?(reader), do: GenServer.stop(reader)
    end
  end

  defp recovery_before_start! do
    set_context(state_context("success"))
    ensure!(read!(path("barrier-crash.json"))["beam_pid"] != System.pid(), "fresh_beam_required")

    ensure!(
      intent("crash").state in ["executing", "unknown", "succeeded"],
      "crash_recovery_state_invalid"
    )

    snapshot!("restart-before-broker")

    write!("restart-before-broker.json", %{
      binding: receipt_binding(),
      beam_pid: System.pid(),
      crash_action_id: action("crash"),
      state: intent("crash").state,
      mutation_count: mutation_count()
    })
  end

  defp seed_read_only_probe!(phase) do
    # Separate still-authorized work must not escape through the broker's global sweep.
    source = intent(if(phase == "recover", do: "failure", else: "success"))
    state = read!(path("state.json"))
    resource = Repo.get!(ProjectResource, source.resource_id)
    project = Repo.get!(SymmetryControl.Workspaces.Project, state["project_id"])

    # A preserved crashed run may have expired; do not renew or alter its fence.
    claim =
      scoped_claim!(
        project,
        resource,
        source.action_binding["subject"],
        source.action_binding["target"]["source_branch"],
        "read-only-#{phase}"
      )

    run = Repo.get!(Run, claim.run_id)
    item = Repo.get!(WorkItem, claim.work_item_id)
    connection = Repo.get!(SymmetryControl.Integrations.Connection, source.connection_id)
    action_id = Ecto.UUID.generate()

    attrs =
      source
      |> Map.from_struct()
      |> Map.merge(%{
        run_id: run.id,
        task_id: claim.task_id,
        runtime_id: claim.runtime_id,
        work_item_id: item.id,
        runtime_epoch: run.claimed_runtime_epoch,
        generation: run.generation,
        claim_id: run.claim_id,
        resource_lock_version: resource.lock_version,
        connection_lock_version: connection.lock_version,
        work_item_lock_version: item.lock_version,
        action_id: action_id,
        action_binding:
          Map.merge(source.action_binding, %{
            "action_id" => action_id,
            "run_id" => run.id,
            "task_id" => claim.task_id
          }),
        state: "accepted"
      })

    authorization =
      transport_context().authorizations[source.action_id]
      |> Map.merge(Map.new(claim, fn {key, value} -> {Atom.to_string(key), value} end))
      |> Map.put("action_id", action_id)

    Agent.update(__MODULE__.State, fn state ->
      %{state | authorizations: Map.put(state.authorizations, action_id, authorization)}
    end)

    set_context(
      claim
      |> Map.new(fn {key, value} -> {Atom.to_string(key), value} end)
      |> Map.merge(%{"action_id" => action_id, "operation" => source.operation})
    )

    %ProviderActionIntent{}
    |> ProviderActionIntent.accept_changeset(attrs)
    |> Repo.insert!()
  end

  defp verify_read_only_probe!(phase) do
    expected_method = if phase == "recover", do: :post, else: :patch
    controller = self()
    observer_id = make_ref()

    observer = fn registered, event, _name ->
      case event do
        {:noreply, %{jobs: jobs}} ->
          Enum.reduce(jobs, registered, fn {worker, job}, seen ->
            if MapSet.member?(seen, worker) do
              seen
            else
              send(controller, {:read_only_probe_job_registered, worker, job.intent_id})
              MapSet.put(seen, worker)
            end
          end)

        {:in, {:provider_action_complete, pid, _result}} ->
          send(controller, {:read_only_probe_job_complete, pid})
          registered

        _ ->
          registered
      end
    end

    :ok = :sys.install(ProviderAccess, {observer_id, observer, MapSet.new()})
    probe = seed_read_only_probe!(phase)
    probe_id = probe.id
    send(ProviderAccess, :recover_interrupted_dispatches)

    worker =
      receive do
        {:read_only_probe_job_registered, worker, ^probe_id} -> worker
      after
        25_000 -> fail!("read_only_recovery_probe_not_dispatched")
      end

    receive do
      {:read_only_provider_write_blocked, ^worker, ^expected_method} -> :ok
    after
      25_000 -> fail!("read_only_recovery_probe_not_blocked")
    end

    receive do
      {:read_only_probe_job_complete, ^worker} -> :ok
    after
      5_000 -> fail!("read_only_recovery_probe_not_finished")
    end

    :ok = :sys.remove(ProviderAccess, observer_id)
    stored = Repo.get!(ProviderActionIntent, probe.id)
    ensure!(stored.state == "unknown", "read_only_recovery_probe_not_attempted")

    # Finish this probe's readback before another phase/context can own the audit.
    send(ProviderAccess, :recover_interrupted_dispatches)

    ProviderAccess
    |> :sys.get_state()
    |> Map.fetch!(:jobs)
    |> Map.keys()
    |> Enum.each(fn pid ->
      reference = Process.monitor(pid)

      receive do
        {:DOWN, ^reference, :process, ^pid, _reason} -> :ok
      after
        25_000 -> fail!("read_only_probe_readback_not_finished")
      end
    end)

    ensure!(
      is_binary(Repo.get!(ProviderActionIntent, probe.id).failure["readback_observed_at"]),
      "read_only_probe_readback_missing"
    )

    requests = Enum.filter(audits(), &(&1["phase"] == phase))
    ensure!(Enum.all?(requests, &(&1["method"] == "GET")), "read_only_transport_mutation")

    write!("read-only-#{phase}.json", %{
      binding: receipt_binding(),
      action_id: probe.action_id,
      action_binding: probe.action_binding,
      operation: probe.operation,
      initial_state: "accepted",
      state: stored.state,
      blocked_method: expected_method |> Atom.to_string() |> String.upcase(),
      blocked_before_real_transport: true,
      provider_mutations: 0
    })
  end

  defp recover! do
    context = state_context("success")

    set_context(
      Map.merge(context, %{"action_id" => action("crash"), "operation" => "change.update"})
    )

    count = mutation_count()
    await_state!("crash", "succeeded", 30_000)

    ensure!(
      intent("crash").result["readback_status"] == "confirmed",
      "restart_exact_readback_missing"
    )

    ensure!(mutation_count() == count, "restart_recovery_redispatched")
    requests = length(audits())
    stored_result = intent("crash").result
    result = execute!(context, "crash", "change.update", input("crash"))

    ensure!(
      result == {:ok, stored_result} and mutation_count() == count and
        length(audits()) == requests,
      "restart_replay_redispatched"
    )

    readbacks =
      audits()
      |> Enum.filter(
        &(&1["phase"] == "recover" and &1["method"] == "GET" and
            &1["intent_states"][action("crash")] == "unknown")
      )

    ensure!(readbacks != [], "unknown_before_exact_readback_not_observed")

    write!("restart-observations.json", %{
      binding: receipt_binding(),
      action_id: action("crash"),
      state: "succeeded",
      readback_status: "confirmed",
      result: result_value(result),
      mutation_count: count,
      restarted_beam_pid: System.pid()
    })
  end

  defp finish! do
    state = read!(path("state.json"))
    set_context(state_context("success"))
    snapshot!("final")
    requests = audits()
    ensure!(Enum.count(requests, &(&1["method"] == "POST")) == 2, "unexpected_post_total")
    ensure!(Enum.count(requests, &(&1["method"] == "PATCH")) == 2, "unexpected_patch_total")

    ensure!(
      Enum.all?(
        Enum.filter(requests, &(&1["provider_status"] in 200..299)),
        &is_binary(&1["rate_limit"]["used"])
      ),
      "provider_rate_limit_usage_missing"
    )

    write!("receipt.json", %{
      status: "passed_with_limitations",
      surface:
        "Control broker; separate standalone Linux BEAM processes; actual PostgreSQL; direct GitHub HTTPS",
      fixture: @fixture,
      repository_id: @repository_id,
      repository_node_id: @node_id,
      binding: receipt_binding(),
      actions: state["actions"],
      action_receipts:
        Enum.map(["success", "failure", "timeout", "crash"], fn label ->
          stored = intent(label)

          subject =
            state_context(if(label == "failure", do: "failure", else: "success"))["subject"]

          %{
            action_id: action(label),
            operation: stored.operation,
            resource_id: stored.resource_id,
            repository_id: @repository_id,
            repository_node_id: @node_id,
            subject: subject,
            subject_hash: subject_hash(subject),
            state: stored.state,
            action_binding: stored.action_binding,
            result: stored.result,
            failure: stored.failure
          }
        end),
      evidence_classes: [
        "real success",
        "exact replay with no mutation",
        "changed action payload conflict",
        "real provider HTTP 422 definitive failure",
        "production broker timeout with exact readback after locally withheld real response",
        "whole BEAM restart recovery with exact readback",
        "live credential locality and redaction",
        "provider-owned rate limit usage"
      ],
      mutations: %{post: 2, patch: 2},
      money: %{
        cost_microusd: nil,
        basis: "unknown",
        evidence_status: "unavailable",
        provider_http_status: nil,
        zero_or_not_billed_established: false,
        blocker:
          "No documented repository-scoped GitHub monetary usage endpoint exists for these PR API calls; account/org billing is outside authorized scope. No fabricated endpoint or unavailable response was requested."
      },
      redaction: %{
        credential_locality:
          "real production gh_cli Auth; operator Windows gh keyring via PATH wrapper; no fake credentials",
        patterns:
          "OAuth token and Authorization patterns checked in emitted output, every evidence file and allowlisted connection/intent/run durable snapshots",
        exact_value_comparison:
          "not performed: credentials are never retrieved for redaction inspection",
        excluded_fields: ["raw auth headers", "machine token", "broker token", "run lease token"],
        deterministic_fake_key_tests:
          "not run by this live witness; not represented as credentialed evidence"
      },
      limits: [
        "Control broker only: no daemon/native harness execution",
        "timeout fault is local response loss after real provider acknowledgement, not actual GitHub network timeout",
        "provider commit/ref identity verified here; tree_digest preparation evidence is separately parent-owned, optionally linked through subject_preparation_evidence, and not independently replayed by this runner",
        "no remote cleanup or automatic mutation retry",
        "monetary evidence unavailable and money unknown; rate limit counters are not money; goal completion is not asserted"
      ]
    })
  end

  def transport_context, do: Agent.get(__MODULE__.State, & &1)

  defp preflight_invocation!(phase, config) do
    case phase do
      "setup" ->
        ensure!(not File.exists?(path("state.json")), "existing_state_refused")
        ensure!(is_nil(Repo.get(ProjectResource, config["resource_id"])), "existing_resource_refused")

      phase when phase in ["timeout", "crash", "recover", "finish"] ->
        state = read!(path("state.json"))
        resource = Repo.get!(ProjectResource, config["resource_id"])

        ensure!(
          state["resource_id"] == config["resource_id"] and
            resource.project_id == state["project_id"] and
            resource.connection_id == state["connection_id"] and resource.provider == "github" and
            resource.kind == "repository" and resource.external_ref == @fixture and
            resource.metadata["repository_id"] == @repository_id and
            resource.metadata["repository_node_id"] == @node_id,
          "invocation_resource_mismatch"
        )

        authorize_state!(config, state)

      _ ->
        fail!("invalid_phase")
    end

    # The global broker can recover more than this resource. Inspect before it starts;
    # never delete, update or adopt another invocation's durable authorization.
    pending =
      Repo.all(
        from i in ProviderActionIntent,
          where:
            i.state in ["accepted", "executing"] or
              (i.state == "unknown" and
                 fragment("COALESCE(?->>'readback_observed_at', '') = ''", i.failure))
      )

    authorize_pending_intents!(pending, transport_context().authorizations)
  end

  defp authorize_state!(config, state) do
    authorizations = invocation_authorizations(config, state)

    Enum.each(["success", "failure"], fn label ->
      context = state[label]
      task = Repo.get!(ControlTask, context["task_id"])
      run = Repo.get!(Run, context["run_id"])

      ensure!(
        task.input["subject"] == context["subject"] and run.task_id == task.id and
          run.runtime_id == context["runtime_id"] and task.work_item_id == context["work_item_id"] and
          task.goal_id == context["goal_id"],
        "invocation_claim_mismatch"
      )
    end)

    Agent.update(__MODULE__.State, &%{&1 | authorizations: authorizations})
  end

  def invocation_authorizations(config, state) do
    labels = ["success", "failure", "timeout", "crash"]
    ensure!(Enum.sort(Map.keys(state["actions"])) == Enum.sort(labels), "invocation_actions_mismatch")
    ids = Map.values(state["actions"])

    ensure!(
      length(Enum.uniq(ids)) == 4 and Enum.all?(ids, &match?({:ok, _}, Ecto.UUID.cast(&1))),
      "invocation_actions_mismatch"
    )

    Map.new(labels, fn label ->
      owner = if label == "failure", do: "failure", else: "success"
      context = state[owner]

      ensure!(
        context["subject"] == config[owner <> "_subject"] and
          context["subject_hash"] == subject_hash(context["subject"]) and
          context["project_id"] == state["project_id"] and
          state["resource_id"] == config["resource_id"],
        "invocation_subject_mismatch"
      )

      operation = if label in ["success", "failure"], do: "change.upsert", else: "change.update"
      branch = config[if(owner == "failure", do: "failure_branch", else: "source_branch")]

      {state["actions"][label],
       Map.merge(context, %{
         "resource_id" => config["resource_id"],
         "connection_id" => state["connection_id"],
         "action_id" => state["actions"][label],
         "operation" => operation,
         "target" => %{"kind" => "branches", "source_branch" => branch, "target_branch" => "main"},
         "input" => %{
           "title" => "#{config["namespace"]} #{label}",
           "body" => "Approved isolated Control broker acceptance witness: #{label}."
         }
       })}
    end)
  end

  def authorize_pending_intents!(intents, authorizations) do
    Enum.each(intents, fn intent ->
      authorization = authorizations[intent.action_id]
      ensure!(is_map(authorization), "unrelated_pending_intent")
      authorize_intent!(intent, authorization)
    end)
  end

  def authorize_intent!(intent, authorization) do
    fields = [:action_id, :operation, :resource_id, :connection_id, :project_id, :run_id,
              :task_id, :runtime_id, :work_item_id]

    ensure!(
      Enum.all?(fields, &(Map.get(intent, &1) == authorization[Atom.to_string(&1)])) and
        intent.provider == "github" and intent.resource_kind == "repository" and
        intent.resource_external_ref == @fixture and intent.account_ref == "wxxb789",
      "outbound_intent_identity_mismatch"
    )

    input =
      case intent.operation do
        "change.upsert" ->
          Map.merge(authorization["input"], Map.take(authorization["target"], ["source_branch", "target_branch"]))

        "change.update" ->
          url = intent.input["pull_request_url"]
          ensure!(is_binary(url) and Regex.match?(~r/\Ahttps:\/\/github\.com\/wxxb789\/symmetry-0006h-provider-fixture\/pull\/[1-9][0-9]*\z/, url), "outbound_target_mismatch")
          Map.put(authorization["input"], "pull_request_url", url)
      end

    binding = %{
      "v" => 1, "action_id" => intent.action_id, "run_id" => intent.run_id,
      "task_id" => intent.task_id, "subject" => authorization["subject"],
      "repository" => %{"id" => @repository_id, "node_id" => @node_id},
      "operation" => intent.operation, "input" => input, "target" => authorization["target"],
      "title" => authorization["input"]["title"], "body" => authorization["input"]["body"]
    }

    ensure!(intent.input == input and intent.action_binding == binding, "outbound_action_binding_mismatch")
    Map.put(authorization, "action_binding", binding)
  end

  def authorize_request!(method) do
    state = transport_context()

    context =
      if self() == state.controller and state.phase == "setup" and method == :get do
        state.context
      else
        job = Map.get(:sys.get_state(ProviderAccess).jobs, self())
        ensure!(is_map(job) and is_list(job.lock_key), "outbound_worker_not_authorized")
        action_ids = Map.keys(state.authorizations)

        matches =
          Repo.all(from i in ProviderActionIntent, where: i.action_id in ^action_ids)
          |> Enum.filter(fn intent ->
            <<first::signed-32, second::signed-32, _::binary>> =
              :crypto.hash(:sha256, Ecto.UUID.dump!(intent.id))

            job.lock_key == [first, second] and
              (is_nil(job.intent_id) or job.intent_id == intent.id)
          end)

        ensure!(length(matches) == 1, "outbound_worker_not_authorized")
        intent = hd(matches)
        authorize_intent!(intent, state.authorizations[intent.action_id])
      end

    Process.put({__MODULE__, :authorized_context}, context)
    context
  end

  def audit!(method, uri, result, request_body) do
    state = transport_context()
    context = Process.get({__MODULE__, :authorized_context}) || fail!("missing_transport_authorization")
    ensure!(redacted?(snapshots()) and evidence_redacted?(), "durable_redaction_failed")

    response =
      case result do
        {:ok, status, response_headers, body} ->
          if uri.path == "/repos/#{@fixture}" and status == 200 do
            ensure!(
              body["id"] == @repository_id and body["node_id"] == @node_id and
                body["full_name"] == @fixture,
              "remote_repository_identity_mismatch"
            )
          end

          if uri.path == "/user" and status == 200,
            do: ensure!(body["login"] == "wxxb789", "remote_identity_mismatch")

          %{
            provider_status: status,
            provider_request_id: response_headers["x-github-request-id"],
            rate_limit: %{
              used: response_headers["x-ratelimit-used"],
              remaining: response_headers["x-ratelimit-remaining"],
              resource: response_headers["x-ratelimit-resource"]
            },
            provider_object: safe_object(body)
          }

        {:error, _} ->
          %{provider_status: nil, transport_error: "unclassified; raw error discarded"}
      end

    row =
      Map.merge(response, %{
        phase: state.phase,
        method: method |> Atom.to_string() |> String.upcase(),
        host: uri.host,
        path: uri.path,
        query: uri.query,
        action_id: context["action_id"] || context[:action_id],
        operation: context["operation"] || context[:operation],
        resource_id: state.config["resource_id"],
        repository_id: @repository_id,
        repository_node_id: @node_id,
        subject: context["subject"],
        subject_hash: context["subject_hash"],
        code_base_sha: state.config["code_base_sha"],
        manifest_sha256: read!(path("source.json"))["manifest_sha256"],
        request_payload_sha256:
          if(is_nil(request_body), do: nil, else: hash(Jason.encode!(request_body))),
        intent_states: intent_states(),
        observed_at: DateTime.to_iso8601(DateTime.utc_now())
      })

    ensure!(redacted?(row), "audit_redaction_failed")
    {:ok, stream} = File.open(path("http.jsonl"), [:append, :binary])
    :ok = IO.binwrite(stream, Jason.encode!(row) <> "\n")
    :ok = :file.sync(stream)
    :ok = File.close(stream)
    File.chmod!(path("http.jsonl"), 0o600)

    if method == :patch and state.hold in ["timeout", "crash"] do
      ensure!(
        match?({:ok, status, _, _} when status in 200..299, result),
        "held_mutation_not_acknowledged"
      )

      Agent.update(__MODULE__.State, &%{&1 | hold: nil})
      send(state.controller, {:provider_acknowledged, self(), state.hold})

      receive do
        :release_provider_response -> fail!("response_hold_must_not_release")
      end
    end

    result
  end

  defp safe_object(body) when is_map(body) do
    body
    |> Map.take(["id", "node_id", "number", "html_url", "sha", "full_name", "login"])
    |> Map.put("head", safe_ref(body["head"]))
    |> Map.put("base", safe_ref(body["base"]))
    |> Map.put("body_sha256", if(is_binary(body["body"]), do: hash(body["body"]), else: nil))
  end

  defp safe_object(_), do: nil

  defp safe_ref(ref) when is_map(ref),
    do: %{
      sha: ref["sha"],
      ref: ref["ref"],
      repository_id: get_in(ref, ["repo", "id"]),
      repository_node_id: get_in(ref, ["repo", "node_id"])
    }

  defp safe_ref(_), do: nil

  defp snapshots do
    id = transport_context().config["resource_id"]
    # Explicit safe field sets exclude machine/broker/lease tokens entirely.
    # OAuth patterns are checked without additional credential retrieval or
    # inspection of production Auth's transient request headers.
    %{
      connections:
        sql_rows(
          "SELECT jsonb_build_object('id',c.id,'provider',c.provider,'auth_type',c.auth_type,'account_ref',c.account_ref,'capabilities',c.capabilities,'status',c.status,'metadata',c.metadata) FROM external_connections c WHERE id IN (SELECT connection_id FROM project_resources WHERE id = $1::uuid)",
          id
        ),
      intents:
        sql_rows(
          "SELECT jsonb_build_object('id',i.id,'action_id',i.action_id,'operation',i.operation,'state',i.state,'run_id',i.run_id,'task_id',i.task_id,'resource_id',i.resource_id,'connection_id',i.connection_id,'action_binding',i.action_binding,'input',i.input,'result',i.result,'failure',i.failure,'request_hash',encode(i.request_hash,'hex')) FROM provider_action_intents i WHERE resource_id = $1::uuid",
          id
        ),
      runs:
        sql_rows(
          "SELECT jsonb_build_object('id',r.id,'task_id',r.task_id,'runtime_id',r.runtime_id,'generation',r.generation,'state',r.state,'claimed_runtime_epoch',r.claimed_runtime_epoch,'claim_id',r.claim_id,'provider_access_snapshot',r.provider_access_snapshot) FROM runs r JOIN tasks t ON t.id=r.task_id JOIN work_items w ON w.id=t.work_item_id WHERE w.repository_resource_id = $1::uuid",
          id
        )
    }
  end

  defp sql_rows(sql, id),
    do: Ecto.Adapters.SQL.query!(Repo, sql, [Ecto.UUID.dump!(id)]).rows |> Enum.map(&hd/1)

  defp snapshot!(label), do: write!("snapshots-#{label}.json", snapshots())

  defp intent_states do
    resource_id = transport_context().config["resource_id"]

    Repo.all(
      from i in ProviderActionIntent,
        where: i.resource_id == ^resource_id,
        select: {i.action_id, i.state}
    )
    |> Map.new()
  end

  defp evidence_redacted?,
    do:
      Enum.all?(File.ls!(transport_context().directory), fn name ->
        file = path(name)
        not File.regular?(file) or not Regex.match?(@secret, File.read!(file))
      end)

  defp redacted?(value), do: not Regex.match?(@secret, Jason.encode!(value))

  defp execute!(context, label, operation, input) do
    context = Map.merge(context, %{"action_id" => action(label), "operation" => operation})
    set_context(context)
    run = Repo.get!(Run, context["run_id"])
    task = Repo.get!(ControlTask, context["task_id"])
    {:ok, access} = ProviderAccess.replay_claim_access(run, task)

    ProviderAccess.execute(
      access.token,
      action(label),
      transport_context().config["resource_id"],
      operation,
      input
    )
  end

  defp input(label),
    do: %{
      "title" => "#{transport_context().config["namespace"]} #{label}",
      "body" => "Approved isolated Control broker acceptance witness: #{label}."
    }

  defp state_context(label), do: read!(path("state.json"))[label]
  defp action(label), do: read!(path("state.json"))["actions"][label]

  defp intent(label),
    do:
      Repo.get_by!(ProviderActionIntent,
        resource_id: transport_context().config["resource_id"],
        action_id: action(label)
      )

  defp set_context(context), do: Agent.update(__MODULE__.State, &%{&1 | context: context})

  defp audits,
    do:
      if(File.exists?(path("http.jsonl")),
        do: File.stream!(path("http.jsonl")) |> Enum.map(&Jason.decode!/1),
        else: []
      )

  defp mutation_count, do: Enum.count(audits(), &(&1["method"] in ["POST", "PATCH"]))
  defp result_value({:ok, value}), do: %{outcome: "ok", result: value}

  defp result_value({:error, reason}) when is_atom(reason),
    do: %{outcome: "error", reason: Atom.to_string(reason)}

  defp result_value(_), do: %{outcome: "error", reason: "unclassified"}

  defp await_state!(label, expected, timeout),
    do: await_state_until!(label, expected, System.monotonic_time(:millisecond) + timeout)

  defp await_state_until!(label, expected, deadline) do
    if intent(label).state != expected do
      ensure!(
        System.monotonic_time(:millisecond) < deadline,
        "expected_intent_state_not_observed"
      )

      Process.sleep(10)
      await_state_until!(label, expected, deadline)
    end
  end

  defp receipt_binding do
    config = transport_context().config

    %{
      resource_id: config["resource_id"],
      repository_id: @repository_id,
      repository_node_id: @node_id,
      success_subject: config["success_subject"],
      success_subject_hash: subject_hash(config["success_subject"]),
      failure_subject: config["failure_subject"],
      failure_subject_hash: subject_hash(config["failure_subject"]),
      subject_preparation_evidence: config["subject_preparation_evidence"],
      code_base_sha: config["code_base_sha"],
      code_head_sha: config["code_head_sha"],
      changed_source_manifest: config["changed_source_manifest"],
      manifest_sha256: read!(path("source.json"))["manifest_sha256"]
    }
  end

  # Complete Subject has three string fields; lexicographic key order is fixed.
  defp subject_hash(subject),
    do:
      "sha256:" <>
        hash([
          "{\"commit\":",
          Jason.encode!(subject["commit"]),
          ",\"resource_id\":",
          Jason.encode!(subject["resource_id"]),
          ",\"tree_digest\":",
          Jason.encode!(subject["tree_digest"]),
          "}"
        ])

  defp hash(data), do: :crypto.hash(:sha256, data) |> Base.encode16(case: :lower)
  defp path(name), do: Path.join(transport_context().directory, name)
  defp read!(file), do: file |> File.read!() |> Jason.decode!()

  defp write!(name, value) do
    ensure!(redacted?(value), "evidence_redaction_failed")
    {:ok, stream} = File.open(path(name), [:write, :binary, :exclusive])
    File.chmod!(path(name), 0o600)
    :ok = IO.binwrite(stream, Jason.encode!(value, pretty: true) <> "\n")
    :ok = :file.sync(stream)
    :ok = File.close(stream)
  end

  defp ensure!(true, _reason), do: :ok
  defp ensure!(_, reason), do: fail!(reason)
  defp fail!(reason), do: raise(CredentialedProviderWitness.Failure, code: reason)

  def safe_diagnostic(error, stacktrace) do
    exception_module =
      case error do
        %{__struct__: module} when is_atom(module) -> diagnostic_name(module)
        _ -> nil
      end

    top =
      case stacktrace do
        [{module, function, arity, location} | _]
        when is_atom(module) and is_atom(function) and is_list(location) ->
          argument_count =
            cond do
              is_integer(arity) and arity >= 0 and arity <= 255 -> arity
              is_list(arity) -> length(arity)
              true -> nil
            end

          file =
            case Keyword.get(location, :file) do
              value when is_binary(value) ->
                value |> Path.basename() |> diagnostic_file()

              value when is_list(value) ->
                value |> List.to_string() |> Path.basename() |> diagnostic_file()

              _ ->
                nil
            end

          line = Keyword.get(location, :line)

          %{
            stack_module: diagnostic_name(module),
            stack_function: diagnostic_name(function),
            stack_arity: argument_count,
            source_file: file,
            source_line:
              if(is_integer(line) and line > 0 and line <= 1_000_000, do: line, else: nil)
          }

        _ ->
          %{
            stack_module: nil,
            stack_function: nil,
            stack_arity: nil,
            source_file: nil,
            source_line: nil
          }
      end

    Map.put(top, :exception_module, exception_module)
  end

  defp diagnostic_name(value) do
    name = Atom.to_string(value)

    if byte_size(name) <= 160 and Regex.match?(~r/\A[A-Za-z0-9_.!?]+\z/, name),
      do: name,
      else: nil
  end

  defp diagnostic_file(name) do
    if byte_size(name) <= 200 and Regex.match?(~r/\A[A-Za-z0-9_.-]+\z/, name), do: name, else: nil
  end

  defp policy(id, actions),
    do: %{
      "automatic_execution" => false,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "max_run_attempts_per_task" => 1,
      "budget_limit_microusd" => nil,
      "per_run_cost_limit_microusd" => nil,
      "budget_mode" => "soft",
      "hard_cost_limit_required" => false,
      "allowed_runtime_ids" => [],
      "allowed_model_profiles" => [],
      "final_acceptance" => "operator",
      "allowed_actions" => actions,
      "allowed_resource_ids" => [id]
    }

  defp capabilities,
    do: %{
      "provider_access" => true,
      "structured_input" => true,
      "interactive" => true,
      "adapter" => %{
        "kind" => "codex",
        "native_version" => "0.153.4",
        "implementation_version" => "symmetry-codex-1",
        "protocol_version" => 1,
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
    }
end

defmodule CredentialedProviderWitness.HTTP do
  @root "/repos/wxxb789/symmetry-0006h-provider-fixture"

  def request(method, url, headers, body \\ nil) do
    uri = URI.parse(url)
    decoded_path = URI.decode(uri.path || "")

    safe_segments =
      not Enum.any?(String.split(decoded_path, "/"), &(&1 in [".", ".."])) and
        not String.contains?(decoded_path, ["\\", <<0>>])

    allowed_path =
      (method == :get and
         (uri.path == "/user" or uri.path == @root or
            Regex.match?(
              ~r"\A#{@root}/(?:git/ref/heads/[A-Za-z0-9_.%/-]+|git/commits/[0-9a-f]{40}|pulls(?:/[1-9][0-9]*)?)\z",
              uri.path || ""
            ))) or
        (method == :post and uri.path == @root <> "/pulls" and is_nil(uri.query)) or
        (method == :patch and Regex.match?(~r"\A#{@root}/pulls/[1-9][0-9]*\z", uri.path || "") and
           is_nil(uri.query))

    unless uri.scheme == "https" and uri.host == "api.github.com" and uri.port == 443 and
             is_nil(uri.userinfo) and is_nil(uri.fragment) and allowed_path and safe_segments do
      raise "provider_destination_not_allowlisted"
    end

    state = CredentialedProviderWitness.transport_context()

    if state.phase in ["recover", "finish"] and method != :get do
      send(state.controller, {:read_only_provider_write_blocked, self(), method})
      raise "provider_phase_read_only"
    end

    context = CredentialedProviderWitness.authorize_request!(method)
    if method != :get, do: authorize_write!(method, uri, body, context)
    result = SymmetryControl.Integrations.HTTP.request(method, url, headers, body)
    if method == :get, do: remember_targets(result, context)
    CredentialedProviderWitness.audit!(method, uri, result, body)
  end

  def authorize_write!(method, uri, body, %{"action_binding" => binding}) do
    digest = binding |> SymmetryControl.RequestHash.canonical() |> Base.encode16(case: :lower)
    marker = "<!-- symmetry-provider-action:v1:#{binding["action_id"]}:#{digest} -->"
    marked_body = if binding["body"] == "", do: marker, else: binding["body"] <> "\n\n" <> marker
    payload = %{"title" => binding["title"], "body" => marked_body}

    allowed =
      case method do
        :post ->
          binding["operation"] == "change.upsert" and uri.path == @root <> "/pulls" and
            body == Map.merge(payload, %{
              "head" => binding["target"]["source_branch"],
              "base" => binding["target"]["target_branch"]
            })

        :patch ->
          observed = Process.get({__MODULE__, :observed_targets}, %{})
          paths = Map.get(observed, binding["action_id"], MapSet.new())

          target_matches =
            case binding["operation"] do
              "change.upsert" -> true
              "change.update" ->
                uri.path == String.replace(binding["input"]["pull_request_url"],
                  "https://github.com/wxxb789/symmetry-0006h-provider-fixture/pull/",
                  @root <> "/pulls/")
              _ -> false
            end

          target_matches and MapSet.member?(paths, uri.path) and body == payload

        _ ->
          false
      end

    unless allowed, do: raise(CredentialedProviderWitness.Failure, code: "outbound_write_not_authorized")
    :ok
  end

  def authorize_write!(_method, _uri, _body, _context),
    do: raise(CredentialedProviderWitness.Failure, code: "outbound_write_not_authorized")

  def remember_targets({:ok, status, _, body}, %{"action_binding" => binding})
      when status in 200..299 do
    paths =
      List.wrap(body)
      |> Enum.filter(fn pr ->
        is_map(pr) and is_integer(pr["number"]) and pr["number"] > 0 and
          pr["html_url"] == "https://github.com/wxxb789/symmetry-0006h-provider-fixture/pull/#{pr["number"]}" and
          get_in(pr, ["head", "sha"]) == binding["subject"]["commit"] and
          get_in(pr, ["head", "ref"]) == binding["target"]["source_branch"] and
          get_in(pr, ["base", "ref"]) == binding["target"]["target_branch"] and
          Enum.all?(["head", "base"], fn side ->
            get_in(pr, [side, "repo", "id"]) == binding["repository"]["id"] and
              get_in(pr, [side, "repo", "node_id"]) == binding["repository"]["node_id"]
          end)
      end)
      |> MapSet.new(&(@root <> "/pulls/#{&1["number"]}"))

    observed = Process.get({__MODULE__, :observed_targets}, %{})
    Process.put({__MODULE__, :observed_targets}, Map.put(observed, binding["action_id"], paths))
    :ok
  end

  def remember_targets(_result, _context), do: :ok
end

try do
  case System.argv() do
    [phase, directory] -> CredentialedProviderWitness.run!(phase, directory)
    _ -> raise "invalid_witness_arguments"
  end
rescue
  error ->
    reason =
      case error do
        %{__struct__: CredentialedProviderWitness.Failure, code: code} when is_binary(code) ->
          if Regex.match?(~r/\A[a-z_]+\z/, code), do: code, else: "witness_phase_failed"

        _ ->
          "witness_phase_failed"
      end

    IO.puts(
      :stderr,
      Jason.encode!(%{
        status: "failed",
        reason: reason,
        diagnostic: CredentialedProviderWitness.safe_diagnostic(error, __STACKTRACE__)
      })
    )

    System.halt(1)
catch
  _, _ ->
    IO.puts(:stderr, "{\"status\":\"failed\",\"reason\":\"witness_phase_failed\"}")
    System.halt(1)
end
