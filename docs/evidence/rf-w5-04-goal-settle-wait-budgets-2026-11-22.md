# rf-w5-04-goal-settle-wait-budgets — evidence record (2026-11-22)

Subject: the Goal acceptance settle is driven by the goal query's own `refetchInterval` while a
settle is pending — no private 120 ms / 2,500 ms poll — with the frozen notice semantics
(confirmation or the bound, failures neither confirm nor abort, both follow-up invalidations after
the notice), and the frontend carries an explicit `testTimeout: 15_000` with every owned wait
strictly below its enclosing budget. Contract: `goal-rf-w5-04-goal-settle-wait-budgets.md` (GDD,
2026-11-21, binding with decision C, the strict owned set, the lesson-32 control-flow audit, the
mutation-proof rule and the owner comment policy of 2026-11-22). Base revision `42afb56c`
(`refactor(frontend): own read-cache keys in one module (#245)`, this worktree's dispatch HEAD,
tree clean). Plan: `docs/plans/2026-11-22-rf-w5-04-goal-settle-wait-budgets-plan.md`. Judging
artifact (contract clause 2, "answer.md"): this record plus the implement-stage report.

Host note: every `pnpm` invocation ran as `mise x pnpm@12.17.0 -- pnpm --trust-lockfile
--config.verify-deps-before-run=false ...` (the workspace pins pnpm 12.17.0; the verify step is
skipped because the local install carries the three override versions below). The host's npm feed
404s `knip@6.46.1`, `@vitest/coverage-v8@5.1.1` and `msw@3.0.7` (the merged #240 bump), so the
one-time `pnpm install --no-frozen-lockfile` ran with the documented temporary `overrides` block
in `pnpm-workspace.yaml` pinning exactly those three to `6.45.1`/`5.1.0`/`3.0.5`; both
`pnpm-workspace.yaml` and `pnpm-lock.yaml` were restored immediately after the install
(`git status --porcelain` empty, re-checked), and none of the three is a gate tool.

## 1. Deliverable shape (files, counts)

| file | status | lines | note |
| --- | --- | ---: | --- |
| `frontend/src/features/goals/detail.tsx` | edited | 757 → 778 | the settle conversion only: the goal query's `refetchInterval` while a settle is outstanding and `refetchIntervalInBackground` (`:82-83`), the outstanding-settle count (`:78-79`, incremented/decremented at `:92`/`:96`), `goalMarker` (`:47-49`), `waitForGoalSettle` (`:51-73`), `settleGoalRead` (`:91-98`), the two call sites' compressed comments (`:171`, `:227`) |
| `frontend/vitest.config.ts` | edited | 39 → 41 | the explicit `testTimeout: 15_000` (`:24-25`) |
| `frontend/src/features/goals/goal-settle.test.tsx` | new | 0 → 339 | 8 cases: the six settle cases + the two budget-guard cases (the apply pass of §12 added the overlapping-settle and unfocused-document cases and extended cases 1 and 4; the fix pass of §13 widened the shared reads-stopped probe and unified the before-bound thresholds; the F1–F4 pass of §14 named the two inline budgets and relaxed the two exact moved-read counts) |
| `frontend/src/features/goals/goal-attention.test.tsx` | **unedited** | 1,321 | 29 case names, the eight 5,000 ms waits and the seven `}, 15_000)` budgets byte-identical |
| `frontend/src/tool-cache-config.test.ts` | **unedited** | 71 | oracle; its two cases keep their own 30,000 ms budgets |
| `frontend/src/features/connections/connections.test.tsx` | **unedited** | — | oracle; its three wait sites keep 5,000 ms under the new 15,000 ms default |
| `frontend/src/features/chat/chat.test.tsx` | **unedited** | — | oracle; its 8,000 ms wait stays under its `}, 20_000)` case budget |
| `docs/plans/2026-11-22-rf-w5-04-goal-settle-wait-budgets-plan.md` | new | 243 | the plan |
| `docs/evidence/rf-w5-04-goal-settle-wait-budgets-2026-11-22.md` | new | — | this record |

Not edited: `app/application.ts` (the options overlay lives at the call site), `app/realtime.tsx`
and `services/realtime.ts` (the epoch contract), `services/api.ts`, `docs/TODO.md` /
`docs/archive/CLOSED-TODO.md` (no entry closes), `docs/design/**` (no document names the settle
wait — checked), `contracts/**`, `browser/**`, `pnpm-lock.yaml`, `pnpm-workspace.yaml`.

## 2. Dispatch gates (base `42afb56c`)

| gate | state |
| --- | --- |
| `#164`, W3-12 (`#190`), W4-11 (`#220`), W4-08 (`#232`), W4-09 (`#239`), W4-10 (`#241`) | merged (all ancestors of HEAD) |
| A5 (`#245`, this HEAD) | merged; `app/read-cache.ts` owns the key the settle's cache lookup uses |
| open PRs | `gh pr list --state open` → `[{"number":243,"title":"refactor(harness): drive adapter sessions through one staged driver"}]`; its files are `daemon/internal/harness/**` + `docs/TODO.md` + its own plan/evidence — none in the owned set |
| `vitest.config.ts` at dispatch | 39 L, no `testTimeout` (the contract's re-derivation holds) |
| owned files untouched by other work | `git status --porcelain` empty |

Re-derivation deltas vs the contract's coordinates: `goals/detail.tsx` 757 L, `settleGoalRead`
`:61-74` (doc comment `:50-60`; contract `:65-78`/`:57-64`), decision points `:62`/`:71`, callers
`:147-155` and `:205-212`; `goal-attention.test.tsx` wait sites `:360,408,426,791,877,983,1073,1158`
inside the `}, 15_000)` cases at `:361,432,793,878,984,1074,1159`; `connections.test.tsx` sites
`:878,918,935` (no explicit case budget — the equal-budget family); `chat.test.tsx` site `:1276`
inside the `}, 20_000)` case at `:1283`; `tool-cache-config.test.ts` 71 L with its two
`{ timeout: CONFIG_IMPORT_TIMEOUT_MS }` = 30,000 ms case budgets and no same-sized inner wait.

## 3. The settle conversion (delivered shape)

- **Drive.** `useQuery({ ...goalOptions(application.api, goalId), refetchInterval: settlesPending > 0 ? settleReadIntervalMs : false, refetchIntervalInBackground: true })` (`:80-84`): the goal query keeps its key and its
  `api.goal` `queryFn`; the interval exists while at least one settle is outstanding
  (`settlesPending`, `:78-79`), so it is cleared on confirmation, on the bound and on unmount
  (the observer is destroyed with the surface) — and not by the first of two overlapping
  settles, which the pre-apply single flag did (apply pass, §12; pinned by case 5, mutant (k)).
  `refetchIntervalInBackground` keeps the reads flowing while the document is unfocused, as
  base's private loop did (pinned by case 6, mutant (l)).
- **Observation.** `waitForGoalSettle(queryClient, goalId, before)` (`:51-73`) subscribes to
  `queryClient.getQueryCache()`, keeps only `updated` events of the goal query
  (`event.query === cache.find({ queryKey: goalReadKey(goalId), exact: true })`), treats `success`
  as a read result and `error` as a failed read, and ignores `fetch`/`failed` (a retrying attempt)/
  `invalidate`/`setState`. The comparison is base's verbatim
  `marker !== null && marker !== before && previous === marker` → confirmed, else `previous = marker`;
  a failed result resets `previous = null`; the bound is one `setTimeout` (`:66`).
- **Unseeded by construction.** v5 dispatches a fetch's `success` action before its promise settles
  and `await invalidateQueries(...)` resolves after that promise, so the invalidate-triggered result
  is past when the settle subscribes: the settle observes exactly the results delivered while it is
  pending — two of *its* reads, as at base.
- **Constants.** `settleReadIntervalMs = 120`, `settleReadBoundMs = 2_500` (`:43-45`) — the frozen
  numbers.

## 4. Base → HEAD control-flow audit (lesson 32; preserved yes/no)

| row | base (`:61-74` + callers) | HEAD | preserved |
| --- | --- | --- | --- |
| pre-command marker | `acknowledged === undefined ? null : [version, state, current_revision].join("|")` (`:63`) | `goalMarker(acknowledged)` (`:47-49`, called `:94`) — same join, `undefined → null` | yes |
| per-read fetch | private `application.api.goal(...).catch(() => null)` GET (`:66`) | the goal query's own `api.goal` read, driven by the interval (`:82`) | carrier changed (decision C) |
| read result observed | each loop iteration's read | each `success` cache result | yes |
| confirmation | `:68` | `:62` | yes (verbatim) |
| chain bookkeeping | `previous = marker` (`:69`) | `:63` | yes |
| failed read | `catch(() => null)` → `marker = null` → `previous = null`; neither confirms nor aborts (`:66-69`) | `error` result → `previous = null` (`:59`); neither confirms nor aborts; a failed *attempt* inside a retrying fetch is internal to the query's own retry policy (`readRetry`, 500/1,000 ms) | semantics yes; attempt-level retries are the query's carrier |
| bound exit | `while (Date.now() < deadline)` with the loop's own sleeps (`:62`,`:65`,`:70-72`) | one 2,500 ms `setTimeout` on the wait (`:66`) | yes (the notice at the bound) |
| unmount during the wait | the private loop kept reading until the bound | the query's interval stops with the observer (no reads); the wait still resolves at the bound | **changed, authorized** ("cleared on unmount"; pinned by test case 4) |
| overlapping settles (apply pass, §12) | each caller's private loop read and confirmed independently, so two in-flight settles both confirmed promptly | `settlesPending` counts outstanding settles, so the interval outlives the first finisher and each settle keeps its own reads; the pre-apply single flag dropped the second settle to its bound | yes (restored by the apply pass; pinned by test case 5, mutant (k)) |
| notice then follow-ups (generic caller) | `:150-155`: acknowledged → `await invalidate(goalReadKey)` → `await settleGoalRead` → `setNotice` → `void invalidate(goalEvents)` → `void invalidate(attention)` (comment `:147-149`) | `:172-177`, byte-identical sequence | yes |
| notice then follow-ups (plan approval) | `:205-212` | `:228-233`, byte-identical sequence | yes |
| realtime epoch | not used by the settle | not used by the settle (`useRealtimeEpoch` stays at its receipts use, now `:749`) | yes |
| rendered surface | notice texts | unchanged (both strings byte-identical) | yes |

Accepted consequences of routing the reads through the query (decision C): a *failing* settle read
now sets the goal query's own error state, so the page's existing stale-error presentation applies
to it (base's private reads swallowed their failures); the query's retry schedule governs
transient attempt failures. Neither confirms nor aborts the wait.

## 5. The new settle cases (and the base characterization observed first)

`frontend/src/features/goals/goal-settle.test.tsx` — the implement stage's 6 cases (4 settle plus
2 budget); the delivered file carries 8 — §1, §12:

| case | pins | base run (before the source change) | head run |
| --- | --- | --- | --- |
| announces acceptance only after the moved marker holds across two reads of the goal query | the two-consecutive-reads confirmation, before the bound, with the follow-ups issued and the reads stopped after the notice (+ the apply pass's unrelated-cache-success injection, §12) | pass (frozen semantics) | pass |
| announces acceptance at the bound when the served marker never moves | the bound exit at ≥ 2,400 ms with the pending reads flowing through the query (> 3 reads), and the reads stop after the notice | pass | pass |
| neither confirms nor aborts on a refused goal read and breaks the consecutive chain | refusals neither confirm nor abort; a failed result breaks the chain (three moved reads needed) | pass | pass |
| clears the pending reads with the observer on unmount and still runs the follow-up invalidations | "cleared on unmount" + the two follow-up invalidations after a pending settle (+ the apply pass's pre-notice invalidation assertions, §12) | **fail** — `expected 16 to be less than 3` (the private poll kept reading after the unmount) | pass |
| keeps every wait in this file strictly below the effective per-test budget | the wait budgets vs the runner-resolved budget | **fail** — `expected 5000 to be 15000` | pass |
| carries the explicit per-test budget in vitest.config | the parsed config option | **fail** — `expected undefined to be 15000` | pass |

Base run of the final artifact (sources restored to `42afb56c`, test file as delivered): **3 failed
/ 3 passed (6)**, exit 1, run duration 29.36 s. Head run: **6 passed (6)**, exit 0; file duration
12,061 ms in the full-suite json (isolated runs: 9,111 ms and 14,951 ms warm; 24–35 s when the cold
config import lands in its worker — the config case's own 30 s budget covers it).

The apply pass (§12) added the overlapping-settle and unfocused-document cases (delivered cases 5
and 6, after this table's budget rows) and extended case 1 (the unrelated-cache-success injection)
and case 4 (the pre-notice invalidation assertions). The head/base columns above are the
implement-stage runs of the six cases as then delivered; §12 carries the apply-stage runs of all
eight.

## 6. Per-site wait-budget table (site → budget → enclosing budget)

| # | site | wait | enclosing | strict? |
| ---: | --- | ---: | --- | --- |
| 1 | `goal-attention.test.tsx:360` | 5,000 | 15,000 (`}, 15_000)` `:361`) | yes |
| 2 | `goal-attention.test.tsx:408` | 5,000 | 15,000 (`:432`) | yes |
| 3 | `goal-attention.test.tsx:426` | 5,000 | 15,000 (`:432`) | yes |
| 4 | `goal-attention.test.tsx:791` | 5,000 | 15,000 (`:793`) | yes |
| 5 | `goal-attention.test.tsx:877` | 5,000 | 15,000 (`:878`) | yes |
| 6 | `goal-attention.test.tsx:983` | 5,000 | 15,000 (`:984`) | yes |
| 7 | `goal-attention.test.tsx:1073` | 5,000 | 15,000 (`:1074`) | yes |
| 8 | `goal-attention.test.tsx:1158` | 5,000 | 15,000 (`:1159`) | yes |
| 9 | `connections.test.tsx:878` | 5,000 | 15,000 (global default; **was 5,000** — the equal-budget contradiction) | yes |
| 10 | `connections.test.tsx:918` | 5,000 | 15,000 (global; was 5,000) | yes |
| 11 | `connections.test.tsx:935` | 5,000 | 15,000 (global; was 5,000) | yes |
| 12 | `chat.test.tsx:1276` | 8,000 | 20,000 (`}, 20_000)` `:1283`) | yes |
| 13 | `goal-settle.test.tsx` `NOTICE_WAIT_MS` (`:16`; the notice waits of cases 1–3 and 6, and case 4's read wait) | 5,000 | 15,000 (global; asserted at runtime, `:329-330`) | yes |
| 14 | `goal-settle.test.tsx` `FOLLOW_UP_WAIT_MS` (`:17`) | 4,000 | 15,000 (global; asserted at runtime, `:329-330`) | yes |
| 15 | `goal-settle.test.tsx` `PRE_BOUND_WAIT_MS` (`:19`; case 5's inline notice wait and the elapsed thresholds of all six settle cases) | 2,400 | 15,000 (global; asserted at runtime, `:329-330`) | yes |
| 16 | `goal-settle.test.tsx` `READS_STOPPED_WINDOW_MS` (`:20`; the shared reads-stopped probe's window) | 800 | 15,000 (global; asserted at runtime, `:329-330`) | yes |
| — | `tool-cache-config.test.ts:48,59` (oracle) | no inner wait | 30,000 per case (sized from the cold import) | n/a |
| — | `goal-settle.test.tsx:333` config case | no inner wait | 30,000 (`CONFIG_IMPORT_BUDGET_MS`) | n/a |

The budget rule's runnable guards: `task.timeout === 15_000` for a case without an explicit budget
(reddens when the option is removed, mutation (c)), every wait budget of the new file asserted
strictly below that runtime number (`:329-330`), and the parsed `vitest.config.test?.testTimeout`
(`:337`). The guard loops over all four named budgets, so "every wait in this file" holds; the two
inline ones were named in the F1–F4 pass of §14. The eight `goal-attention` and one `chat` sites keep their numbers and their explicit
case budgets; the three `connections` sites are resolved by the global default with no site edit.
Not covered by a runnable check: the per-case-budget containment of the eight `goal-attention`
sites (their case budgets are explicit and this table records them; a source-text scan was
rejected as a test of source text rather than behavior).

## 7. Gate rows (all foreground, serialized; exit codes)

| # | command (cwd `symmetry`, pnpm via `mise x pnpm@12.17.0 -- ... --filter @symmetry/frontend`) | base | head |
| --- | --- | --- | --- |
| 1 | `run typecheck` (`tsc --noEmit`) | exit 0 | **exit 0** |
| 2 | `run lint` (`eslint . --max-warnings 229`) | exit 0 — 218 warnings, 0 errors | **exit 0 — 219 warnings, 0 errors** (+1: the new file's `no-invalid-void-type` on `Promise.withResolvers<void>()`, the repo's existing pattern — 26 pre-existing warnings of that rule) |
| 3 | `exec vitest run src/features/goals/goal-attention.test.tsx` | 29 passed; file 17,012 ms (in-suite json), isolated run 22.70 s | **29 passed; file 14,973 ms isolated, 17,307 ms in-suite json** (the contract's recorded base was 13.4 s; host variance) |
| 4 | `exec vitest run src/features/goals/goal-settle.test.tsx` | 3 failed / 3 passed (the artifact's base characterization) | **6 passed; file 9,111 ms isolated (12,061 ms in-suite json)** |
| 5 | `exec vitest run src/tool-cache-config.test.ts` | 2 passed; file 788 ms (in-suite) | **2 passed; file 877 ms isolated (2,153 ms in-suite: the cold import landed there this run)** |
| 6 | `exec vitest run src/features/chat/chat.test.tsx src/features/connections/connections.test.tsx` | 105 passed | **105 passed** (connections 26,473 ms / chat 29,577 ms isolated; in-suite 36,555 ms / 18,866 ms — host noise) |
| 7 | `run test` (full suite; json reporter variant for the per-file table) | 24 files / 605 tests passed, run 64.94 s (json) | **25 files / 611 tests passed** (+1 file / +6 tests = the new settle file); json run 79.31 s, final `pnpm test` run on the settled tree 72.34 s |
| 8 | `git diff --check` | exit 0 | **exit 0** |

Full-suite per-file durations (ms, base → head), from the json reports:
`application.test.ts` 78→63, `legacy.test.ts` 5→7, `read-cache.test.ts` 15→12, `router.test.tsx`
10,111→9,942, `primitives.test.tsx` 615→611, `chat-hooks.test.tsx` 361→346, `chat.test.tsx`
22,144→18,866, `chat/presentation.test.ts` 41→44, `connections.test.tsx` 28,358→36,555,
`goal-attention.test.tsx` 17,012→17,307, `goal-settle.test.tsx` –→12,061, `planning.test.tsx`
14,391→14,053, `filters.test.ts` 15→12, `saved-views.test.ts` 19→12, `runtimes/presentation.test.ts`
15→17, `runtimes.test.tsx` 930→859, `assignment.test.tsx` 4,398→3,761, `control.test.tsx`
3,194→2,780, `work/detail.test.tsx` 2,744→1,318, `work/presentation.test.ts` 26→24,
`receipts.test.ts` 13→12, `api.test.ts` 253→225, `realtime.test.ts` 80→43, `refusals.test.ts`
13→13, `tool-cache-config.test.ts` 788→2,153. Counts and statuses are the gate; durations are
report-only (the contract records the host as shared).

## 8. Mutation runs (each applied alone to the final tree, run, reverted hash-verified)

Every flip was applied to the delivered tree, the new settle file re-run, and the tree restored by
byte copy with `sha256sum -c` (all three delivered files OK after each revert).

| mutant | flip | result |
| --- | --- | --- |
| (a) | two-consecutive requirement dropped (`previous === marker` removed) | 2 failed / 4 passed: case 1 `expected 1 to be 2`, case 3 `expected 1 to be 3` |
| (b) | moved-marker requirement dropped (`marker !== before` removed) | 3 failed / 3 passed: case 1 `expected +0 to be 2`, case 2 `expected 689 to be >= 2400`, case 4 `expected 731 to be >= 2400` (the notice confirmed on the unmoved marker) |
| (c) | explicit `testTimeout` removed from `vitest.config.ts` | 2 failed / 4 passed: budget guard `expected 5000 to be 15000`, config guard `expected undefined to be 15000` |
| (d) | settle query's refetch wiring removed (`refetchInterval: false`) | 4 failed / 2 passed: case 1 `expected +0 to be 2`, case 2 `expected 1 to be greater than 3`, case 3 `expected +0 to be 3`, case 4 `expected 1 to be greater than 2` (the notice fell back to the bound) |
| (e) | hand-rolled private poll reintroduced (`setInterval` issuing `api.goal`) | 3 failed / 3 passed: case 1 `expected 4 to be 2`, case 3 `expected 2 to be 3`, case 4 `expected 18 to be less than 3` (reads continued after the unmount) |
| (f) | chain not reset on a failed result | 1 failed / 5 passed: case 3 `expected 2 to be 3` |
| (g) | failed result resolves the wait | 1 failed / 5 passed: case 3 `expected +0 to be 3` |

No test name or assertion was deleted or weakened; `goal-attention.test.tsx` is byte-identical, so
no converted assertion needs a mutation record.

## 9. Removed-poll proofs, DLP, inventory

- `grep -n "api\.goal(" frontend/src/features/goals/detail.tsx` → one site, `:117`, inside
  `servedEnvelope` (the submit-time read, unchanged); the settle block has none.
- `grep -n "setInterval\|setTimeout\|Date\.now\|refetchInterval" frontend/src/features/goals/detail.tsx`
  → `:66` (the single bound `setTimeout`), `:82` (the query's own `refetchInterval`), `:83`
  (`refetchIntervalInBackground`). No `setInterval`, no `Date.now()`, no recursive `setTimeout`,
  no `2_500` loop.
- `grep -n "useRealtimeEpoch" frontend/src/features/goals/detail.tsx` → import `:7`, use `:749`
  (the receipts catch-up) — untouched; `services/realtime.ts` and `app/realtime.tsx` are not in the
  diff.
- DLP: a case-insensitive profile-directory-path grep over the diff and over this record's
  companions → no match (exit 1, re-checked after this record was written); no backslash-u
  sequence in any added line of the changed/new files (exit 1).
- Inventory: `git status --porcelain -uall` lists exactly the plan, the evidence record, the new
  test file and the two edited files (`detail.tsx`, `vitest.config.ts`); `git diff --stat` is
  `detail.tsx` 72 changed lines (+46/−26 across the diff, `git diff --numstat`) and
  `vitest.config.ts` +2. The apply pass's own deltas are in §12.1.

## 10. Residual risks and known limits

- **Failing settle reads are now visible.** A goal read that fails during a settle sets the query's
  error state, so the page's existing stale-error presentation applies where base swallowed the
  failure inside its private poll. Authorized by decision C; the four frozen notice semantics are
  unchanged and mutation-pinned.
- **Retry carrier.** A transient attempt failure inside a retrying fetch no longer breaks the
  consecutive chain (base's per-attempt `catch → null` did); only an exhausted read failure does.
  Both never confirm during failures; the confirmation after a failure burst costs the query's own
  retry schedule. Recorded in the plan §11, not traded.
- **`testTimeout: 15_000` loosens hang detection** for every un-budgeted test (the contract's
  recorded accepted cost).
- **Wall time is report-only.** The five replay cases in `goal-attention.test.tsx` legitimately wait
  the frozen 2.5 s bound; its file duration is 17.0 s → 17.3 s in-suite (14,973 ms isolated at
  head), inside the same band as base. The new file adds two bound-length cases; the full-suite run
  grows by the new file and one extra cold config import (~8.2 s measured; the config case's own
  30 s budget covers it).
- **Per-case-budget containment is table-checked, not runtime-checked**, for the eight
  `goal-attention` sites (their budgets are explicit and recorded in §6); the runtime guards cover
  the parsed global option and the new file's own waits.
- **Cadence pin strength (apply pass, simplify finding 9).** The frozen 120 ms cadence is pinned
  loosely: a 120 → 240 ms variant passes all eight cases (case 1 confirms in ~0.96 s against its
  2,400 ms assertion), and only roughly 2× reddens case 1, so the effective pin is "much faster
  than the 2.5 s bound". No timer-exact test was added (flake-prone); recorded as the residual.
- **Unfocused document (apply pass, simplify finding 11).** The interval reads while the document
  is unfocused (`refetchIntervalInBackground: true`), as base's private loop did; jsdom observes
  it only through `focusManager.setFocused(false)` (case 6, mutant (l)). No browser experiment was
  run, so real-tab timer throttling is not measured here.
- **Overlapping settles (apply pass, simplify finding 10).** The pre-apply single pending flag let
  the first of two overlapping settles clear the interval under the second, which then always
  resolved at its bound (base's independent loops confirmed both). The counted fix is pinned by
  case 5 and mutant (k); the overlap trigger itself is a test-only construction (two operator
  commands inside the first settle's window).
- **Host contention.** The gate rows record wall times under same-host conditions (other worktrees
  run in parallel); counts and names, not the wall clock, are the gates.

## 11. Implement-stage re-verification (2026-11-22, second pass on the same artifact bytes)

Bytes under verification: `detail.tsx` `7df9cafb…`, `vitest.config.ts` `661cbe33…`,
`goal-settle.test.tsx` `b9074eb4…` (sha256; every mutant flip below applied alone to these bytes
and reverted under `sha256sum -c`). All rows foreground and serialized. The test file gained one
assertion after the first pass: case 2 now also observes that the goal reads stop after the bound
notice (the third "cleared" point of clause 1; §7 case 2, mutant (h)) — the source bytes are
unchanged from the first pass.

| # | command | re-run result (exit code) |
| --- | --- | --- |
| 1 | `run typecheck` | exit 0 |
| 2 | `run lint` | exit 0 — 219 warnings, 0 errors (`--max-warnings 229` unchanged) |
| 3 | `exec vitest run src/features/goals/goal-attention.test.tsx` | 29 passed (29); rc 0; 24.86 s isolated, 15,812 ms in-suite (base recorded: 13.4 s contract, 17,012 ms in-suite; host-load variance, report-only) |
| 4 | `exec vitest run src/features/goals/goal-settle.test.tsx` | 6 passed (6); rc 0; 16.11 s isolated, 9,858 ms in-suite (final bytes) |
| 5 | `exec vitest run src/tool-cache-config.test.ts` ×3 (2 warm, 1 with `SYMMETRY_DISABLE_TOOL_CACHES=1` + `SYMMETRY_TOOL_CACHE_DIR`) | 2 passed ×3; rc 0; first case (cold config import) 2.5 s / 1.3 s / 2.2 s against its own 30,000 ms per-case budget; second case 2.4 ms |
| 6 | `exec vitest run src/features/chat/chat.test.tsx src/features/connections/connections.test.tsx` | 105 passed (105); rc 0; 46.35 s both files |
| 7 | `run test` (plain, the contract's gate command) | 25 files / 611 tests passed; rc 0; 36.76 s, and 62.83 s re-run on the final bytes |
| 8 | `run test --reporter=json` | 611 passed (611); rc 0 both times; per-file durations below (final bytes) |
| 9 | `git diff --check` | exit 0 |

Per-file durations, head json run (ms): connections 26,991; chat 19,394; goal-attention 15,812;
planning 14,347; goal-settle 9,571; router 9,455; assignment 3,798; control 2,858; work/detail
1,384; tool-cache-config 792; runtimes 762; primitives 576; chat-hooks 411; api 204;
application 62; realtime 44; three presentation files 29/23/16; read-cache 15; refusals 14;
filters 14; receipts 13; saved-views 13; legacy 9.

Observed flake (recorded; the rerun is not a gate waiver): the first full-suite json run of this
pass reddened one `chat.test.tsx` case, "per-scope state keeps each scope's draft, intent and
scroll and restores them on a switch" — `Error: Unable to find role="heading" and name "Chat"`
with the loading presentation in the DOM (a 1,000 ms `findByRole` default inside a 20,000 ms case
budget); that run took 83.77 s against 36.76 s for the green rerun, and the same case passed in
the isolated oracle run (105/105) and in both green full runs. Load-sensitive; no chat-path code
in this diff. Recording it in `docs/TODO.md` is outside this slice's owned set.

Mutant flips (each alone, `vitest run src/features/goals/goal-settle.test.tsx`, reverted):

| mutant | flip | re-verified result |
| --- | --- | --- |
| (a) | `previous === marker` removed | 2 failed / 4 passed, rc 1: case 1 `expected 1 to be 2`, case 3 `expected 1 to be 3` |
| (b) | `marker !== before` removed | 3 failed / 3 passed, rc 1: case 1 `expected +0 to be 2`, cases 2/4 `expected 633/652 to be >= 2400` |
| (c) | `testTimeout` removed from `vitest.config.ts` | 2 failed / 4 passed, rc 1: `expected 5000 to be 15000`, `expected undefined to be 15000` |
| (d) | `refetchInterval: false` (no query-driven re-read) | 4 failed / 2 passed, rc 1: cases 1–4 |
| (e) | private `setInterval` poll reintroduced | 3 failed / 3 passed, rc 1: case 1 `expected 4 to be 2`, case 3 `expected 2 to be 3`, case 4 `expected 18 to be less than 3` |
| (f) | chain not reset on a failed result | 1 failed / 5 passed, rc 1: case 3 `expected 2 to be 3` |
| (g) | failed result resolves the wait | 1 failed / 5 passed, rc 1: case 3 `expected +0 to be 3` |
| (h) | the wait resolves without clearing the pending state (no `finally` clear; the interval stays live) | 2 failed / 4 passed, rc 1: case 1 and case 2 `expected 3 to be less than 3` (reads continued past the notice) |

Base characterization of the final test file (sources at `42afb56c`, test file as delivered):
3 failed / 3 passed, rc 1 — case 4 `expected 17 to be less than 3` (the private poll kept reading
after the unmount), budget guard `expected 5000 to be 15000`, config guard `expected undefined to
be 15000`; cases 1–3 (the frozen semantics, including case 2's new post-bound window assertion)
green at base, so the base characterization is unchanged by the added assertion. Sources restored
from the byte backup and `sha256sum -c` re-run OK afterwards.

Removed-poll greps re-run at these bytes: `api.goal(` matches only `:115` (the submit-time
`servedEnvelope` read); `setTimeout` only `:66` (the single 2,500 ms bound); `refetchInterval`
only `:81`; no `setInterval`, no `Date.now()`; `useRealtimeEpoch` import `:7` / use `:748`
untouched. DLP: no profile path and no backslash-u sequence in the changed/new files (no
matches). Inventory unchanged: the two edits and the three new files of §1.

The apply pass (§12) then changed these bytes again: `detail.tsx`, `goal-settle.test.tsx` and the
plan. The hashes, coordinates and greps above bind the implement-stage bytes (they were true of
them); §12 carries the apply-stage hashes, coordinate re-derivation, gate rows and mutant re-runs.

## 12. Apply-stage re-verification (2026-11-22, third pass on the post-simplify bytes)

Scope: the simplify pass's accepted findings — the `detail.tsx` comment rewrites/deletions
(`:51-52`, `:171`, `:227`), the settle's outstanding-count fix and `refetchIntervalInBackground`,
the test file's module-name fix (`vitest.config`), case 4's explicit read-wait budget, the shared
`expectReadsStopped` probe, case 1's unrelated-cache-success injection, case 4's pre-notice
invalidation assertions, and the new cases 5 (overlapping settles) and 6 (unfocused document).
Reported as a per-finding table in the apply-stage answer.

Bytes under verification (final apply bytes; every mutant flip below was applied alone to them,
run, and reverted under `sha256sum -c` with a byte backup):

| file | lines | sha256 |
| --- | ---: | --- |
| `frontend/src/features/goals/detail.tsx` | 778 | `72863f5f358498344c12f422f4f29c68a147791bfe8df9f13162b26b2546cdb0` |
| `frontend/src/features/goals/goal-settle.test.tsx` | 337 | `9aca7241d3ad75d064f7eebf7e9917b34e5be73f8a071b31fe6168421510a6cc` |
| `frontend/vitest.config.ts` | 41 | `661cbe330e5c9c16ed088911f81708648abd17935fad2768383765b3d2cbc0df` (unchanged from §11) |

### 12.1 Gate rows (foreground, serialized; exit codes)

| # | command (pnpm via `mise x pnpm@12.17.0 -- ... --filter @symmetry/frontend`) | exit | result |
| --- | --- | ---: | --- |
| 1 | `run typecheck` | 0 | `tsc --noEmit`, no output |
| 2 | `run lint` | 0 | 221 warnings, 0 errors (`--max-warnings 229` unchanged; +2 over §7: the new file's `max-lines-per-function` on the describe callback (232 > 150) and a second `Promise.withResolvers<void>()` under the repo's existing `no-invalid-void-type` pattern) |
| 3 | `exec vitest run src/features/goals/goal-attention.test.tsx` | 0 | 29 passed (29); 21.73 s isolated (report-only) |
| 4 | `exec vitest run src/features/goals/goal-settle.test.tsx` | 0 | 8 passed (8); 29.27 s isolated, 11,731 ms in-suite (json) |
| 5 | `exec vitest run src/tool-cache-config.test.ts` | 0 | 2 passed (2); 2.95 s |
| 6 | `exec vitest run src/features/chat/chat.test.tsx src/features/connections/connections.test.tsx` | 0 | 105 passed (105); 33.45 s both files |
| 7 | `run test` (the contract's gate command) | 0 | 25 files / 613 passed (613); 44.44 s |
| 8 | `exec vitest run --reporter=json --outputFile=…` | 0 | 613 passed (613); `goal-settle` 11,731 ms, `goal-attention` 17,827 ms, `chat` 21,861 ms, `connections` 27,410 ms in-suite (report-only) |
| 9 | `git diff --check` | 0 | clean; `git diff --numstat`: plan +31/−13, `detail.tsx` +10/−9, `goal-settle.test.tsx` +130/−13 |
| 10 | DLP: profile-path/users grep over the diff + `git status --porcelain -uall`; backslash-u over added lines | 1 / 1 | no matches (exit 1 = clean) |
| 11 | removed-poll greps (§9 form) | 0 | `api.goal(` only `:117`; `setTimeout` only `:66`; `refetchInterval` `:82`; `refetchIntervalInBackground` `:83`; no `setInterval`, no `Date.now()`; `useRealtimeEpoch` import `:7` / use `:749` |

### 12.2 Mutation runs (each applied alone, run, reverted; `vitest run src/features/goals/goal-settle.test.tsx`)

| mutant | flip | result (rc 1 in every row) |
| --- | --- | --- |
| (a) | `previous === marker` removed | 3 failed / 5 passed: case 1 `expected 1 to be 2`, case 3 `expected 1 to be 3`, case 6 `expected 1 to be 2` |
| (b) | `marker !== before` removed | 4 failed / 4 passed: case 1 `expected +0 to be 2`, case 2 `expected 607 to be >= 2400`, case 4 the pre-notice invalidation assertion, case 6 `expected +0 to be 2` |
| (c) | `testTimeout` removed from `vitest.config.ts` | 2 failed / 6 passed: `expected 5000 to be 15000`, `expected undefined to be 15000` |
| (d) | `refetchInterval: false` | 6 failed / 2 passed: cases 1–4, case 5 (no notice inside its 2,000 ms window), case 6 `expected +0 to be 2` |
| (e) | private `setInterval` poll reintroduced | 4 failed / 4 passed: case 1 `expected 4 to be 2`, case 3 `expected 2 to be 3`, case 4 `expected 18 to be less than 3`, case 6 `expected 4 to be 2` |
| (f) | chain not reset on a failed result | 1 failed / 7 passed: case 3 `expected 2 to be 3` |
| (g) | failed result resolves the wait | 1 failed / 7 passed: case 3 `expected +0 to be 3` |
| (h) | the wait resolves without clearing the pending state | 2 failed / 6 passed: case 1 and case 2 `expected 3 to be less than 3` (the shared probe still reddens) |
| (i) | goal-query filter dropped (`event.query !== goalQuery` removed) | 1 failed / 7 passed: case 1 `expected 1 to be 2` (the injected unrelated success confirmed in place of the goal query's second read) |
| (j) | the attention invalidation moved before the settle | 1 failed / 7 passed: case 4's pre-notice assertion (not-called-with `[{ queryKey: ['attention'] }]` reddened) |
| (k) | the pending count collapsed to one boolean flag | 1 failed / 7 passed: case 5 — the pause notice did not arrive inside its 2,000 ms window (the second settle fell to its bound) |
| (l) | `refetchIntervalInBackground` removed | 1 failed / 7 passed: case 6 `expected +0 to be 2` (no read while unfocused; the notice came at the bound) |

### 12.3 Base characterization of the apply-stage artifact

Sources at `42afb56c` (`vitest.config.ts` left at the delivered bytes so the budget guards keep
their subject), the eight-case test file as delivered: **1 failed / 7 passed (8)**, exit 1, 21.74 s
— only case 4 reddens (`expected 18 to be less than 3`: the private poll kept reading after the
unmount). Cases 1–3 stay green, and cases 5 and 6 are green at base too: the two new cases pin the
pre-existing (frozen) semantics — the base's independent loops confirm an overlapping settle and
keep reading while the document is unfocused — so the counted flag and
`refetchIntervalInBackground` restore base behavior rather than adding to it.

## 13. Fix-stage re-verification (2026-11-22, fourth pass; review findings REV-A01/A02/A03)

Scope: the fix stage's three accepted P3 findings — the shared reads-stopped probe's window
(`expectReadsStopped`: 400 → 800 ms, REV-A01), the before-bound thresholds (2,000 → 2,400 ms at
the four discriminating sites: the moved-marker case's elapsed assertion, the overlapping-settle
case's notice wait and elapsed assertion, and the unfocused case's elapsed assertion, REV-A02),
and this record's §5 staging label (now marked as the implement stage's 6 cases, with the apply
pass's additions numbered in the delivered order, REV-A03). The pass changed only the test file
and this record (`git diff --numstat`: test file +7/−6, record +71/−12); the probe comment carries
the recalculated arithmetic (a live 120 ms cadence yields ~6 reads in the 800 ms window).

Bytes under verification (final fix-stage bytes; the mutant flips below were applied alone to
them and reverted under `sha256sum -c` against a byte backup):

| file | lines | sha256 |
| --- | ---: | --- |
| `frontend/src/features/goals/detail.tsx` | 778 | `72863f5f358498344c12f422f4f29c68a147791bfe8df9f13162b26b2546cdb0` (unchanged from §12) |
| `frontend/src/features/goals/goal-settle.test.tsx` | 338 | `a9629e2b4cdef53f6a426ddd148fb7ed21da8400fa59a8924dca6b3f99880ca5` |
| `frontend/vitest.config.ts` | 41 | `661cbe330e5c9c16ed088911f81708648abd17935fad2768383765b3d2cbc0df` (unchanged from §12) |

§12's `goal-settle.test.tsx` hash and the window numbers recorded in §12.2 (d)/(h)/(k) bind the
pre-fix bytes; the rows below re-run every probe whose window changed.

### 13.1 Before-state proof of REV-A01 (observed red before the edit)

Mutant (h) applied alone to the pre-fix tree (§12 bytes): `vitest run
src/features/goals/goal-settle.test.tsx` → **2 failed / 6 passed**, rc 1, case 1 and case 2
`expected 3 to be less than 3` — a live 120 ms cadence produced exactly 3 reads in the 400 ms
window, the zero-margin boundary the finding names (reproducing §12.2 (h)).

### 13.2 Gate rows (foreground, serialized; exit codes)

| # | command (cwd `frontend`; workspace-resolved binaries) | exit | result |
| --- | --- | ---: | --- |
| 1 | `node ../node_modules/typescript/bin/tsc --noEmit` | 0 | no output; 5 s |
| 2 | `node node_modules/eslint/bin/eslint.js . --max-warnings 229` | 0 | 221 warnings, 0 errors (count unchanged from §12.1); 67 s |
| 3 | `node node_modules/vitest/vitest.mjs run src/features/goals/goal-attention.test.tsx` | 0 | 29 passed (29); 33 s |
| 4 | `node node_modules/vitest/vitest.mjs run src/features/goals/goal-settle.test.tsx` | 0 | 8 passed (8); 23 s |
| 5 | `node node_modules/vitest/vitest.mjs run src/tool-cache-config.test.ts` | 0 | 2 passed (2); 8 s |
| 6 | `node node_modules/vitest/vitest.mjs run src/features/chat/chat.test.tsx src/features/connections/connections.test.tsx` | 0 | 105 passed (105); 38 s |
| 7 | `node node_modules/vitest/vitest.mjs run` (full suite) | 0 | 25 files / 613 passed (613); 69 s |
| 8 | `git diff --check`; `git status --porcelain -uall` | 0 | clean; exactly this record and the test file modified |
| 9 | DLP: profile-path and host-absolute-path greps over the added lines of the branch and working diffs and over all five delivered files; backslash-u over added lines | 1 | no matches (exit 1 = clean) |

### 13.3 Mutant re-runs (fix-stage bytes; each applied alone, run, reverted)

`vitest run src/features/goals/goal-settle.test.tsx` on each flip; rc 1 in every row.

| mutant | flip | result |
| --- | --- | --- |
| (h) | no pending-clear (the interval stays live; §12.2 (h) form) | 2 failed / 6 passed: case 1 and case 2 `expected 5` / `expected 6 to be less than 3` — the probe reddens 2–3 reads clear of the `< 3` allowance (pre-fix: exactly 3) |
| (k) | the pending count collapsed to one boolean flag | 1 failed / 7 passed: case 5 — the notice does not arrive inside the widened 2,400 ms window (the second settle fell to its bound) |
| (d) | `refetchInterval: false` | 6 failed / 2 passed: cases 1–4, case 5 (notice not inside its 2,400 ms window), case 6 |

Counts: the settle file stays at 8 cases and the full suite at 613 (no case added, renamed or
removed; no oracle file touched by this pass).

## 14. Fix-stage re-verification (2026-11-22, fifth pass; independent-review findings F1–F4)

Scope: the four P3 findings of the independent adversarial review `251-opus-review-1`. F1 (the
stale PR description): a refreshed body binding the numbers of *this* pass was written to the
findings directory; the in-repo §12/§13 numbers were already accurate for their own bytes, and the
one number that had drifted for the delivered tree is the full-suite count (see 14.1). F2 (the
budget guard's coverage): case 5's inline 2,400 ms notice wait and the shared reads-stopped probe's
800 ms window are now module constants `PRE_BOUND_WAIT_MS` (`:19`) and `READS_STOPPED_WINDOW_MS`
(`:20`), both entries of `WAIT_BUDGETS` (`:21`), so the guard's "every wait in this file" name is
true; the six elapsed thresholds share `PRE_BOUND_WAIT_MS`, `observeFor` takes the named window
(`:61`), and §6 gains their rows. F3 (the latent real-timer window): the two moved-read counts at
`:107` and `:319` are `toBeGreaterThanOrEqual(2)`; case 3's exact chain-break count (`:180`) and
case 5's `pauseReads` count are kept. F4 (the comment policy): the three-line comment on the
injected unrelated cache success is trimmed to the one-line trap (`:93`). Only the test file and
this record changed (`git diff --numstat` vs HEAD: test file +15/−14; `detail.tsx` and
`vitest.config.ts` byte-unchanged).

Bytes under verification (successor of §13; every mutant flip below was applied alone to a scratch
mirror outside the repo, run, and reverted under `sha256sum -c` against a byte backup):

| file | lines | sha256 |
| --- | ---: | --- |
| `frontend/src/features/goals/detail.tsx` | 778 | `72863f5f358498344c12f422f4f29c68a147791bfe8df9f13162b26b2546cdb0` (unchanged from §13) |
| `frontend/src/features/goals/goal-settle.test.tsx` | 339 | `258e131a123b788750f05f41e09884646826787ac58b5f2410bcf9ba7a1fa538` |
| `frontend/vitest.config.ts` | 41 | `661cbe330e5c9c16ed088911f81708648abd17935fad2768383765b3d2cbc0df` (unchanged from §13) |

### 14.1 Gate rows (foreground, serialized; workspace-resolved binaries, cwd `frontend`)

| # | command | exit | result |
| --- | --- | ---: | --- |
| 1 | `node ../node_modules/typescript/bin/tsc --noEmit` | 0 | no output; 9.7 s |
| 2 | `node node_modules/eslint/bin/eslint.js . --max-warnings 229 --no-cache` | 0 | 221 warnings, 0 errors (count unchanged from §12.1); 179 s |
| 3 | `node node_modules/vitest/vitest.mjs run src/features/goals/goal-attention.test.tsx` | 0 | 29 passed (29); 35.7 s |
| 4 | `node node_modules/vitest/vitest.mjs run src/features/goals/goal-settle.test.tsx` | 0 | 8 passed (8); 37.0 s |
| 5 | `node node_modules/vitest/vitest.mjs run src/tool-cache-config.test.ts` | 0 | 2 passed (2); 7.3 s |
| 6 | `node node_modules/vitest/vitest.mjs run src/features/chat/chat.test.tsx src/features/connections/connections.test.tsx` | 0 | 105 passed (105); 85.2 s |
| 7 | `node node_modules/vitest/vitest.mjs run` (full suite) | 0 | **26 files / 619 passed (619)**; 51.4 s |
| 8 | `node node_modules/vitest/vitest.mjs run src/app/selected-project.test.tsx` | 0 | 6 passed (6); 12.6 s |
| 9 | `git diff --check`; `git status --porcelain -uall` | 0 | clean; exactly this record and the settle test modified |

Full-suite count reconciliation: the delivered HEAD `7aef088e` is a merge of `origin/main` that
brought #249's `selected-project.test.tsx` (6 cases, outside this slice's owned set) into the
branch tree. §12.1/§13.2's full-suite rows (25 files / 613) were measured on the pre-merge branch
bytes; the delivered tree re-measures at **26 files / 619**, rc 0, and the +1 file / +6 cases are
exactly that file (row 8). The same drift moves the per-file-duration table of §7/§11 (report-only)
by nothing else: the file set is otherwise the same 25 files.

### 14.2 Mutant re-runs (the rows crossing the relaxed assertions; scratch mirror)

`vitest run src/features/goals/goal-settle.test.tsx` with caches disabled and a scratch cache dir;
rc 1 in every row.

| mutant | flip | result (this pass's bytes) |
| --- | --- | --- |
| (a) | `previous === marker` removed | 3 failed / 5 passed: case 1 `expected 1 to be greater than or equal to 2`, case 3 `expected 1 to be 3`, case 6 `expected 1 to be greater than or equal to 2` — both relaxed cases stay red, so F3 was applied |
| (b) | `marker !== before` removed | 4 failed / 4 passed: case 1 `expected +0 to be greater than or equal to 2`, case 2 `expected 658 to be >= 2400`, case 4 the pre-notice invalidation assertion, case 6 `expected +0 to be greater than or equal to 2` |
| (i) | goal-query filter dropped (`event.query !== goalQuery` removed) | 1 failed / 7 passed: case 1 `expected 1 to be greater than or equal to 2` |
| (l) | `refetchIntervalInBackground` removed | 1 failed / 7 passed: case 6 `expected +0 to be greater than or equal to 2` (the only red rides the relaxed count, and stays red) |

Rows (a), (b) and (i) are the recorded mutants whose red path crosses the two relaxed assertions;
(l) is the row whose *only* red does. The other rows (c–h, j, k) were demonstrated at §12.2/§13.3
on their carrying assertions — case 3's exact count, the reads-stopped probes, the elapsed checks,
the guard's first assertion — all byte-unchanged by this pass, so their reds stand without a
re-run; (d)/(e) additionally keep reds on unchanged assertions beyond the relaxed cases.

### 14.3 Base characterization of the delivered artifact (re-run)

Base `42afb56c` sources for `detail.tsx` (`sha256 062777db3ab14c5edb3fc35d4dfe8491ebc59736129f949396f65a9cd4f93cab`),
the delivered test file and the delivered `vitest.config.ts` (so the budget guards keep their
subject), in the scratch mirror: **1 failed / 7 passed (8)**, rc 1 — only case 4 reddens
(`expected 17 to be less than 3`: the private poll kept reading after the unmount). Cases 1–3, 5
and 6 and both budget guards stay green; the count is unchanged from §12.3 and is the F1 body's
"base characterization" number.
