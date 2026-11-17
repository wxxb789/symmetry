defmodule SymmetryControl.Portal.Invalidations do
  @moduledoc """
  Operator-facing change invalidation for the portal real-time channel.

  This is a notification path, never authority. A published change only tells a
  connected operator browser that a served read may have changed, so the browser
  refetches the authoritative document; nothing here grants a permission, changes
  a transition policy or carries business truth. Payloads name the changed
  resource kind, its identity when the publisher knows it, the served version
  when the changed stream has one, and the durable event cursor when the changed
  stream is an event log. Transcript content, message text, credentials and
  evidence bodies are never published.

  Publishing is deliberately cheap and idempotent: a repeated or unexpected
  invalidation costs one extra read, while a missed one is recovered by the
  browser's reconnect snapshot refetch and by its polling fallback.
  """

  @topic "portal:invalidations"
  @kinds ~w(connection project resource work_item goal chat run runtime workspace)
  @fields [:kind, :id, :project_id, :work_item_id, :goal_id, :run_id, :version, :cursor]

  @doc "The PubSub topic every authenticated operator socket subscribes to."
  def topic, do: @topic

  @doc "Subscribes the calling process to portal invalidations (channel joins)."
  def subscribe do
    Phoenix.PubSub.subscribe(SymmetryControl.PubSub, @topic)
  end

  @doc """
  Publishes one committed change. `:kind` is required and must be a known
  resource kind; every other member is optional and dropped when nil. Call this
  after the change committed, never inside the writing transaction.
  """
  def publish(change) when is_map(change) do
    payload = change |> enrich() |> normalize()
    Phoenix.PubSub.broadcast(SymmetryControl.PubSub, @topic, {:portal_invalidation, payload})
    :ok
  end

  @doc "The normalized payload shape a publish accepts, for tests and publishers."
  def fields, do: @fields

  # A goal change carries the goal's own event sequence as its cursor, so a
  # connected receipt list continues from the last sequence it rendered. The
  # lookup is a hint only: when it fails the change is still published (without a
  # cursor) rather than failing the request that already committed.
  defp enrich(%{kind: kind, id: id} = change)
       when kind in ["goal", :goal] and is_binary(id) and not is_map_key(change, :cursor) do
    try do
      case SymmetryControl.Goals.current_event_sequence(id) do
        nil -> change
        cursor -> Map.put(change, :cursor, cursor)
      end
    rescue
      _exception -> change
    end
  end

  defp enrich(change), do: change

  defp normalize(change) do
    kind = change |> Map.get(:kind) |> to_string()

    if kind not in @kinds do
      raise ArgumentError, "unknown portal invalidation kind: #{inspect(kind)}"
    end

    change
    |> Map.take(@fields)
    |> Map.put(:kind, kind)
    |> Enum.reject(fn {_key, value} -> is_nil(value) end)
    |> Map.new()
  end
end
