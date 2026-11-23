defmodule SymmetryControlWeb.Plugs.PortalInvalidations do
  @moduledoc """
  Publishes one portal invalidation for every request that may have committed a
  change to a served read.

  Registering the mapping here, once, keeps change discovery to a single owner:
  a controller action does not have to remember to announce its own change, and a
  new write route is announced as soon as it is classified. The classification
  reads the request path only: identities come from path parameters, no
  response body is parsed, and the only read added to a write request is the
  goal event-sequence cursor lookup `Invalidations` documents for goal changes.

  The registered callback runs after the action returned (the domain commit is
  already done) and before the response is sent. Client refusals
  (400/401/403/404/405/409/422) publish nothing because they commit nothing; a
  5xx may have committed before failing, so it publishes. An unclassified write
  publishes the broadest kind, which costs one extra read and never loses state.
  """

  import Plug.Conn

  alias SymmetryControl.Portal.Invalidations

  @write_methods ~w(POST PATCH PUT DELETE)

  def init(options), do: options

  def call(%Plug.Conn{method: method} = conn, _options) when method in @write_methods do
    register_before_send(conn, fn conn ->
      if publishable_status?(conn.status), do: Invalidations.publish(change_for(conn))
      conn
    end)
  end

  def call(conn, _options), do: conn

  defp publishable_status?(status), do: status < 400 or status >= 500

  defp change_for(conn), do: classify(conn.path_info)

  # Portal writes: the changed resource is the last identity in the path.
  defp classify(["portal", "api", "connections"]), do: %{kind: "connection"}
  defp classify(["portal", "api", "connections", id | _rest]), do: %{kind: "connection", id: id}
  defp classify(["portal", "api", "projects"]), do: %{kind: "project"}
  defp classify(["portal", "api", "projects", id, "sync"]), do: %{kind: "project", id: id}

  defp classify(["portal", "api", "projects", id, "resources"]), do: %{kind: "resource", project_id: id}

  defp classify(["portal", "api", "projects", id, "work-items"]), do: %{kind: "work_item", project_id: id}

  defp classify(["portal", "api", "projects", id, "goals"]), do: %{kind: "goal", project_id: id}
  defp classify(["portal", "api", "projects", id | _rest]), do: %{kind: "project", id: id}
  defp classify(["portal", "api", "resources", id | _rest]), do: %{kind: "resource", id: id}
  defp classify(["portal", "api", "work-items", id | _rest]), do: %{kind: "work_item", id: id}
  defp classify(["portal", "api", "goals", id | _rest]), do: %{kind: "goal", id: id}
  defp classify(["portal", "api", "chat", "messages"]), do: %{kind: "chat"}

  # Daemon-facing writes: everything under a run belongs to that run's execution
  # truth; runtime and machine writes change the runtime projection.
  defp classify(["api", "v1", "runs", id | _rest]), do: %{kind: "run", id: id}
  defp classify(["api", "v1", "runtimes", id | _rest]), do: %{kind: "runtime", id: id}
  defp classify(["api", "v1", "machines" | _rest]), do: %{kind: "runtime"}
  defp classify(["api", "v1", "commands" | _rest]), do: %{kind: "run"}

  # Any other committed write (a new route the classification does not name yet)
  # invalidates broadly rather than silently dropping the change. An enrollment
  # is not routed through this plug and invalidates nothing: a machine changes
  # no served read until it registers a session, which does publish.
  defp classify(_path), do: %{kind: "workspace"}
end
