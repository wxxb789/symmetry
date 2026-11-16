# Fixture launcher for the connection-management browser acceptance run.
#
#   PHX_SERVER=true PORT=4021 mix run --no-halt scripts/ci/portal-connection-fixture.exs
#
# The browser suite drives the real Portal API, the real PostgreSQL state and
# the real GitHub/Azure DevOps provider modules. Only each provider's external
# transport is replaced here: the HTTP client answers canned provider documents
# and the CLI runner answers the local-session probes. Nothing in this file
# contacts a provider host, and every fixture call is appended to the JSONL log
# named by SYMMETRY_FIXTURE_TRANSPORT_LOG so a receipt can show what ran.
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

  def request(method, url, headers, _body \\ nil) do
    uri = URI.parse(url)
    unauthorized = unauthorized_accounts()

    case respond(method, uri, unauthorized) do
      {:ok, status, document} ->
        PortalConnectionFixture.Log.append(%{
          kind: "http",
          method: to_string(method),
          host: uri.host,
          path: uri.path,
          status: status,
          authorization: authorization_shape(headers)
        })

        {:ok, status, %{}, document}

      {:error, reason} ->
        PortalConnectionFixture.Log.append(%{
          kind: "http",
          method: to_string(method),
          host: uri.host,
          path: uri.path,
          status: "rejected",
          reason: inspect(reason),
          authorization: authorization_shape(headers)
        })

        {:error, reason}
    end
  end

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

  defp respond(_method, %URI{host: "api.github.com", path: "/user"}, _unauthorized),
    do: {:ok, 200, %{"login" => "fixture-actor", "html_url" => "https://github.com/fixture-actor", "type" => "User"}}

  defp respond(_method, %URI{host: "api.github.com", path: "/user/memberships/orgs/" <> account}, unauthorized) do
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

  defp respond(_method, %URI{host: "api.github.com", path: "/repos/" <> reference}, unauthorized) do
    with {:ok, {owner, repository}} <- repository_parts(reference),
         :ok <- authorize(owner, unauthorized) do
      {:ok, 200,
       %{
         "id" => 1_457_073_269,
         "node_id" => "R_fixture_repository",
         "html_url" => "https://github.com/#{owner}/#{repository}",
         "default_branch" => "main",
         "visibility" => "private",
         "archived" => false
       }}
    else
      :unauthorized -> {:ok, 401, %{"message" => "Bad credentials"}}
      _error -> {:ok, 404, %{"message" => "Not Found"}}
    end
  end

  defp respond(_method, %URI{host: "dev.azure.com", path: path}, unauthorized) do
    case path do
      "/" <> rest ->
        [organization | _tail] = String.split(rest, "/")

        if organization in unauthorized do
          {:ok, 401, %{"message" => "The access token expired"}}
        else
          {:ok, 200, %{"count" => 1, "value" => [%{"id" => "fixture-project", "name" => "Fixture project"}]}}
        end

      _other ->
        {:ok, 404, %{"message" => "Not Found"}}
    end
  end

  defp respond(_method, %URI{host: host}, _unauthorized), do: {:error, {:fixture_no_route, host}}

  defp authorize(owner, unauthorized) do
    if owner in unauthorized, do: :unauthorized, else: :ok
  end

  defp repository_parts(reference) do
    case String.split(reference, "/") do
      [owner, repository] -> {:ok, {owner, repository}}
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
