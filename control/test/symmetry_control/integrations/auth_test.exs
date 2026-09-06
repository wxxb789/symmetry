defmodule SymmetryControl.Integrations.AuthTest do
  use ExUnit.Case, async: false

  alias SymmetryControl.Integrations.{Auth, CommandStub}

  setup do
    previous = Application.get_env(:symmetry_control, :integration_command_runner)
    Application.put_env(:symmetry_control, :integration_command_runner, CommandStub)

    on_exit(fn ->
      CommandStub.expect([])

      if previous do
        Application.put_env(:symmetry_control, :integration_command_runner, previous)
      else
        Application.delete_env(:symmetry_control, :integration_command_runner)
      end
    end)

    :ok
  end

  test "GitHub authorization comes from gh auth and requires HTTPS git protocol" do
    CommandStub.expect([
      %{
        executable: "gh",
        arguments: ["config", "get", "git_protocol", "--host", "github.com"],
        options: [env: [{"GH_TOKEN", nil}, {"GITHUB_TOKEN", nil}], stderr_to_stdout: false],
        response: {:ok, "https\n"}
      },
      %{
        executable: "gh",
        arguments: ["auth", "token", "--hostname", "github.com"],
        options: [env: [{"GH_TOKEN", nil}, {"GITHUB_TOKEN", nil}], stderr_to_stdout: false],
        response: {:ok, "gho_runtime-github-token\n"}
      }
    ])

    assert {:ok, headers} = Auth.github_headers(%{auth_type: "gh_cli"})
    assert {"authorization", "Bearer gho_runtime-github-token"} in headers
    CommandStub.verify!()
  end

  test "GitHub rejects non-HTTPS gh configuration" do
    CommandStub.expect([
      %{
        executable: "gh",
        arguments: ["config", "get", "git_protocol", "--host", "github.com"],
        options: [env: [{"GH_TOKEN", nil}, {"GITHUB_TOKEN", nil}], stderr_to_stdout: false],
        response: {:ok, "ssh\n"}
      }
    ])

    assert {:error, :github_https_required} = Auth.github_headers(%{auth_type: "gh_cli"})
    CommandStub.verify!()
  end

  test "GitHub rejects personal access tokens returned by gh" do
    for token <- ["ghp_classic", "github_pat_fine_grained"] do
      CommandStub.expect([
        %{
          executable: "gh",
          arguments: ["config", "get", "git_protocol", "--host", "github.com"],
          options: [env: [{"GH_TOKEN", nil}, {"GITHUB_TOKEN", nil}], stderr_to_stdout: false],
          response: {:ok, "https\n"}
        },
        %{
          executable: "gh",
          arguments: ["auth", "token", "--hostname", "github.com"],
          options: [env: [{"GH_TOKEN", nil}, {"GITHUB_TOKEN", nil}], stderr_to_stdout: false],
          response: {:ok, token}
        }
      ])

      assert {:error, :github_oauth_required} =
               Auth.github_headers(%{auth_type: "gh_cli"})

      CommandStub.verify!()
    end
  end

  test "Azure DevOps authorization uses a Microsoft Entra access token" do
    CommandStub.expect([
      %{
        executable: "az",
        arguments: [
          "--only-show-errors",
          "account",
          "get-access-token",
          "--resource",
          "499b84ac-1321-427f-aa17-267ca6975798",
          "--query",
          "accessToken",
          "--output",
          "tsv"
        ],
        options: [stderr_to_stdout: false],
        response: {:ok, "runtime-entra-token\n"}
      }
    ])

    assert {:ok, headers} = Auth.azure_devops_headers(%{auth_type: "entra_id"})
    assert {"authorization", "Bearer runtime-entra-token"} in headers
    CommandStub.verify!()
  end
end
