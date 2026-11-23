Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.CommandsFlowTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Goals.WorkDependency
  alias SymmetryControl.Goals.Workers.GoalControlWorker
  alias SymmetryControl.Orchestration
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces.WorkItem

  @now ~U[2026-09-09 00:00:00.000000Z]

  use SymmetryControl.TestFixtures,
    command_opts: [now: @now, validation_profiles: validation_profiles()]

  setup :put_goal_validation_profiles

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

    put_run_task_result!(run, task_result(task, "plan_proposed", %{proposal: proposal}))

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

  test "plan preflight requires an integration designation for multiple required items and a same-resource dependency baseline" do
    project = project_fixture()
    first_repository = repository_fixture(project)
    second_repository = repository_fixture(project)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

    no_designation =
      plan_proposal(created.goal.id, [
        %{
          key: "first-required",
          title: "First required work",
          description: "Multiple required items still need an explicit integration designation.",
          required: true,
          integration: false,
          repository_resource_id: first_repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: baseline_subject(first_repository.id)
        },
        %{
          key: "second-required",
          title: "Second required work",
          description: "Multiple required items still need an explicit integration designation.",
          required: true,
          integration: false,
          repository_resource_id: first_repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: baseline_subject(first_repository.id)
        }
      ])

    assert :invalid_plan == reject_plan_admission!(created.goal.id, no_designation)

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

  test "plan admission binds the single required WorkItem without an integration designation" do
    project = project_fixture()
    repository = repository_fixture(project)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

    goal_id = created.goal.id

    proposal =
      plan_proposal(goal_id, [
        %{
          key: "optional-before",
          title: "Optional preparation",
          description: "Optional work outside the completion contract.",
          required: false,
          integration: false,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: baseline_subject(repository.id)
        },
        %{
          key: "only-required",
          title: "Only required work",
          description: "The contract has exactly one required work item.",
          required: true,
          integration: false,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: baseline_subject(repository.id)
        },
        %{
          key: "optional-after",
          title: "Optional follow-up",
          description: "Optional work outside the completion contract.",
          required: false,
          integration: false,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: baseline_subject(repository.id)
        }
      ])

    assert {:ok, decision, :created} =
             command_current(goal_id, "request_decision", %{
               kind: "plan",
               question: "Accept the single-required plan?",
               options: [
                 %{"id" => "accept", "label" => "Accept", "consequence" => "Admit the plan"}
               ],
               proposal: proposal
             })

    decision_id = decision.response["decision"]["id"]

    resolve_decision!(goal_id, decision_id)

    authorized_hash =
      "sha256:" <> Base.encode16(SymmetryControl.RequestHash.canonical(proposal), case: :lower)

    accept_command =
      goal_command(fetch_goal!(goal_id), "accept_plan", %{
        proposal: proposal,
        proposal_hash: authorized_hash,
        decision_id: decision_id
      })

    assert {:ok, planned, :created} =
             Goals.command(goal_id, accept_command, "operator:test",
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert length(planned.response["plan"]["work_item_ids"]) == 3

    items_by_title = Map.new(fetch_goal!(goal_id).work_items, &{&1.title, &1})

    assert Map.fetch!(items_by_title, "Only required work").required?
    assert Map.fetch!(items_by_title, "Only required work").integration
    refute Map.fetch!(items_by_title, "Optional preparation").integration
    refute Map.fetch!(items_by_title, "Optional follow-up").integration

    assert {:ok, replayed, :replayed} =
             Goals.command(goal_id, accept_command, "operator:test",
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert replayed == planned

    tampered = put_in(proposal, [:items, Access.at(1), :title], "Renamed required work")

    assert {:error, :invalid_decision} =
             command_current(
               goal_id,
               "accept_plan",
               %{
                 proposal: tampered,
                 proposal_hash:
                   "sha256:" <>
                     Base.encode16(SymmetryControl.RequestHash.canonical(tampered), case: :lower),
                 decision_id: decision_id
               },
               rollout_enabled: true
             )

    assert length(fetch_goal!(goal_id).work_items) == 3
  end

  test "plan admission refuses a conflicting optional integration designation" do
    project = project_fixture()
    repository = repository_fixture(project)

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(), "operator:test", now: @now)

    proposal =
      plan_proposal(created.goal.id, [
        %{
          key: "only-required",
          title: "Only required work",
          description: "The single required item carries the integration role.",
          required: true,
          integration: false,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: baseline_subject(repository.id)
        },
        %{
          key: "optional-integration",
          title: "Optional integration candidate",
          description: "An optional item cannot take over the integration role.",
          required: false,
          integration: true,
          repository_resource_id: repository.id,
          acceptance: check_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: baseline_subject(repository.id)
        }
      ])

    assert :invalid_plan == reject_plan_admission!(created.goal.id, proposal)

    assert Repo.aggregate(WorkItem, :count) == 0
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

    _planned =
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

    goal = fetch_goal!(created.goal.id)
    items_by_title = Map.new(goal.work_items, &{&1.title, &1})

    assert Enum.map(goal.work_items, & &1.repository_resource_id) |> Enum.sort() ==
             [consumer_repository.id, source_repository.id]

    assert Repo.one!(
             from(dependency in WorkDependency,
               where: dependency.work_item_id == ^items_by_title["Consumer work"].id,
               select: dependency.depends_on_id
             )
           ) == items_by_title["Source work"].id
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

    put_run_task_result!(run, task_result(task, "plan_proposed", %{proposal: proposal}))

    assert {:ok, receipt} = Goals.settle_task(task.id, run.id, 1, now: @now)
    assert receipt["settlement"] == "historical_result"
    assert receipt["decision_id"] == nil
    assert amended.goal.current_revision == 2
    assert Repo.aggregate(WorkItem, :count) == 0
    assert Repo.aggregate(SymmetryControl.Goals.GoalDecision, :count) == 0
  end

  test "a Goal-level operator acceptance predicate is satisfied by the operator achieve" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} =
      independent_validation_attempt_fixture(check_contract(), %{
        acceptance_contract: check_and_operator_acceptance_contract()
      })

    {evidence, stored_evidence} =
      append_validation_evidence_and_settle_accepted!(
        validation,
        validation_run,
        validation_runtime,
        validation_fence,
        item,
        candidate_subject,
        assert_reason?: true
      )

    refute Repo.exists?(
             from(decision in SymmetryControl.Goals.GoalDecision,
               where:
                 decision.goal_id == ^goal.id and
                   decision.goal_revision == ^goal.current_revision and
                   decision.kind == "completion"
             )
           )

    pre_achieve = fetch_goal!(goal.id)

    assert pre_achieve.completion.ready?
    assert "achieve" in pre_achieve.allowed_actions
    refute "final_acceptance_required" in pre_achieve.blocker_reasons

    assert {:ok, achieved, :created} =
             Goals.command(
               goal.id,
               goal_command(fetch_goal!(goal.id), "achieve", %{
                 subject: evidence.subject,
                 integration_work_item_id: item.id,
                 evidence_ids: [stored_evidence.id],
                 decision_id: nil
               }),
               "portal_operator",
               now: @now,
               validation_profiles: validation_profiles()
             )

    assert achieved.goal.state == "achieved"
  end

  test "a single-required plan admitted without an integration designation achieves without the payload field" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} =
      independent_validation_attempt_fixture(check_contract(), %{plan_integration: false})

    {evidence, stored_evidence} =
      append_validation_evidence_and_settle_accepted!(
        validation,
        validation_run,
        validation_runtime,
        validation_fence,
        item,
        candidate_subject
      )

    refute Repo.exists?(
             from(decision in SymmetryControl.Goals.GoalDecision,
               where:
                 decision.goal_id == ^goal.id and
                   decision.goal_revision == ^goal.current_revision and
                   decision.kind == "completion"
             )
           )

    projection = fetch_goal!(goal.id)

    assert Enum.find(projection.work_items, &(&1.id == item.id)).integration
    assert projection.completion.ready?
    assert "achieve" in projection.allowed_actions

    achieve_command =
      goal_command(fetch_goal!(goal.id), "achieve", %{
        subject: evidence.subject,
        evidence_ids: [stored_evidence.id],
        decision_id: nil
      })

    assert {:ok, achieved, :created} =
             Goals.command(goal.id, achieve_command, "operator:test",
               now: @now,
               validation_profiles: validation_profiles()
             )

    assert achieved.goal.state == "achieved"

    achieve_event_count =
      Repo.aggregate(
        from(event in SymmetryControl.Goals.GoalEvent,
          where: event.goal_id == ^goal.id and event.kind == "achieve"
        ),
        :count
      )

    assert achieve_event_count == 1

    assert {:ok, replayed, :replayed} =
             Goals.command(goal.id, achieve_command, "operator:test",
               now: @now,
               validation_profiles: validation_profiles()
             )

    assert replayed == achieved

    conflicting_achieve_command = put_in(achieve_command, [:payload, :evidence_ids], [])

    assert {:error, :idempotency_conflict} =
             Goals.command(goal.id, conflicting_achieve_command, "operator:test",
               now: @now,
               validation_profiles: validation_profiles()
             )

    assert Repo.aggregate(
             from(event in SymmetryControl.Goals.GoalEvent,
               where: event.goal_id == ^goal.id and event.kind == "achieve"
             ),
             :count
           ) == achieve_event_count
  end

  test "a legacy unmarked single-required plan achieves without the payload field" do
    {goal, item, _producer, _producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject} =
      legacy_unmarked_validation_attempt_fixture()

    {evidence, stored_evidence} =
      append_validation_evidence_and_settle_accepted!(
        validation,
        validation_run,
        validation_runtime,
        validation_fence,
        item,
        candidate_subject
      )

    projection = fetch_goal!(goal.id)

    # The required-count rule is derived; the legacy row keeps its false marker.
    refute Enum.find(projection.work_items, &(&1.id == item.id)).integration
    assert projection.completion.ready?
    assert "achieve" in projection.allowed_actions

    assert {:ok, achieved, :created} =
             Goals.command(
               goal.id,
               goal_command(fetch_goal!(goal.id), "achieve", %{
                 subject: evidence.subject,
                 evidence_ids: [stored_evidence.id],
                 decision_id: nil
               }),
               "operator:test",
               now: @now,
               validation_profiles: validation_profiles()
             )

    assert achieved.goal.state == "achieved"
    refute Repo.get!(WorkItem, item.id).integration
  end

  test "deterministic final acceptance rejects an operator-only predicate" do
    project = project_fixture()

    attrs =
      goal_attrs(nil, %{"final_acceptance" => "deterministic"})
      |> put_in(
        [:initial_revision, :authority_policy, "operator_required_for_completion"],
        false
      )
      |> put_in(
        [:initial_revision, :acceptance_contract],
        check_and_operator_acceptance_contract()
      )

    assert {:error, {:invalid_contract, :deterministic_acceptance_contract}} =
             Goals.create_goal(project.id, attrs, "operator:test", now: @now)
  end

  test "Goal control retry recovers a committed runless cancellation before settlement" do
    {goal, _item, task} = admitted_task_fixture()
    enable_rollout!()

    assert {:ok, cancelled, :created} = command_current(goal.id, "cancel", %{reason: "stop"})
    action_id = cancelled.response["control_action_id"]
    idempotency_key = "goal-control:#{action_id}:cancel:#{task.id}"

    assert {:ok, command, :created} =
             Goals.dispatch_goal_control_command(
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

    assert_runless_cancellation_landed!(goal, task, job)
  end

  test "Goal resume waits for an old amendment cancellation to settle" do
    {goal, _item, task} = admitted_task_fixture()
    enable_rollout!()

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
             Goals.dispatch_goal_control_command(
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
    enable_rollout!()

    assert {:ok, first_amendment, :created} =
             command_current(goal.id, "amend", %{
               revision_contract: amended_revision_contract("First amendment."),
               reason: "First scope change"
             })

    first_action_id = first_amendment.response["control_action_id"]

    assert {:ok, _command, :created} =
             Goals.dispatch_goal_control_command(
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

    assert_runless_cancellation_landed!(goal, task, job)
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

  test "automatic execution queues an unstarted root until a matching runtime arrives" do
    project = project_fixture()
    repository = repository_fixture(project)

    automatic_policy = %{
      "automatic_execution" => true,
      "budget_limit_microusd" => 1_000_000,
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

    _planned = accept_plan!(created.goal.id, proposal)
    [item] = fetch_goal!(created.goal.id).work_items

    assert {:ok, _active, :created} =
             command_current(created.goal.id, "activate", %{approved_revision: 1})

    set_goal_due!(created.goal.id)

    assert {:ok, reconciliations} =
             Goals.reconcile_due_goals(
               goal_ids: [created.goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert [%{disposition: :admitted, purpose: "implement"}] =
             Enum.filter(reconciliations, &(&1.disposition == :admitted))

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
      "max_parallel_tasks" => 2,
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

    assert Enum.count(admissions, &(&1.disposition == :admitted)) == 2

    assert Enum.all?(
             Enum.filter(admissions, &(&1.disposition == :admitted)),
             &(&1.purpose == "implement")
           )

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^created.goal.id), :count) ==
             2

    assert Repo.aggregate(
             from(task in Task,
               where: task.goal_id == ^created.goal.id and task.state == "queued"
             ),
             :count
           ) == 2

    set_goal_due!(created.goal.id)

    assert {:ok, [%{disposition: :deferred, reason: "admission_capacity_unavailable"}]} =
             Goals.reconcile_due_goals(
               goal_ids: [created.goal.id],
               now: @now,
               rollout_enabled: true,
               validation_profiles: validation_profiles()
             )

    assert Repo.aggregate(from(task in Task, where: task.goal_id == ^created.goal.id), :count) ==
             2
  end

  test "resume admission accepts a verified available session while its runtime is offline" do
    {goal, item, task} = admitted_task_fixture()
    complete_task_and_release_reservation!(goal, task)
    goal = fetch_goal!(goal.id)
    runtime = runtime_fixture()

    Repo.update_all(
      from(row in Runtime, where: row.id == ^runtime.id),
      set: [
        status: "offline",
        capabilities: %{"adapter" => %{"operations" => %{"resume" => true}}}
      ]
    )

    retained = available_session_fixture(runtime, item, session_attrs(item, "workspace-offline"))
    binding_id = retained.binding_id

    request =
      goal_command(
        goal,
        "admit_task",
        admission_payload(item, %{
          session_mode: "resume",
          requested_session_id: retained.id
        })
      )

    counts_before = %{
      snapshots: Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count),
      tasks: Repo.aggregate(Task, :count),
      reservations: Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count),
      events: Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count)
    }

    assert {:ok, admitted, :created} =
             Goals.command(goal.id, request, "operator:test", now: @now, rollout_enabled: true)

    admitted_task = Repo.get!(Task, admitted.response["task"]["id"])
    assert admitted_task.state == "queued"
    assert admitted_task.requested_session_id == retained.id
    assert admitted_task.input["session_mode"] == "resume"
    assert admitted_task.input["requested_session_id"] == retained.id

    counts_after = %{
      snapshots: Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count),
      tasks: Repo.aggregate(Task, :count),
      reservations: Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count),
      events: Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count)
    }

    assert counts_after == %{
             snapshots: counts_before.snapshots + 1,
             tasks: counts_before.tasks + 1,
             reservations: counts_before.reservations + 1,
             events: counts_before.events + 1
           }

    assert %{
             binding_id: ^binding_id,
             binding_verified: true,
             state: "available",
             active_run_id: nil
           } =
             Repo.get!(HarnessSession, retained.id)

    assert {:ok, replayed, :replayed} =
             Goals.command(goal.id, request, "operator:test", now: @now, rollout_enabled: true)

    assert replayed == admitted

    assert counts_after == %{
             snapshots: Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count),
             tasks: Repo.aggregate(Task, :count),
             reservations: Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count),
             events: Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count)
           }

    assert %{binding_id: ^binding_id, state: "available", active_run_id: nil} =
             Repo.get!(HarnessSession, retained.id)
  end

  test "request_plan resume admission accepts a verified available session while its runtime is offline" do
    project = project_fixture()
    repository = repository_fixture(project)

    assert {:ok, created, :created} =
             Goals.create_goal(
               project.id,
               goal_attrs(nil, %{"max_task_admissions" => 2}),
               "operator:test",
               now: @now
             )

    runtime = plan_runtime_fixture(repository)

    Repo.update_all(
      from(row in Runtime, where: row.id == ^runtime.id),
      set: [
        status: "offline",
        capabilities: %{"adapter" => %{"operations" => %{"resume" => true}}}
      ]
    )

    session_attrs = %{
      local_handle_id: Ecto.UUID.generate(),
      harness_kind: "codex",
      harness_version: runtime.harness_version,
      adapter_version: runtime.adapter_version,
      workspace_fingerprint: "workspace-plan-offline",
      workspace: "primary",
      repository_resource_id: repository.id
    }

    retained =
      %HarnessSession{}
      |> HarnessSession.changeset(%{
        machine_id: runtime.machine_id,
        runtime_id: runtime.id,
        repository_resource_id: repository.id,
        local_handle_id: session_attrs.local_handle_id,
        binding_id: Ecto.UUID.generate(),
        binding_verified: true,
        harness_kind: session_attrs.harness_kind,
        harness_version: session_attrs.harness_version,
        adapter_version: session_attrs.adapter_version,
        workspace_fingerprint: session_attrs.workspace_fingerprint,
        state: "available"
      })
      |> Repo.insert!()

    binding_id = retained.binding_id
    subject = baseline_subject(repository.id).subject

    request =
      goal_command(created.goal, "request_plan", %{
        model_profile: "codex",
        repository_resource_id: repository.id,
        subject: subject,
        session_mode: "resume",
        requested_session_id: retained.id
      })

    counts_before = %{
      snapshots: Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count),
      tasks: Repo.aggregate(Task, :count),
      reservations: Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count),
      events: Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count)
    }

    assert {:ok, admitted, :created} =
             Goals.command(created.goal.id, request, "operator:test",
               now: @now,
               rollout_enabled: true
             )

    admitted_task = Repo.get!(Task, admitted.response["task"]["id"])
    assert admitted_task.state == "queued"
    assert admitted_task.purpose == "plan"
    assert admitted_task.requested_session_id == retained.id
    assert admitted_task.input["session_mode"] == "resume"
    assert admitted_task.input["requested_session_id"] == retained.id

    counts_after = %{
      snapshots: Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count),
      tasks: Repo.aggregate(Task, :count),
      reservations: Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count),
      events: Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count)
    }

    assert counts_after == %{
             snapshots: counts_before.snapshots + 1,
             tasks: counts_before.tasks + 1,
             reservations: counts_before.reservations + 1,
             events: counts_before.events + 1
           }

    assert %{
             binding_id: ^binding_id,
             binding_verified: true,
             state: "available",
             active_run_id: nil
           } =
             Repo.get!(HarnessSession, retained.id)

    assert {:ok, replayed, :replayed} =
             Goals.command(created.goal.id, request, "operator:test",
               now: @now,
               rollout_enabled: true
             )

    assert replayed == admitted

    assert counts_after == %{
             snapshots: Repo.aggregate(SymmetryControl.Goals.ContextSnapshot, :count),
             tasks: Repo.aggregate(Task, :count),
             reservations: Repo.aggregate(SymmetryControl.Goals.GoalBudgetReservation, :count),
             events: Repo.aggregate(SymmetryControl.Goals.GoalEvent, :count)
           }

    assert %{binding_id: ^binding_id, state: "available", active_run_id: nil} =
             Repo.get!(HarnessSession, retained.id)
  end

  defp legacy_unmarked_work_item!(project, repository, goal, title) do
    insert_goal_work_item!(
      %{
        project_id: project.id,
        repository_resource_id: repository.id,
        title: title,
        description: "Required work admitted before the integration derivation.",
        status: "backlog",
        priority: "no_priority",
        position: 0,
        assignee_type: "agent",
        assignee_name: "codex",
        agent_profile: "codex",
        workspace: project.default_workspace,
        blocked: false
      },
      %{
        goal_id: goal.id,
        admitted_revision: goal.current_revision,
        required: true,
        integration: false,
        acceptance_contract: check_contract(),
        baseline_subject: baseline_subject(repository.id).subject,
        baseline_dependency_id: nil,
        change_target: nil
      }
    )
  end

  defp legacy_unmarked_validation_attempt_fixture do
    project = project_fixture()
    repository = repository_fixture(project)

    execution_policy = %{
      "max_task_admissions" => 4,
      "budget_limit_microusd" => 42,
      "allowed_resource_ids" => [repository.id],
      "allowed_model_profiles" => ["codex"]
    }

    assert {:ok, created, :created} =
             Goals.create_goal(
               project.id,
               goal_attrs(nil, execution_policy),
               "operator:test",
               now: @now
             )

    item = legacy_unmarked_work_item!(project, repository, created.goal, "Legacy required work")

    assert {:ok, _active, :created} =
             command_current(created.goal.id, "activate", %{approved_revision: 1})

    {:ok, goal} = Goals.fetch_goal(created.goal.id)

    assert {:ok, admitted, :created} =
             command_current(
               created.goal.id,
               "admit_task",
               admission_payload(item),
               rollout_enabled: true
             )

    producer = Repo.get!(Task, admitted.response["task"]["id"])

    candidate_subject = candidate_subject(repository.id)

    {producer_run, validation, validation_run, validation_runtime, validation_fence} =
      settled_validation_attempt!(goal, item, producer, candidate_subject)

    {goal, item, producer, producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject}
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

  defp reject_plan_admission!(goal_id, proposal, opts \\ []) do
    assert {:ok, decision, :created} =
             command_current(goal_id, "request_decision", %{
               kind: "plan",
               question: "Accept this plan?",
               options: [%{"id" => "accept", "label" => "Accept", "consequence" => "Proceed"}],
               proposal: proposal
             })

    decision_id = decision.response["decision"]["id"]

    resolve_decision!(goal_id, decision_id)

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

  defp plan_runtime_fixture(repository),
    do: runtime_fixture(nil, repository_resource_id: repository.id)

  defp assert_runless_cancellation_landed!(goal, task, job) do
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
end
