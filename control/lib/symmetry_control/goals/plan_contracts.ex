defmodule SymmetryControl.Goals.PlanContracts do
  @moduledoc false

  import SymmetryControl.Goals.ContractValidation, only: [validate_contract: 3]

  import SymmetryControl.Goals.Internal,
    only: [
      is_short_identifier?: 1,
      map_has_key?: 2,
      only_known_keys?: 2,
      parse_subject: 1,
      required_safe_positive_integer: 2,
      rollback: 1,
      valid_long_text?: 1,
      valid_uuid?: 1,
      value: 2,
      value: 3
    ]

  alias SymmetryControl.Goals.ContractValidation
  alias SymmetryControl.Goals.ValidationProfiles

  @max_plan_items 256
  @max_plan_dependencies_per_item 256

  @doc false

  def valid_acceptance_contract?(contract) when is_map(contract) do
    predicates = value(contract, :predicates)

    predicate_ids =
      if is_list(predicates) and Enum.all?(predicates, &is_map/1),
        do: Enum.map(predicates, &value(&1, :id)),
        else: []

    only_known_keys?(contract, ["schema_version", "description", "predicates"]) and
      value(contract, :schema_version) == "symmetry.acceptance.v1" and
      is_binary(value(contract, :description)) and
      String.length(value(contract, :description)) in 1..16_384 and
      is_list(predicates) and length(predicates) in 1..256 and
      Enum.all?(predicates, &valid_predicate?/1) and
      length(predicate_ids) == MapSet.size(MapSet.new(predicate_ids))
  end

  def valid_acceptance_contract?(_contract), do: false

  defp immutable_plan_contracts_valid?(items, opts) do
    items
    |> Enum.map(&value(&1, :acceptance))
    |> immutable_acceptance_contracts_valid?(opts)
  end

  @doc false

  def immutable_acceptance_contracts_valid?(acceptance_contracts, opts)
      when is_list(acceptance_contracts) and is_list(opts) do
    registry_opts =
      case Keyword.fetch(opts, :validation_profiles) do
        {:ok, profiles} -> [profiles: profiles]
        :error -> []
      end

    with {:ok, profiles} <- ValidationProfiles.snapshot(registry_opts) do
      if Enum.all?(acceptance_contracts, fn acceptance_contract ->
           match?(
             {:ok, _bindings},
             ValidationProfiles.bindings_for_acceptance(acceptance_contract, profiles: profiles)
           )
         end),
         do: :ok,
         else: {:error, :invalid_validation_profile}
    else
      {:error, _reason} -> {:error, :invalid_validation_profile}
    end
  end

  def immutable_acceptance_contracts_valid?(_, _), do: {:error, :invalid_validation_profile}

  @doc false

  def valid_plan_proposal?(proposal, goal, opts) when is_map(proposal) do
    items = value(proposal, :items)

    validate_contract(:plan_proposal, proposal, opts) == :ok and
      valid_plan_envelope?(proposal, goal) and
      is_list(items) and
      length(items) in 1..@max_plan_items and
      Enum.all?(items, &valid_plan_item?/1)
  end

  def valid_plan_proposal?(_proposal, _goal, _opts), do: false

  @doc false

  # This is intentionally Goal-context admission validation rather than JSON
  # Schema validation: a PlanProposal remains wire-valid without an integration
  # item, but it cannot be admitted as a complete Goal plan.
  def plan_admission_preflight?(items, opts) when is_list(items) and is_list(opts) do
    if Enum.all?(items, &is_map/1) do
      keys = Enum.map(items, &value(&1, :key))
      items_by_key = Map.new(items, &{value(&1, :key), &1})

      plan_integration_designated?(items) and
        Enum.all?(items, &valid_plan_item?/1) and
        length(keys) == MapSet.size(MapSet.new(keys)) and
        Enum.all?(items, &plan_item_dependencies_valid?(&1, items_by_key)) and
        plan_dependencies_acyclic?(items_by_key) and
        immutable_plan_contracts_valid?(items, opts) == :ok
    else
      false
    end
  end

  def plan_admission_preflight?(_items, _opts), do: false

  # The designation rule lives in ContractValidation.integration_designation/1.
  defp plan_integration_designated?(items) do
    items
    |> designation_triples()
    |> ContractValidation.integration_designation()
    |> case do
      {:conflict} -> false
      {:derived, _ref} -> true
      {:explicit, refs} -> refs != []
    end
  end

  @doc false

  # Only the exactly-one-required-item case derives a designation, and the
  # admission preflight already refused a contradicting explicit one.
  def derived_integration_key(items) do
    items
    |> designation_triples()
    |> ContractValidation.integration_designation()
    |> case do
      {:derived, ref} -> ref
      _other -> nil
    end
  end

  defp designation_triples(items) do
    Enum.map(items, fn item ->
      {value(item, :key), value(item, :required, true) == true,
       value(item, :integration, false) == true}
    end)
  end

  defp plan_item_dependencies_valid?(item, items_by_key) do
    item_key = value(item, :key)
    dependencies = value(item, :depends_on_keys, [])

    Enum.all?(dependencies, &(Map.has_key?(items_by_key, &1) and &1 != item_key)) and
      plan_dependency_baseline_same_resource?(item, items_by_key)
  end

  defp plan_dependency_baseline_same_resource?(item, items_by_key) do
    case value(value(item, :baseline), :kind) do
      "dependency" ->
        dependency_key = value(value(item, :baseline), :key)
        dependency = Map.get(items_by_key, dependency_key)

        dependency_key in value(item, :depends_on_keys, []) and is_map(dependency) and
          value(dependency, :repository_resource_id) == value(item, :repository_resource_id)

      _ ->
        true
    end
  end

  defp plan_dependencies_acyclic?(items_by_key) do
    Enum.all?(Map.keys(items_by_key), fn key ->
      not plan_dependency_cycle?(key, items_by_key, MapSet.new(), MapSet.new())
    end)
  end

  defp plan_dependency_cycle?(key, items_by_key, visiting, visited) do
    cond do
      MapSet.member?(visiting, key) ->
        true

      MapSet.member?(visited, key) ->
        false

      true ->
        item = Map.fetch!(items_by_key, key)
        visiting = MapSet.put(visiting, key)
        visited = MapSet.put(visited, key)

        Enum.any?(value(item, :depends_on_keys, []), fn dependency_key ->
          plan_dependency_cycle?(dependency_key, items_by_key, visiting, visited)
        end)
    end
  end

  defp valid_plan_envelope?(proposal, goal) do
    full_keys = ["schema_version", "proposal_id", "goal_id", "expected_revision", "items"]

    only_known_keys?(proposal, full_keys) and
      Enum.all?(full_keys, &map_has_key?(proposal, &1)) and
      value(proposal, :schema_version) == "symmetry.plan.v1" and
      valid_uuid?(value(proposal, :proposal_id)) and value(proposal, :goal_id) == goal.id and
      required_safe_positive_integer(proposal, :expected_revision) ==
        {:ok, goal.current_revision}
  end

  defp valid_plan_item?(item) when is_map(item) do
    required_keys = [
      "key",
      "title",
      "description",
      "required",
      "repository_resource_id",
      "acceptance",
      "depends_on_keys",
      "model_profile",
      "baseline",
      "change_target"
    ]

    dependencies = value(item, :depends_on_keys)

    only_known_keys?(item, required_keys ++ ["integration"]) and
      Enum.all?(required_keys, &map_has_key?(item, &1)) and
      is_short_identifier?(value(item, :key)) and
      valid_long_text?(value(item, :title)) and valid_long_text?(value(item, :description)) and
      is_boolean(value(item, :required)) and is_boolean(value(item, :integration, false)) and
      valid_uuid?(value(item, :repository_resource_id)) and
      valid_acceptance_contract?(value(item, :acceptance)) and is_list(dependencies) and
      length(dependencies) <= @max_plan_dependencies_per_item and
      Enum.all?(dependencies, &is_short_identifier?/1) and
      length(dependencies) == MapSet.size(MapSet.new(dependencies)) and
      is_short_identifier?(value(item, :model_profile)) and
      valid_plan_baseline?(value(item, :baseline), value(item, :repository_resource_id)) and
      valid_plan_change_target?(value(item, :change_target))
  end

  defp valid_plan_item?(_item), do: false

  defp valid_plan_baseline?(nil, _resource_id), do: false

  defp valid_plan_baseline?(baseline, resource_id) when is_map(baseline) do
    case value(baseline, :kind) do
      "subject" ->
        only_known_keys?(baseline, ["kind", "subject"]) and
          map_has_key?(baseline, "subject") and
          match?(
            {:ok, %{"resource_id" => ^resource_id}},
            parse_subject(value(baseline, :subject))
          )

      "dependency" ->
        only_known_keys?(baseline, ["kind", "key"]) and
          is_short_identifier?(value(baseline, :key))

      _ ->
        false
    end
  end

  defp valid_plan_baseline?(_baseline, _resource_id), do: false

  defp valid_plan_change_target?(nil), do: true

  defp valid_plan_change_target?(target) when is_map(target) do
    case value(target, :kind) do
      "branches" ->
        only_known_keys?(target, ["kind", "source_branch", "target_branch"]) and
          untrimmed_string?(value(target, :source_branch)) and
          untrimmed_string?(value(target, :target_branch)) and
          value(target, :source_branch) != value(target, :target_branch)

      "pull_request" ->
        only_known_keys?(target, ["kind", "pull_request_url"]) and
          untrimmed_string?(value(target, :pull_request_url))

      _ ->
        false
    end
  end

  defp valid_plan_change_target?(_target), do: false

  @doc false

  def required_untrimmed_string!(map, key) do
    case value(map, key) do
      value when is_binary(value) ->
        if value == String.trim(value) and value != "", do: value, else: rollback(:invalid_plan)

      _ ->
        rollback(:invalid_plan)
    end
  end

  defp untrimmed_string?(value) when is_binary(value),
    do: value == String.trim(value) and value != ""

  defp untrimmed_string?(_value), do: false

  defp valid_predicate?(predicate) when is_map(predicate) do
    case value(predicate, :kind) do
      "check" ->
        only_known_keys?(predicate, ["id", "kind", "validator_profile"]) and
          is_short_identifier?(value(predicate, :id)) and
          is_short_identifier?(value(predicate, :validator_profile))

      "artifact" ->
        only_known_keys?(predicate, ["id", "kind", "resource_id", "path"]) and
          is_short_identifier?(value(predicate, :id)) and
          valid_uuid?(value(predicate, :resource_id)) and
          valid_commit_path?(value(predicate, :path))

      "review" ->
        only_known_keys?(predicate, ["id", "kind", "reviewer_profile"]) and
          is_short_identifier?(value(predicate, :id)) and
          is_short_identifier?(value(predicate, :reviewer_profile))

      "operator_acceptance" ->
        only_known_keys?(predicate, ["id", "kind"]) and
          is_short_identifier?(value(predicate, :id))

      _ ->
        false
    end
  end

  defp valid_predicate?(_), do: false

  @doc false

  def acceptance_contract_matches_resource?(contract, resource_id) do
    Enum.all?(value(contract, :predicates, []), fn predicate ->
      value(predicate, :kind) != "artifact" or value(predicate, :resource_id) == resource_id
    end)
  end

  defp valid_commit_path?(value), do: ContractValidation.valid_commit_path?(value)
end
