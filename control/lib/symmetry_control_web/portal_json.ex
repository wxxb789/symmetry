defmodule SymmetryControlWeb.PortalJSON do
  @moduledoc false

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.{ProjectResource, WorkItem, WorkItemEligibility}
  alias SymmetryControlWeb.Protocol

  def workspace(model) do
    snapshot = model.snapshot

    %{
      selected_project_id: snapshot.selected_project && snapshot.selected_project.id,
      projects: Enum.map(snapshot.projects, &project(&1, model.projections)),
      connections: connections(model),
      runtimes: Enum.map(model.runtimes, &Protocol.runtime/1),
      registered_runtimes: Enum.map(model.registered_runtimes, &Protocol.runtime/1),
      activity: Enum.map(model.activity, &activity_entry/1),
      health: model.health
    }
  end

  def connections(model) do
    Enum.map(model.connections, &connection(&1, Map.get(model.bound_resources, &1.id, [])))
  end

  def connections_read(model) do
    %{
      providers: Enum.map(model.providers, &provider/1),
      connections: connections(model)
    }
  end

  def connection(connection, bound_resources) do
    %{
      id: connection.id,
      provider: connection.provider,
      name: connection.name,
      account_ref: connection.account_ref,
      auth_type: connection.auth_type,
      capabilities: connection.capabilities,
      status: connection.status,
      status_message: connection.status_message,
      metadata: connection.metadata,
      last_checked_at: iso8601(connection.last_checked_at),
      version: connection.lock_version,
      resources: Enum.map(bound_resources, &connection_resource/1)
    }
  end

  def connection_resource(%ProjectResource{} = resource) do
    resource
    |> resource()
    |> Map.merge(%{
      project_key: resource.project.key,
      project_name: resource.project.name,
      project_status: resource.project.status
    })
  end

  defp provider(provider) do
    %{
      id: provider.id,
      auth_type: provider.auth_type,
      capabilities: provider.capabilities
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
      connection_id: resource.connection_id,
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

  @doc """
  The attention entry point: Goal attention merged with the project blocked items
  the workspace read already serves.

  `goal_result` is `%{page: Goals attention page}`. The wire's `unavailable`
  member stays (the frontend decodes it) but is always `null`: no read path
  produces an unavailable Goal feed, and a failed Goal read is answered as its
  mapped protocol status before this projection runs. Nothing here invents a
  Goal entry or hides an authority refusal.
  """
  def attention(selected_project_id, goal_result, workspace_result) do
    goal_page = goal_result.page

    %{
      selected_project_id: selected_project_id,
      goal_attention: %{
        entries: goal_page.entries,
        next_cursor: goal_page.next_cursor,
        unavailable: nil
      },
      project_blocked: project_blocked(workspace_result)
    }
  end

  defp project_blocked({:ok, model}) do
    entries =
      model.snapshot.projects
      |> Enum.flat_map(&blocked_work_items/1)

    %{entries: entries, unavailable: nil}
  end

  defp project_blocked({:error, reason}) do
    {_status, code, message} = Protocol.error_details(reason)
    %{entries: [], unavailable: %{code: code, message: message}}
  end

  # The workspace snapshot hydrates work items for the selected project only, so
  # an unloaded association means "not read", never "no blocked work".
  defp blocked_work_items(%{work_items: %Ecto.Association.NotLoaded{}}), do: []

  defp blocked_work_items(%{work_items: work_items} = project) when is_list(work_items) do
    work_items
    |> Enum.filter(& &1.blocked)
    |> Enum.map(&project_blocked_entry(project, &1))
  end

  defp blocked_work_items(_project), do: []

  defp project_blocked_entry(project, work_item) do
    %{
      project_id: project.id,
      project_key: project.key,
      project_name: project.name,
      project_status: project.status,
      work_item_id: work_item.id,
      work_item_key: "#{project.key}-#{work_item.number}",
      title: work_item.title,
      status: work_item.status,
      blocked: work_item.blocked,
      blocker: work_item.blocker,
      updated_at: iso8601(work_item.updated_at)
    }
  end

  defp project_label(nil), do: {nil, nil}
  defp project_label(project), do: {project.key, project.name}

  @doc """
  Portal projection of one Goal read.

  Every authority member (allowed_actions, blockers, readiness) stays exactly
  what the durable projection serves; this function only adds the project label
  the surface renders, injects each decision's `version` from
  `decision_versions` (the durable projection serves none), and stamps the
  served timestamps.
  """
  def goal(projection, project \\ nil, decision_versions \\ %{}) do
    {project_key, project_name} = project_label(project)

    Map.merge(projection, %{
      project_key: project_key,
      project_name: project_name,
      updated_at: iso8601(Map.get(projection, :updated_at)),
      revision: goal_revision(Map.get(projection, :revision)),
      decisions:
        Enum.map(Map.get(projection, :decisions, []), &goal_decision(decision_versions, &1)),
      accepted_outcomes: Enum.map(Map.get(projection, :accepted_outcomes, []), &goal_outcome/1),
      external_waits: Enum.map(Map.get(projection, :external_waits, []), &goal_external_wait/1)
    })
  end

  defp goal_revision(nil), do: nil

  defp goal_revision(revision),
    do: %{revision | inserted_at: iso8601(revision.inserted_at)}

  defp goal_decision(decision_versions, decision) do
    Map.merge(decision, %{
      version: Map.get(decision_versions, Map.get(decision, :id)),
      inserted_at: iso8601(Map.get(decision, :inserted_at)),
      updated_at: iso8601(Map.get(decision, :updated_at)),
      expires_at: iso8601(Map.get(decision, :expires_at))
    })
  end

  defp goal_outcome(outcome), do: %{outcome | inserted_at: iso8601(outcome.inserted_at)}

  defp goal_external_wait(wait),
    do: %{
      wait
      | next_check_at: iso8601(wait.next_check_at),
        inserted_at: iso8601(wait.inserted_at),
        updated_at: iso8601(wait.updated_at)
    }

  def work_item(work_item, projections \\ %{}) do
    projection = Map.get(projections, work_item.orchestration_task_id)
    evidence = projection && projection.evidence
    delivery = delivery(work_item, evidence)
    goal_managed = WorkItem.goal_managed?(work_item)
    execution = projection |> execution_summary() |> legacy_capabilities(work_item)

    %{
      id: work_item.id,
      key: "#{work_item.project.key}-#{work_item.number}",
      project_id: work_item.project_id,
      goal_id: work_item.goal_id,
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
      ci_resource_id: work_item.ci_resource_id,
      ci_resource: resource_label(work_item.ci_resource),
      branch: work_item.branch,
      pull_request_url: delivery.pull_request.url,
      ci_status: delivery.ci.status,
      review_status: delivery.review.status,
      delivery: delivery,
      external: external_work_item(work_item),
      goal_managed: goal_managed,
      version: work_item.lock_version,
      can_start: WorkItemEligibility.start_allowed?(work_item),
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

  # The work-item-less task view: control affordances only, no retry claim.
  def execution(nil), do: nil
  def execution(task), do: execution_summary(task)

  @doc """
  The command responses' execution summary: the same projection with the work
  item's own capability gates applied, so a response serves the `can_cancel`
  and `can_retry` the item's read would rather than state-only ones.
  Eligibility resolves the work item's corrected binding
  (`Workspaces.retry_binding/2`, including the attempt's supervisory
  requirement), so a re-bound work item answers the response exactly as it
  answers the read. A bare task snapshot claims no retry (`execution/1`), and a
  work item without its project and provider resources loaded answers those
  gates false.
  """
  def execution(nil, _work_item), do: nil
  def execution(task, nil), do: execution(task)

  def execution(task, work_item) do
    case execution_summary(task) do
      nil ->
        nil

      execution ->
        execution
        |> Map.put(:can_retry, work_item_retry_runtime_eligible?(task, work_item))
        |> legacy_capabilities(work_item)
    end
  end

  # The re-admission a retry would apply: the work item's corrected binding with
  # the current attempt's supervisory requirement, for a goal-less terminal
  # attempt. A live attempt answers without a query.
  defp work_item_retry_runtime_eligible?(%Task{} = task, %WorkItem{} = work_item) do
    Orchestration.retryable_attempt?(task) and
      Orchestration.retry_runtime_eligible?(Workspaces.retry_binding_for(task, work_item))
  end

  defp work_item_retry_runtime_eligible?(%{task: %Task{} = task}, %WorkItem{} = work_item),
    do: work_item_retry_runtime_eligible?(task, work_item)

  defp work_item_retry_runtime_eligible?(_task, _work_item), do: false

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

  # `can_cancel` joins the state permission with the run's advertised adapter
  # `cancel` operation whenever a runtime holds the attempt (claimed and
  # beyond): that runtime is the one that has to stop the process. A cancel of
  # an attempt no runtime holds yet (queued, assigned) is applied by the
  # control plane itself, so no runtime operation is required. `can_retry`
  # re-queues a terminal attempt, and a retried attempt starts fresh, so it
  # needs some runtime assignment could select for the task that advertises
  # `start` — the previous attempt's runtime is irrelevant. That eligibility is
  # a workspace-read projection field, resolved from the work item there and by
  # `execution/2`; a snapshot without it (the work-item-less task view) claims
  # no retry. Unknown runtime support fails closed: the served read never
  # offers an operation no runtime could perform. The work item's own fences —
  # the Goal refusal of `can_cancel` and the retry conditions — are applied by
  # `legacy_capabilities/2` after this summary.
  defp execution_summary(%{task: task_record, run: run} = snapshot) do
    task = Protocol.task(snapshot)
    runtime = Map.get(snapshot, :runtime)

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
      supervisory_control: task.controls.supervisory_control,
      can_guide: task.controls.can_guide,
      can_pause: task.controls.can_pause,
      can_resume: task.controls.can_resume,
      can_cancel:
        task.state in ["queued", "assigned"] or
          (task.state in ["claimed", "running", "paused", "waiting_for_input"] and
             Orchestration.adapter_operation_supported?(runtime, :cancel)),
      can_retry:
        Orchestration.retryable_attempt?(task_record) and
          Map.get(snapshot, :retry_runtime_eligible, false),
      intent_locked: task.state not in ["failed", "cancelled"]
    }
  end

  # The execution members only the work item decides, resolved by
  # `WorkItemEligibility` — the same verdicts the commands enforce.
  # `can_cancel` is withheld from every Goal-managed item in every state: the
  # legacy cancel path refuses that item (`ensure_legacy_work_item/1` applies
  # `WorkItemEligibility.cancel_gate/1`), and a Goal is the authority that stops
  # its work. `can_retry` joins the attempt's own gates with the work item's
  # agent-assignment, external-work and active-project conditions.
  defp legacy_capabilities(nil, _work_item), do: nil

  defp legacy_capabilities(execution, work_item) do
    execution
    |> Map.update!(:can_cancel, &(&1 and WorkItemEligibility.cancel_allowed?(work_item)))
    |> Map.update!(:can_retry, &(&1 and WorkItemEligibility.retry_allowed?(work_item)))
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

  # The three delivery members resolve the same way — a provider-owned value
  # wins over the operator's manual value, which wins over the attempt's agent
  # evidence, and with none of the three the member serves its own fallback —
  # though only the provider branch adds `provider`/`recorded_at` and only the
  # agent branch carries the evidence map's own members; manual and none serve
  # the same five. The descriptor names that member's three inputs, the field
  # they compete for, the fallback the none branch serves, and the provider
  # metadata the provider branch serves.
  defp delivery(work_item, evidence) do
    %{
      pull_request:
        work_item
        |> delivery_descriptor(:pull_request, evidence)
        |> resolve_delivery()
        |> provider_pull_request_state(work_item),
      ci: work_item |> delivery_descriptor(:ci, evidence) |> resolve_delivery(),
      review: work_item |> delivery_descriptor(:review, evidence) |> resolve_delivery()
    }
  end

  defp delivery_descriptor(work_item, :pull_request, evidence) do
    %{
      provider_value: work_item.external_pull_request_url,
      manual_value: work_item.pull_request_url,
      agent: evidence && evidence.pull_request,
      field: :url,
      fallback: nil,
      provider: change_provider(work_item),
      recorded_at: work_item.external_change_updated_at
    }
  end

  defp delivery_descriptor(work_item, :ci, evidence) do
    %{
      provider_value: work_item.external_ci_status,
      manual_value: work_item.ci_status,
      agent: evidence && evidence.ci,
      field: :status,
      fallback: "unknown",
      provider: ci_provider(work_item),
      recorded_at: work_item.external_ci_updated_at
    }
  end

  defp delivery_descriptor(work_item, :review, evidence) do
    %{
      provider_value: work_item.external_review_status,
      manual_value: work_item.review_status,
      agent: evidence && evidence.review,
      field: :status,
      fallback: "none",
      provider: change_provider(work_item),
      recorded_at: work_item.external_change_updated_at
    }
  end

  # An empty provider value means "never synchronized", so it falls through to
  # the manual value exactly as an absent one does.
  defp resolve_delivery(%{provider_value: provider_value} = descriptor)
       when is_binary(provider_value) and provider_value != "" do
    %{
      value: provider_value,
      source: "provider",
      provider: descriptor.provider,
      generation: nil,
      recorded_at: iso8601(descriptor.recorded_at)
    }
    |> Map.put(descriptor.field, provider_value)
    |> normalize_delivery(descriptor.fallback)
  end

  # The manual branch serves neither `provider` nor `recorded_at`: those members
  # exist only where a provider owns the value.
  defp resolve_delivery(%{manual_value: manual_value} = descriptor)
       when is_binary(manual_value) and manual_value != "" do
    %{value: manual_value, source: "manual", generation: nil}
    |> Map.put(descriptor.field, manual_value)
    |> normalize_delivery(descriptor.fallback)
  end

  # The agent branch serves the whole evidence map: its own `source` (the
  # evidence's, not necessarily "agent") and every member the agent wrote.
  # `:value` is always replaced by the resolved value; `source`, the member's own
  # field, `generation`, `url` and `status` are added only where the map does not
  # already carry them.
  defp resolve_delivery(%{agent: agent} = descriptor) when is_map(agent) do
    value = Map.get(agent, descriptor.field) || descriptor.fallback

    agent
    |> Map.put(:value, value)
    |> Map.put_new(:source, "agent")
    |> Map.put_new(descriptor.field, value)
    |> normalize_delivery(descriptor.fallback)
  end

  # Nothing resolved: a `:url` member claims neither a value nor a status, a
  # `:status` member serves its fallback in both.
  defp resolve_delivery(%{field: :url}),
    do: %{value: nil, url: nil, status: nil, source: "none", generation: nil}

  defp resolve_delivery(%{field: :status, fallback: fallback}),
    do: %{value: fallback, url: nil, status: fallback, source: "none", generation: nil}

  defp normalize_delivery(delivery, fallback) do
    delivery
    |> Map.put_new(:generation, nil)
    |> Map.put_new(:url, nil)
    |> Map.put_new(:status, fallback)
  end

  defp external_work_item(%{external_work_item_resource_id: nil}), do: nil

  defp external_work_item(work_item) do
    %{
      provider: work_item.external_provider,
      id: work_item.external_id,
      url: work_item.external_url,
      state: work_item.external_state,
      available: work_item.external_available,
      assignee: work_item.external_assignee_name,
      labels: work_item.labels,
      updated_at: iso8601(work_item.external_updated_at),
      resource_id: work_item.external_work_item_resource_id,
      data: work_item.external_data,
      delivery_data: %{
        change: work_item.external_change_data,
        ci: work_item.external_ci_data
      },
      provider_owned_fields: ["title", "description", "priority", "state", "labels", "assignee"]
    }
  end

  defp repository_label(work_item) do
    case work_item.repository_resource do
      %ProjectResource{} = resource -> resource.external_ref || resource.name
      _not_loaded_or_missing -> work_item.repository
    end
  end

  defp resource_label(%ProjectResource{} = resource), do: resource.external_ref || resource.name
  defp resource_label(_not_loaded_or_missing), do: nil

  defp provider_pull_request_state(%{source: "provider"} = delivery, work_item),
    do: Map.put(delivery, :status, work_item.external_pull_request_state || "unknown")

  defp provider_pull_request_state(delivery, _work_item), do: delivery

  defp change_provider(work_item) do
    case work_item.repository_resource do
      %ProjectResource{provider: provider} when is_binary(provider) -> provider
      _not_loaded_or_missing -> work_item.external_provider
    end
  end

  defp ci_provider(work_item) do
    case work_item.ci_resource do
      %ProjectResource{provider: provider} when is_binary(provider) -> provider
      _not_loaded_or_missing -> change_provider(work_item)
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
          "rationale",
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

  # The getter answers the two spellings a key can arrive under: the string is
  # the wire spelling (the attempt's result JSON, deep-stringified before it is
  # stored and string-keyed again after the JSONB round trip), the atom arm is a
  # legacy fallback that no producer in this revision reaches. One table names
  # both, so a new evidence field is a word here instead of another clause, and
  # the atoms are this module's own literals (never built from a served key).
  @evidence_keys ~w[summary message question findings changed_artifacts changed_files tests]a
  @evidence_key_strings Map.new(@evidence_keys, &{Atom.to_string(&1), &1})

  defp value(map, key) when is_map(map) do
    case Map.fetch(@evidence_key_strings, key) do
      {:ok, atom_key} -> Map.get(map, key) || Map.get(map, atom_key)
      :error -> nil
    end
  end

  defp value(_map, _key), do: nil

  defp list_value(map, primary, fallback \\ nil) do
    value = value(map, primary) || (fallback && value(map, fallback))
    if is_list(value), do: value, else: []
  end

  defp iso8601(nil), do: nil
  defp iso8601(%DateTime{} = value), do: DateTime.to_iso8601(value)
  defp iso8601(%NaiveDateTime{} = value), do: NaiveDateTime.to_iso8601(value)
end
