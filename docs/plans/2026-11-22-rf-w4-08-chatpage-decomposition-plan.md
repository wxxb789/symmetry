# rf-w4-08-chatpage-decomposition plan — ChatPage into three hooks and four sections

Contract: `goal-rf-w4-08-chatpage-decomposition.md` (GDD, 2026-11-21/22), binding with its
conductor decisions D1 (byte-identical surfaces), D2 (dLOC report-only), the dispatch-gate
decisions, lesson 32 (base-vs-HEAD control-flow audit) and the owner comment policy
(2026-11-22). Base revision for every coordinate: `e84973a7`
(`test(frontend): one portal double and a vitest pool split (#220)`, this worktree's HEAD,
tree clean).

Shape references: `docs/plans/2026-11-22-rf-w4-11-frontend-test-harness-plan.md` (same-stage
GDD shape and the shared portal double this slice's tests build on) and
`docs/plans/2026-11-21-rf-w4-02-chat-history-chain-plan.md` (the chat surface's own
previous decomposition — evidence record with per-region control-flow audit).

## Dispatch gates (checked at dispatch, base `e84973a7`)

| gate | state at dispatch |
| --- | --- |
| #164 rf-w2-15-served-value-module merged | yes (`presentation.ts` 455 L, `chat.test.tsx` 2062 L — re-measured, not the 454/2086 of the draft) |
| W4-11 frontend test harness merged | yes — this commit (`#220`); `app/portal-double.ts`, `app/test-harness.tsx`, `test-setup.ts`, `vitest.config.ts` present |
| owned files untouched by open work | `chat.tsx` 734 L at base; the three hook/section files do not exist yet |

## Measured basis (base revision `e84973a7`)

| artifact | measurement |
| --- | --- |
| `frontend/src/features/chat/chat.tsx` | 734 L; `ChatPage` `:78-670`; eslint `max-lines-per-function` counts it as **446 lines, complexity 96** (comments/blank skipped) |
| file-local helpers | `runOptionLabel:672`, `panePinnedBelowOffset:681`, `ChatMessageRow:686`, `definitiveRefusal:708`, `sameRequestBody:718`, `definitiveReadRefusal:731` |
| sub-regions (base lines) | scope/URL parse + store binding `:79-104`; conversation/directory reads `:106-124`; run facts `:122-141`; intent math `:142-146`; resources `:148-156`; history + note `:159-169`; served-page retention effect `:171-175`; older-page machine `:187-247`; scroll restoration `:249-292`; send path `:301-411`; scope switching `:430-454`; view model + JSX `:456-670` |
| `chat.test.tsx` (frozen oracle) | 2062 L, **48** `it()` cases; `vitest run` exit 0 in the baseline |
| `presentation.test.ts` (frozen oracle) | 559 L, **39** cases; `vitest run` exit 0 |
| lint ratchet | `--max-warnings 229`; measured at base: **223** warnings (70 no-unnecessary-condition, 40 restrict-template-expressions, 27 no-invalid-void-type, 16 max-lines-per-function, 14 no-non-null-assertion, 13 no-deprecated, 12 complexity, 7 prefer-nullish-coalescing, 6 no-dynamic-delete, 6 prefer-find, 4 no-empty-function, 4 prefer-optional-chain, 2 dot-notation, 2 no-unnecessary-type-conversion); `ChatPage` carries the max-lines + complexity pair |
| typecheck | `pnpm --filter @symmetry/frontend typecheck` exit 0 |
| full suite | recorded in the evidence record (count reconciled to 411 declaration sites + 7 `.each` sites + this slice's additions) |

Host note: every `pnpm` invocation runs as `mise x pnpm@12.17.0 -- pnpm --trust-lockfile ...`
(the pinned 12.17.0; the workspace lockfile policy feed is not reachable on this host, the
same bypass W4-11 recorded). The gate commands are otherwise the contract's.

## Deliverable shape

| file | units | responsibility | budget |
| --- | --- | --- | --- |
| `frontend/src/features/chat/use-chat-scope-state.ts` (new) | `useChatScopeState` + `buildChatRequest` (module helper) | URL scope parse -> identity/key/complete; one store binding (`stateOf` by key, `latestStateOf` settle ref, `update`/`updateWith`/`edit`); the intent fallback rule (`intentWithin`); the status machine's write side (`send`/`submit`/`sendDecision`) incl. the frozen-identity retry rule | hook ≤150 L |
| `frontend/src/features/chat/use-chat-scroll-restoration.ts` (new) | `useChatScrollRestoration` + `panePinnedBelowOffset` | `scrollRef`, the owed-offset apply, the per-key recorded-offset apply, the `onRendered` re-assertion subscription, the pane's scroll rule (clamp vs reader) and `rememberLeavingScope` | hook ≤150 L |
| `frontend/src/features/chat/use-older-page-loading.ts` (new) | `useOlderPageLoading` + `definitiveReadRefusal` | the `Load older` press state machine: press capture (scope key + cursor + boundary at press time), one-press-per-boundary admission, retention under the captured boundary, the refusal recovery (reset + capture-scope re-read) judged by `ownsChainPointer` | hook ≤150 L |
| `frontend/src/features/chat/chat-sections.tsx` (new) | `ChatScopeBar`, `ChatDecisionSection`, `ChatEvidenceList`, `ChatTranscript`, `ChatComposer` + `servedRuns`/`runOptionLabel`/`ServedRun`/`ChatMessageRow` | presentational sections, byte-identical DOM; `ChatComposer` emits edit patches and the submit event, never store/fetch calls; `ChatTranscript` is the pane + history controls section added during implementation (see the decisions table) | each ≤150 L |
| `frontend/src/features/chat/chat.tsx` (rewritten) | `ChatPage` + `conversationRunOf`, `servedRunOf`, `runSignalsOf`, `chatRunFacts`, `chatHistoryNote`, `scopeProjectOf`, `chatContextLabel` | wiring only: queries, view model, sections, hooks | `ChatPage` ≤150 L, complexity ≤25 |
| `frontend/src/features/chat/chat-hooks.test.tsx` (new) | hook/section unit tests | press capture, two-sided boundary comparison, refusal ownership, scroll restore ordering, composer patch emission | each names its mutant |
| `docs/evidence/rf-w4-08-chatpage-decomposition-2026-11-22.md` (new) | evidence record | before/after lengths, extraction map, control-flow audit, gate outputs, mutation flips, new-test list | — |

No change to `presentation.ts`, `router.tsx`, `chat.test.tsx`, `presentation.test.ts`, the
harness files or any other package. `scopes.ts` gains exactly two `export` keywords
(`ChatScopeState`, `ChatScopeStore`) so the hooks can name the store's record and contract; it is
type-only, keeps the file's line count and changes no runtime surface (delivered; see the
evidence record §10).

## API shape

```ts
// use-chat-scope-state.ts
export interface ChatScopeSearch { scope?: ChatScope; projectId?: string | null; runId?: string | null }
export interface ChatScopeParams { scope; projectId; runId; identity: ChatScopeKey; key: string; complete: boolean }
export function chatScopeParams(search: ChatScopeSearch): ChatScopeParams; // the pure scope math, called by ChatPage before the reads and by the hook

export function useChatScopeState(
  search: ChatScopeSearch,
  writes: { api: Api; intents: readonly ChatIntent[]; runItem: WorkItem | null; runGeneration: number | null; waitingTransitionId: string | null },
): ChatScopeParams & {
  state: ChatScopeState; latestStateOf: RefObject<(key: string) => ChatScopeState>;
  targetProjectId: string; resourcesProjectId: string | null;
  intent: ChatIntent; startFields: boolean; sending: boolean;
  update: ChatScopeStore["update"]; updateWith: ChatScopeStore["updateWith"]; // raw store writers, called with the key (base call sites)
  edit(patch: Partial<ChatScopeState>): void;
  submit(event: SyntheticEvent<HTMLFormElement>): void;
  sendDecision(option: ChatDecisionOption): void;
  scroll: Pick<ChatScopeStore, "scrollOffsetOf" | "recordScrollOffset" | "scrollRestoreOf" | "markScrollRestore" | "settleScrollRestore">;
};

// use-chat-scroll-restoration.ts
export type ChatScopeScroll = Pick<ChatScopeStore, "scrollOffsetOf" | "recordScrollOffset" | "scrollRestoreOf" | "markScrollRestore" | "settleScrollRestore">;
export interface RouterRenderedEvents { subscribe(event: "onRendered", listener: () => void): () => void }
export function useChatScrollRestoration(key: string, scroll: ChatScopeScroll, router: RouterRenderedEvents, scrollRef: RefObject<HTMLDivElement | null>): {
  onScroll(event: UIEvent<HTMLDivElement>): void;
  rememberLeavingScope(): void;
}; // the page owns the pane ref (React 19's compiler rules forbid touching a hook-owned ref outside the hook)

// use-older-page-loading.ts
export function useOlderPageLoading(options: {
  key: string; identity: ChatScopeKey;
  cursor: string | null; boundaryId: string | null;
  latestStateOf: RefObject<(key: string) => ChatScopeState>;
  application: Application; queryClient: QueryClient;
  updateWith(key: string, change: (state: ChatScopeState) => ChatScopeState): void;
}): { pending: boolean; load(): void };

// chat-sections.tsx
export interface ServedRun { runId: string; item: WorkItem; execution: NonNullable<WorkItem["execution"]> }
export function servedRuns(projects: readonly Project[]): readonly ServedRun[];
export function ChatScopeBar(props: { scope; runId; runRows; runSelectVisible; onScope(next: ChatScope): void; onRun(runId: string): void });
export interface ChatDecisionView { question: string | null; context: string | null; options: readonly ChatDecisionOption[] }
export function ChatDecisionSection(props: { decision: ChatDecisionView | null; sending: boolean; onChoose(option: ChatDecisionOption): void });
export interface ChatEvidenceView { key: string; source: "event" | "transition" | "command"; kind: string; summary: string | null }
export function ChatEvidenceList(props: { rows: readonly ChatEvidenceView[] });
export interface ChatHistoryView { messages: readonly ChatConversationMessage[]; currentIds: ReadonlySet<string>; gap: { current: string; retained: string } | null; loadCursor: string | null }
export function ChatTranscript(props: { complete; pending; empty: EmptyScopeView; history: ChatHistoryView; historyNote: string | null; olderPending: boolean; onOlderLoad(): void; scrollRef: RefObject<HTMLDivElement | null>; onScroll(event): void });
export interface ChatComposerEdit { intent?; draft?; workTitle?; repositoryId?; ciId?; targetProjectId? }
export function ChatComposer(props: { scope; complete; intent; intents; startFields; projectId; scopeProject; projects; targetProjectId; draft; workTitle; repositoryId; ciId; repositories; ciResources; sending; status; onEdit(patch: ChatComposerEdit): void; onSubmit(event): void });
```

The section view types (`ChatDecisionView`, `ChatEvidenceView`, `ChatHistoryView`,
`EmptyScopeView`, `ResourceOption`, `ProjectOption`) name what each section renders; the
presentation module's own interfaces stay internal, so `presentation.ts` needs no change.

Composition order in the rewritten `ChatPage` preserves the base effect order: the store
binding's `useLayoutEffect` (latest-state ref) is registered before the owed-offset and
recorded-offset `useLayoutEffect`s, which are registered before the retention `useEffect` and
the `onRendered` subscription `useEffect`; layout effects run before passive effects within a
commit, so the base's commit order (latestState -> owed -> recorded; retention -> onRendered)
is preserved.

## Exact edge-case semantics the contract demands

1. **Byte-identical surfaces (D1).** Every element, id, class, label text, option text,
   placeholder, aria attribute, `data-*` attribute, required/disabled mapping and click
   ordering is copied from the base. The sections receive values and emit callbacks; they
   never read the store. The composer's target-project change reaches the store as one patch
   `{ targetProjectId, repositoryId: "", ciId: "" }` exactly as the base `edit` call did.
2. **Response retention writes stay one-per-serve.** The retention effect keeps the base
   `servedSource` identity guard path (`retainServedPage`), its dependency list
   `[conversation.data, key, updateWith]` and its position (after the read, before the scroll
   units).
3. **Press capture.** `load` captures `{ scopeKey: key, cursor, boundaryId }` from the render
   that pressed, before the fetch is issued; the served response is retained with the
   *captured* cursor and boundary (`retainOlderPage(previous, press.cursor, press.boundaryId,
   page)`), not with an argument re-read at settle time. Admission uses
   `pendingOlderLoadFor(olderLoads, press)` (two-sided: boundary id while both name one,
   cursor bytes otherwise) and the `aria-disabled` value is that same predicate over the
   current cursor/boundary.
4. **Refusal recovery.** `definitiveReadRefusal` moves whole (Conflict/Validation, plus
   `UnexpectedResponse` status 400). The captured-scope re-read gate keeps using
   `latestStateOf.current(key)` (last committed store, not the render's own state) and the
   `isFetching === 0` check; the committed write keeps the two-branch update (owned +
   refused -> pointer falls back to `pages[0]`'s token/oldest id and notice; otherwise notice
   only, or no write at all when a superseded refusal arrives).
5. **Scroll ordering.** Owed-offset `useLayoutEffect` (no dependency array, re-runs every
   commit) precedes the `[key, mark, offsetOf]` recorded-offset `useLayoutEffect`; the
   `onRendered` subscription's callback re-asserts the *recorded* offset and marks it owed
   when the pane cannot hold it. The pane's own scroll rule keeps the pinned-below test
   `node.scrollTop < max(recorded, owed) && range < max(recorded, owed) && node.scrollTop === max(0, range)`.
6. **Send path.** Guard order unchanged: `submit` -> `!complete` -> `sending` -> build (byte
   bound -> start_work target -> run control) -> frozen-body reuse. `sendDecision` guard:
   run item, execution/generation, waiting transition, run id all present. Success/error
   statuses and the `invalidateQueries({ queryKey: ["chat", sendIdentity] })` call unchanged.
7. **Comments.** The owner comment policy applies to every hunk: the base's rationale blocks
   (loadOlder chain-pointer essay, intent fallback, history note, send/retry, scope-switch
   clamp) move to this plan/evidence and the module headers are dropped; only 1-2 line
   why-only comments for lifecycle/DOM hazards survive beside their lines.

## Decisions (and the alternatives)

| # | decision | rationale |
| --- | --- | --- |
| P1 | The status machine's write side (`send`/`submit`/`sendDecision` + request building) lives inside `useChatScopeState`, not as `ChatPage` closures | the contract names `useChatScopeState` as the "store binding + scope/status/index math" owner; every write that moves `ChatSendStatus` is that status math, and leaving it in `ChatPage` would make the page carry domain guards and miss `≤150 L` (base region `:301-411` is ~110 L). Request building is a module-level pure function `buildChatRequest` returning `{request} | {notice}`; the notice patch is written by the hook. |
| P2 | `useChatScopeState` takes the resolved `intents` list (not the intent) | the intent fallback rule is stored-state math (`intents.includes(state.intent) ? ... : intents[0] ?? "discuss"`), but `intents` itself comes from the served run facts, so the page passes the list and the hook owns the rule. |
| P3 | Scroll accessors stay one store binding, and the pane ref is caller-owned | `useChatScopeStore()` holds `useState`/`useRef`; a second call would create a second store. The one binding is returned as `scroll` and passed to `useChatScrollRestoration(key, scroll, router, scrollRef)` — the contract's `useChatScrollRestoration(scopeKey)` signature plus the store slice it acts on. `scrollRef` is a parameter (the page creates it) because React 19's compiler rules reject both reading a hook-returned ref during render and writing it outside render; the delivered shape keeps `<div ref={paneRef}>` canonical in `ChatPage` and the tests. |
| P4 | `useOlderPageLoading` keeps both clients the base used (`application.queryClient` for `fetchQuery`, `useQueryClient()` for `isFetching`/`refetchQueries`) | under the single `QueryClientProvider` they are the same instance, but the extraction preserves the exact access sites instead of relying on that identity. |
| P5 | New tests are hook-level (`renderHook`/`render`) over the real store and the shared portal double, plus one section-level patch test | the frozen oracle already pins the page's end-to-end behaviours; the contract asks for the extracted units' state transitions (press capture, boundary comparison, restore ordering), which hook-level rendering isolates. No new fetch double: `portal.serve` + `jsonResponse` + `createApplication`, with `trackApplications` from `test-harness.tsx` (whose `renderRoute` mounts the page, which hook-level tests must not do). |
| P6 | The four section components are module-level components in one file | the base's element tree keeps its position; a module-level component identity preserves DOM and focus across re-renders (the oracle's composer-focus case), unlike an inline-defined component. |
| P7 | `scopes.ts` gains two `export` keywords (`ChatScopeState`, `ChatScopeStore`) | the hooks must name the store's record and contract in their signatures; a `ReturnType`-derived alias would publish an implementation helper instead of the owner's type. Type-only, zero runtime surface, line count unchanged (delivered rather than planned; recorded in the evidence record §10). |
| P8 | `ChatTranscript` (pane + history controls + `ChatMessageRow`) is a fifth section | with that DOM inline, `ChatPage` measured cx 31 (> 25) while the contract's clause 1 caps it at 150 L/25; the four named sections keep their listed responsibilities and the fifth is a pure presentational split. The measured `ChatPage` is 127 L/cx 20 (evidence §2). |
| P9 | `useChatScopeState` returns the store's raw `update`/`updateWith` | binding the key inside the hook would make the writers' identities render-dependent and change the retention effect's re-run schedule (base deps `[conversation.data, key, updateWith]`); the raw writers keep every call site's expression identical to base. |

## Evidence plan

All commands foreground and serialized (no measurement harness beside a suite); baseline rows
at `e84973a7` (recorded above), head rows on the final tree.

| # | command | proves |
| --- | --- | --- |
| 1 | `pnpm --filter @symmetry/frontend typecheck` | types hold |
| 2 | `pnpm --filter @symmetry/frontend lint` | `--max-warnings 229`, measured count ≤ base (223) |
| 3 | `pnpm --filter @symmetry/frontend exec vitest run src/features/chat/chat.test.tsx` | 48 cases, unmodified, same names, exit 0 |
| 4 | `pnpm --filter @symmetry/frontend exec vitest run src/features/chat/presentation.test.ts` | 39 cases, exit 0 |
| 5 | `pnpm --filter @symmetry/frontend exec vitest run src/features/chat/chat-hooks.test.tsx` | the new units' state transitions |
| 6 | `pnpm --filter @symmetry/frontend test` | full suite; executed count reconciled to 411 declaration sites + 7 `.each` sites + the new cases |
| 7 | `git diff --check`; a profile-path grep over the diff | hygiene + the DLP rule |
| 8 | eslint `max-lines-per-function`/`complexity` output per unit | `ChatPage` ≤150/≤25, each new unit ≤150/≤25 |
| 9 | mutation flips (below), each shown red then reverted | each new test kills its named mutant |
| 10 | frozen-oracle check (`git status --porcelain` + the oracle run's names/counts) | the oracles and harness are untouched |
| 11 | render-gate spec reference check (`ci.yml` `portal-chat.spec.mjs`, the delivered coordinates `:759` spec list / `:781` `--expect-file`; the contract's pre-cutover `:794`/`:816` drifted) | the second DOM pin is named; the spec runs in CI on this PR's tree |

Mutation list (at minimum; each named by the test that must redden):

| mutant | flip | test |
| --- | --- | --- |
| M1 press capture | retain the page with the settle-time `cursor`/`boundaryId` arguments instead of `press.cursor`/`press.boundaryId` | press-capture test (retained `fetchBefore`/`fetchBeforeId`) |
| M2 admission predicate | `pendingOlderLoadFor` compares cursor bytes only | boundary-comparison test (second press at a different boundary, same bytes) and the re-signed-cursor test |
| M3 ownership predicate | `ownsChainPointer` compares cursor bytes only | refusal-ownership test (re-signed cursor at the same boundary must still own; a moved boundary must not) |
| M4 scroll effect order | swap the owed and recorded `useLayoutEffect` definitions | restore-ordering test (final pane position / settled owed) |
| M5 settle condition | settle the owed restore when the pane did *not* accept it | owed-until-accepted test (restore stays owed while clamped) |
| M6 router re-assertion | the `onRendered` callback drops the owed-mark (re-asserting the recorded offset only) | re-assertion test (recorded offset marked owed when the pane cannot hold it) |
| M7 composer patch | drop the `repositoryId`/`ciId` clearing from the target-project patch | composer section test |

## Control-flow audit (lesson 32)

Per unit: base span -> HEAD equivalent, with each early return, fall-through, error
short-circuit and dependency array listed. The audit table lives in
`docs/evidence/rf-w4-08-chatpage-decomposition-2026-11-22.md`; a `yes` verdict requires the
same condition and the same branch outcome, not merely the same statement text.
All 21 audited rows are preserved; the two deliberate call-site changes (A5's value guards,
D2's optional chain) are recorded there with their equivalence argument.

## Out of scope (with reasons)

Exactly contract clause 4: refusal/outcome classification (W4-10), `useSelectedProject` scope
chain (W5-03), test-harness consolidation/MSW/pool (W4-11), API read/write split (W4-09),
route code splitting (W6-07), UI primitives (W3-12), `presentation.ts`/`scopes.ts` internals.
Additionally: no `router.tsx` change (the one-line import keeps working), no new dependency,
no coverage/threshold change, no `chat.test.tsx`/`presentation.test.ts` edit (frozen; a needed
edit would be a stop-and-report).

## Residual risks / known limits

- **Comment removal vs move proof.** The owner comment policy strips the base's rationale
  blocks, so `git diff --color-moved` will not be clean; the acceptance evidence is the
  per-unit control-flow audit plus the frozen oracle's green run, as the contract's lesson-32
  decision requires.
- **`ChatPage` budget.** Delivered at 127 L / cx 20. The planned fallback was taken for the
  transcript/history DOM: it moved into the `ChatTranscript` section (decision P8) because the
  inline version measured cx 31 > 25; the cap was never raised.
- **Hook-level tests on the real store.** The new tests drive `useChatScopeStore` state
  directly; they pin the units' transitions, and the page-level oracle remains the end-to-end
  authority. jsdom has no layout, so the scroll tests install a controllable `scrollTop`
  model on the pane node, exactly as the frozen oracle does.
