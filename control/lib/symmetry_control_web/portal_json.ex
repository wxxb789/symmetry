defmodule SymmetryControlWeb.PortalJSON do
  @moduledoc false

  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Workspaces.ProjectResource
  alias SymmetryControlWeb.Protocol

  def workspace(model) do
    snapshot = model.snapshot

    %{
      selected_project_id: snapshot.selected_project && snapshot.selected_project.id,
      projects: Enum.map(snapshot.projects, &project(&1, model.projections)),
      runtimes: Enum.map(model.runtimes, &Protocol.runtime/1),
      registered_runtimes: Enum.map(model.registered_runtimes, &Protocol.runtime/1),
      activity: Enum.map(model.activity, &activity_entry/1),
      health: model.health
    }
  end

  def project(project, projections \\ %{}) do
    %{
      id: project.id,
      key: project.key,
      name: project.name,
      description: project.description,
      status: project.status,
      version: project.lock_version,
      default_agent_profile: project.default_agent_profile,
      default_workspace: project.default_workspace,
      resources: Enum.map(loaded(project.resources), &resource/1),
      work_items: Enum.map(loaded(project.work_items), &work_item(&1, projections))
    }
  end

  def resource(resource) do
    %{
      id: resource.id,
      project_id: resource.project_id,
      kind: resource.kind,
      name: resource.name,
      provider: resource.provider,
      external_ref: resource.external_ref,
      url: resource.url,
      status: resource.status,
      sync_status: resource.sync_status,
      status_message: resource.status_message,
      metadata: resource.metadata,
      last_checked_at: iso8601(resource.last_checked_at),
      last_synced_at: iso8601(resource.last_synced_at),
      version: resource.lock_version
    }
  end

  def work_item(work_item, projections \\ %{}) do
    projection = Map.get(projections, work_item.orchestration_task_id)
    evidence = projection && projection.evidence
    delivery = delivery(work_item, evidence)
    execution = projection |> execution_summary() |> retry_capability(work_item)

    %{
      id: work_item.id,
      key: "#{work_item.project.key}-#{work_item.number}",
      project_id: work_item.project_id,
      title: work_item.title,
      description: work_item.description,
      status: work_item.status,
      priority: work_item.priority,
      position: work_item.position,
      assignee: owner(work_item),
      workspace: work_item.workspace,
      blocked: work_item.blocked,
      blocker: work_item.blocker,
      repository_resource_id: work_item.repository_resource_id,
      repository: repository_label(work_item),
      branch: work_item.branch,
      pull_request_url: delivery.pull_request.url,
      ci_status: delivery.ci.status,
      review_status: delivery.review.status,
      delivery: delivery,
      version: work_item.lock_version,
      can_start:
        is_nil(work_item.orchestration_task_id) and work_item.assignee_type == "agent" and
          work_item.status in ["backlog", "ready"] and project_active?(work_item.project),
      execution: execution,
      updated_at: iso8601(work_item.updated_at)
    }
  end

  def work_item_detail(
        %{work_item: work_item, task: task, timeline: timeline} = snapshot,
        projection
      ) do
    task_snapshot = (projection && projection.execution) || task
    task_dto = task_snapshot && Protocol.task(task_snapshot)
    raw_page = timeline_page(task_snapshot, timeline, Map.get(snapshot, :next_before))
    evidence = projection && projection.evidence
    delivery = delivery(work_item, evidence)

    %{
      work_item: work_item(work_item, projection_map(work_item, projection)),
      outcome: outcome(work_item, task_snapshot, raw_page.items, evidence, delivery),
      timeline: meaningful_timeline(raw_page.items),
      raw: %{
        task: task_dto,
        timeline: raw_page.items,
        next_before: raw_page.next_before
      }
    }
  end

  def execution(nil), do: nil
  def execution(task), do: execution_summary(task)

  def changeset_errors(changeset) do
    Ecto.Changeset.traverse_errors(changeset, fn {message, options} ->
      Regex.replace(~r"%{(\w+)}", message, fn _, key ->
        options |> Keyword.get(String.to_existing_atom(key), key) |> to_string()
      end)
    end)
  end

  defp activity_entry(entry) do
    projection = %{
      entry.execution.task.id => %{execution: entry.execution, evidence: entry.evidence}
    }

    item = work_item(entry.work_item, projection)

    %{
      work_item: %{
        id: item.id,
        key: item.key,
        title: item.title,
        status: item.status,
        priority: item.priority,
        blocked: item.blocked
      },
      execution: item.execution,
      delivery: item.delivery,
      runtime: entry.runtime && Protocol.runtime(entry.runtime)
    }
  end

  defp projection_map(_work_item, nil), do: %{}
  defp projection_map(work_item, projection), do: %{work_item.orchestration_task_id => projection}

  defp execution_summary(nil), do: nil
  defp execution_summary(%{execution: execution}), do: execution_summary(execution)

  defp execution_summary(%Task{} = task), do: execution_summary(%{task: task, run: nil})

  defp execution_summary(%{task: task_record, run: run} = snapshot) do
    task = Protocol.task(snapshot)

    %{
      task_id: task.task_id,
      run_id: task.run_id,
      runtime_id: run && run.runtime_id,
      generation: task.generation,
      state: task.state,
      waiting: task.waiting,
      latest_command: task.latest_command,
      result: task.result,
      failure: task.failure,
      timing: execution_timing(task_record, run),
      can_cancel: task.state in ["queued", "assigned", "claimed", "running", "waiting_for_input"],
      can_retry: task.state in ["failed", "cancelled"],
      intent_locked: task.state not in ["failed", "cancelled"]
    }
  end

  defp retry_capability(nil, _work_item), do: nil

  defp retry_capability(execution, work_item) do
    Map.update!(execution, :can_retry, &(&1 and work_item.assignee_type == "agent"))
  end

  defp execution_timing(task, run) do
    started_at = run && (run.claimed_at || run.assigned_at)

    finished_at =
      if run && task.state in ["completed", "failed", "cancelled"],
        do: run.updated_at,
        else: nil

    %{
      state_since: iso8601(task.updated_at),
      started_at: iso8601(started_at),
      finished_at: iso8601(finished_at)
    }
  end

  defp outcome(work_item, nil, _timeline, _evidence, delivery) do
    %{
      goal: work_item.title,
      phase: work_item.status,
      owner: owner(work_item),
      summary: nil,
      findings: [],
      changed_artifacts: [],
      tests: [],
      blocker: blocker(work_item, nil),
      pull_request_url: delivery.pull_request.url,
      ci_status: delivery.ci.status,
      review_status: delivery.review.status,
      delivery: delivery,
      result: nil,
      failure: nil
    }
  end

  defp outcome(work_item, task, _timeline, evidence, delivery) do
    result = task.task.result || (task.run && task.run.result) || %{}
    failure = task.task.failure || (task.run && task.run.failure)

    %{
      goal: task.task.goal || work_item.title,
      phase: task.task.state,
      owner: owner(work_item),
      summary: evidence_text(evidence, :summary) || value(result, "summary"),
      findings: evidence_list(evidence, :findings, list_value(result, "findings")),
      changed_artifacts:
        evidence_list(
          evidence,
          :artifacts,
          list_value(result, "changed_artifacts", "changed_files")
        ),
      tests: evidence_list(evidence, :tests, value(result, "tests") || []),
      blocker: blocker(work_item, task.waiting),
      pull_request_url: delivery.pull_request.url,
      ci_status: delivery.ci.status,
      review_status: delivery.review.status,
      delivery: delivery,
      result: result,
      failure: failure
    }
  end

  defp delivery(work_item, evidence) do
    %{
      pull_request:
        effective_delivery(
          work_item.pull_request_url,
          evidence && evidence.pull_request,
          :url,
          nil
        ),
      ci: effective_delivery(work_item.ci_status, evidence && evidence.ci, :status, "unknown"),
      review:
        effective_delivery(work_item.review_status, evidence && evidence.review, :status, "none")
    }
  end

  defp effective_delivery(manual_value, _agent_value, field, fallback)
       when is_binary(manual_value) and manual_value != "" do
    %{value: manual_value, source: "manual", generation: nil}
    |> Map.put(field, manual_value)
    |> normalize_delivery(fallback)
  end

  defp effective_delivery(_manual_value, agent_value, field, fallback) when is_map(agent_value) do
    value = Map.get(agent_value, field) || fallback

    agent_value
    |> Map.put(:value, value)
    |> Map.put_new(:source, "agent")
    |> Map.put_new(field, value)
    |> normalize_delivery(fallback)
  end

  defp effective_delivery(_manual_value, _agent_value, :url, _fallback),
    do: %{value: nil, url: nil, status: nil, source: "none", generation: nil}

  defp effective_delivery(_manual_value, _agent_value, :status, fallback),
    do: %{value: fallback, url: nil, status: fallback, source: "none", generation: nil}

  defp normalize_delivery(delivery, fallback) do
    delivery
    |> Map.put_new(:generation, nil)
    |> Map.put_new(:url, nil)
    |> Map.put_new(:status, fallback)
  end

  defp repository_label(work_item) do
    case work_item.repository_resource do
      %ProjectResource{} = resource -> resource.external_ref || resource.name
      _not_loaded_or_missing -> work_item.repository
    end
  end

  defp evidence_text(nil, _field), do: nil

  defp evidence_text(evidence, field) do
    case Map.get(evidence, field) do
      %{text: text} -> text
      _other -> nil
    end
  end

  defp evidence_list(nil, _field, fallback), do: fallback

  defp evidence_list(evidence, field, fallback) do
    case Map.get(evidence, field) do
      values when is_list(values) and values != [] -> values
      _other -> fallback
    end
  end

  defp owner(work_item) do
    %{
      type: work_item.assignee_type,
      name: work_item.assignee_name,
      agent_profile: work_item.agent_profile
    }
  end

  defp blocker(%{blocked: true, blocker: blocker}, _waiting), do: blocker
  defp blocker(_work_item, %{question: question}) when is_binary(question), do: question
  defp blocker(_work_item, _waiting), do: nil

  defp timeline_page(nil, _timeline, _next_before), do: %{items: [], next_before: nil}

  defp timeline_page(task, timeline, next_before) do
    Protocol.timeline_page(task.task.id, %{entries: timeline, next_before: next_before})
  end

  defp meaningful_timeline(items) do
    Enum.filter(items, fn
      %{source: "event", data: %{kind: kind}} ->
        kind in [
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

      %{source: source} ->
        source in ["transition", "command"]

      _other ->
        false
    end)
  end

  defp loaded(%Ecto.Association.NotLoaded{}), do: []
  defp loaded(values), do: values

  defp project_active?(%{status: "active"}), do: true
  defp project_active?(_project), do: false

  defp value(map, "summary") when is_map(map),
    do: Map.get(map, "summary") || Map.get(map, :summary)

  defp value(map, "message") when is_map(map),
    do: Map.get(map, "message") || Map.get(map, :message)

  defp value(map, "question") when is_map(map),
    do: Map.get(map, "question") || Map.get(map, :question)

  defp value(map, "findings") when is_map(map),
    do: Map.get(map, "findings") || Map.get(map, :findings)

  defp value(map, "changed_artifacts") when is_map(map),
    do: Map.get(map, "changed_artifacts") || Map.get(map, :changed_artifacts)

  defp value(map, "changed_files") when is_map(map),
    do: Map.get(map, "changed_files") || Map.get(map, :changed_files)

  defp value(map, "tests") when is_map(map), do: Map.get(map, "tests") || Map.get(map, :tests)
  defp value(_map, _key), do: nil

  defp list_value(map, primary, fallback \\ nil) do
    value = value(map, primary) || (fallback && value(map, fallback))
    if is_list(value), do: value, else: []
  end

  defp iso8601(nil), do: nil
  defp iso8601(%DateTime{} = value), do: DateTime.to_iso8601(value)
  defp iso8601(%NaiveDateTime{} = value), do: NaiveDateTime.to_iso8601(value)
end
