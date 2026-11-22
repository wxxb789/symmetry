defmodule SymmetryControlWeb.ProtocolReasonCoverageTest do
  use ExUnit.Case, async: true

  alias SymmetryControlWeb.Protocol

  @event [:symmetry_control_web, :protocol, :unmapped_reason]
  @lib_dir Path.expand("../../lib", __DIR__)
  @reason_pattern ~r/\{:error,\s*:([a-z][a-zA-Z0-9_]*)\}/

  @frozen_wire_table %{
    internal_error: {500, "internal_error", "internal server error"},
    invalid_request: {400, "invalid_request", "request payload is invalid"},
    unauthenticated: {401, "unauthenticated", "credential is missing or invalid"},
    forbidden: {403, "forbidden", "credential does not own this resource"},
    capability_not_granted:
      {403, "capability_not_granted", "connection capability is not granted"},
    provider_owned: {409, "provider_owned", "field is authoritative in the external provider"},
    not_found: {404, "not_found", "resource was not found"},
    requested_session_not_found:
      {404, "requested_session_not_found", "requested harness session was not found"},
    handoff_source_not_found:
      {404, "handoff_source_not_found", "handoff source Run was not found"},
    capacity_exhausted: {409, "capacity_exhausted", "runtime capacity is exhausted"},
    idempotency_conflict:
      {409, "idempotency_conflict", "idempotency key was reused with different input"},
    ownership_lost: {409, "ownership_lost", "execution lease is no longer authoritative"},
    stale: {409, "stale", "resource changed since it was loaded"},
    stale_revision: {409, "stale_revision", "goal revision is no longer current"},
    stale_run: {409, "stale_run", "run is no longer current for this Goal"},
    terminal_grace_expired:
      {409, "terminal_grace_expired", "terminal delivery grace period has expired"},
    state_conflict: {409, "state_conflict", "state has already advanced"},
    requested_session_unavailable:
      {409, "requested_session_unavailable", "requested harness session is unavailable"},
    handoff_source_consumed:
      {409, "handoff_source_consumed", "handoff source Run already has a successor admission"},
    handoff_session_reuse:
      {409, "handoff_session_reuse", "handoff must create a new native session"},
    invalid_decision: {409, "invalid_decision", "decision is no longer valid for this Goal"},
    decision_expired: {409, "decision_expired", "decision has expired"},
    goal_admission_disabled:
      {409, "goal_admission_disabled", "Goal admission is currently unavailable"},
    unsupported_control:
      {409, "unsupported_control", "runtime does not support supervisory control"},
    assignment_expired: {410, "assignment_expired", "assignment has expired"},
    invalid_cursor: {400, "invalid_cursor", "cursor is invalid"},
    invalid_transition: {422, "invalid_transition", "state transition is invalid"},
    invalid_contract: {422, "invalid_contract", "goal contract is invalid"},
    admitted_work_required: {422, "admitted_work_required", "Goal requires admitted work"},
    integration_outcome_required:
      {422, "integration_outcome_required", "Goal requires an accepted integration outcome"},
    context_budget_exceeded:
      {422, "context_budget_exceeded", "mandatory Goal context exceeds the configured budget"},
    required_context_source_missing:
      {422, "required_context_source_missing", "a required Goal context source is missing"},
    unsatisfied_predicate:
      {422, "unsatisfied_predicate", "goal command predicates are not satisfied"},
    unsupported_capability:
      {422, "unsupported_capability", "required capability is not supported"},
    dependency_cycle: {422, "dependency_cycle", "dependency would create a cycle"},
    waiting_dependency:
      {422, "waiting_dependency", "a required Goal dependency is not yet accepted"},
    budget_exhausted: {422, "budget_exhausted", "Goal budget is exhausted"},
    budget_unknown: {422, "budget_unknown", "Goal budget cannot be safely determined"},
    missing_evidence: {422, "missing_evidence", "required evidence is missing"},
    goal_authority_required:
      {403, "goal_authority_required", "Goal authority is required for this operation"},
    admission_limit_reached:
      {422, "admission_limit_reached", "Goal admission limit has been reached"},
    parallel_limit_reached:
      {422, "parallel_limit_reached", "Goal parallel execution limit has been reached"},
    active_task: {422, "active_task", "work item already has active Goal work"},
    incomplete_work: {422, "incomplete_work", "required Goal work is incomplete"},
    unresolved_decision: {422, "unresolved_decision", "Goal has unresolved decisions"},
    nonterminal_task: {422, "nonterminal_task", "Goal has nonterminal tasks"},
    strict_budget_requires_reservation:
      {422, "strict_budget_requires_reservation", "strict budget policy requires a reservation"},
    validation_required: {422, "validation_required", "independent validation is required"},
    invalid_validation: {422, "invalid_validation", "validation does not satisfy Goal policy"},
    invalid_outcome: {422, "invalid_outcome", "outcome does not match Goal ownership"},
    invalid_evidence_identity:
      {422, "invalid_evidence_identity", "evidence does not match the admitted Goal subject"},
    invalid_subject: {422, "invalid_subject", "repository subject is invalid for the Goal"},
    invalid_context_snapshot:
      {422, "invalid_context_snapshot", "context snapshot does not match its Goal identity"},
    requested_session_required:
      {422, "requested_session_required", "resume requires a requested harness session"},
    handoff_source_ineligible:
      {422, "handoff_source_ineligible",
       "handoff source Run is not a current settled progression"},
    handoff_source_stale:
      {422, "handoff_source_stale", "handoff source Run is not the current progression tip"},
    handoff_source_subject_mismatch:
      {422, "handoff_source_subject_mismatch", "handoff source subject is inconsistent"},
    invalid_plan: {422, "invalid_plan", "plan does not satisfy Goal contract"},
    resource_not_allowed: {422, "resource_not_allowed", "resource is not allowed by Goal policy"},
    model_not_allowed: {422, "model_not_allowed", "model profile is not allowed by Goal policy"},
    not_connected: {409, "not_connected", "resource has no external connection"},
    provider_unauthorized:
      {502, "provider_unauthorized", "external provider authentication failed"},
    provider_failure: {502, "provider_failure", "external provider request failed"},
    provider_access_unavailable:
      {503, "provider_access_unavailable", "required provider access is unavailable"}
  }

  @scanned_mapped [
    :budget_exhausted,
    :budget_unknown,
    :capability_not_granted,
    :forbidden,
    :goal_authority_required,
    :idempotency_conflict,
    :internal_error,
    :invalid_context_snapshot,
    :invalid_cursor,
    :invalid_request,
    :invalid_subject,
    :not_connected,
    :not_found,
    :ownership_lost,
    :provider_access_unavailable,
    :provider_failure,
    :provider_owned,
    :stale,
    :stale_revision,
    :stale_run,
    :state_conflict,
    :strict_budget_requires_reservation,
    :unauthenticated
  ]

  @scanned_unmapped [
    :already_accepted,
    :ambiguous_provider_objects,
    :attempt_limit,
    :authentication_unavailable,
    :blocked_result_requires_blocker,
    :ci_history_limit,
    :deterministic_acceptance_contract,
    :down,
    :duplicate_evidence_key,
    :duplicate_json_key,
    :duplicate_runtime_id,
    :evidence_batch_duplicate_id,
    :evidence_batch_response_run_id_mismatch,
    :evidence_batch_run_id_mismatch,
    :ex_json_schema_unavailable,
    :expected_json_object,
    :failed_result_requires_terminal_reason,
    :github_https_required,
    :github_oauth_required,
    :invalid_acceptance_contract,
    :invalid_auth_type,
    :invalid_commit_path,
    :invalid_datetime,
    :invalid_evidence_batch,
    :invalid_evidence_batch_item,
    :invalid_evidence_batch_run_id,
    :invalid_final_acceptance,
    :invalid_goal_control_descriptor,
    :invalid_options,
    :invalid_profile_digest,
    :invalid_profile_enabled,
    :invalid_profile_field,
    :invalid_profile_kind,
    :invalid_profile_name,
    :invalid_provider_response,
    :invalid_provider_success,
    :invalid_pull_request_url,
    :invalid_repository_reference,
    :invalid_runtime_id,
    :invalid_unstarted_settlement,
    :invalid_validation_profile,
    :invalid_validation_profile_registry,
    :malformed_json,
    :missing_ci_reference,
    :missing_commit_id,
    :missing_head_sha,
    :no_assignment,
    :not_captured,
    :outcome_derived,
    :plan_proposal_hash_mismatch,
    :provider_change_target_branches_must_differ,
    :provider_scope_invalid,
    :provider_scope_not_allowed_for_purpose,
    :provider_scope_resource_operations_mismatch,
    :request_plan_subject_resource_mismatch,
    :schema_root_must_be_absolute,
    :schema_root_required,
    :timeout,
    :transaction_required,
    :unauthorized,
    :unsupported_resource,
    :validation_pending,
    :validation_reconciliation_not_pending
  ]

  test "every closed {:error, :atom} reason in control/lib is pinned as mapped or known-unmapped" do
    assert scanned_reasons() ==
             (@scanned_mapped ++ @scanned_unmapped)
             |> Enum.map(&Atom.to_string/1)
             |> Enum.sort()
  end

  test "a scanned reason with a table entry returns exactly that entry" do
    for reason <- @scanned_mapped do
      assert Protocol.error_details(reason) == Map.fetch!(@frozen_wire_table, reason)
    end
  end

  test "a scanned reason without a table entry returns the invalid_request fallback" do
    for reason <- @scanned_unmapped do
      assert Protocol.error_details(reason) == @frozen_wire_table.invalid_request
    end
  end

  test "every pinned table entry is the entry Protocol serves" do
    for {reason, details} <- @frozen_wire_table do
      assert Protocol.error_details(reason) == details
    end
  end

  test "the served table has exactly the pinned keys" do
    assert served_table_keys() ==
             @frozen_wire_table |> Map.keys() |> Enum.map(&Atom.to_string/1) |> Enum.sort()
  end

  test "the tuple-shaped contract and stale clauses keep their entries" do
    attach_counter!(:tuple_clause_probe)

    assert Protocol.error_details({:invalid_contract, %{details: %{}}}) ==
             @frozen_wire_table.invalid_contract

    assert Protocol.error_details({:stale, %{current_version: 2}}) == @frozen_wire_table.stale

    refute_receive {:unmapped_reason, _, _, %{reason: {:invalid_contract, _}}}
    refute_receive {:unmapped_reason, _, _, %{reason: {:stale, _}}}
  end

  test "an unmapped reason emits the fallback counter once and keeps the response bytes" do
    probe_reason = :rf_w5_05_unmapped_probe
    attach_counter!(:unmapped_probe)

    assert Protocol.error_details(probe_reason) == @frozen_wire_table.invalid_request

    assert_unmapped_event!(probe_reason)
    refute_receive {:unmapped_reason, _, _, %{reason: ^probe_reason}}

    tuple_probe = {:rf_w5_05_unmapped_probe, %{}}

    assert Protocol.error_details(tuple_probe) == @frozen_wire_table.invalid_request
    assert_unmapped_event!(tuple_probe)
    refute_receive {:unmapped_reason, _, _, %{reason: ^tuple_probe}}

    response = Protocol.error(Plug.Test.conn(:get, "/"), probe_reason)
    assert response.status == 400

    assert_unmapped_event!(probe_reason)
    refute_receive {:unmapped_reason, _, _, %{reason: ^probe_reason}}

    assert response.resp_body ==
             ~s({"error":{"code":"invalid_request","message":"request payload is invalid"}})
  end

  test "a mapped reason does not emit the fallback counter" do
    attach_counter!(:mapped_probe)

    assert Protocol.error_details(:not_found) == @frozen_wire_table.not_found
    refute_receive {:unmapped_reason, _, _, %{reason: :not_found}}
  end

  test "a raising counter handler cannot change the response bytes" do
    probe_reason = :rf_w5_05_raising_handler_probe
    handler_id = {__MODULE__, :raising_probe, make_ref()}
    test_pid = self()

    :ok =
      :telemetry.attach(
        handler_id,
        @event,
        fn
          _event, _measurements, %{reason: ^probe_reason}, _config ->
            send(test_pid, :raising_handler_ran)
            raise "rf_w5_05 raising handler probe"

          _event, _measurements, _metadata, _config ->
            :ok
        end,
        nil
      )

    on_exit(fn -> :telemetry.detach(handler_id) end)

    {response, _log} =
      ExUnit.CaptureLog.with_log(fn ->
        Protocol.error(Plug.Test.conn(:get, "/"), probe_reason)
      end)

    assert_receive :raising_handler_ran
    assert response.status == 400

    assert response.resp_body ==
             ~s({"error":{"code":"invalid_request","message":"request payload is invalid"}})
  end

  # The reason is pinned in the pattern so an emission from a concurrently running
  # test module cannot satisfy the receive.
  defp assert_unmapped_event!(reason) do
    assert_receive {:unmapped_reason, event, measurements, %{reason: ^reason} = metadata}
    assert event == @event
    assert measurements == %{count: 1}
    assert metadata == %{reason: reason}
  end

  defp attach_counter!(tag) do
    handler_id = {__MODULE__, tag, make_ref()}
    test_pid = self()

    :ok =
      :telemetry.attach(
        handler_id,
        @event,
        fn event, measurements, metadata, _config ->
          send(test_pid, {:unmapped_reason, event, measurements, metadata})
        end,
        nil
      )

    on_exit(fn -> :telemetry.detach(handler_id) end)
  end

  defp served_table_keys do
    source = File.read!(Path.join(@lib_dir, "symmetry_control_web/protocol.ex"))
    [_, block] = Regex.run(~r/@error_statuses %\{(.*?)\n  \}/s, source)

    Regex.scan(~r/^    ([a-z][a-z0-9_]*):/m, block, capture: :all_but_first)
    |> List.flatten()
    |> Enum.sort()
  end

  defp scanned_reasons do
    @lib_dir
    |> Path.join("**/*.ex")
    |> Path.wildcard()
    |> Enum.flat_map(fn path ->
      @reason_pattern
      |> Regex.scan(File.read!(path), capture: :all_but_first)
      |> List.flatten()
    end)
    |> Enum.uniq()
    |> Enum.sort()
  end
end
