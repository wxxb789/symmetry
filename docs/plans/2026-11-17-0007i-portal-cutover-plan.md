# 0007i single real-time Portal cutover and 0007 series release receipt — implementation plan

Goal contract: [docs/goals/0007i-portal-cutover.md](../goals/0007i-portal-cutover.md).
Base revision: `671525c7249b3234b64e4a83b2f4a82fbc663b75` (branch `feat/0007i-portal-cutover`).
Shape reference: 0007h (`671525c`, goal/attention workspace) — one real-API surface per route, one
PR with its evidence record, receipts that pair every Done-when clause with the exact check that
demonstrated it, gates recorded with counts and exit codes, unresolved limits stated as limits.

This increment is the series closer: the real-time invalidation protocol, the legacy hash repair, the
legacy-presentation removal after parity, and the single series release receipt. It adds one new SPA
surface (`/portal/chat`) because that is the one workflow the classic renderer still served (§3).

## 1. What exists at the base revision (measured)

| Layer | State at `671525c` |
| --- | --- |
| SPA routes | `/portal` (redirect → `/attention`), `/attention`, `/projects`, `/projects/:projectId`, `/work/:workItemId`, `/connections`, `/runtimes`, `/goals/:goalId` (`frontend/src/app/router.tsx:18-52`). `/portal/chat` (design frontend.md:79) is **missing**; `#chat` still redirects to `/portal/classic` (`frontend/src/app/legacy.ts:10-11`). |
| Bootstrap | `GET /portal/api/session` returns exactly `{csrf_token}` (`portal_session_controller.ex:8-10`); the shell hands that token to pages as its command credential (`shell.tsx:160-166`, `session.tsx`). |
| Real-time | None. The only change discovery is polling: React Query `staleTime: 15_000` + `refetchOnWindowFocus` (`application.ts:74-75`), a surface-local 5 s poll on the work detail (`work/control.tsx:54,249-255`), and the classic asset's 5 s poll (`portal.js:2460-2462`). No operator PubSub topic exists (only `daemon:<machine_id>`; `notifier.ex:10-14`, `scheduler.ex:67-71`). |
| Socket | `socket "/socket", UserSocket` with machine-token auth in the `x-symmetry-token` header and only `daemon:*` topics (`endpoint.ex:16-18`, `user_socket.ex`, `channels/daemon_channel.ex`). Phoenix does not allow two sockets on one path (generated `do_socket_dispatch/2` clauses collide), so the operator socket needs its own path. |
| Incremental reads | Attention `cursor` (`project_blocked` + goal feed), goal events `after` (ascending `sequence`, limit ≤ 100, `read_model.ex:252-280`), chat `before` and work timeline `before` (opaque `Phoenix.Token` cursors). Nothing accepts `since`/`updated_after`. |
| Classic presentation | `/portal/classic` renders `portal_html/index.html.eex` with `priv/portal_assets/portal.js` (2 469 lines) and `portal.css` (2 329 lines, also the login page's stylesheet); `PortalHTML.index/1`, `PortalController.classic/2`, `router.ex:100`, `endpoint.ex:21-25`. |
| Classic coverage | `browser/tests/portal.spec.mjs` (3 cases), `portal-chat.spec.mjs` (deterministic chat), `portal-chat-live.spec.mjs` (live chat daemon), the classic halves of `portal-compose-daemon.spec.mjs` and `portal-compose-restart.spec.mjs`; two SPA-side cases assert the classic document (`workspace-shell.spec.mjs:250-284`, `portal-connections.spec.mjs:1307,1339`). Elixir pins: `portal_controller_test.exs` (6 cases incl. the classic document and `/assets/portal.{js,css}`), `portal_html_test.exs` (3), `portal_session_test.exs` (4 classic allowlist cases), `workspaces_read_model_test.exs:49,67` (uses `portal.js` as an artifact-path fixture). |
| Gates wired today | `contracts:check`; frontend typecheck/lint/test/build; control `mix format --check-formatted`, `mix compile --warnings-as-errors`, `mix test`; `MIX_ENV=prod mix release`; the 4-spec release browser gate; `scripts/audit-no-legacy-routes.sh` (U2 legacy **action endpoints**, unrelated to portal presentation). No axe dependency and no dedicated accessibility gate exists (§9). |

## 2. Owner authorization recorded in this increment

Root `AGENTS.md` forbids silent fixed-design changes. The owner has directed, twice, that legacy is
**not** preserved and no backward compatibility is required. Recorded here, in the goal receipt and in
the PR body:

- authorized: removing the classic routes, `portal.js`/`portal.css` presentation, the EEx workspace
  renderer, duplicate legacy presentation, and the classic acceptance specs in the same PR as the
  cutover;
- limit (a) ordering: nothing classic is deleted before the migrated suites demonstrate parity for
  every surface it served — chat included (§3);
- limit (b) fences: `scripts/audit-no-legacy-routes.sh` and its CI job, protocol/wire contracts, the
  fixtures-only provider boundary, credential locality and every fail-closed gate stay untouched;
- kept: the one-time legacy `/portal#...` hash redirect (a deep-link repair, not presentation) and the
  login template;
- no second writable UI state owner.

## 3. Prerequisite gap and its resolution (stated, not silently absorbed)

The contract lists "the accepted 0007a through 0007h outcomes" as its prerequisite. 0007a–0007f and
0007h are merged; **0007g (scoped engineering conversation) is unstarted**, and chat is the one
supported workflow still served only by the classic renderer (`/portal/classic#chat`,
`browser/tests/portal-chat*.spec.mjs`). The cutover's own Done-when requires the classic routes,
`portal.js`/CSS and the EEx renderer to be gone, and the goal text says removal happens only after
parity for every surface the renderer served. Leaving chat classic-only would either keep a second
writable UI state owner or delete a supported workflow — both out of bounds. This run therefore
**migrates the chat workflow to `/portal/chat` at parity with the classic surface** (scope switching,
drafts per scope, history pagination, command intents with durable receipts, decision answers, run
context), retires the classic chat specs into migrated specs, and records 0007g's own outstanding
*native-admission capability* clause as out of scope (§12). This resolution is reported in the receipt;
it is not presented as accepted 0007g evidence.

## 4. Done-when → deliverable map

| Contract clause | Deliverable | Evidence |
| --- | --- | --- |
| full frontend, contract, browser, accessibility and Phoenix release gates pass on a clean revision | §10 gate list executed once on the frozen revision | gate table with exact commands, counts and exit codes; §9 defines the accessibility scope honestly (no axe gate exists) |
| real-time invalidation loses no state | operator socket + channel + invalidation publishes on every committed change (§5); client invalidation of the affected query keys | two-context browser case (writer mutates, reader updates without manual refresh), a disconnected-then-reconnected case, and the receipts catch-up case |
| reconnect snapshot refetch + goal-event cursor catch-up, receipt history | reconnect invalidates every read (snapshot refetch) and the Goal receipts panel catches up from its last rendered `sequence` | the reconnect case asserts the goal read, the receipts list (contiguous sequences, no duplicates) and the workspace snapshot after reconnect |
| polling fallback | the realtime bridge polls (invalidation on an interval) while the socket is not connected; the surface-local 5 s poll is removed so one owner drives change discovery | browser case with WebSocket blocked asserts convergence; unit tests for the status transitions |
| expired auth, deep links, focus restoration, reduced motion, narrow screens end to end | existing shell/session behavior plus the new chat surface; reduced-motion CSS and a rendered reduced-motion case; narrow-width cases | the focused release gate (expiry instance), deep-link cases, chat focus/narrow/reduced-motion cases with captures |
| legacy hash redirects land on the new routes exactly once | `legacyDestination` mapping (§6), bootstrap-only `history.replaceState`, no classic target | browser cases asserting the final URL, one document load and a stable URL after settle; unit tests for every mapping |
| classic routes, `portal.js`/CSS, EEx renderer gone after parity; no second writable UI state model | §7 removal list + migrated suites | removed-file list, the 404 assertions for the removed route, and the executed migrated suites |
| one series release receipt binding one exact Subject, every full-series clause mapped, revision, evidence-validity column, capture set, unresolved findings | `docs/evidence/goal-0007i-portal-cutover-series-receipt-<date>.md` with the contract text verbatim | the receipt itself; reused evidence carries its own validity column and is only accepted when it names the reviewed revision |
| provider evidence stays fixtures-only | no provider code touched; the provider fixture boundary is re-run as the connection acceptance (0007d/0007e fence) | the connection-fixture suite run on this revision, with its canary scans |

## 5. Real-time delivery (fixed protocol compliance)

`docs/design/protocol.md:328-338` is normative; every clause maps to one mechanism:

| Protocol clause | Mechanism |
| --- | --- |
| authenticated operator Phoenix Channel topics, invalidation only | new `SymmetryControlWeb.PortalSocket` at `/portal/socket` (own path: Phoenix cannot mount two sockets on one path) with channel `portal:invalidations` → `SymmetryControlWeb.PortalChannel`; the channel has **no** `handle_in` that mutates anything |
| short-lived audience-scoped socket token from the same-origin bootstrap | `SymmetryControlWeb.PortalSocketToken` (`Phoenix.Token`, salt `portal-socket-v1`, audience `portal:operator`, TTL `portal_socket_token_max_age_seconds`, default 300 s) minted **only** by the authenticated `GET /portal/api/session`, which now returns `{csrf_token, socket_token}`; the socket connects with `params: {token}` |
| no operator bearer token in JS/localStorage | the operator token never leaves the login POST; the socket token is derived from the session cookie server-side and is held in memory only (the existing saved-views localStorage document is unchanged and stores preferences only) |
| authorize topic joins | `PortalChannel.join/3` refuses any socket without the verified audience; the socket's `id/1` is audience-scoped |
| payload: resource type/id, latest version, event cursor; no raw transcript | `SymmetryControl.Portal.Invalidations.publish/1` normalizes `%{kind, id, project_id, work_item_id, goal_id, version, cursor}` (nil members dropped). `cursor` is the changed stream's durable sequence when it has one (Goal events: `goal_events.sequence`), else `nil`. No message content, no transcript, no payload text |
| on notification TanStack Query invalidates and fetches authoritative data | `frontend/src/services/realtime.ts` maps each kind to query-key prefixes and calls `queryClient.invalidateQueries`; batching window 200 ms so a burst (a run appending events) is one invalidation pass |
| reconnect: snapshot refetch + goal-event cursor catch-up | the bridge exposes a reconnect epoch; on every (re)connect it invalidates every read (except `["session"]`), and the Goal receipts panel appends a boundary `after=<last rendered sequence>` when it had reached the tail, so missed receipts arrive contiguously |
| polling remains a fallback | while the socket is not connected the bridge invalidates all reads on a 5 s interval (the retired classic/chat cadence); the socket's own reconnect backoff is the off-path |
| connection loss or missed messages cannot lose state | every surface's state is a server read; reconnect refetches it; invalidation is idempotent and no client keeps an event log |
| no second browser event store or transition engine | the client keeps only connection status, the batching timer and the reconnect epoch; no event list, no cursor store, no policy |

Publish sites (one line each, after the commit, never inside a transaction): `PortalApiController`
writes (connection/project/resource/work-item actions), `GoalController.create|command`,
`ChatController.create`, the daemon-facing `DaemonController` actions that change execution truth
(register_session, heartbeat, reconcile, claim, heartbeat_run, append_events, transition, session
attach/stop, append_evidence, record_usage, acknowledge_command → `kind: "run", id: run_id`), the Goal
settlement/control workers (`kind: "goal"` with the new event sequence), and the Integrations sync
worker (`kind: "resource"`/`"connection"`). Raw transcript *text* is never published; `append_events`
publishes a `run` invalidation only.

## 6. Legacy hash redirect (kept, one-time, exactly once)

`frontend/src/app/legacy.ts` maps the documented classic hash locations to the new routes; `main.tsx`
applies it **once**, at bootstrap, only for pathname `/portal` or `/portal/`:

| Legacy location | New destination |
| --- | --- |
| `/portal#connections[?connectionId=x]` | `/portal/connections[?connectionId=x]` |
| `/portal?project_id=P#resources` | `/portal/connections?projectId=P` |
| `/portal?project_id=P#board` | `/portal/projects/P` (no project → `/portal/projects`) |
| `/portal?project_id=P#activity` | `/portal/projects/P` — the board carries the work and its execution state; runs are drill-down from work detail. Reversible default: no `/portal/activity` route exists in the fixed IA (`docs/design/frontend.md:73-81`) and inventing one is out of bounds |
| `/portal?chat_scope=S&run_id=R#chat` | `/portal/chat?scope=S&runId=R` (project scope carries `projectId`) |
| `/portal?project_id=P#health`, `/portal?project_id=P#attention` | `/portal/attention?projectId=P` (no project → `/portal/attention`): the classic attention and health anchors were project-scoped views of a link that carried `project_id`, and the `/portal/attention` route accepts that scope |
| `/portal#projects`, `/portal?project_id=P#projects` | `/portal/projects[/P]` |
| `/portal#projects/<id>` or `#project=<id>` | `/portal/projects/<id>` |
| unknown fragment | `/portal/attention?unavailable=<fragment>` (unchanged honest statement) |

"Exactly once": the mapping runs before the router mounts, uses `history.replaceState` (same document,
no reload), and is not re-applied on later hash changes; the destination routes are stable canonical
URLs, so the browser observes one document load, one replace, and no further redirect. The
`PortalSession.return_to/1` allowlist keeps accepting `/portal` with `project_id`/`run_id`/`chat_scope`
(the redirect's search carrier) plus the new `/portal/chat` search state, so a signed-out legacy deep
link still lands on its destination after login.

## 7. Removal and migration list

Removed (after the migrated suites pass on this revision):

| Artifact | Replacement |
| --- | --- |
| `control/lib/symmetry_control_web/portal_html/index.html.eex`, `PortalHTML.index/1` | the SPA document `priv/static/portal/index.html` served by `PortalController.index/2` |
| `PortalController.classic/2` + `get "/classic"` | — (the route becomes a real 404; the SPA keeps `/portal`, `/portal/chat` and the other routes) |
| `control/priv/portal_assets/portal.js` | `frontend/src/services/realtime.ts` + the feature surfaces |
| `control/priv/portal_assets/portal.css` | `control/priv/portal_assets/login.css`: the login page's own rules, extracted verbatim from `portal.css` (`portal.css:142,270-302,363,1429-1546,1762`) — the login template stays and keeps its rendering |
| classic browser specs | migration map below |
| classic Elixir pins (`portal_controller_test.exs`, `portal_html_test.exs`, `portal_session_test.exs`, `workspaces_read_model_test.exs` artifact-path fixture) | rewritten to the login template, the SPA document, the new allowlist and a different artifact-path fixture |

Flow → migrated suite:

| Classic flow | Migrated coverage |
| --- | --- |
| resource attach/update/detach + truthful health | `portal-connections.spec.mjs` (0007e, already new-route) — the classic case's health-rail assertion moves to the attention/connection reads |
| session boundary (login, CSRF 403, logout, signed-out deep link) | `workspace-shell.spec.mjs` (already new-route; the classic case's CSRF probe is migrated there) |
| keyboard/dialog/drawer usability without overflow | `workspace-shell.spec.mjs` (keyboard palette/nav) + `portal-work-views.spec.mjs` + new chat keyboard case |
| chat (deterministic) | rewritten `portal-chat.spec.mjs` on `/portal/chat` |
| chat (live daemon) | rewritten `portal-chat-live.spec.mjs` on `/portal/chat`, same runner (`run-chat-daemon-tests.ps1`) |
| Compose daemon end-to-end (task completion, generation, durable events, final board move) and the restart read | migrated to the SPA board/work-detail/run surfaces, same `run-compose-daemon-tests.ps1` runner |
| legacy board link through logout/login | `workspace-shell.spec.mjs` asserting the new destination and the one-time replace |
| classic attention keyboard walk, classic project/work planning, classic execution control, classic resource checks | already retired by 0007b/0007c/0007e/0007f/0007h with their successors (browser/README.md) |

## 8. Chat surface parity contract

Classic behavior that the migrated surface must keep (from `portal.js` and `portal-chat*.spec.mjs`):
scope workspace/project/run with the scope in the URL; per-scope draft, selection and scroll preserved
across scope switches and background refreshes; history rendered oldest-first with an "Earlier
messages" boundary from the served `next_before`; composer intents `discuss|status|start_work|
guidance|decision|pause|resume|cancel` with the served intent notes; `start_work` fields (title,
agent profile, workspace, repository, CI) fed by the workspace read and disabled when the context is
missing; a client-minted `action_id` persisted per draft intent and replayed verbatim on retry;
control intents dispatched only for a run-scoped context with a served execution generation and the
waiting transition id; the command receipt rendered as *requested, pending acknowledgement* until the
server serves `applied`/acknowledged state; decision answers carrying `option_id` when the served
decision package offers one; read-only `discuss`/`status` that never steer a worker; scope-keyed
notices that survive other-scope updates; no raw agent HTML. URL state: `scope`, `projectId`, `runId`.

## 9. Accessibility, reduced motion, narrow screens

No axe dependency or dedicated a11y gate exists in the repository, so the "accessibility gate" this run
reports is the set of accessibility-covering checks that do exist, run in one pass: the frontend
unit/router suites' role/focus/ARIA assertions, the browser keyboard/focus/ARIA cases at both
Chromium widths, and the new rendered reduced-motion and narrow-screen cases. The receipt names the
scope as "accessibility-covering browser and unit checks; no axe-based audit exists".

New work: a workspace-wide `prefers-reduced-motion: reduce` rule that removes animation and transition
duration (the loading indicator is the only animation today), a rendered case under
`emulateMedia({reducedMotion: "reduce"})` asserting no running animation and a readable surface, and
narrow-width cases (existing `mobile-chromium` project plus an explicit 390 px chat case asserting
`scrollWidth <= innerWidth`).

## 10. Gates (each executed once, on the frozen revision)

1. `pnpm contracts:check` (root).
2. `pnpm --filter @symmetry/frontend typecheck` / `lint` / `test` / `build`.
3. control: `mix format --check-formatted`, `mix compile --warnings-as-errors`, `mix test`
   (`POSTGRES_*` → this worktree's container).
4. `MIX_ENV=prod mix release --overwrite`, then start the release plus a second true-TTL instance
   (`SYMMETRY_PORTAL_SESSION_MAX_AGE_SECONDS=2`) on this worktree's database.
5. Browser gate on that release: the four focused release specs + the new realtime/cutover spec +
   the migrated chat spec + the goal-attention spec with `SYMMETRY_GOALS_OFF_URL` where available,
   then the full suite (`pnpm --filter symmetry-browser-acceptance test`) in the same pass, naming
   every skipped case and its missing environment.
6. Harness suites that cover the migrated surfaces, each once: `run-live-daemon-tests.ps1`,
   `run-chat-daemon-tests.ps1`, `run-planning-tests.ps1`, `run-connection-fixture-tests.ps1`
   (+ `test:connection-env`), `run-compose-daemon-tests.ps1` as host capacity allows; anything not
   executed is reported as unverified, never as passed.
7. `scripts/audit-no-legacy-routes.sh` (fence, unchanged) and `pwsh -File browser/scripts/…` runners'
   own exit codes.

Not run concurrently with any measurement harness (host memory rule). Known host limitation to check
first, not to hide: `SymmetryControl.Integrations.CommandTest` fails when bare `pwsh` resolves to the
0-byte WindowsApps alias; if no working `pwsh` exists, `mix test` reports that pre-existing failure and
the receipt states it as an environment limit with the untouched file named.

## 11. Evidence, receipt and subject binding

- `docs/evidence/goal-0007i-portal-cutover-series-receipt-<date>.md`: the single series receipt. It
  preserves the full-series acceptance contract **verbatim**, binds one exact Subject (commit when one
  exists, otherwise the working tree plus a source-subject digest over every non-`docs/**` path), and
  maps every clause to evidence with an evidence-validity column (each row names the revision the
  evidence was produced on), the rendered capture set (spec, case, viewport, artifact path), the
  unresolved findings, and the statement that the owner's verdict is not part of this run.
- The implementation section of the same file records what changed, the migrated-spec map and the gate
  table with exact commands, counts and exit codes.
- `docs/goals/README.md` gets the 0007i closure paragraph with the receipt link; the goal file itself
  is not edited (the 0007a–0007h series convention: goal files stay, receipts carry closure).
- `docs/TODO.md` records dead-code candidates and verification residuals found here, not cleaned up
  opportunistically.

## 12. Out of scope (with reason)

- **0007g's native-admission capability clause** (replacing Chat's blanket generic supervisory-control
  check with required start/events/result capabilities for native admissions): a control-plane policy
  change with its own acknowledgement tests and evidence; it is an enhancement, not classic parity, and
  it would silently rewrite 0007g's contract. Chat start keeps the accepted rule
  (`required_capabilities: %{"supervisory_control" => true}`, `chat.ex:180-184`).
- **New wire contracts.** The realtime channel is additive (a new socket path, a new session member,
  new invalidation events); no protocol v1 endpoint changes, no schema rewrite. `contracts/**` gains
  only what the new chat surface needs to decode (`chat-conversation` read + message/command shapes)
  if a generated boundary is used, plus fixtures.
- **Provider-side effects / live provider contact**: unchanged, fixtures-only.
- **Owner verdict and `achieve`**: never produced by the model.
- **`/portal/activity` route, workflow canvas, mobile app, second business-policy engine, production
  mock data, a second writable UI state owner**: excluded by the fixed design; the `#activity` mapping
  above is the documented reversible default.

## 13. Open details and chosen defaults

| Detail | Default taken | Reversible? |
| --- | --- | --- |
| socket path | `/portal/socket` (own path; Phoenix cannot mount two sockets per path) | yes (config-free, one line in `endpoint.ex` + the Vite proxy) |
| socket-token TTL | 300 s, refreshed by the session read on every (re)connect attempt | yes (config attribute) |
| polling fallback cadence | 5 s, matching the retired classic and work-detail cadence | yes (one constant) |
| goal-event catch-up overlap | continue from the last rendered `sequence` with no overlap (sequences are unique and ascending per goal) | yes |
| chat `#chat` project scope | `chat_scope`/`run_id`/`project_id` map to `scope`/`runId`/`projectId` | yes |
| `#activity` destination | project board (documented above) | yes |

## 14. Verification notes for this host

- Local toolchain: `mise exec erlang@29.1.2 elixir@1.20.4-otp-29 -- mix …`; `mise exec pnpm@12.17.0 -- pnpm …`.
- Postgres for this worktree: container `symmetry-0007i-postgres`, `127.0.0.1:55463`, DBs
  `symmetry_control_dev` (release) and `symmetry_control_test` (ExUnit).
- Do not run a measurement harness concurrently with the full suite.

## 15. Rebase onto current `main` (2026-11-17, `ca29a8e`)

The branch was rebased onto `ca29a8e` (goal 0007g, scoped engineering conversation). Section 3 above
recorded that 0007g was unstarted when this plan was written; it is now delivered, and its `/portal/chat`
surface, `chat-read` contract family and daemon envelope registration are the ones this revision ships.
The cutover's own chat implementation, its `chat-conversation` schema/fixtures/generated DTOs and its
`chat-conversation` daemon envelope are therefore **removed as superseded**, not merged alongside 0007g —
keeping both would have shipped two implementations of one surface and two contract families for one
endpoint (the repository asserts exactly 21 v1 schemas). §8's chat parity contract is now carried by
0007g's delivered suite on the same route; §10's gate list is re-established on the rebased bytes in the
receipt's rebase section, and every row there names the revision that produced it.
