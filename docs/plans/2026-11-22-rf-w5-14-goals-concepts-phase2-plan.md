# rf-w5-14 goals.ex concepts phase 2 — plan and evidence

Contract: `goal-rf-w5-14-goals-concepts-phase2.md` (GDD state root), binding sections: the
`Pattern / Design (binding)` block, the two `Conductor decision (2026-11-21, arch-decision-2 …)`
sections (SUPERSEDES the kernel-module names of the base text), the `arch-decision-2 verification
amendments`, the `Conductor amendment (2026-11-22, from W4-07 / PR #233 review)` and the owner's
`Comment policy (2026-11-22 — binding)`.

Baseline: this worktree's HEAD `642928b3` (= `origin/main`, the reshaped W4-07 = PR #233 merge;
all dispatch prerequisites are merged). Base artifact under analysis:
`control/lib/symmetry_control/goals.ex`, 8,121 lines.

## Public / API shape

`SymmetryControl.Goals` keeps exactly its 31 public functions: names, arities, default arguments,
`@spec`s, `@doc`s and the `@moduledoc` are unchanged. Nine `Reads` bodies stay inline together with
the two private option validators they call (11 inline definitions); the other 22 are `defdelegate`
to the concept module that now owns the body (nine of those delegates already existed from W4-07;
thirteen are new).

Everything else becomes module-private inside its concept module, except the pairs the tracer shows
crossing a module boundary: those become `@doc false def` on the owning side and are called as
`Alias.fun(...)` from the caller (no `import` between concept modules, hence no compile-connected
cycle). The kernel modules keep their W4-07 shapes; `run_fence.ex` changes only the eight call sites
and four `alias` lines (three added, one deleted) recorded in the cross-edge table.

## Exact edge-case semantics the contract demands

* Nothing observable changes. Same messages, atoms, ordering, digest inputs, SQL, transaction
  boundaries, `Repo.rollback` short-circuits, fences and idempotency behaviour — proven by the
  per-function AST comparison below, not by inspection.
* `SymmetryControl.Goals` keeps working for the 31 public entries (delegates preserve defaults and
  specs), so every existing caller and controller keeps compiling and passing without edits.
* Test files may not be weakened: the split moves whole tests and helpers; the 147 base test titles
  remain, as a multiset, unchanged.
* Files stay under 1,500 lines, which is why the two sub-group splits above are declared.

## What moves

Twelve concept modules: `Goals.Settlement`, `Goals.ValidationOutcome`, `Goals.Recovery`,
`Goals.Reconciliation`, `Goals.Admission`, `Goals.Budget`, `Goals.Commands`, `Goals.GoalControl`,
`Goals.Decisions`, `Goals.PlanContracts`, `Goals.ContextAssembly`, `Goals.Handoff`.

Recorded deviations from the literal k2 module list, all inside the contract's own size/shape rules:

* `Goals.ValidationOutcome` is the contract-required split of k2 Settlement (validation-outcome
  derivation plus `reconcile_validation_outcome/4`).
* `Goals.Budget` takes the k2 Admission budget group and, as the contract allows,
  `reconcile_reservation!/3` (its only in-file callers are Budget accounting and RunFence, which now
  calls `Budget.reconcile_reservation!/3`).
* `Goals.Handoff` carries the k2 Admission `handoff` sub-group (name pattern `handoff|session|resume`, 9 functions, 281 base block lines): with the handoff group inline `admission.ex` would measure about 1619 lines against the 1,500-line cap (`wc -l` 1337 + 281 + 1).
* `Goals.GoalControl` carries the k2 Commands control family (13 functions whose names carry `control|pause|goal_control|descriptor|pending_goal_control`, 420 base block lines): with it inline `commands.ex` would measure about 1559 lines against the cap (`wc -l` 1138 + 420 + 1).

Kernel modules (W4-07) are consumed, not changed: `Goals.Internal`, `Goals.EventsReceipts`,
`Goals.RunFence`, `Goals.HarnessSessions`, `Goals.ContractValidation`.

## Re-derived membership at HEAD

Method: AST census (`dump_fns.exs` + `assemble.py`, token metadata with `:end`) over the base blob
gives 312 functions / 451 clauses; the k2 name lists (`refactor/scratch-k2/clusters.tsv`) map every
base name to its k2 cluster; the compile tracer (`trace_fns.exs`, `Code.put_compiler_option`
tracers over the test build) gives 7,315 call events (482 local, 3,244 remote, 1,689 imported,
1,900 imported-macro) with **0 unresolved
local callees**, which fixes the cross-module edge set below. The 17 kernel-only leftovers that k2
assigned to split modules but whose only callers stayed in the kernel were re-homed with their sole
caller cluster (list below the cross-edge table).

| destination | functions | base block lines | final `wc -l` |
| --- | --- | --- | --- |
| `Goals.Settlement` | 44 | 1160 | 1239 |
| `Goals.ValidationOutcome` | 13 | 521 | 565 |
| `Goals.Recovery` | 12 | 466 | 491 |
| `Goals.Reconciliation` | 57 | 1218 | 1290 |
| `Goals.Admission` | 48 | 1230 | 1337 |
| `Goals.Budget` | 11 | 301 | 339 |
| `Goals.Commands` | 30 | 1018 | 1138 |
| `Goals.GoalControl` | 13 | 420 | 411 |
| `Goals.Decisions` | 13 | 188 | 248 |
| `Goals.PlanContracts` | 21 | 300 | 339 |
| `Goals.ContextAssembly` | 21 | 664 | 699 |
| `Goals.Handoff` | 9 | 281 | 320 |
| `SymmetryControl.Goals` (facade) | 11 inline + 22 delegates | 115 | 332 |
| `Goals.RunFence` (W4-07 `defdelegate`s kept on the facade) | 9 | — (already in the kernel) | 1261 |
| **total** | **312** | **7922** | **8748** |

Cap check: every generated file is under 1,500 lines (largest `admission.ex` 1337 lines).

Re-homed kernel leftovers (moved with their sole caller cluster, as the contract's
`Comment policy` permits): `lock_settlement_tasks!/2`, `lock_settlement_runs!/3`, `validate_goal_contract/3`, `require_reason!/1`, `required_nonnegative_integer/2`, `required_safe_nonnegative_integer/2`, `required_safe_positive_integer!/2`, `required_digest!/2`, `optional_digest!/1,2`, `digest!/1`, `optional_string_strict!/2`, `valid_actor/1`, `optional_string/2`, `policy_nonnegative_integer/3`, `dependency_action_hash/4`, `dispatch_result/9`.

## Declaration set (every base function → destination, kind)

`def` = public entry, `@doc false def` = internal interface called from another module (or from
`run_fence.ex`), `defp` = module-private, `delegate` = W4-07 `defdelegate` kept verbatim.

