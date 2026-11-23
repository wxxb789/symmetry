defmodule SymmetryControl.Goals.ValidationOutcome do
  @moduledoc false

  import Ecto.Query
  import SymmetryControl.Goals.ContractValidation, only: [validate_contract: 3]

  import SymmetryControl.Goals.EventsReceipts,
    only: [append_event!: 8, nullable_datetime: 1, replay_command: 3]

  import SymmetryControl.Goals.Internal,
    only: [
      lock_goal!: 1,
      lock_work_item!: 1,
      mutation_id: 2,
      now: 1,
      parse_subject: 1,
      required_uuid: 2,
      rollback: 1,
      settlement_mutation_id: 3,
      stamp_insert: 2,
      valid_uuid: 1,
      value: 2,
      value: 3,
      work_item_acceptance_action_hash: 3
    ]

  alias SymmetryControl.Goals.Admission
  alias SymmetryControl.Goals.ContextAssembly
  alias SymmetryControl.Goals.ContextSnapshot
  alias SymmetryControl.Goals.GoalDecision
  alias SymmetryControl.Goals.GoalEvent
  alias SymmetryControl.Goals.RunEvidence
  alias SymmetryControl.Goals.Settlement
  alias SymmetryControl.Goals.WorkOutcome
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash

  @terminal_run_states ["completed", "failed", "cancelled"]

  def reconcile_validation_outcome(task_id, run_id, generation, opts \\ [])

  def reconcile_validation_outcome(task_id, run_id, generation, opts)
      when is_binary(task_id) and is_binary(run_id) and is_integer(generation) and generation >= 0 and is_list(opts) do
    with :ok <- valid_uuid(task_id), :ok <- valid_uuid(run_id) do
      fn ->
        task_locator = Repo.get(Task, task_id)

        if is_nil(task_locator) or is_nil(task_locator.goal_id) do
          %{task_id: task_id, run_id: run_id, generation: generation}
        else
          goal = lock_goal!(task_locator.goal_id)

          item =
            if task_locator.work_item_id,
              do: lock_work_item!(task_locator.work_item_id)

          {task, producer} =
            Settlement.lock_settlement_tasks!(task_id, task_locator.validation_of_task_id)

          if !(Settlement.settlement_task_owned_by_goal?(task, goal, item) and
                 task.work_item_id == task_locator.work_item_id and
                 task.validation_of_task_id == task_locator.validation_of_task_id),
             do: rollback(:stale_run)

          {run, producing_run} = Settlement.lock_settlement_runs!(run_id, task_id, producer)

          if is_nil(run) or run.generation != generation or
               run.state not in @terminal_run_states or task.current_generation != generation or
               task.state not in @terminal_run_states do
            rollback(:stale_run)
          end

          original_mutation_id = settlement_mutation_id(task.id, run.id, generation)

          original_body = %{
            task_id: task.id,
            run_id: run.id,
            generation: generation,
            state: run.state,
            goal_revision: task.goal_revision
          }

          original_receipt =
            case replay_command(goal.id, original_mutation_id, original_body) do
              {:ok, receipt} -> receipt
              :missing -> rollback(:validation_reconciliation_not_pending)
              {:error, reason} -> rollback(reason)
            end

          if value(original_receipt.response, "settlement") != "awaiting_validation",
            do: rollback(:validation_reconciliation_not_pending)

          late_mutation_id =
            late_validation_outcome_mutation_id(task.id, run.id, run.generation)

          late_event =
            Repo.one(
              from(event in GoalEvent,
                where: event.id == ^late_mutation_id,
                lock: "FOR UPDATE"
              )
            )

          outcome = existing_validation_outcome(task.id)

          if late_event do
            if !(late_event.kind == "validation_outcome_settled" and outcome),
              do: rollback(:internal_error)

            late_validation_outcome_receipt!(goal, task, run, outcome, opts)
          else
            # Re-run the complete frozen Subject/revision/evidence validation
            # before creating the independent reconciliation receipt. The
            # existing outcome is only sufficient for exact replay.
            Settlement.terminal_settlement!(
              goal,
              item,
              task,
              run,
              producer,
              producing_run,
              opts
            )

            outcome = existing_validation_outcome(task.id)

            if outcome do
              late_validation_outcome_receipt!(goal, task, run, outcome, opts)
            else
              rollback(:validation_pending)
            end
          end
        end
      end
      |> Repo.transaction()
      |> case do
        {:ok, response} -> {:ok, response}
        {:error, reason} -> {:error, reason}
      end
    end
  end

  def reconcile_validation_outcome(_, _, _, _), do: {:error, :invalid_request}

  @doc false

  # Validation outcomes are evidence-driven rather than model-declared. The
  # validator terminal result closes the evidence set; frozen input and
  # persisted evidence identify the exact producer candidate being evaluated.
  def derive_validation_outcome!(
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
         true <- validation_subject_matches_candidate?(goal, item, task, subject, subject_hash) do
      outcome =
        validation_evidence_outcome(
          goal,
          item,
          item.acceptance_contract,
          run.id,
          subject,
          subject_hash
        )

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

  def derive_validation_outcome!(
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
      ), do: nil

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
         true <-
           value(task_result, "subject_hash") == ContextAssembly.context_digest(subject_hash) do
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
         {:ok, context} <- ContextAssembly.context_wire_envelope(snapshot, goal, item, task),
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
        do: resolved_work_item_acceptance_decision(goal, item, subject_hash)

    operator_acceptance_decision_id =
      if operator_acceptance_decision, do: operator_acceptance_decision.id

    evidence =
      from(evidence in RunEvidence,
        where: evidence.run_id == ^validation_run_id and evidence.subject_hash == ^subject_hash,
        lock: "FOR UPDATE"
      )
      |> Repo.all()
      |> Enum.filter(fn evidence ->
        value(evidence.payload, "subject") == subject and
          value(evidence.payload, "subject_hash") == ContextAssembly.context_digest(subject_hash)
      end)

    matching_evidence =
      Enum.filter(evidence, fn evidence ->
        Enum.any?(required_predicates, fn predicate ->
          is_map(predicate) and is_binary(value(predicate, "id")) and
            value(predicate, "kind") != "operator_acceptance" and
            value(evidence.payload, "predicate_id") == value(predicate, "id") and
            Admission.goal_predicate_matches_evidence?(predicate, evidence)
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

    from(decision in GoalDecision,
      where:
        decision.goal_id == ^goal.id and decision.goal_revision == ^goal.current_revision and
          decision.state == "resolved" and decision.kind == "review" and
          decision.work_item_id == ^item.id and decision.subject_hash == ^subject_hash and
          decision.action_hash == ^action_hash,
      select: %{id: decision.id, resolution: decision.resolution}
    )
    |> Repo.one()
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
        wake_at = if(settlement == "accepted", do: :immediate)
        next_wake_at = Settlement.terminal_next_wake_at(goal, task, %{wake_at: wake_at}, opts)

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
            |> Keyword.put(:settlement_next_wake_at, next_wake_at),
            false
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

  defp late_validation_outcome_mutation_id(task_id, run_id, generation) do
    mutation_id("late-validation-outcome", [task_id, run_id, Integer.to_string(generation)])
  end
end
