defmodule SymmetryControlWeb.Router do
  use SymmetryControlWeb, :router

  pipeline :api do
    plug :accepts, ["json"]
  end

  pipeline :enrollment do
    plug SymmetryControlWeb.Plugs.CredentialAuth, accepts: :enrollment
  end

  pipeline :machine do
    plug SymmetryControlWeb.Plugs.CredentialAuth, accepts: :machine
    plug SymmetryControlWeb.Plugs.PortalInvalidations
  end

  pipeline :operator do
    plug :put_surface, surface: :operator
    plug SymmetryControlWeb.Plugs.CredentialAuth, accepts: :operator
    plug SymmetryControlWeb.Plugs.PortalInvalidations
  end

  pipeline :portal_browser do
    plug :accepts, ["html"]
    plug :fetch_session
    plug :protect_from_forgery
    plug :put_secure_browser_headers
    plug :put_portal_no_store
  end

  pipeline :portal_api do
    plug :accepts, ["json"]
    plug :put_surface, surface: :portal
    plug :fetch_session
    plug :protect_from_forgery
    plug :put_secure_browser_headers
    plug :put_portal_no_store
    plug SymmetryControlWeb.Plugs.PortalAuth, format: :json
    plug SymmetryControlWeb.Plugs.PortalInvalidations
  end

  pipeline :portal_authenticated do
    plug SymmetryControlWeb.Plugs.PortalAuth, format: :html
  end

  scope "/", SymmetryControlWeb do
    pipe_through :portal_browser

    get "/", PortalController, :home
    get "/portal/login", PortalSessionController, :new
    post "/portal/login", PortalSessionController, :create
    post "/portal/logout", PortalSessionController, :delete
  end

  scope "/portal/api", SymmetryControlWeb do
    pipe_through :portal_api

    get "/session", PortalSessionController, :show
    get "/workspace", PortalApiController, :workspace
    get "/connections", PortalApiController, :connections
    get "/chat", ChatController, :index
    post "/chat/messages", ChatController, :create
    post "/connections", PortalApiController, :create_connection
    patch "/connections/:connection_id", PortalApiController, :update_connection
    delete "/connections/:connection_id", PortalApiController, :delete_connection
    post "/connections/:connection_id/check", PortalApiController, :check_connection
    post "/projects", PortalApiController, :create_project
    patch "/projects/:project_id", PortalApiController, :update_project
    post "/projects/:project_id/sync", PortalApiController, :sync_project
    post "/projects/:project_id/resources", PortalApiController, :create_resource
    patch "/resources/:resource_id", PortalApiController, :update_resource
    delete "/resources/:resource_id", PortalApiController, :delete_resource
    post "/resources/:resource_id/sync", PortalApiController, :sync_resource
    post "/projects/:project_id/work-items", PortalApiController, :create_work_item
    post "/projects/:project_id/goals", GoalController, :create
    get "/attention", PortalApiController, :attention
    get "/goals/:goal_id", GoalController, :show
    post "/goals/:goal_id/commands", GoalController, :command
    get "/goals/:goal_id/events", GoalController, :events
    get "/goals/:goal_id/graph", GoalController, :graph
    get "/goals/:goal_id/contexts/:snapshot_id", GoalController, :context
    get "/work-items/:work_item_id", PortalApiController, :show_work_item
    get "/work-items/:work_item_id/timeline", PortalApiController, :work_item_timeline
    patch "/work-items/:work_item_id", PortalApiController, :update_work_item
    patch "/work-items/:work_item_id/move", PortalApiController, :move_work_item
    post "/work-items/:work_item_id/run", PortalApiController, :run_work_item
    post "/work-items/:work_item_id/cancel", PortalApiController, :cancel_work_item
    post "/work-items/:work_item_id/retry", PortalApiController, :retry_work_item
    post "/work-items/:work_item_id/input", PortalApiController, :provide_input
  end

  scope "/portal", SymmetryControlWeb do
    pipe_through [:portal_browser, :portal_authenticated]

    get "/", PortalController, :index
    get "/attention", PortalController, :index
    get "/chat", PortalController, :index
    get "/connections", PortalController, :index
    get "/projects", PortalController, :index
    get "/projects/:project_id", PortalController, :index
    get "/work/:work_item_id", PortalController, :index
    get "/runtimes", PortalController, :index
    get "/goals/:goal_id", PortalController, :index
  end

  defp put_portal_no_store(conn, _options), do: put_resp_header(conn, "cache-control", "no-store")

  # The authority surface a route belongs to, assigned once per pipeline: a
  # `:portal` route serves the portal projection and acts as the portal
  # operator, an `:operator` route the durable projection and the operator.
  defp put_surface(conn, options), do: assign(conn, :surface, Keyword.fetch!(options, :surface))

  scope "/", SymmetryControlWeb do
    pipe_through :api

    get "/healthz", HealthController, :show
  end

  scope "/api/v1", SymmetryControlWeb do
    pipe_through [:api, :enrollment]

    post "/machines", DaemonController, :enroll
  end

  scope "/api/v1", SymmetryControlWeb do
    pipe_through [:api, SymmetryControlWeb.Plugs.PortalInvalidations]

    post "/provider-actions", ProviderActionController, :create, log: false
  end

  scope "/api/v1", SymmetryControlWeb do
    pipe_through [:api, :machine]

    put "/machines/:machine_id/sessions/:daemon_instance_id", DaemonController, :register_session
    patch "/runtimes/:runtime_id", DaemonController, :heartbeat
    get "/runtimes/:runtime_id/dispatch", DaemonController, :work
    put "/runtimes/:runtime_id/reconciliation", DaemonController, :reconcile
    put "/runs/:run_id/claims/:claim_id", DaemonController, :claim
    patch "/runs/:run_id/lease", DaemonController, :heartbeat_run
    post "/runs/:run_id/events", DaemonController, :append_events
    put "/runs/:run_id/transitions/:transition_id", DaemonController, :transition
    put "/runs/:run_id/session", DaemonController, :attach_harness_session
    get "/runs/:run_id/session", DaemonController, :fetch_harness_session_attachment
    put "/runs/:run_id/session/stopped", DaemonController, :mark_harness_session_stopped
    post "/runs/:run_id/evidence", DaemonController, :append_evidence
    post "/runs/:run_id/usage", DaemonController, :record_usage
    get "/runs/:run_id/context", DaemonController, :fetch_run_context
    put "/commands/:command_id/acknowledgements/:ack_id", DaemonController, :acknowledge_command
  end

  scope "/api/v1", SymmetryControlWeb do
    pipe_through [:api, :operator]

    post "/projects/:project_id/goals", GoalController, :create
    get "/goals/:goal_id", GoalController, :show
    post "/goals/:goal_id/commands", GoalController, :command
    get "/goals/:goal_id/events", GoalController, :events
    get "/goals/:goal_id/graph", GoalController, :graph
    get "/goals/:goal_id/contexts/:snapshot_id", GoalController, :context
    get "/attention", GoalController, :attention
    post "/tasks", TaskController, :create
    get "/tasks/:task_id", TaskController, :show
    post "/tasks/:task_id/commands", TaskController, :command
    get "/tasks/:task_id/timeline", TaskHistoryController, :timeline
    get "/tasks/:task_id/events", TaskHistoryController, :events
    get "/tasks/:task_id/transitions", TaskHistoryController, :transitions
    get "/tasks/:task_id/commands", TaskHistoryController, :commands
    get "/runtimes", RuntimeController, :index
    get "/runtimes/:runtime_id", RuntimeController, :show
  end
end
