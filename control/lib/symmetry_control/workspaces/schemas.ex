defmodule SymmetryControl.Workspaces.ChangesetValidators do
  @moduledoc false

  import Ecto.Changeset

  def validate_http_url(changeset, field) do
    validate_change(changeset, field, fn ^field, value ->
      case URI.parse(value) do
        %URI{scheme: scheme, host: host}
        when is_binary(scheme) and is_binary(host) and host != "" ->
          if String.downcase(scheme) in ["http", "https"],
            do: [],
            else: [{field, "must be an http or https URL"}]

        _uri ->
          [{field, "must be an http or https URL"}]
      end
    end)
  end
end

defmodule SymmetryControl.Workspaces.Project do
  use Ecto.Schema
  import Ecto.Changeset

  alias SymmetryControl.Workspaces.{ProjectResource, WorkItem}

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "projects" do
    field :name, :string
    field :key, :string
    field :description, :string
    field :status, :string, default: "active"
    field :default_agent_profile, :string, default: "default"
    field :default_workspace, :string, default: "primary"
    field :lock_version, :integer, default: 1
    has_many :resources, ProjectResource
    has_many :work_items, WorkItem
    timestamps(type: :utc_datetime_usec)
  end

  def changeset(project, attrs) do
    project
    |> cast(attrs, [
      :name,
      :key,
      :description,
      :status,
      :default_agent_profile,
      :default_workspace
    ])
    |> update_change(:name, &trim/1)
    |> update_change(:key, &normalize_key/1)
    |> validate_project()
    |> unique_constraint(:key)
  end

  def update_changeset(project, attrs) do
    project
    |> cast(attrs, [:name, :description, :status, :default_agent_profile, :default_workspace])
    |> update_change(:name, &trim/1)
    |> reject_key_change(attrs)
    |> validate_project()
    |> optimistic_lock(:lock_version)
  end

  defp validate_project(changeset) do
    changeset
    |> validate_required([:name, :key, :status, :default_agent_profile, :default_workspace])
    |> validate_length(:name, min: 1, max: 120)
    |> validate_length(:description, max: 4_000)
    |> validate_length(:default_agent_profile, min: 1, max: 120)
    |> validate_length(:default_workspace, min: 1, max: 240)
    |> validate_format(:key, ~r/^[A-Z][A-Z0-9]{1,7}$/)
    |> validate_inclusion(:status, ["active", "archived"])
    |> check_constraint(:status, name: :projects_status_check)
  end

  defp reject_key_change(changeset, attrs) do
    requested = Map.get(attrs, :key) || Map.get(attrs, "key")

    if is_binary(requested) and normalize_key(requested) != changeset.data.key,
      do: add_error(changeset, :key, "cannot be changed"),
      else: changeset
  end

  defp normalize_key(value) when is_binary(value), do: value |> String.trim() |> String.upcase()
  defp normalize_key(value), do: value

  defp trim(value) when is_binary(value), do: String.trim(value)
  defp trim(value), do: value
end

