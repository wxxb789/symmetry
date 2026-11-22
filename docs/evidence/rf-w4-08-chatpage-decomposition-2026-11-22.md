# rf-w4-08-chatpage-decomposition — evidence record (2026-11-22)

Subject: `ChatPage` became wiring only. The scoped conversation surface's three decomposed
regions are the hooks `useChatScopeState` (URL scope math + one store binding + the send/decision
status machine), `useChatScrollRestoration` (the owed/recorded offset effects, the pane's scroll
rule and the router `onRendered` re-assertion) and `useOlderPageLoading` (the `Load older` press
machine incl. press-time capture and the refusal recovery); the presentational sections
`ChatScopeBar`, `ChatDecisionSection`, `ChatEvidenceList`, `ChatTranscript` and `ChatComposer`
carry the DOM. Contract: `goal-rf-w4-08-chatpage-decomposition.md` (GDD, 2026-11-21/22, binding
conductor decisions D1/D2, the dispatch gates, lesson 32 and the owner comment policy of
2026-11-22). Base revision `e84973a76abd46125cad08ed76710cec446388f4` (this worktree's dispatch
HEAD, all base coordinates below). Plan:
`docs/plans/2026-11-22-rf-w4-08-chatpage-decomposition-plan.md`. Judging artifact (contract
clause 2, "answer.md"): this record plus the implement-stage report.

Dispatch gates re-checked at `e84973a7`: W4-11 (`#220`, the same commit: `app/portal-double.ts`,
`app/test-harness.tsx`, the pool split) and `#164` (W2-15) are merged — `chat.test.tsx` measures
2062 L / 48 cases and `presentation.ts` 455 L at the base, so the pre-dispatch numbers (2086 /
454) are superseded by the re-measured pair.

## 1. Deliverable shape (files, counts)

| file | status | lines (raw) | note |
| --- | --- | ---: | --- |
| `frontend/src/features/chat/chat.tsx` | rewritten | 734 → 255 | `ChatPage` `:108-255` (148 raw lines; 127 eslint-counted) + module helpers `conversationRunOf`, `servedRunOf`, `runSignalsOf`, `chatRunFacts`, `chatHistoryNote`, `scopeProjectOf`, `chatContextLabel` |
| `frontend/src/features/chat/use-chat-scope-state.ts` | new | 258 | `chatScopeParams`, `buildChatRequest`, `definitiveRefusal`, `sameRequestBody`, `useChatScopeState` |
| `frontend/src/features/chat/use-chat-scroll-restoration.ts` | new | 74 | `ChatScopeScroll`, `RouterRenderedEvents`, `panePinnedBelowOffset`, `useChatScrollRestoration` |
| `frontend/src/features/chat/use-older-page-loading.ts` | new | 58 | `definitiveReadRefusal`, `useOlderPageLoading` |
| `frontend/src/features/chat/chat-sections.tsx` | new | 286 | `servedRuns`, `runOptionLabel`, `ServedRun`, `ChatScopeBar`, `ChatDecisionView`, `ChatDecisionSection`, `ChatEvidenceView`, `ChatEvidenceList`, `EmptyScopeView`, `ChatHistoryView`, `ChatMessageRow`, `ChatTranscript`, `ChatComposerEdit`, `ChatComposer` |
| `frontend/src/features/chat/chat-hooks.test.tsx` | new | 314 | 8 tests (see §8) |
| `frontend/src/features/chat/scopes.ts` | edited | 268 → 268 | two `export` keywords only: `interface ChatScopeState` and `interface ChatScopeStore` become exported (type-only; see §10) |
| `docs/plans/2026-11-22-rf-w4-08-chatpage-decomposition-plan.md` | new | — | plan with decisions P1-P9 |
| `docs/evidence/rf-w4-08-chatpage-decomposition-2026-11-22.md` | new | — | this record |
| `chat.test.tsx`, `presentation.test.ts`, `presentation.ts`, `router.tsx`, the harness files | untouched | — | frozen oracle + harness; `git status` lists only `chat.tsx` and `scopes.ts` as modified tracked files |

