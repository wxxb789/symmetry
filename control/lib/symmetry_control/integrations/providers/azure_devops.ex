defmodule SymmetryControl.Integrations.Providers.AzureDevOps do
  @moduledoc false
  @behaviour SymmetryControl.Integrations.Provider

  alias SymmetryControl.Integrations.ChangeAction

  @api_version "7.1"
  @max_build_pages 10
  @work_item_fields [
    "System.Id",
    "System.Title",
    "System.Description",
    "System.State",
    "System.Tags",
    "System.AssignedTo",
    "System.ChangedDate",
    "System.WorkItemType"
  ]

  @impl true
  def authenticate(connection), do: auth().azure_devops_headers(connection)

  @impl true
  def validate_resource_reference(_connection, "work_tracking", reference)
      when is_binary(reference) and reference != "",
      do: :ok

  def validate_resource_reference(_connection, "repository", reference) do
    case repository_parts(reference) do
      {:ok, _parts} -> :ok
      {:error, _reason} -> {:error, :invalid_request}
    end
  end

  def validate_resource_reference(_connection, "ci", reference) do
    case ci_reference(reference) do
      {:ok, _parts} -> :ok
      {:error, _reason} -> {:error, :invalid_request}
    end
  end

  def validate_resource_reference(_connection, _kind, _reference),
    do: {:error, :invalid_request}

  def check(connection) do
    with {:ok, headers} <- authenticate(connection), do: check(connection, headers)
  end

  @impl true
  def check(connection, headers) do
    path = "/#{segment(connection.account_ref)}/_apis/projects?" <> query(%{"$top" => 1})

    with {:ok, body} <- request(:get, path, headers) do
      {:ok,
       %{
         "organization" => connection.account_ref,
         "project_count" => body["count"] || length(body["value"] || [])
       }}
    end
  end

  def sync_resource(connection, resource) do
    with {:ok, headers} <- authenticate(connection),
         do: sync_resource(connection, resource, headers)
  end

  @impl true
  def sync_resource(connection, resource, headers),
    do: sync_resource_with_headers(connection, resource, headers)

  defp sync_resource_with_headers(
         connection,
         %{kind: "work_tracking", external_ref: project},
         headers
       ) do
    with {:ok, references} <- work_item_references(connection, project, headers),
         {:ok, items} <- work_items(connection, project, references, headers) do
      {:ok,
       %{
         resource: %{
           url: "https://dev.azure.com/#{connection.account_ref}/#{project}/_boards",
           metadata: %{"project" => project}
         },
         work_items: Enum.map(items, &work_item/1)
       }}
    end
  end

  defp sync_resource_with_headers(
         connection,
         %{kind: "repository", external_ref: reference},
         headers
       ) do
    with {:ok, {project, repository}} <- repository_parts(reference),
         {:ok, body} <- repository(connection, project, repository, headers) do
      {:ok,
       %{
         resource: %{
           url:
             body["webUrl"] ||
               "https://dev.azure.com/#{connection.account_ref}/#{project}/_git/#{repository}",
           metadata: %{
             "project" => project,
             "repository" => repository,
             "repository_id" => body["id"],
             "default_branch" => body["defaultBranch"]
           }
         },
         work_items: []
       }}
    end
  end

  defp sync_resource_with_headers(connection, %{kind: "ci", external_ref: reference}, headers) do
    with {:ok, {project, {:definition, definition_id} = selector}} <- ci_reference(reference),
         {:ok, definition} <- build_definition(connection, project, definition_id, headers),
         {:ok, builds} <- builds(connection, project, selector, nil, headers) do
      {:ok,
       %{
         resource: %{
           url: ci_url(connection.account_ref, project, definition_id),
           metadata: ci_metadata(project, definition, builds)
         },
         work_items: []
       }}
    end
  end

  defp sync_resource_with_headers(_connection, _resource, _headers),
    do: {:error, :unsupported_resource}

  @impl true
  def execute(connection, %{kind: "repository"} = resource, work_item, operation, input, headers)
      when operation in ["change.upsert", "change.update"] and is_map(work_item) and
             is_map(input) do
    with true <- "changes" in connection.capabilities || {:error, :capability_not_granted},
         {:ok, {project, repository}} <- repository_parts(resource.external_ref),
         :ok <- ChangeAction.validate_keys(operation, input) do
      execute_change(connection, project, repository, work_item, operation, input, headers)
    end
  end

  def execute(_connection, _resource, _work_item, _operation, _input, _headers),
    do: {:error, :invalid_request}

  defp execute_change(
         connection,
         project,
         repository,
         _work_item,
         "change.upsert",
         input,
         headers
       ) do
    with {:ok, source} <- ChangeAction.branch(input, "source_branch"),
         {:ok, target} <- ChangeAction.branch(input, "target_branch"),
         {:ok, title} <- ChangeAction.title(input),
         {:ok, body} <- ChangeAction.body(input),
         {:ok, pull_request} <-
           find_pull_request(connection, project, repository, source, target, headers),
         {:ok, pull_request} <-
           maybe_create_pull_request(
             pull_request,
             connection,
             project,
             repository,
             source,
             target,
             title,
             body,
             headers
           ) do
      delivery(pull_request, connection.account_ref, project, repository)
    end
  end

  defp execute_change(
         connection,
         project,
         repository,
         work_item,
         "change.update",
         input,
         headers
       ) do
    with {:ok, title} <- ChangeAction.title(input),
         {:ok, body} <- ChangeAction.body(input),
         {:ok, pull_request_id} <-
           action_pull_request_id(input, work_item, connection.account_ref, project, repository),
         {:ok, pull_request} <-
           request(
             :patch,
             pull_request_path(connection, project, repository, pull_request_id),
             headers,
             ChangeAction.put_body(%{"title" => title}, "description", body)
           ) do
      delivery(pull_request, connection.account_ref, project, repository)
    end
  end

  def sync_delivery(_connection, _resource, %{pull_request_url: nil}),
    do: {:ok, nil}

  def sync_delivery(connection, resource, work_item) do
    with {:ok, headers} <- authenticate(connection),
         do: sync_delivery(connection, resource, work_item, headers)
  end

  @impl true
  def sync_delivery(_connection, _resource, %{pull_request_url: nil}, _headers),
    do: {:ok, nil}

  def sync_delivery(connection, resource, work_item, headers) do
    with {:ok, {project, repository}} <- repository_parts(resource.external_ref),
         {:ok, pull_request_id} <-
           pull_request_id(
             work_item.pull_request_url,
             connection.account_ref,
             project,
             repository
           ),
         {:ok, pull_request} <-
           request(
             :get,
             "/#{segment(connection.account_ref)}/#{segment(project)}/_apis/git/repositories/" <>
               "#{segment(repository)}/pullrequests/#{pull_request_id}?" <> query(%{}),
             headers
           ),
         source_commit_id when is_binary(source_commit_id) <-
           get_in(pull_request, ["lastMergeSourceCommit", "commitId"]),
         merge_commit_id = get_in(pull_request, ["lastMergeCommit", "commitId"]),
         commit_ids =
           [source_commit_id, merge_commit_id]
           |> Enum.filter(&(is_binary(&1) and &1 != ""))
           |> Enum.uniq(),
         repository_id =
           resource |> Map.get(:metadata, %{}) |> Map.get("repository_id") || repository,
         {:ok, ci} <-
           delivery_builds(connection, project, repository_id, commit_ids, headers) do
      provider_data = %{
        "commit_id" => source_commit_id,
        "pull_request_state" => pull_request["status"]
      }

      provider_data =
        if is_binary(merge_commit_id),
          do: Map.put(provider_data, "merge_commit_id", merge_commit_id),
          else: provider_data

      delivery = %{
        pull_request_url: work_item.pull_request_url,
        pull_request_state: pull_request_state(pull_request["status"]),
        review_status: review_status(pull_request["reviewers"] || []),
        updated_at: now(),
        provider_data: provider_data
      }

      delivery =
        if ci do
          delivery
          |> Map.put(:ci_status, ci.status)
          |> put_in([:provider_data, "ci_url"], ci.url)
        else
          delivery
        end

      {:ok, delivery}
    else
      nil -> {:error, :missing_commit_id}
      {:error, reason} -> {:error, reason}
    end
  end

  @impl true
  def sync_ci(connection, resource, work_item, headers) do
    with {:ok, {project, selector}} <- ci_reference(resource.external_ref),
         {:ok, commit_ids} <- ci_commit_ids(work_item),
         {:ok, builds} <- builds(connection, project, selector, commit_ids, headers) do
      {:ok,
       %{
         ci_status: build_status(builds),
         updated_at: now(),
         provider_data: %{"ci_url" => builds |> List.first() |> web_link()}
       }}
    end
  end

  defp ci_commit_ids(work_item) do
    data = work_item.external_change_data || %{}

    case [data["head_sha"], data["commit_id"], data["merge_commit_id"]]
         |> Enum.filter(&(is_binary(&1) and &1 != ""))
         |> Enum.uniq() do
      [] -> {:error, :missing_ci_reference}
      values -> {:ok, values}
    end
  end

  defp delivery_builds(connection, project, repository_id, commit_ids, headers) do
    if "ci" in connection.capabilities do
      with {:ok, builds} <-
             builds(connection, project, {:repository, repository_id}, commit_ids, headers) do
        {:ok, %{status: build_status(builds), url: builds |> List.first() |> web_link()}}
      end
    else
      {:ok, nil}
    end
  end

  defp work_item_references(connection, project, headers) do
    path =
      "/#{segment(connection.account_ref)}/#{segment(project)}/_apis/wit/wiql?" <>
        query(%{})

    body = %{
      "query" =>
        "SELECT [System.Id] FROM WorkItems " <>
          "WHERE [System.TeamProject] = @project ORDER BY [System.ChangedDate] DESC"
    }

    with {:ok, response} <- request(:post, path, headers, body) do
      {:ok, Enum.map(response["workItems"] || [], & &1["id"])}
    end
  end

  defp work_items(_connection, _project, [], _headers), do: {:ok, []}

  defp work_items(connection, project, ids, headers) do
    result =
      ids
      |> Enum.chunk_every(200)
      |> Enum.reduce_while({:ok, []}, fn chunk, {:ok, acc} ->
        path =
          "/#{segment(connection.account_ref)}/#{segment(project)}/_apis/wit/workitemsbatch?" <>
            query(%{})

        body = %{"ids" => chunk, "fields" => @work_item_fields, "$expand" => "Relations"}

        case request(:post, path, headers, body) do
          {:ok, response} -> {:cont, {:ok, Enum.reverse(response["value"] || [], acc)}}
          {:error, reason} -> {:halt, {:error, reason}}
        end
      end)

    case result do
      {:ok, items} -> {:ok, Enum.reverse(items)}
      {:error, reason} -> {:error, reason}
    end
  end

  defp work_item(item) do
    fields = item["fields"] || %{}
    labels = labels(fields["System.Tags"])

    %{
      external_id: to_string(item["id"]),
      external_url: get_in(item, ["_links", "html", "href"]) || item["url"],
      external_state: fields["System.State"] || "Unknown",
      external_updated_at: required_datetime(fields["System.ChangedDate"]),
      external_assignee_name: assigned_to(fields["System.AssignedTo"]),
      labels: labels,
      title: fields["System.Title"],
      description: fields["System.Description"],
      status: azure_status(fields["System.State"]),
      priority: priority(labels),
      provider_data: %{
        "work_item_type" => fields["System.WorkItemType"],
        "revision" => item["rev"]
      }
    }
  end

  defp repository(connection, project, repository, headers) do
    request(
      :get,
      "/#{segment(connection.account_ref)}/#{segment(project)}/_apis/git/repositories/" <>
        "#{segment(repository)}?" <> query(%{}),
      headers
    )
  end

  defp find_pull_request(connection, project, repository, source, target, headers) do
    params = %{
      "searchCriteria.status" => "active",
      "searchCriteria.sourceRefName" => ref_name(source),
      "searchCriteria.targetRefName" => ref_name(target),
      "$top" => 1
    }

    path =
      "/#{segment(connection.account_ref)}/#{segment(project)}/_apis/git/repositories/" <>
        "#{segment(repository)}/pullrequests?" <> query(params)

    case request(:get, path, headers) do
      {:ok, %{"value" => pull_requests}} when is_list(pull_requests) ->
        {:ok, List.first(pull_requests)}

      {:ok, _invalid} ->
        {:error, :invalid_provider_response}

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp maybe_create_pull_request(
         nil,
         connection,
         project,
         repository,
         source,
         target,
         title,
         body,
         headers
       ) do
    case request(
           :post,
           pull_requests_path(connection, project, repository),
           headers,
           ChangeAction.put_body(
             %{
               "sourceRefName" => ref_name(source),
               "targetRefName" => ref_name(target),
               "title" => title
             },
             "description",
             body
           )
         ) do
      {:error, {:http, status, _response} = create_error} when status in [400, 409] ->
        replay_after_create_conflict(
          connection,
          project,
          repository,
          source,
          target,
          title,
          body,
          headers,
          create_error
        )

      result ->
        result
    end
  end

  defp maybe_create_pull_request(
         pull_request,
         connection,
         project,
         repository,
         _source,
         _target,
         title,
         body,
         headers
       )
       when is_map(pull_request) do
    with {:ok, {pull_request_id, _status, _url}} <-
           pull_request_details(pull_request, connection.account_ref, project, repository) do
      request(
        :patch,
        pull_request_path(connection, project, repository, pull_request_id),
        headers,
        ChangeAction.put_body(%{"title" => title}, "description", body)
      )
    end
  end

  defp replay_after_create_conflict(
         connection,
         project,
         repository,
         source,
         target,
         title,
         body,
         headers,
         create_error
       ) do
    case find_pull_request(connection, project, repository, source, target, headers) do
      {:ok, pull_request} when is_map(pull_request) ->
        maybe_create_pull_request(
          pull_request,
          connection,
          project,
          repository,
          source,
          target,
          title,
          body,
          headers
        )

      _not_found_or_failed ->
        {:error, create_error}
    end
  end

  defp delivery(pull_request, organization, project, repository) when is_map(pull_request) do
    with {:ok, {id, status, url}} <-
           pull_request_details(pull_request, organization, project, repository) do
      provider_data = %{
        "pull_request_id" => id,
        "pull_request_state" => status
      }

      provider_data =
        case get_in(pull_request, ["lastMergeSourceCommit", "commitId"]) do
          commit_id when is_binary(commit_id) and commit_id != "" ->
            Map.put(provider_data, "commit_id", commit_id)

          _missing ->
            provider_data
        end

      {:ok,
       %{
         pull_request_url: url,
         pull_request_state: pull_request_state(status),
         updated_at: now(),
         provider_data: provider_data
       }}
    else
      _invalid -> {:error, :invalid_provider_response}
    end
  end

  defp delivery(_pull_request, _organization, _project, _repository),
    do: {:error, :invalid_provider_response}

  defp pull_request_details(pull_request, organization, project, repository) do
    with id when is_integer(id) <- pull_request["pullRequestId"],
         status when is_binary(status) <- pull_request["status"],
         url <-
           get_in(pull_request, ["_links", "web", "href"]) ||
             "https://dev.azure.com/#{organization}/#{segment(project)}/_git/#{segment(repository)}/pullrequest/#{id}",
         {:ok, ^id} <- pull_request_id(url, organization, project, repository) do
      {:ok, {id, status, url}}
    else
      _invalid -> {:error, :invalid_provider_response}
    end
  end

  defp pull_requests_path(connection, project, repository) do
    "/#{segment(connection.account_ref)}/#{segment(project)}/_apis/git/repositories/" <>
      "#{segment(repository)}/pullrequests?" <> query(%{})
  end

  defp pull_request_path(connection, project, repository, pull_request_id) do
    "/#{segment(connection.account_ref)}/#{segment(project)}/_apis/git/repositories/" <>
      "#{segment(repository)}/pullrequests/#{pull_request_id}?" <> query(%{})
  end

  defp ref_name(branch), do: "refs/heads/" <> branch

  defp build_definition(connection, project, definition_id, headers) do
    request(
      :get,
      "/#{segment(connection.account_ref)}/#{segment(project)}/_apis/build/definitions/" <>
        "#{definition_id}?" <> query(%{}),
      headers
    )
  end

  defp builds(connection, project, selector, commit_ids, headers) do
    build_pages(connection, project, selector, commit_ids, headers, nil, [], MapSet.new())
  end

  defp build_pages(
         connection,
         project,
         selector,
         commit_ids,
         headers,
         continuation_token,
         matches,
         seen_tokens
       ) do
    params =
      selector
      |> build_filter()
      |> Map.merge(%{"$top" => 100, "queryOrder" => "queueTimeDescending"})

    params =
      if is_binary(continuation_token),
        do: Map.put(params, "continuationToken", continuation_token),
        else: params

    path =
      "/#{segment(connection.account_ref)}/#{segment(project)}/_apis/build/builds?" <>
        query(params)

    with {:ok, response_headers, body} <- request_with_headers(:get, path, headers),
         builds when is_list(builds) <- body["value"] || [] do
      page_matches =
        if is_list(commit_ids),
          do: Enum.filter(builds, &build_matches_commit?(&1, commit_ids)),
          else: builds

      matches = matches ++ page_matches
      next_token = response_headers["x-ms-continuationtoken"]

      cond do
        is_nil(commit_ids) or not is_binary(next_token) or next_token == "" ->
          {:ok, matches}

        match?({:definition, _definition_id}, selector) and page_matches != [] ->
          {:ok, matches}

        MapSet.size(seen_tokens) + 1 >= @max_build_pages ->
          {:error, :ci_history_limit}

        MapSet.member?(seen_tokens, next_token) ->
          {:error, :invalid_provider_response}

        true ->
          build_pages(
            connection,
            project,
            selector,
            commit_ids,
            headers,
            next_token,
            matches,
            MapSet.put(seen_tokens, next_token)
          )
      end
    else
      {:error, reason} -> {:error, reason}
      _invalid_body -> {:error, :invalid_provider_response}
    end
  end

  defp build_matches_commit?(build, commit_ids) do
    trigger_info = build["triggerInfo"] || %{}

    candidates = [
      build["sourceVersion"],
      trigger_info["pr.sourceSha"],
      trigger_info["pr.sourceCommitId"],
      trigger_info["pr.sourceVersion"],
      trigger_info["sourceSha"],
      trigger_info["sourceVersion"]
    ]

    Enum.any?(candidates, &(&1 in commit_ids))
  end

  defp build_filter({:repository, repository}) do
    %{"repositoryId" => repository, "repositoryType" => "TfsGit"}
  end

  defp build_filter({:definition, definition_id}),
    do: %{"definitions" => Integer.to_string(definition_id)}

  defp ci_metadata(project, definition, builds) do
    %{
      "project" => project,
      "definition_id" => definition["id"],
      "definition_name" => definition["name"],
      "repository_type" => get_in(definition, ["repository", "type"]),
      "latest" => build_metadata(List.first(builds))
    }
  end

  defp ci_url(organization, project, definition_id) do
    "https://dev.azure.com/#{organization}/#{segment(project)}/_build?definitionId=#{definition_id}"
  end

  defp review_status(reviewers) do
    cond do
      Enum.any?(reviewers, &((&1["vote"] || 0) <= -5)) -> "changes_requested"
      Enum.any?(reviewers, &((&1["vote"] || 0) >= 5)) -> "approved"
      true -> "required"
    end
  end

  defp pull_request_state("active"), do: "open"
  defp pull_request_state("completed"), do: "merged"
  defp pull_request_state("abandoned"), do: "closed"
  defp pull_request_state(_state), do: "unknown"

  defp build_status([]), do: "unknown"

  defp build_status(builds) do
    builds =
      latest_by(builds, &(get_in(&1, ["definition", "id"]) || get_in(&1, ["definition", "name"])))

    cond do
      Enum.any?(builds, &(&1["status"] != "completed")) ->
        "pending"

      Enum.any?(builds, &(&1["result"] in ["failed", "canceled", "partiallySucceeded"])) ->
        "failed"

      Enum.all?(builds, &(&1["result"] == "succeeded")) ->
        "passed"

      true ->
        "unknown"
    end
  end

  defp latest_by(entries, key_fun) do
    entries
    |> Enum.with_index()
    |> Enum.uniq_by(fn {entry, index} -> key_fun.(entry) || {:entry, index} end)
    |> Enum.map(&elem(&1, 0))
  end

  defp build_metadata(nil), do: nil

  defp build_metadata(build) do
    %{
      "id" => build["id"],
      "status" => build["status"],
      "result" => build["result"],
      "url" => web_link(build),
      "source_version" => build["sourceVersion"]
    }
  end

  defp azure_status(state) when is_binary(state) do
    if String.downcase(state) in ["closed", "done", "resolved", "removed", "completed"],
      do: "done",
      else: "ready"
  end

  defp azure_status(_state), do: "ready"

  defp labels(nil), do: []

  defp labels(value) when is_binary(value) do
    value
    |> String.split(";")
    |> Enum.map(&String.trim/1)
    |> Enum.reject(&(&1 == ""))
  end

  defp assigned_to(%{"displayName" => name}), do: name
  defp assigned_to(name) when is_binary(name), do: name
  defp assigned_to(_value), do: nil

  defp priority(labels) do
    Enum.find_value(labels, "no_priority", fn label ->
      case String.downcase(label) do
        "priority:urgent" -> "urgent"
        "priority:high" -> "high"
        "priority:medium" -> "medium"
        "priority:low" -> "low"
        _ -> nil
      end
    end)
  end

  defp repository_parts(value) when is_binary(value) do
    case String.split(value, "/", parts: 2) do
      [project, repository] when project != "" and repository != "" ->
        {:ok, {project, repository}}

      _ ->
        {:error, :invalid_repository_reference}
    end
  end

  defp repository_parts(_value), do: {:error, :invalid_repository_reference}

  defp ci_reference(value) when is_binary(value) do
    case String.split(value, "/") do
      [project, "pipeline", definition_id] when project != "" ->
        case Integer.parse(definition_id) do
          {id, ""} when id > 0 -> {:ok, {project, {:definition, id}}}
          _invalid -> {:error, :invalid_repository_reference}
        end

      _invalid_reference ->
        {:error, :invalid_repository_reference}
    end
  end

  defp ci_reference(_value), do: {:error, :invalid_repository_reference}

  defp pull_request_id(url, organization, project, repository) when is_binary(url) do
    uri = URI.parse(url)

    with "https" <- uri.scheme,
         "dev.azure.com" <- String.downcase(uri.host || ""),
         {:ok, [url_organization, url_project, "_git", url_repository, "pullrequest", id]} <-
           decoded_path_segments(uri.path),
         true <- same_reference?(url_organization, organization),
         true <- same_reference?(url_project, project),
         true <- same_reference?(url_repository, repository),
         {pull_request_id, ""} when pull_request_id > 0 <- Integer.parse(id) do
      {:ok, pull_request_id}
    else
      _ -> {:error, :invalid_pull_request_url}
    end
  end

  defp pull_request_id(_url, _organization, _project, _repository),
    do: {:error, :invalid_pull_request_url}

  defp action_pull_request_id(input, work_item, organization, project, repository) do
    linked_url = ChangeAction.linked_pull_request_url(work_item)

    requested_url =
      case ChangeAction.value(input, "pull_request_url") do
        :missing -> linked_url
        supplied -> supplied
      end

    with {:ok, linked_id} <- pull_request_id(linked_url, organization, project, repository),
         {:ok, ^linked_id} <- pull_request_id(requested_url, organization, project, repository) do
      {:ok, linked_id}
    else
      _invalid -> {:error, :invalid_pull_request_url}
    end
  end

  defp decoded_path_segments(path) when is_binary(path) do
    {:ok,
     path
     |> String.split("/", trim: true)
     |> Enum.map(&URI.decode/1)}
  rescue
    ArgumentError -> {:error, :invalid_pull_request_url}
  end

  defp decoded_path_segments(_path), do: {:error, :invalid_pull_request_url}

  defp same_reference?(left, right),
    do: String.downcase(left) == String.downcase(right)

  defp request(method, path, headers, body \\ nil) do
    with {:ok, _response_headers, response} <- request_with_headers(method, path, headers, body) do
      {:ok, response}
    end
  end

  defp request_with_headers(method, path, headers, body \\ nil) do
    url = "https://dev.azure.com" <> path

    case http().request(method, url, headers, body) do
      {:ok, status, response_headers, response} when status in 200..299 ->
        {:ok, response_headers, response}

      {:ok, status, _headers, response} ->
        {:error, {:http, status, response}}

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp query(params), do: params |> Map.put("api-version", @api_version) |> URI.encode_query()

  defp segment(value), do: URI.encode(value, &URI.char_unreserved?/1)

  defp required_datetime(value) when is_binary(value) do
    case DateTime.from_iso8601(value) do
      {:ok, parsed, _offset} -> DateTime.truncate(parsed, :microsecond)
      _ -> nil
    end
  end

  defp required_datetime(_value), do: nil

  defp web_link(nil), do: nil
  defp web_link(build), do: get_in(build, ["_links", "web", "href"])

  defp http,
    do:
      Application.get_env(
        :symmetry_control,
        :integration_http_client,
        SymmetryControl.Integrations.HTTP
      )

  defp auth,
    do:
      Application.get_env(
        :symmetry_control,
        :integration_auth_provider,
        SymmetryControl.Integrations.Auth
      )

  defp now, do: DateTime.utc_now() |> DateTime.truncate(:microsecond)
end
