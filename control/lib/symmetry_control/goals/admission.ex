defmodule SymmetryControl.Goals.Admission do
  @moduledoc false

  import Ecto.Query
  import SymmetryControl.Goals.ContractValidation, only: [validate_contract: 3]

  import SymmetryControl.Goals.Internal,
    only: [
      active_task_for_item?: 1,
      allowed_values: 2,
      current_revision!: 1,
      lock_repository_resources!: 1,
      lock_task!: 1,
      map_has_key?: 2,
      now: 1,
      only_known_keys?: 2,
      optional_uuid!: 2,
      parse_digest: 1,
      parse_subject!: 1,
      parse_subject: 1,
      policy_integer: 2,
      policy_integer: 3,
      required_list!: 2,
      required_map!: 2,
      required_string!: 2,
      required_uuid!: 2,
      rollback: 1,
      stamp_insert: 2,
      stamp_update: 2,
      unwrap!: 1,
      valid_long_text?: 1,
      valid_uuid?: 1,
      value: 2,
      value: 3
    ]

  alias Ecto.Changeset
  alias SymmetryControl.Goals.Budget
  alias SymmetryControl.Goals.ContextAssembly
  alias SymmetryControl.Goals.ContextSnapshot
  alias SymmetryControl.Goals.ContractValidation
  alias SymmetryControl.Goals.Decisions
  alias SymmetryControl.Goals.GoalBudgetReservation
  alias SymmetryControl.Goals.GoalDecision
  alias SymmetryControl.Goals.GoalExternalWait
  alias SymmetryControl.Goals.Handoff
  alias SymmetryControl.Goals.PlanContracts
  alias SymmetryControl.Goals.Reconciliation
  alias SymmetryControl.Goals.RunEvidence
  alias SymmetryControl.Goals.ValidationProfiles
  alias SymmetryControl.Goals.WorkDependency
  alias SymmetryControl.Goals.WorkOutcome
  alias SymmetryControl.Integrations.ChangeAction
  alias SymmetryControl.Integrations.Connection
  alias SymmetryControl.Integrations.Providers.AzureDevOps
  alias SymmetryControl.Integrations.Providers.GitHub
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces.Project
  alias SymmetryControl.Workspaces.ProjectResource
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
  @default_max_parallel_tasks 1
  @default_max_run_attempts 2

  @doc false

  def admit_plan_items!(goal, items, opts) do
    if !PlanContracts.plan_admission_preflight?(items, opts), do: rollback(:invalid_plan)

    keys = Enum.map(items, &required_string!(&1, :key))
    if length(keys) != MapSet.size(MapSet.new(keys)), do: rollback(:invalid_plan)
    item_ids = Map.new(keys, &{&1, Ecto.UUID.generate()})
    derived_key = PlanContracts.derived_integration_key(items)

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
        if !PlanContracts.valid_acceptance_contract?(acceptance), do: rollback(:invalid_plan)

        if !PlanContracts.acceptance_contract_matches_resource?(acceptance, resource_id),
          do: rollback(:resource_not_allowed)

        if resource.project_id != goal.project_id, do: rollback(:resource_not_allowed)
        if !(resources == [] or resource_id in resources), do: rollback(:resource_not_allowed)
        if !(profiles == [] or profile in profiles), do: rollback(:model_not_allowed)

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
          integration: plan_membership_integration!(item, derived_key),
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
    source_branch = PlanContracts.required_untrimmed_string!(target, :source_branch)
    target_branch = PlanContracts.required_untrimmed_string!(target, :target_branch)

    if !(only_known_keys?(target, ["kind", "source_branch", "target_branch"]) and
           source_branch != target_branch and
           {:ok, source_branch} == ChangeAction.normalize_branch(source_branch) and
           {:ok, target_branch} == ChangeAction.normalize_branch(target_branch) and
           provider_operation_allowed?(revision, "change.upsert")) do
      rollback(:invalid_plan)
    end

    %{
      "kind" => "branches",
      "source_branch" => source_branch,
      "target_branch" => target_branch
    }
  end

  defp pull_request_change_target!(target, connection, resource, revision) do
    pull_request_url = PlanContracts.required_untrimmed_string!(target, :pull_request_url)

    if !(only_known_keys?(target, ["kind", "pull_request_url"]) and
           valid_long_text?(pull_request_url) and
           provider_operation_allowed?(revision, "change.update") and
           valid_pull_request_change_target?(connection, resource, pull_request_url)) do
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

    requested_session_id = optional_uuid!(payload, :requested_session_id)
    session_mode = Handoff.admission_session_mode!(payload, requested_session_id)
    Handoff.ensure_requested_session_available!(item, requested_session_id, session_mode)

    handoff_source =
      Handoff.handoff_source!(
        goal,
        item.id,
        item.repository_resource_id,
        payload,
        session_mode,
        opts
      )

    subject =
      case handoff_source do
        nil -> admission_subject!(goal, item, purpose, payload, opts)
        source -> Handoff.handoff_admission_subject!(goal, item, purpose, payload, source, opts)
      end

    limits = Budget.admission_limits!(payload, opts)

    {limits, reservation_amount, required_capabilities} =
      Budget.server_admission_budget!(goal, revision, limits)

    provider_scope = admission_provider_scope!(goal, revision, item, purpose)

    required_capabilities = require_provider_access(required_capabilities, provider_scope)

    validation_bindings =
      admission_validation_bindings!(
        item.acceptance_contract,
        revision.execution_policy,
        purpose,
        opts
      )

    snapshot_id = Ecto.UUID.generate()

    snapshot_document =
      ContextAssembly.context_snapshot_document(
        goal,
        revision,
        item,
        purpose,
        model_profile,
        subject,
        validation_bindings,
        snapshot_id,
        admission_next_action(payload, purpose, model_profile),
        now(opts),
        handoff_source
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

    admission =
      if handoff_source,
        do: Map.put(admission, "handoff_source_run_id", handoff_source.run_id),
        else: admission

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
        allowed_runtime_ids: admission_allowed_runtime_ids!(revision),
        max_run_attempts:
          policy_integer(
            revision.execution_policy,
            "max_run_attempts_per_task",
            @default_max_run_attempts
          ),
        requested_session_id: requested_session_id,
        handoff_source_run_id: handoff_source && handoff_source.run_id
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

  @doc false

  def admit_plan_task!(goal, resource, model_profile, subject, payload, admission_key, opts) do
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
    session_mode = Handoff.admission_session_mode!(payload, requested_session_id)
    Handoff.ensure_plan_requested_session_available!(resource, requested_session_id, session_mode)

    if session_mode == "handoff", do: rollback(:unsupported_capability)

    limits = Budget.admission_limits!(%{}, opts)

    {limits, reservation_amount, required_capabilities} =
      Budget.server_admission_budget!(goal, revision, limits)

    snapshot_id = Ecto.UUID.generate()

    snapshot_document =
      ContextAssembly.planning_context_snapshot_document(
        goal,
        revision,
        resource,
        subject,
        snapshot_id,
        now(opts),
        nil
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
        allowed_runtime_ids: admission_allowed_runtime_ids!(revision),
        max_run_attempts:
          policy_integer(
            revision.execution_policy,
            "max_run_attempts_per_task",
            @default_max_run_attempts
          ),
        requested_session_id: requested_session_id,
        handoff_source_run_id: nil
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

  @doc false

  def admit_authorized_task!(goal, item, purpose, model_profile, payload, opts) do
    ensure_admissible_item!(goal, item)
    ensure_unaccepted_item!(goal, item)
    ensure_dependencies_satisfied!(goal, item)
    ensure_task_policy!(goal, item, purpose, model_profile, payload)
    admit_task!(goal, item, purpose, model_profile, payload, opts)
  end

  @doc false

  def ensure_unaccepted_item!(goal, item) do
    if Reconciliation.accepted_work_item?(goal, item.id), do: rollback(:already_accepted)
  end

  @doc false

  def manual_admission_payload!(goal, item, purpose, payload, mutation_id, opts) do
    reject_caller_derived_admission_fields!(payload)

    case {purpose, value(payload, :session_mode, "fresh")} do
      {"observe", "handoff"} ->
        rollback(:unsupported_capability)

      {_purpose, "handoff"} ->
        Map.put(payload, :admission_key, mutation_id)

      {"observe", _session_mode} ->
        manual_external_observation_payload!(goal, item, payload)

      {"validate", _session_mode} ->
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
    case Reconciliation.automatic_baseline_subject(goal, item) do
      {:ok, subject, _source_identity} ->
        subject

      :none ->
        manual_predecessor_subject!(goal, item, opts)
    end
  end

  defp manual_predecessor_subject!(goal, item, opts) do
    from(task in Task,
      where:
        task.goal_id == ^goal.id and task.goal_revision == ^goal.current_revision and
          task.work_item_id == ^item.id and task.purpose == "implement" and
          task.state == "completed" and task.current_generation > 0,
      order_by: [desc: task.inserted_at, desc: task.id]
    )
    |> Repo.all()
    |> Enum.find_value(fn task ->
      case Reconciliation.automatic_progress_subject(goal, item, task, opts) do
        {:ok, subject, _source_identity} -> subject
        :none -> nil
      end
    end)
    |> case do
      nil -> rollback(:baseline_subject_required)
      subject -> subject
    end
  end

  @doc false

  def ensure_achievable!(goal, payload) do
    revision = current_revision!(goal)
    final_acceptance = ensure_goal_final_acceptance!(revision)
    subject = payload |> required_map!(:subject) |> parse_subject!()
    subject_hash = RequestHash.canonical(subject)
    evidence_ids = required_list!(payload, :evidence_ids)
    integration_work_item_id = optional_uuid!(payload, :integration_work_item_id)

    current_items =
      Repo.all(
        from(item in WorkItem,
          where: item.goal_id == ^goal.id and item.admitted_revision == ^goal.current_revision,
          order_by: [asc: item.id],
          lock: "FOR UPDATE"
        )
      )

    required_items = Enum.filter(current_items, & &1.required)

    integration_item =
      resolve_integration_item!(current_items, required_items, integration_work_item_id)

    current_items
    |> completion_relevant_items(goal, required_items, integration_item)
    |> Enum.each(&ensure_dependencies_satisfied!(goal, &1))

    accepted_outcomes =
      from(outcome in WorkOutcome,
        where:
          outcome.goal_id == ^goal.id and outcome.goal_revision == ^goal.current_revision and
            outcome.disposition == "accepted",
        select: {outcome.work_item_id, outcome.subject_hash}
      )
      |> Repo.all()
      |> Map.new()

    if Enum.any?(required_items, &(not Map.has_key?(accepted_outcomes, &1.id))),
      do: rollback(:incomplete_work)

    if !integration_outcome?(goal, integration_item.id, subject, subject_hash),
      do: rollback(:integration_outcome_required)

    if Decisions.unresolved_decisions?(goal.id, goal.current_revision),
      do: rollback(:unresolved_decision)

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

    ensure_required_evidence!(revision.acceptance_contract, evidence, subject_hash)
  end

  # The designation rule lives in ContractValidation.integration_designation/1.
  defp resolve_integration_item!(current_items, required_items, integration_work_item_id) do
    case required_items do
      [required_item] ->
        designation =
          current_items
          |> Enum.map(fn item ->
            {item.id, item.id == required_item.id, item.integration == true}
          end)
          |> ContractValidation.integration_designation()

        case designation do
          {:conflict} ->
            rollback(:invalid_plan)

          _designation ->
            case integration_work_item_id do
              nil -> required_item
              id when id == required_item.id -> required_item
              _other -> rollback(:integration_outcome_required)
            end
        end

      _other_count ->
        id = integration_work_item_id || rollback(:invalid_request)

        Enum.find(current_items, &(&1.id == id and &1.integration)) ||
          rollback(:integration_outcome_required)
    end
  end

  defp ensure_task_policy!(goal, item, purpose, model_profile, payload) do
    revision = current_revision!(goal)
    profiles = allowed_values(revision.execution_policy, "allowed_model_profiles")
    if profiles != [] and model_profile not in profiles, do: rollback(:model_not_allowed)

    if purpose != "validate" and optional_uuid!(payload, :validation_of_task_id),
      do: rollback(:invalid_request)

    if purpose == "validate" do
      producer_id = optional_uuid!(payload, :validation_of_task_id) || rollback(:invalid_request)
      producer = lock_task!(producer_id)

      if !(producer.goal_id == goal.id and producer.work_item_id == item.id and
             producer.goal_revision == goal.current_revision and
             producer.state == "completed" and producer.purpose != "validate") do
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

  defp constrain_validation_runtime_intersection!(bindings, purpose) when purpose != "validate", do: bindings

  defp constrain_validation_runtime_intersection!([], "validate"), do: []

  defp constrain_validation_runtime_intersection!([first | rest] = bindings, "validate") do
    common_runtime_ids =
      Enum.filter(first["allowed_runtime_ids"], fn runtime_id ->
        Enum.all?(rest, &(runtime_id in &1["allowed_runtime_ids"]))
      end)

    if common_runtime_ids == [], do: rollback(:validation_runtime_not_allowed)

    Enum.map(bindings, &%{&1 | "allowed_runtime_ids" => common_runtime_ids})
  end

  @doc false

  def ensure_admissible_item!(goal, item) do
    if !(item.goal_id == goal.id and item.admitted_revision == goal.current_revision),
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
      from(outcome in WorkOutcome,
        where:
          outcome.goal_id == ^goal.id and outcome.goal_revision == ^goal.current_revision and
            outcome.disposition == "accepted" and outcome.work_item_id in ^dependency_ids,
        select: outcome.work_item_id
      )
      |> Repo.all()
      |> MapSet.new()

    if !Enum.all?(dependency_ids, &MapSet.member?(accepted_ids, &1)),
      do: rollback(:waiting_dependency)
  end

  defp completion_relevant_items(current_items, goal, required_items, integration_item) do
    item_by_id = Map.new(current_items, &{&1.id, &1})
    item_ids = Map.keys(item_by_id)

    dependency_ids_by_item =
      from(edge in WorkDependency,
        where: edge.goal_id == ^goal.id and edge.work_item_id in ^item_ids,
        select: {edge.work_item_id, edge.depends_on_id}
      )
      |> Repo.all()
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

  defp candidate_subject_from_result!(run, opts, error) do
    task_result = value(run.result || %{}, "task_result")

    if !is_map(task_result), do: rollback(error)

    case validate_contract(:task_result, task_result, opts) do
      :ok -> :ok
      {:error, _reason} -> rollback(error)
    end

    subject = parse_subject!(value(task_result, "subject"))
    subject_hash = RequestHash.canonical(subject)
    result_id = required_uuid!(task_result, :result_id)

    if !(value(task_result, "kind") == "candidate_completion" and
           value(task_result, "subject_hash") == ContextAssembly.context_digest(subject_hash)),
       do: rollback(error)

    {subject, subject_hash, result_id}
  end

  defp ensure_subject_resource!(subject, item, error) do
    if value(subject, "resource_id") != item.repository_resource_id, do: rollback(error)
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

  defp plan_membership_integration!(item, derived_key) do
    explicit = plan_item_integration!(item)

    case derived_key do
      nil -> explicit
      key -> explicit or value(item, :key) == key
    end
  end

  defp ensure_evidence_subject!(evidence, subject, subject_hash) do
    expected_hash = ContextAssembly.context_digest(subject_hash)

    if !Enum.all?(evidence, fn row ->
         row.subject_hash == subject_hash and value(row.payload, "subject") == subject and
           value(row.payload, "subject_hash") == expected_hash and
           value(row.source_ref, "subject_hash") == expected_hash
       end),
       do: rollback(:missing_evidence)
  end

  defp goal_evidence_for!(goal, evidence_ids) do
    if !Enum.all?(evidence_ids, &valid_uuid?(&1)), do: rollback(:invalid_request)

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

  defp ensure_required_evidence!(contract, evidence, subject_hash) do
    predicates = value(contract || %{}, "predicates", [])
    if predicates == [], do: rollback(:missing_evidence)

    Enum.each(predicates, fn predicate ->
      predicate_id = required_string!(predicate, :id)
      kind = required_string!(predicate, :kind)

      case kind do
        "operator_acceptance" ->
          # Enforced by the preceding ensure_final_acceptance_authority!/4 call.
          :ok

        _ ->
          found =
            Enum.any?(evidence, fn row ->
              row.subject_hash == subject_hash and row.verdict == "passed" and
                value(row.payload, "predicate_id") == predicate_id and
                goal_predicate_matches_evidence?(predicate, row)
            end)

          if !found, do: rollback(:missing_evidence)
      end
    end)
  end

  @doc false

  def goal_predicate_matches_evidence?(predicate, evidence) do
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

  @doc false

  def creates_cycle?(goal_id, work_item_id, depends_on_id) do
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
        Enum.map([depends_on_id, goal_id, work_item_id], &Ecto.UUID.dump!/1)
      )

    match?(%{rows: [[true]]}, result)
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

  # The authenticated operator command is itself the Goal-level acceptance act,
  # so no separate completion Decision has to be raised and resolved first. A
  # supplied completion Decision is still validated exactly, and a Subject this
  # revision already rejected stays rejected.
  defp ensure_final_acceptance_authority!(goal, :operator, decision_id, subject_hash) do
    case decision_id do
      nil ->
        ensure_completion_subject_not_rejected!(goal, subject_hash)

      decision_id ->
        Decisions.ensure_resolved_completion_decision!(goal, decision_id, subject_hash)
    end
  end

  defp ensure_final_acceptance_authority!(_goal, :deterministic, _decision_id, _subject_hash), do: :ok

  defp ensure_completion_subject_not_rejected!(goal, subject_hash) do
    expected_action_hash =
      ContractValidation.completion_action_hash(goal.id, goal.current_revision, subject_hash)

    decision =
      Repo.one(
        from(decision in GoalDecision,
          where:
            decision.goal_id == ^goal.id and decision.goal_revision == ^goal.current_revision and
              decision.state == "resolved" and decision.kind == "completion" and
              is_nil(decision.work_item_id) and decision.subject_hash == ^subject_hash and
              decision.action_hash == ^expected_action_hash,
          lock: "FOR UPDATE"
        )
      )

    if decision && value(decision.resolution || %{}, "option_id") == "reject",
      do: rollback(:invalid_decision)
  end

  defp admission_allowed_runtime_ids!(revision) do
    case value(revision.execution_policy || %{}, "allowed_runtime_ids") do
      runtime_ids when is_list(runtime_ids) ->
        if length(runtime_ids) <= 256 and
             length(runtime_ids) == length(Enum.uniq(runtime_ids)) and
             Enum.all?(runtime_ids, &valid_uuid?/1),
           do: runtime_ids,
           else: rollback(:invalid_request)

      _ ->
        rollback(:invalid_request)
    end
  end

  defp admission_subject!(goal, item, "validate", payload, opts) do
    if Map.has_key?(payload, :subject) or Map.has_key?(payload, "subject"),
      do: rollback(:invalid_validation)

    producer_id = optional_uuid!(payload, :validation_of_task_id) || rollback(:invalid_request)
    producer = lock_task!(producer_id)

    if !(producer.goal_id == goal.id and producer.work_item_id == item.id and
           producer.goal_revision == goal.current_revision and producer.state == "completed" and
           producer.current_generation > 0),
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

  defp require_provider_access(required_capabilities, nil), do: required_capabilities

  defp require_provider_access(required_capabilities, _provider_scope),
    do: Map.put(required_capabilities, "provider_access", true)

  defp admission_provider_scope!(_goal, revision, item, purpose) do
    resource_id = item.repository_resource_id

    if !resource_allowed?(revision.execution_policy, resource_id) do
      rollback(:resource_not_allowed)
    end

    if purpose == "implement" do
      case item.change_target do
        nil ->
          if provider_operation_allowed?(revision, "resource.sync") do
            provider_scope_resource!(resource_id, ["repositories"])
            provider_scope(resource_id, ["resource.sync"], nil)
          end

        %{"kind" => "branches"} = target ->
          if !provider_operation_allowed?(revision, "change.upsert"),
            do: rollback(:invalid_plan)

          provider_scope_resource!(resource_id, ["repositories", "changes"])
          target = branch_change_target!(target, revision)

          operations =
            if provider_operation_allowed?(revision, "change.update"),
              do: ["change.upsert", "change.update"],
              else: ["change.upsert"]

          provider_scope(resource_id, operations, target)

        %{"kind" => "pull_request"} = target ->
          if !provider_operation_allowed?(revision, "change.update"),
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

  @doc false

  def resource_allowed?(policy, resource_id) do
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

  defp optional_string(map, key), do: if(is_binary(value(map, key)), do: String.trim(value(map, key)))
end
