defmodule SymmetryControl.Integrations.HTTP do
  @moduledoc false

  @timeout 15_000

  def request(method, url, headers, body \\ nil) when method in [:get, :post, :patch, :put] do
    request = encode_request(url, headers, body)

    options = [
      autoredirect: false,
      timeout: @timeout,
      connect_timeout: 5_000,
      ssl: [
        verify: :verify_peer,
        cacerts: :public_key.cacerts_get(),
        customize_hostname_check: [match_fun: :public_key.pkix_verify_hostname_match_fun(:https)]
      ]
    ]

    case perform(method, request, options) do
      {:ok, {{_version, status, _reason}, response_headers, response_body}} ->
        {:ok, status, decode_headers(response_headers), decode_body(response_body)}

      {:error, reason} ->
        {:error, {:transport, reason}}

      {:rescue, error} ->
        {:error, {:transport, Exception.message(error)}}
    end
  end

  defp encode_request(url, headers, nil), do: {String.to_charlist(url), encode_headers(headers)}

  defp encode_request(url, headers, value) do
    {
      String.to_charlist(url),
      encode_headers(headers),
      ~c"application/json",
      Jason.encode!(value)
    }
  end

  # Only the socket call is rescued; the `{:error, reason}` clause above cannot
  # double-wrap this tagged outcome.
  defp perform(method, request, options) do
    try do
      :httpc.request(method, request, options, body_format: :binary)
    rescue
      error -> {:rescue, error}
    end
  end

  defp encode_headers(headers) do
    Enum.map(headers, fn {name, value} ->
      {String.to_charlist(to_string(name)), String.to_charlist(to_string(value))}
    end)
  end

  defp decode_headers(headers) do
    Map.new(headers, fn {name, value} ->
      {name |> to_string() |> String.downcase(), to_string(value)}
    end)
  end

  defp decode_body(""), do: nil

  defp decode_body(body) do
    case Jason.decode(body) do
      {:ok, decoded} -> decoded
      {:error, _reason} -> body
    end
  end
end
