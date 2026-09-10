defmodule SymmetryControl.Goals do
  @moduledoc """
  PostgreSQL-backed Goal transition policy.

  This context is the only writer for Goal lifecycle, revision, plan, admission,
  decision and acceptance records.  It deliberately does not call Orchestration:
  goal-aware writes acquire the Goal lock first, while Orchestration owns the
  inverse Task/Run lifecycle.  Terminal Task handling reaches `settle_task/4`
  only after the orchestration transaction has committed.
  """

  import Ecto.Query

  alias Ecto.Changeset

  alias SymmetryControl.Goals.{
    ContextSnapshot,
    ContractValidation,
    Goal,
    GoalBudgetReservation,
    GoalDecision,
    GoalExternalWait,
    GoalEvent,
    GoalRevision,
    HarnessSession,
    ReadModel,
    RunEvidence,
    RunUsage,
    ValidationProfiles,
    WorkDependency,
    WorkOutcome
  }

  alias SymmetryControl.Goals.Workers.{GoalControlWorker, WakeupWorker}

  alias SymmetryControl.Integrations.{ChangeAction, Connection}
  alias SymmetryControl.Integrations.Providers.{AzureDevOps, GitHub}
  alias SymmetryControl.Orchestration.Scheduler
  alias SymmetryControl.Orchestration.{Command, Run, Runtime, Task}
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces.{Project, ProjectResource, WorkItem}

  @command_kinds [
    "activate",
    "pause",
    "resume",
    "cancel",
    "amend",
    "request_plan",
    "accept_plan",
    "request_decision",
    "resolve_decision",
    "admit_task",
    "add_dependency",
    "remove_dependency",
    "achieve"
  ]
  @nonterminal_task_states [
    "queued",
    "assigned",
    "claimed",
    "running",
    "waiting_for_input",
    "paused",
    "cancelling"
  ]
  @cancellable_task_states [
    "queued",
    "assigned",
    "claimed",
    "running",
    "paused",
    "waiting_for_input"
  ]
  @terminal_run_states ["completed", "failed", "cancelled"]
  @accounting_terminal_run_states @terminal_run_states ++ ["expired"]
  @terminal_semantic_failure_reasons [
    "auth",
    "quota",
    "rate_limit",
    "network",
    "unsupported_version",
    "resume_rejected",
    "handoff_unsupported",
    "context_overflow",
    "missing_result",
    "process_failure",
    "cancelled",
    "unknown_outcome",
    "lease_expired",
    "attempt_limit",
    "supervised_worker_lost"
  ]
  @automatic_terminal_settlements [
    "failed",
    "replan_required",
    "no_verified_progress",
    "validation_failed",
    "invalid_task_result",
    "missing_result"
  ]
  @terminal_settlement_grace_ms 8 * 60 * 1_000
  @task_purposes ["implement", "validate", "plan", "observe", "chat"]
  @default_max_parallel_tasks 1
  @default_max_run_attempts 2
  @automatic_reconciliation_backoff_seconds 60
  @default_event_limit 100
  @default_attention_limit 100
  @max_plan_items 256
  @max_plan_dependencies_per_item 256
  @max_safe_integer 9_007_199_254_740_991
  @max_microusd 9_223_372_036_854_775_807

  @type receipt :: %{goal: map(), event: map(), response: map()}

  @spec create_goal(Ecto.UUID.t(), map(), String.t(), keyword()) ::
          {:ok, receipt(), :created | :replayed} | {:error, term()}
  def create_goal(project_id, attrs, actor_ref, opts \\ [])

  def create_goal(project_id, attrs, actor_ref, opts)
      when is_binary(project_id) and is_map(attrs) and is_binary(actor_ref) and is_list(opts) do
    attrs = normalize_map(attrs)

    with :ok <- valid_uuid(project_id),
         :ok <- valid_actor(actor_ref),
         :ok <- validate_goal_contract(:goal_create, attrs, opts),
         {:ok, mutation_id} <- required_uuid(attrs, :mutation_id),
         {:ok, title} <- required_string(attrs, :title),
         {:ok, initial_revision} <- required_map(attrs, :initial_revision) do
      body = %{project_id: project_id, title: title, initial_revision: initial_revision}

      case replay_create(mutation_id, body) do
        {:ok, receipt} ->
          {:ok, receipt, :replayed}

        :missing ->
          with :ok <- valid_initial_revision(initial_revision, opts) do
            create_new_goal(
              project_id,
              title,
              initial_revision,
              mutation_id,
              actor_ref,
              body,
              opts
            )
          end

        {:error, reason} ->
          {:error, reason}
      end
    end
  end

  def create_goal(_, _, _, _), do: {:error, :invalid_request}

  @spec fetch_goal(Ecto.UUID.t()) :: {:ok, map()} | {:error, :not_found | :invalid_request}
  def fetch_goal(goal_id) when is_binary(goal_id) do
    with :ok <- valid_uuid(goal_id), do: ReadModel.fetch(goal_id)
  end

  def fetch_goal(_), do: {:error, :invalid_request}

  @spec goal_managed_task?(Ecto.UUID.t()) :: boolean()
  def goal_managed_task?(task_id) when is_binary(task_id) do
    if valid_uuid?(task_id) do
      Repo.exists?(from(task in Task, where: task.id == ^task_id and not is_nil(task.goal_id)))
    else
      false
    end
  end

  def goal_managed_task?(_), do: false

  @spec legacy_task_command_allowed?(Ecto.UUID.t()) :: boolean()
  def legacy_task_command_allowed?(task_id), do: not goal_managed_task?(task_id)

  @spec command(Ecto.UUID.t(), map(), String.t(), keyword()) ::
          {:ok, receipt(), :created | :replayed} | {:error, term()}
  def command(goal_id, command, actor_ref, opts \\ [])

  def command(goal_id, command, actor_ref, opts)
      when is_binary(goal_id) and is_map(command) and is_binary(actor_ref) and is_list(opts) do
    command = normalize_map(command)

    with :ok <- valid_uuid(goal_id),
         :ok <- valid_actor(actor_ref),
         {:ok, parsed} <- parse_command(command, opts) do
      body = Map.take(parsed, [:expected_version, :expected_revision, :kind, :payload])

      Repo.transaction(fn ->
        case replay_command(goal_id, parsed.mutation_id, body) do
          {:ok, receipt} -> {:replayed, receipt}
          :missing -> execute_command(goal_id, parsed, actor_ref, body, opts)
          {:error, reason} -> rollback(reason)
        end
      end)
      |> case do
        {:ok, {:created, receipt}} ->
          {:ok, receipt, :created}

        {:ok, {:replayed, receipt}} ->
          {:ok, receipt, :replayed}

        {:error, :idempotency_conflict} ->
          case replay_command_after_conflict(goal_id, parsed.mutation_id, body) do
            {:ok, receipt} -> {:ok, receipt, :replayed}
            _ -> {:error, :idempotency_conflict}
          end

        {:error, reason} ->
          {:error, reason}
      end
    end
  end

  def command(_, _, _, _), do: {:error, :invalid_request}

  @doc false
  @spec accept_outcome(Ecto.UUID.t(), map(), keyword()) :: {:error, :outcome_derived}
  def accept_outcome(_goal_id, _payload, _opts \\ []), do: {:error, :outcome_derived}

  @spec list_events(Ecto.UUID.t(), keyword()) :: {:ok, map()} | {:error, term()}
  def list_events(goal_id, opts \\ [])

  def list_events(goal_id, opts) when is_binary(goal_id) and is_list(opts) do
    with :ok <- valid_uuid(goal_id),
         :ok <- valid_event_options(opts) do
      ReadModel.events(goal_id, Keyword.put_new(opts, :limit, @default_event_limit))
    end
  end

  def list_events(_, _), do: {:error, :invalid_request}

  @spec graph(Ecto.UUID.t()) :: {:ok, map()} | {:error, term()}
  def graph(goal_id) when is_binary(goal_id) do
    with :ok <- valid_uuid(goal_id), do: ReadModel.graph(goal_id)
  end

  def graph(_), do: {:error, :invalid_request}

  @spec fetch_context(Ecto.UUID.t(), Ecto.UUID.t()) :: {:ok, map()} | {:error, term()}
  def fetch_context(goal_id, snapshot_id) when is_binary(goal_id) and is_binary(snapshot_id) do
    with :ok <- valid_uuid(goal_id), :ok <- valid_uuid(snapshot_id) do
      Repo.transaction(fn ->
        goal = lock_goal(goal_id)
        snapshot = lock_context_snapshot(goal.id, snapshot_id)
        item = if snapshot.work_item_id, do: lock_work_item(snapshot.work_item_id)

        if item && (item.goal_id != goal.id or item.admitted_revision != snapshot.goal_revision) do
          rollback(:ownership_lost)
        end

        context_wire_envelope!(snapshot, goal, item, nil)
      end)
      |> case do
        {:ok, context} -> {:ok, context}
        {:error, reason} -> {:error, reason}
      end
    end
  end

  def fetch_context(_, _), do: {:error, :invalid_request}

  @spec attention(keyword()) :: {:ok, map()} | {:error, term()}
  def attention(opts \\ [])

  def attention(opts) when is_list(opts) do
    with :ok <- valid_attention_options(opts) do
      ReadModel.attention(Keyword.put_new(opts, :limit, @default_attention_limit))
    end
  end

  def attention(_), do: {:error, :invalid_request}

  @spec attach_harness_session(Ecto.UUID.t(), Ecto.UUID.t(), map(), map(), keyword()) ::
          {:ok, map(), :created | :replayed} | {:error, term()}
  def attach_harness_session(machine_id, run_id, fence, attrs, opts \\ [])

  def attach_harness_session(machine_id, run_id, fence, attrs, opts)
      when is_binary(machine_id) and is_binary(run_id) and is_map(fence) and is_map(attrs) and
             is_list(opts) do
    with :ok <- valid_uuid(machine_id),
         :ok <- valid_uuid(run_id),
         :ok <- valid_fence(fence),
         {:ok, session_attrs} <- session_attrs(attrs) do
      Repo.transaction(fn ->
        {goal, task, item, run, runtime} =
          lock_goal_run!(machine_id, run_id, fence, opts, :attach_replay)

        ensure_session_scope!(session_attrs, task, item, run, runtime, machine_id)
        session_mode = value(task.input || %{}, "session_mode", "fresh")

        if session_mode == "handoff", do: rollback(:unsupported_capability)

        session =
          if task.requested_session_id do
            Repo.one(
              from(session in HarnessSession,
                where: session.id == ^task.requested_session_id,
                lock: "FOR UPDATE"
              )
            )
          else
            Repo.one(
              from(session in HarnessSession,
                where:
                  session.machine_id == ^machine_id and
                    session.local_handle_id == ^session_attrs.local_handle_id,
                lock: "FOR UPDATE"
              )
            )
          end

        if replayed_attached_session?(session, session_attrs, run, runtime, item, task) do
          {:replayed, session_receipt(goal, task, run, session)}
        else
          ensure_current_attachment!(goal, task, run, runtime, machine_id, fence, opts)

          case session do
            nil when not is_nil(run.harness_session_id) ->
              rollback(:ownership_lost)

            nil when session_mode == "resume" ->
              rollback(:requested_session_not_found)

            nil ->
              create_harness_session!(
                goal,
                task,
                item,
                run,
                runtime,
                machine_id,
                session_attrs,
                opts
              )

            session ->
              attach_existing_session!(
                session,
                session_attrs,
                run,
                runtime,
                item,
                machine_id,
                goal,
                task,
                session_mode,
                opts
              )
          end
        end
      end)
      |> machine_write_result()
    end
  end

  def attach_harness_session(_, _, _, _, _), do: {:error, :invalid_request}

  @spec append_evidence(Ecto.UUID.t(), Ecto.UUID.t(), map(), map(), keyword()) ::
          {:ok, map(), :created | :replayed} | {:error, term()}
  def append_evidence(machine_id, run_id, fence, evidence, opts \\ [])

  def append_evidence(machine_id, run_id, fence, evidence, opts)
      when is_binary(machine_id) and is_binary(run_id) and is_map(fence) and is_map(evidence) and
             is_list(opts) do
    with :ok <- valid_uuid(machine_id),
         :ok <- valid_uuid(run_id),
         :ok <- valid_fence(fence),
         :ok <- safe_evidence_document(evidence),
         :ok <- validate_contract(:evidence, evidence, opts),
         {:ok, evidence_attrs} <- evidence_attrs(evidence, run_id) do
      Repo.transaction(fn ->
        {goal, task, item, run, runtime} =
          lock_goal_run!(machine_id, run_id, fence, opts, :delivery_replay)

        snapshot =
          lock_context_snapshot(goal.id, task.context_snapshot_id || rollback(:not_found))

        unless snapshot.goal_revision == task.goal_revision and
                 snapshot.work_item_id == task.work_item_id do
          rollback(:ownership_lost)
        end

        context_wire_envelope!(snapshot, goal, item, task)

        evidence_attrs =
          ensure_evidence_identity!(task, item, run, runtime, snapshot, evidence_attrs)

        case Repo.one(
               from(row in RunEvidence,
                 where:
                   row.run_id == ^run.id and row.evidence_key == ^evidence_attrs.evidence_key,
                 lock: "FOR UPDATE"
               )
             ) do
          nil ->
            if validation_evidence_closed?(goal, task, run, evidence_attrs) do
              rollback(:validation_evidence_closed)
            end

            changeset =
              %RunEvidence{id: evidence_attrs.id}
              |> RunEvidence.changeset(Map.drop(evidence_attrs, [:id, :observed_at]))
              |> stamp_insert(evidence_attrs.observed_at)

            case Repo.insert(changeset) do
              {:ok, row} ->
                {:created, evidence_receipt(row)}

              {:error, _changeset} ->
                row =
                  Repo.one(
                    from(row in RunEvidence,
                      where:
                        row.run_id == ^run.id and row.evidence_key == ^evidence_attrs.evidence_key,
                      lock: "FOR UPDATE"
                    )
                  ) || rollback(:idempotency_conflict)

                if evidence_matches?(row, evidence_attrs),
                  do: {:replayed, evidence_receipt(row)},
                  else: rollback(:idempotency_conflict)
            end

          row ->
            if evidence_matches?(row, evidence_attrs),
              do: {:replayed, evidence_receipt(row)},
              else: rollback(:idempotency_conflict)
        end
      end)
      |> machine_write_result()
    end
  end

  def append_evidence(_, _, _, _, _), do: {:error, :invalid_request}

  @spec record_usage(Ecto.UUID.t(), Ecto.UUID.t(), map(), map(), keyword()) ::
          {:ok, map(), :created | :replayed} | {:error, term()}
  def record_usage(machine_id, run_id, fence, usage, opts \\ [])

  def record_usage(machine_id, run_id, fence, usage, opts)
      when is_binary(machine_id) and is_binary(run_id) and is_map(fence) and is_map(usage) and
             is_list(opts) do
    with :ok <- valid_uuid(machine_id),
         :ok <- valid_uuid(run_id),
         :ok <- valid_fence(fence),
         {:ok, usage_attrs} <- usage_attrs(usage, run_id),
         :ok <- validate_contract(:usage, usage, opts) do
      Repo.transaction(fn ->
        {goal, task, _item, run, _runtime} =
          lock_goal_run!(machine_id, run_id, fence, opts, :late_accounting)

        case Repo.one(
               from(row in RunUsage,
                 where: row.run_id == ^run.id and row.usage_key == ^usage_attrs.usage_key,
                 lock: "FOR UPDATE"
               )
             ) do
          nil ->
            ensure_usage_supersedes_run!(usage_attrs, run)

            changeset =
              %RunUsage{id: usage_attrs.id}
              |> RunUsage.changeset(Map.drop(usage_attrs, [:id, :observed_at]))
              |> stamp_insert(usage_attrs.observed_at)

            case Repo.insert(changeset) do
              {:ok, row} ->
                reconcile_reservation_and_arm!(goal, task, opts)

                {:created, usage_receipt(row)}

              {:error, _changeset} ->
                row =
                  Repo.one(
                    from(row in RunUsage,
                      where: row.run_id == ^run.id and row.usage_key == ^usage_attrs.usage_key,
                      lock: "FOR UPDATE"
                    )
                  ) || rollback(:idempotency_conflict)

                if usage_matches?(row, usage_attrs) do
                  {:replayed, usage_receipt(row)}
                else
                  rollback(:idempotency_conflict)
                end
            end

          row ->
            if usage_matches?(row, usage_attrs) do
              {:replayed, usage_receipt(row)}
            else
              rollback(:idempotency_conflict)
            end
        end
      end)
      |> machine_write_result()
    end
  end

  def record_usage(_, _, _, _, _), do: {:error, :invalid_request}

  @spec fetch_run_context(Ecto.UUID.t(), Ecto.UUID.t(), map()) :: {:ok, map()} | {:error, term()}
  def fetch_run_context(machine_id, run_id, fence)
      when is_binary(machine_id) and is_binary(run_id) and is_map(fence) do
    with :ok <- valid_uuid(machine_id), :ok <- valid_uuid(run_id), :ok <- valid_fence(fence) do
      Repo.transaction(fn ->
        {goal, task, item, run, _runtime} =
          lock_goal_run!(machine_id, run_id, fence, [], :current)

        session_id =
          if task.purpose == "validate" and not is_nil(task.validation_of_task_id) and
               is_nil(run.harness_session_id) do
            # Deterministic validation is daemon-owned work, not a retained native
            # session. Its claimed fence remains mandatory, but no opaque handle is
            # required to obtain the immutable context snapshot.
            nil
          else
            Repo.one(
              from(session in HarnessSession,
                where:
                  session.id == ^run.harness_session_id and session.machine_id == ^machine_id and
                    session.active_run_id == ^run.id,
                lock: "FOR UPDATE"
              )
            )
            |> case do
              %HarnessSession{id: id} -> id
              nil -> rollback(:ownership_lost)
            end
          end

        snapshot_id = task.context_snapshot_id || rollback(:not_found)
        snapshot = lock_context_snapshot(goal.id, snapshot_id)

        %{
          goal_id: goal.id,
          task_id: task.id,
          run_id: run.id,
          generation: run.generation,
          session_id: session_id,
          context: context_wire_envelope!(snapshot, goal, item, task)
        }
      end)
      |> case do
        {:ok, context} -> {:ok, context}
        {:error, reason} -> {:error, reason}
      end
    end
  end

  def fetch_run_context(_, _, _), do: {:error, :invalid_request}

  defp lock_goal_run!(machine_id, run_id, fence, opts, mode) do
    task_id =
      Repo.one(from(run in Run, where: run.id == ^run_id, select: run.task_id)) ||
        rollback(:not_found)

    untrusted_task = Repo.get(Task, task_id) || rollback(:not_found)
    goal_id = untrusted_task.goal_id || rollback(:not_found)

    goal = lock_goal(goal_id)
    item = if untrusted_task.work_item_id, do: lock_work_item(untrusted_task.work_item_id)
    task = lock_task(task_id)
    run = lock_run(run_id)

    runtime =
      Repo.one(from(runtime in Runtime, where: runtime.id == ^run.runtime_id, lock: "FOR UPDATE")) ||
        rollback(:not_found)

    unless settlement_task_owned_by_goal?(task, goal, item) do
      rollback(:ownership_lost)
    end

    if mode == :current and
         (task.goal_revision != goal.current_revision or
            not ((goal.state == "draft" and plan_task?(task)) or
                   (goal.state == "active" and task.purpose != "plan"))) do
      rollback(:ownership_lost)
    end

    ensure_machine_fence!(machine_id, task, run, runtime, fence, mode, now(opts))
    {goal, task, item, run, runtime}
  end

  defp ensure_machine_fence!(machine_id, task, run, runtime, fence, mode, current) do
    static? =
      runtime.machine_id == machine_id and
        run.runtime_id == value(fence, :runtime_id) and
        run.claimed_runtime_epoch == value(fence, :runtime_epoch) and
        run.generation == value(fence, :generation) and
        run.claim_id == value(fence, :claim_id) and
        run.lease_token == value(fence, :lease_token)

    unless static?, do: rollback(:ownership_lost)

    if mode not in [:late_accounting, :attach_replay, :delivery_replay] and
         task.current_generation != value(fence, :generation),
       do: rollback(:ownership_lost)

    if mode == :current do
      current? =
        runtime.connection_epoch == value(fence, :runtime_epoch) and
          run.state in @nonterminal_task_states and not is_nil(run.lease_expires_at) and
          DateTime.compare(run.lease_expires_at, current) == :gt

      unless current?, do: rollback(:ownership_lost)
    end
  end

  defp valid_fence(fence) do
    fields = [:runtime_id, :claim_id, :lease_token]

    if Enum.all?(fields, &valid_uuid?(value(fence, &1))) and
         is_integer(value(fence, :runtime_epoch)) and value(fence, :runtime_epoch) > 0 and
         is_integer(value(fence, :generation)) and value(fence, :generation) > 0 do
      :ok
    else
      {:error, :invalid_request}
    end
  end

  defp session_attrs(attrs) do
    allowed = [
      "local_handle_id",
      "harness_kind",
      "harness_version",
      "adapter_version",
      "workspace_fingerprint",
      "workspace",
      "repository_resource_id"
    ]

    with true <- only_known_keys?(attrs, allowed),
         {:ok, local_handle_id} <- required_uuid(attrs, :local_handle_id),
         {:ok, harness_kind} <- required_string(attrs, :harness_kind),
         true <- harness_kind in ["codex", "claude_code", "pi", "opencode"],
         {:ok, harness_version} <- required_string(attrs, :harness_version),
         {:ok, adapter_version} <- required_string(attrs, :adapter_version),
         {:ok, workspace_fingerprint} <- required_string(attrs, :workspace_fingerprint),
         {:ok, workspace} <- required_string(attrs, :workspace),
         {:ok, repository_resource_id} <- optional_uuid(attrs, :repository_resource_id) do
      {:ok,
       %{
         local_handle_id: local_handle_id,
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

  defp ensure_session_scope!(attrs, task, item, run, runtime, machine_id) do
    repository_resource_id = task_repository_resource_id!(task, item)

    unless attrs.workspace == task.workspace and
             (is_nil(attrs.repository_resource_id) or
                attrs.repository_resource_id == repository_resource_id) and
             runtime.repository_resource_id == repository_resource_id and
             runtime.id == run.runtime_id and runtime.machine_id == machine_id and
             runtime.harness_kind == attrs.harness_kind and
             runtime.harness_version == attrs.harness_version and
             runtime.adapter_version == attrs.adapter_version do
      rollback(:ownership_lost)
    end
  end

  defp ensure_current_attachment!(goal, task, run, runtime, machine_id, fence, opts) do
    unless task.goal_revision == goal.current_revision and
             ((goal.state == "draft" and plan_task?(task)) or
                (goal.state == "active" and task.purpose != "plan")),
           do: rollback(:ownership_lost)

    ensure_machine_fence!(machine_id, task, run, runtime, fence, :current, now(opts))
  end

  defp replayed_attached_session?(nil, _attrs, _run, _runtime, _item, _task), do: false

  defp replayed_attached_session?(session, attrs, run, runtime, item, task) do
    run.harness_session_id == session.id and
      session_matches?(
        session,
        attrs,
        run.id,
        runtime.id,
        task_repository_resource_id!(task, item)
      )
  end

  defp session_matches?(session, attrs, run_id, runtime_id, repository_resource_id) do
    session.local_handle_id == attrs.local_handle_id and session.active_run_id == run_id and
      session.state == "busy" and session.runtime_id == runtime_id and
      session.repository_resource_id == repository_resource_id and
      session.harness_kind == attrs.harness_kind and
      session.harness_version == attrs.harness_version and
      session.adapter_version == attrs.adapter_version and
      session.workspace_fingerprint == attrs.workspace_fingerprint and
      (is_nil(attrs.repository_resource_id) or
         session.repository_resource_id == attrs.repository_resource_id)
  end

  defp create_harness_session!(goal, task, item, run, runtime, machine_id, attrs, opts) do
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
          state: "busy",
          active_run_id: run.id
        })
      )
      |> stamp_insert(now(opts))

    case Repo.insert(changeset) do
      {:ok, session} ->
        attach_session_to_run!(run, session, opts)
        {:created, session_receipt(goal, task, run, session)}

      {:error, _changeset} ->
        session =
          Repo.one(
            from(session in HarnessSession,
              where:
                session.machine_id == ^machine_id and
                  session.local_handle_id == ^attrs.local_handle_id,
              lock: "FOR UPDATE"
            )
          ) || rollback(:idempotency_conflict)

        attach_existing_session!(
          session,
          attrs,
          run,
          runtime,
          item,
          machine_id,
          goal,
          task,
          "fresh",
          opts
        )
    end
  end

  defp attach_existing_session!(
         session,
         attrs,
         run,
         runtime,
         item,
         machine_id,
         goal,
         task,
         session_mode,
         opts
       ) do
    compatible? =
      session.machine_id == machine_id and session.runtime_id == runtime.id and
        session.repository_resource_id == task_repository_resource_id!(task, item) and
        session.local_handle_id == attrs.local_handle_id and
        session.harness_kind == attrs.harness_kind and
        session.harness_version == attrs.harness_version and
        session.adapter_version == attrs.adapter_version and
        session.workspace_fingerprint == attrs.workspace_fingerprint

    unless compatible?, do: rollback(:idempotency_conflict)

    case session.state do
      "available" when is_nil(session.active_run_id) and is_nil(run.harness_session_id) ->
        session =
          session
          |> HarnessSession.update_changeset(%{state: "busy", active_run_id: run.id})
          |> stamp_update(now(opts))
          |> Repo.update!()

        attach_session_to_run!(run, session, opts)
        {:created, session_receipt(goal, task, run, session)}

      "busy" ->
        if run.harness_session_id in [nil, session.id] and
             session_matches?(
               session,
               attrs,
               run.id,
               runtime.id,
               task_repository_resource_id!(task, item)
             ) do
          attach_session_to_run!(run, session, opts)
          {:replayed, session_receipt(goal, task, run, session)}
        else
          rollback(:idempotency_conflict)
        end

      _ ->
        if session_mode == "resume",
          do: rollback(:requested_session_unavailable),
          else: rollback(:ownership_lost)
    end
  end

  defp attach_session_to_run!(run, session, opts) do
    cond do
      is_nil(run.harness_session_id) ->
        run
        |> Changeset.change(harness_session_id: session.id)
        |> stamp_update(now(opts))
        |> Repo.update!()

      run.harness_session_id == session.id ->
        :ok

      true ->
        rollback(:ownership_lost)
    end
  end

  # A terminal Run does not prove that a retained native session stopped.
  # Until a durable session_stopped receipt exists, preserve the session as
  # unavailable rather than letting a later admission reuse it.
  defp mark_harness_session_unavailable!(%Run{harness_session_id: nil}, _opts), do: :ok

  defp mark_harness_session_unavailable!(run, opts) do
    session =
      Repo.one(
        from(session in HarnessSession,
          where: session.id == ^run.harness_session_id,
          lock: "FOR UPDATE"
        )
      )

    if session && session.state == "busy" && session.active_run_id == run.id do
      session
      |> HarnessSession.update_changeset(%{state: "unavailable", active_run_id: nil})
      |> stamp_update(now(opts))
      |> Repo.update!()
    else
      :ok
    end
  end

  defp session_receipt(goal, task, run, session) do
    %{
      session: %{
        id: session.id,
        goal_id: goal.id,
        task_id: task.id,
        run_id: run.id,
        runtime_id: session.runtime_id,
        machine_id: session.machine_id,
        repository_resource_id: session.repository_resource_id,
        active_run_id: session.active_run_id,
        local_handle_id: session.local_handle_id,
        harness_kind: session.harness_kind,
        harness_version: session.harness_version,
        adapter_version: session.adapter_version,
        state: session.state,
        workspace_fingerprint: session.workspace_fingerprint,
        workspace: task.workspace
      }
    }
  end

  defp evidence_attrs(evidence, run_id) do
    allowed = [
      "schema_version",
      "evidence_id",
      "run_id",
      "evidence_key",
      "kind",
      "subject",
      "subject_hash",
      "source_ref",
      "source_revision",
      "validator_profile",
      "verdict",
      "payload",
      "observed_at"
    ]

    with true <- only_known_keys?(evidence, allowed),
         "symmetry.evidence.v1" <- value(evidence, :schema_version),
         {:ok, id} <- required_uuid(evidence, :evidence_id),
         ^run_id <- value(evidence, :run_id),
         {:ok, evidence_key} <- required_string(evidence, :evidence_key),
         {:ok, kind} <- required_string(evidence, :kind),
         true <- kind in ["check", "artifact", "review", "observation"],
         {:ok, subject} <- parse_subject(value(evidence, :subject)),
         {:ok, subject_hash} <- required_sha256_digest(evidence, :subject_hash),
         {:ok, source_ref} <- required_map(evidence, :source_ref),
         ^kind <- value(source_ref, :kind),
         {:ok, source_revision} <- required_string(evidence, :source_revision),
         {:ok, validator_profile} <- evidence_validator_profile(evidence, kind),
         {:ok, verdict} <- required_string(evidence, :verdict),
         true <- verdict in ["passed", "failed", "unknown", "not_applicable"],
         {:ok, payload} <- required_map(evidence, :payload),
         {:ok, observed_at} <- required_datetime(evidence, :observed_at),
         true <- safe_document?(source_ref) and safe_document?(payload) do
      {:ok,
       %{
         id: id,
         run_id: run_id,
         evidence_key: evidence_key,
         kind: kind,
         subject_hash: subject_hash,
         source_ref: normalize_map(source_ref),
         source_revision: source_revision,
         validator_profile: validator_profile,
         verdict: verdict,
         payload: normalize_map(payload),
         subject: subject,
         observed_at: observed_at
       }}
    else
      _ -> {:error, :invalid_request}
    end
  end

  defp evidence_matches?(row, attrs) do
    row.id == attrs.id and row.kind == attrs.kind and row.subject_hash == attrs.subject_hash and
      row.source_ref == attrs.source_ref and row.source_revision == attrs.source_revision and
      row.validator_profile == attrs.validator_profile and row.verdict == attrs.verdict and
      row.payload == attrs.payload and row.inserted_at == attrs.observed_at
  end

  defp validation_evidence_closed?(goal, task, run, evidence) do
    task.purpose == "validate" and not is_nil(task.validation_of_task_id) and
      evidence.kind != "observation" and
      Repo.exists?(
        from(event in GoalEvent,
          where:
            event.goal_id == ^goal.id and
              event.id == ^settlement_mutation_id(task.id, run.id, run.generation) and
              event.kind == "task_settled"
        )
      )
  end

  defp ensure_evidence_identity!(task, item, run, runtime, snapshot, evidence) do
    subject = value(task.input || %{}, "subject")
    predicate_id = value(evidence.payload, "predicate_id")

    unless is_map(subject) and subject == evidence.subject and
             evidence.subject_hash == RequestHash.canonical(subject) and
             is_binary(predicate_id) do
      rollback(:invalid_evidence_identity)
    end

    if evidence.kind == "observation" do
      unless source_matches_evidence?(evidence, subject, item, nil),
        do: rollback(:invalid_evidence_identity)

      evidence
    else
      predicate =
        snapshot
        |> snapshot_acceptance_contract!()
        |> value("predicates", [])
        |> Enum.find(&(value(&1, "id") == predicate_id and value(&1, "kind") == evidence.kind))

      unless is_map(predicate) and source_matches_evidence?(evidence, subject, item, predicate),
        do: rollback(:invalid_evidence_identity)

      unless task.purpose == "validate" and not is_nil(task.validation_of_task_id),
        do: rollback(:invalid_evidence_identity)

      expected_validator_profile = validator_profile_for!(predicate, evidence.kind)

      unless evidence.validator_profile == expected_validator_profile,
        do: rollback(:invalid_evidence_identity)

      if evidence.kind in ["check", "review"] do
        ensure_validation_binding!(
          snapshot,
          evidence,
          expected_validator_profile,
          task,
          run,
          runtime
        )
      end

      %{evidence | validator_profile: expected_validator_profile}
    end
  end

  defp snapshot_acceptance_contract!(snapshot) do
    snapshot.payload
    |> value("work_contract", %{})
    |> value("acceptance")
    |> case do
      acceptance when is_map(acceptance) -> acceptance
      _ -> rollback(:invalid_evidence_identity)
    end
  end

  defp ensure_validation_binding!(snapshot, evidence, expected_profile, task, run, runtime) do
    bindings =
      snapshot.payload
      |> value("work_contract", %{})
      |> value("validation_bindings")

    binding =
      if is_list(bindings) do
        Enum.find(bindings, fn candidate ->
          value(candidate, "profile_name") == expected_profile and
            value(candidate, "kind") == evidence.kind
        end)
      end

    unless is_map(binding) and
             binding_matches_evidence?(binding, evidence, expected_profile, task, run, runtime) do
      rollback(:invalid_evidence_identity)
    end
  end

  defp binding_matches_evidence?(binding, evidence, expected_profile, task, run, runtime) do
    allowed_runtime_ids = value(binding, "allowed_runtime_ids")
    digest = value(binding, "profile_digest")

    base? =
      only_known_keys?(binding, ["profile_name", "kind", "profile_digest", "allowed_runtime_ids"]) and
        value(binding, "profile_name") == expected_profile and
        value(binding, "kind") == evidence.kind and
        is_binary(digest) and Regex.match?(~r/^sha256:[0-9a-f]{64}$/, digest) and
        is_list(allowed_runtime_ids) and allowed_runtime_ids != [] and
        Enum.all?(allowed_runtime_ids, &valid_uuid?/1) and
        length(allowed_runtime_ids) == MapSet.size(MapSet.new(allowed_runtime_ids)) and
        runtime.id == run.runtime_id and runtime.id in allowed_runtime_ids

    case evidence.kind do
      "check" ->
        base? and value(evidence.payload, "profile_digest") == digest

      "review" ->
        base? and evidence.source_revision == digest and
          value(evidence.source_ref, "review_task_id") == task.id and
          value(evidence.payload, "review_task_id") == task.id

      _ ->
        false
    end
  end

  defp validator_profile_for!(predicate, kind) do
    case kind do
      "check" -> required_string!(predicate, :validator_profile)
      "review" -> required_string!(predicate, :reviewer_profile)
      "artifact" -> "artifact"
      _ -> rollback(:invalid_evidence_identity)
    end
  end

  defp source_matches_evidence?(evidence, subject, item, predicate) do
    source = evidence.source_ref

    payload_subject = value(evidence.payload, "subject")
    payload_hash = value(evidence.payload, "subject_hash")

    unless payload_subject == subject and
             payload_hash == "sha256:" <> Base.encode16(evidence.subject_hash, case: :lower) do
      false
    else
      source_matches_evidence_kind?(evidence, subject, item, predicate, source)
    end
  end

  defp source_matches_evidence_kind?(evidence, subject, item, predicate, source) do
    source_hash = "sha256:" <> Base.encode16(evidence.subject_hash, case: :lower)

    case evidence.kind do
      "check" ->
        only_known_keys?(source, ["kind", "ref", "validator_profile", "subject_hash"]) and
          is_short_identifier?(value(source, "ref")) and
          value(source, "validator_profile") == evidence.validator_profile and
          value(source, "subject_hash") == source_hash and
          is_short_identifier?(evidence.validator_profile) and
          evidence.validator_profile == value(predicate, "validator_profile")

      "artifact" ->
        only_known_keys?(source, ["kind", "ref", "resource_id", "commit", "path", "subject_hash"]) and
          is_short_identifier?(value(source, "ref")) and
          value(source, "resource_id") == item.repository_resource_id and
          value(source, "resource_id") == value(subject, "resource_id") and
          value(source, "commit") == value(subject, "commit") and
          value(source, "subject_hash") == source_hash and is_binary(value(source, "path")) and
          value(source, "path") != "" and
          value(predicate, "resource_id") == item.repository_resource_id and
          value(source, "path") == value(predicate, "path") and
          value(evidence.payload, "resource_id") == item.repository_resource_id and
          value(evidence.payload, "commit") == value(subject, "commit") and
          value(evidence.payload, "path") == value(predicate, "path")

      "review" ->
        only_known_keys?(source, ["kind", "ref", "review_task_id", "subject_hash"]) and
          is_short_identifier?(value(source, "ref")) and
          valid_uuid?(value(source, "review_task_id")) and
          value(source, "subject_hash") == source_hash and
          value(evidence.payload, "review_task_id") == value(source, "review_task_id") and
          value(evidence.payload, "verdict") == evidence.verdict and
          is_short_identifier?(evidence.validator_profile) and
          evidence.validator_profile == value(predicate, "reviewer_profile")

      "observation" ->
        only_known_keys?(source, ["kind", "ref", "external_ref", "subject_hash"]) and
          is_short_identifier?(value(source, "ref")) and
          value(source, "subject_hash") == source_hash and
          value(source, "external_ref") == value(evidence.payload, "external_ref") and
          is_binary(value(evidence.payload, "external_ref")) and
          value(evidence.payload, "external_ref") != ""

      _ ->
        false
    end
  end

  defp evidence_receipt(row) do
    %{
      evidence: %{
        id: row.id,
        run_id: row.run_id,
        evidence_key: row.evidence_key,
        kind: row.kind,
        subject_hash: context_digest(row.subject_hash),
        verdict: row.verdict,
        observed_at: DateTime.to_iso8601(row.inserted_at)
      }
    }
  end

  defp usage_attrs(usage, run_id) do
    allowed = [
      "schema_version",
      "usage_id",
      "run_id",
      "usage_key",
      "provider",
      "model",
      "input_tokens",
      "output_tokens",
      "cached_input_tokens",
      "cost_microusd",
      "cost_basis",
      "price_version",
      "supersedes_id",
      "observed_at"
    ]

    with true <- only_known_keys?(usage, allowed),
         "symmetry.usage.v1" <- value(usage, :schema_version),
         {:ok, id} <- required_uuid(usage, :usage_id),
         ^run_id <- value(usage, :run_id),
         {:ok, usage_key} <- required_string(usage, :usage_key),
         {:ok, provider} <- required_string(usage, :provider),
         {:ok, model} <- required_string(usage, :model),
         {:ok, input_tokens} <- nullable_safe_integer(usage, :input_tokens),
         {:ok, output_tokens} <- nullable_safe_integer(usage, :output_tokens),
         {:ok, cached_input_tokens} <- nullable_safe_integer(usage, :cached_input_tokens),
         {:ok, cost_microusd} <- nullable_microusd(usage, :cost_microusd),
         {:ok, cost_basis} <- required_string(usage, :cost_basis),
         true <- cost_basis in ["reported", "estimated", "unknown"],
         {:ok, price_version} <- nullable_string(usage, :price_version),
         {:ok, supersedes_id} <- optional_uuid(usage, :supersedes_id),
         {:ok, observed_at} <- required_datetime(usage, :observed_at),
         false <- same_uuid?(id, supersedes_id),
         true <- valid_usage_cost?(cost_basis, cost_microusd) do
      {:ok,
       %{
         id: id,
         run_id: run_id,
         usage_key: usage_key,
         provider: provider,
         model: model,
         input_tokens: input_tokens,
         output_tokens: output_tokens,
         cached_input_tokens: cached_input_tokens,
         cost_microusd: cost_microusd,
         cost_basis: cost_basis,
         price_version: price_version,
         supersedes_id: supersedes_id,
         observed_at: observed_at
       }}
    else
      _ -> {:error, :invalid_request}
    end
  end

  defp usage_matches?(row, attrs) do
    row.id == attrs.id and row.provider == attrs.provider and row.model == attrs.model and
      row.input_tokens == attrs.input_tokens and row.output_tokens == attrs.output_tokens and
      row.cached_input_tokens == attrs.cached_input_tokens and
      row.cost_microusd == attrs.cost_microusd and
      row.cost_basis == attrs.cost_basis and row.price_version == attrs.price_version and
      row.supersedes_id == attrs.supersedes_id and row.inserted_at == attrs.observed_at
  end

  defp usage_receipt(row) do
    %{
      usage: %{
        id: row.id,
        run_id: row.run_id,
        usage_key: row.usage_key,
        cost_microusd: microusd_wire_value(row.cost_microusd),
        cost_basis: row.cost_basis
      }
    }
  end

  defp microusd_wire_value(nil), do: nil
  defp microusd_wire_value(value) when is_integer(value), do: Integer.to_string(value)

  defp ensure_usage_supersedes_run!(%{supersedes_id: nil}, _run), do: :ok

  defp ensure_usage_supersedes_run!(%{supersedes_id: supersedes_id}, run) do
    parent =
      Repo.one(
        from(usage in RunUsage,
          where: usage.id == ^supersedes_id and usage.run_id == ^run.id,
          lock: "FOR UPDATE"
        )
      )

    unless parent, do: rollback(:invalid_request)
  end

  # Reservation state is a projection of the complete Task accounting history.
  # A terminal retry cannot be settled by a known earlier Run while another Run
  # remains unreported or unknown.
  defp reconcile_reservation!(goal, task, opts) do
    reservation =
      Repo.one(
        from(reservation in GoalBudgetReservation,
          where: reservation.goal_id == ^goal.id and reservation.task_id == ^task.id,
          lock: "FOR UPDATE"
        )
      )

    if reservation do
      accounting = task_accounting(task, reservation)

      if reservation.state != accounting.reservation_state do
        reservation
        |> GoalBudgetReservation.settle_changeset(accounting.reservation_state)
        |> stamp_update(now(opts))
        |> Repo.update!()

        true
      else
        false
      end
    else
      false
    end
  end

  defp reconcile_reservation_and_arm!(goal, task, opts) do
    reconcile_reservation!(goal, task, opts)

    if goal.state == "active" do
      current = now(opts)

      updated =
        goal
        |> Goal.transition_changeset(%{state: "active", next_wake_at: current})
        |> stamp_update(current)
        |> Repo.update!()

      enqueue_wakeup!(updated, current)
    end
  end

  defp nullable_string(map, key) do
    case value(map, key) do
      nil ->
        {:ok, nil}

      value when is_binary(value) ->
        value = String.trim(value)
        if value != "", do: {:ok, value}, else: {:error, :invalid_request}

      _ ->
        {:error, :invalid_request}
    end
  end

  defp evidence_validator_profile(evidence, "observation"),
    do: nullable_string(evidence, :validator_profile)

  defp evidence_validator_profile(evidence, _kind),
    do: required_string(evidence, :validator_profile)

  defp nullable_safe_integer(map, key) do
    case value(map, key) do
      nil ->
        {:ok, nil}

      value when is_integer(value) and value >= 0 and value <= 9_007_199_254_740_991 ->
        {:ok, value}

      _ ->
        {:error, :invalid_request}
    end
  end

  defp nullable_microusd(map, key) do
    case value(map, key) do
      nil ->
        {:ok, nil}

      value when is_binary(value) ->
        if Regex.match?(~r/^(?:0|[1-9][0-9]{0,18})$/, value) do
          case Integer.parse(value) do
            {parsed, ""} when parsed <= 9_223_372_036_854_775_807 -> {:ok, parsed}
            _ -> {:error, :invalid_request}
          end
        else
          {:error, :invalid_request}
        end

      _ ->
        {:error, :invalid_request}
    end
  end

  defp policy_microusd!(nil), do: nil

  defp policy_microusd!(value) when is_binary(value) do
    case nullable_microusd(%{"value" => value}, "value") do
      {:ok, parsed} -> parsed
      {:error, reason} -> rollback(reason)
    end
  end

  defp policy_microusd!(value)
       when is_integer(value) and value >= 0 and value <= @max_microusd,
       do: value

  defp policy_microusd!(_value), do: rollback(:invalid_request)

  defp valid_usage_cost?("unknown", nil), do: true

  defp valid_usage_cost?(basis, amount)
       when basis in ["reported", "estimated"] and is_integer(amount), do: true

  defp valid_usage_cost?(_, _), do: false

  defp required_datetime(map, key) do
    case value(map, key) do
      %DateTime{} = datetime ->
        {:ok, DateTime.truncate(datetime, :microsecond)}

      value when is_binary(value) ->
        case DateTime.from_iso8601(value) do
          {:ok, datetime, _offset} -> {:ok, DateTime.truncate(datetime, :microsecond)}
          _ -> {:error, :invalid_request}
        end

      _ ->
        {:error, :invalid_request}
    end
  end

  defp safe_document?(value) when is_map(value) do
    Enum.all?(value, fn {key, nested} ->
      case key do
        key when is_atom(key) or is_binary(key) ->
          not sensitive_document_key?(key) and
            safe_document?(nested)

        _ ->
          false
      end
    end)
  end

  defp safe_document?(value) when is_list(value), do: Enum.all?(value, &safe_document?/1)
  defp safe_document?(_value), do: true

  defp safe_evidence_document(evidence) do
    if safe_document?(evidence), do: :ok, else: {:error, :invalid_request}
  end

  defp sensitive_document_key?(key) do
    normalized =
      key
      |> to_string()
      |> Macro.underscore()
      |> String.replace(~r/[^a-z0-9]/, "")

    normalized != "tokenestimate" and
      Enum.any?(
        [
          "token",
          "secret",
          "password",
          "credential",
          "authorization",
          "rawtranscript",
          "apikey",
          "privatekey",
          "sessionfilename",
          "localhandle"
        ],
        &String.contains?(normalized, &1)
      )
  end

  defp sanitize_machine_context(value) when is_map(value) do
    Map.new(value, fn {key, nested} -> {key, sanitize_machine_context(nested)} end)
    |> Enum.reject(fn {key, _value} ->
      (is_atom(key) or is_binary(key)) and sensitive_document_key?(key)
    end)
    |> Map.new()
  end

  defp sanitize_machine_context(value) when is_list(value),
    do: Enum.map(value, &sanitize_machine_context/1)

  defp sanitize_machine_context(value), do: value

  defp context_wire_envelope!(snapshot, goal, item, task) do
    case context_wire_envelope(snapshot, goal, item, task) do
      {:ok, context} ->
        context

      {:error, reason} ->
        rollback({:invalid_contract, reason})
    end
  end

  defp context_wire_envelope(snapshot, goal, item, task) do
    document =
      (snapshot.payload || %{})
      |> sanitize_machine_context()
      |> normalize_map()

    expected_hash = context_content_hash(document)
    expected_subject = task && value(task.input || %{}, "subject")

    with true <- document["schema_version"] == "symmetry.context_snapshot.v1",
         true <- document["snapshot_id"] == snapshot.id,
         true <- document["goal_id"] == goal.id,
         true <- document["goal_revision"] == snapshot.goal_revision,
         true <- document["work_item_id"] == if(item, do: item.id, else: nil),
         true <- document["content_hash"] == context_digest(snapshot.content_hash),
         true <- document["content_hash"] == expected_hash,
         true <-
           is_nil(expected_subject) or document["subject"] == normalize_map(expected_subject),
         :ok <- validate_contract(:context_snapshot, document, []) do
      {:ok, document}
    else
      false -> {:error, :invalid_context_snapshot}
      {:error, _reason} = error -> error
    end
  end

  defp context_snapshot_document(
         goal,
         revision,
         item,
         purpose,
         _model_profile,
         subject,
         validation_bindings,
         snapshot_id,
         next_action,
         inserted_at
       ) do
    byte_budget = value(revision.context_manifest || %{}, "byte_budget", 32_768)

    approved_goal = %{
      "goal_id" => goal.id,
      "revision" => revision.revision,
      "objective" => revision.objective,
      "authority_policy" => revision.authority_policy
    }

    work_contract = %{
      "title" => item.title,
      "description" => item.description || "Approved work item.",
      "purpose" => purpose,
      "acceptance" => item.acceptance_contract,
      "validation_bindings" => validation_bindings,
      "change_target" => item.change_target
    }

    document = %{
      "schema_version" => "symmetry.context_snapshot.v1",
      "snapshot_id" => snapshot_id,
      "goal_id" => goal.id,
      "goal_revision" => revision.revision,
      "work_item_id" => item.id,
      "content_hash" => "sha256:" <> String.duplicate("0", 64),
      "created_at" => DateTime.to_iso8601(inserted_at),
      "approved_goal" => approved_goal,
      "work_contract" => work_contract,
      "subject" => subject,
      "sources" =>
        context_sources!(goal, revision, item, subject, approved_goal, work_contract, inserted_at),
      "current_decisions" => context_decisions(goal.id, revision.revision),
      "validated_evidence" => context_evidence(goal.id, revision.revision, item.id, subject),
      "failed_attempts" => context_failed_attempts(goal.id, revision.revision, item.id),
      "advisory_recall" => advisory_recall(goal, revision, item, subject),
      "next_action" => next_action,
      "size" => %{
        "mandatory_bytes" => 0,
        "optional_bytes" => 0,
        "total_bytes" => 0,
        "byte_budget" => byte_budget,
        "token_estimate" => nil
      }
    }

    sized = stabilize_context_size(document, byte_budget)

    if sized["size"]["mandatory_bytes"] > byte_budget do
      rollback(:context_budget_exceeded)
    end

    Map.put(sized, "content_hash", context_content_hash(sized))
  end

  defp planning_context_snapshot_document(
         goal,
         revision,
         resource,
         subject,
         snapshot_id,
         inserted_at
       ) do
    byte_budget = value(revision.context_manifest || %{}, "byte_budget", 32_768)

    approved_goal = %{
      "goal_id" => goal.id,
      "revision" => revision.revision,
      "objective" => revision.objective,
      "authority_policy" => revision.authority_policy
    }

    work_contract = %{
      "title" => "Plan the approved Goal",
      "description" =>
        "Produce a bounded PlanProposal for the approved Goal. This does not approve or admit work.",
      "purpose" => "plan",
      "acceptance" => planning_acceptance_contract(),
      "validation_bindings" => [],
      "change_target" => nil
    }

    document = %{
      "schema_version" => "symmetry.context_snapshot.v1",
      "snapshot_id" => snapshot_id,
      "goal_id" => goal.id,
      "goal_revision" => revision.revision,
      "work_item_id" => nil,
      "content_hash" => "sha256:" <> String.duplicate("0", 64),
      "created_at" => DateTime.to_iso8601(inserted_at),
      "approved_goal" => approved_goal,
      "work_contract" => work_contract,
      "subject" => subject,
      "sources" =>
        planning_context_sources!(
          goal,
          revision,
          resource,
          subject,
          approved_goal,
          work_contract,
          inserted_at
        ),
      "current_decisions" => context_decisions(goal.id, revision.revision),
      "validated_evidence" => [],
      "failed_attempts" => [],
      "advisory_recall" => [],
      "next_action" => nil,
      "size" => %{
        "mandatory_bytes" => 0,
        "optional_bytes" => 0,
        "total_bytes" => 0,
        "byte_budget" => byte_budget,
        "token_estimate" => nil
      }
    }

    sized = stabilize_context_size(document, byte_budget)

    if sized["size"]["mandatory_bytes"] > byte_budget do
      rollback(:context_budget_exceeded)
    end

    Map.put(sized, "content_hash", context_content_hash(sized))
  end

  defp planning_context_sources!(
         goal,
         revision,
         resource,
         subject,
         approved_goal,
         work_contract,
         inserted_at
       ) do
    observed_at = DateTime.to_iso8601(inserted_at)
    required_kinds = value(revision.context_manifest || %{}, "required_source_kinds", [])

    sources = [
      %{
        "resource_id" => resource.id,
        "source_kind" => "approved_goal",
        "source_revision" => "goal:#{goal.id}:#{revision.revision}",
        "content_hash" => context_digest(RequestHash.canonical(approved_goal)),
        "observed_at" => observed_at,
        "trust" => "trusted_policy",
        "required" => "approved_goal" in required_kinds,
        "content" => %{"kind" => "pointer", "value" => "goal:#{goal.id}:#{revision.revision}"}
      },
      %{
        "resource_id" => resource.id,
        "source_kind" => "work_contract",
        "source_revision" => "goal:#{goal.id}:#{revision.revision}:plan",
        "content_hash" => context_digest(RequestHash.canonical(work_contract)),
        "observed_at" => observed_at,
        "trust" => "trusted_policy",
        "required" => "work_contract" in required_kinds,
        "content" => %{
          "kind" => "pointer",
          "value" => "goal:#{goal.id}:#{revision.revision}:plan"
        }
      },
      %{
        "resource_id" => resource.id,
        "source_kind" => "repository_subject",
        "source_revision" => subject["commit"],
        "content_hash" => subject["tree_digest"],
        "observed_at" => observed_at,
        "trust" => "repository_untrusted",
        "required" => "repository_subject" in required_kinds,
        "content" => %{"kind" => "pointer", "value" => "repository:" <> resource.id}
      },
      %{
        "resource_id" => resource.id,
        "source_kind" => "repository",
        "source_revision" => subject["commit"],
        "content_hash" => subject["tree_digest"],
        "observed_at" => observed_at,
        "trust" => "repository_untrusted",
        "required" => "repository" in required_kinds,
        "content" => %{"kind" => "pointer", "value" => "repository:" <> resource.id}
      }
    ]

    source_kinds = sources |> Enum.map(& &1["source_kind"]) |> MapSet.new()

    unless is_list(required_kinds) and
             Enum.all?(required_kinds, &MapSet.member?(source_kinds, &1)) do
      rollback(:required_context_source_missing)
    end

    sources
  end

  defp context_sources!(goal, revision, item, subject, approved_goal, work_contract, inserted_at) do
    observed_at = DateTime.to_iso8601(inserted_at)
    required_kinds = value(revision.context_manifest || %{}, "required_source_kinds", [])

    sources = [
      %{
        "resource_id" => item.repository_resource_id,
        "source_kind" => "approved_goal",
        "source_revision" => "goal:#{goal.id}:#{revision.revision}",
        "content_hash" => context_digest(RequestHash.canonical(approved_goal)),
        "observed_at" => observed_at,
        "trust" => "trusted_policy",
        "required" => "approved_goal" in required_kinds,
        "content" => %{"kind" => "pointer", "value" => "goal:#{goal.id}:#{revision.revision}"}
      },
      %{
        "resource_id" => item.repository_resource_id,
        "source_kind" => "work_contract",
        "source_revision" => "work-item:#{item.id}",
        "content_hash" => context_digest(RequestHash.canonical(work_contract)),
        "observed_at" => observed_at,
        "trust" => "trusted_policy",
        "required" => "work_contract" in required_kinds,
        "content" => %{"kind" => "pointer", "value" => "work-item:#{item.id}"}
      },
      %{
        "resource_id" => item.repository_resource_id,
        "source_kind" => "repository_subject",
        "source_revision" => subject["commit"],
        "content_hash" => subject["tree_digest"],
        "observed_at" => observed_at,
        "trust" => "repository_untrusted",
        "required" => "repository_subject" in required_kinds,
        "content" => %{
          "kind" => "pointer",
          "value" => "repository:" <> item.repository_resource_id
        }
      },
      %{
        "resource_id" => item.repository_resource_id,
        "source_kind" => "repository",
        "source_revision" => subject["commit"],
        "content_hash" => subject["tree_digest"],
        "observed_at" => observed_at,
        "trust" => "repository_untrusted",
        "required" => "repository" in required_kinds,
        "content" => %{
          "kind" => "pointer",
          "value" => "repository:" <> item.repository_resource_id
        }
      }
    ]

    source_kinds = sources |> Enum.map(& &1["source_kind"]) |> MapSet.new()

    unless is_list(required_kinds) and
             Enum.all?(required_kinds, &MapSet.member?(source_kinds, &1)) do
      rollback(:required_context_source_missing)
    end

    sources
  end

  defp advisory_recall(goal, revision, item, subject) do
    if value(revision.context_manifest || %{}, "include_advisory_recall", false) == true do
      Repo.all(
        from(snapshot in ContextSnapshot,
          join: source_item in WorkItem,
          on: source_item.id == snapshot.work_item_id,
          join: task in Task,
          on: task.context_snapshot_id == snapshot.id,
          where:
            snapshot.goal_id == ^goal.id and
              source_item.repository_resource_id == ^item.repository_resource_id and
              task.state not in ^@nonterminal_task_states,
          order_by: [
            asc:
              fragment(
                "CASE WHEN ? = ? THEN 0 ELSE 1 END",
                snapshot.work_item_id,
                type(^item.id, :binary_id)
              ),
            desc: snapshot.goal_revision,
            desc: snapshot.inserted_at,
            asc: snapshot.id
          ],
          limit: 256,
          select: %{
            id: snapshot.id,
            work_item_id: snapshot.work_item_id,
            goal_revision: snapshot.goal_revision,
            content_hash: snapshot.content_hash,
            payload: snapshot.payload,
            inserted_at: snapshot.inserted_at
          }
        )
      )
      |> Enum.map(fn snapshot ->
        %{
          "resource_id" => item.repository_resource_id,
          "source_kind" => "context_snapshot",
          "source_revision" => "snapshot:" <> snapshot.id,
          "content_hash" => context_digest(snapshot.content_hash),
          "observed_at" => DateTime.to_iso8601(snapshot.inserted_at),
          "trust" => "advisory",
          "required" => false,
          "stale" =>
            snapshot.goal_revision != revision.revision or snapshot.work_item_id != item.id or
              normalize_map(value(snapshot.payload || %{}, "subject", %{})) != subject,
          "content" => %{"kind" => "pointer", "value" => "context_snapshot:" <> snapshot.id}
        }
      end)
    else
      []
    end
  end

  defp stabilize_context_size(document, byte_budget) do
    mandatory =
      document
      |> Map.put("advisory_recall", [])
      |> stabilize_mandatory_context_size(byte_budget)

    mandatory_bytes = mandatory["size"]["mandatory_bytes"]

    if mandatory_bytes > byte_budget do
      rollback(:context_budget_exceeded)
    end

    recall = value(document, "advisory_recall", [])

    selected =
      Enum.reduce_while(recall, [], fn source, selected ->
        candidate =
          document
          |> Map.put("advisory_recall", selected ++ [source])
          |> stabilize_optional_context_size(byte_budget, mandatory_bytes)

        if candidate["size"]["total_bytes"] <= byte_budget do
          {:cont, selected ++ [source]}
        else
          {:halt, selected}
        end
      end)

    document
    |> Map.put("advisory_recall", selected)
    |> stabilize_optional_context_size(byte_budget, mandatory_bytes)
  end

  defp stabilize_mandatory_context_size(document, byte_budget, previous \\ nil, attempts \\ 0)

  defp stabilize_mandatory_context_size(_document, _byte_budget, _previous, attempts)
       when attempts >= 16,
       do: rollback(:context_budget_exceeded)

  defp stabilize_mandatory_context_size(document, byte_budget, previous, attempts) do
    bytes = byte_size(Jason.encode!(document))

    size = %{
      "mandatory_bytes" => bytes,
      "optional_bytes" => 0,
      "total_bytes" => bytes,
      "byte_budget" => byte_budget,
      "token_estimate" => nil
    }

    if size == previous do
      document
    else
      document
      |> Map.put("size", size)
      |> stabilize_mandatory_context_size(byte_budget, size, attempts + 1)
    end
  end

  defp stabilize_optional_context_size(
         document,
         byte_budget,
         mandatory_bytes,
         previous \\ nil,
         attempts \\ 0
       )

  defp stabilize_optional_context_size(
         _document,
         _byte_budget,
         _mandatory_bytes,
         _previous,
         attempts
       )
       when attempts >= 16,
       do: rollback(:context_budget_exceeded)

  defp stabilize_optional_context_size(document, byte_budget, mandatory_bytes, previous, attempts) do
    bytes = byte_size(Jason.encode!(document))

    size = %{
      "mandatory_bytes" => mandatory_bytes,
      "optional_bytes" => max(bytes - mandatory_bytes, 0),
      "total_bytes" => bytes,
      "byte_budget" => byte_budget,
      "token_estimate" => nil
    }

    if size == previous do
      document
    else
      document
      |> Map.put("size", size)
      |> stabilize_optional_context_size(byte_budget, mandatory_bytes, size, attempts + 1)
    end
  end

  defp context_content_hash(document) when is_map(document) do
    document
    |> Map.delete("content_hash")
    |> RequestHash.canonical()
    |> context_digest()
  end

  defp context_digest(<<_::binary-size(32)>> = digest),
    do: "sha256:" <> Base.encode16(digest, case: :lower)

  defp context_digest(value) when is_binary(value) do
    if Regex.match?(~r/^sha256:[0-9a-f]{64}$/, value), do: value, else: nil
  end

  defp context_digest(_value), do: nil

  defp context_decisions(goal_id, revision) do
    Repo.all(
      from(decision in GoalDecision,
        where: decision.goal_id == ^goal_id and decision.goal_revision == ^revision,
        order_by: [asc: decision.inserted_at, asc: decision.id],
        select: %{id: decision.id, action_hash: decision.action_hash, state: decision.state}
      )
    )
    |> Enum.map(fn decision ->
      %{
        "decision_id" => decision.id,
        "action_hash" => context_digest(decision.action_hash),
        "state" => decision.state
      }
    end)
  end

  defp context_evidence(goal_id, revision, work_item_id, subject) do
    subject_hash = RequestHash.canonical(subject)

    Repo.all(
      from(evidence in RunEvidence,
        join: run in Run,
        on: run.id == evidence.run_id,
        join: task in Task,
        on: task.id == run.task_id,
        where:
          task.goal_id == ^goal_id and task.goal_revision == ^revision and
            task.work_item_id == ^work_item_id and
            task.purpose == "validate" and task.state == "completed" and
            run.generation == task.current_generation and run.state == "completed" and
            evidence.verdict == "passed" and evidence.subject_hash == ^subject_hash,
        order_by: [asc: evidence.inserted_at, asc: evidence.id],
        select: %{
          id: evidence.id,
          subject_hash: evidence.subject_hash,
          verdict: evidence.verdict,
          payload: evidence.payload
        }
      )
    )
    |> Enum.map(fn evidence ->
      %{
        "evidence_id" => evidence.id,
        "predicate_id" => value(evidence.payload || %{}, "predicate_id"),
        "subject_hash" => context_digest(evidence.subject_hash),
        "verdict" => evidence.verdict
      }
    end)
  end

  defp context_failed_attempts(goal_id, revision, work_item_id) do
    Repo.all(
      from(run in Run,
        join: task in Task,
        on: task.id == run.task_id,
        where:
          task.goal_id == ^goal_id and task.goal_revision == ^revision and
            task.work_item_id == ^work_item_id and
            run.state == "failed",
        order_by: [asc: run.inserted_at, asc: run.id],
        select: %{task_id: task.id, run_id: run.id, inserted_at: run.inserted_at}
      )
    )
    |> Enum.map(fn run ->
      %{
        "task_id" => run.task_id,
        "run_id" => run.run_id,
        "reason" => "run_failed",
        "observed_at" => DateTime.to_iso8601(run.inserted_at)
      }
    end)
  end

  defp canonical_authority_policy(policy) do
    policy
    |> normalize_map()
    |> Map.put_new("operator_required_for_scope_change", true)
    |> Map.put_new("operator_required_for_completion", true)
    |> Map.put_new("publication_allowed", false)
    |> Map.put_new("allowed_actions", [])
  end

  defp canonical_context_manifest(manifest) do
    manifest
    |> normalize_map()
    |> Map.put_new("byte_budget", 32_768)
    |> Map.put_new("required_source_kinds", ["repository"])
    |> Map.put_new("include_advisory_recall", false)
  end

  defp only_known_keys?(map, allowed) do
    map
    |> Map.keys()
    |> Enum.all?(fn
      key when is_atom(key) or is_binary(key) -> to_string(key) in allowed
      _ -> false
    end)
  end

  defp machine_write_result(result) do
    case result do
      {:ok, {disposition, receipt}} when disposition in [:created, :replayed] ->
        {:ok, receipt, disposition}

      {:error, reason} ->
        {:error, reason}
    end
  end

  @doc """
  Reconcile a terminal Task after Orchestration has committed its terminal Run.

  A terminal process settles its budget reservation and records a compact event.
  A producer result never directly accepts work. A distinct terminal validation
  Task may derive an immutable accepted or rejected outcome from its frozen
  candidate Subject and persisted evidence.
  """
  @spec settle_task(Ecto.UUID.t(), Ecto.UUID.t(), non_neg_integer(), keyword()) ::
          {:ok, map()} | {:error, term()}
  def settle_task(task_id, run_id, generation, opts \\ [])

  def settle_task(task_id, run_id, generation, opts)
      when is_binary(task_id) and is_binary(run_id) and is_integer(generation) and generation >= 0 and
             is_list(opts) do
    with :ok <- valid_uuid(task_id), :ok <- valid_uuid(run_id) do
      Repo.transaction(fn ->
        task_locator = Repo.get(Task, task_id)

        cond do
          is_nil(task_locator) or is_nil(task_locator.goal_id) ->
            {:ignored, %{task_id: task_id, run_id: run_id, generation: generation}}

          true ->
            goal = lock_goal(task_locator.goal_id)

            # Keep settlement compatible with commands that take a WorkItem
            # lock: Goal -> WorkItem -> Tasks (sorted) -> Runs (sorted).
            item =
              if task_locator.work_item_id,
                do: lock_work_item(task_locator.work_item_id)

            {task, producer} =
              lock_settlement_tasks!(task_id, task_locator.validation_of_task_id)

            unless settlement_task_owned_by_goal?(task, goal, item) and
                     task.work_item_id == task_locator.work_item_id and
                     task.validation_of_task_id == task_locator.validation_of_task_id,
                   do: rollback(:stale_run)

            {run, producing_run} = lock_settlement_runs!(run_id, task_id, producer)

            if is_nil(run) or run.generation != generation or
                 run.state not in @terminal_run_states or task.current_generation != generation or
                 task.state not in @terminal_run_states do
              rollback(:stale_run)
            end

            mutation_id = settlement_mutation_id(task.id, run.id, generation)

            body = %{
              task_id: task.id,
              run_id: run.id,
              generation: generation,
              state: run.state,
              goal_revision: task.goal_revision
            }

            case replay_command(goal.id, mutation_id, body) do
              {:ok, receipt} ->
                if plan_task?(task) do
                  {:replayed, Map.put(receipt.response, "event_sequence", receipt.event.sequence)}
                else
                  case rederive_awaiting_validation_outcome!(
                         goal,
                         item,
                         task,
                         run,
                         producer,
                         producing_run,
                         receipt.response,
                         opts
                       ) do
                    nil ->
                      {:replayed,
                       Map.put(receipt.response, "event_sequence", receipt.event.sequence)}

                    response ->
                      {:created, response}
                  end
                end

              :missing ->
                reconcile_reservation!(goal, task, opts)

                mark_harness_session_unavailable!(run, opts)

                terminal =
                  if plan_task?(task) do
                    terminal_plan_settlement!(goal, task, run, opts)
                  else
                    terminal_settlement!(goal, item, task, run, producer, producing_run, opts)
                  end

                external_wait =
                  if plan_task?(task),
                    do: nil,
                    else: prepare_external_wait!(goal, item, task, run, terminal, opts)

                terminal =
                  terminal
                  |> external_observation_terminal!(task, external_wait)
                  |> unsupported_external_wait_terminal(external_wait)

                next_wake_at = terminal_next_wake_at(goal, task, terminal, opts)

                terminal = Map.put(terminal, :next_wake_at, next_wake_at)

                response = %{
                  "goal_id" => goal.id,
                  "goal_revision" => task.goal_revision,
                  "mutation_id" => mutation_id,
                  "task_id" => task.id,
                  "run_id" => run.id,
                  "generation" => generation,
                  "settlement" => terminal.settlement,
                  "result_id" => terminal.result_id,
                  "result_kind" => terminal.result_kind,
                  "reason" => terminal.reason,
                  "blocker" => terminal.blocker,
                  "decision_id" => terminal.decision_id,
                  "proposed_next_action" => terminal.proposed_next_action,
                  "proposed_next_action_status" =>
                    if(is_nil(terminal.proposed_next_action), do: nil, else: "proposal_only"),
                  "subject_hash" => terminal.subject_hash,
                  "evidence_refs" => terminal.evidence_refs,
                  "next_wake_at" => nullable_datetime(next_wake_at)
                }

                event =
                  append_event!(
                    goal,
                    mutation_id,
                    "task_settled",
                    "system:settlement",
                    body,
                    response,
                    opts
                    |> Keyword.put(:event_revision, task.goal_revision)
                    |> Keyword.put(:settlement_next_wake_at, next_wake_at)
                  )

                finalize_external_wait!(
                  external_wait,
                  event,
                  goal,
                  item,
                  task,
                  run,
                  terminal,
                  opts
                )

                {:created, Map.put(response, "event_sequence", event.sequence)}

              {:error, reason} ->
                rollback(reason)
            end
        end
      end)
      |> case do
        {:ok, {_disposition, response}} -> {:ok, response}
        {:error, reason} -> {:error, reason}
      end
    end
  end

  def settle_task(_, _, _, _), do: {:error, :invalid_request}

  # A terminal Run is durable execution history. A completed validation candidate
  # may derive accepted or rejected work from its frozen Subject and persisted
  # evidence; other typed results remain immutable receipts only.
  defp terminal_settlement!(
         goal,
         item,
         task,
         %Run{state: "completed"} = run,
         producer,
         producing_run,
         opts
       ) do
    task_result = value(run.result || %{}, "task_result")

    if is_nil(task_result) do
      invalid_terminal_settlement("missing_result")
    else
      with true <- is_map(task_result),
           :ok <- validate_contract(:task_result, task_result, opts),
           {:ok, result_id} <- required_uuid(task_result, :result_id),
           {:ok, kind} <- required_string(task_result, :kind),
           true <- task_result_matches_task?(task, kind),
           {:ok, subject} <- parse_subject(value(task_result, "subject")),
           true <- value(subject, "resource_id") == item.repository_resource_id,
           subject_hash = RequestHash.canonical(subject),
           true <- value(task_result, "subject_hash") == context_digest(subject_hash),
           {:ok, evidence_refs} <-
             verified_result_evidence_refs(run, subject, subject_hash, task_result),
           {:ok, blocker, blocker_wake_at} <-
             validate_result_blocker(goal, item, task, task_result, opts),
           {:ok, proposed_next_action} <-
             validate_proposed_next_action(goal, item, task, task_result, opts),
           validation_outcome =
             derive_validation_outcome!(
               goal,
               item,
               task,
               run,
               producer,
               producing_run,
               kind,
               subject,
               subject_hash,
               opts
             ),
           {:ok, settlement, wake_at} <-
             dispatch_terminal_result(
               goal,
               item,
               task,
               run,
               kind,
               blocker,
               blocker_wake_at,
               subject_hash,
               evidence_refs,
               validation_outcome,
               opts
             ) do
        %{
          settlement: settlement,
          result_id: result_id,
          result_kind: kind,
          reason: validation_outcome_reason(validation_outcome, task_result),
          blocker: blocker,
          decision_id: nil,
          proposed_next_action: proposed_next_action,
          subject_hash: context_digest(subject_hash),
          evidence_refs: evidence_refs,
          wake_at: wake_at
        }
      else
        {:error, reason} ->
          invalid_terminal_settlement(terminal_error_reason(reason))

        _ ->
          invalid_terminal_settlement("invalid_task_result")
      end
    end
  end

  defp terminal_settlement!(
         _goal,
         _item,
         _task,
         %Run{state: "failed"} = run,
         _producer,
         _producing_run,
         opts
       ),
       do: process_terminal_settlement("failed", failed_terminal_reason(run, opts))

  defp terminal_settlement!(
         _goal,
         _item,
         _task,
         %Run{state: "cancelled"},
         _producer,
         _producing_run,
         _opts
       ),
       do: process_terminal_settlement("cancelled", "cancelled")

  defp terminal_plan_settlement!(goal, task, %Run{state: "completed"} = run, opts) do
    task_result = value(run.result || %{}, "task_result")
    admitted_subject = value(task.input || %{}, "subject")

    with true <- is_map(task_result),
         :ok <- validate_contract(:task_result, task_result, opts),
         {:ok, result_id} <- required_uuid(task_result, :result_id),
         "plan_proposed" <- value(task_result, :kind),
         true <- task_result_matches_task?(task, "plan_proposed"),
         {:ok, subject} <- parse_subject(value(task_result, :subject)),
         true <- subject == admitted_subject,
         subject_hash = RequestHash.canonical(subject),
         true <- value(task_result, :subject_hash) == context_digest(subject_hash),
         {:ok, evidence_refs} <-
           verified_result_evidence_refs(run, subject, subject_hash, task_result),
         proposal when is_map(proposal) <- value(task_result, :proposal),
         proposal = canonical_plan_proposal(proposal),
         true <-
           valid_plan_proposal?(proposal, %{goal | current_revision: task.goal_revision}, opts) do
      decision_id =
        if plan_result_authorized?(goal, task) do
          create_plan_decision!(goal, proposal, task, run, opts).id
        end

      %{
        settlement: if(decision_id, do: "plan_proposed", else: "historical_result"),
        result_id: result_id,
        result_kind: "plan_proposed",
        reason: nil,
        blocker: nil,
        decision_id: decision_id,
        proposed_next_action: nil,
        subject_hash: context_digest(subject_hash),
        evidence_refs: evidence_refs,
        wake_at: nil
      }
    else
      _ -> invalid_terminal_settlement("invalid_task_result")
    end
  end

  defp terminal_plan_settlement!(_goal, _task, %Run{state: "failed"} = run, opts),
    do: process_terminal_settlement("failed", failed_terminal_reason(run, opts))

  defp terminal_plan_settlement!(_goal, _task, %Run{state: "cancelled"}, _opts),
    do: process_terminal_settlement("cancelled", "cancelled")

  defp plan_result_authorized?(goal, task) do
    task.goal_revision == goal.current_revision and goal.state == "draft" and
      not pending_plan_task?(goal, task.id) and
      not Repo.exists?(
        from(item in WorkItem,
          where: item.goal_id == ^goal.id and item.admitted_revision == ^goal.current_revision
        )
      ) and not plan_decision_pending_acceptance?(goal)
  end

  defp create_plan_decision!(goal, proposal, _task, _run, opts) do
    proposal_hash = RequestHash.canonical(proposal)

    decision_changeset =
      %GoalDecision{id: Ecto.UUID.generate()}
      |> GoalDecision.changeset(%{
        goal_id: goal.id,
        goal_revision: goal.current_revision,
        work_item_id: nil,
        kind: "plan",
        action_hash: plan_action_hash(goal, proposal_hash),
        state: "open",
        question: "Accept the proposed plan?",
        options: derived_decision_options("plan"),
        subject_hash: nil,
        actor_ref: "system:plan_settlement",
        expires_at: nil
      })
      |> stamp_insert(now(opts))

    validate_decision_changeset!(decision_changeset, opts)

    case Repo.insert(decision_changeset) do
      {:ok, decision} ->
        decision

      {:error, changeset} ->
        if Keyword.has_key?(changeset.errors, :action_hash),
          do: rollback(:idempotency_conflict),
          else: rollback(:invalid_request)
    end
  end

  defp failed_terminal_reason(run, opts) do
    task_result = value(run.result || %{}, "task_result")

    task_result_failure_reason(task_result, opts) ||
      terminal_payload_failure_reason(run.failure || %{}) || "process_failure"
  end

  defp task_result_failure_reason(task_result, opts) do
    with true <- is_map(task_result),
         :ok <- validate_contract(:task_result, task_result, opts),
         true <- value(task_result, "kind") == "failed",
         reason when reason in @terminal_semantic_failure_reasons <- value(task_result, "reason") do
      reason
    else
      _ -> nil
    end
  end

  defp terminal_payload_failure_reason(payload) when is_map(payload) do
    summary = value(payload, "summary")
    task_result = value(payload, "task_result")
    error = value(payload, "error")
    reason = value(payload, "reason")

    if only_known_keys?(payload, ["summary", "task_result", "reason", "error"]) and
         is_binary(summary) and (is_nil(task_result) or is_map(task_result)) and
         (is_nil(error) or is_binary(error)) and reason in @terminal_semantic_failure_reasons do
      reason
    end
  end

  defp dispatch_task_result(
         goal,
         _item,
         task,
         _run,
         _kind,
         _blocker,
         _blocker_wake_at,
         _subject_hash,
         _evidence_refs,
         _opts
       )
       when task.goal_revision != goal.current_revision or goal.state != "active",
       do: {:ok, "historical_result", nil}

  defp dispatch_task_result(
         _goal,
         _item,
         _task,
         _run,
         "candidate_completion",
         _blocker,
         _blocker_wake_at,
         _subject_hash,
         _evidence_refs,
         _opts
       ),
       do: {:ok, "awaiting_validation", :immediate}

  defp dispatch_task_result(
         goal,
         _item,
         task,
         run,
         "progress",
         _blocker,
         _blocker_wake_at,
         subject_hash,
         evidence_refs,
         opts
       ) do
    if progress_has_new_verification?(goal, task, run, subject_hash, evidence_refs, opts),
      do: {:ok, "progress", :immediate},
      else: {:ok, "no_verified_progress", nil}
  end

  defp dispatch_task_result(
         _goal,
         _item,
         _task,
         _run,
         "blocked",
         _blocker,
         blocker_wake_at,
         _subject_hash,
         _evidence_refs,
         _opts
       ),
       do: {:ok, "blocked", blocker_wake_at}

  defp dispatch_task_result(
         _goal,
         _item,
         _task,
         _run,
         "repair_required",
         _blocker,
         _blocker_wake_at,
         _subject_hash,
         _evidence_refs,
         _opts
       ),
       do: {:ok, "repair_required", :immediate}

  defp dispatch_task_result(
         _goal,
         _item,
         _task,
         _run,
         "replan_required",
         _blocker,
         _blocker_wake_at,
         _subject_hash,
         _evidence_refs,
         _opts
       ),
       do: {:ok, "replan_required", nil}

  defp dispatch_task_result(
         _goal,
         _item,
         _task,
         _run,
         "failed",
         _blocker,
         _blocker_wake_at,
         _subject_hash,
         _evidence_refs,
         _opts
       ),
       do: {:ok, "failed", nil}

  defp dispatch_terminal_result(
         _goal,
         _item,
         _task,
         _run,
         _kind,
         _blocker,
         _blocker_wake_at,
         _subject_hash,
         _evidence_refs,
         %WorkOutcome{disposition: "rejected", reason: "historical_result"},
         _opts
       ),
       do: {:ok, "historical_result", nil}

  defp dispatch_terminal_result(
         %Goal{state: "active"},
         _item,
         _task,
         _run,
         _kind,
         _blocker,
         _blocker_wake_at,
         _subject_hash,
         _evidence_refs,
         %WorkOutcome{disposition: "rejected"},
         _opts
       ),
       do: {:ok, "validation_failed", nil}

  defp dispatch_terminal_result(
         %Goal{state: "paused"},
         _item,
         _task,
         _run,
         _kind,
         _blocker,
         _blocker_wake_at,
         _subject_hash,
         _evidence_refs,
         %WorkOutcome{disposition: "rejected"},
         _opts
       ),
       do: {:ok, "historical_result", nil}

  defp dispatch_terminal_result(
         %Goal{state: "active"},
         _item,
         _task,
         _run,
         _kind,
         _blocker,
         _blocker_wake_at,
         _subject_hash,
         _evidence_refs,
         %WorkOutcome{disposition: "accepted"},
         _opts
       ),
       do: {:ok, "accepted", :immediate}

  defp dispatch_terminal_result(
         %Goal{state: "paused"},
         _item,
         _task,
         _run,
         _kind,
         _blocker,
         _blocker_wake_at,
         _subject_hash,
         _evidence_refs,
         %WorkOutcome{disposition: "accepted"},
         _opts
       ),
       do: {:ok, "accepted", nil}

  defp dispatch_terminal_result(
         goal,
         item,
         task,
         run,
         kind,
         blocker,
         blocker_wake_at,
         subject_hash,
         evidence_refs,
         _rejected_outcome,
         opts
       ) do
    dispatch_task_result(
      goal,
      item,
      task,
      run,
      kind,
      blocker,
      blocker_wake_at,
      subject_hash,
      evidence_refs,
      opts
    )
  end

  defp validation_outcome_reason(
         %WorkOutcome{disposition: "rejected", reason: "historical_result"},
         _task_result
       ),
       do: "historical_result"

  defp validation_outcome_reason(%WorkOutcome{disposition: "rejected"}, _task_result),
    do: "validation_failed"

  defp validation_outcome_reason(%WorkOutcome{disposition: "accepted"}, _task_result),
    do: "validation_passed"

  defp validation_outcome_reason(_validation_outcome, task_result),
    do: value(task_result, "reason")

  defp invalid_terminal_settlement(reason) do
    %{
      settlement:
        if(reason == "missing_result", do: "missing_result", else: "invalid_task_result"),
      result_id: nil,
      result_kind: nil,
      reason: reason,
      blocker: nil,
      decision_id: nil,
      proposed_next_action: nil,
      subject_hash: nil,
      evidence_refs: [],
      wake_at: nil
    }
  end

  defp terminal_error_reason(reason) when is_binary(reason), do: reason
  defp terminal_error_reason(_reason), do: "invalid_task_result"

  defp process_terminal_settlement(settlement, reason) do
    %{
      settlement: settlement,
      result_id: nil,
      result_kind: nil,
      reason: reason,
      blocker: nil,
      decision_id: nil,
      proposed_next_action: nil,
      subject_hash: nil,
      evidence_refs: [],
      wake_at: nil
    }
  end

  defp prepare_external_wait!(goal, item, task, _run, terminal, _opts) do
    active_wait =
      if task.purpose == "observe" do
        external_wait_for_observation(goal, item, task)
      else
        Repo.one(
          from(wait in GoalExternalWait,
            where:
              wait.goal_id == ^goal.id and wait.goal_revision == ^task.goal_revision and
                wait.work_item_id == ^item.id and wait.state in ^GoalExternalWait.active_states(),
            lock: "FOR UPDATE"
          )
        )
      end

    case active_wait do
      %GoalExternalWait{} = wait ->
        # A pre-existing active wait cannot be observed without a server-owned
        # checker receipt. Terminalize it rather than rearming model polling.
        {:unsupported, wait}

      nil ->
        case {task.purpose, terminal.result_kind, terminal.blocker} do
          {_purpose, "blocked", %{"kind" => "external"} = blocker} ->
            {:new, blocker}

          _ ->
            nil
        end
    end
  end

  defp external_wait_for_observation(_goal, _item, %Task{purpose: purpose})
       when purpose != "observe",
       do: nil

  defp external_wait_for_observation(goal, item, task) do
    case Repo.one(
           from(wait in GoalExternalWait,
             where:
               wait.goal_id == ^goal.id and wait.goal_revision == ^task.goal_revision and
                 wait.work_item_id == ^item.id and wait.state == "unsupported",
             lock: "FOR UPDATE"
           )
         ) do
      nil ->
        nil

      %GoalExternalWait{} = wait ->
        if external_observation_admission_matches_wait?(goal, item, task, wait),
          do: wait,
          else: rollback(:external_wait_stale)
    end
  end

  defp external_observation_admission_matches_wait?(goal, item, task, wait) do
    expected_action = %{
      "kind" => "observe",
      "resource_id" => wait.resource_id,
      "external_ref" => wait.external_ref
    }

    with true <-
           task.admission_key ==
             automatic_admission_mutation_id(goal, item, "observe", external_wait_identity(wait)),
         true <- value(task.input || %{}, "purpose") == "observe",
         true <- value(task.input || %{}, "subject") == wait.subject,
         %ContextSnapshot{} = snapshot <- Repo.get(ContextSnapshot, task.context_snapshot_id),
         payload when is_map(payload) <- snapshot.payload,
         true <- snapshot.goal_id == goal.id,
         true <- snapshot.goal_revision == task.goal_revision,
         true <- snapshot.work_item_id == item.id,
         true <- value(payload, "subject") == wait.subject,
         true <- value(payload, "next_action") == expected_action do
      true
    else
      _ -> false
    end
  end

  # External checks require a server-derived Integration receipt contract. The
  # current adapters do not expose that capability, so retain the model result
  # as durable history without turning its selector into a model observation.
  defp create_external_wait!(goal, item, task, run, terminal, blocker, event, opts) do
    task_result = value(run.result || %{}, "task_result")
    subject = value(task_result, "subject")

    %GoalExternalWait{id: Ecto.UUID.generate()}
    |> GoalExternalWait.changeset(%{
      goal_id: goal.id,
      goal_revision: task.goal_revision,
      work_item_id: item.id,
      task_id: task.id,
      run_id: run.id,
      run_generation: run.generation,
      source_ref: %{
        "kind" => "task_result",
        "task_id" => task.id,
        "run_id" => run.id,
        "generation" => run.generation,
        "result_id" => terminal.result_id
      },
      result_id: terminal.result_id,
      result: task_result,
      resource_id: blocker["resource_id"],
      external_ref: blocker["external_ref"],
      subject: subject,
      subject_hash: RequestHash.canonical(subject),
      next_check_at: nil,
      state: "unsupported",
      receipt_event_id: event.id
    })
    |> stamp_insert(now(opts))
    |> Repo.insert!()
  end

  defp finalize_external_wait!(nil, _event, _goal, _item, _task, _run, _terminal, _opts), do: :ok

  defp finalize_external_wait!({:new, blocker}, event, goal, item, task, run, terminal, opts) do
    create_external_wait!(goal, item, task, run, terminal, blocker, event, opts)
    :ok
  end

  defp finalize_external_wait!(
         {:unsupported, _wait},
         _event,
         _goal,
         _item,
         _task,
         _run,
         _terminal,
         _opts
       ) do
    # Existing unsupported waits are terminal historical records. Their first
    # settlement event is immutable, so a later Task cannot attach a new one.
    :ok
  end

  defp external_observation_terminal!(terminal, _task, _external_wait), do: terminal

  defp unsupported_external_wait_terminal(
         %{result_kind: "progress"} = terminal,
         {:unsupported, %GoalExternalWait{}}
       ) do
    %{
      terminal
      | settlement: "no_verified_progress",
        reason: "unsupported_external_check",
        wake_at: nil,
        proposed_next_action: nil
    }
  end

  defp unsupported_external_wait_terminal(
         terminal,
         {:new, %{"kind" => "external"}}
       ) do
    %{
      terminal
      | reason: "unsupported_external_check",
        wake_at: nil,
        proposed_next_action: nil
    }
  end

  defp unsupported_external_wait_terminal(terminal, {:unsupported, %GoalExternalWait{}}) do
    %{
      terminal
      | reason: "unsupported_external_check",
        wake_at: nil,
        proposed_next_action: nil
    }
  end

  defp unsupported_external_wait_terminal(terminal, _external_wait), do: terminal

  defp verified_result_evidence_refs(run, subject, subject_hash, task_result) do
    with refs when is_list(refs) <- value(task_result, "evidence_refs"),
         true <- Enum.all?(refs, &valid_uuid?/1),
         true <- length(refs) == MapSet.size(MapSet.new(refs)) do
      evidence =
        Repo.all(
          from(row in RunEvidence,
            where: row.id in ^refs and row.run_id == ^run.id and row.kind != "observation",
            select: %{
              id: row.id,
              subject_hash: row.subject_hash,
              payload: row.payload,
              source_ref: row.source_ref
            }
          )
        )

      if length(evidence) == length(refs) and
           Enum.all?(evidence, fn row ->
             row.subject_hash == subject_hash and value(row.payload, "subject") == subject and
               value(row.payload, "subject_hash") == context_digest(subject_hash) and
               value(row.source_ref, "subject_hash") == context_digest(subject_hash)
           end) do
        {:ok, Enum.sort(refs)}
      else
        {:error, "invalid_evidence_refs"}
      end
    else
      _ -> {:error, "invalid_evidence_refs"}
    end
  end

  defp validate_result_blocker(goal, item, task, task_result, opts) do
    blocker = value(task_result, "blocker")
    kind = value(task_result, "kind")

    cond do
      kind == "blocked" and not is_map(blocker) ->
        {:error, "invalid_blocker"}

      kind != "blocked" and not is_nil(blocker) ->
        {:error, "unexpected_blocker"}

      is_nil(blocker) ->
        {:ok, nil, nil}

      task.goal_revision != goal.current_revision or goal.state != "active" ->
        {:ok, normalize_map(blocker), nil}

      true ->
        validate_current_blocker(goal, item, task, blocker, now(opts))
    end
  end

  defp validate_current_blocker(goal, item, task, blocker, current) do
    case value(blocker, "kind") do
      "decision" -> validate_decision_blocker(goal, item, task, blocker, current)
      "external" -> validate_external_blocker(item, blocker, current)
      "dependency" -> validate_dependency_blocker(goal, item, task, blocker)
      "environment" -> {:ok, normalize_map(blocker), nil}
      _ -> {:error, "invalid_blocker"}
    end
  end

  defp validate_decision_blocker(goal, item, task, blocker, current) do
    with {:ok, decision_id} <- required_uuid(blocker, :decision_id),
         %GoalDecision{} = decision <-
           Repo.one(
             from(decision in GoalDecision,
               where: decision.id == ^decision_id,
               lock: "FOR UPDATE"
             )
           ),
         true <-
           decision.goal_id == goal.id and decision.goal_revision == task.goal_revision and
             decision.state == "open" and decision.work_item_id in [nil, item.id] and
             (is_nil(decision.expires_at) or DateTime.compare(decision.expires_at, current) == :gt) do
      {:ok, %{"kind" => "decision", "decision_id" => decision.id}, nil}
    else
      _ -> {:error, "invalid_blocker"}
    end
  end

  defp validate_external_blocker(item, blocker, current) do
    with {:ok, resource_id} <- required_uuid(blocker, :resource_id),
         true <- resource_id == item.repository_resource_id,
         {:ok, external_ref} <- required_string(blocker, :external_ref),
         true <- byte_size(external_ref) <= 1_000,
         {:ok, next_check_at} <- future_utc_datetime(value(blocker, "next_check_at"), current) do
      {:ok,
       %{
         "kind" => "external",
         "resource_id" => resource_id,
         "external_ref" => external_ref,
         "next_check_at" => DateTime.to_iso8601(next_check_at)
       }, next_check_at}
    else
      _ -> {:error, "invalid_blocker"}
    end
  end

  defp validate_dependency_blocker(goal, item, task, blocker) do
    with ids when is_list(ids) <- value(blocker, "work_item_ids"),
         true <- Enum.all?(ids, &valid_uuid?/1),
         true <- length(ids) == MapSet.size(MapSet.new(ids)) do
      dependency_ids =
        Repo.all(
          from(dependency in WorkDependency,
            where: dependency.goal_id == ^goal.id and dependency.work_item_id == ^item.id,
            select: dependency.depends_on_id
          )
        )

      unresolved? =
        Enum.all?(ids, fn dependency_id ->
          not Repo.exists?(
            from(outcome in WorkOutcome,
              where:
                outcome.goal_id == ^goal.id and outcome.goal_revision == ^task.goal_revision and
                  outcome.work_item_id == ^dependency_id and outcome.disposition == "accepted"
            )
          )
        end)

      if MapSet.equal?(MapSet.new(ids), MapSet.new(dependency_ids)) and unresolved? do
        {:ok, %{"kind" => "dependency", "work_item_ids" => Enum.sort(ids)}, nil}
      else
        {:error, "invalid_blocker"}
      end
    else
      _ -> {:error, "invalid_blocker"}
    end
  end

  defp validate_proposed_next_action(goal, item, task, task_result, opts) do
    case value(task_result, "proposed_next_action") do
      nil ->
        {:ok, nil}

      action when is_map(action) ->
        validate_current_next_action(goal, item, task, action, now(opts))

      _ ->
        {:error, "invalid_next_action"}
    end
  end

  defp validate_current_next_action(goal, item, task, action, current) do
    if task.goal_revision != goal.current_revision or goal.state != "active" do
      {:ok, normalize_map(action)}
    else
      case value(action, "kind") do
        "validate" ->
          if value(action, "producing_task_id") == task.id,
            do: {:ok, normalize_map(action)},
            else: {:error, "invalid_next_action"}

        "repair" ->
          if value(action, "work_item_id") == item.id,
            do: {:ok, normalize_map(action)},
            else: {:error, "invalid_next_action"}

        "observe" ->
          if value(action, "resource_id") == item.repository_resource_id,
            do: {:ok, normalize_map(action)},
            else: {:error, "invalid_next_action"}

        "replan" ->
          {:ok, normalize_map(action)}

        "wait" ->
          case validate_current_blocker(goal, item, task, value(action, "blocker"), current) do
            {:ok, _blocker, _wake_at} -> {:ok, normalize_map(action)}
            {:error, _reason} -> {:error, "invalid_next_action"}
          end

        _ ->
          {:error, "invalid_next_action"}
      end
    end
  end

  defp progress_has_new_verification?(goal, task, run, subject_hash, evidence_refs, opts) do
    previous_progress =
      Repo.all(
        from(previous_run in Run,
          join: previous_task in Task,
          on: previous_task.id == previous_run.task_id,
          where:
            previous_run.id != ^run.id and previous_run.state == "completed" and
              previous_task.goal_id == ^goal.id and
              previous_task.goal_revision == ^task.goal_revision and
              previous_task.work_item_id == ^item_id(task),
          select: %{run: previous_run, task: previous_task}
        )
      )
      |> Enum.flat_map(fn %{run: previous_run} ->
        previous_result = value(previous_run.result || %{}, "task_result")

        with true <- is_map(previous_result),
             :ok <- validate_contract(:task_result, previous_result, opts),
             "progress" <- value(previous_result, "kind"),
             {:ok, previous_subject} <- parse_subject(value(previous_result, "subject")),
             previous_subject_hash = RequestHash.canonical(previous_subject),
             true <-
               value(previous_result, "subject_hash") == context_digest(previous_subject_hash),
             {:ok, previous_refs} <-
               verified_result_evidence_refs(
                 previous_run,
                 previous_subject,
                 previous_subject_hash,
                 previous_result
               ) do
          [%{subject_hash: previous_subject_hash, evidence_refs: previous_refs}]
        else
          _ -> []
        end
      end)

    previous_subject_hashes = MapSet.new(previous_progress, & &1.subject_hash)

    previous_evidence_fingerprints =
      previous_progress
      |> Enum.flat_map(& &1.evidence_refs)
      |> evidence_fingerprints()

    current_evidence_fingerprints = evidence_fingerprints(evidence_refs)

    not MapSet.member?(previous_subject_hashes, subject_hash) or
      not MapSet.subset?(current_evidence_fingerprints, previous_evidence_fingerprints)
  end

  defp item_id(%Task{work_item_id: item_id}), do: item_id

  defp evidence_fingerprints([]), do: MapSet.new()

  defp evidence_fingerprints(evidence_ids) do
    Repo.all(
      from(evidence in RunEvidence,
        where: evidence.id in ^evidence_ids,
        select: %{
          kind: evidence.kind,
          source_ref: evidence.source_ref,
          source_revision: evidence.source_revision,
          validator_profile: evidence.validator_profile,
          verdict: evidence.verdict,
          payload: evidence.payload
        }
      )
    )
    |> MapSet.new(&RequestHash.canonical/1)
  end

  defp future_utc_datetime(value, current) when is_binary(value) do
    case DateTime.from_iso8601(value) do
      {:ok, datetime, 0} ->
        if DateTime.compare(datetime, current) == :gt,
          do: {:ok, datetime},
          else: {:error, :invalid_datetime}

      _ ->
        {:error, :invalid_datetime}
    end
  end

  defp future_utc_datetime(_value, _current), do: {:error, :invalid_datetime}

  defp terminal_next_wake_at(goal, task, terminal, opts) do
    current = now(opts)

    next_wake_at =
      if task.goal_revision == goal.current_revision and goal.state == "active" do
        case terminal.wake_at do
          :immediate -> current
          %DateTime{} = requested -> earliest_future_wake(goal.next_wake_at, requested, current)
          nil -> existing_future_wake(goal.next_wake_at, current)
        end
      else
        existing_future_wake(goal.next_wake_at, current)
      end

    external_wait_wake_at =
      if goal.state == "active", do: external_wait_wake_at(goal, current), else: nil

    earliest_future_wake(next_wake_at, external_wait_wake_at, current)
  end

  defp external_wait_wake_at(_goal, _current), do: nil

  defp earliest_future_wake(existing, requested, current) do
    case existing_future_wake(existing, current) do
      nil -> requested
      existing when is_nil(requested) -> existing
      existing -> if(DateTime.compare(existing, requested) == :lt, do: existing, else: requested)
    end
  end

  defp existing_future_wake(%DateTime{} = wake_at, current) do
    if DateTime.compare(wake_at, current) == :gt, do: wake_at, else: nil
  end

  defp existing_future_wake(_wake_at, _current), do: nil

  @doc """
  Settles a Goal Task cancelled before any Run existed.

  The caller must invoke this only after Orchestration durably records the
  applied runless cancellation command. This context verifies that no Run was
  fabricated, releases the reservation, and records one deterministic receipt.
  """
  @spec settle_unstarted_task(Ecto.UUID.t(), String.t(), keyword()) ::
          {:ok, map()} | {:error, term()}
  def settle_unstarted_task(task_id, reason, opts \\ [])

  def settle_unstarted_task(task_id, reason, opts)
      when is_binary(task_id) and is_binary(reason) and is_list(opts) do
    with :ok <- valid_uuid(task_id),
         {:ok, reason} <- required_string(%{reason: reason}, :reason) do
      if reason == "cancelled",
        do: settle_unstarted_cancelled(task_id, reason, opts),
        else: {:error, :invalid_request}
    end
  end

  def settle_unstarted_task(_, _, _), do: {:error, :invalid_request}

  defp settle_unstarted_cancelled(task_id, reason, opts) do
    Repo.transaction(fn ->
      untrusted_task = Repo.get(Task, task_id) || rollback(:not_found)
      goal = lock_goal(untrusted_task.goal_id || rollback(:not_found))
      mutation_id = unstarted_settlement_mutation_id(task_id, reason)

      body = %{
        task_id: task_id,
        state: "cancelled",
        reason: reason,
        goal_revision: untrusted_task.goal_revision
      }

      case replay_command(goal.id, mutation_id, body) do
        {:ok, receipt} ->
          {:replayed, receipt.response}

        :missing ->
          item = if untrusted_task.work_item_id, do: lock_work_item(untrusted_task.work_item_id)
          task = lock_task(task_id)

          unless settlement_task_owned_by_goal?(task, goal, item) and task.state == "cancelled" and
                   not Repo.exists?(from(run in Run, where: run.task_id == ^task.id)) do
            rollback(:invalid_unstarted_settlement)
          end

          unless recoverable_historical_unstarted_cancellation?(goal, task.id),
            do: rollback(:invalid_unstarted_settlement)

          reservation =
            Repo.one(
              from(reservation in GoalBudgetReservation,
                where: reservation.goal_id == ^goal.id and reservation.task_id == ^task.id,
                lock: "FOR UPDATE"
              )
            ) || rollback(:not_found)

          unless reservation.state == "held", do: rollback(:invalid_unstarted_settlement)

          reservation
          |> GoalBudgetReservation.settle_changeset("released")
          |> stamp_update(now(opts))
          |> Repo.update!()

          response = %{
            "goal_id" => goal.id,
            "goal_revision" => task.goal_revision,
            "task_id" => task.id,
            "reason" => reason,
            "settlement" => "released_without_run"
          }

          event =
            append_event!(
              goal,
              mutation_id,
              "task_unstarted_settled",
              "system:goal_control",
              body,
              response,
              Keyword.put(opts, :event_revision, task.goal_revision)
            )

          {:created, Map.put(response, "event_sequence", event.sequence)}

        {:error, error} ->
          rollback(error)
      end
    end)
    |> case do
      {:ok, {_disposition, response}} -> {:ok, response}
      {:error, error} -> {:error, error}
    end
  end

  @doc """
  Returns active Goals due for reconciliation. The caller is a durable worker;
  this function takes fresh row locks before making each scheduling decision.
  """
  @spec reconcile_due_goals(keyword()) :: {:ok, [map()]} | {:error, term()}
  def reconcile_due_goals(opts \\ [])

  def reconcile_due_goals(opts) when is_list(opts) do
    current = now(opts)
    requested_goal_ids = Keyword.get(opts, :goal_ids)

    if not (is_nil(requested_goal_ids) or
              (is_list(requested_goal_ids) and Enum.all?(requested_goal_ids, &valid_uuid?/1))) do
      {:error, :invalid_request}
    else
      reconcile_due_goal_rows(current, requested_goal_ids, opts)
    end
  rescue
    error in [Ecto.Query.CastError] -> {:error, error}
  end

  def reconcile_due_goals(_), do: {:error, :invalid_request}

  defp reconcile_automatic_goal!(goal, current, opts) do
    case automatic_reconciliation_next(goal, opts) do
      {:admit, item, purpose, model_profile, payload, source_identity} ->
        {:candidate,
         %{
           goal_id: goal.id,
           revision: goal.current_revision,
           item: item,
           purpose: purpose,
           model_profile: model_profile,
           payload: payload,
           source_identity: source_identity,
           identity: automatic_candidate_identity(item, purpose, source_identity)
         }}

      {:deferred, reason, deferred_identity} ->
        defer_automatic_reconciliation!(goal, current, opts, reason, deferred_identity)

      {:goal_blocked, reason} ->
        append_automatic_goal_blocker!(goal, current, opts, reason)

      {:policy_blocked, item, purpose, source_identity, reason} ->
        append_automatic_admission_blocker!(
          goal,
          current,
          opts,
          item,
          purpose,
          source_identity,
          reason
        )

      :quiescent ->
        next_wake_at = external_wait_wake_at(goal, current)

        updated =
          goal
          |> Goal.transition_changeset(%{state: "active", next_wake_at: next_wake_at})
          |> stamp_update(current)
          |> Repo.update!()

        enqueue_wakeup!(updated, current)

        %{goal_id: updated.id, revision: updated.current_revision, disposition: :noop}
    end
  end

  defp automatic_reconciliation_next(goal, opts) do
    revision = current_revision!(goal)

    cond do
      value(revision.execution_policy || %{}, "automatic_execution", false) != true ->
        :quiescent

      not admission_enabled?(opts) ->
        {:deferred, "goal_admission_disabled", "goal:goal_admission_disabled"}

      true ->
        case automatic_admission_capacity_status(goal, revision) do
          :ok ->
            automatic_reconciliation_after_capacity(goal, revision, opts)

          :quiescent ->
            :quiescent

          {:blocked, reason} ->
            if automatic_goal_blocked?(goal, reason),
              do: :quiescent,
              else: {:goal_blocked, reason}

          {:deferred, reason} ->
            {:deferred, reason, "goal:" <> reason}
        end
    end
  end

  defp automatic_reconciliation_after_capacity(goal, revision, opts) do
    case automatic_budget_status(goal, revision) do
      :ok ->
        case automatic_next_admission(goal, opts) do
          {:admit, _item, _purpose, _model_profile, _payload, _source_identity} = admission ->
            admission

          :none ->
            case automatic_policy_blocked_candidate(goal, opts) do
              {item, purpose, _model_profile, _payload, source_identity} ->
                {:policy_blocked, item, purpose, source_identity, "action_not_allowed"}

              nil ->
                cond do
                  automatic_admission_candidates(goal, opts) != [] ->
                    :quiescent

                  automatic_recoverable_pending_work?(goal) ->
                    {:deferred, "waiting_for_admission_condition",
                     "goal:waiting_for_admission_condition"}

                  true ->
                    :quiescent
                end
            end
        end

      {:error, :budget_exhausted} ->
        if automatic_budget_may_change?(goal) do
          {:deferred, "budget_exhausted", "goal:budget_exhausted"}
        else
          :quiescent
        end

      {:error, reason} ->
        reason = automatic_deferred_reason(reason)
        {:deferred, reason, "goal:" <> reason}
    end
  end

  defp attempt_automatic_admission!(
         goal,
         current,
         opts,
         item,
         purpose,
         model_profile,
         payload,
         source_identity
       ) do
    task = admit_authorized_task!(goal, item, purpose, model_profile, payload, opts)
    mutation_id = automatic_admission_mutation_id(goal, item, purpose, source_identity)

    event =
      append_event!(
        goal,
        mutation_id,
        "automatic_task_admitted",
        "system:wakeup",
        %{
          work_item_id: item.id,
          task_id: task.id,
          purpose: purpose,
          source_task_id: automatic_source_task_id(source_identity),
          source_result_id: automatic_source_result_id(source_identity),
          source_identity: source_identity,
          admission_key: task.admission_key
        },
        %{
          "goal_id" => goal.id,
          "goal_revision" => goal.current_revision,
          "disposition" => "admitted",
          "task_id" => task.id,
          "work_item_id" => item.id,
          "purpose" => purpose,
          "source_identity" => source_identity
        },
        Keyword.merge(opts, now: current, settlement_next_wake_at: current)
      )

    %{
      goal_id: goal.id,
      revision: goal.current_revision,
      disposition: :admitted,
      task_id: task.id,
      work_item_id: item.id,
      purpose: purpose,
      event_sequence: event.sequence
    }
  end

  defp automatic_next_admission(goal, opts) do
    revision = current_revision!(goal)

    goal
    |> automatic_admission_candidates(opts)
    |> Enum.find_value(:none, fn {item, purpose, model_profile, payload, source_identity} ->
      if automatic_action_allowed?(revision, purpose) and
           not automatic_candidate_blocked?(goal, item, purpose, source_identity) do
        {:admit, item, purpose, model_profile, payload, source_identity}
      end
    end)
  end

  defp automatic_policy_blocked_candidate(goal, opts) do
    revision = current_revision!(goal)

    Enum.find(automatic_admission_candidates(goal, opts), fn {item, purpose, _profile, _payload,
                                                              source_identity} ->
      not automatic_action_allowed?(revision, purpose) and
        not automatic_candidate_blocked?(goal, item, purpose, source_identity)
    end)
  end

  defp automatic_admission_candidates(goal, opts) do
    goal
    |> automatic_candidates(opts)
    |> Enum.flat_map(fn candidate ->
      case automatic_admission_payload(goal, candidate, opts) do
        {:ok, item, purpose, model_profile, payload, source_identity} ->
          if not accepted_work_item?(goal, item.id) and
               automatic_dependencies_satisfied?(goal, item) and
               not automatic_source_consumed?(goal, item, purpose, source_identity) do
            [{item, purpose, model_profile, payload, source_identity}]
          else
            []
          end

        :none ->
          []
      end
    end)
  end

  # The candidate task/result is a durable source identity. Neither time nor an
  # event sequence participates in this choice, so a repeated wake selects the
  # same admission until its transaction commits.
  defp automatic_candidates(goal, opts) do
    automatic_validation_candidates(goal) ++
      automatic_external_wait_candidates(goal, opts) ++
      automatic_repair_candidates(goal) ++
      automatic_baseline_candidates(goal) ++ automatic_progress_candidates(goal)
  end

  defp automatic_validation_candidates(goal) do
    current_work_items(goal)
    |> Enum.flat_map(fn item ->
      if active_task_for_item?(item.id) do
        []
      else
        Repo.all(
          from(task in Task,
            where:
              task.goal_id == ^goal.id and task.goal_revision == ^goal.current_revision and
                task.work_item_id == ^item.id and task.purpose != "validate" and
                task.state == "completed" and task.current_generation > 0,
            order_by: [asc: task.inserted_at, asc: task.id]
          )
        )
        |> Enum.flat_map(fn task ->
          if awaiting_validation_settlement?(goal, task) and
               not Repo.exists?(
                 from(validation in Task,
                   where:
                     validation.goal_id == ^goal.id and
                       validation.validation_of_task_id == ^task.id
                 )
               ) do
            [{:validate, item.id, task.id}]
          else
            []
          end
        end)
      end
    end)
  end

  # An external wait becomes schedulable only with a server-owned Integration
  # checker and exact Subject-bound receipt contract.
  defp automatic_external_wait_candidates(_goal, _opts), do: []

  defp automatic_repair_candidates(goal) do
    current_work_items(goal)
    |> Enum.flat_map(fn item ->
      if active_task_for_item?(item.id) or accepted_work_item?(goal, item.id) do
        []
      else
        Repo.all(
          from(task in Task,
            where:
              task.goal_id == ^goal.id and task.goal_revision == ^goal.current_revision and
                task.work_item_id == ^item.id and task.purpose != "validate" and
                task.state == "completed" and task.current_generation > 0,
            order_by: [desc: task.inserted_at, desc: task.id]
          )
        )
        |> Enum.flat_map(fn task ->
          if settled_as?(goal, task, "repair_required"),
            do: [{:repair, item.id, task.id}],
            else: []
        end)
      end
    end)
  end

  defp automatic_progress_candidates(goal) do
    current_work_items(goal)
    |> Enum.flat_map(fn item ->
      if active_task_for_item?(item.id) or accepted_work_item?(goal, item.id) do
        []
      else
        Repo.all(
          from(task in Task,
            where:
              task.goal_id == ^goal.id and task.goal_revision == ^goal.current_revision and
                task.work_item_id == ^item.id and task.purpose in ["implement", "observe"] and
                task.state == "completed" and task.current_generation > 0,
            order_by: [desc: task.inserted_at, desc: task.id]
          )
        )
        |> Enum.flat_map(fn task ->
          if settled_as?(goal, task, "progress"), do: [{:implement, item.id, task.id}], else: []
        end)
      end
    end)
  end

  defp automatic_baseline_candidates(goal) do
    current_work_items(goal)
    |> Enum.filter(fn item ->
      baseline_source_present?(item) and
        not active_task_for_item?(item.id) and
        not current_revision_task?(goal, item.id) and
        not accepted_work_item?(goal, item.id)
    end)
    |> Enum.map(fn item -> {:implement, item.id, :baseline} end)
  end

  defp automatic_admission_payload(goal, {:validate, work_item_id, producer_id}, _opts) do
    item = lock_work_item(work_item_id)
    producer = lock_task(producer_id)
    model_profile = producer.agent_profile

    if is_short_identifier?(model_profile) do
      {:ok, item, "validate", model_profile,
       %{
         validation_of_task_id: producer.id,
         admission_key: automatic_admission_mutation_id(goal, item, "validate", producer.id)
       }, producer.id}
    else
      :none
    end
  end

  defp automatic_admission_payload(goal, {:observe, wait_id}, _opts) do
    wait = lock_external_wait(wait_id)
    item = lock_work_item(wait.work_item_id)
    source_task = lock_task(wait.task_id)

    with true <- wait.goal_id == goal.id,
         true <- wait.goal_revision == goal.current_revision,
         true <- wait.state in GoalExternalWait.active_states(),
         true <- source_task.goal_id == goal.id,
         true <- source_task.goal_revision == goal.current_revision,
         true <- source_task.work_item_id == item.id,
         true <- is_short_identifier?(source_task.agent_profile) do
      source_identity = external_wait_identity(wait)

      {:ok, item, "observe", source_task.agent_profile,
       %{
         subject: wait.subject,
         admission_key: automatic_admission_mutation_id(goal, item, "observe", source_identity),
         next_action: %{
           "kind" => "observe",
           "resource_id" => wait.resource_id,
           "external_ref" => wait.external_ref
         }
       }, source_identity}
    else
      _ -> :none
    end
  end

  defp automatic_admission_payload(goal, {:repair, work_item_id, source_task_id}, opts) do
    item = lock_work_item(work_item_id)
    source_task = lock_task(source_task_id)

    with true <- source_task.goal_id == goal.id,
         true <- source_task.goal_revision == goal.current_revision,
         true <- source_task.purpose != "validate",
         true <- source_task.state == "completed",
         true <- is_short_identifier?(source_task.agent_profile),
         {:ok, subject, result_id, action} <- automatic_repair_subject(source_task, item, opts) do
      source_identity = "repair:" <> source_task.id <> ":" <> result_id

      {:ok, item, "implement", source_task.agent_profile,
       %{
         subject: subject,
         admission_key: automatic_admission_mutation_id(goal, item, "implement", source_identity),
         next_action: action
       }, source_identity}
    else
      _ -> :none
    end
  end

  defp automatic_admission_payload(goal, {:implement, work_item_id, :baseline}, _opts) do
    item = lock_work_item(work_item_id)
    model_profile = item.agent_profile

    with true <- is_short_identifier?(model_profile),
         {:ok, subject, source_identity} <- automatic_baseline_subject(goal, item) do
      {:ok, item, "implement", model_profile,
       %{
         subject: subject,
         admission_key: automatic_admission_mutation_id(goal, item, "implement", source_identity)
       }, source_identity}
    else
      _ -> :none
    end
  end

  defp automatic_admission_payload(goal, {:implement, work_item_id, source_task_id}, opts) do
    item = lock_work_item(work_item_id)
    source_task = lock_task(source_task_id)

    with true <- source_task.goal_id == goal.id,
         true <- source_task.goal_revision == goal.current_revision,
         true <- source_task.purpose in ["implement", "observe"],
         true <- source_task.state == "completed",
         true <- is_short_identifier?(source_task.agent_profile),
         {:ok, subject, source_identity} <-
           automatic_progress_subject(goal, item, source_task, opts) do
      {:ok, item, "implement", source_task.agent_profile,
       %{
         subject: subject,
         admission_key: automatic_admission_mutation_id(goal, item, "implement", source_identity)
       }, source_identity}
    else
      _ -> :none
    end
  end

  defp automatic_progress_subject(goal, item, source_task, opts) do
    with %Run{} = run <-
           Repo.one(
             from(run in Run,
               where:
                 run.task_id == ^source_task.id and
                   run.generation == ^source_task.current_generation,
               lock: "FOR UPDATE"
             )
           ),
         true <- run.state == "completed",
         true <- settled_as?(goal, source_task, "progress"),
         task_result when is_map(task_result) <- value(run.result || %{}, "task_result"),
         :ok <- validate_contract(:task_result, task_result, opts),
         {:ok, subject} <- parse_subject(value(task_result, "subject")),
         {:ok, result_id} <- required_uuid(task_result, :result_id),
         subject_hash = RequestHash.canonical(subject),
         true <- value(task_result, "kind") == "progress",
         true <- value(task_result, "subject_hash") == context_digest(subject_hash),
         true <- value(subject, "resource_id") == item.repository_resource_id do
      {:ok, subject,
       "progress:" <>
         source_task.id <>
         ":" <> run.id <> ":" <> Integer.to_string(run.generation) <> ":" <> result_id}
    else
      _ -> :none
    end
  end

  defp automatic_repair_subject(source_task, item, opts) do
    with %Run{} = run <-
           Repo.one(
             from(run in Run,
               where:
                 run.task_id == ^source_task.id and
                   run.generation == ^source_task.current_generation,
               lock: "FOR UPDATE"
             )
           ),
         true <- run.state == "completed",
         task_result when is_map(task_result) <- value(run.result || %{}, "task_result"),
         :ok <- validate_contract(:task_result, task_result, opts),
         "repair_required" <- value(task_result, "kind"),
         {:ok, result_id} <- required_uuid(task_result, :result_id),
         {:ok, subject} <- parse_subject(value(task_result, "subject")),
         true <- value(subject, "resource_id") == item.repository_resource_id,
         %{"kind" => "repair", "work_item_id" => work_item_id} = action <-
           value(task_result, "proposed_next_action"),
         true <- work_item_id == item.id do
      {:ok, subject, result_id, action}
    else
      _ -> :none
    end
  end

  defp automatic_baseline_subject(goal, item) do
    case item.baseline_subject do
      subject when is_map(subject) ->
        with {:ok, parsed} <- parse_subject(subject),
             true <- value(parsed, "resource_id") == item.repository_resource_id do
          {:ok, parsed, baseline_subject_identity(parsed)}
        else
          _ -> :none
        end

      nil when is_binary(item.baseline_dependency_id) ->
        outcome =
          Repo.one(
            from(outcome in WorkOutcome,
              join: edge in WorkDependency,
              on:
                edge.goal_id == outcome.goal_id and
                  edge.work_item_id == ^item.id and
                  edge.depends_on_id == outcome.work_item_id,
              where:
                outcome.goal_id == ^goal.id and
                  outcome.goal_revision == ^goal.current_revision and
                  outcome.work_item_id == ^item.baseline_dependency_id and
                  outcome.disposition == "accepted",
              order_by: [desc: outcome.inserted_at, desc: outcome.id]
            )
          )

        with %WorkOutcome{candidate_subject: candidate_subject} <- outcome,
             {:ok, parsed} <- parse_subject(candidate_subject),
             true <- value(parsed, "resource_id") == item.repository_resource_id do
          {:ok, parsed,
           "baseline:dependency:" <> item.baseline_dependency_id <> ":" <> outcome.id}
        else
          _ -> :none
        end

      _ ->
        :none
    end
  end

  defp baseline_subject_identity(subject) do
    "baseline:subject:" <>
      (subject
       |> RequestHash.canonical()
       |> Base.encode16(case: :lower))
  end

  defp baseline_source_present?(item) do
    is_map(item.baseline_subject) or is_binary(item.baseline_dependency_id)
  end

  defp current_revision_task?(goal, work_item_id) do
    Repo.exists?(
      from(task in Task,
        where:
          task.goal_id == ^goal.id and task.goal_revision == ^goal.current_revision and
            task.work_item_id == ^work_item_id
      )
    )
  end

  defp automatic_action_allowed?(revision, purpose) do
    purpose in allowed_values(revision.execution_policy, "allowed_actions")
  end

  defp automatic_source_consumed?(goal, item, purpose, source_identity) do
    admission_key = automatic_admission_mutation_id(goal, item, purpose, source_identity)

    Repo.exists?(
      from(task in Task,
        where: task.goal_id == ^goal.id and task.admission_key == ^admission_key
      )
    )
  end

  defp automatic_candidate_blocked?(goal, item, purpose, source_identity) do
    candidate_identity = automatic_candidate_identity(item, purpose, source_identity)

    Repo.exists?(
      from(event in GoalEvent,
        where:
          event.goal_id == ^goal.id and event.revision == ^goal.current_revision and
            event.kind == "automatic_admission_blocked" and
            fragment("? ->> 'candidate_identity' = ?", event.payload, ^candidate_identity)
      )
    )
  end

  defp automatic_goal_blocked?(goal, reason) do
    candidate_identity = "goal:" <> reason

    Repo.exists?(
      from(event in GoalEvent,
        where:
          event.goal_id == ^goal.id and event.revision == ^goal.current_revision and
            event.kind == "automatic_admission_blocked" and
            fragment("? ->> 'candidate_identity' = ?", event.payload, ^candidate_identity)
      )
    )
  end

  defp automatic_candidate_identity(item, purpose, source_identity) do
    item.id <> ":" <> purpose <> ":" <> source_identity
  end

  defp automatic_admission_blocker_reason(reason)
       when reason in [
              :context_budget_exceeded,
              :required_context_source_missing,
              :model_not_allowed,
              :resource_not_allowed,
              :invalid_plan,
              :invalid_validation,
              :invalid_subject
            ],
       do: {:ok, Atom.to_string(reason)}

  defp automatic_admission_blocker_reason(_reason), do: :retry

  defp automatic_admission_disposition(reason) do
    case automatic_admission_blocker_reason(reason) do
      {:ok, blocker_reason} ->
        {:blocked, blocker_reason}

      :retry
      when reason in [
             :unsupported_capability,
             :strict_budget_requires_reservation,
             :validation_runtime_not_allowed
           ] ->
        {:deferred, Atom.to_string(reason)}

      :retry when reason in [:budget_exhausted, :budget_unknown] ->
        {:deferred, Atom.to_string(reason)}

      :retry ->
        case reason do
          {:invalid_validation_profile, _profile_reason} ->
            {:deferred, "invalid_validation_profile"}

          _ ->
            :retry
        end
    end
  end

  defp automatic_budget_status(goal, revision) do
    with {:ok, amount} <- server_reservation_amount(revision),
         :ok <- budget_status(goal, revision, amount) do
      :ok
    end
  end

  defp automatic_pending_work?(goal) do
    Enum.any?(current_work_items(goal), &(not accepted_work_item?(goal, &1.id)))
  end

  defp automatic_recoverable_pending_work?(goal) do
    Enum.any?(current_work_items(goal), fn item ->
      not accepted_work_item?(goal, item.id) and not automatic_fixed_blocked_item?(goal, item)
    end)
  end

  defp automatic_fixed_blocked_item?(goal, item) do
    missing_baseline? =
      not current_revision_task?(goal, item.id) and not baseline_source_present?(item)

    unsupported_external_wait? =
      Repo.exists?(
        from(wait in GoalExternalWait,
          where:
            wait.goal_id == ^goal.id and wait.goal_revision == ^goal.current_revision and
              wait.work_item_id == ^item.id and wait.state == "unsupported"
        )
      )

    missing_baseline? or unsupported_external_wait? or
      automatic_terminal_settlement_without_candidate?(goal, item)
  end

  defp automatic_terminal_settlement_without_candidate?(goal, item) do
    task =
      Repo.one(
        from(task in Task,
          where:
            task.goal_id == ^goal.id and task.goal_revision == ^goal.current_revision and
              task.work_item_id == ^item.id and
              task.state in ^["completed", "failed", "cancelled"],
          order_by: [desc: task.inserted_at, desc: task.id],
          limit: 1
        )
      )

    task && Enum.any?(@automatic_terminal_settlements, &settled_as?(goal, task, &1))
  end

  defp automatic_admission_capacity_status(goal, revision) do
    task_count = Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count)
    max_admissions = policy_integer(revision.execution_policy, "max_task_admissions")

    active_count =
      Repo.aggregate(
        from(task in Task,
          where: task.goal_id == ^goal.id and task.state in ^@nonterminal_task_states
        ),
        :count
      )

    max_parallel_tasks =
      policy_integer(revision.execution_policy, "max_parallel_tasks", @default_max_parallel_tasks)

    cond do
      task_count >= max_admissions ->
        if automatic_pending_work?(goal),
          do: {:blocked, "admission_limit_reached"},
          else: :quiescent

      active_count >= max_parallel_tasks ->
        {:deferred, "admission_capacity_unavailable"}

      true ->
        :ok
    end
  end

  defp automatic_budget_may_change?(goal) do
    Repo.exists?(
      from(task in Task,
        where: task.goal_id == ^goal.id and task.state in ^@nonterminal_task_states
      )
    )
  end

  defp automatic_deferred_reason(reason) when is_atom(reason), do: Atom.to_string(reason)

  defp defer_automatic_reconciliation!(goal, current, opts, reason, deferred_identity) do
    next_wake_at = DateTime.add(current, @automatic_reconciliation_backoff_seconds, :second)
    event_id = automatic_reconciliation_deferred_mutation_id(goal, deferred_identity, reason)

    {updated, event_sequence} =
      case Repo.get(GoalEvent, event_id) do
        nil ->
          event =
            append_event!(
              goal,
              event_id,
              "automatic_reconciliation_deferred",
              "system:wakeup",
              %{deferred_identity: deferred_identity, reason: reason},
              %{
                "goal_id" => goal.id,
                "goal_revision" => goal.current_revision,
                "disposition" => "deferred",
                "reason" => reason,
                "next_wake_at" => DateTime.to_iso8601(next_wake_at)
              },
              Keyword.merge(opts, now: current, settlement_next_wake_at: next_wake_at)
            )

          {Repo.get!(Goal, goal.id), event.sequence}

        event ->
          updated =
            goal
            |> Goal.transition_changeset(%{state: "active", next_wake_at: next_wake_at})
            |> stamp_update(current)
            |> Repo.update!()

          enqueue_wakeup!(updated, current)
          {updated, event.sequence}
      end

    %{
      goal_id: updated.id,
      revision: updated.current_revision,
      disposition: :deferred,
      reason: reason,
      next_wake_at: next_wake_at,
      event_sequence: event_sequence
    }
  end

  defp automatic_dependencies_satisfied?(goal, item) do
    dependency_ids =
      Repo.all(
        from(edge in WorkDependency,
          where: edge.goal_id == ^goal.id and edge.work_item_id == ^item.id,
          select: edge.depends_on_id
        )
      )

    accepted_count =
      Repo.aggregate(
        from(outcome in WorkOutcome,
          where:
            outcome.goal_id == ^goal.id and outcome.goal_revision == ^goal.current_revision and
              outcome.disposition == "accepted" and outcome.work_item_id in ^dependency_ids
        ),
        :count
      )

    accepted_count == length(dependency_ids)
  end

  defp current_work_items(goal) do
    Repo.all(
      from(item in WorkItem,
        where: item.goal_id == ^goal.id and item.admitted_revision == ^goal.current_revision,
        order_by: [asc: item.id]
      )
    )
  end

  defp accepted_work_item?(goal, work_item_id) do
    Repo.exists?(
      from(outcome in WorkOutcome,
        where:
          outcome.goal_id == ^goal.id and outcome.goal_revision == ^goal.current_revision and
            outcome.work_item_id == ^work_item_id and outcome.disposition == "accepted"
      )
    )
  end

  defp awaiting_validation_settlement?(goal, task),
    do: settled_as?(goal, task, "awaiting_validation")

  defp settled_as?(goal, task, settlement) do
    case Repo.one(
           from(run in Run,
             where: run.task_id == ^task.id and run.generation == ^task.current_generation
           )
         ) do
      %Run{} = run ->
        Repo.exists?(
          from(event in GoalEvent,
            where:
              event.goal_id == ^goal.id and event.revision == ^goal.current_revision and
                event.kind == "task_settled" and
                fragment("? ->> 'task_id' = CAST(? AS text)", event.payload, ^task.id) and
                fragment("? ->> 'run_id' = CAST(? AS text)", event.payload, ^run.id) and
                fragment(
                  "? ->> 'generation' = CAST(? AS text)",
                  event.payload,
                  type(^run.generation, :integer)
                ) and
                fragment("? ->> 'settlement' = ?", event.response, ^settlement)
          )
        )

      nil ->
        false
    end
  end

  @doc """
  Produces durable pause/cancel control descriptors from one Goal audit action.

  The GoalControlWorker owns dispatch after commit.  This function never calls
  Orchestration and never infers work from an in-memory scheduler.
  """
  @spec control_dispatch_plan(Ecto.UUID.t(), pos_integer(), Ecto.UUID.t(), keyword()) ::
          {:ok, :superseded | [map()]} | {:error, term()}
  def control_dispatch_plan(goal_id, revision, action_id, opts \\ [])

  def control_dispatch_plan(goal_id, revision, action_id, _opts)
      when is_binary(goal_id) and is_integer(revision) and revision > 0 and is_binary(action_id) do
    with :ok <- valid_uuid(goal_id), :ok <- valid_uuid(action_id) do
      Repo.transaction(fn ->
        goal = lock_goal(goal_id)

        event =
          Repo.one(
            from(event in GoalEvent,
              where: event.id == ^action_id and event.goal_id == ^goal.id,
              lock: "FOR UPDATE"
            )
          )

        valid_lifecycle? =
          event &&
            ((event.kind in ["pause", "amend"] and goal.state == "paused") or
               (event.kind == "cancel" and goal.state == "cancelled"))

        if is_nil(event) or event.revision != revision or goal.current_revision != revision or
             event.kind not in ["pause", "cancel", "amend"] or not valid_lifecycle? do
          :superseded
        else
          control_descriptors(goal, event.kind, action_id)
        end
      end)
      |> case do
        {:ok, :superseded} -> {:ok, :superseded}
        {:ok, descriptors} -> {:ok, descriptors}
        {:error, reason} -> {:error, reason}
      end
    end
  end

  def control_dispatch_plan(_, _, _, _), do: {:error, :invalid_request}

  @doc """
  Finds every historical Goal-authorized runless cancellation that still holds
  a reservation. This is deliberately independent of the triggering action:
  an Oban control job may be discarded after the cancellation commits, while a
  later Goal control job must still be able to repair the settlement gap.
  """
  @spec recover_pending_unstarted_cancellations(Ecto.UUID.t()) ::
          {:ok, [Ecto.UUID.t()]} | {:error, term()}
  def recover_pending_unstarted_cancellations(goal_id) when is_binary(goal_id) do
    if valid_uuid(goal_id) == :ok do
      Repo.transaction(fn ->
        goal = lock_goal(goal_id)

        task_ids =
          Repo.all(
            from(task in Task,
              where: task.goal_id == ^goal.id and task.state == "cancelled",
              order_by: [asc: task.id],
              lock: "FOR UPDATE",
              select: task.id
            )
          )

        Enum.filter(task_ids, &recoverable_historical_unstarted_cancellation?(goal, &1))
      end)
      |> case do
        {:ok, task_ids} -> {:ok, task_ids}
        {:error, reason} -> {:error, reason}
      end
    else
      {:error, :invalid_request}
    end
  end

  def recover_pending_unstarted_cancellations(_), do: {:error, :invalid_request}

  @doc false
  @spec recoverable_unstarted_cancellation_goal_ids(keyword()) ::
          {:ok, [Ecto.UUID.t()]} | {:error, term()}
  def recoverable_unstarted_cancellation_goal_ids(opts \\ [])

  def recoverable_unstarted_cancellation_goal_ids(opts) when is_list(opts) do
    requested_goal_ids = Keyword.get(opts, :goal_ids)
    limit = Keyword.get(opts, :limit, 100)

    if not (is_integer(limit) and limit > 0) or
         not (is_nil(requested_goal_ids) or
                (is_list(requested_goal_ids) and Enum.all?(requested_goal_ids, &valid_uuid?/1))) do
      {:error, :invalid_request}
    else
      recoverable =
        from(reservation in GoalBudgetReservation,
          join: task in Task,
          on: task.id == reservation.task_id,
          left_join: run in Run,
          on: run.task_id == task.id,
          where:
            reservation.state == "held" and not is_nil(task.goal_id) and task.state == "cancelled" and
              is_nil(run.id),
          order_by: [asc: task.goal_id],
          distinct: true,
          select: task.goal_id,
          limit: ^limit
        )

      recoverable =
        if is_list(requested_goal_ids),
          do:
            where(
              recoverable,
              [reservation, task],
              reservation.goal_id in ^requested_goal_ids and task.goal_id in ^requested_goal_ids
            ),
          else: recoverable

      {:ok, Repo.all(recoverable)}
    end
  rescue
    error in [Ecto.Query.CastError] -> {:error, error}
  end

  def recoverable_unstarted_cancellation_goal_ids(_), do: {:error, :invalid_request}

  @doc """
  Finds current terminal Goal executions whose settlement still needs evaluation.
  The periodic wakeup worker replays each descriptor through `settle_task/4`;
  a closed validation attempt awaiting an operator predicate is reevaluated
  against any subsequently resolved scoped review decision.
  """
  @spec recover_pending_terminal_settlements(keyword()) :: {:ok, [map()]} | {:error, term()}
  def recover_pending_terminal_settlements(opts \\ [])

  def recover_pending_terminal_settlements(opts) when is_list(opts) do
    current = now(opts)
    requested_goal_ids = Keyword.get(opts, :goal_ids)
    limit = Keyword.get(opts, :limit, 100)

    if not (is_integer(limit) and limit > 0) or
         not (is_nil(requested_goal_ids) or
                (is_list(requested_goal_ids) and Enum.all?(requested_goal_ids, &valid_uuid?/1))) do
      {:error, :invalid_request}
    else
      unsettled =
        from(reservation in GoalBudgetReservation,
          join: task in Task,
          as: :task,
          on: task.id == reservation.task_id,
          join: run in Run,
          as: :run,
          on: run.task_id == task.id and run.generation == task.current_generation,
          where:
            reservation.state in ["held", "settled", "unknown"] and not is_nil(task.goal_id) and
              task.state in ^@terminal_run_states and run.state in ^@terminal_run_states and
              fragment(
                "COALESCE(? ->> 'reason', '') <> 'lease_expired' OR (? + (? * interval '1 millisecond')) <= ?",
                run.failure,
                run.updated_at,
                ^@terminal_settlement_grace_ms,
                ^current
              ) and
              not exists(
                from(event in GoalEvent,
                  where:
                    event.goal_id == parent_as(:task).goal_id and event.kind == "task_settled" and
                      fragment(
                        "? ->> 'task_id' = CAST(? AS text)",
                        event.payload,
                        parent_as(:task).id
                      ) and
                      fragment(
                        "? ->> 'run_id' = CAST(? AS text)",
                        event.payload,
                        parent_as(:run).id
                      ) and
                      fragment(
                        "? ->> 'generation' = CAST(? AS text)",
                        event.payload,
                        parent_as(:run).generation
                      )
                )
              ),
          order_by: [asc: task.goal_id, asc: task.id, asc: run.id],
          select: %{task_id: task.id, run_id: run.id, generation: run.generation},
          limit: ^limit
        )

      unsettled =
        if is_list(requested_goal_ids),
          do:
            where(
              unsettled,
              [reservation, task],
              reservation.goal_id in ^requested_goal_ids and task.goal_id in ^requested_goal_ids
            ),
          else: unsettled

      awaiting_validation =
        from(task in Task,
          as: :task,
          join: goal in Goal,
          on: goal.id == task.goal_id,
          join: item in WorkItem,
          on: item.id == task.work_item_id,
          join: run in Run,
          as: :run,
          on: run.task_id == task.id and run.generation == task.current_generation,
          join: decision in GoalDecision,
          on:
            decision.goal_id == task.goal_id and decision.goal_revision == goal.current_revision and
              decision.kind == "review" and decision.state == "resolved" and
              decision.work_item_id == item.id,
          where:
            task.purpose == "validate" and task.goal_revision == goal.current_revision and
              task.state == "completed" and run.state == "completed" and
              fragment(
                "EXISTS (SELECT 1 FROM jsonb_array_elements(? -> 'predicates') AS predicate WHERE predicate ->> 'kind' = 'operator_acceptance')",
                item.acceptance_contract
              ) and
              fragment("? ->> 'option_id' IN ('accept', 'reject')", decision.resolution) and
              fragment(
                "encode(?, 'hex') = replace(? #>> '{task_result,subject_hash}', 'sha256:', '')",
                decision.subject_hash,
                run.result
              ) and
              exists(
                from(event in GoalEvent,
                  where:
                    event.goal_id == parent_as(:task).goal_id and event.kind == "task_settled" and
                      fragment(
                        "? ->> 'task_id' = CAST(? AS text)",
                        event.payload,
                        parent_as(:task).id
                      ) and
                      fragment(
                        "? ->> 'run_id' = CAST(? AS text)",
                        event.payload,
                        parent_as(:run).id
                      ) and
                      fragment(
                        "? ->> 'generation' = CAST(? AS text)",
                        event.payload,
                        parent_as(:run).generation
                      ) and
                      fragment("? ->> 'settlement' = 'awaiting_validation'", event.response)
                )
              ) and
              not exists(
                from(outcome in WorkOutcome,
                  where: outcome.validation_task_id == parent_as(:task).id
                )
              ),
          order_by: [asc: task.goal_id, asc: task.id, asc: run.id],
          select: %{task_id: task.id, run_id: run.id, generation: run.generation},
          limit: ^limit
        )

      awaiting_validation =
        if is_list(requested_goal_ids),
          do: where(awaiting_validation, [task: task], task.goal_id in ^requested_goal_ids),
          else: awaiting_validation

      pending =
        (Repo.all(unsettled) ++ Repo.all(awaiting_validation))
        |> Enum.uniq_by(&{&1.task_id, &1.run_id, &1.generation})
        |> Enum.sort_by(&{&1.task_id, &1.run_id})
        |> Enum.take(limit)

      {:ok, pending}
    end
  rescue
    error in [Ecto.Query.CastError] -> {:error, error}
  end

  def recover_pending_terminal_settlements(_), do: {:error, :invalid_request}

  @doc false
  @spec recover_pending_goal_control_actions(keyword()) :: {:ok, [map()]} | {:error, term()}
  def recover_pending_goal_control_actions(opts \\ [])

  def recover_pending_goal_control_actions(opts) when is_list(opts) do
    requested_goal_ids = Keyword.get(opts, :goal_ids)
    limit = Keyword.get(opts, :limit, 100)

    if not (is_integer(limit) and limit > 0) or
         not (is_nil(requested_goal_ids) or
                (is_list(requested_goal_ids) and Enum.all?(requested_goal_ids, &valid_uuid?/1))) do
      {:error, :invalid_request}
    else
      pending =
        from(goal in Goal,
          as: :goal,
          join: event in GoalEvent,
          as: :event,
          on:
            event.goal_id == goal.id and event.revision == goal.current_revision and
              event.kind in ["pause", "cancel", "amend"],
          join: task in Task,
          on: task.goal_id == goal.id and task.state in ^@cancellable_task_states,
          where:
            (event.kind == "cancel" and goal.state == "cancelled") or
              (event.kind == "amend" and goal.state == "paused"),
          where:
            not exists(
              from(newer in GoalEvent,
                where:
                  newer.goal_id == parent_as(:goal).id and
                    newer.revision == parent_as(:goal).current_revision and
                    newer.kind in ["pause", "cancel", "amend"] and
                    newer.sequence > parent_as(:event).sequence
              )
            ),
          order_by: [asc: goal.id],
          select: goal.id,
          distinct: true,
          limit: ^limit
        )

      pending =
        if is_list(requested_goal_ids),
          do: where(pending, [goal], goal.id in ^requested_goal_ids),
          else: pending

      pending
      |> Repo.all()
      |> Enum.reduce_while({:ok, []}, fn goal_id, {:ok, actions} ->
        case recover_pending_goal_control_action(goal_id) do
          {:ok, nil} -> {:cont, {:ok, actions}}
          {:ok, action} -> {:cont, {:ok, [action | actions]}}
          {:error, reason} -> {:halt, {:error, reason}}
        end
      end)
      |> case do
        {:ok, actions} -> {:ok, Enum.reverse(actions)}
        error -> error
      end
    end
  rescue
    error in [Ecto.Query.CastError] -> {:error, error}
  end

  def recover_pending_goal_control_actions(_), do: {:error, :invalid_request}

  defp recover_pending_goal_control_action(goal_id) do
    Repo.transaction(fn ->
      goal = lock_goal(goal_id)

      event =
        Repo.one(
          from(event in GoalEvent,
            where:
              event.goal_id == ^goal.id and event.revision == ^goal.current_revision and
                event.kind in ["pause", "cancel", "amend"],
            order_by: [desc: event.sequence],
            limit: 1,
            lock: "FOR UPDATE"
          )
        )

      valid? =
        event &&
          ((event.kind == "cancel" and goal.state == "cancelled") or
             (event.kind in ["pause", "amend"] and goal.state == "paused"))

      if valid? and control_descriptors(goal, event.kind, event.id) != [] do
        %{goal_id: goal.id, revision: event.revision, action_id: event.id}
      else
        nil
      end
    end)
  end

  defp control_descriptors(goal, "pause", action_id) do
    safe_pause_task_ids(goal.id)
    |> control_descriptors_for("pause", action_id)
  end

  defp control_descriptors(goal, "cancel", action_id) do
    cancellable_task_ids(goal.id)
    |> control_descriptors_for("cancel", action_id)
  end

  defp control_descriptors(goal, "amend", action_id) do
    pausable_ids = safe_pause_task_ids(goal.id)

    control_descriptors_for(pausable_ids, "pause", action_id) ++
      (goal.id
       |> cancellable_task_ids()
       |> Enum.reject(&(&1 in pausable_ids))
       |> control_descriptors_for("cancel", action_id))
  end

  # No native adapter has a verified retained pause/resume lifecycle yet.
  # Goal pause stops future admission; amendment and cancellation use cancel.
  defp safe_pause_task_ids(_goal_id), do: []

  defp ensure_amendment_controls_settled!(goal) do
    latest_lifecycle_action =
      Repo.one(
        from(event in GoalEvent,
          where:
            event.goal_id == ^goal.id and event.kind in ["pause", "resume", "cancel", "amend"],
          order_by: [desc: event.sequence],
          limit: 1
        )
      )

    if latest_lifecycle_action && latest_lifecycle_action.kind == "amend" do
      stale_work_remaining? =
        Repo.exists?(
          from(task in Task,
            where:
              task.goal_id == ^goal.id and task.goal_revision < ^goal.current_revision and
                task.state in ^@nonterminal_task_states
          )
        )

      stale_reservation_remaining? =
        Repo.exists?(
          from(reservation in GoalBudgetReservation,
            where:
              reservation.goal_id == ^goal.id and
                reservation.goal_revision < ^goal.current_revision and
                reservation.state == "held"
          )
        )

      if stale_work_remaining? or stale_reservation_remaining?, do: rollback(:state_conflict)
    end
  end

  defp cancellable_task_ids(goal_id) do
    Repo.all(
      from(task in Task,
        where: task.goal_id == ^goal_id and task.state in ^@cancellable_task_states,
        order_by: [asc: task.id],
        select: task.id
      )
    )
  end

  defp control_descriptors_for(task_ids, kind, action_id) do
    Enum.map(task_ids, fn task_id ->
      %{
        task_id: task_id,
        kind: kind,
        payload: %{},
        idempotency_key: "goal-control:#{action_id}:#{kind}:#{task_id}"
      }
    end)
  end

  defp recoverable_historical_unstarted_cancellation?(goal, task_id) do
    if Repo.exists?(from(run in Run, where: run.task_id == ^task_id)) do
      false
    else
      reservation =
        Repo.one(
          from(reservation in GoalBudgetReservation,
            where: reservation.goal_id == ^goal.id and reservation.task_id == ^task_id,
            lock: "FOR UPDATE"
          )
        )

      not is_nil(reservation) and reservation.state == "held" and
        Repo.all(
          from(command in Command,
            where:
              command.task_id == ^task_id and command.kind == "cancel" and
                command.state == "applied" and is_nil(command.run_id),
            select: command.idempotency_key
          )
        )
        |> Enum.any?(&historical_goal_cancel_command?(goal.id, task_id, &1))
    end
  end

  defp historical_goal_cancel_command?(goal_id, task_id, command_key)
       when is_binary(command_key) do
    case String.split(command_key, ":", parts: 4) do
      ["goal-control", action_id, "cancel", ^task_id] ->
        valid_uuid(action_id) == :ok and
          Repo.exists?(
            from(event in GoalEvent,
              where:
                event.id == ^action_id and event.goal_id == ^goal_id and
                  event.kind in ["cancel", "amend"]
            )
          )

      _ ->
        false
    end
  end

  defp historical_goal_cancel_command?(_goal_id, _task_id, _command_key), do: false

  defp reconcile_due_goal_rows(current, requested_goal_ids, opts) do
    limit = Keyword.get(opts, :limit, 100)

    if not (is_integer(limit) and limit > 0) do
      {:error, :invalid_request}
    else
      due =
        from(goal in Goal,
          where:
            goal.state == "active" and not is_nil(goal.next_wake_at) and
              goal.next_wake_at <= ^current,
          order_by: [asc: goal.next_wake_at, asc: goal.id],
          select: goal.id,
          limit: ^limit
        )

      due =
        if is_list(requested_goal_ids),
          do: where(due, [goal], goal.id in ^requested_goal_ids),
          else: due

      goal_ids = Repo.all(due)

      results = Enum.map(goal_ids, &reconcile_due_goal(&1, current, opts))

      case Enum.find(results, fn result -> match?({:error, _}, result) end) do
        {:error, reason} ->
          {:error, reason}

        nil ->
          reconciled = Enum.flat_map(results, fn {:ok, result} -> List.wrap(result) end)

          if Enum.any?(reconciled, &(&1.disposition == :admitted)), do: Scheduler.wake()
          {:ok, reconciled}
      end
    end
  end

  # Each admission candidate commits in its own Goal transaction. A durable
  # business blocker therefore survives a later candidate's retryable failure.
  defp reconcile_due_goal(goal_id, current, opts),
    do: reconcile_due_goal(goal_id, current, opts, true, [])

  defp reconcile_due_goal(goal_id, current, opts, require_due?, reconciled) do
    case reconcile_due_goal_once(goal_id, current, opts, require_due?) do
      {:ok, {:candidate, candidate}} ->
        reconcile_automatic_candidate(goal_id, current, opts, candidate, reconciled)

      {:ok, %{disposition: disposition} = reconciliation}
      when disposition in [:admitted, :blocked] ->
        reconcile_due_goal(goal_id, current, opts, false, [reconciliation | reconciled])

      {:ok, nil} ->
        {:ok, Enum.reverse(reconciled)}

      {:ok, %{disposition: disposition}}
      when reconciled != [] and disposition in [:noop, :deferred] ->
        {:ok, Enum.reverse(reconciled)}

      {:ok, reconciliation} ->
        {:ok, Enum.reverse([reconciliation | reconciled])}

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp reconcile_automatic_candidate(goal_id, current, opts, candidate, reconciled) do
    case attempt_automatic_candidate(goal_id, current, opts, candidate) do
      {:error, :already_accepted} ->
        reconcile_due_goal(goal_id, current, opts, false, reconciled)

      {:ok, nil} ->
        reconcile_due_goal(goal_id, current, opts, false, reconciled)

      {:ok, reconciliation} ->
        reconcile_due_goal(goal_id, current, opts, false, [reconciliation | reconciled])

      {:error, reason} ->
        case automatic_admission_disposition(reason) do
          {:blocked, blocker_reason} ->
            case record_automatic_admission_blocker(
                   goal_id,
                   current,
                   opts,
                   candidate,
                   blocker_reason
                 ) do
              {:ok, nil} ->
                reconcile_due_goal(goal_id, current, opts, false, reconciled)

              {:ok, reconciliation} ->
                reconcile_due_goal(goal_id, current, opts, false, [reconciliation | reconciled])

              {:error, error} ->
                {:error, error}
            end

          {:deferred, deferred_reason} ->
            case defer_automatic_candidate(goal_id, current, opts, candidate, deferred_reason) do
              {:ok, nil} ->
                reconcile_due_goal(goal_id, current, opts, false, reconciled)

              {:ok, reconciliation} ->
                {:ok, Enum.reverse([reconciliation | reconciled])}

              {:error, error} ->
                {:error, error}
            end

          :retry ->
            {:error, reason}
        end
    end
  end

  defp attempt_automatic_candidate(goal_id, current, opts, candidate) do
    Repo.transaction(fn ->
      with %Project{status: "active"} <- try_lock_active_goal_project(goal_id),
           %Goal{} = goal <- lock_goal(goal_id),
           true <- goal.state == "active" and goal.current_revision == candidate.revision,
           {:admit, item, purpose, model_profile, payload, source_identity} <-
             automatic_next_admission(goal, opts),
           true <-
             automatic_candidate_identity(item, purpose, source_identity) == candidate.identity do
        attempt_automatic_admission!(
          goal,
          current,
          opts,
          item,
          purpose,
          model_profile,
          payload,
          source_identity
        )
      else
        _ -> nil
      end
    end)
  end

  defp record_automatic_admission_blocker(goal_id, current, opts, candidate, blocker_reason) do
    Repo.transaction(fn ->
      with %Project{status: "active"} <- try_lock_active_goal_project(goal_id),
           %Goal{} = goal <- lock_goal(goal_id),
           true <- goal.state == "active" and goal.current_revision == candidate.revision,
           {:admit, item, purpose, _model_profile, _payload, source_identity} <-
             automatic_next_admission(goal, opts),
           true <-
             automatic_candidate_identity(item, purpose, source_identity) == candidate.identity do
        append_automatic_admission_blocker!(
          goal,
          current,
          opts,
          item,
          purpose,
          source_identity,
          blocker_reason
        )
      else
        _ -> nil
      end
    end)
  end

  defp append_automatic_admission_blocker!(
         goal,
         current,
         opts,
         item,
         purpose,
         source_identity,
         blocker_reason
       ) do
    candidate_identity = automatic_candidate_identity(item, purpose, source_identity)
    admission_key = automatic_admission_mutation_id(goal, item, purpose, source_identity)

    event =
      append_event!(
        goal,
        automatic_admission_blocked_mutation_id(goal, candidate_identity),
        "automatic_admission_blocked",
        "system:wakeup",
        %{
          candidate_identity: candidate_identity,
          work_item_id: item.id,
          purpose: purpose,
          source_identity: source_identity,
          admission_key: admission_key,
          reason: blocker_reason
        },
        %{
          "goal_id" => goal.id,
          "goal_revision" => goal.current_revision,
          "disposition" => "blocked",
          "work_item_id" => item.id,
          "purpose" => purpose,
          "candidate_identity" => candidate_identity,
          "reason" => blocker_reason
        },
        Keyword.merge(opts, now: current, settlement_next_wake_at: current)
      )

    %{
      goal_id: goal.id,
      revision: goal.current_revision,
      disposition: :blocked,
      work_item_id: item.id,
      purpose: purpose,
      reason: blocker_reason,
      event_sequence: event.sequence
    }
  end

  defp append_automatic_goal_blocker!(goal, current, opts, blocker_reason) do
    candidate_identity = "goal:" <> blocker_reason

    event =
      append_event!(
        goal,
        automatic_admission_blocked_mutation_id(goal, candidate_identity),
        "automatic_admission_blocked",
        "system:wakeup",
        %{candidate_identity: candidate_identity, reason: blocker_reason, scope: "goal"},
        %{
          "goal_id" => goal.id,
          "goal_revision" => goal.current_revision,
          "disposition" => "blocked",
          "candidate_identity" => candidate_identity,
          "reason" => blocker_reason
        },
        Keyword.merge(opts, now: current, settlement_next_wake_at: current)
      )

    %{
      goal_id: goal.id,
      revision: goal.current_revision,
      disposition: :blocked,
      reason: blocker_reason,
      event_sequence: event.sequence
    }
  end

  defp defer_automatic_candidate(goal_id, current, opts, candidate, deferred_reason) do
    Repo.transaction(fn ->
      with %Project{status: "active"} <- try_lock_active_goal_project(goal_id),
           %Goal{} = goal <- lock_goal(goal_id),
           true <- goal.state == "active" and goal.current_revision == candidate.revision,
           {:admit, item, purpose, _model_profile, _payload, source_identity} <-
             automatic_next_admission(goal, opts),
           true <-
             automatic_candidate_identity(item, purpose, source_identity) == candidate.identity do
        defer_automatic_reconciliation!(goal, current, opts, deferred_reason, candidate.identity)
      else
        _ -> nil
      end
    end)
  end

  defp reconcile_due_goal_once(goal_id, current, opts, require_due?) do
    Repo.transaction(fn ->
      with %Project{status: "active"} <- try_lock_active_goal_project(goal_id),
           %Goal{} = goal <- lock_goal(goal_id) do
        due? =
          goal.state == "active" and not is_nil(goal.next_wake_at) and
            goal.next_wake_at <= current

        if goal.state == "active" and (due? or not require_due?) do
          automatic? =
            value(
              current_revision!(goal).execution_policy || %{},
              "automatic_execution",
              false
            )

          if automatic? do
            reconcile_automatic_goal!(goal, current, opts)
          else
            updated =
              goal
              |> Goal.transition_changeset(%{state: "active", next_wake_at: nil})
              |> stamp_update(current)
              |> Repo.update!()

            %{
              goal_id: updated.id,
              revision: updated.current_revision,
              disposition: :noop
            }
          end
        else
          nil
        end
      else
        _ -> nil
      end
    end)
  end

  defp create_new_goal(project_id, title, initial_revision, mutation_id, actor_ref, body, opts) do
    current = now(opts)

    Repo.transaction(fn ->
      lock_active_project!(project_id)

      case Repo.one(from(event in GoalEvent, where: event.id == ^mutation_id, lock: "FOR UPDATE")) do
        nil ->
          goal_id = Ecto.UUID.generate()

          goal =
            %Goal{id: goal_id}
            |> Goal.changeset(%{
              project_id: project_id,
              title: title,
              state: "draft",
              current_revision: 1,
              event_sequence: 0
            })
            |> stamp_insert(current)
            |> Repo.insert!()

          revision_attrs = revision_attrs(goal.id, 1, initial_revision, actor_ref)

          case validate_contract(
                 :goal_revision,
                 goal_revision_wire(revision_attrs, current),
                 opts
               ) do
            :ok -> :ok
            {:error, reason} -> rollback({:invalid_contract, reason})
          end

          revision =
            %GoalRevision{}
            |> GoalRevision.changeset(revision_attrs)
            |> stamp_insert(current)
            |> Repo.insert!()

          response = %{
            "goal_id" => goal.id,
            "mutation_id" => mutation_id,
            "state" => "draft",
            "version" => goal.lock_version + 1,
            "current_revision" => revision.revision
          }

          event =
            append_event!(goal, mutation_id, "goal_created", actor_ref, body, response, opts)

          goal = Repo.get!(Goal, goal.id)
          {:created, receipt!(goal, event, response)}

        event ->
          case replay_event(event, body) do
            {:ok, receipt} -> {:replayed, receipt}
            {:error, reason} -> rollback(reason)
          end
      end
    end)
    |> case do
      {:ok, {:created, receipt}} ->
        {:ok, receipt, :created}

      {:ok, {:replayed, receipt}} ->
        {:ok, receipt, :replayed}

      {:error, :idempotency_conflict} ->
        case replay_create(mutation_id, body) do
          {:ok, receipt} -> {:ok, receipt, :replayed}
          _ -> {:error, :idempotency_conflict}
        end

      {:error, reason} ->
        {:error, reason}
    end
  end

  defp execute_command(goal_id, parsed, actor_ref, body, opts) do
    # Exact receipts stay readable after archival. New authority must serialize
    # with archival before taking the Goal lock.
    case replay_command(goal_id, parsed.mutation_id, body) do
      {:ok, receipt} ->
        {:replayed, receipt}

      :missing ->
        goal_id |> goal_project_id!() |> lock_active_project!()
        goal = lock_goal(goal_id)

        case replay_command(goal.id, parsed.mutation_id, body) do
          {:ok, receipt} ->
            {:replayed, receipt}

          :missing ->
            ensure_preconditions!(goal, parsed)

            {goal, response} = apply_command!(goal, parsed, actor_ref, opts)

            response =
              if parsed.kind in ["pause", "cancel", "amend"] do
                Map.put(response, "control_action_id", parsed.mutation_id)
              else
                response
              end

            event =
              append_event!(
                goal,
                parsed.mutation_id,
                parsed.kind,
                actor_ref,
                body,
                response,
                opts
              )

            if parsed.kind in ["pause", "cancel", "amend"], do: enqueue_goal_control!(goal, event)
            goal = Repo.get!(Goal, goal.id)
            {:created, receipt!(goal, event, response)}

          {:error, reason} ->
            rollback(reason)
        end

      {:error, reason} ->
        rollback(reason)
    end
  end

  defp apply_command!(goal, %{kind: "activate", payload: payload}, _actor_ref, _opts) do
    if goal.state != "draft" or value(payload, :approved_revision) != goal.current_revision,
      do: rollback(:invalid_transition)

    unless Repo.exists?(
             from(item in WorkItem,
               where:
                 item.goal_id == ^goal.id and item.admitted_revision == ^goal.current_revision
             )
           ),
           do: rollback(:admitted_work_required)

    transition_goal!(goal, "active")
  end

  defp apply_command!(goal, %{kind: "pause", payload: payload}, _actor_ref, _opts) do
    require_reason!(payload)
    if goal.state != "active", do: rollback(:invalid_transition)
    transition_goal!(goal, "paused")
  end

  defp apply_command!(goal, %{kind: "resume", payload: payload}, _actor_ref, _opts) do
    require_reason!(payload)

    if goal.state != "paused" or unresolved_decisions?(goal.id, goal.current_revision),
      do: rollback(:invalid_transition)

    ensure_amendment_controls_settled!(goal)
    ensure_resume_budget!(goal, current_revision!(goal))
    transition_goal!(goal, "active")
  end

  defp apply_command!(goal, %{kind: "cancel", payload: payload}, _actor_ref, _opts) do
    require_reason!(payload)
    if goal.state not in ["draft", "active", "paused"], do: rollback(:invalid_transition)
    transition_goal!(goal, "cancelled")
  end

  defp apply_command!(goal, %{kind: "amend", payload: payload}, actor_ref, opts) do
    if goal.state not in ["draft", "active", "paused"], do: rollback(:invalid_transition)

    revision_contract = required_map!(payload, :revision_contract)
    reason = required_string!(payload, :reason)

    case valid_initial_revision(revision_contract, opts) do
      :ok -> :ok
      {:error, reason} -> rollback(reason)
    end

    next_revision = goal.current_revision + 1

    next_revision_attrs =
      revision_attrs(
        goal.id,
        next_revision,
        Map.put(revision_contract, :reason, reason),
        actor_ref
      )

    case validate_contract(
           :goal_revision,
           goal_revision_wire(next_revision_attrs, now(opts)),
           opts
         ) do
      :ok -> :ok
      {:error, validation_error} -> rollback({:invalid_contract, validation_error})
    end

    %GoalRevision{}
    |> GoalRevision.changeset(next_revision_attrs)
    |> stamp_insert(now(opts))
    |> Repo.insert!()

    Repo.update_all(
      from(decision in GoalDecision,
        where: decision.goal_id == ^goal.id and decision.state == "open"
      ),
      set: [state: "superseded", updated_at: now(opts)],
      inc: [lock_version: 1]
    )

    updated =
      goal
      |> Changeset.change(current_revision: next_revision, state: "paused")
      |> Changeset.optimistic_lock(:lock_version)
      |> stamp_update(now(opts))
      |> Repo.update!()

    {updated,
     response_for(updated, %{
       amendment: %{revision: next_revision, state: "paused", superseded_open_decisions: true}
     })}
  end

  # Planning is an explicit, operator-authorized Goal task. It intentionally
  # has no WorkItem: the proposal remains non-authoritative until the operator
  # resolves its resulting plan Decision and separately accepts that plan.
  defp apply_command!(
         goal,
         %{kind: "request_plan", payload: payload, mutation_id: mutation_id},
         _actor_ref,
         opts
       ) do
    unless admission_enabled?(opts), do: rollback(:goal_admission_disabled)
    if goal.state != "draft", do: rollback(:invalid_transition)

    model_profile = required_string!(payload, :model_profile)
    repository_resource_id = required_uuid!(payload, :repository_resource_id)
    subject = required_map!(payload, :subject) |> parse_subject!()
    resource = lock_repository_resource!(repository_resource_id)

    unless resource.project_id == goal.project_id and
             resource_allowed?(current_revision!(goal).execution_policy, repository_resource_id) and
             subject["resource_id"] == repository_resource_id do
      rollback(:resource_not_allowed)
    end

    if Repo.exists?(
         from(item in WorkItem,
           where: item.goal_id == ^goal.id and item.admitted_revision == ^goal.current_revision
         )
       ) do
      rollback(:invalid_plan)
    end

    if pending_plan_task?(goal) or plan_decision_pending_acceptance?(goal),
      do: rollback(:plan_pending)

    task = admit_plan_task!(goal, resource, model_profile, subject, payload, mutation_id, opts)

    {goal,
     response_for(goal, %{
       task: %{
         id: task.id,
         work_item_id: nil,
         purpose: task.purpose,
         state: task.state
       }
     })}
  end

  defp apply_command!(goal, %{kind: "accept_plan", payload: payload}, _actor_ref, opts) do
    unless admission_enabled?(opts), do: rollback(:goal_admission_disabled)
    if goal.state not in ["draft", "active", "paused"], do: rollback(:invalid_transition)
    proposal = required_map!(payload, :proposal)
    proposal_hash = required_digest!(payload, :proposal_hash)
    decision_id = required_uuid!(payload, :decision_id)

    unless valid_plan_proposal?(proposal, goal, opts), do: rollback(:invalid_plan)
    if RequestHash.canonical(proposal) != proposal_hash, do: rollback(:invalid_request)
    decision = lock_decision(decision_id)

    unless decision.goal_id == goal.id and decision.goal_revision == goal.current_revision and
             decision.kind == "plan" and
             decision.state == "resolved" and
             is_nil(decision.work_item_id) and
             value(decision.resolution || %{}, "option_id") == "accept" and
             decision.action_hash == plan_action_hash(goal, proposal_hash) do
      rollback(:invalid_decision)
    end

    items = required_list!(proposal, :items)

    if items == [] or
         Repo.exists?(
           from(item in WorkItem,
             where: item.goal_id == ^goal.id and item.admitted_revision == ^goal.current_revision
           )
         ),
       do: rollback(:invalid_plan)

    inserted = admit_plan_items!(goal, items, opts)

    {goal,
     response_for(goal, %{
       plan: %{decision_id: decision.id, work_item_ids: Enum.map(inserted, & &1.id)}
     })}
  end

  defp apply_command!(goal, %{kind: "request_decision", payload: payload}, actor_ref, opts) do
    if goal.state in ["achieved", "cancelled"], do: rollback(:invalid_transition)
    kind = required_string!(payload, :kind)
    work_item_id = optional_uuid!(payload, :work_item_id)
    subject_hash = optional_digest!(payload, :subject_hash)

    {work_item_id, subject_hash, action_hash, question} =
      decision_request!(goal, kind, work_item_id, subject_hash, value(payload, :proposal), opts)

    options = derived_decision_options(kind)

    decision_changeset =
      %GoalDecision{id: Ecto.UUID.generate()}
      |> GoalDecision.changeset(%{
        goal_id: goal.id,
        goal_revision: goal.current_revision,
        work_item_id: work_item_id,
        kind: kind,
        action_hash: action_hash,
        state: "open",
        question: question,
        options: options,
        subject_hash: subject_hash,
        actor_ref: actor_ref,
        expires_at: nil
      })
      |> stamp_insert(now(opts))

    validate_decision_changeset!(decision_changeset, opts)

    decision =
      case Repo.insert(decision_changeset) do
        {:ok, decision} ->
          decision

        {:error, changeset} ->
          if Keyword.has_key?(changeset.errors, :action_hash),
            do: rollback(:idempotency_conflict),
            else: rollback(:invalid_request)
      end

    {goal,
     response_for(goal, %{
       decision: %{
         id: decision.id,
         state: decision.state,
         action_hash: context_digest(action_hash)
       }
     })}
  end

  defp apply_command!(goal, %{kind: "resolve_decision", payload: payload}, actor_ref, opts) do
    if goal.state in ["achieved", "cancelled"], do: rollback(:invalid_transition)
    decision_id = required_uuid!(payload, :decision_id)
    expected_version = required_safe_positive_integer!(payload, :expected_decision_version)
    option_id = required_string!(payload, :option_id)
    decision = lock_decision(decision_id)

    unless decision.goal_id == goal.id and decision.goal_revision == goal.current_revision and
             decision.state == "open" and
             decision.lock_version == expected_version and decision_option?(decision, option_id) do
      rollback(:invalid_decision)
    end

    if decision.expires_at && DateTime.compare(decision.expires_at, now(opts)) == :lt,
      do: rollback(:decision_expired)

    resolution = %{
      "option_id" => option_id,
      "comment" => optional_string_strict!(payload, :comment),
      "actor_ref" => actor_ref,
      "resolved_at" => DateTime.to_iso8601(now(opts))
    }

    decision_changeset =
      decision
      |> GoalDecision.resolve_changeset(%{state: "resolved", resolution: resolution})
      |> stamp_update(now(opts))

    validate_decision_changeset!(decision_changeset, opts)
    decision = Repo.update!(decision_changeset)

    {goal,
     response_for(goal, %{
       decision: %{id: decision.id, state: decision.state, version: decision.lock_version}
     })}
  end

  defp apply_command!(
         goal,
         %{kind: "admit_task", payload: payload, mutation_id: mutation_id},
         _actor_ref,
         opts
       ) do
    unless admission_enabled?(opts), do: rollback(:goal_admission_disabled)
    if goal.state != "active", do: rollback(:invalid_transition)
    work_item_id = required_uuid!(payload, :work_item_id)
    purpose = required_string!(payload, :purpose)
    model_profile = required_string!(payload, :model_profile)
    unless purpose in @task_purposes, do: rollback(:invalid_request)
    item = lock_work_item(work_item_id)
    ensure_admissible_item!(goal, item)
    ensure_unaccepted_item!(goal, item)

    server_payload =
      manual_admission_payload!(goal, item, purpose, payload, mutation_id, opts)

    task = admit_authorized_task!(goal, item, purpose, model_profile, server_payload, opts)

    {goal,
     response_for(goal, %{
       task: %{id: task.id, work_item_id: item.id, purpose: task.purpose, state: task.state}
     })}
  end

  defp apply_command!(goal, %{kind: kind, payload: payload}, _actor_ref, opts)
       when kind in ["add_dependency", "remove_dependency"] do
    if goal.state not in ["draft", "active", "paused"], do: rollback(:invalid_transition)
    work_item_id = required_uuid!(payload, :work_item_id)
    depends_on_id = required_uuid!(payload, :depends_on_id)
    decision_id = required_uuid!(payload, :decision_id)
    [item, dependency] = lock_work_items([work_item_id, depends_on_id])
    ensure_admissible_item!(goal, item)
    ensure_admissible_item!(goal, dependency)
    if active_task_for_item?(item.id), do: rollback(:active_task)
    ensure_resolved_scope_decision!(goal, item, decision_id, kind, dependency.id)

    case kind do
      "add_dependency" ->
        if creates_cycle?(goal.id, item.id, dependency.id), do: rollback(:dependency_cycle)

        %WorkDependency{}
        |> WorkDependency.changeset(%{
          goal_id: goal.id,
          work_item_id: item.id,
          depends_on_id: dependency.id
        })
        |> stamp_insert(now(opts))
        |> Repo.insert!()

      "remove_dependency" ->
        # A dependency-backed baseline is part of the immutable admitted work
        # contract. Reject it before the deferred database trigger turns the
        # operator command into an untyped commit error.
        if item.baseline_dependency_id == dependency.id, do: rollback(:invalid_plan)

        Repo.delete_all(
          from(edge in WorkDependency,
            where:
              edge.goal_id == ^goal.id and edge.work_item_id == ^item.id and
                edge.depends_on_id == ^dependency.id
          )
        )
    end

    {goal,
     response_for(goal, %{
       dependency: %{operation: kind, work_item_id: item.id, depends_on_id: dependency.id}
     })}
  end

  defp apply_command!(goal, %{kind: "achieve", payload: payload}, _actor_ref, opts) do
    if goal.state not in ["active", "paused"], do: rollback(:invalid_transition)
    ensure_achievable!(goal, payload)
    transition_goal!(goal, "achieved", opts)
  end

  defp apply_command!(_goal, _parsed, _actor_ref, _opts), do: rollback(:invalid_request)

  defp decision_request!(goal, "plan", nil, nil, proposal, opts) when is_map(proposal) do
    unless valid_plan_proposal?(proposal, goal, opts), do: rollback(:invalid_plan)

    {nil, nil, plan_action_hash(goal, RequestHash.canonical(proposal)),
     "Accept the proposed plan?"}
  end

  defp decision_request!(goal, "scope", work_item_id, nil, proposal, _opts)
       when is_binary(work_item_id) and is_map(proposal) do
    ensure_decision_work_item!(goal, work_item_id)
    operation = required_string!(proposal, :kind)
    dependency_id = required_uuid!(proposal, :depends_on_id)

    unless operation in ["add_dependency", "remove_dependency"], do: rollback(:invalid_request)

    {work_item_id, nil, dependency_action_hash(goal, operation, work_item_id, dependency_id),
     "Approve the requested dependency change?"}
  end

  defp decision_request!(goal, "review", work_item_id, subject_hash, nil, _opts)
       when is_binary(work_item_id) and is_binary(subject_hash) do
    ensure_decision_work_item!(goal, work_item_id)

    {work_item_id, subject_hash,
     work_item_acceptance_action_hash(goal, work_item_id, subject_hash),
     "Accept the validated work outcome?"}
  end

  defp decision_request!(goal, "completion", nil, subject_hash, nil, _opts)
       when is_binary(subject_hash) do
    {nil, subject_hash, completion_action_hash(goal, subject_hash), "Accept the Goal outcome?"}
  end

  defp decision_request!(_goal, _kind, _work_item_id, _subject_hash, _proposal, _opts),
    do: rollback(:invalid_request)

  defp derived_decision_options(_kind) do
    [
      %{"id" => "accept", "label" => "Accept", "consequence" => "Apply the approved action."},
      %{
        "id" => "reject",
        "label" => "Reject",
        "consequence" => "Keep the current approved state."
      }
    ]
  end

  defp transition_goal!(goal, state, opts \\ []) do
    updated =
      goal
      |> Goal.transition_changeset(%{state: state, next_wake_at: nil})
      |> stamp_update(now(opts))
      |> Repo.update!()

    {updated, response_for(updated, %{transition: state})}
  end

  defp admit_plan_items!(goal, items, opts) do
    unless plan_admission_preflight?(items, opts), do: rollback(:invalid_plan)

    keys = Enum.map(items, &required_string!(&1, :key))
    if length(keys) != MapSet.size(MapSet.new(keys)), do: rollback(:invalid_plan)
    item_ids = Map.new(keys, &{&1, Ecto.UUID.generate()})

    policy = current_revision!(goal).execution_policy || %{}
    resources = allowed_values(policy, "allowed_resource_ids")
    profiles = allowed_values(policy, "allowed_model_profiles")
    project = Repo.get(Project, goal.project_id) || rollback(:not_found)

    resources_by_id =
      items
      |> Enum.map(&required_uuid!(&1, :repository_resource_id))
      |> lock_repository_resources!()

    inserted =
      Enum.map(items, fn item ->
        item_id = Map.fetch!(item_ids, required_string!(item, :key))
        resource_id = required_uuid!(item, :repository_resource_id)
        profile = required_string!(item, :model_profile)
        acceptance = required_map!(item, :acceptance)
        resource = Map.fetch!(resources_by_id, resource_id)
        unless valid_acceptance_contract?(acceptance), do: rollback(:invalid_plan)

        unless acceptance_contract_matches_resource?(acceptance, resource_id),
          do: rollback(:resource_not_allowed)

        unless resource.project_id == goal.project_id, do: rollback(:resource_not_allowed)
        unless resources == [] or resource_id in resources, do: rollback(:resource_not_allowed)
        unless profiles == [] or profile in profiles, do: rollback(:model_not_allowed)

        baseline = plan_item_baseline!(item, item_ids)
        change_target = plan_item_change_target!(item, resource, current_revision!(goal))

        base =
          %WorkItem{id: item_id}
          |> WorkItem.changeset(%{
            project_id: goal.project_id,
            repository_resource_id: resource_id,
            title: required_string!(item, :title),
            description: optional_string(item, :description),
            status: "backlog",
            priority: "no_priority",
            position: 0,
            assignee_type: "agent",
            assignee_name: profile,
            agent_profile: profile,
            workspace: project.default_workspace,
            blocked: false
          })
          |> Changeset.apply_changes()

        base
        |> WorkItem.goal_membership_changeset(%{
          goal_id: goal.id,
          admitted_revision: goal.current_revision,
          required: value(item, :required, true),
          integration: plan_item_integration!(item),
          acceptance_contract: acceptance,
          baseline_subject: baseline.subject,
          baseline_dependency_id: baseline.dependency_id,
          change_target: change_target
        })
        |> stamp_insert(now(opts))
        |> Repo.insert!()
      end)

    key_to_id = item_ids

    Enum.each(items, fn item ->
      item_id = Map.fetch!(key_to_id, required_string!(item, :key))

      Enum.each(value(item, :depends_on_keys, []), fn dependency_key ->
        dependency_id = Map.get(key_to_id, dependency_key) || rollback(:invalid_plan)
        if creates_cycle?(goal.id, item_id, dependency_id), do: rollback(:dependency_cycle)

        %WorkDependency{}
        |> WorkDependency.changeset(%{
          goal_id: goal.id,
          work_item_id: item_id,
          depends_on_id: dependency_id
        })
        |> stamp_insert(now(opts))
        |> Repo.insert!()
      end)
    end)

    inserted
  end

  defp plan_item_baseline!(item, key_to_id) do
    case value(item, :baseline) do
      nil ->
        %{subject: nil, dependency_id: nil}

      baseline ->
        case value(baseline, :kind) do
          "subject" ->
            subject = parse_subject!(value(baseline, :subject))

            if subject["resource_id"] != required_uuid!(item, :repository_resource_id),
              do: rollback(:invalid_plan)

            %{subject: subject, dependency_id: nil}

          "dependency" ->
            item_key = required_string!(item, :key)
            dependency_key = required_string!(baseline, :key)
            dependencies = value(item, :depends_on_keys, [])

            if dependency_key == item_key or dependency_key not in dependencies,
              do: rollback(:invalid_plan)

            %{
              subject: nil,
              dependency_id: Map.get(key_to_id, dependency_key) || rollback(:invalid_plan)
            }

          _ ->
            rollback(:invalid_plan)
        end
    end
  end

  defp plan_item_change_target!(item, resource, revision) do
    case value(item, :change_target) do
      nil ->
        nil

      target when is_map(target) ->
        connection = provider_scope_connection!(resource, ["repositories", "changes"])

        case value(target, :kind) do
          "branches" ->
            branch_change_target!(target, revision)

          "pull_request" ->
            pull_request_change_target!(target, connection, resource, revision)

          _ ->
            rollback(:invalid_plan)
        end

      _ ->
        rollback(:invalid_plan)
    end
  end

  defp branch_change_target!(target, revision) do
    source_branch = required_untrimmed_string!(target, :source_branch)
    target_branch = required_untrimmed_string!(target, :target_branch)

    unless only_known_keys?(target, ["kind", "source_branch", "target_branch"]) and
             source_branch != target_branch and
             {:ok, source_branch} == ChangeAction.normalize_branch(source_branch) and
             {:ok, target_branch} == ChangeAction.normalize_branch(target_branch) and
             provider_operation_allowed?(revision, "change.upsert") do
      rollback(:invalid_plan)
    end

    %{
      "kind" => "branches",
      "source_branch" => source_branch,
      "target_branch" => target_branch
    }
  end

  defp pull_request_change_target!(target, connection, resource, revision) do
    pull_request_url = required_untrimmed_string!(target, :pull_request_url)

    unless only_known_keys?(target, ["kind", "pull_request_url"]) and
             valid_long_text?(pull_request_url) and
             provider_operation_allowed?(revision, "change.update") and
             valid_pull_request_change_target?(connection, resource, pull_request_url) do
      rollback(:invalid_plan)
    end

    %{"kind" => "pull_request", "pull_request_url" => pull_request_url}
  end

  defp valid_pull_request_change_target?(connection, resource, pull_request_url) do
    case resource.provider do
      "github" ->
        GitHub.validate_pull_request_url(connection, resource, pull_request_url) == :ok

      "azure_devops" ->
        AzureDevOps.validate_pull_request_url(connection, resource, pull_request_url) == :ok

      _ ->
        false
    end
  end

  defp admit_task!(goal, item, purpose, model_profile, payload, opts) do
    revision = current_revision!(goal)
    admission_key = optional_uuid!(payload, :admission_key) || Ecto.UUID.generate()

    task_count =
      Repo.aggregate(
        from(task in Task,
          where: task.goal_id == ^goal.id
        ),
        :count
      )

    max_admissions = policy_integer(revision.execution_policy, "max_task_admissions")
    if max_admissions && task_count >= max_admissions, do: rollback(:admission_limit_reached)

    active_count =
      Repo.aggregate(
        from(task in Task,
          where: task.goal_id == ^goal.id and task.state in ^@nonterminal_task_states
        ),
        :count
      )

    if active_count >=
         policy_integer(
           revision.execution_policy,
           "max_parallel_tasks",
           @default_max_parallel_tasks
         ),
       do: rollback(:parallel_limit_reached)

    if active_task_for_item?(item.id), do: rollback(:active_task)

    subject = admission_subject!(goal, item, purpose, payload, opts)
    requested_session_id = optional_uuid!(payload, :requested_session_id)
    session_mode = admission_session_mode!(payload, requested_session_id)
    ensure_requested_session_available!(item, requested_session_id, session_mode)
    limits = admission_limits!(payload, opts)

    {limits, reservation_amount, required_capabilities} =
      server_admission_budget!(goal, revision, limits)

    provider_scope = admission_provider_scope!(goal, revision, item, purpose)

    required_capabilities =
      required_capabilities
      |> require_provider_access(provider_scope)

    validation_bindings =
      admission_validation_bindings!(
        item.acceptance_contract,
        revision.execution_policy,
        purpose,
        opts
      )

    snapshot_id = Ecto.UUID.generate()

    snapshot_document =
      context_snapshot_document(
        goal,
        revision,
        item,
        purpose,
        model_profile,
        subject,
        validation_bindings,
        snapshot_id,
        admission_next_action(payload, purpose, model_profile),
        now(opts)
      )

    case validate_contract(:context_snapshot, snapshot_document, opts) do
      :ok -> :ok
      {:error, reason} -> rollback({:invalid_contract, reason})
    end

    content_hash = snapshot_document["content_hash"] |> parse_digest() |> unwrap!()

    snapshot =
      %ContextSnapshot{id: snapshot_id}
      |> ContextSnapshot.changeset(%{
        goal_id: goal.id,
        goal_revision: goal.current_revision,
        work_item_id: item.id,
        schema_version: 1,
        content_hash: content_hash,
        payload: snapshot_document
      })
      |> stamp_insert(now(opts))
      |> Repo.insert!()

    admission = %{
      "schema_version" => "symmetry.admission.v1",
      "admission_id" => admission_key,
      "goal_id" => goal.id,
      "goal_revision" => goal.current_revision,
      "work_item_id" => item.id,
      "purpose" => purpose,
      "context_snapshot_id" => snapshot.id,
      "context_hash" => snapshot_document["content_hash"],
      "model_profile" => model_profile,
      "session_mode" => session_mode,
      "requested_session_id" => requested_session_id,
      "subject" => subject,
      "limits" => limits,
      "validation_of_task_id" => optional_uuid!(payload, :validation_of_task_id),
      "provider_scope" => provider_scope
    }

    case validate_contract(:admission, admission, opts) do
      :ok -> :ok
      {:error, reason} -> rollback({:invalid_contract, reason})
    end

    {request_hash, request_hash_version} = RequestHash.write(admission)

    task =
      %Task{}
      |> Task.changeset(%{
        idempotency_key: "goal:#{goal.id}:#{admission_key}",
        request_hash: request_hash,
        request_hash_version: request_hash_version,
        goal: goal.title,
        agent_profile: model_profile,
        workspace: item.workspace,
        input: admission,
        required_capabilities: required_capabilities,
        state: "queued",
        current_generation: 0,
        attempt_generation: 1,
        work_item_id: item.id,
        goal_id: goal.id,
        goal_revision: goal.current_revision,
        context_snapshot_id: snapshot.id,
        purpose: purpose,
        validation_of_task_id: optional_uuid!(payload, :validation_of_task_id),
        admission_key: admission_key,
        max_run_attempts:
          policy_integer(
            revision.execution_policy,
            "max_run_attempts_per_task",
            @default_max_run_attempts
          ),
        requested_session_id: optional_uuid!(payload, :requested_session_id)
      })
      |> Changeset.force_change(:request_hash_version, request_hash_version)
      |> stamp_insert(now(opts))
      |> Repo.insert!()

    %GoalBudgetReservation{}
    |> GoalBudgetReservation.changeset(%{
      goal_id: goal.id,
      goal_revision: goal.current_revision,
      task_id: task.id,
      admission_key: admission_key,
      reserved_microusd: reservation_amount,
      state: "held"
    })
    |> stamp_insert(now(opts))
    |> Repo.insert!()

    item
    |> Changeset.change(orchestration_task_id: task.id)
    |> Changeset.optimistic_lock(:lock_version)
    |> stamp_update(now(opts))
    |> Repo.update!()

    task
  end

  defp admit_plan_task!(goal, resource, model_profile, subject, payload, admission_key, opts) do
    revision = current_revision!(goal)
    profiles = allowed_values(revision.execution_policy, "allowed_model_profiles")

    if profiles != [] and model_profile not in profiles, do: rollback(:model_not_allowed)

    task_count = Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count)
    max_admissions = policy_integer(revision.execution_policy, "max_task_admissions")
    if max_admissions && task_count >= max_admissions, do: rollback(:admission_limit_reached)

    active_count =
      Repo.aggregate(
        from(task in Task,
          where: task.goal_id == ^goal.id and task.state in ^@nonterminal_task_states
        ),
        :count
      )

    if active_count >=
         policy_integer(
           revision.execution_policy,
           "max_parallel_tasks",
           @default_max_parallel_tasks
         ),
       do: rollback(:parallel_limit_reached)

    requested_session_id = optional_uuid!(payload, :requested_session_id)
    session_mode = admission_session_mode!(payload, requested_session_id)
    ensure_plan_requested_session_available!(resource, requested_session_id, session_mode)

    limits = admission_limits!(%{}, opts)

    {limits, reservation_amount, required_capabilities} =
      server_admission_budget!(goal, revision, limits)

    snapshot_id = Ecto.UUID.generate()

    snapshot_document =
      planning_context_snapshot_document(
        goal,
        revision,
        resource,
        subject,
        snapshot_id,
        now(opts)
      )

    case validate_contract(:context_snapshot, snapshot_document, opts) do
      :ok -> :ok
      {:error, reason} -> rollback({:invalid_contract, reason})
    end

    content_hash = snapshot_document["content_hash"] |> parse_digest() |> unwrap!()

    snapshot =
      %ContextSnapshot{id: snapshot_id}
      |> ContextSnapshot.changeset(%{
        goal_id: goal.id,
        goal_revision: goal.current_revision,
        work_item_id: nil,
        schema_version: 1,
        content_hash: content_hash,
        payload: snapshot_document
      })
      |> stamp_insert(now(opts))
      |> Repo.insert!()

    admission = %{
      "schema_version" => "symmetry.admission.v1",
      "admission_id" => admission_key,
      "goal_id" => goal.id,
      "goal_revision" => goal.current_revision,
      "work_item_id" => nil,
      "purpose" => "plan",
      "context_snapshot_id" => snapshot.id,
      "context_hash" => snapshot_document["content_hash"],
      "model_profile" => model_profile,
      "session_mode" => session_mode,
      "requested_session_id" => requested_session_id,
      "subject" => subject,
      "limits" => limits,
      "validation_of_task_id" => nil,
      "provider_scope" => nil
    }

    case validate_contract(:admission, admission, opts) do
      :ok -> :ok
      {:error, reason} -> rollback({:invalid_contract, reason})
    end

    {request_hash, request_hash_version} = RequestHash.write(admission)

    project = Repo.get(Project, goal.project_id) || rollback(:not_found)

    task =
      %Task{}
      |> Task.changeset(%{
        idempotency_key: "goal:#{goal.id}:#{admission_key}",
        request_hash: request_hash,
        request_hash_version: request_hash_version,
        goal: goal.title,
        agent_profile: model_profile,
        workspace: project.default_workspace,
        input: admission,
        required_capabilities: required_capabilities,
        state: "queued",
        current_generation: 0,
        attempt_generation: 1,
        work_item_id: nil,
        goal_id: goal.id,
        goal_revision: goal.current_revision,
        context_snapshot_id: snapshot.id,
        purpose: "plan",
        validation_of_task_id: nil,
        admission_key: admission_key,
        max_run_attempts:
          policy_integer(
            revision.execution_policy,
            "max_run_attempts_per_task",
            @default_max_run_attempts
          ),
        requested_session_id: requested_session_id
      })
      |> Changeset.force_change(:request_hash_version, request_hash_version)
      |> stamp_insert(now(opts))
      |> Repo.insert!()

    %GoalBudgetReservation{}
    |> GoalBudgetReservation.changeset(%{
      goal_id: goal.id,
      goal_revision: goal.current_revision,
      task_id: task.id,
      admission_key: admission_key,
      reserved_microusd: reservation_amount,
      state: "held"
    })
    |> stamp_insert(now(opts))
    |> Repo.insert!()

    task
  end

  defp admit_authorized_task!(goal, item, purpose, model_profile, payload, opts) do
    ensure_admissible_item!(goal, item)
    ensure_unaccepted_item!(goal, item)
    ensure_dependencies_satisfied!(goal, item)
    ensure_task_policy!(goal, item, purpose, model_profile, payload)
    admit_task!(goal, item, purpose, model_profile, payload, opts)
  end

  defp ensure_unaccepted_item!(goal, item) do
    if accepted_work_item?(goal, item.id), do: rollback(:already_accepted)
  end

  defp manual_admission_payload!(goal, item, purpose, payload, mutation_id, opts) do
    reject_caller_derived_admission_fields!(payload)

    case purpose do
      "observe" ->
        manual_external_observation_payload!(goal, item, payload)

      "validate" ->
        Map.put(payload, :admission_key, mutation_id)

      _ ->
        payload
        |> Map.put(:admission_key, mutation_id)
        |> Map.put(:subject, manual_admission_subject!(goal, item, opts))
    end
  end

  defp manual_external_observation_payload!(goal, item, _payload) do
    case Repo.one(
           from(wait in GoalExternalWait,
             where:
               wait.goal_id == ^goal.id and wait.goal_revision == ^goal.current_revision and
                 wait.work_item_id == ^item.id and
                 wait.state in ^(GoalExternalWait.active_states() ++ ["unsupported"]),
             lock: "FOR UPDATE"
           )
         ) do
      nil -> rollback(:external_wait_required)
      %GoalExternalWait{} -> rollback(:unsupported_external_check)
    end
  end

  defp reject_caller_derived_admission_fields!(payload) do
    if Enum.any?(
         ["subject", "limits", "reserved_microusd", "admission_key"],
         &map_has_key?(payload, &1)
       ) do
      rollback(:invalid_request)
    end
  end

  defp manual_admission_subject!(goal, item, opts) do
    case automatic_baseline_subject(goal, item) do
      {:ok, subject, _source_identity} ->
        subject

      :none ->
        manual_predecessor_subject!(goal, item, opts)
    end
  end

  defp manual_predecessor_subject!(goal, item, opts) do
    Repo.all(
      from(task in Task,
        where:
          task.goal_id == ^goal.id and task.goal_revision == ^goal.current_revision and
            task.work_item_id == ^item.id and task.purpose == "implement" and
            task.state == "completed" and task.current_generation > 0,
        order_by: [desc: task.inserted_at, desc: task.id]
      )
    )
    |> Enum.find_value(fn task ->
      case automatic_progress_subject(goal, item, task, opts) do
        {:ok, subject, _source_identity} -> subject
        :none -> nil
      end
    end)
    |> case do
      nil -> rollback(:baseline_subject_required)
      subject -> subject
    end
  end

  defp ensure_achievable!(goal, payload) do
    revision = current_revision!(goal)
    final_acceptance = ensure_goal_final_acceptance!(revision)
    subject = required_map!(payload, :subject) |> parse_subject!()
    subject_hash = RequestHash.canonical(subject)
    evidence_ids = required_list!(payload, :evidence_ids)
    integration_work_item_id = required_uuid!(payload, :integration_work_item_id)

    current_items =
      Repo.all(
        from(item in WorkItem,
          where: item.goal_id == ^goal.id and item.admitted_revision == ^goal.current_revision,
          order_by: [asc: item.id],
          lock: "FOR UPDATE"
        )
      )

    integration_item =
      Enum.find(current_items, &(&1.id == integration_work_item_id)) ||
        rollback(:integration_outcome_required)

    unless integration_item.integration, do: rollback(:integration_outcome_required)

    required_items = Enum.filter(current_items, & &1.required)

    current_items
    |> completion_relevant_items(goal, required_items, integration_item)
    |> Enum.each(&ensure_dependencies_satisfied!(goal, &1))

    accepted_outcomes =
      Repo.all(
        from(outcome in WorkOutcome,
          where:
            outcome.goal_id == ^goal.id and outcome.goal_revision == ^goal.current_revision and
              outcome.disposition == "accepted",
          select: {outcome.work_item_id, outcome.subject_hash}
        )
      )
      |> Map.new()

    if Enum.any?(required_items, &(not Map.has_key?(accepted_outcomes, &1.id))),
      do: rollback(:incomplete_work)

    unless integration_outcome?(goal, integration_item.id, subject, subject_hash),
      do: rollback(:integration_outcome_required)

    if unresolved_decisions?(goal.id, goal.current_revision), do: rollback(:unresolved_decision)

    if Repo.exists?(
         from(task in Task,
           where: task.goal_id == ^goal.id and task.state in ^@nonterminal_task_states
         )
       ),
       do: rollback(:nonterminal_task)

    decision_id = optional_uuid!(payload, :decision_id)
    evidence = goal_evidence_for!(goal, evidence_ids)
    ensure_evidence_subject!(evidence, subject, subject_hash)
    ensure_final_acceptance_authority!(goal, final_acceptance, decision_id, subject_hash)

    ensure_required_evidence!(
      revision.acceptance_contract,
      evidence,
      subject_hash,
      decision_id,
      goal,
      nil
    )
  end

  defp ensure_task_policy!(goal, item, purpose, model_profile, payload) do
    revision = current_revision!(goal)
    profiles = allowed_values(revision.execution_policy, "allowed_model_profiles")
    if profiles != [] and model_profile not in profiles, do: rollback(:model_not_allowed)

    if purpose != "validate" and optional_uuid!(payload, :validation_of_task_id),
      do: rollback(:invalid_request)

    if purpose == "validate" do
      producer_id = optional_uuid!(payload, :validation_of_task_id) || rollback(:invalid_request)
      producer = lock_task(producer_id)

      unless producer.goal_id == goal.id and producer.work_item_id == item.id and
               producer.goal_revision == goal.current_revision and
               producer.state == "completed" and producer.purpose != "validate" do
        rollback(:invalid_validation)
      end
    end
  end

  defp admission_validation_bindings!(acceptance_contract, execution_policy, purpose, opts) do
    registry_opts =
      case Keyword.fetch(opts, :validation_profiles) do
        {:ok, profiles} -> [profiles: profiles]
        :error -> []
      end

    case ValidationProfiles.bindings_for_acceptance(acceptance_contract, registry_opts) do
      {:ok, bindings} ->
        policy_runtime_ids = allowed_values(execution_policy, "allowed_runtime_ids")

        bindings
        |> Enum.map(fn binding ->
          runtime_ids = binding["allowed_runtime_ids"]

          effective_runtime_ids =
            if policy_runtime_ids == [] do
              runtime_ids
            else
              Enum.filter(runtime_ids, &(&1 in policy_runtime_ids))
            end

          if effective_runtime_ids == [], do: rollback(:validation_runtime_not_allowed)
          %{binding | "allowed_runtime_ids" => effective_runtime_ids}
        end)
        |> constrain_validation_runtime_intersection!(purpose)

      {:error, reason} ->
        rollback({:invalid_validation_profile, reason})
    end
  end

  defp constrain_validation_runtime_intersection!(bindings, purpose) when purpose != "validate",
    do: bindings

  defp constrain_validation_runtime_intersection!([], "validate"), do: []

  defp constrain_validation_runtime_intersection!([first | rest] = bindings, "validate") do
    common_runtime_ids =
      Enum.filter(first["allowed_runtime_ids"], fn runtime_id ->
        Enum.all?(rest, &(runtime_id in &1["allowed_runtime_ids"]))
      end)

    if common_runtime_ids == [], do: rollback(:validation_runtime_not_allowed)

    Enum.map(bindings, &%{&1 | "allowed_runtime_ids" => common_runtime_ids})
  end

  defp ensure_admissible_item!(goal, item) do
    unless item.goal_id == goal.id and item.admitted_revision == goal.current_revision,
      do: rollback(:stale_revision)
  end

  defp ensure_dependencies_satisfied!(goal, item) do
    dependency_ids =
      Repo.all(
        from(edge in WorkDependency,
          where: edge.goal_id == ^goal.id and edge.work_item_id == ^item.id,
          select: edge.depends_on_id
        )
      )

    accepted_ids =
      Repo.all(
        from(outcome in WorkOutcome,
          where:
            outcome.goal_id == ^goal.id and outcome.goal_revision == ^goal.current_revision and
              outcome.disposition == "accepted" and outcome.work_item_id in ^dependency_ids,
          select: outcome.work_item_id
        )
      )
      |> MapSet.new()

    unless Enum.all?(dependency_ids, &MapSet.member?(accepted_ids, &1)),
      do: rollback(:waiting_dependency)
  end

  defp completion_relevant_items(current_items, goal, required_items, integration_item) do
    item_by_id = Map.new(current_items, &{&1.id, &1})
    item_ids = Map.keys(item_by_id)

    dependency_ids_by_item =
      Repo.all(
        from(edge in WorkDependency,
          where: edge.goal_id == ^goal.id and edge.work_item_id in ^item_ids,
          select: {edge.work_item_id, edge.depends_on_id}
        )
      )
      |> Enum.group_by(&elem(&1, 0), &elem(&1, 1))

    root_ids = MapSet.new(Enum.map(required_items, & &1.id) ++ [integration_item.id])
    relevant_ids = dependency_closure(root_ids, dependency_ids_by_item)

    Enum.filter(current_items, &MapSet.member?(relevant_ids, &1.id))
  end

  defp dependency_closure(ids, dependency_ids_by_item) do
    next_ids =
      ids
      |> Enum.flat_map(&Map.get(dependency_ids_by_item, &1, []))
      |> MapSet.new()
      |> MapSet.difference(ids)

    if MapSet.size(next_ids) == 0 do
      ids
    else
      dependency_closure(MapSet.union(ids, next_ids), dependency_ids_by_item)
    end
  end

  defp ensure_budget!(goal, revision, reserved_microusd) do
    case budget_status(goal, revision, reserved_microusd) do
      :ok -> :ok
      {:error, reason} -> rollback(reason)
    end
  end

  defp server_reservation_amount(revision) do
    case value(revision.execution_policy || %{}, "budget_mode", "soft") do
      "soft" ->
        {:ok, nil}

      "strict" ->
        ceiling =
          policy_nonnegative_integer(revision.execution_policy, "per_run_cost_limit_microusd")

        if is_nil(ceiling) or
             value(revision.execution_policy, "hard_cost_limit_required", false) != true do
          {:error, :strict_budget_requires_reservation}
        else
          {:ok,
           multiply_microusd!(
             ceiling,
             policy_integer(
               revision.execution_policy,
               "max_run_attempts_per_task",
               @default_max_run_attempts
             )
           )}
        end

      _ ->
        {:error, :invalid_request}
    end
  end

  defp budget_status(goal, revision, reserved_microusd) do
    limit = policy_nonnegative_integer(revision.execution_policy, "budget_limit_microusd")
    mode = value(revision.execution_policy || %{}, "budget_mode", "soft")

    reservations =
      Repo.all(
        from(reservation in GoalBudgetReservation,
          where: reservation.goal_id == ^goal.id,
          lock: "FOR UPDATE"
        )
      )

    reservations_by_task = Map.new(reservations, &{&1.task_id, &1})

    accounting =
      Repo.all(from(task in Task, where: task.goal_id == ^goal.id))
      |> Enum.map(fn task -> task_accounting(task, Map.get(reservations_by_task, task.id)) end)

    unknown? = Enum.any?(accounting, & &1.unknown?)

    cond do
      mode == "strict" and unknown? ->
        {:error, :budget_unknown}

      mode == "strict" and is_nil(reserved_microusd) ->
        {:error, :strict_budget_requires_reservation}

      limit ->
        committed =
          accounting
          |> Enum.reduce(0, fn task, total ->
            total + task.known_cost_microusd + task.remaining_liability_microusd
          end)

        if committed + (reserved_microusd || 0) > limit,
          do: {:error, :budget_exhausted},
          else: :ok

      true ->
        :ok
    end
  end

  defp ensure_resume_budget!(goal, revision) do
    if value(revision.execution_policy || %{}, "budget_mode", "soft") == "strict" do
      ensure_budget!(goal, revision, 0)
    end
  end

  defp task_accounting(task, reservation) do
    runs =
      Repo.all(
        from(run in Run,
          where: run.task_id == ^task.id,
          select: %{id: run.id, state: run.state}
        )
      )

    effective_usage_by_run =
      Repo.all(
        from(usage in RunUsage,
          join: run in Run,
          on: run.id == usage.run_id,
          where: run.task_id == ^task.id,
          select: {run.id, usage.id, usage.supersedes_id, usage.cost_microusd, usage.cost_basis}
        )
      )
      |> Enum.group_by(&elem(&1, 0), fn {_run_id, id, supersedes_id, cost, basis} ->
        {id, supersedes_id, cost, basis}
      end)
      |> Map.new(fn {run_id, rows} -> {run_id, latest_usage_rows(rows)} end)

    leaves = Map.values(effective_usage_by_run) |> List.flatten()

    known_cost_microusd =
      leaves
      |> Enum.map(&elem(&1, 2))
      |> Enum.reject(&is_nil/1)
      |> Enum.sum()

    unknown_leaf? =
      Enum.any?(leaves, fn {_id, _supersedes_id, cost, basis} ->
        is_nil(cost) or basis == "unknown"
      end)

    estimated_leaf? =
      Enum.any?(leaves, fn {_id, _supersedes_id, _cost, basis} -> basis == "estimated" end)

    missing_terminal_usage? =
      Enum.any?(runs, fn run ->
        run.state in @accounting_terminal_run_states and
          Map.get(effective_usage_by_run, run.id, []) == []
      end)

    missing_task_usage? =
      runs != [] and Enum.any?(runs, &(Map.get(effective_usage_by_run, &1.id, []) == []))

    terminal? = task.state in @terminal_run_states

    reservation_state =
      cond do
        task.state in @nonterminal_task_states ->
          "held"

        terminal? and runs == [] ->
          "released"

        terminal? and (unknown_leaf? or estimated_leaf? or missing_task_usage?) ->
          "unknown"

        terminal? ->
          "settled"

        true ->
          "unknown"
      end

    unknown? =
      unknown_leaf? or
        estimated_leaf? or
        missing_terminal_usage? or
        (terminal? and missing_task_usage?) or
        (not terminal? and (is_nil(reservation) or is_nil(reservation.reserved_microusd)))

    remaining_liability_microusd =
      if reservation && reservation_state in ["held", "unknown"] do
        max((reservation.reserved_microusd || 0) - known_cost_microusd, 0)
      else
        0
      end

    %{
      known_cost_microusd: known_cost_microusd,
      remaining_liability_microusd: remaining_liability_microusd,
      reservation_state: reservation_state,
      unknown?: unknown?
    }
  end

  defp latest_usage_rows(rows) do
    usage_ids = MapSet.new(rows, &elem(&1, 0))

    superseded_ids =
      rows
      |> Enum.flat_map(fn {_id, supersedes_id, _cost, _basis} ->
        if is_nil(supersedes_id) or not MapSet.member?(usage_ids, supersedes_id),
          do: [],
          else: [supersedes_id]
      end)
      |> MapSet.new()

    Enum.reject(rows, &MapSet.member?(superseded_ids, elem(&1, 0)))
  end

  defp candidate_subject_from_result!(run, opts, error) do
    task_result = value(run.result || %{}, "task_result")

    unless is_map(task_result), do: rollback(error)

    case validate_contract(:task_result, task_result, opts) do
      :ok -> :ok
      {:error, _reason} -> rollback(error)
    end

    subject = parse_subject!(value(task_result, "subject"))
    subject_hash = RequestHash.canonical(subject)
    result_id = required_uuid!(task_result, :result_id)

    unless value(task_result, "kind") == "candidate_completion" and
             value(task_result, "subject_hash") == context_digest(subject_hash),
           do: rollback(error)

    {subject, subject_hash, result_id}
  end

  # Validation outcomes are evidence-driven rather than model-declared. The
  # validator terminal result closes the evidence set; frozen input and
  # persisted evidence identify the exact producer candidate being evaluated.
  defp derive_validation_outcome!(
         goal,
         item,
         task,
         run,
         producer,
         producing_run,
         "candidate_completion",
         terminal_subject,
         terminal_subject_hash,
         opts
       ) do
    with true <- task.goal_revision == goal.current_revision,
         true <- goal.state in ["active", "paused"],
         true <-
           task.state == "completed" and task.current_generation == run.generation and
             run.task_id == task.id,
         true <- eligible_validation_outcome?(task, goal, item, producer, producing_run),
         {:ok, {subject, subject_hash, producing_result_id}} <-
           candidate_subject_from_terminal_result(producing_run, opts),
         true <- terminal_subject == subject,
         true <- terminal_subject_hash == subject_hash,
         true <- validation_subject_matches_candidate?(goal, item, task, subject, subject_hash),
         outcome <-
           validation_evidence_outcome(
             goal,
             item,
             item.acceptance_contract,
             run.id,
             subject,
             subject_hash
           ) do
      existing_validation = existing_validation_outcome(task.id)
      existing_accepted = existing_accepted_work_outcome(goal, item)

      case existing_validation do
        %WorkOutcome{} = existing ->
          existing

        nil ->
          cond do
            is_struct(existing_accepted, WorkOutcome) and
                outcome_matches_candidate?(
                  existing_accepted,
                  subject,
                  subject_hash,
                  producer,
                  producing_run,
                  producing_result_id
                ) ->
              existing_accepted

            is_struct(existing_accepted, WorkOutcome) ->
              insert_validation_outcome!(
                goal,
                item,
                producer,
                producing_run,
                task,
                subject,
                subject_hash,
                producing_result_id,
                [],
                "rejected",
                "historical_result",
                nil,
                opts
              )

            true ->
              case outcome do
                {:accepted, evidence_ids, decision_id} ->
                  insert_validation_outcome!(
                    goal,
                    item,
                    producer,
                    producing_run,
                    task,
                    subject,
                    subject_hash,
                    producing_result_id,
                    evidence_ids,
                    "accepted",
                    "validation_passed",
                    decision_id,
                    opts
                  )

                {:rejected, evidence_ids, decision_id} ->
                  insert_validation_outcome!(
                    goal,
                    item,
                    producer,
                    producing_run,
                    task,
                    subject,
                    subject_hash,
                    producing_result_id,
                    evidence_ids,
                    "rejected",
                    "validation_failed",
                    decision_id,
                    opts
                  )

                :awaiting_validation ->
                  nil
              end
          end
      end
    else
      _ -> nil
    end
  end

  defp derive_validation_outcome!(
         _goal,
         _item,
         _task,
         _run,
         _producer,
         _producing_run,
         _kind,
         _terminal_subject,
         _terminal_subject_hash,
         _opts
       ),
       do: nil

  defp eligible_validation_outcome?(task, goal, item, %Task{} = producer, %Run{} = producing_run) do
    task.purpose == "validate" and task.validation_of_task_id == producer.id and
      producer.id != task.id and producer.goal_id == goal.id and producer.work_item_id == item.id and
      producer.goal_revision == goal.current_revision and producer.purpose != "validate" and
      producer.state == "completed" and producing_run.task_id == producer.id and
      producing_run.generation == producer.current_generation and
      producing_run.state == "completed"
  end

  defp eligible_validation_outcome?(_task, _goal, _item, _producer, _producing_run), do: false

  defp candidate_subject_from_terminal_result(run, opts) do
    task_result = value(run.result || %{}, "task_result")

    with true <- is_map(task_result),
         :ok <- validate_contract(:task_result, task_result, opts),
         {:ok, subject} <- parse_subject(value(task_result, "subject")),
         {:ok, result_id} <- required_uuid(task_result, :result_id),
         subject_hash = RequestHash.canonical(subject),
         true <- value(task_result, "kind") == "candidate_completion",
         true <- value(task_result, "subject_hash") == context_digest(subject_hash) do
      {:ok, {subject, subject_hash, result_id}}
    else
      _ -> :invalid
    end
  end

  defp validation_subject_matches_candidate?(goal, item, task, subject, subject_hash) do
    with %ContextSnapshot{} = snapshot <-
           Repo.one(
             from(snapshot in ContextSnapshot,
               where: snapshot.id == ^task.context_snapshot_id,
               lock: "FOR UPDATE"
             )
           ),
         true <- snapshot.goal_revision == task.goal_revision and snapshot.work_item_id == item.id,
         {:ok, context} <- context_wire_envelope(snapshot, goal, item, task),
         {:ok, validation_subject} <- parse_subject(value(context, "subject")) do
      validation_subject == subject and RequestHash.canonical(validation_subject) == subject_hash
    else
      _ -> false
    end
  end

  defp validation_evidence_outcome(goal, item, contract, validation_run_id, subject, subject_hash) do
    predicates = value(contract || %{}, "predicates", [])

    required_predicates =
      if is_list(predicates) do
        predicates
      else
        []
      end

    operator_acceptance_decision =
      if Enum.any?(required_predicates, &(value(&1, "kind") == "operator_acceptance")),
        do: resolved_work_item_acceptance_decision(goal, item, subject_hash),
        else: nil

    operator_acceptance_decision_id =
      if operator_acceptance_decision, do: operator_acceptance_decision.id

    evidence =
      Repo.all(
        from(evidence in RunEvidence,
          where: evidence.run_id == ^validation_run_id and evidence.subject_hash == ^subject_hash,
          lock: "FOR UPDATE"
        )
      )
      |> Enum.filter(fn evidence ->
        value(evidence.payload, "subject") == subject and
          value(evidence.payload, "subject_hash") == context_digest(subject_hash)
      end)

    matching_evidence =
      Enum.filter(evidence, fn evidence ->
        Enum.any?(required_predicates, fn predicate ->
          is_map(predicate) and is_binary(value(predicate, "id")) and
            value(predicate, "kind") != "operator_acceptance" and
            value(evidence.payload, "predicate_id") == value(predicate, "id") and
            goal_predicate_matches_evidence?(predicate, evidence)
        end)
      end)

    failed_ids =
      matching_evidence
      |> Enum.filter(&(&1.verdict == "failed"))
      |> Enum.map(& &1.id)
      |> Enum.sort()

    all_required_passed? =
      required_predicates != [] and
        Enum.all?(required_predicates, fn predicate ->
          is_map(predicate) and is_binary(value(predicate, "id")) and
            case value(predicate, "kind") do
              "operator_acceptance" ->
                not is_nil(operator_acceptance_decision_id)

              _ ->
                Enum.any?(matching_evidence, fn evidence ->
                  evidence.verdict == "passed" and
                    value(evidence.payload, "predicate_id") == value(predicate, "id")
                end)
            end
        end)

    cond do
      failed_ids != [] ->
        {:rejected, failed_ids, operator_acceptance_decision_id}

      operator_acceptance_decision && operator_acceptance_decision.option_id == "reject" ->
        {:rejected, [], operator_acceptance_decision_id}

      all_required_passed? ->
        {:accepted,
         matching_evidence
         |> Enum.filter(&(&1.verdict == "passed"))
         |> Enum.map(& &1.id)
         |> Enum.sort(), operator_acceptance_decision_id}

      true ->
        :awaiting_validation
    end
  end

  defp existing_validation_outcome(validation_task_id) do
    Repo.one(
      from(outcome in WorkOutcome,
        where: outcome.validation_task_id == ^validation_task_id,
        lock: "FOR UPDATE"
      )
    )
  end

  defp existing_accepted_work_outcome(goal, item) do
    Repo.one(
      from(outcome in WorkOutcome,
        where:
          outcome.goal_id == ^goal.id and outcome.work_item_id == ^item.id and
            outcome.goal_revision == ^goal.current_revision and outcome.disposition == "accepted",
        lock: "FOR UPDATE"
      )
    )
  end

  defp outcome_matches_candidate?(
         %WorkOutcome{} = outcome,
         subject,
         subject_hash,
         %Task{} = producer,
         %Run{} = producing_run,
         producing_result_id
       ) do
    outcome.candidate_subject == subject and outcome.subject_hash == subject_hash and
      outcome.producing_task_id == producer.id and outcome.producing_run_id == producing_run.id and
      outcome.producing_result_id == producing_result_id
  end

  defp resolved_work_item_acceptance_decision(goal, item, subject_hash) do
    action_hash = work_item_acceptance_action_hash(goal, item.id, subject_hash)

    Repo.one(
      from(decision in GoalDecision,
        where:
          decision.goal_id == ^goal.id and decision.goal_revision == ^goal.current_revision and
            decision.state == "resolved" and decision.kind == "review" and
            decision.work_item_id == ^item.id and decision.subject_hash == ^subject_hash and
            decision.action_hash == ^action_hash,
        select: %{id: decision.id, resolution: decision.resolution}
      )
    )
    |> case do
      %{id: id, resolution: resolution} when is_map(resolution) ->
        case value(resolution, "option_id") do
          option_id when option_id in ["accept", "reject"] ->
            %{id: id, option_id: option_id}

          _ ->
            nil
        end

      _ ->
        nil
    end
  end

  defp rederive_awaiting_validation_outcome!(
         goal,
         item,
         task,
         run,
         producer,
         producing_run,
         receipt,
         opts
       ) do
    if value(receipt, "settlement") == "awaiting_validation" do
      terminal_settlement!(goal, item, task, run, producer, producing_run, opts)
      outcome = existing_validation_outcome(task.id)

      if outcome do
        late_validation_outcome_receipt!(goal, task, run, outcome, opts)
      end
    end
  end

  defp late_validation_outcome_receipt!(goal, task, run, outcome, opts) do
    mutation_id = late_validation_outcome_mutation_id(task.id, run.id, run.generation)

    settlement =
      if outcome.reason == "historical_result", do: "historical_result", else: outcome.disposition

    body = %{
      task_id: task.id,
      run_id: run.id,
      generation: run.generation,
      outcome_id: outcome.id,
      disposition: outcome.disposition,
      goal_revision: task.goal_revision
    }

    case replay_command(goal.id, mutation_id, body) do
      {:ok, receipt} ->
        Map.put(receipt.response, "event_sequence", receipt.event.sequence)

      :missing ->
        wake_at = if(settlement == "accepted", do: :immediate, else: nil)
        next_wake_at = terminal_next_wake_at(goal, task, %{wake_at: wake_at}, opts)

        response = %{
          "goal_id" => goal.id,
          "goal_revision" => task.goal_revision,
          "mutation_id" => mutation_id,
          "task_id" => task.id,
          "run_id" => run.id,
          "generation" => run.generation,
          "settlement" => settlement,
          "reason" => outcome.reason,
          "outcome_id" => outcome.id,
          "next_wake_at" => nullable_datetime(next_wake_at)
        }

        event =
          append_event!(
            goal,
            mutation_id,
            "validation_outcome_settled",
            "system:settlement",
            body,
            response,
            opts
            |> Keyword.put(:event_revision, task.goal_revision)
            |> Keyword.put(:settlement_next_wake_at, next_wake_at)
          )

        Map.put(response, "event_sequence", event.sequence)

      {:error, reason} ->
        rollback(reason)
    end
  end

  defp insert_validation_outcome!(
         goal,
         item,
         producer,
         producing_run,
         validation_task,
         subject,
         subject_hash,
         producing_result_id,
         evidence_ids,
         disposition,
         reason,
         decision_id,
         opts
       ) do
    %WorkOutcome{}
    |> WorkOutcome.changeset(%{
      goal_id: goal.id,
      goal_revision: goal.current_revision,
      work_item_id: item.id,
      producing_task_id: producer.id,
      producing_run_id: producing_run.id,
      validation_task_id: validation_task.id,
      candidate_subject: subject,
      subject_hash: subject_hash,
      producing_result_id: producing_result_id,
      evidence_ids: evidence_ids,
      disposition: disposition,
      reason: reason,
      decision_id: decision_id
    })
    |> stamp_insert(now(opts))
    |> Repo.insert!()
  end

  defp ensure_subject_resource!(subject, item, error) do
    unless value(subject, "resource_id") == item.repository_resource_id, do: rollback(error)
  end

  defp integration_outcome?(goal, work_item_id, subject, subject_hash) do
    Repo.exists?(
      from(outcome in WorkOutcome,
        where:
          outcome.goal_id == ^goal.id and outcome.goal_revision == ^goal.current_revision and
            outcome.work_item_id == ^work_item_id and outcome.disposition == "accepted" and
            outcome.subject_hash == ^subject_hash and outcome.candidate_subject == ^subject,
        lock: "FOR UPDATE"
      )
    )
  end

  defp plan_item_integration!(item) do
    case value(item, :integration, false) do
      value when is_boolean(value) -> value
      _ -> rollback(:invalid_plan)
    end
  end

  defp ensure_evidence_subject!(evidence, subject, subject_hash) do
    expected_hash = context_digest(subject_hash)

    unless Enum.all?(evidence, fn row ->
             row.subject_hash == subject_hash and value(row.payload, "subject") == subject and
               value(row.payload, "subject_hash") == expected_hash and
               value(row.source_ref, "subject_hash") == expected_hash
           end),
           do: rollback(:missing_evidence)
  end

  defp goal_evidence_for!(goal, evidence_ids) do
    unless Enum.all?(evidence_ids, &valid_uuid?(&1)), do: rollback(:invalid_request)

    rows =
      Repo.all(
        from(evidence in RunEvidence,
          join: run in Run,
          on: run.id == evidence.run_id,
          join: task in Task,
          on: task.id == run.task_id,
          where:
            evidence.id in ^evidence_ids and task.goal_id == ^goal.id and
              task.goal_revision == ^goal.current_revision and task.purpose == "validate" and
              task.state == "completed" and
              run.generation == task.current_generation and run.state == "completed" and
              (evidence.kind != "review" or
                 fragment("? ->> 'review_task_id' = ?::text", evidence.source_ref, task.id)),
          lock: "FOR UPDATE"
        )
      )

    if length(rows) != length(Enum.uniq(evidence_ids)), do: rollback(:missing_evidence)
    rows
  end

  defp ensure_required_evidence!(contract, evidence, subject_hash, decision_id, goal, work_item) do
    predicates = value(contract || %{}, "predicates", [])
    if predicates == [], do: rollback(:missing_evidence)

    Enum.each(predicates, fn predicate ->
      predicate_id = required_string!(predicate, :id)
      kind = required_string!(predicate, :kind)

      case kind do
        "operator_acceptance" ->
          decision_id || rollback(:operator_acceptance_required)

          if work_item do
            ensure_resolved_work_item_acceptance_decision!(
              goal,
              work_item,
              decision_id,
              subject_hash
            )
          else
            ensure_resolved_completion_decision!(goal, decision_id, subject_hash)
          end

        _ ->
          found =
            Enum.any?(evidence, fn row ->
              row.subject_hash == subject_hash and row.verdict == "passed" and
                value(row.payload, "predicate_id") == predicate_id and
                goal_predicate_matches_evidence?(predicate, row)
            end)

          unless found, do: rollback(:missing_evidence)
      end
    end)
  end

  defp goal_predicate_matches_evidence?(predicate, evidence) do
    source = evidence.source_ref

    case value(predicate, "kind") do
      "check" ->
        evidence.kind == "check" and
          value(source, "validator_profile") == value(predicate, "validator_profile") and
          evidence.validator_profile == value(predicate, "validator_profile")

      "artifact" ->
        subject = value(evidence.payload, "subject")

        evidence.kind == "artifact" and
          value(source, "resource_id") == value(predicate, "resource_id") and
          value(source, "path") == value(predicate, "path") and
          value(source, "commit") == value(evidence.payload, "commit") and
          value(source, "resource_id") == value(subject, "resource_id") and
          value(source, "commit") == value(subject, "commit") and
          value(evidence.payload, "resource_id") == value(predicate, "resource_id") and
          value(evidence.payload, "path") == value(predicate, "path")

      "review" ->
        evidence.kind == "review" and
          evidence.validator_profile == value(predicate, "reviewer_profile") and
          value(evidence.payload, "verdict") == evidence.verdict

      _ ->
        false
    end
  end

  defp creates_cycle?(goal_id, work_item_id, depends_on_id) do
    # Commands already hold the Goal row lock. Keep the traversal in PostgreSQL
    # so that check and insert observe the same serialized Goal mutation stream.
    result =
      Ecto.Adapters.SQL.query!(
        Repo,
        """
        WITH RECURSIVE reachable(id) AS (
          SELECT $1::uuid

          UNION

          SELECT dependency.depends_on_id
          FROM work_dependencies AS dependency
          INNER JOIN reachable ON dependency.work_item_id = reachable.id
          WHERE dependency.goal_id = $2::uuid
        )
        SELECT EXISTS (SELECT 1 FROM reachable WHERE id = $3::uuid)
        """,
        [depends_on_id, goal_id, work_item_id]
      )

    match?(%{rows: [[true]]}, result)
  end

  defp replay_create(mutation_id, body) do
    case Repo.get(GoalEvent, mutation_id) do
      nil -> :missing
      event -> replay_event(event, body)
    end
  end

  defp replay_command(goal_id, mutation_id, body) do
    case Repo.one(from(event in GoalEvent, where: event.id == ^mutation_id, lock: "FOR UPDATE")) do
      nil -> :missing
      %GoalEvent{goal_id: ^goal_id} = event -> replay_event(event, body)
      _event -> {:error, :idempotency_conflict}
    end
  end

  defp replay_command_after_conflict(goal_id, mutation_id, body) do
    case Repo.get(GoalEvent, mutation_id) do
      %GoalEvent{goal_id: ^goal_id} = event -> replay_event(event, body)
      _ -> {:error, :idempotency_conflict}
    end
  end

  defp replay_event(event, body) do
    if RequestHash.matches?(event.request_hash, event.request_hash_version, body) do
      goal = Repo.get(Goal, event.goal_id)
      if goal, do: {:ok, receipt!(goal, event, event.response)}, else: {:error, :not_found}
    else
      {:error, :idempotency_conflict}
    end
  end

  defp append_event!(goal, mutation_id, kind, actor_ref, body, response, opts) do
    # Goal events are new v1 domain receipts. Their canonical bytes must not
    # depend on the legacy Orchestration request-hash rollout mode.
    request_hash = RequestHash.canonical(body)
    request_hash_version = 2
    sequence = goal.event_sequence + 1
    current = now(opts)
    next_wake_at = event_next_wake_at(goal, kind, opts)
    event_revision = Keyword.get(opts, :event_revision, goal.current_revision)
    response = normalize_map(response)

    receipt =
      immutable_receipt_snapshot(
        goal,
        mutation_id,
        kind,
        sequence,
        event_revision,
        response,
        next_wake_at,
        current
      )

    stored_response = Map.put(response, "_receipt_v1", encode_receipt_snapshot(receipt))

    event_changeset =
      %GoalEvent{}
      |> GoalEvent.changeset(%{
        id: mutation_id,
        goal_id: goal.id,
        sequence: sequence,
        kind: kind,
        actor_ref: actor_ref,
        request_hash: request_hash,
        request_hash_version: request_hash_version,
        revision: event_revision,
        payload: normalize_map(body),
        response: stored_response
      })
      |> stamp_insert(current)
      |> Changeset.unique_constraint(:id, name: :goal_events_pkey)

    event =
      case Repo.insert(event_changeset) do
        {:ok, event} -> event
        {:error, _changeset} -> rollback(:idempotency_conflict)
      end

    updated_goal =
      goal
      |> Changeset.change(event_sequence: sequence, next_wake_at: next_wake_at)
      |> Changeset.optimistic_lock(:lock_version)
      |> stamp_update(current)
      |> Repo.update!()

    if next_wake_at != goal.next_wake_at or kind in ["resolve_decision", "task_settled"],
      do: enqueue_wakeup!(updated_goal, current)

    event
  end

  defp enqueue_wakeup!(%Goal{next_wake_at: nil}, _current), do: :ok

  defp enqueue_wakeup!(%Goal{next_wake_at: next_wake_at} = goal, current) do
    job_options =
      if DateTime.compare(next_wake_at, current) == :gt do
        # A future external check must survive an already-queued immediate hint.
        # The Goal row remains authoritative; stale wake jobs simply find it not due.
        [scheduled_at: next_wake_at, unique: false]
      else
        []
      end

    %{"goal_id" => goal.id}
    |> WakeupWorker.new(job_options)
    |> Oban.insert!()
  end

  defp enqueue_goal_control!(goal, event) do
    %{
      "goal_id" => goal.id,
      "revision" => event.revision,
      "action_id" => event.id
    }
    |> GoalControlWorker.new()
    |> Oban.insert!()
  end

  defp receipt!(goal, event, response) do
    case decode_receipt_snapshot(response) do
      {:ok, receipt} -> receipt
      :error -> current_receipt(goal, event, response)
    end
  end

  defp immutable_receipt_snapshot(
         goal,
         mutation_id,
         kind,
         sequence,
         event_revision,
         response,
         next_wake_at,
         current
       ) do
    %{
      "schema_version" => "symmetry.goal_receipt.v1",
      "goal" => %{
        "id" => goal.id,
        "state" => goal.state,
        "version" => goal.lock_version + 1,
        "current_revision" => goal.current_revision,
        "next_wake_at" => nullable_datetime(next_wake_at),
        "updated_at" => DateTime.to_iso8601(current)
      },
      "event" => %{
        "id" => mutation_id,
        "sequence" => sequence,
        "kind" => kind,
        "revision" => event_revision
      },
      "response" => response
    }
  end

  defp current_receipt(goal, event, response) do
    {:ok, projection} = ReadModel.fetch(goal)

    %{
      goal: projection,
      event: %{id: event.id, sequence: event.sequence, kind: event.kind, revision: event.revision},
      response: response,
      goal_id: goal.id,
      mutation_id: event.id
    }
  end

  defp encode_receipt_snapshot(receipt), do: normalize_map(receipt)

  defp decode_receipt_snapshot(response) when is_map(response) do
    with %{
           "schema_version" => "symmetry.goal_receipt.v1",
           "goal" => %{
             "id" => goal_id,
             "state" => state,
             "version" => version,
             "current_revision" => current_revision
           },
           "event" => %{
             "id" => event_id,
             "sequence" => sequence,
             "kind" => kind,
             "revision" => revision
           },
           "response" => stored_response
         } <- value(response, "_receipt_v1"),
         true <-
           is_binary(goal_id) and is_binary(state) and is_integer(version) and
             is_integer(current_revision) and is_binary(event_id) and is_integer(sequence) and
             is_binary(kind) and is_integer(revision) and is_map(stored_response),
         %{
           "next_wake_at" => next_wake_at,
           "updated_at" => updated_at
         } <- value(value(response, "_receipt_v1"), "goal", %{}) do
      {:ok,
       %{
         goal: %{
           id: goal_id,
           state: state,
           version: version,
           current_revision: current_revision,
           next_wake_at: next_wake_at,
           updated_at: updated_at
         },
         event: %{id: event_id, sequence: sequence, kind: kind, revision: revision},
         response: stored_response,
         goal_id: goal_id,
         mutation_id: event_id
       }}
    else
      _ -> :error
    end
  end

  defp decode_receipt_snapshot(_response), do: :error

  defp nullable_datetime(nil), do: nil
  defp nullable_datetime(%DateTime{} = datetime), do: DateTime.to_iso8601(datetime)

  defp response_for(goal, details) do
    Map.merge(
      %{
        "goal_id" => goal.id,
        "state" => goal.state,
        "version" => goal.lock_version + 1,
        "current_revision" => goal.current_revision
      },
      normalize_map(details)
    )
  end

  defp event_next_wake_at(goal, kind, opts) do
    case Keyword.fetch(opts, :settlement_next_wake_at) do
      {:ok, next_wake_at} ->
        next_wake_at

      :error ->
        if goal.state == "active" and
             kind in [
               "activate",
               "resume",
               "admit_task",
               "resolve_decision",
               "task_unstarted_settled"
             ] do
          now(opts)
        else
          goal.next_wake_at
        end
    end
  end

  defp ensure_preconditions!(goal, parsed) do
    if goal.lock_version != parsed.expected_version or
         goal.current_revision != parsed.expected_revision do
      {:ok, projection} = ReadModel.fetch(goal)

      rollback(
        {:stale,
         %{
           current_version: goal.lock_version,
           current_revision: goal.current_revision,
           allowed_actions: projection.allowed_actions
         }}
      )
    end
  end

  defp lock_goal(goal_id) do
    Repo.one(from(goal in Goal, where: goal.id == ^goal_id, lock: "FOR UPDATE")) ||
      rollback(:not_found)
  end

  defp goal_project_id!(goal_id) do
    Repo.one(from(goal in Goal, where: goal.id == ^goal_id, select: goal.project_id)) ||
      rollback(:not_found)
  end

  # Archival takes FOR UPDATE. Acquire the compatible Project lock before a
  # command can authorize new Goal work or mutate Goal authority.
  defp lock_active_project!(project_id) do
    project =
      Repo.one(from(project in Project, where: project.id == ^project_id, lock: "FOR SHARE")) ||
        rollback(:not_found)

    if project.status == "active", do: project, else: rollback(:state_conflict)
  end

  defp try_lock_active_goal_project(goal_id) do
    with project_id when is_binary(project_id) <-
           Repo.one(from(goal in Goal, where: goal.id == ^goal_id, select: goal.project_id)) do
      Repo.one(
        from(project in Project,
          where: project.id == ^project_id,
          lock: "FOR SHARE SKIP LOCKED"
        )
      )
    end
  end

  defp lock_work_item(id) do
    Repo.one(from(item in WorkItem, where: item.id == ^id, lock: "FOR UPDATE")) ||
      rollback(:not_found)
  end

  defp lock_external_wait(id) do
    Repo.one(from(wait in GoalExternalWait, where: wait.id == ^id, lock: "FOR UPDATE")) ||
      rollback(:not_found)
  end

  defp lock_context_snapshot(goal_id, snapshot_id) do
    Repo.one(
      from(snapshot in ContextSnapshot,
        where: snapshot.goal_id == ^goal_id and snapshot.id == ^snapshot_id,
        lock: "FOR UPDATE"
      )
    ) || rollback(:not_found)
  end

  defp lock_repository_resource!(id) do
    resource =
      Repo.one(from(resource in ProjectResource, where: resource.id == ^id, lock: "FOR UPDATE")) ||
        rollback(:not_found)

    if resource.kind != "repository", do: rollback(:resource_not_allowed)
    resource
  end

  # A plan may refer to several repositories in arbitrary proposal order.
  # Take every resource lock once in canonical UUID order, then use the map to
  # preserve the proposal's original business ordering during admission.
  defp lock_repository_resources!(ids) do
    ids = ids |> Enum.uniq() |> Enum.sort()

    resources =
      Repo.all(
        from(resource in ProjectResource,
          where: resource.id in ^ids,
          order_by: [asc: resource.id],
          lock: "FOR UPDATE"
        )
      )

    if length(resources) != length(ids), do: rollback(:not_found)
    if Enum.any?(resources, &(&1.kind != "repository")), do: rollback(:resource_not_allowed)

    Map.new(resources, &{&1.id, &1})
  end

  defp lock_work_items(ids) do
    rows =
      Repo.all(
        from(item in WorkItem,
          where: item.id in ^ids,
          order_by: [asc: item.id],
          lock: "FOR UPDATE"
        )
      )

    if length(rows) != length(Enum.uniq(ids)), do: rollback(:not_found)
    Map.new(rows, &{&1.id, &1}) |> then(fn found -> Enum.map(ids, &Map.fetch!(found, &1)) end)
  end

  defp lock_task(id),
    do:
      Repo.one(from(task in Task, where: task.id == ^id, lock: "FOR UPDATE")) ||
        rollback(:not_found)

  defp lock_settlement_tasks!(task_id, validation_of_task_id) do
    ids =
      [task_id, validation_of_task_id]
      |> Enum.filter(&valid_uuid?/1)
      |> Enum.uniq()
      |> Enum.sort()

    tasks =
      Repo.all(
        from(task in Task,
          where: task.id in ^ids,
          order_by: [asc: task.id],
          lock: "FOR UPDATE"
        )
      )
      |> Map.new(&{&1.id, &1})

    task = Map.get(tasks, task_id) || rollback(:not_found)
    {task, Map.get(tasks, task.validation_of_task_id)}
  end

  defp lock_settlement_runs!(run_id, task_id, producer) do
    producing_run_id =
      case producer do
        %Task{} ->
          Repo.one(
            from(run in Run,
              where:
                run.task_id == ^producer.id and run.generation == ^producer.current_generation and
                  run.state == "completed",
              select: run.id
            )
          )

        _ ->
          nil
      end

    ids =
      [run_id, producing_run_id]
      |> Enum.filter(&valid_uuid?/1)
      |> Enum.uniq()
      |> Enum.sort()

    runs =
      Repo.all(
        from(run in Run,
          where: run.id in ^ids,
          order_by: [asc: run.id],
          lock: "FOR UPDATE"
        )
      )
      |> Map.new(&{&1.id, &1})

    run = Map.get(runs, run_id)

    if is_nil(run) or run.task_id != task_id do
      {nil, nil}
    else
      {run, Map.get(runs, producing_run_id)}
    end
  end

  defp lock_run(id),
    do:
      Repo.one(from(run in Run, where: run.id == ^id, lock: "FOR UPDATE")) || rollback(:not_found)

  defp lock_decision(id),
    do:
      Repo.one(from(decision in GoalDecision, where: decision.id == ^id, lock: "FOR UPDATE")) ||
        rollback(:not_found)

  defp current_revision!(goal) do
    Repo.one(
      from(revision in GoalRevision,
        where: revision.goal_id == ^goal.id and revision.revision == ^goal.current_revision
      )
    ) || rollback(:stale_revision)
  end

  defp unresolved_decisions?(goal_id, revision) do
    Repo.exists?(
      from(decision in GoalDecision,
        where:
          decision.goal_id == ^goal_id and decision.goal_revision == ^revision and
            decision.state == "open"
      )
    )
  end

  defp active_task_for_item?(work_item_id) do
    Repo.exists?(
      from(task in Task,
        where: task.work_item_id == ^work_item_id and task.state in ^@nonterminal_task_states
      )
    )
  end

  defp plan_task?(%Task{purpose: "plan", work_item_id: nil, validation_of_task_id: nil}), do: true
  defp plan_task?(_task), do: false

  # `plan_proposed` is a scoped planning result, never a generic Task result.
  # Other purpose/result combinations remain intentionally open until a full
  # purpose matrix is part of the approved contract.
  defp task_result_matches_task?(task, "plan_proposed"), do: plan_task?(task)

  defp task_result_matches_task?(task, kind)
       when kind in [
              "progress",
              "candidate_completion",
              "blocked",
              "repair_required",
              "replan_required",
              "failed"
            ],
       do: not plan_task?(task)

  defp task_result_matches_task?(_task, _kind), do: false

  defp task_repository_resource_id!(%Task{} = task, nil) do
    if plan_task?(task) do
      task.input
      |> value("subject")
      |> parse_subject!()
      |> Map.fetch!("resource_id")
    else
      rollback(:ownership_lost)
    end
  end

  defp task_repository_resource_id!(_task, item) when not is_nil(item),
    do: item.repository_resource_id

  defp settlement_task_owned_by_goal?(task, goal, nil) do
    plan_task?(task) and task.goal_id == goal.id
  end

  defp settlement_task_owned_by_goal?(task, goal, item) do
    task.goal_id == goal.id and task.work_item_id == item.id and item.goal_id == goal.id
  end

  defp pending_plan_task?(goal, excluded_task_id \\ nil) do
    query =
      from(task in Task,
        where:
          task.goal_id == ^goal.id and task.goal_revision == ^goal.current_revision and
            task.purpose == "plan",
        select: %{id: task.id, state: task.state}
      )

    query =
      if is_binary(excluded_task_id),
        do: where(query, [task], task.id != ^excluded_task_id),
        else: query

    tasks = Repo.all(query)

    Enum.any?(tasks, fn task ->
      task.state in @nonterminal_task_states or
        not Repo.exists?(
          from(event in GoalEvent,
            where:
              event.goal_id == ^goal.id and event.kind == "task_settled" and
                fragment("? ->> 'task_id' = ?", event.response, ^task.id)
          )
        )
    end)
  end

  defp plan_decision_pending_acceptance?(goal) do
    Repo.exists?(
      from(decision in GoalDecision,
        where:
          decision.goal_id == ^goal.id and decision.goal_revision == ^goal.current_revision and
            decision.kind == "plan" and
            (decision.state == "open" or
               (decision.state == "resolved" and
                  fragment("? ->> 'option_id' = 'accept'", decision.resolution)))
      )
    )
  end

  defp planning_acceptance_contract do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "The operator must accept the proposed plan.",
      "predicates" => [%{"id" => "operator", "kind" => "operator_acceptance"}]
    }
  end

  defp ensure_decision_work_item!(goal, work_item_id) do
    item = lock_work_item(work_item_id)
    ensure_admissible_item!(goal, item)
  end

  defp ensure_resolved_scope_decision!(goal, item, decision_id, operation, dependency_id) do
    decision = lock_decision(decision_id)

    unless decision.goal_id == goal.id and decision.goal_revision == goal.current_revision and
             decision.state == "resolved" and decision.kind == "scope" and
             decision.work_item_id == item.id and
             decision.action_hash ==
               dependency_action_hash(goal, operation, item.id, dependency_id) and
             value(decision.resolution || %{}, "option_id") == "accept",
           do: rollback(:invalid_decision)
  end

  defp ensure_resolved_completion_decision!(goal, decision_id, subject_hash) do
    decision = lock_decision(decision_id)

    unless decision.goal_id == goal.id and decision.goal_revision == goal.current_revision and
             decision.state == "resolved" and decision.kind == "completion" and
             is_nil(decision.work_item_id) and
             decision.subject_hash == subject_hash and
             decision.action_hash == completion_action_hash(goal, subject_hash) and
             value(decision.resolution || %{}, "option_id") == "accept",
           do: rollback(:invalid_decision)
  end

  defp ensure_resolved_work_item_acceptance_decision!(goal, item, decision_id, subject_hash) do
    decision = lock_decision(decision_id)

    unless decision.goal_id == goal.id and decision.goal_revision == goal.current_revision and
             decision.state == "resolved" and decision.kind == "review" and
             decision.work_item_id == item.id and decision.subject_hash == subject_hash and
             decision.action_hash == work_item_acceptance_action_hash(goal, item.id, subject_hash) and
             value(decision.resolution || %{}, "option_id") == "accept",
           do: rollback(:invalid_decision)
  end

  defp decision_option?(decision, option_id) do
    Enum.any?(decision.options || [], &(value(&1, :id) == option_id))
  end

  defp ensure_goal_final_acceptance!(revision) do
    case ContractValidation.final_acceptance_authority(
           revision.authority_policy || %{},
           revision.execution_policy || %{},
           revision.acceptance_contract || %{}
         ) do
      {:ok, authority} -> authority
      {:error, _reason} -> rollback(:invalid_contract)
    end
  end

  defp ensure_final_acceptance_authority!(goal, :operator, decision_id, subject_hash) do
    decision_id || rollback(:operator_acceptance_required)
    ensure_resolved_completion_decision!(goal, decision_id, subject_hash)
  end

  defp ensure_final_acceptance_authority!(_goal, :deterministic, _decision_id, _subject_hash),
    do: :ok

  defp valid_initial_revision(revision, opts) when is_map(revision) and is_list(opts) do
    with {:ok, _} <- required_string(revision, :objective),
         {:ok, non_goals} <- optional_list(revision, :non_goals, []),
         true <- Enum.all?(non_goals, &(is_binary(&1) and String.trim(&1) != "")),
         {:ok, acceptance} <- required_map(revision, :acceptance_contract),
         true <- is_map(value(revision, :authority_policy, %{})),
         true <- is_map(value(revision, :execution_policy, %{})),
         true <- is_map(value(revision, :context_manifest, %{})),
         {:ok, _} <- required_string(revision, :reason),
         true <- valid_acceptance_contract?(acceptance),
         :ok <- immutable_acceptance_contracts_valid?([acceptance], opts) do
      :ok
    else
      {:error, reason} -> {:error, reason}
      _ -> {:error, :invalid_request}
    end
  end

  defp valid_initial_revision(_, _), do: {:error, :invalid_request}

  defp valid_acceptance_contract?(contract) when is_map(contract) do
    predicates = value(contract, :predicates)

    predicate_ids =
      if is_list(predicates) and Enum.all?(predicates, &is_map/1),
        do: Enum.map(predicates, &value(&1, :id)),
        else: []

    only_known_keys?(contract, ["schema_version", "description", "predicates"]) and
      value(contract, :schema_version) == "symmetry.acceptance.v1" and
      is_binary(value(contract, :description)) and
      String.length(value(contract, :description)) in 1..16_384 and
      is_list(predicates) and length(predicates) in 1..256 and
      Enum.all?(predicates, &valid_predicate?/1) and
      length(predicate_ids) == MapSet.size(MapSet.new(predicate_ids))
  end

  defp valid_acceptance_contract?(_contract), do: false

  defp immutable_plan_contracts_valid?(items, opts) do
    items
    |> Enum.map(&value(&1, :acceptance))
    |> immutable_acceptance_contracts_valid?(opts)
  end

  defp immutable_acceptance_contracts_valid?(acceptance_contracts, opts)
       when is_list(acceptance_contracts) and is_list(opts) do
    registry_opts =
      case Keyword.fetch(opts, :validation_profiles) do
        {:ok, profiles} -> [profiles: profiles]
        :error -> []
      end

    with {:ok, profiles} <- ValidationProfiles.snapshot(registry_opts) do
      if Enum.all?(acceptance_contracts, fn acceptance_contract ->
           match?(
             {:ok, _bindings},
             ValidationProfiles.bindings_for_acceptance(acceptance_contract, profiles: profiles)
           )
         end),
         do: :ok,
         else: {:error, :invalid_validation_profile}
    else
      {:error, _reason} -> {:error, :invalid_validation_profile}
    end
  end

  defp immutable_acceptance_contracts_valid?(_, _), do: {:error, :invalid_validation_profile}

  defp valid_plan_proposal?(proposal, goal, opts) when is_map(proposal) do
    items = value(proposal, :items)

    validate_contract(:plan_proposal, proposal, opts) == :ok and
      valid_plan_envelope?(proposal, goal) and
      is_list(items) and
      length(items) in 1..@max_plan_items and
      Enum.all?(items, &valid_plan_item?/1)
  end

  defp valid_plan_proposal?(_proposal, _goal, _opts), do: false

  # This is intentionally Goal-context admission validation rather than JSON
  # Schema validation: a PlanProposal remains wire-valid without an integration
  # item, but it cannot be admitted as a complete Goal plan.
  defp plan_admission_preflight?(items, opts) when is_list(items) and is_list(opts) do
    if Enum.all?(items, &is_map/1) do
      keys = Enum.map(items, &value(&1, :key))
      items_by_key = Map.new(items, &{value(&1, :key), &1})

      Enum.any?(items, &(value(&1, :integration, false) == true)) and
        Enum.all?(items, &valid_plan_item?/1) and
        length(keys) == MapSet.size(MapSet.new(keys)) and
        Enum.all?(items, &plan_item_dependencies_valid?(&1, items_by_key)) and
        plan_dependencies_acyclic?(items_by_key) and
        immutable_plan_contracts_valid?(items, opts) == :ok
    else
      false
    end
  end

  defp plan_admission_preflight?(_items, _opts), do: false

  defp plan_item_dependencies_valid?(item, items_by_key) do
    item_key = value(item, :key)
    dependencies = value(item, :depends_on_keys, [])

    Enum.all?(dependencies, &(Map.has_key?(items_by_key, &1) and &1 != item_key)) and
      plan_dependency_baseline_same_resource?(item, items_by_key)
  end

  defp plan_dependency_baseline_same_resource?(item, items_by_key) do
    case value(value(item, :baseline), :kind) do
      "dependency" ->
        dependency_key = value(value(item, :baseline), :key)
        dependency = Map.get(items_by_key, dependency_key)

        dependency_key in value(item, :depends_on_keys, []) and is_map(dependency) and
          value(dependency, :repository_resource_id) == value(item, :repository_resource_id)

      _ ->
        true
    end
  end

  defp plan_dependencies_acyclic?(items_by_key) do
    Enum.all?(Map.keys(items_by_key), fn key ->
      not plan_dependency_cycle?(key, items_by_key, MapSet.new(), MapSet.new())
    end)
  end

  defp plan_dependency_cycle?(key, items_by_key, visiting, visited) do
    cond do
      MapSet.member?(visiting, key) ->
        true

      MapSet.member?(visited, key) ->
        false

      true ->
        item = Map.fetch!(items_by_key, key)
        visiting = MapSet.put(visiting, key)
        visited = MapSet.put(visited, key)

        Enum.any?(value(item, :depends_on_keys, []), fn dependency_key ->
          plan_dependency_cycle?(dependency_key, items_by_key, visiting, visited)
        end)
    end
  end

  defp valid_plan_envelope?(proposal, goal) do
    full_keys = ["schema_version", "proposal_id", "goal_id", "expected_revision", "items"]

    only_known_keys?(proposal, full_keys) and
      Enum.all?(full_keys, &map_has_key?(proposal, &1)) and
      value(proposal, :schema_version) == "symmetry.plan.v1" and
      valid_uuid?(value(proposal, :proposal_id)) and value(proposal, :goal_id) == goal.id and
      required_safe_positive_integer(proposal, :expected_revision) ==
        {:ok, goal.current_revision}
  end

  defp valid_plan_item?(item) when is_map(item) do
    required_keys = [
      "key",
      "title",
      "description",
      "required",
      "repository_resource_id",
      "acceptance",
      "depends_on_keys",
      "model_profile",
      "baseline",
      "change_target"
    ]

    dependencies = value(item, :depends_on_keys)

    only_known_keys?(item, required_keys ++ ["integration"]) and
      Enum.all?(required_keys, &map_has_key?(item, &1)) and
      is_short_identifier?(value(item, :key)) and
      valid_long_text?(value(item, :title)) and valid_long_text?(value(item, :description)) and
      is_boolean(value(item, :required)) and is_boolean(value(item, :integration, false)) and
      valid_uuid?(value(item, :repository_resource_id)) and
      valid_acceptance_contract?(value(item, :acceptance)) and is_list(dependencies) and
      length(dependencies) <= @max_plan_dependencies_per_item and
      Enum.all?(dependencies, &is_short_identifier?/1) and
      length(dependencies) == MapSet.size(MapSet.new(dependencies)) and
      is_short_identifier?(value(item, :model_profile)) and
      valid_plan_baseline?(value(item, :baseline), value(item, :repository_resource_id)) and
      valid_plan_change_target?(value(item, :change_target))
  end

  defp valid_plan_item?(_item), do: false

  defp valid_plan_baseline?(nil, _resource_id), do: false

  defp valid_plan_baseline?(baseline, resource_id) when is_map(baseline) do
    case value(baseline, :kind) do
      "subject" ->
        only_known_keys?(baseline, ["kind", "subject"]) and
          map_has_key?(baseline, "subject") and
          match?(
            {:ok, %{"resource_id" => ^resource_id}},
            parse_subject(value(baseline, :subject))
          )

      "dependency" ->
        only_known_keys?(baseline, ["kind", "key"]) and
          is_short_identifier?(value(baseline, :key))

      _ ->
        false
    end
  end

  defp valid_plan_baseline?(_baseline, _resource_id), do: false

  defp valid_plan_change_target?(nil), do: true

  defp valid_plan_change_target?(target) when is_map(target) do
    case value(target, :kind) do
      "branches" ->
        only_known_keys?(target, ["kind", "source_branch", "target_branch"]) and
          untrimmed_string?(value(target, :source_branch)) and
          untrimmed_string?(value(target, :target_branch)) and
          value(target, :source_branch) != value(target, :target_branch)

      "pull_request" ->
        only_known_keys?(target, ["kind", "pull_request_url"]) and
          untrimmed_string?(value(target, :pull_request_url))

      _ ->
        false
    end
  end

  defp valid_plan_change_target?(_target), do: false

  defp required_untrimmed_string!(map, key) do
    case value(map, key) do
      value when is_binary(value) ->
        if value == String.trim(value) and value != "", do: value, else: rollback(:invalid_plan)

      _ ->
        rollback(:invalid_plan)
    end
  end

  defp untrimmed_string?(value) when is_binary(value),
    do: value == String.trim(value) and value != ""

  defp untrimmed_string?(_value), do: false

  defp valid_predicate?(predicate) when is_map(predicate) do
    case value(predicate, :kind) do
      "check" ->
        only_known_keys?(predicate, ["id", "kind", "validator_profile"]) and
          is_short_identifier?(value(predicate, :id)) and
          is_short_identifier?(value(predicate, :validator_profile))

      "artifact" ->
        only_known_keys?(predicate, ["id", "kind", "resource_id", "path"]) and
          is_short_identifier?(value(predicate, :id)) and
          valid_uuid?(value(predicate, :resource_id)) and
          valid_commit_path?(value(predicate, :path))

      "review" ->
        only_known_keys?(predicate, ["id", "kind", "reviewer_profile"]) and
          is_short_identifier?(value(predicate, :id)) and
          is_short_identifier?(value(predicate, :reviewer_profile))

      "operator_acceptance" ->
        only_known_keys?(predicate, ["id", "kind"]) and
          is_short_identifier?(value(predicate, :id))

      _ ->
        false
    end
  end

  defp valid_predicate?(_), do: false

  defp acceptance_contract_matches_resource?(contract, resource_id) do
    Enum.all?(value(contract, :predicates, []), fn predicate ->
      value(predicate, :kind) != "artifact" or value(predicate, :resource_id) == resource_id
    end)
  end

  defp revision_attrs(goal_id, revision, contract, actor_ref) do
    %{
      goal_id: goal_id,
      revision: revision,
      objective: required_string!(contract, :objective),
      non_goals: value(contract, :non_goals, []),
      acceptance_contract: required_map!(contract, :acceptance_contract),
      authority_policy: canonical_authority_policy(value(contract, :authority_policy, %{})),
      execution_policy: normalized_policy(value(contract, :execution_policy, %{})),
      context_manifest: canonical_context_manifest(value(contract, :context_manifest, %{})),
      reason: required_string!(contract, :reason),
      actor_ref: actor_ref
    }
  end

  defp goal_revision_wire(attrs, inserted_at) do
    execution_policy =
      case attrs.execution_policy["budget_limit_microusd"] do
        value when is_integer(value) ->
          Map.put(attrs.execution_policy, "budget_limit_microusd", Integer.to_string(value))

        _ ->
          attrs.execution_policy
      end

    execution_policy =
      case execution_policy["per_run_cost_limit_microusd"] do
        value when is_integer(value) ->
          Map.put(execution_policy, "per_run_cost_limit_microusd", Integer.to_string(value))

        _ ->
          execution_policy
      end

    %{
      "schema_version" => "symmetry.goal_revision.v1",
      "goal_id" => attrs.goal_id,
      "revision" => attrs.revision,
      "objective" => attrs.objective,
      "non_goals" => attrs.non_goals,
      "acceptance_contract" => attrs.acceptance_contract,
      "authority_policy" => attrs.authority_policy,
      "execution_policy" => execution_policy,
      "context_manifest" => attrs.context_manifest,
      "reason" => attrs.reason,
      "actor_ref" => attrs.actor_ref,
      "inserted_at" => DateTime.to_iso8601(inserted_at)
    }
  end

  defp normalized_policy(policy) do
    policy = normalize_map(policy)

    budget_limit = policy_microusd!(value(policy, "budget_limit_microusd"))
    per_run_cost_limit = policy_microusd!(value(policy, "per_run_cost_limit_microusd"))
    budget_mode = value(policy, "budget_mode", "soft")

    hard_cost_limit_required =
      case value(policy, "hard_cost_limit_required", budget_mode == "strict") do
        value when is_boolean(value) -> value
        _ -> rollback(:invalid_request)
      end

    normalized =
      policy
      |> Map.put_new("automatic_execution", false)
      |> Map.put_new("max_parallel_tasks", @default_max_parallel_tasks)
      |> Map.put_new("max_task_admissions", 1)
      |> Map.put_new("max_run_attempts_per_task", @default_max_run_attempts)
      |> Map.put("budget_limit_microusd", budget_limit)
      |> Map.put("per_run_cost_limit_microusd", per_run_cost_limit)
      |> Map.put("budget_mode", budget_mode)
      |> Map.put("hard_cost_limit_required", hard_cost_limit_required)
      |> Map.put_new("allowed_runtime_ids", [])
      |> Map.put_new("final_acceptance", "operator")
      |> Map.put_new("allowed_actions", [])
      |> Map.put_new("allowed_resource_ids", [])
      |> Map.put_new("allowed_model_profiles", [])

    validate_execution_policy!(normalized)
    normalized
  end

  defp admission_subject!(goal, item, "validate", payload, opts) do
    if Map.has_key?(payload, :subject) or Map.has_key?(payload, "subject"),
      do: rollback(:invalid_validation)

    producer_id = optional_uuid!(payload, :validation_of_task_id) || rollback(:invalid_request)
    producer = lock_task(producer_id)

    unless producer.goal_id == goal.id and producer.work_item_id == item.id and
             producer.goal_revision == goal.current_revision and producer.state == "completed" and
             producer.current_generation > 0,
           do: rollback(:invalid_validation)

    run =
      Repo.one(
        from(run in Run,
          where:
            run.task_id == ^producer.id and run.generation == ^producer.current_generation and
              run.state == "completed",
          lock: "FOR UPDATE"
        )
      ) || rollback(:invalid_validation)

    {subject, _subject_hash, _result_id} =
      candidate_subject_from_result!(run, opts, :invalid_validation)

    ensure_subject_resource!(subject, item, :invalid_validation)
    subject
  end

  defp admission_subject!(_goal, item, _purpose, payload, _opts) do
    subject = required_map!(payload, :subject)

    case parse_subject(subject) do
      {:ok, %{"resource_id" => resource_id} = parsed}
      when resource_id == item.repository_resource_id ->
        parsed

      _ ->
        rollback(:invalid_subject)
    end
  end

  defp parse_subject!(subject) do
    case parse_subject(subject) do
      {:ok, parsed} -> parsed
      _ -> rollback(:invalid_subject)
    end
  end

  defp parse_subject(subject) when is_map(subject) do
    with true <- only_known_keys?(subject, ["resource_id", "commit", "tree_digest"]),
         {:ok, resource_id} <- required_uuid(subject, :resource_id),
         {:ok, commit} <- required_string(subject, :commit),
         {:ok, tree_digest} <- required_sha256(subject, :tree_digest),
         true <- valid_commit?(commit) do
      {:ok, %{"resource_id" => resource_id, "commit" => commit, "tree_digest" => tree_digest}}
    else
      _ -> {:error, :invalid_subject}
    end
  end

  defp parse_subject(_), do: {:error, :invalid_subject}

  defp admission_session_mode!(payload, requested_session_id) do
    mode =
      value(payload, :session_mode, if(is_nil(requested_session_id), do: "fresh", else: "resume"))

    unless mode in ["fresh", "resume", "handoff"], do: rollback(:invalid_request)
    if mode == "fresh" and not is_nil(requested_session_id), do: rollback(:invalid_request)

    if mode == "resume" and is_nil(requested_session_id),
      do: rollback(:requested_session_required)

    if mode == "handoff" and not is_nil(requested_session_id), do: rollback(:invalid_request)
    if mode == "handoff", do: rollback(:unsupported_capability)

    mode
  end

  defp ensure_requested_session_available!(_item, nil, "fresh"), do: :ok

  defp ensure_requested_session_available!(item, requested_session_id, session_mode)
       when session_mode == "resume" do
    untrusted_session =
      Repo.get(HarnessSession, requested_session_id) || rollback(:requested_session_not_found)

    runtime =
      Repo.one(
        from(runtime in Runtime,
          where: runtime.id == ^untrusted_session.runtime_id,
          lock: "FOR UPDATE"
        )
      ) || rollback(:requested_session_unavailable)

    session =
      Repo.one(
        from(session in HarnessSession,
          where: session.id == ^requested_session_id,
          lock: "FOR UPDATE"
        )
      ) || rollback(:requested_session_not_found)

    unless session.state == "available" and is_nil(session.active_run_id) and
             session.repository_resource_id == item.repository_resource_id and
             session.runtime_id == runtime.id and runtime.status == "online" and
             runtime.repository_resource_id == item.repository_resource_id and
             session.harness_kind == runtime.harness_kind and
             session.harness_version == runtime.harness_version and
             session.adapter_version == runtime.adapter_version and
             native_resume_capable?(runtime.capabilities) do
      rollback(:requested_session_unavailable)
    end
  end

  defp ensure_plan_requested_session_available!(_resource, nil, "fresh"), do: :ok

  defp ensure_plan_requested_session_available!(resource, requested_session_id, "resume") do
    untrusted_session =
      Repo.get(HarnessSession, requested_session_id) || rollback(:requested_session_not_found)

    runtime =
      Repo.one(
        from(runtime in Runtime,
          where: runtime.id == ^untrusted_session.runtime_id,
          lock: "FOR UPDATE"
        )
      ) || rollback(:requested_session_unavailable)

    session =
      Repo.one(
        from(session in HarnessSession,
          where: session.id == ^requested_session_id,
          lock: "FOR UPDATE"
        )
      ) || rollback(:requested_session_not_found)

    unless session.state == "available" and is_nil(session.active_run_id) and
             session.repository_resource_id == resource.id and session.runtime_id == runtime.id and
             runtime.status == "online" and runtime.repository_resource_id == resource.id and
             session.harness_kind == runtime.harness_kind and
             session.harness_version == runtime.harness_version and
             session.adapter_version == runtime.adapter_version and
             native_resume_capable?(runtime.capabilities) do
      rollback(:requested_session_unavailable)
    end
  end

  defp native_resume_capable?(capabilities) do
    value(capabilities, "adapter", %{})
    |> value("operations", %{})
    |> value("resume") == true
  end

  defp admission_limits!(payload, opts) do
    supplied = value(payload, :limits, %{})

    unless is_map(supplied) and only_known_keys?(supplied, ["max_turns", "deadline_at"]),
      do: rollback(:invalid_request)

    max_turns = value(supplied, :max_turns, 1)
    deadline_at = value(supplied, :deadline_at, DateTime.add(now(opts), 15 * 60, :second))

    unless is_integer(max_turns) and max_turns > 0 and max_turns <= 9_007_199_254_740_991,
      do: rollback(:invalid_request)

    deadline_at =
      case deadline_at do
        %DateTime{} = datetime ->
          datetime

        value when is_binary(value) ->
          case DateTime.from_iso8601(value) do
            {:ok, datetime, _offset} -> datetime
            _ -> rollback(:invalid_request)
          end

        _ ->
          rollback(:invalid_request)
      end

    if DateTime.compare(deadline_at, now(opts)) != :gt, do: rollback(:invalid_request)
    %{"max_turns" => max_turns, "deadline_at" => DateTime.to_iso8601(deadline_at)}
  end

  # Cost ceilings and reservations are approved revision policy, never an
  # operator-command parameter. A strict admission reserves every permitted
  # infrastructure attempt before it can be queued.
  defp server_admission_budget!(goal, revision, limits) do
    case value(revision.execution_policy || %{}, "budget_mode", "soft") do
      "soft" ->
        ensure_budget!(goal, revision, nil)
        {Map.put(limits, "max_cost_microusd", nil), nil, %{}}

      "strict" ->
        ceiling =
          policy_nonnegative_integer(revision.execution_policy, "per_run_cost_limit_microusd") ||
            rollback(:strict_budget_requires_reservation)

        unless value(revision.execution_policy, "hard_cost_limit_required", false) == true do
          rollback(:strict_budget_requires_reservation)
        end

        {:ok, reservation_amount} = server_reservation_amount(revision)

        ensure_budget!(goal, revision, reservation_amount)

        {
          Map.put(limits, "max_cost_microusd", Integer.to_string(ceiling)),
          reservation_amount,
          %{"adapter" => %{"operations" => %{"hard_cost_limit" => true}}}
        }

      _ ->
        rollback(:invalid_request)
    end
  end

  defp multiply_microusd!(left, right)
       when is_integer(left) and left >= 0 and is_integer(right) and right > 0 do
    if left == 0 or right <= div(@max_microusd, left),
      do: left * right,
      else: rollback(:invalid_request)
  end

  defp multiply_microusd!(_left, _right), do: rollback(:invalid_request)

  defp require_provider_access(required_capabilities, nil), do: required_capabilities

  defp require_provider_access(required_capabilities, _provider_scope),
    do: Map.put(required_capabilities, "provider_access", true)

  defp admission_provider_scope!(_goal, revision, item, purpose) do
    resource_id = item.repository_resource_id

    unless resource_allowed?(revision.execution_policy, resource_id) do
      rollback(:resource_not_allowed)
    end

    if purpose != "implement" do
      nil
    else
      case item.change_target do
        nil ->
          if provider_operation_allowed?(revision, "resource.sync") do
            provider_scope_resource!(resource_id, ["repositories"])
            provider_scope(resource_id, ["resource.sync"], nil)
          end

        %{"kind" => "branches"} = target ->
          unless provider_operation_allowed?(revision, "change.upsert"),
            do: rollback(:invalid_plan)

          provider_scope_resource!(resource_id, ["repositories", "changes"])
          target = branch_change_target!(target, revision)

          operations =
            if provider_operation_allowed?(revision, "change.update"),
              do: ["change.upsert", "change.update"],
              else: ["change.upsert"]

          provider_scope(resource_id, operations, target)

        %{"kind" => "pull_request"} = target ->
          unless provider_operation_allowed?(revision, "change.update"),
            do: rollback(:invalid_plan)

          {resource, connection} =
            provider_scope_resource!(resource_id, ["repositories", "changes"])

          target = pull_request_change_target!(target, connection, resource, revision)
          provider_scope(resource_id, ["change.update"], target)

        _ ->
          rollback(:invalid_plan)
      end
    end
  end

  defp provider_scope(resource_id, operations, change_target) do
    %{
      "resource_ids" => [resource_id],
      "operations_by_resource" => %{resource_id => operations},
      "change_target" => change_target
    }
  end

  defp provider_scope_resource!(resource_id, required_capabilities) do
    resource =
      Repo.one(from(resource in ProjectResource, where: resource.id == ^resource_id)) ||
        rollback(:resource_not_allowed)

    {resource, provider_scope_connection!(resource, required_capabilities)}
  end

  defp provider_scope_connection!(resource, required_capabilities) do
    connection =
      case resource.connection_id do
        connection_id when is_binary(connection_id) -> Repo.get(Connection, connection_id)
        _ -> nil
      end

    valid? =
      resource.kind == "repository" and resource.provider in ["github", "azure_devops"] and
        is_map(connection) and connection.provider == resource.provider and
        is_list(connection.capabilities) and
        Enum.all?(required_capabilities, &(&1 in connection.capabilities))

    if valid?, do: connection, else: rollback(:unsupported_capability)
  end

  defp resource_allowed?(policy, resource_id) do
    case allowed_values(policy, "allowed_resource_ids") do
      [] -> true
      resource_ids -> resource_id in resource_ids
    end
  end

  defp provider_operation_allowed?(revision, operation) do
    operation in allowed_values(revision.authority_policy, "allowed_actions") and
      operation in allowed_values(revision.execution_policy, "allowed_actions")
  end

  defp admission_next_action(payload, purpose, model_profile) do
    case value(payload, :next_action) do
      action when is_map(action) ->
        action

      nil ->
        %{
          "kind" => "replan",
          "reason" => "Execute the admitted #{purpose} task with model profile #{model_profile}."
        }

      _ ->
        rollback(:invalid_request)
    end
  end

  defp valid_commit?(value),
    do: is_binary(value) and Regex.match?(~r/^[0-9a-f]{40}(?:[0-9a-f]{24})?$/, value)

  defp valid_commit_path?(value) when is_binary(value) do
    String.length(value) in 1..1024 and
      not String.starts_with?(value, "/") and
      not Enum.any?(["\\", "//", <<0>>], &String.contains?(value, &1)) and
      Enum.all?(String.split(value, "/"), &(&1 != ".."))
  end

  defp valid_commit_path?(_value), do: false

  defp required_sha256(map, key) do
    case value(map, key) do
      value when is_binary(value) ->
        if Regex.match?(~r/^sha256:[0-9a-f]{64}$/, value),
          do: {:ok, value},
          else: {:error, :invalid_request}

      _ ->
        {:error, :invalid_request}
    end
  end

  defp parse_command(command, opts) do
    with true <-
           only_known_keys?(command, [
             "schema_version",
             "mutation_id",
             "expected_version",
             "expected_revision",
             "kind",
             "payload"
           ]),
         {:ok, command_kind} <- required_string(command, :kind),
         true <- command_kind in @command_kinds,
         :ok <- validate_goal_contract(:goal_command, command, opts),
         {:ok, mutation_id} <- required_uuid(command, :mutation_id),
         {:ok, expected_version} <- required_safe_nonnegative_integer(command, :expected_version),
         {:ok, expected_revision} <- required_safe_positive_integer(command, :expected_revision),
         {:ok, kind} <- required_string(command, :kind),
         true <- kind in @command_kinds,
         {:ok, payload} <- required_map(command, :payload),
         payload = canonical_command_payload(kind, payload),
         true <- valid_command_payload?(kind, payload) do
      {:ok,
       %{
         mutation_id: mutation_id,
         expected_version: expected_version,
         expected_revision: expected_revision,
         kind: kind,
         payload: payload
       }}
    else
      {:error, {:invalid_contract, _reason}} = error -> error
      _ -> {:error, :invalid_request}
    end
  end

  defp valid_command_payload?("activate", payload),
    do: only_known_keys?(payload, ["approved_revision"])

  defp valid_command_payload?(kind, payload) when kind in ["pause", "resume", "cancel"],
    do: only_known_keys?(payload, ["reason"])

  defp valid_command_payload?("amend", payload),
    do: only_known_keys?(payload, ["revision_contract", "reason"])

  defp valid_command_payload?("request_plan", payload),
    do:
      only_known_keys?(payload, [
        "model_profile",
        "repository_resource_id",
        "subject",
        "session_mode",
        "requested_session_id"
      ])

  defp valid_command_payload?("accept_plan", payload),
    do: only_known_keys?(payload, ["proposal", "proposal_hash", "decision_id"])

  defp valid_command_payload?("request_decision", payload),
    do:
      only_known_keys?(payload, [
        "kind",
        "question",
        "options",
        "work_item_id",
        "subject_hash",
        "depends_on_id",
        "dependency_operation",
        "proposal",
        "expires_at"
      ])

  defp valid_command_payload?("resolve_decision", payload),
    do:
      only_known_keys?(payload, [
        "decision_id",
        "expected_decision_version",
        "option_id",
        "comment"
      ])

  defp valid_command_payload?("admit_task", payload),
    do:
      only_known_keys?(payload, [
        "work_item_id",
        "purpose",
        "model_profile",
        "session_mode",
        "requested_session_id",
        "validation_of_task_id"
      ])

  defp valid_command_payload?(kind, payload) when kind in ["add_dependency", "remove_dependency"],
    do: only_known_keys?(payload, ["work_item_id", "depends_on_id", "decision_id"])

  defp valid_command_payload?("achieve", payload),
    do:
      only_known_keys?(payload, [
        "subject",
        "integration_work_item_id",
        "evidence_ids",
        "decision_id"
      ])

  defp valid_command_payload?(_, _payload), do: false

  defp canonical_command_payload("accept_plan", payload) do
    Map.update(payload, "proposal", nil, &canonical_plan_proposal/1)
  end

  defp canonical_command_payload("request_decision", payload) do
    if value(payload, :kind) == "plan" do
      Map.update(payload, "proposal", nil, &canonical_plan_proposal/1)
    else
      payload
    end
  end

  defp canonical_command_payload(_kind, payload), do: payload

  defp canonical_plan_proposal(proposal) when is_map(proposal) do
    proposal = normalize_map(proposal)

    Map.update(proposal, "items", [], fn items ->
      if is_list(items),
        do:
          Enum.map(items, fn item ->
            item
            |> normalize_map()
            |> Map.put_new("integration", false)
            |> Map.put_new("change_target", nil)
          end),
        else: items
    end)
  end

  defp canonical_plan_proposal(proposal), do: proposal

  defp valid_event_options(opts) do
    after_value = Keyword.get(opts, :after)
    limit = Keyword.get(opts, :limit, @default_event_limit)

    if (is_nil(after_value) or (is_integer(after_value) and after_value >= 0)) and
         is_integer(limit) and limit > 0, do: :ok, else: {:error, :invalid_request}
  end

  defp validate_contract(_kind, _document, opts) when not is_list(opts),
    do: {:error, :invalid_request}

  defp validate_contract(kind, document, opts) do
    schema_root =
      Keyword.get_lazy(opts, :schema_root, fn ->
        :symmetry_control
        |> Application.fetch_env!(:contracts)
        |> Keyword.fetch!(:directory)
        |> Path.join("v1")
      end)

    case kind do
      :goal_create ->
        ContractValidation.validate_goal_create(document, schema_root: schema_root)

      :goal_command ->
        ContractValidation.validate_goal_command(document, schema_root: schema_root)

      :plan_proposal ->
        ContractValidation.validate_plan_proposal(document, schema_root: schema_root)

      :admission ->
        ContractValidation.validate_admission(document, schema_root: schema_root)

      :context_snapshot ->
        ContractValidation.validate_context_snapshot(document, schema_root: schema_root)

      :task_result ->
        ContractValidation.validate_task_result(document, schema_root: schema_root)

      :evidence ->
        ContractValidation.validate_evidence(document, schema_root: schema_root)

      :usage ->
        ContractValidation.validate_usage(document, schema_root: schema_root)

      :decision ->
        ContractValidation.validate_decision(document, schema_root: schema_root)

      :goal_revision ->
        ContractValidation.validate_goal_revision(document, schema_root: schema_root)
    end
  end

  defp validate_goal_contract(kind, document, opts) do
    case validate_contract(kind, document, opts) do
      :ok -> :ok
      {:error, reason} -> {:error, {:invalid_contract, reason}}
    end
  end

  defp valid_attention_options(opts) do
    limit = Keyword.get(opts, :limit, @default_attention_limit)
    cursor = Keyword.get(opts, :cursor)

    if is_integer(limit) and limit > 0 and
         (is_nil(cursor) or (is_binary(cursor) and cursor != "")),
       do: :ok,
       else: {:error, :invalid_request}
  end

  defp required_uuid(map, key) do
    case value(map, key) do
      value when is_binary(value) ->
        if valid_uuid?(value), do: {:ok, value}, else: {:error, :invalid_request}

      _ ->
        {:error, :invalid_request}
    end
  end

  defp required_uuid!(map, key), do: required_uuid(map, key) |> unwrap!()

  defp optional_uuid(map, key) do
    case value(map, key) do
      nil ->
        {:ok, nil}

      value when is_binary(value) ->
        if valid_uuid?(value), do: {:ok, value}, else: {:error, :invalid_request}

      _ ->
        {:error, :invalid_request}
    end
  end

  defp optional_uuid!(map, key), do: value(map, key) |> optional_uuid!()
  defp optional_uuid!(nil), do: nil

  defp optional_uuid!(value) when is_binary(value),
    do: if(valid_uuid?(value), do: value, else: rollback(:invalid_request))

  defp optional_uuid!(_), do: rollback(:invalid_request)
  defp valid_uuid(value), do: if(valid_uuid?(value), do: :ok, else: {:error, :invalid_request})
  defp valid_uuid?(value) when is_binary(value), do: match?({:ok, _}, Ecto.UUID.cast(value))
  defp valid_uuid?(_), do: false

  defp same_uuid?(_left, nil), do: false

  defp same_uuid?(left, right) when is_binary(left) and is_binary(right) do
    case {Ecto.UUID.dump(left), Ecto.UUID.dump(right)} do
      {{:ok, left}, {:ok, right}} -> left == right
      _ -> false
    end
  end

  defp same_uuid?(_left, _right), do: false

  defp required_string(map, key) do
    case value(map, key) do
      value when is_binary(value) ->
        value = String.trim(value)
        if byte_size(value) > 0, do: {:ok, value}, else: {:error, :invalid_request}

      _ ->
        {:error, :invalid_request}
    end
  end

  defp required_string!(map, key), do: required_string(map, key) |> unwrap!()

  defp optional_string(map, key),
    do: if(is_binary(value(map, key)), do: String.trim(value(map, key)), else: nil)

  defp require_reason!(payload), do: required_string!(payload, :reason)

  defp required_map(map, key) do
    case value(map, key) do
      value when is_map(value) -> {:ok, value}
      _ -> {:error, :invalid_request}
    end
  end

  defp required_map!(map, key), do: required_map(map, key) |> unwrap!()
  defp required_list!(map, key), do: optional_list(map, key, :missing) |> unwrap!()

  defp optional_list(map, key, default) do
    case value(map, key, default) do
      value when is_list(value) -> {:ok, value}
      _ -> {:error, :invalid_request}
    end
  end

  defp required_nonnegative_integer(map, key) do
    case value(map, key) do
      value when is_integer(value) and value >= 0 -> {:ok, value}
      _ -> {:error, :invalid_request}
    end
  end

  defp required_safe_nonnegative_integer(map, key) do
    with {:ok, value} <- required_nonnegative_integer(map, key),
         true <- value <= @max_safe_integer do
      {:ok, value}
    else
      _ -> {:error, :invalid_request}
    end
  end

  defp required_positive_integer(map, key) do
    case value(map, key) do
      value when is_integer(value) and value > 0 -> {:ok, value}
      _ -> {:error, :invalid_request}
    end
  end

  defp required_safe_positive_integer(map, key) do
    with {:ok, value} <- required_positive_integer(map, key),
         true <- value <= @max_safe_integer do
      {:ok, value}
    else
      _ -> {:error, :invalid_request}
    end
  end

  defp required_safe_positive_integer!(map, key),
    do: required_safe_positive_integer(map, key) |> unwrap!()

  defp required_digest!(map, key), do: value(map, key) |> digest!()

  defp parse_digest(value) when is_binary(value) do
    encoded = String.replace_prefix(value, "sha256:", "")

    if Regex.match?(~r/^[0-9a-f]{64}$/, encoded) and
         (value == encoded or value == "sha256:" <> encoded) do
      Base.decode16(encoded, case: :lower)
    else
      {:error, :invalid_request}
    end
  end

  defp parse_digest(_), do: {:error, :invalid_request}

  defp required_sha256_digest(map, key) do
    value = value(map, key)

    if is_binary(value) and Regex.match?(~r/^sha256:[0-9a-f]{64}$/, value) do
      parse_digest(value)
    else
      {:error, :invalid_request}
    end
  end

  defp is_short_identifier?(value),
    do: is_binary(value) and Regex.match?(~r/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/, value)

  defp optional_digest!(map, key), do: value(map, key) |> optional_digest!()
  defp optional_digest!(nil), do: nil
  defp optional_digest!(value), do: digest!(value)
  defp digest!(value) when is_binary(value) and byte_size(value) == 32, do: value

  defp digest!(value) when is_binary(value), do: parse_digest(value) |> unwrap!()

  defp digest!(_), do: rollback(:invalid_request)

  defp policy_integer(policy, key, default \\ nil) do
    case value(policy || %{}, key, default) do
      value when is_integer(value) and value > 0 and value <= @max_safe_integer -> value
      nil -> nil
      _ -> rollback(:invalid_request)
    end
  end

  defp policy_nonnegative_integer(policy, key, default \\ nil) do
    case value(policy || %{}, key, default) do
      value when is_integer(value) and value >= 0 and value <= @max_microusd -> value
      nil -> nil
      _ -> rollback(:invalid_request)
    end
  end

  defp validate_execution_policy!(policy) do
    for key <- ["max_parallel_tasks", "max_task_admissions", "max_run_attempts_per_task"] do
      value = value(policy, key)

      unless is_integer(value) and value > 0 and value <= @max_safe_integer do
        rollback(:invalid_request)
      end
    end

    budget_limit = value(policy, "budget_limit_microusd")

    unless is_nil(budget_limit) or
             (is_integer(budget_limit) and budget_limit >= 0 and budget_limit <= @max_microusd) do
      rollback(:invalid_request)
    end

    per_run_cost_limit = value(policy, "per_run_cost_limit_microusd")

    unless is_nil(per_run_cost_limit) or
             (is_integer(per_run_cost_limit) and per_run_cost_limit >= 0 and
                per_run_cost_limit <= @max_microusd) do
      rollback(:invalid_request)
    end

    case value(policy, "budget_mode") do
      "soft" ->
        :ok

      "strict" ->
        if is_nil(per_run_cost_limit) or value(policy, "hard_cost_limit_required") != true do
          rollback(:strict_budget_requires_reservation)
        end

      _ ->
        rollback(:invalid_request)
    end

    unless is_boolean(value(policy, "automatic_execution")) do
      rollback(:invalid_request)
    end

    if value(policy, "automatic_execution") == true and is_nil(budget_limit) do
      rollback(:automatic_budget_limit_required)
    end
  end

  defp allowed_values(policy, key) do
    case value(policy || %{}, key, []) do
      values when is_list(values) ->
        if Enum.all?(values, &is_binary/1), do: values, else: rollback(:invalid_request)

      _ ->
        rollback(:invalid_request)
    end
  end

  defp settlement_mutation_id(task_id, run_id, generation) do
    :crypto.hash(
      :sha256,
      "settlement:" <> task_id <> ":" <> run_id <> ":" <> Integer.to_string(generation)
    )
    |> Base.encode16(case: :lower)
    |> then(fn hex ->
      String.slice(hex, 0, 8) <>
        "-" <>
        String.slice(hex, 8, 4) <>
        "-5" <>
        String.slice(hex, 13, 3) <>
        "-a" <> String.slice(hex, 17, 3) <> "-" <> String.slice(hex, 20, 12)
    end)
  end

  defp late_validation_outcome_mutation_id(task_id, run_id, generation) do
    :crypto.hash(
      :sha256,
      "late-validation-outcome:" <>
        task_id <> ":" <> run_id <> ":" <> Integer.to_string(generation)
    )
    |> Base.encode16(case: :lower)
    |> then(fn hex ->
      String.slice(hex, 0, 8) <>
        "-" <>
        String.slice(hex, 8, 4) <>
        "-5" <>
        String.slice(hex, 13, 3) <>
        "-a" <> String.slice(hex, 17, 3) <> "-" <> String.slice(hex, 20, 12)
    end)
  end

  defp unstarted_settlement_mutation_id(task_id, reason) do
    :crypto.hash(:sha256, "unstarted-settlement:" <> task_id <> ":" <> reason)
    |> Base.encode16(case: :lower)
    |> then(fn hex ->
      String.slice(hex, 0, 8) <>
        "-" <>
        String.slice(hex, 8, 4) <>
        "-5" <>
        String.slice(hex, 13, 3) <>
        "-a" <>
        String.slice(hex, 17, 3) <>
        "-" <>
        String.slice(hex, 20, 12)
    end)
  end

  defp automatic_admission_mutation_id(goal, item, purpose, source_identity) do
    :crypto.hash(
      :sha256,
      "automatic-admission:" <>
        goal.id <>
        ":" <>
        Integer.to_string(goal.current_revision) <>
        ":" <> item.id <> ":" <> purpose <> ":" <> source_identity
    )
    |> Base.encode16(case: :lower)
    |> then(fn hex ->
      String.slice(hex, 0, 8) <>
        "-" <>
        String.slice(hex, 8, 4) <>
        "-5" <>
        String.slice(hex, 13, 3) <>
        "-a" <>
        String.slice(hex, 17, 3) <>
        "-" <>
        String.slice(hex, 20, 12)
    end)
  end

  defp automatic_admission_blocked_mutation_id(goal, candidate_identity) do
    :crypto.hash(
      :sha256,
      "automatic-admission-blocked:" <>
        goal.id <> ":" <> Integer.to_string(goal.current_revision) <> ":" <> candidate_identity
    )
    |> Base.encode16(case: :lower)
    |> then(fn hex ->
      String.slice(hex, 0, 8) <>
        "-" <>
        String.slice(hex, 8, 4) <>
        "-5" <>
        String.slice(hex, 13, 3) <>
        "-a" <>
        String.slice(hex, 17, 3) <>
        "-" <> String.slice(hex, 20, 12)
    end)
  end

  defp automatic_reconciliation_deferred_mutation_id(goal, deferred_identity, reason) do
    :crypto.hash(
      :sha256,
      "automatic-reconciliation-deferred:" <>
        goal.id <>
        ":" <>
        Integer.to_string(goal.current_revision) <> ":" <> deferred_identity <> ":" <> reason
    )
    |> Base.encode16(case: :lower)
    |> then(fn hex ->
      String.slice(hex, 0, 8) <>
        "-" <>
        String.slice(hex, 8, 4) <>
        "-5" <>
        String.slice(hex, 13, 3) <>
        "-a" <>
        String.slice(hex, 17, 3) <> "-" <> String.slice(hex, 20, 12)
    end)
  end

  defp automatic_source_task_id(source_identity) when is_binary(source_identity) do
    case String.split(source_identity, ":") do
      ["progress", task_id, _run_id, _generation, _result_id] ->
        if valid_uuid?(task_id), do: task_id

      ["repair", task_id, _result_id] ->
        if valid_uuid?(task_id), do: task_id

      ["external", _wait_id, task_id, _run_id, _result_id, _check_seq] ->
        if valid_uuid?(task_id), do: task_id

      [task_id] ->
        if valid_uuid?(task_id), do: task_id

      _ ->
        nil
    end
  end

  defp automatic_source_result_id(source_identity) when is_binary(source_identity) do
    case String.split(source_identity, ":") do
      ["progress", _task_id, _run_id, _generation, result_id] ->
        if valid_uuid?(result_id), do: result_id

      ["repair", _task_id, result_id] ->
        if valid_uuid?(result_id), do: result_id

      ["external", _wait_id, _task_id, _run_id, result_id, _check_seq] ->
        if valid_uuid?(result_id), do: result_id

      _ ->
        nil
    end
  end

  defp external_wait_identity(wait) do
    "external:" <>
      wait.id <>
      ":" <>
      wait.task_id <>
      ":" <> wait.run_id <> ":" <> wait.result_id <> ":" <> Integer.to_string(wait.check_seq)
  end

  defp plan_action_hash(goal, proposal_hash) do
    RequestHash.canonical(%{
      kind: "plan",
      goal_id: goal.id,
      revision: goal.current_revision,
      proposal_hash: Base.encode16(proposal_hash, case: :lower)
    })
  end

  defp completion_action_hash(goal, subject_hash) do
    RequestHash.canonical(%{
      kind: "completion",
      goal_id: goal.id,
      revision: goal.current_revision,
      subject_hash: Base.encode16(subject_hash, case: :lower)
    })
  end

  defp work_item_acceptance_action_hash(goal, work_item_id, subject_hash) do
    RequestHash.canonical(%{
      kind: "work_item_acceptance",
      goal_id: goal.id,
      revision: goal.current_revision,
      work_item_id: work_item_id,
      subject_hash: Base.encode16(subject_hash, case: :lower)
    })
  end

  defp dependency_action_hash(goal, operation, work_item_id, dependency_id) do
    RequestHash.canonical(%{
      kind: operation,
      goal_id: goal.id,
      revision: goal.current_revision,
      work_item_id: work_item_id,
      depends_on_id: dependency_id
    })
  end

  defp validate_decision_changeset!(changeset, opts) do
    unless changeset.valid?, do: rollback(:invalid_request)

    decision = Changeset.apply_changes(changeset)

    case validate_contract(:decision, decision_wire(decision), opts) do
      :ok -> :ok
      {:error, _reason} -> rollback(:invalid_request)
    end
  end

  defp decision_wire(decision) do
    %{
      "schema_version" => "symmetry.decision.v1",
      "decision_id" => decision.id,
      "goal_id" => decision.goal_id,
      "goal_revision" => decision.goal_revision,
      "work_item_id" => decision.work_item_id,
      "kind" => decision.kind,
      "action_hash" => context_digest(decision.action_hash),
      "subject_hash" => context_digest(decision.subject_hash),
      "state" => decision.state,
      "question" => decision.question,
      "options" => decision.options,
      "resolution" => decision.resolution,
      "actor_ref" => decision.actor_ref,
      "expires_at" => nullable_datetime(decision.expires_at),
      "lock_version" => decision.lock_version,
      "created_at" => DateTime.to_iso8601(decision.inserted_at),
      "updated_at" => DateTime.to_iso8601(decision.updated_at)
    }
  end

  defp admission_enabled?(opts) do
    Keyword.get(
      opts,
      :rollout_enabled,
      Application.get_env(:symmetry_control, :goals, [])[:rollout_enabled] == true
    )
  end

  defp value(map, key, default \\ nil) when is_map(map) do
    Map.get(map, key, Map.get(map, to_string(key), default))
  end

  defp normalize_map(map) when is_map(map),
    do: Map.new(map, fn {key, value} -> {to_string(key), normalize_value(value)} end)

  defp normalize_value(value) when is_map(value), do: normalize_map(value)
  defp normalize_value(value) when is_list(value), do: Enum.map(value, &normalize_value/1)
  defp normalize_value(value), do: value

  defp stamp_insert(changeset, current) do
    changeset
    |> Changeset.put_change(:inserted_at, current)
    |> maybe_put_timestamp(:updated_at, current)
  end

  defp maybe_put_timestamp(changeset, field, value) do
    if Map.has_key?(changeset.types, field),
      do: Changeset.put_change(changeset, field, value),
      else: changeset
  end

  defp stamp_update(changeset, current),
    do: Changeset.force_change(changeset, :updated_at, current)

  defp now(opts),
    do: Keyword.get(opts, :now, DateTime.utc_now() |> DateTime.truncate(:microsecond))

  defp valid_actor(actor_ref),
    do:
      if(is_binary(actor_ref) and String.trim(actor_ref) != "",
        do: :ok,
        else: {:error, :invalid_request}
      )

  defp map_has_key?(map, key) when is_map(map) do
    Map.has_key?(map, key) or
      Enum.any?(Map.keys(map), fn candidate ->
        is_atom(candidate) and Atom.to_string(candidate) == key
      end)
  end

  defp valid_long_text?(value),
    do: is_binary(value) and String.length(value) in 1..16_384

  defp optional_string_strict!(map, key) do
    case value(map, key) do
      nil ->
        nil

      value when is_binary(value) ->
        if String.length(value) in 1..16_384, do: value, else: rollback(:invalid_request)

      _ ->
        rollback(:invalid_request)
    end
  end

  defp unwrap!({:ok, value}), do: value
  defp unwrap!({:error, reason}), do: rollback(reason)
  defp rollback(reason), do: Repo.rollback(reason)
end
