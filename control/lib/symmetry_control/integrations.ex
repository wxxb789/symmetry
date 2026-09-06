defmodule SymmetryControl.Integrations do
  @moduledoc """
  Provider-neutral engineering connections and synchronization.

  Provider authorization comes from local CLI sessions and is never persisted
  or included in daemon protocol messages.
  """

  import Ecto.Query

  alias SymmetryControl.Integrations.Connection
  alias SymmetryControl.Orchestration.{Run, RunEvent, Task}
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces.{Project, ProjectResource, WorkItem}

  @secret_key_names ~w(
    accesstoken
    apikey
    authorization
    clientsecret
    credential
    password
    pat
    refreshtoken
    token
  )

  def list_connections do
    Repo.all(from connection in Connection, order_by: [asc: connection.name, asc: connection.id])
  end

  def create_connection(attrs) when is_map(attrs) do
    with :ok <- reject_secret_attrs(attrs),
         {:ok, auth_type} <- auth_type_for(attr_value(attrs, :provider)) do
      attrs =
        attrs
        |> drop_attrs([:status, :status_message, :metadata, :last_checked_at])
        |> put_attr(:auth_type, auth_type)

      %Connection{}
      |> Connection.changeset(attrs)
      |> Repo.insert()
    end
  end

  def update_connection(connection_id, attrs) when is_map(attrs) do
    with :ok <- reject_secret_attrs(attrs),
         {:ok, expected_version} <- expected_version(attrs),
         {:ok, connection_id} <- Ecto.UUID.cast(connection_id) do
      Repo.transaction(fn ->
        connection_snapshot =
          Repo.get(Connection, connection_id) || Repo.rollback(:not_found)

        project_ids =
          Repo.all(
            from resource in ProjectResource,
              where: resource.connection_id == ^connection_snapshot.id,
              distinct: resource.project_id,
              select: resource.project_id
          )

        Repo.all(
          from project in Project,
            where: project.id in ^project_ids,
            order_by: [asc: project.id],
            lock: "FOR UPDATE"
        )

        Repo.all(
          from resource in ProjectResource,
            where: resource.connection_id == ^connection_snapshot.id,
            order_by: [asc: resource.id],
            lock: "FOR UPDATE"
        )

        connection =
          Repo.one(
            from connection in Connection,
              where: connection.id == ^connection_id,
              lock: "FOR UPDATE"
          ) || Repo.rollback(:not_found)

        with :ok <- matches_version(connection, expected_version),
             :ok <- connection_identity_editable(connection, attrs) do
          attrs =
            attrs
            |> drop_attr(:version)
            |> drop_attrs([:auth_type, :status, :status_message, :metadata, :last_checked_at])

          changeset = Connection.update_changeset(connection, attrs)
          reset_health? = Map.has_key?(changeset.changes, :capabilities)

          changeset =
            if reset_health? do
              changeset
              |> Ecto.Changeset.put_change(:status, "unknown")
              |> Ecto.Changeset.put_change(:status_message, nil)
              |> Ecto.Changeset.put_change(:metadata, %{})
              |> Ecto.Changeset.put_change(:last_checked_at, nil)
            else
              changeset
            end

          case update_with_stale_error(changeset) do
            {:ok, updated} ->
              if reset_health? do
                from(resource in ProjectResource,
                  join: project in Project,
                  on: project.id == resource.project_id,
                  where: resource.connection_id == ^updated.id and project.status == "active"
                )
                |> Repo.update_all(
                  set: [
                    status: "unknown",
                    sync_status: "unknown",
                    status_message: nil,
                    last_checked_at: nil,
                    last_synced_at: nil
                  ],
                  inc: [lock_version: 1]
                )

                clear_removed_delivery_authority(connection, updated)
              end

              updated

            {:error, reason} ->
              Repo.rollback(reason)
          end
        else
          {:error, reason} -> Repo.rollback(reason)
        end
      end)
      |> unwrap_transaction()
    else
      :error -> {:error, :not_found}
      {:error, reason} -> {:error, reason}
    end
  end

  def delete_connection(connection_id, expected_version) do
    with {:ok, connection} <- fetch(Connection, connection_id),
         :ok <- matches_version(connection, expected_version) do
      connection
      |> Connection.delete_changeset()
      |> Repo.delete(stale_error_field: :lock_version, stale_error_message: "is stale")
      |> normalize_stale_result()
    end
  end

  def check_connection(connection_id) do
    with {:ok, connection} <- fetch(Connection, connection_id) do
      check_connection_snapshot(connection)
    end
  end

  defp check_connection_snapshot(%Connection{} = connection) do
    with provider <- provider_module(connection.provider),
         {:ok, auth} <- provider.authenticate(connection),
         {:ok, metadata} <- provider_call(fn -> provider.check(connection, auth) end),
         {:ok, metadata} <- validate_provider_map(metadata || %{}) do
      update_connection_health(connection, %{
        status: "healthy",
        status_message: nil,
        metadata: Map.merge(connection.metadata || %{}, metadata),
        last_checked_at: now()
      })
    else
      {:error, :stale} -> {:error, :stale}
      {:error, reason} -> record_connection_failure(connection, reason)
    end
  end

  def sync_project(project_id) do
    with {:ok, project} <- fetch(Project, project_id),
         :ok <- require_active_project(project) do
      resources =
        Repo.all(
          from resource in ProjectResource,
            where: resource.project_id == ^project.id and not is_nil(resource.connection_id),
            order_by: [asc: resource.inserted_at, asc: resource.id]
        )

      results = Enum.map(resources, fn resource -> {resource.id, sync_resource(resource.id)} end)

      if Enum.all?(results, fn {_id, result} -> match?({:ok, _}, result) end),
        do: {:ok, results},
        else: {:error, results}
    end
  end

  def sync_all_resources do
    results =
      Repo.all(
        from resource in ProjectResource,
          join: project in Project,
          on: project.id == resource.project_id,
          where: project.status == "active" and not is_nil(resource.connection_id),
          order_by: [asc: resource.inserted_at, asc: resource.id]
      )
      |> Enum.map(fn resource -> {resource.id, sync_resource(resource.id)} end)

    %{
      synced: Enum.count(results, fn {_id, result} -> match?({:ok, _}, result) end),
      failed: Enum.count(results, fn {_id, result} -> not match?({:ok, _}, result) end),
      results: results
    }
  end

  def sync_resource(resource_id) do
    with {:ok, resource} <- fetch(ProjectResource, resource_id),
         {:ok, project} <- fetch(Project, resource.project_id),
         :ok <- require_active_project(project) do
      sync_connected_resource(resource)
    end
  end

  def execute_provider_action(
        %Connection{} = connection,
        %ProjectResource{} = resource,
        %WorkItem{} = work_item,
        operation,
        input
      )
      when operation in ["change.upsert", "change.update"] and is_map(input) do
    with true <- resource.kind == "repository" || {:error, :forbidden},
         true <- work_item.repository_resource_id == resource.id || {:error, :forbidden},
         true <- resource.connection_id == connection.id || {:error, :forbidden},
         :ok <- require_capabilities(connection, ["repositories", "changes"]),
         provider <- provider_module(connection.provider),
         {:ok, auth} <- provider.authenticate(connection) do
      execute_authenticated_provider_action(
        provider,
        connection,
        resource,
        work_item,
        operation,
        input,
        auth
      )
    else
      {:error, reason} -> record_provider_action_failure(resource, reason, :definite)
    end
  end

  def execute_provider_action(_, _, _, _, _), do: {:error, :invalid_request}

  defp execute_authenticated_provider_action(
         provider,
         connection,
         resource,
         work_item,
         operation,
         input,
         auth
       ) do
    case provider_call(fn ->
           provider.execute(connection, resource, work_item, operation, input, auth)
         end) do
      {:ok, delivery} ->
        persist_provider_delivery(connection, resource, work_item, operation, delivery)

      {:error, reason} ->
        record_provider_action_failure(resource, reason, mutation_failure_outcome(reason))
    end
  end

  defp persist_provider_delivery(connection, resource, work_item, operation, delivery) do
    with true <- valid_provider_action_delivery?(delivery) || {:error, :invalid_provider_response},
         {:ok, attrs} <- delivery_attrs(work_item, delivery) do
      case persist_provider_action(connection, resource, work_item, attrs) do
        {:ok, persisted} ->
          {:ok,
           provider_action_result(
             operation,
             resource,
             persisted,
             provider_action_delivery(persisted),
             true
           )}

        {:error, reason}
        when reason in [:stale, :state_conflict, :capability_not_granted] ->
          {:ok,
           provider_action_result(
             operation,
             resource,
             work_item,
             provider_action_delivery(delivery),
             false
           )}

        {:error, reason} ->
          record_provider_action_failure(resource, reason, :ambiguous)
      end
    else
      {:error, reason} -> record_provider_action_failure(resource, reason, :ambiguous)
    end
  end

  defp valid_provider_action_delivery?(delivery) when is_map(delivery) do
    case Map.get(delivery, :pull_request_url) do
      value when is_binary(value) -> String.trim(value) != ""
      _value -> false
    end
  end

  defp valid_provider_action_delivery?(_delivery), do: false

  defp mutation_failure_outcome({:http, status, _body}) when status in 400..499, do: :definite

  defp mutation_failure_outcome(reason) do
    if provider_action_error(reason) in [
         :invalid_request,
         :forbidden,
         :not_found,
         :provider_unauthorized
       ],
       do: :definite,
       else: :ambiguous
  end

  def execute_accepted_resource_sync(%Connection{} = connection, %ProjectResource{} = resource) do
    provider = provider_module(connection.provider)

    with {:ok, auth} <- provider.authenticate(connection),
         {:ok, result} <-
           provider_call(fn -> provider.sync_resource(connection, resource, auth) end),
         {:ok, deliveries} <-
           provider_call(fn ->
             delivery_updates(provider, connection, delivery_resource(resource, result), auth)
           end) do
      case persist_sync(connection, resource, result, deliveries) do
        {:ok, synced} ->
          {:ok, accepted_sync_result(synced, true)}

        {:error, reason} when reason in [:stale, :state_conflict] ->
          {:ok, accepted_sync_result(resource, false)}

        {:error, reason} ->
          record_provider_action_failure(resource, reason)
      end
    else
      {:error, reason} -> record_provider_action_failure(resource, reason)
    end
  end

  def execute_accepted_resource_sync(_, _), do: {:error, :invalid_request}

  defp sync_connected_resource(resource) do
    with true <- is_binary(resource.connection_id) || {:error, :not_connected},
         connection <- Repo.get!(Connection, resource.connection_id),
         :ok <- require_capability(connection, resource.kind),
         provider <- provider_module(connection.provider),
         {:ok, auth} <- provider.authenticate(connection),
         {:ok, result} <-
           provider_call(fn -> provider.sync_resource(connection, resource, auth) end),
         {:ok, deliveries} <-
           provider_call(fn ->
             delivery_updates(provider, connection, delivery_resource(resource, result), auth)
           end),
         {:ok, synced} <- persist_sync(connection, resource, result, deliveries) do
      {:ok, synced}
    else
      {:error, :stale} -> {:error, :stale}
      {:error, :state_conflict} -> {:error, :state_conflict}
      {:error, reason} -> record_resource_failure(resource, reason)
      false -> record_resource_failure(resource, :not_connected)
    end
  end

  defp delivery_resource(resource, result) when is_map(result) do
    provider_metadata = result |> Map.get(:resource, %{}) |> Map.get(:metadata, %{})
    %{resource | metadata: Map.merge(resource.metadata || %{}, provider_metadata || %{})}
  end

  defp delivery_resource(resource, _result), do: resource

  def validate_resource_reference(connection, kind, reference) do
    connection.provider
    |> provider_module()
    |> apply(:validate_resource_reference, [connection, kind, reference])
  end

  defp delivery_updates(provider, connection, %{kind: kind} = resource, auth)
       when kind == "repository" do
    items =
      Repo.all(
        from item in WorkItem,
          where:
            item.repository_resource_id == ^resource.id and
              (not is_nil(item.pull_request_url) or
                 not is_nil(item.external_pull_request_url) or
                 not is_nil(item.orchestration_task_id)),
          order_by: [asc: item.id]
      )

    pull_requests = latest_pull_requests(items)

    Enum.reduce_while(items, {:ok, []}, fn item, {:ok, updates} ->
      item = with_effective_pull_request(item, pull_requests)

      case maybe_sync_delivery(provider, connection, resource, item, auth) do
        {:ok, nil} -> {:cont, {:ok, updates}}
        {:ok, delivery} -> {:cont, {:ok, [{item, delivery} | updates]}}
        {:error, reason} -> {:halt, {:error, reason}}
      end
    end)
  end

  defp delivery_updates(provider, connection, %{kind: "ci"} = resource, auth) do
    Repo.all(
      from item in WorkItem,
        where: item.ci_resource_id == ^resource.id,
        order_by: [asc: item.id]
    )
    |> Enum.reduce_while({:ok, []}, fn item, {:ok, updates} ->
      case provider.sync_ci(connection, resource, item, auth) do
        {:ok, delivery} -> {:cont, {:ok, [{item, delivery} | updates]}}
        {:error, :missing_ci_reference} -> {:cont, {:ok, updates}}
        {:error, reason} -> {:halt, {:error, reason}}
      end
    end)
  end

  defp delivery_updates(_provider, _connection, _resource, _auth), do: {:ok, []}

  defp maybe_sync_delivery(_provider, _connection, _resource, %{pull_request_url: nil}, _auth),
    do: {:ok, nil}

  defp maybe_sync_delivery(provider, connection, resource, item, auth) do
    if "changes" in connection.capabilities do
      connection =
        if is_binary(item.ci_resource_id),
          do: %{connection | capabilities: List.delete(connection.capabilities, "ci")},
          else: connection

      provider.sync_delivery(connection, resource, item, auth)
    else
      {:ok, nil}
    end
  end

  defp with_effective_pull_request(%WorkItem{pull_request_url: url} = item, _pull_requests)
       when is_binary(url) and url != "",
       do: item

  defp with_effective_pull_request(%WorkItem{orchestration_task_id: nil} = item, _pull_requests),
    do: with_cached_pull_request(item)

  defp with_effective_pull_request(item, pull_requests) do
    case pull_requests[item.orchestration_task_id] do
      url when is_binary(url) and url != "" -> %{item | pull_request_url: url}
      _ -> with_cached_pull_request(item)
    end
  end

  defp with_cached_pull_request(%WorkItem{external_pull_request_url: url} = item)
       when is_binary(url) and url != "",
       do: %{item | pull_request_url: url}

  defp with_cached_pull_request(item), do: item

  defp latest_pull_requests(items) do
    task_ids = items |> Enum.map(& &1.orchestration_task_id) |> Enum.reject(&is_nil/1)

    if task_ids == [] do
      %{}
    else
      Repo.all(
        from event in RunEvent,
          join: run in Run,
          on: run.id == event.run_id,
          join: task in Task,
          on: task.id == run.task_id and task.attempt_generation == run.generation,
          where: task.id in ^task_ids and event.kind == "pull_request",
          distinct: task.id,
          order_by: [asc: task.id, desc: event.sequence, desc: event.inserted_at, desc: event.id],
          select: {task.id, event.payload}
      )
      |> Map.new(fn {task_id, payload} -> {task_id, payload["url"] || payload[:url]} end)
    end
  end

  defp persist_sync(connection, resource, result, deliveries) do
    with {:ok, resource_attrs} <- provider_resource_attrs(result, resource),
         {:ok, work_items} <- provider_work_items(result),
         {:ok, prepared_deliveries} <- prepare_deliveries(deliveries) do
      Repo.transaction(fn ->
        synced_at = now()

        project =
          Repo.one(
            from project in Project,
              where: project.id == ^resource.project_id,
              lock: "FOR UPDATE"
          )

        with :ok <- require_active_project(project),
             {:ok, synced_resource} <-
               resource
               |> ProjectResource.sync_changeset(
                 Map.merge(resource_attrs, %{
                   provider: connection.provider,
                   status: "healthy",
                   sync_status: "synced",
                   status_message: nil,
                   last_checked_at: synced_at,
                   last_synced_at: synced_at
                 })
               )
               |> update_with_stale_error(),
             :ok <- upsert_work_items(synced_resource, connection, work_items),
             :ok <- persist_deliveries(prepared_deliveries) do
          synced_resource
        else
          {:error, reason} -> Repo.rollback(reason)
        end
      end)
    end
  end

  defp provider_resource_attrs(result, resource) when is_map(result) do
    case Map.get(result, :resource, %{}) do
      attrs when is_map(attrs) ->
        case Map.get(attrs, :metadata, %{}) do
          metadata when is_map(metadata) ->
            with {:ok, metadata} <- validate_provider_map(metadata) do
              {:ok, Map.put(attrs, :metadata, Map.merge(resource.metadata || %{}, metadata))}
            end

          _metadata ->
            {:error, :invalid_provider_response}
        end

      _attrs ->
        {:error, :invalid_provider_response}
    end
  end

  defp provider_resource_attrs(_result, _resource), do: {:error, :invalid_provider_response}

  defp provider_work_items(result) do
    if is_map(result) do
      case Map.get(result, :work_items, []) do
        items when is_list(items) ->
          with true <- Enum.all?(items, &is_map/1),
               :ok <- validate_work_item_provider_data(items) do
            {:ok, items}
          else
            _invalid -> {:error, :invalid_provider_response}
          end

        _items ->
          {:error, :invalid_provider_response}
      end
    else
      {:error, :invalid_provider_response}
    end
  end

  defp prepare_deliveries(deliveries) when is_list(deliveries),
    do: prepare_deliveries(deliveries, [])

  defp prepare_deliveries([], prepared), do: {:ok, Enum.reverse(prepared)}

  defp prepare_deliveries(
         [{%WorkItem{} = item, delivery} | remaining],
         prepared
       )
       when is_map(delivery) do
    with {:ok, attrs} <- delivery_attrs(item, delivery) do
      prepare_deliveries(remaining, [{item, attrs} | prepared])
    end
  end

  defp prepare_deliveries(_deliveries, _prepared),
    do: {:error, :invalid_provider_response}

  defp persist_deliveries(prepared_deliveries) do
    Enum.reduce_while(prepared_deliveries, :ok, fn {item, attrs}, :ok ->
      case item |> WorkItem.delivery_changeset(attrs) |> update_with_stale_error() do
        {:ok, _item} ->
          {:cont, :ok}

        {:error, reason} ->
          {:halt, {:error, reason}}
      end
    end)
  end

  defp persist_provider_action(connection, resource, work_item, attrs) do
    Repo.transaction(fn ->
      project =
        Repo.one(
          from project in Project,
            where: project.id == ^work_item.project_id,
            lock: "FOR UPDATE"
        )

      current_resource =
        Repo.one(
          from current in ProjectResource,
            where: current.id == ^resource.id,
            lock: "FOR UPDATE"
        )

      current_connection =
        Repo.one(
          from current in Connection,
            where: current.id == ^connection.id,
            lock: "FOR UPDATE"
        )

      current_work_item =
        Repo.one(
          from current in WorkItem,
            where: current.id == ^work_item.id,
            lock: "FOR UPDATE"
        )

      with :ok <- require_active_project(project),
           true <- match?(%ProjectResource{}, current_resource) || {:error, :stale},
           true <- match?(%Connection{}, current_connection) || {:error, :stale},
           true <- match?(%WorkItem{}, current_work_item) || {:error, :stale},
           :ok <- matches_version(current_work_item, work_item.lock_version),
           true <- current_resource.project_id == current_work_item.project_id || {:error, :stale},
           true <- current_resource.connection_id == current_connection.id || {:error, :stale},
           true <- same_resource_identity?(current_resource, resource) || {:error, :stale},
           true <- same_connection_identity?(current_connection, connection) || {:error, :stale},
           true <-
             current_work_item.repository_resource_id == current_resource.id ||
               {:error, :stale},
           {:ok, persisted} <-
             current_work_item
             |> WorkItem.delivery_changeset(attrs)
             |> update_with_stale_error() do
        persisted
      else
        {:error, reason} -> Repo.rollback(reason)
      end
    end)
    |> unwrap_transaction()
  end

  defp same_resource_identity?(current, snapshot) do
    current.connection_id == snapshot.connection_id and current.provider == snapshot.provider and
      current.kind == snapshot.kind and current.external_ref == snapshot.external_ref
  end

  defp same_connection_identity?(current, snapshot) do
    current.provider == snapshot.provider and current.account_ref == snapshot.account_ref
  end

  defp delivery_attrs(item, delivery) when is_map(delivery) do
    provider_data = Map.get(delivery, :provider_data, %{})

    with {:ok, provider_data} <- validate_provider_map(provider_data) do
      attrs =
        %{}
        |> maybe_put_delivery(delivery, :pull_request_url, :external_pull_request_url)
        |> maybe_put_delivery(delivery, :pull_request_state, :external_pull_request_state)
        |> maybe_put_delivery(delivery, :ci_status, :external_ci_status)
        |> maybe_put_delivery(delivery, :review_status, :external_review_status)
        |> maybe_put_change_timestamp(delivery)
        |> maybe_put_delivery_data(item, delivery, provider_data)

      {:ok, attrs}
    end
  end

  defp validate_work_item_provider_data(items) do
    Enum.reduce_while(items, :ok, fn item, :ok ->
      case validate_provider_map(Map.get(item, :provider_data, %{})) do
        {:ok, _provider_data} -> {:cont, :ok}
        {:error, reason} -> {:halt, {:error, reason}}
      end
    end)
  end

  defp validate_provider_map(value) when is_map(value) do
    if secret_data?(value),
      do: {:error, :invalid_provider_response},
      else: {:ok, value}
  end

  defp validate_provider_map(_value), do: {:error, :invalid_provider_response}

  defp secret_data?(value) when is_map(value) do
    Enum.any?(value, fn {key, nested} -> secret_key?(key) or secret_data?(nested) end)
  end

  defp secret_data?(value) when is_list(value), do: Enum.any?(value, &secret_data?/1)
  defp secret_data?(_value), do: false

  defp secret_key?(key) when is_binary(key) or is_atom(key) do
    normalized =
      key
      |> to_string()
      |> String.downcase()
      |> String.replace(~r/[^a-z0-9]/u, "")

    normalized in @secret_key_names
  end

  defp secret_key?(_key), do: false

  defp provider_action_delivery(%WorkItem{} = work_item) do
    %{
      pull_request_url: work_item.external_pull_request_url,
      pull_request_state: work_item.external_pull_request_state,
      review_status: work_item.external_review_status,
      ci_status: work_item.external_ci_status,
      updated_at: work_item.external_change_updated_at
    }
  end

  defp provider_action_delivery(delivery) when is_map(delivery) do
    %{
      pull_request_url: Map.get(delivery, :pull_request_url),
      pull_request_state: Map.get(delivery, :pull_request_state),
      review_status: Map.get(delivery, :review_status),
      ci_status: Map.get(delivery, :ci_status),
      updated_at: Map.get(delivery, :updated_at)
    }
  end

  defp provider_action_result(operation, resource, work_item, delivery, projected) do
    %{
      operation: operation,
      resource_id: resource.id,
      work_item_id: work_item.id,
      projected: projected,
      delivery: delivery
    }
  end

  defp accepted_sync_result(resource, projected) do
    %{
      operation: "resource.sync",
      resource_id: resource.id,
      projected: projected,
      resource: %{
        provider: resource.provider,
        kind: resource.kind,
        status: resource.status,
        sync_status: resource.sync_status,
        last_synced_at: resource.last_synced_at
      }
    }
  end

  defp provider_action_error(reason)
       when reason in [
              :invalid_request,
              :not_found,
              :forbidden,
              :stale,
              :state_conflict,
              :ownership_lost
            ],
       do: reason

  defp provider_action_error(reason), do: reason |> failure() |> elem(0)

  defp record_provider_action_failure(resource, reason, outcome \\ nil) do
    case provider_action_error(reason) do
      code
      when code in [
             :invalid_request,
             :forbidden,
             :stale,
             :state_conflict,
             :ownership_lost
           ] ->
        provider_action_failure_result(code, outcome)

      provider_error ->
        case record_resource_failure(resource, reason) do
          {:error, _code, _degraded} -> provider_action_failure_result(provider_error, outcome)
          {:error, _update_reason} -> provider_action_failure_result(provider_error, outcome)
        end
    end
  end

  defp provider_action_failure_result(code, nil), do: {:error, code}

  defp provider_action_failure_result(code, outcome),
    do: {:error, {:provider_action_failure, code, outcome}}

  defp maybe_put_delivery(attrs, delivery, source, target) do
    if Map.has_key?(delivery, source),
      do: Map.put(attrs, target, Map.get(delivery, source)),
      else: attrs
  end

  defp maybe_put_change_timestamp(attrs, delivery) do
    attrs =
      if Map.has_key?(delivery, :pull_request_url) or Map.has_key?(delivery, :review_status),
        do: Map.put(attrs, :external_change_updated_at, Map.get(delivery, :updated_at)),
        else: attrs

    if Map.has_key?(delivery, :ci_status),
      do: Map.put(attrs, :external_ci_updated_at, Map.get(delivery, :updated_at)),
      else: attrs
  end

  defp maybe_put_delivery_data(attrs, item, delivery, provider_data) do
    attrs =
      if Map.has_key?(delivery, :pull_request_url) or Map.has_key?(delivery, :review_status) do
        Map.put(
          attrs,
          :external_change_data,
          Map.merge(item.external_change_data || %{}, Map.delete(provider_data, "ci_url"))
        )
      else
        attrs
      end

    if Map.has_key?(delivery, :ci_status) do
      Map.put(
        attrs,
        :external_ci_data,
        Map.merge(item.external_ci_data || %{}, Map.take(provider_data, ["ci_url"]))
      )
    else
      attrs
    end
  end

  defp upsert_work_items(resource, connection, items) do
    repository_id = matching_repository(resource)

    with {:ok, items} <- prepare_provider_work_items(resource, connection, repository_id, items) do
      do_upsert_work_items(resource, connection, repository_id, items)
    end
  end

  defp do_upsert_work_items(resource, connection, repository_id, items) do
    external_ids = Enum.map(items, &Map.get(&1, :external_id))

    existing =
      Repo.all(
        from item in WorkItem,
          where:
            item.external_work_item_resource_id == ^resource.id and
              item.external_id in ^external_ids
      )
      |> Map.new(&{&1.external_id, &1})

    positions =
      Repo.all(
        from item in WorkItem,
          where: item.project_id == ^resource.project_id,
          group_by: item.status,
          select: {item.status, max(item.position)}
      )
      |> Map.new()

    result =
      Enum.reduce_while(items, {:ok, positions}, fn item, {:ok, current_positions} ->
        attrs = provider_work_item_attrs(resource, connection, item)

        case existing[Map.get(item, :external_id)] do
          nil ->
            status = Map.get(item, :status)
            position = Map.get(current_positions, status, -1) + 1

            insert_result =
              %WorkItem{}
              |> WorkItem.provider_changeset(
                Map.merge(attrs, %{
                  repository_resource_id: repository_id,
                  status: status,
                  assignee_type: "unassigned",
                  blocked: false,
                  position: position
                })
              )
              |> Repo.insert()

            case insert_result do
              {:ok, _item} -> {:cont, {:ok, Map.put(current_positions, status, position)}}
              {:error, reason} -> {:halt, {:error, reason}}
            end

          work_item ->
            attrs =
              if is_nil(work_item.orchestration_task_id) and
                   is_nil(work_item.repository_resource_id) and is_binary(repository_id),
                 do: Map.put(attrs, :repository_resource_id, repository_id),
                 else: attrs

            case update_provider_work_item(work_item, attrs) do
              {:ok, _item} -> {:cont, {:ok, current_positions}}
              {:error, reason} -> {:halt, {:error, reason}}
            end
        end
      end)

    case result do
      {:ok, _positions} -> mark_missing_work_items_unavailable(resource.id, external_ids)
      {:error, reason} -> {:error, reason}
    end
  end

  defp prepare_provider_work_items(resource, connection, repository_id, items) do
    Enum.reduce_while(items, {:ok, [], %{}}, fn item, {:ok, order, by_id} ->
      with :ok <- validate_provider_revision(connection.provider, item),
           {:ok, item} <-
             validate_provider_work_item(resource, connection, repository_id, item) do
        external_id = Map.get(item, :external_id)

        case by_id[external_id] do
          nil ->
            {:cont, {:ok, [external_id | order], Map.put(by_id, external_id, item)}}

          existing ->
            case provider_item_order(connection.provider, item, existing) do
              :newer ->
                {:cont, {:ok, order, Map.put(by_id, external_id, item)}}

              :older ->
                {:cont, {:ok, order, by_id}}

              :same ->
                if provider_item_snapshot(item) == provider_item_snapshot(existing),
                  do: {:cont, {:ok, order, by_id}},
                  else: {:halt, {:error, :invalid_provider_response}}
            end
        end
      else
        {:error, reason} -> {:halt, {:error, reason}}
      end
    end)
    |> case do
      {:ok, order, by_id} -> {:ok, order |> Enum.reverse() |> Enum.map(&Map.fetch!(by_id, &1))}
      {:error, reason} -> {:error, reason}
    end
  end

  defp validate_provider_revision("azure_devops", item) do
    case item |> Map.get(:provider_data, %{}) |> provider_revision() do
      revision when is_integer(revision) and revision > 0 -> :ok
      _revision -> {:error, :invalid_provider_response}
    end
  end

  defp validate_provider_revision(_provider, _item), do: :ok

  defp validate_provider_work_item(resource, connection, repository_id, item) do
    attrs =
      resource
      |> provider_work_item_attrs(connection, item)
      |> Map.merge(%{
        repository_resource_id: repository_id,
        status: Map.get(item, :status),
        assignee_type: "unassigned",
        blocked: false,
        position: 0
      })

    changeset = WorkItem.provider_changeset(%WorkItem{}, attrs)

    if changeset.valid? do
      normalized = Ecto.Changeset.apply_changes(changeset)

      {:ok,
       %{
         external_id: normalized.external_id,
         external_url: normalized.external_url,
         external_state: normalized.external_state,
         external_updated_at: normalized.external_updated_at,
         external_assignee_name: normalized.external_assignee_name,
         labels: normalized.labels,
         provider_data: normalized.external_data,
         title: normalized.title,
         description: normalized.description,
         status: normalized.status,
         priority: normalized.priority
       }}
    else
      {:error, changeset}
    end
  end

  defp provider_item_order("azure_devops", incoming, stored) do
    case {provider_revision(incoming.provider_data), provider_revision(stored.provider_data)} do
      {incoming_revision, stored_revision}
      when is_integer(incoming_revision) and is_integer(stored_revision) ->
        compare(incoming_revision, stored_revision)

      {nil, stored_revision} when is_integer(stored_revision) ->
        :older

      {_incoming_revision, nil} ->
        provider_item_order("timestamp", incoming, stored)
    end
  end

  defp provider_item_order(_provider, incoming, stored) do
    incoming.external_updated_at
    |> DateTime.compare(stored.external_updated_at)
    |> compare_result()
  end

  defp provider_item_snapshot(item) do
    Map.take(item, [
      :external_url,
      :external_state,
      :external_updated_at,
      :external_assignee_name,
      :labels,
      :provider_data,
      :title,
      :description,
      :status,
      :priority
    ])
  end

  defp update_provider_work_item(work_item, attrs) do
    changeset = WorkItem.provider_update_changeset(work_item, attrs)

    if changeset.valid? do
      candidate = Ecto.Changeset.apply_changes(changeset)

      case provider_snapshot_order(candidate, work_item) do
        :newer ->
          update_with_stale_error(changeset)

        :older ->
          restore_provider_availability(work_item, attrs)

        :same ->
          if provider_snapshot(candidate) == provider_snapshot(work_item),
            do: restore_provider_availability(work_item, attrs),
            else: {:error, :invalid_provider_response}
      end
    else
      {:error, changeset}
    end
  end

  defp restore_provider_availability(work_item, attrs) do
    availability_attrs =
      attrs
      |> Map.take([:repository_resource_id])
      |> Map.put(:external_available, true)

    work_item
    |> WorkItem.provider_update_changeset(availability_attrs)
    |> update_with_stale_error()
  end

  defp provider_snapshot_order(
         %{external_provider: "azure_devops"} = candidate,
         %{external_provider: "azure_devops"} = current
       ) do
    case {provider_revision(candidate.external_data), provider_revision(current.external_data)} do
      {incoming, stored} when is_integer(incoming) and is_integer(stored) ->
        compare(incoming, stored)

      {_incoming, stored} when is_integer(stored) ->
        :older

      _revisions ->
        compare_external_timestamp(candidate, current)
    end
  end

  defp provider_snapshot_order(
         %{external_provider: "github"} = candidate,
         %{external_provider: "github"} = current
       ) do
    case compare_external_timestamp(candidate, current) do
      :same -> :newer
      order -> order
    end
  end

  defp provider_snapshot_order(candidate, current),
    do: compare_external_timestamp(candidate, current)

  defp compare_external_timestamp(
         %{external_updated_at: %DateTime{} = incoming},
         %{external_updated_at: %DateTime{} = stored}
       ),
       do: DateTime.compare(incoming, stored) |> compare_result()

  defp compare_external_timestamp(%{external_updated_at: %DateTime{}}, _current), do: :newer
  defp compare_external_timestamp(_candidate, _current), do: :older

  defp provider_snapshot(item) do
    Map.take(item, [
      :external_url,
      :external_state,
      :external_updated_at,
      :external_assignee_name,
      :labels,
      :external_data,
      :title,
      :description,
      :priority
    ])
  end

  defp compare(left, right) when left > right, do: :newer
  defp compare(left, right) when left < right, do: :older
  defp compare(_left, _right), do: :same

  defp compare_result(:gt), do: :newer
  defp compare_result(:lt), do: :older
  defp compare_result(:eq), do: :same

  defp provider_revision(data) when is_map(data) do
    case Map.get(data, "revision", Map.get(data, :revision)) do
      revision when is_integer(revision) and revision >= 0 -> revision
      _revision -> nil
    end
  end

  defp provider_revision(_data), do: nil

  defp mark_missing_work_items_unavailable(resource_id, external_ids) do
    query =
      from item in WorkItem,
        where:
          item.external_work_item_resource_id == ^resource_id and item.external_available == true

    query =
      if external_ids == [],
        do: query,
        else: from(item in query, where: item.external_id not in ^external_ids)

    Repo.update_all(query,
      set: [external_available: false, updated_at: now()],
      inc: [lock_version: 1]
    )

    :ok
  end

  defp provider_work_item_attrs(resource, connection, item) do
    %{
      project_id: resource.project_id,
      external_work_item_resource_id: resource.id,
      external_provider: connection.provider,
      external_id: Map.get(item, :external_id),
      external_url: Map.get(item, :external_url),
      external_state: Map.get(item, :external_state),
      external_updated_at: Map.get(item, :external_updated_at),
      external_available: true,
      external_assignee_name: Map.get(item, :external_assignee_name),
      external_data: Map.get(item, :provider_data, %{}),
      labels: Map.get(item, :labels, []),
      title: Map.get(item, :title),
      description: Map.get(item, :description),
      priority: Map.get(item, :priority)
    }
  end

  defp matching_repository(%{provider: "github", external_ref: reference} = resource)
       when is_binary(reference) do
    reference = String.downcase(reference)

    Repo.one(
      from repository in ProjectResource,
        where:
          repository.project_id == ^resource.project_id and
            repository.connection_id == ^resource.connection_id and
            repository.kind == "repository" and
            fragment("lower(?)", repository.external_ref) == ^reference,
        select: repository.id,
        limit: 1
    )
  end

  defp matching_repository(_resource), do: nil

  defp update_connection_health(connection, attrs) do
    connection
    |> Connection.health_changeset(attrs)
    |> update_with_stale_error()
  end

  defp record_connection_failure(%Connection{} = connection, reason) do
    {code, message} = failure(reason)

    case update_connection_health(connection, %{
           status: "degraded",
           status_message: message,
           metadata: connection.metadata || %{},
           last_checked_at: now()
         }) do
      {:ok, degraded} -> {:error, code, degraded}
      {:error, update_reason} -> {:error, update_reason}
    end
  end

  defp record_resource_failure(%ProjectResource{} = resource_snapshot, reason) do
    {code, message} = failure(reason)

    case Repo.transaction(fn ->
           project =
             Repo.one(
               from project in Project,
                 where: project.id == ^resource_snapshot.project_id,
                 lock: "FOR UPDATE"
             )

           resource =
             Repo.one(
               from resource in ProjectResource,
                 where:
                   resource.id == ^resource_snapshot.id and
                     resource.project_id == ^resource_snapshot.project_id,
                 lock: "FOR UPDATE"
             ) || Repo.rollback(:stale)

           with :ok <- require_active_project(project),
                :ok <- matches_version(resource, resource_snapshot.lock_version),
                {:ok, degraded} <-
                  resource
                  |> ProjectResource.sync_changeset(%{
                    status: "degraded",
                    sync_status: "failed",
                    status_message: message,
                    last_checked_at: now()
                  })
                  |> update_with_stale_error() do
             degraded
           else
             {:error, update_reason} -> Repo.rollback(update_reason)
           end
         end) do
      {:ok, degraded} -> {:error, code, degraded}
      {:error, update_reason} -> {:error, update_reason}
    end
  end

  defp failure({:http, 401, _body}),
    do: {:provider_unauthorized, "Provider authentication failed"}

  defp failure({:http, 403, _body}), do: {:forbidden, "Provider permission denied"}
  defp failure({:http, 404, _body}), do: {:not_found, "Provider resource was not found"}

  defp failure({:http, status, _body}),
    do: {:provider_failure, "Provider returned HTTP #{status}"}

  defp failure(:not_connected), do: {:not_connected, "Resource has no external connection"}
  defp failure(:capability_not_granted), do: {:forbidden, "Connection capability is not granted"}

  defp failure(:github_https_required),
    do: {:provider_failure, "GitHub CLI authentication must use HTTPS"}

  defp failure(:github_oauth_required),
    do: {:provider_unauthorized, "GitHub CLI must use browser-based OAuth authentication"}

  defp failure(:authentication_unavailable),
    do: {:provider_unauthorized, "Provider CLI is not authenticated"}

  defp failure({:command_unavailable, executable}),
    do: {:provider_failure, "Required provider CLI is unavailable: #{executable}"}

  defp failure({:command_failed, executable}),
    do: {:provider_unauthorized, "Provider CLI authentication failed: #{executable}"}

  defp failure({:command_timeout, executable}),
    do: {:provider_failure, "Provider CLI authentication timed out: #{executable}"}

  defp failure({:transport, _reason}), do: {:provider_failure, "Provider transport failed"}

  defp failure(:invalid_provider_response),
    do: {:provider_failure, "Provider response is invalid"}

  defp failure(%Ecto.Changeset{}), do: {:provider_failure, "Provider response failed validation"}
  defp failure(:forbidden), do: {:forbidden, "Provider account does not own this resource"}

  defp failure(:invalid_repository_reference),
    do: {:invalid_request, "Repository reference is invalid"}

  defp failure(:invalid_pull_request_url),
    do: {:invalid_request, "Pull request URL does not match the bound repository"}

  defp failure(:missing_head_sha),
    do: {:provider_failure, "Pull request is missing a head commit"}

  defp failure(:missing_commit_id),
    do: {:provider_failure, "Pull request is missing a source commit"}

  defp failure(:missing_ci_reference),
    do: {:provider_failure, "Work item has no commit or branch for CI lookup"}

  defp failure(:ci_history_limit),
    do: {:provider_failure, "CI history search exceeded the configured page limit"}

  defp failure(:unsupported_resource),
    do: {:invalid_request, "Provider resource type is unsupported"}

  defp failure(_reason), do: {:provider_failure, "Provider request failed"}

  defp provider_call(fun) do
    case fun.() do
      {:ok, _value} = result -> result
      {:error, _reason} = result -> result
      _result -> {:error, :invalid_provider_response}
    end
  rescue
    _error -> {:error, :invalid_provider_response}
  end

  defp provider_module("github") do
    configured_provider(:github, SymmetryControl.Integrations.Providers.GitHub)
  end

  defp provider_module("azure_devops") do
    configured_provider(:azure_devops, SymmetryControl.Integrations.Providers.AzureDevOps)
  end

  defp configured_provider(key, default) do
    :symmetry_control
    |> Application.get_env(:integration_providers, [])
    |> Keyword.get(key, default)
  end

  defp require_capability(connection, "repository"),
    do: require_capabilities(connection, ["repositories"])

  defp require_capability(connection, "work_tracking"),
    do: require_capabilities(connection, ["work_items"])

  defp require_capability(connection, "ci"), do: require_capabilities(connection, ["ci"])
  defp require_capability(_connection, _kind), do: {:error, :unsupported_resource}

  defp require_capabilities(connection, required) do
    if Enum.all?(required, &(&1 in connection.capabilities)),
      do: :ok,
      else: {:error, :capability_not_granted}
  end

  defp require_active_project(%Project{status: "active"}), do: :ok
  defp require_active_project(%Project{}), do: {:error, :state_conflict}
  defp require_active_project(nil), do: {:error, :not_found}

  defp connection_identity_editable(connection, attrs) do
    changed? =
      case attr_value(attrs, :account_ref) do
        value when is_binary(value) -> String.trim(value) != connection.account_ref
        _value -> false
      end

    if changed?, do: {:error, :state_conflict}, else: :ok
  end

  defp clear_removed_delivery_authority(previous, updated) do
    removed =
      MapSet.difference(MapSet.new(previous.capabilities), MapSet.new(updated.capabilities))

    repository_ids = connection_resource_ids(updated.id, "repository")
    ci_ids = connection_resource_ids(updated.id, "ci")

    if MapSet.member?(removed, "changes") and repository_ids != [] do
      from(item in WorkItem, where: item.repository_resource_id in ^repository_ids)
      |> Repo.update_all(
        set: [
          external_pull_request_url: nil,
          external_pull_request_state: nil,
          external_review_status: nil,
          external_change_updated_at: nil,
          external_change_data: %{}
        ],
        inc: [lock_version: 1]
      )
    end

    if (MapSet.member?(removed, "changes") or MapSet.member?(removed, "ci")) and
         repository_ids != [] do
      from(item in WorkItem,
        where: item.repository_resource_id in ^repository_ids and is_nil(item.ci_resource_id)
      )
      |> clear_ci_authority()
    end

    if MapSet.member?(removed, "ci") do
      if ci_ids != [] do
        from(item in WorkItem, where: item.ci_resource_id in ^ci_ids)
        |> clear_ci_authority()
      end
    end
  end

  defp connection_resource_ids(connection_id, kind) do
    Repo.all(
      from resource in ProjectResource,
        where: resource.connection_id == ^connection_id and resource.kind == ^kind,
        select: resource.id
    )
  end

  defp clear_ci_authority(query) do
    Repo.update_all(query,
      set: [external_ci_status: nil, external_ci_updated_at: nil, external_ci_data: %{}],
      inc: [lock_version: 1]
    )
  end

  defp reject_secret_attrs(attrs) do
    if secret_data?(attrs),
      do: {:error, :invalid_request},
      else: :ok
  end

  defp auth_type_for("github"), do: {:ok, "gh_cli"}
  defp auth_type_for("azure_devops"), do: {:ok, "entra_id"}
  defp auth_type_for(_provider), do: {:error, :invalid_request}

  defp fetch(schema, id) when is_binary(id) do
    with {:ok, id} <- Ecto.UUID.cast(id), value when not is_nil(value) <- Repo.get(schema, id) do
      {:ok, value}
    else
      _ -> {:error, :not_found}
    end
  end

  defp fetch(_schema, _id), do: {:error, :not_found}

  defp expected_version(attrs) do
    case attr_value(attrs, :version) do
      version when is_integer(version) and version > 0 -> {:ok, version}
      _ -> {:error, :invalid_request}
    end
  end

  defp matches_version(%{lock_version: version}, version), do: :ok
  defp matches_version(_, _), do: {:error, :stale}

  defp update_with_stale_error(changeset) do
    Repo.update(changeset, stale_error_field: :lock_version, stale_error_message: "is stale")
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

  defp unwrap_transaction({:ok, value}), do: {:ok, value}
  defp unwrap_transaction({:error, reason}), do: {:error, reason}

  defp attr_value(attrs, key), do: Map.get(attrs, key) || Map.get(attrs, Atom.to_string(key))

  defp put_attr(attrs, key, value) do
    if Enum.any?(Map.keys(attrs), &is_binary/1),
      do: Map.put(attrs, Atom.to_string(key), value),
      else: Map.put(attrs, key, value)
  end

  defp drop_attr(attrs, key), do: Map.drop(attrs, [key, Atom.to_string(key)])

  defp drop_attrs(attrs, keys) do
    Enum.reduce(keys, attrs, fn key, acc -> drop_attr(acc, key) end)
  end

  defp now, do: DateTime.utc_now() |> DateTime.truncate(:microsecond)
end
