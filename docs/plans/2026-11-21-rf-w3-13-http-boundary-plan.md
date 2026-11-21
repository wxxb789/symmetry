# rf-w3-13-http-boundary plan — one HTTP boundary for command POSTs

Contract: `goal-rf-w3-13-http-boundary.md` (GDD, 2026-11-21), binding with its conductor
decision (2026-11-21): one refusal parser replaces `parseRefusal` + `CommandErrorBody` +
`refusal`; one fatal body decode serves reads and commands; the single authorized message
change is the **undescribed command 403** (D1); D3 = `NonEmptyString` (read strictness kept),
D4 = `fields` read-only, D5 fatal decode on every path, D9 = identical `Network` message in
both paths; the −120…−180 dLOC band is report-only (D2). Base for every coordinate below:
`77861e0010ead8525b508f37bb20cb89bb91422a` (this worktree's HEAD = the W3-12 tip, whose
ancestor `79548368` contains PR #164 `rf-w2-15-served-value-module`; both dispatch gates are
in history and the tree is clean), so the contract's D6 rebase is a no-op and every line
reference is re-derived at it. Shape references:
`docs/plans/2026-11-21-rf-w3-12-ui-primitives-plan.md` (#164/W3-12, same wave-3 dispatch
shape) and `docs/plans/2026-11-21-rf-w1-10-frontend-eslint-plan.md` (#146, which reformatted
`api.ts` and owns the `--max-warnings 229` ratchet this slice must not raise).

## Measured basis (base revision)

`frontend/src/services/api.ts` is 1,044 L at the base. The two hand-written boundaries:

| region | lines | behaviour |
| --- | --- | --- |
| `ServerError` schema | `:290-299` | `error.code`/`error.message` `Schema.NonEmptyString`, optional `fields` (own default `null`) |
| `decodeBody` | `:531-535` | `TextDecoder("utf-8", { fatal: true })` → `UnexpectedResponse` (`status: null`) |
| `statusError` | `:537-555` | 401 `Unauthenticated` "Your session has ended. Sign in to continue."; 403 `Forbidden` "You do not have access to this workspace."; 409 `Conflict`; 422 `Validation`; 405/501 `Unsupported`; 404 non-transient `UnexpectedResponse`; 5xx/408/429 transient |
| `mutationStatusError` | `:561-566` | non-GET 403 keeps "This change was refused. If you signed in elsewhere, refresh the page and try again." |
| `parseRefusal` | `:568-582` | its own fatal `TextDecoder` → `ServerRefusal` (`code`, `message`, `fields`) |
| `request` | `:584-632` | 302/303/`opaqueredirect` → `statusError(401)`; a failed GET reads no body; a failed non-GET parses the refusal first; fetch/body-read catch `:614` is `Network` "Cannot reach the server. Check your connection and try again." |
| `run` | `:634-639` | `runPromise(Effect.either(effect), { signal })`, throws the left side |
| `CommandErrorBody` | `:906-915` | `code`/`message` `Schema.String`, `current_version`, `current_revision`, `allowed_actions`, `details` |
| `GoalReceiptBody` | `:921-925` | `goal_id`/`mutation_id` `NonEmptyString` |
| `postGoalCommand` | `:934-942` | delegates to `postPortalCommand` with the goal commands path |
| `postPortalCommand` | `:944-992` | plain-async POST: own 302/303/401 handling, **non-fatal** `await response.text()` `:975`, 200-without-receipt → non-transient `UnexpectedResponse` |
| `refusal` | `:994-1006` | `CommandErrorBody` first → `MutationRefused` with exactly the sent command members; else `statusError` (undescribed 403 → "You do not have access to this workspace.") |

`MutationRefused` stays in `frontend/src/services/commands.ts:92-110` (`status`, `code`,
`details`, `stale` getter over `409 && (stale || stale_revision)`); `newMutationId` `:111` and
`commandEnvelope` `:115` are not touched. `commands.ts` does not import `api.ts`, and `api.ts`
already imports `MutationRefused` from `./commands` (`:17`), so no type has to move.

Census at the base (whole frontend tree, `git grep`): `postPortalCommand` 2 in `api.ts`
(`:940`, `:944`) + 2 in `attention.tsx` (`:8`, `:178`) + 2 in `api.test.ts` (`:3`, `:213`);
`CommandErrorBody` 2 in `api.ts` (`:906`, `:995`); `function refusal` 1 (`:994`);
`function parseRefusal` 1 (`:568`); `TextDecoder` 2 (`:533`, `:570`); `response.text()` 1 code
use (`:975`) + 1 comment (`:530`); `decodeBody` 22 call sites + its own definition (`:531`);
no `AbortController` in
`api.ts`. Callers: `goals/detail.tsx:147`, `:198`, `:205` (`postGoalCommand`);
`attention.tsx:178` (`postPortalCommand` → `/portal/api/projects/{id}/goals`). The contract's
`attention.tsx:177`/`detail.tsx:146,197,204` are the pre-W3-12 coordinates (W3-12 added one
import line to each file).

LOC baseline of the owned set: `api.ts` 1,044, `api.test.ts` 410, `commands.ts` 129,
`detail.tsx` 762, `attention.tsx` 201, `goal-attention.test.tsx` 1,342.

Oracle pins that must not move (all re-read at the base):

- `api.test.ts` 30 cases. `:43` invalid-UTF-8 read rejection (the shape the new write case
  mirrors); the "goal command boundary" block `:204-244` — JSON 401 as expiry (and the direct
  `postPortalCommand` creation call at `:213`), redirect as expiry `:217-222`, 200 without a
  receipt `:225-229`, receipt members `:230-234`, guidance map `:236-243`; control-command
  described refusal `:270-275`; undescribed control 403 `:277-285`; chat replay refusal `:401-408`.
  At the base `api.test.ts` constructs no `MutationRefused` and pins no `details` (verified: no
  match); the final tree adds one `details` assertion at `:306`, a second witness for
  mutation (b) beside `goal-attention.test.tsx:322`.
