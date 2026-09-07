defmodule SymmetryControl.Repo.Migrations.CreateChat do
  use Ecto.Migration

  def change do
    create table(:chat_messages, primary_key: false) do
      add :id, :uuid, primary_key: true
      add :scope_key, :text, null: false
      add :project_id, references(:projects, type: :uuid, on_delete: :restrict)
      add :run_id, references(:runs, type: :uuid, on_delete: :restrict)
      add :work_item_id, references(:work_items, type: :uuid, on_delete: :restrict)
      add :command_id, references(:commands, type: :uuid, on_delete: :restrict)
      add :role, :text, null: false
      add :intent, :text, null: false
      add :content, :text, null: false
      add :metadata, :map, null: false, default: %{}
      timestamps(type: :utc_datetime_usec, updated_at: false)
    end

    create index(:chat_messages, [:scope_key, :inserted_at, :id])

    create constraint(:chat_messages, :chat_messages_role_check,
             check: "role IN ('human', 'assistant')"
           )

    create table(:chat_actions, primary_key: false) do
      add :id, :uuid, primary_key: true
      add :action_id, :text, null: false
      add :request_hash, :binary, null: false
      add :message_id, references(:chat_messages, type: :uuid, on_delete: :restrict), null: false
      add :reply_id, references(:chat_messages, type: :uuid, on_delete: :restrict), null: false
      timestamps(type: :utc_datetime_usec, updated_at: false)
    end

    create unique_index(:chat_actions, [:action_id])
  end
end
