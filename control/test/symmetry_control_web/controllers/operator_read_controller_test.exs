defmodule SymmetryControlWeb.OperatorReadControllerTest do
  use SymmetryControlWeb.ConnCase, async: false

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Command, RunEvent, RunTransition}
  alias SymmetryControl.Repo

  @operator_token "test-operator-token"
  @now ~U[2026-09-03 00:00:00.000000Z]

  test "operator runtime resources expose machine identity, status, capacity, and active runs", %{
    conn: conn
  } do
    %{machine: machine, runtime: runtime, task: task, run: run} = runtime_fixture()

    assert %{"runtimes" => [snapshot]} =
             bearer(conn)
             |> get("/api/v1/runtimes")
             |> json_response(200)

    assert_runtime(snapshot, machine, runtime, task, run)

    assert ^snapshot =
             bearer(conn)
             |> get("/api/v1/runtimes/#{runtime.id}")
             |> json_response(200)

    assert_error(bearer(conn) |> get("/api/v1/runtimes/#{uuid()}"), 404, "not_found")
  end

  test "task resource includes nullable waiting context and stable latest command states", %{
    conn: conn
  } do
    %{task: task, run: run, fence: fence} = waiting_fixture()

    assert %{
             "waiting" => %{
               "run_id" => run_id,
               "generation" => generation,
               "transition_id" => _,
               "question" => "Which branch?",
               "payload" => %{"question" => "Which branch?"},
               "recorded_at" => recorded_at
             },
             "latest_command" => nil
           } =
             bearer(conn)
             |> get("/api/v1/tasks/#{task.id}")
             |> json_response(200)

    assert run_id == run.id
    assert generation == run.generation
    assert is_binary(recorded_at)

    pending = create_input_command(task.id, %{"branch" => "main"}, "pending")

    assert %{"waiting" => waiting, "latest_command" => latest} =
             bearer(conn)
             |> get("/api/v1/tasks/#{task.id}")
             |> json_response(200)

    assert waiting["question"] == "Which branch?"
    assert_command(latest, pending, "pending")

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "running",
               %{},
               uuid(),
               now: DateTime.add(@now, 1, :microsecond)
             )

    assert {:ok, _} =
             Orchestration.acknowledge_command(
               pending.id,
               fence,
               "applied",
               uuid(),
               now: DateTime.add(@now, 2, :microsecond)
             )

    assert %{"latest_command" => acknowledged} =
             bearer(conn)
             |> get("/api/v1/tasks/#{task.id}")
             |> json_response(200)

    assert_command(acknowledged, pending, "acknowledged")

    {:ok, queued, :created} =
      Orchestration.submit_task(task_attrs(goal: "runless"), "applied-command", now: @now)

    assert {:ok, _cancelled, applied} = Orchestration.request_cancel(queued.id, now: @now)

    assert %{"waiting" => nil, "latest_command" => applied_command} =
             bearer(conn)
             |> get("/api/v1/tasks/#{queued.id}")
             |> json_response(200)

    assert_command(applied_command, applied, "applied")
    assert_error(bearer(conn) |> get("/api/v1/tasks/#{uuid()}"), 404, "not_found")
  end

  test "operator history collections use ascending after cursors, a default limit, and a maximum limit",
       %{
         conn: conn
       } do
    %{task: task, first_run: first_run, second_run: second_run} = history_fixture()
    at = DateTime.add(@now, 1, :second)

    event_ids = insert_events(first_run, second_run, at, 501)

    assert %{"events" => first_page, "next_after" => after_cursor} =
             bearer(conn)
             |> get("/api/v1/tasks/#{task.id}/events")
             |> json_response(200)

    assert length(first_page) == 100
    assert is_binary(after_cursor)

    assert %{"events" => second_page, "next_after" => nil} =
             bearer(conn)
             |> get(
               "/api/v1/tasks/#{task.id}/events?after=#{URI.encode_www_form(after_cursor)}&limit=500"
             )
             |> json_response(200)

    assert Enum.map(first_page ++ second_page, & &1["event_id"]) ==
             event_ids

    transition = insert_transition(first_run, "running", DateTime.add(at, 102, :microsecond))
    command = insert_command(task, second_run, "pending", DateTime.add(at, 103, :microsecond))

    assert %{"transitions" => [transition_dto], "next_after" => nil} =
             bearer(conn)
             |> get("/api/v1/tasks/#{task.id}/transitions?limit=500")
             |> json_response(200)

    assert transition_dto["transition_id"] == transition.transition_id

    assert %{"commands" => [command_dto], "next_after" => nil} =
             bearer(conn)
             |> get("/api/v1/tasks/#{task.id}/commands?limit=500")
             |> json_response(200)

    assert command_dto["command_id"] == command.id

    for path <- [
          "/api/v1/tasks/#{task.id}/events?limit=0",
          "/api/v1/tasks/#{task.id}/events?limit=501",
          "/api/v1/tasks/#{task.id}/events?limit=invalid",
          "/api/v1/tasks/#{task.id}/events?before=not-allowed"
        ] do
      assert_error(bearer(conn) |> get(path), 400, "invalid_request")
    end
  end

  test "task timeline is newest-first across generations and only accepts before cursors", %{
    conn: conn
  } do
    %{task: task, first_run: first_run, second_run: second_run} = history_fixture()
    at = DateTime.add(@now, 1, :second)

    older_event = insert_event(first_run, 1, at)
    transition = insert_transition(first_run, "running", DateTime.add(at, 1, :microsecond))

    newest_command =
      insert_command(task, second_run, "pending", DateTime.add(at, 2, :microsecond))

    assert %{"items" => first_page, "next_before" => before_cursor} =
             bearer(conn)
             |> get("/api/v1/tasks/#{task.id}/timeline?limit=2")
             |> json_response(200)

    assert Enum.map(first_page, & &1["source"]) == ["command", "transition"]

    assert Enum.map(first_page, & &1["generation"]) == [
             second_run.generation,
             first_run.generation
           ]

    assert first_page |> hd() |> get_in(["data", "command_id"]) == newest_command.id

    assert first_page |> List.last() |> get_in(["data", "transition_id"]) ==
             transition.transition_id

    assert is_binary(before_cursor)

    assert %{"items" => second_page, "next_before" => nil} =
             bearer(conn)
             |> get(
               "/api/v1/tasks/#{task.id}/timeline?before=#{URI.encode_www_form(before_cursor)}"
             )
             |> json_response(200)

    assert [%{"source" => "event", "data" => %{"event_id" => event_id}}] = second_page
    assert event_id == older_event.event_id

    for path <- [
          "/api/v1/tasks/#{task.id}/timeline?after=not-allowed",
          "/api/v1/tasks/#{task.id}/timeline?limit=0",
          "/api/v1/tasks/#{task.id}/timeline?limit=501",
          "/api/v1/tasks/#{task.id}/timeline?limit=invalid"
        ] do
      assert_error(bearer(conn) |> get(path), 400, "invalid_request")
    end
  end

  test "history cursors reject tampering and every scope mismatch while ignoring additive query params",
       %{
         conn: conn
       } do
    %{task: task, first_run: first_run, second_run: second_run} = history_fixture()
    at = DateTime.add(@now, 1, :second)

    insert_event(first_run, 1, at)
    insert_event(second_run, 2, DateTime.add(at, 1, :microsecond))
    insert_transition(first_run, "running", DateTime.add(at, 2, :microsecond))
    insert_transition(second_run, "running", DateTime.add(at, 3, :microsecond))
    insert_command(task, second_run, "pending", DateTime.add(at, 4, :microsecond))

    %{"next_after" => event_cursor} =
      bearer(conn)
      |> get("/api/v1/tasks/#{task.id}/events?limit=1&ignored=value")
      |> json_response(200)

    %{"next_after" => transition_cursor} =
      bearer(conn)
      |> get("/api/v1/tasks/#{task.id}/transitions?limit=1")
      |> json_response(200)

    %{"next_before" => timeline_cursor} =
      bearer(conn)
      |> get("/api/v1/tasks/#{task.id}/timeline?limit=1")
      |> json_response(200)

    other_task_id = create_task("cursor-other")

    valid_cursor_payload = %{
      "v" => 1,
      "task_id" => task.id,
      "surface" => "events",
      "direction" => "after",
      "inserted_at" => DateTime.to_iso8601(@now),
      "id" => uuid()
    }

    unknown_version =
      Phoenix.Token.sign(
        SymmetryControlWeb.Endpoint,
        "task-history-v1",
        Map.put(valid_cursor_payload, "v", 2)
      )

    invalid_field_type =
      Phoenix.Token.sign(
        SymmetryControlWeb.Endpoint,
        "task-history-v1",
        Map.put(valid_cursor_payload, "inserted_at", 1)
      )

    expired_cursor =
      Phoenix.Token.sign(
        SymmetryControlWeb.Endpoint,
        "task-history-v1",
        valid_cursor_payload,
        signed_at: System.system_time(:second) - 86_401
      )

    tampered = event_cursor <> "x"

    for path <- [
          "/api/v1/tasks/#{task.id}/events?after=#{URI.encode_www_form(tampered)}",
          "/api/v1/tasks/#{other_task_id}/events?after=#{URI.encode_www_form(event_cursor)}",
          "/api/v1/tasks/#{task.id}/transitions?after=#{URI.encode_www_form(event_cursor)}",
          "/api/v1/tasks/#{task.id}/events?before=#{URI.encode_www_form(event_cursor)}",
          "/api/v1/tasks/#{task.id}/events?after=#{URI.encode_www_form(timeline_cursor)}",
          "/api/v1/tasks/#{task.id}/timeline?after=#{URI.encode_www_form(timeline_cursor)}",
          "/api/v1/tasks/#{task.id}/timeline?before=#{URI.encode_www_form(event_cursor)}",
          "/api/v1/tasks/#{task.id}/events?after=#{URI.encode_www_form(unknown_version)}",
          "/api/v1/tasks/#{task.id}/events?after=#{URI.encode_www_form(invalid_field_type)}",
          "/api/v1/tasks/#{task.id}/events?after=#{URI.encode_www_form(expired_cursor)}",
          "/api/v1/tasks/#{task.id}/timeline?before=#{URI.encode_www_form(transition_cursor)}"
        ] do
      assert_error(bearer(conn) |> get(path), 400, "invalid_request")
    end

    for surface <- ["timeline", "events", "transitions", "commands"] do
      assert_error(bearer(conn) |> get("/api/v1/tasks/#{uuid()}/#{surface}"), 404, "not_found")
    end
  end

  defp runtime_fixture do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, capacity: 2)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "runtime-read", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    %{machine: machine, runtime: runtime, task: task, run: run}
  end

  defp waiting_fixture do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "waiting-read", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)

    assert {:ok, _} = Orchestration.transition(run.id, fence, "running", %{}, uuid(), now: @now)

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "waiting_for_input",
               %{"question" => "Which branch?"},
               uuid(),
               now: DateTime.add(@now, 1, :microsecond)
             )

    %{task: task, run: run, fence: fence}
  end

  defp history_fixture do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, capacity: 2)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "history-read", now: @now)
    {:ok, first_run} = Orchestration.assign_one(now: @now)

    assert %{expired_runs: 1} = Orchestration.expire(now: DateTime.add(@now, 31, :second))

    assert {:ok, _} =
             Orchestration.heartbeat(runtime.id, runtime.connection_epoch, [],
               now: DateTime.add(@now, 31, :second)
             )

    assert {:ok, second_run} = Orchestration.assign_one(now: DateTime.add(@now, 31, :second))
    %{task: task, first_run: first_run, second_run: second_run}
  end

  defp create_input_command(task_id, payload, suffix) do
    assert {:ok, command, :created} =
             Orchestration.create_command(
               task_id,
               "provide_input",
               payload,
               "command-#{suffix}",
               now: @now
             )

    command
  end

  defp create_task(key) do
    assert {:ok, task, :created} = Orchestration.submit_task(task_attrs(), key, now: @now)
    task.id
  end

  defp enroll_machine do
    assert {:ok, enrolled} =
             Orchestration.enroll_machine(
               %{name: "operator-read-machine-#{System.unique_integer([:positive])}"},
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret"
             )

    enrolled
  end

  defp register_runtime(machine, overrides \\ []) do
    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               uuid(),
               [
                 %{
                   runtime_key: "default",
                   name: "Local Codex",
                   capacity: Keyword.get(overrides, :capacity, 1),
                   agent_profile: "codex",
                   workspace: "primary",
                   capabilities: %{}
                 }
               ],
               now: @now
             )

    runtime
  end

  defp claim(run, runtime) do
    assert {:ok, claimed} =
             Orchestration.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: uuid()
               },
               now: @now
             )

    %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: claimed.claim_id,
      lease_token: claimed.lease_token
    }
  end

  defp insert_event(run, sequence, inserted_at) do
    event_id = uuid()

    %RunEvent{}
    |> RunEvent.changeset(%{
      run_id: run.id,
      event_id: event_id,
      request_hash: :crypto.hash(:sha256, event_id),
      sequence: sequence,
      kind: "progress",
      payload: %{"sequence" => sequence},
      occurred_at: DateTime.add(inserted_at, -1, :second)
    })
    |> Ecto.Changeset.change(inserted_at: inserted_at, updated_at: inserted_at)
    |> Repo.insert!()
  end

  defp insert_events(first_run, second_run, at, count) do
    events =
      for sequence <- 1..count do
        event_id = uuid()
        inserted_at = DateTime.add(at, sequence, :microsecond)

        %{
          id: uuid(),
          run_id: if(rem(sequence, 2) == 0, do: second_run.id, else: first_run.id),
          event_id: event_id,
          request_hash: :crypto.hash(:sha256, event_id),
          sequence: sequence,
          kind: "progress",
          payload: %{"sequence" => sequence},
          occurred_at: DateTime.add(inserted_at, -1, :second),
          inserted_at: inserted_at,
          updated_at: inserted_at
        }
      end

    assert {^count, nil} = Repo.insert_all(RunEvent, events)
    Enum.map(events, & &1.event_id)
  end

  defp insert_transition(run, state, inserted_at) do
    transition_id = uuid()

    %RunTransition{}
    |> RunTransition.changeset(%{
      run_id: run.id,
      transition_id: transition_id,
      request_hash: :crypto.hash(:sha256, transition_id),
      state: state,
      payload: %{"state" => state}
    })
    |> Ecto.Changeset.change(inserted_at: inserted_at, updated_at: inserted_at)
    |> Repo.insert!()
  end

  defp insert_command(task, run, state, inserted_at) do
    idempotency_key = uuid()

    %Command{}
    |> Command.changeset(%{
      task_id: task.id,
      run_id: run.id,
      generation: run.generation,
      kind: "cancel",
      payload: %{},
      idempotency_key: idempotency_key,
      request_hash: :crypto.hash(:sha256, idempotency_key),
      state: state
    })
    |> Ecto.Changeset.change(inserted_at: inserted_at, updated_at: inserted_at)
    |> Repo.insert!()
  end

  defp task_attrs(overrides \\ []) do
    Map.merge(
      %{goal: "Run tests", agent_profile: "codex", workspace: "primary", input: %{}},
      Map.new(overrides)
    )
  end

  defp assert_runtime(snapshot, machine, runtime, task, run) do
    assert %{
             "machine_id" => machine_id,
             "machine_name" => machine_name,
             "runtime_id" => runtime_id,
             "runtime_key" => runtime_key,
             "runtime_name" => runtime_name,
             "status" => "online",
             "last_heartbeat_at" => last_heartbeat_at,
             "connection_epoch" => connection_epoch,
             "capacity" => 2,
             "reserved_capacity" => 1,
             "active_runs" => [active_run]
           } = snapshot

    assert machine_id == machine.id
    assert machine_name == machine.name
    assert runtime_id == runtime.id
    assert runtime_key == runtime.runtime_key
    assert runtime_name == runtime.name
    assert connection_epoch == runtime.connection_epoch
    assert is_binary(last_heartbeat_at)
    assert active_run["run_id"] == run.id
    assert active_run["task_id"] == task.id
    assert active_run["generation"] == run.generation
    assert active_run["state"] == "assigned"
    assert is_binary(active_run["recorded_at"])
  end

  defp assert_command(dto, command, state) do
    assert %{
             "command_id" => command_id,
             "task_id" => task_id,
             "run_id" => run_id,
             "generation" => generation,
             "kind" => kind,
             "payload" => payload,
             "state" => ^state,
             "issued_at" => issued_at,
             "applied_at" => _,
             "acknowledgement_id" => _,
             "acknowledgement_outcome" => _,
             "acknowledged_at" => _
           } = dto

    assert command_id == command.id
    assert task_id == command.task_id
    assert run_id == command.run_id
    assert generation == command.generation
    assert kind == command.kind
    assert payload == command.payload
    assert is_binary(issued_at)
  end

  defp assert_error(conn, status, code) do
    assert %{"error" => %{"code" => ^code}} = json_response(conn, status)
  end

  defp bearer(conn), do: put_req_header(conn, "authorization", "Bearer " <> @operator_token)
  defp uuid, do: Ecto.UUID.generate()
end
