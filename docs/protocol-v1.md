# Symmetry Daemon Protocol v1

This document defines the first durable contract between the Phoenix control
plane and the Go execution daemon. JSON over HTTP owns every state-changing
operation. A Phoenix Channel provides low-latency notifications, but a missed
notification never loses work because the daemon polls and reconciles through
the HTTP API.

## Authority And Identity

PostgreSQL is the only authority for machines, runtimes, tasks, runs, leases,
commands, and execution history. OTP processes, PubSub messages, WebSocket
connections, and daemon memory are rebuildable coordination state.

The protocol uses these identifiers:

- `machine_id`: stable identity for an enrolled execution machine.
- `runtime_id`: stable identity for one configured agent runtime on a machine.
- `daemon_instance_id`: new UUID for each daemon process start.
- `runtime_epoch`: monotonically increasing connection generation issued when
  a runtime registers a new daemon instance.
- `task_id`: stable identity for submitted work.
- `run_id`: identity for one execution attempt of a task.
- `generation`: monotonically increasing task execution generation.
- `claim_id`: daemon-generated UUID for one durable claim-and-launch intent.
- `lease_token`: opaque UUID issued only after a run is claimed.
- `event_id`: daemon-generated UUID used to deduplicate an appended event.
- `transition_id`: daemon-generated UUID used to deduplicate a state change.
- `command_id`: server-generated UUID for a durable control instruction.

Every execution-side mutation after claim must match this fence:

```text
runtime_id + runtime_epoch + run_id + generation + claim_id + lease_token
```

The control plane rejects a mutation when the runtime epoch is stale, the
lease is expired, the run is terminal, or the task has advanced to a newer
generation. A stale execution can never overwrite the result of a newer one.

## Durable Lifecycles

Task states:

```text
queued -> assigned -> claimed -> running -> waiting_for_input -> running
assigned | claimed | running | waiting_for_input -> cancelling -> cancelled
queued -> cancelled
running -> completed | failed
assigned | claimed | running | waiting_for_input -> queued (new generation only)
```

Run states:

```text
assigned -> claimed -> running -> waiting_for_input -> running
assigned | claimed | running | waiting_for_input -> cancelling -> cancelled
running -> completed | failed
assigned | claimed | running | waiting_for_input | cancelling -> expired
```

A retry or reclaim creates a new `run_id` and increments `generation`. A run
never changes generation and a lease token is never reused. Terminal states are
immutable. `waiting_for_input` remains capacity-bearing until resumed,
cancelled, or expired. `assigned`, `claimed`, `running`,
`waiting_for_input`, and `cancelling` all reserve one runtime slot.

Cancellation and completion serialize on the task and current run rows. If
completion commits first, cancellation returns the existing terminal result.
If cancellation commits first, the run moves to `cancelling`; later completion
or failure reports are rejected, while fenced events and a final `cancelled`
transition remain allowed. If the daemon does not acknowledge cancellation
before lease expiry, the reaper finalizes the run and task as `cancelled`.

## Authentication

`POST /api/v1/daemon/enroll` accepts the configured one-time enrollment bearer
token and returns a stable `machine_id` plus a machine bearer token. The control
plane stores only the SHA-256 digest of the machine token.

All other daemon endpoints and the WebSocket connection use the machine bearer
token. A runtime must belong to the authenticated machine. The Task Control API
uses a separate operator bearer token; an execution machine credential cannot
submit, cancel, or provide input to tasks. Coding-agent, Git, SSH, and
repository-provider credentials remain on the execution machine and must not
appear in protocol payloads.

Production traffic must use TLS. Plain HTTP is permitted only on an explicitly
trusted local or container network.

## HTTP Endpoints

### Enroll A Machine

```http
POST /api/v1/daemon/enroll
Authorization: Bearer <enrollment-token>
Content-Type: application/json
```

```json
{
  "machine": {
    "name": "builder-01"
  }
}
```

```json
{
  "machine_id": "uuid",
  "machine_token": "opaque-token"
}
```

### Register A Daemon Session And Runtime

```http
POST /api/v1/daemon/sessions
Authorization: Bearer <machine-token>
```

