defmodule SymmetryControl.Goals.EligibilityTest do
  use SymmetryControl.OrchestrationCase, async: false

  alias SymmetryControl.Goals.Eligibility
  alias SymmetryControl.Goals.Goal
  alias SymmetryControl.Orchestration.Runtime
  alias SymmetryControl.Orchestration.Task
  alias SymmetryControl.Workspaces.WorkItem

  @now ~U[2026-09-09 09:00:00.000000Z]

  test "planning_task? accepts only plan tasks without a work item or producer" do
    plan = %Task{purpose: "plan", work_item_id: nil, validation_of_task_id: nil}

    assert Eligibility.planning_task?(plan)
    refute Eligibility.planning_task?(%{plan | work_item_id: Ecto.UUID.generate()})
    refute Eligibility.planning_task?(%{plan | validation_of_task_id: Ecto.UUID.generate()})
    refute Eligibility.planning_task?(%{plan | purpose: "implement"})
  end

  test "goal_task_current? requires the current revision and a state-matching purpose" do
    goal_id = Ecto.UUID.generate()
    draft = %Goal{id: goal_id, state: "draft", current_revision: 3}
    active = %{draft | state: "active"}
    plan = %Task{goal_id: goal_id, goal_revision: 3, purpose: "plan"}
    implement = %Task{goal_id: goal_id, goal_revision: 3, purpose: "implement"}

    assert Eligibility.goal_task_current?(draft, plan)
    refute Eligibility.goal_task_current?(draft, implement)
    assert Eligibility.goal_task_current?(active, implement)
    refute Eligibility.goal_task_current?(active, plan)
    refute Eligibility.goal_task_current?(%{active | current_revision: 4}, implement)
    refute Eligibility.goal_task_current?(active, %{implement | goal_id: Ecto.UUID.generate()})
  end

  test "goal_task_assignable? requires a current queued task with an unconsumed attempt" do
    goal = %Goal{id: Ecto.UUID.generate(), state: "active", current_revision: 1}

    task = %Task{
      goal_id: goal.id,
      goal_revision: 1,
      purpose: "implement",
      work_item_id: Ecto.UUID.generate(),
      state: "queued",
      current_generation: 0,
      attempt_generation: 1,
      max_run_attempts: 2
    }

    assert Eligibility.goal_task_assignable?(goal, task)
    refute Eligibility.goal_task_assignable?(goal, %{task | state: "assigned"})
    refute Eligibility.goal_task_assignable?(goal, %{task | current_generation: 1})
    refute Eligibility.goal_task_assignable?(goal, %{task | attempt_generation: 3})
    refute Eligibility.goal_task_assignable?(goal, %{task | goal_revision: 2})
  end

  test "goal_task_repository_resource_id reads plan subjects and work item resources" do
    resource_id = Ecto.UUID.generate()
    plan = %Task{purpose: "plan", input: %{"subject" => %{"resource_id" => resource_id}}}
    item = %WorkItem{repository_resource_id: resource_id}

    assert Eligibility.goal_task_repository_resource_id(plan, nil) == resource_id
    assert Eligibility.goal_task_repository_resource_id(plan, item) == resource_id
    assert Eligibility.goal_task_repository_resource_id(%{plan | input: nil}, nil) == nil

    malformed = %{plan | input: %{"subject" => %{"resource_id" => "not-a-uuid"}}}
    assert Eligibility.goal_task_repository_resource_id(malformed, nil) == nil

    assert Eligibility.goal_task_repository_resource_id(
             %{plan | purpose: "implement"},
             nil
           ) == nil
  end

  test "goal_work_item_owned? binds plan tasks to the goal and items to the admitted revision" do
    goal = %Goal{id: Ecto.UUID.generate()}
    plan = %Task{goal_id: goal.id, goal_revision: 3, purpose: "plan"}
    item = %WorkItem{id: Ecto.UUID.generate(), goal_id: goal.id, admitted_revision: 3}
    task = %Task{goal_id: goal.id, goal_revision: 3, purpose: "implement", work_item_id: item.id}

    assert Eligibility.goal_work_item_owned?(goal, nil, plan)
    refute Eligibility.goal_work_item_owned?(goal, nil, %{plan | purpose: "implement"})
    assert Eligibility.goal_work_item_owned?(goal, item, task)
    refute Eligibility.goal_work_item_owned?(goal, %{item | admitted_revision: 2}, task)

    refute Eligibility.goal_work_item_owned?(
             goal,
             item,
             %{task | work_item_id: Ecto.UUID.generate()}
           )

    refute Eligibility.goal_work_item_owned?(goal, item, %{task | goal_revision: 2})
  end

  test "task_allowed_runtime_ids admits unique canonical ids for goal tasks only" do
    first = Ecto.UUID.generate()
    second = Ecto.UUID.generate()
    goal_id = Ecto.UUID.generate()

    legacy = %Task{goal_id: nil, allowed_runtime_ids: nil}
    assert Eligibility.task_allowed_runtime_ids(legacy) == {:ok, nil}

    allowed = %Task{goal_id: goal_id, allowed_runtime_ids: [first, second]}
    assert Eligibility.task_allowed_runtime_ids(allowed) == {:ok, [first, second]}

    duplicated = %Task{goal_id: goal_id, allowed_runtime_ids: [first, first]}
    assert Eligibility.task_allowed_runtime_ids(duplicated) == :invalid

    malformed = %Task{goal_id: goal_id, allowed_runtime_ids: ["not-a-uuid"]}
    assert Eligibility.task_allowed_runtime_ids(malformed) == :invalid

    missing = %Task{goal_id: goal_id, allowed_runtime_ids: nil}
    assert Eligibility.task_allowed_runtime_ids(missing) == :invalid
  end

  test "runtime_allowed_for_task? treats an empty set as unrestricted" do
    runtime = %Runtime{id: Ecto.UUID.generate()}
    goal_id = Ecto.UUID.generate()

    unrestricted = %Task{goal_id: goal_id, allowed_runtime_ids: []}
    assert Eligibility.runtime_allowed_for_task?(runtime, unrestricted)

    permitted = %Task{goal_id: goal_id, allowed_runtime_ids: [runtime.id]}
    assert Eligibility.runtime_allowed_for_task?(runtime, permitted)

    other = %Task{goal_id: goal_id, allowed_runtime_ids: [Ecto.UUID.generate()]}
    refute Eligibility.runtime_allowed_for_task?(runtime, other)

    malformed = %Task{goal_id: goal_id, allowed_runtime_ids: "unexpected"}
    refute Eligibility.runtime_allowed_for_task?(runtime, malformed)
  end

  test "validation runtime ids are unrestricted without a validation snapshot" do
    runtime = %Runtime{id: Ecto.UUID.generate()}
    implement = %Task{purpose: "implement"}

    validate = %Task{
      purpose: "validate",
      context_snapshot_id: Ecto.UUID.generate(),
      goal_id: Ecto.UUID.generate(),
      goal_revision: 1,
      work_item_id: Ecto.UUID.generate()
    }

    assert Eligibility.validation_runtime_ids_for_task(implement) == {:ok, nil}
    assert Eligibility.validation_runtime_allowed?(runtime, implement)
    assert Eligibility.validation_runtime_ids_for_task(validate) == :invalid
    refute Eligibility.validation_runtime_allowed?(runtime, validate)
  end

  test "handoff and goal resource checks reject sources that do not exist" do
    machine_id = Ecto.UUID.generate()
    resource_id = Ecto.UUID.generate()
    runtime = %Runtime{id: Ecto.UUID.generate(), machine_id: machine_id}
    runtime = %{runtime | repository_resource_id: resource_id}
    no_source = %Task{handoff_source_run_id: nil}
    handoff = %Task{handoff_source_run_id: Ecto.UUID.generate()}

    assert Eligibility.handoff_source_machine_matches?(runtime, no_source)
    refute Eligibility.handoff_source_machine_matches?(runtime, handoff)
    refute Eligibility.runtime_matches_goal_resource?(runtime, nil, resource_id)

    refute Eligibility.runtime_matches_goal_resource?(
             runtime,
             Ecto.UUID.generate(),
             Ecto.UUID.generate()
           )
  end

  test "goal_subject_matches_resource? compares the input subject to the repository resource" do
    resource_id = Ecto.UUID.generate()
    task = %Task{input: %{"subject" => %{"resource_id" => resource_id}}}

    assert Eligibility.goal_subject_matches_resource?(task, resource_id)
    refute Eligibility.goal_subject_matches_resource?(task, Ecto.UUID.generate())
    refute Eligibility.goal_subject_matches_resource?(%{task | input: nil}, resource_id)
    refute Eligibility.goal_subject_matches_resource?(task, nil)
  end

  test "valid_validation_binding? and canonical_runtime_uuid? require canonical unique ids" do
    runtime_id = Ecto.UUID.generate()
    valid = binding("review:alpha", "review", [runtime_id])

    assert Eligibility.valid_validation_binding?(valid)
    assert Eligibility.canonical_runtime_uuid?(runtime_id)
    refute Eligibility.canonical_runtime_uuid?(String.upcase(runtime_id))
    refute Eligibility.valid_validation_binding?(Map.put(valid, "extra", true))

    empty = %{valid | "allowed_runtime_ids" => []}
    refute Eligibility.valid_validation_binding?(empty)
    refute Eligibility.valid_validation_binding?(%{valid | "kind" => "audit"})

    noncanonical = %{valid | "allowed_runtime_ids" => [String.upcase(runtime_id)]}
    refute Eligibility.valid_validation_binding?(noncanonical)

    duplicated = %{valid | "allowed_runtime_ids" => [runtime_id, runtime_id]}
    refute Eligibility.valid_validation_binding?(duplicated)
    refute Eligibility.valid_validation_binding?(nil)
  end

  test "validation binding runtime ids intersect and reject disjoint or duplicate profiles" do
    first = Ecto.UUID.generate()
    second = Ecto.UUID.generate()
    third = Ecto.UUID.generate()
    alpha = binding("review:alpha", "review", [first, second])
    beta = binding("check:beta", "check", [second, third])

    assert Eligibility.validation_binding_runtime_ids([]) == {:ok, nil}

    assert {:ok, single} = Eligibility.validation_binding_runtime_ids([alpha])
    assert Enum.sort(single) == Enum.sort([first, second])

    assert {:ok, intersection} =
             Eligibility.intersect_validation_binding_runtime_ids([alpha, beta])

    assert Enum.sort(intersection) == [second]

    disjoint = [alpha, binding("check:gamma", "check", [third])]
    assert Eligibility.intersect_validation_binding_runtime_ids(disjoint) == :invalid

    duplicate = binding("review:alpha", "check", [first])
    duplicated = [duplicate, binding("review:alpha", "check", [second])]
    assert Eligibility.intersect_validation_binding_runtime_ids(duplicated) == :invalid
  end

  test "value, valid_uuid?, and exact_map_keys? share string keys" do
    uuid = Ecto.UUID.generate()
    map = %{"outer" => %{inner: 1}, "atom_key" => 2}

    assert Eligibility.value(map, :outer) == %{inner: 1}
    assert Eligibility.value(map, :atom_key) == 2
    assert Eligibility.value(map, :missing, :fallback) == :fallback
    assert Eligibility.value(nil, :missing, :fallback) == :fallback

    assert Eligibility.valid_uuid?(uuid)
    assert Eligibility.valid_uuid?(String.upcase(uuid))
    refute Eligibility.valid_uuid?("not-a-uuid")

    assert Eligibility.exact_map_keys?(%{:a => 1, "b" => 2}, ["a", "b"])
    refute Eligibility.exact_map_keys?(%{"a" => 1}, ["a", "b"])
    refute Eligibility.exact_map_keys?(%{"a" => 1, "b" => 2}, ["a"])
  end

  test "candidate_task_query selects current plan and active tasks and skips stale revisions" do
    plan_id = seed_candidate_task!(goal_state: "draft", purpose: "plan")
    active_id = seed_candidate_task!(goal_state: "active", purpose: "implement")

    stale_id = seed_candidate_task!(goal_state: "active", purpose: "implement", stale?: true)

    expected = Enum.sort([plan_id, active_id])

    query_ids =
      @now
      |> Eligibility.candidate_task_query(nil)
      |> Repo.all()
      |> Enum.map(& &1.task_id)
      |> Enum.sort()

    page_ids =
      @now
      |> Eligibility.next_goal_task_candidate_page(nil)
      |> Enum.map(& &1.task_id)
      |> Enum.sort()

    assert query_ids == expected
    assert page_ids == expected
    refute stale_id in query_ids
  end

  defp binding(profile_name, kind, allowed_runtime_ids) do
    %{
      "profile_name" => profile_name,
      "kind" => kind,
      "profile_digest" => "sha256:" <> String.duplicate("a", 64),
      "allowed_runtime_ids" => allowed_runtime_ids
    }
  end

  defp seed_candidate_task!(opts) do
    goal_state = Keyword.fetch!(opts, :goal_state)
    purpose = Keyword.fetch!(opts, :purpose)
    stale? = Keyword.get(opts, :stale?, false)
    current_revision = if stale?, do: 2, else: 1
    task_revision = if stale?, do: 1, else: current_revision
    work_item? = purpose != "plan"
    project_id = Ecto.UUID.generate()
    goal_id = Ecto.UUID.generate()
    snapshot_id = Ecto.UUID.generate()
    task_id = Ecto.UUID.generate()
    work_item_id = if work_item?, do: Ecto.UUID.generate()
    repository_id = if work_item?, do: Ecto.UUID.generate()
    idempotency_key = "candidate-task-#{System.unique_integer([:positive])}"

    {:ok, task_id} =
      Repo.transaction(fn ->
        Repo.query!(
          """
          INSERT INTO projects (
            id, name, key, status, default_agent_profile, default_workspace, inserted_at, updated_at
          ) VALUES ($1, $2, $3, 'active', 'codex', 'primary', $4, $4)
          """,
          [db_uuid(project_id), "Eligibility project", project_key(), @now]
        )

        if work_item? do
          Repo.query!(
            """
            INSERT INTO project_resources (
              id, project_id, kind, name, status, sync_status, metadata, lock_version,
              inserted_at, updated_at
            ) VALUES ($1, $2, 'repository', $3, 'unknown', 'unknown', '{}'::jsonb, 1, $4, $4)
            """,
            [db_uuid(repository_id), db_uuid(project_id), "Eligibility repository", @now]
          )
        end

        Repo.query!(
          """
          INSERT INTO goals (
            id, project_id, title, state, current_revision, event_sequence, inserted_at, updated_at
          ) VALUES ($1, $2, 'Eligibility goal', $3, $4, 0, $5, $5)
          """,
          [db_uuid(goal_id), db_uuid(project_id), goal_state, current_revision, @now]
        )

        for revision <- 1..current_revision do
          Repo.query!(
            """
            INSERT INTO goal_revisions (
              goal_id, revision, objective, non_goals, acceptance_contract, authority_policy,
              execution_policy, context_manifest, reason, actor_ref, inserted_at
            ) VALUES ($1, $2, $3, '[]'::jsonb, '{}'::jsonb, '{}'::jsonb, $4::text::jsonb,
              '{}'::jsonb, 'initial', 'operator:test', $5)
            """,
            [
              db_uuid(goal_id),
              revision,
              "Eligibility goal revision #{revision}",
              Jason.encode!(goal_execution_policy()),
              @now
            ]
          )
        end

        if work_item? do
          Repo.query!(
            """
            INSERT INTO work_items (
              id, project_id, title, status, priority, position, assignee_type, blocked,
              goal_id, admitted_revision, acceptance_contract, integration, repository_resource_id,
              baseline_subject, inserted_at, updated_at
            ) VALUES ($1, $2, 'Eligibility work item', 'ready', 'no_priority', 0, 'unassigned',
              FALSE, $3, $4, '{}'::jsonb, TRUE, $5, $6::text::jsonb, $7, $7)
            """,
            [
              db_uuid(work_item_id),
              db_uuid(project_id),
              db_uuid(goal_id),
              task_revision,
              db_uuid(repository_id),
              Jason.encode!(subject_document(repository_id)),
              @now
            ]
          )
        end

        Repo.query!(
          """
          INSERT INTO context_snapshots (
            id, goal_id, goal_revision, work_item_id, schema_version, content_hash, payload,
            inserted_at
          ) VALUES ($1, $2, $3, $4, 1, $5, $6::text::jsonb, $7)
          """,
          [
            db_uuid(snapshot_id),
            db_uuid(goal_id),
            task_revision,
            maybe_db_uuid(work_item_id),
            :crypto.hash(:sha256, idempotency_key),
            Jason.encode!(%{"work_contract" => %{"purpose" => purpose}}),
            @now
          ]
        )

        Repo.query!(
          """
          INSERT INTO tasks (
            id, idempotency_key, request_hash, request_hash_version, goal, agent_profile, workspace,
            input, required_capabilities, state, current_generation, attempt_generation, work_item_id,
            goal_id, goal_revision, context_snapshot_id, purpose, validation_of_task_id, admission_key,
            allowed_runtime_ids, max_run_attempts, inserted_at, updated_at
          ) VALUES ($1, $2, $3, 1, 'Eligibility task', 'codex', 'primary', '{}'::jsonb,
            '{}'::jsonb, 'queued', 0, 1, $4, $5, $6, $7, $8, NULL, $9,
            ARRAY[]::uuid[], 1, $10, $10)
          """,
          [
            db_uuid(task_id),
            idempotency_key,
            :crypto.hash(:sha256, idempotency_key),
            maybe_db_uuid(work_item_id),
            db_uuid(goal_id),
            task_revision,
            db_uuid(snapshot_id),
            purpose,
            db_uuid(Ecto.UUID.generate()),
            @now
          ]
        )

        task_id
      end)

    task_id
  end

  defp subject_document(repository_id) do
    %{
      "resource_id" => repository_id,
      "commit" => String.duplicate("a", 40),
      "tree_digest" => "sha256:" <> String.duplicate("b", 64)
    }
  end

  defp goal_execution_policy do
    %{
      "automatic_execution" => true,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "max_run_attempts_per_task" => 1,
      "budget_limit_microusd" => 1_000_000,
      "per_run_cost_limit_microusd" => nil,
      "budget_mode" => "soft",
      "hard_cost_limit_required" => false,
      "allowed_runtime_ids" => [],
      "allowed_model_profiles" => ["codex"],
      "final_acceptance" => "operator",
      "allowed_actions" => [],
      "allowed_resource_ids" => []
    }
  end

  defp project_key do
    "P" <> Integer.to_string(rem(System.unique_integer([:positive]), 9_999_999))
  end

  defp maybe_db_uuid(nil), do: nil
  defp maybe_db_uuid(uuid), do: db_uuid(uuid)

  defp db_uuid(uuid), do: Ecto.UUID.dump!(uuid)
end
