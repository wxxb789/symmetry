defmodule SymmetryControl.WorkspacesReadModelTest do
  use SymmetryControl.DataCase, async: false

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.ReadModel
  alias SymmetryControlWeb.PortalJSON

  @now ~U[2026-09-04 10:00:00.000000Z]

  test "workspace projection joins compatible runtime, current execution evidence, and distinct health" do
    project = project_fixture()

    assert {:ok, _connection} =
             Workspaces.create_resource(project.id, %{
               kind: "connection",
               name: "GitHub",
               status: "healthy",
               sync_status: "synced"
             })

    assert {:ok, _ci} =
             Workspaces.create_resource(project.id, %{
               kind: "ci",
               name: "Actions",
               status: "healthy",
               sync_status: "failed",
               status_message: "Webhook delayed"
             })

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Ship outcome projection",
               status: "ready",
               assignee_type: "agent",
               agent_profile: "codex"
             })

    assert {:ok, launched, :created} = Workspaces.launch_work_item(item.id)
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)
    transition(run.id, fence, "running", 1)

    append_events(run.id, fence, [
      event(2, 1, "progress", %{"message" => "Implementing portal"}),
      event(3, 2, "finding", %{"message" => "Fixed stale response race", "severity" => "high"}),
      event(4, 3, "artifact", %{"path" => "control/priv/portal_assets/login.css"}),
      event(5, 4, "test", %{"name" => "portal workflow", "status" => "passed"}),
      event(6, 5, "pull_request", %{"url" => "https://github.com/acme/symmetry/pull/42"}),
      event(7, 6, "ci", %{"status" => "passed"}),
      event(8, 7, "review", %{"status" => "required"}),
      event(9, 8, "summary", %{"summary" => "Portal workflow is ready for review"})
    ])

    assert {:ok, model} = ReadModel.workspace(project.id)
    projection = Map.fetch!(model.projections, launched.task.task.id)

    assert Enum.map(model.runtimes, & &1.runtime_id) == [runtime.id]
    assert projection.execution.task.state == "running"
    assert projection.execution.run.id == run.id
    assert projection.evidence.summary.text == "Portal workflow is ready for review"
    assert Enum.map(projection.evidence.findings, & &1.message) == ["Fixed stale response race"]

    assert Enum.map(projection.evidence.artifacts, & &1.path) == [
             "control/priv/portal_assets/login.css"
           ]

    assert Enum.map(projection.evidence.tests, &{&1.name, &1.status}) == [
             {"portal workflow", "passed"}
           ]

    assert projection.evidence.pull_request.url == "https://github.com/acme/symmetry/pull/42"
    assert projection.evidence.ci.status == "passed"
    assert projection.evidence.review.status == "required"
    assert model.health.connections == "healthy"
    assert model.health.synchronization == "attention"
    assert model.health.runtimes == "healthy"
    assert model.health.executions == "active"

    execution =
      model
      |> PortalJSON.workspace()
      |> then(fn workspace -> hd(workspace.activity).execution end)

    assert execution.timing.state_since ==
             DateTime.to_iso8601(projection.execution.task.updated_at)

    assert execution.timing.started_at == DateTime.to_iso8601(@now)
    assert execution.timing.finished_at == nil
  end

  test "retry hides prior-generation delivery evidence from the current projection" do
    project = project_fixture()

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Retry projection",
               status: "ready",
               assignee_type: "agent",
               agent_profile: "codex"
             })

    assert {:ok, first, :created} = Workspaces.launch_work_item(item.id)
    %{machine: machine} = enroll_machine("retry-machine")
    runtime = register_runtime(machine, "retry-runtime")
    assert {:ok, first_run} = Orchestration.assign_one(now: @now)
    fence = claim(first_run, runtime)
    transition(first_run.id, fence, "running", 20)
    append_events(first_run.id, fence, [event(21, 1, "ci", %{"status" => "failed"})])
    transition(first_run.id, fence, "failed", 22)

    assert {:ok, _retried, :created} =
             Workspaces.retry_work_item(item.id, 1, "retry-projection")

    assert {:ok, queued_model} = ReadModel.workspace(project.id)
    queued = Map.fetch!(queued_model.projections, first.task.task.id)
    assert queued.execution.task.state == "queued"
    assert queued.execution.task.attempt_generation == 2
    assert queued.execution.run == nil
    assert queued.evidence.ci == nil

    queued_execution =
      queued_model
      |> PortalJSON.workspace()
      |> then(fn workspace -> hd(workspace.activity).execution end)

    assert queued_execution.timing.state_since != nil
    assert queued_execution.timing.started_at == nil
    assert queued_execution.timing.finished_at == nil

    assert {:ok, second_run} = Orchestration.assign_one(now: DateTime.add(@now, 1, :second))
    assert second_run.generation == 2

    assert {:ok, active_model} = ReadModel.workspace(project.id)
    active = Map.fetch!(active_model.projections, first.task.task.id)
    assert active.execution.run.generation == 2
    assert active.evidence.ci == nil
  end

  test "attached runtime resources do not claim scheduling compatibility by identity alone" do
    project = project_fixture()
    %{machine: machine} = enroll_machine("mismatched-machine")

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: "mismatched-runtime",
                   name: "Mismatched runtime",
                   capacity: 1,
                   agent_profile: "other-profile",
                   workspace: "other-workspace",
                   capabilities: %{}
                 }
               ],
               now: @now
             )

    assert {:ok, _resource} =
             Workspaces.create_resource(project.id, %{
               kind: "runtime",
               name: "Observed runtime",
               external_ref: runtime.id,
               status: "healthy",
               sync_status: "synced"
             })

    assert {:ok, model} = ReadModel.workspace(project.id)
    assert model.runtimes == []
    assert Enum.map(model.registered_runtimes, & &1.runtime_id) == [runtime.id]
    assert model.health.runtimes == "offline"
  end

  test "terminal and expiry projections expose stable execution timing" do
    project = project_fixture()

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Timed execution",
               status: "ready",
               assignee_type: "agent",
               agent_profile: "codex"
             })

    assert {:ok, _launched, :created} = Workspaces.launch_work_item(item.id)
    %{machine: machine} = enroll_machine("timing-machine")
    runtime = register_runtime(machine, "timing-runtime")
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    fence = claim(run, runtime)
    transition(run.id, fence, "running", 30)
    finished_at = DateTime.add(@now, 10, :second)

    assert {:ok, _completed} =
             Orchestration.transition(
               run.id,
               fence,
               "completed",
               %{"summary" => "done"},
               uuid(31),
               now: finished_at
             )

    assert {:ok, completed_model} = ReadModel.workspace(project.id)

    completed_execution =
      completed_model
      |> PortalJSON.workspace()
      |> then(fn workspace -> hd(workspace.activity).execution end)

    assert completed_execution.timing.started_at == DateTime.to_iso8601(@now)
    assert completed_execution.timing.finished_at == DateTime.to_iso8601(finished_at)

    assert {:ok, expiring_item} =
             Workspaces.create_work_item(project.id, %{
               title: "Expired execution",
               status: "ready",
               assignee_type: "agent",
               agent_profile: "codex"
             })

    assert {:ok, _launched, :created} = Workspaces.launch_work_item(expiring_item.id)
    assert {:ok, expiring_run} = Orchestration.assign_one(now: @now)
    expiring_fence = claim(expiring_run, runtime)
    transition(expiring_run.id, expiring_fence, "running", 32)
    assert %{expired_runs: 1} = Orchestration.expire(now: DateTime.add(@now, 30, :second))
    assert {:ok, expired_model} = ReadModel.workspace(project.id)

    expired_execution =
      expired_model
      |> PortalJSON.workspace()
      |> then(fn workspace ->
        workspace.activity
        |> Enum.find(&(&1.work_item.id == expiring_item.id))
        |> Map.fetch!(:execution)
      end)

    assert expired_execution.state == "queued"
    assert expired_execution.timing.state_since != nil
    assert expired_execution.timing.started_at == nil
    assert expired_execution.timing.finished_at == nil
  end

  test "cancel and retry controls follow the runtime's advertised adapter operations" do
    project = project_fixture()

    supported = launch_for(project, "cap-supported", "Advertises start and cancel")
    cancel_unsupported = launch_for(project, "cap-no-cancel", "Advertises start without cancel")
    retry_unsupported = launch_for(project, "cap-no-start", "Advertises neither operation")
    unknown = launch_for(project, "cap-unknown", "Serves no adapter record")

    %{machine: machine} = enroll_machine("capability-machine")

    supported_runtime =
      register_runtime(machine, "cap-supported",
        agent_profile: "cap-supported",
        workspace: "cap-supported",
        metadata: adapter_metadata(),
        capabilities: adapter_capabilities(start: true, cancel: true)
      )

    no_cancel_runtime =
      register_runtime(machine, "cap-no-cancel",
        agent_profile: "cap-no-cancel",
        workspace: "cap-no-cancel",
        metadata: adapter_metadata(),
        capabilities: adapter_capabilities(start: true, cancel: false)
      )

    no_start_runtime =
      register_runtime(machine, "cap-no-start",
        agent_profile: "cap-no-start",
        workspace: "cap-no-start",
        metadata: adapter_metadata(),
        capabilities: adapter_capabilities(start: false, cancel: false)
      )

    unknown_runtime =
      register_runtime(machine, "cap-unknown",
        agent_profile: "cap-unknown",
        workspace: "cap-unknown"
      )

    {supported_run, supported_fence} = assign_and_run(supported_runtime, 40, 101)
    {no_cancel_run, no_cancel_fence} = assign_and_run(no_cancel_runtime, 41, 102)
    {no_start_run, no_start_fence} = assign_and_run(no_start_runtime, 42, 103)
    {unknown_run, unknown_fence} = assign_and_run(unknown_runtime, 43, 104)

    assert {:ok, running_model} = ReadModel.workspace(project.id)

    # state-ok + supported: the running attempt's runtime advertises cancel.
    assert served_execution(running_model, supported.work_item).can_cancel
    # state-ok + unsupported: the runtime itself advertises `cancel: false`.
    refute served_execution(running_model, cancel_unsupported.work_item).can_cancel
    # state-ok + unknown: a runtime without an adapter record fails closed.
    refute served_execution(running_model, unknown.work_item).can_cancel
    # state-not-ok: a running attempt is not retryable.
    refute served_execution(running_model, supported.work_item).can_retry

    transition(supported_run.id, supported_fence, "failed", 44)
    transition(no_cancel_run.id, no_cancel_fence, "failed", 45)
    transition(no_start_run.id, no_start_fence, "failed", 46)
    transition(unknown_run.id, unknown_fence, "failed", 47)

    assert {:ok, failed_model} = ReadModel.workspace(project.id)

    # state-not-ok: a failed attempt is not cancellable.
    refute served_execution(failed_model, supported.work_item).can_cancel
    # state-ok + supported: the runtime advertises `start`, the retried
    # attempt's operation.
    assert served_execution(failed_model, supported.work_item).can_retry
    # A runtime that can start but not cancel keeps retry and drops cancel.
    assert served_execution(failed_model, cancel_unsupported.work_item).can_retry
    # state-ok + unsupported/unknown: no advertised start, no retry.
    refute served_execution(failed_model, retry_unsupported.work_item).can_retry
    refute served_execution(failed_model, unknown.work_item).can_retry

    queued = launch_for(project, "cap-local", "No runtime matches this binding")
    assert {:ok, queued_model} = ReadModel.workspace(project.id)
    queued_execution = served_execution(queued_model, queued.work_item)

    # A cancel of an attempt no runtime holds is applied by the control plane
    # itself, so no runtime advertisement gates it; retry of a live attempt
    # stays unavailable.
    assert queued_execution.can_cancel
    refute queued_execution.can_retry

    assert {:ok, _snapshot, :created} =
             Workspaces.cancel_work_item(queued.work_item.id, 1, "cap-local-cancel")

    assert {:ok, cancelled_model} = ReadModel.workspace(project.id)
    cancelled_execution = served_execution(cancelled_model, queued.work_item)

    # The cancelled attempt carries no run, so retry is a local re-queue.
    refute cancelled_execution.can_cancel
    assert cancelled_execution.can_retry
  end

  defp project_fixture do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: "Symmetry",
               key: "SYM",
               default_agent_profile: "codex",
               default_workspace: "primary"
             })

    project
  end

  defp enroll_machine(name \\ "portal-machine") do
    key = Ecto.UUID.generate()

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(
               %{name: name, machine_token: "token-#{key}"},
               key,
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret",
               now: @now
             )

    enrolled
  end

  defp register_runtime(machine, key \\ "portal-runtime", opts \\ []) do
    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               Ecto.UUID.generate(),
               [
                 Map.merge(
                   %{
                     runtime_key: key,
                     name: key,
                     capacity: 1,
                     agent_profile: Keyword.get(opts, :agent_profile, "codex"),
                     workspace: Keyword.get(opts, :workspace, "primary"),
                     capabilities: Keyword.get(opts, :capabilities, %{})
                   },
                   Keyword.get(opts, :metadata, %{})
                 )
               ],
               now: @now
             )

    runtime
  end

  defp launch_for(project, profile, title) do
    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: title,
               status: "ready",
               assignee_type: "agent",
               agent_profile: profile,
               workspace: profile
             })

    assert {:ok, launched, :created} = Workspaces.launch_work_item(item.id)
    launched
  end

  defp assign_and_run(runtime, transition_number, claim_number) do
    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.runtime_id == runtime.id
    fence = claim(run, runtime, claim_number)
    transition(run.id, fence, "running", transition_number)
    {run, fence}
  end

  # The served control projection: the same merge the portal read applies.
  defp served_execution(model, work_item) do
    work_item
    |> Repo.preload([:project, :repository_resource, :ci_resource])
    |> PortalJSON.work_item(model.projections)
    |> Map.fetch!(:execution)
  end

  # The registered metadata an adapter-backed runtime carries.
  defp adapter_metadata do
    %{
      harness_kind: "generic",
      harness_version: "generic-v1",
      adapter_version: "generic-v1",
      adapter_protocol_version: 1
    }
  end

  defp adapter_capabilities(start: start, cancel: cancel) do
    %{
      "adapter" => %{
        "kind" => "generic",
        "native_version" => "generic-v1",
        "implementation_version" => "generic-v1",
        "protocol_version" => 1,
        "operations" => %{
          "start" => start,
          "events" => start,
          "cancel" => cancel,
          "resume" => false,
          "handoff" => false,
          "guidance" => "unsupported",
          "pause" => "unsupported",
          "approval_response" => false,
          "usage" => "unknown",
          "hard_cost_limit" => false
        }
      }
    }
  end

  defp claim(run, runtime, claim_number \\ 100) do
    assert {:ok, claimed} =
             Orchestration.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: uuid(claim_number)
               },
               now: @now
             )

    %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: claimed.claim_id,
      lease_token: claimed.lease_token
    }
  end

  defp transition(run_id, fence, state, number) do
    assert {:ok, _run} =
             Orchestration.transition(run_id, fence, state, %{}, uuid(number), now: @now)
  end

  defp append_events(run_id, fence, events) do
    assert {:ok, _events} = Orchestration.append_events(run_id, fence, events, now: @now)
  end

  defp event(id, sequence, kind, payload) do
    %{
      event_id: uuid(id),
      sequence: sequence,
      kind: kind,
      payload: payload,
      occurred_at: DateTime.add(@now, sequence, :second)
    }
  end

  defp uuid(number),
    do: "00000000-0000-0000-0000-" <> String.pad_leading(Integer.to_string(number), 12, "0")
end
