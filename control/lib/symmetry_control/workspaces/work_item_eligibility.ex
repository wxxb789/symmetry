defmodule SymmetryControl.Workspaces.WorkItemEligibility do
  @moduledoc """
  The work-item-level verdicts behind the start, cancel and retry commands, and
  the work-item conjuncts the portal serves as `can_start`, `can_cancel` and
  `can_retry`.

  `SymmetryControl.Workspaces` enforces the verdicts — the launch transaction
  (`launch_work_item/3` and `launch_locked_work_item/2`'s fresh-start clause),
  `cancel_work_item/3` through `ensure_legacy_work_item/1`, and
  `retry_work_item/3` — and `SymmetryControlWeb.PortalJSON` projects the boolean
  helpers, so one transition policy answers both the command and the read
  instead of two spellings kept in agreement by comments.

  Every function is pure over an already-loaded `%WorkItem{}`: no query, no
  transaction, no process state. The work item's project must be loaded for the
  active-project clause (an unloaded project is never `"active"`, so the check
  fails closed); `cancel_gate/1` reads only the item's own membership columns.
  """

  alias SymmetryControl.Workspaces.WorkItem

  @startable_statuses ["backlog", "ready"]

  @doc """
  The verdict of a launch request against this row's current binding, in the
  enforcing order: the Goal gate, then the active project, then — when no
  attempt is launched yet — the fresh-start conditions (agent assignment, a
  startable status, available external work).

  An already-launched item answers `:ok`: `launch_locked_work_item/2`'s replay
  clause re-issues the existing attempt instead of refusing, so the request
  itself is acceptable. The read's `can_start` is the fresh-start question, so
  `start_allowed?/1` adds the launched conjunct.
  """
  @spec start(WorkItem.t()) :: :ok | {:error, :goal_authority_required | :state_conflict}
  def start(%WorkItem{} = work_item) do
    with :ok <- cancel_gate(work_item),
         :ok <- start_project(work_item) do
      if is_binary(work_item.orchestration_task_id) do
        :ok
      else
        start_state(work_item)
      end
    end
  end

  @doc """
  The served `can_start`: a launch would create a fresh attempt — the verdict is
  `:ok` and no attempt is launched yet.
  """
  @spec start_allowed?(WorkItem.t()) :: boolean()
  def start_allowed?(%WorkItem{} = work_item) do
    is_nil(work_item.orchestration_task_id) and match?(:ok, start(work_item))
  end

  @doc """
  The Goal ownership gate: `:ok` for a legacy item, the
  `:goal_authority_required` refusal for a Goal-managed one.

  Every legacy command path applies it (`Workspaces.ensure_legacy_work_item/1`),
  and the served `can_cancel` carries exactly this clause as its work-item gate:
  a Goal is the authority that stops its work.
  """
  @spec cancel_gate(WorkItem.t()) :: :ok | {:error, :goal_authority_required}
  def cancel_gate(%WorkItem{} = work_item) do
    if WorkItem.goal_managed?(work_item), do: {:error, :goal_authority_required}, else: :ok
  end

  @doc """
  The work-item half of the served `can_cancel`: the Goal gate answers `:ok`.
  """
  @spec cancel_allowed?(WorkItem.t()) :: boolean()
  def cancel_allowed?(%WorkItem{} = work_item), do: match?(:ok, cancel_gate(work_item))

  @doc """
  The verdict of a retry request against this row's binding, in the enforcing
  order: the Goal gate, the active project, available external work, then the
  agent assignment. The three state conditions share the `:state_conflict`
  refusal, so only their common precedence against `:goal_authority_required` is
  observable.
  """
  @spec retry_gate(WorkItem.t()) :: :ok | {:error, :goal_authority_required | :state_conflict}
  def retry_gate(%WorkItem{} = work_item) do
    with :ok <- cancel_gate(work_item),
         :ok <- start_project(work_item),
         :ok <- retry_external_work(work_item) do
      if work_item.assignee_type == "agent", do: :ok, else: {:error, :state_conflict}
    end
  end

  @doc """
  The work-item half of the served `can_retry`: agent assignment, available
  external work and an active project.

  It carries no Goal term — the baseline projection has none, because a
  Goal-owned attempt is already excluded by `Orchestration.retryable_attempt?/1`
  — while `retry_gate/1` refuses a Goal-managed item outright.
  """
  @spec retry_allowed?(WorkItem.t()) :: boolean()
  def retry_allowed?(%WorkItem{} = work_item) do
    work_item.assignee_type == "agent" and WorkItem.external_work_available?(work_item) and
      match?(:ok, start_project(work_item))
  end

  defp start_project(%WorkItem{project: %{status: "active"}}), do: :ok
  defp start_project(_work_item), do: {:error, :state_conflict}

  defp start_state(%WorkItem{} = work_item) do
    if work_item.assignee_type == "agent" and work_item.status in @startable_statuses and
         WorkItem.external_work_available?(work_item),
       do: :ok,
       else: {:error, :state_conflict}
  end

  defp retry_external_work(work_item) do
    if WorkItem.external_work_available?(work_item), do: :ok, else: {:error, :state_conflict}
  end
end
