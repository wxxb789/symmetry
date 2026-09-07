defmodule SymmetryControl.Workspaces do
  @moduledoc """
  Project, engineering-resource, and work-item boundaries for the web portal.

  Execution remains owned by `SymmetryControl.Orchestration`; a work item links
  to one durable orchestration task whose generations retain the run history.
  """

  import Ecto.Query

  alias SymmetryControl.Integrations.{ChangeAction, Connection}
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Notifier, Runtime, Scheduler}
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces.{Project, ProjectResource, WorkItem}

  @active_execution_states [
    "queued",
    "assigned",
    "claimed",
    "running",
    "paused",
    "waiting_for_input",
    "cancelling"
  ]
  @execution_defining_fields [
    :title,
    :description,
    :assignee_type,
    :assignee_name,
    :agent_profile,
    :workspace,
    :repository,
    :repository_resource_id,
    :ci_resource_id,
    :branch,
    :pull_request_url
  ]

  @spec create_project(map()) :: {:ok, Project.t()} | {:error, Ecto.Changeset.t()}
  def create_project(attrs) when is_map(attrs) do
    %Project{}
    |> Project.changeset(attrs)
    |> Repo.insert()
  end

  @spec update_project(Ecto.UUID.t(), map()) ::
          {:ok, Project.t()}
          | {:error, :not_found | :invalid_request | :stale | Ecto.Changeset.t()}
  def update_project(project_id, attrs) when is_map(attrs) do
    if valid_uuid?(project_id) do
      Repo.transaction(fn ->
        project = lock_project(project_id)

        with {:ok, attrs} <- expect_version(project, attrs),
             :ok <- project_mutation_allowed(project, attrs),
             changeset <- Project.update_changeset(project, attrs),
             :ok <- project_archive_allowed(project, changeset) do
          changeset |> update_with_stale_error() |> unwrap_or_rollback()
        else
          {:error, reason} -> Repo.rollback(reason)
        end
      end)
      |> unwrap_transaction()
    else
      {:error, :not_found}
    end
  end

  @spec create_resource(Ecto.UUID.t(), map()) ::
          {:ok, ProjectResource.t()} | {:error, :not_found | Ecto.Changeset.t()}
  def create_resource(project_id, attrs) when is_map(attrs) do
    if valid_uuid?(project_id) do
      Repo.transaction(fn ->
        project = lock_project(project_id)
        ensure_active_project(project) |> require_ok!()
        attrs = prepare_resource_attrs(attrs) |> unwrap_or_rollback()

        project
        |> Ecto.build_assoc(:resources)
        |> ProjectResource.changeset(attrs)
        |> validate_registered_reference()
        |> Repo.insert()
        |> unwrap_or_rollback()
      end)
      |> unwrap_transaction()
    else
      {:error, :not_found}
    end
  end

  @spec update_resource(Ecto.UUID.t(), map()) ::
          {:ok, ProjectResource.t()} | {:error, :not_found | Ecto.Changeset.t()}
  def update_resource(resource_id, attrs) when is_map(attrs) do
    if valid_uuid?(resource_id) do
      Repo.transaction(fn ->
        resource = lock_resource_with_project(resource_id)
        ensure_active_project(resource.project) |> require_ok!()

        with {:ok, attrs} <- expect_version(resource, attrs),
             {:ok, attrs} <- prepare_resource_attrs(attrs, resource),
             changeset <-
               ProjectResource.update_changeset(
                 resource,
                 Map.drop(attrs, [:project_id, "project_id"])
               )
               |> validate_registered_reference(),
             :ok <- resource_identity_change_allowed(resource, changeset) do
          changeset |> update_with_stale_error() |> unwrap_or_rollback()
        else
          {:error, reason} -> Repo.rollback(reason)
        end
      end)
      |> unwrap_transaction()
    else
      {:error, :not_found}
    end
  end

  @spec delete_resource(Ecto.UUID.t(), pos_integer()) ::
          {:ok, ProjectResource.t()} | {:error, :not_found | :invalid_request | :stale}
  def delete_resource(resource_id, expected_version) do
    if valid_uuid?(resource_id) do
      Repo.transaction(fn ->
        resource = lock_resource_with_project(resource_id)

        with :ok <- ensure_active_project(resource.project),
             :ok <- ensure_resource_unreferenced(resource),
             :ok <- matches_version(resource, expected_version) do
          resource
          |> ProjectResource.delete_changeset()
          |> delete_with_stale_error()
          |> unwrap_or_rollback()
        else
          {:error, reason} -> Repo.rollback(reason)
        end
      end)
      |> unwrap_transaction()
    else
      {:error, :not_found}
    end
  end

  @spec create_work_item(Ecto.UUID.t(), map()) ::
          {:ok, WorkItem.t()} | {:error, :not_found | Ecto.Changeset.t()}
  def create_work_item(project_id, attrs) when is_map(attrs) do
    if valid_uuid?(project_id) do
      Repo.transaction(fn ->
        project = lock_project(project_id)
        ensure_active_project(project) |> require_ok!()

        attrs =
          attrs
          |> normalize_work_item_attrs(project)
          |> put_attr(:position, next_position(project.id, work_item_status(attrs)))

        changeset =
          project
          |> Ecto.build_assoc(:work_items)
          |> WorkItem.changeset(attrs)
          |> validate_work_item_resources(project.id)

        changeset |> Repo.insert() |> unwrap_or_rollback()
      end)
      |> unwrap_transaction()
    else
      {:error, :not_found}
    end
  end

  @spec update_work_item(Ecto.UUID.t(), map()) ::
          {:ok, WorkItem.t()} | {:error, :not_found | Ecto.Changeset.t()}
  def update_work_item(work_item_id, attrs) when is_map(attrs) do
    if valid_uuid?(work_item_id) do
      Repo.transaction(fn ->
        work_item = lock_work_item_with_project(work_item_id)
        ensure_active_project(work_item.project) |> require_ok!()

        with {:ok, attrs} <- expect_version(work_item, attrs),
             changeset <-
               WorkItem.update_changeset(
                 work_item,
                 attrs
                 |> Map.drop([:project_id, "project_id"])
                 |> normalize_work_item_attrs(work_item.project, work_item)
               ),
             :ok <- provider_owned_fields_editable(work_item, changeset),
             :ok <- execution_fields_editable(work_item, changeset) do
          changeset
          |> validate_work_item_resources(work_item.project_id)
          |> update_with_stale_error()
          |> unwrap_or_rollback()
        else
          {:error, reason} -> Repo.rollback(reason)
        end
      end)
      |> unwrap_transaction()
    else
      {:error, :not_found}
    end
  end

  @spec move_work_item(Ecto.UUID.t(), map()) ::
          {:ok, WorkItem.t()}
          | {:error, :not_found | :invalid_request | :stale | Ecto.Changeset.t()}
  def move_work_item(work_item_id, attrs) when is_map(attrs) do
    with {:ok, expected_version} <- expected_version(attrs),
         {:ok, status} <- work_item_move_status(attrs),
         {:ok, before_id} <-
           optional_uuid(attr_value(attrs, :before_id, nil)),
         true <- valid_uuid?(work_item_id) do
      Repo.transaction(fn ->
        project_id = work_item_project_id(work_item_id)
        project = lock_project(project_id)
        ensure_active_project(project) |> require_ok!()

        locked_items =
          Repo.all(
            from item in WorkItem,
              where: item.project_id == ^project_id,
              order_by: [asc: item.id],
              lock: "FOR UPDATE"
          )

        work_item = Enum.find(locked_items, &(&1.id == work_item_id)) || Repo.rollback(:not_found)
        if work_item.lock_version != expected_version, do: Repo.rollback(:stale)

        items = Enum.sort_by(locked_items, &{&1.position, &1.number, &1.id})

        target_items =
          items
          |> Enum.filter(&(&1.status == status and &1.id != work_item.id))
          |> insert_before(work_item, before_id)

        source_items =
          if work_item.status == status,
            do: [],
            else: Enum.filter(items, &(&1.status == work_item.status and &1.id != work_item.id))

        persist_positions(source_items, work_item.status)
        persist_positions(target_items, status)

        Repo.get!(WorkItem, work_item.id)
      end)
      |> unwrap_transaction()
    else
      false -> {:error, :not_found}
      {:error, reason} -> {:error, reason}
    end
  end

  @spec workspace_snapshot(Ecto.UUID.t() | nil) :: {:ok, map()} | {:error, :not_found}
  def workspace_snapshot(selected_project_id \\ nil) do
    projects =
      Project
      |> order_by([project], asc: project.status, asc: project.name, asc: project.id)
      |> Repo.all()

    selected_project = select_project(projects, selected_project_id)

    case {projects, selected_project} do
      {[], nil} ->
        {:ok, %{projects: [], selected_project: nil, resources: [], work_items: []}}

      {_, nil} ->
        {:error, :not_found}

      {_, project} ->
        project = hydrate_project(project)
        projects = Enum.map(projects, &if(&1.id == project.id, do: project, else: &1))

        {:ok,
         %{
           projects: projects,
           selected_project: project,
           resources: project.resources,
           work_items: project.work_items
         }}
    end
  end

  @spec launch_work_item(Ecto.UUID.t()) ::
          {:ok, map(), :created | :replayed} | {:error, atom() | Ecto.Changeset.t()}
  def launch_work_item(work_item_id), do: launch_work_item(work_item_id, "server")

  @spec launch_work_item(Ecto.UUID.t(), String.t()) ::
          {:ok, map(), :created | :replayed} | {:error, atom() | Ecto.Changeset.t()}
  def launch_work_item(work_item_id, action_id), do: launch_work_item(work_item_id, action_id, [])

  @spec launch_work_item(Ecto.UUID.t(), String.t(), keyword()) ::
          {:ok, map(), :created | :replayed} | {:error, atom() | Ecto.Changeset.t()}
  def launch_work_item(work_item_id, action_id, opts)
      when is_binary(action_id) and is_list(opts) do
    with :ok <- validate_action_id(action_id),
         true <- valid_uuid?(work_item_id) do
      result =
        Repo.transaction(fn ->
          work_item = lock_work_item_with_project(work_item_id)
          ensure_active_project(work_item.project) |> require_ok!()

          launch_locked_work_item(work_item, opts)
        end)

      case result do
        {:ok, {snapshot, disposition, should_wake?}} ->
          if should_wake? and Keyword.get(opts, :notify, true), do: Scheduler.wake()
          {:ok, snapshot, disposition}

        {:error, reason} ->
          {:error, reason}
      end
    else
      false -> {:error, :not_found}
      {:error, reason} -> {:error, reason}
    end
  end

  def launch_work_item(_, _, _), do: {:error, :invalid_request}

  @spec cancel_work_item(Ecto.UUID.t(), non_neg_integer(), String.t()) ::
          {:ok, map(), :created | :replayed} | {:error, atom()}
  def cancel_work_item(work_item_id, generation, action_id)
      when is_integer(generation) and generation >= 0 and is_binary(action_id) do
    with :ok <- validate_action_id(action_id),
         {:ok, work_item} <- fetch(WorkItem, work_item_id),
         task_id when is_binary(task_id) <- work_item.orchestration_task_id,
         {:ok, command, disposition} <-
           Orchestration.create_command(
             task_id,
             "cancel",
             %{},
             control_idempotency_key("cancel", work_item.id, generation, nil, action_id),
             expected_generation: generation
           ),
         {:ok, task} <- Orchestration.task_snapshot(task_id) do
      notify_command(command, disposition)

      {:ok, %{work_item: work_item, task: task, command: command}, disposition}
    else
      nil -> {:error, :state_conflict}
      {:error, reason} -> {:error, reason}
    end
  end

  def cancel_work_item(_, _, _), do: {:error, :invalid_request}

  @spec provide_work_item_input(Ecto.UUID.t(), map(), Ecto.UUID.t(), String.t()) ::
          {:ok, map(), :created | :replayed} | {:error, atom()}
  def provide_work_item_input(work_item_id, input, waiting_transition_id, action_id)
      when is_map(input) and is_binary(waiting_transition_id) and is_binary(action_id) do
    with :ok <- validate_action_id(action_id),
         {:ok, work_item} <- fetch(WorkItem, work_item_id),
         task_id when is_binary(task_id) <- work_item.orchestration_task_id,
         {:ok, task_before} <- Orchestration.task_snapshot(task_id),
         {:ok, command, disposition} <-
           Orchestration.provide_input(
             task_id,
             input,
             control_idempotency_key(
               "input",
               work_item.id,
               task_before.task.current_generation,
               waiting_transition_id,
               action_id
             ),
             expected_waiting_transition_id: waiting_transition_id
           ),
         {:ok, task} <- Orchestration.task_snapshot(task_id) do
      notify_command(command, disposition)

      {:ok, %{work_item: work_item, task: task, command: command}, disposition}
    else
      nil -> {:error, :state_conflict}
      {:error, reason} -> {:error, reason}
    end
  end

  def provide_work_item_input(_, _, _, _), do: {:error, :invalid_request}

  @spec retry_work_item(Ecto.UUID.t(), non_neg_integer(), String.t()) ::
          {:ok, map(), :created | :replayed} | {:error, atom() | Ecto.Changeset.t()}
  def retry_work_item(work_item_id, generation, action_id)
      when is_integer(generation) and generation >= 0 and is_binary(action_id) do
    with :ok <- validate_action_id(action_id),
         true <- valid_uuid?(work_item_id) do
      result =
        Repo.transaction(fn ->
          work_item = lock_work_item_with_project(work_item_id)
          ensure_active_project(work_item.project) |> require_ok!()
          ensure_external_work_available(work_item) |> require_ok!()
          if work_item.assignee_type != "agent", do: Repo.rollback(:state_conflict)

          task_id = work_item.orchestration_task_id || Repo.rollback(:state_conflict)

          task_attrs = execution_attrs(work_item)

          case Orchestration.retry_task(
                 task_id,
                 task_attrs,
                 control_idempotency_key(
                   "retry",
                   work_item.id,
                   generation,
                   nil,
                   action_id
                 ),
                 expected_generation: generation
               ) do
            {:ok, _task, command, :replayed} ->
              {:ok, task_snapshot} = Orchestration.task_snapshot(task_id)
              {%{work_item: work_item, task: task_snapshot, command: command}, :replayed, false}

            {:ok, _task, command, :created} ->
              work_item =
                work_item
                |> WorkItem.execution_changeset(%{
                  orchestration_task_id: task_id,
                  status: "in_progress"
                })
                |> Repo.update!()

              {:ok, task_snapshot} = Orchestration.task_snapshot(task_id)
              {%{work_item: work_item, task: task_snapshot, command: command}, :created, true}

            {:error, reason} ->
              Repo.rollback(reason)
          end
        end)

      case result do
        {:ok, {snapshot, disposition, should_wake?}} ->
          if should_wake?, do: Scheduler.wake()
          {:ok, snapshot, disposition}

        {:error, reason} ->
          {:error, reason}
      end
    else
      false -> {:error, :not_found}
      {:error, reason} -> {:error, reason}
    end
  end

  def retry_work_item(_, _, _), do: {:error, :invalid_request}

  @spec work_item_snapshot(Ecto.UUID.t()) :: {:ok, map()} | {:error, atom()}
  def work_item_snapshot(work_item_id) do
    with {:ok, work_item} <- fetch(WorkItem, work_item_id) do
      work_item = Repo.preload(work_item, [:project, :repository_resource, :ci_resource])

      case work_item.orchestration_task_id do
        nil ->
          {:ok, %{work_item: work_item, task: nil, timeline: [], next_before: nil}}

        task_id ->
          with {:ok, task} <- Orchestration.task_snapshot(task_id),
               {:ok, page} <- Orchestration.task_timeline(task_id, limit: 100) do
            {:ok,
             %{
               work_item: work_item,
               task: task,
               timeline: page.entries,
               next_before: page.next_before
             }}
          end
      end
    end
  end

  @spec fetch_work_item(Ecto.UUID.t()) :: {:ok, WorkItem.t()} | {:error, :not_found}
  def fetch_work_item(work_item_id), do: fetch(WorkItem, work_item_id)

  defp launch_locked_work_item(%WorkItem{orchestration_task_id: task_id} = work_item, opts)
       when is_binary(task_id) do
    case Orchestration.task_snapshot(task_id) do
      {:ok, task} ->
        required = Keyword.get(opts, :required_capabilities, %{})

        unless is_map(required) and
                 Enum.all?(required, fn {key, value} ->
                   Map.get(task.task.required_capabilities, to_string(key)) == value
                 end),
               do: Repo.rollback(:idempotency_conflict)

        {%{work_item: work_item, task: task}, :replayed, false}

      {:error, reason} ->
        Repo.rollback(reason)
    end
  end

  defp launch_locked_work_item(%WorkItem{project: project} = work_item, opts) do
    if project.status != "active" or work_item.assignee_type != "agent" or
         work_item.status not in ["backlog", "ready"] or
         not WorkItem.external_work_available?(work_item) do
      Repo.rollback(:state_conflict)
    end

    agent_profile = present(work_item.agent_profile) || project.default_agent_profile
    workspace = present(work_item.workspace) || project.default_workspace

    attrs = execution_attrs(work_item, agent_profile, workspace)
    required = Keyword.get(opts, :required_capabilities, %{})
    unless is_map(required), do: Repo.rollback(:invalid_request)
    attrs = Map.update!(attrs, :required_capabilities, &Map.merge(required, &1))

    case Orchestration.submit_task(attrs, "portal-work-item-#{work_item.id}") do
      {:ok, task, disposition} ->
        changeset =
          WorkItem.execution_changeset(work_item, %{
            orchestration_task_id: task.id,
            status: "in_progress",
            assignee_type: "agent",
            assignee_name: agent_profile,
            agent_profile: agent_profile,
            workspace: workspace
          })

        with {:ok, work_item} <- Repo.update(changeset),
             {:ok, task_snapshot} <- Orchestration.task_snapshot(task.id) do
          {%{work_item: work_item, task: task_snapshot}, disposition, disposition == :created}
        else
          {:error, reason} -> Repo.rollback(reason)
        end

      {:error, reason} ->
        Repo.rollback(reason)
    end
  end

  defp execution_attrs(work_item) do
    project = work_item.project
    agent_profile = present(work_item.agent_profile) || project.default_agent_profile
    workspace = present(work_item.workspace) || project.default_workspace
    execution_attrs(work_item, agent_profile, workspace)
  end

  defp execution_attrs(work_item, agent_profile, workspace) do
    project = work_item.project
    provider_resource_ids = provider_resource_ids(work_item)

    repository =
      case work_item.repository_resource do
        %ProjectResource{} = resource -> present(resource.external_ref) || resource.name
        _not_loaded_or_missing -> work_item.repository
      end

    input =
      %{
        "project_id" => project.id,
        "project_key" => project.key,
        "provider_resource_ids" => provider_resource_ids,
        "repository" => repository,
        "repository_resource_id" => work_item.repository_resource_id,
        "work_item_id" => work_item.id,
        "work_item_key" => work_item_key(project, work_item)
      }
      |> maybe_put_ci_resource(work_item)
      |> maybe_put_external_work_item(work_item)
      |> maybe_put_change_scope(work_item)
      |> unwrap_or_rollback()

    %{
      goal: execution_goal(work_item),
      agent_profile: agent_profile,
      workspace: workspace,
      input: input,
      required_capabilities: required_capabilities(provider_resource_ids)
    }
  end

  defp required_capabilities([]), do: %{}
  defp required_capabilities(_provider_resource_ids), do: %{"provider_access" => true}

  defp provider_resource_ids(work_item) do
    [
      work_item.external_work_item_resource,
      work_item.repository_resource,
      work_item.ci_resource
    ]
    |> Enum.flat_map(fn
      %ProjectResource{id: id, connection_id: connection_id} when is_binary(connection_id) ->
        [id]

      _manual_or_missing ->
        []
    end)
    |> Enum.uniq()
  end

  defp maybe_put_change_scope(
         input,
         %WorkItem{
           repository_resource: %ProjectResource{
             connection_id: connection_id,
             connection: %Connection{capabilities: capabilities}
           }
         } = work_item
       )
       when is_binary(connection_id) do
    if "changes" in capabilities do
      with {:ok, source_branch} <- ChangeAction.normalize_branch(work_item.branch),
           {:ok, target_branch} <-
             ChangeAction.normalize_branch(
               work_item.repository_resource.metadata["default_branch"]
             ) do
        input =
          input
          |> Map.put("source_branch", source_branch)
          |> Map.put("target_branch", target_branch)
          |> maybe_put_linked_pull_request(ChangeAction.linked_pull_request_url(work_item))

        {:ok, input}
      else
        {:error, :invalid_request} -> {:error, :state_conflict}
      end
    else
      {:ok, input}
    end
  end

  defp maybe_put_change_scope(input, _work_item), do: {:ok, input}

  defp maybe_put_linked_pull_request(input, pull_request_url) do
    case present(pull_request_url) do
      nil -> input
      url -> Map.put(input, "pull_request_url", url)
    end
  end

  defp maybe_put_external_work_item(input, %WorkItem{external_work_item_resource_id: nil}),
    do: input

  defp maybe_put_external_work_item(input, work_item) do
    Map.put(input, "external_work_item", %{
      "provider" => work_item.external_provider,
      "id" => work_item.external_id,
      "url" => work_item.external_url,
      "state" => work_item.external_state,
      "resource_id" => work_item.external_work_item_resource_id
    })
  end

  defp maybe_put_ci_resource(input, %WorkItem{ci_resource_id: nil}), do: input

  defp maybe_put_ci_resource(input, work_item),
    do: Map.put(input, "ci_resource_id", work_item.ci_resource_id)

  defp execution_goal(%WorkItem{description: description, title: title}) do
    case present(description) do
      nil -> title
      detail -> title <> "\n\n" <> detail
    end
  end

  defp work_item_key(project, work_item), do: "#{project.key}-#{work_item.number}"

  defp hydrate_project(project) do
    resources =
      from resource in ProjectResource,
        order_by: [asc: resource.inserted_at, asc: resource.id]

    work_items =
      from work_item in WorkItem,
        order_by: [asc: work_item.position, desc: work_item.updated_at, asc: work_item.number],
        preload: [:project, :repository_resource, :ci_resource, :external_work_item_resource]

    Repo.preload(project, resources: resources, work_items: work_items)
  end

  defp select_project([], _selected_project_id), do: nil

  defp select_project(projects, nil),
    do: Enum.find(projects, &(&1.status == "active")) || List.first(projects)

  defp select_project(projects, selected_project_id),
    do: Enum.find(projects, &(&1.id == selected_project_id))

  defp fetch(schema, id) when is_binary(id) do
    with {:ok, id} <- Ecto.UUID.cast(id),
         value when not is_nil(value) <- Repo.get(schema, id) do
      {:ok, value}
    else
      _ -> {:error, :not_found}
    end
  end

  defp fetch(_schema, _id), do: {:error, :not_found}

  defp lock_project(project_id) do
    Repo.one(from project in Project, where: project.id == ^project_id, lock: "FOR UPDATE") ||
      Repo.rollback(:not_found)
  end

  defp work_item_project_id(work_item_id) do
    Repo.one(from item in WorkItem, where: item.id == ^work_item_id, select: item.project_id) ||
      Repo.rollback(:not_found)
  end

  defp lock_work_item_with_project(work_item_id) do
    project = work_item_id |> work_item_project_id() |> lock_project()

    work_item =
      Repo.one(from item in WorkItem, where: item.id == ^work_item_id, lock: "FOR UPDATE") ||
        Repo.rollback(:not_found)

    work_item
    |> Map.put(:project, project)
    |> Repo.preload([
      :ci_resource,
      :external_work_item_resource,
      repository_resource: :connection
    ])
  end

  defp lock_resource_with_project(resource_id) do
    project_id =
      Repo.one(
        from resource in ProjectResource,
          where: resource.id == ^resource_id,
          select: resource.project_id
      ) || Repo.rollback(:not_found)

    project = lock_project(project_id)

    resource =
      Repo.one(
        from resource in ProjectResource,
          where: resource.id == ^resource_id,
          lock: "FOR UPDATE"
      ) || Repo.rollback(:not_found)

    Map.put(resource, :project, project)
  end

  defp require_ok!(:ok), do: :ok
  defp require_ok!({:error, reason}), do: Repo.rollback(reason)

  defp ensure_active_project(%Project{status: "active"}), do: :ok
  defp ensure_active_project(%Project{}), do: {:error, :state_conflict}

  defp ensure_external_work_available(work_item) do
    if WorkItem.external_work_available?(work_item),
      do: :ok,
      else: {:error, :state_conflict}
  end

  defp project_mutation_allowed(%Project{status: "active"}, _attrs), do: :ok

  defp project_mutation_allowed(%Project{status: "archived"}, attrs) do
    keys = attrs |> Map.keys() |> Enum.map(&to_string/1) |> MapSet.new()
    requested_status = Map.get(attrs, :status) || Map.get(attrs, "status")

    if requested_status == "active" and MapSet.subset?(keys, MapSet.new(["status"])),
      do: :ok,
      else: {:error, :state_conflict}
  end

  defp project_archive_allowed(project, changeset) do
    if Ecto.Changeset.get_change(changeset, :status) == "archived" and
         project_has_active_execution?(project.id),
       do: {:error, :state_conflict},
       else: :ok
  end

  defp project_has_active_execution?(project_id) do
    Repo.exists?(
      from item in WorkItem,
        join: task in SymmetryControl.Orchestration.Task,
        on: task.id == item.orchestration_task_id,
        where: item.project_id == ^project_id and task.state in ^@active_execution_states
    )
  end

  defp ensure_resource_unreferenced(resource) do
    if Repo.exists?(
         from item in WorkItem,
           where:
             item.repository_resource_id == ^resource.id or
               item.ci_resource_id == ^resource.id or
               item.external_work_item_resource_id == ^resource.id
       ),
       do: {:error, :state_conflict},
       else: :ok
  end

  defp resource_identity_change_allowed(resource, changeset) do
    if Enum.any?([:kind, :connection_id, :external_ref], &Map.has_key?(changeset.changes, &1)),
      do: ensure_resource_unreferenced(resource),
      else: :ok
  end

  defp validate_work_item_resources(changeset, project_id) do
    resources =
      [:repository_resource_id, :ci_resource_id]
      |> Enum.map(&Ecto.Changeset.get_field(changeset, &1))
      |> Enum.reject(&is_nil/1)
      |> Enum.uniq()
      |> case do
        [] ->
          %{}

        ids ->
          from(resource in ProjectResource, where: resource.id in ^ids)
          |> Repo.all()
          |> Map.new(&{&1.id, &1})
      end

    changeset
    |> validate_project_resource(
      :repository_resource_id,
      project_id,
      "repository",
      "must belong to the work item's project",
      resources
    )
    |> validate_project_resource(
      :ci_resource_id,
      project_id,
      "ci",
      "must be a CI resource in the work item's project",
      resources
    )
  end

  defp validate_project_resource(
         changeset,
         field,
         project_id,
         kind,
         error_message,
         resources
       ) do
    case Ecto.Changeset.get_field(changeset, field) do
      nil ->
        changeset

      resource_id ->
        case Map.get(resources, resource_id) do
          %ProjectResource{project_id: ^project_id, kind: ^kind} ->
            changeset

          _resource ->
            Ecto.Changeset.add_error(changeset, field, error_message)
        end
    end
  end

  defp provider_owned_fields_editable(
         %WorkItem{external_work_item_resource_id: resource_id},
         changeset
       )
       when is_binary(resource_id) do
    if Enum.any?(
         Map.keys(changeset.changes),
         &WorkItem.provider_owned_field?/1
       ),
       do: {:error, :provider_owned},
       else: :ok
  end

  defp provider_owned_fields_editable(_work_item, _changeset), do: :ok

  defp ensure_connection_capability(connection, kind) do
    capability =
      case kind do
        "repository" -> "repositories"
        "work_tracking" -> "work_items"
        "ci" -> "ci"
        _ -> nil
      end

    cond do
      is_nil(capability) -> {:error, :invalid_request}
      capability in connection.capabilities -> :ok
      true -> {:error, :forbidden}
    end
  end

  defp prepare_resource_attrs(attrs, resource \\ nil) do
    connection_id =
      if attr_present?(attrs, :connection_id),
        do: present(attr_value(attrs, :connection_id, nil)),
        else: resource && resource.connection_id

    case connection_id do
      nil ->
        attrs =
          if resource && resource.connection_id do
            attrs
            |> put_attr(:connection_id, nil)
            |> put_attr(:provider, nil)
            |> put_attr(:status, "unknown")
            |> put_attr(:sync_status, "unknown")
            |> put_attr(:status_message, nil)
            |> put_attr(:last_checked_at, nil)
            |> put_attr(:last_synced_at, nil)
          else
            attrs
          end

        {:ok, attrs}

      id ->
        with {:ok, connection_id} <- Ecto.UUID.cast(id),
             %Connection{} = connection <-
               Repo.one(
                 from connection in Connection,
                   where: connection.id == ^connection_id,
                   lock: "FOR SHARE"
               ) do
          kind = attr_value(attrs, :kind, resource && resource.kind)
          ensure_connection_capability(connection, kind) |> require_ok!()

          SymmetryControl.Integrations.validate_resource_reference(
            connection,
            kind,
            attr_value(attrs, :external_ref, resource && resource.external_ref)
          )
          |> require_ok!()

          reset? =
            is_nil(resource) or resource.connection_id != connection.id or
              kind != resource.kind or
              (attr_present?(attrs, :external_ref) and
                 present(attr_value(attrs, :external_ref, nil)) != resource.external_ref)

          attrs =
            attrs
            |> drop_attrs([
              :provider,
              :url,
              :status,
              :sync_status,
              :status_message,
              :metadata,
              :last_checked_at,
              :last_synced_at
            ])
            |> put_attr(:connection_id, connection.id)
            |> put_attr(:provider, connection.provider)

          attrs =
            if reset? do
              attrs
              |> put_attr(:url, nil)
              |> put_attr(:status, "unknown")
              |> put_attr(:sync_status, "unknown")
              |> put_attr(:status_message, nil)
              |> put_attr(:metadata, %{})
              |> put_attr(:last_checked_at, nil)
              |> put_attr(:last_synced_at, nil)
            else
              attrs
            end

          {:ok, attrs}
        else
          _ -> {:error, :not_found}
        end
    end
  end

  defp execution_fields_editable(%WorkItem{orchestration_task_id: nil}, _changeset), do: :ok

  defp execution_fields_editable(work_item, changeset) do
    execution_change? =
      changeset.changes
      |> Map.keys()
      |> Enum.any?(&(&1 in @execution_defining_fields))

    if execution_change? do
      case Orchestration.fetch_task(work_item.orchestration_task_id) do
        {:ok, %{state: state}} when state in ["failed", "cancelled"] -> :ok
        {:ok, _task} -> {:error, :state_conflict}
        {:error, reason} -> {:error, reason}
      end
    else
      :ok
    end
  end

  defp valid_uuid?(id), do: match?({:ok, _}, Ecto.UUID.cast(id))

  defp expected_version(attrs) do
    case Map.get(attrs, :version) || Map.get(attrs, "version") do
      version when is_integer(version) and version > 0 -> {:ok, version}
      _ -> {:error, :invalid_request}
    end
  end

  defp expect_version(record, attrs) do
    with {:ok, expected_version} <- expected_version(attrs),
         :ok <- matches_version(record, expected_version) do
      {:ok, Map.drop(attrs, [:version, "version"])}
    end
  end

  defp matches_version(%{lock_version: version}, version), do: :ok

  defp matches_version(%{lock_version: _version}, expected) when is_integer(expected),
    do: {:error, :stale}

  defp matches_version(_record, _expected), do: {:error, :invalid_request}

  defp update_with_stale_error(changeset) do
    Repo.update(changeset, stale_error_field: :lock_version, stale_error_message: "is stale")
    |> normalize_stale_result()
  end

  defp delete_with_stale_error(changeset) do
    Repo.delete(changeset, stale_error_field: :lock_version, stale_error_message: "is stale")
    |> normalize_stale_result()
  end

  defp normalize_stale_result({:error, changeset} = error) do
    cond do
      Keyword.has_key?(changeset.errors, :lock_version) -> {:error, :stale}
      Keyword.has_key?(changeset.errors, :provider_action_intents) -> {:error, :state_conflict}
      true -> error
    end
  end

  defp normalize_stale_result(result), do: result

  defp unwrap_or_rollback({:ok, value}), do: value
  defp unwrap_or_rollback({:error, reason}), do: Repo.rollback(reason)

  defp unwrap_transaction({:ok, value}), do: {:ok, value}
  defp unwrap_transaction({:error, reason}), do: {:error, reason}

  defp work_item_status(attrs), do: attr_value(attrs, :status, "backlog")

  defp next_position(project_id, status) do
    Repo.one(
      from item in WorkItem,
        where: item.project_id == ^project_id and item.status == ^status,
        select: max(item.position)
    )
    |> case do
      nil -> 0
      position -> position + 1
    end
  end

  defp normalize_work_item_attrs(attrs, project, work_item \\ nil) do
    current_type = work_item && work_item.assignee_type
    type = attr_value(attrs, :assignee_type, current_type || "unassigned")
    blocked = attr_value(attrs, :blocked, (work_item && work_item.blocked) || false)

    attrs =
      case type do
        "agent" ->
          profile =
            present(attr_value(attrs, :agent_profile, work_item && work_item.agent_profile)) ||
              project.default_agent_profile

          workspace =
            present(attr_value(attrs, :workspace, work_item && work_item.workspace)) ||
              project.default_workspace

          assignee_name =
            cond do
              attr_present?(attrs, :assignee_name) ->
                present(attr_value(attrs, :assignee_name, nil))

              work_item && work_item.assignee_type == "agent" ->
                present(work_item.assignee_name)

              true ->
                nil
            end

          attrs
          |> put_attr(:agent_profile, profile)
          |> put_attr(:workspace, workspace)
          |> put_attr(:assignee_name, assignee_name || profile)

        "human" ->
          attrs
          |> put_attr(:agent_profile, nil)
          |> put_attr(:workspace, nil)

        "unassigned" ->
          attrs
          |> put_attr(:assignee_name, nil)
          |> put_attr(:agent_profile, nil)
          |> put_attr(:workspace, nil)

        _ ->
          attrs
      end

    if blocked == false, do: put_attr(attrs, :blocker, nil), else: attrs
  end

  defp validate_registered_reference(changeset) do
    kind = Ecto.Changeset.get_field(changeset, :kind)
    external_ref = Ecto.Changeset.get_field(changeset, :external_ref)

    cond do
      not changeset.valid? or kind not in ["agent", "runtime"] ->
        changeset

      kind == "runtime" and
          (not valid_uuid?(external_ref) or
             not Repo.exists?(from runtime in Runtime, where: runtime.id == ^external_ref)) ->
        Ecto.Changeset.add_error(changeset, :external_ref, "must reference a registered runtime")

      kind == "agent" and
          not Repo.exists?(from runtime in Runtime, where: runtime.agent_profile == ^external_ref) ->
        Ecto.Changeset.add_error(
          changeset,
          :external_ref,
          "must reference a registered agent profile"
        )

      true ->
        changeset
    end
  end

  defp attr_present?(attrs, key),
    do: Map.has_key?(attrs, key) or Map.has_key?(attrs, Atom.to_string(key))

  defp drop_attrs(attrs, keys) do
    Enum.reduce(keys, attrs, fn key, acc -> Map.drop(acc, [key, Atom.to_string(key)]) end)
  end

  defp attr_value(attrs, key, default) do
    cond do
      Map.has_key?(attrs, key) -> Map.get(attrs, key)
      Map.has_key?(attrs, Atom.to_string(key)) -> Map.get(attrs, Atom.to_string(key))
      true -> default
    end
  end

  defp put_attr(attrs, key, value) do
    if Enum.any?(Map.keys(attrs), &is_binary/1),
      do: Map.put(attrs, Atom.to_string(key), value),
      else: Map.put(attrs, key, value)
  end

  defp work_item_move_status(attrs) do
    status = attr_value(attrs, :status, nil)

    if status in ["backlog", "ready", "in_progress", "review", "done"],
      do: {:ok, status},
      else: {:error, :invalid_request}
  end

  defp optional_uuid(nil), do: {:ok, nil}

  defp optional_uuid(value) when is_binary(value) do
    if valid_uuid?(value), do: {:ok, value}, else: {:error, :invalid_request}
  end

  defp optional_uuid(_value), do: {:error, :invalid_request}

  defp insert_before(items, work_item, nil), do: items ++ [work_item]

  defp insert_before(items, work_item, before_id) do
    case Enum.split_while(items, &(&1.id != before_id)) do
      {_before, []} -> Repo.rollback(:invalid_request)
      {before, after_items} -> before ++ [work_item | after_items]
    end
  end

  defp persist_positions(items, status) do
    items
    |> Enum.with_index()
    |> Enum.each(fn {item, position} -> persist_position(item, status, position) end)
  end

  defp persist_position(item, status, position) do
    if item.status != status or item.position != position do
      item
      |> WorkItem.move_changeset(%{status: status, position: position})
      |> Repo.update!()
    end
  end

  defp notify_command(command, :created) do
    Notifier.command_available(command)
    Scheduler.wake()
  end

  defp notify_command(_command, _disposition), do: :ok

  defp present(value) when is_binary(value) do
    case String.trim(value) do
      "" -> nil
      trimmed -> trimmed
    end
  end

  defp present(_value), do: nil

  defp validate_action_id(action_id) do
    if byte_size(action_id) in 1..128 and not String.contains?(action_id, <<0>>),
      do: :ok,
      else: {:error, :invalid_request}
  end

  defp control_idempotency_key(kind, work_item_id, generation, transition_id, action_id) do
    ["portal", kind, work_item_id, Integer.to_string(generation), transition_id, action_id]
    |> Enum.reject(&is_nil/1)
    |> Enum.join(":")
  end
end
