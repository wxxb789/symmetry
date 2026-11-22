Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.CommandsInterfaceTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals.Commands
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.GoalDecision
  alias SymmetryControl.Goals.GoalEvent
  alias SymmetryControl.Goals.GoalRevision
  alias SymmetryControl.Repo

  @now ~U[2026-09-09 00:00:00.000000Z]

  setup :put_goal_validation_profiles

  test "create_goal/4 persists one draft goal, revision and goal_created event behind a created receipt" do
    project = project_fixture()
    mutation_id = Ecto.UUID.generate()
    attrs = goal_attrs(mutation_id)

    assert {:ok, receipt, :created} =
             Commands.create_goal(project.id, attrs, "operator:test", now: @now)

    goal_id = receipt.goal.id

    assert receipt.goal == %{
             id: goal_id,
             state: "draft",
             version: 2,
             current_revision: 1,
             next_wake_at: nil,
             updated_at: "2026-09-09T00:00:00.000000Z"
           }

    assert receipt.event == %{
             id: mutation_id,
             sequence: 1,
             kind: "goal_created",
             revision: 1
           }

    assert receipt.response == %{
             "goal_id" => goal_id,
             "mutation_id" => mutation_id,
             "state" => "draft",
             "version" => 2,
             "current_revision" => 1
           }

    goal = Repo.get!(Goal, goal_id)

    assert goal.state == "draft"
    assert goal.current_revision == 1
    assert goal.event_sequence == 1
    assert goal.operator_version == 2
    assert goal.project_id == project.id
    assert goal.title == attrs.title

    revision = Repo.get_by!(GoalRevision, goal_id: goal_id, revision: 1)

    assert revision.objective == attrs.initial_revision.objective
    assert revision.actor_ref == "operator:test"
    assert revision.inserted_at == @now

    event = Repo.get!(GoalEvent, mutation_id)

    assert event.goal_id == goal_id
    assert event.kind == "goal_created"
    assert event.sequence == 1
    assert event.actor_ref == "operator:test"
    assert event.request_hash_version == 2
    assert event.payload["project_id"] == project.id
    assert event.payload["initial_revision"]["objective"] == attrs.initial_revision.objective
    assert event.response["_receipt_v1"]["schema_version"] == "symmetry.goal_receipt.v1"

    assert {:ok, ^receipt, :replayed} =
             Commands.create_goal(project.id, attrs, "operator:test", now: @now)
  end

  test "command/4 returns the created request_decision receipt and replays it exactly" do
    project = project_fixture()

    assert {:ok, created, :created} =
             Commands.create_goal(
               project.id,
               goal_attrs(Ecto.UUID.generate()),
               "operator:test",
               now: @now
             )

    goal_id = created.goal.id
    raw_subject_hash = :crypto.hash(:sha256, "completion-subject")
    subject_hash = "sha256:" <> Base.encode16(raw_subject_hash, case: :lower)
    mutation_id = Ecto.UUID.generate()

    command =
      goal_command(
        fetch_goal!(goal_id),
        "request_decision",
        %{kind: "completion", subject_hash: subject_hash},
        mutation_id
      )

    assert {:ok, receipt, :created} =
             Commands.command(goal_id, command, "operator:test", now: @now)

    assert receipt.event == %{
             id: mutation_id,
             sequence: 2,
             kind: "request_decision",
             revision: 1
           }

    assert receipt.goal.version == 3

    assert Map.keys(receipt.response) |> Enum.sort() == [
             "current_revision",
             "decision",
             "goal_id",
             "state",
             "version"
           ]

    assert receipt.response["goal_id"] == goal_id
    assert receipt.response["state"] == "draft"
    assert receipt.response["version"] == 3

    decision_id = receipt.response["decision"]["id"]

    assert receipt.response["decision"]["state"] == "open"
    assert receipt.response["decision"]["action_hash"] =~ ~r/^sha256:[0-9a-f]{64}$/

    decision = Repo.get!(GoalDecision, decision_id)

    assert decision.goal_id == goal_id
    assert decision.goal_revision == 1
    assert decision.kind == "completion"
    assert decision.state == "open"
    assert decision.work_item_id == nil
    assert decision.subject_hash == raw_subject_hash

    assert decision.options == [
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

    assert {:ok, ^receipt, :replayed} =
             Commands.command(goal_id, command, "operator:test", now: @now)

    assert Repo.aggregate(GoalDecision, :count) == 1
  end

  test "canonical_plan_proposal/1 fills missing integration and change_target defaults per item" do
    proposal = %{
      "schema_version" => "symmetry.plan.v1",
      "proposal_id" => Ecto.UUID.generate(),
      "items" => [
        %{key: "planned-work", title: "Planned work", extra: %{nested: true}},
        %{
          "key" => "marked-work",
          "integration" => true,
          "change_target" => %{
            "kind" => "pull_request",
            "pull_request_url" => "https://example.test/pr/1"
          }
        }
      ]
    }

    assert Commands.canonical_plan_proposal(proposal) == %{
             "schema_version" => "symmetry.plan.v1",
             "proposal_id" => proposal["proposal_id"],
             "items" => [
               %{
                 "key" => "planned-work",
                 "title" => "Planned work",
                 "extra" => %{"nested" => true},
                 "integration" => false,
                 "change_target" => nil
               },
               %{
                 "key" => "marked-work",
                 "integration" => true,
                 "change_target" => %{
                   "kind" => "pull_request",
                   "pull_request_url" => "https://example.test/pr/1"
                 }
               }
             ]
           }

    assert Commands.canonical_plan_proposal(%{"items" => "not-a-list"}) == %{
             "items" => "not-a-list"
           }

    assert Commands.canonical_plan_proposal("not-a-map") == "not-a-map"
  end
end
