# rf-w4-10-refusal-classification plan — one refusal/outcome vocabulary under `services/refusals.ts`

Contract: `goal-rf-w4-10-refusal-classification.md` (GDD, 2026-11-21/22), binding with its
conductor decisions: **D1** (rendered refusal sentences, notice roles, tone classes and alert
semantics frozen; pinned suites pass unmodified; code hygiene with no observable effect is the one
authorized delta class and must be listed base→HEAD), **D2** (LOC report-only), the mutation-proof
rule, the **lesson-32** control-flow audit, the **W4-review-reconcile** decisions (E8 runnable
oracles, the binding "no base map unless every surface carries the identical step", call-site
ownership) and the owner **comment policy** (2026-11-22). Base revision for every coordinate:
`7f34d40d` (`refactor(frontend): split api reads and writes (#239)`, this worktree's HEAD), tree
clean at dispatch (`git status --porcelain` empty).

Shape references: `docs/plans/2026-11-22-rf-w4-09-api-reads-writes-split-plan.md` and its evidence
record (same-stage GDD shape: dispatch gates, measured basis, decisions with alternatives,
lesson-32 audit, mutation table, residual risks).

Host note: every `pnpm` invocation runs as `mise x pnpm@12.17.0 -- pnpm --trust-lockfile
--config.verify-deps-before-run=false ...` (the workspace pins pnpm 12.17.0, as W4-08/W4-09/W4-11
recorded). The host's npm feed does not carry three versions the merged `#240` dependency bump
pinned — `knip@6.46.1`, `@vitest/coverage-v8@5.1.1`, `msw@3.0.7` (feed 404, absent from the local
store; the store holds `6.45.1`/`5.1.0`/`3.0.5`) — so the one-time `pnpm install` ran with a
temporary `overrides` block in `pnpm-workspace.yaml` pinning exactly those three to the
store-available versions; both `pnpm-workspace.yaml` and `pnpm-lock.yaml` were restored
immediately after the install (`git status --porcelain` empty, re-checked), and none of the three
is a gate tool (see §12).

## Dispatch gates (checked at dispatch, base `7f34d40d`)

| gate | state at dispatch |
| --- | --- |
| W3-12 (`#190`) UI primitives/resource-dialog collapse merged | yes (`c0784952`); `resources.tsx`/`editors.tsx` are at their post-W3-12 line state |
| W3-13 (`#204`) one HTTP boundary for command posts merged | yes (`f1fe1e59`); `parseRefusal`/`MutationRefused` construction lives in `api.ts` |
| W2-15 (`#164`) merged | yes (`c6b2c352`); `connections.tsx`/`resources.tsx`/`assignment.tsx`/`detail.tsx` are at the rebased line state |
| W4-11 (`#220`) test harness merged | yes (`e84973a7`); `app/portal-double.ts` + `app/test-harness.tsx` own the DOM suites' fixtures |
| W4-08 (`#232`) ChatPage decomposition merged | yes (`d5b14b85`) — **it moved both chat predicates out of `chat.tsx`** (see §3) |
| W4-09 (`#239`) merged | yes (`7f34d40d`, this HEAD); `api.ts` is at its post-split line state |
| owned files untouched by open work | `git status --porcelain` empty; no open PR touches the owned set |

## 2. Re-derivation of the owned set against HEAD

The contract re-derived its coordinates against `origin/main b89e14d7`; three sites moved after
that revision, and one contract item is already satisfied. Re-derived per site:

1. **`chat.tsx` holds no predicate at HEAD.** W4-08 split ChatPage: `definitiveRefusal` now lives
   in `frontend/src/features/chat/use-chat-scope-state.ts:92-95` (used at `:172`) and
   `definitiveReadRefusal` in `frontend/src/features/chat/use-older-page-loading.ts:7-10` (used at
   `:39`). Those two files are the owned sites for the contract's `chat.tsx:708-711`/`:731-734`;
   `chat.tsx` itself (255 L) needs no edit. Both hook suites are part of the foreground gate.
2. **A fifth copy of the same predicate.** `frontend/src/features/work/control.tsx:463-467`
   defines `definitiveRefusal` with a byte-identical body to the chat hook's, used at `:203`,
   `:220`, `:237`, `:255` (via `clearActionId`) and `:473` (via `failurePatch`). Clause 1's
   observable condition — "exactly one implementation of each predicate ... exists under `src/`"
   — is not satisfiable while that copy stands, so `control.tsx` is added to the owned set for
   that one deletion plus the import line and the six call sites that reference it. Findings F3
   enumerated four predicates but not this copy; the contract's own grep criterion is the
   authority here (decision P8).
3. **Two more inline staleness sites.** Beyond the contract's `resources.tsx` pair,
   `connections.tsx:138` and `assignment.tsx:245` each derive `stale` as
   `failure instanceof ServerRefusal && failure.code === "stale"` — the same policy as the
   contract's `resources.tsx:390` site (same file pair the contract already owns for its five
   wrapper calls and its call sites). Both migrate to the module predicate (decision P9); no new
   policy is invented and no set is merged.
