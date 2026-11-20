defmodule SymmetryControlWeb.GoalController do
  use SymmetryControlWeb, :controller

  import Ecto.Query
  import SymmetryControlWeb.QueryParams

  alias SymmetryControl.Goals
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces.Project
  alias SymmetryControlWeb.Protocol
  alias SymmetryControlWeb.PortalJSON

  @max_bigint 9_223_372_036_854_775_807

  def create(conn, _params) do
    project_id = Map.fetch!(conn.path_params, "project_id")

    case Goals.create_goal(project_id, body_params(conn), actor_ref(conn)) do
      {:ok, receipt, :created} -> conn |> put_status(:created) |> json(receipt)
      {:ok, receipt, :replayed} -> json(conn, receipt)
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def show(conn, _params) do
    goal_id = Map.fetch!(conn.path_params, "goal_id")

    case Goals.fetch_goal(goal_id) do
      {:ok, projection} -> json(conn, portal_goal(conn, projection))
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def command(conn, _params) do
    goal_id = Map.fetch!(conn.path_params, "goal_id")

    case Goals.command(goal_id, body_params(conn), actor_ref(conn)) do
      {:ok, receipt, _disposition} -> json(conn, receipt)
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def events(conn, params) do
    goal_id = Map.fetch!(conn.path_params, "goal_id")

    with {:ok, options} <- event_options(conn, params),
         {:ok, page} <- Goals.list_events(goal_id, options) do
      json(conn, page)
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def graph(conn, _params) do
    goal_id = Map.fetch!(conn.path_params, "goal_id")

    case Goals.graph(goal_id) do
      {:ok, projection} -> json(conn, projection)
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def context(conn, _params) do
    goal_id = Map.fetch!(conn.path_params, "goal_id")
    snapshot_id = Map.fetch!(conn.path_params, "snapshot_id")

    case Goals.fetch_context(goal_id, snapshot_id) do
      {:ok, snapshot} -> json(conn, snapshot)
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  def attention(conn, params) do
    with {:ok, options} <- attention_options(params),
         {:ok, page} <- Goals.attention(options) do
      json(conn, page)
    else
      {:error, reason} -> Protocol.error(conn, reason)
    end
  end

  defp actor_ref(conn) do
    if String.starts_with?(conn.request_path, "/portal/") do
      "portal_operator"
    else
      "operator"
    end
  end

  # The portal read adds the project label the surface renders and the durable
  # decision lock version that `resolve_decision` requires as
  # `expected_decision_version`. Every authority member (allowed_actions,
  # blockers, readiness) stays exactly what the durable projection served, and
  # the operator API projection is untouched.
  defp portal_goal(conn, projection) do
    if String.starts_with?(conn.request_path, "/portal/") do
      PortalJSON.goal(projection, project(projection), decision_versions(projection))
    else
      projection
    end
  end

  defp project(projection), do: Repo.get(Project, projection.project_id)

  defp decision_versions(%{decisions: []}), do: %{}

  defp decision_versions(%{decisions: decisions}) do
    ids = Enum.map(decisions, & &1.id)

    Repo.all(from decision in SymmetryControl.Goals.GoalDecision, where: decision.id in ^ids)
    |> Map.new(&{&1.id, &1.lock_version})
  end

  defp body_params(conn) when is_map(conn.body_params), do: conn.body_params
  defp body_params(_conn), do: %{}

  defp event_options(conn, params) when is_map(params) do
    # The router's own path params travel with the query params; every other
    # parameter must be part of the accepted event read.
    # `after` is the goal audit sequence the reader has already rendered and
    # `limit` is the served page size, capped at the read model's own maximum.
    with true <- allowed_query_params?(conn, params, ["after", "limit"]),
         {:ok, after_sequence} <- nonnegative_integer(Map.get(params, "after")),
         {:ok, limit} <- optional_positive_integer(Map.get(params, "limit")),
         true <- is_nil(limit) or limit <= 100 do
      {:ok, [] |> maybe_put(:after, after_sequence) |> maybe_put(:limit, limit)}
    else
      _ -> {:error, :invalid_request}
    end
  end

  defp event_options(_conn, _params), do: {:error, :invalid_request}

  defp attention_options(params) when is_map(params) do
    with true <- MapSet.subset?(MapSet.new(Map.keys(params)), MapSet.new(["cursor", "limit"])),
         {:ok, cursor} <- optional_text(Map.get(params, "cursor")),
         {:ok, limit} <- optional_positive_integer(Map.get(params, "limit")) do
      {:ok, [] |> maybe_put(:cursor, cursor) |> maybe_put(:limit, limit)}
    else
      _ -> {:error, :invalid_request}
    end
  end

  defp attention_options(_params), do: {:error, :invalid_request}

  defp nonnegative_integer(nil), do: {:ok, nil}

  defp nonnegative_integer(value) when is_integer(value) and value >= 0 and value <= @max_bigint,
    do: {:ok, value}

  defp nonnegative_integer(value) when is_binary(value) do
    case Integer.parse(value) do
      {parsed, ""} when parsed >= 0 and parsed <= @max_bigint -> {:ok, parsed}
      _ -> {:error, :invalid_request}
    end
  end

  defp nonnegative_integer(_value), do: {:error, :invalid_request}
end
