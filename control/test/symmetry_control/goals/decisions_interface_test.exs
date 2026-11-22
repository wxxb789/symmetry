Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.DecisionsInterfaceTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.Decisions
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.GoalDecision
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash

  @now ~U[2026-09-09 00:00:00.000000Z]

  use SymmetryControl.TestFixtures,
    command_opts: [now: @now, validation_profiles: validation_profiles()]

  setup :put_goal_validation_profiles

  defp command!(goal_id, kind, payload, opts \\ []) do
    assert {:ok, receipt, :created} = command_current(goal_id, kind, payload, opts)
    receipt
  end

  defp planned_item(repository) do
    %{
      key: "planned-work",
      title: "Planned work",
      description: "Remain pending explicit operator approval.",
      required: true,
      repository_resource_id: repository.id,
      acceptance: check_contract(),
      depends_on_keys: [],
      model_profile: "codex",
      baseline: baseline_subject(repository.id)
    }
  end

  test "decision_request!/6 hashes the plan proposal pre-image and asks the plan question" do
    project = project_fixture()
    repository = repository_fixture(project)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(nil), "operator:test", now: @now)

    goal = Repo.get!(Goal, created.goal.id)

    proposal = plan_proposal(goal.id, [planned_item(repository)])

    expected_action_hash =
      RequestHash.canonical(%{
        kind: "plan",
        goal_id: goal.id,
        revision: goal.current_revision,
        proposal_hash: Base.encode16(RequestHash.canonical(proposal), case: :lower)
      })

    assert Decisions.decision_request!(goal, "plan", nil, nil, proposal, []) ==
             {nil, nil, expected_action_hash, "Accept the proposed plan?"}
  end

  test "derived_decision_options/1 derives the accept and reject options with their consequences" do
    assert Decisions.derived_decision_options("plan") == [
             %{
               "id" => "accept",
               "label" => "Accept",
               "consequence" => "Apply the approved action."
             },
             %{
               "id" => "reject",
               "label" => "Reject",
               "consequence" => "Keep the current approved state."
             }
           ]
  end

  test "unresolved_decisions?/2 is true only while a decision at the revision stays open" do
    project = project_fixture()

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(nil), "operator:test", now: @now)

    goal_id = created.goal.id

    decision_receipt =
      command!(goal_id, "request_decision", %{
        kind: "completion",
        subject_hash:
          "sha256:" <> Base.encode16(:crypto.hash(:sha256, "unresolved-subject"), case: :lower)
      })

    decision_id = decision_receipt.response["decision"]["id"]

    assert Decisions.unresolved_decisions?(goal_id, 1)
    refute Decisions.unresolved_decisions?(goal_id, 2)

    resolve_decision!(goal_id, decision_id)

    refute Decisions.unresolved_decisions?(goal_id, 1)
  end

  test "pending_plan_task?/2 tracks an unsettled plan task at the current revision" do
    project = project_fixture()
    repository = repository_fixture(project)

    assert {:ok, created, :created} =
             Goals.create_goal(
               project.id,
               goal_attrs(nil, %{"max_task_admissions" => 2}),
               "operator:test",
               now: @now
             )

    goal_id = created.goal.id

    admitted =
      command!(
        goal_id,
        "request_plan",
        %{
          model_profile: "codex",
          repository_resource_id: repository.id,
          subject: %{
            resource_id: repository.id,
            commit: String.duplicate("a", 40),
            tree_digest: "sha256:" <> String.duplicate("b", 64)
          },
          session_mode: "fresh",
          requested_session_id: nil
        },
        rollout_enabled: true
      )

    task = Repo.get!(Task, admitted.response["task"]["id"])
    goal = Repo.get!(Goal, goal_id)

    assert task.purpose == "plan"
    assert task.state == "queued"
    assert Decisions.pending_plan_task?(goal)
    refute Decisions.pending_plan_task?(goal, task.id)

    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "completed"])

    assert Decisions.pending_plan_task?(goal)
  end

  test "plan_decision_pending_acceptance?/1 tracks an open or accepted plan decision at the revision" do
    project = project_fixture()
    repository = repository_fixture(project)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(nil), "operator:test", now: @now)

    goal_id = created.goal.id
    refute Decisions.plan_decision_pending_acceptance?(Repo.get!(Goal, goal_id))

    decision_receipt =
      command!(goal_id, "request_decision", %{
        kind: "plan",
        proposal: plan_proposal(goal_id, [planned_item(repository)])
      })

    decision_id = decision_receipt.response["decision"]["id"]

    assert Decisions.plan_decision_pending_acceptance?(Repo.get!(Goal, goal_id))

    resolve_decision!(goal_id, decision_id, "reject")

    refute Decisions.plan_decision_pending_acceptance?(Repo.get!(Goal, goal_id))

    accepted_receipt =
      command!(goal_id, "request_decision", %{
        kind: "plan",
        proposal: plan_proposal(goal_id, [planned_item(repository)])
      })

    resolve_decision!(goal_id, accepted_receipt.response["decision"]["id"])

    assert Decisions.plan_decision_pending_acceptance?(Repo.get!(Goal, goal_id))
  end

  test "planning_acceptance_contract/0 is the operator-acceptance contract for proposed plans" do
    assert Decisions.planning_acceptance_contract() == %{
             "schema_version" => "symmetry.acceptance.v1",
             "description" => "The operator must accept the proposed plan.",
             "predicates" => [%{"id" => "operator", "kind" => "operator_acceptance"}]
           }
  end

  test "ensure_resolved_scope_decision!/5 admits the accepted dependency decision and refuses another operation" do
    {goal_projection, [first_item, second_item]} = goal_with_items_fixture(%{}, 2)

    decision_receipt =
      command!(goal_projection.id, "request_decision", %{
        kind: "scope",
        work_item_id: first_item.id,
        depends_on_id: second_item.id,
        dependency_operation: "add_dependency"
      })

    decision_id = decision_receipt.response["decision"]["id"]

    resolve_decision!(goal_projection.id, decision_id)

    goal = Repo.get!(Goal, goal_projection.id)

    assert Decisions.ensure_resolved_scope_decision!(
             goal,
             first_item,
             decision_id,
             "add_dependency",
             second_item.id
           ) == nil

    assert {:error, :invalid_decision} =
             Repo.transaction(fn ->
               Decisions.ensure_resolved_scope_decision!(
                 goal,
                 first_item,
                 decision_id,
                 "remove_dependency",
                 second_item.id
               )
             end)
  end

  test "ensure_resolved_completion_decision!/3 admits the accepted completion subject hash only" do
    project = project_fixture()

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(nil), "operator:test", now: @now)

    goal_id = created.goal.id
    raw_subject_hash = :crypto.hash(:sha256, "completion-subject")
    subject_hash = "sha256:" <> Base.encode16(raw_subject_hash, case: :lower)

    decision_receipt =
      command!(goal_id, "request_decision", %{kind: "completion", subject_hash: subject_hash})

    decision_id = decision_receipt.response["decision"]["id"]

    resolve_decision!(goal_id, decision_id)

    goal = Repo.get!(Goal, goal_id)

    assert Repo.get!(GoalDecision, decision_id).subject_hash == raw_subject_hash

    assert Decisions.ensure_resolved_completion_decision!(goal, decision_id, raw_subject_hash) ==
             nil

    assert {:error, :invalid_decision} =
             Repo.transaction(fn ->
               Decisions.ensure_resolved_completion_decision!(
                 goal,
                 decision_id,
                 :crypto.hash(:sha256, "other-completion-subject")
               )
             end)
  end

  test "decision_option?/2 matches an option id inside the decision options" do
    decision = %{
      options: [
        %{
          "id" => "accept",
          "label" => "Accept",
          "consequence" => "Apply the approved action."
        }
      ]
    }

    assert Decisions.decision_option?(decision, "accept")
    refute Decisions.decision_option?(decision, "reject")
    refute Decisions.decision_option?(%{options: nil}, "accept")
  end

  test "validate_decision_changeset!/2 accepts a compatible wire and aborts an invalid one" do
    project = project_fixture()

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(nil), "operator:test", now: @now)

    decision_receipt =
      command!(created.goal.id, "request_decision", %{
        kind: "completion",
        subject_hash:
          "sha256:" <> Base.encode16(:crypto.hash(:sha256, "changeset-subject"), case: :lower)
      })

    decision = Repo.get!(GoalDecision, decision_receipt.response["decision"]["id"])

    assert Decisions.validate_decision_changeset!(Ecto.Changeset.change(decision), []) == :ok

    assert {:error, :invalid_request} =
             Repo.transaction(fn ->
               Decisions.validate_decision_changeset!(
                 Ecto.Changeset.change(decision, work_item_id: Ecto.UUID.generate()),
                 []
               )
             end)

    assert {:error, :invalid_request} =
             Repo.transaction(fn ->
               Decisions.validate_decision_changeset!(
                 GoalDecision.changeset(%GoalDecision{}, %{}),
                 []
               )
             end)
  end
end
