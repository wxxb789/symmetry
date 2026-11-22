# rf-a5-frontend-query-keys — evidence record (2026-11-22)

Subject: one `frontend/src/app/read-cache.ts` owning every read key (eleven root builders) and the
change→reads map (moved from `services/realtime.ts`); every base key site reduces to a builder
call, byte-identically. Contract: `goal-rf-a5-frontend-query-keys.md` (GDD, 2026-11-21, binding
with its design clauses, the strict owned set, the lesson-32 control-flow audit, the
mutation-proof rule and the owner comment policy of 2026-11-22). Base revision `c5d0a4a8`
(`refactor(frontend): classify refusals in one module (#241)`, this worktree's dispatch HEAD,
tree clean; every base coordinate below). Plan:
`docs/plans/2026-11-22-rf-a5-frontend-query-keys-plan.md`. Judging artifact (contract clause 2,
"answer.md"): this record plus the implement-stage report (interface list, identity table,
per-success-path table, grep-gate outputs, mutation runs, gate outputs with exit codes).

## 1. Deliverable shape (files, counts)

| file | status | lines (base → head) | note |
| --- | --- | ---: | --- |
| `frontend/src/app/read-cache.ts` | new | 0 → 113 | the eleven builders, `InvalidationPayload`, `invalidationKeys`, `isSessionReadKey` |
| `frontend/src/app/read-cache.test.ts` | new | 0 → 78 | 38 cases: 24 builder-tuple rows, 13 change-kind rows, 1 predicate case |
| `frontend/src/app/application.ts` | edited | 355 → 356 | import line + the ten option builders' keys |
| `frontend/src/services/realtime.ts` | edited | 462 → 417 | `InvalidationPayload` and the map moved out; import line; the every-read predicate |
| `frontend/src/app/shell.tsx` | edited | 280 → 281 | import line + one key |
| `frontend/src/features/attention/attention.tsx` | edited | 201 → 202 | import line + one key |
| `frontend/src/features/chat/use-chat-scope-state.ts` | edited | 248 → 249 | import line + one key (the contract's `chat.tsx:321` site, moved here by W4-08) |
| `frontend/src/features/connections/connections.tsx` | edited | 496 → 497 | import line + four keys |
| `frontend/src/features/connections/resources.tsx` | edited | 671 → 672 | import line + one key |
| `frontend/src/features/goals/detail.tsx` | edited | 756 → 757 | import line + seven keys |
| `frontend/src/features/projects/projects.tsx` | edited | 144 → 145 | import line + two keys |
| `frontend/src/features/projects/work-surface.tsx` | edited | 361 → 362 | import line + three keys |
| `frontend/src/features/work/assignment.tsx` | edited | 324 → 325 | import line + two keys |
| `frontend/src/features/work/control.tsx` | edited | 468 → 469 | import line + five keys (two positional reads included) |
| `frontend/src/features/work/detail.tsx` | edited | 456 → 457 | import line + three keys |
| `frontend/src/features/work/history.tsx` | edited | 74 → 75 | import line + two keys |
| `frontend/src/services/realtime.test.ts` | edited | 608 → 566 | the map-restatement case deleted per clause 4 (−42: the 40-line case, one blank line, the import name) |
| `docs/plans/2026-11-22-rf-a5-frontend-query-keys-plan.md` | new | 339 | the plan |
| `docs/evidence/rf-a5-frontend-query-keys-2026-11-22.md` | new | — | this record |

Not edited: `frontend/src/features/chat/chat.tsx` (no key site at HEAD after W4-08), the two chat
hooks' `ChatScopeKey` import lines, `frontend/src/features/chat/use-older-page-loading.ts` (its
key is already derived from `chatConversationOptions(...).queryKey`), every other test file,
`docs/TODO.md` / `docs/archive/CLOSED-TODO.md` (no entry closes), `docs/design/**` (no document
names the key vocabulary), `contracts/**`, `browser/**`, `pnpm-lock.yaml`, `pnpm-workspace.yaml`
(install overrides restored; §10).

## 2. Re-derived owned set against HEAD (dispatch deltas vs the contract's coordinates)

1. **`chat.tsx` holds no key site.** W4-08 (`#232`) moved the chat send invalidation into
   `features/chat/use-chat-scope-state.ts:165`; that file joins the owned set for the one site,
   `chat.tsx` is not edited.
2. **Line drift on every other site** (contract coordinate → HEAD coordinate):
   `application.ts` builders `:210-341` → `:211-341`; `shell.tsx:262` → `:262`;
   `attention.tsx:181` → `:182`; `connections.tsx:120,208,429,447` → `:121,209,422,440`;
   `resources.tsx:576` → `:584`; `goals/detail.tsx:155,158,159,212,215,216,735` →
   `:150,153,154,207,210,211,730`; `projects.tsx:35,91` → `:37,93`;
   `work-surface.tsx:229,230,236` → `:231,232,238`; `assignment.tsx:239,240` → `:229,230`;
   `control.tsx:154,157,158` → `:150,153,154`, `getQueryData :132` → `:128`,
   `getQueryState :116` → `:112`; `work/detail.tsx:104,105,262` → `:105,106,263`;
   `work/history.tsx:42,43` → `:42,43`; `realtime.ts` `invalidationKeys` `:432-460` →
   `:432-462`.