4. **The Elixir citation blocks are already gone.** The contract's comment-only item (the two
   `portal_*_test.exs` comment blocks citing `write-policy.ts` lines) was removed by `ae4b345f`
   ("apply the comment policy to refactor-program comments", `#223`): `grep -rn "features/"` and
   `grep -rn "write-policy"` over `control/` at HEAD find no citation, so the item is vacuous at
   HEAD. No Elixir file is edited. (One stale citation survives outside the owned set:
   `docs/TODO.md:645` names `write-policy.ts:251` with a line number the deletion in §5 shifts;
   see §12.)
5. `frontend/src/features/planning/detail.tsx`... n/a: the contract's `goals/detail.tsx` is
   unchanged in ownership (`outcomeUnknown` `:41-43`, `refusal.stale` `:262`).

Owned set as delivered (grouped by edit class): new `frontend/src/services/refusals.ts` and
`refusals.test.ts`; predicate call sites `use-chat-scope-state.ts`, `use-older-page-loading.ts`,
`goals/detail.tsx`, `work/control.tsx`; vocabulary owner `planning/write-policy.ts`; guidance call
sites `connections/resources.tsx`, `connections/connections.tsx`, `work/assignment.tsx`;
import-line-only `planning/editors.tsx`, `projects/projects.tsx`, `projects/work-surface.tsx`;
the fold's old home `services/api.ts` (the `refusalNotice` block only); tests
`services/api.test.ts`, `features/planning/planning.test.tsx` (import lines only) and the new
`services/refusals.test.ts`; docs (`docs/plans/...`, `docs/evidence/...`). Oracles never edited:
`connections.test.tsx`, `goal-attention.test.tsx`, `chat.test.tsx`, `work/assignment.test.tsx`,
`work/detail.test.tsx`, `work/control.test.tsx`, `chat/presentation.test.ts`,
`work/receipts.test.ts` (see §11).

## measured basis (base `7f34d40d`)

