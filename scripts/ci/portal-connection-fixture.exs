# Fixture launcher for the connection-management browser acceptance run.
#
#   PHX_SERVER=true PORT=4021 mix run --no-halt --no-start scripts/ci/portal-connection-fixture.exs
#
# `--no-start` is required, not optional: this script installs the fixture
# transports before it starts the application, so no boot-time provider
# recovery can reach a real HTTP client or CLI session. That is how
# `browser/scripts/run-connection-fixture-tests.ps1` starts it.
#
# The browser suite drives the real Portal API, the real PostgreSQL state and
# the real GitHub/Azure DevOps provider modules. Only each provider's external
# transport is replaced here: the HTTP client answers the canned provider
# documents the connection, resource-binding and sync flows read (accounts,
# repository documents, issues, workflow runs, pull requests, projects,
# repositories, work items and builds) and the CLI runner answers the
# local-session probes. Any other provider path or method is refused, never
# answered by a blanket document. Nothing in this file contacts a provider host,
# and every fixture call is appended to the JSONL log named by
# SYMMETRY_FIXTURE_TRANSPORT_LOG so a receipt can show what ran.
#
# SYMMETRY_FIXTURE_STATE_FILE optionally steers individual provider visits:
# `fail`/`transport_fail` inject a definitive HTTP 500 or an unknown
# (transport) outcome, `issues`/`work_items` choose the reported work items,
# `repository` overrides a document, and `hold` blocks a read until the state
# file releases it. The file is re-read on every request; absent, unreadable or
# invalid is the empty state. See browser/README.md for the schema.
#
# Accounts listed in SYMMETRY_FIXTURE_UNAUTHORIZED_ACCOUNTS answer 401, which is
# the expired-authentication fixture. The CLI tokens below are canaries: the
# browser acceptance run asserts that no canary reaches browser state, a recorded
# response body or the server log.

defmodule PortalConnectionFixture.Log do
  @moduledoc false

  def append(entry) do
    with path when is_binary(path) <- System.get_env("SYMMETRY_FIXTURE_TRANSPORT_LOG") do
      File.write!(path, Jason.encode!(entry) <> "\n", [:append])
    end

    :ok
  end
end

defmodule PortalConnectionFixture.Commands do
  @moduledoc false

  @github_token "gho_fixture_connection_token_canary"
  @azure_token "fixture-ado-access-token-canary"

  def token(:github), do: @github_token
  def token(:azure_devops), do: @azure_token

  def run("gh", ["config", "get", "git_protocol" | _rest], _options) do
    PortalConnectionFixture.Log.append(%{kind: "command", executable: "gh", command: "git_protocol", output: "https"})
    {:ok, "https"}
  end

  def run("gh", ["auth", "token" | _rest], _options) do
    PortalConnectionFixture.Log.append(%{kind: "command", executable: "gh", command: "auth_token", output: "fixture-canary"})
    {:ok, @github_token}
  end

  def run("az", arguments, _options) when is_list(arguments) do
    PortalConnectionFixture.Log.append(%{kind: "command", executable: "az", command: "access_token", output: "fixture-canary"})
    {:ok, @azure_token}
  end

  def run(executable, _arguments, _options) do
    PortalConnectionFixture.Log.append(%{kind: "command", executable: executable, command: "unsupported", output: "rejected"})
    {:error, {:command_unavailable, executable}}
  end
end

