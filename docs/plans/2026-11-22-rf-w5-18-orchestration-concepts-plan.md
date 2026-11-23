# rf-w5-18 orchestration concepts plan — facade over Registry / Assignment / Claims / Transitions / Commands / TaskReads + `Goals.Eligibility`

Contract: `goal-rf-w5-18-orchestration-concepts.md` (GDD state root, sha256
`3d6bed26be1ec8ac712f126e833800c6a2d7a831c659dc01792eda7f699442f6`). Binding sections: the
`arch-decision-2` conductor decision and its superseding sections (`open-item rulings`,
`verification amendments`, owner `L3=a`, the Tier-2 approval record + scope pin) and the owner's
`Comment policy (2026-11-22)`.

Baseline: this worktree's HEAD `66bc9dcb5bc5a2012f16480dbb4c6e13e4f34283` (= `origin/main`).
Every dispatch prerequisite is merged into it: W2-07 #184, W4-06 #189, W3-03 #192, W3-14 #211,
A4 #209, and the Tier-2 doc amendment #246 (which *is* the HEAD commit), so the contract's HOLD
condition is lifted and `docs/design/core.md`/`data.md` are already amended. arch-decision-2 is
BOUND (`refactor/arch-decision-2.md`). No open PR touches an owned file (`gh pr list` → empty).
Base file under analysis: `control/lib/symmetry_control/orchestration.ex`, 4,731 L, 247 function
groups / 401 clauses.

## Owned files

`control/lib/symmetry_control/orchestration.ex` (facade); new
`control/lib/symmetry_control/orchestration/{registry,assignment,claims,transitions,commands,task_reads}.ex`;
new `control/lib/symmetry_control/goals/eligibility.ex`;
`control/lib/symmetry_control_web/controllers/daemon_controller.ex` (claim region `:100-140` plus
the two aliases the change makes unused); new tests
`control/test/symmetry_control/orchestration/{registry,assignment,claims,transitions,commands,task_reads}_test.exs`
and `control/test/symmetry_control/goals/eligibility_test.exs`; the split of
`control/test/symmetry_control/orchestration_test.exs` and
`control/test/symmetry_control/orchestration_goal_admission_test.exs`; this plan; the evidence
record; `docs/TODO.md` / `docs/archive/CLOSED-TODO.md`. Not owned and not edited: `goals.ex` and
every existing `goals/*.ex`, `integrations/provider_access.ex`, the existing
`orchestration/{schemas,scheduler,reconciler,notifier}.ex`, `docs/design/core.md`, `docs/design/data.md`.

## Slice shape

`SymmetryControl.Orchestration` becomes a thin facade of 51 `defdelegate` clauses (264 L) over six
concept modules, and the goal-eligibility policy moves to a new `SymmetryControl.Goals.Eligibility`
leaf. `Orchestration.Claims` also gains the unified claim seam `claim_run/3`, which the HTTP
controller now calls instead of re-implementing the `:goal | :legacy` fork and the provider-scope
lock. No behaviour change: wire bytes, persisted bytes, SQL text, lock order, transaction
boundaries, returned atoms, error/log text and telemetry are byte-identical (evidence record).

