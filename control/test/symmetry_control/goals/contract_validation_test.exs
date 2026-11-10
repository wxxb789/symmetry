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

  test "bounds CommitPath by valid UTF-8 code points across acceptance and evidence" do
    opts = [schema_root: @schema_root]
    composed = String.duplicate("\u00e9", 1024)
    combining = String.duplicate("e\u0301", 512)
    overlong_combining = combining <> "x"
    current_combining = String.duplicate("e\u0301", 1024)

    revision = fixture(@valid_root, "goal-revision.basic.json")

    artifact_revision = fn path ->
      put_in(
        revision,
        ["acceptance_contract", "predicates"],
        [
          %{
            "id" => "artifact",
            "kind" => "artifact",
            "resource_id" => "22222222-2222-4222-8222-222222222222",
            "path" => path
          }
        ]
      )
    end

    assert :ok == ContractValidation.validate_goal_revision(artifact_revision.(composed), opts)
    assert :ok == ContractValidation.validate_goal_revision(artifact_revision.(combining), opts)

    assert {:error, :invalid_commit_path} =
             ContractValidation.validate_goal_revision(
               artifact_revision.(overlong_combining),
               opts
             )

    assert {:error, :invalid_commit_path} =
             ContractValidation.validate_goal_revision(
               artifact_revision.(current_combining),
               opts
             )

    artifact_evidence = fixture(@valid_root, "evidence.artifact.json")

    for field <- ["source_ref", "payload"] do
      invalid_evidence = put_in(artifact_evidence, [field, "path"], current_combining)

      assert {:error, :invalid_commit_path} =
               ContractValidation.validate_evidence(invalid_evidence, opts)
    end

    refute ContractValidation.valid_commit_path?(<<0xFF>>)
  end

  test "preserves public semantic error tags and duplicate predicate details" do
    opts = [schema_root: @schema_root]

    for {filename, validate, tag} <- [
          {"context-snapshot.content-hash-mismatch.json",
           &ContractValidation.validate_context_snapshot/2, :content_hash_mismatch},
          {"task-result.subject-hash-mismatch.json", &ContractValidation.validate_task_result/2,
           :subject_hash_mismatch},
          {"evidence.subject-hash-mismatch.json", &ContractValidation.validate_evidence/2,
           :subject_hash_mismatch},
          {"evidence.source-ref-mismatch.json", &ContractValidation.validate_evidence/2,
           :evidence_identity_mismatch}
        ] do
      assert {:error, {^tag, _}} = validate.(fixture(@invalid_root, filename), opts)
    end

    for {filename, validate} <- [
          {"goal-revision.duplicate-predicate-id.json",
           &ContractValidation.validate_goal_revision/2},
          {"context-snapshot.duplicate-predicate-id.json",
           &ContractValidation.validate_context_snapshot/2},
          {"plan-proposal.duplicate-predicate-id.json",
           &ContractValidation.validate_plan_proposal/2}
        ] do
      assert {:error, {:duplicate_predicate_id, "tests"}} =
               validate.(fixture(@invalid_root, filename), opts)
    end

    for {filename, validate, reason} <- [
          {"goal-create.deterministic-review.json", &ContractValidation.validate_goal_create/2,
           :deterministic_acceptance_contract},
          {"admission.provider-scope-resource-mismatch.json",
           &ContractValidation.validate_admission/2,
           :provider_scope_resource_operations_mismatch},
          {"admission.provider-scope-equal-branches.json",
           &ContractValidation.validate_admission/2, :provider_change_target_branches_must_differ}
        ] do
      assert {:error, ^reason} = validate.(fixture(@invalid_root, filename), opts)
    end
  end

  test "rejects synthesized invalid values after schema validation" do
    opts = [schema_root: @schema_root]

    blocked_without_blocker =
      fixture(@valid_root, "task-result.candidate.json")
      |> Map.put("kind", "blocked")

    assert {:error, :blocked_result_requires_blocker} =
             ContractValidation.validate_task_result(blocked_without_blocker, opts)

    invalid_decision =
      fixture(@valid_root, "decision.open.json")
      |> Map.put("unexpected", true)

    assert {:error, {:validation_failed, _}} =
             ContractValidation.validate_decision(invalid_decision, opts)
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

  test "validates a synthesized strict-policy Goal create" do
    opts = [schema_root: @schema_root]

    strict_create =
      fixture(@valid_root, "goal-create.basic.json")
      |> put_in(
        ["initial_revision", "execution_policy"],
        fixture(@valid_root, "goal-revision.strict-budget.json")["execution_policy"]
      )

    assert :ok == ContractValidation.validate_goal_create(strict_create, opts)
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

  test "malformed plan items remain schema errors instead of crashing canonicalization" do
    opts = [schema_root: @schema_root]

    for filename <- [
          "goal-command.accept-plan.json",
          "goal-command.request-plan-decision.json"
        ],
        malformed_item <- [nil, "not-an-object", 42] do
      malformed =
        fixture(@valid_root, filename)
        |> put_in(["payload", "proposal", "items"], [malformed_item])

      assert {:error, {:validation_failed, _}} =
               ContractValidation.validate_goal_command(malformed, opts)
    end
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
