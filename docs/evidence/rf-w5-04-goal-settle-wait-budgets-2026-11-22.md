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
| `frontend/src/features/goals/detail.tsx` | edited | 757 → 777 | the settle conversion only: the goal query's `refetchInterval` while pending (`:79-82`), `goalMarker` (`:47-49`), `waitForGoalSettle` (`:51-73`), `settleGoalRead` (`:89-98`), the two call sites' compressed comments (`:168-169`, `:226`) |
| `frontend/vitest.config.ts` | edited | 39 → 41 | the explicit `testTimeout: 15_000` (`:24-25`) |
| `frontend/src/features/goals/goal-settle.test.tsx` | new | 0 → 220 | 6 cases: the four settle cases + the two budget-guard cases |
| `frontend/src/features/goals/goal-attention.test.tsx` | **unedited** | 1,321 | 29 case names, the eight 5,000 ms waits and the seven `}, 15_000)` budgets byte-identical |
| `frontend/src/tool-cache-config.test.ts` | **unedited** | 71 | oracle; its two cases keep their own 30,000 ms budgets |
| `frontend/src/features/connections/connections.test.tsx` | **unedited** | — | oracle; its three wait sites keep 5,000 ms under the new 15,000 ms default |
| `frontend/src/features/chat/chat.test.tsx` | **unedited** | — | oracle; its 8,000 ms wait stays under its `}, 20_000)` case budget |
| `docs/plans/2026-11-22-rf-w5-04-goal-settle-wait-budgets-plan.md` | new | 223 | the plan |
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

- **Drive.** `useQuery({ ...goalOptions(application.api, goalId), refetchInterval: settlePending ? settleReadIntervalMs : false })` (`:79-82`): the goal query keeps its key and its
  `api.goal` `queryFn`; the interval exists only while `settlePending` (`:78`), so it is cleared on
  confirmation, on the bound and on unmount (the observer is destroyed with the surface).
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
| pre-command marker | `acknowledged === undefined ? null : [version, state, current_revision].join("|")` (`:63`) | `goalMarker(acknowledged)` (`:47-49`, called `:92`) — same join, `undefined → null` | yes |
| per-read fetch | private `application.api.goal(...).catch(() => null)` GET (`:66`) | the goal query's own `api.goal` read, driven by the interval (`:81`) | carrier changed (decision C) |
| read result observed | each loop iteration's read | each `success` cache result | yes |
| confirmation | `:68` | `:61` | yes (verbatim) |
| chain bookkeeping | `previous = marker` (`:69`) | `:62` | yes |
| failed read | `catch(() => null)` → `marker = null` → `previous = null`; neither confirms nor aborts (`:66-69`) | `error` result → `previous = null` (`:59`); neither confirms nor aborts; a failed *attempt* inside a retrying fetch is internal to the query's own retry policy (`readRetry`, 500/1,000 ms) | semantics yes; attempt-level retries are the query's carrier |
| bound exit | `while (Date.now() < deadline)` with the loop's own sleeps (`:62`,`:65`,`:70-72`) | one 2,500 ms `setTimeout` on the wait (`:66`) | yes (the notice at the bound) |
| unmount during the wait | the private loop kept reading until the bound | the query's interval stops with the observer (no reads); the wait still resolves at the bound | **changed, authorized** ("cleared on unmount"; pinned by test case 4) |
| notice then follow-ups (generic caller) | `:170-176`: acknowledged → `await invalidate(goalReadKey)` → `await settleGoalRead` → `setNotice` → `void invalidate(goalEvents)` → `void invalidate(attention)` | `:171-176`, byte-identical sequence | yes |
| notice then follow-ups (plan approval) | `:205-212` | `:227-232`, byte-identical sequence | yes |
| realtime epoch | not used by the settle | not used by the settle (`useRealtimeEpoch` stays at its receipts use, now `:748`) | yes |
| rendered surface | notice texts | unchanged (both strings byte-identical) | yes |

