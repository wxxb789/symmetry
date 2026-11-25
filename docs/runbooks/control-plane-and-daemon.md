# Control plane and daemon operations

Status: **operator runbook** for the Elixir control plane (`control/`) and the Go
execution daemon (`daemon/`). It covers deployment, configuration, the operator
API and the opt-in live checks. The wire contract, lifecycle and recovery
semantics are normative in [`../protocol-v1.md`](../protocol-v1.md); the
production topology is `compose.production.yaml` + `docker/edge-nginx.conf`; the
checks CI runs are listed under *Commands and gates* in
[`../design/quality.md`](../design/quality.md).

## Runtime model

- PostgreSQL is the authoritative business store. Phoenix Channels send wake
  hints only; HTTP performs every consequential state transition.
- A run mutation is authorized by `runtime_id`, `runtime_epoch`, `run_id`,
  `generation`, `claim_id` and `lease_token` together. Ordinary authority ends at
  lease expiry; terminal transitions and command acknowledgements retain a
  separate eight-minute, current-generation-only grace, and a newer generation
  always wins.
- Lease maintenance and terminal slot deadlines run independently from polling,
  reconciliation, outbox delivery and cleanup. Each control-plane request is
  bounded to 15 seconds. Enrollment, session registration, claim and terminal
  journal recovery retry transport, `429` and `5xx` failures under the daemon
  lifecycle and honor `Retry-After`; permanent client errors fail fast. Periodic
  runtime calls stay at one attempt per cadence, as does ordinary outbox delivery
  unless a local wakeup announces new durable work.
- Claim retries stop at `assignment_expires_at`, release their local slot, and
  wait for a later snapshot instead of pinning expired work during an outage.
- Claim and renewal responses may include an additive `lease_remaining_ms`,
  sampled by the control plane. When present it is strictly positive; exhausted
  leases and legacy-compatible responses omit the field. A daemon subtracts
  request elapsed time and the five-second lease safety margin before arming or
  extending the Windows containment helper, and never derives a local watchdog
  deadline from response receipt wall time. The helper rejects stale sequences
  and stops an armed Job when the last accepted deadline expires, even while the
  daemon owner pipe remains open.
- A daemon executes only machine-local allowlisted agent and workspace bindings.
  Execution machines must synchronize time with NTP: the daemon compares the
  control snapshot's `server_time` with its local clock and warns when absolute
  skew exceeds the five-second lease safety margin. That warning is diagnostic
  and does not alter lease behavior.
- Before downgrading a daemon binary, drain its pending journals. Journal
  decoding rejects unknown fields, so an older binary cannot read journals
  written by a newer binary that adds durable state.
- The operator token creates, reads, cancels and supplies input to tasks. A
  machine token cannot use operator endpoints.

## Control plane deployment

The control plane is supported as a native Linux deployment. Windows and macOS
hosts must run it through Docker. Kubernetes deployments use the same Linux
container image; non-Linux control-plane hosts are not native deployment
targets.

Start here on Windows or macOS: the supported control plane is the Linux
container — the trusted-local `compose.yaml` for a local stack,
`compose.production.yaml` behind the TLS edge for production — not a native
release. A Windows daemon is a separate execution host, not a control plane, and
CI's Windows jobs are tests rather than a deployment. A release serves the
workspace only after the SPA has been staged ([Portal operations](portal.md)),
and the first daemon start needs `-config` and an enrollment environment of its
own (*Daemon configuration*).

These commands start the control service as a private HTTP backend. Run it only
behind a trusted TLS-terminating proxy or load balancer, and prevent port `4000`
from being reachable outside that private network. Exposing this listener
directly is not a supported production deployment.

```sh
cd control
MIX_ENV=prod mix deps.get
MIX_ENV=prod mix release --overwrite
DATABASE_URL=... SECRET_KEY_BASE=... PHX_HOST=control.example.com \
  SYMMETRY_ENROLLMENT_TOKEN=... SYMMETRY_OPERATOR_TOKEN=... \
  _build/prod/rel/symmetry_control/bin/symmetry_control eval \
  'SymmetryControl.Release.migrate()'
PHX_SERVER=true DATABASE_URL=... SECRET_KEY_BASE=... \
  PHX_HOST=control.example.com SYMMETRY_ENROLLMENT_TOKEN=... \
  SYMMETRY_OPERATOR_TOKEN=... \
  _build/prod/rel/symmetry_control/bin/symmetry_control start
```

