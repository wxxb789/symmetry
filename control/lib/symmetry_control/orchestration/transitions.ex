defmodule SymmetryControl.Orchestration.Transitions do
  @moduledoc false

  @terminal_states ["completed", "failed", "cancelled", "expired"]
  @terminal_grace_ms 8 * 60 * 1_000
  @terminal_targets ["completed", "failed", "cancelled"]
  @supervisory_commands ["guidance", "pause", "resume"]
  @decision_reasons [
    "blocked",
    "consequential",
    "irreversible",
    "security",
    "business_policy",
    "expensive",
    "product_change"
  ]

  import Ecto.Query

  import SymmetryControl.Goals.Eligibility,
    only: [
      valid_uuid?: 1,
      value: 2,
      value: 3
    ]

  import SymmetryControl.Orchestration.Assignment,
    only: [
      mark_reserved_session_unavailable!: 2,
      release_reserved_session!: 2
    ]

  import SymmetryControl.Orchestration.Claims,
    only: [
      emit: 2,
      ensure_current_goal_execution!: 3,
      ensure_current_goal_execution_if_present!: 3,
      ensure_fence!: 5,
      ensure_static_fence!: 4,
      lock_chain: 1,
      lock_goal_chain: 1,
      lock_runtime: 1,
      now: 1,
      rollback: 1,
      static_fence_matches?: 3,
      stamp_insert: 2,
      stamp_update: 2,
      valid_fence?: 1
    ]

  import SymmetryControl.Orchestration.Registry,
    only: [
      jsonb_compatible?: 1,
      nonempty_text?: 1,
      snapshot_for: 2,
      valid_active_run?: 1
    ]

  import SymmetryControl.Orchestration.TaskReads,
    only: [
      current_waiting_context: 2,
      share_runtime: 1
    ]

  alias SymmetryControl.Orchestration.Command
  alias SymmetryControl.Goals.GoalEvent
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.RunEvent
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.RunTransition
  alias SymmetryControl.Goals.Workers.SettleTaskWorker
  alias SymmetryControl.Orchestration.Task

  @doc false
  @spec emit_transition(Run.t(), String.t()) :: :ok
  def emit_transition(%Run{} = run, state) when is_binary(state) do
    emit([:run, :transition], %{
      run_id: run.id,
      generation: run.generation,
      state: state
    })
  end

  @spec append_events(Ecto.UUID.t(), map(), [map()], keyword()) ::
          {:ok, [RunEvent.t()]} | {:error, atom()}
  def append_events(run_id, fence, events, opts \\ [])

  def append_events(run_id, fence, events, opts) when is_map(fence) and is_list(events) do
    if not (valid_uuid?(run_id) and valid_fence?(fence) and
              Enum.all?(events, &valid_event?/1)) do
      {:error, :invalid_request}
    else
      Repo.transaction(fn ->
        {task, run, runtime, goal, item} = lock_goal_chain(run_id)
        current = now(opts)
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
      if target_state == "paused" and goal_owned_run?(run_id) do
        {:error, :goal_authority_required}
      else
        body = %{state: target_state, payload: payload}
        {body_hash, body_hash_version} = RequestHash.write(body)
        notify? = not Repo.in_transaction?()

        result =
          Repo.transaction(fn ->
            {task, run, runtime, goal, item} = lock_transition_chain(run_id, target_state)
            current = now(opts)
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

                if target_state == "waiting_for_input",
                  do: validate_decision_packet!(task, payload)

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
            emit_transition(run, target_state)

            notify_goal_task_terminal(receipt, opts, notify?)
            {:ok, run}

          {:ok, {:replayed, run}} ->
            {:ok, run}

          error ->
            error
        end
      end
    end
  end

  def transition(_, _, _, _, _, _), do: {:error, :invalid_request}

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
  def apply_goal_pause_transition_in_transaction(
        task,
        run,
        fence,
        payload,
        transition_id,
        opts \\ []
      )

  def apply_goal_pause_transition_in_transaction(
        %Task{} = task,
        %Run{} = run,
        fence,
        payload,
        transition_id,
        opts
      )
      when is_map(fence) and is_map(payload) and is_binary(transition_id) and is_list(opts) do
    if not (Repo.in_transaction?() and valid_fence?(fence) and valid_uuid?(transition_id) and
              jsonb_compatible?(payload)) do
      {:error, :invalid_request}
    else
      body = %{state: "paused", payload: payload}
      {body_hash, body_hash_version} = RequestHash.write(body)
      current = now(opts)

      if run.task_id != task.id do
        {:error, :ownership_lost}
      else
        runtime = lock_runtime(run.runtime_id)
        ensure_transition_static_fence!(task, run, runtime, fence, "paused")

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
            ensure_transition_fence!(task, run, runtime, fence, "paused", current)

            {:created,
             transition_once!(
               task,
               run,
               "paused",
               payload,
               transition_id,
               body_hash,
               body_hash_version,
               current,
               nil
             )}
        end
      end
    end
  end

  def apply_goal_pause_transition_in_transaction(_, _, _, _, _, _),
    do: {:error, :invalid_request}

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
    Repo.transaction(fn ->
      runtime = share_runtime(runtime_id)
      current = now(opts)
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
        case expire_run(run_id, opts) do
          {:ok, {metadata, receipt}} ->
            emit([:run, :expired], metadata)
            notify_goal_task_terminal(receipt, opts, notify?)
            true

          :not_expired ->
            false
        end
      end)

    offline_runtimes = expire_offline_runtimes(opts)
    %{expired_runs: expired_runs, offline_runtimes: offline_runtimes}
  end

  defp expire_run(run_id, opts) do
    case Repo.transaction(fn ->
           {task, run, _runtime} = lock_chain(run_id)
           current = now(opts)

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

  defp expire_offline_runtimes(opts) do
    runtime_ids =
      Repo.all(
        from runtime in Runtime,
          where: runtime.status == "online",
          select: runtime.id
      )

    Enum.count(runtime_ids, fn runtime_id ->
      case Repo.transaction(fn ->
             runtime = lock_runtime(runtime_id)
             current = now(opts)
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

  @doc false
  def next_retry_generation(%Task{goal_id: nil, attempt_generation: generation}),
    do: generation + 1

  def next_retry_generation(%Task{} = task) do
    case Repo.one(from run in Run, where: run.task_id == ^task.id, select: max(run.generation)) do
      nil -> 1
      generation -> generation + 1
    end
  end

  @doc false
  def goal_attempt_limit_exhausted?(
        %Task{goal_id: goal_id, max_run_attempts: maximum},
        generation
      )
      when not is_nil(goal_id) and is_integer(maximum),
      do: generation > maximum

  def goal_attempt_limit_exhausted?(_task, _generation), do: false

  @doc false
  def fail_task_for_attempt_limit!(task, current) do
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

  @doc false
  def enqueue_goal_task_terminal!(task, run, terminal_state, opts)
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

  def enqueue_goal_task_terminal!(_task, _run, _state, _opts), do: nil

  defp goal_task_terminal_receipt(%Task{goal_id: goal_id}, %Run{} = run)
       when not is_nil(goal_id) do
    %{task_id: run.task_id, run_id: run.id, generation: run.generation}
  end

  defp goal_task_terminal_receipt(_task, _run), do: nil

  defp settlement_job_args(%{task_id: task_id, run_id: run_id, generation: generation}) do
    %{"task_id" => task_id, "run_id" => run_id, "generation" => generation}
  end

  @doc false
  def notify_goal_task_terminal(_receipt, _opts, false), do: :ok

  def notify_goal_task_terminal(nil, _opts, true), do: :ok

  def notify_goal_task_terminal(receipt, opts, true) do
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

  @doc false
  def goal_control_idempotency_key(action_id, kind, task_id),
    do: "goal-control:#{action_id}:#{kind}:#{task_id}"

  defp pending_pause?(run_id) do
    Repo.exists?(
      from command in Command,
        where:
          command.run_id == ^run_id and command.kind == "pause" and command.state == "pending"
    )
  end

  @doc false
  def reject_pending_commands!(task, run_id, current) do
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

  @doc false
  def validate_decision_choice!(task, run, payload, opts) do
    waiting = current_waiting_context(task, run)

    if waiting && waiting.decision do
      unless Keyword.get(opts, :expected_waiting_transition_id) == task.waiting_transition_id,
        do: rollback(:invalid_request)

      option_id = value(payload, :option_id)

      unless Enum.any?(value(waiting.decision, :options, []), &(value(&1, :id) == option_id)),
        do: rollback(:invalid_request)
    end
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

  @doc false
  def ensure_terminal_fence!(task, run, _runtime, fence, current) do
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

  @doc false
  def ensure_terminal_static_fence!(task, run, fence) do
    unless static_fence_matches?(task, run, fence), do: rollback(:ownership_lost)
  end

  # A Goal pause transition consumes a Goal-issued control action and keeps the
  # Goal -> WorkItem -> Task -> Run lock order; its paused Goal state is the authority.
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

  defp goal_owned_run?(run_id) do
    Repo.exists?(
      from run in Run,
        join: task in Task,
        on: task.id == run.task_id,
        where: run.id == ^run_id and not is_nil(task.goal_id)
    )
  end

  defp ensure_current_goal_transition_authority!(nil, _item, _task, _target_state), do: :ok

  defp ensure_current_goal_transition_authority!(_goal, _item, _task, target_state)
       when target_state in @terminal_targets or target_state == "paused",
       do: :ok

  defp ensure_current_goal_transition_authority!(goal, item, task, _target_state) do
    ensure_current_goal_execution!(goal, item, task)
  end

  defp event_replay([], _existing_events), do: :missing

  defp event_replay(events, existing_events) do
    {all_present?, conflict?, stored, _request_hashes} =
      Enum.reduce(events, {true, false, [], %{}}, fn event,
                                                     {all_present?, conflict?, stored,
                                                      request_hashes} ->
        event_id = value(event, :event_id)
        body = event_body(event)
        request_hash = RequestHash.write(body)

        batch_conflict? =
          case Map.get(request_hashes, event_id) do
            nil ->
              false

            ^request_hash ->
              false

            _ ->
              true
          end

        request_hashes = Map.put_new(request_hashes, event_id, request_hash)

        case Map.get(existing_events, event_id) do
          nil ->
            {false, conflict? or batch_conflict?, stored, request_hashes}

          existing ->
            matches? =
              RequestHash.matches?(existing.request_hash, existing.request_hash_version, body)

            {all_present?, conflict? or batch_conflict? or not matches?, [existing | stored],
             request_hashes}
        end
      end)

    cond do
      conflict? -> :conflict
      all_present? -> {:replayed, Enum.reverse(stored)}
      true -> :missing
    end
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

  defp valid_event?(event) when is_map(event) do
    valid_uuid?(value(event, :event_id)) and
      is_integer(value(event, :sequence)) and value(event, :sequence) >= 0 and
      is_binary(value(event, :kind)) and is_map(value(event, :payload, %{})) and
      match?(%DateTime{}, value(event, :occurred_at)) and
      jsonb_compatible?(value(event, :payload, %{}))
  end

  defp valid_event?(_), do: false

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
end