Module sizes on the applied delivered tree (`wc -l`, apply stage 2026-11-22): facade 327;
Registry 659; Assignment 639; Claims 612; Transitions 1,181; Commands 798; TaskReads 874;
Goals.Eligibility 301 — every module ≤ 1,500 L. (The pre-apply slice head measured facade 266,
Registry 669, Assignment 640, Claims 607, Transitions 1,188, Commands 808, TaskReads 909,
Eligibility 307; the earlier table was a pre-implementation estimate. The delta is the apply
stage's changes plus the doc-block move of finding F6.)

## Public / API shape

- **The facade keeps every current public function**: 51 `defdelegate`s with the base `@spec` and
  `@doc false` headers preserved, name/arity/defaults/return shape unchanged. The two
  controller-only fork helpers named in Design 3 (`claim_owner/1`, `claim_with_disposition/3`)
  keep their facade clauses too, because `orchestration_telemetry_test.exs` and the controller's
  transition endpoint still call them; making them "internal to Claims" means the *controller's
  claim path* no longer routes on them, not that they leave the public module.
  The apply stage returned the seven public `@doc` blocks to their facade delegates (simplify
  finding F6), restored the base delegate parameter names where the split had left generated
  `arg1` (F4) and re-added the `RunEvent` alias the `append_events/4` spec resolves against (D7);
  the public inventory is unchanged.
- **`Claims.claim_run/3`** (new, `@doc false`): `claim_owner/1` read outside the transaction →
  `Repo.transaction` → `ProviderAccess.lock_claim_scope/1` *first* → owner dispatch
  (`:goal` → `Goals.claim_with_disposition/3`, `:legacy` → `claim_with_disposition/3`) →
  `TaskReads.task_snapshot/1` → provider-access issue/persist or replay → commit →
  `emit_claimed/1` only for `:created` after commit. Returns exactly the controller's previous
  `{:ok, {run, task, provider_access}} | {:error, reason}`. `daemon_controller.ex` calls it as
  `Orchestration.Claims.claim_run(run_id, request, lease_duration_ms: lease_duration_ms())`.
- **`Goals.Eligibility`** (`@moduledoc false`) exposes exactly the Design 2 (a)-(c) policy:
  `candidate_task_query/2` (composable Ecto query for the candidate-scan predicate set; the runner
  `next_goal_task_candidate_page/2` wraps it with the base `Repo.all/1`), the locked-row recheck
  predicates (`goal_task_current?/2`, `planning_task?/1`, `goal_task_repository_resource_id/2`,
  `goal_work_item_owned?/3`, `goal_task_assignable?/2`) and the goal-specific runtime constraints
  (`validation_runtime_allowed?/2`, `validation_runtime_ids_for_task/1`,
  `validation_binding_runtime_ids/1`, `intersect_validation_binding_runtime_ids/1`,
  `valid_validation_binding?/1`, `canonical_runtime_uuid?/1`, `goal_subject_matches_resource?/2`,
  `runtime_matches_goal_resource?/3`, `runtime_allowed_for_task?/2`, `task_allowed_runtime_ids/1`,
  `handoff_source_machine_matches?/2`) plus the shared map/UUID vocabulary those predicates read
  (`value/2,3`, `valid_uuid?/1`, `exact_map_keys?/2`). It depends on schemas and
  `Repo` only — no `Goals` or `Orchestration` facade (only the three `Orchestration` schema
  aliases, which the binding text permits).
- **No other public Orchestration API change**: no function added to the facade, none removed, no
  arity or return-shape change. `claim_run/3` lives on `Orchestration.Claims` and is exercised by
  `control/test/symmetry_control/orchestration/claims_test.exs` without `Plug.Conn`.

## Re-derived partition at HEAD (method)

1. **Declaration census** (`analyze.exs`, `Code.string_to_quoted/2` with `columns: true,
   token_metadata: true`): 247 function groups / 401 clauses, each group with its clause span,
   its attached attribute/doc/spec block and its local-call list.
2. **Block tiling**: each group owns the contiguous non-blank run above its first clause (bounded
   by the previous group's clause end) plus its clauses; the blocks tile lines 65-4,730 with 0
   overlaps and 0 duplicated lines; the only unowned non-blank lines are the module header
   (lines 1-63: moduledoc, import, aliases, 15 attribute definitions) and the module's closing
   `end`. Clause ends are the AST clause end extended over body continuation lines, so
   `do:`-form clauses and heredocs are covered.
3. **Call graph**: local calls collected from the AST with pipe-aware arity
   (`x |> f(y)` counts as `f/2`), canonicalised to the defined group when the called arity is a
   default-argument expansion; the caller side of every call is recorded, so the module graph is
   derived from the placement, not guessed.
4. **Placement**: the contract's binding module semantics (Registry = machine enrollment and
   authentication, runtime registration and heartbeat, runtime/adapter metadata validation;
   Assignment = assignment, runtime choice, capacity, session reservation; Claims = claim/lease/
   fence plus the seam; Transitions = fenced run transitions and run events, with the expiry
   cluster folded in; Commands = command creation/replay/ack/cancel/input/retry; TaskReads = the
   task/runtime read models, timeline and submission entry, the extra named concept Design 1
   allows) plus the goal-eligibility leaf. Where the module boundary and the call graph disagreed,
   the helper moved to the module that keeps the import graph acyclic and is recorded below.

### Function → module table (re-derived at the slice head)

Every base function group is named below: 250 entries in all — the six Orchestration concept rows
228 (227 base groups plus the new `claim_run/3`) and the `Goals.Eligibility` row 22 (20 base groups
plus the new `candidate_task_query/2` and the `value/2` default arity).

| module | functions |
| --- | --- |
| `Orchestration.Registry` | native_harness_kind?/1, native_harness_kinds/0, enroll_machine/3, authenticate_machine/1, register_runtimes/4, heartbeat/4, heartbeat_runtime/3, machine_owns_runtime?/2, machine_owns_run?/2, machine_owns_command?/2, valid_runtime_specification?/1, valid_runtime_capabilities_schema?/2, legacy_contract_fallback?/2, goals_rollout_disabled?/0, valid_runtime_repository_resource?/1, ensure_runtime_repository_resource!/1, ensure_runtime_repository_binding_change_safe!/2, valid_boolean_capability?/2, valid_runtime_adapter_metadata?/2, runtime_metadata_present?/1, valid_runtime_metadata?/1, valid_registered_adapter?/4, adapter_matches_runtime_metadata?/2, valid_adapter_operations?/1, generic_adapter_operations_allowed?/2, native_capabilities_match_adapter?/3, legacy_supervisory_runtime?/1, nonempty_bounded_text?/2, digest/1, secure_compare/2, owned?/4, jsonb_compatible?/1, jsonb_key_compatible?/1, valid_active_run?/1, snapshot_for/2, nonempty_text?/1, map_key?/2 |
| `Orchestration.Assignment` | assignment_target/1, assign_one/1, assign_one_goal/3, assign_one_goal/4, assign_goal_candidate_page/4, try_assign_goal_candidate/4, try_lock_goal_assignment_chain/1, try_lock_goal_project/1, try_lock_goal/1, try_lock_work_item/1, try_lock_goal_task_item/1, try_lock_task/1, assign_one_legacy/3, assign_task_to_runtime!/5, runtime_has_capacity?/1, runtime_available_at?/2, next_assignable_legacy_task_and_runtime/1, runtime_matches_binding/0, next_goal_runtime/3, restrict_to_requested_session/3, restrict_to_handoff_source_machine/2, reserve_requested_session!/5, release_reserved_session!/2, mark_reserved_session_unavailable!/2, goal_runtime_matches?/3, native_goal_adapter_requirement/0, native_goal_resume_requirement/0, native_goal_handoff_requirement/0, native_goal_session_requirement/1, native_goal_adapter_capable?/2, strict_budget_capability_requirement/1, strict_budget_task?/1, runtime_matches_task?/2, assign_all/1, assign_all/2 |
| `Orchestration.Claims` | claim/3, claim_owner/1, claim_with_disposition/3, prepare_claim_in_transaction/4, commit_new_claim_in_transaction/2, prepare_claim_locked/5, commit_new_claim!/3, emit_claimed/1, renew_lease/3, ensure_fence!/5, ensure_static_fence!/4, lock_chain/2, notify_claim_task_locked/2, lock_goal_chain/1, ensure_goal_work_item_ownership!/3, ensure_current_goal_execution!/3, ensure_current_goal_execution_if_present!/3, ensure_requested_session_claim_binding!/4, replayed_claim?/5, lock_goal/1, lock_work_item/1, lock_machine/1, lock_task/1, lock_current_run/1, lock_runtime/1, lock_execution_chain/1, lease_duration_ms/1, valid_claim_request?/1, valid_fence?/1, insert_ignoring_conflict/3, persist_insert/1, persist_update/1, now/1, rollback/1, stamp_insert/2, stamp_update/2, emit/2, claim_run/3 (new) |
| `Orchestration.Transitions` | append_events/4, transition/6, transition_once!/9, apply_goal_pause_transition_in_transaction/6, expire/1, expire_run/2, expire_offline_runtimes/1, reconcile/4, reconcile_runtime/4, emit_transition/2, lock_transition_chain/2, ensure_transition_fence!/6, ensure_transition_static_fence!/5, ensure_terminal_fence!/5, within_terminal_grace?/2, ensure_terminal_static_fence!/3, valid_transition?/2, validate_transition!/2, transition_response/2, apply_supervisory_transition!/6, ensure_goal_pause_command_current!/4, ensure_current_goal_control_action!/4, goal_control_idempotency_key/3, pending_pause?/1, transition_origin_state/1, ensure_cancelled_transition_authority!/2, validate_decision_packet!/2, valid_decision?/1, validate_decision_choice!/4, task_attrs_for_transition/3, task_attrs_for_expiry/5, next_retry_generation/1, goal_attempt_limit_exhausted?/2, fail_task_for_attempt_limit!/2, attempt_limit_failure/1, lease_expired_failure/0, restore_expired_attempt_generation/2, enqueue_goal_task_terminal!/4, goal_task_terminal_receipt/2, settlement_job_args/1, notify_goal_task_terminal/3, ensure_expired_run_can_settle_task!/2, reject_pending_commands!/3, event_replay/2, event_body/1, valid_event?/1, valid_journal?/1, goal_owned_run?/1, ensure_current_goal_transition_authority!/4 |
| `Orchestration.Commands` | create_command/5, create_goal_control_command/8, create_goal_control_command_in_transaction/3, request_cancel/2, request_task_cancel/2, provide_input/4, retry_task/4, create_or_replay_locked_command!/7, create_new_command!/7, insert_command!/8, acknowledge_command/5, ensure_goal_control_task_ownership!/3, ensure_goal_control_run_envelope!/2, ensure_expected_generation!/2, ensure_expected_waiting!/3, normalize_command_payload/2, command_request_hash/3, command_request_body/3, ensure_command_replay!/4, lock_task_command/2, legacy_cancel_idempotency_key/1, valid_command_request?/2, retry_required_capabilities/2 |
| `Orchestration.TaskReads` | submit_task/3, fetch_task/1, fetch_run/1, fetch_runtime/1, runtime_snapshots/0, runtime_snapshot/1, fetch_command/1, task_snapshot/1, control_capabilities/4, adapter_operation_supported?/2, retryable_attempt?/1, retry_runtime_eligible_task_ids/1, retry_runtime_eligible?/1, retry_binding_row/1, retry_adapter_requirement/0, list_task_events/2, list_task_transitions/2, list_task_commands/2, task_timeline/2, runtime_read_models/1, runtime_read_model/1, add_active_run/2, finish_runtime_read_model/1, current_waiting_context/2, latest_task_command/1, with_history_task/4, history_limit/1, history_position/2, validate_history_position/1, validate_timeline_position/1, after_position/2, page_query/2, page_with_next/3, entry_position/1, task_timeline_events/3, task_timeline_transitions/3, task_timeline_commands/3, timeline_before_position/3, timeline_precedes?/2, work_snapshot/3, daemon_runtime_snapshot/3, share_runtime/1, share_task/1, pending_supervisory_transition?/1, normalize_task_attrs/1, valid_task_attrs?/1 |
| `Goals.Eligibility` | candidate_task_query/2, next_goal_task_candidate_page/2, goal_task_current?/2, planning_task?/1, goal_task_repository_resource_id/2, goal_work_item_owned?/3, goal_task_assignable?/2, validation_runtime_allowed?/2, validation_runtime_ids_for_task/1, validation_binding_runtime_ids/1, intersect_validation_binding_runtime_ids/1, valid_validation_binding?/1, canonical_runtime_uuid?/1, goal_subject_matches_resource?/2, runtime_matches_goal_resource?/3, runtime_allowed_for_task?/2, task_allowed_runtime_ids/1, handoff_source_machine_matches?/2, value/2, value/3, valid_uuid?/1, exact_map_keys?/2 |

## Implementation decisions the accepted plan leaves open (recorded)

1. **Expiry folds into `Transitions`** (O4 default). Deletion test (R0): removing the `Expiry`
   boundary and keeping one `Transitions` module does not re-spread an invariant and forces no
   caller to know more — the reaper (`expire/1`, `expire_run/2`, `expire_offline_runtimes/1`,
   `reconcile/4`) and the fenced transition writer share one lock chain, one terminal-settlement
   path (`ensure_expired_run_can_settle_task!/2`, `enqueue_goal_task_terminal!/4`,
   `notify_goal_task_terminal/3`) and one set of `Run`/`Task` writers, so a separate module would
   be a size-only split (R0 "locality split") with no size-policy reason: the folded module is
   1,191 L, under the 1,500 L cap. Outcome recorded in the evidence record.
2. **`native_goal_adapter_capable?/2` stays in Orchestration** (`Assignment`), because the scope
   pin defines "goal predicate" as exactly the Design 2 (a)-(c) set and (c) enumerates the four
   runtime-constraint families (validation runtime ids, goal resource match, allowed runtime ids,
   handoff source machine) — all four moved to `Goals.Eligibility`. The adapter/session-mode/
   budget capability checks (`native_goal_adapter_capable?/2`, `native_goal_*_requirement/0,1`,
   `strict_budget_task?/1`, `strict_budget_capability_requirement/1`) are runtime-metadata policy
   the assignment path and the claim recheck both need through `goal_runtime_matches?/3`;
   `goal_runtime_matches?/3` (the composition of (c) with those checks) also stays there and is
   one of the two qualified back-edge callees below.
3. **`goal_attempt_limit_exhausted?/2` stays in Orchestration** (`Transitions`, with
   `fail_task_for_attempt_limit!/2` and the retry path) exactly as the scope pin requires; it
   reads the immutable task fields, not goal policy.
4. **Helper placement keeps the concept graph acyclic** (`mix xref graph --format cycles --label
   compile-connected` at base: "No cycles found", so every new compile-connected cycle would be a
   regression):
   - `value/2,3`, `valid_uuid?/1`, `exact_map_keys?/2` → `Goals.Eligibility`, the leaf.
     Dominant-caller census over the placed functions: Eligibility 12 `value/3` call sites
     and the other map/UUID vocabulary reads are its own (b)/(c) predicates; putting the leaf's
     vocabulary anywhere else would make the leaf import an Orchestration module that transitively
     depends on it (Registry via Claims, TaskReads via Assignment), i.e. a new Goals ↔
     Orchestration compile-connected cycle — forbidden by the AD2 gate. `map_key?/2` moved to
     `Registry` in the apply stage (simplify finding F5): its only callers were `registry.ex`'s
     runtime-metadata validation (`valid_runtime_repository_resource?/1`,
     `valid_runtime_adapter_metadata?/2`, `runtime_metadata_present?/1`) and no Eligibility
     predicate reads it, so the leaf-vocabulary rationale did not hold for it; the move adds no
     leaf → Orchestration edge.
   - `now/1`, `rollback/1`, `emit/2`, `stamp_insert/2`, `stamp_update/2`, the single-row and
     shared locks (`lock_*`, `share_*`), the fence helpers (`ensure_fence!/5`,
     `ensure_static_fence!/4`, `lock_chain/2`, `lock_goal_chain/1`) and the write primitives
     (`persist_insert/1`, `persist_update/1`, `insert_ignoring_conflict/3`) → `Claims`
     (call-site census: rollback 28, now 2, and every writer imports Claims for the locks anyway).
   - JSON compat (`jsonb_compatible?/1`, `jsonb_key_compatible?/1`), active-run/journal
     validation, `nonempty_text?/1`, `legacy_supervisory_runtime?/1`, `digest/1`,
     `secure_compare/2`, `owned?/4`, `nonempty_bounded_text?/2` and `snapshot_for/2` (the
     heartbeat payload builder) → `Registry`, the module every other concept module can import
     without a back edge.
   - **Recorded waiver (fix stage 2026-11-22, review finding R-16)**: `nonempty_text?/1`
     (`registry.ex:310`) and `valid_boolean_capability?/2` (`registry.ex:450-455`) stay in
     `Registry` although no Registry function calls either. Call sites at the fix-stage tree
     (probe `fix1-callers.out`, `rg -n`): `nonempty_text?/1` — `transitions.ex` 958, 967, 969,
     970 (two calls on 970), `commands.ex` 731; `valid_boolean_capability?/2` —
     `task_reads.ex` 872. Design 1's dominant-caller default is waived here: `Registry` is the
     split's shared validation-vocabulary home (its other members already have cross-concept
     callers — `jsonb_compatible?/1` in `commands.ex`, `registry.ex`, `task_reads.ex`,
     `transitions.ex`; `legacy_supervisory_runtime?/1` in `assignment.ex`, `commands.ex`,
     `task_reads.ex`; `snapshot_for/2` in `registry.ex`, `task_reads.ex`, `transitions.ex`) and
     every concept module already imports it, so hosting them adds no edge; moving a generic
     predicate to whichever concept currently calls it most would re-couple the vocabulary to
     that concept. Deviation recorded rather than silently held.
   - `share_runtime/1`/`share_task/1` → `TaskReads` (read-side share locks: `daemon_runtime_snapshot/3`,
     `task_snapshot/1`, `reconcile_runtime/4` callers).
