defmodule SymmetryControl.Workspaces.ReadModel do
  @moduledoc false

  import Ecto.Query

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Command, Run, RunEvent, RunTransition, Task}
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces

  @active_states ["queued", "assigned", "claimed", "running", "cancelling"]
  @semantic_kinds [
    "progress",
    "summary",
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
      work_items = if project, do: project.work_items, else: []
      projections = projections(work_items)
      compatible_runtimes = compatible_runtimes(project, runtimes)

      {:ok,
       %{
         snapshot: snapshot,
         runtimes: compatible_runtimes,
         registered_runtimes: runtimes,
         projections: projections,
         activity: activity(work_items, projections, compatible_runtimes),
         health: health(project, projections, compatible_runtimes)
       }}
    end
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
      commands = latest_commands(task_ids)
      waiting = waiting_contexts(tasks, runs)
      evidence = evidence(task_ids)

      Map.new(task_ids, fn task_id ->
        task = Map.fetch!(tasks, task_id)

        {task_id,
         %{
           execution: %{
             task: task,
             run: Map.get(runs, task_id),
             waiting: Map.get(waiting, task_id),
             latest_command: Map.get(commands, task_id)
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

  defp current_runs(task_ids) do
    Repo.all(
      from run in Run,
        join: task in Task,
        on: task.id == run.task_id and task.attempt_generation == run.generation,
        where: task.id in ^task_ids,
        select: run
    )
    |> Map.new(&{&1.task_id, &1})
  end

  defp latest_commands(task_ids) do
    Repo.all(
      from command in Command,
        where: command.task_id in ^task_ids,
        distinct: command.task_id,
        order_by: [asc: command.task_id, desc: command.inserted_at, desc: command.id]
    )
    |> Map.new(&{&1.task_id, &1})
  end

  defp waiting_contexts(tasks, runs) do
    waiting_runs =
      tasks
      |> Enum.flat_map(fn {task_id, task} ->
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
        Repo.all(
          from transition in RunTransition,
            where:
              transition.transition_id in ^transition_ids and
                transition.state == "waiting_for_input"
        )
        |> Map.new(&{&1.transition_id, &1})

      events =
        Repo.all(
          from event in RunEvent,
            where: event.event_id in ^transition_ids and event.kind == "waiting_for_input"
        )
        |> Map.new(&{&1.event_id, &1})

      Map.new(waiting_runs, fn run ->
        transition_id = Map.fetch!(tasks, run.task_id).waiting_transition_id
        transition = Map.get(transitions, transition_id)
        event = Map.get(events, transition_id)
        payload = if event, do: event.payload, else: transition && transition.payload
        recorded_at = if event, do: event.inserted_at, else: transition && transition.inserted_at

        {run.task_id,
         if transition do
           %{
             run_id: run.id,
             generation: run.generation,
             transition_id: transition.transition_id,
             question: value(payload, "question"),
             payload: payload || %{},
             recorded_at: recorded_at
           }
         end}
      end)
    end
  end

  defp evidence(task_ids) do
    Repo.all(
      from event in RunEvent,
        join: run in Run,
        on: run.id == event.run_id,
        join: task in Task,
        on: task.id == run.task_id and task.attempt_generation == run.generation,
        where: task.id in ^task_ids and event.kind in ^@semantic_kinds,
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
      [
        {project.default_agent_profile, project.default_workspace}
        | agent_bindings(project.work_items)
      ]
      |> MapSet.new()

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
  defp activity_rank(state) when state in @active_states, do: 1
  defp activity_rank("failed"), do: 2
  defp activity_rank(_state), do: 3

  defp health(nil, _projections, _runtimes) do
    %{connections: "unknown", runtimes: "offline", executions: "idle", synchronization: "unknown"}
  end

  defp health(project, projections, runtimes) do
    external_resources =
      Enum.filter(
        project.resources,
        &(&1.kind in ["repository", "work_tracking", "ci", "connection"])
      )

    %{
      connections: connection_health(external_resources),
      runtimes: runtime_health(runtimes),
      executions: execution_health(Map.values(projections)),
      synchronization: synchronization_health(external_resources)
    }
  end

  defp connection_health([]), do: "unknown"

  defp connection_health(resources) do
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
      Enum.any?(states, &(&1 == "waiting_for_input")) -> "waiting"
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

  defp value(map, key) when is_map(map),
    do: Map.get(map, key) || Map.get(map, String.to_atom(key))

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
