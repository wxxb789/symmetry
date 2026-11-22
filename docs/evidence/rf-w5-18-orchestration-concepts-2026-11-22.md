# rf-w5-18 orchestration concepts — evidence record (2026-11-22)

Subject: `control/lib/symmetry_control/orchestration.ex` (4,731 L god module) becomes a facade over
`Orchestration.{Registry,Assignment,Claims,Transitions,Commands,TaskReads}` plus
`Goals.Eligibility`, and the HTTP controller's claim region calls one claim seam. Contract:
`<HOST-PATH>/.local/state/gdd/contracts/goal-rf-w5-18-orchestration-concepts.md`, sha256
`3d6bed26be1ec8ac712f126e833800c6a2d7a831c659dc01792eda7f699442f6`, including the four conductor
decision sections (arch-decision-2, its open-item rulings, the verification amendments, and the
2026-11-22 Tier-2 approval/scope pin). Base revision `66bc9dcb5bc5a2012f16480dbb4c6e13e4f34283` for
every coordinate below; the slice head is that same revision because nothing is committed — the
gates below ran on the working tree described in section 1.

Plan: `docs/plans/2026-11-22-rf-w5-18-orchestration-concepts-plan.md` (function → module table,
recorded decisions, implementation record). Worktrees: this one
(`Q:/repos/wt-w5-18-orchestration-concepts/symmetry`, branch
`refactor/rf-w5-18-orchestration-concepts`) and the disposable base checkout
(`Q:/repos/wt-w5-18-base/symmetry`, `66bc9dcb`). Raw instrument output lives in
`<HOST-PATH>/.local/state/gdd/scratch/w5-18/` (scratch, not committed); every number below names
its file.

Subject identity (sha256 of delivered working-tree files):

| file | sha256 |
|---|---|
| `control/lib/symmetry_control/orchestration/registry.ex` | `b54e4deb52f1f9f9d5e75b8c1ab93483d5fe2f879713e1188106999a69453366` |
| `control/lib/symmetry_control/orchestration/assignment.ex` | `7fce97be1bd74487b7b8672d18d61c99ba1efc0c98a10923e1df52f38aae0184` |
| `control/lib/symmetry_control/orchestration/claims.ex` | `09d42a1af140b9f3dde49f084893368c4978f02c51b99362fc83ec029f583679` |
| `control/lib/symmetry_control/orchestration/transitions.ex` | `288052b07f080fe3aa6be620e9db81f21499a581ddad53df478fe65b9e36a05d` |
| `control/lib/symmetry_control/orchestration/commands.ex` | `c60a84fb5fccc75a95add9053200ce72a5b82479ca2d911207b8fb09dab9d753` |
| `control/lib/symmetry_control/orchestration/task_reads.ex` | `68dd563a273a3850c3819883105972c1c774163288387ac501ad7e1e6564c5b3` |
| `control/lib/symmetry_control/goals/eligibility.ex` | `8ae06aed5a33305f84856fb5bf3274aba8c6fef8d7bc92a7f948629470a6481e` |
| `control/test/symmetry_control/orchestration/registry_test.exs` | `83e29810520c204f7c78d2f26894468d66f30e86d42806b7e53c45744d4519d5` |
| `control/test/symmetry_control/orchestration/assignment_test.exs` | `6e2b33db979ecc6170c7c394692e46ac3c1ce66c9ebef432e0bf47d184ecb40c` |
| `control/test/symmetry_control/orchestration/claims_test.exs` | `181e30cdfe61608a5535abe4d164f08bb54903071b29b1c90c884c8a88c8b9f1` |
| `control/test/symmetry_control/orchestration/transitions_test.exs` | `7ac3e3c6393105804f0f955dd22ea107bd9927e8181d7f0f20d1d6cdbeb26f12` |
| `control/test/symmetry_control/orchestration/commands_test.exs` | `f351431d6aa09dceaa98d479c68224faf93b35e97da0a0c4a97f1aacbf7fbcdb` |
| `control/test/symmetry_control/orchestration/task_reads_test.exs` | `66a5e861baed3a0d221b32e6a04557c99cc867f8d2c39d88d02a5594cd77e1ee` |
| `control/test/symmetry_control/goals/eligibility_test.exs` | `b4c55c080586e80c36cac350d5f52152c12143c10a161eeab936688b7d1fe896` |

(`hashes-owner.txt` in the scratch directory; the test-file hashes were re-verified after the
red-mutant batch, section 7.)

## 1. Deliverable shape (files, counts)