```json
{
  "daemon_instance_id": "uuid",
  "runtimes": [
    {
      "runtime_key": "default",
      "name": "Local Codex",
      "capacity": 1,
      "agent_profile": "codex",
      "workspace": "primary",
      "capabilities": {}
    }
  ]
}
```

```json
{
  "runtimes": [
    {
      "runtime_key": "default",
      "runtime_id": "uuid",
      "runtime_epoch": 3
    }
  ],
  "heartbeat_interval_ms": 5000,
  "poll_interval_ms": 5000,
  "lease_duration_ms": 30000,
  "websocket_path": "/socket/websocket?vsn=2.0.0"
}
```

`runtime_key` is stable in machine-local configuration and unique within a
machine. Registering a different `daemon_instance_id` increments each declared
runtime's `runtime_epoch` and fences the previous process. Repeating the same
instance registration is idempotent and returns the existing epochs. Lowering
capacity prevents new assignment while current reservations meet or exceed the
new value; it does not cancel existing work.

A runtime is online when its last valid heartbeat is no older than three
heartbeat intervals. The control plane marks it offline after that threshold,
but does not reassign its work until each run's durable lease expires.

### Heartbeat And Work Snapshot

```http
POST /api/v1/runtimes/{runtime_id}/heartbeat
GET  /api/v1/runtimes/{runtime_id}/work?runtime_epoch={epoch}
```

The heartbeat request carries active run references:

```json
{
  "runtime_epoch": 3,
  "active_runs": [
    {
      "run_id": "uuid",
      "generation": 2,
      "claimed_runtime_epoch": 3,
      "claim_id": "uuid",
      "lease_token": "uuid",
      "state": "running"
    }
  ]
}
```

Both heartbeat and work return the complete current snapshot for that runtime,
not a destructive queue read:

```json
{
  "assignments": [
    {
      "run_id": "uuid",
      "task_id": "uuid",
      "generation": 2,
      "assignment_expires_at": "2026-09-02T00:00:30Z",
      "work": {
        "goal": "Run the configured agent",
        "agent_profile": "codex",
        "workspace": "primary",
        "input": {}
      }
    }
  ],
  "commands": [
    {
      "command_id": "uuid",
      "run_id": "uuid",
      "generation": 2,
      "kind": "cancel",
      "payload": {},
      "issued_at": "2026-09-02T00:00:20Z"
    }
  ],
  "server_time": "2026-09-02T00:00:21Z"
}
```

The same live assignments and unacknowledged commands may be returned multiple
times. The daemon must treat the snapshot idempotently. It polls while any
runtime is registered, even when capacity is full, because cancellation and
human-input commands must remain deliverable during active execution.

### Claim And Renew A Run

```http
POST /api/v1/runs/{run_id}/claim
POST /api/v1/runs/{run_id}/heartbeat
```

Before the request, the daemon atomically writes a local journal entry containing
`run_id`, `generation`, the current `runtime_epoch`, and a new `claim_id`.
It reuses that `claim_id` after an HTTP timeout or network reconnect by the
same daemon instance and never launches the process until the returned lease is
durably journaled.

```json
{
  "runtime_id": "uuid",
  "runtime_epoch": 3,
  "generation": 2,
  "claim_id": "uuid"
}
```

Claim is a single PostgreSQL transaction that verifies assignment ownership,
runtime capacity, and expiry before issuing a lease:

```json
{
  "run_id": "uuid",
  "task_id": "uuid",
  "generation": 2,
  "claim_id": "uuid",
  "lease_token": "uuid",
  "lease_expires_at": "2026-09-02T00:00:30Z",
  "work": {
    "goal": "Run the configured agent",
    "agent_profile": "codex",
    "workspace": "primary",
    "input": {}
  }
}
```

Repeating the same `claim_id` returns the same lease. A different `claim_id`
for an already claimed run returns `409 ownership_lost`; therefore two local
paths cannot obtain the same fence and independently launch the agent.

A new daemon process registers a new instance and epoch. It never retries or
launches a claim journaled under an older epoch. Reconciliation returns
`stale_stop` for that journal entry; the daemon terminates any surviving local
process and the control plane waits for the old lease to expire before requeue.

