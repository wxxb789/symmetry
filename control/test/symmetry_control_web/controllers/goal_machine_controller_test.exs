defmodule SymmetryControlWeb.GoalMachineControllerTest do
  use SymmetryControlWeb.ConnCase, async: false

  import Ecto.Query

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Orchestration.{Run, Runtime, Task}
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces

  @enrollment_token "test-enrollment-token"

  test "machine registration persists an optional runtime repository affinity", %{conn: conn} do
    {machine_id, token} = enroll(conn, "runtime-affinity")
    project = project_fixture()
    repository = repository_fixture(project)

    response =
      bearer(conn, token)
      |> put("/api/v1/machines/#{machine_id}/sessions/#{Ecto.UUID.generate()}", %{
        "runtimes" => [
          %{
            "runtime_key" => "affinity-runtime",
            "name" => "Affinity runtime",
            "capacity" => 1,
            "agent_profile" => "codex",
            "workspace" => "primary",
            "repository_resource_id" => repository.id,
            "capabilities" => %{}
          }
        ]
      })
      |> json_response(200)

    [registered] = response["runtimes"]
    runtime = Repo.get!(Runtime, registered["runtime_id"])
    assert runtime.repository_resource_id == repository.id

    response =
      bearer(conn, token)
      |> put("/api/v1/machines/#{machine_id}/sessions/#{Ecto.UUID.generate()}", %{
        "runtimes" => [
          %{
            "runtime_key" => "legacy-affinity-runtime",
            "name" => "Legacy affinity runtime",
            "capacity" => 1,
            "agent_profile" => "codex",
            "workspace" => "primary",
            "repository_resource_id" => nil,
            "capabilities" => %{}
          }
        ]
      })
      |> json_response(200)

    [legacy] = response["runtimes"]
    assert Repo.get!(Runtime, legacy["runtime_id"]).repository_resource_id == nil
  end

  test "the owning machine attaches an opaque harness session and rejects stale fences", %{
    conn: conn
  } do
    %{token: token, item: item, run: run, fence: fence} = claimed_goal_run_fixture(conn)
    request = Map.merge(fence, session_attrs(item))

    attached =
      bearer(conn, token)
      |> put("/api/v1/runs/#{run.id}/session", request)
      |> json_response(201)

    assert %{
             "session" => %{
               "run_id" => run_id,
               "state" => "busy",
               "active_run_id" => active_run_id,
               "local_handle_id" => local_handle_id,
               "harness_version" => "1.0.0",
               "adapter_version" => "1.0.0",
               "repository_resource_id" => repository_resource_id,
               "workspace" => "primary"
             }
           } = attached

    assert run_id == run.id
    assert active_run_id == run.id
    assert repository_resource_id == get_in(item, [:baseline, :subject, "resource_id"])
    assert is_binary(local_handle_id)
    refute inspect(attached) =~ "token"
    refute inspect(attached) =~ "transcript"

    assert ^attached =
             bearer(conn, token)
             |> put("/api/v1/runs/#{run.id}/session", request)
             |> json_response(200)

    assert_error(
      bearer(conn, token)
      |> put(
        "/api/v1/runs/#{run.id}/session",
        Map.put(request, :lease_token, Ecto.UUID.generate())
      ),
      409,
      "ownership_lost"
    )

    assert_error(
      bearer(conn, token)
      |> put(
        "/api/v1/runs/#{run.id}/session",
        Map.put(request, "machine_id", Ecto.UUID.generate())
      ),
      400,
      "invalid_request"
    )
  end

  test "resume session receipts preserve durable identity on replay", %{conn: conn} do
    %{token: token, item: item, run: run, fence: fence} = claimed_goal_run_fixture(conn)
    runtime = Repo.get!(Runtime, run.runtime_id)
    attrs = session_attrs(item)
    task = Repo.get!(Task, run.task_id)

    retained =
      %HarnessSession{}
      |> HarnessSession.changeset(%{
        machine_id: runtime.machine_id,
        runtime_id: runtime.id,
        repository_resource_id: get_in(item, [:baseline, :subject, "resource_id"]),
        local_handle_id: attrs.local_handle_id,
        harness_kind: attrs.harness_kind,
        harness_version: attrs.harness_version,
        adapter_version: attrs.adapter_version,
        workspace_fingerprint: attrs.workspace_fingerprint,
        state: "available"
      })
      |> Repo.insert!()

    Repo.update_all(
      from(task_row in Task, where: task_row.id == ^task.id),
      set: [
        input: Map.put(task.input, "session_mode", "resume"),
        requested_session_id: retained.id
      ]
    )

    request = Map.merge(fence, attrs)

    attached =
      bearer(conn, token)
      |> put("/api/v1/runs/#{run.id}/session", request)
      |> json_response(201)

    assert %{
             "session" => %{
               "id" => session_id,
               "run_id" => run_id,
               "active_run_id" => active_run_id,
               "runtime_id" => runtime_id,
               "machine_id" => machine_id,
               "repository_resource_id" => repository_resource_id,
               "local_handle_id" => local_handle_id,
               "harness_kind" => "codex",
               "harness_version" => "1.0.0",
               "adapter_version" => "1.0.0",
               "workspace_fingerprint" => "workspace-1",
               "workspace" => "primary",
               "state" => "busy"
             }
           } = attached

    assert session_id == retained.id
    assert run_id == run.id
    assert active_run_id == run.id
    assert runtime_id == runtime.id
    assert machine_id == runtime.machine_id
    assert repository_resource_id == get_in(item, [:baseline, :subject, "resource_id"])
    assert local_handle_id == attrs.local_handle_id
    refute inspect(attached) =~ "native_session"
    refute inspect(attached) =~ "credential"

    assert ^attached =
             bearer(conn, token)
             |> put("/api/v1/runs/#{run.id}/session", request)
             |> json_response(200)
  end

  test "handoff session attachment is visibly unsupported and never claims a retained session", %{
    conn: conn
  } do
    %{token: token, item: item, run: run, fence: fence} = claimed_goal_run_fixture(conn)
    task = Repo.get!(Task, run.task_id)
    attrs = session_attrs(item)

    Repo.update_all(
      from(task_row in Task, where: task_row.id == ^task.id),
      set: [input: Map.put(task.input, "session_mode", "handoff"), requested_session_id: nil]
    )

    session_count = Repo.aggregate(HarnessSession, :count)

    assert_error(
      bearer(conn, token)
      |> put("/api/v1/runs/#{run.id}/session", Map.merge(fence, attrs)),
      422,
      "unsupported_capability"
    )

    assert Repo.aggregate(HarnessSession, :count) == session_count
    assert Repo.get!(Run, run.id).harness_session_id == nil
  end

  test "evidence and usage use exact per-run replay identities", %{conn: conn} do
    %{token: token, run: run, fence: fence, subject: subject} = claimed_goal_run_fixture(conn)
    evidence = Map.merge(fence, evidence_attrs(run.id, subject))

    stored_evidence =
      bearer(conn, token)
      |> post("/api/v1/runs/#{run.id}/evidence", evidence)
      |> json_response(201)

    assert %{
             "evidence" => %{"run_id" => run_id, "evidence_key" => "observation:unit"}
           } =
             stored_evidence

    assert run_id == run.id

    assert ^stored_evidence =
             bearer(conn, token)
             |> post("/api/v1/runs/#{run.id}/evidence", evidence)
             |> json_response(200)

    assert_error(
      bearer(conn, token)
      |> post(
        "/api/v1/runs/#{run.id}/evidence",
        put_in(evidence, [:payload, :note], "changed observation")
      ),
      409,
      "idempotency_conflict"
    )

    usage = Map.merge(fence, usage_attrs(run.id))

    stored_usage =
      bearer(conn, token)
      |> post("/api/v1/runs/#{run.id}/usage", usage)
      |> json_response(201)

    assert %{
             "usage" => %{
               "run_id" => ^run_id,
               "usage_key" => "turn-1",
               "cost_microusd" => "42"
             }
           } = stored_usage

    assert is_binary(stored_usage["usage"]["cost_microusd"])

    assert ^stored_usage =
             bearer(conn, token)
             |> post("/api/v1/runs/#{run.id}/usage", usage)
             |> json_response(200)

    assert_error(
      bearer(conn, token)
      |> post("/api/v1/runs/#{run.id}/usage", Map.put(usage, :cost_microusd, "43")),
      409,
      "idempotency_conflict"
    )

    unknown_usage = %{
      usage
      | usage_id: Ecto.UUID.generate(),
        usage_key: "turn-unknown",
        cost_microusd: nil,
        cost_basis: "unknown"
    }

    assert %{
             "usage" => %{
               "run_id" => ^run_id,
               "usage_key" => "turn-unknown",
               "cost_microusd" => nil
             }
           } =
             bearer(conn, token)
             |> post("/api/v1/runs/#{run.id}/usage", Map.merge(fence, unknown_usage))
             |> json_response(201)
  end

  test "foreign machines cannot read or mutate a Goal run, and context is sanitized", %{
    conn: conn
  } do
    %{
      token: owner_token,
      goal_id: goal_id,
      item: item,
      run: run,
      fence: fence,
      subject: subject
    } = claimed_goal_run_fixture(conn)

    session = Map.merge(fence, session_attrs(item))

    assert %{"session" => _} =
             bearer(conn, owner_token)
             |> put("/api/v1/runs/#{run.id}/session", session)
             |> json_response(201)

    {_machine_id, foreign_token} = enroll(conn, "foreign")
    evidence = Map.merge(fence, evidence_attrs(run.id, subject))
    usage = Map.merge(fence, usage_attrs(run.id))
    context_path = "/api/v1/runs/#{run.id}/context?#{URI.encode_query(stringify_keys(fence))}"

    assert_error(
      bearer(conn, foreign_token) |> put("/api/v1/runs/#{run.id}/session", session),
      403,
      "forbidden"
    )

    assert_error(
      bearer(conn, foreign_token) |> post("/api/v1/runs/#{run.id}/evidence", evidence),
      403,
      "forbidden"
    )

    assert_error(
      bearer(conn, foreign_token) |> post("/api/v1/runs/#{run.id}/usage", usage),
      403,
      "forbidden"
    )

    assert_error(bearer(conn, foreign_token) |> get(context_path), 403, "forbidden")

    context =
      bearer(conn, owner_token)
      |> get(context_path)
      |> json_response(200)

    assert %{
             "run_id" => run_id,
             "context" => %{
               "schema_version" => "symmetry.context_snapshot.v1",
               "snapshot_id" => snapshot_id,
               "goal_id" => ^goal_id,
               "goal_revision" => 1,
               "work_item_id" => work_item_id,
               "content_hash" => "sha256:" <> _content_hash,
               "created_at" => created_at,
               "approved_goal" => approved_goal,
               "work_contract" => work_contract,
               "subject" => ^subject,
               "sources" => sources,
               "current_decisions" => current_decisions,
               "validated_evidence" => validated_evidence,
               "failed_attempts" => failed_attempts,
               "advisory_recall" => advisory_recall,
               "next_action" => next_action,
               "size" => %{
                 "mandatory_bytes" => mandatory_bytes,
                 "optional_bytes" => optional_bytes,
                 "total_bytes" => total_bytes,
                 "byte_budget" => byte_budget,
                 "token_estimate" => token_estimate
               }
             }
           } = context

    assert run_id == run.id
    assert is_binary(snapshot_id)
    assert is_binary(created_at)
    assert work_item_id == item.id
    assert is_map(approved_goal)
    assert is_map(work_contract)
    assert [_ | _] = sources
    assert is_list(current_decisions)
    assert is_list(validated_evidence)
    assert is_list(failed_attempts)
    assert is_list(advisory_recall)
    assert is_map(next_action) or is_nil(next_action)
    assert is_integer(mandatory_bytes) and mandatory_bytes >= 0
    assert is_integer(optional_bytes) and optional_bytes >= 0
    assert is_integer(total_bytes) and total_bytes >= mandatory_bytes
    assert is_integer(byte_budget) and byte_budget > 0
    assert is_integer(token_estimate) or is_nil(token_estimate)
    refute inspect(context) =~ "provider-secret"
    refute inspect(context) =~ "Bearer"
    refute inspect(context) =~ "transcript"

    stale_path =
      "/api/v1/runs/#{run.id}/context?#{URI.encode_query(stringify_keys(Map.put(fence, :lease_token, Ecto.UUID.generate())))}"

    assert_error(bearer(conn, owner_token) |> get(stale_path), 409, "ownership_lost")
  end

  defp claimed_goal_run_fixture(conn) do
    {machine_id, token} = enroll(conn, "owner")
    runtime = register_runtime(conn, machine_id, token)
    project = project_fixture()
    repository = repository_fixture(project)
    subject = subject(repository.id)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(repository.id), "operator:test")

    goal_id = created.goal.id
    proposal = plan_proposal(goal_id, repository.id)

    assert {:ok, decision, :created} =
             command_current(goal_id, "request_decision", %{
               kind: "plan",
               work_item_id: nil,
               subject_hash: nil,
               proposal: proposal
             })

    decision_id = decision.response["decision"]["id"]
    decision_version = Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).lock_version

    assert {:ok, _resolved, :created} =
             command_current(goal_id, "resolve_decision", %{
               decision_id: decision_id,
               expected_decision_version: decision_version,
               option_id: "accept"
             })

    assert {:ok, planned, :created} =
             command_current(
               goal_id,
               "accept_plan",
               %{
                 proposal: proposal,
                 proposal_hash:
                   "sha256:" <> Base.encode16(RequestHash.canonical(proposal), case: :lower),
                 decision_id: decision_id
               },
               rollout_enabled: true,
               validation_profiles: validation_profiles(runtime.id)
             )

    [item] = planned.goal.work_items

    Repo.update_all(
      from(runtime_row in Runtime, where: runtime_row.id == ^runtime.id),
      set: [repository_resource_id: repository.id]
    )

    runtime = Repo.get!(Runtime, runtime.id)

    assert {:ok, _active, :created} =
             command_current(goal_id, "activate", %{approved_revision: 1})

    assert {:ok, admitted, :created} =
             command_current(
               goal_id,
               "admit_task",
               %{
                 work_item_id: item.id,
                 purpose: "implement",
                 model_profile: "codex",
                 session_mode: "fresh",
                 requested_session_id: nil,
                 validation_of_task_id: nil
               },
               rollout_enabled: true
             )

    task = Repo.get!(Task, admitted.response["task"]["id"])

    assert %{
             "schema_version" => "symmetry.admission.v1",
             "admission_id" => _,
             "goal_id" => ^goal_id,
             "goal_revision" => 1,
             "work_item_id" => work_item_id,
             "context_snapshot_id" => _,
             "context_hash" => "sha256:" <> _,
             "model_profile" => "codex",
             "session_mode" => "fresh",
             "requested_session_id" => nil,
             "subject" => ^subject,
             "limits" => %{"max_turns" => 1, "deadline_at" => _},
             "validation_of_task_id" => nil
           } = task.input

    assert work_item_id == item.id
    now = DateTime.utc_now() |> DateTime.truncate(:microsecond)
    claim_id = Ecto.UUID.generate()
    lease_token = Ecto.UUID.generate()

    Repo.update_all(
      from(task_row in Task, where: task_row.id == ^task.id),
      set: [state: "running", current_generation: 1, updated_at: now]
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
        assigned_at: now,
        assignment_expires_at: DateTime.add(now, 60, :second),
        claimed_at: now,
        lease_expires_at: DateTime.add(now, 60, :second)
      })
      |> Repo.insert!()

    %{
      token: token,
      goal_id: goal_id,
      item: item,
      run: run,
      subject: subject,
      fence: %{
        runtime_id: runtime.id,
        runtime_epoch: runtime.connection_epoch,
        generation: 1,
        claim_id: claim_id,
        lease_token: lease_token
      }
    }
  end

  defp command_current(goal_id, kind, payload, opts \\ []) do
    assert {:ok, goal} = Goals.fetch_goal(goal_id)
    opts = Keyword.put_new(opts, :validation_profiles, validation_profiles())

    Goals.command(
      goal_id,
      %{
        schema_version: "symmetry.goal_command.v1",
        mutation_id: Ecto.UUID.generate(),
        expected_version: goal.version,
        expected_revision: goal.current_revision,
        kind: kind,
        payload: payload
      },
      "operator:test",
      opts
    )
  end

  defp validation_profiles(runtime_id \\ Ecto.UUID.generate()) do
    [
      test: [
        kind: :check,
        profile_digest: "sha256:" <> String.duplicate("c", 64),
        enabled: true,
        allowed_runtime_ids: [runtime_id]
      ]
    ]
  end

  defp goal_attrs(repository_id) do
    %{
      schema_version: "symmetry.goal_create.v1",
      title: "Machine endpoint Goal #{System.unique_integer([:positive])}",
      mutation_id: Ecto.UUID.generate(),
      initial_revision: %{
        objective: "Persist machine-native receipts behind the claimed fence.",
        non_goals: [],
        acceptance_contract: check_contract(),
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
          "max_run_attempts_per_task" => 2,
          "budget_limit_microusd" => nil,
          "per_run_cost_limit_microusd" => nil,
          "budget_mode" => "soft",
          "hard_cost_limit_required" => false,
          "allowed_runtime_ids" => [],
          "allowed_resource_ids" => [repository_id],
          "allowed_model_profiles" => ["codex"],
          "final_acceptance" => "operator",
          "allowed_actions" => []
        },
        context_manifest: %{
          "byte_budget" => 32_768,
          "required_source_kinds" => ["approved_goal", "work_contract", "repository_subject"],
          "include_advisory_recall" => false
        },
        reason: "Initial Goal contract"
      }
    }
  end

  defp plan_proposal(goal_id, repository_id) do
    %{
      schema_version: "symmetry.plan.v1",
      proposal_id: Ecto.UUID.generate(),
      goal_id: goal_id,
      expected_revision: 1,
      items: [
        %{
          key: "implement",
          title: "Implement",
          description: "Bounded Goal work",
          required: true,
          repository_resource_id: repository_id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: %{kind: "subject", subject: subject(repository_id)}
        }
      ]
    }
  end

  defp check_contract do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "An independent check must pass.",
      "predicates" => [%{"id" => "check", "kind" => "check", "validator_profile" => "test"}]
    }
  end

  defp session_attrs(item) do
    repository_resource_id =
      Map.get(item, :repository_resource_id) ||
        get_in(item, [:baseline, :subject, "resource_id"])

    %{
      local_handle_id: Ecto.UUID.generate(),
      harness_kind: "codex",
      harness_version: "1.0.0",
      adapter_version: "1.0.0",
      workspace_fingerprint: "workspace-1",
      workspace: "primary",
      repository_resource_id: repository_resource_id
    }
  end

  defp evidence_attrs(run_id, subject) do
    subject_hash = "sha256:" <> Base.encode16(RequestHash.canonical(subject), case: :lower)

    %{
      schema_version: "symmetry.evidence.v1",
      evidence_id: Ecto.UUID.generate(),
      run_id: run_id,
      evidence_key: "observation:unit",
      kind: "observation",
      subject: subject,
      subject_hash: subject_hash,
      source_ref: %{
        kind: "observation",
        ref: "unit-observation",
        external_ref: "unit-tests",
        subject_hash: subject_hash
      },
      source_revision: "observation-v1",
      validator_profile: nil,
      verdict: "passed",
      payload: %{
        predicate_id: "observation",
        subject: subject,
        subject_hash: subject_hash,
        external_ref: "unit-tests",
        observed_at: "2026-09-09T00:00:00.000000Z",
        note: "unit observation"
      },
      observed_at: "2026-09-09T00:00:00.000000Z"
    }
  end

  defp subject(repository_id) do
    %{
      "resource_id" => repository_id,
      "commit" => String.duplicate("a", 40),
      "tree_digest" => "sha256:" <> String.duplicate("4", 64)
    }
  end

  defp usage_attrs(run_id) do
    %{
      schema_version: "symmetry.usage.v1",
      usage_id: Ecto.UUID.generate(),
      run_id: run_id,
      usage_key: "turn-1",
      provider: "codex",
      model: "codex-test",
      input_tokens: 10,
      output_tokens: 20,
      cached_input_tokens: 0,
      cost_microusd: "42",
      cost_basis: "reported",
      price_version: nil,
      supersedes_id: nil,
      observed_at: "2026-09-09T00:00:00.000000Z"
    }
  end

  defp project_fixture do
    {:ok, project} =
      Workspaces.create_project(%{
        name: "Machine Goal #{System.unique_integer([:positive])}",
        key: "M#{System.unique_integer([:positive])}",
        default_agent_profile: "codex",
        default_workspace: "primary"
      })

    project
  end

  defp repository_fixture(project) do
    {:ok, repository} =
      Workspaces.create_resource(project.id, %{
        kind: "repository",
        name: "Repository #{System.unique_integer([:positive])}"
      })

    repository
  end

  defp enroll(conn, name) do
    token = "goal-machine-#{Ecto.UUID.generate()}"

    response =
      bearer(conn, @enrollment_token)
      |> put_req_header("idempotency-key", Ecto.UUID.generate())
      |> post("/api/v1/machines", %{"machine" => %{"name" => name}, "machine_token" => token})
      |> json_response(201)

    {response["machine_id"], response["machine_token"]}
  end

  defp register_runtime(conn, machine_id, token) do
    response =
      bearer(conn, token)
      |> put("/api/v1/machines/#{machine_id}/sessions/#{Ecto.UUID.generate()}", %{
        "runtimes" => [
          %{
            "runtime_key" => "goal-runtime-#{System.unique_integer([:positive])}",
            "name" => "Goal runtime",
            "capacity" => 1,
            "agent_profile" => "codex",
            "workspace" => "primary",
            "capabilities" => %{}
          }
        ]
      })
      |> json_response(200)

    [runtime] = response["runtimes"]

    Repo.update_all(
      from(runtime_row in Runtime, where: runtime_row.id == ^runtime["runtime_id"]),
      set: [
        harness_kind: "codex",
        harness_version: "1.0.0",
        adapter_version: "1.0.0",
        adapter_protocol_version: 1
      ]
    )

    Repo.get!(Runtime, runtime["runtime_id"])
  end

  defp stringify_keys(map), do: Map.new(map, fn {key, value} -> {to_string(key), value} end)
  defp bearer(conn, token), do: put_req_header(conn, "authorization", "Bearer " <> token)

  defp assert_error(conn, status, code) do
    assert %{"error" => %{"code" => ^code}} = json_response(conn, status)
  end
end
