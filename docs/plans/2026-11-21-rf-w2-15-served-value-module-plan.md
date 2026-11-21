# rf-w2-15-served-value-module plan — one served-value narrowing module

Contract: `goal-rf-w2-15-served-value-module.md` (GDD, 2026-11-21), binding with its
conductor decision C1–C6. Base for every coordinate in this plan:
`c87c9918e4f8aa09511018a4e23c42fc949d8295` (= `origin/main`, this worktree's HEAD, clean
tree). The contract's dispatch gate (#146 `rf-w1-10-frontend-eslint` merged) is satisfied by
that revision, so C6's rebase is a no-op and every line reference below is derived at it.

Sources: `program-plan.md` §5 W2-15 (row: risk low, proto none, deps W1-10);
`findings/frontend.md` F4:302-324 (four drifting copies, the `Loosen<T>` /
`no-unnecessary-condition` tip, oracle list); `w2-contract-review-opus-2.md` and
`w2-contract-review-ds-2.md` as reconciled into the contract's C1–C6. Shape references:
`docs/plans/2026-11-21-rf-w1-10-frontend-eslint-plan.md` (#146, same area) and
`docs/plans/2026-11-21-rf-w2-05-schema-validators-plan.md` (#150, same wave; pin-tests-first
procedure).

## Measured basis (base revision)

Declaration-level copies at HEAD (`grep -rn "const asText\|function asText\|const asRecord\|function asRecord\|const safeUrl\|function safeUrl" frontend/src`):

| copy | location | semantics |
| --- | --- | --- |
| `asText` | `features/work/presentation.ts:32` (exported) | string, **trimmed** blank → null |
| `asRecord` | `:36` (exported) | non-array object or null |
| `asRecords` | `:41` (exported) | array of records, non-records dropped |
| `safeUrl` | `:49` (exported) | `asText` first, then http(s) only |
| `asText` | `features/connections/resources.tsx:70` (module-local) | string, **empty** only → null (no trim) |
| `asVersion` | `resources.tsx:71` (module-local) | finite number or 0 — **not a copy**, stays |
| `safeUrl` | `resources.tsx:168` (module-local) | http(s) only, takes `string` |
| `failureText` | `features/work/presentation.ts:185` (exported) | `reason → message → summary`, `"Failed: "` prefix |
| `failureText` | `features/chat/presentation.ts:432` (module-local) | `message → summary → error`, no prefix |
| `servedText`/`servedMember` | `features/goals/served-text.ts` (exported, only file) | scalars stringified, caller-supplied absence |

Import/render surface at the base revision: 13 modules import the `work/presentation` module —
6 through `../work/presentation` (`git grep -l '\.\./work/presentation' c87c9918 -- frontend/src`)
and 7 through `./presentation` inside `features/work` (`git grep -l -e "from ['\"]\./presentation['\"]" c87c9918 -- frontend/src/features/work`);
12 import it at the final tree, because `chat.test.tsx` moves its `asRecord` import to
`services/served`; `goals/served-text.ts` has exactly one importer (`goals/detail.tsx:8`). Pins
that must stay green: `work/presentation.test.ts` (22 cases, including `asText("  ") → null` at
`:10`), `chat/presentation.test.ts` (39 cases), `connections.test.tsx` (the resources DOM tests).

## Deliverable shape

1. **`frontend/src/services/served.ts` (new)** — the module header states both policies, then:
   - `asText`, `asRecord`, `asRecords`, `safeUrl` — moved **byte-for-byte** from
     `work/presentation.ts:27-58` (each block verbatim, including its doc comment);
   - `servedText`, `servedMember` — moved from `goals/served-text.ts` verbatim (the old file
     header becomes `servedText`'s doc comment, unchanged bytes);
   - a named-policy paragraph that points at the three functions that are *deliberately not*
     here (C2/C3/second-convention boundary).
   No re-export shim anywhere: `work/presentation.ts` does not re-export the moved names.
2. **`frontend/src/features/work/presentation.ts`** — lines 27-58 deleted; the four names
   imported from `../../services/served`; `failureText` renamed `workFailureText` (definition
   `:185`, internal use in `reportedResult` `:210`). No other edit.
3. **`frontend/src/features/chat/presentation.ts`** — the `work/presentation` import keeps only
   `decisionRecord`, `executionStateLabel`; the four primitives come from
   `../../services/served`; the local `failureText` (`:432`) renamed `chatFailureText` (its one
   other use is in `chatFailureView`, `:451`). Doc comment at the old `:12` updated to name
   both import sources.
4. **`frontend/src/features/connections/resources.tsx`** — local `safeUrl` deleted and adopted
   from `../../services/served` (identical for every string input: the shared helper's `asText`
   guard only short-circuits empty or whitespace-only strings, which `new URL` throws on as well,
   and every other throwing form is covered by the identical `try`/`catch`). Local `asText` renamed
   `presentText` with a one-line rationale, **pending the C1 measurement below**; if the
   measurement shows the trimming helper is rendering-equivalent on the pinned inputs, the local
   helper is deleted and the shared `asText` adopted instead.
5. **`frontend/src/features/goals/served-text.ts`** — deleted; `goals/detail.tsx` imports
   `servedText`/`servedMember` from `../../services/served`.
6. **Import/call-site migration only** (C4): `work/assignment.tsx` (`asText`),
   `work/control.tsx` (`asRecord`, `asText`), `work/detail.tsx` (`asText`, `safeUrl`,
   `workFailureText`), `work/receipts.ts` (`asRecord`, `asText`), `chat/chat.test.tsx`
   (`asRecord`), `work/presentation.test.ts` (`asText`, `asRecord`, `workFailureText`). No call
   site changes shape, no logic edits.
7. **`frontend/src/features/connections/connections.test.tsx`** — one new case in the
   `project resources` block pinning whitespace-only `name`/`status`/`sync_status` rendering
   (C1). Added and run **before** the resources change, then re-run after.
8. **This plan file** (`docs/plans/2026-11-21-rf-w2-15-served-value-module-plan.md`).

No other file changes. `docs/TODO.md` / `docs/archive/CLOSED-TODO.md` do not move: no open entry
names this surface — `grep -n "asText\|safeUrl\|asRecord\|served-text\|servedText\|servedMember\|presentation\.ts" docs/TODO.md docs/archive/CLOSED-TODO.md`
matches only 11 historical lines in `CLOSED-TODO.md` (prose inside closed entries), zero in
`docs/TODO.md`. No other doc in `docs/` names any of these helpers —
`grep -rn "asText\|safeUrl\|served-text\|servedText\|servedMember" docs/ --include=*.md` exits 1
once this plan file is excluded — so no design/contract document is made stale;
`services/commands.ts` and `services/control-actions.ts` are the existing precedent for non-API, non-event modules in `services/`.

## API shape

```ts
// frontend/src/services/served.ts
export function asText(value: unknown): string | null;                       // unchanged
export function asRecord(value: unknown): Record<string, unknown> | null;    // unchanged
export function asRecords(value: unknown): Record<string, unknown>[];        // unchanged
export function safeUrl(value: unknown): string | null;                      // unchanged
export function servedText(value: unknown, absence: string): string;         // unchanged
export function servedMember(value: unknown, member: string, absence: string): string; // unchanged
```

Nothing else is exported; nothing is re-exported. The signature set is exactly the union of the
two moved sources, so every call site compiles unchanged apart from its import path.

## Exact edge-case semantics the contract demands

- **Blank text is absent (work policy, trimmed).** `asText("  ") === null` — pinned at
  `work/presentation.test.ts:10`, which moves its import, not its assertion. `asRecords` drops
  non-record members and non-arrays; `asRecord` rejects arrays (an array is not a record).
- **Only http(s) links render as anchors.** `safeUrl` narrows through `asText` first, so
  `""`, `"   "`, `"javascript:…"`, `"mailto:…"` and non-strings are all null; a valid URL is
  returned as served (unnormalized), never re-serialized.
- **`resources.tsx` keeps its no-trim policy unless proven identical (C1).** Base behaviour:
  a whitespace-only `name` renders as the served blank text (not the id fallback), a
  whitespace-only `status`/`sync_status` renders `Unknown resource status: `/`Unknown sync
  status: ` (not the known-word label `Unknown`). The new DOM case pins exactly that on base
  bytes; the adopt/keep decision is made from its run under the trimming helper, and whichever
  way it lands is recorded in the answer with the run's output.
- **The two failure-text policies stay separate and keep their rendered text (C2).**
  `workFailureText` = first readable of `reason`/`message`/`summary`, prefixed `"Failed: "`
  (pinned at `work/presentation.test.ts:32` and the reported-result cases);
  `chatFailureText` = first readable of `message`/`summary`/`error`, no prefix (pinned by the
  `chatFailureView` cases in `chat/presentation.test.ts`). Distinct policy names, no merged
  resolver, no signature change at any call site.
- **`servedText`/`servedMember` stay a third, declared policy (C3).** Scalars render as
  themselves (`12` → `"12"`, `false` → `"false"`), strings render even when blank, everything
  else renders the caller's absence text; `servedMember` also treats a missing member as
  absence. It is not an `asText` variant and no call site's absence string changes.
- **The second-convention rule applies only to identical semantics.** After this change there
  is exactly one definition of each of the six moved names in `frontend/src`; the three
  remaining local functions (`presentText`, `workFailureText`, `chatFailureText`, plus
  `asVersion`) are declared policies named for their surface, and each carries its rationale.

## Decisions

- **D1 — `servedText`/`servedMember` move into `services/served.ts`.**
  Options: (A) move, rewriting only the new module header; (B) leave `goals/served-text.ts` in
  place. The contract allows both (C3) but its removal list names "the goals variant's local
  definitions", and clause 1's observable is "one shared served-value module". (A) gives every
  surface one import site for served-value narrowing, deletes a 20-line single-importer module,
  and keeps the function bytes identical; the cost is that one file now documents two policies,
  which the header states explicitly. Chosen: **(A)**. The two policies are not merged — a
  future merge is exactly what C3 forbids, and the header says so.
- **D2 — the failure-text policies stay in their surface modules, renamed.**
  Options: (A) rename in place (`workFailureText` in `work/presentation.ts`, `chatFailureText`
  in `chat/presentation.ts`); (B) move both into `services/served.ts` under policy names.
  (A) keeps "no call-site signature change" (C2) and keeps a surface's rendered-policy decision
  next to the surface that renders it; (B) puts two non-shareable policies beside six shareable
  helpers and makes `services/served.ts` the place a future author would merge them. Chosen:
  **(A)**, per the conductor ruling ("stay separate with policy names").
- **D3 — `resources.tsx` keeps a local helper for its own text policy.**
  Options: (A) adopt the shared trimming `asText`; (B) keep the no-trim rule as a renamed local
  `presentText` with a one-line rationale. The contract makes this evidence-driven: (A) only
  after the whitespace-only DOM pins stay green. The measurement below decides; the plan's
  recommendation before measuring is (B), because the two rules answer different questions
  (`asText`: "is this member worth rendering at all" for free-form payload JSON; `presentText`:
  "is this served field present" for a declared DTO field) and the base rendering is the
  observed contract. Recorded outcome: see "C1 measurement" below.
- **D4 — moving the four primitives changes no import-time behaviour.**
  They are pure functions; `work/presentation.ts` keeps its own exports (`ownerKey`,
  `ciLabel`, …) and its importers of those names are untouched. The only visible change for a
  consumer is the import path of the six moved names, which the migration covers completely —
  a missed site is a `tsc` error, not a silent duplicate.

## C1 measurement (base pins first)

Procedure, in this order, all foreground:

1. Add the whitespace-only DOM case to `connections.test.tsx` and run it against HEAD
   `resources.tsx` (local no-trim `asText`) — must be green.
2. Point `resources.tsx` at the shared trimming `asText` and re-run the same case.
3. If green, keep the adoption; if red, revert to the local helper renamed `presentText` and
   record the failing assertion verbatim as the evidence for the declared policy.

Result is recorded in the "C1 outcome" note at the end of this file (appended at apply time).

## C1 outcome (measured 2026-11-21, at `c87c9918` + this slice's edits)

1. Base pin (HEAD `resources.tsx`): `pnpm --pm-on-fail=ignore exec vitest run src/features/connections/connections.test.tsx -t "whitespace-only"`
   → **exit 0**, `1 passed | 56 skipped (57)`.
2. Adoption probe (the local helper deleted, `asText` imported from `../../services/served`), same
   command → **exit 1**, `AssertionError: expected false to be true` at the blank-heading
   assertion (`connections.test.tsx:1247`): the trimming helper substitutes the id fallback for
   the served blank name, so the whitespace-only rendering is not equivalent.
3. Chosen branch: **(B)** — `resources.tsx` keeps its no-trim rule as a renamed local
   `presentText` with the rationale above it. `safeUrl` is adopted; equivalence for string input
   measured separately (23 inputs incl. blank/whitespace, non-http schemes, relative and
   malformed forms, and the composed `presentText` → `safeUrl` path: 0 mismatches).
4. Final tree: same case → **exit 0**, `1 passed | 56 skipped (57)`.

## Evidence plan (what will be run, and what proves what)

All gates foreground and serialized (never a vitest run beside another full suite on this host).

| what | command (from repo root unless noted) | proves |
| --- | --- | --- |
| typecheck | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend typecheck` | every migrated import resolves; no stale name survives; no unused local left |
| named unit tests | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend test src/features/work/presentation.test.ts src/features/chat/presentation.test.ts` (in `frontend/`) | the 22 + 39 rendered-text pins (incl. `asText("  ")` and both failure policies) unchanged |
| resources DOM tests | `… test src/features/connections/connections.test.tsx` (in `frontend/`) | the resources surface renders as before, plus the new whitespace pin |
| full suite | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend test` | 510 tests / 20 files (509 + the new case), 0 failed |
| lint ratchet | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend lint` | exit 0 at `--max-warnings 229`: the move introduces no new typed-lint finding |
| copy count | `grep -rn "const asText\|function asText\|…"` (the base command above) | each of the six names has exactly one definition |
| moved bytes | extraction + `diff` of each moved block against `git show HEAD:<file>` | the move is byte-identical, not a rewrite |
| diff hygiene | `git diff --check`; the program's case-insensitive profile-directory grep over `git diff origin/main...HEAD` | no whitespace damage, no host path in the diff (grep exits 1) |

Judging artifacts for the answer: the copy-count table before/after, the C1 pin's three runs
(base green / adopting red / final green), the targeted and full suite counts, the lint exit
code with its warning count, and the moved-byte diff (empty).

Failure-control checks: the new whitespace pin is the control for the `resources.tsx` branch
(it fails if a trimming helper is adopted silently); the moved-byte diff is the control for
"moved, not rewritten"; the existing 61 presentation pins are the control for "rendering
unchanged". No test is renamed, deleted or weakened; one case is added.

## Out of scope (with reasons)

- Refusal/outcome classification (`services/refusals.ts`) — contract clause 4, W4-10.
- UI primitives and the resource-dialog collapse — W3-12, which follows on `resources.tsx`.
- The HTTP boundary (`services/api.ts` `request()`) — W3-13.
- `Loosen<T>` / decode-boundary retyping and the `no-unnecessary-condition` burn-down (78
  findings): F4 names it as the same family, but the contract's Change clause and C1 keep this
  slice at the narrowing helpers; the burn-down needs the decode boundary re-typed first.
- `asVersion` stays local to `resources.tsx`: it is a numeric policy with one call site, not a
  copy of anything (documented in the copy table above).

## Residual risks / known limits

- `services/served.ts` documents two policies in one file (D1); the header and the function
  docs state that they must not be merged, but a future author could still try. The
  `servedText` doc keeps the original wording, which names its own rule.
- The C1 decision is data-dependent: if the measured rendering happens to be identical, the
  slice deletes one more local helper than the recommendation above; the answer records which
  branch ran with its output either way.
- `connections.test.tsx` whitespace pin asserts DOM text (`h3` textContent and badge labels)
  rather than an accessible name, because a whitespace-only accessible name normalizes to the
  empty string; the assertions are still user-visible rendering, not class names.
- Lint is not in the contract's gate row but the slice depends on the W1-10 ratchet
  (`--max-warnings 229`); it is run and reported so a moved finding would be visible. If the
  move removed findings, the ceiling would no longer be exact — reported as a residual for the
  `package.json` owner (outside this slice's owned set) rather than silently re-pinned.

## Gate results (implement stage, 2026-11-21, final tree, all foreground, serialized)

| gate | command | exit | counts |
| --- | --- | ---: | --- |
| typecheck | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend typecheck` | 0 | no output |
| named tests | `pnpm --pm-on-fail=ignore exec vitest run src/features/work/presentation.test.ts src/features/chat/presentation.test.ts src/features/connections/connections.test.tsx` (frontend) | 0 | 3 files / 118 tests passed (22 + 39 + 57) |
| full suite | `pnpm --pm-on-fail=ignore test` (frontend) | 0 | 20 files / 510 tests passed (509 at base + the new DOM case), 47.4 s |
| lint ratchet | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend lint` | 0 | `229 problems (0 errors, 229 warnings)` — equals the recorded ceiling in `frontend/package.json` |
| knip | `pnpm --pm-on-fail=ignore knip` (frontend) | 0 | no findings; one pre-existing advisory config hint (`coverage/**` ignoreFiles) |
| C1 pin (base) | `… vitest run src/features/connections/connections.test.tsx -t "whitespace-only"` at HEAD `resources.tsx` | 0 | 1 passed / 56 skipped |
| C1 pin (adoption probe) | same, with the shared trimming `asText` imported | 1 | `AssertionError: expected false to be true` |
| C1 pin (final) | same, final tree | 0 | 1 passed / 56 skipped |
| moved bytes | `diff -u <(git show HEAD:<file> \| sed -n …) <(sed -n … ` for both moved blocks | 0 | empty diff (byte-identical) |
| diff hygiene | `git diff --check`; the program's case-insensitive profile-directory grep over `git diff origin/main...HEAD` | 0 / 1 | no whitespace damage; no host path |
| dLOC (C5, reported, not gated) | `git diff origin/main...HEAD -- frontend/src` (`-U0` hunks): raw added−removed; executable = non-blank, non-comment-only; executable non-test = excluding `*.test.ts(x)` | — | raw **+36**, executable **+9**, executable non-test **−3** (non-test raw net +18) — the C5 prediction "about −10..−25" does not reproduce at any scope; only the non-test executable net is negative, at −3. Measured at the fix stage on the same final tree (the fix is docs-only) |

Copy census on the final tree (`grep -rn` over `frontend/src` for every definition form):
exactly one definition each of `asText`, `asRecord`, `asRecords`, `safeUrl`, `servedText`,
`servedMember` — all in `frontend/src/services/served.ts` — and the four declared policies that
deliberately stay local (`presentText`, `asVersion`, `workFailureText`, `chatFailureText`) plus
zero occurrences of the bare identifier `failureText` and zero importers of
`features/goals/served-text.ts` (file deleted).

## Comment-policy sweep (2026-11-22)

Module intent removed from this slice's files
(`control/test/symmetry_control_web/controllers/portal_workflow_test.exs`,
`frontend/src/services/served.ts`) by the 2026-11-22 comment-policy sweep is recorded in
[`docs/evidence/comment-policy-sweep-2026-11-22-moved-intent.md`](../evidence/comment-policy-sweep-2026-11-22-moved-intent.md)
("ws9b-control-test", "ws10-frontend"); the sweep's plan is
[`docs/plans/2026-11-22-comment-policy-sweep-plan.md`](2026-11-22-comment-policy-sweep-plan.md).
