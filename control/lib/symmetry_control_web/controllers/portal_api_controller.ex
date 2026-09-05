defmodule SymmetryControlWeb.PortalApiController do
  use SymmetryControlWeb, :controller

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
    do: SymmetryControl.Repo.preload(work_item, [:project, :repository_resource])

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