Accepted consequences of routing the reads through the query (decision C): a *failing* settle read
now sets the goal query's own error state, so the page's existing stale-error presentation applies
to it (base's private reads swallowed their failures); the query's retry schedule governs
transient attempt failures. Neither confirms nor aborts the wait.

## 5. The new settle cases (and the base characterization observed first)

`frontend/src/features/goals/goal-settle.test.tsx` (6 cases):

| case | pins | base run (before the source change) | head run |
| --- | --- | --- | --- |
| announces acceptance only after the moved marker holds across two reads of the goal query | the two-consecutive-reads confirmation, before the bound, with the follow-ups issued and the reads stopped after the notice | pass (frozen semantics) | pass |
| announces acceptance at the bound when the served marker never moves | the bound exit at ≥ 2,400 ms with the pending reads flowing through the query (> 3 reads), and the reads stop after the notice | pass | pass |
| neither confirms nor aborts on a refused goal read and breaks the consecutive chain | refusals neither confirm nor abort; a failed result breaks the chain (three moved reads needed) | pass | pass |
| clears the pending reads with the observer on unmount and still runs the follow-up invalidations | "cleared on unmount" + the two follow-up invalidations after a pending settle | **fail** — `expected 16 to be less than 3` (the private poll kept reading after the unmount) | pass |
| keeps every wait in this file strictly below the effective per-test budget | the wait budgets vs the runner-resolved budget | **fail** — `expected 5000 to be 15000` | pass |
| carries the explicit per-test budget in vitest.config | the parsed config option | **fail** — `expected undefined to be 15000` | pass |

Base run of the final artifact (sources restored to `42afb56c`, test file as delivered): **3 failed
/ 3 passed (6)**, exit 1, run duration 29.36 s. Head run: **6 passed (6)**, exit 0; file duration
12,061 ms in the full-suite json (isolated runs: 9,111 ms and 14,951 ms warm; 24–35 s when the cold
config import lands in its worker — the config case's own 30 s budget covers it).

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
| 13 | `goal-settle.test.tsx` `NOTICE_WAIT_MS` (`:14`) | 5,000 | 15,000 (global; asserted at runtime, `:206`) | yes |
| 14 | `goal-settle.test.tsx` `FOLLOW_UP_WAIT_MS` (`:15`) | 4,000 | 15,000 (global; asserted at runtime, `:206`) | yes |
| — | `tool-cache-config.test.ts:48,59` (oracle) | no inner wait | 30,000 per case (sized from the cold import) | n/a |
| — | `goal-settle.test.tsx:209` config case | no inner wait | 30,000 (`CONFIG_IMPORT_BUDGET_MS`) | n/a |

The budget rule's runnable guards: `task.timeout === 15_000` for a case without an explicit budget
(reddens when the option is removed, mutation (c)), every wait budget of the new file asserted
strictly below that runtime number (`:206`), and the parsed `vitest.config.test?.testTimeout`
(`:213`). The eight `goal-attention` and one `chat` sites keep their numbers and their explicit
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

- `grep -n "api\.goal(" frontend/src/features/goals/detail.tsx` → one site, `:115`, inside
  `servedEnvelope` (the submit-time read, unchanged); the settle block has none.
- `grep -n "setInterval\|setTimeout\|Date\.now\|refetchInterval" frontend/src/features/goals/detail.tsx`
  → `:51` (comment), `:66` (the single bound `setTimeout`), `:81` (the query's own
  `refetchInterval`). No `setInterval`, no `Date.now()`, no recursive `setTimeout`, no
  `2_500` loop.
- `grep -n "useRealtimeEpoch" frontend/src/features/goals/detail.tsx` → import `:7`, use `:748`
  (the receipts catch-up) — untouched; `services/realtime.ts` and `app/realtime.tsx` are not in the
  diff.
- DLP: a case-insensitive profile-directory-path grep over the diff and over this record's
  companions → no match (exit 1, re-checked after this record was written); no backslash-u
  sequence in any added line of the changed/new files (exit 1).
- Inventory: `git status --porcelain -uall` lists exactly the plan, the evidence record, the new
  test file and the two edited files (`detail.tsx`, `vitest.config.ts`); `git diff --stat` is
  `detail.tsx` 72 changed lines (+48/−26 across the diff) and `vitest.config.ts` +2.

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
