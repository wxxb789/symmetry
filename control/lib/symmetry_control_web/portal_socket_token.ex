defmodule SymmetryControlWeb.PortalSocketToken do
  @moduledoc """
  Short-lived, audience-scoped token for the operator invalidation socket.

  The token is minted only by the authenticated same-origin bootstrap
  (`GET /portal/api/session`) and verified when a socket connects. It carries no
  operator credential: the operator bearer token stays in the login POST and the
  session cookie, never in the browser's JavaScript or storage. Its lifetime is
  short by configuration (`:portal_socket_token_max_age_seconds`) so a token
  copied out of a live page stops working on its own, and the browser fetches a
  fresh one from the same bootstrap before every connect attempt.
  """

  @salt "portal-socket-v1"
  @audience "portal:operator"
  @default_max_age_seconds 300

  def audience, do: @audience

  def sign do
    Phoenix.Token.sign(SymmetryControlWeb.Endpoint, @salt, %{audience: @audience})
  end

  @doc """
  Verifies a token and returns its audience. Expired, tampered or foreign-audience
  tokens are refused; there is no other credential this socket accepts.
  """
  def verify(token) when is_binary(token) and byte_size(token) > 0 do
    case Phoenix.Token.verify(SymmetryControlWeb.Endpoint, @salt, token,
           max_age: max_age_seconds()
         ) do
      {:ok, payload} when is_map(payload) -> audience_of(payload)
      {:error, reason} -> {:error, reason}
    end
  end

  def verify(_token), do: {:error, :unauthenticated}

  # Phoenix.Token delegates to Plug.Crypto, which signs the payload with
  # `:erlang.term_to_binary/2` and verifies it with a non-executable
  # binary-to-term read, so the atom-keyed map arrives with its atoms intact:
  # `:audience` is the key that actually arrives. The string read stays as the
  # fallback.
  defp audience_of(payload) do
    case Map.get(payload, "audience") || Map.get(payload, :audience) do
      @audience -> {:ok, @audience}
      _other -> {:error, :unauthorized}
    end
  end

  def max_age_seconds do
    :symmetry_control
    |> Application.get_env(:orchestration, [])
    |> Keyword.get(:portal_socket_token_max_age_seconds, @default_max_age_seconds)
  end
end