defmodule SymmetryControl.Workspaces.ProjectResource do
  use Ecto.Schema
  import Ecto.Changeset

  alias SymmetryControl.Integrations.Connection
  alias SymmetryControl.Workspaces.Project
  alias SymmetryControl.Workspaces.ChangesetValidators

  @kinds ["repository", "work_tracking", "ci", "agent", "runtime", "connection"]
  @statuses ["healthy", "degraded", "offline", "unknown"]
  @sync_statuses ["unknown", "syncing", "synced", "stale", "failed"]
  @connected_identity_index :project_resources_connected_identity

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "project_resources" do
    belongs_to :project, Project
    belongs_to :connection, Connection
    field :kind, :string
    field :name, :string
    field :provider, :string
    field :external_ref, :string
    field :url, :string
    field :status, :string, default: "unknown"
    field :sync_status, :string, default: "unknown"
    field :status_message, :string
    field :metadata, :map, default: %{}
    field :last_checked_at, :utc_datetime_usec
    field :last_synced_at, :utc_datetime_usec
    field :lock_version, :integer, default: 1

    has_many :provider_action_intents, SymmetryControl.Integrations.ProviderActionIntent,
      foreign_key: :resource_id

    timestamps(type: :utc_datetime_usec)
  end

  def changeset(resource, attrs) do
    resource
    |> cast(attrs, [
      :project_id,
      :connection_id,
      :kind,
      :name,
      :provider,
      :external_ref,
      :url,
      :status,
      :sync_status,
      :status_message,
      :metadata,
      :last_checked_at,
      :last_synced_at
    ])
    |> update_change(:name, &trim/1)
    |> update_change(:external_ref, &trim/1)
    |> validate_resource()
    |> assoc_constraint(:project)
    |> assoc_constraint(:connection)
    |> unique_constraint([:project_id, :kind, :name])
    |> connected_identity_constraint()
  end

  def update_changeset(resource, attrs) do
    resource
    |> cast(attrs, [
      :connection_id,
      :kind,
      :name,
      :provider,
      :external_ref,
      :url,
      :status,
      :sync_status,
      :status_message,
      :metadata,
      :last_checked_at,
      :last_synced_at
    ])
    |> update_change(:name, &trim/1)
    |> update_change(:external_ref, &trim/1)
    |> validate_resource()
    |> assoc_constraint(:connection)
    |> unique_constraint([:project_id, :kind, :name])
    |> connected_identity_constraint()
    |> optimistic_lock(:lock_version)
  end

  def sync_changeset(resource, attrs) do
    resource
    |> cast(attrs, [
      :provider,
      :url,
      :status,
      :sync_status,
      :status_message,
      :metadata,
      :last_checked_at,
      :last_synced_at
    ])
    |> validate_resource()
    |> optimistic_lock(:lock_version)
  end

  def delete_changeset(resource) do
    resource
    |> change()
    |> no_assoc_constraint(:provider_action_intents)
    |> optimistic_lock(:lock_version)
  end

  defp validate_resource(changeset) do
    changeset
    |> validate_required([:project_id, :kind, :name, :status, :sync_status])
    |> validate_length(:name, min: 1, max: 160)
    |> validate_length(:provider, max: 120)
    |> validate_length(:external_ref, max: 500)
    |> validate_length(:status_message, max: 1_000)
    |> validate_inclusion(:kind, @kinds)
    |> validate_inclusion(:status, @statuses)
    |> validate_inclusion(:sync_status, @sync_statuses)
    |> validate_registered_reference_required()
    |> validate_connected_reference()
    |> ChangesetValidators.validate_http_url(:url)
    |> validate_attention_message()
    |> check_constraint(:kind, name: :project_resources_kind_check)
    |> check_constraint(:status, name: :project_resources_status_check)
    |> check_constraint(:sync_status, name: :project_resources_sync_status_check)
  end

  defp validate_registered_reference_required(changeset) do
    if get_field(changeset, :kind) in ["agent", "runtime"],
      do: validate_required(changeset, [:external_ref]),
      else: changeset
  end

  defp validate_connected_reference(changeset) do
    connection_id = get_field(changeset, :connection_id)
    kind = get_field(changeset, :kind)

    cond do
      is_nil(connection_id) ->
        changeset

      kind in ["repository", "work_tracking", "ci"] ->
        validate_required(changeset, [:external_ref])

      true ->
        add_error(changeset, :connection_id, "can only be used by external engineering resources")
    end
  end

  defp connected_identity_constraint(changeset) do
    unique_constraint(changeset, :external_ref,
      name: @connected_identity_index,
      message: "has already been connected for this resource kind"
    )
  end

  defp validate_attention_message(changeset) do
    needs_attention? =
      get_field(changeset, :status) in ["degraded", "offline"] or
        get_field(changeset, :sync_status) in ["stale", "failed"]

    if needs_attention? and blank?(get_field(changeset, :status_message)),
      do:
        add_error(
          changeset,
          :status_message,
          "must be present when health or synchronization needs attention"
        ),
      else: changeset
  end

  defp blank?(value), do: is_nil(value) or (is_binary(value) and String.trim(value) == "")
  defp trim(value) when is_binary(value), do: String.trim(value)
  defp trim(value), do: value
