defmodule SymmetryControl.Integrations.ProviderActionTestStub do
  @moduledoc false
  @behaviour SymmetryControl.Integrations.Provider

  import Ecto.Query

  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces.{ProjectResource, WorkItem}

  @impl true
  def authenticate(%{name: "Unavailable auth"}), do: {:error, :authentication_unavailable}
  def authenticate(_connection), do: {:ok, %{access_token: "provider-secret"}}

  @impl true
  def check(_connection, _auth), do: {:ok, %{}}

  @impl true
  def validate_resource_reference(_connection, _kind, _reference), do: :ok

  @impl true
  def sync_resource(connection, %{kind: "work_tracking"} = resource, _auth) do
    {:ok,
     %{
       resource: %{url: resource_url(connection, resource), metadata: %{}},
       work_items: [
         %{
           external_id: "101",
           external_url: resource_url(connection, resource) <> "/items/101",
           external_state: "open",
           external_updated_at: ~U[2026-09-06 08:00:00.000000Z],
           title: "Provider-backed work",
           description: "Exercise provider access.",
           labels: [],
           external_assignee_name: nil,
           status: "ready",
           priority: "high",
           provider_data: provider_work_item_data(connection.provider)
         }
       ]
     }}
  end

  def sync_resource(connection, resource, _auth) do
    {:ok,
     %{
       resource: %{
         url: resource_url(connection, resource),
         metadata: %{"default_branch" => "main"}
       },
       work_items: []
     }}
  end

  @impl true
  def sync_delivery(_connection, _resource, _work_item, _auth), do: {:ok, nil}

  @impl true
  def sync_ci(_connection, _resource, _work_item, _auth), do: {:error, :missing_ci_reference}

  @impl true
  def execute(connection, resource, work_item, operation, input, auth) do
    send(
      Application.fetch_env!(:symmetry_control, :provider_action_test_controller),
      {:provider_action, connection.provider, resource.id, work_item.id, operation, input, auth}
    )

    if input["title"] == "Recover original target" do
      send(
        Application.fetch_env!(:symmetry_control, :provider_action_test_controller),
        {:provider_action_target, connection.account_ref, resource.external_ref}
      )
    end

    cond do
      input["title"] == "Fail once" and
          :ets.insert_new(:provider_action_test_state, {{:failed, work_item.id}, true}) ->
        {:error, {:transport, :retryable}}

      input["title"] == "Fail once after resource race" and
          :ets.insert_new(:provider_action_test_state, {{:failed, work_item.id}, true}) ->
        from(stored in ProjectResource, where: stored.id == ^resource.id)
        |> Repo.update_all(inc: [lock_version: 1])

        {:error, {:transport, :retryable}}

      input["title"] == "Fail transport" ->
        {:error, {:transport, {:authorization, "Bearer provider-secret"}}}

      input["title"] == "Fail forbidden" ->
        {:error, :forbidden}

      input["title"] == "Fail HTTP 422" ->
        {:error, {:http, 422, "invalid request"}}

      input["title"] == "Fail HTTP 503" ->
        {:error, {:http, 503, "temporarily unavailable"}}

      input["title"] == "Invalid pull request URL" ->
        {:error, :invalid_pull_request_url}

      input["title"] == "Return invalid success" ->
        {:ok, :invalid_success}

      input["title"] == "Return credential" ->
        delivery(%{"authorization" => "Bearer provider-secret"})

      input["title"] == "Advance projection" ->
        from(item in WorkItem, where: item.id == ^work_item.id)
        |> Repo.update_all(inc: [lock_version: 1])

        delivery(%{"head_sha" => "abc123"})

      input["title"] == "Wait for cancel" ->
        send(
          Application.fetch_env!(:symmetry_control, :provider_action_test_controller),
          {:provider_action_waiting, self()}
        )

        receive do
          :continue -> delivery(%{"head_sha" => "abc123"})
        after
          5_000 -> {:error, {:transport, :test_timeout}}
        end

      true ->
        delivery(%{"head_sha" => "abc123"})
    end
  end

  defp delivery(provider_data),
    do:
      {:ok,
       %{
         pull_request_url: "https://github.com/acme/symmetry/pull/42",
         pull_request_state: "open",
         review_status: "required",
         updated_at: ~U[2026-09-06 08:05:00.000000Z],
         provider_data: provider_data
       }}

  defp resource_url(%{provider: "github"}, resource),
    do: "https://github.com/" <> resource.external_ref

  defp resource_url(%{provider: "azure_devops"}, resource),
    do: "https://dev.azure.com/acme/" <> resource.external_ref

  defp provider_work_item_data("azure_devops"), do: %{"revision" => 1}
  defp provider_work_item_data(_provider), do: %{}
end

