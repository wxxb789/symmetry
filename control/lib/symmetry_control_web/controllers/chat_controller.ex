defmodule SymmetryControlWeb.ChatController do
  use SymmetryControlWeb, :controller

  alias SymmetryControl.Chat
  alias SymmetryControlWeb.{PortalJSON, Protocol}

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

  defp error(conn, %Ecto.Changeset{} = changeset) do
    conn
    |> put_status(:unprocessable_entity)
    |> json(%{
      error: %{
        code: "validation_failed",
        message: "request contains invalid fields",
        fields: PortalJSON.changeset_errors(changeset)
      }
    })
  end

  defp error(conn, reason), do: Protocol.error(conn, reason)
end
