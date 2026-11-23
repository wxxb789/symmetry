defmodule SymmetryControl.Goals.RunFence do
  @moduledoc false

  import Ecto.Query
  import SymmetryControl.Goals.ContractValidation, only: [validate_contract: 3]
  import SymmetryControl.Goals.EventsReceipts, only: [enqueue_wakeup!: 2]

  import SymmetryControl.Goals.Internal,
    only: [
      is_short_identifier?: 1,
      lock_context_snapshot!: 2,
      lock_goal!: 1,
      lock_project!: 1,
      lock_run!: 1,
      lock_task!: 1,
      lock_work_item!: 1,
      normalize_map: 1,
      now: 1,
      nullable_microusd: 2,
      nullable_safe_integer: 2,
      nullable_string: 2,
      only_known_keys?: 2,
      optional_uuid: 2,
      parse_digest: 1,
      parse_subject: 1,
      parse_subject!: 1,
      plan_task?: 1,
      required_map: 2,
      required_string: 2,
      required_string!: 2,
      required_uuid: 2,
      rollback: 1,
      settlement_mutation_id: 3,
      stamp_insert: 2,
      stamp_update: 2,
      valid_uuid: 1,
      valid_uuid?: 1,
      value: 2,
      value: 3
    ]

  alias SymmetryControl.Goals.Budget
  alias SymmetryControl.Goals.ContextAssembly
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.GoalEvent
  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Goals.RunEvidence
  alias SymmetryControl.Goals.RunUsage
  alias SymmetryControl.Goals.Settlement
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.Scheduler
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces.Project
  alias SymmetryControl.Workspaces.WorkItem

  @nonterminal_task_states [
    "queued",
    "assigned",
    "claimed",
    "running",
    "waiting_for_input",
    "paused",
    "cancelling"
  ]
  @max_evidence_batch_items 256

  @typedoc false
  @type new_claim_authority_envelope :: %{
          required(:project) => Project.t(),
          required(:goal) => Goal.t(),
          required(:work_item) => WorkItem.t() | nil,
          required(:task) => Task.t(),
          required(:run) => Run.t(),
          required(:ownership) => map()
        }

  @spec new_claim_authority(new_claim_authority_envelope()) ::
          :ok | {:error, :state_conflict | :ownership_lost}
  defp new_claim_authority(%{
         project: %Project{id: project_id, status: status},
         goal: %Goal{project_id: goal_project_id} = goal,
         work_item: work_item,
         task: %Task{} = task,
         run: %Run{} = run,
         ownership: ownership
       }) do
    cond do
      status != "active" ->
        {:error, :state_conflict}

      project_id != goal_project_id ->
        {:error, :ownership_lost}

      not claim_identity_current?(ownership, task, run) ->
        {:error, :ownership_lost}

      not claim_work_item_owned?(project_id, goal, work_item, task) ->
        {:error, :ownership_lost}

      not claim_task_current?(goal, task) ->
        {:error, :ownership_lost}

      true ->
        :ok
    end
  end

  defp new_claim_authority(_), do: {:error, :ownership_lost}

  def claim(run_id, request, opts \\ []) do
    notify? = not Repo.in_transaction?()

    case claim_with_disposition(run_id, request, opts) do
      {:ok, run, :created} ->
        if notify?, do: SymmetryControl.Orchestration.emit_claimed(run)
        {:ok, run}

      {:ok, run, :replayed} ->
        {:ok, run}

      error ->
        error
    end
  end

  def claim_with_disposition(run_id, request, opts \\ [])

  def claim_with_disposition(run_id, request, opts) when is_binary(run_id) and is_map(request) and is_list(opts) do
    if valid_uuid?(run_id) do
      result =
        if Repo.in_transaction?() do
          {:ok, claim_with_disposition_in_transaction(run_id, request, opts)}
        else
          Repo.transaction(fn -> claim_with_disposition_in_transaction(run_id, request, opts) end)
        end

      case result do
        {:ok, {:created, run}} -> {:ok, run, :created}
        {:ok, {:replayed, run}} -> {:ok, run, :replayed}
        {:error, reason} -> {:error, reason}
      end
    else
      {:error, :invalid_request}
    end
  end

  def claim_with_disposition(_, _, _), do: {:error, :invalid_request}

  defp claim_with_disposition_in_transaction(run_id, request, opts) do
    ownership =
      Repo.one(
        from run in Run,
          join: task in Task,
          on: task.id == run.task_id,
          left_join: goal in Goal,
          on: goal.id == task.goal_id,
          where: run.id == ^run_id,
          select: %{
            task_id: task.id,
            goal_id: task.goal_id,
            project_id: goal.project_id,
            work_item_id: task.work_item_id
          }
      ) || rollback(:not_found)

    notify_claim_authority_routed(opts, ownership)

    if is_nil(ownership.goal_id), do: rollback(:goal_authority_required)

    project = lock_project!(ownership.project_id)
    goal = lock_goal!(ownership.goal_id)
    item = if ownership.work_item_id, do: lock_work_item!(ownership.work_item_id)

    case SymmetryControl.Orchestration.prepare_claim_in_transaction(
           run_id,
           request,
           opts,
           item
         ) do
      {:replayed, %{run: run, task: task}} ->
        if claim_identity_current?(ownership, task, run) do
          {:replayed, run}
        else
          rollback(:ownership_lost)
        end

      {:new, %{run: run, task: task} = claim_context} ->
        case new_claim_authority(%{
               project: project,
               goal: goal,
               work_item: item,
               task: task,
               run: run,
               ownership: ownership
             }) do
          :ok ->
            case SymmetryControl.Orchestration.commit_new_claim_in_transaction(
                   claim_context,
                   opts
                 ) do
              {:created, claimed_run} -> {:created, claimed_run}
              {:error, reason} -> rollback(reason)
            end

          {:error, reason} ->
            rollback(reason)
        end

      {:error, reason} ->
        rollback(reason)
    end
  end

  def append_evidence(machine_id, run_id, fence, evidence, opts \\ [])

  def append_evidence(machine_id, run_id, fence, evidence, opts)
      when is_binary(machine_id) and is_binary(run_id) and is_map(fence) and is_map(evidence) and is_list(opts) do
    with :ok <- valid_uuid(machine_id),
         :ok <- valid_uuid(run_id),
         :ok <- valid_fence(fence),
         :ok <- ContextAssembly.safe_evidence_document(evidence),
         {:ok, evidence_attrs} <- evidence_attrs(evidence, run_id) do
      fn ->
        {goal, task, item, run, runtime} =
          lock_goal_run!(machine_id, run_id, fence, opts, :delivery_replay)

        case Repo.one(
               from(row in RunEvidence,
                 where: row.run_id == ^run.id and row.evidence_key == ^evidence_attrs.evidence_key,
                 lock: "FOR UPDATE"
               )
             ) do
          nil ->
            validate_evidence_contract!(evidence, opts)

            snapshot =
              lock_context_snapshot!(goal.id, task.context_snapshot_id || rollback(:not_found))

            if !(snapshot.goal_revision == task.goal_revision and
                   snapshot.work_item_id == task.work_item_id) do
              rollback(:ownership_lost)
            end

            context_wire_envelope!(snapshot, goal, item, task)

            evidence_attrs =
              ensure_evidence_identity!(task, item, run, runtime, snapshot, evidence_attrs)

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
                      where: row.run_id == ^run.id and row.evidence_key == ^evidence_attrs.evidence_key,
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
      end
      |> Repo.transaction()
      |> machine_write_result()
    end
  end

  def append_evidence(_, _, _, _, _), do: {:error, :invalid_request}

  def append_evidence_batch(machine_id, run_id, fence, batch, opts \\ [])

  def append_evidence_batch(machine_id, run_id, fence, batch, opts)
      when is_binary(machine_id) and is_binary(run_id) and is_map(fence) and is_map(batch) and is_list(opts) do
    with :ok <- valid_uuid(machine_id),
         :ok <- valid_uuid(run_id),
         :ok <- valid_fence(fence),
         :ok <- ContextAssembly.safe_evidence_document(batch),
         {:ok, evidence_items} <- evidence_batch_attrs(batch, run_id) do
      fn ->
        {goal, task, item, run, runtime} =
          lock_goal_run!(machine_id, run_id, fence, opts, :delivery_replay)

        classify_and_insert_evidence_batch!(
          goal,
          task,
          item,
          run,
          runtime,
          evidence_items,
          opts
        )
      end
      |> Repo.transaction()
      |> machine_write_result()
    end
  end

  def append_evidence_batch(_, _, _, _, _), do: {:error, :invalid_request}

  defp classify_and_insert_evidence_batch!(goal, task, item, run, runtime, evidence_items, opts) do
    keys = Enum.map(evidence_items, & &1.attrs.evidence_key)
    ids = Enum.map(evidence_items, & &1.attrs.id)

    existing_by_key =
      from(row in RunEvidence,
        where: row.run_id == ^run.id and row.evidence_key in ^keys
      )
      |> Repo.all()
      |> Map.new(&{&1.evidence_key, &1})

    existing_by_id =
      from(row in RunEvidence,
        where: row.id in ^ids
      )
      |> Repo.all()
      |> Map.new(&{&1.id, &1})

    classified =
      Enum.map(evidence_items, fn %{index: index, attrs: attrs} = item_data ->
        case Map.get(existing_by_key, attrs.evidence_key) do
          %RunEvidence{} = row ->
            if evidence_matches?(row, attrs) and row.id == attrs.id do
              {:replayed, index, row}
            else
              evidence_batch_conflict!(index, attrs)
            end

          nil ->
            case Map.get(existing_by_id, attrs.id) do
              nil ->
                {:new, index, item_data}

              _row ->
                evidence_batch_conflict!(index, attrs)
            end
        end
      end)

    new_items =
      Enum.flat_map(classified, fn
        {:new, _index, item_data} -> [item_data]
        _ -> []
      end)

    prepared_new_items =
      case new_items do
        [] ->
          []

        new_items ->
          validate_new_evidence_batch!(run.id, new_items, opts)

          snapshot =
            lock_context_snapshot!(goal.id, task.context_snapshot_id || rollback(:not_found))

          if !(snapshot.goal_revision == task.goal_revision and
                 snapshot.work_item_id == task.work_item_id) do
            rollback(:ownership_lost)
          end

          context_wire_envelope!(snapshot, goal, item, task)

          Enum.map(new_items, fn %{attrs: attrs} = item_data ->
            %{
              item_data
              | attrs: ensure_evidence_identity!(task, item, run, runtime, snapshot, attrs)
            }
          end)
      end

    prepared_by_index = Map.new(prepared_new_items, &{&1.index, &1})

    outcomes =
      Enum.map(classified, fn
        {:replayed, _index, row} ->
          {:replayed, row}

        {:new, index, _item_data} ->
          %{attrs: attrs} = Map.fetch!(prepared_by_index, index)

          if validation_evidence_closed?(goal, task, run, attrs) do
            rollback(:validation_evidence_closed)
          end

          changeset =
            %RunEvidence{id: attrs.id}
            |> RunEvidence.changeset(Map.drop(attrs, [:id, :observed_at]))
            |> stamp_insert(attrs.observed_at)

          row_attrs = Map.put(changeset.changes, :id, attrs.id)

          case Repo.insert_all(RunEvidence, [row_attrs], on_conflict: :nothing) do
            {1, _} ->
              {:created, Repo.get!(RunEvidence, attrs.id)}

            {0, _} ->
              row =
                Repo.one(
                  from(row in RunEvidence,
                    where: row.run_id == ^run.id and row.evidence_key == ^attrs.evidence_key
                  )
                )

              if (row && evidence_matches?(row, attrs)) and row.id == attrs.id do
                {:replayed, row}
              else
                evidence_batch_conflict!(index, attrs)
              end
          end
      end)

    disposition =
      if Enum.any?(outcomes, fn {item_disposition, _row} -> item_disposition == :created end),
        do: :created,
        else: :replayed

    response = %{
      evidence_batch: %{
        run_id: run.id,
        receipts: Enum.map(outcomes, &evidence_batch_receipt/1)
      }
    }

    case validate_contract(:evidence_batch_response, response, opts) do
      :ok -> {disposition, response}
      {:error, _reason} -> rollback(:invalid_request)
    end
  end

  defp evidence_batch_receipt({disposition, row}) do
    row
    |> evidence_receipt()
    |> Map.fetch!(:evidence)
    |> Map.put(:disposition, Atom.to_string(disposition))
  end

  defp evidence_batch_conflict!(index, attrs) do
    rollback(
      {:idempotency_conflict,
       %{
         details: %{
           items: [
             %{
               index: index,
               evidence_key: attrs.evidence_key,
               disposition: "conflict"
             }
           ]
         }
       }}
    )
  end

  def record_usage(machine_id, run_id, fence, usage, opts \\ [])

  def record_usage(machine_id, run_id, fence, usage, opts)
      when is_binary(machine_id) and is_binary(run_id) and is_map(fence) and is_map(usage) and is_list(opts) do
    with :ok <- valid_uuid(machine_id),
         :ok <- valid_uuid(run_id),
         :ok <- valid_fence(fence),
         {:ok, usage_attrs} <- usage_attrs(usage, run_id),
         :ok <- validate_contract(:usage, usage, opts) do
      fn ->
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
      end
      |> Repo.transaction()
      |> machine_write_result()
    end
  end

  def record_usage(_, _, _, _, _), do: {:error, :invalid_request}

  def fetch_run_context(machine_id, run_id, fence) when is_binary(machine_id) and is_binary(run_id) and is_map(fence) do
    with :ok <- valid_uuid(machine_id), :ok <- valid_uuid(run_id), :ok <- valid_fence(fence) do
      fn ->
        {goal, task, item, run, _runtime} =
          lock_goal_run!(machine_id, run_id, fence, [], :current)

        session_id =
          if task.purpose == "validate" and not is_nil(task.validation_of_task_id) and
               is_nil(run.harness_session_id) do
            # Deterministic validation is daemon-owned: fence required, no handle.
            nil
          else
            from(session in HarnessSession,
              where:
                session.id == ^run.harness_session_id and session.machine_id == ^machine_id and
                  session.active_run_id == ^run.id and
                  session.binding_id == ^run.harness_binding_id,
              lock: "FOR UPDATE"
            )
            |> Repo.one()
            |> case do
              %HarnessSession{id: id} -> id
              nil -> rollback(:ownership_lost)
            end
          end

        snapshot_id = task.context_snapshot_id || rollback(:not_found)
        snapshot = lock_context_snapshot!(goal.id, snapshot_id)

        %{
          goal_id: goal.id,
          task_id: task.id,
          run_id: run.id,
          generation: run.generation,
          session_id: session_id,
          context: context_wire_envelope!(snapshot, goal, item, task)
        }
      end
      |> Repo.transaction()
      |> case do
        {:ok, context} -> {:ok, context}
        {:error, reason} -> {:error, reason}
      end
    end
  end

  def fetch_run_context(_, _, _), do: {:error, :invalid_request}

  @doc false
  def lock_goal_run!(machine_id, run_id, fence, opts, mode) do
    task_id =
      Repo.one(from(run in Run, where: run.id == ^run_id, select: run.task_id)) ||
        rollback(:not_found)

    untrusted_task = Repo.get(Task, task_id) || rollback(:not_found)
    goal_id = untrusted_task.goal_id || rollback(:not_found)

    goal = lock_goal!(goal_id)
    item = if untrusted_task.work_item_id, do: lock_work_item!(untrusted_task.work_item_id)
    task = lock_task!(task_id)
    run = lock_run!(run_id)

    runtime =
      Repo.one(from(runtime in Runtime, where: runtime.id == ^run.runtime_id, lock: "FOR UPDATE")) ||
        rollback(:not_found)

    if !Settlement.settlement_task_owned_by_goal?(task, goal, item) do
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

  @doc false
  def ensure_machine_fence!(machine_id, task, run, runtime, fence, mode, current) do
    static? =
      runtime.machine_id == machine_id and
        run.runtime_id == value(fence, :runtime_id) and
        run.claimed_runtime_epoch == value(fence, :runtime_epoch) and
        run.generation == value(fence, :generation) and
        run.claim_id == value(fence, :claim_id) and
        run.lease_token == value(fence, :lease_token)

    if !static?, do: rollback(:ownership_lost)

    if mode not in [:late_accounting, :attach_replay, :delivery_replay, :session_stop] and
         task.current_generation != value(fence, :generation),
       do: rollback(:ownership_lost)

    if mode == :current do
      current? =
        runtime.connection_epoch == value(fence, :runtime_epoch) and
          run.state in @nonterminal_task_states and not is_nil(run.lease_expires_at) and
          DateTime.after?(run.lease_expires_at, current)

      if !current?, do: rollback(:ownership_lost)
    end
  end

  @doc false
  def valid_fence(fence) do
    fields = [:runtime_id, :claim_id, :lease_token]

    if Enum.all?(fields, &valid_uuid?(value(fence, &1))) and
         is_integer(value(fence, :runtime_epoch)) and value(fence, :runtime_epoch) > 0 and
         is_integer(value(fence, :generation)) and value(fence, :generation) > 0 do
      :ok
    else
      {:error, :invalid_request}
    end
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
         true <-
           ContextAssembly.safe_document?(source_ref) and
             ContextAssembly.safe_document?(payload) do
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

  defp evidence_batch_attrs(batch, run_id) do
    with true <- only_known_keys?(batch, ["schema_version", "run_id", "items"]),
         "symmetry.evidence_batch.v1" <- value(batch, :schema_version),
         ^run_id <- value(batch, :run_id),
         items when is_list(items) <- value(batch, :items),
         true <- items != [] and length(items) <= @max_evidence_batch_items do
      items
      |> Enum.with_index()
      |> Enum.reduce_while({:ok, []}, fn {item, index}, {:ok, acc} ->
        if is_map(item) do
          case evidence_attrs(item, run_id) do
            {:ok, attrs} ->
              {:cont, {:ok, [%{index: index, attrs: attrs, document: item} | acc]}}

            {:error, reason} ->
              {:halt, {:error, reason}}
          end
        else
          {:halt, {:error, :invalid_request}}
        end
      end)
      |> case do
        {:ok, attrs} ->
          attrs = Enum.reverse(attrs)
          keys = Enum.map(attrs, & &1.attrs.evidence_key)
          ids = Enum.map(attrs, & &1.attrs.id)

          cond do
            length(keys) != MapSet.size(MapSet.new(keys)) ->
              {:error, {:invalid_contract, :duplicate_evidence_key}}

            length(ids) != MapSet.size(MapSet.new(ids)) ->
              {:error, {:invalid_contract, :evidence_batch_duplicate_id}}

            true ->
              {:ok, attrs}
          end

        {:error, reason} ->
          {:error, reason}
      end
    else
      _ -> {:error, :invalid_request}
    end
  end

  # RunEvidence stores only the canonical Subject digest. Recompute it from
  # the outer request Subject before treating an old row as an exact replay.
  defp evidence_matches?(row, attrs) do
    row.id == attrs.id and row.kind == attrs.kind and row.subject_hash == attrs.subject_hash and
      row.subject_hash == RequestHash.canonical(attrs.subject) and
      value(row.payload, "subject") == normalize_map(attrs.subject) and
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

    if !(is_map(subject) and subject == evidence.subject and
           evidence.subject_hash == RequestHash.canonical(subject) and
           is_binary(predicate_id)) do
      rollback(:invalid_evidence_identity)
    end

    if evidence.kind == "observation" do
      if !source_matches_evidence?(evidence, subject, item, nil),
        do: rollback(:invalid_evidence_identity)

      evidence
    else
      predicate =
        snapshot
        |> snapshot_acceptance_contract!()
        |> value("predicates", [])
        |> Enum.find(&(value(&1, "id") == predicate_id and value(&1, "kind") == evidence.kind))

      if !(is_map(predicate) and source_matches_evidence?(evidence, subject, item, predicate)),
        do: rollback(:invalid_evidence_identity)

      if !(task.purpose == "validate" and not is_nil(task.validation_of_task_id)),
        do: rollback(:invalid_evidence_identity)

      expected_validator_profile = validator_profile_for!(predicate, evidence.kind)

      if evidence.validator_profile != expected_validator_profile,
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

    if !(is_map(binding) and
           binding_matches_evidence?(binding, evidence, expected_profile, task, run, runtime)) do
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

    if payload_subject == subject and
         payload_hash == "sha256:" <> Base.encode16(evidence.subject_hash, case: :lower) do
      source_matches_evidence_kind?(evidence, subject, item, predicate, source)
    else
      false
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
        subject_hash: ContextAssembly.context_digest(row.subject_hash),
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

    if !parent, do: rollback(:invalid_request)
  end

  defp reconcile_reservation_and_arm!(goal, task, opts) do
    Budget.reconcile_reservation!(goal, task, opts)

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

  defp evidence_validator_profile(evidence, "observation"), do: nullable_string(evidence, :validator_profile)

  defp evidence_validator_profile(evidence, _kind), do: required_string(evidence, :validator_profile)

  defp valid_usage_cost?("unknown", nil), do: true

  defp valid_usage_cost?(basis, amount) when basis in ["reported", "estimated"] and is_integer(amount), do: true

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

  def context_wire_envelope!(snapshot, goal, item, task) do
    case ContextAssembly.context_wire_envelope(snapshot, goal, item, task) do
      {:ok, context} ->
        context

      {:error, reason} ->
        rollback({:invalid_contract, reason})
    end
  end

  @doc false
  def machine_write_result(result) do
    case result do
      {:ok, {disposition, receipt}} when disposition in [:created, :replayed] ->
        {:ok, receipt, disposition}

      {:error, reason} ->
        {:error, reason}
    end
  end

  @doc false
  def machine_session_stop_result(result) do
    case machine_write_result(result) do
      {:ok, _receipt, :created} = created ->
        Scheduler.wake()
        created

      result ->
        result
    end
  end

  defp claim_task_current?(goal, task) do
    task.goal_id == goal.id and task.goal_revision == goal.current_revision and
      ((goal.state == "draft" and claim_planning_task?(task)) or
         (goal.state == "active" and task.purpose != "plan"))
  end

  defp claim_planning_task?(%Task{purpose: "plan", work_item_id: nil, validation_of_task_id: nil}), do: true

  defp claim_planning_task?(_task), do: false

  defp claim_identity_current?(ownership, %Task{} = task, %Run{} = run) do
    task.id == ownership.task_id and run.task_id == task.id and
      task.goal_id == ownership.goal_id and task.work_item_id == ownership.work_item_id and
      task.current_generation == run.generation
  end

  defp notify_claim_authority_routed(opts, ownership) do
    case Keyword.get(opts, :on_goal_claim_routed) do
      hook when is_function(hook, 1) -> hook.(ownership)
      _ -> :ok
    end
  end

  defp claim_work_item_owned?(project_id, goal, nil, %Task{} = task) do
    claim_planning_task?(task) and task.goal_id == goal.id and goal.project_id == project_id
  end

  defp claim_work_item_owned?(project_id, goal, %WorkItem{} = work_item, %Task{} = task) do
    task.goal_id == goal.id and task.work_item_id == work_item.id and
      work_item.project_id == project_id and work_item.goal_id == goal.id and
      work_item.admitted_revision == task.goal_revision
  end

  defp claim_work_item_owned?(_project_id, _goal, _work_item, _task), do: false

  @doc false
  def task_repository_resource_id!(%Task{} = task, nil) do
    if plan_task?(task) do
      task.input
      |> value("subject")
      |> parse_subject!()
      |> Map.fetch!("resource_id")
    else
      rollback(:ownership_lost)
    end
  end

  def task_repository_resource_id!(_task, item) when not is_nil(item), do: item.repository_resource_id

  defp validate_evidence_batch_contract(batch, opts) do
    case validate_contract(:evidence_batch, batch, opts) do
      :ok -> :ok
      {:error, reason} -> {:error, {:invalid_contract, reason}}
    end
  end

  defp validate_evidence_contract!(evidence, opts) do
    case validate_contract(:evidence, evidence, opts) do
      :ok -> :ok
      {:error, :invalid_commit_path} -> rollback({:invalid_contract, :invalid_commit_path})
      {:error, reason} -> rollback(reason)
    end
  end

  defp validate_new_evidence_batch!(run_id, evidence_items, opts) do
    batch = %{
      "schema_version" => "symmetry.evidence_batch.v1",
      "run_id" => run_id,
      "items" => Enum.map(evidence_items, & &1.document)
    }

    case validate_evidence_batch_contract(batch, opts) do
      :ok -> :ok
      {:error, reason} -> rollback(reason)
    end
  end

  defp same_uuid?(_left, nil), do: false

  defp same_uuid?(left, right) when is_binary(left) and is_binary(right) do
    case {Ecto.UUID.dump(left), Ecto.UUID.dump(right)} do
      {{:ok, left}, {:ok, right}} -> left == right
      _ -> false
    end
  end

  defp same_uuid?(_left, _right), do: false

  defp required_sha256_digest(map, key) do
    value = value(map, key)

    if is_binary(value) and Regex.match?(~r/^sha256:[0-9a-f]{64}$/, value) do
      parse_digest(value)
    else
      {:error, :invalid_request}
    end
  end
end