The run heartbeat carries the complete fence and extends an unexpired lease.
It never revives an expired or terminal run:

```json
{
  "runtime_id": "uuid",
  "runtime_epoch": 3,
  "generation": 2,
  "claim_id": "uuid",
  "lease_token": "uuid"
}
```

```json
{
  "lease_expires_at": "2026-09-02T00:00:45Z",
  "commands": []
}
```

### Append Events And Advance State

```http
POST /api/v1/runs/{run_id}/events
POST /api/v1/runs/{run_id}/state
```

Both requests carry the complete fence and `claim_id`. Events are append-only
and deduplicated by `(run_id, event_id)`:

```json
{
  "runtime_id": "uuid",
  "runtime_epoch": 3,
  "generation": 2,
  "claim_id": "uuid",
  "lease_token": "uuid",
  "events": [
    {
      "event_id": "uuid",
      "sequence": 4,
      "kind": "progress",
      "occurred_at": "2026-09-02T00:00:10Z",
      "payload": {"message": "Tests are running"}
    }
  ]
}
```

Materialized lifecycle state changes only through the state endpoint:

```json
{
  "runtime_id": "uuid",
  "runtime_epoch": 3,
  "generation": 2,
  "claim_id": "uuid",
  "lease_token": "uuid",
  "transition_id": "uuid",
  "state": "waiting_for_input",
  "payload": {
    "question": "Choose the target branch",
    "recommended_choice": "main"
  }
}
```

Valid targets are `running`, `waiting_for_input`, `completed`, `failed`,
and `cancelled`. Completion and failure include a structured result or failure
payload. `(run_id, transition_id)` is the retry identity: repeating the same ID
and canonical JSON body returns the stored response; reusing the ID with a
different body returns `409 idempotency_conflict`. A transition whose state has
already advanced under a different ID returns `409 state_conflict`.

### Reconcile After Start Or Reconnect

```http
POST /api/v1/runtimes/{runtime_id}/reconcile
```

The daemon sends its current local run journal:

```json
{
  "runtime_epoch": 3,
  "runs": [
    {
      "run_id": "uuid",
      "generation": 2,
      "claimed_runtime_epoch": 3,
      "claim_id": "uuid",
      "lease_token": "uuid",
      "local_state": "running",
      "last_event_sequence": 4
    }
  ]
}
```

```json
{
  "decisions": [
    {
      "run_id": "uuid",
      "generation": 2,
      "decision": "continue",
      "lease_expires_at": "2026-09-02T00:00:45Z"
    }
  ],
  "assignments": [],
  "commands": []
}
```

Each decision is one of `continue`, `cancel`, `stale_stop`, `terminal`, or
`unknown_stop`. The response also includes the normal assignment and command
snapshot. The daemon must stop a local process for `stale_stop` or
`unknown_stop` and must not retry fenced writes. `continue` is valid only
when `claimed_runtime_epoch` equals the runtime's current epoch and every other
fence field matches; its response returns the current lease expiry without
changing the fence.

For `cancel`, the daemon terminates the complete execution process tree,
acknowledges the command, and makes a fenced `cancelled` transition. For
`terminal`, it stops any still-running local child and records the server's
terminal result in its journal without sending another terminal transition.

### Task Control API

```http
POST /api/v1/tasks
GET  /api/v1/tasks/{task_id}
POST /api/v1/tasks/{task_id}/cancel
POST /api/v1/tasks/{task_id}/input
POST /api/v1/commands/{command_id}/ack
```

Task submission accepts an `Idempotency-Key` header and a work object containing
`goal`, `agent_profile`, `workspace`, and structured `input`. The scheduler
matches the requested agent profile and workspace against a runtime before
assignment. Repeating the same key and payload returns the existing task;
reusing the key with different payload returns `409 idempotency_conflict`.
All Task Control API requests authenticate with the configured operator bearer
token rather than a daemon machine token.

