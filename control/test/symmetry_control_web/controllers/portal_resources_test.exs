defmodule SymmetryControlWeb.PortalResourcesTest.Provider do
  @moduledoc false

  # A provider module that answers from a steering map keyed by resource name,
  # so one test can drive the real sync pipeline through every provider outcome
  # (success document, definitive HTTP failure, ambiguous transport failure)
  # without any provider host. A steered delivery answer is keyed under
  # {resource name, :delivery}, so one resource carries its sync document and its
  # delivery independently and every unsteered resource still answers {:ok, nil}
  # on the delivery path. The provider policy (reference and pull request
  # validation) is the real GitHub/Azure DevOps policy.

  @behaviour SymmetryControl.Integrations.Provider

  @impl true
  def authenticate(_connection), do: {:ok, :steered}

  @impl true
  def validate_resource_reference(%{provider: "github"} = connection, kind, reference),
    do:
      SymmetryControl.Integrations.Providers.GitHub.validate_resource_reference(
        connection,
        kind,
        reference
      )

  def validate_resource_reference(%{provider: "azure_devops"} = connection, kind, reference),
    do:
      SymmetryControl.Integrations.Providers.AzureDevOps.validate_resource_reference(
        connection,
        kind,
        reference
      )

  @impl true
  def check(connection, :steered) do
    {:ok, %{"account" => connection.account_ref, "actor" => connection.provider <> "-actor"}}
  end

  @impl true
  def sync_resource(connection, resource, :steered) do
    case Map.get(steering(), resource.name) do
      nil -> {:ok, provider_document(connection, resource)}
      answer -> answer
    end
  end

  @impl true
  def sync_delivery(connection, resource, work_item, :steered) do
    with :ok <- validate_pull_request_url(connection, resource, work_item.pull_request_url) do
      case Map.get(steering(), {resource.name, :delivery}) do
        nil -> {:ok, nil}
        answer -> answer
      end
    end
  end

  @impl true
  def sync_ci(_connection, _resource, _work_item, :steered) do
    {:ok,
     %{
       ci_status: "passed",
       updated_at: ~U[2026-09-05 09:35:00.000000Z],
       provider_data: %{"steered" => true}
     }}
  end

  @impl true
  def execute(_connection, _resource, _work_item, _operation, _input, :steered),
    do: {:error, :unsupported_resource}

  # The pull request URL is the delivery's identity, so the policy runs before
  # any steered answer: a malformed URL, or one that belongs to a repository
  # other than the bound resource, is refused instead of reported as a delivery.
  defp validate_pull_request_url(%{provider: "github"} = connection, resource, url),
    do:
      SymmetryControl.Integrations.Providers.GitHub.validate_pull_request_url(
        connection,
        resource,
        url
      )

  defp validate_pull_request_url(%{provider: "azure_devops"} = connection, resource, url),
    do:
      SymmetryControl.Integrations.Providers.AzureDevOps.validate_pull_request_url(
        connection,
        resource,
        url
      )

  defp steering, do: Application.get_env(:symmetry_control, :portal_resources_steering, %{})

  defp provider_document(connection, resource) do
    %{
      resource: %{
        url: provider_url(connection, resource.external_ref),
        metadata: %{"remote_kind" => resource.kind}
      },
      work_items: []
    }
  end

  defp provider_url(%{provider: "github"}, external_ref),
    do: "https://github.com/" <> external_ref

  defp provider_url(%{provider: "azure_devops", account_ref: account_ref}, external_ref),
    do: "https://dev.azure.com/#{account_ref}/" <> external_ref
end

