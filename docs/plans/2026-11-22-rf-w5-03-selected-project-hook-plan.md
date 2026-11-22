# rf-w5-03-selected-project-hook plan — one `useSelectedProject()` owner, fed by the router match

Contract: `goal-rf-w5-03-selected-project-hook.md` (GDD, 2026-11-21, amended 2026-11-21 and
2026-11-22), binding with conductor decisions D1 (byte-identical surfaces), D2 (dLOC
report-only), the dispatch gates, lesson 32 (control-flow preservation), arch-decision-2 R12 and
the owner comment policy (2026-11-22). Base revision `42afb56c`
(`refactor(frontend): own read-cache keys in one module (#245)`, this worktree's dispatch HEAD;
`git status --porcelain -uall` empty at dispatch).

Shape references: `docs/plans/2026-11-22-rf-a5-frontend-query-keys-plan.md` and
`docs/evidence/rf-w4-10-refusal-classification-2026-11-22.md` (same-stage GDD shape: dispatch
gates, measured basis, decisions with alternatives, lesson-32 audit, mutation table, residual
risks); `docs/plans/2026-11-22-rf-w4-11-frontend-test-harness-plan.md` (the MSW double and its
request log, the pin this slice's request-set clause rides on).

Host note: the workspace pins pnpm 12.17.0 (`package.json`’s packageManager) while the host PATH
carries 12.21.0, which the version check rejects; every `pnpm` invocation below runs the
mise-installed pnpm 12.17.0 executable (the pinned version; the A5 plan's `mise x pnpm@12.17.0`
route) from the workspace cwd, plus `--config.verify-deps-before-run=false`: without it pnpm runs
a pre-script dependency verification that attempts a registry install step and stalls ~6 minutes
on this host. The host's npm feed does
not carry `knip@6.46.1`, `@vitest/coverage-v8@5.1.1`, `msw@3.0.7`; the install ran
`--no-frozen-lockfile` with the A5 plan's temporary `overrides` block in
`pnpm-workspace.yaml` pinning those three to `6.45.1`/`5.1.0`/`3.0.5`, and both that file and
`pnpm-lock.yaml` were restored immediately after (re-checked `git status --porcelain`, §13).

## 1. Dispatch gates (checked at dispatch, base `42afb56c`)

| gate | state at dispatch |
| --- | --- |
| #164 (W2-15) merge-first | merged (`c6b2c352`, per the W4-11 plan's dispatch table) |
| W3-12 (`#190`) UI primitives merged | merged; `resources.tsx`/`attention.tsx` are at their post-W3-12 line state |
| W4-11 (`#220`) one portal double merged | merged; `app/portal-double.ts` owns `portal.requests` (the request log this slice pins) |
| W4-08 (`#232`) ChatPage decomposition merged | merged; `chat.tsx` holds the `route.useSearch()` + `chatScopeParams` identity |
| W4-09 (`#239`) merged | merged |
| W4-10 (`#241`) merged | merged (`c5d0a4a8`) |
| A5 (`#245`) merged | merged (`42afb56c`, this HEAD; `read-cache.ts` owns the keys, `application.ts` the option builders) |
| listed pages at their post-dependency line state | yes, re-derived per site in §2 |
| owned files touched by other work | none: `git status --porcelain -uall` empty at dispatch |

## 2. Re-derivation of the owned set against HEAD (`42afb56c`)

The contract's coordinates were re-derived against `origin/main 6a815c0f` and spot-re-verified at
`c9ed23b4`; between that revision and this base, A5 moved only import/key lines. Re-derived
coordinates of every site this slice edits (all verified by `nl -ba` at the base):

| site | base coordinate | what it holds |
| --- | --- | --- |
| `app/shell.tsx:68-81` | contract `:67-80` | `useProjectScope`: six `useMatch` selectors + the work-detail read; fallback return at `:80` |
| `app/shell.tsx:83-89` | `:84-88` | `useProjectDirectory` (hook consumer + workspace reads) |
| `app/shell.tsx:91-99` | `:91-` | `NavigationLinks` (`:92` hook consumer) |
| `app/shell.tsx:102-124` | `:105` | `ProjectPicker`: `:103` directory, `:106` hook, `:109` highlight expression |
| `app/shell.tsx:125-176` | `:126` | `CommandPalette`: `:126` directory, `:127` hook |
| `features/attention/attention.tsx:14-15` | `:10-40` block | `search.projectId ?? null` for the workspace/attention reads (`:21`, `:24`, `:33`), the NewGoalButton scope and `selectedProjectName` (`:38`) |
| `features/runtimes/runtimes.tsx:69,79-87` | `:68-80` | `getRouteApi("/runtimes")` + `route.useSearch()` scope and `scoped: projectId !== undefined` |
| `features/connections/connections.tsx:302-303` | `:298-330` | workspace read scope + `selectedProjectId` served fallback |
| `features/work/detail.tsx:61,72-75` | `:58-90` | `search.projectId` claim, `foreignScope`, `scopedProjectId` projection hop |
| `features/chat/chat.tsx:113-117` | `:86` post W4-08 | `route.useSearch()` → `chatScopeParams` identity; `directory` read scope |
| `features/projects/projects.tsx:78,84` | `:75` | `useParams({ from: "/projects/$projectId" })` + the read it scopes |

Contract claims that no longer hold verbatim and are carried into the PR as re-derived: (a) the
shell hook already existed as `useProjectScope` — this slice's deliverable is the context-fed hook
and the page conversion, not a first hook; (b) `router.test.tsx` is 1,215 L at this base (not
1,328) and its executed count is re-measured in §4; (c) the cited line numbers shifted again under
A5/`#146` — the table above is the dispatch-time measurement; (d) `#164` leads the serialization
(already merged here).

## 3. Measured basis (base `42afb56c`)

| artifact | measurement |
| --- | --- |
| `app/shell.tsx` | 281 L; `useProjectScope` `:68-81` (6 selectors `:70-79`, chain return `:80`), 4 consumers (`:85`, `:92`, `:106`, `:127`) |
| `workDetailOptions` subscriptions at base | 3: shell `useProjectScope` `shell.tsx:75`; `work/detail.tsx:69` (page), `:238` (`WorkDetailBody`) — same key `workItemReadKey(workItemId)`, one request |
| workspace read sites in the owned set | shell `:86` scope, `:87` recovery; attention `:21`; runtimes `:80`; connections `:302`; chat `:117`; projects `:84`; work detail `:81` (disabled cache probe), `:207` (background), `:137` (editor fetch) |
| page chains | attention `search.projectId ?? null` (`:15`) + served fallback (`:38`, NewGoalButton); runtimes search-only (`:79`, `:84`); connections `search.projectId ?? workspace.data?.selected_project_id ?? null` (`:303`); work detail `itemProjectId ?? queryProjectId` (`:75`) + `foreignScope` (`:74`) + body args (`:187`, `:193`); chat `chatScopeParams(search).projectId` (`:114`, `:117`); projects route param (`:78`, `:84`) |
| `router.test.tsx` | 1,215 L, 41 declared cases (40 `it(` + one `it.each` over 2 paths = 42 executed); no request-log assertion today (the log is a capability of the double) |
| `portal-double.ts` request log | `requests: PortalRequest[]` (`:89`), live array reference (`:432`), cleared per case by `resetPortalDouble` (`:450-455`) |
| page suites' mount path | every suite that renders an owned page goes through `renderRoute` → `createAppRouter` → `WorkspaceRoot` (chat, connections, runtimes, goal-attention, planning, assignment, control, work/detail; `chat-hooks.test.tsx` renders hooks only) |

## 4. Deliverable shape

1. **`frontend/src/app/selected-project.tsx`** (new; the contract's example name): the context,
   the provider that computes the chain from the router match, and `useSelectedProject()`.
   The chain expression moves **verbatim** (byte-identical selectors, member order and `??`
   sequence) out of `shell.tsx:68-81`; the only edit inside it is that the `useQuery` call and the
   `useRouteContext` read now live in the provider component.
2. **`frontend/src/app/shell.tsx`**: `useProjectScope` deleted; `WorkspaceRoot` wraps its
   signed-in tree in `SelectedProjectProvider`; `useProjectDirectory`, `NavigationLinks`,
   `ProjectPicker` and `CommandPalette` read `useSelectedProject()`; the `useMatch` and
   `workDetailOptions` imports leave the file (they are unused after the move).
3. **Pages consume the hook for the scope read only** (§5 table): each page deletes the
   `search.projectId ?? …` / route-param / served-projection derivation that scoped its reads and
   calls the hook instead; page-local **second hops** (attention's `first.data.selected_project_id`,
   connections' `workspace.data.selected_project_id`), the work URL claim, the chat scope identity
   (`chatScopeParams`) and the projects route param's navigation identity stay where they are.
4. **`frontend/src/app/selected-project.test.tsx`** (new): the hook's resolution and request-set
   cases, each naming its mutant (§11).
5. Docs: this plan and `docs/evidence/rf-w5-03-selected-project-hook-2026-11-22.md` (base→HEAD
   tables, deleted definitions, request-log diff, mutant flips, gate rows). `docs/TODO.md` /
   `docs/archive/CLOSED-TODO.md`: no open entry covers this slice (`grep` for
   `useSelectedProject`, `project picker`, `W5-03`, `scope hook` finds none) — unchanged.

### Module shape

```tsx
// frontend/src/app/selected-project.tsx
const SelectedProjectContext = createContext<string | undefined>(undefined);

function useRouterSelectedProject() { /* the moved chain */ }
export function SelectedProjectProvider({ children }: { children: ReactNode }) { … }
export function useSelectedProject(): string | undefined { return useContext(SelectedProjectContext); }
```

The value is the chain itself (`string | undefined`), not a served-fallback-resolved selection:
the pages' second hops stay page-local, so no page gains a fallback it did not have at base. A
`{ selectedProjectId, scopeSource }` shape (the contract's alternative) buys nothing here: on a
single matched route the chain collapses to that route's own hops, so the value alone carries
every per-surface decision this slice must preserve (see §6, D3).

## 5. Base → HEAD per-surface chain table (order-preserving conversion)

| surface | base chain | HEAD call | preserved |
| --- | --- | --- | --- |
| shell directory / navigation / picker / palette | `useProjectScope()` (6 selectors + served work read) | provider value (same expression, moved verbatim) | yes |
| picker highlight | `projectId ?? (isAttention \|\| isConnections ? directory.data?.selected_project_id ?? "" : "")` | unchanged, fed by the hook | yes |
| attention reads/`NewGoalButton`/`selectedProjectName` | `search.projectId ?? null`, `projectId ?? first?.data?.selected_project_id` | `selectedProjectId` (normalized once at the declaration), `selectedProjectId ?? first?.data?.selected_project_id` | yes |
| runtimes read + `scoped` | `route.useSearch().projectId` → `?? null`, `!== undefined` | `selectedProjectId ?? null`, `!== undefined` | yes (search-only, no served fallback) |
| connections read/key/`projectRef`/detail | `search.projectId ?? null`; `search.projectId ?? workspace.data?.selected_project_id ?? null` | `selectedProjectId ?? null`; `selectedProjectId ?? workspace.data?.selected_project_id ?? null` | yes (served fallback kept) |
| work detail scope | `itemProjectId ?? queryProjectId` (`:75`) | `selectedProjectId` (served projection ?? URL claim) | yes |
| work detail `foreignScope` + canonical redirect | `queryProjectId !== undefined && itemProjectId !== undefined && queryProjectId !== itemProjectId` → `navigate({ projectId: itemProjectId, replace: true })` | unchanged (page-local claim + served item) | yes |
| work detail body args / background / close | `queryProjectId ?? itemProjectId ?? null`, `queryProjectId ?? null`, `foreignScope ? undefined : itemProjectId`, `itemProjectId ?? queryProjectId` | unchanged except `close()` uses the hook value (same expression) | yes |
| chat directory read | `chatScopeParams(search).projectId` | `selectedProjectId ?? null` | yes (URL-scope identity stays with `chatScopeParams`) |
| projects read | `useParams({ from: "/projects/$projectId" })` | `selectedProjectId ?? null` | yes (the route param is the chain's first hop; navigation identity keeps `useParams`) |

Order preservation is by construction for every row: the moved chain expression is byte-identical,
and the page conversions substitute the hook value for the *same* sub-expression (each page's base
chain is the route-local slice of the shell chain). §7 lists what that means observably.

## 6. Deleted-definition list (base line → HEAD call)

| base definition | HEAD |
| --- | --- |
| `shell.tsx:68-81` `useProjectScope` (chain) | `selected-project.tsx` provider value |
| `shell.tsx:85`, `:92`, `:106`, `:127` `useProjectScope()` | `useSelectedProject()` |
| `attention.tsx:15` `const projectId = search.projectId ?? null` | `const selectedProjectId = useSelectedProject() ?? null` |
| `runtimes.tsx:69,79` `getRouteApi("/runtimes")` + `route.useSearch()` | `const selectedProjectId = useSelectedProject()` |
| `connections.tsx:302-303` `search.projectId` scope + fallback | hook value + same fallback |
| `work/detail.tsx:75` `scopedProjectId = itemProjectId ?? queryProjectId` | `scopedProjectId = useSelectedProject()` |
| `work/detail.tsx:157` `close()`'s `itemProjectId ?? queryProjectId` | `selectedProjectId` |
| `chat.tsx:117` `workspaceOptions(api, params.projectId)` | `workspaceOptions(api, selectedProjectId ?? null)` |
| `projects.tsx:84` `workspaceOptions(api, projectId)` | `workspaceOptions(api, selectedProjectId ?? null)` |
| `shell.tsx:9` `workDetailOptions` import, `:3` `useMatch` import | deleted (unused) |
| `runtimes.tsx:8` `getRouteApi` import, `:69` route const, `:79` `route.useSearch()` | deleted (unused) |

## 7. Control-flow audit (lesson 32) — what is observable and what is not

Every base branch, `undefined` member and enabled/disabled gate in §5's rows is preserved by the
table; the evidence record carries the per-selector audit. Two rows need a stated equivalence
rather than a mechanical one:

1. **Chain order is largely unobservable by construction.** TanStack Router matches one route at a
   time, so at most one of the six search hops (attention/chat/connections/runtimes) and at most
   one of the two path hops (projects param / work item) is ever defined; on the work route hops
   2-3 (`servedWork.data?.work_item.project_id` then `workScope.claimedProjectId`) are the only
   pair that can both resolve. The order between them is observable only inside the foreign-scope
   window (`work/detail.tsx:109-113` rewrites the claim to the served project), which is why the
   contract's mutant (a) is "drop the served hop" (testable) rather than "reorder hops"
   (unobservable). The moved expression keeps the order byte-identically; the evidence record
   states this instead of claiming a reorder mutant.
2. **The picker's empty-string branch** (`:108`-`110`, `""` when no scope and not
   attention/connections) has no router-suite case; the new hook test pins it on `/portal/runtimes`
   without a search member (mutant: "unify with the served selection"), and the pre-refactor
   expression is preserved verbatim in the picker.

`work/detail.tsx`'s `scopedProjectId` conversion is safe because the provider's served hop reads
the same query key (`workItemReadKey(workItemId)`) the page reads at `:69`, so `selectedProjectId
=== itemProjectId ?? queryProjectId` in every render; `detailBody(queryProjectId ?? itemProjectId
?? null, …)` and `detailBody(queryProjectId ?? null)` stay untouched (the body re-reads the same
item) and `foreignScope`/`backgroundProjectId` keep both base hops.

## 8. Request-set pin (contract change item 1)

Base and HEAD issue the same queries on every route: the provider's single `workDetailOptions`
subscription replaces the shell's (same key, same `enabled: workScope !== undefined` gate; the page
and body keep their own subscriptions, so the work route still issues exactly one GET
`/portal/api/work-items/:id`), and the provider adds no read of its own. Evidence: a per-case dump
of `portal.requests` for `router.test.tsx` captured through a throwaway vitest config that adds one
setup file overriding `portal.requests.push` (the double's live log array) and snapshotting the
case's method+path sequence once its pending reads settle; the base dumps come from a scratch
checkout of `42afb56c`, the HEAD dump from the final tree. Realized result (evidence record §6):
**per-case request key sets identical in all 42 cases across four runs on both trees**; concurrent
fetches log in scheduling order, so the per-case *sequence* also differs between two runs of the
unchanged base tree (2/42), and one case repeats an existing key run-dependently, at base too. The
mutant-(d) capture (18/42 cases gain a new key) proves the comparison detects an added query. The
scaffold files are deleted before the final tree; their exact contents and the commands are in the
evidence record. The new hook test adds an independent in-suite pin (counts of the workspace/work-item
requests per route) whose mutant is the extra-query flip.

## 9. Decisions (and the alternatives)

| # | decision | rationale / alternative |
| --- | --- | --- |
| D1 | module `frontend/src/app/selected-project.tsx` with `SelectedProjectProvider` + `useSelectedProject()` | the contract's example; `app/` owns the shell and the option builders. Alternative: keep the provider inside `shell.tsx` — the contract asks for a sibling, and the shell file would then export two unrelated surfaces |
| D2 | chain expression moved byte-identically, including the two `why` lines about the served projection | §7.1: the order is unobservable; byte identity is the strongest available proof. The comment policy keeps those 2-3 lines (line-level authority hazard: the projection is read from the page's key) |
| D3 | hook value is the chain (`string \| undefined`), not `{ selectedProjectId, scopeSource }` | no consumer needs the source: runtimes' `scoped` check is `!== undefined` on a route whose only hop is its own search member; the pages' second hops stay page-local. A source field would add an unused branch (no-unrequested-flexibility) |
| D4 | the picker, navigation, palette and `useProjectDirectory` keep their bodies; only the chain's source changes | they are already correct; the contract freezes their rendered/ARIA/navigation surfaces (D1) |
| D5 | pages keep reading their own route search where it is *identity*, not scope: chat's `chatScopeParams`, projects' `useParams` for links, work detail's `search.projectId` for `foreignScope`/`setSearch`/body args, attention's `unavailable`, connections' `connectionId` | contract change item 2 lists these as deliberate asymmetries; `use-chat-scope-state.ts` is not in the owned set, so the chat identity cannot move |
| D6 | the hook test file mounts through the app-router mount path (its local `renderScope` fixture) and asserts existing observables (picker value, nav hrefs, request log) | every page suite already mounts this way; a test-local router would need its own route tree for `useMatch({ from })`. No new selector or data attribute is added (D1) |
| D7 | `getRouteApi("/runtimes")` is deleted from `runtimes.tsx` | after the conversion the route object has no remaining use; leaving it would be dead weight |
| D8 | no `docs/design/*.md` amendment | no design document names the scope chain or `useProjectScope` (`grep -rn "useProjectScope\|project scope" docs/design/` finds only unrelated prose) |
| D9 | `docs/TODO.md` unchanged | no open entry covers this slice's scope (grep above); nothing is deferred |
| D10 | the contract file at its GDD path stays untouched | as the merged W5-01/W5-05/W5-06/W5-11 contracts did; the operator/harness owns ledger and status |

## 10. Evidence plan

All commands foreground and serialized; no measurement harness beside a suite (sequencing rule).
Every `pnpm` runs with the pinned launcher (§host note).

| # | command (frontend package cwd) | proves |
| --- | --- | --- |
| 1 | `pnpm --filter @symmetry/frontend typecheck` | the cutover compiles |
| 2 | `pnpm --filter @symmetry/frontend lint` (`--max-warnings 229`) | ratchet held; warning count ≤ base |
| 3 | `vitest run src/app/router.test.tsx` (oracle, unmodified) | 42 executed cases, identical names |
| 4 | the per-case request dump + settled DOM dump (base and HEAD) | §8: key-set diff empty; D1: per-case DOM identical after the three run-dependent values are normalised |
| 5 | `vitest run src/features/chat/chat.test.tsx src/features/goals/goal-attention.test.tsx src/app/selected-project.test.tsx` | the chat/attention scope suites + the new hook cases |
| 6 | `vitest run src/features/runtimes/runtimes.test.tsx src/features/connections/connections.test.tsx src/features/work/detail.test.tsx` | the other owned pages' suites (mutants (b), (c) redden here) |
| 7 | `pnpm --filter @symmetry/frontend test` | full suite; executed count = base + the new hook cases only |
| 8 | `git diff --check` | whitespace/EOF hygiene |
| 9 | mutation flips (§11), each applied alone, run red, reverted | the contract's judging mutants |
| 10 | DLP greps over added lines (profile-path pattern, backslash-u) | the program-wide rules |
| 11 | `git status --porcelain -uall` + `git diff --stat` at the end | the edit set is exactly the owned files; oracles untouched |

## 11. Mutation list (each applied alone to the final tree, run, reverted)

| mutant | flip | expected red |
| --- | --- | --- |
| M-a (contract a) | drop `servedWork.data?.work_item.project_id` from the chain | `selected-project.test.tsx` work-route case; `router.test.tsx` "keeps the desktop drawer and the supplied view state for a work URL without project scope" |
| M-b (contract b) | runtimes always passes a scope (`runtimesOptions(api, selectedProjectId ?? <fixed id>)`, `scoped: true`) | `runtimes.test.tsx` unscoped cases (rendered rows + absent unscoped request) |
| M-c (contract c) | drop connections' `?? workspace.data?.selected_project_id` | `connections.test.tsx` unscoped-page resource cases |
| M-d (contract d) | provider adds a second workspace read (`useQuery(workspaceOptions(api, null))`) | `selected-project.test.tsx` request-set case; the per-case request diff |
| M-claim | drop `workScope?.claimedProjectId` from the chain | `selected-project.test.tsx` held-detail case |
| M-search | drop the four search hops | `selected-project.test.tsx` per-route scope case |
| M-param | drop the `/projects/$projectId` param hop | `selected-project.test.tsx` project-route case |
| M-unify (hook) | chain gains a served-selection fallback (`?? directory.data?.selected_project_id`) | `selected-project.test.tsx` unscoped-runtimes case (picker/branch) |

## 12. Oracles that must pass unmodified

`src/app/router.test.tsx` (42 executed cases, names byte-identical) and every page suite whose
mount exercises the moved paths: `chat.test.tsx`, `chat-hooks.test.tsx`, `goal-attention.test.tsx`,
`connections.test.tsx`, `runtimes.test.tsx`, `detail.test.tsx`, `assignment.test.tsx`,
`control.test.tsx`, `planning.test.tsx`, plus the full run. No assertion in any existing test file
is edited; `vitest.config.ts`, `test-setup.ts`, `portal-double.ts` and `eslint.config.js` are not
edited.

## 13. Out of scope (with reasons)

Contract clause 4 verbatim: refusal classification (W4-10, merged); the API read/write split
(W4-09, merged); the goal-settle poll and wait budgets (W5-04); the jsdom/browser split (W5-08);
route code splitting (W6-07); UI primitives (`components/*`, W3-12); browser fixtures (W4-14).
Additionally: the chat scope *identity* (`use-chat-scope-state.ts`, not owned); `presentation.ts` /
`scopes.ts`, `services/api.ts`, `app/application.ts`, the browser tree, `vitest.config.ts`
(W5-04), `eslint.config.js` (W1-10); the contract file at its GDD path; `docs/goals`' status
table (operator-owned). The measurement scar tissue: no benchmark harness is run; dLOC is
report-only (D2).

## 14. Residual risks

- The request-log dump depends on a throwaway setup file that patches `portal.requests.push`;
  both scaffold files are deleted before the final tree and the dump files live outside the repo.
  Realized: the per-case *sequence* is not deterministic at base (2/42 cases differ between two runs
  of the unchanged base tree, and one case repeats an existing key run-dependently), so the
  comparison is recorded as per-case key-set equality with the base-vs-base control, not as a
  sequence diff (§8, evidence record §6).
- The provider re-renders its children when the chain value changes (a navigation or the served
  projection arriving); the base re-rendered only the four shell consumers. No test observes it,
  and the pages subscribe to the same queries they read.
- The temporary install overrides are host-side only; both YAML files are restored and the final
  `git status` proves it.
