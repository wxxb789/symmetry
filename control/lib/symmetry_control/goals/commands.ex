defmodule SymmetryControl.Goals.Commands do
  @moduledoc false

  import Ecto.Query
  import SymmetryControl.Goals.ContractValidation, only: [validate_contract: 3]

  import SymmetryControl.Goals.EventsReceipts,
    only: [
      append_event!: 8,
      enqueue_goal_control!: 2,
      ensure_preconditions!: 2,
      receipt!: 2,
      replay_command: 3,
      replay_command_after_conflict: 3,
      replay_create: 2,
      replay_event: 2,
      response_for: 2
    ]

  import SymmetryControl.Goals.Internal,
    only: [
      active_task_for_item?: 1,
      admission_enabled?: 1,
      current_revision!: 1,
      ensure_project_active!: 1,
      goal_project_id!: 1,
      lock_command_goal!: 2,
      lock_decision!: 1,
      lock_project!: 1,
      lock_repository_resource!: 1,
      lock_work_item!: 1,
      lock_work_items!: 1,
      normalize_map: 1,
      now: 1,
      nullable_microusd: 2,
      only_known_keys?: 2,
      optional_list: 3,
      optional_uuid!: 2,
      parse_digest: 1,
      parse_subject!: 1,
      plan_action_hash: 2,
      required_list!: 2,
      required_map!: 2,
      required_map: 2,
      required_safe_positive_integer: 2,
      required_string!: 2,
      required_string: 2,
      required_uuid!: 2,
      required_uuid: 2,
      rollback: 1,
      stamp_insert: 2,
      stamp_update: 2,
      unwrap!: 1,
      valid_uuid: 1,
      value: 2,
      value: 3
    ]

  alias Ecto.Changeset
  alias SymmetryControl.Goals.Admission
  alias SymmetryControl.Goals.Budget
  alias SymmetryControl.Goals.ContextAssembly
  alias SymmetryControl.Goals.Decisions
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.GoalControl
  alias SymmetryControl.Goals.GoalDecision
  alias SymmetryControl.Goals.GoalEvent
  alias SymmetryControl.Goals.GoalRevision
  alias SymmetryControl.Goals.PlanContracts
  alias SymmetryControl.Goals.Recovery
  alias SymmetryControl.Goals.WorkDependency
  alias SymmetryControl.Goals.Workers.WakeupWorker
  alias SymmetryControl.Orchestration.Scheduler
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces.WorkItem

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
  @task_purposes ["implement", "validate", "plan", "observe", "chat"]
  @default_max_parallel_tasks 1
  @default_max_run_attempts 2
  @max_safe_integer 9_007_199_254_740_991
  @max_microusd 9_223_372_036_854_775_807

  def create_goal(project_id, attrs, actor_ref, opts \\ [])

  def create_goal(project_id, attrs, actor_ref, opts)
      when is_binary(project_id) and is_map(attrs) and is_binary(actor_ref) and is_list(opts) do
    attrs = normalize_map(attrs)

    with :ok <- valid_uuid(project_id),
         :ok <- valid_actor(actor_ref),
         {:ok, %{mutation_id: mutation_id, title: title, initial_revision: initial_revision}} <-
           parse_create_envelope(attrs) do
      body = %{project_id: project_id, title: title, initial_revision: initial_revision}

      case replay_create(mutation_id, body) do
        {:ok, receipt} ->
          {:ok, receipt, :replayed}

        :missing ->
          with :ok <- validate_goal_contract(:goal_create, attrs, opts),
               :ok <- valid_initial_revision(initial_revision, opts) do
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

  def command(goal_id, command, actor_ref, opts \\ [])

  def command(goal_id, command, actor_ref, opts)
      when is_binary(goal_id) and is_map(command) and is_binary(actor_ref) and is_list(opts) do
    command = normalize_map(command)

    with :ok <- valid_uuid(goal_id),
         :ok <- valid_actor(actor_ref),
         {:ok, parsed} <- parse_command(command, opts) do
      body = Map.take(parsed, [:expected_version, :expected_revision, :kind, :payload])

      fn ->
        case replay_command(goal_id, parsed.mutation_id, body) do
          {:ok, receipt} -> {:replayed, receipt}
          :missing -> execute_command(goal_id, parsed, actor_ref, body, opts)
          {:error, reason} -> rollback(reason)
        end
      end
      |> Repo.transaction()
      |> case do
        {:ok, {:created, receipt}} ->
          {:ok, receipt, :created}

        {:ok, {:replayed, receipt}} ->
          {:ok, receipt, :replayed}

        {:error, :idempotency_conflict} ->
          case replay_command_after_conflict(goal_id, parsed.mutation_id, body) do
            {:ok, receipt} -> {:ok, receipt, :replayed}
            {:error, :internal_error} -> {:error, :internal_error}
            _ -> {:error, :idempotency_conflict}
          end

        {:error, reason} ->
          {:error, reason}
      end
      |> maybe_wake_command(parsed.kind)
    end
  end

  def command(_, _, _, _), do: {:error, :invalid_request}

  defp maybe_wake_command({:ok, _receipt, _disposition} = result, kind) when kind in ["request_plan", "admit_task"] do
    if !Repo.in_transaction?(), do: Scheduler.wake()
    result
  end

  defp maybe_wake_command(result, _kind), do: result

  defp policy_microusd!(nil), do: nil

  defp policy_microusd!(value) when is_binary(value) do
    case nullable_microusd(%{"value" => value}, "value") do
      {:ok, parsed} -> parsed
      {:error, reason} -> rollback(reason)
    end
  end

  defp policy_microusd!(value) when is_integer(value) and value >= 0 and value <= @max_microusd, do: value

  defp policy_microusd!(_value), do: rollback(:invalid_request)

  defp create_new_goal(project_id, title, initial_revision, mutation_id, actor_ref, body, opts) do
    current = now(opts)

    fn ->
      project = lock_project!(project_id)

      case Repo.one(from(event in GoalEvent, where: event.id == ^mutation_id, lock: "FOR UPDATE")) do
        nil ->
          ensure_project_active!(project)
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
            "version" => goal.operator_version + 1,
            "current_revision" => revision.revision
          }

          event =
            append_event!(
              goal,
              mutation_id,
              "goal_created",
              actor_ref,
              body,
              response,
              opts,
              true
            )

          goal = Repo.get!(Goal, goal.id)
          {:created, receipt!(goal, event)}

        event ->
          case replay_event(event, body) do
            {:ok, receipt} -> {:replayed, receipt}
            {:error, reason} -> rollback(reason)
          end
      end
    end
    |> Repo.transaction()
    |> case do
      {:ok, {:created, receipt}} ->
        {:ok, receipt, :created}

      {:ok, {:replayed, receipt}} ->
        {:ok, receipt, :replayed}

      {:error, :idempotency_conflict} ->
        case replay_create(mutation_id, body) do
          {:ok, receipt} -> {:ok, receipt, :replayed}
          {:error, :internal_error} -> {:error, :internal_error}
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
        project = goal_id |> goal_project_id!() |> lock_project!()

        case replay_command(goal_id, parsed.mutation_id, body) do
          {:ok, receipt} ->
            {:replayed, receipt}

          :missing ->
            ensure_project_active!(project)
            goal = lock_command_goal!(goal_id, parsed.mutation_id)

            case replay_command(goal.id, parsed.mutation_id, body) do
              {:ok, receipt} ->
                {:replayed, receipt}

              :missing ->
                case validate_goal_contract(:goal_command, parsed.document, opts) do
                  :ok -> :ok
                  {:error, reason} -> rollback(reason)
                end

                ensure_preconditions!(goal, parsed)

                {goal, response} = apply_command!(goal, parsed, actor_ref, opts)

                if parsed.kind in ["pause", "resume", "cancel", "amend"] do
                  GoalControl.reject_pending_goal_control_commands!(goal.id, now(opts))
                end

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
                    opts,
                    true
                  )

                if parsed.kind in ["pause", "cancel", "amend"],
                  do: enqueue_goal_control!(goal, event)

                if parsed.kind in ["request_plan", "admit_task"] do
                  %{"goal_id" => goal.id}
                  |> WakeupWorker.new(unique: false)
                  |> Oban.insert!()
                end

                goal = Repo.get!(Goal, goal.id)
                {:created, receipt!(goal, event)}

              {:error, reason} ->
                rollback(reason)
            end

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

    if !Repo.exists?(
         from(item in WorkItem,
           where: item.goal_id == ^goal.id and item.admitted_revision == ^goal.current_revision
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

    if goal.state != "paused" or Decisions.unresolved_decisions?(goal.id, goal.current_revision),
      do: rollback(:invalid_transition)

    Recovery.ensure_amendment_controls_settled!(goal)
    Budget.ensure_resume_budget!(goal, current_revision!(goal))
    transition_goal!(goal, "active")
  end

  defp apply_command!(goal, %{kind: "cancel", payload: payload}, _actor_ref, _opts) do
    require_reason!(payload)
    if goal.state not in ["draft", "active", "paused"], do: rollback(:invalid_transition)
    transition_goal!(goal, "cancelled")
  end

  defp apply_command!(goal, %{kind: "amend", payload: payload}, actor_ref, opts) do
    if goal.state not in ["draft", "active", "paused"], do: rollback(:invalid_transition)

    reason = required_string!(payload, :reason)

    revision_contract =
      payload
      |> required_map!(:revision_contract)
      |> Map.put("reason", reason)

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
  defp apply_command!(goal, %{kind: "request_plan", payload: payload, mutation_id: mutation_id}, _actor_ref, opts) do
    if !admission_enabled?(opts), do: rollback(:goal_admission_disabled)
    if goal.state != "draft", do: rollback(:invalid_transition)

    model_profile = required_string!(payload, :model_profile)
    repository_resource_id = required_uuid!(payload, :repository_resource_id)
    subject = payload |> required_map!(:subject) |> parse_subject!()
    resource = lock_repository_resource!(repository_resource_id)

    if !(resource.project_id == goal.project_id and
           Admission.resource_allowed?(
             current_revision!(goal).execution_policy,
             repository_resource_id
           ) and
           subject["resource_id"] == repository_resource_id) do
      rollback(:resource_not_allowed)
    end

    if Repo.exists?(
         from(item in WorkItem,
           where: item.goal_id == ^goal.id and item.admitted_revision == ^goal.current_revision
         )
       ) do
      rollback(:invalid_plan)
    end

    if Decisions.pending_plan_task?(goal) or Decisions.plan_decision_pending_acceptance?(goal),
      do: rollback(:plan_pending)

    task =
      Admission.admit_plan_task!(
        goal,
        resource,
        model_profile,
        subject,
        payload,
        mutation_id,
        opts
      )

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
    if !admission_enabled?(opts), do: rollback(:goal_admission_disabled)
    if goal.state not in ["draft", "active", "paused"], do: rollback(:invalid_transition)
    proposal = required_map!(payload, :proposal)
    proposal_hash = required_digest!(payload, :proposal_hash)
    decision_id = required_uuid!(payload, :decision_id)

    if !PlanContracts.valid_plan_proposal?(proposal, goal, opts), do: rollback(:invalid_plan)
    if RequestHash.canonical(proposal) != proposal_hash, do: rollback(:invalid_request)
    decision = lock_decision!(decision_id)

    if !(decision.goal_id == goal.id and decision.goal_revision == goal.current_revision and
           decision.kind == "plan" and
           decision.state == "resolved" and
           is_nil(decision.work_item_id) and
           value(decision.resolution || %{}, "option_id") == "accept" and
           decision.action_hash == plan_action_hash(goal, proposal_hash)) do
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

    inserted = Admission.admit_plan_items!(goal, items, opts)

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
      Decisions.decision_request!(
        goal,
        kind,
        work_item_id,
        subject_hash,
        value(payload, :proposal),
        opts
      )

    options = Decisions.derived_decision_options(kind)

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

    Decisions.validate_decision_changeset!(decision_changeset, opts)

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
         action_hash: ContextAssembly.context_digest(action_hash)
       }
     })}
  end

  defp apply_command!(goal, %{kind: "resolve_decision", payload: payload}, actor_ref, opts) do
    if goal.state in ["achieved", "cancelled"], do: rollback(:invalid_transition)
    decision_id = required_uuid!(payload, :decision_id)
    expected_version = required_safe_positive_integer!(payload, :expected_decision_version)
    option_id = required_string!(payload, :option_id)
    decision = lock_decision!(decision_id)

    if !(decision.goal_id == goal.id and decision.goal_revision == goal.current_revision and
           decision.state == "open" and
           decision.lock_version == expected_version and
           Decisions.decision_option?(decision, option_id)) do
      rollback(:invalid_decision)
    end

    if decision.expires_at && DateTime.before?(decision.expires_at, now(opts)),
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

    Decisions.validate_decision_changeset!(decision_changeset, opts)
    decision = Repo.update!(decision_changeset)

    {goal,
     response_for(goal, %{
       decision: %{id: decision.id, state: decision.state, version: decision.lock_version}
     })}
  end

  defp apply_command!(goal, %{kind: "admit_task", payload: payload, mutation_id: mutation_id}, _actor_ref, opts) do
    if !admission_enabled?(opts), do: rollback(:goal_admission_disabled)
    if goal.state != "active", do: rollback(:invalid_transition)
    work_item_id = required_uuid!(payload, :work_item_id)
    purpose = required_string!(payload, :purpose)
    model_profile = required_string!(payload, :model_profile)
    if purpose not in @task_purposes, do: rollback(:invalid_request)
    item = lock_work_item!(work_item_id)
    Admission.ensure_admissible_item!(goal, item)
    Admission.ensure_unaccepted_item!(goal, item)

    server_payload =
      Admission.manual_admission_payload!(goal, item, purpose, payload, mutation_id, opts)

    task =
      Admission.admit_authorized_task!(goal, item, purpose, model_profile, server_payload, opts)

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
    [item, dependency] = lock_work_items!([work_item_id, depends_on_id])
    Admission.ensure_admissible_item!(goal, item)
    Admission.ensure_admissible_item!(goal, dependency)
    if active_task_for_item?(item.id), do: rollback(:active_task)
    Decisions.ensure_resolved_scope_decision!(goal, item, decision_id, kind, dependency.id)

    case kind do
      "add_dependency" ->
        if Admission.creates_cycle?(goal.id, item.id, dependency.id),
          do: rollback(:dependency_cycle)

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
    Admission.ensure_achievable!(goal, payload)
    transition_goal!(goal, "achieved", opts)
  end

  defp apply_command!(_goal, _parsed, _actor_ref, _opts), do: rollback(:invalid_request)

  defp transition_goal!(goal, state, opts \\ []) do
    updated =
      goal
      |> Goal.transition_changeset(%{state: state, next_wake_at: nil})
      |> stamp_update(now(opts))
      |> Repo.update!()

    {updated, response_for(updated, %{transition: state})}
  end

  defp valid_initial_revision(revision, opts) when is_map(revision) and is_list(opts) do
    with {:ok, _} <- required_string(revision, :objective),
         {:ok, non_goals} <- optional_list(revision, :non_goals, []),
         true <- Enum.all?(non_goals, &(is_binary(&1) and String.trim(&1) != "")),
         {:ok, acceptance} <- required_map(revision, :acceptance_contract),
         true <- is_map(value(revision, :authority_policy, %{})),
         true <- is_map(value(revision, :execution_policy, %{})),
         true <- is_map(value(revision, :context_manifest, %{})),
         {:ok, _} <- required_string(revision, :reason),
         true <- PlanContracts.valid_acceptance_contract?(acceptance),
         :ok <- PlanContracts.immutable_acceptance_contracts_valid?([acceptance], opts) do
      :ok
    else
      {:error, reason} -> {:error, reason}
      _ -> {:error, :invalid_request}
    end
  end

  defp valid_initial_revision(_, _), do: {:error, :invalid_request}

  defp revision_attrs(goal_id, revision, contract, actor_ref) do
    %{
      goal_id: goal_id,
      revision: revision,
      objective: required_string!(contract, :objective),
      non_goals: value(contract, :non_goals, []),
      acceptance_contract: required_map!(contract, :acceptance_contract),
      authority_policy: ContextAssembly.canonical_authority_policy(value(contract, :authority_policy, %{})),
      execution_policy: normalized_policy(value(contract, :execution_policy, %{})),
      context_manifest: ContextAssembly.canonical_context_manifest(value(contract, :context_manifest, %{})),
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

  defp parse_create_envelope(attrs) do
    with true <-
           only_known_keys?(attrs, ["schema_version", "mutation_id", "title", "initial_revision"]),
         "symmetry.goal_create.v1" <- value(attrs, :schema_version),
         {:ok, mutation_id} <- required_uuid(attrs, :mutation_id),
         {:ok, title} <- required_string(attrs, :title),
         {:ok, initial_revision} <- required_map(attrs, :initial_revision) do
      {:ok, %{mutation_id: mutation_id, title: title, initial_revision: initial_revision}}
    else
      _ -> {:error, :invalid_request}
    end
  end

  defp parse_command(command, opts) do
    allowed = [
      "schema_version",
      "mutation_id",
      "expected_version",
      "expected_revision",
      "kind",
      "payload"
    ]

    cond do
      not only_known_keys?(command, allowed) ->
        {:error, :invalid_request}

      match?({:ok, kind} when kind in @command_kinds, required_string(command, :kind)) ->
        parse_known_command_envelope(command, opts)

      true ->
        {:error, :invalid_request}
    end
  end

  defp parse_known_command_envelope(command, opts) do
    with "symmetry.goal_command.v1" <- value(command, :schema_version),
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
         payload: payload,
         document: command
       }}
    else
      _ ->
        case validate_goal_contract(:goal_command, command, opts) do
          :ok -> {:error, :invalid_request}
          {:error, reason} -> {:error, reason}
        end
    end
  end

  defp valid_command_payload?("activate", payload), do: only_known_keys?(payload, ["approved_revision"])

  defp valid_command_payload?(kind, payload) when kind in ["pause", "resume", "cancel"],
    do: only_known_keys?(payload, ["reason"])

  defp valid_command_payload?("amend", payload), do: only_known_keys?(payload, ["revision_contract", "reason"])

  defp valid_command_payload?("request_plan", payload),
    do:
      only_known_keys?(payload, [
        "model_profile",
        "repository_resource_id",
        "subject",
        "session_mode",
        "requested_session_id",
        "handoff_source_run_id"
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
    do: only_known_keys?(payload, ["decision_id", "expected_decision_version", "option_id", "comment"])

  defp valid_command_payload?("admit_task", payload),
    do:
      only_known_keys?(payload, [
        "work_item_id",
        "purpose",
        "model_profile",
        "session_mode",
        "requested_session_id",
        "handoff_source_run_id",
        "validation_of_task_id"
      ])

  defp valid_command_payload?(kind, payload) when kind in ["add_dependency", "remove_dependency"],
    do: only_known_keys?(payload, ["work_item_id", "depends_on_id", "decision_id"])

  defp valid_command_payload?("achieve", payload),
    do: only_known_keys?(payload, ["subject", "integration_work_item_id", "evidence_ids", "decision_id"])

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

  @doc false

  def canonical_plan_proposal(proposal) when is_map(proposal) do
    proposal = normalize_map(proposal)

    Map.update(proposal, "items", [], fn items ->
      if is_list(items),
        do:
          Enum.map(items, fn item ->
            if is_map(item) do
              item
              |> normalize_map()
              |> Map.put_new("integration", false)
              |> Map.put_new("change_target", nil)
            else
              item
            end
          end),
        else: items
    end)
  end

  def canonical_plan_proposal(proposal), do: proposal

  defp validate_goal_contract(kind, document, opts) do
    case validate_contract(kind, document, opts) do
      :ok -> :ok
      {:error, reason} -> {:error, {:invalid_contract, reason}}
    end
  end

  defp require_reason!(payload), do: required_string!(payload, :reason)

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

  defp required_safe_positive_integer!(map, key), do: map |> required_safe_positive_integer(key) |> unwrap!()

  defp required_digest!(map, key), do: map |> value(key) |> digest!()

  defp optional_digest!(map, key), do: map |> value(key) |> optional_digest!()

  defp optional_digest!(nil), do: nil
  defp optional_digest!(value), do: digest!(value)

  defp digest!(value) when is_binary(value) and byte_size(value) == 32, do: value

  defp digest!(value) when is_binary(value), do: value |> parse_digest() |> unwrap!()

  defp digest!(_), do: rollback(:invalid_request)

  defp validate_execution_policy!(policy) do
    for key <- ["max_parallel_tasks", "max_task_admissions", "max_run_attempts_per_task"] do
      value = value(policy, key)

      if !(is_integer(value) and value > 0 and value <= @max_safe_integer) do
        rollback(:invalid_request)
      end
    end

    budget_limit = value(policy, "budget_limit_microusd")

    if !(is_nil(budget_limit) or
           (is_integer(budget_limit) and budget_limit >= 0 and budget_limit <= @max_microusd)) do
      rollback(:invalid_request)
    end

    per_run_cost_limit = value(policy, "per_run_cost_limit_microusd")

    if !(is_nil(per_run_cost_limit) or
           (is_integer(per_run_cost_limit) and per_run_cost_limit >= 0 and
              per_run_cost_limit <= @max_microusd)) do
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

    if !is_boolean(value(policy, "automatic_execution")) do
      rollback(:invalid_request)
    end

    if value(policy, "automatic_execution") == true and is_nil(budget_limit) do
      rollback(:automatic_budget_limit_required)
    end
  end

  defp valid_actor(actor_ref),
    do: if(is_binary(actor_ref) and String.trim(actor_ref) != "", do: :ok, else: {:error, :invalid_request})

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
end
