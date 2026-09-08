defmodule SymmetryControl.OrchestrationTelemetryTest do
  use SymmetryControl.DataCase, async: false

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Repo

  @now ~U[2026-09-07 00:00:00.000000Z]
  @events [
    [:symmetry_control, :orchestration, :run, :assigned],
    [:symmetry_control, :orchestration, :run, :claimed],
    [:symmetry_control, :orchestration, :run, :transition],
    [:symmetry_control, :orchestration, :run, :expired],
    [:symmetry_control, :orchestration, :runtime, :offline]
  ]

  test "emits committed orchestration lifecycle events" do
    ref = :telemetry_test.attach_event_handlers(self(), @events)
    on_exit(fn -> :telemetry.detach(ref) end)

    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)
    {:ok, _task, :created} = Orchestration.submit_task(task_attrs(), "telemetry-task", now: @now)
    {:ok, run} = Orchestration.assign_one(now: @now)

    assert_event([:symmetry_control, :orchestration, :run, :assigned], ref, fn metadata ->
      assert metadata.run_id == run.id
      assert metadata.task_id == run.task_id
      assert metadata.runtime_id == runtime.id
      assert metadata.generation == run.generation
    end)

    fence = claim(run, runtime)

    assert_event([:symmetry_control, :orchestration, :run, :claimed], ref, fn metadata ->
      assert metadata.run_id == run.id
      assert metadata.generation == run.generation
    end)

    transition_id = Ecto.UUID.generate()

    assert {:ok, %{state: "running"}} =
             Orchestration.transition(run.id, fence, "running", %{}, transition_id, now: @now)

    assert_event([:symmetry_control, :orchestration, :run, :transition], ref, fn metadata ->
      assert metadata.run_id == run.id
      assert metadata.generation == run.generation
      assert metadata.state == "running"
    end)

    assert %{expired_runs: 1, offline_runtimes: 1} =
             Orchestration.expire(now: DateTime.add(@now, 31, :second))

    assert_event([:symmetry_control, :orchestration, :run, :expired], ref, fn metadata ->
      assert metadata.run_id == run.id
      assert metadata.generation == run.generation
      assert metadata.state == "expired"
    end)

    assert_event([:symmetry_control, :orchestration, :runtime, :offline], ref, fn metadata ->
      assert metadata.runtime_id == runtime.id
    end)
  end

  test "does not emit a claim event when an enclosing transaction rolls back" do
    ref =
      :telemetry_test.attach_event_handlers(self(), [
        [:symmetry_control, :orchestration, :run, :claimed]
      ])

    on_exit(fn -> :telemetry.detach(ref) end)

    %{machine: machine} = enroll_machine()
    runtime = register_runtime(machine)

    {:ok, _task, :created} =
      Orchestration.submit_task(task_attrs(), "rolled-back-claim", now: @now)

    {:ok, run} = Orchestration.assign_one(now: @now)

    request = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: Ecto.UUID.generate()
    }

    assert {:error, :later_failure} =
             Repo.transaction(fn ->
               assert {:ok, _claimed, :created} =
                        Orchestration.claim_with_disposition(run.id, request, now: @now)

               Repo.rollback(:later_failure)
             end)

    refute_receive {[:symmetry_control, :orchestration, :run, :claimed], ^ref, _, _}
  end

  defp assert_event(event, ref, assert_metadata) do
    assert_receive {^event, ^ref, %{count: 1}, metadata}
    assert_metadata.(metadata)
  end

  defp enroll_machine do
    key = Ecto.UUID.generate()

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(%{name: "telemetry-builder", machine_token: key}, key,
               enrollment_token: "test-enrollment-token",
               expected_enrollment_token: "test-enrollment-token"
             )

    enrolled
  end

  defp register_runtime(machine) do
    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: "telemetry",
                   name: "Telemetry",
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

  defp task_attrs,
    do: %{goal: "Observe events", agent_profile: "codex", workspace: "primary", input: %{}}

  defp claim(run, runtime) do
    assert {:ok, claimed} =
             Orchestration.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: Ecto.UUID.generate()
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
end
