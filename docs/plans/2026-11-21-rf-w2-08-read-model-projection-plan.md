# rf-w2-08 read-model projection plan — one `%Projection{}` through `project/2`

Contract: `goal-rf-w2-08-read-model-projection.md` (GDD state root, 2026-11-21) with its binding
review amendments: `refactor/w2-contract-review-opus-1.md` P1-4 and
`refactor/w2-contract-review-ds-1.md` (`rf-w2-08` entry, note N2). Baseline for every coordinate in
this plan: `origin/main`/worktree HEAD `4dd374bdcdbe09a05287e77ad90faaa949fcddcd`. Owned files:
`control/lib/symmetry_control/goals/read_model.ex`,
`control/test/symmetry_control/goals_read_model_test.exs`, plus this plan file. Nothing else in the
repository is edited.

The slice removes the positional argument threading that F5 (`findings/control-core.md:258-278`)
recorded: `blockers/20` invoked once with a 20-argument literal, `completion_projection/12`, three
readiness helpers `/10` that re-spell the same 10-value sequence, and `task_settlement_entry/9`.
One `%Projection{}` value is built once in `project/2` and threaded through every pass that needs
the goal-revision context. Observable projection output must stay byte-identical.

## Public / API shape

- Unchanged: `fetch/2`, `project/2`, `events/2`, `graph/2`, `attention/1`, `context/3` and their
  return shapes. `project/2` keeps its `(goal, related)` signature; it still accepts a keyword list
  for `related` and still returns the same plain map. No exported symbol is added, renamed or
  removed, so `goals.ex:316/:423/:456/:8620` and every web controller call site are untouched.
- New, module-private shape: `SymmetryControl.Goals.ReadModel.Projection`, a nested `defstruct` in
  the owned file (no new file, no new public API). Fields hold the loaded records and the derived
  current-revision subsets:

  | Field | Meaning (identical to the expression it replaces) |
  | --- | --- |
  | `goal` | the goal struct/map, after `map_value(goal, :goal, goal)` unwrapping |
  | `current_revision` | `map_value(goal, :current_revision)` |
  | `revision` | `current_revision_record(revisions, current_revision)` |
  | `work_items`, `dependencies`, `outcomes`, `tasks`, `runs` | the unfiltered `records/3` loads |
  | `decisions`, `evidence`, `reservations`, `usage`, `runtimes`, `external_waits` | direct `records/3` loads, unfiltered (every reader filters itself, as before) |
  | `automatic_admission_blocked_events`, `automatic_reconciliation_deferred_events`, `task_settled_events` | direct `records/3` loads with their existing fallbacks |
  | `current_work_items`, `current_dependencies`, `current_outcomes` | `current_revision_*` subsets |
  | `current_work_item_ids` | the `MapSet` of current work-item ids (history assembly) |
  | `current_tasks`, `current_runs` | `current_goal_tasks/3` and the runs of those tasks |
  | `accepted` | `accepted_outcomes(current_outcomes)`, the by-work-item-id map |
  | `current_work_items_by_id`, `tasks_by_id`, `runs_by_id` | the by-id maps the settlement receipt pass built locally (over `current_work_items`, all tasks, all runs); the `current_*` prefix marks the one map over a filtered subset |
  | `settled_receipts` | `current_task_settlements(projection)` |
  | `execution`, `graph` | `execution_by_work_item/4` and `graph_projection/4`, computed once |

  `%Projection{}` is a context carrier, not a rename of a 20-tuple: the 20-positional call shape
  and every `/10`, `/12` re-spelling disappears (see the function table below), and each field is
  read by name (`projection.runs`) at the point of use.

## Function shapes: before → after

