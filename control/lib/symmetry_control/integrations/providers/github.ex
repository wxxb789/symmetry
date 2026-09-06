defmodule SymmetryControl.Integrations.Providers.GitHub do
  @moduledoc false
  @behaviour SymmetryControl.Integrations.Provider

  alias SymmetryControl.Integrations.ChangeAction

  @api "https://api.github.com"
  @owner_pattern ~r/\A[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?\z/
  @repository_pattern ~r/\A[A-Za-z0-9._-]+\z/

  @impl true
  def authenticate(connection), do: auth().github_headers(connection)

  @impl true
  def validate_resource_reference(connection, kind, reference)
      when kind in ["repository", "work_tracking", "ci"] do
    with {:ok, {owner, _repository}} <- repository_parts(reference),
         :ok <- require_account(connection, owner) do
      :ok
    end
  end

  def validate_resource_reference(_connection, _kind, _reference),
    do: {:error, :invalid_request}

  def check(connection) do
    with {:ok, headers} <- authenticate(connection), do: check(connection, headers)
  end

  @impl true
  def check(connection, headers) do
    with {:ok, actor} <- get("/user", headers),
         {:ok, account} <- account(connection.account_ref, actor, headers) do
      {:ok,
       %{
         "actor" => actor["login"],
         "actor_url" => actor["html_url"],
         "account" => account["login"],
         "account_url" => account["html_url"],
         "account_type" => account["type"] || "Organization"
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
         %{kind: "work_tracking", external_ref: repository},
         headers
       ) do
    with {:ok, {owner, name}} <- repository_parts(repository),
         :ok <- require_account(connection, owner),
         {:ok, issues} <- issues(owner, name, headers) do
      {:ok,
       %{
         resource: %{
           url: "https://github.com/#{owner}/#{name}/issues",
           metadata: %{"owner" => owner, "repository" => name}
         },
         work_items: issues
       }}
    end
  end

  defp sync_resource_with_headers(
         connection,
         %{kind: "repository", external_ref: repository},
         headers
       ) do
    with {:ok, {owner, name}} <- repository_parts(repository),
         :ok <- require_account(connection, owner),
         {:ok, body} <- get("/repos/#{segment(owner)}/#{segment(name)}", headers) do
      {:ok,
       %{
         resource: %{
           url: body["html_url"] || "https://github.com/#{owner}/#{name}",
           metadata: %{
             "owner" => owner,
             "repository" => name,
             "default_branch" => body["default_branch"],
             "visibility" => body["visibility"],
             "archived" => body["archived"] || false
           }
         },
         work_items: []
       }}
    end
  end

  defp sync_resource_with_headers(connection, %{kind: "ci", external_ref: repository}, headers) do
    with {:ok, {owner, name}} <- repository_parts(repository),
         :ok <- require_account(connection, owner),
         {:ok, body} <-
           get(
             "/repos/#{segment(owner)}/#{segment(name)}/actions/runs?per_page=1",
             headers
           ) do
      runs = body["workflow_runs"] || []
      latest = List.first(runs)

      {:ok,
       %{
         resource: %{
           url: "https://github.com/#{owner}/#{name}/actions",
           metadata: %{
             "owner" => owner,
             "repository" => name,
             "latest" => workflow_run_metadata(latest)
           }
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
         {:ok, {owner, name}} <- repository_parts(resource.external_ref),
         :ok <- require_account(connection, owner),
         :ok <- ChangeAction.validate_keys(operation, input) do
      execute_change(connection, owner, name, work_item, operation, input, headers)
    end
  end

  def execute(_connection, _resource, _work_item, _operation, _input, _headers),
    do: {:error, :invalid_request}

  defp execute_change(_connection, owner, name, _work_item, "change.upsert", input, headers) do
    with {:ok, source} <- ChangeAction.branch(input, "source_branch"),
         {:ok, target} <- ChangeAction.branch(input, "target_branch"),
         {:ok, title} <- ChangeAction.title(input),
         {:ok, body} <- ChangeAction.body(input),
         {:ok, pull_request} <- find_pull_request(owner, name, source, target, headers),
         {:ok, pull_request} <-
           maybe_create_pull_request(
             pull_request,
             owner,
             name,
             source,
             target,
             title,
             body,
             headers
           ) do
      delivery(pull_request, owner, name)
    end
  end

  defp execute_change(_connection, owner, name, work_item, "change.update", input, headers) do
    with {:ok, title} <- ChangeAction.title(input),
         {:ok, body} <- ChangeAction.body(input),
         {:ok, number} <- action_pull_request_number(input, work_item, owner, name),
         {:ok, pull_request} <-
           request(
             :patch,
             "/repos/#{segment(owner)}/#{segment(name)}/pulls/#{number}",
             headers,
             ChangeAction.put_body(%{"title" => title}, "body", body)
           ) do
      delivery(pull_request, owner, name)
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
    with {:ok, {owner, name}} <- repository_parts(resource.external_ref),
         :ok <- require_account(connection, owner),
         {:ok, number} <- pull_request_number(work_item.pull_request_url, owner, name),
         {:ok, pull_request} <-
           get("/repos/#{segment(owner)}/#{segment(name)}/pulls/#{number}", headers),
         {:ok, reviews} <- reviews(owner, name, number, headers),
         head_sha when is_binary(head_sha) <- get_in(pull_request, ["head", "sha"]),
         {:ok, ci_status} <- ci_status(connection, owner, name, head_sha, headers) do
      delivery = %{
        pull_request_url: pull_request["html_url"] || work_item.pull_request_url,
        pull_request_state: pull_request_state(pull_request),
        review_status: review_status(reviews, pull_request),
        updated_at: now(),
        provider_data: %{
          "head_sha" => head_sha,
          "pull_request_state" => pull_request["state"]
        }
      }

      delivery =
        if ci_status do
          delivery
          |> Map.put(:ci_status, ci_status.status)
          |> put_in([:provider_data, "ci_url"], ci_status.url)
        else
          delivery
        end

      {:ok, delivery}
    else
      nil -> {:error, :missing_head_sha}
      {:error, reason} -> {:error, reason}
    end
  end

  @impl true
  def sync_ci(connection, resource, work_item, headers) do
    with {:ok, {owner, name}} <- repository_parts(resource.external_ref),
         :ok <- require_account(connection, owner),
         {:ok, reference} <- ci_reference(work_item),
         {:ok, actions} <-
           get(
             "/repos/#{segment(owner)}/#{segment(name)}/actions/runs?" <>
               URI.encode_query(Map.put(reference, "per_page", 100)),
             headers
           ) do
      runs = actions["workflow_runs"] || []

      {:ok,
       %{
         ci_status: actions_status(runs),
         updated_at: now(),
         provider_data: %{"ci_url" => runs |> List.first() |> link("html_url")}
       }}
    end
  end

  defp ci_status(connection, owner, name, head_sha, headers) do
    if "ci" in connection.capabilities do
      with {:ok, actions} <-
             get(
               "/repos/#{segment(owner)}/#{segment(name)}/actions/runs?" <>
                 URI.encode_query(%{"head_sha" => head_sha, "per_page" => 100}),
               headers
             ) do
        {:ok,
         %{
           status: actions_status(actions["workflow_runs"] || []),
           url: actions |> Map.get("workflow_runs", []) |> List.first() |> link("html_url")
         }}
      end
    else
      {:ok, nil}
    end
  end

  defp ci_reference(work_item) do
    data = work_item.external_change_data || %{}

    cond do
      is_binary(data["head_sha"]) and data["head_sha"] != "" ->
        {:ok, %{"head_sha" => data["head_sha"]}}

      is_binary(work_item.branch) and work_item.branch != "" ->
        {:ok, %{"branch" => work_item.branch}}

      true ->
        {:error, :missing_ci_reference}
    end
  end

  defp issues(owner, name, headers), do: issue_page(owner, name, headers, 1, [])

  defp reviews(owner, name, number, headers),
    do: review_page(owner, name, number, headers, 1, [])

  defp account(account_ref, actor, headers) do
    if is_binary(actor["login"]) and
         String.downcase(actor["login"]) == String.downcase(account_ref) do
      {:ok, actor}
    else
      with {:ok, membership} <-
             get("/user/memberships/orgs/#{segment(account_ref)}", headers),
           "active" <- membership["state"],
           %{} = organization <- membership["organization"] do
        {:ok, Map.put_new(organization, "type", "Organization")}
      else
        {:error, reason} -> {:error, reason}
        _membership -> {:error, :forbidden}
      end
    end
  end

  defp issue_page(owner, name, headers, page, acc) do
    query =
      URI.encode_query(%{
        "state" => "all",
        "sort" => "updated",
        "direction" => "desc",
        "per_page" => 100,
        "page" => page
      })

    with {:ok, entries} <-
           get("/repos/#{segment(owner)}/#{segment(name)}/issues?#{query}", headers) do
      issues = entries |> Enum.reject(&Map.has_key?(&1, "pull_request")) |> Enum.map(&issue/1)
      acc = Enum.reverse(issues, acc)

      if length(entries) < 100,
        do: {:ok, Enum.reverse(acc)},
        else: issue_page(owner, name, headers, page + 1, acc)
    end
  end

  defp review_page(owner, name, number, headers, page, acc) do
    query = URI.encode_query(%{"per_page" => 100, "page" => page})

    with {:ok, entries} <-
           get(
             "/repos/#{segment(owner)}/#{segment(name)}/pulls/#{number}/reviews?#{query}",
             headers
           ) do
      acc = Enum.reverse(entries, acc)

      if length(entries) < 100,
        do: {:ok, Enum.reverse(acc)},
        else: review_page(owner, name, number, headers, page + 1, acc)
    end
  end

  defp issue(issue) do
    labels =
      Enum.map(issue["labels"] || [], fn
        %{"name" => name} -> name
        name when is_binary(name) -> name
      end)

    %{
      external_id: to_string(issue["number"]),
      external_url: issue["html_url"],
      external_state: issue["state"],
      external_updated_at: required_datetime(issue["updated_at"]),
      external_assignee_name: get_in(issue, ["assignee", "login"]),
      labels: labels,
      title: issue["title"],
      description: issue["body"],
      status: if(issue["state"] == "closed", do: "done", else: "ready"),
      priority: priority(labels),
      provider_data: %{"number" => issue["number"]}
    }
  end

  defp pull_request_state(%{"merged_at" => merged_at}) when is_binary(merged_at), do: "merged"
  defp pull_request_state(pull_request), do: pull_request["state"] || "unknown"

  defp review_status(reviews, pull_request) do
    latest =
      Enum.reduce(reviews, %{}, fn review, acc ->
        case {get_in(review, ["user", "login"]), review["state"]} do
          {login, state} when is_binary(login) and state in ["APPROVED", "CHANGES_REQUESTED"] ->
            Map.put(acc, login, state)

          {login, "DISMISSED"} when is_binary(login) ->
            Map.delete(acc, login)

          _non_decision ->
            acc
        end
      end)
      |> Map.values()

    cond do
      pull_request["merged_at"] -> "approved"
      Enum.any?(latest, &(&1 == "CHANGES_REQUESTED")) -> "changes_requested"
      Enum.any?(latest, &(&1 == "APPROVED")) -> "approved"
      true -> "required"
    end
  end

  defp actions_status([]), do: "unknown"

  defp actions_status(runs) do
    runs = latest_by(runs, &(&1["workflow_id"] || &1["name"]))

    cond do
      Enum.any?(runs, &(&1["status"] != "completed")) ->
        "pending"

      Enum.any?(
        runs,
        &(&1["conclusion"] in [
            "failure",
            "cancelled",
            "timed_out",
            "action_required",
            "startup_failure"
          ])
      ) ->
        "failed"

      Enum.all?(runs, &(&1["conclusion"] in ["success", "neutral", "skipped"])) ->
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

  defp workflow_run_metadata(nil), do: nil

  defp workflow_run_metadata(run) do
    %{
      "id" => run["id"],
      "status" => run["status"],
      "conclusion" => run["conclusion"],
      "url" => run["html_url"],
      "head_sha" => run["head_sha"]
    }
  end

  defp pull_request_number(url, owner, name) when is_binary(url) do
    uri = URI.parse(url)

    with "https" <- String.downcase(uri.scheme || ""),
         "github.com" <- String.downcase(uri.host || ""),
         nil <- uri.userinfo,
         true <- uri.port in [nil, 443],
         {:ok, [url_owner, url_name, "pull", id]} <- decoded_path_segments(uri.path),
         true <- same_reference?(url_owner, owner),
         true <- same_reference?(url_name, name),
         {number, ""} when number > 0 <- Integer.parse(id) do
      {:ok, number}
    else
      _invalid -> {:error, :invalid_pull_request_url}
    end
  end

  defp pull_request_number(_url, _owner, _name), do: {:error, :invalid_pull_request_url}

  defp find_pull_request(owner, name, source, target, headers) do
    query =
      URI.encode_query(%{
        "state" => "open",
        "head" => "#{owner}:#{source}",
        "base" => target,
        "per_page" => 1
      })

    case get("/repos/#{segment(owner)}/#{segment(name)}/pulls?#{query}", headers) do
      {:ok, pull_requests} when is_list(pull_requests) -> {:ok, List.first(pull_requests)}
      {:ok, _invalid} -> {:error, :invalid_provider_response}
      {:error, reason} -> {:error, reason}
    end
  end

  defp maybe_create_pull_request(nil, owner, name, source, target, title, body, headers) do
    case request(
           :post,
           "/repos/#{segment(owner)}/#{segment(name)}/pulls",
           headers,
           ChangeAction.put_body(
             %{"head" => source, "base" => target, "title" => title},
             "body",
             body
           )
         ) do
      {:error, {:http, 422, _response} = create_error} ->
        replay_after_create_conflict(
          owner,
          name,
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
         owner,
         name,
         _source,
         _target,
         title,
         body,
         headers
       )
       when is_map(pull_request) do
    with {:ok, number} <- pull_request_response_number(pull_request, owner, name) do
      request(
        :patch,
        "/repos/#{segment(owner)}/#{segment(name)}/pulls/#{number}",
        headers,
        ChangeAction.put_body(%{"title" => title}, "body", body)
      )
    end
  end

  defp replay_after_create_conflict(
         owner,
         name,
         source,
         target,
         title,
         body,
         headers,
         create_error
       ) do
    case find_pull_request(owner, name, source, target, headers) do
      {:ok, pull_request} when is_map(pull_request) ->
        maybe_create_pull_request(
          pull_request,
          owner,
          name,
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

  defp delivery(pull_request, owner, name) when is_map(pull_request) do
    with {:ok, number} <- pull_request_response_number(pull_request, owner, name) do
      url = pull_request["html_url"]

      provider_data = %{
        "pull_request_number" => number,
        "pull_request_state" => pull_request["state"]
      }

      provider_data =
        case get_in(pull_request, ["head", "sha"]) do
          head_sha when is_binary(head_sha) and head_sha != "" ->
            Map.put(provider_data, "head_sha", head_sha)

          _missing ->
            provider_data
        end

      {:ok,
       %{
         pull_request_url: url,
         pull_request_state: pull_request_state(pull_request),
         updated_at: now(),
         provider_data: provider_data
       }}
    else
      _invalid -> {:error, :invalid_provider_response}
    end
  end

  defp delivery(_pull_request, _owner, _name), do: {:error, :invalid_provider_response}

  defp pull_request_response_number(pull_request, owner, name) do
    with number when is_integer(number) <- pull_request["number"],
         url when is_binary(url) <- pull_request["html_url"],
         {:ok, ^number} <- pull_request_number(url, owner, name) do
      {:ok, number}
    else
      _invalid -> {:error, :invalid_provider_response}
    end
  end

  defp action_pull_request_number(input, work_item, owner, name) do
    linked_url = ChangeAction.linked_pull_request_url(work_item)

    requested_url =
      case ChangeAction.value(input, "pull_request_url") do
        :missing -> linked_url
        supplied -> supplied
      end

    with {:ok, linked_number} <- pull_request_number(linked_url, owner, name),
         {:ok, ^linked_number} <- pull_request_number(requested_url, owner, name) do
      {:ok, linked_number}
    else
      _invalid -> {:error, :invalid_pull_request_url}
    end
  end

  defp decoded_path_segments(path) when is_binary(path) do
    {:ok, path |> String.split("/", trim: true) |> Enum.map(&URI.decode/1)}
  rescue
    ArgumentError -> {:error, :invalid_pull_request_url}
  end

  defp decoded_path_segments(_path), do: {:error, :invalid_pull_request_url}

  defp same_reference?(left, right),
    do: String.downcase(left) == String.downcase(right)

  defp repository_parts(value) when is_binary(value) do
    case String.split(value, "/") do
      [owner, name] ->
        if valid_owner?(owner) and valid_repository?(name),
          do: {:ok, {owner, name}},
          else: {:error, :invalid_repository_reference}

      _ ->
        {:error, :invalid_repository_reference}
    end
  end

  defp repository_parts(_value), do: {:error, :invalid_repository_reference}

  defp valid_owner?(owner) do
    byte_size(owner) in 1..39 and Regex.match?(@owner_pattern, owner) and
      not String.contains?(owner, "--")
  end

  defp valid_repository?(name) do
    byte_size(name) in 1..100 and name not in [".", ".."] and
      Regex.match?(@repository_pattern, name)
  end

  defp require_account(connection, owner) do
    if String.downcase(connection.account_ref) == String.downcase(owner),
      do: :ok,
      else: {:error, :forbidden}
  end

  defp get(path, headers) do
    request(:get, path, headers)
  end

  defp request(method, path, headers, body \\ nil) do
    case http().request(method, @api <> path, headers, body) do
      {:ok, status, _headers, body} when status in 200..299 -> {:ok, body}
      {:ok, status, _headers, body} -> {:error, {:http, status, body}}
      {:error, reason} -> {:error, reason}
    end
  end

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

  defp segment(value), do: URI.encode(value, &URI.char_unreserved?/1)

  defp required_datetime(value) when is_binary(value) do
    case DateTime.from_iso8601(value) do
      {:ok, parsed, _offset} -> DateTime.truncate(parsed, :microsecond)
      _ -> nil
    end
  end

  defp required_datetime(_value), do: nil

  defp link(nil, _key), do: nil
  defp link(value, key), do: value[key]
  defp now, do: DateTime.utc_now() |> DateTime.truncate(:microsecond)
end
