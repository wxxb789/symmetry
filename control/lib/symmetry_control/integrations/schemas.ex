defmodule SymmetryControl.Integrations.Connection do
  use Ecto.Schema
  import Ecto.Changeset

  alias SymmetryControl.Workspaces.ProjectResource

  @providers ["github", "azure_devops"]
  @statuses ["healthy", "degraded", "offline", "unknown"]
  @capabilities ["repositories", "work_items", "changes", "ci"]

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "external_connections" do
    field :provider, :string
    field :name, :string
    field :account_ref, :string
    field :auth_type, :string
    field :capabilities, {:array, :string}, default: []
    field :status, :string, default: "unknown"
    field :status_message, :string
    field :metadata, :map, default: %{}
    field :last_checked_at, :utc_datetime_usec
    field :lock_version, :integer, default: 1
    has_many :resources, ProjectResource

    has_many :provider_action_intents, SymmetryControl.Integrations.ProviderActionIntent,
      foreign_key: :connection_id

    timestamps(type: :utc_datetime_usec)
  end

  def changeset(connection, attrs) do
    connection
    |> cast(attrs, [
      :provider,
      :name,
      :account_ref,
      :auth_type,
      :capabilities,
      :status,
      :status_message,
      :metadata,
      :last_checked_at
    ])
    |> normalize()
    |> validate_connection()
    |> validate_inclusion(:status, @statuses)
    |> validate_status_message()
    |> unique_constraint([:provider, :name])
  end

  def update_changeset(connection, attrs) do
    connection
    |> cast(attrs, [
      :name,
      :account_ref,
      :capabilities,
      :status,
      :status_message,
      :metadata,
      :last_checked_at
    ])
    |> normalize()
    |> validate_connection()
    |> validate_inclusion(:status, @statuses)
    |> validate_status_message()
    |> unique_constraint([:provider, :name])
    |> optimistic_lock(:lock_version)
  end

  def health_changeset(connection, attrs) do
    connection
    |> cast(attrs, [:status, :status_message, :metadata, :last_checked_at])
    |> validate_required([:status, :metadata, :last_checked_at])
    |> validate_inclusion(:status, @statuses)
    |> validate_length(:status_message, max: 1_000)
    |> validate_status_message()
    |> check_constraint(:status, name: :external_connections_status_check)
    |> optimistic_lock(:lock_version)
  end

  def delete_changeset(connection) do
    connection
    |> change()
    |> no_assoc_constraint(:resources)
    |> no_assoc_constraint(:provider_action_intents)
    |> optimistic_lock(:lock_version)
  end

  defp normalize(changeset) do
    changeset
    |> update_change(:name, &trim/1)
    |> update_change(:account_ref, &trim/1)
    |> update_change(:capabilities, &(&1 |> Enum.uniq() |> Enum.sort()))
  end

  defp validate_connection(changeset) do
    changeset
    |> validate_required([
      :provider,
      :name,
      :account_ref,
      :auth_type,
      :capabilities
    ])
    |> validate_length(:name, min: 1, max: 160)
    |> validate_length(:account_ref, min: 1, max: 240)
    |> validate_format(:account_ref, ~r/^[A-Za-z0-9][A-Za-z0-9._-]*$/)
    |> validate_inclusion(:provider, @providers)
    |> validate_change(:capabilities, fn :capabilities, capabilities ->
      cond do
        capabilities == [] ->
          [capabilities: "must select at least one capability"]

        not Enum.all?(capabilities, &(&1 in @capabilities)) ->
          [capabilities: "contains an unsupported capability"]

        "changes" in capabilities and "repositories" not in capabilities ->
          [capabilities: "changes requires repositories"]

        true ->
          []
      end
    end)
    |> validate_auth_type()
    |> check_constraint(:provider, name: :external_connections_provider_check)
    |> check_constraint(:auth_type, name: :external_connections_auth_type_check)
  end

  defp validate_auth_type(changeset) do
    allowed =
      case get_field(changeset, :provider) do
        "github" -> ["gh_cli"]
        "azure_devops" -> ["entra_id"]
        _ -> []
      end

    validate_inclusion(changeset, :auth_type, allowed)
  end

  defp validate_status_message(changeset) do
    if get_field(changeset, :status) in ["degraded", "offline"] and
         blank?(get_field(changeset, :status_message)),
       do: add_error(changeset, :status_message, "must describe the connection failure"),
       else: changeset
  end

  defp blank?(value), do: is_nil(value) or (is_binary(value) and String.trim(value) == "")
  defp trim(value) when is_binary(value), do: String.trim(value)
  defp trim(value), do: value
end

