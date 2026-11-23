defmodule SymmetryControl.Goals.ContractFixtureManifestTest do
  use ExUnit.Case, async: true

  alias SymmetryControl.ContractSchema
  alias SymmetryControl.Goals.ContractValidation
  alias SymmetryControlWeb.StrictJSONDecoder

  @manifest_path Path.expand("../../../../contracts/fixtures/manifest.json", __DIR__)
  @schema_root Path.expand("../../../../contracts/v1", __DIR__)
  @fixture_root Path.expand("../../../../contracts/fixtures", __DIR__)
  @schema_bundle Path.expand("../../../../daemon/internal/contracts/schema_bundle.json", __DIR__)
  # Presentation reads consumed by the portal/daemon boundaries rather than by a
  # Goals semantic validator: the canonical bundle schema is the expectation.
  @presentation_reads [
    "attention-read",
    "goal-read",
    "work-detail-read",
    "work-timeline-read"
  ]
  # Raw-corpus acceptance is measured on the decode each row's schema selects:
  # the strict duplicate-member walk for machine requests, Jason otherwise.
  @machine_request_schemas ["evidence", "evidence-batch"]

  @validators %{
    "adapter-capabilities" => &ContractValidation.validate_adapter_capabilities/2,
    "admission" => &ContractValidation.validate_admission/2,
    "connections-read" => &ContractValidation.validate_connections_read/2,
    "context-snapshot" => &ContractValidation.validate_context_snapshot/2,
    "decision" => &ContractValidation.validate_decision/2,
    "evidence" => &ContractValidation.validate_evidence/2,
    "evidence-batch" => &ContractValidation.validate_evidence_batch/2,
    "evidence-batch-response" => &ContractValidation.validate_evidence_batch_response/2,
    "evidence-batch-conflict-details" => &ContractValidation.validate_evidence_batch_conflict_details/2,
    "goal-command" => &ContractValidation.validate_goal_command/2,
    "goal-create" => &ContractValidation.validate_goal_create/2,
    "goal-revision" => &ContractValidation.validate_goal_revision/2,
    "plan-proposal" => &ContractValidation.validate_plan_proposal/2,
    "task-result" => &ContractValidation.validate_task_result/2,
    "usage" => &ContractValidation.validate_usage/2,
    "workspace-read" => &ContractValidation.validate_workspace_read/2,
    "chat-read" => &ContractValidation.validate_chat_read/2
  }

  test "public Elixir validators agree with every manifest semantic expectation" do
    manifest = @manifest_path |> File.read!() |> Jason.decode!()
    fixtures = manifest["fixtures"]

    assert manifest["manifest_version"] == 1
    assert is_list(fixtures)
    assert fixtures != []

    fixture_ids = Enum.map(fixtures, & &1["id"])
    assert length(fixture_ids) == MapSet.size(MapSet.new(fixture_ids))

    bundle = @schema_bundle |> File.read!() |> Jason.decode!()

    read_schemas =
      Map.new(@presentation_reads, fn name ->
        schema =
          bundle
          |> Map.fetch!(name <> ".schema.json")
          |> ContractSchema.rewrite_pcre2_pattern_escapes()
          |> ExJsonSchema.Schema.resolve()

        {name, schema}
      end)

    Enum.each(fixtures, fn fixture ->
      path = Path.join(@fixture_root, fixture["path"])

      if fixture["wire_error"] do
        assert_raw_wire_read(fixture, read_schemas)
      else
        expected = Map.get(fixture, "semantic_valid", Map.fetch!(fixture, "valid"))

        case Jason.decode(File.read!(path)) do
          {:ok, data} ->
            result = validate_fixture(fixture["schema"], data, read_schemas)
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
      end
    end)
  end

  defp assert_raw_wire_read(fixture, read_schemas) do
    body = @fixture_root |> Path.join(fixture["path"]) |> File.read!()
    accepts = get_in(fixture, ["runtime_divergence", "elixir"]) == "accepts"

    actual =
      case raw_wire_decode(fixture["schema"], body) do
        {:ok, data} -> validate_fixture(fixture["schema"], data, read_schemas) == :ok
        :error -> false
      end

    assert actual == accepts,
           "#{fixture["id"]} expected elixir wire acceptance=#{accepts}, got #{actual}"
  end

  defp raw_wire_decode(schema, body) when schema in @machine_request_schemas do
    {:ok, StrictJSONDecoder.decode!(body)}
  rescue
    _error in [Jason.DecodeError, StrictJSONDecoder.DuplicateJSONKeyError] -> :error
  end

  defp raw_wire_decode(_schema, body) do
    case Jason.decode(body) do
      {:ok, value} -> {:ok, value}
      {:error, _error} -> :error
    end
  end

  defp validate_fixture(schema, data, read_schemas) when schema in @presentation_reads do
    ExJsonSchema.Validator.validate(Map.fetch!(read_schemas, schema), data)
  end

  defp validate_fixture(schema, data, _read_schemas) do
    validator = Map.fetch!(@validators, schema)
    validator.(data, schema_root: @schema_root)
  end

  defp assert_expected_semantic_error(%{"semantic_error" => expected}, result) when is_binary(expected),
    do: assert_error_code(result, expected)

  defp assert_expected_semantic_error(%{"semantic_error_code" => expected}, result) when is_binary(expected),
    do: assert_error_code(result, expected)

  defp assert_expected_semantic_error(_fixture, _result), do: :ok

  defp assert_error_code({:error, code}, expected) when is_atom(code), do: assert(Atom.to_string(code) == expected)

  defp assert_error_code({:error, {code, _details}}, expected) when is_atom(code),
    do: assert(Atom.to_string(code) == expected)

  defp assert_error_code(result, expected), do: flunk("expected semantic error #{expected}, got #{inspect(result)}")
end
