defmodule SymmetryControl.ProviderBrokerE2ETest do
  use SymmetryControlWeb.ConnCase, async: false

  alias SymmetryControl.Integrations
  alias SymmetryControl.Integrations.HTTPStub
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.WorkItem
  alias SymmetryControlWeb.PortalSession

  @moduletag :provider_broker_e2e
  @moduletag timeout: 120_000
  @moduletag skip: System.get_env("SYMMETRY_PROVIDER_BROKER_E2E") != "1"

  setup_all do
    daemon_dir = Path.expand("../../../daemon", __DIR__)

    build_dir =
      Path.join(System.tmp_dir!(), "symmetry-provider-broker-e2e-#{Ecto.UUID.generate()}")

    File.rm_rf!(build_dir)
    File.mkdir_p!(build_dir)
    on_exit(fn -> File.rm_rf!(build_dir) end)

    extension = if match?({:win32, _}, :os.type()), do: ".exe", else: ""

    daemon =
      System.get_env("SYMMETRY_PROVIDER_BROKER_E2E_DAEMON") ||
        build_go_binary!(daemon_dir, build_dir, "symmetry-daemon" <> extension)

    agent =
      System.get_env("SYMMETRY_PROVIDER_BROKER_E2E_AGENT") ||
        build_go_binary!(daemon_dir, build_dir, "symmetry-fake-agent" <> extension)

    {:ok, daemon: daemon, agent: agent}
  end

  setup %{daemon: daemon, agent: agent} do
    previous_http = Application.get_env(:symmetry_control, :integration_http_client)
    previous_auth = Application.get_env(:symmetry_control, :integration_auth_provider)
    previous_providers = Application.get_env(:symmetry_control, :integration_providers)
    previous_orchestration = Application.fetch_env!(:symmetry_control, :orchestration)

    Application.put_env(:symmetry_control, :integration_http_client, HTTPStub)

    Application.put_env(
      :symmetry_control,
      :integration_auth_provider,
      SymmetryControl.Integrations.AuthStub
    )

    Application.put_env(:symmetry_control, :integration_providers,
      github: SymmetryControl.Integrations.Providers.GitHub,
      azure_devops: SymmetryControl.Integrations.Providers.AzureDevOps
    )

    Application.put_env(
      :symmetry_control,
      :orchestration,
      Keyword.merge(previous_orchestration,
        heartbeat_interval_ms: 100,
        poll_interval_ms: 100,
        lease_duration_ms: 30_000
      )
    )

    HTTPStub.stop_shared()

    on_exit(fn ->
      HTTPStub.stop_shared()
      restore_env(:integration_http_client, previous_http)
      restore_env(:integration_auth_provider, previous_auth)
      restore_env(:integration_providers, previous_providers)
      Application.put_env(:symmetry_control, :orchestration, previous_orchestration)
    end)

    listener =
      start_supervised!(
        {Bandit,
         plug: SymmetryControlWeb.Endpoint,
         scheme: :http,
         ip: {127, 0, 0, 1},
         port: 0,
         startup_log: false}
      )

    {:ok, {{127, 0, 0, 1}, port}} = ThousandIsland.listener_info(listener)
    runtime_key = "provider-e2e-#{Ecto.UUID.generate()}"
    profile = "provider-e2e"
    workspace = "provider-e2e"
    run_dir = Path.join(System.tmp_dir!(), "symmetry-provider-broker-run-#{Ecto.UUID.generate()}")
    state_dir = Path.join(run_dir, "state")
    workspace_path = Path.join(run_dir, "workspace")
    File.mkdir_p!(state_dir)
    File.mkdir_p!(workspace_path)

    config_path = Path.join(run_dir, "daemon.json")

    File.write!(
      config_path,
      Jason.encode!(%{
        control_plane_url: "http://127.0.0.1:#{port}",
        allow_insecure_http: true,
        state_dir: state_dir,
        machine_name: "provider-e2e-#{Ecto.UUID.generate()}",
        agent_profiles: %{
          profile => %{
            command: agent,
            args: [],
            input_mode: "json",
            provider_access: true,
            interactive: false,
            event_format: "jsonl",
            env_allowlist: []
          }
        },
        workspaces: %{
          workspace => %{
            policy: "existing_checkout",
            path: workspace_path,
            cleanup: "never"
          }
        },
        runtime: %{
          runtime_key: runtime_key,
          name: "Provider broker E2E",
          capacity: 1,
          agent_profile: profile,
          workspace: workspace
        }
      })
    )

    daemon_process = start_daemon!(daemon, config_path)

    on_exit(fn ->
      stop_daemon!(daemon_process)
      File.rm_rf!(run_dir)
    end)

    daemon_port = daemon_process.port

    await!(daemon_port, "provider-capable runtime registration", fn ->
      case Repo.get_by(Runtime, runtime_key: runtime_key) do
        %Runtime{status: "online", capabilities: %{"provider_access" => true}} -> :ok
        _runtime -> :retry
      end
    end)

    {:ok, daemon_port: daemon_port, profile: profile, workspace: workspace}
  end

  test "GitHub Issue reaches GitHub pull request, review, and Actions projection", context do
    run_provider_flow!(github_case(), context)
  end

  test "Azure Boards work item reaches GitHub changes and Azure Pipelines projection", context do
    run_provider_flow!(mixed_provider_case(), context)
  end

  test "Azure Boards work item reaches Azure Repos, review, and Pipelines projection", context do
    run_provider_flow!(azure_devops_case(), context)
  end

  defp run_provider_flow!(provider_case, context) do
    HTTPStub.expect_shared(provider_case.expectations)
    work_item = create_connected_work_item!(provider_case, context.profile, context.workspace)

    assert work_item.title == provider_case.title
    assert work_item.description == provider_case.description
    assert work_item.priority == provider_case.priority
    assert work_item.external_assignee_name == provider_case.external_assignee_name

    assert {:ok, %{task: %{task: task}}, :created} =
             Workspaces.launch_work_item(
               work_item.id,
               "provider-e2e-#{provider_case.provider}"
             )

    _run =
      await!(context.daemon_port, "#{provider_case.provider} task assignment", fn ->
        case Orchestration.assign_one() do
          {:ok, run} -> {:ok, run}
          {:error, :no_assignment} -> :retry
        end
      end)

    await!(context.daemon_port, "#{provider_case.provider} provider action completion", fn ->
      case Orchestration.fetch_task(task.id) do
        {:ok, %{state: "completed"}} -> :ok
        _task -> :retry
      end
    end)

    HTTPStub.verify_shared!()

    assert %{
             "work_item" => %{
               "title" => title,
               "description" => description,
               "priority" => priority,
               "external" => %{
                 "id" => external_id,
                 "provider" => external_provider,
                 "assignee" => external_assignee_name,
                 "state" => external_state,
                 "available" => true
               },
               "pull_request_url" => pull_request_url,
               "review_status" => "approved",
               "ci_status" => "passed",
               "delivery" => %{
                 "pull_request" => %{
                   "source" => "provider",
                   "provider" => change_provider,
                   "status" => "open"
                 },
                 "review" => %{"source" => "provider", "provider" => change_provider},
                 "ci" => %{"source" => "provider", "provider" => ci_provider}
               }
             }
           } =
             portal_conn()
             |> get("/portal/api/work-items/#{work_item.id}")
             |> json_response(200)

    assert title == provider_case.title
    assert description == provider_case.description
    assert priority == provider_case.priority
    assert external_assignee_name == provider_case.external_assignee_name
    assert external_id == provider_case.external_id
    assert external_state == provider_case.external_state
    assert pull_request_url == provider_case.pull_request_url
    assert external_provider == provider_case.work_provider
    assert change_provider == provider_case.repository_provider
    assert ci_provider == provider_case.ci_provider
  end

  defp create_connected_work_item!(provider_case, profile, workspace) do
    suffix = String.slice(Ecto.UUID.generate(), 0, 8)

    {:ok, project} =
      Workspaces.create_project(%{
        name: "Provider E2E #{provider_case.provider} #{suffix}",
        key: "E#{String.upcase(String.slice(suffix, 0, 5))}",
        default_agent_profile: profile,
        default_workspace: workspace
      })

    connections =
      [
        provider_case.work_provider,
        provider_case.repository_provider,
        provider_case.ci_provider
      ]
      |> Enum.uniq()
      |> Map.new(fn provider ->
        {:ok, connection} =
          Integrations.create_connection(%{
            provider: provider,
            name: "#{provider}-#{suffix}",
            account_ref: "acme",
            capabilities: ["repositories", "work_items", "changes", "ci"]
          })

        assert connection.auth_type == auth_type(provider)
        {provider, connection}
      end)

    {:ok, repository} =
      Workspaces.create_resource(project.id, %{
        connection_id: connections[provider_case.repository_provider].id,
        kind: "repository",
        name: "Repository #{suffix}",
        external_ref: provider_case.repository_ref
      })

    {:ok, ci} =
      Workspaces.create_resource(project.id, %{
        connection_id: connections[provider_case.ci_provider].id,
        kind: "ci",
        name: "CI #{suffix}",
        external_ref: provider_case.ci_ref
      })

    {:ok, work_tracking} =
      Workspaces.create_resource(project.id, %{
        connection_id: connections[provider_case.work_provider].id,
        kind: "work_tracking",
        name: "Work tracking #{suffix}",
        external_ref: provider_case.work_tracking_ref
      })

    assert {:ok, _repository} = Integrations.sync_resource(repository.id)
    assert {:ok, _work_tracking} = Integrations.sync_resource(work_tracking.id)

    imported =
      Repo.get_by!(WorkItem,
        external_work_item_resource_id: work_tracking.id,
        external_id: provider_case.external_id
      )

    assert {:error, :provider_owned} =
             Workspaces.update_work_item(imported.id, %{
               version: imported.lock_version,
               title: "Local title must not replace provider work"
             })

    assert {:ok, bound} =
             Workspaces.update_work_item(imported.id, %{
               version: imported.lock_version,
               repository_resource_id: repository.id,
               ci_resource_id: ci.id,
               assignee_type: "agent",
               agent_profile: profile,
               workspace: workspace,
               branch: provider_case.branch
             })

    bound
  end

  defp github_case do
    description = "[symmetry-fake-agent:provider_action_flow]\nKeep GitHub authoritative."

    %{
      provider: "github",
      work_provider: "github",
      repository_provider: "github",
      ci_provider: "github",
      repository_ref: "acme/symmetry",
      work_tracking_ref: "acme/symmetry",
      ci_ref: "acme/symmetry",
      external_id: "12",
      external_state: "open",
      title: "Ship connected GitHub work",
      description: description,
      priority: "high",
      external_assignee_name: "octocat",
      branch: "codex/provider-e2e-github",
      pull_request_url: "https://github.com/acme/symmetry/pull/42",
      expectations: [
        github_expectation(:get, "/repos/acme/symmetry", %{
          "html_url" => "https://github.com/acme/symmetry",
          "default_branch" => "main",
          "visibility" => "private",
          "archived" => false
        }),
        github_expectation(:get, "/repos/acme/symmetry/issues?", [
          %{
            "number" => 12,
            "title" => "Ship connected GitHub work",
            "body" => description,
            "state" => "open",
            "html_url" => "https://github.com/acme/symmetry/issues/12",
            "updated_at" => "2026-09-05T09:30:00Z",
            "labels" => [%{"name" => "priority:high"}],
            "assignee" => %{"login" => "octocat"}
          }
        ]),
        github_expectation(:get, "/repos/acme/symmetry/pulls?", []),
        github_expectation(
          :post,
          "/repos/acme/symmetry/pulls",
          github_pull_request(),
          &assert_github_pull_request_body!/1
        ),
        github_expectation(:get, "/repos/acme/symmetry", %{
          "html_url" => "https://github.com/acme/symmetry",
          "default_branch" => "main",
          "visibility" => "private",
          "archived" => false
        }),
        github_expectation(:get, "/repos/acme/symmetry/pulls/42", github_pull_request()),
        github_expectation(:get, "/repos/acme/symmetry/pulls/42/reviews?", [
          %{"user" => %{"login" => "reviewer"}, "state" => "APPROVED"}
        ]),
        github_expectation(:get, "/repos/acme/symmetry/actions/runs?per_page=1", %{
          "workflow_runs" => [github_workflow_run()]
        }),
        github_expectation(:get, "head_sha=gh-head-42", %{
          "workflow_runs" => [github_workflow_run()]
        })
      ]
    }
  end

  defp mixed_provider_case do
    description = "[symmetry-fake-agent:provider_action_flow]\nKeep Azure Boards authoritative."

    %{
      provider: "azure-github",
      work_provider: "azure_devops",
      repository_provider: "github",
      ci_provider: "azure_devops",
      repository_ref: "acme/symmetry",
      work_tracking_ref: "Platform",
      ci_ref: "Platform/pipeline/17",
      external_id: "77",
      external_state: "Active",
      title: "Ship connected Azure work",
      description: description,
      priority: "urgent",
      external_assignee_name: "Grace Hopper",
      branch: "codex/provider-e2e-mixed",
      pull_request_url: "https://github.com/acme/symmetry/pull/42",
      expectations: [
        github_expectation(:get, "/repos/acme/symmetry", %{
          "html_url" => "https://github.com/acme/symmetry",
          "default_branch" => "main",
          "visibility" => "private",
          "archived" => false
        }),
        azure_expectation(:post, "/acme/Platform/_apis/wit/wiql?", %{
          "workItems" => [%{"id" => 77}]
        }),
        azure_expectation(:post, "/acme/Platform/_apis/wit/workitemsbatch?", %{
          "value" => [
            %{
              "id" => 77,
              "rev" => 7,
              "fields" => %{
                "System.Title" => "Ship connected Azure work",
                "System.Description" => description,
                "System.State" => "Active",
                "System.Tags" => "agent-ready; priority:urgent",
                "System.AssignedTo" => %{"displayName" => "Grace Hopper"},
                "System.ChangedDate" => "2026-09-05T10:00:00Z",
                "System.WorkItemType" => "User Story"
              },
              "_links" => %{
                "html" => %{"href" => "https://dev.azure.com/acme/Platform/_workitems/edit/77"}
              }
            }
          ]
        }),
        github_expectation(:get, "/repos/acme/symmetry/pulls?", []),
        github_expectation(
          :post,
          "/repos/acme/symmetry/pulls",
          github_pull_request(),
          &assert_mixed_github_pull_request_body!/1
        ),
        github_expectation(:get, "/repos/acme/symmetry", %{
          "html_url" => "https://github.com/acme/symmetry",
          "default_branch" => "main",
          "visibility" => "private",
          "archived" => false
        }),
        github_expectation(:get, "/repos/acme/symmetry/pulls/42", github_pull_request()),
        github_expectation(:get, "/repos/acme/symmetry/pulls/42/reviews?", [
          %{"user" => %{"login" => "reviewer"}, "state" => "APPROVED"}
        ]),
        azure_expectation(:get, "/_apis/build/definitions/17?", %{
          "id" => 17,
          "name" => "symmetry-ci",
          "repository" => %{"type" => "GitHub", "name" => "acme/symmetry"}
        }),
        azure_expectation(:get, "/_apis/build/builds?", %{
          "value" => [azure_build("gh-head-42")]
        }),
        azure_expectation(:get, "/_apis/build/builds?", %{
          "value" => [azure_build("gh-head-42")]
        })
      ]
    }
  end

  defp azure_devops_case do
    description = "[symmetry-fake-agent:provider_action_flow]\nKeep Azure Boards authoritative."

    %{
      provider: "azure_devops",
      work_provider: "azure_devops",
      repository_provider: "azure_devops",
      ci_provider: "azure_devops",
      repository_ref: "Platform/symmetry",
      work_tracking_ref: "Platform",
      ci_ref: "Platform/pipeline/17",
      external_id: "77",
      external_state: "Active",
      title: "Ship connected Azure work",
      description: description,
      priority: "urgent",
      external_assignee_name: "Grace Hopper",
      branch: "codex/provider-e2e-azure",
      pull_request_url: "https://dev.azure.com/acme/Platform/_git/symmetry/pullrequest/84",
      expectations: [
        azure_expectation(:get, "/acme/Platform/_apis/git/repositories/symmetry?", %{
          "id" => "repo-123",
          "name" => "symmetry",
          "webUrl" => "https://dev.azure.com/acme/Platform/_git/symmetry",
          "defaultBranch" => "refs/heads/main"
        }),
        azure_expectation(:post, "/acme/Platform/_apis/wit/wiql?", %{
          "workItems" => [%{"id" => 77}]
        }),
        azure_expectation(:post, "/acme/Platform/_apis/wit/workitemsbatch?", %{
          "value" => [
            %{
              "id" => 77,
              "rev" => 7,
              "fields" => %{
                "System.Title" => "Ship connected Azure work",
                "System.Description" => description,
                "System.State" => "Active",
                "System.Tags" => "agent-ready; priority:urgent",
                "System.AssignedTo" => %{"displayName" => "Grace Hopper"},
                "System.ChangedDate" => "2026-09-05T10:00:00Z",
                "System.WorkItemType" => "User Story"
              },
              "_links" => %{
                "html" => %{"href" => "https://dev.azure.com/acme/Platform/_workitems/edit/77"}
              }
            }
          ]
        }),
        azure_expectation(:get, "/pullrequests?", %{"value" => []}),
        azure_expectation(
          :post,
          "/pullrequests?",
          azure_pull_request(),
          &assert_azure_pull_request_body!/1
        ),
        azure_expectation(:get, "/acme/Platform/_apis/git/repositories/symmetry?", %{
          "id" => "repo-123",
          "name" => "symmetry",
          "webUrl" => "https://dev.azure.com/acme/Platform/_git/symmetry",
          "defaultBranch" => "refs/heads/main"
        }),
        azure_expectation(:get, "/pullrequests/84?", azure_pull_request()),
        azure_expectation(:get, "/_apis/build/definitions/17?", %{
          "id" => 17,
          "name" => "symmetry-ci",
          "repository" => %{"type" => "TfsGit", "id" => "repo-123"}
        }),
        azure_expectation(:get, "/_apis/build/builds?", %{"value" => [azure_build()]}),
        azure_expectation(:get, "/_apis/build/builds?", %{"value" => [azure_build()]})
      ]
    }
  end

  defp github_expectation(method, path, response, body_assertion \\ nil) do
    %{
      method: method,
      url_contains: path,
      headers: %{
        "authorization" => "Bearer gho_github-token",
        "x-github-api-version" => "2026-03-10"
      },
      body_assertion: body_assertion,
      response: {:ok, success_status(method), %{}, response}
    }
  end

  defp azure_expectation(method, path, response, body_assertion \\ nil) do
    %{
      method: method,
      url_contains: path,
      headers: %{"authorization" => "Bearer azure-token"},
      body_assertion: body_assertion,
      response: {:ok, success_status(method), %{}, response}
    }
  end

  defp github_pull_request do
    %{
      "number" => 42,
      "html_url" => "https://github.com/acme/symmetry/pull/42",
      "state" => "open",
      "merged_at" => nil,
      "head" => %{"sha" => "gh-head-42"}
    }
  end

  defp github_workflow_run do
    %{
      "id" => 901,
      "workflow_id" => 9,
      "name" => "CI",
      "status" => "completed",
      "conclusion" => "success",
      "head_sha" => "gh-head-42",
      "html_url" => "https://github.com/acme/symmetry/actions/runs/901"
    }
  end

  defp azure_pull_request do
    %{
      "pullRequestId" => 84,
      "status" => "active",
      "sourceRefName" => "refs/heads/codex/provider-e2e-azure",
      "lastMergeSourceCommit" => %{"commitId" => "az-head-84"},
      "lastMergeCommit" => %{"commitId" => "az-merge-84"},
      "reviewers" => [%{"displayName" => "Reviewer", "vote" => 10}],
      "_links" => %{
        "web" => %{
          "href" => "https://dev.azure.com/acme/Platform/_git/symmetry/pullrequest/84"
        }
      }
    }
  end

  defp azure_build(source_version \\ "az-head-84") do
    %{
      "id" => 701,
      "status" => "completed",
      "result" => "succeeded",
      "sourceVersion" => source_version,
      "definition" => %{"id" => 17, "name" => "symmetry-ci"},
      "_links" => %{
        "web" => %{"href" => "https://dev.azure.com/acme/Platform/_build/results?buildId=701"}
      }
    }
  end

  defp assert_github_pull_request_body!(body) do
    expected = %{
      "head" => "codex/provider-e2e-github",
      "base" => "main",
      "title" => "Provider broker end-to-end"
    }

    unless body == expected, do: raise("unexpected GitHub pull request body: #{inspect(body)}")
  end

  defp assert_mixed_github_pull_request_body!(body) do
    expected = %{
      "head" => "codex/provider-e2e-mixed",
      "base" => "main",
      "title" => "Provider broker end-to-end"
    }

    unless body == expected, do: raise("unexpected GitHub pull request body: #{inspect(body)}")
  end

  defp assert_azure_pull_request_body!(body) do
    expected = %{
      "sourceRefName" => "refs/heads/codex/provider-e2e-azure",
      "targetRefName" => "refs/heads/main",
      "title" => "Provider broker end-to-end"
    }

    unless body == expected, do: raise("unexpected Azure pull request body: #{inspect(body)}")
  end

  defp auth_type("github"), do: "gh_cli"
  defp auth_type("azure_devops"), do: "entra_id"

  defp success_status(:post), do: 201
  defp success_status(_method), do: 200

  defp build_go_binary!(daemon_dir, build_dir, name) do
    package = "./cmd/" <> Path.rootname(name)
    path = Path.join(build_dir, name)

    case System.cmd("go", ["build", "-o", path, package],
           cd: daemon_dir,
           stderr_to_stdout: true
         ) do
      {_output, 0} -> path
      {output, status} -> raise "go build #{package} failed (#{status}):\n#{output}"
    end
  end

  defp start_daemon!(executable, config_path) do
    {spawn_executable, arguments} = daemon_command(executable, config_path)

    port =
      Port.open(
        {:spawn_executable, String.to_charlist(spawn_executable)},
        [
          :binary,
          :exit_status,
          :stderr_to_stdout,
          args: arguments,
          env: [{~c"SYMMETRY_ENROLLMENT_TOKEN", ~c"test-enrollment-token"}]
        ]
      )

    case Port.info(port, :os_pid) do
      {:os_pid, os_pid} when is_integer(os_pid) and os_pid > 0 ->
        session_id =
          case :os.type() do
            {:win32, _name} -> nil
            _unix -> await_isolated_session_id!(os_pid, 2_000)
          end

        %{port: port, os_pid: os_pid, session_id: session_id}

      _missing ->
        raise "daemon port did not expose an OS process ID"
    end
  end

  defp daemon_command(executable, config_path) do
    case :os.type() do
      {:win32, _name} ->
        {executable, ["-config", config_path]}

      _unix ->
        setsid =
          System.find_executable("setsid") ||
            raise "provider broker E2E requires setsid on Unix"

        {setsid, ["--wait", executable, "-config", config_path]}
    end
  end

  defp stop_daemon!(%{port: port, os_pid: os_pid, session_id: nil}) do
    if os_process_alive?(os_pid), do: terminate_windows_process_tree(os_pid)

    unless await_process_exit(os_pid, 5_000),
      do: raise("daemon process tree #{os_pid} did not terminate")

    close_port(port)
  end

  defp stop_daemon!(%{port: port, os_pid: os_pid, session_id: session_id}) do
    terminate_unix_session(session_id, :term)

    unless await_unix_shutdown(session_id, os_pid, 5_000) do
      terminate_unix_session(session_id, :kill)
      if os_process_alive?(os_pid), do: signal_unix_process(os_pid, :kill)

      unless await_unix_shutdown(session_id, os_pid, 5_000),
        do: raise("daemon session #{session_id} did not terminate")
    end

    close_port(port)
  end

  defp terminate_windows_process_tree(os_pid) do
    System.cmd("taskkill", ["/PID", Integer.to_string(os_pid), "/T", "/F"],
      stderr_to_stdout: true
    )

    :ok
  end

  defp await_isolated_session_id!(os_pid, timeout) do
    beam_pid = System.pid() |> String.to_integer()
    beam_session_id = unix_process_info(beam_pid).session_id
    deadline = System.monotonic_time(:millisecond) + timeout
    await_isolated_session_id!(os_pid, beam_session_id, deadline)
  end

  defp await_isolated_session_id!(os_pid, beam_session_id, deadline) do
    processes = unix_processes()
    children = Enum.group_by(Map.values(processes), & &1.parent_pid, & &1.pid)

    session_id =
      [os_pid | descendants(os_pid, children)]
      |> Enum.map(&Map.get(processes, &1))
      |> Enum.reject(&is_nil/1)
      |> Enum.map(& &1.session_id)
      |> Enum.find(&(&1 != beam_session_id))

    cond do
      is_integer(session_id) ->
        session_id

      System.monotonic_time(:millisecond) >= deadline ->
        raise "daemon did not enter an isolated Unix session"

      true ->
        Process.sleep(10)
        await_isolated_session_id!(os_pid, beam_session_id, deadline)
    end
  end

  defp unix_processes do
    "/proc/*/stat"
    |> Path.wildcard()
    |> Enum.reduce(%{}, &collect_unix_process/2)
  end

  defp collect_unix_process(path, processes) do
    with {pid, ""} <- path |> Path.dirname() |> Path.basename() |> Integer.parse(),
         {:ok, stat} <- File.read(path),
         [_name, parent_pid, _process_group_id, session_id] <-
           Regex.run(
             ~r/^\d+ \((.*)\) \S (\d+) (\d+) (\d+) /,
             stat,
             capture: :all_but_first
           ),
         {parent_pid, ""} <- Integer.parse(parent_pid),
         {session_id, ""} <- Integer.parse(session_id) do
      Map.put(processes, pid, %{pid: pid, parent_pid: parent_pid, session_id: session_id})
    else
      _unavailable -> processes
    end
  end

  defp descendants(parent_pid, children) do
    Enum.flat_map(Map.get(children, parent_pid, []), fn child_pid ->
      descendants(child_pid, children) ++ [child_pid]
    end)
  end

  defp await_process_exit(os_pid, timeout) do
    deadline = System.monotonic_time(:millisecond) + timeout
    await_process_exit_loop(os_pid, deadline)
  end

  defp await_process_exit_loop(os_pid, deadline) do
    cond do
      not os_process_alive?(os_pid) ->
        true

      System.monotonic_time(:millisecond) >= deadline ->
        false

      true ->
        Process.sleep(25)
        await_process_exit_loop(os_pid, deadline)
    end
  end

  defp terminate_unix_session(session_id, signal) do
    session_id
    |> unix_session_processes()
    |> Enum.each(&signal_unix_process(&1, signal))
  end

  defp unix_session_processes(session_id) do
    unix_processes()
    |> Map.values()
    |> Enum.filter(&(&1.session_id == session_id))
    |> Enum.map(& &1.pid)
  end

  defp await_unix_shutdown(session_id, os_pid, timeout) do
    deadline = System.monotonic_time(:millisecond) + timeout
    await_unix_shutdown_loop(session_id, os_pid, deadline)
  end

  defp await_unix_shutdown_loop(session_id, os_pid, deadline) do
    cond do
      unix_session_processes(session_id) == [] and not os_process_alive?(os_pid) ->
        true

      System.monotonic_time(:millisecond) >= deadline ->
        false

      true ->
        Process.sleep(25)
        await_unix_shutdown_loop(session_id, os_pid, deadline)
    end
  end

  defp os_process_alive?(os_pid) do
    case :os.type() do
      {:win32, _name} ->
        case System.cmd(
               "tasklist",
               ["/FI", "PID eq #{os_pid}", "/FO", "CSV", "/NH"],
               stderr_to_stdout: true
             ) do
          {output, 0} -> String.contains?(output, "\"#{os_pid}\"")
          {_output, _status} -> false
        end

      _unix ->
        if File.dir?("/proc"),
          do: File.dir?("/proc/#{os_pid}"),
          else: unix_process_alive?(os_pid)
    end
  end

  defp signal_unix_process(os_pid, signal) do
    System.cmd(
      "sh",
      [
        "-c",
        ~S|kill "$1" "$2"|,
        "kill",
        unix_signal(signal),
        Integer.to_string(os_pid)
      ],
      stderr_to_stdout: true
    )
  end

  defp unix_process_info(os_pid) do
    Map.get(unix_processes(), os_pid) || raise "Unix process #{os_pid} disappeared"
  end

  defp unix_process_alive?(os_pid) do
    match?(
      {_output, 0},
      System.cmd(
        "sh",
        ["-c", ~S|kill -0 "$1"|, "kill", Integer.to_string(os_pid)],
        stderr_to_stdout: true
      )
    )
  end

  defp close_port(port) do
    if Port.info(port) do
      try do
        Port.close(port)
      rescue
        ArgumentError -> :ok
      end
    end

    await_port_closed(port, System.monotonic_time(:millisecond) + 1_000)
  end

  defp await_port_closed(port, deadline) do
    if Port.info(port) && System.monotonic_time(:millisecond) < deadline do
      Process.sleep(10)
      await_port_closed(port, deadline)
    else
      :ok
    end
  end

  defp unix_signal(:term), do: "-TERM"
  defp unix_signal(:kill), do: "-KILL"

  defp await!(daemon_port, description, assertion, timeout \\ 20_000) do
    deadline = System.monotonic_time(:millisecond) + timeout
    await_loop!(daemon_port, description, assertion, deadline, "")
  end

  defp await_loop!(daemon_port, description, assertion, deadline, output) do
    case assertion.() do
      :ok ->
        :ok

      {:ok, value} ->
        value

      :retry ->
        if System.monotonic_time(:millisecond) >= deadline do
          flunk("timed out waiting for #{description}; daemon output:\n#{output}")
        end

        receive do
          {^daemon_port, {:data, data}} ->
            await_loop!(daemon_port, description, assertion, deadline, output <> data)

          {^daemon_port, {:exit_status, status}} ->
            flunk(
              "daemon exited with status #{status} while waiting for #{description}:\n#{output}"
            )
        after
          50 -> await_loop!(daemon_port, description, assertion, deadline, output)
        end
    end
  end

  defp portal_conn do
    Phoenix.ConnTest.build_conn()
    |> init_test_session(%{portal_operator: PortalSession.issue("test-operator-token")})
    |> put_private(:plug_skip_csrf_protection, true)
    |> put_req_header("accept", "application/json")
  end

  defp restore_env(key, nil), do: Application.delete_env(:symmetry_control, key)
  defp restore_env(key, value), do: Application.put_env(:symmetry_control, key, value)
end
