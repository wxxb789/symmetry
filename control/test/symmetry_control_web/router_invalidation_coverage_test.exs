defmodule SymmetryControlWeb.RouterInvalidationCoverageTest do
  @moduledoc """
  Every write route announces its change to the portal channel unless the
  allowlist below names it as changing no served read.

  `Plugs.PortalInvalidations` promises that a new write route is announced as
  soon as it is classified, but `Router.__routes__/0` exposes neither the
  pipeline plugs nor their order, and reading them from the router source would
  keep the promise green after a plug was dropped from a pipeline. So each
  route's scope chain is executed the way the dispatcher executes it — the
  closure `Router.__match_route__/3` returns, run on a conn authenticated for
  the route's scope — and the invalidation its `before_send` publishes is what
  the assertion reads. A route whose chain carries no plug publishes nothing
  and is refused unless `@non_publishing` names it.
  """

  use SymmetryControlWeb.ConnCase, async: false

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Portal.Invalidations
  alias SymmetryControlWeb.Endpoint
  alias SymmetryControlWeb.PortalSession
  alias SymmetryControlWeb.Protocol
  alias SymmetryControlWeb.Router

  @write_verbs [:post, :patch, :put, :delete]

  # Write routes that are deliberately not announced, with the reason each
  # changes no served read. A write route added to a scope whose chain carries
  # the plug needs no entry here; an entry that names no route at all is a
  # defect of this list and fails the stale-entry check.
  @non_publishing [
    {:post, "/portal/login", "the session cookie itself is not a served read"},
    {:post, "/portal/logout", "removing the session cookie changes no served read"},
    {:post, "/api/v1/machines",
     "an enrollment changes no served read until the machine registers a session, which does publish"},
    {:post, "/api/v1/provider-actions",
     "the action records its own provider-action intent rows, which no served read names; the provider's effect reaches the portal through the syncer, which does publish"}
  ]

  setup do
    Invalidations.subscribe()

    {:ok, machine_token: enroll_machine!()}
  end

  test "every write route publishes an invalidation unless the allowlist names it", %{
    machine_token: machine_token
  } do
    routes = write_routes()

    assert routes != []

    for {verb, path} = route <- routes, not allowlisted?(route) do
      run_scope_chain(route, machine_token)

      assert_receive {:portal_invalidation, %{kind: kind}} when is_binary(kind),
                     1_000,
                     "no invalidation was published for #{request_line(verb, path)}"
    end

    assert stale_allowlist_entries(routes) == []
  end

  defp write_routes do
    for route <- Router.__routes__(), route.verb in @write_verbs, do: {route.verb, route.path}
  end

  defp allowlisted?({verb, path}) do
    Enum.any?(@non_publishing, fn {listed_verb, listed_path, _reason} ->
      listed_verb == verb and listed_path == path
    end)
  end

  defp stale_allowlist_entries(routes) do
    known = MapSet.new(routes)

    for {verb, path, _reason} <- @non_publishing,
        not MapSet.member?(known, {verb, path}),
        do: {verb, path}
  end

  # The dispatcher hands the matcher the path segments it matched; the prepare
  # closure and pipe chain below are the ones it then runs, every pipe_through
  # of the route's scope included.
  defp run_scope_chain({verb, path}, machine_token) do
    segments = concrete_segments(path)

    assert {metadata, prepare, pipeline, {_plug, _opts}} =
             Router.__match_route__(request_method(verb), segments, "www.example.com")

    conn =
      build_conn(verb, "/" <> Enum.join(segments, "/"))
      |> Map.put(:secret_key_base, Endpoint.config(:secret_key_base))
      |> authenticate(metadata.pipe_through, machine_token)

    conn = pipeline.(prepare.(conn, metadata))

    if conn.halted or conn.state != :unset do
      flunk(
        "#{request_line(verb, path)} halted in its #{inspect(metadata.pipe_through)} chain " <>
          "without reaching Plug.PortalInvalidations; give the probe that chain's credential " <>
          "or allowlist the route deliberately"
      )
    end

    send_resp(conn, 200, "")
  end

  defp authenticate(conn, pipes, machine_token) do
    conn =
      cond do
        :machine in pipes -> put_req_header(conn, "authorization", "Bearer " <> machine_token)
        :operator in pipes -> put_req_header(conn, "authorization", "Bearer " <> operator_token())
        true -> conn
      end

    conn
    |> put_req_header("accept", "application/json")
    |> init_test_session(%{portal_operator: PortalSession.issue(operator_token())})
  end

  defp concrete_segments(path) do
    for segment <- String.split(path, "/"), segment != "" do
      case segment do
        ":" <> _name -> "probe"
        literal -> literal
      end
    end
  end

  defp request_method(verb), do: verb |> Atom.to_string() |> String.upcase()

  defp request_line(verb, path), do: "#{request_method(verb)} #{path}"

  defp operator_token, do: Protocol.configured_token(:operator_token)

  defp enroll_machine! do
    key = Ecto.UUID.generate()
    token = "router-coverage-#{key}"

    assert {:ok, %{token: ^token}, :created} =
             Orchestration.enroll_machine(%{name: "router-coverage", machine_token: token}, key,
               enrollment_token: "test-enrollment-token",
               expected_enrollment_token: "test-enrollment-token"
             )

    token
  end
end
