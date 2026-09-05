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
      event(4, 3, "artifact", %{"path" => "control/priv/portal_assets/portal.js"}),
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
             "control/priv/portal_assets/portal.js"
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

  defp register_runtime(machine, key \\ "portal-runtime") do
    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: key,
                   name: key,
                   capacity: 1,
                   agent_profile: "codex",
                   workspace: "primary",
                   capabilities: %{}
                 }
               ],
               now: @now
             )

    runtime
  end

  defp claim(run, runtime) do
    assert {:ok, claimed} =
             Orchestration.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: uuid(100)
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
