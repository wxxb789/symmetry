defmodule SymmetryControlWeb.PortalApiController do
  use SymmetryControlWeb, :controller

  alias SymmetryControl.Integrations
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.ReadModel
  alias SymmetryControlWeb.{PortalJSON, Protocol}

  def workspace(conn, params) do
    case ReadModel.workspace(Map.get(params, "project_id")) do
      {:ok, model} -> json(conn, PortalJSON.workspace(model))
      {:error, reason} -> error(conn, reason)
    end
  end

  def create_connection(conn, params) do
    case Integrations.create_connection(params) do
      {:ok, connection} ->
        conn
        |> put_status(:created)
        |> json(%{connection: PortalJSON.connection(connection)})

      {:error, reason} ->
        error(conn, reason)
    end
  end

  def update_connection(conn, params) do
    connection_id = Map.fetch!(conn.path_params, "connection_id")

    case Integrations.update_connection(connection_id, Map.delete(params, "connection_id")) do
      {:ok, connection} -> json(conn, %{connection: PortalJSON.connection(connection)})
      {:error, reason} -> error(conn, reason)
    end
  end

  def delete_connection(conn, params) do
    connection_id = Map.fetch!(conn.path_params, "connection_id")

    with {:ok, version} <- version_param(params),
         {:ok, connection} <- Integrations.delete_connection(connection_id, version) do
      json(conn, %{deleted_connection_id: connection.id})
    else
      {:error, reason} -> error(conn, reason)
    end
  end

  def check_connection(conn, _params) do
    connection_id = Map.fetch!(conn.path_params, "connection_id")

    case Integrations.check_connection(connection_id) do
      {:ok, connection} ->
        json(conn, %{connection: PortalJSON.connection(connection)})

      {:error, reason, connection} ->
        projected_error(conn, reason, :connection, PortalJSON.connection(connection))

      {:error, reason} ->
        error(conn, reason)
    end
  end

  def sync_project(conn, _params) do
    project_id = Map.fetch!(conn.path_params, "project_id")

    case Integrations.sync_project(project_id) do
      {:ok, results} ->
        json(conn, %{synced_resource_ids: Enum.map(results, &elem(&1, 0))})

      {:error, results} when is_list(results) ->
        {status, error, formatted_results} = sync_failure(results)

        conn
        |> put_status(status)
        |> json(%{error: error, results: formatted_results})

      {:error, reason} ->
        error(conn, reason)
    end
  end

  def sync_resource(conn, _params) do
    resource_id = Map.fetch!(conn.path_params, "resource_id")

    case Integrations.sync_resource(resource_id) do
      {:ok, resource} ->
        json(conn, %{resource: PortalJSON.resource(resource)})

      {:error, reason, resource} ->
        projected_error(conn, reason, :resource, PortalJSON.resource(resource))

      {:error, reason} ->
        error(conn, reason)
    end
  end

  def create_project(conn, params) do
    case Workspaces.create_project(params) do
      {:ok, project} ->
        conn |> put_status(:created) |> json(%{project: PortalJSON.project(project)})

      {:error, reason} ->
        error(conn, reason)
    end
  end

  def update_project(conn, params) do
    project_id = Map.fetch!(conn.path_params, "project_id")
    attrs = Map.delete(params, "project_id")

    case Workspaces.update_project(project_id, attrs) do
      {:ok, project} -> json(conn, %{project: PortalJSON.project(project)})
      {:error, reason} -> error(conn, reason)
    end
  end

  def create_resource(conn, params) do
    project_id = Map.fetch!(conn.path_params, "project_id")

    case Workspaces.create_resource(project_id, params) do
      {:ok, resource} ->
        conn |> put_status(:created) |> json(%{resource: PortalJSON.resource(resource)})

      {:error, reason} ->
        error(conn, reason)
    end
  end

  def update_resource(conn, params) do
    resource_id = Map.fetch!(conn.path_params, "resource_id")

    case Workspaces.update_resource(resource_id, params) do
      {:ok, resource} ->
        json(conn, %{resource: PortalJSON.resource(resource)})

      {:error, reason} ->
        error(conn, reason)
    end
  end

  def delete_resource(conn, params) do
    resource_id = Map.fetch!(conn.path_params, "resource_id")

    with {:ok, version} <- version_param(params),
         {:ok, resource} <- Workspaces.delete_resource(resource_id, version) do
      json(conn, %{deleted_resource_id: resource.id})
    else
      {:error, reason} -> error(conn, reason)
    end
  end

  def create_work_item(conn, params) do
    project_id = Map.fetch!(conn.path_params, "project_id")

    case Workspaces.create_work_item(project_id, params) do
      {:ok, work_item} ->
        conn
        |> put_status(:created)
        |> json(%{work_item: work_item_json(work_item)})

      {:error, reason} ->
        error(conn, reason)
    end
  end

  def update_work_item(conn, params) do
    work_item_id = Map.fetch!(conn.path_params, "work_item_id")

    case Workspaces.update_work_item(work_item_id, params) do
      {:ok, work_item} ->
        json(conn, %{work_item: work_item_json(work_item)})

      {:error, reason} ->
        error(conn, reason)
    end
  end

  def move_work_item(conn, params) do
    work_item_id = Map.fetch!(conn.path_params, "work_item_id")

    case Workspaces.move_work_item(work_item_id, params) do
      {:ok, work_item} ->
        json(conn, %{work_item: work_item_json(work_item)})

      {:error, reason} ->
        error(conn, reason)
    end
  end

  def show_work_item(conn, _params) do
    work_item_id = Map.fetch!(conn.path_params, "work_item_id")

    case Workspaces.work_item_snapshot(work_item_id) do
      {:ok, snapshot} ->
        projection =
          Map.get(
            ReadModel.projections([snapshot.work_item]),
            snapshot.work_item.orchestration_task_id
          )

        json(conn, PortalJSON.work_item_detail(snapshot, projection))

      {:error, reason} ->
        error(conn, reason)
    end
  end

  def work_item_timeline(conn, params) do
    work_item_id = Map.fetch!(conn.path_params, "work_item_id")

    with {:ok, work_item} <- Workspaces.fetch_work_item(work_item_id),
         task_id when is_binary(task_id) <- work_item.orchestration_task_id,
         {:ok, options} <- Protocol.history_options(params, task_id, "timeline", "before"),
         {:ok, page} <- Orchestration.task_timeline(task_id, options) do
      json(conn, Protocol.timeline_page(task_id, page))
    else
      nil -> error(conn, :state_conflict)
      {:error, reason} -> error(conn, reason)
    end
  end

  def run_work_item(conn, %{"action_id" => action_id}) when is_binary(action_id) do
    work_item_id = Map.fetch!(conn.path_params, "work_item_id")

    case Workspaces.launch_work_item(work_item_id, action_id) do
      {:ok, snapshot, disposition} ->
        status = if disposition == :created, do: :accepted, else: :ok

        conn
        |> put_status(status)
        |> json(%{
          disposition: Atom.to_string(disposition),
          work_item: work_item_json(snapshot.work_item),
          execution: PortalJSON.execution(snapshot.task)
        })

      {:error, reason} ->
        error(conn, reason)
    end
  end

  def run_work_item(conn, _params), do: error(conn, :invalid_request)

  def cancel_work_item(conn, %{"action_id" => action_id, "generation" => generation}) do
    work_item_id = Map.fetch!(conn.path_params, "work_item_id")

    case Workspaces.cancel_work_item(work_item_id, generation, action_id) do
      {:ok, snapshot, disposition} ->
        status = if disposition == :created, do: :accepted, else: :ok

        conn
        |> put_status(status)
        |> json(%{
          disposition: Atom.to_string(disposition),
          command: Protocol.command(snapshot.command),
          execution: PortalJSON.execution(snapshot.task)
        })

      {:error, reason} ->
        error(conn, reason)
    end
  end

  def cancel_work_item(conn, _params), do: error(conn, :invalid_request)

  def retry_work_item(conn, %{"action_id" => action_id, "generation" => generation})
      when is_binary(action_id) and is_integer(generation) do
    work_item_id = Map.fetch!(conn.path_params, "work_item_id")

    case Workspaces.retry_work_item(work_item_id, generation, action_id) do
      {:ok, snapshot, disposition} ->
        status = if disposition == :created, do: :accepted, else: :ok

        conn
        |> put_status(status)
        |> json(%{
          disposition: Atom.to_string(disposition),
          command: Protocol.command(snapshot.command),
          work_item: work_item_json(snapshot.work_item),
          execution: PortalJSON.execution(snapshot.task)
        })

      {:error, reason} ->
        error(conn, reason)
    end
  end

  def retry_work_item(conn, _params), do: error(conn, :invalid_request)

  def provide_input(
        conn,
        %{
          "input" => input,
          "action_id" => action_id,
          "waiting_transition_id" => waiting_transition_id
        }
      )
      when is_map(input) and is_binary(action_id) and is_binary(waiting_transition_id) do
    work_item_id = Map.fetch!(conn.path_params, "work_item_id")

    case Workspaces.provide_work_item_input(
           work_item_id,
           input,
           waiting_transition_id,
           action_id
         ) do
      {:ok, snapshot, disposition} ->
        status = if disposition == :created, do: :accepted, else: :ok

        conn
        |> put_status(status)
        |> json(%{
          disposition: Atom.to_string(disposition),
          command: Protocol.command(snapshot.command),
          execution: PortalJSON.execution(snapshot.task)
        })

      {:error, reason} ->
        error(conn, reason)
    end
  end

  def provide_input(conn, _params), do: error(conn, :invalid_request)

  defp hydrate_work_item(work_item),
    do:
      SymmetryControl.Repo.preload(work_item, [
        :project,
        :repository_resource,
        :ci_resource,
        :external_work_item_resource
      ])

  defp work_item_json(work_item) do
    work_item = hydrate_work_item(work_item)
    PortalJSON.work_item(work_item, ReadModel.projections([work_item]))
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

  defp projected_error(conn, reason, projection_key, projection) do
    {status, code, message} = Protocol.error_details(reason)

    conn
    |> put_status(status)
    |> json(%{projection_key => projection, error: %{code: code, message: message}})
  end

  defp sync_results(results) do
    Enum.map(results, fn
      {id, {:ok, _resource}} ->
        %{resource_id: id, status: "synced"}

      {id, {:error, reason, resource}} ->
        failed_sync_result(id, reason, resource)

      {id, {:error, reason}} ->
        failed_sync_result(id, reason, nil)
    end)
  end

  defp sync_failure(results) do
    formatted_results = sync_results(results)

    causes =
      formatted_results
      |> Enum.filter(&(&1.status == "failed"))
      |> Enum.group_by(& &1.error.code)
      |> Enum.map(fn {_code, failures} ->
        error = hd(failures).error
        Map.put(error, :count, length(failures))
      end)
      |> Enum.sort_by(& &1.code)

    {status, error} = aggregate_sync_error(causes)
    {status, Map.put(error, :causes, causes), formatted_results}
  end

  defp aggregate_sync_error([cause]) do
    {cause.http_status, Map.take(cause, [:code, :message])}
  end

  defp aggregate_sync_error(causes) when length(causes) > 1 do
    statuses = causes |> Enum.map(& &1.http_status) |> Enum.uniq()
    status = if length(statuses) == 1, do: hd(statuses), else: 422

    {status,
     %{
       code: "multiple_failures",
       message: "resources failed to synchronize for multiple reasons"
     }}
  end

  defp aggregate_sync_error([]) do
    {status, code, message} = Protocol.error_details(:provider_failure)
    {status, %{code: code, message: message}}
  end

  defp failed_sync_result(id, reason, resource) do
    {status, code, default_message} = Protocol.error_details(reason)
    message = resource_status_message(resource) || default_message

    %{
      resource_id: id,
      status: "failed",
      reason: to_string(reason),
      error: %{code: code, message: message, http_status: status}
    }
  end

  defp resource_status_message(%{status_message: message})
       when is_binary(message) and message != "",
       do: message

  defp resource_status_message(_resource), do: nil

  defp version_param(%{"version" => version}) when is_integer(version) and version > 0,
    do: {:ok, version}

  defp version_param(%{"version" => version}) when is_binary(version) do
    case Integer.parse(version) do
      {parsed, ""} when parsed > 0 -> {:ok, parsed}
      _ -> {:error, :invalid_request}
    end
  end

  defp version_param(_params), do: {:error, :invalid_request}
end
