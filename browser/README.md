# Browser acceptance tests

These tests exercise the authenticated React workspace at `/portal` against a
running Phoenix control plane. The classic workspace presentation is gone as of
the 0007i cutover: the EEx workspace renderer, its client-side script and its
stylesheet under `control/priv/portal_assets` were deleted, the classic route is
a real 404, and only the server-rendered login template and its extracted
`login.css` remain. `tests/portal.spec.mjs` is deleted with its last classic
surfaces; its coverage moved as follows:

- resource attach/update/detach and the truthful health read (including the
  degraded health the classic rail surfaced) ->
  `tests/portal-connections.spec.mjs` (0007e), whose degraded badge assertion
  was added for it;
- browser session boundary (login, logout, signed-out deep link) ->
  `tests/workspace-shell.spec.mjs`; its invalid-token alert and missing-CSRF
  header 403 probe are migrated there in the same cutover revision;
- keyboard/dialog walk -> `tests/portal-work-planning.spec.mjs`,
  `tests/portal-work-views.spec.mjs` and `tests/workspace-shell.spec.mjs`
  (navigation palette), and its no-horizontal-overflow assertion ->
  `tests/portal-compose-daemon.spec.mjs` on the SPA board;
- the classic Compose halves (daemon and restart) were migrated in place onto
  `/portal/projects/:projectId`, `/portal/work/:workItemId` and
  `/portal/connections?projectId=`, with every retired classic control named
  at its replacement in the spec.

The paragraphs below record where each earlier goal's classic check went; they
are history, and every named successor still runs.

Chat was the last surface the classic renderer served, and it is goal 0007g's
scoped conversation that covers it on `/portal/chat` (delivered on `main` as
`ca29a8e`): the per-scope draft/selection/scroll walk, the
question-versus-control separation, the uncertain-send retry under one action
identity, the cursored history walk, the run-scope decision packet with durable
receipts and the inert unsafe-output walk run against the real Portal API, and
the classic live Chat walk moved to the same route against a real daemon. Both
specs lose their classic halves with this cutover; the retired assertions and
their successors are listed in
`docs/evidence/goal-0007g-scoped-engineering-chat-*.md`.

`tests/portal-cutover.spec.mjs` is the cutover's own acceptance spec: six cases
on the release instance. The legacy `/portal#...` hash table (board, activity,
chat with its scope and run, health, unknown anchors) lands exactly once on the
new routes with the classic document and `/assets/portal.js` absent, keeping the
`project_id` scope on the anchors whose classic view was project-scoped; a connected
surface refreshes from the operator invalidation channel without a reload; a
disconnected surface reconnects and catches its receipts up from the served
event cursor; polling keeps a surface fresh when the socket cannot be
established; a `prefers-reduced-motion: reduce` client renders no running
animation; and the 390 px width with the navigation palette's Escape focus
return stays usable.