| base function | base kind | destination | HEAD kind |
| --- | --- | --- | --- |
| `accept_outcome/3` | def | `Goals (facade, inline)` | def |
| `attention/1` | def | `Goals (facade, inline)` | def |
| `current_event_sequence/1` | def | `Goals (facade, inline)` | def |
| `fetch_context/2` | def | `Goals (facade, inline)` | def |
| `fetch_goal/1` | def | `Goals (facade, inline)` | def |
| `goal_managed_task?/1` | def | `Goals (facade, inline)` | def |
| `graph/1` | def | `Goals (facade, inline)` | def |
| `legacy_task_command_allowed?/1` | def | `Goals (facade, inline)` | def |
| `list_events/2` | def | `Goals (facade, inline)` | def |
| `valid_attention_options/1` | defp | `Goals (facade, inline)` | defp |
| `valid_event_options/1` | defp | `Goals (facade, inline)` | defp |
| `admission_allowed_runtime_ids!/1` | defp | `Goals.Admission` | defp/@doc false def |
| `admission_next_action/3` | defp | `Goals.Admission` | defp/@doc false def |
| `admission_provider_scope!/4` | defp | `Goals.Admission` | defp/@doc false def |
| `admission_subject!/5` | defp | `Goals.Admission` | defp/@doc false def |
| `admission_validation_bindings!/4` | defp | `Goals.Admission` | defp/@doc false def |
| `admit_authorized_task!/6` | defp | `Goals.Admission` | defp/@doc false def |
| `admit_plan_items!/3` | defp | `Goals.Admission` | defp/@doc false def |
| `admit_plan_task!/7` | defp | `Goals.Admission` | defp/@doc false def |
| `admit_task!/6` | defp | `Goals.Admission` | defp/@doc false def |
| `branch_change_target!/2` | defp | `Goals.Admission` | defp/@doc false def |
| `candidate_subject_from_result!/3` | defp | `Goals.Admission` | defp/@doc false def |
| `completion_relevant_items/4` | defp | `Goals.Admission` | defp/@doc false def |
| `constrain_validation_runtime_intersection!/2` | defp | `Goals.Admission` | defp/@doc false def |
| `creates_cycle?/3` | defp | `Goals.Admission` | defp/@doc false def |
| `dependency_closure/2` | defp | `Goals.Admission` | defp/@doc false def |
| `ensure_achievable!/2` | defp | `Goals.Admission` | defp/@doc false def |
| `ensure_admissible_item!/2` | defp | `Goals.Admission` | defp/@doc false def |
| `ensure_completion_subject_not_rejected!/2` | defp | `Goals.Admission` | defp/@doc false def |
| `ensure_dependencies_satisfied!/2` | defp | `Goals.Admission` | defp/@doc false def |
| `ensure_evidence_subject!/3` | defp | `Goals.Admission` | defp/@doc false def |
| `ensure_final_acceptance_authority!/4` | defp | `Goals.Admission` | defp/@doc false def |
| `ensure_goal_final_acceptance!/1` | defp | `Goals.Admission` | defp/@doc false def |
| `ensure_required_evidence!/3` | defp | `Goals.Admission` | defp/@doc false def |
| `ensure_subject_resource!/3` | defp | `Goals.Admission` | defp/@doc false def |
| `ensure_task_policy!/5` | defp | `Goals.Admission` | defp/@doc false def |
| `ensure_unaccepted_item!/2` | defp | `Goals.Admission` | defp/@doc false def |
| `goal_evidence_for!/2` | defp | `Goals.Admission` | defp/@doc false def |
| `goal_predicate_matches_evidence?/2` | defp | `Goals.Admission` | defp/@doc false def |
| `integration_outcome?/4` | defp | `Goals.Admission` | defp/@doc false def |
| `manual_admission_payload!/6` | defp | `Goals.Admission` | defp/@doc false def |
| `manual_admission_subject!/3` | defp | `Goals.Admission` | defp/@doc false def |
| `manual_external_observation_payload!/3` | defp | `Goals.Admission` | defp/@doc false def |
| `manual_predecessor_subject!/3` | defp | `Goals.Admission` | defp/@doc false def |
| `optional_string/2` | defp | `Goals.Admission` | defp/@doc false def |
| `plan_item_baseline!/2` | defp | `Goals.Admission` | defp/@doc false def |
| `plan_item_change_target!/3` | defp | `Goals.Admission` | defp/@doc false def |
| `plan_item_integration!/1` | defp | `Goals.Admission` | defp/@doc false def |
| `plan_membership_integration!/2` | defp | `Goals.Admission` | defp/@doc false def |
| `provider_operation_allowed?/2` | defp | `Goals.Admission` | defp/@doc false def |
| `provider_scope/3` | defp | `Goals.Admission` | defp/@doc false def |
| `provider_scope_connection!/2` | defp | `Goals.Admission` | defp/@doc false def |
| `provider_scope_resource!/2` | defp | `Goals.Admission` | defp/@doc false def |
| `pull_request_change_target!/4` | defp | `Goals.Admission` | defp/@doc false def |
| `reject_caller_derived_admission_fields!/1` | defp | `Goals.Admission` | defp/@doc false def |
| `require_provider_access/2` | defp | `Goals.Admission` | defp/@doc false def |
| `resolve_integration_item!/3` | defp | `Goals.Admission` | defp/@doc false def |
| `resource_allowed?/2` | defp | `Goals.Admission` | defp/@doc false def |
| `valid_pull_request_change_target?/3` | defp | `Goals.Admission` | defp/@doc false def |
| `admission_limits!/2` | defp | `Goals.Budget` | defp/@doc false def |
| `budget_status/3` | defp | `Goals.Budget` | defp/@doc false def |
| `ensure_budget!/3` | defp | `Goals.Budget` | defp/@doc false def |
| `ensure_resume_budget!/2` | defp | `Goals.Budget` | defp/@doc false def |
| `latest_usage_rows/1` | defp | `Goals.Budget` | defp/@doc false def |
| `multiply_microusd!/2` | defp | `Goals.Budget` | defp/@doc false def |
| `policy_nonnegative_integer/3` | defp | `Goals.Budget` | defp/@doc false def |
| `reconcile_reservation!/3` | def | `Goals.Budget` | def |
| `server_admission_budget!/3` | defp | `Goals.Budget` | defp/@doc false def |
| `server_reservation_amount/1` | defp | `Goals.Budget` | defp/@doc false def |
| `task_accounting/2` | defp | `Goals.Budget` | defp/@doc false def |
| `apply_command!/4` | defp | `Goals.Commands` | defp/@doc false def |
| `canonical_command_payload/2` | defp | `Goals.Commands` | defp/@doc false def |
| `canonical_plan_proposal/1` | defp | `Goals.Commands` | defp/@doc false def |
| `command/4` | def | `Goals.Commands` | def |
| `create_goal/4` | def | `Goals.Commands` | def |
| `create_new_goal/7` | defp | `Goals.Commands` | defp/@doc false def |
| `digest!/1` | defp | `Goals.Commands` | defp/@doc false def |
| `execute_command/5` | defp | `Goals.Commands` | defp/@doc false def |
| `goal_revision_wire/2` | defp | `Goals.Commands` | defp/@doc false def |
| `maybe_wake_command/2` | defp | `Goals.Commands` | defp/@doc false def |
| `normalized_policy/1` | defp | `Goals.Commands` | defp/@doc false def |
| `optional_digest!/1` | defp | `Goals.Commands` | defp/@doc false def |
| `optional_digest!/2` | defp | `Goals.Commands` | defp/@doc false def |
| `optional_string_strict!/2` | defp | `Goals.Commands` | defp/@doc false def |
| `parse_command/2` | defp | `Goals.Commands` | defp/@doc false def |
| `parse_create_envelope/1` | defp | `Goals.Commands` | defp/@doc false def |
| `parse_known_command_envelope/2` | defp | `Goals.Commands` | defp/@doc false def |
| `policy_microusd!/1` | defp | `Goals.Commands` | defp/@doc false def |
| `require_reason!/1` | defp | `Goals.Commands` | defp/@doc false def |
| `required_digest!/2` | defp | `Goals.Commands` | defp/@doc false def |
| `required_nonnegative_integer/2` | defp | `Goals.Commands` | defp/@doc false def |
| `required_safe_nonnegative_integer/2` | defp | `Goals.Commands` | defp/@doc false def |
| `required_safe_positive_integer!/2` | defp | `Goals.Commands` | defp/@doc false def |
| `revision_attrs/4` | defp | `Goals.Commands` | defp/@doc false def |
| `transition_goal!/3` | defp | `Goals.Commands` | defp/@doc false def |
| `valid_actor/1` | defp | `Goals.Commands` | defp/@doc false def |
| `valid_command_payload?/2` | defp | `Goals.Commands` | defp/@doc false def |
| `valid_initial_revision/2` | defp | `Goals.Commands` | defp/@doc false def |
| `validate_execution_policy!/1` | defp | `Goals.Commands` | defp/@doc false def |
| `validate_goal_contract/3` | defp | `Goals.Commands` | defp/@doc false def |
| `advisory_recall/4` | defp | `Goals.ContextAssembly` | defp/@doc false def |
| `canonical_authority_policy/1` | defp | `Goals.ContextAssembly` | defp/@doc false def |
| `canonical_context_manifest/1` | defp | `Goals.ContextAssembly` | defp/@doc false def |
| `context_content_hash/1` | defp | `Goals.ContextAssembly` | defp/@doc false def |
| `context_decisions/2` | defp | `Goals.ContextAssembly` | defp/@doc false def |
| `context_digest/1` | def | `Goals.ContextAssembly` | def |
| `context_evidence/4` | defp | `Goals.ContextAssembly` | defp/@doc false def |
| `context_failed_attempts/3` | defp | `Goals.ContextAssembly` | defp/@doc false def |
| `context_snapshot_document/11` | defp | `Goals.ContextAssembly` | defp/@doc false def |
| `context_sources!/8` | defp | `Goals.ContextAssembly` | defp/@doc false def |
| `context_wire_envelope/4` | def | `Goals.ContextAssembly` | def |
| `handoff_context_source/2` | defp | `Goals.ContextAssembly` | defp/@doc false def |
| `planning_context_snapshot_document/7` | defp | `Goals.ContextAssembly` | defp/@doc false def |
| `planning_context_sources!/8` | defp | `Goals.ContextAssembly` | defp/@doc false def |
| `safe_document?/1` | def | `Goals.ContextAssembly` | def |
| `safe_evidence_document/1` | def | `Goals.ContextAssembly` | def |
| `sanitize_machine_context/1` | defp | `Goals.ContextAssembly` | defp/@doc false def |
| `sensitive_document_key?/1` | defp | `Goals.ContextAssembly` | defp/@doc false def |
| `stabilize_context_size/2` | defp | `Goals.ContextAssembly` | defp/@doc false def |
| `stabilize_mandatory_context_size/4` | defp | `Goals.ContextAssembly` | defp/@doc false def |
| `stabilize_optional_context_size/5` | defp | `Goals.ContextAssembly` | defp/@doc false def |
| `decision_option?/2` | defp | `Goals.Decisions` | defp/@doc false def |
| `decision_request!/6` | defp | `Goals.Decisions` | defp/@doc false def |
| `decision_wire/1` | defp | `Goals.Decisions` | defp/@doc false def |
| `dependency_action_hash/4` | defp | `Goals.Decisions` | defp/@doc false def |
| `derived_decision_options/1` | defp | `Goals.Decisions` | defp/@doc false def |
| `ensure_decision_work_item!/2` | defp | `Goals.Decisions` | defp/@doc false def |
| `ensure_resolved_completion_decision!/3` | defp | `Goals.Decisions` | defp/@doc false def |
| `ensure_resolved_scope_decision!/5` | defp | `Goals.Decisions` | defp/@doc false def |
| `pending_plan_task?/2` | defp | `Goals.Decisions` | defp/@doc false def |
| `plan_decision_pending_acceptance?/1` | defp | `Goals.Decisions` | defp/@doc false def |
| `planning_acceptance_contract/0` | defp | `Goals.Decisions` | defp/@doc false def |
| `unresolved_decisions?/2` | defp | `Goals.Decisions` | defp/@doc false def |
| `validate_decision_changeset!/2` | defp | `Goals.Decisions` | defp/@doc false def |
| `apply_goal_pause_transition/5` | def | `Goals.GoalControl` | def |
| `control_descriptor_authorized?/6` | defp | `Goals.GoalControl` | defp/@doc false def |
| `control_dispatch_plan/4` | def | `Goals.GoalControl` | def |
| `dispatch_authorized_goal_control_command!/8` | defp | `Goals.GoalControl` | defp/@doc false def |
| `dispatch_goal_control_command/8` | def | `Goals.GoalControl` | def |
| `ensure_goal_pause_task_ownership!/4` | defp | `Goals.GoalControl` | defp/@doc false def |
| `goal_control_command_exists?/2` | defp | `Goals.GoalControl` | defp/@doc false def |
| `goal_control_idempotency_key/3` | defp | `Goals.GoalControl` | defp/@doc false def |
| `goal_pause_action_id!/2` | defp | `Goals.GoalControl` | defp/@doc false def |
| `goal_work_item_owned?/3` | defp | `Goals.GoalControl` | defp/@doc false def |
| `lock_current_goal_control_action/4` | defp | `Goals.GoalControl` | defp/@doc false def |
| `reject_pending_goal_control_commands!/2` | defp | `Goals.GoalControl` | defp/@doc false def |
| `valid_control_command_payload?/2` | defp | `Goals.GoalControl` | defp/@doc false def |
| `admission_session_mode!/2` | defp | `Goals.Handoff` | defp/@doc false def |
| `ensure_handoff_source_is_current_tip!/2` | defp | `Goals.Handoff` | defp/@doc false def |
| `ensure_plan_requested_session_available!/3` | defp | `Goals.Handoff` | defp/@doc false def |
| `ensure_requested_session_available!/3` | defp | `Goals.Handoff` | defp/@doc false def |
| `handoff_admission_subject!/6` | defp | `Goals.Handoff` | defp/@doc false def |
| `handoff_evidence_refs!/1` | defp | `Goals.Handoff` | defp/@doc false def |
| `handoff_source!/6` | defp | `Goals.Handoff` | defp/@doc false def |
| `handoff_source_settlement!/6` | defp | `Goals.Handoff` | defp/@doc false def |
| `native_resume_capable?/1` | defp | `Goals.Handoff` | defp/@doc false def |
| `acceptance_contract_matches_resource?/2` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `derived_integration_key/1` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `designation_triples/1` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `immutable_acceptance_contracts_valid?/2` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `immutable_plan_contracts_valid?/2` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `plan_admission_preflight?/2` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `plan_dependencies_acyclic?/1` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `plan_dependency_baseline_same_resource?/2` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `plan_dependency_cycle?/4` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `plan_integration_designated?/1` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `plan_item_dependencies_valid?/2` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `required_untrimmed_string!/2` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `untrimmed_string?/1` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `valid_acceptance_contract?/1` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `valid_commit_path?/1` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `valid_plan_baseline?/2` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `valid_plan_change_target?/1` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `valid_plan_envelope?/2` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `valid_plan_item?/1` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `valid_plan_proposal?/3` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `valid_predicate?/1` | defp | `Goals.PlanContracts` | defp/@doc false def |
| `accepted_work_item?/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `append_automatic_admission_blocker!/7` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `append_automatic_goal_blocker!/4` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `attempt_automatic_admission!/8` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `attempt_automatic_candidate/4` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_action_allowed?/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_admission_blocked_mutation_id/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_admission_blocker_reason/1` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_admission_candidates/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_admission_capacity_status/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_admission_disposition/1` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_admission_mutation_id/4` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_admission_payload/3` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_baseline_candidates/1` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_baseline_subject/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_budget_may_change?/1` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_budget_status/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_candidate_blocked?/4` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_candidate_identity/3` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_candidates/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_deferred_reason/1` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_dependencies_satisfied?/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_external_wait_candidates/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_fixed_blocked_item?/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_goal_blocked?/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_next_admission/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_pending_work?/1` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_policy_blocked_candidate/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_progress_candidates/1` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_progress_subject/4` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_reconciliation_after_capacity/3` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_reconciliation_deferred_mutation_id/3` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_reconciliation_next/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_recoverable_pending_work?/1` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_repair_candidates/1` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_repair_subject/3` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_source_consumed?/4` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_source_result_id/1` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_source_task_id/1` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_terminal_settlement_without_candidate?/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `automatic_validation_candidates/1` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `awaiting_validation_settlement?/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `baseline_source_present?/1` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `baseline_subject_identity/1` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `current_revision_task?/2` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `current_work_items/1` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `defer_automatic_candidate/5` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `defer_automatic_reconciliation!/5` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `reconcile_automatic_candidate/5` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `reconcile_automatic_goal!/3` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `reconcile_due_goal/3` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `reconcile_due_goal/5` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `reconcile_due_goal_once/4` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `reconcile_due_goal_rows/3` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `reconcile_due_goals/1` | def | `Goals.Reconciliation` | def |
| `record_automatic_admission_blocker/5` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `settled_as?/3` | defp | `Goals.Reconciliation` | defp/@doc false def |
| `cancellable_task_ids/1` | defp | `Goals.Recovery` | defp/@doc false def |
| `control_descriptors/3` | defp | `Goals.Recovery` | defp/@doc false def |
| `control_descriptors_for/3` | defp | `Goals.Recovery` | defp/@doc false def |
| `ensure_amendment_controls_settled!/1` | defp | `Goals.Recovery` | defp/@doc false def |
| `historical_goal_cancel_command?/3` | defp | `Goals.Recovery` | defp/@doc false def |
| `recover_pending_goal_control_action/1` | defp | `Goals.Recovery` | defp/@doc false def |
| `recover_pending_goal_control_actions/1` | def | `Goals.Recovery` | def |
| `recover_pending_terminal_settlements/1` | def | `Goals.Recovery` | def |
| `recover_pending_unstarted_cancellations/1` | def | `Goals.Recovery` | def |
| `recoverable_historical_unstarted_cancellation?/2` | defp | `Goals.Recovery` | defp/@doc false def |
| `recoverable_unstarted_cancellation_goal_ids/1` | def | `Goals.Recovery` | def |
| `safe_pause_task_ids/1` | defp | `Goals.Recovery` | defp/@doc false def |
| `append_evidence/5` | defdelegate | `Goals.RunFence (kernel, W4-07)` | delegate |
| `append_evidence_batch/5` | defdelegate | `Goals.RunFence (kernel, W4-07)` | delegate |
| `attach_harness_session/5` | defdelegate | `Goals.RunFence (kernel, W4-07)` | delegate |
| `claim/3` | defdelegate | `Goals.RunFence (kernel, W4-07)` | delegate |
| `claim_with_disposition/3` | defdelegate | `Goals.RunFence (kernel, W4-07)` | delegate |
| `fetch_harness_session_attachment/3` | defdelegate | `Goals.RunFence (kernel, W4-07)` | delegate |
| `fetch_run_context/3` | defdelegate | `Goals.RunFence (kernel, W4-07)` | delegate |
| `mark_harness_session_stopped/5` | defdelegate | `Goals.RunFence (kernel, W4-07)` | delegate |
| `record_usage/5` | defdelegate | `Goals.RunFence (kernel, W4-07)` | delegate |
| `create_external_wait!/8` | defp | `Goals.Settlement` | defp/@doc false def |
| `create_plan_decision!/5` | defp | `Goals.Settlement` | defp/@doc false def |
| `dispatch_result/9` | defp | `Goals.Settlement` | defp/@doc false def |
| `earliest_future_wake/3` | defp | `Goals.Settlement` | defp/@doc false def |
| `evidence_fingerprints/1` | defp | `Goals.Settlement` | defp/@doc false def |
| `existing_future_wake/2` | defp | `Goals.Settlement` | defp/@doc false def |
| `external_observation_admission_matches_wait?/4` | defp | `Goals.Settlement` | defp/@doc false def |
| `external_observation_terminal!/3` | defp | `Goals.Settlement` | defp/@doc false def |
| `external_wait_for_observation/3` | defp | `Goals.Settlement` | defp/@doc false def |
| `external_wait_identity/1` | defp | `Goals.Settlement` | defp/@doc false def |
| `external_wait_wake_at/2` | defp | `Goals.Settlement` | defp/@doc false def |
| `failed_terminal_reason/2` | defp | `Goals.Settlement` | defp/@doc false def |
| `finalize_external_wait!/8` | defp | `Goals.Settlement` | defp/@doc false def |
| `future_utc_datetime/2` | defp | `Goals.Settlement` | defp/@doc false def |
| `invalid_terminal_settlement/1` | defp | `Goals.Settlement` | defp/@doc false def |
| `item_id/1` | defp | `Goals.Settlement` | defp/@doc false def |
| `lock_settlement_runs!/3` | defp | `Goals.Settlement` | defp/@doc false def |
| `lock_settlement_tasks!/2` | defp | `Goals.Settlement` | defp/@doc false def |
| `plan_result_authorized?/2` | defp | `Goals.Settlement` | defp/@doc false def |
| `prepare_external_wait!/6` | defp | `Goals.Settlement` | defp/@doc false def |
| `process_terminal_settlement/2` | defp | `Goals.Settlement` | defp/@doc false def |
| `progress_has_new_verification?/6` | defp | `Goals.Settlement` | defp/@doc false def |
| `settle_task/4` | def | `Goals.Settlement` | def |
| `settle_unstarted_cancelled/3` | defp | `Goals.Settlement` | defp/@doc false def |
| `settle_unstarted_task/3` | def | `Goals.Settlement` | def |
| `settlement_task_owned_by_goal?/3` | def | `Goals.Settlement` | def |
| `task_result_failure_reason/2` | defp | `Goals.Settlement` | defp/@doc false def |
| `task_result_matches_task?/2` | defp | `Goals.Settlement` | defp/@doc false def |
| `terminal_error_reason/1` | defp | `Goals.Settlement` | defp/@doc false def |
| `terminal_next_wake_at/4` | defp | `Goals.Settlement` | defp/@doc false def |
| `terminal_payload_failure_reason/1` | defp | `Goals.Settlement` | defp/@doc false def |
| `terminal_plan_settlement!/4` | defp | `Goals.Settlement` | defp/@doc false def |
| `terminal_settlement!/7` | defp | `Goals.Settlement` | defp/@doc false def |
| `unstarted_settlement_mutation_id/2` | defp | `Goals.Settlement` | defp/@doc false def |
| `unsupported_external_wait_terminal/2` | defp | `Goals.Settlement` | defp/@doc false def |
| `validate_current_blocker/5` | defp | `Goals.Settlement` | defp/@doc false def |
| `validate_current_next_action/5` | defp | `Goals.Settlement` | defp/@doc false def |
| `validate_decision_blocker/5` | defp | `Goals.Settlement` | defp/@doc false def |
| `validate_dependency_blocker/4` | defp | `Goals.Settlement` | defp/@doc false def |
| `validate_external_blocker/3` | defp | `Goals.Settlement` | defp/@doc false def |
| `validate_proposed_next_action/5` | defp | `Goals.Settlement` | defp/@doc false def |
| `validate_result_blocker/5` | defp | `Goals.Settlement` | defp/@doc false def |
| `validation_outcome_reason/2` | defp | `Goals.Settlement` | defp/@doc false def |
| `verified_result_evidence_refs/4` | defp | `Goals.Settlement` | defp/@doc false def |
| `candidate_subject_from_terminal_result/2` | defp | `Goals.ValidationOutcome` | defp/@doc false def |
| `derive_validation_outcome!/10` | defp | `Goals.ValidationOutcome` | defp/@doc false def |
| `eligible_validation_outcome?/5` | defp | `Goals.ValidationOutcome` | defp/@doc false def |
| `existing_accepted_work_outcome/2` | defp | `Goals.ValidationOutcome` | defp/@doc false def |
| `existing_validation_outcome/1` | defp | `Goals.ValidationOutcome` | defp/@doc false def |
| `insert_validation_outcome!/13` | defp | `Goals.ValidationOutcome` | defp/@doc false def |
| `late_validation_outcome_mutation_id/3` | defp | `Goals.ValidationOutcome` | defp/@doc false def |
| `late_validation_outcome_receipt!/5` | defp | `Goals.ValidationOutcome` | defp/@doc false def |
| `outcome_matches_candidate?/6` | defp | `Goals.ValidationOutcome` | defp/@doc false def |
| `reconcile_validation_outcome/4` | def | `Goals.ValidationOutcome` | def |
| `resolved_work_item_acceptance_decision/3` | defp | `Goals.ValidationOutcome` | defp/@doc false def |
| `validation_evidence_outcome/6` | defp | `Goals.ValidationOutcome` | defp/@doc false def |
| `validation_subject_matches_candidate?/5` | defp | `Goals.ValidationOutcome` | defp/@doc false def |