5. **Three qualified back-edge call sites** (recorded body deviations; everything else uses
   compile-time imports along the DAG): in `Claims.commit_new_claim!/3`,
   `goal_runtime_matches?(` → `Assignment.goal_runtime_matches?(` (line 2108 of the base) and
   `runtime_matches_task?(` → `Assignment.runtime_matches_task?(` (base line 2122); in the new
   seam, `TaskReads.task_snapshot(run.task_id)`. `Claims` therefore imports nothing from
   `Assignment`/`TaskReads`; the three calls are runtime dependencies. Every other module pair is
   one-directional (`Assignment → Registry → Claims`, `TaskReads → {Assignment,
   Claims, Registry}`, `Commands → Transitions → …`) and imports are emitted with the exact called
   arities, so no module needs a runtime call to a sibling except these three.
6. **`@type retry_binding` + its `@typedoc`** are kept in the facade (the public `@spec`s of
   `retry_runtime_eligible_task_ids/1` and `retry_runtime_eligible?/1` reference `retry_binding()`)
   and also in `TaskReads`, where the moved specs reference them; a two-line type declaration in
   two modules is recorded in `docs/TODO.md` as a drift risk.
7. **Compile-time constants used by two or more modules** (`@terminal_states`,
   `@terminal_targets`, `@supervisory_commands`, `@capacity_bearing_states`,
   `@native_harness_kinds`) are declared in each module that reads them (attributes cannot be
   imported); the copies are recorded in `docs/TODO.md` as a drift risk. `@retry_binding_types`
   moves inside its owning block (`retryable_attempt?/1`), so it is not re-declared. The apply
   stage removed the `@native_harness_kinds` copy: `Assignment.next_goal_runtime/3` consumes
   `Registry.native_harness_kinds/0` (simplify finding F8's fold); the remaining copies stay
   TODO-recorded.
8. **Module attributes travel with their dominant user** exactly as in 7; `@terminal_targets`,
   `@terminal_states` and `@supervisory_commands` are the only multi-module ones and
   `@decision_reasons`, `@terminal_grace_ms`, `@minimum_lease_duration_ms`,
   `@goal_assignment_scan_page_size`, `@validation_profile_name`, `@sha256`,
   `@default_history_limit`, `@max_history_limit`, `@timeline_source_ranks`,
   `@capacity_bearing_states` land in the single module that uses them.
9. **Comment policy**: every moved block keeps its line-level hazard comments verbatim; the
   non-verbatim trims are enumerated in the evidence record (each is a boilerplate/step-label or
   rationale paragraph whose "why" is already carried by the design docs, per policy 1-4).
10. **Controller**: the claim region (`:100-140`) is replaced by the seam call; the transition
    fork (`:180-200`) is untouched (O11). `alias SymmetryControl.Integrations.ProviderAccess` and
    `alias SymmetryControl.Repo` are removed because the change makes them unused (an unused alias
    fails `--warnings-as-errors`); no other header line changes.
11. **`candidate_task_query/2`**: the base `next_goal_task_candidate_page/2` body is split into a
    query builder (returning the composed `Ecto.Query`) and a one-line runner; the SQL is
    identical (proved by the base/head capture comparison) and the query count per candidate is
    unchanged.
12. **Docs**: `docs/design/core.md` and `docs/design/data.md` are *not* edited by this slice (the
    Tier-2 amendment shipped as PR #246 = HEAD, with the independent-review approvals recorded in
    the contract). `docs/TODO.md` gains: the duplicated `ContextSnapshot` read in the goal runtime
    match (Design 2, deferring the fix), the attribute copies of 7, and the `retry_binding` type
    copy of 6.

## Evidence plan

Instruments (all raw output in the evidence record):
1. **SQL identity + lock sequences**: a temporary `test/test_helper.exs` capture hook (Ecto
   `[:symmetry_control, :repo, :query]` telemetry → ETS → file), run for the same six probes in a
   base worktree (`66bc9dcb`, own DB partition `rf-w5-18-base`) and in this worktree: goal HTTP
   claim (`provider_action_controller_test.exs:500`), the lock pin (`:403`), legacy HTTP claim
   (`protocol_controller_test.exs:605`), direct `Goals.claim` (`orchestration_goal_admission_test.exs:1397`),
   direct `Orchestration.claim` (`orchestration_test.exs:10`) and goal assignment
   (`orchestration_goal_admission_test.exs:695`). Compare the full `(sql, params)` sequence per
   probe (SQL byte-identical; params normalised only for generated UUIDs/timestamps) and extract
   the lock statement order per path.
2. **Declaration census**: every base function group → exactly one module, no group lost or
   duplicated; `claim_run/3` and the controller seam are the only additions.
3. **Control-flow audit** (lesson 32) for the claim seam, the two controller regions and every
   function whose visibility changed, against the base text.
4. **Title multiset** over `control/test` before/after (equal except the new cases), **`wc -l`**
   table, **string/atom/telemetry byte-compare** over moved functions, **`mix credo --strict`**
   issue count on touched files (must not rise), **citations** (`scripts/ci/check-citation-existence.py`
   — this slice's docs cite `orchestration.ex` lines), **`git diff --check`**.
5. Gates (foreground, one mix run at a time): `mix format --check-formatted`;
   `MIX_ENV=test mix compile --warnings-as-errors`; `MIX_ENV=test mix xref graph --format cycles --label compile-connected`
   (no new compile-connected cycle vs base) with the plain cycles output recorded; the targeted
   suite, the CI e2e file and the full control suite; red mutants for every new test case.

## Out of scope (accepted plan + reasons)

`provider_access.ex` internals (W4-06 landed); Goals-side claim predicates in `run_fence.ex`
(W4-07/W5-14); removing the duplicated `ContextSnapshot` read (TODO entry); scheduler, reconciler
and notifier processes (callers keep using the facade); the controller's transition fork (O11);
any behaviour change; `docs/design/core.md`/`data.md` (PR #246 carries both).

## Residual risks / known limits

- Three moved function bodies carry a module-qualified call that the base wrote unqualified; the
  qualification is the mechanism that keeps the compile-connected module graph acyclic and is
  recorded per line.
- Attribute and type copies (three attribute copies after the apply stage's `@native_harness_kinds`
  fold, plus the `retry_binding` type copy) can drift; pinned by `docs/TODO.md` entries.
- The comment-policy trims change comment text inside moved functions; the evidence record lists
  each one so the reviewer can check that no rationale needed for maintenance was dropped.
- The base-side SQL/lock capture comes from the base worktree's test run, not from a frozen log;
  it is reproducible from the recorded command lines.

## Implementation record (2026-11-22, filled during execution)

Everything below is measured on the delivered working tree (uncommitted; base `66bc9dcb`).
Raw outputs: `docs/evidence/rf-w5-18-orchestration-concepts-2026-11-22.md` and the scratch
directory `<HOST-PATH>/.local/state/gdd/scratch/w5-18/` (not committed).

1. **Module set, slice head** (`wc -l`, pre-apply; the applied-revision table is in "Slice
   shape" and the evidence record): facade 266; `Registry` 669, `Assignment` 640, `Claims`
   607, `Transitions` 1188, `Commands` 808, `TaskReads` 909, `Goals.Eligibility` 307. No module
   over 1,500 L; `daemon_controller.ex` 529 L. Decision 1 (Expiry folded into `Transitions`)
   stands: the folded module stays under the cap and the R0 deletion test found no invariant or
   lock chain that only a separate `Expiry` module would hold.
2. **Claim seam, delivered**: `SymmetryControl.Orchestration.Claims.claim_run/3`
   (`run_id`, `request`, `opts`), returning the controller's exact
   `{:ok, {run, task, provider_access}} | {:error, reason}`; `lease_duration_ms` is resolved
   inside the seam from `opts`. The controller's claim region calls it and nothing else; its
   transition region is untouched (O11).
3. **Qualified back edges (decision 5, upheld)**: three call sites are module-qualified —
   `Assignment.goal_runtime_matches?/3` and `Assignment.runtime_matches_task?/2` inside
   `Claims.commit_new_claim!/3`, and `TaskReads.task_snapshot/1` in the seam. Every other module
   pair is a compile-time import along the DAG.
4. **`candidate_task_query/2` (decision 11, upheld)**: the base `next_goal_task_candidate_page/2`
   body is a query builder plus a one-line runner; the capture comparison shows the candidate SQL
   byte-identical and the per-candidate query count unchanged.
5. **Attribute copies, measured (supersedes decision 7's list; apply stage 2026-11-22)**: the
   multi-module compile-time constants are three — `@terminal_states` (Registry, Transitions),
   `@supervisory_commands` (Commands, Registry, Transitions), `@capacity_bearing_states`
   (Assignment, TaskReads). `@terminal_targets` is read by `Transitions` only.
   `@native_harness_kinds` is no longer copied: `Assignment.next_goal_runtime/3` consumes
   `Registry.native_harness_kinds/0` (simplify finding F8's fold; the SQL/lock probe set re-ran
   with identical SQL). `docs/TODO.md` records the three remaining copies as a drift risk.
6. **`retry_binding` type copy (decision 6, upheld)**: declared in the facade and in `TaskReads`;
   `docs/TODO.md` carries the drift risk.
7. **Comments, measured**: 13 comment paragraphs at base in `orchestration.ex`; 13 across the
   facade and the seven modules. Six paragraphs were re-wrapped/reworded to the 98-column format
   with the same "why" (listed pairwise in the evidence record), one roadmap paragraph
   (`# Pause application remains an Orchestration-owned transition until the separate Goal pause
   authority slice moves that boundary.`) was dropped under comment policy 4, and the
   controller's `# Scope locking precedes a new claim ...` paragraph moved verbatim into
   `Claims.claim_run/3` (`claims.ex:570-571`). No hazard comment lost its reason.
8. **SQL and lock evidence**: six base/head capture pairs (two controller paths, two direct-claim
   paths, one assignment path, one lock-pin path) — identical statement counts, identical lock
   sequences, identical non-INSERT SQL, identical canonical INSERT column→value mappings. Raw
   text differs in five changeset-driven INSERT statements by column order only (same column set,
   same bound values); each side re-run twice produced byte-identical output, so the order is a
   property of the compiled build, not of the run. Details and the comparator's normalisation
   rules are in the evidence record.
9. **Test files**: eight split files (two renamed in the apply stage from their `_b` placeholders
   to `orchestration_goal_assignment_test.exs` and `orchestration_goal_transitions_test.exs`,
   simplify finding F3; title multiset intact: 0 deleted) and seven
   new interface files with 66 cases (+64 at the slice head, +1 by the `map_key?/2` case of F5,
   +1 by the fix stage's R-21 case); the largest file is 1,280 L. Six warnings the split left
   behind (two now-unused helpers, three unused aliases, two never-used default arguments) were
   removed; the suite runs warning-free.
10. **`mix credo --strict`, measured (deviation from the evidence plan's "must not rise")**: slice
    head 553 issues (5 warnings / 362 refactoring / 95 readability / 91 design; 4,465 mods/funs);
    applied revision 552 (5 / 362 / 93 / 92; 4,516) against base 500 (5 / 357 / 82 / 56; 4,323).
    Attribution over credo's own listing: the base
    `orchestration.ex` carried 68 findings and the facade now carries 2, while the seven concept
    modules carry 76 — the same function-level complexity/depth findings the god module already
    had, now attributed per module — and the split leaves 7 duplicate-code items across the split
    test files, which the contract's verbatim split rule requires (each file keeps the helpers its
    cases use). The only new class is alias ordering in the new alias blocks; it was left in place
    because reordering aliases would invalidate the recorded gate run for a cosmetic `--strict`
    suggestion. Raw output: `gates/credo-head.out`, `gates/apply-credo.out`,
    `gates/credo-base.out`.

13. **Apply stage (2026-11-22, simplify findings F1–F9 / D1–D9)**: the accepted findings were
    applied inside the owned files — 122 helpers back to `defp` (F1), the `_b` split-file renames
    (F3), the facade parameter names and the seven `@doc` blocks (F4/F6), `map_key?/2` to
    `Registry` (F5), `Claims.static_fence_matches?/3` (F7), the `@native_harness_kinds` fold (F8)
    and the `RunEvent` alias (D7) — with the record corrections of D1/D3–D9; F2, F8's test half and
    F9 were rejected for needing artifacts outside the contract's owned set. The SQL/lock probes,
    the red-mutant batch (65/65, 0 UNKILLED) and the AST token audit (242/247 token-identical, the
    five recorded deviations) re-ran at the applied revision; the committed head's two rotted
    `CLOSED-TODO.md` citations were pinned per the citation gate's prescription. Numbers:
    evidence record sections 2, 3, 7, 8, 12.
14. **Fix stage (2026-11-22, review findings R-01–R-25)**: `Claims.claim_run/3`'s `@spec` third
    element gained `| nil` (R-01); `Assignment.assign_all/2` returned to `defp` (R-15, restoring
    the base visibility); the seam gained one case pinning "no claimed event unless the claim
    committed" (R-21) with red mutant `m-c6`; the five split files' second `@now` gained a
    shadowing-hazard comment (R-19); the plan/evidence corrections R-02–R-14, R-17, R-18 and the
    R-16 waiver above. R-20, R-23, R-25 are no-action findings; R-22 needs a conductor-side
    contract note (O11 narrows clause 3's `claim_owner` sentence to the claim path) and R-24's
    `docs/evidence` path is the repo convention that stands for clause 2's `answer.md`. Numbers:
    evidence record section 13.
