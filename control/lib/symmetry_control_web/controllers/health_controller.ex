defmodule SymmetryControlWeb.HealthController do
  use SymmetryControlWeb, :controller

  def show(conn, _params) do
    json(conn, %{status: "ok"})
  end
end
