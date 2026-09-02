defmodule SymmetryControlWeb.Router do
  use SymmetryControlWeb, :router

  pipeline :api do
    plug :accepts, ["json"]
  end

  scope "/", SymmetryControlWeb do
    pipe_through :api

    get "/healthz", HealthController, :show
  end
end