Applied rule: a private function **on the argument-threading chain** takes `projection` instead when
it today receives three or more projection-resident values threaded down from `project/2` through
another converted function, keeping its genuinely per-row/per-candidate arguments
(the settlement-receipt chain keeps `projection` first for the same reason). Deliberately **not**
converted, although each also takes three or more projection-resident values: the three pure
builders `graph_projection/4`, `execution_by_work_item/4` and `planning_execution_projection/3` —
each is called once from `build_projection/2`, takes plain lists and returns one projection
fragment, so swapping its arguments for `projection` would add coupling without removing a
threaded call shape. Functions below the
threshold keep their shape and read `projection.<field>` at the call site: `waiting_decision_blocker/2`,
`stale_context_blocker/2`, `automatic_execution?/1`, `automatic_admission_blocker_replaced?/5`,
`current_runtime_run?/2`, `latest_planning_task_id/2`, `planning_task?/2`, `matching_receipt_field/3`,
`disposition_required_blocker?/2`, `settlement_attention?/4`, `effective_integration_item_ids/1`,
`marked_integration_item_ids/1`, `integration_outcome_required_details/2`, `final_acceptance_authority/1`,
`valid_digest?/1`, `valid_subject_evidence?/1`, `goal_predicate_matches_evidence?/2`,
`dependency_closure/2`.

| Function | Before | After |
| --- | --- | --- |
| `blockers` | `blockers/20` (goal, revision, work_items, dependencies, outcomes, decisions, tasks, runs, settled_receipts, evidence, reservations, usage, runtimes, external_waits, automatic_admission_blocked_events, automatic_reconciliation_deferred_events, current_revision, accepted, all_tasks, all_runs) | `blockers/1` |
| `completion_projection` | `/12` (10-value readiness sequence + outcomes + **unused** `_blockers`) | `/1` |
| `final_acceptance_readiness_blocker` | `/10` | `/1` |
| `completion_readiness_blocker` | `/10` | `/1` |
| `final_acceptance_ready?` | `/10` | `/1` |
| `completion_readiness_candidates` | `/7` | `/1` |
| `completion_readiness_gate?` | `/6` | `/2` (projection, tasks — see the two-call-site note) |
| `integration_outcome_required_blocker` | `/6` | `/1` |
| `current_task_settlements` | `/7` (goal, work_items, outcomes, tasks, runs, events, current_revision) | `/1` |
| `current_task_settlement_entry` | `/7` (event, goal_id, current_revision, 3 by-id maps, accepted) | `/2` (projection, event) |
| `task_settlement_entry` | `/9` | `/6` (projection, event, response, task, run, settlement) |
| `work_item_task_settlement_entry` | `/8` | `/6` (projection, event, response, task, run, settlement) |
| `settlement_entry` | `/8` | `/8` (projection replaces `current_revision`; per-row values stay) |
| `settlement_attention` | `/4` | `/1` |
| `waiting_dependency_blocker` | `/4` | `/1` |
| `waiting_integration_dependency_rows` | `/3` | `/1` |
| `automatic_baseline_missing_blocker`, `automatic_baseline_dependency_blocker` | `/5` each | `/1` each |
| `unstarted_current_item?` | `/4` | `/2` (projection, item) |
| `validation_failed_blocker` | `/3` | `/1` |
| `runtime_unavailable_blocker` | `/4` | `/1` |
| `automatic_admission_blocked_blocker` | `/6` | `/1` |
| `automatic_admission_blocked_event_relevant?` | `/6` | `/2` (projection, event) |
| `automatic_reconciliation_deferred_blocker` | `/3` | `/1` |
| `unsupported_external_check_blocker` | `/3` | `/1` |
| `waiting_external_blocker` | `/4` | `/1` |
| `budget_blocker` | `/5` | `/1` |
| `effective_usage_summary` | `/3` | `/1` |
| `accounting_projection` | `/4` | `/1` |
| `integration_candidate_dependencies_satisfied?` | `/4` | `/2` (projection, subject_hash) |
| `integration_item_dependencies_satisfied?` | `/4` | `/2` (projection, work_item_id) |
| `rejected_completion_subjects` | `/3` | `/1` |
| `accepted_integration_outcomes` | `/2` | `/1` |
| `completion_relevant_item_ids` | `/3` (+ default arg) | `/2` (projection, integration_work_item_id) |
| `dependencies_satisfied?` | `/3` | `/2` (projection, relevant_ids) |
| `required_goal_predicates_satisfied?` | `/7` | `/3` (projection, authority, subject_hash) |
| `unmet_required_goal_evidence_predicates` | `/6` | `/2` (projection, subject_hash) |
| `eligible_goal_evidence` | `/4` | `/1` |
| `allowed_actions` | `/7` | `/3` (state, projection, completion) — the five state clauses and their match semantics are preserved |
| `pending_planning_task?` | `/3` | `/1` |
| `plan_decision_pending_acceptance?` | `/2` | `/1` |
| `item_projection` | `/5` | `/2` (projection, item) |

