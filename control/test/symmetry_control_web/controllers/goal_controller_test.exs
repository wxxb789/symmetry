defmodule SymmetryControlWeb.GoalControllerTest do
  use SymmetryControlWeb.ConnCase, async: false

  import SymmetryControlWeb.PortalFixtures,
    only: [acceptance_contract: 1, create_context_project: 2, goal_create_payload: 3]

  import SymmetryControlWeb.TestConn,
    only: [api_conn: 0, operator_conn: 1, assert_error: 3, bearer: 2]

  import Ecto.Query

  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces
  alias SymmetryControlWeb.Protocol

  @enrollment_token "test-enrollment-token"

  test "operator creates and reads a goal, and creation replays by mutation identity", %{
    conn: conn
  } do
    project = create_project()
    payload = goal_payload()

    created =
      operator_conn(conn)
      |> post("/api/v1/projects/#{project.id}/goals", payload)
      |> json_response(201)

    assert %{"goal" => %{"id" => goal_id, "state" => "draft", "current_revision" => 1}} = created

    assert ^created =
             operator_conn(conn)
             |> post("/api/v1/projects/#{project.id}/goals", payload)
             |> json_response(200)

    assert %{
             "id" => ^goal_id,
             "allowed_actions" => allowed_actions,
             "blocker_reasons" => blocker_reasons
           } =
             operator_conn(conn)
             |> get("/api/v1/goals/#{goal_id}")
             |> json_response(200)

    assert allowed_actions == ["request_plan", "amend", "cancel"]
    assert is_list(blocker_reasons)

    assert_error(
      operator_conn(conn)
      |> post("/api/v1/projects/#{project.id}/goals", %{payload | "title" => "Different goal"}),
      409,
      "idempotency_conflict"
    )
  end

  test "Goal creation rejects duplicate JSON members before controller parsing", %{conn: conn} do
    project = create_project()

    body =
      goal_payload()
      |> Jason.encode!()
      |> String.replace(
        ~s("title":"Validate durable Goal API"),
        ~s("title":"first","title":"second")
      )

    assert_error(
      operator_conn(conn)
      |> put_req_header("content-type", "application/json")
      |> post("/api/v1/projects/#{project.id}/goals", body),
      400,
      "invalid_request"
    )
  end

  test "goal commands replay before stale preconditions and expose current state", %{conn: conn} do
    goal_id = create_goal_with_accepted_plan(conn)

    goal =
      operator_conn(conn)
      |> get("/api/v1/goals/#{goal_id}")
      |> json_response(200)

    command = %{
      "schema_version" => "symmetry.goal_command.v1",
      "mutation_id" => Ecto.UUID.generate(),
      "expected_version" => goal["version"],
      "expected_revision" => goal["current_revision"],
      "kind" => "activate",
      "payload" => %{"approved_revision" => goal["current_revision"]}
    }

    receipt =
      operator_conn(conn)
      |> post("/api/v1/goals/#{goal_id}/commands", command)
      |> json_response(200)

    assert %{"goal_id" => ^goal_id, "mutation_id" => mutation_id} = receipt
    assert mutation_id == command["mutation_id"]

    assert ^receipt =
             operator_conn(conn)
             |> post("/api/v1/goals/#{goal_id}/commands", command)
             |> json_response(200)

    assert %{
             "error" => %{
               "code" => "stale",
               "current_version" => current_version,
               "current_revision" => 1,
               "allowed_actions" => allowed_actions
             }
           } =
             operator_conn(conn)
             |> post("/api/v1/goals/#{goal_id}/commands", %{
               "schema_version" => "symmetry.goal_command.v1",
               "mutation_id" => Ecto.UUID.generate(),
               "expected_version" => goal["version"],
               "expected_revision" => goal["current_revision"],
               "kind" => "pause",
               "payload" => %{"reason" => "Verify stale Goal version handling."}
             })
             |> json_response(409)

    assert current_version > goal["version"]
    assert "pause" in allowed_actions

    assert_error(
      operator_conn(conn)
      |> post("/api/v1/goals/#{goal_id}/commands", %{
        command
        | "kind" => "pause",
          "payload" => %{"reason" => "Verify mutation identity conflicts."}
      }),
      409,
      "idempotency_conflict"
    )
  end

  test "goal events, graph, context, and attention preserve read authorization and unavailable resources",
       %{
         conn: conn
       } do
    goal_id = create_goal(conn)

    assert %{"entries" => events, "next_after" => _} =
             operator_conn(conn)
             |> get("/api/v1/goals/#{goal_id}/events?after=0")
             |> json_response(200)

    assert Enum.all?(events, &(Map.has_key?(&1, "sequence") and Map.has_key?(&1, "kind")))

    assert %{"nodes" => nodes, "edges" => edges} =
             operator_conn(conn)
             |> get("/api/v1/goals/#{goal_id}/graph")
             |> json_response(200)

    assert is_list(nodes)
    assert is_list(edges)

    assert_error(
      operator_conn(conn) |> get("/api/v1/goals/#{goal_id}/contexts/#{Ecto.UUID.generate()}"),
      404,
      "not_found"
    )

    assert %{"entries" => attention, "next_after" => _} =
             operator_conn(conn)
             |> get("/api/v1/attention")
             |> json_response(200)

    assert Enum.any?(attention, &(&1["goal_id"] == goal_id))

    assert_error(get(conn, "/api/v1/goals/#{goal_id}"), 401, "unauthenticated")

    {_machine_id, machine_token} = enroll(conn)

    for path <- [
          "/api/v1/goals/#{goal_id}",
          "/api/v1/goals/#{goal_id}/events",
          "/api/v1/goals/#{goal_id}/graph",
          "/api/v1/goals/#{goal_id}/contexts/#{Ecto.UUID.generate()}",
          "/api/v1/attention"
        ] do
      assert_error(bearer(conn, machine_token) |> get(path), 403, "forbidden")
    end

    assert_error(
      bearer(conn, machine_token) |> post("/api/v1/goals/#{goal_id}/commands", %{}),
      403,
      "forbidden"
    )
  end

  test "goal events serve the after/limit window, cap the page and refuse unknown parameters", %{
    conn: conn
  } do
    goal_id = create_goal_with_accepted_plan(conn)

    # `after` is the last rendered sequence and `limit` the served page size:
    # one entry per page here, paging strictly forward.
    assert %{"entries" => [first], "next_after" => next_after} =
             operator_conn(conn)
             |> get("/api/v1/goals/#{goal_id}/events?limit=1")
             |> json_response(200)

    assert first["sequence"] == 1
    assert next_after == 1

    assert %{"entries" => [second]} =
             operator_conn(conn)
             |> get("/api/v1/goals/#{goal_id}/events?after=#{next_after}&limit=1")
             |> json_response(200)

    assert second["sequence"] > next_after

    # The read model's own maximum (100) is served; anything beyond it and any
    # parameter the read does not define is caller input, not a served window.
    assert %{"entries" => entries} =
             operator_conn(conn)
             |> get("/api/v1/goals/#{goal_id}/events?after=0&limit=100")
             |> json_response(200)

    assert entries != []

    for query <- ["limit=101", "after=-1", "after=not-a-sequence", "unknown=1", "limit=0"] do
      assert_error(
        operator_conn(conn) |> get("/api/v1/goals/#{goal_id}/events?#{query}"),
        400,
        "invalid_request"
      )
    end
  end

  test "portal routes share the Goal command implementation and retain portal authentication", %{
    conn: conn
  } do
    project = create_project()

    assert_error(
      conn
      |> put_req_header("accept", "application/json")
      |> get("/portal/api/attention"),
      401,
      "unauthenticated"
    )

    created =
      api_conn()
      |> post("/portal/api/projects/#{project.id}/goals", goal_payload())
      |> json_response(201)

    assert %{"goal" => %{"id" => goal_id}} = created

    assert %{"id" => ^goal_id} =
             api_conn()
             |> get("/portal/api/goals/#{goal_id}")
             |> json_response(200)
  end

  test "portal Goal creation rejects escaped duplicate JSON members", %{} do
    project = create_project()

    body =
      goal_payload()
      |> Jason.encode!()
      |> String.replace(
        ~s("title":"Validate durable Goal API"),
        ~S("title":"first","\u0074itle":"second")
      )

    assert_error(
      api_conn()
      |> put_req_header("content-type", "application/json")
      |> post("/portal/api/projects/#{project.id}/goals", body),
      400,
      "invalid_request"
    )
  end

  test "the Goal surface decides the durable actor and the served projection", %{conn: conn} do
    project = create_project()
    initial_revision = goal_payload()["initial_revision"]
    portal_conn = api_conn()
    operator = operator_conn(conn)

    %{"goal" => %{"id" => portal_goal_id}} =
      portal_conn
      |> post("/portal/api/projects/#{project.id}/goals", goal_payload())
      |> json_response(201)

    %{"goal" => %{"id" => operator_goal_id}} =
      operator
      |> post("/api/v1/projects/#{project.id}/goals", goal_payload())
      |> json_response(201)

    assert revision_actor(portal_goal_id, 1) == "portal_operator"
    assert revision_actor(operator_goal_id, 1) == "operator"

    assert %{"event" => _} =
             amend_goal(portal_conn, "/portal/api", portal_goal_id, initial_revision)
             |> json_response(200)

    assert %{"event" => _} =
             amend_goal(operator, "/api/v1", operator_goal_id, initial_revision)
             |> json_response(200)

    # The durable command rows carry the same actor the create did.
    assert event_actor(portal_goal_id) == "portal_operator"
    assert event_actor(operator_goal_id) == "operator"
    assert revision_actor(portal_goal_id, 2) == "portal_operator"
    assert revision_actor(operator_goal_id, 2) == "operator"

    # The portal read adds the project label the surface renders; the operator
    # API never serves that portal projection.
    portal_read = portal_conn |> get("/portal/api/goals/#{portal_goal_id}") |> json_response(200)
    assert Map.has_key?(portal_read, "project_key")
    assert Map.has_key?(portal_read, "project_name")

    operator_read =
      operator |> get("/api/v1/goals/#{operator_goal_id}") |> json_response(200)

    assert Map.has_key?(operator_read, "source")
    refute Map.has_key?(operator_read, "project_key")
    refute Map.has_key?(operator_read, "project_name")
  end

  test "an encoded portal path keeps the portal surface the router matched" do
    project = create_project()
    initial_revision = goal_payload()["initial_revision"]
    portal_conn = api_conn()

    %{"goal" => %{"id" => goal_id}} =
      portal_conn
      |> post("/portal/api/projects/#{project.id}/goals", goal_payload())
      |> json_response(201)

    # The router matches decoded path segments (`phoenix/router.ex` decodes
    # `path_info`), so `/%70ortal/api/...` is the portal route even though
    # `conn.request_path` keeps the encoded text. The surface follows the
    # matched pipeline, so the projection and the durable actor are the
    # portal's for this spelling too.
    read =
      portal_conn
      |> get("/%70ortal/api/goals/#{goal_id}")
      |> json_response(200)

    assert Map.has_key?(read, "project_key")
    assert Map.has_key?(read, "project_name")

    assert %{"event" => _} =
             amend_goal(portal_conn, "/%70ortal/api", goal_id, initial_revision)
             |> json_response(200)

    assert revision_actor(goal_id, 2) == "portal_operator"
    assert event_actor(goal_id) == "portal_operator"
  end

  test "no Goal route 500s on its pipeline", %{conn: conn} do
    project = create_project()
    initial_revision = goal_payload()["initial_revision"]

    for {conn_at_surface, prefix} <- [
          {api_conn(), "/portal/api"},
          {operator_conn(conn), "/api/v1"}
        ] do
      goal_id =
        conn_at_surface
        |> post("#{prefix}/projects/#{project.id}/goals", goal_payload())
        |> json_response(201)
        |> get_in(["goal", "id"])

      for path <- [
            "/goals/#{goal_id}",
            "/goals/#{goal_id}/events",
            "/goals/#{goal_id}/graph"
          ] do
        assert json_response(get(conn_at_surface, "#{prefix}#{path}"), 200)
      end

      assert_error(
        get(conn_at_surface, "#{prefix}/goals/#{goal_id}/contexts/#{Ecto.UUID.generate()}"),
        404,
        "not_found"
      )

      assert %{"event" => _} =
               amend_goal(conn_at_surface, prefix, goal_id, initial_revision)
               |> json_response(200)
    end

    assert %{"entries" => _} =
             operator_conn(conn) |> get("/api/v1/attention") |> json_response(200)
  end

  test "goal integer reads accept exactly the documented forms", %{conn: conn} do
    goal_id = create_goal(conn)
    operator = operator_conn(conn)
    events = "/api/v1/goals/#{goal_id}/events"

    # `after` is the last rendered sequence: non-negative, at most the durable
    # bigint maximum, and only as decimal text — `Integer.parse/1` keeps the
    # explicit `+` sign the table below accepts.
    for {value, status} <- [
          {"0", 200},
          {"+1", 200},
          {" 1", 400},
          {"", 400},
          {"-1", 400},
          {"1.5", 400},
          {"9223372036854775807", 200},
          {"9223372036854775808", 400}
        ] do
      assert_goal_parameter(operator, events, "after", value, status)
    end

    # Two bare members keep the last; an array member is not a sequence.
    after_one = json_response(get(operator, "#{events}?after=1"), 200)

    assert json_response(get(operator, "#{events}?after=0&after=1"), 200) == after_one
    refute json_response(get(operator, "#{events}?after=0"), 200) == after_one

    assert_error(
      get(operator, "#{events}?after[]=0&after[]=1"),
      400,
      "invalid_request"
    )

    # The event page is capped at the read model's own 100 even though the
    # parameter itself accepts any positive integer.
    for {value, status} <- [
          {"1", 200},
          {"+1", 200},
          {" 1", 400},
          {"", 400},
          {"0", 400},
          {"-1", 400},
          {"1.5", 400},
          {"100", 200},
          {"101", 400},
          {"9223372036854775807", 400},
          {"9223372036854775808", 400}
        ] do
      assert_goal_parameter(operator, events, "limit", value, status)
    end

    # The attention read has no page cap, so it serves the positive forms the
    # event read refuses for size.
    for {value, status} <- [
          {"1", 200},
          {"+1", 200},
          {" 1", 400},
          {"", 400},
          {"0", 400},
          {"-1", 400},
          {"1.5", 400},
          {"101", 200},
          {"9223372036854775808", 200}
        ] do
      assert_goal_parameter(operator, "/api/v1/attention", "limit", value, status)
    end
  end

  test "one validation envelope serves every refused changeset" do
    changeset =
      %Workspaces.Project{}
      |> Ecto.Changeset.change(%{})
      |> Ecto.Changeset.add_error(:key, "can't be blank")

    response = Protocol.validation_error(build_conn(), changeset)

    assert response.status == 422

    assert %{
             "error" => %{
               "code" => "validation_failed",
               "message" => "request contains invalid fields",
               "fields" => %{"key" => ["can't be blank"]}
             }
           } = Jason.decode!(response.resp_body)
  end

  test "Goal failures retain their protocol status and error code" do
    for {reason, status, code} <- [
          {:goal_authority_required, 403, "goal_authority_required"},
          {:dependency_cycle, 422, "dependency_cycle"},
          {:budget_exhausted, 422, "budget_exhausted"},
          {:budget_unknown, 422, "budget_unknown"},
          {:stale_run, 409, "stale_run"},
          {:goal_admission_disabled, 409, "goal_admission_disabled"},
          {:requested_session_not_found, 404, "requested_session_not_found"},
          {:requested_session_unavailable, 409, "requested_session_unavailable"},
          {:decision_expired, 409, "decision_expired"},
          {:invalid_cursor, 400, "invalid_cursor"},
          {:missing_evidence, 422, "missing_evidence"},
          {:admitted_work_required, 422, "admitted_work_required"},
          {:integration_outcome_required, 422, "integration_outcome_required"},
          {:context_budget_exceeded, 422, "context_budget_exceeded"},
          {:required_context_source_missing, 422, "required_context_source_missing"},
          {:waiting_dependency, 422, "waiting_dependency"},
          {:invalid_evidence_identity, 422, "invalid_evidence_identity"},
          {:invalid_subject, 422, "invalid_subject"},
          {:invalid_context_snapshot, 422, "invalid_context_snapshot"},
          {:unsupported_capability, 422, "unsupported_capability"},
          {:requested_session_required, 422, "requested_session_required"}
        ] do
      response = Protocol.error(build_conn(), reason)

      assert response.status == status
      assert %{"error" => %{"code" => ^code}} = Jason.decode!(response.resp_body)
    end
  end

  test "invalid contract details remain typed and do not expose validator internals" do
    response =
      Protocol.error(
        build_conn(),
        {:invalid_contract, {:validation_failed, [%{"path" => "/token", "message" => "secret"}]}}
      )

    assert response.status == 422

    body = Jason.decode!(response.resp_body)

    assert %{
             "error" => %{
               "code" => "invalid_contract",
               "message" => "goal contract is invalid"
             }
           } = body

    refute Map.has_key?(body["error"], "details")
    refute Jason.encode!(body) =~ "secret"
  end

  defp create_goal(conn) do
    project = create_project()

    operator_conn(conn)
    |> post("/api/v1/projects/#{project.id}/goals", goal_payload())
    |> json_response(201)
    |> get_in(["goal", "id"])
  end

  defp create_goal_with_accepted_plan(conn) do
    enable_goal_rollout()
    project = create_project()

    {:ok, repository} =
      Workspaces.create_resource(project.id, %{
        "kind" => "repository",
        "name" => "Goal API repository #{System.unique_integer([:positive])}"
      })

    %{"goal" => %{"id" => goal_id}} =
      operator_conn(conn)
      |> post("/api/v1/projects/#{project.id}/goals", goal_payload())
      |> json_response(201)

    proposal = %{
      "schema_version" => "symmetry.plan.v1",
      "proposal_id" => Ecto.UUID.generate(),
      "goal_id" => goal_id,
      "expected_revision" => 1,
      "items" => [
        %{
          "key" => "implement",
          "title" => "Implement Goal API",
          "description" => "Bounded Goal work",
          "required" => true,
          "integration" => true,
          "repository_resource_id" => repository.id,
          "acceptance" => acceptance_contract("Operator verifies the Goal API boundary."),
          "depends_on_keys" => [],
          "model_profile" => "codex",
          "change_target" => nil,
          "baseline" => %{
            "kind" => "subject",
            "subject" => %{
              "resource_id" => repository.id,
              "commit" => String.duplicate("a", 40),
              "tree_digest" => "sha256:" <> String.duplicate("b", 64)
            }
          }
        }
      ]
    }

    %{"response" => %{"decision" => %{"id" => decision_id}}} =
      post_goal_command(conn, goal_id, "request_decision", %{
        "kind" => "plan",
        "work_item_id" => nil,
        "subject_hash" => nil,
        "proposal" => proposal
      })

    decision_version = Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).lock_version

    %{"response" => %{"decision" => %{"state" => "resolved"}}} =
      post_goal_command(conn, goal_id, "resolve_decision", %{
        "decision_id" => decision_id,
        "expected_decision_version" => decision_version,
        "option_id" => "accept"
      })

    %{"response" => %{"plan" => %{"decision_id" => ^decision_id}}} =
      post_goal_command(conn, goal_id, "accept_plan", %{
        "proposal" => proposal,
        "proposal_hash" =>
          "sha256:" <> Base.encode16(RequestHash.canonical(proposal), case: :lower),
        "decision_id" => decision_id
      })

    goal_id
  end

  defp post_goal_command(conn, goal_id, kind, payload) do
    operator_conn(conn)
    |> goal_command("/api/v1", goal_id, kind, payload)
    |> json_response(200)
  end

  defp create_project, do: create_context_project("Goal API project", "G")

  defp amend_goal(conn, prefix, goal_id, initial_revision) do
    goal_command(conn, prefix, goal_id, "amend", %{
      "reason" => "Amend the contract from the surface under test",
      "revision_contract" => Map.delete(initial_revision, "reason")
    })
  end

  # The one goal-command envelope both helpers above send: read the goal's
  # current version/revision on the surface under test, then post the command.
  defp goal_command(conn, prefix, goal_id, kind, payload) do
    goal = conn |> get("#{prefix}/goals/#{goal_id}") |> json_response(200)

    post(conn, "#{prefix}/goals/#{goal_id}/commands", %{
      "schema_version" => "symmetry.goal_command.v1",
      "mutation_id" => Ecto.UUID.generate(),
      "expected_version" => goal["version"],
      "expected_revision" => goal["current_revision"],
      "kind" => kind,
      "payload" => payload
    })
  end

  defp assert_goal_parameter(conn, path, key, value, status) do
    response = get(conn, "#{path}?#{URI.encode_query(%{key => value})}")

    if status == 200 do
      assert json_response(response, 200)
    else
      assert_error(response, status, "invalid_request")
    end
  end

  defp revision_actor(goal_id, revision) do
    Repo.get_by!(SymmetryControl.Goals.GoalRevision, goal_id: goal_id, revision: revision).actor_ref
  end

  defp event_actor(goal_id) do
    Repo.one!(
      from(event in SymmetryControl.Goals.GoalEvent,
        where: event.goal_id == ^goal_id,
        order_by: [desc: event.sequence],
        limit: 1
      )
    ).actor_ref
  end

  defp enable_goal_rollout do
    goals_config = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(goals_config, :rollout_enabled, true)
    )

    on_exit(fn -> Application.put_env(:symmetry_control, :goals, goals_config) end)
  end

  defp goal_payload do
    goal_create_payload(
      "Validate durable Goal API",
      "Expose the durable Goal API through authenticated operator routes.",
      "Operator verifies the Goal API boundary."
    )
  end

  defp enroll(conn) do
    token = "goal-machine-#{Ecto.UUID.generate()}"

    response =
      bearer(conn, @enrollment_token)
      |> put_req_header("idempotency-key", Ecto.UUID.generate())
      |> post("/api/v1/machines", %{
        "machine" => %{"name" => "goal-api-machine"},
        "machine_token" => token
      })
      |> json_response(201)

    {response["machine_id"], response["machine_token"]}
  end
end