Required production environment variables:

```text
DATABASE_URL
PHX_HOST
SECRET_KEY_BASE
SYMMETRY_ENROLLMENT_TOKEN
SYMMETRY_OPERATOR_TOKEN
```

Optional cadence settings (defaults in parentheses):

```text
SYMMETRY_HEARTBEAT_INTERVAL_MS (5000)
SYMMETRY_POLL_INTERVAL_MS (5000)
SYMMETRY_LEASE_DURATION_MS (120000, minimum 30000)
SYMMETRY_ASSIGNMENT_DURATION_MS (30000)
SYMMETRY_REAPER_INTERVAL_MS (5000)
```

`control/config/runtime.exs` owns these settings and their minimums; the values
above are its fallbacks at this revision.

The daemon starts lease renewal when 90 seconds remain, or at three quarters of a
shorter granted lease.

### Request-hash write mode

Keep `SYMMETRY_REQUEST_HASH_WRITE_MODE` unset (the safe `legacy` default) through
a rolling control-plane deployment: new releases still write legacy hash
versions, so pre-migration application nodes can coexist without changing
idempotency replay behavior. It is safe to apply the request-hash migration
before the rolling deployment because its database defaults remain
legacy-compatible.

The shipped production Compose service forwards only the environment it declares
(`compose.production.yaml`): it forwards neither
`SYMMETRY_REQUEST_HASH_WRITE_MODE` nor `SYMMETRY_GOALS_ROLLOUT_ENABLED`, so
exporting either on the host and rebuilding leaves the container on runtime's
defaults (`legacy`, and goals rollout off). Set the variable on the service's own
environment — the deployment environment or an untracked env file — and deploy
that configuration.

Enable `SYMMETRY_REQUEST_HASH_WRITE_MODE=canonical` only after every old
control-plane node has exited and cannot return to traffic — draining a node is
not sufficient if its process, container or pod can be restored behind the load
balancer. Then set the variable on every remaining control-plane release and
deploy that configuration. This switches standard request rows to hash version
`2` and command rows to version `3`. New releases keep reading legacy version
`1`, legacy command version `2` and the canonical versions during and after the
cutover.

### Production Compose edge

`compose.production.yaml` is independent from the trusted-local
`compose.yaml`; do not combine the two files. The production topology exposes
only the Nginx edge on ports `80` and `443` (`SYMMETRY_HTTP_PORT` and
`SYMMETRY_HTTPS_PORT` override the host ports). Only the configured `PHX_HOST`
is served: its HTTP listener returns an exact `308` redirect to HTTPS, other Host
values receive `421`, and unknown TLS server names are rejected during the
handshake. Nginx terminates TLS, sends HSTS responses, forwards WebSocket
upgrades, and overwrites client-supplied forwarding headers with the configured
authority before proxying to the private HTTP control service. The control port
is not published.

Provide these values through the deployment environment or an untracked env
file. The certificate and key files must be externally provisioned and must not
be committed.

```text
DATABASE_URL
PHX_HOST
SECRET_KEY_BASE
SYMMETRY_ENROLLMENT_TOKEN
SYMMETRY_OPERATOR_TOKEN
SYMMETRY_TLS_CERT_FILE
SYMMETRY_TLS_KEY_FILE
```

Start the production control plane and edge:

```sh
docker compose -f compose.production.yaml config --quiet
docker compose -f compose.production.yaml up --build --detach
```

The daemon is not part of the production control-plane Compose topology. Run it
on each execution machine with `control_plane_url` set to the public `https://`
address and without `allow_insecure_http`. Plain HTTP is allowed only for an
explicitly trusted local or container network. Configure the canonical control
URL directly: the daemon rejects redirects for authenticated HTTP and WebSocket
requests.

Codex normally reads its authentication from its machine-local credential store.
When the configured CLI instead requires an environment credential, allowlist
only that specific variable, for example `"env_allowlist": ["OPENAI_API_KEY"]`.
Never allowlist a `SYMMETRY_CONTROL_*`, `SYMMETRY_AUTH_*` or other Symmetry
control credential for an agent process; the daemon refuses those prefixes.

### Terminal delivery recovery

