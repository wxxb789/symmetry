defmodule SymmetryControl.Goals.HarnessSessions do
  @moduledoc false

  import Ecto.Query

  import SymmetryControl.Goals.Internal,
    only: [
      has_key?: 2,
      normalize_map: 1,
      now: 1,
      only_known_keys?: 2,
      optional_uuid: 2,
      plan_task?: 1,
      required_string: 2,
      required_uuid: 2,
      rollback: 1,
      stamp_insert: 2,
      stamp_update: 2,
      valid_uuid: 1,
      value: 2,
      value: 3
    ]

  import SymmetryControl.Goals.RunFence,
    only: [
      ensure_machine_fence!: 7,
      lock_goal_run!: 5,
      machine_session_stop_result: 1,
      machine_write_result: 1,
      task_repository_resource_id!: 2,
      valid_fence: 1
    ]

  alias Ecto.Changeset
  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Goals.HarnessSessionAttachReceipt
  alias SymmetryControl.Goals.HarnessSessionStopReceipt
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash

  @terminal_run_states ["completed", "failed", "cancelled"]
  @accounting_terminal_run_states @terminal_run_states ++ ["expired"]

  def attach_harness_session(machine_id, run_id, fence, attrs, opts \\ [])

  def attach_harness_session(machine_id, run_id, fence, attrs, opts)
      when is_binary(machine_id) and is_binary(run_id) and is_map(fence) and is_map(attrs) and is_list(opts) do
    with :ok <- valid_uuid(machine_id),
         :ok <- valid_uuid(run_id),
         :ok <- valid_fence(fence),
         {:ok, session_attrs} <- session_attrs(attrs) do
      fn ->
        {goal, task, item, run, runtime} =
          lock_goal_run!(machine_id, run_id, fence, opts, :attach_replay)

        session_mode = value(task.input || %{}, "session_mode", "fresh")
        request_hash = RequestHash.canonical(session_attach_request(fence, session_attrs))

        case lock_attach_receipt(run.id) do
          %HarnessSessionAttachReceipt{} = receipt ->
            if attach_receipt_matches?(receipt, machine_id, fence, request_hash) do
              {:replayed, attachment_receipt_response(receipt)}
            else
              rollback(:idempotency_conflict)
            end

          nil ->
            ensure_current_attachment!(goal, task, run, runtime, machine_id, fence, opts)
            ensure_attachment_binding_shape!(session_mode, run, session_attrs)
            ensure_session_scope!(session_attrs, task, item, run, runtime, machine_id)

            session =
              case session_mode do
                "resume" ->
                  attach_reserved_session!(task, run, runtime, item, machine_id, session_attrs)

                "fresh" ->
                  attach_fresh_session!(task, item, run, runtime, machine_id, session_attrs, opts)

                "handoff" ->
                  attach_handoff_session!(
                    task,
                    item,
                    run,
                    runtime,
                    machine_id,
                    session_attrs,
                    opts
                  )

                _ ->
                  rollback(:invalid_request)
              end

            response =
              create_attach_receipt!(
                goal,
                task,
                run,
                session,
                machine_id,
                fence,
                request_hash,
                opts
              )

            {:created, response}
        end
      end
      |> Repo.transaction()
      |> machine_write_result()
    end
  end

  def attach_harness_session(_, _, _, _, _), do: {:error, :invalid_request}

  def fetch_harness_session_attachment(machine_id, run_id, fence)
      when is_binary(machine_id) and is_binary(run_id) and is_map(fence) do
    with :ok <- valid_uuid(machine_id), :ok <- valid_uuid(run_id), :ok <- valid_fence(fence) do
      fn ->
        {_goal, _task, _item, run, _runtime} =
          lock_goal_run!(machine_id, run_id, fence, [], :attach_replay)

        receipt = lock_attach_receipt(run.id) || rollback(:not_found)

        if attach_receipt_fence_matches?(receipt, machine_id, fence),
          do: attachment_receipt_response(receipt),
          else: rollback(:ownership_lost)
      end
      |> Repo.transaction()
      |> case do
        {:ok, receipt} -> {:ok, receipt}
        {:error, reason} -> {:error, reason}
      end
    end
  end

  def fetch_harness_session_attachment(_, _, _), do: {:error, :invalid_request}

  def mark_harness_session_stopped(machine_id, run_id, fence, attrs, opts \\ [])

  def mark_harness_session_stopped(machine_id, run_id, fence, attrs, opts)
      when is_binary(machine_id) and is_binary(run_id) and is_map(fence) and is_map(attrs) and is_list(opts) do
    with :ok <- valid_uuid(machine_id),
         :ok <- valid_uuid(run_id),
         :ok <- valid_fence(fence),
         {:ok, stop_attrs} <- session_stop_attrs(attrs) do
      fn ->
        {_goal, _task, _item, run, runtime} =
          lock_goal_run!(machine_id, run_id, fence, opts, :session_stop)

        if run.state not in @accounting_terminal_run_states, do: rollback(:state_conflict)

        session =
          Repo.one(
            from(session in HarnessSession,
              where: session.id == ^stop_attrs.session_id,
              lock: "FOR UPDATE"
            )
          ) || rollback(:ownership_lost)

        request_hash = RequestHash.canonical(session_stop_request(fence, stop_attrs))

        case Repo.one(
               from(receipt in HarnessSessionStopReceipt,
                 where:
                   receipt.session_id == ^session.id and
                     receipt.binding_id == ^stop_attrs.binding_id,
                 lock: "FOR UPDATE"
               )
             ) do
          %HarnessSessionStopReceipt{} = receipt ->
            if stop_receipt_matches?(receipt, machine_id, run, request_hash) do
              {:replayed, receipt.response}
            else
              rollback(:idempotency_conflict)
            end

          nil ->
            ensure_stoppable_session!(session, run, runtime, machine_id, stop_attrs)

            receipt_id = Ecto.UUID.generate()

            response =
              normalize_map(session_stop_receipt(receipt_id, session, run, stop_attrs.binding_id))

            %HarnessSessionStopReceipt{id: receipt_id}
            |> HarnessSessionStopReceipt.changeset(%{
              session_id: session.id,
              run_id: run.id,
              machine_id: machine_id,
              binding_id: stop_attrs.binding_id,
              request_hash: request_hash,
              response: normalize_map(response)
            })
            |> stamp_insert(now(opts))
            |> Repo.insert!()

            session
            |> HarnessSession.update_changeset(%{state: "available", active_run_id: nil})
            |> stamp_update(now(opts))
            |> Repo.update!()

            {:created, response}
        end
      end
      |> Repo.transaction()
      |> machine_session_stop_result()
    end
  end

  def mark_harness_session_stopped(_, _, _, _, _), do: {:error, :invalid_request}

  defp session_attrs(attrs) do
    allowed = [
      "local_handle_id",
      "binding_id",
      "harness_kind",
      "harness_version",
      "adapter_version",
      "workspace_fingerprint",
      "workspace",
      "repository_resource_id"
    ]

    with true <- only_known_keys?(attrs, allowed),
         {:ok, local_handle_id} <- required_uuid(attrs, :local_handle_id),
         {:ok, binding_id} <- optional_uuid(attrs, :binding_id),
         {:ok, harness_kind} <- required_string(attrs, :harness_kind),
         true <- Orchestration.native_harness_kind?(harness_kind),
         {:ok, harness_version} <- required_string(attrs, :harness_version),
         {:ok, adapter_version} <- required_string(attrs, :adapter_version),
         {:ok, workspace_fingerprint} <- required_string(attrs, :workspace_fingerprint),
         {:ok, workspace} <- required_string(attrs, :workspace),
         {:ok, repository_resource_id} <- optional_uuid(attrs, :repository_resource_id) do
      {:ok,
       %{
         local_handle_id: local_handle_id,
         binding_id: binding_id,
         binding_supplied?: has_key?(attrs, :binding_id),
         harness_kind: harness_kind,
         harness_version: harness_version,
         adapter_version: adapter_version,
         workspace_fingerprint: workspace_fingerprint,
         workspace: workspace,
         repository_resource_id: repository_resource_id
       }}
    else
      _ -> {:error, :invalid_request}
    end
  end

  defp session_stop_attrs(attrs) do
    allowed = ["session_id", "local_handle_id", "binding_id"]

    with true <- only_known_keys?(attrs, allowed),
         {:ok, session_id} <- required_uuid(attrs, :session_id),
         {:ok, local_handle_id} <- required_uuid(attrs, :local_handle_id),
         {:ok, binding_id} <- required_uuid(attrs, :binding_id) do
      {:ok,
       %{
         session_id: session_id,
         local_handle_id: local_handle_id,
         binding_id: binding_id
       }}
    else
      _ -> {:error, :invalid_request}
    end
  end

  defp session_stop_request(fence, attrs) do
    %{
      "runtime_id" => value(fence, :runtime_id),
      "runtime_epoch" => value(fence, :runtime_epoch),
      "generation" => value(fence, :generation),
      "claim_id" => value(fence, :claim_id),
      "lease_token" => value(fence, :lease_token),
      "session_id" => attrs.session_id,
      "local_handle_id" => attrs.local_handle_id,
      "binding_id" => attrs.binding_id
    }
  end

  defp session_attach_request(fence, attrs) do
    request = %{
      "runtime_id" => value(fence, :runtime_id),
      "runtime_epoch" => value(fence, :runtime_epoch),
      "generation" => value(fence, :generation),
      "claim_id" => value(fence, :claim_id),
      "lease_token" => value(fence, :lease_token),
      "local_handle_id" => attrs.local_handle_id,
      "harness_kind" => attrs.harness_kind,
      "harness_version" => attrs.harness_version,
      "adapter_version" => attrs.adapter_version,
      "workspace_fingerprint" => attrs.workspace_fingerprint,
      "workspace" => attrs.workspace,
      "repository_resource_id" => attrs.repository_resource_id
    }

    if attrs.binding_supplied?,
      do: Map.put(request, "binding_id", attrs.binding_id),
      else: request
  end

  defp ensure_stoppable_session!(session, run, runtime, machine_id, attrs) do
    stoppable? =
      session.machine_id == machine_id and session.runtime_id == runtime.id and
        session.id == run.harness_session_id and run.harness_binding_id == attrs.binding_id and
        session.local_handle_id == attrs.local_handle_id and
        session.binding_id == attrs.binding_id and session.binding_verified and
        session.state == "unavailable" and
        is_nil(session.active_run_id)

    if !stoppable?, do: rollback(:ownership_lost)
  end

  defp stop_receipt_matches?(receipt, machine_id, run, request_hash) do
    receipt.machine_id == machine_id and receipt.run_id == run.id and
      receipt.request_hash == request_hash
  end

  defp ensure_session_scope!(attrs, task, item, run, runtime, machine_id) do
    repository_resource_id = task_repository_resource_id!(task, item)

    if !(attrs.workspace == task.workspace and
           (is_nil(attrs.repository_resource_id) or
              attrs.repository_resource_id == repository_resource_id) and
           runtime.repository_resource_id == repository_resource_id and
           runtime.id == run.runtime_id and runtime.machine_id == machine_id and
           runtime.harness_kind == attrs.harness_kind and
           runtime.harness_version == attrs.harness_version and
           runtime.adapter_version == attrs.adapter_version) do
      rollback(:ownership_lost)
    end
  end

  defp ensure_current_attachment!(goal, task, run, runtime, machine_id, fence, opts) do
    if !(task.goal_revision == goal.current_revision and
           ((goal.state == "draft" and plan_task?(task)) or
              (goal.state == "active" and task.purpose != "plan"))),
       do: rollback(:ownership_lost)

    ensure_machine_fence!(machine_id, task, run, runtime, fence, :current, now(opts))
  end

  # The admission mode, not the caller, decides binding authority: fresh/handoff
  # carry no server-issued identity; resume may only echo the reserved pair.
  defp ensure_attachment_binding_shape!("resume", run, attrs) do
    if !attrs.binding_supplied?, do: rollback(:invalid_request)

    if !(is_binary(run.harness_session_id) and attrs.binding_id == run.harness_binding_id) do
      rollback(:ownership_lost)
    end
  end

  defp ensure_attachment_binding_shape!(mode, run, attrs) when mode in ["fresh", "handoff"] do
    if attrs.binding_supplied?, do: rollback(:invalid_request)

    if not is_nil(run.harness_session_id) or not is_nil(run.harness_binding_id) do
      rollback(:ownership_lost)
    end
  end

  defp ensure_attachment_binding_shape!(_, _run, _attrs), do: rollback(:invalid_request)

  defp attach_fresh_session!(task, item, run, runtime, machine_id, attrs, opts) do
    reject_existing_handle!(machine_id, attrs.local_handle_id, :idempotency_conflict)
    create_fresh_harness_session!(task, item, run, runtime, machine_id, attrs, opts)
  end

  defp attach_handoff_session!(task, item, run, runtime, machine_id, attrs, opts) do
    reject_existing_handle!(machine_id, attrs.local_handle_id, :handoff_session_reuse)
    create_fresh_harness_session!(task, item, run, runtime, machine_id, attrs, opts)
  end

  defp reject_existing_handle!(machine_id, local_handle_id, reason) do
    if Repo.exists?(
         from(session in HarnessSession,
           where: session.machine_id == ^machine_id and session.local_handle_id == ^local_handle_id,
           lock: "FOR KEY SHARE"
         )
       ),
       do: rollback(reason)
  end

  defp create_fresh_harness_session!(task, item, run, runtime, machine_id, attrs, opts) do
    binding_id = Ecto.UUID.generate()

    changeset =
      %HarnessSession{}
      |> HarnessSession.changeset(
        attrs
        |> Map.delete(:workspace)
        |> Map.merge(%{
          machine_id: machine_id,
          runtime_id: runtime.id,
          repository_resource_id: task_repository_resource_id!(task, item),
          harness_kind: runtime.harness_kind,
          harness_version: runtime.harness_version,
          adapter_version: runtime.adapter_version,
          binding_id: binding_id,
          binding_verified: true,
          state: "busy",
          active_run_id: run.id
        })
      )
      |> stamp_insert(now(opts))

    case Repo.insert(changeset) do
      {:ok, session} ->
        attach_session_to_run!(run, session, opts)
        session

      {:error, _changeset} ->
        rollback(:idempotency_conflict)
    end
  end

  defp attach_reserved_session!(task, run, runtime, item, machine_id, attrs) do
    session =
      Repo.one(
        from(session in HarnessSession,
          where: session.id == ^task.requested_session_id,
          lock: "FOR UPDATE"
        )
      ) || rollback(:requested_session_not_found)

    compatible? =
      session.machine_id == machine_id and session.runtime_id == runtime.id and
        session.repository_resource_id == task_repository_resource_id!(task, item) and
        session.local_handle_id == attrs.local_handle_id and
        session.harness_kind == attrs.harness_kind and
        session.harness_version == attrs.harness_version and
        session.adapter_version == attrs.adapter_version and
        session.workspace_fingerprint == attrs.workspace_fingerprint and session.binding_verified and
        session.state == "busy" and session.active_run_id == run.id and
        session.id == run.harness_session_id and session.binding_id == run.harness_binding_id

    if !compatible?, do: rollback(:ownership_lost)
    session
  end

  defp attach_session_to_run!(run, session, opts) do
    cond do
      is_nil(run.harness_session_id) and is_nil(run.harness_binding_id) ->
        run
        |> Changeset.change(
          harness_session_id: session.id,
          harness_binding_id: session.binding_id
        )
        |> stamp_update(now(opts))
        |> Repo.update!()

      run.harness_session_id == session.id and run.harness_binding_id == session.binding_id ->
        :ok

      true ->
        rollback(:ownership_lost)
    end
  end

  defp lock_attach_receipt(run_id) do
    Repo.one(
      from(receipt in HarnessSessionAttachReceipt,
        where: receipt.run_id == ^run_id,
        lock: "FOR UPDATE"
      )
    )
  end

  defp attach_receipt_matches?(receipt, machine_id, fence, request_hash) do
    receipt.request_hash == request_hash and
      attach_receipt_fence_matches?(receipt, machine_id, fence)
  end

  defp attach_receipt_fence_matches?(receipt, machine_id, fence) do
    receipt.machine_id == machine_id and
      receipt.runtime_id == value(fence, :runtime_id) and
      receipt.runtime_epoch == value(fence, :runtime_epoch) and
      receipt.generation == value(fence, :generation) and
      receipt.claim_id == value(fence, :claim_id) and
      receipt.lease_token == value(fence, :lease_token)
  end

  defp create_attach_receipt!(goal, task, run, session, machine_id, fence, request_hash, opts) do
    receipt_id = Ecto.UUID.generate()
    response = session_receipt(receipt_id, goal, task, run, session)

    %HarnessSessionAttachReceipt{id: receipt_id}
    |> HarnessSessionAttachReceipt.changeset(%{
      session_id: session.id,
      run_id: run.id,
      runtime_id: value(fence, :runtime_id),
      machine_id: machine_id,
      runtime_epoch: value(fence, :runtime_epoch),
      generation: value(fence, :generation),
      claim_id: value(fence, :claim_id),
      lease_token: value(fence, :lease_token),
      request_hash: request_hash,
      response: normalize_map(response)
    })
    |> stamp_insert(now(opts))
    |> Repo.insert!()

    response
  end

  # Receipt storage stays JSONB string-keyed while direct callers expect atoms:
  # decode only the fixed envelope, never arbitrary persisted keys.
  defp attachment_receipt_response(%HarnessSessionAttachReceipt{response: %{"session" => session}})
       when is_map(session) do
    fields = [
      {:attachment_receipt_id, "attachment_receipt_id"},
      {:id, "id"},
      {:session_id, "session_id"},
      {:goal_id, "goal_id"},
      {:task_id, "task_id"},
      {:run_id, "run_id"},
      {:runtime_id, "runtime_id"},
      {:machine_id, "machine_id"},
      {:repository_resource_id, "repository_resource_id"},
      {:active_run_id, "active_run_id"},
      {:local_handle_id, "local_handle_id"},
      {:binding_id, "binding_id"},
      {:harness_kind, "harness_kind"},
      {:harness_version, "harness_version"},
      {:adapter_version, "adapter_version"},
      {:state, "state"},
      {:workspace_fingerprint, "workspace_fingerprint"},
      {:workspace, "workspace"}
    ]

    receipt_session =
      Enum.reduce(fields, %{}, fn {field, key}, acc ->
        Map.put(acc, field, Map.fetch!(session, key))
      end)

    %{session: receipt_session}
  end

  defp attachment_receipt_response(_receipt), do: rollback(:ownership_lost)

  # A terminal Run does not prove the retained session stopped, so without a
  # durable session_stopped receipt keep it unavailable against reuse.
  def mark_harness_session_unavailable!(%Run{harness_session_id: nil}, _opts), do: :ok

  def mark_harness_session_unavailable!(run, opts) do
    session =
      Repo.one(
        from(session in HarnessSession,
          where: session.id == ^run.harness_session_id,
          lock: "FOR UPDATE"
        )
      )

    if session && session.state == "busy" && session.active_run_id == run.id &&
         session.binding_id == run.harness_binding_id do
      session
      |> HarnessSession.update_changeset(%{state: "unavailable", active_run_id: nil})
      |> stamp_update(now(opts))
      |> Repo.update!()
    else
      :ok
    end
  end

  defp session_receipt(receipt_id, goal, task, run, session) do
    %{
      session: %{
        attachment_receipt_id: receipt_id,
        id: session.id,
        session_id: session.id,
        goal_id: goal.id,
        task_id: task.id,
        run_id: run.id,
        runtime_id: session.runtime_id,
        machine_id: session.machine_id,
        repository_resource_id: session.repository_resource_id,
        active_run_id: session.active_run_id,
        local_handle_id: session.local_handle_id,
        binding_id: session.binding_id,
        harness_kind: session.harness_kind,
        harness_version: session.harness_version,
        adapter_version: session.adapter_version,
        state: session.state,
        workspace_fingerprint: session.workspace_fingerprint,
        workspace: task.workspace
      }
    }
  end

  defp session_stop_receipt(receipt_id, session, run, binding_id) do
    %{
      session_stopped: %{
        receipt_id: receipt_id,
        run_id: run.id,
        session_id: session.id,
        local_handle_id: session.local_handle_id,
        binding_id: binding_id,
        state: "available",
        active_run_id: nil,
        lock_version: session.lock_version + 1
      }
    }
  end
end
