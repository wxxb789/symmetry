defmodule SymmetryControlWeb.Plugs.StrictEvidenceJSONTest do
  use ExUnit.Case, async: true

  import Plug.Conn
  import Plug.Test

  alias SymmetryControlWeb.Plugs.StrictEvidenceJSON
  alias SymmetryControlWeb.StrictJSONDecoder

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

  test "captures evidence JSON for a trailing slash and an escaped route segment" do
    for path <- ["/api/v1/runs/run-1/evidence/", "/api/v1/runs/run-1/%65vidence"] do
      body = ~s({"value":1})

      conn =
        :post
        |> conn(path, body)
        |> put_req_header("content-type", "application/json")
        |> StrictEvidenceJSON.call(@parser_options)

      assert conn.body_params == %{}
      assert conn.private[StrictEvidenceJSON.raw_body_private_key()] == body
      assert {:ok, %{"value" => 1}} = StrictEvidenceJSON.materialize(conn)
    end
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

  test "rejects duplicate JSON members on protocol v1 mutation routes before parsing" do
    conn =
      :post
      |> conn("/%61pi/v1/projects/project-1/goals", ~S({"items":[{"name":1,"\u006eame":2}]}))
      |> put_req_header("content-type", "application/json")
      |> StrictEvidenceJSON.call(@parser_options)

    assert conn.halted
    assert conn.status == 400
    assert %{"error" => %{"code" => "invalid_request"}} = Jason.decode!(conn.resp_body)
  end

  test "rejects duplicate JSON members on portal API mutation routes before parsing" do
    conn =
      :post
      |> conn(
        "/portal/%61pi/projects/project-1/goals",
        ~S({"title":"first","\u0074itle":"second"})
      )
      |> put_req_header("content-type", "application/vnd.symmetry+json")
      |> StrictEvidenceJSON.call(@parser_options)

    assert conn.halted
    assert conn.status == 400
    assert %{"error" => %{"code" => "invalid_request"}} = Jason.decode!(conn.resp_body)
  end

  test "rejects duplicate JSON members with the JSON structured suffix on mutation routes" do
    conn =
      :post
      |> conn("/api/v1/runs/run-1/usage", ~s({"name":1,"name":2}))
      |> put_req_header("content-type", "application/vnd.symmetry+json")
      |> StrictEvidenceJSON.call(@parser_options)

    assert conn.halted
    assert conn.status == 400
    assert %{"error" => %{"code" => "invalid_request"}} = Jason.decode!(conn.resp_body)
  end

  test "answers duplicate members on the strict scope with the protocol envelope bytes" do
    conn =
      :post
      |> conn("/api/v1/runs/run-1/usage", ~s({"name":1,"name":2}))
      |> put_req_header("content-type", "application/json")
      |> SymmetryControlWeb.Endpoint.call([])

    assert conn.status == 400

    assert conn.resp_body ==
             ~s({"error":{"code":"invalid_request","message":"request payload is invalid"}})
  end

  test "leaves a malformed strict mutation body on the parser error path" do
    conn =
      :post
      |> conn("/api/v1/runs/run-1/usage", ~s({"name":))
      |> put_req_header("content-type", "application/json")

    assert_raise Plug.Parsers.ParseError, fn ->
      StrictEvidenceJSON.call(conn, @parser_options)
    end
  end

  test "parses a top-level array on the strict scope into the _json body param" do
    conn =
      :post
      |> conn("/api/v1/runs/run-1/usage", "[1,2,3]")
      |> put_req_header("content-type", "application/json")
      |> StrictEvidenceJSON.call(@parser_options)

    assert conn.body_params == %{"_json" => [1, 2, 3]}
    refute conn.halted
  end

  test "rejects duplicate members nested in a top-level array on the strict scope" do
    conn =
      :post
      |> conn("/api/v1/runs/run-1/usage", ~s([{"name":1,"name":2}]))
      |> put_req_header("content-type", "application/json")
      |> StrictEvidenceJSON.call(@parser_options)

    assert conn.halted
    assert conn.status == 400
  end

  test "treats an empty JSON mutation body as empty params on the strict scope" do
    conn =
      :post
      |> conn("/api/v1/runs/run-1/usage", "")
      |> put_req_header("content-type", "application/json")
      |> StrictEvidenceJSON.call(@parser_options)

    assert conn.body_params == %{}
    refute conn.halted
  end

  test "parses duplicate members with the first occurrence winning outside the strict scope" do
    body = ~s({"operator_token":"wrong","operator_token":"test-operator-token"})

    conn =
      :post
      |> conn("/portal/login", body)
      |> put_req_header("content-type", "application/json")
      |> StrictEvidenceJSON.call(@parser_options)

    assert conn.body_params == %{"operator_token" => "wrong"}
    refute conn.halted
  end

  test "does not reject duplicate members on the portal login route" do
    conn =
      :post
      |> conn(
        "/portal/login",
        ~s({"operator_token":"wrong","operator_token":"test-operator-token"})
      )
      |> put_req_header("content-type", "application/json")
      |> put_private(:plug_skip_csrf_protection, true)
      |> SymmetryControlWeb.Endpoint.call([])

    assert conn.status == 401
  end

  test "installs one ordered decoder on the strict options and keeps the plain decoder elsewhere" do
    {parser_options, strict_options} = @parser_options

    strict_json = List.keyfind(elem(strict_options, 0), Plug.Parsers.JSON, 0)
    plain_json = List.keyfind(elem(parser_options, 0), Plug.Parsers.JSON, 0)

    assert strict_json ==
             {Plug.Parsers.JSON,
              {{Plug.Conn, :read_body, []}, {StrictJSONDecoder, :decode!, []}, []}}

    assert plain_json ==
             {Plug.Parsers.JSON, {{Plug.Conn, :read_body, []}, {Jason, :decode!, []}, []}}
  end

  test "allows equal member names in distinct JSON objects on protocol mutation routes" do
    body = ~s({"left":{"name":1},"right":{"name":2}})

    conn =
      :post
      |> conn("/api/v1/runs/run-1/usage", body)
      |> put_req_header("content-type", "application/json")
      |> StrictEvidenceJSON.call(@parser_options)

    assert conn.body_params == %{"left" => %{"name" => 1}, "right" => %{"name" => 2}}
    refute conn.halted
    refute Map.has_key?(conn.private, StrictEvidenceJSON.raw_body_private_key())
  end

  test "does not inspect JSON bodies on GET requests" do
    conn =
      :get
      |> conn("/api/v1/runs/run-1/usage", ~S({"name":1,"name":2}))
      |> put_req_header("content-type", "application/json")
      |> StrictEvidenceJSON.call(@parser_options)

    assert conn.body_params == %{}
    refute conn.halted
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

  test "does not parse multipart/form-data bodies at the endpoint" do
    boundary = "strict-evidence-json-test"

    body =
      "--#{boundary}\r\n" <>
        "Content-Disposition: form-data; name=\"operator_token\"\r\n" <>
        "\r\nwrong\r\n--#{boundary}--\r\n"

    multipart_type = "multipart/form-data; boundary=#{boundary}"

    login =
      :post
      |> conn("/portal/login", body)
      |> put_req_header("content-type", multipart_type)
      |> put_private(:plug_skip_csrf_protection, true)
      |> SymmetryControlWeb.Endpoint.call([])

    assert login.body_params == %Plug.Conn.Unfetched{aspect: :body_params}
    assert login.status == 422

    machine_api =
      :post
      |> conn("/api/v1/machines", body)
      |> put_req_header("content-type", multipart_type)
      |> SymmetryControlWeb.Endpoint.call([])

    assert machine_api.body_params == %Plug.Conn.Unfetched{aspect: :body_params}
    assert machine_api.status == 401
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
