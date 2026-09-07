defmodule SymmetryControlWeb.ChatControllerTest do
  use SymmetryControlWeb.ConnCase, async: false

  alias SymmetryControl.Workspaces
  alias SymmetryControlWeb.PortalSession

  test "chat is authenticated and unsafe requests remain CSRF protected", %{conn: conn} do
    assert %{"error" => %{"code" => "unauthenticated"}} =
             conn
             |> put_req_header("accept", "application/json")
             |> get("/portal/api/chat?scope=workspace")
             |> json_response(401)

    protected = api_conn() |> Map.update!(:private, &Map.delete(&1, :plug_skip_csrf_protection))

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

  defp api_conn do
    Phoenix.ConnTest.build_conn()
    |> init_test_session(%{portal_operator: PortalSession.issue("test-operator-token")})
    |> put_private(:plug_skip_csrf_protection, true)
    |> put_req_header("accept", "application/json")
  end
end
