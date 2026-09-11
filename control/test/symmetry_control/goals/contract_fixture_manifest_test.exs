defmodule SymmetryControl.Goals.ContractFixtureManifestTest do
  use ExUnit.Case, async: false

  alias SymmetryControl.Goals.ContractValidation

  @manifest_path Path.expand("../../../../contracts/fixtures/manifest.json", __DIR__)
  @schema_root Path.expand("../../../../contracts/v1", __DIR__)
  @fixture_root Path.expand("../../../../contracts/fixtures", __DIR__)

  @validators %{
    "adapter-capabilities" => &ContractValidation.validate_adapter_capabilities/2,
    "admission" => &ContractValidation.validate_admission/2,
    "context-snapshot" => &ContractValidation.validate_context_snapshot/2,
    "decision" => &ContractValidation.validate_decision/2,
    "evidence" => &ContractValidation.validate_evidence/2,
    "goal-command" => &ContractValidation.validate_goal_command/2,
    "goal-create" => &ContractValidation.validate_goal_create/2,
    "goal-revision" => &ContractValidation.validate_goal_revision/2,
    "plan-proposal" => &ContractValidation.validate_plan_proposal/2,
    "task-result" => &ContractValidation.validate_task_result/2,
    "usage" => &ContractValidation.validate_usage/2
  }

  test "public Elixir validators agree with every manifest semantic expectation" do
    manifest = @manifest_path |> File.read!() |> Jason.decode!()
    fixtures = manifest["fixtures"]

    assert manifest["manifest_version"] == 1
    assert is_list(fixtures)
    assert fixtures != []

    fixture_ids = Enum.map(fixtures, & &1["id"])
    assert length(fixture_ids) == MapSet.size(MapSet.new(fixture_ids))

    Enum.each(fixtures, fn fixture ->
      expected = Map.get(fixture, "semantic_valid", Map.fetch!(fixture, "valid"))
      validator = Map.fetch!(@validators, fixture["schema"])
      path = Path.join(@fixture_root, fixture["path"])

      case Jason.decode(File.read!(path)) do
        {:ok, data} ->
          result = validator.(data, schema_root: @schema_root)
          actual = result == :ok

          assert actual == expected,
                 "#{fixture["id"]} expected semantic_valid=#{expected}, got #{inspect(result)}"

          assert_expected_semantic_error(fixture, result)

        {:error, error} ->
          assert fixture["id"] == "goal-command.unpaired-surrogate",
                 "unexpected fixture decode failure for #{fixture["id"]}: #{inspect(error)}"

          assert fixture["schema_valid"] == true
          refute expected
      end
    end)
  end

  defp assert_expected_semantic_error(%{"semantic_error" => expected}, result)
       when is_binary(expected),
       do: assert_error_code(result, expected)

  defp assert_expected_semantic_error(%{"semantic_error_code" => expected}, result)
       when is_binary(expected),
       do: assert_error_code(result, expected)

  defp assert_expected_semantic_error(_fixture, _result), do: :ok

  defp assert_error_code({:error, code}, expected) when is_atom(code),
    do: assert(Atom.to_string(code) == expected)

  defp assert_error_code({:error, {code, _details}}, expected) when is_atom(code),
    do: assert(Atom.to_string(code) == expected)

  defp assert_error_code(result, expected),
    do: flunk("expected semantic error #{expected}, got #{inspect(result)}")
end
