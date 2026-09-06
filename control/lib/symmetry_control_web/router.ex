defmodule SymmetryControlWeb.Router do
  use SymmetryControlWeb, :router

  pipeline :api do
    plug :accepts, ["json"]
  end

  pipeline :enrollment do
    plug SymmetryControlWeb.Plugs.EnrollmentAuth
  end

  pipeline :machine do
    plug SymmetryControlWeb.Plugs.MachineAuth
  end

  pipeline :operator do
    plug SymmetryControlWeb.Plugs.OperatorAuth
  end

  pipeline :portal_browser do
    plug :accepts, ["html"]
    plug :fetch_session
    plug :protect_from_forgery
    plug :put_secure_browser_headers
  end

  pipeline :portal_api do
    plug :accepts, ["json"]
    plug :fetch_session
    plug :protect_from_forgery
    plug :put_secure_browser_headers
    plug SymmetryControlWeb.Plugs.PortalAuth, format: :json
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

  scope "/portal", SymmetryControlWeb do
    pipe_through [:portal_browser, :portal_authenticated]

    get "/", PortalController, :index
  end

  scope "/portal/api", SymmetryControlWeb do
    pipe_through :portal_api

    get "/workspace", PortalApiController, :workspace
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
    get "/work-items/:work_item_id", PortalApiController, :show_work_item
    get "/work-items/:work_item_id/timeline", PortalApiController, :work_item_timeline
    patch "/work-items/:work_item_id", PortalApiController, :update_work_item
    patch "/work-items/:work_item_id/move", PortalApiController, :move_work_item
    post "/work-items/:work_item_id/run", PortalApiController, :run_work_item
    post "/work-items/:work_item_id/cancel", PortalApiController, :cancel_work_item
    post "/work-items/:work_item_id/retry", PortalApiController, :retry_work_item
    post "/work-items/:work_item_id/input", PortalApiController, :provide_input
  end

  scope "/", SymmetryControlWeb do
    pipe_through :api

    get "/healthz", HealthController, :show
  end

  scope "/api/v1", SymmetryControlWeb do
    pipe_through [:api, :enrollment]

    post "/machines", DaemonController, :enroll
  end

  scope "/api/v1", SymmetryControlWeb do
    pipe_through :api

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
    put "/commands/:command_id/acknowledgements/:ack_id", DaemonController, :acknowledge_command
  end

  scope "/api/v1", SymmetryControlWeb do
    pipe_through [:api, :operator]

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
