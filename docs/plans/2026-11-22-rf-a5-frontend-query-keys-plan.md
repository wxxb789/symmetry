# rf-a5-frontend-query-keys plan — one `frontend/src/app/read-cache.ts` owns every read key and the change→reads map

Contract: `goal-rf-a5-frontend-query-keys.md` (GDD, 2026-11-21), binding with its design clauses
(one module, byte identity, grep gates, the table test with red mutants), the strict owned set, the
per-success-path control-flow audit (lesson 32), the mutation-proof rule and the owner comment
policy (2026-11-22). Base revision `c5d0a4a8` (`refactor(frontend): classify refusals in one
module (#241)`, this worktree's dispatch HEAD; `git status --porcelain` empty at dispatch).

Shape references: `docs/plans/2026-11-22-rf-w4-10-refusal-classification-plan.md` and
`docs/evidence/rf-w4-10-refusal-classification-2026-11-22.md` (same-stage GDD shape: dispatch
gates, measured basis, decisions with alternatives, lesson-32 audit, mutation table, residual
risks); `docs/plans/2026-11-22-rf-w4-09-api-reads-writes-split-plan.md`.

Host note: every `pnpm` invocation runs as `mise x pnpm@12.17.0 -- pnpm --trust-lockfile
--config.verify-deps-before-run=false ...` (the workspace pins pnpm 12.17.0). The host's npm feed
does not carry `knip@6.46.1`, `@vitest/coverage-v8@5.1.1`, `msw@3.0.7` (the merged #240 bump;
`pnpm view` fails for all three), so the one-time `pnpm install --no-frozen-lockfile` ran with a
temporary `overrides` block in `pnpm-workspace.yaml` pinning exactly those three to the
feed-available `6.45.1`/`5.1.0`/`3.0.5`; both `pnpm-workspace.yaml` and `pnpm-lock.yaml` were
restored immediately after the install (`git status --porcelain` empty, re-checked), and none of
the three is a gate tool (see §12).

## Dispatch gates (checked at dispatch, base `c5d0a4a8`)

| gate | state at dispatch |
| --- | --- |
| W3-12 (`#190`) UI primitives/resource-dialog collapse merged | yes; `resources.tsx` is at its post-W3-12 line state |
| W4-11 (`#220`) test harness merged | yes; `app/test-harness.tsx` + `app/portal-double.ts` own the DOM fixtures |
| W4-08 (`#232`) ChatPage decomposition merged | yes — it moved the chat send invalidation out of `chat.tsx` into `features/chat/use-chat-scope-state.ts` (§2.1) |
| W4-09 (`#239`) merged | yes |
| W4-10 (`#241`) merged | yes (`c5d0a4a8`, this HEAD); `goals/detail.tsx` is at its post-W4-10 line state |
| open PRs touching the owned set | none: `gh pr list --repo wxxb789/symmetry --state open --json number,title` → `[]` |
| owned files untouched by open work | `git status --porcelain` empty |

## 2. Re-derivation of the owned set against HEAD

The contract re-derived its coordinates against `origin/main 79548368`; several sites moved after
that revision, and one contract item is already satisfied. Re-derived per site:

1. **`chat.tsx` holds no key site at HEAD.** W4-08 split ChatPage: the send success path now lives
   in `frontend/src/features/chat/use-chat-scope-state.ts:165`
   (`invalidateQueries({ queryKey: ["chat", sendIdentity] })`). That file joins the owned set for
   that one site (the contract's `chat.tsx:321`); `chat.tsx` itself is not edited (it contains no
   `queryKey` occurrence). The same hook imports `type ChatScopeKey` from `application.ts` — an
   import line outside this slice's edit surface, which is why the type stays where it is (D4).
2. **Line drift on every other site** (contract coordinate → HEAD coordinate, re-derived by
   `grep -rn "queryKey: \[" src` minus tests): `application.ts` option builders
   `:210-341` → `:211-341`; `shell.tsx:262` → `:262`; `attention.tsx:181` → `:182`;
   `connections.tsx:120,208,429,447` → `:121,209,422,440`; `resources.tsx:576` → `:584`;
   `goals/detail.tsx:155,158,159,212,215,216,735` → `:150,153,154,207,210,211,730`;
   `projects.tsx:35,91` → `:37,93`; `work-surface.tsx:229,230,236` → `:231,232,238`;
   `assignment.tsx:239,240` → `:229,230`; `control.tsx:154,157,158` → `:150,153,154` plus
   `getQueryData :132` → `:128` and `getQueryState :116` → `:112`; `work/detail.tsx:104,105,262` →
   `:105,106,263`; `work/history.tsx:42,43` → `:42,43`; `realtime.ts` `invalidationKeys`
   `:432-460` → `:432-462`.
3. **Site census at HEAD.** 40 literal `queryKey: [` sites in non-test source: 10 in
   `application.ts` (the ten option builders) and 30 across 12 files
   (`shell.tsx` 1, `attention.tsx` 1, `use-chat-scope-state.ts` 1, `connections.tsx` 4,
   `resources.tsx` 1, `goals/detail.tsx` 7, `projects.tsx` 2, `work-surface.tsx` 3,
   `assignment.tsx` 2, `control.tsx` 3, `work/detail.tsx` 3, `work/history.tsx` 2) — the
   contract's census, shifted as above. The in-memory key references outside `queryKey: [` are
   `control.tsx:112` `getQueryState([...])` and `control.tsx:128` `getQueryData<...>([...])` (both
   in the census above as key sites), plus `use-older-page-loading.ts:39`, which already derives
   its key from `chatConversationOptions(application.api, identity).queryKey` — it holds no
   literal and needs no edit. No `setQueryData`/`removeQueries`/`prefetchQuery` exists in
   production code.
4. **The every-read predicate is a root literal the contract's evidence missed.**
   `realtime.ts:218` reads `predicate: (query) => query.queryKey[0] !== "session"` — a key-root
   literal under design clause 3 ("no root string literal remains in non-test `frontend/src`
   outside the module"), because `"session"` is one of the eleven roots. It moves to the module as
   `isSessionReadKey` (D6); realtime keeps the predicate call shape byte-identically.
5. **`realtime.test.ts`'s map-restatement case** (`"maps a resource invalidation onto the query
   keys that read it"`, `:297-336`, 11 `invalidationKeys` calls; the only `invalidationKeys` use in
   the file, import at `:5`) is deleted under clause 4 with the recorded red mutants of §10, and the
   now-unused import name is dropped as part of that deletion. Every other case in the file is
   untouched; the `keys()`-based cases (`:296-317` batch, `:288-293` delivery) keep verifying the
   map indirectly through `flush()`.

Owned set as delivered (grouped by edit class): new `frontend/src/app/read-cache.ts` and
`frontend/src/app/read-cache.test.ts`;
key construction `app/application.ts`; map + predicate consumer
`services/realtime.ts`; key-expression-only call sites in `app/shell.tsx`,
`features/attention/attention.tsx`, `features/chat/use-chat-scope-state.ts`,
`features/connections/connections.tsx`, `features/connections/resources.tsx`,
`features/goals/detail.tsx`, `features/projects/projects.tsx`,
`features/projects/work-surface.tsx`, `features/work/assignment.tsx`,
`features/work/control.tsx`, `features/work/detail.tsx`, `features/work/history.tsx`; the
mutation-proven deletion in `services/realtime.test.ts`; docs (`docs/plans/...`,
`docs/evidence/...`). Oracles never edited: every other test file (§11).

## measured basis (base `c5d0a4a8`)

| artifact | measurement |
| --- | --- |
| `app/application.ts` | 355 L; the ten option builders with literal keys `:211` `["session"]`, `:218`, `:223`, `:233`, `:238`, `:248`, `:260`, `:268`, `:328`, `:341` |
| `services/realtime.ts` | 462 L; `invalidationKeys` `:432-462` (8 kinds + `default → []`), `flush()` `:224-240` (JSON-dedupe, empty-list fallback to `invalidateReads`), the `"session"` predicate `:218` |
| `features/chat/use-chat-scope-state.ts` | 248 L; `:165` `["chat", sendIdentity]` |
| `features/work/control.tsx` | 468 L; `:112` `getQueryState`, `:128` `getQueryData`, `:150-154` invalidate/timeline-guard |
| success-path key sites (12 files) | 30 literal `queryKey: [` sites (§2.3): 26 `invalidateQueries`, 1 `refetchQueries` (`type: "active"`, `work/detail.tsx:263`), 1 `resetQueries` (`work/history.tsx:42`), 1 `isFetching` (`control.tsx:153`), 1 read option (`goals/detail.tsx:730`, the catch-up `useQuery`); plus the two positional reads `control.tsx:112` (`getQueryState`), `:128` (`getQueryData`) |
| test restatements | 36 literal `queryKey: [` sites in 9 test files: `connections.test.tsx` 14, `router.test.tsx` 7, `realtime.test.ts` 5, `planning.test.tsx` 4, `chat.test.tsx` 2, `application.test.ts` 1, `goal-attention.test.tsx` 1, `assignment.test.tsx` 1, `control.test.tsx` 1; `browser/` has none |
| `services/realtime.test.ts` | 608 L; the deleted case `:297-336` |

Baseline gate rows (typecheck, lint warning count, knip, full-suite file/case counts,
`git diff --check`) are recorded in the evidence record §6; the same commands are re-run on the
final tree.

## Module shape

```ts
// frontend/src/app/read-cache.ts
import type { ChatScopeKey } from "./application";

export interface InvalidationPayload { kind: string; id?: string; project_id?: string; work_item_id?: string; goal_id?: string; run_id?: string; version?: number; cursor?: number }

export function sessionReadKey(): readonly ["session"];
export function workspaceReadKey(): readonly ["workspace"];
export function workspaceReadKey(projectId: string | null): readonly ["workspace", { projectId: string | null }];
export function workItemReadKey(): readonly ["work-item"];
export function workItemReadKey(workItemId: string): readonly ["work-item", { workItemId: string }];
export function attentionReadKey(): readonly ["attention"];
export function attentionReadKey(projectId: string | null, cursor: string | null): readonly ["attention", { projectId: string | null; cursor: string | null }];
export function goalReadKey(): readonly ["goal"];
export function goalReadKey(goalId: string): readonly ["goal", { goalId: string }];
export function goalEventsReadKey(): readonly ["goal-events"];
export function goalEventsReadKey(goalId: string): readonly ["goal-events", { goalId: string }];
export function goalEventsReadKey(goalId: string, after: number): readonly ["goal-events", { goalId: string; after: number }];
export function goalEventsCatchUpReadKey(): readonly ["goal-events-catch-up"];
export function goalEventsCatchUpReadKey(goalId: string, epoch: number): readonly ["goal-events-catch-up", { goalId: string; epoch: number }];
export function workItemTimelineReadKey(): readonly ["work-item-timeline"];
export function workItemTimelineReadKey(workItemId: string): readonly ["work-item-timeline", { workItemId: string }];
export function workItemTimelineReadKey(workItemId: string, cursor: string | null): readonly ["work-item-timeline", { workItemId: string; cursor: string | null }];
export function connectionsReadKey(): readonly ["connections"];
export function chatReadKey(): readonly ["chat"];
export function chatReadKey(scope: ChatScopeKey): readonly ["chat", ChatScopeKey];
export function chatOlderReadKey(scope: ChatScopeKey, before: string): readonly ["chat-older", ChatScopeKey & { readonly before: string }];
export function invalidationKeys(payload: InvalidationPayload): readonly (readonly unknown[])[];
export const isSessionReadKey = (key: readonly unknown[]): boolean;
```

Callers (final list): the eleven builders — `application.ts` (the ten option builders) and the
thirty post-mutation sites in §2.2/§2.3, plus `realtime.ts`'s map; `invalidationKeys` —
`realtime.ts:221` `flush()` only (base `:231`); `isSessionReadKey` — `realtime.ts:208`
`invalidateReads`'s predicate only (base `:218`); `InvalidationPayload` — `realtime.ts`
(`pending`, `receive`, `flush`).
`invalidationKeys` + `isSessionReadKey` are the two exported invalidation functions: within the
contract's "at most 4" budget (D8).

The builders select the tuple shape by arity: 0 args = the root prefix used for invalidation
(`["workspace"]`), the full arity = the exact key used by a read. Overloads make each call shape
type-enforced, so an `undefined`-capable variable cannot silently degrade an exact key to a prefix
(D2). `undefined` is not a member value in any base shape, and `null` is only ever a member value
(`workspace`, `attention`, `work-item-timeline` cursors) — arity keeps the two apart.

The map itself moves verbatim except that its key expressions are the builders:

```ts
const identity = payload.id ?? payload.work_item_id;
switch (payload.kind) {
  case "work_item": return [...(identity === undefined ? [workItemReadKey()] : [workItemReadKey(identity)]), workItemTimelineReadKey(), workspaceReadKey(), attentionReadKey()];
  case "run": return [workItemReadKey(), workItemTimelineReadKey(), workspaceReadKey()];
  case "goal": return [...(identity === undefined ? [goalReadKey()] : [goalReadKey(identity)]), goalEventsReadKey(), goalEventsCatchUpReadKey(), attentionReadKey()];
  case "chat": return [chatReadKey()];
  case "connection": case "resource": return [connectionsReadKey(), workspaceReadKey()];
  case "project": return [workspaceReadKey(), connectionsReadKey()];
  case "runtime": return [workspaceReadKey()];
  default: return [];
}
```

`realtime.ts` then imports `invalidationKeys`, `isSessionReadKey` and
`type InvalidationPayload`; its `invalidateReads` predicate becomes
`(query) => !isSessionReadKey(query.queryKey)`; `flush()` is untouched. No re-export exists on
either side (clean cutover, as W4-09/W4-10 did).

## 4. Base → builder table (every shape, byte identity by construction)

| base literal shape (base sites) | HEAD call | identical |
| --- | --- | --- |
| `["session"]` (`application.ts:211`, `shell.tsx:262`) | `sessionReadKey()` | yes |
| `["workspace"]` (`app:…` map; `resources.tsx:584`, `projects.tsx:37,93`, `work-surface.tsx:231,238`, `detail.tsx:106,263`) | `workspaceReadKey()` | yes |
| `["workspace", { projectId }]` (`application.ts:218`, `assignment.tsx:230`, map `{ workItemId: identity }`-analogue) | `workspaceReadKey(projectId)` | yes |
| `["work-item"]` (map `run`/`work_item` identity-less) | `workItemReadKey()` | yes |
| `["work-item", { workItemId }]` (`application.ts:223`, `work-surface.tsx:232`, `assignment.tsx:229`, `control.tsx:112,150`, `detail.tsx:105`, `history.tsx:43`, map) | `workItemReadKey(workItemId)` | yes |
| `["attention"]` (map; `attention.tsx:182`, `goals/detail.tsx:154,211`) | `attentionReadKey()` | yes |
| `["attention", { projectId, cursor }]` (`application.ts:233`) | `attentionReadKey(projectId, cursor)` | yes |
| `["goal"]` (map identity-less) | `goalReadKey()` | yes |
| `["goal", { goalId }]` (`application.ts:238`, `goals/detail.tsx:150,207`, map) | `goalReadKey(goalId)` | yes |
| `["goal-events"]` (map) | `goalEventsReadKey()` | yes |
| `["goal-events", { goalId }]` (`goals/detail.tsx:153,210`) | `goalEventsReadKey(goalId)` | yes |
| `["goal-events", { goalId, after }]` (`application.ts:248`) | `goalEventsReadKey(goalId, after)` | yes |
| `["goal-events-catch-up"]` (map) | `goalEventsCatchUpReadKey()` | yes |
| `["goal-events-catch-up", { goalId, epoch }]` (`goals/detail.tsx:730`) | `goalEventsCatchUpReadKey(goalId, epoch)` | yes |
| `["work-item-timeline"]` (map) | `workItemTimelineReadKey()` | yes |
| `["work-item-timeline", { workItemId }]` (`control.tsx:153,154`, `history.tsx:42`) | `workItemTimelineReadKey(workItemId)` | yes |
| `["work-item-timeline", { workItemId, cursor }]` (`application.ts:260`, `control.tsx:128`) | `workItemTimelineReadKey(workItemId, cursor)` | yes |
| `["connections"]` (`application.ts:268`, `connections.tsx:121,209,422,440`, map) | `connectionsReadKey()` | yes |
| `["chat"]` (map) | `chatReadKey()` | yes |
| `["chat", key]` (`application.ts:328`, `use-chat-scope-state.ts:165`) | `chatReadKey(scope)` | yes |
| `["chat-older", { ...key, before }]` (`application.ts:341`) | `chatOlderReadKey(scope, before)` | yes |

The table test pins the `JSON.stringify` of every row at sample inputs including `null` members;
the evidence record carries the generated equality table.

## 5. Edge-case semantics the contract demands

1. **Byte identity.** Root strings, member names, member order and object-vs-positional shape are
   unchanged for all 20 rows, so TanStack partial-key matching behaves identically (the only
   consumers are the option builders and the invalidation calls, both keyed by structural match).
2. **Call-sequence identity.** Every success path keeps its `invalidateQueries` / `refetchQueries`
   / `resetQueries` / `getQueryData` / `getQueryState` / `isFetching` calls with the same options,
   the same order and the same branches (`type: "active"` at `work/detail.tsx:263` included). The
   §7 audit is filled row-by-row in the evidence record.
3. **The map moves with its meaning.** The eight kind cases, the two identity-less variants, the
   `default → []` contract and `flush()`'s empty-list → every-read fallback are preserved; the
   every-read predicate still excludes exactly `["session"]`-rooted keys.
4. **Frozen options.** `staleTime: 15_000`, `retry: readRetry`, the mutation `retry: false`, the
   `sessionOptions` `staleTime: 0`/`gcTime: 0`, the chat `staleTime: 0` and the session cancel
   (`application.ts` `expireSession`) are untouched.
5. **The `chat-older` gap is intended, not fixed** (contract: record, do not change).
   `chatOlderPageOptions`'s docstring states the design: the page is retained by the surface
   ("the page it returns is retained by the surface rather than by this cursor's own key"), the
   query runs only through `fetchQuery` on an explicit press and no mounted observer ever holds
   the key — so a key-root invalidation of `["chat-older", ...]` would have no observable effect,
   and the per-`(scope, before)` keys carry no mapping from a change. Recorded here and in the
   evidence record; no `docs/TODO.md` entry is added.
6. **Comments.** The owner policy applies to every touched hunk: the map keeps its one-line
   purpose plus the three per-kind why comments (non-obvious read coupling: attention carries
   blocked work, run changes are work-item-keyed, the catch-up continuation is the same served
   log); nothing else is added, and none of the existing comments is carried over verbatim where
   the new home makes it redundant (the deleted realtime.test case's comment moves to the table
   test's case name).

## 7. Control-flow audit (lesson 32)

The evidence record carries the base-vs-HEAD table with a preserved yes/no per row. The rows that
need a stated equivalence rather than a mechanical one:

- `realtime.ts` `flush()`: the JSON-dedupe map, the `allReads` flag, the early return into
  `invalidateReads("invalidation")` and the per-key `invalidateQueries` loop are untouched; only
  the key objects change producer.
- `realtime.ts` `invalidateReads`: both arms keep their `cancelRefetch` (`source !== "fallback"`)
  and the settle bookkeeping; the predicate body is the same comparison reached through
  `isSessionReadKey`.
- `goals/detail.tsx` both success paths: `await goal → await settleGoalRead → void goal-events →
  void attention`, with the notice between settle and the events invalidate — the interleaving
  (which no single "set" export could express) is preserved exactly.
- `resources.tsx` `reconcile` runs before each early return (`if (!live()) return`) in all four
  success/error paths; unchanged.
- `control.tsx` `invalidate`: the timeline invalidate stays guarded by the `isFetching === 0`
  check; `snapshotVersion`/`getQueryData` keep their positional reads.
- `work-surface.tsx` keeps the `Promise.all` of the two invalidations and the separate
  `workspace`-only error path; `assignment.tsx` keeps the conditional `workspace` member of its
  `Promise.all`; `work/detail.tsx` keeps the `refetchQueries` `type: "active"` refresh;
  `work/history.tsx` keeps the reset-then-invalidate order; `connections.tsx` keeps the four call
  sites' `void`/`await` and `.then()` shapes; `shell.tsx` keeps the awaited session invalidate
  before `fetchQuery`.

## 8. Decisions (and the alternatives)

| # | decision | rationale / alternative |
| --- | --- | --- |
| D1 | module name `frontend/src/app/read-cache.ts` (the contract's proposal; no existing file or symbol collision) | `app/` already owns the option builders that consume it; alternative `services/read-cache.ts` rejected: the keys are app-level vocabulary, not a service |
| D2 | arity-selected shapes via overloads, not optional-`undefined` parameters | an optional `projectId?: string \| null` parameter accepts an `undefined`-capable variable and silently yields the prefix; overloads type-error that call. Alternative rejected on that failure mode; a rest-tuple union (`...args: [] \| [string \| null]`) has the same safety but is less idiomatic |
| D3 | prefix and mid keys are produced by the same builder (arity 0 / short arity), not by extra exports | keeps "one exported builder per root" and the 11 + ≤4 budget; the base shapes are exactly the builder's arities (20 shapes over 11 roots) |
| D4 | `ChatScopeKey` stays in `application.ts`; `read-cache.ts` takes it with `import type` | `verbatimModuleSyntax` erases the type import, so there is no runtime cycle (the value edge is application → read-cache only); moving the type would edit the two chat hooks' import lines (not in the owned set) or add a re-export alias. Alternative: declare a structural copy — rejected as a second vocabulary |
| D5 | `InvalidationPayload` moves into the module (exported) with the map | the map's parameter contract belongs with the map; `realtime.ts` imports the type. Alternative: keep it in realtime and type-import back — leaves one signature across two modules |
| D6 | `isSessionReadKey` joins the invalidation exports | the `"session"` root literal at `realtime.ts:218` must leave non-test source under clause 3; the predicate keeps the exact comparison |
| D7 | delete `realtime.test.ts`'s map-restatement case per clause 4 | the case's 11 assertions only restate `invalidationKeys`; the table test supersedes it with the recorded red mutants (§10); every other case in the file, including those exercising `flush()` through `keys()`, stays unedited |
| D8 | no named post-mutation set exports; the sets stay composed of builders at their existing call sites | the contract caps invalidation exports at 4 and the 12 write families carry distinct multi-key sequences interleaved with awaits/branches (e.g. the goal settle between the first and second key); restructuring them into set iterations would alter the lesson-32 sequences. The evidence record carries the per-success-path table instead. Conductor amendment 2026-11-22 (review REV-A02): design clause 1(c) is amended to this builders-only shape — a success path composes its reads from the module's exported builders at the call site, the module exports no named set where the sequence interleaves awaits, notices, conditional spreads or `isFetching` guards, and no follow-up slice is scheduled |
| D9 | no `docs/design/*.md` amendment | no design document names the key roots or the invalidation map (`grep -rn "queryKey\|invalidationKeys" docs/design/` finds nothing); this is code hygiene inside frozen presentation behaviour |
| D10 | `docs/TODO.md` unchanged | no open entry covers the key vocabulary (grep for `read-cache`, `query key`, `A5`, `chat-older` finds none); the `chat-older` assessment lands in §5.5 and the evidence record, not as new debt |

## 9. Evidence plan

All commands foreground and serialized; no measurement harness beside a suite (the sequencing
rule). Baseline rows at `c5d0a4a8`, head rows on the final tree.

| # | command | proves |
| --- | --- | --- |
| 1 | `pnpm --filter @symmetry/frontend run typecheck` | the cutover compiles |
| 2 | `pnpm --filter @symmetry/frontend run lint` (`--max-warnings 229`) | ratchet held; measured count ≤ base |
| 3 | `pnpm --filter @symmetry/frontend run knip` | no new unused export |
| 4 | `pnpm --filter @symmetry/frontend exec vitest run src/app/read-cache.test.ts src/services/realtime.test.ts src/app/application.test.ts` | the new table test + the two map/predicate consumers |
| 5 | `pnpm --filter @symmetry/frontend test` | full suite; head count = base − the deleted case + the new table cases |
| 6 | `git diff --check` | whitespace/EOF hygiene |
| 7 | grep gates of design clause 3, one command per root (plus the `queryKey: [` gate) | no root literal outside the module |
| 8 | key-identity table generation (base literal JSON vs builder-call JSON at sample inputs) | byte identity per §4 row |
| 9 | per-success-path call-sequence extraction (base vs HEAD, `git show c5d0a4a8:<file>` diffed by call) | the §7 audit |
| 10 | mutation flips (§10), each applied alone, run red, reverted | clause 4's last condition + the sanctioned deletion |
| 11 | DLP greps over added lines (profile-path pattern, backslash-u) | the program-wide rules |
| 12 | `git status --porcelain` at the end | the edit set is exactly the owned files; oracles untouched |

## 10. Mutation list

Each applied alone to the final tree, run, reverted (the contract's floor: drop a key from one
kind / reorder a member).

| mutant | flip | expected red |
| --- | --- | --- |
| M1 | drop `attentionReadKey()` from the `work_item` kind | table `work_item` rows |
| M2 | drop `goalEventsCatchUpReadKey()` from the `goal` kind | table `goal` rows |
| M3 | reorder the `work_item` kind list (attention first) | table `work_item` row (order pinned) |
| M4 | `workspaceReadKey()` returns `["workspace", { projectId: null }]` | table prefix row + all prefix callers' identity |
| M5 | `attentionReadKey` full form swaps member order (`{ cursor, projectId }`) | table attention full row |
| M6 | `workItemTimelineReadKey(workItemId)` returns the full form with `cursor: null` | table timeline mid row (and the mid-form callers) |
| M7 | `chatOlderReadKey` spread order reversed (`{ before, ...scope }`) | table chat-older row (member order) |
| M8 | `goalEventsReadKey(goalId)` returns `{ goalId, after: 0 }` | table goal-events mid row |
| M9 | `isSessionReadKey` inverted | table predicate row + `realtime.test.ts` predicate cases (`:180-181`, `:205-206`) |
| M10 | `runtime` kind returns `[workspaceReadKey(), connectionsReadKey()]` | table runtime row |

## 11. Oracles that must pass unmodified

Every existing test file except the clause-4 deletion in `services/realtime.test.ts`:
`application.test.ts`, `router.test.tsx`, `chat.test.tsx`, `chat-hooks.test.tsx`,
`connections.test.tsx`, `goal-attention.test.tsx`, `planning.test.tsx`, `assignment.test.tsx`,
`control.test.tsx`, `work/detail.test.tsx`, `presentation.test.ts`, `receipts.test.ts` and every
other suite in the full run. Their literal keys stay as the independent check against the module.

## 12. Out of scope (with reasons)

Contract clause 4 verbatim: changing which reads a change refreshes (incl. the `chat-older` gap —
recorded in §5.5, unchanged); `services/api.ts` read/write split (W4-09, merged); refusal
classification (W4-10, merged); selected-project hook (W5-03); settle-wait budgets (W5-04 — its
`settleGoalRead` and call sites keep their interleaving); test harness (W4-11, merged).
Additionally: the `ChatScopeKey` type's home (D4); the realtime module's socket/polling/refresh
logic; no new dependency, no React provider, no registry; no lint suppression; no
`--max-warnings` change; the contract file at its GDD path stays untouched (as the merged
W4-09/W4-10 contracts did — the harness owns its status); browser specs untouched.

## 13. Residual risks

- The temporary install overrides are host-side only; both YAML files are restored and the final
  `git status` proves it. If the feed regains the three pinned versions, the next clean install
  needs no workaround.
- The table test pins the module's own outputs; the independent check that they equal base lives
  in the evidence record's generated identity table plus the unmodified consumer suites (whose
  mock assertions carry their own literal keys).
- `realtime.test.ts` loses 11 direct `invalidationKeys` assertions; the replacements are the table
  rows M1-M3, M10 flip red (recorded), and the surviving `keys()`-based cases still exercise the
  map through `flush()`.
