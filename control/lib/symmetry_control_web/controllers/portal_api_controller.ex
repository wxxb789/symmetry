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

  def connections(conn, _params) do
    json(conn, PortalJSON.connections_read(ReadModel.connections()))
  end

  @doc """
  The attention entry point: the existing Goal attention feed merged with the
  project-level blocked work items the workspace read already serves.

  Goal rollout is not required for this page: the feed serves the durable Goal
  projection whether or not admission is enabled. The Goal feed's own admission
  refusal is the only refusal stated inline under HTTP 200 (the real workspace
  blockers stay served); caller-input errors keep their protocol status; any
  other Goal-feed reason is a server fault and surfaces with its real status.
  When the workspace read is unavailable the Goal entries are still served and
  the Project feed states the refusal. Nothing here grants or implies Goal
  authority.
  """
  def attention(conn, params) do
    with {:ok, project_id, goal_options} <- attention_options(conn, params),
         {:ok, goal_result} <- goal_attention(goal_options) do
      json(
        conn,
        PortalJSON.attention(
          project_id,
          goal_result,
          ReadModel.workspace(project_id)
        )
      )
    else
      {:error, reason} -> error(conn, reason)
    end
  end

  defp goal_attention(goal_options) do
    case SymmetryControl.Goals.attention(goal_options) do
      {:ok, page} ->
        {:ok, %{page: page, unavailable: nil}}

      # The caller's own input is a request error, not an unavailable feed.
      {:error, reason} when reason in [:invalid_cursor, :invalid_request] ->
        {:error, reason}

      # The feed's own admission refusal is the one refusal this page states
      # inline: the page serves the real project blockers and names the refusal.
      {:error, :goal_admission_disabled} ->
        {:ok, %{page: empty_attention_page(), unavailable: :goal_admission_disabled}}

      # Anything else is a server fault. It must surface with its real status
      # instead of being reported as a client error inside a 200 response.
      {:error, _unexpected} ->
        {:error, :internal_error}
    end
  end

  defp empty_attention_page, do: %{entries: [], next_after: nil, next_cursor: nil}

  defp attention_options(conn, params) when is_map(params) do
    # The route's own path params are not caller input; every other parameter
    # must be part of the accepted attention read.
    with true <- allowed_query_params?(conn, params, ["project_id", "cursor", "limit"]),
         {:ok, project_id} <- optional_uuid(Map.get(params, "project_id")),
         {:ok, cursor} <- optional_text(Map.get(params, "cursor")),
         {:ok, limit} <- optional_positive_integer(Map.get(params, "limit")) do
      goal_options =
        [project_id: project_id, cursor: cursor, limit: limit]
        |> Enum.reject(fn {_key, value} -> is_nil(value) end)

      {:ok, project_id, goal_options}
    else
      _ -> error_invalid_request()
    end
  end

  defp attention_options(_conn, _params), do: error_invalid_request()

  defp error_invalid_request, do: {:error, :invalid_request}

  defp allowed_query_params?(conn, params, allowed) do
    path_keys = MapSet.new(Map.keys(conn.path_params))

    params
    |> Map.keys()
    |> MapSet.new()
    |> MapSet.difference(path_keys)
    |> MapSet.subset?(MapSet.new(allowed))
  end

  defp optional_uuid(nil), do: {:ok, nil}

  defp optional_uuid(value) when is_binary(value) do
    case Ecto.UUID.cast(value) do
      {:ok, cast} -> {:ok, cast}
      :error -> {:error, :invalid_request}
    end
  end

  defp optional_uuid(_value), do: {:error, :invalid_request}

  defp optional_text(nil), do: {:ok, nil}
  defp optional_text(value) when is_binary(value) and byte_size(value) > 0, do: {:ok, value}
  defp optional_text(_value), do: {:error, :invalid_request}

  defp optional_positive_integer(nil), do: {:ok, nil}

  defp optional_positive_integer(value) when is_integer(value) and value > 0, do: {:ok, value}

  defp optional_positive_integer(value) when is_binary(value) do
    case Integer.parse(value) do
      {parsed, ""} when parsed > 0 -> {:ok, parsed}
      _ -> {:error, :invalid_request}
    end
  end

  defp optional_positive_integer(_value), do: {:error, :invalid_request}

  def create_connection(conn, params) do
    case Integrations.create_connection(params) do
      {:ok, connection} ->
        connection_receipt(conn, :created, connection, :created)

      {:replayed, connection} ->
        connection_receipt(conn, :ok, connection, :replayed)

      {:error, reason} ->
        error(conn, reason)
    end
  end

  def update_connection(conn, params) do
    connection_id = Map.fetch!(conn.path_params, "connection_id")

    case Integrations.update_connection(connection_id, Map.delete(params, "connection_id")) do
      {:ok, connection} -> connection_receipt(conn, :ok, connection, :updated)
      {:error, reason} -> error(conn, reason)
    end
  end

  def delete_connection(conn, params) do
    connection_id = Map.fetch!(conn.path_params, "connection_id")

    with {:ok, version} <- version_param(params),
         {:ok, connection} <- Integrations.delete_connection(connection_id, version) do
      json(conn, %{disposition: "deleted", deleted_connection_id: connection.id})
    else
      {:error, reason} -> error(conn, reason)
    end
  end

  def check_connection(conn, _params) do
    connection_id = Map.fetch!(conn.path_params, "connection_id")

    case Integrations.check_connection(connection_id) do
      {:ok, connection} ->
        connection_receipt(conn, :ok, connection, :checked)

      {:error, reason, connection} ->
        {status, code, message} = Protocol.error_details(reason)

        conn
        |> put_status(status)
        |> json(%{
          disposition: "checked",
          connection: connection_projection(connection),
          error: %{code: code, message: message}
        })

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
        |> json(%{disposition: "created", work_item: work_item_json(work_item)})

      {:replayed, work_item} ->
        json(conn, %{disposition: "replayed", work_item: work_item_json(work_item)})

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
      ], in_parallel: false)

  defp connection_receipt(conn, status, connection, disposition) do
    conn
    |> put_status(status)
    |> json(%{
      disposition: Atom.to_string(disposition),
      connection: connection_projection(connection)
    })
  end

  defp connection_projection(connection) do
    bound_resources =
      connection.id
      |> List.wrap()
      |> Integrations.list_bound_resources()
      |> Map.get(connection.id, [])

    PortalJSON.connection(connection, bound_resources)
  end

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