3. **Site census at HEAD (base state).** 40 literal `queryKey: [` sites in non-test source: 10 in
   `application.ts` (the ten option builders) and 30 across 12 files (`shell.tsx` 1,
   `attention.tsx` 1, `use-chat-scope-state.ts` 1, `connections.tsx` 4, `resources.tsx` 1,
   `goals/detail.tsx` 7, `projects.tsx` 2, `work-surface.tsx` 3, `assignment.tsx` 2,
   `control.tsx` 3, `work/detail.tsx` 3, `work/history.tsx` 2), plus the two positional reads
   inside those files (`control.tsx:112` `getQueryState`, `:128` `getQueryData`) and the two map
   call paths in `realtime.ts`. 36 further literal sites live in 9 test files and stay as the
   independent check (§11 of the plan).
4. **A root literal the contract's evidence did not enumerate.** `realtime.ts:218` held
   `predicate: (query) => query.queryKey[0] !== "session"` — a key-root literal under design
   clause 3; it now reads `!isSessionReadKey(query.queryKey)` (D6).
5. **The map-restatement case.** `realtime.test.ts`'s
   `"maps a resource invalidation onto the query keys that read it"` (`:297-336`, 11
   `invalidationKeys` calls) was its only `invalidationKeys` use; it is deleted per clause 4 with
   the red mutants M1/M2/M3/M10 (§7) proving the table test catches the same defects, and the
   import name goes with it. Every other case in the file is untouched, including the two
   predicate cases (`:181-182`, `:206-207`) and the `flush()` cases that exercise the map through
   `keys()` (`:297-317` batch after the deletion, `:290-294` delivery).

## 3. Module interface (final list, with callers)

| export | shape | callers |
| --- | --- | --- |
| `sessionReadKey()` | `["session"]` | `application.ts` (`sessionOptions`), `shell.tsx:263` |
| `workspaceReadKey(projectId)` / `workspaceReadKey()` | `["workspace", { projectId }]` / `["workspace"]` | `application.ts` (`workspaceOptions`, `runtimesOptions` reuses it), `assignment.tsx:231`, `resources.tsx:585`, `projects.tsx:38,94`, `work-surface.tsx:232,239`, `work/detail.tsx:107,264`, the map |
| `workItemReadKey(workItemId)` / `workItemReadKey()` | `["work-item", { workItemId }]` / `["work-item"]` | `application.ts` (`workDetailOptions`), `work-surface.tsx:233`, `assignment.tsx:230`, `control.tsx:113,151`, `work/detail.tsx:106`, `history.tsx:44`, the map |
| `attentionReadKey(projectId, cursor)` / `attentionReadKey()` | `["attention", { projectId, cursor }]` / `["attention"]` | `application.ts` (`attentionOptions`), `attention.tsx:183`, `goals/detail.tsx:155,212`, the map |
| `goalReadKey(goalId)` / `goalReadKey()` | `["goal", { goalId }]` / `["goal"]` | `application.ts` (`goalOptions`), `goals/detail.tsx:151,208`, the map |
| `goalEventsReadKey(goalId, after)` / `(goalId)` / `()` | `["goal-events", { goalId, after }]` / `["goal-events", { goalId }]` / `["goal-events"]` | `application.ts` (`goalEventsOptions`), `goals/detail.tsx:154,211`, the map |
| `goalEventsCatchUpReadKey(goalId, epoch)` / `()` | `["goal-events-catch-up", { goalId, epoch }]` / `["goal-events-catch-up"]` | `goals/detail.tsx:731`, the map |
| `workItemTimelineReadKey(workItemId, cursor)` / `(workItemId)` / `()` | `["work-item-timeline", { workItemId, cursor }]` / `["work-item-timeline", { workItemId }]` / `["work-item-timeline"]` | `application.ts` (`workTimelineOptions`), `control.tsx:129,154,155`, `history.tsx:43`, the map |
| `connectionsReadKey()` | `["connections"]` | `application.ts` (`connectionsOptions`), `connections.tsx:122,210,423,441`, the map |
| `chatReadKey(scope)` / `chatReadKey()` | `["chat", scope]` / `["chat"]` | `application.ts` (`chatConversationOptions`), `use-chat-scope-state.ts:166`, the map |
| `chatOlderReadKey(scope, before)` | `["chat-older", { ...scope, before }]` | `application.ts` (`chatOlderPageOptions`) |
| `invalidationKeys(payload)` | the change→reads map (8 kinds + `default → []`) | `realtime.ts` `flush():229-232` |
| `isSessionReadKey(key)` | `key[0] === "session"` | `realtime.ts` `invalidateReads():208` |
| `InvalidationPayload` (type) | the pushed change's members | `realtime.ts` (`pending`, `receive`, `flush`) |