| artifact | measurement |
| --- | --- |
| `services/api.ts` | 1,003 L; the fold `refusalNotice(error, guidance)` at `:999-1003` (doc comment `:991-998`); `ownValue` `:987-989`; `errorMessage` `:971-977` stay |
| `chat/use-chat-scope-state.ts` | 252 L; `definitiveRefusal` `:92-95`, call site `:172` |
| `chat/use-older-page-loading.ts` | 62 L; `definitiveReadRefusal` `:7-10`, call site `:39` |
| `goals/detail.tsx` | 762 L; `outcomeUnknown` `:41-43`, call sites `:167`, `:224`; `refusal.stale` `:262` |
| `planning/write-policy.ts` | 388 L; `FailureMode` `:106`, `failureMode` `:108-119`, `refusalGuidance` `:122-131` (8 codes), `refusalNotice` `:133-137` |
| `connections/resources.tsx` | 688 L; `refusalGuidance` `:31-41` (9 codes), wrapper `:47`, call sites `:290`, `:636`, `:652`; inline `stale` `:471`, `terminal` `:472-473` |
| `connections/connections.tsx` | 496 L; wrapper import `:20`, five calls `:177`, `:178`, `:219`, `:234`, `:284`; inline `stale` `:138` |
| `work/assignment.tsx` | 334 L; `assignmentGuidance` `:82-86` (4 codes), two calls `:324-325`; inline `stale` `:245` |
| `work/control.tsx` | 478 L; `definitiveRefusal` `:463-467`, five call sites (`:203`, `:220`, `:237`, `:255`, `:473`) |
| guidance tables | 21 (code, table) steps over 12 distinct codes; every code shared by two surfaces carries a **different** step (`stale`, `state_conflict`, `not_found`, `provider_owned`, `validation_failed`, `forbidden`) |
| oracles (unmodified) | `chat/presentation.test.ts` 559 L / 39 cases; `work/receipts.test.ts` 236 L / 14; `connections.test.tsx` 1,516 L / 57; `goal-attention.test.tsx` 1,321 L / 29; `chat.test.tsx` 2,062 L / 48; `work/assignment.test.tsx` 376 L; `work/detail.test.tsx` 128 L; `work/control.test.tsx` 13 cases |
| owned tests | `planning.test.tsx` 790 L (import line `:8` + the `failureMode`/`refusalNotice` unit cases `:162`, `:216-217`, `:220`, `:783`); `services/api.test.ts` 631 L (import lines `:2-3`, refusal cases `:344-349`, `failureMode` case `:150`) |
| guidance-step witnesses (base) | 8 of 21 steps are asserted by a surface suite: `state_conflict` `connections.test.tsx:1371`, `not_found` `:1354`, `not_connected` `:1247`, `capability_not_granted` `:1266`, `provider_unauthorized` `:611`, `provider_failure` `:1282`, `goal_authority_required` `planning.test.tsx:783`, `invalid_request` `planning.test.tsx:221`; the other 13 (resources `stale`/`forbidden`/`validation_failed`; planning `stale`/`state_conflict`/`not_found`/`provider_owned`/`validation_failed`/`forbidden`; all four assignment steps) have no asserting case — `refusals.test.ts` pins all 21 joined outputs (§7) |

Baseline gate rows (typecheck, lint, focused, full, `git diff --check`) are recorded in the
evidence record §6; the same commands are re-run on the final tree.

## Module shape

```ts
// frontend/src/services/refusals.ts
import { Conflict, Forbidden, Network, ServerRefusal, UnexpectedResponse, Validation, errorMessage, ownValue } from "./api";
import { MutationRefused } from "./commands";

const staleServerCodes: readonly string[] = ["stale"];
const terminalStateServerCodes: readonly string[] = ["stale", "state_conflict", "not_found"];

export function isDefinitive(error: unknown): boolean            // ← chat hook's definitiveRefusal, control.tsx's copy
export function isDefinitiveReadRefusal(error: unknown): boolean // ← the read-cursor policy, kept distinct
export function isUnknownOutcome(error: unknown): boolean        // ← goals/detail.tsx's outcomeUnknown
export function isStaleRefusal(error: unknown): boolean          // ← MutationRefused.stale ∪ ServerRefusal{stale}
export function isTerminalStateRefusal(error: unknown): boolean  // ← resources.tsx's terminal set
type FailureMode = "fields" | "retryable" | "authoritative";
export function failureMode(error: unknown): FailureMode         // ← verbatim from write-policy.ts
export const resourceRefusalGuidance: Record<string, string>;    // ← resources.tsx's map (9 codes)
export const planningRefusalGuidance: Record<string, string>;    // ← write-policy.ts's map (8 codes)
export const assignmentRefusalGuidance: Record<string, string>;  // ← assignment.tsx's map (4 codes)
export function refusalNotice(error: unknown, guidance: Record<string, string> = planningRefusalGuidance): string;
```

