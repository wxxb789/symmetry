defmodule SymmetryControl.Goals.Settlement do
  @moduledoc false

  import Ecto.Query
  import SymmetryControl.Goals.ContractValidation, only: [validate_contract: 3]

  import SymmetryControl.Goals.EventsReceipts,
    only: [append_event!: 8, nullable_datetime: 1, replay_command: 3]

  import SymmetryControl.Goals.HarnessSessions, only: [mark_harness_session_unavailable!: 2]

  import SymmetryControl.Goals.Internal,
    only: [
      lock_goal!: 1,
      lock_task!: 1,
      lock_work_item!: 1,
      mutation_id: 2,
      normalize_map: 1,
      now: 1,
      only_known_keys?: 2,
      parse_subject: 1,
      plan_action_hash: 2,
      plan_task?: 1,
      required_string: 2,
      required_uuid: 2,
      rollback: 1,
      settlement_mutation_id: 3,
      stamp_insert: 2,
      stamp_update: 2,
      valid_uuid: 1,
      valid_uuid?: 1,
      value: 2
    ]

  alias SymmetryControl.Goals.Budget
  alias SymmetryControl.Goals.Commands
  alias SymmetryControl.Goals.ContextAssembly
  alias SymmetryControl.Goals.ContextSnapshot
  alias SymmetryControl.Goals.Decisions
  alias SymmetryControl.Goals.GoalBudgetReservation
  alias SymmetryControl.Goals.GoalDecision
  alias SymmetryControl.Goals.GoalExternalWait
  alias SymmetryControl.Goals.PlanContracts
  alias SymmetryControl.Goals.Reconciliation
  alias SymmetryControl.Goals.Recovery
  alias SymmetryControl.Goals.RunEvidence
  alias SymmetryControl.Goals.ValidationOutcome
  alias SymmetryControl.Goals.WorkDependency
  alias SymmetryControl.Goals.WorkOutcome
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces.WorkItem

  @terminal_run_states ["completed", "failed", "cancelled"]
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
            goal = lock_goal!(task_locator.goal_id)

            # Keep settlement compatible with commands that take a WorkItem
            # lock: Goal -> WorkItem -> Tasks (sorted) -> Runs (sorted).
            item =
              if task_locator.work_item_id,
                do: lock_work_item!(task_locator.work_item_id)

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
                {:replayed, Map.put(receipt.response, "event_sequence", receipt.event.sequence)}

              :missing ->
                Budget.reconcile_reservation!(goal, task, opts)

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
                    |> Keyword.put(:settlement_next_wake_at, next_wake_at),
                    false
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

  @doc false

  # A terminal Run is durable execution history. A completed validation candidate
  # may derive accepted or rejected work from its frozen Subject and persisted
  # evidence; other typed results remain immutable receipts only.
  def terminal_settlement!(
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
           true <-
             value(task_result, "subject_hash") == ContextAssembly.context_digest(subject_hash),
           {:ok, evidence_refs} <-
             verified_result_evidence_refs(run, subject, subject_hash, task_result),
           {:ok, blocker, blocker_wake_at} <-
             validate_result_blocker(goal, item, task, task_result, opts),
           {:ok, proposed_next_action} <-
             validate_proposed_next_action(goal, item, task, task_result, opts),
           validation_outcome =
             ValidationOutcome.derive_validation_outcome!(
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
             dispatch_result(
               goal,
               task,
               run,
               kind,
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
          subject_hash: ContextAssembly.context_digest(subject_hash),
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

  def terminal_settlement!(
        _goal,
        _item,
        _task,
        %Run{state: "failed"} = run,
        _producer,
        _producing_run,
        opts
      ),
      do: process_terminal_settlement("failed", failed_terminal_reason(run, opts))

  def terminal_settlement!(
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
         true <- value(task_result, :subject_hash) == ContextAssembly.context_digest(subject_hash),
         {:ok, evidence_refs} <-
           verified_result_evidence_refs(run, subject, subject_hash, task_result),
         proposal when is_map(proposal) <- value(task_result, :proposal),
         proposal = Commands.canonical_plan_proposal(proposal),
         true <-
           PlanContracts.valid_plan_proposal?(
             proposal,
             %{goal | current_revision: task.goal_revision},
             opts
           ) do
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
        subject_hash: ContextAssembly.context_digest(subject_hash),
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
      not Decisions.pending_plan_task?(goal, task.id) and
      not Repo.exists?(
        from(item in WorkItem,
          where: item.goal_id == ^goal.id and item.admitted_revision == ^goal.current_revision
        )
      ) and not Decisions.plan_decision_pending_acceptance?(goal)
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
        options: Decisions.derived_decision_options("plan"),
        subject_hash: nil,
        actor_ref: "system:plan_settlement",
        expires_at: nil
      })
      |> stamp_insert(now(opts))

    Decisions.validate_decision_changeset!(decision_changeset, opts)

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

  # Arm order = settlement evaluation order: validation outcome, revision guard, then
  # the TaskResult kind; no catch-all, so an unexpected kind raises instead of settling.
  defp dispatch_result(
         goal,
         task,
         run,
         kind,
         blocker_wake_at,
         subject_hash,
         evidence_refs,
         outcome,
         opts
       ) do
    case {outcome, goal.state, kind} do
      {%WorkOutcome{disposition: "rejected", reason: "historical_result"}, _state, _kind} ->
        {:ok, "historical_result", nil}

      {%WorkOutcome{disposition: "rejected"}, "active", _kind} ->
        {:ok, "validation_failed", nil}

      {%WorkOutcome{disposition: "rejected"}, "paused", _kind} ->
        {:ok, "historical_result", nil}

      {%WorkOutcome{disposition: "accepted"}, "active", _kind} ->
        {:ok, "accepted", :immediate}

      {%WorkOutcome{disposition: "accepted"}, "paused", _kind} ->
        {:ok, "accepted", nil}

      {_outcome, _state, _kind}
      when task.goal_revision != goal.current_revision or goal.state != "active" ->
        {:ok, "historical_result", nil}

      {_outcome, _state, "candidate_completion"} ->
        {:ok, "awaiting_validation", :immediate}

      {_outcome, _state, "progress"} ->
        if progress_has_new_verification?(goal, task, run, subject_hash, evidence_refs, opts),
          do: {:ok, "progress", :immediate},
          else: {:ok, "no_verified_progress", nil}

      {_outcome, _state, "blocked"} ->
        {:ok, "blocked", blocker_wake_at}

      {_outcome, _state, "repair_required"} ->
        {:ok, "repair_required", :immediate}

      {_outcome, _state, "replan_required"} ->
        {:ok, "replan_required", nil}

      {_outcome, _state, "failed"} ->
        {:ok, "failed", nil}
    end
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
             Reconciliation.automatic_admission_mutation_id(
               goal,
               item,
               "observe",
               external_wait_identity(wait)
             ),
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
               value(row.payload, "subject_hash") == ContextAssembly.context_digest(subject_hash) and
               value(row.source_ref, "subject_hash") ==
                 ContextAssembly.context_digest(subject_hash)
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
               value(previous_result, "subject_hash") ==
                 ContextAssembly.context_digest(previous_subject_hash),
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

  @doc false

  def terminal_next_wake_at(goal, task, terminal, opts) do
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

    if next_wake_at == current do
      current
    else
      earliest_future_wake(next_wake_at, external_wait_wake_at, current)
    end
  end

  @doc false

  def external_wait_wake_at(_goal, _current), do: nil

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
      goal = lock_goal!(untrusted_task.goal_id || rollback(:not_found))
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
          item = if untrusted_task.work_item_id, do: lock_work_item!(untrusted_task.work_item_id)
          task = lock_task!(task_id)

          unless settlement_task_owned_by_goal?(task, goal, item) and task.state == "cancelled" and
                   not Repo.exists?(from(run in Run, where: run.task_id == ^task.id)) do
            rollback(:invalid_unstarted_settlement)
          end

          unless Recovery.recoverable_historical_unstarted_cancellation?(goal, task.id),
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
              Keyword.put(opts, :event_revision, task.goal_revision),
              false
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

  @doc false

  def lock_settlement_tasks!(task_id, validation_of_task_id) do
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

  @doc false

  def lock_settlement_runs!(run_id, task_id, producer) do
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

  @doc false
  def settlement_task_owned_by_goal?(task, goal, nil) do
    plan_task?(task) and task.goal_id == goal.id
  end

  def settlement_task_owned_by_goal?(task, goal, item) do
    task.goal_id == goal.id and task.work_item_id == item.id and item.goal_id == goal.id
  end

  defp unstarted_settlement_mutation_id(task_id, reason) do
    mutation_id("unstarted-settlement", [task_id, reason])
  end

  @doc false

  def external_wait_identity(wait) do
    "external:" <>
      wait.id <>
      ":" <>
      wait.task_id <>
      ":" <> wait.run_id <> ":" <> wait.result_id <> ":" <> Integer.to_string(wait.check_seq)
  end
end
