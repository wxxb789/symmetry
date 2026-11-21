defmodule SymmetryControlWeb.QueryParams do
  @moduledoc false

  import Plug.Conn

  @max_integer 9_223_372_036_854_775_807

  # The router's own path params travel with the query params in `conn.params`,
  # so a read's allowlist only governs the parameters a caller actually added.
  def allowed_query_params?(conn, params, allowed) do
    path_keys = MapSet.new(Map.keys(conn.path_params))

    params
    |> Map.keys()
    |> MapSet.new()
    |> MapSet.difference(path_keys)
    |> MapSet.subset?(MapSet.new(allowed))
  end

  def optional_text(nil), do: {:ok, nil}
  def optional_text(value) when is_binary(value) and byte_size(value) > 0, do: {:ok, value}
  def optional_text(_value), do: {:error, :invalid_request}

  @doc "The largest integer the durable integer columns accept."
  def max_integer, do: @max_integer

  # One integer family for every numeric wire parameter. `bounds` is the closed
  # range the call site accepts — `{min, max}` or `{min, :infinity}` for the
  # sites that never had an upper bound — and the carrier the site reads is the
  # function: a JSON body carries an integer (`json_integer/2`), a query string
  # carries decimal text (`query_integer/2`; `Integer.parse/1` accepts the
  # `"+1"` sign exactly as every baseline query parser did), and the two sites that
  # already accepted either form keep `wire_integer/2`. `optional_wire_integer/2`
  # additionally passes an absent parameter through as `nil`.
  def json_integer(value, bounds) when is_integer(value), do: in_bounds(value, bounds)
  def json_integer(_value, _bounds), do: {:error, :invalid_request}

  def query_integer(value, bounds) when is_binary(value), do: text_in_bounds(value, bounds)
  def query_integer(_value, _bounds), do: {:error, :invalid_request}

  def wire_integer(value, bounds) when is_integer(value), do: in_bounds(value, bounds)
  def wire_integer(value, bounds) when is_binary(value), do: text_in_bounds(value, bounds)
  def wire_integer(_value, _bounds), do: {:error, :invalid_request}

  def optional_wire_integer(nil, _bounds), do: {:ok, nil}
  def optional_wire_integer(value, bounds), do: wire_integer(value, bounds)

  defp text_in_bounds(value, bounds) do
    case Integer.parse(value) do
      {parsed, ""} -> in_bounds(parsed, bounds)
      _ -> {:error, :invalid_request}
    end
  end

  defp in_bounds(value, {min, :infinity}) when value >= min, do: {:ok, value}
  defp in_bounds(value, {min, max}) when value >= min and value <= max, do: {:ok, value}
  defp in_bounds(_value, _bounds), do: {:error, :invalid_request}

  @doc """
  The body a controller parses: the decoded JSON map, or the empty map a request
  without a parseable body carries.
  """
  def body_params(conn) when is_map(conn.body_params), do: conn.body_params
  def body_params(_conn), do: %{}

  @doc "The replay key every write route requires exactly once."
  def idempotency_key(conn) do
    case get_req_header(conn, "idempotency-key") do
      [key] when byte_size(key) > 0 -> {:ok, key}
      _ -> {:error, :invalid_request}
    end
  end

  def maybe_put(options, _key, nil), do: options
  def maybe_put(options, key, value), do: Keyword.put(options, key, value)
end