## 2. Unit sizes (eslint `max-lines-per-function` 150 / `complexity` 25, measured with a `max: 1`
and `complexity: 0` probe so every function reports its count; blank lines and comments skipped)

| unit | base | head |
| --- | --- | --- |
| `ChatPage` | 446 L / cx 96 | **127 L / cx 20** |
| `chatRunFacts` | inline in `ChatPage` | 19 L / cx 12 |
| `conversationRunOf` / `servedRunOf` / `runSignalsOf` / `scopeProjectOf` | inline | 3 L/cx 4 · 7 L/cx 11 · 15 L/cx 10 · 3 L/cx 3 |
| `chatHistoryNote` | inline | 5 L / cx 5 |
| `chatContextLabel` | inline (the `chatScopeContext` literal) | 13 L / cx 9 |
| `ChatMessageRow` | 13 L / cx 2 | 16 L / cx 3 |
| `runOptionLabel` | 3 L / cx 1 | 3 L / cx 1 |
| `servedRuns` | inline (`runRows` literal, 6 L / cx 6) | 9 L / cx 1 |
| `ChatScopeBar` | inline | 27 L / cx 5 |
| `ChatDecisionSection` | inline | 20 L / cx 4 |
| `ChatEvidenceList` | inline | 13 L / cx 2 |
| `ChatTranscript` | inline | 46 L / cx 7 |
| `ChatComposer` | inline | 85 L / cx 9 |
| `useChatScopeState` | — | 121 L / cx 9 (`send` 25 L/cx 1, `submit` 24 L/cx 6, `sendDecision` 17 L/cx 9, `edit` 3 L/cx 1, `onSuccess` 6 L/cx 3, `onError` 7 L/cx 4) |
| `buildChatRequest` | inline `buildRequest` (inside the 446 L `ChatPage`) | 48 L / cx 18 |
| `chatScopeParams` | inline (the URL parse, 6 lines) | 13 L / cx 6 |
| `useChatScrollRestoration` | — | 43 L / cx 1 (effects 6 L/cx 4, 7 L/cx 3, callback 7 L/cx 3; `onScroll` 11 L/cx 5; `rememberLeavingScope` 7 L/cx 6) |
| `panePinnedBelowOffset` | 4 L / cx 3 | 4 L / cx 3 |
| `useOlderPageLoading` | — | 45 L / cx 2 (`load` 30 L/cx 3, error callback 16 L/cx 4, retain callback 3 L/cx 1) |

Every delivered function is inside the caps (the largest are `ChatPage` 127/20 and
`useChatScopeState` 121/9; the most complex is `ChatPage` cx 20, then `buildChatRequest` cx 18),
and the base's `ChatPage` pair (`max-lines-per-function` 446, `complexity` 96) is gone — which is
exactly the two-warning drop the repo-wide count shows (§6). The moved closures kept their base
sizes (`load` 30 vs 31, the refusal error callback 16, the retain callback 7, `send` 25 vs 26).
The plan's "cx <20 per unit" is report-only per the contract.

## 3. Extraction map (base `chat.tsx` region → delivered unit)

