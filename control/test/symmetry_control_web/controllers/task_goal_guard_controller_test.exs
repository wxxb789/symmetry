defmodule SymmetryControlWeb.TaskGoalGuardControllerTest do
  use SymmetryControlWeb.ConnCase, async: false

  alias SymmetryControl.Goals
  alias SymmetryControl.Repo
  alias SymmetryControl.RequestHash
  alias SymmetryControl.Workspaces

  @operator_token "test-operator-token"

  test "a queued Goal Task cannot be cancelled through the legacy Task command endpoint", %{
    conn: conn
  } do
    task_id = admitted_goal_task()

    assert_error(
      operator_conn(conn)
      |> put_req_header("idempotency-key", Ecto.UUID.generate())
      |> post("/api/v1/tasks/#{task_id}/commands", %{"kind" => "cancel"}),
      403,
      "goal_authority_required"
    )
  end

  test "goal-less Task commands retain their legacy idempotency and state conflict behavior", %{
    conn: conn
  } do
    %{"task_id" => task_id} =
      operator_conn(conn)
      |> put_req_header("idempotency-key", Ecto.UUID.generate())
      |> post("/api/v1/tasks", %{
        "work" => %{
          "goal" => "Preserve legacy Task command behavior",
          "agent_profile" => "codex",
          "workspace" => "primary",
          "input" => %{}
        }
      })
      |> json_response(201)

    assert %{"kind" => "cancel", "state" => "applied"} =
             operator_conn(conn)
             |> put_req_header("idempotency-key", Ecto.UUID.generate())
             |> post("/api/v1/tasks/#{task_id}/commands", %{"kind" => "cancel"})
             |> json_response(201)

    assert_error(
      operator_conn(conn)
      |> put_req_header("idempotency-key", Ecto.UUID.generate())
      |> post("/api/v1/tasks/#{task_id}/commands", %{"kind" => "cancel"}),
      409,
      "state_conflict"
    )
  end

  defp admitted_goal_task do
    {:ok, project} =
      Workspaces.create_project(%{
        name: "Goal Task Guard #{System.unique_integer([:positive])}",
        key: "TG#{System.unique_integer([:positive])}",
        default_agent_profile: "codex",
        default_workspace: "primary"
      })

    {:ok, repository} =
      Workspaces.create_resource(project.id, %{
        kind: "repository",
        name: "Goal Task Guard repository #{System.unique_integer([:positive])}"
      })

    assert {:ok, created, :created} =
             Goals.create_goal(project.id, goal_attrs(repository.id), "operator:test",
               validation_profiles: validation_profiles()
             )

    goal_id = created.goal.id
    proposal = proposal(goal_id, repository.id)

    assert {:ok, decision, :created} =
             command_current(goal_id, "request_decision", %{
               kind: "plan",
               work_item_id: nil,
               subject_hash: nil,
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
                   "sha256:" <> Base.encode16(RequestHash.canonical(proposal), case: :lower),
                 decision_id: decision_id
               },
               rollout_enabled: true
             )

    assert {:ok, _active, :created} =
             command_current(goal_id, "activate", %{approved_revision: 1})

    assert {:ok, admitted, :created} =
             command_current(
               goal_id,
               "admit_task",
               %{
                 work_item_id: goal_work_item_id(goal_id),
                 purpose: "implement",
                 model_profile: "codex",
                 session_mode: "fresh",
                 requested_session_id: nil,
                 validation_of_task_id: nil
               },
               rollout_enabled: true
             )

    admitted.response["task"]["id"]
  end

  defp command_current(goal_id, kind, payload, opts \\ []) do
    assert {:ok, goal} = Goals.fetch_goal(goal_id)
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

  defp goal_work_item_id(goal_id) do
    assert {:ok, %{work_items: [item]}} = Goals.fetch_goal(goal_id)
    item.id
  end

  defp goal_attrs(repository_id) do
    %{
      schema_version: "symmetry.goal_create.v1",
      title: "Legacy Task guard Goal",
      mutation_id: Ecto.UUID.generate(),
      initial_revision: %{
        objective: "Keep Goal Task control on the Goal authority path.",
        non_goals: [],
        acceptance_contract: acceptance_contract(),
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
          "allowed_resource_ids" => [repository_id],
          "allowed_model_profiles" => ["codex"],
          "final_acceptance" => "operator",
          "allowed_actions" => []
        },
        context_manifest: %{
          "byte_budget" => 32_768,
          "required_source_kinds" => ["approved_goal", "work_contract", "repository_subject"],
          "include_advisory_recall" => false
        },
        reason: "Initial Task control contract"
      }
    }
  end

  defp proposal(goal_id, repository_id) do
    %{
      schema_version: "symmetry.plan.v1",
      proposal_id: Ecto.UUID.generate(),
      goal_id: goal_id,
      expected_revision: 1,
      items: [
        %{
          key: "implement",
          title: "Protect Task commands",
          description: "No legacy Goal Task commands",
          required: true,
          repository_resource_id: repository_id,
          acceptance: acceptance_contract(),
          depends_on_keys: [],
          model_profile: "codex",
          baseline: %{kind: "subject", subject: subject(repository_id)}
        }
      ]
    }
  end

  defp acceptance_contract do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "A separate check is required.",
      "predicates" => [%{"id" => "check", "kind" => "check", "validator_profile" => "test"}]
    }
  end

  defp subject(repository_id) do
    %{
      "resource_id" => repository_id,
      "commit" => String.duplicate("a", 40),
      "tree_digest" => "sha256:" <> String.duplicate("4", 64)
    }
  end

  defp operator_conn(conn),
    do: put_req_header(conn, "authorization", "Bearer " <> @operator_token)

  defp assert_error(conn, status, code) do
    assert %{"error" => %{"code" => ^code}} = json_response(conn, status)
  end
end