| file | status | lines | note |
|---|---|---:|---|
| `control/lib/symmetry_control/orchestration.ex` | edited | 4,731 → 266 | facade; 51 `defdelegate`, public API unchanged |
| `control/lib/symmetry_control/orchestration/registry.ex` | new | 669 | enrollment/auth, runtime registration, heartbeat, validation vocabulary |
| `control/lib/symmetry_control/orchestration/assignment.ex` | new | 640 | legacy + goal candidate scans, runtime choice, capacity, session reservation |
| `control/lib/symmetry_control/orchestration/claims.ex` | new | 607 | claim/lease/fence, locks, write primitives, `claim_run/3` seam |
| `control/lib/symmetry_control/orchestration/transitions.ex` | new | 1,188 | fenced transitions, run events, expiry/reconcile (Expiry folded) |
| `control/lib/symmetry_control/orchestration/commands.ex` | new | 808 | command create/ack/cancel/input/retry, goal-control helpers |
| `control/lib/symmetry_control/orchestration/task_reads.ex` | new | 909 | task submit/retry, snapshots, timelines |
| `control/lib/symmetry_control/goals/eligibility.ex` | new | 307 | goal candidate query + locked-row recheck + runtime constraints |
| `control/lib/symmetry_control_web/controllers/daemon_controller.ex` | edited | 586 → 529 | claim region replaced by the seam; transition region untouched |
| `control/test/symmetry_control/orchestration_{registry,assignment,assignment_b,claims,transitions,transitions_b,commands,task_reads}_test.exs` | new (split) | 1,256 / 1,139 / 1,087 / 1,278 / 1,243 / 1,183 / 990 / 875 | base titles verbatim, multiset preserved |
| `control/test/symmetry_control/orchestration_{registry,assignment,claims,transitions,commands,task_reads}_test.exs` | new | 142 / 171 / 356 / 329 / 335 / 156 | 64 new interface cases |
| `control/test/symmetry_control/goals/eligibility_test.exs` | new | 451 | truth table + seeded query |
| `docs/plans/2026-11-22-rf-w5-18-orchestration-concepts-plan.md` | new | 298 | plan + implementation record |
| `docs/evidence/rf-w5-18-orchestration-concepts-2026-11-22.md` | new | — | this record (own line count not self-measured) |
| `docs/TODO.md` | edited | +3 entries | duplicated `ContextSnapshot` read, four attribute copies, `retry_binding` type copy |

No file exceeds the 1,500 L cap (`wc -l`, section 8). The two base test files
(`orchestration_test.exs` 2,575 L, `orchestration_goal_admission_test.exs` 4,051 L) are deleted
because their content moved into the eight split files with the title multiset preserved
(section 6), which is the contract's split rule, not a deletion of coverage.

## 2. Interface, callers, census

- **Facade API preserved** (clause 3's failure control): every public function of the base module
  exists with the same name/arity and return shape; the facade holds 51 `defdelegate` lines plus
  the two `@spec`/`@type` declarations the public specs reference. `audit2.out` compares the base
  module's parsed function groups with the head modules': all 247 base groups are present and 245
  of them are token-identical; the two recorded deviations are
  (a) `commit_new_claim!/3`, where `goal_runtime_matches?(...)` and `runtime_matches_task?(...)`
  become `Assignment`-qualified calls, and (b) `next_goal_task_candidate_page/2`, split into
  `candidate_task_query/2` plus a one-line runner. The head has 253 groups = 247 + six: the query
  builder `candidate_task_query/2`, the seam `claim_run/3` with `claim_run_result/1`, and the three
  helpers the controller's claim region used to own (`claim_in_transaction/4`,
  `claim_with_owner/4`, `provider_access_for_claim/4`), which moved into `Claims`.
- **Public API delta: zero** (source-derived, `facade-api-check` output in the scratch directory):
  the base module declares 75 distinct public `(name, arity)` pairs (401 `def`/`defdelegate`
  occurrences including multi-clause definitions); the facade exposes exactly the same 75 through
  51 `defdelegate` lines, with 0 missing and 0 extra. The contract's Design-3 exception (the
  controller-only fork helpers) was not needed: those helpers are private, so removing them
  changed no public name. `Claims.claim_run/3` is the only public addition, and it lives in
  `Claims`, not in the facade. Helpers a sibling imports are `@doc false def` (76 across the seven
  modules: Registry 9, Assignment 5, Claims 26, Transitions 12, Commands 2, TaskReads 5,
  Eligibility 17).
