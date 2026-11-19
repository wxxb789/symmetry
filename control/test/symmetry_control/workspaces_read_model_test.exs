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
    # The work-item-less task snapshot claims no retry: the read and the
    # command responses resolve that from the work item's binding.
    refute snapshot_execution(supported.work_item).can_retry
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

    # Retry re-queues into assignment, and no runtime assignment could select
    # for this binding advertises `start`, so the control is never offered.
    refute cancelled_execution.can_cancel
    refute cancelled_execution.can_retry

    # Assigned (never claimed): the control plane applies the cancel itself, so
    # the runtime's advertisement does not gate it even though it says
    # `cancel: false`.
    assigned = launch_for(project, "cap-no-cancel", "Assigned cancel without advertisement")
    assert {:ok, assigned_run} = Orchestration.assign_one(now: @now)
    assert assigned_run.runtime_id == no_cancel_runtime.id
    assert {:ok, assigned_model} = ReadModel.workspace(project.id)
    assert served_execution(assigned_model, assigned.work_item).can_cancel

    # Retry reads assignment's own eligibility, not the previous attempt's
    # runtime: a second runtime with the same binding that advertises `start`
    # makes the retried attempt possible even though the failed attempt's
    # runtime does not.
    other_runtime =
      register_runtime(machine, "cap-no-start-other",
        agent_profile: "cap-no-start",
        workspace: "cap-no-start",
        metadata: adapter_metadata(),
        capabilities: adapter_capabilities(start: true, cancel: true)
      )

    assert other_runtime.id != no_start_runtime.id

    assert {:ok, eligible_model} = ReadModel.workspace(project.id)
    assert served_execution(eligible_model, retry_unsupported.work_item).can_retry

    # The retry re-binds the next generation from the corrected work item, so
    # the served control follows the item's binding, not the failed task row's.
    rebound = fetch_item(supported.work_item.id)

    assert {:ok, _edited} =
             Workspaces.update_work_item(supported.work_item.id, %{
               version: rebound.lock_version,
               agent_profile: "cap-unknown",
               workspace: "cap-unknown"
             })

    assert {:ok, rebound_model} = ReadModel.workspace(project.id)
    refute served_execution(rebound_model, rebound).can_retry

    restored = fetch_item(supported.work_item.id)

    assert {:ok, _edited} =
             Workspaces.update_work_item(supported.work_item.id, %{
               version: restored.lock_version,
               agent_profile: "cap-supported",
               workspace: "cap-supported"
             })

    assert {:ok, restored_model} = ReadModel.workspace(project.id)
    assert served_execution(restored_model, restored).can_retry

    # Eligibility ignores the transient state assignment filters on top: the
    # only eligible runtime is saturated and then goes offline, and the served
    # control stays offered because the scheduler still has an eligible start.
    launch_for(project, "cap-supported", "Saturates the supported runtime")
    assert {:ok, capacity_run} = Orchestration.assign_one(now: @now)
    assert capacity_run.runtime_id == supported_runtime.id
    capacity_fence = claim(capacity_run, supported_runtime, 106)

    Repo.update_all(
      from(runtime in Orchestration.Runtime, where: runtime.id == ^supported_runtime.id),
      set: [status: "offline"]
    )

    assert {:ok, transient_model} = ReadModel.workspace(project.id)
    assert served_execution(transient_model, restored).can_retry

    # Settle the saturating attempt so the project can be archived below.
    transition(capacity_run.id, capacity_fence, "failed", 54)

    # An archived project refuses the retry the endpoint would reject, so the
    # control is withheld even though a selectable runtime advertises `start`.
    assert {:ok, _assigned_snapshot, :created} =
             Workspaces.cancel_work_item(assigned.work_item.id, 1, "cap-assigned-cancel")

    assert {:ok, _archived} =
             Workspaces.update_project(project.id, %{
               version: project.lock_version,
               status: "archived"
             })

    assert {:ok, archived_model} = ReadModel.workspace(project.id)
    refute served_execution(archived_model, supported.work_item).can_retry
  end

  test "the served retry control keeps a chat-launched attempt's supervisory requirement" do
    project = project_fixture()
    %{machine: machine} = enroll_machine("supervisory-retry-machine")

    # The chat launch path can require supervision, and the retry of that
    # attempt retains the requirement even though the corrected work item
    # derives no capability of its own.
    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Supervised retry",
               status: "ready",
               assignee_type: "agent",
               agent_profile: "cap-supervised-retry",
               workspace: "cap-supervised-retry"
             })

    assert {:ok, launched, :created} =
             Workspaces.launch_work_item(item.id, "cap-supervised-retry-launch",
               required_capabilities: %{"supervisory_control" => true},
               notify: false
             )

    assert launched.task.task.required_capabilities == %{"supervisory_control" => true}

    assert {:ok, _cancelled, :created} =
             Workspaces.cancel_work_item(item.id, 1, "cap-supervised-retry-cancel")

    served = fetch_item(item.id)

    # A runtime that could start an unsupervised attempt of this binding does
    # not make the retried attempt selectable: it advertises no
    # `supervisory_control`, and the retry re-applies the requirement.
    register_runtime(machine, "cap-supervised-retry-unsupervised",
      agent_profile: "cap-supervised-retry",
      workspace: "cap-supervised-retry",
      metadata: adapter_metadata(),
      capabilities: adapter_capabilities(start: true, cancel: true)
    )

    assert {:ok, unsupervised_model} = ReadModel.workspace(project.id)
    refute served_execution(unsupervised_model, served).can_retry

    # A runtime that advertises the requirement and the adapter `start`
    # operation makes the same retried attempt selectable.
    register_runtime(machine, "cap-supervised-retry-supervisor",
      agent_profile: "cap-supervised-retry",
      workspace: "cap-supervised-retry",
      metadata: adapter_metadata(),
      capabilities: supervised_adapter_capabilities()
    )

    assert {:ok, supervised_model} = ReadModel.workspace(project.id)
    assert served_execution(supervised_model, served).can_retry
  end

  test "the task snapshot offers cancel for claimed, paused and waiting attempts that advertise it" do
    project = project_fixture()
    item = launch_for(project, "cap-supervised", "Supervised cancel surface")
    %{machine: machine} = enroll_machine("supervised-machine")

    runtime =
      register_runtime(machine, "cap-supervised",
        agent_profile: "cap-supervised",
        workspace: "cap-supervised",
        metadata: adapter_metadata(),
        capabilities: supervised_adapter_capabilities()
      )

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.runtime_id == runtime.id
    fence = claim(run, runtime, 105)

    # claimed: the runtime holds the attempt and advertises cancel.
    assert %{can_cancel: true} = snapshot_execution(item.work_item)

    transition(run.id, fence, "running", 50)
    assert %{can_cancel: true} = snapshot_execution(item.work_item)

    transition(run.id, fence, "waiting_for_input", 51)
    assert %{can_cancel: true} = snapshot_execution(item.work_item)

    # Back to running, then a safe-boundary pause the same runtime supervises.
    transition(run.id, fence, "running", 52)

    assert {:ok, pause, :created} =
             Orchestration.create_command(
               item.work_item.orchestration_task_id,
               "pause",
               %{},
               "cap-supervised-pause",
               expected_generation: 1,
               now: @now
             )

    assert {:ok, _paused} =
             Orchestration.transition(
               run.id,
               fence,
               "paused",
               %{"command_id" => pause.id},
               uuid(53),
               now: @now
             )

    assert %{can_cancel: true} = snapshot_execution(item.work_item)
  end

  test "a workspace snapshot read keeps every query on the calling process" do
    project = project_fixture()

    assert {:ok, _connection} =
             Workspaces.create_resource(project.id, %{
               kind: "connection",
               name: "Snapshot connection",
               status: "healthy",
               sync_status: "synced"
             })

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Snapshot on one connection",
               status: "ready"
             })

    reader = self()
    forms = [project.id, Ecto.UUID.dump!(project.id)]
    handler_id = {__MODULE__, :snapshot_read, project.id}

    :telemetry.attach(
      handler_id,
      [:symmetry_control, :repo, :query],
      fn _event, _measurements, metadata, %{reader: reader, forms: forms} ->
        if snapshot_read?(metadata.params, forms),
          do: send(reader, {:snapshot_read, self(), to_string(metadata.query)})
      end,
      %{reader: reader, forms: forms}
    )

    on_exit(fn -> :telemetry.detach(handler_id) end)

    assert {:ok, %{work_items: work_items}} = Workspaces.workspace_snapshot(project.id)
    assert Enum.any?(work_items, &(&1.id == item.id))

    reads = snapshot_reads([])

    queries = Enum.map(reads, fn {_pid, query} -> query end)
    processes = reads |> Enum.map(&elem(&1, 0)) |> Enum.uniq() |> Enum.sort()

    # Two preloads must have rows for this test to have power: Ecto only
    # preloads in parallel from two preloads with fetch work, and every parallel
    # preload would queue for the sandbox connection the test process owns.
    assert Enum.any?(queries, &String.contains?(&1, "\"project_resources\""))
    assert Enum.any?(queries, &String.contains?(&1, "\"work_items\""))

    assert processes == [reader],
           "expected every workspace snapshot read query on #{inspect(reader)}, got #{inspect(processes)}"
  end

  defp snapshot_reads(reads) do
    receive do
      {:snapshot_read, pid, query} -> snapshot_reads([{pid, query} | reads])
    after
      0 -> reads
    end
  end

  defp snapshot_read?(params, forms) do
    params
    |> List.wrap()
    |> Enum.any?(fn
      value when is_binary(value) -> value in forms
      values when is_list(values) -> Enum.any?(values, &(&1 in forms))
      _value -> false
    end)
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

  defp fetch_item(work_item_id) do
    assert {:ok, item} = Workspaces.fetch_work_item(work_item_id)
    item
  end

  # The served control projection: the same merge the portal read applies.
  defp served_execution(model, work_item) do
    work_item
    |> Repo.preload([:project, :repository_resource, :ci_resource], force: true)
    |> PortalJSON.work_item(model.projections)
    |> Map.fetch!(:execution)
  end

  # The work-item-less task snapshot's control projection: cancel affordances
  # only, no retry claim — the read and the command responses resolve that from
  # the work item's binding.
  defp snapshot_execution(work_item) do
    assert {:ok, snapshot} = Orchestration.task_snapshot(work_item.orchestration_task_id)
    PortalJSON.execution(snapshot)
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

  defp supervised_adapter_capabilities do
    adapter_capabilities(start: true, cancel: true)
    |> Map.merge(%{
      "structured_input" => true,
      "interactive" => true,
      "supervisory_control" => true
    })
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
