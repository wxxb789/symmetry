Code.require_file("goals_test_support.exs", __DIR__)

defmodule SymmetryControl.Goals.PlanContractsInterfaceTest do
  use SymmetryControl.OrchestrationCase, async: false

  import SymmetryControl.GoalsTestSupport

  alias SymmetryControl.Goals.PlanContracts
  alias SymmetryControl.Repo

  setup :put_goal_validation_profiles

  defp plan_item(key, resource_id, overrides \\ %{}) do
    Map.merge(
      %{
        key: key,
        title: "Implement #{key}",
        description: "Bounded work for #{key}.",
        required: true,
        integration: false,
        repository_resource_id: resource_id,
        acceptance: check_contract(),
        depends_on_keys: [],
        model_profile: "codex",
        baseline: baseline_subject(resource_id),
        change_target: nil
      },
      overrides
    )
  end

  test "valid_plan_proposal?/3 accepts an envelope bound to the goal revision and rejects another" do
    goal_id = Ecto.UUID.generate()
    resource_id = Ecto.UUID.generate()

    proposal = plan_proposal(goal_id, [plan_item("planned-work", resource_id)])
    goal = %{id: goal_id, current_revision: 1}

    assert PlanContracts.valid_plan_proposal?(proposal, goal, [])
    refute PlanContracts.valid_plan_proposal?(%{proposal | expected_revision: 2}, goal, [])
    refute PlanContracts.valid_plan_proposal?(nil, goal, [])
  end

  test "plan_admission_preflight?/2 requires an integration designation and valid immutable contracts" do
    resource_id = Ecto.UUID.generate()

    assert PlanContracts.plan_admission_preflight?([plan_item("only", resource_id)], [])

    refute PlanContracts.plan_admission_preflight?(
             [plan_item("first", resource_id), plan_item("second", resource_id)],
             []
           )

    refute PlanContracts.plan_admission_preflight?(
             [
               plan_item("first", resource_id, %{depends_on_keys: ["missing"]})
             ],
             []
           )

    refute PlanContracts.plan_admission_preflight?(%{}, [])
  end

  test "valid_acceptance_contract?/1 refuses duplicate predicate ids and unknown keys" do
    assert PlanContracts.valid_acceptance_contract?(check_contract())

    duplicate = %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "Duplicate predicate ids.",
      "predicates" => [
        %{"id" => "check", "kind" => "check", "validator_profile" => "test"},
        %{"id" => "check", "kind" => "check", "validator_profile" => "test"}
      ]
    }

    refute PlanContracts.valid_acceptance_contract?(duplicate)
    refute PlanContracts.valid_acceptance_contract?(Map.put(check_contract(), "extra", true))
    refute PlanContracts.valid_acceptance_contract?(nil)
  end

  test "immutable_acceptance_contracts_valid?/2 resolves every contract against the profile registry" do
    assert PlanContracts.immutable_acceptance_contracts_valid?([check_contract()], []) == :ok

    assert PlanContracts.immutable_acceptance_contracts_valid?(
             [check_contract()],
             validation_profiles: validation_profiles()
           ) == :ok

    unknown_profile = %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "An unknown operator profile.",
      "predicates" => [%{"id" => "check", "kind" => "check", "validator_profile" => "absent"}]
    }

    assert PlanContracts.immutable_acceptance_contracts_valid?([unknown_profile], []) ==
             {:error, :invalid_validation_profile}

    assert PlanContracts.immutable_acceptance_contracts_valid?([check_contract()], :invalid) ==
             {:error, :invalid_validation_profile}
  end

  test "acceptance_contract_matches_resource?/2 binds artifact predicates to one resource" do
    resource_id = Ecto.UUID.generate()

    artifact_contract = %{
      "schema_version" => "symmetry.acceptance.v1",
      "description" => "An approved artifact is required.",
      "predicates" => [
        %{
          "id" => "artifact",
          "kind" => "artifact",
          "resource_id" => resource_id,
          "path" => "build/report.txt"
        }
      ]
    }

    assert PlanContracts.acceptance_contract_matches_resource?(artifact_contract, resource_id)

    refute PlanContracts.acceptance_contract_matches_resource?(
             artifact_contract,
             Ecto.UUID.generate()
           )

    assert PlanContracts.acceptance_contract_matches_resource?(check_contract(), resource_id)
  end

  test "derived_integration_key/1 derives only the single required item key" do
    assert PlanContracts.derived_integration_key([
             %{key: "only", required: true, integration: false}
           ]) == "only"

    assert PlanContracts.derived_integration_key([
             %{key: "first", required: true, integration: true},
             %{key: "second", required: true, integration: false}
           ]) == nil

    assert PlanContracts.derived_integration_key([
             %{key: "first", required: true, integration: false},
             %{key: "second", required: false, integration: true}
           ]) == nil
  end

  test "required_untrimmed_string!/2 returns an unchanged non-empty string or aborts the transaction" do
    assert PlanContracts.required_untrimmed_string!(
             %{"source_branch" => "release/next"},
             "source_branch"
           ) == "release/next"

    assert {:error, :invalid_plan} =
             Repo.transaction(fn ->
               PlanContracts.required_untrimmed_string!(
                 %{source_branch: " release/next"},
                 :source_branch
               )
             end)

    assert {:error, :invalid_plan} =
             Repo.transaction(fn ->
               PlanContracts.required_untrimmed_string!(%{"source_branch" => ""}, "source_branch")
             end)

    assert {:error, :invalid_plan} =
             Repo.transaction(fn ->
               PlanContracts.required_untrimmed_string!(%{}, :source_branch)
             end)
  end
end
