defmodule SymmetryControlWeb.Plugs.EnrollmentAuth do
  @moduledoc false
  import Plug.Conn

  alias SymmetryControlWeb.Protocol

  def init(options), do: options

  def call(conn, _options) do
    Protocol.authenticate(conn, fn token ->
      case Protocol.credential_class(token) do
        :enrollment -> assign(conn, :enrollment_token, token)
        :unknown -> conn |> Protocol.error(:unauthenticated) |> halt()
        _known_credential -> conn |> Protocol.error(:forbidden) |> halt()
      end
    end)
  end
end

defmodule SymmetryControlWeb.Plugs.MachineAuth do
  @moduledoc false
  import Plug.Conn

  alias SymmetryControlWeb.Protocol

  def init(options), do: options

  def call(conn, _options) do
    Protocol.authenticate(conn, fn token ->
      case Protocol.credential_class(token) do
        {:machine, machine} ->
          assign(conn, :machine, machine)

        :unknown ->
          conn |> Protocol.error(:unauthenticated) |> halt()

        _known_credential ->
          conn |> Protocol.error(:forbidden) |> halt()
      end
    end)
  end
end

defmodule SymmetryControlWeb.Plugs.OperatorAuth do
  @moduledoc false
  import Plug.Conn

  alias SymmetryControlWeb.Protocol

  def init(options), do: options

  def call(conn, _options) do
    Protocol.authenticate(conn, fn token ->
      case Protocol.credential_class(token) do
        :operator -> conn
        :unknown -> conn |> Protocol.error(:unauthenticated) |> halt()
        _known_credential -> conn |> Protocol.error(:forbidden) |> halt()
      end
    end)
  end
end
