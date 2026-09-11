defmodule SymmetryControl.Goals.ContractValidationTest do
  use ExUnit.Case, async: false

  alias SymmetryControl.Goals.ContractValidation
  alias SymmetryControl.RequestHash

  @moduletag if Code.ensure_loaded?(ExJsonSchema.Schema),
               do: [],
               else: [skip: "ExJsonSchema dependency is not available in this checkout"]

  @schema_root Path.expand("../../../../contracts/v1", __DIR__)
  @valid_root Path.expand("../../../../contracts/fixtures/valid", __DIR__)
  @invalid_root Path.expand("../../../../contracts/fixtures/invalid", __DIR__)

  test "validates every public Goal v1 envelope against canonical schemas" do
    opts = [schema_root: @schema_root]

    assert :ok ==
             ContractValidation.validate_goal_revision(
               fixture(@valid_root, "goal-revision.basic.json"),
               opts
             )

    assert :ok ==
             ContractValidation.validate_admission(
               fixture(@valid_root, "admission.basic.json"),
               opts
             )

    assert :ok ==
             ContractValidation.validate_admission(
               fixture(@valid_root, "admission.plan.json"),
               opts
             )

    assert :ok ==
             ContractValidation.validate_admission(
               fixture(@valid_root, "admission.validate.json"),
               opts
             )

    assert :ok ==
             ContractValidation.validate_context_snapshot(
               fixture(@valid_root, "context-snapshot.basic.json"),
               opts
             )

    assert :ok ==
             ContractValidation.validate_context_snapshot(
               fixture(@valid_root, "context-snapshot.plan.json"),
               opts
             )

    assert :ok ==
             ContractValidation.validate_task_result(
               fixture(@valid_root, "task-result.candidate.json"),
               opts
             )

    assert :ok ==
             ContractValidation.validate_task_result(
               fixture(@valid_root, "task-result.failed.json"),
               opts
             )

    assert :ok ==
             ContractValidation.validate_task_result(
               fixture(@valid_root, "task-result.plan-proposed.json"),
               opts
             )

    assert :ok ==
             ContractValidation.validate_evidence(
               fixture(@valid_root, "evidence.check.json"),
               opts
             )

    assert :ok ==
             ContractValidation.validate_evidence(
               fixture(@valid_root, "evidence.artifact.json"),
               opts
             )

    assert :ok ==
             ContractValidation.validate_evidence(
               fixture(@valid_root, "evidence.review.json"),
               opts
             )

    assert :ok ==
             ContractValidation.validate_evidence(
               fixture(@valid_root, "evidence.observation.json"),
               opts
             )

    assert :ok ==
             ContractValidation.validate_evidence_batch(
               fixture(@valid_root, "evidence-batch.basic.json"),
               opts
             )

    assert :ok ==
             ContractValidation.validate_decision(
               fixture(@valid_root, "decision.open.json"),
               opts
             )

    assert :ok ==
             ContractValidation.validate_usage(fixture(@valid_root, "usage.nullable.json"), opts)

    assert :ok ==
             ContractValidation.validate_usage(
               fixture(@valid_root, "usage.estimated-cost.json"),
               opts
             )

    assert :ok ==
             ContractValidation.validate_adapter_capabilities(
               fixture(@valid_root, "adapter-capabilities.basic.json"),
               opts
             )
  end

  test "rejects strict fields, unknown enums and unsafe fixture values" do
    opts = [schema_root: @schema_root]

    assert {:error, {:validation_failed, _}} =
             ContractValidation.validate_goal_revision(
               fixture(@invalid_root, "goal-revision.extra-field.json"),
               opts
             )

    assert {:error, {:validation_failed, _}} =
             ContractValidation.validate_admission(
               fixture(@invalid_root, "admission.version-mismatch.json"),
               opts
             )

    assert {:error, {:validation_failed, _}} =
             ContractValidation.validate_goal_revision(
               fixture(@invalid_root, "goal-revision.null-reason.json"),
               opts
             )

    assert {:error, {:content_hash_mismatch, _}} =
             ContractValidation.validate_context_snapshot(
               fixture(@invalid_root, "context-snapshot.content-hash-mismatch.json"),
               opts
             )

    assert {:error, {:validation_failed, _}} =
             ContractValidation.validate_context_snapshot(
               fixture(@invalid_root, "context-snapshot.missing-sources.json"),
               opts
             )

    assert {:error, {:subject_hash_mismatch, _}} =
             ContractValidation.validate_task_result(
               fixture(@invalid_root, "task-result.subject-hash-mismatch.json"),
               opts
             )

    blocked_without_blocker =
      fixture(@valid_root, "task-result.candidate.json")
      |> Map.put("kind", "blocked")

    assert {:error, :blocked_result_requires_blocker} =
             ContractValidation.validate_task_result(blocked_without_blocker, opts)

    assert {:error, {:subject_hash_mismatch, _}} =
             ContractValidation.validate_evidence(
               fixture(@invalid_root, "evidence.subject-hash-mismatch.json"),
               opts
             )

    assert {:error, {:evidence_identity_mismatch, _}} =
             ContractValidation.validate_evidence(
               fixture(@invalid_root, "evidence.source-ref-mismatch.json"),
               opts
             )

    assert {:error, {:validation_failed, _}} =
             ContractValidation.validate_usage(
               fixture(@invalid_root, "usage.microusd-overflow.json"),
               opts
             )

    assert {:error, {:duplicate_predicate_id, "tests"}} =
             ContractValidation.validate_goal_revision(
               fixture(@invalid_root, "goal-revision.duplicate-predicate-id.json"),
               opts
             )

    assert {:error, {:duplicate_predicate_id, "tests"}} =
             ContractValidation.validate_context_snapshot(
               fixture(@invalid_root, "context-snapshot.duplicate-predicate-id.json"),
               opts
             )

    invalid_decision =
      fixture(@valid_root, "decision.open.json")
      |> Map.put("unexpected", true)

    assert {:error, {:validation_failed, _}} =
             ContractValidation.validate_decision(invalid_decision, opts)

    assert {:error, :duplicate_evidence_key} =
             ContractValidation.validate_evidence_batch(
               fixture(@invalid_root, "evidence-batch.duplicate-key.json"),
               opts
             )

    assert {:error, {:validation_failed, _}} =
             ContractValidation.validate_evidence_batch(
               fixture(@invalid_root, "evidence-batch.empty-items.json"),
               opts
             )

    assert {:error, {:validation_failed, _}} =
             ContractValidation.validate_evidence_batch(
               fixture(@invalid_root, "evidence-batch.extra-control-field.json"),
               opts
             )
  end

  test "requires every batch item to use the outer batch run identity" do
    opts = [schema_root: @schema_root]

    invalid =
      fixture(@valid_root, "evidence-batch.basic.json")
      |> put_in(["items", Access.at(0), "run_id"], Ecto.UUID.generate())

    assert {:error, :evidence_batch_run_id_mismatch} =
             ContractValidation.validate_evidence_batch(invalid, opts)
  end

  test "validates pure decision and GoalRevision semantics after schema validation" do
    opts = [schema_root: @schema_root]

    decision = fixture(@valid_root, "decision.open.json")
    [first_option, second_option] = decision["options"]

    duplicate_option_ids =
      put_in(
        decision,
        ["options"],
        [first_option, Map.put(second_option, "id", first_option["id"])]
      )

    assert {:error, :duplicate_decision_option_id} =
             ContractValidation.validate_decision(duplicate_option_ids, opts)

    resolved_without_resolution = Map.put(decision, "state", "resolved")

    assert {:error, {:decision_resolution_shape, "resolved"}} =
             ContractValidation.validate_decision(resolved_without_resolution, opts)

    resolved_valid =
      decision
      |> Map.put("state", "resolved")
      |> Map.put("resolution", %{
        "option_id" => "accept",
        "comment" => nil,
        "actor_ref" => "operator:local",
        "resolved_at" => "2026-09-09T00:03:00Z"
      })

    assert :ok == ContractValidation.validate_decision(resolved_valid, opts)

    resolved_with_unknown_option =
      decision
      |> Map.put("state", "resolved")
      |> Map.put("resolution", %{
        "option_id" => "missing",
        "comment" => nil,
        "actor_ref" => nil,
        "resolved_at" => "2026-09-09T00:03:00Z"
      })

    assert {:error, {:decision_resolution_unknown_option, "missing"}} =
             ContractValidation.validate_decision(resolved_with_unknown_option, opts)

    open_with_resolution = Map.put(decision, "resolution", resolved_valid["resolution"])

    assert {:error, {:decision_resolution_shape, "open"}} =
             ContractValidation.validate_decision(open_with_resolution, opts)

    artifact_evidence = fixture(@valid_root, "evidence.artifact.json")

    payload_subject_mismatch =
      put_in(
        artifact_evidence,
        ["payload", "subject", "commit"],
        String.duplicate("e", 40)
      )

    assert {:error, {:evidence_identity_mismatch, :payload_subject}} =
             ContractValidation.validate_evidence(payload_subject_mismatch, opts)

    payload_identity_mismatch =
      put_in(artifact_evidence, ["payload", "path"], "src/other.txt")

    assert {:error, {:evidence_identity_mismatch, :artifact_path}} =
             ContractValidation.validate_evidence(payload_identity_mismatch, opts)

    goal_revision = fixture(@valid_root, "goal-revision.basic.json")
    [first_predicate, second_predicate] = goal_revision["acceptance_contract"]["predicates"]

    duplicate_predicate_ids =
      put_in(
        goal_revision,
        ["acceptance_contract", "predicates"],
        [first_predicate, Map.put(second_predicate, "id", first_predicate["id"])]
      )

    assert {:error, {:duplicate_predicate_id, "tests"}} =
             ContractValidation.validate_goal_revision(duplicate_predicate_ids, opts)

    automatic_execution =
      put_in(goal_revision, ["execution_policy", "automatic_execution"], true)

    assert :ok ==
             ContractValidation.validate_goal_revision(automatic_execution, opts)

    deterministic_with_operator_acceptance =
      goal_revision
      |> put_in(["execution_policy", "final_acceptance"], "deterministic")
      |> put_in(["authority_policy", "operator_required_for_completion"], false)

    assert {:error, :deterministic_acceptance_contract} =
             ContractValidation.validate_goal_revision(
               deterministic_with_operator_acceptance,
               opts
             )
  end

  test "resolves final authority from both execution and authority policies" do
    check_contract = %{"predicates" => [%{"kind" => "check"}]}

    assert {:ok, :operator} =
             ContractValidation.final_acceptance_authority(
               %{"operator_required_for_completion" => true},
               %{"final_acceptance" => "deterministic"},
               check_contract
             )

    assert {:ok, :operator} =
             ContractValidation.final_acceptance_authority(
               %{"operator_required_for_completion" => false},
               %{"final_acceptance" => "operator"},
               check_contract
             )

    assert {:ok, :deterministic} =
             ContractValidation.final_acceptance_authority(
               %{operator_required_for_completion: false},
               %{final_acceptance: "deterministic"},
               %{predicates: [%{kind: "artifact"}]}
             )

    assert {:error, :deterministic_acceptance_contract} =
             ContractValidation.final_acceptance_authority(
               %{"operator_required_for_completion" => false},
               %{"final_acceptance" => "deterministic"},
               %{"predicates" => [%{"kind" => "review"}]}
             )
  end

  test "validates Goal create, command, plan, admission, and strict-policy fixture coverage" do
    opts = [schema_root: @schema_root]

    assert :ok ==
             ContractValidation.validate_goal_create(
               fixture(@valid_root, "goal-create.basic.json"),
               opts
             )

    strict_create =
      fixture(@valid_root, "goal-create.basic.json")
      |> put_in(
        ["initial_revision", "execution_policy"],
        fixture(@valid_root, "goal-revision.strict-budget.json")["execution_policy"]
      )

    assert :ok == ContractValidation.validate_goal_create(strict_create, opts)

    for filename <- [
          "goal-revision.automatic-soft-budget.json",
          "goal-revision.strict-budget.json"
        ] do
      assert :ok ==
               ContractValidation.validate_goal_revision(fixture(@valid_root, filename), opts)
    end

    for filename <- [
          "goal-command.activate.json",
          "goal-command.pause.json",
          "goal-command.resume.json",
          "goal-command.cancel.json",
          "goal-command.amend.json",
          "goal-command.request-plan-fresh.json",
          "goal-command.request-plan-resume.json",
          "goal-command.accept-plan.json",
          "goal-command.request-plan-decision.json",
          "goal-command.request-scope-decision.json",
          "goal-command.request-review-decision.json",
          "goal-command.request-completion-decision.json",
          "goal-command.resolve-decision.json",
          "goal-command.admit-fresh.json",
          "goal-command.admit-validate-fresh.json",
          "goal-command.admit-resume.json",
          "goal-command.admit-handoff.json",
          "goal-command.add-dependency.json",
          "goal-command.remove-dependency.json",
          "goal-command.achieve.json"
        ] do
      assert :ok ==
               ContractValidation.validate_goal_command(fixture(@valid_root, filename), opts)
    end

    for filename <- ["plan-proposal.basic.json", "plan-proposal.integration-omitted.json"] do
      assert :ok ==
               ContractValidation.validate_plan_proposal(fixture(@valid_root, filename), opts)
    end

    assert :ok ==
             ContractValidation.validate_admission(
               fixture(@valid_root, "admission.provider-scope.json"),
               opts
             )
  end

  test "rejects invalid Goal create, command, plan, and provider-scope fixtures" do
    opts = [schema_root: @schema_root]

    assert {:error, :deterministic_acceptance_contract} =
             ContractValidation.validate_goal_create(
               fixture(@invalid_root, "goal-create.deterministic-review.json"),
               opts
             )

    for filename <- [
          "goal-revision.strict-budget-without-hard-limit.json",
          "goal-revision.automatic-unlimited-budget.json"
        ] do
      assert {:error, {:validation_failed, _}} =
               ContractValidation.validate_goal_revision(fixture(@invalid_root, filename), opts)
    end

    for filename <- [
          "goal-command.derived-admission-field.json",
          "goal-command.request-plan-derived-field.json",
          "goal-command.request-plan-fresh-session-id.json",
          "goal-command.request-plan-handoff.json",
          "goal-command.admit-implement-validation-id.json",
          "goal-command.admit-validate-null-validation-id.json",
          "goal-command.kind-payload-mismatch.json",
          "goal-command.request-decision-caller-options.json",
          "goal-command.request-decision-unsupported-kind.json",
          "goal-command.fresh-session-id.json"
        ] do
      assert {:error, {:validation_failed, _}} =
               ContractValidation.validate_goal_command(fixture(@invalid_root, filename), opts)
    end

    assert {:error, :request_plan_subject_resource_mismatch} =
             ContractValidation.validate_goal_command(
               fixture(@invalid_root, "goal-command.request-plan-subject-resource-mismatch.json"),
               opts
             )

    for filename <- [
          "admission.implement-validation-id.json",
          "admission.validate-null-validation-id.json"
        ] do
      assert {:error, {:validation_failed, _}} =
               ContractValidation.validate_admission(fixture(@invalid_root, filename), opts)
    end

    assert {:error, {:validation_failed, _}} =
             ContractValidation.validate_plan_proposal(
               fixture(@invalid_root, "plan-proposal.missing-baseline.json"),
               opts
             )

    assert {:error, {:validation_failed, _}} =
             ContractValidation.validate_plan_proposal(
               fixture(@invalid_root, "plan-proposal.missing-change-target.json"),
               opts
             )

    assert {:error, {:duplicate_predicate_id, "tests"}} =
             ContractValidation.validate_plan_proposal(
               fixture(@invalid_root, "plan-proposal.duplicate-predicate-id.json"),
               opts
             )

    assert {:error, :provider_scope_resource_operations_mismatch} =
             ContractValidation.validate_admission(
               fixture(@invalid_root, "admission.provider-scope-resource-mismatch.json"),
               opts
             )

    assert {:error, {:validation_failed, _}} =
             ContractValidation.validate_admission(
               fixture(@invalid_root, "admission.non-implement-provider-scope.json"),
               opts
             )

    assert {:error, :provider_change_target_branches_must_differ} =
             ContractValidation.validate_admission(
               fixture(@invalid_root, "admission.provider-scope-equal-branches.json"),
               opts
             )

    for filename <- [
          "task-result.failed-null-reason.json",
          "task-result.failed-nonterminal-reason.json",
          "task-result.plan-proposed-null-proposal.json",
          "task-result.progress-reason.json"
        ] do
      assert {:error, {:validation_failed, _}} =
               ContractValidation.validate_task_result(fixture(@invalid_root, filename), opts)
    end
  end

  test "canonicalizes optional PlanItem.integration while retaining required change targets" do
    opts = [schema_root: @schema_root]
    accepted = fixture(@valid_root, "goal-command.accept-plan.json")
    proposal = accepted["payload"]["proposal"]

    assert :ok == ContractValidation.validate_goal_command(accepted, opts)

    assert accepted["payload"]["proposal_hash"] == proposal_hash(proposal)

    omitted = fixture(@valid_root, "plan-proposal.integration-omitted.json")
    explicit_false = canonical_plan_proposal(omitted)

    assert :ok == ContractValidation.validate_plan_proposal(omitted, opts)
    assert :ok == ContractValidation.validate_plan_proposal(explicit_false, opts)

    mismatch = fixture(@invalid_root, "goal-command.proposal-hash-mismatch.json")

    assert mismatch["payload"]["proposal_hash"] !=
             proposal_hash(mismatch["payload"]["proposal"])

    assert {:error, _reason} = ContractValidation.validate_goal_command(mismatch, opts)
  end

  test "normalizes atom keys recursively but preserves atom values" do
    data = fixture(@valid_root, "admission.basic.json")

    limits =
      data["limits"]
      |> Map.put(:max_turns, data["limits"]["max_turns"])
      |> Map.delete("max_turns")

    atom_keyed =
      data
      |> Map.put(:schema_version, data["schema_version"])
      |> Map.delete("schema_version")
      |> Map.put("limits", limits)

    assert :ok == ContractValidation.validate_admission(atom_keyed, schema_root: @schema_root)

    atom_value = Map.put(data, "purpose", :implement)

    assert {:error, {:validation_failed, _}} =
             ContractValidation.validate_admission(atom_value, schema_root: @schema_root)
  end

  test "requires an absolute explicit schema root and never falls back to CWD" do
    data = fixture(@valid_root, "usage.nullable.json")

    assert {:error, :schema_root_required} = ContractValidation.validate_usage(data)

    assert {:error, :schema_root_must_be_absolute} =
             ContractValidation.validate_usage(data, schema_root: "contracts/v1")

    missing_root =
      Path.join(
        System.tmp_dir!(),
        "symmetry-contracts-missing-#{System.unique_integer([:positive])}"
      )

    assert {:error, {:schema_unreadable, _, :enoent}} =
             ContractValidation.validate_usage(data, schema_root: missing_root)
  end

  test "caches only the resolved immutable document for an explicit root" do
    root =
      Path.join(
        System.tmp_dir!(),
        "symmetry-contracts-cache-#{System.unique_integer([:positive])}"
      )

    File.mkdir_p!(root)
    on_exit(fn -> File.rm_rf(root) end)

    for filename <- ["common.schema.json", "usage.schema.json"] do
      File.cp!(Path.join(@schema_root, filename), Path.join(root, filename))
    end

    data = fixture(@valid_root, "usage.nullable.json")
    assert :ok == ContractValidation.validate_usage(data, schema_root: root)

    usage_path = Path.join(root, "usage.schema.json")

    changed =
      usage_path |> File.read!() |> String.replace("symmetry.usage.v1", "symmetry.usage.changed")

    File.write!(usage_path, changed)

    assert :ok == ContractValidation.validate_usage(data, schema_root: root)
  end

  defp fixture(root, filename) do
    root
    |> Path.join(filename)
    |> File.read!()
    |> Jason.decode!()
  end

  defp canonical_plan_proposal(proposal) do
    Map.update!(proposal, "items", fn items ->
      Enum.map(items, &Map.put_new(&1, "integration", false))
      |> Enum.map(&Map.put_new(&1, "change_target", nil))
    end)
  end

  defp proposal_hash(proposal) do
    proposal
    |> canonical_plan_proposal()
    |> RequestHash.canonical()
    |> then(&("sha256:" <> Base.encode16(&1, case: :lower)))
  end
end
