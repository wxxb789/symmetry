# rf-w4-10-refusal-classification — evidence record (2026-11-22)

Subject: one refusal/outcome classification vocabulary in `frontend/src/services/refusals.ts` — the
four predicates, `failureMode`, the one guidance fold and the three named per-surface guidance
tables; every base site reduces to a call of the module. Contract:
`goal-rf-w4-10-refusal-classification.md` (GDD, 2026-11-21/22, binding conductor decisions D1
frozen bytes, D2 LOC report-only, the mutation-proof rule, the lesson-32 control-flow audit, the
W4-review-reconcile decisions — E8 runnable oracles, "no base map unless every surface carries the
identical step", call-site ownership — and the owner comment policy of 2026-11-22). Base revision
`7f34d40d` (`refactor(frontend): split api reads and writes (#239)`, this worktree's dispatch HEAD,
tree clean; every base coordinate below). Plan:
`docs/plans/2026-11-22-rf-w4-10-refusal-classification-plan.md`. Judging artifact (contract clause
2, "answer.md"): this record plus the implement-stage report (definition-count greps, the
base→module predicate table, the twelve mutation runs, the focused + full suite output, the
import-line diff and the LOC delta).

## 1. Deliverable shape (files, counts)

| file | status | lines (base → head) | note |
| --- | --- | ---: | --- |
| `frontend/src/services/refusals.ts` | new | 0 → 77 | the five predicates, `failureMode` + its `FailureMode` type, the three guidance tables, one `refusalNotice` |
| `frontend/src/services/refusals.test.ts` | new | 0 → 212 | 12 cases: five truth tables, `failureMode`'s 5-way table, the 21 joined-output pins, the default-table pin, the fallback pins |
| `frontend/src/services/api.ts` | edited | 1,003 → 989 | the `refusalNotice` fold deleted (`:991-1003` incl. its doc comment); `ownValue` `:987-989` and `errorMessage` `:971-977` stay, untouched |
| `frontend/src/services/api.test.ts` | edited | 631 → 631 | import lines only (`:2` `failureMode`+`refusalNotice` → `./refusals`; `:3` drops `refusalNotice`) |
| `frontend/src/features/chat/use-chat-scope-state.ts` | edited | 252 → 248 | local `definitiveRefusal` deleted, `isDefinitive` imported, call site `:168` |
| `frontend/src/features/chat/use-older-page-loading.ts` | edited | 62 → 58 | local `definitiveReadRefusal` deleted, `isDefinitiveReadRefusal` imported, call site `:35` |
| `frontend/src/features/goals/detail.tsx` | edited | 762 → 756 | local `outcomeUnknown` deleted (`isUnknownOutcome` at `:161`, `:218`); `refusal.stale` → `isStaleRefusal(refusal)` at `:256` |
| `frontend/src/features/planning/write-policy.ts` | edited | 388 → 350 | `FailureMode`/`failureMode`/`refusalGuidance`/`refusalNotice` deleted; the file keeps only its own domain logic |
| `frontend/src/features/planning/editors.tsx` | edited | 559 → 558 | import line split; 4 `refusalNotice` + 4 `failureMode` calls unchanged |
| `frontend/src/features/planning/planning.test.tsx` | edited | 790 → 791 | import line split (`:8` → `../../services/refusals`, `:9` keeps the domain helpers); every assertion byte-identical |
| `frontend/src/features/connections/resources.tsx` | edited | 688 → 671 | local map + wrapper deleted; 3 calls pass `resourceRefusalGuidance`; `stale`/`terminal` → `isStaleRefusal`/`isTerminalStateRefusal` `:455-456` |
| `frontend/src/features/connections/connections.tsx` | edited | 496 → 496 | import line, 5 calls pass `resourceRefusalGuidance`, `:138` → `isStaleRefusal` |
| `frontend/src/features/projects/projects.tsx` | edited | 143 → 144 | import line split only |
| `frontend/src/features/projects/work-surface.tsx` | edited | 360 → 361 | import line split only |
| `frontend/src/features/work/assignment.tsx` | edited | 334 → 324 | local map deleted; 2 calls pass `assignmentRefusalGuidance`; `:235` → `isStaleRefusal` |
| `frontend/src/features/work/control.tsx` | edited | 478 → 468 | local `definitiveRefusal` deleted, `isDefinitive` imported, 5 call sites (`:199`, `:216`, `:233`, `:251`, `:463`) |
| `docs/plans/2026-11-22-rf-w4-10-refusal-classification-plan.md` | new | 316 | the plan |
| `docs/evidence/rf-w4-10-refusal-classification-2026-11-22.md` | new | 332 | this record |

Not edited: `frontend/src/features/chat/chat.tsx` (W4-08 moved both predicates into the two hooks;
the file holds no refusal classification at HEAD), `services/commands.ts` (the `MutationRefused`
class and its `stale` getter stay; the module consumes the getter), `services/control-actions.ts`,
`features/attention/attention.tsx`, `features/work/receipts.ts`, `features/chat/presentation.ts`,
every other package, `contracts/**`, `browser/**`, and every oracle suite (§12).

## 2. Re-derived owned set against HEAD (dispatch deltas vs the contract's `b89e14d7` coordinates)

1. **`chat.tsx` holds no predicate.** W4-08 (`d5b14b85`) split ChatPage: `definitiveRefusal` lives
   in `use-chat-scope-state.ts:92-95` (call `:172`) and `definitiveReadRefusal` in
   `use-older-page-loading.ts:7-10` (call `:39`). Those two files are the contract's
   `chat.tsx:708-711`/`:731-734` sites; `chat.tsx` itself needed no edit.
2. **A fifth copy of the same predicate.** `work/control.tsx:463-467` defined `definitiveRefusal`
   with a byte-identical body, used at `:203`, `:220`, `:237`, `:255`, `:473`. Clause 1's "exactly
   one implementation of each predicate" is unsatisfiable while that copy stands, so the file's
   predicate, its doc comment and its import block were edited and its five call sites re-pointed;
   no other line changed.
3. **Two more inline staleness sites.** `connections.tsx:138` and `assignment.tsx:245` derived
   `stale` as `failure instanceof ServerRefusal && failure.code === "stale"` — the same policy as
   the contract's `resources.tsx:390`. Both now call `isStaleRefusal`; the local variable names and
   every render around them are unchanged.
4. **The Elixir comment-only item is already satisfied.** `ae4b345f` (#223, "apply the comment
   policy to refactor-program comments") removed the two `portal_*_test.exs` comment blocks: at
   HEAD `grep -rn "features/"` and `grep -rn "write-policy"` over `control/` find no citation, so
   no Elixir file was edited (cited line numbers move with the plan's §2.4 and §12).

## 3. Base → module predicate table (clause 2's per-site "identical yes/no")

Each row's HEAD claim is a runnable case in `frontend/src/services/refusals.test.ts` (the case
name in the third column), not a reading.

| base site (base policy text) | module export | runnable case | identical |
| --- | --- | --- | --- |
| `use-chat-scope-state.ts:92` `definitiveRefusal` = `Conflict \|\| Validation \|\| Forbidden \|\| Unsupported \|\| (ServerRefusal && 400 ≤ status < 500)` (call `:172`) | `isDefinitive` | "reproduces definitiveRefusal as chat/use-chat-scope-state.ts and work/control.tsx defined it" (12 rows incl. `Unsupported` and `ServerRefusal 499/500`, the status boundary) | yes |
| `work/control.tsx:463` byte-identical copy (5 calls) | `isDefinitive` | same case | yes |
| `use-older-page-loading.ts:7` `definitiveReadRefusal` = `Conflict \|\| Validation \|\| (UnexpectedResponse && status === 400)` (call `:39`) | `isDefinitiveReadRefusal` | "keeps the read-cursor policy distinct from isDefinitive" (10 rows incl. 400/404/500 and `Forbidden`) | yes |
| `goals/detail.tsx:41` `outcomeUnknown` = `Network \|\| UnexpectedResponse` (calls `:167`, `:224`) | `isUnknownOutcome` | "reproduces outcomeUnknown as goals/detail.tsx defined it" (7 rows, transient and permanent) | yes |
| `commands.ts:105-107` `MutationRefused.stale` = `409 && (stale \|\| stale_revision)` (call `detail.tsx:262`) | `isStaleRefusal` class arm delegates to the getter | "delegates the command surface's staleness to MutationRefused.stale" (5 rows incl. `stale_revision`, a 404 and both non-stale codes) | yes |
| `resources.tsx:471` + `connections.tsx:138` + `assignment.tsx:245` `stale` = `ServerRefusal && code === "stale"` (**no status condition**) | `isStaleRefusal` server arm over `staleServerCodes = ["stale"]` | "reproduces the write surfaces' ServerRefusal stale check, status-insensitive as base was" (7 rows incl. a 500/`stale` true and a 409/`stale_revision` false) | yes |
| `resources.tsx:472-473` `terminal` = `ServerRefusal && code ∈ {stale, state_conflict, not_found}` (**no status condition**) | `isTerminalStateRefusal` over `terminalStateServerCodes` | "keeps the unbind dialog's own three-code set, never merged with the stale set" (8 rows incl. `MutationRefused` false) | yes |
| `write-policy.ts:108-119` `failureMode` 5-way table (5xx → retryable; `validation_failed` → fields; other `ServerRefusal` → authoritative; `Network` → retryable; `UnexpectedResponse` → transient ? retryable : authoritative; default → authoritative) | `failureMode` (moved with its `FailureMode` type) | "reproduces write-policy.ts's five-way table" (10 rows) | yes |

The plan's decision P1 records why `isStaleRefusal`'s server arm carries **no** status rule and
**not** `stale_revision`: every base site was status-insensitive and `stale`-only, so a status rule
would classify a 500/`stale` refusal differently than its base site (clause 3's failure control),
and widening the set would widen a policy base never had. `stale_revision` is pinned where it
lives — the class arm (mutant d2, §7).

## 4. Guidance tables and the witness gap

Three named tables replace the three local maps and both local wrappers; the fold moved from
`api.ts` with its `ownValue` step lookup. A byte-comparison of every entry against base
(script-extracted from both revisions' sources): `resourceRefusalGuidance` 9 entries — keys and
values identical; `planningRefusalGuidance` 8 — identical; `assignmentRefusalGuidance` 4 —
identical; 21 entries, 12 distinct codes.

Base map rule (binding, contract's W4-review reconcile): no base map unless every surface that can
reach a code carries the identical step. The six codes two or three surfaces share (`stale`,
`state_conflict`, `not_found`, `provider_owned`, `validation_failed`, `forbidden`) each carry
different step text per surface, so the three tables stay — proven by the base maps themselves and
pinned by the 21 rendered-sentence rows.

Witnesses before this slice: 8 of the 21 steps had an asserting surface test —
`state_conflict` `connections.test.tsx:1371`, `not_found` `:1354`, `not_connected` `:1247`,
`capability_not_granted` `:1266`, `provider_unauthorized` `:611`, `provider_failure` `:1282`,
`goal_authority_required` `planning.test.tsx:783`, `invalid_request` `planning.test.tsx:221`. The
other 13 (resources `stale`/`forbidden`/`validation_failed`; planning
`stale`/`state_conflict`/`not_found`/`provider_owned`/`validation_failed`/`forbidden`; all four
assignment steps) had none. `refusals.test.ts` closes the gap: three cases pin the joined output of
**every** entry of **every** table (`"… (code) — step"`), one pins the
`planningRefusalGuidance` default, one pins the fallbacks (unlisted code, `constructor`/`toString`
prototype names on two tables, a non-`ServerRefusal` message, a bare `Error`'s generic sentence).

Callers: `resources.tsx` (3), `connections.tsx` (5) and `assignment.tsx` (2) pass their table
explicitly; `editors.tsx` (4), `projects.tsx` (1), `work-surface.tsx` (1) and `planning.test.tsx`
(1) keep one-argument calls served by the `planningRefusalGuidance` default (decision P3).

## 5. Control-flow audit (lesson 32, contract clause 2's base-vs-HEAD list)

| # | decision point | base | HEAD | preserved |
| --- | --- | --- | --- | --- |
| C1 | chat send refusal branch | `const refused = definitiveRefusal(error)` → `refused ? {kind:"refused"} : {kind:"unconfirmed", source: …}` + `frozen` clear | `isDefinitive(error)` in the same ternary, same arms | yes |
| C2 | chat older-page read branch | `definitiveReadRefusal(error)` → refetch gate + cursor rewind arms (both `instanceof` early-outs inside the callback) | `isDefinitiveReadRefusal(error)` in the same two arms | yes |
| C3 | goal command settle | `if (!outcomeUnknown(error)) releaseAttempt()` (and `sentApproval.current = null` in the approval mutation) | `isUnknownOutcome(error)`, same polarity and comment | yes |
| C4 | goal `RefusalNotice` tone | `refusal.stale` (getter) → `notice-conflict` vs `notice-error`, the stale detail paragraph and the button | `isStaleRefusal(refusal)` — same value for every `MutationRefused` | yes |
| C5 | control attempt settle | `definitiveRefusal(error)` → `clearActionId` (4 mutations) and `failurePatch`'s refusal-vs-unconfirmed split | `isDefinitive(error)`, same sites | yes |
| C6 | resources dialog freeze | `stale` (`code === "stale"`) / `terminal` (`stale \| state_conflict \| not_found`) → `frozen` by intent kind, `blocked`, `submitBlocked` | `isStaleRefusal`/`isTerminalStateRefusal`, same ternaries | yes |
| C7 | connections editor freeze | `stale` → `blocked`, fieldset `disabled`, the appended stale sentence | `isStaleRefusal`, same arms | yes |
| C8 | assignment freeze | `stale` → `blocked`, 5 `disabled` props, the appended sentence | `isStaleRefusal`, same arms | yes |
| C9 | planning `failureMode` consumers | `authoritative` → editor closes + notice; `retryable` → frozen intent + retry; `fields` → field errors | identical (function moved verbatim) | yes |
| C10 | fold | `!(error instanceof ServerRefusal)` early return → `errorMessage`; `ownValue` step lookup; `(code)` + conditional ` — step` | byte-identical body; the default parameter (planning table) replaces the deleted planning wrapper's map argument | yes |
| C11 | `Unsupported` and the 499/500 boundary inside `isDefinitive` | present but unexercised by the chat/control suites | present; pinned only by `refusals.test.ts`'s truth table (stated as a limit, §13) | yes |

## 6. Gates (all foreground, serialized; no measurement harness beside a suite)

Host note: every `pnpm` invocation runs as `mise x pnpm@12.17.0 -- pnpm --trust-lockfile
--config.verify-deps-before-run=false …`. The host's npm feed cannot serve three versions the
merged `#240` bump pinned — `knip@6.46.1`, `@vitest/coverage-v8@5.1.1`, `msw@3.0.7` (feed 404,
absent from the local store; the store holds `6.45.1`/`5.1.0`/`3.0.5`) — so each install ran with a
temporary `overrides` block in `pnpm-workspace.yaml` pinning exactly those three to the
store-available versions; `pnpm-workspace.yaml` and `pnpm-lock.yaml` were restored immediately
(`git status --porcelain` re-checked empty), and no gate tool is affected (§13). The first install
left a stale second `vitest` instance behind (`node_modules/.pnpm/vitest@5.1.0_@types+node@26_a41a…`
while the frontend linked `…_5750e1f3…`), which breaks `@testing-library/jest-dom`'s
`declare module "vitest"` augmentation for `tsc` (705 `TS2339` matcher errors in ten untouched
suites); a clean reinstall (delete `node_modules`, install again) yields one instance with
jest-dom's peer pointing at it, and both `tsc` 7.0.2-native and `tsc6` 6.0.3 pass.

Baseline in a detached `git worktree` at `7f34d40d` with the same install method (so the
comparison is apples-to-apples), then head rows on the frozen tree:

| # | command | base (`7f34d40d`) | head |
| --- | --- | --- | --- |
| 1 | `pnpm --filter @symmetry/frontend typecheck` (`tsc --noEmit`, TS 7.0.2 native) | exit 0, 0 errors | exit 0, 0 errors |
| 1b | `frontend/node_modules/.bin/tsc6 --noEmit` (TS 6.0.3, report-only cross-check) | exit 0 | exit 0 |
| 2 | `pnpm --filter @symmetry/frontend lint` (`--max-warnings 229`) | exit 0, **218** warnings, 0 errors (69 `no-unnecessary-condition`, 40 `restrict-template-expressions`, 27 `no-invalid-void-type`, 14 `no-non-null-assertion`, 12 `no-deprecated`, …) | exit 0, **218** warnings, 0 errors — ceiling not raised |
| 3 | `… exec vitest run src/services/refusals.test.ts src/features/chat/presentation.test.ts src/features/work/receipts.test.ts src/features/planning/planning.test.tsx src/services/api.test.ts` | 4 files / **147** tests (without `refusals.test.ts`), exit 0 | 5 files / **159** tests (147 + the new suite's 12), exit 0 |
| 4 | `… exec vitest run src/features/chat/chat.test.tsx src/features/chat/chat-hooks.test.tsx src/features/connections/connections.test.tsx src/features/goals/goal-attention.test.tsx src/features/work/assignment.test.tsx src/features/work/detail.test.tsx src/features/work/control.test.tsx` | — (unmodified files) | 7 files / **173** tests, exit 0 (chat 48, chat-hooks 9, connections 57, goal-attention 29, assignment 10, detail 7, control 13) |
| 4b | the same set + `src/app/router.test.tsx`; then `src/features/projects/filters.test.ts src/features/projects/saved-views.test.ts src/features/runtimes/runtimes.test.tsx src/components/primitives.test.tsx` | — (unmodified files) | 8 files / **215** tests, exit 0, and 4 files / **33** tests, exit 0 |
| 5 | `pnpm --filter @symmetry/frontend test` | 22 files / **556** tests, exit 0, 32.06 s | 23 files / **568** tests, exit 0, 29.93 s (556 + 12) |
| 6 | `git diff --check` | exit 0 | exit 0 |
| 7 | `pnpm --filter @symmetry/frontend build` (CI's frontend job runs it; report-only here) | — | exit 0, `✓ built in 6.59s`, the pre-existing >500 kB chunk warning only |
| 8 | DLP greps over added lines and the new files (profile-path pattern, backslash-u) | — | 0 matches (45 tracked added lines; four new files scanned whole) |

Suite arithmetic: base 556 = 449 line-anchored `it()` sites + 107 `it.each` expansion cases
(`grep -cE '^\s*it\('` over `frontend/src` at base = 449); head 568 = 461 sites + the same 107
expansions (the 12 new `refusals.test.ts` cases; no existing case was edited — the two owned test
files' diffs are import hunks only, §12).

### 6.1 The loaded-host full-suite runs on the frozen bytes

The full suite ran five times on the exact frozen tree (delivered-file hashes unchanged throughout,
`sha256sum -c` green) and twice at the baseline:

| run | tree | result | duration | failing cases |
| --- | --- | --- | ---: | --- |
| base2 (baseline worktree) | `7f34d40d`, no slice edits | 22 files / **556** passed, exit 0 | 32.06 s | — |
| head gate run | frozen head | 23 files / **568** passed, exit 0 | 29.93 s | — |
| final post-mutation run | frozen head | 568 = 2 failed + 566, exit 1 | 123.82 s | `router.test.tsx > workspace navigation > does not report unloaded project work as zero…`, `goal-attention.test.tsx > attention entry point > lists Goal attention and the project blocked work together` |
| recheck run | frozen head | 568 = 3 failed + 565, exit 1 | 90.32 s | the two above plus `goal-attention.test.tsx > attention entry point > renders a served Goal-feed refusal statement…` and `assignment.test.tsx > work assignment > offers only the item's project's resources of the matching kind` |
| base3 (baseline worktree) | `7f34d40d`, no slice edits | 556 = 1 failed + 555, exit 1 | 91.99 s | `goal-attention.test.tsx > attention entry point > lists Goal attention and the project blocked work together` |
| head3 | frozen head | 568 = 1 failed + 567, exit 1 | 83.74 s | `work/detail.test.tsx > work detail conversation entry point > links the execution's run into the conversation scope the chat route serves` |
| head4 | frozen head | 568 = 4 failed + 564, exit 1 | 142.43 s | one case each in `planning.test.tsx`, `runtimes.test.tsx`, `work/control.test.tsx`, `work/detail.test.tsx` |
| base4 (baseline worktree) | `7f34d40d`, no slice edits | 556 = 2 failed + 554, exit 1 | 132.19 s | one case in `chat.test.tsx`, one in `connections.test.tsx` |
| head retry 5 | frozen head | 568 = 1 failed + 567, exit 1 | 86.58 s | `work/detail.test.tsx > … links the execution's run into the conversation scope the chat route serves` |
| head retry 6 | frozen head | 568 = 1 failed + 567, exit 1 | 99.92 s | `goal-attention.test.tsx > attention entry point > lists Goal attention and the project blocked work together` |
| head retry 7 | frozen head | 568 = 1 failed + 567, exit 1 | 95.41 s | `router.test.tsx > workspace navigation > does not report unloaded project work as zero…` |
| variant `--maxWorkers=3` | frozen head | 568 = 1 failed + 567, exit 1 | 134.58 s | `work/control.test.tsx > control mutation boundary > renders the server's typed refusal as a server refusal, never as a runtime rejection` |
| isolation run of the failing files | frozen head | 2 files / **71** passed, exit 0 | — | — |
| head final run (after the baseline worktree was removed) | frozen head | 23 files / **568** passed, exit 0 | 90.75 s | — |

The red runs' durations (84–142 s against the green runs' 30–32 s) and the failing cases (all
"element/heading appears within the wait" assertions) place them in the recorded
wall-clock-budget class: `docs/plans/2026-11-21-rf-w1-10-frontend-eslint-plan.md:288-293` records
two of these very cases failing under extra transform cost while both files pass in isolation, and
the host was measured busy during the red runs (`top`: `rustc`, `postgres`, `Docker Desktop` and
`node` processes from other workstreams on the shared 16-core host; `git worktree list` shows five
other active slice worktrees). **The baseline tree fails the same way under the same load with a
different case subset each run** (base3: goal-attention; base4: chat + connections) — the reds are
the host's, not the slice's. Every failing case in every run belongs to a suite that passes in the
head gate run, the baseline green run, the focal runs of §6 rows 3–4b, and isolation; no assertion,
wait or test was edited, and the delivered bytes were hash-verified between all runs.

Gate verdict across every run: typecheck green on every attempt (exit 0, 0 errors, twice),
`tsc6` green, lint green on every attempt (exit 0, 218 warnings, three times), the focused suites
green on every attempt (147 / 159 / 173 / 215 / 33 / 71 tests), `pnpm build` green, `git diff
--check` green, DLP greps clean. **The full-suite gate is green on the frozen bytes in 2 of the 10
runs — 23 files / 568 tests, exit 0 at 29.93 s on an idle host and again at 90.75 s on the loaded
host (the final run, after the baseline worktree was removed)** — and red in the other 8, all inside
oversubscribed windows. The unchanged baseline tree is red there the same way, and the failing case
is different in nearly every run (eight head red runs hit seven distinct suites — router,
goal-attention, assignment, detail, planning, runtimes, control — while the baseline's red runs add
chat and connections). A lower-parallelism variant (`--maxWorkers=3`) stayed red in the same class
at 134.58 s, so the reds track the host's contention rather than the pool size, the slice's bytes or
any one suite.

## 7. Mutation flips (each applied alone, run, reverted; hash-verified revert)

Every flip below is a real run. The revert of each flip over a delivered file was verified by
`sha256sum` against the pre-round hash (`refusals.ts` `949748bf…`); the two `commands.ts` flips
(judging-only edits to a file this slice does not deliver) were verified by
`git status --porcelain frontend/src/services/commands.ts` returning empty, and the delivered-file
hash set was re-verified as a whole after the round (all 16 files match).

| mutant | flip | red (measured, exit 1 each) |
| --- | --- | --- |
| a (contract a) | `Forbidden` deleted from `isDefinitive` | `refusals.test.ts` — 1 failed / 11 passed: the `Forbidden` row of the `isDefinitive` truth table |
| b (contract b) | `isDefinitiveReadRefusal` widened to any `UnexpectedResponse` (the `status === 400` condition dropped) | `refusals.test.ts` — 1 failed / 11 passed: the `UnexpectedResponse 404` row |
| c (contract c) | `isUnknownOutcome` false for `UnexpectedResponse` | `refusals.test.ts` + `goal-attention.test.tsx` — 2 failed / 39 passed: the truth-table row and *goal detail surface > never announces success from an undecodable receipt* |
| d (contract d) | `stale` dropped from `MutationRefused.stale`'s code set (`commands.ts`) | `refusals.test.ts` + `goal-attention.test.tsx` — 3 failed / 38 passed: the class-arm truth table, *sends the served expected version and revision, and renders the stale conflict without overwriting*, *releases the plan approval attempt when the server answers its resolution* |
| d2 (added; the `stale_revision` pin's own mutant) | `stale_revision` dropped from the class set | `refusals.test.ts` — 1 failed / 40 passed: the `MutationRefused 409 stale_revision` row; `goal-attention.test.tsx` **passed**, confirming the contract's note that no upgraded surface case serves `stale_revision` |
| e (contract e) | `failureMode` returns `authoritative` for `status >= 500` | `planning.test.tsx` — 1 failed / 34 passed: *keeps a described server fault retryable and states what an invalid request does* (the `failureMode(ServerRefusal 500) === "retryable"` assertion) |
| f1 (contract f) | `capability_not_granted` dropped from `resourceRefusalGuidance` | `connections.test.tsx` — 1 failed / 56 passed: *renders the served capability refusal with the capability-specific next step* |
| f2 (contract f) | `goal_authority_required` dropped from `planningRefusalGuidance` | `planning.test.tsx` — 1 failed / 34 passed: *surfaces the server's Goal refusal when the loaded row predates the admission, then re-reads* |
| g (added) | `isStaleRefusal`'s server arm gains `status === 409` | `refusals.test.ts` — 1 failed / 11 passed: the `ServerRefusal 500 stale` (status-insensitivity) row |
| h (added) | the two server staleness sets merged (`isStaleRefusal` uses the terminal set) | `refusals.test.ts` — 1 failed / 11 passed: the same case (the `state_conflict` row turns true) |
| i (added) | the fold's `ownValue` step lookup replaced by a plain index | `refusals.test.ts` + `api.test.ts` — 2 failed / 69 passed: *keeps the frozen fallbacks*, and `api.test.ts`'s *adds a guidance step only for a code the map itself owns* (`:349`, the `constructor` row) |
| j (added) | the fold drops the `(code)` suffix and the ` — ` joiner | `refusals.test.ts` — 5 failed / 7 passed: all three per-table sentence pins, the default-table pin and the fallbacks pin |

## 8. Clause-1 definition-count greps (final tree)

Run over `frontend/src` (`--include=*.ts --include=*.tsx`):

1. `function isDefinitive|definitiveRefusal|definitiveReadRefusal|outcomeUnknown|isUnknownOutcome|isStaleRefusal|isTerminalStateRefusal|failureMode|FailureMode =` → 7 hits, all in
   `services/refusals.ts` (`:8`, `:13`, `:18`, `:22`, `:27`, `:31`, `:33`). The four base predicate
   names (`definitiveRefusal`, `definitiveReadRefusal`, `outcomeUnknown`, and the inline staleness
   checks) have **zero** hits.
2. `ResourceRefusalGuidance|refusalGuidance: Record<|assignmentGuidance: Record<|const refusalGuidance`
   → 3 hits, all in `services/refusals.ts` (`:43` mine, `:55`, `:66`); the three local maps are gone.
3. `refusalNotice = |function refusalNotice|refusalNotice as sharedRefusalNotice` → 1 hit,
   `services/refusals.ts:73`; the two local wrappers and the `api.ts` fold are gone (no
   `export { … } from "./refusals"` re-export exists anywhere).
4. `code === "stale"|code === "state_conflict"|code === "not_found"|stale_revision` outside
   `refusals.*` and `commands.ts` → 0 hits; the only remaining staleness rule text is the module's
   and `commands.ts`'s own getter.
5. Every use site of the nine module exports lives in the 11 callers listed in §9; no file imports
   `refusalNotice` from `api.ts` or `./resources` any more.

## 9. Import-line diff

| importer | base import | head import |
| --- | --- | --- |
| `services/api.test.ts:2-3` | `failureMode` from `../features/planning/write-policy`; `refusalNotice` from `./api` | both from `./refusals` |
| `features/planning/planning.test.tsx:8-9` | `failureMode`+`refusalNotice` inside the `./write-policy` import | own line from `../../services/refusals`; the domain helpers stay on `./write-policy` |
| `features/planning/editors.tsx:8` | both inside the `./write-policy` block | own line from `../../services/refusals` |
| `features/projects/projects.tsx:9-11` | `refusalNotice` from `../planning/write-policy` | `../../services/refusals` |
| `features/projects/work-surface.tsx:10-11` | `refusalNotice` from `../planning/write-policy` | `../../services/refusals` |
| `features/connections/connections.tsx:16,20` | `refusalNotice` from `./resources` | `isStaleRefusal`+`refusalNotice`+`resourceRefusalGuidance` from `../../services/refusals`; `./resources` keeps `DialogFrame`, `ProjectResources`, `providerLabel` |
| `features/connections/resources.tsx:15` | `refusalNotice as sharedRefusalNotice` from `../../services/api` | four exports from `../../services/refusals`; the local wrapper export removed |
| `features/work/assignment.tsx:15` | `refusalNotice` from `../../services/api` | three exports from `../../services/refusals` (the local map removed) |
| `features/goals/detail.tsx:11` | — | `isStaleRefusal`+`isUnknownOutcome` from `../../services/refusals` |
| `features/work/control.tsx:33` | — | `isDefinitive` from `../../services/refusals` |
| `features/chat/use-chat-scope-state.ts:6`, `use-older-page-loading.ts:5` | — | `isDefinitive` / `isDefinitiveReadRefusal` from `../../services/refusals` |

`services/refusals.ts` is the only module exporting the vocabulary; `api.ts` gained no import
change beyond the deletion (it still imports `MutationRefused` from `./commands`).

## 10. LOC delta (D2 — reported, never a gate)

Per-file line counts (`git show 7f34d40d:<file> | wc -l` vs `wc -l`): production files
`api.ts` −14, `use-chat-scope-state.ts` −4, `use-older-page-loading.ts` −4, `detail.tsx` −6,
`write-policy.ts` −38, `editors.tsx` −1, `resources.tsx` −17, `connections.tsx` ±0,
`projects.tsx` +1, `work-surface.tsx` +1, `assignment.tsx` −10, `control.tsx` −10; plus the new
`refusals.ts` +77. Tracked diff: +45/−146; tests: `planning.test.tsx` +1, `api.test.ts` ±0, new
`refusals.test.ts` +212.

- **Production net: −25 LOC** (143 removed, 118 added: 41 edits + the 77-line module).
- **Tests net: +213 LOC** (the new suite is the contract's E8 witness gap closure; D1 froze the
  oracles, so the pins had to land somewhere).
- The plan row's `−60…−100` is **not met as a net figure**, and D2 makes that report-only. The
  binding W4-review decision removed the mechanism the estimate assumed ("one shared guidance map
  with per-surface extensions" → no base map is legal while shared codes carry different steps), so
  the three tables' 21 step lines survive as module data; the removed duplication is the predicate,
  wrapper and map *implementations*, not the step text.

## 11. D1 hygiene delta list (base → HEAD, the only observable-effect classes)

1. **Prototype-named guidance codes on the planning surfaces.** Base's planning fold used a plain
   index (`refusalGuidance[error.code]`), so a `ServerRefusal` whose served `code` is a prototype
   member rendered `Object.prototype`'s member text as its step (`constructor` → `… (constructor) —
   function Object() { [native code] }`). HEAD's fold resolves steps with `ownValue` for every
   table, so such a code contributes no step — the behavior the resource/assignment folds and
   `api.test.ts:349` already pinned. No surface can produce a prototype-named code from the
   control plane's own vocabulary; the row is pinned in `refusals.test.ts`.
2. **Deletions of superseded comments** (comment policy item 4, in touched hunks only):
   `write-policy.ts`'s three-line `FailureMode` docstring and its in-body reference to
   `definitiveRefusal`, the two maps' "The server owns the reason; …" headers, the assignment map's
   two-line header, and the fold's `api.ts` docstring (its content moved into the module's structure
   and this record). `control.tsx`'s one-line predicate docstring went with the predicate.
3. Nothing else: no rendered sentence, joiner, `(code)` suffix, tone class, role, or appended
   per-dialog text changed — the pinned suites (§12) pass byte-identically, and the three surface
   tables were verified entry-by-entry against base by script.

## 12. Oracles untouched (byte-identical to `7f34d40d`)

`git diff --stat` over the oracle set is empty: `connections.test.tsx`, `goal-attention.test.tsx`,
`chat.test.tsx`, `chat-hooks.test.tsx`, `work/assignment.test.tsx`, `work/detail.test.tsx`,
`work/control.test.tsx`, `chat/presentation.test.ts`, `work/receipts.test.ts`, `app/router.test.tsx`.
The two owned test files changed on their import hunks only:

- `planning.test.tsx`: the diff is one hunk — the import split; every `expect(...)` line is
  unchanged (the `failureMode`/`refusalNotice` cases now sit at `:163`, `:217-218`, `:221`, `:783`,
  shifted one line by the added import).
- `api.test.ts`: the diff is one hunk — the two import lines; the `failureMode` case `:150` and the
  refusal-fold cases `:344-349` are unchanged.

## 13. Residual risks and known limits

- **Host environment.** Three pinned dev-dependency versions are unservable on this host (feed 404
  + absent from the store), so the local install is not the lockfile's exact tree: `knip` 6.45.1,
  `@vitest/coverage-v8` 5.1.0, `msw` 3.0.5 instead of 6.46.1/5.1.1/3.0.7. None participates in a
  gate (`knip` is report-only by the W4-09 record; coverage is off; `msw` 3.0.5 is the version the
  base suites were written against and every DOM suite passes with it). `pnpm-workspace.yaml` and
  `pnpm-lock.yaml` are byte-identical to HEAD in the delivered tree. The browser acceptance suite is
  CI-only and was not run here.
- **Unpinned-by-surface branches.** `isDefinitive`'s `Unsupported` and 499/500 boundary, and
  `isStaleRefusal`'s status-insensitivity, have no production path exercised by a pinned suite; the
  module truth table is their only pin (declared as coverage, not proof).
- **`stale_revision` on the write surfaces.** No control-plane write surface serves it; the class
  arm's pin is the only witness (mutant d2 showed `goal-attention.test.tsx` stays green).
- **Comment-policy reading of the contract's "`write-policy.ts` re-imports `failureMode`/
  `refusalNotice`".** `write-policy.ts` has no internal caller of either symbol, so an import there
  would be dead; the sentence is realized as "every importer re-imports from `refusals.ts`" with no
  re-export shim (plan decision P3, clause 1's clean-cutover requirement).
- **`docs/TODO.md:645`** cites `frontend/src/features/planning/write-policy.ts:251`; the deletion
  shifts that line number, and the entry's own cleanup revalidates the citation (plan decision P10).
  No TODO entry is closed by this slice, so `docs/TODO.md` and `docs/archive/CLOSED-TODO.md` are
  untouched.
- **knip (report-only).** Not re-measured after the host install limitation; the module's exports
  are all consumed by the 11 callers except `isTerminalStateRefusal`'s single call site, which the
  contract mandates.
