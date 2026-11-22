defmodule SymmetryControl.Goals.Internal do
  @moduledoc false

  @nonterminal_task_states [
    "queued",
    "assigned",
    "claimed",
    "running",
    "waiting_for_input",
    "paused",
    "cancelling"
  ]
  @max_safe_integer 9_007_199_254_740_991

  import Ecto.Query

  alias Ecto.Changeset
  alias SymmetryControl.Goals.ContextSnapshot
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.GoalDecision
  alias SymmetryControl.Goals.GoalExternalWait
  alias SymmetryControl.Goals.GoalRevision
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces.Project
  alias SymmetryControl.Workspaces.ProjectResource
  alias SymmetryControl.Workspaces.WorkItem

  def nullable_string(map, key) do
    case value(map, key) do
      nil ->
        {:ok, nil}

      value when is_binary(value) ->
        value = String.trim(value)
        if value != "", do: {:ok, value}, else: {:error, :invalid_request}

      _ ->
        {:error, :invalid_request}
    end
  end

  def nullable_safe_integer(map, key) do
    case value(map, key) do
      nil ->
        {:ok, nil}

      value when is_integer(value) and value >= 0 and value <= 9_007_199_254_740_991 ->
        {:ok, value}

      _ ->
        {:error, :invalid_request}
    end
  end

  def nullable_microusd(map, key) do
    case value(map, key) do
      nil ->
        {:ok, nil}

      value when is_binary(value) ->
        if Regex.match?(~r/^(?:0|[1-9][0-9]{0,18})$/, value) do
          case Integer.parse(value) do
            {parsed, ""} when parsed <= 9_223_372_036_854_775_807 -> {:ok, parsed}
            _ -> {:error, :invalid_request}
          end
        else
          {:error, :invalid_request}
        end

      _ ->
        {:error, :invalid_request}
    end
  end

  def only_known_keys?(map, allowed) do
    map
    |> Map.keys()
    |> Enum.all?(fn
      key when is_atom(key) or is_binary(key) -> to_string(key) in allowed
      _ -> false
    end)
  end

  def lock_goal(goal_id) do
    Repo.one(from(goal in Goal, where: goal.id == ^goal_id, lock: "FOR UPDATE")) ||
      rollback(:not_found)
  end

  def lock_command_goal(goal_id, mutation_id) do
    command_goal_lock_telemetry(:attempt, goal_id, mutation_id)
    goal = lock_goal(goal_id)
    command_goal_lock_telemetry(:acquired, goal.id, mutation_id)
    goal
  end

  def command_goal_lock_telemetry(phase, goal_id, mutation_id) do
    :telemetry.execute(
      [:symmetry_control, :goals, :command, :goal_lock],
      %{},
      %{phase: phase, goal_id: goal_id, mutation_id: mutation_id}
    )
  end

  def goal_project_id!(goal_id) do
    Repo.one(from(goal in Goal, where: goal.id == ^goal_id, select: goal.project_id)) ||
      rollback(:not_found)
  end

  # Archival takes FOR UPDATE. Acquire the compatible Project lock before a
  # command can authorize new Goal work or mutate Goal authority.
  def lock_project!(project_id) do
    Repo.one(from(project in Project, where: project.id == ^project_id, lock: "FOR SHARE")) ||
      rollback(:not_found)
  end

  def ensure_project_active!(%Project{status: "active"} = project), do: project
  def ensure_project_active!(_project), do: rollback(:state_conflict)

  def try_lock_active_goal_project(goal_id) do
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

  def lock_work_item(id) do
    Repo.one(from(item in WorkItem, where: item.id == ^id, lock: "FOR UPDATE")) ||
      rollback(:not_found)
  end

  def lock_external_wait(id) do
    Repo.one(from(wait in GoalExternalWait, where: wait.id == ^id, lock: "FOR UPDATE")) ||
      rollback(:not_found)
  end

  def lock_context_snapshot(goal_id, snapshot_id) do
    Repo.one(
      from(snapshot in ContextSnapshot,
        where: snapshot.goal_id == ^goal_id and snapshot.id == ^snapshot_id,
        lock: "FOR UPDATE"
      )
    ) || rollback(:not_found)
  end

  def lock_repository_resource!(id) do
    resource =
      Repo.one(from(resource in ProjectResource, where: resource.id == ^id, lock: "FOR UPDATE")) ||
        rollback(:not_found)

    if resource.kind != "repository", do: rollback(:resource_not_allowed)
    resource
  end

  # A plan may refer to several repositories in arbitrary proposal order.
  # Take every resource lock once in canonical UUID order, then use the map to
  # preserve the proposal's original business ordering during admission.
  def lock_repository_resources!(ids) do
    ids = ids |> Enum.uniq() |> Enum.sort()

    resources =
      Repo.all(
        from(resource in ProjectResource,
          where: resource.id in ^ids,
          order_by: [asc: resource.id],
          lock: "FOR UPDATE"
        )
      )

    if length(resources) != length(ids), do: rollback(:not_found)
    if Enum.any?(resources, &(&1.kind != "repository")), do: rollback(:resource_not_allowed)

    Map.new(resources, &{&1.id, &1})
  end

  def lock_work_items(ids) do
    rows =
      Repo.all(
        from(item in WorkItem,
          where: item.id in ^ids,
          order_by: [asc: item.id],
          lock: "FOR UPDATE"
        )
      )

    if length(rows) != length(Enum.uniq(ids)), do: rollback(:not_found)
    Map.new(rows, &{&1.id, &1}) |> then(fn found -> Enum.map(ids, &Map.fetch!(found, &1)) end)
  end

  def lock_task(id),
    do:
      Repo.one(from(task in Task, where: task.id == ^id, lock: "FOR UPDATE")) ||
        rollback(:not_found)

  def lock_run(id),
    do:
      Repo.one(from(run in Run, where: run.id == ^id, lock: "FOR UPDATE")) || rollback(:not_found)

  def lock_decision(id),
    do:
      Repo.one(from(decision in GoalDecision, where: decision.id == ^id, lock: "FOR UPDATE")) ||
        rollback(:not_found)

  def current_revision!(goal) do
    Repo.one(
      from(revision in GoalRevision,
        where: revision.goal_id == ^goal.id and revision.revision == ^goal.current_revision
      )
    ) || rollback(:stale_revision)
  end

  def active_task_for_item?(work_item_id) do
    Repo.exists?(
      from(task in Task,
        where: task.work_item_id == ^work_item_id and task.state in ^@nonterminal_task_states
      )
    )
  end

  def plan_task?(%Task{purpose: "plan", work_item_id: nil, validation_of_task_id: nil}), do: true
  def plan_task?(_task), do: false

  def parse_subject!(subject) do
    case parse_subject(subject) do
      {:ok, parsed} -> parsed
      _ -> rollback(:invalid_subject)
    end
  end

  def parse_subject(subject) when is_map(subject) do
    with true <- only_known_keys?(subject, ["resource_id", "commit", "tree_digest"]),
         {:ok, resource_id} <- required_uuid(subject, :resource_id),
         {:ok, commit} <- required_string(subject, :commit),
         {:ok, tree_digest} <- required_sha256(subject, :tree_digest),
         true <- valid_commit?(commit) do
      {:ok, %{"resource_id" => resource_id, "commit" => commit, "tree_digest" => tree_digest}}
    else
      _ -> {:error, :invalid_subject}
    end
  end

  def parse_subject(_), do: {:error, :invalid_subject}

  def valid_commit?(value),
    do: is_binary(value) and Regex.match?(~r/^[0-9a-f]{40}(?:[0-9a-f]{24})?$/, value)

  def required_sha256(map, key) do
    case value(map, key) do
      value when is_binary(value) ->
        if Regex.match?(~r/^sha256:[0-9a-f]{64}$/, value),
          do: {:ok, value},
          else: {:error, :invalid_request}

      _ ->
        {:error, :invalid_request}
    end
  end

  def required_uuid(map, key) do
    case value(map, key) do
      value when is_binary(value) ->
        if valid_uuid?(value), do: {:ok, value}, else: {:error, :invalid_request}

      _ ->
        {:error, :invalid_request}
    end
  end

  def required_uuid!(map, key), do: required_uuid(map, key) |> unwrap!()

  def optional_uuid(map, key) do
    case value(map, key) do
      nil ->
        {:ok, nil}

      value when is_binary(value) ->
        if valid_uuid?(value), do: {:ok, value}, else: {:error, :invalid_request}

      _ ->
        {:error, :invalid_request}
    end
  end

  def optional_uuid!(map, key), do: value(map, key) |> optional_uuid!()

  def optional_uuid!(nil), do: nil

  def optional_uuid!(value) when is_binary(value),
    do: if(valid_uuid?(value), do: value, else: rollback(:invalid_request))

  def optional_uuid!(_), do: rollback(:invalid_request)

  def valid_uuid(value), do: if(valid_uuid?(value), do: :ok, else: {:error, :invalid_request})

  def valid_uuid?(value) when is_binary(value), do: match?({:ok, _}, Ecto.UUID.cast(value))
  def valid_uuid?(_), do: false

  def required_string(map, key) do
    case value(map, key) do
      value when is_binary(value) ->
        value = String.trim(value)
        if byte_size(value) > 0, do: {:ok, value}, else: {:error, :invalid_request}

      _ ->
        {:error, :invalid_request}
    end
  end

  def required_string!(map, key), do: required_string(map, key) |> unwrap!()

  def required_map(map, key) do
    case value(map, key) do
      value when is_map(value) -> {:ok, value}
      _ -> {:error, :invalid_request}
    end
  end

  def required_map!(map, key), do: required_map(map, key) |> unwrap!()

  def required_list!(map, key), do: optional_list(map, key, :missing) |> unwrap!()

  def optional_list(map, key, default) do
    case value(map, key, default) do
      value when is_list(value) -> {:ok, value}
      _ -> {:error, :invalid_request}
    end
  end

  def required_positive_integer(map, key) do
    case value(map, key) do
      value when is_integer(value) and value > 0 -> {:ok, value}
      _ -> {:error, :invalid_request}
    end
  end

  def required_safe_positive_integer(map, key) do
    with {:ok, value} <- required_positive_integer(map, key),
         true <- value <= @max_safe_integer do
      {:ok, value}
    else
      _ -> {:error, :invalid_request}
    end
  end

  def parse_digest(value) when is_binary(value) do
    encoded = String.replace_prefix(value, "sha256:", "")

    if Regex.match?(~r/^[0-9a-f]{64}$/, encoded) and
         (value == encoded or value == "sha256:" <> encoded) do
      Base.decode16(encoded, case: :lower)
    else
      {:error, :invalid_request}
    end
  end

  def parse_digest(_), do: {:error, :invalid_request}

  def is_short_identifier?(value),
    do: is_binary(value) and Regex.match?(~r/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/, value)

  def policy_integer(policy, key, default \\ nil) do
    case value(policy || %{}, key, default) do
      value when is_integer(value) and value > 0 and value <= @max_safe_integer -> value
      nil -> nil
      _ -> rollback(:invalid_request)
    end
  end

  def allowed_values(policy, key) do
    case value(policy || %{}, key, []) do
      values when is_list(values) ->
        if Enum.all?(values, &is_binary/1), do: values, else: rollback(:invalid_request)

      _ ->
        rollback(:invalid_request)
    end
  end

  def settlement_mutation_id(task_id, run_id, generation) do
    mutation_id("settlement", [task_id, run_id, Integer.to_string(generation)])
  end

  # One deterministic UUID-shaped id for the durable mutation keys: sha256 over
  # "<prefix>:<part>:<part>…" rendered with the version-5 and "a" variant nibbles these
  # keys have always carried (not RFC 9562 name hashing). Settlement events and automatic
  # admissions are keyed by it, so a change to these bytes is a durable-key change.
  def mutation_id(prefix, parts) do
    hex =
      :crypto.hash(:sha256, prefix <> ":" <> Enum.join(parts, ":"))
      |> Base.encode16(case: :lower)

    String.slice(hex, 0, 8) <>
      "-" <>
      String.slice(hex, 8, 4) <>
      "-5" <>
      String.slice(hex, 13, 3) <>
      "-a" <> String.slice(hex, 17, 3) <> "-" <> String.slice(hex, 20, 12)
  end

  def plan_action_hash(goal, proposal_hash) do
    RequestHash.canonical(%{
      kind: "plan",
      goal_id: goal.id,
      revision: goal.current_revision,
      proposal_hash: Base.encode16(proposal_hash, case: :lower)
    })
  end

  def work_item_acceptance_action_hash(goal, work_item_id, subject_hash) do
    RequestHash.canonical(%{
      kind: "work_item_acceptance",
      goal_id: goal.id,
      revision: goal.current_revision,
      work_item_id: work_item_id,
      subject_hash: Base.encode16(subject_hash, case: :lower)
    })
  end

  def admission_enabled?(opts) do
    Keyword.get(
      opts,
      :rollout_enabled,
      Application.get_env(:symmetry_control, :goals, [])[:rollout_enabled] == true
    )
  end

  def value(map, key, default \\ nil) when is_map(map) do
    Map.get(map, key, Map.get(map, to_string(key), default))
  end

  def has_key?(map, key) when is_map(map),
    do: Map.has_key?(map, key) or Map.has_key?(map, to_string(key))

  def normalize_map(map) when is_map(map),
    do: Map.new(map, fn {key, value} -> {to_string(key), normalize_value(value)} end)

  def normalize_value(value) when is_map(value), do: normalize_map(value)
  def normalize_value(value) when is_list(value), do: Enum.map(value, &normalize_value/1)
  def normalize_value(value), do: value

  def stamp_insert(changeset, current) do
    changeset
    |> Changeset.put_change(:inserted_at, current)
    |> maybe_put_timestamp(:updated_at, current)
  end

  def maybe_put_timestamp(changeset, field, value) do
    if Map.has_key?(changeset.types, field),
      do: Changeset.put_change(changeset, field, value),
      else: changeset
  end

  def stamp_update(changeset, current),
    do: Changeset.force_change(changeset, :updated_at, current)

  def now(opts),
    do: Keyword.get(opts, :now, DateTime.utc_now() |> DateTime.truncate(:microsecond))

  def map_has_key?(map, key) when is_map(map) do
    Map.has_key?(map, key) or
      Enum.any?(Map.keys(map), fn candidate ->
        is_atom(candidate) and Atom.to_string(candidate) == key
      end)
  end

  def valid_long_text?(value),
    do: is_binary(value) and String.length(value) in 1..16_384

  def unwrap!({:ok, value}), do: value
  def unwrap!({:error, reason}), do: rollback(reason)

  def rollback(reason), do: Repo.rollback(reason)
end