| base span | responsibility | delivered unit |
| --- | --- | --- |
| `:79-86` | URL search → `urlScope`/`urlProjectId`/`urlRunId`/`identity`/`key`/`scopeComplete` | `chatScopeParams` (`use-chat-scope-state.ts`), called by `ChatPage` before the reads and again inside the hook |
| `:91-104` | one `useChatScopeStore` binding, keyed `stateOf`, `latestStateOf` ref + layout effect | `useChatScopeState` (same call sites; the store's own `update`/`updateWith` are returned raw and called with the key, as at base) |
| `:106-124` | conversation read, directory read, `runRows` | `ChatPage` + `servedRuns` (`chat-sections.tsx`) |
| `:122-141` | conversation/directory run projection, adapter/pause, `runSignals` | `conversationRunOf`/`servedRunOf`/`runSignalsOf`/`chatRunFacts` (helpers in `chat.tsx`; the inline expressions are unchanged) |
| `:142-146` | `intents`, stored-intent fallback, `startFields` | `applicableChatIntents` call in `ChatPage`; fallback + `startFields` inside `useChatScopeState` |
| `:148-156` | start-work resource read, repository/CI rows, scope project, target project | `resourcesProjectId`/`targetProjectId` in the hook; the read + row filters in `ChatPage`; `scopeProjectOf` helper |
| `:159-169` | `chatHistoryView` + the empty/failed history note | `ChatPage` + `chatHistoryNote` helper |
| `:171-175` | served-page retention effect (`retainServedPage`, `servedSource` identity guard) | `ChatPage`, unchanged byte-for-byte |
| `:187-247` | `olderLoads` + `loadOlder` press machine | `useOlderPageLoading` |
| `:249-292` | scroll ref, owed/recorded layout effects, `onRendered` subscription, pane scroll rule, leave capture | `useChatScrollRestoration` |
| `:301-411` | mutation, `send`, `buildRequest`, `submit`, `sendDecision` | `useChatScopeState` (`buildChatRequest` is the module-level pure builder) |
| `:430-454` | `rememberLeavingScope`, `changeScope`, `changeRun` | `ChatPage` wiring over `scrollRestoration.rememberLeavingScope` + `scope.update` |
| `:456-474` | decision/evidence/links/empty view model | `chatRunFacts` (decision/failure/evidence/adapter/run state/run-select), `chatRunLinks` call, `emptyScope` literal in `ChatPage` |
| `:478-500` | scope bar DOM | `ChatScopeBar` |
| `:503-519` | scope-context DOM | `ChatPage` (unchanged DOM, rendered inline) |
| `:521-533` | decision packet DOM | `ChatDecisionSection` (null decision renders nothing) |
| `:535-545` | evidence DOM | `ChatEvidenceList` (empty rows render nothing) |
| `:547-582` | transcript pane DOM + `ChatMessageRow` | `ChatTranscript` + `ChatMessageRow` |
| `:584-605` | history gap/note/`Load older` DOM | `ChatTranscript` (the `aria-disabled` value is the hook's `pending`) |
| `:607-668` | composer DOM | `ChatComposer` (edit patches + submit event out) |
| `:672-734` | `runOptionLabel`, `panePinnedBelowOffset`, `ChatMessageRow`, `definitiveRefusal`, `sameRequestBody`, `definitiveReadRefusal` | `chat-sections.tsx` (first three), `use-chat-scope-state.ts` (write refusal + body equality), `use-older-page-loading.ts` (read refusal) |

The plan named four sections; the delivered tree has five: `ChatTranscript` is the pane + history
controls section, split out because keeping that DOM inline left `ChatPage` at cx 31 (> 25) while
the contract's clause 1 demands `ChatPage` be wiring only inside the same caps. The four named
sections exist with their listed responsibilities unchanged; the fifth is a pure presentational
split of the same kind (recorded deviation from the plan row's count).

## 4. Control-flow audit (lesson 32)

Per unit, base expression → head equivalent. `yes` means the same condition and the same branch
outcome, not merely the same statement text.

| # | base | head | preserved |
| --- | --- | --- | --- |
| A1 | `!!search.scope ?? "workspace"`, `?? null` members, `chatScopeKey`, `scopeComplete` = `workspace \|\| (project ? projectId !== null : runId !== null)` | `chatScopeParams` returns the same six values from the same expressions | yes |
| A2 | `stateOf(key)`; `useRef(stateOf)` + `useLayoutEffect(() => { latestStateOf.current = stateOf; })` (no dep array) | same ref/effect, same position (registered before the scroll effects) | yes |
| A3 | `edit` clears `{kind:"unconfirmed"}` else keeps `status`; spreads patch over the record | same callback body (now created by the hook, still per render) | yes |
| A4 | stored intent used iff `intents.includes(state.intent)` else `intents[0] ?? "discuss"` | same expression | yes |
| A5 | `send` early exits: none (it is the write); `submit` guard order `!scopeComplete` → `status==="sending"` → build (`guidance` byte bound → `start_work` target → run control) → frozen-body reuse; `sendDecision` guard `status==="sending"` → run item → execution → waiting → run id | identical order; the two object-identity guards become value guards: `servedRunExecution === null` ⟺ `runGeneration === null`, `waiting === null` ⟺ `waitingTransitionId === null` (both source values are non-nullable members of their records) | yes |
| A6 | `send` success writes `saved-question`/`saved-command`, clears `frozen`, clears the draft iff it is the sent content, then invalidates `["chat", sendIdentity]`; error writes `refused` (and clears `frozen`) or `unconfirmed` with `source` from `request.intent` | identical, same expressions | yes |
| A7 | `buildRequest` returns the exact request object or writes a notice and returns null | `buildChatRequest` returns `{kind:"request"}` or `{kind:"notice"}`; `submit` writes the notice patch then returns (same three messages, same branch order) | yes |
| B1 | owed layout effect (no deps): return when node null or owed null; set `scrollTop`; settle iff it stuck | identical | yes |
| B2 | recorded layout effect: return when node null; set `scrollTop`; mark owed iff it did not stick; deps `[key, markScrollRestore, scrollOffsetOf]` | identical (same dep identities) | yes |
| B3 | `router.subscribe("onRendered", …)`: return when node null; set the recorded offset; mark owed iff it did not stick; deps `[key, markScrollRestore, router, scrollOffsetOf]` | identical | yes |
| B4 | pane `onScroll`: pinned-below → `markScrollRestore(max(recorded, owed ?? 0))` and return; else record the shown offset; settle iff owed existed and differs | identical order and expressions | yes |
| B5 | `rememberLeavingScope`: return when node null; return when pinned below; record `node.scrollTop`; settle iff owed existed and differs | identical | yes |
| B6 | `panePinnedBelowOffset`: `scrollTop < wanted && range < wanted && scrollTop === max(0, range)` | identical | yes |
| C1 | `loadOlder`: return when the cursor is null; press = `{key, cursor, nextBoundaryId}`; return when `pendingOlderLoadFor(olderLoads, press)`; admit; `fetchQuery(chatOlderPageOptions(api, identity, cursor))` | identical (the fetch uses `press.cursor`, the same value the closure captured) | yes |
| C2 | success: `retainOlderPage(previous, cursor, press.boundaryId, page)` | `retainOlderPage(previous, press.cursor, press.boundaryId, page)` — same value | yes |
| C3 | error: `definitiveReadRefusal` (Conflict/Validation, `UnexpectedResponse` 400); refused → captured-scope re-read gate `ownsChainPointer(latestStateOf.current(key), cursor, press.boundaryId) && isFetching === 0`; committed write returns `previous` when refused and not owned; refused reset = served page's `nextBefore`/oldest message id + notice; non-refusal = notice only; `.finally` clears the press | identical conditions, same `latestStateOf` (last committed store) semantics | yes |
| C4 | `aria-disabled` = `pendingOlderLoadFor(olderLoads, {key, history.loadCursor, state.nextBoundaryId})` under a `history.loadCursor !== null` render guard | hook `pending` = `cursor !== null && pendingOlderLoadFor(olderLoads, {key, cursor, boundaryId})` — equal whenever the button renders | yes |
| D1 | retention effect body + deps `[conversation.data, key, updateWith]` | identical body + deps (the raw store `updateWith` identity, so the effect re-runs exactly when base did) | yes |
| D2 | `conversation.data !== undefined && conversation.data.scope === "run"` | `conversation?.scope === "run"` — equal (undefined scope is never `"run"`) | yes |
| D3 | decision section rendered iff `urlScope === "run" && decision !== null` | `scope === "run" && <ChatDecisionSection>` with `decision === null` → `null` | yes |
| D4 | evidence section rendered iff `evidence.length > 0` | `<ChatEvidenceList>` with empty rows → `null` | yes |

No early return, dependency array, error short-circuit or default clause changed. The two
deliberate call-site changes are A5's value guards and D2's optional chain (both recorded above);
everything else is a move.

## 5. Byte-identical surfaces (D1)

Method: the multiset of double-quoted string literals of base `chat.tsx` compared against the
union of the five delivered source files (`grep -o '"[^"]*"' | sort | uniq -c`, diff). Every
difference is one of:

- import specifiers of the new modules (`./chat-sections`, `./use-chat-scope-state`,
  `./use-chat-scroll-restoration`, `./use-older-page-loading`) and the moved imports
  (`react`, `@tanstack/react-query`, `../../services/api`, `./presentation`, `./scopes`,
  `../../components/workspace`, `../../app/application`);
- names of new type members and store accessors (`"event"`, `"transition"`, `"command"`,
  `"request"`, `"markScrollRestore"`, `"recordScrollOffset"`, `"scrollOffsetOf"`,
  `"scrollRestoreOf"`, `"settleScrollRestore"`, `"onRendered"`, `"execution"`);
- moved/duplicated counts of import strings.

No rendered text, label, `id`, `className`, `aria-*`, `data-*`, placeholder, option text or
required/disabled literal differs — the diff has no such entry. The frozen oracle
(`chat.test.tsx`, 48 cases, unmodified) and the full suite are green on the delivered tree (§6),
and the render-gate spec `portal-chat.spec.mjs` (`ci.yml:759` in the spec list, `:781`
`--expect-file`; the contract's `:794`/`:816` coordinates predate the CI toolchain commits) is
the second DOM pin, running in CI on this tree.

## 6. Gates (all foreground, serialized)

Baseline rows were taken at `e84973a7` before any edit; head rows on the delivered tree. Host
note: every `pnpm` invocation runs as `mise x pnpm@12.17.0 -- pnpm --trust-lockfile …` (the
workspace pins pnpm 12.17.0; the lockfile policy feed is unreachable on this host, the same
bypass W4-11 recorded).

| # | command | base | head |
| --- | --- | --- | --- |
| 1 | `pnpm --filter @symmetry/frontend typecheck` | exit 0 | exit 0 |
| 2 | `pnpm --filter @symmetry/frontend lint` (`--max-warnings 229`) | exit 0, **223** warnings | exit 0, **218** warnings, 0 errors |
| 3 | `… exec vitest run src/features/chat/chat.test.tsx` | 48 passed, exit 0 | 48 passed, exit 0 (unmodified file) |
| 4 | `… exec vitest run src/features/chat/presentation.test.ts` | 39 passed, exit 0 | 39 passed, exit 0 (unmodified file) |
| 5 | `… exec vitest run src/features/chat/chat-hooks.test.tsx` | — | 8 passed, exit 0 |
| 6 | `pnpm --filter @symmetry/frontend test` | 21 files / 534 tests, exit 0 | 22 files / **542** tests, exit 0 |
| 7 | `git diff --check` | — | exit 0 |
| 8 | DLP greps (profile-path token, backslash-u) over the changed/new files | — | no match (exit 1 from `grep`) |

Suite-count reconciliation (contract clause 1): base 534 executed = 411 declaration sites + 7
`.each` sites expanded; head 542 = base 534 + this slice's 8 new tests in the one new file (all
`it()` sites, no `.each`). Lint per-rule counts (base → head): `no-unnecessary-condition`
70 → 69, `restrict-template-expressions` 40 → 40, `no-invalid-void-type` 27 → 27,
`max-lines-per-function` 16 → 15, `no-non-null-assertion` 14 → 14, `no-deprecated` 13 → 12,
`complexity` 12 → 11, `prefer-nullish-coalescing` 7 → 7, `no-dynamic-delete` 6 → 6,
`prefer-find` 6 → 6, `no-empty-function` 4 → 4, `prefer-optional-chain` 4 → 3,
`dot-notation` 2 → 2, `no-unnecessary-type-conversion` 2 → 2 — 223 → 218, and the −5 is the
removed `ChatPage` `max-lines-per-function` + `complexity` pair, the removed `FormEvent`
deprecation and the two `no-empty-function` findings kept out of the new test file (its
default callbacks are `vi.fn()`).

## 7. Mutation flips (each applied alone, run, then reverted)

| mutant | flip | reddened test (measured) |
| --- | --- | --- |
| M1 press capture | retention reads a live "latest arguments" ref instead of the press's captured cursor/boundary | press-capture test: `fetchBefore` `"cursor-b"` instead of `"cursor-a"` |
| M2 admission predicate | `pendingOlderLoadFor` compares cursor bytes only | press-capture test + same-cursor/different-boundary test: 2 failed |
| M3 ownership predicate | `ownsChainPointer` compares cursor bytes only | refusal-ownership test red (no notice, no reset) |
| M3b supersession guard | the committed refusal write drops the `!ownsChainPointer` early return | superseded-refusal test red (notice written, chain reset) |
| M4 effect order | the owed and recorded layout effects swapped | ordering test red (final pane position) |
| M5 settle condition | the owed effect settles when the pane did **not** accept the offset | owed-until-accepted test red (restore settled) |
| M6 owed-mark | the `onRendered` re-assertion drops the owed-mark | re-assertion test red (owed stays 90 instead of 40) |
| M7 composer patch | the target-project change stops clearing repository/CI | composer test red (patch without the cleared members) |

Each mutant was reverted immediately; `scopes.ts` was re-diffed after the M2/M3 round trips and
carries only its two `export` keywords (§10), and the post-mutation runs of §6 are the delivered
tree's rows.

## 8. New tests (each names the mutant in its title)

`frontend/src/features/chat/chat-hooks.test.tsx`:

1. "captures the press's cursor and boundary at press time and retains the page under them
   (mutant: settle-time arguments)" — press, held response, re-sign, second press blocked, then
   the retained page's `fetchBefore`/`fetchBeforeId` are asserted against the captured pair.
2. "admits a press for a boundary the in-flight load never captured at the same cursor bytes
   (mutant: cursor-byte comparison)" — two-sided admission; two retained bindings under one
   deduplicated fetch.
3. "resets a refused chain while the press owns the boundary across a re-signed cursor
   (mutant: cursor-byte ownership)" — 400 refusal, store re-signs the token at the same boundary;
   the reset lands on the served page's own token/id plus the notice.
4. "leaves a refused chain alone when a moved boundary supersedes the press (mutant: dropped
   supersession guard)" — the superseded refusal writes nothing.
5. "applies the owed offset before the recorded offset and settles the owed restore (mutant:
   swapped effect order)" — scope-switch commit on the real store.
6. "keeps the owed restore owed while the pane clamps it (mutant: inverted settle condition)" —
   the owed offset survives a clamping pane and is re-asserted on the next commit.
7. "re-asserts the recorded offset and marks it owed on the router's own restore (mutant:
   dropped owed-mark)" — the `onRendered` listener marks the recorded offset owed.