When an agent exits, the daemon persists `terminal_pending` before sending the
terminal transition. That run stops renewing its lease and is omitted from active
heartbeat and reconciliation. If the control plane is unavailable, the daemon
retries only terminal delivery and command acknowledgement.

An accepted terminal transition records `accepted`. A newer generation records
`ownership_lost`; a matching generation beyond the terminal grace records
`terminal_grace_expired`. Exact request replay remains valid after the deadline.
A daemon restart may drain the journal with its original claimed epoch as long as
no newer task generation exists.

The local execution slot is released once on terminal or acknowledgement
acceptance, or after at most eight minutes in `terminal_pending`. A local timeout
does not discard the journal or workspace reservation; they remain available for
later delivery and cleanup recovery.

## Daemon configuration

The execution daemon is natively supported on Linux and Windows. FreeBSD is
unsupported. Native macOS daemon support and verification are deferred; do not
deploy or describe a macOS daemon as supported. Linux execution requires
`PIDFD_SIGNAL_PROCESS_GROUP` support (Linux 6.9 or later, permitted by the host
security policy); the daemon checks that capability before launch and fails
closed without a numeric-PID fallback.

Minimal trusted-local configuration using the deterministic fake-agent fixture
(`daemon/cmd/symmetry-fake-agent`):

```json
{
  "control_plane_url": "http://127.0.0.1:4000",
  "allow_insecure_http": true,
  "state_dir": "/var/lib/symmetry",
  "machine_name": "builder-01",
  "agent_profiles": {
    "default": {
      "command": "/absolute/path/to/symmetry-fake-agent",
      "args": [],
      "input_mode": "json",
      "interactive": true,
      "event_format": "jsonl",
      "env_allowlist": []
    }
  },
  "workspaces": {
    "primary": {
      "policy": "existing_checkout",
      "path": "/workspaces",
      "cleanup": "never"
    }
  },
  "runtime": {
    "runtime_key": "default",
    "name": "default",
    "capacity": 1,
    "agent_profile": "default",
    "workspace": "primary"
  }
}
```

Replace the placeholder paths with local absolute paths. The
`daemon/cmd/symmetry-daemon/testdata` configurations are validation fixtures, not
runnable local profiles. Use HTTPS outside a trusted local or container network
and omit `allow_insecure_http`.

`state_dir` is optional for a plain daemon start. When the key is absent, the
daemon resolves a per-user platform default: on Linux
`$XDG_STATE_HOME/symmetry`, falling back to `~/.local/state/symmetry` when
`XDG_STATE_HOME` is unset or not absolute; on Windows `%LOCALAPPDATA%\Symmetry`;
on macOS `~/Library/Application Support/Symmetry` (logic-only row, not
host-verified). An explicit `state_dir` always wins, and a start fails closed
when the environment component it needs (`$HOME`, `%LOCALAPPDATA%`) is unset or
not absolute. The installed owner path never resolves this default: the
installer probe `-print-state-dir` and the owner commands
`-verify-legacy-owner-drained`, `-verify-linux-owner-install` and
`-recover-containment-only` require an explicit `state_dir` and fail closed
without one. The example above keeps the explicit installed-owner form. Only
`state_dir` has a default; workspace `root` and `seed_root` stay explicit.

The daemon takes its configuration path from `-config`, with no default, and its
first start needs `SYMMETRY_ENROLLMENT_TOKEN` in the daemon process's own
environment, not only in the control plane's.

The daemon writes one initial stdin record for every launch:

- `input_mode: "goal"` writes the task goal as plain text followed by a newline,
  and is valid only when `interactive` is false.
- `input_mode: "json"` writes one newline-terminated JSON envelope, for example
  `{"type":"task_input","goal":"Update the project","input":{"branch":"main"}}`.
  The envelope always contains `type`, a non-empty `goal` and `input`; omitted
  task input is encoded as `null`, while an explicit `{}` stays `{}`.

Interactive profiles must use `input_mode: "json"`, keep stdin open, and receive
later input as
`{"type":"provide_input","goal":"Update the project","input":{"answer":"continue"}}`;
follow-up `input` must be an object. Non-interactive profiles receive EOF after
the first record. `interactive: true` with `input_mode: "goal"` is rejected as
invalid configuration.

