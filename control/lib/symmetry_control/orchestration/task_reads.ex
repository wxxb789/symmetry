defmodule SymmetryControl.Orchestration.TaskReads do
  @moduledoc false

  @capacity_bearing_states [
    "assigned",
    "claimed",
    "running",
    "paused",
    "waiting_for_input",
    "cancelling"
  ]
  @max_history_limit 500
  @default_history_limit 100
  @timeline_source_ranks %{"event" => 0, "transition" => 1, "command" => 2}

  import Ecto.Query

  import SymmetryControl.Goals.Eligibility,
    only: [
      valid_uuid?: 1,
      value: 2,
      value: 3
    ]

  import SymmetryControl.Orchestration.Assignment,
    only: [
      runtime_matches_binding: 0
    ]

  import SymmetryControl.Orchestration.Claims,
    only: [
      insert_ignoring_conflict: 3,
      now: 1,
      rollback: 1,
      stamp_insert: 2
    ]

  import SymmetryControl.Orchestration.Registry,
    only: [
      jsonb_compatible?: 1,
      legacy_supervisory_runtime?: 1,
      snapshot_for: 2,
      valid_boolean_capability?: 2
    ]

  alias SymmetryControl.Orchestration.Command
  alias SymmetryControl.Orchestration.Machine
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.RunEvent
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.RunTransition
  alias SymmetryControl.Orchestration.Task

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
  def task_snapshot(task_id) when is_binary(task_id) do
    if valid_uuid?(task_id) do
      Repo.transaction(fn ->
        task = share_task(task_id)

        run =
          Repo.one(
            from run in Run,
              where: run.task_id == ^task.id and run.generation == ^task.attempt_generation
          )

        runtime = run && Repo.get(Runtime, run.runtime_id)

        %{
          task: task,
          run: run,
          runtime: runtime,
          waiting: current_waiting_context(task, run),
          latest_command: latest_task_command(task.id),
          controls:
            control_capabilities(
              task,
              run,
              runtime,
              not is_nil(run) and pending_supervisory_transition?(run.id)
            )
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

  @spec adapter_operation_supported?(Runtime.t() | nil, atom()) :: boolean()
  def adapter_operation_supported?(%Runtime{} = runtime, operation) when is_atom(operation) do
    adapter = value(runtime.capabilities, :adapter)

    is_map(adapter) and value(value(adapter, :operations), operation) == true
  end

  def adapter_operation_supported?(_runtime, _operation), do: false

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

  @retry_binding_types %{
    task_id: :binary_id,
    agent_profile: :string,
    workspace: :string,
    required_capabilities: :map
  }

  @spec retryable_attempt?(Task.t()) :: boolean()
  def retryable_attempt?(%Task{goal_id: nil, state: state}) when state in ["failed", "cancelled"],
    do: true

  def retryable_attempt?(%Task{}), do: false

  @spec retry_runtime_eligible_task_ids([retry_binding()]) :: MapSet.t(Ecto.UUID.t())
  def retry_runtime_eligible_task_ids(bindings) when is_list(bindings) do
    rows = Enum.map(bindings, &retry_binding_row/1)

    from(binding in values(rows, @retry_binding_types),
      cross_join: runtime in Runtime,
      where: ^runtime_matches_binding(),
      where: fragment("? @> ?", runtime.capabilities, type(^retry_adapter_requirement(), :map)),
      distinct: true,
      select: binding.task_id
    )
    |> Repo.all()
    |> MapSet.new()
  end

  @spec retry_runtime_eligible?(retry_binding()) :: boolean()
  def retry_runtime_eligible?(binding) when is_map(binding) do
    not Enum.empty?(retry_runtime_eligible_task_ids([binding]))
  end

  defp retry_binding_row(binding) do
    Map.take(binding, Map.keys(@retry_binding_types))
  end

  # A retried attempt starts a fresh run, so its runtime must advertise `start`.
  def retry_adapter_requirement, do: %{"adapter" => %{"operations" => %{"start" => true}}}

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

  @doc false
  def current_waiting_context(
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

  def current_waiting_context(_, _), do: nil

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
    Repo.transaction(fn ->
      runtime = share_runtime(runtime_id)
      current = now(opts)
      if runtime.connection_epoch != runtime_epoch, do: rollback(:ownership_lost)
      snapshot_for(runtime, current)
    end)
  end

  @doc false
  def pending_supervisory_transition?(run_id) do
    Repo.exists?(
      from command in Command,
        where:
          command.run_id == ^run_id and command.kind in ["pause", "resume"] and
            command.state in ["pending", "applied"]
    )
  end

  @doc false
  def share_runtime(runtime_id),
    do:
      Repo.one(from runtime in Runtime, where: runtime.id == ^runtime_id, lock: "FOR SHARE") ||
        rollback(:not_found)

  defp share_task(task_id),
    do:
      Repo.one(from task in Task, where: task.id == ^task_id, lock: "FOR SHARE") ||
        rollback(:not_found)

  @doc false
  def normalize_task_attrs(attrs) do
    %{
      goal: value(attrs, :goal),
      agent_profile: value(attrs, :agent_profile),
      workspace: value(attrs, :workspace),
      input: value(attrs, :input),
      required_capabilities: value(attrs, :required_capabilities, %{})
    }
  end

  @doc false
  def valid_task_attrs?(task_attrs) do
    input = value(task_attrs, :input)
    required_capabilities = value(task_attrs, :required_capabilities, %{})

    (is_nil(input) or is_map(input)) and jsonb_compatible?(input) and
      is_map(required_capabilities) and jsonb_compatible?(required_capabilities) and
      valid_boolean_capability?(required_capabilities, :supervisory_control)
  end
end