Interface budget: 11 builders + 2 invalidation functions + 1 moved type — within the contract's
"11 + at most 4" (D8 records why the post-mutation sets stay composed of builders at their
existing call sites instead of extra exports). `ChatScopeKey` stays in `application.ts`;
`read-cache.ts` takes it with `import type` (erased by `verbatimModuleSyntax`; the value edge is
application → read-cache only, so no runtime cycle — D4). `realtime.ts` imports the map instead
of defining it; the map has one owner.

## 4. Key identity table (base literal → builder → produced key at a sample input)

Every row's produced JSON is a runnable expectation in `read-cache.test.ts` (its expected strings
are the base literals' JSON at the same sample inputs); the base side is the literal verbatim
from `c5d0a4a8` (Appendix A pairs every site). `JSON.stringify` equality is the contract's
identity notion: member names, member order and object-vs-positional shape all participate.

| base literal shape (base sites) | HEAD call | sample | JSON (equal both sides) |
| --- | --- | --- | --- |
| `["session"]` (`application.ts:211`, `shell.tsx:262`) | `sessionReadKey()` | — | `["session"]` |
| `["workspace"]` (map; `resources.tsx:584`, `projects.tsx:37,93`, `work-surface.tsx:231,238`, `work/detail.tsx:106,263`) | `workspaceReadKey()` | — | `["workspace"]` |
| `["workspace", { projectId }]` (`application.ts:218`, `assignment.tsx:230`) | `workspaceReadKey(projectId)` | `"p1"` / `null` | `["workspace",{"projectId":"p1"}]` / `["workspace",{"projectId":null}]` |
| `["work-item"]` (map: `run`, identity-less `work_item`) | `workItemReadKey()` | — | `["work-item"]` |
| `["work-item", { workItemId }]` (`application.ts:223`, `work-surface.tsx:232`, `assignment.tsx:229`, `control.tsx:112,150`, `work/detail.tsx:105`, `history.tsx:43`, map) | `workItemReadKey(workItemId)` | `"w1"` | `["work-item",{"workItemId":"w1"}]` |
| `["attention"]` (map; `attention.tsx:182`, `goals/detail.tsx:154,211`) | `attentionReadKey()` | — | `["attention"]` |
| `["attention", { projectId, cursor }]` (`application.ts:233`) | `attentionReadKey(projectId, cursor)` | `"p1"`, `null` | `["attention",{"projectId":"p1","cursor":null}]` |
| `["goal"]` (map: identity-less `goal`) | `goalReadKey()` | — | `["goal"]` |
| `["goal", { goalId }]` (`application.ts:238`, `goals/detail.tsx:150,207`, map) | `goalReadKey(goalId)` | `"g1"` | `["goal",{"goalId":"g1"}]` |
| `["goal-events"]` (map) | `goalEventsReadKey()` | — | `["goal-events"]` |
| `["goal-events", { goalId }]` (`goals/detail.tsx:153,210`) | `goalEventsReadKey(goalId)` | `"g1"` | `["goal-events",{"goalId":"g1"}]` |
| `["goal-events", { goalId, after }]` (`application.ts:248`) | `goalEventsReadKey(goalId, after)` | `"g1"`, `4` | `["goal-events",{"goalId":"g1","after":4}]` |
| `["goal-events-catch-up"]` (map) | `goalEventsCatchUpReadKey()` | — | `["goal-events-catch-up"]` |
| `["goal-events-catch-up", { goalId, epoch }]` (`goals/detail.tsx:730`) | `goalEventsCatchUpReadKey(goalId, epoch)` | `"g1"`, `2` | `["goal-events-catch-up",{"goalId":"g1","epoch":2}]` |
| `["work-item-timeline"]` (map) | `workItemTimelineReadKey()` | — | `["work-item-timeline"]` |
| `["work-item-timeline", { workItemId }]` (`control.tsx:153,154`, `history.tsx:42`) | `workItemTimelineReadKey(workItemId)` | `"w1"` | `["work-item-timeline",{"workItemId":"w1"}]` |
| `["work-item-timeline", { workItemId, cursor }]` (`application.ts:260`, `control.tsx:128`) | `workItemTimelineReadKey(workItemId, cursor)` | `"w1"`, `null` / `"c1"` | `["work-item-timeline",{"workItemId":"w1","cursor":null}]` / `…"cursor":"c1"}]` |
| `["connections"]` (`application.ts:268`, `connections.tsx:121,209,422,440`, map) | `connectionsReadKey()` | — | `["connections"]` |
| `["chat"]` (map: `chat`) | `chatReadKey()` | — | `["chat"]` |
| `["chat", key]` (`application.ts:328`, `use-chat-scope-state.ts:165`) | `chatReadKey(scope)` | `{"scope":"run","projectId":"p1","runId":"r1"}` | `["chat",{"scope":"run","projectId":"p1","runId":"r1"}]` |
| `["chat-older", { ...key, before }]` (`application.ts:341`) | `chatOlderReadKey(scope, before)` | `"c9"` | `["chat-older",{"scope":"run","projectId":"p1","runId":"r1","before":"c9"}]` |
| `query.queryKey[0] !== "session"` (`realtime.ts:218`) | `!isSessionReadKey(query.queryKey)`, `isSessionReadKey = key[0] === "session"` | `["session"]` / `["workspace"]` / `[]` | `false` / `true` / `true` refreshable (same polarity as base) |

