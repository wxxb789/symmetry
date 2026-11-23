# rf-w6-07-frontend-route-splitting — evidence record (2026-11-22)

Contract: `goal-rf-w6-07-frontend-route-splitting.md` (GDD, 2026-11-21, binding with the W6-reconcile
stale-chunk policy, the lesson-32 control-flow decision and the owner comment policy of 2026-11-22).
Plan: `docs/plans/2026-11-22-rf-w6-07-route-splitting-plan.md` (its §6 carries the conflict found
below, resolved by the conductor ruling of 2026-11-22). Base revision `6d55c575`
(`test(frontend): keep layout, scroll and focus invariants in the browser layer only (#254)`).
Files: `frontend/src/app/router.tsx` (modified, +10/-6), `frontend/src/app/route-chunks.ts` (new,
70 L at revision 4; +8/-2 of it is the storage-blocked guard, §13),
`frontend/src/app/route-chunks.test.tsx` (new, 146 L: 7 cases at implement, 4 more at the
2026-11-23 apply stage, 1 more at the r2 fix stage),
`frontend/src/features/connections/connections.tsx` (modified, +8/-6 at implement: the mount-timing
guard, §9a; +4/-2 more at the 2026-11-23 fix stage: the trailing-slash trim, §12; +5/-4 at the r2
fix stage: the route-id guard, §13),
`frontend/src/features/connections/connections.test.tsx` (modified at the fix stages, +15/-0 and
+15/-0: the trailing-slash case, §12, and the mixed-case case, §13), the plan, this record.

Verdict up front: every contract gate is green on the settled tree — typecheck, lint, the 42-case
router suite (byte-identical file), the 59-case connections suite (the pinned case from §9 among
them), the full suite (27 files / 632 tests), and the builds. §9 carries the conflict attempt 1
hit, the conductor ruling that resolved it and the corrected guard; §6 carries the mutation runs
(chunk-level, wrong lazy target, the mount-only guard revert, the three apply-stage reds, the
fix stage's trailing-slash row and the r2 fix stage's two rows); §12 carries the fix stage's
finding, probes and pin; §13 carries the r2 fix stage's findings; §10 carries the host-feed limits.

## 1. Dispatch re-derivation (base `6d55c575`)

| probe | contract's re-measure (base `0fa9b705`) | at dispatch (`6d55c575`) |
| --- | --- | --- |
| `frontend/src/app/router.tsx` | 80 L / 12 static imports / 9 route declarations | 80 L / 12 static imports / 9 route declarations (identical) |
| `features/goals/detail.tsx` | 761 L | 778 L |
| `features/chat/chat.tsx` | 734 L | 257 L (`chat-sections.tsx` 272 L carries the rest; W4-08 split it) |
| `features/connections/resources.tsx` | 675 L | 672 L |
| `features/planning/editors.tsx` | 583 L | 558 L |
| `features/work/detail.tsx` | 456 L | 457 L |
| `router.test.tsx` | 1,328 L / 41 declarations / 42 executed cases | 1,215 L / 41 declarations (40 `it` + one `it.each` with 2 paths) / 42 executed cases |
| `lazyRouteComponent` / `React.lazy` in `frontend/src` | 0 hits | 0 hits |
| `@tanstack/react-router` | 1.170.46 | 1.170.46 (`@tanstack/router-core` resolved at 1.171.39) |
| render gate as landed (W6-02 not merged) | the 7-spec render step + executed-set assertion | step "Render authenticated release shell on desktop and mobile" (`ci.yml:479-533`), "Assert the render gate's executed set" (`ci.yml:535-553`) |
| open PRs at dispatch | none touching `router.tsx` | #258, #257, #256 — no `frontend/**` path |

The base build reproduces the audit's F13 measurement: one chunk
`dist/assets/index-BMaoDiZV.js` **887.65 kB (257.27 kB gzip)** plus `index-Ccu45kBm.css`
35.35 kB (7.45 kB gzip), and the >500 kB chunk warning (audit: 886.2 kB / 255.9 kB gzip).

## 2. The lazy set as implemented

| route | module | base import site | lazy boundary |
| --- | --- | --- | --- |
| `/chat` | `features/chat/chat` → `ChatPage` | `router.tsx:4` (static) | `lazyRouteComponent(loadRouteChunk(() => import("../features/chat/chat")), "ChatPage")` |
| `/projects` | `features/projects/projects` → `ProjectsPage` | `router.tsx:7` | same shape, `"ProjectsPage"` |
| `/projects/$projectId` | `features/projects/projects` → `ProjectPage` | `router.tsx:7` | same shape, `"ProjectPage"` |
| `/work/$workItemId` | `features/work/detail` → `WorkDetailPage` | `router.tsx:9` | same shape, `"WorkDetailPage"` |
| `/connections` | `features/connections/connections` → `ConnectionsPage` (pulls `connections/resources`) | `router.tsx:5` | same shape, `"ConnectionsPage"` |
| `/goals/$goalId` | `features/goals/detail` → `GoalDetailPage` | `router.tsx:6` | same shape, `"GoalDetailPage"` |

Eager and byte-identical: the root route and `WorkspaceRoot`, the `/` redirect (`router.tsx:20`),
`/attention` (`AttentionPage`), `/runtimes` (`RuntimesPage`), `features/projects/filters` (both
`validateSearch` functions), `app/**`, `services/**`, `components/**`. The route declarations
themselves are untouched: `git diff frontend/src/app/router.tsx` changes the import block, adds six
`const <Name> = lazyRouteComponent(...)` lines and one `installStaleChunkReload();` call inside
`createAppRouter` — no route body line changes. `planning/editors` reaches the graph only through the
projects/work lazy chunks (measured: §4).

## 3. Route base→HEAD audit (control-flow preservation)