42 private functions collapse (the 42 names in the 41 rows above; one row holds two names); the
`project/2` → `fetch/1` → `graph/1` → `attention/1` surface keeps its
signature and its output.

`project/2` becomes: `build_projection/2` (loads, derives, fills `%Projection{}` including
`settled_receipts`, `execution`, `graph`) → `blockers/1`, `settlement_attention/1`,
`completion_projection/1`, then the same public map assembled from `projection.<field>` reads.

## Edge-case semantics the contract demands

- **Byte-identical output.** No expression's inputs change: every projection field is bound to the
  exact expression that was previously passed positionally, in the same order, and each converted
  function reads the same value it received before. The one expression replaced by an equivalent
  is `current_task_settlements`' local `accepted = accepted_outcomes(outcomes)`: `outcomes` there
  is already the current-revision outcome list, so the value equals the projection's `accepted`
  built from the same list. Everything else is a pure move of a value into a named field.
- **`outcomes` shadowing.** Inside `blockers/20` the parameter named `outcomes` was the
  *current-revision* list; `tasks`/`runs` were the *current* sets while `all_tasks`/`all_runs` were
  the unfiltered sets. The struct names keep that distinction explicitly (`outcomes`/`tasks`/`runs`
  vs `all_tasks`/`all_runs`); no call site may swap them.
- **Unused argument dropped.** `completion_projection`'s `_blockers` parameter was dead; it is
  removed rather than carried into the struct, and `completion` is still computed independently of
  `blockers`.
- **The current/all task distinction is load-bearing per call site.** Before the refactor
  `completion_readiness_gate?` received *unfiltered* tasks through
  `blockers → final_acceptance_readiness_blocker → completion_readiness_blocker →
  completion_readiness_candidates`, but *current-revision* tasks through
  `blockers → integration_outcome_required_blocker` (the two calls were spelled with different
  locals of the same name). The converted `completion_readiness_gate?(projection, tasks)` therefore
  keeps the task list explicit and each call site passes its own list (`projection.tasks` vs
  `projection.current_tasks`), with a comment at the candidates call site. The struct stores the
  unfiltered list as `tasks`/`runs` and the filtered subsets as `current_tasks`/`current_runs`, so a
  later edit cannot silently swap them.
- **Nil/absent input semantics unchanged.** `map_value`/`records`/`normalize_records` are not
  touched, so a goal map without `state`, an unloaded association, `nil` related values and
  list-shaped `related` (the second `project/2` clause) behave exactly as before.
- **`allowed_actions` clause matching.** The five clauses keep `state` as the first argument and
  therefore keep their clause selection; the context fields they read come from the projection.
- **Block/attention output is the judged surface.** `attention/1` derives its entries from the
  public projection (`attention_entry/1`), so the byte-identical `project/2` oracle covers the
  attention payload construction; `attention/1` itself is additionally dumped (see below).

## Evidence plan

Gates, all foreground, Elixir via the portable toolchain recipe adapted from
`scratch/retro-g2-mix.sh` for this worktree (`scratch/rf-w2-08-mix.sh`: Erlang 29.0.6 `erl.exe`,
Elixir 1.20.4, `MIX_ENV=test`, `MIX_TEST_PARTITION=rf-w2-08`, `PHX_SERVER`/`POSTGRES_PORT`/
`SYMMETRY_CONTRACTS_DIR` unset):

