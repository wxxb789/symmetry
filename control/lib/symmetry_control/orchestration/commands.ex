defmodule SymmetryControl.Orchestration.Commands do
  @moduledoc false

  @supervisory_commands ["guidance", "pause", "resume"]

  import Ecto.Query

  import SymmetryControl.Goals.Eligibility,
    only: [
      valid_uuid?: 1,
      value: 2,
      value: 3
    ]

  import SymmetryControl.Orchestration.Assignment,
    only: [
      release_reserved_session!: 2
    ]

  import SymmetryControl.Orchestration.Claims,
    only: [
      ensure_fence!: 5,
      lock_chain: 1,
      lock_current_run: 1,
      lock_runtime: 1,
      lock_task: 1,
      lock_work_item: 1,
      now: 1,
      persist_insert: 1,
      rollback: 1,
      stamp_insert: 2,
      stamp_update: 2,
      valid_fence?: 1
    ]

  import SymmetryControl.Orchestration.Registry,
    only: [
      jsonb_compatible?: 1,
      legacy_supervisory_runtime?: 1,
      nonempty_text?: 1
    ]

  import SymmetryControl.Orchestration.TaskReads,
    only: [
      normalize_task_attrs: 1,
      pending_supervisory_transition?: 1,
      valid_task_attrs?: 1
    ]

  import SymmetryControl.Orchestration.Transitions,
    only: [
      enqueue_goal_task_terminal!: 4,
      ensure_terminal_fence!: 5,
      ensure_terminal_static_fence!: 3,
      fail_task_for_attempt_limit!: 2,
      goal_attempt_limit_exhausted?: 2,
      goal_control_idempotency_key: 3,
      next_retry_generation: 1,
      notify_goal_task_terminal: 3,
      reject_pending_commands!: 3,
      validate_decision_choice!: 4
    ]

  alias SymmetryControl.Orchestration.Command
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Task

  @spec retry_required_capabilities(map(), map() | nil) :: map()
  def retry_required_capabilities(required, current_required) when is_map(required) do
    if value(current_required, :supervisory_control, false) == true,
      do: Map.put(required, "supervisory_control", true),
      else: required
  end

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
      notify? = not Repo.in_transaction?()

      Repo.transaction(fn ->
        task = lock_task(task_id)
        current = now(opts)

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
             is_binary(task_id) and is_binary(kind) and is_map(payload) and
             is_binary(idempotency_key) and byte_size(idempotency_key) > 0 and is_list(opts) do
    {:error, :goal_authority_required}
  end

  def create_goal_control_command(_, _, _, _, _, _, _, _), do: {:error, :invalid_request}

  @doc false
  @spec create_goal_control_command_in_transaction(map(), map(), keyword()) ::
          {Command.t(), :created | :replayed, map() | nil} | {:error, term()}
  def create_goal_control_command_in_transaction(authorization, envelope, opts \\ [])

  def create_goal_control_command_in_transaction(
        %{
          goal_id: goal_id,
          revision: revision,
          action_id: action_id,
          kind: kind
        },
        %{task_id: task_id, payload: payload, idempotency_key: idempotency_key} = envelope,
        opts
      )
      when is_integer(revision) and revision > 0 and is_binary(kind) and is_binary(task_id) and
             is_map(payload) and is_binary(idempotency_key) and byte_size(idempotency_key) > 0 and
             is_list(opts) do
    if valid_uuid?(goal_id) and valid_uuid?(action_id) and valid_uuid?(task_id) and
         kind in ["pause", "cancel"] and valid_command_request?(kind, payload) and
         idempotency_key == goal_control_idempotency_key(action_id, kind, task_id) do
      if Repo.in_transaction?() do
        command_hash = command_request_hash(kind, payload, opts)
        untrusted_task = Repo.get(Task, task_id) || rollback(:not_found)
        item = if untrusted_task.work_item_id, do: lock_work_item(untrusted_task.work_item_id)
        task = lock_task(task_id)
        current = now(opts)

        ensure_goal_control_task_ownership!(goal_id, item, task)
        ensure_goal_control_run_envelope!(task, envelope)

        create_or_replay_locked_command!(
          task,
          kind,
          payload,
          idempotency_key,
          command_hash,
          current,
          opts
        )
      else
        {:error, :transaction_required}
      end
    else
      {:error, :invalid_request}
    end
  end

  def create_goal_control_command_in_transaction(_, _, _), do: {:error, :invalid_request}

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
    notify? = not Repo.in_transaction?()

    Repo.transaction(fn ->
      task = lock_task(task_id)
      current = now(opts)

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
      Repo.transaction(fn ->
        task = lock_task(task_id)
        current = now(opts)

        if not is_nil(task.goal_id), do: rollback(:goal_authority_required)

        task_attrs =
          Map.update!(
            task_attrs,
            :required_capabilities,
            &retry_required_capabilities(&1, task.required_capabilities)
          )

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

              current = now(opts)

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
        current = now(opts)

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
        current = now(opts)
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
         _current,
         opts
       ) do
    ensure_expected_generation!(task, Keyword.get(opts, :expected_generation))

    if task.state != "waiting_for_input" do
      rollback(:state_conflict)
    end

    run = lock_current_run(task)
    current = now(opts)

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

  defp create_new_command!(task, kind, payload, idempotency_key, command_hash, _current, opts)
       when kind in @supervisory_commands do
    ensure_expected_generation!(task, Keyword.get(opts, :expected_generation))
    if task.state not in ["running", "paused"], do: rollback(:state_conflict)
    run = lock_current_run(task)
    runtime = lock_runtime(run.runtime_id)
    current = now(opts)

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
      Repo.transaction(fn ->
        run_id =
          Repo.one(
            from command in Command, where: command.id == ^command_id, select: command.run_id
          ) ||
            rollback(:not_found)

        {task, run, runtime} = lock_chain(run_id)

        command =
          Repo.one!(from command in Command, where: command.id == ^command_id, lock: "FOR UPDATE")

        current = now(opts)

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

  defp ensure_goal_control_task_ownership!(goal_id, item, task) do
    owned? =
      task.goal_id == goal_id and
        case task.work_item_id do
          nil -> is_nil(item)
          work_item_id -> (item && item.id == work_item_id) and item.goal_id == goal_id
        end

    unless owned?, do: rollback(:ownership_lost)
  end

  defp ensure_goal_control_run_envelope!(task, envelope) do
    run_id = value(envelope, :run_id)
    generation = value(envelope, :generation)

    case {run_id, generation} do
      {nil, nil} ->
        :ok

      {run_id, generation} when is_binary(run_id) and is_integer(generation) and generation > 0 ->
        run = lock_current_run(task)

        unless run.id == run_id and run.generation == generation,
          do: rollback(:ownership_lost)

      _ ->
        rollback(:invalid_request)
    end
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
end