8. "emits the target-project change with the cleared resource selections in one patch (mutant:
   dropped cleared members)" — the composer section's emitted patch.

Harness: the older-page tests drive the real `useChatScopeStore` through `useOlderPageLoading`
with a real `Application` (`createApplication`, tracked by `app/test-harness.tsx`'s
`trackApplications`) and the shared portal double (`portal.serve` + `jsonResponse`); the scroll
tests render `useChatScrollRestoration` over the real store with a controllable pane model and a
router listener hub; the composer test renders the section directly. No new fetch double exists;
`test-harness.tsx`'s `renderRoute` mounts the page, which is exactly what a hook-level test must
avoid, so its `trackApplications` helper is the shared piece used here.

## 9. Comment policy (owner, 2026-11-22) — what was removed and what stayed

Removed from the moved code (rationale now lives here and in the plan): the module header,
the `Load older` chain-pointer essay (base `:177-186`, `:195-235`), the served-cursor renewal and
press-capture paragraphs, the send/retry paragraphs (base `:302`, `:376-390`), the
history-note/empty-claim paragraph, the intent-fallback note, the pane-clamp paragraph (base
`:550-557`), the leave-capture paragraph (base `:430-433`), the resource-clearing note in the
composer, the `Cancel and retry stay on the work detail` JSX comment, the docstrings on
`definitiveRefusal`/`sameRequestBody`/`definitiveReadRefusal`/`panePinnedBelowOffset`/
`runOptionLabel` and the `ChatScopeState`/`ChatScopeStore` docstrings that were already
unconsumed at base (they stay in `scopes.ts`, which this slice does not otherwise touch).