Two further identity rows pinned by the table test but with no base literal of their own:
`goalEventsReadKey`'s and `workItemTimelineReadKey`'s mid forms are exactly the base literals at
`goals/detail.tsx:153` and `control.tsx:153` (rows above); `attentionReadKey(null, "cursor")` is
the builder's only other member order pinned by the test.

## 5. Control-flow audit (lesson 32; base vs HEAD call sequences)

Appendix A carries the raw paired extraction (base line → HEAD line per file, one pair per site,
`git show c5d0a4a8:…` vs the working tree). Every pair differs only in the key expression; the
call options, the `await`/`void` shape, the ordering and the branches are unchanged.

| # | decision point | base | HEAD | preserved |
| --- | --- | --- | --- | --- |
| C1 | `realtime.ts` `flush()` | JSON-dedupe map, `allReads` flag, empty-list early return into `invalidateReads("invalidation")`, per-key `invalidateQueries({ queryKey: key })` loop | identical lines (`:223-243` → `:213-233` shifted); only the key objects' producer changed | yes |
| C2 | `realtime.ts` `invalidateReads()` | `{ predicate: (query) => query.queryKey[0] !== "session" }, { cancelRefetch: source !== "fallback" }`, `.then(settled, settled)` | `{ predicate: (query) => !isSessionReadKey(query.queryKey) }, { cancelRefetch: … }`, same settle arms | yes — same comparison, same polarity |
| C3 | goal command `onSuccess` | `await goal → await settleGoalRead → notice → void goal-events → void attention` | same order, same `await`/`void`; `settleGoalRead` untouched | yes |
| C4 | plan approval `onSuccess` | same sequence with its own notice string | same | yes |
| C5 | goal receipts catch-up read | `useQuery({ queryKey: [...], queryFn, enabled: epoch > 0 && nextAfter === null && tail > 0 })` | key via builder; `queryFn`/`enabled` byte-identical | yes |
| C6 | `resources.tsx` `reconcile` | called before `if (!live()) return` in `syncResource` success/error and `syncProject` success/error, and from `saved` | identical call sites (the function body's key only) | yes |
| C7 | `control.tsx` `invalidate` | work-item invalidate, then `isFetching(timeline-mid) === 0` guard around the timeline invalidate | identical guard and order | yes |
| C8 | `control.tsx` `snapshotVersion` / retained pages | `getQueryState([...])`, `getQueryData([...])` positional reads | same calls via builders | yes |
| C9 | work-board move | `Promise.all([workspace, work-item])` on success; workspace-only on error | identical, `request.itemId` mapping kept | yes |
| C10 | assignment `onSettled` | `Promise.all([work-item, …(itemProjectId === null ? [] : [workspace(project)])])` | identical conditional spread | yes |
| C11 | work detail `reRead` / `refresh` | `Promise.all([work-item, workspace])`; `refetchQueries({ queryKey: ["workspace"], type: "active" })` | identical incl. `type: "active"` | yes |
| C12 | history retry | `resetQueries(timeline-mid)` then `invalidateQueries(work-item)` | identical order | yes |
| C13 | connections four sites | `void` in `onError`; `refresh()` returning the promise (awaited by `check`/`remove`); `void … .then(navigate)` in `saved`; `void` in the late-response branch | identical | yes |
| C14 | attention create, chat send, shell logout | `await attention`; `void chat(scope)`; `await session` before `fetchQuery` | identical | yes |

## 6. Gates (all foreground, serialized; no measurement harness beside a suite)

Host note: every `pnpm` invocation runs as `mise x pnpm@12.17.0 -- pnpm --trust-lockfile
--config.verify-deps-before-run=false …` (the workspace pins pnpm 12.17.0). The host's npm feed
does not carry `knip@6.46.1`, `@vitest/coverage-v8@5.1.1`, `msw@3.0.7` (the merged #240 bump;
`pnpm view` fails for all three), so the one-time `pnpm install --no-frozen-lockfile` ran with a
temporary `overrides` block pinning exactly those three to the feed-available
`6.45.1`/`5.1.0`/`3.0.5`; both `pnpm-workspace.yaml` and `pnpm-lock.yaml` were restored
immediately after (`git status --porcelain` empty, re-checked), and none of the three is a gate
tool (§10).

| # | command | base (`c5d0a4a8`, clean worktree) | head (final tree) |
| --- | --- | --- | --- |
| 1 | `pnpm run typecheck` (`tsc --noEmit`) | exit 0, 0 diagnostics | exit 0, 0 diagnostics (three head runs: pre-fix, post-fix, final tree) |
| 2 | `pnpm run lint` (`eslint . --max-warnings 229`) | exit 0, **218** warnings, 0 errors | exit 0, **218** warnings, 0 errors — final run on the frozen tree; the only head transient was 2 `array-type` errors in the new test, fixed before the final runs |
| 3 | `pnpm run knip` | exit 1, 3 pre-existing unused exported types (`PortalSeedInput`, `PortalResponder`, `Reads` — report-only per the W4-09 record) | exit 1, the **same 3** findings — no new unused export; every new export is consumed |
| 4 | `pnpm exec vitest run src/app/read-cache.test.ts src/services/realtime.test.ts src/app/application.test.ts` | — (the first file does not exist) | exit 0, 3 files / **147** tests |
| 5 | `pnpm test` (`vitest run`) | 23 files / **568** tests — 567 passed, 1 red: `assignment.test.tsx > offers only the item's project's resources of the matching kind` (heading-wait), which passes in isolation (10/10, exit 0) — the host-load flake class W4-10 recorded (§6.1) | 24 files / **605** tests, exit 0 — the predicted 568 − 1 + 38; run twice (before and after the whitespace-only EOF cleanup of `realtime.ts`), both green |
| 6 | `git diff --check` | exit 0 | exit 0 (final tree; the pre-cleanup tree flagged one trailing blank line in `realtime.ts`, fixed) |
| 7 | grep gates (design clause 3) | 40 literal key sites outside the module | 0 `queryKey: [` sites in non-test source; root-literal gates in Appendix B (distinctive roots: module-only; ordinary-word roots: every remaining hit classified as non-key vocabulary) |
| 8 | key identity | base literals (Appendix A) | table §4 + `read-cache.test.ts` rows 1-24 |
| 9 | mutations | — | §7 (10 flips, each red, each reverted hash-verified) |
| 10 | DLP greps over added lines and new files (profile-path pattern, backslash-u) | — | 0 matches (final pass: the profile-name path pattern and a case-insensitive backslash-u scan over the diff and the four new documents return nothing) |

Frozen-tree hashes (every gate row above ran on these bytes):
`read-cache.ts 66e61d38a43c1a2d…`, `read-cache.test.ts c7dd9ca4e9cef683…`,
`realtime.ts 099dc64d1d3b67e6…`, `realtime.test.ts 5700c62ce3e9fb9f…`

### 6.1 Host-load context for the baseline red

The baseline full-suite run finished 23 files / 568 with exactly one red —
`assignment.test.tsx > work assignment > offers only the item's project's resources of the
matching kind` (`openAssignmentEditor`'s `findByRole` heading wait, `:136`). The same file
passes in isolation immediately after (`1 file / 10 tests`, exit 0). This is the recorded
wall-clock-budget class: W4-10's evidence §6.1 lists the same case failing on its base3/base4
runs and passing everywhere in isolation, and this host runs several slice worktrees
concurrently. The head full-suite row is judged against this: a red in the same class is
classified the same way with the isolation re-run.

Suite arithmetic: base 23 files / 568 cases; the deleted `realtime.test.ts` case is one `it`, the
new file adds 38 cases (24 `it.each` builder rows + 13 `it.each` kind rows + 1 plain `it`), so the
head expectation is 24 files / 568 − 1 + 38 = **605** cases.

## 7. Mutation flips (each applied alone, run, reverted; hashes verified)

Every flip below is a real run against the delivered test file; the four delivered files were
hash-verified before the round and after every revert
(`read-cache.ts 66e61d38…`, `read-cache.test.ts c7dd9ca4…`, `realtime.test.ts 5700c62c…`,
`application.ts 906b6a3a…`; `sha256sum -c` → all OK at the end of the round), and the post-round
focused run is green (38/38).

| mutant | flip | red (measured, exit 1 each) |
| --- | --- | --- |
| M1 | `work_item` kind drops `attentionReadKey()` | `read-cache.test.ts` — 3 failed / 35: all three `work_item` kind rows |
| M2 | `goal` kind drops `goalEventsCatchUpReadKey()` | `read-cache.test.ts` — 2 failed / 36: both `goal` kind rows |
| M3 | `work_item` kind reordered (attention first) | `read-cache.test.ts` — 3 failed / 35: the same three rows (order pinned) |
| M4 | `workspaceReadKey()` returns `["workspace", { projectId: null }]` | `read-cache.test.ts` — 9 failed / 29: the `workspaceReadKey()` tuple row + every kind row carrying `["workspace"]` |
| M5 | `attentionReadKey` full form member order swapped (`{ cursor, projectId }`) | `read-cache.test.ts` — 2 failed / 36: both full-form rows |
| M6 | `workItemTimelineReadKey(workItemId)` returns the full form with `cursor: null` | `read-cache.test.ts` — 1 failed / 37: the mid-form row |
| M7 | `chatOlderReadKey` spread order reversed (`{ before, ...scope }`) | `read-cache.test.ts` — 1 failed / 37: the `chat-older` row (member order) |
| M8 | `goalEventsReadKey(goalId)` returns `{ goalId, after: 0 }` | `read-cache.test.ts` — 1 failed / 37: the mid-form row |
| M9 | `isSessionReadKey` inverted (`!==`) | `read-cache.test.ts` + `realtime.test.ts` — 3 failed / 54: the predicate case and both realtime predicate cases (`:181-182`, `:206-207`) |
| M10 | `runtime` kind returns `[workspaceReadKey(), connectionsReadKey()]` | `read-cache.test.ts` — 1 failed / 37: the `runtime` kind row |

The M1/M2/M3/M10 rows are exactly the assertions of the clause-4-deleted realtime case (its
identity-bearing and identity-less rows, the key order, the `runtime` row), and M9 shows the
predicate's realtime cases still kill it after the deletion — the recorded proof clause 4 asks
for. The deleted case's remaining rows (`connection`, `resource`, `project`, the two default
rows) are pinned by the table test's rows of the same names.

## 8. Comments and deletions (owner comment policy)

- `realtime.ts` loses the map's two-line doc comment with the moved function; the module states
  the contract in one line ("an empty list means every read, never a drop"), and the "empty list
  refetches broadly" wording survives in the test row names.
- The three per-kind why comments move with their cases verbatim (attention carries blocked work;
  run changes are work-item-keyed; the catch-up continuation reads the same served log). No other
  comment is added anywhere: the new module, the new test and every touched hunk carry zero new
  prose beyond those carried why-comments.
- No stale comment, banner or commented-out code existed in the touched hunks; the deleted
  realtime case's attention-scoping comment leaves with it (its fact is now the module's
  work_item case comment, from which it was originally duplicated).