The 0007f execution-control and runtime checks that the new
`/portal/work/:workItemId` and `/portal/runtimes` surfaces replace — the
classic work-detail control walk (Cancel run -> Cancelled, Retry availability,
retry hidden after reassignment) and the classic drawer draft/focus refresh
case — are retired from the classic suite and covered by
`tests/portal-execution-control.spec.mjs` (real API, no daemon),
`tests/portal-goal-managed.spec.mjs` (a real Goal-managed row prepared by
`scripts/ci/portal-planning-fixture.exs` presents no legacy control path) and
`tests/portal-live-daemon.spec.mjs` (real daemons). The classic walk's live
timer and its Activity-row `Execution stopped` assertion are retired, not
migrated: the new surface renders the served execution state and no
client-side elapsed time, and the migrated check asserts the served state chip
instead. Retired with the live-daemon rewrite, each with its successor named:
the classic forged-runtime-reference refusal walk (server enforcement in
`control/test/symmetry_control_web/controllers/portal_workflow_test.exs`
"resource APIs expose and enforce registered runtime identities", form
field-error rendering in `connections.test.tsx`) and the obsolete-response
phases of the classic cursored history case (its obsolete older response has
the daemon-free synthetic guard in `tests/portal-work-views.spec.mjs`; the real
cursor load and exhaustion stay in the live suite). The migratable half of the
classic draft/focus case moved; the classic raw-execution expander has no
equivalent on the new surface and is recorded as retired, not migrated, in the
goal receipt. The classic Resources view and its registered-runtime reference
selection moved to the new-route resource flows in
`tests/portal-connections.spec.mjs` (0007e) and were deleted with the classic
document in the 0007i cutover.
The 0007b list, board, detail and history read checks that the new
`/portal/projects/:projectId` and `/portal/work/:workItemId` surfaces replace -
including the classic detail-security probe and the delayed-detail,
older-history and delayed-workspace races - are retired from the classic suite
and covered by `tests/portal-work-views.spec.mjs` (races replay held real
responses; the older-history failure's obsolete-response guard is daemon-free
and synthetic, and the real cursored history load runs in
`tests/portal-live-daemon.spec.mjs`). No resource or control coverage was
removed without a named successor.
The 0007h attention and Goal-detail checks are retired from the classic suite the
same way: the classic `#attention-list` keyboard walk (`portal.spec.mjs`,
keyboard case), the "Attention filtering is read on the new routes" comment in
the feature case and the retired classic detail/board read surfaces already
replaced by 0007b. The keyboard walk is migrated, not dropped: the equivalent
walk on the new surface ("the attention list is keyboard reachable and the Goal
surface hands focus back") runs in `tests/portal-goal-attention.spec.mjs` beside
the attention and `/portal/goals/:goalId` acceptance cases. That spec's
rollout-off case needs a second release instance started with
`SYMMETRY_GOALS_ROLLOUT_ENABLED=false` and passed as `SYMMETRY_GOALS_OFF_URL`;
without it the case is explicitly skipped, never passed. No editing, resource or
control coverage was removed.

```sh
mise exec pnpm@12.17.0 -- pnpm install --frozen-lockfile
mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend build
mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance exec playwright install chromium
mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance test
```

The default target is `http://127.0.0.1:4000` with the development operator
token. Override either value when needed:

```sh
SYMMETRY_PORTAL_URL=http://127.0.0.1:4001 \
SYMMETRY_OPERATOR_TOKEN="$FIXTURE_OPERATOR_TOKEN" \
mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance test
```

The suite runs desktop and mobile Chromium projects. Failure screenshots,
videos, and traces are written under the system temporary directory by default;
set `SYMMETRY_PLAYWRIGHT_OUTPUT` to retain them elsewhere. The HTML report is
written under `playwright-report/`.

The focused release-shell gate (also what CI runs against the release) is:

```sh
SYMMETRY_SHELL_EXPIRY_URL=http://127.0.0.1:4001 \
POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=55442 POSTGRES_DB=symmetry_control_dev \
mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance test \
  tests/workspace-shell.spec.mjs tests/portal-work-views.spec.mjs \
  tests/portal-execution-control.spec.mjs tests/portal-goal-managed.spec.mjs \
  tests/portal-cutover.spec.mjs tests/portal-chat.spec.mjs
```

It renders desktop/mobile navigation, keyboard focus, loading, empty, network
error, signed-out, deep-link and refresh/history states, with screenshots and
observation JSON in each test output directory. Loading holds a real request;
network errors abort it. It does not substitute JSON or run a mock backend.
On the same release it also drives the execution-control surface end to end:
start, a live queued state, cancel, the server-gated retry control, a rejection
answered by the real server, an in-flight request held past the rendered receipt
(never rendered as applied) and a refused control request rendered as an unknown
outcome with the same attempt replayed once; the runtimes list's scoped and
unscoped empty states; plus the Goal-managed detail, whose row is written by
`scripts/ci/portal-planning-fixture.exs` through Control's own changesets — the
`POSTGRES_*` variables above must name the database the release serves, and
without them that case skips with its missing environment named instead of
passing. That case asserts the served `goal_id`, no run/cancel/retry control,
the 403 `goal_authority_required` refusals of the legacy paths and a sibling
legacy item that keeps its control.
The expiry URL must serve the same release and fixture database with
`SYMMETRY_PORTAL_SESSION_MAX_AGE_SECONDS=2`; without it the true-TTL expiry cases
are explicitly skipped, not passed. The focused suite also checks Attention
scope through selection, history, navigation and login with real fractional
resource metadata, legacy `/portal` project-board links repaired to their SPA
destination through real logout and login, plus logout after another
shared-cookie tab renews its
session. CI starts both release instances and saves the render report.
Fixture-only project/work/resource creation uses the existing
authenticated APIs; the React shell's own mutation surfaces are connection
management at `/portal/connections`, project/work-item planning on the project
and work-item surfaces (both covered below), the work-detail execution controls
added by goal 0007f, and the scoped conversation added by goal 0007g.
The shared-cookie CSRF case sends an explicit `visibilitychange` notification:
The 0007a host kept its Playwright documents visible even after native window
minimization.
Cookies, bootstrap response and logout are real, but this case does not claim
native tab background/foreground event propagation.

## Readable work views acceptance

`tests/portal-work-views.spec.mjs` covers the 0007b read surfaces on the running
release: the project list and board with filters, saved view and URL state, the
work detail with outcome, evidence, blocker, next action and execution history,
plus the empty and error states. Every state is captured full page with an
observation JSON (URL, viewport, labels, and the largest observed workspace
snapshot in bytes and work items) in each test's output directory.

Fixtures are created only through the authenticated Portal API; the shell has no
mutation controls. The list read keeps the existing workspace snapshot contract,
so the snapshot size is reported as a limitation rather than paginated. Deep
links are re-entered from fresh signed-out browser contexts and reloaded in
separate self-contained cases, so each scenario still executes while another is
blocked: one for the work detail URL (desktop drawer over its project board
background, asserted again after reload) and one for the project board/filter
URL (login return target, retained view/filter params and reload). A genuinely
captured real response is held while another project renders and then released,
to prove a late response never appears as another project's data or newer state.
A work URL that claims a different project is canonicalized to the project the
served projection confirms, and the claimed project's work item is never
rendered anywhere in the document.
The classic detail-security check is kept as an explicitly separate
malicious-response injection probe: the tampered response stays schema-valid and
must render its payload as escaped text, so the probe cannot pass through a
decode error, and it never substitutes for real-data acceptance. The obsolete
older-history request has a daemon-free, always-on guard: the suite injects a
generated, schema-valid contract fixture that supplies the history cursor the
backend cannot serve without a real run, holds the older page, selects a newer
real detail and proves the obsolete failure never mutates it. That case is
synthetic fault injection; it takes no capture and is never counted as real
history evidence. Genuine cursor-paginated execution history requires real
daemon records, so `tests/portal-live-daemon.spec.mjs` loads older pages on
`/portal/work/:workItemId` and the Compose runners read the same surface after
their daemon flows.

The release serves `/portal/work/:workItemId` as the authenticated shell
document, the same way it serves the project routes, and the login allowlist
keeps the work and filtered-project URLs (view, text filters, blocked flag,
project scope) as its return target while every other path stays rejected. Both
are asserted by the fresh-session deep-link cases, which re-enter the work
detail and the filtered board from a signed-out context and reload them.

## Connection management acceptance

`tests/portal-connections.spec.mjs` drives `/portal/connections` against the real
Portal API and PostgreSQL: create, inspect, test, update, remove, repeated
submission, stale update, concurrent removal, refused submissions and the
capability reset of bound resource health, plus the resource surface: binding,
updating, unbinding and synchronizing resources of both provider kinds — the
version-fenced update, identity reset and unbind are exercised for the GitHub
repository kind, while the Azure DevOps kinds (`project/repository`, `project`
and `project/pipeline/<definitionId>`) and the work-tracking/CI kinds are bound
and synchronized only; their update and unbind are covered by the routed API
suite (`portal_resources_test.exs`) — plus injected provider failures, the
unknown-outcome readback, the failed re-read after a write rendering the stale
rows as a failed refresh, project switching under a sync in flight, and work
assignment scoped to the item's project. Provider calls go through the
controlled fixture transport in `scripts/ci/portal-connection-fixture.exs`, which
replaces the provider HTTP client and the local CLI session; no provider host is
contacted and every fixture call is appended to the JSONL log below. The fixture
CLI tokens are canaries: each test scans the recorded same-origin response
bodies and response headers, the rendered HTML, web storage, the cookie jar
(sampled during the run, so a cookie that was set and cleared — HttpOnly
included — is still visible), request headers and bodies, and every console
message and page error the page produced during the run. Response records,
header lists and jar samples must be non-empty, so a missing capture fails the
test instead of passing the scan. Every completed response body must be
readable; the one bounded exception is a response whose read the page aborted
(a project switch or a document navigation cancelled the request), which is
allowed only when the request is a failed `GET`/`HEAD` — headers are still
scanned, and a write can never take that branch.

### Fixture provider documents and the steering file

The fixture answers exactly the provider reads these flows make; every other path
or method under `api.github.com` / `dev.azure.com` is refused
(`fixture_no_route` / `fixture_unexpected_request`) instead of being answered by
a blanket document:

- GitHub (`GET`, the fixture `gh` token): `/user`,
  `/user/memberships/orgs/{account}`, `/repos/{owner}/{repository}` (the
  repository document), `…/issues` (the issue list), `…/actions/runs` (one
  completed workflow run), `…/pulls` and `…/pulls/{number}` (one open pull
  request) and `…/pulls/{number}/reviews` (one approving review). Issues default
  to `[101, 102]` (101 open, 102 closed); the default repository document keeps
  its `id`, `node_id`, `html_url`, `default_branch`, `visibility` and `archived`.
- Azure DevOps (`GET` and `POST`, the fixture `az` token): `/{org}/_apis/projects`
  (the connection check), `GET …/_apis/git/repositories/{repository}`,
  `POST /{org}/{project}/_apis/wit/wiql`, `POST …/_apis/wit/workitemsbatch`
  (work item `101` with `System.*` fields and a positive `rev`), `GET
  …/_apis/build/definitions/{id}` (definition `{id}`) and `GET
  …/_apis/build/builds` (build `42`, completed/succeeded, naming the definition
  the read filters by).

`SYMMETRY_FIXTURE_STATE_FILE` points at a JSON steering document the harness
creates per run and passes to both the fixture process and the spec. The fixture
re-reads it on every provider request, so the spec steers individual visits by
rewriting the file; an absent, unreadable or invalid document is the empty state:

```json
{
  "github": {
    "fail": ["owner/repo"],
    "transport_fail": ["owner/repo"],
    "hold": ["owner/repo"],
    "issues": {"owner/repo": [101, 102]},
    "repository": {"owner/repo": {"archived": true, "html_url": "…", "default_branch": "main", "visibility": "private"}}
  },
  "azure_devops": {
    "fail": ["org/project"],
    "transport_fail": ["org/project"],
    "hold": ["org/project"],
    "work_items": {"org/project": [101]},
    "repository": {"org/project/repo": {"webUrl": "…", "defaultBranch": "refs/heads/main"}}
  }
}
```

- `fail` / `transport_fail`: the provider read answers HTTP 500 with a fixed
  fixture message, or no HTTP document at all (`{:error, {:transport, …}}`) — the
  ambiguous outcome that must never be rendered as synchronized.
  `transport_fail` wins when both list the reference.
- `hold`: the read blocks server-side (re-reading the file every 100 ms, at most
  300 polls) until the reference leaves the list; the answer then comes from the
  state as of release, so one rewrite can release a hold into a failure. This is
  how a browser flow keeps a sync in flight while it switches project or asserts
  the disabled controls.
- `issues` / `work_items`: the issue numbers / work-item ids the fixture reports
  (defaults `[101, 102]` / `[101]`); an empty list makes a previously imported
  item absent from the readback, which then marks it unavailable.
- `repository`: overrides for the repository document (GitHub `archived`,
  `html_url`, `default_branch`, `visibility`; Azure DevOps `webUrl`,
  `defaultBranch`), so a readback can show state that contradicts a pre-failure
  assumption.

GitHub references are keyed `owner/repository` (issues, repository, actions and
pulls share one key); Azure DevOps scopes are keyed `org/project` (one key covers
the project's repository, work-tracking and CI reads). Every steered call is
still recorded in the JSONL call log with its `steered` marker (`fail` → status
500, `transport_fail` → rejected with the transport reason) and still records
only the bearer shape, never a token value.

The spec runs only against this fixture. The harness starts the control plane
with a per-run identity, an identity file, a per-run operator token and the
per-run provider steering file; the spec proves the identity file carries the
run's identity for the portal port and logs in with the per-run token before any
provider check. That fixture identity
decides the collection contract: without `SYMMETRY_FIXTURE_IDENTITY` /
`SYMMETRY_FIXTURE_IDENTITY_FILE` the spec cannot prove its target and skips at
collection, so the default target and the `SYMMETRY_PORTAL_URL` /
`SYMMETRY_OPERATOR_TOKEN` override above stay loadable custom-target runs instead
of driving a normal Control instance that would use its real `gh`/`az` sessions;
a run that carries part of the identity (a half-configured harness) fails at
collection instead. The harness itself fails a run that reports a skip. That
contract is exercised by
`pnpm --filter symmetry-browser-acceptance test:connection-env`
(`browser/scripts/connection-spec-env.test.mjs`), which drives the Playwright CLI
for the absent, documented-override, partial-identity and complete-fixture
environments. The fixture keeps the application's real supervision topology —
scheduler, reconciler and provider access stay up — and readiness requires
`/healthz` to answer `ok`. The harness refuses a `-PortalUrl` whose port
is not the one it starts the fixture on, refuses an occupied port, and stops only
the process tree it started plus the listener PID recorded after the identity
handshake.

```sh
cd control
POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=55444 POSTGRES_DB=symmetry_0007e_test mix ecto.create
POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=55444 POSTGRES_DB=symmetry_0007e_test mix ecto.migrate
POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=55444 POSTGRES_DB=symmetry_0007e_test \
  SYMMETRY_OPERATOR_TOKEN=<run token> SYMMETRY_FIXTURE_IDENTITY=<run identity> \
  SYMMETRY_FIXTURE_IDENTITY_FILE=/tmp/symmetry-fixture-identity.json \
  SYMMETRY_FIXTURE_STATE_FILE=/tmp/symmetry-fixture-state.json \
  PHX_SERVER=true PORT=4021 \
  SYMMETRY_FIXTURE_TRANSPORT_LOG=/tmp/symmetry-fixture-calls.jsonl \
  SYMMETRY_FIXTURE_UNAUTHORIZED_ACCOUNTS=expired-fixture \
  mix run --no-halt --no-start ../scripts/ci/portal-connection-fixture.exs
```

`--no-start` is required: the script installs the fixture transports and only
then starts the application, so no boot-time provider recovery can reach a real
HTTP client or CLI session.

Then, from the repository root, with the same operator token and the built
frontend staged into `control/priv/static/portal`:

```sh
SYMMETRY_PORTAL_URL=http://127.0.0.1:4021 SYMMETRY_OPERATOR_TOKEN=<run token> \
  SYMMETRY_FIXTURE_IDENTITY=<run identity> \
  SYMMETRY_FIXTURE_IDENTITY_FILE=/tmp/symmetry-fixture-identity.json \
  SYMMETRY_FIXTURE_STATE_FILE=/tmp/symmetry-fixture-state.json \
  mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance test tests/portal-connections.spec.mjs
```

`scripts/run-connection-fixture-tests.ps1` does that whole sequence in one
command — creates the per-run steering file, recreates and migrates the fixture
database in this worktree's own container (other portal specs leave projects,
work items and resources behind in it), starts the fixture control plane, waits
for the identity file and a healthy `/healthz`, runs the spec (its 22 tests must
pass on the desktop and mobile projects; a skip or a different count fails the
run), scans the control logs and the provider-call log for the fixture canaries
(a missing scan file or an empty provider-call log fails the run), and stops its
own server:

```powershell
pwsh -NoProfile -File .\browser\scripts\run-connection-fixture-tests.ps1
```

The fixture is deliberately quieter than a development server — it logs at
`warning` level so the acceptance run's output stays readable — but it does not
change the application topology under test. It does pin
`SYMMETRY_INTEGRATION_SYNC_INTERVAL_MS=3600000`: the periodic integration sweep
stays enabled (the real syncer child is part of the topology `/healthz` reports),
but its five-minute cadence would otherwise re-synchronize the acceptance rows
mid-run and turn a failure or an unavailability a flow is observing back into a
success. Nothing else about the application is reconfigured.

The classic resource checks are retired in the same change:
`tests/portal-classic-resources.spec.mjs` (four tests, eight runs across the
desktop and mobile projects) no longer runs, and its intent moved to the
new-route flows above — binding for both provider kinds, update and unbind with
the version-fenced `stale` refusals (GitHub repository kind; the routed API
suite covers the other kinds × providers), per-resource sync and the `Synced`
row, project sync and its failure notice, single-flight across a project
switch, the discarded late response, exact external identity with
provider-owned fields locked and availability rendered, and assignment scoped
to the item's project on the work detail. The retired ADO delivery-row render
assertions have no replacement here and are listed as a limitation in the
0007e receipt §6. Assertions that only ever depended on
synthetic browser mocks (held responses, injected failures, hand-written
provider payloads) moved to frontend component tests with deferred response
gates. Connection management itself is asserted on the new route above.

## Safe planning acceptance

`tests/portal-work-planning.spec.mjs` is the real-API planning acceptance for
the new `/portal/projects` and `/portal/work/:workItemId` surfaces: project
create/edit/archive/restore, work-item create/edit, board keyboard move and
reorder, double submit, explicit retry, two-context conflicts on projects and
work items, a draft kept across a background refresh, the cross-project race
where a late board move lands only on its own project, Goal-managed and
provider-owned items with no writable path, an archived project's read-only
board, a create whose committed response is lost and replayed without a second
row, moves into rendered empty columns (a backlog-only board and a board
filtered to one state), and an open editor draft kept across the responsive
breakpoint. Every mutation goes through the running control plane; interception
only holds, aborts or replays a captured request to inject the races the
contract names.
`browser/scripts/run-planning-tests.ps1` is its harness: it starts the real
control plane on a loopback port with a dedicated database and a per-run
operator token, and the spec skips itself without that environment (the runner
turns a skipped summary into a failure).

The cases with a rendered end state (fourteen of the eighteen) capture a full-page
screenshot plus an observation JSON through the shared `capture` helper; they
land in the Playwright output directory
(`SYMMETRY_PLAYWRIGHT_OUTPUT` when set, otherwise the per-user temp
`symmetry-playwright-results`), and the harness prints that run's spec and
control-plane log paths.

The classic planning dialogs were retired from the classic suite earlier; the
0007i cutover deletes that suite (`tests/portal.spec.mjs`) entirely with the
migration list at the top of this file. Two item kinds cannot come from the
portal API - Goal admission is 0007h's surface and
provider sync is 0007e's - so `scripts/ci/portal-planning-fixture.exs` applies
them through Control's own changesets: `--goal-managed <work_item_id>
<repository_resource_id>` writes the Goal, its first revision, the repository
baseline and the all-or-nothing membership, and `--provider-owned
<work_item_id> <resource_id> <provider> <external_id>` binds the item to an
external tracker.

## Live daemon acceptance

With the control plane running at `http://127.0.0.1:4000`, run the real daemon
lifecycle matrix from the repository root:

```powershell
pwsh -NoProfile -File .\browser\scripts\run-live-daemon-tests.ps1
```

The runner builds isolated daemon and fake-agent binaries, starts unique
wait/retry/cancel/history/race runtimes plus one runtime bound to an installed
real harness adapter, runs the browser scenarios on both Chromium projects, and
stops every fixture process. The tests verify durable input, process
cancellation, failed-to-successful retry across generations, semantic delivery
evidence, a cancel/completion race, a lost response replayed with the same
attempt identity, the runtime row compared with the daemon's own registration
payload, and raw-history pagination through `Load older`. The waiting-input and
retry lifecycle and the real cursor scenario run at desktop and narrow (Pixel 7)
widths with fresh isolated fixtures in each project, so each flow and the
full-page `work-history-pagination` capture and observation JSON exist at both
widths; the race, lost-response and capability cases run at desktop width and
skip narrow inside the spec. This runner is the only executor of that
real-cursor race; the CI browser gate runs against a release with no daemon and
therefore cannot produce a real history cursor.
The runner prints the log folder, the screenshots/observation-JSON folder
(`SYMMETRY_PLAYWRIGHT_OUTPUT`, defaulting to a per-run folder and left untouched
when already set) and the HTML report path (`PLAYWRIGHT_HTML_OUTPUT_DIR` when
set, otherwise `browser/playwright-report`).

Use `-ControlUrl`, `-OperatorToken`, or `-EnrollmentToken` to target a different
local control plane. The runner generates unique runtime bindings on every run
so recently stopped fixtures cannot receive new assignments.

For the daemon bundled by the trusted-local Compose stack, including a control
restart with database fingerprint and browser-read verification, run:

```powershell
pwsh -NoProfile -File .\browser\scripts\run-compose-daemon-tests.ps1
```

This scenario uses the Compose runtime's `default` agent profile and `primary`
workspace, then verifies task completion, generation, durable events and the
final board move on the SPA project/work surfaces (the classic Activity rail's
rows are asserted there; see the migration list above).

## Live Chat acceptance

```powershell
pwsh -NoProfile -File .\browser\scripts\run-chat-daemon-tests.ps1
```

The isolated cooperative daemon drives `/portal/chat`: Chat-created work,
questions without execution mutation (the commands list is compared byte for
byte before and after), durable guidance in real artifacts after a real
reconnect that re-reads its acknowledgement, safe-boundary pause, same-run
resume, the structured decision packet, completion with the guidance retained
in the artifact, one action identity's identical replay and changed-payload
refusal, a stale-generation control refused by the server, unsafe served output
rendered inertly on a real run, and cancellation with retained artifacts. It
asserts the cancellation on the cancelled attempt's own `result.json` step
counter — read before the control, then compared across the daemon's release of
its local journal — because that work item's goal already contains the word
"cancelled" and a `progress.md` substring match would be satisfied before the
control takes effect. Its keyboard walk waits for the window-focus refetch's own
response (`window`, where TanStack Query's focus manager listens) instead of
sleeping past the refresh. It reports its log/artifact directory and stops its
own daemon on exit. The normal
suite includes deterministic scoped-conversation coverage at desktop/mobile
widths.

The runner's cooperative agent profile is deliberately slower and longer than
the fake agent's default (200 steps at 400 ms, decision at step 150): the
operator's pre-decision controls must be acknowledged before the agent asks for
its decision, and the walk judges observable behaviour, never these timings.

## Scoped conversation acceptance (0007g)

`tests/portal-chat.spec.mjs` is the real-API acceptance for `/portal/chat`: the
three scopes in one session with per-scope draft, selection and scroll (a held
late read for one scope is released while another is selected), a question saved
without a command, an uncertain send retried with the frozen action identity so
the server replay applies exactly one message, a changed replay refused with the
server's own 409 `idempotency_conflict`, the served cursor walk over 66 real
messages with the retained older page and the explicit `data-boundary-*` note
when the refreshed page and the loaded history share no message id, a failed
read recovering into the server's snapshot, inert rendering of unsafe content,
and the run scope's own draft, intent and offset round-tripped through the
in-page scope control (the pane must really be able to hold the asserted offset,
or the case fails on that precondition instead of reporting a clamped 0).
Interception appears in five named mechanisms: holding one real conversation
read; aborting one real POST and rewriting one real request body; aborting one
real conversation read; and fulfilling the run-scope conversation plus one
runtime row from the committed
`contracts/fixtures/valid/chat-read.run-waiting-decision.json` fixture, because
no native runtime exists on this host — the two cases that use that fixture are
schema-valid synthetic projections and never count as real run evidence; the
real run-scope walk is the live-daemon suite above. The scope-switch, run round
trip and history walks capture a full-page screenshot plus an observation JSON
through the shared `capture` helper, the same way the live-daemon walk records
its decision packet and keyboard walk.
