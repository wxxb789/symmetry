# rf-w3-10 result dispatch plan — one `case` over the live settlement inputs

Contract: `goal-rf-w3-10-result-dispatch.md` (GDD, 2026-11-21), including the binding
`## Conductor decision (2026-11-21, W3 review reconcile)` (A8/P1-1, P1-2, P1-3, P2-1/P2-2, N3, D6) and
the lesson-32 control-flow audit rule. Program-plan row: §6 W3-10 (area control-core, dLOC
−100…−150, report-only per D2); findings `refactor/findings/control-core.md` F6:279-297.

Coordinate baseline: this worktree's HEAD `5f28b728c3f8752fdb8313f121f85c1ba3063911` (W2-07's branch,
already merged with `origin/main` at `79548368`), i.e. W2-07's `goals.ex` mutation-id work is present
and #149 (`b89e14d7`, `test(control): share Elixir test fixtures`) is in the base. Every line number,
count and test name below was read or measured at that revision.

Shape reference: `a503eab7` (#163, rf-w2-03 controller plumbing) — a control-subtree refactor closed by
a plan file under `docs/plans/`, host-local scratch evidence, no in-repo goal archive. This slice
follows that shape: the plan file is in-repo, the flip harness and its raw logs live in host-local
scratch, and the load-bearing numbers are repeated in the plan's evidence section and the run report.

## Owned set (STRICT)

- `control/lib/symmetry_control/goals.ex` — the dispatch region `:3220-3422` plus the call site's
  argument list at `:3042-3053`. Nothing else in the file; the mutation-id region `:10519-10647`
  belongs to W2-07 and stays untouched.
- `control/test/symmetry_control/goals_test.exs` (146 tests, 9,837 lines) — oracle; receives the new
  wake assertions for R3 and R7 plus one focused case for R8 (the only row the flip matrix left
  uncaught — see "The one row without a catcher").
- `control/test/symmetry_control/orchestration_review_test.exs` (18 tests, 777 lines) and
  `control/test/symmetry_control/orchestration_supervision_test.exs` (21 tests, 748 lines) — regression
  oracles only (P2-1/P2-2: neither reaches the dispatch; measured: neither file mentions `settle_task`
  or `reconcile_validation_outcome`).
- `docs/plans/2026-11-21-rf-w3-10-result-dispatch-plan.md` — this file.
- `docs/TODO.md` / `docs/archive/CLOSED-TODO.md` only if a TODO entry names this slice (measured: none
  does; no TODO edit).

## Baseline facts (measured at HEAD)

| fact | value | how measured |
| --- | --- | --- |
| `goals.ex` | 10,755 lines, 659 `def\|defp` (79 `def` + 580 `defp`) | `wc -l`, `grep -cE '^\s{2}defp? '` |
| dispatch region | `:3220-3422`, 203 lines, 13 `defp dispatch_*` clause heads, 12 blank lines | `sed -n '3220,3422p'` |
| arities | `dispatch_task_result/10` (7 heads), `dispatch_terminal_result/11` (6 heads) | `grep -n 'defp dispatch_'` |
| call site | `:3042-3053` in `terminal_settlement!/7` (`:2999`), consumes only `{:ok, settlement, wake_at}` | read |
| reachable callers | `settle_task/4` (`:2749`, call at `:2810`), `reconcile_validation_outcome/4` (`:2898`, call at `:2976`) | `grep` |
| other callers of the two dispatch functions | none (only the terminal catch-all at `:3410`) | `grep -n 'dispatch_task_result\|dispatch_terminal_result'` |
| test files exercising the dispatch | `goals_test.exs` (direct), `goals/workers_test.exs` (`SettleTaskWorker`/`WakeupWorker`), `symmetry_control_web/controllers/goal_machine_controller_test.exs` (`Goals.settle_task/4` at `:178,:287,:839`); skipped by default: `goals/dispatch_notification_test.exs` (`SYMMETRY_GOAL_DISPATCH_E2E`), `pi_control_e2e_test.exs`, `provider_broker_e2e_test.exs` (`SYMMETRY_PROVIDER_BROKER_E2E`) | `grep -rln 'settle_task\|reconcile_validation_outcome' control/test` |

The diff of this slice against `origin/main` is **only** the uncommitted working tree (`git status`
clean at dispatch): W2-07's `goals.ex` diff against `origin/main` is confined to `:10519+`
(verified: `git diff origin/main...HEAD -- control/lib/symmetry_control/goals.ex` shows hunks in the
mutation-id region only), so the dispatch region is byte-identical to `origin/main` `f4e4ace6`.

## The 13 rows and their exact semantics

`terminal_settlement!/7` computes `validation_outcome` (a `%WorkOutcome{}` or `nil`) and
`blocker_wake_at` (from `validate_result_blocker/5`, `:3681-3702`) and calls
`dispatch_terminal_result/11`. Rows 8-12 match on `%WorkOutcome{}` + `goal.state` **before** any
revision guard; row 13 delegates into `dispatch_task_result/10`, whose first row is the revision guard
and whose remaining rows are the `kind` literals.

| row | base clause | match | settlement | wake |
| --- | --- | --- | --- | --- |
| R1 | `dispatch_task_result` `:3220` | `task.goal_revision != goal.current_revision or goal.state != "active"` | `"historical_result"` | `nil` |
| R2 | `:3235` | `kind == "candidate_completion"` | `"awaiting_validation"` | `:immediate` |
| R3 | `:3249` | `kind == "progress"` and `progress_has_new_verification?/6` | `"progress"` / `"no_verified_progress"` | `:immediate` / `nil` |
| R4 | `:3266` | `kind == "blocked"` | `"blocked"` | `blocker_wake_at` |
| R5 | `:3280` | `kind == "repair_required"` | `"repair_required"` | `:immediate` |
| R6 | `:3294` | `kind == "replan_required"` | `"replan_required"` | `nil` |
| R7 | `:3308` | `kind == "failed"` | `"failed"` | `nil` |
| R8 | `dispatch_terminal_result` `:3322` | `%WorkOutcome{disposition: "rejected", reason: "historical_result"}` | `"historical_result"` | `nil` |
| R9 | `:3337` | `%WorkOutcome{disposition: "rejected"}` + `goal.state == "active"` | `"validation_failed"` | `nil` |
| R10 | `:3352` | `%WorkOutcome{disposition: "rejected"}` + `goal.state == "paused"` | `"historical_result"` | `nil` |
| R11 | `:3367` | `%WorkOutcome{disposition: "accepted"}` + `goal.state == "active"` | `"accepted"` | `:immediate` |
| R12 | `:3382` | `%WorkOutcome{disposition: "accepted"}` + `goal.state == "paused"` | `"accepted"` | `nil` |
| R13 | `:3397` | anything else (incl. `outcome == nil`, non-`WorkOutcome`) | R1-R7's row | R1-R7's row |

Order is load-bearing in three places: (a) R8 precedes R9/R10, so a rejected/`historical_result`
outcome on an **active** goal is `historical_result`, not `validation_failed`; (b) R9-R12 read
`goal.state` only, so a stale `task.goal_revision` does not make an accepted/rejected outcome
historical; (c) R1 precedes the `kind` literals, so a stale revision wins over `kind`.

## Change

### New shape

One private function, one `case`, no catch-all:

```elixir
defp dispatch_result(goal, task, run, kind, blocker_wake_at, subject_hash, evidence_refs, outcome, opts) do
  case {outcome, goal.state, kind} do
    {%WorkOutcome{disposition: "rejected", reason: "historical_result"}, _state, _kind} -> R8
    {%WorkOutcome{disposition: "rejected"}, "active", _kind} -> R9
    {%WorkOutcome{disposition: "rejected"}, "paused", _kind} -> R10
    {%WorkOutcome{disposition: "accepted"}, "active", _kind} -> R11
    {%WorkOutcome{disposition: "accepted"}, "paused", _kind} -> R12
    {_outcome, _state, _kind} when guard -> R1
    {_outcome, _state, "candidate_completion"} -> R2
    {_outcome, _state, "progress"} -> R3
    {_outcome, _state, "blocked"} -> R4
    {_outcome, _state, "repair_required"} -> R5
    {_outcome, _state, "replan_required"} -> R6
    {_outcome, _state, "failed"} -> R7
  end
end
```

`item` and `blocker` are dropped from the parameter list and from the `:3042` call site (dead in
12/13 base clauses: `_item`/`_blocker` are underscored and unused; the 13th head (R13, `:3397`)
binds both bare only to forward them into `dispatch_task_result/10`, whose heads all ignore them;
the returned map reads the enclosing scope's `blocker`, so nothing changes for the response). Arity 9
is the floor (P1-2): the terminal rows read `%WorkOutcome{}` and `progress_has_new_verification?/6`
must stay behind R3's arm — hoisting it would add a `Repo.all` to every settlement.

### Unknown `kind`

Base raises `FunctionClauseError` after the guard fails (R1's guard is a clause head; the six `kind`
heads follow; the terminal catch-all only forwards the same `kind`). The new `case` has **no default
arm**, so an unmatched `kind` still raises rather than returning — no `progress`/`no_verified_progress`
fall-through is introduced (clause 3). Two facts bound what the raised term can be:

- the exception struct differs (`CaseClauseError` vs `FunctionClauseError`); both are "no clause
  matched" errors and neither returns a settlement;
- the reachable `kind` set is exactly the six literals: `terminal_settlement!` (`:3013-3023`) admits
  only `task_result_matches_task?/2` kinds, which for a non-plan Task (`:8743-8756` delivered) are
  `progress | candidate_completion | blocked | repair_required | replan_required | failed`. The only
  other admitted kind, `"plan_proposed"`, requires `plan_task?/1` (`:8737` delivered), and it cannot
  reach the dispatch on either entry point: `settle_task/4` routes every plan Task to
  `terminal_plan_settlement!/4` (`:2807-2810`), and `reconcile_validation_outcome/4` only re-derives a
  settlement after replaying an existing receipt whose settlement is `"awaiting_validation"`
  (`:2951`), which only R2 (`candidate_completion`, itself admitted only for a non-plan Task) can have
  produced — so its re-check of `task_result_matches_task?/2` also sees a non-plan Task.

The contract deliberately allows either "reproduce exactly" or "prove the path cannot produce such a
kind and say so" (Change (2)). This increment takes the second branch — it keeps "raise, do not
return" with the same clause exhaustion (without a default arm) and proves the trigger unreachable —
and does **not** claim the first: the exception struct is not reproduced exactly. That residual
difference is recorded here and in the control-flow audit (row 9) rather than hidden.

### Control-flow audit (lesson 32)

Each base control-flow decision → its HEAD equivalent. "Preserved" = same settlement and wake for the
same inputs, reached in the same order.

| # | base decision point | HEAD equivalent | preserved |
| --- | --- | --- | --- |
| 1 | R8 match (rejected + reason `historical_result`, any state) | 1st arm | yes |
| 2 | R9 match (rejected + active) | 2nd arm | yes |
| 3 | R10 match (rejected + paused) | 3rd arm | yes |
| 4 | R11 match (accepted + active) | 4th arm | yes |
| 5 | R12 match (accepted + paused) | 5th arm | yes |
| 6 | R13 fall-through to `dispatch_task_result/10` with the same 10 values | fall-through to the guard/kind arms of the same `case` (no call, same values) | yes |
| 7 | R1 guard (stale revision or non-active goal) after R8-R13 | 6th arm, same guard text | yes |
| 8 | R2-R7 `kind` literal exhaustion | 7th-12th arms, same literals, same order | yes |
| 9 | guard-failed + unknown `kind` → clause exhaustion (`FunctionClauseError`) | `case` arm exhaustion (`CaseClauseError`) | same "raise, do not return"; struct differs, unreachable (above) |
| 10 | `progress_has_new_verification?/6` called only on the `progress` row | only in R3's arm | yes |
| 11 | error short-circuits: none inside the 13 clauses (all rows return a value) | none in the `case` | yes |
| 12 | `terminal_settlement!` `with`-chain failures (`invalid_task_result`, `{:error, reason}`) | untouched (`:3013-3033`) | yes |
| 13 | call-site consumption `{:ok, settlement, wake_at}` | unchanged `with` clause, 9 arguments | yes |
| 14 | goal parameter shape: the base R8 and R13 heads accept any term for `goal` (only R1's guard and the R9-R12 `%Goal{}` patterns read it) | the `case` subject reads `goal.state`, so a non-`%Goal{}` goal raises `KeyError` before any arm | **no, and unreachable**: both callers bind `goal = lock_goal/1`, which is `Repo.one(...) || rollback(:not_found)`, and `terminal_settlement!/7` is private with exactly those two callers (`:2810`, `:2976`); a `%Goal{}` is the only value that can arrive |

### Awk structural acceptance

Measured with `grep`/`awk` (`<HOST-PATH>/.local/state/gdd/refactor/scratch/w3-10/counts.sh`):

| measurement | base (`HEAD`) | delivered |
| --- | --- | --- |
| dispatch clause heads in the region | 13 (`dispatch_task_result/10` ×7 at `:3220-3320`, `dispatch_terminal_result/11` ×6 at `:3322-3422`) | 1 (`dispatch_result/9`, `:3222`) |
| `defp dispatch_*` in the whole file | 14 (the 13 plus the unrelated `dispatch_authorized_goal_control_command!` at `:5062`) | 2 (the same unrelated function at `:4913`) |
| region LOC | 203 | 56 (4 comment lines + the function) |
| file LOC | 10,755 | 10,606 |
| `def`/`defp` in the file | 659 (79 `def`, 580 `defp`) | 647 (79 `def`, 568 `defp`) |
| dispatch call site | 11 arguments incl. `item`, `blocker` | 9 arguments, no `item`/`blocker` |

The LOC delta (report-only per D2) is −147 in the region and −149 for the file, against the plan row's
−100…−150 estimate. `grep` for a bare `item`/`blocker` inside the delivered region returns 0 matches.

### Where a wake becomes observable

The dispatch's second element is consumed by `terminal_next_wake_at/4` (`:3916` base / `:3767` HEAD),
surfaced as `"next_wake_at"` in the `settle_task/4` response (`nullable_datetime/1` →
`DateTime.to_iso8601/1` for a `%DateTime{}`, `nil` for `nil`) and persisted to `goals.next_wake_at`
through `append_event!` (`event_next_wake_at/3` reads `opts[:settlement_next_wake_at]`).
`terminal_next_wake_at/4` discards the settlement's own wake unless
`task.goal_revision == goal.current_revision and goal.state == "active"`, and `:immediate` always
yields `now(opts)` while `nil` only *preserves* a strictly future `goals.next_wake_at`.

Measured assertions before this slice (in `goals_test.exs`): `progress`/`no_verified_progress` →
the `no_verified_progress` nil (`:4443`), `repair_required` → `now` (`:4384`, `:8237`),
`replan_required` → nil (`:4412`), active+rejected (`validation_failed`) → nil (`:3985`),
paused+rejected (`historical_result`) → nil (`:4249`), paused+accepted (`accepted`) → nil (`:4282`).
This slice adds the two asserted halves the contract names (D-Change 3):

- `progress` (`:immediate`): the first settlement of
  `"duplicate progress without a new Subject or verified evidence stops at no_verified_progress"`
  now binds `"next_wake_at" => next_wake_at` and asserts `next_wake_at == DateTime.to_iso8601(@now)`;
- `failed` (nil): `"failed validation evidence does not turn a self-declared failure into a rejected
  outcome"` now matches `"settlement" => "failed", "next_wake_at" => nil` (that test settles a
  *completed* Run carrying the `failed` kind, i.e. R7 — the other `failed` builders settle Runs whose
  own state is `failed`, which is the `process_terminal_settlement/2` path and never reaches the
  dispatch).

Both edits add assertions only; no `test "…"` macro line of an existing test is touched
(`git diff -U0 -- control/test/symmetry_control/goals_test.exs` shows two changed and one added
assertion line plus the new case below; the suite has 146 static names with no interpolation, and the
only new name is the one added for R8).

### The one row without a catcher (R8)

R8 (terminal `%WorkOutcome{disposition: "rejected", reason: "historical_result"}`) had no catcher at
the baseline. The closest test, `"a competing accepted Subject is historical and does not loop
validation recovery"` (`:3781`), drives the settlement through `recover_pending_terminal_settlements/1`
+ `WakeupWorker`, i.e. `reconcile_validation_outcome/4`, which **discards** `terminal_settlement!`'s
return value (`:2976`) and recomputes the receipt's settlement from `outcome.reason` inside
`late_validation_outcome_receipt!/5`; its `"historical_result"` assertions therefore cannot observe the
dispatch arm.

One focused case is added (new name, existing suite):
`"a competing accepted Subject settles a first terminal validation as historical result"`. It reuses
the same fixture (`independent_validation_attempt_fixture(check_and_operator_acceptance_contract/0)`),
appends passing evidence, inserts an accepted `WorkOutcome` for a **different** candidate subject
(`insert_accepted_work_outcome!/4`), and then calls `Goals.settle_task/4` **once** on the validation
task: `derive_validation_outcome!/10` sees an existing accepted outcome that does not match the
candidate and inserts `rejected`/`historical_result`, which R8 turns into
`{:ok, "historical_result", nil}`. The assertions are the settlement, the wake
(`"next_wake_at" => nil`, so R8's wake half becomes observable too) and the persisted outcome row. The
second settlement is not reachable as a replay: R8 is reached on the **first** settlement of that
Run, whose mutation id (`settlement_mutation_id/3`) is unused.

## Evidence plan

### Flip-matrix method

One host-local instrument (`<HOST-PATH>/.local/state/gdd/refactor/scratch/w3-10/flips.py`) drives every
flip identically: copy the delivered `goals.ex` to a pristine snapshot once; for each flip apply one
exact-string replacement to one arm (the harness aborts if the anchor is not unique), run
`mix test test/symmetry_control/{goals_test.exs,orchestration_review_test.exs,orchestration_supervision_test.exs}`
with `MIX_ENV=test`, `MIX_TEST_PARTITION=rf-w3-10` and the same portable `erl.exe`/Elixir 1.20.4
invocation the shell runner uses (no `bash` dependency), then restore the pristine bytes and verify the
file's sha256 before the next flip. Row `PRISTINE` runs the same command with no mutation first, so the
matrix carries its own green control. Per flip the harness records the exit code, ExUnit's `Result:`
line, the count and names of failing tests, and the restore verdict; the driver's final check compares
the tree's sha256 against the pristine snapshot so a flip cannot survive.

Two flips are not single-value edits and are recorded as such: `S_R13` inserts an unconditional arm at
the delegation point (before the guard) so no non-terminal case can fall through into R1-R7, and `R13`
has no wake literal of its own (its wake is the delegating row's).

### Flip matrix (measured)

Columns: the row of the dispatch table; the one-arm settlement mutation applied for that row; the
first failing test (plus how many others failed) with the driver's exit code; the wake mutation for
the same row (rows whose wake cannot reach an assertion are listed as unobservable, per the contract's
P1-3: the wake is discarded unless the goal is active at the current revision, and row 4's wake is
overwritten by the external-wait path); and the measured catch. `PRISTINE` is the unmutated control.
The driver ran 3,482 s of wall time on a host shared with other slices; `PRISTINE` is `Result: 186
passed` (exit 0) and the driver's final sha256 check confirms the tree was restored to
`b6b1cd2334a896c51d5c253ad55e3f9b652d02cedeb4112ff4e10d2b60ac2c55` after the last flip.

| row | settlement flip | caught? | wake flip | caught? |
| --- | --- | --- | --- | --- |
| R1 guard/historical | guard: `historical_result` → `validation_failed` | red: `superseded validation failure remains historical and cannot reject the new revision` (1 test) | `nil` → `:immediate` | **not caught** (186 passed) |
| R2 candidate_completion | `awaiting_validation` → `historical_result` | red: `a conflicting stored optional integration marker refuses achieve for a single-required plan` +36 (37 tests) | `:immediate` → `nil` | red: `terminal settlement persists one replay-safe wakeup job` +1 (2 tests) |
| R3 progress | swap `progress` ↔ `no_verified_progress` | red: `duplicate progress without a new Subject or verified evidence stops at no_verified_progress` +2 (3 tests) | swap `:immediate` ↔ `nil` | red: `duplicate progress without a new Subject or verified evidence stops at no_verified_progress` (1 test) |
| R4 blocked | `blocked` → `progress` | red: `an external blocker is retained as unsupported history without a model observation` (1 test) | `blocker_wake_at` → `:immediate` | **not caught** (186 passed) |
| R5 repair_required | `repair_required` → `replan_required` | red: `repair and replan results are retained as proposals without admitting work` +1 (2 tests) | `:immediate` → `nil` | red: `repair and replan results are retained as proposals without admitting work` +1 (2 tests) |
| R6 replan_required | `replan_required` → `repair_required` | red: `repair and replan results are retained as proposals without admitting work` (1 test) | `nil` → `:immediate` | red: `repair and replan results are retained as proposals without admitting work` (1 test) |
| R7 failed | `failed` → `replan_required` | red: `failed validation evidence does not turn a self-declared failure into a rejected outcome` (1 test) | `nil` → `:immediate` | red: `failed validation evidence does not turn a self-declared failure into a rejected outcome` (1 test) |
| R8 rejected+historical_result | `historical_result` → `validation_failed` | red: `a competing accepted Subject settles a first terminal validation as historical result` (1 test) | `nil` → `:immediate` | red: `a competing accepted Subject settles a first terminal validation as historical result` (1 test) |
| R9 active+rejected | `validation_failed` → `historical_result` | red: `failed independent validation records one immutable rejection and cannot be bypassed by passed evidence from that attempt` +1 (2 tests) | `nil` → `:immediate` | red: `failed independent validation records one immutable rejection and cannot be bypassed by passed evidence from that attempt` (1 test) |
| R10 paused+rejected | `historical_result` → `validation_failed` | red: `paused current revision records trusted validation rejection without waking execution` (1 test) | `nil` → `:immediate` | **not caught** (186 passed) |
| R11 active+accepted | `accepted` → `validation_failed` | red: `a single-required plan admitted without an integration designation achieves without the payload field` +10 (11 tests) | `:immediate` → `nil` | red: `terminal producer, validation evidence, accepted outcome, and achieve close the Goal loop` (1 test) |
| R12 paused+accepted | `accepted` → `historical_result` | red: `paused current revision records accepted validation without waking execution` (1 test) | `nil` → `:immediate` | **not caught** (186 passed) |
| R13 delegation | unconditional arm before the guard | red: `a competing accepted Subject settles a first terminal validation as historical result` +43 (44 tests) | no wake literal (structural delegation) | n/a |

Measured wake observability matches P1-3 exactly: the only uncaught wake flips are R1, R4, R10 and
R12, and no row the contract calls observable turned out unobservable. R7 and R8 are caught only
because this slice added their assertions, and R13's delegation is pinned by 44 tests, so the matrix
describes the delivered test set rather than the baseline's.

1. **Gates** (foreground, portable-toolchain recipe, `MIX_TEST_PARTITION=rf-w3-10`, local PostgreSQL on
   `localhost:5432`, `POSTGRES_PORT`/`SYMMETRY_CONTRACTS_DIR` unset as in the w2-06 runner):
   `mix format --check-formatted`; `MIX_ENV=test mix compile --warnings-as-errors`;
   `mix test test/symmetry_control/goals_test.exs test/symmetry_control/orchestration_review_test.exs
   test/symmetry_control/orchestration_supervision_test.exs`; `git diff --check`.
2. **Lesson-32 extended gate**: the same command plus every other test file whose tests call
   `Goals.settle_task/4` — `test/symmetry_control/goals/workers_test.exs` (through
   `SettleTaskWorker`/`WakeupWorker`) and
   `test/symmetry_control_web/controllers/goal_machine_controller_test.exs` (direct calls at
   `:178/:287/:839`), plus the two opt-in files added after the codex audit's P1,
   `test/symmetry_control/pi_control_e2e_test.exs` and `test/symmetry_control/provider_broker_e2e_test.exs`,
   whose `@moduletag skip` gates make them register as skipped on this host. Measured: the 5-file
   command `Result: 202 passed, 1 skipped` (exit 0, 76.4 s); the 7-file command
   `Result: 204 passed, 11 skipped` (exit 0, 69.9 s). Its JSON event stream gives the composition —
   `GoalsTest` 147, `OrchestrationSupervisionTest` 21, `OrchestrationReviewTest` 18,
   `GoalMachineControllerTest` 10 passed + 1 skipped, `Goals.WorkersTest` 6,
   `DataCaseSandboxOptionsTest` 2 (the second module of `pi_control_e2e_test.exs`), `PiControlE2ETest`
   6 skipped and `ProviderBrokerE2ETest` 4 skipped: 215 total, 204 passed, 11 skipped, 0 failed.
3. **Flip matrix** (judging artifact): 13 settlement flips (one arm mutated at a time, exact-string
   replacement asserted unique, file restored and sha256-verified after each flip) and the wake flip per
   row, each run against the three oracle suites; per flip the exit code, the failure count and the
   failing test names are recorded. Predicted wake observability from P1-3 is 2, 3, 5, 6, 7, 9, 11, with
   1/4/10/12 unobservable; the matrix records the measured result, and any row that the P1-3 prediction
   calls unobservable but whose wake flip is caught is reported as a divergence.
4. **New assertions**: wake assertions for R3 (`progress` → `:immediate`; `no_verified_progress` → nil,
   already pinned at `:4443`) and R7 (`failed` → the goal's existing wake, `nil` in the fixture); plus
   the R8 focused case above (R8's wake half becomes observable with it). No existing test name changes.
5. **Secondary measurement (not a pass condition)**: `mix credo --strict` on `goals.ex`, before and
   after, for `Credo.Check.Refactor.FunctionArity`; `control/.credo.exs` keeps the default `[]` options
   (max_arity 8), so arity 9 is expected to produce one finding, recorded rather than suppressed.

## Secondary measurement: `mix credo --strict` (not a pass condition)

Credo as pinned in `mix.lock`, `control/.credo.exs` unchanged (`{Credo.Check.Refactor.FunctionArity, []}`,
default `max_arity: 8`), run on `control/lib/symmetry_control/goals.ex` at the `HEAD` revision of that
file and on the delivered file (`git show HEAD:` swap, then restored). The swap script's own verification
printed `RESTORE_FAILED` while it exited 0: its python helper received an MSYS-style `/c/...` path that
native Windows python cannot open, so the check never ran — an instrument defect, not a lost restore.
The restored bytes were confirmed afterwards by three independent reads of the same file (the sha256
line `final-gates.sh` prints before the gates, the auditor's own digest check, and the direct
tree-vs-snapshot comparison recorded as `HASH_VERIFY` in `<HOST-PATH>/…/w3-10/extended7.log`); the
script's path form and its failure exit code are fixed, and the original `RESTORE_FAILED` line is kept
in `closing.log` rather than edited away.

| measurement | base (`HEAD` file) | delivered |
| --- | --- | --- |
| arity findings | 18 total, **13 on the dispatch functions** (7 × `dispatch_task_result` arity 10, 6 × `dispatch_terminal_result` arity 11) | 6 total, **1 on `dispatch_result` (arity 9)**; the other 5 pre-existing (`derive_validation_outcome!` ×2, `insert_validation_outcome!`, `immutable_receipt_snapshot`, `context_snapshot_document`) |
| cyclomatic-complexity findings on the dispatch functions | 0 | 1 on `dispatch_result` (15 > 9) |
| total issues reported for the file | 141 (660 mods/funs) | 130 (648 mods/funs) |
| `mix credo --strict` exit code | 14 | 14 |

The single arity-9 finding is the outcome the contract asks to record, with no suppression added (no
`credo:disable`, no `//nolint`, `.credo.exs` untouched). The new `Refactor.CyclomaticComplexity` finding
is the price of one `case` with 12 arms; it is recorded here rather than suppressed, and the file
already carries findings of that class elsewhere (`:1593`, `:10428`, …).

## Independent read-only audits

Two independent auditors read the delivered tree (both verified
`sha256(control/lib/symmetry_control/goals.ex) = b6b1cd23…ac2c55` before starting; neither ran `mix`,
built, tested or edited anything): a Tencent DeepSeek V4.1 Flash Fast `task` child and a
`ghc/gpt-6.2-sol` pass at `xhigh` effort through `codex exec -s read-only`, both against the same
brief (`<HOST-PATH>/.local/state/gdd/refactor/scratch/w3-10/audit-brief.md`, raw output in
`audit-sol.log` and the agent transcript).

DeepSeek auditor: **no P0, P1 or P2 finding**; row parity, structural claims and the test diff
"no finding"; 13/13 rows marked `preserved` with the order-sensitive facts re-verified independently
(terminal rows precede the guard, `historical_result` wins over active/paused, R11/R12 match state
only, `progress_has_new_verification?/6` has exactly one call site inside the progress arm) and the
`{:ok, …}` literal-set diff counted as 1:1 (13 removed, 13 added). Three P3 notes, all reconciled:

| auditor note | reconciliation |
| --- | --- |
| P3-1 eager `goal.state` read ⇒ `KeyError` for a non-`%Goal{}` goal where base's R8 head accepted any term | already recorded as control-flow audit row 14; the auditor independently re-derived the reachability proof (`lock_goal/1` = `Repo.one(...) || rollback(:not_found)`, two private callers inside `Repo.transaction`) |
| P3-2 unmatched `kind` raises `CaseClauseError` instead of `FunctionClauseError` | already recorded as audit row 9; the auditor closed the last gap by noting the reconcile path's prior receipt must be `"awaiting_validation"`, which only R2 can produce, so `plan_proposed` cannot reach the dispatch there either |
| P3-3 informational: the new R8 test's settlement is also produced by R10/R1, so a future fixture change could "re-point" it | not a reachable risk: R8's arm precedes R10 and constrains neither goal state nor revision, so a paused or stale goal still takes R8 for a `rejected`/`historical_result` outcome; R10 only fires for a rejected outcome with another reason. The test also asserts the persisted outcome row |

DeepSeek clause-3 verdict: no blocker — no settlement/wake change, no guard-order change, no changed
pinned name (146 → 147 with only an addition), no default arm, no weakened or skipped test.

The `ghc/gpt-6.2-sol` pass: first attempt (`-m ghc/gpt-6.2-sol`) failed with `The selected model could
not be resolved`; re-run with the model id this host's `~/.codex/config.toml` declares (`gpt-6.2-sol`)
at `model_reasoning_effort="xhigh"`, read-only sandbox (exit 0, 414 s). **No finding in row parity or
evaluation semantics** (its own arm-by-arm table, including "the `progress` arm still holds the only
`progress_has_new_verification?/6` call and its `Repo.all` was not hoisted"), **no finding in reachable
error or type paths**, **no finding in the three test changes** (it re-derived (a) the `failed` fixture
reaching R7 with a nil wake, (b) the first `progress` settlement returning `true` from
`progress_has_new_verification?/6` so `:immediate` becomes `DateTime.to_iso8601(@now)`, and (c) the R8
case travelling `:missing` → `is_struct(existing_accepted, WorkOutcome)` → inserted
`rejected`/`historical_result` → R8, with the row assertion reading the new rejection rather than the
pre-inserted accepted row), and it confirmed the structural counts independently (region 13 → 1 head,
file 14 → 2, 203 → 56 lines, 10,755 → 10,606, nine parameters/arguments, no `item`/`blocker`, the
outer `blocker: blocker` retained). Three findings, all accepted and closed:

| auditor finding | resolution |
| --- | --- |
| **P1** the lesson-32 foreground gate omitted two files that call `Goals.settle_task/4` (`pi_control_e2e_test.exs`, `provider_broker_e2e_test.exs`) | accepted: the extended gate command now includes both (Evidence plan item 2). Both are opt-in (`@moduletag skip:` on `SYMMETRY_*` env vars), so they register as skipped on this host — recorded as a host limit, never as a pass |
| **P2** two inaccuracies in the unreachability proof: `plan_task?/2` is `plan_task?/1`, and reconciliation does not check a Task's `purpose` | accepted and fixed in "Unknown `kind`" above (`plan_task?/1`; the chain now rests on the `"awaiting_validation"` receipt precondition at `:2951` plus the R2 admission rule) |
| **P2** "does both" overstated exact reproduction (the exception struct differs) | accepted and reworded: the increment takes the "prove unreachable" branch with the same clause exhaustion and does **not** claim exact struct reproduction |
| **P3** the Credo receipt said "restored and sha256-verified" while `closing.log` records `RESTORE_FAILED` | accepted: the Credo section above discloses the instrument defect, keeps the original line, and cites three independent confirmations; the script's MSYS/Windows path form and its failure exit code are fixed |

Codex clause-3 verdict: no blocker, with the explicit caveat that contract acceptance also depends on the
conductor's full-suite gate (full `mix test`), which this stage does not run by contract.

## Out of scope, with reasons

- The `Goals.*` facade split (W4-07), the mutation-id helpers (W2-07), payload parsing /
  context-snapshot regions, and `mix xref unreachable` surface work (W1-14 landed) — contract clause 4.
- `terminal_settlement!`'s own parameter list (7 args, `item` live in its body at
  `:3017`/`:3029`/`:3090`): not a dead-parameter list, and outside the owned region.
- `mix credo --strict` as a gate: the ratchet is W1-01/#157's advisory CI step, and clause 3 forbids
  new suppressions; the finding is reported in the PR body instead.

## Risks

- **Fixture coverage, not code risk**: the wake halves of R1, R4, R10 and R12 are unobservable by
  construction (measured: those four wake flips leave the suite green; every other wake flip and all 13
  settlement flips are red). The matrix states that explicitly instead of claiming per-row proof.
- **Shared-file serialization**: `goals_test.exs` is shared with W3-15's split and #149 already
  rewrote it; this slice adds tests inside the existing file and renames nothing, so a later split
  rebases cleanly.
- **Exception struct difference** for an unreachable unknown `kind` (documented above); a mutation test
  cannot reach it without disabling `task_result_matches_task?/2` and the contract schema at once.
