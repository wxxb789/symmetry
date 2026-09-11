defmodule SymmetryControl.Goals.ContractValidation do
  @moduledoc """
  Boundary validation for the additive Goal v1 control envelopes.

  The caller must pass `schema_root: absolute_path` where `absolute_path` is
  the configured `contracts/v1` directory. There is intentionally no default
  path and no lookup relative to the current working directory.

  The public functions return `:ok` for valid data or
  `{:error, reason}` for an invalid schema root, unavailable validator,
  unreadable canonical schema, or validation failure:

       validate_goal_revision(data, schema_root: contracts_v1_path)
       validate_goal_create(data, schema_root: contracts_v1_path)
       validate_goal_command(data, schema_root: contracts_v1_path)
       validate_plan_proposal(data, schema_root: contracts_v1_path)
       validate_admission(data, schema_root: contracts_v1_path)
      validate_context_snapshot(data, schema_root: contracts_v1_path)
      validate_task_result(data, schema_root: contracts_v1_path)
      validate_evidence(data, schema_root: contracts_v1_path)
      validate_decision(data, schema_root: contracts_v1_path)
      validate_usage(data, schema_root: contracts_v1_path)
      validate_adapter_capabilities(data, schema_root: contracts_v1_path)

  Atom map keys are normalized to JSON string keys recursively. Values are
  preserved as-is; atom values are not converted to strings. The canonical
  schema documents are resolved once per absolute root and envelope in an
  immutable `:persistent_term` cache. Context snapshots additionally require
  `content_hash` to match the SHA-256 of canonical JSON with that top-level
  field omitted; required stable sections remain enforced by the canonical
  schema.
  """

  @type validation_result :: :ok | {:error, term()}

  @schema_files %{
    goal_create: "goal-create.schema.json",
    goal_command: "goal-command.schema.json",
    plan_proposal: "plan-proposal.schema.json",
    goal_revision: "goal-revision.schema.json",
    admission: "admission.schema.json",
    context_snapshot: "context-snapshot.schema.json",
    task_result: "task-result.schema.json",
    evidence: "evidence.schema.json",
    decision: "decision.schema.json",
    usage: "usage.schema.json",
    adapter_capabilities: "adapter-capabilities.schema.json"
  }

  @draft7 "http://json-schema.org/draft-07/schema#"
  @cache_prefix {__MODULE__, :resolved_schema}
  @task_result_terminal_failure_reasons ~w(
    auth
    quota
    rate_limit
    network
    unsupported_version
    resume_rejected
    handoff_unsupported
    context_overflow
    missing_result
    process_failure
    cancelled
    unknown_outcome
  )

  @spec validate_goal_revision(map() | list(), keyword()) :: validation_result()
  def validate_goal_revision(data, opts \\ []), do: validate(:goal_revision, data, opts)

  @spec validate_goal_create(map() | list(), keyword()) :: validation_result()
  def validate_goal_create(data, opts \\ []), do: validate(:goal_create, data, opts)

  @spec validate_goal_command(map() | list(), keyword()) :: validation_result()
  def validate_goal_command(data, opts \\ []), do: validate(:goal_command, data, opts)

  @spec validate_plan_proposal(map() | list(), keyword()) :: validation_result()
  def validate_plan_proposal(data, opts \\ []), do: validate(:plan_proposal, data, opts)

  @spec validate_admission(map() | list(), keyword()) :: validation_result()
  def validate_admission(data, opts \\ []), do: validate(:admission, data, opts)

  @spec validate_context_snapshot(map() | list(), keyword()) :: validation_result()
  def validate_context_snapshot(data, opts \\ []), do: validate(:context_snapshot, data, opts)

  @spec validate_task_result(map() | list(), keyword()) :: validation_result()
  def validate_task_result(data, opts \\ []), do: validate(:task_result, data, opts)

  @spec validate_evidence(map() | list(), keyword()) :: validation_result()
  def validate_evidence(data, opts \\ []), do: validate(:evidence, data, opts)

  @spec validate_decision(map() | list(), keyword()) :: validation_result()
  def validate_decision(data, opts \\ []), do: validate(:decision, data, opts)

  @spec validate_usage(map() | list(), keyword()) :: validation_result()
  def validate_usage(data, opts \\ []), do: validate(:usage, data, opts)

  @spec validate_adapter_capabilities(map() | list(), keyword()) :: validation_result()
  def validate_adapter_capabilities(data, opts \\ []),
    do: validate(:adapter_capabilities, data, opts)

  @spec validate_adapter(map() | list(), keyword()) :: validation_result()
  def validate_adapter(data, opts \\ []), do: validate_adapter_capabilities(data, opts)

  @doc """
  Resolve the effective final-acceptance authority for one immutable revision.

  A revision can require an exact operator completion Decision either directly
  through `execution_policy.final_acceptance` or through the broader
  `authority_policy.operator_required_for_completion` fence. Deterministic
  acceptance is valid only when no operator fence applies and every predicate
  is independently checkable.
  """
  @spec final_acceptance_authority(map(), map(), map()) ::
          {:ok, :operator | :deterministic} | {:error, term()}
  def final_acceptance_authority(authority_policy, execution_policy, acceptance_contract)
      when is_map(authority_policy) and is_map(execution_policy) and
             is_map(acceptance_contract) do
    predicates = value(acceptance_contract, "predicates")
    final_acceptance = value(execution_policy, "final_acceptance")

    cond do
      final_acceptance == "operator" or
          value(authority_policy, "operator_required_for_completion", true) == true ->
        {:ok, :operator}

      final_acceptance == "deterministic" and is_list(predicates) and predicates != [] and
          Enum.all?(predicates, &deterministic_predicate?/1) ->
        {:ok, :deterministic}

      final_acceptance == "deterministic" ->
        {:error, :deterministic_acceptance_contract}

      true ->
        {:error, :invalid_final_acceptance}
    end
  end

  def final_acceptance_authority(_, _, _), do: {:error, :invalid_final_acceptance}

  defp validate(kind, data, opts) when is_list(opts) do
    with :ok <- ensure_ex_json_schema(),
         {:ok, schema_root} <- schema_root(opts),
         {:ok, normalized} <- normalize_json_keys(data),
         {:ok, schema} <- resolved_schema(schema_root, kind) do
      case validate_resolved_schema(schema, normalized) do
        :ok -> validate_semantics(kind, normalized)
        result -> result
      end
    end
  end

  defp validate(_kind, _data, _opts), do: {:error, :invalid_options}

  defp ensure_ex_json_schema do
    if Code.ensure_loaded?(ExJsonSchema.Schema) and Code.ensure_loaded?(ExJsonSchema.Validator) do
      :ok
    else
      {:error, :ex_json_schema_unavailable}
    end
  end

  defp schema_root(opts) do
    case Keyword.fetch(opts, :schema_root) do
      :error -> {:error, :schema_root_required}
      {:ok, root} when not is_binary(root) -> {:error, :schema_root_must_be_absolute}
      {:ok, root} -> validate_absolute_root(root)
    end
  end

  defp validate_absolute_root(root) do
    if Path.type(root) == :absolute do
      {:ok, Path.expand(root)}
    else
      {:error, :schema_root_must_be_absolute}
    end
  end

  defp resolved_schema(schema_root, kind) do
    key = {@cache_prefix, schema_root, kind}

    case :persistent_term.get(key, :missing) do
      :missing ->
        with {:ok, schema} <- load_schema(schema_root, kind),
             {:ok, resolved} <- resolve_schema(schema) do
          :persistent_term.put(key, resolved)
          {:ok, resolved}
        end

      resolved ->
        {:ok, resolved}
    end
  end

  defp load_schema(schema_root, kind) do
    with {:ok, filename} <- Map.fetch(@schema_files, kind),
         {:ok, common} <- read_schema(Path.join(schema_root, "common.schema.json")),
         {:ok, envelope} <- read_schema(Path.join(schema_root, filename)),
         :ok <- ensure_draft7(common, "common.schema.json"),
         :ok <- ensure_draft7(envelope, filename) do
      materialized = materialize_schema(envelope, common)

      with :ok <- reject_external_refs(materialized, filename) do
        {:ok, materialized}
      end
    else
      :error -> {:error, {:unknown_schema, kind}}
      {:error, _reason} = error -> error
    end
  end

  defp read_schema(path) do
    case File.read(path) do
      {:ok, contents} ->
        case Jason.decode(contents) do
          {:ok, schema} when is_map(schema) -> {:ok, schema}
          {:ok, _schema} -> {:error, {:schema_not_an_object, path}}
          {:error, reason} -> {:error, {:schema_json_invalid, path, reason}}
        end

      {:error, reason} ->
        {:error, {:schema_unreadable, path, reason}}
    end
  end

  defp ensure_draft7(%{"$schema" => @draft7}, _filename), do: :ok

  defp ensure_draft7(%{"$schema" => version}, filename),
    do: {:error, {:unsupported_schema_dialect, filename, version}}

  defp ensure_draft7(_schema, filename), do: {:error, {:schema_dialect_missing, filename}}

  defp reject_external_refs(schema, filename) do
    case external_ref(schema) do
      nil -> :ok
      ref -> {:error, {:external_schema_ref, filename, ref}}
    end
  end

  defp external_ref(value) when is_map(value) do
    case Map.get(value, "$ref") do
      ref when is_binary(ref) ->
        if String.starts_with?(ref, "#"),
          do: Enum.find_value(value, &external_ref_pair/1),
          else: ref

      _ ->
        Enum.find_value(value, &external_ref_pair/1)
    end
  end

  defp external_ref(value) when is_list(value), do: Enum.find_value(value, &external_ref/1)
  defp external_ref(_value), do: nil

  defp external_ref_pair({"$ref", ref}) when is_binary(ref),
    do: if(String.starts_with?(ref, "#"), do: nil, else: ref)

  defp external_ref_pair({_key, value}), do: external_ref(value)

  defp materialize_schema(envelope, common) do
    definitions =
      common
      |> Map.get("definitions", %{})
      |> Map.merge(Map.get(envelope, "definitions", %{}))

    envelope
    |> Map.put("definitions", definitions)
    |> rewrite_common_refs()
    |> rewrite_pcre2_pattern_escapes()
  end

  defp rewrite_common_refs(value) when is_map(value) do
    Enum.into(value, %{}, fn
      {"$ref", "common.schema.json#/definitions/" <> definition} ->
        {"$ref", "#/definitions/" <> definition}

      {key, child} ->
        {key, rewrite_common_refs(child)}
    end)
  end

  defp rewrite_common_refs(value) when is_list(value), do: Enum.map(value, &rewrite_common_refs/1)
  defp rewrite_common_refs(value), do: value

  # Draft 7 permits ECMA-262 `\\u0000` patterns, while PCRE2 rejects that
  # escape. Keep the canonical document untouched and adapt only the local
  # validator copy to the equivalent PCRE2 `\\x{0}` form.
  defp rewrite_pcre2_pattern_escapes(value) when is_map(value) do
    Enum.into(value, %{}, fn
      {"pattern", pattern} when is_binary(pattern) ->
        {"pattern", String.replace(pattern, "\\u0000", "\\x{0}")}

      {key, child} ->
        {key, rewrite_pcre2_pattern_escapes(child)}
    end)
  end

  defp rewrite_pcre2_pattern_escapes(value) when is_list(value),
    do: Enum.map(value, &rewrite_pcre2_pattern_escapes/1)

  defp rewrite_pcre2_pattern_escapes(value), do: value

  defp resolve_schema(schema) do
    try do
      {:ok, apply(ExJsonSchema.Schema, :resolve, [schema])}
    rescue
      exception -> {:error, {:schema_resolution_failed, exception}}
    catch
      kind, reason -> {:error, {:schema_resolution_failed, {kind, reason}}}
    end
  end

  defp validate_resolved_schema(schema, data) do
    try do
      case apply(ExJsonSchema.Validator, :validate, [schema, data, [error_formatter: false]]) do
        :ok -> :ok
        {:error, errors} -> {:error, {:validation_failed, errors}}
        other -> {:error, {:validation_failed, other}}
      end
    rescue
      exception -> {:error, {:validation_failed, exception}}
    catch
      kind, reason -> {:error, {:validation_failed, {kind, reason}}}
    end
  end

  defp validate_semantics(:goal_revision, revision) do
    with :ok <- validate_acceptance_predicate_ids(revision["acceptance_contract"]),
         :ok <- validate_goal_revision_semantics(revision) do
      :ok
    end
  end

  defp validate_semantics(:goal_create, %{"initial_revision" => revision}) do
    with :ok <- validate_acceptance_predicate_ids(revision["acceptance_contract"]),
         :ok <- validate_goal_revision_semantics(revision) do
      :ok
    end
  end

  defp validate_semantics(:plan_proposal, %{"items" => items}) when is_list(items) do
    Enum.reduce_while(items, :ok, fn item, :ok ->
      with :ok <- validate_acceptance_predicate_ids(item["acceptance"]),
           :ok <- validate_provider_change_target(item["change_target"]) do
        {:cont, :ok}
      else
        error -> {:halt, error}
      end
    end)
  end

  defp validate_semantics(:plan_proposal, _proposal), do: :ok

  defp validate_semantics(:goal_command, command) do
    case {command["kind"], command["payload"]} do
      {"amend", %{"revision_contract" => revision}} ->
        with :ok <- validate_acceptance_predicate_ids(revision["acceptance_contract"]),
             :ok <- validate_goal_revision_semantics(revision) do
          :ok
        end

      {"accept_plan", %{"proposal" => proposal, "proposal_hash" => proposal_hash}} ->
        with :ok <- validate_semantics(:plan_proposal, proposal),
             :ok <- validate_plan_proposal_hash(proposal, proposal_hash) do
          :ok
        end

      {"request_decision", %{"kind" => "plan", "proposal" => proposal}} ->
        validate_semantics(:plan_proposal, proposal)

      {"request_plan",
       %{
         "repository_resource_id" => repository_resource_id,
         "subject" => %{"resource_id" => subject_resource_id}
       }}
      when repository_resource_id == subject_resource_id ->
        :ok

      {"request_plan", _payload} ->
        {:error, :request_plan_subject_resource_mismatch}

      _ ->
        :ok
    end
  end

  defp validate_semantics(:admission, admission) do
    with :ok <- validate_admission_provider_scope_purpose(admission) do
      validate_provider_scope(admission["provider_scope"])
    end
  end

  defp validate_semantics(:context_snapshot, snapshot) do
    with :ok <- validate_context_snapshot_hash(snapshot),
         :ok <-
           snapshot
           |> Map.get("work_contract", %{})
           |> Map.get("change_target")
           |> validate_provider_change_target() do
      snapshot
      |> Map.get("work_contract", %{})
      |> Map.get("acceptance")
      |> validate_acceptance_predicate_ids()
    end
  end

  defp validate_semantics(:task_result, task_result),
    do: validate_task_result_semantics(task_result)

  defp validate_semantics(:evidence, evidence),
    do: validate_evidence_semantics(evidence)

  defp validate_semantics(:usage, usage),
    do: validate_usage_semantics(usage)

  defp validate_semantics(:decision, decision),
    do: validate_decision_semantics(decision)

  defp validate_semantics(_kind, _data), do: :ok

  defp validate_task_result_semantics(task_result) do
    with :ok <- validate_subject_hash(task_result),
         :ok <- validate_task_result_reason(task_result),
         :ok <- validate_task_result_blocker(task_result),
         :ok <- validate_task_result_proposal(task_result) do
      :ok
    end
  end

  defp validate_task_result_proposal(%{"kind" => "plan_proposed", "proposal" => proposal}),
    do: validate_semantics(:plan_proposal, proposal)

  defp validate_task_result_proposal(_task_result), do: :ok

  defp validate_task_result_reason(%{"kind" => "failed", "reason" => reason})
       when reason in @task_result_terminal_failure_reasons,
       do: :ok

  defp validate_task_result_reason(%{"kind" => "failed"}),
    do: {:error, :failed_result_requires_terminal_reason}

  defp validate_task_result_reason(%{"kind" => kind, "reason" => nil})
       when kind in [
              "progress",
              "candidate_completion",
              "blocked",
              "repair_required",
              "replan_required",
              "plan_proposed"
            ],
       do: :ok

  defp validate_task_result_reason(%{"kind" => kind, "reason" => reason}),
    do: {:error, {:unexpected_task_result_reason, kind, reason}}

  defp validate_task_result_reason(_task_result), do: :ok

  defp validate_provider_change_target(nil), do: :ok

  defp validate_provider_change_target(%{
         "kind" => "branches",
         "source_branch" => source_branch,
         "target_branch" => target_branch
       })
       when is_binary(source_branch) and is_binary(target_branch) do
    if source_branch == target_branch do
      {:error, :provider_change_target_branches_must_differ}
    else
      :ok
    end
  end

  defp validate_provider_change_target(_target), do: :ok

  defp validate_task_result_blocker(%{"kind" => "blocked", "blocker" => blocker})
       when is_map(blocker),
       do: :ok

  defp validate_task_result_blocker(%{"kind" => "blocked"}),
    do: {:error, :blocked_result_requires_blocker}

  defp validate_task_result_blocker(%{"kind" => kind, "blocker" => nil})
       when kind != "blocked",
       do: :ok

  defp validate_task_result_blocker(%{"kind" => kind}),
    do: {:error, {:unexpected_blocker, kind}}

  defp validate_task_result_blocker(_task_result), do: :ok

  defp validate_context_snapshot_hash(snapshot) do
    expected =
      "sha256:" <>
        (SymmetryControl.RequestHash.canonical(Map.delete(snapshot, "content_hash"))
         |> Base.encode16(case: :lower))

    if snapshot["content_hash"] == expected do
      :ok
    else
      {:error, {:content_hash_mismatch, expected}}
    end
  end

  defp validate_acceptance_predicate_ids(%{"predicates" => predicates})
       when is_list(predicates) do
    predicates
    |> Enum.reduce_while({:ok, MapSet.new()}, fn
      %{"id" => id}, {:ok, seen} when is_binary(id) ->
        if MapSet.member?(seen, id) do
          {:halt, {:error, {:duplicate_predicate_id, id}}}
        else
          {:cont, {:ok, MapSet.put(seen, id)}}
        end

      _predicate, _result ->
        {:halt, {:error, :invalid_acceptance_contract}}
    end)
    |> case do
      {:ok, _seen} -> :ok
      {:error, _reason} = error -> error
    end
  end

  defp validate_acceptance_predicate_ids(_acceptance), do: {:error, :invalid_acceptance_contract}

  defp validate_subject_hash(document) do
    expected = canonical_sha256(document["subject"])

    if document["subject_hash"] == expected do
      :ok
    else
      {:error, {:subject_hash_mismatch, expected}}
    end
  end

  defp validate_evidence_semantics(evidence) do
    source_ref = evidence["source_ref"]
    payload = evidence["payload"]

    with :ok <- validate_subject_hash(evidence),
         :ok <- ensure_equal(evidence["subject"], payload["subject"], :payload_subject),
         :ok <-
           ensure_equal(
             evidence["subject_hash"],
             source_ref["subject_hash"],
             :source_ref_subject_hash
           ),
         :ok <-
           ensure_equal(
             evidence["subject_hash"],
             payload["subject_hash"],
             :payload_subject_hash
           ),
         :ok <- ensure_equal(evidence["kind"], source_ref["kind"], :source_ref_kind),
         :ok <- validate_evidence_validator_profile(evidence),
         :ok <- validate_evidence_verdict(evidence),
         :ok <-
           validate_evidence_kind_identity(
             evidence["kind"],
             evidence["subject"],
             source_ref,
             payload
           ) do
      :ok
    end
  end

  defp validate_evidence_verdict(%{
         "kind" => "review",
         "verdict" => verdict,
         "payload" => payload
       }),
       do: ensure_equal(verdict, payload["verdict"], :review_verdict)

  defp validate_evidence_verdict(_evidence), do: :ok

  defp validate_evidence_validator_profile(%{
         "validator_profile" => validator_profile,
         "source_ref" => source_ref
       }) do
    case Map.fetch(source_ref, "validator_profile") do
      {:ok, source_profile} when source_profile != validator_profile ->
        {:error, {:evidence_identity_mismatch, :validator_profile}}

      _ ->
        :ok
    end
  end

  defp validate_evidence_kind_identity("artifact", subject, source_ref, payload) do
    with :ok <-
           ensure_equal(
             source_ref["resource_id"],
             payload["resource_id"],
             :artifact_resource_id
           ),
         :ok <- ensure_equal(source_ref["commit"], payload["commit"], :artifact_commit),
         :ok <- ensure_equal(source_ref["path"], payload["path"], :artifact_path),
         :ok <-
           ensure_equal(
             subject["resource_id"],
             payload["resource_id"],
             :artifact_subject_resource_id
           ),
         :ok <-
           ensure_equal(subject["commit"], payload["commit"], :artifact_subject_commit) do
      :ok
    end
  end

  defp validate_evidence_kind_identity("review", _subject, source_ref, payload),
    do: ensure_equal(source_ref["review_task_id"], payload["review_task_id"], :review_task_id)

  defp validate_evidence_kind_identity("observation", _subject, source_ref, payload),
    do: ensure_equal(source_ref["external_ref"], payload["external_ref"], :external_ref)

  defp validate_evidence_kind_identity(_kind, _subject, _source_ref, _payload), do: :ok

  defp validate_usage_semantics(%{"cost_basis" => cost_basis, "cost_microusd" => cost_microusd}) do
    case {cost_basis, cost_microusd} do
      {"unknown", nil} ->
        :ok

      {"unknown", _amount} ->
        {:error, {:usage_cost_basis_mismatch, cost_basis}}

      {basis, nil} when basis in ["reported", "estimated"] ->
        {:error, {:usage_cost_basis_mismatch, cost_basis}}

      _ ->
        :ok
    end
  end

  defp validate_usage_semantics(_usage), do: :ok

  defp validate_decision_semantics(decision) do
    with :ok <- validate_unique_ids(decision["options"], :duplicate_decision_option_id),
         :ok <- validate_decision_resolution(decision) do
      :ok
    end
  end

  defp validate_unique_ids(values, error_tag) do
    ids = Enum.map(values, & &1["id"])

    if length(ids) == MapSet.size(MapSet.new(ids)) do
      :ok
    else
      {:error, error_tag}
    end
  end

  defp validate_decision_resolution(decision) do
    state = decision["state"]
    resolution = decision["resolution"]
    options = decision["options"]

    case {state, resolution} do
      {state, nil} when state in ["open", "superseded"] ->
        :ok

      {"resolved", %{"option_id" => option_id}} ->
        if Enum.any?(options, &(&1["id"] == option_id)) do
          :ok
        else
          {:error, {:decision_resolution_unknown_option, option_id}}
        end

      {state, _resolution} when state in ["open", "superseded"] ->
        {:error, {:decision_resolution_shape, state}}

      {"resolved", nil} ->
        {:error, {:decision_resolution_shape, "resolved"}}

      _ ->
        :ok
    end
  end

  defp validate_goal_revision_semantics(%{
         "acceptance_contract" => %{"predicates" => predicates},
         "authority_policy" => authority_policy,
         "execution_policy" => execution_policy
       }) do
    case final_acceptance_authority(authority_policy, execution_policy, %{
           "predicates" => predicates
         }) do
      {:ok, _authority} -> :ok
      {:error, reason} -> {:error, reason}
    end
  end

  defp validate_goal_revision_semantics(_revision), do: :ok

  defp validate_admission_provider_scope_purpose(%{
         "purpose" => purpose,
         "provider_scope" => provider_scope
       })
       when purpose in ["validate", "plan", "observe", "chat"] and not is_nil(provider_scope),
       do: {:error, :provider_scope_not_allowed_for_purpose}

  defp validate_admission_provider_scope_purpose(_admission), do: :ok

  defp validate_provider_scope(nil), do: :ok

  defp validate_provider_scope(%{
         "resource_ids" => resource_ids,
         "operations_by_resource" => operations_by_resource,
         "change_target" => change_target
       })
       when is_list(resource_ids) and is_map(operations_by_resource) do
    resource_ids = MapSet.new(resource_ids)

    if MapSet.size(resource_ids) > 0 and
         MapSet.equal?(resource_ids, MapSet.new(Map.keys(operations_by_resource))) and
         Enum.all?(operations_by_resource, fn {_resource_id, operations} ->
           is_list(operations) and operations != [] and
             length(operations) == length(Enum.uniq(operations))
         end) do
      validate_provider_change_target(change_target)
    else
      {:error, :provider_scope_resource_operations_mismatch}
    end
  end

  defp validate_provider_scope(_scope), do: {:error, :provider_scope_invalid}

  defp validate_plan_proposal_hash(proposal, proposal_hash) when is_binary(proposal_hash) do
    expected =
      "sha256:" <>
        (proposal
         |> canonical_plan_proposal()
         |> SymmetryControl.RequestHash.canonical()
         |> Base.encode16(case: :lower))

    if proposal_hash == expected,
      do: :ok,
      else: {:error, :plan_proposal_hash_mismatch}
  end

  defp validate_plan_proposal_hash(_proposal, _proposal_hash),
    do: {:error, :plan_proposal_hash_mismatch}

  defp canonical_plan_proposal(%{"items" => items} = proposal) when is_list(items) do
    Map.put(
      proposal,
      "items",
      Enum.map(items, fn item ->
        item
        |> Map.put_new("integration", false)
        |> Map.put_new("change_target", nil)
      end)
    )
  end

  defp canonical_plan_proposal(proposal), do: proposal

  defp deterministic_predicate?(predicate) when is_map(predicate),
    do: value(predicate, "kind") in ["check", "artifact"]

  defp deterministic_predicate?(_predicate), do: false

  defp value(map, key, default \\ nil)

  defp value(map, "predicates", default),
    do: Map.get(map, "predicates", Map.get(map, :predicates, default))

  defp value(map, "final_acceptance", default),
    do: Map.get(map, "final_acceptance", Map.get(map, :final_acceptance, default))

  defp value(map, "operator_required_for_completion", default),
    do:
      Map.get(
        map,
        "operator_required_for_completion",
        Map.get(map, :operator_required_for_completion, default)
      )

  defp value(map, "kind", default), do: Map.get(map, "kind", Map.get(map, :kind, default))

  defp ensure_equal(left, right, _field) when left == right, do: :ok
  defp ensure_equal(_left, _right, field), do: {:error, {:evidence_identity_mismatch, field}}

  defp canonical_sha256(value) do
    "sha256:" <>
      (SymmetryControl.RequestHash.canonical(value)
       |> Base.encode16(case: :lower))
  end

  defp normalize_json_keys(value) when is_list(value) do
    Enum.reduce_while(value, {:ok, []}, fn item, {:ok, normalized} ->
      case normalize_json_keys(item) do
        {:ok, item} -> {:cont, {:ok, [item | normalized]}}
        {:error, _reason} = error -> {:halt, error}
      end
    end)
    |> case do
      {:ok, normalized} -> {:ok, Enum.reverse(normalized)}
      error -> error
    end
  end

  defp normalize_json_keys(value) when is_map(value) do
    Enum.reduce_while(value, {:ok, %{}}, fn {key, item}, {:ok, normalized} ->
      with {:ok, json_key} <- normalize_json_key(key),
           {:ok, normalized_item} <- normalize_json_keys(item) do
        if Map.has_key?(normalized, json_key) do
          {:halt, {:error, {:duplicate_json_key, json_key}}}
        else
          {:cont, {:ok, Map.put(normalized, json_key, normalized_item)}}
        end
      else
        {:error, _reason} = error -> {:halt, error}
      end
    end)
  end

  defp normalize_json_keys(value), do: {:ok, value}

  defp normalize_json_key(key) when is_binary(key), do: {:ok, key}
  defp normalize_json_key(key) when is_atom(key), do: {:ok, Atom.to_string(key)}
  defp normalize_json_key(key), do: {:error, {:invalid_json_key, key}}
end
