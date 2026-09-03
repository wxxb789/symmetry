# Symmetry Control

`control/` is the Phoenix control plane for Symmetry. It owns durable
orchestration state and exposes the versioned API consumed by operators and
execution daemons.

For local development, install dependencies and start the endpoint:

```sh
mix setup
mix phx.server
```

The local endpoint is available at `http://127.0.0.1:4000`. Local HTTP is only
for an explicitly trusted development or container network.

In production, do not publish Phoenix directly. It must receive private HTTP
only from the trusted TLS edge; the public boundary terminates HTTPS before
requests reach this service. See the repository [README](../README.md) for the
topology and [`docs/goal-01-runbook.md`](../docs/goal-01-runbook.md) for
deployment and operational steps. The API contract is
[`docs/protocol-v1.md`](../docs/protocol-v1.md).
