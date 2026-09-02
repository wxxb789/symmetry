defmodule SymmetryControlWeb.ProtocolControllerTest do
  use SymmetryControlWeb.ConnCase, async: false

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Scheduler

  @enrollment_token "test-enrollment-token"
  @operator_token "test-operator-token"

  test "authenticates enrollment and keeps daemon and operator APIs isolated", %{conn: conn} do
    assert %{"error" => %{"code" => "unauthenticated"}} =
             conn
             |> post("/api/v1/daemon/enroll", %{"machine" => %{"name" => "builder"}})
             |> json_response(401)

    enrollment_conn = bearer(conn, @enrollment_token)

    assert %{"machine_id" => machine_id, "machine_token" => machine_token} =
             enrollment_conn
             |> post("/api/v1/daemon/enroll", %{"machine" => %{"name" => "builder"}})
             |> json_response(201)

    assert %{"error" => %{"code" => "forbidden"}} =
             bearer(conn, machine_token)
             |> post("/api/v1/tasks", wire_task_payload())
             |> json_response(403)

    assert %{"error" => %{"code" => "forbidden"}} =
             bearer(conn, @operator_token)
             |> post("/api/v1/daemon/sessions", session_payload())
             |> json_response(403)

    assert is_binary(machine_id)
  end

  test "enrolls, registers an online runtime, and returns compatible session DTO", %{conn: conn} do
    {_machine_id, machine_token} = enroll(conn)

    assert %{
             "runtimes" => [
               %{"runtime_id" => runtime_id, "runtime_epoch" => 1, "runtime_key" => "default"}
             ],
             "heartbeat_interval_ms" => 5_000,
             "poll_interval_ms" => 5_000,
             "lease_duration_ms" => 30_000,
             "websocket_path" => "/socket/websocket?vsn=2.0.0"
           } =
             bearer(conn, machine_token)
             |> post("/api/v1/daemon/sessions", session_payload())
             |> json_response(201)

    assert {:ok, snapshot} = Orchestration.work_snapshot(runtime_id, 1)
    assert snapshot.assignments == []
  end

  test "session rejects a non-map runtime specification", %{conn: conn} do
    {_machine_id, machine_token} = enroll(conn)

    assert %{"error" => %{"code" => "invalid_request"}} =
             bearer(conn, machine_token)
             |> post("/api/v1/daemon/sessions", %{
               "daemon_instance_id" => "00000000-0000-0000-0000-000000000001",
               "runtimes" => ["not-a-runtime"]
             })
             |> json_response(400)
  end

  test "submitting a task wakes scheduling and exposes its assignment snapshot", %{conn: conn} do
    {_machine_id, machine_token} = enroll(conn)
    runtime_id = register(conn, machine_token)

    assert %{
             "task_id" => task_id,
             "state" => "queued",
             "run_id" => nil,
             "generation" => 0,
             "work" => %{"goal" => "Run tests", "input" => %{"branch" => "main"}},
             "result" => nil,
             "failure" => nil
           } =
             bearer(conn, @operator_token)
             |> put_req_header("idempotency-key", "task-auto-assigned")
             |> post("/api/v1/tasks", wire_task_payload())
             |> json_response(201)

    assert :ok = Scheduler.drain()

    assert %{
             "assignments" => [
               %{"task_id" => ^task_id, "work" => %{"input" => %{"branch" => "main"}}}
             ]
           } =
             bearer(conn, machine_token)
             |> get("/api/v1/runtimes/#{runtime_id}/work?runtime_epoch=1")
             |> json_response(200)
  end

  test "task create requires the Go work envelope", %{conn: conn} do
    assert %{"error" => %{"code" => "invalid_request"}} =
             bearer(conn, @operator_token)
             |> put_req_header("idempotency-key", "flat-task")
             |> post("/api/v1/tasks", work_payload())
             |> json_response(400)
  end

  test "runtime ownership prevents a machine from reading or mutating another machine runtime", %{
    conn: conn
  } do
    {_first_id, first_token} = enroll(conn)
    first_runtime_id = register(conn, first_token)
    {_second_id, second_token} = enroll(conn, "second")

    assert %{"error" => %{"code" => "forbidden"}} =
             bearer(conn, second_token)
             |> get("/api/v1/runtimes/#{first_runtime_id}/work?runtime_epoch=1")
             |> json_response(403)
  end

  test "claim, fenced state and event writes, cancellation command, and acknowledgement use protocol DTOs",
       %{conn: conn} do
    {_machine_id, machine_token} = enroll(conn)
    runtime_id = register(conn, machine_token)
    task_id = submit_and_assign(conn)

    work =
      bearer(conn, machine_token)
      |> get("/api/v1/runtimes/#{runtime_id}/work?runtime_epoch=1")
      |> json_response(200)

    [%{"run_id" => run_id, "generation" => generation}] = work["assignments"]
    claim_id = "00000000-0000-0000-0000-000000000101"

    assert %{
             "lease_token" => lease_token,
             "lease_expires_at" => lease_expires_at,
             "work" => %{"goal" => "Run tests"}
           } =
             bearer(conn, machine_token)
             |> post("/api/v1/runs/#{run_id}/claim", %{
               "runtime_id" => runtime_id,
               "runtime_epoch" => 1,
               "generation" => generation,
               "claim_id" => claim_id
             })
             |> json_response(200)

    assert String.ends_with?(lease_expires_at, "Z")
    fence = fence(runtime_id, generation, claim_id, lease_token)

    assert %{"events" => [_]} =
             bearer(conn, machine_token)
             |> post(
               "/api/v1/runs/#{run_id}/events",
               Map.merge(fence, %{"events" => [event_payload()]})
             )
             |> json_response(201)

    assert %{"state" => "running"} =
             bearer(conn, machine_token)
             |> post(
               "/api/v1/runs/#{run_id}/state",
               Map.merge(fence, %{
                 "transition_id" => "00000000-0000-0000-0000-000000000102",
                 "state" => "running",
                 "payload" => %{}
               })
             )
             |> json_response(200)

    assert %{
             "state" => "cancelling",
             "command" => %{"kind" => "cancel", "command_id" => command_id}
           } =
             bearer(conn, @operator_token)
             |> post("/api/v1/tasks/#{task_id}/cancel", %{})
             |> json_response(200)

    assert %{"command_id" => ^command_id, "acknowledgement_outcome" => "applied"} =
             bearer(conn, machine_token)
             |> post(
               "/api/v1/commands/#{command_id}/ack",
               Map.merge(fence, %{
                 "outcome" => "applied",
                 "ack_id" => "00000000-0000-0000-0000-000000000103"
               })
             )
             |> json_response(200)
  end

  test "waiting task accepts input and a stale machine receives ownership_lost", %{conn: conn} do
    {_machine_id, machine_token} = enroll(conn)
    runtime_id = register(conn, machine_token)
    _task_id = submit_and_assign(conn)

    [%{"run_id" => run_id, "generation" => generation}] =
      bearer(conn, machine_token)
      |> get("/api/v1/runtimes/#{runtime_id}/work?runtime_epoch=1")
      |> json_response(200)
      |> Map.fetch!("assignments")

    claim_id = "00000000-0000-0000-0000-000000000104"

    %{"lease_token" => lease_token} =
      bearer(conn, machine_token)
      |> post("/api/v1/runs/#{run_id}/claim", %{
        "runtime_id" => runtime_id,
        "runtime_epoch" => 1,
        "generation" => generation,
        "claim_id" => claim_id
      })
      |> json_response(200)

    fence = fence(runtime_id, generation, claim_id, lease_token)

    assert %{"error" => %{"code" => "ownership_lost"}} =
             bearer(conn, machine_token)
             |> post("/api/v1/runs/#{run_id}/heartbeat", %{fence | "runtime_epoch" => 2})
             |> json_response(409)
  end

  test "waiting task input is delivered in the machine snapshot and reconcile returns a decision",
       %{conn: conn} do
    {_machine_id, machine_token} = enroll(conn)
    runtime_id = register(conn, machine_token)
    task_id = submit_and_assign(conn)

    [%{"run_id" => run_id, "generation" => generation}] =
      bearer(conn, machine_token)
      |> get("/api/v1/runtimes/#{runtime_id}/work?runtime_epoch=1")
      |> json_response(200)
      |> Map.fetch!("assignments")

    claim_id = "00000000-0000-0000-0000-000000000106"

    %{"lease_token" => lease_token} =
      bearer(conn, machine_token)
      |> post("/api/v1/runs/#{run_id}/claim", %{
        "runtime_id" => runtime_id,
        "runtime_epoch" => 1,
        "generation" => generation,
        "claim_id" => claim_id
      })
      |> json_response(200)

    fence = fence(runtime_id, generation, claim_id, lease_token)

    for {state, transition_id} <- [
          {"running", "00000000-0000-0000-0000-000000000107"},
          {"waiting_for_input", "00000000-0000-0000-0000-000000000108"}
        ] do
      assert %{"state" => ^state} =
               bearer(conn, machine_token)
               |> post(
                 "/api/v1/runs/#{run_id}/state",
                 Map.merge(fence, %{
                   "transition_id" => transition_id,
                   "state" => state,
                   "payload" => %{}
                 })
               )
               |> json_response(200)
    end

    assert %{
             "task_id" => ^task_id,
             "state" => "waiting_for_input",
             "run_id" => ^run_id,
             "generation" => ^generation,
             "work" => %{"goal" => "Run tests"},
             "command" => %{"kind" => "provide_input", "payload" => %{"branch" => "release"}}
           } =
             bearer(conn, @operator_token)
             |> put_req_header("idempotency-key", "task-input")
             |> post("/api/v1/tasks/#{task_id}/input", %{"input" => %{"branch" => "release"}})
             |> json_response(201)

    assert %{"commands" => [%{"kind" => "provide_input"}]} =
             bearer(conn, machine_token)
             |> get("/api/v1/runtimes/#{runtime_id}/work?runtime_epoch=1")
             |> json_response(200)

    assert %{"decisions" => [%{"run_id" => ^run_id, "decision" => "continue"}]} =
             bearer(conn, machine_token)
             |> post("/api/v1/runtimes/#{runtime_id}/reconcile", %{
               "runtime_epoch" => 1,
               "runs" => [
                 Map.merge(fence, %{
                   "run_id" => run_id,
                   "claimed_runtime_epoch" => 1,
                   "local_state" => "waiting_for_input",
                   "last_event_sequence" => 1
                 })
               ]
             })
             |> json_response(200)
  end

  test "a machine cannot append events or acknowledge commands for another machine run", %{
    conn: conn
  } do
    {_first_machine_id, first_token} = enroll(conn)
    runtime_id = register(conn, first_token)
    task_id = submit_and_assign(conn)
    {_second_machine_id, second_token} = enroll(conn, "other-machine")

    [%{"run_id" => run_id, "generation" => generation}] =
      bearer(conn, first_token)
      |> get("/api/v1/runtimes/#{runtime_id}/work?runtime_epoch=1")
      |> json_response(200)
      |> Map.fetch!("assignments")

    claim_id = "00000000-0000-0000-0000-000000000109"

    %{"lease_token" => lease_token} =
      bearer(conn, first_token)
      |> post("/api/v1/runs/#{run_id}/claim", %{
        "runtime_id" => runtime_id,
        "runtime_epoch" => 1,
        "generation" => generation,
        "claim_id" => claim_id
      })
      |> json_response(200)

    fence = fence(runtime_id, generation, claim_id, lease_token)

    assert %{"error" => %{"code" => "forbidden"}} =
             bearer(conn, second_token)
             |> post(
               "/api/v1/runs/#{run_id}/events",
               Map.merge(fence, %{"events" => [event_payload()]})
             )
             |> json_response(403)

    assert %{"error" => %{"code" => "forbidden"}} =
             bearer(conn, second_token)
             |> post("/api/v1/runs/#{run_id}/heartbeat", fence)
             |> json_response(403)

    assert %{"state" => "running"} =
             bearer(conn, first_token)
             |> post(
               "/api/v1/runs/#{run_id}/state",
               Map.merge(fence, %{
                 "transition_id" => "00000000-0000-0000-0000-000000000110",
                 "state" => "running",
                 "payload" => %{}
               })
             )
             |> json_response(200)

    assert %{"command" => %{"command_id" => command_id}} =
             bearer(conn, @operator_token)
             |> post("/api/v1/tasks/#{task_id}/cancel", %{})
             |> json_response(200)

    assert %{"error" => %{"code" => "forbidden"}} =
             bearer(conn, second_token)
             |> post(
               "/api/v1/commands/#{command_id}/ack",
               Map.merge(fence, %{
                 "outcome" => "applied",
                 "ack_id" => "00000000-0000-0000-0000-000000000111"
               })
             )
             |> json_response(403)
  end

  test "legacy acknowledgement_id is rejected", %{conn: conn} do
    {_machine_id, machine_token} = enroll(conn)
    runtime_id = register(conn, machine_token)
    task_id = submit_and_assign(conn)

    [%{"run_id" => run_id, "generation" => generation}] =
      bearer(conn, machine_token)
      |> get("/api/v1/runtimes/#{runtime_id}/work?runtime_epoch=1")
      |> json_response(200)
      |> Map.fetch!("assignments")

    claim_id = "00000000-0000-0000-0000-000000000112"

    %{"lease_token" => lease_token} =
      bearer(conn, machine_token)
      |> post("/api/v1/runs/#{run_id}/claim", %{
        "runtime_id" => runtime_id,
        "runtime_epoch" => 1,
        "generation" => generation,
        "claim_id" => claim_id
      })
      |> json_response(200)

    fence = fence(runtime_id, generation, claim_id, lease_token)

    assert %{"state" => "running"} =
             bearer(conn, machine_token)
             |> post(
               "/api/v1/runs/#{run_id}/state",
               Map.merge(fence, %{
                 "transition_id" => "00000000-0000-0000-0000-000000000113",
                 "state" => "running",
                 "payload" => %{}
               })
             )
             |> json_response(200)

    assert %{"command" => %{"command_id" => command_id}} =
             bearer(conn, @operator_token)
             |> post("/api/v1/tasks/#{task_id}/cancel", %{})
             |> json_response(200)

    assert %{"error" => %{"code" => "invalid_request"}} =
             bearer(conn, machine_token)
             |> post(
               "/api/v1/commands/#{command_id}/ack",
               Map.merge(fence, %{
                 "outcome" => "applied",
                 "acknowledgement_id" => "00000000-0000-0000-0000-000000000114"
               })
             )
             |> json_response(400)
  end

  defp enroll(conn, name \\ "builder") do
    response =
      bearer(conn, @enrollment_token)
      |> post("/api/v1/daemon/enroll", %{"machine" => %{"name" => name}})
      |> json_response(201)

    {response["machine_id"], response["machine_token"]}
  end

  defp register(conn, machine_token) do
    bearer(conn, machine_token)
    |> post("/api/v1/daemon/sessions", session_payload())
    |> json_response(201)
    |> get_in(["runtimes", Access.at(0), "runtime_id"])
  end

  defp submit_and_assign(conn) do
    task_id =
      bearer(conn, @operator_token)
      |> put_req_header("idempotency-key", "task-#{System.unique_integer([:positive])}")
      |> post("/api/v1/tasks", wire_task_payload())
      |> json_response(201)
      |> Map.fetch!("task_id")

    assert :ok = Scheduler.drain()
    task_id
  end

  defp bearer(conn, token), do: put_req_header(conn, "authorization", "Bearer " <> token)

  defp session_payload do
    %{
      "daemon_instance_id" => "00000000-0000-0000-0000-000000000001",
      "runtimes" => [
        %{
          "runtime_key" => "default",
          "name" => "Local Codex",
          "capacity" => 1,
          "agent_profile" => "codex",
          "workspace" => "primary",
          "capabilities" => %{}
        }
      ]
    }
  end

  defp wire_task_payload, do: %{"work" => work_payload()}

  defp work_payload do
    %{
      "goal" => "Run tests",
      "agent_profile" => "codex",
      "workspace" => "primary",
      "input" => %{"branch" => "main"}
    }
  end

  defp fence(runtime_id, generation, claim_id, lease_token) do
    %{
      "runtime_id" => runtime_id,
      "runtime_epoch" => 1,
      "generation" => generation,
      "claim_id" => claim_id,
      "lease_token" => lease_token
    }
  end

  defp event_payload do
    %{
      "event_id" => "00000000-0000-0000-0000-000000000105",
      "sequence" => 1,
      "kind" => "progress",
      "occurred_at" => "2026-09-02T00:00:00Z",
      "payload" => %{"message" => "running"}
    }
  end
end