Counts by HEAD kind: delegate 9, def 28, defp 2, defp/@doc false def 273.

## Cross-edge table (re-derived at HEAD)

Every crossing below becomes one `@doc false def` (or an already-public function) on the owning
side; the caller reaches it as a qualified `Alias.fun(...)` call. Edges to the kernel modules are
consumed through `import …, only:` with exactly the names the tracer shows each module calling.

| caller → callee | pairs | sites |
| --- | --- | --- |
| `Goals.Admission -> Goals.Budget` | 4 | 4 |
| `Goals.Admission -> Goals.ContextAssembly` | 4 | 4 |
| `Goals.Admission -> Goals.Decisions` | 2 | 2 |
| `Goals.Admission -> Goals.Handoff` | 6 | 6 |
| `Goals.Admission -> Goals.PlanContracts` | 6 | 7 |
| `Goals.Admission -> Goals.Reconciliation` | 3 | 3 |
| `Goals.Commands -> Goals.Admission` | 9 | 11 |
| `Goals.Commands -> Goals.Budget` | 1 | 1 |
| `Goals.Commands -> Goals.ContextAssembly` | 3 | 3 |
| `Goals.Commands -> Goals.Decisions` | 8 | 9 |
| `Goals.Commands -> Goals.GoalControl` | 1 | 1 |
| `Goals.Commands -> Goals.PlanContracts` | 3 | 3 |
| `Goals.Commands -> Goals.Recovery` | 1 | 1 |
| `Goals.ContextAssembly -> Goals.Decisions` | 1 | 1 |
| `Goals.Decisions -> Goals.Admission` | 1 | 1 |
| `Goals.Decisions -> Goals.ContextAssembly` | 1 | 2 |
| `Goals.Decisions -> Goals.PlanContracts` | 1 | 1 |
| `Goals.GoalControl -> Goals.Recovery` | 2 | 2 |
| `Goals.Handoff -> Goals.ContextAssembly` | 2 | 2 |
| `Goals.Reconciliation -> Goals.Admission` | 1 | 1 |
| `Goals.Reconciliation -> Goals.Budget` | 2 | 2 |
| `Goals.Reconciliation -> Goals.ContextAssembly` | 1 | 1 |
| `Goals.Reconciliation -> Goals.Settlement` | 2 | 2 |
| `Goals.Settlement -> Goals.Budget` | 1 | 1 |
| `Goals.Settlement -> Goals.Commands` | 1 | 1 |
| `Goals.Settlement -> Goals.ContextAssembly` | 4 | 7 |
| `Goals.Settlement -> Goals.Decisions` | 4 | 4 |
| `Goals.Settlement -> Goals.PlanContracts` | 1 | 1 |
| `Goals.Settlement -> Goals.Reconciliation` | 1 | 1 |
| `Goals.Settlement -> Goals.Recovery` | 1 | 1 |
| `Goals.Settlement -> Goals.ValidationOutcome` | 1 | 1 |
| `Goals.ValidationOutcome -> Goals.Admission` | 1 | 1 |
| `Goals.ValidationOutcome -> Goals.ContextAssembly` | 3 | 3 |
| `Goals.ValidationOutcome -> Goals.Settlement` | 5 | 5 |

Kernel imports (per caller module, from the tracer's `imported` events):

| caller module | kernel module | names |
| --- | --- | --- |
| `Goals (facade, inline)` | `Goals.Internal` | 6 (lock_context_snapshot!/2, lock_goal!/1, lock_work_item!/1, rollback/1, valid_uuid/1, valid_uuid?/1) |
| `Goals (facade, inline)` | `Goals.RunFence` | 1 (context_wire_envelope!/4) |
| `Goals.Admission` | `Goals.ContractValidation` | 1 (validate_contract/3) |
| `Goals.Admission` | `Goals.Internal` | 26 (active_task_for_item?/1, allowed_values/2, current_revision!/1, lock_repository_resources!/1, lock_task!/1, map_has_key?/2, now/1, only_known_keys?/2, optional_uuid!/2, parse_digest/1, parse_subject!/1, parse_subject/1, policy_integer/2, policy_integer/3, required_list!/2, required_map!/2, required_string!/2, required_uuid!/2, rollback/1, stamp_insert/2, stamp_update/2, unwrap!/1, valid_long_text?/1, valid_uuid?/1, value/2, value/3) |
| `Goals.Budget` | `Goals.Internal` | 6 (now/1, only_known_keys?/2, policy_integer/3, rollback/1, stamp_update/2, value/3) |
| `Goals.Commands` | `Goals.ContractValidation` | 1 (validate_contract/3) |
| `Goals.Commands` | `Goals.EventsReceipts` | 9 (append_event!/8, enqueue_goal_control!/2, ensure_preconditions!/2, receipt!/2, replay_command/3, replay_command_after_conflict/3, replay_create/2, replay_event/2, response_for/2) |
| `Goals.Commands` | `Goals.Internal` | 35 (active_task_for_item?/1, admission_enabled?/1, current_revision!/1, ensure_project_active!/1, goal_project_id!/1, lock_command_goal!/2, lock_decision!/1, lock_project!/1, lock_repository_resource!/1, lock_work_item!/1, lock_work_items!/1, normalize_map/1, now/1, nullable_microusd/2, only_known_keys?/2, optional_list/3, optional_uuid!/2, parse_digest/1, parse_subject!/1, plan_action_hash/2, required_list!/2, required_map!/2, required_map/2, required_safe_positive_integer/2, required_string!/2, required_string/2, required_uuid!/2, required_uuid/2, rollback/1, stamp_insert/2, stamp_update/2, unwrap!/1, valid_uuid/1, value/2, value/3) |
| `Goals.ContextAssembly` | `Goals.ContractValidation` | 1 (validate_contract/3) |
| `Goals.ContextAssembly` | `Goals.Internal` | 4 (normalize_map/1, rollback/1, value/2, value/3) |
| `Goals.Decisions` | `Goals.ContractValidation` | 1 (validate_contract/3) |
| `Goals.Decisions` | `Goals.EventsReceipts` | 1 (nullable_datetime/1) |
| `Goals.Decisions` | `Goals.Internal` | 8 (lock_decision!/1, lock_work_item!/1, plan_action_hash/2, required_string!/2, required_uuid!/2, rollback/1, value/2, work_item_acceptance_action_hash/3) |
| `Goals.GoalControl` | `Goals.Internal` | 10 (ensure_project_active!/1, lock_goal!/1, lock_project!/1, lock_run!/1, lock_task!/1, lock_work_item!/1, rollback/1, valid_uuid/1, valid_uuid?/1, value/2) |
| `Goals.Handoff` | `Goals.ContractValidation` | 1 (validate_contract/3) |
| `Goals.Handoff` | `Goals.Internal` | 10 (lock_run!/1, lock_task!/1, optional_uuid!/2, parse_subject!/1, required_uuid!/2, rollback/1, settlement_mutation_id/3, valid_uuid?/1, value/2, value/3) |
| `Goals.PlanContracts` | `Goals.ContractValidation` | 1 (validate_contract/3) |
| `Goals.PlanContracts` | `Goals.Internal` | 10 (is_short_identifier?/1, map_has_key?/2, only_known_keys?/2, parse_subject/1, required_safe_positive_integer/2, rollback/1, valid_long_text?/1, valid_uuid?/1, value/2, value/3) |
| `Goals.Reconciliation` | `Goals.ContractValidation` | 1 (validate_contract/3) |
| `Goals.Reconciliation` | `Goals.EventsReceipts` | 2 (append_event!/8, enqueue_wakeup!/2) |
| `Goals.Reconciliation` | `Goals.Internal` | 20 (active_task_for_item?/1, admission_enabled?/1, allowed_values/2, current_revision!/1, is_short_identifier?/1, lock_external_wait!/1, lock_goal!/1, lock_task!/1, lock_work_item!/1, mutation_id/2, now/1, parse_subject/1, policy_integer/2, policy_integer/3, required_uuid/2, stamp_update/2, try_lock_active_goal_project/1, valid_uuid?/1, value/2, value/3) |
| `Goals.Recovery` | `Goals.Internal` | 5 (lock_goal!/1, now/1, rollback/1, valid_uuid/1, valid_uuid?/1) |
| `Goals.Settlement` | `Goals.ContractValidation` | 1 (validate_contract/3) |
| `Goals.Settlement` | `Goals.EventsReceipts` | 3 (append_event!/8, nullable_datetime/1, replay_command/3) |
| `Goals.Settlement` | `Goals.HarnessSessions` | 1 (mark_harness_session_unavailable!/2) |
| `Goals.Settlement` | `Goals.Internal` | 19 (lock_goal!/1, lock_task!/1, lock_work_item!/1, mutation_id/2, normalize_map/1, now/1, only_known_keys?/2, parse_subject/1, plan_action_hash/2, plan_task?/1, required_string/2, required_uuid/2, rollback/1, settlement_mutation_id/3, stamp_insert/2, stamp_update/2, valid_uuid/1, valid_uuid?/1, value/2) |
| `Goals.ValidationOutcome` | `Goals.ContractValidation` | 1 (validate_contract/3) |
| `Goals.ValidationOutcome` | `Goals.EventsReceipts` | 3 (append_event!/8, nullable_datetime/1, replay_command/3) |
| `Goals.ValidationOutcome` | `Goals.Internal` | 13 (lock_goal!/1, lock_work_item!/1, mutation_id/2, now/1, parse_subject/1, required_uuid/2, rollback/1, settlement_mutation_id/3, stamp_insert/2, valid_uuid/1, value/2, value/3, work_item_acceptance_action_hash/3) |

`run_fence.ex` changes (the only kernel edit): the call lines below are repointed to the module
that now owns each function, and the three `alias` lines those calls need are added
(`alias SymmetryControl.Goals.Budget`, `.ContextAssembly`, `.Settlement`); the existing
`import SymmetryControl.Goals.ContractValidation` / `.EventsReceipts` / `.Internal` blocks and
the existing `alias SymmetryControl.Goals.HarnessSession` schema alias stay untouched, and
`alias SymmetryControl.Goals` is deleted because RunFence keeps no facade call.

| `run_fence.ex` line | function | was | is |
| --- | --- | --- | --- |
| 226, 303 | `safe_evidence_document/1` | `Goals.safe_evidence_document` | `ContextAssembly.safe_evidence_document` |
| 612 | `settlement_task_owned_by_goal?/3` | `Goals.settlement_task_owned_by_goal?` | `Settlement.settlement_task_owned_by_goal?` |
| 700 | `safe_document?/1` (twice in one guard) | `Goals.safe_document?` | `ContextAssembly.safe_document?` |
| 977 | `context_digest/1` | `Goals.context_digest` | `ContextAssembly.context_digest` |
| 1080 | `reconcile_reservation!/3` | `Goals.reconcile_reservation!` | `Budget.reconcile_reservation!` |
| 1125 | `context_wire_envelope/4` | `Goals.context_wire_envelope` | `ContextAssembly.context_wire_envelope` |

## Move proof: per-function AST comparison

`verify_ast.exs` compares every base function's AST (metadata stripped, `defp` folded to `def`, the
recorded cross-module call prefixes folded back to local calls) against the function that now
defines it:

