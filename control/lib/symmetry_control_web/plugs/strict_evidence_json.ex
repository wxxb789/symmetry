defmodule SymmetryControlWeb.Plugs.StrictEvidenceJSON do
  @moduledoc """
  Captures the raw JSON body for the machine evidence endpoint.

  All requests outside that endpoint are delegated to Plug.Parsers with the
  exact options supplied to this plug. Evidence JSON is deliberately not
  decoded until the controller has authenticated and claimed run ownership.
  """

  @behaviour Plug

  alias Plug.Conn

  @raw_body_private_key :symmetry_control_raw_evidence_json

  @type materialize_error ::
          :not_captured
          | :malformed_json
          | :expected_json_object
          | :duplicate_json_key

  @doc """
  Returns the private connection key used for a captured evidence body.

  The key is public so a caller that needs to inspect the capture does not
  duplicate an implementation detail as a bare atom.
  """
  @spec raw_body_private_key() :: atom()
  def raw_body_private_key, do: @raw_body_private_key

  @doc """
  Materializes a captured evidence body after authorization has completed.

  The decoder retains object order while decoding so duplicate keys can be
  rejected before converting objects into ordinary maps. Errors intentionally
  contain no decoder exception or raw request body.
  """
  @spec materialize(Conn.t()) :: {:ok, map()} | {:error, materialize_error()}
  def materialize(%Conn{private: private}) do
    case Map.fetch(private, @raw_body_private_key) do
      {:ok, ""} -> {:ok, %{}}
      {:ok, raw_body} when is_binary(raw_body) -> decode_object(raw_body)
      {:ok, _raw_body} -> {:error, :malformed_json}
      :error -> {:error, :not_captured}
    end
  end

  @impl true
  def init(opts) do
    parser_options = Plug.Parsers.init(opts)
    json_options = json_options!(parser_options)
    {parser_options, json_options}
  end

  @impl true
  def call(%Conn{body_params: %Conn.Unfetched{}} = conn, {parser_options, json_options}) do
    if evidence_json_request?(conn) do
      capture(conn, parser_options, json_options)
    else
      Plug.Parsers.call(conn, parser_options)
    end
  end

  def call(conn, {parser_options, _json_options}) do
    Plug.Parsers.call(conn, parser_options)
  end

  defp capture(conn, parser_options, {body_reader, _decoder, body_options}) do
    case read_body(conn, body_reader, body_options) do
      {:ok, body, conn} ->
        conn
        |> Conn.put_private(@raw_body_private_key, body)
        |> Map.put(:body_params, %{})
        |> then(&Plug.Parsers.call(&1, parser_options))

      {:more, _body, _conn} ->
        raise Plug.Parsers.RequestTooLargeError

      {:error, :timeout} ->
        raise Plug.TimeoutError

      {:error, _reason} ->
        raise Plug.BadRequestError
    end
  end

  defp read_body(conn, {module, function, args}, options)
       when is_atom(module) and is_atom(function) and is_list(args) do
    apply(module, function, [conn, options | args])
  end

  defp evidence_json_request?(
         %Conn{
           method: "POST",
           path_info: ["api", "v1", "runs", run_id, "evidence"],
           request_path: request_path
         } = conn
       )
       when is_binary(run_id) and run_id != "" do
    request_path == "/api/v1/runs/" <> run_id <> "/evidence" and json_content_type?(conn)
  end

  defp evidence_json_request?(_conn), do: false

  defp json_content_type?(%Conn{} = conn) do
    case List.keyfind(conn.req_headers, "content-type", 0) do
      {"content-type", content_type} ->
        case Conn.Utils.content_type(content_type) do
          {:ok, "application", subtype, _params} ->
            subtype == "json" or String.ends_with?(subtype, "+json")

          _ ->
            false
        end

      nil ->
        false
    end
  end

  defp json_options!({parsers, _pass, _query_string_length, _validate_utf8}) do
    case Enum.find(parsers, fn {parser, _options} -> parser == Plug.Parsers.JSON end) do
      {_parser, options} -> options
      nil -> raise ArgumentError, "StrictEvidenceJSON expects the :json parser"
    end
  end

  defp decode_object(raw_body) do
    case Jason.decode(raw_body, objects: :ordered_objects) do
      {:ok, %Jason.OrderedObject{} = object} -> materialize_object(object)
      {:ok, _value} -> {:error, :expected_json_object}
      {:error, %Jason.DecodeError{}} -> {:error, :malformed_json}
      {:error, _reason} -> {:error, :malformed_json}
    end
  end

  defp materialize_object(%Jason.OrderedObject{values: values}) do
    materialize_pairs(values, %{})
  end

  defp materialize_pairs([], object), do: {:ok, object}

  defp materialize_pairs([{key, value} | rest], object) do
    if Map.has_key?(object, key) do
      {:error, :duplicate_json_key}
    else
      case materialize_value(value) do
        {:ok, value} -> materialize_pairs(rest, Map.put(object, key, value))
        {:error, reason} -> {:error, reason}
      end
    end
  end

  defp materialize_value(%Jason.OrderedObject{values: values}) do
    materialize_pairs(values, %{})
  end

  defp materialize_value(values) when is_list(values) do
    materialize_list(values, [])
  end

  defp materialize_value(value), do: {:ok, value}

  defp materialize_list([], values), do: {:ok, Enum.reverse(values)}

  defp materialize_list([value | rest], values) do
    case materialize_value(value) do
      {:ok, value} -> materialize_list(rest, [value | values])
      {:error, reason} -> {:error, reason}
    end
  end
end
