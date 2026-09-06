defmodule SymmetryControl.Integrations.ProviderStub do
  @moduledoc false
  @behaviour SymmetryControl.Integrations.Provider

  @impl true
  def authenticate(_connection), do: {:ok, :stub_auth}

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
  def check(%{name: "Forbidden"}, :stub_auth), do: {:error, {:http, 403, "forbidden"}}
  def check(%{name: "Unauthorized"}, :stub_auth), do: {:error, {:http, 401, "unauthorized"}}

  def check(connection, :stub_auth) do
    {:ok,
     %{
       "account" => connection.account_ref,
       "actor" => connection.provider <> "-actor"
     }}
  end

  @impl true
  def sync_resource(%{name: "Malformed"}, _resource, :stub_auth) do
    {:ok,
     %{
       resource: %{},
       work_items: [
         %{
           external_id: "malformed",
           external_url: "https://github.com/acme/symmetry/issues/999",
           external_state: "open",
           external_updated_at: ~U[2026-09-05 09:30:00.000000Z],
           title: String.duplicate("x", 256),
           description: "Oversized provider title",
           labels: [],
           external_assignee_name: nil,
           status: "ready",
           priority: "no_priority",
           provider_data: %{}
         }
       ]
     }}
  end

  def sync_resource(%{name: "Empty work items"}, %{kind: "work_tracking"}, :stub_auth) do
    {:ok, %{resource: %{metadata: %{"remote_kind" => "work_tracking"}}, work_items: []}}
  end

  def sync_resource(%{name: "Fail after archive"}, _resource, :stub_auth) do
    controller =
      Application.fetch_env!(:symmetry_control, :integration_provider_test_controller)

    send(controller, {:integration_provider_waiting, self()})

    receive do
      :continue -> {:error, {:transport, :forced_failure}}
    after
      5_000 -> {:error, {:transport, :test_timeout}}
    end
  end

  def sync_resource(connection, %{kind: "work_tracking"} = resource, :stub_auth) do
    {:ok,
     %{
       resource: %{
         url: resource_url(connection.provider, resource.external_ref),
         metadata: %{"remote_kind" => "work_tracking"}
       },
       work_items: [
         %{
           external_id: "101",
           external_url: resource_url(connection.provider, resource.external_ref) <> "/items/101",
           external_state: "open",
           external_updated_at: ~U[2026-09-05 09:30:00.000000Z],
           title: "External work stays authoritative",
           description: "Imported from the connected provider.",
           labels: ["agent-ready", "priority:high"],
           external_assignee_name: "Ada Lovelace",
           status: "ready",
           priority: "high",
           provider_data: %{"revision" => 7}
         }
       ]
     }}
  end

  def sync_resource(connection, %{kind: kind} = resource, :stub_auth)
      when kind in ["repository", "ci"] do
    {:ok,
     %{
       resource: %{
         url: resource_url(connection.provider, resource.external_ref),
         metadata: %{"remote_kind" => kind, "default_branch" => "main"}
       },
       work_items: []
     }}
  end

  @impl true
  def sync_delivery(_connection, _resource, %{pull_request_url: nil}, :stub_auth),
    do: {:ok, nil}

  def sync_delivery(%{name: "Malformed delivery"}, _resource, work_item, :stub_auth) do
    {:ok,
     %{
       pull_request_url: work_item.pull_request_url,
       pull_request_state: "open",
       provider_data: "not-a-map"
     }}
  end

  def sync_delivery(%{name: "Wait for delivery"}, _resource, work_item, :stub_auth) do
    controller =
      Application.fetch_env!(:symmetry_control, :integration_provider_test_controller)

    send(controller, {:integration_delivery_waiting, self()})

    receive do
      :continue ->
        {:ok,
         %{
           pull_request_url: work_item.pull_request_url,
           pull_request_state: "open",
           review_status: "approved",
           updated_at: ~U[2026-09-05 09:35:00.000000Z],
           provider_data: %{"head_sha" => "stale-head"}
         }}
    after
      5_000 -> {:error, {:transport, :test_timeout}}
    end
  end

  def sync_delivery(connection, _resource, work_item, :stub_auth) do
    delivery = %{
      pull_request_url: work_item.pull_request_url,
      pull_request_state: "open",
      review_status: "approved",
      updated_at: ~U[2026-09-05 09:35:00.000000Z],
      provider_data: %{"head_sha" => "abc123"}
    }

    delivery =
      if "ci" in connection.capabilities,
        do: Map.put(delivery, :ci_status, "passed"),
        else: delivery

    {:ok, delivery}
  end

  @impl true
  def sync_ci(_connection, _resource, _work_item, :stub_auth) do
    {:ok,
     %{
       ci_status: "passed",
       updated_at: ~U[2026-09-05 09:36:00.000000Z],
       provider_data: %{"ci_url" => "https://ci.example/build/9"}
     }}
  end

  @impl true
  def execute(connection, _resource, work_item, operation, input, :stub_auth)
      when operation in ["change.upsert", "change.update"] and is_map(input) do
    pull_request_url =
      input["pull_request_url"] || work_item.pull_request_url ||
        work_item.external_pull_request_url || provider_pull_request_url(connection.provider)

    {:ok,
     %{
       pull_request_url: pull_request_url,
       pull_request_state: "open",
       review_status: "required",
       updated_at: ~U[2026-09-05 09:37:00.000000Z],
       provider_data: %{"head_sha" => "action-abc123"}
     }}
  end

  defp resource_url("github", external_ref), do: "https://github.com/" <> external_ref

  defp resource_url("azure_devops", external_ref),
    do: "https://dev.azure.com/acme/" <> external_ref

  defp provider_pull_request_url("github"),
    do: "https://github.com/acme/symmetry/pull/42"

  defp provider_pull_request_url("azure_devops"),
    do: "https://dev.azure.com/acme/symmetry/_git/symmetry/pullrequest/42"
end
