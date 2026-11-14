# Browser acceptance tests

These tests exercise the authenticated React shell and the retained classic
workspace against a running Phoenix control plane. Classic acceptance retains
its original controls and fixture semantics at `/portal/classic`; no behavior
assertions are replaced by shell-only checks.

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

The focused release-shell gate is:

```sh
SYMMETRY_SHELL_EXPIRY_URL=http://127.0.0.1:4001 \
mise exec pnpm@12.17.0 -- pnpm --filter symmetry-browser-acceptance test tests/workspace-shell.spec.mjs
```

It renders desktop/mobile navigation, keyboard focus, loading, empty, network
error, signed-out, deep-link and refresh/history states, with screenshots and
observation JSON in each test output directory. Loading holds a real request;
network errors abort it. It does not substitute JSON or run a mock backend.
The expiry URL must serve the same release and fixture database with
`SYMMETRY_PORTAL_SESSION_MAX_AGE_SECONDS=2`; without it the true-TTL expiry cases
are explicitly skipped, not passed. The focused suite also checks Attention
scope through selection, history, navigation and login with real fractional
resource metadata, plus logout after another shared-cookie tab renews its
session. CI starts both release instances and saves the render report.
Fixture-only project/work/resource creation uses the existing
authenticated APIs; the React shell itself provides no mutation controls.
The shared-cookie CSRF case sends an explicit `visibilitychange` notification:
The 0007a host kept its Playwright documents visible even after native window
minimization.
Cookies, bootstrap response and logout are real, but this case does not claim
native tab background/foreground event propagation.

## Live daemon acceptance

With the control plane running at `http://127.0.0.1:4000`, run the real daemon
lifecycle matrix from the repository root:

```powershell
pwsh -NoProfile -File .\browser\scripts\run-live-daemon-tests.ps1
```

The runner builds isolated daemon and fake-agent binaries, starts unique
wait/cancel/retry/history runtimes, runs the desktop browser scenarios, and
stops every fixture process. The tests verify durable input, process
cancellation, failed-to-successful retry across generations, semantic delivery
evidence, Activity state, and raw-history pagination through `Load older`.

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
