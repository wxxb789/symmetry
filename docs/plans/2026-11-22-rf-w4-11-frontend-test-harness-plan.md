# rf-w4-11-frontend-test-harness plan — one portal double (MSW) + vitest config split

Contract: `goal-rf-w4-11-frontend-test-harness.md` (GDD, 2026-11-22), binding with its four
conductor decisions (D1 frozen bytes, D13 MSW, D2 LOC/wall-time report-only, DLP path rule) and
the lesson-32 control-flow preservation decision. Base for every coordinate below:
`f1fe1e590276883415ede724dce788aa22838029` (this worktree's HEAD; the dispatch gate checked
W3-12 `c0784952`/PR #190, W3-13 `f1fe1e59`/PR #204, W2-15 `c6b2c352`/PR #164 and the
consolidated npm bump `4bf4acbb`/PR #183 all merged into it, tree clean). Shape references:
`docs/plans/2026-11-21-rf-w3-13-http-boundary-plan.md` (same-stage GDD shape) and
`docs/plans/2026-11-21-rf-w4-14-browser-fixtures-plan.md` (the same "one shared double" surgery
on the browser side, with its per-helper table and control-flow audit sections).

## Measured basis (base revision `f1fe1e59`)

Test files under `frontend/src`: **21** (contract's "20" predates `tool-cache-config.test.ts`,
added by the tool-cache slice; that file is out of this slice's change scope but inside its
run scope — it must stay green, and its cache assertions bind the config work).

| file | LOC | portal HTTP double today |
| --- | ---: | --- |
| `features/chat/chat.test.tsx` | 2086 | `renderChat` own `vi.fn<typeof fetch>` (`:186-211`), per-case `conversation`/`message`/`workspace` factories |
| `features/connections/connections.test.tsx` | 1535 | `renderConnections` routing block (`:128-154`) over `recordingFetcher`, per-case `handler` |
| `features/goals/goal-attention.test.tsx` | 1342 | `portalFetcher(handler)` (`:22-35`) over `recordingFetcher`, per-case `handler` |
| `app/router.test.tsx` | 1328 | `servedFetcher()` (`:32-49`) + 4 more named factories + 16 anonymous per-case doubles |
| `features/planning/planning.test.tsx` | 1018 | `createHarness` state model (`:173-303`), `failNextMutation`/`loseNextCreateResponse`/`holdNextMutation` |
| `features/work/control.test.tsx` | 516 | local scripted-fetcher harness (`renderDetail(fetcher)`, `unscriptedResponse`) |
| `features/work/assignment.test.tsx` | 395 | `renderWorkDetail` over `recordingFetcher` + `requestRecord` |
| `features/work/detail.test.tsx` | 132 | inline fetcher double |
| `features/runtimes/runtimes.test.tsx` | 127 | inline `vi.fn<typeof fetch>` |
| `services/api.test.ts` | 579 | 45 unit transport doubles — **retained by design** (they test the HTTP boundary itself, one per case) |
| `app/application.test.ts` | 109 | unit transport doubles — **retained by design** (createApplication retry/expiry wiring) |
| other 10 files | — | no HTTP double (pure units, WebSocket, config) |

Harness/config at the base:

- `frontend/src/app/test-harness.tsx` 61 L: `renderRoute(path, fetcher, options)` (`:15-21`),
  `jsonResponse` (`:39-41`), `requestPath` (`:43`), `recordingFetcher` (`:25-32`),
  `trackApplications` (`:52-59`). Users of `recordingFetcher`: connections, goal-attention,
  assignment. `jsonResponse`: goal-attention. `requestPath`: detail only.
- `frontend/src/test-setup.ts` 19 L: `matchMedia` stub, `afterEach(cleanup)`.
- `frontend/vitest.config.ts` 41 L: `environment: jsdom`, `pretendToBeVisual`, `setupFiles`,
  `restoreMocks`, `clearMocks`, tool-cache redirection (`SYMMETRY_TOOL_CACHE_DIR`,
  `SYMMETRY_DISABLE_TOOL_CACHES`) and coverage redirection. No `isolate`, `pool`, `maxWorkers`
  or `fileParallelism` member.
- `frontend/src/tool-cache-config.test.ts` asserts `vite.cacheDir`, `vitest.cacheDir`,
  `vitest.test.cache`, `vitest.test.fsModuleCachePath`, `vitest.test.fsModuleCache` — both
  populated and unset. Any config edit must leave those five expressions byte-identical.
- `msw` is absent from the lockfile (only `@vitest/mocker`'s optional peer range
  `^2.4.9 || ^3.0.0`; verified on the installed `@vitest/mocker@5.1.0`). Registry metadata for
  `msw` on this host: `3.0.5`.
- Module-scope state inventory (contract change item 3): `write-policy.ts`'s
  `pendingCreationIds` map, reset only by `planning.test.tsx:306`'s `afterEach`; `vi.stubGlobal`
  users (`planning.test.tsx:762` crypto, `control.test.tsx:286` sessionStorage,
  `realtime.test.ts` WebSocket ×3) each restore with `vi.unstubAllGlobals()` in their own
  cleanup; `localStorage` users (router, goal-attention, planning) clear in `afterEach`;
  fake timers only in `services/realtime.test.ts` (`useFakeTimers` in `beforeEach`,
  `useRealTimers` in `afterEach`).
- No first-party `src/` module calls `window.scrollTo`; the jsdom `Not implemented:
  window.scrollTo` noise is emitted by a dependency during navigation (the baseline run's stack
  trace names the caller — recorded in the implement-stage section).

## Deliverable shape

1. **`frontend/src/app/portal-double.ts`** (new, the one MSW handler table + request log).

   One module-scope MSW server, one typed request log, one seeded portal state, one default
   handler table (reads derived from the seed + the planning write model), and one per-case
   override layer. Every former private double becomes a *configuration*: a seed plus explicit
   per-case resolvers; nothing keeps a second routing table.

2. **`frontend/src/test-setup.ts`**: install the double's server for every test file
   (`listen({ onUnhandledRequest: "error" })`), reset state/log/handlers after each case, close
   after each file; `window.scrollTo` no-op stub (comment policy 3: one `why` line naming the
   jsdom gap).

3. **`frontend/vitest.config.ts`**: the measured config shape (isolate/pool/maxWorkers), chosen
   from the state inventory and the shuffle run, with the tool-cache behaviour byte-identical.

4. **`frontend/package.json` + root `pnpm-lock.yaml`**: `msw` exact pin `3.0.5` in
   `devDependencies` (inside `@vitest/mocker`'s optional peer range `^2.4.9 || ^3.0.0`; all 22
   existing devDependencies are exact pins), scripts unchanged, lockfile closure regenerated and
   re-checked with `scripts/ci/check-lockfile-registry.py`.

5. **Suite migrations** (per-file table of deleted definition → configuration site in the
   artifact): planning, connections, goal-attention, chat, router, control, assignment, detail,
   runtimes.

6. **`frontend/src/app/test-harness.tsx`**: keeps `renderRoute(path, options?)` and
   `trackApplications`; the base file's `recordingFetcher`, `requestPath` and `jsonResponse`
   leave the harness (the body helper moves into the double), and the base fetcher seam is
   deleted with the last migration (F2).

7. **`frontend/src/services/api.fixtures.ts`**: stays the served-body fixture home (the double
   calls `readWorkDetail`/`readAttention` for its derived defaults);
   only genuinely unused exports are removed.

## API shape

```ts
// frontend/src/app/portal-double.ts
export interface PortalRequest {
  method: string;                      // upper-case
  path: string;                        // pathname + search, e.g. "/portal/api/attention?cursor=cursor-one"
  url: URL;
  body: Record<string, unknown> | null; // JSON string body decoded once, here
  rawBody: string | null;
  headers: Record<string, string>;
}

export type PortalResolver = (request: PortalRequest) => Response | undefined | Promise<Response | undefined>;

export interface PortalSeed {
  session: { csrfToken: string; socketToken: string | null };
  workspace: { selectedProjectId: string | null; projects: PortalProject[]; runtimes: unknown[]; registeredRuntimes: unknown[] };
  connections: { providers: unknown[]; connections: unknown[] };
  details: Record<string, unknown>;     // work item id -> detail body
}

export interface PortalDouble {
  seed(seed: Partial<PortalSeed>): void;         // per suite (beforeEach)
  state: PortalSeed;                              // the seeded state the table serves from
  requests: PortalRequest[];                      // the request log, cleared per case
  sent(method: string, path: string): PortalRequest[];
  serve(resolver: PortalResolver): void;          // per-case layer, ahead of the default table
  holdNextMutation(): () => void;                 // holds every mutation until released
}
export const portal: PortalDouble;
export function resetPortalDouble(): void;        // test-setup afterEach
```

`renderRoute(path, options?)` needs no fetcher: the app's default transport is the intercepted
global fetch, and a case that needs a non-HTTP failure scripts it through the double's `serve`
layer returning `HttpResponse.error()`. The seed groups are `session`, `workspace`
(`selectedProjectId` absent = the first seeded project, explicit `null` = no selection, `projects`,
`runtimes`, `registeredRuntimes`, `health`), `connections` and `details`; mutation control is
driven through `portal.state.mutations`, never a seed. In the delivered shape (one
`setupServer`) the resolver runs exactly once per request — MSW 3.0.5's `executeHandlers` runs
once per frame — so a suite's counters and state changes happen exactly once per request, and
the body is decoded once at record time.

`send`-side helpers the suites keep locally (planning's `postsOf`/`patchesOf`, chat's `posts()`)
become one-line filters over `portal.requests`; `portal.sent(method, path)` covers the exact
method+path case.

### Default table (seed-derived; registered-default deltas vs the deleted doubles enumerated below)

| path | default |
| --- | --- |
| `GET /portal/api/session` | `{ csrf_token, socket_token? }` from the seed |
| `GET /portal/api/workspace` | seeded projects with resources and work items scoped to `project_id` (workspace reads scope rows like the server does), `selected_project_id`, `runtimes`, `registered_runtimes` |
| `GET /portal/api/connections` | seeded providers + connections |
| `GET /portal/api/attention` | derived from the seeded projects' blocked items exactly as `router.test.tsx`'s `servedFetcher` derived it, scoped by `project_id` (explicit select, else the first project) |
| `GET /portal/api/work-items/:id` | seeded detail body, else `readWorkDetail` over the seeded row's projection |
| `GET /portal/api/work-items/:id/timeline` | `{ items: [], next_before: null }`; a case that needs another body scripts it through `serve` |
| `GET /portal/api/chat` | the empty conversation for the URL's scope key; a case scripts the conversation through `serve` |
| planning's write model | `POST /portal/api/projects`, `PATCH /portal/api/projects/:id`, `POST /portal/api/projects/:id/work-items`, `PATCH /portal/api/work-items/:id`, `PATCH /portal/api/work-items/:id/move` — the seven refusal rules the deleted `createHarness` held (version 409, archived 409, move-fields 422, goal authority 403, provider-owned 409, blocked-without-blocker 422, duplicate key 422, creation-id replay) |

Everything else is **unhandled**: MSW's `onUnhandledFrame: "error"` rejects the request at the
interceptor without a network attempt, so a case that silently stops being scripted reddens
instead of passing. This is stricter than every deleted double (each of them answered 404/500 or, in
router's and goal-attention's case, *any* path with a workspace body).

#### Registered defaults vs the deleted doubles (enumerated; no case provokes an extra answer)

The unified table answers some paths an individual deleted double refused. The deltas are
deliberate (one table plus per-case `serve` layers, which still see every request first), so
"no wider" is a checkable claim instead of an assertion; `connections` routed unmatched paths to
the case's own handler, which answered 404 unless the case scripted them.

| registered default | deleted doubles that answered differently |
| --- | --- |
| `GET /portal/api/attention` (200, derived) | detail, planning, connections, runtimes, control, assignment and chat refused it (404); router derived it and goal-attention scripted it |
| `GET /portal/api/work-items/:id/timeline` (200, `{items: [], next_before: null}`) | detail, planning, connections, runtimes and chat refused it (404); control answered the detail envelope from its `work-items/` prefix (its timeline harness scripted real cursors); assignment answered the same empty body; router and goal-attention answered the workspace catch-all |
| `GET /portal/api/chat` (200, empty conversation) | detail, planning, connections, runtimes, control and assignment refused it (404); chat scripted it; router and goal-attention answered the workspace catch-all |
| `GET /portal/api/connections` (200, seeded) | detail, planning, runtimes, control, assignment and chat refused it (404); router and goal-attention answered the workspace catch-all |
| `GET /portal/api/work-items/:id` for an unseeded id (200, derived `Item` row) | detail matched only its scripted subject id; connections, runtimes and chat refused it (404); planning, assignment and control derived a row per read |
| `GET /portal/api/workspace`, unscoped (the seeded selection's full rows) | planning's deleted double served every project's `work_items: []` for the unscoped read — the one workspace read where the unified table is wider |
| registered writes with no responder (transport error) | the deleted doubles' fallback 404/500: the same failure in a different carrier, named in the branch-preservation audit |

The workspace row is the clause-3 delta the review found unasserted: the planning board cases now
pin the scoped read (`/portal/api/workspace?project_id=…` present, the unscoped path absent;
`planning.test.tsx` "does not apply a late board move…" and "moves the focused card…"), and the
fix-stage mutations that drop the route→project scope (the project page's read, then the shell's
directory read) redden exactly those assertions.

## Exact edge-case semantics the contract demands

1. **Names and asserted values frozen (D1).** No test name, selector, aria name, refusal
   sentence or asserted value changes. What may change is the *accessor expression* that reads a
   recorded request (`calls[i].init.body` → `portal.requests[i].body`, `fetcher.mock.calls[0][0]`
   → `portal.requests[0].path`); each rewrite is listed case-by-case with the same observables
   (path, method, body, headers) asserted.
2. **Body decoding.** The double decodes the JSON string body once at record time, so the
   suites' `JSON.parse(init.body) as {...}` casts disappear; a non-JSON or absent body records
   `body: null` and keeps `rawBody` for the rare raw-text assertion.
3. **Failure injection stays explicit.** A case needing a non-HTTP failure either returns
   `HttpResponse.error()` from its own resolver (the double's table is the carrier; the client
   sees a rejected fetch) or keeps an injected fetcher; the artifact names each such case and
   why. `loseNextCreateResponse` (state commits, answer dies at the transport) is exactly
   `HttpResponse.error()` after the state mutation.
4. **Gates and holds** are `serve` resolvers that await the case's promise before answering —
   the deleted doubles' `readGate`/`workspaceGate`/`holdNextMutation` become one mechanism.
5. **`window.scrollTo`**: no-op stub in `test-setup.ts`; nothing a case asserts is stubbed
   (no case asserts `window.scrollTo`; `chat.test.tsx`'s 27 scroll references are all
   `scrollTop` reads/writes on the pane element, which the stub does not touch).
6. **Config split**: the shape is chosen from measurement, not preference — `isolate: false`
   only if the shuffled run and the state inventory hold; otherwise a projects/pool split.
   `maxWorkers` is sized from measured per-file startup. The tool-cache expressions in
   `tool-cache-config.test.ts` stay byte-identical.

## Decisions

| # | decision | rationale |
| --- | --- | --- |
| P1 | Convert **nine** suites (planning, connections, goal-attention, chat, router, control, assignment, detail, runtimes), not only the five the contract names | clause 1 demands exactly one shared portal double; control/assignment/detail/runtimes each carry their own portal routing table today. `api.test.ts`/`application.test.ts` keep their per-case transport doubles: they test the fetch boundary and the application wiring itself, which the contract's own carve-out names, and their doubles are not portal surfaces. |
| P2 | `serve()` layer + seeded defaults instead of a catch-all-then-defaults chain | keeps MSW's `onUnhandledRequest: "error"` as the real unhandled mechanism; a suite resolver that answers nothing for a registered path is a defect and is caught by the same error path. |
| P3 | Implementation lands as one working tree, staged in the contract's two logical steps (config split + `scrollTo` first, double + dependency second) | the stage template forbids committing; the verifier/committer lands the two commits, and every gate below is run on the final tree. The step order is honoured in the *edits*: config/stub edits are verifiable alone (mutation (d)/(e) run against exactly those edits). |
| P4 | `pnpm install --trust-lockfile` on this host | the lockfile supply-chain policy verification hangs against the host's registry proxy (no policy feed); the contract's fail-closed gate is the registry check, which still runs. Recorded with its reason in the artifact. |
| P5 | The suite that cannot hold `isolate: false` (if any) goes to an isolated project/pool rather than reverting the whole change | conductor decision in the contract's change item 3. |

## Evidence plan (what will be run, and what proves what)

All foreground, serialized; no measurement harness beside a suite. Baseline rows are produced
**before** any edit (pre-change head `f1fe1e59`), head rows on the final tree.

| # | command | proves |
| --- | --- | --- |
| 1 | `pnpm install --trust-lockfile` then `python3 scripts/ci/check-lockfile-registry.py` | dependency lands; every recorded tarball is on `registry.npmjs.org` |
| 2 | `pnpm --filter @symmetry/frontend typecheck` | types hold |
| 3 | `pnpm --filter @symmetry/frontend lint` | `--max-warnings 229` not raised |
| 4 | `pnpm --filter @symmetry/frontend test` (default + json reporter) | pre/post executed count, per-file durations, wall time, names |
| 5 | `vitest run --sequence.shuffle` at the pre-change head | which files depend on ordering/isolation |
| 6 | baseline/head stderr greps for `Not implemented: window.scrollTo` | pre count + emitting caller; head 0 |
| 7 | mutation (a) one handler's method/path changed | the case reddens |
| 8 | mutation (b) one handler dropped | its cases redden, no silent pass |
| 9 | mutation (c) a refusal case answered 200 | the case reddens |
| 10 | mutation (d) `scrollTo` stub removed | the recorded pre-change line count returns |
| 11 | mutation (e) the config change flipped back | wall time returns to baseline |
| 12 | `git diff --check`; profile-path grep over the diff | hygiene + DLP |

## Out of scope (with reasons)

Exactly the contract's clause 4: the portal API's own behaviour, ChatPage decomposition (W4-08),
the `Api` signal split (W4-09), refusal classification (W4-10), the goal-settle poll and
`tool-cache-config.test.ts`'s wall-clock dependence (W5-04), `browser/**` (W4-14), ESLint config
(W1-10/W6-01), coverage thresholds (W2-17/W6-12). Additionally: no production `src/` module is
edited (the double and every helper live in test/owned files), and the two unit-double files
named in P1 keep their injected fetchers.

## Residual risks / known limits

- **Host contention.** This host is shared with other agent worktrees; the baseline and head
  runs record wall time and any worker bound used, so the contract's wall-time band is read
  against same-host conditions, and the count/name equality — not the wall clock — is the gate.
- **router.test.tsx** holds 21 doubles; the conversion is the largest single file change and the
  most likely to hide a branch. Its inventory (every double, branch and captured request state)
  is recorded in the artifact.
- `msw@3.0.5` behaviour that the design depends on (relative-URL normalization in Node,
  `onUnhandledRequest: "error"` on an unmatched path, `HttpResponse.error()` rejecting the
  client fetch) is probed empirically before the port; the probe result is recorded.

## Implement-stage notes (environment and probes, 2026-11-22)

All commands below ran in this worktree at `f1fe1e59` before the change.

| probe | command | result |
| --- | --- | --- |
| baseline suite | `pnpm --filter @symmetry/frontend test` | exit 0 — **21 files / 534 tests**, 33.67 s, wall 34.6 s |
| jsdom noise | stderr grep `Not implemented` | **349** occurrences of `Not implemented: Window's scrollTo() method`; jsdom prints no caller, the router's scroll restoration is the only caller path |
| shuffle, no isolation | `vitest run --no-isolate --sequence.shuffle --sequence.seed=20261122` | exit 0 — 21/534, wall 35.8 s (aggregate file time 80.8 s) |
| shuffle, isolated | `vitest run --sequence.shuffle --sequence.seed=20261122` | exit 0 — 21/534, wall 42.0 s (aggregate file time 94.2 s) |
| heaviest files | from the json reporters | connections 24.3 s / 57 tests, chat 16.8 s / 48, goal-attention 12.6 s / 29, planning 11.1 s / 35, router 6.2 s / 42 |

Environment findings that shape the delivered tree:

- `pnpm install` on this host hangs in "Verifying lockfile against supply-chain policies"
  (the registry proxy serves no policy feed), so every install ran with `--trust-lockfile`;
  the contract's fail-closed gate (`scripts/ci/check-lockfile-registry.py`) still runs on the
  delivered lockfile.
- Adding `msw` through the mirror recorded mirror tarball URLs and, for 62 entries, legacy sha1
  integrities. The delivered lockfile is repaired to canonical form: pre-existing entries are
  byte-restored from the previous lockfile, the 40 new closure entries carry
  `https://registry.npmjs.org/...` with the sha512 the registry metadata reports (sha1 only where
  the metadata has no sha512). `check-lockfile-registry.py` then reports **303 entries checked,
  exit 0**, and `pnpm install --trust-lockfile --frozen-lockfile` exits 0 with no download.
- MSW v3 names the unhandled-request strategy `onUnhandledFrame` (`onUnhandledRequest` is not a
  member of v3's listen options; the v2 spelling is silently ignored). `listen({ onUnhandledFrame:
  "error" })` prints `[MSW] Error: intercepted a request without a matching request handler` and
  the request is rejected at the interceptor — no real network attempt (apply-stage re-probe:
  `error` rejects, `bypass` reaches a live local server) — the contract's mechanism.
- Probes that the design depends on: a **relative** `fetch("/portal/api/...")` is matched and
  answered by a relative handler path; a handler returning `HttpResponse.error()` rejects the
  client fetch; a plain `new Response(...)` returned by a handler is delivered normally. The
  implement-stage probe that reported two resolver invocations per request ran with **two** MSW
  servers listening (the project `setupFiles`' `portalServer` plus the probe's own
  `setupServer`); in the delivered shape — one `setupServer`, held and gated handlers included —
  the resolver runs exactly once per request (`executeHandlers` once per frame), so the
  duplicate-resolution memo built on the two-server reading was dead code and is removed (R2).
- Defect found and fixed during the reference migration: two default-table responders returned a
  plain object instead of a `Response` (the array literal is annotated `PortalEntry[]`, so the
  type checker must fail on that shape — it now does, and the gate confirms it). Those objects
  surfaced as 12 unhandled rejections per file (`Reflect.has called on non-object` inside
  `@mswjs/interceptors`); after the fix the reference suites run with an empty stderr.

## Implement-stage result (final tree, 2026-11-22)

### Final tree census

Republished 2026-11-22 at the fix stage against the delivered tree (`wc -l`); the implement-stage
values recorded a pre-apply revision, and the apply-stage census near the end of this file records
the apply revision.

| file | base LOC | head LOC | delta |
| --- | ---: | ---: | ---: |
| `app/router.test.tsx` | 1,328 | 1,215 | −113 |
| `features/chat/chat.test.tsx` | 2,086 | 2,062 | −24 |
| `features/connections/connections.test.tsx` | 1,535 | 1,521 | −14 |
| `features/goals/goal-attention.test.tsx` | 1,342 | 1,323 | −19 |
| `features/planning/planning.test.tsx` | 1,018 | 792 | −226 |
| `features/runtimes/runtimes.test.tsx` | 127 | 111 | −16 |
| `features/work/assignment.test.tsx` | 395 | 376 | −19 |
| `features/work/control.test.tsx` | 516 | 481 | −35 |
| `features/work/detail.test.tsx` | 132 | 128 | −4 |
| **all 21 test files** | **11,624** | **11,154** | **−470** |

Harness modules: `app/test-harness.tsx` 61 → 29, `services/api.fixtures.ts` 246 → 246,
`test-setup.ts` 19 → 31, `app/portal-double.ts` 0 → **460** (the one shared double). Whole
test-side net (21 test files + the three harness modules + the double): 11,704 → 11,674,
**−30** — the test files lose 470 lines, the harness modules lose 20 net (`test-harness.tsx` −32,
`test-setup.ts` +12), and the double adds 460. The contract's `−400…−600 test` band is met by the
test files alone (−470) and is report-only (D2).

### Name/count/observable equality (the clause-1 pin)

| evidence | result |
| --- | --- |
| pre-change head name set (json reporter, `--no-isolate --sequence.shuffle`) | 534 names |
| head name set (json reporter, default config) | 534 names |
| `diff` of the two sorted sets | **0 lines** |
| static check: `it(`/`it.each(`/`test(`/`describe(` name strings, HEAD vs tree, all 21 files | **0 of 21 files changed** |
| stderr at head (default config, 21 files) | **0 bytes** — no MSW unhandled-request error, no unhandled rejection |
| jsdom `Not implemented: Window's scrollTo() method` lines | baseline **349** → head **0** |

### Deleted definitions → configuration sites (per file, HEAD coordinates)

| file | deleted | replaced by |
| --- | --- | --- |
| `features/planning/planning.test.tsx` | `HarnessResource` 29-33, `HarnessProject` 35-48, `HarnessState` 50-54, `RecordedRequest` 56-60, `baseState()` 86-143, `projectOf`/`itemOf` 145-155, `text()` 157-159, `Harness` 161-171, `createHarness()` fetch double 173-303 (`json`, `workspaceBody`, 11 route branches), `renderWorkspace(path, harness)` 308-315, `postsOf`/`patchesOf` 317-318, `sessionToken` 20 | `PortalProject`/`PortalWorkItem` seed (`baseProjects()`/`seedWorkspace()`), the double's default table for the reads and the five write routes, `portal.state.mutations.{failNext,loseNextCreateResponse}` + `portal.holdNextMutation()`, `postsOf(path)`/`patchesOf(path)` over `portal.requests`, `renderRoute` |
| `features/connections/connections.test.tsx` | `recordingFetcher` import 6, `Harness.requests` 101-111, `renderConnections` routing block 115-168 (session 133-135, connections read 136-140 with gate/failure, workspace read 141-152 with gate/failure/scoping, per-case fallback 153, record 129-132), `current`/`projects`/`registeredRuntimes`/`readFailure`/`workspaceFailure`/`readGate`/`workspaceGate` closures 120-126, local `requests` 127, `unexpectedRequest()` 183-185, 404 stand-ins 189/200 | seed `connections`/`workspace.{projects,registeredRuntimes,selectedProjectId}` + `portal.state` mutation for `setConnections`/`setProjects`, read-control `serve` layer for gates/failures, the case's `handler` as its `serve` layer with `undefined` fall-through, `portal.requests` for all 45 assertions |
| `features/goals/goal-attention.test.tsx` | `portalFetcher(handler)` 23-35 (session stub 28, catch-all workspace 29-30), `workspaceResponse` 33-35, stale banner 20-22 | `beforeEach(() => portal.seed({ workspace: … }))`, per-case `portal.serve` (29 sites), default table for session/workspace, `portal.requests`/`portal.sent` for the three request assertions |
| `features/chat/chat.test.tsx` | `RecordedRequest` 167-172, `HarnessOptions` doc comments, `renderChat`'s `vi.fn<typeof fetch>` 191-211 (session 196, workspace 197, chat read 198-202, message POST + `messagePosts` 203-206, 404 catch-all 207), local `requests` 189, `applications` + `afterEach` 182-186, returned `requests` 217 | `renderRoute(path)` + `portal.serve` layer answering the same branches (`((options.workspace ?? workspaceBody)())`, `options.conversation?.(request.url)`, `messagePosts += 1; options.message?.(request.body ?? {}, messagePosts)`), `trackApplications()`, `portal.requests`/`posts()` |
| `app/router.test.tsx` | `servedFetcher()` 29-49, `workReadFetcher()` 356-372, `movableSubjectFetcher()` 382-401, `historyFetcher()` 747-765, `chatFetcher()` 1021-1037, 16 anonymous per-case doubles (159-164, 176-184, 216-220, 229, 237-245, 279-283, 305-312, 463-475, 636-663, 711-720, 820-844, 883-907, 930-957, 975-998, 1161-1167, 1254-1275), the `fetcher` parameter of `renderWorkspace` | `seedProjectsWorkspace`/`seedWorkView`/`serveWorkViewProjects`/`serveMovableSubject`/`serveAgedOutHistory`/`serveHistoryReads` seeds and `serve` layers (21 doubles → 21 configuration sites, table row by row in the migration report), `renderWorkspace(path, realtime?)` on the intercepted fetch, and a default inert `socketFactory` so a seeded socket token cannot make an incidental shell connect reach MSW's WebSocket interception |
| `features/work/control.test.tsx` | `workspaceBody()` 23-47, `RecordedRequest` 63-68, `accepted` raw `new Response` 71, `HarnessOptions` 73-80, `applications`+`afterEach` 82-86, `unscriptedResponse` 90-93, `renderDetail(fetcher)` 96-105, `renderWorkDetail` fetcher routing 126-155, `renderTimelineHarness` fetcher 493-516 | workspace seed, `PortalRequest` log via `posts()`, `jsonResponse(body, 202)`, `renderRoute`, seed + `serve` layers (detail by read index, commands, timeline cursors), `trackApplications()` |
| `features/work/assignment.test.tsx` | `recordingFetcher` import 4, `workspaceBody(requested)` 51-62, `RequestRecord` + `requestRecord` 96-107, `HarnessOptions` 109-114, `renderWorkDetail` fetcher routing 117-154, `countReads` over `calls` 138-139, `requests` getter 142, `patches()` 152 | workspace/details seed, `PortalRequest` (body pre-decoded), `serve` layer for the assignment PATCH and the workspace override, `countReads`/`patches()` over `portal.requests` |
| `features/runtimes/runtimes.test.tsx` (reference) | `renderRuntimes` inner fetcher (session, scoped/unscoped workspace) + `vi.fn<typeof fetch>` | seed-less `serve` layer for the scoped/unscoped workspace answers, `renderRoute`, `portal.requests` for the workspace-request assertion |
| `features/work/detail.test.tsx` (reference) | `renderWorkDetail` inner fetcher (session, work-item detail, workspace) | `portal.seed({ workspace, details })` + `renderRoute` |

### Accessor rewrites (case by case; every row asserts the same observables)

`planning.test.tsx` (13 request-log sites, below; old text is the same expression with `harness`
inserted):

| case | old → new |
| --- | --- |
| `postsOf`/`patchesOf` helpers 124-125 | `harness.requests.filter((r) => r.method === "POST"/"PATCH" && r.path === path)` → `portal.requests.filter(…)` |
| submits a new project once (271) | `postsOf(harness, "/portal/api/projects")` → `postsOf("/portal/api/projects")` |
| keeps the editor open with the server's field error (291) | same rewrite |
| replays the failed intent verbatim (307) | `patchesOf(harness, \`…/projects/${firstProjectId}\`)` → `patchesOf(…)` |
| keeps a draft across a background refresh (380) | same |
| clears the optional fields (417) | `patchesOf(harness, \`…/work-items/${firstItemId}\`)[0]` → `patchesOf(…)[0]` |
| restores an archived project (435) | `patchesOf(harness, \`…/projects/${secondProjectId}\`)[0]?.body` → `patchesOf(…)[0]?.body` |
| does not apply a late board move (446) | `patchesOf(harness, \`…/move\`)` → `patchesOf(…)` |
| creates a work item from the toolbar (478) | `postsOf(harness, \`…/work-items\`)[0]?.body` → `postsOf(…)[0]?.body` |
| mints one creation identity (505) | `postsOf(harness, …)` → `postsOf(…)` |
| answers a lost create response (527) | same |
| keeps the pending creation identity (549) | same |
| refuses a create visibly (568, 574) | `postsOf(harness, …)` lengths 0 then 1 → over `portal.requests` |
| moves the focused card (586, 587) | `patchesOf(harness, …/move)` → `patchesOf(…)` |

Two further rewrite families in the same file (asserted values byte-identical; every row keeps its
observables):

| family | old → new |
| --- | --- |
| state readers, 15 sites (280, 295, 303, 311, 332, 350, 386, 403, 434, 482, 497, 523, 588, 712, 774) | `projectOf(state, …)` / `itemOf(state, …)` over the local harness state → `projectOf(portal.state.workspace, …)` / `itemOf(portal.state.workspace, …)` |
| request-count filters, 4 sites (576, 761, 781, 790) | `harness.requests.filter(…)` (the `PATCH` count and the workspace `GET` count) → `portal.requests.filter(…)` |

`chat.test.tsx` (4 sites): "leaves another scope's draft alone" (389/394) `const { requests } = renderChat(…)` → `renderChat(…)` and `requests.filter(…)` → `portal.requests.filter(…)` (chat-GET count for `scope=project`); "settles a scope's pane on its own offset" (596/634) same for `scope=workspace`; "keeps the composer focused across a background refresh" (757/765/771) `requests.filter(r => r.path.startsWith("/portal/api/chat?"))` → `portal.requests.filter(…)` (read count before/after refetch); `posts()` (218) → `portal.requests.filter(POST)`.

`goal-attention.test.tsx` (14 rows): universal `const { fetcher/… } = portalFetcher((path, init) => {…})` → `portal.serve((request) => {…})` with `new URL(path, "http://localhost")` → `request.url`; `renderRoute(path, fetcher)` → `renderRoute(path)` (29 sites); `calls.some(c => c.path.includes("cursor=cursor-one"))` (112) → `portal.requests.some(…)`; `init?.headers` → `request.headers` (153-156, 169); `JSON.parse(rawBody) as {…}` → decoded `request.body` at 169, 292, 315, 334, 375-377, 416-417, 450, 474-476, 555, 611, 665, 743, 752-753, 784-786, 832-833, 842, 867-869, 929-930, 955, 974-975, 1027, 1072, 1118, 1123, 1159, 1172, 1211; `calls.filter(…"/commands")` → `portal.requests.filter(…)` (315); `JSON.parse(bodies[1] ?? "{}") as {…}` → `portal.sent("POST", …)[1]?.body` (429, 884, 991); `throw new TypeError("network down")` → `HttpResponse.error()` (396, 628, 859, 972, 1142); 17 `JSON.parse(` casts → 0.

`connections.test.tsx` (45 sites, all `harness.requests` → `portal.requests`; observables in brackets): submits a new connection once (227 POST count, 229 body `toEqual`); stale capability update (263 body); removal version (337 DELETE path with `?version=3`); draft version (416 `.some(PATCH)`, 417 body `toMatchObject({version:3})`); mutual exclusion (440/447 POST count, 469/470/477, 499/500/507, 558/565, 593 counts); another connection selected (695 GET count = 3); refresh re-reads (909, 939 workspace read deltas); provider catalog (990 POST count, 991 body `toEqual`); name-only edit (1033 body); replayed create (1051 count); resource create (1076/1077/1078 count/path/body, 1110/1132 paths, 1167/1168 count/body); refused sync (1263/1272 workspace re-read count), project sync receipt (1319 path), aggregate failure (1331/1334 counts); resource update (1355/1356 count/body), unbind (1387/1388 count/path); control locks (1413/1421 counts); late response (1464 sync count), late editor response (1481/1490 read deltas).

`control.test.tsx` (2 sites): session rotation — `harness.requests.filter(r => r.path === "/portal/api/session")` → `portal.requests.filter(…)`; `harness.posts()` → `portal.requests.filter((r) => r.method === "POST")` (receipt and mutation cases; the header assertions read the double's lower-cased `Headers` snapshot: the `x-csrf-token` positive read and the `x-csrftoken` duplicate check — the spelling a duplicated camelCase `x-csrfToken` header collides with — which the apply-stage mutation check reddens, F14).

`assignment.test.tsx` (4 helpers; assertions unchanged): `patches()` → `portal.requests.filter((r) => r.method === "PATCH")`; `detailReads(id)` → `r.url.pathname === \`/portal/api/work-items/${id}\``; `projectReads(id)` → `r.url.searchParams.get("project_id")`; `requestRecord` deleted (`PortalRequest` carries the decoded body).

`router.test.tsx`: **none** — the file had no request-recording assertion; every assertion reads DOM/history (verified: 0 `fetcher.mock.calls`, 0 `calls[`).

`runtimes.test.tsx`: `fetcher.mock.calls` workspace-request list → `portal.requests.filter((request) => request.path.startsWith("/portal/api/workspace")).map((request) => request.path)`, asserting the same `toContain`/`not.toContain`.
`detail.test.tsx`: none.

### Branch-preservation audit (lesson 32, per converted double)

`planning`: session/workspace/detail reads → default table; `failNextMutation` → `portal.state.mutations.failNext` (non-GET only, after the read routes, before the hold); `holdNextMutation` → `portal.state.mutations.hold`; `loseNextCreateResponse` → `portal.state.mutations.loseNextCreateResponse` (consumed inside the create branch after the row commits, before the 201); duplicate key 422, blank title 422, creation-id replay 200, project 404/409 stale/409 state_conflict, move 403 goal/409 stale, item 422 move-fields/403/409 stale/409 provider_owned/422 blocked-without-blocker — **all preserved**, verified branch by branch against the head double; the fallback 404 is intentionally gone (unhandled → MSW error).
`connections`: session, connections read (gate → failure → list), workspace read (gate → failure → scoped body), 502/409/404/422/403 `provider_unauthorized`/`provider_failure` branches, all pending/hold branches, read-count observables — preserved; deltas recorded: an unscripted scripted-only write answers `HttpResponse.error()` (one case, its assertions are the recorded POST count/body), `work_items` scoping added (every fixture carries `[]`), `body: null` instead of `undefined` for body-less requests (unasserted). No 401 branch exists in this suite.
`goal-attention`: cursor pagination (two first-page branches + `firstPageReads` counter), `goalUnavailable` refusal, session-expiry 401, refusal codes 422 `invalid_contract`/409 `stale`/403 `forbidden`/422 `invalid_plan`/`invalid_decision`, 200 undecodable/empty receipts, 5 thrown transports → `HttpResponse.error()` — preserved; the catch-all workspace fallback is intentionally gone (no case needed it).
`chat`: per-scope conversation bodies and their `scope`/`before` conditions, 13 `Promise.withResolvers` holds (late read, late write, pane settle, cursor renew, refused press, 8k, three in-flight pages, scope switch, same-cursor-bytes), 409 `idempotency_conflict`, 400/500 older-page refusals, 403 read failures, three thrown `TypeError`s, one rejected held promise, 1-based `postIndex` — preserved; the unscripted message-POST 500 default and the 404 catch-all are gone (no case relied on them; unscripted now fails loudly).
`router`: 400 aged-out timeline, 401 bootstrap expiry, 403 recoverable workspace error, 404 `project_id=missing`, held session/first-project/bootstrap reads, `holdDetail`, mutable `subjectStatus` projection, dynamic detail reads, scoped attention where a case asserts it — preserved; the 404 catch-all was unexercised; the default attention derivation is scope-aware (`project_id` → explicit select → first project) and the four cases that read unscoped attention assert no blocked row; the scoped layer one case carried at implement time duplicated that derivation and was removed in the apply stage (F5), its DOM/history assertions unchanged.
`control`: session rotation by read index, detail by read index, command acceptances (202), refusals (409/403/default 500), a never-settling second detail read, timeline cursor reads, `Promise.reject` → `HttpResponse.error()`, the "must not reach the server" tripwire (a sent POST would be logged and answered by the layer's default 500 responder) — preserved.
`assignment`: session/workspace/detail/timeline reads, 403 refusal, gated workspace read, per-item detail overrides and the live `intent_locked` flip via `portal.state.details`, deferred PATCH for the late save, 409 `provider_owned` — preserved.

No case in any converted suite kept an injected fetcher, and no assertion was deleted or weakened.

### Config shape, measured (final tree, serialized runs, same host)

| shape | members | suite `Duration` (repeated runs) | stderr | exit |
| --- | --- | --- | --- | --- |
| pre-change baseline | forks, isolate (vitest default) | 33.67 s | 349 scrollTo lines | 0 |
| A | forks, isolate (default at head) | 46.74 / 67.26 / 39.18 s | 0 | 0 |
| B | forks, `isolate: false` | 41.61 / 43.53 s | 0 | 0 |
| C | forks, `isolate: false`, `maxWorkers: 4` | 49.47 s | 0 | 0 |
| D | `pool: threads`, `isolate: false` | 49.13 s | 0 | 0 |
| E | `pool: threads`, `isolate: false`, `maxWorkers: 4` | 36.75 / 32.28 / 33.55 s | 0 | 0 |
| E4 | `pool: threads`, `isolate: false`, `maxWorkers: 50%` | 34.92 s | 0 | 0 |
| E5 | `pool: threads`, `isolate: false`, `maxWorkers: 8` | 48.09 s | 0 | 0 |
| **delivered** | `pool: threads`, `maxWorkers: "50%"`, isolation at the default | **32.82 s** (contract command) / 33.32 / 33.34 s (shuffled on the final config: 34.77 s) | 0 | 0 |

Delivered shape: `pool: "threads"` + `maxWorkers: "50%"`, isolation left at vitest's default —
the pool split the contract's change item 3 names as the alternative to `isolate: false`.
Measurements behind it:

- **The pool is the lever.** Threads collapse the per-file import share (isolation on: 24% of a
  46-47 s run; threads: 8-19%) because workers in one process share the module graph. Every
  threads run (32.3-38.8 s) beat every isolation-on forks run (39.2-67.3 s) on this host.
- **`isolate: false` was measured and not adopted.** Under forks it looked like −10% (B 41.6/43.5
  vs A 46.7/67.3/39.2), but under threads the two repeated json-measured pairs are
  indistinguishable: aggregate per-file time 86.6/83.4 s (isolate off) vs 85.0/86.2 s (isolate
  on), and wall time 32.8-41.8 s (off) vs 33.3-40.2 s (on). No measured benefit, and keeping
  isolation removes the whole class of cross-file module-state risk, so the delivered file keeps
  it. (The pre-change shuffle probes are still the record for the file set that would have taken
  it: `--no-isolate --sequence.shuffle` was green at the pre-change head, 21/534.)
- **Worker count is a percentage** so a smaller host scales down; four workers (E) and eight
  (E4) measured within noise of each other.
- **The shape is in effect in the delivered file**: with the config in place a worker reports
  `isMainThread: false, threadId: 1` (threads), while `--pool=forks` reports
  `isMainThread: true`.

Wall time: the contract's own command measures **32.82 s** on the delivered tree against the
33.67 s pre-change baseline, i.e. the pool split recovers slightly more than the double's
interception cost (aggregate file time 80.7 s → 102.5 s). The plan row's −30…−50% band (from a
~114 s basis) is reported, not achieved: that basis never held on this host (D2: report-only).

### Mutation checks (each applied alone, run, reverted; portal-double.ts sha256 restored to `82a31524670f…` after every mutation, then to the delivered `143d4aa564ea…` by the lint-gate fixes)

| # | mutation | target | result |
| --- | --- | --- | --- |
| a | `PATCH /portal/api/work-items/:id` entry changed to `POST` | `planning.test.tsx -t "clears the optional fields"` | **exit 1**, 1 failed / 34 skipped — the case's request assertion and its DOM outcome redden |
| b | `GET /portal/api/work-items/:id` handler dropped (`respond: undefined`) | `detail.test.tsx` (7 cases) | **exit 1**, 7 failed / 7 — no silent pass, the read fails as a transport error |
| c | `stale()` refusal replaced by `200 {project: {}}` | `planning.test.tsx -t "surfaces a stale refusal"` | **exit 1**, 1 failed / 34 skipped — the permissive answer reddens the case that pins the refusal |
| d | `window.scrollTo` stub removed from `test-setup.ts` | full suite | exit 0, 21/534, and the stderr carries **349** `Not implemented` lines — the recorded pre-change count, exactly |
| e | `isolate: false` (the shape this slice measured but did not adopt) flipped against the delivered default | full suite | exit 0, 21/534 either way; `Duration` 40.18 / 32.76 / 38.76 s with the flip applied vs 33.32 / 33.34 s with the default — inside this host's run-to-run variance, which is exactly why the delivered file keeps isolation |
| e' | the delivered shape's pool flipped back (`--pool=forks`) | full suite | exit 0, 21/534, `Duration` 39.18-67.26 s (forks) vs 32.28-38.76 s (threads) — the decisive config flip, recorded because (e) showed nothing |

### Gate results (final tree, foreground, serialized; no measurement harness beside a suite)

| gate | command | exit | counts |
| --- | --- | ---: | --- |
| install (plain) | `pnpm install` | 0 | `Already up to date`, 17 ms — the policy-verification stall of the intermediate state is gone once the edited lockfile has settled |
| install (host flag, frozen) | `pnpm install --trust-lockfile --frozen-lockfile` | 0 | no download; needed only while the lockfile was newly written |
| lockfile registry | `python3 scripts/ci/check-lockfile-registry.py` | 0 | `303 entries checked`, every recorded tarball on `registry.npmjs.org` |
| typecheck | `pnpm --filter @symmetry/frontend typecheck` | 0 | no output |
| lint | `pnpm --filter @symmetry/frontend lint` | 0 | 223 problems (0 errors, 223 warnings); ceiling `--max-warnings 229` untouched |
| suite (contract command) | `pnpm --filter @symmetry/frontend test` | 0 | 21 files / 534 tests; 32.82 s before the lint-gate fixes and 60.75 s on the delivered bytes — this host's run-to-run spread, see the shape table; stderr carries only pnpm's `$ vitest run` banner |
| suite (with json, name pin) | `vitest run --reporter=default --reporter=json` | 0 | 21 files / 534 tests, 0 stderr bytes, name set identical to the pre-change run |
| suite (shuffled, delivered config) | `vitest run --sequence.shuffle --sequence.seed=20261122` | 0 | 21 files / 534 tests, 0 stderr bytes, 34.77 s |
| diff hygiene | `git diff --check` | 0 | clean |
| DLP | profile-path and backslash-u greps over the diff and the two new files (commands and exit codes in the fix-stage annotation; both select no line) | 1 | no match — 0 profile paths, 0 backslash-u sequences in the delivered tree |

### Closeout state

The repo-side increment is this plan file plus the owned sources; the contract file
(`<HOST-PATH>/.local/state/gdd/contracts/goal-rf-w4-11-frontend-test-harness.md`) and the GDD
ledger row are conductor-owned and outside the repository: nothing is committed, and nothing in
the repository is archived or re-indexed here.

- **Documents made stale**: none. `docs/design/*.md` and `frontend/AGENTS.md` name no test
  double, harness module or vitest config member (`grep` over both trees); the only MSW
  reference in-repo is `docs/goals/0010-refactor-program.md:148`, decision D13 — "Adopt in the
  test-harness slice (W4-11)" — which this slice makes true. The wave-table row for W4-11
  (`docs/goals/0010-refactor-program.md:259`) carries a conductor-owned queue status.
- **`docs/TODO.md`**: one entry added (the WebSocket-interception trap for a future suite that
  seeds a socket token). No existing entry names a symbol or file this slice deletes or moves;
  `docs/TODO.md:616` cites `frontend/src/services/api.fixtures.ts:44`, whose line number and
  text are unchanged (only three `interface` declarations gained `export`, on their own lines).
- **Lockfile caveat for the conductor**: 23 of the 40 added closure entries record
  `sha1-` integrities because this host's mirrored metadata publishes only the legacy `shasum`
  for those packages (e.g. `@types/debug@4.1.13`); the digest was re-derived as base64 of the
  same bytes, so a canonical install verifies the same content. The registry checker verifies
  tarball hosts only — its own `docs/TODO.md` entry records that limit.
- **Not committed** (stage rule): the tree is left for verification with 16 modified files and
  two new files, all inside the contract's owned set.

## Annotation (2026-11-22, apply stage)

The accepted simplify findings are applied to the working tree (not committed; nothing pushed or
rewritten). Code changes and the record corrections made above:

- **F1** `portal-double.ts`: `scriptedOnly` and the 18 `respond: scriptedOnly` entries deleted;
  `dispatch`'s `entry.respond?.(call, state) ?? HttpResponse.error()` is the one failure path for
  a registered-but-unscripted request (same transport error, paths still registered and logged).
- **F2** `test-harness.tsx`: the callerless `options.fetcher` seam deleted; failure injection
  stays available through `serve` layers returning `HttpResponse.error()` (edge-case semantics 3),
  and no converted case ever needed the seam (above).
- **F3** `chat.test.tsx`: `renderChat`'s unused `history` return deleted.
- **F4** `api.fixtures.ts`: no option interface is exported any more — `ReadWorkItemOptions` and
  `ReadWorkDetailOptions` revert, and `ReadAttentionOptions` follows once F7 removes its last
  importer; the census phrase "three option interfaces exported for the double's seed types" is
  superseded by this annotation.
- **F5** `router.test.tsx`: the scoped attention layer (implement `:74-83`) deleted; its case now
  runs against the double's scope-aware default derivation and the `readAttention` import is
  dropped; all DOM/history assertions unchanged.
- **F6** `planning.test.tsx`: `renderWorkspace`'s inert `location` override and parameter deleted
  (`location.href` is read only on the login-redirect path; no planning case scripts a 401, and
  `replace` is unasserted there).
- **F7** `portal-double.ts`: the never-seeded `attention` and `timeline` seed groups deleted; the
  API-shape sketch, seed-groups paragraph and default-table rows above are corrected accordingly.
- **F8/F9** `router.test.tsx`: `serveHistoryReads` reuses `serveWorkViewProjects()`; the CI-tone
  case seeds the existing `subjectDetail()`.
- **F10** `planning.test.tsx`: applications tracked with `trackApplications()`; the extra
  `localStorage.clear()`/`resetPendingWorkItemCreations()` hook is registered *before* it, so the
  LIFO order (query clients first) is unchanged.
- **F11** `portal-double.ts`/`planning.test.tsx`: one exported `workItem` row builder serves both
  the double's creations and the suite's seeds (the suite's private copy deleted); the double's
  `itemOf`/`projectOf` are exported and the suite's duplicate copies deleted, and their 15
  state-reader call sites now pass `portal.state.workspace` (listed in the accessor section above).
- **F12**/**PD2** `portal-double.ts` header trimmed to the failure-semantics statement, corrected
  (the fix stage then removed that header entirely under the comment policy, R8): an unhandled
  path is rejected at the MSW interceptor without a network attempt. Re-probe on the
  delivered bytes: with `onUnhandledFrame: "error"` the client fetch rejects and a live local
  server on the target port is not reached; with `"bypass"` it answers 200 from the network. The
  two plan sentences and the probe row above are corrected with it.
- **F13** `vitest.config.ts`: the pool comment restated to the measured fact (vitest's own
  diagnostics print the `isolate: false` suggestion — that switch is what shares modules across
  files; the delivered config keeps isolation at the default).
- **F14**/**PD1** `control.test.tsx:190`: the duplicate-header assertion now reads `x-csrftoken`,
  the spelling a duplicated camelCase `x-csrfToken` header collides with in the double's
  lower-cased `Headers` snapshot. Mutation check: a temporary second header in `services/api.ts`
  (`"x-csrfToken": "duplicate"`) reddens the three `it.each` mutation cases (exit 1,
  `expected 'duplicate' to be undefined`); reverted, the same filter is green (3 passed). PD1 is
  resolved by this repair, so "no assertion was deleted or weakened" holds.
- **PD3**/**PD5**: the `control` and `router` branch-audit sentences above corrected to the
  delivered layer behaviour.

Apply-stage census (`wc -l`, measured):

| file | base `f1fe1e59` | pre-apply `5a22476` | apply tree | apply Δ |
| --- | ---: | ---: | ---: | ---: |
| `app/portal-double.ts` | 0 (new) | 498 | 477 | −21 |
| `app/router.test.tsx` | 1,328 | 1,229 | 1,215 | −14 |
| `features/planning/planning.test.tsx` | 1,018 | 819 | 783 | −36 |
| `features/chat/chat.test.tsx` | 2,086 | 2,063 | 2,062 | −1 |
| `features/work/control.test.tsx` | 516 | 483 | 483 | 0 |
| `app/test-harness.tsx` | 61 | 32 | 31 | −1 |
| `services/api.fixtures.ts` | 246 | 246 | 246 | 0 |
| `vitest.config.ts` | 41 | 45 | 45 | 0 |
| all 21 test files | 11,624 | 11,200 | 11,149 | −51 |

(The rows above record the apply revision; the delivered census in the implement-stage result is
the authoritative tree measurement.)

Gates re-run on the apply tree (foreground, serialized; the host's pnpm is 12.21.0 against the
project's 12.17.0 pin, so every command carries `--pm-on-fail=ignore`, a host-version escape and
not a project change; the apply set touches no dependency file, so `pnpm install` and
`scripts/ci/check-lockfile-registry.py` are not re-run):

| gate | command | exit | counts |
| --- | --- | ---: | --- |
| typecheck | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend typecheck` | 0 | no output |
| lint | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend lint` | 0 | 223 problems (0 errors, 223 warnings), ceiling 229 untouched |
| suite (contract command) | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend test` | 0 | 21 files / 534 tests, 33.78 s |
| suite (json, name pin) | `… exec vitest run --reporter=default --reporter=json` | 0 | 534/534, stderr 0 bytes; no test-name declaration line changed in the diff |
| suite (shuffled) | `… exec vitest run --sequence.shuffle --sequence.seed=20261122` | 0 | 21 files / 534 tests, 56.75 s (host variance; the count, not the clock, is the pin) |
| diff hygiene | `git diff --check` | 0 | clean |
| DLP | `git diff` greps for a users-directory path and a backslash before `u` | 1 | no match |
| mutation (F14) | duplicate `x-csrfToken` header in `services/api.ts`, control suite `-t "sends the"` | 1 | 3 failed / 10 skipped; reverted → exit 0, 3 passed |

## Fix-stage annotation (2026-11-22, review round 1: findings R1–R8)

The review's `MUST_FIX` findings (R1, R2) and its record corrections (R3–R8) are applied in the
working tree (not committed; nothing pushed or rewritten). No production `src/` file is edited;
the temporary mutation edits of this round were reverted byte-identically.

| finding | change |
| --- | --- |
| R1 | `planning.test.tsx`: the two board cases now pin the workspace read's scope — `/portal/api/workspace?project_id=…` must be present and the unscoped path absent (the `workspaceReadPaths` helper, mirroring `runtimes.test.tsx`). Mutation checks: dropping the project page's scope and dropping the shell's directory scope each redden exactly those assertions (2 failed / 33 passed, `expected [...] to not include '/portal/api/workspace'`); both reverted to exit 0 / 35 passed. |
| R2 | `portal-double.ts`: the dead `answered` memo (and its "MSW resolves a handler twice" comment) deleted; the body is read once with `request.text()`; the plan's memo sentences and its probe row corrected (the two-invocation reading came from a two-server probe). |
| R3 | The "strictly no wider" claim replaced by the enumerated per-suite registered-default deltas above; no current case provokes an extra answer. |
| R4 | `PortalSeedInput` no longer accepts `mutations` (the group was silently reset by `cloneSeed`); mutation control stays on `portal.state.mutations`. |
| R5 | The final census, the harness-module LOC row and the test-side net arithmetic republished from the delivered bytes (test files 11,624 → 11,154, −470; whole test-side 11,704 → 11,674, −30); shape items 6/7 corrected; the router row's double list now names its 16 ranges (the two spurious ranges removed). |
| R6 | The accessor section now lists the 15 state-reader rewrites and the 4 request-count filters, and F11 no longer claims unchanged call sites. |
| R7 | The DLP row no longer carries the forbidden two-character sequence; the exact commands and exit codes are recorded below. |
| R8 | The slice-authored intent/JSDoc comments in `portal-double.ts`, `test-harness.tsx`, `assignment.test.tsx` and `control.test.tsx` dropped. `control.test.tsx`'s `session` comment line is untouched context (pre-existing, outside the slice's hunks) and is the recorded explicit exception. |

Fix-stage gates (foreground, serialized; host pnpm 12.21.0 against the project's 12.17.0 pin, so
every command carries `--pm-on-fail=ignore`; this round touches no dependency file, so the
lockfile is unchanged):

| gate | command | exit | counts |
| --- | --- | ---: | --- |
| install (plain, bounded) | `timeout 90 pnpm --pm-on-fail=ignore install --frozen-lockfile` | 124 | stalls at "Verifying lockfile against supply-chain policies (489 entries)" — the recorded host behaviour; the lockfile is unchanged in this round |
| install (host flag) | `pnpm --pm-on-fail=ignore install --trust-lockfile --frozen-lockfile` | 0 | `Lockfile is up to date`, 363 ms |
| lockfile registry | `python3 scripts/ci/check-lockfile-registry.py` | 0 | 303 entries checked, every tarball on `registry.npmjs.org` |
| typecheck | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend typecheck` | 0 | no output |
| lint | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend lint` | 0 | 223 problems (0 errors, 223 warnings), ceiling untouched |
| suite (contract command) | `pnpm --pm-on-fail=ignore --filter @symmetry/frontend test` | 0 | 21 files / 534 tests (the pre-change count), 34.80 / 35.0 s, 0 `Not implemented` lines, no MSW stderr |
| diff hygiene | `git diff --check` | 0 | clean |
| DLP profile path | case-insensitive Users-directory-fragment grep over the worktree diff (the fragment is not spelled literally here, so the check cannot self-match) | 1 | 0 matching lines (grep exits 1 when it selects none) |
| DLP sequence | `git diff origin/main \| grep -c '[\\]u'` | 1 | 0 matching lines; the same pattern selects the one pre-fix occurrence in `git diff e0782105 fe5b0690`, so the pattern is validated |