defmodule SymmetryControlWeb.ProviderActionControllerTest do
  use SymmetryControlWeb.ConnCase, async: false

  import Ecto.Query
  import ExUnit.CaptureLog

  alias SymmetryControl.Integrations
  alias SymmetryControl.Integrations.{Connection, ProviderActionIntent}
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Run, Runtime, Task}
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.{ProjectResource, WorkItem}

  @enrollment_token "test-enrollment-token"

  setup %{conn: conn} do
    previous_providers = Application.get_env(:symmetry_control, :integration_providers)
    previous_controller = Application.get_env(:symmetry_control, :provider_action_test_controller)

    Application.put_env(:symmetry_control, :integration_providers,
      github: SymmetryControl.Integrations.ProviderActionTestStub,
      azure_devops: SymmetryControl.Integrations.ProviderActionTestStub
    )

    Application.put_env(:symmetry_control, :provider_action_test_controller, self())
    :ets.new(:provider_action_test_state, [:named_table, :public, :set])

    on_exit(fn ->
      restore_env(:integration_providers, previous_providers)
      restore_env(:provider_action_test_controller, previous_controller)
    end)

    {machine_id, machine_token} = enroll(conn)
    runtime_id = register(conn, machine_id, machine_token)
    project = project_fixture()

    github = connection_fixture("github", ["repositories", "changes"])
    azure = connection_fixture("azure_devops", ["work_items", "ci"])

    repository = resource_fixture(project.id, github.id, "repository", "acme/symmetry")
    tracker = resource_fixture(project.id, azure.id, "work_tracking", "symmetry/items")
    ci = resource_fixture(project.id, azure.id, "ci", "symmetry/pipelines/9")

    assert {:ok, repository} = Integrations.sync_resource(repository.id)
    assert {:ok, _tracker} = Integrations.sync_resource(tracker.id)
    item = Repo.get_by!(WorkItem, external_work_item_resource_id: tracker.id)

    assert {:ok, item} =
             Workspaces.update_work_item(item.id, %{
               version: item.lock_version,
               repository_resource_id: repository.id,
               ci_resource_id: ci.id,
               branch: "feature/provider-access",
               pull_request_url: "https://github.com/acme/symmetry/pull/42",
               assignee_type: "agent",
               agent_profile: "codex",
               workspace: "primary"
             })

    assert {:ok, %{task: %{task: task}}, :created} =
             Workspaces.launch_work_item(item.id, "provider-access-test")

    assert {:ok, run} = Orchestration.assign_one()
    claim_id = uuid()

    claim =
      bearer(conn, machine_token)
      |> put("/api/v1/runs/#{run.id}/claims/#{claim_id}", %{
        "runtime_id" => runtime_id,
        "runtime_epoch" => 1,
        "generation" => run.generation
      })
      |> json_response(200)

    %{
      access: claim["provider_access"],
      azure: azure,
      claim: claim,
      ci: ci,
      github: github,
      item: item,
      repository: repository,
      run: Repo.get!(Run, run.id),
      runtime_id: runtime_id,
      claim_id: claim_id,
      machine_id: machine_id,
      machine_token: machine_token,
      task: task,
      tracker: tracker
    }
  end

  test "historical action intents prevent deleting their resource and connection", context do
    assert %{"operation" => "change.upsert"} =
             context
             |> provider_request(context.repository.id, "change.upsert", %{
               "title" => "Retain action history"
             })
             |> json_response(200)

    intent = Repo.get_by!(ProviderActionIntent, run_id: context.run.id)

    Repo.update_all(from(item in WorkItem, where: item.id == ^context.item.id),
      set: [repository_resource_id: nil]
    )

    repository = Repo.get!(ProjectResource, context.repository.id)

    assert {:error, :state_conflict} =
             Workspaces.delete_resource(repository.id, repository.lock_version)

    assert Repo.get!(ProjectResource, repository.id)

    assert {:ok, detached_connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: "Historical intent only",
               account_ref: "acme",
               auth_type: "gh_cli",
               capabilities: ["repositories", "changes"]
             })

    Repo.update_all(from(stored in ProviderActionIntent, where: stored.id == ^intent.id),
      set: [connection_id: detached_connection.id]
    )

    assert {:error, :state_conflict} =
             Integrations.delete_connection(
               detached_connection.id,
               detached_connection.lock_version
             )

    assert Repo.get!(Connection, detached_connection.id)
  end

  test "claim emits exact mixed-provider grants without provider credentials", context do
    access = context.access

    assert MapSet.new(Map.keys(access)) ==
             MapSet.new(["path", "token", "grants"])

    assert access["path"] == "/api/v1/provider-actions"
    assert is_binary(access["token"])

    assert access["grants"] == [
             %{
               "resource_id" => context.tracker.id,
               "provider" => "azure_devops",
               "kind" => "work_tracking",
               "operations" => ["resource.sync"]
             },
             %{
               "resource_id" => context.repository.id,
               "provider" => "github",
               "kind" => "repository",
               "operations" => ["resource.sync", "change.upsert", "change.update"]
             },
             %{
               "resource_id" => context.ci.id,
               "provider" => "azure_devops",
               "kind" => "ci",
               "operations" => ["resource.sync"]
             }
           ]

    refute inspect(context.claim) =~ "provider-secret"
  end

  test "allows a granted change action and persists only normalized delivery", context do
    response =
      provider_request(context, context.repository.id, "change.upsert", %{
        "title" => "Add provider access"
      })
      |> json_response(200)

    assert %{
             "operation" => "change.upsert",
             "resource_id" => resource_id,
             "work_item_id" => work_item_id,
             "delivery" => %{
               "pull_request_url" => "https://github.com/acme/symmetry/pull/42",
               "pull_request_state" => "open",
               "review_status" => "required"
             }
           } = response

    assert resource_id == context.repository.id
    assert work_item_id == context.item.id
    refute inspect(response) =~ "provider-secret"

    assert_received {:provider_action, "github", ^resource_id, ^work_item_id, "change.upsert",
                     %{
                       "source_branch" => "feature/provider-access",
                       "target_branch" => "main",
                       "title" => "Add provider access"
                     }, %{access_token: "provider-secret"}}

    stored = Repo.get!(WorkItem, context.item.id)
    assert stored.external_pull_request_url == "https://github.com/acme/symmetry/pull/42"
    assert stored.external_change_data == %{"head_sha" => "abc123"}
    refute inspect(stored) =~ "provider-secret"
  end

  test "denies cross-resource and ungranted operations", context do
    extra =
      resource_fixture(
        context.item.project_id,
        context.github.id,
        "repository",
        "acme/other"
      )

    assert_error(provider_request(context, extra.id, "resource.sync", %{}), 403, "forbidden")

    assert_error(
      provider_request(context, context.tracker.id, "change.upsert", %{"title" => "Denied"}),
      403,
      "forbidden"
    )

    assert_error(
      provider_request(context, context.repository.id, "change.merge", %{}),
      400,
      "invalid_request"
    )
  end

  test "rejects tampered capability tokens", context do
    tampered = Map.update!(context.access, "token", &(&1 <> "x"))

    assert_error(
      provider_request(
        %{context | access: tampered},
        context.repository.id,
        "change.update",
        %{"title" => "Tampered"}
      ),
      401,
      "unauthenticated"
    )
  end

  test "rejects an expired lease", context do
    expire_run(context.run.id)

    assert_error(
      provider_request(context, context.repository.id, "change.update", %{"title" => "Expired"}),
      409,
      "ownership_lost"
    )
  end

  test "rejects cancelled and terminal executions", context do
    set_execution_state(context.run.id, context.task.id, "cancelled")

    assert_error(
      provider_request(context, context.repository.id, "change.update", %{"title" => "Cancelled"}),
      409,
      "ownership_lost"
    )

    set_execution_state(context.run.id, context.task.id, "completed")

    assert_error(
      provider_request(context, context.repository.id, "change.update", %{"title" => "Terminal"}),
      409,
      "ownership_lost"
    )
  end

  test "rejects a token from an old generation", context do
    Repo.update_all(from(task in Task, where: task.id == ^context.task.id),
      set: [current_generation: 2, attempt_generation: 2]
    )

    assert_error(
      provider_request(context, context.repository.id, "change.update", %{"title" => "Old"}),
      409,
      "ownership_lost"
    )
  end

  test "re-evaluates current connection capabilities", context do
    connection = Repo.get!(Connection, context.github.id)

    assert {:ok, _connection} =
             Integrations.update_connection(connection.id, %{
               version: connection.lock_version,
               capabilities: ["repositories"]
             })

    assert_error(
      provider_request(context, context.repository.id, "change.update", %{"title" => "Revoked"}),
      503,
      "provider_access_unavailable"
    )
  end

  test "re-evaluates the current runtime profile workspace and capabilities", context do
    cases = [
      [agent_profile: "other"],
      [workspace: "other"],
      [capabilities: %{"provider_access" => false, "structured_input" => true}]
    ]

    Enum.each(cases, fn attrs ->
      Repo.update_all(from(runtime in Runtime, where: runtime.id == ^context.runtime_id),
        set: attrs
      )

      assert_error(
        provider_request(context, context.repository.id, "change.upsert", %{
          "title" => "Revoked runtime"
        }),
        409,
        "ownership_lost"
      )

      assert Repo.aggregate(ProviderActionIntent, :count) == 0
      refute_receive {:provider_action, _, _, _, _, _, _}, 50

      Repo.update_all(from(runtime in Runtime, where: runtime.id == ^context.runtime_id),
        set: [
          agent_profile: "codex",
          workspace: "primary",
          capabilities: %{"provider_access" => true, "structured_input" => true}
        ]
      )
    end)
  end

  test "partial grants roll back claim and the same claim succeeds after recovery", context do
    reset_assignment(context.run.id, context.task.id)
    connection = Repo.get!(Connection, context.github.id)

    assert {:ok, without_changes} =
             Integrations.update_connection(connection.id, %{
               version: connection.lock_version,
               capabilities: ["repositories"]
             })

    request = %{
      "runtime_id" => context.runtime_id,
      "runtime_epoch" => 1,
      "generation" => context.run.generation
    }

    assert_error(
      bearer(build_conn(), context.machine_token)
      |> put("/api/v1/runs/#{context.run.id}/claims/#{context.claim_id}", request),
      503,
      "provider_access_unavailable"
    )

    rolled_back = Repo.get!(Run, context.run.id)
    assert rolled_back.state == "assigned"
    assert rolled_back.claim_id == nil
    assert rolled_back.lease_token == nil

    assert {:ok, _restored} =
             Integrations.update_connection(without_changes.id, %{
               version: without_changes.lock_version,
               capabilities: ["repositories", "changes"]
             })

    recovered =
      bearer(build_conn(), context.machine_token)
      |> put("/api/v1/runs/#{context.run.id}/claims/#{context.claim_id}", request)
      |> json_response(200)

    assert recovered["claim_id"] == context.claim_id

    assert Enum.any?(recovered["provider_access"]["grants"], fn grant ->
             grant["resource_id"] == context.repository.id and
               "change.upsert" in grant["operations"]
           end)
  end

  test "manual bindings are excluded from the connected provider snapshot", context do
    set_execution_state(context.run.id, context.task.id, "completed")

    assert {:ok, manual_repository} =
             Workspaces.create_resource(context.item.project_id, %{
               kind: "repository",
               name: "Manual repository #{uuid()}",
               external_ref: "manual/symmetry"
             })

    assert {:ok, manual_item} =
             Workspaces.create_work_item(context.item.project_id, %{
               title: "Manual repository with connected CI",
               description: "Only the connected CI resource needs a provider grant.",
               status: "ready",
               priority: "medium",
               assignee_type: "agent",
               agent_profile: "codex",
               workspace: "primary",
               repository_resource_id: manual_repository.id,
               ci_resource_id: context.ci.id
             })

    assert {:ok, %{task: %{task: task}}, :created} =
             Workspaces.launch_work_item(manual_item.id, "manual-connected-snapshot")

    assert task.input["provider_resource_ids"] == [context.ci.id]
    assert {:ok, run} = Orchestration.assign_one()
    claim_id = uuid()

    access =
      bearer(build_conn(), context.machine_token)
      |> put("/api/v1/runs/#{run.id}/claims/#{claim_id}", %{
        "runtime_id" => context.runtime_id,
        "runtime_epoch" => 1,
        "generation" => run.generation
      })
      |> json_response(200)
      |> Map.fetch!("provider_access")

    assert access["grants"] == [
             %{
               "resource_id" => context.ci.id,
               "provider" => "azure_devops",
               "kind" => "ci",
               "operations" => ["resource.sync"]
             }
           ]
  end

  test "disconnecting a launch-snapshot resource rolls back claim", context do
    reset_assignment(context.run.id, context.task.id)

    Repo.update_all(from(resource in ProjectResource, where: resource.id == ^context.ci.id),
      set: [connection_id: nil],
      inc: [lock_version: 1]
    )

    assert_error(
      bearer(build_conn(), context.machine_token)
      |> put("/api/v1/runs/#{context.run.id}/claims/#{context.claim_id}", %{
        "runtime_id" => context.runtime_id,
        "runtime_epoch" => 1,
        "generation" => context.run.generation
      }),
      503,
      "provider_access_unavailable"
    )

    rolled_back = Repo.get!(Run, context.run.id)
    assert rolled_back.state == "assigned"
    assert rolled_back.claim_id == nil
  end

  test "action IDs conflict on changed input and replay a stored success", context do
    action_id = uuid()
    input = %{"title" => "Idempotent change"}

    first =
      provider_request(context, context.repository.id, "change.upsert", input, action_id)
      |> json_response(200)

    assert_received {:provider_action, "github", _, _, "change.upsert", _, _}

    replay =
      provider_request(context, context.repository.id, "change.upsert", input, action_id)
      |> json_response(200)

    assert replay == first
    refute_receive {:provider_action, _, _, _, _, _, _}, 50

    expire_run(context.run.id)

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", input, action_id),
      409,
      "ownership_lost"
    )

    assert_error(
      provider_request(
        context,
        context.repository.id,
        "change.upsert",
        %{"title" => "Different input"},
        action_id
      ),
      409,
      "idempotency_conflict"
    )
  end

  test "provider success returns delivery when projection loses a stale race", context do
    action_id = uuid()

    response =
      provider_request(
        context,
        context.repository.id,
        "change.upsert",
        %{"title" => "Advance projection"},
        action_id
      )
      |> json_response(200)

    assert response["operation"] == "change.upsert"
    assert response["resource_id"] == context.repository.id
    assert response["work_item_id"] == context.item.id
    assert response["projected"] == false

    assert response["delivery"] == %{
             "ci_status" => nil,
             "pull_request_state" => "open",
             "pull_request_url" => "https://github.com/acme/symmetry/pull/42",
             "review_status" => "required",
             "updated_at" => "2026-09-06T08:05:00.000000Z"
           }

    assert_received {:provider_action, "github", _, _, "change.upsert", _, _}

    intent =
      Repo.get_by!(ProviderActionIntent, run_id: context.run.id, action_id: action_id)

    assert intent.state == "succeeded"
    assert intent.failure == nil
    assert intent.result["projected"] == false

    resource = Repo.get!(ProjectResource, context.repository.id)
    assert resource.status == "healthy"
    assert resource.sync_status == "synced"
  end

  test "an unknown change outcome retries the same action and then replays success", context do
    action_id = uuid()
    input = %{"title" => "Fail once"}

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", input, action_id),
      502,
      "provider_failure"
    )

    assert Repo.get_by!(ProviderActionIntent, run_id: context.run.id, action_id: action_id).state ==
             "unknown"

    assert_received {:provider_action, "github", _, _, "change.upsert", _, _}

    retried =
      provider_request(context, context.repository.id, "change.upsert", input, action_id)
      |> json_response(200)

    assert retried["operation"] == "change.upsert"
    assert retried["projected"] == true
    assert_received {:provider_action, "github", _, _, "change.upsert", _, _}

    intent = Repo.get_by!(ProviderActionIntent, run_id: context.run.id, action_id: action_id)
    assert intent.state == "succeeded"
    assert intent.failure == nil

    assert provider_request(context, context.repository.id, "change.upsert", input, action_id)
           |> json_response(200) == retried

    refute_receive {:provider_action, _, _, _, _, _, _}, 50
  end

  test "an accepted intent is safely resumed by the same action ID", context do
    action_id = uuid()
    input = %{"title" => "Resume accepted"}
    insert_accepted_intent(context, action_id, "change.upsert", input)

    assert %{"operation" => "change.upsert"} =
             provider_request(
               context,
               context.repository.id,
               "change.upsert",
               input,
               action_id
             )
             |> json_response(200)

    assert_received {:provider_action, "github", _, _, "change.upsert", _, _}

    intent = Repo.get_by!(ProviderActionIntent, run_id: context.run.id, action_id: action_id)
    assert intent.state == "succeeded"
    assert intent.dispatch_token == nil
  end

  test "accepted recovery uses the immutable provider target snapshot", context do
    action_id = uuid()
    input = %{"title" => "Recover original target"}
    insert_accepted_intent(context, action_id, "change.upsert", input)

    Repo.update_all(
      from(resource in ProjectResource, where: resource.id == ^context.repository.id),
      set: [external_ref: "acme/retargeted"],
      inc: [lock_version: 1]
    )

    assert %{"operation" => "change.upsert", "projected" => false} =
             provider_request(
               context,
               context.repository.id,
               "change.upsert",
               input,
               action_id
             )
             |> json_response(200)

    assert_received {:provider_action_target, "acme", "acme/symmetry"}
  end

  test "a broker restart automatically recovers accepted work after lease expiry", context do
    action_id = uuid()
    input = %{"title" => "Wait for cancel"}
    insert_accepted_intent(context, action_id, "change.upsert", input)
    expire_run(context.run.id)

    assert :ok =
             Supervisor.terminate_child(
               SymmetryControl.Supervisor,
               SymmetryControl.Integrations.ProviderAccess
             )

    assert {:ok, _pid} =
             Supervisor.restart_child(
               SymmetryControl.Supervisor,
               SymmetryControl.Integrations.ProviderAccess
             )

    assert_receive {:provider_action, "github", _, _, "change.upsert", _, _}
    assert_receive {:provider_action_waiting, provider_pid}
    provider_ref = Process.monitor(provider_pid)
    send(provider_pid, :continue)
    assert_receive {:DOWN, ^provider_ref, :process, ^provider_pid, :normal}

    intent = Repo.get_by!(ProviderActionIntent, run_id: context.run.id, action_id: action_id)
    assert intent.state == "succeeded"
  end

  test "the broker worker completes after the request process exits", context do
    action_id = uuid()
    input = %{"title" => "Wait for cancel"}

    requester =
      Elixir.Task.async(fn ->
        try do
          provider_request(context, context.repository.id, "change.upsert", input, action_id)
        catch
          :exit, reason -> {:exit, reason}
        end
      end)

    assert_receive {:provider_action, "github", _, _, "change.upsert", _, _}
    assert_receive {:provider_action_waiting, provider_pid}
    provider_ref = Process.monitor(provider_pid)

    Elixir.Task.shutdown(requester, :brutal_kill)
    assert Process.alive?(provider_pid)

    send(provider_pid, :continue)
    assert_receive {:DOWN, ^provider_ref, :process, ^provider_pid, :normal}

    intent = Repo.get_by!(ProviderActionIntent, run_id: context.run.id, action_id: action_id)
    assert intent.state == "succeeded"
    assert intent.dispatch_token == nil
  end

  test "the broker kills a hung dispatch before exposing an unknown outcome", context do
    integrations = Application.fetch_env!(:symmetry_control, :integrations)

    Application.put_env(
      :symmetry_control,
      :integrations,
      Keyword.put(integrations, :provider_action_timeout_ms, 500)
    )

    on_exit(fn -> Application.put_env(:symmetry_control, :integrations, integrations) end)

    action_id = uuid()
    input = %{"title" => "Wait for cancel"}

    requester =
      Elixir.Task.async(fn ->
        provider_request(context, context.repository.id, "change.upsert", input, action_id)
      end)

    assert_receive {:provider_action, "github", _, _, "change.upsert", _, _}
    assert_receive {:provider_action_waiting, provider_pid}
    provider_ref = Process.monitor(provider_pid)
    assert_receive {:DOWN, ^provider_ref, :process, ^provider_pid, :killed}, 2_000

    assert_error(Elixir.Task.await(requester, 2_000), 502, "provider_failure")
    recover_pending_and_wait()

    intent = Repo.get_by!(ProviderActionIntent, run_id: context.run.id, action_id: action_id)
    assert intent.state == "unknown"
    refute_receive {:provider_action, _, _, _, _, _, _}, 50
  end

  test "a broker restart fences an interrupted dispatch and permits only same-ID retry",
       context do
    action_id = uuid()
    input = %{"title" => "Wait for cancel"}

    requester =
      Elixir.Task.async(fn ->
        try do
          provider_request(context, context.repository.id, "change.upsert", input, action_id)
        catch
          :exit, reason -> {:exit, reason}
        end
      end)

    assert_receive {:provider_action, "github", _, _, "change.upsert", _, _}
    assert_receive {:provider_action_waiting, first_provider_pid}
    provider_ref = Process.monitor(first_provider_pid)

    assert :ok =
             Supervisor.terminate_child(
               SymmetryControl.Supervisor,
               SymmetryControl.Integrations.ProviderAccess
             )

    assert_receive {:DOWN, ^provider_ref, :process, ^first_provider_pid, :killed}

    assert {:ok, _pid} =
             Supervisor.restart_child(
               SymmetryControl.Supervisor,
               SymmetryControl.Integrations.ProviderAccess
             )

    recover_pending_and_wait()

    assert Repo.get_by!(ProviderActionIntent, run_id: context.run.id, action_id: action_id).state ==
             "unknown"

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", input, uuid()),
      409,
      "state_conflict"
    )

    retry =
      Elixir.Task.async(fn ->
        provider_request(context, context.repository.id, "change.upsert", input, action_id)
      end)

    assert_receive {:provider_action, "github", _, _, "change.upsert", _, _}
    assert_receive {:provider_action_waiting, second_provider_pid}
    refute second_provider_pid == first_provider_pid
    send(second_provider_pid, :continue)

    assert retry |> Elixir.Task.await(5_000) |> json_response(200)

    assert Repo.get_by!(ProviderActionIntent, run_id: context.run.id, action_id: action_id).state ==
             "succeeded"

    _ = Elixir.Task.yield(requester, 0)
  end

  test "recovery never fences an executing intent while another owner holds its advisory lock",
       context do
    action_id = uuid()
    intent = insert_accepted_intent(context, action_id, "change.upsert", %{"title" => "Owned"})
    dispatch_token = uuid()

    assert {:ok, _intent} =
             intent
             |> ProviderActionIntent.dispatch_changeset(dispatch_token)
             |> Repo.update()

    {:ok, lock_connection} = Postgrex.start_link(postgrex_test_options())

    assert {:ok, %Postgrex.Result{rows: [[true]]}} =
             Postgrex.query(
               lock_connection,
               "SELECT pg_try_advisory_lock($1, $2)",
               advisory_key(intent.id)
             )

    recover_pending_and_wait()

    still_owned = Repo.get!(ProviderActionIntent, intent.id)
    assert still_owned.state == "executing"
    assert still_owned.dispatch_token == dispatch_token
    refute_receive {:provider_action, _, _, _, _, _, _}, 50

    GenServer.stop(lock_connection, :normal, :infinity)
    recover_pending_and_wait()

    recovered = Repo.get!(ProviderActionIntent, intent.id)
    assert recovered.state == "unknown"
    assert recovered.dispatch_token == nil
    refute_receive {:provider_action, _, _, _, _, _, _}, 50
  end

  test "an unknown change outcome cannot retry after ownership is lost", context do
    action_id = uuid()
    input = %{"title" => "Fail once"}

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", input, action_id),
      502,
      "provider_failure"
    )

    assert_received {:provider_action, "github", _, _, "change.upsert", _, _}
    expire_run(context.run.id)

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", input, action_id),
      409,
      "ownership_lost"
    )

    assert Repo.get_by!(ProviderActionIntent, run_id: context.run.id, action_id: action_id).state ==
             "unknown"

    refute_receive {:provider_action, _, _, _, _, _, _}, 50
  end

  test "a health-write race preserves an ambiguous provider outcome for retry", context do
    action_id = uuid()
    input = %{"title" => "Fail once after resource race"}

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", input, action_id),
      502,
      "provider_failure"
    )

    assert_received {:provider_action, "github", _, _, "change.upsert", _, _}

    assert Repo.get_by!(ProviderActionIntent, run_id: context.run.id, action_id: action_id).state ==
             "unknown"

    assert %{"operation" => "change.upsert"} =
             provider_request(context, context.repository.id, "change.upsert", input, action_id)
             |> json_response(200)

    assert_received {:provider_action, "github", _, _, "change.upsert", _, _}
  end

  test "a definitive action failure replays without redispatch", context do
    action_id = uuid()
    input = %{"title" => "Fail forbidden"}

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", input, action_id),
      403,
      "forbidden"
    )

    assert_received {:provider_action, "github", _, _, "change.upsert", _, _}

    intent = Repo.get_by!(ProviderActionIntent, run_id: context.run.id, action_id: action_id)
    assert intent.state == "failed"
    assert intent.failure == %{"code" => "forbidden"}

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", input, action_id),
      403,
      "forbidden"
    )

    refute_receive {:provider_action, _, _, _, _, _, _}, 50
  end

  test "HTTP 4xx mutation failures are definite and replay without redispatch", context do
    action_id = uuid()
    input = %{"title" => "Fail HTTP 422"}

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", input, action_id),
      502,
      "provider_failure"
    )

    assert_received {:provider_action, "github", _, _, "change.upsert", _, _}
    intent = Repo.get_by!(ProviderActionIntent, run_id: context.run.id, action_id: action_id)
    assert intent.state == "failed"

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", input, action_id),
      502,
      "provider_failure"
    )

    refute_receive {:provider_action, _, _, _, _, _, _}, 50
  end

  test "provider-specific pre-dispatch validation failures normalize to definite errors",
       context do
    action_id = uuid()
    input = %{"title" => "Invalid pull request URL"}

    assert_error(
      provider_request(context, context.repository.id, "change.update", input, action_id),
      400,
      "invalid_request"
    )

    assert_received {:provider_action, "github", _, _, "change.update", _, _}
    intent = Repo.get_by!(ProviderActionIntent, run_id: context.run.id, action_id: action_id)
    assert intent.state == "failed"
    assert intent.failure == %{"code" => "invalid_request"}

    assert_error(
      provider_request(context, context.repository.id, "change.update", input, action_id),
      400,
      "invalid_request"
    )

    refute_receive {:provider_action, _, _, _, _, _, _}, 50
  end

  test "HTTP 5xx mutation failures remain retryable unknowns", context do
    assert_unknown_mutation_failure(context, "Fail HTTP 503")
  end

  test "invalid successful mutation responses remain retryable unknowns", context do
    assert_unknown_mutation_failure(context, "Return invalid success")
  end

  test "pre-dispatch authentication failures are definite", context do
    connection = Repo.get!(Connection, context.github.id)

    assert {:ok, _connection} =
             Integrations.update_connection(connection.id, %{
               version: connection.lock_version,
               name: "Unavailable auth"
             })

    action_id = uuid()
    input = %{"title" => "Must not dispatch"}

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", input, action_id),
      502,
      "provider_unauthorized"
    )

    intent = Repo.get_by!(ProviderActionIntent, run_id: context.run.id, action_id: action_id)
    assert intent.state == "failed"
    refute_receive {:provider_action, _, _, _, _, _, _}, 50

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", input, action_id),
      502,
      "provider_unauthorized"
    )
  end

  test "concurrent requests with the same action ID dispatch only once", context do
    action_id = uuid()
    input = %{"title" => "Wait for cancel"}

    requester =
      Elixir.Task.async(fn ->
        receive do
          :start ->
            provider_request(context, context.repository.id, "change.upsert", input, action_id)
        end
      end)

    Ecto.Adapters.SQL.Sandbox.allow(Repo, self(), requester.pid)
    send(requester.pid, :start)

    assert_receive {:provider_action, "github", _, _, "change.upsert", _, _}
    assert_receive {:provider_action_waiting, provider_pid}

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", input, action_id),
      409,
      "state_conflict"
    )

    refute_receive {:provider_action, _, _, _, _, _, _}, 50
    send(provider_pid, :continue)
    assert requester |> Elixir.Task.await(5_000) |> json_response(200)
  end

  test "concurrent action IDs for one resource dispatch only once", context do
    input = %{"title" => "Wait for cancel"}

    requester =
      Elixir.Task.async(fn ->
        receive do
          :start ->
            provider_request(context, context.repository.id, "change.upsert", input, uuid())
        end
      end)

    Ecto.Adapters.SQL.Sandbox.allow(Repo, self(), requester.pid)
    send(requester.pid, :start)

    assert_receive {:provider_action, "github", _, _, "change.upsert", _, _}
    assert_receive {:provider_action_waiting, provider_pid}

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", input, uuid()),
      409,
      "state_conflict"
    )

    refute_receive {:provider_action, _, _, _, _, _, _}, 50
    send(provider_pid, :continue)
    assert requester |> Elixir.Task.await(5_000) |> json_response(200)
  end

  test "rejects caller-controlled change scope and injects task scope", context do
    assert_error(
      provider_request(context, context.repository.id, "change.upsert", %{
        "title" => "Unsafe",
        "source_branch" => "attacker/branch"
      }),
      400,
      "invalid_request"
    )

    refute_receive {:provider_action, _, _, _, _, _, _}, 50

    assert %{"operation" => "change.upsert"} =
             provider_request(context, context.repository.id, "change.upsert", %{
               "title" => "Scoped",
               "body" => "Safe body"
             })
             |> json_response(200)

    assert_received {:provider_action, "github", _, _, "change.upsert",
                     %{
                       "title" => "Scoped",
                       "body" => "Safe body",
                       "source_branch" => "feature/provider-access",
                       "target_branch" => "main"
                     }, _}

    assert_error(
      provider_request(context, context.repository.id, "change.update", %{
        "title" => "Unsafe update",
        "pull_request_url" => "https://example.invalid/attacker"
      }),
      400,
      "invalid_request"
    )

    assert %{"operation" => "change.update"} =
             provider_request(context, context.repository.id, "change.update", %{
               "title" => "Scoped update"
             })
             |> json_response(200)

    assert_received {:provider_action, "github", _, _, "change.update",
                     %{
                       "title" => "Scoped update",
                       "pull_request_url" => "https://github.com/acme/symmetry/pull/42"
                     }, _}
  end

  test "rejects the current broker token in caller-controlled text", context do
    token = context.access["token"]

    title_response =
      provider_request(context, context.repository.id, "change.upsert", %{"title" => token})

    assert_error(title_response, 400, "invalid_request")
    refute title_response.resp_body =~ token

    body_response =
      provider_request(context, context.repository.id, "change.upsert", %{
        "title" => "Boundary",
        "body" => "before-#{token}-after"
      })

    assert_error(body_response, 400, "invalid_request")
    refute body_response.resp_body =~ token
    assert Repo.aggregate(ProviderActionIntent, :count) == 0
    refute_receive {:provider_action, _, _, _, _, _, _}, 50
  end

  test "rejects explicit null bodies like the shared change action contract", context do
    assert_error(
      provider_request(context, context.repository.id, "change.upsert", %{
        "title" => "Null body",
        "body" => nil
      }),
      400,
      "invalid_request"
    )

    assert Repo.aggregate(ProviderActionIntent, :count) == 0
    refute_receive {:provider_action, _, _, _, _, _, _}, 50
  end

  test "provider action request logs omit bearer and caller-controlled text", context do
    title = "log-title-#{uuid()}"
    body = "log-body-#{uuid()}"
    token = context.access["token"]

    logs =
      capture_log([level: :debug], fn ->
        assert %{"operation" => "change.upsert"} =
                 provider_request(context, context.repository.id, "change.upsert", %{
                   "title" => title,
                   "body" => body
                 })
                 |> json_response(200)
      end)

    refute logs =~ token
    refute logs =~ title
    refute logs =~ body
  end

  test "mutable work-item URLs cannot widen the launch-snapshot update scope", context do
    forged_url = "https://github.com/acme/symmetry/pull/99"

    Repo.update_all(from(item in WorkItem, where: item.id == ^context.item.id),
      set: [pull_request_url: forged_url, external_pull_request_url: forged_url],
      inc: [lock_version: 1]
    )

    assert %{"operation" => "change.update"} =
             provider_request(context, context.repository.id, "change.update", %{
               "title" => "Snapshot scoped"
             })
             |> json_response(200)

    assert_received {:provider_action, "github", _, _, "change.update",
                     %{
                       "title" => "Snapshot scoped",
                       "pull_request_url" => "https://github.com/acme/symmetry/pull/42"
                     }, _}
  end

  test "an update without a launch URL uses only this run's successful upsert", context do
    task = Repo.get!(Task, context.task.id)
    input_without_pull_request = Map.delete(task.input, "pull_request_url")

    Repo.update_all(from(stored_task in Task, where: stored_task.id == ^task.id),
      set: [input: input_without_pull_request]
    )

    forged_url = "https://github.com/acme/symmetry/pull/99"

    Repo.update_all(from(item in WorkItem, where: item.id == ^context.item.id),
      set: [pull_request_url: forged_url, external_pull_request_url: forged_url],
      inc: [lock_version: 1]
    )

    assert_error(
      provider_request(context, context.repository.id, "change.update", %{
        "title" => "Must not trust current URL"
      }),
      503,
      "provider_access_unavailable"
    )

    assert %{"operation" => "change.upsert"} =
             provider_request(context, context.repository.id, "change.upsert", %{
               "title" => "Create scoped PR"
             })
             |> json_response(200)

    Repo.update_all(from(item in WorkItem, where: item.id == ^context.item.id),
      set: [external_pull_request_url: forged_url],
      inc: [lock_version: 1]
    )

    assert %{"operation" => "change.update"} =
             provider_request(context, context.repository.id, "change.update", %{
               "title" => "Update scoped PR"
             })
             |> json_response(200)

    assert_received {:provider_action, "github", _, _, "change.update",
                     %{
                       "title" => "Update scoped PR",
                       "pull_request_url" => "https://github.com/acme/symmetry/pull/42"
                     }, _}
  end

  test "an old signed token follows the live renewed lease", context do
    renewed_until = DateTime.add(DateTime.utc_now(), 172_800, :second)

    Repo.update_all(from(run in Run, where: run.id == ^context.run.id),
      set: [lease_expires_at: renewed_until]
    )

    old_token =
      Phoenix.Token.sign(
        SymmetryControlWeb.Endpoint,
        "provider-access-v1",
        %{
          "v" => 1,
          "run" => context.run.id,
          "task" => context.task.id,
          "runtime" => context.runtime_id,
          "runtime_epoch" => 1,
          "generation" => context.run.generation,
          "claim" => context.claim_id
        },
        signed_at: System.system_time(:second) - 90_000
      )

    old_context = put_in(context.access["token"], old_token)

    assert %{"operation" => "resource.sync"} =
             provider_request(old_context, context.tracker.id, "resource.sync", %{})
             |> json_response(200)

    expire_run(context.run.id)

    assert_error(
      provider_request(old_context, context.tracker.id, "resource.sync", %{}),
      409,
      "ownership_lost"
    )
  end

  test "runtime re-registration invalidates the old provider token before intent creation",
       context do
    assert context.runtime_id == register(build_conn(), context.machine_id, context.machine_token)

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", %{
        "title" => "Old runtime epoch"
      }),
      409,
      "ownership_lost"
    )

    assert Repo.aggregate(ProviderActionIntent, :count) == 0
    refute_receive {:provider_action, _, _, _, _, _, _}, 50
  end

  test "cancellation committed before intent blocks provider dispatch", context do
    cancel_execution(context)

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", %{
        "title" => "Must not dispatch"
      }),
      409,
      "ownership_lost"
    )

    assert Repo.aggregate(ProviderActionIntent, :count) == 0
    refute_receive {:provider_action, _, _, _, _, _, _}, 50
  end

  test "accepted action finishes after cancellation and then replays once", context do
    action_id = uuid()
    input = %{"title" => "Wait for cancel"}

    requester =
      Elixir.Task.async(fn ->
        receive do
          :start ->
            provider_request(context, context.repository.id, "change.upsert", input, action_id)
        end
      end)

    Ecto.Adapters.SQL.Sandbox.allow(Repo, self(), requester.pid)
    send(requester.pid, :start)

    assert_receive {:provider_action, "github", _, _, "change.upsert", _, _}
    assert_receive {:provider_action_waiting, provider_pid}

    cancel_execution(context)

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", input, action_id),
      409,
      "ownership_lost"
    )

    refute_receive {:provider_action, _, _, _, _, _, _}, 50
    send(provider_pid, :continue)

    first = requester |> Elixir.Task.await(5_000) |> json_response(200)

    assert first["operation"] == "change.upsert"

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", input, action_id),
      409,
      "ownership_lost"
    )

    refute_receive {:provider_action, _, _, _, _, _, _}, 50
  end

  test "rejects provider responses containing credential-shaped data", context do
    response =
      provider_request(context, context.repository.id, "change.upsert", %{
        "title" => "Return credential"
      })

    assert_error(response, 502, "provider_failure")
    refute response.resp_body =~ "provider-secret"

    stored = Repo.get!(WorkItem, context.item.id)
    assert stored.external_change_data == %{}
    refute inspect(stored) =~ "provider-secret"

    resource = Repo.get!(SymmetryControl.Workspaces.ProjectResource, context.repository.id)
    assert resource.status == "degraded"
    assert resource.sync_status == "failed"
    assert resource.status_message == "Provider response is invalid"
    refute resource.status_message =~ "provider-secret"

    intent = Repo.get_by!(ProviderActionIntent, run_id: context.run.id)
    refute inspect(intent) =~ "provider-secret"
  end

  test "provider transport failures persist sanitized resource health", context do
    response =
      provider_request(context, context.repository.id, "change.update", %{
        "title" => "Fail transport"
      })

    assert_error(response, 502, "provider_failure")
    refute response.resp_body =~ "provider-secret"

    resource = Repo.get!(SymmetryControl.Workspaces.ProjectResource, context.repository.id)
    assert resource.status == "degraded"
    assert resource.sync_status == "failed"
    assert resource.status_message == "Provider transport failed"
    refute resource.status_message =~ "provider-secret"

    intent = Repo.get_by!(ProviderActionIntent, run_id: context.run.id)
    assert intent.failure == %{"code" => "provider_failure"}
    refute inspect(intent) =~ "provider-secret"
  end

  defp provider_request(context, resource_id, operation, input, action_id \\ nil) do
    action_id = action_id || uuid()

    bearer(build_conn(), context.access["token"])
    |> post(context.access["path"], %{
      "action_id" => action_id,
      "resource_id" => resource_id,
      "operation" => operation,
      "input" => input
    })
  end

  defp insert_accepted_intent(context, action_id, operation, caller_input) do
    connection = Repo.get!(Connection, context.github.id)
    resource = Repo.get!(ProjectResource, context.repository.id)
    work_item = Repo.get!(WorkItem, context.item.id)

    scoped_input =
      caller_input
      |> Map.put("source_branch", "feature/provider-access")
      |> Map.put("target_branch", "main")

    request_hash =
      :crypto.hash(
        :sha256,
        :erlang.term_to_binary(%{
          "resource_id" => context.repository.id,
          "operation" => operation,
          "input" => caller_input
        })
      )

    attrs = %{
      run_id: context.run.id,
      task_id: context.task.id,
      runtime_id: context.runtime_id,
      project_id: work_item.project_id,
      work_item_id: work_item.id,
      resource_id: resource.id,
      connection_id: connection.id,
      action_id: action_id,
      runtime_epoch: 1,
      generation: context.run.generation,
      claim_id: context.claim_id,
      operation: operation,
      request_hash: request_hash,
      input: scoped_input,
      state: "accepted",
      provider: connection.provider,
      account_ref: connection.account_ref,
      resource_kind: resource.kind,
      resource_external_ref: resource.external_ref,
      resource_lock_version: resource.lock_version,
      connection_lock_version: connection.lock_version,
      work_item_lock_version: work_item.lock_version
    }

    assert {:ok, intent} =
             %ProviderActionIntent{}
             |> ProviderActionIntent.accept_changeset(attrs)
             |> Repo.insert(log: false)

    intent
  end

  defp assert_unknown_mutation_failure(context, title) do
    action_id = uuid()
    input = %{"title" => title}

    assert_error(
      provider_request(context, context.repository.id, "change.upsert", input, action_id),
      502,
      "provider_failure"
    )

    assert_received {:provider_action, "github", _, _, "change.upsert", _, _}
    intent = Repo.get_by!(ProviderActionIntent, run_id: context.run.id, action_id: action_id)
    assert intent.state == "unknown"
  end

  defp recover_pending_and_wait do
    send(SymmetryControl.Integrations.ProviderAccess, :recover_interrupted_dispatches)
    state = :sys.get_state(SymmetryControl.Integrations.ProviderAccess)

    Enum.each(Map.keys(state.jobs), fn pid ->
      reference = Process.monitor(pid)
      assert_receive {:DOWN, ^reference, :process, ^pid, _reason}
    end)

    :sys.get_state(SymmetryControl.Integrations.ProviderAccess)
  end

  defp postgrex_test_options do
    Repo.config()
    |> Keyword.take([
      :hostname,
      :port,
      :database,
      :username,
      :password,
      :parameters,
      :ssl,
      :socket_options
    ])
  end

  defp advisory_key(intent_id) do
    <<first::signed-32, second::signed-32, _rest::binary>> =
      :crypto.hash(:sha256, Ecto.UUID.dump!(intent_id))

    [first, second]
  end

  defp set_execution_state(run_id, task_id, state) do
    Repo.update_all(from(run in Run, where: run.id == ^run_id), set: [state: state])
    Repo.update_all(from(task in Task, where: task.id == ^task_id), set: [state: state])
  end

  defp expire_run(run_id) do
    Repo.update_all(from(run in Run, where: run.id == ^run_id),
      set: [lease_expires_at: DateTime.add(DateTime.utc_now(), -1, :second)]
    )
  end

  defp reset_assignment(run_id, task_id) do
    Repo.update_all(from(run in Run, where: run.id == ^run_id),
      set: [
        state: "assigned",
        claimed_runtime_epoch: nil,
        claim_id: nil,
        lease_token: nil,
        claimed_at: nil,
        lease_expires_at: nil
      ]
    )

    Repo.update_all(from(task in Task, where: task.id == ^task_id), set: [state: "assigned"])
  end

  defp cancel_execution(context) do
    assert {:ok, _command, :created} =
             Orchestration.create_command(
               context.task.id,
               "cancel",
               %{},
               "provider-action-cancel-#{uuid()}",
               expected_generation: context.run.generation
             )
  end

  defp project_fixture do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: "Provider access",
               key: "P" <> String.slice(uuid(), 0, 6),
               default_agent_profile: "codex",
               default_workspace: "primary"
             })

    project
  end

  defp connection_fixture(provider, capabilities) do
    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: provider,
               name: provider <> "-" <> uuid(),
               account_ref: "acme",
               capabilities: capabilities
             })

    connection
  end

  defp resource_fixture(project_id, connection_id, kind, external_ref) do
    assert {:ok, resource} =
             Workspaces.create_resource(project_id, %{
               connection_id: connection_id,
               kind: kind,
               name: kind <> "-" <> uuid(),
               external_ref: external_ref
             })

    resource
  end

  defp enroll(conn) do
    token = "machine-token-" <> uuid()

    response =
      bearer(conn, @enrollment_token)
      |> put_req_header("idempotency-key", uuid())
      |> post("/api/v1/machines", %{
        "machine" => %{"name" => "provider-access-builder"},
        "machine_token" => token
      })
      |> json_response(201)

    {response["machine_id"], response["machine_token"]}
  end

  defp register(conn, machine_id, machine_token) do
    response =
      bearer(conn, machine_token)
      |> put("/api/v1/machines/#{machine_id}/sessions/#{uuid()}", %{
        "runtimes" => [
          %{
            "runtime_key" => "provider-access",
            "name" => "Provider access runtime",
            "capacity" => 1,
            "agent_profile" => "codex",
            "workspace" => "primary",
            "capabilities" => %{"provider_access" => true, "structured_input" => true}
          }
        ]
      })
      |> json_response(200)

    get_in(response, ["runtimes", Access.at(0), "runtime_id"])
  end

  defp assert_error(conn, status, code) do
    assert %{"error" => %{"code" => ^code}} = json_response(conn, status)
  end

  defp restore_env(key, nil), do: Application.delete_env(:symmetry_control, key)
  defp restore_env(key, value), do: Application.put_env(:symmetry_control, key, value)
  defp bearer(conn, token), do: put_req_header(conn, "authorization", "Bearer " <> token)
  defp uuid, do: Ecto.UUID.generate()
end