defmodule SymmetryControlWeb.PortalResourcesTest do
  use SymmetryControlWeb.ConnCase, async: false

  import Ecto.Query
  import SymmetryControlWeb.PortalFixtures, only: [create_connection: 4, create_project: 2]
  import SymmetryControlWeb.ServedDocument, only: [validate_served_document!: 2]
  import SymmetryControlWeb.TestConn, only: [api_conn: 0]

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Repo
  alias SymmetryControl.TestEnv
  alias SymmetryControl.Workspaces.{ProjectResource, WorkItem}

  @delivery_url "https://github.com/acme/symmetry/pull/42"
  # The manual seed is GitHub's diff permalink, so the provider's own canonical
  # answer stays distinguishable from the value the work item was given.
  @delivery_seed_url "https://github.com/acme/symmetry/pull/42?diff=unified"
  @delivery_updated_at ~U[2026-09-05 09:45:00.000000Z]
  @delivery_head_sha "9f2c1d4b6a8e0f3c5d7b9a1e2f4c6d8b0a2e4f6c"
  @delivery_ci_url "https://github.com/acme/symmetry/actions/runs/9"

  # The optional-but-consumed members of `workspace-read`
  # (`contracts/v1/workspace-read.schema.json`): each name is read by
  # `frontend/src` feature code and absent from its definition's `required`
  # array, so the bundle comparison alone cannot refuse a serving-side rename of
  # it. The presence assertions in "the real served workspace read carries every
  # member the surface reads" are what fails red for these. Frontend read sites,
  # list by list:
  #
  #   root             features/runtimes/runtimes.tsx:81,85,95; features/connections/resources.tsx:136
  #   project row      features/projects/projects.tsx:89,108; features/planning/write-policy.ts:50,201;
  #                    features/work/assignment.tsx:250; features/connections/resources.tsx:130
  #   resource row     features/connections/resources.tsx:102-116; features/work/assignment.tsx:53-57
  #   registered row   features/connections/resources.tsx:137-140
  #   runtime row      features/runtimes/runtimes.tsx:17,25,37-41
  #   capabilities     features/runtimes/runtimes.tsx:16,50-56 read `adapter`; the four booleans have no
  #                    member-named read site — the whole map is serialized into `data-capabilities`
  #                    (`runtimes.tsx:24`) and `browser/tests/portal-live-daemon.spec.mjs` asserts that
  #                    attribute equals the registration-side map
  #   work row         features/work/control.tsx:304,444; features/work/assignment.tsx:152-155;
  #                    features/planning/write-policy.ts:27-28,40,264-279; features/work/detail.tsx:310-343,370
  #   delivery value   features/work/presentation.ts:293,339-340,347-348,355-356
  #   external row     features/work/detail.tsx:320; features/planning/write-policy.ts:20
  #   waiting decision features/work/presentation.ts:399-409
  @workspace_read_members ~w(runtimes registered_runtimes)
  @workspace_project_members ~w(version default_agent_profile default_workspace resources)
  @workspace_resource_members ~w(id project_id connection_id kind name provider external_ref url
    status sync_status status_message last_synced_at version)
  @workspace_registered_runtime_members ~w(runtime_id runtime_name agent_profile workspace)
  @workspace_runtime_members ~w(capacity reserved_capacity connection_epoch last_heartbeat_at
    repository_resource_id agent_profile workspace)
  @workspace_runtime_capability_members ~w(adapter interactive provider_access structured_input
    supervisory_control)
  @workspace_work_item_members ~w(project_id goal_id description priority assignee workspace
    repository_resource_id repository ci_resource_id ci_resource branch pull_request_url
    ci_status review_status delivery external goal_managed version can_start execution updated_at)
  @workspace_delivery_value_members ~w(provider recorded_at)
  @workspace_external_members ~w(provider_owned_fields)
  @workspace_waiting_decision_members ~w(context reason options recommended_option_id)
  @workspace_waiting_decision_option_members ~w(id label consequence)

  # The capability maps the two runtime fixtures register.
  @adapter_runtime_capabilities %{
    "structured_input" => true,
    "provider_access" => true,
    "adapter" => %{
      "kind" => "generic",
      "native_version" => "generic-v1",
      "implementation_version" => "generic-v1",
      "protocol_version" => 1,
      "operations" => %{
        "start" => true,
        "events" => true,
        "cancel" => false,
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

  # The runtime row the workspace gate serves: registered with every
  # `RuntimeCapabilities` member the surface reads — `adapter` plus the four
  # booleans — so the registered map's pass-through is exercised whole, and
  # advertising the adapter `start` operation the item's launch needs a
  # selectable runtime for.
  @gate_runtime_capabilities %{
    "interactive" => true,
    "structured_input" => true,
    "provider_access" => true,
    "supervisory_control" => true,
    "adapter" => %{
      "kind" => "generic",
      "native_version" => "generic-v1",
      "implementation_version" => "generic-v1",
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

  setup do
    TestEnv.put!(:integration_providers,
      github: SymmetryControlWeb.PortalResourcesTest.Provider,
      azure_devops: SymmetryControlWeb.PortalResourcesTest.Provider
    )

    TestEnv.put!(:portal_resources_steering, %{})

    :ok
  end

  test "resources bind, reset identity on reference change and unbind for every kind and provider" do
    project = create_project("BIND")

    github =
      create_connection("github", "GitHub binding", "acme", [
        "repositories",
        "work_items",
        "ci"
      ])

    azure =
      create_connection("azure_devops", "Azure binding", "acme", [
        "repositories",
        "work_items",
        "ci"
      ])

    cases = [
      {github, "repository", "acme/symmetry", "acme/readme"},
      {github, "work_tracking", "acme/issues", "acme/boards"},
      {github, "ci", "acme/actions", "acme/pipelines"},
      {azure, "repository", "Platform/symmetry", "Platform/portal"},
      {azure, "work_tracking", "Platform Boards", "Platform Boards Two"},
      {azure, "ci", "Platform/pipeline/42", "Platform/pipeline/43"}
    ]

    bindings =
      Enum.map(cases, fn {connection, kind, reference, next_reference} ->
        resource =
          create_resource(
            project,
            connection,
            kind,
            "Bound #{connection["provider"]} #{kind}",
            reference
          )

        assert resource["provider"] == connection["provider"]
        assert resource["kind"] == kind
        assert resource["external_ref"] == reference
        assert resource["connection_id"] == connection["id"]
        assert resource["project_id"] == project["id"]
        assert resource["url"] == nil
        assert resource["status"] == "unknown"
        assert resource["sync_status"] == "unknown"
        assert resource["last_synced_at"] == nil
        assert resource["version"] == 1

        %{
          connection: connection,
          kind: kind,
          reference: reference,
          next_reference: next_reference,
          resource: resource
        }
      end)

    served = project_read(project["id"])
    assert length(served["resources"]) == 6
    served_by_id = Map.new(served["resources"], &{&1["id"], &1})

    Enum.each(bindings, fn %{connection: connection, resource: resource} ->
      assert served_by_id[resource["id"]]["provider"] == connection["provider"]
      assert served_by_id[resource["id"]]["external_ref"] == resource["external_ref"]
    end)

    assert %{"connections" => connections} =
             api_conn() |> get("/portal/api/connections") |> json_response(200)

    bound_ids =
      connections
      |> Enum.flat_map(& &1["resources"])
      |> Enum.map(& &1["id"])
      |> Enum.sort()

    assert bound_ids == Enum.map(bindings, & &1.resource["id"]) |> Enum.sort()

    Enum.each(bindings, fn binding ->
      %{connection: connection, resource: resource, reference: reference} = binding

      assert %{"resource" => synced} = sync_resource(resource["id"]) |> json_response(200)
      assert synced["status"] == "healthy"
      assert synced["sync_status"] == "synced"
      assert synced["url"] == provider_url(connection, reference)
      assert synced["last_synced_at"] != nil

      assert %{"resource" => reset} =
               api_conn()
               |> patch("/portal/api/resources/#{resource["id"]}", %{
                 "version" => synced["version"],
                 "external_ref" => binding.next_reference
               })
               |> json_response(200)

      assert reset["external_ref"] == binding.next_reference
      assert reset["provider"] == connection["provider"]
      assert reset["status"] == "unknown"
      assert reset["sync_status"] == "unknown"
      assert reset["status_message"] == nil
      assert reset["url"] == nil
      assert reset["metadata"] == %{}
      assert reset["last_checked_at"] == nil
      assert reset["last_synced_at"] == nil
      assert reset["version"] == synced["version"] + 1

      assert %{"deleted_resource_id" => deleted_id} =
               api_conn()
               |> delete("/portal/api/resources/#{resource["id"]}?version=#{reset["version"]}")
               |> json_response(200)

      assert deleted_id == resource["id"]
    end)

    assert project_read(project["id"])["resources"] == []
  end

  test "a connection without the kind's capability refuses binding and reports the sync refusal" do
    project = create_project("CAPS")
    limited = create_connection("github", "Repositories only", "acme", ["repositories"])

    for kind <- ["work_tracking", "ci"] do
      assert %{"error" => %{"code" => "capability_not_granted", "message" => message}} =
               api_conn()
               |> post("/portal/api/projects/#{project["id"]}/resources", %{
                 "connection_id" => limited["id"],
                 "kind" => kind,
                 "name" => "Refused #{kind}",
                 "external_ref" => "acme/symmetry"
               })
               |> json_response(403)

      assert is_binary(message)
    end

    granted = create_connection("github", "Work items granted", "acme", ["work_items"])
    tracker = create_tracker(project, granted, "Capability tracker", "acme/issues")

    assert %{"connection" => _narrowed} =
             api_conn()
             |> patch("/portal/api/connections/#{granted["id"]}", %{
               "version" => granted["version"],
               "capabilities" => ["repositories"]
             })
             |> json_response(200)

    assert %{"resource" => refused, "error" => %{"code" => "capability_not_granted"}} =
             sync_resource(tracker["id"]) |> json_response(403)

    assert refused["status"] == "degraded"
    assert refused["sync_status"] == "failed"
    assert refused["status_message"] == "Connection capability is not granted"
    assert refused["last_synced_at"] == nil

    served = Enum.find(project_read(project["id"])["resources"], &(&1["id"] == tracker["id"]))
    assert served["sync_status"] == "failed"
    assert served["status_message"] == "Connection capability is not granted"
  end

  test "an update cannot move a resource onto a connection without the kind's capability" do
    project = create_project("MOVECAPS")
    granted = create_connection("github", "Work items granted", "acme", ["work_items"])
    tracker = create_tracker(project, granted, "Movable tracker", "acme/issues")
    limited = create_connection("github", "Repositories only", "acme", ["repositories"])

    assert %{"error" => %{"code" => "capability_not_granted", "message" => message}} =
             api_conn()
             |> patch("/portal/api/resources/#{tracker["id"]}", %{
               "version" => tracker["version"],
               "connection_id" => limited["id"]
             })
             |> json_response(403)

    assert is_binary(message)

    served = Enum.find(project_read(project["id"])["resources"], &(&1["id"] == tracker["id"]))
    assert served["connection_id"] == granted["id"]
  end

  test "a definitive provider failure is recorded without touching the last synchronized state" do
    project = create_project("FAIL")
    connection = create_connection("github", "Failure binding", "acme", ["work_items"])
    tracker = create_tracker(project, connection, "Failing tracker", "acme/issues")

    assert %{"resource" => synced} = sync_resource(tracker["id"]) |> json_response(200)
    assert synced["status"] == "healthy"
    assert synced["sync_status"] == "synced"
    assert synced["url"] == "https://github.com/acme/issues"
    assert synced["last_synced_at"] != nil

    before = resource_snapshot(tracker["id"])
    assert before.status == "healthy"
    assert before.sync_status == "synced"

    steer("Failing tracker", {:error, {:http, 500, "provider exploded"}})

    assert %{"resource" => failed, "error" => %{"code" => "provider_failure"}} =
             sync_resource(tracker["id"]) |> json_response(502)

    assert failed["status"] == "degraded"
    assert failed["sync_status"] == "failed"
    assert failed["status_message"] == "Provider returned HTTP 500"
    assert failed["last_synced_at"] == synced["last_synced_at"]

    after_failure = resource_snapshot(tracker["id"])
    assert after_failure.last_synced_at == before.last_synced_at
    assert after_failure.url == before.url
    assert after_failure.metadata == before.metadata
    assert after_failure.sync_status == "failed"
    assert after_failure.status_message == "Provider returned HTTP 500"
  end

  test "an ambiguous transport outcome stays failed until a readback writes exactly the provider answer" do
    project = create_project("READBACK")
    connection = create_connection("github", "Readback binding", "acme", ["repositories"])

    repository =
      create_resource(project, connection, "repository", "Readback repository", "acme/symmetry")

    steer(
      "Readback repository",
      {:ok,
       %{
         resource: %{
           url: "https://github.com/acme/symmetry-before",
           metadata: %{"stage" => "before"}
         },
         work_items: []
       }}
    )

    assert %{"resource" => synced} = sync_resource(repository["id"]) |> json_response(200)
    assert synced["status"] == "healthy"
    assert synced["sync_status"] == "synced"
    assert synced["url"] == "https://github.com/acme/symmetry-before"

    before = resource_snapshot(repository["id"])
    assert before.metadata == %{"stage" => "before"}
    assert before.last_synced_at != nil

    steer("Readback repository", {:error, {:transport, :closed}})

    assert %{"resource" => failed, "error" => %{"code" => "provider_failure"}} =
             sync_resource(repository["id"]) |> json_response(502)

    assert failed["status"] == "degraded"
    assert failed["sync_status"] == "failed"
    assert failed["status_message"] == "Provider transport failed"
    assert failed["last_synced_at"] == synced["last_synced_at"]

    ambiguous = resource_snapshot(repository["id"])
    assert ambiguous.last_synced_at == before.last_synced_at
    assert ambiguous.url == "https://github.com/acme/symmetry-before"
    assert ambiguous.sync_status == "failed"

    # The readback is the same sync read: whatever the provider answers now is
    # written verbatim, including a state that contradicts the earlier one.
    steer(
      "Readback repository",
      {:ok,
       %{
         resource: %{
           url: "https://github.com/acme/symmetry-after",
           metadata: %{"stage" => "after", "archived" => true}
         },
         work_items: []
       }}
    )

    assert %{"resource" => resolved} = sync_resource(repository["id"]) |> json_response(200)
    assert resolved["status"] == "healthy"
    assert resolved["sync_status"] == "synced"
    assert resolved["url"] == "https://github.com/acme/symmetry-after"
    assert resolved["metadata"] == %{"stage" => "after", "archived" => true}

    readback = resource_snapshot(repository["id"])
    assert readback.url == "https://github.com/acme/symmetry-after"
    assert readback.metadata == %{"stage" => "after", "archived" => true}
    assert DateTime.compare(readback.last_synced_at, before.last_synced_at) == :gt
  end

  test "project sync reports the failing resource and still synchronizes the healthy one" do
    project = create_project("AGG")
    connection = create_connection("github", "Aggregate binding", "acme", ["work_items"])
    healthy = create_tracker(project, connection, "Healthy tracker", "acme/healthy")
    failing = create_tracker(project, connection, "Failing tracker", "acme/failing")

    steer("Failing tracker", {:error, {:http, 500, "provider exploded"}})

    assert %{
             "error" => %{"code" => "provider_failure", "message" => "Provider returned HTTP 500"},
             "results" => results
           } =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/sync")
             |> json_response(502)

    assert length(results) == 2
    results_by_id = Map.new(results, &{&1["resource_id"], &1})

    assert results_by_id[failing["id"]]["status"] == "failed"
    assert get_in(results_by_id, [failing["id"], "error", "code"]) == "provider_failure"

    assert get_in(results_by_id, [failing["id"], "error", "message"]) ==
             "Provider returned HTTP 500"

    assert results_by_id[healthy["id"]] == %{
             "resource_id" => healthy["id"],
             "status" => "synced"
           }

    assert resource_snapshot(healthy["id"]).sync_status == "synced"
    assert resource_snapshot(healthy["id"]).last_synced_at != nil
    assert resource_snapshot(failing["id"]).sync_status == "failed"
  end

  test "external work items keep identity, refuse provider-owned edits and lose execution when unavailable" do
    project = create_project("EXT")
    connection = create_connection("github", "External binding", "acme", ["work_items"])
    tracker = create_tracker(project, connection, "External tracker", "acme/issues")

    document = fn items ->
      {:ok,
       %{
         resource: %{
           url: "https://github.com/acme/issues",
           metadata: %{"remote_kind" => "work_tracking"}
         },
         work_items: items
       }}
    end

    steer(
      "External tracker",
      document.([
        provider_item("101", "https://github.com/acme/issues/101"),
        provider_item("102", "https://github.com/acme/issues/102")
      ])
    )

    assert %{"resource" => synced} = sync_resource(tracker["id"]) |> json_response(200)
    assert synced["status"] == "healthy"
    assert synced["sync_status"] == "synced"

    assert Repo.all(
             from item in WorkItem,
               where: item.external_work_item_resource_id == ^tracker["id"],
               order_by: [asc: item.external_id],
               select: {item.external_id, item.external_url, item.external_available}
           ) == [
             {"101", "https://github.com/acme/issues/101", true},
             {"102", "https://github.com/acme/issues/102", true}
           ]

    items = served_items(project["id"])
    item_101 = items["101"]
    item_102 = items["102"]

    assert item_101["external"]["provider"] == "github"
    assert item_101["external"]["id"] == "101"
    assert item_101["external"]["url"] == "https://github.com/acme/issues/101"
    assert item_101["external"]["available"] == true
    assert item_101["external"]["resource_id"] == tracker["id"]
    assert item_102["external"]["url"] == "https://github.com/acme/issues/102"

    assert %{"error" => %{"code" => "provider_owned"}} =
             api_conn()
             |> patch("/portal/api/work-items/#{item_101["id"]}", %{
               "version" => item_101["version"],
               "title" => "Local overwrite"
             })
             |> json_response(409)

    assert %{"work_item" => assigned_101} =
             api_conn()
             |> patch("/portal/api/work-items/#{item_101["id"]}", %{
               "version" => item_101["version"],
               "assignee_type" => "agent",
               "agent_profile" => "codex",
               "workspace" => "primary"
             })
             |> json_response(200)

    assert assigned_101["assignee"]["type"] == "agent"

    assert %{"work_item" => assigned_102} =
             api_conn()
             |> patch("/portal/api/work-items/#{item_102["id"]}", %{
               "version" => item_102["version"],
               "assignee_type" => "agent",
               "agent_profile" => "codex",
               "workspace" => "primary"
             })
             |> json_response(200)

    assert assigned_102["external"]["id"] == "102"

    available = served_items(project["id"])
    assert available["101"]["can_start"] == true
    assert available["101"]["external"]["available"] == true

    # The served retry control reads assignment eligibility, so a runtime the
    # task could be assigned to must advertise the adapter `start` operation
    # for the external-work gate below to be the only refusal left.
    enroll_adapter_runtime("resource-retry", "Resource retry runtime")

    assert %{"execution" => %{"generation" => 1, "state" => "queued"}} =
             api_conn()
             |> post("/portal/api/work-items/#{item_102["id"]}/run", %{
               "action_id" => "external-run"
             })
             |> json_response(202)

    assert %{"command" => _cancel} =
             api_conn()
             |> post("/portal/api/work-items/#{item_102["id"]}/cancel", %{
               "action_id" => "external-cancel",
               "generation" => 1
             })
             |> json_response(202)

    cancelled = served_items(project["id"])
    assert cancelled["102"]["execution"]["state"] == "cancelled"
    assert cancelled["102"]["execution"]["can_retry"] == true

    # The readback omits both items: availability is written from the provider
    # answer, never carried over from the previous synchronization.
    steer("External tracker", document.([]))

    assert %{"resource" => _resynced} = sync_resource(tracker["id"]) |> json_response(200)

    unavailable = served_items(project["id"])

    assert unavailable["101"]["external"]["available"] == false
    assert unavailable["101"]["can_start"] == false
    assert unavailable["102"]["external"]["available"] == false
    assert unavailable["102"]["execution"]["can_retry"] == false

    assert Repo.all(
             from item in WorkItem,
               where: item.external_work_item_resource_id == ^tracker["id"],
               order_by: [asc: item.external_id],
               select: {item.external_id, item.external_available}
           ) == [{"101", false}, {"102", false}]
  end

  test "a repository sync writes the provider delivery for a pull request bound to the resource" do
    project = create_project("DELIVERY")

    connection =
      create_connection("github", "Delivery binding", "acme", ["repositories", "changes", "ci"])

    repository =
      create_resource(project, connection, "repository", "Delivery repository", "acme/symmetry")

    item = create_work_item(project, "Delivery target")

    assert %{"work_item" => seeded} =
             api_conn()
             |> patch("/portal/api/work-items/#{item["id"]}", %{
               "version" => item["version"],
               "repository_resource_id" => repository["id"],
               "pull_request_url" => @delivery_seed_url
             })
             |> json_response(200)

    assert seeded["repository_resource_id"] == repository["id"]
    assert seeded["pull_request_url"] == @delivery_seed_url
    assert seeded["delivery"]["pull_request"]["source"] == "manual"
    assert seeded["delivery"]["pull_request"]["generation"] == nil

    steer_delivery("Delivery repository", {:ok, provider_delivery(@delivery_url)})

    assert %{"resource" => synced} = sync_resource(repository["id"]) |> json_response(200)
    assert synced["status"] == "healthy"
    assert synced["sync_status"] == "synced"
    assert synced["url"] == "https://github.com/acme/symmetry"

    served = served_item(project["id"], item["id"])
    delivery = served["delivery"]

    assert delivery["pull_request"]["source"] == "provider"
    assert delivery["pull_request"]["provider"] == "github"
    assert delivery["pull_request"]["value"] == @delivery_url
    assert delivery["pull_request"]["url"] == @delivery_url
    assert delivery["pull_request"]["status"] == "open"
    assert delivery["pull_request"]["generation"] == nil
    assert delivery["pull_request"]["recorded_at"] == DateTime.to_iso8601(@delivery_updated_at)

    assert served["pull_request_url"] == @delivery_url

    assert delivery["ci"]["source"] == "provider"
    assert delivery["ci"]["provider"] == "github"
    assert delivery["ci"]["value"] == "passed"
    assert delivery["ci"]["status"] == "passed"
    assert delivery["ci"]["url"] == nil
    assert delivery["ci"]["generation"] == nil
    assert delivery["ci"]["recorded_at"] == DateTime.to_iso8601(@delivery_updated_at)
    assert served["ci_status"] == "passed"

    assert delivery["review"]["source"] == "provider"
    assert delivery["review"]["provider"] == "github"
    assert delivery["review"]["value"] == "approved"
    assert delivery["review"]["status"] == "approved"
    assert delivery["review"]["generation"] == nil
    assert served["review_status"] == "approved"

    assert %{"work_item" => detail} =
             api_conn()
             |> get("/portal/api/work-items/#{item["id"]}")
             |> json_response(200)

    assert detail["pull_request_url"] == @delivery_url
    assert detail["delivery"] == delivery

    stored = work_item_snapshot(item["id"])

    assert stored.external_pull_request_url == @delivery_url
    assert stored.external_pull_request_state == "open"
    assert stored.external_change_updated_at == @delivery_updated_at
    assert stored.external_review_status == "approved"
    assert stored.external_ci_status == "passed"
    assert stored.external_ci_updated_at == @delivery_updated_at

    assert stored.external_change_data == %{
             "head_sha" => @delivery_head_sha,
             "pull_request_state" => "open"
           }

    assert stored.external_ci_data == %{"ci_url" => @delivery_ci_url}

    # The provider write lands in the external columns; the work item's own
    # column keeps the manual value the projection no longer prefers.
    assert stored.pull_request_url == @delivery_seed_url
  end

  test "a pull request outside the bound repository is refused and no delivery is written" do
    project = create_project("FOREIGN")

    connection =
      create_connection("github", "Foreign delivery binding", "acme", [
        "repositories",
        "changes",
        "ci"
      ])

    repository =
      create_resource(
        project,
        connection,
        "repository",
        "Foreign delivery repository",
        "acme/symmetry"
      )

    item = create_work_item(project, "Foreign delivery target")

    assert %{"work_item" => seeded} =
             api_conn()
             |> patch("/portal/api/work-items/#{item["id"]}", %{
               "version" => item["version"],
               "repository_resource_id" => repository["id"],
               "pull_request_url" => "https://github.com/other/repository/pull/9"
             })
             |> json_response(200)

    # A delivery answer is steered for the bound repository, so this refusal can
    # only come from the pull request URL policy.
    steer_delivery("Foreign delivery repository", {:ok, provider_delivery(@delivery_url)})

    assert %{
             "resource" => refused,
             "error" => %{"code" => "invalid_request", "message" => "request payload is invalid"}
           } = sync_resource(repository["id"]) |> json_response(400)

    assert refused["status"] == "degraded"
    assert refused["sync_status"] == "failed"
    assert refused["status_message"] == "Pull request URL does not match the bound repository"
    assert refused["url"] == nil
    assert refused["last_synced_at"] == nil

    malformed = "https://github.com/acme/symmetry/pull/not-a-number"

    assert %{"work_item" => reseeded} =
             api_conn()
             |> patch("/portal/api/work-items/#{item["id"]}", %{
               "version" => seeded["version"],
               "pull_request_url" => malformed
             })
             |> json_response(200)

    assert reseeded["pull_request_url"] == malformed

    assert %{
             "error" => %{
               "code" => "invalid_request",
               "message" => "Pull request URL does not match the bound repository"
             },
             "results" => [result]
           } =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/sync")
             |> json_response(400)

    assert result["resource_id"] == repository["id"]
    assert result["status"] == "failed"
    assert result["error"]["code"] == "invalid_request"
    assert result["error"]["http_status"] == 400

    served_resource =
      Enum.find(project_read(project["id"])["resources"], &(&1["id"] == repository["id"]))

    assert served_resource["status"] == "degraded"
    assert served_resource["sync_status"] == "failed"

    assert served_resource["status_message"] ==
             "Pull request URL does not match the bound repository"

    # The refused synchronization leaves the manual delivery exactly as the work
    # item carries it: no provider value is reported and nothing is stored.
    served = served_item(project["id"], item["id"])

    assert served["pull_request_url"] == malformed
    assert served["delivery"]["pull_request"]["source"] == "manual"
    assert served["delivery"]["pull_request"]["value"] == malformed
    assert served["delivery"]["pull_request"]["url"] == malformed
    assert served["delivery"]["pull_request"]["status"] == nil
    assert served["delivery"]["pull_request"]["generation"] == nil
    assert served["delivery"]["ci"]["source"] == "none"
    assert served["delivery"]["review"]["source"] == "none"

    assert %{"work_item" => detail} =
             api_conn()
             |> get("/portal/api/work-items/#{item["id"]}")
             |> json_response(200)

    assert detail["delivery"] == served["delivery"]
    assert detail["pull_request_url"] == malformed

    stored = work_item_snapshot(item["id"])

    assert stored.pull_request_url == malformed
    assert stored.lock_version == reseeded["version"]
    assert stored.external_pull_request_url == nil
    assert stored.external_pull_request_state == nil
    assert stored.external_review_status == nil
    assert stored.external_ci_status == nil
    assert stored.external_change_updated_at == nil
    assert stored.external_ci_updated_at == nil
    assert stored.external_change_data == %{}
    assert stored.external_ci_data == %{}
  end

  test "work item assignment is scoped to the work item's project and resource kind" do
    project = create_project("ASSIGN")
    other_project = create_project("OTHER")
    connection = create_connection("github", "Assignment binding", "acme", ["repositories", "ci"])

    repository =
      create_resource(project, connection, "repository", "Own repository", "acme/symmetry")

    other_repository =
      create_resource(other_project, connection, "repository", "Other repository", "acme/other")

    ci = create_resource(project, connection, "ci", "Own pipeline", "acme/pipeline")

    item = create_work_item(project, "Assignment target")

    assert %{"work_item" => assigned} =
             api_conn()
             |> patch("/portal/api/work-items/#{item["id"]}", %{
               "version" => item["version"],
               "repository_resource_id" => repository["id"],
               "ci_resource_id" => ci["id"]
             })
             |> json_response(200)

    assert assigned["repository_resource_id"] == repository["id"]
    assert assigned["ci_resource_id"] == ci["id"]

    assert %{
             "error" => %{
               "code" => "validation_failed",
               "fields" => %{
                 "repository_resource_id" => ["must belong to the work item's project"]
               }
             }
           } =
             api_conn()
             |> patch("/portal/api/work-items/#{item["id"]}", %{
               "version" => assigned["version"],
               "repository_resource_id" => other_repository["id"]
             })
             |> json_response(422)

    assert %{
             "error" => %{
               "code" => "validation_failed",
               "fields" => %{
                 "ci_resource_id" => ["must be a CI resource in the work item's project"]
               }
             }
           } =
             api_conn()
             |> patch("/portal/api/work-items/#{item["id"]}", %{
               "version" => assigned["version"],
               "ci_resource_id" => repository["id"]
             })
             |> json_response(422)

    assert %{
             "error" => %{
               "code" => "validation_failed",
               "fields" => %{
                 "repository_resource_id" => ["must belong to the work item's project"]
               }
             }
           } =
             api_conn()
             |> patch("/portal/api/work-items/#{item["id"]}", %{
               "version" => assigned["version"],
               "repository_resource_id" => Ecto.UUID.generate()
             })
             |> json_response(422)
  end

  test "resource mutations refuse stale versions, referenced identities and unknown ids" do
    project = create_project("REFUSE")
    connection = create_connection("github", "Refusal binding", "acme", ["repositories"])
    azure = create_connection("azure_devops", "Azure refusal binding", "acme", ["work_items"])

    repository =
      create_resource(project, connection, "repository", "Referenced repository", "acme/symmetry")

    spare = create_resource(project, connection, "repository", "Spare repository", "acme/spare")

    create_work_item(project, "Referenced work", %{
      "repository_resource_id" => repository["id"]
    })

    assert %{"error" => %{"code" => "stale"}} =
             api_conn()
             |> patch("/portal/api/resources/#{repository["id"]}", %{
               "version" => repository["version"] + 1,
               "name" => "Stale rename"
             })
             |> json_response(409)

    assert %{"error" => %{"code" => "state_conflict"}} =
             api_conn()
             |> patch("/portal/api/resources/#{repository["id"]}", %{
               "version" => repository["version"],
               "external_ref" => "acme/renamed"
             })
             |> json_response(409)

    assert %{"error" => %{"code" => "state_conflict"}} =
             api_conn()
             |> delete(
               "/portal/api/resources/#{repository["id"]}?version=#{repository["version"]}"
             )
             |> json_response(409)

    # A name-only update of a referenced resource stays allowed.
    assert %{"resource" => renamed} =
             api_conn()
             |> patch("/portal/api/resources/#{repository["id"]}", %{
               "version" => repository["version"],
               "name" => "Renamed repository"
             })
             |> json_response(200)

    assert renamed["name"] == "Renamed repository"
    assert renamed["external_ref"] == "acme/symmetry"

    unknown = Ecto.UUID.generate()

    assert %{"error" => %{"code" => "not_found"}} =
             api_conn()
             |> patch("/portal/api/resources/#{unknown}", %{"version" => 1, "name" => "Nowhere"})
             |> json_response(404)

    assert %{"error" => %{"code" => "not_found"}} =
             api_conn()
             |> delete("/portal/api/resources/#{unknown}?version=1")
             |> json_response(404)

    # A connection-bound resource is validated by the provider module before the
    # changeset runs, so a blank reference answers the provider's own refusal
    # (400 invalid_request). This increment adds no wire surface and changes no
    # existing answer, so the pre-existing shape is pinned here rather than
    # reshaped into the changeset's field-error envelope.
    assert %{"error" => %{"code" => "invalid_request"}} =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/resources", %{
               "connection_id" => connection["id"],
               "kind" => "repository",
               "name" => "Missing reference"
             })
             |> json_response(400)

    assert %{"error" => %{"code" => "invalid_request"}} =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/resources", %{
               "connection_id" => azure["id"],
               "kind" => "work_tracking",
               "name" => "Azure missing reference"
             })
             |> json_response(400)

    # A non-blank reference still answers the provider's own refusal.
    assert %{"error" => %{"code" => "invalid_request"}} =
             api_conn()
             |> post("/portal/api/projects/#{project["id"]}/resources", %{
               "connection_id" => connection["id"],
               "kind" => "repository",
               "name" => "Malformed reference",
               "external_ref" => "not-a-repository"
             })
             |> json_response(400)

    assert %{"deleted_resource_id" => deleted_id} =
             api_conn()
             |> delete("/portal/api/resources/#{spare["id"]}?version=#{spare["version"]}")
             |> json_response(200)

    assert deleted_id == spare["id"]
    assert Enum.map(project_read(project["id"])["resources"], & &1["id"]) == [repository["id"]]
  end

  # The workspace-read gate: a real served document — never a fixture — is
  # validated against the canonical bundle AND every member the surface reads is
  # asserted present. The bundle comparison refuses a renamed member only where
  # the schema requires it, so the member lists above carry the
  # optional-but-consumed names, the class a serving-side rename would otherwise
  # leave green. `WorkspaceWorkItem` is one definition body across
  # `workspace-read`, `work-detail-read` and `chat-read`, and
  # `PortalJSON.work_item/2` is the one body that serves it: the provider-sourced
  # delivery values, the external row and the waiting packet pinned here are that
  # shared row's own shape.
  test "the real served workspace read carries every member the surface reads" do
    project = create_project("WSGATE")

    connection =
      create_connection("github", "Workspace gate binding", "acme", [
        "repositories",
        "changes",
        "ci",
        "work_items"
      ])

    repository =
      create_resource(
        project,
        connection,
        "repository",
        "Workspace gate repository",
        "acme/symmetry"
      )

    ci =
      create_resource(project, connection, "ci", "Workspace gate pipeline", "acme/symmetry-ci")

    tracker = create_tracker(project, connection, "Workspace gate tracker", "acme/issues")

    steer("Workspace gate tracker", {:ok, tracker_document(connection)})
    assert %{"resource" => _tracker_synced} = sync_resource(tracker["id"]) |> json_response(200)

    # The repository's own document: the `changes` capability makes the launch
    # derive a change scope from the repository's default branch, so the synced
    # metadata carries it.
    steer("Workspace gate repository", {:ok, repository_document(connection, "acme/symmetry")})

    steer_delivery("Workspace gate repository", {:ok, provider_delivery(@delivery_url)})

    runtime =
      enroll_adapter_runtime(
        "workspace-gate",
        "Workspace gate runtime",
        @gate_runtime_capabilities
      )

    item =
      create_work_item(project, "Workspace gate item", %{
        "assignee_type" => "agent",
        "agent_profile" => "codex",
        "workspace" => "primary"
      })

    assert %{"work_item" => bound} =
             api_conn()
             |> patch("/portal/api/work-items/#{item["id"]}", %{
               "version" => item["version"],
               "description" => "Served by the workspace gate.",
               "priority" => "urgent",
               "repository_resource_id" => repository["id"],
               "ci_resource_id" => ci["id"],
               "branch" => "codex/workspace-gate",
               "pull_request_url" => @delivery_seed_url,
               "review_status" => "required",
               "ci_status" => "pending",
               "blocked" => true,
               "blocker" => "Waiting on the operator decision"
             })
             |> json_response(200)

    assert %{"resource" => _repository_synced} =
             sync_resource(repository["id"]) |> json_response(200)

    # `execution` is served only for a work item with an attempt, and the
    # waiting packet only for a `waiting_for_input` attempt: the fixture drives
    # the durable states the shared row's optional members need.
    assert %{"execution" => %{"generation" => 1, "task_id" => task_id}} =
             api_conn()
             |> post("/portal/api/work-items/#{item["id"]}/run", %{
               "action_id" => "workspace-gate-run"
             })
             |> json_response(202)

    {run, fence} = claim_gate_attempt(runtime, task_id, 4_100)
    transition(run, fence, "running", 4_101)
    transition(run, fence, "waiting_for_input", 4_102, waiting_decision_payload())

    served =
      api_conn() |> get("/portal/api/workspace?project_id=#{project["id"]}") |> json_response(200)

    validate_served_document!("workspace-read", served)

    assert_served_members!("workspace-read root", served, @workspace_read_members)

    assert_served_rows!(
      "workspace-read project row",
      served["projects"],
      @workspace_project_members
    )

    [project_row] = served["projects"]

    assert_served_rows!(
      "workspace-read resource row",
      project_row["resources"],
      @workspace_resource_members
    )

    assert_served_rows!(
      "workspace-read work row",
      project_row["work_items"],
      @workspace_work_item_members
    )

    assert_served_rows!(
      "workspace-read runtime row",
      served["runtimes"],
      @workspace_runtime_members
    )

    assert_served_rows!(
      "workspace-read registered runtime row",
      served["registered_runtimes"],
      @workspace_registered_runtime_members
    )

    Enum.each(served["runtimes"], fn row ->
      assert_served_members!(
        "workspace-read runtime capabilities",
        row["capabilities"],
        @workspace_runtime_capability_members
      )
    end)

    delivery_item = Enum.find(project_row["work_items"], &(&1["id"] == bound["id"]))

    Enum.each(["pull_request", "ci", "review"], fn field ->
      value = delivery_item["delivery"][field]
      assert value["source"] == "provider"

      assert_served_members!(
        "workspace-read delivery #{field} value",
        value,
        @workspace_delivery_value_members
      )
    end)

    external_item = Enum.find(project_row["work_items"], &(&1["external"] != nil))
    assert external_item, "the gate scenario must serve an external row"
    assert external_item["external"]["provider"] == "github"

    assert_served_members!(
      "workspace-read external row",
      external_item["external"],
      @workspace_external_members
    )

    decision = delivery_item["execution"]["waiting"]["decision"]

    assert_served_members!(
      "workspace-read waiting decision",
      decision,
      @workspace_waiting_decision_members
    )

    assert_served_rows!(
      "workspace-read waiting decision option",
      decision["options"],
      @workspace_waiting_decision_option_members
    )
  end

  defp create_project(key), do: create_project(key, "Resource project #{key}")

  defp create_resource(project, connection, kind, name, external_ref) do
    %{"resource" => resource} =
      api_conn()
      |> post("/portal/api/projects/#{project["id"]}/resources", %{
        "connection_id" => connection["id"],
        "kind" => kind,
        "name" => name,
        "external_ref" => external_ref
      })
      |> json_response(201)

    resource
  end

  defp create_tracker(project, connection, name, external_ref) do
    create_resource(project, connection, "work_tracking", name, external_ref)
  end

  defp create_work_item(project, title, attrs \\ %{}) do
    %{"work_item" => work_item} =
      api_conn()
      |> post(
        "/portal/api/projects/#{project["id"]}/work-items",
        Map.merge(%{"title" => title}, attrs)
      )
      |> json_response(201)

    work_item
  end

  defp sync_resource(resource_id) do
    api_conn() |> post("/portal/api/resources/#{resource_id}/sync")
  end

  defp project_read(project_id) do
    %{"projects" => [project]} =
      api_conn()
      |> get("/portal/api/workspace?project_id=#{project_id}")
      |> json_response(200)

    project
  end

  defp served_items(project_id) do
    project_id
    |> project_read()
    |> Map.fetch!("work_items")
    |> Map.new(&{&1["external"]["id"], &1})
  end

  defp served_item(project_id, work_item_id) do
    project_id
    |> project_read()
    |> Map.fetch!("work_items")
    |> Enum.find(&(&1["id"] == work_item_id))
  end

  defp resource_snapshot(resource_id) do
    Repo.one(from resource in ProjectResource, where: resource.id == ^resource_id)
  end

  defp work_item_snapshot(work_item_id) do
    Repo.one(from item in WorkItem, where: item.id == ^work_item_id)
  end

  # An enrolled machine with one adapter-backed runtime for the project's
  # default binding: provider-capable because the retried item's binding
  # requires `provider_access`, and advertising the adapter `start` operation.
  defp enroll_adapter_runtime(key, name, capabilities \\ @adapter_runtime_capabilities) do
    enrollment_key = Ecto.UUID.generate()

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(
               %{name: "#{key}-machine", machine_token: "token-#{enrollment_key}"},
               enrollment_key,
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret"
             )

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               enrolled.machine.id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: "#{key}-runtime",
                   name: name,
                   capacity: 1,
                   agent_profile: "codex",
                   workspace: "primary",
                   harness_kind: "generic",
                   harness_version: "generic-v1",
                   adapter_version: "generic-v1",
                   adapter_protocol_version: 1,
                   capabilities: capabilities
                 }
               ]
             )

    runtime
  end

  # `assert_served_members!/3` and `assert_served_rows!/3` are the gate's
  # presence oracle: a member is pinned by key, so a served `null` stays
  # accepted while a renamed or omitted key fails here. They are defined in this
  # file because `control/test/support/**` is outside this increment's owned
  # files, and a module defined in one test file would break a single-file run
  # of the others.
  defp assert_served_members!(label, served, members) do
    assert is_map(served), "#{label} is not served at all: #{inspect(served)}"

    assert Enum.all?(members, &Map.has_key?(served, &1)),
           "#{label} stopped serving #{inspect(Enum.reject(members, &Map.has_key?(served, &1)))}"
  end

  defp assert_served_rows!(label, rows, members) do
    assert is_list(rows) and rows != [], "#{label}: the gate must serve at least one row"
    Enum.each(rows, &assert_served_members!(label, &1, members))
  end

  # Claims the gate item's queued attempt for the runtime the gate registered:
  # that runtime is the only one assignment can select for this test's binding,
  # so the picked attempt must be the gate item's own.
  defp claim_gate_attempt(runtime, task_id, claim_number) do
    assert {:ok, run} = Orchestration.assign_one()
    assert run.task_id == task_id

    assert {:ok, claimed} =
             Orchestration.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: uuid(claim_number)
               }
             )

    {run,
     %{
       runtime_id: runtime.id,
       runtime_epoch: runtime.connection_epoch,
       generation: run.generation,
       claim_id: claimed.claim_id,
       lease_token: claimed.lease_token
     }}
  end

  defp transition(run, fence, state, number, payload \\ %{}) do
    assert {:ok, _run} = Orchestration.transition(run.id, fence, state, payload, uuid(number))
  end

  # The agent's waiting packet. The read model serves the transition's payload as
  # the authoritative packet when it carries a decision
  # (`workspaces/read_model.ex:221`), so this is the packet the surface reads.
  defp waiting_decision_payload do
    %{
      "question" => "Which reviewer should accept this?",
      "decision" => %{
        "context" => "The attempt needs an operator decision before it continues.",
        "reason" => "consequential",
        "options" => [
          %{
            "id" => "reviewer-a",
            "label" => "Reviewer A",
            "consequence" => "Reviews the change first."
          },
          %{
            "id" => "reviewer-b",
            "label" => "Reviewer B",
            "consequence" => "Reviews the change second."
          }
        ],
        "recommended_option_id" => "reviewer-a"
      }
    }
  end

  # The tracker's own sync document: the shape `Integrations` writes an external
  # work item from.
  defp tracker_document(connection) do
    %{
      resource: %{
        url: provider_url(connection, "acme/issues"),
        metadata: %{"remote_kind" => "work_tracking"}
      },
      work_items: [provider_item("701", "https://github.com/acme/issues/701")]
    }
  end

  defp repository_document(connection, external_ref) do
    %{
      resource: %{
        url: provider_url(connection, external_ref),
        metadata: %{"remote_kind" => "repository", "default_branch" => "main"}
      },
      work_items: []
    }
  end

  # A deterministic version-4 UUID: the contract bundle's `UUID` pattern pins the
  # version and variant nibbles, so a zeroed UUID is refused by the gate's own
  # bundle comparison.
  defp uuid(number),
    do: "00000000-0000-4000-8000-" <> String.pad_leading(Integer.to_string(number), 12, "0")

  defp provider_item(external_id, external_url) do
    %{
      external_id: external_id,
      external_url: external_url,
      external_state: "open",
      external_updated_at: ~U[2026-09-05 09:30:00.000000Z],
      title: "External #{external_id}",
      description: "Steered provider item.",
      labels: ["agent-ready"],
      external_assignee_name: "Ada Lovelace",
      status: "ready",
      priority: "high",
      provider_data: %{"revision" => 7}
    }
  end

  # The provider-owned delivery answer: the pull request URL the provider
  # reports, its state, the CI and review verdicts, and the provider-side change
  # timestamp the delivery write stores as the recording time.
  defp provider_delivery(pull_request_url) do
    %{
      pull_request_url: pull_request_url,
      pull_request_state: "open",
      review_status: "approved",
      ci_status: "passed",
      updated_at: @delivery_updated_at,
      provider_data: %{
        "head_sha" => @delivery_head_sha,
        "pull_request_state" => "open",
        "ci_url" => @delivery_ci_url
      }
    }
  end

  defp steer(resource_name, answer) do
    steering = Application.get_env(:symmetry_control, :portal_resources_steering, %{})

    Application.put_env(
      :symmetry_control,
      :portal_resources_steering,
      Map.put(steering, resource_name, answer)
    )
  end

  # A delivery answer is steered under {resource name, :delivery} so it never
  # collides with the resource's own sync document.
  defp steer_delivery(resource_name, answer), do: steer({resource_name, :delivery}, answer)

  defp provider_url(%{"provider" => "github"}, external_ref),
    do: "https://github.com/" <> external_ref

  defp provider_url(%{"provider" => "azure_devops", "account_ref" => account_ref}, external_ref),
    do: "https://dev.azure.com/#{account_ref}/" <> external_ref
end
