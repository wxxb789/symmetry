defmodule SymmetryControl.Chat.Message do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "chat_messages" do
    field :scope_key, :string
    belongs_to :project, SymmetryControl.Workspaces.Project
    belongs_to :run, SymmetryControl.Orchestration.Run
    belongs_to :work_item, SymmetryControl.Workspaces.WorkItem
    belongs_to :command, SymmetryControl.Orchestration.Command
    field :role, :string
    field :intent, :string
    field :content, :string
    field :metadata, :map, default: %{}
    timestamps(type: :utc_datetime_usec, updated_at: false)
  end

  def changeset(message, attrs) do
    message
    |> cast(attrs, [
      :scope_key,
      :project_id,
      :run_id,
      :work_item_id,
      :command_id,
      :role,
      :intent,
      :content,
      :metadata
    ])
    |> validate_required([:scope_key, :role, :intent, :content])
    |> validate_inclusion(:role, ["human", "assistant"])
    |> validate_length(:content, max: 100_000)
    |> foreign_key_constraint(:work_item_id)
    |> foreign_key_constraint(:command_id)
  end
end

defmodule SymmetryControl.Chat.Action do
  use Ecto.Schema
  import Ecto.Changeset

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "chat_actions" do
    field :action_id, :string
    field :request_hash, :binary
    field :request_hash_version, :integer, default: 1
    belongs_to :message, SymmetryControl.Chat.Message
    belongs_to :reply, SymmetryControl.Chat.Message
    timestamps(type: :utc_datetime_usec, updated_at: false)
  end

  def changeset(action, attrs) do
    action
    |> cast(attrs, [:action_id, :request_hash, :request_hash_version, :message_id, :reply_id])
    |> validate_required([:action_id, :request_hash, :message_id, :reply_id])
    |> validate_inclusion(:request_hash_version, [1, 2])
    |> unique_constraint(:action_id)
    |> check_constraint(:request_hash_version, name: :chat_actions_request_hash_version_check)
  end
end