defmodule SymmetryControl.Integrations.ProviderActionIntent do
  use Ecto.Schema
  import Ecto.Changeset

  alias SymmetryControl.Integrations.Connection
  alias SymmetryControl.Orchestration.{Run, Runtime, Task}
  alias SymmetryControl.Workspaces.{Project, ProjectResource, WorkItem}

  @states ["accepted", "executing", "succeeded", "failed", "unknown"]
  @operations ["resource.sync", "change.upsert", "change.update"]
  @accept_fields [
    :run_id,
    :task_id,
    :runtime_id,
    :project_id,
    :work_item_id,
    :resource_id,
    :connection_id,
    :action_id,
    :runtime_epoch,
    :generation,
    :claim_id,
    :operation,
    :request_hash,
    :input,
    :state,
    :provider,
    :account_ref,
    :resource_kind,
    :resource_external_ref,
    :resource_lock_version,
    :connection_lock_version,
    :work_item_lock_version
  ]

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "provider_action_intents" do
    belongs_to :run, Run
    belongs_to :task, Task
    belongs_to :runtime, Runtime
    belongs_to :project, Project
    belongs_to :work_item, WorkItem
    belongs_to :resource, ProjectResource
    belongs_to :connection, Connection
    field :action_id, Ecto.UUID
    field :runtime_epoch, :integer
    field :generation, :integer
    field :claim_id, Ecto.UUID
    field :operation, :string
    field :request_hash, :binary
    field :input, :map, default: %{}
    field :state, :string, default: "accepted"
    field :dispatch_token, Ecto.UUID
    field :provider, :string
    field :account_ref, :string
    field :resource_kind, :string
    field :resource_external_ref, :string
    field :resource_lock_version, :integer
    field :result, :map
    field :failure, :map
    field :connection_lock_version, :integer
    field :work_item_lock_version, :integer
    field :completed_at, :utc_datetime_usec
    timestamps(type: :utc_datetime_usec)
  end

  def accept_changeset(intent, attrs) do
    intent
    |> cast(attrs, @accept_fields)
    |> validate_required(@accept_fields)
    |> validate_inclusion(:operation, @operations)
    |> validate_inclusion(:state, ["accepted"])
    |> validate_inclusion(:provider, ["github", "azure_devops"])
    |> validate_inclusion(:resource_kind, ["repository", "work_tracking", "ci"])
    |> validate_length(:account_ref, min: 1, max: 500)
    |> validate_length(:resource_external_ref, min: 1, max: 500)
    |> validate_number(:runtime_epoch, greater_than: 0)
    |> validate_number(:generation, greater_than: 0)
    |> validate_number(:resource_lock_version, greater_than: 0)
    |> validate_number(:connection_lock_version, greater_than: 0)
    |> validate_number(:work_item_lock_version, greater_than: 0)
    |> validate_change(:request_hash, fn :request_hash, hash ->
      if is_binary(hash) and byte_size(hash) == 32,
        do: [],
        else: [request_hash: "must be a 32-byte digest"]
    end)
    |> unique_constraint([:run_id, :action_id])
    |> unique_constraint([:run_id, :resource_id],
      name: :provider_action_intents_active_resource
    )
  end

  def complete_changeset(intent, state, outcome, completed_at)
      when state in ["succeeded", "failed", "unknown"] and is_map(outcome) do
    {result, failure} = if state == "succeeded", do: {outcome, nil}, else: {nil, outcome}

    intent
    |> cast(
      %{
        state: state,
        dispatch_token: nil,
        result: result,
        failure: failure,
        completed_at: completed_at
      },
      [
        :state,
        :dispatch_token,
        :result,
        :failure,
        :completed_at
      ]
    )
    |> validate_completion()
  end

  def dispatch_changeset(intent, dispatch_token) do
    intent
    |> cast(
      %{
        state: "executing",
        dispatch_token: dispatch_token,
        result: nil,
        failure: nil,
        completed_at: nil
      },
      [:state, :dispatch_token, :result, :failure, :completed_at]
    )
    |> validate_required([:state, :dispatch_token])
    |> validate_inclusion(:state, ["executing"])
    |> check_constraint(:state, name: :provider_action_intents_state_check)
    |> check_constraint(:state, name: :provider_action_intents_outcome_check)
  end

  def retry_changeset(intent) do
    intent
    |> cast(
      %{state: "accepted", dispatch_token: nil, result: nil, failure: nil, completed_at: nil},
      [
        :state,
        :dispatch_token,
        :result,
        :failure,
        :completed_at
      ]
    )
    |> validate_required([:state])
    |> validate_inclusion(:state, ["accepted"])
    |> check_constraint(:state, name: :provider_action_intents_state_check)
    |> check_constraint(:state, name: :provider_action_intents_outcome_check)
  end

  defp validate_completion(changeset) do
    changeset
    |> validate_required([:state, :completed_at])
    |> validate_inclusion(:state, @states)
    |> check_constraint(:state, name: :provider_action_intents_state_check)
    |> check_constraint(:state, name: :provider_action_intents_outcome_check)
  end
end
