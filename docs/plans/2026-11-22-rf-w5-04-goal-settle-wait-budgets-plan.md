# rf-w5-04-goal-settle-wait-budgets plan — the goal query's own `refetchInterval` drives the settle; explicit `testTimeout: 15_000`

Contract: `goal-rf-w5-04-goal-settle-wait-budgets.md` (GDD, 2026-11-21, binding with decision C
(query-driven settle, frozen semantics, report-only wall time), the strict owned set, the
lesson-32 control-flow audit, the mutation-proof rule and the owner comment policy of
2026-11-22). Base revision `42afb56c` (`refactor(frontend): own read-cache keys in one module
(#245)`, this worktree's dispatch HEAD; `git status --porcelain` empty at dispatch).

Shape references: `docs/plans/2026-11-22-rf-a5-frontend-query-keys-plan.md` and
`docs/evidence/rf-a5-frontend-query-keys-2026-11-22.md` (same-stage GDD shape: dispatch gates,
re-derivation, control-flow audit, mutation table, gate rows, residual risks);
`docs/plans/2026-11-22-rf-w4-11-frontend-test-harness-plan.md` (the harness/install basis).

Host note: every `pnpm` invocation runs as `mise x pnpm@12.17.0 -- pnpm --trust-lockfile
--config.verify-deps-before-run=false ...` (the workspace pins pnpm 12.17.0; the verify step is
skipped because the local install carries the three override versions below). The host's npm feed
404s `knip@6.46.1`, `@vitest/coverage-v8@5.1.1` and `msw@3.0.7` (the merged #240 bump), so the
one-time `pnpm install --no-frozen-lockfile` ran with the documented temporary `overrides` block
in `pnpm-workspace.yaml` pinning exactly those three to `6.45.1`/`5.1.0`/`3.0.5`; both
`pnpm-workspace.yaml` and `pnpm-lock.yaml` were restored immediately after the install
(`git status --porcelain` empty, re-checked), and none of the three is a gate tool.

## 1. Dispatch gates (checked at dispatch, base `42afb56c`)

| gate | state at dispatch |
| --- | --- |
| `#164` (W2-15), W3-12, W4-11 (`#220`), W4-08 (`#232`), W4-09 (`#239`), W4-10 (`#241`) merged | yes — all are ancestors of HEAD |
| A5 (`#245`, `goal-rf-a5-frontend-query-keys`, this HEAD) merged | yes; `app/read-cache.ts` owns the keys the settle's cache lookup uses |
| open PRs touching the owned set | none: `gh pr list --repo wxxb789/symmetry --state open` → `#243` (`refactor(harness): drive adapter sessions through one staged driver`), whose files are `daemon/internal/harness/**`, `docs/TODO.md` and its own plan/evidence |
| `vitest.config.ts` state | 39 L, still sets no `testTimeout` (the contract's re-derivation holds at HEAD) |
| owned files untouched by other work | `git status --porcelain` empty |

## 2. Re-derivation of the owned set against HEAD (deltas vs the contract's coordinates)

1. **`goals/detail.tsx` is 757 L** (contract: 761). `settleGoalRead` is `:61-74` (doc comment
   `:50-60`; contract `:65-78` / `:57-64`), its decision points are `:62` (`Date.now() + 2_500`)
   and `:71` (`setTimeout(resolve, 120)`), and its callers are the generic command's `onSuccess`
   `:147-155` (comment `:147-149`) and the plan approval's `:205-212` (comment `:205-206`)
   (contract: `:156`/`:213`).
2. **`vitest.config.ts` (39 L) sets no `testTimeout`**; the frontend default is 5,000 ms. The
   global change is the whole per-site fix the contract asks for.
3. **`goal-attention.test.tsx` is 1,321 L / 29 cases** (plan says 29 ✓). Its eight wait sites are
   `:360,408,426,791,877,983,1073,1158` (`{ timeout: 5_000 }`), each inside a case carrying an
   explicit `}, 15_000)` budget (`:361,432,793,878,984,1074,1159`) — the contract's re-derived
   list, shifted.
4. **`connections.test.tsx` wait sites are `:878,918,935`** (contract `:892,932,949`); their
   cases carry no explicit budget, so their enclosing budget is the default — the genuine
   equal-budget family the global `testTimeout` resolves with no site edit.
5. **`chat.test.tsx`'s wait site is `:1276`** (`{ timeout: 8_000 }`, contract `:1300`), inside the
   case budgeted `}, 20_000)` at `:1283`.
6. **12 DOM wait sites** = 8 (`goal-attention`) + 3 (`connections`) + 1 (`chat`) ✓, matching the
   contract's census; `tool-cache-config.test.ts` (71 L) carries its two cases' own 30,000 ms
   budgets and no same-sized inner wait — oracle, no edit.

## 3. Design (the choices the accepted contract leaves unresolved)

1. **Drive source.** The goal read moves from `goalOptions(application.api, goalId)` to
   `{ ...goalOptions(application.api, goalId), refetchInterval: settlePending ? 120 : false }`.
   The key, the `queryFn` (`api.goal`) and every other option stay the same, so the settle's
   reads are the goal query's own reads and nothing else in the file issues a goal GET. The
   interval is an observer option: it is cleared on confirmation and on the bound (the pending
   state flips back) and on unmount (the observer is destroyed) — and, unlike a private poll, it
   stops reading when the surface is gone (pinned by the unmount case, §7 (e)).
2. **Observation.** A module-level `waitForGoalSettle(queryClient, goalId, before)` awaits two
   consecutive cache results: it subscribes to `queryClient.getQueryCache()` and observes only
   the goal query's `updated` events (`event.query ===
   queryClient.getQueryCache().find({ queryKey: goalReadKey(goalId), exact: true })`), treating
   `success` as a read result and `error` as a failed read, and ignoring `fetch`, `failed`
   (a retrying attempt's internal failure), `invalidate` and `setState`. The comparison order is
   base's verbatim: `marker !== null && marker !== before && previous === marker` → confirmed;
   otherwise `previous = marker`. A failed result resets `previous` to `null` (base's
   `catch(() => null)` chain break). The bound is one `setTimeout` of 2,500 ms — a single timer,
   not a poll.
3. **Unseeded by construction.** In v5 a fetch dispatches its `success` action before its promise
   resolves, and `await invalidateQueries(...)` resolves after the refetch's promise — so the
   invalidate-triggered result is already past when the settle subscribes, and the settle
   observes exactly the results delivered while it is pending (two of *its* reads, as at base).
   This is why the design needs no "ignore results from before the settle" bookkeeping.
4. **`settlesPending` counts outstanding settles.** It is incremented before the await and
   decremented in a `finally`, so the interval lives while at least one settle is outstanding: a
   plan approval and a generic command can overlap (the approval keeps `Decisions` disabled, not
   `NextAction`), and the first finisher must not clear the interval under the second. The
   interval is also allowed to read while the document is unfocused
   (`refetchIntervalInBackground: true`), as base's private loop did.
5. **Budgets.** `vitest.config.ts` gains `testTimeout: 15_000`; the wait sites need no edit (all
   12 are strictly smaller against it, the three `connections` sites included). The budget guard
   is two cases in the new test file: one without an explicit budget that (i) asserts the
   runner-resolved effective budget (`task.timeout === 15_000`) — the option *as the runner
   applies it*, which is the strongest available oracle and reddens exactly when the option is
   removed — and (ii) asserts every wait budget that file passes is strictly below that runtime
   number, and one that (iii) imports `vitest.config` and asserts the parsed
   `test?.testTimeout === 15_000` under its own 30 s import budget (its budget keeps it out of
   the runtime-budget assertions), the style reference's own mechanism
   (`tool-cache-config.test.ts`). Alternative rejected: a second config-importing file (the cold
   config import measured 8.2 s and is already paid once by `tool-cache-config.test.ts`; a
   separate file risks paying it in another worker for a three-line assertion).
6. **New test file.** `frontend/src/features/goals/goal-settle.test.tsx` carries the settle
   cases (confirmation, bound, read-error, unmount) and the budget guard. Against the base tree
   the three frozen-semantics cases run green (they pin the frozen *semantics*), the unmount case
   reddens (its subject is the authorized "cleared on unmount" change) and the two budget cases
   redden; the head red proofs are the mutants of §8.
7. **Comments (owner policy).** The settle block's ten-line doc comment compresses to the
   two-line why (why the wait exists: only the command's own commit moves the served marker);
   the two call-site comments compress to one line each; no new comments in the new test file or
   the config beyond the existing file's own convention.
8. **Edits outside the settle block: none.** `detail.tsx` changes are the settle block, its two
   call sites, the `useQuery` options object (the settle's drive), the one new state hook, the
   module-level constants/helper, and the imports the helper needs.

## 4. Base → HEAD control-flow table (lesson 32; filled with base/HEAD lines in the evidence record)

| row | base (`:61-74`) | HEAD | preserved |
| --- | --- | --- | --- |
| pre-command marker | `acknowledged === undefined ? null : join("|")` | `goalMarker(acknowledged)` (same join, `undefined → null`) | yes |
| per-read fetch | private `application.api.goal` GET, `catch(() => null)` | the goal query's own `api.goal` read (interval-driven); a failed *attempt* is internal to the query's retry policy | semantics, not carrier |
| confirmation | `marker !== null && marker !== before && previous === marker` | verbatim, evaluated per observed cache result | yes |
| chain bookkeeping | `previous = marker` after every read | after every observed success result | yes |
| failed read | `catch → null` → `previous = null` (neither confirms nor aborts) | `error` result → `previous = null`; `failed` (retrying attempt) is invisible to the settle | yes (attempt-level retries are the query's own policy, authorized by decision C) |
| bound exit | `while (Date.now() < deadline)` with the read loop's own sleeps | one 2,500 ms `setTimeout` on the wait | yes (notice at the bound) |
| unmount | the private loop keeps reading until the bound | the query's interval stops with the observer; the wait still resolves at the bound | changed (authorized: "cleared on unmount") |
| notice + follow-ups after the wait | `setNotice` → `void` goal-events → `void` attention | unchanged, both callers, byte-identical order | yes |
| second caller ordering | approval: comment + identical sequence | unchanged | yes |

## 5. Exact edge-case semantics the contract demands

1. **Confirmation** needs the served marker to differ from the pre-command marker and to be the
   same on two consecutive read results; one read of the moved marker is not enough.
2. **Bound**: with a marker that never moves (or moves only once), the notice still appears, at
   the 2.5 s bound.
3. **Read failures** never confirm and never abort; a failed result breaks the consecutive chain
   (two consecutive successful moved reads still confirm after it).
4. **Unmount** during a pending settle: the interval is gone (no further goal reads) and the two
   follow-up invalidations still run after the wait resolves.
5. **Both follow-up invalidations** (`goal-events`, `attention`) always run after the notice, for
   both callers.
6. **No realtime-epoch dependency**: `useRealtimeEpoch`'s existing receipts use at `:728-733`
   stays; the settle does not read the epoch.
7. **Byte-identical surfaces**: no rendered text, selector or notice text changes; the 29
   `goal-attention.test.tsx` case names stay byte-identical, its eight 5,000 ms waits and its
   `}, 15_000)` budgets stay as they are.

## 6. Evidence plan

All commands foreground and serialized; no measurement harness beside a suite. Baseline rows at
`42afb56c` (before any edit), head rows on the final tree.

| # | command | proves |
| --- | --- | --- |
| 1 | `pnpm --filter @symmetry/frontend typecheck` | the settle conversion compiles |
| 2 | `pnpm --filter @symmetry/frontend lint` (`--max-warnings 229`) | ratchet held; measured count ≤ base |
| 3 | `vitest run src/features/goals/goal-attention.test.tsx` | 29 cases, unchanged names; file duration recorded against the 13.4 s base |
| 4 | `vitest run src/features/goals/goal-settle.test.tsx` | the new settle + budget cases green |
| 5 | `vitest run src/tool-cache-config.test.ts` | oracle green + its cold/warm import vs its 30 s per-case budget |
| 6 | `vitest run src/features/chat/chat.test.tsx src/features/connections/connections.test.tsx` | oracle suites green under the changed default budget |
| 7 | `pnpm --filter @symmetry/frontend test` (json reporter for per-file durations) | full executed count + per-file durations, base vs head |
| 8 | `git diff --check` | whitespace/EOF hygiene |
| 9 | grep gates: no `api.goal(` inside the settle block, no `setInterval`/recursive `setTimeout` poll, no `Date.now() + 2_500` loop; `useRealtimeEpoch` untouched | the removed poll |
| 10 | per-site wait-budget table extraction (site → budget → enclosing) | the budget rule |
| 11 | mutation flips (§8), each applied alone, run, reverted | the contract's judging mutants |
| 12 | DLP greps over added lines (profile path, backslash-u); `git status --porcelain` at the end | program-wide rules + the owned set is exactly what changed |

## 7. New settle test cases (each pins a frozen semantic; reds in §8)

1. **Confirmation before the bound** — the double serves the unmoved marker for the first three
   reads after the command's POST and the moved marker from the fourth on, and injects one
   unrelated cache success the moment the moved projection is cached (a dropped goal-query
   filter reddens, mutant (i)); the notice appears with ≥ 2 *moved-marker* reads observed, the
   elapsed time is well below the bound, the follow-up invalidations were issued, and the goal
   reads stop after the notice (the interval is cleared on confirmation).
2. **Bound exit** — the served marker never moves; the notice appears at ≥ 2,400 ms and the goal
   query kept reading while the settle was pending (more reads than the command path's two), and
   the goal reads stop after the notice (the interval is cleared on the bound).
3. **Read-error path** — 409-answered goal reads (a non-retryable read error) neither confirm nor
   abort: the notice still arrives by confirmation; the consecutive chain reset is pinned by
   counting the moved-marker reads (three: before the second failure, after it, and the
   confirming read).
4. **Unmount path** — unmount while the settle is pending: no goal read after the unmount (the
   interval is cleared with the observer), neither follow-up invalidation has run while the
   settle is pending, and both still run after the wait resolves.
5. **Overlapping settles** — the plan approval's settle is still outstanding when the pause's
   settle starts (the case waits for the approval's own settled read, then dispatches the pause):
   both notices appear before the bound and the pause's commit is served to at least one further
   read; reds on mutant (k) (a single pending flag clears the interval under the second settle).
6. **Unfocused document** — with `focusManager` reporting the document unfocused, the settle's
   interval keeps reading (`refetchIntervalInBackground`), so the notice still arrives by
   confirmation; reds on mutant (l) (the bound releases it instead).
7. **Budget guard** — `task.timeout === 15_000` for a case without an explicit budget, every wait
   budget this file passes is strictly below it, and the parsed `vitest.config` carries
   `test?.testTimeout === 15_000`.

## 8. Mutation list

Each applied alone to the final tree, run, reverted.

| mutant | flip | expected red |
| --- | --- | --- |
| (a) | drop the two-consecutive requirement (`previous === marker` removed) | settle case 1 (one moved read before the notice) |
| (b) | resolve without observing a moved marker (`marker !== before` removed) | settle case 1 (zero moved reads) |
| (c) | remove the explicit `testTimeout` from `vitest.config.ts` | budget guard case |
| (d) | remove the settle query's refetch wiring (`refetchInterval: false`) | settle case 1 (confirmation falls to the bound) and case 2 (no reads) |
| (e) | reintroduce a hand-rolled poll for the settle (a private `api.goal` loop) | settle case 4 (goal reads continue after unmount) |
| (f) | stop resetting the chain on a failed result | settle case 3 (moved-read count drops to two) |
| (g) | resolve the wait on a failed result (confirm on error) | settle case 3 (notice before any moved read) |
| (h) | the wait resolves without clearing the pending state (the interval stays live) | settle case 2's post-notice window (reads continue past the bound) and case 1's |
| (i) | drop the goal-query filter on the observed cache events (`event.query !== goalQuery` removed) | settle case 1 (an unrelated cache success confirms in place of the goal query's second read: the moved-read count drops to one) |
| (j) | move one follow-up invalidation before the settle (the attention key) | settle case 4 (the pre-unmount assertion: the invalidation already ran) |
| (k) | collapse the pending count to one boolean flag | settle case 5 (the second settle's notice falls to the bound) |
| (l) | remove `refetchIntervalInBackground` | settle case 6 (no read while unfocused: zero moved reads, the notice at the bound) |

## 9. Oracles that must pass unmodified

`frontend/src/tool-cache-config.test.ts`, `frontend/src/features/connections/connections.test.tsx`,
`frontend/src/features/chat/chat.test.tsx`, `frontend/src/features/goals/goal-attention.test.tsx`
(names/assertions byte-identical; only the settle-acceptance additions), `frontend/src/app/read-cache.test.ts`,
`frontend/src/app/realtime.*`, every other suite in the full run. `services/realtime.ts`,
`app/realtime.tsx`, `app/application.ts`, `services/api.ts` are not edited.

## 10. Out of scope (with reasons)

Contract clause 4 verbatim: the project-scope hook (W5-03); refusal classification (W4-10); the
MSW/harness consolidation (W4-11); the jsdom/browser split (W5-08); the remaining wait budgets
outside the owned sites (W6-12); coverage thresholds (W2-17/W6-12); browser specs. Additionally:
`app/application.ts` (the goal query's options object is overlaid at the call site, so the shared
option builder stays untouched), the realtime epoch's contract, the `Receipts` catch-up query,
coverage/deps/toolchain files, `--max-warnings`.

## 11. Residual risks

- The settle's reads are now the goal query's, so a *failing* read also sets the query's own
  error state: a read that fails during a settle surfaces the page's existing stale-error
  presentation (base's private reads swallowed their failures). This is the authorized
  consequence of decision C (reads flow through the query); the frozen notice semantics are
  unaffected (§4's failed-read row).
- The query's retry policy (`readRetry`, 500/1,000 ms delays) applies to the settle's reads: a
  transient attempt failure no longer breaks the consecutive chain (base's per-attempt
  `catch → null` did). Both never confirm during failures; the confirmation after a failure burst
  costs the query's own retry schedule. Recorded, not traded.
- `testTimeout: 15_000` loosens hang detection for every un-budgeted test (the contract's
  recorded accepted cost).
- Wall time is report-only: the five replay cases legitimately wait the frozen 2.5 s bound, so
  the goal-attention file stays in the same band; the new file adds its own bound/unmount waits.
- Host contention: the gate rows record wall time under the same-host conditions; the counts and
  names, not the wall clock, are the gates.