- `goal-attention.test.tsx` 29 cases: stale conflict witnesses `:285-328` (`/version 9,
  revision 3/` at `:322`, rendered from `refusal.details.current_version/current_revision`),
  goal-creation refusal `:156-187` (described 422 → "goal contract is invalid
  (invalid_contract)"), command 401 expiry `:447`, served refusal panel `:592`.
- `application.test.ts`: the 401/302/303 lifecycle `it.each` (reads), command 401 expiry with a
  local `onError`, and `readRetry` classification.

## Deliverable shape

1. **`frontend/src/services/api.ts`** — one boundary for reads and command POSTs:
   - `ServerError` (`:290-299`) becomes `RefusalBody`: the union schema of the members the
     server sends and both surfaces consume (`code`, `message` `NonEmptyString`; `fields` with
     the read path's `null` default; `current_version`, `current_revision`, `allowed_actions`,
     `details` optional). `CommandErrorBody` (`:906-915`) is deleted; `GoalReceiptBody` moves
     up beside the other receipt schemas (verbatim comment and members).
   - `decodeBodyText(body)` is the single `TextDecoder("utf-8", { fatal: true })` site;
     `decodeBody` wraps it as the Effect the OK path uses, and `unreadableBody(status)` is the
     one construction of the unreadable-body `UnexpectedResponse` (message byte-identical to
     today's two sites).
   - `parseRefusal(status, body, surface)` is the one parser: fatal decode + `RefusalBody`;
     `surface === "server"` → `ServerRefusal` (with `fields`), otherwise `MutationRefused`
     whose `details` carry exactly the old command members (D4: a sent `fields` never reaches
     `details`). The old local `refusal(status, document)` is deleted.
   - `request()` gains a trailing `surface: RefusalSurface = "server"` parameter; the failed
     non-GET branch calls the one parser with it. Every existing read/control/chat call site is
     unchanged.
   - `run(effect, signal?)` — optional signal; the two Goal command functions call it without
     one (writes have no caller cancellation today; W4-09 owns the split; no
     `AbortController` is minted).
   - `goalCommand(fetcher, csrfToken, path, envelope)` — the one write body: POST through
     `request()` with `goalReceipt` (fatal `decodeBody` + `GoalReceiptBody`) and
     `surface: "mutation"`.
   - `postGoalCommand(fetcher, csrfToken, goalId, envelope)` — signature kept, now the goal
     commands path over `goalCommand`.
   - `postGoalCreation(fetcher, csrfToken, projectId, envelope)` — new named function for
     `POST /portal/api/projects/{projectId}/goals`; `postPortalCommand` is deleted.
2. **`frontend/src/features/attention/attention.tsx`** — import line and `:178` call move to
   `postGoalCreation`; the create `mutation_id` stays minted inside `mutationFn` (`:140-142`),
   nothing else changes.
3. **`frontend/src/services/api.test.ts`** — the direct `postPortalCommand` creation call
   (`:213`) moves to `postGoalCreation`; new cases: undescribed command 403 for both command
   functions (D1), an empty-code command refusal degrading to the status mapping (D3), a
   described command refusal's `details` (including a sent `fields` that must not appear), and
   invalid UTF-8 inside an otherwise valid write receipt (D5).
4. **`docs/plans/2026-11-21-rf-w3-13-http-boundary-plan.md`** — this file, with the gate and
   mutation results appended at implement time.

`frontend/src/features/goals/detail.tsx` is in the owned set but needs no edit: the
`postGoalCommand` signature and its `MutationRefused` branching are kept, so W2-15's
`../../services/served` import line stays untouched.

## API shape

```ts
// frontend/src/services/api.ts (module-local unless exported today)
const RefusalBody = Schema.Struct({ error: Schema.Struct({
  code: Schema.NonEmptyString, message: Schema.NonEmptyString,
  fields: /* NullOr(Record<string, string[]>), default null */,
  current_version: Schema.optional(Schema.Number), current_revision: Schema.optional(Schema.Number),
  allowed_actions: Schema.optional(Schema.Array(Schema.String)), details: Schema.optional(Schema.Unknown),
}) });

const decodeBodyText: (body: Uint8Array) => string;                    // fatal UTF-8, the one decoder
const unreadableBody: (status: number | null) => UnexpectedResponse;   // the one construction
const decodeBody: (body: Uint8Array) => Effect.Effect<string, UnexpectedResponse>;

type RefusalSurface = "server" | "mutation";                           // module-local
function parseRefusal(status: number, body: Uint8Array, surface: RefusalSurface): ApiError | null;
  // "server"   -> ServerRefusal { status, code, message, fields }
  // "mutation" -> MutationRefused(status, code, message, {current_version, current_revision, allowed_actions, details} minus undefined)

function request<A>(fetcher: typeof fetch, path: string, init: RequestInit,
  decode: (body: Uint8Array, status: number) => Effect.Effect<A, unknown>,
  surface: RefusalSurface = "server"): Effect.Effect<A, ApiError>;

function run<A>(effect: Effect.Effect<A, ApiError>, signal?: AbortSignal): Promise<A>;

export async function postGoalCommand(fetcher: typeof fetch, csrfToken: string, goalId: string, envelope: unknown): Promise<unknown>;
export async function postGoalCreation(fetcher: typeof fetch, csrfToken: string, projectId: string, envelope: unknown): Promise<unknown>;
```

`ApiError` grows to `ReadError | ServerRefusal | MutationRefused` (the write Effect can now
fail with either refusal type). Nothing is re-exported; `readRetry`, `errorMessage`,
`refusalNotice`, `ownValue` keep their signatures and bodies.

## Exact edge-case semantics the contract demands

Each row is the contract's divergence table; "pin" names the case that reddens if the row
moves.

| # | case | behaviour after the change | pin |
| --- | --- | --- | --- |
| D1 | undescribed 403 on a command POST (body that is not the refusal envelope, e.g. `{}`) | `mutationStatusError(403)` → `Forbidden` "This change was refused. If you signed in elsewhere, refresh the page and try again." for **both** command functions (was: "You do not have access to this workspace." on the goal path). Authorized change. | new `api.test.ts` case (both functions); mutation (e) |
| D2 | described refusal | one parser: reads/control → `ServerRefusal{status, code, message, fields}`; Goal commands → `MutationRefused(status, code, message, details)`, same members as today | existing control `:270`, chat `:401`, creation `goal-attention.test.tsx:156`; new `details` case |
| D3 | empty `code`/`message` | `NonEmptyString` rejects → `null` → status mapping (`mutationStatusError` for writes). For a command 403 that is the D1 message, i.e. an empty-code refusal now degrades instead of rendering an empty code | new `api.test.ts` case |
| D4 | a type-conforming `fields` sent on a command refusal | ignored by the command path exactly as today; `ServerRefusal.fields` keeps the read member. A *wrong-typed* cross-surface member is stricter than either half: the whole decode fails and the status mapping answers — unreachable from the current server (`goal_error_fields/1` takes only the command members; read `fields` come from `changeset_errors`) | new `details` case asserts `fields` is absent from `details` |
| D5 | body decode | fatal UTF-8 on every path (one `TextDecoder` site): an OK write whose body is invalid UTF-8 raises the unreadable-body `UnexpectedResponse`, never a U+FFFD success | new invalid-UTF-8 write case; mutation (a) |
| D6 | 2xx without a receipt | non-transient `UnexpectedResponse` (message unchanged) | existing `:225-229` |
| D7 | 401 / 302 / 303 / `opaqueredirect` | `Unauthenticated` "Your session has ended. Sign in to continue." on every path | existing `:208-223`, `goal-attention.test.tsx:447`; mutation (c) |
| D8 | other statuses | `statusError`/`mutationStatusError` mapping unchanged (D1 excepted) | existing control 403 `:277`; mutation (d) |
| D9 | fetch/body-read rejection | `Network` "Cannot reach the server. Check your connection and try again." in both paths; the frozen envelope is untouched (the same `JSON.stringify(init.body)` bytes). The fetch half was identical at the base; the Goal path's *body-read* half is new (the base escaped it uncaught — recorded as D12), and the contract's "verified identical at `:614` and `:964-966`" cites the fetch catch only, so D9's premise needs that caveat when the contract is archived | existing `Network` cases; `detail.tsx` frozen-envelope tests; the D12 erroring-body write case |
| D10 | failed GET | reads no body (the GET branch reads `arrayBuffer()` only when `ok`) | existing `:100-105` body-read failure case |
| D11 | expiry answer whose body cannot be read (apply-stage record 2026-11-21, not a contract row) | the 302/303/`opaqueredirect`/401 classification runs before the body read on every path, so an expiry answer is classified from its status alone and its body is never read: an unreadable one cannot produce `Network`, and a body-read failure under any other status keeps the D9 mapping | `api.test.ts` cases "classifies a 401 write's expiry from its status when its body cannot be read", "classifies a redirect write's expiry from its status when its body cannot be read" (302) and "classifies an opaqueredirect write's expiry from its type before the body" (fix r1). Conductor decision outstanding: ratify D11 as a contract row (amend D7/D9) when archiving, or reject it |
| D12 | a Goal command/creation whose response body cannot be read (fix r1, not a contract row) | `Network` "Cannot reach the server. Check your connection and try again." from the same catch a failed fetch uses, and the frozen envelope keeps its `mutation_id` (`detail.tsx`'s `outcomeUnknown` keeps the attempt for a replay; at the base the stream error escaped `postPortalCommand` uncaught at `:975`, outside its fetch-only try at `:951-966`, and released the attempt) | new `api.test.ts` case "keeps a goal write's unreadable body a transport failure that preserves the attempt" (409 refusal status + 200 accepted status); mutation (h) |

Additional invariants: the write request sends the same method, headers (`Accept`,
`content-type`, `x-csrf-token`), `credentials: "same-origin"`, `cache: "no-store"`,
`redirect: "manual"` and body bytes as `postPortalCommand` did; the accepted write returns the
decoded `GoalReceiptBody` object (`{goal_id, mutation_id}`, extra members stripped) exactly as
today, so `detail.tsx:198`'s truthiness check and `attention.tsx`'s notice logic are unaffected.

## Decisions

- **D1 — the refusal surface is an explicit parameter of `request()`, not a new error class or
  a per-call converter.** Options: (A) a `surface: "server" | "mutation"` parameter with a
  default; (B) attach the command members to `ServerRefusal` and convert in the goal wrapper;
  (C) let `postGoalCommand`/`postGoalCreation` convert a decoded refusal themselves. (B) leaks
  command members into the read surface's error type and changes a class that existing tests
  construct positionally (`new ServerRefusal({status, code, message, fields})`, 8 sites). (C)
  forces the parser's result to survive as data through the Effect and moves the "exactly the
  old members" mapping out of the boundary. (A) keeps one parser and one mapping, costs one
  argument at exactly one new call site — the shared `goalCommand` body both command functions
  delegate to (`api.ts:975`) — and leaves the base's 29 read/control/chat `request()` call
  sites byte-identical (30 total at the final tree). Chosen: **(A)**.
- **D2 — the new named creation function is `postGoalCreation(fetcher, csrfToken, projectId,
  envelope)`.** It mirrors `postGoalCommand`'s shape and the server path
  (`/portal/api/projects/{id}/goals`), and matches the two callers' vocabulary ("Goal
  creation" in the test comment and in `attention.tsx`'s comment). Alternative rejected:
  parameterizing `postGoalCommand` with a path (it would let any surface post any path and
  loses the goal identity from the signature).
- **D3 — `MutationRefused` stays in `services/commands.ts`.** It is the write-policy type, the
  only refusal type with a `stale` getter, and both command surfaces already import it from
  there; `api.ts` imports it today. Moving it would touch `detail.tsx`, `attention.tsx` and
  three non-owned importers for no reduction.
- **D4 — the one parser keeps the name `parseRefusal`.** The contract's deletion grep names the
  old local `refusal`; `parseRefusal` is the parser that survives, now carrying the union, and
  `refusal` must have zero occurrences afterwards. `parseRefusal`'s return type becomes
  `ApiError | null` (the error it would throw), which is what the old one did for its half.
- **D5 — `decodeBodyText` + `unreadableBody` are factored out of `decodeBody`.** The parser is
  synchronous and must share the fatal decode; without the split the module would keep two
  `TextDecoder` sites (the contract's "one fatal body decode" is then literally one grep hit),
  and the two copies of the unreadable-body message can no longer drift. The `status: null`
  construction is preserved for the OK path, and `request()` keeps stamping the response status
  on its `mapError`.
- **D6 — `GoalReceiptBody` moves up beside `ProjectReceiptBody`/`WorkItemReceiptBody`.** The
  new `goalReceipt` decoder joins the other `*Receipt` decoders above it; leaving the schema
  below its only consumer reads as an accidental forward reference. Comment and members move
  verbatim.
- **D7 — an empty-code command refusal degrades to the status mapping (record-only).** D3 is a
  conductor decision (read strictness kept). It costs the goal surface a hand-written empty
  `code` (the server never sends one: the schema is `NonEmptyString` server-side too) and it is
  pinned by a new case instead of being left implicit.
- **D8 — the writes' `run()` gets no signal, and no `AbortController` is minted.** Writes have
  no caller cancellation today and W4-09 owns the reads/writes signal split; `run(effect)`
  passes no `runPromise` options, so the Effect runtime supplies its own fiber signal to
  `fetch` (the same mechanism the reads already use). A throwaway controller would be the
  contract's explicit failure control.

## Evidence plan (what will be run, and what proves what)

All gates foreground and serialized (never a suite beside a measurement/benchmark on this
host).

| what | command (from the repo root unless noted) | proves |
| --- | --- | --- |
| typecheck | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend typecheck` | the union schema, the `surface` parameter, the optional signal and both command functions type-check; no stale `postPortalCommand` reference survives |
| lint ratchet | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend lint` | exit 0 at `--max-warnings 229` (the ceiling is not raised) |
| focused suites | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend exec vitest run src/services/api.test.ts src/features/goals/goal-attention.test.tsx src/app/application.test.ts` | every pinned message, retry classification, expiry and refusal rendering still holds, plus the new cases |
| full suite | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend test` | the whole frontend suite |
| deletion greps | `git grep -nE "CommandErrorBody\|postPortalCommand" frontend/src` → 0; `git grep -nE "\brefusal\(" frontend/src` → 0; `git grep -n "TextDecoder" frontend/src/services/api.ts` → 1; `git grep -n "response\.text()" frontend/src` → comment only | clause 1's "gone" set and "one fatal body decode" |
| mutation (a) | `goalReceipt` decodes with a non-fatal `TextDecoder`, run `api.test.ts` | red: the new invalid-UTF-8 write case |
| mutation (b) | drop the command members from `MutationRefused.details`, run `goal-attention.test.tsx` | red: `goal-attention.test.tsx:322` (`/version 9, revision 3/`); `api.test.ts:306` (the `details` `toEqual`) reddens on the same flip (fix-stage re-run below) |
| mutation (c) | replace the 302/303 branch with `statusError(status)`, run `api.test.ts` | red: the redirect-as-expiry case `:217-222` |
| mutation (d) | make `mutationStatusError(403)` return `statusError(403)`, run `api.test.ts` | red: the control-command 403 case `:277` |
| mutation (e) | make the Goal command surface use the server 403 mapping, run `api.test.ts` | red: the new undescribed-command-403 pin |
| diff hygiene | `git diff --check`; the program's profile-directory grep over the diff | no whitespace damage, no host path |
| LOC delta | `git diff --stat` | reported against −120…−180 (report-only, D2) |

Judging artifacts for the answer: the deletion greps, the five flip runs (red output), the new
pins, the focused and full suite output with counts, and the LOC delta.

## Out of scope (with reasons)

- The `Reads`/`Writes` signal split and ceremony deletion — W4-09, which depends on this slice;
  the writes deliberately keep no caller signal and mint no controller.
- Refusal classification/guidance maps (`planning/write-policy.ts`, the resource/connection
  refusal folds) — W4-10.
- Reads' own paths beyond the shared parser — contract clause 4; the GET branch is unchanged.
- `services/realtime.ts`, the ChatPage boundary (`chat.tsx` already uses `request()`),
  `services/served.ts` (W2-15), `features/work/*`.
- `frontend/src/services/commands.ts` — no move needed (`MutationRefused` stays; the envelope
  helpers are untouched).

## Residual risks / known limits

- The D1 message change is user-visible on exactly one input (an undescribed 403 on a Goal
  command or Goal creation). It is authorized and pinned for both functions; the Goal surface
  now renders the same text the chat and control surfaces render for the same answer.
- An empty-code refusal on a command degrades to the status mapping (D3). Recorded in the
  plan, pinned by a case, and reported in the answer as the documented degradation.
- The removal of `postPortalCommand` is an internal-API removal: the function was exported, but
  no module outside `api.ts`, its tests and `attention.tsx` imports it (census above), and the
  program mandate drops backward compatibility.
- The dLOC band (−120…−180) is report-only: the union schema and the two surfaced command
  functions add lines back where the two hand-written boundaries collapse. The real delta is
  reported with its composition.
- `run`'s optional signal changes a module-local function's signature; the reads' call sites
  keep passing their signal, and no behaviour changes for them (no new default path is taken).

## Implement-stage result (final tree, 2026-11-21)

Base unchanged (`77861e00` = the W3-12 tip; both dispatch gates in history), so no line
reference moved for a rebase reason. Every gate ran foreground and serialized; no measurement
harness ran beside a suite.

### Gates (real exit codes; logs in the run directory)

| gate | command | exit | counts |
| --- | --- | ---: | --- |
| typecheck | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend typecheck` | 0 | `tsc --noEmit`, no output, 2.7 s |
| lint | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend lint` | 0 | 227 problems (0 errors, 227 warnings); `--max-warnings 229` untouched |
| focused suites | `… exec vitest run src/services/api.test.ts src/features/goals/goal-attention.test.tsx src/app/application.test.ts` | 0 | 3 files / 159 tests passed, 17.91 s |
| full suite | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend test` | 0 | 21 files / 528 tests passed, 75.74 s |
| new pins, by name | `… vitest run src/services/api.test.ts -t "undescribed command 403\|degrades a command refusal\|described command refusal\|invalid UTF-8 bytes in an accepted write"` | 0 | 4 passed / 36 skipped |
| moved pins, by name | the 401 creation case (`api.test.ts`) and the stale witness (`goal-attention.test.tsx:322`) | 0 | 1 passed each |
| diff hygiene | `git diff --check`; case-insensitive profile-directory grep over the diff and the new plan | 0 | clean; no host path in either |

The full-suite count is 528: the file's declared cases grew 30 → 34 (four new cases), and the
base already executed 524 (W3-12's recorded 522 predates its own round-1 fix commit
`40d192b1`, which added two `primitives.test.tsx` cases).

### Deletion greps / definition census (final tree)

| grep | result |
| --- | --- |
| `git grep -nE "\bpostPortalCommand\b\|CommandErrorBody" -- frontend/src` | 0 lines (exit 1) |
| `git grep -nE "^\s*function refusal\(\|^\s*const refusal =\|\brefusal\(" -- frontend/src/services/api.ts` | 0 lines (exit 1); other files' own `refusal` fixtures are unrelated |
| `git grep -n "TextDecoder" -- frontend/src` | 1 (`api.ts:562`, `decodeBodyText`) |
| `git grep -n "response\.text()" -- frontend/src` | 1 (the comment at `api.ts:559`); 0 code uses |
| `git grep -n "new AbortController" -- frontend/src/services` | 31 at the review tree, all in `api.test.ts` — the `0` was false (0 only for `-- frontend/src/services/api.ts`); see the F2 erratum and the PR-review stage note below |
| `git grep -nE "\bServerError\b\|\bCommandErrorBody\b\|\bpostPortalCommand\b" -- frontend/src` | 0 lines (exit 1) |
| api.ts line count | 1,044 → 1,042 |

### Mutation checks (each flip applied alone, run, reverted; sha256 restored)

`sha256sum -c` over the three changed files after the last revert printed OK for all three, so
every flip was reverted byte-identically.

| flip | applied mutation | witness run | result |
| --- | --- | --- | --- |
| (a) | `goalReceipt` decodes with a non-fatal `TextDecoder` (the old write behaviour) | `vitest run src/services/api.test.ts -t "rejects invalid UTF-8 bytes in an accepted write"` | exit 1, 1 failed / 39 skipped — `expected null to be an instance of UnexpectedResponse` (the non-fatal decode announced success) |
| (b) | `parseRefusal` passes `{}` as `MutationRefused.details` | `vitest run src/features/goals/goal-attention.test.tsx -t "sends the served expected version and revision"` | exit 1, 1 failed / 28 skipped — `Unable to find an element with the text: /version 9, revision 3/` at `goal-attention.test.tsx:322` |
| (c) | the 302/303 branch fails `statusError(response.status)` | `vitest run src/services/api.test.ts -t "classifies a redirect on a write as a session expiry"` | exit 1, 1 failed / 39 skipped — `expected UnexpectedResponse … to be an instance of Unauthenticated` |
| (d) | `mutationStatusError` becomes `statusError` | `vitest run src/services/api.test.ts -t "keeps an undescribed control refusal a session-protection 403"` | exit 1, 1 failed / 39 skipped — `expected Forbidden: You do not have access to this…` to match the session-protection message |
| (e) | the `"mutation"` surface's 403 uses the server mapping (the pre-change goal behaviour) | `vitest run src/services/api.test.ts -t "keeps an undescribed command 403"` | exit 1, 1 failed / 39 skipped — the D1 pin fails on the old "You do not have access to this workspace." text |

### LOC delta against the −120…−180 estimate (report-only, D2)

`git diff --numstat HEAD -- frontend/src`:

| file | added | deleted | net |
| --- | ---: | ---: | ---: |
| `frontend/src/services/api.ts` | 131 | 133 | **−2** (1,044 → 1,042 L) |
| `frontend/src/features/attention/attention.tsx` | 2 | 2 | 0 |
| `frontend/src/services/api.test.ts` | 64 | 2 | +62 (410 → 472 L) |
| `docs/plans/…-plan.md` (new, untracked) | 346 | 0 | +346 |

Production net is −2 L, not the estimated −120…−180. Composition of the api.ts delta (block
sizes at the base, by `git show HEAD:…`; at the final tree, by `awk` on the changed ranges):
the removed hand-written boundary is `CommandErrorBody` 10 L, `postPortalCommand` 50 L (with
its doc), the local `refusal` 13 L, the old `parseRefusal` 15 L and `ServerError` 10 L = 98 L,
plus the write's non-fatal decode/parse pair; the shared replacement is the union `RefusalBody`
schema 19 L (vs 10), `RefusalSurface` + the one parser 29 L (vs 15; `api.ts:598-626`), the
three one-site helpers `unreachable`/`unreadableBody`/`decodeBodyText` 15 L (vs the 7-line
`decodeBody`), `goalReceipt` 4 L (`:706-709`), `goalCommand` 17 L (`:964-980`) and the two
named entry points 22 L (`:982-1004`, against the old `postGoalCommand`'s 16 L), with the
write's body-read split adding 5 L to `request()` (54 L vs 49). The estimate assumed the write
path would ride the existing boundary nearly free; in the merged shape the Goal write keeps its
own receipt decode, its refusal construction and two named entry points, and the one parser
carries strictly more members than either of the two it replaces. Reported, not gated.

### Closeout state

Three tracked files changed (the owned set) plus this plan file (new). `docs/TODO.md` has no
entry naming this slice, the deleted names or `api.ts` line numbers, so it is unchanged. No
other document cites a *moved* `api.ts` line: the fix-stage census found
`docs/plans/2026-11-21-rf-w1-10-frontend-eslint-plan.md:37` citing `api.ts:121`/`:122`, both
before this slice's first hunk at `:289`, so it is unaffected; the remaining `api.ts:`
citations live in historical `docs/evaluations/model-routing/*` and
`docs/evidence/goal-0007c-*` records. The
contract file is left unchanged for the conductor to archive. Nothing is committed.

## Apply-stage note (2026-11-21, simplify apply)

Conductor brief: accept every simplify finding that stays inside this contract's owned set and
weakens no test/type/validation/permission; reject the others with the reason kept. The simplify
answer reviewed `52bdffd..3f1163e` (this slice stacked on W3-12/PR #190); its five findings:

| # | file | disposition | what changed / reason kept |
| --- | --- | --- | --- |
| 1 | `frontend/src/services/api.ts` (the `surface` parameter comment, `:636-638` before this stage) | **APPLIED** | the three-line comment deleted; the `RefusalSurface` type doc above the function carries the same rule. |
| 2 | `frontend/src/features/connections/resources.tsx:460-468` | **REJECTED** | file outside this contract's owned set — `features/connections/resources.tsx` is W3-12's (delivered by PR #190) — and no conductor amendment extends the set. Not applied; the duplicated kind-default derivation stands. Route: the W3-12 branch/#190, or an explicit extension here. |
| 3 | `frontend/src/features/connections/connections.tsx:279-281`; `frontend/src/features/work/assignment.tsx:202,268-270` | **REJECTED** | needs `connections.tsx` (W3-12 owns it as "import line only") and `assignment.tsx` (W3-12's explicit non-goal; W4-10 owns it); no amendment extends the set. Not applied; the two hand-written clear-then-restore copies stand. |
| 4 | `frontend/src/services/api.test.ts` (goal command boundary) | **APPLIED** | one new case: a 2xx receipt decodes on both command functions, the creation path percent-encodes its project id, and a receiptless creation 2xx is refused (D6). |
| 5 | the boundary's expiry/body-read order vs D7/D9 | **APPLIED as its recommended option (a)** | no behaviour change. The shipped boundary classifies an expiry answer from its status alone before reading the body — what D7/Change (4) require ("401 → `Unauthenticated`, byte for byte") — so the precedence is recorded as new row **D11** above and pinned by one new case. Option (b) (body read before the expiry checks) was not taken: it makes a 401 write whose body cannot be read produce `Network`, contradicting D7/Change (4). The contract file is unchanged; D11 is this stage's record, not a contract row. |

Refreshed numbers (single source = this plan; the implement-stage tables above remain that
stage's record and are superseded by this note):

| value | implement record | on the final tree |
| --- | --- | --- |
| `api.ts` line count | 1,044 → 1,042 | **1,039** (apply: −3) |
| `api.ts` numstat vs `77861e00` | +131/−133 (−2) | **+128/−133 (−5)** |
| `api.ts` refs after the deletion point | `goalReceipt :706-709`, `goalCommand :964-980`, entry points `:982-1004` | `:703-706`, `:961-977`, `:979-1001` (−3); refs before `:636` (`:559`, `:562`, `:598-626`) unchanged |
| `api.test.ts` LOC | 410 → 472 | **503** (apply +31/−0; vs base +95/−2) |
| `api.test.ts` declared cases | 30 → 34 | **36** (apply +2) |
| focused suites | 3 files / 159 tests | **3 files / 161 tests** |
| full suite | 21 files / 528 tests | **21 files / 530 tests** |

### Gates (final tree, uncommitted; foreground, serialized; no measurement harness beside a suite)

| gate | command | exit | counts |
| --- | --- | ---: | --- |
| typecheck | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend typecheck` | 0 | `tsc --noEmit`, no output, 2.71 s |
| lint | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend lint` | 0 | 227 problems (0 errors, 227 warnings); `--max-warnings 229` untouched, 36.92 s |
| focused suites | `… exec vitest run src/services/api.test.ts src/features/goals/goal-attention.test.tsx src/app/application.test.ts` | 0 | 3 files / 161 tests passed, 18.18 s |
| full suite | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend test` | 0 | 21 files / 530 tests passed, 35.20 s |
| the touched suite alone | `… vitest run src/services/api.test.ts` | 0 | 42 passed (36 declared cases) |
| diff hygiene | `git diff --check` | 0 | clean |

### Deletion greps / census (re-run by this stage)

| grep | result |
| --- | --- |
| `git grep -nE "\bpostPortalCommand\b\|CommandErrorBody" -- frontend/src` | 0 lines (exit 1) |
| `git grep -nE "\brefusal\(" -- frontend/src/services/api.ts` | 0 lines (exit 1) |
| `git grep -n "TextDecoder" -- frontend/src/services/api.ts` | 1 (`:562`, `decodeBodyText`; before the apply's deletion point) |
| `git grep -n "response\.text()" -- frontend/src` | 1 (the comment at `:559`); 0 code uses |
| `git grep -n "new AbortController" -- frontend/src/services/api.ts` | 0 (the boundary mints none) |
| `git grep -n "new AbortController" -- frontend/src/services/api.test.ts` | 31 (the file's per-case test signal, +1 from the new pin) |

### New-pin flips (each applied alone, run, reverted; sha256 restored)

`sha256sum -c` after the last revert printed `frontend/src/services/api.ts: OK`, and the touched
suite passed again on the restored bytes.

| flip | applied mutation | witness | result |
| --- | --- | --- | --- |
| (f) | `postGoalCreation` drops `encodeURIComponent(projectId)` | `vitest run src/services/api.test.ts -t "carries an accepted write's receipt on both command paths"` | exit 1, 1 failed / 41 skipped — `expected '/portal/api/projects/p x/goals' to be '/portal/api/projects/p%20x/goals'` |
| (g) | the early `status === 401` classification removed (the option-(b) shape) | `vitest run src/services/api.test.ts -t "classifies a 401 write's expiry from its status"` | exit 1, 1 failed / 41 skipped — `expected Network: Cannot reach the server… to be an instance of Unauthenticated` |

## Fix-stage note (2026-11-21, review r1)

Conductor brief: the review's findings, fixed inside this contract's owned set. This stage
touched only `frontend/src/services/api.test.ts` (three new pins) and this plan; no production
file moved, so every production coordinate above stands. `git diff origin/main` carries no
profile-directory path.

| # | disposition | what changed / why valid |
| --- | --- | --- |
| F1 | FIXED (record + pin) | the new **D12** row records the base divergence (the Goal path's body-read rejection was an uncaught stream error) and the D9 row now carries its caveat; the goal path is pinned by the new erroring-body case (409 + 200 → `Network`, exact message). The contract file is outside the owned set, so D9's wording there (`verified identical at :614 and :964-966`) is left for the conductor to amend at archiving. The finding's optional detail-level `mutation_id` assertion was not added (scope). |
| F2 | FIXED (erratum) | the implement answer is outside the owned set; the erratum below records the false grep row and the stage-scoping its counts need. |
| F3 | FIXED (pin) + conductor decision outstanding | the D11 pin now covers 302 and a hand-built `opaqueredirect` write (unreadable/absent body → `Unauthenticated`); ratifying D11 as a contract row (amending D7/D9) remains the conductor's archiving decision, restated in the D11 row. |
| F4 | FIXED (record) | `decodeBody` count corrected (22 call sites + its definition); the "24 other call sites" claim corrected (one new call site at `api.ts:975`, the base's 29 unchanged); the `api.ts` line-citation claim corrected with the W1-10 citation (`:121`/`:122`, before the first hunk at `:289`); the `api.test.ts:306` second witness for mutation (b) cross-referenced (its flip was re-run). |
| F5 | FIXED (record) | D4 now reads "a type-conforming `fields`" and records the wrong-typed cross-surface strictness with its unreachability (`goal_error_fields/1`; read `fields` come from `changeset_errors`). |

### Erratum for the implement-stage artifact (F2)

`symmetry-rf-w3-13-http-boundary-implement/answer.md:24` (and its JSON gate row) records
`git grep -n 'new AbortController' -- frontend/src/services` as exit 1 with 0 matches. That
scope contains `api.test.ts` (29 such lines at the base, 31 at that stage's tree), so the row's
exit code and count are false; only `-- frontend/src/services/api.ts` yields exit 1. Its other
counts (`api.ts` 1,042, `api.test.ts` 472, 34 cases, focused 159, full 528) were true of that
stage's tree and are stage-scoped; the live values are the table below. The erratum also covers
the identical false row in this plan's implement-stage census (`:304`), which now carries the
re-measured 31 (all `api.test.ts`); the PR-#204 review round's P2-1 pin raises the live count to
32 (the stage note appended at the end of this plan is the current single source).

### Counts after the fix (single source; supersedes the apply note's refreshed table)

| value | apply note | now |
| --- | --- | --- |
| `api.test.ts` LOC | 503 | **549** (three new cases) |
| `api.test.ts` declared cases | 36 | **39** |
| focused suites | 3 files / 161 tests | **3 files / 164 tests** |
| full suite | 21 files / 530 tests | **21 files / 533 tests** |
| `frontend/src/services/api.ts` | 1,039 | **1,039** (untouched by this stage) |
| `new AbortController` in `frontend/src/services` | 31 (all `api.test.ts`) | **31** (all `api.test.ts`; the three pins add none) |

### Gates (fix r1, foreground, serialized; no suite beside another)

| gate | command | exit | counts |
| --- | --- | ---: | --- |
| typecheck | `pnpm --pm-on-fail=ignore typecheck` (in `frontend/`) | 0 | `tsc --noEmit`, no output |
| lint | `pnpm --pm-on-fail=ignore lint` (in `frontend/`) | 0 | 227 problems (0 errors, 227 warnings); `--max-warnings 229` untouched |
| focused suites | `pnpm --pm-on-fail=ignore exec vitest run src/services/api.test.ts src/features/goals/goal-attention.test.tsx src/app/application.test.ts` | 0 | 3 files / 164 tests passed, 44.22 s |
| full suite | `pnpm --pm-on-fail=ignore test` (in `frontend/`) | 0 | 21 files / 533 tests passed, 64.34 s (captured re-run; see the disclosure below) |
| touched suite alone | `… exec vitest run src/services/api.test.ts` | 0 | 45 passed |

Gate-hygiene disclosure: the first post-fix full-suite run exited 1 while the host ran ~6×
slower than the greens (136 s wall, jsdom init 411 s tracked); that run's output was truncated
by this stage's own `tail` before capture, so the failing test is unnamed. Two subsequent full
runs exited 0 (the fix's three new tests pass in the focused and `api.test.ts` runs), and the
red was not reproduced. Flagged for the conductor's CI as a possible contention flake, not
attributed to a named test.

### New-pin flips (fix r1; each applied alone, run, reverted; sha256 restored)

| flip | applied mutation | witness | result |
| --- | --- | --- | --- |
| (h) | the body-read catch answers `unreadableBody(response.status)` instead of `unreachable` | `api.test.ts -t "keeps a goal write's unreadable body a transport failure"` | exit 1, 1 failed / 44 skipped — `expected UnexpectedResponse … { …(3) } to be an instance of Network` |
| (i) | the 302/303/`opaqueredirect` expiry branch removed (the option-(b) shape for redirects) | `api.test.ts -t "classifies a redirect"` and `-t "classifies an opaqueredirect"` | exit 1 — 2 failed (existing redirect-as-expiry → `UnexpectedResponse`; the new 302 pin → `Network`) and 1 failed (the new `opaqueredirect` pin → `Network`) |
| (j) | mutation (b) re-run: `parseRefusal` passes `{}` as `MutationRefused.details` | `api.test.ts -t "carries a described command refusal"`; `goal-attention.test.tsx -t "sends the served expected version and revision"` | exit 1 — 1 failed (`expected {} to deeply equal { current_version: 9, …(3) }`, the second witness) and 1 failed (`Unable to find … /version 9, revision 3/` at `:322`) |

`sha256sum -c` over `frontend/src/services/api.ts` printed OK after every revert.

## Fix-stage note (2026-11-21, PR #204 review r1: P2-1/P3-1/P3-2)

Conductor brief: clear the Opus review's `CHANGES REQUESTED` inside this contract's owned set —
P2-1 (a request cancelled during the body read no longer aborted its fetch), P3-1 (PR-body
staleness; no repo edit beyond this record) and P3-2 (the false `new AbortController` row at
`:304`). Files touched: `frontend/src/services/api.ts`, `frontend/src/services/api.test.ts` and
this plan.

| # | disposition | what changed / why valid |
| --- | --- | --- |
| P2-1 | FIXED | `request()` (`api.ts:639-671`) fetches, classifies `expired` (`opaqueredirect`/302/303/401) and reads the body — only when `!expired && (response.ok \|\| method !== "GET")` — inside one signalled `Effect.tryPromise` again, so a caller cancel during the body read aborts the fetch signal (the base layout; the split second step had no controller). D11/D12 unchanged: the expiry classification still precedes any read, and a body-read rejection still maps through `unreachable` → `Network` (all pinned cases stay green). |
| P2-1 pin | ADDED | `api.test.ts` `"aborts the in-flight body download when the consumer cancels during the read"` (new `request cancellation` describe, `:198-226`): a hand-built 200 whose `arrayBuffer` never settles (`Response` in the test environment pulls a stream body at construction, so a stream probe would fire before the boundary reads); the consumer aborts once the read has been called and the captured `init.signal` must be aborted. |
| P3-1 | FIXED (no repo edit) | the PR body now carries the delivered counts, the `api.ts` LOC delta and the D1/D11/D12 user-visible changes; "Nothing committed" is gone. |
| P3-2 | FIXED (record) | the implement-stage census row at `:304` carries the re-measured scope (31 at the review tree, all `api.test.ts`); the F2 erratum covers it. |

### P2-1 pin: red and green (the unfixed bytes restored with `git stash push/pop` over `api.ts`)

| run | `api.ts` under test | command | exit | result |
| --- | --- | --- | ---: | --- |
| red | HEAD `833423f9` | `vitest run src/services/api.test.ts -t "aborts the in-flight body download"` | 1 | 1 failed / 45 skipped (46) — `expected false to be true` at `api.test.ts:224` (the fetch signal was not aborted) |
| green | the fix | the same filter | 0 | 1 passed / 45 skipped (46) |

### Counts after this round (single source; supersedes the fix-r1 table above)

| value | fix r1 | now |
| --- | --- | --- |
| `frontend/src/services/api.ts` LOC | 1,039 | **1,042** (+3) |
| `api.ts` numstat vs the base | +128/−133 (−5) | **+119/−121 (−2)** |
| `api.test.ts` LOC | 549 | **579** (+30) |
| `api.test.ts` declared cases | 39 | **40** |
| focused suites (3 files) | 164 tests | **165 tests** |
| full suite | 21 files / 533 tests | **21 files / 534 tests** |
| `new AbortController` in `frontend/src/services` | 31 | **32** (all `api.test.ts`) |

### Gates (PR-review r1 stage; foreground, serialized; no suite beside another)

| gate | command | exit | counts |
| --- | --- | ---: | --- |
| typecheck | `pnpm exec tsc --noEmit -p .` (in `frontend/`) | 0 | no output |
| lint | `pnpm lint` (in `frontend/`) | 0 | 227 problems (0 errors, 227 warnings); ceiling 229 untouched |
| focused suites | `pnpm exec vitest run src/services/api.test.ts src/features/goals/goal-attention.test.tsx src/app/application.test.ts` | 0 | 3 files / 165 tests passed |
| full suite | `pnpm exec vitest run --maxWorkers=2` (in `frontend/`) | 0 | 21 files / 534 tests passed (240.65 s) |
| diff hygiene | `git diff --check`; case-insensitive profile-directory grep over the diff | 0 / 1 | clean; 0 matches |

Full-suite disclosure: this stage's full suite needed bounded workers because the host was shared
with other agent worktrees and ran up to ~4× slower than the fix-r1 runs (16 cores, 15 forked
vitest workers by default). The two default-worker runs exited 1 with 7 and 9 load-induced
TestingLibrary ready-timeouts (e.g. `Unable to find role="heading" and name "Subject work item"`,
one `Test timed out in 5000ms`); the eight files run 2 flagged all pass together in one
invocation (8 files / 241 tests, exit 0). A 4-worker run then passed (21 files / 534 tests,
64.46 s) on a tree differing from the delivered bytes only in the P2-1 pin's readiness mechanism,
and a second 4-worker run under heavier load exited 1 (9 timeouts across 5 files, 339.12 s; the
delivered `api.test.ts` passed inside it). The delivered-bytes gate above (2 workers) is green
with no failures; the attribution to host contention is the same the fix-r1 disclosure records.
