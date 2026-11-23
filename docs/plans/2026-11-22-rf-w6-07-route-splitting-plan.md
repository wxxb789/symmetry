# rf-w6-07-route-splitting plan — the lazy route set, the boundary helper, and the stale-chunk policy

Contract: `goal-rf-w6-07-frontend-route-splitting.md` (GDD, 2026-11-21, binding with D1, D2, the
conductor decisions of 2026-11-21 — including the W6-reconcile stale-chunk policy — and the owner
comment policy of 2026-11-22). Base revision `6d55c575`
(`test(frontend): keep layout, scroll and focus invariants in the browser layer only (#254)`, this
worktree's dispatch HEAD; `git status --porcelain` clean at dispatch apart from gitignored
`node_modules/` install output).

Shape references: `docs/plans/2026-11-22-rf-w5-08-jsdom-browser-split-plan.md` and
`docs/evidence/rf-w5-08-jsdom-browser-split-2026-11-22.md` (dispatch gates, re-derivation,
control-flow audit, mutation table, gate rows, residual risks); the program row
`docs/plans/2026-11-21-refactor-program-plan.md:563`.

Revision 2 (attempt 2, 2026-11-22): the conductor ruling of 2026-11-22 (implement attempt 1
BLOCKED) resolves §6 — the connections guard change is authorized (option A), the accessor is
`router.latestLocation`, and the audit it demands is §6b. Attempt 2 continues from attempt 1's tree;
§7 carries the settled numbers.

Revision 3 (fix stage, 2026-11-23): the r1 review's STD-A01 is fixed in the same guard — a URL that
names this route with a trailing slash keeps the slash in the router's parsed pathname, so the
comparison trims it the way the router's own matching does (`trimPathRight`); the review's literal
`endsWith` suggestion alone does not survive that form (measured; evidence §12). One case in
`connections.test.tsx` pins the reachable branch — the create's selection move; the suite counts
move 57 → 58 and 629 → 630, and §3 row h is the branch's mutant row. The added case sits above the
pinned mount-timing case, which moves from `connections.test.tsx:735`/`:746` to `:744`/`:761`.

## 1. Dispatch gates (checked at dispatch, base `6d55c575`)

