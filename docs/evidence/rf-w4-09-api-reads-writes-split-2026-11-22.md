# rf-w4-09-api-reads-writes-split — evidence record (2026-11-22)

Subject: the portal API surface is split into a reads half and a writes half. `export interface Reads`
carries the 9 read methods, each requiring `signal: AbortSignal`; `export interface Writes` carries
the 20 write methods, none of which can take a signal; `export type Api = Reads & Writes` keeps every
`application.api.<method>` call site's name. The shared `request()` boundary gained a trailing
optional `signal`, so a write's `fetch` init carries exactly the base's members minus `signal` and a
read still forwards its caller's signal. Contract: `goal-rf-w4-09-api-reads-writes-split.md` (GDD,
2026-11-21/22, binding conductor decisions D1/D2, option (a) no bounded write policy, the lesson-32
control-flow audit, the E8 runnable-oracle list, the pin-hygiene rules and the owner comment policy
of 2026-11-22). Base revision `d5b14b85` (`refactor(frontend): decompose ChatPage (#232)`, this
worktree's dispatch HEAD, all base coordinates below; tree clean). Plan:
`docs/plans/2026-11-22-rf-w4-09-api-reads-writes-split-plan.md`. Judging artifact (contract clause 2,
"answer.md"): this record plus the implement-stage report (interface diff, per-file
`AbortController` grep, the mutation runs, the focused + full vitest output, the LOC delta).

## 1. Deliverable shape (files, counts)

| file | status | lines (base → head) | note |
| --- | --- | ---: | --- |
| `frontend/src/services/api.ts` | edited | 1005 → 1002 | `Reads` `:487-502`, `Writes` `:504-541`, `Api` `:543`; `request()` `:611-658`; 9 read bodies, 20 write bodies; −2 from the apply stage's note deletion (§12) |
| `frontend/src/services/api.test.ts` | edited | 556 → 610 | 14 write calls lose their signal; the control table's `send` loses its parameter; 4 guarding clauses (a bodyed and a bodyless init-shape clause, the write pin widened by a mapped all-writes assertion, the read pin) |
| `frontend/src/app/application.ts` | edited | 355 → 355 | 5 write mutations lose `new AbortController().signal`; `:181` (`api.session`, a read) keeps its controller |
| `frontend/src/app/application.test.ts` | edited | 109 → 105 | the `createConnection` call loses its signal; −4 from the apply stage's note deletion (§12) |
| `frontend/src/features/connections/connections.tsx` | edited | 504 → 496 | 4 write sites; −8 from the apply stage's note deletion (§12) |
| `frontend/src/features/connections/resources.tsx` | edited | 690 → 688 | 5 write sites; −2 from the apply stage's note deletion (§12) |
| `frontend/src/features/planning/editors.tsx` | edited | 559 → 559 | 4 write sites |
| `frontend/src/features/projects/projects.tsx` | edited | 143 → 143 | 1 write site |
| `frontend/src/features/projects/work-surface.tsx` | edited | 360 → 360 | 1 write site |
| `frontend/src/features/work/assignment.tsx` | edited | 334 → 334 | 1 write site |
| `docs/plans/2026-11-22-rf-w4-09-api-reads-writes-split-plan.md` | new | 214 | plan with decisions P1-P9 |
| `docs/evidence/rf-w4-09-api-reads-writes-split-2026-11-22.md` | new | — | this record |
| `docs/TODO.md` | edited | 1411 → 1429 | the goal-write cancellation entry moved out (§10); this slice's own delta is −4 lines, and the merge with main's later edits (`9e360586`) leaves the delivered file at 1429 L |
| `docs/archive/CLOSED-TODO.md` | edited | 4423 → 4442 | that entry, closed by decision (a) |

Not edited: `frontend/src/features/goals/detail.tsx` (its two constructions are reads),
`attention.tsx`, `services/commands.ts`, `services/realtime.ts`, `features/chat/chat.tsx`,
`frontend/vitest.config.ts`, every DOM suite, `contracts/**`, `browser/**` and every non-frontend
package. The delivery is committed as `d2d7b780` — `refactor(frontend): split api reads and writes`,
body `GDD apply stage output (rf-w4-09-api-reads-writes-split)`, parent `a159f0c0` (the branch merge
of main's `9e360586` into the branch-side `54c7a3d1`); it carries the apply stage's record refresh
and the four comment deletions (§12). The fix stage's edits — `frontend/src/services/api.test.ts`,
this record and the plan — are the working tree's only uncommitted modifications (§13), and the
frontend files' sha256 (final tree) are in §10.

## 2. The split (`Api`: 29 methods → 9 + 20)

Base `export interface Api` `:487-539` declared all 29 methods, each with `signal: AbortSignal`.

| half | methods | delivered line |
| --- | --- | --- |
| `Reads` (9, `signal: AbortSignal` required) | `workspace`, `workDetail`, `workTimeline`, `attention`, `goal`, `goalEvents`, `session`, `connections`, `chatConversation` | `:488-495`, `:501` |
| `Writes` (20, no signal parameter) | `createConnection`, `updateConnection`, `deleteConnection`, `checkConnection`, `createProject`, `updateProject`, `createWorkItem`, `updateWorkItem`, `assignWorkItem`, `moveWorkItem`, `createResource`, `updateResource`, `deleteResource`, `syncResource`, `syncProject`, `chatMessage`, `startWork`, `cancelWork`, `retryWork`, `provideInput` | `:505-524`, `:530-533`, `:534-540` |

`chatConversation` and the two doc-commented writes (`assignWorkItem`, `chatMessage`) moved with
their comments intact; the control quartet's own note lives inside `createApi` (base `:848-854`)
and did not move. Recomposed as
`export type Api = Reads & Writes;` (`:543`) — decision P1: the contract's own notation, one line,
and no module declaration-merges `Api` (`rg "extends Api"` finds none).

## 3. `request()` / `run()` shape and the D1 init delta

```ts
function request<A>(
  fetcher: typeof fetch, path: string, init: RequestInit,
  decode: (body: Uint8Array, status: number) => Effect.Effect<A, unknown>,
  surface: RefusalSurface = "server",
  signal?: AbortSignal,
): Effect.Effect<A, ApiError>
```

- `signal` is the trailing parameter *after* `surface`, so W3-13's merged `goalCommand` call
  `request(fetcher, path, { method: "POST", body: envelope, csrf: csrfToken }, goalReceipt, "mutation")`
  is byte-identical (decision P2); reads pass `"server"` explicitly, writes call `run(request(…))`.
- Reads keep both arms: `run(request(…, decode, "server", signal), signal)` (9 sites).
- `run()` is byte-identical to W3-13's shape: `:660`, `runtime.runPromise(Effect.either(effect), signal === undefined ? undefined : { signal })` — zero diff hunks.
- The fetch init's last member is now `...(signal === undefined ? {} : { signal })`, and the
  `Effect.tryPromise` thunk no longer names the interruption parameter (`try: async () => {}`).
  A write's init is therefore its base init minus `signal`: `method`, `credentials`, `headers`,
  `cache` and `redirect` on every write, plus `body`/`content-type` on the 15 bodyed writes — the
  five bodyless writes (`deleteConnection`, `checkConnection`, `deleteResource`, `syncResource`,
  `syncProject`) carry neither, as in base; never `signal: undefined` (D1, P4/P6).

## 4. `AbortController` before/after, per file

`rg -o "new AbortController" <file> | wc -l`, base = `git show d5b14b85:<file>`:

| file | base | head | classification |
| --- | ---: | ---: | --- |
| `frontend/src/services/api.ts` | 0 | 0 | boundary: no construction in either revision |
| `frontend/src/app/application.ts` | 6 | 1 | 5 writes removed; `:181` `api.session` **retained (read)** |
| `frontend/src/features/connections/connections.tsx` | 4 | 0 | writes |
| `frontend/src/features/connections/resources.tsx` | 5 | 0 | writes |
| `frontend/src/features/goals/detail.tsx` | 2 | 2 | **retained reads** `:71`, `:98` `api.goal` (W5-04's target; not owned) |
| `frontend/src/features/planning/editors.tsx` | 4 | 0 | writes |
| `frontend/src/features/projects/projects.tsx` | 1 | 0 | write |
| `frontend/src/features/projects/work-surface.tsx` | 1 | 0 | write |
| `frontend/src/features/work/assignment.tsx` | 1 | 0 | write |
| **non-test total** | **24** | **3** | 21 write sites → 0; the 3 retained read sites are `application.ts:181`, `detail.tsx:71`, `detail.tsx:98` |
| `frontend/src/services/api.test.ts` | 32 | 19 | write signals removed, read signals kept |
| `frontend/src/app/application.test.ts` | 1 | 0 | write signal removed |
| **test total** | **33** | **19** | |

The plan's `−24 sites` is superseded per D2 (report-only): 21 write constructions deleted, 3
read-side ones retained.

## 5. Control-flow audit (lesson 32)

Base → HEAD, write path. The audit is mechanical where it can be:
`git diff -U0 d5b14b85 -- frontend/src/services/api.ts | grep -c '^@@'` yields **48** hunks on the
delivered tree (47 at the implementation revision; the apply stage's two-line note deletion, §12,
is the `+622,0` hunk). Every hunk lies in one of three regions — the interface split `:487-543`,
`request()` `:611-658` and `createApi`'s bodies `:733-905` — and no hunk touches the W3-13
boundary: `parseRefusal` `:594`, `goalReceipt` `:679`, `goalCommand` `:935`, `postGoalCommand`
`:946` and `postGoalCreation` `:957` are untouched. `function request`'s code changes are exactly
the parameter, the thunk's parameter list and the init spread, plus that note deletion.

| # | decision point | base | head | preserved |
| --- | --- | --- | --- | --- |
| R1 | expiry classification | `response.type === "opaqueredirect" \|\| status === 302 \|\| 303 \|\| 401` | no hunk — byte-identical | yes |
| R2 | body-read rule | `reads = !expired && (response.ok \|\| init.method !== "GET")` | no hunk | yes |
| R3 | transport catch | `catch: unreachable` on the one `tryPromise` (fetch + body read in one step) | no hunk | yes |
| R4 | expired answer | `if (expired) return Effect.fail(statusError(401))` | no hunk | yes |
| R5 | refusal precedence | non-GET: `parseRefusal(status, body, surface)` then `mutationStatusError`; GET: `statusError(status)` | no hunk | yes |
| R6 | decode failure | `decode(body, status).pipe(Effect.mapError(() => unreadableBody(response.status)))` | no hunk | yes |
| R7 | `parseRefusal`, `RefusalBody`, `MutationRefused`, `ServerRefusal`, `goalCommand`/`postGoalCommand`/`postGoalCreation`, `goalReceipt` | W3-13's merged boundary | zero hunks in all of it | yes |
| R8 | `run()` | `runPromise(Effect.either(effect), signal === undefined ? undefined : { signal })` | no hunk | yes |
| R9 | 20 write bodies | each `run(request(fetcher, path, init, decode))` | normalized diff empty (§5.1) | yes |
| R10 | read `init.signal` source | `Effect.tryPromise`'s interruption signal (the same `AbortSignal` `run()` hands `runPromise`) | the caller's `signal` passed explicitly | yes — see §5.2 |

### 5.1 The 20 write bodies (mechanical)

Method: extract `createApi`'s region from both revisions, delete every signal token
(`, new AbortController().signal`, `, signal: AbortSignal`, `, signal`, a bare `signal,`), diff.
Result: the only remaining differences are the 9 read call sites' new `, "server"` surface argument
(the reads' own shape, §3) and sed's blank-line artifacts — **no write body differs** in method,
path expression, body construction, `csrf` carrier or decoder. Per body:

| write | method | URL | body | csrf |
| --- | --- | --- | --- | --- |
| `createConnection` | POST | `/portal/api/connections` | `draft` | yes |
| `updateConnection` | PATCH | `/portal/api/connections/{id}` | `update` | yes |
| `deleteConnection` | DELETE | `/portal/api/connections/{id}?version=` | — | yes |
| `checkConnection` | POST | `/portal/api/connections/{id}/check` | — | yes |
| `createProject` | POST | `/portal/api/projects` | `draft` | yes |
| `updateProject` | PATCH | `/portal/api/projects/{id}` | `changes` | yes |
| `createWorkItem` | POST | `/portal/api/projects/{id}/work-items` | `draft` | yes |
| `updateWorkItem` | PATCH | `/portal/api/work-items/{id}` | `changes` | yes |
| `assignWorkItem` | PATCH | `/portal/api/work-items/{id}` | version-fenced assignment | yes |
| `moveWorkItem` | PATCH | `/portal/api/work-items/{id}/move` | `{version,status,before_id}` | yes |
| `createResource` | POST | `/portal/api/projects/{id}/resources` | `draft` | yes |
| `updateResource` | PATCH | `/portal/api/resources/{id}` | `update` | yes |
| `deleteResource` | DELETE | `/portal/api/resources/{id}?version=` | — | yes |
| `syncResource` | POST | `/portal/api/resources/{id}/sync` | — | yes |
| `syncProject` | POST | `/portal/api/projects/{id}/sync` | — | yes |
| `chatMessage` | POST | `/portal/api/chat/messages` | frozen payload | yes |
| `startWork` | POST | `/portal/api/work-items/{id}/run` | `{action_id}` | yes |
| `cancelWork` | POST | `/portal/api/work-items/{id}/cancel` | `{action_id,generation}` | yes |
| `retryWork` | POST | `/portal/api/work-items/{id}/retry` | `{action_id,generation}` | yes |
| `provideInput` | POST | `/portal/api/work-items/{id}/input` | `{input,action_id,waiting_transition_id}` | yes |

### 5.2 The read `init.signal` source change (equivalence row)

Base: a read's fetch init carried `Effect.tryPromise`'s interruption signal, which `run()`'s
`runPromise(effect, { signal })` aborts when the caller's signal aborts. HEAD: the fetch init
carries the caller's signal itself, and the thunk no longer receives the step's interruption signal
— this increment deliberately removed that step-signal-to-fetch link as part of the same change
that re-sourced the init. A caller cancel still aborts the transfer, now through the caller's
signal, and both signals abort on the same caller event, so a read still fails the same way; a
fiber interruption that is not the caller's signal no longer aborts the transport, which today
reaches no different state (the `runtime` at `:385` is created once and never disposed, and `run()`
supplies the only signal). The two runnable pins are the focused suite's cancellation cases
(`aborts the underlying request when the consuming query is cancelled`,
`aborts the in-flight body download when the consumer cancels during the read`), both green (§6
row 3). Three deltas that sentence does not cover (no current call site constructs an
already-aborted controller, so none is reachable from production today):

- **Already-aborted caller signal.** HEAD hands it to `fetch` as `init.signal`, so the fetch
  rejects with the signal's reason instead of dispatching; base handed `fetch` the runtime's step
  signal, which was not aborted, so the transfer was dispatched and then aborted. The ordering is
  mechanical: `unsafeRunPromiseExit` starts the fiber synchronously (`effect@3.22.4`
  `dist/esm/internal/runtime.js:210`; `unsafeFork` → `FiberRuntime.start`,
  `dist/esm/internal/core.js:33-66`) before its `options.signal.aborted` check (`runtime.js:215`),
  and the fiber's synchronous start runs the thunk's `fetch` call before either point.
- **Signal identity and reason.** The fetch now receives the caller's own `AbortSignal` object
  (same identity, same `reason`); base's init carried the runtime's per-run interruption signal.
  An injected fetcher can observe the difference; the two cancellation tests observe only
  `init.signal.aborted`, which both objects satisfy.
- **Post-abort rejection precedence.** After a caller abort, the fetch's own `AbortError` (from the
  now-inherited signal) and the runtime's fiber interruption both occur; `run()` surfaces the
  fiber's exit, and no test pins the rejecting error's identity — the two cancellation cases
  assert only that the promise rejects.

This is an argument plus the HEAD-side tests, not an A/B measurement — see §11.

## 6. Gates (all foreground, serialized)

Baseline rows at `d5b14b85` before any edit; head rows on the final tree (implementation revision →
apply stage §12 → fix stage §13), re-run at every stage. Host note: every
`pnpm` invocation runs as `mise x pnpm@12.17.0 -- pnpm --trust-lockfile …` (the workspace pins pnpm
12.17.0; the lockfile policy feed is unreachable on this host, the same bypass W4-08/W4-11
recorded). No measurement harness ran beside a suite.

| # | command | base | head |
| --- | --- | --- | --- |
| 1 | `pnpm --filter @symmetry/frontend typecheck` | exit 0 | exit 0 |
| 2 | `pnpm --filter @symmetry/frontend lint` (`--max-warnings 229`) | exit 0, **218** warnings, 0 errors | exit 0, **218** warnings, 0 errors (the apply stage's per-rule distribution was identical; the fix stage re-measured the total; ceiling not raised) |
| 3 | `… exec vitest run src/services/api.test.ts src/app/application.test.ts` | 2 files / **136** tests, exit 0 | 2 files / **140** tests, exit 0 (`api.test.ts` 46 → 50, `application.test.ts` 90) |
| 4 | `… exec vitest run src/features/chat/chat.test.tsx` | — (unmodified file; 48 cases, the W4-08 record's count) | 48 tests, exit 0 |
| 5 | `… exec vitest run src/features/connections/connections.test.tsx src/features/planning/planning.test.tsx src/features/work/control.test.tsx src/features/work/assignment.test.tsx` | — (unmodified files) | 5 files in total with row 4: **163** tests, exit 0 (connections 57, planning 35, control 13, assignment 10, chat 48) |
| 6 | `pnpm --filter @symmetry/frontend test` | 22 files / **543** tests, exit 0, 33.47 s | 22 files / **547** tests, exit 0 (three green runs, 43.19–62.44 s; two load-flake reds, §13) |
| 7 | `git diff --check` | — | exit 0 |
| 8 | DLP greps over added lines only (profile-path pattern, backslash-u) | — | no match (grep exit 1; re-run at the fix stage over the final worktree's 877 added lines, §13) |
| 9 | before/after `AbortController` grep (row set §4) | 24 non-test / 21 write | 3 non-test / 0 write |
| 10 | `knip --no-progress` (report-only, not a contract gate) | exit 1, **2** unused exported types, both `app/portal-double.ts` (measured in a detached `git worktree` at `d5b14b85`, removed afterwards) | exit 1, **4**: the same 2 plus `Reads` (`api.ts:487`) and `Writes` (`api.ts:504`) — the two exports the contract requires |
| 11 | `pnpm --filter @symmetry/frontend build` (the contract's build oracle; CI runs it) | — | exit 0, `✓ built in 1.42s` (the only output note is the pre-existing >500 kB chunk warning, W6-07's subject) |

Suite-count reconciliation: base 543 = 445 line-anchored `it()` sites + 7 `it.each` sites expanding
to 98 cases (`git grep -c -E '^\s*it\(' d5b14b85 -- frontend/src` sums 445; 445+98=543); head 547 =
449 plain sites (445 + this slice's 4 guarding clauses) + the same 98 cases. The other per-file
counts above are from a `--reporter=verbose` run whose per-file sums equal 547.

Final-tree re-run at the fix stage (§13, after the mutation round and the documentation edits,
serialized and verified against the §10 hashes): `typecheck` exit 0; `lint` exit 0, 218 warnings;
the focused pair exit 0 at 2 files / 140 tests; `chat.test.tsx` exit 0 at 48; the four DOM suites
exit 0 at 4 files / 115 (163 with chat); the full suite exit 0 at 22 files / 547 tests in three of
five runs (43.19 s, 45.43 s, 62.44 s; the two red runs and the identified load flake are in §13);
`pnpm build` exit 0 (`✓ built in 2.14s`); `git diff --check` exit 0; `sha256sum -c` `OK` for all
ten frontend files. No frontend file changed after the mutation round. The apply stage re-ran rows
1-7 and 11 on the comment-deleted tree (§12) at 139 focused / 546 full; the counts above are the
fix-stage tree's (one added init-shape clause and the widened write pin).

## 7. Mutation flips (each applied alone, run, reverted; hash-verified revert)

Re-measured at the fix stage on the final tree (§13); M1b, M6 and M7 are the new/changed clauses'
mutants. Each flip's revert was hash-verified against §10.

| mutant | flip | red (measured) |
| --- | --- | --- |
| M1 write signal re-added, pinned call (write pin's mutant) | `Writes.createConnection` regains `signal?: AbortSignal` | `typecheck` exit 1: `api.test.ts(599,5): error TS2578: Unused '@ts-expect-error' directive.` **and** `api.test.ts(598,42): error TS2349: This expression is not callable.` (the mapped assertion) |
| M1b write signal re-added on an unpinned member (mapped assertion's mutant) | `Writes.updateConnection` regains `signal?: AbortSignal` | `typecheck` exit 1: `api.test.ts(598,42): error TS2349: This expression is not callable.` — the single error; previously no gate saw this member |
| M2 read signal dropped at a call site (contract mutation b) | `application.ts` `sessionOptions`'s `api.session(signal)` → `api.session()` (destructure kept) | `typecheck` exit 1: `application.ts(212,15): error TS6133: 'signal' is declared but its value is never read.` and `application.ts(212,32): error TS2554: Expected 1 arguments, but got 0.` |
| M3 read signal made optional, interface and implementation (read pin's mutant) | `Reads.session` and its implementation both take `signal?: AbortSignal` | `typecheck` exit 1: `api.test.ts(606,5): error TS2578: Unused '@ts-expect-error' directive.` — the only error |
| M4 csrf header dropped (contract mutation c) | `request()`'s init drops the `x-csrf-token` header spread | `vitest run src/services/api.test.ts src/features/chat/chat.test.tsx` exit 1: `Tests 7 failed \| 91 passed (98)` — the four control rows, the chat row (`sends the frozen request verbatim with the caller's CSRF token and never retries`) and both init-shape clauses; `chat.test.tsx` passed (48; its write assertions pin path/method/body, not the header) |
| M5a undefined-valued signal member, faithful form (init-shape pin's mutant / contract mutation d) | init always spreads `signal` (`signal,`) | `typecheck` exit 1: `api.ts(625,46): error TS2769` — under `exactOptionalPropertyTypes: true`, `signal: AbortSignal \| undefined` is not assignable to the DOM `RequestInit`'s `signal: AbortSignal \| null`; **and** `vitest run src/services/api.test.ts` exit 1: `Tests 2 failed \| 48 passed (50)`, `AssertionError: expected true to be false` at both init-shape clauses' own-member assertion |
| M5b undefined-valued member, compiling form | init spreads `{ signal: null }` when absent | `typecheck` exit 0; `vitest run src/services/api.test.ts` exit 1: `Tests 2 failed \| 48 passed (50)`, `AssertionError: expected true to be false` at the same two assertions |
| M6 init value changed (both init-shape clauses' mutant) | `request()`'s `credentials: "same-origin"` → `"include"` | `vitest run src/services/api.test.ts` exit 1: `Tests 2 failed \| 48 passed (50)`, both clauses' value equality (`expected { method: 'POST', … } to deeply equal { method: 'POST', … }`) |
| M7 body member added to a bodyless write (bodyless clause's mutant) | `checkConnection`'s init gains `body: {}` | `vitest run src/services/api.test.ts` exit 1: `Tests 1 failed \| 49 passed (50)`, the bodyless clause's member set (`expected [ Array(6) ] to deeply equal [ 'cache', 'credentials', … ]`) |

Revert proof: after the flip round, `sha256sum` over the three mutated files printed the §10 hashes
(`api.ts` `2156ab72…`, `application.ts` `9ed8c691…`, `api.test.ts` `30be5cae…`), and `typecheck`
was re-run green (exit 0) on the restored tree. M5a/M5b are two forms of the same mutant: the
faithful `signal: undefined` shape is additionally rejected by the type system, so the compiling
`null` form proves the init-shape clauses guard the runtime shape independently.

## 8. New and changed test clauses (each names the mutant it kills)

`frontend/src/services/api.test.ts`, `describe("read/write split")`:

1. "sends a bodyed write with the boundary's own init values and no signal (mutants: a changed
   credentials value; an undefined-valued signal member)" — calls `createConnection` and asserts
   the recorded init's own-member absence (`Object.hasOwn(init, "signal") === false`), its exact
   member set (`Reflect.ownKeys(init).map(String).sort()` equals
   `["body","cache","credentials","headers","method","redirect"]`, so non-enumerable members
   cannot hide) and the full value equality (`method`, `credentials: "same-origin"`,
   `cache: "no-store"`, `redirect: "manual"`, the `Accept`/`content-type`/`x-csrf-token` headers,
   the JSON body bytes); red under M6, M5a and M5b.
2. "keeps a bodyless write to the five init members it carries in base (mutant: a body member
   added to a bodyless write)" — the same assertions for `checkConnection`: five members
   (`["cache","credentials","headers","method","redirect"]`), no `signal`, values equal; red
   under M7 (and under M6/M5a/M5b, which change every write's init).
3. "keeps every write from accepting a caller signal (mutant: a write signal re-added)" — two
   guards: the mapped assertion `expectTypeOf<SignalTakers<Writes>>().toBeNever()`, which stops
   compiling when a `signal?` is re-added on any of the 20 members (red under M1b, and under M1
   too), and the `// @ts-expect-error write paths must not accept a signal` pin on
   `api.createConnection(draft, "csrf", new AbortController().signal)` (red under M1).
4. "keeps a read requiring a caller signal (mutant: a read signal made optional)" — the
   `// @ts-expect-error reads require a signal` pin on `api.session()`; red under M3, and the
   call-site form is red under M2.

Both `@ts-expect-error` pins are never-called arrows inside their `it()` bodies: `tsc` checks
them, no request is issued, and each arrow is named by `expect(pin).toBeTypeOf("function")` so
`noUnusedLocals` stays satisfied; the mapped assertion is a type-argument `expectTypeOf` call (a
runtime no-op). The changed call-shape clauses in the two owned test files kept every
path/method/body/header assertion: the control table's rows still assert `path`, `method` and the
`x-csrf-token` header, the chat row still asserts the byte-identical payload and the CSRF header,
and the receipt/refusal assertions are untouched (`git diff` shows only the signal argument
removed).

## 9. Comment policy (owner, 2026-11-22)

Implementation revision: nothing was added except the two `@ts-expect-error` directives
(machine-consumed) and the tests' mutant-naming titles; the touched regions' existing why-only
notes were left in place (the opaque-`before`-cursor note in the read bodies, the control quartet's
contract note, and the per-file notes in the seven feature files, whose diffs touch single call
lines). No test step labels, no commented-out code, no suppression, no `eslint-disable`.

Apply stage (2026-11-22, §12): four comment blocks were deleted under the accepted simplify
findings (S1-S4) — `request()`'s stale one-signalled-step note (this increment removed the step
signal it named), `application.test.ts`'s false handler-precedence note (`application.ts:153-156`
documents the cache-level `onError` as running for every mutation, including the ones that supply
their own, and `@tanstack/query-core@5.107.1` chains rather than replaces), `connections.tsx`'s
eight-line rationale duplicating the note beside `noticeFor`, and `resources.tsx`'s two-line
receipt note. 16 comment lines in four files; no test, type, assertion or runtime line was touched.

## 10. Boundary checks

- **Owned set exactly.** The delivery's whole diff — `git diff --name-status 9e360586` (main's tip →
  the worktree: the committed branch tip `d2d7b780` plus the fix stage's uncommitted edits, §13) —
  is exactly the ten owned frontend files, `docs/TODO.md`, `docs/archive/CLOSED-TODO.md`, the plan
  and this record; `git status --porcelain` lists the fix stage's three edited files
  (`frontend/src/services/api.test.ts`, this record, the plan), and nothing else.
  `goals/detail.tsx`, `attention.tsx`, `commands.ts`, `realtime.ts`, `chat.tsx`, `vitest.config.ts`,
  every DOM suite, `contracts/**` and `browser/**` are untouched (each appears in no diff of either
  revision, and rows 4/5/6 re-run those suites as byte-identical files).
- **Identities** (sha256, final worktree; the apply stage's four deletions are included — §12 —
  and the fix stage's test edits — §13):

| file | sha256 |
| --- | --- |
| `frontend/src/services/api.ts` | `2156ab727bacdfb151b3397f8cdac391ef33a12e787ce5747f6d5ee89caecf80` |
| `frontend/src/services/api.test.ts` | `30be5cae560a38ebdc7b642e900585806da100032af585d11e914daead41d4b1` |
| `frontend/src/app/application.ts` | `9ed8c691b80394fe04b81ff30c4905fffe40a19e9c166479741e83f5318ed677` |
| `frontend/src/app/application.test.ts` | `d93a2210805c331de7b806696a2e8506d905f69b094a965ce83ee77cfe23b883` |
| `frontend/src/features/connections/connections.tsx` | `f448401f92791b701f1258016a3549b68ba1d0eb3772075f28cffaafdcf86c66` |
| `frontend/src/features/connections/resources.tsx` | `86c496356d8ea32de16d7e12f74f8878d467287e6618feaefd9926d73f071ff0` |
| `frontend/src/features/planning/editors.tsx` | `1bf4205ad503a2fd696c148e7dbdf6f43ffba38cffb88a0d272015dc36eb28f9` |
| `frontend/src/features/projects/projects.tsx` | `d628e83e46828e22fb31f70eb47253c1d35ef7f04edf48f165bce347d208b49f` |
| `frontend/src/features/projects/work-surface.tsx` | `7f2ccf74ccbf8a8dda74f5b7df261bc062dfe1cac710ba3ad8f3c53ecc26907a` |
| `frontend/src/features/work/assignment.tsx` | `d72a69aabcf3a54d601f525b088e0c2ba9ea76c6713b3753e51bc22d42c84423` |

- **LOC delta (report-only, D2).** `git diff --numstat d5b14b85 -- frontend/src docs/TODO.md
  docs/archive/CLOSED-TODO.md` over the final worktree (the unrestricted command emits 40 rows —
  the merge brings main's daemon/control/contracts/docs changes along; the pathspec yields exactly
  the 12 rows below): `api.ts` +76/−79 (net −3), `api.test.ts` +75/−21 (net +54),
  `application.ts` +5/−5, `application.test.ts` +1/−5 (net −4), `connections.tsx` +4/−12 (net −8),
  `resources.tsx` +5/−7 (net −2), `editors.tsx` +4/−4, `projects.tsx` +1/−1, `work-surface.tsx`
  +1/−1, `assignment.tsx` +1/−1 (net 0 each), `docs/TODO.md` +22/−4 (this slice −4; the merge with
  main's edits +22), `docs/archive/CLOSED-TODO.md` +19/−0. Net, delivered: non-test source −13
  (implementation revision −1; −12 is the apply stage's comment deletions — 16 comment lines, 12
  in non-test source and 4 in the test file), test files +50; write-site constructions removed: 21;
  `signal` tokens deleted in `api.ts`: 20 interface rows, 20 implementation signatures, 20 `run(…)`
  arguments; added to the production files: one optional parameter, one conditional init spread,
  nine `"server"` read arguments.
- **Frozen bytes (D1).** Every changed *call line* in the seven app/feature files differs from its
  base line only by the deleted `, new AbortController().signal` token (mechanical check over
  `git diff -U0 d5b14b85 HEAD -- frontend/src/app/application.ts frontend/src/features`: 21 call
  lines replaced, none gaining a token), plus §12's 10 comment deletions in two of those files
  (`connections.tsx` 8, `resources.tsx` 2); the header/body/credentials/cache/redirect construction
  in `request()` is byte-identical (§5 R1-R6).
- **DLP.** No user-directory host path, no drive-letter path, no backslash-u and no
  path-separated profile name in the final worktree's added lines (three case-insensitive greps
  over `git diff 9e360586 | grep '^+'`, all exit 1 with no match, §13); no host path appears in
  this record or the plan.
- **Docs reconciliation.** `docs/design/frontend.md`'s cancellation sentence scopes AbortSignal
  forwarding to the query adapter (reads) and stays true; no design document is made stale by this
  change, so none was amended (decision P8).
- **TODO closure.** `docs/TODO.md`'s `api.ts` goal-write cancellation entry (the comment-policy
  sweep's debt record) moved to `docs/archive/CLOSED-TODO.md` under its preserved section, closed by
  conductor decision option (a): the split states that a write keeps no signal instead of pretending
  to add one. The intro line naming the move and branch was added; the archive's other entries are
  untouched.
- **E8 respect.** The three write-free suites (`saved-views.test.ts`, `filters.test.ts`,
  `work/detail.test.tsx`) were not treated as write-path oracles; they ran only inside the full
  suite.

## 11. Residual risks / known limits

- **The read init-signal equivalence is an argument, not a measurement** (§5.2): the two revisions
  cannot be compared in one run, and the two HEAD-side cancellation tests are the runnable pins. A
  future change that makes reads pass a different signal object would still pass those tests, and
  the three deltas §5.2 records (already-aborted dispatch, signal identity, post-abort rejection
  precedence) have no test of their own.
- **A write's body read is no longer cancellable at all** — it never was in practice (the base
  controller was fresh and never aborted), and option (a) makes that explicit. A bounded write
  policy would be a new increment with its own acceptance.
- **`Reads`/`Writes` add two knip unused-export findings** (row 10). knip is not a contract gate;
  the exports are contract-mandated, and nothing consumes them but `Api` today.
- **The write-side type pin is interface-level** (STD-A01's implementation form): the mapped
  assertion rejects a `signal?` on any `Writes` member, but a write *implementation* that takes a
  trailing optional `signal` and forwards it while its `Writes` member declares none stays
  assignable and cannot be reached through the typed `Api` surface (`createApi` returns `Api`), so
  it is recorded here rather than pinned; M1/M1b cover the interface (§7).
- **The chat DOM suite does not pin the write's CSRF header** (M4 row): its write assertion covers
  the path, method and body, so the header pin lives in `api.test.ts`. Not changed here — that file
  is W4-11's and out of this slice's owned set.
- **The browser render-gate specs are CI-only** (Chromium compose stack) and were not run locally;
  the DOM suites in §6 rows 4-5 are the local write-path proxies.
- **Lint ceiling:** the count stays at 218 against the pinned 229; if a later slice adds warnings the
  headroom is 11.

## 12. Apply stage (2026-11-22, accepted simplify findings S1-S4, PD1-PD2)

Comment-only deletions on top of the implementation revision; no test, type, assertion or runtime
line changed. Pre-apply coordinates and what was deleted:

| # | file:lines (pre-apply) | deleted | why it was safe |
| --- | --- | --- | --- |
| S1 | `frontend/src/services/api.ts:623-624` | `request()`'s one-signalled-step note (2 lines) | the mechanism it named — the step's interruption signal feeding the fetch — was removed by this increment's own `init.signal` re-sourcing (R10/§5.2); a caller cancel now travels through the caller's signal, and `api.test.ts:153`/`:196` (green, row 3) pin the cancellation |
| S2 | `frontend/src/app/application.test.ts:83-86` | the handler-precedence note (4 lines) | its premise was false: `application.ts:153-156` documents and implements the cache-level `onError` for every mutation, including the ones that supply their own; `defaultOptions.mutations` carries no handler to be replaced; and `@tanstack/query-core@5.107.1` (`build/modern/mutation.js` `:205`/`:210`) awaits the cache-level `onError` and then the mutation's own. The test's title and assertions carry the rule |
| S3 | `frontend/src/features/connections/connections.tsx:210-217` | the remount/rationale block (8 lines; the finding cites `:210-216`) | it duplicated the note beside `noticeFor` (`:403-409` on the delivered tree), and the invariant is pinned by `connections.test.tsx:522` and `:554`, both green (row 5) |
| S4 | `frontend/src/features/connections/resources.tsx:453-454` | the receipt note (2 lines) | the first clause restated what the types already say (`mutationFn …: Promise<unknown>` `:453`, `onSuccess: (_receipt, submission)` `:464`); the residue was design rationale, outside the comment policy's keep classes |

16 comment lines in four files; `git diff --numstat` deltas: `api.ts` −2, `application.test.ts` −4,
`connections.tsx` −8, `resources.tsx` −2 (§1 and §10 are refreshed in place). PD1: §5's hunk count
is corrected and now carries its counting command (48 on the delivered tree; 47 at the
implementation revision). PD2: §5.2 no longer claims the whole interruption path is unchanged — it
states the deliberately removed step-signal-to-fetch link and the consequence that today reaches no
different state. The plan's "Exact edge-case semantics" item 6 records the implementation decision
that the touched regions' 1-2 line why-only notes stay; the owner comment policy's keep list
governs what stays, and the four blocks deleted here are recorded in this section and §9.

Gates re-run on the frozen apply-stage frontend tree (serialized; the §10 hashes were re-verified
after the run): `typecheck` exit 0; `lint` exit 0 at the same 218 warnings / 0 errors (ceiling 229
untouched); the focused pair exit 0 at 2 files / 139 tests; `chat.test.tsx` exit 0 at 48 tests; the
four remaining DOM write-path suites exit 0 at 4 files / 115 tests (163 with chat); the full suite
exit 0 at 22 files / 546 tests (37.77 s); `pnpm build` exit 0 (`✓ built in 1.31s`); `git diff
--check` exit 0; `sha256sum -c` `OK` for all ten frontend files; §4's `AbortController` grep is
re-measured identically (3 non-test / 19 test), and the DLP greps over the then-worktree's added
lines (`git diff 9e360586 | grep '^+'`; the committed range `9e360586..HEAD` is 737 insertions by
`git diff --shortstat 9e360586..HEAD`) have no match (profile-path pattern, backslash-u and the
profile name; grep exit 1).
Mutation rows M3 and M5a were re-measured on the
delivered tree (§7). The review round then rejected the M1/M2/M4/M5b carry-forward ("byte-identical
files" stopped being a proof once the apply stage touched `api.ts`), and the fix stage re-ran every
flip — M1, M1b, M2, M3, M4, M5a, M5b, M6, M7 — on the final tree (§7, §13); the DLP greps were
re-run over the final worktree's 877 added lines (§10, no match). No measurement harness exists for
this goal; the counts above are its measurements.

## 13. Fix stage (2026-11-22, review findings R1)

The review of `9e360586..d2d7b780` raised 16 findings (2 P3 test hardening, 3 P2 and 11 P3
document defects); this stage resolves all of them — two by test change, fourteen by document
change. The working tree carries the three uncommitted files named in §1; no production file
changed.

- **STD-A01 (test):** the write-side pin now covers every `Writes` member. `SignalTakers<Writes>`
  (a mapped type extracting any `AbortSignal`-compatible parameter from `ParametersOf<M[K]>`) is
  asserted `never` via `expectTypeOf`; a `signal?` re-added on any of the 20 members stops the
  call compiling (mutant M1b red, §7), where the same mutant was previously green on every gate.
- **STD-A02 (test):** the init-shape clause now pins D1's values (`credentials`, `cache`,
  `redirect`, the three header values, the body bytes), uses `Reflect.ownKeys` for exact
  membership (non-enumerable-proof), and a second clause pins a bodyless write's five members;
  mutants M6 and M7 are red (§7).
- **Document corrections** (the 14 SPEC findings): §1 commit state (`d2d7b780`; the fix stage's
  three uncommitted files), §1/§6 counts (445 base sites, 449 head sites, 543/547 suites, 140
  focused), §2 (two doc-commented writes; the quartet note stays in `createApi`), §3 (per-write
  base-minus-`signal` phrasing, five bodyless writes), §4 (`git show d5b14b85:` base command),
  §5.2 (the three read-signal deltas), §6 (reconciliation and the fix-stage re-run + flake note),
  §7 (M1/M3 flip forms and transcripts aligned; every flip re-measured at this stage), §8 (the
  four clauses and their mutants), §10 (owned-path numstat pathspec; `api.test.ts` hash; DLP count
  737 → 877 with the worktree command), §11 (the deltas' no-test status; the interface-level pin
  limit), §12 (the mutation carry-forward gap superseded by the §7 re-run), and the plan (M1's
  member, M4's chat clause, the `:730-912`/`:527-538` coordinates, P5/P6 and the mutation list
  aligned with the delivered clauses).

Gates re-run on the final tree (serialized, foreground): `typecheck` exit 0; `lint` exit 0 at 218
warnings / 0 errors; the focused pair exit 0 at 2 files / 140 tests; `chat.test.tsx` exit 0 at 48;
the four DOM suites exit 0 at 4 files / 115 (163 with chat); the full suite exit 0 at 22 files /
547 tests in three green runs (43.19 s, 45.43 s, 62.44 s); `pnpm build` exit 0 (`✓ built in
2.14s`); `git diff --check` exit 0; the DLP greps over the final worktree's 877 added lines
(`git diff 9e360586 | grep '^+'`) exit 1 (no match); `sha256sum -c` `OK` for all ten frontend
files. The mutation round ran nine flips (M1, M1b, M2, M3, M4, M5a, M5b, M6, M7), each reverted
and hash-verified (§7).

Full-suite flake observed at this stage: two of the five full-suite runs failed under host load —
one at 83.36 s whose failure section was truncated by the pipeline's own `tail` (unidentified),
and one at 82.48 s whose JSON report names `src/features/goals/goal-attention.test.tsx`'s "lists
Goal attention and the project blocked work together" (`findByRole("link", …)` not found within
the default timeout). That file is untouched by this slice and the suite passes in isolation
(29/29, exit 0); the three passing runs took 43.19-62.44 s against the red runs' 82-83 s.
