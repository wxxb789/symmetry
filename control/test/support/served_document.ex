defmodule SymmetryControlWeb.ServedDocument do
  @moduledoc """
  Validates a real served document against the canonical bundle the manifest
  gate and the daemon use. A member-name disagreement with the serving
  projection fails here, on the server's own output, instead of passing review.

  It also hosts the served-document gate's shared fixtures: the presence
  oracle the two portal gate suites walk (`assert_served_members!/3`,
  `assert_served_rows!/3`), the member set each assertion walks — the
  definition's declared members read from the contract bundle plus the
  frontend-read list the call passes (`gate_members/3`, `declared_members/2`,
  `definition_variants/2`, `contract_bundle/0`) — and the deterministic ids and
  waiting packet those scenarios transition with (`uuid/1`,
  `waiting_decision_payload/0`). The canonical bundle
  `validate_served_document!/2` compares every gate's response against is the
  same one the presence assertions derive their declared-member half from at
  test time.
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
        flunk(
          "#{name} real served document failed the contract bundle: #{inspect(errors, pretty: true)}"
        )
    end
  end

  @doc """
  A deterministic version-4 UUID: the contract bundle's `UUID` pattern pins the
  version and variant nibbles, so a zeroed UUID is refused by the
  served-document gates that read these ids back.
  """
  def uuid(number),
    do: "00000000-0000-4000-8000-" <> String.pad_leading(Integer.to_string(number), 12, "0")

  @doc """
  The agent's waiting packet. The read model serves the transition's payload as
  the authoritative packet when it carries a decision
  (`workspaces/read_model.ex:221`), so this is the packet the surface reads;
  its members are the ones `Orchestration.validate_decision_packet!/2` admits.
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

  @doc """
  The gate's presence oracle: a member is pinned by key, so a served `null`
  stays accepted while a renamed or omitted key fails here.
  """
  def assert_served_members!(label, served, members) do
    assert is_map(served), "#{label} is not served at all: #{inspect(served)}"

    assert Enum.all?(members, &Map.has_key?(served, &1)),
           "#{label} stopped serving #{inspect(Enum.reject(members, &Map.has_key?(served, &1)))}"
  end

  @doc "Asserts every row of a gate's list is served with `members` present."
  def assert_served_rows!(label, rows, members) do
    assert is_list(rows) and rows != [], "#{label}: the gate must serve at least one row"
    Enum.each(rows, &assert_served_members!(label, &1, members))
  end

  @doc """
  The member set a gate assertion walks: the definition's declared members
  from the contract bundle plus the frontend-read list the call passes. A
  schema-side rename (bundle side) and a serving-side rename or omission
  (either side) fail the same "stopped serving" message, so the assertion
  cannot stay green while the schema, the projection and the read sites
  disagree.
  """
  def gate_members(read, definition, frontend_members) do
    Enum.uniq(declared_members(read, definition) ++ frontend_members)
  end

  @doc """
  The contract bundle's declared members for `definition` in `read`, its
  `oneOf` variants included (`WorkItemTimelineItem` declares its names in the
  variants, not in the parent body); `:root` names the read's own schema. The
  bundle is read at test time, so a rename inside it (e.g.
  `WorkspaceWorkItem.goal_id` -> `goal_ref`) fails the presence assertion
  until the served projection follows: the bundle comparison alone cannot
  refuse it, because the definitions are `additionalProperties: true` and an
  optional member may be absent.
  """
  def declared_members(read, definition) do
    schema = contract_bundle() |> Map.fetch!("#{read}.schema.json")

    schema
    |> definition_variants(definition)
    |> Enum.flat_map(&Map.keys(Map.get(&1, "properties", %{})))
    |> Enum.uniq()
  end

  @doc "The definition itself and its `oneOf` variants; `:root` is the schema."
  def definition_variants(schema, :root), do: [schema]

  def definition_variants(schema, definition) do
    definition = Map.fetch!(schema["definitions"], definition)
    [definition | Map.get(definition, "oneOf", [])]
  end

  @doc "The decoded canonical contract bundle `@schema_bundle` names."
  def contract_bundle,
    do: @schema_bundle |> File.read!() |> Jason.decode!()
end
