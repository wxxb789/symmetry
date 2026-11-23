defmodule SymmetryControl.Goals.Reconciliation do
  @moduledoc false

  import Ecto.Query
  import SymmetryControl.Goals.ContractValidation, only: [validate_contract: 3]
  import SymmetryControl.Goals.EventsReceipts, only: [append_event!: 8, enqueue_wakeup!: 2]

  import SymmetryControl.Goals.Internal,
    only: [
      active_task_for_item?: 1,
      admission_enabled?: 1,
      allowed_values: 2,
      current_revision!: 1,
      is_short_identifier?: 1,
      lock_external_wait!: 1,
      lock_goal!: 1,
      lock_task!: 1,
      lock_work_item!: 1,
      mutation_id: 2,
      now: 1,
      parse_subject: 1,
      policy_integer: 2,
      policy_integer: 3,
      required_uuid: 2,
      stamp_update: 2,
      try_lock_active_goal_project: 1,
      valid_uuid?: 1,
      value: 2,
      value: 3
    ]

  alias SymmetryControl.Goals.Admission
  alias SymmetryControl.Goals.Budget
  alias SymmetryControl.Goals.ContextAssembly
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.GoalEvent
  alias SymmetryControl.Goals.GoalExternalWait
  alias SymmetryControl.Goals.Settlement
  alias SymmetryControl.Goals.WorkDependency
  alias SymmetryControl.Goals.WorkOutcome
  alias SymmetryControl.Orchestration.Run
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
  @automatic_terminal_settlements [
    "failed",
    "replan_required",
    "no_verified_progress",
    "validation_failed",
    "invalid_task_result",
    "missing_result"
  ]
  @default_max_parallel_tasks 1
  @automatic_reconciliation_backoff_seconds 60

  def reconcile_due_goals(opts \\ [])

  def reconcile_due_goals(opts) when is_list(opts) do
    current = now(opts)
    requested_goal_ids = Keyword.get(opts, :goal_ids)

    if is_nil(requested_goal_ids) or
         (is_list(requested_goal_ids) and Enum.all?(requested_goal_ids, &valid_uuid?/1)) do
      reconcile_due_goal_rows(current, requested_goal_ids, opts)
    else
      {:error, :invalid_request}
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
        next_wake_at = Settlement.external_wait_wake_at(goal, current)

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
                    {:deferred, "waiting_for_admission_condition", "goal:waiting_for_admission_condition"}

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

  defp attempt_automatic_admission!(goal, current, opts, item, purpose, model_profile, payload, source_identity) do
    task = Admission.admit_authorized_task!(goal, item, purpose, model_profile, payload, opts)
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
        Keyword.merge(opts, now: current, settlement_next_wake_at: current),
        false
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

    Enum.find(automatic_admission_candidates(goal, opts), fn {item, purpose, _profile, _payload, source_identity} ->
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
    goal
    |> current_work_items()
    |> Enum.flat_map(fn item ->
      if active_task_for_item?(item.id) do
        []
      else
        from(task in Task,
          where:
            task.goal_id == ^goal.id and task.goal_revision == ^goal.current_revision and
              task.work_item_id == ^item.id and task.purpose != "validate" and
              task.state == "completed" and task.current_generation > 0,
          order_by: [asc: task.inserted_at, asc: task.id]
        )
        |> Repo.all()
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
    goal
    |> current_work_items()
    |> Enum.flat_map(fn item ->
      if active_task_for_item?(item.id) or accepted_work_item?(goal, item.id) do
        []
      else
        from(task in Task,
          where:
            task.goal_id == ^goal.id and task.goal_revision == ^goal.current_revision and
              task.work_item_id == ^item.id and task.purpose != "validate" and
              task.state == "completed" and task.current_generation > 0,
          order_by: [desc: task.inserted_at, desc: task.id]
        )
        |> Repo.all()
        |> Enum.flat_map(fn task ->
          if settled_as?(goal, task, "repair_required"),
            do: [{:repair, item.id, task.id}],
            else: []
        end)
      end
    end)
  end

  defp automatic_progress_candidates(goal) do
    goal
    |> current_work_items()
    |> Enum.flat_map(fn item ->
      if active_task_for_item?(item.id) or accepted_work_item?(goal, item.id) do
        []
      else
        from(task in Task,
          where:
            task.goal_id == ^goal.id and task.goal_revision == ^goal.current_revision and
              task.work_item_id == ^item.id and task.purpose in ["implement", "observe"] and
              task.state == "completed" and task.current_generation > 0,
          order_by: [desc: task.inserted_at, desc: task.id]
        )
        |> Repo.all()
        |> Enum.flat_map(fn task ->
          if settled_as?(goal, task, "progress"), do: [{:implement, item.id, task.id}], else: []
        end)
      end
    end)
  end

  defp automatic_baseline_candidates(goal) do
    goal
    |> current_work_items()
    |> Enum.filter(fn item ->
      baseline_source_present?(item) and
        not active_task_for_item?(item.id) and
        not current_revision_task?(goal, item.id) and
        not accepted_work_item?(goal, item.id)
    end)
    |> Enum.map(fn item -> {:implement, item.id, :baseline} end)
  end

  defp automatic_admission_payload(goal, {:validate, work_item_id, producer_id}, _opts) do
    item = lock_work_item!(work_item_id)
    producer = lock_task!(producer_id)
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
    wait = lock_external_wait!(wait_id)
    item = lock_work_item!(wait.work_item_id)
    source_task = lock_task!(wait.task_id)

    with true <- wait.goal_id == goal.id,
         true <- wait.goal_revision == goal.current_revision,
         true <- wait.state in GoalExternalWait.active_states(),
         true <- source_task.goal_id == goal.id,
         true <- source_task.goal_revision == goal.current_revision,
         true <- source_task.work_item_id == item.id,
         true <- is_short_identifier?(source_task.agent_profile) do
      source_identity = Settlement.external_wait_identity(wait)

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
    item = lock_work_item!(work_item_id)
    source_task = lock_task!(source_task_id)

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
    item = lock_work_item!(work_item_id)
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
    item = lock_work_item!(work_item_id)
    source_task = lock_task!(source_task_id)

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

  @doc false

  def automatic_progress_subject(goal, item, source_task, opts) do
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
         true <-
           value(task_result, "subject_hash") == ContextAssembly.context_digest(subject_hash),
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

  @doc false

  def automatic_baseline_subject(goal, item) do
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
          {:ok, parsed, "baseline:dependency:" <> item.baseline_dependency_id <> ":" <> outcome.id}
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
            ], do: {:ok, Atom.to_string(reason)}

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
    with {:ok, amount} <- Budget.server_reservation_amount(revision) do
      Budget.budget_status(goal, revision, amount)
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
    next_wake_at = DateTime.shift(current, second: @automatic_reconciliation_backoff_seconds)
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
              Keyword.merge(opts, now: current, settlement_next_wake_at: next_wake_at),
              false
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

  @doc false

  def accepted_work_item?(goal, work_item_id) do
    Repo.exists?(
      from(outcome in WorkOutcome,
        where:
          outcome.goal_id == ^goal.id and outcome.goal_revision == ^goal.current_revision and
            outcome.work_item_id == ^work_item_id and outcome.disposition == "accepted"
      )
    )
  end

  defp awaiting_validation_settlement?(goal, task), do: settled_as?(goal, task, "awaiting_validation")

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

  defp reconcile_due_goal_rows(current, requested_goal_ids, opts) do
    limit = Keyword.get(opts, :limit, 100)

    if is_integer(limit) and limit > 0 do
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
    else
      {:error, :invalid_request}
    end
  end

  # Each admission candidate commits in its own Goal transaction. A durable
  # business blocker therefore survives a later candidate's retryable failure.
  defp reconcile_due_goal(goal_id, current, opts), do: reconcile_due_goal(goal_id, current, opts, true, [])

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
           %Goal{} = goal <- lock_goal!(goal_id),
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
           %Goal{} = goal <- lock_goal!(goal_id),
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

  defp append_automatic_admission_blocker!(goal, current, opts, item, purpose, source_identity, blocker_reason) do
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
        Keyword.merge(opts, now: current, settlement_next_wake_at: current),
        false
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
        Keyword.merge(opts, now: current, settlement_next_wake_at: current),
        false
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
           %Goal{} = goal <- lock_goal!(goal_id),
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
           %Goal{} = goal <- lock_goal!(goal_id) do
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
        end
      else
        _ -> nil
      end
    end)
  end

  @doc false

  def automatic_admission_mutation_id(goal, item, purpose, source_identity) do
    mutation_id("automatic-admission", [
      goal.id,
      Integer.to_string(goal.current_revision),
      item.id,
      purpose,
      source_identity
    ])
  end

  defp automatic_admission_blocked_mutation_id(goal, candidate_identity) do
    mutation_id("automatic-admission-blocked", [
      goal.id,
      Integer.to_string(goal.current_revision),
      candidate_identity
    ])
  end

  defp automatic_reconciliation_deferred_mutation_id(goal, deferred_identity, reason) do
    mutation_id("automatic-reconciliation-deferred", [
      goal.id,
      Integer.to_string(goal.current_revision),
      deferred_identity,
      reason
    ])
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
end
