defmodule SymmetryControl.GoalsTest do
  use SymmetryControl.DataCase, async: false

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.ReadModel
  alias SymmetryControl.Goals.{GoalExternalWait, HarnessSession}
  alias SymmetryControl.Goals.Workers.{GoalControlWorker, SettleTaskWorker, WakeupWorker}
  alias SymmetryControl.Integrations
  alias SymmetryControl.Orchestration.{Machine, Run, Runtime, Task}
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.WorkItem

  @now ~U[2026-09-09 00:00:00.000000Z]
  @validation_runtime_id "00000000-0000-4000-8000-000000000001"
  @validation_profile_digest "sha256:" <> String.duplicate("c", 64)

  setup do
    previous_goals = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(previous_goals, :validation_profiles, validation_profiles())
    )

    on_exit(fn -> Application.put_env(:symmetry_control, :goals, previous_goals) end)
    :ok
  end

  test "creation and command receipts replay before stale preconditions" do
    project = project_fixture()
    mutation_id = Ecto.UUID.generate()
    attrs = goal_attrs(mutation_id)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, attrs, "operator:test", now: @now)

    assert created.goal.state == "draft"
    assert created.goal.current_revision == 1

    assert created.goal.revision.execution_policy == %{
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

    assert created_replay.goal.id == created.goal.id
    assert created_replay.goal.state == created.goal.state
    assert created_replay.goal.version == created.goal.version
    assert created_replay.goal.current_revision == created.goal.current_revision
    assert created_replay.goal.next_wake_at == nil
    assert created_replay.goal.updated_at == DateTime.to_iso8601(created.goal.updated_at)
    assert created_replay.event == created.event
    assert created_replay.response == created.response
    refute Map.has_key?(created_replay.goal, :revision)

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

    command =
      command(created.goal, "request_decision", %{
        kind: "scope",
        question: "Keep the scope bounded?",
        options: [%{"id" => "yes", "label" => "Yes", "consequence" => "No scope expansion"}]
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

    assert {:error, :invalid_request} =
             command_current(goal_id, "request_decision", %{
               kind: "scope",
               question: "Duplicate option identifiers are ambiguous.",
               options: [
                 %{"id" => "same", "label" => "First", "consequence" => "First outcome"},
                 %{"id" => "same", "label" => "Second", "consequence" => "Second outcome"}
               ]
             })

    assert {:ok, receipt_replay, :replayed} =
             Goals.command(goal_id, command, "operator:test", now: @now)

    assert receipt_replay.response == receipt.response
    assert receipt_replay.event == receipt.event
    refute Map.has_key?(receipt_replay.goal, :decisions)

    assert {:ok, _later_receipt, :created} =
             command_current(goal_id, "request_decision", %{
               kind: "scope",
               question: "Keep a second scoped decision?",
               options: [%{"id" => "yes", "label" => "Yes", "consequence" => "Keep it recorded"}]
             })

    assert {:ok, ^receipt_replay, :replayed} =
             Goals.command(goal_id, command, "operator:test", now: @now)

    assert {:ok, ^created_replay, :replayed} =
             Goals.create_goal(project.id, attrs, "operator:test", now: @now)

    assert {:error,
            {:stale, %{current_revision: 1, current_version: version, allowed_actions: actions}}} =
             Goals.command(
               goal_id,
               command(created.goal, "pause", %{reason: "later"}),
               "operator:test",
               now: @now
             )

    assert version > created.goal.version
    assert "request_plan" in actions

    assert {:error, {:stale, %{current_revision: 1, current_version: _, allowed_actions: _}}} =
             Goals.command(
               goal_id,
               command(receipt.goal, "activate", %{approved_revision: 1}),
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
               kind: "scope",
               question: "Would change Goal authority.",
               options: [
                 %{
                   "id" => "no",
                   "label" => "No",
                   "consequence" => "Project is archived."
                 }
               ]
             })
  end

  test "amendment appends immutable revision and supersedes open decisions" do
    project = project_fixture()

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

    assert {:ok, decision_receipt, :created} =
             command_current(created.goal.id, "request_decision", %{
               kind: "scope",
               question: "Include follow-up work?",
               options: [%{"id" => "no", "label" => "No", "consequence" => "Keep scope bounded"}]
             })

    assert decision_receipt.goal.decisions |> Enum.any?(&(&1.state == "open"))

    revision_contract =
      goal_attrs()
      |> Map.fetch!(:initial_revision)
      |> Map.put(:objective, "Amended objective")

    assert {:ok, amended, :created} =
             command_current(created.goal.id, "amend", %{
               revision_contract: revision_contract,
               reason: "Scope was clarified"
             })

    assert amended.goal.state == "paused"
    assert amended.goal.current_revision == 2

    assert Enum.any?(
             amended.goal.decisions,
             &(&1.state == "superseded" and &1.goal_revision == 1)
           )
  end

  test "an amended Goal accepts a new revision plan without rewriting historical work" do
    {goal, item, _task} = admitted_task_fixture()

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract:
                 amended_revision_contract("A revised plan requires new admitted work."),
               reason: "The prior revision is retained as history."
             })

    proposal = %{
      schema_version: "symmetry.plan.v1",
      proposal_id: Ecto.UUID.generate(),
      goal_id: amended.goal.id,
      expected_revision: amended.goal.current_revision,
      items: [
        %{
          key: "revised-work",
          title: "Revised work",
          description: "Admitted under the revised Goal contract.",
          required: true,
          integration: true,
          repository_resource_id: item_repository_resource_id(item),
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          change_target: nil,
          baseline: baseline_subject(item_repository_resource_id(item))
        }
      ]
    }

    assert {:ok, decision, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "plan",
               question: "Accept the revised plan?",
               options: [
                 %{"id" => "accept", "label" => "Accept", "consequence" => "Admit revised work"}
               ],
               proposal: proposal
             })

    decision_id = decision.response["decision"]["id"]
    decision_version = Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).lock_version

    assert {:ok, _resolved, :created} =
             command_current(goal.id, "resolve_decision", %{
               decision_id: decision_id,
               expected_decision_version: decision_version,
               option_id: "accept"
             })

    assert {:ok, planned, :created} =
             command_current(
               goal.id,
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

    [revised_item] = planned.goal.work_items
    assert revised_item.id != item.id
    assert revised_item.admitted_revision == amended.goal.current_revision
    assert Repo.get!(SymmetryControl.Workspaces.WorkItem, revised_item.id).integration
    assert Enum.any?(planned.goal.history.work_items, &(&1.id == item.id))
  end

  test "an amendment retains prior spent admissions under the Goal admission cap" do
    {goal, item, task, runtime, run, fence} = claimed_goal_run_fixture()

    Repo.update_all(from(row in Task, where: row.id == ^task.id),
      set: [state: "completed", current_generation: 1, updated_at: @now]
    )

    Repo.update_all(from(row in Run, where: row.id == ^run.id),
      set: [state: "completed", updated_at: @now]
    )

    assert {:ok, %{usage: %{cost_microusd: "42"}}, :created} =
             Goals.record_usage(runtime.machine_id, run.id, fence, usage_attrs(run.id), now: @now)

    assert Repo.one!(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.task_id == ^task.id
             )
           ).state == "settled"

    revision_contract = amended_revision_contract("Do not reset spent admissions on amendment.")

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract: revision_contract,
               reason: "Continue within the original Goal budget."
             })

    proposal = %{
      schema_version: "symmetry.plan.v1",
      proposal_id: Ecto.UUID.generate(),
      goal_id: amended.goal.id,
      expected_revision: amended.goal.current_revision,
      items: [
        %{
          key: "revised-work",
          title: "Revised work",
          description: "Admitted under the revised Goal contract.",
          required: true,
          integration: true,
          repository_resource_id: item_repository_resource_id(item),
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          change_target: nil,
          baseline: baseline_subject(item_repository_resource_id(item))
        }
      ]
    }

    assert {:ok, decision, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "plan",
               question: "Accept the revised plan?",
               options: [
                 %{"id" => "accept", "label" => "Accept", "consequence" => "Admit revised work"}
               ],
               proposal: proposal
             })

    decision_id = decision.response["decision"]["id"]
    decision_version = Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).lock_version

    assert {:ok, _resolved, :created} =
             command_current(goal.id, "resolve_decision", %{
               decision_id: decision_id,
               expected_decision_version: decision_version,
               option_id: "accept"
             })

    assert {:ok, planned, :created} =
             command_current(
               goal.id,
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

    [revised_item] = planned.goal.work_items

    assert {:ok, _resumed, :created} =
             command_current(goal.id, "resume", %{reason: "Revised work is approved."})

    assert {:error, :admission_limit_reached} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(revised_item, %{reserved_microusd: 1}),
               rollout_enabled: true
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

    assert created.goal.revision.execution_policy["budget_limit_microusd"] == "42"
  end

  test "Goal plan and admitted Task preserve the project workspace and agent assignment" do
    {_goal, item, task} = admitted_task_fixture("goal-workspace")

    assert item.assignee_type == "agent"
    assert item.assignee_name == "codex"
    assert item.agent_profile == "codex"
    assert item.workspace == "goal-workspace"
    assert task.workspace == "goal-workspace"
  end

  test "revision validation rejects duplicate or unsafe acceptance predicates" do
    project = project_fixture()

    duplicate_predicates =
      goal_attrs()
      |> put_in([:initial_revision, :acceptance_contract, "predicates"], [
        %{"id" => "same", "kind" => "check", "validator_profile" => "test"},
        %{"id" => "same", "kind" => "check", "validator_profile" => "test"}
      ])

    assert {:error, :invalid_request} =
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

    assert {:error, :invalid_request} =
             Goals.create_goal(project.id, unsafe_artifact, "operator:test", now: @now)

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

    assert {:error, :invalid_request} =
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

    assert {:error, :invalid_plan} =
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

    for proposal <- malformed do
      assert {:error, :invalid_plan} =
               command_current(created.goal.id, "request_decision", %{
                 kind: "plan",
                 question: "Accept this plan?",
                 options: [
                   %{"id" => "accept", "label" => "Accept", "consequence" => "Proceed"}
                 ],
                 proposal: proposal
               })
    end
  end

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

    assert {:ok, plan_decision, :created} =
             command_current(created.goal.id, "request_decision", %{
               kind: "plan",
               question: "Accept this bounded plan?",
               options: [
                 %{"id" => "accept", "label" => "Accept", "consequence" => "Admit the items"}
               ],
               proposal: proposal
             })

    plan_decision_id = plan_decision.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(created.goal.id, "resolve_decision", %{
               decision_id: plan_decision_id,
               expected_decision_version:
                 Repo.get!(SymmetryControl.Goals.GoalDecision, plan_decision_id).lock_version,
               option_id: "accept"
             })

    assert {:ok, planned, :created} =
             command_current(
               created.goal.id,
               "accept_plan",
               %{
                 proposal: proposal,
                 proposal_hash:
                   "sha256:" <>
                     Base.encode16(SymmetryControl.RequestHash.canonical(proposal), case: :lower),
                 decision_id: plan_decision_id
               },
               rollout_enabled: true
             )

    [first, second] = planned.goal.work_items

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

    assert {:ok, _resolved, :created} =
             command_current(created.goal.id, "resolve_decision", %{
               decision_id: scope_decision_id,
               expected_decision_version:
                 Repo.get!(SymmetryControl.Goals.GoalDecision, scope_decision_id).lock_version,
               option_id: "accept"
             })

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

    assert {:ok, _resolved, :created} =
             command_current(created.goal.id, "resolve_decision", %{
               decision_id: baseline_scope_decision_id,
               expected_decision_version:
                 Repo.get!(SymmetryControl.Goals.GoalDecision, baseline_scope_decision_id).lock_version,
               option_id: "accept"
             })

    assert {:error, :invalid_plan} =
             command_current(created.goal.id, "remove_dependency", %{
               work_item_id: second.id,
               depends_on_id: first.id,
               decision_id: baseline_scope_decision_id
             })

    assert Repo.aggregate(SymmetryControl.Goals.WorkDependency, :count) == 1
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

    planned =
      created.goal.id
      |> plan_proposal([plan_item.("first"), plan_item.("second"), plan_item.("integration")])
      |> then(&accept_plan!(created.goal.id, &1))

    items_by_title = Map.new(planned.goal.work_items, &{&1.title, &1})
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

  test "Decision v1 validation rejects untrusted options and malformed resolutions" do
    project = project_fixture()

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

    assert {:error, :invalid_request} =
             command_current(created.goal.id, "request_decision", %{
               kind: "scope",
               question: "Keep scope bounded?",
               options: [
                 %{
                   "id" => "accept",
                   "label" => "Accept",
                   "consequence" => "Proceed",
                   "unreviewed" => true
                 }
               ]
             })

    assert {:ok, decision, :created} =
             command_current(created.goal.id, "request_decision", %{
               kind: "scope",
               question: "Keep scope bounded?",
               options: [
                 %{"id" => "accept", "label" => "Accept", "consequence" => "Proceed"}
               ]
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
      assert {:error, :invalid_request} =
               command_current(created.goal.id, "resolve_decision", payload)
    end

    assert {:ok, _resolved, :created} =
             command_current(created.goal.id, "resolve_decision", %{
               decision_id: decision_id,
               expected_decision_version: decision_version,
               option_id: "accept",
               comment: "The bounded scope is approved."
             })

    assert Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).resolution == %{
             "option_id" => "accept",
             "comment" => "The bounded scope is approved.",
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

    assert {:ok, planned, :created} =
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

    assert {:error, :invalid_request} =
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

    assert {:error, :invalid_request} =
             command_current(
               goal_id,
               "accept_plan",
               %{proposal: proposal, proposal_hash: 42, decision_id: decision_id},
               rollout_enabled: true
             )

    [item] = planned.goal.work_items
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

    planned = accept_plan!(created.goal.id, proposal)
    [item] = planned.goal.work_items

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

  test "plan acceptance rejects change targets without a reachable provider connection" do
    unconnected_project = project_fixture()
    unconnected_repository = repository_fixture(unconnected_project)

    assert_branch_target_rejected!(unconnected_project, unconnected_repository)

    limited_project = project_fixture()

    limited_repository =
      connected_github_repository_fixture(limited_project, "acme/symmetry", ["repositories"])

    assert_branch_target_rejected!(limited_project, limited_repository)
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

    planned = accept_plan!(created.goal.id, proposal)
    [item] = planned.goal.work_items

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
      command(created.goal, "request_plan", %{
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

  test "plan_proposed settles into exactly one open Decision without admitting work" do
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

    assert {:ok, admitted, :created} =
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

    task = Repo.get!(Task, admitted.response["task"]["id"])
    runtime = plan_runtime_fixture(repository)
    {run, _fence} = completed_goal_run_fixture(task, runtime)

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

    put_task_result!(run, task_result(task, "plan_proposed", %{proposal: proposal}))

    assert {:ok, receipt} = Goals.settle_task(task.id, run.id, 1, now: @now)
    decision_id = receipt["decision_id"]

    assert receipt["settlement"] == "plan_proposed"
    assert is_binary(decision_id)
    assert Repo.aggregate(WorkItem, :count) == 0
    assert Repo.aggregate(SymmetryControl.Goals.WorkOutcome, :count) == 0

    decision = Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id)
    assert decision.goal_id == created.goal.id
    assert decision.goal_revision == created.goal.current_revision
    assert decision.kind == "plan"
    assert decision.state == "open"
    assert decision.work_item_id == nil

    assert {:ok, replayed} = Goals.settle_task(task.id, run.id, 1, now: @now)
    assert replayed == receipt

    assert Repo.aggregate(
             from(decision in SymmetryControl.Goals.GoalDecision,
               where: decision.goal_id == ^created.goal.id and decision.kind == "plan"
             ),
             :count
           ) == 1
  end

  test "plan_proposed from a WorkItem Task settles as a stable invalid receipt" do
    {goal, item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    proposal =
      plan_proposal(goal.id, [
        %{
          key: "misplaced-plan",
          title: "Misplaced plan result",
          description: "A WorkItem Task must not create planning authority.",
          required: true,
          integration: true,
          repository_resource_id: item_repository_resource_id(item),
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: baseline_subject(item_repository_resource_id(item))
        }
      ])

    put_task_result!(run, task_result(task, "plan_proposed", %{proposal: proposal}))

    assert {:ok, receipt} = Goals.settle_task(task.id, run.id, 1, now: @now)
    assert receipt["settlement"] == "invalid_task_result"
    assert receipt["reason"] == "invalid_task_result"

    assert {:ok, replayed} = Goals.settle_task(task.id, run.id, 1, now: @now)
    assert replayed == receipt
  end

  test "Goal creation rejects an immutable validation contract without its operator profile" do
    project = project_fixture()

    assert {:error, :invalid_validation_profile} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test",
               now: @now,
               validation_profiles: []
             )
  end

  test "Goal amendment rejects an immutable validation contract without its operator profile" do
    project = project_fixture()

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

    assert {:error, :invalid_validation_profile} =
             command_current(
               created.goal.id,
               "amend",
               %{
                 revision_contract: amended_revision_contract("A profile-less amendment."),
                 reason: "The configured validator profile is unavailable."
               },
               validation_profiles: []
             )

    assert {:ok, unchanged} = Goals.fetch_goal(created.goal.id)
    assert unchanged.current_revision == 1
  end

  test "plan preflight requires an integration item and a same-resource dependency baseline" do
    project = project_fixture()
    first_repository = repository_fixture(project)
    second_repository = repository_fixture(project)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

    no_integration =
      plan_proposal(created.goal.id, [
        %{
          key: "no-integration",
          title: "No integration work",
          description: "This plan cannot establish final completion.",
          required: true,
          integration: false,
          repository_resource_id: first_repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: baseline_subject(first_repository.id)
        }
      ])

    assert :invalid_plan == reject_plan_admission!(created.goal.id, no_integration)

    cross_resource_baseline =
      plan_proposal(created.goal.id, [
        %{
          key: "source",
          title: "Source work",
          description: "Produce a source Subject.",
          required: true,
          integration: false,
          repository_resource_id: first_repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: baseline_subject(first_repository.id)
        },
        %{
          key: "consumer",
          title: "Consumer work",
          description: "Cannot inherit a Subject from another repository.",
          required: true,
          integration: true,
          repository_resource_id: second_repository.id,
          acceptance: check_contract(),
          depends_on_keys: ["source"],
          model_profile: "codex",
          baseline: %{kind: "dependency", key: "source"}
        }
      ])

    assert :invalid_plan == reject_plan_admission!(created.goal.id, cross_resource_baseline)
  end

  test "plan acceptance rejects a missing immutable validation profile before inserting WorkItems" do
    project = project_fixture()
    repository = repository_fixture(project)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

    proposal =
      plan_proposal(created.goal.id, [
        %{
          key: "profile-bound-work",
          title: "Profile-bound work",
          description: "The validator profile must exist before plan admission.",
          required: true,
          integration: true,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: baseline_subject(repository.id)
        }
      ])

    assert :invalid_plan ==
             reject_plan_admission!(created.goal.id, proposal, validation_profiles: [])

    assert Repo.aggregate(WorkItem, :count) == 0
  end

  test "plan admission accepts reverse-sorted repository resources and retains cross-resource edges" do
    project = project_fixture()

    [consumer_repository, source_repository] =
      [repository_fixture(project), repository_fixture(project)]
      |> Enum.sort_by(& &1.id)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

    planned =
      accept_plan!(
        created.goal.id,
        plan_proposal(created.goal.id, [
          %{
            key: "source",
            title: "Source work",
            description: "Produce an independently scoped source outcome.",
            required: true,
            integration: false,
            repository_resource_id: source_repository.id,
            acceptance: check_contract(),
            depends_on_keys: [],
            model_profile: "codex",
            baseline: baseline_subject(source_repository.id)
          },
          %{
            key: "consumer",
            title: "Consumer work",
            description: "May depend on completion in another repository.",
            required: true,
            integration: true,
            repository_resource_id: consumer_repository.id,
            acceptance: check_contract(),
            depends_on_keys: ["source"],
            model_profile: "codex",
            baseline: baseline_subject(consumer_repository.id)
          }
        ])
      )

    assert Enum.map(planned.goal.work_items, & &1.repository_resource_id) |> Enum.sort() ==
             [consumer_repository.id, source_repository.id]
  end

  test "amended planning Task results remain historical and do not create current authorization" do
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

    assert {:ok, admitted, :created} =
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

    task = Repo.get!(Task, admitted.response["task"]["id"])

    assert {:ok, amended, :created} =
             command_current(created.goal.id, "amend", %{
               revision_contract: amended_revision_contract("A later approved objective."),
               reason: "Supersede the planning attempt."
             })

    runtime = plan_runtime_fixture(repository)
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    proposal =
      plan_proposal(created.goal.id, [
        %{
          key: "historical-work",
          title: "Historical planning work",
          description: "Must not become current authorization.",
          required: true,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: baseline_subject(repository.id)
        }
      ])
      |> Map.put(:expected_revision, task.goal_revision)

    put_task_result!(run, task_result(task, "plan_proposed", %{proposal: proposal}))

    assert {:ok, receipt} = Goals.settle_task(task.id, run.id, 1, now: @now)
    assert receipt["settlement"] == "historical_result"
    assert receipt["decision_id"] == nil
    assert amended.goal.current_revision == 2
    assert Repo.aggregate(WorkItem, :count) == 0
    assert Repo.aggregate(SymmetryControl.Goals.GoalDecision, :count) == 0
  end

  test "a completed Run is not accepted work and a producer cannot validate itself" do
    {goal, item, task} = admitted_task_fixture()
    now = @now

    Repo.update_all(from(task in Task, where: task.id == ^task.id),
      set: [state: "completed", updated_at: now]
    )

    runtime = runtime_fixture()

    run =
      %Run{}
      |> Run.changeset(%{
        task_id: task.id,
        runtime_id: runtime.id,
        generation: 1,
        state: "completed",
        assigned_at: now,
        assignment_expires_at: DateTime.add(now, 60, :second)
      })
      |> Repo.insert!()

    assert {:ok, projection} = Goals.fetch_goal(goal.id)
    refute Enum.any?(projection.accepted_outcomes, &(&1.work_item_id == item.id))

    assert {:error, :outcome_derived} =
             Goals.accept_outcome(goal.id, %{
               work_item_id: item.id,
               producing_task_id: task.id,
               producing_run_id: run.id,
               subject_hash: Base.encode16(:crypto.hash(:sha256, "candidate"), case: :lower),
               evidence_ids: [],
               reason: "A producer result alone is not acceptance"
             })

    assert {:error, :invalid_request} = command_current(goal.id, "achieve", %{})
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
      assert {:error, :invalid_request} =
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
               command(current, "admit_task", admission_payload(item), mutation_id),
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

    assert {:error, :invalid_request} =
             command_current(
               goal.id,
               "admit_task",
               Map.put(admission_payload(item), :validation_of_task_id, Ecto.UUID.generate()),
               rollout_enabled: true
             )
  end

  test "evidence stores observed_at in its row timestamp without polluting the payload" do
    {_goal, item, _task, runtime, run, fence} = validation_goal_run_fixture()
    observed_at = DateTime.add(@now, -60, :second)

    evidence =
      evidence_attrs(run.id, item)
      |> Map.put(:observed_at, DateTime.to_iso8601(observed_at))

    assert {:ok, %{evidence: receipt}, :created} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, evidence, now: @now)

    row = Repo.get!(SymmetryControl.Goals.RunEvidence, evidence.evidence_id)
    assert row.inserted_at == observed_at
    refute Map.has_key?(row.payload, "_observed_at")
    assert receipt.observed_at == DateTime.to_iso8601(observed_at)

    assert {:ok, %{evidence: ^receipt}, :replayed} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, evidence, now: @now)
  end

  test "terminal producer, validation evidence, accepted outcome, and achieve close the Goal loop" do
    {goal, item, producer} = admitted_task_fixture()
    producer_runtime = runtime_fixture(@validation_runtime_id)

    candidate_subject = %{
      "resource_id" => item_repository_resource_id(item),
      "commit" => String.duplicate("c", 40),
      "tree_digest" => "sha256:" <> String.duplicate("d", 64)
    }

    {producer_run, _producer_fence} =
      completed_goal_run_fixture(producer, producer_runtime, candidate_subject)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(producer.id, producer_run.id, 1, now: @now)

    assert {:error, :invalid_validation} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 purpose: "validate",
                 validation_of_task_id: producer.id,
                 reserved_microusd: 0
               })
               |> Map.put(:subject, candidate_subject),
               rollout_enabled: true
             )

    assert {:ok, validation_receipt, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 purpose: "validate",
                 validation_of_task_id: producer.id,
                 reserved_microusd: 0
               })
               |> Map.delete(:subject),
               rollout_enabled: true
             )

    validation = Repo.get!(Task, validation_receipt.response["task"]["id"])
    assert validation.input["subject"] == candidate_subject
    validation_runtime = runtime_fixture(@validation_runtime_id)

    {validation_run, validation_fence} =
      completed_goal_run_fixture(validation, validation_runtime)

    evidence = evidence_attrs(validation_run.id, item, candidate_subject)

    assert {:ok, %{evidence: stored_evidence}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence,
               now: @now
             )

    assert stored_evidence.subject_hash == evidence.subject_hash

    old_subject_evidence = evidence_attrs(validation_run.id, item)

    assert {:error, :invalid_evidence_identity} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               Map.put(old_subject_evidence, :evidence_key, "check:initial-subject"),
               now: @now
             )

    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, %{"settlement" => "accepted", "reason" => "validation_passed"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert wakeup_job_count(goal.id) == 1

    assert {:ok, %{evidence: ^stored_evidence}, :replayed} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence,
               now: @now
             )

    assert {:error, :validation_evidence_closed} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence
               |> Map.put(:evidence_id, Ecto.UUID.generate())
               |> Map.put(:evidence_key, "check:late"),
               now: @now
             )

    assert {:error, :invalid_validation} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 purpose: "validate",
                 validation_of_task_id: validation.id,
                 reserved_microusd: 0
               })
               |> Map.delete(:subject),
               rollout_enabled: true
             )

    subject_hash = String.replace_prefix(evidence.subject_hash, "sha256:", "")

    [accepted_outcome] =
      Repo.all(
        from(outcome in SymmetryControl.Goals.WorkOutcome,
          where:
            outcome.goal_id == ^goal.id and outcome.work_item_id == ^item.id and
              outcome.validation_task_id == ^validation.id and outcome.disposition == "accepted"
        )
      )

    assert accepted_outcome.candidate_subject == candidate_subject
    assert accepted_outcome.producing_result_id == producer_run.result["task_result"]["result_id"]

    assert {:error, :outcome_derived} =
             Goals.accept_outcome(goal.id, %{
               work_item_id: item.id,
               producing_task_id: producer.id,
               producing_run_id: producer_run.id,
               validation_task_id: validation.id,
               subject_hash: subject_hash,
               evidence_ids: [stored_evidence.id],
               reason: "A validator outcome is derived during settlement."
             })

    assert {:error, :operator_acceptance_required} =
             command_current(goal.id, "achieve", %{
               subject: evidence.subject,
               integration_work_item_id: item.id,
               evidence_ids: [stored_evidence.id]
             })

    assert {:error, :invalid_request} =
             command_current(goal.id, "request_decision", %{
               kind: "completion",
               work_item_id: item.id,
               subject_hash: subject_hash,
               question: "Accept one work item?",
               options: [
                 %{
                   "id" => "accept",
                   "label" => "Accept",
                   "consequence" => "Mark the Goal achieved"
                 }
               ]
             })

    assert {:ok, completion_request, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "completion",
               subject_hash: subject_hash,
               question: "Accept the validated Goal outcome?",
               options: [
                 %{
                   "id" => "accept",
                   "label" => "Accept",
                   "consequence" => "Mark the Goal achieved"
                 }
               ]
             })

    decision_id = completion_request.response["decision"]["id"]
    decision_version = Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).lock_version

    assert {:ok, _resolved, :created} =
             command_current(goal.id, "resolve_decision", %{
               decision_id: decision_id,
               expected_decision_version: decision_version,
               option_id: "accept"
             })

    weaker_evidence =
      %SymmetryControl.Goals.RunEvidence{}
      |> SymmetryControl.Goals.RunEvidence.changeset(%{
        run_id: validation_run.id,
        evidence_key: "check:weaker-profile",
        kind: "check",
        subject_hash: SymmetryControl.RequestHash.canonical(evidence.subject),
        source_ref: Map.put(evidence.source_ref, "validator_profile", "weaker"),
        source_revision: evidence.source_revision,
        validator_profile: "weaker",
        verdict: "passed",
        payload: evidence.payload
      })
      |> Repo.insert!()

    assert {:error, :missing_evidence} =
             command_current(goal.id, "achieve", %{
               subject: evidence.subject,
               integration_work_item_id: item.id,
               evidence_ids: [weaker_evidence.id],
               decision_id: decision_id
             })

    assert {:error, :invalid_request} =
             command_current(goal.id, "achieve", %{
               subject: evidence.subject,
               evidence_ids: [stored_evidence.id],
               decision_id: decision_id
             })

    assert {:error, :integration_outcome_required} =
             command_current(goal.id, "achieve", %{
               subject: evidence.subject,
               integration_work_item_id: Ecto.UUID.generate(),
               evidence_ids: [stored_evidence.id],
               decision_id: decision_id
             })

    optional_dependency = optional_goal_work_item!(goal, item, "Optional prerequisite")
    optional_follow_up = optional_goal_work_item!(goal, item, "Optional follow-up")

    %SymmetryControl.Goals.WorkDependency{}
    |> SymmetryControl.Goals.WorkDependency.changeset(%{
      goal_id: goal.id,
      work_item_id: optional_follow_up.id,
      depends_on_id: optional_dependency.id
    })
    |> Repo.insert!()

    assert {:ok, achieved, :created} =
             command_current(goal.id, "achieve", %{
               subject: evidence.subject,
               integration_work_item_id: item.id,
               evidence_ids: [stored_evidence.id],
               decision_id: decision_id
             })

    assert achieved.goal.state == "achieved"

    assert Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.id == ^accepted_outcome.id
             )
           )
  end

  test "accept_outcome is retired and cannot bypass terminal settlement" do
    {goal, item, _task} = admitted_task_fixture()

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract: amended_revision_contract("Revised acceptance subject."),
               reason: "Scope changed"
             })

    assert amended.goal.current_revision == goal.current_revision + 1

    assert {:error, :outcome_derived} =
             Goals.accept_outcome(goal.id, %{
               work_item_id: item.id,
               producing_task_id: Ecto.UUID.generate(),
               producing_run_id: Ecto.UUID.generate(),
               validation_task_id: Ecto.UUID.generate(),
               subject_hash: String.duplicate("f", 64),
               evidence_ids: [],
               reason: "A superseded revision cannot be accepted."
             })
  end

  test "an accepted current-revision WorkItem rejects later manual admission" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    assert {:ok, %{evidence: _stored}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence_attrs(validation_run.id, item, candidate_subject),
               now: @now
             )

    assert {:ok, %{"settlement" => "accepted"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert {:error, :already_accepted} =
             command_current(goal.id, "admit_task", admission_payload(item),
               rollout_enabled: true
             )
  end

  test "incomplete validation closes its evidence set without accepting work" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    refute Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.goal_id == ^goal.id and outcome.validation_task_id == ^validation.id
             )
           )

    late_evidence =
      evidence_attrs(validation_run.id, item, candidate_subject)
      |> Map.put(:evidence_id, Ecto.UUID.generate())
      |> Map.put(:evidence_key, "check:late-after-incomplete")

    assert {:error, :validation_evidence_closed} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               late_evidence,
               now: @now
             )
  end

  test "automatic reconciliation does not admit an accepted current-revision WorkItem" do
    policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 4,
      "allowed_actions" => ["validate"],
      "allowed_model_profiles" => ["codex"],
      "allowed_runtime_ids" => [],
      "budget_limit_microusd" => nil
    }

    {goal, item, producer} =
      admitted_task_fixture("primary", check_contract(), %{execution_policy: policy})

    candidate_subject = %{
      "resource_id" => item_repository_resource_id(item),
      "commit" => String.duplicate("c", 40),
      "tree_digest" => "sha256:" <> String.duplicate("d", 64)
    }

    {producer_run, _producer_fence} =
      completed_goal_run_fixture(producer, runtime_fixture(), candidate_subject)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(producer.id, producer_run.id, 1, now: @now)

    %SymmetryControl.Goals.WorkOutcome{}
    |> SymmetryControl.Goals.WorkOutcome.changeset(%{
      goal_id: goal.id,
      goal_revision: goal.current_revision,
      work_item_id: item.id,
      producing_task_id: producer.id,
      producing_run_id: producer_run.id,
      validation_task_id: nil,
      candidate_subject: candidate_subject,
      subject_hash: SymmetryControl.RequestHash.canonical(candidate_subject),
      producing_result_id: producer_run.result["task_result"]["result_id"],
      evidence_ids: [],
      disposition: "accepted",
      reason: "validation_passed",
      decision_id: nil
    })
    |> Repo.insert!()

    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :noop}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 1
  end

  test "operator acceptance requires a current scoped review and wakes closed validation evaluation once" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} =
      independent_validation_attempt_fixture(check_and_operator_acceptance_contract())

    evidence = evidence_attrs(validation_run.id, item, candidate_subject)

    assert {:ok, %{evidence: _stored}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence,
               now: @now
             )

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    subject_hash = evidence_subject_hash(candidate_subject)

    assert {:ok, completion, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "completion",
               subject_hash: subject_hash,
               question: "Accept the Goal?",
               options: [%{"id" => "accept", "label" => "Accept", "consequence" => "Finish"}]
             })

    completion_id = completion.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(
               goal.id,
               "resolve_decision",
               %{
                 decision_id: completion_id,
                 expected_decision_version:
                   Repo.get!(SymmetryControl.Goals.GoalDecision, completion_id).lock_version,
                 option_id: "accept"
               },
               now: DateTime.add(@now, 1, :second)
             )

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert {:ok, unscoped_review, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "review",
               subject_hash: subject_hash,
               question: "Accept without an item?",
               options: [
                 %{"id" => "accept", "label" => "Accept", "consequence" => "Incorrect scope"}
               ]
             })

    unscoped_review_id = unscoped_review.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(
               goal.id,
               "resolve_decision",
               %{
                 decision_id: unscoped_review_id,
                 expected_decision_version:
                   Repo.get!(SymmetryControl.Goals.GoalDecision, unscoped_review_id).lock_version,
                 option_id: "accept"
               },
               now: DateTime.add(@now, 2, :second)
             )

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert {:ok, wrong_subject_review, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "review",
               work_item_id: item.id,
               subject_hash:
                 evidence_subject_hash(%{
                   candidate_subject
                   | "commit" => String.duplicate("e", 40)
                 }),
               question: "Accept the wrong subject?",
               options: [
                 %{"id" => "accept", "label" => "Accept", "consequence" => "Incorrect subject"}
               ]
             })

    wrong_subject_review_id = wrong_subject_review.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(
               goal.id,
               "resolve_decision",
               %{
                 decision_id: wrong_subject_review_id,
                 expected_decision_version:
                   Repo.get!(SymmetryControl.Goals.GoalDecision, wrong_subject_review_id).lock_version,
                 option_id: "accept"
               },
               now: DateTime.add(@now, 3, :second)
             )

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    refute Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where:
                 outcome.validation_task_id == ^validation.id and
                   outcome.disposition == "accepted"
             )
           )

    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, scoped_review, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "review",
               work_item_id: item.id,
               subject_hash: subject_hash,
               question: "Accept this validated WorkItem?",
               options: [%{"id" => "accept", "label" => "Accept", "consequence" => "Accept work"}]
             })

    scoped_review_id = scoped_review.response["decision"]["id"]
    assert {:ok, current_goal} = Goals.fetch_goal(goal.id)

    resolve_command =
      command(current_goal, "resolve_decision", %{
        decision_id: scoped_review_id,
        expected_decision_version:
          Repo.get!(SymmetryControl.Goals.GoalDecision, scoped_review_id).lock_version,
        option_id: "accept"
      })

    assert {:ok, _resolved, :created} =
             Goals.command(
               goal.id,
               resolve_command,
               "operator:test",
               now: DateTime.add(@now, 4, :second),
               validation_profiles: validation_profiles()
             )

    assert wakeup_job_count(goal.id) == 1

    assert {:ok, _replayed, :replayed} =
             Goals.command(
               goal.id,
               resolve_command,
               "operator:test",
               now: DateTime.add(@now, 4, :second),
               validation_profiles: validation_profiles()
             )

    previous_goals = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(previous_goals, :rollout_enabled, true)
    )

    on_exit(fn -> Application.put_env(:symmetry_control, :goals, previous_goals) end)

    assert :ok = WakeupWorker.perform(%Oban.Job{args: %{"goal_id" => goal.id}})
    assert :ok = WakeupWorker.perform(%Oban.Job{args: %{"goal_id" => goal.id}})

    assert [late_outcome_event] =
             Repo.all(
               from(event in SymmetryControl.Goals.GoalEvent,
                 where:
                   event.goal_id == ^goal.id and event.kind == "validation_outcome_settled" and
                     event.payload["task_id"] == ^validation.id,
                 order_by: [asc: event.sequence]
               )
             )

    assert late_outcome_event.response["settlement"] == "accepted"

    assert {:ok, late_receipt} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert late_receipt["settlement"] == "accepted"
    assert late_receipt["event_sequence"] == late_outcome_event.sequence

    assert {:ok, projection} = Goals.fetch_goal(goal.id)
    assert Enum.any?(projection.accepted_outcomes, &(&1.work_item_id == item.id))

    assert [%{decision_id: ^scoped_review_id}] =
             Repo.all(
               from(outcome in SymmetryControl.Goals.WorkOutcome,
                 where:
                   outcome.validation_task_id == ^validation.id and
                     outcome.disposition == "accepted",
                 select: %{decision_id: outcome.decision_id}
               )
             )
  end

  test "operator review rejection settles the corresponding validation as rejected once" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} =
      independent_validation_attempt_fixture(check_and_operator_acceptance_contract())

    assert {:ok, %{evidence: _stored}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence_attrs(validation_run.id, item, candidate_subject),
               now: @now
             )

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert {:ok, review, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "review",
               work_item_id: item.id,
               subject_hash: evidence_subject_hash(candidate_subject),
               question: "Reject this validated WorkItem?",
               options: [
                 %{"id" => "accept", "label" => "Accept", "consequence" => "Accept work"},
                 %{"id" => "reject", "label" => "Reject", "consequence" => "Keep work open"}
               ]
             })

    review_id = review.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(goal.id, "resolve_decision", %{
               decision_id: review_id,
               expected_decision_version:
                 Repo.get!(SymmetryControl.Goals.GoalDecision, review_id).lock_version,
               option_id: "reject"
             })

    assert :ok = WakeupWorker.perform(%Oban.Job{args: %{"goal_id" => goal.id}})
    assert :ok = WakeupWorker.perform(%Oban.Job{args: %{"goal_id" => goal.id}})

    validation_id = validation.id

    assert [
             %{
               disposition: "rejected",
               decision_id: ^review_id,
               validation_task_id: ^validation_id
             }
           ] =
             Repo.all(
               from(outcome in SymmetryControl.Goals.WorkOutcome,
                 where:
                   outcome.validation_task_id == ^validation.id and
                     outcome.disposition == "rejected",
                 select: %{
                   disposition: outcome.disposition,
                   decision_id: outcome.decision_id,
                   validation_task_id: outcome.validation_task_id
                 }
               )
             )

    assert [%{response: response}] =
             Repo.all(
               from(event in SymmetryControl.Goals.GoalEvent,
                 where:
                   event.goal_id == ^goal.id and event.kind == "validation_outcome_settled" and
                     event.payload["task_id"] == ^validation.id,
                 select: %{response: event.response}
               )
             )

    assert response["settlement"] == "rejected"
    assert {:ok, []} = Goals.recover_pending_terminal_settlements(goal_ids: [goal.id], now: @now)
  end

  test "a competing accepted Subject is historical and does not loop validation recovery" do
    {goal, item, producer, producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} =
      independent_validation_attempt_fixture(check_and_operator_acceptance_contract())

    assert {:ok, %{evidence: _stored}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence_attrs(validation_run.id, item, candidate_subject),
               now: @now
             )

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    accepted_subject = Map.put(candidate_subject, "commit", String.duplicate("a", 40))

    %SymmetryControl.Goals.WorkOutcome{}
    |> SymmetryControl.Goals.WorkOutcome.changeset(%{
      goal_id: goal.id,
      goal_revision: goal.current_revision,
      work_item_id: item.id,
      producing_task_id: producer.id,
      producing_run_id: producer_run.id,
      validation_task_id: nil,
      candidate_subject: accepted_subject,
      subject_hash: SymmetryControl.RequestHash.canonical(accepted_subject),
      producing_result_id: producer_run.result["task_result"]["result_id"],
      evidence_ids: [],
      disposition: "accepted",
      reason: "validation_passed",
      decision_id: nil
    })
    |> Repo.insert!()

    assert {:ok, review, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "review",
               work_item_id: item.id,
               subject_hash: evidence_subject_hash(candidate_subject),
               question: "Accept this validated WorkItem?",
               options: [%{"id" => "accept", "label" => "Accept", "consequence" => "Accept work"}]
             })

    review_id = review.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(goal.id, "resolve_decision", %{
               decision_id: review_id,
               expected_decision_version:
                 Repo.get!(SymmetryControl.Goals.GoalDecision, review_id).lock_version,
               option_id: "accept"
             })

    validation_id = validation.id

    assert {:ok, [%{task_id: ^validation_id}]} =
             Goals.recover_pending_terminal_settlements(goal_ids: [goal.id], now: @now)

    assert :ok = WakeupWorker.perform(%Oban.Job{args: %{"goal_id" => goal.id}})
    assert :ok = WakeupWorker.perform(%Oban.Job{args: %{"goal_id" => goal.id}})

    assert [%{disposition: "rejected", reason: "historical_result"}] =
             Repo.all(
               from(outcome in SymmetryControl.Goals.WorkOutcome,
                 where: outcome.validation_task_id == ^validation_id,
                 select: %{disposition: outcome.disposition, reason: outcome.reason}
               )
             )

    assert [%{response: response}] =
             Repo.all(
               from(event in SymmetryControl.Goals.GoalEvent,
                 where:
                   event.goal_id == ^goal.id and event.kind == "validation_outcome_settled" and
                     event.payload["task_id"] == ^validation_id,
                 select: %{response: event.response}
               )
             )

    assert response["settlement"] == "historical_result"
    assert {:ok, []} = Goals.recover_pending_terminal_settlements(goal_ids: [goal.id], now: @now)
  end

  test "failed validation evidence remains rejected even with scoped operator acceptance" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} =
      independent_validation_attempt_fixture(check_and_operator_acceptance_contract())

    failed_evidence =
      evidence_attrs(validation_run.id, item, candidate_subject)
      |> Map.put(:verdict, "failed")
      |> put_in([:payload, :exit_code], 1)

    assert {:ok, %{evidence: _failed}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               failed_evidence,
               now: @now
             )

    assert {:ok, %{"settlement" => "validation_failed"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert {:ok, scoped_review, :created} =
             command_current(goal.id, "request_decision", %{
               kind: "review",
               work_item_id: item.id,
               subject_hash: evidence_subject_hash(candidate_subject),
               question: "Accept failed work?",
               options: [
                 %{
                   "id" => "accept",
                   "label" => "Accept",
                   "consequence" => "Must not override failure"
                 }
               ]
             })

    scoped_review_id = scoped_review.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(goal.id, "resolve_decision", %{
               decision_id: scoped_review_id,
               expected_decision_version:
                 Repo.get!(SymmetryControl.Goals.GoalDecision, scoped_review_id).lock_version,
               option_id: "accept"
             })

    assert Repo.aggregate(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where:
                 outcome.validation_task_id == ^validation.id and
                   outcome.disposition == "rejected"
             ),
             :count
           ) == 1

    refute Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where:
                 outcome.validation_task_id == ^validation.id and
                   outcome.disposition == "accepted"
             )
           )
  end

  test "unknown and not-applicable validation evidence remain awaiting validation" do
    for verdict <- ["unknown", "not_applicable"] do
      {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
       validation_fence, candidate_subject} = independent_validation_attempt_fixture()

      evidence =
        evidence_attrs(validation_run.id, item, candidate_subject)
        |> Map.put(:evidence_id, Ecto.UUID.generate())
        |> Map.put(:evidence_key, "check:#{verdict}")
        |> Map.put(:verdict, verdict)

      assert {:ok, %{evidence: _stored}, :created} =
               Goals.append_evidence(
                 validation_runtime.machine_id,
                 validation_run.id,
                 validation_fence,
                 evidence,
                 now: @now
               )

      assert {:ok, %{"settlement" => "awaiting_validation"}} =
               Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

      refute Repo.exists?(
               from(outcome in SymmetryControl.Goals.WorkOutcome,
                 where:
                   outcome.goal_id == ^goal.id and outcome.validation_task_id == ^validation.id
               )
             )
    end
  end

  test "failed independent validation records one immutable rejection and cannot be bypassed by passed evidence from that attempt" do
    {goal, item, producer, producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    failed_evidence =
      evidence_attrs(validation_run.id, item, candidate_subject)
      |> Map.put(:evidence_id, Ecto.UUID.generate())
      |> Map.put(:evidence_key, "check:failed")
      |> Map.put(:verdict, "failed")
      |> put_in([:payload, :exit_code], 1)

    assert {:ok, %{evidence: failed}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               failed_evidence,
               now: @now
             )

    passed_evidence =
      evidence_attrs(validation_run.id, item, candidate_subject)
      |> Map.put(:evidence_id, Ecto.UUID.generate())
      |> Map.put(:evidence_key, "check:passed")

    assert {:ok, %{evidence: passed}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               passed_evidence,
               now: @now
             )

    assert {:ok, receipt} = Goals.settle_task(validation.id, validation_run.id, 1, now: @now)
    assert receipt["settlement"] == "validation_failed"
    assert receipt["reason"] == "validation_failed"
    assert receipt["next_wake_at"] == nil

    [rejected] =
      Repo.all(
        from(outcome in SymmetryControl.Goals.WorkOutcome,
          where:
            outcome.goal_id == ^goal.id and outcome.work_item_id == ^item.id and
              outcome.disposition == "rejected"
        )
      )

    assert rejected.goal_revision == goal.current_revision
    assert rejected.producing_task_id == producer.id
    assert rejected.producing_run_id == producer_run.id
    assert rejected.validation_task_id == validation.id
    assert rejected.candidate_subject == candidate_subject
    assert rejected.subject_hash == SymmetryControl.RequestHash.canonical(candidate_subject)
    assert rejected.producing_result_id == producer_run.result["task_result"]["result_id"]
    assert rejected.evidence_ids == [failed.id]
    assert rejected.reason == "validation_failed"

    subject_hash = evidence_subject_hash(candidate_subject)

    assert {:error, :outcome_derived} =
             Goals.accept_outcome(goal.id, %{
               work_item_id: item.id,
               producing_task_id: producer.id,
               producing_run_id: producer_run.id,
               validation_task_id: validation.id,
               subject_hash: String.replace_prefix(subject_hash, "sha256:", ""),
               evidence_ids: [passed.id],
               reason: "A passed subset cannot hide a required failed check."
             })

    assert {:ok, ^receipt} = Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert Repo.aggregate(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where:
                 outcome.validation_task_id == ^validation.id and
                   outcome.disposition == "rejected"
             ),
             :count
           ) == 1

    assert {:ok, retry_admission, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 purpose: "validate",
                 validation_of_task_id: producer.id,
                 reserved_microusd: 0
               })
               |> Map.delete(:subject),
               rollout_enabled: true
             )

    retry_validation = Repo.get!(Task, retry_admission.response["task"]["id"])
    retry_runtime = runtime_fixture(@validation_runtime_id)

    {retry_run, retry_fence} =
      completed_goal_run_fixture(retry_validation, retry_runtime, candidate_subject)

    retry_evidence =
      evidence_attrs(retry_run.id, item, candidate_subject)
      |> Map.put(:evidence_key, "check:retry-passed")

    assert {:ok, %{evidence: retry_passed}, :created} =
             Goals.append_evidence(
               retry_runtime.machine_id,
               retry_run.id,
               retry_fence,
               retry_evidence,
               now: @now
             )

    assert {:ok, %{"settlement" => "accepted", "reason" => "validation_passed"}} =
             Goals.settle_task(retry_validation.id, retry_run.id, 1, now: @now)

    assert Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where:
                 outcome.goal_id == ^goal.id and outcome.work_item_id == ^item.id and
                   outcome.validation_task_id == ^retry_validation.id and
                   outcome.disposition == "accepted" and
                   outcome.evidence_ids == ^[retry_passed.id]
             )
           )
  end

  test "failed validation evidence does not turn a self-declared failure into a rejected outcome" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    failed_evidence =
      evidence_attrs(validation_run.id, item, candidate_subject)
      |> Map.put(:evidence_id, Ecto.UUID.generate())
      |> Map.put(:evidence_key, "check:self-declared-failure")
      |> Map.put(:verdict, "failed")
      |> put_in([:payload, :exit_code], 1)

    assert {:ok, %{evidence: _failed}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               failed_evidence,
               now: @now
             )

    put_task_result!(
      validation_run,
      task_result(validation, "failed", %{"reason" => "network"})
    )

    assert {:ok, %{"settlement" => "failed"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    refute Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.goal_id == ^goal.id and outcome.disposition == "rejected"
             )
           )
  end

  test "failed Runs preserve safe normalized semantic reasons without deriving outcomes or retries" do
    for {shape, expected_reason} <- [
          {:daemon_terminal_payload, "unknown_outcome"},
          {:persisted_failure, "lease_expired"},
          {:persisted_failure, "attempt_limit"},
          {:persisted_failure, "supervised_worker_lost"},
          {:existing_task_result, "network"}
        ] do
      {goal, _item, task, _runtime, run, _fence} = claimed_goal_run_fixture()

      result =
        case shape do
          :daemon_terminal_payload ->
            %{
              "summary" => "Native terminal framing failed.",
              "reason" => "unknown_outcome",
              "error" => "close failed"
            }

          :persisted_failure ->
            %{"summary" => "A terminal daemon failure occurred.", "reason" => expected_reason}

          :existing_task_result ->
            %{"task_result" => task_result(task, "failed", %{"reason" => "network"})}
        end

      Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "failed"])

      Repo.update_all(
        from(row in Run, where: row.id == ^run.id),
        set:
          if(shape in [:daemon_terminal_payload, :persisted_failure],
            do: [state: "failed", result: nil, failure: result],
            else: [state: "failed", result: result, failure: nil]
          )
      )

      assert {:ok, %{"settlement" => "failed", "reason" => ^expected_reason}} =
               Goals.settle_task(task.id, run.id, 1, now: @now)

      refute Repo.exists?(
               from(outcome in SymmetryControl.Goals.WorkOutcome,
                 where: outcome.goal_id == ^goal.id
               )
             )

      assert Repo.aggregate(from(candidate in Run, where: candidate.task_id == ^task.id), :count) ==
               1
    end

    for invalid_result <- [
          nil,
          %{"task_result" => %{"reason" => "unknown_outcome"}},
          %{"summary" => "Untrusted", "reason" => "outside_whitelist"},
          %{"reason" => "unknown_outcome"}
        ] do
      {invalid_goal, _invalid_item, invalid_task, _invalid_runtime, invalid_run, _invalid_fence} =
        claimed_goal_run_fixture()

      Repo.update_all(from(row in Task, where: row.id == ^invalid_task.id),
        set: [state: "failed"]
      )

      Repo.update_all(
        from(row in Run, where: row.id == ^invalid_run.id),
        set: [state: "failed", result: nil, failure: invalid_result]
      )

      assert {:ok, %{"settlement" => "failed", "reason" => "process_failure"}} =
               Goals.settle_task(invalid_task.id, invalid_run.id, 1, now: @now)

      refute Repo.exists?(
               from(outcome in SymmetryControl.Goals.WorkOutcome,
                 where: outcome.goal_id == ^invalid_goal.id
               )
             )
    end
  end

  test "failed validation evidence cannot reject a candidate that differs from the frozen producer Subject" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    failed_evidence =
      evidence_attrs(validation_run.id, item, candidate_subject)
      |> Map.put(:evidence_id, Ecto.UUID.generate())
      |> Map.put(:evidence_key, "check:mismatched-candidate")
      |> Map.put(:verdict, "failed")
      |> put_in([:payload, :exit_code], 1)

    assert {:ok, %{evidence: _failed}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               failed_evidence,
               now: @now
             )

    put_task_result!(
      validation_run,
      task_result(validation, "candidate_completion", %{"subject" => evidence_subject(item)})
    )

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    refute Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.goal_id == ^goal.id and outcome.disposition == "rejected"
             )
           )
  end

  test "paused current revision records trusted validation rejection without waking execution" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    failed_evidence =
      evidence_attrs(validation_run.id, item, candidate_subject)
      |> Map.put(:evidence_id, Ecto.UUID.generate())
      |> Map.put(:evidence_key, "check:paused-failure")
      |> Map.put(:verdict, "failed")
      |> put_in([:payload, :exit_code], 1)

    assert {:ok, %{evidence: _failed}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               failed_evidence,
               now: @now
             )

    assert {:ok, _paused, :created} =
             command_current(goal.id, "pause", %{reason: "Pause before validation settlement."})

    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, %{"settlement" => "historical_result", "next_wake_at" => nil}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert Repo.aggregate(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.goal_id == ^goal.id and outcome.disposition == "rejected"
             ),
             :count
           ) == 1

    assert wakeup_job_count(goal.id) == 0
  end

  test "paused current revision records accepted validation without waking execution" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    evidence = evidence_attrs(validation_run.id, item, candidate_subject)

    assert {:ok, %{evidence: _stored}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence,
               now: @now
             )

    assert {:ok, _paused, :created} =
             command_current(goal.id, "pause", %{reason: "Pause before validation settlement."})

    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, %{"settlement" => "accepted", "next_wake_at" => nil}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    assert Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where:
                 outcome.goal_id == ^goal.id and outcome.validation_task_id == ^validation.id and
                   outcome.disposition == "accepted"
             )
           )

    assert wakeup_job_count(goal.id) == 0
  end

  test "superseded validation failure remains historical and cannot reject the new revision" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} = independent_validation_attempt_fixture()

    failed_evidence =
      evidence_attrs(validation_run.id, item, candidate_subject)
      |> Map.put(:evidence_id, Ecto.UUID.generate())
      |> Map.put(:evidence_key, "check:superseded-failure")
      |> Map.put(:verdict, "failed")
      |> put_in([:payload, :exit_code], 1)

    assert {:ok, %{evidence: _failed}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               failed_evidence,
               now: @now
             )

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract: amended_revision_contract("A new approved revision."),
               reason: "Supersede the failed validation."
             })

    assert amended.goal.current_revision == goal.current_revision + 1

    assert {:ok, %{"settlement" => "historical_result"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    refute Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.goal_id == ^goal.id and outcome.disposition == "rejected"
             )
           )
  end

  test "invalid blockers settle durably without scheduling a retry loop" do
    {goal, _item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    put_task_result!(
      run,
      task_result(task, "blocked", %{
        "blocker" => %{"kind" => "decision", "decision_id" => Ecto.UUID.generate()}
      })
    )

    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, receipt} = Goals.settle_task(task.id, run.id, 1, now: @now)
    assert receipt["settlement"] == "invalid_task_result"
    assert receipt["reason"] == "invalid_blocker"
    assert receipt["next_wake_at"] == nil
    assert wakeup_job_count(goal.id) == 0

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "unknown"
  end

  test "repair and replan results are retained as proposals without admitting work" do
    {goal, item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)
    tasks_before = Repo.aggregate(Task, :count)

    repair = %{
      "kind" => "repair",
      "work_item_id" => item.id,
      "reason" => "Fix the failing check."
    }

    put_task_result!(
      run,
      task_result(task, "repair_required", %{
        "proposed_next_action" => repair
      })
    )

    assert {:ok, repair_receipt} = Goals.settle_task(task.id, run.id, 1, now: @now)
    assert repair_receipt["settlement"] == "repair_required"
    assert repair_receipt["proposed_next_action"] == repair
    assert repair_receipt["proposed_next_action_status"] == "proposal_only"
    assert repair_receipt["next_wake_at"] == nil
    assert Repo.aggregate(Task, :count) == tasks_before

    assert {:ok, admitted, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{reserved_microusd: 0}),
               rollout_enabled: true
             )

    replan_task = Repo.get!(Task, admitted.response["task"]["id"])
    {replan_run, _fence} = completed_goal_run_fixture(replan_task, runtime)
    replan = %{"kind" => "replan", "reason" => "The approved plan needs a scope decision."}

    put_task_result!(
      replan_run,
      task_result(replan_task, "replan_required", %{
        "proposed_next_action" => replan
      })
    )

    assert {:ok, replan_receipt} =
             Goals.settle_task(replan_task.id, replan_run.id, 1, now: @now)

    assert replan_receipt["settlement"] == "replan_required"
    assert replan_receipt["proposed_next_action"] == replan
    assert replan_receipt["proposed_next_action_status"] == "proposal_only"
    assert replan_receipt["next_wake_at"] == nil
    assert Repo.aggregate(Task, :count) == tasks_before + 1
  end

  test "duplicate progress without a new Subject or verified evidence stops at no_verified_progress" do
    {goal, item, first_task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {first_run, _fence} = completed_goal_run_fixture(first_task, runtime)
    first_progress = task_result(first_task, "progress")

    put_task_result!(first_run, first_progress)
    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, %{"settlement" => "progress"}} =
             Goals.settle_task(first_task.id, first_run.id, 1, now: @now)

    assert {:ok, admitted, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{reserved_microusd: 0}),
               rollout_enabled: true
             )

    second_task = Repo.get!(Task, admitted.response["task"]["id"])
    {second_run, _fence} = completed_goal_run_fixture(second_task, runtime)
    put_task_result!(second_run, task_result(second_task, "progress"))
    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, receipt} = Goals.settle_task(second_task.id, second_run.id, 1, now: @now)
    assert receipt["settlement"] == "no_verified_progress"
    assert receipt["next_wake_at"] == nil
    assert wakeup_job_count(goal.id) == 0
  end

  test "terminal settlement persists one replay-safe wakeup job" do
    {goal, _item, task} = admitted_task_fixture()

    assert {:ok, [%{disposition: :noop}]} =
             Goals.reconcile_due_goals(goal_ids: [goal.id], now: @now)

    Repo.delete_all(
      from(job in Oban.Job,
        where:
          job.worker == "SymmetryControl.Goals.Workers.WakeupWorker" and
            fragment("? ->> 'goal_id' = ?", job.args, ^goal.id)
      )
    )

    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    assert {:ok, first} = Goals.settle_task(task.id, run.id, 1, now: @now)
    assert wakeup_job_count(goal.id) == 1

    assert {:ok, ^first} = Goals.settle_task(task.id, run.id, 1, now: @now)
    assert wakeup_job_count(goal.id) == 1
  end

  test "the global wakeup recovers a terminal settlement for a paused Goal exactly once" do
    {goal, _item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)
    previous_goals = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(previous_goals, :rollout_enabled, true)
    )

    on_exit(fn -> Application.put_env(:symmetry_control, :goals, previous_goals) end)

    assert {:ok, _paused, :created} =
             command_current(goal.id, "pause", %{reason: "Pause before recovery."})

    assert :ok = WakeupWorker.perform(%Oban.Job{args: %{}})

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "unknown"

    assert Repo.aggregate(
             from(event in SymmetryControl.Goals.GoalEvent,
               where:
                 event.goal_id == ^goal.id and event.kind == "task_settled" and
                   event.payload["run_id"] == ^run.id
             ),
             :count
           ) == 1

    assert :ok = WakeupWorker.perform(%Oban.Job{args: %{}})

    assert Repo.aggregate(
             from(event in SymmetryControl.Goals.GoalEvent,
               where:
                 event.goal_id == ^goal.id and event.kind == "task_settled" and
                   event.payload["run_id"] == ^run.id
             ),
             :count
           ) == 1
  end

  test "terminal settlement recovery respects the lease-expiry grace boundary" do
    {_goal, _item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "failed"])

    Repo.update_all(
      from(row in Run, where: row.id == ^run.id),
      set: [state: "failed", failure: %{"reason" => "lease_expired"}, updated_at: @now]
    )

    assert {:ok, []} = Goals.recover_pending_terminal_settlements(now: @now)

    assert {:ok, [%{task_id: task_id, run_id: run_id, generation: 1}]} =
             Goals.recover_pending_terminal_settlements(now: DateTime.add(@now, 8 * 60, :second))

    assert task_id == task.id
    assert run_id == run.id
  end

  test "Goal control retry recovers a committed runless cancellation before settlement" do
    {goal, _item, task} = admitted_task_fixture()
    previous_goals = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(previous_goals, :rollout_enabled, true)
    )

    on_exit(fn -> Application.put_env(:symmetry_control, :goals, previous_goals) end)

    assert {:ok, cancelled, :created} = command_current(goal.id, "cancel", %{reason: "stop"})
    action_id = cancelled.response["control_action_id"]
    idempotency_key = "goal-control:#{action_id}:cancel:#{task.id}"

    assert {:ok, command, :created} =
             Orchestration.create_goal_control_command(
               goal.id,
               goal.current_revision,
               action_id,
               task.id,
               "cancel",
               %{},
               idempotency_key,
               now: @now
             )

    assert command.state == "applied"
    assert command.run_id == nil

    assert Repo.one!(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.task_id == ^task.id
             )
           ).state == "held"

    job = %Oban.Job{
      args: %{"goal_id" => goal.id, "revision" => goal.current_revision, "action_id" => action_id}
    }

    assert :ok = GoalControlWorker.perform(job)

    assert Repo.one!(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.task_id == ^task.id
             )
           ).state == "released"

    assert unstarted_settlement_event_count(goal.id, task.id) == 1

    assert 1 ==
             Repo.aggregate(
               from(command in SymmetryControl.Orchestration.Command,
                 where: command.task_id == ^task.id
               ),
               :count
             )

    refute Repo.exists?(from(run in Run, where: run.task_id == ^task.id))

    refute Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.producing_task_id == ^task.id
             )
           )

    assert :ok = GoalControlWorker.perform(job)
    assert unstarted_settlement_event_count(goal.id, task.id) == 1
  end

  test "Goal resume waits for an old amendment cancellation to settle" do
    {goal, _item, task} = admitted_task_fixture()
    previous_goals = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(previous_goals, :rollout_enabled, true)
    )

    on_exit(fn -> Application.put_env(:symmetry_control, :goals, previous_goals) end)

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract: amended_revision_contract("Cancel the old revision safely."),
               reason: "Scope changed"
             })

    action_id = amended.response["control_action_id"]
    revision = amended.goal.current_revision
    idempotency_key = "goal-control:#{action_id}:cancel:#{task.id}"

    # The old revision is still queued until the durable cancellation lands.
    assert {:error, :state_conflict} = command_current(goal.id, "resume", %{reason: "continue"})

    assert {:ok, command, :created} =
             Orchestration.create_goal_control_command(
               goal.id,
               revision,
               action_id,
               task.id,
               "cancel",
               %{},
               idempotency_key,
               now: @now
             )

    assert command.state == "applied"
    assert command.run_id == nil
    # Cancellation is durable, but the held reservation still has to settle.
    assert {:error, :state_conflict} = command_current(goal.id, "resume", %{reason: "continue"})

    job = %Oban.Job{
      args: %{"goal_id" => goal.id, "revision" => revision, "action_id" => action_id}
    }

    assert :ok = GoalControlWorker.perform(job)

    assert Repo.one!(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.task_id == ^task.id
             )
           ).state == "released"

    assert unstarted_settlement_event_count(goal.id, task.id) == 1

    assert {:ok, resumed, :created} = command_current(goal.id, "resume", %{reason: "continue"})
    assert resumed.goal.state == "active"
  end

  test "later Goal control work recovers a historical runless cancellation" do
    {goal, _item, task} = admitted_task_fixture()
    previous_goals = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(previous_goals, :rollout_enabled, true)
    )

    on_exit(fn -> Application.put_env(:symmetry_control, :goals, previous_goals) end)

    assert {:ok, first_amendment, :created} =
             command_current(goal.id, "amend", %{
               revision_contract: amended_revision_contract("First amendment."),
               reason: "First scope change"
             })

    first_action_id = first_amendment.response["control_action_id"]

    assert {:ok, _command, :created} =
             Orchestration.create_goal_control_command(
               goal.id,
               first_amendment.goal.current_revision,
               first_action_id,
               task.id,
               "cancel",
               %{},
               "goal-control:#{first_action_id}:cancel:#{task.id}",
               now: @now
             )

    assert Repo.one!(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.task_id == ^task.id
             )
           ).state == "held"

    assert {:ok, second_amendment, :created} =
             command_current(goal.id, "amend", %{
               revision_contract: amended_revision_contract("Second amendment."),
               reason: "Second scope change"
             })

    job = %Oban.Job{
      args: %{
        "goal_id" => goal.id,
        "revision" => second_amendment.goal.current_revision,
        "action_id" => second_amendment.response["control_action_id"]
      }
    }

    assert :ok = GoalControlWorker.perform(job)

    assert Repo.one!(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.task_id == ^task.id
             )
           ).state == "released"

    assert unstarted_settlement_event_count(goal.id, task.id) == 1

    assert 1 ==
             Repo.aggregate(
               from(command in SymmetryControl.Orchestration.Command,
                 where: command.task_id == ^task.id
               ),
               :count
             )

    refute Repo.exists?(from(run in Run, where: run.task_id == ^task.id))

    refute Repo.exists?(
             from(outcome in SymmetryControl.Goals.WorkOutcome,
               where: outcome.producing_task_id == ^task.id
             )
           )

    assert :ok = GoalControlWorker.perform(job)
    assert unstarted_settlement_event_count(goal.id, task.id) == 1
  end

  test "admission defaults off and terminal usage gaps remain unknown without a caller reservation" do
    {goal, item, task} = admitted_task_fixture()

    assert {:error, :goal_admission_disabled} =
             command_current(goal.id, "admit_task", admission_payload(item))

    Repo.update_all(from(row in Task, where: row.id == ^task.id),
      set: [state: "completed", current_generation: 1, updated_at: @now]
    )

    runtime = runtime_fixture()

    run =
      %Run{}
      |> Run.changeset(%{
        task_id: task.id,
        runtime_id: runtime.id,
        generation: 1,
        state: "completed",
        assigned_at: @now,
        assignment_expires_at: DateTime.add(@now, 60, :second)
      })
      |> Repo.insert!()

    assert {:ok, %{"settlement" => "missing_result"}} =
             Goals.settle_task(task.id, run.id, 1, now: @now)

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "unknown"

    assert {:ok, admitted, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item),
               rollout_enabled: true
             )

    assert Repo.one!(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.task_id == ^admitted.response["task"]["id"],
               select: reservation.reserved_microusd
             )
           ) == nil
  end

  test "disabled rollout settles existing terminal work but rejects a new admission" do
    {goal, item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)
    previous_goals = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(previous_goals, :rollout_enabled, false)
    )

    on_exit(fn -> Application.put_env(:symmetry_control, :goals, previous_goals) end)

    assert :ok =
             SettleTaskWorker.perform(%Oban.Job{
               args: %{"task_id" => task.id, "run_id" => run.id, "generation" => run.generation}
             })

    assert Repo.exists?(
             from(event in SymmetryControl.Goals.GoalEvent,
               where:
                 event.goal_id == ^goal.id and event.kind == "task_settled" and
                   fragment("? ->> 'task_id' = ?", event.payload, ^task.id)
             )
           )

    assert {:error, :goal_admission_disabled} =
             command_current(goal.id, "admit_task", admission_payload(item))
  end

  test "due reconciliation honors a requested Goal id set" do
    {first, _first_item, _first_task} = admitted_task_fixture()
    {_second, _second_item, _second_task} = admitted_task_fixture()

    assert {:ok, [%{goal_id: goal_id}]} =
             Goals.reconcile_due_goals(goal_ids: [first.id], now: @now)

    assert goal_id == first.id
  end

  test "control recovery selects pending actions after more than one page of terminal no-op Goals" do
    project = project_fixture()

    for _ <- 1..101 do
      assert {:ok, created, :created} =
               Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

      assert {:ok, _cancelled, :created} =
               command_current(created.goal.id, "cancel", %{reason: "No work requires control."})
    end

    {goal, _item, _task} = admitted_task_fixture()

    assert {:ok, cancelled, :created} =
             command_current(goal.id, "cancel", %{reason: "This action requires recovery."})

    assert {:ok, [%{goal_id: goal_id, revision: revision, action_id: action_id}]} =
             Goals.recover_pending_goal_control_actions(limit: 100)

    assert goal_id == goal.id
    assert revision == cancelled.goal.current_revision
    assert action_id == cancelled.response["control_action_id"]
  end

  test "automatic execution disabled clears a due wake without producing a perpetual audit loop" do
    {goal, _item, _task} = admitted_task_fixture()

    assert {:ok, [%{goal_id: goal_id, disposition: :noop}]} =
             Goals.reconcile_due_goals(goal_ids: [goal.id], now: @now)

    assert goal_id == goal.id
    assert {:ok, refreshed} = Goals.fetch_goal(goal.id)
    assert refreshed.next_wake_at == nil
    assert {:ok, []} = Goals.reconcile_due_goals(goal_ids: [goal.id], now: @now)
  end

  test "automatic reconciliation defers a disabled rollout instead of clearing its recovery wake" do
    policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "budget_limit_microusd" => 1_000_000,
      "allowed_actions" => ["implement"]
    }

    {goal, [_item]} = goal_with_items_fixture(policy)
    set_goal_due!(goal.id)

    assert {:ok,
            [
              %{
                disposition: :deferred,
                reason: "goal_admission_disabled",
                next_wake_at: next_wake_at
              }
            ]} = Goals.reconcile_due_goals(goal_ids: [goal.id], now: @now, rollout_enabled: false)

    assert next_wake_at == DateTime.add(@now, 60, :second)
    assert Repo.get!(SymmetryControl.Goals.Goal, goal.id).next_wake_at == next_wake_at
    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 0
  end

  test "automatic reconciliation records a durable admission limit blocker for unaccepted work" do
    policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 2,
      "max_task_admissions" => 1,
      "budget_limit_microusd" => 1_000_000,
      "allowed_actions" => ["implement"]
    }

    {goal, [first_item, _second_item]} = goal_with_items_fixture(policy, 2)

    assert {:ok, _admitted, :created} =
             command_current(goal.id, "admit_task", admission_payload(first_item),
               rollout_enabled: true
             )

    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :blocked, reason: "admission_limit_reached"}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    event =
      Repo.one!(
        from(event in SymmetryControl.Goals.GoalEvent,
          where:
            event.goal_id == ^goal.id and event.kind == "automatic_admission_blocked" and
              fragment("? ->> 'reason' = 'admission_limit_reached'", event.payload)
        )
      )

    assert event.payload["candidate_identity"] == "goal:admission_limit_reached"
    assert Repo.get!(SymmetryControl.Goals.Goal, goal.id).next_wake_at == nil

    assert {:ok, projection} = Goals.fetch_goal(goal.id)
    assert "automatic_admission_blocked" in projection.blocker_reasons
  end

  test "automatic execution admits one independent validation Task from a settled candidate" do
    producer_runtime = runtime_fixture()

    {goal, item, producer} =
      admitted_task_fixture("primary", check_contract(), %{
        execution_policy: %{
          "automatic_execution" => true,
          "max_parallel_tasks" => 1,
          "budget_limit_microusd" => 1_000_000,
          "allowed_actions" => ["implement", "validate"]
        }
      })

    Repo.update_all(
      from(runtime in Runtime, where: runtime.id == ^producer_runtime.id),
      set: [
        repository_resource_id: item_repository_resource_id(item),
        capabilities: %{"adapter" => %{"operations" => %{"start" => true}}}
      ]
    )

    candidate_subject = %{
      "resource_id" => item_repository_resource_id(item),
      "commit" => String.duplicate("c", 40),
      "tree_digest" => "sha256:" <> String.duplicate("d", 64)
    }

    {producer_run, _fence} =
      completed_goal_run_fixture(producer, producer_runtime, candidate_subject)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(producer.id, producer_run.id, 1, now: @now)

    assert {:ok,
            [
              %{
                goal_id: goal_id,
                disposition: :admitted,
                task_id: validation_task_id,
                work_item_id: work_item_id,
                purpose: "validate"
              }
            ]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert goal_id == goal.id
    assert work_item_id == item.id

    validation = Repo.get!(Task, validation_task_id)
    assert validation.validation_of_task_id == producer.id
    assert validation.input["subject"] == candidate_subject

    assert {:ok, refreshed} = Goals.fetch_goal(goal.id)
    assert refreshed.next_wake_at == DateTime.add(@now, 60, :second)

    Repo.update_all(from(row in SymmetryControl.Goals.Goal, where: row.id == ^goal.id),
      set: [next_wake_at: @now]
    )

    assert {:ok, [%{goal_id: ^goal_id, disposition: :noop}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Repo.aggregate(
             from(task in Task, where: task.goal_id == ^goal.id and task.purpose == "validate"),
             :count
           ) == 1
  end

  test "automatic progress continuation binds the current terminal result Subject and result identity" do
    {goal, item, task} =
      admitted_task_fixture("primary", check_contract(), %{
        execution_policy: %{
          "automatic_execution" => true,
          "max_task_admissions" => 2,
          "budget_limit_microusd" => 1_000_000,
          "allowed_actions" => ["implement"]
        }
      })

    runtime = runtime_fixture()

    progressed_subject = %{
      "resource_id" => item_repository_resource_id(item),
      "commit" => String.duplicate("c", 40),
      "tree_digest" => "sha256:" <> String.duplicate("d", 64)
    }

    {run, _fence} = completed_goal_run_fixture(task, runtime, progressed_subject)
    progress_result = task_result(task, "progress", %{"subject" => progressed_subject})
    put_task_result!(run, progress_result)

    assert {:ok, %{"settlement" => "progress"}} =
             Goals.settle_task(task.id, run.id, 1, now: @now)

    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :admitted, task_id: continuation_task_id, purpose: "implement"}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    continuation = Repo.get!(Task, continuation_task_id)
    assert continuation.id != task.id
    assert continuation.input["subject"] == progressed_subject

    event =
      Repo.one!(
        from(event in SymmetryControl.Goals.GoalEvent,
          where: event.goal_id == ^goal.id and event.kind == "automatic_task_admitted",
          order_by: [desc: event.sequence],
          limit: 1
        )
      )

    assert event.payload["source_task_id"] == task.id
    assert event.payload["source_result_id"] == progress_result["result_id"]
    assert event.response["source_identity"] =~ progress_result["result_id"]
  end

  test "automatic execution queues an unstarted root until a matching runtime arrives" do
    project = project_fixture()
    repository = repository_fixture(project)

    automatic_policy = %{
      "automatic_execution" => true,
      "allowed_actions" => ["implement"],
      "allowed_resource_ids" => [repository.id]
    }

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(nil, automatic_policy), "operator:test",
               now: @now
             )

    subject = baseline_subject(repository.id).subject

    proposal =
      plan_proposal(created.goal.id, [
        %{
          key: "root",
          title: "Root work",
          description: "Start from the approved Subject.",
          required: true,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: baseline_subject(repository.id)
        }
      ])

    planned = accept_plan!(created.goal.id, proposal)
    [item] = planned.goal.work_items

    assert {:ok, _active, :created} =
             command_current(created.goal.id, "activate", %{approved_revision: 1})

    set_goal_due!(created.goal.id)

    assert {:ok, [%{disposition: :admitted, purpose: "implement"}]} =
             Goals.reconcile_due_goals(
               goal_ids: [created.goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    [task] = Repo.all(from(task in Task, where: task.goal_id == ^created.goal.id))
    assert task.state == "queued"
    assert task.input["subject"] == subject
    refute Repo.exists?(from(run in Run, where: run.task_id == ^task.id))

    runtime = runtime_fixture()
    configure_automatic_runtimes!(item, [runtime])

    assert {:ok, run} = Orchestration.assign_one(now: @now)
    assert run.task_id == task.id

    set_goal_due!(created.goal.id)

    assert {:ok, [%{disposition: :noop}]} =
             Goals.reconcile_due_goals(
               goal_ids: [created.goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^created.goal.id), :count) ==
             1
  end

  test "automatic reconciliation fills only the approved parallel admission capacity" do
    project = project_fixture()
    repository = repository_fixture(project)

    automatic_policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 3,
      "max_task_admissions" => 3,
      "allowed_actions" => ["implement"],
      "allowed_resource_ids" => [repository.id],
      "budget_limit_microusd" => 1_000_000
    }

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(nil, automatic_policy), "operator:test",
               now: @now
             )

    proposal =
      plan_proposal(
        created.goal.id,
        Enum.map(1..3, fn index ->
          %{
            key: "parallel-#{index}",
            title: "Parallel work #{index}",
            description: "Use the approved immutable baseline.",
            required: true,
            repository_resource_id: repository.id,
            acceptance: check_contract(),
            depends_on_keys: [],
            model_profile: "codex",
            baseline: baseline_subject(repository.id)
          }
        end)
      )

    _planned = accept_plan!(created.goal.id, proposal)

    assert {:ok, _active, :created} =
             command_current(created.goal.id, "activate", %{approved_revision: 1})

    set_goal_due!(created.goal.id)

    assert {:ok, admissions} =
             Goals.reconcile_due_goals(
               goal_ids: [created.goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Enum.count(admissions, &(&1.disposition == :admitted)) == 3
    assert Enum.all?(admissions, &(&1.purpose == "implement"))

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^created.goal.id), :count) ==
             3

    assert Repo.aggregate(
             from(task in Task,
               where: task.goal_id == ^created.goal.id and task.state == "queued"
             ),
             :count
           ) == 3

    set_goal_due!(created.goal.id)

    assert {:ok, [%{disposition: :noop}]} =
             Goals.reconcile_due_goals(
               goal_ids: [created.goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^created.goal.id), :count) ==
             3
  end

  test "automatic reconciliation records a business admission blocker, skips its candidate, and admits another" do
    policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 2,
      "max_task_admissions" => 2,
      "budget_limit_microusd" => 1_000_000,
      "allowed_actions" => ["implement"]
    }

    {goal, items} = goal_with_items_fixture(policy, 2)
    [blocked_item | remaining_items] = Enum.sort_by(items, & &1.id)
    [admitted_item] = remaining_items

    Repo.update_all(
      from(item in WorkItem, where: item.id == ^blocked_item.id),
      set: [agent_profile: "unapproved"]
    )

    set_goal_due!(goal.id)

    assert {:ok, reconciliations} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Enum.any?(reconciliations, fn reconciliation ->
             reconciliation.disposition == :blocked and
               reconciliation.work_item_id == blocked_item.id and
               reconciliation.purpose == "implement" and
               reconciliation.reason == "model_not_allowed"
           end)

    assert Enum.any?(reconciliations, fn reconciliation ->
             reconciliation.disposition == :admitted and
               reconciliation.work_item_id == admitted_item.id and
               reconciliation.purpose == "implement"
           end)

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 1

    assert Repo.aggregate(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.goal_id == ^goal.id
             ),
             :count
           ) == 1

    assert Repo.aggregate(
             from(snapshot in SymmetryControl.Goals.ContextSnapshot,
               where: snapshot.goal_id == ^goal.id
             ),
             :count
           ) == 1

    blocked_event =
      Repo.one!(
        from(event in SymmetryControl.Goals.GoalEvent,
          where: event.goal_id == ^goal.id and event.kind == "automatic_admission_blocked"
        )
      )

    assert blocked_event.revision == goal.current_revision
    assert blocked_event.payload["work_item_id"] == blocked_item.id
    assert blocked_event.payload["reason"] == "model_not_allowed"

    assert blocked_event.response["candidate_identity"] ==
             blocked_event.payload["candidate_identity"]

    assert {:ok, projection} = Goals.fetch_goal(goal.id)

    blocker = Enum.find(projection.blockers, &(&1.reason == "automatic_admission_blocked"))

    assert blocker.details == [
             %{
               event_id: blocked_event.id,
               work_item_id: blocked_item.id,
               purpose: "implement",
               candidate_identity: blocked_event.payload["candidate_identity"],
               source_identity: blocked_event.payload["source_identity"],
               reason: "model_not_allowed"
             }
           ]

    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :noop}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 1

    assert Repo.aggregate(
             from(event in SymmetryControl.Goals.GoalEvent,
               where: event.goal_id == ^goal.id and event.kind == "automatic_admission_blocked"
             ),
             :count
           ) == 1

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract: amended_revision_contract("Supersede the blocked candidate."),
               reason: "Revise the automatic admission policy."
             })

    assert amended.goal.current_revision == 2
    assert {:ok, amended_projection} = Goals.fetch_goal(goal.id)
    refute "automatic_admission_blocked" in amended_projection.blocker_reasons
  end

  test "automatic context admission rejection commits only its blocker receipt" do
    {goal, item, nil} =
      admitted_task_fixture(
        "primary",
        check_contract(),
        %{
          execution_policy: %{
            "automatic_execution" => true,
            "max_task_admissions" => 1,
            "max_parallel_tasks" => 1,
            "budget_limit_microusd" => 1_000_000,
            "allowed_actions" => ["implement"]
          },
          context_manifest: %{
            "byte_budget" => 1,
            "required_source_kinds" => ["repository"],
            "include_advisory_recall" => false
          }
        },
        false
      )

    set_goal_due!(goal.id)

    assert {:ok,
            [
              %{
                disposition: :blocked,
                work_item_id: work_item_id,
                purpose: "implement",
                reason: "context_budget_exceeded"
              }
            ]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert work_item_id == item.id
    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 0

    assert Repo.aggregate(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.goal_id == ^goal.id
             ),
             :count
           ) == 0

    assert Repo.aggregate(
             from(snapshot in SymmetryControl.Goals.ContextSnapshot,
               where: snapshot.goal_id == ^goal.id
             ),
             :count
           ) == 0

    assert Repo.aggregate(
             from(usage in SymmetryControl.Goals.RunUsage,
               join: run in Run,
               on: run.id == usage.run_id,
               join: task in Task,
               on: task.id == run.task_id,
               where: task.goal_id == ^goal.id
             ),
             :count
           ) == 0

    assert Repo.aggregate(
             from(event in SymmetryControl.Goals.GoalEvent,
               where: event.goal_id == ^goal.id and event.kind == "automatic_admission_blocked"
             ),
             :count
           ) == 1

    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :noop}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )
  end

  test "automatic reconciliation defers a mutable validation profile configuration and retries after repair" do
    policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "budget_limit_microusd" => 1_000_000,
      "allowed_actions" => ["implement"]
    }

    {goal, [item]} = goal_with_items_fixture(policy)
    set_goal_due!(goal.id)

    assert {:ok,
            [
              %{
                disposition: :deferred,
                work_item_id: work_item_id,
                purpose: "implement",
                reason: "invalid_validation_profile",
                next_wake_at: next_wake_at
              }
            ]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: []
             )

    assert work_item_id == item.id
    assert next_wake_at == DateTime.add(@now, 60, :second)
    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 0

    assert Repo.aggregate(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.goal_id == ^goal.id
             ),
             :count
           ) == 0

    assert Repo.aggregate(
             from(snapshot in SymmetryControl.Goals.ContextSnapshot,
               where: snapshot.goal_id == ^goal.id
             ),
             :count
           ) == 0

    refute Repo.exists?(
             from(event in SymmetryControl.Goals.GoalEvent,
               where: event.goal_id == ^goal.id and event.kind == "automatic_admission_blocked"
             )
           )

    assert {:ok, [%{disposition: :admitted, work_item_id: ^work_item_id}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: next_wake_at,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )
  end

  test "automatic deferred receipts distinguish changing reasons for one candidate" do
    policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "budget_limit_microusd" => 1_000_000,
      "allowed_actions" => ["implement"],
      "allowed_runtime_ids" => [Ecto.UUID.generate()]
    }

    {goal, [_item]} = goal_with_items_fixture(policy)
    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :deferred, reason: "validation_runtime_not_allowed"}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :deferred, reason: "invalid_validation_profile"}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: []
             )

    deferred_events =
      Repo.all(
        from(event in SymmetryControl.Goals.GoalEvent,
          where: event.goal_id == ^goal.id and event.kind == "automatic_reconciliation_deferred",
          order_by: [asc: event.sequence]
        )
      )

    assert Enum.map(deferred_events, & &1.response["reason"]) == [
             "validation_runtime_not_allowed",
             "invalid_validation_profile"
           ]
  end

  test "automatic reconciliation quiesces after an unattended terminal failure" do
    {goal, item, task} =
      admitted_task_fixture(
        "primary",
        check_contract(),
        %{
          execution_policy: %{
            "automatic_execution" => true,
            "max_task_admissions" => 2,
            "max_parallel_tasks" => 1,
            "budget_limit_microusd" => 1_000_000,
            "allowed_actions" => ["implement"]
          }
        }
      )

    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "failed"])

    Repo.update_all(
      from(row in Run, where: row.id == ^run.id),
      set: [state: "failed", result: nil, failure: %{"reason" => "network"}]
    )

    assert {:ok, %{"settlement" => "failed"}} =
             Goals.settle_task(task.id, run.id, run.generation, now: @now)

    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :noop}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Repo.get!(SymmetryControl.Goals.Goal, goal.id).next_wake_at == nil
    assert {:ok, projection} = Goals.fetch_goal(goal.id)
    assert projection.settlement_attention != []
    assert item.id == hd(projection.settlement_attention).work_item_id
  end

  test "automatic read model reports a missing explicit baseline blocker" do
    projection =
      ReadModel.project(
        %{id: "goal-baseline", state: "active", current_revision: 1},
        %{
          revisions: [
            %{
              revision: 1,
              execution_policy: %{"automatic_execution" => true}
            }
          ],
          work_items: [
            %{
              id: "unstarted-item",
              required: true,
              admitted_revision: 1,
              repository_resource_id: "repository-1"
            }
          ],
          dependencies: [],
          outcomes: [],
          decisions: [],
          tasks: [],
          runs: [],
          runtimes: []
        }
      )

    assert [%{reason: "automatic_baseline_missing"}] =
             Enum.filter(projection.blockers, &(&1.reason == "automatic_baseline_missing"))
  end

  test "automatic execution derives one dependency baseline from its accepted current outcome" do
    project = project_fixture()
    repository = repository_fixture(project)
    source_runtime = runtime_fixture()
    validation_runtime = runtime_fixture(@validation_runtime_id)

    automatic_policy = %{
      "automatic_execution" => true,
      "max_task_admissions" => 6,
      "allowed_runtime_ids" => [source_runtime.id, validation_runtime.id],
      "allowed_actions" => ["implement", "validate"],
      "allowed_resource_ids" => [repository.id],
      "budget_limit_microusd" => 1_000_000
    }

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(nil, automatic_policy), "operator:test",
               now: @now
             )

    proposal =
      plan_proposal(created.goal.id, [
        %{
          key: "source",
          title: "Source work",
          description: "Produce the dependency Subject.",
          required: true,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: baseline_subject(repository.id)
        },
        %{
          key: "dependent",
          title: "Dependent work",
          description: "Use only the accepted source Subject.",
          required: true,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: ["source"],
          model_profile: "codex",
          baseline: %{kind: "dependency", key: "source"}
        }
      ])

    planned = accept_plan!(created.goal.id, proposal)
    source_item = Enum.find(planned.goal.work_items, &(&1.title == "Source work"))
    dependent_item = Enum.find(planned.goal.work_items, &(&1.title == "Dependent work"))

    candidate_subject = %{
      "resource_id" => repository.id,
      "commit" => String.duplicate("c", 40),
      "tree_digest" => "sha256:" <> String.duplicate("d", 64)
    }

    configure_automatic_runtimes!(dependent_item, [source_runtime, validation_runtime])

    assert {:ok, _active, :created} =
             command_current(created.goal.id, "activate", %{approved_revision: 1})

    assert {:ok, producer_admission, :created} =
             command_current(
               created.goal.id,
               "admit_task",
               admission_payload(source_item, %{reserved_microusd: 0}),
               rollout_enabled: true
             )

    producer = Repo.get!(Task, producer_admission.response["task"]["id"])

    {producer_run, _producer_fence} =
      completed_goal_run_fixture(producer, source_runtime, candidate_subject)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(producer.id, producer_run.id, 1, now: @now)

    assert {:ok, validation_admission, :created} =
             command_current(
               created.goal.id,
               "admit_task",
               admission_payload(source_item, %{
                 purpose: "validate",
                 validation_of_task_id: producer.id,
                 reserved_microusd: 0
               })
               |> Map.delete(:subject),
               rollout_enabled: true
             )

    validation = Repo.get!(Task, validation_admission.response["task"]["id"])

    {validation_run, validation_fence} =
      completed_goal_run_fixture(validation, validation_runtime, candidate_subject)

    assert {:ok, %{evidence: _stored}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence_attrs(validation_run.id, source_item, candidate_subject),
               now: @now
             )

    assert {:ok, %{"settlement" => "accepted"}} =
             Goals.settle_task(validation.id, validation_run.id, 1, now: @now)

    set_goal_due!(created.goal.id)

    assert {:ok, [%{disposition: :admitted, work_item_id: dependent_id}]} =
             Goals.reconcile_due_goals(
               goal_ids: [created.goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert dependent_id == dependent_item.id
    dependent_task = Repo.get_by!(Task, goal_id: created.goal.id, work_item_id: dependent_item.id)
    assert dependent_task.input["subject"] == candidate_subject

    set_goal_due!(created.goal.id)

    assert {:ok, [%{disposition: :deferred, reason: "admission_capacity_unavailable"}]} =
             Goals.reconcile_due_goals(
               goal_ids: [created.goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Repo.aggregate(
             from(task in Task,
               where: task.goal_id == ^created.goal.id and task.work_item_id == ^dependent_item.id
             ),
             :count
           ) == 1
  end

  test "advisory recall copies only bounded immutable same-resource snapshot pointers" do
    {goal, item, task} =
      admitted_task_fixture("primary", check_contract(), %{
        context_manifest: %{
          "byte_budget" => 32_768,
          "required_source_kinds" => ["repository"],
          "include_advisory_recall" => true
        }
      })

    previous_snapshot = Repo.get!(SymmetryControl.Goals.ContextSnapshot, task.context_snapshot_id)
    complete_task_and_release_reservation!(goal, task)

    assert {:ok, admission, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item),
               rollout_enabled: true
             )

    snapshot =
      Repo.get!(
        SymmetryControl.Goals.ContextSnapshot,
        Repo.get!(Task, admission.response["task"]["id"]).context_snapshot_id
      )

    assert [recall | _] = snapshot.payload["advisory_recall"]

    assert recall["content"] == %{
             "kind" => "pointer",
             "value" => "context_snapshot:" <> previous_snapshot.id
           }

    assert recall["source_revision"] == "snapshot:" <> previous_snapshot.id

    assert recall["content_hash"] ==
             "sha256:" <> Base.encode16(previous_snapshot.content_hash, case: :lower)

    assert recall["trust"] == "advisory"
    assert recall["required"] == false
    assert recall["stale"] == false
    assert snapshot.payload["size"]["optional_bytes"] > 0
    assert snapshot.payload["size"]["total_bytes"] <= snapshot.payload["size"]["byte_budget"]
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

    assert {:error, :ownership_lost} =
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

    assert {:error, :ownership_lost} =
             Goals.attach_harness_session(
               runtime.machine_id,
               run.id,
               fence,
               %{attrs | adapter_version: "2.0.0"},
               now: @now
             )
  end

  test "a requested available session is claimed, then settlement quarantines its terminal run" do
    {_goal, item, task, runtime, run, fence} = claimed_goal_run_fixture()
    attrs = session_attrs(item, "workspace-requested")
    requested = available_session_fixture(runtime, item, attrs)

    unrelated =
      available_session_fixture(runtime, item, session_attrs(item, "workspace-unrelated"))

    Repo.update_all(
      from(row in Task, where: row.id == ^task.id),
      set: [requested_session_id: requested.id]
    )

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

  test "resume without its requested session never creates a fresh session" do
    {_goal, item, task, runtime, run, fence} = claimed_goal_run_fixture()

    Repo.update_all(
      from(row in Task, where: row.id == ^task.id),
      set: [input: Map.put(task.input, "session_mode", "resume"), requested_session_id: nil]
    )

    assert {:error, :requested_session_not_found} =
             Goals.attach_harness_session(
               runtime.machine_id,
               run.id,
               fence,
               session_attrs(item, "workspace-resume"),
               now: @now
             )

    assert Repo.aggregate(from(session in HarnessSession), :count) == 0
  end

  test "resume requires a retained session and unsupported handoff creates no admission records" do
    {goal, item, task} = admitted_task_fixture()
    complete_task_and_release_reservation!(goal, task)

    assert {:error, :requested_session_required} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{session_mode: "resume", reserved_microusd: 1}),
               rollout_enabled: true
             )

    delete_goal_wakeup_jobs(goal.id)

    counts_before = %{
      snapshots: Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count),
      tasks: Repo.aggregate(Task, :count),
      reservations: Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count),
      events: Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count),
      wakeups: wakeup_job_count(goal.id),
      goal: Repo.get!(SymmetryControl.Goals.Goal, goal.id)
    }

    assert {:error, :unsupported_capability} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{session_mode: "handoff", reserved_microusd: 1}),
               rollout_enabled: true
             )

    assert counts_before.snapshots ==
             Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count)

    assert counts_before.tasks == Repo.aggregate(Task, :count)

    assert counts_before.reservations ==
             Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count)

    assert counts_before.events == Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count)
    assert counts_before.wakeups == wakeup_job_count(goal.id)

    previous_next_wake_at = counts_before.goal.next_wake_at
    previous_lock_version = counts_before.goal.lock_version

    assert %{next_wake_at: ^previous_next_wake_at, lock_version: ^previous_lock_version} =
             Repo.get!(SymmetryControl.Goals.Goal, goal.id)
  end

  test "resume admission rejects an unavailable retained session before writing admission records" do
    {goal, item, task} = admitted_task_fixture()
    complete_task_and_release_reservation!(goal, task)
    runtime = runtime_fixture()
    retained = available_session_fixture(runtime, item, session_attrs(item, "workspace-retained"))

    counts_before = %{
      snapshots: Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count),
      tasks: Repo.aggregate(Task, :count),
      reservations: Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count),
      events: Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count)
    }

    assert {:error, :requested_session_unavailable} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 session_mode: "resume",
                 requested_session_id: retained.id,
                 admission_key: Ecto.UUID.generate(),
                 reserved_microusd: 1
               }),
               rollout_enabled: true
             )

    assert counts_before ==
             %{
               snapshots: Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count),
               tasks: Repo.aggregate(Task, :count),
               reservations: Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count),
               events: Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count)
             }
  end

  test "admission freezes validation bindings and evidence ignores later registry changes" do
    {_goal, item, task, runtime, run, fence} = validation_goal_run_fixture()
    snapshot = Repo.get!(SymmetryControl.Goals.ContextSnapshot, task.context_snapshot_id)

    assert snapshot.payload["work_contract"]["validation_bindings"] == [
             %{
               "profile_name" => "test",
               "kind" => "check",
               "profile_digest" => @validation_profile_digest,
               "allowed_runtime_ids" => [@validation_runtime_id]
             }
           ]

    changed_profiles = [
      test: [
        kind: :check,
        profile_digest: "sha256:" <> String.duplicate("f", 64),
        enabled: false,
        allowed_runtime_ids: [Ecto.UUID.generate()]
      ]
    ]

    evidence = evidence_attrs(run.id, item)
    previous_goals = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(previous_goals, :validation_profiles, changed_profiles)
    )

    try do
      assert {:ok, %{evidence: stored}, :created} =
               Goals.append_evidence(runtime.machine_id, run.id, fence, evidence, now: @now)

      assert stored.id == evidence.evidence_id
    after
      Application.put_env(:symmetry_control, :goals, previous_goals)
    end

    assert Repo.get!(SymmetryControl.Goals.ContextSnapshot, snapshot.id).payload ==
             snapshot.payload
  end

  test "check evidence rejects profile spoofing, digest mismatch, and unbound runtimes" do
    {_goal, item, _task, runtime, run, fence} = validation_goal_run_fixture()
    evidence = evidence_attrs(run.id, item)

    assert {:error, :invalid_evidence_identity} =
             Goals.append_evidence(
               runtime.machine_id,
               run.id,
               fence,
               evidence
               |> Map.put(:validator_profile, "spoofed")
               |> put_in([:source_ref, :validator_profile], "spoofed"),
               now: @now
             )

    assert {:error, :invalid_evidence_identity} =
             Goals.append_evidence(
               runtime.machine_id,
               run.id,
               fence,
               evidence
               |> Map.put(:evidence_id, Ecto.UUID.generate())
               |> Map.put(:evidence_key, "check:wrong-digest")
               |> put_in([:payload, :profile_digest], "sha256:" <> String.duplicate("f", 64)),
               now: @now
             )

    outsider_runtime_id = Ecto.UUID.generate()

    {_other_goal, other_item, _other_task, outsider, outsider_run, outsider_fence} =
      validation_goal_run_fixture(check_contract(), outsider_runtime_id)

    assert {:error, :invalid_evidence_identity} =
             Goals.append_evidence(
               outsider.machine_id,
               outsider_run.id,
               outsider_fence,
               evidence_attrs(outsider_run.id, other_item),
               now: @now
             )
  end

  test "review evidence requires the snapshot-bound profile digest and review task identity" do
    {_goal, item, task, runtime, run, fence} = validation_goal_run_fixture(review_contract())
    evidence = review_evidence_attrs(run.id, item, task.id)

    assert {:ok, %{evidence: stored}, :created} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, evidence, now: @now)

    assert stored.id == evidence.evidence_id

    assert {:error, :invalid_evidence_identity} =
             Goals.append_evidence(
               runtime.machine_id,
               run.id,
               fence,
               evidence
               |> Map.put(:evidence_id, Ecto.UUID.generate())
               |> Map.put(:evidence_key, "review:wrong-digest")
               |> Map.put(:source_revision, "sha256:" <> String.duplicate("f", 64)),
               now: @now
             )
  end

  test "admission rejects a validation profile with no runtime allowed by the revision policy" do
    policy_runtime_id = Ecto.UUID.generate()
    validation_runtime_id = Ecto.UUID.generate()

    {goal, item, producer} =
      admitted_task_fixture(
        "primary",
        check_contract(),
        %{execution_policy: %{"allowed_runtime_ids" => [policy_runtime_id]}},
        true,
        validation_profiles([policy_runtime_id], [policy_runtime_id])
      )

    complete_task_and_release_reservation!(goal, producer)
    producer_runtime = runtime_fixture()
    {_producer_run, _producer_fence} = completed_goal_run_fixture(producer, producer_runtime)

    snapshot_count = Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count)

    assert {:error, :validation_runtime_not_allowed} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 purpose: "validate",
                 validation_of_task_id: producer.id,
                 admission_key: Ecto.UUID.generate(),
                 reserved_microusd: 0
               })
               |> Map.delete(:subject),
               rollout_enabled: true,
               validation_profiles:
                 validation_profiles([validation_runtime_id], [validation_runtime_id])
             )

    assert Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count) == snapshot_count
  end

  test "validation admission freezes the common runtime intersection for every required profile" do
    {goal, item, producer} = admitted_task_fixture("primary", check_and_review_contract())
    producer_runtime = runtime_fixture()
    {_producer_run, _producer_fence} = completed_goal_run_fixture(producer, producer_runtime)

    common_runtime_id = @validation_runtime_id
    check_only_runtime_id = Ecto.UUID.generate()
    review_only_runtime_id = Ecto.UUID.generate()

    assert {:ok, admission, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 purpose: "validate",
                 validation_of_task_id: producer.id,
                 reserved_microusd: 0
               })
               |> Map.delete(:subject),
               rollout_enabled: true,
               validation_profiles:
                 validation_profiles(
                   [common_runtime_id, check_only_runtime_id],
                   [common_runtime_id, review_only_runtime_id]
                 )
             )

    validation_task = Repo.get!(Task, admission.response["task"]["id"])

    snapshot =
      Repo.get!(SymmetryControl.Goals.ContextSnapshot, validation_task.context_snapshot_id)

    assert Enum.map(
             snapshot.payload["work_contract"]["validation_bindings"],
             & &1["allowed_runtime_ids"]
           ) == [
             [common_runtime_id],
             [common_runtime_id]
           ]

    {empty_goal, empty_item, empty_producer} =
      admitted_task_fixture("primary", check_and_review_contract())

    empty_runtime = runtime_fixture()
    {_empty_run, _empty_fence} = completed_goal_run_fixture(empty_producer, empty_runtime)
    snapshot_count = Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count)

    assert {:error, :validation_runtime_not_allowed} =
             command_current(
               empty_goal.id,
               "admit_task",
               admission_payload(empty_item, %{
                 purpose: "validate",
                 validation_of_task_id: empty_producer.id,
                 reserved_microusd: 0
               })
               |> Map.delete(:subject),
               rollout_enabled: true,
               validation_profiles:
                 validation_profiles([Ecto.UUID.generate()], [Ecto.UUID.generate()])
             )

    assert Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count) == snapshot_count
  end

  test "historical evidence is immutable by key and accepts only the owning machine fence" do
    {_goal, item, _task, runtime, run, fence} = claimed_goal_run_fixture()
    evidence = observation_evidence_attrs(run.id, item)

    assert {:ok, %{evidence: stored}, :created} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, evidence, now: @now)

    assert stored.evidence_key == "observation:remote"
    assert stored.subject_hash == evidence.subject_hash

    Repo.update_all(from(row in Task, where: row.id == ^run.task_id),
      set: [current_generation: 2, attempt_generation: 2]
    )

    assert {:ok, %{evidence: ^stored}, :replayed} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, evidence, now: @now)

    assert {:error, :idempotency_conflict} =
             Goals.append_evidence(
               runtime.machine_id,
               run.id,
               fence,
               %{evidence | verdict: "failed"},
               now: @now
             )

    assert {:error, :ownership_lost} =
             Goals.append_evidence(Ecto.UUID.generate(), run.id, fence, evidence, now: @now)

    for key <- [:raw_transcript, :api_key, :private_key, :session_filename, :local_handle] do
      assert {:error, :invalid_request} =
               Goals.append_evidence(
                 runtime.machine_id,
                 run.id,
                 fence,
                 %{evidence | payload: Map.put(evidence.payload, key, "private")},
                 now: @now
               )
    end

    assert {:error, :invalid_evidence_identity} =
             Goals.append_evidence(
               runtime.machine_id,
               run.id,
               fence,
               evidence_attrs(run.id, item),
               now: @now
             )
  end

  test "a fenced historical observation can arrive after an amendment but cannot restore current authority" do
    {goal, item, _task, runtime, run, fence} = claimed_goal_run_fixture()
    evidence = observation_evidence_attrs(run.id, item)

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract: amended_revision_contract("Preserve historical evidence."),
               reason: "The original execution is now historical."
             })

    assert amended.goal.current_revision == 2

    assert {:ok, %{evidence: stored}, :created} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, evidence, now: @now)

    assert {:ok, %{evidence: ^stored}, :replayed} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, evidence, now: @now)

    assert {:error, :ownership_lost} = Goals.fetch_run_context(runtime.machine_id, run.id, fence)
    assert Repo.aggregate(SymmetryControl.Goals.WorkOutcome, :count) == 0
  end

  test "late usage remains fenced to the original machine and replays by exact usage key" do
    {goal, _item, task, runtime, run, fence} = claimed_goal_run_fixture()

    Repo.update_all(from(row in Task, where: row.id == ^task.id),
      set: [state: "completed", current_generation: 2, attempt_generation: 2]
    )

    Repo.update_all(from(row in Run, where: row.id == ^run.id), set: [state: "completed"])
    usage = usage_attrs(run.id)
    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, %{usage: stored}, :created} =
             Goals.record_usage(runtime.machine_id, run.id, fence, usage, now: @now)

    assert stored.cost_microusd == "42"
    assert Repo.get!(SymmetryControl.Goals.RunUsage, usage.usage_id).inserted_at == @now

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "settled"

    assert wakeup_job_count(goal.id) == 1
    after_created = Repo.get!(SymmetryControl.Goals.Goal, goal.id)
    created_next_wake_at = after_created.next_wake_at
    created_lock_version = after_created.lock_version

    assert {:ok, %{usage: ^stored}, :replayed} =
             Goals.record_usage(runtime.machine_id, run.id, fence, usage, now: @now)

    assert wakeup_job_count(goal.id) == 1

    assert %{next_wake_at: ^created_next_wake_at, lock_version: ^created_lock_version} =
             Repo.get!(SymmetryControl.Goals.Goal, goal.id)

    assert {:error, :idempotency_conflict} =
             Goals.record_usage(
               runtime.machine_id,
               run.id,
               fence,
               %{usage | observed_at: DateTime.to_iso8601(DateTime.add(@now, 1, :second))},
               now: @now
             )

    assert {:error, :idempotency_conflict} =
             Goals.record_usage(runtime.machine_id, run.id, fence, %{usage | cost_microusd: "43"},
               now: @now
             )

    assert {:error, :ownership_lost} =
             Goals.record_usage(Ecto.UUID.generate(), run.id, fence, usage, now: @now)

    assert {:error, :invalid_request} =
             Goals.record_usage(
               runtime.machine_id,
               run.id,
               fence,
               %{usage | usage_key: "too-large", cost_microusd: "9223372036854775808"},
               now: @now
             )

    assert {:ok, goal_projection} = Goals.fetch_goal(goal.id)
    assert goal_projection.accounting.usage_records == 1
  end

  test "usage corrections settle a terminal Task only when every effective leaf is known" do
    {goal, _item, task, runtime, run, fence} = claimed_goal_run_fixture()

    Repo.update_all(from(row in Task, where: row.id == ^task.id),
      set: [state: "completed", updated_at: @now]
    )

    Repo.update_all(from(row in Run, where: row.id == ^run.id),
      set: [state: "completed", updated_at: @now]
    )

    unknown_usage = %{usage_attrs(run.id) | cost_microusd: nil, cost_basis: "unknown"}

    assert {:ok, %{usage: _}, :created} =
             Goals.record_usage(runtime.machine_id, run.id, fence, unknown_usage, now: @now)

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "unknown"

    delete_goal_wakeup_jobs(goal.id)

    correction = %{
      usage_attrs(run.id)
      | usage_key: "turn-1-corrected",
        supersedes_id: unknown_usage.usage_id
    }

    assert {:ok, %{usage: _}, :created} =
             Goals.record_usage(runtime.machine_id, run.id, fence, correction, now: @now)

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "settled"

    assert wakeup_job_count(goal.id) == 1
    after_correction = Repo.get!(SymmetryControl.Goals.Goal, goal.id)
    correction_next_wake_at = after_correction.next_wake_at
    correction_lock_version = after_correction.lock_version

    assert {:ok, %{usage: _}, :replayed} =
             Goals.record_usage(runtime.machine_id, run.id, fence, correction, now: @now)

    assert wakeup_job_count(goal.id) == 1

    assert %{next_wake_at: ^correction_next_wake_at, lock_version: ^correction_lock_version} =
             Repo.get!(SymmetryControl.Goals.Goal, goal.id)
  end

  test "new active usage wakes a Goal even when its reservation remains held" do
    {goal, _item, task, runtime, run, fence} = claimed_goal_run_fixture()
    usage = usage_attrs(run.id)
    delete_goal_wakeup_jobs(goal.id)

    assert {:ok, %{usage: stored}, :created} =
             Goals.record_usage(runtime.machine_id, run.id, fence, usage, now: @now)

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "held"

    assert wakeup_job_count(goal.id) == 1
    after_created = Repo.get!(SymmetryControl.Goals.Goal, goal.id)
    created_next_wake_at = after_created.next_wake_at
    created_lock_version = after_created.lock_version

    assert {:ok, %{usage: ^stored}, :replayed} =
             Goals.record_usage(runtime.machine_id, run.id, fence, usage, now: @now)

    assert wakeup_job_count(goal.id) == 1

    assert %{next_wake_at: ^created_next_wake_at, lock_version: ^created_lock_version} =
             Repo.get!(SymmetryControl.Goals.Goal, goal.id)
  end

  test "known usage from an earlier Run cannot settle another terminal Run without usage" do
    {goal, item, task, runtime, first_run, first_fence} =
      claimed_goal_run_fixture(check_contract(), nil, %{
        execution_policy: %{"budget_mode" => "strict"}
      })

    Repo.update_all(from(row in Task, where: row.id == ^task.id),
      set: [state: "completed", current_generation: 2, attempt_generation: 2, updated_at: @now]
    )

    Repo.update_all(from(row in Run, where: row.id == ^first_run.id),
      set: [state: "completed", updated_at: @now]
    )

    %Run{}
    |> Run.changeset(%{
      task_id: task.id,
      runtime_id: runtime.id,
      generation: 2,
      state: "completed",
      assigned_at: @now,
      assignment_expires_at: DateTime.add(@now, 60, :second)
    })
    |> Repo.insert!()

    assert {:ok, %{usage: _}, :created} =
             Goals.record_usage(
               runtime.machine_id,
               first_run.id,
               first_fence,
               usage_attrs(first_run.id),
               now: @now
             )

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "unknown"

    assert {:error, :budget_unknown} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{reserved_microusd: 1}),
               rollout_enabled: true
             )
  end

  test "terminal unknown accounting is recovered before a missing settlement receipt" do
    {_goal, _item, task, runtime, run, fence} = claimed_goal_run_fixture()

    Repo.update_all(from(row in Task, where: row.id == ^task.id),
      set: [state: "completed", updated_at: @now]
    )

    Repo.update_all(from(row in Run, where: row.id == ^run.id),
      set: [state: "completed", updated_at: @now]
    )

    unknown_usage = %{usage_attrs(run.id) | cost_microusd: nil, cost_basis: "unknown"}

    assert {:ok, %{usage: _}, :created} =
             Goals.record_usage(runtime.machine_id, run.id, fence, unknown_usage, now: @now)

    assert {:ok, [%{task_id: task_id, run_id: run_id, generation: 1}]} =
             Goals.recover_pending_terminal_settlements(now: DateTime.add(@now, 8 * 60, :second))

    assert task_id == task.id
    assert run_id == run.id
  end

  test "terminal unknown cost does not block a soft-budget amendment resume" do
    {goal, _item, task} = admitted_task_fixture()
    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(task.id, run.id, run.generation, now: @now)

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "unknown"

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract:
                 amended_revision_contract("Resume after unknown terminal cost."),
               reason: "A terminal usage receipt is still pending."
             })

    assert {:ok, resumed, :created} =
             command_current(goal.id, "resume", %{reason: "Continue under the amended policy."})

    assert amended.goal.current_revision == 2
    assert resumed.goal.state == "active"
  end

  test "strict mode without a configured budget limit still blocks terminal unknown cost" do
    {goal, _item, task} =
      admitted_task_fixture("primary", check_contract(), %{
        execution_policy: %{
          "budget_mode" => "strict",
          "budget_limit_microusd" => nil,
          "per_run_cost_limit_microusd" => 1,
          "hard_cost_limit_required" => true
        }
      })

    runtime = runtime_fixture()
    {run, _fence} = completed_goal_run_fixture(task, runtime)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(task.id, run.id, run.generation, now: @now)

    assert {:ok, _paused, :created} =
             command_current(goal.id, "pause", %{reason: "Review unknown accounting."})

    assert {:error, :budget_unknown} =
             command_current(goal.id, "resume", %{reason: "Do not bypass strict accounting."})
  end

  test "an expired earlier Run without usage blocks strict resume while its Task retries" do
    {goal, _item, task, _runtime, run, _fence} =
      claimed_goal_run_fixture(check_contract(), nil, %{
        execution_policy: %{"budget_mode" => "strict"}
      })

    Repo.update_all(from(row in Task, where: row.id == ^task.id),
      set: [state: "queued", attempt_generation: 2, updated_at: @now]
    )

    Repo.update_all(from(row in Run, where: row.id == ^run.id),
      set: [state: "expired", updated_at: @now]
    )

    assert {:ok, _paused, :created} =
             command_current(goal.id, "pause", %{reason: "Review an expired retry."})

    assert {:error, :budget_unknown} =
             command_current(goal.id, "resume", %{reason: "Account for the expired execution."})
  end

  test "usage cannot supersede itself or make a held reservation disappear" do
    {_goal, _item, task, runtime, run, fence} = claimed_goal_run_fixture()
    usage = usage_attrs(run.id)

    assert {:error, :invalid_request} =
             Goals.record_usage(
               runtime.machine_id,
               run.id,
               fence,
               %{usage | supersedes_id: usage.usage_id},
               now: @now
             )

    refute Repo.exists?(from(row in SymmetryControl.Goals.RunUsage, where: row.run_id == ^run.id))

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "held"
  end

  test "artifact and observation evidence bind canonical receipt identity" do
    {_goal, item, _task, runtime, run, fence} = claimed_goal_run_fixture()

    artifact = artifact_evidence_attrs(run.id, item)

    assert {:error, :invalid_evidence_identity} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, artifact, now: @now)

    assert {:error, {:evidence_identity_mismatch, :artifact_path}} =
             Goals.append_evidence(
               runtime.machine_id,
               run.id,
               fence,
               put_in(artifact, [:source_ref, :path], "wrong-proof.txt"),
               now: @now
             )

    observation = observation_evidence_attrs(run.id, item)

    assert {:ok, %{evidence: %{kind: "observation"}}, :created} =
             Goals.append_evidence(runtime.machine_id, run.id, fence, observation, now: @now)
  end

  test "context manifest rejects missing sources and mandatory budget overflow before insertion" do
    {missing_goal, missing_item, _missing_task} =
      admitted_task_fixture(
        "primary",
        check_contract(),
        %{
          context_manifest: %{
            "byte_budget" => 32_768,
            "required_source_kinds" => ["unavailable"],
            "include_advisory_recall" => false
          }
        },
        false
      )

    snapshot_count = Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count)

    assert {:error, :required_context_source_missing} =
             command_current(
               missing_goal.id,
               "admit_task",
               admission_payload(missing_item, %{reserved_microusd: 1}),
               rollout_enabled: true
             )

    assert Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count) == snapshot_count

    {budget_goal, budget_item, _budget_task} =
      admitted_task_fixture(
        "primary",
        check_contract(),
        %{
          context_manifest: %{
            "byte_budget" => 1,
            "required_source_kinds" => ["repository"],
            "include_advisory_recall" => false
          }
        },
        false
      )

    snapshot_count = Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count)

    assert {:error, :context_budget_exceeded} =
             command_current(
               budget_goal.id,
               "admit_task",
               admission_payload(budget_item, %{reserved_microusd: 1}),
               rollout_enabled: true
             )

    assert Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count) == snapshot_count
  end

  test "run context requires the current owning machine and returns a sanitized snapshot" do
    {goal, item, task, runtime, run, fence} = claimed_goal_run_fixture()

    snapshot = Repo.get!(SymmetryControl.Goals.ContextSnapshot, task.context_snapshot_id)

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
        goal_revision: task.goal_revision,
        work_item_id: item.id,
        schema_version: 1,
        content_hash: content_hash,
        payload: snapshot_payload
      })
      |> Repo.insert!()

    Repo.update_all(
      from(row in Task, where: row.id == ^task.id),
      set: [
        context_snapshot_id: snapshot_id,
        input: Map.put(task.input, "context_snapshot_id", snapshot_id)
      ]
    )

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

  test "deterministic validation fetches claimed context without a retained harness session" do
    {_goal, _item, task, runtime, run, fence} = claimed_goal_run_fixture()
    producer = validation_producer_task_fixture(task)

    Repo.update_all(
      from(row in Task, where: row.id == ^task.id),
      set: [purpose: "validate", validation_of_task_id: producer.id]
    )

    assert {:ok, context} = Goals.fetch_run_context(runtime.machine_id, run.id, fence)
    assert context.run_id == run.id
    assert context.session_id == nil
    assert context.context["schema_version"] == "symmetry.context_snapshot.v1"
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

  test "admission rejects a caller-defined Subject before a Task row is written" do
    {goal, item, task} = admitted_task_fixture()
    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "completed"])

    assert {:error, :invalid_request} =
             command_current(
               goal.id,
               "admit_task",
               Map.put(admission_payload(item), :subject, %{
                 resource_id: item_repository_resource_id(item)
               }),
               rollout_enabled: true
             )
  end

  test "runless cancellation settlement requires a canonical Goal control cancellation" do
    previous_goals = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(previous_goals, :rollout_enabled, true)
    )

    on_exit(fn -> Application.put_env(:symmetry_control, :goals, previous_goals) end)

    {_goal, _item, task} = admitted_task_fixture()
    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "cancelled"])

    assert {:error, :invalid_unstarted_settlement} =
             Goals.settle_unstarted_task(task.id, "cancelled", now: @now)

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^task.id
             )
           ).state == "held"

    {controlled_goal, _controlled_item, controlled_task} = admitted_task_fixture()

    assert {:ok, cancelled, :created} =
             command_current(controlled_goal.id, "cancel", %{reason: "Stop before a Run starts."})

    assert :ok =
             GoalControlWorker.perform(%Oban.Job{
               args: %{
                 "goal_id" => controlled_goal.id,
                 "revision" => cancelled.goal.current_revision,
                 "action_id" => cancelled.response["control_action_id"]
               }
             })

    assert {:ok, %{"settlement" => "released_without_run"} = receipt} =
             Goals.settle_unstarted_task(controlled_task.id, "cancelled", now: @now)

    assert {:ok, ^receipt} =
             Goals.settle_unstarted_task(controlled_task.id, "cancelled", now: @now)

    assert Repo.aggregate(from(run in Run, where: run.task_id == ^controlled_task.id), :count) ==
             0

    assert Repo.one!(
             from(row in SymmetryControl.Goals.GoalBudgetReservation,
               where: row.task_id == ^controlled_task.id
             )
           ).state == "released"

    assert {:error, :invalid_request} =
             Goals.settle_unstarted_task(controlled_task.id, "failed", now: @now)

    assert {:ok, projection} = Goals.fetch_goal(controlled_goal.id)
    assert projection.execution.run_count == 0
  end

  test "historical task settlement retains the producing Task revision in its audit receipt" do
    {goal, _item, task, _runtime, run, _fence} = claimed_goal_run_fixture()

    assert {:ok, amended, :created} =
             command_current(goal.id, "amend", %{
               revision_contract:
                 amended_revision_contract("Record historical settlement accurately."),
               reason: "The active work is now historical."
             })

    assert amended.goal.current_revision == 2

    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "completed"])
    Repo.update_all(from(row in Run, where: row.id == ^run.id), set: [state: "completed"])

    assert {:ok, %{"goal_revision" => 1, "settlement" => "missing_result"}} =
             Goals.settle_task(task.id, run.id, run.generation, now: @now)

    event =
      Repo.one!(
        from(event in SymmetryControl.Goals.GoalEvent,
          where: event.goal_id == ^goal.id and event.kind == "task_settled",
          order_by: [desc: event.sequence],
          limit: 1
        )
      )

    assert event.revision == 1
    assert event.payload["goal_revision"] == 1
    assert event.response["goal_revision"] == 1
    assert event.response["_receipt_v1"]["event"]["revision"] == 1
  end

  test "a resumed Goal makes an old pause control action superseded" do
    {goal, _item, _task} = admitted_task_fixture()

    assert {:ok, paused, :created} =
             command_current(goal.id, "pause", %{reason: "operator review"})

    action_id = paused.response["control_action_id"]

    assert Repo.exists?(
             from(job in Oban.Job,
               where: job.worker == "SymmetryControl.Goals.Workers.GoalControlWorker",
               where: fragment("? ->> 'action_id' = ?", job.args, ^action_id)
             )
           )

    assert {:ok, []} =
             Goals.control_dispatch_plan(goal.id, paused.goal.current_revision, action_id)

    assert {:ok, _resumed, :created} = command_current(goal.id, "resume", %{reason: "approved"})

    assert {:ok, :superseded} =
             Goals.control_dispatch_plan(goal.id, paused.goal.current_revision, action_id)
  end

  test "cancel dispatch excludes a Task already cancelling" do
    {goal, _item, task} = admitted_task_fixture()
    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "cancelling"])

    assert {:ok, cancelled, :created} = command_current(goal.id, "cancel", %{reason: "stop"})

    assert {:ok, []} =
             Goals.control_dispatch_plan(
               goal.id,
               cancelled.goal.current_revision,
               cancelled.response["control_action_id"]
             )
  end

  test "amend cancels runs when native safe pause is unavailable" do
    {safe_goal, _safe_item, safe_task, safe_runtime, _safe_run, _safe_fence} =
      claimed_goal_run_fixture()

    Repo.update_all(
      from(row in Runtime, where: row.id == ^safe_runtime.id),
      set: [capabilities: %{"adapter" => %{"operations" => %{"pause" => "safe_boundary"}}}]
    )

    assert {:ok, safe_amendment, :created} =
             command_current(safe_goal.id, "amend", %{
               revision_contract: amended_revision_contract("Pause supported work."),
               reason: "Scope was clarified"
             })

    assert {:ok, descriptors} =
             Goals.control_dispatch_plan(
               safe_goal.id,
               safe_amendment.goal.current_revision,
               safe_amendment.response["control_action_id"]
             )

    safe_task_id = safe_task.id
    assert [%{task_id: ^safe_task_id, kind: "cancel", payload: %{}}] = descriptors

    assert String.contains?(hd(descriptors).idempotency_key, ":cancel:")

    {cancel_goal, _cancel_item, cancel_task, cancel_runtime, _cancel_run, _cancel_fence} =
      claimed_goal_run_fixture()

    Repo.update_all(
      from(row in Runtime, where: row.id == ^cancel_runtime.id),
      set: [capabilities: %{"adapter" => %{"operations" => %{"pause" => "unsupported"}}}]
    )

    assert {:ok, cancel_amendment, :created} =
             command_current(cancel_goal.id, "amend", %{
               revision_contract: amended_revision_contract("Pause unsupported work."),
               reason: "Scope was clarified"
             })

    cancel_task_id = cancel_task.id

    assert {:ok, [%{task_id: ^cancel_task_id, kind: "cancel", payload: %{}}]} =
             Goals.control_dispatch_plan(
               cancel_goal.id,
               cancel_amendment.goal.current_revision,
               cancel_amendment.response["control_action_id"]
             )
  end

  test "strict automatic and manual admissions derive the operator-approved per-run ceiling" do
    project = project_fixture()

    incomplete_strict_policy = %{
      "automatic_execution" => false,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "max_run_attempts_per_task" => 2,
      "budget_limit_microusd" => 239,
      "per_run_cost_limit_microusd" => nil,
      "budget_mode" => "strict",
      "hard_cost_limit_required" => false,
      "allowed_runtime_ids" => [],
      "allowed_model_profiles" => ["codex"],
      "final_acceptance" => "operator",
      "allowed_actions" => ["implement"],
      "allowed_resource_ids" => []
    }

    assert {:error, _reason} =
             Goals.create_goal(
               project.id,
               goal_attrs(nil, incomplete_strict_policy),
               "operator:test",
               now: @now
             )

    strict_policy = %{
      "automatic_execution" => false,
      "max_parallel_tasks" => 2,
      "max_task_admissions" => 2,
      "max_run_attempts_per_task" => 2,
      "budget_limit_microusd" => 100,
      "per_run_cost_limit_microusd" => 60,
      "budget_mode" => "strict",
      "hard_cost_limit_required" => true,
      "allowed_runtime_ids" => [],
      "allowed_model_profiles" => ["codex"],
      "final_acceptance" => "operator",
      "allowed_actions" => ["implement"],
      "allowed_resource_ids" => []
    }

    {goal, [first_item, second_item]} = goal_with_items_fixture(strict_policy, 2)

    assert {:error, :invalid_request} =
             command_current(
               goal.id,
               "admit_task",
               Map.put(admission_payload(first_item), :reserved_microusd, 60),
               rollout_enabled: true
             )

    assert {:ok, first_admission, :created} =
             command_current(goal.id, "admit_task", admission_payload(first_item),
               rollout_enabled: true
             )

    first_task = Repo.get!(Task, first_admission.response["task"]["id"])

    first_reservation =
      Repo.one!(
        from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
          where: reservation.task_id == ^first_task.id
        )
      )

    assert first_reservation.reserved_microusd == 120

    assert first_task.input["limits"]["max_turns"] == 1

    assert first_task.input["limits"]["deadline_at"] ==
             DateTime.to_iso8601(DateTime.add(@now, 15 * 60, :second))

    assert {:error, :budget_exhausted} =
             command_current(goal.id, "admit_task", admission_payload(second_item),
               rollout_enabled: true
             )

    automatic_policy = Map.merge(strict_policy, %{"automatic_execution" => true})

    {automatic_goal, [automatic_item, automatic_second_item]} =
      goal_with_items_fixture(automatic_policy, 2)

    set_goal_due!(automatic_goal.id)

    assert {:ok, [%{disposition: :admitted, purpose: "implement", task_id: task_id}]} =
             Goals.reconcile_due_goals(
               goal_ids: [automatic_goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    automatic_task = Repo.get!(Task, task_id)

    assert automatic_task.work_item_id == automatic_item.id
    assert automatic_task.input["limits"]["max_turns"] == 1
    assert automatic_task.work_item_id != automatic_second_item.id

    assert Repo.aggregate(
             from(task in Task, where: task.goal_id == ^automatic_goal.id),
             :count
           ) == 1

    assert Repo.one!(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.task_id == ^automatic_task.id
             )
           ).reserved_microusd == 120
  end

  test "an external blocker is retained as unsupported history without a model observation" do
    policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 5,
      "max_run_attempts_per_task" => 2,
      "budget_limit_microusd" => 1_000_000,
      "per_run_cost_limit_microusd" => nil,
      "budget_mode" => "soft",
      "hard_cost_limit_required" => false,
      "allowed_runtime_ids" => [],
      "allowed_model_profiles" => ["codex"],
      "final_acceptance" => "operator",
      "allowed_actions" => ["implement", "observe"],
      "allowed_resource_ids" => []
    }

    {goal, [item]} = goal_with_items_fixture(policy)

    assert {:ok, admission, :created} =
             command_current(goal.id, "admit_task", admission_payload(item),
               rollout_enabled: true
             )

    source_task = Repo.get!(Task, admission.response["task"]["id"])
    runtime = runtime_fixture()
    {source_run, _source_fence} = completed_goal_run_fixture(source_task, runtime)
    requested_check_at = DateTime.add(@now, 60, :second)

    external_blocker = %{
      "kind" => "external",
      "resource_id" => item_repository_resource_id(item),
      "external_ref" => "ci://build/unsupported",
      "next_check_at" => DateTime.to_iso8601(requested_check_at)
    }

    blocked_result =
      task_result(source_task, "blocked", %{
        "blocker" => external_blocker,
        "proposed_next_action" => %{
          "kind" => "observe",
          "resource_id" => item_repository_resource_id(item),
          "external_ref" => external_blocker["external_ref"]
        }
      })

    put_task_result!(source_run, blocked_result)

    assert {:ok, receipt} = Goals.settle_task(source_task.id, source_run.id, 1, now: @now)
    assert receipt["settlement"] == "blocked"
    assert receipt["reason"] == "unsupported_external_check"
    assert receipt["next_wake_at"] == nil
    assert receipt["proposed_next_action"] == nil

    wait = Repo.get_by!(GoalExternalWait, goal_id: goal.id, work_item_id: item.id)
    assert wait.state == "unsupported"
    assert wait.next_check_at == nil
    assert wait.check_seq == 0
    assert wait.result == blocked_result
    assert wait.subject == source_task.input["subject"]
    assert wait.resource_id == item_repository_resource_id(item)
    assert wait.external_ref == external_blocker["external_ref"]
    assert wait.source_ref["task_id"] == source_task.id
    assert wait.source_ref["run_id"] == source_run.id
    assert wait.source_ref["generation"] == source_run.generation
    assert wait.source_ref["result_id"] == blocked_result["result_id"]
    assert wait.receipt_event_id == receipt["mutation_id"]

    assert {:ok, projection} = Goals.fetch_goal(goal.id)
    assert projection.next_wake_at == nil
    assert "unsupported_external_check" in projection.blocker_reasons
    refute "waiting_external" in projection.blocker_reasons

    assert [
             %{id: wait_id, state: "unsupported", reason: "unsupported_external_check"} =
               projected_wait
           ] =
             projection.external_waits

    assert wait_id == wait.id
    assert projected_wait.subject == wait.subject
    assert projected_wait.source_ref == wait.source_ref
    assert projected_wait.resource_id == wait.resource_id
    assert projected_wait.external_ref == wait.external_ref

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 1

    assert Repo.aggregate(
             from(reservation in SymmetryControl.Goals.GoalBudgetReservation,
               where: reservation.goal_id == ^goal.id
             ),
             :count
           ) == 1

    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :noop}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Repo.get!(GoalExternalWait, wait.id).state == "unsupported"

    assert Repo.get!(SymmetryControl.Goals.Goal, goal.id).next_wake_at == nil

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 1

    assert {:error, :unsupported_external_check} =
             command_current(
               goal.id,
               "admit_task",
               %{work_item_id: item.id, purpose: "observe", model_profile: "codex"},
               rollout_enabled: true
             )
  end

  test "automatic repair creates a fresh implementation Task with immutable source identity" do
    policy = %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 2,
      "max_run_attempts_per_task" => 2,
      "budget_limit_microusd" => 1_000_000,
      "per_run_cost_limit_microusd" => nil,
      "budget_mode" => "soft",
      "hard_cost_limit_required" => false,
      "allowed_runtime_ids" => [],
      "allowed_model_profiles" => ["codex"],
      "final_acceptance" => "operator",
      "allowed_actions" => ["implement"],
      "allowed_resource_ids" => []
    }

    {goal, [item]} = goal_with_items_fixture(policy)

    assert {:ok, admission, :created} =
             command_current(goal.id, "admit_task", admission_payload(item),
               rollout_enabled: true
             )

    source_task = Repo.get!(Task, admission.response["task"]["id"])
    runtime = runtime_fixture()
    {source_run, _source_fence} = completed_goal_run_fixture(source_task, runtime)

    repair_action = %{
      "kind" => "repair",
      "work_item_id" => item.id,
      "reason" => "Fix the failed check."
    }

    repair_result =
      task_result(source_task, "repair_required", %{
        "proposed_next_action" => repair_action
      })

    put_task_result!(source_run, repair_result)

    assert {:ok, repair_receipt} =
             Goals.settle_task(source_task.id, source_run.id, 1, now: @now)

    assert repair_receipt["settlement"] == "repair_required"
    assert repair_receipt["next_wake_at"] == DateTime.to_iso8601(@now)

    assert {:ok, refreshed} = Goals.fetch_goal(goal.id)
    assert DateTime.compare(refreshed.next_wake_at, @now) == :eq

    assert {:ok, [%{disposition: :admitted, purpose: "implement", task_id: repair_task_id}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    repair_task = Repo.get!(Task, repair_task_id)
    assert repair_task.id != source_task.id
    assert repair_task.purpose == "implement"
    assert repair_task.input["subject"] == source_task.input["subject"]
    assert repair_task.input["limits"]["max_turns"] == 1

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 2

    event =
      Repo.one!(
        from(event in SymmetryControl.Goals.GoalEvent,
          where:
            event.goal_id == ^goal.id and event.kind == "automatic_task_admitted" and
              fragment("? ->> 'task_id' = ?", event.payload, ^repair_task.id)
        )
      )

    assert event.payload["source_task_id"] == source_task.id
    assert event.payload["source_result_id"] == repair_result["result_id"]
    assert event.response["source_identity"] =~ repair_result["result_id"]

    set_goal_due!(goal.id)

    assert {:ok, [%{disposition: :noop}]} =
             Goals.reconcile_due_goals(
               goal_ids: [goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^goal.id), :count) == 2
  end

  defp goal_with_items_fixture(execution_policy, item_count \\ 1) do
    project = project_fixture()
    repository = repository_fixture(project)

    policy =
      %{
        "max_parallel_tasks" => max(item_count, 1),
        "max_task_admissions" => max(item_count, 1),
        "max_run_attempts_per_task" => 2,
        "budget_limit_microusd" => nil,
        "per_run_cost_limit_microusd" => nil,
        "budget_mode" => "soft",
        "hard_cost_limit_required" => false,
        "allowed_resource_ids" => [repository.id],
        "allowed_model_profiles" => ["codex"],
        "allowed_actions" => ["implement"]
      }
      |> Map.merge(execution_policy)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(nil, policy), "operator:test", now: @now)

    proposal =
      plan_proposal(
        created.goal.id,
        Enum.map(1..item_count, fn index ->
          %{
            key: "goal-test-item-#{index}",
            title: "Goal test item #{index}",
            description: "Bounded test work.",
            required: true,
            integration: index == 1,
            repository_resource_id: repository.id,
            acceptance: check_contract(),
            depends_on_keys: [],
            model_profile: "codex",
            baseline: baseline_subject(repository.id)
          }
        end)
      )

    _planned = accept_plan!(created.goal.id, proposal)

    assert {:ok, _active, :created} =
             command_current(created.goal.id, "activate", %{approved_revision: 1})

    {:ok, goal} = Goals.fetch_goal(created.goal.id)

    items =
      Repo.all(
        from(item in WorkItem,
          where: item.goal_id == ^created.goal.id,
          order_by: [asc: item.id]
        )
      )

    {goal, items}
  end

  defp optional_goal_work_item!(goal, source_item, title) do
    %WorkItem{id: Ecto.UUID.generate()}
    |> WorkItem.changeset(%{
      project_id: goal.project_id,
      repository_resource_id: source_item.repository_resource_id,
      title: title,
      description: "Optional work outside the completion dependency closure.",
      status: "backlog",
      priority: "no_priority",
      position: 0,
      assignee_type: "agent",
      assignee_name: source_item.agent_profile,
      agent_profile: source_item.agent_profile,
      workspace: source_item.workspace,
      blocked: false
    })
    |> Ecto.Changeset.apply_changes()
    |> WorkItem.goal_membership_changeset(%{
      goal_id: goal.id,
      admitted_revision: goal.current_revision,
      required: false,
      integration: false,
      acceptance_contract: source_item.acceptance_contract,
      baseline_subject: source_item.baseline_subject,
      baseline_dependency_id: nil,
      change_target: nil
    })
    |> Repo.insert!()
  end

  defp admitted_task_fixture(
         default_workspace \\ "primary",
         acceptance_contract \\ check_contract(),
         revision_overrides \\ %{},
         admit? \\ true,
         validation_profiles_override \\ nil
       ) do
    project = project_fixture(default_workspace)
    repository = repository_fixture(project)

    default_execution_policy = %{
      "max_task_admissions" => 2,
      "budget_limit_microusd" => 42,
      "allowed_resource_ids" => [repository.id],
      "allowed_model_profiles" => ["codex"]
    }

    execution_policy =
      Map.merge(default_execution_policy, Map.get(revision_overrides, :execution_policy, %{}))

    context_manifest = Map.get(revision_overrides, :context_manifest, %{})

    assert {:ok, created, :created} =
             Goals.create_goal(
               project.id,
               goal_attrs(nil, execution_policy, context_manifest),
               "operator:test",
               now: @now
             )

    goal_id = created.goal.id

    proposal = %{
      schema_version: "symmetry.plan.v1",
      proposal_id: Ecto.UUID.generate(),
      goal_id: goal_id,
      expected_revision: 1,
      items: [
        %{
          key: "implement",
          title: "Implement",
          description: "Bounded work",
          required: true,
          integration: true,
          repository_resource_id: repository.id,
          acceptance: acceptance_contract,
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
               question: "Accept?",
               options: [%{"id" => "accept", "label" => "Accept", "consequence" => "Proceed"}],
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

    assert {:ok, planned, :created} =
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

    [item] = planned.goal.work_items

    assert {:ok, _active, :created} =
             command_current(goal_id, "activate", %{approved_revision: 1})

    {:ok, goal} = Goals.fetch_goal(goal_id)

    if admit? do
      admission_opts =
        case validation_profiles_override do
          nil -> [rollout_enabled: true]
          profiles -> [rollout_enabled: true, validation_profiles: profiles]
        end

      assert {:ok, admitted, :created} =
               command_current(
                 goal_id,
                 "admit_task",
                 admission_payload(item, %{reserved_microusd: 42}),
                 admission_opts
               )

      {goal, item, Repo.get!(Task, admitted.response["task"]["id"])}
    else
      {goal, item, nil}
    end
  end

  defp claimed_goal_run_fixture(
         acceptance_contract \\ check_contract(),
         runtime_id \\ nil,
         revision_overrides \\ %{}
       ) do
    {goal, item, task} = admitted_task_fixture("primary", acceptance_contract, revision_overrides)
    runtime = runtime_fixture(runtime_id)
    claim_id = Ecto.UUID.generate()
    lease_token = Ecto.UUID.generate()

    Repo.update_all(
      from(row in Task, where: row.id == ^task.id),
      set: [state: "running", current_generation: 1, updated_at: @now]
    )

    run =
      %Run{}
      |> Run.changeset(%{
        task_id: task.id,
        runtime_id: runtime.id,
        generation: 1,
        state: "running",
        claimed_runtime_epoch: runtime.connection_epoch,
        claim_id: claim_id,
        lease_token: lease_token,
        assigned_at: @now,
        assignment_expires_at: DateTime.add(@now, 60, :second),
        claimed_at: @now,
        lease_expires_at: DateTime.add(DateTime.utc_now(), 3_600, :second)
      })
      |> Repo.insert!()

    {goal, item, Repo.get!(Task, task.id), runtime, run,
     %{
       runtime_id: runtime.id,
       runtime_epoch: runtime.connection_epoch,
       generation: 1,
       claim_id: claim_id,
       lease_token: lease_token
     }}
  end

  defp validation_goal_run_fixture(
         acceptance_contract \\ check_contract(),
         runtime_id \\ @validation_runtime_id
       ) do
    {goal, item, task, runtime, run, fence} =
      claimed_goal_run_fixture(acceptance_contract, runtime_id)

    producer = validation_producer_task_fixture(task)

    Repo.update_all(
      from(row in Task, where: row.id == ^task.id),
      set: [purpose: "validate", validation_of_task_id: producer.id]
    )

    {goal, item, task, runtime, run, fence}
  end

  defp validation_producer_task_fixture(task) do
    %Task{}
    |> Task.changeset(%{
      idempotency_key: "validation-producer:" <> Ecto.UUID.generate(),
      request_hash: :crypto.hash(:sha256, Ecto.UUID.generate()),
      request_hash_version: 2,
      work_item_id: task.work_item_id,
      goal_id: task.goal_id,
      goal_revision: task.goal_revision,
      context_snapshot_id: task.context_snapshot_id,
      goal: task.goal,
      agent_profile: task.agent_profile,
      workspace: task.workspace,
      input: task.input,
      required_capabilities: task.required_capabilities,
      state: "completed",
      current_generation: 1,
      attempt_generation: 1,
      purpose: "implement",
      admission_key: Ecto.UUID.generate(),
      max_run_attempts: task.max_run_attempts
    })
    |> Repo.insert!()
  end

  defp completed_goal_run_fixture(task, runtime, candidate_subject \\ nil) do
    claim_id = Ecto.UUID.generate()
    lease_token = Ecto.UUID.generate()

    subject = candidate_subject || Map.fetch!(task.input, "subject")

    subject_hash =
      subject
      |> SymmetryControl.RequestHash.canonical()
      |> Base.encode16(case: :lower)
      |> then(&("sha256:" <> &1))

    result = %{
      "task_result" => %{
        "schema_version" => "symmetry.task_result.v1",
        "result_id" => Ecto.UUID.generate(),
        "kind" => "candidate_completion",
        "summary" => "Candidate completion for the admitted subject.",
        "subject" => subject,
        "subject_hash" => subject_hash,
        "evidence_refs" => [],
        "blocker" => nil,
        "proposed_next_action" => nil,
        "proposal" => nil,
        "reason" => nil,
        "diagnostics" => []
      }
    }

    Repo.update_all(
      from(row in Task, where: row.id == ^task.id),
      set: [state: "completed", current_generation: 1, result: result, updated_at: @now]
    )

    run =
      %Run{}
      |> Run.changeset(%{
        task_id: task.id,
        runtime_id: runtime.id,
        generation: 1,
        state: "completed",
        claimed_runtime_epoch: runtime.connection_epoch,
        claim_id: claim_id,
        lease_token: lease_token,
        assigned_at: @now,
        assignment_expires_at: DateTime.add(@now, 60, :second),
        claimed_at: @now,
        lease_expires_at: DateTime.add(@now, 3_600, :second),
        result: result
      })
      |> Repo.insert!()

    {run,
     %{
       runtime_id: runtime.id,
       runtime_epoch: runtime.connection_epoch,
       generation: 1,
       claim_id: claim_id,
       lease_token: lease_token
     }}
  end

  defp independent_validation_attempt_fixture(acceptance_contract \\ check_contract()) do
    {goal, item, producer} =
      admitted_task_fixture("primary", acceptance_contract, %{
        execution_policy: %{"max_task_admissions" => 4}
      })

    candidate_subject = %{
      "resource_id" => item_repository_resource_id(item),
      "commit" => String.duplicate("c", 40),
      "tree_digest" => "sha256:" <> String.duplicate("d", 64)
    }

    producer_runtime = runtime_fixture()

    {producer_run, _producer_fence} =
      completed_goal_run_fixture(producer, producer_runtime, candidate_subject)

    assert {:ok, %{"settlement" => "awaiting_validation"}} =
             Goals.settle_task(producer.id, producer_run.id, 1, now: @now)

    assert {:ok, validation_admission, :created} =
             command_current(
               goal.id,
               "admit_task",
               admission_payload(item, %{
                 purpose: "validate",
                 validation_of_task_id: producer.id,
                 reserved_microusd: 0
               })
               |> Map.delete(:subject),
               rollout_enabled: true
             )

    validation = Repo.get!(Task, validation_admission.response["task"]["id"])
    validation_runtime = runtime_fixture(@validation_runtime_id)

    {validation_run, validation_fence} =
      completed_goal_run_fixture(validation, validation_runtime, candidate_subject)

    {goal, item, producer, producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject}
  end

  defp task_result(task, kind, overrides \\ %{}) do
    overrides = Map.new(overrides, fn {key, value} -> {to_string(key), value} end)
    subject = Map.get(overrides, "subject", Map.fetch!(task.input, "subject"))

    Map.merge(
      %{
        "schema_version" => "symmetry.task_result.v1",
        "result_id" => Ecto.UUID.generate(),
        "kind" => kind,
        "summary" => "A bounded TaskResult for settlement coverage.",
        "subject" => subject,
        "subject_hash" =>
          "sha256:" <> Base.encode16(SymmetryControl.RequestHash.canonical(subject), case: :lower),
        "evidence_refs" => [],
        "blocker" => nil,
        "proposed_next_action" => nil,
        "proposal" => nil,
        "reason" => nil,
        "diagnostics" => []
      },
      overrides
    )
  end

  defp put_task_result!(run, task_result) do
    Repo.update_all(
      from(row in Run, where: row.id == ^run.id),
      set: [result: %{"task_result" => task_result}]
    )
  end

  defp delete_goal_wakeup_jobs(goal_id) do
    Repo.delete_all(
      from(job in Oban.Job,
        where:
          job.worker == "SymmetryControl.Goals.Workers.WakeupWorker" and
            fragment("? ->> 'goal_id' = ?", job.args, ^goal_id)
      )
    )
  end

  defp wakeup_job_count(goal_id) do
    Repo.aggregate(
      from(job in Oban.Job,
        where:
          job.worker == "SymmetryControl.Goals.Workers.WakeupWorker" and
            fragment("? ->> 'goal_id' = ?", job.args, ^goal_id)
      ),
      :count
    )
  end

  defp unstarted_settlement_event_count(goal_id, task_id) do
    Repo.aggregate(
      from(event in SymmetryControl.Goals.GoalEvent,
        where:
          event.goal_id == ^goal_id and event.kind == "task_unstarted_settled" and
            fragment("? ->> 'task_id' = ?", event.payload, ^task_id)
      ),
      :count
    )
  end

  defp session_attrs(item, workspace_fingerprint) do
    %{
      local_handle_id: Ecto.UUID.generate(),
      harness_kind: "codex",
      harness_version: "1.0.0",
      adapter_version: "1.0.0",
      workspace_fingerprint: workspace_fingerprint,
      workspace: "primary",
      repository_resource_id: item_repository_resource_id(item)
    }
  end

  defp available_session_fixture(runtime, item, attrs) do
    %HarnessSession{}
    |> HarnessSession.changeset(%{
      machine_id: runtime.machine_id,
      runtime_id: runtime.id,
      repository_resource_id: item_repository_resource_id(item),
      local_handle_id: attrs.local_handle_id,
      harness_kind: attrs.harness_kind,
      harness_version: attrs.harness_version,
      adapter_version: attrs.adapter_version,
      workspace_fingerprint: attrs.workspace_fingerprint,
      state: "available"
    })
    |> Repo.insert!()
  end

  defp amended_revision_contract(objective) do
    goal_attrs()
    |> Map.fetch!(:initial_revision)
    |> Map.put(:objective, objective)
  end

  defp complete_task_and_release_reservation!(goal, task) do
    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "completed"])

    Repo.update_all(
      from(row in SymmetryControl.Goals.GoalBudgetReservation,
        where: row.goal_id == ^goal.id and row.task_id == ^task.id
      ),
      set: [state: "released"]
    )
  end

  defp admission_payload(item, overrides \\ %{}) do
    Map.merge(
      %{
        work_item_id: item.id,
        purpose: "implement",
        model_profile: "codex"
      },
      Map.drop(overrides, [:subject, :limits, :reserved_microusd, :admission_key])
    )
  end

  defp evidence_attrs(run_id, item), do: evidence_attrs(run_id, item, evidence_subject(item))

  defp evidence_attrs(run_id, _item, subject) do
    %{
      schema_version: "symmetry.evidence.v1",
      evidence_id: Ecto.UUID.generate(),
      run_id: run_id,
      evidence_key: "check:unit",
      kind: "check",
      subject: subject,
      subject_hash:
        "sha256:" <> Base.encode16(SymmetryControl.RequestHash.canonical(subject), case: :lower),
      source_ref: %{
        kind: "check",
        ref: "unit-tests",
        validator_profile: "test",
        subject_hash:
          "sha256:" <> Base.encode16(SymmetryControl.RequestHash.canonical(subject), case: :lower)
      },
      source_revision: "checks-v1",
      validator_profile: "test",
      verdict: "passed",
      payload: %{
        predicate_id: "check",
        subject: subject,
        subject_hash:
          "sha256:" <> Base.encode16(SymmetryControl.RequestHash.canonical(subject), case: :lower),
        profile_digest: "sha256:" <> String.duplicate("c", 64),
        command_argv_digest: "sha256:" <> String.duplicate("d", 64),
        exit_code: 0,
        started_at: DateTime.to_iso8601(@now),
        finished_at: DateTime.to_iso8601(@now),
        output_ref: %{kind: "artifact", value: "check-output"}
      },
      observed_at: DateTime.to_iso8601(@now)
    }
  end

  defp review_evidence_attrs(run_id, item, review_task_id) do
    subject = evidence_subject(item)
    subject_hash = evidence_subject_hash(subject)

    %{
      schema_version: "symmetry.evidence.v1",
      evidence_id: Ecto.UUID.generate(),
      run_id: run_id,
      evidence_key: "review:independent",
      kind: "review",
      subject: subject,
      subject_hash: subject_hash,
      source_ref: %{
        kind: "review",
        ref: "independent-review",
        review_task_id: review_task_id,
        subject_hash: subject_hash
      },
      source_revision: @validation_profile_digest,
      validator_profile: "test-review",
      verdict: "passed",
      payload: %{
        predicate_id: "review",
        subject: subject,
        subject_hash: subject_hash,
        review_task_id: review_task_id,
        findings: [],
        verdict: "passed"
      },
      observed_at: DateTime.to_iso8601(@now)
    }
  end

  defp usage_attrs(run_id) do
    %{
      schema_version: "symmetry.usage.v1",
      usage_id: Ecto.UUID.generate(),
      run_id: run_id,
      usage_key: "turn-1",
      provider: "codex",
      model: "codex-test",
      input_tokens: 10,
      output_tokens: 20,
      cached_input_tokens: 0,
      cost_microusd: "42",
      cost_basis: "reported",
      price_version: nil,
      supersedes_id: nil,
      observed_at: DateTime.to_iso8601(@now)
    }
  end

  defp artifact_evidence_attrs(run_id, item) do
    subject = evidence_subject(item)
    subject_hash = evidence_subject_hash(subject)

    %{
      schema_version: "symmetry.evidence.v1",
      evidence_id: Ecto.UUID.generate(),
      run_id: run_id,
      evidence_key: "artifact:proof",
      kind: "artifact",
      subject: subject,
      subject_hash: subject_hash,
      source_ref: %{
        kind: "artifact",
        ref: "proof-file",
        resource_id: item_repository_resource_id(item),
        commit: subject["commit"],
        path: "proof.txt",
        subject_hash: subject_hash
      },
      source_revision: "artifact-v1",
      validator_profile: "artifact",
      verdict: "passed",
      payload: %{
        predicate_id: "artifact",
        subject: subject,
        subject_hash: subject_hash,
        resource_id: item_repository_resource_id(item),
        commit: subject["commit"],
        path: "proof.txt",
        content_digest: "sha256:" <> String.duplicate("c", 64)
      },
      observed_at: DateTime.to_iso8601(@now)
    }
  end

  defp observation_evidence_attrs(run_id, item) do
    subject = evidence_subject(item)
    subject_hash = evidence_subject_hash(subject)

    %{
      schema_version: "symmetry.evidence.v1",
      evidence_id: Ecto.UUID.generate(),
      run_id: run_id,
      evidence_key: "observation:remote",
      kind: "observation",
      subject: subject,
      subject_hash: subject_hash,
      source_ref: %{
        kind: "observation",
        ref: "remote-status",
        external_ref: "status:123",
        subject_hash: subject_hash
      },
      source_revision: "observation-v1",
      validator_profile: nil,
      verdict: "passed",
      payload: %{
        predicate_id: "observation",
        subject: subject,
        subject_hash: subject_hash,
        external_ref: "status:123",
        observed_at: DateTime.to_iso8601(@now),
        note: "The external status is available."
      },
      observed_at: DateTime.to_iso8601(@now)
    }
  end

  defp evidence_subject(item) do
    %{
      "resource_id" => item_repository_resource_id(item),
      "commit" => String.duplicate("a", 40),
      "tree_digest" => "sha256:" <> String.duplicate("b", 64)
    }
  end

  defp evidence_subject_hash(subject) do
    "sha256:" <> Base.encode16(SymmetryControl.RequestHash.canonical(subject), case: :lower)
  end

  defp command_current(goal_id, kind, payload, opts \\ []) do
    assert {:ok, goal} = Goals.fetch_goal(goal_id)
    opts = Keyword.put_new(opts, :validation_profiles, validation_profiles())

    Goals.command(
      goal_id,
      command(goal, kind, payload),
      "operator:test",
      Keyword.merge([now: @now], opts)
    )
  end

  defp plan_proposal(goal_id, items) do
    %{
      schema_version: "symmetry.plan.v1",
      proposal_id: Ecto.UUID.generate(),
      goal_id: goal_id,
      expected_revision: 1,
      items:
        Enum.map(items, fn item ->
          item
          |> Map.put_new(:integration, true)
          |> Map.put_new(:change_target, nil)
        end)
    }
  end

  defp accept_plan!(goal_id, proposal) do
    assert {:ok, decision, :created} =
             command_current(goal_id, "request_decision", %{
               kind: "plan",
               question: "Accept the explicit baseline plan?",
               options: [
                 %{"id" => "accept", "label" => "Accept", "consequence" => "Admit the plan"}
               ],
               proposal: proposal
             })

    decision_id = decision.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(goal_id, "resolve_decision", %{
               decision_id: decision_id,
               expected_decision_version:
                 Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).lock_version,
               option_id: "accept"
             })

    assert {:ok, planned, :created} =
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

    planned
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

  defp reject_plan_admission!(goal_id, proposal, opts \\ []) do
    assert {:ok, decision, :created} =
             command_current(goal_id, "request_decision", %{
               kind: "plan",
               question: "Accept this plan?",
               options: [%{"id" => "accept", "label" => "Accept", "consequence" => "Proceed"}],
               proposal: proposal
             })

    decision_id = decision.response["decision"]["id"]

    assert {:ok, _resolved, :created} =
             command_current(goal_id, "resolve_decision", %{
               decision_id: decision_id,
               expected_decision_version:
                 Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).lock_version,
               option_id: "accept"
             })

    assert {:error, reason} =
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
               Keyword.merge([rollout_enabled: true], opts)
             )

    reason
  end

  defp configure_automatic_runtimes!(item, runtimes) do
    Enum.each(runtimes, fn runtime ->
      Repo.update_all(
        from(row in Runtime, where: row.id == ^runtime.id),
        set: [
          repository_resource_id: item_repository_resource_id(item),
          last_heartbeat_at: @now,
          capabilities: %{
            "adapter" => %{
              "kind" => "codex",
              "native_version" => "1.0.0",
              "implementation_version" => "1.0.0",
              "protocol_version" => 1,
              "operations" => %{
                "start" => true,
                "events" => true,
                "cancel" => true,
                "resume" => false,
                "guidance" => "unsupported",
                "pause" => "unsupported",
                "approval_response" => false,
                "usage" => "unknown",
                "hard_cost_limit" => false
              }
            }
          }
        ]
      )
    end)
  end

  defp set_goal_due!(goal_id) do
    Repo.update_all(
      from(goal in SymmetryControl.Goals.Goal, where: goal.id == ^goal_id),
      set: [state: "active", next_wake_at: @now, updated_at: @now]
    )
  end

  defp command(goal, kind, payload, mutation_id \\ Ecto.UUID.generate()) do
    %{
      schema_version: "symmetry.goal_command.v1",
      mutation_id: mutation_id,
      expected_version: goal.version,
      expected_revision: goal.current_revision,
      kind: kind,
      payload: canonical_command_payload(kind, payload)
    }
  end

  defp canonical_command_payload("request_decision", payload) do
    case Map.get(payload, :kind, Map.get(payload, "kind")) do
      "plan" ->
        %{
          kind: "plan",
          work_item_id: nil,
          subject_hash: nil,
          proposal: Map.get(payload, :proposal, Map.get(payload, "proposal"))
        }

      "scope" ->
        %{
          kind: "scope",
          work_item_id: Map.get(payload, :work_item_id, Map.get(payload, "work_item_id")),
          subject_hash: nil,
          proposal: %{
            kind:
              Map.get(payload, :dependency_operation, Map.get(payload, "dependency_operation")),
            depends_on_id: Map.get(payload, :depends_on_id, Map.get(payload, "depends_on_id"))
          }
        }

      "review" ->
        %{
          kind: "review",
          work_item_id: Map.get(payload, :work_item_id, Map.get(payload, "work_item_id")),
          subject_hash: Map.get(payload, :subject_hash, Map.get(payload, "subject_hash")),
          proposal: nil
        }

      "completion" ->
        %{
          kind: "completion",
          work_item_id: Map.get(payload, :work_item_id, Map.get(payload, "work_item_id")),
          subject_hash: Map.get(payload, :subject_hash, Map.get(payload, "subject_hash")),
          proposal: nil
        }

      decision_kind ->
        %{kind: decision_kind, work_item_id: nil, subject_hash: nil, proposal: nil}
    end
  end

  defp canonical_command_payload(_kind, payload), do: payload

  defp goal_attrs(mutation_id \\ nil, execution_policy \\ %{}, context_manifest \\ %{}) do
    execution_policy =
      execution_policy
      |> Map.new(fn {key, value} -> {to_string(key), value} end)
      |> Enum.reduce(["budget_limit_microusd", "per_run_cost_limit_microusd"], fn key, policy ->
        case Map.get(policy, key) do
          value when is_integer(value) -> Map.put(policy, key, Integer.to_string(value))
          _ -> policy
        end
      end)

    execution_policy =
      %{
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
      |> Map.merge(execution_policy)

    context_manifest =
      %{
        "byte_budget" => 32_768,
        "required_source_kinds" => ["repository"],
        "include_advisory_recall" => false
      }
      |> Map.merge(context_manifest)

    %{
      schema_version: "symmetry.goal_create.v1",
      title: "Durable goal #{System.unique_integer([:positive])}",
      mutation_id: mutation_id || Ecto.UUID.generate(),
      initial_revision: %{
        objective: "Keep approved work and evidence durable.",
        non_goals: ["No autonomous publication"],
        acceptance_contract: check_contract(),
        authority_policy: %{
          "operator_required_for_scope_change" => true,
          "operator_required_for_completion" => true,
          "publication_allowed" => false,
          "allowed_actions" => []
        },
        execution_policy: execution_policy,
        context_manifest: context_manifest,
        reason: "Initial approved contract"
      }
    }
  end

  defp check_contract do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "An independent check must pass.",
      "predicates" => [%{"id" => "check", "kind" => "check", "validator_profile" => "test"}]
    }
  end

  defp baseline_subject(resource_id) do
    %{
      kind: "subject",
      subject: %{
        "resource_id" => resource_id,
        "commit" => String.duplicate("a", 40),
        "tree_digest" => "sha256:" <> String.duplicate("b", 64)
      }
    }
  end

  defp item_repository_resource_id(item) do
    Map.get(item, :repository_resource_id) ||
      get_in(item, [:baseline, :subject, "resource_id"])
  end

  defp review_contract do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "An independent review must pass.",
      "predicates" => [
        %{"id" => "review", "kind" => "review", "reviewer_profile" => "test-review"}
      ]
    }
  end

  defp check_and_review_contract do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "An independent check and review must pass.",
      "predicates" => [
        %{"id" => "check", "kind" => "check", "validator_profile" => "test"},
        %{"id" => "review", "kind" => "review", "reviewer_profile" => "test-review"}
      ]
    }
  end

  defp check_and_operator_acceptance_contract do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "An independent check and scoped operator acceptance are required.",
      "predicates" => [
        %{"id" => "check", "kind" => "check", "validator_profile" => "test"},
        %{"id" => "operator", "kind" => "operator_acceptance"}
      ]
    }
  end

  defp validation_profiles(
         check_runtime_ids \\ [@validation_runtime_id],
         review_runtime_ids \\ [@validation_runtime_id]
       ) do
    [
      test: [
        kind: :check,
        profile_digest: @validation_profile_digest,
        enabled: true,
        allowed_runtime_ids: check_runtime_ids
      ],
      "test-review": [
        kind: :review,
        profile_digest: @validation_profile_digest,
        enabled: true,
        allowed_runtime_ids: review_runtime_ids
      ]
    ]
  end

  defp project_fixture(default_workspace \\ "primary") do
    {:ok, project} =
      Workspaces.create_project(%{
        name: "Goals #{System.unique_integer([:positive])}",
        key: "G#{System.unique_integer([:positive])}",
        default_agent_profile: "codex",
        default_workspace: default_workspace
      })

    project
  end

  defp repository_fixture(project) do
    {:ok, repository} =
      Workspaces.create_resource(project.id, %{
        kind: "repository",
        name: "Repository #{System.unique_integer([:positive])}"
      })

    repository
  end

  defp assert_branch_target_rejected!(project, repository) do
    attrs =
      goal_attrs(nil, %{
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
          title: "Create the approved provider change",
          description: "Use only the approved repository target.",
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

    assert {:ok, decision, :created} =
             command_current(created.goal.id, "request_decision", %{
               kind: "plan",
               question: "Accept this provider plan?",
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

    assert {:error, :unsupported_capability} =
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

  defp connected_github_repository_fixture(
         project,
         external_ref,
         capabilities \\ ["repositories", "changes"]
       ) do
    assert {:ok, connection} =
             Integrations.create_connection(%{
               provider: "github",
               name: "Goal GitHub #{System.unique_integer([:positive])}",
               account_ref: "acme",
               capabilities: capabilities
             })

    assert {:ok, repository} =
             Workspaces.create_resource(project.id, %{
               connection_id: connection.id,
               kind: "repository",
               name: "Repository #{System.unique_integer([:positive])}",
               external_ref: external_ref
             })

    repository
  end

  defp plan_runtime_fixture(repository) do
    machine =
      %Machine{}
      |> Machine.changeset(%{
        name: "plan-machine-#{System.unique_integer([:positive])}",
        token_digest: :crypto.strong_rand_bytes(32)
      })
      |> Repo.insert!()

    %Runtime{}
    |> Runtime.changeset(%{
      machine_id: machine.id,
      runtime_key: "plan-runtime-#{System.unique_integer([:positive])}",
      name: "Plan runtime",
      daemon_instance_id: Ecto.UUID.generate(),
      connection_epoch: 1,
      capacity: 1,
      agent_profile: "codex",
      workspace: "primary",
      repository_resource_id: repository.id,
      capabilities: %{},
      harness_kind: "codex",
      harness_version: "1.0.0",
      adapter_version: "1.0.0",
      adapter_protocol_version: 1,
      status: "online",
      heartbeat_interval_ms: 5_000
    })
    |> Repo.insert!()
  end

  defp runtime_fixture(runtime_id \\ nil) do
    case runtime_id && Repo.get(Runtime, runtime_id) do
      %Runtime{} = runtime ->
        runtime

      _ ->
        repository_resource_id =
          Repo.one(
            from(item in WorkItem,
              where: not is_nil(item.goal_id) and not is_nil(item.repository_resource_id),
              order_by: [desc: item.inserted_at, desc: item.id],
              limit: 1,
              select: item.repository_resource_id
            )
          )

        machine =
          %Machine{}
          |> Machine.changeset(%{
            name: "goal-machine-#{System.unique_integer([:positive])}",
            token_digest: :crypto.strong_rand_bytes(32)
          })
          |> Repo.insert!()

        if(runtime_id, do: %Runtime{id: runtime_id}, else: %Runtime{})
        |> Runtime.changeset(%{
          machine_id: machine.id,
          runtime_key: "goal-runtime-#{System.unique_integer([:positive])}",
          name: "Goal runtime",
          daemon_instance_id: Ecto.UUID.generate(),
          connection_epoch: 1,
          capacity: 1,
          agent_profile: "codex",
          workspace: "primary",
          repository_resource_id: repository_resource_id,
          capabilities: %{},
          harness_kind: "codex",
          harness_version: "1.0.0",
          adapter_version: "1.0.0",
          adapter_protocol_version: 1,
          status: "online",
          heartbeat_interval_ms: 5_000
        })
        |> Repo.insert!()
    end
  end
end