- **Claim seam**: `Claims.claim_run/3` performs the base order — `claim_owner/1` read outside the
  transaction, `Repo.transaction`, `ProviderAccess.lock_claim_scope/1` first, owner dispatch,
  `task_snapshot/1`, provider-access issue/persist or replay, commit, `emit_claimed/1` only for
  `:created` after commit — and returns the controller's exact tuple. The controller's claim
  region (`:105-135`) calls it; its transition region is byte-identical to base (O11). Function
  mapping (base `daemon_controller.ex` → head `claims.ex`): `claim_with_provider_access/2` +
  `claim_in_transaction/3` merge into `claim_run/3` with `lease_duration_ms` resolved from `opts`;
  `claim_provider_access_result/1` becomes `claim_run_result/1`; `claim_with_owner/4` and
  `provider_access_for_claim/4` moved with their bodies. A line-level diff of the four pairs
  (`seam-audit.py`) shows exactly: `provider_access_for_claim/4` identically (9 lines, 3 clauses);
  `claim_with_owner/4` identical except the legacy clause's `Orchestration.claim_with_disposition`
  becoming a local `claim_with_disposition`; `claim_provider_access_result/1` becoming
  `claim_run_result/1` with the same three-case logic as three clauses and
  `Orchestration.emit_claimed` becoming a local `emit_claimed`; `claim_in_transaction` gaining the
  `opts` argument (arity 3 → 4) so the inline `lease_duration_ms: lease_duration_ms()` moves to the
  seam, with `Orchestration.task_snapshot` → `TaskReads.task_snapshot`. The two controller claim
  routes are probes A and B in section 3 — byte-identical SQL and lock sequences on both.
  `daemon_controller.ex`'s diff is that region plus the two now-unused aliases
  (`ProviderAccess`, `Repo`).
- **Goal policy**: no goal predicate remains under `orchestration/`. The Design 2 (a)-(c) set lives
  in `Goals.Eligibility` (`candidate_task_query/2`, `goal_task_assignable?/2`,
  `goal_task_current?/2`, `planning_task?/1`, `goal_work_item_owned?/3`, `goal_runtime_matches?/3`,
  the four runtime-constraint predicates). The scope pin's exceptions stay in Orchestration:
  `goal_attempt_limit_exhausted?/2` (Transitions, retry path) and `native_goal_adapter_capable?/2`
  with the runtime-metadata capability checks (Assignment) — both recorded in the plan, decision 2.
  Evidence that the concepts only *consume* the policy: `assignment.ex:16` and `claims.ex:11`
  import the named `Goals.Eligibility` functions (`goal_task_repository_resource_id/2`,
  `task_allowed_runtime_ids/1`, `validation_runtime_ids_for_task/1`,
  `candidate_task_query/2`, …) and define none of them
  (`grep -rn "def goal_task_repository_resource_id\|def task_allowed_runtime_ids\|def
  validation_runtime_ids_for_task\|def candidate_task_query" lib/symmetry_control/orchestration/*.ex`
  returns nothing); the only composite that stays under `orchestration/` is
  `Assignment.goal_runtime_matches?/3` (`assignment.ex:521`), which composes the Eligibility
  constraints with the runtime-metadata capability checks the plan classified as
  Orchestration-owned. The controller keeps no owner branch on the claim path: `:goal`/`:legacy`
  appears once in `daemon_controller.ex` (`:187`, the untouched transition fork, O11).
- **Declaration census** (`census.py`): every base function group is owned by exactly one head
  module; zero groups lost, zero duplicated. Strings and single-quoted atoms compare 1:1
  (110/110 and 151/151, zero dropped, zero added); the added tokens are the new `claim_run/3`
  helpers' own atoms.
  Independently re-derived here (`census-functions.py`, 264 base `(name, primary arity)` keys):
  264/264 have exactly one owner among the seven modules, 51 of them also delegated from the
  facade, 0 duplicated, 0 unplaced. The two censuses use different arity models (the children's
  parser groups multi-clause/default-arg functions differently: 247 vs 264 keys), and they agree.
- **Control-flow audit (lesson 32)**: for every function whose visibility changed (`defp` → `def`,
  because a sibling imports it) and for the seam, the base body and the head body are the same
  token stream (`audit2.out`), so no branch order, guard, rollback or emit site moved. The
  three module-qualified back edges are the only call-shape changes; each is recorded with its base
  line in the plan (decision 5).

## 3. Generated SQL and lock sequences (Design 4 gate)

Instrument: a telemetry capture hook in `control/test/test_helper.exs`
(`[:symmetry_control, :repo, :query]` → ETS → `W518_CAPTURE_OUT` at `:after_suite`, one
base64 record per statement: SQL text plus `inspect(params)`), installed identically in both
worktrees, removed before delivery (`git status` shows the file back at its 2-line base content).
Six probes, each `mix test <file>:<line>` on the same test in both trees:

| probe | test | statements base/head | SQL text identical | canonical statements identical | locks identical |
|---|---|---:|---|---|---|
| A `A_http_goal_claim` | `provider_action_controller_test.exs:500` (goal claim, HTTP) | 105/105 | yes | yes | yes (26) |
| A-pin `A_lock_pin` | `provider_action_controller_test.exs:403` (lock-order pin) | 91/91 | yes | yes | yes (24) |
| B `B_http_legacy_claim` | `protocol_controller_test.exs:605` (legacy claim, HTTP) | 40/40 | yes | yes | yes (11) |
| C `C_goals_claim_direct` | `orchestration_goal_admission_test.exs:1397` → `orchestration_claims_test.exs:413` | 31/31 | 30/31 | yes | yes (11) |
| D `D_orchestration_claim_direct` | `orchestration_test.exs:10` → `orchestration_transitions_test.exs:8` | 32/32 | 31/32 | yes | yes (8) |
| assignment `assignment_goal` | `orchestration_goal_admission_test.exs:695` → `orchestration_assignment_test.exs:115` | 56/56 | 53/56 | yes | yes (21) |

