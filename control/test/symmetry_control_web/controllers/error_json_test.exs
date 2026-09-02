defmodule SymmetryControlWeb.ErrorJSONTest do
  use SymmetryControlWeb.ConnCase, async: true

  test "renders 404 with the protocol envelope" do
    assert SymmetryControlWeb.ErrorJSON.render("404.json", %{}) == %{
             error: %{code: "not_found", message: "resource was not found"}
           }
  end

  test "renders invalid request fallback statuses with the protocol envelope" do
    assert SymmetryControlWeb.ErrorJSON.render("400.json", %{}) == %{
             error: %{code: "invalid_request", message: "request is invalid"}
           }

    assert SymmetryControlWeb.ErrorJSON.render("405.json", %{}) == %{
             error: %{code: "invalid_request", message: "request is invalid"}
           }
  end

  test "renders 5xx without exception details" do
    assert SymmetryControlWeb.ErrorJSON.render("500.json", %{}) ==
             %{error: %{code: "internal_error", message: "internal server error"}}

    assert SymmetryControlWeb.ErrorJSON.render("503.json", %{reason: "secret failure detail"}) ==
             %{error: %{code: "internal_error", message: "internal server error"}}
  end

  test "router 404 fallback renders the protocol envelope", %{conn: conn} do
    assert %{"error" => %{"code" => "not_found", "message" => "resource was not found"}} =
             conn
             |> put_req_header("accept", "application/json")
             |> get("/api/v1/not-a-route")
             |> json_response(404)
  end
end
