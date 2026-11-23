defmodule SymmetryControl.Workspaces.ReadModel do
  @moduledoc false

  import Ecto.Query

  alias SymmetryControl.Integrations
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Command
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.RunEvent
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.RunTransition
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces

  @active_states ["queued", "assigned", "claimed", "running", "cancelling"]
  @semantic_kinds [
    "progress",
    "summary",
    "rationale",
    "finding",
    "artifact",
    "test",
    "pull_request",
    "ci",
    "review",
    "waiting_for_input"
  ]

  def workspace(selected_project_id \\ nil) do
    with {:ok, snapshot} <- Workspaces.workspace_snapshot(selected_project_id),
         {:ok, runtimes} <- Orchestration.runtime_snapshots() do
      project = snapshot.selected_project
      connections = Integrations.list_connections()
      work_items = if project, do: project.work_items, else: []
      projections = projections(work_items)
      compatible_runtimes = compatible_runtimes(project, runtimes)

      {:ok,
       %{
         snapshot: snapshot,
         connections: connections,
         bound_resources: Integrations.list_bound_resources(),
         runtimes: compatible_runtimes,
         registered_runtimes: runtimes,
         projections: projections,
         activity: activity(work_items, projections, compatible_runtimes),
         health: health(project, connections, projections, compatible_runtimes)
       }}
    end
  end

  def connections do
    %{
      providers: Integrations.connection_providers(),
      connections: Integrations.list_connections(),
      bound_resources: Integrations.list_bound_resources()
    }
  end

  def projections(work_items) do
    task_ids =
      work_items
      |> Enum.map(& &1.orchestration_task_id)
      |> Enum.reject(&is_nil/1)
      |> Enum.uniq()

    if task_ids == [] do
      %{}
    else
      tasks =
        Task
        |> where([task], task.id in ^task_ids)
        |> Repo.all()
        |> Map.new(&{&1.id, &1})

      runs = current_runs(task_ids)
      runtimes = current_runtimes(runs)
      commands = latest_commands(task_ids)
      waiting = waiting_contexts(tasks, runs)
      pending = pending_supervisory_commands(runs)
      retry_eligible = retry_runtime_eligible(work_items, tasks)
      evidence = evidence(task_ids)

      Map.new(task_ids, fn task_id ->
        task = Map.fetch!(tasks, task_id)
        run = Map.get(runs, task_id)
        runtime = run && Map.get(runtimes, run.runtime_id)

        {task_id,
         %{
           execution: %{
             task: task,
             run: run,
             runtime: runtime,
             retry_runtime_eligible: MapSet.member?(retry_eligible, task_id),
             waiting: Map.get(waiting, task_id),
             latest_command: Map.get(commands, task_id),
             controls:
               Orchestration.control_capabilities(
                 task,
                 run,
                 runtime,
                 not is_nil(run) and MapSet.member?(pending, run.id)
               )
           },
           evidence:
             Map.get(
               evidence,
               task_id,
               task.attempt_generation |> empty_evidence() |> finalize_evidence()
             )
         }}
      end)
    end
  end

  # The current runs' runtimes, loaded in one query; the per-task capability
  # projection reads them from this map instead of a query per task.
  defp current_runtimes(runs) do
    runtime_ids = runs |> Map.values() |> Enum.map(& &1.runtime_id) |> Enum.uniq()

    from(runtime in Runtime, where: runtime.id in ^runtime_ids)
    |> Repo.all()
    |> Map.new(&{&1.id, &1})
  end

  # Eligibility reads the binding the retried generation is re-admitted with —
  # `retry_binding/2` resolves the work item's corrected profile and workspace
  # with the attempt's supervisory requirement — not the failed task row's. Only
  # a goal-less terminal attempt (`Orchestration.retryable_attempt?/1`) can be
  # retried, so live and Goal-owned tasks never enter the query.
  defp retry_runtime_eligible(work_items, tasks) do
    bindings =
      Enum.flat_map(work_items, fn item ->
        case Map.get(tasks, item.orchestration_task_id) do
          %Task{} = task ->
            if Orchestration.retryable_attempt?(task),
              do: [Workspaces.retry_binding_for(task, item)],
              else: []

          _absent ->
            []
        end
      end)

    if bindings == [] do
      MapSet.new()
    else
      Orchestration.retry_runtime_eligible_task_ids(bindings)
    end
  end

  # The runs whose pause/resume command is pending or applied: an attempt at a
  # supervisory transition the served controls must not duplicate.
  defp pending_supervisory_commands(runs) do
    run_ids = runs |> Map.values() |> Enum.map(& &1.id)

    from(command in Command,
      where:
        command.run_id in ^run_ids and command.kind in ["pause", "resume"] and
          command.state in ["pending", "applied"],
      select: command.run_id
    )
    |> Repo.all()
    |> MapSet.new()
  end

  defp current_runs(task_ids) do
    from(run in Run,
      join: task in Task,
      on: task.id == run.task_id and task.attempt_generation == run.generation,
      where: task.id in ^task_ids,
      select: run
    )
    |> Repo.all()
    |> Map.new(&{&1.task_id, &1})
  end

  defp latest_commands(task_ids) do
    from(command in Command,
      where: command.task_id in ^task_ids,
      distinct: command.task_id,
      order_by: [asc: command.task_id, desc: command.inserted_at, desc: command.id]
    )
    |> Repo.all()
    |> Map.new(&{&1.task_id, &1})
  end

  defp waiting_contexts(tasks, runs) do
    waiting_runs =
      Enum.flat_map(tasks, fn {task_id, task} ->
        case {task.state, Map.get(runs, task_id)} do
          {"waiting_for_input", %Run{state: "waiting_for_input"} = run} -> [run]
          _other -> []
        end
      end)

    transition_ids =
      Enum.map(waiting_runs, fn run -> Map.fetch!(tasks, run.task_id).waiting_transition_id end)

    if transition_ids == [] do
      %{}
    else
      transitions =
        from(transition in RunTransition,
          where:
            transition.transition_id in ^transition_ids and
              transition.state == "waiting_for_input"
        )
        |> Repo.all()
        |> Map.new(&{&1.transition_id, &1})

      events =
        from(event in RunEvent,
          where: event.event_id in ^transition_ids and event.kind == "waiting_for_input"
        )
        |> Repo.all()
        |> Map.new(&{&1.event_id, &1})

      Map.new(waiting_runs, fn run ->
        transition_id = Map.fetch!(tasks, run.task_id).waiting_transition_id
        transition = Map.get(transitions, transition_id)
        event = Map.get(events, transition_id)
        authoritative_packet? = transition && value(transition.payload, "decision")

        payload =
          if event && !authoritative_packet?,
            do: event.payload,
            else: transition && transition.payload

        recorded_at =
          if event && !authoritative_packet?,
            do: event.inserted_at,
            else: transition && transition.inserted_at

        {run.task_id,
         if transition do
           %{
             run_id: run.id,
             generation: run.generation,
             transition_id: transition.transition_id,
             question: value(payload, "question"),
             decision: value(payload, "decision"),
             payload: payload || %{},
             recorded_at: recorded_at
           }
         end}
      end)
    end
  end

  def evidence_for_run(%Run{id: run_id, task_id: task_id, generation: generation}) do
    from(event in RunEvent,
      join: run in Run,
      on: run.id == event.run_id,
      where: run.id == ^run_id and run.task_id == ^task_id and run.generation == ^generation
    )
    |> project_evidence()
    |> Map.get(task_id, generation |> empty_evidence() |> finalize_evidence())
  end

  defp evidence(task_ids) do
    project_evidence(
      from(event in RunEvent,
        join: run in Run,
        on: run.id == event.run_id,
        join: task in Task,
        on: task.id == run.task_id and task.attempt_generation == run.generation,
        where: task.id in ^task_ids
      )
    )
  end

  defp project_evidence(query) do
    from([event, run] in query,
      where: event.kind in ^@semantic_kinds,
      order_by: [
        asc: run.generation,
        asc: event.sequence,
        asc: event.inserted_at,
        asc: event.id
      ],
      select: %{
        task_id: run.task_id,
        generation: run.generation,
        event_id: event.event_id,
        kind: event.kind,
        payload: event.payload,
        recorded_at: event.inserted_at
      }
    )
    |> Repo.all()
    |> Enum.reduce(%{}, fn event, acc ->
      Map.update(
        acc,
        event.task_id,
        apply_event(empty_evidence(event.generation), event),
        &apply_event(&1, event)
      )
    end)
    |> Map.new(fn {task_id, evidence} -> {task_id, finalize_evidence(evidence)} end)
  end

  defp empty_evidence(generation) do
    %{
      generation: generation,
      summary: nil,
      findings: [],
      artifacts: [],
      tests_by_name: %{},
      pull_request: nil,
      ci: nil,
      review: nil
    }
  end

  defp apply_event(evidence, event) do
    source = source(event)

    case event.kind do
      "summary" ->
        case value(event.payload, "summary") || value(event.payload, "message") do
          text when is_binary(text) and text != "" ->
            %{evidence | summary: Map.put(source, :text, text)}

          _other ->
            evidence
        end

      "finding" ->
        case value(event.payload, "message") || value(event.payload, "title") do
          message when is_binary(message) and message != "" ->
            finding =
              source
              |> Map.put(:message, message)
              |> maybe_put(:severity, value(event.payload, "severity"))

            %{evidence | findings: [finding | evidence.findings]}

          _other ->
            evidence
        end

      "artifact" ->
        path = value(event.payload, "path")
        url = value(event.payload, "url")

        if present?(path) or http_url?(url) do
          artifact =
            source
            |> maybe_put(:path, path)
            |> maybe_put(:url, if(http_url?(url), do: url))
            |> maybe_put(:kind, value(event.payload, "kind"))
            |> maybe_put(:name, value(event.payload, "name"))

          %{evidence | artifacts: [artifact | evidence.artifacts]}
        else
          evidence
        end

      "test" ->
        name = value(event.payload, "name")
        status = value(event.payload, "status")

        if present?(name) and status in ["passed", "failed", "skipped", "running"] do
          test =
            source
            |> Map.put(:name, name)
            |> Map.put(:status, status)
            |> maybe_put(:summary, value(event.payload, "summary"))

          put_in(evidence, [:tests_by_name, name], test)
        else
          evidence
        end

      "pull_request" ->
        url = value(event.payload, "url")

        if http_url?(url) do
          pull_request =
            source
            |> Map.put(:url, url)
            |> maybe_put(:state, value(event.payload, "state"))

          %{evidence | pull_request: pull_request}
        else
          evidence
        end

      "ci" ->
        status = value(event.payload, "status")

        if status in ["unknown", "pending", "passed", "failed"] do
          ci =
            source
            |> Map.put(:status, status)
            |> maybe_put(:url, valid_optional_url(value(event.payload, "url")))
            |> maybe_put(:summary, value(event.payload, "summary"))

          %{evidence | ci: ci}
        else
          evidence
        end

      "review" ->
        status = value(event.payload, "status")

        if status in ["none", "required", "changes_requested", "approved"] do
          review =
            source
            |> Map.put(:status, status)
            |> maybe_put(:url, valid_optional_url(value(event.payload, "url")))
            |> maybe_put(:summary, value(event.payload, "summary"))

          %{evidence | review: review}
        else
          evidence
        end

      _kind ->
        evidence
    end
  end

  defp finalize_evidence(evidence) do
    evidence
    |> Map.put(:findings, Enum.reverse(evidence.findings))
    |> Map.put(:artifacts, Enum.reverse(evidence.artifacts))
    |> Map.put(:tests, evidence.tests_by_name |> Map.values() |> Enum.sort_by(& &1.name))
    |> Map.delete(:tests_by_name)
  end

  defp source(event) do
    %{
      source: "agent",
      generation: event.generation,
      event_id: event.event_id,
      recorded_at: event.recorded_at
    }
  end

  defp compatible_runtimes(nil, _runtimes), do: []

  defp compatible_runtimes(project, runtimes) do
    bindings =
      MapSet.new([{project.default_agent_profile, project.default_workspace} | agent_bindings(project.work_items)])

    Enum.filter(runtimes, fn runtime ->
      MapSet.member?(bindings, {runtime.agent_profile, runtime.workspace})
    end)
  end

  defp agent_bindings(work_items) do
    work_items
    |> Enum.filter(&(&1.assignee_type == "agent"))
    |> Enum.map(&{&1.agent_profile, &1.workspace})
    |> Enum.reject(fn {profile, workspace} ->
      not present?(profile) or not present?(workspace)
    end)
  end

  defp activity(work_items, projections, runtimes) do
    runtime_by_id = Map.new(runtimes, &{&1.runtime_id, &1})

    work_items
    |> Enum.flat_map(fn item ->
      case Map.get(projections, item.orchestration_task_id) do
        nil ->
          []

        projection ->
          run = projection.execution.run
          runtime = run && Map.get(runtime_by_id, run.runtime_id)

          [
            %{
              work_item: item,
              execution: projection.execution,
              evidence: projection.evidence,
              runtime: runtime
            }
          ]
      end
    end)
    |> Enum.sort_by(fn entry ->
      {activity_rank(entry.execution.task.state), entry.work_item.updated_at}
    end)
  end

  defp activity_rank("waiting_for_input"), do: 0
  defp activity_rank("paused"), do: 0
  defp activity_rank(state) when state in @active_states, do: 1
  defp activity_rank("failed"), do: 2
  defp activity_rank(_state), do: 3

  defp health(nil, _connections, _projections, _runtimes) do
    %{connections: "unknown", runtimes: "offline", executions: "idle", synchronization: "unknown"}
  end

  defp health(project, connections, projections, runtimes) do
    external_resources =
      Enum.filter(
        project.resources,
        &(&1.kind in ["repository", "work_tracking", "ci", "connection"])
      )

    %{
      connections: connection_health(external_resources, connections),
      runtimes: runtime_health(runtimes),
      executions: execution_health(Map.values(projections)),
      synchronization: synchronization_health(external_resources)
    }
  end

  defp connection_health(resources, connections) do
    connection_ids =
      resources
      |> Enum.map(& &1.connection_id)
      |> Enum.reject(&is_nil/1)
      |> MapSet.new()

    bound_connections = Enum.filter(connections, &MapSet.member?(connection_ids, &1.id))

    connected_resources =
      Enum.filter(resources, &MapSet.member?(connection_ids, &1.connection_id))

    cond do
      bound_connections == [] ->
        resource_connection_health(resources)

      Enum.any?(bound_connections ++ connected_resources, &(&1.status in ["degraded", "offline"])) ->
        "degraded"

      Enum.all?(bound_connections ++ connected_resources, &(&1.status == "healthy")) ->
        "healthy"

      true ->
        "unknown"
    end
  end

  defp resource_connection_health([]), do: "unknown"

  defp resource_connection_health(resources) do
    cond do
      Enum.any?(resources, &(&1.status in ["degraded", "offline"])) -> "degraded"
      Enum.all?(resources, &(&1.status == "healthy")) -> "healthy"
      true -> "unknown"
    end
  end

  defp runtime_health([]), do: "offline"

  defp runtime_health(runtimes) do
    if Enum.any?(runtimes, &(&1.status == "online")), do: "healthy", else: "offline"
  end

  defp execution_health(projections) do
    states = Enum.map(projections, & &1.execution.task.state)

    cond do
      Enum.any?(states, &(&1 == "failed")) -> "fault"
      Enum.any?(states, &(&1 in ["waiting_for_input", "paused"])) -> "waiting"
      Enum.any?(states, &(&1 in @active_states)) -> "active"
      true -> "idle"
    end
  end

  defp synchronization_health([]), do: "unknown"

  defp synchronization_health(resources) do
    statuses = Enum.map(resources, & &1.sync_status)

    cond do
      Enum.any?(statuses, &(&1 in ["failed", "stale"])) -> "attention"
      Enum.any?(statuses, &(&1 == "syncing")) -> "syncing"
      Enum.all?(statuses, &(&1 == "synced")) -> "synced"
      true -> "unknown"
    end
  end

  defp value(map, key) when is_map(map), do: Map.get(map, key) || Map.get(map, String.to_atom(key))

  defp value(_map, _key), do: nil

  defp maybe_put(map, _key, nil), do: map
  defp maybe_put(map, _key, ""), do: map
  defp maybe_put(map, key, value), do: Map.put(map, key, value)

  defp present?(value), do: is_binary(value) and String.trim(value) != ""

  defp http_url?(value) when is_binary(value) do
    case URI.parse(value) do
      %URI{scheme: scheme, host: host}
      when scheme in ["http", "https"] and is_binary(host) and host != "" ->
        true

      _uri ->
        false
    end
  end

  defp http_url?(_value), do: false
  defp valid_optional_url(value), do: if(http_url?(value), do: value)
end