Raw files: `capture-compare4.out` (per-probe verdicts), `capture-locks.out` (full lock lists per
probe), `cap-base/`, `cap-head/` (the captures), comparison scripts `compare2.py`, `compare4.py`.

- **Lock sequences** (each list is the head run; the base run is statement-for-statement identical,
  `capture-locks.out`), covering paths A (goal claim over HTTP), A-pin, B (legacy claim over HTTP),
  C (`Goals.claim` direct), D (`Orchestration.claim` direct) and assignment from the contract's
  k5 §1.2:
  - `A_http_goal_claim` (26 locks): machines FOR UPDATE → machines FOR UPDATE → runtimes FOR UPDATE → projects FOR UPDATE → external_connections FOR SHARE → external_connections FOR SHARE → projects FOR SHARE → project_resources FOR UPDATE → work_items FOR UPDATE → project_resources FOR UPDATE → work_items FOR UPDATE → work_items FOR UPDATE → tasks FOR UPDATE → tasks FOR SHARE → tasks FOR UPDATE SKIP LOCKED → project_resources FOR UPDATE → external_connections FOR UPDATE → tasks FOR UPDATE → runs FOR UPDATE → runtimes FOR UPDATE → goals FOR UPDATE → project_resources FOR UPDATE → external_connections FOR UPDATE → tasks FOR UPDATE → runs FOR UPDATE → tasks FOR SHARE
  - `A_lock_pin` (24 locks): machines FOR UPDATE → machines FOR UPDATE → runtimes FOR UPDATE → projects FOR UPDATE → external_connections FOR SHARE → external_connections FOR SHARE → projects FOR SHARE → project_resources FOR UPDATE → work_items FOR UPDATE → project_resources FOR UPDATE → work_items FOR UPDATE → work_items FOR UPDATE → tasks FOR UPDATE → tasks FOR SHARE → tasks FOR UPDATE SKIP LOCKED → project_resources FOR UPDATE → external_connections FOR UPDATE → tasks FOR UPDATE → runs FOR UPDATE → runtimes FOR UPDATE → goals FOR UPDATE → project_resources FOR UPDATE → external_connections FOR UPDATE → tasks FOR UPDATE
  - `B_http_legacy_claim` (11 locks): machines FOR UPDATE → machines FOR UPDATE → runtimes FOR UPDATE → tasks FOR UPDATE → tasks FOR SHARE → tasks FOR UPDATE SKIP LOCKED → tasks FOR UPDATE SKIP LOCKED → runtimes FOR SHARE → tasks FOR UPDATE → runs FOR UPDATE → runtimes FOR UPDATE
  - `C_goals_claim_direct` (11 locks): projects FOR UPDATE → machines FOR UPDATE → machines FOR UPDATE → project_resources FOR SHARE → runtimes FOR UPDATE → projects FOR SHARE → goals FOR UPDATE → work_items FOR UPDATE → tasks FOR UPDATE → runs FOR UPDATE → runtimes FOR UPDATE
  - `D_orchestration_claim_direct` (8 locks): machines FOR UPDATE → machines FOR UPDATE → runtimes FOR UPDATE → tasks FOR UPDATE → tasks FOR UPDATE SKIP LOCKED → tasks FOR UPDATE → runs FOR UPDATE → runtimes FOR UPDATE
  - `assignment_goal` (21 locks): machines FOR UPDATE → machines FOR UPDATE → runtimes FOR UPDATE → machines FOR UPDATE → machines FOR UPDATE → runtimes FOR UPDATE → machines FOR UPDATE → machines FOR UPDATE → runtimes FOR UPDATE → projects FOR SHARE SKIP LOCKED → goals FOR UPDATE SKIP LOCKED → work_items FOR UPDATE SKIP LOCKED → tasks FOR UPDATE SKIP LOCKED → runtimes FOR UPDATE SKIP LOCKED → tasks FOR UPDATE SKIP LOCKED → projects FOR SHARE → goals FOR UPDATE → work_items FOR UPDATE → tasks FOR UPDATE → runs FOR UPDATE → runtimes FOR UPDATE
  The `ProviderAccess.lock_claim_scope/1` lock stays ahead of the goal lock chain on the claim
  paths (`projects FOR SHARE`/`projects FOR UPDATE` before `goals FOR UPDATE`), which is the
  contract's R10 requirement; the assignment path keeps its `SKIP LOCKED` scan prefix.
