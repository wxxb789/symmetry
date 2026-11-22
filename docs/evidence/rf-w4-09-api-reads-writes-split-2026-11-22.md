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
| `frontend/src/services/api.ts` | edited | 1005 → 1004 | `Reads` `:487-502`, `Writes` `:504-541`, `Api` `:543`; `request()` `:611-660`; 9 read bodies, 20 write bodies |
| `frontend/src/services/api.test.ts` | edited | 556 → 581 | 14 write calls lose their signal; the control table's `send` loses its parameter; 3 new guarding clauses |
| `frontend/src/app/application.ts` | edited | 355 → 355 | 5 write mutations lose `new AbortController().signal`; `:181` (`api.session`, a read) keeps its controller |
| `frontend/src/app/application.test.ts` | edited | 109 → 109 | the `createConnection` call loses its signal |
| `frontend/src/features/connections/connections.tsx` | edited | 504 → 504 | 4 write sites |
| `frontend/src/features/connections/resources.tsx` | edited | 690 → 690 | 5 write sites |
| `frontend/src/features/planning/editors.tsx` | edited | 559 → 559 | 4 write sites |
| `frontend/src/features/projects/projects.tsx` | edited | 143 → 143 | 1 write site |
| `frontend/src/features/projects/work-surface.tsx` | edited | 360 → 360 | 1 write site |
| `frontend/src/features/work/assignment.tsx` | edited | 334 → 334 | 1 write site |
| `docs/plans/2026-11-22-rf-w4-09-api-reads-writes-split-plan.md` | new | 211 | plan with decisions P1-P9 |
| `docs/evidence/rf-w4-09-api-reads-writes-split-2026-11-22.md` | new | — | this record |
| `docs/TODO.md` | edited | 1411 → 1407 | the goal-write cancellation entry moved out (§10) |
| `docs/archive/CLOSED-TODO.md` | edited | 4423 → 4442 | that entry, closed by decision (a) |

Not edited: `frontend/src/features/goals/detail.tsx` (its two constructions are reads),
`attention.tsx`, `services/commands.ts`, `services/realtime.ts`, `features/chat/chat.tsx`,
`frontend/vitest.config.ts`, every DOM suite, `contracts/**`, `browser/**` and every non-frontend
package. `git status --porcelain` lists exactly the twelve modified files above plus the new plan;
the frontend files' sha256 (final tree) are in §10.

## 2. The split (`Api`: 29 methods → 9 + 20)

Base `export interface Api` `:487-539` declared all 29 methods, each with `signal: AbortSignal`.

| half | methods | delivered line |
| --- | --- | --- |
| `Reads` (9, `signal: AbortSignal` required) | `workspace`, `workDetail`, `workTimeline`, `attention`, `goal`, `goalEvents`, `session`, `connections`, `chatConversation` | `:488-495`, `:501` |
| `Writes` (20, no signal parameter) | `createConnection`, `updateConnection`, `deleteConnection`, `checkConnection`, `createProject`, `updateProject`, `createWorkItem`, `updateWorkItem`, `assignWorkItem`, `moveWorkItem`, `createResource`, `updateResource`, `deleteResource`, `syncResource`, `syncProject`, `chatMessage`, `startWork`, `cancelWork`, `retryWork`, `provideInput` | `:505-524`, `:530-533`, `:534-540` |

`chatConversation` and the three doc-commented writes (`assignWorkItem`, `chatMessage` and the
control quartet's own comment) moved with their comments intact. Recomposed as
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
- `run()` is byte-identical to W3-13's shape: `:662`, `runtime.runPromise(Effect.either(effect), signal === undefined ? undefined : { signal })` — zero diff hunks.
- The fetch init's last member is now `...(signal === undefined ? {} : { signal })`, and the
  `Effect.tryPromise` thunk no longer names the interruption parameter (`try: async () => {}`).
  A write's init therefore has exactly `method`, `credentials`, `headers`, `cache`, `redirect`,
  `body` — no `signal` member at all, never `signal: undefined` (D1, P4/P6).

## 4. `AbortController` before/after, per file

`rg -o "new AbortController" <file> | wc -l`, base = `git show HEAD:<file>`:

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

Base → HEAD, write path. The audit is mechanical where it can be: `git diff -U0` over
`frontend/src/services/api.ts` yields 45 hunks, all inside the interface region `:487-543`,
`request()` `:611-660` and `createApi` `:736-908`; `function request`'s three changed lines are
exactly the parameter, the thunk's parameter list, and the init spread.

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
carries the caller's signal itself. Both signals abort on the same caller event; the interruption
path is unchanged, so a read still fails the same way. The two runnable pins are the focused
suite's cancellation cases (`aborts the underlying request when the consuming query is cancelled`,
`aborts the in-flight body download when the consumer cancels during the read`), both green (§6
row 3). This is an argument plus the HEAD-side tests, not an A/B measurement — see §11.

