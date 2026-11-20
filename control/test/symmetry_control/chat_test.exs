defmodule SymmetryControl.ChatTest do
  use SymmetryControl.DataCase, async: false

  alias SymmetryControl.{Chat, Orchestration, Workspaces}
  alias SymmetryControl.Chat.{Action, Message}
  alias SymmetryControl.Orchestration.{Command, Machine, Run, Runtime, Task}
  alias SymmetryControl.Workspaces.{Project, WorkItem}
  alias SymmetryControl.TelemetryReads
  alias Ecto.Adapters.SQL.Sandbox

  test "reading a conversation does not create durable conversation state" do
    assert {:ok, %{messages: [], runs: [], next_before: nil}} =
             Chat.conversation(%{scope: "workspace"})

    assert Repo.aggregate(Message, :count) == 0
    assert Repo.aggregate(Action, :count) == 0
  end

  test "workspace start atomically retains the complete goal and replays identical actions" do
    project = project("CHAT")
    goal = "Implement durable notifications.\n" <> String.duplicate("Keep retries safe. ", 200)

    params = %{
      scope: "workspace",
      target_project_id: project.id,
      intent: "start_work",
      action_id: "start-one",
      content: goal,
      work: %{title: "Durable notifications"}
    }

    assert {:ok, response, :created} = Chat.post_message(params)
    assert {:ok, replay, :replayed} = Chat.post_message(params)
    assert response == replay
    assert Repo.aggregate(WorkItem, :count) == 1
    assert Repo.aggregate(Task, :count) == 1
    assert Repo.aggregate(Message, :count) == 2
    assert Repo.aggregate(Action, :count) == 1
    assert Repo.get_by!(Action, action_id: "start-one").request_hash_version == 1

    item = Repo.get!(WorkItem, response.work_item_id)
    task = Repo.get!(Task, item.orchestration_task_id)
    assert item.description == goal
    assert task.goal == "Durable notifications\n\n" <> String.trim(goal)
    assert response.message.content == goal
    assert task.required_capabilities["supervisory_control"] == true
    assert task.state == "queued"

    assert {:ok, workspace} = Chat.conversation(%{scope: "workspace"})
    assert length(workspace.messages) == 2
    assert workspace.project_id == nil
    assert {:ok, %{messages: []}} = Chat.conversation(%{scope: "project", project_id: project.id})

    assert {:error, :idempotency_conflict} =
             Chat.post_message(put_in(params, [:work, :title], "Changed title"))

    other_project = project("OTHER")

    assert {:error, :idempotency_conflict} =
             Chat.post_message(%{params | target_project_id: other_project.id})

    assert Repo.aggregate(WorkItem, :count) == 1
  end

  test "a native-only execution binding admits Chat work through the required native operations" do
    project = project("NATIVE")
    now = DateTime.utc_now()
    runtime = register_native_runtime(now)

    assert {:ok, response, :created} =
             Chat.post_message(%{
               scope: "workspace",
               target_project_id: project.id,
               intent: "start_work",
               action_id: "native-start",
               content: "Implement the native start path",
               work: %{title: "Native start"}
             })

    item = Repo.get!(WorkItem, response.work_item_id)
    task = Repo.get!(Task, item.orchestration_task_id)

    assert task.required_capabilities == %{
             "adapter" => %{
               "operations" => %{
                 "start" => true,
                 "events" => true,
                 "cancel" => true,
                 "pause" => "unsupported"
               }
             }
           }

    assert {:ok, run} = Orchestration.assign_one(now: now)
    assert run.task_id == task.id
    assert run.runtime_id == runtime.id
  end

  test "a registered legacy runtime keeps the supervisory requirement and excludes native runtimes" do
    project = project("MIXED")
    now = DateTime.utc_now()
    register_native_runtime(now)
    legacy_runtime = register_legacy_runtime(now)

    assert {:ok, response, :created} =
             Chat.post_message(%{
               scope: "workspace",
               target_project_id: project.id,
               intent: "start_work",
               action_id: "mixed-start",
               content: "Implement the legacy start path",
               work: %{title: "Mixed start"}
             })

    item = Repo.get!(WorkItem, response.work_item_id)
    task = Repo.get!(Task, item.orchestration_task_id)
    assert task.required_capabilities == %{"supervisory_control" => true}

    assert {:ok, run} = Orchestration.assign_one(now: now)
    assert run.task_id == task.id
    assert run.runtime_id == legacy_runtime.id
  end

  test "a native harness kind outside the Chat vocabulary is admitted natively" do
    project = project("SHARED")
    now = DateTime.utc_now()
    # `omo` is native for Orchestration's own admission vocabulary, while the
    # Chat start used to keep its own four-kind copy of that list. The
    # derivation asks `Orchestration.native_harness_kind?/1` now, so every
    # registered runtime of this binding is native and the start is admitted
    # through the adapter operations.
    omo = register_native_runtime(now, harness_kind: "omo", runtime_key: "omo-chat")
    codex = register_native_runtime(now, harness_kind: "codex", runtime_key: "codex-chat")
    assert Orchestration.native_harness_kind?("omo")
    refute Orchestration.native_harness_kind?(nil)
    refute Orchestration.native_harness_kind?("generic")

    assert {:ok, response, :created} =
             Chat.post_message(%{
               scope: "workspace",
               target_project_id: project.id,
               intent: "start_work",
               action_id: "shared-vocabulary",
               content: "Start through the shared native vocabulary",
               work: %{title: "Shared vocabulary"}
             })

    item = Repo.get!(WorkItem, response.work_item_id)
    task = Repo.get!(Task, item.orchestration_task_id)

    assert task.required_capabilities == %{
             "adapter" => %{
               "operations" => %{
                 "start" => true,
                 "events" => true,
                 "cancel" => true,
                 "pause" => "unsupported"
               }
             }
           }

    # The stored requirement is the claimable one. `omo` itself can never claim
    # a goal-less task — the claim query excludes it — and the native
    # requirement lets the binding's other native runtime claim it instead of
    # stranding it behind the legacy supervisory flag no native runtime
    # advertises.
    assert {:ok, run} = Orchestration.assign_one(now: now)
    assert run.task_id == task.id
    assert run.runtime_id == codex.id
    refute run.runtime_id == omo.id
  end

  test "a binding change while the launch waits for the work item lock cannot desynchronise the stored requirement" do
    # The admission requirement comes from the work item's execution binding, and
    # the launch reads the work item before it locks it. The derivation must run
    # inside the launch transaction on the locked row, so this test holds that
    # row's lock, lets a real launcher reach the lock, changes the binding, and
    # then asserts the stored requirement describes the row the launch locked.
    Sandbox.unboxed_run(Repo, fn ->
      now = DateTime.utc_now()
      project = project("BIND", %{default_agent_profile: "native-binding"})
      register_native_runtime(now, agent_profile: "native-binding", runtime_key: "binding-native")

      legacy_runtime =
        register_legacy_runtime(now,
          agent_profile: "legacy-binding",
          runtime_key: "binding-legacy"
        )

      assert {:ok, item} =
               Workspaces.create_work_item(project.id, %{
                 title: "Derive under the lock",
                 description: "The stored requirement must describe the locked work item.",
                 agent_profile: "native-binding",
                 status: "ready",
                 assignee_type: "agent"
               })

      # Registered before the risky section: these rows are committed outside the
      # sandbox so the launcher's own connection can see them, and a later
      # assertion failure must not leave them behind for the suite's counts.
      on_exit(fn -> purge_binding_race_fixture(project.key) end)

      parent = self()

      # The launcher runs outside this test's sandbox transaction on its own
      # connection: the row locks have to be real for the launch to wait on one.
      launcher =
        Elixir.Task.async(fn ->
          Sandbox.unboxed_run(Repo, fn ->
            [[backend]] = Repo.query!("SELECT pg_backend_pid()").rows
            send(parent, {:launcher_backend, backend})

            Chat.post_message(%{
              scope: "workspace",
              target_project_id: project.id,
              work_item_id: item.id,
              generation: 0,
              intent: "start_work",
              action_id: "binding-lock",
              content: "Start after the binding changed"
            })
          end)
        end)

      assert_receive {:launcher_backend, launcher_backend}, 15_000

      assert {:ok, _} =
               Repo.transaction(fn ->
                 [[locker_backend]] = Repo.query!("SELECT pg_backend_pid()").rows

                 assert Repo.one!(
                          from work_item in WorkItem,
                            where: work_item.id == ^item.id,
                            lock: "FOR UPDATE"
                        )

                 # The launcher has read the work item, and is now waiting for
                 # this lock: the row it will lock is the legacy binding.
                 assert_launch_waiting_on_work_item(launcher_backend, locker_backend)

                 Repo.get!(WorkItem, item.id)
                 |> WorkItem.changeset(%{agent_profile: "legacy-binding"})
                 |> Repo.update!()
               end)

      assert {:ok, response, :created} = Elixir.Task.await(launcher, 30_000)
      launched_item = Repo.get!(WorkItem, response.work_item_id)
      task = Repo.get!(Task, launched_item.orchestration_task_id)

      assert launched_item.agent_profile == "legacy-binding"
      assert task.agent_profile == "legacy-binding"
      assert task.required_capabilities == %{"supervisory_control" => true}

      assert {:ok, run} = Orchestration.assign_one(now: now)
      assert run.task_id == task.id
      assert run.runtime_id == legacy_runtime.id
    end)
  end

  test "an invalid work request leaves no partial work or conversation records" do
    project = project("INVALID")

    assert {:error, _reason} =
             Chat.post_message(%{
               scope: "project",
               project_id: project.id,
               intent: "start_work",
               action_id: "invalid-launch",
               content: "Implement a real feature",
               work: %{repository_resource_id: Ecto.UUID.generate()}
             })

    assert Repo.aggregate(WorkItem, :count) == 0
    assert Repo.aggregate(Task, :count) == 0
    assert Repo.aggregate(Message, :count) == 0
    assert Repo.aggregate(Action, :count) == 0
  end

  test "an existing ready work item is launched without duplicating it or replacing its goal" do
    project = project("EXIST")

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Existing goal",
               description: "Preserve this agreed goal",
               status: "ready",
               assignee_type: "agent"
             })

    params = %{
      scope: "project",
      project_id: project.id,
      intent: "start_work",
      action_id: "assign-existing",
      content: "Start the existing work",
      work_item_id: item.id,
      generation: 0
    }

    assert {:ok, response, :created} = Chat.post_message(params)
    assert response.work_item_id == item.id
    assert Repo.aggregate(WorkItem, :count) == 1
    assert Repo.one!(Task).goal == "Existing goal\n\nPreserve this agreed goal"

    assert {:error, :state_conflict} = Chat.post_message(%{params | action_id: "start-again"})
    assert Repo.aggregate(Message, :count) == 2
  end

  test "message persistence failure rolls back an already-created work item and task" do
    project = project("ATOMIC")

    Ecto.Adapters.SQL.query!(
      Repo,
      "ALTER TABLE chat_messages ADD CONSTRAINT reject_chat_test_write CHECK (role <> 'human')",
      []
    )

    assert_raise Ecto.ConstraintError, fn ->
      Chat.post_message(%{
        scope: "project",
        project_id: project.id,
        intent: "start_work",
        action_id: "failed-persistence",
        content: "Implement an atomic work request"
      })
    end

    assert Repo.aggregate(WorkItem, :count) == 0
    assert Repo.aggregate(Task, :count) == 0
    assert Repo.aggregate(Message, :count) == 0
    assert Repo.aggregate(Action, :count) == 0
  end

  test "discussion records durable evidence without interpreting text as a control command" do
    {project, item, run, fence, now} = live_work("DISCUSS")
    append_event(run, fence, now, "summary", %{summary: "The adapter is implemented."}, 1)
    append_event(run, fence, now, "finding", %{message: "Retries must reuse the request ID."}, 2)
    append_event(run, fence, now, "tool_output", %{message: "PRIVATE RAW OUTPUT"}, 3)
    before_task = Repo.get!(Task, item.orchestration_task_id)

    assert {:ok, response, :created} =
             Chat.post_message(%{
               scope: "run",
               run_id: run.id,
               intent: "discuss",
               action_id: "question",
               content: "Would you cancel this run? What happened?"
             })

    assert response.command == nil
    assert response.reply.content =~ "The adapter is implemented."
    assert response.reply.content =~ "Retries must reuse the request ID."
    refute response.reply.content =~ "PRIVATE RAW OUTPUT"
    assert Repo.aggregate(Command, :count) == 0
    assert Repo.get!(Task, item.orchestration_task_id) == before_task

    assert {:ok, conversation} = Chat.conversation(%{scope: "run", run_id: run.id})
    assert conversation.project_id == project.id
    assert hd(conversation.runs).work_item.execution.run_id == run.id
    refute Jason.encode!(conversation) =~ "PRIVATE RAW OUTPUT"
    assert Enum.map(conversation.messages, & &1.role) == ["human", "assistant"]
  end

  test "guidance is durable, replayable, and its message reflects the current acknowledgement" do
    {_project, item, run, fence, now} = live_work("GUIDE")

    params = %{
      scope: "run",
      run_id: run.id,
      work_item_id: item.id,
      generation: run.generation,
      intent: "guidance",
      action_id: "guide",
      content: "Reuse the existing adapter."
    }

    assert {:ok, response, :created} = Chat.post_message(params)
    assert response.command.state == "pending"
    assert response.command.payload == %{"message" => "Reuse the existing adapter."}
    assert Repo.get!(Task, item.orchestration_task_id).state == "running"
    assert {:ok, replay, :replayed} = Chat.post_message(params)
    assert replay.command.command_id == response.command.command_id
    assert Repo.aggregate(Command, :count) == 1
    assert {:error, :idempotency_conflict} = Chat.post_message(%{params | content: "Replace it."})

    assert {:ok, _command} =
             Orchestration.acknowledge_command(
               response.command.command_id,
               fence,
               "applied",
               Ecto.UUID.generate(),
               now: now
             )

    assert {:ok, conversation} = Chat.conversation(%{scope: "run", run_id: run.id})
    assert Enum.all?(conversation.messages, &(&1.command.acknowledgement_outcome == "applied"))
  end

  test "guidance byte limits preserve exact ASCII and multibyte boundaries without partial records" do
    {_project, item, run, _fence, _now} = live_work("BYTES")

    params = %{
      scope: "run",
      run_id: run.id,
      work_item_id: item.id,
      generation: run.generation,
      intent: "guidance"
    }

    boundaries = [
      {"ascii", String.duplicate("a", 32_768)},
      {"multibyte", String.duplicate("\u00E9", 16_384)}
    ]

    for {label, boundary} <- boundaries do
      oversized = boundary <> "x"
      assert byte_size(oversized) == 32_769

      assert {:error, :invalid_request} =
               Chat.post_message(
                 Map.merge(
                   params,
                   %{action_id: "oversized-#{label}", content: oversized}
                 )
               )

      assert Repo.aggregate(Message, :count) == 0
      assert Repo.aggregate(Action, :count) == 0
      assert Repo.aggregate(Command, :count) == 0
    end

    for {label, boundary} <- boundaries do
      assert byte_size(boundary) == 32_768

      assert {:ok, response, :created} =
               Chat.post_message(
                 Map.merge(
                   params,
                   %{action_id: "boundary-#{label}", content: boundary}
                 )
               )

      assert response.message.content == boundary
      assert response.command.payload["message"] == boundary
    end

    assert Repo.aggregate(Message, :count) == 4
    assert Repo.aggregate(Action, :count) == 2
    assert Repo.aggregate(Command, :count) == 2
  end

  test "questions select recorded rationale, tests, and outcome rather than repeating one status answer" do
    {_project, _item, run, fence, now} = live_work("QUEST")

    params = %{
      scope: "run",
      run_id: run.id,
      intent: "discuss",
      action_id: "why-missing",
      content: "Why did you choose this adapter?"
    }

    assert {:ok, missing, :created} = Chat.post_message(params)
    assert missing.reply.content =~ "No rationale has been recorded"

    append_event(
      run,
      fence,
      now,
      "rationale",
      %{message: "The existing adapter retains the tested retry semantics."},
      1
    )

    append_event(
      run,
      fence,
      now,
      "test",
      %{name: "response-loss replay", status: "passed", summary: "Covers duplicate delivery."},
      2
    )

    append_event(run, fence, now, "summary", %{summary: "The durable adapter is implemented."}, 3)

    assert {:ok, why, :created} = Chat.post_message(%{params | action_id: "why-recorded"})
    assert why.reply.content =~ "The existing adapter retains the tested retry semantics."
    refute why.reply.content =~ "response-loss replay"

    assert {:ok, tests, :created} =
             Chat.post_message(%{
               params
               | action_id: "which-tests",
                 content: "Which tests have run?"
             })

    assert tests.reply.content =~ "Recorded tests: response-loss replay"
    assert tests.reply.content =~ "passed"
    refute tests.reply.content =~ "The durable adapter is implemented."

    assert {:ok, outcome, :created} =
             Chat.post_message(%{
               params
               | action_id: "what-outcome",
                 content: "What is the outcome?"
             })

    assert outcome.reply.content =~ "The durable adapter is implemented."
    refute outcome.reply.content =~ "response-loss replay"
    assert Repo.aggregate(Command, :count) == 0
  end

  test "status and outcome questions explain a recorded daemon error without creating commands" do
    {_project, item, run, fence, now} = live_work("ERROR")
    error = "A supervised waiting request must include a valid decision packet."

    assert {:ok, _run} =
             Orchestration.transition(
               run.id,
               fence,
               "failed",
               %{error: error},
               Ecto.UUID.generate(),
               now: now
             )

    failed_task = Repo.get!(Task, item.orchestration_task_id)

    for {intent, content} <- [
          {"status", "What is happening?"},
          {"discuss", "What was the outcome?"}
        ] do
      assert {:ok, response, :created} =
               Chat.post_message(%{
                 scope: "run",
                 run_id: run.id,
                 intent: intent,
                 action_id: "failure-#{intent}",
                 content: content
               })

      assert response.reply.content =~ "Failure: #{error}"
      assert response.command == nil
      assert Repo.get!(Task, item.orchestration_task_id) == failed_task
    end

    assert Repo.aggregate(Command, :count) == 0
  end

  test "project and generation boundaries reject controls without persisting messages" do
    {_project, item, run, _fence, _now} = live_work("BOUND")
    other = project("BNDOTHER")

    params = %{
      scope: "project",
      project_id: other.id,
      work_item_id: item.id,
      generation: run.generation,
      intent: "pause",
      action_id: "wrong-project",
      content: "Pause"
    }

    assert {:error, :not_found} = Chat.post_message(params)

    assert {:error, :state_conflict} =
             Chat.post_message(%{
               scope: "run",
               run_id: run.id,
               work_item_id: item.id,
               generation: run.generation + 1,
               intent: "pause",
               action_id: "stale-generation",
               content: "Pause"
             })

    assert {:error, :invalid_request} =
             Chat.post_message(%{
               scope: "run",
               run_id: run.id,
               work_item_id: item.id,
               intent: "pause",
               action_id: "missing-generation",
               content: "Pause"
             })

    assert Repo.aggregate(Command, :count) == 0
    assert Repo.aggregate(Message, :count) == 0
  end

  test "decision replies bind the waiting transition and selected option atomically" do
    {_project, item, run, fence, now} = live_work("DECIDE")
    waiting_id = Ecto.UUID.generate()

    packet = %{
      question: "Which migration strategy?",
      decision: %{
        reason: "irreversible",
        context: "The migration removes an existing column.",
        recommended_option_id: "staged",
        options: [
          %{id: "staged", label: "Stage it", consequence: "Retains rollback."},
          %{id: "defer", label: "Defer", consequence: "Leaves the work blocked."}
        ]
      }
    }

    assert {:ok, _run} =
             Orchestration.transition(run.id, fence, "waiting_for_input", packet, waiting_id,
               now: now
             )

    params = %{
      scope: "run",
      run_id: run.id,
      work_item_id: item.id,
      generation: run.generation,
      intent: "decision",
      action_id: "decision",
      content: "Stage it safely.",
      waiting_transition_id: waiting_id,
      option_id: "unknown"
    }

    assert {:error, _reason} = Chat.post_message(params)
    assert Repo.aggregate(Message, :count) == 0
    assert Repo.aggregate(Command, :count) == 0

    assert {:error, _reason} =
             Chat.post_message(%{
               params
               | option_id: "staged",
                 waiting_transition_id: Ecto.UUID.generate()
             })

    assert {:ok, result, :created} = Chat.post_message(%{params | option_id: "staged"})
    assert result.command.kind == "provide_input"
    assert result.command.payload["option_id"] == "staged"
    assert Repo.aggregate(Message, :count) == 2
    assert Repo.aggregate(Command, :count) == 1
  end

  test "historical run chat remains pinned after retry and cannot control a newer attempt" do
    {project, item, run, fence, now} = live_work("HISTORY")

    append_semantic_events(run, fence, now, [
      {"summary", %{summary: "Original attempt summary"}},
      {"finding", %{message: "Original finding"}},
      {"artifact", %{path: "original.txt"}},
      {"test", %{name: "original-test", status: "passed"}},
      {"pull_request", %{url: "https://github.com/acme/repo/pull/1"}},
      {"ci", %{status: "failed"}},
      {"review", %{status: "changes_requested"}}
    ])

    # Evidence older than the timeline page must remain available to the outcome.
    progress_time = DateTime.add(now, 1, :millisecond)

    progress =
      for sequence <- 10..70 do
        %{
          event_id: Ecto.UUID.generate(),
          sequence: sequence,
          kind: "progress",
          payload: %{message: "Later progress #{sequence}"},
          occurred_at: progress_time
        }
      end

    assert {:ok, _events} =
             Orchestration.append_events(run.id, fence, progress, now: progress_time)

    assert {:ok, _run} =
             Orchestration.transition(
               run.id,
               fence,
               "failed",
               %{message: "Original failure"},
               Ecto.UUID.generate(),
               now: DateTime.add(now, 2, :millisecond)
             )

    assert {:ok, _snapshot, :created} =
             Workspaces.retry_work_item(item.id, run.generation, "retry")

    newer_time = DateTime.utc_now()
    assert {:ok, newer_run} = Orchestration.assign_one(now: newer_time)

    assert {:ok, claimed} =
             Orchestration.claim(
               newer_run.id,
               %{
                 runtime_id: fence.runtime_id,
                 runtime_epoch: fence.runtime_epoch,
                 generation: newer_run.generation,
                 claim_id: Ecto.UUID.generate()
               },
               now: newer_time
             )

    newer_fence = %{
      fence
      | generation: newer_run.generation,
        claim_id: claimed.claim_id,
        lease_token: claimed.lease_token
    }

    assert {:ok, newer_run} =
             Orchestration.transition(
               newer_run.id,
               newer_fence,
               "running",
               %{},
               Ecto.UUID.generate(),
               now: newer_time
             )

    append_semantic_events(newer_run, newer_fence, newer_time, [
      {"summary", %{summary: "Newer attempt summary"}},
      {"finding", %{message: "Newer finding"}},
      {"artifact", %{path: "newer.txt"}},
      {"test", %{name: "newer-test", status: "passed"}},
      {"pull_request", %{url: "https://github.com/acme/repo/pull/2"}},
      {"ci", %{status: "passed"}},
      {"review", %{status: "approved"}}
    ])

    assert {:ok, historical} = Chat.conversation(%{scope: "run", run_id: run.id})
    detail = hd(historical.runs)
    assert detail.work_item.execution.run_id == run.id
    assert detail.work_item.execution.generation == run.generation
    assert detail.work_item.execution.state == "failed"
    assert detail.work_item.execution.historical
    refute detail.work_item.execution.can_cancel
    assert detail.outcome.failure["message"] == "Original failure"
    assert detail.outcome.summary == "Original attempt summary"
    assert Enum.map(detail.outcome.findings, & &1.message) == ["Original finding"]
    assert Enum.map(detail.outcome.changed_artifacts, & &1.path) == ["original.txt"]
    assert Enum.map(detail.outcome.tests, &{&1.name, &1.status}) == [{"original-test", "passed"}]
    assert detail.outcome.pull_request_url == "https://github.com/acme/repo/pull/1"
    assert detail.outcome.ci_status == "failed"
    assert detail.outcome.review_status == "changes_requested"
    assert detail.outcome.delivery.ci.generation == run.generation
    refute Enum.any?(detail.timeline, &(&1.source == "event" and &1.data.kind == "test"))

    assert {:ok, project_chat} = Chat.conversation(%{scope: "project", project_id: project.id})
    assert hd(project_chat.runs).work_item.execution.generation == run.generation + 1
    assert hd(project_chat.runs).work_item.execution.state == "running"
    assert hd(project_chat.runs).outcome.summary == "Newer attempt summary"
    assert hd(project_chat.runs).outcome.pull_request_url == "https://github.com/acme/repo/pull/2"
    assert hd(project_chat.runs).outcome.ci_status == "passed"

    assert {:error, :state_conflict} =
             Chat.post_message(%{
               scope: "run",
               run_id: run.id,
               work_item_id: item.id,
               generation: run.generation + 1,
               intent: "cancel",
               action_id: "cancel-new-via-old",
               content: "Cancel"
             })

    assert Repo.aggregate(Message, :count) == 0

    questions = [
      {"tests", "Which tests ran?", "original-test", "newer-test"},
      {"artifacts", "Which files changed?", "original.txt", "newer.txt"},
      {"findings", "What were the findings?", "Original finding", "Newer finding"},
      {"delivery", "What are the PR and CI results?", "/pull/1", "/pull/2"},
      {"outcome", "What was the outcome?", "Original attempt summary", "Newer attempt summary"}
    ]

    for {label, question, expected, excluded} <- questions do
      assert {:ok, response, :created} =
               Chat.post_message(%{
                 scope: "run",
                 run_id: run.id,
                 intent: "discuss",
                 action_id: "historical-#{label}",
                 content: question
               })

      assert response.reply.content =~ expected
      refute response.reply.content =~ excluded
      refute response.reply.content =~ "No #{label}"
    end
  end

  test "batched timelines retain each run's history and match single-run ordering with three queries" do
    fixtures =
      for {key, count} <- [{"BATCHA", 65}, {"BATCHB", 65}, {"BATCHC", 3}] do
        {_project, item, run, fence, now} = live_work(key)

        events =
          for sequence <- 1..(count + 80) do
            %{
              event_id: Ecto.UUID.generate(),
              sequence: sequence,
              kind: if(sequence <= count, do: "progress", else: "tool_output"),
              payload: %{message: "#{key} event #{sequence}"},
              occurred_at: now
            }
          end

        assert {:ok, _events} = Orchestration.append_events(run.id, fence, events, now: now)
        command_time = DateTime.add(now, 1, :millisecond)

        assert {:ok, _guidance, :created} =
                 Orchestration.create_command(
                   item.orchestration_task_id,
                   "guidance",
                   %{message: "Preserve #{key} history"},
                   "batch-guidance-#{key}",
                   expected_generation: run.generation,
                   now: command_time
                 )

        assert {:ok, pause, :created} =
                 Orchestration.create_command(
                   item.orchestration_task_id,
                   "pause",
                   %{},
                   "batch-pause-#{key}",
                   expected_generation: run.generation,
                   now: command_time
                 )

        assert {:ok, _paused} =
                 Orchestration.transition(
                   run.id,
                   fence,
                   "paused",
                   %{command_id: pause.id},
                   Ecto.UUID.generate(),
                   now: DateTime.add(now, 2, :millisecond)
                 )

        {run, count}
      end

    owner = self()
    ref = make_ref()
    handler_id = {__MODULE__, ref}

    :ok =
      :telemetry.attach(
        handler_id,
        [:symmetry_control, :repo, :query],
        fn _event, _measurements, metadata, _config ->
          if self() == owner and String.contains?(metadata.query, "row_number()") do
            send(owner, {:batch_timeline_query, ref})
          end
        end,
        nil
      )

    workspace =
      try do
        assert {:ok, workspace} = Chat.conversation(%{scope: "workspace"})
        workspace
      after
        :telemetry.detach(handler_id)
      end

    for _ <- 1..3, do: assert_received({:batch_timeline_query, ^ref})
    refute_received {:batch_timeline_query, ^ref}

    for {run, count} <- fixtures do
      detail = Enum.find(workspace.runs, &(&1.work_item.execution.run_id == run.id))
      assert {:ok, single} = Chat.conversation(%{scope: "run", run_id: run.id})
      assert detail.timeline == hd(single.runs).timeline
      assert length(detail.timeline) == min(count + 4, 50)
      assert Enum.all?(detail.timeline, &(&1.run_id == run.id))
      assert Enum.any?(detail.timeline, &(&1.source == "command"))
      assert Enum.any?(detail.timeline, &(&1.source == "transition"))
      refute Enum.any?(detail.timeline, &(&1.source == "event" and &1.data.kind == "tool_output"))
    end
  end

  test "message pagination is stable and a cursor cannot cross contexts" do
    project = project("PAGE")

    Enum.each(1..26, fn number ->
      assert {:ok, _, :created} =
               Chat.post_message(%{
                 scope: "workspace",
                 intent: "discuss",
                 action_id: "page-#{number}",
                 content: "Question #{number}"
               })
    end)

    assert {:ok, first} = Chat.conversation(%{scope: "workspace"})
    assert length(first.messages) == 50
    assert is_binary(first.next_before)
    assert Enum.all?(Repo.all(Action), &(&1.request_hash_version == 1))

    assert {:ok, second} = Chat.conversation(%{scope: "workspace", before: first.next_before})
    assert length(second.messages) == 2
    assert second.next_before == nil
    all_ids = Enum.map(second.messages ++ first.messages, & &1.id)
    assert length(Enum.uniq(all_ids)) == 52

    assert {:error, :invalid_request} =
             Chat.conversation(%{
               scope: "project",
               project_id: project.id,
               before: first.next_before
             })

    assert {:error, :invalid_request} =
             Chat.conversation(%{scope: "workspace", before: "invalid"})
  end

  test "a workspace conversation read keeps every query on the calling process" do
    project = project("CHR")

    assert {:ok, connection} =
             SymmetryControl.Integrations.create_connection(%{
               provider: "github",
               name: "chat-read-" <> Ecto.UUID.generate(),
               account_ref: "acme",
               auth_type: "gh_cli",
               capabilities: ["repositories", "work_items", "changes", "ci"]
             })

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "repository",
               name: "Chat read repository",
               external_ref: "acme/chat-read"
             })

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Conversation read on one connection",
               status: "ready",
               repository_resource_id: repository.id
             })

    reader = self()

    forms = [
      project.id,
      Ecto.UUID.dump!(project.id),
      item.id,
      Ecto.UUID.dump!(item.id),
      repository.id,
      Ecto.UUID.dump!(repository.id)
    ]

    handler_id = {__MODULE__, :conversation_read, project.id}

    :telemetry.attach(
      handler_id,
      [:symmetry_control, :repo, :query],
      fn _event, _measurements, metadata, %{reader: reader, forms: forms} ->
        if TelemetryReads.read?(metadata.params, forms),
          do: send(reader, {:conversation_read, self(), to_string(metadata.query)})
      end,
      %{reader: reader, forms: forms}
    )

    on_exit(fn -> :telemetry.detach(handler_id) end)

    assert {:ok, conversation} = Chat.conversation(%{scope: "workspace"})
    assert is_list(conversation.runs)

    reads = TelemetryReads.drain(:conversation_read)
    queries = Enum.map(reads, fn {_pid, query} -> query end)
    processes = reads |> Enum.map(&elem(&1, 0)) |> Enum.uniq() |> Enum.sort()

    # Two preloads must have rows for this test to have power: Ecto only
    # preloads in parallel from two preloads with fetch work, and every parallel
    # preload would queue for the sandbox connection the test process owns.
    assert Enum.any?(queries, &String.contains?(&1, "\"projects\""))
    assert Enum.any?(queries, &String.contains?(&1, "\"project_resources\""))

    assert processes == [reader],
           "expected every conversation read query on #{inspect(reader)}, got #{inspect(processes)}"
  end

  # Bounded wait on observable server state, never a sleep: the launcher is
  # blocked on the locker's `work_items` row lock when PostgreSQL reports an
  # ungranted lock for the launcher's own backend, names the locker in
  # `pg_blocking_pids/1`, and shows that blocker holding a lock on `work_items`.
  # (The launcher's statement text is not usable evidence here:
  # `pg_stat_activity.query` truncates the long column list before the table.)
  defp assert_launch_waiting_on_work_item(backend, blocker, deadline \\ nil) do
    deadline = deadline || System.monotonic_time(:millisecond) + 10_000

    [[waiting?]] =
      Repo.query!(
        """
        SELECT EXISTS (
                 SELECT 1 FROM pg_locks
                 WHERE pid = $1 AND NOT granted
               )
               AND EXISTS (
                 SELECT 1 FROM pg_locks held
                 WHERE held.pid = $2 AND held.granted AND held.relation = 'work_items'::regclass
               )
               AND $2::integer = ANY(pg_blocking_pids($1))
        """,
        [backend, blocker]
      ).rows

    if waiting? do
      :ok
    else
      assert System.monotonic_time(:millisecond) < deadline,
             "the launch never waited for the locked WorkItem"

      assert_launch_waiting_on_work_item(backend, blocker, deadline)
    end
  end

  # Removes the committed rows the lock-race fixture writes outside the sandbox.
  # Keyed by identity, so it deletes exactly this fixture whether the test
  # reached its assertions or failed before them.
  defp purge_binding_race_fixture(project_key) do
    Sandbox.unboxed_run(Repo, fn ->
      project = Repo.one(from project in Project, where: project.key == ^project_key)

      items =
        if project,
          do: Repo.all(from item in WorkItem, where: item.project_id == ^project.id),
          else: []

      item_ids = Enum.map(items, & &1.id)
      task_ids = items |> Enum.map(& &1.orchestration_task_id) |> Enum.filter(&is_binary/1)
      project_ids = if project, do: [project.id], else: []

      Repo.delete_all(from action in Action, where: action.action_id == "binding-lock")
      Repo.delete_all(from message in Message, where: message.work_item_id in ^item_ids)
      Repo.delete_all(from run in Run, where: run.task_id in ^task_ids)
      Repo.delete_all(from task in Task, where: task.id in ^task_ids)
      Repo.delete_all(from item in WorkItem, where: item.project_id in ^project_ids)
      Repo.delete_all(from project_row in Project, where: project_row.id in ^project_ids)

      Repo.delete_all(
        from machine in Machine,
          where:
            machine.id in subquery(
              from runtime in Runtime,
                where: runtime.runtime_key in ^["binding-native", "binding-legacy"],
                select: runtime.machine_id
            )
      )
    end)
  end

  defp project(key, attrs \\ %{}) do
    assert {:ok, project} =
             Workspaces.create_project(
               Map.merge(
                 %{
                   name: "Project #{key}",
                   key: key,
                   default_agent_profile: "codex",
                   default_workspace: "primary"
                 },
                 attrs
               )
             )

    project
  end

  defp live_work(key) do
    project = project(key)

    assert {:ok, item} =
             Workspaces.create_work_item(project.id, %{
               title: "Implement #{key}",
               status: "ready",
               assignee_type: "agent"
             })

    assert {:ok, snapshot, :created} =
             Workspaces.launch_work_item(item.id, "launch-#{key}",
               required_capabilities: %{"supervisory_control" => true},
               notify: false
             )

    item = snapshot.work_item
    now = DateTime.utc_now()
    token = Ecto.UUID.generate()

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(
               %{name: "Chat runtime", machine_token: "token-#{token}"},
               token,
               enrollment_token: "enroll",
               expected_enrollment_token: "enroll",
               now: now
             )

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               enrolled.machine.id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: "chat",
                   name: "Chat runtime",
                   capacity: 1,
                   agent_profile: "codex",
                   workspace: "primary",
                   capabilities: %{
                     "supervisory_control" => true,
                     "structured_input" => true,
                     "interactive" => true
                   }
                 }
               ],
               now: now
             )

    assert {:ok, run} = Orchestration.assign_one(now: now)

    assert {:ok, claimed} =
             Orchestration.claim(
               run.id,
               %{
                 runtime_id: runtime.id,
                 runtime_epoch: runtime.connection_epoch,
                 generation: run.generation,
                 claim_id: Ecto.UUID.generate()
               },
               now: now
             )

    fence = %{
      runtime_id: runtime.id,
      runtime_epoch: runtime.connection_epoch,
      generation: run.generation,
      claim_id: claimed.claim_id,
      lease_token: claimed.lease_token
    }

    assert {:ok, run} =
             Orchestration.transition(run.id, fence, "running", %{}, Ecto.UUID.generate(),
               now: now
             )

    {project, item, run, fence, now}
  end

  defp register_native_runtime(now, opts \\ []) do
    kind = Keyword.get(opts, :harness_kind, "codex")
    runtime_key = Keyword.get(opts, :runtime_key, "native-chat")
    agent_profile = Keyword.get(opts, :agent_profile, "codex")
    %{machine: machine} = enroll_machine("Native runtime #{runtime_key}", now)

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: runtime_key,
                   name: "Native runtime #{runtime_key}",
                   capacity: 1,
                   agent_profile: agent_profile,
                   workspace: "primary",
                   harness_kind: kind,
                   harness_version: "0.153.4",
                   adapter_version: "symmetry-#{kind}-1",
                   adapter_protocol_version: 1,
                   capabilities: native_runtime_capabilities(kind),
                   heartbeat_interval_ms: 60_000
                 }
               ],
               now: now
             )

    runtime
  end

  defp native_runtime_capabilities(kind) do
    %{
      "interactive" => false,
      "adapter" => %{
        "kind" => kind,
        "native_version" => "0.153.4",
        "implementation_version" => "symmetry-#{kind}-1",
        "protocol_version" => 1,
        "operations" => %{
          "start" => true,
          "events" => true,
          "cancel" => true,
          "resume" => false,
          "handoff" => false,
          "guidance" => "next_turn",
          "pause" => "unsupported",
          "approval_response" => false,
          "usage" => "reported",
          "hard_cost_limit" => false
        }
      }
    }
  end

  defp register_legacy_runtime(now, opts \\ []) do
    runtime_key = Keyword.get(opts, :runtime_key, "legacy-chat")
    agent_profile = Keyword.get(opts, :agent_profile, "codex")
    %{machine: machine} = enroll_machine("Legacy runtime #{runtime_key}", now)

    assert {:ok, [runtime]} =
             Orchestration.register_runtimes(
               machine.id,
               Ecto.UUID.generate(),
               [
                 %{
                   runtime_key: runtime_key,
                   name: "Legacy runtime #{runtime_key}",
                   capacity: 1,
                   agent_profile: agent_profile,
                   workspace: "primary",
                   capabilities: %{
                     "supervisory_control" => true,
                     "structured_input" => true,
                     "interactive" => true
                   }
                 }
               ],
               now: now
             )

    runtime
  end

  defp enroll_machine(name, now) do
    key = Ecto.UUID.generate()

    assert {:ok, enrolled, :created} =
             Orchestration.enroll_machine(
               %{name: name, machine_token: "token-#{key}"},
               key,
               enrollment_token: "enroll",
               expected_enrollment_token: "enroll",
               now: now
             )

    enrolled
  end

  defp append_event(run, fence, now, kind, payload, sequence) do
    assert {:ok, _events} =
             Orchestration.append_events(
               run.id,
               fence,
               [
                 %{
                   event_id: Ecto.UUID.generate(),
                   sequence: sequence,
                   kind: kind,
                   payload: payload,
                   occurred_at: now
                 }
               ],
               now: now
             )
  end

  defp append_semantic_events(run, fence, now, entries) do
    events =
      entries
      |> Enum.with_index(1)
      |> Enum.map(fn {{kind, payload}, sequence} ->
        %{
          event_id: Ecto.UUID.generate(),
          sequence: sequence,
          kind: kind,
          payload: payload,
          occurred_at: now
        }
      end)

    assert {:ok, _events} = Orchestration.append_events(run.id, fence, events, now: now)
  end
end