- **Non-INSERT SQL**: byte-identical in every probe (statement text *and* normalised parameters,
  including the candidate scan, the runtime choice, the `claim_owner` read, the fence checks and
  every `UPDATE`).
- **INSERT statements**: identical column *sets* and identical canonical column→value mappings
  everywhere; the raw text differs in five statements — probe C `#10` (`runtimes`), probe D `#2`
  (`machines`), probe assignment `#6`, `#12`, `#17` (`runtimes`) — by column *order* only
  (`INSERT INTO "machines" (... "enrollment_idempotency_key","enrollment_request_hash",
  "enrollment_request_hash_version")` versus `(... "enrollment_request_hash",
  "enrollment_request_hash_version","enrollment_idempotency_key")`). The comparator pairs each
  column with its bound parameter by placeholder index, so the mapping equality proves the written
  row is the same; Postgres binds `INSERT` columns by name.
- **Cause and determinism**: those five statements are built by `insert_ignoring_conflict/3`
  (`Repo.insert_all(schema, [Map.put(changeset.changes, :id, _)], on_conflict: :nothing)`), whose
  column order is the map iteration order of `changeset.changes`; it is deterministic per compiled
  build: probe C ran twice per side — base `captures-base/C_goals_claim_direct.txt` (first capture
  round, line-based hook) versus `cap-base/C_goals_claim_direct.txt` (second round, base64 hook),
  31 statements with identical SQL text, and head `cap-head-c1` versus `cap-head-c2`, 31 statements
  byte-identical including the permuted INSERT. It differs between the trees even though the source
  text of the changeset call chain is token-identical, so the difference is a property of the
  compiled build (the same literal compiled into `orchestration.ex` versus `registry.ex`), not of
  the run: the same expression chain evaluated at runtime in both trees yields the same key order
  (`keyprobe.exs`, both sides: literal, changeset, stamped, merged and `Map.put(:id, _)` orders all
  equal). Recorded as the one text-level deviation from "SQL text unchanged", with the canonical
  statement comparison above as its replacement evidence.
- Comparator normalisation (needed because the fixtures generate fresh UUIDs and project keys each
  run): UUIDs → `<uuid>`, timestamps → `<ts>`, binary literals → `<bin>`, project keys `P[0-9A-F]+`
  → `P<n>`, integers ≥3 digits → `<int>`. Nothing else is rewritten; statement text is compared
  raw.

## 4. String, atom and comment census

- Strings/atoms: 1:1 (`census.py`, section 2). Telemetry events: the capture hook shows the same
  `[:symmetry_control, :orchestration, :run, :claimed]` event for the seam-created claim as for the
  base controller path (probe A/B run the assertion that pins it; the claims interface test
  asserts the event fires once for `:created` and not for a replay).
- Error/return atoms: the six probes' non-INSERT statements and the interface tests pin the
  returned atoms (`:replayed`, `:created`, `:idempotency_conflict`, `:ownership_lost`,
  `:goal_authority_required`, `:invalid_request`, `:not_found`, `:no_assignment`,
  `:state_conflict`, `:unauthenticated`, `:capacity_exhausted`).
- Comments: 13 paragraphs at base in `orchestration.ex`; 13 across the facade and seven modules,
  with seven removed/added pairs (`comment_census.py`):
  1. candidate-scan keyset paragraph — reworded to the 98-column wrap, same why;
  2. goal-less runtime eligibility paragraph — reworded, same why (the base's caller names
     `next_assignable_legacy_task_and_runtime/1` are no longer spelled out);
  3. handoff machine paragraph — reworded, same why;
  4. pause-transition authority paragraph — reworded, same why;
  5. "read ownership without a lock" paragraph — reworded, same why;
  6. pre-Goal contracts paragraph — reworded, same why;
  7. `# Pause application remains an Orchestration-owned transition until the separate Goal pause
     authority slice moves that boundary.` — dropped: roadmap prose, not a line-level hazard
     (comment policy 4).
  One paragraph is *added* to the lib set because it moved out of the controller verbatim:
  `# Scope locking precedes a new claim to preserve the established / # Goal -> resource lock
  order. A replay ignores a now-stale scope result.` now sits at
  `control/lib/symmetry_control/orchestration/claims.ex:565` and is gone from
  `daemon_controller.ex`. No hazard comment lost its reason.

## 5. Suite results (targeted)

- New + split tests, one run, `MIX_TEST_PARTITION=rf-w5-18`:
  `sh rf-w5-18-mix.sh test test/symmetry_control/orchestration/ test/symmetry_control/goals/eligibility_test.exs
  test/symmetry_control/orchestration_{registry,assignment,assignment_b,claims,transitions,transitions_b,commands,task_reads}_test.exs`
  → `Result: 189 passed`, exit 0, zero compiler warnings (`testrun4.out`). The 189 = 64 new cases +
  125 moved cases.
