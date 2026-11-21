defmodule SymmetryControlWeb.ChatController do
  use SymmetryControlWeb, :controller

  alias SymmetryControl.Chat
  alias SymmetryControlWeb.Protocol

  def index(conn, params) do
    case Chat.conversation(params) do
      {:ok, conversation} -> json(conn, conversation)
      {:error, reason} -> error(conn, reason)
    end
  end

  def create(conn, params) do
    case Chat.post_message(params) do
      {:ok, response, disposition} ->
        conn |> put_status(if(disposition == :created, do: :created, else: :ok)) |> json(response)

      {:error, reason} ->
        error(conn, reason)
    end
  end

  # A chat write can roll back a workspace changeset (`start_work`); those are
  # served with the field errors, every other refusal with its protocol status.
  defp error(conn, %Ecto.Changeset{} = changeset), do: Protocol.validation_error(conn, changeset)
  defp error(conn, reason), do: Protocol.error(conn, reason)
end
