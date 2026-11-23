defmodule SymmetryControlWeb.ChatControllerTest do
  use SymmetryControlWeb.ConnCase, async: true

  import SymmetryControlWeb.ServedDocument, only: [validate_served_document!: 2]
  import SymmetryControlWeb.TestConn, only: [api_conn: 0]

  alias SymmetryControl.Workspaces

  test "chat is authenticated and unsafe requests remain CSRF protected", %{conn: conn} do
    assert %{"error" => %{"code" => "unauthenticated"}} =
             conn
             |> put_req_header("accept", "application/json")
             |> get("/portal/api/chat?scope=workspace")
             |> json_response(401)

    protected = Map.update!(api_conn(), :private, &Map.delete(&1, :plug_skip_csrf_protection))

    assert_raise Plug.CSRFProtection.InvalidCSRFTokenError, fn ->
      post(protected, "/portal/api/chat/messages", %{
        scope: "workspace",
        intent: "discuss",
        content: "Hello"
      })
    end
  end

  test "chat API creates real work, returns durable messages and rejects changed action replays" do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: "Chat API",
               key: "API",
               default_agent_profile: "codex",
               default_workspace: "primary"
             })

    params = %{
      scope: "project",
      project_id: project.id,
      action_id: "api-start",
      intent: "start_work",
      content: "Implement the durable command adapter."
    }

    response = api_conn() |> post("/portal/api/chat/messages", params) |> json_response(201)
    assert response["work_item_id"]
    assert response["message"]["role"] == "human"
    assert response["reply"]["role"] == "assistant"
    assert response["command"] == nil
    replay = api_conn() |> post("/portal/api/chat/messages", params) |> json_response(200)
    assert replay == response

    assert %{"error" => %{"code" => "idempotency_conflict"}} =
             api_conn()
             |> post("/portal/api/chat/messages", %{params | content: "Different goal"})
             |> json_response(409)

    conversation =
      api_conn()
      |> get("/portal/api/chat", %{scope: "project", project_id: project.id})
      |> json_response(200)

    assert length(conversation["messages"]) == 2
    assert hd(conversation["runs"])["work_item"]["id"] == response["work_item_id"]
    assert hd(conversation["runs"])["work_item"]["execution"]["state"] == "queued"
  end

  test "mutating messages require explicit intent and action identity" do
    assert %{"error" => %{"code" => "invalid_request"}} =
             api_conn()
             |> post("/portal/api/chat/messages", %{
               scope: "workspace",
               content: "Cancel all work"
             })
             |> json_response(400)

    assert %{"error" => %{"code" => "invalid_request"}} =
             api_conn()
             |> post("/portal/api/chat/messages", %{
               scope: "workspace",
               intent: "cancel",
               content: "Cancel"
             })
             |> json_response(400)
  end

  test "a refused chat work changeset serves the validation envelope" do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: "Chat refusal",
               key: "REFUSE",
               default_agent_profile: "codex",
               default_workspace: "primary"
             })

    assert %{
             "error" => %{
               "code" => "validation_failed",
               "message" => "request contains invalid fields",
               "fields" => %{"title" => ["can't be blank" | _]}
             }
           } =
             api_conn()
             |> post("/portal/api/chat/messages", %{
               scope: "project",
               project_id: project.id,
               action_id: "refused-work-title",
               intent: "start_work",
               content: "Start a work item whose title is blank.",
               work: %{"title" => ""}
             })
             |> json_response(422)
  end

  # The served conversation document is validated against the same canonical
  # bundle the fixture manifest and the daemon use. A member-name disagreement
  # between `Chat.conversation/1` and `contracts/v1/chat-read.schema.json`
  # (whose positive fixtures are sanitized captures of this response) fails here.
  test "the real served conversation read validates against the contract bundle" do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: "Chat contract",
               key: "CONTRACT",
               default_agent_profile: "codex",
               default_workspace: "primary"
             })

    assert %{"reply" => %{"role" => "assistant"}} =
             api_conn()
             |> post("/portal/api/chat/messages", %{
               scope: "project",
               project_id: project.id,
               action_id: "contract-discuss",
               intent: "discuss",
               content: "What is the current state of the migration work?"
             })
             |> json_response(201)

    assert %{"work_item_id" => work_item_id} =
             api_conn()
             |> post("/portal/api/chat/messages", %{
               scope: "project",
               project_id: project.id,
               action_id: "contract-start",
               intent: "start_work",
               content: "Implement the reversible migration",
               work: %{
                 "title" => "Implement the reversible migration",
                 "agent_profile" => "codex",
                 "workspace" => "primary"
               }
             })
             |> json_response(201)

    conversation =
      api_conn()
      |> get("/portal/api/chat", %{scope: "project", project_id: project.id})
      |> json_response(200)

    assert length(conversation["messages"]) == 4
    assert hd(conversation["runs"])["work_item"]["id"] == work_item_id
    validate_served_document!("chat-read", conversation)
  end
end
