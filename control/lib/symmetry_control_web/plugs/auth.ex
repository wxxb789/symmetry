defmodule SymmetryControlWeb.Plugs.CredentialAuth do
  @moduledoc false
  import Plug.Conn

  alias SymmetryControlWeb.Protocol

  # One credential-classification policy for the token pipelines: the pipeline declares
  # the class it accepts; precedence stays in `Protocol.credential_class/1`.
  def init(options), do: options

  def call(conn, options) do
    accepts = Keyword.fetch!(options, :accepts)

    Protocol.authenticate(conn, fn token -> classify(conn, token, accepts) end)
  end

  defp classify(conn, token, accepts) do
    case {accepts, Protocol.credential_class(token)} do
      {:enrollment, :enrollment} -> assign(conn, :enrollment_token, token)
      {:machine, {:machine, machine}} -> assign(conn, :machine, machine)
      {:operator, :operator} -> conn
      {_accepts, :unknown} -> conn |> Protocol.error(:unauthenticated) |> halt()
      {_accepts, _known_credential} -> conn |> Protocol.error(:forbidden) |> halt()
    end
  end
end