end

defmodule SymmetryControl.Workspaces.WorkItem do
  use Ecto.Schema
  import Ecto.Changeset

  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Workspaces.{ChangesetValidators, Project, ProjectResource}

  @statuses ["backlog", "ready", "in_progress", "review", "done"]
  @priorities ["urgent", "high", "medium", "low", "no_priority"]
  @assignee_types ["unassigned", "human", "agent"]
  @ci_statuses ["unknown", "pending", "passed", "failed"]
  @review_statuses ["none", "required", "changes_requested", "approved"]
  @pull_request_states ["unknown", "open", "closed", "merged"]
  @provider_owned_fields [:title, :description, :priority]

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "work_items" do
    field :number, :integer, read_after_writes: true
    belongs_to :project, Project
    belongs_to :orchestration_task, Task
    belongs_to :repository_resource, ProjectResource
    belongs_to :ci_resource, ProjectResource
    belongs_to :external_work_item_resource, ProjectResource
    field :title, :string
    field :description, :string
    field :status, :string, default: "backlog"
    field :priority, :string, default: "no_priority"
    field :position, :integer, default: 0
    field :assignee_type, :string, default: "unassigned"
    field :assignee_name, :string
    field :agent_profile, :string
    field :workspace, :string
    field :blocked, :boolean, default: false
    field :blocker, :string
    field :repository, :string
    field :branch, :string
    field :pull_request_url, :string
    field :ci_status, :string
    field :review_status, :string
    field :external_provider, :string
    field :external_id, :string
    field :external_url, :string
    field :external_state, :string
    field :external_updated_at, :utc_datetime_usec
    field :external_available, :boolean, default: true
    field :external_assignee_name, :string
    field :labels, {:array, :string}, default: []
    field :external_data, :map, default: %{}
    field :external_pull_request_url, :string
    field :external_pull_request_state, :string
    field :external_ci_status, :string
    field :external_review_status, :string
    field :external_change_updated_at, :utc_datetime_usec
    field :external_ci_updated_at, :utc_datetime_usec
    field :external_change_data, :map, default: %{}
    field :external_ci_data, :map, default: %{}
    field :lock_version, :integer, default: 1
    timestamps(type: :utc_datetime_usec)
  end

  def provider_owned_field?(field), do: field in @provider_owned_fields

  def external_work_available?(%{external_work_item_resource_id: nil}), do: true
  def external_work_available?(%{external_available: available}), do: available

  def changeset(work_item, attrs) do
    work_item
    |> cast(attrs, [
      :project_id,
      :repository_resource_id,
      :ci_resource_id,
      :title,
      :description,
      :status,
      :priority,
      :position,
      :assignee_type,
      :assignee_name,
      :agent_profile,
      :workspace,
      :blocked,
      :blocker,
      :repository,
      :branch,
      :pull_request_url,
      :ci_status,
      :review_status
    ])
    |> validate_work_item()
    |> assoc_constraint(:project)
  end

  def update_changeset(work_item, attrs) do
    work_item
    |> cast(attrs, [
      :title,
      :description,
      :priority,
      :repository_resource_id,
      :ci_resource_id,
      :assignee_type,
      :assignee_name,
      :agent_profile,
      :workspace,
      :blocked,
      :blocker,
      :repository,
      :branch,
      :pull_request_url,
      :ci_status,
      :review_status
    ])
    |> clear_changed_delivery_bindings()
    |> reject_move_fields(attrs)
    |> validate_work_item()
    |> optimistic_lock(:lock_version)
  end

  def move_changeset(work_item, attrs) do
    work_item
    |> cast(attrs, [:status, :position])
    |> validate_required([:status, :position])
    |> validate_number(:position, greater_than_or_equal_to: 0)
    |> validate_inclusion(:status, @statuses)
    |> optimistic_lock(:lock_version)
  end

  defp validate_work_item(changeset), do: validate_work_item(changeset, 240, 20_000)

  defp validate_work_item(changeset, title_limit, description_limit) do
    changeset
    |> update_change(:title, &trim/1)
    |> validate_required([
      :project_id,
      :title,
      :status,
      :priority,
      :position,
      :assignee_type,
      :blocked
    ])
    |> validate_length(:title, min: 1, max: title_limit)
    |> validate_length(:description, max: description_limit)
    |> validate_length(:assignee_name, max: 120)
    |> validate_length(:agent_profile, max: 120)
    |> validate_length(:workspace, max: 240)
    |> validate_length(:blocker, max: 1_000)
    |> validate_length(:repository, max: 500)
    |> validate_length(:branch, max: 500)
    |> validate_number(:position, greater_than_or_equal_to: 0)
    |> validate_inclusion(:status, @statuses)
    |> validate_inclusion(:priority, @priorities)
    |> validate_inclusion(:assignee_type, @assignee_types)
    |> validate_inclusion(:ci_status, @ci_statuses)
    |> validate_inclusion(:review_status, @review_statuses)
    |> ChangesetValidators.validate_http_url(:pull_request_url)
    |> validate_blocker()
    |> validate_assignee()
    |> check_constraint(:status, name: :work_items_status_check)
    |> check_constraint(:priority, name: :work_items_priority_check)
    |> check_constraint(:assignee_type, name: :work_items_assignee_type_check)
    |> check_constraint(:ci_status, name: :work_items_ci_status_check)
    |> check_constraint(:review_status, name: :work_items_review_status_check)
    |> check_constraint(:blocker, name: :work_items_blocker_check)
    |> check_constraint(:assignee_type, name: :work_items_owner_fields_check)
    |> assoc_constraint(:repository_resource)
    |> assoc_constraint(:ci_resource)
  end

  def execution_changeset(work_item, attrs) do
    work_item
    |> cast(attrs, [
      :orchestration_task_id,
      :status,
      :assignee_type,
      :assignee_name,
      :agent_profile,
      :workspace
    ])
    |> validate_required([
      :orchestration_task_id,
      :status,
      :assignee_type,
      :assignee_name,
      :agent_profile,
      :workspace
    ])
    |> validate_inclusion(:status, @statuses)
    |> validate_inclusion(:assignee_type, @assignee_types)
    |> assoc_constraint(:orchestration_task)
    |> optimistic_lock(:lock_version)
  end

  def provider_changeset(work_item, attrs) do
    work_item
    |> cast(attrs, [
      :project_id,
      :repository_resource_id,
      :external_work_item_resource_id,
      :external_provider,
      :external_id,
      :external_url,
      :external_state,
      :external_updated_at,
      :external_available,
      :external_assignee_name,
      :labels,
      :external_data,
      :title,
      :description,
      :status,
      :priority,
      :position,
      :assignee_type,
      :blocked
    ])
    |> validate_work_item(255, 1_048_576)
    |> validate_external_work_item()
    |> assoc_constraint(:project)
    |> assoc_constraint(:external_work_item_resource)
    |> unique_constraint([:external_work_item_resource_id, :external_id],
      name: :work_items_external_identity
    )
  end

  def provider_update_changeset(work_item, attrs) do
    work_item
    |> cast(attrs, [
      :repository_resource_id,
      :external_provider,
      :external_id,
      :external_url,
      :external_state,
      :external_updated_at,
      :external_available,
      :external_assignee_name,
      :labels,
      :external_data,
      :title,
      :description,
      :priority
    ])
    |> validate_external_work_item()
    |> validate_required([:title, :status, :priority])
    |> validate_length(:title, min: 1, max: 255)
    |> validate_length(:description, max: 1_048_576)
    |> validate_inclusion(:priority, @priorities)
    |> assoc_constraint(:repository_resource)
    |> unique_constraint([:external_work_item_resource_id, :external_id],
      name: :work_items_external_identity
    )
    |> optimistic_lock(:lock_version)
  end

  def delivery_changeset(work_item, attrs) do
    work_item
    |> cast(attrs, [
      :external_pull_request_url,
      :external_pull_request_state,
      :external_ci_status,
      :external_review_status,
      :external_change_updated_at,
      :external_ci_updated_at,
      :external_change_data,
      :external_ci_data
    ])
    |> ChangesetValidators.validate_http_url(:external_pull_request_url)
    |> validate_inclusion(:external_pull_request_state, @pull_request_states)
    |> validate_inclusion(:external_ci_status, @ci_statuses)
    |> validate_inclusion(:external_review_status, @review_statuses)
    |> check_constraint(:external_ci_status, name: :work_items_external_ci_status_check)
    |> check_constraint(:external_review_status, name: :work_items_external_review_status_check)
    |> check_constraint(:external_pull_request_state,
      name: :work_items_external_pull_request_state_check
    )
    |> optimistic_lock(:lock_version)
  end

  defp validate_external_work_item(changeset) do
    changeset
    |> validate_required([
      :external_work_item_resource_id,
      :external_provider,
      :external_id,
      :external_url,
      :external_state,
      :external_updated_at
    ])
    |> validate_inclusion(:external_provider, ["github", "azure_devops"])
    |> validate_length(:external_id, min: 1, max: 240)
    |> validate_length(:external_state, min: 1, max: 240)
    |> validate_length(:external_assignee_name, max: 240)
    |> ChangesetValidators.validate_http_url(:external_url)
    |> check_constraint(:external_provider, name: :work_items_external_provider_check)
  end

  defp clear_changed_delivery_bindings(changeset) do
    cond do
      Enum.any?(
        [:repository_resource_id, :pull_request_url],
        &Map.has_key?(changeset.changes, &1)
      ) ->
        changeset
        |> put_change(:external_pull_request_url, nil)
        |> put_change(:external_pull_request_state, nil)
        |> put_change(:external_review_status, nil)
        |> put_change(:external_ci_status, nil)
        |> put_change(:external_change_updated_at, nil)
        |> put_change(:external_ci_updated_at, nil)
        |> put_change(:external_change_data, %{})
        |> put_change(:external_ci_data, %{})

      Enum.any?([:ci_resource_id, :branch], &Map.has_key?(changeset.changes, &1)) ->
        changeset
        |> put_change(:external_ci_status, nil)
        |> put_change(:external_ci_updated_at, nil)
        |> put_change(:external_ci_data, %{})

      true ->
        changeset
    end
  end

  defp validate_blocker(changeset) do
    if get_field(changeset, :blocked) and blank?(get_field(changeset, :blocker)) do
      add_error(changeset, :blocker, "must be present when blocked")
    else
      changeset
    end
  end

  defp validate_assignee(changeset) do
    case get_field(changeset, :assignee_type) do
      "human" ->
        if blank?(get_field(changeset, :assignee_name)),
          do: add_error(changeset, :assignee_name, "must be present for a human assignee"),
          else: changeset

      "agent" ->
        if blank?(get_field(changeset, :agent_profile)),
          do: add_error(changeset, :agent_profile, "must be present for an agent assignee"),
          else: changeset

      _ ->
        changeset
    end
  end

  defp reject_move_fields(changeset, attrs) do
    if Map.has_key?(attrs, :status) or Map.has_key?(attrs, "status") or
         Map.has_key?(attrs, :position) or Map.has_key?(attrs, "position"),
       do: add_error(changeset, :status, "must be changed through a Kanban move"),
       else: changeset
  end

  defp blank?(value), do: is_nil(value) or (is_binary(value) and String.trim(value) == "")

  defp trim(value) when is_binary(value), do: String.trim(value)
  defp trim(value), do: value
end
