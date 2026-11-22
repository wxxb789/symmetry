Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.HandoffInterfaceTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.Handoff
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces.ProjectResource

  @now ~U[2026-09-09 00:00:00.000000Z]

  setup :put_goal_validation_profiles

  test "handoff_source!/6 returns the settled source identity of a completed candidate" do
    {goal, item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(task.id, run.id, 1, now: @now)

    subject = baseline_subject(item_repository_resource_id(item)).subject
    result_id = Repo.get!(Task, task.id).result["task_result"]["result_id"]

    assert {:ok, source} =
             Repo.transaction(fn ->
               Handoff.handoff_source!(
                 goal,
                 item.id,
                 item.repository_resource_id,
                 %{handoff_source_run_id: run.id},
                 "handoff",
                 now: @now
               )
             end)

    assert source == %{
             run_id: run.id,
             task_id: task.id,
             result_id: result_id,
             result_kind: "candidate_completion",
             subject: subject,
             subject_hash: RequestHash.canonical(subject),
             evidence_ids: []
           }
  end

  test "handoff_admission_subject!/6 binds a validation handoff to the source candidate" do
    subject = candidate_subject(Ecto.UUID.generate())

    source = %{
      run_id: Ecto.UUID.generate(),
      task_id: Ecto.UUID.generate(),
      result_id: Ecto.UUID.generate(),
      result_kind: "candidate_completion",
      subject: subject,
      subject_hash: RequestHash.canonical(subject),
      evidence_ids: []
    }

    assert Handoff.handoff_admission_subject!(
             nil,
             nil,
             "validate",
             %{validation_of_task_id: source.task_id},
             source,
             []
           ) == subject

    assert {:error, :handoff_source_ineligible} =
             Repo.transaction(fn ->
               Handoff.handoff_admission_subject!(
                 nil,
                 nil,
                 "validate",
                 %{validation_of_task_id: source.task_id},
                 %{source | result_kind: "progress"},
                 []
               )
             end)
  end

  test "admission_session_mode!/2 derives the mode from the requested session" do
    session_id = Ecto.UUID.generate()

    assert Handoff.admission_session_mode!(%{}, nil) == "fresh"
    assert Handoff.admission_session_mode!(%{}, session_id) == "resume"
    assert Handoff.admission_session_mode!(%{session_mode: "handoff"}, nil) == "handoff"

    assert {:error, :requested_session_required} =
             Repo.transaction(fn ->
               Handoff.admission_session_mode!(%{session_mode: "resume"}, nil)
             end)

    assert {:error, :invalid_request} =
             Repo.transaction(fn ->
               Handoff.admission_session_mode!(%{session_mode: "fresh"}, session_id)
             end)
  end

  test "ensure_requested_session_available!/3 binds resume to a verified available session" do
    {_goal, item, _task} = admitted_task_fixture()
    runtime = runtime_fixture()

    {retained, unverified} =
      resume_capable_sessions!(runtime, item, "workspace-handoff", "workspace-unverified")

    assert :ok = Handoff.ensure_requested_session_available!(item, nil, "fresh")

    assert {:ok, nil} =
             Repo.transaction(fn ->
               Handoff.ensure_requested_session_available!(item, retained.id, "resume")
             end)

    assert {:error, :requested_session_unavailable} =
             Repo.transaction(fn ->
               Handoff.ensure_requested_session_available!(item, unverified.id, "resume")
             end)
  end

  test "ensure_plan_requested_session_available!/3 locks the plan resource session and runtime" do
    {_goal, [item]} = goal_with_items_fixture(%{})
    resource = Repo.get!(ProjectResource, item_repository_resource_id(item))
    runtime = runtime_fixture()

    {retained, unverified} =
      resume_capable_sessions!(runtime, item, "workspace-plan", "workspace-plan-unverified")

    assert :ok = Handoff.ensure_plan_requested_session_available!(resource, nil, "fresh")

    assert {:ok, nil} =
             Repo.transaction(fn ->
               Handoff.ensure_plan_requested_session_available!(resource, retained.id, "resume")
             end)

    assert {:error, :requested_session_unavailable} =
             Repo.transaction(fn ->
               Handoff.ensure_plan_requested_session_available!(resource, unverified.id, "resume")
             end)
  end

  defp resume_capable_sessions!(runtime, item, workspace, unverified_workspace) do
    Repo.update_all(
      from(row in Runtime, where: row.id == ^runtime.id),
      set: [capabilities: %{"adapter" => %{"operations" => %{"resume" => true}}}]
    )

    retained = available_session_fixture(runtime, item, session_attrs(item, workspace))

    unverified =
      available_session_fixture(runtime, item, session_attrs(item, unverified_workspace),
        binding_verified: false
      )

    {retained, unverified}
  end
end
