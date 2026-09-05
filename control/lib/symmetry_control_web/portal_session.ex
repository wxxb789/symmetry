defmodule SymmetryControlWeb.PortalSession do
  @moduledoc false

  @default_max_age_seconds 8 * 60 * 60

  def issue(operator_token, opts \\ []) when is_binary(operator_token) do
    %{
      issued_at: Keyword.get(opts, :now, System.system_time(:second)),
      token_fingerprint: fingerprint(operator_token)
    }
  end

  def valid?(session, operator_token, opts \\ [])

  def valid?(session, operator_token, opts) when is_map(session) and is_binary(operator_token) do
    now = Keyword.get(opts, :now, System.system_time(:second))
    max_age_seconds = Keyword.get(opts, :max_age_seconds, configured_max_age_seconds())
    issued_at = value(session, :issued_at)
    stored_fingerprint = value(session, :token_fingerprint)
    expected_fingerprint = fingerprint(operator_token)

    is_integer(issued_at) and is_integer(max_age_seconds) and max_age_seconds > 0 and
      now >= issued_at and now - issued_at < max_age_seconds and
      is_binary(stored_fingerprint) and
      byte_size(stored_fingerprint) == byte_size(expected_fingerprint) and
      Plug.Crypto.secure_compare(stored_fingerprint, expected_fingerprint)
  end

  def valid?(_session, _operator_token, _opts), do: false

  def configured_max_age_seconds do
    :symmetry_control
    |> Application.get_env(:orchestration, [])
    |> Keyword.get(:portal_session_max_age_seconds, @default_max_age_seconds)
  end

  defp fingerprint(token),
    do: token |> then(&:crypto.hash(:sha256, &1)) |> Base.encode16(case: :lower)

  defp value(map, key), do: Map.get(map, key) || Map.get(map, Atom.to_string(key))
end