| gate | state at dispatch |
| --- | --- |
| dependency chain merged — #164 (W2-15, `c6b2c352`), W3-12, W4-11 (#220, `e84973a7`), W4-08, W4-09, W4-10, W4-14 (#205, `105d6eda`), W5-03 (#249, `cbe4910e`), W5-04 (#251, `956ff4f6`), W5-08 (#254, this HEAD) | yes — each named revision is an ancestor of HEAD |
| open PRs touching the owned set | none — the three open PRs (#258 control orchestration, #257 daemon fault store, #256 ci/linux-v2) name no `frontend/**` path |
| W6-02 (ci.yml / browser wiring) | not merged at dispatch; the contract says to use the gate as landed: the `control` job's step "Render authenticated release shell on desktop and mobile" (seven spec files) plus "Assert the render gate's executed set" |
| owned files re-derived | `frontend/src/app/router.tsx` 80 L / 12 static imports / 9 route declarations, unchanged from the contract's re-measure; heavy entries drifted after the contract's base: `goals/detail.tsx` 778 L (761), `chat/chat.tsx` 257 L (734 — W4-08 split it; `chat-sections.tsx` 272 L + `chat-hooks.ts` carry the rest), `connections/resources.tsx` 672 L (675), `planning/editors.tsx` 558 L (583), `work/detail.tsx` 457 L (456) |
| `router.test.tsx` re-derived | 1,215 L (the contract's re-measure says 1,328 L at `0fa9b705`; the declaration count reproduces exactly): 40 `it` + one `it.each` with 2 paths = 41 declarations / **42 executed cases** |

## 2. Design decisions the contract leaves unresolved

1. **The lazy set** (recorded here and in the PR): `/chat` → `features/chat/chat`, `/projects` and
   `/projects/$projectId` → `features/projects/projects`, `/work/$workItemId` →
   `features/work/detail`, `/connections` → `features/connections/connections` (which pulls
   `connections/resources`), `/goals/$goalId` → `features/goals/detail`. Eager, byte-identical:
   the root route and `WorkspaceRoot`, the `/` redirect, `/attention`, `/runtimes`,
   `features/projects/filters` (both `validateSearch` functions run at route-definition and
   navigation time, so the module stays in the entry graph), `app/**`, `services/**`,
   `components/**`. The set covers the contract's minimum: goals/detail, chat/chat,
   connections/resources and the two routes that import `planning/editors`
   (`projects/projects.tsx:9`, `work/detail.tsx:12`; `planning/write-policy` additionally reaches
   the entry graph only through those files).
   Attention (the default landing page) and Runtimes stay eager: their measured contribution to
   the entry chunk decides it, and the numbers for both variants are recorded in the evidence
   record. The default is "stay eager"; a lazy boundary is only adopted for them if the measured
   entry chunk shows it materially helps, and the measurement is reported either way.
2. **Mechanism**: `lazyRouteComponent(() => import("../features/<file>"), "<NamedExport>")` per
   route, the contract's named mechanism for this repo's code-based `createRoute` router. Every
   named export already exists, so the mechanism itself needs **no import-shape change in
   `features/**`** (that allowance stays unused); the only `features/**` edit is the connections
   guard the ruling authorizes (§6a). No route `lazy` option, no file-based route plugin, no
   `manualChunks`, no new dependency.
3. **The boundary helper file** (name recorded): `frontend/src/app/route-chunks.ts`, with
   `frontend/src/app/route-chunks.test.tsx` as its unit test. It owns exactly two things: the
   stale-chunk policy and the `loadRouteChunk` boundary each lazy route's importer is wrapped in.
   `router.tsx` keeps one line per route (`component: lazyRouteComponent(loadRouteChunk(() =>
   import(...)), "Name")`) and installs the policy in `createAppRouter` (the app's one router
   construction point).
4. **Stale-chunk policy** (W6-reconcile decision, binding): one full page reload per build,
   guarded by a `sessionStorage` marker keyed by the build id; a second failure for the same build
   reaches the router's error state instead of reloading again. Decisions inside that frame:
   - **Build id** = `import.meta.url` of the entry chunk. It is the URL a deployment replaces, so
     a marker an earlier build left behind never suppresses the reload this build needs. Marker
     key: `portal:stale-chunk-reload`.
   - **Two entry points, one claim**: the `vite:preloadError` listener claims (and
     `preventDefault`s) only when it is the claim; the `loadRouteChunk` boundary handles the
     import rejection, including the `undefined` module a prevented Vite preload resolves with.
     Both funnel into the same claim check.
   - **Failure shapes decided by the load-failure message**, not by "any error": a chunk that
     evaluates and throws, or any other rejection, is passed through to the router's error state
     without a reload. The three load-failure prefixes are the browser wordings
     (Chrome/Firefox/Safari); `@tanstack/router-core`'s `isModuleNotFoundError` carries the same
     three, and the boundary must decide before the library's own reload does.
   - **The library's own reload path is deliberately bypassed**: `lazyRouteComponent` 1.170.46
     implements a reload-once guard keyed by the error *message*
     (`tanstack_router_reload:<message>`); left alone it would fire a second reload for the same
     build after this policy's claim. The boundary therefore throws a plain `Error`
     (`route-chunks.ts:41`, `:49`) whose message is not a module-not-found wording, so exactly one
     reload happens per build and the terminal surface is the router's own error state.
   - **The terminal surface is the router's existing error state** (the default `ErrorComponent`,
     reached through the router's own `CatchBoundary`); no `errorComponent` or `pendingComponent`
     is added anywhere. The policy's one new string is `staleChunkMessage` (`route-chunks.ts:14`),
     and it renders only in the stale-chunk failure state; every normal path is byte-identical.
5. **The pending surface is the router's own**: no route or router pending component is
   configured, so the router's client-side boundaries (`Matches` wraps the match tree and the root
   `Outlet` wraps the child match, both falling back to `null`) render *nothing* for the outlet
   region while a route chunk resolves. Measured (evidence §5, `route-chunks.test.tsx` cases 1-2):
   on a navigation the previous page and the shell stay rendered until the new route is ready, and a
   cold deep link into a split route commits shell and page together; `router-core`'s
   `routeNeedsLoad` (`@tanstack/router-core@1.171.39` `Router.js:22-23`, its
   `route.options.component?.preload` branch) treats `component.preload` as a route loader, so the
   location commit waits for that chunk.
6. **`router.test.tsx` stays byte-identical** unless a case actually needs an added `await`; every
   case already awaits a `findBy*`/`waitFor` after `renderWorkspace`, so the expected number of
   additions is zero, and any addition would be enumerated per case with its mutant.

## 3. Mutations (judging, each applied alone and reverted)

| # | mutation | expected | recorded as |
| --- | --- | --- | --- |
| a | one heavy route back to the static import (`chat`) | the entry chunk regains `chat`'s module; the recorded chunk check reddens | **measured red**: entry 321.32 → 349.39 kB (101.84 → 109.90 gzip), no `chat-*.js`, the chat literal resolves to the entry (evidence §6) |
| b | one lazy import pointed at the wrong module (chat route → the attention module) | the `router.test.tsx` cases exercising `/chat` redden | **measured red**: 1 of 42 at attempt 2 — `chat route > subscribes /chat and carries the shell's project scope in its navigation`, router error surface `Invariant failed: Could not find an active match from "/attention"` (attempt 1 measured the same shape as 3 of 42; evidence §6) |
| c | only if a test needed an added `await`: the case red without it, green with it | — | unused: `router.test.tsx` is byte-identical to HEAD, no `await` added |
| d | the connections guard back to mount-only under lazy routes (`stillOnConnections = () => mounted.current`) | the pinned `connections.test.tsx:735` case reddens | **measured red**: `1 failed, 56 skipped (57)`, exit 1, `Expected: "/portal/projects" / Received: "/portal/connections"` (evidence §6) |
| e-g | the apply stage's three: build-id keying loosened, each non-Chrome load-failure wording broken, the listener's payload filter deleted | the case pinning each rule reddens | **measured red** (three runs; evidence §6 rows e-g) |
| h | the fix stage's branch: the guard's trailing-slash trim removed (back to `router.latestLocation.pathname === "/connections"`, i.e. the pre-fix bytes) | the trailing-slash case in `connections.test.tsx` reddens | **measured red on the pre-fix bytes**: exit 1, `AssertionError: expected '' to contain '20000000-0000-4000-8000-000000000001'`; the same case exits 0 on the fixed bytes (evidence §12) |

## 4. Gates (all foreground, in this order; never the build concurrently with the suite)

| gate | command | expected |
| --- | --- | --- |
| typecheck | `pnpm --filter @symmetry/frontend typecheck` | exit 0 |
| lint | `pnpm --filter @symmetry/frontend lint` (`--max-warnings 229`, re-derived and not raised) | exit 0 |
| router suite | `pnpm --filter @symmetry/frontend exec vitest run src/app/router.test.tsx` | 42 executed cases, names unchanged, exit 0 |
| full suite | `pnpm --filter @symmetry/frontend test` | exit 0, executed count and per-file durations recorded |
| build before/after | `pnpm --filter @symmetry/frontend build` | chunk list (entry + per-route, raw + gzip) recorded before and after |
| diff hygiene | `git diff --check`; the DLP greps | no whitespace damage, no user-profile path, no backslash-u |
| render gate (CI) | the PR's `control` job render step + executed-set assertion | CI-verified; the release-shell harness cannot run on this host (W5-08 records the limitation) |

Host invocation (evidence §10): `pnpm` is the pinned 12.17.0 binary, invoked as
`pnpm --trust-lockfile --config.verify-deps-before-run=false <script>`; both flags are the
host-feed accommodation for the four dev packages this host's registry does not serve, not a change
to any gate's payload.

## 5. Out of scope, with reasons

- The shell eager path, `features/**` internals (W4-08/09/10/W5-03 own those regions; the
  connections guard of §6a is the ruling's one authorized exception),
  test-harness/MSW (W4-11), jsdom/browser split (W5-08), browser wiring and the render-gate step
  (W6-02), the remaining wall-clock sites (W6-12), ESLint/knip configs (W1-10/W6-01), and any
  `manualChunks` configuration — the split must come from the route boundaries.
- No `errorComponent`/`pendingComponent` additions: the router's own surfaces carry the failure
  and pending states, which keeps every rendered byte of the normal path frozen.
- `browser/**` and `.github/workflows/ci.yml` stay untouched; the render gate is the PR's CI
  oracle, not a local one.

## 6. The connections guard conflict (attempt 1) — resolved by the conductor ruling

Attempt 1's full-suite gate was red on exactly one pre-existing case, `connections.test.tsx:735 >
does not pull the operator back after they left the page while a save was pending`
(`Expected: "/portal/projects"`, `Received: "/portal/connections"`), green at base (2/2 isolated
runs). Mechanism, instrumented evidence and rejected alternatives: evidence §9. In one line: the
case leaves /connections by pushing the history to /portal/projects and then settles a pending save;
the page's continuation is guarded by `mounted.current` (`connections.tsx:357-360`, `:445`), and
with a lazy `/projects` route React keeps the connections page mounted while the destination chunk
loads (the "no flash of missing content" behaviour this contract asks for, pinned by
`route-chunks.test.tsx` case 1), so the guard passes and the route is restored to /connections.

Options were (A) replace the mount-based guard with the router's current location, (B) amend the
case's premise (loses the in-flight-navigation window), (C) narrow the lazy set (keeps
`planning/editors`, 558 L, in the entry chunk — fails clause 3). The **conductor ruling of
2026-11-22 authorizes A**, adds `features/connections/connections.tsx` to the owned set, keeps
`connections.test.tsx:735` byte-identical, demands mutant (d), and demands the mount-timing audit
of §6b.

### 6a. The guard change as implemented

One predicate, `const stillOnConnections = () => mounted.current &&
trimPathRight(router.latestLocation.pathname).endsWith("/connections");` (`:356-365`), replaces the
three mount-only checks — `noticeFor` (`:417`), `clearSelectionFor` (`:420`), the `saved`
continuation (`:449`). `mounted.current` stays as the second conjunct so the unmounted case keeps
its semantics; the trailing-slash trim is the fix-stage change (revision 3, evidence §12).

A URL that names this route with a trailing slash keeps the slash in the router's parsed pathname
when the URL carries no search — `latestLocation.pathname` reads `/connections/` and the exact
comparison answered false (temporary probe, evidence §12) — while a trailing-slash URL that *does*
carry a search parses slash-free, so only the no-search flows (the create's selection move;
nothing that needs an open detail) are reachable in that shape.

Accessor, re-derived with a temporary probe on this tree (deleted before the gates; raw output in
evidence §9a): on `/portal/connections`, `router.latestLocation.pathname` and
`router.state.location.pathname` read `/connections` (basepath stripped) while the raw
`router.history.location.pathname` reads `/portal/connections`; after
`history.push("/portal/projects")` both parsed locations already read `/projects`, while the
committed match keeps rendering `/connections` for the chunk-load window
(`router.state.matches.at(-1).routeId` stays `/connections` until the destination chunk resolves).
So the router's parsed current location — not the raw history pathname, not the committed match
tree — is what answers "the page's own route still matches" at the moment a late continuation runs.
Attempt 1's recorded 3-line shape compared the raw `history.location.pathname`, which reads
`/portal/connections` on this route — always true, i.e. a no-op guard that could neither fix the
pinned case nor preserve the notice path, so it cannot be the shape behind attempt 1's green
measurements (evidence §9); 6a is the corrected accessor.

### 6b. The mount-timing guard audit (every page under a split route)

Method: `grep -rn "mounted\.current|useRef(true)|isMounted|mountedRef" frontend/src` (non-test) and
`grep -rn "if (![A-Za-z]*\.current" frontend/src/features frontend/src/app` (non-test). Those are
the class in the whole frontend; no other page under a split route carries a mount-based
continuation guard.

| site | what the guard gates | disposition |
| --- | --- | --- |
| `features/connections/connections.tsx:356-365`, `:417`, `:420`, `:449` | two navigations (pull-back, clear selection) and one notice | **fixed** (§6a) |
| `features/connections/resources.tsx:566-574` (`live()`, gated at `:603` and `:611-635`) | `setNotice` writes plus the reconcile invalidation; no navigation | **not fixed**: the project fence (`projectRef.current === projectId`) already discards continuations for a project the operator left; the mount conjunct only distinguishes "operator left the whole page", where a late write can at most render a notice on a page that is still visible while the destination chunk loads. No navigation, no URL change, no gate pins that window; outside the ruling's owned set (residual, evidence §11) |
| `features/work/assignment.tsx:116-121`, `:137` (`saved`) | `setEditingFor`/`setNotice` writes; no navigation | **not fixed**: same reasoning — the subject fence (`subjectRef.current === workItemId`) already discards continuations for another item (residual, evidence §11) |

## 7. Outcome on the settled tree

- Entry chunk 887.65 → 321.32 kB (257.27 → 101.84 kB gzip), initial payload 887.65 → 722.09 kB
  (257.27 → 217.95 kB gzip); the six route chunks and three small shared chunks are outside it
  (evidence §4). The route→module→chunk literals check out for all six lazy routes, and
  `/attention` / `/runtimes` stay in the entry by the measured decision (evidence §8).
- `router.test.tsx` byte-identical, 42/42 green; `connections.test.tsx` 58/58 green (the fix stage's
  one added case; the pinned case from §6 and every other case byte-identical); typecheck exit 0;
  lint 220 problems / 0 errors under the `--max-warnings 229` cap; build deterministic; full suite
  green — 27 files / 630 tests at the fix-stage refresh, 27 files / 629 at the apply-stage refresh,
  the pre-apply bytes' 27 files / 625 green in 7 of 8 invocations (one unidentified,
  non-reproducing host flake: evidence §11).
- Mutants (a) chunk-level, (b) wrong lazy target and (d) the mount-only connections guard each
  measured red and reverted byte-identically; (c) unused because `router.test.tsx` needed no added
  `await`. The 2026-11-23 apply stage's three further mutants (build-id keying, the Firefox
  wording, the listener's payload filter — evidence §6 rows e-g) each measured red and reverted.
  The fix stage's row (h) is the trailing-slash pair: red on the pre-fix guard bytes, green on the
  fixed bytes (evidence §12).
