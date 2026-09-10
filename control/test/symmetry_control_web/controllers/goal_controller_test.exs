defmodule SymmetryControlWeb.GoalControllerTest do
  use SymmetryControlWeb.ConnCase, async: false

  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces
  alias SymmetryControlWeb.PortalSession
  alias SymmetryControlWeb.Protocol

  @enrollment_token "test-enrollment-token"
  @operator_token "test-operator-token"

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

    assert "activate" in allowed_actions
    assert is_list(blocker_reasons)

    assert_error(
      operator_conn(conn)
      |> post("/api/v1/projects/#{project.id}/goals", %{payload | "title" => "Different goal"}),
      409,
      "idempotency_conflict"
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
               command
               | "mutation_id" => Ecto.UUID.generate(),
                 "expected_version" => goal["version"]
             })
             |> json_response(409)

    assert current_version > goal["version"]
    assert "pause" in allowed_actions

    assert_error(
      operator_conn(conn)
      |> post("/api/v1/goals/#{goal_id}/commands", %{command | "kind" => "pause"}),
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
      portal_conn()
      |> post("/portal/api/projects/#{project.id}/goals", goal_payload())
      |> json_response(201)

    assert %{"goal" => %{"id" => goal_id}} = created

    assert %{"id" => ^goal_id} =
             portal_conn()
             |> get("/portal/api/goals/#{goal_id}")
             |> json_response(200)
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
          "integration" => false,
          "repository_resource_id" => repository.id,
          "acceptance" => acceptance_contract(),
          "depends_on_keys" => [],
          "model_profile" => "codex",
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
    goal =
      operator_conn(conn)
      |> get("/api/v1/goals/#{goal_id}")
      |> json_response(200)

    operator_conn(conn)
    |> post("/api/v1/goals/#{goal_id}/commands", %{
      "schema_version" => "symmetry.goal_command.v1",
      "mutation_id" => Ecto.UUID.generate(),
      "expected_version" => goal["version"],
      "expected_revision" => goal["current_revision"],
      "kind" => kind,
      "payload" => payload
    })
    |> json_response(200)
  end

  defp create_project do
    {:ok, project} =
      SymmetryControl.Workspaces.create_project(%{
        "name" => "Goal API project #{System.unique_integer([:positive])}",
        "key" => "G#{System.unique_integer([:positive])}",
        "default_agent_profile" => "codex",
        "default_workspace" => "primary"
      })

    project
  end

  defp goal_payload do
    %{
      "schema_version" => "symmetry.goal_create.v1",
      "title" => "Validate durable Goal API",
      "mutation_id" => Ecto.UUID.generate(),
      "initial_revision" => %{
        "objective" => "Expose the durable Goal API through authenticated operator routes.",
        "non_goals" => [],
        "acceptance_contract" => acceptance_contract(),
        "authority_policy" => %{
          "operator_required_for_scope_change" => true,
          "operator_required_for_completion" => true,
          "publication_allowed" => false,
          "allowed_actions" => []
        },
        "execution_policy" => %{
          "automatic_execution" => false,
          "max_parallel_tasks" => 1,
          "max_task_admissions" => 2,
          "max_run_attempts_per_task" => 2,
          "budget_limit_microusd" => nil,
          "per_run_cost_limit_microusd" => nil,
          "budget_mode" => "soft",
          "hard_cost_limit_required" => false,
          "allowed_runtime_ids" => [],
          "allowed_model_profiles" => [],
          "final_acceptance" => "operator",
          "allowed_actions" => [],
          "allowed_resource_ids" => []
        },
        "context_manifest" => %{
          "byte_budget" => 32768,
          "required_source_kinds" => ["repository"],
          "include_advisory_recall" => false
        },
        "reason" => "Initial approved Goal contract"
      }
    }
  end

  defp acceptance_contract do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "Operator verifies the Goal API boundary.",
      "predicates" => [%{"id" => "operator", "kind" => "operator_acceptance"}]
    }
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

  defp operator_conn(conn), do: bearer(conn, @operator_token)

  defp portal_conn do
    Phoenix.ConnTest.build_conn()
    |> init_test_session(%{portal_operator: PortalSession.issue(@operator_token)})
    |> skip_csrf()
    |> put_req_header("accept", "application/json")
  end

  defp bearer(conn, token), do: put_req_header(conn, "authorization", "Bearer " <> token)
  defp skip_csrf(conn), do: put_private(conn, :plug_skip_csrf_protection, true)

  defp assert_error(conn, status, code) do
    assert %{"error" => %{"code" => ^code}} = json_response(conn, status)
  end
end