`refusalNotice`'s body is the fold's, moved from `api.ts` byte-identically (`ServerRefusal`-only
arm, `ownValue` step lookup, `${message} (${code})${step ? \` — ${step}\` : ""}`); `api.ts` keeps
no shim and no re-export. Import direction is one-way (`refusals.ts` → `api.ts` → `commands.ts`),
so no cycle exists. `write-policy.ts` keeps its domain logic and re-imports nothing: every
importer of `failureMode`/`refusalNotice` moves its import line in this change (clean cutover).

## 4. Base → module predicate table (each row must be *identical*, not approximate)

| base site | base policy text | module export | identical |
| --- | --- | --- | --- |
| `chat/use-chat-scope-state.ts:92` `definitiveRefusal` (call site `:172`) | `Conflict \|\| Validation \|\| Forbidden \|\| Unsupported \|\| (ServerRefusal && 400 ≤ status < 500)` | `isDefinitive` | yes — same five classes, same half-open status range |
| `work/control.tsx:463` `definitiveRefusal` (5 call sites) | byte-identical to the row above | `isDefinitive` | yes |
| `chat/use-older-page-loading.ts:7` `definitiveReadRefusal` (call site `:39`) | `Conflict \|\| Validation \|\| (UnexpectedResponse && status === 400)` | `isDefinitiveReadRefusal` | yes — kept distinct from `isDefinitive`; a 404/500 read failure stays non-definitive |
| `goals/detail.tsx:41` `outcomeUnknown` (call sites `:167`, `:224`) | `Network \|\| UnexpectedResponse` | `isUnknownOutcome` | yes |
| `services/commands.ts:105-107` `MutationRefused.stale` (call site `detail.tsx:262`) | `status === 409 && (code === "stale" \|\| code === "stale_revision")` | `isStaleRefusal` (class arm delegates to the getter; the getter stays in `commands.ts`) | yes — the module consumes the getter, never re-implements the class rule |
| `resources.tsx:471` `stale`; `connections.tsx:138`; `assignment.tsx:245` | `ServerRefusal && code === "stale"`, **no status condition** | `isStaleRefusal` (ServerRefusal arm over `staleServerCodes`) | yes — the arm is status-insensitive exactly as base; `stale_revision` is *not* in this set (base never had it) |
| `resources.tsx:472-473` `terminal` | `ServerRefusal && code ∈ {stale, state_conflict, not_found}`, no status condition | `isTerminalStateRefusal` over `terminalStateServerCodes` | yes — recorded as its own named policy; the two sets are never merged |
| `write-policy.ts:108-119` `failureMode` | 5-way table: 5xx → `retryable`; `validation_failed` → `fields`; any other `ServerRefusal` → `authoritative`; `Network` → `retryable`; `UnexpectedResponse` → `transient ? retryable : authoritative`; default → `authoritative` | `failureMode` | yes — moved verbatim incl. the `FailureMode` union |

The module's own tests pin every row of that table as a truth-table case (with the base site named
in the case), so a "yes" is a runnable claim, not a reading.

## 5. Guidance tables (three named tables + one fold; no base map)

The contract's binding rule — no base map unless every surface that can reach a code carries the
identical step — holds by inspection of the base maps: the six shared codes each carry different
steps per surface (e.g. `stale`: resources "Reload to see the server's current values.", planning
"The server's current state is shown instead.", assignment "The work item changed elsewhere; …").
So the three tables stay, with their entries byte-identical to base:

