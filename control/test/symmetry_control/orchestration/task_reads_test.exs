defmodule SymmetryControl.Orchestration.TaskReadsTest do
  use SymmetryControl.OrchestrationCase, async: false

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Assignment
  alias SymmetryControl.Orchestration.TaskReads

  @now ~U[2026-09-02 00:00:00.000000Z]

  test "submit_task creates a queued task and replays the same request" do
    attrs = task_attrs()

    assert {:ok, task, :created} = TaskReads.submit_task(attrs, "submit-replay", now: @now)
    assert task.state == "queued"
    assert task.attempt_generation == 1

    assert {:ok, replayed, :replayed} = TaskReads.submit_task(attrs, "submit-replay", now: @now)
    assert replayed.id == task.id
  end

  test "submit_task rejects an idempotency key reused for a different request" do
    attrs = task_attrs()

    assert {:ok, _task, :created} = TaskReads.submit_task(attrs, "submit-conflict", now: @now)

    assert {:error, :idempotency_conflict} =
             TaskReads.submit_task(Map.put(attrs, :goal, "another goal"), "submit-conflict",
               now: @now
             )
  end

  test "fetch_task returns the stored task and not_found for an unknown uuid" do
    assert {:ok, task, :created} = TaskReads.submit_task(task_attrs(), "fetch-task", now: @now)

    assert {:ok, fetched} = TaskReads.fetch_task(task.id)
    assert fetched.id == task.id
    assert {:error, :not_found} = TaskReads.fetch_task(Ecto.UUID.generate())
  end

  test "fetch_run returns the assigned run and not_found for an unknown uuid" do
    %{machine: machine} = enroll_machine()
    register_runtime(machine, @now)
    {:ok, task, :created} = TaskReads.submit_task(task_attrs(), "fetch-run", now: @now)
    {:ok, run} = Assignment.assign_one(now: @now)

    assert {:ok, fetched} = TaskReads.fetch_run(run.id)
    assert fetched.task_id == task.id
    assert fetched.state == "assigned"
    assert {:error, :not_found} = TaskReads.fetch_run(Ecto.UUID.generate())
  end

  test "fetch_command returns the recorded command and not_found for an unknown uuid" do
    {:ok, task, :created} = TaskReads.submit_task(task_attrs(), "fetch-command", now: @now)
    {:ok, _cancelled, command} = Orchestration.request_cancel(task.id, now: @now)

    assert {:ok, fetched} = TaskReads.fetch_command(command.id)
    assert fetched.id == command.id
    assert fetched.kind == "cancel"
    assert {:error, :not_found} = TaskReads.fetch_command(Ecto.UUID.generate())
  end

  test "task_snapshot includes the waiting question after a waiting_for_input transition" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    {:ok, task, :created} = TaskReads.submit_task(task_attrs(), "snapshot-waiting", now: @now)
    {:ok, run} = Assignment.assign_one(now: @now)
    fence = claim(run, runtime, @now)
    running_id = Ecto.UUID.generate()
    waiting_id = Ecto.UUID.generate()
    payload = %{"question" => "Continue?"}

    assert {:ok, _run} =
             Orchestration.transition(run.id, fence, "running", %{}, running_id, now: @now)

    assert {:ok, _run} =
             Orchestration.transition(run.id, fence, "waiting_for_input", payload, waiting_id,
               now: @now
             )

    assert {:ok, snapshot} = TaskReads.task_snapshot(task.id)
    assert snapshot.waiting.transition_id == waiting_id
    assert snapshot.waiting.question == "Continue?"
  end

  test "task_snapshot reports not_found for an unknown task uuid" do
    assert {:error, :not_found} = TaskReads.task_snapshot(Ecto.UUID.generate())
  end

  test "runtime_snapshots lists the active run and reserved capacity of a runtime" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now, capacity: 2)
    {:ok, task, :created} = TaskReads.submit_task(task_attrs(), "runtime-list", now: @now)
    {:ok, run} = Assignment.assign_one(now: @now)

    assert {:ok, [snapshot]} = TaskReads.runtime_snapshots()
    assert snapshot.machine_id == machine.id
    assert snapshot.machine_name == machine.name
    assert snapshot.runtime_id == runtime.id
    assert snapshot.runtime_key == runtime.runtime_key
    assert snapshot.status == "online"
    assert snapshot.capacity == 2
    assert snapshot.reserved_capacity == 1

    assert Enum.map(snapshot.active_runs, &{&1.run_id, &1.task_id, &1.state}) ==
             [{run.id, task.id, "assigned"}]
  end

  test "runtime_snapshot matches the list projection and reports unknown ids as not_found" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    {:ok, task, :created} = TaskReads.submit_task(task_attrs(), "runtime-one", now: @now)
    {:ok, run} = Assignment.assign_one(now: @now)

    assert {:ok, [listed]} = TaskReads.runtime_snapshots()
    assert {:ok, snapshot} = TaskReads.runtime_snapshot(runtime.id)
    assert snapshot == listed
    assert [%{run_id: run_id, task_id: task_id, state: "assigned"}] = snapshot.active_runs
    assert run_id == run.id
    assert task_id == task.id
    assert {:error, :not_found} = TaskReads.runtime_snapshot(Ecto.UUID.generate())
  end

  test "work_snapshot serves the assignment projection and rejects a stale epoch" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine, @now)
    {:ok, task, :created} = TaskReads.submit_task(task_attrs(), "work-snapshot", now: @now)
    {:ok, run} = Assignment.assign_one(now: @now)
    reconnected = register_runtime(machine, @now, daemon_instance_id: Ecto.UUID.generate())

    assert reconnected.connection_epoch == runtime.connection_epoch + 1

    assert {:ok, snapshot} =
             TaskReads.work_snapshot(runtime.id, reconnected.connection_epoch, now: @now)

    assert snapshot.server_time == @now
    assert [%{run: active_run, task: active_task}] = snapshot.assignments
    assert active_run.id == run.id
    assert active_task.id == task.id

    assert {:error, :ownership_lost} =
             TaskReads.work_snapshot(runtime.id, runtime.connection_epoch, now: @now)
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
end
