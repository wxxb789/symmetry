defmodule SymmetryControl.ProviderBrokerE2ETest do
  use SymmetryControlWeb.ConnCase, async: false

  import Ecto.Query

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.{Goal, GoalDecision, HarnessSession, RunEvidence, WorkOutcome}
  alias SymmetryControl.Integrations
  alias SymmetryControl.Integrations.HTTPStub
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Run, RunTransition, Runtime, Task}
  alias SymmetryControl.RequestHash
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

  setup context do
    daemon = context.daemon
    agent = context.agent
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

    ingress_recorder =
      if context[:artifact_validation_e2e] do
        start_supervised!({Agent, fn -> %{next_order: 0, events: []} end})
      end

    listener_plug =
      if ingress_recorder do
        {&recording_endpoint_plug/2, ingress_recorder}
      else
        SymmetryControlWeb.Endpoint
      end

    listener =
      start_supervised!(
        {Bandit,
         plug: listener_plug, scheme: :http, ip: {127, 0, 0, 1}, port: 0, startup_log: false}
      )

    {:ok, {{127, 0, 0, 1}, port}} = ThousandIsland.listener_info(listener)
    runtime_key = "provider-e2e-#{Ecto.UUID.generate()}"
    profile = "provider-e2e"
    workspace = "provider-e2e"
    artifact = if context[:artifact_validation_e2e], do: create_artifact_repository_fixture!()
    run_dir = Path.join(System.tmp_dir!(), "symmetry-provider-broker-run-#{Ecto.UUID.generate()}")
    state_dir = Path.join(run_dir, "state")
    workspace_path = Path.join(run_dir, "workspace")
    File.mkdir_p!(state_dir)
    File.mkdir_p!(workspace_path)

    config_path = Path.join(run_dir, "daemon.json")

    workspace_config =
      case artifact do
        %{repository_path: repository_path, subject: %{"commit" => commit}} ->
          %{
            workspace => %{
              policy: "git_worktree",
              repository: repository_path,
              root: workspace_path,
              ref: commit,
              cleanup: "never"
            }
          }

        nil ->
          %{workspace => %{policy: "existing_checkout", path: workspace_path, cleanup: "never"}}
      end

    runtime_config = %{
      runtime_key: runtime_key,
      name: "Provider broker E2E",
      capacity: 1,
      agent_profile: profile,
      workspace: workspace
    }

    runtime_config =
      case artifact do
        %{repository_resource_id: repository_resource_id} ->
          Map.put(runtime_config, :repository_resource_id, repository_resource_id)

        nil ->
          runtime_config
      end

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
        workspaces: workspace_config,
        runtime: runtime_config
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

    {:ok,
     daemon_port: daemon_port,
     profile: profile,
     workspace: workspace,
     runtime_key: runtime_key,
     artifact: artifact,
     ingress_recorder: ingress_recorder}
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

  @tag artifact_validation_e2e: true
  test "artifact-only Goal validation uses the fixed Git object without a native session",
       context do
    %{artifact: artifact} = context
    runtime = Repo.get_by!(Runtime, runtime_key: context.runtime_key)
    # These are fixture preconditions for this focused seam. The producer
    # terminal is seeded in Control, and the runtime capability projection is
    # artificial so Control can assign the Goal task; neither is native evidence.
    install_artificial_runtime_projection_for_assignment!(runtime)

    fixture = create_artifact_goal_fixture!(artifact, context.profile, runtime)

    assert {:ok, producer_settlement} =
             Goals.settle_task(
               fixture.producer.id,
               fixture.producer_run.id,
               fixture.producer_run.generation
             )

    assert producer_settlement["settlement"] == "awaiting_validation"

    assert {:ok, admission, :created} =
             command_current(
               fixture.goal.id,
               "admit_task",
               %{
                 work_item_id: fixture.item.id,
                 purpose: "validate",
                 model_profile: context.profile,
                 session_mode: "fresh",
                 requested_session_id: nil,
                 validation_of_task_id: fixture.producer.id
               },
               rollout_enabled: true
             )

    validation_task = Repo.get!(Task, admission.response["task"]["id"])
    assert validation_task.input["subject"] == artifact.subject
    assert validation_task.input["requested_session_id"] == nil
    assert validation_task.input["provider_scope"] == nil

    # Freeze the Subject first, then make the repository checkout dirty. The
    # daemon must read proof.txt from the original Git object, not this file.
    dirty_content = "uncommitted checkout bytes must not be accepted\n"
    File.write!(Path.join(artifact.repository_path, "proof.txt"), dirty_content)
    refute digest!(dirty_content) == artifact.content_digest

    assert {:ok, _snapshot} = Orchestration.heartbeat(runtime.id, runtime.connection_epoch, [])
    assert {:ok, validation_run} = Orchestration.assign_one()
    assert validation_run.task_id == validation_task.id
    assert validation_run.runtime_id == runtime.id
    assert validation_run.generation == validation_task.attempt_generation

    await!(context.daemon_port, "artifact-only validation terminal delivery", fn ->
      case Repo.get(Task, validation_task.id) do
        %Task{state: "completed"} -> :ok
        _task -> :retry
      end
    end)

    validation_task = Repo.get!(Task, validation_task.id)
    validation_run = Repo.get!(Run, validation_run.id)
    subject = artifact.subject
    assert validation_run.provider_access_snapshot == %{"v" => 1, "kind" => "none"}

    assert is_nil(validation_run.harness_session_id)
    assert is_nil(validation_run.harness_binding_id)

    assert %{
             "schema_version" => "symmetry.task_result.v1",
             "kind" => "candidate_completion",
             "subject" => ^subject,
             "subject_hash" => subject_hash,
             "evidence_refs" => [evidence_id]
           } = validation_task.result["task_result"]

    assert subject_hash == subject_hash!(artifact.subject)

    evidence = Repo.get!(RunEvidence, evidence_id)
    assert evidence.run_id == validation_run.id
    assert evidence.kind == "artifact"
    assert evidence.evidence_key == artifact_evidence_key("proof")
    assert evidence.validator_profile == "artifact"
    assert evidence.verdict == "passed"
    assert evidence.subject_hash == RequestHash.canonical(artifact.subject)

    assert evidence.source_ref == %{
             "kind" => "artifact",
             "ref" => artifact_evidence_key("proof"),
             "resource_id" => artifact.repository_resource_id,
             "commit" => artifact.subject["commit"],
             "path" => "proof.txt",
             "subject_hash" => subject_hash
           }

    assert evidence.payload == %{
             "predicate_id" => "proof",
             "subject" => artifact.subject,
             "subject_hash" => subject_hash,
             "resource_id" => artifact.repository_resource_id,
             "commit" => artifact.subject["commit"],
             "path" => "proof.txt",
             "content_digest" => artifact.content_digest
           }

    transition =
      Repo.one!(
        from transition in RunTransition,
          where: transition.run_id == ^validation_run.id and transition.state == "completed"
      )

    assert DateTime.compare(evidence.inserted_at, transition.inserted_at) == :lt
    assert_artifact_ingress_order!(context.ingress_recorder, validation_run.id)

    assert {:ok, accepted} =
             Goals.settle_task(validation_task.id, validation_run.id, validation_run.generation)

    assert accepted["settlement"] == "accepted"
    assert accepted["subject_hash"] == subject_hash
    assert accepted["evidence_refs"] == [evidence_id]

    outcome =
      Repo.get_by!(WorkOutcome,
        validation_task_id: validation_task.id,
        work_item_id: fixture.item.id,
        goal_revision: fixture.goal.current_revision
      )

    assert outcome.disposition == "accepted"
    assert outcome.reason == "validation_passed"
    assert outcome.candidate_subject == artifact.subject
    assert outcome.subject_hash == RequestHash.canonical(artifact.subject)
    assert outcome.evidence_ids == [evidence_id]
    assert outcome.producing_task_id == fixture.producer.id
    assert outcome.producing_run_id == fixture.producer_run.id

    refute Repo.exists?(
             from session in HarnessSession, where: session.active_run_id == ^validation_run.id
           )
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

  defp create_artifact_repository_fixture! do
    suffix = Ecto.UUID.generate() |> String.replace("-", "")

    repository_path =
      Path.join(System.tmp_dir!(), "symmetry-provider-artifact-repository-#{suffix}")

    File.rm_rf!(repository_path)
    File.mkdir_p!(repository_path)
    on_exit(fn -> File.rm_rf!(repository_path) end)

    run_git!(repository_path, ["init", "--quiet"])
    run_git!(repository_path, ["config", "user.email", "symmetry-e2e@example.test"])
    run_git!(repository_path, ["config", "user.name", "Symmetry E2E"])

    content = "artifact accepted from the immutable Git object\n"
    File.write!(Path.join(repository_path, "proof.txt"), content)
    run_git!(repository_path, ["add", "proof.txt"])
    run_git!(repository_path, ["commit", "--quiet", "-m", "initial artifact subject"])

    commit = git_output!(repository_path, ["rev-parse", "HEAD"]) |> String.trim()

    {:ok, project} =
      Workspaces.create_project(%{
        name: "Artifact validation #{suffix}",
        key: "A#{String.slice(suffix, 0, 7)}",
        default_agent_profile: "provider-e2e",
        default_workspace: "provider-e2e"
      })

    {:ok, repository} =
      Workspaces.create_resource(project.id, %{
        kind: "repository",
        name: "Artifact repository #{suffix}"
      })

    subject = %{
      "resource_id" => repository.id,
      "commit" => commit,
      "tree_digest" => git_tree_digest!(repository_path, commit)
    }

    %{
      project_id: project.id,
      repository_resource_id: repository.id,
      repository_path: repository_path,
      subject: subject,
      content_digest: digest!(content)
    }
  end

  defp create_artifact_goal_fixture!(artifact, profile, runtime) do
    acceptance = artifact_acceptance_contract(artifact.repository_resource_id)

    assert {:ok, created, :created} =
             Goals.create_goal(
               artifact.project_id,
               artifact_goal_attrs(artifact.repository_resource_id, profile, acceptance),
               "operator:test",
               validation_profiles: []
             )

    goal_id = created.goal.id

    proposal = %{
      schema_version: "symmetry.plan.v1",
      proposal_id: Ecto.UUID.generate(),
      goal_id: goal_id,
      expected_revision: 1,
      items: [
        %{
          key: "implement",
          title: "Produce artifact",
          description: "Produce the candidate committed artifact.",
          required: true,
          integration: true,
          repository_resource_id: artifact.repository_resource_id,
          acceptance: acceptance,
          depends_on_keys: [],
          model_profile: profile,
          change_target: nil,
          baseline: %{kind: "subject", subject: artifact.subject}
        }
      ]
    }

    assert {:ok, decision, :created} =
             command_current(goal_id, "request_decision", %{
               kind: "plan",
               work_item_id: nil,
               subject_hash: nil,
               proposal: proposal
             })

    decision_id = decision.response["decision"]["id"]
    decision_version = Repo.get!(GoalDecision, decision_id).lock_version

    assert {:ok, _resolved, :created} =
             command_current(goal_id, "resolve_decision", %{
               decision_id: decision_id,
               expected_decision_version: decision_version,
               option_id: "accept"
             })

    proposal_hash = "sha256:" <> Base.encode16(RequestHash.canonical(proposal), case: :lower)

    assert {:ok, _planned, :created} =
             command_current(
               goal_id,
               "accept_plan",
               %{proposal: proposal, proposal_hash: proposal_hash, decision_id: decision_id},
               rollout_enabled: true
             )

    assert {:ok, _active, :created} =
             command_current(goal_id, "activate", %{approved_revision: 1})

    assert {:ok, %{work_items: [item]}} = Goals.fetch_goal(goal_id)

    assert {:ok, producer_receipt, :created} =
             command_current(
               goal_id,
               "admit_task",
               %{
                 work_item_id: item.id,
                 purpose: "implement",
                 model_profile: profile,
                 session_mode: "fresh",
                 requested_session_id: nil,
                 validation_of_task_id: nil
               },
               rollout_enabled: true
             )

    producer = Repo.get!(Task, producer_receipt.response["task"]["id"])
    producer_result = candidate_completion_result(artifact.subject)
    now = DateTime.utc_now() |> DateTime.truncate(:microsecond)

    # The producer terminal is a Control-side fixture precondition for this
    # validation seam; it is not claimed as live native execution evidence.
    producer =
      producer
      |> Task.changeset(%{
        state: "completed",
        current_generation: 1,
        result: %{"task_result" => producer_result}
      })
      |> Repo.update!()

    producer_run =
      %Run{}
      |> Run.changeset(%{
        task_id: producer.id,
        runtime_id: runtime.id,
        generation: 1,
        state: "completed",
        claimed_runtime_epoch: runtime.connection_epoch,
        claim_id: Ecto.UUID.generate(),
        lease_token: Ecto.UUID.generate(),
        assigned_at: now,
        assignment_expires_at: DateTime.add(now, 60, :second),
        claimed_at: now,
        lease_expires_at: DateTime.add(now, 60, :second),
        result: %{"task_result" => producer_result}
      })
      |> Repo.insert!()

    %{goal: Repo.get!(Goal, goal_id), item: item, producer: producer, producer_run: producer_run}
  end

  defp artifact_goal_attrs(repository_resource_id, profile, acceptance) do
    %{
      schema_version: "symmetry.goal_create.v1",
      title: "Verify committed artifact #{System.unique_integer([:positive])}",
      mutation_id: Ecto.UUID.generate(),
      initial_revision: %{
        objective: "Verify a candidate from an exact committed Git object.",
        non_goals: [],
        acceptance_contract: acceptance,
        authority_policy: %{
          "operator_required_for_scope_change" => true,
          "operator_required_for_completion" => true,
          "publication_allowed" => false,
          "allowed_actions" => []
        },
        execution_policy: %{
          "automatic_execution" => false,
          "max_parallel_tasks" => 1,
          "max_task_admissions" => 2,
          "max_run_attempts_per_task" => 2,
          "budget_limit_microusd" => nil,
          "per_run_cost_limit_microusd" => nil,
          "budget_mode" => "soft",
          "hard_cost_limit_required" => false,
          "allowed_runtime_ids" => [],
          "allowed_resource_ids" => [repository_resource_id],
          "allowed_model_profiles" => [profile],
          "final_acceptance" => "operator",
          "allowed_actions" => []
        },
        context_manifest: %{
          "byte_budget" => 32_768,
          "required_source_kinds" => ["approved_goal", "work_contract", "repository_subject"],
          "include_advisory_recall" => false
        },
        reason: "Artifact validation E2E contract"
      }
    }
  end

  defp artifact_acceptance_contract(repository_resource_id) do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "The committed proof artifact must be present.",
      "predicates" => [
        %{
          "id" => "proof",
          "kind" => "artifact",
          "resource_id" => repository_resource_id,
          "path" => "proof.txt"
        }
      ]
    }
  end

  defp candidate_completion_result(subject) do
    %{
      "schema_version" => "symmetry.task_result.v1",
      "result_id" => Ecto.UUID.generate(),
      "kind" => "candidate_completion",
      "summary" => "Candidate completion for the admitted artifact subject.",
      "subject" => subject,
      "subject_hash" => subject_hash!(subject),
      "evidence_refs" => [],
      "blocker" => nil,
      "proposed_next_action" => nil,
      "proposal" => nil,
      "reason" => nil,
      "diagnostics" => []
    }
  end

  defp command_current(goal_id, kind, payload, opts \\ []) do
    assert {:ok, goal} = Goals.fetch_goal(goal_id)

    Goals.command(
      goal_id,
      %{
        schema_version: "symmetry.goal_command.v1",
        mutation_id: Ecto.UUID.generate(),
        expected_version: goal.version,
        expected_revision: goal.current_revision,
        kind: kind,
        payload: payload
      },
      "operator:test",
      Keyword.merge([rollout_enabled: true], opts)
    )
  end

  defp install_artificial_runtime_projection_for_assignment!(runtime) do
    # Control's Goal scheduler requires a native-shaped capability projection
    # even when the selected validation path must launch no native session.
    # This test-only projection is an assignment precondition, not native
    # capability evidence and is never used to start an adapter.
    native_version = "artifact-e2e"

    capabilities = %{
      "structured_input" => true,
      "provider_access" => false,
      "interactive" => false,
      "supervisory_control" => false,
      "adapter" => %{
        "kind" => "opencode",
        "native_version" => native_version,
        "implementation_version" => native_version,
        "protocol_version" => 1,
        "operations" => %{
          "start" => true,
          "events" => true,
          "cancel" => true,
          "resume" => false,
          "handoff" => false,
          "guidance" => "unsupported",
          "pause" => "unsupported",
          "approval_response" => false,
          "usage" => "unknown",
          "hard_cost_limit" => false
        }
      }
    }

    Repo.update_all(
      from(runtime_row in Runtime, where: runtime_row.id == ^runtime.id),
      set: [
        harness_kind: "opencode",
        harness_version: native_version,
        adapter_version: native_version,
        adapter_protocol_version: 1,
        capabilities: capabilities
      ]
    )
  end

  defp recording_endpoint_plug(conn, recorder) do
    ingress = record_request_ingress!(recorder, conn)

    conn =
      Plug.Conn.register_before_send(conn, fn response ->
        record_request_response!(recorder, ingress, response)
        response
      end)

    SymmetryControlWeb.Endpoint.call(conn, SymmetryControlWeb.Endpoint.init([]))
  end

  defp record_request_ingress!(recorder, conn) do
    case recorder_request_kind(conn.method, conn.request_path) do
      nil ->
        nil

      {kind, run_id} ->
        evidence_present_at_ingress =
          if kind == :transition do
            Repo.exists?(from evidence in RunEvidence, where: evidence.run_id == ^run_id)
          end

        Agent.get_and_update(recorder, fn state ->
          ingress_order = state.next_order + 1

          event = %{
            ingress_order: ingress_order,
            kind: kind,
            method: conn.method,
            path: conn.request_path,
            run_id: run_id,
            status: nil,
            completed: false,
            evidence_present_at_ingress: evidence_present_at_ingress
          }

          {ingress_order, %{state | next_order: ingress_order, events: [event | state.events]}}
        end)
    end
  end

  defp record_request_response!(_recorder, nil, _conn), do: :ok

  defp record_request_response!(recorder, ingress_order, conn) do
    completed =
      case conn.body_params do
        params when is_map(params) -> Map.get(params, "state") == "completed"
        _unparsed -> false
      end

    Agent.update(recorder, fn state ->
      events =
        Enum.map(state.events, fn event ->
          if event.ingress_order == ingress_order,
            do: %{event | status: conn.status, completed: completed},
            else: event
        end)

      %{state | events: events}
    end)
  end

  defp recorder_request_kind("POST", path) do
    case Regex.run(~r{\A/api/v1/runs/([^/]+)/evidence\z}, path) do
      [_, run_id] -> {:evidence, run_id}
      _ -> nil
    end
  end

  defp recorder_request_kind("PUT", path) do
    case Regex.run(~r{\A/api/v1/runs/([^/]+)/transitions/[^/]+\z}, path) do
      [_, run_id] -> {:transition, run_id}
      _ -> nil
    end
  end

  defp recorder_request_kind(_method, _path), do: nil

  defp assert_artifact_ingress_order!(recorder, run_id) do
    events = Agent.get(recorder, fn state -> Enum.reverse(state.events) end)

    evidence_events =
      Enum.filter(events, fn event ->
        event.kind == :evidence and event.run_id == run_id
      end)

    terminal_events =
      Enum.filter(events, fn event ->
        event.kind == :transition and event.run_id == run_id and event.completed
      end)

    assert [%{method: "POST", path: evidence_path, status: evidence_status} = evidence_event] =
             evidence_events

    assert String.ends_with?(evidence_path, "/evidence")
    assert evidence_status in [200, 201]

    assert [
             %{
               method: "PUT",
               path: terminal_path,
               status: 200,
               evidence_present_at_ingress: true
             } = terminal_event
           ] = terminal_events

    assert String.contains?(terminal_path, "/transitions/")
    assert evidence_event.ingress_order < terminal_event.ingress_order
  end

  defp subject_hash!(subject) do
    "sha256:" <> Base.encode16(RequestHash.canonical(subject), case: :lower)
  end

  defp artifact_evidence_key(predicate_id) do
    "artifact:" <> Base.encode16(:crypto.hash(:sha256, predicate_id), case: :lower)
  end

  defp digest!(content) do
    "sha256:" <> Base.encode16(:crypto.hash(:sha256, content), case: :lower)
  end

  defp git_tree_digest!(repository_path, commit) do
    entries =
      repository_path
      |> git_output_bytes!(["ls-tree", "--full-tree", "-r", "-z", commit])
      |> :binary.split(<<0>>, [:global])
      |> Enum.reject(&(&1 == <<>>))
      |> Enum.map(fn record ->
        [header, path] = :binary.split(record, <<9>>, [:global])
        [mode, type, object_id] = :binary.split(header, <<32>>, [:global])
        {path, mode, type, object_id}
      end)
      |> Enum.sort_by(&elem(&1, 0))

    manifest =
      entries
      |> Enum.flat_map(fn {path, mode, type, object_id} ->
        blob_digest =
          repository_path
          |> git_output_bytes!(["cat-file", "blob", object_id])
          |> digest!()

        [mode, <<0>>, type, <<0>>, path, <<0>>, blob_digest, <<0>>]
      end)
      |> IO.iodata_to_binary()

    digest!(manifest)
  end

  defp run_git!(directory, args) do
    case System.cmd("git", ["-C", directory | args], stderr_to_stdout: true) do
      {_output, 0} -> :ok
      {output, status} -> raise "git #{Enum.join(args, " ")} failed (#{status}): #{output}"
    end
  end

  defp git_output!(directory, args) do
    case System.cmd("git", ["-C", directory | args], stderr_to_stdout: true) do
      {output, 0} -> output
      {output, status} -> raise "git #{Enum.join(args, " ")} failed (#{status}): #{output}"
    end
  end

  defp git_output_bytes!(directory, args), do: git_output!(directory, args)

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
