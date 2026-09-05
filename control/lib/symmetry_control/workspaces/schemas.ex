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

  alias SymmetryControl.Workspaces.Project
  alias SymmetryControl.Workspaces.ChangesetValidators

  @kinds ["repository", "work_tracking", "ci", "agent", "runtime", "connection"]
  @statuses ["healthy", "degraded", "offline", "unknown"]
  @sync_statuses ["unknown", "syncing", "synced", "stale", "failed"]

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "project_resources" do
    belongs_to :project, Project
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
    timestamps(type: :utc_datetime_usec)
  end

  def changeset(resource, attrs) do
    resource
    |> cast(attrs, [
      :project_id,
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
    |> unique_constraint([:project_id, :kind, :name])
  end

  def update_changeset(resource, attrs) do
    resource
    |> cast(attrs, [
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
    |> unique_constraint([:project_id, :kind, :name])
    |> optimistic_lock(:lock_version)
  end

  def delete_changeset(resource) do
    resource
    |> change()
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

  @primary_key {:id, :binary_id, autogenerate: true}
  @foreign_key_type :binary_id
  schema "work_items" do
    field :number, :integer, read_after_writes: true
    belongs_to :project, Project
    belongs_to :orchestration_task, Task
    belongs_to :repository_resource, ProjectResource
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
    field :lock_version, :integer, default: 1
    timestamps(type: :utc_datetime_usec)
  end

  def changeset(work_item, attrs) do
    work_item
    |> cast(attrs, [
      :project_id,
      :repository_resource_id,
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

  defp validate_work_item(changeset) do
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
    |> validate_length(:title, min: 1, max: 240)
    |> validate_length(:description, max: 20_000)
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
