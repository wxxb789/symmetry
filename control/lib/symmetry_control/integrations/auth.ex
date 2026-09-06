defmodule SymmetryControl.Integrations.Auth do
  @moduledoc false

  @azure_devops_resource "499b84ac-1321-427f-aa17-267ca6975798"
  @github_api_version "2026-03-10"
  @github_command_options [
    env: [{"GH_TOKEN", nil}, {"GITHUB_TOKEN", nil}],
    stderr_to_stdout: false
  ]

  def github_headers(%{auth_type: "gh_cli"}) do
    with {:ok, protocol} <-
           runner().run(
             "gh",
             ["config", "get", "git_protocol", "--host", "github.com"],
             @github_command_options
           ),
         :ok <- require_https(protocol),
         {:ok, token} <-
           runner().run(
             "gh",
             ["auth", "token", "--hostname", "github.com"],
             @github_command_options
           ),
         :ok <- require_github_oauth_token(token) do
      {:ok,
       [
         {"accept", "application/vnd.github+json"},
         {"authorization", "Bearer " <> String.trim(token)},
         {"user-agent", "symmetry-control"},
         {"x-github-api-version", @github_api_version}
       ]}
    end
  end

  def github_headers(_connection), do: {:error, :invalid_auth_type}

  def azure_devops_headers(%{auth_type: "entra_id"}) do
    with {:ok, token} <-
           runner().run(
             "az",
             [
               "--only-show-errors",
               "account",
               "get-access-token",
               "--resource",
               @azure_devops_resource,
               "--query",
               "accessToken",
               "--output",
               "tsv"
             ],
             stderr_to_stdout: false
           ),
         :ok <- require_token(token) do
      {:ok,
       [
         {"accept", "application/json"},
         {"authorization", "Bearer " <> String.trim(token)}
       ]}
    end
  end

  def azure_devops_headers(_connection), do: {:error, :invalid_auth_type}

  defp require_https(protocol) do
    if String.downcase(String.trim(protocol)) == "https",
      do: :ok,
      else: {:error, :github_https_required}
  end

  defp require_token(token) do
    if is_binary(token) and String.trim(token) != "",
      do: :ok,
      else: {:error, :authentication_unavailable}
  end

  defp require_github_oauth_token(token) do
    case String.trim(token || "") do
      "gho_" <> value when value != "" -> :ok
      _token -> {:error, :github_oauth_required}
    end
  end

  defp runner do
    Application.get_env(
      :symmetry_control,
      :integration_command_runner,
      SymmetryControl.Integrations.Command
    )
  end
end