## 6. Gates (all foreground, serialized)

Baseline rows at `d5b14b85` before any edit; head rows on the delivered tree. Host note: every
`pnpm` invocation runs as `mise x pnpm@12.17.0 -- pnpm --trust-lockfile …` (the workspace pins pnpm
12.17.0; the lockfile policy feed is unreachable on this host, the same bypass W4-08/W4-11
recorded). No measurement harness ran beside a suite.

| # | command | base | head |
| --- | --- | --- | --- |
| 1 | `pnpm --filter @symmetry/frontend typecheck` | exit 0 | exit 0 |
| 2 | `pnpm --filter @symmetry/frontend lint` (`--max-warnings 229`) | exit 0, **218** warnings, 0 errors | exit 0, **218** warnings, 0 errors (identical per-rule counts; ceiling not raised) |
| 3 | `… exec vitest run src/services/api.test.ts src/app/application.test.ts` | 2 files / **136** tests, exit 0 | 2 files / **139** tests, exit 0 (`api.test.ts` 46 → 49, `application.test.ts` 90) |
| 4 | `… exec vitest run src/features/chat/chat.test.tsx` | — (unmodified file; 48 cases, the W4-08 record's count) | 48 tests, exit 0 |
| 5 | `… exec vitest run src/features/connections/connections.test.tsx src/features/planning/planning.test.tsx src/features/work/control.test.tsx src/features/work/assignment.test.tsx` | — (unmodified files) | 5 files in total with row 4: **163** tests, exit 0 (connections 57, planning 35, control 13, assignment 10, chat 48) |
| 6 | `pnpm --filter @symmetry/frontend test` | 22 files / **543** tests, exit 0, 33.47 s | 22 files / **546** tests, exit 0, 30.87 s |
| 7 | `git diff --check` | — | exit 0 |
| 8 | DLP greps over added lines only (profile-path pattern, backslash-u) | — | no match (grep exit 1); the new plan file likewise |
| 9 | before/after `AbortController` grep (row set §4) | 24 non-test / 21 write | 3 non-test / 0 write |
| 10 | `knip --no-progress` (report-only, not a contract gate) | exit 1, **2** unused exported types, both `app/portal-double.ts` (measured in a detached `git worktree` at `d5b14b85`, removed afterwards) | exit 1, **4**: the same 2 plus `Reads` (`api.ts:487`) and `Writes` (`api.ts:504`) — the two exports the contract requires |
| 11 | `pnpm --filter @symmetry/frontend build` (the contract's build oracle; CI runs it) | — | exit 0, `✓ built in 1.42s` (the only output note is the pre-existing >500 kB chunk warning, W6-07's subject) |

Suite-count reconciliation: base 543 = 436 line-anchored `it()` sites + 7 `it.each` sites expanding
to 98 cases (the W4-08 record's reconciliation, unchanged here); head 546 = 543 + this slice's 3 new
clauses in `api.test.ts`. The other per-file counts above are from a `--reporter=verbose` run whose
per-file sums equal 546.

Final-tree re-run (after the mutation round and the documentation edits, serialized and verified
against the §10 hashes): `typecheck` exit 0; `lint` exit 0, 218 warnings; the focused pair exit 0 at
139 tests; the full suite exit 0 at 22 files / 546 tests (46.01 s this run against 30.87 s for the
first run — same counts, host-load variance); `git diff --check` exit 0; `sha256sum -c` `OK` for all
ten frontend files. No frontend file changed after the mutation round.

## 7. Mutation flips (each applied alone, run, reverted; hash-verified revert)

| mutant | flip | red (measured) |
| --- | --- | --- |
| M1 write signal re-added (write pin's mutant) | `Writes.createConnection` regains `signal?: AbortSignal` | `typecheck` exit 1: `src/services/api.test.ts(570,5): error TS2578: Unused '@ts-expect-error' directive.` |
| M2 read signal dropped at a call site (contract mutation b) | `application.ts` `sessionOptions`'s `api.session(signal)` → `api.session()` | `typecheck` exit 1: `src/app/application.ts(212,22): error TS2554: Expected 1 arguments, but got 0.` |
| M3 read signal made optional (read pin's mutant) | `Reads.session` drops its `signal` parameter | `typecheck` exit 1: `api.test.ts(577,5): error TS2578: Unused '@ts-expect-error' directive.` **and** `api.ts(765,5): error TS2322` (the implementation still requires the signal) |
| M4 csrf header dropped (contract mutation c) | `request()`'s init drops the `x-csrf-token` header spread | `vitest run src/services/api.test.ts src/features/chat/chat.test.tsx` exit 1: `Tests 5 failed \| 92 passed (97)` — the four control rows and the chat row (`sends the frozen request verbatim with the caller's CSRF token and never retries`); `chat.test.tsx` passed (its write assertions pin path/method/body, not the header) |
| M5a undefined-valued signal member, faithful form (init-shape pin's mutant / contract mutation d) | init always spreads `signal` (`signal,`) | `typecheck` exit 1: `api.ts(627,46): error TS2769` — under `exactOptionalPropertyTypes: true`, `signal: AbortSignal \| undefined` is not assignable to the DOM `RequestInit`'s `signal: AbortSignal \| null`; **and** `vitest run src/services/api.test.ts` exit 1: `Tests 1 failed \| 48 passed (49)` at `expect(Object.hasOwn(init ?? {}, "signal")).toBe(false)` |
| M5b undefined-valued member, compiling form | init spreads `{ signal: null }` when absent | `vitest run src/services/api.test.ts` exit 1: `Tests 1 failed \| 48 passed (49)`, `AssertionError: expected true to be false` at the same own-member assertion |

Revert proof: after the flip round, `sha256sum -c` over all ten delivered frontend files printed `OK`
for each (hash set in §10), and `typecheck` was re-run green (exit 0). M5a/M5b are two forms of the
same mutant: the faithful `signal: undefined` shape is additionally rejected by the type system, so
the compiling `null` form proves the init-shape pin guards the runtime shape independently.

## 8. New test clauses (each names the mutant it kills)

`frontend/src/services/api.test.ts`, `describe("read/write split")`:

1. "sends a write with exactly the boundary's own init members and no signal (mutant: an
   undefined-valued signal member)" — calls `createConnection` and asserts the recorded init's
   own-member absence (`Object.hasOwn(init, "signal") === false`) and its exact sorted member set
   `["body","cache","credentials","headers","method","redirect"]`; red under M5a and M5b.
2. "keeps a write from accepting a caller signal (mutant: a write signal re-added)" — the
   `// @ts-expect-error write paths must not accept a signal` pin on `api.createConnection(draft,
   "csrf", new AbortController().signal)`; red under M1.
3. "keeps a read requiring a caller signal (mutant: a read signal made optional)" — the
   `// @ts-expect-error reads require a signal` pin on `api.session()`; red under M3, and the
   call-site form is red under M2.

Both pins are never-called arrows inside their `it()` bodies: `tsc` checks them, no request is
issued, and each arrow is named by `expect(pin).toBeTypeOf("function")` so `noUnusedLocals` stays
satisfied. The changed call-shape clauses in the two owned test files kept every path/method/body/
header assertion: the control table's rows still assert `path`, `method` and the `x-csrf-token`
header, the chat row still asserts the byte-identical payload and the CSRF header, and the
receipt/refusal assertions are untouched (`git diff` shows only the signal argument removed).

## 9. Comment policy (owner, 2026-11-22)

Nothing was added except the two `@ts-expect-error` directives (machine-consumed) and the tests'
mutant-naming titles. The touched regions' existing why-only notes were left as they are: the
one-signalled-step note inside `request()`, the opaque-`before`-cursor note in the read bodies, the
control quartet's contract note, and the per-file notes in the seven feature files (their diffs
touch single call lines only). No test step labels, no commented-out code, no suppression, no
`eslint-disable`.

## 10. Boundary checks

- **Owned set exactly.** `git status --porcelain`: 12 modified files (the ten owned frontend files,
  `docs/TODO.md`, `docs/archive/CLOSED-TODO.md`) plus the new plan; the evidence file is this one.
  `goals/detail.tsx`, `attention.tsx`, `commands.ts`, `realtime.ts`, `chat.tsx`, `vitest.config.ts`,
  every DOM suite, `contracts/**` and `browser/**` are untouched (the four frontend of these appear
  in no diff, and rows 4/5/6 re-run their suites unmodified).
- **Identities** (sha256, final tree):

| file | sha256 |
| --- | --- |
| `frontend/src/services/api.ts` | `011fcd4b712f6d4d614e2654193b945f86b60c9d4ba77fef287336b4ebd106e2` |
| `frontend/src/services/api.test.ts` | `e4f0b4888183fdbed4c8ce25430c5dbd40a5a1b17205f9f2096f02814a1fe11a` |
| `frontend/src/app/application.ts` | `9ed8c691b80394fe04b81ff30c4905fffe40a19e9c166479741e83f5318ed677` |
| `frontend/src/app/application.test.ts` | `db9a3262235f5ede87c543dcaa0c93aa1a7b4459aeb25671b3bba88bdf08ebda` |
| `frontend/src/features/connections/connections.tsx` | `440f707634f6a86c255f3a85142c61ee2d8177f9651ff68aba52e64de5551eaf` |
| `frontend/src/features/connections/resources.tsx` | `c9b657b46130ffe453171a8206f1990bbe0a5c6a9fb897fb5c9989109278a80d` |
| `frontend/src/features/planning/editors.tsx` | `1bf4205ad503a2fd696c148e7dbdf6f43ffba38cffb88a0d272015dc36eb28f9` |
| `frontend/src/features/projects/projects.tsx` | `d628e83e46828e22fb31f70eb47253c1d35ef7f04edf48f165bce347d208b49f` |
| `frontend/src/features/projects/work-surface.tsx` | `7f2ccf74ccbf8a8dda74f5b7df261bc062dfe1cac710ba3ad8f3c53ecc26907a` |
| `frontend/src/features/work/assignment.tsx` | `d72a69aabcf3a54d601f525b088e0c2ba9ea76c6713b3753e51bc22d42c84423` |

- **LOC delta (report-only, D2).** `git diff --numstat`: `api.ts` +76/−77 (net −1 line),
  `api.test.ts` +44/−19 (net +25), the five-feature/app set +1/−1 each (net 0), `docs/TODO.md` −4,
  `docs/archive/CLOSED-TODO.md` +19. Non-test source LOC: −1; write-site constructions removed: 21;
  `signal` tokens deleted in `api.ts`: 20 interface rows, 20 implementation signatures, 20 `run(…)`
  arguments; added: one optional parameter, one conditional init spread, nine `"server"` read
  arguments.
- **Frozen bytes (D1).** No changed line in the seven app/feature files differs from its base line
  once the `, new AbortController().signal` token is deleted (mechanical check over
  `git diff -U0 -- frontend/src/app/application.ts frontend/src/features`); the header/body/
  credentials/cache/redirect construction in `request()` is byte-identical (§5 R1-R6).
- **DLP.** No user-profile host path and no backslash-u in any added line or new file (greps exit 1);
  no host path appears in this record or the plan.
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
  future change that makes reads pass a different signal object would still pass those tests.
- **A write's body read is no longer cancellable at all** — it never was in practice (the base
  controller was fresh and never aborted), and option (a) makes that explicit. A bounded write
  policy would be a new increment with its own acceptance.
- **`Reads`/`Writes` add two knip unused-export findings** (row 10). knip is not a contract gate;
  the exports are contract-mandated, and nothing consumes them but `Api` today.
- **The chat DOM suite does not pin the write's CSRF header** (M4 row): its write assertion covers
  the path, method and body, so the header pin lives in `api.test.ts`. Not changed here — that file
  is W4-11's and out of this slice's owned set.
- **The browser render-gate specs are CI-only** (Chromium compose stack) and were not run locally;
  the DOM suites in §6 rows 4-5 are the local write-path proxies.
- **Lint ceiling:** the count stays at 218 against the pinned 229; if a later slice adds warnings the
  headroom is 11.
