defmodule SymmetryControl.Goals.EventsReceipts do
  @moduledoc false

  import Ecto.Query

  import SymmetryControl.Goals.Internal,
    only: [
      normalize_map: 1,
      now: 1,
      rollback: 1,
      stamp_insert: 2,
      stamp_update: 2,
      valid_uuid?: 1,
      value: 2
    ]

  alias Ecto.Changeset
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.GoalEvent
  alias SymmetryControl.Goals.ReadModel
  alias SymmetryControl.Goals.Workers.GoalControlWorker
  alias SymmetryControl.Goals.Workers.WakeupWorker
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash

  def replay_create(mutation_id, body) do
    case Repo.get(GoalEvent, mutation_id) do
      nil -> :missing
      event -> replay_event(event, body)
    end
  end

  def replay_command(goal_id, mutation_id, body) do
    case Repo.one(from(event in GoalEvent, where: event.id == ^mutation_id, lock: "FOR UPDATE")) do
      nil -> :missing
      %GoalEvent{goal_id: ^goal_id} = event -> replay_event(event, body)
      _event -> {:error, :idempotency_conflict}
    end
  end

  def replay_command_after_conflict(goal_id, mutation_id, body) do
    case Repo.get(GoalEvent, mutation_id) do
      %GoalEvent{goal_id: ^goal_id} = event -> replay_event(event, body)
      _ -> {:error, :idempotency_conflict}
    end
  end

  def replay_event(event, body) do
    if RequestHash.matches?(event.request_hash, event.request_hash_version, body) do
      goal = Repo.get(Goal, event.goal_id)

      if goal do
        case decode_receipt_snapshot(event.response, goal, event) do
          {:ok, receipt} -> {:ok, receipt}
          :error -> {:error, :internal_error}
        end
      else
        {:error, :not_found}
      end
    else
      {:error, :idempotency_conflict}
    end
  end

  def append_event!(
        goal,
        mutation_id,
        kind,
        actor_ref,
        body,
        response,
        opts,
        operator_advance
      ) do
    # Goal events are new v1 domain receipts. Their canonical bytes must not
    # depend on the legacy Orchestration request-hash rollout mode.
    request_hash = RequestHash.canonical(body)
    request_hash_version = 2
    sequence = goal.event_sequence + 1
    current = now(opts)
    next_wake_at = event_next_wake_at(goal, kind, opts)
    event_revision = Keyword.get(opts, :event_revision, goal.current_revision)
    # Only an operator command advances the version a command must carry. The
    # advance authority is this explicit argument, not a caller option: the
    # internal settlement, admission and reconcile appends pass false, so they
    # leave the version where the operator's last read left it and an
    # asynchronous advance cannot refuse a command whose own predicates still
    # hold.
    next_operator_version = goal.operator_version + if(operator_advance, do: 1, else: 0)

    response = normalize_map(response)

    receipt =
      immutable_receipt_snapshot(
        goal,
        mutation_id,
        kind,
        sequence,
        event_revision,
        response,
        next_wake_at,
        current,
        next_operator_version
      )

    stored_response = Map.put(response, "_receipt_v1", encode_receipt_snapshot(receipt))

    event_changeset =
      %GoalEvent{}
      |> GoalEvent.changeset(%{
        id: mutation_id,
        goal_id: goal.id,
        sequence: sequence,
        kind: kind,
        actor_ref: actor_ref,
        request_hash: request_hash,
        request_hash_version: request_hash_version,
        revision: event_revision,
        payload: normalize_map(body),
        response: stored_response
      })
      |> stamp_insert(current)
      |> Changeset.unique_constraint(:id, name: :goal_events_pkey)

    event =
      case Repo.insert(event_changeset) do
        {:ok, event} -> event
        {:error, _changeset} -> rollback(:idempotency_conflict)
      end

    updated_goal =
      goal
      |> Changeset.change(
        event_sequence: sequence,
        next_wake_at: next_wake_at,
        operator_version: next_operator_version
      )
      |> Changeset.optimistic_lock(:lock_version)
      |> stamp_update(current)
      |> Repo.update!()

    if next_wake_at != goal.next_wake_at or kind in ["resolve_decision", "task_settled"],
      do: enqueue_wakeup!(updated_goal, current)

    event
  end

  def enqueue_wakeup!(%Goal{next_wake_at: nil}, _current), do: :ok

  def enqueue_wakeup!(%Goal{next_wake_at: next_wake_at} = goal, current) do
    job_options =
      if DateTime.compare(next_wake_at, current) == :gt do
        # A future external check must survive an already-queued immediate hint.
        # The Goal row remains authoritative; stale wake jobs simply find it not due.
        [scheduled_at: next_wake_at, unique: false]
      else
        []
      end

    %{"goal_id" => goal.id}
    |> WakeupWorker.new(job_options)
    |> Oban.insert!()
  end

  def enqueue_goal_control!(goal, event) do
    %{
      "goal_id" => goal.id,
      "revision" => event.revision,
      "action_id" => event.id
    }
    |> GoalControlWorker.new()
    |> Oban.insert!()
  end

  def receipt!(goal, event) do
    case decode_receipt_snapshot(event.response, goal, event) do
      {:ok, receipt} -> receipt
      :error -> rollback(:internal_error)
    end
  end

  def immutable_receipt_snapshot(
        goal,
        mutation_id,
        kind,
        sequence,
        event_revision,
        response,
        next_wake_at,
        current,
        next_operator_version
      ) do
    %{
      "schema_version" => "symmetry.goal_receipt.v1",
      "goal" => %{
        "id" => goal.id,
        "state" => goal.state,
        "version" => next_operator_version,
        "current_revision" => goal.current_revision,
        "next_wake_at" => nullable_datetime(next_wake_at),
        "updated_at" => DateTime.to_iso8601(current)
      },
      "event" => %{
        "id" => mutation_id,
        "sequence" => sequence,
        "kind" => kind,
        "revision" => event_revision
      },
      "response" => response
    }
  end

  def encode_receipt_snapshot(receipt), do: normalize_map(receipt)

  def decode_receipt_snapshot(response, %Goal{} = goal, %GoalEvent{} = event)
      when is_map(response) do
    response = normalize_map(response)
    snapshot = value(response, "_receipt_v1")

    with %{
           "schema_version" => "symmetry.goal_receipt.v1",
           "goal" => goal_snapshot,
           "event" => event_snapshot,
           "response" => stored_response
         } <- snapshot,
         true <-
           Map.keys(snapshot) |> Enum.sort() == ["event", "goal", "response", "schema_version"],
         :ok <- validate_receipt_goal_snapshot(goal_snapshot, goal),
         :ok <- validate_receipt_event_snapshot(event_snapshot, event),
         true <- is_map(stored_response),
         true <- stored_response == Map.delete(response, "_receipt_v1"),
         {:ok, next_wake_at, updated_at} <- receipt_timestamps(goal_snapshot, event) do
      {:ok,
       %{
         goal: %{
           id: goal_snapshot["id"],
           state: goal_snapshot["state"],
           version: goal_snapshot["version"],
           current_revision: goal_snapshot["current_revision"],
           next_wake_at: next_wake_at,
           updated_at: updated_at
         },
         event: %{
           id: event_snapshot["id"],
           sequence: event_snapshot["sequence"],
           kind: event_snapshot["kind"],
           revision: event_snapshot["revision"]
         },
         response: stored_response,
         goal_id: goal_snapshot["id"],
         mutation_id: event_snapshot["id"]
       }}
    else
      _ -> :error
    end
  end

  def decode_receipt_snapshot(_response, _goal, _event), do: :error

  def validate_receipt_goal_snapshot(snapshot, goal) when is_map(snapshot) do
    with true <-
           Map.keys(snapshot) |> Enum.sort() ==
             ["current_revision", "id", "next_wake_at", "state", "updated_at", "version"],
         true <- valid_uuid?(snapshot["id"]) and snapshot["id"] == goal.id,
         true <- snapshot["state"] in ["draft", "active", "paused", "achieved", "cancelled"],
         true <- is_integer(snapshot["version"]) and snapshot["version"] > 0,
         true <- is_integer(snapshot["current_revision"]) and snapshot["current_revision"] > 0 do
      :ok
    else
      _ -> :error
    end
  end

  def validate_receipt_goal_snapshot(_snapshot, _goal), do: :error

  def validate_receipt_event_snapshot(snapshot, event) when is_map(snapshot) do
    with true <- Map.keys(snapshot) |> Enum.sort() == ["id", "kind", "revision", "sequence"],
         true <- valid_uuid?(snapshot["id"]) and snapshot["id"] == event.id,
         true <- is_integer(snapshot["sequence"]) and snapshot["sequence"] > 0,
         true <- snapshot["sequence"] == event.sequence,
         true <-
           is_binary(snapshot["kind"]) and snapshot["kind"] != "" and
             snapshot["kind"] == event.kind,
         true <- is_integer(snapshot["revision"]) and snapshot["revision"] > 0,
         true <- snapshot["revision"] == event.revision do
      :ok
    else
      _ -> :error
    end
  end

  def validate_receipt_event_snapshot(_snapshot, _event), do: :error

  def receipt_timestamps(snapshot, event) do
    with true <- valid_receipt_utc?(snapshot["updated_at"]),
         true <-
           is_nil(snapshot["next_wake_at"]) or valid_receipt_utc?(snapshot["next_wake_at"]),
         true <- is_struct(event.inserted_at, DateTime),
         true <- snapshot["updated_at"] == DateTime.to_iso8601(event.inserted_at) do
      {:ok, snapshot["next_wake_at"], snapshot["updated_at"]}
    else
      _ -> :error
    end
  end

  def valid_receipt_utc?(value) when is_binary(value) do
    case DateTime.from_iso8601(value) do
      {:ok, _datetime, 0} -> String.ends_with?(value, "Z")
      _ -> false
    end
  end

  def valid_receipt_utc?(_value), do: false

  def nullable_datetime(nil), do: nil
  def nullable_datetime(%DateTime{} = datetime), do: DateTime.to_iso8601(datetime)

  def response_for(goal, details) do
    Map.merge(
      %{
        "goal_id" => goal.id,
        "state" => goal.state,
        "version" => goal.operator_version + 1,
        "current_revision" => goal.current_revision
      },
      normalize_map(details)
    )
  end

  def event_next_wake_at(goal, kind, opts) do
    case Keyword.fetch(opts, :settlement_next_wake_at) do
      {:ok, next_wake_at} ->
        next_wake_at

      :error ->
        if goal.state == "active" and
             kind in [
               "activate",
               "resume",
               "admit_task",
               "resolve_decision",
               "task_unstarted_settled"
             ] do
          now(opts)
        else
          goal.next_wake_at
        end
    end
  end

  def ensure_preconditions!(goal, parsed) do
    if goal.operator_version != parsed.expected_version or
         goal.current_revision != parsed.expected_revision do
      {:ok, projection} = ReadModel.fetch(goal)

      rollback(
        {:stale,
         %{
           current_version: goal.operator_version,
           current_revision: goal.current_revision,
           allowed_actions: projection.allowed_actions
         }}
      )
    end
  end
end
