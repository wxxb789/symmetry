# rf-w5-03-selected-project-hook — evidence record (2026-11-22)

Subject: one `useSelectedProject()` owner in `frontend/src/app/selected-project.tsx` — the context,
the provider that computes the project-scope chain from the router match, and the hook — with the
shell's four consumers and the seven listed pages reading it instead of re-deriving scope. Contract:
`goal-rf-w5-03-selected-project-hook.md` (GDD, 2026-11-21/22, binding conductor decisions D1
byte-identical surfaces, D2 dLOC report-only, the dispatch gates, lesson 32 control-flow
preservation, the R12/A5 gate and the owner comment policy of 2026-11-22). Base revision
`42afb56c` (`refactor(frontend): own read-cache keys in one module (#245)`, this worktree's dispatch
HEAD, `git status --porcelain -uall` empty). Plan:
`docs/plans/2026-11-22-rf-w5-03-selected-project-hook-plan.md`. Judging artifact (contract clause 2,
"answer.md"): this record plus the implement-stage report (per-surface chain table, deleted
definitions, the unmodified `router.test.tsx` run, the per-case request/DOM captures, the mutation
flips and the gate rows).

## 1. Deliverable shape (files, counts)

| file | status | lines (base → head) | note |
| --- | --- | ---: | --- |
| `frontend/src/app/selected-project.tsx` | new | 0 → 30 | context + `SelectedProjectProvider` + `useSelectedProject()`; the chain expression moved verbatim from `shell.tsx:68-81` |
| `frontend/src/app/selected-project.test.tsx` | new | 0 → 131 | 6 cases, each naming its mutant |
| `frontend/src/app/shell.tsx` | edited | 281 → 267 | `useProjectScope` deleted; the signed-in tree wrapped in the provider; 4 consumers read the hook |
| `frontend/src/features/attention/attention.tsx` | edited | 202 → 203 | `search.projectId` scope reads → hook |
| `frontend/src/features/runtimes/runtimes.tsx` | edited | 99 → 98 | `getRouteApi`/`useSearch` scope → hook (`scoped` stays `!== undefined`) |
| `frontend/src/features/connections/connections.tsx` | edited | 497 → 499 | scope read → hook; served fallback kept |
| `frontend/src/features/work/detail.tsx` | edited | 457 → 457 | `scopedProjectId` → hook; `close()`'s chain → hook |
| `frontend/src/features/chat/chat.tsx` | edited | 255 → 257 | `directory` read scope → hook; `chatScopeParams` identity untouched |
| `frontend/src/features/projects/projects.tsx` | edited | 145 → 147 | project-route read scope → hook; `useParams` keeps navigation identity |
| `docs/plans/2026-11-22-rf-w5-03-selected-project-hook-plan.md` | new | 0 → 278 | the plan |
| `docs/evidence/rf-w5-03-selected-project-hook-2026-11-22.md` | new | 0 → this file | this record |

Tracked production source: +38/−46 lines (net −8) plus the 30-line new module = **net +22 dLOC**
against the plan's report-only −60..−100 estimate: the chain *moved* instead of being deleted
(13 lines), the four shell consumers keep their bodies, and each page swaps one chain expression for
one hook call. D2 makes this report-only; recorded here and §11.

Not edited: `frontend/src/app/router.test.tsx`, `app/test-harness.tsx`, `app/portal-double.ts`,
`app/application.ts`, `app/read-cache.ts`, `services/api.ts`, `features/chat/use-chat-scope-state.ts`
(not owned), `features/connections/resources.tsx`, `presentation.ts`/`scopes.ts`,
`test-setup.ts`, `vitest.config.ts`, `eslint.config.js`, `presentation`/`scopes`, `browser/**`,
`contracts/**`, `docs/TODO.md` (no open entry covers this slice: grep for `useSelectedProject`,
`scope hook`, `project picker`, `W5-03` finds none) and every oracle suite.

## 2. Re-derived coordinates at the base

The contract's coordinates (`origin/main 6a815c0f`, spot-re-verified `c9ed23b4`) drifted again under
A5; the dispatch-time measurement is in the plan §2. Re-derived facts carried into the PR: the shell
hook existed as `useProjectScope` (the deliverable is the context-fed hook + page conversion);
`router.test.tsx` is 1,215 L at this base and executes 42 cases from 41 declarations; `#164` leads
the serialization and is already merged here.

