Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.HarnessSessionFlowTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Goals.HarnessSessionAttachReceipt
  alias SymmetryControl.Goals.HarnessSessionStopReceipt
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo

  @now ~U[2026-09-09 00:00:00.000000Z]

  use SymmetryControl.TestFixtures,
    command_opts: [now: @now, validation_profiles: validation_profiles()]

  setup :put_goal_validation_profiles

  test "dependency changes require a scope decision bound to its operation and target" do
    project = project_fixture()
    repository = repository_fixture(project)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

    plan_item = fn key ->
      %{
        key: key,
        title: "#{key} work",
        description: "Keep #{key} work bounded.",
        required: true,
        integration: key == "first",
        repository_resource_id: repository.id,
        acceptance: check_contract(),
        depends_on_keys: [],
        model_profile: "codex",
        change_target: nil,
        baseline: baseline_subject(repository.id)
      }
    end

    proposal = %{
      schema_version: "symmetry.plan.v1",
      proposal_id: Ecto.UUID.generate(),
      goal_id: created.goal.id,
      expected_revision: created.goal.current_revision,
      items: [
        plan_item.("first"),
        plan_item.("second")
        |> Map.put(:depends_on_keys, ["first"])
        |> Map.put(:baseline, %{kind: "dependency", key: "first"})
      ]
    }

    accept_plan!(created.goal.id, proposal)

    items_by_title = Map.new(fetch_goal!(created.goal.id).work_items, &{&1.title, &1})
    first = Map.fetch!(items_by_title, "first work")
    second = Map.fetch!(items_by_title, "second work")

    assert {:ok, scope_decision, :created} =
             command_current(created.goal.id, "request_decision", %{
               kind: "scope",
               question: "Change this dependency?",
               options: [
                 %{"id" => "accept", "label" => "Accept", "consequence" => "Remove dependency"}
               ],
               work_item_id: first.id,
               depends_on_id: second.id,
               dependency_operation: "remove_dependency"
             })

    scope_decision_id = scope_decision.response["decision"]["id"]

    resolve_decision!(created.goal.id, scope_decision_id)

    assert {:error, :invalid_decision} =
             command_current(created.goal.id, "add_dependency", %{
               work_item_id: first.id,
               depends_on_id: second.id,
               decision_id: scope_decision_id
             })

    assert {:error, :invalid_decision} =
             command_current(created.goal.id, "add_dependency", %{
               work_item_id: second.id,
               depends_on_id: first.id,
               decision_id: scope_decision_id
             })

    assert Repo.aggregate(SymmetryControl.Goals.WorkDependency, :count) == 1

    assert {:ok, baseline_scope_decision, :created} =
             command_current(created.goal.id, "request_decision", %{
               kind: "scope",
               question: "Remove the immutable baseline dependency?",
               options: [
                 %{
                   "id" => "accept",
                   "label" => "Accept",
                   "consequence" => "Attempt to remove the baseline"
                 }
               ],
               work_item_id: second.id,
               depends_on_id: first.id,
               dependency_operation: "remove_dependency"
             })

    baseline_scope_decision_id = baseline_scope_decision.response["decision"]["id"]

    resolve_decision!(created.goal.id, baseline_scope_decision_id)

    assert {:error, :invalid_plan} =
             command_current(created.goal.id, "remove_dependency", %{
               work_item_id: second.id,
               depends_on_id: first.id,
               decision_id: baseline_scope_decision_id
             })

    assert Repo.aggregate(SymmetryControl.Goals.WorkDependency, :count) == 1
  end

  test "machine session attach binds the claimed run and replays only the same opaque handle" do
    {goal, item, _task, runtime, run, fence} = claimed_goal_run_fixture()
    attrs = session_attrs(item, "workspace-1")

    assert {:ok, %{session: session}, :created} =
             Goals.attach_harness_session(runtime.machine_id, run.id, fence, attrs, now: @now)

    assert session.goal_id == goal.id
    assert session.run_id == run.id
    assert session.runtime_id == runtime.id
    assert session.local_handle_id == attrs.local_handle_id
    assert is_binary(session.attachment_receipt_id)
    assert Repo.aggregate(HarnessSessionAttachReceipt, :count) == 1

    Repo.update_all(from(row in Task, where: row.id == ^run.task_id),
      set: [current_generation: 2, attempt_generation: 2]
    )

    assert {:ok, %{session: ^session}, :replayed} =
             Goals.attach_harness_session(runtime.machine_id, run.id, fence, attrs, now: @now)

    assert {:error, :ownership_lost} =
             Goals.attach_harness_session(Ecto.UUID.generate(), run.id, fence, attrs, now: @now)

    assert {:error, :ownership_lost} =
             Goals.attach_harness_session(
               runtime.machine_id,
               run.id,
               %{fence | lease_token: Ecto.UUID.generate()},
               attrs,
               now: @now
             )

    assert {:error, :idempotency_conflict} =
             Goals.attach_harness_session(
               runtime.machine_id,
               run.id,
               fence,
               %{attrs | workspace: "other"},
               now: @now
             )

    assert {:error, :invalid_request} =
             Goals.attach_harness_session(
               runtime.machine_id,
               run.id,
               fence,
               Map.put(attrs, :native_session_id, "must-stay-local"),
               now: @now
             )
  end

  test "fresh attach never adopts an existing available local handle" do
    {_goal, item, _task, runtime, run, fence} = claimed_goal_run_fixture()
    attrs = session_attrs(item, "workspace-fresh-reuse")
    existing = available_session_fixture(runtime, item, attrs)

    assert {:error, :idempotency_conflict} =
             Goals.attach_harness_session(runtime.machine_id, run.id, fence, attrs, now: @now)

    assert %{id: existing_id, state: "available", active_run_id: nil} =
             Repo.get!(HarnessSession, existing.id)

    assert existing_id == existing.id
    assert %{harness_session_id: nil, harness_binding_id: nil} = Repo.get!(Run, run.id)
    assert Repo.aggregate(HarnessSessionAttachReceipt, :count) == 0
  end

  test "an exact attached session replays after pause or amendment but rejects changed runtime identity" do
    {goal, item, _task, runtime, run, fence} = claimed_goal_run_fixture()
    attrs = session_attrs(item, "workspace-ack-lost")

    assert {:ok, %{session: session}, :created} =
             Goals.attach_harness_session(runtime.machine_id, run.id, fence, attrs, now: @now)

    assert {:ok, _paused, :created} =
             command_current(goal.id, "pause", %{
               reason: "Pause after the daemon persisted its handle."
             })

    assert {:ok, %{session: ^session}, :replayed} =
             Goals.attach_harness_session(runtime.machine_id, run.id, fence, attrs, now: @now)

    assert {:ok, _amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract:
                 amended_revision_contract("Amend after an attach acknowledgement loss."),
               reason: "The original attach remains an idempotent fact."
             })

    assert {:ok, %{session: ^session}, :replayed} =
             Goals.attach_harness_session(runtime.machine_id, run.id, fence, attrs, now: @now)

    assert {:error, :idempotency_conflict} =
             Goals.attach_harness_session(
               runtime.machine_id,
               run.id,
               fence,
               %{attrs | adapter_version: "2.0.0"},
               now: @now
             )
  end

  test "a requested available session is claimed, then settlement quarantines its terminal run" do
    {goal, item, nil} = admitted_task_fixture("primary", check_contract(), %{}, false)
    runtime = runtime_fixture()

    Repo.update_all(
      from(row in Runtime, where: row.id == ^runtime.id),
      set: [capabilities: %{"adapter" => %{"operations" => %{"resume" => true}}}]
    )

    runtime = Repo.get!(Runtime, runtime.id)
    attrs = session_attrs(item, "workspace-requested")
    requested = available_session_fixture(runtime, item, attrs)

    unrelated =
      available_session_fixture(runtime, item, session_attrs(item, "workspace-unrelated"))

    assert {:ok, admission, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 session_mode: "resume",
                 requested_session_id: requested.id
               }),
               rollout_enabled: true
             )

    task = Repo.get!(Task, admission.response["task"]["id"])
    {run, fence} = running_goal_run_fixture(task, runtime)
    run = reserve_requested_session!(requested, run)
    attrs = Map.put(attrs, :binding_id, run.harness_binding_id)

    assert {:ok, %{session: %{id: requested_id, state: "busy"}}, :created} =
             Goals.attach_harness_session(runtime.machine_id, run.id, fence, attrs, now: @now)

    assert requested_id == requested.id
    run_id = run.id
    assert %{state: "busy", active_run_id: ^run_id} = Repo.get!(HarnessSession, requested.id)
    assert %{state: "available", active_run_id: nil} = Repo.get!(HarnessSession, unrelated.id)

    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "completed"])
    Repo.update_all(from(row in Run, where: row.id == ^run.id), set: [state: "completed"])

    assert {:ok, %{"settlement" => "missing_result"}} =
             Goals.settle_task(task.id, run.id, 1, now: @now)

    assert %{state: "unavailable", active_run_id: nil} = Repo.get!(HarnessSession, requested.id)
    assert %{state: "available", active_run_id: nil} = Repo.get!(HarnessSession, unrelated.id)
  end

  test "a terminal stop receipt releases one attachment identity and replays exactly" do
    {_goal, item, task, runtime, run, fence} = claimed_goal_run_fixture()
    attrs = session_attrs(item, "workspace-stop-receipt")

    assert {:ok, %{session: session}, :created} =
             Goals.attach_harness_session(runtime.machine_id, run.id, fence, attrs, now: @now)

    stop = %{
      session_id: session.id,
      local_handle_id: attrs.local_handle_id,
      binding_id: session.binding_id
    }

    assert {:error, :state_conflict} =
             Goals.mark_harness_session_stopped(runtime.machine_id, run.id, fence, stop,
               now: @now
             )

    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "completed"])
    Repo.update_all(from(row in Run, where: row.id == ^run.id), set: [state: "completed"])

    assert {:ok, %{"settlement" => "missing_result"}} =
             Goals.settle_task(task.id, run.id, run.generation, now: @now)

    assert %{state: "unavailable", active_run_id: nil, binding_id: binding_id} =
             Repo.get!(HarnessSession, session.id)

    assert binding_id == session.binding_id

    assert {:ok,
            %{
              "session_stopped" => %{
                "receipt_id" => receipt_id,
                "run_id" => run_id,
                "session_id" => session_id,
                "local_handle_id" => local_handle_id,
                "binding_id" => binding_id,
                "state" => "available",
                "active_run_id" => nil
              }
            } = receipt, :created} =
             Goals.mark_harness_session_stopped(runtime.machine_id, run.id, fence, stop,
               now: @now
             )

    assert receipt_id
    assert run_id == run.id
    assert session_id == session.id
    assert local_handle_id == attrs.local_handle_id
    assert binding_id == session.binding_id
    assert %{state: "available", active_run_id: nil} = Repo.get!(HarnessSession, session.id)
    assert Repo.aggregate(HarnessSessionStopReceipt, :count) == 1

    assert {:ok, ^receipt, :replayed} =
             Goals.mark_harness_session_stopped(runtime.machine_id, run.id, fence, stop,
               now: @now
             )

    assert %{binding_id: ^binding_id, state: "available", active_run_id: nil} =
             Repo.get!(HarnessSession, session.id)

    assert {:error, :idempotency_conflict} =
             Goals.mark_harness_session_stopped(
               runtime.machine_id,
               run.id,
               fence,
               %{stop | local_handle_id: Ecto.UUID.generate()},
               now: @now
             )
  end

  test "run context requires the current owning machine and returns a sanitized snapshot" do
    {goal, item, admitted_task} = admitted_task_fixture()

    Repo.update_all(from(row in Task, where: row.id == ^admitted_task.id),
      set: [state: "completed", updated_at: @now]
    )

    snapshot = Repo.get!(SymmetryControl.Goals.ContextSnapshot, admitted_task.context_snapshot_id)

    snapshot_id = Ecto.UUID.generate()

    snapshot_payload =
      snapshot.payload
      |> Map.put("snapshot_id", snapshot_id)
      |> Map.put("providerToken", "private")

    content_hash =
      snapshot_payload
      |> Map.delete("providerToken")
      |> Map.delete("content_hash")
      |> SymmetryControl.RequestHash.canonical()

    snapshot_payload =
      Map.put(
        snapshot_payload,
        "content_hash",
        "sha256:" <> Base.encode16(content_hash, case: :lower)
      )

    snapshot =
      %SymmetryControl.Goals.ContextSnapshot{id: snapshot_id}
      |> SymmetryControl.Goals.ContextSnapshot.changeset(%{
        goal_id: goal.id,
        goal_revision: admitted_task.goal_revision,
        work_item_id: item.id,
        schema_version: 1,
        content_hash: content_hash,
        payload: snapshot_payload
      })
      |> Repo.insert!()

    task_input =
      admitted_task.input
      |> Map.put("admission_id", Ecto.UUID.generate())
      |> Map.put("context_snapshot_id", snapshot_id)
      |> Map.put("context_hash", "sha256:" <> Base.encode16(content_hash, case: :lower))

    task =
      %Task{}
      |> Task.changeset(%{
        idempotency_key: Ecto.UUID.generate(),
        request_hash: :crypto.strong_rand_bytes(32),
        request_hash_version: admitted_task.request_hash_version,
        work_item_id: item.id,
        goal_id: goal.id,
        goal_revision: admitted_task.goal_revision,
        context_snapshot_id: snapshot_id,
        goal: admitted_task.goal,
        agent_profile: admitted_task.agent_profile,
        workspace: admitted_task.workspace,
        input: task_input,
        required_capabilities: admitted_task.required_capabilities,
        state: "queued",
        current_generation: 0,
        attempt_generation: 1,
        purpose: "implement",
        admission_key: task_input["admission_id"],
        allowed_runtime_ids: admitted_task.allowed_runtime_ids,
        max_run_attempts: admitted_task.max_run_attempts
      })
      |> Repo.insert!()

    runtime = runtime_fixture()
    {run, fence} = running_goal_run_fixture(task, runtime)

    assert {:ok, %{session: _}, :created} =
             Goals.attach_harness_session(
               runtime.machine_id,
               run.id,
               fence,
               session_attrs(item, "workspace-2"),
               now: @now
             )

    assert {:ok, context} = Goals.fetch_run_context(runtime.machine_id, run.id, fence)
    assert context.run_id == run.id
    assert context.context["schema_version"] == "symmetry.context_snapshot.v1"
    assert context.context["snapshot_id"] == snapshot.id

    assert context.context["approved_goal"]["authority_policy"][
             "operator_required_for_scope_change"
           ]

    assert context.context["subject"]["resource_id"] == item_repository_resource_id(item)

    assert context.context["sources"] |> hd() |> Map.fetch!("resource_id") ==
             item_repository_resource_id(item)

    assert context.context["current_decisions"] != []
    assert context.context["validated_evidence"] == []
    assert context.context["failed_attempts"] == []
    assert context.context["next_action"]["kind"] == "replan"
    assert context.context["size"]["total_bytes"] > 0

    assert context.context["content_hash"] ==
             "sha256:" <>
               Base.encode16(
                 SymmetryControl.RequestHash.canonical(
                   Map.delete(context.context, "content_hash")
                 ),
                 case: :lower
               )

    context_document = context.context
    assert {:ok, ^context_document} = Goals.fetch_context(goal.id, snapshot.id)
    refute inspect(context.context) =~ "providerToken"
    refute inspect(context) =~ "private"

    assert {:error, :ownership_lost} =
             Goals.fetch_run_context(Ecto.UUID.generate(), run.id, fence)
  end

  test "a paused Goal rejects post-claim session attachment and context retrieval" do
    {goal, item, _task, runtime, run, fence} = claimed_goal_run_fixture()

    assert {:ok, _receipt, :created} =
             command_current(goal.id, "pause", %{reason: "Operator paused this Goal."})

    assert {:error, :ownership_lost} =
             Goals.attach_harness_session(
               runtime.machine_id,
               run.id,
               fence,
               session_attrs(item, "workspace-paused"),
               now: @now
             )

    assert {:error, :ownership_lost} = Goals.fetch_run_context(runtime.machine_id, run.id, fence)
  end

  test "an amended Goal rejects post-claim session attachment and context retrieval" do
    {goal, item, _task, runtime, run, fence} = claimed_goal_run_fixture()

    assert {:ok, _receipt, :created} =
             command_current(goal.id, "amend", %{
               revision_contract: amended_revision_contract("Amended goal authority."),
               reason: "Operator amended the approved contract."
             })

    assert {:error, :ownership_lost} =
             Goals.attach_harness_session(
               runtime.machine_id,
               run.id,
               fence,
               session_attrs(item, "workspace-amended"),
               now: @now
             )

    assert {:error, :ownership_lost} = Goals.fetch_run_context(runtime.machine_id, run.id, fence)
  end

  defp reserve_requested_session!(session, run) do
    binding_id = Ecto.UUID.generate()

    session
    |> HarnessSession.update_changeset(%{
      state: "busy",
      active_run_id: run.id,
      binding_id: binding_id
    })
    |> Repo.update!()

    run
    |> Ecto.Changeset.change(harness_session_id: session.id, harness_binding_id: binding_id)
    |> Repo.update!()
  end
end