| Gate | Command | Result |
| --- | --- | --- |
| format | `mix format --check-formatted` | exit 0 |
| compile | `mix compile --warnings-as-errors` | exit 0, no warnings |
| targeted suite | `mix test test/symmetry_control/goals_read_model_test.exs` | exit 0, `Result: 51 passed` (apply stage, head `99cde951…`; the implement stage ran the same command at head `e254f025…` with `Result: 50 passed`) |
| full suite (conductor gate) | `mix test` (own `MIX_TEST_PARTITION=rf-w2-08`) | implement stage, head `e254f025…`: exit 2, `938/942 passed, 21 skipped, 4 failed`; all 4 are the `:portal_assets` cases of `SymmetryControlWeb.PortalControllerTest`, failing in `ConnCase` setup because the gitignored frontend bundle is not staged in this fresh worktree (the failure text itself documents `mix test --exclude portal_assets` as the host remedy and that CI stages the bundle) |
| full suite (apply-stage re-run) | `mix test` (own `MIX_TEST_PARTITION=rf-w2-08`) | apply stage, head `99cde951…`: exit 2, `951/955 passed, 21 skipped, 4 failed`; the same four `:portal_assets` prerequisite cases and no other failure (the run's totals grew from 942 to 955 because `bda93b11` — main's goal-schema validators — is now merged into this branch); the `AddOmoHarnessKindMigrationTest` connection-pool flake did not recur |
| full suite, documented exclusion | `mix test --exclude portal_assets` | implement stage, head `e254f025…`: exit 2, `937/938 passed, 21 skipped, 4 excluded, 1 failed`; the failure is `AddOmoHarnessKindMigrationTest` "rolling the OmO kind back…" with `DBConnection.ConnectionError … connection not available and request was dropped from queue after 4000ms`; the isolated re-run of that file is exit 0 with `3 passed`, and the same shared-PostgreSQL host-load artifact is already recorded at `docs/archive/CLOSED-TODO.md:998-1000` |
| whitespace | `git diff --check` | exit 0 |
| user-path scan | the contract's literal check `git diff origin/main...HEAD \| grep -i "users/"` returns **1** line, and that line is this row's own committed text (the quoted pattern self-matches; recorded by the 2026-11-21 simplify pass; the working-tree diff of the same literal returns 2 lines — this row's `-`/`+` pair). The substantive check uses a pattern that cannot match this row: `git diff origin/main...HEAD \| grep -icE "[Cc]:[\\/]Users"` | **0 hits** — no host path in the committed diff (0) nor in the working-tree diff (0); self-matches only: 1 committed / 2 worktree. `git status --short --untracked-files=all` names only `control/lib/symmetry_control/goals/read_model.ex`, `control/test/symmetry_control/goals_read_model_test.exs` and this plan file |

Every suite run above was run alone, never concurrently with the differential dumps or another full
suite (shared Postgres 100-connection cap); the three full-suite runs were sequential.

### Differential oracle (amendment P1-4, binding)

Throwaway and uncommitted, in the GDD scratch tree, no repository edit:

- `scratch/rf-w2-08-dump.exs` — installs a `:dbg` **return tracer** on
  `ReadModel.project/2` and `ReadModel.attention/*` and writes one line per returned value in call
  order to `RF_W2_08_DUMP`, then runs the named test files through ExUnit (`seed: 1`,
  `max_cases: 1`) and appends an `# ExUnit total=… failures=…` summary line after an explicit
  tracer flush handshake (a flush message the tracer acknowledges once it has written every earlier
  value; without it the summary line can overtake queued dump lines).
- Runner: `RF_W2_08_DUMP=… RF_W2_08_FILES="test/symmetry_control/goals_read_model_test.exs"
  sh scratch/rf-w2-08-mix.sh run scratch/rf-w2-08-dump.exs` (same for `goals_test.exs`). The dump
  and the read-model file swap live in `scratch/rf-w2-08/capture.sh`, which snapshots the head
  source by sha256, puts the base (`git show <baseline>:…`; the baseline `4dd374b…` and the
  pre-slice `bda93b11…` both carry the file as `d70d4dba…`) or head copy in place, runs the dumps,
  and restores the head copy. The apply stage re-captured through
  `scratch/rf-w2-08/apply-capture.sh` — same hook and same mechanism, with the base revision pinned
  to `bda93b11` because `HEAD` now carries the slice.
- The same hook runs unchanged on the base and head trees, so the comparison is
  implementation-independent: no source edit is needed to capture either side.
- **Canonical form.** Each captured value is rendered through one normalization before `inspect/2`:
  map and struct keys are sorted and structs become `{:struct, Module, sorted_pairs}`; every value,
  nesting and list order is otherwise untouched. This is forced by the oracle, not chosen for
  convenience: `inspect/2` prints map keys in the VM's internal order, and two runs of the *same*
  base code already differ on that order (implement-stage measurement: base run 1 vs base run 2
  differ on 89/89 read-model lines before this normalization, and on 51 of 1335 `goals_test.exs`
  lines; the apply stage widens the identical-code sample to six pairs per side and measures 79
  base / 86 head noise lines — see the noise floor in the results). Key order is not observable behaviour of an Elixir
  map, so normalizing it cannot mask a projection change; a changed, added or missing key, value,
  count, blocker, attention reason or list order still changes the canonical text.
- `goals_read_model_test.exs` builds every goal from hand-written maps with fixed ids and
  timestamps, so its canonical dump is compared **byte for byte** after normalization, and the
  base-vs-base control proves the oracle is stable (identical dumps).
- `goals_test.exs` is `DataCase`-backed; beyond key order its goals carry database-generated UUIDs,
  wall-clock `inserted_at`/`updated_at` values and `System.unique_integer`-derived work-item
  numbers, and some expectations resolve ties through unordered queries, so two base runs also
  differ on a few lines. Its canonical dump is additionally reduced with uuid/timestamp/digest/
  unique-number tokens → `<uuid>`/`<ts>`/`<digest>`/`<n>`, and head-vs-base differences are required
  to be confined to the lines where identical base code already differs (the noise floor), with an
  explicit count of the byte-identical lines.

Both dumps are captured **before** any edit (base) and again after the last edit (head): twice per
side at the implement stage, four times per side in the apply-stage re-measurement, and the counts
and diffs are recorded here and in the stage report.

**Sensitivity control (planted red).** After the clean head capture, one blocker line is deleted
from the head copy (`stale_context_blocker` dropped from `blockers/1`), the read-model dump is
retaken, and the diff against the clean head dump is required to be non-empty; the deletion is then
reverted and the source hash re-checked. A differential oracle that cannot see a removed blocker is
not evidence.

### Results (implement stage head `e254f025…`; re-measured by the apply stage at head `99cde951…`)

The numbers below are the **apply-stage re-measurement** of the applied head; the implement-stage
values are named where they differ. The accepted apply-stage findings (one duplicated by-id
derivation removed, one struct-field rename, one coverage pin) changed the source text, so the
implement-stage hashes, dump counts and stage-report numbers no longer bind the shipped revision
and are replaced here.

Dumps (GDD scratch tree, `scratch/rf-w2-08/`): implement stage `base-rm-1/2.dump`, `head-rm-1/2.dump`,
`base-goals-1/2.dump`, `head-goals-1/2.dump`; apply stage `ap-base-rm-1/2.dump`,
`ap-head-rm-1/2.dump`, `ap-base-goals-1..4.dump`, `ap-head-goals-1..4.dump`, each ending with
`# ExUnit total=… failures=0 excluded=0`.

**Tier A — `goals_read_model_test.exs`, canonical byte comparison.**

| Check | Command | Result |
| --- | --- | --- |
| determinism control (head) | `diff ap-head-rm-1.dump ap-head-rm-2.dump` | exit 0, 0 bytes (92 lines = 82 `project:` + 9 `attention:` + summary) |
| determinism control (base) | `diff ap-base-rm-1.dump ap-base-rm-2.dump` | exit 0, 0 bytes |
| **base vs head** | `diff ap-base-rm-1.dump ap-head-rm-1.dump` | **exit 0, 0 bytes** over 91 value lines (82 `project:`, 9 `attention:`) |

Both sides ran `51 passed` (`# ExUnit total=51 failures=0 excluded=0`); the base file sha256 is the
pre-slice content `d70d4dba…` (`bda93b11:` and `4dd374b:` agree) and the applied head file sha256 is
`99cde951…`. The two extra `project:` lines over the implement stage's 80 are the coverage pin's two
`project/2` calls; the implement stage measured the same 0-byte comparison over 89 value lines at
`e254f025…` with 50 tests.

**Tier B — `goals_test.exs`, noise-floor comparison (146 passed per run; four dumps per side).**

| Check | Lines |
| --- | --- |
| dump lines per run | 1335 (1332 `project:`, 2 `attention:`, 1 summary) |
| identical-code pairs per side | 6, from four dumps per side (1-2, 1-3, 1-4, 2-3, 2-4, 3-4) |
| noise base (union of the six base pairs) | 79 |
| noise head (union of the six head pairs) | 86 |
| noise union | 95 |
| base vs head (base-1 vs head-1) | 46 |
| base-vs-head lines inside the widened noise | 46 |
| **base-vs-head lines outside the noise** | **0** |
| identical across all eight dumps | 1240 |

Widening the identical-code sample from one pair per side to six removes the need for the
implement-stage outlier inspection: every base-vs-head difference also appears between two runs of
identical code on the same side. The implement stage's single-pair measurement (noise 51/45, union
64; base vs head 47, of which 31 inside the noise, 16 outliers = lines 436-443/910/1012/1199-1204)
is superseded; the two classes it classified are the classes the widened noise reproduces:

