defmodule SymmetryControl.GoalsTestSupport do
  import Ecto.Query
  import ExUnit.Assertions
  import ExUnit.Callbacks

  @now ~U[2026-09-09 00:00:00.000000Z]
  @validation_runtime_id "00000000-0000-4000-8000-000000000001"
  @validation_profile_digest "sha256:" <> String.duplicate("c", 64)

  use SymmetryControl.TestFixtures,
    command_opts: [now: @now, validation_profiles: validation_profiles()]

  alias SymmetryControl.Goals
  alias SymmetryControl.Goals.HarnessSession
  alias SymmetryControl.Integrations
  alias SymmetryControl.Orchestration.Machine
  alias SymmetryControl.Orchestration.Run
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Repo
  alias SymmetryControl.Workspaces
  alias SymmetryControl.Workspaces.WorkItem

  def put_goal_validation_profiles(_context) do
    previous_goals = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(previous_goals, :validation_profiles, validation_profiles())
    )

    on_exit(fn -> Application.put_env(:symmetry_control, :goals, previous_goals) end)
    :ok
  end

  def enable_rollout! do
    previous_goals = Application.fetch_env!(:symmetry_control, :goals)

    Application.put_env(
      :symmetry_control,
      :goals,
      Keyword.put(previous_goals, :rollout_enabled, true)
    )

    on_exit(fn -> Application.put_env(:symmetry_control, :goals, previous_goals) end)
  end

  def goal_with_items_fixture(execution_policy, item_count \\ 1) do
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

  def insert_goal_work_item!(work_item_attrs, membership_attrs) do
    %WorkItem{id: Ecto.UUID.generate()}
    |> WorkItem.changeset(work_item_attrs)
    |> Ecto.Changeset.apply_changes()
    |> WorkItem.goal_membership_changeset(membership_attrs)
    |> Repo.insert!()
  end

  defp put_goal_acceptance_contract(attrs, nil), do: attrs

  defp put_goal_acceptance_contract(attrs, contract),
    do: put_in(attrs, [:initial_revision, :acceptance_contract], contract)

  def admitted_task_fixture(
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

    authority_policy =
      revision_overrides
      |> Map.get(:authority_policy, %{})
      |> Map.new(fn {key, value} -> {to_string(key), value} end)

    attrs =
      goal_attrs(nil, execution_policy, context_manifest)
      |> update_in(
        [:initial_revision, :authority_policy],
        &Map.merge(&1, authority_policy)
      )
      |> put_goal_acceptance_contract(Map.get(revision_overrides, :acceptance_contract))

    assert {:ok, created, :created} =
             Goals.create_goal(
               project.id,
               attrs,
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
          integration: Map.get(revision_overrides, :plan_integration, true),
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

    resolve_decision!(goal_id, decision_id)

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

    [item] = fetch_goal!(goal_id).work_items

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
                 admission_payload(item),
                 admission_opts
               )

      {goal, item, Repo.get!(Task, admitted.response["task"]["id"])}
    else
      {goal, item, nil}
    end
  end

  def claimed_goal_run_fixture(
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

  def completed_goal_run_fixture(task, runtime, candidate_subject \\ nil) do
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

  def insert_accepted_work_outcome!(goal, work_item_id, run, subject, evidence_ids \\ []) do
    %SymmetryControl.Goals.WorkOutcome{}
    |> SymmetryControl.Goals.WorkOutcome.changeset(%{
      goal_id: goal.id,
      goal_revision: goal.current_revision,
      work_item_id: work_item_id,
      producing_task_id: run.task_id,
      producing_run_id: run.id,
      validation_task_id: nil,
      candidate_subject: subject,
      subject_hash: SymmetryControl.RequestHash.canonical(subject),
      producing_result_id: run.result["task_result"]["result_id"],
      evidence_ids: evidence_ids,
      disposition: "accepted",
      reason: "validation_passed",
      decision_id: nil
    })
    |> Repo.insert!()
  end

  def append_validation_evidence_and_settle_accepted!(
        validation,
        validation_run,
        validation_runtime,
        validation_fence,
        item,
        candidate_subject,
        opts \\ []
      ) do
    evidence = evidence_attrs(validation_run.id, item, candidate_subject)

    assert {:ok, %{evidence: stored_evidence}, :created} =
             Goals.append_evidence(
               validation_runtime.machine_id,
               validation_run.id,
               validation_fence,
               evidence,
               now: @now
             )

    assert {:ok, settlement} = Goals.settle_task(validation.id, validation_run.id, 1, now: @now)
    assert settlement["settlement"] == "accepted"

    if Keyword.get(opts, :assert_reason?, false) do
      assert settlement["reason"] == "validation_passed"
    end

    {evidence, stored_evidence}
  end

  def running_goal_run_fixture(task, runtime) do
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

    {run,
     %{
       runtime_id: runtime.id,
       runtime_epoch: runtime.connection_epoch,
       generation: 1,
       claim_id: claim_id,
       lease_token: lease_token
     }}
  end

  def settled_validation_attempt!(goal, item, producer, candidate_subject) do
    {producer_run, _producer_fence} =
      completed_goal_run_fixture(producer, runtime_fixture(), candidate_subject)

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
               }),
               rollout_enabled: true
             )

    validation = Repo.get!(Task, validation_admission.response["task"]["id"])
    validation_runtime = runtime_fixture(@validation_runtime_id)

    {validation_run, validation_fence} =
      completed_goal_run_fixture(validation, validation_runtime, candidate_subject)

    {producer_run, validation, validation_run, validation_runtime, validation_fence}
  end

  def independent_validation_attempt_fixture(
        acceptance_contract \\ check_contract(),
        revision_overrides \\ %{}
      ) do
    revision_overrides =
      Map.update(
        revision_overrides,
        :execution_policy,
        %{"max_task_admissions" => 4},
        fn policy ->
          policy
          |> Map.new(fn {key, value} -> {to_string(key), value} end)
          |> Map.put_new("max_task_admissions", 4)
        end
      )

    {goal, item, producer} =
      admitted_task_fixture("primary", acceptance_contract, revision_overrides)

    candidate_subject = candidate_subject(item_repository_resource_id(item))

    {producer_run, validation, validation_run, validation_runtime, validation_fence} =
      settled_validation_attempt!(goal, item, producer, candidate_subject)

    {goal, item, producer, producer_run, validation, validation_run, validation_runtime,
     validation_fence, candidate_subject}
  end

  def task_result(task, kind, overrides \\ %{}) do
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

  def put_run_task_result!(run, task_result) do
    Repo.update_all(
      from(row in Run, where: row.id == ^run.id),
      set: [result: %{"task_result" => task_result}]
    )
  end

  def resolve_decision!(goal_id, decision_id, option_id \\ "accept") do
    payload = %{
      decision_id: decision_id,
      expected_decision_version:
        Repo.get!(SymmetryControl.Goals.GoalDecision, decision_id).lock_version,
      option_id: option_id
    }

    assert {:ok, resolved, :created} =
             command_current(goal_id, "resolve_decision", payload)

    resolved
  end

  def delete_goal_wakeup_jobs(goal_id) do
    Repo.delete_all(
      from(job in Oban.Job,
        where:
          job.worker == "SymmetryControl.Goals.Workers.WakeupWorker" and
            fragment("? ->> 'goal_id' = ?", job.args, ^goal_id)
      )
    )
  end

  def wakeup_job_count(goal_id) do
    Repo.aggregate(
      from(job in Oban.Job,
        where:
          job.worker == "SymmetryControl.Goals.Workers.WakeupWorker" and
            fragment("? ->> 'goal_id' = ?", job.args, ^goal_id)
      ),
      :count
    )
  end

  def session_attrs(item, workspace_fingerprint) do
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

  def available_session_fixture(runtime, item, attrs, opts \\ []) do
    %HarnessSession{}
    |> HarnessSession.changeset(%{
      machine_id: runtime.machine_id,
      runtime_id: runtime.id,
      repository_resource_id: item_repository_resource_id(item),
      local_handle_id: attrs.local_handle_id,
      binding_id: Ecto.UUID.generate(),
      binding_verified: Keyword.get(opts, :binding_verified, true),
      harness_kind: attrs.harness_kind,
      harness_version: attrs.harness_version,
      adapter_version: attrs.adapter_version,
      workspace_fingerprint: attrs.workspace_fingerprint,
      state: "available"
    })
    |> Repo.insert!()
  end

  def amended_revision_contract(objective) do
    goal_attrs()
    |> Map.fetch!(:initial_revision)
    |> Map.drop([:reason])
    |> Map.put(:objective, objective)
  end

  def complete_task_and_release_reservation!(goal, task) do
    Repo.update_all(from(row in Task, where: row.id == ^task.id), set: [state: "completed"])

    Repo.update_all(
      from(row in SymmetryControl.Goals.GoalBudgetReservation,
        where: row.goal_id == ^goal.id and row.task_id == ^task.id
      ),
      set: [state: "released"]
    )
  end

  def admission_payload(item, overrides \\ %{}) do
    Map.merge(
      %{
        work_item_id: item.id,
        purpose: "implement",
        model_profile: "codex",
        session_mode: "fresh",
        requested_session_id: nil,
        validation_of_task_id: nil
      },
      Map.drop(overrides, [:subject, :limits, :reserved_microusd, :admission_key])
    )
  end

  def evidence_attrs(run_id, item), do: evidence_attrs(run_id, item, evidence_subject(item))

  def evidence_attrs(run_id, _item, subject) do
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

  def evidence_subject(item) do
    %{
      "resource_id" => item_repository_resource_id(item),
      "commit" => String.duplicate("a", 40),
      "tree_digest" => "sha256:" <> String.duplicate("b", 64)
    }
  end

  def evidence_subject_hash(subject) do
    "sha256:" <> Base.encode16(SymmetryControl.RequestHash.canonical(subject), case: :lower)
  end

  # By design independent of `Goals.mutation_id/2` so served ids cannot change bytes without
  # a red test: sha256 over "<prefix>:<part>:…", version-5/"a" nibbles, not RFC 9562 name hashing.
  def frozen_mutation_id(prefix, parts) do
    hex =
      :crypto.hash(:sha256, prefix <> ":" <> Enum.join(parts, ":"))
      |> Base.encode16(case: :lower)

    String.slice(hex, 0, 8) <>
      "-" <>
      String.slice(hex, 8, 4) <>
      "-5" <>
      String.slice(hex, 13, 3) <>
      "-a" <> String.slice(hex, 17, 3) <> "-" <> String.slice(hex, 20, 12)
  end

  def fetch_goal!(goal_id) do
    assert {:ok, goal} = Goals.fetch_goal(goal_id)
    goal
  end

  def plan_proposal(goal_id, items) do
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

  def accept_plan!(goal_id, proposal) do
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

    resolve_decision!(goal_id, decision_id)

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

  def configure_automatic_runtimes!(item, runtimes) do
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
                "handoff" => false,
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

  def set_goal_due!(goal_id) do
    Repo.update_all(
      from(goal in SymmetryControl.Goals.Goal, where: goal.id == ^goal_id),
      set: [state: "active", next_wake_at: @now, updated_at: @now]
    )
  end

  def goal_attrs(mutation_id \\ nil, execution_policy \\ %{}, context_manifest \\ %{}) do
    execution_policy =
      Enum.reduce(
        ["budget_limit_microusd", "per_run_cost_limit_microusd"],
        Map.new(execution_policy, fn {key, value} -> {to_string(key), value} end),
        fn key, policy ->
          case Map.get(policy, key) do
            value when is_integer(value) -> Map.put(policy, key, Integer.to_string(value))
            _ -> policy
          end
        end
      )

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

  defdelegate check_contract(), to: SymmetryControl.TestFixtures

  def baseline_subject(resource_id) do
    %{
      kind: "subject",
      subject: %{
        "resource_id" => resource_id,
        "commit" => String.duplicate("a", 40),
        "tree_digest" => "sha256:" <> String.duplicate("b", 64)
      }
    }
  end

  def candidate_subject(resource_id) do
    %{
      "resource_id" => resource_id,
      "commit" => String.duplicate("c", 40),
      "tree_digest" => "sha256:" <> String.duplicate("d", 64)
    }
  end

  def item_repository_resource_id(item) do
    Map.get(item, :repository_resource_id) ||
      get_in(item, [:baseline, :subject, "resource_id"])
  end

  def check_and_operator_acceptance_contract do
    %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "An independent check and scoped operator acceptance are required.",
      "predicates" => [
        %{"id" => "check", "kind" => "check", "validator_profile" => "test"},
        %{"id" => "operator", "kind" => "operator_acceptance"}
      ]
    }
  end

  def validation_profiles(
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

  def project_fixture(default_workspace \\ "primary") do
    {:ok, project} =
      Workspaces.create_project(%{
        name: "Goals #{System.unique_integer([:positive])}",
        key: "G#{System.unique_integer([:positive])}",
        default_agent_profile: "codex",
        default_workspace: default_workspace
      })

    project
  end

  defdelegate repository_fixture(project), to: SymmetryControl.TestFixtures

  def connected_github_repository_fixture(
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

  def runtime_fixture(runtime_id \\ nil, opts \\ []) do
    case runtime_id && Repo.get(Runtime, runtime_id) do
      %Runtime{} = runtime ->
        runtime

      _ ->
        repository_resource_id =
          Keyword.get_lazy(opts, :repository_resource_id, &runtime_repository_resource_id/0)

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

  defp runtime_repository_resource_id do
    Repo.one(
      from(item in WorkItem,
        where: not is_nil(item.goal_id) and not is_nil(item.repository_resource_id),
        order_by: [desc: item.inserted_at, desc: item.id],
        limit: 1,
        select: item.repository_resource_id
      )
    )
  end
end
