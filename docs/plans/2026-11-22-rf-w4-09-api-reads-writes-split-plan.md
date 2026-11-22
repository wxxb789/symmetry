# rf-w4-09-api-reads-writes-split plan — `Reads`/`Writes`: a signal only where one exists

Contract: `goal-rf-w4-09-api-reads-writes-split.md` (GDD, 2026-11-21/22), binding with its
conductor decisions D1 (frozen bytes: a write's `fetch` init carries exactly today's members
minus `signal`), D2 (LOC report-only), option (a) (no internal bounded write timeout), the
lesson-32 control-flow audit, the E8 runnable-oracle list, the pin-hygiene rules and the owner
comment policy (2026-11-22). Base revision for every coordinate: `d5b14b85`
(`refactor(frontend): decompose ChatPage (#232)`, this worktree's HEAD, tree clean).

Shape references: `docs/plans/2026-11-22-rf-w4-08-chatpage-decomposition-plan.md` and its
evidence record (same-stage GDD shape, lesson-32 audit table, mutation table).

## Dispatch gates (checked at dispatch, base `d5b14b85`)

| gate | state at dispatch |
| --- | --- |
| W3-12 (`#190`) UI primitives/resource-dialog collapse merged | yes (`c0784952`); the five shared files are at their post-W3-12 line state |
| W3-13 (`#204`) one HTTP boundary for command posts merged | yes (`f1fe1e59`); `request()` takes no signal parameter, `run()`'s second parameter is already optional, `goalCommand` calls `request(fetcher, path, init, goalReceipt, "mutation")` |
| W2-15 (`#164`) merged | yes; `connections.tsx`/`resources.tsx`/`assignment.tsx` are at the rebased line state |
| W4-11 (`#220`) test harness merged | yes (`e84973a7`); `app/portal-double.ts`, `app/test-harness.tsx`, both test files' current shape |
| W4-08 (`#232`) ChatPage decomposition merged | yes (`d5b14b85`, this HEAD) |
| owned files untouched by open work | `git status --porcelain` empty; no open PR touches the owned set |

## Measured basis (base revision `d5b14b85`)

| artifact | measurement |
| --- | --- |
| `frontend/src/services/api.ts` | 1,005 L; `export interface Api` `:487-539` = **29 methods**: 9 reads (`:488-495`, `:521`) each with `signal: AbortSignal`, 20 writes (`:496-515`, `:527-538`) each with `signal: AbortSignal` |
| `request()` | `:607-655`: no signal parameter; the fetch init's `signal` comes from `Effect.tryPromise`'s interruption callback |
| `run()` | `:657`: `signal?: AbortSignal` (already optional, W3-13) |
| `createApi()` | `:730-912`: 9 read bodies call `run(request(...), signal)`; 20 write bodies call `run(request(...), signal)` |
| non-test `new AbortController()` | **24 sites / 8 files**: 21 write sites — `application.ts` `:289,:295,:301,:312,:354`, `connections.tsx` `:106,:107,:219,:232`, `resources.tsx` `:457,:458,:459,:624,:642`, `editors.tsx` `:72,:73,:247,:248`, `projects.tsx` `:97`, `work-surface.tsx` `:223`, `assignment.tsx` `:231` — and 3 read sites that stay: `application.ts:181` (`api.session`), `goals/detail.tsx:71,:98` (`api.goal`) |
| test-side `new AbortController()` | 33 sites: `api.test.ts` 32, `application.test.ts` 1; no DOM suite constructs one |
| write call sites outside the owned set | none: `rg "\.(createConnection|…|provideInput)\("` over non-test `src` matches only the 21 owned sites |
| `goals/detail.tsx` | calls `api.goal` (read) and `postGoalCommand` (no signal); not edited |
| focused oracles | `vitest run src/services/api.test.ts src/app/application.test.ts` → 2 files / **136** tests, exit 0 |
| full suite | 22 files / **543** tests, exit 0, 33.47 s |
| lint ratchet | `eslint . --max-warnings 229`; measured at base: exit 0, **218** warnings, 0 errors (69 no-unnecessary-condition, 40 restrict-template-expressions, 27 no-invalid-void-type, 15 max-lines-per-function, 14 no-non-null-assertion, 11 complexity, 11 no-deprecated, 7 prefer-nullish-coalescing, 6 prefer-find, 6 no-dynamic-delete, 4 no-empty-function, 3 prefer-optional-chain, 2 no-unnecessary-type-conversion, 2 dot-notation, one wrapped message line) |
| typecheck | `pnpm --filter @symmetry/frontend typecheck` exit 0 |

Host note: every `pnpm` invocation runs as `mise x pnpm@12.17.0 -- pnpm --trust-lockfile ...`
(the workspace pins pnpm 12.17.0; the lockfile policy feed is unreachable on this host, the same
bypass W4-08/W4-11 recorded). `pnpm install --frozen-lockfile` from the content-addressable
store was run once at dispatch; the tree stayed clean.

## Deliverable shape

| file | change | detail |
| --- | --- | --- |
| `frontend/src/services/api.ts` | edited | `Api` becomes `Reads & Writes`; `request()` gains a trailing optional `signal` and omits the fetch-init member when absent; 20 write bodies lose their `signal` parameter and the second `run()` argument; 9 read bodies pass the caller's signal to `request()` |
| `frontend/src/app/application.ts` | edited | 5 write mutations drop `new AbortController().signal`; `:181`'s read signal stays |
| `frontend/src/features/connections/connections.tsx` | edited | 4 write sites |
| `frontend/src/features/connections/resources.tsx` | edited | 5 write sites |
| `frontend/src/features/planning/editors.tsx` | edited | 4 write sites |
| `frontend/src/features/projects/projects.tsx` | edited | 1 write site |
| `frontend/src/features/projects/work-surface.tsx` | edited | 1 write site |
| `frontend/src/features/work/assignment.tsx` | edited | 1 write site |
| `frontend/src/services/api.test.ts` | edited | write calls lose their signal; the control-command table's `send` loses its signal parameter; 4 new guarding clauses (two init-shape pins — a bodyed write's values/members and a bodyless write's members — two `@ts-expect-error` pins and one mapped-type assertion over every `Writes` member) |
| `frontend/src/app/application.test.ts` | edited | 1 write call loses its signal |
| `docs/plans/2026-11-22-rf-w4-09-api-reads-writes-split-plan.md` | new | this plan |
| `docs/evidence/rf-w4-09-api-reads-writes-split-2026-11-22.md` | new | evidence record (clause 2's judging artifact) |
| `docs/TODO.md` + `docs/archive/CLOSED-TODO.md` | edited | the `api.ts` goal-write cancellation entry is closed by decision (a) |

Not edited: `goals/detail.tsx`, `attention.tsx`, `commands.ts`, `realtime.ts`, `chat.tsx`,
`vitest.config.ts`, the DOM suites, `contracts/**`, every non-frontend package.

## API shape

```ts
export interface Reads {
  workspace: (projectId: string | null, signal: AbortSignal) => Promise<Workspace>;
  workDetail: (workItemId: string, signal: AbortSignal) => Promise<WorkDetail>;
  workTimeline: (workItemId: string, before: string | null, signal: AbortSignal) => Promise<WorkTimeline>;
  attention: (projectId: string | null, cursor: string | null, signal: AbortSignal) => Promise<Attention>;
  goal: (goalId: string, signal: AbortSignal) => Promise<GoalProjection>;
  goalEvents: (goalId: string, after: number, signal: AbortSignal) => Promise<GoalEvents>;
  session: (signal: AbortSignal) => Promise<Session>;
  connections: (signal: AbortSignal) => Promise<ConnectionsReadModel>;
  chatConversation: (query: ChatConversationQuery, signal: AbortSignal) => Promise<ChatConversation>;
}

export interface Writes {
  createConnection: (draft: ConnectionDraft, csrf: string) => Promise<ConnectionReceipt>;
  // …the other 19 write methods, each (…, csrf: string) with no signal parameter
}

export type Api = Reads & Writes;
```

```ts
function request<A>(
  fetcher: typeof fetch,
  path: string,
  init: RequestInit,
  decode: (body: Uint8Array, status: number) => Effect.Effect<A, unknown>,
  surface: RefusalSurface = "server",
  signal?: AbortSignal,
): Effect.Effect<A, ApiError>;

// read body (9 sites):  run(request(fetcher, path, init, decode, "server", signal), signal)
// write body (20 sites): run(request(fetcher, path, init, decode))
// W3-13 boundary, unchanged: request(fetcher, path, init, goalReceipt, "mutation")
```

The fetch init is built with a conditional spread, so a write's init has no `signal` member at
all (never `signal: undefined`):

```ts
...(init.body === undefined ? {} : { body: JSON.stringify(init.body) }),
...(signal === undefined ? {} : { signal }),
```

## Exact edge-case semantics the contract demands

1. **Frozen bytes (D1).** `method`, `credentials: "same-origin"`, the `Accept` /
   `content-type` / `x-csrf-token` headers, `cache: "no-store"`, `redirect: "manual"` and the
   JSON body bytes are byte-identical; the only init delta is the absent `signal` member on
   writes. The read init gains no new member either: its `signal` value is the caller's signal
   where the base's was the interruption signal of the same run.
2. **One request owner.** `request()` stays the single HTTP boundary; reads keep both arms
   (`request(…, signal)` and `run(…, signal)`), writes keep `run(request(…))`. The Goal
   command/creation callers (W3-13) are untouched, because `signal` is appended after
   `surface`.
3. **No hidden write policy (option (a)).** No `AbortController`, no timeout, no deadline in a
   write path; a write is exactly as cancellable as the base's never-aborted fresh controller
   made it (i.e. not at all), now stated by the type.
4. **Retry/identity semantics unchanged.** Mutation retries stay off; no call site's argument
   order changes other than dropping the signal; the chat payload identity, `action_id` and
   `creation_id` carriers are untouched.
5. **Type surface is the enforcement.** The two pins, the mapped assertion over every `Writes`
   member and the two init-shape clauses are the runnable guards; the call-shape edits in the two
   owned test files keep every path/method/body/header assertion byte-identical.
6. **Comments.** Owner policy applies to every touched hunk: the surrounding 1-2 line
   why-only notes already in the touched regions stay, and no comment is added except the two
   `@ts-expect-error` directives the contract requires (machine-consumed pragmas) and each
   test's mutant-naming title.

## Decisions (and the alternatives)

| # | decision | rationale |
| --- | --- | --- |
| P1 | `Api` is recomposed as `export type Api = Reads & Writes` (not `interface Api extends Reads, Writes`) | the contract names the shape `Reads & Writes`; the intersection is one line, keeps every `application.api.<method>` call site's name, and no module declaration-merges `Api` (`rg "extends Api"` finds none) |
| P2 | `signal` is the trailing parameter *after* `surface` in `request()` | W3-13's merged `goalCommand` passes `"mutation"` as the fifth positional argument and must stay byte-identical; with `signal` fifth that call would break. Reads therefore pass `"server"` explicitly, which is also self-documenting |
| P3 | No bounded write timeout/abort policy | conductor option (a): a hidden write deadline is new production behaviour with no accepted requirement and would break D1; the type surface is the whole change |
| P4 | The two `@ts-expect-error` pins are never-called arrows inside `it()` bodies; the all-writes assertion is a type-argument `expectTypeOf` call | the guards must be *type-checked* but must not issue requests: an uncalled arrow is checked by `tsc` and costs nothing at runtime, and each arrow is named by `expect(arrow).toBeTypeOf("function")` so `noUnusedLocals` stays satisfied; `expectTypeOf` is a runtime no-op |
| P5 | Each guarded pin carries its killed mutant in the test title | the conductor's mutation-proof decision: the write pin and its mapped all-writes assertion (a write signal re-added on the pinned call or on any other member), the read pin (read signal made optional), the init-shape clauses (an `undefined`-valued `signal` member, a changed init value, a `body` member on a bodyless write) |
| P6 | The init-shape pins assert own-member absence, the exact member set *and* D1's values | D1 says the init carries exactly today's members minus `signal`: `Object.hasOwn(init, "signal") === false` is the named absence assertion; the sorted `Reflect.ownKeys` list pins "exactly" including non-enumerable members; the full-object equality pins the values (`credentials: "same-origin"`, `cache: "no-store"`, `redirect: "manual"`, the `Accept`/`content-type`/`x-csrf-token` headers, the body bytes); the second clause pins a bodyless write's five members (no `body`/`content-type`) |
| P7 | `run()` is left as W3-13 wrote it (`signal?: AbortSignal`, `signal === undefined ? undefined : { signal }`) | already the contract's shape; editing it would be a no-op diff |
| P8 | No doc amendment in `docs/design/frontend.md` | its cancellation sentence scopes the AbortSignal forwarding to "the query adapter" (reads), which is preserved; the split states the write half without contradicting it |
| P9 | The `docs/TODO.md` goal-write cancellation entry is closed, not scheduled | the entry says the split "owns adding one"; option (a) refuses a write signal, so the residual becomes a stated design property. The closure text records the decision and the archive keeps the trace |

## Evidence plan

All commands foreground and serialized (no measurement harness beside a suite); baseline rows
at `d5b14b85` (recorded above), head rows on the final tree.

| # | command | proves |
| --- | --- | --- |
| 1 | `pnpm --filter @symmetry/frontend typecheck` | the split types hold; the two pins are consumed errors |
| 2 | `pnpm --filter @symmetry/frontend lint` | `--max-warnings 229`, measured count ≤ base (218) |
| 3 | `pnpm --filter @symmetry/frontend exec vitest run src/services/api.test.ts src/app/application.test.ts` | the focused oracles plus the new pins |
| 4 | `… exec vitest run src/features/chat/chat.test.tsx` | the DOM suite that records and asserts the chat POST write |
| 5 | `… exec vitest run src/features/connections/connections.test.tsx src/features/planning/planning.test.tsx src/features/work/control.test.tsx src/features/work/assignment.test.tsx` | the remaining DOM write-path oracles (E8 list) |
| 6 | `pnpm --filter @symmetry/frontend test` | full suite; head count = base 543 + the new cases |
| 7 | `git diff --check`; DLP greps over the diff | hygiene + the DLP rule |
| 8 | before/after `new AbortController` grep per file | the 24 → 3 non-test delta with the 3 read sites named |
| 9 | base-vs-HEAD write-path audit (request status branches, `run()`, every write body's method/URL/body) | lesson 32: no decision point moved out of the write path |
| 10 | mutation flips (below), each shown red then reverted | each pin kills its named mutant; the header mutant reddens its assertions |
| 11 | `git status --porcelain` + `git diff --stat` | the edit set is exactly the owned files; frozen oracles untouched |
| 12 | `knip --no-progress` (report-only) | the two contract-required exported types' effect on the unused-export report, recorded |

Mutation list (each applied alone, run, reverted):

| mutant | flip | expected red |
| --- | --- | --- |
| M1 write signal re-added (write pin's mutant) | `Writes.createConnection` regains `signal?: AbortSignal` | typecheck: unused `@ts-expect-error` directive at the write pin |
| M1b write signal re-added on another member (mapped assertion's mutant) | `Writes.updateConnection` regains `signal?: AbortSignal` | typecheck: the `expectTypeOf<SignalTakers<Writes>>().toBeNever()` assertion stops compiling |
| M2 read signal dropped at a call site | `application.ts` `sessionOptions`'s `api.session(signal)` → `api.session()` | typecheck: missing argument at the call (+ the unused destructured `signal`) |
| M3 read signal made optional | `Reads.session` and its implementation both take `signal?: AbortSignal` | typecheck: unused `@ts-expect-error` directive at the read pin |
| M4 csrf header dropped | `request()`'s init drops the `x-csrf-token` header | `api.test.ts`: the four control rows, the chat row and both init-shape clauses |
| M5 undefined-valued signal member | the init always spreads `signal` | both init-shape clauses (`Object.hasOwn(init, "signal")` true) |
| M6 init value changed | `request()`'s `credentials: "same-origin"` → `"include"` | the bodyed init-shape clause's value equality |
| M7 body member added to a bodyless write | `checkConnection`'s init gains `body: {}` | the bodyless init-shape clause's member set and value equality |

## Control-flow audit (lesson 32)

Recorded in the evidence record: `request()`'s status branches (401/403/302/303/opaqueredirect,
non-GET 403's session-protection message, `parseRefusal` fallback), `run()`'s execution, and all
20 write bodies' method/URL/body/csrf construction, base → HEAD with a preserved yes/no per row;
plus the read `init.signal` source change as one audited equivalence row.

## Out of scope (with reasons)

Exactly contract clause 4: the command-POST boundary (W3-13), refusal classification (W4-10),
ChatPage decomposition (W4-08), the test harness/MSW work (W4-11), the goal-settle poll and
`detail.tsx` (W5-04), route code splitting (W6-07), `realtime.ts`, the browser specs. Additionally:
no write timeout/abort policy (P3), no `Api` consumer renaming, no new dependency, no lint
ceiling change, no edit to `goals/detail.tsx`'s two read signals or `application.ts:181`'s
recorded ceremonial read signal, no DOM-suite edit (frozen oracles).

## Residual risks / known limits

- **Read/init equivalence rests on one argument, not on a test.** The base's read init signal
  was `Effect.tryPromise`'s interruption signal; HEAD's is the caller's signal, which is the
  value `run()` passes to `runPromise`. No new test can compare the two revisions, so the
  equivalence row in the evidence record is the artifact; the two cancellation tests
  (`aborts the underlying request when the consuming query is cancelled`, `aborts the in-flight
  body download…`) are the runnable pins on the HEAD side.
- **A write's body read is no longer cancellable at all** — it never was in practice (the base
  controller was fresh and never aborted); a future bounded write policy is a new increment.
- **`Writes`/`Reads` are exported but only `Api` consumes them**; knip may report them
  (report-only row 12).
- The browser render-gate specs are CI-only and were not run locally; the DOM suites in the E8
  list are the local write-path proxies.