| code | `resourceRefusalGuidance` (9) | `planningRefusalGuidance` (8) | `assignmentRefusalGuidance` (4) |
| --- | --- | --- | --- |
| `stale` | Reload to see the server's current values. | The server's current state is shown instead. | The work item changed elsewhere; close the editor and reopen it from the re-read detail. |
| `state_conflict` | Reload and resolve the changed state. | The server's current state is shown instead. | This item or its project is in a state that refuses the write; re-read both, then retry. |
| `not_found` | Reload the page to see the server's current state. | The server's current state is shown instead. | — |
| `forbidden` | Grant the required provider permissions, then test the connection again. | Refresh the page and sign in again if the session was renewed elsewhere. | — |
| `not_connected` | Bind the resource to a connection first. | — | — |
| `capability_not_granted` | Grant the connection the capability this resource kind needs, then try again. | — | — |
| `provider_unauthorized` | Reauthenticate the provider session on this server, then test the connection again. | — | — |
| `provider_failure` | Check provider availability, then test the connection again. | — | — |
| `validation_failed` | The server rejected this submission; re-read the current values and try again. | Correct the highlighted fields and submit again. | Correct the highlighted fields and submit again. |
| `provider_owned` | — | Fields owned by the external work tracker are edited in that tracker. | The provider keeps the title, description and priority; only the assignment fields here are editable. |
| `goal_authority_required` | — | Goal-managed work is changed through Goal admission, not this surface. | — |
| `invalid_request` | — | This request cannot be corrected here; the editor closes and the server's current state is shown. | — |

Callers after cutover: `resources.tsx` (3 calls) and `connections.tsx` (5 calls) pass
`resourceRefusalGuidance`; `assignment.tsx` (2 calls) passes `assignmentRefusalGuidance`;
`editors.tsx` (4 calls), `projects.tsx` (1), `work-surface.tsx` (1) and `planning.test.tsx` (1)
keep one-argument calls served by the `planningRefusalGuidance` default (decision P4).
`refusals.test.ts` pins the joined output of **every** entry of every table — all 21 steps —
plus the fallback rows (an unlisted code, a prototype-named code, a non-`ServerRefusal`, a bare
`Error`), closing the 13-step witness gap.

## 6. Edge-case semantics the contract demands

1. **Frozen bytes (D1).** Message text, the `(code)` suffix, the `` ` — ` `` joiner, the step
   text of all 21 entries, the per-dialog appended sentences (`connections.tsx`'s "This connection
   changed elsewhere …", resources' two appended sentences, assignment's "Close and reopen the
   editor …") and the fallback text of `errorMessage` are unchanged; the pinned suites pass
   unmodified. The **one** authorized hygiene delta (listed base→HEAD in the evidence record):
   planning's fold used a plain index (`refusalGuidance[error.code]`) where the shared fold uses
   `ownValue`, so a `ServerRefusal` whose code is a prototype member (`constructor`, `toString`)
   rendered `Object.prototype`'s member text on planning surfaces in base and contributes no step
   at HEAD — the same prototype-safe behavior the resource/assignment folds and `api.test.ts:349`
   already pinned.
2. **Distinct policies stay distinct.** `isDefinitive` ≠ `isDefinitiveReadRefusal` (a 404
   `UnexpectedResponse` is definitive for neither; a `Forbidden` is definitive only for the
   write policy); `isStaleRefusal`'s ServerRefusal arm ≠ `isTerminalStateRefusal`'s set; the
   `MutationRefused` arm is the getter's own 409 rule. No status condition is added to a base
   predicate that had none, and none is dropped.
3. **No silent merge, no silent default.** The three tables are separate exports; the fold's
   default parameter is the planning table only (decision P4); `api.ts` keeps no shim, and no
   `export { ... } from "./refusals"` re-export exists anywhere.
4. **One owner per rule.** `MutationRefused.stale`'s rule stays in `commands.ts` (class member,
   not owned); the module delegates. Every other rule text appears exactly once under `src/`.
5. **Comments.** The owner policy applies to every touched hunk: `failureMode`'s
   three-line docstring and its in-body `definitiveRefusal` reference are dropped (unconsumed
   boilerplate / stale name), the fold's docstring moves with the function minus the
   surface-specific wording, and the three maps' "The server owns the reason; …" header line and
   per-map "this surface" comments are dropped as redundant with the export names. No comment is
   added except the mutant-naming test titles the mutation-proof rule requires. Every deletion is
   listed base→HEAD in the evidence record.

## 7. Control-flow audit (lesson 32)

