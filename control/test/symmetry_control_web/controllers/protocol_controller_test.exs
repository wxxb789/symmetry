defmodule SymmetryControlWeb.ProtocolControllerTest do
  use SymmetryControlWeb.ConnCase, async: false

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Scheduler

  @enrollment_token "test-enrollment-token"
  @operator_token "test-operator-token"

  test "renders terminal grace expiry with the protocol error envelope" do
    response = SymmetryControlWeb.Protocol.error(build_conn(), :terminal_grace_expired)

    assert response.status == 409

    assert %{"error" => %{"code" => "terminal_grace_expired"}} =
             Jason.decode!(response.resp_body)
  end

  test "uses the resource-oriented machine, runtime, run, and acknowledgement routes", %{
    conn: conn
  } do
    {machine_id, machine_token} = enroll(conn)
    runtime_id = register(conn, machine_id, machine_token)
    task_id = submit_and_assign(conn)

    assert %{"assignments" => [_], "commands" => [], "server_time" => _} =
             bearer(conn, machine_token)
             |> patch("/api/v1/runtimes/#{runtime_id}", %{
               "runtime_id" => uuid(),
               "runtime_epoch" => 1,
               "active_runs" => []
             })
             |> json_response(200)

    [%{"run_id" => run_id, "generation" => generation}] =
      bearer(conn, machine_token)
      |> get("/api/v1/runtimes/#{runtime_id}/dispatch?runtime_epoch=1")
      |> json_response(200)
      |> Map.fetch!("assignments")

    claim_id = uuid()

    assert %{
             "run_id" => ^run_id,
             "task_id" => ^task_id,
             "generation" => ^generation,
             "claim_id" => ^claim_id,
             "lease_token" => lease_token
           } =
             bearer(conn, machine_token)
             |> put("/api/v1/runs/#{run_id}/claims/#{claim_id}", %{
               "run_id" => uuid(),
               "claim_id" => uuid(),
               "runtime_id" => runtime_id,
               "runtime_epoch" => 1,
               "generation" => generation
             })
             |> json_response(200)

    fence = fence(runtime_id, generation, claim_id, lease_token)
    backlog = String.duplicate("x", 1_100_000)

    events_conn =
      bearer(conn, machine_token)
      |> post(
        "/api/v1/runs/#{run_id}/events",
        Map.merge(fence, %{
          "run_id" => uuid(),
          "events" => [Map.put(event_payload(), "payload", %{"backlog" => backlog})]
        })
      )

    assert response(events_conn, 204) == ""

    transition_id = uuid()

    assert %{"run_id" => ^run_id, "state" => "running"} =
             bearer(conn, machine_token)
             |> put(
               "/api/v1/runs/#{run_id}/transitions/#{transition_id}",
               Map.merge(fence, %{
                 "transition_id" => uuid(),
                 "state" => "running",
                 "payload" => %{}
               })
             )
             |> json_response(200)

    assert %{"lease_expires_at" => _, "commands" => []} =
             bearer(conn, machine_token)
             |> patch("/api/v1/runs/#{run_id}/lease", Map.put(fence, "run_id", uuid()))
             |> json_response(200)

    command =
      bearer(conn, @operator_token)
      |> put_req_header("idempotency-key", "cancel-#{System.unique_integer([:positive])}")
      |> post("/api/v1/tasks/#{task_id}/commands", %{"kind" => "cancel"})
      |> json_response(201)

    assert_command(command, task_id, run_id, generation, "cancel", %{}, "pending")
    command_id = command["command_id"]
    ack_id = uuid()

    assert_error(
      bearer(conn, machine_token)
      |> put(
        "/api/v1/commands/#{command_id}/acknowledgements/#{uuid()}",
        fence |> Map.delete("run_id") |> Map.put("outcome", "applied")
      ),
      400,
      "invalid_request"
    )

    assert_error(
      bearer(conn, machine_token)
      |> put(
        "/api/v1/commands/#{command_id}/acknowledgements/#{uuid()}",
        fence |> Map.put("run_id", uuid()) |> Map.put("outcome", "applied")
      ),
      400,
      "invalid_request"
    )

    assert_error(
      bearer(conn, machine_token)
      |> put(
        "/api/v1/commands/#{command_id}/acknowledgements/#{uuid()}?outcome=applied",
        Map.put(fence, "run_id", run_id)
      ),
      400,
      "invalid_request"
    )

    assert %{
             "command_id" => ^command_id,
             "state" => "acknowledged",
             "acknowledgement_id" => ^ack_id
           } =
             bearer(conn, machine_token)
             |> put(
               "/api/v1/commands/#{command_id}/acknowledgements/#{ack_id}",
               Map.merge(fence, %{
                 "command_id" => uuid(),
                 "ack_id" => uuid(),
                 "run_id" => run_id,
                 "outcome" => "applied"
               })
             )
             |> json_response(200)

    assert %{"decisions" => _} =
             bearer(conn, machine_token)
             |> put("/api/v1/runtimes/#{runtime_id}/reconciliation", %{
               "runtime_id" => uuid(),
               "runtime_epoch" => 1,
               "runs" => [
                 Map.merge(fence, %{
                   "run_id" => run_id,
                   "claimed_runtime_epoch" => 1,
                   "local_state" => "cancelling",
                   "last_event_sequence" => 1
                 })
               ]
             })
             |> json_response(200)
  end

  test "machine enrollment requires a replay key and reuses the exact request", %{conn: conn} do
    key = uuid()
    token = "machine-token-#{uuid()}"
    request = %{"machine" => %{"name" => "builder"}, "machine_token" => token}

    first =
      bearer(conn, @enrollment_token)
      |> put_req_header("idempotency-key", key)
      |> post("/api/v1/machines", request)
      |> json_response(201)

    replayed =
      bearer(conn, @enrollment_token)
      |> put_req_header("idempotency-key", key)
      |> post("/api/v1/machines", request)
      |> json_response(200)

    assert replayed == first
    assert first["machine_token"] == token

    assert_error(
      bearer(conn, @enrollment_token)
      |> put_req_header("idempotency-key", key)
      |> post("/api/v1/machines", put_in(request, ["machine", "name"], "other")),
      409,
      "idempotency_conflict"
    )

    assert_error(
      bearer(conn, @enrollment_token) |> post("/api/v1/machines", request),
      400,
      "invalid_request"
    )

    assert_error(
      bearer(conn, @enrollment_token)
      |> put_req_header("idempotency-key", uuid())
      |> post("/api/v1/machines", %{request | "machine_token" => ""}),
      400,
      "invalid_request"
    )

    assert_error(
      bearer(conn, @enrollment_token)
      |> put_req_header("idempotency-key", key)
      |> post("/api/v1/machines", %{request | "machine_token" => "different-token"}),
      409,
      "idempotency_conflict"
    )
  end

  test "session rejects a non-map runtime specification", %{conn: conn} do
    {machine_id, machine_token} = enroll(conn)

    assert_error(
      bearer(conn, machine_token)
      |> put("/api/v1/machines/#{machine_id}/sessions/#{uuid()}", %{
        "runtimes" => ["not-a-runtime"]
      }),
      400,
      "invalid_request"
    )
  end

  test "session response advertises the configured lease duration", %{conn: conn} do
    {machine_id, machine_token} = enroll(conn)

    response =
      bearer(conn, machine_token)
      |> put("/api/v1/machines/#{machine_id}/sessions/#{uuid()}", %{
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
      })
      |> json_response(200)

    configured = Application.fetch_env!(:symmetry_control, :orchestration)
    assert response["lease_duration_ms"] == configured[:lease_duration_ms]
  end

  test "runtime mutations do not source required business fields from query params", %{conn: conn} do
    {machine_id, machine_token} = enroll(conn)
    runtime_id = register(conn, machine_id, machine_token)

    assert_error(
      bearer(conn, machine_token)
      |> patch("/api/v1/runtimes/#{runtime_id}?runtime_epoch=1", %{"active_runs" => []}),
      400,
      "invalid_request"
    )

    assert %{"server_time" => _} =
             bearer(conn, machine_token)
             |> patch("/api/v1/runtimes/#{runtime_id}?runtime_epoch=2", %{
               "runtime_epoch" => 1,
               "active_runs" => []
             })
             |> json_response(200)
  end

  test "runtime heartbeat ignores action params that are not body params", %{conn: conn} do
    {machine_id, machine_token} = enroll(conn)
    runtime_id = register(conn, machine_id, machine_token)
    {:ok, machine} = Orchestration.authenticate_machine(machine_token)

    response =
      build_conn()
      |> assign(:machine, machine)
      |> Map.put(:path_params, %{"runtime_id" => runtime_id})
      |> Map.put(:body_params, %{"active_runs" => []})
      |> SymmetryControlWeb.DaemonController.heartbeat(%{"runtime_epoch" => 1})

    assert response.status == 400
    assert %{"error" => %{"code" => "invalid_request"}} = Jason.decode!(response.resp_body)
  end

  test "mutation actions only consume business fields from body params", %{conn: conn} do
    {machine_id, machine_token} = enroll(conn)
    runtime_id = register(conn, machine_id, machine_token)
    {:ok, machine} = Orchestration.authenticate_machine(machine_token)

    assert_direct_invalid_request(
      build_conn()
      |> assign(:enrollment_token, @enrollment_token)
      |> Map.put(:body_params, %{})
      |> SymmetryControlWeb.DaemonController.enroll(%{"machine" => %{"name" => "query-only"}})
    )

    assert_direct_invalid_request(
      build_conn()
      |> put_req_header("idempotency-key", "body-only-task")
      |> Map.put(:body_params, %{})
      |> SymmetryControlWeb.TaskController.create(%{"work" => work_payload()})
    )

    assert_direct_invalid_request(
      machine_conn(
        machine,
        %{
          "machine_id" => machine_id,
          "daemon_instance_id" => uuid()
        },
        %{}
      )
      |> SymmetryControlWeb.DaemonController.register_session(%{"runtimes" => []})
    )

    assert_direct_invalid_request(
      machine_conn(machine, %{"runtime_id" => runtime_id}, %{"active_runs" => []})
      |> SymmetryControlWeb.DaemonController.heartbeat(%{"runtime_epoch" => 1})
    )

    assert_direct_invalid_request(
      machine_conn(machine, %{"runtime_id" => runtime_id}, %{"runs" => []})
      |> SymmetryControlWeb.DaemonController.reconcile(%{"runtime_epoch" => 1})
    )

    task_id = submit_and_assign(conn)

    [%{"run_id" => pending_run_id, "generation" => pending_generation}] =
      bearer(conn, machine_token)
      |> get("/api/v1/runtimes/#{runtime_id}/dispatch?runtime_epoch=1")
      |> json_response(200)
      |> Map.fetch!("assignments")

    assert_direct_invalid_request(
      machine_conn(machine, %{"run_id" => pending_run_id, "claim_id" => uuid()}, %{})
      |> SymmetryControlWeb.DaemonController.claim(%{
        "runtime_id" => runtime_id,
        "runtime_epoch" => 1,
        "generation" => pending_generation
      })
    )

    {run_id, _generation, fence} = claim(conn, machine_token, runtime_id)

    assert_direct_invalid_request(
      machine_conn(machine, %{"run_id" => run_id}, %{})
      |> SymmetryControlWeb.DaemonController.heartbeat_run(fence)
    )

    assert_direct_invalid_request(
      machine_conn(machine, %{"run_id" => run_id}, fence)
      |> SymmetryControlWeb.DaemonController.append_events(%{"events" => [event_payload()]})
    )

    assert_direct_invalid_request(
      machine_conn(machine, %{"run_id" => run_id, "transition_id" => uuid()}, fence)
      |> SymmetryControlWeb.DaemonController.transition(%{"state" => "running"})
    )

    assert_direct_invalid_request(
      build_conn()
      |> put_req_header("idempotency-key", "body-only-command")
      |> Map.put(:path_params, %{"task_id" => task_id})
      |> Map.put(:body_params, %{})
      |> SymmetryControlWeb.TaskController.command(%{"kind" => "cancel"})
    )

    command =
      bearer(conn, @operator_token)
      |> put_req_header("idempotency-key", "ack-command")
      |> post("/api/v1/tasks/#{task_id}/commands", %{"kind" => "cancel"})
      |> json_response(201)

    assert_direct_invalid_request(
      machine_conn(
        machine,
        %{"command_id" => command["command_id"], "ack_id" => uuid()},
        Map.put(fence, "run_id", run_id)
      )
      |> SymmetryControlWeb.DaemonController.acknowledge_command(%{"outcome" => "applied"})
    )
  end

  test "task creation requires a map work envelope", %{conn: conn} do
    for body <- [%{}, work_payload(), %{"work" => "not-a-map"}] do
      assert_error(
        bearer(conn, @operator_token)
        |> put_req_header("idempotency-key", "task-#{System.unique_integer([:positive])}")
        |> post("/api/v1/tasks", body),
        400,
        "invalid_request"
      )
    end
  end

  test "task creation preserves omitted and explicit empty input distinctly", %{conn: conn} do
    omitted_input = Map.delete(work_payload(), "input")
    explicit_empty_input = Map.put(work_payload(), "input", %{})

    assert %{"work" => %{"input" => nil}} =
             bearer(conn, @operator_token)
             |> put_req_header("idempotency-key", "omitted-input")
             |> post("/api/v1/tasks", %{"work" => omitted_input})
             |> json_response(201)

    assert %{"work" => %{"input" => %{}}} =
             bearer(conn, @operator_token)
             |> put_req_header("idempotency-key", "empty-input")
             |> post("/api/v1/tasks", %{"work" => explicit_empty_input})
             |> json_response(201)
  end

  test "a stale fence on the lease resource returns ownership_lost", %{conn: conn} do
    {machine_id, machine_token} = enroll(conn)
    runtime_id = register(conn, machine_id, machine_token)
    _task_id = submit_and_assign(conn)
    {run_id, _generation, fence} = claim(conn, machine_token, runtime_id)

    assert_error(
      bearer(conn, machine_token)
      |> patch("/api/v1/runs/#{run_id}/lease", Map.update!(fence, "runtime_epoch", &(&1 + 1))),
      409,
      "ownership_lost"
    )
  end

  test "task commands are exact, idempotent command resources", %{conn: conn} do
    task_id = submit_task(conn)
    key = "cancel-#{System.unique_integer([:positive])}"

    created_conn =
      bearer(conn, @operator_token)
      |> put_req_header("idempotency-key", key)
      |> post("/api/v1/tasks/#{task_id}/commands", %{"kind" => "cancel"})

    command = json_response(created_conn, 201)
    assert_command(command, task_id, nil, nil, "cancel", %{}, "applied")
    assert is_binary(command["issued_at"])
    assert is_binary(command["applied_at"])
    assert command["acknowledgement_id"] == nil
    assert command["acknowledgement_outcome"] == nil
    assert command["acknowledged_at"] == nil

    assert ^command =
             bearer(conn, @operator_token)
             |> put_req_header("idempotency-key", key)
             |> post("/api/v1/tasks/#{task_id}/commands", %{"kind" => "cancel"})
             |> json_response(200)

    assert %{"error" => %{"code" => "idempotency_conflict"}} =
             bearer(conn, @operator_token)
             |> put_req_header("idempotency-key", key)
             |> post("/api/v1/tasks/#{task_id}/commands", %{
               "kind" => "provide_input",
               "payload" => %{}
             })
             |> json_response(409)

    assert %{"error" => %{"code" => "state_conflict"}} =
             bearer(conn, @operator_token)
             |> put_req_header("idempotency-key", "new-#{key}")
             |> post("/api/v1/tasks/#{task_id}/commands", %{"kind" => "cancel"})
             |> json_response(409)

    for body <- [
          %{},
          %{"kind" => "cancel", "payload" => %{}},
          %{"kind" => "provide_input", "payload" => nil},
          %{"kind" => "provide_input", "payload" => %{}, "extra" => true}
        ] do
      assert %{"error" => %{"code" => "invalid_request"}} =
               bearer(conn, @operator_token)
               |> put_req_header(
                 "idempotency-key",
                 "invalid-#{System.unique_integer([:positive])}"
               )
               |> post("/api/v1/tasks/#{task_id}/commands", body)
               |> json_response(400)
    end
  end

  test "provide_input permits an empty payload only for a current waiting run", %{conn: conn} do
    {machine_id, machine_token} = enroll(conn)
    runtime_id = register(conn, machine_id, machine_token)
    task_id = submit_and_assign(conn)
    {run_id, generation, fence} = claim(conn, machine_token, runtime_id)

    for state <- ["running", "waiting_for_input"] do
      assert %{"state" => ^state} =
               bearer(conn, machine_token)
               |> put(
                 "/api/v1/runs/#{run_id}/transitions/#{uuid()}",
                 Map.merge(fence, %{"state" => state, "payload" => %{}})
               )
               |> json_response(200)
    end

    assert %{"run_id" => ^run_id, "generation" => ^generation} =
             command =
             bearer(conn, @operator_token)
             |> put_req_header("idempotency-key", "input-#{System.unique_integer([:positive])}")
             |> post("/api/v1/tasks/#{task_id}/commands", %{
               "kind" => "provide_input",
               "payload" => %{}
             })
             |> json_response(201)

    assert_command(command, task_id, run_id, generation, "provide_input", %{}, "pending")
  end

  test "provide_input commands are returned in dispatch and reconciliation snapshots", %{
    conn: conn
  } do
    {machine_id, machine_token} = enroll(conn)
    runtime_id = register(conn, machine_id, machine_token)
    task_id = submit_and_assign(conn)
    {run_id, generation, fence} = claim(conn, machine_token, runtime_id)

    for state <- ["running", "waiting_for_input"] do
      assert %{"state" => ^state} =
               bearer(conn, machine_token)
               |> put(
                 "/api/v1/runs/#{run_id}/transitions/#{uuid()}",
                 Map.merge(fence, %{"state" => state, "payload" => %{}})
               )
               |> json_response(200)
    end

    %{"command_id" => command_id} =
      bearer(conn, @operator_token)
      |> put_req_header("idempotency-key", "input-#{System.unique_integer([:positive])}")
      |> post("/api/v1/tasks/#{task_id}/commands", %{
        "kind" => "provide_input",
        "payload" => %{"branch" => "release"}
      })
      |> json_response(201)

    assert %{
             "commands" => [
               %{
                 "command_id" => ^command_id,
                 "generation" => ^generation,
                 "kind" => "provide_input"
               }
             ]
           } =
             bearer(conn, machine_token)
             |> get("/api/v1/runtimes/#{runtime_id}/dispatch?runtime_epoch=1")
             |> json_response(200)

    assert %{
             "commands" => [
               %{
                 "command_id" => ^command_id,
                 "generation" => ^generation,
                 "kind" => "provide_input"
               }
             ]
           } =
             bearer(conn, machine_token)
             |> put("/api/v1/runtimes/#{runtime_id}/reconciliation", %{
               "runtime_epoch" => 1,
               "runs" => [
                 Map.merge(fence, %{
                   "run_id" => run_id,
                   "claimed_runtime_epoch" => 1,
                   "local_state" => "waiting_for_input",
                   "last_event_sequence" => 0
                 })
               ]
             })
             |> json_response(200)
  end

  test "auth class and machine ownership distinguish unauthenticated from forbidden", %{
    conn: conn
  } do
    assert_error(
      post(conn, "/api/v1/machines", %{"machine" => %{"name" => "missing"}}),
      401,
      "unauthenticated"
    )

    assert_error(
      bearer(conn, "unknown") |> post("/api/v1/tasks", wire_task_payload()),
      401,
      "unauthenticated"
    )

    assert_error(
      bearer(conn, @operator_token) |> post("/api/v1/machines", %{"machine" => %{}}),
      403,
      "forbidden"
    )

    assert_error(
      bearer(conn, @enrollment_token) |> post("/api/v1/tasks", wire_task_payload()),
      403,
      "forbidden"
    )

    {first_machine_id, first_token} = enroll(conn, "first")
    first_runtime_id = register(conn, first_machine_id, first_token)
    {_second_machine_id, second_token} = enroll(conn, "second")

    assert_error(
      get(conn, "/api/v1/runtimes/#{first_runtime_id}/dispatch?runtime_epoch=1"),
      401,
      "unauthenticated"
    )

    assert_error(
      bearer(conn, "unknown")
      |> get("/api/v1/runtimes/#{first_runtime_id}/dispatch?runtime_epoch=1"),
      401,
      "unauthenticated"
    )

    assert_error(
      bearer(conn, first_token) |> post("/api/v1/tasks", wire_task_payload()),
      403,
      "forbidden"
    )

    assert_error(
      bearer(conn, first_token) |> post("/api/v1/machines", %{"machine" => %{}}),
      403,
      "forbidden"
    )

    assert_error(
      bearer(conn, @operator_token)
      |> put("/api/v1/machines/#{first_machine_id}/sessions/#{uuid()}", %{"runtimes" => []}),
      403,
      "forbidden"
    )

    assert_error(
      bearer(conn, @enrollment_token)
      |> get("/api/v1/runtimes/#{first_runtime_id}/dispatch?runtime_epoch=1"),
      403,
      "forbidden"
    )

    assert_error(
      bearer(conn, second_token)
      |> get("/api/v1/runtimes/#{first_runtime_id}/dispatch?runtime_epoch=1"),
      403,
      "forbidden"
    )
  end

  test "a machine cannot append events or acknowledge commands for another machine run", %{
    conn: conn
  } do
    {first_machine_id, first_token} = enroll(conn, "first")
    runtime_id = register(conn, first_machine_id, first_token)
    task_id = submit_and_assign(conn)
    {run_id, _generation, fence} = claim(conn, first_token, runtime_id)
    {_second_machine_id, second_token} = enroll(conn, "second")

    command =
      bearer(conn, @operator_token)
      |> put_req_header("idempotency-key", "cancel-#{System.unique_integer([:positive])}")
      |> post("/api/v1/tasks/#{task_id}/commands", %{"kind" => "cancel"})
      |> json_response(201)

    assert_error(
      bearer(conn, second_token)
      |> post("/api/v1/runs/#{run_id}/events", Map.put(fence, "events", [event_payload()])),
      403,
      "forbidden"
    )

    assert_error(
      bearer(conn, second_token)
      |> put(
        "/api/v1/commands/#{command["command_id"]}/acknowledgements/#{uuid()}",
        fence |> Map.put("run_id", run_id) |> Map.put("outcome", "applied")
      ),
      403,
      "forbidden"
    )
  end

  test "event payload containing NUL is rejected as invalid_request", %{conn: conn} do
    {machine_id, machine_token} = enroll(conn)
    runtime_id = register(conn, machine_id, machine_token)
    _task_id = submit_and_assign(conn)
    {run_id, _generation, fence} = claim(conn, machine_token, runtime_id)

    assert_error(
      bearer(conn, machine_token)
      |> post(
        "/api/v1/runs/#{run_id}/events",
        Map.put(fence, "events", [
          Map.put(event_payload(), "payload", %{"nested" => ["bad\0value"]})
        ])
      ),
      400,
      "invalid_request"
    )
  end

  test "does not retain legacy action routes", %{conn: conn} do
    for {method, path} <- [
          {:post, "/api/v1/daemon/enroll"},
          {:post, "/api/v1/daemon/sessions"},
          {:post, "/api/v1/runtimes/#{uuid()}/heartbeat"},
          {:get, "/api/v1/runtimes/#{uuid()}/work"},
          {:post, "/api/v1/runtimes/#{uuid()}/reconcile"},
          {:post, "/api/v1/runs/#{uuid()}/claim"},
          {:post, "/api/v1/runs/#{uuid()}/heartbeat"},
          {:post, "/api/v1/runs/#{uuid()}/state"},
          {:post, "/api/v1/commands/#{uuid()}/ack"},
          {:post, "/api/v1/tasks/#{uuid()}/cancel"},
          {:post, "/api/v1/tasks/#{uuid()}/input"}
        ] do
      legacy = request(conn, method, path)
      assert legacy.status in [404, 405]
      assert %{"error" => %{"code" => "not_found"}} = Jason.decode!(legacy.resp_body)
    end
  end

  defp enroll(conn, name \\ "builder") do
    key = uuid()
    token = "machine-token-#{uuid()}"

    response =
      bearer(conn, @enrollment_token)
      |> put_req_header("idempotency-key", key)
      |> post("/api/v1/machines", %{
        "machine" => %{"name" => name},
        "machine_token" => token
      })
      |> json_response(201)

    {response["machine_id"], response["machine_token"]}
  end

  defp register(conn, machine_id, machine_token) do
    response =
      bearer(conn, machine_token)
      |> put("/api/v1/machines/#{machine_id}/sessions/#{uuid()}", %{
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
      })
      |> json_response(200)

    get_in(response, ["runtimes", Access.at(0), "runtime_id"])
  end

  defp submit_task(conn) do
    bearer(conn, @operator_token)
    |> put_req_header("idempotency-key", "task-#{System.unique_integer([:positive])}")
    |> post("/api/v1/tasks", wire_task_payload())
    |> json_response(201)
    |> Map.fetch!("task_id")
  end

  defp submit_and_assign(conn) do
    task_id = submit_task(conn)
    assert :ok = Scheduler.drain()
    task_id
  end

  defp claim(conn, machine_token, runtime_id) do
    [%{"run_id" => run_id, "generation" => generation}] =
      bearer(conn, machine_token)
      |> get("/api/v1/runtimes/#{runtime_id}/dispatch?runtime_epoch=1")
      |> json_response(200)
      |> Map.fetch!("assignments")

    claim_id = uuid()

    %{"lease_token" => lease_token} =
      bearer(conn, machine_token)
      |> put("/api/v1/runs/#{run_id}/claims/#{claim_id}", %{
        "runtime_id" => runtime_id,
        "runtime_epoch" => 1,
        "generation" => generation
      })
      |> json_response(200)

    {run_id, generation, fence(runtime_id, generation, claim_id, lease_token)}
  end

  defp assert_command(command, task_id, run_id, generation, kind, payload, state) do
    assert MapSet.new(Map.keys(command)) ==
             MapSet.new([
               "command_id",
               "task_id",
               "run_id",
               "generation",
               "kind",
               "payload",
               "state",
               "issued_at",
               "applied_at",
               "acknowledgement_id",
               "acknowledgement_outcome",
               "acknowledged_at"
             ])

    assert %{
             "command_id" => command_id,
             "task_id" => ^task_id,
             "run_id" => ^run_id,
             "generation" => ^generation,
             "kind" => ^kind,
             "payload" => ^payload,
             "state" => ^state,
             "issued_at" => issued_at,
             "applied_at" => applied_at,
             "acknowledgement_id" => acknowledgement_id,
             "acknowledgement_outcome" => acknowledgement_outcome,
             "acknowledged_at" => acknowledged_at
           } = command

    assert is_binary(command_id)
    assert is_binary(issued_at)
    assert applied_at == nil or is_binary(applied_at)
    assert acknowledgement_id == nil or is_binary(acknowledgement_id)
    assert acknowledgement_outcome in [nil, "applied", "rejected", "failed"]
    assert acknowledged_at == nil or is_binary(acknowledged_at)
  end

  defp assert_error(conn, status, code) do
    assert %{"error" => %{"code" => ^code}} = json_response(conn, status)
  end

  defp assert_direct_invalid_request(conn) do
    assert conn.status == 400
    assert %{"error" => %{"code" => "invalid_request"}} = Jason.decode!(conn.resp_body)
  end

  defp machine_conn(machine, path_params, body_params) do
    build_conn()
    |> assign(:machine, machine)
    |> Map.put(:path_params, path_params)
    |> Map.put(:body_params, body_params)
  end

  defp request(conn, :get, path), do: get(conn, path)
  defp request(conn, :post, path), do: post(conn, path, %{})

  defp bearer(conn, token), do: put_req_header(conn, "authorization", "Bearer " <> token)
  defp uuid, do: Ecto.UUID.generate()

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
      "event_id" => uuid(),
      "sequence" => 1,
      "kind" => "progress",
      "occurred_at" => "2026-09-03T00:00:00Z",
      "payload" => %{"message" => "running"}
    }
  end
end