Cancellation locks the task and its current run when one exists. A queued task
with no capacity-bearing run moves directly to `cancelled` and creates no
command. If `completed`, `failed`, or `cancelled` committed first,
cancellation returns the existing terminal result. Only a live run moves with
its task to `cancelling` and inserts one durable `cancel` command in the same
transaction. The command includes the current `run_id` and `generation`. It
remains in heartbeat, work, and reconcile snapshots until the daemon reports
the run `cancelled`. Duplicate cancellation returns the same command. Expiry
finalizes a still-cancelling run as cancelled.

Human input is accepted only while the current run is `waiting_for_input`.
`Idempotency-Key` identifies the input submission. In one transaction the
control plane locks the task and current run, rechecks the waiting state, and
stores a `provide_input` command bound to that `run_id` and `generation`.
If cancellation, expiry, reclaim, or a terminal result committed first, the
input is rejected and is never delivered to a later generation. The run stays
`waiting_for_input` until the daemon delivers the input, acknowledges the
command, and makes a fenced transition back to `running`. The command remains
visible until both acknowledgement and the target lifecycle transition exist,
so either request may be retried after an unknown outcome.

Command acknowledgement carries the complete execution fence, `command_id`,
outcome (`applied`, `rejected`, or `failed`), and an acknowledgement UUID.
Repeating the same acknowledgement UUID and body is idempotent; reusing it with
a different body returns `409 idempotency_conflict`. Acknowledgement records
delivery outcome but never changes run state or removes a lifecycle command by
itself.

## Phoenix Channel Notifications

The daemon connects with Phoenix Channel protocol v2 and joins
`daemon:{machine_id}`. The channel emits hints only:

```json
{"type":"work_available","runtime_id":"uuid"}
{"type":"command_available","runtime_id":"uuid","command_id":"uuid"}
{"type":"reconcile_required","reason":"server_restart"}
```

On any hint, the daemon immediately fetches its HTTP work snapshot. It also
polls continuously while registered; lack of capacity suppresses claiming new
assignments, not command delivery. Channel disconnects, `phx_error`, and
`phx_close` trigger reconnect with backoff but do not fail active runs.

## Error Contract

Errors use one JSON envelope:

```json
{"error":{"code":"ownership_lost","message":"execution lease is no longer authoritative"}}
```

- `400 invalid_request`: malformed or unsupported input; do not retry unchanged.
- `401 unauthenticated`: missing or invalid credential.
- `403 forbidden`: authenticated machine does not own the resource.
- `404 not_found`: resource is absent; reconcile local state.
- `409 capacity_exhausted`: retry after the next snapshot.
- `409 idempotency_conflict`: the same key was reused with different input.
- `409 ownership_lost`: stop the local execution and reconcile immediately.
- `409 state_conflict`: state already advanced or a terminal payload conflicts.
- `410 assignment_expired`: discard the assignment and fetch a new snapshot.
- `422 invalid_transition`: daemon or client state-machine error.
- `429` or `503`: retry with backoff and `Retry-After` when present.

An HTTP timeout has an unknown outcome. The daemon retries only idempotent reads
or a write carrying the same claim, transition, command acknowledgement,
idempotency, or event identifier.

## Recovery Invariants

1. Notifications may be duplicated or lost without changing correctness.
2. Claim, event append, terminal reporting, and cancellation are idempotent.
3. Capacity is reserved by durable assigned and active runs, not client claims
   about free slots.
4. Assignment locks one queued task and one eligible runtime, increments the
   task generation, creates exactly one run, and reserves capacity in one
   transaction. Database constraints enforce unique `(task_id, generation)`
   and at most one capacity-bearing run per task.
5. Claim, completion, cancellation, and expiry lock task, run, and runtime rows
   in that order. Expiry releases capacity once, makes the old run terminal,
   and requeues the task before a later scheduler creates a higher generation.
6. Multiple scheduler nodes use row locking with skip-locked selection or an
   equivalent compare-and-set transaction; live process ownership is never the
   mutual-exclusion mechanism.
7. A server or BEAM restart reconstructs work from PostgreSQL and daemon
   reconciliation; no in-memory mailbox is required for recovery.
8. Failures in one runtime or run are persisted locally to that execution and
   do not crash unrelated schedulers, runtime connections, or runs.
