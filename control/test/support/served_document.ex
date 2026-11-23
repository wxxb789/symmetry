defmodule SymmetryControlWeb.ServedDocument do
  @moduledoc """
  Validates a real served document against the canonical bundle the manifest
  gate and the daemon use. A member-name disagreement with the serving
  projection fails here, on the server's own output, instead of passing review.
  """
  import ExUnit.Assertions

  alias SymmetryControl.ContractSchema

  @schema_bundle Path.expand("../../../daemon/internal/contracts/schema_bundle.json", __DIR__)

  @doc "Validates `document` against the bundle's `\"\#{name}.schema.json\"`."
  def validate_served_document!(name, document) do
    bundle = @schema_bundle |> File.read!() |> Jason.decode!()

    schema =
      bundle
      |> Map.fetch!("#{name}.schema.json")
      |> ContractSchema.rewrite_pcre2_pattern_escapes()
      |> ExJsonSchema.Schema.resolve()

    case ExJsonSchema.Validator.validate(schema, document) do
      :ok ->
        :ok

      {:error, errors} ->
        flunk("#{name} real served document failed the contract bundle: #{inspect(errors, pretty: true)}")
    end
  end

  @doc """
  A deterministic v4 UUID: the contract bundle's `UUID` pattern pins the version and variant
  nibbles, so a zeroed UUID is refused by the served-document gates.
  """
  def uuid(number), do: "00000000-0000-4000-8000-" <> String.pad_leading(Integer.to_string(number), 12, "0")

  @doc """
  The agent's waiting packet: at `read_model.ex:221` a payload carrying `"decision"` wins, so
  this is the packet served, with the members `Orchestration.validate_decision_packet!/2` admits.
  """
  def waiting_decision_payload do
    %{
      "question" => "Which reviewer should accept this?",
      "decision" => %{
        "context" => "The attempt needs an operator decision before it continues.",
        "reason" => "consequential",
        "options" => [
          %{
            "id" => "reviewer-a",
            "label" => "Reviewer A",
            "consequence" => "Reviews the change first."
          },
          %{
            "id" => "reviewer-b",
            "label" => "Reviewer B",
            "consequence" => "Reviews the change second."
          }
        ],
        "recommended_option_id" => "reviewer-a"
      }
    }
  end

  def assert_served_members!(label, served, members) do
    assert is_map(served), "#{label} is not served at all: #{inspect(served)}"

    assert Enum.all?(members, &Map.has_key?(served, &1)),
           "#{label} stopped serving #{inspect(Enum.reject(members, &Map.has_key?(served, &1)))}"
  end

  def assert_served_rows!(label, rows, members) do
    assert is_list(rows) and rows != [], "#{label}: the gate must serve at least one row"
    Enum.each(rows, &assert_served_members!(label, &1, members))
  end

  def gate_members(read, definition, frontend_members) do
    Enum.uniq(declared_members(read, definition) ++ frontend_members)
  end

  def declared_members(read, definition) do
    schema = Map.fetch!(contract_bundle(), "#{read}.schema.json")

    schema
    |> definition_variants(definition)
    |> Enum.flat_map(&Map.keys(Map.get(&1, "properties", %{})))
    |> Enum.uniq()
  end

  def definition_variants(schema, :root), do: [schema]

  def definition_variants(schema, definition) do
    definition = Map.fetch!(schema["definitions"], definition)
    [definition | Map.get(definition, "oneOf", [])]
  end

  def contract_bundle, do: @schema_bundle |> File.read!() |> Jason.decode!()
end
