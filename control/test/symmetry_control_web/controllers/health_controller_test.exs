defmodule SymmetryControlWeb.HealthControllerTest do
  use SymmetryControlWeb.ConnCase, async: false

  test "GET /healthz returns an ok status" do
    response = build_conn() |> get("/healthz") |> json_response(200)

    assert response == %{
             "status" => "ok",
             "checks" => %{
               "database" => "ok",
               "scheduler" => "ok",
               "reaper" => "ok",
               "provider_access" => "ok"
             }
           }
  end
end