| route | component wiring | `validateSearch` / `beforeLoad` / `notFound` | rendered first paint, pinned by | preserved |
| --- | --- | --- | --- | --- |
| `/` | none (redirect) | `beforeLoad` → `redirect({ to: "/attention", search: {}, throw: true })` at `:20`, byte-identical | **no case names it** (jsdom or render gate): named here, not assumed | yes |
| `/attention` | eager `AttentionPage` (static import kept) | `validateSearch` `:24-27` unchanged | router.test.tsx 7 cases (":73 `keeps Attention project scope…`", ":192 returns keyboard focus…", ":208 shows a recoverable error…", ":223 removes private shell content…", ":232 keeps the operator's focus…", ":268 uses the current bootstrap CSRF token…", ":292 awaits the current session…") + the `it.each` path "/portal/attention?projectId=missing"; render gate `workspace-shell`, `portal-goal-attention`, `portal-cutover` | yes |
| `/chat` | lazy `ChatPage` | `validateSearch` `:39-43` unchanged, `ChatScope` filter `:32-33` unchanged | router.test.tsx `describe("chat route")`: ":944 `subscribes /chat and carries the shell's project scope in its navigation`", ":964 `returns a session expiry to the chat scope it was holding`"; `chat.test.tsx` (47 cases, real router through `renderRoute`); render gate `portal-chat`, `portal-cutover` | yes |
| `/projects` | lazy `ProjectsPage` | none | router.test.tsx ":49 `does not report unloaded project work as zero…`", ":208 `shows a recoverable error…`"; render gate `workspace-shell`, `portal-work-views`, `portal-cutover` | yes |
| `/projects/$projectId` | lazy `ProjectPage` | `validateSearch: workSearch` (filters `:7`) unchanged | router.test.tsx `readable work views` (16 cases over `:395-722`); render gate `portal-work-views`, `portal-cutover` | yes |
| `/work/$workItemId` | lazy `WorkDetailPage` | `validateSearch: workItemSearch` unchanged | router.test.tsx `readable work views` + `execution history boundaries` (6 cases) + `channel-join read epoch`; `work/detail.test.tsx`, `work/assignment.test.tsx`, `work/control.test.tsx` through `renderRoute`; render gate `portal-work-views`, `portal-execution-control`, `portal-goal-managed` | yes |
| `/connections` | lazy `ConnectionsPage` | `validateSearch` `:54-57` unchanged | **no router.test.tsx case** (named): `connections.test.tsx` (58 cases through the real router at the fix stage; the trailing-slash case is §12's), `selected-project.test.tsx`, render gate `portal-cutover` (`/portal/connections` at `:109-110`, `:356`) | yes |
| `/runtimes` | eager `RuntimesPage` (static import kept) | `validateSearch` `:65-66` unchanged | `runtimes.test.tsx`, `selected-project.test.tsx` (`:56-121`), render gate `portal-execution-control`, `portal-cutover` | yes |
| `/goals/$goalId` | lazy `GoalDetailPage` | none | **no router.test.tsx case** (named): `goal-attention.test.tsx` (29 cases) and `goal-settle.test.tsx` through `renderRoute`; render gate `portal-goal-attention`, `portal-cutover` | yes |

Deep links: `createMemoryHistory({ initialEntries })` + a cold mount per case in every suite above, so
each split route is exercised as a first-load deep link, not only as a navigation target.

## 4. Chunk tables (before / after, raw + gzip)

`pnpm --filter @symmetry/frontend build`, run on the same host tree (§10 dependency note).

Before (base `6d55c575`):

| chunk | raw | gzip |
| --- | --- | --- |
| `dist/assets/index-BMaoDiZV.js` (entry, single chunk) | 887.65 kB | 257.27 kB |
| `dist/assets/index-Ccu45kBm.css` | 35.35 kB | 7.45 kB |

After (settled tree — the fix-stage rebuild of 2026-11-23, after the §12 guard fix; the numbers
below are this build's own Vite report; Vite prints decimal kB):

| chunk | raw | gzip | role |
| --- | --- | --- | --- |
| `dist/assets/index-CQBS3fYd.js` | 321.32 kB | 101.84 kB | entry |
| `dist/assets/served-ZmW5x40A.js` | 400.76 kB | 116.11 kB | the entry's own static import (shared framework/services); preloaded by `index.html` |
| `dist/assets/chat-aGFvs6k_.js` | 27.11 kB | 8.68 kB | `/chat` |
| `dist/assets/connections-CfOHmnu2.js` | 34.44 kB | 9.13 kB | `/connections` (+ `connections/resources`) |
| `dist/assets/detail-DpAXdPwO.js` | 30.40 kB | 8.40 kB | `/goals/$goalId` |
| `dist/assets/detail-C6ujCpv3.js` | 35.08 kB | 10.39 kB | `/work/$workItemId` |
| `dist/assets/projects-BKBDczFV.js` | 6.23 kB | 2.23 kB | `/projects`, `/projects/$projectId` |
| `dist/assets/work-surface-zsE8WuKj.js` | 34.51 kB | 9.58 kB | shared by the projects/work routes: `projects/work-surface` + `planning/editors` |
| `dist/assets/refusals-C5yaP-QR.js` | 2.67 kB | 1.06 kB | shared by the lazy chunks |
| `dist/assets/receipts-DBf43aY4.js` | 2.96 kB | 1.21 kB | shared by the lazy chunks |
| `dist/assets/use-dialog-opener-BywfTvAD.js` | 0.42 kB | 0.28 kB | shared by the lazy chunks |
| `dist/assets/index-Ccu45kBm.css` | 35.35 kB | 7.45 kB | unchanged stylesheet |

The fix-stage rebuild vs attempt 2's table: sizes are identical except this route's own chunk
(`connections-*.js` 34.42 → 34.44 kB raw / 9.11 → 9.13 kB gzip — the trim import and the predicate
text) and the work-detail chunk's gzip figure (10.38 → 10.39 kB, a rounding artifact); the hash moved
for the entry and every chunk whose bytes or dynamic-import specifiers changed (eight assets);
`served`, `refusals`, `use-dialog-opener` and the stylesheet kept their hashes.

Attempt 1's build of the pre-ruling tree has the same sizes for every chunk except
`connections-*.js` (34.34 kB / 9.07 kB) and the same entry (321.32 kB / 101.84 kB); the asset
hashes cascade from the guard edit (routes whose chunks import `work-surface`, which imports
`connections/resources`, re-hash with it). A rebuild of the settled tree reproduced the same hashes
in the same run order (§7).

Initial script payload: `index.html` references the entry plus one `modulepreload` of
`served-ZmW5x40A.js` → **722,093 bytes raw / 217.95 kB gzip**, versus the base's single
887.65 kB / 257.27 kB (−165.6 kB raw, −39.3 kB gzip, −15.3 % gzip). The six route chunks and the
three small shared chunks are **not** in the initial payload.

Entry chunk: 887.65 → 321.32 kB raw (−63.8 %), 257.27 → 101.84 kB gzip (−60.4 %) with the heavy
features outside it. The delivered build is deterministic: the settled tree's rebuild reproduced the
same asset hashes (`index-Cfd4EG-e.js`, `served-ZmW5x40A.js`, …). The fix-stage build re-took the
entry and `served` byte counts (`321,329` and `400,764` bytes) and `index.html` still names one
`modulepreload`, so the initial-payload figure above is the fix-stage build's own.

Route→module→chunk check (each literal is unique inside `frontend/src`, so its chunk is a fact, not
an inference; re-run against the fix-stage `dist/`):

| module | distinctive literal | chunk containing it (settled build) |
| --- | --- | --- |
| `features/chat/chat` | `Open Chat from a project to hold a project conversation` | `chat-aGFvs6k_.js` |
| `features/connections/connections` | `Microsoft Entra ID` | `connections-CfOHmnu2.js` |
| `features/connections/resources` | `No agent profile` | `connections-CfOHmnu2.js` |
| `features/goals/detail` | `Approve the plan` | `detail-DpAXdPwO.js` |
| `features/work/detail` | `This project is archived. Restore it before editing its work.` | `detail-C6ujCpv3.js` |
| `features/projects/projects` | `Your workspace, organized by project.` | `projects-BKBDczFV.js` |
| `features/planning/editors` | `Project archived.` | `work-surface-zsE8WuKj.js` |
| `features/attention/attention` | `Refreshing attention` | `index-CQBS3fYd.js` (eager, by decision) |
| `features/runtimes/runtimes` | `Refreshing runtimes` | `index-CQBS3fYd.js` (eager, by decision) |

## 5. Stale-chunk policy: upstream mechanics and the pinned unit test

Upstream facts (installed: vite 8.3.8, `@tanstack/react-router` 1.170.46 → `@tanstack/router-core`
1.171.39):

- Vite's preload helper (`vite/dist/node/chunks/node.js:27046-27058`) dispatches a cancelable
  `vite:preloadError` event carrying `payload: Error` and then `if (!e.defaultPrevented) throw err`,
  so a **prevented** event makes the dynamic import resolve with `undefined`.
- `lazyRouteComponent` (`@tanstack/react-router/dist/esm/lazyRouteComponent.js`) already carries its
  own reload-once guard: for a module-not-found error — the three browser wordings, matched by
  `isModuleNotFoundError` (`@tanstack/router-core/dist/esm/utils.js:147-150`) — it writes
  `tanstack_router_reload:<error.message>` and reloads. That key is the *message*, not the build, and
  it fires only when the rejection reaches the component.

`frontend/src/app/route-chunks.ts` implements the decision:

- marker `portal:stale-chunk-reload`, value = `import.meta.url` of the entry chunk (the URL a
  deployment replaces); `claimReloadOnce` writes it and calls the injected `reload` exactly once per
  build;
- `installStaleChunkReload` (called from `createAppRouter`) registers one `vite:preloadError`
  listener per window: it examines `event.payload`, claims when this build has not reloaded yet and
  `preventDefault()`s only when it claimed;
- `loadRouteChunk(importer, reload)` wraps every route importer: a load-failure rejection claims (if
  unclaimed) and then throws a plain `Error` whose message is *not* a module-not-found wording, so
  the library's message-keyed guard cannot fire a second reload; the `undefined` module a prevented
  event leaves behind takes the same path (the listener already owns that failure's reload);
- non-load failures (a chunk that evaluates and throws) are rethrown untouched: error state, no
  reload;
- the terminal surface is the router's existing error state (`MatchesInner`'s `CatchBoundary` →
  `ErrorComponent`) — no `errorComponent`/`pendingComponent` is added anywhere; the policy's one
  new string is `staleChunkMessage`, and it renders only in the stale-chunk failure state (case 3);
- `reload` is an injected default because jsdom's `Location.reload` is unforgeable
  (`Object.getOwnPropertyDescriptor(window.location, "reload")` → `{ configurable: false,
  writable: false }`; `Object.defineProperty` throws `Cannot redefine property: reload`). The unit
  test drives that seam; the real reload is the render gate's business.

Unit test `frontend/src/app/route-chunks.test.tsx` (12 cases at revision 4, all green; 11/11 at the
fix stage, 491 ms in that stage's full-suite json run). Cases 8-11 are the 2026-11-23 apply stage's
additions and case 12 is §13's storage-blocked case; each is shown red under the mutant of the rule
it pins (§6 rows e-g and j):

| case | pins |
| --- | --- |
| `keeps the shell and the current route mounted while the next route's chunk is still loading` | navigation into a lazy route holds the previous page (no flash of missing content) |
| `holds the first paint of a cold lazy deep link until that route's chunk resolves` | a cold deep link renders shell and page in one commit, never a half-shell |
| `reloads the page once and shows the route's error state when a chunk load fails` | first failure: one reload, the marker written, the route's error state rendered |
| `shows the route's error state without another reload once this build has reloaded` | second failure: no loop, error state |
| `leaves a failing route chunk that is not a stale chunk to the error state without reloading` | an eval-time error is not a stale chunk: no reload, no marker, the real message shown |
| `claims one reload for a prevented vite:preloadError and leaves the repeat to the error state` | the event path: claimed + prevented once, then left alone |
| `shows the route's error state when a prevented preload left the chunk import empty` | the swallowed-`undefined` path and the shared claim across both entry points (exactly one reload) |
| `reloads for this build even when a marker an older build left is on record` | the marker is keyed by *this* build's id: an older build's marker never suppresses the reload this build needs |
| `claims one reload for a Firefox module-not-found wording` | the boundary claims before the library's message-keyed guard can, for the Firefox wording |
| `claims one reload for a Safari module-not-found wording` | same, for the Safari wording |
| `leaves a preload failure that is not a stale chunk unclaimed and unprevented` | the listener's payload filter: a preload failure that is not a load-failure wording (Vite's CSS-dep message) causes no reload |
| `shows the route's error state without reloading when the session storage is blocked` | the storage-blocked claim: no reload and no throw out of the listener or the boundary; the policy's stale-chunk error state is the terminal state |

## 6. Mutation runs (each applied alone, reverted, hash-verified)

| # | mutation | observed | proof |
| --- | --- | --- | --- |
| a | `/chat` back to the static import (`import { ChatPage } from "../features/chat/chat";`, lazy const deleted) | build exit 0 on the mutant tree: entry `index-D1ZKwl5i.js` **349.39 kB (gzip 109.90 kB)** vs the delivered 321.32 kB (101.84), **no `chat-*.js` chunk**, and the chat-only literal (`Open Chat from a project to hold a project conversation`) resolves to `index-D1ZKwl5i.js` — the entry regains the module | chunk-level mutant red |
| b | chat lazy target pointed at `../features/attention/attention` / `"AttentionPage"` | `vitest run src/app/router.test.tsx` exit 1: **1 of 42 red** — `chat route > subscribes /chat and carries the shell's project scope in its navigation`, `TestingLibraryElementError: Unable to find role="heading" and name "Chat"`, error surface `Invariant failed: Could not find an active match from "/attention"` (attempt 1 measured the same shape as 3 of 42; the two extra failures there did not reproduce in either attempt-2 run) | route-level mutant red |
| c | no test needed an added `await`: `git diff --exit-code frontend/src/app/router.test.tsx frontend/src/features/connections/connections.test.tsx` → 0 (both byte-identical) | — | mutant not applicable, recorded |
| d | the connections guard back to mount-only under lazy routes (`stillOnConnections = () => mounted.current`) | `vitest run src/features/connections/connections.test.tsx -t "does not pull the operator back"` exit 1: `1 failed, 56 skipped (57)`, `Expected: "/portal/projects"` / `Received: "/portal/connections"` at `connections.test.tsx:746` | route-guard mutant red |
| e | the build-id keying loosened to "any marker suppresses" (`claimReloadOnce`: `getItem(...) === buildId` → `!== null`), applied by the 2026-11-23 apply stage | `vitest run src/app/route-chunks.test.tsx -t "reloads for this build"` exit 1: `1 failed, 10 skipped (11)` — `expected "vi.fn()" to be called 1 times, but got 0 times`, failed at the count assertion (`waitFor`, 1,060 ms) | apply-stage red (case 8) |
| f | each non-Chrome wording broken in `chunkLoadFailurePrefixes`, one at a time (`"error loading dynamically imported module"` → `"…modulX"`; then `"Importing a module script failed"` → `"…failedX"`) | `-t "Firefox module-not-found"` exit 1: `1 failed, 10 skipped (11)`, injected `reload` 0 calls (the boundary rethrows and `lazyRouteComponent`'s message-keyed guard takes over); `-t "Safari module-not-found"` exit 1: same shape, failed at the count assertion (`waitFor`, 1,062 ms) | apply-stage red (cases 9, 10) |
| g | the listener's payload filter deleted (`if (!isChunkLoadFailure(event.payload)) return;`) | `vitest run src/app/route-chunks.test.tsx -t "unclaimed and unprevented"` exit 1: `1 failed, 10 skipped (11)`, `expected "vi.fn()" to not be called at all, but actually been called 1 times` | apply-stage red (case 11) |
| h | the guard's trailing-slash trim removed — `stillOnConnections` back to `router.latestLocation.pathname === "/connections"` (HEAD's guard bytes; applied by the 2026-11-23 fix stage) | `vitest run src/features/connections/connections.test.tsx -t "keeps this page's own continuations"` exit 1 on the pre-fix bytes: `Test Files 1 failed (1)`, `Tests 1 failed \| 57 skipped (58)`, `AssertionError: expected '' to contain '20000000-0000-4000-8000-000000000001'`; the same case is `1 passed \| 57 skipped (58)`, exit 0, on the fixed bytes (`connections.tsx` sha256 `315dc0e4…`) | fix-stage red (case 58, §12) |
| i | the r2 fix stage's branch: the guard's route-id compare back to the pathname's text (`stillOnConnections` = `trimPathRight(router.latestLocation.pathname).endsWith("/connections")`, case-sensitive; HEAD's predicate) | `vitest run src/features/connections/connections.test.tsx -t "spells the route in another case"` exit 1 on the mutant bytes: `Test Files 1 failed (1)`, `Tests 1 failed \| 58 skipped (59)`, `AssertionError: expected '' to contain '20000000-0000-4000-8000-000000000001'` at the selection-move assertion; the same case is `1 passed \| 58 skipped (59)`, exit 0, 8.30 s, on the fixed bytes (`connections.tsx` sha256 `9c17cc62…`; re-run green after the two-line comment trim, `be5e44b4…`, §13) | r2-fix-stage red (case 59, §13) |
| j | the r2 fix stage's branch: `claimReloadOnce` back to the unguarded get/set pair (the `try`/`catch` guard removed) | `vitest run src/app/route-chunks.test.tsx -t "session storage is blocked"` exit 1 on the mutant bytes: `Test Files 1 failed (1)`, `Tests 1 failed \| 11 skipped (12)`; the route's error state renders the raw `SecurityError` (`The operation is insecure.`) instead of the stale-chunk message, failing the stale-message assertion at `route-chunks.test.tsx:115`; the same case is `1 passed \| 11 skipped (12)`, exit 0, 2.69 s, on the fixed bytes (`route-chunks.ts` sha256 `58a7d89a…`) | r2-fix-stage red (case 12, §13) |

Every mutant was reverted and the revert checked with `sha256sum -c`:
`frontend/src/app/router.tsx: OK`, `frontend/src/app/route-chunks.ts: OK`,
`frontend/src/app/route-chunks.test.tsx: OK`, `frontend/src/app/router.test.tsx: OK`,
`frontend/src/features/connections/connections.test.tsx: OK`, and the guard mutant against the
pre-mutant `connections.tsx` hash
(`1c6761b7a13a3a4aa29f0a77099c823930051b2e55997bd0690ca926006ad492: OK`).
Rows e-g reverted the same way: `route-chunks.ts` back to
`96e682aa2abc390fd8908d90388c2baed2252fded9f61c2ed53468aaec3a82bf: OK` after each of the three
mutants.

Row h is the reverse pair: its red ran on HEAD's `connections.tsx` bytes
(`1c6761b7a13a3a4aa29f0a77099c823930051b2e55997bd0690ca926006ad492`) and the restored state is the
fixed file (`315dc0e4f97e8d0f218fe867c58b1ddd18777926cc844e4b393f3fc47debf4c4`); case 58's name and the
suite's counts are §7's. `connections.test.tsx` moved from `ac68f053…` (the pinned case's file) to
`8b1cf080…` by the one added case — the pinned §9 case's bytes are unchanged inside it.

Rows i/j are revision 4's: each ran on its mutant bytes and was reverted by restoring the fixed
file, checked with `sha256sum` against the pre-mutant hash — `connections.tsx`
`9c17cc62e95a24dc47dada68f471dcac64c5c6b0434fbf73e3fbbd3e23b5f223` and `route-chunks.ts`
`58a7d89a4b9c3b302953c496efacaa47d05040c70c5f560b6d742f1496c0f011` (identical before and after the
mutant in each case). The revision-4 pin files hash `59876c18…` (`connections.test.tsx`, case 59)
and `37ea8598…` (`route-chunks.test.tsx`, case 12). `connections.tsx` re-hashed to
`be5e44b4…` with §13's comment trim (no code change); its isolation case re-ran green on the new
bytes.

## 7. Gates (settled tree, attempt 2)

Host invocation (§10): `pnpm` is the pinned 12.17.0 binary, invoked as
`pnpm --trust-lockfile --config.verify-deps-before-run=false <script>`; the two flags skip the
lockfile policy sweep and the pre-run install against the host feed only — each script's payload is
the contract's command.

| gate | command (script payload) | result |
| --- | --- | --- |
| typecheck | `--filter @symmetry/frontend typecheck` | **exit 0**, `$ tsc --noEmit`, no diagnostics, 7.03 s |
| lint | `--filter @symmetry/frontend lint` | **exit 0**, `220 problems (0 errors, 220 warnings)`, 74.15 s; base is the same 220 → the `--max-warnings 229` cap is not raised |
| router suite (frozen) | `--filter @symmetry/frontend exec vitest run src/app/router.test.tsx` | **exit 0**, `Tests 42 passed (42)`, 13.05 s, run with `router.tsx` sha256 `c12df3df…` and `router.test.tsx` sha256 `9b46ec7d…` (the file is byte-identical to HEAD) |
| connections suite | `--filter @symmetry/frontend exec vitest run src/features/connections/connections.test.tsx` | **exit 0**, 58/58 at the fix stage (57/57 at attempt 2 and the apply stage, when the file was byte-identical to HEAD, `ac68f053…`); the fix stage's one added case is §12's, the pinned §9 case is byte-identical inside the file |
| full suite | `--filter @symmetry/frontend test` | **exit 0** on 7 of 8 invocations of the pre-apply settled bytes, `Test Files 27 passed (27)`, `Tests 625 passed (625)` each (33-90 s). One earlier invocation of the same bytes exited 1 with an unidentified failure (my own `tail` truncation dropped the case name) and never reproduced — see §11. The apply-stage and fix-stage refreshes (below) re-ran the gate at the current revision: 27 files / 629 tests, then 27 files / 630 tests, exit 0 |
| build before | `--filter @symmetry/frontend build` (attempt 1, base tree) | exit 0, single 887.65 kB chunk (§4) |
| build after | `--filter @symmetry/frontend build` (settled tree, run twice; again at the fix stage) | exit 0, the §4 table, 4.28 s and 2.07 s; the rebuild reproduced every asset hash. The fix-stage build (1.32 s) is §4's after-table, with `index.html` naming the entry plus one `modulepreload` |
| diff hygiene | `git diff --check` | exit 0 |
| DLP | the user-profile-path grep and the backslash-u grep over the changed/new files | 0 matches each; `scripts/ci/model-routing-record-sanitize.py --check --root-only` on this record: exit 0 |
| render gate (CI) | the PR's `control` job: the seven spec files + the executed-set assertion | CI-verified on the PR; the release-shell harness cannot run on this host (W5-08 §3.3 records the limitation) |

Full-suite composition at the fix-stage refresh (27 files / 630 tests; per-file test counts and
durations from `vitest run --reporter=json`, the same tree and config as the gate above):

| file | tests | duration |
| --- | --- | --- |
| `src/app/application.test.ts` | 90 | 79 ms |
| `src/app/legacy.test.ts` | 7 | 7 ms |
| `src/app/read-cache.test.ts` | 38 | 12 ms |
| `src/app/route-chunks.test.tsx` (new) | 11 | 470 ms |
| `src/app/router.test.tsx` | 42 | 9,098 ms |
| `src/app/selected-project.test.tsx` | 6 | 1,541 ms |
| `src/components/primitives.test.tsx` | 14 | 529 ms |
| `src/features/chat/chat-hooks.test.tsx` | 9 | 351 ms |
| `src/features/chat/chat.test.tsx` | 47 | 19,084 ms |
| `src/features/chat/presentation.test.ts` | 39 | 34 ms |
| `src/features/connections/connections.test.tsx` | 58 (all green) | 27,534 ms |
| `src/features/goals/goal-attention.test.tsx` | 29 | 15,722 ms |
| `src/features/goals/goal-settle.test.tsx` | 8 | 13,103 ms |
| `src/features/planning/planning.test.tsx` | 35 | 12,974 ms |
| `src/features/projects/filters.test.ts` | 9 | 13 ms |
| `src/features/projects/saved-views.test.ts` | 6 | 12 ms |
| `src/features/runtimes/presentation.test.ts` | 20 | 22 ms |
| `src/features/runtimes/runtimes.test.tsx` | 4 | 698 ms |
| `src/features/work/assignment.test.tsx` | 10 | 3,602 ms |
| `src/features/work/control.test.tsx` | 13 | 3,132 ms |
| `src/features/work/detail.test.tsx` | 7 | 1,398 ms |
| `src/features/work/presentation.test.ts` | 22 | 31 ms |
| `src/features/work/receipts.test.ts` | 14 | 14 ms |
| `src/services/api.test.ts` | 59 | 199 ms |
| `src/services/realtime.test.ts` | 19 | 60 ms |
| `src/services/refusals.test.ts` | 12 | 11 ms |
| `src/tool-cache-config.test.ts` | 2 | 516 ms |

Base for comparison: 26 files / 618 tests, green; final = 618 + 11 new + 1 fix-stage case = 630
(7 cases at implement, 4 added by the 2026-11-23 apply stage, 1 by the 2026-11-23 fix stage).

Apply-stage refresh (2026-11-23): the only source change is the helper's unit test (the four cases
above); `route-chunks.ts`, `router.tsx` and `router.test.tsx` are byte-identical (`96e682aa…`,
`c12df3df…`, `9b46ec7d…`), `connections.test.tsx` keeps `ac68f053…`, and `git status` lists only
three modified files (the unit test, the plan, this record). Gates re-run, all foreground and in
order:
typecheck exit 0; lint exit 0 (same 220 problems / 0 errors, cap unraised); router suite
`Tests 42 passed (42)`, 14.02 s, exit 0 at the same two hashes; `--filter @symmetry/frontend test`
exit 0, `Test Files 27 passed (27)`, `Tests 629 passed (629)`, 42.85 s; the same suite under
`--reporter=json` (source of the table above) 27 files / 629 tests / 0 failed, 32.4 s from first
start to last end; the standalone `route-chunks.test.tsx` run 11/11 in 4.41 s; `build` exit 0 and
reproduced attempt 2's asset table asset-for-asset (`index-DDkgw58W.js`, `served-ZmW5x40A.js`, …); the
apply-stage mutants are §6 rows e-g; `git diff --check` exit 0; both DLP greps 0 matches.

Fix-stage refresh (2026-11-23, after the §12 guard fix; the only source changes are the guard and
its one case): typecheck exit 0; lint exit 0 (same 220 problems / 0 errors, cap unraised); router
suite exit 0, `Tests 42 passed (42)` in 10.71 s; connections suite exit 0, `Tests 58 passed (58)` in
29.87 s; the helper's suite exit 0, 11/11 in 2.89 s; `--filter @symmetry/frontend test` exit 0,
`Test Files 27 passed (27)`, `Tests 630 passed (630)` in 44.74 s and green again on a second run;
the json run of the same suite (source of the table above) 27 files / 630 tests / 0 failed, exit 0;
`build` exit 0 in 1.32 s and the §4 after-table plus the route→module→chunk re-check are that
build's own output; the fix-stage mutant is §6 row h; `git diff --check` exit 0; both DLP greps 0
matches and the slice records' host-path scan 0 matches (§12).

R2-fix-stage refresh (2026-11-23, after §13's three findings; the source changes are the two guards
and their two cases): typecheck exit 0 (`tsc --noEmit`, 4.86 s); lint exit 0 (`220 problems (0
errors, 220 warnings)`, 50.51 s, cap unraised); the two changed pin files in one run exit 0,
`Test Files 2 passed (2)`, `Tests 71 passed (71)`; the connections suite alone 59/59 green in 3 of 3
runs; the helper's suite 12/12 green in 8 of 8 runs; the isolated new cases green — the mixed-case
case `1 passed \| 58 skipped (59)`, 8.30 s, and the storage-blocked case `1 passed \| 11 skipped
(12)`, 2.69 s; the gate payload `vitest run` exit 0, `Test Files 27 passed (27)`, `Tests 632 passed
(632)` in 39.90 s, 35.20 s, four further runs (145.08 s for the four) and once more after the
owner-policy comment trim (39.41 s) — one earlier invocation of the same bytes exited 1 with `Tests
1 failed \| 631 passed (632)`, name not captured, and never reproduced (§11's host-flake class);
`build` exit 0 (1.33 s) and §13's asset table is that build's own output, reproduced name- and
size-identical after the comment trim; the r2-fix-stage mutants are §6 rows i-j; `git diff --check`
exit 0; both DLP greps over the worktree diff (HEAD → revision 4) 0 matches and the record
sanitizer exit 0 on each record (§13).

## 8. Attention and Runtimes: the eager decision with numbers

Both variants were built on the same tree (the second one reverted immediately afterwards):

| variant | entry chunk | entry's static shared chunk | attention / runtimes chunks | initial payload |
| --- | --- | --- | --- | --- |
| delivered (both eager) | 321.32 kB (101.84 gzip) | 400.76 kB (116.11 gzip) | — (both inside the entry) | 722.09 kB / 217.95 kB gzip |
| both lazy (measured, reverted) | 301.64 kB (95.85 gzip) | 398.04 kB (115.38 gzip) | `attention-DL7A5b2m.js` 9.36 kB (3.19 gzip), `runtimes-BRRhG7Zz.js` 6.29 kB (2.02 gzip) | 699.68 kB / 211.23 kB gzip |

The lazy boundary would move 19.68 kB raw / 5.99 kB gzip off the entry and 22.41 kB raw /
6.72 kB gzip off the initial payload — bought by making the **default landing route** (`/` redirects
to `/attention`) wait for a chunk before the router commits, i.e. an extra round trip on the most
common cold load, for 3.19 kB gzip. Decision: both stay eager, as the contract's default states,
with these numbers recorded.

## 9. The mount-timing conflict (attempt 1) — resolved by the conductor ruling

**Case**: `frontend/src/features/connections/connections.test.tsx:735`
`connection management > does not pull the operator back after they left the page while a save was
pending`, failing at `:746`: `Expected: "/portal/projects"`, `Received: "/portal/connections"`.

Attempt 1 hit this as a blocking conflict between clause 1 (full suite green) and the split's own
"previous page stays rendered while the destination chunk loads" wording. The conductor ruling of
2026-11-22 resolved it: the mount-based guard is replaced by a check of the router's current
location at all the page's late-continuation sites, and every other page under a split route is
audited for the same class (plan §6a/§6b).

**Base comparison**: the same case passes at base in isolation (2 of 2 runs, exit 0) on this host;
the failure is introduced by the split, deterministically (3 of 3 isolated runs, plus both full-suite
runs).

**Mechanism** (measured with an instrumented copy of the case, deleted before hand-off; history
subscription + `push`/`replace` stack logging):

1. `act(() => { harness.history.push("/portal/projects"); })` — the history pathname becomes
   `/portal/projects`, the connections page's DOM is detached, the content area is empty while the
   `/projects` chunk loads. The push itself is logged from the test.
2. The pending PATCH response is released. `connections.tsx:443-448`'s continuation runs while
   `mounted.current` (`:357-360`) is still `true`, because React keeps the previous route's
   component tree mounted while the new route's chunk resolves — the behavior the contract's own
   "no flash of missing content" clause asks for, pinned by `route-chunks.test.tsx` case 1.
3. The guard therefore passes and the page navigates back:
   `PUSH "/portal/connections?connectionId=20000000-0000-4000-8000-000000000001" at
   frontend/src/features/connections/connections.tsx:446:14`.

So the app answers "did the operator leave?" with mount state, and lazy routing deliberately widens
the window in which a mounted page is no longer the visible page.

**Why this is a contract conflict, not an implementation slip**: clause 1 requires the full suite
green; the split's acceptance wording requires the previous page to stay rendered while the
destination's chunk loads ("the router's own pending state is the only new intermediate surface and
the suites must not observe a flash of missing content"). Keeping the previous page mounted is
exactly what keeps `mounted.current` true. The two requirements meet in this one case.

**Rejected alternatives**:

- `wrapInSuspense: true` on the lazy routes (per-route Suspense fallback = `null`): it would drop
  the previous page during the chunk load — the flash of missing content the same paragraph
  forbids — to make one assertion pass. Not run; rejected on the contract's wording.
- removing `lazyRouteComponent`'s `component.preload` so the router's `load()` commits the location
  without awaiting the chunk (measured, reverted): the case still failed 3 of 3 (React keeps the old
  tree while the new render suspends) **and** the frozen case
  `readable work views > focuses the content heading on a genuine route change` (`router.test.tsx:520`)
  failed 3 of 3, because the shell's heading reset is gated on the router's `status === "idle"`
  (`shell.tsx:246-253`), which then no longer implies the new page is mounted. So the current
  behavior is the one that keeps the focus contract.
- preloading every route chunk after first paint: it would fetch the whole application on every load
  and defeat the split the contract asks for.

**Resolution as implemented (attempt 2, conductor ruling of 2026-11-22; the trailing-slash trim is
the fix stage's, §12)** — one predicate at
`frontend/src/features/connections/connections.tsx:365`, used by `noticeFor` (`:417`),
`clearSelectionFor` (`:420`) and the `saved` continuation (`:449`) — these read `:415`, `:418`,
`:447` before the fix stage's two comment lines; the pinned case moved from `:735`/`:746` to
`:744`/`:761` by the same stage's one added test:

```tsx
const stillOnConnections = () => mounted.current && trimPathRight(router.latestLocation.pathname).endsWith("/connections");
```

`mounted.current` stays as the second conjunct, so the unmounted case keeps its semantics; no route,
`validateSearch` or rendered surface changes, and the page's own suite changes only by the fix
stage's one added case (§12).

Note on attempt 1's recorded diff: it compared the raw `router.history.location.pathname` with
`"/connections"`. On this route that value is `/portal/connections` (§9a), so the recorded
comparison is always true here — a no-op guard that could neither fix the pinned case nor preserve
the notice path; it cannot be the exact shape that produced attempt 1's green measurement runs.
Attempt 2 implements the comparison against the router's parsed current location, whose both
branches the suites exercise: `connections.test.tsx:322` and `:663` assert a notice that
`noticeFor` sets (guard pass), and the pinned case asserts the navigation is dropped once the
location moved (guard fail).

### 9a. Location accessors on this tree (temporary probe, deleted before the gates)

A throwaway test (`frontend/src/app/tmp-location-probe.test.tsx`, deleted before any gate) mounted
the real `createAppRouter` at `/portal/connections` through `renderRoute` and printed the accessors:

| moment | `router.latestLocation.pathname` | `router.state.location.pathname` | `history.location.pathname` | `router.state.matches.at(-1).routeId` |
| --- | --- | --- | --- | --- |
| on the page | `/connections` | `/connections` | `/portal/connections` | `/connections` |
| right after `history.push("/portal/projects")` | `/projects` | `/projects` | `/portal/projects` | `/connections` (still) |
| +1 microtask | `/projects` | `/projects` | `/portal/projects` | `/connections` (still) |
| +100 ms (destination chunk resolved) | `/projects` | `/projects` | `/portal/projects` | `/projects` |

Both parsed locations are basepath-stripped and flip with the push, before the destination chunk
resolves (`load()` calls `updateLatestLocation()` first —
`router-core@1.171.39/dist/esm/router.js:487`); the committed match tree keeps the leaving route for
the whole chunk-load window. The parsed current location is the only one of the four that both
strips the basepath and moves at the moment the operator leaves. The same accessor on the
trailing-slash form of the URL is §12's probe.

## 10. Host dependency tree (documented deviation, restored before hand-off)

- HEAD pins four dev packages the only reachable registry on this host does not serve: `msw 3.0.7`
  (feed latest 3.0.6), `@vitest/coverage-v8 5.1.1` (5.1.0), `knip 6.46.1` (6.45.1),
  `typescript-eslint 8.73.0` (8.72.2 stable) — all four from `09bf927c` (#240). `registry.npmjs.org`
  and `registry.npmmirror.com` are TLS-filtered from this network (`curl` → `000`; a
  `--frozen-lockfile` install failed with `ERR_PNPM_META_FETCH_FAIL`, and the host's supply-chain
  sweep reported `✗ Lockfile failed supply-chain policy check (488 entries in 7 m 12.5 s)`).
- The gate runs therefore used a tree materialized from a temporary lockfile whose deltas against
  HEAD's are those four packages (and their peer suffixes, e.g.
  `vitest@5.1.0(@vitest/coverage-v8@5.1.0)(…msw@3.0.6…)`) plus the feed's own tarball URLs. A first
  install attempt left pnpm with two peer variants of `vitest@5.1.0` (the frontend linked one,
  `@testing-library/jest-dom` the other), which made `tsc` report 721
  `Property 'toBeVisible' does not exist on type 'Assertion<…>'` errors; a clean re-install produced
  a single variant (verified by `readlink` on both consumers) and the 721-error class did not
  reappear in any later run.
- `pnpm-lock.yaml` and `frontend/package.json` were restored to HEAD's bytes before the diff and DLP
  checks (`sha256` `b9575212…` and `371e97c3…`, `git status` shows only the four slice files). The
  temporary lockfile is not committed. CI installs from HEAD's lockfile on a runner with npmjs.org
  access, so this deviation is host-local.
- Rejected alternatives: `scripts/ci/check-lockfile-registry.py` on the temporary lockfile would fail
  by design (it carries feed tarball URLs) — it was not run against the temporary file, and the
  committed lockfile is untouched.
- Attempt 2 re-hit the two-variant state: the attempt's first pnpm invocation (the pinned binary
  without the flags, which re-ran `verifyDepsBeforeRun` against the committed lockfile) left
  `@testing-library/jest-dom@7…/node_modules/vitest` pointing at the other `vitest@5.1.0` peer
  variant (`…_a41af5d6…`) than `frontend/node_modules/vitest` (`…_d22a4b15…`), and `tsc` reported
  the 721-error class again. Repaired by re-pointing that one symlink at the frontend's variant
  (both consumers re-checked with `readlink`); typecheck is exit 0 (7.03 s) after it. Every
  attempt-2 gate ran under `--trust-lockfile --config.verify-deps-before-run=false`, which keeps
  pnpm from re-running that install and the lockfile policy sweep (measured: the bare pinned-pnpm
  invocation timed out at 120 s inside the sweep; the accommodated invocation completes in 0.4 s
  with no install attempted).

## 11. Residual risks and limits

- The render gate is CI's verdict; this host cannot run the release-shell harness. Every split route
  is a deep-link target in the gate's wired specs except `/connections`, which appears only in
  `portal-cutover`'s navigation list.
- The `/` redirect has no jsdom case and no wired render-gate case (named in §3); its line is
  byte-identical and it carries no component import.
- The mount-timing class was audited across every page under a split route (plan §6b carries the
  grep methods and the per-site disposition). Two sites remain unfixed, both state-writes-only
  behind a subject fence that already discards another subject's continuations:
  `connections/resources.tsx:566-574` (`live()`) and `work/assignment.tsx:116-121`, `:137`. Under
  the split, a late write from either can at most render a notice on a page that is still visible
  while the destination chunk loads; no navigation, no URL change, no gate pins that window, and
  neither file is inside the ruling's owned set.
- Lazy routes change *when* a navigation's commit happens: on a cold deep link into a split route the
  navigation's commit waits for that route's chunk (`router-core`'s `routeNeedsLoad`,
  `Router.js:22-23`, treats `component.preload` as a route loader), so the shell's first paint
  follows that one fetch; on a navigation the previous page stays until the new one is ready
  (both pinned by `route-chunks.test.tsx` cases 1-2). Dropping the preload measured a lost heading
  focus, so the trade is deliberate.
- One full-suite invocation of the pre-apply settled bytes exited 1 with an unidentified failure: my
  own `tail -10` truncation dropped the failing case's name before the output reached this record,
  and the run does not reproduce — 7 further invocations of that revision are `27/625` green,
  every one of them with a FAIL/`timed out` grep in place (plus two green runs of the same source
  before the new test file's LF normalization), and every suite that exercises the moved
  paths (router, connections, chat, goals, work, planning, route-chunks) is green in all of them.
  Recorded as a host-side flake of one occurrence, unidentified; it is not the deterministic
  single-case red of any mutant (§6), and no evidence attributes it to this change.
- The same class repeated once at the fix stage: of three json-reporter invocations of the 27/630
  suite, one exited 1 with 27 failures, all in the four workers that started first
  (`work/assignment` 10/10, `work/control` 10/13, `router` 6/42, `goal-settle` 1/8) and all with an
  empty rendered tree (`<body><div /></body>` — the portal double never answered); the plain gate
  command on the same bytes was green twice, and the json invocation that produced §7's table was
  green (630 passed, exit 0). Recorded as the same unidentified host flake. The r2 fix stage saw a
  third instance: one invocation of its bytes exited 1 with a single unnamed case red (`Tests 1
  failed | 631 passed (632)`), and six further invocations of the same bytes plus the two new cases'
  stress runs are green — §7's r2 refresh.
- The guard's trailing-slash normalization is this page's: `app/shell.tsx`'s own comparisons
  (`:91`, `:210`) keep the repo's plain `endsWith` form and still read a no-search trailing-slash
  URL as not this route (§12's probe reads `/connections/` for that form), so the project picker's
  connections/attention scope choice has the same shape there. That file is explicitly not owned
  ("stays eager, untouched") and the finding named only `connections.tsx:363`; named here as a
  residual, not fixed.
- 4 dev packages differ from the committed lockfile on this host (§10); no other file in the tree was
  left modified.

## 12. Fix stage (2026-11-23): the r1 review's trailing-slash finding

Finding (r1 review, `rf-w6-07-frontend-route-splitting-fix1`): `connections.tsx:363` compared
`router.latestLocation.pathname === "/connections"` where the repo's convention is
`endsWith("/connections")` (`app/shell.tsx:91`, `:210`); a trailing-slash deep link
(`/portal/connections/`) parses to `/connections/`, the guard answers false and the page's late
continuations are suppressed. Suggested fix: the `endsWith` form.

Probes (temporary test files, deleted before the gates; raw rows):

| URL at mount | moment | `latestLocation.pathname` | `history.location.pathname` | matching route |
| --- | --- | --- | --- | --- |
| `/portal/connections/` | mount, after the create editor opened | `/connections/` | `/portal/connections/` | `/connections` |
| `/portal/connections/?connectionId=<id>` | mount, detail visible, after a settled refusal | `/connections` | `/portal/connections/?connectionId=<id>` | `/connections` |
| `/portal/connections` | settled | `/connections` | `/portal/connections` | `/connections` |

The slash survives the initial parse on every URL that carries it, with or without a search: a
fresh memory history at `/portal/connections/?connectionId=<id>` reads `latestLocation.pathname`
`/connections/` at mount (revision-4 probe, §13). The table's second row read `/connections`
because its moment is after the app's own selection navigation had rebuilt the URL without the
slash — not because the search changes the parse. Consequences for the finding's reach, corrected:
with the slash intact `noticeFor`/`clearSelectionFor` were reachable too, until such a navigation
happened; the affected continuations are not limited to the `saved` selection move after a create.
The suggested `endsWith` form alone would not fix it (`"/connections/".endsWith("/connections")` is
false — the probe's own columns), so the fix trims the pathname the way the router's own matching
does: `trimPathRight`,
exported by `@tanstack/react-router`, which the router itself calls (`router-core@1.171.39`
`dist/esm/router.js:241` and `:395`, `route.js:41`).

Change: `connections.tsx` +4/-2 — the `trimPathRight` import, the predicate
`mounted.current && trimPathRight(router.latestLocation.pathname).endsWith("/connections")`, and the
two-line why comment beside it. Canonical URLs keep the old answer (`trimPathRight("/connections")`
is `/connections`), so the pinned §9 case and the two notice cases are untouched.

Pin: `connections.test.tsx` gains one case, `connection management > keeps this page's own
continuations when the URL carries a trailing slash` (+15/-0 lines): a mount at
`/portal/connections/`, a create whose re-read settles while the operator stays on the page, and the
selection move asserted on `history.location.search`. §6 row h is the reverse pair: red on HEAD's
guard bytes, green on the fixed bytes. This is the one file outside the contract's strict owned set
this stage touches; the finding's own "no gate pin" clause is why, and the case's home is the page's
suite (the alternative, `route-chunks.test.tsx`, is the helper's unit test).

STD-A02 (the same review's second P3) is recorded as unmet with the exact path
`docs/evidence/rf-w6-07-route-splitting-2026-11-22.md`: it asks the operator to confirm that a
record under `docs/evidence/` is allowed (the contract's strict owned set enumerates only
`docs/plans/<date>-<slice>-plan.md`). The file predates this stage, the review accepted it as the
program's convention (w5-08 precedent), and this stage neither moved nor deleted it; the operator
owns that call, and no edit was made for the finding.

Host-path hygiene at the r1 fix stage: a text scan over the slice's two records finds no Windows
drive path, no account-name spelling and no absolute POSIX home, scratch or mount prefix — the
single prefix-shaped hit is a relative test filename's first segment (the deleted §9a probe), not an
absolute path; prose-only scan, no pattern pasted into this record. The repository's own record
sanitizer accepts both files (`scripts/ci/model-routing-record-sanitize.py --check --root-only`,
exit 0 each), the branch diff's user-profile grep is 0 and the backslash-u grep is 0, and
`git diff --check` is exit 0. The scan covers the slice's own records only — the project-wide
`docs/` tree carries pre-existing quoted log lines and pre-existing entries with such spellings
(e.g. `docs/TODO.md`, `docs/archive/CLOSED-TODO.md`), which this stage did not rewrite.

## 13. R2 fix stage (2026-11-23): the r2 review's P3-1, P3-2 and P3-5

The round-2 review of PR #261 closed three actionable P3s: P3-1 (the guard compared the pathname's
text, so a URL the router matches case-insensitively dropped the page's continuations), P3-2 (the
§12 sentence claiming a slash+search URL parses slash-free) and P3-5 (the storage claim's unguarded
`sessionStorage` access). P3-3/P3-4 are the disclosed scope deviations §12 already records
(accepted or operator-owned); no edit was made for them.

Probes (one temporary probe file, deleted before the gates; raw rows):

| URL at mount | `latestLocation.pathname` | text compare | lowercased text compare | last `matchRoutes` routeId |
| --- | --- | --- | --- | --- |
| `/portal/connections` | `/connections` | true | true | `/connections` |
| `/portal/connections/` | `/connections/` | true (`trimPathRight`) | true | `/connections` |
| `/portal/Connections/` | `/Connections/` | **false** | true | `/connections` |
| `/portal/CONNECTIONS` | `/CONNECTIONS` | **false** | true | `/connections` |
| `/portal/Connections?connectionId=<id>` | `/Connections` | **false** | true | `/connections` |
| `/portal/connections/?connectionId=<id>` | `/connections/` | true | true | `/connections` |
| `/portal/connections?connectionId=<id>` | `/connections` | true | true | `/connections` |
| `/portal/connections/xyz` | `/connections/xyz` | false | false | `/connections` (fuzzy not-found) |
| `/portal/projects` | `/projects` | false | false | `/projects` |
| `/portal/nope` | `/nope` | false | false | `__root__` |

Every row comes from `createAppRouter(createApplication(...), createMemoryHistory({initialEntries:
[url]}))` + `router.load()` on this tree; `matchRoutes(location)` and `matchRoutes(pathname, search)`
returned the same route id on every row, and `getMatchedRoutes(pathname)`'s found route agrees. The
table's second row is what corrects §12's reach claim: the slash survives *at mount* in the search
shape too, so the revision-3 reading (`/connections` for that URL) was a step after an app-driven
selection navigation had rebuilt the URL, not a property of the parse. The storage probe: jsdom
defines `sessionStorage` as a configurable own getter on `window`, `vi.spyOn(window,
"sessionStorage", "get")` stubs it so the property access throws
`SecurityError: The operation is insecure.`, and `mockRestore()` restores the descriptor.

Changes:

- `connections.tsx` +5/-4: `stillOnConnections` reads `mounted.current &&
  router.matchRoutes(router.latestLocation.pathname, router.latestLocation.search).at(-1)?.routeId
  === "/connections"`, with the two-line why comment replacing the two-line trim comment (trimmed to
  the owner policy's two-line hazard-comment cap the same day) and `trimPathRight` leaving the
  import. The non-deprecated `(pathname, search)` overload is used; the older `(location)` overload
  returned the same route id on every probe row. The trim is inert in the shipped bytes: the
  rebuild after it reproduced every asset name and size.
- `route-chunks.ts` +8/-2: `claimReloadOnce`'s get/set pair sits inside `try`/`catch`; a blocked
  storage returns `false` (no claim), so the caller's `staleChunkMessage` rejection reaches the
  router's error state and the listener leaves the event unprevented. Vite's helper rethrows an
  unprevented preload error (`vite@8.3.8` `dist/node/chunks/node.js:27046-27051`), so both paths
  reach the same terminal state; claiming a reload the marker could never suppress is what would
  loop.
- Pins: `connections.test.tsx` +15/-0, case 59 `connection management > keeps this page's own
  continuations when the URL spells the route in another case` (mount at `/portal/Connections`, a
  create's selection move asserted on `history.location.search`); `route-chunks.test.tsx` +20/-0,
  case 12 `shows the route's error state without reloading when the session storage is blocked`
  (stubbed getter; the stale-chunk text in the error state, `reload` 0 calls, and the dispatched
  `vite:preloadError` left unprevented). §6 rows i-j are the reverse pairs: each case green on the
  fixed bytes and red on its mutant.

One consequence of the route-id form, recorded: `/portal/connections/xyz` resolves to the route id
`/connections` (the default fuzzy not-found), so the guard answers true there where the text
compare answered false. That is the router's own resolution of the location — the ruling's "follow
the router's matching" — and no gate pins that URL shape.

Build at revision 4 (`vite build` exit 0, 1.33 s; byte counts from the emitted files, sizes as the
build's own report):

| chunk | bytes | raw | gzip |
| --- | --- | --- | --- |
| `dist/assets/index-DXalnIiK.js` | 321,357 | 321.35 kB | 101.86 kB |
| `dist/assets/served-ZmW5x40A.js` | 400,764 | 400.76 kB | 116.11 kB |
| `dist/assets/chat-BgeMpCWY.js` | 27,115 | 27.11 kB | 8.68 kB |
| `dist/assets/connections-Dyo4xJuW.js` | 34,476 | 34.47 kB | 9.14 kB |
| `dist/assets/detail-Dj3xdiDj.js` (goals) | 30,405 | 30.40 kB | 8.40 kB |
| `dist/assets/detail-DRpbuqHs.js` (work) | 35,088 | 35.08 kB | 10.38 kB |
| `dist/assets/projects-CfjvBJ__.js` | 6,239 | 6.23 kB | 2.23 kB |
| `dist/assets/work-surface-U7eftdWu.js` | 34,515 | 34.51 kB | 9.57 kB |
| `dist/assets/refusals-C5yaP-QR.js` | 2,675 | 2.67 kB | 1.06 kB |
| `dist/assets/receipts-BZD9UU-g.js` | 2,966 | 2.96 kB | 1.21 kB |
| `dist/assets/use-dialog-opener-BywfTvAD.js` | 427 | 0.42 kB | 0.28 kB |
| `dist/assets/index-Ccu45kBm.css` | 35,353 | 35.35 kB | 7.45 kB |
| `dist/index.html` | 604 | 0.60 kB | 0.34 kB |

`index.html` names the entry plus one `modulepreload` of `served-ZmW5x40A.js`, so the initial JS
payload is 722,121 bytes; a second build of the same tree reproduced every asset name and size
(report diff: identical), `portal:stale-chunk-reload` still appears only in the entry, and the
distinctive-literals check still resolves chat → `chat-BgeMpCWY.js`, projects →
`projects-CfjvBJ__.js`, attention and runtimes → the entry. Against §4's fix-stage table the entry
re-took its name and bytes (321,329 → 321,357 B) because `route-chunks.ts` ships in it, and every
chunk whose import specifier names the entry — or a chunk that does — re-hashed with it: `chat`,
`connections`, both `detail`s, `projects`, `work-surface` and `receipts`; `served`, `refusals`,
`use-dialog-opener` and the stylesheet kept their hashes. `connections-*.js` is 34,442 → 34,476 B
(9.13 → 9.14 kB gzip) — the predicate's own bytes — and every other chunk keeps its size figures
except `work-surface-*.js`'s gzip (9.58 → 9.57 kB, the build's own rounding).