The evidence record carries the base-vs-HEAD table: for each of the five module predicates, the
three guidance tables' lookup sites and every call site, each early return, `instanceof`
short-circuit, default clause and ternary in base beside its HEAD equivalent, with a preserved
yes/no per row. The rows that need a stated equivalence rather than a mechanical one:

- `isDefinitive`'s `ServerRefusal && 400 ≤ status < 500` arm: base `definitiveRefusal` had the
  same half-open range; the `Unsupported` class inside the `||` chain is unexercised by the chat
  and control suites (clause 3's "unexercised branch" example), so the module truth table pins it.
- `isStaleRefusal`'s ServerRefusal arm keeps base's status-insensitivity; the class arm keeps the
  getter's 409 rule; the terminal set keeps its own three codes.
- `refusalNotice`'s `!(error instanceof ServerRefusal)` early return, `ownValue` lookup and
  ternary joiner are byte-identical to `api.ts`'s; the default parameter (planning table) is the
  only shape change, and it cannot change a rendered sentence: every one-argument caller in base
  resolved to the planning map.
- `connections.tsx:234`, `resources.tsx:290`, `assignment.tsx:324` etc. keep their ternaries and
  appended sentences verbatim, only the predicate calls behind them move.

## 8. Decisions (and the alternatives)

| # | decision | rationale / alternative |
| --- | --- | --- |
| P1 | `isStaleRefusal`'s ServerRefusal arm is `code === "stale"` only, with **no** status condition and **without** `stale_revision` | base's three inline sites (resources/connections/assignment) had exactly that text; adding `status === 409` would classify a 500/`stale` refusal differently than its base site (clause 3's failure control) and adding `stale_revision` would widen a set base never had. The `stale_revision` code is pinned on the `MutationRefused` arm instead, as mutation (d) demands. Alternative (rejected): the contract sentence "a named code-set + status rule for `ServerRefusal`" read as a *new* 409 rule — observably different from every base site, so rejected |
| P2 | the terminal set is a second named export, `isTerminalStateRefusal`, not a parameter flag or a merged set | the contract requires the two `resources.tsx` sets "recorded as two named policies, not silently merged"; a boolean flag at the call site would be a second convention |
| P3 | `refusalNotice` keeps the two-argument signature with a `planningRefusalGuidance` default | the contract authorizes the default "only where a current caller needs one"; `editors.tsx`/`projects.tsx`/`work-surface.tsx`/`planning.test.tsx` call one-argument, and their base resolution was the planning map, so their call text stays byte-identical (D1 lean). Alternative: pass `planningRefusalGuidance` explicitly at those four call sites — more imports and diff lines, no behavior difference |
| P4 | the module takes its own copy of `ownValue`/`errorMessage` by import from `api.ts` | the contract fixes the direction (lower layer, no cycle); `ownValue`'s prototype safety is the frozen lookup and stays the fold's only step lookup |
| P5 | `FailureMode` stays a non-exported type alias in the module | base did not export it and nothing imports it (knip reports unused exports); the function's return type stays nameable inside the module |
| P6 | `control.tsx` joins the owned set (predicate deletion + import + five call sites) | clause 1's "exactly one implementation of each predicate" cannot hold with the identical copy in place; the edit is mechanical and the file's own suite is in the foreground gate |
| P7 | `connections.tsx:138` and `assignment.tsx:245` join the owned edits | the same `ServerRefusal && code === "stale"` policy; both files are already owned for their guidance call sites, and clause 1's grep proof is about the policy, not the file name. `connections.tsx:138`'s variable keeps its name and rendering |
| P8 | `chat.tsx` is not edited | W4-08 moved both predicates into the two hooks; `chat.tsx` contains no refusal classification at HEAD (§2.1) |
| P9 | no `docs/design/*.md` amendment | no design document names the refusal vocabulary or the guidance maps (`grep -rn "refusalNotice\|failureMode\|refusalGuidance"` over `docs/design/` finds nothing); the module is code hygiene inside the frozen presentation contract |
| P10 | the docs/TODO.md citation at `:645` is left for its own slice | TODO edits are limited to closing entries; the entry's cited line number drifts with the deletion, which its owning cleanup revalidates anyway — recorded as a residual in §12 and the evidence record |

## 9. Evidence plan

All commands foreground and serialized; no measurement harness beside a suite (the sequencing
rule). Baseline rows at `7f34d40d`, head rows on the final tree.

| # | command | proves |
| --- | --- | --- |
| 1 | `pnpm --filter @symmetry/frontend typecheck` | the cutover compiles; no dangling import of the deleted symbols |
| 2 | `pnpm --filter @symmetry/frontend lint` (`--max-warnings 229`) | ratchet held; measured count ≤ base |
| 3 | `pnpm --filter @symmetry/frontend exec vitest run src/services/refusals.test.ts src/features/chat/presentation.test.ts src/features/work/receipts.test.ts src/features/planning/planning.test.tsx src/services/api.test.ts` | the contract's focused oracle set + the new module suite |
| 4 | `… exec vitest run src/features/chat/chat.test.tsx src/features/chat/chat-hooks.test.tsx src/features/connections/connections.test.tsx src/features/goals/goal-attention.test.tsx src/features/work/assignment.test.tsx src/features/work/detail.test.tsx src/features/work/control.test.tsx src/app/router.test.tsx` | the callers' packages (conductor decision: chat.tsx, detail.tsx, resources.tsx, assignment.tsx, connections.tsx, editors.tsx, projects.tsx, work-surface.tsx) + the E8 oracles, unmodified. `chat-hooks.test.tsx` owns the two moved chat predicates; `router.test.tsx` renders `ProjectsPage` (and `ProjectWorkSurface`) — the projects-package pair the decision names |
| 4b | `… exec vitest run src/features/projects/filters.test.ts src/features/projects/saved-views.test.ts src/features/runtimes/runtimes.test.tsx src/components/primitives.test.tsx` | the remaining touched-package neighbourhood suites, unmodified |
| 5 | `pnpm --filter @symmetry/frontend test` | full suite; head count = base + the new module cases |
| 6 | `git diff --check` | whitespace/EOF hygiene |
| 7 | definition-count greps | clause 1: zero local implementations of the four predicates, three maps, two wrappers and the `api.ts` fold; every import line points at `services/refusals.ts` |
| 8 | base-vs-HEAD LOC counts + `git diff --stat` | D2's report-only delta and the exact edit set |
| 9 | mutation flips (below), each applied alone, run red, reverted | clause 1's last condition; each pin kills its named mutant |
| 10 | DLP greps over added lines (profile-path pattern, backslash-u) | the program-wide rule |
| 11 | `git status --porcelain` at the end | the edit set is exactly the owned files; oracles untouched |

Mutation list (each applied alone, run, reverted; the six contract mutants are the floor):

| mutant | flip | expected red |
| --- | --- | --- |
| a | delete `Forbidden` from `isDefinitive` | `refusals.test.ts` truth-table case `isDefinitive(Forbidden) === true` |
| b | widen `isDefinitiveReadRefusal` to any 4xx (drop the `status === 400` condition) | `refusals.test.ts` case `isDefinitiveReadRefusal(UnexpectedResponse 404) === false` |
| c | make `isUnknownOutcome` false for `UnexpectedResponse` | `goal-attention.test.tsx`'s unknown-outcome retry case (plus the module truth table) |
| d | drop `stale` from `MutationRefused.stale`'s code set (judging-only edit in `commands.ts`) | `goal-attention.test.tsx`'s stale notice case + `refusals.test.ts`'s `isStaleRefusal(MutationRefused 409 stale_revision) === true` |
| e | return `authoritative` for `status >= 500` in `failureMode` | `planning.test.tsx:216` (`failureMode(ServerRefusal 500) === "retryable"`) |
| f1 | drop `capability_not_granted` from `resourceRefusalGuidance` | `connections.test.tsx:1266` |
| f2 | drop `goal_authority_required` from `planningRefusalGuidance` | `planning.test.tsx:783` |
| g | widen `isStaleRefusal`'s ServerRefusal arm to any status carrying `stale` (status-blind, i.e. drop a 409 condition if one were added) / or narrow it with a 409 rule | `refusals.test.ts`'s status-insensitivity pin (`ServerRefusal 500 "stale"` → true) |
| h | merge the two ServerRefusal staleness sets (make `isStaleRefusal`'s set the terminal set) | `refusals.test.ts`'s `isStaleRefusal(ServerRefusal 409 state_conflict) === false` and `isTerminalStateRefusal(409 stale_revision) === false` |
| i | replace the fold's `ownValue` step lookup with a plain index | `refusals.test.ts`'s prototype-named-code pin + `api.test.ts:349` |
| j | drop the `(code)` suffix or the ` — ` joiner | `refusals.test.ts`'s 21 joined-output pins + `api.test.ts:346` |

## 10. Out of scope (with reasons)

Contract clause 4 verbatim: the HTTP boundary and command POSTs (W3-13, merged); the
`Reads`/`Writes` split (W4-09, merged); ChatPage decomposition (W4-08, merged); the goal-settle
poll (W5-04); `ErrorState`/`StatusBadge`/dialog primitives (W3-12, merged); `commands.ts`'s class
members (the getter stays, only consumed); the test harness/MSW work (W4-11, merged); browser
specs. Additionally: no change to `attention.tsx`'s inline `MutationRefused` rendering (it renders
no guidance step and is not one of the four predicates); no change to `chat/presentation.ts`'s
`failureText` family (F4's subject); no `docs/design` amendment (P9); no dependency change (the
temporary install overrides are reverted before any gate runs, §12); no new lint suppression, no
`--max-warnings` change.

## 11. Oracles that must pass unmodified

`chat/presentation.test.ts`, `work/receipts.test.ts`, `connections.test.tsx`,
`goal-attention.test.tsx`, `chat.test.tsx`, `work/assignment.test.tsx`, `work/detail.test.tsx`,
`work/control.test.tsx`. `planning.test.tsx` and `services/api.test.ts` are edited on their import
lines only (the cutover forces it); their assertion text stays byte-identical and is re-checked in
the evidence record with a base-vs-HEAD assertion-text diff (the only differences must be the
import line and the module-suite additions).

## 12. Residual risks / known limits

- **Loaded-host full-suite runs.** The gate command is green on the frozen bytes (23 files / 568
  tests, 29.93 s), but repeated re-runs while other slice worktrees compile on this shared host
  (`rustc`, `postgres`, Docker; 84–142 s per run) fail 1–4 DOM "content appears within the wait"
  cases, a different suite each run. The unchanged baseline tree fails identically under the same
  load (its own runs: 1–2 cases in chat/connections/goal-attention), both trees' failing files pass
  in isolation, and the class is the one `docs/plans/2026-11-21-rf-w1-10-frontend-eslint-plan.md`
  already recorded. The full run record is the evidence record's §6.1.
- The three host-unavailable dependency versions (§12) mean the local install is not the
  lockfile's exact tree; the gates that matter (vitest/jsdom/eslint/typescript) are at lockfile
  versions, and neither `knip`, `msw` nor `@vitest/coverage-v8` participates in a gate. The
  browser acceptance suite is CI-only and not run locally, as W4-09 recorded.
- `isDefinitive`'s `Unsupported` branch and `isStaleRefusal`'s status-insensitivity have no
  production call path in the pinned suites; the module truth table is their only pin (clause 3's
  unexercised-branch rule), which the evidence record states as a limit rather than a coverage
  claim.
- The `docs/TODO.md:645` citation drift (P10) is disclosed, not fixed.
- `refusals.ts` exports five predicates + three tables; `isTerminalStateRefusal` has one call
  site today. Both are contract-mandated; knip's unused-export report is not a gate and is
  unchanged by design (recorded report-only).
