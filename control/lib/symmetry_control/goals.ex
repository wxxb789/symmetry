defmodule SymmetryControl.Goals do
  @moduledoc """
  PostgreSQL-backed Goal transition policy.

  This context is the only writer for Goal lifecycle, revision, plan, admission,
  decision and acceptance records. Goal-aware writes acquire Goal authority
  before invoking policy-free Orchestration transaction kernels. Terminal Task
  handling reaches `settle_task/4` only after the orchestration transaction has
  committed.
  """

  import Ecto.Query

  import SymmetryControl.Goals.Internal,
    only: [
      lock_context_snapshot!: 2,
      lock_goal!: 1,
      lock_work_item!: 1,
      rollback: 1,
      valid_uuid: 1,
      valid_uuid?: 1
    ]

  import SymmetryControl.Goals.RunFence, only: [context_wire_envelope!: 4]

  alias SymmetryControl.Goals.Commands
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.GoalControl
  alias SymmetryControl.Goals.HarnessSessions
  alias SymmetryControl.Goals.ReadModel
  alias SymmetryControl.Goals.Reconciliation
  alias SymmetryControl.Goals.Recovery
  alias SymmetryControl.Goals.RunFence
  alias SymmetryControl.Goals.Settlement
  alias SymmetryControl.Goals.ValidationOutcome
  alias SymmetryControl.Orchestration.Command
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo

  @default_event_limit 100
  @default_attention_limit 100
  @type receipt :: %{goal: map(), event: map(), response: map()}

  @doc false
  @spec claim(Ecto.UUID.t(), map(), keyword()) :: {:ok, Run.t()} | {:error, atom()}
  defdelegate claim(run_id, request, opts \\ []), to: RunFence

  @doc false
  @spec claim_with_disposition(Ecto.UUID.t(), map(), keyword()) ::
          {:ok, Run.t(), :created | :replayed} | {:error, atom()}
  defdelegate claim_with_disposition(run_id, request, opts \\ []), to: RunFence

  @spec create_goal(Ecto.UUID.t(), map(), String.t(), keyword()) ::
          {:ok, receipt(), :created | :replayed} | {:error, term()}
  defdelegate create_goal(project_id, attrs, actor_ref, opts \\ []), to: Commands

  @spec fetch_goal(Ecto.UUID.t()) :: {:ok, map()} | {:error, :not_found | :invalid_request}
  def fetch_goal(goal_id) when is_binary(goal_id) do
    with :ok <- valid_uuid(goal_id), do: ReadModel.fetch(goal_id)
  end

  def fetch_goal(_), do: {:error, :invalid_request}

  @doc """
  The goal's current event sequence, or nil for an unknown or malformed id.

  This is the durable cursor of the goal's own event log; the portal
  invalidation path carries it so a connected browser can continue its receipt
  list from the last sequence it rendered instead of refetching every page. It
  is a notification hint, never authority: the served event page stays the
  source of truth.
  """
  @spec current_event_sequence(Ecto.UUID.t()) :: integer() | nil
  def current_event_sequence(goal_id) when is_binary(goal_id) do
    case valid_uuid(goal_id) do
      :ok -> Repo.one(from goal in Goal, where: goal.id == ^goal_id, select: goal.event_sequence)
      _invalid -> nil
    end
  end

  def current_event_sequence(_goal_id), do: nil

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
  defdelegate command(goal_id, command, actor_ref, opts \\ []), to: Commands

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
        goal = lock_goal!(goal_id)
        snapshot = lock_context_snapshot!(goal.id, snapshot_id)
        item = if snapshot.work_item_id, do: lock_work_item!(snapshot.work_item_id)

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
  defdelegate attach_harness_session(machine_id, run_id, fence, attrs, opts \\ []),
    to: HarnessSessions

  @spec fetch_harness_session_attachment(Ecto.UUID.t(), Ecto.UUID.t(), map()) ::
          {:ok, map()} | {:error, term()}
  defdelegate fetch_harness_session_attachment(machine_id, run_id, fence), to: HarnessSessions

  @spec mark_harness_session_stopped(Ecto.UUID.t(), Ecto.UUID.t(), map(), map(), keyword()) ::
          {:ok, map(), :created | :replayed} | {:error, term()}
  defdelegate mark_harness_session_stopped(machine_id, run_id, fence, attrs, opts \\ []),
    to: HarnessSessions

  @spec append_evidence(Ecto.UUID.t(), Ecto.UUID.t(), map(), map(), keyword()) ::
          {:ok, map(), :created | :replayed} | {:error, term()}
  defdelegate append_evidence(machine_id, run_id, fence, evidence, opts \\ []), to: RunFence

  @spec append_evidence_batch(Ecto.UUID.t(), Ecto.UUID.t(), map(), map(), keyword()) ::
          {:ok, map(), :created | :replayed} | {:error, term()}
  defdelegate append_evidence_batch(machine_id, run_id, fence, batch, opts \\ []), to: RunFence

  @spec record_usage(Ecto.UUID.t(), Ecto.UUID.t(), map(), map(), keyword()) ::
          {:ok, map(), :created | :replayed} | {:error, term()}
  defdelegate record_usage(machine_id, run_id, fence, usage, opts \\ []), to: RunFence

  @spec fetch_run_context(Ecto.UUID.t(), Ecto.UUID.t(), map()) :: {:ok, map()} | {:error, term()}
  defdelegate fetch_run_context(machine_id, run_id, fence), to: RunFence

  @doc """
  Reconcile a terminal Task after Orchestration has committed its terminal Run.

  A terminal process settles its budget reservation and records a compact event.
  A producer result never directly accepts work. A distinct terminal validation
  Task may derive an immutable accepted or rejected outcome from its frozen
  candidate Subject and persisted evidence.
  """
  @spec settle_task(Ecto.UUID.t(), Ecto.UUID.t(), non_neg_integer(), keyword()) ::
          {:ok, map()} | {:error, term()}
  defdelegate settle_task(task_id, run_id, generation, opts \\ []), to: Settlement

  @doc """
  Reconcile a terminal validation Task after a scoped operator review resolves.

  This is a distinct mutation from `settle_task/4`: the original settlement
  receipt remains immutable, while the late validation outcome gets its own
  durable `validation_outcome_settled` receipt and event.
  """
  @spec reconcile_validation_outcome(Ecto.UUID.t(), Ecto.UUID.t(), non_neg_integer(), keyword()) ::
          {:ok, map()} | {:error, term()}
  defdelegate reconcile_validation_outcome(task_id, run_id, generation, opts \\ []),
    to: ValidationOutcome

  @doc """
  Settles a Goal Task cancelled before any Run existed.

  The caller must invoke this only after Orchestration durably records the
  applied runless cancellation command. This context verifies that no Run was
  fabricated, releases the reservation, and records one deterministic receipt.
  """
  @spec settle_unstarted_task(Ecto.UUID.t(), String.t(), keyword()) ::
          {:ok, map()} | {:error, term()}
  defdelegate settle_unstarted_task(task_id, reason, opts \\ []), to: Settlement

  @doc """
  Returns active Goals due for reconciliation. The caller is a durable worker;
  this function takes fresh row locks before making each scheduling decision.
  """
  @spec reconcile_due_goals(keyword()) :: {:ok, [map()]} | {:error, term()}
  defdelegate reconcile_due_goals(opts \\ []), to: Reconciliation

  @doc """
  Produces durable pause/cancel control descriptors from one Goal audit action.

  The GoalControlWorker owns dispatch after commit. This function only enumerates
  descriptors; `dispatch_goal_control_command/8` rechecks the same authority
  while creating or replaying each command.
  """
  @spec control_dispatch_plan(Ecto.UUID.t(), pos_integer(), Ecto.UUID.t(), keyword()) ::
          {:ok, :superseded | [map()]} | {:error, term()}
  defdelegate control_dispatch_plan(goal_id, revision, action_id, opts \\ []), to: GoalControl

  @doc """
  Atomically authorizes and dispatches one Goal control descriptor.

  The Goal row and current lifecycle event are locked in the same transaction as
  the Task/Run command write. The descriptor is accepted only for the current
  action; an existing exact command may replay after its Task state has advanced.
  """
  @spec dispatch_goal_control_command(
          Ecto.UUID.t(),
          pos_integer(),
          Ecto.UUID.t(),
          Ecto.UUID.t(),
          String.t(),
          map(),
          String.t(),
          keyword()
        ) ::
          {:ok, Command.t(), :created | :replayed}
          | {:ok, :superseded}
          | {:error, term()}
  defdelegate dispatch_goal_control_command(
                goal_id,
                revision,
                action_id,
                task_id,
                kind,
                payload,
                idempotency_key,
                opts \\ []
              ),
              to: GoalControl

  @doc false
  @spec apply_goal_pause_transition(
          Ecto.UUID.t(),
          map(),
          map(),
          Ecto.UUID.t(),
          keyword()
        ) ::
          {:ok, Run.t(), :created, map() | nil}
          | {:ok, Run.t(), :replayed}
          | {:error, term()}
  defdelegate apply_goal_pause_transition(run_id, fence, payload, transition_id, opts \\ []),
    to: GoalControl

  @doc """
  Finds every historical Goal-authorized runless cancellation that still holds
  a reservation. This is deliberately independent of the triggering action:
  an Oban control job may be discarded after the cancellation commits, while a
  later Goal control job must still be able to repair the settlement gap.
  """
  @spec recover_pending_unstarted_cancellations(Ecto.UUID.t()) ::
          {:ok, [Ecto.UUID.t()]} | {:error, term()}
  defdelegate recover_pending_unstarted_cancellations(goal_id), to: Recovery

  @doc false
  @spec recoverable_unstarted_cancellation_goal_ids(keyword()) ::
          {:ok, [Ecto.UUID.t()]} | {:error, term()}
  defdelegate recoverable_unstarted_cancellation_goal_ids(opts \\ []), to: Recovery

  @doc """
  Finds current terminal Goal executions whose settlement still needs evaluation.
  The periodic wakeup worker sends ordinary descriptors through `settle_task/4`;
  a closed validation attempt awaiting an operator predicate uses the distinct
  `reconcile_validation_outcome/4` mutation instead.
  """
  @spec recover_pending_terminal_settlements(keyword()) :: {:ok, [map()]} | {:error, term()}
  defdelegate recover_pending_terminal_settlements(opts \\ []), to: Recovery

  @doc false
  @spec recover_pending_goal_control_actions(keyword()) :: {:ok, [map()]} | {:error, term()}
  defdelegate recover_pending_goal_control_actions(opts \\ []), to: Recovery

  defp valid_event_options(opts) do
    after_value = Keyword.get(opts, :after)
    limit = Keyword.get(opts, :limit, @default_event_limit)

    if (is_nil(after_value) or (is_integer(after_value) and after_value >= 0)) and
         is_integer(limit) and limit > 0, do: :ok, else: {:error, :invalid_request}
  end

  defp valid_attention_options(opts) do
    limit = Keyword.get(opts, :limit, @default_attention_limit)
    cursor = Keyword.get(opts, :cursor)

    if is_integer(limit) and limit > 0 and
         (is_nil(cursor) or (is_binary(cursor) and cursor != "")),
       do: :ok,
       else: {:error, :invalid_request}
  end
end
