defmodule SymmetryControl.Orchestration.SchedulerTest do
  use SymmetryControl.DataCase, async: false

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Reconciler
  alias SymmetryControl.Orchestration.Scheduler

  @now ~U[2026-09-02 00:00:00.000000Z]

  test "reconciler expiry requeues work and scheduler creates its next generation" do
    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    {:ok, task, :created} = Orchestration.submit_task(task_attrs(), "scheduler-task", now: @now)

    assert :ok = Scheduler.drain(now: @now)
    assert {:ok, assigned} = Orchestration.fetch_task(task.id)
    assert assigned.state == "assigned"

    later = DateTime.add(@now, 31, :second)
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

  defp register_runtime(machine) do
    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               "00000000-0000-0000-0000-000000000201",
               [
                 %{
                   runtime_key: "default",
                   name: "Scheduler",
                   capacity: 1,
                   agent_profile: "codex",
                   workspace: "primary",
                   capabilities: %{},
                   heartbeat_interval_ms: 60_000
                 }
               ],
               now: @now
             )

    runtime
  end

  defp task_attrs,
    do: %{goal: "Run tests", agent_profile: "codex", workspace: "primary", input: %{}}
end