The daemon persists the enrolled machine credential in its protected state
directory. Before the first enrollment request it also persists
`enrollment.json` with the exact machine name, daemon-generated machine token and
idempotency key; unknown outcomes and restarts replay that request. After
`identity.json` is durable the daemon removes the enrollment intent. Do not share
one state directory between daemon processes.

For Codex non-interactive execution a profile can use:

```json
{
  "command": "/absolute/path/to/codex",
  "args": ["exec", "--sandbox", "workspace-write", "--ephemeral", "--json", "-"],
  "input_mode": "goal",
  "interactive": false,
  "event_format": "jsonl",
  "env_allowlist": []
}
```

## Operator API

Create and inspect a task:

```sh
curl -H "Authorization: Bearer $SYMMETRY_OPERATOR_TOKEN" \
  -H "Idempotency-Key: task-001" \
  -H "Content-Type: application/json" \
  -d '{"work":{"goal":"Update the project","agent_profile":"default","workspace":"primary","input":{}}}' \
  http://127.0.0.1:4000/api/v1/tasks

curl -H "Authorization: Bearer $SYMMETRY_OPERATOR_TOKEN" \
  http://127.0.0.1:4000/api/v1/tasks/TASK_ID
```

Create a task-owned command to cancel or supply consequential input. Each command
requires its own `Idempotency-Key`; the success body is the command resource, not
an updated task:

```sh
curl -X POST -H "Authorization: Bearer $SYMMETRY_OPERATOR_TOKEN" \
  -H "Idempotency-Key: cancel-001" \
  -H "Content-Type: application/json" \
  -d '{"kind":"cancel"}' \
  http://127.0.0.1:4000/api/v1/tasks/TASK_ID/commands

curl -X POST -H "Authorization: Bearer $SYMMETRY_OPERATOR_TOKEN" \
  -H "Idempotency-Key: input-001" \
  -H "Content-Type: application/json" \
  -d '{"kind":"provide_input","payload":{"answer":"continue"}}' \
  http://127.0.0.1:4000/api/v1/tasks/TASK_ID/commands
```

`cancel` has no `payload` member. `provide_input` requires an object payload; `{}`
is valid. New commands return `201`; an exact idempotency replay returns `200`;
conflicting key reuse returns `409 idempotency_conflict`; a command that is not
allowed in the current task state returns `409 state_conflict`. For task
creation, omit `work.input` to preserve `null`; send `"input":{}` to preserve an
explicit empty object.

## Live checks (opt-in)

The daemon's live suites need a running migrated control release and a built
fake-agent binary:

```sh
cd daemon
go build -o /tmp/symmetry-fake-agent ./cmd/symmetry-fake-agent
SYMMETRY_E2E=1 \
SYMMETRY_E2E_URL=http://127.0.0.1:4000 \
SYMMETRY_E2E_AGENT=/tmp/symmetry-fake-agent \
go test -v ./e2e
```

The live suite covers dispatch, capacity, cancellation, waiting for input,
failure isolation, polling fallback, stale fencing and daemon restart.

Run the control plane for this suite with `SYMMETRY_LEASE_DURATION_MS=30000`: the
terminal-retry case (`TestTerminalTransitionRetriesAfterLeaseExpiry` in
`daemon/e2e/core_test.go`) skips while the remaining lease exceeds 45 seconds and
names that setting in its skip message, so a package-level pass without it is not
that case's evidence. CI sets 30 seconds on the release it drives and asserts the
named cases individually (`scripts/ci/go-test-json-gate.py --assert`).

Run the real Codex smoke test separately against the same running control plane:

```sh
cd daemon
SYMMETRY_E2E=1 \
SYMMETRY_E2E_URL=http://127.0.0.1:4000 \
SYMMETRY_ENROLLMENT_TOKEN=development-enrollment-token \
SYMMETRY_OPERATOR_TOKEN=development-operator-token \
SYMMETRY_E2E_REAL_AGENT=1 \
SYMMETRY_E2E_REAL_AGENT_PATH=/absolute/path/to/codex \
go test -v ./e2e -run '^TestRealCodingAgentSmoke$'
```

`TestDaemonSurvivesControlRestart` is intentionally operator-driven. Set
`SYMMETRY_E2E_RESTART_MARKER` to a new file path, start that focused test, wait
for the marker, restart the complete control release against the same database,
and let the test verify that the live run keeps its generation and remains
cancellable.

