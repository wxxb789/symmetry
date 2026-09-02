# Symmetry

Symmetry is an agent orchestration and execution platform for engineering work.

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
go run ./cmd/symmetry-daemon -config cmd/symmetry-daemon/testdata/valid-config.json
```

The local control plane uses port `4000`. Configure the services with their
documented environment variables; Docker Compose supplies development defaults
for its isolated network.

## Docker Compose

The default Compose stack starts PostgreSQL, `control`, and `daemon`. PostgreSQL
data is kept in the `postgres_data` named volume and the control plane is only
published to `127.0.0.1:4000`.

```sh
docker compose config
docker compose up --build
docker compose down
```

Use `docker compose down -v` only when intentionally deleting local database
data. The daemon image is a static Go binary and deliberately does not contain
machine-local coding-agent or repository credentials.

## Goal 1 Status

This repository is in bootstrap stage for Goal 1. The current work establishes
the monorepo layout and reproducible development/build entry points only. It
does not yet implement registration, dispatch, leases, reconciliation, agent
execution, or the control-daemon protocol.
