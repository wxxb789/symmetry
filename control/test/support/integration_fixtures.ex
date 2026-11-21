defmodule SymmetryControl.IntegrationFixtures do
  @moduledoc """
  Durable fixtures for the integration sync tests: a fresh project, its
  GitHub connection and a bound work-tracking resource, plus the provider
  documents and lock-observation helpers the sync suite drives them with.

  The lock helpers run their own transactions in unboxed sandbox connections,
  so the two syncs they start really contend on `work_items`; the sampler
  reads the lock manager (`pg_stat_activity`, `pg_locks`), not the statement
  text.
  """
  import ExUnit.Assertions
  import ExUnit.Callbacks, only: [on_exit: 1]

  alias Ecto.Adapters.SQL.Sandbox
  alias SymmetryControl.Integrations
  alias SymmetryControl.Integrations.CredentialLeakProviderStub
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces

  @doc """
  Creates project/connection/resource; `opts` requires `:project_name`,
  `:project_key`, `:connection_name`, `:resource_name`. Returns
  `{project, connection, resource}`.
  """
  def tracked_resource!(opts) do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: Keyword.fetch!(opts, :project_name),
               key: Keyword.fetch!(opts, :project_key),
               default_agent_profile: "codex",
               default_workspace: "primary"
             })

    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: Keyword.fetch!(opts, :connection_name),
               account_ref: "acme",
               auth_type: "gh_cli",
               capabilities: ["work_items"]
             })

    assert {:ok, resource} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "work_tracking",
               name: Keyword.fetch!(opts, :resource_name),
               external_ref: "acme/symmetry"
             })

    {project, connection, resource}
  end

  @doc """
  Starts one sync in its own unboxed sandbox connection, reports the backend
  and waits at the prelock barrier until the caller sends `{:start_sync, ref}`.
  """
  def sync_lock_order_task(resource, provider_items, owner, ref) do
    Task.async(fn ->
      Sandbox.unboxed_run(Repo, fn ->
        [[backend]] = Repo.query!("SELECT pg_backend_pid()").rows
        send(owner, {:sync_backend, ref, self(), backend})

        receive do
          {:start_sync, ^ref} ->
            CredentialLeakProviderStub.respond_with(:sync_resource, %{
              resource: %{metadata: %{}},
              work_items: provider_items
            })

            Integrations.sync_resource(resource.id)
        after
          15_000 -> {:error, :sync_barrier_timeout}
        end
      end)
    end)
  end

  @doc """
  `FOR UPDATE SKIP LOCKED` reports a row locked by an in-progress transaction
  without waiting; the lock a free row takes is released with the statement.
  """
  def work_item_row_locked?(id) do
    Repo.query!(
      "SELECT 1 FROM work_items WHERE id = $1::uuid FOR UPDATE SKIP LOCKED",
      [Ecto.UUID.dump!(id)]
    ).rows == []
  end

  @doc """
  Waits until the import's backend holds an ungranted lock the delivery
  backend blocks, then asserts the delivery backend holds a granted lock on
  `work_items`, the lock the import's ordered prelock waits for.

  The sync's wait is read from the lock manager, not from the statement text:
  run 43891279765 job 133383740181 put `pg_stat_activity.query` on the import's
  repository lookup while the import was in a delivery-attributed lock wait, so
  the query field is not usable evidence here. Anything else keeps polling
  until the deadline, which reports the raw sample.
  """
  def assert_sync_waiting_on_work_item(backend, blocker, deadline) do
    sample = sync_wait_sample(backend, blocker)

    if sample.ungranted_lock? and sample.blocked_by_delivery? do
      assert sample.waiter_on_delivery_xid? and sample.blocker_holds_work_item_lock?,
             "import sync did not wait on the delivery transaction's WorkItem locks" <>
               " (#{format_sync_wait_sample(sample)})"

      :ok
    else
      assert System.monotonic_time(:millisecond) < deadline,
             "import sync did not wait for the delivery WorkItem locks" <>
               " (last sample: #{format_sync_wait_sample(sample)})"

      assert_sync_waiting_on_work_item(backend, blocker, deadline)
    end
  end

  defp sync_wait_sample(backend, blocker) do
    result =
      Repo.query!(
        """
        SELECT activity.query,
               coalesce(activity.wait_event_type, '-'),
               coalesce(activity.wait_event, '-'),
               $2::integer = ANY(pg_blocking_pids($1)),
               EXISTS (SELECT 1 FROM pg_locks WHERE pid = $1 AND NOT granted),
               EXISTS (
                 SELECT 1 FROM pg_locks held
                 WHERE held.pid = $2 AND held.granted AND held.relation = 'work_items'::regclass
               ),
               EXISTS (
                 SELECT 1 FROM pg_locks waiter
                 WHERE waiter.pid = $1 AND NOT waiter.granted
                   AND waiter.transactionid =
                         (SELECT backend_xid FROM pg_stat_activity WHERE pid = $2)
               )
        FROM pg_stat_activity AS activity
        WHERE activity.pid = $1
        """,
        [backend, blocker]
      )

    case result.rows do
      [[query, wait_event_type, wait_event, blocked_by_delivery?, ungranted?, holds?, on_xid?]] ->
        %{
          query: query,
          wait_event_type: wait_event_type,
          wait_event: wait_event,
          blocked_by_delivery?: blocked_by_delivery?,
          ungranted_lock?: ungranted?,
          blocker_holds_work_item_lock?: holds?,
          waiter_on_delivery_xid?: on_xid?
        }

      [] ->
        %{
          query: nil,
          wait_event_type: "absent",
          wait_event: "absent",
          blocked_by_delivery?: false,
          ungranted_lock?: false,
          blocker_holds_work_item_lock?: false,
          waiter_on_delivery_xid?: false
        }
    end
  end

  # The query can be NULL (or held by another backend's activity slot) while the
  # backend still waits, so the sample always names the wait event too.
  defp format_sync_wait_sample(sample) do
    query =
      case sample.query do
        nil -> ""
        value -> " query=" <> String.slice(to_string(value), 0, 160)
      end

    "#{sample.wait_event_type}/#{sample.wait_event}" <>
      " ungranted=#{sample.ungranted_lock?}" <>
      " waiter_on_delivery_xid=#{sample.waiter_on_delivery_xid?}" <>
      " blocker_holds_work_items=#{sample.blocker_holds_work_item_lock?}" <> query
  end

  @doc """
  Points both providers at `CredentialLeakProviderStub`, the credential-leak
  stub this module defines.
  """
  def use_credential_leak_provider do
    Application.put_env(:symmetry_control, :integration_providers,
      github: CredentialLeakProviderStub,
      azure_devops: CredentialLeakProviderStub
    )
  end

  @doc """
  The credential-leak stub plus this process as the connection-check
  controller, restored on exit.
  """
  def use_blocking_connection_check_provider do
    use_credential_leak_provider()
    previous = Application.get_env(:symmetry_control, :integration_provider_test_controller)
    Application.put_env(:symmetry_control, :integration_provider_test_controller, self())

    on_exit(fn ->
      if previous do
        Application.put_env(:symmetry_control, :integration_provider_test_controller, previous)
      else
        Application.delete_env(:symmetry_control, :integration_provider_test_controller)
      end
    end)
  end

  @doc "One provider work item the credential tests sync."
  def provider_work_item(provider_data) do
    %{
      external_id: Ecto.UUID.generate(),
      external_url: "https://github.com/acme/symmetry/issues/credential-check",
      external_state: "open",
      external_updated_at: ~U[2026-09-05 09:30:00.000000Z],
      title: "Provider credential validation",
      description: "Provider data must remain credential-free.",
      labels: ["security"],
      external_assignee_name: nil,
      status: "ready",
      priority: "high",
      provider_data: provider_data
    }
  end

  @doc "One provider sync document for the given provider and work item fields."
  def provider_snapshot(provider, updated_at, title, external_state, provider_data) do
    base_url =
      case provider do
        "github" -> "https://github.com/acme/symmetry/issues/101"
        "azure_devops" -> "https://dev.azure.com/acme/symmetry/_workitems/edit/101"
      end

    %{
      resource: %{metadata: %{}},
      work_items: [
        %{
          external_id: "101",
          external_url: base_url,
          external_state: external_state,
          external_updated_at: updated_at,
          title: title,
          description: title <> " description",
          labels: [String.downcase(title)],
          external_assignee_name: title <> " owner",
          status: "ready",
          priority: "high",
          provider_data: provider_data
        }
      ]
    }
  end