defmodule PortalConnectionFixture.Transport do
  @moduledoc false

  # Fixed provider timestamps keep a run's canned documents reproducible: the
  # provider modules parse them as DateTimes, and nothing treats their value as
  # evidence.
  @fixture_timestamp "2026-11-17T00:00:00Z"
  @fixture_finish_time "2026-11-17T00:15:00Z"

  # A reference listed in `hold` blocks its provider read until the state file
  # releases it, so a browser flow can keep a sync in flight and still act; the
  # cap keeps a forgotten hold from parking a portal request forever.
  @hold_attempts 300
  @hold_poll_ms 100

  def request(method, url, headers, _body \\ nil) do
    uri = URI.parse(url)
    unauthorized = unauthorized_accounts()
    authorization = authorization_shape(headers)

    # Only the fixture's own provider calls are answered: a GET to either
    # provider host, or a POST to Azure DevOps (which reads work items with
    # POST), each carrying that provider's fixture token. A dropped or
    # mismatched bearer, or a method the contract does not accept, fails the
    # run instead of being answered by a blanket canned document.
    expected = expected_authorization(method, uri)

    {result, steered} =
      if expected == authorization do
        visit(method, uri, unauthorized)
      else
        {{:error,
          {:fixture_unexpected_request, uri.host, to_string(method), authorization, expected}}, nil}
      end

    PortalConnectionFixture.Log.append(log_entry(uri, method, authorization, result, steered))

    case result do
      {:ok, status, document} -> {:ok, status, %{}, document}
      {:error, reason} -> {:error, reason}
    end
  end

  # `await_release/2` returns immediately when the reference is not held, so a
  # normal visit reads the state once here; a released hold re-reads it, so the
  # answer comes from the state as of release (`hold` plus `fail` yields the
  # late failure). Both steering and holding read the file on every request.
  defp visit(method, uri, unauthorized) do
    case await_release(uri) do
      :released -> respond_from_state(method, uri, unauthorized)
      :hold_timeout -> {{:error, {:transport, "fixture hold timed out"}}, :hold_timeout}
    end
  end

  defp respond_from_state(method, uri, unauthorized) do
    state = state()

    case steered_outcome(state, uri) do
      :transport_fail ->
        {{:error, {:transport, "fixture transport failure"}}, :transport_fail}

      :fail ->
        {{:ok, 500, %{"message" => "Fixture injected provider failure"}}, :fail}

      nil ->
        {respond(method, uri, unauthorized, state), nil}
    end
  end

  defp await_release(uri, attempts \\ @hold_attempts) do
    if held?(state(), uri) do
      if attempts > 1 do
        Process.sleep(@hold_poll_ms)
        await_release(uri, attempts - 1)
      else
        :hold_timeout
      end
    else
      :released
    end
  end

  # No token value is ever logged; only the bearer's shape, plus the steering
  # marker when a visit was injected, so the log proves which provider read
  # failed and how without recording a credential.
  defp log_entry(uri, method, authorization, result, steered) do
    entry =
      case result do
        {:ok, status, _document} ->
          %{
            kind: "http",
            method: to_string(method),
            host: uri.host,
            path: uri.path,
            status: status,
            authorization: authorization
          }

        {:error, reason} ->
          %{
            kind: "http",
            method: to_string(method),
            host: uri.host,
            path: uri.path,
            status: "rejected",
            reason: inspect(reason),
            authorization: authorization
          }
      end

    if steered, do: Map.put(entry, :steered, to_string(steered)), else: entry
  end

  # The fixture's provider modules authenticate with their own fixture tokens;
  # these shapes are the kinds fixture_token_kind/1 assigns to
  # Commands.token(:github) and Commands.token(:azure_devops), and a token
  # change has to update them in the same edit.
  # `SymmetryControl.Integrations.HTTP.request/4` takes the method as a
  # lowercase atom (`:get`/`:post`/`:patch`/`:put`), which `to_string/1` logs.
  # Azure DevOps is the only provider whose reads use POST (`/_apis/wit/wiql`
  # and `/_apis/wit/workitemsbatch`); GitHub stays GET-only.
  defp expected_authorization(:get, %URI{host: "api.github.com"}), do: "bearer:github-fixture"

  defp expected_authorization(method, %URI{host: "dev.azure.com"}) when method in [:get, :post],
    do: "bearer:azure-fixture"

  defp expected_authorization(_method, _uri), do: :unexpected

  # No status field is logged for the bearer header; only its shape, so the log
  # proves a fixture token was used without recording its value.
  defp authorization_shape(headers) do
    headers
    |> Enum.find_value(fn {name, value} ->
      if String.downcase(to_string(name)) == "authorization" do
        case to_string(value) do
          "Bearer " <> token -> "bearer:" <> fixture_token_kind(token)
          _other -> "other"
        end
      end
    end)
    |> Kernel.||("absent")
  end

  defp fixture_token_kind(token) do
    cond do
      token == PortalConnectionFixture.Commands.token(:github) -> "github-fixture"
      token == PortalConnectionFixture.Commands.token(:azure_devops) -> "azure-fixture"
      true -> "unknown"
    end
  end

  defp respond(_method, %URI{host: "api.github.com", path: "/user"}, _unauthorized, _state),
    do:
      {:ok, 200,
       %{
         "login" => "fixture-actor",
         "html_url" => "https://github.com/fixture-actor",
         "type" => "User"
       }}

  defp respond(
         _method,
         %URI{host: "api.github.com", path: "/user/memberships/orgs/" <> account},
         unauthorized,
         _state
       ) do
    account = URI.decode(account)

    if account in unauthorized do
      {:ok, 401, %{"message" => "Bad credentials"}}
    else
      {:ok, 200,
       %{
         "state" => "active",
         "organization" => %{
           "login" => account,
           "html_url" => "https://github.com/#{account}",
           "type" => "Organization"
         }
       }}
    end
  end

  defp respond(
         _method,
         %URI{host: "api.github.com", path: "/repos/" <> reference},
         unauthorized,
         state
       ) do
    with {:ok, {owner, repository, tail}} <- repository_path(reference),
         :ok <- authorize(owner, unauthorized) do
      github_document(owner, repository, tail, state)
    else
      :unauthorized -> {:ok, 401, %{"message" => "Bad credentials"}}
      _invalid -> {:error, {:fixture_no_route, "api.github.com"}}
    end
  end

  defp respond(method, %URI{host: "dev.azure.com", path: path, query: query}, unauthorized, state) do
    with {:ok, {organization, remainder}} <- dev_azure_scope(path),
         :ok <- authorize(organization, unauthorized) do
      dev_azure_document(method, organization, remainder, query, state)
    else
      :unauthorized -> {:ok, 401, %{"message" => "The access token expired"}}
      _invalid -> {:error, {:fixture_no_route, "dev.azure.com"}}
    end
  end

  # An authenticated fixture request for a provider path the contract does not
  # define is refused, never answered by a blanket document.
  defp respond(_method, %URI{host: host}, _unauthorized, _state),
    do: {:error, {:fixture_no_route, host}}

  defp github_document(owner, repository, [], state),
    do: {:ok, 200, github_repository(owner, repository, state)}

  defp github_document(owner, repository, ["issues"], state) do
    numbers = github_issue_numbers(owner, repository, state)
    {:ok, 200, Enum.map(numbers, &github_issue(owner, repository, &1))}
  end

  defp github_document(owner, repository, ["actions", "runs"], _state),
    do: {:ok, 200, %{"workflow_runs" => [github_workflow_run(owner, repository)]}}

  defp github_document(owner, repository, ["pulls"], _state),
    do: {:ok, 200, [github_pull_request(owner, repository)]}

  defp github_document(owner, repository, ["pulls", _number], _state),
    do: {:ok, 200, github_pull_request(owner, repository)}

  defp github_document(_owner, _repository, ["pulls", _number, "reviews"], _state),
    do: {:ok, 200, [%{"user" => %{"login" => "fixture-reviewer"}, "state" => "APPROVED"}]}

  defp github_document(_owner, _repository, _tail, _state),
    do: {:error, {:fixture_no_route, "api.github.com"}}

  defp github_repository(owner, repository, state) do
    defaults = %{
      "id" => 1_457_073_269,
      "node_id" => "R_fixture_repository",
      "html_url" => "https://github.com/#{owner}/#{repository}",
      "default_branch" => "main",
      "visibility" => "private",
      "archived" => false
    }

    overrides =
      case get_in(provider_state(state, "github"), ["repository", "#{owner}/#{repository}"]) do
        value when is_map(value) ->
          Map.take(value, ["archived", "html_url", "default_branch", "visibility"])

        _unset ->
          %{}
      end

    Map.merge(defaults, overrides)
  end

  defp github_issue_numbers(owner, repository, state) do
    case get_in(provider_state(state, "github"), ["issues", "#{owner}/#{repository}"]) do
      numbers when is_list(numbers) -> numbers
      _unset -> [101, 102]
    end
  end

  # 101 is the open fixture issue; 102 (and any other configured number) is
  # closed. An empty list makes a previously imported issue absent from the
  # readback.
  defp github_issue(owner, repository, number) do
    %{
      "number" => number,
      "html_url" => "https://github.com/#{owner}/#{repository}/issues/#{number}",
      "state" => if(number == 101, do: "open", else: "closed"),
      "updated_at" => @fixture_timestamp,
      "assignee" => %{"login" => "fixture-assignee"},
      "labels" => [%{"name" => "priority: high"}],
      "title" => "Fixture issue #{number}",
      "body" => "Imported from the fixture transport."
    }
  end

  defp github_workflow_run(owner, repository) do
    %{
      "id" => 1,
      "name" => "CI",
      "workflow_id" => 7,
      "status" => "completed",
      "conclusion" => "success",
      "html_url" => "https://github.com/#{owner}/#{repository}/actions/runs/1",
      "head_branch" => "main",
      "head_sha" => "fixture-head-sha"
    }
  end

  defp github_pull_request(owner, repository) do
    %{
      "number" => 42,
      "html_url" => "https://github.com/#{owner}/#{repository}/pull/42",
      "state" => "open",
      "merged_at" => nil,
      "title" => "Fixture pull request",
      "head" => %{"sha" => "fixture-head-sha", "ref" => "fixture-head"},
      "base" => %{"ref" => "main"},
      "user" => %{"login" => "fixture-actor"}
    }
  end

  defp dev_azure_document(method, organization, remainder, query, state) do
    case {method, String.split(remainder, "/")} do
      {:get, ["_apis", "projects"]} ->
        {:ok, 200,
         %{"count" => 1, "value" => [%{"id" => "fixture-project", "name" => "Fixture project"}]}}

      {:get, [project, "_apis", "git", "repositories", repository]} ->
        {:ok, 200, dev_azure_repository(organization, project, repository, state)}

      {:post, [project, "_apis", "wit", "wiql"]} ->
        ids = dev_azure_work_item_ids(organization, project, state)
        {:ok, 200, %{"workItems" => Enum.map(ids, &%{"id" => &1})}}

      {:post, [project, "_apis", "wit", "workitemsbatch"]} ->
        items =
          organization
          |> dev_azure_work_item_ids(project, state)
          |> Enum.map(&dev_azure_work_item(organization, project, &1))

        {:ok, 200, %{"count" => length(items), "value" => items}}

      {:get, [project, "_apis", "build", "definitions", definition_id]} ->
        dev_azure_build_definition(organization, project, definition_id)

      {:get, [project, "_apis", "build", "builds"]} ->
        {:ok, 200, dev_azure_builds(organization, project, query)}

      _other ->
        {:error, {:fixture_no_route, "dev.azure.com"}}
    end
  end

  defp dev_azure_repository(organization, project, repository, state) do
    defaults = %{
      "id" => "fixture-repository",
      "name" => repository,
      "webUrl" => "https://dev.azure.com/#{organization}/#{project}/_git/#{repository}",
      "defaultBranch" => "refs/heads/main"
    }

    overrides =
      case get_in(provider_state(state, "azure_devops"), [
             "repository",
             "#{organization}/#{project}/#{repository}"
           ]) do
        value when is_map(value) -> Map.take(value, ["webUrl", "defaultBranch"])
        _unset -> %{}
      end

    Map.merge(defaults, overrides)
  end

  defp dev_azure_work_item_ids(organization, project, state) do
    case get_in(provider_state(state, "azure_devops"), ["work_items", "#{organization}/#{project}"]) do
      ids when is_list(ids) -> ids
      _unset -> [101]
    end
  end

  defp dev_azure_work_item(organization, project, id) do
    %{
      "id" => id,
      "rev" => 1,
      "url" => "https://dev.azure.com/#{organization}/#{project}/_apis/wit/workItems/#{id}",
      "_links" => %{
        "html" => %{
          "href" => "https://dev.azure.com/#{organization}/#{project}/_workitems/edit/#{id}"
        }
      },
      "fields" => %{
        "System.Id" => id,
        "System.Title" => "Fixture work item #{id}",
        "System.Description" => "Imported from the fixture transport.",
        "System.State" => "Active",
        "System.Tags" => "priority: high",
        "System.AssignedTo" => %{"displayName" => "Fixture Assignee"},
        "System.ChangedDate" => @fixture_timestamp,
        "System.WorkItemType" => "Bug"
      }
    }
  end

  defp dev_azure_build_definition(organization, project, definition_id) do
    case Integer.parse(definition_id) do
      {id, ""} when id > 0 ->
        {:ok, 200,
         %{
           "id" => id,
           "name" => "Fixture pipeline #{id}",
           "url" => "https://dev.azure.com/#{organization}/#{project}/_apis/build/definitions/#{id}"
         }}

      _invalid ->
        {:error, {:fixture_no_route, "dev.azure.com"}}
    end
  end

  defp dev_azure_builds(organization, project, query) do
    %{
      "count" => 1,
      "value" => [
        %{
          "id" => 42,
          "buildNumber" => "20261117.1",
          "status" => "completed",
          "result" => "succeeded",
          "sourceBranch" => "refs/heads/main",
          "sourceVersion" => "fixture-head-sha",
          "definition" => %{"id" => requested_definition_id(query)},
          "_links" => %{
            "web" => %{
              "href" => "https://dev.azure.com/#{organization}/#{project}/_build/results?buildId=42"
            }
          },
          "finishTime" => @fixture_finish_time,
          "queueTime" => @fixture_timestamp
        }
      ]
    }
  end

  # The pipeline-form CI read filters builds by definition, so the canned build
  # names the requested definition when the query carries one; the
  # repository-form read has no such filter and gets a fixed id.
  defp requested_definition_id(query) when is_binary(query) do
    case URI.decode_query(query) do
      %{"definitions" => value} ->
        case Integer.parse(value) do
          {id, ""} when id > 0 -> id
          _invalid -> 1
        end

      _unfiltered ->
        1
    end
  end

  defp requested_definition_id(_query), do: 1

  defp dev_azure_scope("/" <> rest) do
    case String.split(rest, "/", parts: 2) do
      [organization, remainder] when organization != "" and remainder != "" ->
        {:ok, {organization, remainder}}

      _other ->
        :invalid
    end
  end

  defp dev_azure_scope(_path), do: :invalid

  # The steering reference is `owner/repo` for every GitHub repository path and
  # `org/project` for every Azure DevOps project path. Provider paths without a
  # project scope (the GitHub account reads, the Azure DevOps projects read) are
  # never steered.
  defp provider_reference(%URI{host: "api.github.com", path: "/repos/" <> reference}) do
    case repository_path(reference) do
      {:ok, {owner, repository, _tail}} -> {:github, "#{owner}/#{repository}"}
      _invalid -> nil
    end
  end

  defp provider_reference(%URI{host: "dev.azure.com", path: "/" <> rest}) do
    case String.split(rest, "/", parts: 2) do
      [_organization, "_apis/" <> _tail] ->
        nil

      [organization, remainder] ->
        [project | _tail] = String.split(remainder, "/", parts: 2)
        {:azure_devops, "#{organization}/#{project}"}

      _other ->
        nil
    end
  end

  defp provider_reference(_uri), do: nil

  defp steered_outcome(state, uri) do
    case provider_reference(uri) do
      {:github, reference} -> steer(provider_state(state, "github"), reference)
      {:azure_devops, reference} -> steer(provider_state(state, "azure_devops"), reference)
      nil -> nil
    end
  end

  defp held?(state, uri) do
    case provider_reference(uri) do
      {:github, reference} ->
        reference in reference_list(get_in(provider_state(state, "github"), ["hold"]))

      {:azure_devops, reference} ->
        reference in reference_list(get_in(provider_state(state, "azure_devops"), ["hold"]))

      nil ->
        false
    end
  end

  # A reference in both lists answers the transport failure: the unknown
  # outcome is the stronger statement.
  defp steer(section, reference) do
    cond do
      reference in reference_list(section["transport_fail"]) -> :transport_fail
      reference in reference_list(section["fail"]) -> :fail
      true -> nil
    end
  end

  # SYMMETRY_FIXTURE_STATE_FILE is optional and re-read on every request; an
  # absent, unreadable or invalid document is the empty state, so a missing
  # steering file never fabricates a document or a failure.
  defp state do
    with path when is_binary(path) <- System.get_env("SYMMETRY_FIXTURE_STATE_FILE"),
         {:ok, raw} <- File.read(path),
         {:ok, decoded} when is_map(decoded) <- Jason.decode(raw) do
      decoded
    else
      _absent_or_invalid -> %{}
    end
  end

  defp provider_state(state, provider) do
    case state[provider] do
      section when is_map(section) -> section
      _other -> %{}
    end
  end

  defp reference_list(value) when is_list(value), do: value
  defp reference_list(_value), do: []

  defp authorize(owner, unauthorized) do
    if owner in unauthorized, do: :unauthorized, else: :ok
  end

  # The GitHub reference is split into owner, repository and the remaining path
  # segments so one parse serves both steering and document routing.
  defp repository_path(reference) do
    case String.split(reference, "/", parts: 3) do
      [owner, repository] -> {:ok, {owner, repository, []}}
      [owner, repository, tail] -> {:ok, {owner, repository, String.split(tail, "/")}}
      _other -> {:error, :invalid_repository_reference}
    end
  end

  defp unauthorized_accounts do
    "SYMMETRY_FIXTURE_UNAUTHORIZED_ACCOUNTS"
    |> System.get_env("")
    |> String.split(",", trim: true)
    |> Enum.map(&String.trim/1)
  end