### Claude Code local transport smoke (opt-in)

The repository includes an opt-in Windows smoke check for the Claude Code build
its `-ExpectedClaudeVersion` guard pins
(`scripts/claude-code-local-transport-smoke.ps1`; its `-ExpectedClaudeVersion`
default is the guard's only owner, and it is not the daemon's promoted Claude pin
— the Claude Code row in [`../design/harness.md`](../design/harness.md)):

```powershell
pwsh -NoLogo -NoProfile -File .\scripts\claude-code-local-transport-smoke.ps1 -ClaudeExecutable claude.exe
```

The provider endpoint is pinned by default to `http://localhost:4141/v1`; override
it only with another absolute loopback `http(s)` URL whose path is exactly `/v1`:

```powershell
pwsh -NoLogo -NoProfile -File .\scripts\claude-code-local-transport-smoke.ps1 `
  -ClaudeExecutable <HOST-PATH>\.local\bin\claude.exe `
  -ProviderEndpoint http://localhost:4141/v1
```

The Claude child receives only Windows launch essentials plus these local-only
environment values; it never inherits `SYMMETRY_*`, provider credentials or other
credential-like parent variables:

```text
ANTHROPIC_BASE_URL=http://localhost:4141
ANTHROPIC_AUTH_TOKEN=<any non-empty dummy token>
ANTHROPIC_DEFAULT_SONNET_MODEL=gpt-5.6-luna
ANTHROPIC_DEFAULT_OPUS_MODEL=gpt-5.6-luna
ANTHROPIC_DEFAULT_HAIKU_MODEL=gpt-5.6-luna
ANTHROPIC_SMALL_FAST_MODEL=gpt-5.6-luna
CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
DISABLE_TELEMETRY=1
DISABLE_ERROR_REPORTING=1
```

Claude Code receives `ANTHROPIC_BASE_URL=http://localhost:4141` because it appends
`/v1/messages` to that base URL. The endpoint must already be running: the script
starts only the Claude child, uses a temporary settings file, applies a bounded
30-second timeout, kills the entire child process tree through a hidden
tree-aware fallback when needed, and removes the settings file in `finally`. It
does not start, stop or reconfigure an existing endpoint or Codex process. It
invokes Claude with `--bare --settings ... --tools "" --permission-mode dontAsk
--permission-prompts none --no-session-persistence --print --output-format json
--model sonnet`. Passing means only that the child exited with code `0` and its
parsed JSON `result` field is exactly `OK`.

This is transport-only evidence: it does not prove native session lifecycle,
resume, cancellation, permissions, usage, provider access or repository work. It
must not change `ClaudeCandidateAdapter`'s `ErrNativeUnverified` result or the
default harness registry.

## Trusted-local Docker Compose

```sh
docker compose config
docker compose up --build
docker compose down
```

The `control-migrate` service must complete before `control` starts. The daemon
image includes only the deterministic fake-agent fixture; replace that profile
with a machine-local agent binary and credentials for real execution hosts.
`docker compose down -v` also deletes local database data and the persisted
provider CLI login state (see the [connections runbook](connections.md)).

## Verification

The canonical gate list is *Commands and gates* in
[`../design/quality.md`](../design/quality.md); CI owns the full matrix. Locally:

```sh
cd control
mix format --check-formatted
mix compile --warnings-as-errors
mix test
```

The Control suite's four `:portal_assets` cases assert against the staged portal
bundle, so `mix test` also needs `control/priv/static/portal/`; without it they
fail with the staging command rather than a raw file error. Build it with
`mise exec pnpm@12.17.0 -- pnpm --filter @symmetry/frontend build` (see the
[portal runbook](portal.md)).

```sh
cd daemon
test -z "$(gofmt -l .)"
go vet ./...
go test ./...
```

`daemon/AGENTS.md` adds `go test -race ./...` for concurrency or execution
changes on supported runners, and requires real Windows tests for Windows
behavior. The live checks above stay opt-in; a skipped suite is unavailable
evidence, never a pass.

A green ordinary CI run does not replace that evidence: documentation-only pull
requests skip the heavy matrix by design, the goal-receipt gate asserts its
selector so a skipped case cannot be read as that evidence, and the Linux native
acceptance workflow is dispatch-only on a host that proves its prerequisites
first.
