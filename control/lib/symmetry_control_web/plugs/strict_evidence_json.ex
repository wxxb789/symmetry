defmodule SymmetryControlWeb.Plugs.StrictEvidenceJSON do
  @moduledoc """
  Rejects duplicate JSON object members before the normal JSON parser runs.

  The machine evidence endpoint retains its raw body so the controller can
  defer materialization until it has authenticated and claimed run ownership.
  Other JSON mutation routes are parsed normally after the duplicate-member
  check.
  """

  @behaviour Plug

  alias Plug.Conn
  alias SymmetryControlWeb.Protocol

  defmodule DuplicateJSONKeyError do
    defexception message: "duplicate JSON object member"
  end

  @raw_body_private_key :symmetry_control_raw_evidence_json

  @type materialize_error ::
          :not_captured
          | :malformed_json
          | :expected_json_object
          | :duplicate_json_key

  @mutation_methods ~w(POST PUT PATCH DELETE)

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
    _json_options = json_options!(parser_options)
    strict_parser_options = strict_parser_options(parser_options)
    {parser_options, strict_parser_options}
  end

  @impl true
  def call(
        %Conn{body_params: %Conn.Unfetched{}} = conn,
        {parser_options, strict_parser_options}
      ) do
    path_info = Plug.Router.Utils.decode_path_info!(conn)

    cond do
      evidence_json_request?(conn, path_info) ->
        capture(conn, parser_options)

      strict_json_mutation_request?(conn, path_info) ->
        parse_strict_json_mutation(conn, strict_parser_options)

      true ->
        Plug.Parsers.call(conn, parser_options)
    end
  end

  def call(conn, {parser_options, _strict_parser_options}) do
    Plug.Parsers.call(conn, parser_options)
  end

  defp capture(conn, parser_options) do
    {body_reader, _decoder, body_options} = json_options!(parser_options)

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

  defp evidence_json_request?(%Conn{method: "POST"} = conn, [
         "api",
         "v1",
         "runs",
         run_id,
         "evidence"
       ])
       when is_binary(run_id) and run_id != "" do
    json_content_type?(conn)
  end

  defp evidence_json_request?(_conn, _path_info), do: false

  defp strict_json_mutation_request?(%Conn{method: method} = conn, path_info)
       when method in @mutation_methods do
    json_mutation_path?(path_info) and json_content_type?(conn)
  end

  defp strict_json_mutation_request?(_conn, _path_info), do: false

  defp json_mutation_path?(path_info) do
    case path_info do
      ["api", "v1" | _] -> true
      ["portal", "api" | _] -> true
      _ -> false
    end
  end

  defp parse_strict_json_mutation(conn, parser_options) do
    Plug.Parsers.call(conn, parser_options)
  rescue
    DuplicateJSONKeyError -> reject_duplicate_json(conn)
  end

  defp reject_duplicate_json(conn) do
    conn
    |> Protocol.error(:invalid_request)
    |> Conn.halt()
  end

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

  defp strict_parser_options({parsers, pass, query_string_length, validate_utf8}) do
    parsers = Enum.map(parsers, &strict_json_parser/1)
    {parsers, pass, query_string_length, validate_utf8}
  end

  defp strict_json_parser({Plug.Parsers.JSON, {body_reader, decoder, body_options}}) do
    {Plug.Parsers.JSON, {{__MODULE__, :read_json_body, [body_reader]}, decoder, body_options}}
  end

  defp strict_json_parser(parser), do: parser

  defp json_options!({parsers, _pass, _query_string_length, _validate_utf8}) do
    case Enum.find(parsers, fn {parser, _options} -> parser == Plug.Parsers.JSON end) do
      {_parser, options} -> options
      nil -> raise ArgumentError, "StrictEvidenceJSON expects the :json parser"
    end
  end

  @doc false
  def read_json_body(conn, options, body_reader) do
    case read_body(conn, body_reader, options) do
      {:ok, body, conn} ->
        case validate_duplicate_json_members(body) do
          :ok -> {:ok, body, conn}
          {:error, :duplicate_json_key} -> raise DuplicateJSONKeyError
        end

      other ->
        other
    end
  end

  defp validate_duplicate_json_members(raw_body) when is_binary(raw_body) do
    case Jason.decode(raw_body, objects: :ordered_objects) do
      {:ok, value} ->
        case materialize_value(value) do
          {:ok, _value} -> :ok
          {:error, reason} -> {:error, reason}
        end

      {:error, _reason} ->
        :ok
    end
  end

  defp validate_duplicate_json_members(_raw_body), do: :ok

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