end

defmodule SymmetryControl.Integrations.CredentialLeakProviderStub do
  @moduledoc false
  @behaviour SymmetryControl.Integrations.Provider

  alias SymmetryControl.Integrations.ProviderStub

  def respond_with(kind, value), do: Process.put({__MODULE__, kind}, value)

  @impl true
  def authenticate(connection), do: ProviderStub.authenticate(connection)

  @impl true
  def validate_resource_reference(connection, kind, reference),
    do: ProviderStub.validate_resource_reference(connection, kind, reference)

  @impl true
  def check(%{name: "Blocking failure"}, _auth) do
    controller =
      Application.fetch_env!(:symmetry_control, :integration_provider_test_controller)

    send(controller, {:integration_connection_check_waiting, self()})

    receive do
      :continue_failure -> {:error, {:transport, :forced_failure}}
    after
      5_000 -> {:error, {:transport, :test_timeout}}
    end
  end

  def check(connection, auth) do
    case Process.get({__MODULE__, :check}) do
      nil -> ProviderStub.check(connection, auth)
      metadata -> {:ok, metadata}
    end
  end

  @impl true
  def sync_resource(connection, resource, auth) do
    case Process.get({__MODULE__, :sync_resource}) do
      nil -> ProviderStub.sync_resource(connection, resource, auth)
      result -> {:ok, result}
    end
  end

  @impl true
  def sync_delivery(connection, resource, work_item, auth) do
    case Process.get({__MODULE__, :sync_delivery}) do
      nil -> ProviderStub.sync_delivery(connection, resource, work_item, auth)
      delivery -> {:ok, delivery}
    end
  end

  @impl true
  def sync_ci(connection, resource, work_item, auth),
    do: ProviderStub.sync_ci(connection, resource, work_item, auth)

  @impl true
  def execute(connection, resource, work_item, operation, input, auth) do
    case Process.get({__MODULE__, :execute}) do
      nil -> ProviderStub.execute(connection, resource, work_item, operation, input, auth)
      result -> result
    end
  end
end
