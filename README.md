# Symmetry

Symmetry is an agent orchestration and execution platform for engineering work.

The operator-facing engineering workspace is available at `/portal`. It groups
work into projects and Kanban work items, connects repositories and CI, exposes
runtime and connection health, and presents agent runs with outcome-first detail
while retaining paginated raw execution history for debugging. Agent ownership
configures execution intent; the operator starts a run explicitly and can then
provide input, cancel, or retry failed and cancelled work. See
[`docs/goal-02-runbook.md`](docs/goal-02-runbook.md).

## Architecture

The repository is a small monorepo with two independently deployable services:

- `control/` is the Elixir/OTP/Phoenix control plane. It owns orchestration APIs,
  durable state transitions, and live coordination.
- `daemon/` is the Go execution daemon. It runs on an execution machine, owns
  machine-local agent and repository credentials, and reports execution state to
  the control plane over a language-neutral connection.
- PostgreSQL is the authoritative store for control state, tasks, runs, leases,
  and audit history. Live OTP state is reconstructible coordination state, not
  durable business truth.

The control plane does not execute coding agents directly, and the daemon does
not own shared orchestration truth. The cross-language contract is documented
in [`docs/protocol-v1.md`](docs/protocol-v1.md).

## Local Development

Prerequisites are Elixir 1.20 with Erlang/OTP 29 for `control/`, Go 1.27 for
`daemon/`, and PostgreSQL 18 or compatible.

Start PostgreSQL locally, then run the services from separate terminals:

```sh
cd control
mix deps.get
mix ecto.setup
mix phx.server
```

```sh
cd daemon
go test ./...
go run ./cmd/symmetry-daemon -config /absolute/path/to/daemon.json
```

The local control plane uses port `4000`. A daemon using a plain HTTP URL must
set `allow_insecure_http: true`; use that only for an explicitly trusted local
or container network. Agent commands, workspace paths, and allowed environment
variables remain machine-local configuration. Start from the daemon
configuration example in [`docs/goal-01-runbook.md`](docs/goal-01-runbook.md)
and replace its paths with local absolute paths. The
`cmd/symmetry-daemon/testdata` configuration is validation test data, not a
runnable local profile.

For the agent's first stdin record, `input_mode: "goal"` sends the plain-text
task goal followed by a newline. `input_mode: "json"` sends one JSON envelope
containing both `goal` and structured `input`, followed by a newline. The
protocol details, including interactive input behavior and command
acknowledgement authentication, are in
[`docs/protocol-v1.md`](docs/protocol-v1.md).

The operator API creates tasks at `POST /api/v1/tasks` and creates durable
cancel or input instructions at `POST /api/v1/tasks/{task_id}/commands`. Both
write operations require `Idempotency-Key`; task commands return command
resources rather than task snapshots. See the runbook for curl examples and
the protocol document for response and status semantics.

## Docker Compose

The default `compose.yaml` is a trusted-local development stack. It migrates
PostgreSQL, starts `control`, and runs a deterministic fake agent through the
real daemon execution path. PostgreSQL data is kept in the `postgres_data` named
volume and the control plane is only published to `127.0.0.1:4000` over HTTP.

```sh
docker compose config
docker compose up --build
docker compose down
```

Use `docker compose down -v` only when intentionally deleting local database
data. The daemon image is a static Go binary and deliberately does not contain
machine-local coding-agent or repository credentials.

`compose.production.yaml` is an independent production topology: Nginx is the
only public service, terminating TLS on ports `80` and `443`, while the control
plane remains on a private Docker network. It requires deployment-provided
database, application, enrollment, operator, and TLS credentials. See the
production deployment section in [`docs/goal-01-runbook.md`](docs/goal-01-runbook.md).

## Goal 1 Status

Goal 1 is implemented. The control plane persists machines, runtimes, tasks,
runs, leases, transitions, commands, and events in PostgreSQL. The daemon owns
local identity, fenced claims, workspace isolation, process-tree supervision,
Phoenix wake notifications, polling fallback, reconciliation, cancellation,
human input, and durable local outboxes.

See [`docs/goal-01-runbook.md`](docs/goal-01-runbook.md) for configuration,
operator examples, release migration, and end-to-end verification commands.