| result | count |
| --- | --- |
| IDENTICAL | 282 |
| RECORDED_REWRITES | 21 |

`RECORDED_REWRITES` rows are functions whose bodies contain at least one cross-module call prefix, a
`defp` → `def` widening, or a call to a re-homed kernel leftover; each rewrite is name-only and
listed in the cross-edge table:

| function | rewritten call sites |
| --- | --- |
| `admit_plan_items!/3` | 4 |
| `apply_command!/4` | 24 |
| `attempt_automatic_admission!/8` | 1 |
| `automatic_budget_status/2` | 2 |
| `branch_change_target!/2` | 2 |
| `candidate_subject_from_terminal_result/2` | 1 |
| `control_descriptor_authorized?/6` | 1 |
| `create_plan_decision!/5` | 2 |
| `decision_request!/6` | 1 |
| `decision_wire/1` | 2 |
| `ensure_decision_work_item!/2` | 1 |
| `ensure_evidence_subject!/3` | 1 |
| `ensure_final_acceptance_authority!/4` | 1 |
| `ensure_unaccepted_item!/2` | 1 |
| `external_observation_admission_matches_wait?/4` | 1 |
| `manual_admission_subject!/3` | 1 |
| `plan_result_authorized?/2` | 2 |
| `planning_context_snapshot_document/7` | 1 |
| `pull_request_change_target!/4` | 1 |
| `revision_attrs/4` | 2 |
| `validation_subject_matches_candidate?/5` | 1 |
| **total** | **53** |

## String/atom byte-compare and control-flow audit

`string_compare.py` extracts, per function, the multiset of string literals and atoms
(`"[^"]*"` / `:[a-z_][a-z_0-9!?]*`) and the counts of `case`/`cond`/`with`/`if`/`unless`,
`rollback(`, `Repo.rollback(` and `Repo.transaction(` — compared base vs HEAD, per function; the
atom multiset carries every option key the moved code passes (including the `for_update` lock
options and the Oban queue/worker atoms), so lock call sites and ordering stay inside the byte
comparison:

| functions compared | identical | differing |
| --- | --- | --- |
| 312 | 312 | 0 |

