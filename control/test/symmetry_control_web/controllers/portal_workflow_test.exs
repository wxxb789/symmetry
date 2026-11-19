defmodule SymmetryControlWeb.PortalWorkflowTest do
  use SymmetryControlWeb.ConnCase, async: false

  import Ecto.Query

  alias SymmetryControl.Goals
  alias SymmetryControl.{Orchestration, Repo}
  alias SymmetryControl.Orchestration.RunTransition
  alias SymmetryControl.Workspaces.WorkItem
  alias SymmetryControlWeb.PortalSession

  @now ~U[2026-09-04 11:00:00.000000Z]

  test "project settings and resource lifecycle reject stale browser writes" do
    project = create_project("SYM")

    assert %{"project" => updated_project} =
             api_conn()
             |> patch("/portal/api/projects/#{project["id"]}", %{
               "version" => project["version"],
               "name" => "Symmetry Platform",
               "description" => "Daily engineering workspace",
               "default_agent_profile" => "codex",
               "default_workspace" => "repository"
             })
             |> json_response(200)

    assert updated_project["name"] == "Symmetry Platform"
    assert updated_project["key"] == "SYM"
    assert updated_project["version"] == project["version"] + 1

    assert %{"error" => %{"code" => "stale"}} =
             api_conn()
             |> patch("/portal/api/projects/#{project["id"]}", %{
               "version" => project["version"],
               "name" => "Old browser"
             })
             |> json_response(409)

    assert %{"error" => %{"code" => "validation_failed", "fields" => %{"key" => [_]}}} =
             api_conn()
             |> patch("/portal/api/projects/#{project["id"]}", %{
               "version" => updated_project["version"],
               "key" => "NEW"
             })
             |> json_response(422)

    assert %{"resource" => resource} =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/resources", %{
               "kind" => "ci",
               "name" => "GitHub Actions",
               "provider" => "github",
               "status" => "healthy",
               "sync_status" => "stale",
               "status_message" => "Webhook is delayed"
             })
             |> json_response(201)

    assert resource["version"] == 1
    assert resource["sync_status"] == "stale"

    assert %{"resource" => updated_resource} =
             api_conn()
             |> patch("/portal/api/resources/#{resource["id"]}", %{
               "version" => resource["version"],
               "sync_status" => "synced",
               "status_message" => ""
             })
             |> json_response(200)

    assert updated_resource["sync_status"] == "synced"
    assert updated_resource["status_message"] == nil

    assert %{"deleted_resource_id" => resource_id} =
             api_conn()
             |> delete("/portal/api/resources/#{resource["id"]}", %{
               "version" => updated_resource["version"]
             })
             |> json_response(200)

    assert resource_id == resource["id"]
  end

  test "resource APIs expose and enforce registered runtime identities" do
    project = create_project("RID")
    key = Ecto.UUID.generate()

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(
               %{name: "resource-catalog", machine_token: "token-#{key}"},
               key,
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret",
               now: @now
             )

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               enrolled.machine.id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: "catalog-runtime",
                   name: "Catalog runtime",
                   capacity: 1,
                   agent_profile: "catalog-agent",
                   workspace: "other-workspace",
                   capabilities: %{}
                 }
               ],
               now: @now
             )

    assert %{"registered_runtimes" => registered, "runtimes" => []} =
             api_conn()
             |> get("/portal/api/workspace?project_id=#{project["id"]}")
             |> json_response(200)

    assert Enum.any?(registered, &(&1["runtime_id"] == runtime.id))

    assert %{"resource" => %{"external_ref" => runtime_id}} =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/resources", %{
               "kind" => "runtime",
               "name" => "Registered runtime",
               "external_ref" => runtime.id,
               "status" => "healthy",
               "sync_status" => "synced"
             })
             |> json_response(201)

    assert runtime_id == runtime.id

    assert %{"resource" => %{"external_ref" => "catalog-agent"}} =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/resources", %{
               "kind" => "agent",
               "name" => "Registered agent",
               "external_ref" => "catalog-agent",
               "status" => "healthy",
               "sync_status" => "synced"
             })
             |> json_response(201)

    assert %{
             "error" => %{
               "code" => "validation_failed",
               "fields" => %{"external_ref" => [_message]}
             }
           } =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/resources", %{
               "kind" => "runtime",
               "name" => "Forged runtime",
               "external_ref" => Ecto.UUID.generate(),
               "status" => "unknown",
               "sync_status" => "unknown"
             })
             |> json_response(422)
  end

  test "workspace reads serve the runtime capability row verbatim" do
    project = create_project("RTM")
    key = Ecto.UUID.generate()

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(
               %{name: "capability-host", machine_token: "token-#{key}"},
               key,
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret",
               now: @now
             )

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               enrolled.machine.id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: "capability-runtime",
                   name: "Capability runtime",
                   capacity: 2,
                   agent_profile: "codex",
                   workspace: "primary",
                   capabilities: %{"structured_input" => true, "provider_access" => true}
                 }
               ],
               now: @now
             )

    assert %{"runtimes" => [row], "registered_runtimes" => [registered]} =
             api_conn()
             |> get("/portal/api/workspace?project_id=#{project["id"]}")
             |> json_response(200)

    assert row["runtime_id"] == runtime.id
    assert row["machine_id"] == enrolled.machine.id
    assert row["machine_name"] == "capability-host"
    assert row["runtime_key"] == "capability-runtime"
    assert row["runtime_name"] == "Capability runtime"
    assert row["status"] == "online"
    assert row["last_heartbeat_at"] == "2026-09-04T11:00:00.000000Z"
    assert row["connection_epoch"] == runtime.connection_epoch
    assert row["capacity"] == 2
    assert row["reserved_capacity"] == 0
    assert row["agent_profile"] == "codex"
    assert row["workspace"] == "primary"
    assert row["repository_resource_id"] == nil
    assert row["capabilities"] == %{"structured_input" => true, "provider_access" => true}
    assert row["active_runs"] == []
    assert registered["capabilities"] == row["capabilities"]
  end

  test "Goal-managed work items serve goal_id and refuse every legacy control path" do
    enable_goal_rollout()

    project = create_project("GOV")

    assert %{"resource" => %{"id" => repository_id}} =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/resources", %{
               "kind" => "repository",
               "name" => "Goal repository",
               "status" => "healthy"
             })
             |> json_response(201)

    goal_managed = goal_managed_work_item!(project["id"], repository_id)

    legacy =
      create_work_item(project["id"], "Legacy work", %{
        "assignee_type" => "agent",
        "agent_profile" => "codex"
      })

    assert %{
             "work_item" => %{
               "id" => work_item_id,
               "goal_id" => served_goal_id,
               "version" => goal_managed_version
             }
           } =
             api_conn()
             |> get("/portal/api/work-items/#{goal_managed.id}")
             |> json_response(200)

    assert work_item_id == goal_managed.id
    assert served_goal_id == goal_managed.goal_id
    refute is_nil(served_goal_id)

    for {path, body} <- [
          {"run", %{"action_id" => "goal-start"}},
          {"cancel", %{"action_id" => "goal-cancel", "generation" => 1}},
          {"retry", %{"action_id" => "goal-retry", "generation" => 1}},
          {"move", %{"version" => goal_managed_version, "status" => "review"}}
        ] do
      response =
        if path == "move" do
          api_conn() |> patch("/portal/api/work-items/#{goal_managed.id}/move", body)
        else
          api_conn() |> post("/portal/api/work-items/#{goal_managed.id}/#{path}", body)
        end

      assert %{"error" => %{"code" => "goal_authority_required"}} = json_response(response, 403)
    end

    assert %{"disposition" => "created", "work_item" => launched} =
             api_conn()
             |> post("/portal/api/work-items/#{legacy["id"]}/run", %{
               "action_id" => "legacy-start"
             })
             |> json_response(202)

    assert launched["status"] == "in_progress"

    assert %{"work_item" => %{"status" => "review"}} =
             api_conn()
             |> patch("/portal/api/work-items/#{legacy["id"]}/move", %{
               "version" => launched["version"],
               "status" => "review"
             })
             |> json_response(200)
  end

  test "archived projects can be restored through the routed API" do
    project = create_project("ARC")

    assert %{"project" => archived} =
             api_conn()
             |> patch("/portal/api/projects/#{project["id"]}", %{
               "version" => project["version"],
               "status" => "archived"
             })
             |> json_response(200)

    assert archived["status"] == "archived"

    assert %{"project" => restored} =
             api_conn()
             |> patch("/portal/api/projects/#{project["id"]}", %{
               "version" => archived["version"],
               "status" => "active"
             })
             |> json_response(200)

    assert restored["status"] == "active"
  end

  test "complete work-item edits and ordered moves persist through workspace reload" do
    project = create_project("KAN")
    first = create_work_item(project["id"], "First")
    second = create_work_item(project["id"], "Second")
    _third = create_work_item(project["id"], "Third")

    assert %{"work_item" => edited} =
             api_conn()
             |> patch("/portal/api/work-items/#{second["id"]}", %{
               "version" => second["version"],
               "title" => "Second edited",
               "description" => "Complete editor coverage",
               "priority" => "urgent",
               "assignee_type" => "human",
               "assignee_name" => "Lina",
               "workspace" => "primary",
               "repository" => "acme/symmetry",
               "branch" => "codex/goal-02",
               "pull_request_url" => "https://github.com/acme/symmetry/pull/42",
               "ci_status" => "passed",
               "review_status" => "approved",
               "blocked" => false
             })
             |> json_response(200)

    assert edited["title"] == "Second edited"

    assert edited["assignee"] == %{
             "type" => "human",
             "name" => "Lina",
             "agent_profile" => nil
           }

    assert %{"work_item" => moved} =
             api_conn()
             |> patch("/portal/api/work-items/#{second["id"]}/move", %{
               "version" => edited["version"],
               "status" => "ready",
               "before_id" => first["id"]
             })
             |> json_response(200)

    assert moved["status"] == "ready"

    assert %{"projects" => [workspace_project]} =
             api_conn()
             |> get("/portal/api/workspace?project_id=#{project["id"]}")
             |> json_response(200)

    assert Enum.map(workspace_project["work_items"], & &1["title"]) == [
             "Second edited",
             "First",
             "Third"
           ]

    assert %{"error" => %{"code" => "stale"}} =
             api_conn()
             |> patch("/portal/api/work-items/#{second["id"]}", %{
               "version" => edited["version"],
               "title" => "Lost update"
             })
             |> json_response(409)
  end

  test "portal mutation endpoints enforce CSRF when a signed session is present", %{conn: conn} do
    login_page = conn |> enforce_csrf() |> get("/portal/login")
    body = html_response(login_page, 200)
    [_, csrf_token] = Regex.run(~r/name="_csrf_token" value="([^"]+)"/, body)

    authenticated =
      login_page
      |> recycle()
      |> enforce_csrf()
      |> post("/portal/login", %{
        "_csrf_token" => csrf_token,
        "operator_token" => "test-operator-token"
      })

    assert_error_sent 403, fn ->
      authenticated
      |> recycle()
      |> enforce_csrf()
      |> put_req_header("accept", "application/json")
      |> post("/portal/api/projects", %{"name" => "Rejected", "key" => "NO"})
    end
  end

  test "cancel and retry actions are idempotent without losing task history" do
    project = create_project("RUN")

    item =
      create_work_item(project["id"], "Retry this work", %{
        "assignee_type" => "agent",
        "agent_profile" => "codex"
      })

    assert %{"execution" => %{"task_id" => task_id, "generation" => 1}, "work_item" => launched} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/run", %{"action_id" => "start-test"})
             |> json_response(202)

    assert %{"command" => first_cancel} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/cancel", %{
               "action_id" => "cancel-action-1",
               "generation" => 1
             })
             |> json_response(202)

    assert %{"command" => replayed_cancel} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/cancel", %{
               "action_id" => "cancel-action-1",
               "generation" => 1
             })
             |> json_response(200)

    assert replayed_cancel["command_id"] == first_cancel["command_id"]

    assert %{"work_item" => edited} =
             api_conn()
             |> patch("/portal/api/work-items/#{item["id"]}", %{
               "version" => launched["version"],
               "title" => "Retry with corrected intent"
             })
             |> json_response(200)

    assert %{
             "disposition" => "created",
             "command" => retry_command,
             "execution" => %{
               "task_id" => ^task_id,
               "state" => "queued",
               "generation" => 2,
               "run_id" => nil,
               "failure" => nil
             },
             "work_item" => retried
           } =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/retry", %{
               "action_id" => "retry-action-1",
               "generation" => 1
             })
             |> json_response(202)

    assert retried["title"] == edited["title"]

    assert %{"disposition" => "replayed", "command" => replayed_retry} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/retry", %{
               "action_id" => "retry-action-1",
               "generation" => 1
             })
             |> json_response(200)

    assert replayed_retry["command_id"] == retry_command["command_id"]

    assert %{"command" => second_cancel} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/cancel", %{
               "action_id" => "cancel-action-2",
               "generation" => 2
             })
             |> json_response(202)

    refute second_cancel["command_id"] == first_cancel["command_id"]
  end

  test "retry is hidden and rejected after a failed task is reassigned to a human" do
    project = create_project("RPO")

    item =
      create_work_item(project["id"], "Human-owned retry", %{
        "assignee_type" => "agent",
        "agent_profile" => "codex"
      })

    assert %{"execution" => %{"generation" => 1}, "work_item" => launched} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/run", %{
               "action_id" => "start-human-retry"
             })
             |> json_response(202)

    assert %{"execution" => %{"state" => "cancelled"}} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/cancel", %{
               "action_id" => "cancel-human-retry",
               "generation" => 1
             })
             |> json_response(202)

    assert %{"work_item" => human} =
             api_conn()
             |> patch("/portal/api/work-items/#{item["id"]}", %{
               "version" => launched["version"],
               "assignee_type" => "human",
               "assignee_name" => "Lina"
             })
             |> json_response(200)

    assert human["assignee"]["type"] == "human"

    assert %{"work_item" => %{"execution" => %{"can_retry" => false}}} =
             api_conn()
             |> get("/portal/api/work-items/#{item["id"]}")
             |> json_response(200)

    assert %{"error" => %{"code" => "state_conflict"}} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/retry", %{
               "action_id" => "retry-human-owned",
               "generation" => 1
             })
             |> json_response(409)

    assert %{"work_item" => persisted} =
             api_conn()
             |> get("/portal/api/work-items/#{item["id"]}")
             |> json_response(200)

    assert persisted["assignee"] == %{
             "type" => "human",
             "name" => "Lina",
             "agent_profile" => nil
           }
  end

  test "input actions are scoped to the current waiting request and replay after response loss" do
    project = create_project("ASK")

    item =
      create_work_item(project["id"], "Ask for a decision", %{
        "assignee_type" => "agent",
        "agent_profile" => "codex"
      })

    assert %{"execution" => %{"task_id" => task_id}} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/run", %{"action_id" => "start-test"})
             |> json_response(202)

    {runtime, fence, run} = claim_current_task()
    transition(run.id, fence, "running", 301)

    waiting_id = uuid(999)

    assert {:ok, _event} =
             Orchestration.append_events(
               run.id,
               fence,
               [
                 %{
                   event_id: uuid(303),
                   sequence: 1,
                   kind: "waiting_for_input",
                   payload: %{"question" => "Choose a branch"},
                   occurred_at: @now
                 }
               ],
               now: @now
             )

    assert {:ok, _waiting_run} =
             Orchestration.transition(
               run.id,
               fence,
               "waiting_for_input",
               %{"question" => "Choose a branch"},
               waiting_id,
               now: @now
             )

    assert %{
             "work_item" => %{
               "execution" => %{"waiting" => %{"transition_id" => ^waiting_id}}
             }
           } =
             api_conn()
             |> get("/portal/api/work-items/#{item["id"]}")
             |> json_response(200)

    request = %{
      "input" => %{"answer" => "main"},
      "action_id" => "input-action-1",
      "waiting_transition_id" => waiting_id
    }

    assert %{"command" => first_command} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/input", request)
             |> json_response(202)

    assert %{"command" => replayed_command} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/input", request)
             |> json_response(200)

    assert replayed_command["command_id"] == first_command["command_id"]

    transition(run.id, fence, "running", 305)

    assert {:ok, _command} =
             Orchestration.acknowledge_command(
               first_command["command_id"],
               fence,
               "applied",
               uuid(304),
               now: @now
             )

    second_waiting_id = uuid(1)

    assert {:ok, _event} =
             Orchestration.append_events(
               run.id,
               fence,
               [
                 %{
                   event_id: uuid(307),
                   sequence: 2,
                   kind: "waiting_for_input",
                   payload: %{"question" => "Choose a reviewer"},
                   occurred_at: DateTime.add(@now, 1, :second)
                 }
               ],
               now: @now
             )

    assert {:ok, _waiting_run} =
             Orchestration.transition(
               run.id,
               fence,
               "waiting_for_input",
               %{"question" => "Choose a reviewer"},
               second_waiting_id,
               now: @now
             )

    from(transition in RunTransition, where: transition.transition_id == ^waiting_id)
    |> Repo.update_all(set: [id: uuid(998), inserted_at: @now, updated_at: @now])

    from(transition in RunTransition, where: transition.transition_id == ^second_waiting_id)
    |> Repo.update_all(set: [id: uuid(2), inserted_at: @now, updated_at: @now])

    assert %{"error" => %{"code" => "state_conflict"}} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/input", %{
               "input" => %{"answer" => "stale"},
               "action_id" => "input-action-2",
               "waiting_transition_id" => waiting_id
             })
             |> json_response(409)

    assert %{"command" => %{"generation" => 1}} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/input", %{
               "input" => %{"answer" => "Lina"},
               "action_id" => "input-action-3",
               "waiting_transition_id" => second_waiting_id
             })
             |> json_response(202)

    assert runtime.id == run.runtime_id
    assert task_id == run.task_id
  end

  test "raw work-item timeline paginates without dropping durable history" do
    project = create_project("RAW")

    item =
      create_work_item(project["id"], "Inspect complete history", %{
        "assignee_type" => "agent",
        "agent_profile" => "codex"
      })

    assert %{"execution" => %{"task_id" => task_id}} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/run", %{"action_id" => "start-test"})
             |> json_response(202)

    {_runtime, fence, run} = claim_current_task()

    events =
      for sequence <- 1..110 do
        %{
          event_id: uuid(1_000 + sequence),
          sequence: sequence,
          kind: "agent_event",
          payload: %{"message" => "raw event #{sequence}"},
          occurred_at: DateTime.add(@now, sequence, :second)
        }
      end

    assert {:ok, stored} = Orchestration.append_events(run.id, fence, events, now: @now)
    assert length(stored) == 110

    assert %{
             "raw" => %{
               "task" => %{"task_id" => ^task_id},
               "timeline" => first_page,
               "next_before" => cursor
             }
           } =
             api_conn()
             |> get("/portal/api/work-items/#{item["id"]}")
             |> json_response(200)

    assert length(first_page) == 100
    assert is_binary(cursor)

    assert %{"items" => older_page, "next_before" => nil} =
             api_conn()
             |> get(
               "/portal/api/work-items/#{item["id"]}/timeline?before=#{URI.encode_www_form(cursor)}"
             )
             |> json_response(200)

    assert older_page != []

    identity = fn entry ->
      data = entry["data"]

      {entry["source"], entry["run_id"],
       data["event_id"] || data["transition_id"] || data["command_id"]}
    end

    first_ids = MapSet.new(first_page, identity)
    older_ids = MapSet.new(older_page, identity)
    assert MapSet.disjoint?(first_ids, older_ids)

    assert %{"error" => %{"code" => "invalid_request"}} =
             api_conn()
             |> get("/portal/api/work-items/#{item["id"]}/timeline?before=not-a-cursor")
             |> json_response(400)
  end

  test "queued retry has a new attempt identity and raw prior events cannot become its outcome" do
    project = create_project("GEN")

    item =
      create_work_item(project["id"], "Retry without stale outcome", %{
        "assignee_type" => "agent",
        "agent_profile" => "codex"
      })

    assert %{"execution" => %{"generation" => 1}} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/run", %{"action_id" => "start-test"})
             |> json_response(202)

    {_runtime, fence, run} = claim_current_task()
    transition(run.id, fence, "running", 1_300)

    assert {:ok, [_event]} =
             Orchestration.append_events(
               run.id,
               fence,
               [
                 %{
                   event_id: uuid(1_301),
                   sequence: 1,
                   kind: "agent_event",
                   payload: %{"message" => "invalid semantic data must remain raw"},
                   occurred_at: @now
                 }
               ],
               now: @now
             )

    assert {:ok, _failed} =
             Orchestration.transition(
               run.id,
               fence,
               "failed",
               %{"error" => "first attempt"},
               uuid(1_302),
               now: @now
             )

    assert %{"execution" => %{"generation" => 2, "run_id" => nil, "failure" => nil}} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/retry", %{
               "action_id" => "retry-clean-projection",
               "generation" => 1
             })
             |> json_response(202)

    assert %{
             "work_item" => %{
               "execution" => %{
                 "state" => "queued",
                 "generation" => 2,
                 "run_id" => nil,
                 "runtime_id" => nil,
                 "failure" => nil
               }
             },
             "outcome" => %{"summary" => nil, "failure" => nil},
             "raw" => %{"timeline" => timeline}
           } =
             api_conn()
             |> get("/portal/api/work-items/#{item["id"]}")
             |> json_response(200)

    assert Enum.any?(timeline, fn entry ->
             entry["source"] == "event" and entry["generation"] == 1 and
               entry["data"]["payload"]["message"] ==
                 "invalid semantic data must remain raw"
           end)
  end

  test "a work item detail read keeps every query on the calling process" do
    project = create_project("PRD")

    assert {:ok, connection} =
             SymmetryControl.Integrations.create_connection(%{
               provider: "github",
               name: "preload-" <> Ecto.UUID.generate(),
               account_ref: "acme",
               auth_type: "gh_cli",
               capabilities: ["repositories", "work_items", "changes", "ci"]
             })

    assert {:ok, repository} =
             SymmetryControl.Workspaces.create_resource(project["id"], %{
               connection_id: connection.id,
               kind: "repository",
               name: "Preload repository",
               external_ref: "acme/preload"
             })

    assert {:ok, item} =
             SymmetryControl.Workspaces.create_work_item(project["id"], %{
               title: "Read on one connection",
               status: "ready",
               priority: "medium",
               repository_resource_id: repository.id
             })

    reader = self()

    forms = [
      item.id,
      Ecto.UUID.dump!(item.id),
      project["id"],
      Ecto.UUID.dump!(project["id"]),
      repository.id,
      Ecto.UUID.dump!(repository.id)
    ]

    handler_id = {__MODULE__, :work_item_read, item.id}

    :telemetry.attach(
      handler_id,
      [:symmetry_control, :repo, :query],
      fn _event, _measurements, metadata, %{reader: reader, forms: forms} ->
        if work_item_read?(metadata.params, forms),
          do: send(reader, {:work_item_read, self(), to_string(metadata.query)})
      end,
      %{reader: reader, forms: forms}
    )

    on_exit(fn -> :telemetry.detach(handler_id) end)

    assert %{"work_item" => _detail} =
             api_conn()
             |> get("/portal/api/work-items/#{item.id}")
             |> json_response(200)

    reads = work_item_reads([])
    queries = Enum.map(reads, fn {_pid, query} -> query end)
    processes = reads |> Enum.map(&elem(&1, 0)) |> Enum.uniq() |> Enum.sort()

    # Two preloads must have rows for this test to have power: Ecto only
    # preloads in parallel from two preloads with fetch work, and every parallel
    # preload would queue for the sandbox connection the test process owns
    # (the recorded intermittent checkout-timeout failure).
    assert Enum.any?(queries, &String.contains?(&1, "\"projects\""))
    assert Enum.any?(queries, &String.contains?(&1, "\"project_resources\""))

    assert processes == [reader],
           "expected every work item detail read query on #{inspect(reader)}, got #{inspect(processes)}"
  end

  defp work_item_reads(reads) do
    receive do
      {:work_item_read, pid, query} -> work_item_reads([{pid, query} | reads])
    after
      0 -> reads
    end
  end

  defp work_item_read?(params, forms) do
    params
    |> List.wrap()
    |> Enum.any?(fn
      value when is_binary(value) -> value in forms
      values when is_list(values) -> Enum.any?(values, &(&1 in forms))
      _value -> false
    end)
  end

  defp create_project(key) do
    %{"project" => project} =
      api_conn()
      |> post("/portal/api/projects", %{
        "name" => "Project #{key}",
        "key" => key,
        "default_agent_profile" => "codex",
        "default_workspace" => "primary"
      })
      |> json_response(201)

    project
  end

  defp create_work_item(project_id, title, attrs \\ %{}) do
    %{"work_item" => work_item} =
      api_conn()
      |> post(
        "/portal/api/projects/#{project_id}/work-items",
        Map.merge(
          %{
            "title" => title,
            "status" => "ready",
            "priority" => "medium"
          },
          attrs
        )
      )
      |> json_response(201)

    work_item
  end

  defp goal_managed_work_item!(project_id, repository_id) do
    assert {:ok, created, :created} =
             Goals.create_goal(project_id, goal_create_attrs(), "operator:test", now: @now)

    goal = created.goal

    %WorkItem{id: Ecto.UUID.generate()}
    |> WorkItem.changeset(%{
      project_id: project_id,
      repository_resource_id: repository_id,
      title: "Goal-managed work",
      description: "Admitted through Goal membership instead of the legacy work path.",
      status: "backlog",
      priority: "no_priority",
      position: 0,
      assignee_type: "agent",
      assignee_name: "codex",
      agent_profile: "codex",
      workspace: "primary",
      blocked: false
    })
    |> Ecto.Changeset.apply_changes()
    |> WorkItem.goal_membership_changeset(%{
      goal_id: goal.id,
      admitted_revision: goal.current_revision,
      required: true,
      integration: false,
      acceptance_contract: acceptance_contract(),
      baseline_subject: %{
        "resource_id" => repository_id,
        "commit" => String.duplicate("a", 40),
        "tree_digest" => "sha256:" <> String.duplicate("b", 64)
      }
    })
    |> Repo.insert!()
  end

  defp enable_goal_rollout do
    goals_config = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      goals_config
      |> Keyword.put(:rollout_enabled, true)
      |> Keyword.put(:validation_profiles, validation_profiles())
    )

    on_exit(fn -> Application.put_env(:symmetry_control, :goals, goals_config) end)
  end

  defp goal_create_attrs do
    %{
      schema_version: "symmetry.goal_create.v1",
      title: "Goal-managed work",
      mutation_id: Ecto.UUID.generate(),
      initial_revision: %{
        objective: "Keep Goal-owned work off the legacy control path.",
        non_goals: ["No autonomous publication"],
        acceptance_contract: acceptance_contract(),
        authority_policy: %{
          "operator_required_for_scope_change" => true,
          "operator_required_for_completion" => true,
          "publication_allowed" => false,
          "allowed_actions" => []
        },
        execution_policy: %{
          "automatic_execution" => false,
          "max_parallel_tasks" => 1,
          "max_task_admissions" => 1,
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
        context_manifest: %{
          "byte_budget" => 32_768,
          "required_source_kinds" => ["repository"],
          "include_advisory_recall" => false
        },
        reason: "Initial approved contract"
      }
    }
  end

  defp acceptance_contract do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "An independent check must pass.",
      "predicates" => [%{"id" => "check", "kind" => "check", "validator_profile" => "test"}]
    }
  end

  defp validation_profiles do
    [
      test: [
        kind: :check,
        profile_digest: "sha256:" <> String.duplicate("c", 64),
        enabled: true,
        allowed_runtime_ids: ["00000000-0000-4000-8000-000000000001"]
      ]
    ]
  end

  defp api_conn do
    Phoenix.ConnTest.build_conn()
    |> init_test_session(%{portal_operator: PortalSession.issue("test-operator-token")})
    |> put_private(:plug_skip_csrf_protection, true)
    |> put_req_header("accept", "application/json")
  end

  defp claim_current_task do
    key = Ecto.UUID.generate()

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(
               %{name: "portal-runtime", machine_token: "token-#{key}"},
               key,
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret",
               now: @now
             )

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               enrolled.machine.id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: "portal-runtime",
                   name: "Portal runtime",
                   capacity: 1,
                   agent_profile: "codex",
                   workspace: "primary",
                   capabilities: %{}
                 }
               ],
               now: @now
             )

    assert {:ok, run} = Orchestration.assign_one(now: @now)

    assert {:ok, claimed} =
             Orchestration.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: uuid(300)
               },
               now: @now
             )

    fence = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: claimed.claim_id,
      lease_token: claimed.lease_token
    }

    {runtime, fence, run}
  end

  defp transition(run_id, fence, state, number) do
    assert {:ok, _run} =
             Orchestration.transition(run_id, fence, state, %{}, uuid(number), now: @now)
  end

  defp uuid(number),
    do: "00000000-0000-0000-0000-" <> String.pad_leading(Integer.to_string(number), 12, "0")

  defp enforce_csrf(conn),
    do: %{conn | private: Map.delete(conn.private, :plug_skip_csrf_protection)}
end