- Two interface-suite bugs of the workers' test files were found by this run and fixed here: the
  claims fixture inserted a goal revision whose `execution_policy` violated
  `goal_revisions_execution_policy_v1` (now the same policy map the split files use), and the
  affinity-loss case set a random `repository_resource_id`, which the DB trigger
  `runtime_repository_resource_requires_repository` rejects (now a real repository resource of the
  same project). Six warnings the split left behind (two unused helpers, three unused aliases, two
  unused default arguments) were removed.

## 6. Title multiset and line counts

- Base `test "` titles in `orchestration_test.exs` (39) + `orchestration_goal_admission_test.exs`
  (77): 116. Titles across the eight split files: 116. Multiset comparison (counters, not sets):
  0 missing, 0 extra, no rename — the split carried every title and body verbatim.
- The head suite total 189 = 116 (split files) + 64 (new interface files) + 9 (the pre-existing
  `orchestration/scheduler_test.exs` and `orchestration/capability_scheduling_test.exs`, untouched).
  The base run of the two original files was 116 passed (`base-warnings.out`), matching the split
  count. The +64 title delta over the whole tree is exactly the new cases.
- Tree-wide check: `git grep -c '^\s*test "' 66bc9dcb -- control/test` sums to **1,022** base test
  titles; the working tree holds **1,086** over 89 files — a delta of exactly +64, the new interface
  cases. Nothing else in `control/test` gained, lost or renamed a case, so clause 3's
  "weakened, renamed or deleted test" control holds tree-wide, not only in the split files.

## 7. Red-mutant records (clause 1, Design 5)

One deliberate mutant per new case. Each mutant is a minimal exact-string replacement in the module
under test (`mutants/mutations.jsonl`; 62 single-line, 2 two-line), applied by
`mutants/apply_mutation.py` with an assertion that the old string occurs exactly once, run as
`sh rf-w5-18-mix.sh test <that module's test file>` (`mutants/run_mutant.sh`, `timeout 900`), and
followed by restoring the module from `mutants/baseline/` and re-checking its sha256.

- 64 mutants, 65 runs: the final 64 plus one superseded first candidate for `m-r5` (exit 0, 0 kills
  — its mutation was absorbed by another guard, so it was replaced by the guidance-enum mutation and
  re-run; both rows are in `mutants/run-results.txt`, and `mutants.tsv` carries the final version).
- Every final run exited 2 and failed its intended case; the failure header
  (`<n>) test <title> (<Module>)`) and the `Result: n/m passed` line are in `mutants/mutant-<id>.log`.
- Kill distribution: 61 mutants kill exactly one case, 2 kill two (`m-tr4`, `m-e2`), 1 kills five
  (`m-e13`, the shared `value/valid_uuid?/map_key?/exact_map_keys?` vocabulary) — 70 case-kills over
  the 64 cases. **0 UNKILLED** (`mutants/coverage.tsv`).
- Per file: registry 8/8, assignment 10/10, claims 5/5, transitions 9/9, commands 8/8,
  task_reads 10/10, eligibility 14/14.
- Independent recheck for this record: the case titles parsed from every `mutant-<id>.log` with
  `^\s*[0-9]+\) test (.+?) \(([A-Za-z0-9_.]+)\)$` equal the titles recorded in `run-results.txt`
  for that mutant (3 rows differ only in the separator between multi-kill titles), their union is
  exactly the 64 `coverage.tsv` titles, and no coverage row names a mutant whose log does not fail
  that case.
- Restoration: `RESTORED-OK` on all 65 rows, and `sha256sum -c hashes-before-owner.txt` over the 7
  module files and 7 test files prints 14 × `: OK` (exit 0) after the batch, so the delivered tree
  is bit-identical to the pre-batch tree in the header table. No `*.bak`, `*.orig` or `*~` file was
  left in the worktree, and the batch wrote only under the scratch directory.
- Uninformative mutants removed rather than kept: the `m-r5` first candidate passed all 8 registry
  cases (`Result: 8 passed`, exit 0) and is superseded as described.

| specimen | module | mutation | run and killing case |
|---|---|---|---|
| `m-r1` | `registry.ex` | `Repo.get_by(Machine, token_digest: digest(token))` → `digest("machine-token")` | `Result: 7/8 passed`, kills `authenticate_machine resolves a fresh enrollment token and rejects unknown tokens` |
| `m-c1` | `claims.ex` | `emit_claimed(run)` → `:ok` inside `claim_run_result/1` | `Result: 4/5 passed`, kills `claim_run claims a legacy run without provider access` |
| `m-a1` | `assignment.ex` | the assignment changeset's `state: "assigned"` → `"claimed"` | `Result: 9/10 passed`, kills `assign_one assigns a queued legacy task and moves it to assigned` |
| `m-t1` | `task_reads.ex` | idempotency-hit branch `do: {task, :replayed}` → `{task, :created}` | `Result: 9/10 passed`, kills `submit_task creates a queued task and replays the same request` |
| `m-cm1` | `commands.ex` | `create_command` skips the replay lookup | `Result: 7/8 passed`, kills `create_command cancels queued work and replays the same idempotency key` |
| `m-tr1` | `transitions.ex` | the replay clause of `transition/6` returns `rollback(:idempotency_conflict)` | `Result: 8/9 passed`, kills `transition creates a transition and replays the same id without rewriting state` |
| `m-e1` | `eligibility.ex` | `planning_task?/1` also accepts plan tasks with a work item | `Result: 13/14 passed`, kills `planning_task? accepts only plan tasks without a work item or producer` |

