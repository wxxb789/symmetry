Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.CommandsTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.WorkItem

  @now ~U[2026-09-09 00:00:00.000000Z]
  @validation_runtime_id "00000000-0000-4000-8000-000000000001"
  @validation_profile_digest "sha256:" <> String.duplicate("c", 64)

  use SymmetryControl.TestFixtures,
    command_opts: [now: @now, validation_profiles: validation_profiles()]

  setup :put_goal_validation_profiles

  test "creation and command receipts replay before stale preconditions" do
    project = project_fixture()
    mutation_id = Ecto.UUID.generate()
    attrs = goal_attrs(mutation_id)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, attrs, "operator:test", now: @now)

    assert created.goal.state == "draft"
    assert created.goal.current_revision == 1
    assert created.goal.version == 2

    assert Map.keys(created.goal) |> Enum.sort() == [
             :current_revision,
             :id,
             :next_wake_at,
             :state,
             :updated_at,
             :version
           ]

    assert {:ok, created_projection} = Goals.fetch_goal(created.goal.id)
    assert created_projection.version == 2

    assert created_projection.revision.execution_policy == %{
             "automatic_execution" => false,
             "max_parallel_tasks" => 1,
             "max_task_admissions" => 1,
             "max_run_attempts_per_task" => 2,
             "budget_limit_microusd" => nil,
             "per_run_cost_limit_microusd" => nil,
             "budget_mode" => "soft",
             "hard_cost_limit_required" => false,
             "allowed_runtime_ids" => [],
             "allowed_model_profiles" => [],
             "final_acceptance" => "operator",
             "allowed_actions" => [],
             "allowed_resource_ids" => []
           }

    assert {:ok, created_replay, :replayed} =
             Goals.create_goal(project.id, attrs, "operator:test", now: @now)

    assert created_replay == created

    stored_create_event = Repo.get!(SymmetryControl.Goals.GoalEvent, created.event.id)
    stored_create_receipt = stored_create_event.response["_receipt_v1"]

    assert stored_create_receipt["schema_version"] == "symmetry.goal_receipt.v1"

    assert Map.keys(stored_create_receipt) |> Enum.sort() == [
             "event",
             "goal",
             "response",
             "schema_version"
           ]

    assert Map.keys(stored_create_receipt["goal"]) |> Enum.sort() == [
             "current_revision",
             "id",
             "next_wake_at",
             "state",
             "updated_at",
             "version"
           ]

    refute inspect(stored_create_receipt) =~ "term_to_binary"

    goal_id = created.goal.id

    decision_subject_hash = "sha256:" <> String.duplicate("a", 64)

    command =
      goal_command(created.goal, "request_decision", %{
        kind: "completion",
        subject_hash: decision_subject_hash
      })

    assert {:ok, receipt, :created} = Goals.command(goal_id, command, "operator:test", now: @now)
    assert Repo.get!(SymmetryControl.Goals.GoalEvent, receipt.event.id).request_hash_version == 2
    assert String.starts_with?(receipt.response["decision"]["action_hash"], "sha256:")

    assert {:error, {:stale, %{current_revision: 1, current_version: _}}} =
             Goals.command(
               goal_id,
               %{command | mutation_id: Ecto.UUID.generate()},
               "operator:test",
               now: @now
             )

    assert {:error, {:invalid_contract, _}} =
             command_current(goal_id, "request_decision", %{
               kind: "completion",
               work_item_id: Ecto.UUID.generate(),
               subject_hash: decision_subject_hash
             })

    assert {:ok, receipt_replay, :replayed} =
             Goals.command(goal_id, command, "operator:test", now: @now)

    assert receipt_replay == receipt
    refute Map.has_key?(receipt.goal, :decisions)

    assert {:ok, _later_receipt, :created} =
             command_current(goal_id, "request_decision", %{
               kind: "completion",
               subject_hash: "sha256:" <> String.duplicate("b", 64)
             })

    assert {:ok, ^receipt, :replayed} =
             Goals.command(goal_id, command, "operator:test", now: @now)

    assert {:ok, ^created, :replayed} =
             Goals.create_goal(project.id, attrs, "operator:test", now: @now)

    assert {:error,
            {:stale, %{current_revision: 1, current_version: version, allowed_actions: actions}}} =
             Goals.command(
               goal_id,
               goal_command(created.goal, "pause", %{reason: "later"}),
               "operator:test",
               now: @now
             )

    assert version > created.goal.version
    assert "request_plan" in actions

    assert {:error, {:stale, %{current_revision: 1, current_version: _, allowed_actions: _}}} =
             Goals.command(
               goal_id,
               goal_command(receipt.goal, "activate", %{approved_revision: 1}),
               "operator:test",
               now: @now
             )
  end

  test "archived projects preserve exact receipts but reject new Goal authority" do
    project = project_fixture()
    attrs = goal_attrs(Ecto.UUID.generate())

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, attrs, "operator:test", now: @now)

    assert {:ok, archived} =
             Workspaces.update_project(project.id, %{
               version: project.lock_version,
               status: "archived"
             })

    assert archived.status == "archived"

    assert {:ok, ^created, :replayed} =
             Goals.create_goal(project.id, attrs, "operator:test", now: @now)

    assert {:error, :state_conflict} =
             Goals.create_goal(
               project.id,
               goal_attrs(Ecto.UUID.generate()),
               "operator:test",
               now: @now
             )

    assert {:error, :state_conflict} =
             command_current(created.goal.id, "request_decision", %{
               kind: "completion",
               subject_hash: "sha256:" <> String.duplicate("a", 64)
             })
  end

  test "commands recheck exact replay after the Goal lock before semantic validation" do
    project = project_fixture()
    attrs = goal_attrs()

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, attrs, "operator:test", now: @now)

    command =
      goal_command(created.goal, "request_decision", %{
        kind: "invalid",
        work_item_id: nil,
        subject_hash: nil
      })

    mutation_id = command.mutation_id
    body = Map.take(command, [:expected_version, :expected_revision, :kind, :payload])
    original_response = Repo.get!(SymmetryControl.Goals.GoalEvent, created.event.id).response

    event_name = [:symmetry_control, :goals, :command, :goal_lock]
    handler_id = {__MODULE__, :goal_lock_replay_race, make_ref()}
    test_pid = self()

    :telemetry.attach(
      handler_id,
      event_name,
      fn
        ^event_name, %{}, %{phase: :attempt, mutation_id: ^mutation_id}, _config ->
          :ok

        ^event_name, %{}, %{phase: :acquired, mutation_id: ^mutation_id}, _config ->
          persist_goal_event!(
            created.goal.id,
            mutation_id,
            created.event.sequence + 1,
            "request_decision",
            created.event.revision,
            body,
            original_response
          )

          send(test_pid, :goal_lock_event_inserted)

        _event, _measurements, _metadata, _config ->
          :ok
      end,
      nil
    )

    on_exit(fn -> :telemetry.detach(handler_id) end)

    assert {:ok, replayed_receipt, :replayed} =
             Goals.command(created.goal.id, command, "operator:test", now: @now)

    assert_receive :goal_lock_event_inserted, 1_000
    assert replayed_receipt.event.id == mutation_id

    assert Repo.aggregate(
             from(event in SymmetryControl.Goals.GoalEvent,
               where: event.id == ^mutation_id
             ),
             :count
           ) == 1
  end

  test "corrupt persisted goal receipts fail closed instead of rebuilding current state" do
    project = project_fixture()
    created_attrs = goal_attrs()

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, created_attrs, "operator:test", now: @now)

    original_response = Repo.get!(SymmetryControl.Goals.GoalEvent, created.event.id).response

    corrupt_goal_id = Ecto.UUID.generate()
    corrupt_goal_mutation_id = Ecto.UUID.generate()

    corrupt_goal_response =
      put_in(original_response, ["_receipt_v1", "goal", "id"], corrupt_goal_id)

    corrupt_goal_attrs = Map.put(created_attrs, :mutation_id, corrupt_goal_mutation_id)

    persist_goal_event!(
      created.goal.id,
      corrupt_goal_mutation_id,
      created.event.sequence + 3,
      "goal_created",
      created.event.revision,
      %{
        project_id: project.id,
        title: corrupt_goal_attrs.title,
        initial_revision: corrupt_goal_attrs.initial_revision
      },
      corrupt_goal_response
    )

    assert {:error, :internal_error} =
             Goals.create_goal(project.id, corrupt_goal_attrs, "operator:test", now: @now)

    corrupt_response_mutation_id = Ecto.UUID.generate()

    corrupt_response =
      put_in(original_response, ["_receipt_v1", "response"], %{"goal_id" => "tampered"})

    corrupt_response_attrs = Map.put(created_attrs, :mutation_id, corrupt_response_mutation_id)

    persist_goal_event!(
      created.goal.id,
      corrupt_response_mutation_id,
      created.event.sequence + 4,
      "goal_created",
      created.event.revision,
      %{
        project_id: project.id,
        title: corrupt_response_attrs.title,
        initial_revision: corrupt_response_attrs.initial_revision
      },
      corrupt_response
    )

    assert {:error, :internal_error} =
             Goals.create_goal(project.id, corrupt_response_attrs, "operator:test", now: @now)
  end

  test "legacy create and command receipts replay before current semantic validation" do
    project = project_fixture()
    create_attrs = goal_attrs()

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, create_attrs, "operator:test", now: @now)

    legacy_path = String.duplicate("e\u0301", 513)
    artifact_resource_id = Ecto.UUID.generate()
    legacy_create_mutation_id = Ecto.UUID.generate()

    legacy_create_attrs =
      create_attrs
      |> Map.put(:mutation_id, legacy_create_mutation_id)
      |> put_in(
        [:initial_revision, :acceptance_contract],
        artifact_acceptance_contract(artifact_resource_id, legacy_path)
      )

    legacy_create_body = %{
      project_id: project.id,
      title: legacy_create_attrs.title,
      initial_revision: legacy_create_attrs.initial_revision
    }

    persist_goal_event!(
      created.goal.id,
      legacy_create_mutation_id,
      created.event.sequence + 3,
      "goal_created",
      created.event.revision,
      legacy_create_body,
      Repo.get!(SymmetryControl.Goals.GoalEvent, created.event.id).response
    )

    events_before = Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count)

    assert {:ok, replayed_create, :replayed} =
             Goals.create_goal(project.id, legacy_create_attrs, "operator:test", now: @now)

    assert replayed_create.goal == created.goal
    assert replayed_create.response == created.response

    assert replayed_create.event ==
             %{
               id: legacy_create_mutation_id,
               sequence: created.event.sequence + 3,
               kind: "goal_created",
               revision: 1
             }

    assert Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count) == events_before

    changed_create_attrs =
      put_in(
        legacy_create_attrs,
        [:initial_revision, :acceptance_contract, "predicates", Access.at(0), "path"],
        "proof.txt"
      )

    assert {:error, :idempotency_conflict} =
             Goals.create_goal(project.id, changed_create_attrs, "operator:test", now: @now)

    valid_revision =
      amended_revision_contract("Legacy amendment replay.")
      |> put_in(
        [:acceptance_contract],
        artifact_acceptance_contract(artifact_resource_id, "proof.txt")
      )

    command_id = Ecto.UUID.generate()

    amendment =
      goal_command(
        created.goal,
        "amend",
        %{revision_contract: valid_revision, reason: "Legacy amendment replay."},
        command_id
      )

    assert {:ok, amendment_receipt, :created} =
             Goals.command(created.goal.id, amendment, "operator:test", now: @now)

    legacy_revision =
      put_in(
        valid_revision,
        [:acceptance_contract, "predicates", Access.at(0), "path"],
        legacy_path
      )

    legacy_command_id = Ecto.UUID.generate()

    legacy_command =
      amendment
      |> Map.put(:mutation_id, legacy_command_id)
      |> put_in([:payload, :revision_contract], legacy_revision)

    legacy_command_body = %{
      expected_version: amendment.expected_version,
      expected_revision: amendment.expected_revision,
      kind: amendment.kind,
      payload: legacy_command.payload
    }

    persist_goal_event!(
      created.goal.id,
      legacy_command_id,
      created.event.sequence + 2,
      "amend",
      created.event.revision,
      legacy_command_body,
      Repo.get!(SymmetryControl.Goals.GoalEvent, command_id).response
    )

    events_before_replay = Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count)

    assert {:ok, replayed_command, :replayed} =
             Goals.command(created.goal.id, legacy_command, "operator:test", now: @now)

    assert replayed_command.response == amendment_receipt.response
    assert replayed_command.event.id == legacy_command_id
    assert replayed_command.event.kind == "amend"
    assert Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count) == events_before_replay

    changed_command =
      put_in(
        legacy_command,
        [:payload, :revision_contract, :acceptance_contract, "predicates", Access.at(0), "path"],
        "proof-again.txt"
      )

    assert {:error, :idempotency_conflict} =
             Goals.command(created.goal.id, changed_command, "operator:test", now: @now)
  end

  test "amendment appends immutable revision and supersedes open decisions" do
    project = project_fixture()

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

    assert {:ok, _decision_receipt, :created} =
             command_current(created.goal.id, "request_decision", %{
               kind: "completion",
               subject_hash: "sha256:" <> String.duplicate("a", 64)
             })

    assert fetch_goal!(created.goal.id).decisions |> Enum.any?(&(&1.state == "open"))

    revision_contract = amended_revision_contract("Amended objective")

    assert {:ok, amended, :created} =
             command_current(created.goal.id, "amend", %{
               revision_contract: revision_contract,
               reason: "Scope was clarified"
             })

    assert amended.goal.state == "paused"
    assert amended.goal.current_revision == 2

    assert Enum.any?(
             fetch_goal!(created.goal.id).decisions,
             &(&1.state == "superseded" and &1.goal_revision == 1)
           )
  end

  test "revision policy stores a contract decimal microusd limit as a bounded integer" do
    project = project_fixture()

    assert {:ok, created, :created} =
             Goals.create_goal(
               project.id,
               goal_attrs(nil, %{"budget_limit_microusd" => "42"}),
               "operator:test",
               now: @now
             )

    assert fetch_goal!(created.goal.id).revision.execution_policy["budget_limit_microusd"] == "42"
  end

  test "revision validation rejects duplicate or unsafe acceptance predicates" do
    project = project_fixture()

    duplicate_predicates =
      goal_attrs()
      |> put_in([:initial_revision, :acceptance_contract, "predicates"], [
        %{"id" => "same", "kind" => "check", "validator_profile" => "test"},
        %{"id" => "same", "kind" => "check", "validator_profile" => "test"}
      ])

    assert {:error, {:invalid_contract, _}} =
             Goals.create_goal(project.id, duplicate_predicates, "operator:test", now: @now)

    unsafe_artifact =
      goal_attrs()
      |> put_in([:initial_revision, :acceptance_contract], %{
        "schema_version" => "symmetry.acceptance.v1",
        "description" => "An approved artifact is required.",
        "predicates" => [
          %{
            "id" => "artifact",
            "kind" => "artifact",
            "resource_id" => Ecto.UUID.generate(),
            "path" => "../proof.txt"
          }
        ]
      })

    assert {:error, {:invalid_contract, _}} =
             Goals.create_goal(project.id, unsafe_artifact, "operator:test", now: @now)

    overlong_artifact =
      goal_attrs()
      |> put_in([:initial_revision, :acceptance_contract], %{
        "schema_version" => "symmetry.acceptance.v1",
        "description" => "An approved artifact is required.",
        "predicates" => [
          %{
            "id" => "artifact",
            "kind" => "artifact",
            "resource_id" => Ecto.UUID.generate(),
            "path" => String.duplicate("e\u0301", 1024)
          }
        ]
      })

    assert {:error, {:invalid_contract, :invalid_commit_path}} =
             Goals.create_goal(project.id, overlong_artifact, "operator:test", now: @now)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

    overlong_amendment =
      amended_revision_contract("Reject codepoint-overlong artifact paths.")
      |> put_in([:acceptance_contract, "predicates"], [
        %{
          "id" => "artifact",
          "kind" => "artifact",
          "resource_id" => Ecto.UUID.generate(),
          "path" => String.duplicate("e\u0301", 1024)
        }
      ])

    assert {:error, {:invalid_contract, :invalid_commit_path}} =
             command_current(created.goal.id, "amend", %{
               revision_contract: overlong_amendment,
               reason: "The path exceeds the codepoint bound."
             })

    assert fetch_goal!(created.goal.id).current_revision == 1

    unexpected_predicate_field =
      goal_attrs()
      |> put_in([:initial_revision, :acceptance_contract, "predicates"], [
        %{
          "id" => "check",
          "kind" => "check",
          "validator_profile" => "test",
          "unreviewed" => true
        }
      ])

    assert {:error, {:invalid_contract, _}} =
             Goals.create_goal(project.id, unexpected_predicate_field, "operator:test", now: @now)
  end

  test "plan proposals require full v1 envelopes and explicit baselines" do
    project = project_fixture()
    repository = repository_fixture(project)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

    item = %{
      key: "bounded-work",
      title: "Bounded work",
      description: "Keep the proposed work bounded.",
      required: true,
      repository_resource_id: repository.id,
      acceptance: check_contract(),
      depends_on_keys: [],
      model_profile: "codex",
      change_target: nil,
      baseline: baseline_subject(repository.id)
    }

    assert {:error, {:invalid_contract, _}} =
             command_current(created.goal.id, "request_decision", %{
               kind: "plan",
               question: "Accept this plan?",
               options: [%{"id" => "accept", "label" => "Accept", "consequence" => "Proceed"}],
               proposal: %{items: "not-an-array"}
             })

    malformed = [
      %{items: [Map.put(item, :unreviewed, true)]},
      %{items: [Map.put(item, :acceptance, "not-an-object")]},
      %{items: [Map.put(item, :required, "true")]},
      %{items: [Map.delete(item, :change_target)]},
      %{
        items: [
          Map.put(item, :change_target, %{
            kind: "branches",
            source_branch: " codex/goal-0006",
            target_branch: "main"
          })
        ]
      },
      %{
        items: [
          Map.put(item, :change_target, %{
            kind: "pull_request",
            pull_request_url: " https://github.com/acme/symmetry/pull/42"
          })
        ]
      },
      %{items: [Map.put(item, :depends_on_keys, ["same", "same"])]},
      %{items: List.duplicate(item, 257)},
      %{items: [Map.put(item, :depends_on_keys, Enum.map(1..257, &"dependency-#{&1}"))]},
      %{
        schema_version: "symmetry.plan.v1",
        proposal_id: Ecto.UUID.generate(),
        goal_id: Ecto.UUID.generate(),
        expected_revision: 1,
        items: [item]
      }
    ]

    for proposal <- Enum.drop(malformed, -1) do
      assert {:error, {:invalid_contract, _}} =
               command_current(created.goal.id, "request_decision", %{
                 kind: "plan",
                 question: "Accept this plan?",
                 options: [
                   %{"id" => "accept", "label" => "Accept", "consequence" => "Proceed"}
                 ],
                 proposal: proposal
               })
    end

    assert {:error, :invalid_plan} =
             command_current(created.goal.id, "request_decision", %{
               kind: "plan",
               question: "Accept this plan?",
               options: [%{"id" => "accept", "label" => "Accept", "consequence" => "Proceed"}],
               proposal: List.last(malformed)
             })
  end

  test "plan command item type errors remain invalid contracts for both command kinds" do
    project = project_fixture()

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

    malformed_proposal = %{items: [nil]}

    assert {:error, {:invalid_contract, _}} =
             command_current(created.goal.id, "request_decision", %{
               kind: "plan",
               question: "Accept this plan?",
               options: [%{"id" => "accept", "label" => "Accept", "consequence" => "Proceed"}],
               proposal: malformed_proposal
             })

    assert {:error, {:invalid_contract, _}} =
             command_current(
               created.goal.id,
               "accept_plan",
               %{
                 proposal: malformed_proposal,
                 proposal_hash: "sha256:" <> String.duplicate("0", 64),
                 decision_id: Ecto.UUID.generate()
               },
               rollout_enabled: true
             )
  end

  test "same Goal dependency commands retain an acyclic DAG across inverse additions" do
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
        integration: key == "integration",
        repository_resource_id: repository.id,
        acceptance: check_contract(),
        depends_on_keys: [],
        model_profile: "codex",
        change_target: nil,
        baseline: baseline_subject(repository.id)
      }
    end

    _planned =
      created.goal.id
      |> plan_proposal([plan_item.("first"), plan_item.("second"), plan_item.("integration")])
      |> then(&accept_plan!(created.goal.id, &1))

    items_by_title = Map.new(fetch_goal!(created.goal.id).work_items, &{&1.title, &1})
    first = Map.fetch!(items_by_title, "first work")
    second = Map.fetch!(items_by_title, "second work")
    integration = Map.fetch!(items_by_title, "integration work")

    assert {:ok, _receipt, :created} =
             command_current(created.goal.id, "add_dependency", %{
               work_item_id: second.id,
               depends_on_id: first.id,
               decision_id:
                 approve_dependency_change!(
                   created.goal.id,
                   second.id,
                   first.id,
                   "add_dependency"
                 )
             })

    assert {:ok, _receipt, :created} =
             command_current(created.goal.id, "add_dependency", %{
               work_item_id: integration.id,
               depends_on_id: second.id,
               decision_id:
                 approve_dependency_change!(
                   created.goal.id,
                   integration.id,
                   second.id,
                   "add_dependency"
                 )
             })

    # These commands are equivalent to two contenders reaching the Goal lock
    # in either order: the first valid edge persists and its inverse closes a
    # cycle, while the already-admitted chain remains usable.
    assert {:error, :dependency_cycle} =
             command_current(created.goal.id, "add_dependency", %{
               work_item_id: first.id,
               depends_on_id: second.id,
               decision_id:
                 approve_dependency_change!(
                   created.goal.id,
                   first.id,
                   second.id,
                   "add_dependency"
                 )
             })

    assert {:error, :dependency_cycle} =
             command_current(created.goal.id, "add_dependency", %{
               work_item_id: first.id,
               depends_on_id: integration.id,
               decision_id:
                 approve_dependency_change!(
                   created.goal.id,
                   first.id,
                   integration.id,
                   "add_dependency"
                 )
             })

    assert Repo.aggregate(SymmetryControl.Goals.WorkDependency, :count) == 2
  end

  test "Decision v1 validation rejects malformed commands and resolutions" do
    project = project_fixture()

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

    subject_hash = "sha256:" <> String.duplicate("a", 64)

    assert {:error, {:invalid_contract, _}} =
             command_current(created.goal.id, "request_decision", %{
               kind: "completion",
               work_item_id: Ecto.UUID.generate(),
               subject_hash: subject_hash
             })

    assert {:ok, decision, :created} =
             command_current(created.goal.id, "request_decision", %{
               kind: "completion",
               subject_hash: subject_hash
             })

    decision_id = decision.response["decision"]["id"]
    decision_version = Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).lock_version

    for payload <- [
          %{
            decision_id: decision_id,
            expected_decision_version: 9_007_199_254_740_992,
            option_id: "accept"
          },
          %{
            decision_id: decision_id,
            expected_decision_version: decision_version,
            option_id: "accept",
            comment: %{untrusted: true}
          },
          %{
            decision_id: decision_id,
            expected_decision_version: decision_version,
            option_id: "accept",
            comment: String.duplicate("x", 16_385)
          },
          %{
            decision_id: decision_id,
            expected_decision_version: decision_version,
            option_id: "accept",
            resolved_at: DateTime.to_iso8601(@now)
          }
        ] do
      assert {:error, {:invalid_contract, _}} =
               command_current(created.goal.id, "resolve_decision", payload)
    end

    assert {:ok, _resolved, :created} =
             command_current(created.goal.id, "resolve_decision", %{
               decision_id: decision_id,
               expected_decision_version: decision_version,
               option_id: "accept",
               comment: "The bounded completion is approved."
             })

    assert Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).resolution == %{
             "option_id" => "accept",
             "comment" => "The bounded completion is approved.",
             "actor_ref" => "operator:test",
             "resolved_at" => "2026-09-09T00:00:00.000000Z"
           }
  end

  test "operator plan admission creates pinned work, then task admission atomically records snapshot and reservation" do
    project = project_fixture()
    repository = repository_fixture(project)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

    goal_id = created.goal.id

    proposal = %{
      schema_version: "symmetry.plan.v1",
      proposal_id: Ecto.UUID.generate(),
      goal_id: goal_id,
      expected_revision: 1,
      items: [
        %{
          key: "implement",
          title: "Implement the bounded change",
          description: "Use the approved repository only.",
          required: true,
          integration: true,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          change_target: nil,
          baseline: baseline_subject(repository.id)
        }
      ]
    }

    assert {:ok, decision, :created} =
             command_current(goal_id, "request_decision", %{
               kind: "plan",
               question: "Accept this bounded plan?",
               options: [
                 %{
                   "id" => "accept",
                   "label" => "Accept",
                   "consequence" => "Admit exactly this item"
                 }
               ],
               proposal: proposal
             })

    decision_id = decision.response["decision"]["id"]
    decision_version = Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).lock_version

    assert {:ok, _resolved, :created} =
             command_current(goal_id, "resolve_decision", %{
               decision_id: decision_id,
               expected_decision_version: decision_version,
               option_id: "accept"
             })

    assert {:ok, _planned, :created} =
             command_current(
               goal_id,
               "accept_plan",
               %{
                 proposal: proposal,
                 proposal_hash:
                   "sha256:" <>
                     Base.encode16(SymmetryControl.RequestHash.canonical(proposal), case: :lower),
                 decision_id: decision_id
               },
               rollout_enabled: true
             )

    assert {:error, {:invalid_contract, _}} =
             command_current(
               goal_id,
               "accept_plan",
               %{
                 proposal: proposal,
                 proposal_hash:
                   "SHA256:" <>
                     Base.encode16(SymmetryControl.RequestHash.canonical(proposal), case: :upper),
                 decision_id: decision_id
               },
               rollout_enabled: true
             )

    assert {:error, {:invalid_contract, _}} =
             command_current(
               goal_id,
               "accept_plan",
               %{proposal: proposal, proposal_hash: 42, decision_id: decision_id},
               rollout_enabled: true
             )

    [item] = fetch_goal!(goal_id).work_items
    assert item.admitted_revision == 1
    assert item.accepted? == false
    assert Repo.get!(SymmetryControl.Workspaces.WorkItem, item.id).integration

    assert {:ok, _active, :created} =
             command_current(goal_id, "activate", %{approved_revision: 1})

    assert {:ok, admitted, :created} =
             command_current(
               goal_id,
               "admit_task",
               admission_payload(item, %{reserved_microusd: 42}),
               rollout_enabled: true
             )

    task_id = admitted.response["task"]["id"]
    task = Repo.get!(Task, task_id)
    assert task.goal_id == goal_id
    assert task.goal_revision == 1

    assert task.allowed_runtime_ids ==
             fetch_goal!(goal_id).revision.execution_policy["allowed_runtime_ids"]

    assert task.context_snapshot_id
    assert task.admission_key
    assert task.max_run_attempts == 2
    assert task.input["schema_version"] == "symmetry.admission.v1"
    assert task.input["admission_id"] == task.admission_key
    assert task.input["context_snapshot_id"] == task.context_snapshot_id

    assert task.input["context_hash"] ==
             "sha256:" <>
               Base.encode16(
                 Repo.get!(SymmetryControl.Goals.ContextSnapshot, task.context_snapshot_id).content_hash,
                 case: :lower
               )

    assert task.input["subject"]["resource_id"] == item_repository_resource_id(item)
    assert task.input["subject"]["tree_digest"] == "sha256:" <> String.duplicate("b", 64)
    refute Map.has_key?(task.input, "goal_admission")

    assert Repo.exists?(
             from(snapshot in SymmetryControl.Goals.ContextSnapshot,
               where: snapshot.id == ^task.context_snapshot_id
             )
           )

    assert Repo.exists?(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.task_id == ^task.id and is_nil(reservation.reserved_microusd)
             )
           )
  end

  test "a plan-approved branch target is persisted and derives only its frozen provider scope" do
    project = project_fixture()
    repository = connected_github_repository_fixture(project, "acme/symmetry")

    attrs =
      goal_attrs(nil, %{
        "allowed_actions" => ["change.upsert", "change.update"],
        "allowed_resource_ids" => [repository.id]
      })
      |> put_in(
        [:initial_revision, :authority_policy, "allowed_actions"],
        ["change.upsert", "change.update"]
      )

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, attrs, "operator:test", now: @now)

    proposal =
      plan_proposal(created.goal.id, [
        %{
          key: "implementation",
          title: "Implement the approved provider change",
          description: "Use only the operator-approved branches.",
          required: true,
          integration: true,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          change_target: %{
            kind: "branches",
            source_branch: "codex/goal-0006",
            target_branch: "main"
          },
          baseline: baseline_subject(repository.id)
        }
      ])

    _planned = accept_plan!(created.goal.id, proposal)
    [item] = fetch_goal!(created.goal.id).work_items

    assert %{
             "kind" => "branches",
             "source_branch" => "codex/goal-0006",
             "target_branch" => "main"
           } = Repo.get!(WorkItem, item.id).change_target

    assert {:ok, projected} = Goals.fetch_goal(created.goal.id)
    assert hd(projected.work_items).change_target == Repo.get!(WorkItem, item.id).change_target

    assert {:ok, _active, :created} =
             command_current(created.goal.id, "activate", %{approved_revision: 1})

    assert {:ok, admitted, :created} =
             command_current(
               created.goal.id,
               "admit_task",
               admission_payload(item),
               rollout_enabled: true
             )

    task = Repo.get!(Task, admitted.response["task"]["id"])
    snapshot = Repo.get!(SymmetryControl.Goals.ContextSnapshot, task.context_snapshot_id)

    assert task.required_capabilities["provider_access"]

    assert get_in(snapshot.payload, ["work_contract", "change_target"]) ==
             Repo.get!(WorkItem, item.id).change_target

    assert task.input["provider_scope"] == %{
             "resource_ids" => [repository.id],
             "operations_by_resource" => %{repository.id => ["change.upsert", "change.update"]},
             "change_target" => Repo.get!(WorkItem, item.id).change_target
           }

    assert get_in(task.input, ["provider_scope", "change_target", "source_branch"]) ==
             "codex/goal-0006"
  end

  test "plan acceptance rejects a pull-request target outside its bound repository" do
    project = project_fixture()
    repository = connected_github_repository_fixture(project, "acme/symmetry")

    attrs =
      goal_attrs(nil, %{
        "allowed_actions" => ["change.update"],
        "allowed_resource_ids" => [repository.id]
      })
      |> put_in([:initial_revision, :authority_policy, "allowed_actions"], ["change.update"])

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, attrs, "operator:test", now: @now)

    proposal =
      plan_proposal(created.goal.id, [
        %{
          key: "implementation",
          title: "Update the approved pull request",
          description: "Use the approved provider target only.",
          required: true,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          change_target: %{
            kind: "pull_request",
            pull_request_url: "https://github.com/acme/other/pull/42"
          },
          baseline: baseline_subject(repository.id)
        }
      ])

    assert {:ok, decision, :created} =
             command_current(created.goal.id, "request_decision", %{
               kind: "plan",
               question: "Accept this bounded provider plan?",
               options: [
                 %{"id" => "accept", "label" => "Accept", "consequence" => "Admit the plan"}
               ],
               proposal: proposal
             })

    decision_id = decision.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(created.goal.id, "resolve_decision", %{
               decision_id: decision_id,
               expected_decision_version:
                 Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).lock_version,
               option_id: "accept"
             })

    assert {:error, :invalid_plan} =
             command_current(
               created.goal.id,
               "accept_plan",
               %{
                 proposal: proposal,
                 proposal_hash:
                   "sha256:" <>
                     Base.encode16(SymmetryControl.RequestHash.canonical(proposal), case: :lower),
                 decision_id: decision_id
               },
               rollout_enabled: true
             )
  end

  test "non-implement admissions never receive an approved provider target" do
    project = project_fixture()
    repository = connected_github_repository_fixture(project, "acme/symmetry")

    attrs =
      goal_attrs(nil, %{
        "max_task_admissions" => 2,
        "allowed_actions" => ["change.upsert"],
        "allowed_resource_ids" => [repository.id]
      })
      |> put_in([:initial_revision, :authority_policy, "allowed_actions"], ["change.upsert"])

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, attrs, "operator:test", now: @now)

    proposal =
      plan_proposal(created.goal.id, [
        %{
          key: "implementation",
          title: "Bounded provider change",
          description: "Only implementation may use this provider target.",
          required: true,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          change_target: %{
            kind: "branches",
            source_branch: "codex/goal-0006",
            target_branch: "main"
          },
          baseline: baseline_subject(repository.id)
        }
      ])

    _planned = accept_plan!(created.goal.id, proposal)
    [item] = fetch_goal!(created.goal.id).work_items

    assert {:ok, _active, :created} =
             command_current(created.goal.id, "activate", %{approved_revision: 1})

    Repo.update_all(
      from(connection in SymmetryControl.Integrations.Connection,
        where: connection.id == ^repository.connection_id
      ),
      set: [capabilities: []]
    )

    assert {:ok, admitted, :created} =
             command_current(
               created.goal.id,
               "admit_task",
               admission_payload(item, %{purpose: "chat"}),
               rollout_enabled: true
             )

    task = Repo.get!(Task, admitted.response["task"]["id"])
    assert task.input["provider_scope"] == nil
    refute task.required_capabilities["provider_access"]
  end

  test "request_plan creates one fenced planning task without authorizing WorkItems" do
    project = project_fixture()
    repository = repository_fixture(project)

    assert {:ok, created, :created} =
             Goals.create_goal(
               project.id,
               goal_attrs(nil, %{"max_task_admissions" => 2}),
               "operator:test",
               now: @now
             )

    subject = %{
      resource_id: repository.id,
      commit: String.duplicate("a", 40),
      tree_digest: "sha256:" <> String.duplicate("b", 64)
    }

    request =
      goal_command(created.goal, "request_plan", %{
        model_profile: "codex",
        repository_resource_id: repository.id,
        subject: subject,
        session_mode: "fresh",
        requested_session_id: nil
      })

    assert {:ok, admitted, :created} =
             Goals.command(created.goal.id, request, "operator:test",
               now: @now,
               rollout_enabled: true
             )

    task = Repo.get!(Task, admitted.response["task"]["id"])
    snapshot = Repo.get!(SymmetryControl.Goals.ContextSnapshot, task.context_snapshot_id)

    assert task.goal_id == created.goal.id
    assert task.goal_revision == created.goal.current_revision

    assert task.allowed_runtime_ids ==
             fetch_goal!(created.goal.id).revision.execution_policy["allowed_runtime_ids"]

    assert task.purpose == "plan"
    assert task.work_item_id == nil
    assert task.validation_of_task_id == nil
    assert task.input["provider_scope"] == nil
    assert task.input["subject"]["resource_id"] == repository.id
    assert snapshot.work_item_id == nil
    assert snapshot.payload["work_item_id"] == nil
    assert Repo.aggregate(WorkItem, :count) == 0

    assert Repo.aggregate(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.task_id == ^task.id
             ),
             :count
           ) == 1

    assert {:ok, replayed, :replayed} =
             Goals.command(created.goal.id, request, "operator:test",
               now: @now,
               rollout_enabled: true
             )

    assert replayed.response == admitted.response

    assert {:error, :plan_pending} =
             command_current(
               created.goal.id,
               "request_plan",
               %{
                 model_profile: "codex",
                 repository_resource_id: repository.id,
                 subject: subject,
                 session_mode: "fresh",
                 requested_session_id: nil
               },
               rollout_enabled: true
             )
  end

  test "manual admission rejects caller-derived fields and binds its key to the command mutation" do
    {goal, item, task} =
      admitted_task_fixture("primary", check_contract(), %{
        execution_policy: %{"budget_limit_microusd" => nil}
      })

    complete_task_and_release_reservation!(goal, task)

    snapshots_before = Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count)
    reservations_before = Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count)

    for {field, value} <- [
          {:subject,
           %{
             resource_id: item_repository_resource_id(item),
             commit: String.duplicate("c", 40),
             tree_digest: "sha256:" <> String.duplicate("d", 64)
           }},
          {:limits, %{max_turns: 2}},
          {:reserved_microusd, "0"},
          {:admission_key, Ecto.UUID.generate()}
        ] do
      assert {:error, {:invalid_contract, _}} =
               command_current(
                 goal.id,
                 "admit_task",
                 Map.put(admission_payload(item), field, value),
                 rollout_enabled: true
               )
    end

    assert Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count) == snapshots_before

    assert Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count) ==
             reservations_before

    assert {:ok, current} = Goals.fetch_goal(goal.id)
    mutation_id = Ecto.UUID.generate()

    assert {:ok, admission, :created} =
             Goals.command(
               goal.id,
               goal_command(current, "admit_task", admission_payload(item), mutation_id),
               "operator:test",
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    task_id = admission.response["task"]["id"]
    assert Repo.get!(Task, task_id).admission_key == mutation_id

    assert Repo.one!(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.task_id == ^task_id,
               select: reservation.reserved_microusd
             )
           ) == nil

    assert {:error, {:invalid_contract, _}} =
             command_current(
               goal.id,
               "admit_task",
               Map.put(admission_payload(item), :validation_of_task_id, Ecto.UUID.generate()),
               rollout_enabled: true
             )
  end

  defp persist_goal_event!(goal_id, event_id, sequence, kind, revision, body, response) do
    receipt_snapshot =
      response
      |> Map.fetch!("_receipt_v1")
      |> Map.put("event", %{
        "id" => event_id,
        "sequence" => sequence,
        "kind" => kind,
        "revision" => revision
      })

    response = Map.put(response, "_receipt_v1", receipt_snapshot)

    %SymmetryControl.Goals.GoalEvent{id: event_id}
    |> SymmetryControl.Goals.GoalEvent.changeset(%{
      goal_id: goal_id,
      sequence: sequence,
      kind: kind,
      actor_ref: "operator:test",
      request_hash: SymmetryControl.RequestHash.canonical(body),
      request_hash_version: 2,
      revision: revision,
      payload: body,
      response: response
    })
    |> Ecto.Changeset.put_change(:inserted_at, @now)
    |> Repo.insert!()
  end

  defp approve_dependency_change!(goal_id, work_item_id, depends_on_id, operation) do
    assert {:ok, decision, :created} =
             command_current(goal_id, "request_decision", %{
               kind: "scope",
               question: "Approve this dependency change?",
               options: [
                 %{"id" => "accept", "label" => "Accept", "consequence" => "Apply the change"}
               ],
               work_item_id: work_item_id,
               depends_on_id: depends_on_id,
               dependency_operation: operation
             })

    decision_id = decision.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(goal_id, "resolve_decision", %{
               decision_id: decision_id,
               expected_decision_version:
                 Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).lock_version,
               option_id: "accept"
             })

    decision_id
  end

  defp artifact_acceptance_contract(resource_id, path) do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "An approved artifact is required.",
      "predicates" => [
        %{
          "id" => "artifact",
          "kind" => "artifact",
          "resource_id" => resource_id,
          "path" => path
        }
      ]
    }
  end
end