- No lint suppression, no `--max-warnings` change, no dependency change.

## 9. Out of scope / not changed (with reasons)

- **Which reads a change refreshes: unchanged.** The map's kinds, keys, ordering and
  `default → []` semantics are byte-identical; the `chat-older` gap is not fixed. Assessment
  (contract: record, do not change): the gap is **intended by the retention design** —
  `chatOlderPageOptions`'s docstring says "the page it returns is retained by the surface rather
  than by this cursor's own key"; the query runs only through `fetchQuery` on an explicit press
  (no mounted observer ever holds the key), so a root invalidation of `["chat-older", …]` would
  have no observable effect, and a per-`(scope, before)` key has no mapping from a change. No
  `docs/TODO.md` entry is added.
- `settleGoalRead` and its call sites keep their interleaving (W5-04's surface); `api.ts`
  read/write split and refusal classification are merged predecessors; no selected-project hook
  (W5-03); no test-harness change; no `docs/design` amendment (no design document names the key
  vocabulary or the map — grep over `docs/design/` finds no `queryKey`/`invalidationKeys`).
- `docs/TODO.md` and `docs/archive/CLOSED-TODO.md`: no entry covers the key vocabulary or closes
  here (greps for `read-cache`, `query key`, `A5`, `chat-older` find nothing relevant), so both
  files are untouched.
- The contract file at its GDD path is left unchanged, as the merged W4-09/W4-10 contracts were;
  the harness/conductor owns its status.

## 10. Residual risks and limits

- **Host environment.** Three pinned dev-dependency versions are unservable on this host (feed 404
  + absent from the local store), so the local install is not the lockfile's exact tree: `knip`
  6.45.1, `@vitest/coverage-v8` 5.1.0, `msw` 3.0.5 instead of 6.46.1/5.1.1/3.0.7. None
  participates in a gate (`knip` is report-only by the W4-09 record — its base is already red with
  the 3 pre-existing findings; coverage is off; `msw` is not imported by any file this slice
  touches). `pnpm-workspace.yaml` and `pnpm-lock.yaml` are byte-identical to HEAD in the delivered
  tree. The browser acceptance suite is CI-only and was not run here.
- **Identity proof is two-sided but not a byte-diff of history.** The builders' outputs are pinned
  by the table test against the base literals' JSON at the same sample inputs (the test's expected
  strings), and the 30 call-site pairs (Appendix A) show base-literal → builder-call 1:1 with no
  other change; the unchanged consumer suites assert their own literal keys against the served
  behaviour.
