defmodule SymmetryControl.Goals.WorkersTest do
  use SymmetryControl.DataCase, async: false

  alias Oban.Job
  alias SymmetryControl.{Goals, Repo, Workspaces}
  alias SymmetryControl.Goals.GoalDecision
  alias SymmetryControl.Goals.Workers.{GoalControlWorker, SettleTaskWorker, WakeupWorker}

  setup do
    previous = Application.fetch_env!(:symmetry_control, :goals)
    Application.put_env(:symmetry_control, :goals, Keyword.put(previous, :rollout_enabled, false))

    on_exit(fn -> Application.put_env(:symmetry_control, :goals, previous) end)
  end

  test "Goal workers cancel malformed jobs regardless of admission rollout state" do
    assert {:cancel, :invalid_settlement_job} = SettleTaskWorker.perform(%Job{args: %{}})

    assert {:cancel, :invalid_wakeup_job} =
             WakeupWorker.perform(%Job{args: %{"unexpected" => true}})

    assert {:cancel, :invalid_goal_control_job} = GoalControlWorker.perform(%Job{args: %{}})
  end

  test "wakeup completes an ordinary noop reconciliation without retrying it" do
    goal = active_goal()

    assert :ok = WakeupWorker.perform(%Job{args: %{"goal_id" => goal.id}})
  end

  test "control completes a superseded Goal action without retrying it" do
    goal = active_goal()

    assert :ok =
             GoalControlWorker.perform(%Job{
               args: %{
                 "goal_id" => goal.id,
                 "revision" => goal.current_revision,
                 "action_id" => Ecto.UUID.generate()
               }
             })
  end

  test "settlement jobs are uniquely identified by the fenced execution identity" do
    opts = SettleTaskWorker.__opts__()

    assert opts[:queue] == :goal_settlement
    assert opts[:max_attempts] == 5
    assert SettleTaskWorker.timeout(%Job{}) == :timer.seconds(30)

    assert opts[:unique] == [
             period: {10, :minutes},
             fields: [:worker, :args],
             keys: [:task_id, :run_id, :generation],
             states: :incomplete
           ]
  end

  test "wakeup jobs are uniquely identified by the goal and have bounded retries" do
    opts = WakeupWorker.__opts__()

    assert opts[:queue] == :goal_wakeup
    assert opts[:max_attempts] == 5
    assert WakeupWorker.timeout(%Job{}) == :timer.seconds(30)

    assert opts[:unique] == [
             period: {10, :minutes},
             fields: [:worker, :args],
             keys: [:goal_id],
             states: :incomplete
           ]
  end

  test "Goal control jobs are uniquely identified by the accepted Goal action" do
    opts = GoalControlWorker.__opts__()

    assert opts[:queue] == :goal_control
    assert opts[:max_attempts] == 5
    assert GoalControlWorker.timeout(%Job{}) == :timer.seconds(30)

    assert opts[:unique] == [
             period: {10, :minutes},
             fields: [:worker, :args],
             keys: [:goal_id, :revision, :action_id],
             states: :incomplete
           ]
  end

  defp active_goal do
    {:ok, project} =
      Workspaces.create_project(%{
        name: "Worker goal #{System.unique_integer([:positive])}",
        key: "WG#{System.unique_integer([:positive])}",
        default_agent_profile: "codex",
        default_workspace: "primary"
      })

    {:ok, created, :created} =
      Goals.create_goal(project.id, goal_attrs(), "operator:test",
        validation_profiles: validation_profiles()
      )

    {:ok, repository} =
      Workspaces.create_resource(project.id, %{
        kind: "repository",
        name: "Worker repository #{System.unique_integer([:positive])}"
      })

    proposal = %{
      schema_version: "symmetry.plan.v1",
      proposal_id: Ecto.UUID.generate(),
      goal_id: created.goal.id,
      expected_revision: created.goal.current_revision,
      items: [
        %{
          key: "worker-reconciliation",
          title: "Worker reconciliation",
          description: "Provides valid admitted Goal work before activation.",
          required: true,
          repository_resource_id: repository.id,
          acceptance: acceptance_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: %{
            kind: "subject",
            subject: %{
              "resource_id" => repository.id,
              "commit" => String.duplicate("a", 40),
              "tree_digest" => "sha256:" <> String.duplicate("4", 64)
            }
          }
        }
      ]
    }

    {:ok, decision, :created} =
      command_current(created.goal.id, "request_decision", %{
        kind: "plan",
        work_item_id: nil,
        subject_hash: nil,
        proposal: proposal
      })

    decision_id = decision.response["decision"]["id"]

    {:ok, _resolved, :created} =
      command_current(created.goal.id, "resolve_decision", %{
        decision_id: decision_id,
        expected_decision_version: Repo.get!(GoalDecision, decision_id).lock_version,
        option_id: "accept"
      })

    {:ok, _planned, :created} =
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

    assert {:ok, activated, :created} =
             command_current(created.goal.id, "activate", %{
               approved_revision: created.goal.current_revision
             })

    activated.goal
  end

  defp command_current(goal_id, kind, payload, opts \\ []) do
    {:ok, goal} = Goals.fetch_goal(goal_id)
    opts = Keyword.put_new(opts, :validation_profiles, validation_profiles())

    Goals.command(
      goal_id,
      %{
        schema_version: "symmetry.goal_command.v1",
        mutation_id: Ecto.UUID.generate(),
        expected_version: goal.version,
        expected_revision: goal.current_revision,
        kind: kind,
        payload: payload
      },
      "operator:test",
      opts
    )
  end

  defp validation_profiles do
    [
      test: [
        kind: :check,
        profile_digest: "sha256:" <> String.duplicate("c", 64),
        enabled: true,
        allowed_runtime_ids: [Ecto.UUID.generate()]
      ]
    ]
  end

  defp goal_attrs do
    %{
      schema_version: "symmetry.goal_create.v1",
      title: "Durable worker goal",
      mutation_id: Ecto.UUID.generate(),
      initial_revision: %{
        objective: "Keep worker activity bounded and durable.",
        non_goals: ["No autonomous publication"],
        acceptance_contract: %{
          "schema_version" => "symmetry.acceptance.v1",
          "description" => "An independent check must pass.",
          "predicates" => [%{"id" => "check", "kind" => "check", "validator_profile" => "test"}]
        },
        authority_policy: %{
          "operator_required_for_scope_change" => true,
          "operator_required_for_completion" => true,
          "publication_allowed" => false,
          "allowed_actions" => []
        },
        execution_policy: %{
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
        },
        context_manifest: %{
          "byte_budget" => 32_768,
          "required_source_kinds" => ["approved_goal", "work_contract", "repository_subject"],
          "include_advisory_recall" => false
        },
        reason: "Initial approved contract"
      }
    }
  end

  defp acceptance_contract do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "An independent check must pass.",
      "predicates" => [%{"id" => "check", "kind" => "check", "validator_profile" => "test"}]
    }
  end
end