Differing rows (none: only the facade's option validators and the delegates are new code):

* none — every base function's strings, atoms and control-flow counts are byte-identical
  (the AST identity proof above is the stronger statement of the same fact).

## Test split

Assignment rule (recorded): a test belongs to the module owning the majority of its `Goals.<fn>`
calls, ties to the module of the first call in source order; tests with no `Goals.` call stay in
`goals_test.exs`; the Commands group is size-split; a helper moves into a test file only when every
caller of it lives in that file, otherwise it becomes a public helper of
`goals/goals_test_support.exs`; whole tests are then moved by a convergent size-balancing loop
(largest test of the largest file to the smallest file that also exercises it, never above the cap).

| file | `wc -l` | tests | local helpers |
| --- | --- | --- | --- |
| `goals_test.exs` | 1415 | 29 | 9 |
| `goals/settlement_test.exs` | 1347 | 23 | 1 |
| `goals/reconciliation_test.exs` | 934 | 13 | 1 |
| `goals/recovery_test.exs` | 527 | 5 | 0 |
| `goals/commands_test.exs` | 1426 | 18 | 3 |
| `goals/commands_flow_test.exs` | 1372 | 21 | 6 |
| `goals/run_fence_flow_test.exs` | 1399 | 29 | 7 |
| `goals/harness_session_flow_test.exs` | 528 | 9 | 1 |
| `goals/goals_test_support.exs` | 977 | 0 | 44 |

`wc -l` and the local-helper counts are re-measured at the apply stage: the apply edits shrink the
moved bodies, move one settlement helper into `goals_test_support.exs` and leave the test counts
unchanged (the four new cases live in interface files, which this table does not list).

Titles: 147 base titles vs 147 HEAD titles, identical as a multiset (`titles_base.txt` vs
`titles_head2.txt`); re-derived at the apply stage (`titles_dup.py`, base `642928b3` vs the final
tree: the 8 split files carry exactly the base `goals_test.exs` 147 titles), plus the new interface
cases below: 75 at `dc070002`, 4 more at the apply stage, 2 more at the fix stage (both sets listed
in their sections), 81 at the final tree. Repo-wide (`control/test`), every one of the base revision's 1022 titles is
present in the final tree's 1103 (0 lost, +81); the one duplicated title in the tree
(`short-circuits a work item without a pull request before authenticating`) is already duplicated
at the base revision (`integrations/azure_devops_test.exs` = `integrations/github_test.exs`).
Size-balancing moves: 8 (recorded in `split/split_moves.tsv`).

| test moved | from | to |
| --- | --- | --- |

## Interface cases and red mutants

Every concept module has a focused interface file whose cases call the module directly (not through
the facade). One case per public or `@doc false` entry that has a discriminating observable:

* `goals/admission_interface_test.exs` — 11 cases
* `goals/budget_interface_test.exs` — 6 cases
* `goals/commands_interface_test.exs` — 4 cases
* `goals/context_assembly_interface_test.exs` — 8 cases
* `goals/decisions_interface_test.exs` — 10 cases
* `goals/goal_control_interface_test.exs` — 4 cases
* `goals/handoff_interface_test.exs` — 5 cases
* `goals/plan_contracts_interface_test.exs` — 7 cases
* `goals/reconciliation_interface_test.exs` — 5 cases
* `goals/recovery_interface_test.exs` — 7 cases
* `goals/settlement_interface_test.exs` — 9 cases
* `goals/validation_outcome_interface_test.exs` — 5 cases

Red mutants: each interface case is killed by a source mutation of its subject entry
(`gen_mutants.py` builds the manifest, `mutant.py` applies one row at a time, runs the single case
with `mix test file:line` and restores the file; a row whose case still passes is reported as
SURVIVED). Both the mutant manifest and the raw kill table are the evidence for this clause.

Interface-entry coverage audit: the required set is 63 `@doc false` entries — the 61 `widened`
entries of the cross-edge set plus the two entries only `run_fence.ex` calls,
`ContextAssembly.safe_document?/1` and `safe_evidence_document/1` — plus the 13 facade delegates
that land in the concept modules = 76 required entries, and all 76 have a case (the last widened
one, `Admission.goal_predicate_matches_evidence?/2` at `validation_outcome.ex:364`, was added after
the first audit pass; the two RunFence-only entries were added at the fix stage). The 77th case
exercises `Admission.admit_task!/6`, a private step reachable only through
`admit_authorized_task!/6`, which is itself covered. The apply stage adds four more cases (79 in
total): the `:missing` and settled-without-`awaiting_validation` refusals of
`reconcile_validation_outcome/4`, its late-receipt/`WorkOutcome` fail-closed branch, and the frozen
`RequestHash.canonical`/plan-action byte vector in `commands_interface_test.exs`; the fix stage
adds the two RunFence-only cases (81 in total). The red-mutant ledger below therefore executes
92 rows: the 75 `dc070002` rows re-anchored to the final tree, plus 15 rows for the cases the
apply stage strengthened (11) or added (4), plus the 2 fix-stage rows.

Static audit of the 12 interface files: each aliases its module and calls it directly
(4–25 qualified call sites per file); the facade is used only to build fixtures/state. No case has
zero assertions and none asserts a bare shape (`assert true`, `assert is_map(…)`, `assert _ = …`):
that is the vacuity screen that runs before the kill table below.

## Author-designed mutant per interface case (ledger)

Each interface case was written together with a source mutation of its subject entry that flips the
case (`case title :: mutation`); the contracts cluster below is *executed* by the harness
(`mutants.json`, mode `replace`, 26 rows; the fix stage adds 2 more `replace` rows in
`mutants_fix_r1.json` for the two new `ContextAssembly` cases), the remaining files' rows are
executed as call-site failure mutants (`raise` at the subject's first clause) and their designed
semantic mutation is recorded here for the reviewer. A case whose mutation list is empty would be
a defect.

Commands / Decisions / PlanContracts / ContextAssembly (`replace`, executed):

* `create_goal/4 persists one draft goal, revision and goal_created event behind a created receipt` :: `Commands.create_goal/4`: inserted goal attrs `state: "draft"` → `state: "active"`.
* `command/4 returns the created request_decision receipt and replays it exactly` :: the replayed transaction result `{:ok, receipt, :replayed}` → `:created`.
* `canonical_plan_proposal/1 fills missing integration and change_target defaults per item` :: `Map.put_new("integration", false)` → `true`.
* `decision_request!/6 hashes the plan proposal pre-image and asks the plan question` :: `RequestHash.canonical(proposal)` → `…canonical(value(proposal, :items))`.
* `derived_decision_options/1 derives the accept and reject options with their consequences` :: reject consequence `"Keep the current approved state."` → `"Keep the current state."`.
* `unresolved_decisions?/2 is true only while a decision at the revision stays open` :: `decision.state == "open"` → `in ["open", "resolved"]`.
* `pending_plan_task?/2 tracks an unsettled plan task at the current revision` :: `task.purpose == "plan"` → `"implement"`.
* `plan_decision_pending_acceptance?/1 tracks an open or accepted plan decision` :: `decision.kind == "plan"` → `"scope"`.
* `planning_acceptance_contract/0 is the operator-acceptance contract for proposed plans` :: predicate kind `"operator_acceptance"` → `"check"`.
* `ensure_resolved_scope_decision!/5 admits the accepted dependency decision` :: `decision.kind == "scope"` → `"review"`.
* `ensure_resolved_completion_decision!/3 admits the accepted completion subject hash only` :: `== "accept"` → `== "reject"`.
* `decision_option?/2 matches an option id inside the decision options` :: `value(&1, :id) == option_id` → `!=`.
* `validate_decision_changeset!/2 accepts a compatible wire and aborts an invalid one` :: contract arm `{:error, _reason} -> rollback(:invalid_request)` → `:ok`.
* `valid_plan_proposal?/3 accepts an envelope bound to the goal revision` :: `schema_version == "symmetry.plan.v1"` → `"symmetry.plan.v2"`.
* `plan_admission_preflight?/2 requires an integration designation` :: `plan_integration_designated?(items)` → `true`.
* `valid_acceptance_contract?/1 refuses duplicate predicate ids and unknown keys` :: duplicate check → `true`.
* `immutable_acceptance_contracts_valid?/2 resolves every contract against the profile registry` :: `{:ok, _bindings}` → `{:error, _}`.
* `acceptance_contract_matches_resource?/2 binds artifact predicates to one resource` :: `value(predicate, :resource_id) == resource_id` → `!=`.
* `derived_integration_key/1 derives only the single required item key` :: `{:derived, ref} -> ref` → `{:derived, _ref} -> nil`.
* `required_untrimmed_string!/2 returns an unchanged non-empty string or aborts` :: `value == String.trim(value) and value != ""` → `value != ""`.
* `context_digest/1 renders a raw sha256 digest and passes through a wire digest` :: `case: :lower` → `case: :upper`.
* `canonical_authority_policy/1 fills the operator and publication defaults` :: `Map.put_new("publication_allowed", false)` → `true`.
* `canonical_context_manifest/1 fills the byte budget and repository source defaults` :: `Map.put_new("byte_budget", 32_768)` → `65_536`.
* `context_wire_envelope/4 returns the persisted snapshot document only while it still verifies` :: `document["snapshot_id"] == snapshot.id` → `snapshot.goal_id`.
* `planning_context_snapshot_document/7 pins the planning contract, sources and content hash` :: work contract `"purpose" => "plan"` → `"implement"`.
* `context_snapshot_document/11 pins the implement work contract, sources and content hash` :: `"source_revision" => "work-item:#{item.id}"` → `"goal:…"`.
* `safe_document?/1 rejects sensitive keys at any depth and non-string map keys` :: the map clause `not sensitive_document_key?(key) and safe_document?(nested)` → `not sensitive_document_key?(key) and true`.
* `safe_evidence_document/1 accepts a clean evidence document and refuses a sensitive one` :: `else: {:error, :invalid_request}` → `else: :ok`.

Settlement / ValidationOutcome / Recovery / Reconciliation (recorded, executed as call-site mutants):

* `settle_task/4 settles a completed candidate Run as awaiting validation` :: response `"settlement" => terminal.settlement` → `terminal.result_kind`.
* `settle_unstarted_task/3 settles a committed runless cancellation once` :: `"settlement" => "released_without_run"` → `"released"`.
* `terminal_next_wake_at/4 keeps the earliest future wake` :: `DateTime.compare(existing, requested) == :lt` → `:gt`.
* `terminal_settlement!/7 reports a failed Run's semantic reason` :: failed arm → `process_terminal_settlement("cancelled", …)`.
* `settlement_task_owned_by_goal?/3 requires goal, item and task to agree` :: drop `and item.goal_id == goal.id`.
* `external_wait_identity/1 freezes the wait, task, run, result and check sequence` :: swap `wait.run_id` / `wait.result_id`.
* `external_wait_wake_at/2 schedules no wake` :: return `current` instead of `nil`.
* `lock_settlement_tasks!/2 locks the validation task and its producer` :: return `{task, nil}`.
* `lock_settlement_runs!/3 locks the run and its producing run` :: invert the ownership guard.
* `reconcile_validation_outcome/4 settles a resolved operator rejection as rejected` :: operator-reject arm → `:awaiting_validation`.
* `derive_validation_outcome!/10 inserts the accepted outcome bound to the candidate run` :: disposition `"accepted"` → `"rejected"`.
* `recover_pending_unstarted_cancellations/1 returns held cancelled tasks before settlement` :: `task.state == "cancelled"` → `"queued"`.
* `recoverable_unstarted_cancellation_goal_ids/1 lists goals with held runless cancellations` :: reservation filter `"held"` → `"settled"`.
* `recover_pending_terminal_settlements/1 respects the lease-expiry grace boundary` :: grace fragment `<=` → `<`.
* `recover_pending_goal_control_actions/1 returns the latest pending control action` :: `goal.state == "cancelled"` → `"active"`.
* `control_descriptors/3 maps cancellable tasks to cancel descriptors` :: `cancellable_task_ids(goal.id)` → `safe_pause_task_ids(goal.id)`.
* `ensure_amendment_controls_settled!/1 rolls back while stale amendment work remains` :: `rollback(:state_conflict)` → `:ok`.
* `recoverable_historical_unstarted_cancellation?/2 requires an applied runless cancel` :: `command.kind == "cancel"` → `"pause"`.
* `reconcile_due_goals/1 reconciles only the requested goal ids` :: id validity check → `true`.
* `accepted_work_item?/2 recognizes only current-revision accepted outcomes` :: disposition `"accepted"` → `"rejected"`.
* `automatic_admission_mutation_id/4 freezes goal, revision, item, purpose and source identity` :: swap `item.id` / `purpose`.
* `automatic_baseline_subject/2 returns the admitted baseline subject and its frozen identity` :: prefix `"baseline:subject:"` → `"baseline:"`.
* `automatic_progress_subject/4 binds the settled progress run identity` :: swap `run.id` / `result_id`.

Admission / Budget / Handoff / GoalControl (recorded, executed as call-site mutants):

* `goal_predicate_matches_evidence?/2 matches evidence rows against the predicate it belongs to` :: `Admission.goal_predicate_matches_evidence?/2`: check arm `evidence.kind == "check"` → `evidence.kind == "review"`.
* `admit_plan_items!/3 pins the admitted revision, membership and baseline` :: `admitted_revision: goal.current_revision` → `+ 1`.
* `admit_task!/6 records the queued task, snapshot, reservation and item binding` :: Task changeset `state: "queued"` → `"paused"`.
* `admit_plan_task!/7 creates the runless planning task with its own snapshot` :: Task changeset `purpose: "plan"` → `"implement"`.
* `admit_authorized_task!/6 rechecks item admissibility before one admission` :: delete the `ensure_admissible_item!(goal, item)` call.
* `ensure_admissible_item!/2 rejects an item admitted under another revision` :: `unless … and …` → `or`.
* `ensure_unaccepted_item!/2 rejects an item with an accepted outcome` :: accepted-work-item condition → `false`.
* `ensure_achievable!/2 binds the accepted integration outcome to the frozen subject` :: `subject_hash = RequestHash.canonical(subject)` → `context_digest(…)`.
* `creates_cycle?/3 detects only the reversing edge in an admitted DAG` :: swap `depends_on_id` / `work_item_id`.
* `manual_admission_payload!/6 binds the command mutation and the plan baseline subject` :: `Map.put(:subject, manual_admission_subject!(…))` → `nil`.
* `resource_allowed?/2 treats an empty allow-list as all resources` :: `[] -> true` → `false`.
* `budget_status/3 counts a held strict reservation against the total limit` :: `>` → `>=`.
* `admission_limits!/2 defaults one turn to the bounded fifteen minute window` :: `15 * 60` → `16 * 60`.
* `server_reservation_amount/1 reserves every strict attempt and leaves soft mode unreserved` :: `"soft"` arm `{:ok, nil}` → `{:ok, 0}`.
* `server_admission_budget!/3 attaches cost limits only to the strict revision` :: soft arm `nil` → `"0"`.
* `ensure_resume_budget!/2 checks only the strict revision and returns :ok` :: `== "strict"` → `!= "strict"`.
* `reconcile_reservation!/3 releases a terminal task reservation exactly once` :: `settle_changeset(accounting.reservation_state)` → `settle_changeset("settled")`.
* `handoff_source!/6 returns the settled source identity of a completed candidate` :: `{"candidate_completion", "awaiting_validation"}` → `{…, "progress"}`.
* `handoff_admission_subject!/6 binds a validation handoff to the source candidate` :: `source.result_kind == "candidate_completion"` → `"progress"`.
* `admission_session_mode!/2 derives the mode from the requested session` :: default → constant `"resume"`.
* `ensure_requested_session_available!/3 binds resume to a verified available session` :: `session.repository_resource_id == item…` → `!=`.
* `ensure_plan_requested_session_available!/3 locks the plan resource session and runtime` :: `runtime.repository_resource_id == resource.id` → `!=`.
* `control_dispatch_plan/4 enumerates the exact descriptor for the current cancellation` :: `Recovery.control_descriptors(goal, event.kind, action_id)` → `(goal, "pause", action_id)`.
* `dispatch_goal_control_command/8 creates one runless cancellation and replays it exactly` :: delete the idempotency-key guard.
* `apply_goal_pause_transition/5 pauses the run from its pending pause command` :: `command.kind != "pause"` → `!= "cancel"`.
* `reject_pending_goal_control_commands!/2 acknowledges only goal control commands` :: delete the `like(command.idempotency_key, "goal-control:%")` condition.

## Credo `--strict` delta (base `642928b3` vs HEAD)

Both runs are `mix credo --strict` (exit 30 = issues present; the count is the gate). Base **500**
(5 warnings / 357 refactoring / 82 readability / 56 design); HEAD **499** (5 / 357 / 80 / 57) — the
count does not rise, and no new issue kind
appears: comparing the issue *messages* with the file part dropped, the only lines that differ are
`Duplicate code found in …` pairs, whose location anchors are re-pointed by the file split (10
anchors re-pointed, 11 old anchors gone, net −1 on a 500-issue base); the readable checks do not
rise at all (`[R] Line is too long` 82 → 80, `[R] The alias …` −1). Per file, only the moved
code's pre-existing complexity/nesting kinds change attribution:

| file | base | HEAD | delta |
| --- | --- | --- | --- |
| `lib/symmetry_control/goals.ex` | 109 | 2 | −107 |
| `lib/symmetry_control/goals/admission.ex` | 0 | 19 | +19 |
| `lib/symmetry_control/goals/reconciliation.ex` | 0 | 18 | +18 |
| `lib/symmetry_control/goals/settlement.ex` | 0 | 13 | +13 |
| `lib/symmetry_control/goals/validation_outcome.ex` | 0 | 13 | +13 |
| `lib/symmetry_control/goals/commands.ex` | 0 | 11 | +11 |
| `lib/symmetry_control/goals/goal_control.ex` | 0 | 11 | +11 |
| `lib/symmetry_control/goals/recovery.ex` | 0 | 6 | +6 |
| `lib/symmetry_control/goals/plan_contracts.ex` | 0 | 5 | +5 |
| `lib/symmetry_control/goals/handoff.ex` | 0 | 4 | +4 |
| `lib/symmetry_control/goals/budget.ex` | 0 | 3 | +3 |
| `lib/symmetry_control/goals/context_assembly.ex` | 0 | 2 | +2 |
| `lib/symmetry_control/goals/decisions.ex` | 0 | 1 | +1 |
| `test/symmetry_control/goals_test.exs` | 7 | 2 | −5 |
| `test/symmetry_control/goals/commands_test.exs` | 0 | 2 | +2 |
| `test/symmetry_control/goals/goals_test_support.exs` | 0 | 1 | +1 |
| `test/symmetry_control/goals/harness_session_flow_test.exs` | 0 | 1 | +1 |

Two issues were genuinely new at the first HEAD measurement (`run_fence.ex` 100-character line
from the repointed guard; two 99/100-character lines in `settlement_interface_test.exs`) and are
fixed by reflowing those expressions to the formatter's own canonical layout; the per-function AST
comparison was re-run after the fix (`282` identical + `21` recorded rewrites, 0 missing, 0 changed).

Apply-stage re-run: 499 again (5 / 357 / 80 / 57). The first apply-stage run measured 501 because
the F29-3 dedup had inlined the same `Keyword.get_lazy(..., fn -> Repo.one(...) end)` block that
`orchestration_goal_admission_test.exs:3808` already carries, which credo reported as two
`Duplicate code found` issues; extracting the query to `defp runtime_repository_resource_id/0` in
`goals_test_support.exs` restored the count, so the touched files still add no issue kind and the
count does not rise.

## Evidence plan

| claim | check | artifact |
| --- | --- | --- |
| facade API unchanged | `mix compile --warnings-as-errors`; facade diff shows only bodies → `defdelegate`; targeted controller suites | logs below |
| no function lost or duplicated | declaration table (312 base functions) + AST census counts | this table, `verify_ast.exs` |
| behaviour unchanged | per-function AST identity (282 IDENTICAL, 21 RECORDED_REWRITES) + string/atom/CF compare | `ast_compare.tsv`, `string_cf.tsv` |
| no compile-connected cycles | `mix xref graph --format cycles --label compile-connected` | log below |
| runtime xref budget | `mix xref graph --format cycles` (plain): 3 cycles (25 web / 20 runtime / 4 schemas) | log below |
| tests not weakened | 147 titles as a multiset, unchanged; `git diff --check`; suite counts | logs below |
| files under 1,500 lines | `wc -l` table above | this document |
| interface tests exercise each module | interface case list + red-mutant kill table | this document, manifest |
| coverage not reduced | `mix test --cover` on base worktree and HEAD, per touched file | log below |
| R9 native-evidence check still passes | `test-omo-control-native-evidence.py` (SOURCE_PATHS updated) | log below |
| citation check | `docs/TODO.md`/plan references resolve | log below |
| CI-pinned tests unchanged | `grep` of the two pins named by the contract: `goal_machine_controller_test.exs` 'committed harness session receipts recover through real Go HTTP after listener restarts' present at line 764 in an untouched file; `pi_control_e2e_test.exs` tags/titles intact at `ci.yml:382-396,870` | plan row |

## Design trade-offs and out of scope

* Cross-module calls use qualified `Alias.fun(...)` rather than `import`s: the contract's second
  decision makes imports count as connected edges, so no concept module imports another. The cost is
  a longer call site; the benefit is that `--label compile-connected` stays empty.
* `defp` functions called from another module are widened to `@doc false def` (the AST census
  recorded the original kind, so the widening set is auditable). No base `def` became a `defp`:
  the six W4-07 transitional facade interfaces moved to their concept modules as public functions
  (P3-2 ruling below).
* Delegates rather than moves for the public API: `defdelegate` preserves defaults/specs/docs with
  one line per entry, and keeps the facade readable as the map of the feature.
* Two size-forced sub-group splits (`Handoff`, `GoalControl`) exist only because the literal k2
  grouping crosses 1,500 lines; both stay inside the k2 concept and naming rules.
* Out of scope: any change to `Goals.Internal`/`EventsReceipts`/`RunFence`/`HarnessSessions`
  internals; any behaviour, message or schema change; the `Docs` group (no such functions exist in
  this revision); `Goals.Workers` (removed in the W4-07 reshape).









## Conductor rulings (2026-11-22, PR #248 independent review — recorded per the contract)

* P3-2: the facade's public names drop from 37 to 31 because the six W4-07 transitional interfaces
  moved to their concept modules; their only in-repo caller (RunFence) is repointed and nothing
  dangles. Ruled **not a public-API change** for the clause-3 failure control (internal
  transitional surface, no external consumer).
* P3-3: `control/lib/symmetry_control/goals/run_fence.ex` is added to the owned set for the repoint
  edits only (aliases and call-site module names; target functions byte-identical). No behaviour
  change permitted there.
* P3-6: the `portal_goal_attention_test.exs` comment references to moved functions may be
  corrected (comments only); done in the fix-stage section below.

## Results (measured)

Every row below was run in this worktree after the last edit; the log file named in the artifact
column is the raw output kept in the slice's scratch directory (outside the repository; the
delivery report names it).

| gate | command | exit | observed | artifact |
| --- | --- | --- | --- | --- |
| format | `mix format --check-formatted` | 0 | no files need formatting (empty log) | `format.log` |
| compile | `MIX_ENV=test mix compile --warnings-as-errors` | 0 | compiled, no warnings | `compile.log` |
| xref_plain | `mix xref graph --format cycles` | 0 | 3 cycles found | `xref_plain.log` |
| xref_cc | `mix xref graph --format cycles --label compile-connected` | 0 | No cycles found | `xref_cc.log` |
| targeted | `mix test` (goals_test + goals/ + 5 oracle files) | 0 | Result: 380 passed, 11 skipped | `targeted.log` |
| full_cover | `mix test --cover --exclude portal_assets` (alone) | 0 | Result: 1086 passed, 21 skipped, 4 excluded | `full_cover.log` |

Raw counts:

```
Result: 380 passed, 11 skipped
Result: 1086 passed, 21 skipped, 4 excluded
credo head: 499 issues (oneline letters: 57 design / 357 refactoring / 80 readability / 5 warnings)
credo base: 500 issues (5 warnings / 357 refactoring / 82 readability / 56 design)
format: no files need formatting (empty log)
3 cycles found
No cycles found
OK
check-citation-existence: the apply tree refreshed the 3196 citation (committed at 067c0dcc); at 067c0dcc the gate still refused 1 pre-existing citation in docs/archive/CLOSED-TODO.md:2772 (symbol `events` bound by the checker to frontend/src/features/goals/detail.tsx, whose lowercase events query key #245 moved out), and the fix-stage tree repairs that line (", " -> " and ", the smallest wording change that keeps the record truthful) under the fix-stage owned-set amendment; the checker reads commits, so the gate ran on a copy of the slice head (ed9413f7: 067c0dcc plus the fix-stage CLOSED-TODO.md, blob 5cdaad00, base 42afb56c) and exits 0 -- "1 changed document(s) pass the path and symbol check (518 path citation(s))"
git diff --check: clean (exit 0)
host path scan: 0 hits (grep exit 1)
backslash-u scan: scope total (lib `goals.ex` + `goals/` + the goals test files) base 9 = HEAD 9; over the committed range (`git diff origin/main...HEAD`, added lines) the program's DLP pattern (a backslash immediately followed by `u`) matches 4 lines, all four a verbatim carry of a base `goals_test.exs` line (two patterns x two sites), so the slice writes no new occurrence; this committed-range count supersedes the fix-stage scan of the then-empty worktree diff (0, vacuous)
native-evidence: Ran 15 tests — OK
red mutants: 92 KILLED / 0 SURVIVED / 0 COMPILE_ERR (90 apply-stage rows + 2 fix-stage rows)
titles: the 8 split files 147 = base `goals_test.exs` 147; repo-wide base 1022 is contained in final 1103 (0 lost, +81)

R2 line coverage (`mix test --cover --exclude portal_assets`; the base revision runs in a
scratch worktree at `642928b3` sharing this tree's deps, with its own build path and test
partition). The summary cover tool prints percentages only, so the table is percentage-only:

| module | base `642928b3` | HEAD |
| --- | --- | --- |
| `SymmetryControl.ContractSchema` | 100.00% | 100.00% |
| `SymmetryControl.Goals` | 86.32% | 84.38% |
| `SymmetryControl.Goals.Admission` | — (absent) | 85.83% |
| `SymmetryControl.Goals.Budget` | — (absent) | 88.70% |
| `SymmetryControl.Goals.ChangesetValidators` | 100.00% | 100.00% |
| `SymmetryControl.Goals.Commands` | — (absent) | 85.86% |
| `SymmetryControl.Goals.ContextAssembly` | — (absent) | 93.92% |
| `SymmetryControl.Goals.ContextSnapshot` | 100.00% | 100.00% |
| `SymmetryControl.Goals.ContractValidation` | 84.59% | 84.59% |
| `SymmetryControl.Goals.Decisions` | — (absent) | 98.68% |
| `SymmetryControl.Goals.EventsReceipts` | 93.58% | 93.58% |
| `SymmetryControl.Goals.Goal` | 93.33% | 93.33% |
| `SymmetryControl.Goals.GoalBudgetReservation` | 100.00% | 100.00% |
| `SymmetryControl.Goals.GoalControl` | — (absent) | 89.23% |
| `SymmetryControl.Goals.GoalDecision` | 95.24% | 95.24% |
| `SymmetryControl.Goals.GoalEvent` | 100.00% | 100.00% |
| `SymmetryControl.Goals.GoalExternalWait` | 92.59% | 92.59% |
| `SymmetryControl.Goals.GoalRevision` | 85.19% | 85.19% |
| `SymmetryControl.Goals.Handoff` | — (absent) | 92.56% |
| `SymmetryControl.Goals.HarnessSession` | 100.00% | 100.00% |
| `SymmetryControl.Goals.HarnessSessionAttachReceipt` | 100.00% | 100.00% |
| `SymmetryControl.Goals.HarnessSessionStopReceipt` | 100.00% | 100.00% |
| `SymmetryControl.Goals.HarnessSessions` | 92.92% | 92.92% |
| `SymmetryControl.Goals.Internal` | 79.61% | 79.61% |
| `SymmetryControl.Goals.JsonDocument` | 55.56% | 55.56% |
| `SymmetryControl.Goals.PlanContracts` | — (absent) | 85.82% |
| `SymmetryControl.Goals.ReadModel` | 90.85% | 90.85% |
| `SymmetryControl.Goals.ReadModel.Projection` | 100.00% | 100.00% |
| `SymmetryControl.Goals.Reconciliation` | — (absent) | 89.14% |
| `SymmetryControl.Goals.Recovery` | — (absent) | 84.07% |
| `SymmetryControl.Goals.RunEvidence` | 100.00% | 100.00% |
| `SymmetryControl.Goals.RunFence` | 84.84% | 84.69% |
| `SymmetryControl.Goals.RunUsage` | 84.62% | 84.62% |
| `SymmetryControl.Goals.Settlement` | — (absent) | 80.51% |
| `SymmetryControl.Goals.ValidationOutcome` | — (absent) | 92.63% |
| `SymmetryControl.Goals.ValidationProfiles` | 83.33% | 83.33% |
| `SymmetryControl.Goals.WorkDependency` | 83.33% | 83.33% |
| `SymmetryControl.Goals.WorkOutcome` | 85.71% | 85.71% |
| `SymmetryControl.Goals.Workers.GoalControlWorker` | 65.63% | 65.63% |
| `SymmetryControl.Goals.Workers.SettleTaskWorker` | 60.00% | 60.00% |
| `SymmetryControl.Goals.Workers.WakeupWorker` | 50.98% | 50.98% |
| `Total` | 87.07% | 87.32% |

Reading the table: base `goals.ex` (8,121 lines) was 86.32% covered; at the final tree the facade
is 84.38% of its 332 remaining lines (its bodies now live in the concept modules) and those 12
modules range from 80.51% (Settlement) to 98.68% (Decisions). Every other base module keeps its
percentage except `RunFence` (84.84% → 84.69%; the repointed guard's layout, recorded in the credo
section). The apply stage's strengthened and new interface cases lift `Admission` (85.63% → 85.83%)
and `ValidationOutcome` (90.53% → 92.63%) and the `Total` (base 87.07% → `dc070002` 87.21% → final
87.32%). The summary tool prints no line counts, so the deliberate non-reduction evidence is:
the 147 test titles are unchanged as a multiset (the same tests run the same bodies), the
per-function AST of the moved code is identical (`282` exact + `21` recorded call-prefix
rewrites), and the only lines that change class are each new module's header/import
scaffolding (the same artefact phase 1 recorded for its four extracted modules).

Red-mutant results at the apply stage: 91 executed rows, KILLED 91 (SURVIVED 0, COMPILE_ERR 0) —
the 75 `dc070002` rows re-anchored to the final tree (`mutants_reanchored2.tsv`: "total 75 killed
75 survived 0 err 0"), 15 rows for the cases the apply stage strengthened or added
(`mutants_apply2.tsv`: "total 15 killed 15 survived 0 err 0") and the F23 consistency probe
(`mutants_f23.tsv`: the `recovery.ex` key-format mutation is killed by the existing
`commands_flow_test.exs:709` case, which is why F23 needs no new case). The 75-row first-pass table
follows.

| status | case | mutation | file |
| --- | --- | --- | --- |
| KILLED | admit_plan_items!/3 pins the admitted revision, membership and baseline | `raise at   def admit_plan_items!(goal, items, opts) do` | `admission.ex` |
| KILLED | admit_task!/6 records the queued task, snapshot, reservation and item binding | `raise at   defp admit_task!(goal, item, purpose, model_profile, paylo` | `admission.ex` |
| KILLED | admit_plan_task!/7 creates the runless planning task with its own snapshot and r | `raise at   def admit_plan_task!(goal, resource, model_profile, subjec` | `admission.ex` |
| KILLED | admit_authorized_task!/6 rechecks item admissibility before one admission | `raise at   def admit_authorized_task!(goal, item, purpose, model_prof` | `admission.ex` |
| KILLED | ensure_admissible_item!/2 rejects an item admitted under another revision | `raise at   def ensure_admissible_item!(goal, item) do` | `admission.ex` |
| KILLED | ensure_unaccepted_item!/2 rejects an item with an accepted outcome | `raise at   def ensure_unaccepted_item!(goal, item) do` | `admission.ex` |
| KILLED | ensure_achievable!/2 binds the accepted integration outcome to the frozen subjec | `raise at   def ensure_achievable!(goal, payload) do` | `admission.ex` |
| KILLED | creates_cycle?/3 detects only the reversing edge in an admitted DAG | `raise at   def creates_cycle?(goal_id, work_item_id, depends_on_id) d` | `admission.ex` |
| KILLED | manual_admission_payload!/6 binds the command mutation and the plan baseline sub | `raise at   def manual_admission_payload!(goal, item, purpose, payload` | `admission.ex` |
| KILLED | resource_allowed?/2 treats an empty allow-list as all resources | `raise at   def resource_allowed?(policy, resource_id) do` | `admission.ex` |
| KILLED | goal_predicate_matches_evidence?/2 matches evidence rows against the predicate i | `        evidence.kind == "review" and` | `admission.ex` |
| KILLED | budget_status/3 counts a held strict reservation against the total limit | `raise at   def budget_status(goal, revision, reserved_microusd) do` | `budget.ex` |
| KILLED | admission_limits!/2 defaults one turn to the bounded fifteen minute window | `raise at   def admission_limits!(payload, opts) do` | `budget.ex` |
| KILLED | server_reservation_amount/1 reserves every strict attempt and leaves soft mode u | `raise at   def server_reservation_amount(revision) do` | `budget.ex` |
| KILLED | server_admission_budget!/3 attaches cost limits only to the strict revision | `raise at   def server_admission_budget!(goal, revision, limits) do` | `budget.ex` |
| KILLED | ensure_resume_budget!/2 checks only the strict revision and returns :ok | `raise at   def ensure_resume_budget!(goal, revision) do` | `budget.ex` |
| KILLED | reconcile_reservation!/3 releases a terminal task reservation exactly once | `raise at   def reconcile_reservation!(goal, task, opts) do` | `budget.ex` |
| KILLED | create_goal/4 persists one draft goal, revision and goal_created event behind a  | `              state: "active",` | `commands.ex` |
| KILLED | command/4 returns the created request_decision receipt and replays it exactly | `        {:ok, {:replayed, receipt}} ->           {:ok, receipt, :created}` | `commands.ex` |
| KILLED | canonical_plan_proposal/1 fills missing integration and change_target defaults p | `              \|> Map.put_new("integration", true)` | `commands.ex` |
| KILLED | context_digest/1 renders a raw sha256 digest and passes through a wire digest | `    do: "sha256:" <> Base.encode16(digest, case: :upper)` | `context_assembly.ex` |
| KILLED | canonical_authority_policy/1 fills the operator and publication defaults | `    \|> Map.put_new("publication_allowed", true)` | `context_assembly.ex` |
| KILLED | canonical_context_manifest/1 fills the byte budget and repository source default | `    \|> Map.put_new("byte_budget", 65_536)` | `context_assembly.ex` |
| KILLED | context_wire_envelope/4 returns the persisted snapshot document only while it st | `         true <- document["snapshot_id"] == snapshot.goal_id,` | `context_assembly.ex` |
| KILLED | planning_context_snapshot_document/7 pins the planning contract, sources and con | `      "purpose" => "implement",` | `context_assembly.ex` |
| KILLED | context_snapshot_document/11 pins the implement work contract, sources and conte | `        "source_revision" => "goal:#{goal.id}:#{revision.revision}",` | `context_assembly.ex` |
| KILLED | decision_request!/6 hashes the plan proposal pre-image and asks the plan questio | `    {nil, nil, plan_action_hash(goal, RequestHash.canonical(value(proposal, :items))),` | `decisions.ex` |
| KILLED | derived_decision_options/1 derives the accept and reject options with their cons | `        "consequence" => "Keep the current state."` | `decisions.ex` |
| KILLED | unresolved_decisions?/2 is true only while a decision at the revision stays open | `            decision.state in ["open", "resolved"]` | `decisions.ex` |
| KILLED | pending_plan_task?/2 tracks an unsettled plan task at the current revision | `            task.purpose == "implement",` | `decisions.ex` |
| KILLED | plan_decision_pending_acceptance?/1 tracks an open or accepted plan decision at  | `            decision.kind == "scope" and` | `decisions.ex` |
| KILLED | planning_acceptance_contract/0 is the operator-acceptance contract for proposed  | `      "predicates" => [%{"id" => "operator", "kind" => "check"}]` | `decisions.ex` |
| KILLED | ensure_resolved_scope_decision!/5 admits the accepted dependency decision and re | `             decision.state == "resolved" and decision.kind == "review" and` | `decisions.ex` |
| KILLED | ensure_resolved_completion_decision!/3 admits the accepted completion subject ha | `             decision.action_hash == expected_action_hash and              value(decision.resolution \|\| %{},` | `decisions.ex` |
| KILLED | decision_option?/2 matches an option id inside the decision options | `    Enum.any?(decision.options \|\| [], &(value(&1, :id) != option_id))` | `decisions.ex` |
| KILLED | validate_decision_changeset!/2 accepts a compatible wire and aborts an invalid o | `      {:error, _reason} -> :ok` | `decisions.ex` |
| KILLED | control_dispatch_plan/4 enumerates the exact descriptor for the current cancella | `raise at   def control_dispatch_plan(goal_id, revision, action_id, op` | `goal_control.ex` |
| KILLED | dispatch_goal_control_command/8 creates one runless cancellation and replays it  | `raise at   def dispatch_goal_control_command(         goal_id,       ` | `goal_control.ex` |
| KILLED | apply_goal_pause_transition/5 pauses the run from its pending pause command | `raise at   def apply_goal_pause_transition(run_id, fence, payload, tr` | `goal_control.ex` |
| KILLED | reject_pending_goal_control_commands!/2 acknowledges only goal control commands | `raise at   def reject_pending_goal_control_commands!(goal_id, current` | `goal_control.ex` |
| KILLED | handoff_source!/6 returns the settled source identity of a completed candidate | `  def handoff_source!(         _goal,         _target_work_item_id,         _repository_resource_id,         _` | `handoff.ex` |
| KILLED | handoff_admission_subject!/6 binds a validation handoff to the source candidate | `raise at   def handoff_admission_subject!(_goal, _item, "validate", p` | `handoff.ex` |
| KILLED | admission_session_mode!/2 derives the mode from the requested session | `raise at   def admission_session_mode!(payload, requested_session_id)` | `handoff.ex` |
| KILLED | ensure_requested_session_available!/3 binds resume to a verified available sessi | `  def ensure_requested_session_available!(_item, nil, "fresh"), do: raise "rf-w5-14 mutant"` | `handoff.ex` |
| KILLED | ensure_plan_requested_session_available!/3 locks the plan resource session and r | `  def ensure_plan_requested_session_available!(_resource, nil, "fresh"), do: raise "rf-w5-14 mutant"` | `handoff.ex` |
| KILLED | valid_plan_proposal?/3 accepts an envelope bound to the goal revision and reject | `      value(proposal, :schema_version) == "symmetry.plan.v2" and` | `plan_contracts.ex` |
| KILLED | plan_admission_preflight?/2 requires an integration designation and valid immuta | `      true and` | `plan_contracts.ex` |
| KILLED | valid_acceptance_contract?/1 refuses duplicate predicate ids and unknown keys | `      true` | `plan_contracts.ex` |
| KILLED | immutable_acceptance_contracts_valid?/2 resolves every contract against the prof | `             {:error, _},` | `plan_contracts.ex` |
| KILLED | acceptance_contract_matches_resource?/2 binds artifact predicates to one resourc | `      value(predicate, :kind) != "artifact" or value(predicate, :resource_id) != resource_id` | `plan_contracts.ex` |
| KILLED | derived_integration_key/1 derives only the single required item key | `      {:derived, _ref} -> nil` | `plan_contracts.ex` |
| KILLED | required_untrimmed_string!/2 returns an unchanged non-empty string or aborts the | `        if value != "", do: value, else: rollback(:invalid_plan)` | `plan_contracts.ex` |
| KILLED | reconcile_due_goals/1 reconciles only the requested goal ids | `raise at   def reconcile_due_goals(opts \\ [])    def reconcile_due_g` | `reconciliation.ex` |
| KILLED | accepted_work_item?/2 recognizes only current-revision accepted outcomes | `raise at   def accepted_work_item?(goal, work_item_id) do` | `reconciliation.ex` |
| KILLED | automatic_admission_mutation_id/4 freezes goal, revision, item, purpose and sour | `raise at   def automatic_admission_mutation_id(goal, item, purpose, s` | `reconciliation.ex` |
| KILLED | automatic_baseline_subject/2 returns the admitted baseline subject and its froze | `raise at   def automatic_baseline_subject(goal, item) do` | `reconciliation.ex` |
| KILLED | automatic_progress_subject/4 binds the settled progress run identity | `raise at   def automatic_progress_subject(goal, item, source_task, op` | `reconciliation.ex` |
| KILLED | recover_pending_unstarted_cancellations/1 returns held cancelled tasks before se | `raise at   def recover_pending_unstarted_cancellations(goal_id) when ` | `recovery.ex` |
| KILLED | recoverable_unstarted_cancellation_goal_ids/1 lists goals with held runless canc | `raise at   def recoverable_unstarted_cancellation_goal_ids(opts \\ []` | `recovery.ex` |
| KILLED | recover_pending_terminal_settlements/1 respects the lease-expiry grace boundary | `raise at   def recover_pending_terminal_settlements(opts \\ [])    de` | `recovery.ex` |
| KILLED | recover_pending_goal_control_actions/1 returns the latest pending control action | `raise at   def recover_pending_goal_control_actions(opts \\ [])    de` | `recovery.ex` |
| KILLED | control_descriptors/3 maps cancellable tasks to cancel descriptors | `raise at   def control_descriptors(goal, "pause", action_id) do` | `recovery.ex` |
| KILLED | ensure_amendment_controls_settled!/1 rolls back while stale amendment work remai | `raise at   def ensure_amendment_controls_settled!(goal) do` | `recovery.ex` |
| KILLED | recoverable_historical_unstarted_cancellation?/2 requires an applied runless can | `raise at   def recoverable_historical_unstarted_cancellation?(goal, t` | `recovery.ex` |
| KILLED | settle_task/4 settles a completed candidate Run as awaiting validation | `raise at   def settle_task(task_id, run_id, generation, opts \\ [])  ` | `settlement.ex` |
| KILLED | settle_unstarted_task/3 settles a committed runless cancellation once | `raise at   def settle_unstarted_task(task_id, reason, opts \\ [])    ` | `settlement.ex` |
| KILLED | terminal_next_wake_at/4 keeps the earliest future wake | `raise at   def terminal_next_wake_at(goal, task, terminal, opts) do` | `settlement.ex` |
| KILLED | terminal_settlement!/7 reports a failed Run's semantic reason | `      do: process_terminal_settlement("cancelled", failed_terminal_reason(run, opts))` | `settlement.ex` |
| KILLED | settlement_task_owned_by_goal?/3 requires goal, item and task to agree | `raise at   def settlement_task_owned_by_goal?(task, goal, nil) do` | `settlement.ex` |
| KILLED | external_wait_identity/1 freezes the wait, task, run, result and check sequence | `raise at   def external_wait_identity(wait) do` | `settlement.ex` |
| KILLED | external_wait_wake_at/2 schedules no wake | `  def external_wait_wake_at(_goal, _current), do: raise "rf-w5-14 mutant"` | `settlement.ex` |
| KILLED | lock_settlement_tasks!/2 locks the validation task and its producer | `raise at   def lock_settlement_tasks!(task_id, validation_of_task_id)` | `settlement.ex` |
| KILLED | lock_settlement_runs!/3 locks the run and its producing run | `raise at   def lock_settlement_runs!(run_id, task_id, producer) do` | `settlement.ex` |
| KILLED | reconcile_validation_outcome/4 settles a resolved operator rejection as rejected | `raise at   def reconcile_validation_outcome(task_id, run_id, generati` | `validation_outcome.ex` |
| KILLED | derive_validation_outcome!/10 inserts the accepted outcome bound to the candidat | `raise at   def derive_validation_outcome!(         goal,         item` | `validation_outcome.ex` |

Mode `raise` rows append ` raise "rf-w5-14 mutant"` to the subject's first clause head: the case
is killed only if it calls the entry. Mode `replace` rows apply the case author's semantic
mutation (the ledger above lists them) or a one-line `, do:` clause body that raises. Rows first
recorded as `COMPILE_ERR`/`SURVIVED` were anchor defects (multi-line heads, `, do:` clauses, and
one mutant aimed at the sibling function of the same name): they were repaired and re-run in
`mutants_fix*.tsv`, and every one is killed. `mutants_pre.tsv`/`mutants_post.tsv` are kept
unchanged as the raw record of the first pass.

At `dc070002` every interface case had exactly one row: 75 cases across the 12 interface files,
75 rows in `mutants.json` (32 authored semantic mutants executed with mode
`replace`, 43 call-site mutants executed with mode `raise`). The apply stage keeps those 75 rows
(re-anchored) and adds `mutants_apply.json`: 15 `replace` rows — 11 for strengthened assertions of
existing cases and 4 for the new cases (3 x `reconcile_validation_outcome/4`, 1 x frozen
`RequestHash`/plan-action vector); the fix stage adds `mutants_fix_r1.json`: 2 `replace` rows for
the two new `ContextAssembly` cases — so the final tree is 81 cases over 92 executed rows (plus
the F23 consistency probe row), all KILLED. A `SURVIVED` row would mean the case does not exercise
its entry, or no longer kills it.

## Apply stage — accepted simplify findings

Source: the simplify review of `7f34d40d..dc070002` and its accepted-findings file (kept in the
slice's scratch directory, named by the delivery report). The final tree is `dc070002` plus the
edits below; no commit was made. `control/lib/**` is untouched by the whole stage.

Applied: 27 accepted rows (20 findings + 7 PD rows; PD9 pairs with F3).

| group | rows | files | evidence |
| --- | --- | --- | --- |
| compile-warning cleanups | F1, F2 | `goals_test_support.exs` + 9 test files | before: 3 `unused import` warnings + 10 set-only attributes; after: `mix compile --warnings-as-errors` clean. `commands_test.exs`'s `@validation_runtime_id` is kept (reused by F37) |
| dead-override deletion | F3 (= PD9) | support + 4 test files, 25 sites (the finding named 4; the same-key scan found 21 more) | `admission_payload/2` drops `:reserved_microusd`/`:admission_key` unconditionally; the server refuses caller injection, pinned by `commands_test.exs`'s field loop and `reconciliation_test.exs:830`, so deleting the dead keys is the edit that keeps those paths green |
| duplicate helpers / verbatim extractions | F4, F9, F10, F11, F12, F29 (1, 3, 4, 5, 6a, 6b-resolve), F30 | support + 10 test files | field-by-field diffs; `put_task_result!` -> `put_run_task_result!` with 0 leftovers; `check_contract/0` and `repository_fixture/1` delegate to `TestFixtures`; the admission helper is named `task_admission_payload/2` because a same-name `def` would collide with the imported support function (compile error reproduced) |
| comment hygiene | F5, F6 | `reconciliation_test.exs`, `handoff_interface_test.exs` | comment policy items 1/4 |
| strengthened assertions | F31, F32, F33, F34, F35, F37, PD4, PD5, PD6, PD7 | 6 test files | one executed `replace` mutant per row (11 rows in `mutants_apply2.tsv`) |
| new cases | F36 (3), F38 (1) | `validation_outcome_interface_test.exs`, `commands_interface_test.exs` | 4 new titles; F36's third case tampers through `ALTER TABLE work_outcomes DISABLE TRIGGER work_outcomes_immutable_history` (a plain delete hits the `goal_0006_immutable_history` trigger) |
| records | PD1, PD2 | `docs/TODO.md`, this document | state-set homes list re-derived per file; run_fence alias record corrected (3 additions, 1 removal) |

Not applied (18 findings): F7, F8, F13-F22, F23-F27, each with its clause conflict in the delivery
report — the moved-lib dedups would rewrite moved bodies and invalidate the per-function AST proof
and the declaration table (clause 2), F8 additionally deletes a base function, F23/F25 would add a
cross-module edge (clause 3), F26 changes the observable replay envelope (clauses 3/4), F27 reaches
for a third canonicalizer, F28's shared rule-attribution comment is a documented boundary. Skipped
with reasons: PD3 (the replay-shape asymmetry is a behaviour decision, recorded in `docs/TODO.md`),
PD8 (protected non-adoption record; reachability recorded in the plan), F29 sub-items 2/7/8 and
6b-request (data literals / no duplicate to remove / needs a contract change).

Deviations recorded at this stage:

1. `docs/archive/CLOSED-TODO.md:3196` — the `ensure_preconditions!/2` citation's owner path moved
   from `control/lib/symmetry_control/goals.ex:8571-8585` to
   `control/lib/symmetry_control/goals/events_receipts.ex:347-363`: the gate
   `check-citation-existence.py` refuses the old citation at `dc070002` (the symbol moved in the
   W4-07 reshape; main's `7f34d40d` wrote the citation stale), and the contract's gate clause
   requires refreshing citations that move. This single line is the only edit outside the strict
   owned set. The checker reads commits only, so it was verified on a faithful copy of the slice
   head (`git clone --shared` of this worktree in the scratch directory, the full worktree diff
   applied and committed there as `2cb10013`): exit 0, "1 changed document(s) pass the path and
   symbol check (518 path citation(s))".
2. `goals_test_support.exs` credo follow-up (501 -> 499), recorded in the credo section above.
3. Red-mutant evidence re-anchored to the final tree after those edits: 91/91 KILLED (75
   re-anchored rows + 15 new rows + the F23 probe).

Apply-stage gates (final tree; full logs in the slice scratch directory): the refreshed Results and
R2 tables above, `verify_ast.exs` (282 identical + 21 recorded rewrites, byte-identical to the
`dc070002` table), `string_compare.py` (312/312 identical, byte-identical), the title multiset,
`mutant.py` (91 rows), `test-omo-control-native-evidence.py` (15 tests OK),
`check-citation-existence.py`, `git diff --check`, the host-path scan and the backslash-u scan.
The full suite was retried once: the first `mix test --cover --exclude portal_assets` run exited 2
with a single unrelated WebSocket timing flake (`SymmetryControlWeb.DaemonChannelTest`, a 100 ms
`assert_receive` whose mailbox already held the PubSub tuple; the file passes alone: 3 tests), and
the retry is the 1086-passed row recorded in the Results table.

## Tree state (no commits made)

```
 M control/lib/symmetry_control/goals.ex
 M control/lib/symmetry_control/goals/run_fence.ex
 M control/test/symmetry_control/goals_test.exs
 M docs/TODO.md
 M scripts/ci/omo-control-native-evidence.py
?? control/lib/symmetry_control/goals/admission.ex
?? control/lib/symmetry_control/goals/budget.ex
?? control/lib/symmetry_control/goals/commands.ex
?? control/lib/symmetry_control/goals/context_assembly.ex
?? control/lib/symmetry_control/goals/decisions.ex
?? control/lib/symmetry_control/goals/goal_control.ex
?? control/lib/symmetry_control/goals/handoff.ex
?? control/lib/symmetry_control/goals/plan_contracts.ex
?? control/lib/symmetry_control/goals/reconciliation.ex
?? control/lib/symmetry_control/goals/recovery.ex
?? control/lib/symmetry_control/goals/settlement.ex
?? control/lib/symmetry_control/goals/validation_outcome.ex
?? control/test/symmetry_control/goals/admission_interface_test.exs
?? control/test/symmetry_control/goals/budget_interface_test.exs
?? control/test/symmetry_control/goals/commands_flow_test.exs
?? control/test/symmetry_control/goals/commands_interface_test.exs
?? control/test/symmetry_control/goals/commands_test.exs
?? control/test/symmetry_control/goals/context_assembly_interface_test.exs
?? control/test/symmetry_control/goals/decisions_interface_test.exs
?? control/test/symmetry_control/goals/goal_control_interface_test.exs
?? control/test/symmetry_control/goals/goals_test_support.exs
?? control/test/symmetry_control/goals/handoff_interface_test.exs
?? control/test/symmetry_control/goals/harness_session_flow_test.exs
?? control/test/symmetry_control/goals/plan_contracts_interface_test.exs
?? control/test/symmetry_control/goals/reconciliation_interface_test.exs
?? control/test/symmetry_control/goals/reconciliation_test.exs
?? control/test/symmetry_control/goals/recovery_interface_test.exs
?? control/test/symmetry_control/goals/recovery_test.exs
?? control/test/symmetry_control/goals/run_fence_flow_test.exs
?? control/test/symmetry_control/goals/settlement_interface_test.exs
?? control/test/symmetry_control/goals/settlement_test.exs
?? control/test/symmetry_control/goals/validation_outcome_interface_test.exs
?? docs/plans/2026-11-22-rf-w5-14-goals-concepts-phase2-plan.md
```

Why: the contract forbids commits in this stage; the plan document itself is part of the slice.
Everything above is the implement stage's record of the same slice: `dc070002` is the branch tip
that carries it (the phase-2 commits plus the `origin/main` merge), and the apply-stage edits are
the only uncommitted delta.

Current uncommitted state at the apply stage (`git status --short`, captured after the last gate
run, so no mutant-harness edit is in flight):

```
 M control/test/symmetry_control/goals/admission_interface_test.exs
 M control/test/symmetry_control/goals/budget_interface_test.exs
 M control/test/symmetry_control/goals/commands_flow_test.exs
 M control/test/symmetry_control/goals/commands_interface_test.exs
 M control/test/symmetry_control/goals/commands_test.exs
 M control/test/symmetry_control/goals/context_assembly_interface_test.exs
 M control/test/symmetry_control/goals/decisions_interface_test.exs
 M control/test/symmetry_control/goals/goal_control_interface_test.exs
 M control/test/symmetry_control/goals/goals_test_support.exs
 M control/test/symmetry_control/goals/handoff_interface_test.exs
 M control/test/symmetry_control/goals/harness_session_flow_test.exs
 M control/test/symmetry_control/goals/plan_contracts_interface_test.exs
 M control/test/symmetry_control/goals/reconciliation_interface_test.exs
 M control/test/symmetry_control/goals/reconciliation_test.exs
 M control/test/symmetry_control/goals/recovery_test.exs
 M control/test/symmetry_control/goals/run_fence_flow_test.exs
 M control/test/symmetry_control/goals/settlement_test.exs
 M control/test/symmetry_control/goals/validation_outcome_interface_test.exs
 M control/test/symmetry_control/goals_test.exs
 M docs/TODO.md
 M docs/archive/CLOSED-TODO.md
 M docs/plans/2026-11-22-rf-w5-14-goals-concepts-phase2-plan.md
```

## Fix stage (PR #248 review round 1 — 2026-11-22)

Findings source: `248-opus-review-1.md` (verdict: fix first). Fixes applied, all inside the owned
set plus the contract's P3-3/P3-6 rulings:

* P2-1: two cases added to `goals/context_assembly_interface_test.exs` for the two RunFence-only
  `@doc false` entries — `ContextAssembly.safe_document?/1` (line 402) and
  `safe_evidence_document/1` (line 408). Each has one executed `replace` row in
  `mutants_fix_r1.json`; both KILLED with exit 2 (`mutants_fix_r1.tsv`; the killing assertions sit
  at `context_assembly_interface_test.exs:404` and `:413`). The coverage audit above re-derives to
  63 + 13 = 76 required entries.
* P3-2/P3-3: the conductor rulings are recorded in their own section above.
* P3-4: the false "`def` functions … narrowed to `defp`" claim is removed (no base `def` became a
  `defp`); `docs/archive/CLOSED-TODO.md`'s `append_event!/7` citation becomes `append_event!/8` at
  `control/lib/symmetry_control/goals/events_receipts.ex:65` (its lock at `:134`), and the same
  sentence's stale refs are repointed to `commands.ex:458` (amend bump), `goal_control.ex:395` and
  `commands.ex:447` (the two `update_all` sites under `goals/`). The backslash-u gate is re-run on
  the committed range: 4 added lines match, each a verbatim carry of a base `goals_test.exs` line,
  scope total base 9 = HEAD 9 (supersedes the vacuous worktree-diff 0).
* P3-5: the two item-1 comments are removed (`reconciliation_test.exs`,
  `admission_interface_test.exs`).
* P3-6: the two stale comment references in `portal_goal_attention_test.exs` (~:869, ~:918) now
  name the owning modules (comments only).

The apply-stage full-suite and coverage rows above are not re-measured in this stage (the contract's
fix-round gates name the targeted suite only); the two added interface cases can only raise
`ContextAssembly` coverage.

Fix-stage gates (FOREGROUND, one at a time, portable-toolchain wrapper with
`MIX_TEST_PARTITION=rf-w5-14`; the Results table's targeted row is the apply-stage measurement of
the same command):

| gate | exit | observed |
| --- | --- | --- |
| `mix format --check-formatted` | 0 | no files need formatting |
| `MIX_ENV=test mix compile --warnings-as-errors` | 0 | compiled, no warnings |
| targeted `mix test` (goals_test.exs + `goals/` + the 5 oracle files) | 0 | Result: 382 passed, 11 skipped |
| `mix xref graph --format cycles` | 0 | 3 cycles found (25 / 20 / 4, unchanged) |
| `mix xref graph --format cycles --label compile-connected` | 0 | No cycles found |
| `check-citation-existence.py --base 66bc9dcb --head <snapshot>` | 0 | "1 changed document(s) pass the path and symbol check (523 path citation(s))", 3 pre-existing ignore-rule notices |
| `test-omo-control-native-evidence.py` | 0 | Ran 15 tests — OK |
| host-path added lines (committed range + worktree) | — | 0 hits |
| backslash-u added lines (committed range) | — | 4, all verbatim base carries; worktree diff 0 |
| `git diff --check` | 0 | clean |

The citation gate reads commits, so it ran on a faithful committed copy of the fix-stage tree
(`git clone --shared` of this worktree in the scratch directory, the worktree diff applied and
committed there as `2430b440`, base `66bc9dcb`).