- **Deleted restatement.** `realtime.test.ts` loses 11 direct `invalidationKeys` assertions; §7
  records the mutants its replacement kills, and the surviving `keys()`-based cases still exercise
  the map through `flush()`.
- **`realtime.test.ts`'s remaining predicate cases** pin the every-read predicate through the
  mocked client (M9), so the `isSessionReadKey` indirection has independent coverage in the file
  that owns the pass.

## Appendix A — call-site pairing (base `c5d0a4a8` line → head line)

Raw extraction: `git show c5d0a4a8:frontend/<file>` and the working tree, lines matching
`invalidateQueries|refetchQueries|resetQueries|getQueryData|getQueryState|isFetching(|queryKey:`,
paired in file order. One pair per site; every pair differs only in the key expression.

```
src/app/application.ts
  B 211:  queryKey: ["session"],                                  H 212:  queryKey: sessionReadKey(),
  B 218:  queryKey: ["workspace", { projectId }],                 H 219:  queryKey: workspaceReadKey(projectId),
  B 223:  queryKey: ["work-item", { workItemId }],                H 224:  queryKey: workItemReadKey(workItemId),
  B 233:  queryKey: ["attention", { projectId, cursor }],         H 234:  queryKey: attentionReadKey(projectId, cursor),
  B 238:  queryKey: ["goal", { goalId }],                         H 239:  queryKey: goalReadKey(goalId),
  B 248:  queryKey: ["goal-events", { goalId, after }],           H 249:  queryKey: goalEventsReadKey(goalId, after),
  B 260:  queryKey: ["work-item-timeline", { workItemId, cursor }] H 261: queryKey: workItemTimelineReadKey(workItemId, cursor),
  B 268:  queryKey: ["connections"],                              H 269:  queryKey: connectionsReadKey(),
  B 328:  queryKey: ["chat", key],                                H 329:  queryKey: chatReadKey(key),
  B 341:  queryKey: ["chat-older", { ...key, before }],           H 342:  queryKey: chatOlderReadKey(key, before),
src/services/realtime.ts
  B 239:  for (const key of keys.values()) … queryKey: key });    H 229:  (unchanged; the key objects come from the module)
  B 217/[218]: .invalidateQueries( { predicate: (query) => query.queryKey[0] !== "session" },
                                                                  H 207/[208]: { predicate: (query) => !isSessionReadKey(query.queryKey) },
src/app/shell.tsx
  B 262:  … invalidateQueries({ queryKey: ["session"] });         H 263:  … invalidateQueries({ queryKey: sessionReadKey() });
… (attention.tsx B182→H183, use-chat-scope-state.ts B165→H166, connections.tsx
B121/209/422/440→H122/210/423/441, resources.tsx B584→H585, goals/detail.tsx
B150/153/154/207/210/211/730→H151/154/155/208/211/212/731, projects.tsx B37/93→H38/94,
work-surface.tsx B231/232/238→H232/233/239, assignment.tsx B229/230→H230/231,
control.tsx B112/128/150/153/154→H113/129/151/154/155, work/detail.tsx B105/106/263→H106/107/264,
work/history.tsx B42/43→H43/44) — full raw file: the implement-stage report's paired output.
```