## 3. Base → HEAD per-surface chain table (contract clause 1)

| surface | base chain (site) | HEAD call | order preserved |
| --- | --- | --- | --- |
| shell directory / nav / picker / palette | `useProjectScope()` (`shell.tsx:68-81`): `useMatch("/projects/$projectId").params.projectId ?? servedWork.data?.work_item.project_id ?? workScope?.claimedProjectId ?? searchProjectId(attention) ?? chatProjectId ?? connectionsProjectId ?? runtimesProjectId` | provider value (the same expression moved to `selected-project.tsx:10-20`) | yes, byte-identical |
| picker highlight | `projectId ?? (isAttention \|\| isConnections ? directory.data?.selected_project_id ?? "" : "")` (`shell.tsx:108`) | unchanged, fed by the hook | yes |
| attention reads | `search.projectId ?? null` → `workspaceOptions`, `attentionOptions(…, null)`, older cursors (`attention.tsx:15,21,24,33`) | `selectedProjectId ?? null` | yes |
| attention goal button / name | `projectId ?? first?.data?.selected_project_id` (`:38,:55`) | `selectedProjectId ?? first?.data?.selected_project_id` | yes |
| runtimes read + empty state | `route.useSearch().projectId` → `?? null`, `scoped: projectId !== undefined` (`:79,:84`) | `selectedProjectId ?? null`, `scoped: selectedProjectId !== undefined` | yes (search-only; no served fallback) |
| connections read | `search.projectId ?? null` (`:302`) | `scopeProjectId ?? null` | yes |
| connections resolved scope | `search.projectId ?? workspace.data?.selected_project_id ?? null` (`:303`) | `scopeProjectId ?? workspace.data?.selected_project_id ?? null` | yes (served fallback kept) |
| work detail scope | `itemProjectId ?? queryProjectId` (`detail.tsx:75`) | `selectedProjectId` (served projection ?? URL claim) | yes |
| work detail `foreignScope` + canonical redirect | `queryProjectId !== undefined && itemProjectId !== undefined && queryProjectId !== itemProjectId` → `navigate({…, projectId: itemProjectId, replace: true})` (`:74,:108-112`) | unchanged | yes |
| work detail editor fetch / cached snapshot | `item.project_id ?? scopedProjectId`; `workspaceOptions(api, scopedProjectId ?? "")` (`:81,:135`) | `item.project_id ?? selectedProjectId`; same read | yes |
| work detail close | `itemProjectId ?? queryProjectId` (`:143`) | `selectedProjectId` | yes |
| work detail body args | `detailBody(queryProjectId ?? itemProjectId ?? null, …)` / `detailBody(queryProjectId ?? null)` (`:164,:170`) | unchanged (the body's own `item?.project_id ?? scopedProjectId` hop is unchanged) | yes |
| chat directory read | `chatScopeParams(search).projectId` (`:117`) | `selectedProjectId ?? null` | yes (the URL-scope identity stays with `chatScopeParams`) |
| projects read | `useParams({ from: "/projects/$projectId" })` (`:84`) | `selectedProjectId ?? null` | yes (the param is the chain's first hop) |

The chain expression itself is a verbatim move: `diff` of the base `shell.tsx:68-81` (with only the
function name substituted) against `selected-project.tsx:8-21` is **empty**.

## 4. Deleted-definition list (contract clause 2)

| base definition | HEAD |
| --- | --- |
| `shell.tsx:68-81` `useProjectScope` | `selected-project.tsx` provider value |
| `shell.tsx:85,92,106,127` `useProjectScope()` | `useSelectedProject()` |
| `shell.tsx:3` `useMatch` import, `:9` `workDetailOptions` import | deleted (unused) |
| `attention.tsx:15` `const projectId = search.projectId ?? null` | `const selectedProjectId = useSelectedProject()` |
| `runtimes.tsx:3` `getRouteApi` import, `:69` route const, `:79` `route.useSearch()` | `const selectedProjectId = useSelectedProject()` |
| `connections.tsx:302` `search.projectId ?? null` | `scopeProjectId ?? null` |
| `work/detail.tsx:75` `scopedProjectId = itemProjectId ?? queryProjectId` | `scopedProjectId` = hook value |
| `work/detail.tsx:143` `itemProjectId ?? queryProjectId` (close) | `selectedProjectId` |
| `chat.tsx:117` `workspaceOptions(api, params.projectId)` | `workspaceOptions(api, selectedProjectId ?? null)` |
| `projects.tsx:84` `workspaceOptions(api, projectId)` | `workspaceOptions(api, selectedProjectId ?? null)` |

## 5. Control-flow audit (lesson 32)

Per-selector audit of the moved chain: all six `useMatch` selectors, the `shouldThrow: false`
members, the `enabled: workScope !== undefined` gate on the work-detail read and the `??` sequence
are byte-identical; the only structural change is that the read and the `useRouteContext` call now
live in `SelectedProjectProvider`. Row-by-row:

| base branch / gate | HEAD equivalent | preserved |
| --- | --- | --- |
| `/projects/$projectId` param hop | same selector in the provider | yes |
| `/work/$workItemId` param + `search.projectId` claim | same selector | yes |
| work-detail read: `workDetailOptions(api, workScope?.workItemId ?? "")`, `enabled: workScope !== undefined` | same call/gate | yes |
| `servedWork.data?.work_item.project_id` hop | same | yes |
| four search hops (attention/chat/connections/runtimes) | same selectors | yes |
| picker `""` empty-string branch (`isAttention \|\| isConnections` guard) | unchanged in `shell.tsx:94-95` | yes |
| `useProjectDirectory` recovery read `enabled: !!projectId && query.isError` | unchanged, fed by the hook | yes |
| attention `expanded`/older-page cursors keyed by `projectId` | same `selectedProjectId ?? null` | yes |
| runtimes `scoped` gate (`projectId !== undefined`) | `selectedProjectId !== undefined` | yes |
| connections `projectRef` live-scope latch (`:368-369`) | unchanged, fed by the resolved value | yes |
| work detail `foreignScope` predicate and the canonical redirect (`:108-112`) | unchanged | yes |
| work detail body-scope args and `backgroundProjectId = foreignScope ? undefined : itemProjectId` | unchanged | yes |
| chat `params.complete`/`identity`/`key` from `chatScopeParams(search)` | unchanged | yes |
| projects `useParams` navigation identity (`Link`/`navigate` params, `project.id === projectId`) | unchanged | yes |

Two equivalences that a test cannot isolate are stated instead of asserted:

1. **Chain order is unobservable by construction.** The router matches one route at a time, so at
   most one search hop and one path hop are defined; hops 1 and 2-3 can never coexist. The only
   pair that can both resolve is the work route's served projection and URL claim, and the foreign
   scope window (`detail.tsx:108-112`) rewrites the claim to the served project, so only the
   transient window would separate them. That is why the contract's mutant (a) is "drop the served
   hop" (testable, §8 M-a) and not "reorder hops" (unobservable); the moved expression keeps the
   order byte-identically (§3).
2. **The provider's served hop equals the page's `scopedProjectId` in every render**, because both
   read `workItemReadKey(workItemId)` (the page's `detail.tsx:69` query and the provider's single
   subscription), so `selectedProjectId === itemProjectId ?? queryProjectId`; the page's own
   `detailBody` args therefore keep their base values.

## 6. Request-set pin (contract change item 1)

Method: a throwaway vitest config (`frontend/vitest.capture.config.ts`, 13 L) extending the project
config with one extra setup file (`frontend/capture-requests.ts`, 60 L) that overrides
`portal.requests.push` (the double's own live log array, `portal-double.ts:434`) and, per case,
records the `method + path` sequence and the settled `document.body.innerHTML`; both files were
deleted before the final tree (contents preserved with the run artifacts). Commands (frontend cwd,
pinned pnpm 12.17.0, `--config.verify-deps-before-run=false`):

```
vitest run --config vitest.capture.config.ts src/app/router.test.tsx      # captures both sides
```

Base side: `git archive 42afb56c` extracted to a scratch checkout, installed from the same store,
same scaffold, same command. Captures: base settled ×2, HEAD settled ×1, plus base/HEAD with the
pre-settle scaffold and the mutant-(d) run (§8).

| comparison | cases with a differing **key set** | cases with a differing **sequence** | cases with differing **DOM** |
| --- | ---: | ---: | ---: |
| base run 1 vs base run 2 (settled) | 0 | 2 | 0 |
| base run 1 vs HEAD (settled) | 0 | 11 | 0 |
| base/HEAD (unsettled, 5 runs pooled) | 0 | — | — |

Per-case request **key sets are identical in every one of the 42 cases across all five captures**
(4 runs, both trees) — the hook issues no query a page did not issue at base. What varies is not
the key set: (a) concurrent fetches log in a different order (2/42 between two *base* runs; 11/42
base-vs-HEAD), and (b) one case, `readable work views > counts an unrecognised structured outcome
value as reported output, not as an absence`, sometimes repeats an existing key (`GET
/portal/api/workspace`, and its `?project_id=` sibling) inside the capture window — it does so
between two runs of the unchanged base tree as well. The mutant-(d) run proves the comparison
detects an added query: with one extra provider read, 18/42 cases gain the new key
(`GET /portal/api/workspace`, absent from their base key sets). The literal clause ("diff =
empty") is therefore recorded as: **empty key-set diff for all 42 cases; the sequence diff is
non-empty at base too (2/42) because concurrent fetches are logged in scheduling order** — the
strongest true statement, with the base-vs-base control that shows the order noise is not caused by
this change.

Case names: `jq '.[].name'` of both captures diffs empty → the 42 executed names are byte-identical
base vs HEAD.

### 6.1 D1 rendered-surface comparison

The settled per-case DOM dumps are **identical in all 42 cases**, base-vs-base and base-vs-HEAD,
after normalising exactly three run-dependent rendered values: the shell's wall-clock freshness
(`data-refresh-issued-at`, `data-refresh-settled-at`, `refreshed <time>`, `shell.tsx:49-65`) and the
`it.each` CI fixtures' `new Date().toISOString()` timestamps (`router.test.tsx:695`). No other
difference exists: no text, ARIA/label, selector, data-attribute or navigation change. Without the
settle wait the same comparison flagged one transient state difference (a still-pending picker read
in the non-deterministic ordering case above); with pending reads settled the states are equal, and
the flag is recorded here rather than hidden.

## 7. Gates (all foreground, serialized; no measurement harness beside a suite)

| # | command (frontend cwd) | base | HEAD |
| --- | --- | --- | --- |
| 1 | `pnpm --filter @symmetry/frontend typecheck` | exit 0 | exit 0 |
| 2 | `pnpm --filter @symmetry/frontend lint` (`--max-warnings 229`) | exit 0, 218 warnings | exit 0, 218 warnings |
| 3 | `vitest run src/app/router.test.tsx` (oracle, unmodified) | 42 executed (capture runs) | exit 0, 42 executed / 42 passed (16.3 s) |
| 4 | `vitest run src/app/selected-project.test.tsx src/features/chat/chat.test.tsx src/features/goals/goal-attention.test.tsx` | n/a (new file) | exit 0, 3 files / 83 passed (43.4 s) |
| 5 | `vitest run src/features/runtimes/runtimes.test.tsx src/features/connections/connections.test.tsx src/features/work/detail.test.tsx` | n/a | exit 0, 3 files / 68 passed (58.6 s) |
| 6 | `pnpm --filter @symmetry/frontend test` | exit 0, 24 files / 605 tests | exit 0, 25 files / 611 tests (delta: +1 file, +6 tests — the new hook cases only) |
| 7 | `git diff --check` | clean | exit 0, clean |
| 8 | per-case request/DOM capture (§6) | see §6 | see §6 |

`--max-warnings` is untouched (`frontend/package.json` unmodified); the lint warning count did not
rise (218 = base). Base and HEAD gate commands ran the identical script through the pinned
pnpm 12.17.0 with `--config.verify-deps-before-run=false` (without it pnpm runs a pre-script
dependency verification that stalls ~6 minutes on this host and failed its registry fetch on
`@oxc-parser/binding-win32-ia32-msvc`; the script itself, `tsc --noEmit` / `eslint .`, is the gate).
Every HEAD row above was re-run on the settled final tree; the raw outputs (and the capture
artifacts of §6) are kept outside the repo in the throwaway dir `Q:/tmp/w5-03-capture/` as
`verify-typecheck.txt`, `verify-lint.txt`, `verify-router.txt`, `verify-scope.txt`,
`verify-pages.txt`, `verify-full.txt`, `verify-hook-post-revert.txt` and the settled capture JSONs.

## 8. Mutation flips (contract judging mutants; each applied alone, run, reverted, hash-verified)

Source hashes were recorded before the mutation set and re-verified after the last revert
(`sha256sum -c`: `selected-project.tsx` 10ce2db5…, `runtimes.tsx` 0ebb693c…, `connections.tsx`
f705ed63…, `work/detail.tsx` b5210143…, `shell.tsx` 891de027… — all OK).
Each mutant's raw output is kept in the same throwaway dir as `mutant-*.txt`; the M-d re-run on
the settled tree is `verify-mutant-d-hook.txt`.

| mutant | edit | run | result |
| --- | --- | --- | --- |
| M-a (contract a) | chain drops `servedWork.data?.work_item.project_id` | hook test + `router.test.tsx -t "keeps the desktop drawer …"` | hook: 1 failed/5 passed (the served-projection case); router: 1 failed/41 skipped (`toHaveValue(11111111-…)` at the scope-less work URL) |
| M-claim | chain drops `workScope?.claimedProjectId` | hook test | 1 failed/5 passed (the held-detail claim case) |
| M-search | chain drops the four search hops | hook test | 2 failed/4 passed (route-scope case; the scoped-route request case follows) |
| M-param | chain drops the `/projects/$projectId` param hop | hook test | 1 failed/5 passed (the project-route case) |
| M-unify (hook) | chain gains `?? workspaceOptions(api, null).data?.selected_project_id` | hook test + `runtimes.test.tsx` | hook: 2 failed/4 passed (the undefined-scope case); runtimes: 3 failed/1 passed (both unscoped cases + the scoped-rows case) |
| M-b (contract b) | `runtimes.tsx` always passes a scope (`selectedProjectId ?? "10000000-…-0001"`, `scoped: true`) | `runtimes.test.tsx` | 2 failed/2 passed (the two unscoped cases) |
| M-c (contract c) | `connections.tsx` drops `?? workspace.data?.selected_project_id` | `connections.test.tsx` | 21 failed/36 passed (every unscoped-page resource case) |
| M-d (contract d) | provider adds `useQuery(workspaceOptions(api, null))` | capture + hook test | capture: 18/42 cases gain the new key (`mutant-d.json`); hook: the exact-request-set case alone reddens, 1 failed/5 passed, exit 1 (`verify-mutant-d-hook.txt`); revert re-verified (`selected-project.tsx` back to 10ce2db5…, hook test 6 passed, exit 0) |

## 9. Oracles untouched

`router.test.tsx` (byte-identical to `42afb56c`, 42 executed cases, identical names), and every
other test file: `git status --porcelain -uall` at the end lists only the files of §1 plus the plan;
`git diff` contains no test-file hunk. The one new test file is additive. `vitest.config.ts`,
`test-setup.ts`, `portal-double.ts`, `eslint.config.js` and `pnpm-workspace.yaml`/`pnpm-lock.yaml`
are unmodified (the temporary install overrides were restored; the final `git status` proves it).

## 10. DLP / hygiene

Added lines contain no host profile path (`grep -inE "users/"` over the working diff and the new
files: no match) and no backslash-u sequence. `git diff --check` clean.

## 11. LOC delta (D2 — reported, never a gate)

Tracked production: +38/−46 (net −8); new module +30; new test +131; plan +278; this record. The
plan's −60..−100 estimate assumed page-side deletions larger than the chain move allows; the
delivered shape deletes one chain definition, one 2-line re-derivation per page and two imports,
and adds one hook call per consumer. Report-only per D2.

## 12. Residual risks and known limits

- The request-log pin is expressed as a key-set equality plus the base-vs-base order-noise control
  (§6). A stricter per-case *sequence* equality is not a property of the base tree; a future slice
  that wants it must first make the double's log order deterministic.
- The DOM comparison normalises the shell's freshness line and the `it.each` CI fixture timestamps
  (§6.1). A rendered difference confined to those expressions would not be caught; nothing else is
  normalised.
- The provider re-renders its children when the chain value changes (a navigation or the served
  projection arriving); the base re-rendered only the four shell consumers. No test observes it and
  the pages subscribe to the same queries they read, but the shell's render count on those
  transitions is higher than at base.
- `useSelectedProject()` outside the provider returns `undefined` silently (context default). Every
  route in this app renders under the shell's provider (all suites mount through
  `createAppRouter` → `WorkspaceRoot`), so the state is unreachable in production and in tests;
  recorded rather than defended with a throwing accessor.
- The two transient-state observations (pending picker read; the repeated existing key in one case)
  are recorded in §6/§6.1 as run-dependent, with the base-vs-base control.
