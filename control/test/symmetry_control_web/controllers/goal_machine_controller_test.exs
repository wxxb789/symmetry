defmodule SymmetryControlWeb.GoalMachineControllerTest do
  use SymmetryControlWeb.ConnCase, async: false

  import Ecto.Query

  alias SymmetryControl.Goals.ContractValidation
  alias SymmetryControl.Goals

  alias SymmetryControl.Goals.{
    Goal,
    HarnessSession,
    HarnessSessionAttachReceipt,
    HarnessSessionStopReceipt
  }

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Machine, Run, Runtime, Task}
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces

  @enrollment_token "test-enrollment-token"
  @goal_receipt_listener_id :goal_receipt_e2e_listener
  @contracts_root Path.expand("../../../../contracts", __DIR__)
  @contracts_schema_root Path.expand("../../../../contracts/v1", __DIR__)

  setup context do
    if context[:goal_receipt_e2e] == true and System.get_env("SYMMETRY_GOAL_RECEIPT_E2E") == "1" do
      assert_goal_receipt_database!()
      daemon_dir = Path.expand("../../../../daemon", __DIR__)

      build_dir =
        Path.join(System.tmp_dir!(), "symmetry-goal-receipt-e2e-#{Ecto.UUID.generate()}")

      File.mkdir!(build_dir)
      on_exit(fn -> File.rm_rf!(build_dir) end)
      File.chmod!(build_dir, 0o700)

      driver = build_goal_receipt_driver!(daemon_dir, build_dir)

      {:ok, daemon_dir: daemon_dir, driver: driver, build_dir: build_dir}
    else
      :ok
    end
  end

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
    refute Map.has_key?(request, :binding_id)

    assert_error(
      bearer(conn, token)
      |> put(
        "/api/v1/runs/#{run.id}/session",
        Map.put(request, :binding_id, Ecto.UUID.generate())
      ),
      400,
      "invalid_request"
    )

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
    assert is_binary(attached["session"]["binding_id"])
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

  test "an owning machine reads the immutable attach receipt after terminal settlement", %{
    conn: conn
  } do
    %{token: token, item: item, run: run, fence: fence} = claimed_goal_run_fixture(conn)
    request = Map.merge(fence, session_attrs(item))

    attached =
      bearer(conn, token)
      |> put("/api/v1/runs/#{run.id}/session", request)
      |> json_response(201)

    Repo.update_all(from(row in Task, where: row.id == ^run.task_id), set: [state: "completed"])
    Repo.update_all(from(row in Run, where: row.id == ^run.id), set: [state: "completed"])

    assert {:ok, %{"settlement" => "missing_result"}} =
             Goals.settle_task(run.task_id, run.id, run.generation)

    assert %{state: "unavailable", active_run_id: nil} =
             Repo.get!(HarnessSession, attached["session"]["id"])

    receipt_path = "/api/v1/runs/#{run.id}/session?#{URI.encode_query(stringify_keys(fence))}"

    assert ^attached =
             bearer(conn, token)
             |> get(receipt_path)
             |> json_response(200)

    assert attached["session"]["state"] == "busy"
    assert attached["session"]["active_run_id"] == run.id

    stale_path =
      "/api/v1/runs/#{run.id}/session?#{URI.encode_query(stringify_keys(Map.put(fence, :lease_token, Ecto.UUID.generate())))}"

    assert_error(bearer(conn, token) |> get(stale_path), 409, "ownership_lost")
  end

  test "resume session receipts preserve durable identity on replay", %{conn: conn} do
    %{
      token: token,
      item: item,
      run: run,
      fence: fence,
      retained_session: retained,
      session_attrs: attrs
    } = claimed_goal_run_fixture(conn, session_mode: "resume")

    runtime = Repo.get!(Runtime, run.runtime_id)

    request = Map.merge(fence, attrs)

    assert_error(
      bearer(conn, token)
      |> put("/api/v1/runs/#{run.id}/session", Map.delete(request, :binding_id)),
      400,
      "invalid_request"
    )

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

  test "the owning machine releases a settled session through an exact stop receipt", %{
    conn: conn
  } do
    %{token: token, item: item, run: run, fence: fence} = claimed_goal_run_fixture(conn)
    attached_request = Map.merge(fence, session_attrs(item))

    attached =
      bearer(conn, token)
      |> put("/api/v1/runs/#{run.id}/session", attached_request)
      |> json_response(201)

    stop_request =
      Map.merge(fence, %{
        "session_id" => attached["session"]["id"],
        "local_handle_id" => attached["session"]["local_handle_id"],
        "binding_id" => attached["session"]["binding_id"]
      })

    assert_error(
      bearer(conn, token) |> put("/api/v1/runs/#{run.id}/session/stopped", stop_request),
      409,
      "state_conflict"
    )

    Repo.update_all(from(row in Task, where: row.id == ^run.task_id), set: [state: "completed"])
    Repo.update_all(from(row in Run, where: row.id == ^run.id), set: [state: "completed"])

    assert {:ok, %{"settlement" => "missing_result"}} =
             Goals.settle_task(run.task_id, run.id, run.generation)

    stopped =
      bearer(conn, token)
      |> put("/api/v1/runs/#{run.id}/session/stopped", stop_request)
      |> json_response(201)

    assert %{
             "session_stopped" => %{
               "receipt_id" => receipt_id,
               "run_id" => run_id,
               "session_id" => session_id,
               "local_handle_id" => local_handle_id,
               "binding_id" => binding_id,
               "state" => "available",
               "active_run_id" => nil
             }
           } = stopped

    assert receipt_id
    assert run_id == run.id
    assert session_id == attached["session"]["id"]
    assert local_handle_id == attached["session"]["local_handle_id"]
    assert binding_id == attached["session"]["binding_id"]

    assert ^stopped =
             bearer(conn, token)
             |> put("/api/v1/runs/#{run.id}/session/stopped", stop_request)
             |> json_response(200)

    assert_error(
      bearer(conn, token)
      |> put(
        "/api/v1/runs/#{run.id}/session/stopped",
        Map.put(stop_request, "local_handle_id", Ecto.UUID.generate())
      ),
      409,
      "idempotency_conflict"
    )

    assert_error(
      bearer(conn, token)
      |> put(
        "/api/v1/runs/#{run.id}/session/stopped",
        Map.put(stop_request, "machine_id", Ecto.UUID.generate())
      ),
      400,
      "invalid_request"
    )
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

  test "evidence batches preserve request order, replay item receipts, and rollback conflicts", %{
    conn: conn
  } do
    %{token: token, run: run, fence: fence, subject: subject} = claimed_goal_run_fixture(conn)
    first = evidence_attrs(run.id, subject)

    second =
      first
      |> Map.put(:evidence_id, Ecto.UUID.generate())
      |> Map.put(:evidence_key, "observation:remote")
      |> put_in([:source_ref, :ref], "remote-observation")
      |> put_in([:source_ref, :external_ref], "status:remote")
      |> put_in([:payload, :external_ref], "status:remote")
      |> put_in([:payload, :note], "remote observation")

    batch = %{
      schema_version: "symmetry.evidence_batch.v1",
      run_id: run.id,
      items: [first, second]
    }

    created =
      bearer(conn, token)
      |> post("/api/v1/runs/#{run.id}/evidence", Map.merge(fence, batch))
      |> json_response(201)

    assert %{
             "evidence_batch" => %{
               "run_id" => batch_run_id,
               "receipts" => [
                 %{
                   "evidence_key" => "observation:unit",
                   "disposition" => "created"
                 },
                 %{
                   "evidence_key" => "observation:remote",
                   "disposition" => "created"
                 }
               ]
             }
           } = created

    assert batch_run_id == run.id

    assert :ok ==
             ContractValidation.validate_evidence_batch_response(
               created,
               schema_root: @contracts_schema_root
             )

    replayed =
      bearer(conn, token)
      |> post("/api/v1/runs/#{run.id}/evidence", Map.merge(fence, batch))
      |> json_response(200)

    assert get_in(replayed, ["evidence_batch", "run_id"]) == run.id

    assert Enum.map(replayed["evidence_batch"]["receipts"], & &1["evidence_key"]) == [
             "observation:unit",
             "observation:remote"
           ]

    assert Enum.all?(replayed["evidence_batch"]["receipts"], &(&1["disposition"] == "replayed"))

    third =
      second
      |> Map.put(:evidence_id, Ecto.UUID.generate())
      |> Map.put(:evidence_key, "observation:third")
      |> put_in([:source_ref, :ref], "third-observation")
      |> put_in([:source_ref, :external_ref], "status:third")
      |> put_in([:payload, :external_ref], "status:third")
      |> put_in([:payload, :note], "third observation")

    conflict = put_in(first, [:payload, :note], "changed observation")
    count_before = Repo.aggregate(SymmetryControl.Goals.RunEvidence, :count)

    conflict_response =
      bearer(conn, token)
      |> post(
        "/api/v1/runs/#{run.id}/evidence",
        Map.merge(fence, %{batch | items: [third, conflict]})
      )
      |> json_response(409)

    assert %{
             "error" => %{
               "code" => "idempotency_conflict",
               "details" => %{
                 "items" => [
                   %{
                     "index" => 1,
                     "evidence_key" => "observation:unit",
                     "disposition" => "conflict"
                   }
                 ]
               }
             }
           } = conflict_response

    assert :ok ==
             ContractValidation.validate_evidence_batch_conflict_details(
               conflict_response["error"]["details"],
               schema_root: @contracts_schema_root
             )

    assert Repo.aggregate(SymmetryControl.Goals.RunEvidence, :count) == count_before
  end

  test "raw duplicate evidence JSON is rejected after ownership without a write", %{conn: conn} do
    %{token: owner_token, run: run, fence: fence, subject: subject} =
      claimed_goal_run_fixture(conn)

    evidence = evidence_attrs(run.id, subject)
    body = Jason.encode!(Map.merge(fence, evidence))

    duplicate_body =
      String.replace(
        body,
        ~s("schema_version":"symmetry.evidence.v1"),
        ~s("schema_version":"symmetry.evidence.v1","schema_version":"symmetry.evidence.v1"),
        global: false
      )

    count_before = Repo.aggregate(SymmetryControl.Goals.RunEvidence, :count)

    {_foreign_machine_id, foreign_token} = enroll(conn, "foreign-raw")

    assert_error(
      bearer(conn, foreign_token)
      |> put_req_header("content-type", "application/json")
      |> post("/api/v1/runs/#{run.id}/evidence", duplicate_body),
      403,
      "forbidden"
    )

    assert_error(
      bearer(conn, owner_token)
      |> put_req_header("content-type", "application/json")
      |> post("/api/v1/runs/#{run.id}/evidence", duplicate_body),
      400,
      "invalid_request"
    )

    assert Repo.aggregate(SymmetryControl.Goals.RunEvidence, :count) == count_before
  end

  test "batch response validation blocks noncanonical success and conflict responses", %{
    conn: conn
  } do
    %{token: token, run: run, fence: fence, subject: subject} = claimed_goal_run_fixture(conn)
    evidence = evidence_attrs(run.id, subject)

    batch = %{
      schema_version: "symmetry.evidence_batch.v1",
      run_id: run.id,
      items: [evidence]
    }

    temporary_root =
      Path.join(System.tmp_dir!(), "symmetry-invalid-batch-response-#{Ecto.UUID.generate()}")

    File.cp_r!(@contracts_root, temporary_root)

    on_exit(fn -> File.rm_rf!(temporary_root) end)

    previous_contracts = Application.fetch_env!(:symmetry_control, :contracts)

    on_exit(fn -> Application.put_env(:symmetry_control, :contracts, previous_contracts) end)

    for filename <- [
          "evidence-batch-response.schema.json",
          "evidence-batch-conflict-details.schema.json"
        ] do
      path = Path.join([temporary_root, "v1", filename])

      schema =
        path
        |> File.read!()
        |> Jason.decode!()
        |> Map.put("required", ["noncanonical_response"])

      File.write!(path, Jason.encode!(schema))
    end

    Application.put_env(:symmetry_control, :contracts, directory: temporary_root)

    assert_error(
      bearer(conn, token) |> post("/api/v1/runs/#{run.id}/evidence", Map.merge(fence, batch)),
      400,
      "invalid_request"
    )

    conflict = put_in(evidence, [:payload, :note], "changed after response validation")

    assert_error(
      bearer(conn, token)
      |> post(
        "/api/v1/runs/#{run.id}/evidence",
        Map.merge(fence, %{batch | items: [conflict]})
      ),
      400,
      "invalid_request"
    )
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

  # This is a protocol fixture for committed HTTP acknowledgements. It does not
  # prove native-process, scheduler, claim, or daemon-journal restart recovery.
  @tag :goal_receipt_e2e
  @tag timeout: 180_000
  @tag skip: System.get_env("SYMMETRY_GOAL_RECEIPT_E2E") != "1"
  test "committed harness session receipts recover through real Go HTTP after listener restarts",
       %{
         conn: conn,
         daemon_dir: daemon_dir,
         driver: driver,
         build_dir: build_dir
       } do
    assert_goal_receipt_database!()
    unboxed_e2e_run!(fn -> assert_goal_receipt_tables_empty!() end)

    fixture = unboxed_e2e_run!(fn -> claimed_goal_run_fixture(conn) end)
    attrs = session_attrs(fixture.item)
    attach = fixture.fence |> Map.merge(attrs) |> stringify_keys()

    listener = start_goal_receipt_listener!()

    phase1 =
      run_goal_receipt_driver!(
        driver,
        daemon_dir,
        build_dir,
        listener.url,
        fixture.token,
        fixture.run.id,
        attach,
        "attach_lost_ack",
        nil,
        nil
      )

    assert_goal_receipt_output!(phase1, "attach_lost_ack", 1, nil, nil)

    attach_snapshot =
      unboxed_e2e_run!(fn -> observe_attach_receipt!(fixture) end)

    unless attach_snapshot.session_row.state == "busy" and
             attach_snapshot.session_row.active_run_id == fixture.run.id do
      flunk("attached session is not busy for its active run")
    end

    listener = restart_goal_receipt_listener!(listener)

    phase2 =
      run_goal_receipt_driver!(
        driver,
        daemon_dir,
        build_dir,
        listener.url,
        fixture.token,
        fixture.run.id,
        attach,
        "attach_recovery",
        attach_snapshot.session,
        nil
      )

    assert_goal_receipt_output!(
      phase2,
      "attach_recovery",
      0,
      attach_snapshot.session,
      nil
    )

    attach_after_recovery =
      unboxed_e2e_run!(fn -> observe_attach_receipt!(fixture) end)

    assert_attach_receipt_unchanged!(attach_snapshot, attach_after_recovery)
    assert_goal_receipt_session_unchanged!(attach_snapshot, attach_after_recovery)

    terminal =
      unboxed_e2e_run!(fn ->
        assert {:ok, _terminal_run} =
                 Orchestration.transition(
                   fixture.run.id,
                   fixture.fence,
                   "failed",
                   %{"stage" => "receipt_e2e"},
                   Ecto.UUID.generate()
                 )

        assert {:ok, %{"settlement" => "failed"}} =
                 Goals.settle_task(fixture.run.task_id, fixture.run.id, fixture.run.generation)

        run = Repo.get!(Run, fixture.run.id)
        task = Repo.get!(Task, fixture.run.task_id)
        session = Repo.get!(HarnessSession, run.harness_session_id)
        stop_receipt_count = Repo.aggregate(HarnessSessionStopReceipt, :count)

        unless run.state == "failed" and task.state == "failed" and session.state == "unavailable" and
                 is_nil(session.active_run_id) and stop_receipt_count == 0 do
          flunk("terminal transition did not preserve the unavailable session barrier")
        end

        %{run: run, session: session}
      end)

    phase3 =
      run_goal_receipt_driver!(
        driver,
        daemon_dir,
        build_dir,
        listener.url,
        fixture.token,
        fixture.run.id,
        attach,
        "stop_lost_ack",
        attach_snapshot.session,
        nil
      )

    assert_goal_receipt_output!(phase3, "stop_lost_ack", 1, nil, nil)

    stop_snapshot =
      unboxed_e2e_run!(fn ->
        observe_stop_receipt!(fixture, attach_snapshot, terminal.session)
      end)

    listener = restart_goal_receipt_listener!(listener)

    phase4 =
      run_goal_receipt_driver!(
        driver,
        daemon_dir,
        build_dir,
        listener.url,
        fixture.token,
        fixture.run.id,
        attach,
        "stop_recovery",
        attach_snapshot.session,
        stop_snapshot.stopped
      )

    assert_goal_receipt_output!(
      phase4,
      "stop_recovery",
      0,
      attach_snapshot.session,
      stop_snapshot.stopped
    )

    final =
      unboxed_e2e_run!(fn ->
        attach_final = observe_attach_receipt!(fixture)
        stop_final = observe_stop_receipt!(fixture, attach_snapshot, terminal.session)
        %{attach: attach_final, stop: stop_final}
      end)

    assert_attach_receipt_unchanged!(attach_snapshot, final.attach)
    assert_stop_receipt_unchanged!(stop_snapshot, final.stop)

    unless final.stop.session.lock_version == stop_snapshot.session.lock_version and
             final.stop.session.state == "available" and
             is_nil(final.stop.session.active_run_id) do
      flunk("session lock version or availability changed during stop recovery")
    end

    assert Process.alive?(listener.pid)
  end

  defp claimed_goal_run_fixture(conn, opts \\ []) do
    {machine_id, token} = enroll(conn, "owner")
    runtime = register_runtime(conn, machine_id, token)
    project = project_fixture()
    repository = repository_fixture(project)
    subject = subject(repository.id)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(repository.id), "operator:test",
               validation_profiles: validation_profiles(runtime.id)
             )

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

    assert {:ok, _planned, :created} =
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

    assert {:ok, %{work_items: [item]}} = Goals.fetch_goal(goal_id)

    Repo.update_all(
      from(runtime_row in Runtime, where: runtime_row.id == ^runtime.id),
      set: [repository_resource_id: repository.id]
    )

    runtime = Repo.get!(Runtime, runtime.id)

    {runtime, session_mode, retained_session, resume_attrs} =
      case Keyword.get(opts, :session_mode, "fresh") do
        "resume" ->
          Repo.update_all(
            from(runtime_row in Runtime, where: runtime_row.id == ^runtime.id),
            set: [capabilities: %{"adapter" => %{"operations" => %{"resume" => true}}}]
          )

          runtime = Repo.get!(Runtime, runtime.id)
          attrs = session_attrs(item)

          retained =
            %HarnessSession{}
            |> HarnessSession.changeset(%{
              machine_id: runtime.machine_id,
              runtime_id: runtime.id,
              repository_resource_id: get_in(item, [:baseline, :subject, "resource_id"]),
              local_handle_id: attrs.local_handle_id,
              binding_id: Ecto.UUID.generate(),
              binding_verified: true,
              harness_kind: attrs.harness_kind,
              harness_version: attrs.harness_version,
              adapter_version: attrs.adapter_version,
              workspace_fingerprint: attrs.workspace_fingerprint,
              state: "available"
            })
            |> Repo.insert!()

          {runtime, "resume", retained, attrs}

        "fresh" ->
          {runtime, "fresh", nil, nil}
      end

    requested_session_id = if(retained_session, do: retained_session.id, else: nil)

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
                 session_mode: session_mode,
                 requested_session_id: requested_session_id,
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
             "session_mode" => ^session_mode,
             "requested_session_id" => ^requested_session_id,
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

    {run, resume_attrs} =
      if retained_session do
        binding_id = Ecto.UUID.generate()

        retained_session
        |> HarnessSession.update_changeset(%{
          state: "busy",
          active_run_id: run.id,
          binding_id: binding_id
        })
        |> Repo.update!()

        run =
          run
          |> Ecto.Changeset.change(
            harness_session_id: retained_session.id,
            harness_binding_id: binding_id
          )
          |> Repo.update!()

        {run, Map.put(resume_attrs, :binding_id, binding_id)}
      else
        {run, resume_attrs}
      end

    %{
      token: token,
      goal_id: goal_id,
      item: item,
      run: run,
      subject: subject,
      retained_session: retained_session,
      session_attrs: resume_attrs,
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
          integration: true,
          model_profile: "codex",
          change_target: nil,
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

  defp assert_goal_receipt_database! do
    configured_database = Keyword.get(Repo.config(), :database)
    environment_database = System.get_env("POSTGRES_DB")

    valid? =
      is_binary(environment_database) and configured_database == environment_database and
        Regex.match?(~r/^symmetry_receipt_e2e_[0-9a-f]{32}$/, environment_database)

    unless valid?,
      do: flunk("goal receipt E2E requires its dedicated ephemeral PostgreSQL database")
  end

  defp assert_goal_receipt_tables_empty! do
    for {name, schema} <- [
          {"projects", Workspaces.Project},
          {"project_resources", Workspaces.ProjectResource},
          {"work_items", Workspaces.WorkItem},
          {"goals", Goal},
          {"machines", Machine},
          {"runtimes", Runtime},
          {"tasks", Task},
          {"runs", Run},
          {"harness_sessions", HarnessSession},
          {"harness_session_attach_receipts", HarnessSessionAttachReceipt},
          {"harness_session_stop_receipts", HarnessSessionStopReceipt}
        ] do
      unless Repo.aggregate(schema, :count) == 0,
        do: flunk("goal receipt E2E requires an empty #{name} table")
    end
  end

  defp unboxed_e2e_run!(fun) when is_function(fun, 0) do
    Ecto.Adapters.SQL.Sandbox.unboxed_run(Repo, fn ->
      first_txid = current_txid!()
      second_txid = current_txid!()

      unless first_txid != second_txid,
        do: flunk("goal receipt E2E requires separate autocommit connections")

      fun.()
    end)
  end

  defp current_txid! do
    %{rows: [[txid]]} = Repo.query!("SELECT txid_current()")
    txid
  end

  defp start_goal_receipt_listener! do
    listener =
      start_supervised!(
        {Bandit,
         plug: {&goal_receipt_endpoint_plug/2, []},
         scheme: :http,
         ip: {127, 0, 0, 1},
         port: 0,
         startup_log: false},
        id: @goal_receipt_listener_id
      )

    {:ok, {{127, 0, 0, 1}, port}} = ThousandIsland.listener_info(listener)

    unless is_integer(port) and port > 0,
      do: flunk("goal receipt E2E listener did not expose a port")

    %{pid: listener, port: port, url: "http://127.0.0.1:#{port}/api"}
  end

  defp restart_goal_receipt_listener!(listener) do
    stop_goal_receipt_listener!(listener)
    restarted = start_goal_receipt_listener!()

    unless restarted.pid != listener.pid and restarted.port > 0,
      do: flunk("goal receipt E2E listener restart did not create a new listener")

    restarted
  end

  defp stop_goal_receipt_listener!(%{pid: listener}) do
    stop_supervised!(@goal_receipt_listener_id)
    refute Process.alive?(listener)
    :ok
  end

  defp goal_receipt_endpoint_plug(conn, _opts) do
    unboxed_e2e_run!(fn ->
      SymmetryControlWeb.Endpoint.call(conn, SymmetryControlWeb.Endpoint.init([]))
    end)
  end

  defp build_goal_receipt_driver!(daemon_dir, build_dir) do
    extension = if match?({:win32, _}, :os.type()), do: ".exe", else: ""
    path = Path.join(build_dir, "symmetry-goal-receipt-driver#{extension}")

    {_output, status} =
      System.cmd("go", ["test", "-c", "-o", path, "./internal/control"],
        cd: daemon_dir,
        stderr_to_stdout: true
      )

    unless status == 0,
      do: raise("go test -c ./internal/control failed with status #{status}")

    path
  end

  defp run_goal_receipt_driver!(
         driver,
         daemon_dir,
         build_dir,
         url,
         machine_token,
         run_id,
         attach,
         phase,
         expected_attachment,
         expected_stop
       ) do
    input_path = Path.join(build_dir, "#{phase}-input.json")
    output_path = Path.join(build_dir, "#{phase}-output.json")

    File.write!(
      input_path,
      Jason.encode!(%{
        url: url,
        machine_token: machine_token,
        run_id: run_id,
        attach: attach,
        phase: phase,
        expected_attachment: expected_attachment,
        expected_stop: expected_stop,
        output_path: output_path
      }),
      [:exclusive]
    )

    File.chmod!(input_path, 0o600)
    File.rm(output_path)

    {_output, status} =
      System.cmd(
        driver,
        ["-test.run=^TestGoalSessionReceiptHTTP$", "-test.timeout=30s"],
        cd: daemon_dir,
        env: [
          {"SYMMETRY_GOAL_RECEIPT_E2E", "1"},
          {"SYMMETRY_GOAL_RECEIPT_E2E_INPUT", input_path}
        ],
        stderr_to_stdout: true
      )

    unless status == 0,
      do: flunk("Go receipt protocol driver failed in #{phase} phase")

    case Jason.decode(File.read!(output_path)) do
      {:ok, output} when is_map(output) -> output
      _ -> flunk("Go receipt protocol driver did not write a valid output object")
    end
  end

  defp assert_goal_receipt_output!(output, phase, dropped, expected_attachment, expected_stop) do
    unless output["phase"] == phase, do: flunk("receipt protocol output phase mismatch")

    unless output["dropped_responses"] == dropped,
      do: flunk("receipt protocol drop count mismatch")

    unless output["attachment"] == expected_attachment,
      do: flunk("receipt protocol attachment output mismatch")

    unless output["stopped"] == expected_stop,
      do: flunk("receipt protocol stop output mismatch")

    unless is_list(output["requests"]), do: flunk("receipt protocol request observations missing")
  end

  defp observe_attach_receipt!(fixture) do
    run = Repo.get!(Run, fixture.run.id)
    runtime = Repo.get!(Runtime, run.runtime_id)
    attach_receipt_count = Repo.aggregate(HarnessSessionAttachReceipt, :count)
    session_count = Repo.aggregate(HarnessSession, :count)

    unless attach_receipt_count == 1 and session_count == 1,
      do: flunk("expected exactly one attach receipt and harness session")

    receipt =
      Repo.one!(
        from row in HarnessSessionAttachReceipt,
          where: row.run_id == ^run.id
      )

    session = Repo.get!(HarnessSession, receipt.session_id)
    session_response = get_in(receipt.response, ["session"])

    unless receipt.run_id == run.id and receipt.session_id == session.id and
             receipt.runtime_id == fixture.fence.runtime_id and
             receipt.machine_id == runtime.machine_id and
             receipt.runtime_epoch == fixture.fence.runtime_epoch and
             receipt.generation == fixture.fence.generation and
             receipt.claim_id == fixture.fence.claim_id and
             receipt.lease_token == fixture.fence.lease_token and
             run.harness_session_id == session.id and
             run.harness_binding_id == session.binding_id do
      flunk("attach receipt fence or run/session binding pairing mismatch")
    end

    unless is_binary(receipt.request_hash) and byte_size(receipt.request_hash) == 32,
      do: flunk("attach receipt request hash is not a 32-byte digest")

    unless is_map(receipt.response) and is_map(session_response),
      do: flunk("attach receipt response is not a complete object")

    unless session_response["id"] == session.id and
             session_response["session_id"] == session.id and
             session_response["run_id"] == run.id and
             session_response["active_run_id"] == run.id and
             session_response["binding_id"] == session.binding_id and
             session_response["local_handle_id"] == session.local_handle_id and
             session_response["state"] == "busy" do
      flunk("attach receipt response does not preserve the committed busy session")
    end

    unless not is_nil(receipt.inserted_at),
      do: flunk("attach receipt is missing its insertion timestamp")

    %{
      row: receipt,
      response: receipt.response,
      session: session_response,
      inserted_at: receipt.inserted_at,
      request_hash: receipt.request_hash,
      session_row: session
    }
  end

  defp observe_stop_receipt!(fixture, attach_snapshot, terminal_session) do
    run = Repo.get!(Run, fixture.run.id)
    runtime = Repo.get!(Runtime, run.runtime_id)
    stop_receipt_count = Repo.aggregate(HarnessSessionStopReceipt, :count)
    session_count = Repo.aggregate(HarnessSession, :count)

    unless stop_receipt_count == 1 and session_count == 1,
      do: flunk("expected exactly one stop receipt and harness session")

    receipt =
      Repo.one!(
        from row in HarnessSessionStopReceipt,
          where:
            row.session_id == ^attach_snapshot.session["id"] and
              row.binding_id == ^attach_snapshot.session["binding_id"]
      )

    session = Repo.get!(HarnessSession, receipt.session_id)
    stopped = get_in(receipt.response, ["session_stopped"])
    expected_lock_version = terminal_session.lock_version + 1

    unless receipt.run_id == run.id and receipt.machine_id == runtime.machine_id and
             receipt.session_id == attach_snapshot.session["id"] and
             receipt.binding_id == attach_snapshot.session["binding_id"] and
             run.harness_session_id == session.id and
             run.harness_binding_id == session.binding_id do
      flunk("stop receipt does not match the original run/session binding")
    end

    unless is_binary(receipt.request_hash) and byte_size(receipt.request_hash) == 32,
      do: flunk("stop receipt request hash is not a 32-byte digest")

    unless is_map(receipt.response) and is_map(stopped),
      do: flunk("stop receipt response is not a complete object")

    unless stopped["receipt_id"] == receipt.id and stopped["run_id"] == run.id and
             stopped["session_id"] == session.id and
             stopped["local_handle_id"] == session.local_handle_id and
             stopped["binding_id"] == session.binding_id and
             stopped["state"] == "available" and is_nil(stopped["active_run_id"]) and
             stopped["lock_version"] == expected_lock_version do
      flunk("stop receipt response does not preserve the committed available session")
    end

    unless session.state == "available" and is_nil(session.active_run_id) and
             session.lock_version == expected_lock_version do
      flunk("stopped session is not durably available at the expected lock version")
    end

    unless not is_nil(receipt.inserted_at),
      do: flunk("stop receipt is missing its insertion timestamp")

    %{
      row: receipt,
      response: receipt.response,
      stopped: stopped,
      inserted_at: receipt.inserted_at,
      request_hash: receipt.request_hash,
      session: session
    }
  end

  defp assert_attach_receipt_unchanged!(before, after_snapshot) do
    unless before.row == after_snapshot.row and before.response == after_snapshot.response and
             before.inserted_at == after_snapshot.inserted_at and
             before.request_hash == after_snapshot.request_hash do
      flunk("immutable attach receipt changed during recovery")
    end
  end

  defp assert_goal_receipt_session_unchanged!(before, after_snapshot) do
    unless before.session_row == after_snapshot.session_row do
      flunk("harness session changed during attach recovery")
    end
  end

  defp assert_stop_receipt_unchanged!(before, after_snapshot) do
    unless before.row == after_snapshot.row and before.response == after_snapshot.response and
             before.inserted_at == after_snapshot.inserted_at and
             before.request_hash == after_snapshot.request_hash do
      flunk("immutable stop receipt changed during recovery")
    end
  end

  defp stringify_keys(map), do: Map.new(map, fn {key, value} -> {to_string(key), value} end)
  defp bearer(conn, token), do: put_req_header(conn, "authorization", "Bearer " <> token)

  defp assert_error(conn, status, code) do
    assert %{"error" => %{"code" => ^code}} = json_response(conn, status)
  end
end
