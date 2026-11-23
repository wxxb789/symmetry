defmodule SymmetryControl.Goals.Decisions do
  @moduledoc false

  import Ecto.Query
  import SymmetryControl.Goals.ContractValidation, only: [validate_contract: 3]
  import SymmetryControl.Goals.EventsReceipts, only: [nullable_datetime: 1]

  import SymmetryControl.Goals.Internal,
    only: [
      lock_decision!: 1,
      lock_work_item!: 1,
      plan_action_hash: 2,
      required_string!: 2,
      required_uuid!: 2,
      rollback: 1,
      value: 2,
      work_item_acceptance_action_hash: 3
    ]

  alias Ecto.Changeset
  alias SymmetryControl.Goals.Admission
  alias SymmetryControl.Goals.ContextAssembly
  alias SymmetryControl.Goals.ContractValidation
  alias SymmetryControl.Goals.GoalDecision
  alias SymmetryControl.Goals.GoalEvent
  alias SymmetryControl.Goals.PlanContracts
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash

  @nonterminal_task_states [
    "queued",
    "assigned",
    "claimed",
    "running",
    "waiting_for_input",
    "paused",
    "cancelling"
  ]

  @doc false

  def decision_request!(goal, "plan", nil, nil, proposal, opts) when is_map(proposal) do
    if !PlanContracts.valid_plan_proposal?(proposal, goal, opts), do: rollback(:invalid_plan)

    {nil, nil, plan_action_hash(goal, RequestHash.canonical(proposal)), "Accept the proposed plan?"}
  end

  def decision_request!(goal, "scope", work_item_id, nil, proposal, _opts)
      when is_binary(work_item_id) and is_map(proposal) do
    ensure_decision_work_item!(goal, work_item_id)
    operation = required_string!(proposal, :kind)
    dependency_id = required_uuid!(proposal, :depends_on_id)

    if operation not in ["add_dependency", "remove_dependency"], do: rollback(:invalid_request)

    {work_item_id, nil, dependency_action_hash(goal, operation, work_item_id, dependency_id),
     "Approve the requested dependency change?"}
  end

  def decision_request!(goal, "review", work_item_id, subject_hash, nil, _opts)
      when is_binary(work_item_id) and is_binary(subject_hash) do
    ensure_decision_work_item!(goal, work_item_id)

    {work_item_id, subject_hash, work_item_acceptance_action_hash(goal, work_item_id, subject_hash),
     "Accept the validated work outcome?"}
  end

  def decision_request!(goal, "completion", nil, subject_hash, nil, _opts) when is_binary(subject_hash) do
    action_hash =
      ContractValidation.completion_action_hash(goal.id, goal.current_revision, subject_hash)

    {nil, subject_hash, action_hash, "Accept the Goal outcome?"}
  end

  def decision_request!(_goal, _kind, _work_item_id, _subject_hash, _proposal, _opts), do: rollback(:invalid_request)

  @doc false

  def derived_decision_options(_kind) do
    [
      %{"id" => "accept", "label" => "Accept", "consequence" => "Apply the approved action."},
      %{
        "id" => "reject",
        "label" => "Reject",
        "consequence" => "Keep the current approved state."
      }
    ]
  end

  @doc false

  def unresolved_decisions?(goal_id, revision) do
    Repo.exists?(
      from(decision in GoalDecision,
        where:
          decision.goal_id == ^goal_id and decision.goal_revision == ^revision and
            decision.state == "open"
      )
    )
  end

  @doc false

  def pending_plan_task?(goal, excluded_task_id \\ nil) do
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

  @doc false

  def plan_decision_pending_acceptance?(goal) do
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

  @doc false

  def planning_acceptance_contract do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "The operator must accept the proposed plan.",
      "predicates" => [%{"id" => "operator", "kind" => "operator_acceptance"}]
    }
  end

  defp ensure_decision_work_item!(goal, work_item_id) do
    item = lock_work_item!(work_item_id)
    Admission.ensure_admissible_item!(goal, item)
  end

  @doc false

  def ensure_resolved_scope_decision!(goal, item, decision_id, operation, dependency_id) do
    decision = lock_decision!(decision_id)

    if !(decision.goal_id == goal.id and decision.goal_revision == goal.current_revision and
           decision.state == "resolved" and decision.kind == "scope" and
           decision.work_item_id == item.id and
           decision.action_hash ==
             dependency_action_hash(goal, operation, item.id, dependency_id) and
           value(decision.resolution || %{}, "option_id") == "accept"),
       do: rollback(:invalid_decision)
  end

  @doc false

  def ensure_resolved_completion_decision!(goal, decision_id, subject_hash) do
    decision = lock_decision!(decision_id)

    expected_action_hash =
      ContractValidation.completion_action_hash(goal.id, goal.current_revision, subject_hash)

    if !(decision.goal_id == goal.id and decision.goal_revision == goal.current_revision and
           decision.state == "resolved" and decision.kind == "completion" and
           is_nil(decision.work_item_id) and
           decision.subject_hash == subject_hash and
           decision.action_hash == expected_action_hash and
           value(decision.resolution || %{}, "option_id") == "accept"),
       do: rollback(:invalid_decision)
  end

  @doc false

  def decision_option?(decision, option_id) do
    Enum.any?(decision.options || [], &(value(&1, :id) == option_id))
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

  @doc false

  def validate_decision_changeset!(changeset, opts) do
    if !changeset.valid?, do: rollback(:invalid_request)

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
      "action_hash" => ContextAssembly.context_digest(decision.action_hash),
      "subject_hash" => ContextAssembly.context_digest(decision.subject_hash),
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
end