Kept (one to two lines, why-only, beside the hazard): the settle-time-store note in
`useChatScopeState`; the owe-until-accepted note and the pane-pinning note in
`useChatScrollRestoration`; the "reader taking over" line (twice); the router re-assertion note;
the incomplete-scope guard note in `submit`; the same-snapshot identity note in `sendDecision`;
the durable-message invalidation note in `send`; the scope-round-trip note in `changeScope`.

## 10. Boundary checks

- **Frozen files**: `git status --porcelain` lists `chat.tsx` and `scopes.ts` as the only modified
  tracked files; `chat.test.tsx`, `presentation.test.ts`, `presentation.ts`, `router.tsx`,
  `app/*` and `vitest.config.ts` are untouched (the oracle run in §6 proves the same 48/39 cases
  by name and count).
- **`scopes.ts`**: the only change is `export` on two existing interfaces
  (`ChatScopeState`, `ChatScopeStore`), which the hooks must name for their params; it is
  type-only (no runtime surface, nothing to mutation-check) and the file's line count is
  unchanged. Both predicates the held tests mutate are byte-identical to base.
- **`presentation.ts`**: unchanged; the sections declare their own view types instead
  (`ChatDecisionView`, `ChatEvidenceView`, `ChatHistoryView`), so `presentation.ts` keeps its
  internal interfaces.