1. `purpose: "validate"` vs `"implement"` on the task a work item's execution projection selects
   (lines 436-443). The pick follows `latest_tasks_by_work_item/1`'s `{inserted_at, id}` order, and
   the two candidate tasks of that fixture carry the same timestamp, so the *random* UUID decides.
2. the order of two decisions with different `kind`/`goal_revision`/`state` inside the `decisions`
   list (lines 910, 1012, 1199-1204): the projection renders `records(related, :decisions, …)` in
   load order, and the read is unordered.

**Controlled differential probe (fixed inputs, base vs applied head).** `scratch/rf-w2-08/probe-scenario.exs`
builds one work item with two same-timestamp tasks (`implement`/`validate`, ids `task-a`/`task-b`)
and a two-decision list, runs `project/2`, and prints the pick and the decision order. The base and
the applied head run produce **byte-identical output** (`diff ap-probe-scenario-base.out
ap-probe-scenario-head.out` exit 0, 0 bytes): the pick follows the larger id for both id orders and
regardless of the input list order (`["validate"]` for `a=implement,b=validate`;
`["implement"]` for `a=validate,b=implement`), the decision list preserves the input order, and
`completion`, `blocker_reasons`, `attention_reasons` and `allowed_actions` match on every line.
The difference class is therefore input-driven.

