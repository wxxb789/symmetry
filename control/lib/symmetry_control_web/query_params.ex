defmodule SymmetryControlWeb.QueryParams do
  @moduledoc false

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

  def optional_positive_integer(nil), do: {:ok, nil}

  def optional_positive_integer(value) when is_integer(value) and value > 0, do: {:ok, value}

  def optional_positive_integer(value) when is_binary(value) do
    case Integer.parse(value) do
      {parsed, ""} when parsed > 0 -> {:ok, parsed}
      _ -> {:error, :invalid_request}
    end
  end

  def optional_positive_integer(_value), do: {:error, :invalid_request}

  def maybe_put(options, _key, nil), do: options
  def maybe_put(options, key, value), do: Keyword.put(options, key, value)
end
