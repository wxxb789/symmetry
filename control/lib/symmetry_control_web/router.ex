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

  scope "/", SymmetryControlWeb do
    pipe_through :api

    get "/healthz", HealthController, :show
  end

  scope "/api/v1", SymmetryControlWeb do
    pipe_through [:api, :enrollment]

    post "/machines", DaemonController, :enroll
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
