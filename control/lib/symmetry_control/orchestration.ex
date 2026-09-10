defmodule SymmetryControl.Orchestration do
  @moduledoc """
  Durable task orchestration backed exclusively by PostgreSQL.

  This context owns lifecycle transitions and the fencing fields used by the
  daemon protocol. Callers may use PubSub as a wake-up hint, never as state.
  """

  import Ecto.Query

  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash

  alias SymmetryControl.Goals.{
    ContextSnapshot,
    ContractValidation,
    Goal,
    GoalEvent,
    GoalRevision,
    HarnessSession
  }

  alias SymmetryControl.Goals.Workers.SettleTaskWorker
  alias SymmetryControl.Workspaces.{Project, ProjectResource, WorkItem}

  alias SymmetryControl.Orchestration.{
    Command,
    Machine,
    Run,
    RunEvent,
    RunTransition,
    Runtime,
    Task
  }

  @terminal_states ["completed", "failed", "cancelled", "expired"]
  @terminal_targets ["completed", "failed", "cancelled"]
  @terminal_grace_ms 8 * 60 * 1_000
  @minimum_lease_duration_ms 30_000
  @capacity_bearing_states [
    "assigned",
    "claimed",
    "running",
    "paused",
    "waiting_for_input",
    "cancelling"
  ]
  @supervisory_commands ["guidance", "pause", "resume"]
  @native_harness_kinds ["codex", "claude_code", "pi", "opencode"]
  @goal_assignment_scan_page_size 32
  @validation_profile_name ~r/\A[A-Za-z0-9][A-Za-z0-9._:-]{0,127}\z/
  @sha256 ~r/\Asha256:[0-9a-f]{64}\z/
  @decision_reasons [
    "blocked",
    "consequential",
    "irreversible",
    "security",
    "business_policy",
    "expensive",
    "product_change"
  ]
  @default_history_limit 100
  @max_history_limit 500
  @timeline_source_ranks %{"event" => 0, "transition" => 1, "command" => 2}

  @spec enroll_machine(map(), String.t(), keyword()) ::
          {:ok, %{machine: Machine.t(), token: String.t()}, :created | :replayed}
          | {:error, atom() | Ecto.Changeset.t()}
  def enroll_machine(attrs, idempotency_key, opts)
      when is_map(attrs) and is_binary(idempotency_key) and byte_size(idempotency_key) > 0 and
             is_list(opts) do
    submitted = Keyword.get(opts, :enrollment_token)
    expected = Keyword.get(opts, :expected_enrollment_token)
    token = value(attrs, :machine_token)
    current = now(opts)

    cond do
      not (is_binary(submitted) and is_binary(expected) and secure_compare(submitted, expected)) ->
        {:error, :unauthenticated}

      not (is_binary(token) and String.trim(token) != "") ->
        {:error, :invalid_request}

      true ->
        request = %{name: value(attrs, :name), machine_token: token}
        {request_hash, request_hash_version} = RequestHash.write(request)

        Repo.transaction(fn ->
          case Repo.one(
                 from machine in Machine,
                   where: machine.enrollment_idempotency_key == ^idempotency_key,
                   lock: "FOR UPDATE"
               ) do
            nil ->
              changeset =
                Machine.changeset(%Machine{}, %{
                  name: value(attrs, :name),
                  token_digest: digest(token),
                  enrollment_idempotency_key: idempotency_key,
                  enrollment_request_hash: request_hash,
                  enrollment_request_hash_version: request_hash_version
                })
                |> Ecto.Changeset.force_change(
                  :enrollment_request_hash_version,
                  request_hash_version
                )

              case insert_ignoring_conflict(
                     Machine,
                     stamp_insert(changeset, current),
                     nil
                   ) do
                :inserted ->
                  machine =
                    Repo.one!(
                      from machine in Machine,
                        where: machine.enrollment_idempotency_key == ^idempotency_key,
                        lock: "FOR UPDATE"
                    )

                  {machine, :created}

                :conflict ->
                  case Repo.one(
                         from machine in Machine,
                           where: machine.enrollment_idempotency_key == ^idempotency_key,
                           lock: "FOR UPDATE"
                       ) do
                    %Machine{} = machine ->
                      if RequestHash.matches?(
                           machine.enrollment_request_hash,
                           machine.enrollment_request_hash_version,
                           request
                         ),
                         do: {machine, :replayed},
                         else: rollback(:idempotency_conflict)

                    nil ->
                      rollback(:invalid_request)
                  end

                :invalid ->
                  rollback(:invalid_request)
              end

            %Machine{} = machine ->
              if RequestHash.matches?(
                   machine.enrollment_request_hash,
                   machine.enrollment_request_hash_version,
                   request
                 ),
                 do: {machine, :replayed},
                 else: rollback(:idempotency_conflict)
          end
        end)
        |> case do
          {:ok, {machine, disposition}} ->
            {:ok, %{machine: machine, token: token}, disposition}

          {:error, reason} ->
            {:error, reason}
        end
    end
  end

  def enroll_machine(_, _, _), do: {:error, :invalid_request}

  @spec authenticate_machine(String.t()) :: {:ok, Machine.t()} | {:error, :unauthenticated}
  def authenticate_machine(token) when is_binary(token) do
    case Repo.get_by(Machine, token_digest: digest(token)) do
      nil -> {:error, :unauthenticated}
      machine -> {:ok, machine}
    end
  end

  def authenticate_machine(_), do: {:error, :unauthenticated}

  @spec register_runtimes(Ecto.UUID.t(), Ecto.UUID.t(), [map()], keyword()) ::
          {:ok, [Runtime.t()]} | {:error, term()}
  def register_runtimes(machine_id, daemon_instance_id, specifications, opts \\ [])

  def register_runtimes(machine_id, daemon_instance_id, specifications, opts)
      when is_list(specifications) and is_list(opts) do
    if not (valid_uuid?(machine_id) and valid_uuid?(daemon_instance_id) and
              Enum.all?(specifications, &valid_runtime_specification?/1)) do
      {:error, :invalid_request}
    else
      now = now(opts)

      Repo.transaction(fn ->
        _machine = lock_machine(machine_id)

        Enum.map(specifications, fn specification ->
          runtime_key = value(specification, :runtime_key)
          repository_resource_id = value(specification, :repository_resource_id)
          ensure_runtime_repository_resource!(repository_resource_id)

          existing =
            Repo.one(
              from runtime in Runtime,
                where: runtime.machine_id == ^machine_id and runtime.runtime_key == ^runtime_key,
                lock: "FOR UPDATE"
            )

          ensure_runtime_repository_binding_change_safe!(existing, repository_resource_id)

          attrs = %{
            name: value(specification, :name),
            capacity: value(specification, :capacity),
            agent_profile: value(specification, :agent_profile),
            workspace: value(specification, :workspace),
            repository_resource_id: repository_resource_id,
            capabilities: value(specification, :capabilities, %{}),
            harness_kind: value(specification, :harness_kind),
            harness_version: value(specification, :harness_version),
            adapter_version: value(specification, :adapter_version),
            adapter_protocol_version: value(specification, :adapter_protocol_version),
            status: "online",
            last_heartbeat_at: now,
            heartbeat_interval_ms: value(specification, :heartbeat_interval_ms, 5_000)
          }

          case existing do
            nil ->
              %Runtime{}
              |> Runtime.changeset(
                Map.merge(attrs, %{
                  machine_id: machine_id,
                  runtime_key: runtime_key,
                  daemon_instance_id: daemon_instance_id,
                  connection_epoch: 1
                })
              )
              |> stamp_insert(now)
              |> persist_insert()

            runtime when runtime.daemon_instance_id == daemon_instance_id ->
              runtime |> Runtime.changeset(attrs) |> stamp_update(now) |> persist_update()

            runtime ->
              runtime
              |> Runtime.changeset(
                Map.merge(attrs, %{
                  daemon_instance_id: daemon_instance_id,
                  connection_epoch: runtime.connection_epoch + 1
                })
              )
              |> stamp_update(now)
              |> persist_update()
          end
        end)
      end)
    end
  end

  def register_runtimes(_, _, _, _), do: {:error, :invalid_request}

  @spec heartbeat(Ecto.UUID.t(), integer(), [map()], keyword()) :: {:ok, map()} | {:error, atom()}
  def heartbeat(runtime_id, runtime_epoch, active_runs, opts \\ [])

  def heartbeat(runtime_id, runtime_epoch, active_runs, opts)
      when is_integer(runtime_epoch) and runtime_epoch > 0 and is_list(active_runs) do
    if not (valid_uuid?(runtime_id) and Enum.all?(active_runs, &valid_active_run?/1)),
      do: {:error, :invalid_request},
      else: heartbeat_runtime(runtime_id, runtime_epoch, opts)
  end

  def heartbeat(_, _, _, _), do: {:error, :invalid_request}

  defp heartbeat_runtime(runtime_id, runtime_epoch, opts) do
    current = now(opts)

    Repo.transaction(fn ->
      runtime = lock_runtime(runtime_id)

      if runtime.connection_epoch != runtime_epoch do
        rollback(:ownership_lost)
      end

      runtime =
        runtime
        |> Runtime.changeset(%{status: "online", last_heartbeat_at: current})
        |> stamp_update(current)
        |> Repo.update!()

      snapshot_for(runtime, current)
    end)
  end

  @spec submit_task(map(), String.t(), keyword()) ::
          {:ok, Task.t(), :created | :replayed} | {:error, term()}
  def submit_task(attrs, idempotency_key, opts \\ [])

  def submit_task(attrs, idempotency_key, opts)
      when is_map(attrs) and is_binary(idempotency_key) and byte_size(idempotency_key) > 0 do
    task_attrs = normalize_task_attrs(attrs)

    if not valid_task_attrs?(task_attrs) do
      {:error, :invalid_request}
    else
      {request_hash, request_hash_version} = RequestHash.write(task_attrs)
      current = now(opts)

      Repo.transaction(fn ->
        case Repo.one(
               from task in Task,
                 where: task.idempotency_key == ^idempotency_key,
                 lock: "FOR UPDATE"
             ) do
          nil ->
            changeset =
              Task.changeset(
                %Task{},
                Map.merge(task_attrs, %{
                  idempotency_key: idempotency_key,
                  request_hash: request_hash,
                  request_hash_version: request_hash_version,
                  state: "queued",
                  current_generation: 0,
                  attempt_generation: 1,
                  waiting_transition_id: nil
                })
              )
              |> Ecto.Changeset.force_change(:request_hash_version, request_hash_version)

            case insert_ignoring_conflict(
                   Task,
                   stamp_insert(changeset, current),
                   :idempotency_key
                 ) do
              :inserted ->
                task =
                  Repo.one!(
                    from task in Task,
                      where: task.idempotency_key == ^idempotency_key,
                      lock: "FOR UPDATE"
                  )

                {task, :created}

              :conflict ->
                case Repo.one(
                       from task in Task,
                         where: task.idempotency_key == ^idempotency_key,
                         lock: "FOR UPDATE"
                     ) do
                  %Task{} = task ->
                    if RequestHash.matches?(
                         task.request_hash,
                         task.request_hash_version,
                         task_attrs
                       ),
                       do: {task, :replayed},
                       else: rollback(:idempotency_conflict)

                  nil ->
                    rollback(:invalid_request)
                end

              :invalid ->
                rollback(:invalid_request)
            end

          %Task{} = task ->
            if RequestHash.matches?(task.request_hash, task.request_hash_version, task_attrs),
              do: {task, :replayed},
              else: rollback(:idempotency_conflict)
        end
      end)
      |> case do
        {:ok, {task, disposition}} -> {:ok, task, disposition}
        error -> error
      end
    end
  end

  def submit_task(_, _, _), do: {:error, :invalid_request}

  @spec fetch_task(Ecto.UUID.t()) :: {:ok, Task.t()} | {:error, :not_found}
  def fetch_task(id) when is_binary(id) do
    if valid_uuid?(id) do
      case Repo.get(Task, id) do
        nil -> {:error, :not_found}
        task -> {:ok, task}
      end
    else
      {:error, :invalid_request}
    end
  end

  def fetch_task(_), do: {:error, :invalid_request}

  @spec fetch_run(Ecto.UUID.t()) :: {:ok, Run.t()} | {:error, :not_found}
  def fetch_run(id) when is_binary(id) do
    if valid_uuid?(id) do
      case Repo.get(Run, id) do
        nil -> {:error, :not_found}
        run -> {:ok, run}
      end
    else
      {:error, :invalid_request}
    end
  end

  def fetch_run(_), do: {:error, :invalid_request}

  @spec fetch_runtime(Ecto.UUID.t()) ::
          {:ok, Runtime.t()} | {:error, :not_found | :invalid_request}
  def fetch_runtime(id) when is_binary(id) do
    if valid_uuid?(id) do
      case Repo.get(Runtime, id) do
        nil -> {:error, :not_found}
        runtime -> {:ok, runtime}
      end
    else
      {:error, :invalid_request}
    end
  end

  def fetch_runtime(_), do: {:error, :invalid_request}

  @spec runtime_snapshots() :: {:ok, [map()]}
  def runtime_snapshots do
    Repo.all(
      from runtime in Runtime,
        join: machine in Machine,
        on: machine.id == runtime.machine_id,
        left_join: run in Run,
        on:
          run.runtime_id == runtime.id and
            run.state in ^@capacity_bearing_states,
        order_by: [
          asc: machine.inserted_at,
          asc: machine.id,
          asc: runtime.inserted_at,
          asc: runtime.id,
          asc: run.inserted_at,
          asc: run.id
        ],
        select: %{machine: machine, runtime: runtime, run: run}
    )
    |> runtime_read_models()
    |> then(&{:ok, &1})
  end

  @spec runtime_snapshot(Ecto.UUID.t()) :: {:ok, map()} | {:error, :not_found | :invalid_request}
  def runtime_snapshot(id) when is_binary(id) do
    if valid_uuid?(id) do
      case Repo.all(
             from runtime in Runtime,
               join: machine in Machine,
               on: machine.id == runtime.machine_id,
               left_join: run in Run,
               on:
                 run.runtime_id == runtime.id and
                   run.state in ^@capacity_bearing_states,
               where: runtime.id == ^id,
               order_by: [asc: run.inserted_at, asc: run.id],
               select: %{machine: machine, runtime: runtime, run: run}
           ) do
        [] -> {:error, :not_found}
        rows -> {:ok, rows |> runtime_read_models() |> List.first()}
      end
    else
      {:error, :invalid_request}
    end
  end

  def runtime_snapshot(_), do: {:error, :invalid_request}

  @spec fetch_command(Ecto.UUID.t()) ::
          {:ok, Command.t()} | {:error, :not_found | :invalid_request}
  def fetch_command(id) when is_binary(id) do
    if valid_uuid?(id) do
      case Repo.get(Command, id) do
        nil -> {:error, :not_found}
        command -> {:ok, command}
      end
    else
      {:error, :invalid_request}
    end
  end

  def fetch_command(_), do: {:error, :invalid_request}

  @spec machine_owns_runtime?(Ecto.UUID.t(), Ecto.UUID.t()) :: boolean()
  def machine_owns_runtime?(machine_id, runtime_id),
    do: owned?(Runtime, machine_id, runtime_id, :machine_id)

  @spec machine_owns_run?(Ecto.UUID.t(), Ecto.UUID.t()) :: boolean()
  def machine_owns_run?(machine_id, run_id) when is_binary(machine_id) and is_binary(run_id) do
    if valid_uuid?(machine_id) and valid_uuid?(run_id) do
      Repo.exists?(
        from run in Run,
          join: runtime in Runtime,
          on: runtime.id == run.runtime_id,
          where: run.id == ^run_id and runtime.machine_id == ^machine_id
      )
    else
      false
    end
  end

  def machine_owns_run?(_, _), do: false

  @spec machine_owns_command?(Ecto.UUID.t(), Ecto.UUID.t()) :: boolean()
  def machine_owns_command?(machine_id, command_id)
      when is_binary(machine_id) and is_binary(command_id) do
    if valid_uuid?(machine_id) and valid_uuid?(command_id) do
      Repo.exists?(
        from command in Command,
          join: run in Run,
          on: run.id == command.run_id,
          join: runtime in Runtime,
          on: runtime.id == run.runtime_id,
          where: command.id == ^command_id and runtime.machine_id == ^machine_id
      )
    else
      false
    end
  end

  def machine_owns_command?(_, _), do: false

  @spec task_snapshot(Ecto.UUID.t()) ::
          {:ok,
           %{
             task: Task.t(),
             run: Run.t() | nil,
             waiting: map() | nil,
             latest_command: Command.t() | nil
           }}
          | {:error, atom()}
  def task_snapshot(task_id) when is_binary(task_id) do
    if valid_uuid?(task_id) do
      Repo.transaction(fn ->
        task = share_task(task_id)

        run =
          Repo.one(
            from run in Run,
              where: run.task_id == ^task.id and run.generation == ^task.attempt_generation
          )

        %{
          task: task,
          run: run,
          waiting: current_waiting_context(task, run),
          latest_command: latest_task_command(task.id),
          controls: control_capabilities(task, run)
        }
      end)
      |> case do
        {:ok, snapshot} -> {:ok, snapshot}
        {:error, reason} -> {:error, reason}
      end
    else
      {:error, :invalid_request}
    end
  end

  def task_snapshot(_), do: {:error, :invalid_request}

  def control_capabilities(task, run) do
    runtime = run && Repo.get(Runtime, run.runtime_id)
    pending? = not is_nil(run) and pending_supervisory_transition?(run.id)
    control_capabilities(task, run, runtime, pending?)
  end

  def control_capabilities(task, run, runtime, pending?) do
    supported? =
      not is_nil(runtime) and legacy_supervisory_runtime?(runtime) and
        value(runtime.capabilities, :supervisory_control, false) == true

    current? =
      not is_nil(run) and not is_nil(runtime) and
        run.generation == task.attempt_generation and run.state == task.state and
        run.claimed_runtime_epoch == runtime.connection_epoch

    %{
      supervisory_control: supported?,
      can_guide: supported? and current? and task.state in ["running", "paused"],
      can_pause: supported? and current? and task.state == "running" and not pending?,
      can_resume: supported? and current? and task.state == "paused" and not pending?
    }
  end

  @spec list_task_events(Ecto.UUID.t(), keyword()) :: {:ok, map()} | {:error, atom()}
  def list_task_events(task_id, opts \\ []) do
    with_history_task(task_id, opts, :after, fn limit, after_position ->
      query =
        from event in RunEvent,
          join: run in Run,
          on: run.id == event.run_id,
          where: run.task_id == ^task_id,
          order_by: [asc: event.inserted_at, asc: event.id],
          select: %{
            id: event.id,
            run_id: run.id,
            generation: run.generation,
            event_id: event.event_id,
            sequence: event.sequence,
            kind: event.kind,
            payload: event.payload,
            occurred_at: event.occurred_at,
            inserted_at: event.inserted_at
          }

      query
      |> after_position(after_position)
      |> page_query(limit)
      |> Repo.all()
      |> page_with_next(limit, :next_after)
    end)
  end

  @spec list_task_transitions(Ecto.UUID.t(), keyword()) :: {:ok, map()} | {:error, atom()}
  def list_task_transitions(task_id, opts \\ []) do
    with_history_task(task_id, opts, :after, fn limit, after_position ->
      query =
        from transition in RunTransition,
          join: run in Run,
          on: run.id == transition.run_id,
          where: run.task_id == ^task_id,
          order_by: [asc: transition.inserted_at, asc: transition.id],
          select: %{
            id: transition.id,
            run_id: run.id,
            generation: run.generation,
            transition_id: transition.transition_id,
            state: transition.state,
            payload: transition.payload,
            inserted_at: transition.inserted_at
          }

      query
      |> after_position(after_position)
      |> page_query(limit)
      |> Repo.all()
      |> page_with_next(limit, :next_after)
    end)
  end

  @spec list_task_commands(Ecto.UUID.t(), keyword()) :: {:ok, map()} | {:error, atom()}
  def list_task_commands(task_id, opts \\ []) do
    with_history_task(task_id, opts, :after, fn limit, after_position ->
      query =
        from command in Command,
          where: command.task_id == ^task_id,
          order_by: [asc: command.inserted_at, asc: command.id],
          select: %{
            id: command.id,
            task_id: command.task_id,
            run_id: command.run_id,
            generation: command.generation,
            kind: command.kind,
            payload: command.payload,
            state: command.state,
            applied_at: command.applied_at,
            acknowledgement_id: command.acknowledgement_id,
            acknowledgement_outcome: command.acknowledgement_outcome,
            acknowledged_at: command.acknowledged_at,
            inserted_at: command.inserted_at
          }

      query
      |> after_position(after_position)
      |> page_query(limit)
      |> Repo.all()
      |> page_with_next(limit, :next_after)
    end)
  end

  @spec task_timeline(Ecto.UUID.t(), keyword()) :: {:ok, map()} | {:error, atom()}
  def task_timeline(task_id, opts \\ []) do
    with_history_task(task_id, opts, :before, fn limit, before_position ->
      fetch_limit = limit + 1

      entries =
        task_timeline_events(task_id, before_position, fetch_limit) ++
          task_timeline_transitions(task_id, before_position, fetch_limit) ++
          task_timeline_commands(task_id, before_position, fetch_limit)

      entries
      |> Enum.sort(&timeline_precedes?/2)
      |> Enum.take(fetch_limit)
      |> page_with_next(limit, :next_before)
    end)
  end

  defp runtime_read_models(rows) do
    {completed, current} =
      Enum.reduce(rows, {[], nil}, fn row, {completed, current} ->
        case current do
          nil ->
            {completed, runtime_read_model(row)}

          %{runtime_id: runtime_id} when runtime_id == row.runtime.id ->
            {completed, add_active_run(current, row.run)}

          snapshot ->
            {[finish_runtime_read_model(snapshot) | completed], runtime_read_model(row)}
        end
      end)

    completed =
      if is_nil(current), do: completed, else: [finish_runtime_read_model(current) | completed]

    Enum.reverse(completed)
  end

  defp runtime_read_model(%{machine: machine, runtime: runtime, run: run}) do
    %{
      machine_id: machine.id,
      machine_name: machine.name,
      runtime_id: runtime.id,
      runtime_key: runtime.runtime_key,
      runtime_name: runtime.name,
      status: runtime.status,
      last_heartbeat_at: runtime.last_heartbeat_at,
      connection_epoch: runtime.connection_epoch,
      capacity: runtime.capacity,
      reserved_capacity: 0,
      agent_profile: runtime.agent_profile,
      workspace: runtime.workspace,
      repository_resource_id: runtime.repository_resource_id,
      capabilities: runtime.capabilities,
      active_runs: []
    }
    |> add_active_run(run)
  end

  defp add_active_run(snapshot, nil), do: snapshot

  defp add_active_run(snapshot, run) do
    active_run = %{
      run_id: run.id,
      task_id: run.task_id,
      generation: run.generation,
      state: run.state,
      inserted_at: run.inserted_at
    }

    %{
      snapshot
      | reserved_capacity: snapshot.reserved_capacity + 1,
        active_runs: [active_run | snapshot.active_runs]
    }
  end

  defp finish_runtime_read_model(snapshot),
    do: %{snapshot | active_runs: Enum.reverse(snapshot.active_runs)}

  defp current_waiting_context(
         %Task{state: "waiting_for_input", current_generation: generation} = task,
         %Run{state: "waiting_for_input", generation: generation} = run
       ) do
    case Repo.get_by(RunTransition,
           run_id: run.id,
           transition_id: task.waiting_transition_id,
           state: "waiting_for_input"
         ) do
      nil ->
        nil

      transition ->
        {payload, recorded_at} =
          case Repo.one(
                 from event in RunEvent,
                   where: event.run_id == ^run.id and event.kind == "waiting_for_input",
                   order_by: [desc: event.sequence, desc: event.id],
                   limit: 1,
                   select: event
               ) do
            nil ->
              {transition.payload, transition.inserted_at}

            event ->
              if value(transition.payload, :decision),
                do: {transition.payload, transition.inserted_at},
                else: {event.payload, event.inserted_at}
          end

        %{
          run_id: run.id,
          generation: run.generation,
          transition_id: transition.transition_id,
          question: value(payload, :question),
          decision: value(payload, :decision),
          payload: payload,
          recorded_at: recorded_at
        }
    end
  end

  defp current_waiting_context(_, _), do: nil

  defp latest_task_command(task_id) do
    Repo.one(
      from command in Command,
        where: command.task_id == ^task_id,
        order_by: [desc: command.inserted_at, desc: command.id],
        limit: 1
    )
  end

  defp with_history_task(task_id, opts, direction, callback) do
    with true <- valid_uuid?(task_id),
         {:ok, limit} <- history_limit(opts),
         {:ok, position} <- history_position(opts, direction) do
      if Repo.exists?(from task in Task, where: task.id == ^task_id) do
        {:ok, callback.(limit, position)}
      else
        {:error, :not_found}
      end
    else
      _ -> {:error, :invalid_request}
    end
  end

  defp history_limit(opts) when is_list(opts) do
    case Keyword.get(opts, :limit, @default_history_limit) do
      limit when is_integer(limit) and limit > 0 and limit <= @max_history_limit -> {:ok, limit}
      _ -> {:error, :invalid_request}
    end
  end

  defp history_limit(_), do: {:error, :invalid_request}

  defp history_position(opts, :after) when is_list(opts),
    do: validate_history_position(Keyword.get(opts, :after))

  defp history_position(opts, :before) when is_list(opts),
    do: validate_timeline_position(Keyword.get(opts, :before))

  defp history_position(_, _), do: {:error, :invalid_request}

  defp validate_history_position(nil), do: {:ok, nil}

  defp validate_history_position(%{inserted_at: %DateTime{} = inserted_at, id: id})
       when is_binary(id) do
    if valid_uuid?(id),
      do: {:ok, %{inserted_at: inserted_at, id: id}},
      else: {:error, :invalid_request}
  end

  defp validate_history_position(_), do: {:error, :invalid_request}

  defp validate_timeline_position(nil), do: {:ok, nil}

  defp validate_timeline_position(%{
         inserted_at: %DateTime{} = inserted_at,
         source: source,
         id: id
       })
       when is_binary(source) and is_binary(id) do
    if Map.has_key?(@timeline_source_ranks, source) and valid_uuid?(id) do
      {:ok, %{inserted_at: inserted_at, source: source, id: id}}
    else
      {:error, :invalid_request}
    end
  end

  defp validate_timeline_position(_), do: {:error, :invalid_request}

  defp after_position(query, nil), do: query

  defp after_position(query, %{inserted_at: inserted_at, id: id}) do
    where(
      query,
      [entry, ...],
      entry.inserted_at > ^inserted_at or
        (entry.inserted_at == ^inserted_at and entry.id > ^id)
    )
  end

  defp page_query(query, page_limit), do: limit(query, ^(page_limit + 1))

  defp page_with_next(entries, page_limit, cursor_key) do
    {page, remainder} = Enum.split(entries, page_limit)

    next_position =
      if remainder == [] do
        nil
      else
        page |> List.last() |> entry_position()
      end

    %{cursor_key => next_position, entries: page}
  end

  defp entry_position(%{source: source, inserted_at: inserted_at, id: id}),
    do: %{inserted_at: inserted_at, source: source, id: id}

  defp entry_position(%{inserted_at: inserted_at, id: id}),
    do: %{inserted_at: inserted_at, id: id}

  defp task_timeline_events(task_id, before_position, fetch_limit) do
    from(event in RunEvent,
      join: run in Run,
      on: run.id == event.run_id,
      where: run.task_id == ^task_id,
      order_by: [desc: event.inserted_at, desc: event.id],
      select: %{
        source: "event",
        id: event.id,
        run_id: run.id,
        generation: run.generation,
        event_id: event.event_id,
        sequence: event.sequence,
        kind: event.kind,
        payload: event.payload,
        occurred_at: event.occurred_at,
        inserted_at: event.inserted_at
      }
    )
    |> timeline_before_position(before_position, "event")
    |> limit(^fetch_limit)
    |> Repo.all()
  end

  defp task_timeline_transitions(task_id, before_position, fetch_limit) do
    from(transition in RunTransition,
      join: run in Run,
      on: run.id == transition.run_id,
      where: run.task_id == ^task_id,
      order_by: [desc: transition.inserted_at, desc: transition.id],
      select: %{
        source: "transition",
        id: transition.id,
        run_id: run.id,
        generation: run.generation,
        transition_id: transition.transition_id,
        state: transition.state,
        payload: transition.payload,
        inserted_at: transition.inserted_at
      }
    )
    |> timeline_before_position(before_position, "transition")
    |> limit(^fetch_limit)
    |> Repo.all()
  end

  defp task_timeline_commands(task_id, before_position, fetch_limit) do
    from(command in Command,
      where: command.task_id == ^task_id,
      order_by: [desc: command.inserted_at, desc: command.id],
      select: %{
        source: "command",
        id: command.id,
        run_id: command.run_id,
        generation: command.generation,
        kind: command.kind,
        payload: command.payload,
        state: command.state,
        applied_at: command.applied_at,
        acknowledgement_id: command.acknowledgement_id,
        acknowledgement_outcome: command.acknowledgement_outcome,
        acknowledged_at: command.acknowledged_at,
        inserted_at: command.inserted_at
      }
    )
    |> timeline_before_position(before_position, "command")
    |> limit(^fetch_limit)
    |> Repo.all()
  end

  defp timeline_before_position(query, nil, _source), do: query

  defp timeline_before_position(
         query,
         %{inserted_at: inserted_at, source: source, id: id},
         entry_source
       ) do
    source_rank = Map.fetch!(@timeline_source_ranks, entry_source)
    cursor_rank = Map.fetch!(@timeline_source_ranks, source)

    case source_rank - cursor_rank do
      difference when difference > 0 ->
        where(
          query,
          [entry, ...],
          entry.inserted_at < ^inserted_at or entry.inserted_at == ^inserted_at
        )

      0 ->
        where(
          query,
          [entry, ...],
          entry.inserted_at < ^inserted_at or
            (entry.inserted_at == ^inserted_at and entry.id < ^id)
        )

      _ ->
        where(query, [entry, ...], entry.inserted_at < ^inserted_at)
    end
  end

  defp timeline_precedes?(left, right) do
    case DateTime.compare(left.inserted_at, right.inserted_at) do
      :gt ->
        true

      :lt ->
        false

      :eq ->
        left_rank = Map.fetch!(@timeline_source_ranks, left.source)
        right_rank = Map.fetch!(@timeline_source_ranks, right.source)

        cond do
          left_rank < right_rank -> true
          left_rank > right_rank -> false
          true -> left.id > right.id
        end
    end
  end

  @spec assignment_target(Run.t()) ::
          {:ok, %{machine_id: Ecto.UUID.t(), runtime_id: Ecto.UUID.t()}} | {:error, :not_found}
  def assignment_target(%Run{id: run_id}), do: assignment_target(run_id)

  def assignment_target(run_id) when is_binary(run_id) do
    if valid_uuid?(run_id) do
      case Repo.one(
             from run in Run,
               join: runtime in Runtime,
               on: runtime.id == run.runtime_id,
               where: run.id == ^run_id,
               select: %{machine_id: runtime.machine_id, runtime_id: run.runtime_id}
           ) do
        nil -> {:error, :not_found}
        target -> {:ok, target}
      end
    else
      {:error, :not_found}
    end
  end

  def assignment_target(_), do: {:error, :not_found}

  @spec assign_one(keyword()) :: {:ok, Run.t()} | {:error, :no_assignment}
  def assign_one(opts \\ []) do
    current = now(opts)
    assignment_duration_ms = Keyword.get(opts, :assignment_duration_ms, 30_000)

    result =
      case assign_one_goal(current, assignment_duration_ms) do
        {:error, :no_assignment} -> assign_one_legacy(current, assignment_duration_ms)
        result -> result
      end

    case result do
      {:ok, run} ->
        emit([:run, :assigned], %{
          task_id: run.task_id,
          run_id: run.id,
          runtime_id: run.runtime_id,
          generation: run.generation
        })

        result

      _ ->
        result
    end
  end

  # Goal candidates are scanned by a stable keyset. Each candidate gets its own
  # transaction so an unavailable or malformed frozen validation binding cannot
  # retain locks or starve a later eligible Goal task.
  defp assign_one_goal(current, assignment_duration_ms),
    do: assign_one_goal(current, assignment_duration_ms, nil)

  defp assign_one_goal(current, assignment_duration_ms, cursor) do
    case next_goal_task_candidate_page(current, cursor) do
      [] ->
        {:error, :no_assignment}

      candidates ->
        case assign_goal_candidate_page(candidates, current, assignment_duration_ms) do
          {:ok, run} -> {:ok, run}
          :skip -> assign_one_goal(current, assignment_duration_ms, List.last(candidates))
          error -> error
        end
    end
  end

  defp next_goal_task_candidate_page(current, cursor) do
    query =
      from task in Task,
        join: goal in Goal,
        on: goal.id == task.goal_id,
        join: revision in GoalRevision,
        on: revision.goal_id == goal.id and revision.revision == goal.current_revision,
        left_join: item in WorkItem,
        on: item.id == task.work_item_id,
        where: task.state == "queued" and not is_nil(task.goal_id),
        where: task.inserted_at <= ^current,
        where:
          task.goal_revision == goal.current_revision and
            ((goal.state == "draft" and task.purpose == "plan" and is_nil(task.work_item_id)) or
               (goal.state == "active" and task.purpose != "plan" and
                  not is_nil(task.work_item_id))),
        where: task.attempt_generation <= task.max_run_attempts,
        order_by: [asc: task.inserted_at, asc: task.id],
        limit: ^@goal_assignment_scan_page_size,
        select: %{
          goal_id: task.goal_id,
          task_id: task.id,
          work_item_id: task.work_item_id,
          inserted_at: task.inserted_at
        }

    query =
      case cursor do
        nil ->
          query

        %{inserted_at: inserted_at, task_id: task_id} ->
          where(
            query,
            [task],
            task.inserted_at > ^inserted_at or
              (task.inserted_at == ^inserted_at and task.id > ^task_id)
          )
      end

    Repo.all(query)
  end

  defp assign_goal_candidate_page([], _current, _assignment_duration_ms), do: :skip

  defp assign_goal_candidate_page([candidate | remaining], current, assignment_duration_ms) do
    case try_assign_goal_candidate(candidate, current, assignment_duration_ms) do
      {:ok, run} -> {:ok, run}
      :skip -> assign_goal_candidate_page(remaining, current, assignment_duration_ms)
      error -> error
    end
  end

  defp try_assign_goal_candidate(candidate, current, assignment_duration_ms) do
    Repo.transaction(fn ->
      with {:ok, _project, goal, item, revision, task} <-
             try_lock_goal_assignment_chain(candidate),
           true <- goal_task_assignable?(goal, task) and goal_work_item_owned?(goal, item, task),
           %Runtime{} = runtime <- next_goal_runtime(task, revision, item, current),
           true <- runtime_has_capacity?(runtime) do
        {:assigned, assign_task_to_runtime!(task, runtime, item, current, assignment_duration_ms)}
      else
        _ -> :skip
      end
    end)
    |> case do
      {:ok, {:assigned, run}} -> {:ok, run}
      {:ok, :skip} -> :skip
      {:error, reason} when reason in [:no_assignment, :ownership_lost, :stale_revision] -> :skip
      {:error, reason} -> {:error, reason}
    end
  end

  defp try_lock_goal_assignment_chain(candidate) do
    with %Project{} = project <- try_lock_goal_project(candidate.goal_id),
         true <- project.status == "active",
         %Goal{} = goal <- try_lock_goal(candidate.goal_id),
         item <- try_lock_goal_task_item(candidate.work_item_id),
         true <- is_nil(item) or match?(%WorkItem{}, item),
         %GoalRevision{} = revision <- try_lock_goal_revision(goal),
         %Task{} = task <- try_lock_task(candidate.task_id) do
      {:ok, project, goal, item, revision, task}
    else
      _ -> :skip
    end
  end

  defp try_lock_goal_project(goal_id) do
    with project_id when is_binary(project_id) <-
           Repo.one(from(goal in Goal, where: goal.id == ^goal_id, select: goal.project_id)) do
      Repo.one(
        from(project in Project,
          where: project.id == ^project_id,
          lock: "FOR SHARE SKIP LOCKED"
        )
      )
    end
  end

  defp try_lock_goal(goal_id),
    do: Repo.one(from goal in Goal, where: goal.id == ^goal_id, lock: "FOR UPDATE SKIP LOCKED")

  defp try_lock_work_item(work_item_id),
    do:
      Repo.one(
        from item in WorkItem, where: item.id == ^work_item_id, lock: "FOR UPDATE SKIP LOCKED"
      )

  defp try_lock_goal_task_item(nil), do: nil
  defp try_lock_goal_task_item(work_item_id), do: try_lock_work_item(work_item_id)

  defp try_lock_goal_revision(goal) do
    Repo.one(
      from revision in GoalRevision,
        where: revision.goal_id == ^goal.id and revision.revision == ^goal.current_revision,
        lock: "FOR SHARE SKIP LOCKED"
    )
  end

  defp try_lock_task(task_id),
    do: Repo.one(from task in Task, where: task.id == ^task_id, lock: "FOR UPDATE SKIP LOCKED")

  defp assign_one_legacy(current, assignment_duration_ms) do
    Repo.transaction(fn ->
      case next_assignable_legacy_task_and_runtime(current) do
        nil ->
          rollback(:no_assignment)

        {task, runtime} ->
          assign_task_to_runtime!(task, runtime, nil, current, assignment_duration_ms)
      end
    end)
  end

  defp assign_task_to_runtime!(task, runtime, item, current, assignment_duration_ms) do
    generation = task.attempt_generation

    task =
      task
      |> Task.changeset(%{
        state: "assigned",
        current_generation: generation,
        waiting_transition_id: nil
      })
      |> stamp_update(current)
      |> Repo.update!()

    run =
      %Run{}
      |> Run.changeset(%{
        task_id: task.id,
        runtime_id: runtime.id,
        generation: generation,
        state: "assigned",
        assigned_at: current,
        assignment_expires_at: DateTime.add(current, assignment_duration_ms, :millisecond)
      })
      |> stamp_insert(current)
      |> Repo.insert!()

    reserve_requested_session!(task, runtime, item, run, current)
  end

  defp runtime_has_capacity?(runtime) do
    Repo.aggregate(
      from(run in Run,
        where: run.runtime_id == ^runtime.id and run.state in ^@capacity_bearing_states
      ),
      :count
    ) < runtime.capacity
  end

  defp next_assignable_legacy_task_and_runtime(current) do
    Repo.one(
      from task in Task,
        join: runtime in Runtime,
        on:
          runtime.status == "online" and runtime.agent_profile == task.agent_profile and
            runtime.workspace == task.workspace and
            fragment("? @> ?", runtime.capabilities, task.required_capabilities),
        where: task.state == "queued" and is_nil(task.goal_id),
        where:
          fragment(
            "?->>'supervisory_control' IS DISTINCT FROM 'true'",
            task.required_capabilities
          ) or
            is_nil(runtime.harness_kind) or runtime.harness_kind == "generic",
        where:
          fragment(
            "? > CAST(? AS timestamp) - (? * INTERVAL '3 milliseconds')",
            runtime.last_heartbeat_at,
            ^current,
            runtime.heartbeat_interval_ms
          ),
        where:
          fragment(
            "(SELECT count(*) FROM runs AS active_run WHERE active_run.runtime_id = ? AND active_run.state = ANY(CAST(? AS text[]))) < ?",
            runtime.id,
            ^@capacity_bearing_states,
            runtime.capacity
          ),
        order_by: [asc: task.inserted_at, asc: runtime.inserted_at],
        limit: 1,
        lock: "FOR UPDATE SKIP LOCKED",
        select: {task, runtime}
    )
  end

  defp next_goal_runtime(task, revision, item, current) do
    case {goal_task_repository_resource_id(task, item),
          allowed_runtime_ids(revision.execution_policy || %{}),
          validation_runtime_ids_for_task(task), native_goal_session_requirement(task)} do
      {nil, _, _, _} ->
        nil

      {_, :invalid, _, _} ->
        nil

      {_, _, :invalid, _} ->
        nil

      {_, _, _, :invalid} ->
        nil

      {repository_resource_id, {:ok, allowed_runtime_ids}, {:ok, validation_runtime_ids},
       session_requirement} ->
        adapter_requirement = native_goal_adapter_requirement()

        strict_budget_requirement =
          strict_budget_capability_requirement(revision.execution_policy)

        query =
          from runtime in Runtime,
            where:
              runtime.status == "online" and runtime.agent_profile == ^task.agent_profile and
                runtime.workspace == ^task.workspace and
                runtime.repository_resource_id == ^repository_resource_id and
                runtime.harness_kind in ^@native_harness_kinds,
            where:
              fragment("? @> ?", runtime.capabilities, type(^task.required_capabilities, :map)),
            where: fragment("? @> ?", runtime.capabilities, type(^adapter_requirement, :map)),
            where: fragment("? @> ?", runtime.capabilities, type(^session_requirement, :map)),
            where:
              fragment("? @> ?", runtime.capabilities, type(^strict_budget_requirement, :map)),
            where:
              fragment(
                "? > CAST(? AS timestamp) - (? * INTERVAL '3 milliseconds')",
                runtime.last_heartbeat_at,
                ^current,
                runtime.heartbeat_interval_ms
              ),
            where:
              fragment(
                "(SELECT count(*) FROM runs AS active_run WHERE active_run.runtime_id = ? AND active_run.state = ANY(CAST(? AS text[]))) < ?",
                runtime.id,
                ^@capacity_bearing_states,
                runtime.capacity
              ),
            order_by: [asc: runtime.inserted_at, asc: runtime.id],
            limit: 1,
            lock: "FOR UPDATE SKIP LOCKED"

        query =
          if allowed_runtime_ids == [],
            do: query,
            else: where(query, [runtime], runtime.id in ^allowed_runtime_ids)

        query =
          if is_nil(validation_runtime_ids),
            do: query,
            else: where(query, [runtime], runtime.id in ^validation_runtime_ids)

        query = restrict_to_requested_session(query, task, item)
        query = restrict_to_handoff_source_machine(query, task)

        case Repo.one(query) do
          nil ->
            nil

          runtime ->
            if goal_runtime_matches?(runtime, task, revision, item), do: runtime, else: nil
        end
    end
  end

  # A retained native session is machine-local and must not fall back to another
  # runtime merely because it matches the task profile.
  defp restrict_to_requested_session(query, %Task{requested_session_id: nil}, _item), do: query

  defp restrict_to_requested_session(query, %Task{} = task, item) do
    repository_resource_id =
      goal_task_repository_resource_id(task, item) || rollback(:ownership_lost)

    retained_runtime =
      from session in HarnessSession,
        join: runtime in Runtime,
        on: runtime.id == session.runtime_id,
        where:
          session.id == ^task.requested_session_id and session.state == "available" and
            session.binding_verified == true and is_nil(session.active_run_id) and
            session.repository_resource_id == ^repository_resource_id and
            session.machine_id == runtime.machine_id and
            session.harness_kind == runtime.harness_kind and
            session.harness_version == runtime.harness_version and
            session.adapter_version == runtime.adapter_version,
        select: session.runtime_id

    from runtime in query,
      where: runtime.id == subquery(retained_runtime)
  end

  # Handoff starts a fresh native session, but its verified artifact belongs to
  # the source machine. Selecting another machine would turn a local artifact
  # pointer into an unsupported cross-machine transfer.
  defp restrict_to_handoff_source_machine(query, %Task{handoff_source_run_id: nil}), do: query

  defp restrict_to_handoff_source_machine(query, %Task{} = task) do
    source_machine_id =
      Repo.one(
        from(source_run in Run,
          join: source_runtime in Runtime,
          on: source_runtime.id == source_run.runtime_id,
          where: source_run.id == ^task.handoff_source_run_id,
          select: source_runtime.machine_id
        )
      )

    if is_binary(source_machine_id),
      do: where(query, [runtime], runtime.machine_id == ^source_machine_id),
      else: where(query, [runtime], false)
  end

  # The scheduler owns each retained attachment identity. It is persisted with
  # the Run before dispatch so a daemon cannot choose or reuse a stale binding.
  defp reserve_requested_session!(
         %Task{requested_session_id: nil},
         _runtime,
         _item,
         run,
         _current
       ),
       do: run

  defp reserve_requested_session!(task, runtime, item, run, current) do
    repository_resource_id =
      goal_task_repository_resource_id(task, item) || rollback(:ownership_lost)

    session =
      Repo.one(
        from session in HarnessSession,
          where:
            session.id == ^task.requested_session_id and session.runtime_id == ^runtime.id and
              session.machine_id == ^runtime.machine_id and session.binding_verified == true and
              session.state == "available" and
              is_nil(session.active_run_id) and
              session.repository_resource_id == ^repository_resource_id and
              session.harness_kind == ^runtime.harness_kind and
              session.harness_version == ^runtime.harness_version and
              session.adapter_version == ^runtime.adapter_version,
          lock: "FOR UPDATE"
      ) || rollback(:no_assignment)

    binding_id = Ecto.UUID.generate()

    session
    |> HarnessSession.update_changeset(%{
      state: "busy",
      active_run_id: run.id,
      binding_id: binding_id
    })
    |> stamp_update(current)
    |> Repo.update!()

    run
    |> Ecto.Changeset.change(harness_session_id: session.id, harness_binding_id: binding_id)
    |> stamp_update(current)
    |> Repo.update!()
  end

  defp release_reserved_session!(%Run{harness_session_id: nil}, _current), do: :ok

  defp release_reserved_session!(run, current) do
    session =
      Repo.one(
        from session in HarnessSession,
          where: session.id == ^run.harness_session_id,
          lock: "FOR UPDATE"
      )

    if session && session.state == "busy" && session.active_run_id == run.id &&
         session.binding_id == run.harness_binding_id do
      session
      |> HarnessSession.update_changeset(%{state: "available", active_run_id: nil})
      |> stamp_update(current)
      |> Repo.update!()
    end
  end

  defp mark_reserved_session_unavailable!(%Run{harness_session_id: nil}, _current), do: :ok

  defp mark_reserved_session_unavailable!(run, current) do
    session =
      Repo.one(
        from session in HarnessSession,
          where: session.id == ^run.harness_session_id,
          lock: "FOR UPDATE"
      )

    if session && session.state == "busy" && session.active_run_id == run.id &&
         session.binding_id == run.harness_binding_id do
      session
      |> HarnessSession.update_changeset(%{state: "unavailable", active_run_id: nil})
      |> stamp_update(current)
      |> Repo.update!()
    end
  end

  defp goal_task_current?(goal, task) do
    task.goal_id == goal.id and task.goal_revision == goal.current_revision and
      ((goal.state == "draft" and planning_task?(task)) or
         (goal.state == "active" and task.purpose != "plan"))
  end

  defp planning_task?(%Task{purpose: "plan", work_item_id: nil, validation_of_task_id: nil}),
    do: true

  defp planning_task?(_task), do: false

  defp goal_task_repository_resource_id(%Task{} = task, nil) do
    if planning_task?(task) do
      task.input
      |> value(:subject)
      |> value(:resource_id)
      |> case do
        resource_id when is_binary(resource_id) ->
          if valid_uuid?(resource_id), do: resource_id, else: nil

        _ ->
          nil
      end
    end
  end

  defp goal_task_repository_resource_id(_task, %WorkItem{} = item),
    do: item.repository_resource_id

  defp goal_task_repository_resource_id(_task, _item), do: nil

  defp goal_work_item_owned?(goal, nil, task) do
    planning_task?(task) and task.goal_id == goal.id
  end

  defp goal_work_item_owned?(goal, item, task) do
    task.goal_id == goal.id and task.work_item_id == item.id and item.goal_id == goal.id and
      item.admitted_revision == task.goal_revision
  end

  defp goal_task_assignable?(goal, task) do
    goal_task_current?(goal, task) and task.state == "queued" and
      task.current_generation < task.attempt_generation and
      task.attempt_generation <= task.max_run_attempts
  end

  defp goal_runtime_matches?(runtime, task, revision, item) do
    runtime_matches_task?(runtime, task) and
      goal_subject_matches_resource?(task, goal_task_repository_resource_id(task, item)) and
      runtime_matches_goal_resource?(
        runtime,
        task.goal_id,
        goal_task_repository_resource_id(task, item)
      ) and
      runtime_allowed_for_goal?(runtime, revision) and
      validation_runtime_allowed?(runtime, task) and
      handoff_source_machine_matches?(runtime, task) and
      native_goal_adapter_capable?(runtime, task, revision)
  end

  # Validation profile authorization is frozen into the admitted ContextSnapshot.
  # Never consult the live profile registry while assigning or claiming a run.
  defp validation_runtime_allowed?(runtime, task) do
    case validation_runtime_ids_for_task(task) do
      {:ok, nil} -> true
      {:ok, runtime_ids} -> runtime.id in runtime_ids
      :invalid -> false
    end
  end

  defp validation_runtime_ids_for_task(%Task{purpose: "validate"} = task) do
    snapshot =
      Repo.one(
        from snapshot in ContextSnapshot,
          where:
            snapshot.id == ^task.context_snapshot_id and snapshot.goal_id == ^task.goal_id and
              snapshot.goal_revision == ^task.goal_revision and
              snapshot.work_item_id == ^task.work_item_id
      )

    with %ContextSnapshot{} = snapshot <- snapshot,
         work_contract when is_map(work_contract) <-
           value(snapshot.payload || %{}, :work_contract),
         bindings when is_list(bindings) <-
           value(work_contract, :validation_bindings),
         {:ok, runtime_ids} <- validation_binding_runtime_ids(bindings) do
      {:ok, runtime_ids}
    else
      _ -> :invalid
    end
  end

  defp validation_runtime_ids_for_task(_task), do: {:ok, nil}

  defp validation_binding_runtime_ids([]), do: {:ok, nil}

  defp validation_binding_runtime_ids(bindings),
    do: intersect_validation_binding_runtime_ids(bindings)

  defp intersect_validation_binding_runtime_ids(bindings) do
    with true <- Enum.all?(bindings, &valid_validation_binding?/1),
         identities <- Enum.map(bindings, &{value(&1, :profile_name), value(&1, :kind)}),
         true <- length(identities) == MapSet.size(MapSet.new(identities)) do
      bindings
      |> Enum.map(&(value(&1, :allowed_runtime_ids) |> MapSet.new()))
      |> Enum.reduce_while(nil, fn runtime_ids, intersection ->
        intersection =
          if is_nil(intersection),
            do: runtime_ids,
            else: MapSet.intersection(intersection, runtime_ids)

        if MapSet.size(intersection) == 0,
          do: {:halt, :invalid},
          else: {:cont, intersection}
      end)
      |> case do
        :invalid -> :invalid
        nil -> :invalid
        runtime_ids -> {:ok, MapSet.to_list(runtime_ids)}
      end
    else
      _ -> :invalid
    end
  end

  defp valid_validation_binding?(binding) when is_map(binding) do
    runtime_ids = value(binding, :allowed_runtime_ids)

    exact_map_keys?(binding, ["profile_name", "kind", "profile_digest", "allowed_runtime_ids"]) and
      is_binary(value(binding, :profile_name)) and
      Regex.match?(@validation_profile_name, value(binding, :profile_name)) and
      value(binding, :kind) in ["check", "review"] and
      is_binary(value(binding, :profile_digest)) and
      Regex.match?(@sha256, value(binding, :profile_digest)) and
      is_list(runtime_ids) and runtime_ids != [] and
      Enum.all?(runtime_ids, &canonical_runtime_uuid?/1) and
      length(runtime_ids) == MapSet.size(MapSet.new(runtime_ids))
  end

  defp valid_validation_binding?(_binding), do: false

  defp canonical_runtime_uuid?(value) when is_binary(value) do
    match?({:ok, ^value}, Ecto.UUID.cast(value))
  end

  defp canonical_runtime_uuid?(_value), do: false

  defp goal_subject_matches_resource?(%Task{} = task, repository_resource_id)
       when is_binary(repository_resource_id) do
    subject = value(task.input || %{}, :subject)
    is_map(subject) and value(subject, :resource_id) == repository_resource_id
  end

  defp goal_subject_matches_resource?(_task, _repository_resource_id), do: false

  defp runtime_matches_goal_resource?(runtime, goal_id, repository_resource_id)
       when is_binary(goal_id) and is_binary(repository_resource_id) do
    runtime.repository_resource_id == repository_resource_id and
      Repo.exists?(
        from repository in ProjectResource,
          join: goal in Goal,
          on: goal.project_id == repository.project_id,
          where:
            repository.id == ^repository_resource_id and repository.kind == "repository" and
              goal.id == ^goal_id
      )
  end

  defp runtime_matches_goal_resource?(_runtime, _goal_id, _repository_resource_id), do: false

  defp runtime_allowed_for_goal?(runtime, revision) do
    case allowed_runtime_ids(revision.execution_policy || %{}) do
      {:ok, []} -> true
      {:ok, allowed_runtime_ids} -> runtime.id in allowed_runtime_ids
      :invalid -> false
    end
  end

  defp allowed_runtime_ids(policy) do
    case value(policy, :allowed_runtime_ids, []) do
      ids when is_list(ids) ->
        if Enum.all?(ids, &valid_uuid?/1), do: {:ok, ids}, else: :invalid

      _ ->
        :invalid
    end
  end

  defp native_goal_adapter_requirement do
    %{
      "adapter" => %{
        "operations" => %{
          "start" => true,
          "events" => true,
          "cancel" => true,
          "pause" => "unsupported"
        }
      }
    }
  end

  defp native_goal_resume_requirement do
    %{
      "adapter" => %{
        "operations" => %{
          "resume" => true
        }
      }
    }
  end

  defp native_goal_handoff_requirement do
    %{
      "adapter" => %{
        "operations" => %{
          "handoff" => true
        }
      }
    }
  end

  defp native_goal_session_requirement(task) do
    case value(task.input || %{}, :session_mode, "fresh") do
      "fresh" -> %{}
      "resume" -> native_goal_resume_requirement()
      "handoff" -> native_goal_handoff_requirement()
      _ -> :invalid
    end
  end

  defp native_goal_adapter_capable?(runtime, task, revision) do
    adapter = value(runtime.capabilities, :adapter)
    operations = value(adapter, :operations)
    session_mode = value(task.input || %{}, :session_mode, "fresh")

    valid_runtime_metadata?(runtime) and is_map(adapter) and
      adapter_matches_runtime_metadata?(adapter, runtime) and
      valid_adapter_operations?(operations) and
      value(operations, :start) == true and value(operations, :events) == true and
      value(operations, :cancel) == true and value(operations, :pause) == "unsupported" and
      value(runtime.capabilities, :supervisory_control, false) != true and
      (value(revision.execution_policy || %{}, :budget_mode, "soft") != "strict" or
         value(operations, :hard_cost_limit) == true) and
      (session_mode != "resume" or value(operations, :resume) == true) and
      (session_mode != "handoff" or value(operations, :handoff) == true)
  end

  defp strict_budget_capability_requirement(execution_policy) do
    if value(execution_policy || %{}, :budget_mode, "soft") == "strict" do
      %{"adapter" => %{"operations" => %{"hard_cost_limit" => true}}}
    else
      %{}
    end
  end

  @spec assign_all(keyword()) :: {:ok, [Run.t()]}
  def assign_all(opts \\ []) do
    assign_all([], opts)
  end

  defp assign_all(runs, opts) do
    case assign_one(opts) do
      {:ok, run} -> assign_all([run | runs], opts)
      {:error, :no_assignment} -> {:ok, Enum.reverse(runs)}
    end
  end

  @spec claim(Ecto.UUID.t(), map(), keyword()) :: {:ok, Run.t()} | {:error, atom()}
  def claim(run_id, request, opts \\ []) do
    case claim_with_disposition(run_id, request, opts) do
      {:ok, run, :created} ->
        emit_claimed(run)
        {:ok, run}

      {:ok, run, :replayed} ->
        {:ok, run}

      error ->
        error
    end
  end

  @doc false
  @spec claim_with_disposition(Ecto.UUID.t(), map(), keyword()) ::
          {:ok, Run.t(), :created | :replayed} | {:error, atom()}
  def claim_with_disposition(run_id, request, opts) when is_map(request) do
    with true <- valid_uuid?(run_id) and valid_claim_request?(request),
         {:ok, lease_duration_ms} <- lease_duration_ms(opts) do
      current = now(opts)

      result =
        Repo.transaction(fn ->
          {task, run, runtime, goal, item} = lock_claim_chain(run_id)
          request_runtime_id = value(request, :runtime_id)
          request_epoch = value(request, :runtime_epoch)
          request_generation = value(request, :generation)
          request_claim_id = value(request, :claim_id)

          if replayed_claim?(task, run, runtime, request, current) do
            run
          else
            ensure_requested_session_claim_binding!(task, runtime, item, run)
            ensure_new_goal_claim_authority!(goal, item, task, runtime)

            cond do
              run.runtime_id != request_runtime_id or runtime.id != request_runtime_id ->
                rollback(:ownership_lost)

              runtime.connection_epoch != request_epoch or
                task.current_generation != request_generation or
                  run.generation != request_generation ->
                rollback(:ownership_lost)

              not runtime_matches_task?(runtime, task) ->
                rollback(:ownership_lost)

              run.state != "assigned" ->
                rollback(:ownership_lost)

              DateTime.compare(run.assignment_expires_at, current) != :gt ->
                rollback(:assignment_expired)

              task.state != "assigned" ->
                rollback(:ownership_lost)

              true ->
                lease_expires_at = DateTime.add(current, lease_duration_ms, :millisecond)

                run =
                  run
                  |> Run.changeset(%{
                    state: "claimed",
                    claimed_runtime_epoch: request_epoch,
                    claim_id: request_claim_id,
                    lease_token: Ecto.UUID.generate(),
                    claimed_at: current,
                    lease_expires_at: lease_expires_at
                  })
                  |> stamp_update(current)
                  |> Repo.update!()

                task
                |> Task.changeset(%{state: "claimed"})
                |> stamp_update(current)
                |> Repo.update!()

                {:created, run}
            end
          end
        end)
        |> case do
          {:ok, {:created, run}} -> {:created, run}
          {:ok, run} -> {:replayed, run}
          error -> error
        end

      case result do
        {:created, run} ->
          {:ok, run, :created}

        {:replayed, run} ->
          {:ok, run, :replayed}

        error ->
          error
      end
    else
      _ -> {:error, :invalid_request}
    end
  end

  def claim_with_disposition(_, _, _), do: {:error, :invalid_request}

  @doc false
  @spec emit_claimed(Run.t()) :: :ok
  def emit_claimed(%Run{} = run) do
    emit([:run, :claimed], %{
      task_id: run.task_id,
      run_id: run.id,
      runtime_id: run.runtime_id,
      generation: run.generation
    })
  end

  @spec renew_lease(Ecto.UUID.t(), map(), keyword()) :: {:ok, Run.t()} | {:error, atom()}
  def renew_lease(run_id, fence, opts \\ [])

  def renew_lease(run_id, fence, opts) when is_map(fence) do
    with true <- valid_uuid?(run_id) and valid_fence?(fence),
         {:ok, lease_duration_ms} <- lease_duration_ms(opts) do
      current = now(opts)

      Repo.transaction(fn ->
        {task, run, runtime} = lock_execution_chain(run_id)
        if run.state == "cancelling", do: rollback(:ownership_lost)
        ensure_fence!(task, run, runtime, fence, current)

        run
        |> Run.changeset(%{
          lease_expires_at: DateTime.add(current, lease_duration_ms, :millisecond)
        })
        |> stamp_update(current)
        |> Repo.update!()
      end)
    else
      _ -> {:error, :invalid_request}
    end
  end

  def renew_lease(_, _, _), do: {:error, :invalid_request}

  @spec append_events(Ecto.UUID.t(), map(), [map()], keyword()) ::
          {:ok, [RunEvent.t()]} | {:error, atom()}
  def append_events(run_id, fence, events, opts \\ [])

  def append_events(run_id, fence, events, opts) when is_map(fence) and is_list(events) do
    if not (valid_uuid?(run_id) and valid_fence?(fence) and
              Enum.all?(events, &valid_event?/1)) do
      {:error, :invalid_request}
    else
      current = now(opts)

      Repo.transaction(fn ->
        {task, run, runtime, goal, item} = lock_goal_chain(run_id)
        ensure_fence!(task, run, runtime, fence, current)

        event_ids = Enum.map(events, &value(&1, :event_id))

        existing_events =
          Repo.all(
            from event in RunEvent,
              where: event.run_id == ^run.id and event.event_id in ^event_ids
          )
          |> Map.new(&{&1.event_id, &1})

        case event_replay(events, existing_events) do
          {:replayed, stored} ->
            stored

          :missing ->
            ensure_current_goal_execution_if_present!(goal, item, task)

            Enum.each(events, fn event ->
              if value(event, :kind) == "waiting_for_input" do
                validate_decision_packet!(task, value(event, :payload, %{}))
              end
            end)

            {stored, _events_by_id} =
              Enum.map_reduce(events, existing_events, fn event, events_by_id ->
                event_id = value(event, :event_id)
                body = event_body(event)
                {event_hash, event_hash_version} = RequestHash.write(body)

                case Map.get(events_by_id, event_id) do
                  nil ->
                    stored_event =
                      %RunEvent{}
                      |> RunEvent.changeset(%{
                        run_id: run.id,
                        event_id: event_id,
                        request_hash: event_hash,
                        request_hash_version: event_hash_version,
                        sequence: value(event, :sequence),
                        kind: value(event, :kind),
                        payload: value(event, :payload, %{}),
                        occurred_at: value(event, :occurred_at, current)
                      })
                      |> stamp_insert(current)
                      |> Repo.insert!()

                    {stored_event, Map.put(events_by_id, event_id, stored_event)}

                  %RunEvent{} = existing ->
                    {existing, events_by_id}
                end
              end)

            stored

          :conflict ->
            rollback(:idempotency_conflict)
        end
      end)
    end
  end

  def append_events(_, _, _, _), do: {:error, :invalid_request}

  @spec transition(Ecto.UUID.t(), map(), String.t(), map(), String.t(), keyword()) ::
          {:ok, Run.t()} | {:error, atom()}
  def transition(run_id, fence, target_state, payload, transition_id, opts \\ [])

  def transition(run_id, fence, target_state, payload, transition_id, opts)
      when is_map(fence) and is_binary(target_state) and is_map(payload) and
             is_binary(transition_id) do
    if not (valid_uuid?(run_id) and valid_fence?(fence) and valid_uuid?(transition_id) and
              jsonb_compatible?(payload)) do
      {:error, :invalid_request}
    else
      current = now(opts)
      body = %{state: target_state, payload: payload}
      {body_hash, body_hash_version} = RequestHash.write(body)
      notify? = not Repo.in_transaction?()

      result =
        Repo.transaction(fn ->
          {task, run, runtime, goal, item} = lock_transition_chain(run_id, target_state)
          ensure_transition_static_fence!(task, run, runtime, fence, target_state)

          case Repo.get_by(RunTransition, run_id: run.id, transition_id: transition_id) do
            %RunTransition{} = transition ->
              if RequestHash.matches?(
                   transition.request_hash,
                   transition.request_hash_version,
                   body
                 ),
                 do: {:replayed, transition_response(run, transition)},
                 else: rollback(:idempotency_conflict)

            nil ->
              ensure_current_goal_transition_authority!(goal, item, task, target_state)
              ensure_cancelled_transition_authority!(run, target_state)
              ensure_transition_fence!(task, run, runtime, fence, target_state, current)
              if target_state == "waiting_for_input", do: validate_decision_packet!(task, payload)

              {:created,
               transition_once!(
                 task,
                 run,
                 target_state,
                 payload,
                 transition_id,
                 body_hash,
                 body_hash_version,
                 current,
                 goal
               )}
          end
        end)

      case result do
        {:ok, {:created, {run, receipt}}} ->
          emit([:run, :transition], %{
            run_id: run.id,
            generation: run.generation,
            state: target_state
          })

          notify_goal_task_terminal(receipt, opts, notify?)
          {:ok, run}

        {:ok, {:replayed, run}} ->
          {:ok, run}

        error ->
          error
      end
    end
  end

  def transition(_, _, _, _, _, _), do: {:error, :invalid_request}

  @spec create_command(Ecto.UUID.t(), String.t(), map(), String.t(), keyword()) ::
          {:ok, Command.t(), :created | :replayed} | {:error, atom()}
  def create_command(task_id, kind, payload, idempotency_key, opts \\ [])

  def create_command(task_id, kind, payload, idempotency_key, opts)
      when is_binary(task_id) and is_binary(kind) and is_map(payload) and
             is_binary(idempotency_key) and byte_size(idempotency_key) > 0 do
    if not (valid_uuid?(task_id) and valid_command_request?(kind, payload)) do
      {:error, :invalid_request}
    else
      normalized_payload = normalize_command_payload(kind, payload)
      command_hash = command_request_hash(kind, normalized_payload, opts)
      current = now(opts)
      notify? = not Repo.in_transaction?()

      Repo.transaction(fn ->
        task = lock_task(task_id)

        if not is_nil(task.goal_id), do: rollback(:goal_authority_required)

        create_or_replay_locked_command!(
          task,
          kind,
          normalized_payload,
          idempotency_key,
          command_hash,
          current,
          opts
        )
      end)
      |> case do
        {:ok, {command, disposition, receipt}} ->
          notify_goal_task_terminal(receipt, opts, notify?)
          {:ok, command, disposition}

        error ->
          error
      end
    end
  end

  def create_command(_, _, _, _, _), do: {:error, :invalid_request}

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
  def create_goal_control_command(
        goal_id,
        revision,
        action_id,
        task_id,
        kind,
        payload,
        idempotency_key,
        opts \\ []
      )

  def create_goal_control_command(
        goal_id,
        revision,
        action_id,
        task_id,
        kind,
        payload,
        idempotency_key,
        opts
      )
      when is_binary(goal_id) and is_integer(revision) and revision > 0 and is_binary(action_id) and
             is_binary(task_id) and kind in ["pause", "cancel"] and is_map(payload) and
             is_binary(idempotency_key) and byte_size(idempotency_key) > 0 and is_list(opts) do
    if not (valid_uuid?(goal_id) and valid_uuid?(action_id) and valid_uuid?(task_id) and
              valid_command_request?(kind, payload) and
              idempotency_key == goal_control_idempotency_key(action_id, kind, task_id)) do
      {:error, :invalid_request}
    else
      command_hash = command_request_hash(kind, payload, opts)
      current = now(opts)

      Repo.transaction(fn ->
        goal = lock_goal(goal_id)
        ensure_current_goal_control_action!(goal, revision, action_id, kind)
        untrusted_task = Repo.get(Task, task_id) || rollback(:not_found)
        item = if untrusted_task.work_item_id, do: lock_work_item(untrusted_task.work_item_id)
        task = lock_task(task_id)

        ensure_goal_work_item_ownership!(goal, item, task)

        create_or_replay_locked_command!(
          task,
          kind,
          payload,
          idempotency_key,
          command_hash,
          current,
          opts
        )
      end)
      |> case do
        {:ok, {command, disposition, _receipt}} -> {:ok, command, disposition}
        {:error, reason} -> {:error, reason}
      end
    end
  end

  def create_goal_control_command(_, _, _, _, _, _, _, _), do: {:error, :invalid_request}

  @spec request_cancel(Ecto.UUID.t(), keyword()) ::
          {:ok, Task.t(), Command.t() | nil} | {:error, atom()}
  def request_cancel(task_id, opts \\ [])

  def request_cancel(task_id, opts) when is_binary(task_id) do
    if not valid_uuid?(task_id),
      do: {:error, :invalid_request},
      else: request_task_cancel(task_id, opts)
  end

  def request_cancel(_, _), do: {:error, :invalid_request}

  defp request_task_cancel(task_id, opts) do
    current = now(opts)
    notify? = not Repo.in_transaction?()

    Repo.transaction(fn ->
      task = lock_task(task_id)

      if not is_nil(task.goal_id), do: rollback(:goal_authority_required)

      if task.state in ["completed", "failed"] do
        {task, nil, nil}
      else
        idempotency_key = legacy_cancel_idempotency_key(task)
        command_hash = command_request_hash("cancel", %{}, opts)

        {command, _disposition, receipt} =
          create_or_replay_locked_command!(
            task,
            "cancel",
            %{},
            idempotency_key,
            command_hash,
            current,
            opts
          )

        task = lock_task(task.id)
        {task, command, receipt}
      end
    end)
    |> case do
      {:ok, {task, command, receipt}} ->
        notify_goal_task_terminal(receipt, opts, notify?)
        {:ok, task, command}

      error ->
        error
    end
  end

  @spec provide_input(Ecto.UUID.t(), map(), String.t(), keyword()) ::
          {:ok, Command.t(), :created | :replayed} | {:error, atom()}
  def provide_input(task_id, payload, idempotency_key, opts \\ [])

  def provide_input(task_id, payload, idempotency_key, opts)
      when is_binary(task_id) and is_map(payload) and is_binary(idempotency_key) and
             byte_size(idempotency_key) > 0 do
    create_command(task_id, "provide_input", payload, idempotency_key, opts)
  end

  def provide_input(_, _, _, _), do: {:error, :invalid_request}

  @spec retry_task(Ecto.UUID.t(), map(), String.t(), keyword()) ::
          {:ok, Task.t(), Command.t(), :created | :replayed} | {:error, atom()}
  def retry_task(task_id, attrs, idempotency_key, opts \\ [])

  def retry_task(task_id, attrs, idempotency_key, opts)
      when is_binary(task_id) and is_map(attrs) and is_binary(idempotency_key) and
             byte_size(idempotency_key) > 0 do
    task_attrs = normalize_task_attrs(attrs)

    if not (valid_uuid?(task_id) and valid_task_attrs?(task_attrs)) do
      {:error, :invalid_request}
    else
      current = now(opts)

      Repo.transaction(fn ->
        task = lock_task(task_id)

        if not is_nil(task.goal_id), do: rollback(:goal_authority_required)

        task_attrs =
          if value(task.required_capabilities, :supervisory_control, false) == true,
            do:
              Map.update!(
                task_attrs,
                :required_capabilities,
                &Map.put(&1, "supervisory_control", true)
              ),
            else: task_attrs

        payload = %{work: task_attrs}
        command_hash = command_request_hash("retry", payload, opts)

        case lock_task_command(task.id, idempotency_key) do
          %Command{} = command ->
            ensure_command_replay!(command, "retry", payload, opts)
            {task, command, :replayed}

          nil ->
            ensure_expected_generation!(task, Keyword.get(opts, :expected_generation))
            if task.state not in ["failed", "cancelled"], do: rollback(:state_conflict)

            next_generation = next_retry_generation(task)

            if goal_attempt_limit_exhausted?(task, next_generation) do
              {:attempt_limit, fail_task_for_attempt_limit!(task, current)}
            else
              run =
                if task.current_generation == task.attempt_generation,
                  do: lock_current_run(task),
                  else: nil

              command =
                insert_command!(
                  task.id,
                  run && run.id,
                  run && run.generation,
                  "retry",
                  payload,
                  idempotency_key,
                  command_hash,
                  state: "applied",
                  applied_at: current,
                  now: current
                )

              task =
                task
                |> Task.changeset(
                  Map.merge(task_attrs, %{
                    state: "queued",
                    attempt_generation: next_generation,
                    waiting_transition_id: nil,
                    result: nil,
                    failure: nil
                  })
                )
                |> stamp_update(current)
                |> Repo.update!()

              {task, command, :created}
            end
        end
      end)
      |> case do
        {:ok, {task, command, disposition}} -> {:ok, task, command, disposition}
        {:ok, {:attempt_limit, _task}} -> {:error, :attempt_limit}
        error -> error
      end
    end
  end

  def retry_task(_, _, _, _), do: {:error, :invalid_request}

  defp create_or_replay_locked_command!(
         task,
         kind,
         payload,
         idempotency_key,
         command_hash,
         current,
         opts
       ) do
    case lock_task_command(task.id, idempotency_key) do
      %Command{} = command ->
        ensure_command_replay!(command, kind, payload, opts)
        {command, :replayed, nil}

      nil ->
        create_new_command!(task, kind, payload, idempotency_key, command_hash, current, opts)
    end
  end

  defp create_new_command!(task, "cancel", payload, idempotency_key, command_hash, current, opts) do
    ensure_expected_generation!(task, Keyword.get(opts, :expected_generation))

    case task.state do
      "queued" ->
        command =
          insert_command!(task.id, nil, nil, "cancel", payload, idempotency_key, command_hash,
            state: "applied",
            applied_at: current,
            now: current
          )

        task
        |> Task.changeset(%{state: "cancelled", waiting_transition_id: nil})
        |> stamp_update(current)
        |> Repo.update!()

        {command, :created, nil}

      "assigned" ->
        run = lock_current_run(task)

        command =
          insert_command!(
            task.id,
            run.id,
            run.generation,
            "cancel",
            payload,
            idempotency_key,
            command_hash,
            state: "applied",
            applied_at: current,
            now: current
          )

        run =
          run
          |> Run.changeset(%{state: "cancelled"})
          |> stamp_update(current)
          |> Repo.update!()

        release_reserved_session!(run, current)

        task
        |> Task.changeset(%{state: "cancelled", waiting_transition_id: nil})
        |> stamp_update(current)
        |> Repo.update!()

        receipt = enqueue_goal_task_terminal!(task, run, "cancelled", now: current)
        {command, :created, receipt}

      state when state in ["claimed", "running", "paused", "waiting_for_input"] ->
        run = lock_current_run(task)
        reject_pending_commands!(task, run.id, current)

        command =
          insert_command!(
            task.id,
            run.id,
            run.generation,
            "cancel",
            payload,
            idempotency_key,
            command_hash,
            state: "pending",
            now: current
          )

        run
        |> Run.changeset(%{state: "cancelling"})
        |> stamp_update(current)
        |> Repo.update!()

        task
        |> Task.changeset(%{state: "cancelling", waiting_transition_id: nil})
        |> stamp_update(current)
        |> Repo.update!()

        {command, :created, nil}

      _ ->
        rollback(:state_conflict)
    end
  end

  defp create_new_command!(
         task,
         "provide_input",
         payload,
         idempotency_key,
         command_hash,
         current,
         opts
       ) do
    ensure_expected_generation!(task, Keyword.get(opts, :expected_generation))

    if task.state != "waiting_for_input" do
      rollback(:state_conflict)
    end

    run = lock_current_run(task)

    if run.state != "waiting_for_input", do: rollback(:state_conflict)

    ensure_expected_waiting!(task, run, Keyword.get(opts, :expected_waiting_transition_id))
    validate_decision_choice!(task, run, payload, opts)

    case Repo.one(
           from command in Command,
             where:
               command.run_id == ^run.id and command.generation == ^run.generation and
                 command.kind == "provide_input" and is_nil(command.acknowledged_at),
             lock: "FOR UPDATE"
         ) do
      nil ->
        command =
          insert_command!(
            task.id,
            run.id,
            run.generation,
            "provide_input",
            payload,
            idempotency_key,
            command_hash,
            state: "pending",
            now: current
          )

        {command, :created, nil}

      _command ->
        rollback(:state_conflict)
    end
  end

  defp create_new_command!(task, kind, payload, idempotency_key, command_hash, current, opts)
       when kind in @supervisory_commands do
    ensure_expected_generation!(task, Keyword.get(opts, :expected_generation))
    if task.state not in ["running", "paused"], do: rollback(:state_conflict)
    run = lock_current_run(task)
    runtime = lock_runtime(run.runtime_id)

    unless legacy_supervisory_runtime?(runtime) and
             value(runtime.capabilities, :supervisory_control, false) == true,
           do: rollback(:unsupported_control)

    allowed? =
      case kind do
        "guidance" -> task.state in ["running", "paused"]
        "pause" -> task.state == "running"
        "resume" -> task.state == "paused"
      end

    unless allowed? and run.state == task.state and task.attempt_generation == run.generation,
      do: rollback(:state_conflict)

    if runtime.connection_epoch != run.claimed_runtime_epoch or
         is_nil(run.lease_expires_at) or DateTime.compare(run.lease_expires_at, current) != :gt,
       do: rollback(:ownership_lost)

    if kind in ["pause", "resume"] and pending_supervisory_transition?(run.id),
      do: rollback(:state_conflict)

    command =
      insert_command!(
        task.id,
        run.id,
        run.generation,
        kind,
        payload,
        idempotency_key,
        command_hash,
        state: "pending",
        now: current
      )

    {command, :created, nil}
  end

  defp insert_command!(
         task_id,
         run_id,
         generation,
         kind,
         payload,
         idempotency_key,
         {command_hash, command_hash_version},
         opts
       ) do
    %Command{}
    |> Command.changeset(%{
      task_id: task_id,
      run_id: run_id,
      generation: generation,
      kind: kind,
      payload: payload,
      idempotency_key: idempotency_key,
      request_hash: command_hash,
      request_hash_version: command_hash_version,
      state: Keyword.fetch!(opts, :state),
      applied_at: Keyword.get(opts, :applied_at)
    })
    |> stamp_insert(Keyword.fetch!(opts, :now))
    |> persist_insert()
  end

  @spec acknowledge_command(Ecto.UUID.t(), map(), String.t(), Ecto.UUID.t(), keyword()) ::
          {:ok, Command.t()} | {:error, atom()}
  def acknowledge_command(command_id, fence, outcome, acknowledgement_id, opts \\ [])

  def acknowledge_command(command_id, fence, outcome, acknowledgement_id, opts)
      when is_map(fence) and is_binary(outcome) do
    if not (valid_uuid?(command_id) and valid_fence?(fence) and valid_uuid?(acknowledgement_id)) do
      {:error, :invalid_request}
    else
      current = now(opts)

      Repo.transaction(fn ->
        run_id =
          Repo.one(
            from command in Command, where: command.id == ^command_id, select: command.run_id
          ) ||
            rollback(:not_found)

        {task, run, runtime} = lock_chain(run_id)

        command =
          Repo.one!(from command in Command, where: command.id == ^command_id, lock: "FOR UPDATE")

        ensure_terminal_static_fence!(task, run, fence)

        cond do
          command.generation != run.generation ->
            rollback(:ownership_lost)

          command.acknowledgement_id == acknowledgement_id and
              command.acknowledgement_outcome == outcome ->
            command

          command.acknowledgement_id == acknowledgement_id ->
            rollback(:idempotency_conflict)

          not is_nil(command.acknowledgement_id) ->
            rollback(:idempotency_conflict)

          command.state == "acknowledged" and command.acknowledgement_outcome != outcome ->
            rollback(:state_conflict)

          command.state == "applied" and
            command.kind in ["guidance", "pause", "resume", "provide_input"] and
              outcome != "applied" ->
            rollback(:state_conflict)

          true ->
            ensure_terminal_fence!(task, run, runtime, fence, current)

            if command.kind in @supervisory_commands and command.state == "pending" and
                 outcome == "applied",
               do: ensure_fence!(task, run, runtime, fence, current)

            cond do
              outcome not in ["applied", "rejected", "failed"] ->
                rollback(:invalid_request)

              outcome == "applied" and command.kind == "provide_input" and
                run.state == "waiting_for_input" and is_nil(command.applied_at) ->
                rollback(:state_conflict)

              outcome == "applied" and command.kind in ["pause", "resume"] and
                  is_nil(command.applied_at) ->
                rollback(:state_conflict)

              command.kind in @supervisory_commands and command.state == "pending" and
                outcome == "applied" and run.state not in ["running", "paused"] ->
                rollback(:state_conflict)

              true ->
                command
                |> Command.changeset(%{
                  state: "acknowledged",
                  acknowledgement_id: acknowledgement_id,
                  acknowledgement_outcome: outcome,
                  acknowledged_at: current,
                  applied_at:
                    if(
                      outcome == "applied" and
                        command.kind in ["guidance", "pause", "resume", "provide_input"],
                      do: command.applied_at || current,
                      else: command.applied_at
                    )
                })
                |> stamp_update(current)
                |> Repo.update!()
            end
        end
      end)
    end
  end

  def acknowledge_command(_, _, _, _, _), do: {:error, :invalid_request}

  @spec work_snapshot(Ecto.UUID.t(), integer(), keyword()) :: {:ok, map()} | {:error, atom()}
  def work_snapshot(runtime_id, runtime_epoch, opts \\ [])

  def work_snapshot(runtime_id, runtime_epoch, opts)
      when is_integer(runtime_epoch) and runtime_epoch > 0 do
    if not valid_uuid?(runtime_id),
      do: {:error, :invalid_request},
      else: daemon_runtime_snapshot(runtime_id, runtime_epoch, opts)
  end

  def work_snapshot(_, _, _), do: {:error, :invalid_request}

  defp daemon_runtime_snapshot(runtime_id, runtime_epoch, opts) do
    current = now(opts)

    Repo.transaction(fn ->
      runtime = share_runtime(runtime_id)
      if runtime.connection_epoch != runtime_epoch, do: rollback(:ownership_lost)
      snapshot_for(runtime, current)
    end)
  end

  @spec reconcile(Ecto.UUID.t(), integer(), [map()], keyword()) :: {:ok, map()} | {:error, atom()}
  def reconcile(runtime_id, runtime_epoch, journals, opts \\ [])

  def reconcile(runtime_id, runtime_epoch, journals, opts)
      when is_integer(runtime_epoch) and runtime_epoch > 0 and is_list(journals) do
    if not (valid_uuid?(runtime_id) and Enum.all?(journals, &valid_journal?/1)),
      do: {:error, :invalid_request},
      else: reconcile_runtime(runtime_id, runtime_epoch, journals, opts)
  end

  def reconcile(_, _, _, _), do: {:error, :invalid_request}

  defp reconcile_runtime(runtime_id, runtime_epoch, journals, opts) do
    current = now(opts)

    Repo.transaction(fn ->
      runtime = share_runtime(runtime_id)
      if runtime.connection_epoch != runtime_epoch, do: rollback(:ownership_lost)

      run_ids = Enum.map(journals, &value(&1, :run_id))

      runs_by_id =
        Repo.all(from run in Run, where: run.id in ^run_ids)
        |> Map.new(&{&1.id, &1})

      decisions =
        Enum.map(journals, fn journal ->
          run = Map.get(runs_by_id, value(journal, :run_id))

          decision =
            cond do
              is_nil(run) ->
                "unknown_stop"

              run.runtime_id != runtime.id or run.generation != value(journal, :generation) ->
                "stale_stop"

              run.claimed_runtime_epoch != runtime_epoch or
                run.claim_id != value(journal, :claim_id) or
                  run.lease_token != value(journal, :lease_token) ->
                "stale_stop"

              run.state == "cancelling" ->
                "cancel"

              run.state in @terminal_states ->
                "terminal"

              is_nil(run.lease_expires_at) or
                  DateTime.compare(run.lease_expires_at, current) != :gt ->
                "stale_stop"

              true ->
                "continue"
            end

          %{
            run_id: value(journal, :run_id),
            generation: value(journal, :generation),
            decision: decision,
            lease_expires_at: run && run.lease_expires_at
          }
        end)

      Map.put(snapshot_for(runtime, current), :decisions, decisions)
    end)
  end

  @spec expire(keyword()) :: %{
          expired_runs: non_neg_integer(),
          offline_runtimes: non_neg_integer()
        }
  def expire(opts \\ []) do
    current = now(opts)
    notify? = not Repo.in_transaction?()

    run_ids =
      Repo.all(
        from run in Run,
          where:
            (run.state == "assigned" and run.assignment_expires_at <= ^current) or
              (run.state in ["claimed", "running", "paused", "waiting_for_input", "cancelling"] and
                 run.lease_expires_at <= ^current),
          select: run.id
      )

    expired_runs =
      Enum.count(run_ids, fn run_id ->
        case expire_run(run_id, current) do
          {:ok, {metadata, receipt}} ->
            emit([:run, :expired], metadata)
            notify_goal_task_terminal(receipt, opts, notify?)
            true

          :not_expired ->
            false
        end
      end)

    offline_runtimes = expire_offline_runtimes(current)
    %{expired_runs: expired_runs, offline_runtimes: offline_runtimes}
  end

  defp expire_run(run_id, current) do
    case Repo.transaction(fn ->
           {task, run, _runtime} = lock_chain(run_id)

           expired? =
             (run.state == "assigned" and
                DateTime.compare(run.assignment_expires_at, current) != :gt) or
               (run.state in ["claimed", "running", "paused", "waiting_for_input", "cancelling"] and
                  not is_nil(run.lease_expires_at) and
                  DateTime.compare(run.lease_expires_at, current) != :gt)

           if not expired?, do: rollback(:not_expired)

           assigned_before_expiry? = run.state == "assigned"
           stopped? = run.state == "paused" or pending_pause?(run.id)

           terminal_state =
             cond do
               run.state == "cancelling" -> "cancelled"
               stopped? -> "failed"
               true -> "expired"
             end

           retryable_expiration? = terminal_state == "expired"

           attempt_limit_exhausted? =
             retryable_expiration? and goal_attempt_limit_exhausted?(task, run.generation + 1)

           persisted_run_state =
             if attempt_limit_exhausted?, do: "failed", else: terminal_state

           failure =
             cond do
               stopped? ->
                 %{
                   "reason" => "supervised_worker_lost",
                   "message" =>
                     "Paused worker or pending pause lost its lease; explicit retry is required."
                 }

               attempt_limit_exhausted? ->
                 lease_expired_failure()

               true ->
                 nil
             end

           reject_pending_commands!(task, run.id, current)

           run =
             run
             |> Run.changeset(%{state: persisted_run_state, failure: failure})
             |> stamp_update(current)
             |> Repo.update!()

           if assigned_before_expiry? do
             release_reserved_session!(run, current)
           else
             mark_reserved_session_unavailable!(run, current)
           end

           if task.current_generation == run.generation do
             task_attrs =
               task_attrs_for_expiry(
                 task,
                 run,
                 retryable_expiration?,
                 persisted_run_state,
                 failure
               )

             task
             |> Task.changeset(task_attrs)
             |> stamp_update(current)
             |> Repo.update!()
           end

           metadata = %{run_id: run.id, generation: run.generation, state: persisted_run_state}

           receipt =
             cond do
               attempt_limit_exhausted? ->
                 settlement_opts =
                   if is_nil(run.lease_expires_at) do
                     [now: current]
                   else
                     [
                       now: current,
                       scheduled_at: DateTime.add(current, @terminal_grace_ms, :millisecond)
                     ]
                   end

                 enqueue_goal_task_terminal!(task, run, persisted_run_state, settlement_opts)

               not retryable_expiration? ->
                 enqueue_goal_task_terminal!(task, run, persisted_run_state, now: current)

               true ->
                 nil
             end

           {metadata, receipt}
         end) do
      {:ok, result} -> {:ok, result}
      _ -> :not_expired
    end
  end

  defp expire_offline_runtimes(current) do
    runtime_ids =
      Repo.all(
        from runtime in Runtime,
          where: runtime.status == "online",
          select: runtime.id
      )

    Enum.count(runtime_ids, fn runtime_id ->
      case Repo.transaction(fn ->
             runtime = lock_runtime(runtime_id)
             cutoff = DateTime.add(current, -3 * runtime.heartbeat_interval_ms, :millisecond)

             if runtime.status == "online" and
                  (is_nil(runtime.last_heartbeat_at) or
                     DateTime.compare(runtime.last_heartbeat_at, cutoff) != :gt) do
               runtime
               |> Runtime.changeset(%{status: "offline"})
               |> stamp_update(current)
               |> Repo.update!()

               %{runtime_id: runtime.id}
             else
               nil
             end
           end) do
        {:ok, %{runtime_id: _runtime_id} = metadata} ->
          emit([:runtime, :offline], metadata)
          true

        _ ->
          false
      end
    end)
  end

  defp transition_once!(
         task,
         run,
         target_state,
         payload,
         transition_id,
         body_hash,
         body_hash_version,
         current,
         goal
       ) do
    validate_transition!(transition_origin_state(run), target_state)
    ensure_expired_run_can_settle_task!(task, run)
    apply_supervisory_transition!(task, run, target_state, payload, current, goal)
    if target_state in @terminal_targets, do: reject_pending_commands!(task, run.id, current)

    run_attrs =
      case target_state do
        "completed" -> %{state: target_state, result: payload, failure: nil}
        "failed" -> %{state: target_state, failure: payload}
        "cancelled" -> %{state: target_state, result: nil, failure: nil}
        _ -> %{state: target_state}
      end

    task_attrs =
      target_state
      |> task_attrs_for_transition(payload, transition_id)
      |> restore_expired_attempt_generation(run)

    run = run |> Run.changeset(run_attrs) |> stamp_update(current) |> Repo.update!()
    if target_state in @terminal_targets, do: mark_reserved_session_unavailable!(run, current)
    task |> Task.changeset(task_attrs) |> stamp_update(current) |> Repo.update!()

    %RunTransition{}
    |> RunTransition.changeset(%{
      run_id: run.id,
      transition_id: transition_id,
      request_hash: body_hash,
      request_hash_version: body_hash_version,
      state: target_state,
      payload: payload
    })
    |> stamp_insert(current)
    |> Repo.insert!()

    receipt = enqueue_goal_task_terminal!(task, run, target_state, now: current)
    {run, receipt}
  end

  defp task_attrs_for_transition("completed", payload, _transition_id),
    do: %{state: "completed", waiting_transition_id: nil, result: payload, failure: nil}

  defp task_attrs_for_transition("failed", payload, _transition_id),
    do: %{state: "failed", waiting_transition_id: nil, result: nil, failure: payload}

  defp task_attrs_for_transition("cancelled", _payload, _transition_id),
    do: %{state: "cancelled", waiting_transition_id: nil, result: nil, failure: nil}

  defp task_attrs_for_transition("waiting_for_input", _payload, transition_id),
    do: %{state: "waiting_for_input", waiting_transition_id: transition_id}

  defp task_attrs_for_transition(target_state, _payload, _transition_id),
    do: %{state: target_state, waiting_transition_id: nil}

  defp task_attrs_for_expiry(task, run, true, _persisted_run_state, _failure) do
    next_generation = run.generation + 1

    if goal_attempt_limit_exhausted?(task, next_generation) do
      %{
        state: "failed",
        attempt_generation: run.generation,
        waiting_transition_id: nil,
        result: nil,
        failure: attempt_limit_failure(task)
      }
    else
      %{
        state: "queued",
        attempt_generation: next_generation,
        waiting_transition_id: nil
      }
    end
  end

  defp task_attrs_for_expiry(_task, _run, false, terminal_state, failure) do
    %{state: terminal_state, waiting_transition_id: nil, failure: failure}
  end

  defp next_retry_generation(%Task{goal_id: nil, attempt_generation: generation}),
    do: generation + 1

  defp next_retry_generation(%Task{} = task) do
    case Repo.one(from run in Run, where: run.task_id == ^task.id, select: max(run.generation)) do
      nil -> 1
      generation -> generation + 1
    end
  end

  defp goal_attempt_limit_exhausted?(
         %Task{goal_id: goal_id, max_run_attempts: maximum},
         generation
       )
       when not is_nil(goal_id) and is_integer(maximum),
       do: generation > maximum

  defp goal_attempt_limit_exhausted?(_task, _generation), do: false

  defp fail_task_for_attempt_limit!(task, current) do
    task
    |> Task.changeset(%{
      state: "failed",
      attempt_generation: min(task.attempt_generation, task.max_run_attempts),
      waiting_transition_id: nil,
      result: nil,
      failure: attempt_limit_failure(task)
    })
    |> stamp_update(current)
    |> Repo.update!()
  end

  defp attempt_limit_failure(task) do
    %{
      "reason" => "attempt_limit",
      "message" => "The admitted run attempt limit has been exhausted.",
      "max_run_attempts" => task.max_run_attempts
    }
  end

  defp lease_expired_failure do
    %{
      "reason" => "lease_expired",
      "message" => "Worker lease expired before a terminal delivery was accepted."
    }
  end

  defp restore_expired_attempt_generation(task_attrs, %Run{
         state: "expired",
         generation: generation
       }),
       do: Map.put(task_attrs, :attempt_generation, generation)

  defp restore_expired_attempt_generation(task_attrs, %Run{
         state: "failed",
         failure: %{"reason" => "lease_expired"},
         generation: generation
       }),
       do: Map.put(task_attrs, :attempt_generation, generation)

  defp restore_expired_attempt_generation(task_attrs, _run), do: task_attrs

  defp enqueue_goal_task_terminal!(task, run, terminal_state, opts)
       when terminal_state in @terminal_states do
    case goal_task_terminal_receipt(task, run) do
      nil ->
        nil

      receipt ->
        receipt
        |> settlement_job_args()
        |> SettleTaskWorker.new(Keyword.take(opts, [:scheduled_at]))
        |> Oban.insert!()

        receipt
    end
  end

  defp enqueue_goal_task_terminal!(_task, _run, _state, _opts), do: nil

  defp goal_task_terminal_receipt(%Task{goal_id: goal_id}, %Run{} = run)
       when not is_nil(goal_id) do
    %{task_id: run.task_id, run_id: run.id, generation: run.generation}
  end

  defp goal_task_terminal_receipt(_task, _run), do: nil

  defp settlement_job_args(%{task_id: task_id, run_id: run_id, generation: generation}) do
    %{"task_id" => task_id, "run_id" => run_id, "generation" => generation}
  end

  defp notify_goal_task_terminal(_receipt, _opts, false), do: :ok

  defp notify_goal_task_terminal(nil, _opts, true), do: :ok

  defp notify_goal_task_terminal(receipt, opts, true) do
    case Keyword.get(opts, :on_goal_task_terminal) do
      hook when is_function(hook, 1) ->
        _ = hook.(receipt)
        :ok

      _ ->
        :ok
    end
  end

  defp ensure_expired_run_can_settle_task!(%Task{state: "queued"}, %Run{state: "expired"}),
    do: :ok

  defp ensure_expired_run_can_settle_task!(
         %Task{state: "queued"},
         %Run{state: "failed", failure: %{"reason" => "lease_expired"}}
       ),
       do: :ok

  defp ensure_expired_run_can_settle_task!(
         %Task{
           state: "failed",
           current_generation: generation,
           failure: %{"reason" => "attempt_limit"}
         },
         %Run{state: "expired", generation: generation}
       ),
       do: :ok

  defp ensure_expired_run_can_settle_task!(
         %Task{
           state: "failed",
           current_generation: generation,
           failure: %{"reason" => "attempt_limit"}
         },
         %Run{state: "failed", failure: %{"reason" => "lease_expired"}, generation: generation}
       ),
       do: :ok

  defp ensure_expired_run_can_settle_task!(_task, %Run{state: "expired"}),
    do: rollback(:ownership_lost)

  defp ensure_expired_run_can_settle_task!(_task, _run), do: :ok

  defp transition_origin_state(%Run{state: "failed", failure: %{"reason" => "lease_expired"}}),
    do: "expired"

  defp transition_origin_state(%Run{state: state}), do: state

  defp ensure_cancelled_transition_authority!(%Run{state: "cancelled"} = run, target_state) do
    if target_state in @terminal_targets and
         not Repo.exists?(
           from transition in RunTransition,
             where: transition.run_id == ^run.id and transition.state == "cancelled"
         ) do
      rollback(:ownership_lost)
    end
  end

  defp ensure_cancelled_transition_authority!(_run, _target_state), do: :ok

  defp valid_transition?("claimed", state) when state in ["running", "completed", "failed"],
    do: true

  defp valid_transition?("running", state)
       when state in ["paused", "waiting_for_input", "completed", "failed"], do: true

  defp valid_transition?("paused", state) when state in ["running", "failed"], do: true

  defp valid_transition?("waiting_for_input", state)
       when state in ["running", "completed", "failed"],
       do: true

  defp valid_transition?("cancelling", "cancelled"), do: true
  defp valid_transition?(_, _), do: false

  defp validate_transition!(current_state, target_state) do
    cond do
      target_state not in ["running", "paused", "waiting_for_input" | @terminal_targets] ->
        rollback(:invalid_transition)

      current_state == "expired" and target_state in @terminal_targets ->
        :ok

      valid_transition?(current_state, target_state) ->
        :ok

      current_state == target_state or current_state in @terminal_states ->
        rollback(:state_conflict)

      true ->
        rollback(:invalid_transition)
    end
  end

  defp transition_response(run, transition) do
    attrs =
      case transition.state do
        "completed" -> %{state: transition.state, result: transition.payload, failure: nil}
        "failed" -> %{state: transition.state, result: nil, failure: transition.payload}
        _ -> %{state: transition.state, result: nil, failure: nil}
      end

    struct(run, attrs)
  end

  defp apply_supervisory_transition!(task, run, target_state, payload, current, goal) do
    kind =
      case {run.state, target_state} do
        {"running", "paused"} ->
          "pause"

        {"paused", "running"} ->
          "resume"

        {"waiting_for_input", "running"} ->
          if value(task.required_capabilities, :supervisory_control, false) == true,
            do: "provide_input"

        _ ->
          nil
      end

    if kind do
      command_id = value(payload, :command_id)
      unless valid_uuid?(command_id), do: rollback(:invalid_request)

      command =
        Repo.one(
          from command in Command,
            where:
              command.id == ^command_id and command.run_id == ^run.id and
                command.generation == ^run.generation and command.kind == ^kind and
                command.state == "pending",
            lock: "FOR UPDATE"
        )

      if is_nil(command), do: rollback(:state_conflict)

      ensure_goal_pause_command_current!(goal, task, command, kind)

      command
      |> Command.changeset(%{state: "applied", applied_at: current})
      |> stamp_update(current)
      |> Repo.update!()
    end
  end

  defp ensure_goal_pause_command_current!(nil, _task, _command, _kind), do: :ok

  defp ensure_goal_pause_command_current!(_goal, _task, _command, kind) when kind != "pause",
    do: :ok

  defp ensure_goal_pause_command_current!(goal, task, command, "pause") do
    action_id =
      case String.split(command.idempotency_key, ":", parts: 4) do
        ["goal-control", action_id, "pause", task_id] when task_id == task.id ->
          if valid_uuid?(action_id), do: action_id, else: rollback(:goal_authority_required)

        _ ->
          rollback(:goal_authority_required)
      end

    ensure_current_goal_control_action!(goal, goal.current_revision, action_id, "pause")
  end

  defp goal_control_idempotency_key(action_id, kind, task_id),
    do: "goal-control:#{action_id}:#{kind}:#{task_id}"

  defp pending_supervisory_transition?(run_id) do
    Repo.exists?(
      from command in Command,
        where:
          command.run_id == ^run_id and command.kind in ["pause", "resume"] and
            command.state in ["pending", "applied"]
    )
  end

  defp pending_pause?(run_id) do
    Repo.exists?(
      from command in Command,
        where:
          command.run_id == ^run_id and command.kind == "pause" and command.state == "pending"
    )
  end

  defp reject_pending_commands!(task, run_id, current) do
    kinds =
      if value(task.required_capabilities, :supervisory_control, false) == true,
        do: ["provide_input" | @supervisory_commands],
        else: @supervisory_commands

    from(command in Command,
      where:
        command.run_id == ^run_id and command.kind in ^kinds and
          command.state == "pending"
    )
    |> Repo.update_all(
      set: [
        state: "acknowledged",
        acknowledgement_outcome: "rejected",
        acknowledged_at: current,
        updated_at: current
      ]
    )
  end

  defp validate_decision_packet!(task, payload) do
    decision = value(payload, :decision)
    required? = value(task.required_capabilities, :supervisory_control, false) == true

    if (required? or not is_nil(decision)) and
         not (nonempty_text?(value(payload, :question)) and valid_decision?(decision)),
       do: rollback(:invalid_request)
  end

  defp valid_decision?(decision) when is_map(decision) do
    options = value(decision, :options)
    recommendation = value(decision, :recommended_option_id)

    value(decision, :reason) in @decision_reasons and
      nonempty_text?(value(decision, :context)) and is_list(options) and length(options) >= 2 and
      Enum.all?(options, fn option ->
        is_map(option) and nonempty_text?(value(option, :id)) and
          nonempty_text?(value(option, :label)) and nonempty_text?(value(option, :consequence))
      end) and
      length(Enum.uniq_by(options, &value(&1, :id))) == length(options) and
      (is_nil(recommendation) or Enum.any?(options, &(value(&1, :id) == recommendation)))
  end

  defp valid_decision?(_), do: false

  defp validate_decision_choice!(task, run, payload, opts) do
    waiting = current_waiting_context(task, run)

    if waiting && waiting.decision do
      unless Keyword.get(opts, :expected_waiting_transition_id) == task.waiting_transition_id,
        do: rollback(:invalid_request)

      option_id = value(payload, :option_id)

      unless Enum.any?(value(waiting.decision, :options, []), &(value(&1, :id) == option_id)),
        do: rollback(:invalid_request)
    end
  end

  defp nonempty_text?(text), do: is_binary(text) and String.trim(text) != ""

  defp snapshot_for(runtime, current) do
    assignments =
      Repo.all(
        from run in Run,
          join: task in Task,
          on: task.id == run.task_id,
          where:
            run.runtime_id == ^runtime.id and run.state == "assigned" and
              run.assignment_expires_at > ^current and task.current_generation == run.generation,
          order_by: [asc: run.inserted_at],
          select: %{run: run, task: task}
      )

    commands =
      Repo.all(
        from command in Command,
          join: run in Run,
          on: run.id == command.run_id,
          join: task in Task,
          on: task.id == run.task_id,
          where:
            run.runtime_id == ^runtime.id and
              task.current_generation == run.generation and
              ((command.state == "pending" and command.kind == "cancel" and
                  run.state == "cancelling") or
                 (command.state in ["pending", "applied"] and command.kind == "provide_input" and
                    is_nil(command.acknowledged_at) and
                    run.state in ["waiting_for_input", "running"]) or
                 (command.state in ["pending", "applied"] and
                    command.kind in ^@supervisory_commands and
                    run.state in ["running", "paused"])),
          order_by: [asc: command.inserted_at]
      )

    %{assignments: assignments, commands: commands, server_time: current}
  end

  defp ensure_fence!(task, run, runtime, fence, current) do
    ensure_static_fence!(task, run, runtime, fence)

    valid? =
      run.state in ["claimed", "running", "paused", "waiting_for_input", "cancelling"] and
        not is_nil(run.lease_expires_at) and
        DateTime.compare(run.lease_expires_at, current) == :gt

    unless valid?, do: rollback(:ownership_lost)
  end

  defp ensure_transition_fence!(task, run, runtime, fence, target_state, current) do
    if target_state in @terminal_targets do
      ensure_terminal_fence!(task, run, runtime, fence, current)
    else
      ensure_fence!(task, run, runtime, fence, current)
    end
  end

  defp ensure_transition_static_fence!(task, run, runtime, fence, target_state) do
    if target_state in @terminal_targets do
      ensure_terminal_static_fence!(task, run, fence)
    else
      ensure_static_fence!(task, run, runtime, fence)
    end
  end

  defp ensure_terminal_fence!(task, run, _runtime, fence, current) do
    ensure_terminal_static_fence!(task, run, fence)

    valid? =
      (run.state in ["claimed", "running", "paused", "waiting_for_input", "cancelling"] and
         not is_nil(run.lease_expires_at) and
         DateTime.compare(run.lease_expires_at, current) == :gt) or
        within_terminal_grace?(run, current)

    unless valid?, do: rollback(:terminal_grace_expired)
  end

  defp within_terminal_grace?(%Run{lease_expires_at: %DateTime{} = lease_expires_at}, current) do
    DateTime.compare(DateTime.add(lease_expires_at, @terminal_grace_ms, :millisecond), current) !=
      :lt
  end

  defp within_terminal_grace?(_run, _current), do: false

  defp ensure_terminal_static_fence!(task, run, fence) do
    valid? =
      run.runtime_id == value(fence, :runtime_id) and
        run.claimed_runtime_epoch == value(fence, :runtime_epoch) and
        run.generation == value(fence, :generation) and
        task.current_generation == value(fence, :generation) and
        run.claim_id == value(fence, :claim_id) and
        run.lease_token == value(fence, :lease_token)

    unless valid?, do: rollback(:ownership_lost)
  end

  defp ensure_static_fence!(task, run, runtime, fence) do
    valid? =
      run.runtime_id == value(fence, :runtime_id) and
        runtime.connection_epoch == value(fence, :runtime_epoch) and
        run.claimed_runtime_epoch == value(fence, :runtime_epoch) and
        run.generation == value(fence, :generation) and
        task.current_generation == value(fence, :generation) and
        run.claim_id == value(fence, :claim_id) and
        run.lease_token == value(fence, :lease_token)

    unless valid?, do: rollback(:ownership_lost)
  end

  defp lock_chain(run_id) do
    task_id =
      Repo.one(from run in Run, where: run.id == ^run_id, select: run.task_id) ||
        rollback(:not_found)

    task = lock_task(task_id)
    run = Repo.one!(from run in Run, where: run.id == ^run_id, lock: "FOR UPDATE")
    runtime = lock_runtime(run.runtime_id)
    {task, run, runtime}
  end

  # A Goal pause transition consumes a Goal-issued control action. It must keep
  # the Goal -> WorkItem -> Task -> Run lock order, but its paused Goal state is
  # itself the authority for this one transition.
  defp lock_transition_chain(run_id, "paused") do
    {task, run, runtime, goal, _item} = lock_goal_chain(run_id)
    {task, run, runtime, goal, nil}
  end

  # A terminal receipt can still settle an already-started Goal run after its
  # Goal changes. All other progress requires current Goal authority.
  defp lock_transition_chain(run_id, target_state) when target_state in @terminal_targets do
    {task, run, runtime} = lock_chain(run_id)
    {task, run, runtime, nil, nil}
  end

  defp lock_transition_chain(run_id, _target_state) do
    lock_goal_chain(run_id)
  end

  defp lock_execution_chain(run_id) do
    {task, run, runtime, goal, item} = lock_goal_chain(run_id)

    if goal do
      ensure_current_goal_execution!(goal, item, task)
    end

    {task, run, runtime}
  end

  # Read ownership without a lock, then take the durable locks in the one
  # ordering shared by Goal execution paths. This prevents a lifecycle command
  # from racing a legacy Task -> Run acquisition order.
  defp lock_goal_chain(run_id) do
    ownership =
      Repo.one(
        from run in Run,
          join: task in Task,
          on: task.id == run.task_id,
          where: run.id == ^run_id,
          select: %{task_id: task.id, goal_id: task.goal_id, work_item_id: task.work_item_id}
      ) || rollback(:not_found)

    case ownership.goal_id do
      nil ->
        {task, run, runtime} = lock_chain(run_id)
        {task, run, runtime, nil, nil}

      goal_id ->
        goal = lock_goal(goal_id)
        item = if ownership.work_item_id, do: lock_work_item(ownership.work_item_id)
        task = lock_task(ownership.task_id)
        run = Repo.one!(from run in Run, where: run.id == ^run_id, lock: "FOR UPDATE")
        runtime = lock_runtime(run.runtime_id)

        ensure_goal_work_item_ownership!(goal, item, task)
        {task, run, runtime, goal, item}
    end
  end

  defp ensure_goal_work_item_ownership!(goal, item, task) do
    unless goal_work_item_owned?(goal, item, task),
      do: rollback(:ownership_lost)
  end

  defp ensure_current_goal_execution!(goal, item, task) do
    ensure_goal_work_item_ownership!(goal, item, task)
    unless goal_task_current?(goal, task), do: rollback(:ownership_lost)
  end

  defp ensure_current_goal_execution_if_present!(nil, _item, _task), do: :ok

  defp ensure_current_goal_execution_if_present!(goal, item, task) do
    ensure_current_goal_execution!(goal, item, task)
  end

  defp ensure_current_goal_transition_authority!(nil, _item, _task, _target_state), do: :ok

  defp ensure_current_goal_transition_authority!(_goal, _item, _task, target_state)
       when target_state in @terminal_targets or target_state == "paused",
       do: :ok

  defp ensure_current_goal_transition_authority!(goal, item, task, _target_state) do
    ensure_current_goal_execution!(goal, item, task)
  end

  # Claim additionally verifies that the currently selected runtime still
  # satisfies the active Goal policy.
  defp lock_claim_chain(run_id) do
    lock_goal_chain(run_id)
  end

  defp ensure_new_goal_claim_authority!(nil, _item, _task, _runtime), do: :ok

  defp ensure_new_goal_claim_authority!(goal, item, task, runtime) do
    revision = lock_goal_revision(goal)
    ensure_current_goal_execution!(goal, item, task)
    unless goal_runtime_matches?(runtime, task, revision, item), do: rollback(:ownership_lost)
  end

  defp ensure_requested_session_claim_binding!(
         %Task{requested_session_id: nil},
         _runtime,
         _item,
         _run
       ),
       do: :ok

  defp ensure_requested_session_claim_binding!(task, runtime, item, run) do
    repository_resource_id =
      goal_task_repository_resource_id(task, item) || rollback(:ownership_lost)

    session =
      Repo.one(
        from session in HarnessSession,
          where:
            session.id == ^task.requested_session_id and session.runtime_id == ^runtime.id and
              session.machine_id == ^runtime.machine_id and session.state == "busy" and
              session.binding_verified == true and
              session.active_run_id == ^run.id and
              session.repository_resource_id == ^repository_resource_id and
              session.harness_kind == ^runtime.harness_kind and
              session.harness_version == ^runtime.harness_version and
              session.adapter_version == ^runtime.adapter_version,
          lock: "FOR UPDATE"
      )

    unless (session && run.harness_session_id == session.id) and
             run.harness_binding_id == session.binding_id,
           do: rollback(:ownership_lost)
  end

  defp handoff_source_machine_matches?(_runtime, %Task{handoff_source_run_id: nil}), do: true

  defp handoff_source_machine_matches?(runtime, %Task{} = task) do
    Repo.exists?(
      from(source_run in Run,
        join: source_runtime in Runtime,
        on: source_runtime.id == source_run.runtime_id,
        where:
          source_run.id == ^task.handoff_source_run_id and
            source_runtime.machine_id == ^runtime.machine_id
      )
    )
  end

  defp replayed_claim?(task, run, runtime, request, current) do
    run.runtime_id == value(request, :runtime_id) and runtime.id == value(request, :runtime_id) and
      runtime.connection_epoch == value(request, :runtime_epoch) and
      task.current_generation == value(request, :generation) and
      run.generation == value(request, :generation) and
      run.state in ["claimed", "cancelling"] and run.claim_id == value(request, :claim_id) and
      run.claimed_runtime_epoch == value(request, :runtime_epoch) and
      not is_nil(run.lease_expires_at) and
      DateTime.compare(run.lease_expires_at, current) == :gt
  end

  defp event_replay([], _existing_events), do: :missing

  defp event_replay(events, existing_events) do
    {all_present?, conflict?, stored} =
      Enum.reduce(events, {true, false, []}, fn event, {all_present?, conflict?, stored} ->
        event_id = value(event, :event_id)
        body = event_body(event)

        case Map.get(existing_events, event_id) do
          nil ->
            {false, conflict?, stored}

          existing ->
            matches? =
              RequestHash.matches?(existing.request_hash, existing.request_hash_version, body)

            {all_present?, conflict? or not matches?, [existing | stored]}
        end
      end)

    cond do
      conflict? -> :conflict
      all_present? -> {:replayed, Enum.reverse(stored)}
      true -> :missing
    end
  end

  defp lock_goal(goal_id),
    do:
      Repo.one(from goal in Goal, where: goal.id == ^goal_id, lock: "FOR UPDATE") ||
        rollback(:not_found)

  defp lock_work_item(work_item_id),
    do:
      Repo.one(from item in WorkItem, where: item.id == ^work_item_id, lock: "FOR UPDATE") ||
        rollback(:not_found)

  defp lock_goal_revision(goal) do
    Repo.one(
      from revision in GoalRevision,
        where: revision.goal_id == ^goal.id and revision.revision == ^goal.current_revision,
        lock: "FOR SHARE"
    ) || rollback(:stale_revision)
  end

  defp ensure_current_goal_control_action!(goal, revision, action_id, command_kind) do
    action =
      Repo.one(
        from event in GoalEvent,
          where: event.id == ^action_id and event.goal_id == ^goal.id,
          lock: "FOR UPDATE"
      )

    latest_lifecycle_action =
      Repo.one(
        from event in GoalEvent,
          where:
            event.goal_id == ^goal.id and event.kind in ["pause", "resume", "cancel", "amend"],
          order_by: [desc: event.sequence],
          limit: 1,
          lock: "FOR UPDATE"
      )

    current_lifecycle? =
      case {action && action.kind, command_kind, goal.state} do
        {"pause", "pause", "paused"} -> true
        {"cancel", "cancel", "cancelled"} -> true
        {"amend", kind, "paused"} when kind in ["pause", "cancel"] -> true
        _ -> false
      end

    unless (((action && action.revision == revision) and goal.current_revision == revision and
               latest_lifecycle_action) && latest_lifecycle_action.id == action_id) and
             current_lifecycle?,
           do: rollback(:stale_revision)
  end

  defp runtime_matches_task?(runtime, task) do
    runtime.status == "online" and runtime.agent_profile == task.agent_profile and
      runtime.workspace == task.workspace and
      (value(task.required_capabilities, :supervisory_control, false) != true or
         legacy_supervisory_runtime?(runtime)) and
      Repo.exists?(
        from candidate in Runtime,
          where: candidate.id == ^runtime.id,
          where:
            fragment(
              "? @> ?",
              candidate.capabilities,
              type(^task.required_capabilities, :map)
            )
      )
  end

  defp lock_machine(machine_id),
    do:
      Repo.one(from machine in Machine, where: machine.id == ^machine_id, lock: "FOR UPDATE") ||
        rollback(:not_found)

  defp lock_task(task_id),
    do:
      Repo.one(from task in Task, where: task.id == ^task_id, lock: "FOR UPDATE") ||
        rollback(:not_found)

  defp lock_current_run(task),
    do:
      Repo.one(
        from run in Run,
          where: run.task_id == ^task.id and run.generation == ^task.current_generation,
          lock: "FOR UPDATE"
      ) || rollback(:not_found)

  defp lock_runtime(runtime_id),
    do:
      Repo.one(from runtime in Runtime, where: runtime.id == ^runtime_id, lock: "FOR UPDATE") ||
        rollback(:not_found)

  defp share_runtime(runtime_id),
    do:
      Repo.one(from runtime in Runtime, where: runtime.id == ^runtime_id, lock: "FOR SHARE") ||
        rollback(:not_found)

  defp share_task(task_id),
    do:
      Repo.one(from task in Task, where: task.id == ^task_id, lock: "FOR SHARE") ||
        rollback(:not_found)

  defp persist_insert(changeset) do
    case Repo.insert(changeset) do
      {:ok, record} -> record
      {:error, _changeset} -> rollback(:invalid_request)
    end
  end

  defp persist_update(changeset) do
    case Repo.update(changeset) do
      {:ok, record} -> record
      {:error, _changeset} -> rollback(:invalid_request)
    end
  end

  defp insert_ignoring_conflict(schema, changeset, conflict_target) do
    if changeset.valid? do
      row = Map.put(changeset.changes, :id, Ecto.UUID.generate())

      opts =
        if conflict_target == nil,
          do: [on_conflict: :nothing],
          else: [on_conflict: :nothing, conflict_target: conflict_target]

      case Repo.insert_all(schema, [row], opts) do
        {1, _} -> :inserted
        {0, _} -> :conflict
      end
    else
      :invalid
    end
  end

  defp rollback(reason), do: Repo.rollback(reason)

  defp now(opts),
    do: opts |> Keyword.get(:now, DateTime.utc_now()) |> DateTime.truncate(:microsecond)

  defp lease_duration_ms(opts) do
    duration =
      Keyword.get_lazy(opts, :lease_duration_ms, fn ->
        Application.fetch_env!(:symmetry_control, :orchestration)
        |> Keyword.fetch!(:lease_duration_ms)
      end)

    if is_integer(duration) and duration >= @minimum_lease_duration_ms,
      do: {:ok, duration},
      else: :error
  end

  defp stamp_insert(changeset, current),
    do: Ecto.Changeset.change(changeset, inserted_at: current, updated_at: current)

  defp stamp_update(changeset, current), do: Ecto.Changeset.change(changeset, updated_at: current)

  defp valid_claim_request?(request) do
    valid_uuid?(value(request, :runtime_id)) and
      is_integer(value(request, :runtime_epoch)) and value(request, :runtime_epoch) > 0 and
      is_integer(value(request, :generation)) and value(request, :generation) > 0 and
      valid_uuid?(value(request, :claim_id))
  end

  defp valid_fence?(fence) do
    valid_uuid?(value(fence, :runtime_id)) and
      is_integer(value(fence, :runtime_epoch)) and value(fence, :runtime_epoch) > 0 and
      is_integer(value(fence, :generation)) and value(fence, :generation) > 0 and
      valid_uuid?(value(fence, :claim_id)) and valid_uuid?(value(fence, :lease_token))
  end

  defp valid_runtime_specification?(specification) when is_map(specification) do
    capabilities = value(specification, :capabilities, %{})

    is_map(capabilities) and jsonb_compatible?(capabilities) and
      valid_runtime_capabilities_schema?(specification, capabilities) and
      (value(capabilities, :supervisory_control, false) != true or
         (value(capabilities, :structured_input, false) == true and
            value(capabilities, :interactive, false) == true)) and
      (value(capabilities, :provider_access, false) != true or
         value(capabilities, :structured_input, false) == true) and
      valid_runtime_repository_resource?(specification) and
      valid_runtime_adapter_metadata?(specification, capabilities)
  end

  defp valid_runtime_specification?(_), do: false

  # A pre-Goal release can omit contracts entirely, but only old registrations
  # without adapter metadata may use the legacy capability subset in that mode.
  # Any available schema, Goal rollout, or new adapter declaration stays strict.
  defp valid_runtime_capabilities_schema?(specification, capabilities) do
    schema_root =
      :symmetry_control
      |> Application.fetch_env!(:contracts)
      |> Keyword.fetch!(:directory)
      |> Path.join("v1")

    if File.dir?(schema_root) do
      ContractValidation.validate_adapter_capabilities(capabilities, schema_root: schema_root) ==
        :ok
    else
      legacy_contract_fallback?(specification, capabilities)
    end
  end

  defp legacy_contract_fallback?(specification, capabilities) do
    goals_rollout_disabled?() and not runtime_metadata_present?(specification) and
      Enum.all?(capabilities, fn {key, value} ->
        key in [
          :structured_input,
          :provider_access,
          :interactive,
          :supervisory_control,
          "structured_input",
          "provider_access",
          "interactive",
          "supervisory_control"
        ] and is_boolean(value)
      end)
  end

  defp goals_rollout_disabled? do
    goals = Application.get_env(:symmetry_control, :goals) || []
    Keyword.get(goals, :rollout_enabled, false) != true
  end

  defp valid_runtime_repository_resource?(specification) do
    not map_key?(specification, :repository_resource_id) or
      is_nil(value(specification, :repository_resource_id)) or
      valid_uuid?(value(specification, :repository_resource_id))
  end

  defp ensure_runtime_repository_resource!(nil), do: :ok

  defp ensure_runtime_repository_resource!(resource_id) do
    resource =
      Repo.one(
        from resource in ProjectResource,
          where: resource.id == ^resource_id,
          lock: "FOR SHARE"
      ) || rollback(:invalid_request)

    unless resource.kind == "repository", do: rollback(:invalid_request)
  end

  defp ensure_runtime_repository_binding_change_safe!(nil, _repository_resource_id), do: :ok

  defp ensure_runtime_repository_binding_change_safe!(runtime, repository_resource_id)
       when runtime.repository_resource_id == repository_resource_id,
       do: :ok

  defp ensure_runtime_repository_binding_change_safe!(runtime, _repository_resource_id) do
    active_goal_run? =
      Repo.exists?(
        from run in Run,
          join: task in Task,
          on: task.id == run.task_id,
          where:
            run.runtime_id == ^runtime.id and not is_nil(task.goal_id) and
              run.state not in ^@terminal_states
      )

    retained_harness_session? =
      Repo.exists?(
        from session in HarnessSession,
          where: session.runtime_id == ^runtime.id and session.state != "closed"
      )

    if active_goal_run? or retained_harness_session?, do: rollback(:state_conflict)
  end

  defp normalize_task_attrs(attrs) do
    %{
      goal: value(attrs, :goal),
      agent_profile: value(attrs, :agent_profile),
      workspace: value(attrs, :workspace),
      input: value(attrs, :input),
      required_capabilities: value(attrs, :required_capabilities, %{})
    }
  end

  defp valid_boolean_capability?(capabilities, key) do
    case value(capabilities, key, :missing) do
      :missing -> true
      value -> is_boolean(value)
    end
  end

  defp valid_runtime_adapter_metadata?(specification, capabilities) do
    adapter_present? = map_key?(capabilities, :adapter)
    adapter = value(capabilities, :adapter)

    if runtime_metadata_present?(specification) do
      valid_runtime_metadata?(specification) and
        valid_registered_adapter?(adapter, adapter_present?, specification, capabilities)
    else
      not adapter_present?
    end
  end

  defp runtime_metadata_present?(specification) do
    Enum.any?(
      [:harness_kind, :harness_version, :adapter_version, :adapter_protocol_version],
      &map_key?(specification, &1)
    )
  end

  defp valid_runtime_metadata?(specification) do
    harness_kind = value(specification, :harness_kind)
    harness_version = value(specification, :harness_version)
    adapter_version = value(specification, :adapter_version)
    protocol_version = value(specification, :adapter_protocol_version)

    harness_kind in ["generic", "codex", "claude_code", "pi", "opencode"] and
      nonempty_bounded_text?(harness_version, 240) and
      nonempty_bounded_text?(adapter_version, 240) and
      is_integer(protocol_version) and protocol_version > 0 and protocol_version <= 2_147_483_647
  end

  defp valid_registered_adapter?(adapter, _adapter_present?, specification, capabilities)
       when is_map(adapter) do
    adapter_matches_runtime_metadata?(adapter, specification) and
      valid_adapter_operations?(value(adapter, :operations)) and
      generic_adapter_operations_allowed?(
        value(specification, :harness_kind),
        value(adapter, :operations)
      ) and
      native_capabilities_match_adapter?(
        value(specification, :harness_kind),
        capabilities,
        value(adapter, :operations)
      )
  end

  defp valid_registered_adapter?(nil, false, _specification, _capabilities), do: true

  defp valid_registered_adapter?(_adapter, _adapter_present?, _specification, _capabilities),
    do: false

  defp adapter_matches_runtime_metadata?(adapter, specification) do
    exact_map_keys?(adapter, [
      "kind",
      "native_version",
      "implementation_version",
      "protocol_version",
      "operations"
    ]) and
      value(adapter, :kind) == value(specification, :harness_kind) and
      value(adapter, :native_version) == value(specification, :harness_version) and
      value(adapter, :implementation_version) == value(specification, :adapter_version) and
      value(adapter, :protocol_version) == value(specification, :adapter_protocol_version)
  end

  defp valid_adapter_operations?(operations) when is_map(operations) do
    exact_map_keys?(operations, [
      "start",
      "events",
      "cancel",
      "resume",
      "handoff",
      "guidance",
      "pause",
      "approval_response",
      "usage",
      "hard_cost_limit"
    ]) and
      is_boolean(value(operations, :start)) and
      is_boolean(value(operations, :events)) and
      is_boolean(value(operations, :cancel)) and
      is_boolean(value(operations, :resume)) and
      is_boolean(value(operations, :handoff)) and
      value(operations, :guidance) in ["native_steer", "next_turn", "unsupported"] and
      value(operations, :pause) in ["safe_boundary", "unsupported"] and
      is_boolean(value(operations, :approval_response)) and
      value(operations, :usage) in ["reported", "estimated", "unknown"] and
      is_boolean(value(operations, :hard_cost_limit)) and
      (value(operations, :resume) != true or value(operations, :start) == true) and
      (value(operations, :cancel) != true or
         (value(operations, :start) == true and value(operations, :events) == true)) and
      (value(operations, :handoff) != true or
         (value(operations, :start) == true and value(operations, :events) == true and
            value(operations, :cancel) == true)) and
      (value(operations, :events) != true or value(operations, :start) == true) and
      (value(operations, :approval_response) != true or
         (value(operations, :start) == true and value(operations, :events) == true)) and
      (value(operations, :guidance) != "native_steer" or value(operations, :start) == true) and
      (value(operations, :pause) != "safe_boundary" or value(operations, :resume) == true) and
      (value(operations, :usage) != "reported" or value(operations, :events) == true)
  end

  defp valid_adapter_operations?(_operations), do: false

  defp generic_adapter_operations_allowed?("generic", operations) do
    value(operations, :resume) == false and
      value(operations, :handoff) == false and
      value(operations, :guidance) == "unsupported" and
      value(operations, :pause) == "unsupported" and
      value(operations, :approval_response) == false and
      value(operations, :usage) == "unknown" and
      value(operations, :hard_cost_limit) == false
  end

  defp generic_adapter_operations_allowed?(_harness_kind, _operations), do: true

  defp native_capabilities_match_adapter?("generic", _capabilities, _operations), do: true

  defp native_capabilities_match_adapter?(_harness_kind, capabilities, operations) do
    value(capabilities, :supervisory_control, false) != true and
      value(operations, :pause) == "unsupported"
  end

  defp legacy_supervisory_runtime?(runtime),
    do: is_nil(runtime.harness_kind) or runtime.harness_kind == "generic"

  defp exact_map_keys?(map, expected_keys) do
    normalized_keys =
      map
      |> Map.keys()
      |> Enum.map(fn
        key when is_binary(key) -> key
        key when is_atom(key) -> Atom.to_string(key)
        _key -> nil
      end)

    Enum.sort(normalized_keys) == Enum.sort(expected_keys)
  end

  defp map_key?(map, key) when is_map(map),
    do: Map.has_key?(map, key) or Map.has_key?(map, Atom.to_string(key))

  defp map_key?(_map, _key), do: false

  defp nonempty_bounded_text?(value, maximum)
       when is_binary(value) and byte_size(value) <= maximum,
       do: String.trim(value) != ""

  defp nonempty_bounded_text?(_value, _maximum), do: false

  defp valid_task_attrs?(task_attrs) do
    input = value(task_attrs, :input)
    required_capabilities = value(task_attrs, :required_capabilities, %{})

    (is_nil(input) or is_map(input)) and jsonb_compatible?(input) and
      is_map(required_capabilities) and jsonb_compatible?(required_capabilities) and
      valid_boolean_capability?(required_capabilities, :supervisory_control)
  end

  defp valid_command_request?(kind, payload) do
    jsonb_compatible?(payload) and
      case kind do
        kind when kind in ["cancel", "pause", "resume"] ->
          payload == %{}

        "provide_input" ->
          true

        "guidance" ->
          message = value(payload, :message)
          map_size(payload) == 1 and nonempty_text?(message) and byte_size(message) <= 32_768

        _ ->
          false
      end
  end

  defp ensure_expected_generation!(_task, nil), do: :ok

  defp ensure_expected_generation!(task, expected_generation)
       when is_integer(expected_generation) do
    if task.attempt_generation != expected_generation, do: rollback(:state_conflict)
  end

  defp ensure_expected_generation!(_task, _expected_generation), do: rollback(:invalid_request)

  defp ensure_expected_waiting!(_task, _run, nil), do: :ok

  defp ensure_expected_waiting!(task, _run, expected_transition_id)
       when is_binary(expected_transition_id) do
    if task.waiting_transition_id != expected_transition_id, do: rollback(:state_conflict)
  end

  defp ensure_expected_waiting!(_task, _run, _expected_transition_id),
    do: rollback(:invalid_request)

  defp normalize_command_payload("cancel", _payload), do: %{}
  defp normalize_command_payload("provide_input", payload), do: payload
  defp normalize_command_payload(kind, payload) when kind in @supervisory_commands, do: payload

  defp command_request_hash(kind, payload, opts),
    do: kind |> command_request_body(payload, opts) |> RequestHash.write(:command)

  defp command_request_body(kind, payload, opts) do
    context =
      opts |> Keyword.take([:expected_generation, :expected_waiting_transition_id]) |> Map.new()

    body = %{kind: kind, payload: payload}
    if map_size(context) == 0, do: body, else: Map.put(body, :context, context)
  end

  defp ensure_command_replay!(command, kind, payload, opts) do
    expected_hash =
      case command.request_hash_version do
        1 -> RequestHash.legacy(%{kind: kind, payload: payload})
        2 -> RequestHash.legacy(command_request_body(kind, payload, opts))
        3 -> RequestHash.canonical(command_request_body(kind, payload, opts))
      end

    unless command.request_hash == expected_hash, do: rollback(:idempotency_conflict)
  end

  defp lock_task_command(task_id, idempotency_key) do
    Repo.one(
      from command in Command,
        where: command.task_id == ^task_id and command.idempotency_key == ^idempotency_key,
        lock: "FOR UPDATE"
    )
  end

  defp legacy_cancel_idempotency_key(%Task{current_generation: 0, id: task_id}),
    do: "legacy-cancel:task:" <> task_id

  defp legacy_cancel_idempotency_key(task) do
    run = lock_current_run(task)
    "legacy-cancel:run:" <> run.id
  end

  defp valid_event?(event) when is_map(event) do
    valid_uuid?(value(event, :event_id)) and
      is_integer(value(event, :sequence)) and value(event, :sequence) >= 0 and
      is_binary(value(event, :kind)) and is_map(value(event, :payload, %{})) and
      match?(%DateTime{}, value(event, :occurred_at)) and
      jsonb_compatible?(value(event, :payload, %{}))
  end

  defp valid_event?(_), do: false

  # PostgreSQL jsonb rejects U+0000. Validate every untrusted JSONB value before a transaction.
  defp jsonb_compatible?(value) when is_binary(value), do: not String.contains?(value, <<0>>)

  defp jsonb_compatible?(%DateTime{}), do: true
  defp jsonb_compatible?(%_{}), do: false

  defp jsonb_compatible?(value) when is_list(value),
    do: Enum.all?(value, &jsonb_compatible?/1)

  defp jsonb_compatible?(value) when is_map(value) do
    Enum.all?(value, fn {key, nested_value} ->
      jsonb_key_compatible?(key) and jsonb_compatible?(nested_value)
    end)
  end

  defp jsonb_compatible?(value) when is_number(value) or is_boolean(value) or is_nil(value),
    do: true

  defp jsonb_compatible?(_), do: false

  defp jsonb_key_compatible?(key) when is_binary(key), do: jsonb_compatible?(key)

  defp jsonb_key_compatible?(key) when is_atom(key),
    do: key |> Atom.to_string() |> jsonb_compatible?()

  defp jsonb_key_compatible?(_), do: false

  defp valid_active_run?(run) when is_map(run) do
    valid_uuid?(value(run, :run_id)) and
      is_integer(value(run, :generation)) and value(run, :generation) > 0 and
      is_integer(value(run, :claimed_runtime_epoch)) and
      value(run, :claimed_runtime_epoch) > 0 and
      valid_uuid?(value(run, :claim_id)) and valid_uuid?(value(run, :lease_token)) and
      value(run, :state) in ["claimed", "running", "paused", "waiting_for_input", "cancelling"]
  end

  defp valid_active_run?(_), do: false

  defp valid_journal?(journal) when is_map(journal) do
    valid_active_run?(%{
      run_id: value(journal, :run_id),
      generation: value(journal, :generation),
      claimed_runtime_epoch: value(journal, :claimed_runtime_epoch),
      claim_id: value(journal, :claim_id),
      lease_token: value(journal, :lease_token),
      state: value(journal, :local_state)
    }) and
      is_integer(value(journal, :last_event_sequence)) and
      value(journal, :last_event_sequence) >= 0
  end

  defp valid_journal?(_), do: false

  defp event_body(event) do
    %{
      sequence: value(event, :sequence),
      kind: value(event, :kind),
      payload: value(event, :payload, %{}),
      occurred_at: value(event, :occurred_at)
    }
  end

  defp valid_uuid?(value) when is_binary(value), do: match?({:ok, _}, Ecto.UUID.cast(value))
  defp valid_uuid?(_), do: false

  defp value(map, key, default \\ nil)

  defp value(map, key, default) when is_map(map),
    do: Map.get(map, key, Map.get(map, Atom.to_string(key), default))

  defp value(_, _key, default), do: default

  defp digest(value), do: :crypto.hash(:sha256, value)

  defp emit(event, metadata),
    do: :telemetry.execute([:symmetry_control, :orchestration | event], %{count: 1}, metadata)

  defp owned?(schema, machine_id, id, machine_field)
       when is_binary(machine_id) and is_binary(id) do
    if valid_uuid?(machine_id) and valid_uuid?(id) do
      Repo.exists?(
        from record in schema,
          where: field(record, ^machine_field) == ^machine_id and record.id == ^id
      )
    else
      false
    end
  end

  defp owned?(_, _, _, _), do: false

  defp secure_compare(left, right) when byte_size(left) == byte_size(right),
    do: Plug.Crypto.secure_compare(left, right)

  defp secure_compare(_, _), do: false
end
