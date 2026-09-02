defmodule SymmetryControlWeb.Plugs.EnrollmentAuth do
  @moduledoc false
  import Plug.Conn

  alias SymmetryControlWeb.Protocol

  def init(options), do: options

  def call(conn, _options) do
    with {:ok, token} <- Protocol.machine_token(conn),
         true <- Protocol.secure_compare(token, Protocol.configured_token(:enrollment_token)) do
      assign(conn, :enrollment_token, token)
    else
      _ -> conn |> Protocol.error(:unauthenticated) |> halt()
    end
  end
end

defmodule SymmetryControlWeb.Plugs.MachineAuth do
  @moduledoc false
  import Plug.Conn

  alias SymmetryControl.Orchestration
  alias SymmetryControlWeb.Protocol

  def init(options), do: options

  def call(conn, _options) do
    with {:ok, token} <- Protocol.machine_token(conn) do
      case Orchestration.authenticate_machine(token) do
        {:ok, machine} ->
          assign(conn, :machine, machine)

        {:error, :unauthenticated} ->
          if Protocol.secure_compare(token, Protocol.configured_token(:operator_token)) do
            conn |> Protocol.error(:forbidden) |> halt()
          else
            conn |> Protocol.error(:unauthenticated) |> halt()
          end
      end
    else
      _ -> conn |> Protocol.error(:unauthenticated) |> halt()
    end
  end
end

defmodule SymmetryControlWeb.Plugs.OperatorAuth do
  @moduledoc false
  import Plug.Conn

  alias SymmetryControlWeb.Protocol

  def init(options), do: options

  def call(conn, _options) do
    with {:ok, token} <- Protocol.machine_token(conn) do
      if Protocol.secure_compare(token, Protocol.configured_token(:operator_token)) do
        conn
      else
        conn |> Protocol.error(:forbidden) |> halt()
      end
    else
      _ -> conn |> Protocol.error(:unauthenticated) |> halt()
    end
  end
end
