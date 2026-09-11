defmodule SymmetryControl.Goals.ReadModel do
  @moduledoc """
  Read-only projections for the durable Goal records.

  This module deliberately does not use WorkItem board state, model narration, or
  in-memory process state as evidence of progress. Accepted work is derived from
  immutable `WorkOutcome` receipts bound to the current Goal revision and subject.
  Task and Run state is exposed separately as execution information.
  """

  import Ecto.Query

  alias SymmetryControl.Goals.{
    ContractValidation,
    ContextSnapshot,
    Goal,
    GoalBudgetReservation,
    GoalEvent,
    GoalExternalWait,
    RunEvidence,
    RunUsage
  }

  alias SymmetryControl.Orchestration.{Run, Runtime, Task}
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash

  @terminal_goal_states ["achieved", "cancelled"]
  @nonterminal_task_states [
    "queued",
    "assigned",
    "claimed",
    "running",
    "waiting_for_input",
    "paused",
    "cancelling"
  ]
  @active_task_states [
    "queued",
    "assigned",
    "claimed",
    "running",
    "waiting_for_input",
    "paused",
    "cancelling"
  ]
  @terminal_task_states ["completed", "failed", "cancelled"]
  @accounting_terminal_run_states @terminal_task_states ++ ["expired"]
  @runtime_bound_states ["assigned", "claimed", "running"]
  @settlement_attention_states [
    "awaiting_validation",
    "no_verified_progress",
    "repair_required",
    "replan_required",
    "failed",
    "invalid_task_result",
    "missing_result"
  ]
  @event_limit 100
  @attention_limit 100
  @context_limit 32_000
  @task_result_schema_version "symmetry.task_result.v1"

  @doc "Fetch and project one Goal from durable records."
  @spec fetch(Ecto.UUID.t() | Goal.t(), keyword()) :: {:ok, map()} | {:error, :not_found}
  def fetch(goal_or_id, opts \\ [])

  def fetch(%Goal{} = goal, opts), do: {:ok, project(goal, load_data(goal, opts))}

  def fetch(goal_id, opts) do
    repo = Keyword.get(opts, :repo, Repo)

    case repo.get(Goal, goal_id) do
      nil -> {:error, :not_found}
      goal -> {:ok, project(goal, load_data(goal, opts))}
    end
  end

  @doc "Build a Goal projection from a Goal struct/map and optional related rows."
  @spec project(map(), map() | keyword()) :: map()
  def project(goal, related \\ %{})

  def project(goal, related) when is_list(related), do: project(goal, Map.new(related))

  def project(goal, related) when is_map(goal) and is_map(related) do
    goal = map_value(goal, :goal, goal)
    current_revision = map_value(goal, :current_revision)
    revisions = records(related, :revisions, map_value(goal, :revisions, []))
    revision = current_revision_record(revisions, current_revision)
    work_items = records(related, :work_items, map_value(goal, :work_items, []))
    dependencies = records(related, :dependencies, map_value(goal, :dependencies, []))
    outcomes = records(related, :outcomes, map_value(goal, :outcomes, []))
    decisions = records(related, :decisions, map_value(goal, :decisions, []))
    tasks = records(related, :tasks, [])
    runs = records(related, :runs, [])
    evidence = records(related, :evidence, [])
    reservations = records(related, :reservations, [])
    usage = records(related, :usage, [])
    runtimes = records(related, :runtimes, [])
    external_waits = records(related, :external_waits, [])
    task_settled_events = records(related, :task_settled_events, records(related, :events, []))

    automatic_admission_blocked_events =
      records(related, :automatic_admission_blocked_events, records(related, :events, []))

    automatic_reconciliation_deferred_events =
      records(related, :automatic_reconciliation_deferred_events, records(related, :events, []))

    current_work_items = current_revision_work_items(work_items, current_revision)
    current_work_item_ids = MapSet.new(Enum.map(current_work_items, &map_value(&1, :id)))
    current_dependencies = current_revision_dependencies(dependencies, current_work_item_ids)

    current_outcomes =
      current_revision_outcomes(outcomes, current_revision, current_work_item_ids)

    current_tasks = current_goal_tasks(tasks, current_work_item_ids, current_revision)
    current_task_ids = MapSet.new(Enum.map(current_tasks, &map_value(&1, :id)))
    current_runs = Enum.filter(runs, &MapSet.member?(current_task_ids, map_value(&1, :task_id)))

    accepted = accepted_outcomes(current_outcomes)
    execution = execution_by_work_item(current_work_items, current_tasks, current_runs, runtimes)

    graph = graph_projection(current_work_items, current_dependencies, accepted, current_revision)

    settled_receipts =
      current_task_settlements(
        goal,
        current_work_items,
        current_outcomes,
        tasks,
        runs,
        task_settled_events,
        current_revision
      )

    blockers =
      blockers(
        goal,
        revision,
        current_work_items,
        current_dependencies,
        current_outcomes,
        decisions,
        current_tasks,
        current_runs,
        settled_receipts,
        evidence,
        reservations,
        usage,
        runtimes,
        external_waits,
        automatic_admission_blocked_events,
        automatic_reconciliation_deferred_events,
        current_revision,
        accepted,
        tasks,
        runs
      )

    settlement_attention =
      settlement_attention(
        goal,
        settled_receipts,
        decisions,
        current_revision
      )

    completion =
      completion_projection(
        goal,
        revision,
        current_work_items,
        current_dependencies,
        current_outcomes,
        decisions,
        tasks,
        runs,
        evidence,
        current_revision,
        accepted,
        blockers
      )

    %{
      id: map_value(goal, :id),
      project_id: map_value(goal, :project_id),
      title: map_value(goal, :title),
      state: map_value(goal, :state, "draft"),
      current_revision: current_revision,
      next_wake_at: map_value(goal, :next_wake_at),
      updated_at: map_value(goal, :updated_at),
      version: map_value(goal, :lock_version),
      lifecycle: lifecycle_projection(map_value(goal, :state, "draft")),
      revision: revision_projection(revision),
      blockers: blockers,
      blocker_reasons: Enum.map(blockers, & &1.reason),
      settlement_attention: settlement_attention,
      attention_reasons: settlement_attention |> Enum.map(& &1.settlement) |> Enum.uniq(),
      completion: completion,
      allowed_actions:
        allowed_actions(
          map_value(goal, :state, "draft"),
          completion,
          current_work_items,
          current_tasks,
          decisions,
          current_revision,
          task_settled_events
        ),
      work_items:
        Enum.map(current_work_items, fn item ->
          item_projection(item, current_revision, accepted, execution, graph)
        end),
      graph: graph,
      decisions: Enum.map(decisions, &decision_projection(&1, current_revision)),
      accepted_outcomes: accepted |> Map.values() |> Enum.map(&outcome_projection/1),
      execution: goal_execution_projection(current_tasks, current_runs),
      planning: planning_execution_projection(current_tasks, current_runs, runtimes),
      accounting: accounting_projection(reservations, usage, tasks, runs),
      external_waits:
        external_waits
        |> Enum.filter(&(map_value(&1, :goal_revision) == current_revision))
        |> Enum.map(&external_wait_projection/1),
      history: %{
        work_items:
          work_items
          |> Enum.reject(&MapSet.member?(current_work_item_ids, map_value(&1, :id)))
          |> Enum.map(&historical_work_item_projection/1),
        dependencies:
          dependencies
          |> Enum.reject(&current_dependency?(&1, current_work_item_ids))
          |> Enum.map(&dependency_projection/1),
        outcomes:
          outcomes
          |> Enum.reject(&current_outcome?(&1, current_revision, current_work_item_ids))
          |> Enum.map(&outcome_projection/1),
        external_waits:
          external_waits
          |> Enum.reject(&(map_value(&1, :goal_revision) == current_revision))
          |> Enum.map(&external_wait_projection/1)
      },
      source: :durable_records
    }
  end

  @doc "List compact, ordered Goal audit events without exposing raw payloads."
  @spec events(Ecto.UUID.t() | Goal.t(), keyword()) :: {:ok, map()} | {:error, :not_found}
  def events(goal_or_id, opts \\ [])

  def events(%Goal{id: goal_id}, opts), do: events(goal_id, opts)

  def events(goal_id, opts) do
    repo = Keyword.get(opts, :repo, Repo)
    limit = bounded_limit(Keyword.get(opts, :limit, @event_limit), @event_limit)
    after_sequence = Keyword.get(opts, :after)

    query =
      from event in GoalEvent,
        where: event.goal_id == ^goal_id,
        order_by: [asc: event.sequence, asc: event.id],
        limit: ^(limit + 1)

    query =
      if is_integer(after_sequence),
        do: where(query, [event], event.sequence > ^after_sequence),
        else: query

    case repo.get(Goal, goal_id) do
      nil ->
        {:error, :not_found}

      _goal ->
        rows = repo.all(query)
        {page, rest} = Enum.split(rows, limit)
        next_after = if rest == [], do: nil, else: page |> List.last() |> map_value(:sequence)

        {:ok, %{entries: Enum.map(page, &event_projection/1), next_after: next_after}}
    end
  end

  @doc "Return the dependency graph for a Goal, using accepted receipts for edge satisfaction."
  @spec graph(Ecto.UUID.t() | map(), keyword()) :: {:ok, map()} | {:error, :not_found}
  def graph(goal_or_projection, opts \\ [])

  def graph(%{graph: graph}, _opts) when is_map(graph), do: {:ok, graph}

  def graph(%Goal{} = goal, opts), do: graph(project(goal, load_data(goal, opts)), opts)

  def graph(goal_id, opts) do
    case fetch(goal_id, opts) do
      {:ok, projection} -> {:ok, projection.graph}
      error -> error
    end
  end

  @doc "Return a compact attention feed for Goals and their durable blockers."
  @spec attention(keyword() | map()) :: {:ok, map()} | {:error, term()}
  def attention(opts \\ [])

  def attention(opts) when is_map(opts), do: attention(normalize_options(opts))

  def attention(opts) when is_list(opts) do
    repo = Keyword.get(opts, :repo, Repo)
    limit = bounded_limit(Keyword.get(opts, :limit, @attention_limit), @attention_limit)
    state = Keyword.get(opts, :state)
    project_id = Keyword.get(opts, :project_id)

    query =
      from goal in Goal,
        order_by: [desc: goal.updated_at, desc: goal.id],
        limit: ^(limit + 1)

    query = if state, do: where(query, [goal], goal.state == ^state), else: query
    query = if project_id, do: where(query, [goal], goal.project_id == ^project_id), else: query

    with {:ok, query} <-
           attention_cursor(query, Keyword.get(opts, :cursor) || Keyword.get(opts, :after)) do
      rows = repo.all(query)

      {page, rest} = Enum.split(rows, limit)

      entries =
        page
        |> Enum.map(fn goal ->
          case fetch(goal, Keyword.put(opts, :repo, repo)) do
            {:ok, projection} -> attention_entry(projection)
            _ -> nil
          end
        end)
        |> Enum.reject(&is_nil/1)
        |> Enum.filter(&attention_entry?/1)

      next_cursor = next_attention_cursor(rest, page)
      {:ok, %{entries: entries, next_after: next_cursor, next_cursor: next_cursor}}
    end
  rescue
    error in [Ecto.Query.CastError] -> {:error, error}
  end

  @doc "Fetch one authorized compact ContextSnapshot projection."
  @spec context(Ecto.UUID.t(), Ecto.UUID.t() | nil, keyword()) ::
          {:ok, map()} | {:error, :not_found}
  def context(goal_id, snapshot_id, opts \\ []) do
    repo = Keyword.get(opts, :repo, Repo)

    query =
      from snapshot in ContextSnapshot,
        where: snapshot.goal_id == ^goal_id,
        order_by: [desc: snapshot.inserted_at, desc: snapshot.id],
        limit: 1

    query = if snapshot_id, do: where(query, [snapshot], snapshot.id == ^snapshot_id), else: query

    case repo.one(query) do
      nil -> {:error, :not_found}
      snapshot -> {:ok, context_projection(snapshot, opts)}
    end
  end

  @doc "Sanitize an arbitrary context payload at the read boundary."
  @spec sanitize_context(term()) :: term()
  def sanitize_context(payload), do: sanitize(payload, @context_limit)

  defp load_data(goal, opts) do
    repo = Keyword.get(opts, :repo, Repo)

    preloaded =
      goal
      |> repo.preload([:revisions, :work_items, :dependencies, :outcomes, :decisions])

    task_query =
      from task in Task,
        where: task.goal_id == ^goal.id,
        order_by: [asc: task.inserted_at, asc: task.id]

    tasks = repo.all(task_query)
    task_ids = Enum.map(tasks, & &1.id)

    runs =
      if task_ids == [] do
        []
      else
        repo.all(
          from run in Run,
            where: run.task_id in ^task_ids,
            order_by: [asc: run.generation, asc: run.id]
        )
      end

    run_ids = Enum.map(runs, & &1.id)

    evidence =
      if run_ids == [] do
        []
      else
        repo.all(
          from row in RunEvidence,
            where: row.run_id in ^run_ids,
            order_by: [asc: row.inserted_at, asc: row.id]
        )
      end

    usage =
      if run_ids == [] do
        []
      else
        repo.all(
          from row in RunUsage,
            where: row.run_id in ^run_ids,
            order_by: [asc: row.inserted_at, asc: row.id]
        )
      end

    reservations = repo.all(from row in GoalBudgetReservation, where: row.goal_id == ^goal.id)

    external_waits =
      repo.all(
        from wait in GoalExternalWait,
          where: wait.goal_id == ^goal.id,
          order_by: [asc: wait.inserted_at, asc: wait.id]
      )

    task_settled_events =
      repo.all(
        from event in GoalEvent,
          where:
            event.goal_id == ^goal.id and event.revision == ^goal.current_revision and
              event.kind == "task_settled",
          order_by: [asc: event.sequence, asc: event.id]
      )

    automatic_admission_blocked_events =
      repo.all(
        from event in GoalEvent,
          where:
            event.goal_id == ^goal.id and event.revision == ^goal.current_revision and
              event.kind == "automatic_admission_blocked",
          order_by: [asc: event.sequence, asc: event.id]
      )

    automatic_reconciliation_deferred_events =
      repo.all(
        from event in GoalEvent,
          where:
            event.goal_id == ^goal.id and event.revision == ^goal.current_revision and
              event.kind == "automatic_reconciliation_deferred",
          order_by: [asc: event.sequence, asc: event.id]
      )

    runtimes =
      runs
      |> Enum.map(&map_value(&1, :runtime_id))
      |> Enum.reject(&is_nil/1)
      |> Enum.uniq()
      |> case do
        [] -> []
        runtime_ids -> repo.all(from runtime in Runtime, where: runtime.id in ^runtime_ids)
      end

    %{
      goal: preloaded,
      revisions: preloaded.revisions,
      work_items: preloaded.work_items,
      dependencies: preloaded.dependencies,
      outcomes: preloaded.outcomes,
      decisions: preloaded.decisions,
      tasks: tasks,
      runs: runs,
      evidence: evidence,
      usage: usage,
      reservations: reservations,
      external_waits: external_waits,
      runtimes: runtimes,
      task_settled_events: task_settled_events,
      automatic_admission_blocked_events: automatic_admission_blocked_events,
      automatic_reconciliation_deferred_events: automatic_reconciliation_deferred_events
    }
  end

  defp current_revision_record(_revisions, nil), do: nil

  defp current_revision_record(revisions, current_revision),
    do: Enum.find(revisions, &(map_value(&1, :revision) == current_revision))

  defp lifecycle_projection(state) do
    %{
      state: state,
      terminal?: state in @terminal_goal_states,
      scheduling_paused?: state != "active",
      immutable?: state in @terminal_goal_states
    }
  end

  defp revision_projection(nil), do: nil

  defp revision_projection(revision) do
    %{
      revision: map_value(revision, :revision),
      objective: map_value(revision, :objective),
      non_goals: list_value(revision, :non_goals),
      acceptance_contract: sanitize(map_value(revision, :acceptance_contract), @context_limit),
      authority_policy: sanitize(map_value(revision, :authority_policy), @context_limit),
      execution_policy: sanitize(map_value(revision, :execution_policy), @context_limit),
      context_manifest: sanitize(map_value(revision, :context_manifest), @context_limit),
      inserted_at: map_value(revision, :inserted_at)
    }
  end

  defp item_projection(item, current_revision, accepted, execution, graph) do
    id = map_value(item, :id)
    receipt = Map.get(accepted, id)
    item_execution = Map.get(execution, id)
    node = Enum.find(graph.nodes, &(&1.id == id))

    %{
      id: id,
      number: map_value(item, :number),
      title: map_value(item, :title),
      description: map_value(item, :description),
      status: map_value(item, :status),
      board_status: map_value(item, :status),
      required?: map_value(item, :required, true),
      priority: map_value(item, :priority),
      position: map_value(item, :position),
      assignee_type: map_value(item, :assignee_type),
      assignee_name: map_value(item, :assignee_name),
      agent_profile: map_value(item, :agent_profile),
      workspace: map_value(item, :workspace),
      admitted_revision: map_value(item, :admitted_revision),
      repository_resource_id: map_value(item, :repository_resource_id),
      goal_revision: map_value(item, :admitted_revision),
      baseline: baseline_projection(item),
      integration: map_value(item, :integration, false) == true,
      change_target: sanitize(map_value(item, :change_target), @context_limit),
      accepted?: not is_nil(receipt),
      accepted_outcome: receipt && outcome_projection(receipt),
      execution: item_execution,
      blockers: if(node, do: node.blockers, else: [])
    }
    |> maybe_put(:current_revision, current_revision)
  end

  defp baseline_projection(item) do
    cond do
      is_map(map_value(item, :baseline_subject)) ->
        %{kind: "subject", subject: map_value(item, :baseline_subject)}

      is_binary(map_value(item, :baseline_dependency_id)) ->
        %{kind: "dependency", work_item_id: map_value(item, :baseline_dependency_id)}

      true ->
        nil
    end
  end

  defp historical_work_item_projection(item) do
    %{
      id: map_value(item, :id),
      number: map_value(item, :number),
      title: map_value(item, :title),
      status: map_value(item, :status),
      board_status: map_value(item, :status),
      required?: map_value(item, :required, true),
      admitted_revision: map_value(item, :admitted_revision),
      orchestration_task_id: map_value(item, :orchestration_task_id),
      inserted_at: map_value(item, :inserted_at),
      updated_at: map_value(item, :updated_at),
      historical?: true
    }
  end

  defp dependency_projection(dependency) do
    %{
      id: map_value(dependency, :id),
      goal_id: map_value(dependency, :goal_id),
      work_item_id: map_value(dependency, :work_item_id),
      depends_on_id: map_value(dependency, :depends_on_id),
      inserted_at: map_value(dependency, :inserted_at),
      historical?: true
    }
  end

  defp graph_projection(work_items, dependencies, accepted, current_revision) do
    nodes =
      Enum.map(work_items, fn item ->
        id = map_value(item, :id)

        %{
          id: id,
          number: map_value(item, :number),
          title: map_value(item, :title),
          required?: map_value(item, :required, true),
          board_status: map_value(item, :status),
          accepted?: Map.has_key?(accepted, id),
          blockers: []
        }
      end)

    edges =
      dependencies
      |> Enum.map(fn dependency ->
        work_item_id = map_value(dependency, :work_item_id)
        depends_on_id = map_value(dependency, :depends_on_id)
        satisfied? = Map.has_key?(accepted, depends_on_id)

        %{
          id: map_value(dependency, :id),
          work_item_id: work_item_id,
          depends_on_id: depends_on_id,
          from: work_item_id,
          to: depends_on_id,
          satisfied?: satisfied?,
          blocker: if(satisfied?, do: nil, else: "waiting_dependency"),
          revision: current_revision
        }
      end)

    blocked_by_dependency =
      edges
      |> Enum.reject(& &1.satisfied?)
      |> Enum.group_by(& &1.work_item_id)

    nodes =
      Enum.map(nodes, fn node ->
        blockers =
          if Map.has_key?(blocked_by_dependency, node.id), do: ["waiting_dependency"], else: []

        %{node | blockers: blockers}
      end)

    %{nodes: nodes, edges: edges, cycle_detected?: cycle?(edges)}
  end

  defp cycle?(edges) do
    adjacency = Enum.group_by(edges, & &1.from, & &1.to)

    Enum.any?(Map.keys(adjacency), fn node -> reachable?(node, node, adjacency, MapSet.new()) end)
  end

  defp reachable?(start, node, adjacency, seen) do
    node in MapSet.to_list(seen) or
      Enum.any?(Map.get(adjacency, node, []), fn next ->
        next == start or reachable?(start, next, adjacency, MapSet.put(seen, node))
      end)
  end

  defp current_revision_work_items(work_items, current_revision) do
    Enum.filter(work_items, &(map_value(&1, :admitted_revision) == current_revision))
  end

  defp current_revision_dependencies(dependencies, current_work_item_ids) do
    Enum.filter(dependencies, &current_dependency?(&1, current_work_item_ids))
  end

  defp current_dependency?(dependency, current_work_item_ids) do
    MapSet.member?(current_work_item_ids, map_value(dependency, :work_item_id)) and
      MapSet.member?(current_work_item_ids, map_value(dependency, :depends_on_id))
  end

  defp current_revision_outcomes(outcomes, current_revision, current_work_item_ids) do
    Enum.filter(outcomes, &current_outcome?(&1, current_revision, current_work_item_ids))
  end

  defp current_outcome?(outcome, current_revision, current_work_item_ids) do
    map_value(outcome, :goal_revision) == current_revision and
      MapSet.member?(current_work_item_ids, map_value(outcome, :work_item_id))
  end

  defp current_goal_tasks(tasks, current_work_item_ids, current_revision) do
    Enum.filter(tasks, fn task ->
      MapSet.member?(current_work_item_ids, map_value(task, :work_item_id)) or
        planning_task?(task, current_revision)
    end)
  end

  defp planning_task?(task, current_revision) do
    map_value(task, :goal_revision) == current_revision and map_value(task, :purpose) == "plan" and
      is_nil(map_value(task, :work_item_id)) and is_nil(map_value(task, :validation_of_task_id))
  end

  defp accepted_outcomes(outcomes) do
    outcomes
    |> Enum.filter(&(map_value(&1, :disposition) == "accepted"))
    |> Enum.sort_by(fn outcome -> {map_value(outcome, :inserted_at), map_value(outcome, :id)} end)
    |> Map.new(fn outcome -> {map_value(outcome, :work_item_id), outcome} end)
  end

  defp blockers(
         goal,
         revision,
         work_items,
         dependencies,
         outcomes,
         decisions,
         tasks,
         runs,
         settled_receipts,
         evidence,
         reservations,
         usage,
         runtimes,
         external_waits,
         automatic_admission_blocked_events,
         automatic_reconciliation_deferred_events,
         current_revision,
         accepted,
         all_tasks,
         all_runs
       ) do
    []
    |> add_blocker(waiting_decision_blocker(decisions, current_revision))
    |> add_blocker(
      waiting_dependency_blocker(work_items, dependencies, accepted, current_revision)
    )
    |> add_blocker(
      automatic_baseline_missing_blocker(
        revision,
        work_items,
        accepted,
        tasks,
        current_revision
      )
    )
    |> add_blocker(
      automatic_baseline_dependency_blocker(
        revision,
        work_items,
        accepted,
        tasks,
        current_revision
      )
    )
    |> add_blocker(validation_failed_blocker(outcomes, current_revision, accepted))
    |> add_blocker(stale_context_blocker(tasks, current_revision))
    |> add_blocker(runtime_unavailable_blocker(tasks, runs, runtimes, current_revision))
    |> add_blocker(
      automatic_admission_blocked_blocker(
        goal,
        automatic_admission_blocked_events,
        tasks,
        work_items,
        accepted,
        current_revision
      )
    )
    |> add_blocker(
      automatic_reconciliation_deferred_blocker(
        goal,
        automatic_reconciliation_deferred_events,
        current_revision
      )
    )
    |> add_blocker(unsupported_external_check_blocker(external_waits, accepted, current_revision))
    |> add_blocker(
      waiting_external_blocker(work_items, settled_receipts, accepted, external_waits)
    )
    |> add_blocker(
      integration_outcome_required_blocker(
        goal,
        work_items,
        accepted,
        decisions,
        tasks,
        current_revision
      )
    )
    |> add_blocker(
      final_acceptance_required_blocker(
        goal,
        revision,
        work_items,
        dependencies,
        accepted,
        decisions,
        all_tasks,
        all_runs,
        evidence,
        current_revision
      )
    )
    |> add_blocker(budget_blocker(revision, reservations, usage, all_tasks, all_runs))
    |> Enum.sort_by(& &1.reason)
  end

  defp waiting_decision_blocker(decisions, current_revision) do
    rows =
      Enum.filter(
        decisions,
        &(map_value(&1, :goal_revision) == current_revision and map_value(&1, :state) == "open")
      )

    if rows == [] do
      nil
    else
      blocker("waiting_decision", rows, :id, fn row ->
        %{
          decision_id: map_value(row, :id),
          kind: map_value(row, :kind),
          work_item_id: map_value(row, :work_item_id)
        }
      end)
    end
  end

  defp waiting_dependency_blocker(work_items, dependencies, accepted, current_revision) do
    required_rows =
      dependencies
      |> Enum.filter(
        &MapSet.member?(
          completion_relevant_item_ids(work_items, dependencies),
          map_value(&1, :work_item_id)
        )
      )
      |> Enum.reject(&Map.has_key?(accepted, map_value(&1, :depends_on_id)))

    integration_rows = waiting_integration_dependency_rows(work_items, dependencies, accepted)

    rows = Enum.uniq_by(required_rows ++ integration_rows, &dependency_key/1)

    if rows == [] do
      nil
    else
      blocker("waiting_dependency", rows, :work_item_id, fn row ->
        %{
          work_item_id: map_value(row, :work_item_id),
          depends_on_id: map_value(row, :depends_on_id),
          revision: current_revision
        }
      end)
    end
  end

  defp waiting_integration_dependency_rows(work_items, dependencies, accepted) do
    candidates = accepted_integration_outcomes(accepted, work_items)

    if candidates != [] and
         not Enum.any?(candidates, fn candidate ->
           integration_item_dependencies_satisfied?(
             work_items,
             dependencies,
             accepted,
             candidate.work_item_id
           )
         end) do
      candidates
      |> Enum.flat_map(fn candidate ->
        relevant_ids =
          completion_relevant_item_ids(work_items, dependencies, candidate.work_item_id)

        Enum.filter(
          dependencies,
          &(MapSet.member?(relevant_ids, map_value(&1, :work_item_id)) and
              not Map.has_key?(accepted, map_value(&1, :depends_on_id)))
        )
      end)
      |> Enum.uniq_by(&dependency_key/1)
    else
      []
    end
  end

  defp dependency_key(row) do
    {map_value(row, :work_item_id), map_value(row, :depends_on_id)}
  end

  defp automatic_baseline_missing_blocker(
         revision,
         work_items,
         accepted,
         tasks,
         current_revision
       ) do
    if automatic_execution?(revision) do
      rows =
        work_items
        |> Enum.filter(&unstarted_current_item?(&1, accepted, tasks, current_revision))
        |> Enum.filter(fn item ->
          is_nil(map_value(item, :baseline_subject)) and
            is_nil(map_value(item, :baseline_dependency_id))
        end)

      if rows == [] do
        nil
      else
        blocker("automatic_baseline_missing", rows, :id, fn row ->
          %{
            work_item_id: map_value(row, :id),
            reason: "missing_explicit_baseline"
          }
        end)
      end
    end
  end

  defp automatic_baseline_dependency_blocker(
         revision,
         work_items,
         accepted,
         tasks,
         current_revision
       ) do
    if automatic_execution?(revision) do
      rows =
        work_items
        |> Enum.filter(&unstarted_current_item?(&1, accepted, tasks, current_revision))
        |> Enum.filter(fn item ->
          dependency_id = map_value(item, :baseline_dependency_id)
          not is_nil(dependency_id) and not Map.has_key?(accepted, dependency_id)
        end)

      if rows == [] do
        nil
      else
        blocker("automatic_baseline_dependency", rows, :id, fn row ->
          %{
            work_item_id: map_value(row, :id),
            baseline_dependency_id: map_value(row, :baseline_dependency_id),
            reason: "baseline_dependency_not_accepted"
          }
        end)
      end
    end
  end

  defp automatic_execution?(revision) do
    value(map_value(revision, :execution_policy, %{}), :automatic_execution, false) == true
  end

  defp unstarted_current_item?(item, accepted, tasks, current_revision) do
    item_id = map_value(item, :id)

    not Map.has_key?(accepted, item_id) and
      not Enum.any?(
        tasks,
        &(map_value(&1, :work_item_id) == item_id and
            map_value(&1, :goal_revision) == current_revision)
      )
  end

  defp validation_failed_blocker(outcomes, current_revision, accepted) do
    rows =
      Enum.filter(
        outcomes,
        &(map_value(&1, :goal_revision) == current_revision and
            map_value(&1, :disposition) == "rejected" and
            not Map.has_key?(accepted, map_value(&1, :work_item_id)))
      )

    if rows == [] do
      nil
    else
      blocker("validation_failed", rows, :work_item_id, fn row ->
        %{
          outcome_id: map_value(row, :id),
          work_item_id: map_value(row, :work_item_id),
          reason: present(map_value(row, :reason))
        }
      end)
    end
  end

  defp stale_context_blocker(tasks, current_revision) do
    rows =
      Enum.filter(tasks, fn task ->
        map_value(task, :state) in @nonterminal_task_states and
          (map_value(task, :goal_revision) not in [nil, current_revision] or
             (goal_task?(task) and is_nil(map_value(task, :context_snapshot_id))))
      end)

    if rows == [] do
      nil
    else
      blocker("stale_context", rows, :id, fn row ->
        %{
          task_id: map_value(row, :id),
          task_revision: map_value(row, :goal_revision),
          revision: current_revision
        }
      end)
    end
  end

  defp runtime_unavailable_blocker(tasks, runs, runtimes, current_revision) do
    runtimes_by_id = Map.new(runtimes, &{map_value(&1, :id), &1})

    runs_by_task_generation =
      Map.new(runs, fn run ->
        {{map_value(run, :task_id), map_value(run, :generation)}, run}
      end)

    rows =
      tasks
      |> Enum.filter(fn task ->
        map_value(task, :goal_revision) == current_revision and
          map_value(task, :state) in @runtime_bound_states
      end)
      |> Enum.flat_map(fn task ->
        run =
          Map.get(
            runs_by_task_generation,
            {map_value(task, :id), map_value(task, :current_generation)}
          )

        runtime = run && Map.get(runtimes_by_id, map_value(run, :runtime_id))

        if current_runtime_run?(task, run) and is_map(runtime) and
             map_value(runtime, :status) not in ["online", nil],
           do: [run],
           else: []
      end)

    if rows == [] do
      nil
    else
      blocker("runtime_unavailable", rows, :id, fn row ->
        %{run_id: map_value(row, :id), runtime_id: map_value(row, :runtime_id)}
      end)
    end
  end

  defp current_runtime_run?(task, run) when is_map(task) and is_map(run) do
    map_value(run, :generation) == map_value(task, :current_generation) and
      map_value(run, :state) in @runtime_bound_states
  end

  defp current_runtime_run?(_task, _run), do: false

  defp automatic_admission_blocked_blocker(
         goal,
         events,
         tasks,
         work_items,
         accepted,
         current_revision
       ) do
    rows =
      Enum.filter(
        events,
        &automatic_admission_blocked_event_relevant?(
          &1,
          goal,
          tasks,
          work_items,
          accepted,
          current_revision
        )
      )

    if rows == [] do
      nil
    else
      blocker("automatic_admission_blocked", rows, :id, fn event ->
        payload = map_value(event, :payload, %{})
        response = map_value(event, :response, %{})

        %{
          event_id: map_value(event, :id),
          work_item_id: value(payload, :work_item_id),
          purpose: value(payload, :purpose),
          candidate_identity: value(payload, :candidate_identity),
          source_identity: value(payload, :source_identity),
          reason: value(response, :reason, value(payload, :reason))
        }
      end)
    end
  end

  defp automatic_admission_blocked_event_relevant?(
         event,
         goal,
         tasks,
         work_items,
         accepted,
         current_revision
       ) do
    payload = map_value(event, :payload, %{})
    work_item_id = value(payload, :work_item_id)
    admission_key = value(payload, :admission_key)

    base? =
      map_value(goal, :state) not in @terminal_goal_states and
        map_value(event, :revision) == current_revision and
        map_value(event, :kind) == "automatic_admission_blocked"

    if is_nil(work_item_id) do
      base? and Enum.any?(work_items, &(not Map.has_key?(accepted, map_value(&1, :id))))
    else
      base? and
        not Map.has_key?(accepted, work_item_id) and
        not Enum.any?(tasks, fn task ->
          automatic_admission_blocker_replaced?(
            task,
            event,
            work_item_id,
            admission_key,
            current_revision
          )
        end)
    end
  end

  defp automatic_admission_blocker_replaced?(
         task,
         event,
         work_item_id,
         admission_key,
         current_revision
       ) do
    map_value(task, :goal_revision) == current_revision and
      map_value(task, :work_item_id) == work_item_id and
      (map_value(task, :admission_key) == admission_key or
         (map_value(task, :purpose) == value(map_value(event, :payload, %{}), :purpose) and
            inserted_after?(map_value(task, :inserted_at), map_value(event, :inserted_at))))
  end

  defp inserted_after?(%DateTime{} = task_inserted_at, %DateTime{} = event_inserted_at),
    do: DateTime.compare(task_inserted_at, event_inserted_at) != :lt

  defp inserted_after?(%NaiveDateTime{} = task_inserted_at, %NaiveDateTime{} = event_inserted_at),
    do: NaiveDateTime.compare(task_inserted_at, event_inserted_at) != :lt

  defp inserted_after?(_task_inserted_at, _event_inserted_at), do: false

  defp automatic_reconciliation_deferred_blocker(goal, events, current_revision) do
    event =
      events
      |> Enum.filter(fn event ->
        map_value(goal, :state) == "active" and not is_nil(map_value(goal, :next_wake_at)) and
          map_value(event, :revision) == current_revision and
          map_value(event, :kind) == "automatic_reconciliation_deferred"
      end)
      |> List.last()

    if is_nil(event) do
      nil
    else
      payload = map_value(event, :payload, %{})
      response = map_value(event, :response, %{})

      blocker("automatic_reconciliation_deferred", [event], :id, fn _event ->
        %{
          event_id: map_value(event, :id),
          reason: value(response, :reason, value(payload, :reason)),
          next_wake_at: map_value(goal, :next_wake_at)
        }
      end)
    end
  end

  defp unsupported_external_check_blocker(external_waits, accepted, current_revision) do
    rows =
      Enum.filter(external_waits, fn wait ->
        map_value(wait, :goal_revision) == current_revision and
          map_value(wait, :state) == "unsupported" and
          not Map.has_key?(accepted, map_value(wait, :work_item_id))
      end)

    if rows == [] do
      nil
    else
      blocker("unsupported_external_check", rows, :id, fn row ->
        %{
          external_wait_id: map_value(row, :id),
          work_item_id: map_value(row, :work_item_id),
          task_id: map_value(row, :task_id),
          run_id: map_value(row, :run_id),
          generation: map_value(row, :run_generation),
          result_id: map_value(row, :result_id),
          resource_id: map_value(row, :resource_id),
          external_ref: map_value(row, :external_ref),
          state: "unsupported"
        }
      end)
    end
  end

  defp waiting_external_blocker(work_items, settled_receipts, accepted, external_waits) do
    unsupported_work_item_ids =
      external_waits
      |> Enum.filter(&(map_value(&1, :state) == "unsupported"))
      |> Enum.map(&map_value(&1, :work_item_id))
      |> MapSet.new()

    rows =
      Enum.filter(work_items, fn item ->
        external_available = map_value(item, :external_available, true)

        not Map.has_key?(accepted, map_value(item, :id)) and
          not MapSet.member?(unsupported_work_item_ids, map_value(item, :id)) and
          (external_available == false or
             Enum.any?(settled_receipts, fn receipt ->
               receipt.work_item_id == map_value(item, :id) and receipt.settlement == "blocked" and
                 external_blocker?(receipt.blocker)
             end))
      end)

    if rows == [] do
      nil
    else
      blocker("waiting_external", rows, :id, fn row ->
        %{work_item_id: map_value(row, :id), external_id: map_value(row, :external_id)}
      end)
    end
  end

  defp budget_blocker(revision, reservations, usage, tasks, runs) do
    policy = map_value(revision, :execution_policy, %{})
    limit = value(policy, :budget_limit_microusd)
    strict? = value(policy, :budget_mode) == "strict"

    if is_integer(limit) do
      usage_summary = effective_usage_summary(usage, tasks, runs)

      held =
        Enum.reduce(reservations, 0, fn reservation, held_total ->
          state = map_value(reservation, :state)
          reserved = map_value(reservation, :reserved_microusd, 0) || 0
          if state in ["held", "unknown"], do: held_total + reserved, else: held_total
        end)

      spent = usage_summary.known_cost_microusd

      committed =
        spent +
          Enum.reduce(reservations, 0, fn reservation, total ->
            task_id = map_value(reservation, :task_id)
            state = map_value(reservation, :state)
            reserved = map_value(reservation, :reserved_microusd, 0) || 0
            known_for_task = Map.get(usage_summary.known_by_task, task_id, 0)

            liability? =
              state in ["held", "unknown"] or
                (state == "settled" and MapSet.member?(usage_summary.unknown_task_ids, task_id))

            if liability?, do: total + max(reserved - known_for_task, 0), else: total
          end)

      unknown_reservations = Enum.count(reservations, &(map_value(&1, :state) == "unknown"))
      unknown_usage? = usage_summary.unknown_usage_count > 0

      if committed >= limit or (strict? and (unknown_reservations > 0 or unknown_usage?)) do
        %{
          reason: "budget_blocked",
          count: 1,
          details: [
            %{
              limit_microusd: microusd_wire_value(limit),
              reserved_microusd: microusd_wire_value(held),
              spent_microusd: microusd_wire_value(spent),
              committed_microusd: microusd_wire_value(committed),
              unknown_reservation_count: unknown_reservations,
              unknown_usage?: unknown_usage?
            }
          ]
        }
      end
    end
  end

  defp effective_usage_summary(usage, tasks, runs) do
    task_id_by_run = Map.new(runs, &{map_value(&1, :id), map_value(&1, :task_id)})

    goal_task_ids =
      tasks |> Enum.filter(&goal_task?/1) |> Enum.map(&map_value(&1, :id)) |> MapSet.new()

    leaves =
      usage
      |> latest_usage()
      |> Enum.flat_map(fn row ->
        with task_id when is_binary(task_id) <- Map.get(task_id_by_run, map_value(row, :run_id)),
             true <- MapSet.member?(goal_task_ids, task_id) do
          [%{task_id: task_id, row: row}]
        else
          _ -> []
        end
      end)

    leaves_by_run = MapSet.new(leaves, &map_value(&1.row, :run_id))

    missing_terminal_task_ids =
      runs
      |> Enum.filter(fn run ->
        task_id = map_value(run, :task_id)

        MapSet.member?(goal_task_ids, task_id) and
          map_value(run, :state) in @accounting_terminal_run_states and
          not MapSet.member?(leaves_by_run, map_value(run, :id))
      end)
      |> Enum.map(&map_value(&1, :task_id))

    known_by_task =
      Enum.reduce(leaves, %{}, fn %{task_id: task_id, row: row}, totals ->
        case map_value(row, :cost_microusd) do
          cost when is_integer(cost) -> Map.update(totals, task_id, cost, &(&1 + cost))
          _ -> totals
        end
      end)

    known_cost_microusd = known_by_task |> Map.values() |> Enum.sum()

    unknown_leaf_task_ids =
      leaves
      |> Enum.filter(&is_nil(map_value(&1.row, :cost_microusd)))
      |> Enum.map(& &1.task_id)

    unknown_leaf_count = Enum.count(leaves, &is_nil(map_value(&1.row, :cost_microusd)))

    %{
      known_by_task: known_by_task,
      known_cost_microusd: known_cost_microusd,
      unknown_usage_count: unknown_leaf_count + length(missing_terminal_task_ids),
      unknown_task_ids: MapSet.new(unknown_leaf_task_ids ++ missing_terminal_task_ids),
      usage_leaf_count: length(leaves)
    }
  end

  defp blocker(reason, rows, key, detail_fun) do
    %{
      reason: reason,
      count: length(rows),
      ids: rows |> Enum.map(&map_value(&1, key)) |> Enum.reject(&is_nil/1) |> Enum.uniq(),
      details: Enum.map(rows, detail_fun)
    }
  end

  defp add_blocker(blockers, nil), do: blockers
  defp add_blocker(blockers, blocker), do: [blocker | blockers]

  # Settlement receipts gate attention and receipt-derived external blockers,
  # but never directly become completion blockers. Currentness is tied to the
  # WorkItem's latest admitted Task, not event order: a terminal receipt may
  # arrive after a replacement admission is durable.
  defp current_task_settlements(
         goal,
         work_items,
         outcomes,
         tasks,
         runs,
         events,
         current_revision
       ) do
    if map_value(goal, :state) in @terminal_goal_states do
      []
    else
      work_items_by_id = Map.new(work_items, &{map_value(&1, :id), &1})
      tasks_by_id = Map.new(tasks, &{map_value(&1, :id), &1})
      runs_by_id = Map.new(runs, &{map_value(&1, :id), &1})
      accepted = accepted_outcomes(outcomes)

      events
      |> Enum.sort_by(fn event -> {map_value(event, :sequence), map_value(event, :id)} end)
      |> Enum.reduce(%{}, fn event, attention ->
        case current_task_settlement_entry(
               event,
               map_value(goal, :id),
               current_revision,
               work_items_by_id,
               tasks_by_id,
               runs_by_id,
               accepted
             ) do
          nil -> attention
          entry -> Map.put_new(attention, {entry.task_id, entry.run_id, entry.generation}, entry)
        end
      end)
      |> Map.values()
      |> Enum.sort_by(&{&1.event_sequence, &1.event_id})
    end
  end

  defp settlement_attention(goal, settled_receipts, decisions, current_revision) do
    if map_value(goal, :state) in @terminal_goal_states do
      []
    else
      open_decision_ids =
        decisions
        |> Enum.filter(
          &(&1 |> map_value(:goal_revision) == current_revision and
              map_value(&1, :state) == "open")
        )
        |> MapSet.new(&map_value(&1, :id))

      Enum.filter(settled_receipts, fn receipt ->
        settlement_attention?(
          receipt.settlement,
          receipt.blocker,
          open_decision_ids,
          receipt.decision_id
        )
      end)
    end
  end

  defp current_task_settlement_entry(
         event,
         goal_id,
         current_revision,
         work_items_by_id,
         tasks_by_id,
         runs_by_id,
         accepted
       ) do
    payload = map_value(event, :payload)
    response = map_value(event, :response)

    with "task_settled" <- map_value(event, :kind),
         ^current_revision <- map_value(event, :revision),
         {:ok, ^current_revision} <- matching_receipt_field(payload, response, :goal_revision),
         {:ok, task_id} <- matching_receipt_field(payload, response, :task_id),
         {:ok, run_id} <- matching_receipt_field(payload, response, :run_id),
         {:ok, generation} <- matching_receipt_field(payload, response, :generation),
         %{} = task <- Map.get(tasks_by_id, task_id),
         %{} = run <- Map.get(runs_by_id, run_id),
         ^goal_id <- map_value(task, :goal_id),
         ^current_revision <- map_value(task, :goal_revision),
         ^generation <- map_value(task, :current_generation),
         ^task_id <- map_value(run, :task_id),
         ^generation <- map_value(run, :generation),
         true <- map_value(payload, :state) == map_value(run, :state),
         true <- map_value(task, :state) in @terminal_task_states,
         true <- map_value(run, :state) in @terminal_task_states,
         settlement when is_binary(settlement) <- value(response, :settlement) do
      task_settlement_entry(
        event,
        response,
        task,
        run,
        settlement,
        current_revision,
        work_items_by_id,
        accepted,
        tasks_by_id
      )
    else
      _ -> nil
    end
  end

  defp matching_receipt_field(payload, response, key) when is_map(payload) and is_map(response) do
    case value(payload, key) do
      nil ->
        :error

      field ->
        if value(response, key) == field, do: {:ok, field}, else: :error
    end
  end

  defp matching_receipt_field(_payload, _response, _key), do: :error

  defp task_settlement_entry(
         event,
         response,
         task,
         run,
         settlement,
         current_revision,
         work_items_by_id,
         accepted,
         tasks_by_id
       ) do
    if is_nil(map_value(task, :work_item_id)) do
      if planning_task?(task, current_revision) and
           latest_planning_task_id(tasks_by_id, current_revision) == map_value(task, :id) do
        settlement_entry(event, response, task, run, settlement, current_revision, nil, false)
      end
    else
      work_item_task_settlement_entry(
        event,
        response,
        task,
        run,
        settlement,
        current_revision,
        work_items_by_id,
        accepted
      )
    end
  end

  defp work_item_task_settlement_entry(
         event,
         response,
         task,
         run,
         settlement,
         current_revision,
         work_items_by_id,
         accepted
       ) do
    work_item_id = map_value(task, :work_item_id)
    task_id = map_value(task, :id)

    with %{} = item <- Map.get(work_items_by_id, work_item_id),
         true <- map_value(item, :orchestration_task_id) == task_id,
         false <- Map.has_key?(accepted, work_item_id) do
      settlement_entry(event, response, task, run, settlement, current_revision, item, true)
    else
      _ -> nil
    end
  end

  defp latest_planning_task_id(tasks_by_id, current_revision) do
    tasks_by_id
    |> Map.values()
    |> Enum.filter(&planning_task?(&1, current_revision))
    |> Enum.sort_by(fn task -> {map_value(task, :inserted_at), map_value(task, :id)} end)
    |> List.last()
    |> map_value(:id)
  end

  defp settlement_entry(
         event,
         response,
         task,
         run,
         settlement,
         current_revision,
         item,
         work_item?
       ) do
    %{
      event_id: map_value(event, :id),
      event_sequence: map_value(event, :sequence),
      goal_revision: current_revision,
      work_item_id: if(work_item?, do: map_value(task, :work_item_id), else: nil),
      task_id: map_value(task, :id),
      run_id: map_value(run, :id),
      generation: map_value(run, :generation),
      required?: if(work_item?, do: map_value(item, :required, true), else: false),
      settlement: settlement,
      reason: present(value(response, :reason)),
      result_id: value(response, :result_id),
      result_kind: value(response, :result_kind),
      decision_id: value(response, :decision_id),
      purpose: map_value(task, :purpose),
      blocker: blocker_projection(value(response, :blocker)),
      proposed_next_action: sanitize(value(response, :proposed_next_action), 4_000),
      proposed_next_action_status: value(response, :proposed_next_action_status),
      subject_hash: digest(value(response, :subject_hash)),
      evidence_refs: list_value(response, :evidence_refs)
    }
  end

  defp settlement_attention?(settlement, _blocker, _open_decision_ids, _decision_id)
       when settlement in @settlement_attention_states,
       do: true

  defp settlement_attention?("blocked", blocker, open_decision_ids, _decision_id),
    do: disposition_required_blocker?(blocker, open_decision_ids)

  defp settlement_attention?(_settlement, _blocker, _open_decision_ids, _decision_id), do: false

  defp disposition_required_blocker?(blocker, open_decision_ids) when is_map(blocker) do
    case value(blocker, :kind) do
      "environment" -> true
      "decision" -> MapSet.member?(open_decision_ids, value(blocker, :decision_id))
      _ -> false
    end
  end

  defp disposition_required_blocker?(_blocker, _open_decision_ids), do: false

  defp final_acceptance_required_blocker(
         goal,
         revision,
         work_items,
         dependencies,
         accepted,
         decisions,
         tasks,
         runs,
         evidence,
         current_revision
       ) do
    if final_acceptance_ready?(
         goal,
         revision,
         work_items,
         dependencies,
         accepted,
         decisions,
         tasks,
         runs,
         evidence,
         current_revision
       ) do
      nil
    else
      completion_readiness_blocker(
        goal,
        revision,
        work_items,
        dependencies,
        accepted,
        decisions,
        tasks,
        runs,
        evidence,
        current_revision
      )
    end
  end

  # Completion needs one eligible integration subject. A subject that is still
  # missing required check/artifact/review evidence is not an authority problem;
  # render that separately from a missing operator completion Decision.
  defp completion_readiness_blocker(
         goal,
         revision,
         work_items,
         dependencies,
         accepted,
         decisions,
         tasks,
         runs,
         evidence,
         current_revision
       ) do
    authority = final_acceptance_authority(revision)

    candidates =
      completion_readiness_candidates(
        goal,
        work_items,
        dependencies,
        accepted,
        decisions,
        tasks,
        current_revision
      )

    candidates_with_unmet_predicates =
      Enum.map(candidates, fn candidate ->
        {candidate,
         unmet_required_goal_evidence_predicates(
           map_value(revision, :acceptance_contract, %{}),
           candidate.subject_hash,
           evidence,
           tasks,
           runs,
           current_revision
         )}
      end)

    candidates_with_all_evidence =
      Enum.filter(candidates_with_unmet_predicates, fn {candidate, unmet_predicates} ->
        unmet_predicates == [] and
          required_goal_predicates_satisfied?(
            map_value(revision, :acceptance_contract, %{}),
            Atom.to_string(authority),
            candidate.subject_hash,
            evidence,
            tasks,
            runs,
            current_revision
          )
      end)

    case authority do
      :operator when candidates_with_all_evidence != [] ->
        resolved_subjects =
          MapSet.new(resolved_completion_subjects(goal, decisions, current_revision))

        candidates_with_all_evidence
        |> Enum.map(&elem(&1, 0))
        |> Enum.reject(&MapSet.member?(resolved_subjects, &1.subject_hash))
        |> final_acceptance_required_blocker_for_candidates()

      authority when authority in [:operator, :deterministic] ->
        candidates_with_unmet_predicates
        |> Enum.filter(fn {_candidate, unmet_predicates} -> unmet_predicates != [] end)
        |> required_predicates_unmet_blocker()

      :invalid ->
        nil
    end
  end

  defp completion_readiness_candidates(
         goal,
         work_items,
         dependencies,
         accepted,
         decisions,
         tasks,
         current_revision
       ) do
    if completion_readiness_gate?(goal, work_items, accepted, decisions, tasks, current_revision) do
      accepted_integration_outcomes(accepted, work_items)
      |> Enum.filter(fn candidate ->
        valid_digest?(candidate.subject_hash) and
          integration_item_dependencies_satisfied?(
            work_items,
            dependencies,
            accepted,
            candidate.work_item_id
          )
      end)
    else
      []
    end
  end

  defp integration_outcome_required_blocker(
         goal,
         work_items,
         accepted,
         decisions,
         tasks,
         current_revision
       ) do
    integration_items = Enum.filter(work_items, &(map_value(&1, :integration, false) == true))

    if completion_readiness_gate?(goal, work_items, accepted, decisions, tasks, current_revision) and
         accepted_integration_outcomes(accepted, work_items) == [] do
      %{
        reason: "integration_outcome_required",
        count: max(length(integration_items), 1),
        ids: Enum.map(integration_items, &map_value(&1, :id)),
        details:
          case integration_items do
            [] ->
              [%{reason: "no_integration_work_item"}]

            _ ->
              Enum.map(integration_items, fn item ->
                %{work_item_id: map_value(item, :id), reason: "accepted_outcome_required"}
              end)
          end
      }
    end
  end

  defp completion_readiness_gate?(goal, work_items, accepted, decisions, tasks, current_revision) do
    required_items = Enum.filter(work_items, &map_value(&1, :required, true))

    map_value(goal, :state) in ["active", "paused"] and
      Enum.all?(required_items, &Map.has_key?(accepted, map_value(&1, :id))) and
      not Enum.any?(tasks, &(map_value(&1, :state) in @nonterminal_task_states)) and
      not Enum.any?(
        decisions,
        &(map_value(&1, :goal_revision) == current_revision and map_value(&1, :state) == "open")
      )
  end

  defp final_acceptance_required_blocker_for_candidates([]), do: nil

  defp final_acceptance_required_blocker_for_candidates(candidates) do
    %{
      reason: "final_acceptance_required",
      count: length(candidates),
      ids: Enum.map(candidates, & &1.work_item_id),
      details:
        Enum.map(candidates, fn candidate ->
          %{
            work_item_id: candidate.work_item_id,
            subject_hash: digest(candidate.subject_hash),
            final_acceptance: "operator"
          }
        end)
    }
  end

  defp required_predicates_unmet_blocker([]), do: nil

  defp required_predicates_unmet_blocker(candidates_with_unmet_predicates) do
    %{
      reason: "required_predicates_unmet",
      count: length(candidates_with_unmet_predicates),
      ids:
        Enum.map(candidates_with_unmet_predicates, fn {candidate, _} -> candidate.work_item_id end),
      details:
        Enum.map(candidates_with_unmet_predicates, fn {candidate, unmet_predicates} ->
          %{
            work_item_id: candidate.work_item_id,
            subject_hash: digest(candidate.subject_hash),
            predicate_ids: Enum.map(unmet_predicates, &value(&1, :id)),
            predicate_kinds: Enum.map(unmet_predicates, &value(&1, :kind))
          }
        end)
    }
  end

  defp completion_projection(
         goal,
         revision,
         work_items,
         dependencies,
         outcomes,
         decisions,
         tasks,
         runs,
         evidence,
         current_revision,
         accepted,
         _blockers
       ) do
    required_items = Enum.filter(work_items, &map_value(&1, :required, true))
    accepted_required = Enum.count(required_items, &Map.has_key?(accepted, map_value(&1, :id)))

    nonterminal_tasks = Enum.filter(tasks, &(map_value(&1, :state) in @nonterminal_task_states))

    final_acceptance_ready? =
      final_acceptance_ready?(
        goal,
        revision,
        work_items,
        dependencies,
        accepted,
        decisions,
        tasks,
        runs,
        evidence,
        current_revision
      )

    %{
      current_revision?: current_revision == map_value(revision, :revision),
      required_items: length(required_items),
      accepted_required_items: accepted_required,
      all_required_accepted?: accepted_required == length(required_items),
      no_open_decisions?:
        not Enum.any?(
          decisions,
          &(map_value(&1, :goal_revision) == current_revision and map_value(&1, :state) == "open")
        ),
      no_nonterminal_tasks?: nonterminal_tasks == [],
      # Blockers remain visible, but optional rejected/unsupported work and
      # budget history must not hide an action that the authoritative
      # completion predicate would accept.
      ready?:
        map_value(goal, :state) in ["active", "paused"] and
          accepted_required == length(required_items) and
          not Enum.any?(
            decisions,
            &(map_value(&1, :goal_revision) == current_revision and
                map_value(&1, :state) == "open")
          ) and
          nonterminal_tasks == [] and
          final_acceptance_ready?,
      accepted_outcomes: map_size(accepted),
      rejected_outcomes:
        Enum.count(
          outcomes,
          &(map_value(&1, :goal_revision) == current_revision and
              map_value(&1, :disposition) == "rejected")
        )
    }
  end

  defp final_acceptance_ready?(
         goal,
         revision,
         work_items,
         dependencies,
         accepted,
         decisions,
         tasks,
         runs,
         evidence,
         current_revision
       ) do
    case final_acceptance_authority(revision) do
      authority when authority in [:operator, :deterministic] ->
        candidate_subjects =
          case authority do
            :operator -> resolved_completion_subjects(goal, decisions, current_revision)
            :deterministic -> accepted |> Map.values() |> Enum.map(&map_value(&1, :subject_hash))
          end

        Enum.any?(candidate_subjects, fn subject_hash ->
          valid_digest?(subject_hash) and
            integration_candidate_dependencies_satisfied?(
              work_items,
              dependencies,
              accepted,
              subject_hash
            ) and
            required_goal_predicates_satisfied?(
              map_value(revision, :acceptance_contract, %{}),
              Atom.to_string(authority),
              subject_hash,
              evidence,
              tasks,
              runs,
              current_revision
            )
        end)

      :invalid ->
        false
    end
  end

  defp final_acceptance_authority(revision) do
    case ContractValidation.final_acceptance_authority(
           map_value(revision, :authority_policy, %{}),
           map_value(revision, :execution_policy, %{}),
           map_value(revision, :acceptance_contract, %{})
         ) do
      {:ok, authority} -> authority
      {:error, _reason} -> :invalid
    end
  end

  defp integration_candidate_dependencies_satisfied?(
         work_items,
         dependencies,
         accepted,
         subject_hash
       )
       when is_binary(subject_hash) do
    accepted_integration_outcomes(accepted, work_items)
    |> Enum.filter(&(&1.subject_hash == subject_hash))
    |> Enum.any?(
      &integration_item_dependencies_satisfied?(
        work_items,
        dependencies,
        accepted,
        &1.work_item_id
      )
    )
  end

  defp integration_item_dependencies_satisfied?(
         work_items,
         dependencies,
         accepted,
         integration_work_item_id
       ) do
    completion_relevant_item_ids(work_items, dependencies, integration_work_item_id)
    |> dependencies_satisfied?(dependencies, accepted)
  end

  defp completion_relevant_item_ids(work_items, dependencies, integration_work_item_id \\ nil) do
    root_ids =
      work_items
      |> Enum.filter(&map_value(&1, :required, true))
      |> Enum.map(&map_value(&1, :id))
      |> MapSet.new()

    root_ids =
      if integration_work_item_id do
        MapSet.put(root_ids, integration_work_item_id)
      else
        root_ids
      end

    dependency_ids_by_item =
      Enum.group_by(dependencies, &map_value(&1, :work_item_id), &map_value(&1, :depends_on_id))

    dependency_closure(root_ids, dependency_ids_by_item)
  end

  defp dependencies_satisfied?(relevant_ids, dependencies, accepted) do
    Enum.all?(dependencies, fn dependency ->
      not MapSet.member?(relevant_ids, map_value(dependency, :work_item_id)) or
        Map.has_key?(accepted, map_value(dependency, :depends_on_id))
    end)
  end

  defp dependency_closure(ids, dependency_ids_by_item) do
    next_ids =
      ids
      |> Enum.flat_map(&Map.get(dependency_ids_by_item, &1, []))
      |> MapSet.new()
      |> MapSet.difference(ids)

    if MapSet.size(next_ids) == 0 do
      ids
    else
      dependency_closure(MapSet.union(ids, next_ids), dependency_ids_by_item)
    end
  end

  defp resolved_completion_subjects(goal, decisions, current_revision) do
    goal_id = map_value(goal, :id)

    decisions
    |> Enum.filter(fn decision ->
      subject_hash = map_value(decision, :subject_hash)

      map_value(decision, :goal_revision) == current_revision and
        map_value(decision, :kind) == "completion" and is_nil(map_value(decision, :work_item_id)) and
        map_value(decision, :state) == "resolved" and valid_digest?(subject_hash) and
        value(map_value(decision, :resolution, %{}), "option_id") == "accept" and
        map_value(decision, :action_hash) ==
          RequestHash.canonical(%{
            kind: "completion",
            goal_id: goal_id,
            revision: current_revision,
            subject_hash: Base.encode16(subject_hash, case: :lower)
          })
    end)
    |> Enum.map(&map_value(&1, :subject_hash))
  end

  defp accepted_integration_outcomes(accepted, work_items) do
    Enum.flat_map(work_items, fn item ->
      work_item_id = map_value(item, :id)

      case Map.get(accepted, work_item_id) do
        outcome when is_map(outcome) ->
          if map_value(item, :integration, false) == true,
            do: [%{work_item_id: work_item_id, subject_hash: map_value(outcome, :subject_hash)}],
            else: []

        _ ->
          []
      end
    end)
  end

  defp required_goal_predicates_satisfied?(
         acceptance_contract,
         final_acceptance,
         subject_hash,
         evidence,
         tasks,
         runs,
         current_revision
       ) do
    predicates = value(acceptance_contract, :predicates, [])

    predicates != [] and
      unmet_required_goal_evidence_predicates(
        acceptance_contract,
        subject_hash,
        evidence,
        tasks,
        runs,
        current_revision
      ) == [] and
      Enum.all?(predicates, fn predicate ->
        case value(predicate, :kind) do
          "operator_acceptance" ->
            final_acceptance == "operator"

          _ ->
            true
        end
      end)
  end

  defp unmet_required_goal_evidence_predicates(
         acceptance_contract,
         subject_hash,
         evidence,
         tasks,
         runs,
         current_revision
       ) do
    eligible_evidence = eligible_goal_evidence(evidence, tasks, runs, current_revision)

    acceptance_contract
    |> value(:predicates, [])
    |> Enum.reject(fn predicate ->
      case value(predicate, :kind) do
        "operator_acceptance" ->
          true

        kind when kind in ["check", "artifact", "review"] ->
          Enum.any?(eligible_evidence, fn row ->
            map_value(row, :subject_hash) == subject_hash and
              map_value(row, :verdict) == "passed" and
              value(map_value(row, :payload, %{}), "predicate_id") == value(predicate, :id) and
              goal_predicate_matches_evidence?(predicate, row)
          end)

        _ ->
          false
      end
    end)
  end

  defp eligible_goal_evidence(evidence, tasks, runs, current_revision) do
    task_by_id = Map.new(tasks, &{map_value(&1, :id), &1})
    run_by_id = Map.new(runs, &{map_value(&1, :id), &1})

    Enum.filter(evidence, fn row ->
      run = Map.get(run_by_id, map_value(row, :run_id))
      task = run && Map.get(task_by_id, map_value(run, :task_id))

      is_map(task) and map_value(task, :purpose) == "validate" and
        map_value(task, :goal_revision) == current_revision and
        map_value(task, :state) == "completed" and
        map_value(run, :state) == "completed" and
        map_value(run, :generation) == map_value(task, :current_generation) and
        valid_subject_evidence?(row) and
        (map_value(row, :kind) != "review" or
           value(map_value(row, :source_ref, %{}), "review_task_id") == map_value(task, :id))
    end)
  end

  defp valid_subject_evidence?(row) do
    subject_hash = map_value(row, :subject_hash)
    expected_hash = digest(subject_hash)
    payload = map_value(row, :payload, %{})

    valid_digest?(subject_hash) and is_map(value(payload, "subject")) and
      RequestHash.canonical(value(payload, "subject")) == subject_hash and
      value(payload, "subject_hash") == expected_hash and
      value(map_value(row, :source_ref, %{}), "subject_hash") == expected_hash
  end

  defp goal_predicate_matches_evidence?(predicate, evidence) do
    source = map_value(evidence, :source_ref, %{})
    payload = map_value(evidence, :payload, %{})

    case value(predicate, :kind) do
      "check" ->
        map_value(evidence, :kind) == "check" and
          value(source, "validator_profile") == value(predicate, :validator_profile) and
          map_value(evidence, :validator_profile) == value(predicate, :validator_profile)

      "artifact" ->
        subject = value(payload, "subject")

        map_value(evidence, :kind) == "artifact" and
          value(source, "resource_id") == value(predicate, :resource_id) and
          value(source, "path") == value(predicate, :path) and
          value(source, "commit") == value(payload, "commit") and
          value(source, "resource_id") == value(subject, "resource_id") and
          value(source, "commit") == value(subject, "commit") and
          value(payload, "resource_id") == value(predicate, :resource_id) and
          value(payload, "path") == value(predicate, :path)

      "review" ->
        map_value(evidence, :kind) == "review" and
          map_value(evidence, :validator_profile) == value(predicate, :reviewer_profile) and
          value(payload, "verdict") == map_value(evidence, :verdict)

      _ ->
        false
    end
  end

  defp valid_digest?(value), do: is_binary(value) and byte_size(value) == 32

  defp allowed_actions(
         "draft",
         _completion,
         work_items,
         tasks,
         decisions,
         current_revision,
         task_settled_events
       ) do
    actions = ["amend", "cancel"]

    cond do
      work_items != [] ->
        ["activate" | actions]

      not pending_planning_task?(tasks, task_settled_events, current_revision) and
          not plan_decision_pending_acceptance?(decisions, current_revision) ->
        ["request_plan" | actions]

      true ->
        actions
    end
  end

  defp allowed_actions(
         "active",
         completion,
         _work_items,
         _tasks,
         _decisions,
         _current_revision,
         _receipts
       ),
       do: maybe_achieve(["pause", "amend", "cancel"], completion)

  defp allowed_actions(
         "paused",
         completion,
         _work_items,
         _tasks,
         _decisions,
         _current_revision,
         _receipts
       ),
       do: maybe_achieve(["resume", "amend", "cancel"], completion)

  defp allowed_actions(
         _state,
         _completion,
         _work_items,
         _tasks,
         _decisions,
         _current_revision,
         _receipts
       ),
       do: []

  defp pending_planning_task?(tasks, task_settled_events, current_revision) do
    settled_task_ids =
      task_settled_events
      |> Enum.reduce(MapSet.new(), fn event, settled ->
        with "task_settled" <- map_value(event, :kind),
             ^current_revision <- map_value(event, :revision),
             {:ok, task_id} <-
               matching_receipt_field(
                 map_value(event, :payload),
                 map_value(event, :response),
                 :task_id
               ),
             %{} = task <- Enum.find(tasks, &(map_value(&1, :id) == task_id)),
             true <- planning_task?(task, current_revision) do
          MapSet.put(settled, task_id)
        else
          _ -> settled
        end
      end)

    Enum.any?(tasks, fn task ->
      planning_task?(task, current_revision) and
        (map_value(task, :state) in @nonterminal_task_states or
           not MapSet.member?(settled_task_ids, map_value(task, :id)))
    end)
  end

  defp plan_decision_pending_acceptance?(decisions, current_revision) do
    Enum.any?(decisions, fn decision ->
      map_value(decision, :goal_revision) == current_revision and
        map_value(decision, :kind) == "plan" and
        (map_value(decision, :state) == "open" or
           (map_value(decision, :state) == "resolved" and
              value(map_value(decision, :resolution, %{}), :option_id) == "accept"))
    end)
  end

  defp maybe_achieve(actions, %{ready?: true}), do: ["achieve" | actions]
  defp maybe_achieve(actions, _completion), do: actions

  defp outcome_projection(outcome) do
    %{
      id: map_value(outcome, :id),
      goal_id: map_value(outcome, :goal_id),
      goal_revision: map_value(outcome, :goal_revision),
      work_item_id: map_value(outcome, :work_item_id),
      producing_task_id: map_value(outcome, :producing_task_id),
      producing_run_id: map_value(outcome, :producing_run_id),
      producing_result_id: map_value(outcome, :producing_result_id),
      validation_task_id: map_value(outcome, :validation_task_id),
      candidate_subject: sanitize(map_value(outcome, :candidate_subject), 4_000),
      subject_hash: digest(map_value(outcome, :subject_hash)),
      evidence_ids: list_value(outcome, :evidence_ids),
      disposition: map_value(outcome, :disposition),
      reason: present(map_value(outcome, :reason)),
      decision_id: map_value(outcome, :decision_id),
      inserted_at: map_value(outcome, :inserted_at)
    }
  end

  defp decision_projection(decision, current_revision) do
    %{
      id: map_value(decision, :id),
      goal_revision: map_value(decision, :goal_revision),
      work_item_id: map_value(decision, :work_item_id),
      kind: map_value(decision, :kind),
      state: map_value(decision, :state),
      open?:
        map_value(decision, :state) == "open" and
          map_value(decision, :goal_revision) == current_revision,
      question: truncate(map_value(decision, :question), 2_000),
      options: sanitize(map_value(decision, :options, []), 8_000),
      resolution: sanitize(map_value(decision, :resolution), 8_000),
      expires_at: map_value(decision, :expires_at),
      inserted_at: map_value(decision, :inserted_at),
      updated_at: map_value(decision, :updated_at)
    }
  end

  defp execution_by_work_item(work_items, tasks, runs, runtimes) do
    runtime_by_id = Map.new(runtimes, &{map_value(&1, :id), &1})
    latest_tasks = latest_tasks_by_work_item(tasks)
    latest_runs = latest_runs_by_task(runs)

    Map.new(work_items, fn item ->
      item_id = map_value(item, :id)
      task = Map.get(latest_tasks, item_id)
      run = task && Map.get(latest_runs, map_value(task, :id))
      runtime = run && Map.get(runtime_by_id, map_value(run, :runtime_id))
      {item_id, execution_projection(task, run, runtime, item)}
    end)
  end

  defp execution_projection(nil, nil, nil, _work_item), do: nil

  defp execution_projection(task, run, runtime, work_item) do
    state = execution_state(task, run)

    %{
      task: task && task_projection(task),
      run: run && run_projection(run),
      runtime: runtime && runtime_projection(runtime),
      state: state,
      terminal?: terminal_execution?(state),
      pending_assignment: pending_assignment_projection(task, work_item),
      source: :task_run_records
    }
  end

  defp execution_state(task, run) do
    case map_value(task, :state) do
      "queued" -> "queued"
      _ -> map_value(run, :state) || map_value(task, :state)
    end
  end

  defp pending_assignment_projection(task, work_item) do
    if map_value(task, :state) == "queued" do
      %{
        repository_resource_id: pending_assignment_repository_resource_id(task, work_item),
        agent_profile: map_value(task, :agent_profile),
        workspace: map_value(task, :workspace),
        requested_session_id: map_value(task, :requested_session_id),
        handoff_source_run_id: map_value(task, :handoff_source_run_id),
        machine_affinity:
          if(is_nil(map_value(task, :handoff_source_run_id)),
            do: nil,
            else: "handoff_source_machine"
          )
      }
    end
  end

  defp pending_assignment_repository_resource_id(_task, work_item) when is_map(work_item),
    do: map_value(work_item, :repository_resource_id)

  defp pending_assignment_repository_resource_id(task, nil) do
    task
    |> map_value(:input, %{})
    |> map_value(:subject, %{})
    |> map_value(:resource_id)
  end

  defp goal_execution_projection(tasks, runs) do
    states = Enum.map(tasks, &map_value(&1, :state))
    run_states = Enum.map(runs, &map_value(&1, :state))

    %{
      task_count: length(tasks),
      active_task_count: Enum.count(states, &(&1 in @active_task_states)),
      nonterminal_task_count: Enum.count(states, &(&1 in @nonterminal_task_states)),
      terminal_task_count: Enum.count(states, &(&1 not in @nonterminal_task_states)),
      run_count: length(runs),
      active_run_count: Enum.count(run_states, &(&1 in @active_task_states)),
      states: Enum.frequencies(states),
      run_states: Enum.frequencies(run_states)
    }
  end

  defp planning_execution_projection(tasks, runs, runtimes) do
    task =
      tasks
      |> Enum.filter(&planning_task?(&1, map_value(&1, :goal_revision)))
      |> Enum.sort_by(fn row -> {map_value(row, :inserted_at), map_value(row, :id)} end)
      |> List.last()

    run =
      if task do
        runs
        |> Enum.filter(&(map_value(&1, :task_id) == map_value(task, :id)))
        |> Enum.sort_by(fn row -> {map_value(row, :generation), map_value(row, :id)} end)
        |> List.last()
      end

    runtime =
      if run do
        Enum.find(runtimes, &(map_value(&1, :id) == map_value(run, :runtime_id)))
      end

    execution_projection(task, run, runtime, nil)
  end

  defp task_projection(task) do
    %{
      id: map_value(task, :id),
      work_item_id: map_value(task, :work_item_id),
      goal_id: map_value(task, :goal_id),
      goal_revision: map_value(task, :goal_revision),
      purpose: map_value(task, :purpose),
      state: map_value(task, :state),
      current_generation: map_value(task, :current_generation),
      attempt_generation: map_value(task, :attempt_generation),
      waiting_transition_id: map_value(task, :waiting_transition_id),
      result: result_projection(map_value(task, :result)),
      failure: failure_projection(map_value(task, :failure)),
      inserted_at: map_value(task, :inserted_at),
      updated_at: map_value(task, :updated_at)
    }
  end

  defp run_projection(run) do
    %{
      id: map_value(run, :id),
      task_id: map_value(run, :task_id),
      generation: map_value(run, :generation),
      state: map_value(run, :state),
      runtime_id: map_value(run, :runtime_id),
      harness_session_id: map_value(run, :harness_session_id),
      result: result_projection(map_value(run, :result)),
      failure: failure_projection(map_value(run, :failure)),
      assigned_at: map_value(run, :assigned_at),
      claimed_at: map_value(run, :claimed_at),
      inserted_at: map_value(run, :inserted_at),
      updated_at: map_value(run, :updated_at)
    }
  end

  defp runtime_projection(runtime) do
    %{
      id: map_value(runtime, :id),
      status: map_value(runtime, :status),
      agent_profile: map_value(runtime, :agent_profile),
      workspace: map_value(runtime, :workspace),
      capabilities: sanitize(map_value(runtime, :capabilities, %{}), 4_000),
      last_heartbeat_at: map_value(runtime, :last_heartbeat_at)
    }
  end

  defp result_projection(nil), do: nil

  defp result_projection(result) when is_map(result) do
    case semantic_task_result(result) do
      nil ->
        nil

      semantic_result ->
        %{}
        |> maybe_put(:kind, value(semantic_result, :kind))
        |> maybe_put(
          :summary,
          truncate(value(semantic_result, :summary) || value(semantic_result, :message), 2_000)
        )
        |> maybe_put(:blocker, blocker_projection(value(semantic_result, :blocker)))
        |> maybe_put(:subject_hash, digest(value(semantic_result, :subject_hash)))
        |> maybe_put(:evidence_refs, list_value(semantic_result, :evidence_refs))
        |> maybe_put(
          :proposed_next_action,
          truncate(value(semantic_result, :proposed_next_action), 1_000)
        )
    end
  end

  defp result_projection(_result), do: nil

  defp failure_projection(nil), do: nil

  defp failure_projection(failure) when is_map(failure) do
    %{}
    |> maybe_put(:kind, value(failure, :kind))
    |> maybe_put(:reason, truncate(value(failure, :reason) || value(failure, :message), 2_000))
  end

  defp failure_projection(_failure), do: nil

  defp terminal_execution?(state) do
    state not in @nonterminal_task_states and not is_nil(state)
  end

  defp latest_tasks_by_work_item(tasks) do
    tasks
    |> Enum.filter(&(not is_nil(map_value(&1, :work_item_id))))
    |> Enum.sort_by(fn task -> {map_value(task, :inserted_at), map_value(task, :id)} end)
    |> Map.new(fn task -> {map_value(task, :work_item_id), task} end)
  end

  defp latest_runs_by_task(runs) do
    runs
    |> Enum.sort_by(fn run ->
      {map_value(run, :generation), map_value(run, :inserted_at), map_value(run, :id)}
    end)
    |> Map.new(fn run -> {map_value(run, :task_id), run} end)
  end

  defp latest_usage(usage) do
    run_by_usage_id =
      Map.new(usage, fn row ->
        {map_value(row, :id), map_value(row, :run_id)}
      end)

    superseded_ids =
      usage
      |> Enum.flat_map(fn row ->
        id = map_value(row, :id)
        run_id = map_value(row, :run_id)
        supersedes_id = map_value(row, :supersedes_id)

        if is_nil(supersedes_id) or same_uuid?(id, supersedes_id) or
             Map.get(run_by_usage_id, supersedes_id) != run_id,
           do: [],
           else: [supersedes_id]
      end)
      |> MapSet.new()

    usage
    |> Enum.reject(&MapSet.member?(superseded_ids, map_value(&1, :id)))
    |> Enum.sort_by(fn row ->
      {map_value(row, :run_id), map_value(row, :inserted_at), map_value(row, :id)}
    end)
    |> Enum.reduce(%{}, fn row, acc ->
      key = {map_value(row, :run_id), map_value(row, :usage_key)}
      Map.put(acc, key, row)
    end)
    |> Map.values()
  end

  defp same_uuid?(left, right) when is_binary(left) and is_binary(right) do
    case {Ecto.UUID.dump(left), Ecto.UUID.dump(right)} do
      {{:ok, left}, {:ok, right}} -> left == right
      _ -> false
    end
  end

  defp same_uuid?(_left, _right), do: false

  defp accounting_projection(reservations, usage, tasks, runs) do
    usage_summary = effective_usage_summary(usage, tasks, runs)

    %{
      held_microusd:
        reservations
        |> Enum.filter(&(map_value(&1, :state) == "held"))
        |> Enum.map(&map_value(&1, :reserved_microusd, 0))
        |> Enum.sum()
        |> microusd_wire_value(),
      unknown_reservation_count: Enum.count(reservations, &(map_value(&1, :state) == "unknown")),
      settled_reservation_count: Enum.count(reservations, &(map_value(&1, :state) == "settled")),
      released_reservation_count:
        Enum.count(reservations, &(map_value(&1, :state) == "released")),
      usage_records: length(usage),
      usage_leaf_count: usage_summary.usage_leaf_count,
      known_usage_cost_microusd: microusd_wire_value(usage_summary.known_cost_microusd),
      usage_cost_microusd:
        if(usage_summary.unknown_usage_count == 0,
          do: microusd_wire_value(usage_summary.known_cost_microusd),
          else: nil
        ),
      usage_cost_unknown?: usage_summary.unknown_usage_count > 0,
      unknown_usage_count: usage_summary.unknown_usage_count
    }
  end

  defp event_projection(event) do
    %{
      id: map_value(event, :id),
      goal_id: map_value(event, :goal_id),
      sequence: map_value(event, :sequence),
      kind: map_value(event, :kind),
      actor_ref: map_value(event, :actor_ref),
      revision: map_value(event, :revision),
      request_hash: digest(map_value(event, :request_hash)),
      request_hash_version: map_value(event, :request_hash_version),
      payload: event_payload(map_value(event, :payload)),
      response: event_response(map_value(event, :response)),
      inserted_at: map_value(event, :inserted_at)
    }
  end

  defp event_payload(payload) when is_map(payload) do
    pick(
      payload,
      ~w(
        kind reason summary blocker result_kind outcome_id work_item_id task_id run_id generation state goal_revision
        decision_id revision purpose source_identity candidate_identity deferred_identity admission_key
      )
    )
    |> sanitize(@context_limit)
  end

  defp event_payload(_payload), do: %{}

  defp event_response(response) when is_map(response) do
    pick(
      response,
      ~w(
        status code current_version current_revision allowed_actions details next_wake_at task_id run_id generation
        goal_revision settlement result_id result_kind reason blocker proposed_next_action
        proposed_next_action_status subject_hash evidence_refs
        disposition purpose candidate_identity
      )
    )
    |> sanitize(@context_limit)
  end

  defp event_response(_response), do: %{}

  defp external_wait_projection(wait) do
    %{
      id: map_value(wait, :id),
      goal_revision: map_value(wait, :goal_revision),
      work_item_id: map_value(wait, :work_item_id),
      task_id: map_value(wait, :task_id),
      run_id: map_value(wait, :run_id),
      generation: map_value(wait, :run_generation),
      result_id: map_value(wait, :result_id),
      state: map_value(wait, :state),
      reason:
        if(map_value(wait, :state) == "unsupported", do: "unsupported_external_check", else: nil),
      resource_id: map_value(wait, :resource_id),
      external_ref: map_value(wait, :external_ref),
      subject: sanitize(map_value(wait, :subject), @context_limit),
      source_ref: sanitize(map_value(wait, :source_ref), @context_limit),
      next_check_at: map_value(wait, :next_check_at),
      check_seq: map_value(wait, :check_seq),
      receipt_event_id: map_value(wait, :receipt_event_id),
      inserted_at: map_value(wait, :inserted_at),
      updated_at: map_value(wait, :updated_at)
    }
  end

  defp context_projection(snapshot, _opts) do
    %{
      id: map_value(snapshot, :id),
      goal_id: map_value(snapshot, :goal_id),
      work_item_id: map_value(snapshot, :work_item_id),
      goal_revision: map_value(snapshot, :goal_revision),
      schema_version: map_value(snapshot, :schema_version),
      content_hash: digest(map_value(snapshot, :content_hash)),
      payload: sanitize_context(map_value(snapshot, :payload)),
      inserted_at: map_value(snapshot, :inserted_at)
    }
  end

  defp attention_entry(projection) do
    %{
      goal_id: projection.id,
      project_id: projection.project_id,
      title: projection.title,
      state: projection.state,
      current_revision: projection.current_revision,
      blockers: projection.blockers,
      blocker_reasons: projection.blocker_reasons,
      settlement_attention: projection.settlement_attention,
      attention_reasons: projection.attention_reasons,
      active_tasks: projection.execution.active_task_count,
      nonterminal_tasks: projection.execution.nonterminal_task_count,
      ready_to_achieve?: projection.completion.ready?,
      updated_at: projection.updated_at,
      source: :durable_records
    }
  end

  defp attention_entry?(entry) do
    entry.blockers != [] or entry.settlement_attention != [] or entry.active_tasks > 0 or
      entry.state in ["draft", "paused"] or entry.ready_to_achieve?
  end

  defp next_attention_cursor([], _page), do: nil

  defp next_attention_cursor(_rest, page) do
    last = List.last(page)
    encode_attention_cursor(map_value(last, :updated_at), map_value(last, :id))
  end

  defp attention_cursor(query, nil), do: {:ok, query}

  defp attention_cursor(query, %{updated_at: updated_at, id: id}) do
    with {:ok, updated_at} <- attention_timestamp(updated_at),
         true <- is_binary(id) and id != "" do
      {:ok,
       where(
         query,
         [goal],
         goal.updated_at < ^updated_at or (goal.updated_at == ^updated_at and goal.id < ^id)
       )}
    else
      _ -> {:error, :invalid_cursor}
    end
  end

  defp attention_cursor(query, %{"updated_at" => updated_at, "id" => id}) do
    attention_cursor(query, %{updated_at: updated_at, id: id})
  end

  defp attention_cursor(query, cursor) when is_binary(cursor) do
    case decode_attention_cursor(cursor) do
      {:ok, decoded} -> attention_cursor(query, decoded)
      :error -> {:error, :invalid_cursor}
    end
  end

  defp attention_cursor(_query, _cursor), do: {:error, :invalid_cursor}

  defp encode_attention_cursor(updated_at, id) when is_binary(id) and id != "" do
    with {:ok, updated_at} <- attention_timestamp(updated_at) do
      %{"updated_at" => DateTime.to_iso8601(updated_at), "id" => id}
      |> Jason.encode!()
      |> Base.url_encode64(padding: false)
    else
      _ -> nil
    end
  end

  defp encode_attention_cursor(_updated_at, _id), do: nil

  defp decode_attention_cursor(cursor) do
    with {:ok, encoded} <- Base.url_decode64(cursor, padding: false),
         {:ok, %{"updated_at" => updated_at, "id" => id}} <- Jason.decode(encoded),
         {:ok, updated_at} <- attention_timestamp(updated_at),
         true <- is_binary(id) and id != "" do
      {:ok, %{updated_at: updated_at, id: id}}
    else
      _ -> :error
    end
  end

  defp attention_timestamp(%DateTime{} = value), do: {:ok, value}

  defp attention_timestamp(%NaiveDateTime{} = value),
    do: DateTime.from_naive(value, "Etc/UTC")

  defp attention_timestamp(value) when is_binary(value) do
    case DateTime.from_iso8601(value) do
      {:ok, datetime, _offset} -> {:ok, datetime}
      _ -> :error
    end
  end

  defp attention_timestamp(_value), do: :error

  defp records(related, key, fallback) do
    case map_value(related, key) do
      nil -> normalize_records(fallback)
      rows -> normalize_records(rows)
    end
  end

  defp normalize_records(%Ecto.Association.NotLoaded{}), do: []
  defp normalize_records(nil), do: []
  defp normalize_records(rows) when is_list(rows), do: rows
  defp normalize_records(rows) when is_map(rows), do: Map.values(rows)
  defp normalize_records(row), do: [row]

  defp goal_task?(task), do: not is_nil(map_value(task, :goal_id))

  defp external_blocker?(blocker) when is_map(blocker), do: value(blocker, :kind) == "external"

  defp external_blocker?(value) when is_binary(value) do
    String.contains?(String.downcase(value), "external") or
      value in ["waiting_external", "external_action"]
  end

  defp external_blocker?(_value), do: false

  # Native terminal payloads wrap the schema-validated TaskResult. Keep the
  # legacy direct result shape readable, but never fall back to outer fields
  # when a malformed typed envelope was supplied.
  defp semantic_task_result(result) when is_map(result) do
    case task_result_payload(result) do
      {:wrapped, task_result} ->
        if(value(task_result, :schema_version) == @task_result_schema_version, do: task_result)

      :direct ->
        result
    end
  end

  defp task_result_payload(result) do
    cond do
      Map.has_key?(result, :task_result) -> {:wrapped, Map.get(result, :task_result)}
      Map.has_key?(result, "task_result") -> {:wrapped, Map.get(result, "task_result")}
      true -> :direct
    end
  end

  defp blocker_projection(blocker) when is_map(blocker), do: sanitize(blocker, 4_000)
  defp blocker_projection(blocker), do: present(blocker)

  defp map_value(map, key, default \\ nil)
  defp map_value(nil, _key, default), do: default

  defp map_value(map, key, default) when is_map(map) do
    case Map.fetch(map, key) do
      {:ok, nil} -> default
      {:ok, value} -> value
      :error -> Map.get(map, Atom.to_string(key), default)
    end
  end

  defp map_value(_map, _key, default), do: default

  defp value(map, key, default \\ nil), do: map_value(map, key, default)

  defp list_value(map, key) do
    case map_value(map, key, []) do
      value when is_list(value) -> value
      nil -> []
      value -> [value]
    end
  end

  defp maybe_put(map, _key, nil), do: map
  defp maybe_put(map, _key, ""), do: map
  defp maybe_put(map, key, value), do: Map.put(map, key, value)

  defp present(value) when is_binary(value) and value != "", do: value
  defp present(_value), do: nil

  defp truncate(value, limit) when is_binary(value), do: String.slice(value, 0, limit)
  defp truncate(value, _limit), do: value

  defp digest(nil), do: nil
  defp digest(<<_::binary-size(32)>> = value), do: "sha256:" <> Base.encode16(value, case: :lower)
  defp digest(value) when is_binary(value), do: truncate(value, 200)
  defp digest(value), do: value

  defp pick(map, keys) do
    Enum.reduce(keys, %{}, fn key, acc ->
      atom_key = event_key(key)

      case Map.fetch(map, atom_key) do
        {:ok, value} ->
          Map.put(acc, atom_key, value)

        :error ->
          case Map.fetch(map, key) do
            {:ok, value} -> Map.put(acc, atom_key, value)
            :error -> acc
          end
      end
    end)
  end

  defp event_key("kind"), do: :kind
  defp event_key("reason"), do: :reason
  defp event_key("summary"), do: :summary
  defp event_key("blocker"), do: :blocker
  defp event_key("result_kind"), do: :result_kind
  defp event_key("outcome_id"), do: :outcome_id
  defp event_key("work_item_id"), do: :work_item_id
  defp event_key("task_id"), do: :task_id
  defp event_key("run_id"), do: :run_id
  defp event_key("generation"), do: :generation
  defp event_key("state"), do: :state
  defp event_key("goal_revision"), do: :goal_revision
  defp event_key("decision_id"), do: :decision_id
  defp event_key("revision"), do: :revision
  defp event_key("status"), do: :status
  defp event_key("code"), do: :code
  defp event_key("current_version"), do: :current_version
  defp event_key("current_revision"), do: :current_revision
  defp event_key("allowed_actions"), do: :allowed_actions
  defp event_key("details"), do: :details
  defp event_key("next_wake_at"), do: :next_wake_at
  defp event_key("settlement"), do: :settlement
  defp event_key("result_id"), do: :result_id
  defp event_key("proposed_next_action"), do: :proposed_next_action
  defp event_key("proposed_next_action_status"), do: :proposed_next_action_status
  defp event_key("subject_hash"), do: :subject_hash
  defp event_key("evidence_refs"), do: :evidence_refs
  defp event_key("disposition"), do: :disposition
  defp event_key("purpose"), do: :purpose
  defp event_key("source_identity"), do: :source_identity
  defp event_key("candidate_identity"), do: :candidate_identity
  defp event_key("deferred_identity"), do: :deferred_identity
  defp event_key("admission_key"), do: :admission_key

  defp sanitize(value, limit) when is_binary(value), do: truncate(value, limit)

  defp sanitize(value, _limit) when is_number(value) or is_boolean(value) or is_nil(value),
    do: value

  defp sanitize(value, limit) when is_list(value) do
    value
    |> Enum.take(100)
    |> Enum.map(&sanitize(&1, div(limit, 2)))
  end

  defp sanitize(value, limit) when is_map(value) do
    value
    |> Enum.reject(fn {key, _value} -> forbidden_key?(key) end)
    |> Enum.take(200)
    |> Map.new(fn {key, child} ->
      child = sanitize(child, div(limit, 2))
      {key, if(microusd_key?(key), do: microusd_wire_value(child), else: child)}
    end)
  end

  defp sanitize(value, _limit), do: inspect(value) |> truncate(500)

  defp microusd_wire_value(value) when is_integer(value), do: Integer.to_string(value)
  defp microusd_wire_value(value), do: value

  defp microusd_key?(key) do
    key
    |> to_string()
    |> String.ends_with?("_microusd")
  end

  defp forbidden_key?(key) do
    key = canonical_key(key)

    Enum.any?(
      [
        "credential",
        "password",
        "secret",
        "token",
        "bearer",
        "authorization",
        "apikey",
        "privatekey",
        "rawtranscript",
        "transcript",
        "prompt",
        "sessionfilename",
        "localhandle"
      ],
      &String.contains?(key, &1)
    )
  end

  defp canonical_key(key) do
    key
    |> to_string()
    |> String.downcase()
    |> String.replace("_", "")
    |> String.replace("-", "")
  end

  defp bounded_limit(value, max) when is_integer(value) and value > 0, do: min(value, max)
  defp bounded_limit(_value, max), do: max

  defp normalize_options(options) do
    Enum.reduce(options, [], fn
      {:after, value}, acc -> Keyword.put(acc, :after, value)
      {:cursor, value}, acc -> Keyword.put(acc, :cursor, value)
      {:limit, value}, acc -> Keyword.put(acc, :limit, value)
      {:project_id, value}, acc -> Keyword.put(acc, :project_id, value)
      {:repo, value}, acc -> Keyword.put(acc, :repo, value)
      {:state, value}, acc -> Keyword.put(acc, :state, value)
      {"after", value}, acc -> Keyword.put(acc, :after, value)
      {"cursor", value}, acc -> Keyword.put(acc, :cursor, value)
      {"limit", value}, acc -> Keyword.put(acc, :limit, value)
      {"project_id", value}, acc -> Keyword.put(acc, :project_id, value)
      {"repo", value}, acc -> Keyword.put(acc, :repo, value)
      {"state", value}, acc -> Keyword.put(acc, :state, value)
      _, acc -> acc
    end)
  end
end
