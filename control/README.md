# Symmetry Control

`control/` is the Phoenix control plane for Symmetry. It owns durable
orchestration state and exposes the versioned API consumed by operators and
execution daemons.

For local development, install dependencies and start the endpoint:

```sh
mix setup
mix phx.server
```

The local endpoint is available at `http://127.0.0.1:4000`. Open
`http://127.0.0.1:4000/portal` for the engineering workspace and sign in with
the configured operator token. Local HTTP is only for an explicitly trusted
development or container network.

In production, do not publish Phoenix directly. It must receive private HTTP
only from the trusted TLS edge; the public boundary terminates HTTPS before
requests reach this service. See the repository [README](../README.md) for the
topology and [`docs/goal-01-runbook.md`](../docs/goal-01-runbook.md) for
deployment and operational steps. The API contract is
[`docs/protocol-v1.md`](../docs/protocol-v1.md).

Portal project, Kanban, health, and execution workflows are documented in
[`docs/goal-02-runbook.md`](../docs/goal-02-runbook.md). Assigning a work item
to an agent records its execution settings; `Start run` explicitly creates the
durable task, after which the portal supports input, cancellation, and legal
failed/cancelled retries.

GitHub and Azure DevOps connection setup, credential handling, resource binding,
external ownership rules, and synchronization are documented in
[`docs/goal-03-runbook.md`](../docs/goal-03-runbook.md).

Chat, durable instructions, decision packets, and cooperative worker controls
are documented in [`docs/goal-04-runbook.md`](../docs/goal-04-runbook.md).