## 8. Gates (exact commands and exit codes)

Runner: `sh <HOST-PATH>/.local/state/gdd/scratch/w5-18/rf-w5-18-mix.sh <mix args>` in this worktree
and `rf-w5-18-base-mix.sh` in the disposable base checkout; both set `MIX_ENV=test`, their own
`MIX_TEST_PARTITION` (`rf-w5-18` / `rf-w5-18-base`), the portable Elixir 1.20.4 / OTP 29.0.6
toolchain and the local PostgreSQL. Runs were serialized; raw logs in
`<HOST-PATH>/.local/state/gdd/scratch/w5-18/gates/`.

| gate | command | exit | result |
|---|---|---:|---|
| format | `mix format --check-formatted` | 0 | no output (`format.out`) |
| compile | `mix compile --warnings-as-errors` | 0 | `Compiling 1 file (.ex)`, no warnings (`compile-wae.out`) |
| xref, compile-connected | `mix xref graph --format cycles --label compile-connected` | 0 | `No cycles found` (`xref-cc.out`); base identical, exit 0 (`base-xref-cc.out`) |
| xref, plain cycles | `mix xref graph --format cycles` | 0 | head: 2 cycles — 47 modules (the goals ↔ orchestration runtime cycle that now contains the six concept modules) and the 4-module schema cycle; base: 3 cycles — 25 + 11 + 4 (`base-xref-plain.out`). The split merges base's 25- and 11-module runtime cycles into one, so no runtime cycle is added and the count falls; the contract's expected `Orchestration.Claims ↔ Goals` cycle is inside the 47-module list (`xref-plain.out`) |
| targeted suite | `mix test test/symmetry_control/orchestration/ test/symmetry_control/goals/eligibility_test.exs test/symmetry_control/orchestration_{registry,assignment,assignment_b,claims,transitions,transitions_b,commands,task_reads}_test.exs` | 0 | `Result: 189 passed` (`targeted.out`) |
| CI e2e file, default env | `mix test test/symmetry_control/provider_broker_e2e_test.exs` | 0 | `Result: 0 tests, 4 skipped` — the file carries `@moduletag skip: System.get_env("SYMMETRY_PROVIDER_BROKER_E2E") != "1"` (`e2e.out`) |
| CI e2e file, enabled | `SYMMETRY_PROVIDER_BROKER_E2E=1 mix test test/symmetry_control/provider_broker_e2e_test.exs` | 2 then 0 | first run `3/4 passed`: one failure, `DBConnection.ConnectionError` "could not checkout the connection owned by …" in `GitHub Issue reaches GitHub pull request, review, and Actions projection`; two re-runs `4 passed` each (`e2e-env.out`, `e2e-env-rerun1.out`, `e2e-env-rerun2.out`). Base with the same env: `4 passed`, exit 0 (`base-e2e-env.out`). Classified as a sandbox-connection flake, not a code difference — the file drives `Orchestration.heartbeat/4`, `assign_one/1` and `fetch_task/1`, i.e. the moved paths, and passes on re-run |
| full control suite | `mix test` | 2 | `Result: 1069/1075 passed, 21 skipped`, 6 failed (`full-control.out`). Four are `SymmetryControlWeb.PortalControllerTest` asset tests: this worktree has no `control/priv/static/portal` bundle (the directory is absent) because the CI control job downloads it (`actions/download-artifact@v8`, `name: portal-assets`, `path: control/priv/static/portal`, `.github/workflows/ci.yml` control job); the same four fail in the base checkout run alone (`base-portal.out`, `Result: 9/13 passed`). The other two are `SymmetryControl.Migrations.Goal0006MigrationTest` sandbox/DB-order flakes: re-running just that file passes (`failed-files-head.out`, `Result: 69/73 passed`, the four portal failures only). Not green in this environment, for causes that are not the change |
| base full suite (cross-check) | `rf-w5-18-base-mix.sh test` | not obtained | The detached run wrote `Running ExUnit with seed: 465308` and 53 test dots, then produced no further output for 15+ minutes with no exit line (stalled on the base partition database); it was killed and is superseded by the individual base reproductions above (`base-portal.out` 4 failures, `base-e2e-env.out` 4 passed, `base-xref-cc.out` / `base-xref-plain.out`). Raw partial output kept in `base-full-control.out` |
| credo `--strict` | `mix credo --strict` | 30 | head 553 issues (5 warnings / 362 refactoring / 95 readability / 91 design; 4,465 mods/funs), base 500 (5 / 357 / 82 / 56; 4,323). Deviation analysed in the plan's implementation record item 10 (`credo-head.out`, `credo-base.out`); not a contract gate |
| doc-citation gate | `python scripts/ci/check-citation-existence.py --base 66bc9dcb --head HEAD` | 0 | `no audited document added or modified … nothing to check` — the audited corpus (`docs/runbooks/**`, `docs/design/quality.md`, `docs/archive/CLOSED-TODO.md`) does not hold this slice's documents (`citations.out`); the record's own 18 `file:line` citations were verified by hand against the baseline copies (0 mismatches) |
| whitespace | `git diff --check` | 0 | 0 lines (`diff-check.out`) |
| DLP: host paths | `grep -rn "C:/Users" <delivered files>` | 1 (no matches) | 0 matches (`dlp-paths.out`) |
| DLP: backslash-u | `grep -rnF '\u' <delivered files>` | 1 (no matches) | 0 matches (`dlp-backslash-u.out`) |
| `wc -l` | section 1 | — | every file ≤ 1,500 L (`wc-l.txt`) |
| red mutants | section 7 | — | 64/64 cases killed, 0 UNKILLED (`mutants/`) |

