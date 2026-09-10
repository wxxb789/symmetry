defmodule SymmetryControl.Migrations.Goal0006MigrationTest do
  use ExUnit.Case, async: false

  alias Ecto.Adapters.SQL.Sandbox
  alias SymmetryControl.Repo
  alias SymmetryControl.Repo.Migrations.AddGoal0006ControlPlane
  alias SymmetryControl.Repo.Migrations.AddGoalTerminalGuardsAndExecutionPolicy
  alias SymmetryControl.Repo.Migrations.AddGoalTerminalAuthorityGuardsAndSessionReciprocity
  alias SymmetryControl.Repo.Migrations.AddHarnessSessionStopReceipts
  alias SymmetryControl.Repo.Migrations.AddHarnessSessionAttachReceipts
  alias SymmetryControl.Repo.Migrations.AddGoalIntegrationWorkItemDesignation
  alias SymmetryControl.Repo.Migrations.AddGoalIdentityGuards
  alias SymmetryControl.Repo.Migrations.AddTaskHandoffLineage
  alias SymmetryControl.Repo.Migrations.AddPlanningTaskIdentityGuards
  alias SymmetryControl.Repo.Migrations.AddGoalWorkItemBaselines
  alias SymmetryControl.Repo.Migrations.AddGoalWorkItemChangeTargets
  alias SymmetryControl.Repo.Migrations.AddEngineeringConnections
  alias SymmetryControl.Repo.Migrations.AddHistoryLookupIndexes
  alias SymmetryControl.Repo.Migrations.AddMachineEnrollmentReplay
  alias SymmetryControl.Repo.Migrations.AddObanJobsTable
  alias SymmetryControl.Repo.Migrations.AddRuntimeRepositoryResourceAffinity
  alias SymmetryControl.Repo.Migrations.AddProviderActionDispatchOwnership
  alias SymmetryControl.Repo.Migrations.AddRunProviderAccessSnapshots
  alias SymmetryControl.Repo.Migrations.AddRetryCommands
  alias SymmetryControl.Repo.Migrations.AddSupervisoryControls
  alias SymmetryControl.Repo.Migrations.AddTaskAttemptIdentity
  alias SymmetryControl.Repo.Migrations.AddTaskCommandOwnership
  alias SymmetryControl.Repo.Migrations.AddTaskRequiredCapabilities
  alias SymmetryControl.Repo.Migrations.BindWorkItemsToResources
  alias SymmetryControl.Repo.Migrations.CompleteEngineeringWorkspace
  alias SymmetryControl.Repo.Migrations.CreateChat
  alias SymmetryControl.Repo.Migrations.CreateEngineeringWorkspace
  alias SymmetryControl.Repo.Migrations.CreateOrchestrationTables
  alias SymmetryControl.Repo.Migrations.CreateProviderActionIntents
  alias SymmetryControl.Repo.Migrations.EnforceConnectedResourceIdentity
  alias SymmetryControl.Repo.Migrations.EnforceRuntimeRepositoryResourceAffinity
  alias SymmetryControl.Repo.Migrations.EnforceWorkItemCoherence
  alias SymmetryControl.Repo.Migrations.SnapshotProviderActionTargets
  alias SymmetryControl.Repo.Migrations.VersionCommandRequestHashes
  alias SymmetryControl.Repo.Migrations.VersionRequestHashes
  alias SymmetryControl.Orchestration.Task

  @migration_version 20_260_909_000_000
  @oban_migration_version 20_260_909_010_000
  @runtime_affinity_migration_version 20_260_909_020_000
  @integration_designation_migration_version 20_260_909_030_000
  @baseline_migration_version 20_260_909_040_000
  @identity_migration_version 20_260_909_050_000
  @provider_access_snapshot_migration_version 20_260_909_070_000
  @change_target_migration_version 20_260_909_080_000
  @planning_task_migration_version 20_260_910_000_000
  @terminal_policy_migration_version 20_260_910_010_000
  @runtime_affinity_guard_migration_version 20_260_910_020_000
  @terminal_authority_migration_version 20_260_910_030_000
  @handoff_lineage_migration_version 20_260_910_040_000
  @session_stop_receipt_migration_version 20_260_910_050_000
  @session_attach_receipt_migration_version 20_260_911_000_000

  test "upgrades legacy rows without assigning their textual goal to durable Goal history" do
    with_schema(fn ->
      project_id = insert_project!()
      work_item_id = insert_work_item!(project_id)
      task_id = insert_legacy_task!("Legacy textual goal")

      Repo.query!("UPDATE work_items SET orchestration_task_id = $1 WHERE id = $2", [
        task_id,
        work_item_id
      ])

      migrate_goal_up!()

      assert %{rows: [[^work_item_id, nil, nil, nil]]} =
               Repo.query!(
                 "SELECT work_item_id, goal_id, goal_revision, admission_key FROM tasks WHERE id = $1",
                 [task_id]
               )

      assert %{rows: [[nil, nil, nil]]} =
               Repo.query!(
                 "SELECT goal_id, admitted_revision, acceptance_contract FROM work_items WHERE id = $1",
                 [work_item_id]
               )

      assert %{rows: [[0]]} = Repo.query!("SELECT COUNT(*) FROM goals")

      assert %{rows: [["Legacy textual goal"]]} =
               Repo.query!("SELECT goal FROM tasks WHERE id = $1", [task_id])
    end)
  end

  test "backfills only provably provider-free active legacy claim replays" do
    with_schema(fn ->
      machine_id = Ecto.UUID.bingenerate()
      runtime_id = Ecto.UUID.bingenerate()
      provider_free_task_id = insert_legacy_task!("Provider-free claim replay")
      provider_task_id = insert_legacy_task!("Provider claim replay")
      provider_free_run_id = Ecto.UUID.bingenerate()
      provider_run_id = Ecto.UUID.bingenerate()

      Repo.query!(
        "UPDATE tasks SET state = 'claimed', current_generation = 1 WHERE id IN ($1, $2)",
        [
          provider_free_task_id,
          provider_task_id
        ]
      )

      Repo.query!("UPDATE tasks SET required_capabilities = $1::text::jsonb WHERE id = $2", [
        Jason.encode!(%{"provider_access" => true}),
        provider_task_id
      ])

      Repo.query!(
        """
        INSERT INTO machines (id, name, token_digest, inserted_at, updated_at)
        VALUES ($1, 'provider-access-snapshot-machine', $2, now(), now())
        """,
        [machine_id, <<8>>]
      )

      Repo.query!(
        """
        INSERT INTO runtimes (
          id, machine_id, runtime_key, name, daemon_instance_id, connection_epoch, capacity,
          agent_profile, workspace, capabilities, status, heartbeat_interval_ms, inserted_at, updated_at
        )
        VALUES ($1, $2, 'provider-access-snapshot', 'provider-access-snapshot', $3, 1, 1,
                'codex', 'primary', '{}'::jsonb, 'online', 5000, now(), now())
        """,
        [runtime_id, machine_id, Ecto.UUID.bingenerate()]
      )

      Enum.each(
        [{provider_free_run_id, provider_free_task_id}, {provider_run_id, provider_task_id}],
        fn {run_id, task_id} ->
          Repo.query!(
            """
            INSERT INTO runs (
              id, task_id, runtime_id, generation, state, claimed_runtime_epoch, claim_id, lease_token,
              assigned_at, assignment_expires_at, claimed_at, lease_expires_at, inserted_at, updated_at
            )
            VALUES ($1, $2, $3, 1, 'claimed', 1, $4, $5, now(), now() + interval '1 minute',
                    now(), now() + interval '1 minute', now(), now())
            """,
            [run_id, task_id, runtime_id, Ecto.UUID.bingenerate(), Ecto.UUID.bingenerate()]
          )
        end
      )

      assert_raise Postgrex.Error,
                   ~r/cannot add run provider access snapshots while active provider-required claims exist/i,
                   &migrate_provider_access_snapshot_up!/0

      Repo.query!("UPDATE runs SET state = 'completed' WHERE id = $1", [provider_run_id])
      Repo.query!("UPDATE tasks SET state = 'completed' WHERE id = $1", [provider_task_id])
      migrate_provider_access_snapshot_up!()

      assert %{rows: [[%{"v" => 1, "kind" => "none"}]]} =
               Repo.query!("SELECT provider_access_snapshot FROM runs WHERE id = $1", [
                 provider_free_run_id
               ])

      assert %{rows: [[nil]]} =
               Repo.query!("SELECT provider_access_snapshot FROM runs WHERE id = $1", [
                 provider_run_id
               ])

      resource_id = Ecto.UUID.generate()

      invalid_snapshots = [
        %{"v" => 1, "kind" => "none", "token" => "provider-secret"},
        %{
          "v" => 1,
          "kind" => "granted",
          "grants" => [
            %{
              "resource_id" => resource_id,
              "provider" => "github",
              "kind" => "repository",
              "operations" => ["resource.sync"],
              "token" => "provider-secret"
            }
          ]
        },
        %{
          "v" => 1,
          "kind" => "granted",
          "grants" => [
            %{
              "resource_id" => resource_id,
              "provider" => "github",
              "kind" => "repository",
              "operations" => ["repository.delete"]
            }
          ]
        }
      ]

      Enum.each(invalid_snapshots, fn snapshot ->
        assert_raise Postgrex.Error, ~r/runs_provider_access_snapshot_shape_check/i, fn ->
          Repo.query!(
            "UPDATE runs SET provider_access_snapshot = $1::text::jsonb WHERE id = $2",
            [Jason.encode!(snapshot), provider_run_id]
          )
        end
      end)

      valid_snapshot = %{
        "v" => 1,
        "kind" => "granted",
        "grants" => [
          %{
            "resource_id" => resource_id,
            "provider" => "github",
            "kind" => "repository",
            "operations" => ["resource.sync", "change.upsert"]
          }
        ]
      }

      Repo.query!(
        "UPDATE runs SET provider_access_snapshot = $1::text::jsonb WHERE id = $2",
        [Jason.encode!(valid_snapshot), provider_run_id]
      )

      assert %{rows: [[^valid_snapshot]]} =
               Repo.query!("SELECT provider_access_snapshot FROM runs WHERE id = $1", [
                 provider_run_id
               ])

      assert_raise Postgrex.Error, ~r/goal_0006_provider_access_snapshot_immutable/i, fn ->
        Repo.query!(
          "UPDATE runs SET provider_access_snapshot = '{\"v\": 1, \"kind\": \"none\"}'::jsonb WHERE id = $1",
          [provider_run_id]
        )
      end

      assert_raise Postgrex.Error,
                   ~r/cannot roll back run provider access snapshots while replay snapshots exist/i,
                   &migrate_provider_access_snapshot_down!/0
    end)
  end

  test "provider access snapshots roll back and reapply on a clean tree" do
    with_schema(fn ->
      migrate_provider_access_snapshot_up!()
      migrate_provider_access_snapshot_down!()

      assert %{rows: []} =
               Repo.query!(
                 "SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'runs' AND column_name = 'provider_access_snapshot'"
               )

      migrate_provider_access_snapshot_up!()

      assert %{rows: [["provider_access_snapshot"]]} =
               Repo.query!(
                 "SELECT column_name FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'runs' AND column_name = 'provider_access_snapshot'"
               )
    end)
  end

  test "migrated goal-less legacy tasks pass the existing lifecycle changesets" do
    with_schema(fn ->
      project_id = insert_project!()
      work_item_id = insert_work_item!(project_id)
      task_id = insert_legacy_task!("Legacy lifecycle task")

      Repo.query!("UPDATE work_items SET orchestration_task_id = $1 WHERE id = $2", [
        task_id,
        work_item_id
      ])

      migrate_goal_up!()
      migrate_handoff_lineage_up!()
      task = Repo.get!(Task, Ecto.UUID.cast!(task_id))

      assert task.goal_id == nil
      assert task.work_item_id == Ecto.UUID.cast!(work_item_id)

      Enum.reduce(
        ["queued", "assigned", "claimed", "running", "completed"],
        task,
        fn state, task ->
          changeset = Task.changeset(task, %{state: state})
          assert changeset.valid?, inspect(changeset.errors)
          Repo.update!(changeset)
        end
      )
    end)
  end

  test "permits the documented legacy generic runtime adapter" do
    with_schema(fn ->
      migrate_goal_up!()
      machine_id = Ecto.UUID.bingenerate()
      runtime_id = Ecto.UUID.bingenerate()

      Repo.query!(
        """
        INSERT INTO machines (id, name, token_digest, inserted_at, updated_at)
        VALUES ($1, 'generic-adapter-machine', $2, now(), now())
        """,
        [machine_id, <<7>>]
      )

      Repo.query!(
        """
        INSERT INTO runtimes (
          id, machine_id, runtime_key, name, daemon_instance_id, connection_epoch, capacity,
          agent_profile, workspace, status, heartbeat_interval_ms, harness_kind, harness_version,
          adapter_version, adapter_protocol_version, inserted_at, updated_at
        )
        VALUES ($1, $2, 'generic', 'generic', $3, 1, 1, 'default', 'primary', 'online', 5000,
                'generic', 'legacy', 'legacy', 1, now(), now())
        """,
        [runtime_id, machine_id, Ecto.UUID.bingenerate()]
      )

      assert %{rows: [["generic"]]} =
               Repo.query!("SELECT harness_kind FROM runtimes WHERE id = $1", [runtime_id])
    end)
  end

  test "enforces Goal-owned membership, validation identity, and one active task per WorkItem" do
    with_schema(fn ->
      migrate_goal_up!()

      %{goal_id: goal_id, project_id: project_id, work_item_id: work_item_id} =
        insert_goal_fixture!()

      assert_raise Postgrex.Error, ~r/tasks_goal_fields_all_or_none/i, fn ->
        insert_goal_task!(%{goal_id: goal_id})
      end

      assert_raise Postgrex.Error, ~r/tasks_(work_item_id|goal_work_item_membership)_fkey/i, fn ->
        insert_goal_task!(%{
          goal_id: goal_id,
          work_item_id: Ecto.UUID.bingenerate(),
          context_snapshot_id: Ecto.UUID.bingenerate()
        })
      end

      first_task_id = insert_goal_task!(%{goal_id: goal_id, work_item_id: work_item_id})

      assert_raise Postgrex.Error, ~r/tasks_one_active_goal_task_per_work_item/i, fn ->
        insert_goal_task!(%{goal_id: goal_id, work_item_id: work_item_id})
      end

      Repo.query!("UPDATE tasks SET state = 'completed' WHERE id = $1", [first_task_id])

      other_item_id = insert_goal_work_item!(project_id, goal_id)
      other_task_id = insert_goal_task!(%{goal_id: goal_id, work_item_id: other_item_id})

      assert_raise Postgrex.Error, ~r/tasks_validation_task_identity_fkey/i, fn ->
        insert_goal_task!(%{
          goal_id: goal_id,
          work_item_id: work_item_id,
          purpose: "validate",
          validation_of_task_id: other_task_id
        })
      end

      validation_task_id =
        insert_goal_task!(%{
          goal_id: goal_id,
          work_item_id: work_item_id,
          purpose: "validate",
          validation_of_task_id: first_task_id
        })

      self_validation_task_id = Ecto.UUID.bingenerate()

      assert_raise Postgrex.Error,
                   ~r/(tasks_validation_not_self|goal_0006_validation_task_must_not_validate_itself)/i,
                   fn ->
                     insert_goal_task!(%{
                       id: self_validation_task_id,
                       goal_id: goal_id,
                       work_item_id: work_item_id,
                       purpose: "validate",
                       validation_of_task_id: self_validation_task_id
                     })
                   end

      assert_raise Postgrex.Error,
                   ~r/goal_0006_validation_task_requires_nonvalidation_producer/i,
                   fn ->
                     insert_goal_task!(%{
                       goal_id: goal_id,
                       work_item_id: work_item_id,
                       purpose: "validate",
                       validation_of_task_id: validation_task_id
                     })
                   end
    end)
  end

  test "enforces the planning Task and ContextSnapshot ownership branch" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()

      %{goal_id: goal_id, project_id: project_id, work_item_id: work_item_id} =
        insert_goal_fixture!(execution_policy_json(), true)

      second_work_item_id = insert_goal_work_item!(project_id, goal_id)

      migrate_baseline_up!()
      migrate_identity_up!()
      migrate_planning_task_up!()

      plan_task_id =
        Repo.transaction(fn ->
          context_snapshot_id = insert_planning_context_snapshot!(goal_id)

          insert_goal_task!(%{
            goal_id: goal_id,
            work_item_id: nil,
            context_snapshot_id: context_snapshot_id,
            purpose: "plan"
          })
        end)
        |> then(fn {:ok, task_id} -> task_id end)

      assert %{rows: [[^plan_task_id, nil, "plan"]]} =
               Repo.query!(
                 "SELECT id, work_item_id, purpose FROM tasks WHERE id = $1",
                 [plan_task_id]
               )

      assert_raise Postgrex.Error, ~r/tasks_one_active_plan_task_per_goal/i, fn ->
        Repo.transaction(fn ->
          context_snapshot_id = insert_planning_context_snapshot!(goal_id)

          insert_goal_task!(%{
            goal_id: goal_id,
            work_item_id: nil,
            context_snapshot_id: context_snapshot_id,
            purpose: "plan"
          })
        end)
      end

      assert_raise Postgrex.Error,
                   ~r/goal_0006_planning_task_requires_goal_scoped_context_snapshot/i,
                   fn ->
                     insert_goal_task!(%{
                       goal_id: goal_id,
                       work_item_id: work_item_id,
                       purpose: "plan"
                     })
                   end

      assert_raise Postgrex.Error, ~r/tasks_goal_fields_all_or_none/i, fn ->
        Repo.transaction(fn ->
          context_snapshot_id = insert_planning_context_snapshot!(goal_id)

          insert_goal_task!(%{
            goal_id: goal_id,
            work_item_id: nil,
            context_snapshot_id: context_snapshot_id,
            purpose: "plan",
            validation_of_task_id: plan_task_id
          })
        end)
      end

      assert_raise Postgrex.Error,
                   ~r/goal_0006_planning_task_requires_goal_scoped_context_snapshot/i,
                   fn ->
                     insert_goal_task!(%{
                       goal_id: goal_id,
                       work_item_id: nil,
                       context_snapshot_id: insert_context_snapshot!(goal_id, work_item_id),
                       purpose: "plan"
                     })
                   end

      assert_raise Postgrex.Error, ~r/goal_0006_task_context_snapshot_work_item_mismatch/i, fn ->
        insert_goal_task!(%{
          goal_id: goal_id,
          work_item_id: second_work_item_id,
          context_snapshot_id: insert_context_snapshot!(goal_id, work_item_id),
          purpose: "implement"
        })
      end

      assert_raise Postgrex.Error,
                   ~r/goal_0006_planning_context_snapshot_requires_plan_task/i,
                   fn ->
                     Repo.transaction(fn ->
                       insert_planning_context_snapshot!(goal_id)
                     end)
                   end

      assert_raise Postgrex.Error,
                   ~r/cannot roll back planning task identity guards while planning history exists/i,
                   &migrate_planning_task_down!/0
    end)
  end

  test "retains incompatible existing planning WIP by refusing its destructive reinterpretation" do
    with_schema(fn ->
      migrate_goal_up!()
      %{goal_id: goal_id, work_item_id: work_item_id} = insert_goal_fixture!()

      insert_goal_task!(%{goal_id: goal_id, work_item_id: work_item_id, purpose: "plan"})

      assert_raise Postgrex.Error,
                   ~r/cannot add planning task identity guards while existing planning tasks do not meet Goal-scoped ownership/i,
                   &migrate_planning_task_up!/0

      assert %{rows: [[^work_item_id, "plan"]]} =
               Repo.query!("SELECT work_item_id, purpose FROM tasks WHERE goal_id = $1", [goal_id])
    end)
  end

  test "planning Task identity guards roll back and reapply on a clean tree" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()
      migrate_baseline_up!()
      migrate_identity_up!()
      migrate_planning_task_up!()
      migrate_planning_task_down!()

      assert %{rows: [["NO"]]} =
               Repo.query!("""
               SELECT is_nullable
               FROM information_schema.columns
                WHERE table_schema = current_schema()
                  AND table_name = 'context_snapshots'
                 AND column_name = 'work_item_id'
               """)

      migrate_planning_task_up!()

      assert %{rows: [["YES"]]} =
               Repo.query!("""
               SELECT is_nullable
               FROM information_schema.columns
                WHERE table_schema = current_schema()
                  AND table_name = 'context_snapshots'
                 AND column_name = 'work_item_id'
               """)
    end)
  end

  test "enforces composite ownership and immutable Goal history" do
    with_schema(fn ->
      migrate_goal_up!()
      %{goal_id: goal_id, work_item_id: work_item_id} = insert_goal_fixture!()
      snapshot_id = Ecto.UUID.bingenerate()

      Repo.query!(
        """
        INSERT INTO context_snapshots (
          id, goal_id, goal_revision, work_item_id, schema_version, content_hash, payload, inserted_at
        )
        VALUES ($1, $2, 1, $3, 1, $4, '{}'::jsonb, now())
        """,
        [snapshot_id, goal_id, work_item_id, hash(2)]
      )

      assert_raise Postgrex.Error, ~r/goal_0006_immutable_history/i, fn ->
        Repo.query!(
          "UPDATE context_snapshots SET payload = '{\"changed\": true}'::jsonb WHERE id = $1",
          [
            snapshot_id
          ]
        )
      end

      assert_raise Postgrex.Error, ~r/context_snapshots_work_item_ownership_fkey/i, fn ->
        Repo.query!(
          """
          INSERT INTO context_snapshots (
            id, goal_id, goal_revision, work_item_id, schema_version, content_hash, payload, inserted_at
          )
          VALUES ($1, $2, 1, $3, 1, $4, '{}'::jsonb, now())
          """,
          [Ecto.UUID.bingenerate(), goal_id, Ecto.UUID.bingenerate(), hash(3)]
        )
      end
    end)
  end

  test "rejects deletion of a resolved Goal decision" do
    with_schema(fn ->
      migrate_goal_up!()
      %{goal_id: goal_id} = insert_goal_fixture!()
      decision_id = insert_resolved_goal_decision!(goal_id)

      assert_raise Postgrex.Error, ~r/goal_0006_resolved_decision_immutable/i, fn ->
        Repo.query!("DELETE FROM goal_decisions WHERE id = $1", [decision_id])
      end

      assert %{rows: [[^decision_id]]} =
               Repo.query!("SELECT id FROM goal_decisions WHERE id = $1", [decision_id])
    end)
  end

  test "stores an immutable full candidate Subject and producer result identity with each outcome" do
    with_schema(fn ->
      migrate_goal_up!()

      assert %{rows: [["candidate_subject", "NO"], ["producing_result_id", "NO"]]} =
               Repo.query!("""
               SELECT column_name, is_nullable
               FROM information_schema.columns
               WHERE table_schema = current_schema()
                 AND table_name = 'work_outcomes'
                 AND column_name IN ('candidate_subject', 'producing_result_id')
               ORDER BY column_name
               """)

      assert %{rows: [["work_outcomes_candidate_subject_is_object"]]} =
               Repo.query!("""
               SELECT conname
               FROM pg_constraint
               WHERE conrelid = 'work_outcomes'::regclass
                 AND conname = 'work_outcomes_candidate_subject_is_object'
               """)
    end)
  end

  test "enforces canonical UUIDs in Goal JSON arrays at the SQL boundary" do
    with_schema(fn ->
      migrate_goal_up!()

      assert %{rows: [[true]]} =
               Repo.query!(
                 "SELECT goal_0006_jsonb_uuid_array('[\"550e8400-e29b-41d4-a716-446655440000\"]'::jsonb)"
               )

      assert %{rows: [[false]]} =
               Repo.query!(
                 "SELECT goal_0006_jsonb_uuid_array('[\"550E8400-E29B-41D4-A716-446655440000\"]'::jsonb)"
               )

      assert %{rows: [[false]]} =
               Repo.query!(
                 "SELECT goal_0006_jsonb_uuid_array('[\"550e8400-e29b-01d4-a716-446655440000\"]'::jsonb)"
               )

      assert %{rows: [[false]]} =
               Repo.query!(
                 "SELECT goal_0006_jsonb_uuid_array('[\"550e8400-e29b-41d4-c716-446655440000\"]'::jsonb)"
               )
    end)
  end

  test "enforces the run usage cost basis and amount invariant in SQL" do
    with_schema(fn ->
      migrate_goal_up!()
      task_id = insert_legacy_task!("Usage invariant")
      machine_id = Ecto.UUID.bingenerate()
      runtime_id = Ecto.UUID.bingenerate()
      run_id = Ecto.UUID.bingenerate()

      Repo.query!(
        """
        INSERT INTO machines (id, name, token_digest, inserted_at, updated_at)
        VALUES ($1, 'usage-machine', $2, now(), now())
        """,
        [machine_id, <<10>>]
      )

      Repo.query!(
        """
        INSERT INTO runtimes (
          id, machine_id, runtime_key, name, daemon_instance_id, connection_epoch, capacity,
          agent_profile, workspace, status, heartbeat_interval_ms, inserted_at, updated_at
        )
        VALUES ($1, $2, 'usage-runtime', 'usage-runtime', $3, 1, 1, 'default', 'primary',
                'online', 5000, now(), now())
        """,
        [runtime_id, machine_id, Ecto.UUID.bingenerate()]
      )

      Repo.query!(
        """
        INSERT INTO runs (
          id, task_id, runtime_id, generation, state, assigned_at, assignment_expires_at,
          inserted_at, updated_at
        )
        VALUES ($1, $2, $3, 1, 'completed', now(), now(), now(), now())
        """,
        [run_id, task_id, runtime_id]
      )

      assert_raise Postgrex.Error, ~r/run_usage_cost_basis_check/i, fn ->
        insert_run_usage!(run_id, "unknown-with-cost", "unknown", 1)
      end

      assert_raise Postgrex.Error, ~r/run_usage_cost_basis_check/i, fn ->
        insert_run_usage!(run_id, "reported-without-cost", "reported", nil)
      end

      insert_run_usage!(run_id, "unknown-without-cost", "unknown", nil)
      insert_run_usage!(run_id, "reported-with-cost", "reported", 1)

      assert %{rows: [[2]]} =
               Repo.query!("SELECT COUNT(*) FROM run_usage WHERE run_id = $1", [run_id])
    end)
  end

  test "refuses destructive rollback after Goal history and preserves it" do
    with_schema(fn ->
      migrate_goal_up!()
      %{goal_id: goal_id} = insert_goal_fixture!()

      assert_raise Postgrex.Error,
                   ~r/cannot roll back Goal 0006 while Goal history exists/i,
                   &migrate_goal_down!/0

      assert %{rows: [[^goal_id, "draft"]]} =
               Repo.query!("SELECT id, state FROM goals WHERE id = $1", [goal_id])

      assert %{rows: [[@migration_version]]} =
               Repo.query!("SELECT version FROM schema_migrations WHERE version = $1", [
                 @migration_version
               ])
    end)
  end

  test "refuses destructive rollback while a harness session exists" do
    with_schema(fn ->
      migrate_goal_up!()
      machine_id = Ecto.UUID.bingenerate()
      runtime_id = Ecto.UUID.bingenerate()
      project_id = insert_project!()
      resource_id = Ecto.UUID.bingenerate()
      session_id = Ecto.UUID.bingenerate()

      Repo.query!(
        """
        INSERT INTO machines (id, name, token_digest, inserted_at, updated_at)
        VALUES ($1, 'session-machine', $2, now(), now())
        """,
        [machine_id, <<11>>]
      )

      Repo.query!(
        """
        INSERT INTO runtimes (
          id, machine_id, runtime_key, name, daemon_instance_id, connection_epoch, capacity,
          agent_profile, workspace, status, heartbeat_interval_ms, inserted_at, updated_at
        )
        VALUES ($1, $2, 'session-runtime', 'session-runtime', $3, 1, 1, 'default', 'primary',
                'online', 5000, now(), now())
        """,
        [runtime_id, machine_id, Ecto.UUID.bingenerate()]
      )

      Repo.query!(
        """
        INSERT INTO project_resources (
          id, project_id, kind, name, status, sync_status, metadata, lock_version, inserted_at, updated_at
        )
        VALUES ($1, $2, 'repository', 'session-repository', 'unknown', 'unknown', '{}'::jsonb, 1, now(), now())
        """,
        [resource_id, project_id]
      )

      Repo.query!(
        """
        INSERT INTO harness_sessions (
          id, machine_id, runtime_id, harness_kind, harness_version, adapter_version,
          local_handle_id, repository_resource_id, workspace_fingerprint, state, inserted_at, updated_at
        )
        VALUES ($1, $2, $3, 'codex', '1.0.0', 'adapter-1', $4, $5, 'fingerprint', 'available',
                now(), now())
        """,
        [session_id, machine_id, runtime_id, Ecto.UUID.bingenerate(), resource_id]
      )

      assert_raise Postgrex.Error,
                   ~r/cannot roll back Goal 0006 while Goal history exists/i,
                   &migrate_goal_down!/0

      assert %{rows: [[^session_id]]} =
               Repo.query!("SELECT id FROM harness_sessions WHERE id = $1", [session_id])
    end)
  end

  test "rolls back a schema with no Goal history without disturbing legacy rows" do
    with_schema(fn ->
      project_id = insert_project!()
      work_item_id = insert_work_item!(project_id)
      task_id = insert_legacy_task!("No durable Goal")

      Repo.query!("UPDATE work_items SET orchestration_task_id = $1 WHERE id = $2", [
        task_id,
        work_item_id
      ])

      migrate_goal_up!()

      assert %{rows: [[^work_item_id]]} =
               Repo.query!("SELECT work_item_id FROM tasks WHERE id = $1", [task_id])

      migrate_goal_down!()

      assert %{rows: [[^task_id]]} = Repo.query!("SELECT id FROM tasks WHERE id = $1", [task_id])

      assert %{rows: [[^work_item_id]]} =
               Repo.query!("SELECT id FROM work_items WHERE id = $1", [work_item_id])

      assert %{rows: [[^task_id]]} =
               Repo.query!("SELECT orchestration_task_id FROM work_items WHERE id = $1", [
                 work_item_id
               ])

      assert %{rows: []} =
               Repo.query!("SELECT version FROM schema_migrations WHERE version = $1", [
                 @migration_version
               ])
    end)
  end

  test "refuses rollback when inferred legacy membership is no longer the current pointer" do
    with_schema(fn ->
      project_id = insert_project!()
      original_work_item_id = insert_work_item!(project_id)
      replacement_work_item_id = insert_work_item!(project_id)
      task_id = insert_legacy_task!("Moved legacy pointer")

      Repo.query!("UPDATE work_items SET orchestration_task_id = $1 WHERE id = $2", [
        task_id,
        original_work_item_id
      ])

      migrate_goal_up!()

      Repo.query!("UPDATE work_items SET orchestration_task_id = NULL WHERE id = $1", [
        original_work_item_id
      ])

      Repo.query!("UPDATE work_items SET orchestration_task_id = $1 WHERE id = $2", [
        task_id,
        replacement_work_item_id
      ])

      assert_raise Postgrex.Error,
                   ~r/cannot roll back Goal 0006 while Goal history exists/i,
                   &migrate_goal_down!/0

      assert %{rows: [[^original_work_item_id]]} =
               Repo.query!("SELECT work_item_id FROM tasks WHERE id = $1", [task_id])

      assert %{rows: [[@migration_version]]} =
               Repo.query!("SELECT version FROM schema_migrations WHERE version = $1", [
                 @migration_version
               ])
    end)
  end

  test "applies the Goal control plane before its Oban jobs dependency" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_oban_up!()

      assert %{rows: [["public.oban_jobs"]]} =
               Repo.query!("SELECT to_regclass('public.oban_jobs')::text")

      assert %{rows: [[@migration_version], [@oban_migration_version]]} =
               Repo.query!(
                 "SELECT version FROM schema_migrations WHERE version IN ($1, $2) ORDER BY version",
                 [@migration_version, @oban_migration_version]
               )
    end)
  end

  test "refuses Oban rollback while an incomplete Goal worker remains queued" do
    with_schema(fn ->
      migrate_goal_up!()

      assert %{rows: [[oban_jobs_before]]} =
               Repo.query!("SELECT to_regclass('public.oban_jobs')::text")

      migrate_oban_up!()

      job_marker = "goal-0006-oban-#{System.unique_integer([:positive])}"

      assert %{rows: [[job_id]]} =
               Repo.query!(
                 """
                 INSERT INTO public.oban_jobs (state, queue, worker, args)
                 VALUES (
                   'available',
                   'goal_wakeup',
                   'SymmetryControl.Goals.Workers.WakeupWorker',
                   $1::text::jsonb
                 )
                 RETURNING id
                 """,
                 [Jason.encode!(%{"goal_0006_test_marker" => job_marker})]
               )

      assert_raise Postgrex.Error,
                   ~r/cannot roll back Oban jobs while queued Goal wakeups exist/i,
                   &migrate_oban_down!/0

      assert %{rows: [["public.oban_jobs"]]} =
               Repo.query!("SELECT to_regclass('public.oban_jobs')::text")

      Repo.query!(
        "DELETE FROM public.oban_jobs WHERE id = $1 AND args ->> 'goal_0006_test_marker' = $2",
        [job_id, job_marker]
      )

      if is_nil(oban_jobs_before) do
        migrate_oban_down!()
        assert %{rows: [[nil]]} = Repo.query!("SELECT to_regclass('public.oban_jobs')::text")
      else
        assert %{rows: [["public.oban_jobs"]]} =
                 Repo.query!("SELECT to_regclass('public.oban_jobs')::text")
      end
    end)
  end

  test "adds nullable runtime repository affinity without invalidating old runtimes" do
    with_schema(fn ->
      migrate_goal_up!()
      machine_id = Ecto.UUID.bingenerate()
      runtime_id = Ecto.UUID.bingenerate()

      Repo.query!(
        """
        INSERT INTO machines (id, name, token_digest, inserted_at, updated_at)
        VALUES ($1, 'pre-affinity-machine', $2, now(), now())
        """,
        [machine_id, <<8>>]
      )

      Repo.query!(
        """
        INSERT INTO runtimes (
          id, machine_id, runtime_key, name, daemon_instance_id, connection_epoch, capacity,
          agent_profile, workspace, status, heartbeat_interval_ms, harness_kind, harness_version,
          adapter_version, adapter_protocol_version, inserted_at, updated_at
        )
        VALUES ($1, $2, 'pre-affinity', 'pre-affinity', $3, 1, 1, 'default', 'primary', 'online', 5000,
                'codex', '0.153.4', 'symmetry-codex-1', 1, now(), now())
        """,
        [runtime_id, machine_id, Ecto.UUID.bingenerate()]
      )

      migrate_runtime_affinity_up!()

      assert %{rows: [[nil]]} =
               Repo.query!("SELECT repository_resource_id FROM runtimes WHERE id = $1", [
                 runtime_id
               ])

      assert %{rows: [["YES"]]} =
               Repo.query!("""
               SELECT is_nullable
               FROM information_schema.columns
               WHERE table_schema = current_schema()
                 AND table_name = 'runtimes'
                 AND column_name = 'repository_resource_id'
               """)

      assert %{rows: [[@runtime_affinity_migration_version]]} =
               Repo.query!("SELECT version FROM schema_migrations WHERE version = $1", [
                 @runtime_affinity_migration_version
               ])
    end)
  end

  test "refuses runtime affinity rollback while a runtime binding exists" do
    with_schema(fn ->
      migrate_goal_up!()
      machine_id = Ecto.UUID.bingenerate()
      runtime_id = Ecto.UUID.bingenerate()
      project_id = insert_project!()
      resource_id = Ecto.UUID.bingenerate()

      Repo.query!(
        """
        INSERT INTO machines (id, name, token_digest, inserted_at, updated_at)
        VALUES ($1, 'bound-machine', $2, now(), now())
        """,
        [machine_id, <<9>>]
      )

      Repo.query!(
        """
        INSERT INTO runtimes (
          id, machine_id, runtime_key, name, daemon_instance_id, connection_epoch, capacity,
          agent_profile, workspace, status, heartbeat_interval_ms, inserted_at, updated_at
        )
        VALUES ($1, $2, 'bound-runtime', 'bound-runtime', $3, 1, 1, 'default', 'primary',
                'online', 5000, now(), now())
        """,
        [runtime_id, machine_id, Ecto.UUID.bingenerate()]
      )

      migrate_runtime_affinity_up!()

      Repo.query!(
        """
        INSERT INTO project_resources (
          id, project_id, kind, name, status, sync_status, metadata, lock_version, inserted_at, updated_at
        )
        VALUES ($1, $2, 'repository', 'bound-repository', 'unknown', 'unknown', '{}'::jsonb, 1, now(), now())
        """,
        [resource_id, project_id]
      )

      Repo.query!("UPDATE runtimes SET repository_resource_id = $1 WHERE id = $2", [
        resource_id,
        runtime_id
      ])

      assert_raise Postgrex.Error,
                   ~r/cannot roll back runtime repository affinity while active runtime repository bindings exist/i,
                   &migrate_runtime_affinity_down!/0

      assert %{rows: [[^resource_id]]} =
               Repo.query!("SELECT repository_resource_id FROM runtimes WHERE id = $1", [
                 runtime_id
               ])

      Repo.query!("UPDATE runtimes SET repository_resource_id = NULL WHERE id = $1", [runtime_id])
      migrate_runtime_affinity_down!()

      assert %{rows: []} =
               Repo.query!("""
               SELECT 1
               FROM information_schema.columns
               WHERE table_schema = current_schema()
                 AND table_name = 'runtimes'
                 AND column_name = 'repository_resource_id'
               """)
    end)
  end

  test "requires repository resources when runtime affinity is inserted or changed" do
    with_schema(fn ->
      migrate_runtime_affinity_up!()

      project_id = insert_project!()
      repository_id = insert_repository_resource!(project_id)
      ci_id = insert_project_resource!(project_id, "ci")
      machine_id = insert_machine!()
      legacy_runtime_id = insert_runtime!(machine_id, "legacy-runtime")

      Repo.query!("UPDATE runtimes SET repository_resource_id = $1 WHERE id = $2", [
        ci_id,
        legacy_runtime_id
      ])

      assert_raise Postgrex.Error,
                   ~r/cannot add runtime repository resource affinity guards while non-repository runtime bindings exist/i,
                   &migrate_runtime_affinity_guard_up!/0

      Repo.query!("UPDATE runtimes SET repository_resource_id = NULL WHERE id = $1", [
        legacy_runtime_id
      ])

      migrate_runtime_affinity_guard_up!()

      runtime_id = insert_runtime!(machine_id, "bound-runtime", repository_id)

      assert %{rows: [[^repository_id]]} =
               Repo.query!("SELECT repository_resource_id FROM runtimes WHERE id = $1", [
                 runtime_id
               ])

      unbound_runtime_id = insert_runtime!(machine_id, "unbound-runtime")

      assert_raise Postgrex.Error, ~r/runtime_repository_resource_requires_repository/i, fn ->
        Repo.query!("UPDATE runtimes SET repository_resource_id = $1 WHERE id = $2", [
          ci_id,
          unbound_runtime_id
        ])
      end

      assert_raise Postgrex.Error, ~r/runtime_repository_resource_requires_repository/i, fn ->
        insert_runtime!(machine_id, "invalid-bound-runtime", ci_id)
      end

      assert %{rows: [[function_definition]]} =
               Repo.query!("""
               SELECT pg_get_functiondef('validate_runtime_repository_resource()'::regprocedure)
               """)

      assert function_definition =~ "FOR SHARE"
    end)
  end

  test "holds the repository resource share lock while recording a runtime binding" do
    with_schema(fn ->
      migrate_runtime_affinity_up!()
      migrate_runtime_affinity_guard_up!()

      project_id = insert_project!()
      repository_id = insert_repository_resource!(project_id)
      machine_id = insert_machine!()
      runtime_id = insert_runtime!(machine_id, "concurrent-bound-runtime")
      parent = self()

      assert %{rows: [[schema]]} = Repo.query!("SELECT current_schema()")
      runtime_repo = start_schema_repo(schema)
      resource_repo = start_schema_repo(schema)

      runtime_binding_task =
        Elixir.Task.async(fn ->
          previous_dynamic_repo = Repo.put_dynamic_repo(runtime_repo)

          try do
            Repo.transaction(fn ->
              Repo.query!("UPDATE runtimes SET repository_resource_id = $1 WHERE id = $2", [
                repository_id,
                runtime_id
              ])

              send(parent, {:runtime_resource_share_lock_held, self()})

              receive do
                :commit_runtime_binding -> :ok
              after
                15_000 -> raise "timed out waiting to commit runtime binding"
              end
            end)
          after
            Repo.put_dynamic_repo(previous_dynamic_repo)
          end
        end)

      try do
        assert_receive {:runtime_resource_share_lock_held, _runtime_binding_pid}, 15_000

        resource_update_task =
          Elixir.Task.async(fn ->
            previous_dynamic_repo = Repo.put_dynamic_repo(resource_repo)

            try do
              try do
                Repo.transaction(fn ->
                  Repo.query!("SET LOCAL lock_timeout = '1s'")

                  Repo.query!(
                    "UPDATE project_resources SET external_ref = 'rewritten' WHERE id = $1",
                    [repository_id]
                  )
                end)

                :unexpected_success
              rescue
                error in Postgrex.Error -> {:error, error}
              end
            after
              Repo.put_dynamic_repo(previous_dynamic_repo)
            end
          end)

        assert {:error, error} = Elixir.Task.await(resource_update_task, 15_000)
        assert Exception.message(error) =~ "lock timeout"

        send(runtime_binding_task.pid, :commit_runtime_binding)
        assert {:ok, :ok} = Elixir.Task.await(runtime_binding_task, 15_000)
      after
        if Process.alive?(runtime_binding_task.pid) do
          send(runtime_binding_task.pid, :commit_runtime_binding)
          Elixir.Task.shutdown(runtime_binding_task, :brutal_kill)
        end

        GenServer.stop(resource_repo)
        GenServer.stop(runtime_repo)
      end
    end)
  end

  test "freezes resources bound by any runtime while permitting presentation updates and safe rollback" do
    with_schema(fn ->
      migrate_runtime_affinity_up!()
      migrate_runtime_affinity_guard_up!()

      project_id = insert_project!()
      other_project_id = insert_project!()
      repository_id = insert_repository_resource!(project_id)
      connection_id = insert_connection!()
      machine_id = insert_machine!()
      runtime_id = insert_runtime!(machine_id, "bound-runtime", repository_id)

      Repo.query!("UPDATE runtimes SET status = 'offline' WHERE id = $1", [runtime_id])

      for {statement, parameters} <- [
            {"UPDATE project_resources SET project_id = $1 WHERE id = $2",
             [other_project_id, repository_id]},
            {"UPDATE project_resources SET kind = 'ci' WHERE id = $1", [repository_id]},
            {"UPDATE project_resources SET provider = 'github' WHERE id = $1", [repository_id]},
            {"UPDATE project_resources SET external_ref = 'rewritten' WHERE id = $1",
             [repository_id]},
            {"UPDATE project_resources SET connection_id = $1 WHERE id = $2",
             [connection_id, repository_id]}
          ] do
        assert_raise Postgrex.Error, ~r/runtime_repository_resource_identity_immutable/i, fn ->
          Repo.query!(statement, parameters)
        end
      end

      Repo.query!(
        "UPDATE project_resources SET name = 'renamed repository', status = 'healthy' WHERE id = $1",
        [repository_id]
      )

      assert %{rows: [["renamed repository", "healthy"]]} =
               Repo.query!("SELECT name, status FROM project_resources WHERE id = $1", [
                 repository_id
               ])

      assert_raise Postgrex.Error,
                   ~r/cannot roll back runtime repository resource affinity guards while runtime bindings exist/i,
                   &migrate_runtime_affinity_guard_down!/0

      Repo.query!("UPDATE runtimes SET repository_resource_id = NULL WHERE id = $1", [runtime_id])
      migrate_runtime_affinity_guard_down!()

      assert %{rows: [[nil, nil]]} =
               Repo.query!("""
               SELECT
                 to_regprocedure('validate_runtime_repository_resource()')::text,
                 to_regprocedure('freeze_runtime_repository_resource_identity()')::text
               """)

      migrate_runtime_affinity_guard_up!()

      assert %{rows: [[@runtime_affinity_guard_migration_version]]} =
               Repo.query!("SELECT version FROM schema_migrations WHERE version = $1", [
                 @runtime_affinity_guard_migration_version
               ])
    end)
  end

  test "persists integration designation only for Goal WorkItems and rejects mutation after admission" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()

      project_id = insert_project!()
      legacy_work_item_id = insert_work_item!(project_id)

      assert %{rows: [[false, "NO"]]} =
               Repo.query!(
                 """
                 SELECT integration, is_nullable
                 FROM work_items
                 JOIN information_schema.columns
                   ON table_schema = current_schema()
                  AND table_name = 'work_items'
                  AND column_name = 'integration'
                 WHERE id = $1
                 """,
                 [legacy_work_item_id]
               )

      assert_raise Postgrex.Error, ~r/work_items_integration_requires_goal_ownership/i, fn ->
        Repo.query!("UPDATE work_items SET integration = TRUE WHERE id = $1", [
          legacy_work_item_id
        ])
      end

      %{goal_id: goal_id, project_id: goal_project_id} = insert_goal_fixture!()
      designated_id = insert_designated_goal_work_item!(goal_project_id, goal_id)

      assert %{rows: [[true]]} =
               Repo.query!("SELECT integration FROM work_items WHERE id = $1", [designated_id])

      assert_raise Postgrex.Error, ~r/goal_0006_goal_work_item_integration_immutable/i, fn ->
        Repo.query!("UPDATE work_items SET integration = FALSE WHERE id = $1", [designated_id])
      end
    end)
  end

  test "refuses integration designation rollback while a designation would be lost" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()
      %{goal_id: goal_id, project_id: project_id} = insert_goal_fixture!()
      _designated_id = insert_designated_goal_work_item!(project_id, goal_id)

      assert_raise Postgrex.Error,
                   ~r/cannot roll back integration WorkItem designation while designated rows exist/i,
                   &migrate_integration_designation_down!/0

      assert %{rows: [[@integration_designation_migration_version]]} =
               Repo.query!("SELECT version FROM schema_migrations WHERE version = $1", [
                 @integration_designation_migration_version
               ])
    end)
  end

  test "persists nullable immutable Goal change targets without rewriting goal-less items" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()
      migrate_baseline_up!()
      migrate_change_target_up!()

      legacy_project_id = insert_project!()
      legacy_work_item_id = insert_work_item!(legacy_project_id)

      assert %{rows: [[nil, "YES"]]} =
               Repo.query!(
                 """
                 SELECT change_target, is_nullable
                 FROM work_items
                 JOIN information_schema.columns
                   ON table_schema = current_schema()
                  AND table_name = 'work_items'
                  AND column_name = 'change_target'
                 WHERE id = $1
                 """,
                 [legacy_work_item_id]
               )

      assert_raise Postgrex.Error, ~r/goal_0006_work_item_change_target_requires_goal/i, fn ->
        Repo.query!(
          "UPDATE work_items SET change_target = '{\"kind\": \"branches\", \"source_branch\": \"feature\", \"target_branch\": \"main\"}'::jsonb WHERE id = $1",
          [legacy_work_item_id]
        )
      end

      project_id = insert_project!()
      repository_id = insert_repository_resource!(project_id)
      goal_id = Ecto.UUID.bingenerate()
      insert_goal_row!(goal_id, project_id)
      goal_work_item_id = insert_work_item!(project_id, repository_id)

      Repo.query!(
        """
        UPDATE work_items
        SET goal_id = $1,
            admitted_revision = 1,
            acceptance_contract = '{}'::jsonb,
            baseline_subject = $2::text::jsonb,
            integration = TRUE,
            change_target = '{"kind": "branches", "source_branch": "feature", "target_branch": "main"}'::jsonb
        WHERE id = $3
        """,
        [goal_id, Jason.encode!(subject(repository_id)), goal_work_item_id]
      )

      assert %{
               rows: [
                 [
                   %{
                     "kind" => "branches",
                     "source_branch" => "feature",
                     "target_branch" => "main"
                   }
                 ]
               ]
             } =
               Repo.query!("SELECT change_target FROM work_items WHERE id = $1", [
                 goal_work_item_id
               ])

      assert_raise Postgrex.Error, ~r/goal_0006_work_item_change_target_immutable/i, fn ->
        Repo.query!(
          "UPDATE work_items SET change_target = '{\"kind\": \"branches\", \"source_branch\": \"feature\", \"target_branch\": \"main\", \"unexpected\": true}'::jsonb WHERE id = $1",
          [goal_work_item_id]
        )
      end

      whitespace_branch_item_id = insert_work_item!(project_id, repository_id)

      assert_raise Postgrex.Error, ~r/work_items_change_target_shape_check/i, fn ->
        Repo.query!(
          """
          UPDATE work_items
          SET goal_id = $1,
              admitted_revision = 1,
              acceptance_contract = '{}'::jsonb,
              baseline_subject = $2::text::jsonb,
              change_target = '{"kind": "branches", "source_branch": " feature", "target_branch": "main"}'::jsonb
          WHERE id = $3
          """,
          [goal_id, Jason.encode!(subject(repository_id)), whitespace_branch_item_id]
        )
      end

      whitespace_pull_request_item_id = insert_work_item!(project_id, repository_id)

      assert_raise Postgrex.Error, ~r/work_items_change_target_shape_check/i, fn ->
        Repo.query!(
          """
          UPDATE work_items
          SET goal_id = $1,
              admitted_revision = 1,
              acceptance_contract = '{}'::jsonb,
              baseline_subject = $2::text::jsonb,
              change_target = '{"kind": "pull_request", "pull_request_url": " https://github.com/acme/repo/pull/1"}'::jsonb
          WHERE id = $3
          """,
          [goal_id, Jason.encode!(subject(repository_id)), whitespace_pull_request_item_id]
        )
      end

      assert_raise Postgrex.Error, ~r/goal_0006_work_item_change_target_immutable/i, fn ->
        Repo.query!(
          "UPDATE work_items SET change_target = '{\"kind\": \"pull_request\", \"pull_request_url\": \"https://github.com/acme/repo/pull/1\"}'::jsonb WHERE id = $1",
          [goal_work_item_id]
        )
      end

      assert_raise Postgrex.Error,
                   ~r/cannot roll back Goal WorkItem change targets while targets exist/i,
                   &migrate_change_target_down!/0
    end)
  end

  test "rolls back change-target storage when no frozen target exists" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()
      migrate_baseline_up!()
      migrate_change_target_up!()
      migrate_change_target_down!()

      assert %{rows: []} =
               Repo.query!("""
               SELECT 1
               FROM information_schema.columns
               WHERE table_schema = current_schema()
                 AND table_name = 'work_items'
                 AND column_name = 'change_target'
               """)
    end)
  end

  test "adds nullable baseline sources and rolls back when no source is present" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()
      %{work_item_id: goal_work_item_id} = insert_goal_fixture!(execution_policy_json(), true)
      project_id = insert_project!()
      work_item_id = insert_work_item!(project_id)
      migrate_baseline_up!()

      assert %{rows: [["YES"], ["YES"]]} =
               Repo.query!("""
               SELECT is_nullable
               FROM information_schema.columns
               WHERE table_schema = current_schema()
                 AND table_name = 'work_items'
                 AND column_name IN ('baseline_dependency_id', 'baseline_subject')
               ORDER BY column_name
               """)

      for migrated_work_item_id <- [goal_work_item_id, work_item_id] do
        assert %{rows: [[nil, nil]]} =
                 Repo.query!(
                   "SELECT baseline_subject, baseline_dependency_id FROM work_items WHERE id = $1",
                   [migrated_work_item_id]
                 )
      end

      migrate_baseline_down!()

      assert %{rows: []} =
               Repo.query!("""
               SELECT 1
               FROM information_schema.columns
               WHERE table_schema = current_schema()
                 AND table_name = 'work_items'
                 AND column_name IN ('baseline_dependency_id', 'baseline_subject')
               """)
    end)
  end

  test "refuses baseline guards when admitted history lacks an integration WorkItem" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()
      insert_goal_fixture!()

      assert_raise Postgrex.Error,
                   ~r/cannot add Goal WorkItem integration guard while an admitted revision has no integration WorkItem/i,
                   &migrate_baseline_up!/0
    end)
  end

  test "requires an integration WorkItem for each nonempty admitted revision at commit" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()
      migrate_baseline_up!()

      project_id = insert_project!()
      repository_id = insert_repository_resource!(project_id)
      empty_goal_id = Ecto.UUID.bingenerate()
      insert_goal_row!(empty_goal_id, project_id)

      assert %{rows: [[^empty_goal_id]]} =
               Repo.query!("SELECT id FROM goals WHERE id = $1", [empty_goal_id])

      complete_goal_id = Ecto.UUID.bingenerate()
      insert_goal_row!(complete_goal_id, project_id)
      first_item_id = insert_work_item!(project_id, repository_id)
      integration_item_id = insert_work_item!(project_id, repository_id)

      assert {:ok, _} =
               Repo.transaction(fn ->
                 Repo.query!(
                   """
                   UPDATE work_items
                   SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb,
                       baseline_subject = $2::text::jsonb, integration = FALSE
                   WHERE id = $3
                   """,
                   [complete_goal_id, Jason.encode!(subject(repository_id)), first_item_id]
                 )

                 Repo.query!(
                   """
                   UPDATE work_items
                   SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb,
                       baseline_subject = $2::text::jsonb, integration = TRUE
                   WHERE id = $3
                   """,
                   [complete_goal_id, Jason.encode!(subject(repository_id)), integration_item_id]
                 )
               end)

      incomplete_goal_id = Ecto.UUID.bingenerate()
      insert_goal_row!(incomplete_goal_id, project_id)
      incomplete_item_id = insert_work_item!(project_id, repository_id)

      assert_raise Postgrex.Error, ~r/goal_0006_goal_work_items_require_integration/i, fn ->
        Repo.transaction(fn ->
          Repo.query!(
            """
            UPDATE work_items
            SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb,
                baseline_subject = $2::text::jsonb, integration = FALSE
            WHERE id = $3
            """,
            [incomplete_goal_id, Jason.encode!(subject(repository_id)), incomplete_item_id]
          )
        end)
      end
    end)
  end

  test "requires a baseline for new Goal ownership but preserves historic NULL baselines" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()

      %{goal_id: historic_goal_id, work_item_id: historic_item_id} =
        insert_goal_fixture!(execution_policy_json(), true)

      project_id = insert_project!()
      goal_id = Ecto.UUID.bingenerate()
      insert_goal_row!(goal_id, project_id)
      migrate_baseline_up!()

      Repo.query!(
        "UPDATE work_items SET status = 'ready', external_provider = 'github' WHERE id = $1",
        [historic_item_id]
      )

      assert_raise Postgrex.Error, ~r/goal_0006_work_item_baseline_immutable/i, fn ->
        Repo.query!(
          "UPDATE work_items SET baseline_subject = '{}'::jsonb WHERE id = $1",
          [historic_item_id]
        )
      end

      inserted_item_id = Ecto.UUID.bingenerate()

      assert_raise Postgrex.Error, ~r/goal_0006_goal_work_item_requires_baseline/i, fn ->
        Repo.query!(
          """
          INSERT INTO work_items (
            id, project_id, title, status, priority, position, assignee_type, blocked,
            goal_id, admitted_revision, acceptance_contract, baseline_subject,
            baseline_dependency_id, inserted_at, updated_at
          )
          VALUES ($1, $2, 'New Goal WorkItem', 'backlog', 'no_priority', 0, 'unassigned',
                  FALSE, $3, 1, '{}'::jsonb, NULL, NULL, now(), now())
          """,
          [inserted_item_id, project_id, goal_id]
        )
      end

      updated_item_id = insert_work_item!(project_id)

      assert_raise Postgrex.Error, ~r/goal_0006_goal_work_item_requires_baseline/i, fn ->
        Repo.query!(
          """
          UPDATE work_items
          SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb
          WHERE id = $2
          """,
          [goal_id, updated_item_id]
        )
      end

      assert %{rows: [[^historic_goal_id]]} =
               Repo.query!("SELECT goal_id FROM work_items WHERE id = $1", [historic_item_id])
    end)
  end

  test "enforces immutable exact-resource and declared-dependency baseline sources" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()
      migrate_baseline_up!()

      project_id = insert_project!()
      repository_id = insert_repository_resource!(project_id)
      other_repository_id = insert_repository_resource!(project_id)
      goal_id = Ecto.UUID.bingenerate()
      insert_goal_row!(goal_id, project_id)

      subject_item_id = insert_work_item!(project_id, repository_id)

      null_resource_item_id = insert_work_item!(project_id)

      assert_raise Postgrex.Error, ~r/work_items_baseline_subject_shape_check/i, fn ->
        Repo.query!(
          """
          UPDATE work_items
          SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb,
              baseline_subject = $2::text::jsonb
          WHERE id = $3
          """,
          [goal_id, Jason.encode!(subject(repository_id)), null_resource_item_id]
        )
      end

      unknown_key_item_id = insert_work_item!(project_id, repository_id)

      assert_raise Postgrex.Error, ~r/work_items_baseline_subject_shape_check/i, fn ->
        Repo.query!(
          """
          UPDATE work_items
          SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb,
              baseline_subject = $2::text::jsonb
          WHERE id = $3
          """,
          [
            goal_id,
            Jason.encode!(Map.put(subject(repository_id), "unexpected", "key")),
            unknown_key_item_id
          ]
        )
      end

      numeric_commit_item_id = insert_work_item!(project_id, repository_id)

      assert_raise Postgrex.Error, ~r/work_items_baseline_subject_shape_check/i, fn ->
        Repo.query!(
          """
          UPDATE work_items
          SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb,
              baseline_subject = $2::text::jsonb
          WHERE id = $3
          """,
          [
            goal_id,
            Jason.encode!(%{
              "resource_id" => subject(repository_id)["resource_id"],
              "commit" => String.to_integer(String.duplicate("1", 40)),
              "tree_digest" => "sha256:" <> String.duplicate("4", 64)
            }),
            numeric_commit_item_id
          ]
        )
      end

      assert_raise Postgrex.Error, ~r/work_items_baseline_subject_shape_check/i, fn ->
        Repo.query!(
          """
          UPDATE work_items
          SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb,
              baseline_subject = $2::text::jsonb
          WHERE id = $3
          """,
          [
            goal_id,
            Jason.encode!(subject(other_repository_id)),
            subject_item_id
          ]
        )
      end

      dependency_baseline_id = insert_work_item!(project_id, repository_id)
      dependency_id = insert_work_item!(project_id, repository_id)
      target_id = insert_work_item!(project_id, repository_id)

      Repo.transaction(fn ->
        Repo.query!(
          """
          UPDATE work_items
          SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb,
              baseline_subject = $3::text::jsonb
          WHERE id = $2
          """,
          [goal_id, dependency_baseline_id, Jason.encode!(subject(repository_id))]
        )

        Repo.query!(
          """
          UPDATE work_items
          SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb,
              baseline_dependency_id = $2, integration = TRUE
          WHERE id = $3
          """,
          [goal_id, dependency_baseline_id, dependency_id]
        )

        Repo.query!(
          """
          UPDATE work_items
          SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb,
              baseline_dependency_id = $2
          WHERE id = $3
          """,
          [goal_id, dependency_id, target_id]
        )

        Repo.query!(
          """
          INSERT INTO work_dependencies (goal_id, work_item_id, depends_on_id, inserted_at)
          VALUES ($1, $2, $3, now())
          """,
          [goal_id, target_id, dependency_id]
        )

        Repo.query!(
          """
          INSERT INTO work_dependencies (goal_id, work_item_id, depends_on_id, inserted_at)
          VALUES ($1, $2, $3, now())
          """,
          [goal_id, dependency_id, dependency_baseline_id]
        )
      end)

      assert %{rows: [[^dependency_id]]} =
               Repo.query!("SELECT baseline_dependency_id FROM work_items WHERE id = $1", [
                 target_id
               ])

      cross_resource_target_id = insert_work_item!(project_id, other_repository_id)

      assert_raise Postgrex.Error, ~r/goal_0006_baseline_dependency_resource_mismatch/i, fn ->
        Repo.transaction(fn ->
          Repo.query!(
            """
            UPDATE work_items
            SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb,
                baseline_dependency_id = $2
            WHERE id = $3
            """,
            [goal_id, dependency_id, cross_resource_target_id]
          )

          Repo.query!(
            """
            INSERT INTO work_dependencies (goal_id, work_item_id, depends_on_id, inserted_at)
            VALUES ($1, $2, $3, now())
            """,
            [goal_id, cross_resource_target_id, dependency_id]
          )
        end)
      end

      assert_raise Postgrex.Error, ~r/goal_0006_baseline_dependency_resource_mismatch/i, fn ->
        Repo.transaction(fn ->
          Repo.query!("UPDATE work_items SET repository_resource_id = $1 WHERE id = $2", [
            other_repository_id,
            dependency_id
          ])
        end)
      end

      replacement_target_id = insert_work_item!(project_id, repository_id)

      Repo.query!(
        """
        UPDATE work_items
        SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb,
            baseline_subject = $3::text::jsonb
        WHERE id = $2
        """,
        [goal_id, replacement_target_id, Jason.encode!(subject(repository_id))]
      )

      assert_raise Postgrex.Error,
                   ~r/goal_0006_baseline_dependency_must_be_declared_dependency/i,
                   fn ->
                     Repo.query!(
                       """
                       UPDATE work_dependencies
                       SET work_item_id = $1
                       WHERE goal_id = $2 AND work_item_id = $3 AND depends_on_id = $4
                       """,
                       [replacement_target_id, goal_id, target_id, dependency_id]
                     )
                   end

      assert_raise Postgrex.Error,
                   ~r/goal_0006_baseline_dependency_must_be_declared_dependency/i,
                   fn ->
                     Repo.query!(
                       """
                       DELETE FROM work_dependencies
                       WHERE goal_id = $1 AND work_item_id = $2 AND depends_on_id = $3
                       """,
                       [goal_id, target_id, dependency_id]
                     )
                   end

      assert_raise Postgrex.Error, ~r/goal_0006_work_item_baseline_immutable/i, fn ->
        Repo.query!("UPDATE work_items SET baseline_dependency_id = NULL WHERE id = $1", [
          target_id
        ])
      end

      undeclared_target_id = insert_work_item!(project_id, repository_id)

      assert_raise Postgrex.Error,
                   ~r/goal_0006_baseline_dependency_must_be_declared_dependency/i,
                   fn ->
                     Repo.query!(
                       """
                       UPDATE work_items
                       SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb,
                           baseline_dependency_id = $2
                       WHERE id = $3
                       """,
                       [goal_id, dependency_id, undeclared_target_id]
                     )
                   end

      assert_raise Postgrex.Error,
                   ~r/cannot roll back Goal WorkItem baselines while baseline sources exist/i,
                   &migrate_baseline_down!/0
    end)
  end

  test "retains Goal WorkItems while legacy project deletion nilifies legacy task pointers" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()

      legacy_project_id = insert_project!()
      legacy_work_item_id = insert_work_item!(legacy_project_id)
      legacy_task_id = insert_legacy_task!("Legacy deletion")

      Repo.query!("UPDATE work_items SET orchestration_task_id = $1 WHERE id = $2", [
        legacy_task_id,
        legacy_work_item_id
      ])

      %{goal_id: goal_id, work_item_id: goal_work_item_id} =
        insert_goal_fixture!(execution_policy_json(), true)

      migrate_baseline_up!()
      migrate_identity_up!()

      assert_raise Postgrex.Error, ~r/goal_0006_goal_work_item_retained/i, fn ->
        Repo.query!("DELETE FROM work_items WHERE id = $1", [goal_work_item_id])
      end

      assert_raise Postgrex.Error,
                   ~r/(work_items_goal_project_ownership_fkey|goals_current_revision_fkey|goal_revisions_goal_id_fkey)/i,
                   fn ->
                     Repo.query!("DELETE FROM goals WHERE id = $1", [goal_id])
                   end

      Repo.query!("DELETE FROM projects WHERE id = $1", [legacy_project_id])

      assert %{rows: [[nil]]} =
               Repo.query!("SELECT work_item_id FROM tasks WHERE id = $1", [legacy_task_id])

      assert %{rows: []} =
               Repo.query!("SELECT id FROM work_items WHERE id = $1", [legacy_work_item_id])
    end)
  end

  test "enforces Goal repository project and kind identity and freezes referenced resources" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()

      project_id = insert_project!()
      other_project_id = insert_project!()
      repository_id = insert_repository_resource!(project_id)
      other_repository_id = insert_repository_resource!(other_project_id)
      ci_id = insert_project_resource!(project_id, "ci")
      goal_id = Ecto.UUID.bingenerate()
      insert_goal_row!(goal_id, project_id)
      work_item_id = insert_work_item!(project_id, repository_id)

      Repo.query!(
        """
        UPDATE work_items
        SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb, integration = TRUE
        WHERE id = $2
        """,
        [goal_id, work_item_id]
      )

      migrate_baseline_up!()
      legacy_work_item_id = insert_work_item!(project_id, repository_id)
      migrate_identity_up!()

      assert_raise Postgrex.Error,
                   ~r/(work_items_repository_resource_project_fkey|goal_0006_work_item_repository_resource_project)/i,
                   fn ->
                     Repo.query!(
                       "UPDATE work_items SET repository_resource_id = $1 WHERE id = $2",
                       [other_repository_id, legacy_work_item_id]
                     )
                   end

      assert_raise Postgrex.Error, ~r/goal_0006_work_item_repository_resource_kind/i, fn ->
        Repo.query!(
          "UPDATE work_items SET repository_resource_id = $1 WHERE id = $2",
          [ci_id, legacy_work_item_id]
        )
      end

      assert_raise Postgrex.Error, ~r/goal_0006_goal_resource_identity_immutable/i, fn ->
        Repo.query!(
          "UPDATE project_resources SET external_ref = 'rewritten' WHERE id = $1",
          [repository_id]
        )
      end
    end)
  end

  test "freezes Goal WorkItem and Task admission identity while allowing lifecycle updates" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()

      %{goal_id: goal_id, work_item_id: work_item_id} =
        insert_goal_fixture!(execution_policy_json(), true)

      task_id = insert_goal_task!(%{goal_id: goal_id, work_item_id: work_item_id})
      migrate_baseline_up!()
      migrate_identity_up!()

      assert_raise Postgrex.Error, ~r/goal_0006_goal_work_item_identity_immutable/i, fn ->
        Repo.query!("UPDATE work_items SET required = FALSE WHERE id = $1", [work_item_id])
      end

      assert_raise Postgrex.Error, ~r/goal_0006_goal_task_identity_immutable/i, fn ->
        Repo.query!("UPDATE tasks SET admission_key = $1 WHERE id = $2", [
          Ecto.UUID.bingenerate(),
          task_id
        ])
      end

      assert_raise Postgrex.Error, ~r/goal_0006_goal_task_identity_immutable/i, fn ->
        Repo.query!("UPDATE tasks SET input = '{\"changed\": true}'::jsonb WHERE id = $1", [
          task_id
        ])
      end

      assert_raise Postgrex.Error, ~r/goal_0006_goal_task_identity_immutable/i, fn ->
        Repo.query!("UPDATE tasks SET requested_session_id = $1 WHERE id = $2", [
          Ecto.UUID.bingenerate(),
          task_id
        ])
      end

      Repo.query!(
        "UPDATE tasks SET state = 'completed', current_generation = 1, attempt_generation = 1 WHERE id = $1",
        [task_id]
      )

      assert %{rows: [["completed", 1, 1]]} =
               Repo.query!(
                 "SELECT state, current_generation, attempt_generation FROM tasks WHERE id = $1",
                 [task_id]
               )
    end)
  end

  test "refuses identity guard rollback after Goal history exists" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()
      %{goal_id: goal_id} = insert_goal_fixture!(execution_policy_json(), true)
      migrate_baseline_up!()
      migrate_identity_up!()

      assert_raise Postgrex.Error,
                   ~r/cannot roll back Goal identity guards while Goal history exists/i,
                   &migrate_identity_down!/0

      assert %{rows: [[^goal_id]]} = Repo.query!("SELECT id FROM goals WHERE id = $1", [goal_id])
    end)
  end

  test "permits released session reuse while retaining historical run identity" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()

      migrate_baseline_up!()
      migrate_identity_up!()

      migrate_terminal_policy_up!()
      migrate_terminal_authority_up!()

      project_id = insert_project!()
      resource_id = insert_repository_resource!(project_id)
      machine_id = Ecto.UUID.bingenerate()
      runtime_id = Ecto.UUID.bingenerate()
      session_id = Ecto.UUID.bingenerate()
      task_id = insert_legacy_task!("Session reuse")
      first_run_id = Ecto.UUID.bingenerate()

      Repo.query!(
        """
        INSERT INTO machines (id, name, token_digest, inserted_at, updated_at)
        VALUES ($1, 'identity-machine', $2, now(), now())
        """,
        [machine_id, <<21>>]
      )

      Repo.query!(
        """
        INSERT INTO runtimes (
          id, machine_id, runtime_key, name, daemon_instance_id, connection_epoch, capacity,
          agent_profile, workspace, status, heartbeat_interval_ms, inserted_at, updated_at
        )
        VALUES ($1, $2, 'identity-runtime', 'identity-runtime', $3, 1, 1, 'default', 'primary',
                'online', 5000, now(), now())
        """,
        [runtime_id, machine_id, Ecto.UUID.bingenerate()]
      )

      Repo.query!(
        """
        INSERT INTO harness_sessions (
          id, machine_id, runtime_id, harness_kind, harness_version, adapter_version,
          local_handle_id, repository_resource_id, workspace_fingerprint, state, inserted_at, updated_at
        )
        VALUES ($1, $2, $3, 'codex', '1.0.0', 'adapter-1', $4, $5, 'fingerprint', 'available',
                now(), now())
        """,
        [session_id, machine_id, runtime_id, Ecto.UUID.bingenerate(), resource_id]
      )

      Repo.transaction(fn ->
        Repo.query!(
          """
          INSERT INTO runs (
            id, task_id, runtime_id, generation, state, assigned_at, assignment_expires_at,
            harness_session_id, inserted_at, updated_at
          )
          VALUES ($1, $2, $3, 1, 'running', now(), now(), $4, now(), now())
          """,
          [first_run_id, task_id, runtime_id, session_id]
        )

        Repo.query!(
          "UPDATE harness_sessions SET state = 'busy', active_run_id = $1 WHERE id = $2",
          [first_run_id, session_id]
        )
      end)

      assert_raise Postgrex.Error, ~r/goal_0006_active_session_run_identity/i, fn ->
        Repo.query!(
          "UPDATE harness_sessions SET state = 'available', active_run_id = NULL WHERE id = $1",
          [session_id]
        )
      end

      second_task_id = insert_legacy_task!("Session overlap")
      second_run_id = Ecto.UUID.bingenerate()

      Repo.query!(
        """
        INSERT INTO runs (
          id, task_id, runtime_id, generation, state, assigned_at, assignment_expires_at,
          harness_session_id, inserted_at, updated_at
        )
        VALUES ($1, $2, $3, 2, 'completed', now(), now(), $4, now(), now())
        """,
        [second_run_id, second_task_id, runtime_id, session_id]
      )

      assert_raise Postgrex.Error,
                   ~r/(runs_one_active_harness_session|goal_0006_active_session_run_identity)/i,
                   fn ->
                     Repo.transaction(fn ->
                       Repo.query!(
                         "UPDATE harness_sessions SET active_run_id = $1 WHERE id = $2",
                         [second_run_id, session_id]
                       )

                       Repo.query!("UPDATE runs SET state = 'running' WHERE id = $1", [
                         second_run_id
                       ])
                     end)
                   end

      Repo.transaction(fn ->
        Repo.query!("UPDATE runs SET state = 'completed' WHERE id = $1", [first_run_id])

        Repo.query!(
          "UPDATE harness_sessions SET state = 'available', active_run_id = NULL WHERE id = $1",
          [session_id]
        )
      end)

      Repo.transaction(fn ->
        Repo.query!("UPDATE runs SET state = 'running' WHERE id = $1", [second_run_id])

        Repo.query!(
          "UPDATE harness_sessions SET state = 'busy', active_run_id = $1 WHERE id = $2",
          [second_run_id, session_id]
        )
      end)

      assert %{rows: [[^session_id, "completed"], [^session_id, "running"]]} =
               Repo.query!(
                 "SELECT harness_session_id, state FROM runs WHERE id IN ($1, $2) ORDER BY generation",
                 [first_run_id, second_run_id]
               )

      assert_raise Postgrex.Error,
                   ~r/cannot roll back terminal Goal authority and session reciprocity guards while protected Goal or session history exists/i,
                   &migrate_terminal_authority_down!/0
    end)
  end

  test "adds a per-attachment binding and immutable stop receipt history" do
    with_schema(fn ->
      migrate_goal_up!()

      %{
        machine_id: machine_id,
        runtime_id: runtime_id,
        resource_id: resource_id,
        session_id: session_id
      } =
        insert_harness_session_fixture!()

      task_id = insert_legacy_task!("Stop receipt migration")
      run_id = Ecto.UUID.bingenerate()
      historical_task_id = insert_legacy_task!("Historical attachment binding")
      historical_run_id = Ecto.UUID.bingenerate()
      insert_harness_run!(run_id, task_id, runtime_id, session_id, 1, "completed")

      insert_harness_run!(
        historical_run_id,
        historical_task_id,
        runtime_id,
        session_id,
        1,
        "completed"
      )

      migrate_session_stop_receipt_up!()

      assert %{rows: [[session_binding_id]]} =
               Repo.query!("SELECT binding_id::text FROM harness_sessions WHERE id = $1", [
                 session_id
               ])

      assert is_binary(session_binding_id)
      {:ok, session_binding} = Ecto.UUID.dump(session_binding_id)

      assert %{rows: [[false]]} =
               Repo.query!("SELECT binding_verified FROM harness_sessions WHERE id = $1", [
                 session_id
               ])

      assert %{rows: [[run_binding_id]]} =
               Repo.query!("SELECT harness_binding_id::text FROM runs WHERE id = $1", [run_id])

      assert run_binding_id == session_binding_id

      assert %{rows: [[1]]} =
               Repo.query!(
                 "SELECT count(DISTINCT harness_binding_id) FROM runs WHERE id IN ($1, $2)",
                 [run_id, historical_run_id]
               )

      assert_raise Postgrex.Error,
                   ~r/cannot roll back harness session stop receipts while durable session state exists/i,
                   &migrate_session_stop_receipt_down!/0

      assert_raise Postgrex.Error, ~r/goal_0006_harness_session_binding_rotation_invalid/i, fn ->
        Repo.query!("UPDATE harness_sessions SET binding_id = $1 WHERE id = $2", [
          Ecto.UUID.bingenerate(),
          session_id
        ])
      end

      assert_raise Postgrex.Error, ~r/goal_0006_harness_session_binding_unverified/i, fn ->
        Repo.query!(
          "UPDATE harness_sessions SET state = 'busy', active_run_id = $1 WHERE id = $2",
          [run_id, session_id]
        )
      end

      assert_raise Postgrex.Error, ~r/goal_0006_run_harness_attachment_binding_immutable/i, fn ->
        Repo.query!("UPDATE runs SET harness_binding_id = $1 WHERE id = $2", [
          Ecto.UUID.bingenerate(),
          run_id
        ])
      end

      receipt_id = Ecto.UUID.bingenerate()
      legacy_response = Jason.encode!(%{"session_stopped" => %{"state" => "available"}})

      assert_raise Postgrex.Error, ~r/goal_0006_harness_session_stop_receipt_identity/i, fn ->
        Repo.query!(
          """
          INSERT INTO harness_session_stop_receipts (
            id, session_id, run_id, machine_id, binding_id, request_hash, response, inserted_at
          )
          VALUES ($1, $2, $3, $4, $5, $6, $7::text::jsonb, now())
          """,
          [receipt_id, session_id, run_id, machine_id, session_binding, hash(19), legacy_response]
        )
      end

      trusted_session_id = Ecto.UUID.bingenerate()
      trusted_initial_binding_id = Ecto.UUID.bingenerate()
      trusted_binding_id = Ecto.UUID.bingenerate()
      trusted_local_handle_id = Ecto.UUID.bingenerate()
      trusted_task_id = insert_legacy_task!("Verified stop receipt migration")
      trusted_run_id = Ecto.UUID.bingenerate()

      Repo.query!(
        """
        INSERT INTO harness_sessions (
          id, machine_id, runtime_id, harness_kind, harness_version, adapter_version,
          local_handle_id, repository_resource_id, workspace_fingerprint, binding_id,
          binding_verified, state, inserted_at, updated_at
        )
        VALUES ($1, $2, $3, 'codex', '1.0.0', 'adapter-1', $4, $5, 'trusted-fingerprint', $6,
                TRUE, 'available', now(), now())
        """,
        [
          trusted_session_id,
          machine_id,
          runtime_id,
          trusted_local_handle_id,
          resource_id,
          trusted_initial_binding_id
        ]
      )

      Repo.query!(
        """
        INSERT INTO runs (
          id, task_id, runtime_id, generation, state, assigned_at, assignment_expires_at,
          inserted_at, updated_at
        )
        VALUES ($1, $2, $3, 1, 'completed', now(), now(), now(), now())
        """,
        [trusted_run_id, trusted_task_id, runtime_id]
      )

      assert_raise Postgrex.Error, ~r/goal_0006_run_harness_attachment_binding_invalid/i, fn ->
        Repo.query!(
          "UPDATE runs SET harness_session_id = $1, harness_binding_id = $2 WHERE id = $3",
          [trusted_session_id, Ecto.UUID.bingenerate(), trusted_run_id]
        )
      end

      Repo.query!(
        """
        UPDATE harness_sessions
        SET state = 'busy', active_run_id = $1, binding_id = $2
        WHERE id = $3
        """,
        [trusted_run_id, trusted_binding_id, trusted_session_id]
      )

      Repo.query!(
        "UPDATE runs SET harness_session_id = $1, harness_binding_id = $2 WHERE id = $3",
        [trusted_session_id, trusted_binding_id, trusted_run_id]
      )

      Repo.query!(
        "UPDATE harness_sessions SET state = 'unavailable', active_run_id = NULL WHERE id = $1",
        [trusted_session_id]
      )

      response =
        Jason.encode!(%{
          "session_stopped" => %{
            "receipt_id" => Ecto.UUID.load!(receipt_id),
            "run_id" => Ecto.UUID.load!(trusted_run_id),
            "session_id" => Ecto.UUID.load!(trusted_session_id),
            "local_handle_id" => Ecto.UUID.load!(trusted_local_handle_id),
            "binding_id" => Ecto.UUID.load!(trusted_binding_id),
            "state" => "available",
            "active_run_id" => nil
          }
        })

      Repo.query!(
        """
        INSERT INTO harness_session_stop_receipts (
          id, session_id, run_id, machine_id, binding_id, request_hash, response, inserted_at
        )
        VALUES ($1, $2, $3, $4, $5, $6, $7::text::jsonb, now())
        """,
        [
          receipt_id,
          trusted_session_id,
          trusted_run_id,
          machine_id,
          trusted_binding_id,
          hash(20),
          response
        ]
      )

      assert_raise Postgrex.Error, ~r/goal_0006_harness_session_stop_receipt_immutable/i, fn ->
        Repo.query!("DELETE FROM harness_session_stop_receipts WHERE id = $1", [receipt_id])
      end

      assert_raise Postgrex.Error,
                   ~r/harness_session_stop_receipts_session_id_binding_id_key/i,
                   fn ->
                     duplicate_receipt_id = Ecto.UUID.bingenerate()

                     duplicate_response =
                       Jason.encode!(%{
                         "session_stopped" => %{
                           "receipt_id" => Ecto.UUID.load!(duplicate_receipt_id),
                           "run_id" => Ecto.UUID.load!(trusted_run_id),
                           "session_id" => Ecto.UUID.load!(trusted_session_id),
                           "local_handle_id" => Ecto.UUID.load!(trusted_local_handle_id),
                           "binding_id" => Ecto.UUID.load!(trusted_binding_id),
                           "state" => "available",
                           "active_run_id" => nil
                         }
                       })

                     Repo.query!(
                       """
                       INSERT INTO harness_session_stop_receipts (
                         id, session_id, run_id, machine_id, binding_id, request_hash, response, inserted_at
                       )
                       VALUES ($1, $2, $3, $4, $5, $6, $7::text::jsonb, now())
                       """,
                       [
                         duplicate_receipt_id,
                         trusted_session_id,
                         trusted_run_id,
                         machine_id,
                         trusted_binding_id,
                         hash(21),
                         duplicate_response
                       ]
                     )
                   end

      assert_raise Postgrex.Error,
                   ~r/cannot roll back harness session stop receipts while durable session state exists/i,
                   &migrate_session_stop_receipt_down!/0
    end)
  end

  test "rolls back stop receipt storage only after every legacy session is closed" do
    with_schema(fn ->
      migrate_goal_up!()
      %{session_id: session_id} = insert_harness_session_fixture!()

      Repo.query!("UPDATE harness_sessions SET state = 'closed' WHERE id = $1", [session_id])
      migrate_session_stop_receipt_up!()

      assert %{rows: [[false, "closed"]]} =
               Repo.query!(
                 "SELECT binding_verified, state FROM harness_sessions WHERE id = $1",
                 [session_id]
               )

      migrate_session_stop_receipt_down!()

      assert %{rows: [[0]]} =
               Repo.query!("""
               SELECT count(*)
               FROM information_schema.columns
               WHERE table_schema = current_schema()
                 AND table_name = 'harness_sessions'
                 AND column_name IN ('binding_id', 'binding_verified')
               """)
    end)
  end

  test "adds immutable attach receipt storage without reconstructing legacy history" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_session_stop_receipt_up!()
      migrate_session_attach_receipt_up!()

      assert %{rows: [[12]]} =
               Repo.query!("""
               SELECT count(*)
               FROM information_schema.columns
               WHERE table_schema = current_schema()
                 AND table_name = 'harness_session_attach_receipts'
               """)

      migrate_session_attach_receipt_down!()

      assert %{rows: [[0]]} =
               Repo.query!("""
               SELECT count(*)
               FROM information_schema.tables
               WHERE table_schema = current_schema()
                 AND table_name = 'harness_session_attach_receipts'
               """)
    end)
  end

  test "guards terminal Goal authority, terminal decisions, and exact execution policies in SQL" do
    with_schema(fn ->
      migrate_goal_up!()
      %{goal_id: goal_id, project_id: project_id} = insert_goal_fixture!()

      Repo.query!("UPDATE goals SET state = 'achieved', next_wake_at = now() WHERE id = $1", [
        goal_id
      ])

      migrate_terminal_policy_up!()

      assert %{rows: [["achieved", nil, nil, "false"]]} =
               Repo.query!(
                 """
                 SELECT state,
                        next_wake_at,
                        execution_policy ->> 'per_run_cost_limit_microusd',
                        execution_policy ->> 'hard_cost_limit_required'
                 FROM goals
                 JOIN goal_revisions ON goal_revisions.goal_id = goals.id
                 WHERE goals.id = $1 AND revision = 1
                 """,
                 [goal_id]
               )

      assert_raise Postgrex.Error, ~r/goal_0006_immutable_history/i, fn ->
        Repo.query!(
          "UPDATE goal_revisions SET reason = 'rewritten history' WHERE goal_id = $1 AND revision = 1",
          [goal_id]
        )
      end

      assert_raise Postgrex.Error, ~r/goal_0006_decision_transition_invalid/i, fn ->
        insert_resolved_goal_decision!(goal_id)
      end

      for terminal_state <- ["achieved", "cancelled"] do
        %{goal_id: terminal_goal_id} = insert_goal_fixture!(protected_execution_policy_json(%{}))

        Repo.query!("UPDATE goals SET state = $1, next_wake_at = NULL WHERE id = $2", [
          terminal_state,
          terminal_goal_id
        ])

        assert_raise Postgrex.Error, ~r/goal_0006_terminal_goal_immutable/i, fn ->
          Repo.query!("UPDATE goals SET state = 'active' WHERE id = $1", [terminal_goal_id])
        end

        assert_raise Postgrex.Error, ~r/goal_0006_terminal_goal_immutable/i, fn ->
          Repo.query!("UPDATE goals SET current_revision = 2 WHERE id = $1", [terminal_goal_id])
        end

        assert_raise Postgrex.Error, ~r/goal_0006_terminal_goal_immutable/i, fn ->
          Repo.query!("UPDATE goals SET project_id = $1 WHERE id = $2", [
            project_id,
            terminal_goal_id
          ])
        end

        assert_raise Postgrex.Error, ~r/goals_terminal_next_wake_at_null/i, fn ->
          Repo.query!("UPDATE goals SET next_wake_at = now() WHERE id = $1", [terminal_goal_id])
        end

        Repo.query!(
          """
          UPDATE goals
          SET event_sequence = event_sequence + 1, lock_version = lock_version + 1, updated_at = now()
          WHERE id = $1
          """,
          [terminal_goal_id]
        )

        assert %{rows: [[1, 2]]} =
                 Repo.query!(
                   "SELECT event_sequence, lock_version FROM goals WHERE id = $1",
                   [terminal_goal_id]
                 )
      end

      open_decision_id = insert_open_goal_decision!(goal_id)

      Repo.query!(
        """
        UPDATE goal_decisions
        SET state = 'resolved', resolution = '{"option_id": "accept"}'::jsonb
        WHERE id = $1
        """,
        [open_decision_id]
      )

      superseded_decision_id = insert_open_goal_decision!(goal_id)

      Repo.query!("UPDATE goal_decisions SET state = 'superseded' WHERE id = $1", [
        superseded_decision_id
      ])

      assert_raise Postgrex.Error, ~r/goal_0006_terminal_decision_immutable/i, fn ->
        Repo.query!("UPDATE goal_decisions SET state = 'open' WHERE id = $1", [
          superseded_decision_id
        ])
      end

      assert_raise Postgrex.Error, ~r/goal_0006_terminal_decision_immutable/i, fn ->
        Repo.query!("UPDATE goal_decisions SET question = 'Repurpose authority?' WHERE id = $1", [
          superseded_decision_id
        ])
      end

      assert_raise Postgrex.Error, ~r/goal_0006_terminal_decision_immutable/i, fn ->
        Repo.query!("DELETE FROM goal_decisions WHERE id = $1", [superseded_decision_id])
      end

      manual_strict_policy =
        protected_execution_policy_json(%{
          "budget_mode" => "strict",
          "per_run_cost_limit_microusd" => 0,
          "hard_cost_limit_required" => true
        })

      insert_goal_revision_policy!(goal_id, 2, manual_strict_policy)

      assert_raise Postgrex.Error, ~r/goal_revisions_execution_policy_v1/i, fn ->
        insert_goal_revision_policy!(
          goal_id,
          3,
          protected_execution_policy_json(%{"automatic_execution" => true})
        )
      end

      assert_raise Postgrex.Error, ~r/goal_revisions_execution_policy_v1/i, fn ->
        insert_goal_revision_policy!(
          goal_id,
          3,
          protected_execution_policy_json(%{"per_run_cost_limit_microusd" => -1})
        )
      end

      assert_raise Postgrex.Error, ~r/goal_revisions_execution_policy_v1/i, fn ->
        insert_goal_revision_policy!(
          goal_id,
          3,
          protected_execution_policy_json(%{
            "budget_mode" => "strict",
            "per_run_cost_limit_microusd" => nil,
            "hard_cost_limit_required" => false
          })
        )
      end

      assert_raise Postgrex.Error, ~r/goal_revisions_execution_policy_v1/i, fn ->
        insert_goal_revision_policy!(
          goal_id,
          3,
          protected_execution_policy_json(%{"unexpected" => true})
        )
      end
    end)
  end

  test "guards terminal Goal parent authority while preserving nonterminal decisions and late audit history" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_terminal_policy_up!()
      migrate_terminal_authority_up!()

      %{goal_id: goal_id, work_item_id: work_item_id} =
        insert_goal_fixture!(protected_execution_policy_json(%{}))

      insert_goal_revision_policy!(goal_id, 2, protected_execution_policy_json(%{}))

      resolved_decision_id = insert_open_goal_decision!(goal_id)

      Repo.query!("UPDATE goal_decisions SET question = 'Review the final Goal?' WHERE id = $1", [
        resolved_decision_id
      ])

      mutable_decision_id = insert_open_goal_decision!(goal_id)
      late_usage_task_id = insert_goal_task!(%{goal_id: goal_id, work_item_id: work_item_id})
      %{runtime_id: runtime_id, session_id: session_id} = insert_harness_session_fixture!()
      late_usage_run_id = Ecto.UUID.bingenerate()

      insert_harness_run!(
        late_usage_run_id,
        late_usage_task_id,
        runtime_id,
        session_id,
        1,
        "completed"
      )

      Repo.transaction(fn ->
        Repo.query!(
          """
          UPDATE goal_decisions
          SET state = 'resolved', resolution = '{"option_id": "accept"}'::jsonb
          WHERE id = $1
          """,
          [resolved_decision_id]
        )

        Repo.query!("UPDATE goals SET state = 'achieved', next_wake_at = NULL WHERE id = $1", [
          goal_id
        ])
      end)

      assert_raise Postgrex.Error, ~r/goal_0006_terminal_goal_authority/i, fn ->
        insert_goal_revision_policy!(goal_id, 3, protected_execution_policy_json(%{}))
      end

      assert_raise Postgrex.Error, ~r/goal_0006_terminal_goal_authority/i, fn ->
        insert_open_goal_decision!(goal_id)
      end

      assert_raise Postgrex.Error, ~r/goal_0006_terminal_goal_authority/i, fn ->
        Repo.query!("UPDATE goal_decisions SET question = 'Rewrite authority?' WHERE id = $1", [
          mutable_decision_id
        ])
      end

      assert_raise Postgrex.Error, ~r/goal_0006_terminal_goal_authority/i, fn ->
        Repo.query!(
          """
          UPDATE goal_decisions
          SET state = 'resolved', resolution = '{"option_id": "accept"}'::jsonb
          WHERE id = $1
          """,
          [mutable_decision_id]
        )
      end

      assert_raise Postgrex.Error, ~r/goal_0006_terminal_goal_authority/i, fn ->
        Repo.query!("UPDATE goal_decisions SET lock_version = lock_version + 1 WHERE id = $1", [
          resolved_decision_id
        ])
      end

      %{goal_id: active_goal_id} = insert_goal_fixture!(protected_execution_policy_json(%{}))

      assert_raise Postgrex.Error, ~r/goal_0006_terminal_goal_authority/i, fn ->
        Repo.query!(
          "UPDATE goal_decisions SET goal_id = $1, goal_revision = 1 WHERE id = $2",
          [active_goal_id, mutable_decision_id]
        )
      end

      assert_raise Postgrex.Error, ~r/goal_0006_terminal_goal_authority/i, fn ->
        Repo.query!("DELETE FROM goal_decisions WHERE id = $1", [mutable_decision_id])
      end

      audit_event_id = Ecto.UUID.bingenerate()

      Repo.query!(
        """
        INSERT INTO goal_events (
          id, goal_id, sequence, kind, actor_ref, request_hash, request_hash_version, revision,
          payload, response, inserted_at
        )
        VALUES ($1, $2, 1, 'settled', 'operator:test', $3, 1, 1, '{}'::jsonb, '{}'::jsonb, now())
        """,
        [audit_event_id, goal_id, hash(23)]
      )

      assert %{rows: [[^audit_event_id]]} =
               Repo.query!("SELECT id FROM goal_events WHERE id = $1", [audit_event_id])

      insert_run_usage!(late_usage_run_id, "late-terminal-usage", "reported", 1)

      assert %{rows: [["late-terminal-usage"]]} =
               Repo.query!("SELECT usage_key FROM run_usage WHERE run_id = $1", [
                 late_usage_run_id
               ])

      %{goal_id: cancelled_goal_id} = insert_goal_fixture!(protected_execution_policy_json(%{}))
      cancelled_open_decision_id = insert_open_goal_decision!(cancelled_goal_id)

      Repo.query!("UPDATE goals SET state = 'cancelled', next_wake_at = NULL WHERE id = $1", [
        cancelled_goal_id
      ])

      assert_raise Postgrex.Error, ~r/goal_0006_terminal_goal_authority/i, fn ->
        insert_goal_revision_policy!(cancelled_goal_id, 2, protected_execution_policy_json(%{}))
      end

      assert_raise Postgrex.Error, ~r/goal_0006_terminal_goal_authority/i, fn ->
        Repo.query!("DELETE FROM goal_decisions WHERE id = $1", [cancelled_open_decision_id])
      end

      assert_raise Postgrex.Error, ~r/goal_0006_terminal_goal_authority/i, fn ->
        insert_open_goal_decision!(cancelled_goal_id)
      end
    end)
  end

  test "uses final deferred session and run rows while retaining completed run session history" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()

      migrate_baseline_up!()
      migrate_identity_up!()
      migrate_terminal_policy_up!()
      migrate_terminal_authority_up!()

      %{runtime_id: runtime_id, session_id: session_id} = insert_harness_session_fixture!()
      first_task_id = insert_legacy_task!("First session turn")
      second_task_id = insert_legacy_task!("Second session turn")
      third_task_id = insert_legacy_task!("Third session turn")
      first_run_id = Ecto.UUID.bingenerate()
      second_run_id = Ecto.UUID.bingenerate()
      third_run_id = Ecto.UUID.bingenerate()

      Repo.transaction(fn ->
        insert_harness_run!(first_run_id, first_task_id, runtime_id, session_id, 1, "running")

        Repo.query!(
          "UPDATE harness_sessions SET state = 'busy', active_run_id = $1 WHERE id = $2",
          [first_run_id, session_id]
        )
      end)

      assert_raise Postgrex.Error, ~r/goal_0006_active_session_run_identity/i, fn ->
        Repo.query!("UPDATE runs SET state = 'completed' WHERE id = $1", [first_run_id])
      end

      assert_raise Postgrex.Error, ~r/goal_0006_active_session_run_identity/i, fn ->
        Repo.query!(
          "UPDATE harness_sessions SET state = 'available', active_run_id = NULL WHERE id = $1",
          [session_id]
        )
      end

      Repo.transaction(fn ->
        Repo.query!(
          "UPDATE harness_sessions SET state = 'available', active_run_id = NULL WHERE id = $1",
          [session_id]
        )

        Repo.query!("UPDATE runs SET state = 'completed' WHERE id = $1", [first_run_id])
      end)

      insert_harness_run!(second_run_id, second_task_id, runtime_id, session_id, 2, "completed")

      Repo.transaction(fn ->
        Repo.query!("UPDATE runs SET state = 'running' WHERE id = $1", [second_run_id])

        Repo.query!(
          "UPDATE harness_sessions SET state = 'busy', active_run_id = $1 WHERE id = $2",
          [second_run_id, session_id]
        )
      end)

      insert_harness_run!(third_run_id, third_task_id, runtime_id, session_id, 3, "completed")

      Repo.transaction(fn ->
        Repo.query!("UPDATE runs SET state = 'completed' WHERE id = $1", [second_run_id])

        Repo.query!(
          "UPDATE harness_sessions SET state = 'available', active_run_id = NULL WHERE id = $1",
          [session_id]
        )

        Repo.query!("UPDATE runs SET state = 'running' WHERE id = $1", [third_run_id])

        Repo.query!(
          "UPDATE harness_sessions SET state = 'busy', active_run_id = $1 WHERE id = $2",
          [third_run_id, session_id]
        )
      end)

      assert %{rows: [["busy", ^third_run_id]]} =
               Repo.query!("SELECT state, active_run_id FROM harness_sessions WHERE id = $1", [
                 session_id
               ])

      assert %{
               rows: [
                 [^session_id, "completed"],
                 [^session_id, "completed"],
                 [^session_id, "running"]
               ]
             } =
               Repo.query!(
                 "SELECT harness_session_id, state FROM runs WHERE id IN ($1, $2, $3) ORDER BY generation",
                 [first_run_id, second_run_id, third_run_id]
               )

      assert_raise Postgrex.Error,
                   ~r/cannot roll back terminal Goal authority and session reciprocity guards while protected Goal or session history exists/i,
                   &migrate_terminal_authority_down!/0
    end)
  end

  test "refuses inconsistent legacy session and run rows before installing final-state reciprocity" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()
      migrate_baseline_up!()
      migrate_identity_up!()
      migrate_terminal_policy_up!()

      %{runtime_id: runtime_id, session_id: session_id} = insert_harness_session_fixture!()
      task_id = insert_legacy_task!("Inconsistent session run")

      Repo.query!("ALTER TABLE runs DISABLE TRIGGER runs_active_session_identity_guard")

      try do
        insert_harness_run!(
          Ecto.UUID.bingenerate(),
          task_id,
          runtime_id,
          session_id,
          1,
          "running"
        )
      after
        Repo.query!("ALTER TABLE runs ENABLE TRIGGER runs_active_session_identity_guard")
      end

      assert_raise Postgrex.Error,
                   ~r/cannot add Goal final session\/run reciprocity guard while existing session\/run relationships are inconsistent/i,
                   &migrate_terminal_authority_up!/0
    end)
  end

  test "refuses a legacy terminal Run that remains active in a session" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()
      migrate_baseline_up!()
      migrate_identity_up!()
      migrate_terminal_policy_up!()

      %{runtime_id: runtime_id, session_id: session_id} = insert_harness_session_fixture!()
      task_id = insert_legacy_task!("Terminal active session run")
      run_id = Ecto.UUID.bingenerate()

      insert_harness_run!(run_id, task_id, runtime_id, session_id, 1, "completed")

      Repo.query!(
        "ALTER TABLE harness_sessions DISABLE TRIGGER harness_sessions_active_run_identity_guard"
      )

      try do
        Repo.query!(
          "UPDATE harness_sessions SET state = 'busy', active_run_id = $1 WHERE id = $2",
          [
            run_id,
            session_id
          ]
        )
      after
        Repo.query!(
          "ALTER TABLE harness_sessions ENABLE TRIGGER harness_sessions_active_run_identity_guard"
        )
      end

      assert_raise Postgrex.Error,
                   ~r/cannot add Goal final session\/run reciprocity guard while existing session\/run relationships are inconsistent/i,
                   &migrate_terminal_authority_up!/0
    end)
  end

  test "terminal authority and final-state reciprocity guards roll back only without protected Goal or active session history" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()

      %{goal_id: goal_id} = insert_goal_fixture!(protected_execution_policy_json(%{}), true)

      migrate_baseline_up!()
      migrate_identity_up!()
      migrate_terminal_policy_up!()
      migrate_terminal_authority_up!()

      %{runtime_id: runtime_id, session_id: session_id} = insert_harness_session_fixture!()
      task_id = insert_legacy_task!("Released session history")

      insert_harness_run!(
        Ecto.UUID.bingenerate(),
        task_id,
        runtime_id,
        session_id,
        1,
        "completed"
      )

      migrate_terminal_authority_down!()
      migrate_terminal_authority_up!()

      Repo.query!("UPDATE goals SET state = 'cancelled', next_wake_at = NULL WHERE id = $1", [
        goal_id
      ])

      assert_raise Postgrex.Error,
                   ~r/cannot roll back terminal Goal authority and session reciprocity guards while protected Goal or session history exists/i,
                   &migrate_terminal_authority_down!/0
    end)
  end

  test "refuses terminal policy rollback after protected history exists" do
    with_schema(fn ->
      migrate_goal_up!()
      %{goal_id: goal_id} = insert_goal_fixture!()

      insert_goal_revision_policy!(
        goal_id,
        2,
        protected_execution_policy_json(%{
          "per_run_cost_limit_microusd" => 1,
          "hard_cost_limit_required" => true
        })
      )

      migrate_terminal_policy_up!()

      assert_raise Postgrex.Error,
                   ~r/cannot roll back terminal Goal guards and execution policy while protected history exists/i,
                   &migrate_terminal_policy_down!/0
    end)
  end

  test "normalizes immutable legacy execution policies additively without rewriting explicit values" do
    with_schema(fn ->
      migrate_goal_up!()
      %{goal_id: goal_id} = insert_goal_fixture!()

      insert_goal_revision_policy!(
        goal_id,
        2,
        legacy_execution_policy_json(%{"per_run_cost_limit_microusd" => 17})
      )

      migrate_terminal_policy_up!()

      assert %{rows: [[nil, false], [17, false]]} =
               Repo.query!(
                 """
                 SELECT
                   (execution_policy ->> 'per_run_cost_limit_microusd')::bigint,
                   (execution_policy ->> 'hard_cost_limit_required')::boolean
                 FROM goal_revisions
                 WHERE goal_id = $1
                 ORDER BY revision
                 """,
                 [goal_id]
               )

      assert_raise Postgrex.Error, ~r/goal_0006_immutable_history/i, fn ->
        Repo.query!(
          "UPDATE goal_revisions SET reason = 'rewritten history' WHERE goal_id = $1 AND revision = 2",
          [goal_id]
        )
      end

      assert_raise Postgrex.Error,
                   ~r/cannot roll back terminal Goal guards and execution policy while protected history exists/i,
                   &migrate_terminal_policy_down!/0
    end)
  end

  test "rolls terminal policy guards back and reapplies after default-only legacy normalization" do
    with_schema(fn ->
      migrate_goal_up!()
      %{goal_id: goal_id} = insert_goal_fixture!()
      migrate_terminal_policy_up!()
      migrate_terminal_policy_down!()

      assert %{rows: [[nil, false]]} =
               Repo.query!(
                 """
                 SELECT
                   (execution_policy ->> 'per_run_cost_limit_microusd')::bigint,
                   (execution_policy ->> 'hard_cost_limit_required')::boolean
                 FROM goal_revisions
                 WHERE goal_id = $1 AND revision = 1
                 """,
                 [goal_id]
               )

      migrate_terminal_policy_up!()
    end)
  end

  test "refuses incompatible automatic and strict legacy execution policies" do
    for {overrides, expected_error} <- [
          {%{"automatic_execution" => true},
           ~r/cannot require automatic Goal budget while existing automatic revisions have no total budget/i},
          {%{"budget_mode" => "strict"},
           ~r/cannot require strict Goal cost ceiling while existing strict revisions are not enforceable/i},
          {%{"per_run_cost_limit_microusd" => "1"},
           ~r/cannot add exact execution policy guard while revisions contain an invalid per-run cost limit/i},
          {%{"hard_cost_limit_required" => "false"},
           ~r/cannot add exact execution policy guard while revisions contain an invalid hard-cost-limit requirement/i}
        ] do
      with_schema(fn ->
        migrate_goal_up!()
        %{goal_id: goal_id} = insert_goal_fixture!()
        insert_goal_revision_policy!(goal_id, 2, legacy_execution_policy_json(overrides))

        assert_raise Postgrex.Error, expected_error, &migrate_terminal_policy_up!/0
      end)
    end
  end

  test "terminal policy migration rolls back and reapplies on a clean schema" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_terminal_policy_up!()
      migrate_terminal_policy_down!()

      assert %{rows: []} =
               Repo.query!("""
               SELECT 1
               FROM pg_constraint
               WHERE conrelid = 'goals'::regclass
                 AND conname = 'goals_terminal_next_wake_at_null'
               """)

      migrate_terminal_policy_up!()

      assert %{rows: [["goals_terminal_next_wake_at_null"]]} =
               Repo.query!("""
               SELECT conname
               FROM pg_constraint
               WHERE conrelid = 'goals'::regclass
                 AND conname = 'goals_terminal_next_wake_at_null'
               """)
    end)
  end

  test "persists only immutable, terminal, same-scope handoff source Run lineage" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()

      %{goal_id: goal_id, project_id: project_id, work_item_id: work_item_id} =
        insert_goal_fixture!(protected_execution_policy_json(%{}), true)

      source_task_id = insert_goal_task!(%{goal_id: goal_id, work_item_id: work_item_id})

      missing_input_work_item_id = insert_goal_work_item!(project_id, goal_id)
      different_input_work_item_id = insert_goal_work_item!(project_id, goal_id)
      noncanonical_input_work_item_id = insert_goal_work_item!(project_id, goal_id)

      %{goal_id: other_goal_id, work_item_id: other_goal_work_item_id} =
        insert_goal_fixture!(protected_execution_policy_json(%{}), true)

      same_goal_other_work_item_id = insert_goal_work_item!(project_id, goal_id)

      revision_goal_id = Ecto.UUID.bingenerate()
      revision_project_id = insert_project!()
      insert_goal_row!(revision_goal_id, revision_project_id)

      revision_work_item_id =
        insert_designated_goal_work_item!(revision_project_id, revision_goal_id)

      revision_source_task_id =
        insert_goal_task!(%{goal_id: revision_goal_id, work_item_id: revision_work_item_id})

      migrate_baseline_up!()
      migrate_identity_up!()
      migrate_planning_task_up!()
      migrate_terminal_policy_up!()
      migrate_terminal_authority_up!()
      migrate_handoff_lineage_up!()

      source_result = %{"kind" => "progress"}

      Repo.query!(
        "UPDATE tasks SET state = 'completed', current_generation = 1, result = $1::text::jsonb WHERE id = $2",
        [Jason.encode!(source_result), source_task_id]
      )

      Repo.query!("UPDATE tasks SET state = 'completed', current_generation = 1 WHERE id = $1", [
        revision_source_task_id
      ])

      %{runtime_id: runtime_id} = insert_harness_session_fixture!()
      source_run_id = insert_terminal_goal_run!(source_task_id, runtime_id, "completed")

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_run_ineligible/i, fn ->
        insert_handoff_task!(
          goal_id,
          work_item_id,
          1,
          source_run_id,
          handoff_input(source_run_id)
        )
      end

      Repo.query!("UPDATE runs SET result = $1::text::jsonb WHERE id = $2", [
        Jason.encode!(source_result),
        source_run_id
      ])

      target_task_id =
        insert_handoff_task!(
          goal_id,
          work_item_id,
          1,
          source_run_id,
          handoff_input(source_run_id)
        )

      assert %{rows: [[^source_run_id]]} =
               Repo.query!("SELECT handoff_source_run_id FROM tasks WHERE id = $1", [
                 target_task_id
               ])

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_lineage_immutable/i, fn ->
        Repo.query!("UPDATE tasks SET handoff_source_run_id = NULL WHERE id = $1", [
          target_task_id
        ])
      end

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_run_immutable/i, fn ->
        Repo.query!("UPDATE runs SET state = 'failed' WHERE id = $1", [source_run_id])
      end

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_run_immutable/i, fn ->
        Repo.query!("UPDATE runs SET generation = generation + 1 WHERE id = $1", [source_run_id])
      end

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_run_immutable/i, fn ->
        Repo.query!("UPDATE runs SET runtime_id = $1 WHERE id = $2", [
          Ecto.UUID.bingenerate(),
          source_run_id
        ])
      end

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_run_immutable/i, fn ->
        Repo.query!("UPDATE runs SET result = '{}'::jsonb WHERE id = $1", [source_run_id])
      end

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_task_immutable/i, fn ->
        Repo.query!("UPDATE tasks SET state = 'failed' WHERE id = $1", [source_task_id])
      end

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_task_immutable/i, fn ->
        Repo.query!(
          "UPDATE tasks SET current_generation = current_generation + 1 WHERE id = $1",
          [
            source_task_id
          ]
        )
      end

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_task_immutable/i, fn ->
        Repo.query!("UPDATE tasks SET result = '{}'::jsonb WHERE id = $1", [source_task_id])
      end

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_plan_unsupported/i, fn ->
        insert_planning_handoff_task!(goal_id, source_run_id, handoff_input(source_run_id))
      end

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_requires_handoff_mode/i, fn ->
        insert_handoff_task!(goal_id, work_item_id, 1, source_run_id, %{"session_mode" => "fresh"})
      end

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_requires_handoff_mode/i, fn ->
        insert_handoff_task!(goal_id, work_item_id, 1, nil, %{
          "session_mode" => "fresh",
          "handoff_source_run_id" => nil
        })
      end

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_run_mismatch/i, fn ->
        insert_handoff_task!(goal_id, missing_input_work_item_id, 1, source_run_id, %{
          "session_mode" => "handoff"
        })
      end

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_run_mismatch/i, fn ->
        insert_handoff_task!(
          goal_id,
          different_input_work_item_id,
          1,
          source_run_id,
          handoff_input(Ecto.UUID.bingenerate())
        )
      end

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_run_mismatch/i, fn ->
        insert_handoff_task!(
          goal_id,
          noncanonical_input_work_item_id,
          1,
          source_run_id,
          %{
            "session_mode" => "handoff",
            "handoff_source_run_id" => String.upcase(Ecto.UUID.cast!(source_run_id))
          }
        )
      end

      missing_source_run_id = Ecto.UUID.bingenerate()

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_run_ineligible/i, fn ->
        insert_handoff_task!(
          goal_id,
          work_item_id,
          1,
          missing_source_run_id,
          handoff_input(missing_source_run_id)
        )
      end

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_goal_mismatch/i, fn ->
        insert_handoff_task!(
          other_goal_id,
          other_goal_work_item_id,
          1,
          source_run_id,
          handoff_input(source_run_id)
        )
      end

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_work_item_mismatch/i, fn ->
        insert_handoff_task!(
          goal_id,
          same_goal_other_work_item_id,
          1,
          source_run_id,
          handoff_input(source_run_id)
        )
      end

      planning_source_task_id = insert_planning_task!(goal_id)

      planning_source_run_id =
        insert_terminal_goal_run!(planning_source_task_id, runtime_id, "completed")

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_run_ineligible/i, fn ->
        insert_handoff_task!(
          goal_id,
          work_item_id,
          1,
          planning_source_run_id,
          handoff_input(planning_source_run_id)
        )
      end

      generation_source_task_id =
        insert_goal_task!(%{goal_id: goal_id, work_item_id: same_goal_other_work_item_id})

      Repo.query!(
        "UPDATE tasks SET state = 'completed', current_generation = 2, attempt_generation = 2 WHERE id = $1",
        [generation_source_task_id]
      )

      generation_source_run_id =
        insert_terminal_goal_run!(generation_source_task_id, runtime_id, "completed")

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_run_ineligible/i, fn ->
        insert_handoff_task!(
          goal_id,
          same_goal_other_work_item_id,
          1,
          generation_source_run_id,
          handoff_input(generation_source_run_id)
        )
      end

      failed_source_task_id =
        insert_goal_task!(%{goal_id: goal_id, work_item_id: same_goal_other_work_item_id})

      Repo.query!("UPDATE tasks SET state = 'completed' WHERE id = $1", [failed_source_task_id])

      failed_source_run_id =
        insert_terminal_goal_run!(failed_source_task_id, runtime_id, "failed")

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_run_ineligible/i, fn ->
        insert_handoff_task!(
          goal_id,
          same_goal_other_work_item_id,
          1,
          failed_source_run_id,
          handoff_input(failed_source_run_id)
        )
      end

      Repo.query!("UPDATE tasks SET state = 'completed' WHERE id = $1", [revision_source_task_id])

      revision_source_run_id =
        insert_terminal_goal_run!(revision_source_task_id, runtime_id, "completed")

      insert_goal_revision_policy!(revision_goal_id, 2, protected_execution_policy_json(%{}))
      Repo.query!("UPDATE goals SET current_revision = 2 WHERE id = $1", [revision_goal_id])

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_requires_current_goal_revision/i, fn ->
        insert_handoff_task!(
          revision_goal_id,
          revision_work_item_id,
          1,
          revision_source_run_id,
          handoff_input(revision_source_run_id)
        )
      end

      assert_raise Postgrex.Error, ~r/goal_0006_handoff_source_revision_mismatch/i, fn ->
        insert_handoff_task!(
          revision_goal_id,
          revision_work_item_id,
          2,
          revision_source_run_id,
          handoff_input(revision_source_run_id)
        )
      end

      Repo.query!("UPDATE tasks SET state = 'completed' WHERE id = $1", [target_task_id])

      assert_raise Postgrex.Error, ~r/tasks_handoff_source_run_id_key/i, fn ->
        insert_handoff_task!(
          goal_id,
          work_item_id,
          1,
          source_run_id,
          handoff_input(source_run_id)
        )
      end

      assert_raise Postgrex.Error,
                   ~r/cannot roll back task handoff lineage while source provenance exists/i,
                   &migrate_handoff_lineage_down!/0
    end)
  end

  test "serializes handoff lineage admission against source and Goal authority changes" do
    with_schema(fn ->
      migrate_goal_up!()
      migrate_integration_designation_up!()

      %{goal_id: goal_id, work_item_id: work_item_id} =
        insert_goal_fixture!(protected_execution_policy_json(%{}), true)

      source_task_id = insert_goal_task!(%{goal_id: goal_id, work_item_id: work_item_id})

      migrate_baseline_up!()
      migrate_identity_up!()
      migrate_planning_task_up!()
      migrate_terminal_policy_up!()
      migrate_terminal_authority_up!()
      migrate_handoff_lineage_up!()

      Repo.query!("UPDATE tasks SET state = 'completed', current_generation = 1 WHERE id = $1", [
        source_task_id
      ])

      %{runtime_id: runtime_id} = insert_harness_session_fixture!()
      source_run_id = insert_terminal_goal_run!(source_task_id, runtime_id, "completed")
      parent = self()

      assert %{rows: [[schema]]} = Repo.query!("SELECT current_schema()")
      handoff_repo = start_schema_repo(schema)
      contender_repo = start_schema_repo(schema)

      handoff_task =
        Elixir.Task.async(fn ->
          previous_dynamic_repo = Repo.put_dynamic_repo(handoff_repo)

          try do
            Repo.transaction(fn ->
              task_id =
                insert_handoff_task!(
                  goal_id,
                  work_item_id,
                  1,
                  source_run_id,
                  handoff_input(source_run_id)
                )

              send(parent, {:handoff_lineage_locks_held, self()})

              receive do
                :commit_handoff_lineage -> task_id
              after
                15_000 -> raise "timed out waiting to commit handoff lineage"
              end
            end)
          after
            Repo.put_dynamic_repo(previous_dynamic_repo)
          end
        end)

      try do
        assert_receive {:handoff_lineage_locks_held, _handoff_pid}, 15_000

        assert_handoff_lineage_lock_timeout!(contender_repo, fn ->
          Repo.query!("UPDATE goals SET event_sequence = event_sequence + 1 WHERE id = $1", [
            goal_id
          ])
        end)

        assert_handoff_lineage_lock_timeout!(contender_repo, fn ->
          Repo.query!("UPDATE work_items SET position = position + 1 WHERE id = $1", [
            work_item_id
          ])
        end)

        assert_handoff_lineage_lock_timeout!(contender_repo, fn ->
          Repo.query!("UPDATE tasks SET state = 'failed' WHERE id = $1", [source_task_id])
        end)

        assert_handoff_lineage_lock_timeout!(contender_repo, fn ->
          Repo.query!("UPDATE runs SET state = 'failed' WHERE id = $1", [source_run_id])
        end)

        send(handoff_task.pid, :commit_handoff_lineage)
        assert {:ok, _target_task_id} = Elixir.Task.await(handoff_task, 15_000)
      after
        if Process.alive?(handoff_task.pid) do
          send(handoff_task.pid, :commit_handoff_lineage)
          Elixir.Task.shutdown(handoff_task, :brutal_kill)
        end

        GenServer.stop(contender_repo)
        GenServer.stop(handoff_repo)
      end
    end)
  end

  defp with_schema(test) do
    load_migration_modules!()

    schema =
      "goal_0006_migration_#{System.system_time(:microsecond)}_#{System.unique_integer([:positive])}"

    create_schema!(schema)

    try do
      repo = start_schema_repo(schema)
      previous_dynamic_repo = Repo.put_dynamic_repo(repo)

      try do
        migrate_base_up!()
        test.()
      after
        Repo.put_dynamic_repo(previous_dynamic_repo)
        GenServer.stop(repo)
      end
    after
      drop_schema!(schema)
    end
  end

  defp migrate_base_up! do
    Ecto.Migrator.run(Repo, base_migrations(), :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_goal_up! do
    Ecto.Migrator.run(Repo, [{@migration_version, AddGoal0006ControlPlane}], :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_goal_down! do
    Ecto.Migrator.run(
      Repo,
      base_migrations() ++ [{@migration_version, AddGoal0006ControlPlane}],
      :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_oban_up! do
    Ecto.Migrator.run(Repo, [{@oban_migration_version, AddObanJobsTable}], :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_oban_down! do
    Ecto.Migrator.run(Repo, [{@oban_migration_version, AddObanJobsTable}], :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_runtime_affinity_up! do
    Ecto.Migrator.run(
      Repo,
      [{@runtime_affinity_migration_version, AddRuntimeRepositoryResourceAffinity}],
      :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_runtime_affinity_down! do
    Ecto.Migrator.run(
      Repo,
      [{@runtime_affinity_migration_version, AddRuntimeRepositoryResourceAffinity}],
      :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_runtime_affinity_guard_up! do
    Ecto.Migrator.run(
      Repo,
      [{@runtime_affinity_guard_migration_version, EnforceRuntimeRepositoryResourceAffinity}],
      :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_runtime_affinity_guard_down! do
    Ecto.Migrator.run(
      Repo,
      [{@runtime_affinity_guard_migration_version, EnforceRuntimeRepositoryResourceAffinity}],
      :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_integration_designation_up! do
    Ecto.Migrator.run(
      Repo,
      [{@integration_designation_migration_version, AddGoalIntegrationWorkItemDesignation}],
      :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_integration_designation_down! do
    Ecto.Migrator.run(
      Repo,
      base_migrations() ++
        [
          {@migration_version, AddGoal0006ControlPlane},
          {@integration_designation_migration_version, AddGoalIntegrationWorkItemDesignation}
        ],
      :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_baseline_up! do
    Ecto.Migrator.run(
      Repo,
      [{@baseline_migration_version, AddGoalWorkItemBaselines}],
      :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_identity_up! do
    Ecto.Migrator.run(
      Repo,
      [{@identity_migration_version, AddGoalIdentityGuards}],
      :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_identity_down! do
    Ecto.Migrator.run(
      Repo,
      [{@identity_migration_version, AddGoalIdentityGuards}],
      :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_planning_task_up! do
    Ecto.Migrator.run(
      Repo,
      [{@planning_task_migration_version, AddPlanningTaskIdentityGuards}],
      :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_planning_task_down! do
    Ecto.Migrator.run(
      Repo,
      [{@planning_task_migration_version, AddPlanningTaskIdentityGuards}],
      :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_terminal_policy_up! do
    Ecto.Migrator.run(
      Repo,
      [{@terminal_policy_migration_version, AddGoalTerminalGuardsAndExecutionPolicy}],
      :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_terminal_policy_down! do
    Ecto.Migrator.run(
      Repo,
      [{@terminal_policy_migration_version, AddGoalTerminalGuardsAndExecutionPolicy}],
      :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_terminal_authority_up! do
    Ecto.Migrator.run(
      Repo,
      [
        {@terminal_authority_migration_version,
         AddGoalTerminalAuthorityGuardsAndSessionReciprocity}
      ],
      :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_terminal_authority_down! do
    Ecto.Migrator.run(
      Repo,
      [
        {@terminal_authority_migration_version,
         AddGoalTerminalAuthorityGuardsAndSessionReciprocity}
      ],
      :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_handoff_lineage_up! do
    Ecto.Migrator.run(Repo, [{@handoff_lineage_migration_version, AddTaskHandoffLineage}], :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_handoff_lineage_down! do
    Ecto.Migrator.run(Repo, [{@handoff_lineage_migration_version, AddTaskHandoffLineage}], :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_session_stop_receipt_up! do
    Ecto.Migrator.run(
      Repo,
      [{@session_stop_receipt_migration_version, AddHarnessSessionStopReceipts}],
      :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_session_stop_receipt_down! do
    Ecto.Migrator.run(
      Repo,
      [{@session_stop_receipt_migration_version, AddHarnessSessionStopReceipts}],
      :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_session_attach_receipt_up! do
    Ecto.Migrator.run(
      Repo,
      [{@session_attach_receipt_migration_version, AddHarnessSessionAttachReceipts}],
      :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_session_attach_receipt_down! do
    Ecto.Migrator.run(
      Repo,
      [{@session_attach_receipt_migration_version, AddHarnessSessionAttachReceipts}],
      :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_provider_access_snapshot_up! do
    Ecto.Migrator.run(
      Repo,
      [{@provider_access_snapshot_migration_version, AddRunProviderAccessSnapshots}],
      :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_provider_access_snapshot_down! do
    Ecto.Migrator.run(
      Repo,
      [{@provider_access_snapshot_migration_version, AddRunProviderAccessSnapshots}],
      :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_change_target_up! do
    Ecto.Migrator.run(
      Repo,
      [{@change_target_migration_version, AddGoalWorkItemChangeTargets}],
      :up,
      all: true,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_change_target_down! do
    Ecto.Migrator.run(
      Repo,
      [{@change_target_migration_version, AddGoalWorkItemChangeTargets}],
      :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp migrate_baseline_down! do
    Ecto.Migrator.run(
      Repo,
      [{@baseline_migration_version, AddGoalWorkItemBaselines}],
      :down,
      step: 1,
      log: false,
      migration_lock: false,
      dynamic_repo: Repo.get_dynamic_repo()
    )
  end

  defp insert_goal_fixture!(execution_policy \\ execution_policy_json(), integration \\ false) do
    project_id = insert_project!()
    goal_id = Ecto.UUID.bingenerate()
    work_item_id = insert_work_item!(project_id)

    Repo.transaction(fn ->
      Repo.query!(
        """
        INSERT INTO goals (id, project_id, title, state, current_revision, inserted_at, updated_at)
        VALUES ($1, $2, 'Goal migration fixture', 'draft', 1, now(), now())
        """,
        [goal_id, project_id]
      )

      Repo.query!(
        """
        INSERT INTO goal_revisions (
          goal_id, revision, objective, non_goals, acceptance_contract, authority_policy,
          execution_policy, context_manifest, reason, actor_ref, inserted_at
        )
        VALUES ($1, 1, 'Verify database constraints', '[]'::jsonb, '{}'::jsonb, '{}'::jsonb,
                $2::text::jsonb, '{}'::jsonb, 'initial', 'operator:test', now())
        """,
        [goal_id, execution_policy]
      )

      if integration do
        Repo.query!(
          """
          UPDATE work_items
          SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb, integration = TRUE
          WHERE id = $2
          """,
          [goal_id, work_item_id]
        )
      else
        Repo.query!(
          """
          UPDATE work_items
          SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb
          WHERE id = $2
          """,
          [goal_id, work_item_id]
        )
      end
    end)

    %{goal_id: goal_id, project_id: project_id, work_item_id: work_item_id}
  end

  defp insert_goal_work_item!(project_id, goal_id) do
    work_item_id = insert_work_item!(project_id)

    Repo.query!(
      """
      UPDATE work_items
      SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb
      WHERE id = $2
      """,
      [goal_id, work_item_id]
    )

    work_item_id
  end

  defp insert_designated_goal_work_item!(project_id, goal_id) do
    work_item_id = insert_work_item!(project_id)

    Repo.query!(
      """
      UPDATE work_items
      SET goal_id = $1, admitted_revision = 1, acceptance_contract = '{}'::jsonb, integration = TRUE
      WHERE id = $2
      """,
      [goal_id, work_item_id]
    )

    work_item_id
  end

  defp insert_project! do
    id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO projects (id, name, key, status, default_agent_profile, default_workspace, inserted_at, updated_at)
      VALUES ($1, $2, $3, 'active', 'default', 'primary', now(), now())
      """,
      [id, "Goal migration project #{System.unique_integer([:positive])}", project_key()]
    )

    id
  end

  defp insert_repository_resource!(project_id) do
    insert_project_resource!(project_id, "repository")
  end

  defp insert_project_resource!(project_id, kind) do
    id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO project_resources (
        id, project_id, kind, name, status, sync_status, metadata, lock_version, inserted_at, updated_at
      )
      VALUES ($1, $2, $3, $4, 'unknown', 'unknown', '{}'::jsonb, 1, now(), now())
      """,
      [id, project_id, kind, "Baseline #{kind} #{System.unique_integer([:positive])}"]
    )

    id
  end

  defp insert_machine! do
    id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO machines (id, name, token_digest, inserted_at, updated_at)
      VALUES ($1, $2, $3, now(), now())
      """,
      [id, "runtime-affinity-machine-#{System.unique_integer([:positive])}", <<10>>]
    )

    id
  end

  defp insert_runtime!(machine_id, runtime_key, repository_resource_id \\ nil) do
    id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO runtimes (
        id, machine_id, runtime_key, name, daemon_instance_id, connection_epoch, capacity,
        agent_profile, workspace, status, heartbeat_interval_ms, repository_resource_id,
        inserted_at, updated_at
      )
      VALUES ($1, $2, $3, $3, $4, 1, 1, 'default', 'primary', 'online', 5000, $5, now(), now())
      """,
      [id, machine_id, runtime_key, Ecto.UUID.bingenerate(), repository_resource_id]
    )

    id
  end

  defp insert_harness_session_fixture! do
    project_id = insert_project!()
    resource_id = insert_repository_resource!(project_id)
    machine_id = Ecto.UUID.bingenerate()
    runtime_id = Ecto.UUID.bingenerate()
    session_id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO machines (id, name, token_digest, inserted_at, updated_at)
      VALUES ($1, $2, $3, now(), now())
      """,
      [machine_id, "session-machine-#{System.unique_integer([:positive])}", <<24>>]
    )

    Repo.query!(
      """
      INSERT INTO runtimes (
        id, machine_id, runtime_key, name, daemon_instance_id, connection_epoch, capacity,
        agent_profile, workspace, status, heartbeat_interval_ms, inserted_at, updated_at
      )
      VALUES ($1, $2, $3, $3, $4, 1, 1, 'default', 'primary', 'online', 5000, now(), now())
      """,
      [
        runtime_id,
        machine_id,
        "session-runtime-#{System.unique_integer([:positive])}",
        Ecto.UUID.bingenerate()
      ]
    )

    Repo.query!(
      """
      INSERT INTO harness_sessions (
        id, machine_id, runtime_id, harness_kind, harness_version, adapter_version,
        local_handle_id, repository_resource_id, workspace_fingerprint, state, inserted_at, updated_at
      )
      VALUES ($1, $2, $3, 'codex', '1.0.0', 'adapter-1', $4, $5, 'fingerprint', 'available',
              now(), now())
      """,
      [session_id, machine_id, runtime_id, Ecto.UUID.bingenerate(), resource_id]
    )

    %{
      machine_id: machine_id,
      runtime_id: runtime_id,
      resource_id: resource_id,
      session_id: session_id
    }
  end

  defp insert_harness_run!(run_id, task_id, runtime_id, session_id, generation, state) do
    Repo.query!(
      """
      INSERT INTO runs (
        id, task_id, runtime_id, generation, state, assigned_at, assignment_expires_at,
        harness_session_id, inserted_at, updated_at
      )
      VALUES ($1, $2, $3, $4, $5, now(), now(), $6, now(), now())
      """,
      [run_id, task_id, runtime_id, generation, state, session_id]
    )
  end

  defp insert_connection! do
    id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO external_connections (
        id, provider, name, account_ref, auth_type, capabilities, status, metadata, lock_version,
        inserted_at, updated_at
      )
      VALUES ($1, 'github', $2, 'runtime-affinity-account', 'gh_cli', '{}', 'unknown', '{}'::jsonb, 1,
              now(), now())
      """,
      [id, "runtime-affinity-connection-#{System.unique_integer([:positive])}"]
    )

    id
  end

  defp insert_goal_row!(goal_id, project_id) do
    Repo.transaction(fn ->
      Repo.query!(
        """
        INSERT INTO goals (id, project_id, title, state, current_revision, inserted_at, updated_at)
        VALUES ($1, $2, 'Baseline migration Goal', 'draft', 1, now(), now())
        """,
        [goal_id, project_id]
      )

      Repo.query!(
        """
        INSERT INTO goal_revisions (
          goal_id, revision, objective, non_goals, acceptance_contract, authority_policy,
          execution_policy, context_manifest, reason, actor_ref, inserted_at
        )
        VALUES ($1, 1, 'Verify baseline sources', '[]'::jsonb, '{}'::jsonb, '{}'::jsonb,
                $2::text::jsonb, '{}'::jsonb, 'initial', 'operator:test', now())
        """,
        [goal_id, execution_policy_json()]
      )
    end)
  end

  defp insert_work_item!(project_id, repository_resource_id \\ nil) do
    id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO work_items (
        id, project_id, repository_resource_id, title, status, priority, position, assignee_type,
        blocked, ci_status, review_status, inserted_at, updated_at
      )
      VALUES ($1, $2, $3, 'Goal migration WorkItem', 'backlog', 'no_priority', 0, 'unassigned',
              FALSE, NULL, NULL, now(), now())
      """,
      [id, project_id, repository_resource_id]
    )

    id
  end

  defp subject(repository_id) do
    %{
      "resource_id" => Ecto.UUID.cast!(repository_id),
      "commit" => String.duplicate("a", 40),
      "tree_digest" => "sha256:" <> String.duplicate("4", 64)
    }
  end

  defp insert_legacy_task!(goal) do
    id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO tasks (
        id, idempotency_key, request_hash, goal, agent_profile, workspace, input,
        required_capabilities, state, current_generation, attempt_generation, inserted_at, updated_at
      )
      VALUES ($1, $2, $3, $4, 'codex', 'primary', '{}'::jsonb, '{}'::jsonb,
              'completed', 1, 1, now(), now())
      """,
      [id, "legacy-task-#{System.unique_integer([:positive])}", hash(1), goal]
    )

    id
  end

  defp insert_goal_task!(attrs) do
    id = Map.get(attrs, :id, Ecto.UUID.bingenerate())
    goal_id = Map.fetch!(attrs, :goal_id)
    work_item_id = Map.get(attrs, :work_item_id)
    purpose = Map.get(attrs, :purpose, "implement")
    validation_of_task_id = Map.get(attrs, :validation_of_task_id)

    context_snapshot_id =
      Map.get_lazy(attrs, :context_snapshot_id, fn ->
        if is_binary(goal_id) and is_binary(work_item_id) do
          insert_context_snapshot!(goal_id, work_item_id)
        else
          Ecto.UUID.bingenerate()
        end
      end)

    Repo.query!(
      """
      INSERT INTO tasks (
        id, idempotency_key, request_hash, goal, agent_profile, workspace, input,
        required_capabilities, state, current_generation, attempt_generation, work_item_id,
        goal_id, goal_revision, context_snapshot_id, purpose, validation_of_task_id, admission_key,
        max_run_attempts, inserted_at, updated_at
      )
      VALUES ($1, $2, $3, 'Goal task', 'codex', 'primary', '{}'::jsonb, '{}'::jsonb,
              'queued', 0, 1, $4, $5, 1, $6, $7, $8, $9, 2, now(), now())
      """,
      [
        id,
        "goal-task-#{System.unique_integer([:positive])}",
        hash(9),
        work_item_id,
        goal_id,
        context_snapshot_id,
        purpose,
        validation_of_task_id,
        Ecto.UUID.bingenerate()
      ]
    )

    id
  end

  defp insert_resolved_goal_decision!(goal_id) do
    id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO goal_decisions (
        id, goal_id, goal_revision, kind, action_hash, state, question, options, resolution,
        lock_version, inserted_at, updated_at
      )
      VALUES ($1, $2, 1, 'completion', $3, 'resolved', 'Accept the completed Goal?',
              '[{"id": "accept", "label": "Accept", "consequence": "Mark achieved"}]'::jsonb,
              '{"option_id": "accept"}'::jsonb, 1, now(), now())
      """,
      [id, goal_id, hash(10)]
    )

    id
  end

  defp insert_open_goal_decision!(goal_id) do
    id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO goal_decisions (
        id, goal_id, goal_revision, kind, action_hash, state, question, options,
        lock_version, inserted_at, updated_at
      )
      VALUES ($1, $2, 1, 'completion', $3, 'open', 'Accept the completed Goal?',
              '[{"id": "accept", "label": "Accept", "consequence": "Mark achieved"}]'::jsonb,
              1, now(), now())
      """,
      [id, goal_id, :crypto.hash(:sha256, Ecto.UUID.generate())]
    )

    id
  end

  defp insert_goal_revision_policy!(goal_id, revision, execution_policy) do
    Repo.query!(
      """
      INSERT INTO goal_revisions (
        goal_id, revision, objective, non_goals, acceptance_contract, authority_policy,
        execution_policy, context_manifest, reason, actor_ref, inserted_at
      )
      VALUES ($1, $2, 'Verify execution policy constraints', '[]'::jsonb, '{}'::jsonb, '{}'::jsonb,
              $3::text::jsonb, '{}'::jsonb, 'policy test', 'operator:test', now())
      """,
      [goal_id, revision, execution_policy]
    )
  end

  defp insert_context_snapshot!(goal_id, work_item_id) do
    id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO context_snapshots (
        id, goal_id, goal_revision, work_item_id, schema_version, content_hash, payload, inserted_at
      )
      VALUES ($1, $2, 1, $3, 1, $4, '{}'::jsonb, now())
      """,
      [id, goal_id, work_item_id, :crypto.hash(:sha256, Ecto.UUID.generate())]
    )

    id
  end

  defp insert_planning_context_snapshot!(goal_id) do
    id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO context_snapshots (
        id, goal_id, goal_revision, work_item_id, schema_version, content_hash, payload, inserted_at
      )
      VALUES ($1, $2, 1, NULL, 1, $3, '{}'::jsonb, now())
      """,
      [id, goal_id, :crypto.hash(:sha256, Ecto.UUID.generate())]
    )

    id
  end

  defp insert_run_usage!(run_id, usage_key, cost_basis, cost_microusd) do
    Repo.query!(
      """
      INSERT INTO run_usage (
        id, run_id, usage_key, provider, model, cost_microusd, cost_basis, inserted_at
      )
      VALUES ($1, $2, $3, 'provider', 'model', $4, $5, now())
      """,
      [Ecto.UUID.bingenerate(), run_id, usage_key, cost_microusd, cost_basis]
    )
  end

  defp insert_terminal_goal_run!(task_id, runtime_id, state) do
    run_id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO runs (
        id, task_id, runtime_id, generation, state, assigned_at, assignment_expires_at,
        inserted_at, updated_at
      )
      VALUES ($1, $2, $3, 1, $4, now(), now(), now(), now())
      """,
      [run_id, task_id, runtime_id, state]
    )

    run_id
  end

  defp insert_handoff_task!(goal_id, work_item_id, revision, source_run_id, input) do
    task_id = Ecto.UUID.bingenerate()
    context_snapshot_id = insert_context_snapshot_at_revision!(goal_id, work_item_id, revision)

    Repo.query!(
      """
      INSERT INTO tasks (
        id, idempotency_key, request_hash, goal, agent_profile, workspace, input,
        required_capabilities, state, current_generation, attempt_generation, work_item_id,
        goal_id, goal_revision, context_snapshot_id, purpose, validation_of_task_id, admission_key,
        max_run_attempts, requested_session_id, handoff_source_run_id, inserted_at, updated_at
      )
      VALUES ($1, $2, $3, 'Goal task', 'codex', 'primary', $4::text::jsonb, '{}'::jsonb,
              'queued', 0, 1, $5, $6, $7, $8, 'implement', NULL, $9, 2, NULL, $10, now(), now())
      """,
      [
        task_id,
        "handoff-task-#{System.unique_integer([:positive])}",
        hash(13),
        Jason.encode!(input),
        work_item_id,
        goal_id,
        revision,
        context_snapshot_id,
        Ecto.UUID.bingenerate(),
        source_run_id
      ]
    )

    task_id
  end

  defp insert_planning_task!(goal_id) do
    {:ok, task_id} =
      Repo.transaction(fn ->
        context_snapshot_id = insert_planning_context_snapshot!(goal_id)
        task_id = Ecto.UUID.bingenerate()

        Repo.query!(
          """
          INSERT INTO tasks (
            id, idempotency_key, request_hash, goal, agent_profile, workspace, input,
            required_capabilities, state, current_generation, attempt_generation, work_item_id,
            goal_id, goal_revision, context_snapshot_id, purpose, validation_of_task_id, admission_key,
            max_run_attempts, requested_session_id, handoff_source_run_id, inserted_at, updated_at
          )
          VALUES ($1, $2, $3, 'Goal plan', 'codex', 'primary', '{"session_mode":"fresh"}'::jsonb,
                  '{}'::jsonb, 'completed', 1, 1, NULL, $4, 1, $5, 'plan', NULL, $6, 2, NULL, NULL,
                  now(), now())
          """,
          [
            task_id,
            "planning-source-task-#{System.unique_integer([:positive])}",
            hash(14),
            goal_id,
            context_snapshot_id,
            Ecto.UUID.bingenerate()
          ]
        )

        task_id
      end)

    task_id
  end

  defp insert_planning_handoff_task!(goal_id, source_run_id, input) do
    Repo.transaction(fn ->
      context_snapshot_id = insert_planning_context_snapshot!(goal_id)
      task_id = Ecto.UUID.bingenerate()

      Repo.query!(
        """
        INSERT INTO tasks (
          id, idempotency_key, request_hash, goal, agent_profile, workspace, input,
          required_capabilities, state, current_generation, attempt_generation, work_item_id,
          goal_id, goal_revision, context_snapshot_id, purpose, validation_of_task_id, admission_key,
          max_run_attempts, requested_session_id, handoff_source_run_id, inserted_at, updated_at
        )
        VALUES ($1, $2, $3, 'Goal plan', 'codex', 'primary', $4::text::jsonb, '{}'::jsonb,
                'queued', 0, 1, NULL, $5, 1, $6, 'plan', NULL, $7, 2, NULL, $8, now(), now())
        """,
        [
          task_id,
          "planning-handoff-task-#{System.unique_integer([:positive])}",
          hash(15),
          Jason.encode!(input),
          goal_id,
          context_snapshot_id,
          Ecto.UUID.bingenerate(),
          source_run_id
        ]
      )
    end)
  end

  defp handoff_input(source_run_id) do
    %{"session_mode" => "handoff", "handoff_source_run_id" => Ecto.UUID.cast!(source_run_id)}
  end

  defp insert_context_snapshot_at_revision!(goal_id, work_item_id, revision) do
    id = Ecto.UUID.bingenerate()

    Repo.query!(
      """
      INSERT INTO context_snapshots (
        id, goal_id, goal_revision, work_item_id, schema_version, content_hash, payload, inserted_at
      )
      VALUES ($1, $2, $3, $4, 1, $5, '{}'::jsonb, now())
      """,
      [id, goal_id, revision, work_item_id, :crypto.hash(:sha256, Ecto.UUID.generate())]
    )

    id
  end

  defp assert_handoff_lineage_lock_timeout!(repo, update) do
    contender =
      Elixir.Task.async(fn ->
        previous_dynamic_repo = Repo.put_dynamic_repo(repo)

        try do
          try do
            Repo.transaction(fn ->
              Repo.query!("SET LOCAL lock_timeout = '1s'")
              update.()
            end)

            :unexpected_success
          rescue
            error in Postgrex.Error -> {:error, error}
          end
        after
          Repo.put_dynamic_repo(previous_dynamic_repo)
        end
      end)

    assert {:error, error} = Elixir.Task.await(contender, 15_000)
    assert Exception.message(error) =~ "lock timeout"
  end

  defp execution_policy_json do
    legacy_execution_policy_json(%{})
  end

  defp legacy_execution_policy_json(overrides) do
    %{
      "automatic_execution" => false,
      "max_parallel_tasks" => 1,
      "max_task_admissions" => 1,
      "max_run_attempts_per_task" => 2,
      "budget_limit_microusd" => nil,
      "budget_mode" => "soft",
      "allowed_runtime_ids" => [],
      "allowed_model_profiles" => [],
      "final_acceptance" => "operator",
      "allowed_actions" => [],
      "allowed_resource_ids" => []
    }
    |> Map.merge(overrides)
    |> Jason.encode!()
  end

  defp protected_execution_policy_json(overrides) do
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
    |> Map.merge(overrides)
    |> Jason.encode!()
  end

  defp hash(byte), do: :binary.copy(<<byte>>, 32)
  defp project_key, do: "G#{System.unique_integer([:positive])}" |> String.slice(0, 8)

  defp base_migrations do
    [
      {20_260_902_000_000, CreateOrchestrationTables},
      {20_260_903_000_000, AddTaskCommandOwnership},
      {20_260_903_010_000, AddMachineEnrollmentReplay},
      {20_260_903_020_000, AddHistoryLookupIndexes},
      {20_260_904_000_000, CreateEngineeringWorkspace},
      {20_260_904_010_000, CompleteEngineeringWorkspace},
      {20_260_904_020_000, BindWorkItemsToResources},
      {20_260_904_030_000, AddRetryCommands},
      {20_260_904_040_000, AddTaskAttemptIdentity},
      {20_260_904_050_000, EnforceWorkItemCoherence},
      {20_260_905_000_000, AddEngineeringConnections},
      {20_260_906_000_000, AddTaskRequiredCapabilities},
      {20_260_906_010_000, CreateProviderActionIntents},
      {20_260_906_020_000, AddProviderActionDispatchOwnership},
      {20_260_906_030_000, EnforceConnectedResourceIdentity},
      {20_260_906_040_000, SnapshotProviderActionTargets},
      {20_260_906_050_000, AddSupervisoryControls},
      {20_260_906_060_000, CreateChat},
      {20_260_906_070_000, VersionCommandRequestHashes},
      {20_260_907_000_000, VersionRequestHashes}
    ]
  end

  defp load_migration_modules! do
    migrations = [
      {CreateOrchestrationTables, "20260902000000_create_orchestration_tables.exs"},
      {AddTaskCommandOwnership, "20260903000000_add_task_command_ownership.exs"},
      {AddMachineEnrollmentReplay, "20260903010000_add_machine_enrollment_replay.exs"},
      {AddHistoryLookupIndexes, "20260903020000_add_history_lookup_indexes.exs"},
      {CreateEngineeringWorkspace, "20260904000000_create_engineering_workspace.exs"},
      {CompleteEngineeringWorkspace, "20260904010000_complete_engineering_workspace.exs"},
      {BindWorkItemsToResources, "20260904020000_bind_work_items_to_resources.exs"},
      {AddRetryCommands, "20260904030000_add_retry_commands.exs"},
      {AddTaskAttemptIdentity, "20260904040000_add_task_attempt_identity.exs"},
      {EnforceWorkItemCoherence, "20260904050000_enforce_work_item_coherence.exs"},
      {AddEngineeringConnections, "20260905000000_add_engineering_connections.exs"},
      {AddTaskRequiredCapabilities, "20260906000000_add_task_required_capabilities.exs"},
      {CreateProviderActionIntents, "20260906010000_create_provider_action_intents.exs"},
      {AddProviderActionDispatchOwnership,
       "20260906020000_add_provider_action_dispatch_ownership.exs"},
      {EnforceConnectedResourceIdentity,
       "20260906030000_enforce_connected_resource_identity.exs"},
      {SnapshotProviderActionTargets, "20260906040000_snapshot_provider_action_targets.exs"},
      {AddSupervisoryControls, "20260906050000_add_supervisory_controls.exs"},
      {CreateChat, "20260906060000_create_chat.exs"},
      {VersionCommandRequestHashes, "20260906070000_version_command_request_hashes.exs"},
      {VersionRequestHashes, "20260907000000_version_request_hashes.exs"},
      {AddGoal0006ControlPlane, "20260909000000_add_goal_0006_control_plane.exs"},
      {AddObanJobsTable, "20260909010000_add_oban_jobs_table.exs"},
      {AddRuntimeRepositoryResourceAffinity,
       "20260909020000_add_runtime_repository_resource_affinity.exs"},
      {AddGoalIntegrationWorkItemDesignation,
       "20260909030000_add_goal_integration_work_item_designation.exs"},
      {AddGoalWorkItemBaselines, "20260909040000_add_goal_work_item_baselines.exs"},
      {AddGoalIdentityGuards, "20260909050000_add_goal_identity_guards.exs"},
      {AddRunProviderAccessSnapshots, "20260909070000_add_run_provider_access_snapshots.exs"},
      {AddGoalWorkItemChangeTargets, "20260909080000_add_goal_work_item_change_targets.exs"},
      {AddPlanningTaskIdentityGuards, "20260910000000_add_planning_task_identity_guards.exs"},
      {AddGoalTerminalGuardsAndExecutionPolicy,
       "20260910010000_add_goal_terminal_guards_and_execution_policy.exs"},
      {EnforceRuntimeRepositoryResourceAffinity,
       "20260910020000_enforce_runtime_repository_resource_affinity.exs"},
      {AddGoalTerminalAuthorityGuardsAndSessionReciprocity,
       "20260910030000_add_goal_terminal_authority_guards_and_session_reciprocity.exs"},
      {AddTaskHandoffLineage, "20260910040000_add_task_handoff_lineage.exs"},
      {AddHarnessSessionStopReceipts, "20260910050000_add_harness_session_stop_receipts.exs"},
      {AddHarnessSessionAttachReceipts, "20260911000000_add_harness_session_attach_receipts.exs"}
    ]

    Enum.each(migrations, fn {module, filename} ->
      unless function_exported?(module, :__migration__, 0) do
        filename
        |> then(&Path.expand("../../../priv/repo/migrations/#{&1}", __DIR__))
        |> Code.compile_file()
      end
    end)
  end

  defp start_schema_repo(schema) do
    config =
      Application.fetch_env!(:symmetry_control, Repo)
      |> Keyword.merge(name: nil, pool: DBConnection.ConnectionPool, pool_size: 1)
      |> Keyword.put(:parameters, search_path: schema)

    {:ok, repo} = Repo.start_link(config)
    repo
  end

  defp create_schema!(schema) do
    Sandbox.unboxed_run(Repo, fn -> Repo.query!("CREATE SCHEMA #{schema}") end)
  end

  defp drop_schema!(schema) do
    Sandbox.unboxed_run(Repo, fn -> Repo.query!("DROP SCHEMA IF EXISTS #{schema} CASCADE") end)
  end
end
