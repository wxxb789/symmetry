# Browser acceptance tests

These tests exercise the authenticated React shell and the retained classic
workspace against a running Phoenix control plane. Classic acceptance keeps its
editing, resource and control flows and fixture semantics at `/portal/classic`.
The 0007b list, board, detail and history read checks that the new
`/portal/projects/:projectId` and `/portal/work/:workItemId` surfaces replace -
including the classic detail-security probe and the delayed-detail,
older-history and delayed-workspace races - are retired from the classic suite
and covered by `tests/portal-work-views.spec.mjs` (races replay held real
responses; the older-history failure race uses a real daemon cursor in
`tests/portal-live-daemon.spec.mjs`). No editing, resource or control coverage
was removed.

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
mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance test \
  tests/workspace-shell.spec.mjs tests/portal-work-views.spec.mjs
```

It renders desktop/mobile navigation, keyboard focus, loading, empty, network
error, signed-out, deep-link and refresh/history states, with screenshots and
observation JSON in each test output directory. Loading holds a real request;
network errors abort it. It does not substitute JSON or run a mock backend.
The expiry URL must serve the same release and fixture database with
`SYMMETRY_PORTAL_SESSION_MAX_AGE_SECONDS=2`; without it the true-TTL expiry cases
are explicitly skipped, not passed. The focused suite also checks Attention
scope through selection, history, navigation and login with real fractional
resource metadata, legacy project board links through real logout and login,
plus logout after another shared-cookie tab renews its
session. CI starts both release instances and saves the render report.
Fixture-only project/work/resource creation uses the existing
authenticated APIs; the React shell's own mutation surface is connection
management at `/portal/connections` (covered below).
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
their classic mutations.

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
capability reset of bound resource health. Provider calls go through the
controlled fixture transport in `scripts/ci/portal-connection-fixture.exs`, which
replaces the provider HTTP client and the local CLI session; no provider host is
contacted and every fixture call is appended to the JSONL log below. The fixture
CLI tokens are canaries: each test asserts no canary reaches a recorded
same-origin response, the DOM, web storage, the cookie jar (including HttpOnly
cookies), request headers and bodies, or the console. A missing or unreadable
evidence surface fails the test instead of passing the scan.

The spec runs only against this fixture. The harness starts the control plane
with a per-run identity, an identity file and a per-run operator token; the spec
proves the identity file carries the run's identity for the portal port and logs
in with the per-run token before any provider check. Without those values it
fails at collection instead of driving a normal Control instance that would use
its real `gh`/`az` sessions. The fixture keeps the application's real supervision
topology — scheduler, reconciler and provider access stay up — and readiness
requires `/healthz` to answer `ok`. The harness refuses a `-PortalUrl` whose port
is not the one it starts the fixture on, refuses an occupied port, and stops only
the process tree it started plus the listener PID recorded after the identity
handshake.

```sh
cd control
POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=55440 POSTGRES_DB=symmetry_0007d_test mix ecto.create
POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=55440 POSTGRES_DB=symmetry_0007d_test mix ecto.migrate
SYMMETRY_OPERATOR_TOKEN=<run token> SYMMETRY_FIXTURE_IDENTITY=<run identity> \
  SYMMETRY_FIXTURE_IDENTITY_FILE=/tmp/symmetry-fixture-identity.json \
  PHX_SERVER=true PORT=4021 \
  SYMMETRY_FIXTURE_TRANSPORT_LOG=/tmp/symmetry-fixture-calls.jsonl \
  SYMMETRY_FIXTURE_UNAUTHORIZED_ACCOUNTS=expired-fixture \
  mix run --no-halt ../scripts/ci/portal-connection-fixture.exs
```

Then, from the repository root, with the same operator token and the built
frontend staged into `control/priv/static/portal`:

```sh
SYMMETRY_PORTAL_URL=http://127.0.0.1:4021 SYMMETRY_OPERATOR_TOKEN=<run token> \
  SYMMETRY_FIXTURE_IDENTITY=<run identity> \
  SYMMETRY_FIXTURE_IDENTITY_FILE=/tmp/symmetry-fixture-identity.json \
  mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance test tests/portal-connections.spec.mjs
```

`scripts/run-connection-fixture-tests.ps1` does that whole sequence in one
command — recreates and migrates the fixture database (other portal specs leave
projects, work items and resources behind in it), starts the fixture control
plane, waits for the identity file and a healthy `/healthz`, runs the spec, scans
the control logs and the provider-call log for the fixture canaries (a missing
scan file or an empty provider-call log fails the run), and stops its own
server:

```powershell
pwsh -NoProfile -File .\browser\scripts\run-connection-fixture-tests.ps1
```

The fixture is deliberately quieter than a development server — it logs at
`warning` level so the acceptance run's output stays readable — but it does not
change the application topology under test.

`tests/portal-classic-resources.spec.mjs` retains the classic workspace's
resource binding, synchronization and assignment checks at `/portal/classic`;
connection management itself is asserted on the new route above.

## Live daemon acceptance

With the control plane running at `http://127.0.0.1:4000`, run the real daemon
lifecycle matrix from the repository root:

```powershell
pwsh -NoProfile -File .\browser\scripts\run-live-daemon-tests.ps1
```

The runner builds isolated daemon and fake-agent binaries, starts unique
wait/cancel/retry/history runtimes, runs the browser scenarios on both Chromium
projects, and stops every fixture process. The tests verify durable input,
process cancellation, failed-to-successful retry across generations, semantic
delivery evidence, Activity state, and raw-history pagination through `Load
older`. The lifecycle matrix runs once at desktop width; the real cursor
scenario on `/portal/work/:workItemId` runs at desktop and narrow (Pixel 7)
widths with fresh isolated fixtures in each project, so its full-page
`work-history-pagination` capture and observation JSON exist at both widths.
That scenario also holds a failing older-history page to prove it stays on its
own surface while a newer detail is selected. This runner is the only executor
of that real-cursor race; the CI browser gate runs against a release with no
daemon and therefore cannot produce a real history cursor.
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
workspace, then verifies task completion, generation, durable events, Activity,
and the final Kanban move.

## Live Chat acceptance

```powershell
pwsh -NoProfile -File .\browser\scripts\run-chat-daemon-tests.ps1
```

The isolated cooperative daemon verifies Chat-created work, questions without
execution mutation, durable guidance in real artifacts, safe-boundary pause,
same-run resume, structured decisions, completion, and cancellation with retained
artifacts. It reports its log/artifact directory and stops its own daemon on exit.
The normal suite includes deterministic Chat coverage at desktop/mobile widths.
