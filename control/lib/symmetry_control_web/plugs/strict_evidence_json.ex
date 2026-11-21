defmodule SymmetryControlWeb.StrictJSONDecoder do
  @moduledoc """
  Ordered JSON decoder for the strict mutation routes.

  `SymmetryControlWeb.Plugs.StrictEvidenceJSON` installs it as the `:json`
  decoder for the strict-scope parser options, so Plug parses every strict
  mutation body exactly once: one ordered decode plus one
  duplicate-check-and-materialize walk, whose result becomes `body_params`.
  """

  defmodule DuplicateJSONKeyError do
    defexception message: "duplicate JSON object member"
  end

  @type decode_error :: :malformed_json | :expected_json_object | :duplicate_json_key

  @doc """
  Decodes one strict-scope JSON body, rejecting duplicate object members.

  Raises the duplicate-key error so `Plug.Parsers.JSON` wraps it in
  `Plug.Parsers.ParseError`; the plug maps that wrapped error back to the
  protocol envelope. Every other decode error is re-raised unchanged, so a
  malformed body keeps the parser's own rendering.
  """
  @spec decode!(binary()) :: term()
  def decode!(body) when is_binary(body) do
    case decode_ordered(body) do
      {:ok, value} ->
        case materialize(value) do
          {:ok, value} -> value
          {:error, :duplicate_json_key} -> raise DuplicateJSONKeyError
        end

      {:error, error} ->
        raise error
    end
  end

  @doc """
  Decodes one evidence body as a top-level JSON object without duplicates.

  The root shape is checked before the duplicate walk, so a non-object root
  keeps its `:expected_json_object` error even when it contains repeated
  members.
  """
  @spec decode_object(binary()) :: {:ok, map()} | {:error, decode_error()}
  def decode_object(body) when is_binary(body) do
    case decode_ordered(body) do
      {:ok, %Jason.OrderedObject{} = object} -> materialize(object)
      {:ok, _value} -> {:error, :expected_json_object}
      {:error, _error} -> {:error, :malformed_json}
    end
  end

  defp decode_ordered(body), do: Jason.decode(body, objects: :ordered_objects)

  defp materialize(%Jason.OrderedObject{values: values}) do
    materialize_pairs(values, %{})
  end

  defp materialize(values) when is_list(values) do
    materialize_list(values, [])
  end

  defp materialize(value), do: {:ok, value}

  defp materialize_pairs([], object), do: {:ok, object}

  defp materialize_pairs([{key, value} | rest], object) do
    if Map.has_key?(object, key) do
      {:error, :duplicate_json_key}
    else
      case materialize(value) do
        {:ok, value} -> materialize_pairs(rest, Map.put(object, key, value))
        {:error, reason} -> {:error, reason}
      end
    end
  end

  defp materialize_list([], values), do: {:ok, Enum.reverse(values)}

  defp materialize_list([value | rest], values) do
    case materialize(value) do
      {:ok, value} -> materialize_list(rest, [value | values])
      {:error, reason} -> {:error, reason}
    end
  end
end

defmodule SymmetryControlWeb.Plugs.StrictEvidenceJSON do
  @moduledoc """
  Strict JSON handling for the control-plane mutation routes.

  The machine evidence endpoint retains its raw body so the controller can
  defer materialization until it has authenticated and claimed run ownership.
  Every other JSON mutation route on `/api/v1` and `/portal/api` is decoded by
  `SymmetryControlWeb.StrictJSONDecoder`, which rejects duplicate JSON object
  members in the same pass that produces `body_params`. Routes outside that
  scope keep the plain JSON decoder.
  """

  @behaviour Plug

  alias Plug.Conn
  alias SymmetryControlWeb.{Protocol, StrictJSONDecoder}

  @raw_body_private_key :symmetry_control_raw_evidence_json

  @type materialize_error :: :not_captured | StrictJSONDecoder.decode_error()

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
      {:ok, raw_body} when is_binary(raw_body) -> StrictJSONDecoder.decode_object(raw_body)
      {:ok, _raw_body} -> {:error, :malformed_json}
      :error -> {:error, :not_captured}
    end
  end

  @impl true
  def init(opts) do
    parser_options = Plug.Parsers.init(opts)
    json_options!(parser_options)
    {parser_options, strict_decoder_options(parser_options)}
  end

  @impl true
  def call(
        %Conn{body_params: %Conn.Unfetched{}} = conn,
        {parser_options, strict_options}
      ) do
    path_info = Plug.Router.Utils.decode_path_info!(conn)

    cond do
      evidence_json_request?(conn, path_info) ->
        capture(conn, parser_options)

      strict_json_mutation_request?(conn, path_info) ->
        parse_strict_json_mutation(conn, strict_options)

      true ->
        Plug.Parsers.call(conn, parser_options)
    end
  end

  def call(conn, {parser_options, _strict_options}) do
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
    error in Plug.Parsers.ParseError ->
      case error.exception do
        %StrictJSONDecoder.DuplicateJSONKeyError{} -> reject_duplicate_json(conn)
        _other -> reraise error, __STACKTRACE__
      end
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

  defp strict_decoder_options({parsers, pass, query_string_length, validate_utf8}) do
    parsers = Enum.map(parsers, &strict_json_decoder/1)
    {parsers, pass, query_string_length, validate_utf8}
  end

  defp strict_json_decoder({Plug.Parsers.JSON, {body_reader, _decoder, body_options}}) do
    {Plug.Parsers.JSON, {body_reader, {StrictJSONDecoder, :decode!, []}, body_options}}
  end

  defp strict_json_decoder(parser), do: parser

  defp json_options!({parsers, _pass, _query_string_length, _validate_utf8}) do
    case Enum.find(parsers, fn {parser, _options} -> parser == Plug.Parsers.JSON end) do
      {_parser, options} -> options
      nil -> raise ArgumentError, "StrictEvidenceJSON expects the :json parser"
    end
  end
end
