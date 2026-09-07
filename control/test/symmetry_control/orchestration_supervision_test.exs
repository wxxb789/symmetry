defmodule SymmetryControl.OrchestrationSupervisionTest do
  use SymmetryControl.DataCase, async: true

  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.{Command, RunTransition}
  alias SymmetryControl.Workspaces
  alias SymmetryControlWeb.{PortalJSON, Protocol}

  @now ~U[2026-09-06 11:00:00.000000Z]

  test "guidance admission matches the daemon UTF-8 byte boundary" do
    {task, _run, _runtime, _fence} = running()

    for message <- [String.duplicate("a", 32_768), String.duplicate("é", 16_384)] do
      assert byte_size(message) == 32_768
      assert {:ok, _, :created} = command(task, "guidance", %{"message" => message})
      count = Repo.aggregate(Command, :count)

      assert {:error, :invalid_request} =
               command(task, "guidance", %{"message" => message <> "x"})

      assert Repo.aggregate(Command, :count) == count
    end
  end

  test "serialized supervised work carries its per-task requirement without changing legacy work" do
    {task, run, _runtime, _fence} = running()
    requirements = %{"supervisory_control" => true}
    assert Protocol.task(task).work.required_capabilities == requirements
    assert Protocol.claimed_run(run, task).work.required_capabilities == requirements
    legacy = %{task | required_capabilities: %{}}
    refute Map.has_key?(Protocol.task(legacy).work, :required_capabilities)
    refute Map.has_key?(Protocol.claimed_run(run, legacy).work, :required_capabilities)
  end

  test "legacy Workspaces cancel action hashes replay after the run settles" do
    {task, run, _runtime, fence} = running(supervised: false)
    item = linked_work_item(task)
    action_id = "legacy-cancel"
    key = "portal:cancel:#{item.id}:#{task.attempt_generation}:#{action_id}"
    assert {:ok, cancellation, :created} = command(task, "cancel", %{}, key)
    legacy_command_hash(cancellation, "cancel", %{})
    assert {:ok, _} = transition(run, fence, "cancelled", %{})

    assert {:ok, replayed, :replayed} =
             Workspaces.cancel_work_item(item.id, task.attempt_generation, action_id)

    assert replayed.command.id == cancellation.id
    assert replayed.command.request_hash_version == 1
    assert Repo.aggregate(Command, :count) == 1
  end

  test "legacy Workspaces input action hashes replay without changing the old waiting identity" do
    {task, run, _runtime, fence} = running(supervised: false)
    item = linked_work_item(task)
    waiting_id = Ecto.UUID.generate()

    assert {:ok, _} =
             Orchestration.transition(
               run.id,
               fence,
               "waiting_for_input",
               %{"question" => "Continue?"},
               waiting_id,
               now: @now
             )

    payload = %{"answer" => "Continue"}
    action_id = "legacy-input"
    key = "portal:input:#{item.id}:#{run.generation}:#{waiting_id}:#{action_id}"

    assert {:ok, input, :created} =
             Orchestration.provide_input(task.id, payload, key,
               expected_waiting_transition_id: waiting_id,
               now: @now
             )

    legacy_command_hash(input, "provide_input", payload)
    assert {:ok, _} = transition(run, fence, "running", %{})
    assert {:ok, _} = acknowledge(input, fence)
    assert {:ok, _} = transition(run, fence, "completed", %{"summary" => "Done"})

    assert {:ok, replayed, :replayed} =
             Workspaces.provide_work_item_input(item.id, payload, waiting_id, action_id)

    assert replayed.command.id == input.id
    assert replayed.command.request_hash_version == 1

    assert {:error, :idempotency_conflict} =
             Workspaces.provide_work_item_input(
               item.id,
               %{"answer" => "Different"},
               waiting_id,
               action_id
             )

    assert Repo.aggregate(Command, :count) == 1
  end

  test "new input hashes bind the waiting identity as well as the payload" do
    {task, run, _runtime, fence} = running()
    waiting_id = Ecto.UUID.generate()

    assert {:ok, _} =
             Orchestration.transition(run.id, fence, "waiting_for_input", packet(), waiting_id,
               now: @now
             )

    key = Ecto.UUID.generate()
    payload = %{"option_id" => "staged"}

    opts = [
      expected_generation: task.attempt_generation,
      expected_waiting_transition_id: waiting_id,
      now: @now
    ]

    assert {:ok, input, :created} = Orchestration.provide_input(task.id, payload, key, opts)
    assert input.request_hash_version == 2

    assert {:error, :idempotency_conflict} =
             Orchestration.provide_input(
               task.id,
               payload,
               key,
               Keyword.put(opts, :expected_waiting_transition_id, Ecto.UUID.generate())
             )
  end

  test "runtime supervisory capability requires structured interactive input" do
    machine = machine()

    for capabilities <- [
          %{"supervisory_control" => true},
          %{"supervisory_control" => true, "structured_input" => true},
          %{"supervisory_control" => "true", "structured_input" => true, "interactive" => true}
        ] do
      assert {:error, :invalid_request} =
               Orchestration.register_runtimes(
                 machine.id,
                 Ecto.UUID.generate(),
                 [runtime_spec(capabilities)],
                 now: @now
               )
    end

    assert {:ok, [_runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               Ecto.UUID.generate(),
               [runtime_spec(capabilities())],
               now: @now
             )
  end

  test "Chat launch requirements persist and roll back with the caller's transaction" do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: "Chat project",
               key: "CHAT",
               default_agent_profile: "supervised",
               default_workspace: "primary"
             })

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Build adapter",
               description: "Preserve the complete work description.",
               assignee_type: "agent",
               status: "ready"
             })

    opts = [required_capabilities: %{"supervisory_control" => true}, notify: false]

    assert {:error, :abort_message} =
             Repo.transaction(fn ->
               assert {:ok, launched, :created} =
                        Workspaces.launch_work_item(item.id, "chat-launch", opts)

               assert launched.task.task.required_capabilities == %{"supervisory_control" => true}
               Repo.rollback(:abort_message)
             end)

    assert {:ok, unchanged} = Workspaces.fetch_work_item(item.id)
    assert unchanged.orchestration_task_id == nil
    assert Repo.aggregate(SymmetryControl.Orchestration.Task, :count) == 0
    assert {:ok, launched, :created} = Workspaces.launch_work_item(item.id, "chat-launch", opts)
    assert launched.task.task.required_capabilities["supervisory_control"]
    assert launched.task.task.goal =~ "Preserve the complete work description."
  end

  test "retry retains required supervision even if refreshed work omits capabilities" do
    {task, run, _runtime, fence} = running()
    assert {:ok, _} = transition(run, fence, "failed", %{"message" => "Retryable failure"})

    assert {:ok, retried, command, :created} =
             Orchestration.retry_task(task.id, task_attrs(), Ecto.UUID.generate(),
               expected_generation: task.attempt_generation,
               now: @now
             )

    assert retried.required_capabilities == %{"supervisory_control" => true}

    assert Repo.get!(Command, command.id).payload["work"]["required_capabilities"][
             "supervisory_control"
           ] == true
  end

  test "legacy runtime rejects controls and advertises no control affordances" do
    {task, _run, _runtime, _fence} = running(capabilities: %{}, supervised: false)

    assert {:error, :unsupported_control} =
             command(task, "guidance", %{"message" => "Use the adapter."})

    assert {:ok, snapshot} = Orchestration.task_snapshot(task.id)
    refute PortalJSON.execution(snapshot).can_guide
    refute PortalJSON.execution(snapshot).can_pause
  end

  test "guidance is durable, generation fenced and acknowledged only when applied" do
    {task, run, runtime, fence} = running()
    payload = %{"message" => "Use the existing adapter."}
    key = Ecto.UUID.generate()
    assert {:ok, guidance, :created} = command(task, "guidance", payload, key)
    assert {:ok, replayed, :replayed} = command(task, "guidance", payload, key)
    assert replayed.id == guidance.id

    assert {:error, :idempotency_conflict} =
             command(task, "guidance", %{"message" => "Other"}, key)

    assert {:error, :idempotency_conflict} =
             Orchestration.create_command(task.id, "guidance", payload, key,
               expected_generation: task.attempt_generation + 1,
               now: @now
             )

    assert {:error, :state_conflict} =
             Orchestration.create_command(task.id, "guidance", payload, Ecto.UUID.generate(),
               expected_generation: task.attempt_generation + 1,
               now: @now
             )

    assert {:ok, snapshot} =
             Orchestration.work_snapshot(runtime.id, runtime.connection_epoch, now: @now)

    assert Enum.any?(snapshot.commands, &(&1.id == guidance.id and &1.state == "pending"))
    assert {:ok, %{state: "running"}} = Orchestration.fetch_run(run.id)

    assert {:ok, acknowledged} = acknowledge(guidance, fence)
    assert acknowledged.acknowledgement_outcome == "applied"
    assert acknowledged.applied_at == @now
    assert {:ok, %{state: "running"}} = Orchestration.fetch_task(task.id)
  end

  test "pause and resume require matching safe-boundary transitions and retain capacity" do
    {task, run, runtime, fence} = running()
    assert {:ok, pause, :created} = command(task, "pause")
    assert {:ok, snapshot} = Orchestration.task_snapshot(task.id)
    assert snapshot.task.state == "running"
    refute PortalJSON.execution(snapshot).can_pause
    assert {:error, :state_conflict} = command(task, "pause")
    assert {:error, :state_conflict} = acknowledge(pause, fence)
    assert {:error, :invalid_request} = transition(run, fence, "paused", %{})

    assert {:error, :state_conflict} =
             transition(run, fence, "paused", %{"command_id" => Ecto.UUID.generate()})

    assert {:ok, %{state: "paused"}} =
             transition(run, fence, "paused", %{"command_id" => pause.id})

    assert {:error, :state_conflict} = acknowledge(pause, fence, "rejected")
    assert {:ok, _} = acknowledge(pause, fence)

    assert {:ok, snapshot} = Orchestration.task_snapshot(task.id)
    execution = PortalJSON.execution(snapshot)
    assert execution.can_resume
    assert execution.can_cancel
    assert execution.can_guide
    refute execution.can_pause
    assert {:ok, %{state: "paused"}} = Orchestration.renew_lease(run.id, fence, now: @now)
    assert {:ok, runtime_snapshot} = Orchestration.runtime_snapshot(runtime.id)
    assert runtime_snapshot.reserved_capacity == 1

    assert {:ok, _, :created} =
             Orchestration.submit_task(task_attrs(), Ecto.UUID.generate(), now: @now)

    assert {:error, :no_assignment} = Orchestration.assign_one(now: @now)

    assert {:error, :state_conflict} =
             transition(run, fence, "running", %{"command_id" => pause.id})

    assert {:ok, resume, :created} = command(task, "resume")

    assert {:ok, %{state: "running"}} =
             transition(run, fence, "running", %{"command_id" => resume.id})

    assert {:ok, _} = acknowledge(resume, fence)
    assert {:ok, snapshot} = Orchestration.task_snapshot(task.id)
    assert Protocol.task(snapshot).controls.can_pause
  end

  test "control transition replay cannot create a second state change" do
    {task, run, _runtime, fence} = running()
    assert {:ok, pause, :created} = command(task, "pause")
    transition_id = Ecto.UUID.generate()
    payload = %{"command_id" => pause.id}

    assert {:ok, _} =
             Orchestration.transition(run.id, fence, "paused", payload, transition_id, now: @now)

    assert {:ok, _} =
             Orchestration.transition(run.id, fence, "paused", payload, transition_id, now: @now)

    assert Repo.aggregate(
             from(t in RunTransition, where: t.transition_id == ^transition_id),
             :count
           ) == 1

    assert {:error, :state_conflict} = transition(run, fence, "paused", payload)
  end

  test "cancel wins over unconsumed controls and rejected receipt can be settled" do
    {task, run, _runtime, fence} = running()
    assert {:ok, guidance, :created} = command(task, "guidance", %{"message" => "Preserve API."})
    assert {:ok, pause, :created} = command(task, "pause")
    assert {:ok, cancel, :created} = command(task, "cancel")

    for control <- [guidance, pause] do
      assert {:ok, rejected} = Orchestration.fetch_command(control.id)
      assert rejected.state == "acknowledged"
      assert rejected.acknowledgement_outcome == "rejected"
      assert {:error, :state_conflict} = acknowledge(control, fence)
      assert {:ok, _} = acknowledge(control, fence, "rejected")
    end

    assert {:error, :invalid_transition} =
             transition(run, fence, "paused", %{"command_id" => pause.id})

    assert {:ok, %{state: "cancelled"}} = transition(run, fence, "cancelled", %{})
    assert {:ok, _} = acknowledge(cancel, fence)
    assert {:ok, %{state: "cancelled"}} = Orchestration.fetch_task(task.id)
  end

  test "completion retires pending guidance and pause without applying them" do
    {task, run, runtime, fence} = running()
    assert {:ok, guidance, :created} = command(task, "guidance", %{"message" => "Use adapter."})
    assert {:ok, pause, :created} = command(task, "pause")
    assert {:ok, _} = transition(run, fence, "completed", %{"summary" => "Done"})

    assert {:ok, snapshot} =
             Orchestration.work_snapshot(runtime.id, runtime.connection_epoch, now: @now)

    assert snapshot.commands == []

    for control <- [guidance, pause] do
      assert Repo.get!(Command, control.id).acknowledgement_outcome == "rejected"
      assert {:error, :state_conflict} = acknowledge(control, fence)
    end
  end

  test "lost paused workers and pending pauses fail safely without automatic retry" do
    for applied? <- [false, true] do
      {task, run, _runtime, fence} = running()
      assert {:ok, pause, :created} = command(task, "pause")

      if applied? do
        assert {:ok, _} = transition(run, fence, "paused", %{"command_id" => pause.id})
        assert {:ok, _} = acknowledge(pause, fence)
      end

      assert %{expired_runs: 1} = Orchestration.expire(now: DateTime.add(@now, 31, :second))
      assert {:ok, failed} = Orchestration.fetch_task(task.id)
      assert failed.state == "failed"
      assert failed.attempt_generation == run.generation
      assert failed.failure["reason"] == "supervised_worker_lost"
      assert {:error, :state_conflict} = transition(run, fence, "completed", %{})
    end
  end

  test "a replacement daemon cannot apply old guidance or resume a paused worker" do
    {task, run, runtime, fence} = running()
    assert {:ok, pause, :created} = command(task, "pause")
    assert {:ok, _} = transition(run, fence, "paused", %{"command_id" => pause.id})
    assert {:ok, _} = acknowledge(pause, fence)

    assert {:ok, [_]} =
             Orchestration.register_runtimes(
               runtime.machine_id,
               Ecto.UUID.generate(),
               [runtime_spec(capabilities())],
               now: @now
             )

    assert {:error, :ownership_lost} = command(task, "resume")
    assert {:error, :ownership_lost} = Orchestration.renew_lease(run.id, fence, now: @now)
    assert {:ok, snapshot} = Orchestration.task_snapshot(task.id)
    refute PortalJSON.execution(snapshot).can_resume
  end

  test "supervised waits require consequential decision packets but legacy waits remain compatible" do
    {task, run, _runtime, fence} = running()

    assert {:error, :invalid_request} =
             transition(run, fence, "waiting_for_input", %{"question" => "Which tool?"})

    bad_packet = put_in(packet(), ["decision", "reason"], "ordinary_choice")
    assert {:error, :invalid_request} = transition(run, fence, "waiting_for_input", bad_packet)
    bad_recommendation = put_in(packet(), ["decision", "recommended_option_id"], "missing")

    assert {:error, :invalid_request} =
             transition(run, fence, "waiting_for_input", bad_recommendation)

    assert {:ok, _} = transition(run, fence, "waiting_for_input", packet())
    assert {:ok, snapshot} = Orchestration.task_snapshot(task.id)
    assert Protocol.task(snapshot).waiting.decision["recommended_option_id"] == "staged"

    {_legacy_task, legacy_run, _runtime, legacy_fence} = running(supervised: false)

    assert {:ok, _} =
             transition(legacy_run, legacy_fence, "waiting_for_input", %{
               "question" => "Continue?"
             })
  end

  test "decision packets preserve more than ten options and allow choosing the last one" do
    {task, run, _runtime, fence} = running()
    waiting_id = Ecto.UUID.generate()

    options =
      for index <- 1..11 do
        %{
          "id" => "option-#{index}",
          "label" => "Migration strategy #{index}",
          "consequence" => "Uses migration strategy #{index}."
        }
      end

    payload =
      packet()
      |> put_in(["decision", "options"], options)
      |> put_in(["decision", "recommended_option_id"], "option-11")

    assert {:ok, _} =
             Orchestration.transition(run.id, fence, "waiting_for_input", payload, waiting_id,
               now: @now
             )

    assert {:ok, snapshot} = Orchestration.task_snapshot(task.id)
    assert Protocol.task(snapshot).waiting.decision == payload["decision"]

    assert {:ok, input, :created} =
             Orchestration.provide_input(
               task.id,
               %{"option_id" => "option-11"},
               Ecto.UUID.generate(),
               expected_generation: task.attempt_generation,
               expected_waiting_transition_id: waiting_id,
               now: @now
             )

    assert input.payload == %{"option_id" => "option-11"}
  end

  test "decision choices are constrained to the exact durable waiting packet" do
    {task, run, _runtime, fence} = running()
    waiting_id = Ecto.UUID.generate()

    assert {:ok, _} =
             Orchestration.transition(run.id, fence, "waiting_for_input", packet(), waiting_id,
               now: @now
             )

    opts = [
      expected_generation: task.attempt_generation,
      expected_waiting_transition_id: waiting_id,
      now: @now
    ]

    assert {:error, :invalid_request} =
             Orchestration.provide_input(
               task.id,
               %{"answer" => "yes"},
               Ecto.UUID.generate(),
               opts
             )

    assert {:error, :invalid_request} =
             Orchestration.provide_input(
               task.id,
               %{"option_id" => "missing"},
               Ecto.UUID.generate(),
               opts
             )

    assert {:error, :state_conflict} =
             Orchestration.provide_input(
               task.id,
               %{"option_id" => "staged"},
               Ecto.UUID.generate(),
               Keyword.put(opts, :expected_waiting_transition_id, Ecto.UUID.generate())
             )

    assert {:ok, input, :created} =
             Orchestration.provide_input(
               task.id,
               %{"option_id" => "staged"},
               Ecto.UUID.generate(),
               opts
             )

    assert input.payload == %{"option_id" => "staged"}
    assert {:error, :state_conflict} = command(task, "pause")
    assert {:error, :invalid_request} = transition(run, fence, "running", %{})
    assert {:ok, _} = transition(run, fence, "running", %{"command_id" => input.id})
    assert {:ok, %{state: "applied"}} = Orchestration.fetch_command(input.id)
    assert {:ok, _} = transition(run, fence, "completed", %{"summary" => "Migration staged."})
    assert {:ok, acknowledged} = acknowledge(input, fence)
    assert acknowledged.acknowledgement_outcome == "applied"
  end

  test "later waiting diagnostics cannot replace the decision that a human is authorizing" do
    {task, run, _runtime, fence} = running()
    assert {:ok, _} = transition(run, fence, "waiting_for_input", packet())
    altered = put_in(packet(), ["question"], "Unrelated later diagnostic")

    assert {:ok, [_]} =
             Orchestration.append_events(
               run.id,
               fence,
               [
                 %{
                   event_id: Ecto.UUID.generate(),
                   sequence: 10,
                   kind: "waiting_for_input",
                   payload: altered,
                   occurred_at: @now
                 }
               ],
               now: @now
             )

    assert {:ok, snapshot} = Orchestration.task_snapshot(task.id)
    assert snapshot.waiting.question == packet()["question"]
  end

  defp legacy_command_hash(command, kind, payload) do
    hash = :crypto.hash(:sha256, :erlang.term_to_binary(%{kind: kind, payload: payload}))

    command
    |> Command.changeset(%{request_hash: hash, request_hash_version: 1})
    |> Repo.update!()
  end

  defp linked_work_item(task) do
    assert {:ok, project} =
             Workspaces.create_project(%{
               name: "Upgrade project",
               key: "UPGRADE",
               default_agent_profile: "supervised",
               default_workspace: "primary"
             })

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Preserve control history",
               status: "ready",
               assignee_type: "agent"
             })

    item
    |> SymmetryControl.Workspaces.WorkItem.execution_changeset(%{
      orchestration_task_id: task.id,
      status: "in_progress"
    })
    |> Repo.update!()
  end

  defp running(opts \\ []) do
    machine = machine()
    runtime_capabilities = Keyword.get(opts, :capabilities, capabilities())

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               Ecto.UUID.generate(),
               [runtime_spec(runtime_capabilities)],
               now: @now
             )

    attrs = task_attrs()

    attrs =
      if Keyword.get(opts, :supervised, true),
        do: Map.put(attrs, :required_capabilities, %{"supervisory_control" => true}),
        else: attrs

    assert {:ok, task, :created} =
             Orchestration.submit_task(attrs, Ecto.UUID.generate(), now: @now)

    assert {:ok, run} = Orchestration.assign_one(now: @now)

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

    fence = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: claimed.claim_id,
      lease_token: claimed.lease_token
    }

    assert {:ok, run} = transition(run, fence, "running", %{})
    {task, run, runtime, fence}
  end

  defp machine do
    key = Ecto.UUID.generate()

    assert {:ok, %{machine: machine}, :created} =
             Orchestration.enroll_machine(
               %{name: "Supervision builder", machine_token: key},
               key,
               enrollment_token: "secret",
               expected_enrollment_token: "secret",
               now: @now
             )

    machine
  end

  defp task_attrs,
    do: %{
      goal: "Build the adapter",
      agent_profile: "supervised",
      workspace: "primary",
      input: %{}
    }

  defp runtime_spec(capabilities),
    do: %{
      runtime_key: "supervised",
      name: "Supervised runtime",
      capacity: 1,
      agent_profile: "supervised",
      workspace: "primary",
      capabilities: capabilities
    }

  defp capabilities,
    do: %{"structured_input" => true, "interactive" => true, "supervisory_control" => true}

  defp command(task, kind, payload \\ %{}, key \\ Ecto.UUID.generate()),
    do:
      Orchestration.create_command(task.id, kind, payload, key,
        expected_generation: task.attempt_generation,
        now: @now
      )

  defp transition(run, fence, state, payload),
    do: Orchestration.transition(run.id, fence, state, payload, Ecto.UUID.generate(), now: @now)

  defp acknowledge(command, fence, outcome \\ "applied"),
    do:
      Orchestration.acknowledge_command(command.id, fence, outcome, Ecto.UUID.generate(),
        now: @now
      )

  defp packet do
    %{
      "question" => "Which migration strategy should be used?",
      "decision" => %{
        "reason" => "irreversible",
        "context" => "The migration removes the legacy column.",
        "recommended_option_id" => "staged",
        "options" => [
          %{
            "id" => "staged",
            "label" => "Stage migration",
            "consequence" => "Keeps rollback available."
          },
          %{"id" => "defer", "label" => "Defer", "consequence" => "Leaves this work blocked."}
        ]
      }
    }
  end
end
