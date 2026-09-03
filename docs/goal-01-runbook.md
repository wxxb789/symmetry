# Goal 1 Runbook

This runbook covers the Goal 1 control plane and execution daemon. The wire
contract and lifecycle rules are defined in [`protocol-v1.md`](protocol-v1.md).

## Runtime Model

- PostgreSQL is the authoritative business store.
- Phoenix Channels send wake hints only. HTTP performs every consequential
  state transition.
- A run mutation is authorized by `runtime_id`, `runtime_epoch`, `run_id`,
  `generation`, `claim_id`, and `lease_token` together.
- A daemon executes only machine-local allowlisted agent and workspace bindings.
- The operator token creates, reads, cancels, and supplies input to tasks. A
  machine token cannot use operator endpoints.

## Control Plane

Required production environment variables:

```text
DATABASE_URL
SECRET_KEY_BASE
SYMMETRY_ENROLLMENT_TOKEN
SYMMETRY_OPERATOR_TOKEN
```

Optional cadence settings:

```text
SYMMETRY_HEARTBEAT_INTERVAL_MS
SYMMETRY_POLL_INTERVAL_MS
SYMMETRY_LEASE_DURATION_MS
SYMMETRY_ASSIGNMENT_DURATION_MS
SYMMETRY_REAPER_INTERVAL_MS
```

Build, migrate, and start a release:

```sh
cd control
MIX_ENV=prod mix deps.get
MIX_ENV=prod mix release --overwrite
DATABASE_URL=... SECRET_KEY_BASE=... \
  SYMMETRY_ENROLLMENT_TOKEN=... SYMMETRY_OPERATOR_TOKEN=... \
  _build/prod/rel/symmetry_control/bin/symmetry_control eval \
  'SymmetryControl.Release.migrate()'
PHX_SERVER=true DATABASE_URL=... SECRET_KEY_BASE=... \
  SYMMETRY_ENROLLMENT_TOKEN=... SYMMETRY_OPERATOR_TOKEN=... \
  _build/prod/rel/symmetry_control/bin/symmetry_control start
```

Codex normally reads its authentication from its machine-local credential store.
When the configured CLI instead requires an environment credential, allowlist
only that specific variable, for example `"env_allowlist": ["OPENAI_API_KEY"]`.
Never allowlist a `SYMMETRY_CONTROL_*`, `SYMMETRY_AUTH_*`, or other Symmetry
control credential for an agent process.

## Daemon Configuration

Minimal trusted-local configuration using the deterministic fixture:

```json
{
  "control_plane_url": "http://127.0.0.1:4000",
  "allow_insecure_http": true,
  "state_dir": "/var/lib/symmetry",
  "machine_name": "builder-01",
  "agent_profiles": {
    "default": {
      "command": "/symmetry-fake-agent",
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

Use HTTPS outside a trusted local or container network and omit
`allow_insecure_http`. The daemon persists the enrolled machine credential in
its protected state directory. Do not share one state directory between daemon
processes.

The daemon writes one initial stdin record for every launch. `input_mode: "goal"`
writes the task goal as plain text followed by a newline. `input_mode: "json"`
writes one newline-terminated JSON envelope containing both task fields, for
example `{"goal":"Update the project","input":{"branch":"main"}}`.
Interactive profiles keep stdin open for later `provide_input` commands;
non-interactive profiles receive EOF after that first record.

For Codex non-interactive execution, a profile can use:

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

Create a task-owned command to cancel or supply consequential input. Each
command requires its own `Idempotency-Key`; the success body is the command
resource, not an updated task:

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

`cancel` has no `payload` member. `provide_input` requires an object payload;
`{}` is valid. New commands return `201`; an exact idempotency replay returns
`200`; conflicting key reuse returns `409 idempotency_conflict`; a command that
is not allowed in the current task state returns `409 state_conflict`.

For task creation, omit `work.input` to preserve `null`; send `"input":{}` to
preserve an explicit empty object.

## Verification

Default gates:

```sh
cd control
mix format --check-formatted
mix compile --warnings-as-errors
mix test

cd ../daemon
test -z "$(gofmt -l .)"
go vet ./...
go test -count=3 ./...
```

Live tests require a running migrated control release and a built fake-agent
binary:

```sh
cd daemon
go build -o /tmp/symmetry-fake-agent ./cmd/symmetry-fake-agent
SYMMETRY_E2E=1 \
SYMMETRY_E2E_URL=http://127.0.0.1:4000 \
SYMMETRY_E2E_AGENT=/tmp/symmetry-fake-agent \
go test -v ./e2e
```

The live suite covers dispatch, capacity, cancellation, waiting for input,
failure isolation, polling fallback, stale fencing, and daemon restart. Run the
real Codex smoke test separately against the same running control plane:

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

## Docker Compose

```sh
docker compose config
docker compose up --build
docker compose down
```

The `control-migrate` service must complete before `control` starts. The daemon
image includes only the deterministic fixture; replace that profile with a
machine-local agent binary and credentials for real execution hosts.
