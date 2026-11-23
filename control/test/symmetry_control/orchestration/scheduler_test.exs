defmodule SymmetryControl.Orchestration.SchedulerTest do
  use SymmetryControl.OrchestrationCase, async: false

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Reconciler
  alias SymmetryControl.Orchestration.Scheduler

  @now ~U[2026-09-02 00:00:00.000000Z]

  test "reconciler expiry requeues work and scheduler creates its next generation" do
    %{machine: machine} = enroll_machine()

    runtime =
      register_runtime(machine, @now,
        name: "Scheduler",
        daemon_instance_id: "00000000-0000-0000-0000-000000000201",
        extra: %{heartbeat_interval_ms: 60_000}
      )

    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "scheduler-task", now: @now)

    assert :ok = Scheduler.drain(now: @now)
    assert {:ok, assigned} = Orchestration.fetch_task(task.id)
    assert assigned.state == "assigned"

    later = DateTime.shift(@now, second: 31)
    assert %{expired_runs: 1} = Reconciler.run_once(now: later)
    assert {:ok, requeued} = Orchestration.fetch_task(task.id)
    assert requeued.state == "queued"

    assert :ok = Scheduler.drain(now: later)
    assert {:ok, reassigned} = Orchestration.fetch_task(task.id)
    assert reassigned.state == "assigned"
    assert reassigned.current_generation == 2
    assert {:ok, ^runtime} = Orchestration.fetch_runtime(runtime.id)
  end

  test "wake succeeds while the supervised scheduler is temporarily unregistered" do
    config = Application.fetch_env!(:symmetry_control, :orchestration)
    pid = Process.whereis(Scheduler)
    assert is_pid(pid)

    Application.put_env(
      :symmetry_control,
      :orchestration,
      Keyword.put(config, :scheduler_enabled, true)
    )

    Process.unregister(Scheduler)

    try do
      assert :ok = Scheduler.wake()
    after
      if Process.whereis(Scheduler) == nil and Process.alive?(pid),
        do: Process.register(pid, Scheduler)

      Application.put_env(:symmetry_control, :orchestration, config)
    end
  end

  defp enroll_machine do
    key = Ecto.UUID.generate()
    token = "machine-token-#{key}"

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(%{name: "scheduler-builder", machine_token: token}, key,
               enrollment_token: "test-enrollment-token",
               expected_enrollment_token: "test-enrollment-token"
             )

    enrolled
  end
end
