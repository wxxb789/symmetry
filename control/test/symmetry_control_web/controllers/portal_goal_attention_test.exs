defmodule SymmetryControlWeb.PortalGoalAttentionTest do
  use SymmetryControlWeb.ConnCase, async: false

  import Ecto.Query

  import SymmetryControlWeb.PortalFixtures,
    only: [
      create_context_project: 2,
      goal_create_payload: 3,
      acceptance_contract: 1,
      authority_policy: 0,
      context_manifest: 0,
      execution_policy: 0
    ]

  import SymmetryControlWeb.ServedDocument, only: [validate_served_document!: 2]
  import SymmetryControlWeb.TestConn, only: [api_conn: 0, operator_conn: 1, assert_error: 3]

  alias SymmetryControl.Goals.{
    ContextSnapshot,
    GoalDecision,
    GoalEvent,
    GoalExternalWait,
    RunEvidence,
    RunUsage,
    WorkOutcome
  }

  alias SymmetryControl.RequestHash
  alias SymmetryControl.Orchestration.{Machine, Run, Runtime, Task}
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.WorkItem

  @acceptance_description "Operator verifies the Goal workspace boundary."

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

  test "attention merges Goal attention with the project blocked items the workspace read serves",
       %{
         conn: conn
       } do
    project = create_project()
    blocked = create_blocked_work_item(project)
    goal_id = create_goal(conn, project)

    response =
      api_conn()
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

  test "attention serves the real Goal feed while Goal admission is disabled", %{conn: conn} do
    goals_config = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(goals_config, :rollout_enabled, false)
    )

    project = create_project()
    create_blocked_work_item(project)
    goal_id = create_goal(conn, project, "Rollout-off attention goal")

    response =
      api_conn() |> get("/portal/api/attention?project_id=#{project.id}") |> json_response(200)

    # The attention read is not gated by Goal admission: with rollout off the
    # page still serves the real Goal entry beside the project blockers and
    # states no unavailable feed. Rollout gates admission commands, not reads.
    assert %{
             "goal_attention" => %{"entries" => [%{"goal_id" => ^goal_id}], "unavailable" => nil},
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
      api_conn()
      |> get("/portal/api/attention?project_id=#{project.id}&limit=1")
      |> json_response(200)

    assert %{"goal_attention" => %{"entries" => entries, "next_cursor" => cursor}} = page_one
    assert is_binary(cursor)
    assert Enum.map(entries, & &1["goal_id"]) == [third]

    second_page =
      api_conn()
      |> get(
        "/portal/api/attention?project_id=#{project.id}&limit=1&cursor=#{URI.encode_www_form(cursor)}"
      )
      |> json_response(200)

    assert %{"goal_attention" => %{"entries" => remaining}} = second_page
    assert Enum.map(remaining, & &1["goal_id"]) == [second]

    third_page =
      api_conn()
      |> get(
        "/portal/api/attention?project_id=#{project.id}&limit=1&cursor=#{URI.encode_www_form(second_page["goal_attention"]["next_cursor"])}"
      )
      |> json_response(200)

    assert %{"goal_attention" => %{"entries" => [last_entry], "next_cursor" => nil}} = third_page
    assert last_entry["goal_id"] == first

    assert_error(
      api_conn() |> get("/portal/api/attention?cursor=not-a-cursor"),
      400,
      "invalid_cursor"
    )

    assert_error(
      api_conn() |> get("/portal/api/attention?limit=0"),
      400,
      "invalid_request"
    )

    assert_error(
      api_conn() |> get("/portal/api/attention?project_id=#{project.id}&unknown=1"),
      400,
      "invalid_request"
    )

    assert_error(
      api_conn() |> get("/portal/api/attention?project_id=not-a-uuid"),
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
        %{
          id: Ecto.UUID.generate(),
          number: 3,
          title: "Blocked selected item",
          status: "ready",
          blocked: true,
          blocker: "Operator review required",
          updated_at: nil
        }
      ]
    }

    unloaded = %{
      id: Ecto.UUID.generate(),
      key: "OTH",
      name: "Other",
      status: "active",
      work_items: %Ecto.Association.NotLoaded{
        __field__: :work_items,
        __owner__: SymmetryControl.Workspaces.Project,
        __cardinality__: :many
      }
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

  test "attention states an unavailable project feed while the Goal feed stays served" do
    # The workspace read serves an empty workspace only while no project exists
    # at all; an existing project is what makes the unknown-project refusal
    # observable. Its blocked item is observable in the same project's own feed
    # below, and must not leak into the unknown project's feed.
    project = create_project()
    blocked = create_blocked_work_item(project)
    unknown_project_id = Ecto.UUID.generate()

    response =
      api_conn()
      |> get("/portal/api/attention?project_id=#{unknown_project_id}")
      |> json_response(200)

    # The project scope does not exist: the workspace read refuses that feed
    # with its own mapped code while the Goal feed is still served (scoped to
    # the same unknown project, so it is empty but not unavailable).
    assert %{
             "goal_attention" => %{"entries" => [], "unavailable" => nil},
             "project_blocked" => %{
               "entries" => [],
               "unavailable" => %{"code" => "not_found", "message" => _message}
             }
           } = response

    # The blocked item is load-bearing: the same project's feed serves it.
    known =
      api_conn()
      |> get("/portal/api/attention?project_id=#{project.id}")
      |> json_response(200)

    assert %{
             "project_blocked" => %{
               "entries" => [%{"work_item_id" => blocked_id, "blocked" => true}],
               "unavailable" => nil
             }
           } = known

    assert blocked_id == blocked.id
  end

  test "an attention cursor whose id is not a UUID is refused as a client error" do
    cursor =
      %{"updated_at" => "2026-11-16T10:00:00Z", "id" => "not-a-uuid"}
      |> Jason.encode!()
      |> Base.url_encode64(padding: false)

    # The cursor decodes but its id is forged client input: like an undecodable
    # cursor it is refused with the protocol's own code instead of surfacing as
    # a server-side query fault.
    assert %{"error" => %{"code" => "invalid_cursor"}} =
             api_conn()
             |> get("/portal/api/attention?cursor=#{cursor}")
             |> json_response(400)
  end

  # The systemic gate: the documents the server actually serves must validate
  # against the contract bundle. A fixture that spells a member differently
  # from the server fails here, on the server's own output, instead of passing
  # review. The scenario exercises a dependency edge, an integration work item,
  # a settlement receipt, an unknown-cost usage leaf, an accepted outcome and a
  # recorded terminal external wait, so the members those documents consume are
  # pinned by the projection itself and not only by the boundary's own document.
  test "the real served attention and goal reads validate against the contract bundle", %{
    conn: conn
  } do
    project = create_project()
    create_blocked_work_item(project)
    goal_id = create_goal(conn, project, "Served document gate")

    items_fun = fn repository_id ->
      [
        plan_item("implement", "Merge the attention feed", repository_id,
          required: true,
          integration: false,
          depends_on_keys: []
        ),
        plan_item("render", "Render the Goal detail surface", repository_id,
          required: true,
          integration: true,
          depends_on_keys: ["implement"]
        ),
        plan_item("external", "Await the external check", repository_id,
          required: false,
          integration: false,
          depends_on_keys: []
        )
      ]
    end

    {decision_id, proposal} = request_plan_decision(conn, goal_id, items_fun)
    approve_plan(conn, goal_id, decision_id, proposal)
    activate(conn, goal_id)

    read = api_conn() |> get("/portal/api/goals/#{goal_id}") |> json_response(200)

    assert read["state"] == "active"
    integration_item = Enum.find(read["work_items"], &(&1["integration"] == true))
    assert integration_item["title"] == "Render the Goal detail surface"
    assert [%{"satisfied?" => false, "blocker" => "waiting_dependency"}] = read["graph"]["edges"]
    assert read["graph"]["cycle_detected?"] == false

    revision = read["current_revision"]
    implement_item = served_work_item!(read, "Merge the attention feed")
    external_item = served_work_item!(read, "Await the external check")

    insert_settlement_with_unknown_usage!(goal_id, revision, integration_item["id"])

    {accepted_subject, accepted_evidence_id} =
      insert_accepted_outcome!(goal_id, revision, implement_item)

    external_ref = insert_unsupported_external_wait!(goal_id, revision, external_item)

    read = api_conn() |> get("/portal/api/goals/#{goal_id}") |> json_response(200)

    assert read["accounting"]["usage_cost_unknown?"] == true
    assert read["accounting"]["usage_cost_microusd"] == nil

    assert [%{"required?" => true, "settlement" => "repair_required"}] =
             read["settlement_attention"]

    subject_hash =
      "sha256:" <> Base.encode16(RequestHash.canonical(accepted_subject), case: :lower)

    assert [
             %{
               "work_item_id" => accepted_item_id,
               "candidate_subject" => candidate_subject,
               "subject_hash" => served_subject_hash,
               "evidence_ids" => [evidence_id],
               "disposition" => "accepted"
             }
           ] = read["accepted_outcomes"]

    assert accepted_item_id == implement_item["id"]
    assert candidate_subject == accepted_subject
    assert served_subject_hash == subject_hash
    assert evidence_id == accepted_evidence_id

    assert [
             %{
               "state" => "unsupported",
               "reason" => "unsupported_external_check",
               "resource_id" => wait_resource_id,
               "external_ref" => wait_external_ref,
               "next_check_at" => nil,
               "check_seq" => 0
             }
           ] = read["external_waits"]

    assert wait_resource_id == external_item["repository_resource_id"]
    assert wait_external_ref == external_ref

    attention =
      api_conn()
      |> get("/portal/api/attention?project_id=#{project.id}")
      |> json_response(200)

    assert [entry] = attention["goal_attention"]["entries"]
    assert entry["goal_id"] == goal_id
    assert entry["ready_to_achieve?"] == false

    assert [%{"required?" => true, "settlement" => "repair_required"}] =
             entry["settlement_attention"]

    validate_served_document!("goal-read", read)
    validate_served_document!("attention-read", attention)
  end

  test "portal goal read carries the project label, the served versions and no write policy", %{
    conn: conn
  } do
    project = create_project()
    goal_id = create_goal(conn, project)
    {decision_id, _proposal} = request_plan_decision(conn, goal_id)

    portal_read = api_conn() |> get("/portal/api/goals/#{goal_id}") |> json_response(200)

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
    # A draft goal whose plan decision is pending acceptance has exactly the
    # durable transition set; the portal projection copies it unchanged.
    assert allowed_actions == ["amend", "cancel"]
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
    assert operator_read["allowed_actions"] == portal_read["allowed_actions"]

    assert [%{"id" => ^decision_id}] = operator_read["decisions"]
  end

  test "portal goal commands keep the accepted conflict and replay semantics", %{conn: conn} do
    project = create_project()
    goal_id = create_goal(conn, project)
    read = api_conn() |> get("/portal/api/goals/#{goal_id}") |> json_response(200)

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
             api_conn()
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
            "acceptance_contract" => acceptance_contract(@acceptance_description),
            "authority_policy" => authority_policy(),
            "execution_policy" => execution_policy(),
            "context_manifest" => context_manifest()
          }
        }
    }

    receipt =
      api_conn()
      |> post("/portal/api/goals/#{goal_id}/commands", amend)
      |> json_response(200)

    assert %{"event" => %{"id" => mutation_id}} = receipt
    assert mutation_id == amend["mutation_id"]

    # A retried action with the same mutation identity returns the stored receipt
    # and never a second state change.
    assert ^receipt =
             api_conn()
             |> post("/portal/api/goals/#{goal_id}/commands", amend)
             |> json_response(200)

    assert %{"current_revision" => 2, "state" => "paused"} =
             api_conn() |> get("/portal/api/goals/#{goal_id}") |> json_response(200)

    # The replayed command did not advance the goal a second time.
    assert %{"version" => version} =
             api_conn() |> get("/portal/api/goals/#{goal_id}") |> json_response(200)

    assert %{
             "error" => %{
               "code" => "stale",
               "current_version" => ^version,
               "current_revision" => 2
             }
           } =
             api_conn()
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
      api_conn() |> get("/portal/api/goals/#{goal_id}/contexts/#{Ecto.UUID.generate()}"),
      404,
      "not_found"
    )

    # The graph read stays available for the same goal; the refusal above is the
    # ownership check, not an unreadable goal.
    assert %{"nodes" => [], "edges" => []} =
             api_conn() |> get("/portal/api/goals/#{goal_id}/graph") |> json_response(200)
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
    create_context_project("Portal attention project", "A")
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
    payload =
      goal_create_payload(
        title,
        "Expose the Goal workspace through the portal.",
        @acceptance_description
      )

    %{"goal" => %{"id" => goal_id}} =
      operator_conn(conn)
      |> post("/api/v1/projects/#{project.id}/goals", payload)
      |> json_response(201)

    goal_id
  end

  defp request_plan_decision(conn, goal_id, items_fun \\ &single_integration_items/1) do
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
      "items" => items_fun.(repository.id)
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

    {decision_id, proposal}
  end

  defp single_integration_items(repository_id) do
    [
      plan_item("implement", "Implement the Goal workspace", repository_id,
        required: true,
        integration: true,
        depends_on_keys: []
      )
    ]
  end

  defp plan_item(key, title, repository_id, opts) do
    %{
      "key" => key,
      "title" => title,
      "description" => "Bounded Goal work",
      "required" => Keyword.fetch!(opts, :required),
      "integration" => Keyword.fetch!(opts, :integration),
      "repository_resource_id" => repository_id,
      "acceptance" => acceptance_contract(@acceptance_description),
      "depends_on_keys" => Keyword.fetch!(opts, :depends_on_keys),
      "model_profile" => "codex",
      "change_target" => nil,
      "baseline" => %{
        "kind" => "subject",
        "subject" => %{
          "resource_id" => repository_id,
          "commit" => String.duplicate("a", 40),
          "tree_digest" => "sha256:" <> String.duplicate("b", 64)
        }
      }
    }
  end

  defp approve_plan(conn, goal_id, decision_id, proposal) do
    decision_version = Repo.get!(GoalDecision, decision_id).lock_version

    portal_command!(conn, goal_id, "resolve_decision", %{
      "decision_id" => decision_id,
      "expected_decision_version" => decision_version,
      "option_id" => "accept",
      "comment" => nil
    })

    portal_command!(conn, goal_id, "accept_plan", %{
      "proposal" => proposal,
      "proposal_hash" =>
        "sha256:" <> Base.encode16(SymmetryControl.RequestHash.canonical(proposal), case: :lower),
      "decision_id" => decision_id
    })
  end

  defp activate(conn, goal_id),
    do: portal_command!(conn, goal_id, "activate", %{"approved_revision" => 1})

  defp portal_command!(conn, goal_id, kind, payload) do
    read = api_conn() |> get("/portal/api/goals/#{goal_id}") |> json_response(200)

    api_conn()
    |> post("/portal/api/goals/#{goal_id}/commands", %{
      "schema_version" => "symmetry.goal_command.v1",
      "mutation_id" => Ecto.UUID.generate(),
      "expected_version" => read["version"],
      "expected_revision" => read["current_revision"],
      "kind" => kind,
      "payload" => payload
    })
    |> json_response(200)
  end

  defp served_work_item!(read, title) do
    Enum.find(read["work_items"], &(&1["title"] == title)) ||
      flunk("the served goal read must carry the work item #{inspect(title)}")
  end

  # One admitted work item's durable task, bound the way admission binds it: the
  # gate rows below all hang off a task the projection follows through the item.
  defp insert_goal_task!(goal_id, revision, work_item_id, state) do
    snapshot =
      %ContextSnapshot{}
      |> ContextSnapshot.changeset(%{
        goal_id: goal_id,
        goal_revision: revision,
        work_item_id: work_item_id,
        schema_version: 1,
        content_hash: :crypto.hash(:sha256, "served-document-gate-context"),
        payload: %{"sources" => []}
      })
      |> Repo.insert!()

    task =
      %Task{}
      |> Task.changeset(%{
        idempotency_key: "served-gate-task-#{System.unique_integer([:positive])}",
        request_hash: :crypto.hash(:sha256, "served-document-gate"),
        goal_id: goal_id,
        goal_revision: revision,
        work_item_id: work_item_id,
        context_snapshot_id: snapshot.id,
        admission_key: Ecto.UUID.generate(),
        allowed_runtime_ids: [],
        max_run_attempts: 1,
        goal: "Served document gate work",
        agent_profile: "codex",
        workspace: "primary",
        required_capabilities: %{},
        input: %{},
        state: state,
        current_generation: 1,
        attempt_generation: 1,
        purpose: "implement"
      })
      |> Repo.insert!()

    Repo.update_all(
      from(item in WorkItem, where: item.id == ^work_item_id),
      set: [orchestration_task_id: task.id]
    )

    task
  end

  defp insert_goal_run!(task, state, result \\ nil) do
    now = DateTime.utc_now()
    runtime = insert_runtime!()

    %Run{}
    |> Run.changeset(%{
      task_id: task.id,
      runtime_id: runtime.id,
      generation: 1,
      state: state,
      assigned_at: now,
      assignment_expires_at: DateTime.add(now, 60, :second),
      result: result
    })
    |> Repo.insert!()
  end

  # One `task_settled` goal event with the next sequence. The read model only
  # turns an event into a settlement entry when its payload and response agree on
  # the goal revision and task/run/generation identity and the payload's `state`
  # equals the run's, so the payload carries that state while the response is
  # persisted verbatim (the production response itself has no `state` member).
  defp insert_goal_event!(goal_id, revision, state, response) do
    sequence =
      Repo.aggregate(from(event in GoalEvent, where: event.goal_id == ^goal_id), :max, :sequence)

    %GoalEvent{}
    |> GoalEvent.changeset(%{
      id: Ecto.UUID.generate(),
      goal_id: goal_id,
      sequence: (sequence || 0) + 1,
      kind: "task_settled",
      actor_ref: "system:settlement",
      request_hash: :crypto.hash(:sha256, "served-document-gate-#{response["task_id"]}"),
      revision: revision,
      payload: %{
        "task_id" => response["task_id"],
        "run_id" => response["run_id"],
        "generation" => response["generation"],
        "state" => state,
        "goal_revision" => revision
      },
      response: response
    })
    |> Repo.insert!()
  end

  # A durable task/run/settlement receipt with one unknown-cost usage leaf over
  # an admitted work item: the served projection must state `required?` and
  # `usage_cost_unknown?` for the document gate. The rows follow the durable
  # shapes the settlement writer produces (payload/response agreement included).
  defp insert_settlement_with_unknown_usage!(goal_id, revision, work_item_id) do
    task = insert_goal_task!(goal_id, revision, work_item_id, "failed")
    run = insert_goal_run!(task, "failed")

    %RunUsage{}
    |> RunUsage.changeset(%{
      run_id: run.id,
      usage_key: "served-gate-usage",
      provider: "codex",
      model: "codex",
      cost_basis: "unknown",
      cost_microusd: nil,
      input_tokens: 10,
      output_tokens: 5
    })
    |> Repo.insert!()

    insert_goal_event!(
      goal_id,
      revision,
      run.state,
      %{
        "goal_id" => goal_id,
        "goal_revision" => revision,
        "task_id" => task.id,
        "run_id" => run.id,
        "generation" => run.generation,
        "settlement" => "repair_required",
        "reason" => "task failed before a verified result",
        "proposed_next_action" => "Run the bound repair",
        "proposed_next_action_status" => "proposal_only",
        "evidence_refs" => []
      }
    )
  end

  # An accepted outcome over an admitted work item: the served projection must
  # carry the consumed outcome members (`candidate_subject`, `subject_hash`,
  # `evidence_ids`) for the document gate. The row carries the columns
  # `Goals.insert_validation_outcome!/13` writes, over a completed producer run
  # and a bound evidence row; the acceptance pipeline's validate task and its
  # run are not built here, so `validation_task_id` stays null and the gate
  # mints the producing result id the writer takes from the producer run.
  defp insert_accepted_outcome!(goal_id, revision, item) do
    subject = item["baseline"]["subject"]
    subject_hash = RequestHash.canonical(subject)

    task = insert_goal_task!(goal_id, revision, item["id"], "completed")
    run = insert_goal_run!(task, "completed")

    evidence =
      %RunEvidence{}
      |> RunEvidence.changeset(%{
        run_id: run.id,
        evidence_key: "served-gate-evidence-#{System.unique_integer([:positive])}",
        kind: "check",
        subject_hash: subject_hash,
        source_ref: %{"kind" => "validator", "profile" => "served_document_gate"},
        source_revision: subject["commit"],
        validator_profile: "served_document_gate",
        verdict: "passed",
        payload: %{"summary" => "The bound validation profile passed."}
      })
      |> Repo.insert!()

    %WorkOutcome{}
    |> WorkOutcome.changeset(%{
      goal_id: goal_id,
      goal_revision: revision,
      work_item_id: item["id"],
      producing_task_id: task.id,
      producing_run_id: run.id,
      candidate_subject: subject,
      subject_hash: subject_hash,
      producing_result_id: Ecto.UUID.generate(),
      evidence_ids: [evidence.id],
      disposition: "accepted",
      reason: "validation_passed",
      decision_id: nil
    })
    |> Repo.insert!()

    {subject, evidence.id}
  end

  # A recorded terminal external wait over the item whose completed run reported
  # a blocked-external result: the served projection must carry `state`,
  # `reason`, `external_ref`, `next_check_at` and `check_seq` for the document
  # gate. The row follows what `Goals.create_external_wait!/8` writes, and the
  # identity trigger requires exactly this shape (completed task and run, the
  # run's `task_result` equal to the wait's `result`, the item's repository
  # resource in the subject), so the row cannot be invented more loosely.
  defp insert_unsupported_external_wait!(goal_id, revision, item) do
    subject = item["baseline"]["subject"]
    subject_hash = RequestHash.canonical(subject)
    result_id = Ecto.UUID.generate()
    external_ref = "ci://build/unsupported"

    blocked_result = %{
      "schema_version" => "symmetry.task_result.v1",
      "result_id" => result_id,
      "kind" => "blocked",
      "summary" => "The bound external check is unsupported.",
      "subject" => subject,
      "subject_hash" => "sha256:" <> Base.encode16(subject_hash, case: :lower),
      "evidence_refs" => [],
      "blocker" => %{
        "kind" => "external",
        "resource_id" => item["repository_resource_id"],
        "external_ref" => external_ref,
        "next_check_at" => nil
      },
      "proposed_next_action" => nil,
      "proposal" => nil,
      "reason" => nil,
      "diagnostics" => []
    }

    task = insert_goal_task!(goal_id, revision, item["id"], "completed")
    run = insert_goal_run!(task, "completed", %{"task_result" => blocked_result})

    event =
      insert_goal_event!(
        goal_id,
        revision,
        run.state,
        %{
          "goal_id" => goal_id,
          "goal_revision" => revision,
          "task_id" => task.id,
          "run_id" => run.id,
          "generation" => run.generation,
          "settlement" => "blocked",
          "result_id" => result_id,
          "result_kind" => "blocked",
          "reason" => "unsupported_external_check",
          "blocker" => blocked_result["blocker"],
          "decision_id" => nil,
          "proposed_next_action" => nil,
          "proposed_next_action_status" => nil,
          "subject_hash" => blocked_result["subject_hash"],
          "evidence_refs" => []
        }
      )

    %GoalExternalWait{id: Ecto.UUID.generate()}
    |> GoalExternalWait.changeset(%{
      goal_id: goal_id,
      goal_revision: revision,
      work_item_id: item["id"],
      task_id: task.id,
      run_id: run.id,
      run_generation: run.generation,
      source_ref: %{
        "kind" => "task_result",
        "task_id" => task.id,
        "run_id" => run.id,
        "generation" => run.generation,
        "result_id" => result_id
      },
      result_id: result_id,
      result: blocked_result,
      resource_id: item["repository_resource_id"],
      external_ref: external_ref,
      subject: subject,
      subject_hash: subject_hash,
      next_check_at: nil,
      state: "unsupported",
      receipt_event_id: event.id
    })
    |> Repo.insert!()

    external_ref
  end

  defp insert_runtime! do
    machine =
      %Machine{}
      |> Machine.changeset(%{
        name: "served-gate-machine-#{System.unique_integer([:positive])}",
        token_digest: :crypto.strong_rand_bytes(32)
      })
      |> Repo.insert!()

    %Runtime{}
    |> Runtime.changeset(%{
      machine_id: machine.id,
      runtime_key: "served-gate-runtime-#{System.unique_integer([:positive])}",
      name: "Served document gate runtime",
      daemon_instance_id: Ecto.UUID.generate(),
      connection_epoch: 1,
      capacity: 1,
      agent_profile: "codex",
      workspace: "primary",
      capabilities: %{},
      harness_kind: "codex",
      harness_version: "1.0.0",
      adapter_version: "1.0.0",
      adapter_protocol_version: 1,
      status: "online",
      heartbeat_interval_ms: 5_000
    })
    |> Repo.insert!()
  end
end
