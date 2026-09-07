# Browser acceptance tests

These tests exercise the real server-rendered portal and `portal.js` against a
running control plane. They create uniquely keyed projects and assert both
visible behavior and persisted API state.

```sh
cd browser
npm ci
npx playwright install chromium
npm test
```

The default target is `http://127.0.0.1:4000` with the development operator
token. Override either value when needed:

```sh
SYMMETRY_PORTAL_URL=http://127.0.0.1:4001 \
SYMMETRY_OPERATOR_TOKEN=another-token \
npm test
```

The suite runs desktop and mobile Chromium projects. Failure screenshots,
videos, and traces are written under the system temporary directory by default;
set `SYMMETRY_PLAYWRIGHT_OUTPUT` to retain them elsewhere. The HTML report is
written under `playwright-report/`.

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