- **Harness**: no harness file changed and no fifth fetch double was added.
- **Identities** (sha256 of the delivered code files, final tree):

| file | sha256 |
| --- | --- |
| `frontend/src/features/chat/chat.tsx` | `5ea497a892b6c3ca810b37996f3514d8cb703d1f2c092bf8f6814917bafa01fb` |
| `frontend/src/features/chat/chat-sections.tsx` | `1cd2ce729d4ce46c27324bb49faf3b29d9387c971b088a439e7e371784ab27a0` |
| `frontend/src/features/chat/use-chat-scope-state.ts` | `882f4c95aca21831d1ac75f0c06bf16b5766c4922a5e6c3774895612e782f813` |
| `frontend/src/features/chat/use-chat-scroll-restoration.ts` | `637125cd30a429f657087bf601f6d58d063f15073795faa1a24291730339a602` |
| `frontend/src/features/chat/use-older-page-loading.ts` | `a3b36832e6faa649f3bad69d83c15ee32464a067c0b1fbdc68fb49179ddaf333` |
| `frontend/src/features/chat/chat-hooks.test.tsx` | `d87b91ef2a06c8e03978c86a1c9750f660d8883937649f222f03326b4631bb1e` |
| `frontend/src/features/chat/scopes.ts` | `fb01f7906dd039727ee1c8d03a027c3e3f8619ddbdeeb0575c25e564f93001d7` |

  (`scopes.ts`'s hash covers the base file plus the two `export` keywords; the frozen oracle
  files and the harness files were not modified and are byte-identical to `e84973a7`.)
- **`docs/TODO.md`**: no W4-08 entry exists, so nothing moved to `docs/archive/CLOSED-TODO.md`.

## 11. Residual risks / known limits

- The scroll ordering/owed tests exercise the extracted hook, not the mounted page; jsdom has no
  layout, so the pane installs a controllable `scrollTop`/`scrollHeight`/`clientHeight` model
  exactly as the frozen oracle's scroll cases do. The page-level scroll behaviours remain pinned
  by `chat.test.tsx` (27 `scrollTop` references), which passed unmodified.
- The render-gate browser spec (`portal-chat.spec.mjs`) is CI-only (Chromium compose stack); it
  was not run locally. The local DOM proxies are the frozen 48-case render oracle plus the
  literal-multiset diff in §5.
- The five-section count deviates from the plan row's four (§3); the extraction map and the
  contract's clause 1 caps are the binding measurements.
- `fetchQuery` still carries the repo's existing deprecation warning (TanStack v5); the quantity
  moved with the code and the count did not grow.
