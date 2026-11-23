defmodule SymmetryControlWeb.PortalRealtimeTest do
  use SymmetryControlWeb.ConnCase, async: false

  import Phoenix.ChannelTest

  alias Phoenix.Transports.WebSocket
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Portal.Invalidations
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces
  alias SymmetryControlWeb.PortalChannel
  alias SymmetryControlWeb.PortalSession
  alias SymmetryControlWeb.PortalSocket
  alias SymmetryControlWeb.PortalSocketToken

  @operator_token "test-operator-token"
  # One identity reused by the path table so a row's path and its expectation
  # cannot drift apart.
  @resource_id "00000007-0000-4000-8000-000000000007"

  defp operator_conn do
    build_conn()
    |> init_test_session(%{portal_operator: PortalSession.issue(@operator_token)})
    |> Plug.Conn.put_private(:plug_skip_csrf_protection, true)
    |> put_req_header("accept", "application/json")
  end

  defp bootstrap_token do
    token =
      operator_conn()
      |> get("/portal/api/session")
      |> json_response(200)
      |> Map.fetch!("socket_token")

    assert PortalSocketToken.verify(token) == {:ok, PortalSocketToken.audience()}
    token
  end

  test "the bootstrap mints one short-lived audience-scoped socket token" do
    token = bootstrap_token()

    # The audience is the only authority the token carries, and it is not the
    # operator credential: the login token never appears in the bootstrap body.
    refute token =~ @operator_token

    # The token body carries the audience and nothing else: the operator
    # credential is never a member of it.
    assert {:ok, payload} =
             Phoenix.Token.verify(SymmetryControlWeb.Endpoint, "portal-socket-v1", token,
               max_age: PortalSocketToken.max_age_seconds()
             )

    assert Enum.sort(Map.keys(payload)) == [:audience]
    assert payload[:audience] == "portal:operator"

    # A token minted for another audience is refused even though it verifies.
    foreign =
      Phoenix.Token.sign(SymmetryControlWeb.Endpoint, "portal-socket-v1", %{
        audience: "portal:admin"
      })

    assert PortalSocketToken.verify(foreign) == {:error, :unauthorized}

    # A tampered token never verifies.
    [payload, signature] = String.split(token, ".", parts: 2)
    assert {:error, _reason} = PortalSocketToken.verify(payload <> "x." <> signature)
    assert {:error, _reason} = PortalSocketToken.verify("not-a-token")
    assert PortalSocketToken.verify("") == {:error, :unauthenticated}

    # The lifetime is short: a token signed an hour ago is expired.
    stale =
      Phoenix.Token.sign(
        SymmetryControlWeb.Endpoint,
        "portal-socket-v1",
        %{audience: "portal:operator"},
        signed_at: System.system_time(:second) - 3_600
      )

    assert {:error, :expired} = PortalSocketToken.verify(stale)
  end

  test "the operator socket accepts only a verified bootstrap token" do
    token = bootstrap_token()

    assert PortalSocket.connect(%{}, socket(PortalSocket, "portal:a", %{}), %{}) == :error

    assert PortalSocket.connect(%{}, socket(PortalSocket, "portal:a", %{}), %{
             auth_token: "garbage"
           }) ==
             :error

    foreign =
      Phoenix.Token.sign(SymmetryControlWeb.Endpoint, "portal-socket-v1", %{
        audience: "portal:other"
      })

    assert PortalSocket.connect(%{}, socket(PortalSocket, "portal:a", %{}), %{auth_token: foreign}) ==
             :error

    assert {:ok, connected} =
             PortalSocket.connect(%{}, socket(PortalSocket, "portal:a", %{}), %{auth_token: token})

    assert connected.assigns.portal_audience == PortalSocketToken.audience()
    assert PortalSocket.id(connected) == "portal:" <> PortalSocketToken.audience()
  end

  test "the invalidation topic is authorized and receive-only" do
    {:ok, socket} =
      PortalSocket.connect(%{}, socket(PortalSocket, "portal:a", %{}), %{
        auth_token: bootstrap_token()
      })

    assert {:ok, %{audience: "portal:operator"}, _joined} =
             subscribe_and_join(socket, PortalChannel, Invalidations.topic(), %{})

    # Nothing the browser sends can mutate: the channel has no client event
    # handler at all, so the real-time path cannot become a second UI state owner.
    refute function_exported?(PortalChannel, :handle_in, 3)

    # A connection without the operator audience cannot join, and joining is where
    # the invalidation subscription is established.
    unauthorized = socket(PortalSocket, "portal:b", %{portal_audience: "portal:other"})

    assert {:error, %{reason: "forbidden"}} =
             subscribe_and_join(unauthorized, PortalChannel, Invalidations.topic(), %{})

    Invalidations.publish(%{kind: "workspace"})

    # The test transport uses the no-op serializer, so the pushed payload keeps
    # its atom keys; the browser's JSON serializer renders the same members as
    # strings on the wire.
    assert_receive %Phoenix.Socket.Message{event: "invalidation", payload: %{kind: "workspace"}},
                   500
  end

  test "a committed portal write publishes the changed resource identity" do
    Invalidations.subscribe()

    created =
      operator_conn()
      |> post("/portal/api/projects", %{
        "name" => "Realtime project",
        "key" => "RT1",
        "default_agent_profile" => "codex",
        "default_workspace" => "primary"
      })
      |> json_response(201)

    project_id = created["project"]["id"]
    assert_receive {:portal_invalidation, %{kind: "project"}}

    operator_conn()
    |> patch("/portal/api/projects/#{project_id}", %{"version" => 1, "name" => "Realtime renamed"})
    |> json_response(200)

    assert_receive {:portal_invalidation, %{kind: "project", id: ^project_id}}

    work_item =
      operator_conn()
      |> post("/portal/api/projects/#{project_id}/work-items", %{
        "title" => "Realtime item",
        "description" => "Seeded through the portal API",
        "status" => "ready",
        "priority" => "medium",
        "assignee_type" => "unassigned"
      })
      |> json_response(201)

    work_item_id = work_item["work_item"]["id"]
    assert_receive {:portal_invalidation, %{kind: "work_item", project_id: ^project_id}}

    operator_conn()
    |> patch("/portal/api/work-items/#{work_item_id}", %{
      "version" => 1,
      "title" => "Realtime item 2"
    })
    |> json_response(200)

    assert_receive {:portal_invalidation, %{kind: "work_item", id: ^work_item_id}}

    # A refused write committed nothing and publishes nothing.
    operator_conn()
    |> patch("/portal/api/work-items/#{work_item_id}", %{"version" => 1, "title" => "stale"})
    |> json_response(409)

    refute_receive {:portal_invalidation, %{kind: "work_item"}}, 100
  end

  test "the write classification names the changed resource for every write surface" do
    table = [
      {:post, "/portal/api/connections", %{kind: "connection"}},
      {:patch, "/portal/api/connections/#{@resource_id}", %{kind: "connection", id: @resource_id}},
      {:post, "/portal/api/projects/#{@resource_id}/resources", %{kind: "resource", project_id: @resource_id}},
      {:delete, "/portal/api/resources/#{@resource_id}", %{kind: "resource", id: @resource_id}},
      {:post, "/portal/api/projects/#{@resource_id}/goals", %{kind: "goal", project_id: @resource_id}},
      {:post, "/portal/api/goals/#{@resource_id}/commands", %{kind: "goal", id: @resource_id}},
      {:post, "/portal/api/chat/messages", %{kind: "chat"}},
      {:post, "/api/v1/runs/#{@resource_id}/events", %{kind: "run", id: @resource_id}},
      {:put, "/api/v1/runs/#{@resource_id}/transitions/#{@resource_id}", %{kind: "run", id: @resource_id}},
      {:put, "/api/v1/commands/#{@resource_id}/acknowledgements/#{@resource_id}", %{kind: "run"}},
      {:patch, "/api/v1/runtimes/#{@resource_id}", %{kind: "runtime", id: @resource_id}},
      {:put, "/api/v1/machines/#{@resource_id}/sessions/#{@resource_id}", %{kind: "runtime"}},
      {:post, "/api/v1/unclassified-surface", %{kind: "workspace"}}
    ]

    Invalidations.subscribe()

    Enum.each(table, fn {method, path, expected} ->
      conn =
        method
        |> build_conn(path)
        |> Plug.Conn.put_req_header("content-type", "application/json")

      conn
      |> SymmetryControlWeb.Plugs.PortalInvalidations.call([])
      |> send_resp(200, "{}")

      assert_receive {:portal_invalidation, payload}

      # The published payload carries exactly the classified identity, derived
      # from the path alone.
      assert payload == expected
    end)

    refute_receive {:portal_invalidation, _payload}, 50

    # A publisher handing `publish/1` extra members — a transcript-bearing body,
    # say — cannot leak them: normalize/1 keeps the classified identity only.
    Invalidations.publish(%{kind: "chat", messages: [%{body: "RAW_TRANSCRIPT_TEXT"}]})

    assert_receive {:portal_invalidation, payload}
    assert payload == %{kind: "chat"}
  end

  test "a goal change carries the goal's own event cursor" do
    {:ok, project} =
      Workspaces.create_project(%{
        name: "Realtime goal project",
        key: "RTG",
        default_agent_profile: "codex",
        default_workspace: "primary"
      })

    goal =
      Repo.insert!(%Goal{
        project_id: project.id,
        title: "Realtime goal",
        state: "draft",
        current_revision: 1,
        event_sequence: 7
      })

    assert SymmetryControl.Goals.current_event_sequence(goal.id) == 7
    assert SymmetryControl.Goals.current_event_sequence(@resource_id) == nil
    assert SymmetryControl.Goals.current_event_sequence("not-a-uuid") == nil

    Invalidations.subscribe()
    Invalidations.publish(%{kind: "goal", id: goal.id})

    assert_receive {:portal_invalidation, %{kind: "goal", id: goal_id, cursor: 7}}
    assert goal_id == goal.id
  end

  test "an unknown goal still publishes its change without a cursor" do
    # The cursor is a hint for a receipt list's continuation: a goal that cannot
    # be read publishes the change rather than dropping it.
    Invalidations.subscribe()
    Invalidations.publish(%{kind: "goal", id: @resource_id})

    assert_receive {:portal_invalidation, payload}
    assert payload == %{kind: "goal", id: @resource_id}
  end

  test "an unclassified kind is refused instead of publishing a silent change" do
    assert_raise ArgumentError, fn -> Invalidations.publish(%{kind: "not_a_resource"}) end
  end

  test "the websocket transport parses the auth token into the socket connect info" do
    # The whole transport path, not a direct `PortalSocket.connect/3` call: the
    # token arrives as the `base64url.bearer.phx.` WebSocket subprotocol. The
    # endpoint copies the socket-level `auth_token` over any transport-level one,
    # so a declaration nested inside `websocket:` leaves the transport with nil
    # and every handshake is refused with 403 (what the browser gate's
    # connected-surface case exercises end to end).
    {_path, module, config} = portal_socket_config()
    assert config[:auth_token] === true
    refute Keyword.has_key?(config[:websocket], :auth_token)

    # An accepted connection cannot be upgraded by the test adapter, so a
    # successful connect answers 400 while a refused one answers 403.
    assert transport_status(module, config, PortalSocketToken.sign()) != 403
    assert transport_status(module, config, "not-a-token") == 403
  end

  defp portal_socket_config do
    Enum.find(SymmetryControlWeb.Endpoint.__sockets__(), fn {path, _module, _config} ->
      path == "/portal/socket"
    end)
  end

  defp transport_status(module, socket_config, token) do
    url = SymmetryControlWeb.Endpoint.config(:url)
    origin = "#{url[:scheme] || "http"}://#{url[:host]}:#{url[:port] || 80}"

    config =
      socket_config[:websocket]
      |> Keyword.put(:auth_token, socket_config[:auth_token])
      |> Phoenix.Socket.Transport.load_config(WebSocket)

    :get
    |> build_conn("/portal/socket/websocket", %{"vsn" => "2.0.0"})
    |> put_req_header("origin", origin)
    |> put_req_header(
      "sec-websocket-protocol",
      "phoenix,base64url.bearer.phx." <> Base.encode64(token, padding: false)
    )
    |> WebSocket.call({SymmetryControlWeb.Endpoint, module, config})
    |> Map.fetch!(:status)
  end
end
