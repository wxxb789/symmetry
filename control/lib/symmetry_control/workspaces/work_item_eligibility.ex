defmodule SymmetryControl.Workspaces.WorkItemEligibility do
  @moduledoc """
  Verdicts behind the start/cancel/retry commands, served to the portal as
  `can_start`/`can_cancel`/`can_retry`; requires a preloaded `project` (fails closed).
  """

  alias SymmetryControl.Workspaces.WorkItem

  @startable_statuses ["backlog", "ready"]

  @doc """
  Launch verdict, enforcing order: Goal gate, active project, fresh-start conditions.
  An already-launched item replays as `:ok`, so `can_start` is the fresh-start question.
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

  @spec start_allowed?(WorkItem.t()) :: boolean()
  def start_allowed?(%WorkItem{} = work_item) do
    is_nil(work_item.orchestration_task_id) and match?(:ok, start(work_item))
  end

  @doc """
  Goal ownership gate: `:ok` for a legacy item, `:goal_authority_required` for a Goal-managed
  one — a Goal is the authority that stops its work.
  """
  @spec cancel_gate(WorkItem.t()) :: :ok | {:error, :goal_authority_required}
  def cancel_gate(%WorkItem{} = work_item) do
    if WorkItem.goal_managed?(work_item), do: {:error, :goal_authority_required}, else: :ok
  end

  @spec cancel_allowed?(WorkItem.t()) :: boolean()
  def cancel_allowed?(%WorkItem{} = work_item), do: match?(:ok, cancel_gate(work_item))

  @doc """
  Retry verdict, enforcing order: Goal gate, active project, external work, agent
  assignment; the three state conditions all refuse `:state_conflict`.
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
  The served `can_retry` half: agent assignment, external work, active project. No Goal
  term — `Orchestration.retryable_attempt?/1` already excludes Goal-owned attempts.
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
