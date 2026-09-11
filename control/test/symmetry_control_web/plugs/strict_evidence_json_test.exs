defmodule SymmetryControlWeb.Plugs.StrictEvidenceJSONTest do
  use ExUnit.Case, async: true

  import Plug.Conn
  import Plug.Test

  alias SymmetryControlWeb.Plugs.StrictEvidenceJSON

  @parser_options StrictEvidenceJSON.init(
                    parsers: [:urlencoded, :multipart, :json],
                    pass: ["*/*"],
                    json_decoder: Jason
                  )

  test "captures an evidence JSON body without materializing it" do
    body = ~s({"value":1,"nested":{"items":[true,null]}})

    conn =
      :post
      |> conn("/api/v1/runs/run-1/evidence", body)
      |> put_req_header("content-type", "application/json")
      |> StrictEvidenceJSON.call(@parser_options)

    assert conn.body_params == %{}
    assert conn.private[StrictEvidenceJSON.raw_body_private_key()] == body

    assert {:ok, %{"value" => 1, "nested" => %{"items" => [true, nil]}}} =
             StrictEvidenceJSON.materialize(conn)

    conn = StrictEvidenceJSON.call(conn, @parser_options)
    assert conn.private[StrictEvidenceJSON.raw_body_private_key()] == body

    assert {:ok, "", _conn} = read_body(conn)
  end

  test "rejects duplicate decoded keys recursively, including escaped aliases" do
    body = ~S({"items":[{"name":1,"\u006eame":2}]})

    conn =
      :post
      |> conn("/api/v1/runs/run-1/evidence", body)
      |> put_req_header("content-type", "application/json")
      |> StrictEvidenceJSON.call(@parser_options)

    assert {:error, :duplicate_json_key} = StrictEvidenceJSON.materialize(conn)
  end

  test "returns safe errors for malformed JSON and non-object JSON" do
    malformed =
      :post
      |> conn("/api/v1/runs/run-1/evidence", ~s({"secret":"private",))
      |> put_req_header("content-type", "application/json")
      |> StrictEvidenceJSON.call(@parser_options)

    assert {:error, :malformed_json} = result = StrictEvidenceJSON.materialize(malformed)
    refute inspect(result) =~ "private"

    scalar =
      :post
      |> conn("/api/v1/runs/run-1/evidence", "[1,2,3]")
      |> put_req_header("content-type", "application/json")
      |> StrictEvidenceJSON.call(@parser_options)

    assert {:error, :expected_json_object} = StrictEvidenceJSON.materialize(scalar)
  end

  test "delegates non-evidence requests to Plug.Parsers unchanged" do
    cases = [
      {:post, "/api/v1/runs/run-1/evidence/extra", ~s({"value":1}), "application/json"},
      {:put, "/api/v1/runs/run-1/evidence", ~s({"value":1}), "application/json"},
      {:post, "/api/v1/runs/run-1/evidence", "value=1", "application/x-www-form-urlencoded"},
      {:post, "/api/v1/runs/run-1/other", ~s({"value":1}), "application/json"}
    ]

    for {method, path, body, content_type} <- cases do
      conn =
        method
        |> conn(path, body)
        |> put_req_header("content-type", content_type)
        |> StrictEvidenceJSON.call(@parser_options)

      assert conn.body_params in [%{"value" => 1}, %{"value" => "1"}]
      refute Map.has_key?(conn.private, StrictEvidenceJSON.raw_body_private_key())
    end
  end

  test "recognizes the JSON structured suffix on the exact route" do
    conn =
      :post
      |> conn("/api/v1/runs/run-1/evidence", ~s({"value":1}))
      |> put_req_header("content-type", "application/vnd.symmetry+json")
      |> StrictEvidenceJSON.call(@parser_options)

    assert conn.body_params == %{}
    assert {:ok, %{"value" => 1}} = StrictEvidenceJSON.materialize(conn)
  end

  test "preserves Plug.Parsers request-too-large behavior" do
    options =
      StrictEvidenceJSON.init(
        parsers: [:urlencoded, :multipart, :json],
        pass: ["*/*"],
        length: 2,
        json_decoder: Jason
      )

    conn =
      :post
      |> conn("/api/v1/runs/run-1/evidence", ~s({"value":1}))
      |> put_req_header("content-type", "application/json")

    assert_raise Plug.Parsers.RequestTooLargeError, fn ->
      StrictEvidenceJSON.call(conn, options)
    end
  end

  test "reports when materialization is requested without a capture" do
    assert {:error, :not_captured} = StrictEvidenceJSON.materialize(conn(:get, "/healthz"))
  end
end
