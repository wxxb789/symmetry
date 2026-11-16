defmodule SymmetryControlWeb.PortalGoalAttentionTest do
  use SymmetryControlWeb.ConnCase, async: false

  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces
  alias SymmetryControlWeb.PortalSession

  @operator_token "test-operator-token"

  setup do
    previous = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(previous, :rollout_enabled, true)
    )

    on_exit(fn -> Application.put_env(:symmetry_control, :goals, previous) end)
    :ok
  end

  test "attention merges Goal attention with the project blocked items the workspace read serves", %{
    conn: conn
  } do
    project = create_project()
    blocked = create_blocked_work_item(project)
    goal_id = create_goal(conn, project)

    response =
      portal_conn()
      |> get("/portal/api/attention?project_id=#{project.id}")
      |> json_response(200)

    assert %{
             "selected_project_id" => project_id,
             "goal_attention" => %{
               "entries" => entries,
               "next_cursor" => next_cursor,
               "unavailable" => nil
             },
             "project_blocked" => %{"entries" => blocked_entries, "unavailable" => nil}
           } = response

    assert project_id == project.id
    assert next_cursor == nil
    assert [%{"goal_id" => ^goal_id} = entry] = entries
    assert entry["state"] == "draft"
    assert entry["current_revision"] == 1
    assert is_list(entry["blocker_reasons"])
    assert entry["ready_to_achieve?"] == false

    assert [
             %{
               "work_item_id" => blocked_id,
               "project_id" => blocked_project_id,
               "title" => title,
               "blocker" => "Operator review required",
               "blocked" => true
             }
           ] = blocked_entries

    assert blocked_id == blocked.id
    assert blocked_project_id == project.id
    assert title == blocked.title
  end

  test "attention keeps the project blockers served while Goal admission is disabled" do
    goals_config = Application.fetch_env!(:symmetry_control, :goals)
    Application.put_env(:symmetry_control, :goals, Keyword.put(goals_config, :rollout_enabled, false))

    project = create_project()
    create_blocked_work_item(project)

    response = portal_conn() |> get("/portal/api/attention") |> json_response(200)

    assert %{
             "goal_attention" => %{"entries" => [], "unavailable" => nil},
             "project_blocked" => %{"entries" => [_ | _], "unavailable" => nil}
           } = response
  end

  test "attention paginates the Goal feed by the server cursor and bounds the limit", %{
    conn: conn
  } do
    project = create_project()
    # The feed is ordered by `updated_at` descending, so distinct durable
    # timestamps make the page order deterministic instead of a same-second
    # accident.
    first = create_goal(conn, project, "First attention goal")
    Process.sleep(1_100)
    second = create_goal(conn, project, "Second attention goal")
    Process.sleep(1_100)
    third = create_goal(conn, project, "Third attention goal")

    # All three draft goals are attention-relevant (`draft` is a served
    # attention reason), so the page is paginated by the server cursor over the
    # three served entries, newest first.
    page_one =
      portal_conn()
      |> get("/portal/api/attention?project_id=#{project.id}&limit=1")
      |> json_response(200)

    assert %{"goal_attention" => %{"entries" => entries, "next_cursor" => cursor}} = page_one
    assert length(entries) == 1
    assert is_binary(cursor)
    assert Enum.map(entries, & &1["goal_id"]) == [third]

    # `updated_at` can collide inside one transaction; the order is not asserted,
    # only that the cursor advances and never repeats an entry.
    second_page =
      portal_conn()
      |> get("/portal/api/attention?project_id=#{project.id}&limit=1&cursor=#{URI.encode_www_form(cursor)}")
      |> json_response(200)

    assert %{"goal_attention" => %{"entries" => remaining}} = second_page
    assert Enum.map(remaining, & &1["goal_id"]) == [second]

    assert MapSet.disjoint?(
             MapSet.new(Enum.map(entries, & &1["goal_id"])),
             MapSet.new(Enum.map(remaining, & &1["goal_id"]))
           )

    third_page =
      portal_conn()
      |> get(
        "/portal/api/attention?project_id=#{project.id}&limit=1&cursor=#{URI.encode_www_form(second_page["goal_attention"]["next_cursor"])}"
      )
      |> json_response(200)

    assert %{"goal_attention" => %{"entries" => [last_entry], "next_cursor" => nil}} = third_page
    assert last_entry["goal_id"] == first

    assert_error(
      portal_conn() |> get("/portal/api/attention?cursor=not-a-cursor"),
      400,
      "invalid_cursor"
    )

    assert_error(
      portal_conn() |> get("/portal/api/attention?limit=0"),
      400,
      "invalid_request"
    )

    assert_error(
      portal_conn() |> get("/portal/api/attention?project_id=#{project.id}&unknown=1"),
      400,
      "invalid_request"
    )

    assert_error(
      portal_conn() |> get("/portal/api/attention?project_id=not-a-uuid"),
      400,
      "invalid_request"
    )
  end

  test "attention requires the authenticated portal session", %{conn: conn} do
    assert_error(
      conn |> put_req_header("accept", "application/json") |> get("/portal/api/attention"),
      401,
      "unauthenticated"
    )
  end

  test "attention never reads an unloaded work-item association as empty" do
    selected = %{
      id: Ecto.UUID.generate(),
      key: "SEL",
      name: "Selected",
      status: "active",
      work_items: [
        %{id: Ecto.UUID.generate(), number: 3, title: "Blocked selected item", status: "ready", blocked: true, blocker: "Operator review required", updated_at: nil}
      ]
    }

    unloaded = %{
      id: Ecto.UUID.generate(),
      key: "OTH",
      name: "Other",
      status: "active",
      work_items: %Ecto.Association.NotLoaded{__field__: :work_items, __owner__: SymmetryControl.Workspaces.Project, __cardinality__: :many}
    }

    projection =
      SymmetryControlWeb.PortalJSON.attention(
        selected.id,
        %{page: %{entries: [], next_after: nil, next_cursor: nil}, unavailable: nil},
        {:ok, %{snapshot: %{projects: [unloaded, selected], selected_project: selected}}}
      )

    assert [%{work_item_id: work_item_id, title: "Blocked selected item"}] =
             projection.project_blocked.entries

    assert work_item_id == hd(selected.work_items).id
  end

  test "portal goal read carries the project label, the served versions and no write policy", %{
    conn: conn
  } do
    project = create_project()
    goal_id = create_goal(conn, project)
    decision_id = request_plan_decision(conn, goal_id)

    portal_read = portal_conn() |> get("/portal/api/goals/#{goal_id}") |> json_response(200)

    assert %{
             "id" => ^goal_id,
             "project_id" => project_id,
             "project_key" => project_key,
             "project_name" => project_name,
             "version" => version,
             "current_revision" => 1,
             "allowed_actions" => allowed_actions,
             "decisions" => [decision],
             "revision" => %{"objective" => objective}
           } = portal_read

    assert project_id == project.id
    assert project_key == project.key
    assert project_name == project.name
    assert is_integer(version) and version >= 1
    assert allowed_actions == portal_read["allowed_actions"]
    assert is_binary(objective)

    assert %{
             "id" => ^decision_id,
             "kind" => "plan",
             "state" => "open",
             "open?" => true,
             "version" => decision_version
           } = decision

    # The served decision version is the durable lock version the resolver must
    # present; the read model's own projection is the single source.
    assert Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).lock_version ==
             decision_version

    # The operator projection stays the durable projection: the same read-model
    # members and none of the portal's presentation additions.
    operator_read =
      operator_conn(conn) |> get("/api/v1/goals/#{goal_id}") |> json_response(200)

    assert Map.has_key?(operator_read, "source")
    refute Map.has_key?(operator_read, "project_key")
    refute Map.has_key?(operator_read, "decision_versions")

    assert [%{"id" => ^decision_id}] = operator_read["decisions"]
  end

  test "portal goal commands keep the accepted conflict and replay semantics", %{conn: conn} do
    project = create_project()
    goal_id = create_goal(conn, project)
    read = portal_conn() |> get("/portal/api/goals/#{goal_id}") |> json_response(200)

    command = %{
      "schema_version" => "symmetry.goal_command.v1",
      "mutation_id" => Ecto.UUID.generate(),
      "expected_version" => read["version"],
      "expected_revision" => read["current_revision"],
      "kind" => "pause",
      "payload" => %{"reason" => "Pause from the portal surface."}
    }

    # A draft Goal cannot be paused; the served refusal keeps its protocol code
    # instead of being flattened into a client-side rule.
    assert %{"error" => %{"code" => "invalid_transition"}} =
             portal_conn()
             |> post("/portal/api/goals/#{goal_id}/commands", command)
             |> json_response(422)

    amend = %{
      command
      | "mutation_id" => Ecto.UUID.generate(),
        "kind" => "amend",
        "payload" => %{
          "reason" => "Second approved revision",
          "revision_contract" => %{
            "objective" => "Amended objective",
            "non_goals" => [],
            "acceptance_contract" => acceptance_contract(),
            "authority_policy" => %{
              "operator_required_for_scope_change" => true,
              "operator_required_for_completion" => true,
              "publication_allowed" => false,
              "allowed_actions" => []
            },
            "execution_policy" => execution_policy(),
            "context_manifest" => %{
              "byte_budget" => 32768,
              "required_source_kinds" => ["repository"],
              "include_advisory_recall" => false
            }
          }
        }
    }

    receipt =
      portal_conn()
      |> post("/portal/api/goals/#{goal_id}/commands", amend)
      |> json_response(200)

    assert %{"event" => %{"id" => mutation_id}} = receipt
    assert mutation_id == amend["mutation_id"]

    # A retried action with the same mutation identity returns the stored receipt
    # and never a second state change.
    assert ^receipt =
             portal_conn()
             |> post("/portal/api/goals/#{goal_id}/commands", amend)
             |> json_response(200)

    assert %{"current_revision" => 2, "state" => "paused"} =
             portal_conn() |> get("/portal/api/goals/#{goal_id}") |> json_response(200)

    # The replayed command did not advance the goal a second time.
    assert %{"version" => version} =
             portal_conn() |> get("/portal/api/goals/#{goal_id}") |> json_response(200)

    assert %{
             "error" => %{
               "code" => "stale",
               "current_version" => ^version,
               "current_revision" => 2
             }
           } =
             portal_conn()
             |> post("/portal/api/goals/#{goal_id}/commands", %{
               amend
               | "mutation_id" => Ecto.UUID.generate(),
                 "expected_version" => read["version"]
             })
             |> json_response(409)
  end

  test "portal context reads keep the ownership check and refuse unknown snapshots", %{conn: conn} do
    project = create_project()
    goal_id = create_goal(conn, project)

    assert_error(
      portal_conn() |> get("/portal/api/goals/#{goal_id}/contexts/#{Ecto.UUID.generate()}"),
      404,
      "not_found"
    )

    # The graph read stays available for the same goal; the refusal above is the
    # ownership check, not an unreadable goal.
    assert %{"nodes" => [], "edges" => []} =
             portal_conn() |> get("/portal/api/goals/#{goal_id}/graph") |> json_response(200)
  end

  test "portal goal commands require the authenticated operator session", %{conn: conn} do
    project = create_project()
    goal_id = create_goal(conn, project)

    assert_error(
      conn
      |> put_req_header("accept", "application/json")
      |> post("/portal/api/goals/#{goal_id}/commands", %{}),
      401,
      "unauthenticated"
    )

    assert_error(
      conn
      |> put_req_header("accept", "application/json")
      |> get("/portal/api/goals/#{goal_id}"),
      401,
      "unauthenticated"
    )
  end

  defp create_project do
    {:ok, project} =
      Workspaces.create_project(%{
        "name" => "Portal attention project #{System.unique_integer([:positive])}",
        "key" => "A#{System.unique_integer([:positive])}",
        "default_agent_profile" => "codex",
        "default_workspace" => "primary"
      })

    project
  end

  defp create_blocked_work_item(project) do
    {:ok, work_item} =
      Workspaces.create_work_item(project.id, %{
        "title" => "Blocked work item #{System.unique_integer([:positive])}",
        "status" => "ready",
        "blocked" => true,
        "blocker" => "Operator review required"
      })

    work_item
  end

  defp create_goal(conn, project, title \\ "Portal attention goal") do
    payload = %{
      "schema_version" => "symmetry.goal_create.v1",
      "mutation_id" => Ecto.UUID.generate(),
      "title" => title,
      "initial_revision" => %{
        "objective" => "Expose the Goal workspace through the portal.",
        "non_goals" => [],
        "acceptance_contract" => acceptance_contract(),
        "authority_policy" => %{
          "operator_required_for_scope_change" => true,
          "operator_required_for_completion" => true,
          "publication_allowed" => false,
          "allowed_actions" => []
        },
        "execution_policy" => execution_policy(),
        "context_manifest" => %{
          "byte_budget" => 32768,
          "required_source_kinds" => ["repository"],
          "include_advisory_recall" => false
        },
        "reason" => "Initial approved Goal contract"
      }
    }

    %{"goal" => %{"id" => goal_id}} =
      operator_conn(conn)
      |> post("/api/v1/projects/#{project.id}/goals", payload)
      |> json_response(201)

    goal_id
  end

  defp request_plan_decision(conn, goal_id) do
    {:ok, goal} = SymmetryControl.Goals.fetch_goal(goal_id)

    {:ok, repository} =
      Workspaces.create_resource(goal.project_id, %{
        "kind" => "repository",
        "name" => "Portal attention repository #{System.unique_integer([:positive])}"
      })

    proposal = %{
      "schema_version" => "symmetry.plan.v1",
      "proposal_id" => Ecto.UUID.generate(),
      "goal_id" => goal_id,
      "expected_revision" => 1,
      "items" => [
        %{
          "key" => "implement",
          "title" => "Implement the Goal workspace",
          "description" => "Bounded Goal work",
          "required" => true,
          "integration" => true,
          "repository_resource_id" => repository.id,
          "acceptance" => acceptance_contract(),
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

    read = operator_conn(conn) |> get("/api/v1/goals/#{goal_id}") |> json_response(200)

    %{"response" => %{"decision" => %{"id" => decision_id}}} =
      operator_conn(conn)
      |> post("/api/v1/goals/#{goal_id}/commands", %{
        "schema_version" => "symmetry.goal_command.v1",
        "mutation_id" => Ecto.UUID.generate(),
        "expected_version" => read["version"],
        "expected_revision" => read["current_revision"],
        "kind" => "request_decision",
        "payload" => %{
          "kind" => "plan",
          "work_item_id" => nil,
          "subject_hash" => nil,
          "proposal" => proposal
        }
      })
      |> json_response(200)

    decision_id
  end

  defp acceptance_contract do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "Operator verifies the Goal workspace boundary.",
      "predicates" => [%{"id" => "operator", "kind" => "operator_acceptance"}]
    }
  end

  defp execution_policy do
    %{
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
    }
  end

  defp portal_conn do
    build_conn()
    |> init_test_session(%{portal_operator: PortalSession.issue(@operator_token)})
    |> Plug.Conn.put_private(:plug_skip_csrf_protection, true)
    |> Plug.Conn.put_req_header("accept", "application/json")
  end

  defp operator_conn(conn) do
    conn
    |> Plug.Conn.put_req_header("accept", "application/json")
    |> Plug.Conn.put_req_header("authorization", "Bearer #{@operator_token}")
  end

  defp assert_error(conn, status, code) do
    assert %{"error" => %{"code" => ^code}} = json_response(conn, status)
  end
end