end

Application.put_env(
  :symmetry_control,
  :integration_command_runner,
  PortalConnectionFixture.Commands
)

Application.put_env(
  :symmetry_control,
  :integration_http_client,
  PortalConnectionFixture.Transport
)

# The harness starts this script with `mix run --no-start`: the transports must
# be installed before the application boots, because the boot-time provider
# recovery sweep would otherwise use the real HTTP client or CLI session even
# for provider work an earlier run left pending. The application starts here,
# with the fixture transports already in its environment.
{:ok, _started} = Application.ensure_all_started(:symmetry_control)

# The development logger prints every SQL statement at debug level. A browser
# run produces thousands of them, and a blocked output pipe stalls the endpoint
# mid-response, so this fixture keeps warnings and errors only. The remaining
# lines are what the acceptance run scans for fixture canaries.
require Logger

Logger.configure(level: :warning)

# The fixture keeps the application's real supervision topology: the
# orchestration scheduler and reconciler stay up, provider access serves the
# provider actions and /healthz reports all of them. The run identity is
# written for the harness and the browser spec, which use it to fail closed
# before any provider check when the target is not this fixture.
identity = System.get_env("SYMMETRY_FIXTURE_IDENTITY")
identity_file = System.get_env("SYMMETRY_FIXTURE_IDENTITY_FILE")
port = System.get_env("PORT")

if identity && identity_file do
  File.write!(
    identity_file,
    Jason.encode!(%{identity: identity, port: port, pid: System.pid()})
  )
else
  Logger.warning(
    "connection fixture identity is not configured: the browser spec requires " <>
      "SYMMETRY_FIXTURE_IDENTITY and SYMMETRY_FIXTURE_IDENTITY_FILE from " <>
      "browser/scripts/run-connection-fixture-tests.ps1 and refuses to run without them"
  )
end

if path = System.get_env("SYMMETRY_FIXTURE_TRANSPORT_LOG") do
  File.write!(path, "", [:append])
end

Logger.warning(
  "connection fixture transport installed: provider HTTP and CLI sessions are canned, " <>
    "no provider host is contacted, unauthorized accounts=#{inspect(System.get_env("SYMMETRY_FIXTURE_UNAUTHORIZED_ACCOUNTS", ""))}, " <>
    "identity=#{inspect(identity)}, port=#{inspect(port)}"
)