## 9. Contract clause mapping

| clause | evidence |
|---|---|
| 1 — facade ≤ 1,500 L over the named concepts; no goal predicate under `orchestration/`; controller off the `:goal \| :legacy` fork; byte-identical SQL/locks/transactions/atoms/telemetry; new interface tests green with red mutants; split with preserved title multiset; targeted, e2e and full suites green | sections 1, 2, 3, 5, 6, 7, 8 |
| 2 — judging artifact with the named tables | this record (function → module table in the plan, SQL and lock tables in section 3, cycles and declaration outputs in section 8, comment census in section 4, title multiset in section 6, `wc -l` in section 1 and 8, red mutants in section 7, suite outputs in sections 5 and 8) |
| 3 — failure control | sections 2 (declaration census, control-flow audit), 3 (SQL/locks), 4 (strings/comments), 8 (xref cycles); no deleted or renamed test (section 6) |
| 4 — out of scope respected | `provider_access.ex`, `goals.ex`, `run_fence.ex`, scheduler/reconciler/notifier untouched (`git status` section 8); the duplicated `ContextSnapshot` read recorded in `docs/TODO.md`; `core.md`/`data.md` untouched |
| 5 — evidence plan | sections 3-8 |
| 6 — mergeable alone | no behaviour change (sections 3, 5, 8); dependencies #189/#184 merged at `66bc9dcb` |

## 10. Residual risks and limits

- The five INSERT statements' column order differs from base (section 3). Semantics are proven
  identical by the canonical comparison, but a reviewer comparing raw SQL text will see it; the
  per-side re-run evidence is in the scratch directory, not in a committed log.
- The base-side captures come from the base worktree's runs, reproduced by the recorded commands,
  not from a frozen artifact.
- Four attribute copies and one type copy can drift; `docs/TODO.md` carries them.
- Comment paragraphs 1-6 were reworded while keeping their reason; a reviewer must confirm no
  maintenance-relevant rationale was lost (section 4 lists each pair).
- The mutant batch mutates module files temporarily; the restore is proven by sha256 equality
  (section 7).

## 11. Scoped simplification review

- The split's own simplification pass: `Registry` absorbed the shared vocabulary
  (`jsonb_compatible?/1`, `digest/1`, `secure_compare/2`, `value/*`, `snapshot_for/2`) so no module
  needs a helper bucket; `share_runtime/1`/`share_task/1` went to `TaskReads`, their dominant
  caller; `Commands` and the drafted `TaskCommands` are one module (O4); `Expiry` folded into
  `Transitions` (decision 1, deletion test in the plan).
- Nothing in the delivered tree is a wrapper-only layer: the facade's 51 `defdelegate` lines are
  the callers' contract, and no concept module re-exports another concept's function.
- Removed during implementation: the six split-leftover warnings (helpers, aliases, default
  arguments) and the controller's now-dead claim helpers
  (`claim_with_provider_access/2`, `claim_in_transaction/3`, `claim_with_owner/4`,
  `claim_provider_access_result/1`) that the seam replaced; the two aliases they used were
  removed with them.
- Kept deliberately: the three module-qualified back edges (an import would create a
  compile-connected cycle), the four attribute copies (Elixir cannot import attributes), the
  `retry_binding` type copy (the two modules' specs reference it), the controller's transition
  fork (O11).
