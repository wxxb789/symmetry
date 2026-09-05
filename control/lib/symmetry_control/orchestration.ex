defmodule SymmetryControl.Orchestration do
  @moduledoc """
  Durable task orchestration backed exclusively by PostgreSQL.

  This context owns lifecycle transitions and the fencing fields used by the
  daemon protocol. Callers may use PubSub as a wake-up hint, never as state.
  """

  import Ecto.Query

  alias SymmetryControl.Repo

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
  @capacity_bearing_states ["assigned", "claimed", "running", "waiting_for_input", "cancelling"]
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
        request_hash = request_hash(%{name: value(attrs, :name), machine_token: token})

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
                  enrollment_request_hash: request_hash
                })

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
                    %Machine{enrollment_request_hash: ^request_hash} = machine ->
                      {machine, :replayed}

                    %Machine{} ->
                      rollback(:idempotency_conflict)

                    nil ->
                      rollback(:invalid_request)
                  end

                :invalid ->
                  rollback(:invalid_request)
              end

            %Machine{enrollment_request_hash: ^request_hash} = machine ->
              {machine, :replayed}

            %Machine{} ->
              rollback(:idempotency_conflict)
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

          existing =
            Repo.one(
              from runtime in Runtime,
                where: runtime.machine_id == ^machine_id and runtime.runtime_key == ^runtime_key,
                lock: "FOR UPDATE"
            )

          attrs = %{
            name: value(specification, :name),
            capacity: value(specification, :capacity),
            agent_profile: value(specification, :agent_profile),
            workspace: value(specification, :workspace),
            capabilities: value(specification, :capabilities, %{}),
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
    task_attrs = %{
      goal: value(attrs, :goal),
      agent_profile: value(attrs, :agent_profile),
      workspace: value(attrs, :workspace),
      input: value(attrs, :input)
    }

    if not valid_task_attrs?(task_attrs) do
      {:error, :invalid_request}
    else
      request_hash = request_hash(task_attrs)
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
                  state: "queued",
                  current_generation: 0,
                  attempt_generation: 1,
                  waiting_transition_id: nil
                })
              )

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
                  %Task{request_hash: ^request_hash} = task -> {task, :replayed}
                  %Task{} -> rollback(:idempotency_conflict)
                  nil -> rollback(:invalid_request)
                end

              :invalid ->
                rollback(:invalid_request)
            end

          task when task.request_hash == request_hash ->
            {task, :replayed}

          _task ->
            rollback(:idempotency_conflict)
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
          latest_command: latest_task_command(task.id)
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
            nil -> {transition.payload, transition.inserted_at}
            event -> {event.payload, event.inserted_at}
          end

        %{
          run_id: run.id,
          generation: run.generation,
          transition_id: transition.transition_id,
          question: value(payload, :question),
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

    Repo.transaction(fn ->
      {task, runtime} = next_assignable_task_and_runtime(current) || rollback(:no_assignment)

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
    end)
  end

  defp next_assignable_task_and_runtime(current) do
    Repo.one(
      from task in Task,
        join: runtime in Runtime,
        on:
          runtime.status == "online" and runtime.agent_profile == task.agent_profile and
            runtime.workspace == task.workspace,
        where: task.state == "queued",
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
  def claim(run_id, request, opts \\ [])

  def claim(run_id, request, opts) when is_map(request) do
    with true <- valid_uuid?(run_id) and valid_claim_request?(request),
         {:ok, lease_duration_ms} <- lease_duration_ms(opts) do
      current = now(opts)

      Repo.transaction(fn ->
        {task, run, runtime} = lock_chain(run_id)
        request_runtime_id = value(request, :runtime_id)
        request_epoch = value(request, :runtime_epoch)
        request_generation = value(request, :generation)
        request_claim_id = value(request, :claim_id)

        cond do
          run.runtime_id != request_runtime_id or runtime.id != request_runtime_id ->
            rollback(:ownership_lost)

          runtime.connection_epoch != request_epoch or
            task.current_generation != request_generation or
              run.generation != request_generation ->
            rollback(:ownership_lost)

          run.state in ["claimed", "cancelling"] and run.claim_id == request_claim_id and
            run.claimed_runtime_epoch == request_epoch and
            not is_nil(run.lease_expires_at) and
              DateTime.compare(run.lease_expires_at, current) == :gt ->
            run

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

            task |> Task.changeset(%{state: "claimed"}) |> stamp_update(current) |> Repo.update!()
            run
        end
      end)
    else
      _ -> {:error, :invalid_request}
    end
  end

  def claim(_, _, _), do: {:error, :invalid_request}

  @spec renew_lease(Ecto.UUID.t(), map(), keyword()) :: {:ok, Run.t()} | {:error, atom()}
  def renew_lease(run_id, fence, opts \\ [])

  def renew_lease(run_id, fence, opts) when is_map(fence) do
    with true <- valid_uuid?(run_id) and valid_fence?(fence),
         {:ok, lease_duration_ms} <- lease_duration_ms(opts) do
      current = now(opts)

      Repo.transaction(fn ->
        {task, run, runtime} = lock_chain(run_id)
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
        {task, run, runtime} = lock_chain(run_id)
        ensure_fence!(task, run, runtime, fence, current)

        event_ids = Enum.map(events, &value(&1, :event_id))

        existing_events =
          Repo.all(
            from event in RunEvent,
              where: event.run_id == ^run.id and event.event_id in ^event_ids
          )
          |> Map.new(&{&1.event_id, &1})

        {stored, _events_by_id} =
          Enum.map_reduce(events, existing_events, fn event, events_by_id ->
            event_id = value(event, :event_id)
            event_hash = request_hash(event_body(event))

            case Map.get(events_by_id, event_id) do
              nil ->
                stored_event =
                  %RunEvent{}
                  |> RunEvent.changeset(%{
                    run_id: run.id,
                    event_id: event_id,
                    request_hash: event_hash,
                    sequence: value(event, :sequence),
                    kind: value(event, :kind),
                    payload: value(event, :payload, %{}),
                    occurred_at: value(event, :occurred_at, current)
                  })
                  |> stamp_insert(current)
                  |> Repo.insert!()

                {stored_event, Map.put(events_by_id, event_id, stored_event)}

              %RunEvent{request_hash: ^event_hash} = existing ->
                {existing, events_by_id}

              %RunEvent{} ->
                rollback(:idempotency_conflict)
            end
          end)

        stored
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
      body_hash = request_hash(%{state: target_state, payload: payload})

      Repo.transaction(fn ->
        {task, run, runtime} = lock_chain(run_id)
        ensure_transition_static_fence!(task, run, runtime, fence, target_state)

        case Repo.get_by(RunTransition, run_id: run.id, transition_id: transition_id) do
          %RunTransition{request_hash: ^body_hash} = transition ->
            transition_response(run, transition)

          %RunTransition{} ->
            rollback(:idempotency_conflict)

          nil ->
            ensure_cancelled_transition_authority!(run, target_state)
            ensure_transition_fence!(task, run, runtime, fence, target_state, current)

            transition_once!(
              task,
              run,
              target_state,
              payload,
              transition_id,
              body_hash,
              current
            )
        end
      end)
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
      command_hash = request_hash(%{kind: kind, payload: normalized_payload})
      current = now(opts)

      Repo.transaction(fn ->
        task = lock_task(task_id)

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
        {:ok, {command, disposition}} -> {:ok, command, disposition}
        error -> error
      end
    end
  end

  def create_command(_, _, _, _, _), do: {:error, :invalid_request}

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

    Repo.transaction(fn ->
      task = lock_task(task_id)

      if task.state in ["completed", "failed"] do
        {task, nil}
      else
        idempotency_key = legacy_cancel_idempotency_key(task)
        command_hash = request_hash(%{kind: "cancel", payload: %{}})

        {command, _disposition} =
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
        {task, command}
      end
    end)
    |> case do
      {:ok, {task, command}} -> {:ok, task, command}
      error -> error
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
    task_attrs = %{
      goal: value(attrs, :goal),
      agent_profile: value(attrs, :agent_profile),
      workspace: value(attrs, :workspace),
      input: value(attrs, :input)
    }

    if not (valid_uuid?(task_id) and valid_task_attrs?(task_attrs)) do
      {:error, :invalid_request}
    else
      payload = %{work: task_attrs}
      command_hash = request_hash(%{kind: "retry", payload: payload})
      current = now(opts)

      Repo.transaction(fn ->
        task = lock_task(task_id)

        case lock_task_command(task.id, idempotency_key) do
          %Command{request_hash: ^command_hash} = command ->
            {task, command, :replayed}

          %Command{} ->
            rollback(:idempotency_conflict)

          nil ->
            ensure_expected_generation!(task, Keyword.get(opts, :expected_generation))
            if task.state not in ["failed", "cancelled"], do: rollback(:state_conflict)

            run = if task.current_generation > 0, do: lock_current_run(task), else: nil

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
                  attempt_generation: task.attempt_generation + 1,
                  waiting_transition_id: nil,
                  result: nil,
                  failure: nil
                })
              )
              |> stamp_update(current)
              |> Repo.update!()

            {task, command, :created}
        end
      end)
      |> case do
        {:ok, {task, command, disposition}} -> {:ok, task, command, disposition}
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
      %Command{request_hash: ^command_hash} = command ->
        {command, :replayed}

      %Command{} ->
        rollback(:idempotency_conflict)

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

        {command, :created}

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

        run
        |> Run.changeset(%{state: "cancelled"})
        |> stamp_update(current)
        |> Repo.update!()

        task
        |> Task.changeset(%{state: "cancelled", waiting_transition_id: nil})
        |> stamp_update(current)
        |> Repo.update!()

        {command, :created}

      state when state in ["claimed", "running", "waiting_for_input"] ->
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

        {command, :created}

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
    if task.state != "waiting_for_input" do
      rollback(:state_conflict)
    end

    run = lock_current_run(task)

    if run.state != "waiting_for_input", do: rollback(:state_conflict)

    ensure_expected_waiting!(task, run, Keyword.get(opts, :expected_waiting_transition_id))

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

        {command, :created}

      _command ->
        rollback(:state_conflict)
    end
  end

  defp insert_command!(
         task_id,
         run_id,
         generation,
         kind,
         payload,
         idempotency_key,
         command_hash,
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

          true ->
            ensure_terminal_fence!(task, run, runtime, fence, current)

            cond do
              outcome not in ["applied", "rejected", "failed"] ->
                rollback(:invalid_request)

              outcome == "applied" and command.kind == "provide_input" and
                  run.state == "waiting_for_input" ->
                rollback(:state_conflict)

              true ->
                command
                |> Command.changeset(%{
                  state: "acknowledged",
                  acknowledgement_id: acknowledgement_id,
                  acknowledgement_outcome: outcome,
                  acknowledged_at: current
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

    run_ids =
      Repo.all(
        from run in Run,
          where:
            (run.state == "assigned" and run.assignment_expires_at <= ^current) or
              (run.state in ["claimed", "running", "waiting_for_input", "cancelling"] and
                 run.lease_expires_at <= ^current),
          select: run.id
      )

    expired_runs = Enum.count(run_ids, &expire_run(&1, current))
    offline_runtimes = expire_offline_runtimes(current)
    %{expired_runs: expired_runs, offline_runtimes: offline_runtimes}
  end

  defp expire_run(run_id, current) do
    case Repo.transaction(fn ->
           {task, run, _runtime} = lock_chain(run_id)

           expired? =
             (run.state == "assigned" and
                DateTime.compare(run.assignment_expires_at, current) != :gt) or
               (run.state in ["claimed", "running", "waiting_for_input", "cancelling"] and
                  not is_nil(run.lease_expires_at) and
                  DateTime.compare(run.lease_expires_at, current) != :gt)

           if not expired?, do: rollback(:not_expired)

           terminal_state = if run.state == "cancelling", do: "cancelled", else: "expired"

           run
           |> Run.changeset(%{state: terminal_state})
           |> stamp_update(current)
           |> Repo.update!()

           if task.current_generation == run.generation do
             task_state = if terminal_state == "cancelled", do: "cancelled", else: "queued"

             task_attrs =
               if task_state == "queued" do
                 %{
                   state: task_state,
                   attempt_generation: task.current_generation + 1,
                   waiting_transition_id: nil
                 }
               else
                 %{state: task_state, waiting_transition_id: nil}
               end

             task
             |> Task.changeset(task_attrs)
             |> stamp_update(current)
             |> Repo.update!()
           end

           :expired
         end) do
      {:ok, :expired} -> true
      _ -> false
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

               :offline
             else
               :online
             end
           end) do
        {:ok, :offline} -> true
        _ -> false
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
         current
       ) do
    validate_transition!(run.state, target_state)
    ensure_expired_run_can_settle_task!(task, run)

    run_attrs =
      case target_state do
        "completed" -> %{state: target_state, result: payload}
        "failed" -> %{state: target_state, failure: payload}
        _ -> %{state: target_state}
      end

    task_attrs =
      target_state
      |> task_attrs_for_transition(payload, transition_id)
      |> restore_expired_attempt_generation(run)

    run = run |> Run.changeset(run_attrs) |> stamp_update(current) |> Repo.update!()
    task |> Task.changeset(task_attrs) |> stamp_update(current) |> Repo.update!()

    %RunTransition{}
    |> RunTransition.changeset(%{
      run_id: run.id,
      transition_id: transition_id,
      request_hash: body_hash,
      state: target_state,
      payload: payload
    })
    |> stamp_insert(current)
    |> Repo.insert!()

    run
  end

  defp task_attrs_for_transition("completed", payload, _transition_id),
    do: %{state: "completed", waiting_transition_id: nil, result: payload, failure: nil}

  defp task_attrs_for_transition("failed", payload, _transition_id),
    do: %{state: "failed", waiting_transition_id: nil, result: nil, failure: payload}

  defp task_attrs_for_transition("waiting_for_input", _payload, transition_id),
    do: %{state: "waiting_for_input", waiting_transition_id: transition_id}

  defp task_attrs_for_transition(target_state, _payload, _transition_id),
    do: %{state: target_state, waiting_transition_id: nil}

  defp restore_expired_attempt_generation(task_attrs, %Run{
         state: "expired",
         generation: generation
       }),
       do: Map.put(task_attrs, :attempt_generation, generation)

  defp restore_expired_attempt_generation(task_attrs, _run), do: task_attrs

  defp ensure_expired_run_can_settle_task!(%Task{state: "queued"}, %Run{state: "expired"}),
    do: :ok

  defp ensure_expired_run_can_settle_task!(_task, %Run{state: "expired"}),
    do: rollback(:ownership_lost)

  defp ensure_expired_run_can_settle_task!(_task, _run), do: :ok

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
       when state in ["waiting_for_input", "completed", "failed"], do: true

  defp valid_transition?("waiting_for_input", state)
       when state in ["running", "completed", "failed"],
       do: true

  defp valid_transition?("cancelling", "cancelled"), do: true
  defp valid_transition?(_, _), do: false

  defp validate_transition!(current_state, target_state) do
    cond do
      target_state not in ["running", "waiting_for_input" | @terminal_targets] ->
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
                 (command.state == "pending" and command.kind == "provide_input" and
                    is_nil(command.acknowledged_at) and
                    run.state in ["waiting_for_input", "running"])),
          order_by: [asc: command.inserted_at]
      )

    %{assignments: assignments, commands: commands, server_time: current}
  end

  defp ensure_fence!(task, run, runtime, fence, current) do
    ensure_static_fence!(task, run, runtime, fence)

    valid? =
      run.state in ["claimed", "running", "waiting_for_input", "cancelling"] and
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
      (run.state in ["claimed", "running", "waiting_for_input", "cancelling"] and
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
    is_map(capabilities) and jsonb_compatible?(capabilities)
  end

  defp valid_runtime_specification?(_), do: false

  defp valid_task_attrs?(task_attrs) do
    input = value(task_attrs, :input)
    (is_nil(input) or is_map(input)) and jsonb_compatible?(input)
  end

  defp valid_command_request?(kind, payload) do
    kind in ["cancel", "provide_input"] and jsonb_compatible?(payload) and
      (kind != "cancel" or payload == %{})
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

  defp jsonb_compatible?(value) when is_list(value),
    do: Enum.all?(value, &jsonb_compatible?/1)

  defp jsonb_compatible?(value) when is_map(value) do
    Enum.all?(value, fn {key, nested_value} ->
      jsonb_key_compatible?(key) and jsonb_compatible?(nested_value)
    end)
  end

  defp jsonb_compatible?(value) when is_number(value) or is_boolean(value) or is_nil(value),
    do: true

  defp jsonb_compatible?(_), do: true

  defp jsonb_key_compatible?(key) when is_binary(key), do: jsonb_compatible?(key)

  defp jsonb_key_compatible?(key) when is_atom(key),
    do: key |> Atom.to_string() |> jsonb_compatible?()

  defp jsonb_key_compatible?(_), do: true

  defp valid_active_run?(run) when is_map(run) do
    valid_uuid?(value(run, :run_id)) and
      is_integer(value(run, :generation)) and value(run, :generation) > 0 and
      is_integer(value(run, :claimed_runtime_epoch)) and
      value(run, :claimed_runtime_epoch) > 0 and
      valid_uuid?(value(run, :claim_id)) and valid_uuid?(value(run, :lease_token)) and
      value(run, :state) in ["claimed", "running", "waiting_for_input", "cancelling"]
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
  defp request_hash(value), do: value |> :erlang.term_to_binary() |> digest()

  defp secure_compare(left, right) when byte_size(left) == byte_size(right),
    do: Plug.Crypto.secure_compare(left, right)

  defp secure_compare(_, _), do: false
end
