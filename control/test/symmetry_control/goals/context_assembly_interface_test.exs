Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.ContextAssemblyInterfaceTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals.Commands
  alias SymmetryControl.Goals.ContextAssembly
  alias SymmetryControl.Goals.ContextSnapshot
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Goals.GoalDecision
  alias SymmetryControl.Goals.GoalRevision
  alias SymmetryControl.Goals.ValidationProfiles
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash

  setup :put_goal_validation_profiles

  @now ~U[2026-09-09 00:00:00.000000Z]
  @observed_at "2026-09-09T00:00:00.000000Z"

  defp approved_goal_document(goal, revision) do
    %{
      "goal_id" => goal.id,
      "revision" => revision.revision,
      "objective" => revision.objective,
      "authority_policy" => revision.authority_policy
    }
  end

  defp assert_current_decisions(document, goal) do
    [decision] = Repo.all(from(row in GoalDecision, where: row.goal_id == ^goal.id))

    assert document["current_decisions"] == [
             %{
               "decision_id" => decision.id,
               "action_hash" => ContextAssembly.context_digest(decision.action_hash),
               "state" => "resolved"
             }
           ]
  end

  test "context_digest/1 renders a raw sha256 digest and passes through a wire digest" do
    wire = "sha256:3781049acb7fd5d7de257b5522345fccce1e56f50ea02589ab65256ed359f37f"

    assert ContextAssembly.context_digest(:crypto.hash(:sha256, "symmetry")) == wire
    assert ContextAssembly.context_digest(wire) == wire
    assert ContextAssembly.context_digest("sha256:ABC") == nil
    assert ContextAssembly.context_digest(nil) == nil
  end

  test "canonical_authority_policy/1 fills the operator and publication defaults" do
    assert ContextAssembly.canonical_authority_policy(%{
             "publication_allowed" => true,
             "allowed_actions" => ["implement"]
           }) == %{
             "operator_required_for_scope_change" => true,
             "operator_required_for_completion" => true,
             "publication_allowed" => true,
             "allowed_actions" => ["implement"]
           }

    assert ContextAssembly.canonical_authority_policy(%{}) == %{
             "operator_required_for_scope_change" => true,
             "operator_required_for_completion" => true,
             "publication_allowed" => false,
             "allowed_actions" => []
           }
  end

  test "canonical_context_manifest/1 fills the byte budget and repository source defaults" do
    assert ContextAssembly.canonical_context_manifest(%{
             "byte_budget" => 1024,
             "include_advisory_recall" => true
           }) == %{
             "byte_budget" => 1024,
             "required_source_kinds" => ["repository"],
             "include_advisory_recall" => true
           }

    assert ContextAssembly.canonical_context_manifest(%{}) == %{
             "byte_budget" => 32_768,
             "required_source_kinds" => ["repository"],
             "include_advisory_recall" => false
           }
  end

  test "context_wire_envelope/4 returns the persisted snapshot document only while it still verifies" do
    {goal, item, task, _runtime, _run, _fence} = claimed_goal_run_fixture()
    snapshot = Repo.get!(ContextSnapshot, task.context_snapshot_id)

    assert {:ok, document} = ContextAssembly.context_wire_envelope(snapshot, goal, item, task)

    assert document == snapshot.payload
    assert document["schema_version"] == "symmetry.context_snapshot.v1"
    assert document["snapshot_id"] == snapshot.id
    assert document["goal_id"] == goal.id
    assert document["goal_revision"] == 1
    assert document["work_item_id"] == item.id
    assert document["content_hash"] == ContextAssembly.context_digest(snapshot.content_hash)

    assert {:error, :invalid_context_snapshot} =
             ContextAssembly.context_wire_envelope(
               %{
                 snapshot
                 | payload: Map.put(snapshot.payload, "snapshot_id", Ecto.UUID.generate())
               },
               goal,
               item,
               task
             )
  end

  test "planning_context_snapshot_document/7 pins the planning contract, sources and content hash" do
    project = project_fixture()
    repository = repository_fixture(project)

    assert {:ok, created, :created} =
             Commands.create_goal(project.id, goal_attrs(nil), "operator:test", now: @now)

    proposal =
      plan_proposal(created.goal.id, [
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
      ])

    _planned = accept_plan!(created.goal.id, proposal)

    goal = Repo.get!(Goal, created.goal.id)
    revision = Repo.get_by!(GoalRevision, goal_id: goal.id, revision: 1)
    subject = candidate_subject(repository.id)
    snapshot_id = Ecto.UUID.generate()

    document =
      ContextAssembly.planning_context_snapshot_document(
        goal,
        revision,
        repository,
        subject,
        snapshot_id,
        @now,
        nil
      )

    approved_goal = approved_goal_document(goal, revision)

    work_contract = %{
      "title" => "Plan the approved Goal",
      "description" => "Produce a bounded PlanProposal for the approved Goal. This does not approve or admit work.",
      "purpose" => "plan",
      "acceptance" => %{
        "schema_version" => "symmetry.acceptance.v1",
        "description" => "The operator must accept the proposed plan.",
        "predicates" => [%{"id" => "operator", "kind" => "operator_acceptance"}]
      },
      "validation_bindings" => [],
      "change_target" => nil
    }

    assert document |> Map.keys() |> Enum.sort() == [
             "advisory_recall",
             "approved_goal",
             "content_hash",
             "created_at",
             "current_decisions",
             "failed_attempts",
             "goal_id",
             "goal_revision",
             "next_action",
             "schema_version",
             "size",
             "snapshot_id",
             "sources",
             "subject",
             "validated_evidence",
             "work_contract",
             "work_item_id"
           ]

    assert document["schema_version"] == "symmetry.context_snapshot.v1"
    assert document["snapshot_id"] == snapshot_id
    assert document["goal_id"] == goal.id
    assert document["goal_revision"] == 1
    assert document["work_item_id"] == nil
    assert document["created_at"] == @observed_at
    assert document["approved_goal"] == approved_goal
    assert document["work_contract"] == work_contract
    assert document["subject"] == subject
    assert document["validated_evidence"] == []
    assert document["failed_attempts"] == []
    assert document["advisory_recall"] == []
    assert document["next_action"] == nil

    assert document["sources"] == [
             %{
               "resource_id" => repository.id,
               "source_kind" => "approved_goal",
               "source_revision" => "goal:#{goal.id}:1",
               "content_hash" => ContextAssembly.context_digest(RequestHash.canonical(approved_goal)),
               "observed_at" => @observed_at,
               "trust" => "trusted_policy",
               "required" => false,
               "content" => %{"kind" => "pointer", "value" => "goal:#{goal.id}:1"}
             },
             %{
               "resource_id" => repository.id,
               "source_kind" => "work_contract",
               "source_revision" => "goal:#{goal.id}:1:plan",
               "content_hash" => ContextAssembly.context_digest(RequestHash.canonical(work_contract)),
               "observed_at" => @observed_at,
               "trust" => "trusted_policy",
               "required" => false,
               "content" => %{"kind" => "pointer", "value" => "goal:#{goal.id}:1:plan"}
             },
             %{
               "resource_id" => repository.id,
               "source_kind" => "repository_subject",
               "source_revision" => subject["commit"],
               "content_hash" => subject["tree_digest"],
               "observed_at" => @observed_at,
               "trust" => "repository_untrusted",
               "required" => false,
               "content" => %{"kind" => "pointer", "value" => "repository:" <> repository.id}
             },
             %{
               "resource_id" => repository.id,
               "source_kind" => "repository",
               "source_revision" => subject["commit"],
               "content_hash" => subject["tree_digest"],
               "observed_at" => @observed_at,
               "trust" => "repository_untrusted",
               "required" => true,
               "content" => %{"kind" => "pointer", "value" => "repository:" <> repository.id}
             }
           ]

    assert_current_decisions(document, goal)

    assert document["size"]["byte_budget"] == 32_768
    assert document["size"]["token_estimate"] == nil
    assert document["size"]["optional_bytes"] == 0
    assert document["size"]["total_bytes"] == document["size"]["mandatory_bytes"]

    assert document["content_hash"] ==
             ContextAssembly.context_digest(RequestHash.canonical(Map.delete(document, "content_hash")))
  end

  test "context_snapshot_document/11 pins the implement work contract, sources and content hash" do
    {goal_projection, [item]} = goal_with_items_fixture(%{})

    goal = Repo.get!(Goal, goal_projection.id)
    revision = Repo.get_by!(GoalRevision, goal_id: goal.id, revision: 1)
    subject = candidate_subject(item.repository_resource_id)
    snapshot_id = Ecto.UUID.generate()

    next_action = %{
      "kind" => "replan",
      "reason" => "Execute the admitted implement task with model profile codex."
    }

    assert {:ok, bindings} = ValidationProfiles.bindings_for_acceptance(item.acceptance_contract)

    document =
      ContextAssembly.context_snapshot_document(
        goal,
        revision,
        item,
        "implement",
        "codex",
        subject,
        bindings,
        snapshot_id,
        next_action,
        @now,
        nil
      )

    approved_goal = approved_goal_document(goal, revision)

    work_contract = %{
      "title" => item.title,
      "description" => item.description,
      "purpose" => "implement",
      "acceptance" => item.acceptance_contract,
      "validation_bindings" => bindings,
      "change_target" => item.change_target
    }

    assert document |> Map.keys() |> Enum.sort() == [
             "advisory_recall",
             "approved_goal",
             "content_hash",
             "created_at",
             "current_decisions",
             "failed_attempts",
             "goal_id",
             "goal_revision",
             "next_action",
             "schema_version",
             "size",
             "snapshot_id",
             "sources",
             "subject",
             "validated_evidence",
             "work_contract",
             "work_item_id"
           ]

    assert document["schema_version"] == "symmetry.context_snapshot.v1"
    assert document["snapshot_id"] == snapshot_id
    assert document["goal_id"] == goal.id
    assert document["goal_revision"] == 1
    assert document["work_item_id"] == item.id
    assert document["created_at"] == @observed_at
    assert document["approved_goal"] == approved_goal
    assert document["work_contract"] == work_contract
    assert document["subject"] == subject
    assert document["validated_evidence"] == []
    assert document["failed_attempts"] == []
    assert document["advisory_recall"] == []
    assert document["next_action"] == next_action

    assert document["sources"] == [
             %{
               "resource_id" => item.repository_resource_id,
               "source_kind" => "approved_goal",
               "source_revision" => "goal:#{goal.id}:1",
               "content_hash" => ContextAssembly.context_digest(RequestHash.canonical(approved_goal)),
               "observed_at" => @observed_at,
               "trust" => "trusted_policy",
               "required" => false,
               "content" => %{"kind" => "pointer", "value" => "goal:#{goal.id}:1"}
             },
             %{
               "resource_id" => item.repository_resource_id,
               "source_kind" => "work_contract",
               "source_revision" => "work-item:#{item.id}",
               "content_hash" => ContextAssembly.context_digest(RequestHash.canonical(work_contract)),
               "observed_at" => @observed_at,
               "trust" => "trusted_policy",
               "required" => false,
               "content" => %{"kind" => "pointer", "value" => "work-item:#{item.id}"}
             },
             %{
               "resource_id" => item.repository_resource_id,
               "source_kind" => "repository_subject",
               "source_revision" => subject["commit"],
               "content_hash" => subject["tree_digest"],
               "observed_at" => @observed_at,
               "trust" => "repository_untrusted",
               "required" => false,
               "content" => %{
                 "kind" => "pointer",
                 "value" => "repository:" <> item.repository_resource_id
               }
             },
             %{
               "resource_id" => item.repository_resource_id,
               "source_kind" => "repository",
               "source_revision" => subject["commit"],
               "content_hash" => subject["tree_digest"],
               "observed_at" => @observed_at,
               "trust" => "repository_untrusted",
               "required" => true,
               "content" => %{
                 "kind" => "pointer",
                 "value" => "repository:" <> item.repository_resource_id
               }
             }
           ]

    assert_current_decisions(document, goal)

    assert document["size"]["byte_budget"] == 32_768
    assert document["size"]["token_estimate"] == nil
    assert document["size"]["optional_bytes"] == 0
    assert document["size"]["total_bytes"] == document["size"]["mandatory_bytes"]

    assert document["content_hash"] ==
             ContextAssembly.context_digest(RequestHash.canonical(Map.delete(document, "content_hash")))
  end

  test "safe_document?/1 rejects sensitive keys at any depth and non-string map keys" do
    assert ContextAssembly.safe_document?(%{"objective" => "ship", "items" => [%{"id" => "w1"}]})
    refute ContextAssembly.safe_document?(%{"objective" => %{"api_key" => "k"}})
    refute ContextAssembly.safe_document?(%{1 => "objective"})
  end

  test "safe_evidence_document/1 accepts a clean evidence document and refuses a sensitive one" do
    clean = %{"summary" => "done", "checks" => [%{"id" => "c1"}]}
    assert ContextAssembly.safe_evidence_document(clean) == :ok

    sensitive = %{"summary" => "done", "authorization" => "Bearer token"}
    assert ContextAssembly.safe_evidence_document(sensitive) == {:error, :invalid_request}
  end
end
