defmodule SymmetryControl.Orchestration.AssignmentTest do
  use SymmetryControl.OrchestrationCase, async: false

  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Assignment
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.TaskReads
  alias SymmetryControl.Workspaces

  @now ~U[2026-09-02 00:00:00.000000Z]

  test "assign_one assigns a queued legacy task and moves it to assigned" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    {:ok, task, :created} = TaskReads.submit_task(task_attrs(), "assign-legacy", now: @now)

    assert {:ok, run} = Assignment.assign_one(now: @now)

    assert run.task_id == task.id
    assert run.runtime_id == runtime.id
    assert run.generation == 1
    assert run.state == "assigned"
    assert {:ok, %{state: "assigned", current_generation: 1}} = TaskReads.fetch_task(task.id)
  end

  test "assign_one reports no_assignment when nothing is queued and assign_all stays empty" do
    assert {:error, :no_assignment} = Assignment.assign_one(now: @now)
    assert {:ok, []} = Assignment.assign_all(now: @now)
  end

  test "assign_one leaves a queued task unassigned when no runtime matches its binding" do
    %{machine: machine} = enroll_machine()
    register_runtime(machine, @now, agent_profile: "claude_code")
    {:ok, task, :created} = TaskReads.submit_task(task_attrs(), "binding-miss", now: @now)

    assert {:error, :no_assignment} = Assignment.assign_one(now: @now)
    assert {:ok, %{state: "queued", attempt_generation: 1}} = TaskReads.fetch_task(task.id)
    assert Repo.aggregate(Run, :count) == 0
  end

  test "runtime_has_capacity? reports a runtime with a reserved slot as full" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    {:ok, _task, :created} = TaskReads.submit_task(task_attrs(), "capacity-probe", now: @now)

    assert Assignment.runtime_has_capacity?(runtime)

    assert {:ok, _run} = Assignment.assign_one(now: @now)
    refute Assignment.runtime_has_capacity?(runtime)
  end

  test "assignment_target resolves the machine and runtime of an assigned run" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    {:ok, _task, :created} = TaskReads.submit_task(task_attrs(), "assignment-target", now: @now)
    {:ok, run} = Assignment.assign_one(now: @now)

    assert {:ok, %{machine_id: machine_id, runtime_id: runtime_id}} =
             Assignment.assignment_target(run)

    assert machine_id == machine.id
    assert runtime_id == runtime.id
  end

  test "assignment_target reports not_found for an unknown run id" do
    assert {:error, :not_found} = Assignment.assignment_target(Ecto.UUID.generate())
  end

  test "assignment_target reports not_found for a non-binary id" do
    assert {:error, :not_found} = Assignment.assignment_target(17)
  end

  test "release_reserved_session! returns a busy reserved session to available" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    {:ok, _task, :created} = TaskReads.submit_task(task_attrs(), "release-session", now: @now)
    {:ok, run} = Assignment.assign_one(now: @now)
    {session, run} = reserved_session_fixture(machine, runtime, run)

    assert %HarnessSession{state: "available", active_run_id: nil} =
             Assignment.release_reserved_session!(run, @now)

    assert %{state: "available", active_run_id: nil} = Repo.get!(HarnessSession, session.id)
  end

  test "release_reserved_session! leaves a run without a reserved session untouched" do
    %{machine: machine} = enroll_machine()
    register_runtime(machine, @now)
    {:ok, _task, :created} = TaskReads.submit_task(task_attrs(), "release-none", now: @now)
    {:ok, run} = Assignment.assign_one(now: @now)

    assert run.harness_session_id == nil
    assert :ok = Assignment.release_reserved_session!(run, @now)
    assert %{harness_session_id: nil} = Repo.get!(Run, run.id)
  end

  test "assign_one does not overcommit a runtime whose capacity is reserved" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    {:ok, first, :created} = TaskReads.submit_task(task_attrs(), "capacity-first", now: @now)
    {:ok, _second, :created} = TaskReads.submit_task(task_attrs(), "capacity-second", now: @now)

    assert {:ok, run} = Assignment.assign_one(now: @now)
    assert run.task_id == first.id
    assert run.runtime_id == runtime.id
    assert {:error, :no_assignment} = Assignment.assign_one(now: @now)
    assert Repo.aggregate(Run, :count) == 1
  end

  defp enroll_machine do
    key = Ecto.UUID.generate()
    token = "machine-token-#{key}"

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(%{name: "builder", machine_token: token}, key,
               enrollment_token: "enrollment-secret",
               expected_enrollment_token: "enrollment-secret"
             )

    enrolled
  end

  defp repository_fixture do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: "Assignment project",
               key: "ASG",
               default_agent_profile: "codex",
               default_workspace: "primary"
             })

    SymmetryControl.TestFixtures.repository_fixture(project)
  end

  defp reserved_session_fixture(machine, runtime, run) do
    binding_id = Ecto.UUID.generate()

    session =
      %HarnessSession{}
      |> HarnessSession.changeset(%{
        machine_id: machine.id,
        runtime_id: runtime.id,
        repository_resource_id: repository_fixture().id,
        harness_kind: "codex",
        harness_version: "0.153.4",
        adapter_version: "symmetry-codex-1",
        local_handle_id: Ecto.UUID.generate(),
        binding_id: Ecto.UUID.generate(),
        binding_verified: true,
        workspace_fingerprint: "workspace-fingerprint",
        state: "available"
      })
      |> Repo.insert!()

    session =
      session
      |> HarnessSession.update_changeset(%{
        state: "busy",
        active_run_id: run.id,
        binding_id: binding_id
      })
      |> Repo.update!()

    run =
      run
      |> Ecto.Changeset.change(harness_session_id: session.id, harness_binding_id: binding_id)
      |> Repo.update!()

    {session, run}
  end
end
