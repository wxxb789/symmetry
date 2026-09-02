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

    post "/daemon/enroll", DaemonController, :enroll
  end

  scope "/api/v1", SymmetryControlWeb do
    pipe_through [:api, :machine]

    post "/daemon/sessions", DaemonController, :register_session
    post "/runtimes/:runtime_id/heartbeat", DaemonController, :heartbeat
    get "/runtimes/:runtime_id/work", DaemonController, :work
    post "/runtimes/:runtime_id/reconcile", DaemonController, :reconcile
    post "/runs/:run_id/claim", DaemonController, :claim
    post "/runs/:run_id/heartbeat", DaemonController, :heartbeat_run
    post "/runs/:run_id/events", DaemonController, :append_events
    post "/runs/:run_id/state", DaemonController, :transition
    post "/commands/:command_id/ack", DaemonController, :acknowledge_command
  end

  scope "/api/v1", SymmetryControlWeb do
    pipe_through [:api, :operator]

    post "/tasks", TaskController, :create
    get "/tasks/:task_id", TaskController, :show
    post "/tasks/:task_id/cancel", TaskController, :cancel
    post "/tasks/:task_id/input", TaskController, :input
  end
end
