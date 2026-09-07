defmodule SymmetryControl.Chat.ReadModel do
  @moduledoc false

  import Ecto.Query

  alias SymmetryControl.Orchestration.{Command, RunEvent, RunTransition, Task}
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces.ReadModel, as: WorkReadModel
  alias SymmetryControl.Workspaces.WorkItem
  alias SymmetryControlWeb.PortalJSON

  @semantic_kinds ~w(progress summary finding rationale artifact test pull_request ci review waiting_for_input)
  @run_limit 20
  @timeline_limit 50

  def runs(scope, work_item_id \\ nil) do
    items = scoped_items(scope, work_item_id)
    projections = if scope.kind == "run", do: %{}, else: WorkReadModel.projections(items)

    projected_items =
      Enum.map(items, fn item ->
        projection =
          if scope.kind == "run",
            do: run_projection(item, scope.run),
            else: Map.get(projections, item.orchestration_task_id)

        {item, projection}
      end)

    runs =
      projected_items
      |> Enum.flat_map(fn
        {_item, %{execution: %{run: %{id: id} = run}}} -> [{id, run}]
        _ -> []
      end)
      |> Map.new()

    timelines = semantic_timelines(runs)

    Enum.map(projected_items, fn {item, projection} ->
      execution = projection && projection.execution
      run = execution && execution.run
      timeline = if run, do: Map.get(timelines, run.id, []), else: []

      snapshot = %{work_item: item, task: execution, timeline: timeline, next_before: nil}
      detail = PortalJSON.work_item_detail(snapshot, projection)

      # The work detail endpoint remains the diagnostic surface for raw events.
      detail = put_in(detail, [:raw, :timeline], [])

      if projection && Map.get(projection, :historical, false) do
        execution =
          Map.merge(detail.work_item.execution, %{
            can_cancel: false,
            can_retry: false,
            can_guide: false,
            can_pause: false,
            can_resume: false,
            historical: true
          })

        put_in(detail, [:work_item, :execution], execution)
      else
        detail
      end
    end)
  end

  def context_reply([], _intent, _content),
    do:
      "There is no work in this context yet. Choose Start work to send a goal to an autonomous worker."

  def context_reply(runs, intent, content) do
    topics = question_topics(intent, content)
    details = Enum.map_join(Enum.take(runs, 5), "\n\n", &run_context(&1, topics))
    String.slice("From the recorded work history:\n\n" <> details, 0, 32_000)
  end

  defp question_topics("status", _content), do: [:status]

  defp question_topics(_intent, content) do
    [
      {:rationale, ~r/\b(why|rationale|reason|trade.?off)\b/iu},
      {:tests, ~r/\b(tests?|tested|testing|validation|coverage|checks?)\b/iu},
      {:artifacts, ~r/\b(files?|artifacts?|changed|changes|modified)\b/iu},
      {:delivery, ~r/\b(pr|pull request|ci|review|delivery|merge)\b/iu},
      {:findings, ~r/\b(findings?|discovered|learned)\b/iu},
      {:outcome, ~r/\b(outcome|result|completed|finished|failed|failure)\b/iu}
    ]
    |> Enum.flat_map(fn {topic, pattern} ->
      if Regex.match?(pattern, content), do: [topic], else: []
    end)
    |> case do
      [] -> [:status]
      topics -> topics
    end
  end

  defp run_context(detail, topics) do
    item = detail.work_item
    execution = item.execution
    phase = if execution, do: execution.state, else: item.status
    attempt = if execution, do: " (attempt #{execution.generation})", else: ""
    evidence = Enum.map_join(topics, "\n", &topic_context(detail, &1))
    "#{item.key} — #{item.title}#{attempt}: #{phase}.\n" <> evidence
  end

  defp topic_context(detail, :status) do
    [
      topic_context(detail, :outcome),
      topic_context(detail, :findings),
      detail.outcome.blocker && "Decision or blocker: #{detail.outcome.blocker}",
      topic_context(detail, :delivery)
    ]
    |> Enum.reject(&is_nil/1)
    |> Enum.join("\n")
  end

  defp topic_context(detail, :rationale) do
    rationale =
      Enum.find_value(detail.timeline, fn
        %{source: "event", data: %{kind: kind, payload: payload}} ->
          present(value(payload, "rationale")) ||
            if(kind == "rationale",
              do: present(value(payload, "message") || value(payload, "summary"))
            )

        _ ->
          nil
      end)

    if rationale,
      do: "Recorded rationale: " <> String.slice(rationale, 0, 2_000),
      else: "No rationale has been recorded for this attempt."
  end

  defp topic_context(detail, :tests),
    do:
      evidence_list(
        "Recorded tests",
        detail.outcome.tests,
        ~w(name status summary),
        "No test results have been recorded for this attempt."
      )

  defp topic_context(detail, :artifacts),
    do:
      evidence_list(
        "Changed artifacts",
        detail.outcome.changed_artifacts,
        ~w(path name url),
        "No changed artifacts have been recorded for this attempt."
      )

  defp topic_context(detail, :findings),
    do:
      evidence_list(
        "Findings",
        detail.outcome.findings,
        ~w(message title),
        "No findings have been recorded for this attempt."
      )

  defp topic_context(detail, :delivery) do
    outcome = detail.outcome

    pull_request =
      if outcome.pull_request_url,
        do: "Pull request: #{outcome.pull_request_url}",
        else: "No pull request has been recorded."

    "#{pull_request}\nCI: #{outcome.ci_status}; review: #{outcome.review_status}."
  end

  defp topic_context(detail, :outcome) do
    summary =
      present(detail.outcome.summary) || latest_text(detail.timeline, ~w(summary progress))

    failure =
      present(value(detail.outcome.failure, "message")) ||
        present(value(detail.outcome.failure, "summary")) ||
        present(value(detail.outcome.failure, "error"))

    [
      if(summary,
        do: "Summary: " <> String.slice(summary, 0, 2_000),
        else: "No outcome summary has been recorded for this attempt."
      ),
      if(present(failure), do: "Failure: " <> String.slice(failure, 0, 2_000))
    ]
    |> Enum.reject(&is_nil/1)
    |> Enum.join("\n")
  end

  defp evidence_list(label, entries, fields, missing) do
    entries = if is_list(entries), do: entries, else: []

    text =
      entries
      |> Enum.take(5)
      |> Enum.flat_map(fn
        entry when is_binary(entry) ->
          [String.slice(entry, 0, 500)]

        entry when is_map(entry) ->
          parts = fields |> Enum.map(&present(value(entry, &1))) |> Enum.reject(&is_nil/1)
          if parts == [], do: [], else: [String.slice(Enum.join(parts, " — "), 0, 500)]

        _ ->
          []
      end)

    if text == [], do: missing, else: label <> ": " <> Enum.join(text, "; ")
  end

  defp scoped_items(scope, work_item_id) do
    query =
      from item in WorkItem,
        order_by: [desc: item.updated_at, desc: item.id],
        limit: ^@run_limit,
        preload: [:project, :repository_resource, :ci_resource, :external_work_item_resource]

    query =
      case scope.kind do
        "workspace" -> query
        "project" -> where(query, [item], item.project_id == ^scope.project_id)
        "run" -> where(query, [item], item.id == ^scope.work_item.id)
      end

    query = if work_item_id, do: where(query, [item], item.id == ^work_item_id), else: query
    Repo.all(query)
  end

  defp run_projection(item, run) do
    task = Repo.get!(Task, run.task_id)

    if task.attempt_generation == run.generation do
      Map.fetch!(WorkReadModel.projections([item]), task.id)
    else
      pinned_task = %{
        task
        | state: run.state,
          current_generation: run.generation,
          attempt_generation: run.generation,
          result: run.result,
          failure: run.failure,
          waiting_transition_id: nil,
          updated_at: run.updated_at,
          goal: "Historical run #{run.generation}"
      }

      latest_command =
        Repo.one(
          from command in Command,
            where: command.run_id == ^run.id,
            order_by: [desc: command.inserted_at, desc: command.id],
            limit: 1
        )

      %{
        execution: %{task: pinned_task, run: run, waiting: nil, latest_command: latest_command},
        evidence: WorkReadModel.evidence_for_run(run),
        historical: true
      }
    end
  end

  defp semantic_timelines(runs) when map_size(runs) == 0, do: %{}

  defp semantic_timelines(runs) do
    run_ids = Map.keys(runs)

    events =
      from(event in RunEvent, where: event.kind in ^@semantic_kinds)
      |> latest_per_run(run_ids)
      |> Enum.map(&timeline_entry(&1, "event", Map.fetch!(runs, &1.run_id)))

    transitions =
      latest_per_run(RunTransition, run_ids)
      |> Enum.map(&timeline_entry(&1, "transition", Map.fetch!(runs, &1.run_id)))

    commands =
      latest_per_run(Command, run_ids)
      |> Enum.map(&timeline_entry(&1, "command", Map.fetch!(runs, &1.run_id)))

    (events ++ transitions ++ commands)
    |> Enum.group_by(& &1.run_id)
    |> Map.new(fn {run_id, entries} ->
      {run_id,
       entries
       |> Enum.sort_by(&{DateTime.to_unix(&1.inserted_at, :microsecond), &1.id}, :desc)
       |> Enum.take(@timeline_limit)}
    end)
  end

  defp latest_per_run(query, run_ids) do
    ranked =
      from entry in query,
        where: entry.run_id in ^run_ids,
        select: %{
          id: entry.id,
          position:
            over(row_number(),
              partition_by: entry.run_id,
              order_by: [desc: entry.inserted_at, desc: entry.id]
            )
        }

    Repo.all(
      from entry in query,
        join: rank in subquery(ranked),
        on: rank.id == entry.id,
        where: rank.position <= ^@timeline_limit
    )
  end

  defp timeline_entry(record, source, run) do
    record |> Map.from_struct() |> Map.merge(%{source: source, generation: run.generation})
  end

  defp latest_text(timeline, kinds) do
    Enum.find_value(timeline, fn
      %{source: "event", data: %{kind: kind, payload: payload}} ->
        if kind in kinds,
          do:
            present(
              value(payload, "summary") || value(payload, "message") ||
                value(payload, "rationale")
            )

      _ ->
        nil
    end)
  end

  defp value(map, key) when is_map(map),
    do:
      Enum.find_value(map, fn {stored_key, value} ->
        if to_string(stored_key) == key, do: value
      end)

  defp value(_map, _key), do: nil
  defp present(value) when is_binary(value) and value != "", do: value
  defp present(_value), do: nil
end