## Appendix B — grep gates (design clause 3), head tree

Command: `grep -rn "queryKey: \[" src --include=*.ts --include=*.tsx | grep -v '\.test\.'`
(run in `frontend/`) —

```
(none)
```

Command per root: `grep -rn "\"<root>\"" src --include=*.ts --include=*.tsx | grep -v '\.test\.'` —

```
== root "session" ==
src\app\read-cache.ts:14:export function sessionReadKey(): readonly ["session"] {
src\app\read-cache.ts:15:  return ["session"];
src\app\read-cache.ts:113:export const isSessionReadKey = (key: readonly unknown[]): boolean => key[0] === "session";

== root "work-item" ==
src\app\read-cache.ts:24:export function workItemReadKey(): readonly ["work-item"];
src\app\read-cache.ts:25:export function workItemReadKey(workItemId: string): readonly ["work-item", { workItemId: string }];
src\app\read-cache.ts:27:  return workItemId === undefined ? ["work-item"] : ["work-item", { workItemId }];

== root "goal-events" ==
src\app\read-cache.ts:42:export function goalEventsReadKey(): readonly ["goal-events"];
src\app\read-cache.ts:43:export function goalEventsReadKey(goalId: string): readonly ["goal-events", { goalId: string }];
src\app\read-cache.ts:44:export function goalEventsReadKey(goalId: string, after: number): readonly ["goal-events", { goalId: string; after: number }];
src\app\read-cache.ts:46:  if (goalId === undefined) return ["goal-events"];
src\app\read-cache.ts:47:  return after === undefined ? ["goal-events", { goalId }] : ["goal-events", { goalId, after }];

== root "goal-events-catch-up" ==
src\app\read-cache.ts:50:export function goalEventsCatchUpReadKey(): readonly ["goal-events-catch-up"];
src\app\read-cache.ts:51:export function goalEventsCatchUpReadKey(goalId: string, epoch: number): readonly ["goal-events-catch-up", { goalId: string; epoch: number }];
src\app\read-cache.ts:53:  if (goalId === undefined || epoch === undefined) return ["goal-events-catch-up"];
src\app\read-cache.ts:54:  return ["goal-events-catch-up", { goalId, epoch }];

== root "work-item-timeline" ==
src\app\read-cache.ts:57:export function workItemTimelineReadKey(): readonly ["work-item-timeline"];
src\app\read-cache.ts:58:export function workItemTimelineReadKey(workItemId: string): readonly ["work-item-timeline", { workItemId: string }];
src\app\read-cache.ts:59:export function workItemTimelineReadKey(workItemId: string, cursor: string | null): readonly ["work-item-timeline", { workItemId: string; cursor: string | null }];
src\app\read-cache.ts:61:  if (workItemId === undefined) return ["work-item-timeline"];
src\app\read-cache.ts:62:  if (cursor === undefined) return ["work-item-timeline", { workItemId }];
src\app\read-cache.ts:63:  return ["work-item-timeline", { workItemId, cursor }];

== root "chat-older" ==
src\app\read-cache.ts:76:export function chatOlderReadKey(scope: ChatScopeKey, before: string): readonly ["chat-older", ChatScopeKey & { readonly before: string }] {
src\app\read-cache.ts:77:  return ["chat-older", { ...scope, before }];

== root "goal" ==
src\app\read-cache.ts:36:export function goalReadKey(): readonly ["goal"];
src\app\read-cache.ts:37:export function goalReadKey(goalId: string): readonly ["goal", { goalId: string }];
src\app\read-cache.ts:39:  return goalId === undefined ? ["goal"] : ["goal", { goalId }];
src\app\read-cache.ts:94:    case "goal":

== root "chat" ==
src\app\legacy.ts:33:  if (fragment === "chat") {
src\app\read-cache.ts:70:export function chatReadKey(): readonly ["chat"];
src\app\read-cache.ts:71:export function chatReadKey(scope: ChatScopeKey): readonly ["chat", ChatScopeKey];
src\app\read-cache.ts:73:  return scope === undefined ? ["chat"] : ["chat", scope];
src\app\read-cache.ts:99:    case "chat":
src\app\shell.tsx:166:          {(!filter || "chat".includes(filter)) && … }   ← command-palette label match
src\app\shell.tsx:171:          {filter && projects.length === 0 && !"attention".includes(filter) && !"chat".includes(filter) && … }  ← palette label match

== root "attention" ==
src\app\legacy.ts:59:  if (fragment === "health" || fragment === "attention") return scoped("/portal/attention");
src\app\read-cache.ts:30-33  (builder)
src\app\shell.tsx:165,171      (palette label matches)

== root "connections" ==
src\app\legacy.ts:26:  if (fragment === "connections") {
src\app\read-cache.ts:66-67  (builder)
src\app\shell.tsx:168,171    (palette label matches)
src\services\api.ts:48:export type Connection = ConnectionsReadModel["connections"][number];   ← generated member name

== root "workspace" ==
src\app\legacy.ts:36 / src\app\portal-double.ts:74,75,142,150,311 / src\app\router.tsx:31 /
src\components\primitives.tsx:22,24,25 / src\features\chat\chat-sections.tsx:39,226,233 /
src\features\chat\presentation.ts:74,104 / src\features\chat\scopes.ts:94 /
src/features/chat/use-chat-scope-state.ts:108,117,144,146 /
src\features\planning\editors.tsx:307,328,475,476 / src\features\planning\write-policy.ts:281,291,299,300 /
src\features\work\assignment.tsx:291 / src\app\read-cache.ts:18,19,21
   ← chat-scope values and comparisons, StatusScope, seed members, a search-param default,
     editor field names, write-policy field lists, the builder itself
```

Classification: `"work-item"`, `"goal-events"`, `"goal-events-catch-up"`,
`"work-item-timeline"`, `"chat-older"` and `"session"` appear only inside
`frontend/src/app/read-cache.ts`; every remaining hit of the ordinary words (`"workspace"`,
`"goal"`, `"chat"`, `"attention"`, `"connections"`) is non-key vocabulary (route fragments,
palette label matching, chat scopes, field names, seed/type members, generated member names) and
constructs no query key.

Array-shape gate: `grep -rnE
'\["(session|workspace|work-item|attention|goal|goal-events|goal-events-catch-up|work-item-timeline|connections|chat|chat-older)"'
src --include=*.ts --include=*.tsx | grep -v '\.test\.' | grep -v '^src.app.read-cache.ts'` —

```
src\app\portal-double.ts:75,142,150              (seed type members)
src\app\router.tsx:31                            chatScopeSearchValues = ["workspace", "project", "run"]
src\features\planning\write-policy.ts:299,300    field-pair tables, e.g. ["workspace", "workspace"]
src\services\api.ts:48                           ConnectionsReadModel["connections"] member
```

All four are non-key arrays; no array whose first element is a key root exists outside the
module.