**Flakiness reproduction on the real fixture (implement stage, head `e254f025…`).** The Tier B
outlier test was identified by bisecting
`only_test_ids` over the suite (7 runs, `scratch/rf-w2-08/bisect.sh`):
`"terminal producer, validation evidence, accepted outcome, and achieve close the Goal loop"`, 22
`project/2` calls per run. Running *that single test* six times per side
(`scratch/rf-w2-08/repeat.sh`) shows the same two variants, `13 validate / 4 implement` and
`0 validate / 15 implement`, occurring under both revisions:

| Side | runs (validate/implement counts) |
| --- | --- |
| head `e254f025…` | 13/4, 13/4, 13/4, 13/4, 13/4, **0/15** |
| base `d70d4dba…` | 0/15, 0/15, **13/4**, 13/4, **0/15**, 13/4 |

The same code produces both variants, so the Tier B line differences are test-data nondeterminism
(the fixture's task pick depends on generated UUIDs / unordered reads), not a projection change. That
this pre-existing flakiness is a *test-data* property and not the read model's contract is also why
the differential oracle must not be run as an assertion inside the suite: it is evidence, not a gate.

**Sensitivity control result (apply stage, head `99cde951…`).** With `stale_context_blocker` deleted
from `blockers/1` on the applied head copy, the read-model dump run reports `ExUnit total=51
failures=1` (the failing test is `projects lifecycle separately from execution and accepted outcome
receipts`, which asserts `"stale_context" in reasons`) and `diff ap-head-rm-1.dump ap-mut-rm-1.dump`
lists 14 changed dump lines (28 diff lines over 7 hunks). The oracle therefore sees a one-line
blocker change; the deletion was reverted and the source hash re-checked (`99cde951…`). The
implement stage measured the same planted red at `e254f025…` with `ExUnit total=50 failures=1` and
the same 14 changed lines.

## Design trade-offs

- **Struct, not keyed map.** The contract names `%Projection{}`; a struct also makes a typo a
  compile-time warning-free `nil` at worst and documents the field set in one place. `Map` would
  hide misspellings; `@enforce_keys` is not used because the struct is filled in one builder.
- **Nested module in the owned file.** A separate `projection.ex` would be one more file for one
  `defstruct` and would leave the owned set. The struct is a private implementation detail of this
  module: not exported through `Projection` in any public signature.
- **Convert only ≥3-field readers.** Reading `projection.<field>` inside a two-argument helper
  (`effective_integration_item_ids(projection.work_items)`, `planning_task?(task, …)`) keeps the
  diff focused; converting them would enlarge the diff without removing a long call shape.
- **Not stored on the struct: `blockers`, `settlement_attention`, `completion`.** They are pass
  outputs, not context; storing them would make `project/2`'s assembly order significant. Their
  dependencies are already on the struct (`completion_projection/1` no longer needs `blockers`).
- **dLOC is reported, not gated** (amendment P3 of the review): the removal of the 20-argument
  literal, the four readiness re-spellings and the settlement-entry chains are expected to land
  around −150…−200 lines before the `%Projection{}` definition; the measured number is reported
  even if a later simplification pass moves it.
- **The three readiness helpers and `completion_projection/12` do collapse onto the same
  `%Projection{}`** (stated as the amendment requires): all four become `/1` readers of one struct
  built once in `project/2`. Nothing keeps a separate argument bag, so the dLOC estimate does not
  drop below −100 for that reason.

## Out of scope (with reasons)

- `control/lib/symmetry_control/goals.ex` regions (W2-07 settlement/usage, W3-10, W4-07): read
  models are not edited there; `goals.ex` is untouched by this slice.
- `control/lib/symmetry_control_web/**` (W2-04's `portal_goal_attention_test.exs` oracle) and
  `workspaces/read_model.ex`: separate read models, no shared code path.
- The other Credo `FunctionArity`/`CyclomaticComplexity` reports in the same file that F5 mentions
  beyond the converted set (for example `budget_blocker/5` internals): not argument-threading
  defects of this slice.
- New tests: the oracle is the existing read-model tests plus the differential dump; no test may be
  weakened, and no new assertion was needed during implementation because the differential check
  observed identical output. If the differential check had shown a difference, the fix would have
  been in the implementation, not in a test. The apply stage added exactly one coverage test (see
  the apply-stage record below) for a load-bearing split that had no fixture; no existing assertion
  was changed, weakened or removed.

## Apply stage (accepted simplify findings, 2026-11-21)

Two code changes (one duplicated derivation removed, one struct-field rename), three record
corrections and one coverage pin, applied to the owned set only and then re-measured:

| Finding | Change | Verification |
| --- | --- | --- |
| duplicate derivation | `eligible_goal_evidence/1` no longer rebuilds `tasks_by_id`/`runs_by_id`; it reads `projection.tasks_by_id`/`projection.runs_by_id` (2 lines fewer) | same by-id maps, same key function; re-measured dumps below |
| misleading name | struct field `work_items_by_id` → `current_work_items_by_id` (defstruct, builder, single reader) so the `current_*` prefix matches the struct's own rule for filtered subsets | rename only; re-measured dumps below |
| record rule (P-1) | the conversion rule above now names the argument-threading chain and lists the three deliberately unconverted builders (`graph_projection/4`, `execution_by_work_item/4`, `planning_execution_projection/3`) | rule text checked against the head source: 41 rows, 42 names, all 42 take `projection` |
| record count (P-2) | "41 private functions collapse" → 42 (42 names in 41 rows; one row holds two names) | table recount by script over the head source |
| record scan (P-3) | the user-path row records the measured 1-line self-match of the contract's literal pattern (spelled `users[/]` here so this row cannot match it) and a substantive check whose pattern cannot match the row | `git diff origin/main...HEAD \| grep -icE "[Cc]:[\\/]Users"` = 0 and the same scan over the working-tree diff = 0; the contract's literal scan self-matches only (1 committed / 2 worktree lines, the gate row's `-`/`+` pair) |
| coverage pin | one new test: `a nonterminal Task from a prior revision splits the two readiness gates by task scope` | two-sided mutation check: flipping the candidates gate to `projection.current_tasks` fails only this test at its first assertion, flipping the integration gate to `projection.tasks` fails only this test at its second assertion; both mutations reverted, source hash re-checked (`99cde951…`) |

## Contract-preserving decisions taken without a guess

- `project/2`'s public signature and return remain unchanged, although the amendment notes its only
  external callers are the owned test file: keeping it is the boring choice and keeps `fetch/1`,
  `graph/1`, `attention/1` untouched.
- The differential oracle is a tracer rather than an edited test hook: it needs no source change on
  either side, so base and head are measured by the identical mechanism and the oracle cannot
  accidentally become part of the deliverable.
