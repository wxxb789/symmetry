defmodule SymmetryControlWeb.HealthControllerTest do
  use ExUnit.Case, async: true

  import Phoenix.ConnTest

  @endpoint SymmetryControlWeb.Endpoint

  test "GET /healthz returns an ok status" do
    response = build_conn() |> get("/healthz") |> json_response(200)

    assert response == %{"status" => "ok"}
  end
end
