defmodule SymmetryControl.Orchestration do
  @moduledoc """
  Durable task orchestration backed exclusively by PostgreSQL.

  This context owns lifecycle transitions and the fencing fields used by the
  daemon protocol. Callers may use PubSub as a wake-up hint, never as state.
  """

  alias SymmetryControl.Orchestration.Machine
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Orchestration.Command
  alias SymmetryControl.Workspaces.WorkItem

  @typedoc """
  One retried task's binding: the agent profile, workspace and required
  capabilities `Workspaces.retry_binding/2` resolves for its work item,
  including the requirement the attempt being retried imposed.
  """
  @type retry_binding :: %{
          task_id: Ecto.UUID.t(),
          agent_profile: String.t() | nil,
          workspace: String.t() | nil,
          required_capabilities: map()
        }

  alias SymmetryControl.Orchestration.Claims
  alias SymmetryControl.Orchestration.Assignment
  alias SymmetryControl.Orchestration.Registry
  alias SymmetryControl.Orchestration.Transitions
  alias SymmetryControl.Orchestration.Commands
  alias SymmetryControl.Orchestration.TaskReads

  @spec native_harness_kind?(term()) :: boolean()
  defdelegate native_harness_kind?(kind), to: Registry

  @spec native_harness_kinds() :: [String.t()]
  defdelegate native_harness_kinds(), to: Registry

  @spec enroll_machine(map(), String.t(), keyword()) ::
          {:ok, %{machine: Machine.t(), token: String.t()}, :created | :replayed}
          | {:error, atom() | Ecto.Changeset.t()}
  defdelegate enroll_machine(attrs, idempotency_key, opts), to: Registry

  @spec authenticate_machine(String.t()) :: {:ok, Machine.t()} | {:error, :unauthenticated}
  defdelegate authenticate_machine(token), to: Registry

  @spec register_runtimes(Ecto.UUID.t(), Ecto.UUID.t(), [map()], keyword()) ::
          {:ok, [Runtime.t()]} | {:error, term()}
  defdelegate register_runtimes(machine_id, daemon_instance_id, specifications, opts \\ []),
    to: Registry

  @spec heartbeat(Ecto.UUID.t(), integer(), [map()], keyword()) :: {:ok, map()} | {:error, atom()}
  defdelegate heartbeat(runtime_id, runtime_epoch, active_runs, opts \\ []), to: Registry

  @spec submit_task(map(), String.t(), keyword()) ::
          {:ok, Task.t(), :created | :replayed} | {:error, term()}
  defdelegate submit_task(attrs, idempotency_key, opts \\ []), to: TaskReads

  @spec fetch_task(Ecto.UUID.t()) :: {:ok, Task.t()} | {:error, :not_found}
  defdelegate fetch_task(id), to: TaskReads

  @spec fetch_run(Ecto.UUID.t()) :: {:ok, Run.t()} | {:error, :not_found}
  defdelegate fetch_run(id), to: TaskReads

  @spec fetch_runtime(Ecto.UUID.t()) ::
          {:ok, Runtime.t()} | {:error, :not_found | :invalid_request}
  defdelegate fetch_runtime(id), to: TaskReads

  @spec runtime_snapshots() :: {:ok, [map()]}
  defdelegate runtime_snapshots(), to: TaskReads

  @spec runtime_snapshot(Ecto.UUID.t()) :: {:ok, map()} | {:error, :not_found | :invalid_request}
  defdelegate runtime_snapshot(id), to: TaskReads

  @spec fetch_command(Ecto.UUID.t()) ::
          {:ok, Command.t()} | {:error, :not_found | :invalid_request}
  defdelegate fetch_command(id), to: TaskReads

  @spec machine_owns_runtime?(Ecto.UUID.t(), Ecto.UUID.t()) :: boolean()
  defdelegate machine_owns_runtime?(machine_id, runtime_id), to: Registry

  @spec machine_owns_run?(Ecto.UUID.t(), Ecto.UUID.t()) :: boolean()
  defdelegate machine_owns_run?(machine_id, run_id), to: Registry

  @spec machine_owns_command?(Ecto.UUID.t(), Ecto.UUID.t()) :: boolean()
  defdelegate machine_owns_command?(machine_id, command_id), to: Registry

  @spec task_snapshot(Ecto.UUID.t()) ::
          {:ok,
           %{
             task: Task.t(),
             run: Run.t() | nil,
             runtime: Runtime.t() | nil,
             waiting: map() | nil,
             latest_command: Command.t() | nil
           }}
          | {:error, atom()}
  defdelegate task_snapshot(task_id), to: TaskReads

  defdelegate control_capabilities(task, run, runtime, pending?), to: TaskReads

  @spec adapter_operation_supported?(Runtime.t() | nil, atom()) :: boolean()
  defdelegate adapter_operation_supported?(arg1, operation), to: TaskReads

  @spec retryable_attempt?(Task.t()) :: boolean()
  defdelegate retryable_attempt?(arg1), to: TaskReads

  @spec retry_runtime_eligible_task_ids([retry_binding()]) :: MapSet.t(Ecto.UUID.t())
  defdelegate retry_runtime_eligible_task_ids(bindings), to: TaskReads

  @spec retry_runtime_eligible?(retry_binding()) :: boolean()
  defdelegate retry_runtime_eligible?(binding), to: TaskReads

  @spec retry_required_capabilities(map(), map() | nil) :: map()
  defdelegate retry_required_capabilities(required, current_required), to: Commands

  @spec list_task_events(Ecto.UUID.t(), keyword()) :: {:ok, map()} | {:error, atom()}
  defdelegate list_task_events(task_id, opts \\ []), to: TaskReads

  @spec list_task_transitions(Ecto.UUID.t(), keyword()) :: {:ok, map()} | {:error, atom()}
  defdelegate list_task_transitions(task_id, opts \\ []), to: TaskReads

  @spec list_task_commands(Ecto.UUID.t(), keyword()) :: {:ok, map()} | {:error, atom()}
  defdelegate list_task_commands(task_id, opts \\ []), to: TaskReads

  @spec task_timeline(Ecto.UUID.t(), keyword()) :: {:ok, map()} | {:error, atom()}
  defdelegate task_timeline(task_id, opts \\ []), to: TaskReads

  @spec assignment_target(Run.t()) ::
          {:ok, %{machine_id: Ecto.UUID.t(), runtime_id: Ecto.UUID.t()}} | {:error, :not_found}
  defdelegate assignment_target(arg1), to: Assignment

  @spec assign_one(keyword()) :: {:ok, Run.t()} | {:error, :no_assignment}
  defdelegate assign_one(opts \\ []), to: Assignment

  @spec assign_all(keyword()) :: {:ok, [Run.t()]}
  defdelegate assign_all(opts \\ []), to: Assignment

  @spec claim(Ecto.UUID.t(), map(), keyword()) :: {:ok, Run.t()} | {:error, atom()}
  defdelegate claim(run_id, request, opts \\ []), to: Claims

  @doc false
  @spec claim_owner(Ecto.UUID.t()) :: {:ok, :goal | :legacy} | {:error, atom()}
  defdelegate claim_owner(run_id), to: Claims

  @doc false
  @spec claim_with_disposition(Ecto.UUID.t(), map(), keyword()) ::
          {:ok, Run.t(), :created | :replayed} | {:error, atom()}
  defdelegate claim_with_disposition(run_id, request, opts), to: Claims

  @doc false
  @spec prepare_claim_in_transaction(Ecto.UUID.t(), map(), keyword(), WorkItem.t() | nil) ::
          {:replayed, map()} | {:new, map()} | {:error, atom()}
  defdelegate prepare_claim_in_transaction(run_id, request, opts, work_item), to: Claims

  @doc false
  @spec commit_new_claim_in_transaction(map(), keyword()) ::
          {:created, Run.t()} | {:error, atom()}
  defdelegate commit_new_claim_in_transaction(claim_context, opts), to: Claims

  @doc false
  @spec emit_claimed(Run.t()) :: :ok
  defdelegate emit_claimed(arg1), to: Claims

  @doc false
  @spec emit_transition(Run.t(), String.t()) :: :ok
  defdelegate emit_transition(arg1, state), to: Transitions

  @spec renew_lease(Ecto.UUID.t(), map(), keyword()) :: {:ok, Run.t()} | {:error, atom()}
  defdelegate renew_lease(run_id, fence, opts \\ []), to: Claims

  @spec append_events(Ecto.UUID.t(), map(), [map()], keyword()) ::
          {:ok, [RunEvent.t()]} | {:error, atom()}
  defdelegate append_events(run_id, fence, events, opts \\ []), to: Transitions

  @spec transition(Ecto.UUID.t(), map(), String.t(), map(), String.t(), keyword()) ::
          {:ok, Run.t()} | {:error, atom()}
  defdelegate transition(run_id, fence, target_state, payload, transition_id, opts \\ []),
    to: Transitions

  @spec create_command(Ecto.UUID.t(), String.t(), map(), String.t(), keyword()) ::
          {:ok, Command.t(), :created | :replayed} | {:error, atom()}
  defdelegate create_command(task_id, kind, payload, idempotency_key, opts \\ []), to: Commands

  @doc false
  @spec create_goal_control_command(
          Ecto.UUID.t(),
          pos_integer(),
          Ecto.UUID.t(),
          Ecto.UUID.t(),
          String.t(),
          map(),
          String.t(),
          keyword()
        ) :: {:ok, Command.t(), :created | :replayed} | {:error, atom()}
  defdelegate create_goal_control_command(
                goal_id,
                revision,
                action_id,
                task_id,
                kind,
                payload,
                idempotency_key,
                opts \\ []
              ),
              to: Commands

  @doc false
  @spec create_goal_control_command_in_transaction(map(), map(), keyword()) ::
          {Command.t(), :created | :replayed, map() | nil} | {:error, term()}
  defdelegate create_goal_control_command_in_transaction(authorization, envelope, opts \\ []),
    to: Commands

  @doc false
  @spec apply_goal_pause_transition_in_transaction(
          Task.t(),
          Run.t(),
          map(),
          map(),
          Ecto.UUID.t(),
          keyword()
        ) ::
          {:created, {Run.t(), map() | nil}}
          | {:replayed, Run.t()}
          | {:error, term()}
  defdelegate apply_goal_pause_transition_in_transaction(
                task,
                run,
                fence,
                payload,
                transition_id,
                opts \\ []
              ),
              to: Transitions

  @spec request_cancel(Ecto.UUID.t(), keyword()) ::
          {:ok, Task.t(), Command.t() | nil} | {:error, atom()}
  defdelegate request_cancel(task_id, opts \\ []), to: Commands

  @spec provide_input(Ecto.UUID.t(), map(), String.t(), keyword()) ::
          {:ok, Command.t(), :created | :replayed} | {:error, atom()}
  defdelegate provide_input(task_id, payload, idempotency_key, opts \\ []), to: Commands

  @spec retry_task(Ecto.UUID.t(), map(), String.t(), keyword()) ::
          {:ok, Task.t(), Command.t(), :created | :replayed} | {:error, atom()}
  defdelegate retry_task(task_id, attrs, idempotency_key, opts \\ []), to: Commands

  @spec acknowledge_command(Ecto.UUID.t(), map(), String.t(), Ecto.UUID.t(), keyword()) ::
          {:ok, Command.t()} | {:error, atom()}
  defdelegate acknowledge_command(command_id, fence, outcome, acknowledgement_id, opts \\ []),
    to: Commands

  @spec work_snapshot(Ecto.UUID.t(), integer(), keyword()) :: {:ok, map()} | {:error, atom()}
  defdelegate work_snapshot(runtime_id, runtime_epoch, opts \\ []), to: TaskReads

  @spec reconcile(Ecto.UUID.t(), integer(), [map()], keyword()) :: {:ok, map()} | {:error, atom()}
  defdelegate reconcile(runtime_id, runtime_epoch, journals, opts \\ []), to: Transitions

  @spec expire(keyword()) :: %{
          expired_runs: non_neg_integer(),
          offline_runtimes: non_neg_integer()
        }
  defdelegate expire(opts \\ []), to: Transitions
end
