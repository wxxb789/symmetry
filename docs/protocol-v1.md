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

The control plane rejects an ordinary mutation when the runtime epoch is stale,
the lease is expired, the run is terminal, or the task has advanced to a newer
generation. Terminal transitions and command acknowledgements have one bounded
exception: the original claimed fence may be used for up to eight minutes after
lease expiry while that run remains the task's current generation. This
terminal-only path still requires the original runtime, claimed runtime epoch,
claim ID, lease token, run ID, and generation. It does not require the runtime's
current connection epoch to remain unchanged, so a restarted daemon can drain a
durable terminal journal. A stale execution can never overwrite the result of
a newer generation.

## Durable Lifecycles

Task states:

```text
queued -> assigned -> claimed -> running -> waiting_for_input -> running
assigned | claimed | running | waiting_for_input -> cancelling -> cancelled
queued -> cancelled
claimed | running | waiting_for_input -> completed | failed
assigned | claimed | running | waiting_for_input -> queued (new generation only)
```

Run states:

```text
assigned -> claimed -> running -> waiting_for_input -> running
assigned | claimed | running | waiting_for_input -> cancelling -> cancelled
claimed | running | waiting_for_input -> completed | failed
assigned | claimed | running | waiting_for_input | cancelling -> expired
expired -> completed | failed | cancelled (current-generation terminal grace only)
```

A retry or reclaim creates a new `run_id` and increments `generation`. A run
never changes generation and a lease token is never reused. `completed`,
`failed`, and `cancelled` are immutable. An `expired` run may accept one terminal
result during its bounded grace only while its task is still queued at the same
generation; an operator-applied queued cancellation cannot be overwritten.
`waiting_for_input` remains capacity-bearing until resumed, cancelled, or
expired. `assigned`, `claimed`, `running`,
`waiting_for_input`, and `cancelling` all reserve one runtime slot.

Cancellation and completion serialize on the task and current run rows. If
completion commits first, cancellation returns the existing terminal result.
If cancellation commits first, the run moves to `cancelling`; later completion
or failure reports are rejected, while fenced events and a final `cancelled`
transition remain allowed. If the daemon does not acknowledge cancellation
before lease expiry, the reaper finalizes the run and task as `cancelled`.

## Authentication

`POST /api/v1/machines` accepts the configured one-time enrollment bearer token,
a daemon-generated machine token, and a durable idempotency key. It returns a
stable `machine_id` plus that same machine bearer token. The control plane stores
only the SHA-256 digest of the machine token.

All other daemon HTTP endpoints use the machine bearer token. The WebSocket
upgrade sends the same token in `X-Symmetry-Token`, because Phoenix exposes
`x-` request headers to socket connect callbacks without putting credentials in
the URL. A runtime must belong to the authenticated machine. The Task Control
API uses a separate operator bearer token; an execution machine credential
cannot submit tasks or create task commands. Command acknowledgement is not a
Task Control API operation: it is a machine-authenticated, fenced daemon
mutation. An invalid, unknown, or missing credential returns `401`; a valid
credential of the wrong class, or a machine credential that does not own a
resource, returns `403`. Coding-agent, Git, SSH, and repository-provider
credentials remain on the execution machine and must not appear in protocol
payloads.

Production traffic must use TLS. Plain HTTP is permitted only on an explicitly
trusted local or container network.

## HTTP Endpoints

### Enroll A Machine

```http
POST /api/v1/machines
Authorization: Bearer <enrollment-token>
Idempotency-Key: <daemon-generated-key>
Content-Type: application/json
```

```json
{
  "machine": {
    "name": "builder-01"
  },
  "machine_token": "daemon-generated-opaque-token"
}
```

```json
{
  "machine_id": "uuid",
  "machine_token": "opaque-token"
}
```

The daemon persists the exact idempotency key, machine name, and machine token
before the first request. A new enrollment returns `201`; an exact replay returns
the original `machine_id` and request token with `200`; reusing the key with a
different name or token returns `409 idempotency_conflict`. The daemon rejects a
response whose token differs from its request and removes the enrollment intent
only after the machine identity is durable locally.

### Register A Daemon Session And Runtime

```http
PUT /api/v1/machines/{machine_id}/sessions/{daemon_instance_id}
Authorization: Bearer <machine-token>
```

```json
{
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
  "lease_duration_ms": 120000,
  "websocket_path": "/socket/websocket?vsn=2.0.0"
}
```

`runtime_key` is stable in machine-local configuration and unique within a
machine. Registering a different `daemon_instance_id` increments each declared
runtime's `runtime_epoch` and fences the previous process. Repeating the same
instance registration is idempotent and returns the existing epochs. Lowering
capacity prevents new assignment while current reservations meet or exceed the
new value; it does not cancel existing work.

The default execution lease is two minutes, and registration rejects lease
durations below 30 seconds. The daemon maintains leases in an
independent liveness lifecycle, so a blocked dispatch, reconciliation, outbox,
or workspace operation cannot starve renewal. Renewal begins when the remaining
lease reaches `min(90 seconds, 3/4 of the granted duration)`. Only a locally
live, non-terminal agent process is eligible.

A runtime is online when its last valid heartbeat is no older than three
heartbeat intervals. The control plane marks it offline after that threshold,
but does not reassign its work until each run's durable lease expires.

### Update Runtime And Read Dispatch Snapshot

```http
PATCH /api/v1/runtimes/{runtime_id}
GET   /api/v1/runtimes/{runtime_id}/dispatch?runtime_epoch={epoch}
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

The runtime update returns the complete current dispatch snapshot for that
runtime. Dispatch is also available as a non-destructive GET for recovery and
polling:

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
PUT   /api/v1/runs/{run_id}/claims/{claim_id}
PATCH /api/v1/runs/{run_id}/lease
```

Before the request, the daemon atomically writes a local journal entry containing
`run_id`, `generation`, the current `runtime_epoch`, and a new `claim_id`. It
uses that `claim_id` in the claim resource path after an HTTP timeout or network
reconnect by the same daemon instance and never launches the process until the
returned lease is durably journaled.

```json
{
  "runtime_id": "uuid",
  "runtime_epoch": 3,
  "generation": 2
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
Each renewal uses its own request context with a maximum 15-second deadline. It
never revives an expired or terminal run:

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
PUT  /api/v1/runs/{run_id}/transitions/{transition_id}
```

Both requests carry the complete fence and `claim_id`. Events are append-only
and deduplicated by `(run_id, event_id)`. Event append returns `204 No Content`;
clients must not decode or expect a response body:

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

Daemon-originated materialized lifecycle state changes use the transition resource:

```json
{
  "runtime_id": "uuid",
  "runtime_epoch": 3,
  "generation": 2,
  "claim_id": "uuid",
  "lease_token": "uuid",
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
and canonical JSON body is idempotent. The response is assembled from the
current run representation at replay time, with the stored transition state and
terminal payload projection (`result` or `failure`); it is not a byte-frozen
historical HTTP response. Reusing the ID with a different body returns
`409 idempotency_conflict`. A transition whose state has already advanced under
a different ID returns `409 state_conflict`. If the reaper has already finalized
`cancelling` as `cancelled`, any new late terminal transition returns
`409 ownership_lost`; an exact stored transition replay remains idempotent.

When the agent process exits, the daemon durably enters `terminal_pending`,
records the intended terminal state and first terminal-pending time, stops lease
renewal, and removes the run from active heartbeat and reconciliation input.
Any in-flight renewal is cancelled and its late response cannot update the
journal. Ordinary events and non-terminal transitions are not authorized by
terminal grace; the daemon retires them locally instead of sending them, so
they cannot block or determine the terminal verdict. Control accepts direct
`claimed` or `waiting_for_input` completion/failure when an undelivered local
transition was retired this way. Only a successful terminal transition records
`accepted`. A terminal transition response or a command acknowledgement
response may independently record the conclusive verdict `ownership_lost` or
`terminal_grace_expired`. Exact transition replay remains valid after the grace
deadline, but a new transition ID is rejected. A newer task generation always
returns `ownership_lost`.

The daemon releases its local execution slot once when its terminal transition
response records `accepted`, when either terminal response records a conclusive
rejection, or when eight minutes have elapsed since it entered
`terminal_pending`. This local capacity deadline is separate from Control's
acceptance deadline at `lease_expires_at + 8 minutes`; terminal retries are
paced to reach the earlier remote deadline when necessary. Local expiry releases
capacity only: the durable journal remains until the control plane accepts or
conclusively rejects terminal delivery and workspace cleanup succeeds.

### Reconcile After Start Or Reconnect

```http
PUT /api/v1/runtimes/{runtime_id}/reconciliation
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

`terminal_pending` journals are intentionally omitted from ordinary heartbeat,
lease renewal, and reconciliation. After daemon restart they retain the original
claimed fence and continue only terminal transition and command acknowledgement
delivery. A newer task generation still fences the retained journal.

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

For `cancel`, the daemon terminates the complete execution process tree, makes
a fenced `cancelled` transition, and independently acknowledges the command. For
`terminal`, it stops any still-running local child and records the server's
terminal result in its journal without sending another terminal transition.

### Task Control API

```http
POST /api/v1/tasks
GET  /api/v1/tasks/{task_id}
POST /api/v1/tasks/{task_id}/commands
```

Task submission requires an `Idempotency-Key` header and accepts a work object
containing `goal`, `agent_profile`, `workspace`, and structured `input`. The
scheduler matches the requested agent profile and workspace against a runtime
before assignment. Repeating the same key and payload returns the existing task;
reusing the key with different payload returns `409 idempotency_conflict`. An
omitted task input is stored and returned as `null`; an explicit `{}` stays `{}`.
All Task Control API requests authenticate with the configured operator bearer
token rather than a daemon machine token.

Task creation and reads return the same stable shape:

```json
{
  "task_id": "uuid",
  "state": "assigned",
  "run_id": "uuid",
  "generation": 1,
  "work": {
    "goal": "Run tests",
    "agent_profile": "codex",
    "workspace": "primary",
    "input": {}
  },
  "result": null,
  "failure": null,
  "waiting": null,
  "latest_command": null
}
```

`waiting` is `null` unless the current run is in `waiting_for_input`. When it
is present, it identifies the current run and generation. Its `transition_id`
is the identity of the transition that put the current run into
`waiting_for_input`. Its `question`, `payload`, and `recorded_at` come from the
current run's highest-sequence `waiting_for_input` event. For older data with
no such event, those fields fall back to the waiting transition's payload and
recorded time:

```json
{
  "run_id": "uuid",
  "generation": 2,
  "transition_id": "uuid",
  "question": "Choose the target branch",
  "payload": {"question": "Choose the target branch"},
  "recorded_at": "2026-09-03T00:00:20Z"
}
```

`latest_command` is `null` when the task has no commands; otherwise it uses
the command resource shape documented below. It may be `pending`, `applied`,
or `acknowledged` independently of whether `waiting` is currently present.

Task commands require an `Idempotency-Key` header and accept exactly one of the
following request bodies:

```http
POST /api/v1/tasks/{task_id}/commands
Authorization: Bearer <operator-token>
Idempotency-Key: <client-generated-key>
Content-Type: application/json
```

```json
{"kind":"cancel"}
```

```json
{"kind":"provide_input","payload":{"answer":"continue"}}
```

`cancel` forbids `payload` and normalizes its stored payload to `{}`.
`provide_input` requires a non-null JSON object; `{}` is valid and remains
distinct from an omitted payload. A command response is a command resource,
not a task snapshot, and has these required fields. Clients must reject a
response that omits any required field, including a nullable field; unknown
fields are additive and clients must accept them.

```json
{
  "command_id": "uuid",
  "task_id": "uuid",
  "run_id": "uuid",
  "generation": 2,
  "kind": "provide_input",
  "payload": {"answer": "continue"},
  "state": "pending",
  "issued_at": "2026-09-03T00:00:20Z",
  "applied_at": null,
  "acknowledgement_id": null,
  "acknowledgement_outcome": null,
  "acknowledged_at": null
}
```

`run_id` and `generation` are nullable together. `applied_at`, `acknowledgement_id`,
`acknowledgement_outcome`, and `acknowledged_at` are nullable. Command state is
`pending`, `applied`, or `acknowledged`; acknowledgement outcome is `applied`,
`rejected`, or `failed`. A new command returns `201`; an exact replay of
`(task_id, Idempotency-Key, kind, payload)` returns the original command with
`200`; reusing a key with different normalized content returns
`409 idempotency_conflict`; a disallowed new command returns
`409 state_conflict`. Replay and idempotency-conflict resolution occur before
current-state validation.

Cancellation locks the task and current run when one exists. A queued
cancellation creates and applies a runless command in one transaction, and it
never enters daemon dispatch. An assigned-but-unclaimed cancellation binds the
existing `run_id` and `generation` and is immediately `applied`. These
control-applied cancellations are audited by their durable command resource and
directly materialize task/run state; they do not create synthetic transition
rows. They remain visible through command history and the unified timeline.
A claimed-run command remains `pending`, daemon-delivered, and fenced. Human
input is accepted only while the current run is `waiting_for_input`; its command
is atomically bound to the current run and generation, never a later generation.
Daemon-originated lifecycle writes use transition resources. The daemon
acknowledges and transitions the run back to `running`; both writes are
independently retryable.

### Operator Runtime And History Reads

All of the following resources require the operator bearer token. They expose
durable PostgreSQL state for inspection and do not affect scheduling, leases,
or daemon delivery:

```http
GET /api/v1/runtimes
GET /api/v1/runtimes/{runtime_id}
GET /api/v1/tasks/{task_id}/timeline?before={cursor}&limit={limit}
GET /api/v1/tasks/{task_id}/events?after={cursor}&limit={limit}
GET /api/v1/tasks/{task_id}/transitions?after={cursor}&limit={limit}
GET /api/v1/tasks/{task_id}/commands?after={cursor}&limit={limit}
```

The runtime collection returns `{"runtimes":[...]}`; the detail resource
returns one item with the same shape. Each runtime item identifies the machine
and runtime, and reports its durable liveness and reservations:

```json
{
  "machine_id": "uuid",
  "machine_name": "builder-01",
  "runtime_id": "uuid",
  "runtime_key": "default",
  "runtime_name": "Local Codex",
  "status": "online",
  "last_heartbeat_at": "2026-09-03T00:00:20Z",
  "connection_epoch": 3,
  "capacity": 2,
  "reserved_capacity": 1,
  "active_runs": [
    {
      "run_id": "uuid",
      "task_id": "uuid",
      "generation": 2,
      "state": "running",
      "recorded_at": "2026-09-03T00:00:10Z"
    }
  ]
}
```

`reserved_capacity` and `active_runs` include every capacity-bearing run:
`assigned`, `claimed`, `running`, `waiting_for_input`, and `cancelling`.
Unknown task and runtime IDs return `404 not_found`.

Separate task history collections are oldest-first. Their response collection
keys are respectively `events`, `transitions`, and `commands`; each response
also includes nullable `next_after`. An event item contains `run_id`,
`generation`, `event_id`, `sequence`, `kind`, `payload`, `occurred_at`, and
`recorded_at`. A transition item contains `run_id`, `generation`,
`transition_id`, `state`, `payload`, and `recorded_at`. A command item uses the
full command resource shape above.

The unified timeline is newest-first, including entries from every generation:

```json
{
  "items": [
    {
      "source": "transition",
      "run_id": "uuid",
      "generation": 2,
      "recorded_at": "2026-09-03T00:00:20Z",
      "data": {
        "transition_id": "uuid",
        "state": "waiting_for_input",
        "payload": {"question": "Choose the target branch"}
      }
    }
  ],
  "next_before": "opaque-cursor-or-null"
}
```

Timeline `source` is one of `event`, `transition`, or `command`. Its `data`
contains the source-specific fields from the corresponding history item, except
for the entry's repeated `run_id`, `generation`, and `recorded_at` fields.

History cursors are opaque `Phoenix.Token` values. A cursor is bound to version
1, the task ID, endpoint surface, direction, durable insertion timestamp, and
record ID; timeline cursors are also bound to their source. Clients must only
reuse a returned cursor. A cursor expires 24 hours after issuance. Collections
accept only `after`; timeline accepts only `before`. A cursor that is tampered
with, belongs to another task or surface, uses the wrong direction, has
expired, or has an unsupported version returns `400 invalid_request`.

`limit` defaults to `100` and accepts integers from `1` through `500`.
`0`, `501`, invalid values, and a cursor under the wrong direction parameter
return `400 invalid_request`. Unknown additive query parameters are ignored.

### Acknowledge A Command

```http
PUT /api/v1/commands/{command_id}/acknowledgements/{ack_id}
Authorization: Bearer <machine-token>
Content-Type: application/json
```

This is a machine-authenticated, fenced daemon mutation, not an operator Task
Control endpoint. The acknowledgement body carries `run_id`, the verified fence
fields `runtime_id`, `runtime_epoch`, `generation`, `claim_id`, and
`lease_token`, plus outcome (`applied`, `rejected`, or `failed`). The supplied
`run_id` must own the command. The caller supplies `command_id` and `ack_id` in
the resource path. The authenticated machine must own the runtime named by the
fence.
Repeating the same acknowledgement UUID and body is idempotent; reusing it with
a different body returns `409 idempotency_conflict`. Acknowledgement records
delivery outcome but never changes run state or removes a lifecycle command by
itself. A new acknowledgement follows the same eight-minute terminal grace and
current-generation fence as terminal delivery. Exact replay remains valid after
the deadline. Acknowledgement success records its command outcome, never
`accepted`; while draining a terminal journal, its response can instead record
the conclusive `ownership_lost` or `terminal_grace_expired` verdict.

## Agent Standard Input

The daemon writes exactly one initial stdin record when it launches an agent.
With `input_mode: "goal"`, the record is the plain-text task goal followed by a
newline; structured task input is not written. This mode is valid only for a
non-interactive profile. With `input_mode: "json"`, the record is one
newline-terminated JSON envelope:

```json
{"type":"task_input","goal":"Run the configured agent","input":{"branch":"main"}}
```

The envelope always has `type`, `goal`, and `input`. The `goal` is non-empty.
The `input` member preserves the task's structured input, using `null` when no
input was supplied, and otherwise is a JSON object. Interactive profiles use
`input_mode: "json"`, keep stdin open, and receive each `provide_input` command
as another newline-terminated envelope with the same goal:

```json
{"type":"provide_input","goal":"Run the configured agent","input":{"answer":"continue"}}
```

Follow-up `input` must be a JSON object; `{}` is valid. Non-interactive
profiles receive EOF after the first record. An `interactive: true` profile
with `input_mode: "goal"` is invalid daemon configuration.

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

Phoenix-generated fallback errors use this same envelope and do not expose
exception details:

- `404 not_found`: `resource was not found`.
- Fallback `4xx` errors, including `400` and `405`, use `invalid_request` with
  `request is invalid`.
- Phoenix fallback `5xx` errors use `internal_error` with `internal server error`.
  They follow the idempotent and unknown-outcome retry rules below.

- `400 invalid_request`: malformed or unsupported input; do not retry unchanged.
- `401 unauthenticated`: missing, invalid, or unknown credential.
- `403 forbidden`: valid credential has the wrong class or the authenticated
  machine does not own the resource.
- `404 not_found`: resource is absent; reconcile local state.
- `409 capacity_exhausted`: retry after the next snapshot.
- `409 idempotency_conflict`: the same key was reused with different input.
- `409 ownership_lost`: the run no longer owns the requested mutation because
  its static claim/generation fence is stale or a later task-level decision,
  such as queued cancellation, has settled authority. Stop ordinary execution;
  for a terminal journal, persist the conclusive verdict and proceed to cleanup.
- `409 terminal_grace_expired`: the static fence still matches, but a new
  terminal transition or command acknowledgement arrived more than eight
  minutes after lease expiry. Persist the conclusive verdict and proceed to
  cleanup; exact replay of an already stored request remains valid.
- `409 state_conflict`: state already advanced or a terminal payload conflicts.
- `410 assignment_expired`: discard the assignment and fetch a new snapshot.
- `422 invalid_transition`: daemon or client state-machine error.
- `429 rate_limited`: retry with backoff and `Retry-After` when present.
- `503 service_unavailable`: retry with backoff and `Retry-After` when present.

An HTTP timeout has an unknown outcome. Each control request has a 15-second
maximum. Enrollment, session registration, claim, and terminal-journal recovery
retry transport failures, `429`, and `5xx` under the daemon lifecycle, honoring
`Retry-After` when present; other `4xx` responses and malformed successful
responses stop that recovery attempt. Heartbeat, dispatch, reconciliation, and
ordinary non-terminal outbox delivery make one attempt per cadence or wakeup.
Writes are retried only when they carry the same claim, transition, command
acknowledgement, idempotency, or event identifier.
Enrollment retries, including retries after daemon restart, reuse the exact
persisted idempotency key, machine name, and machine token.
Claim retries never continue beyond `assignment_expires_at`; local expiry
releases the reserved slot and lets a later snapshot provide fresh work.

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
9. Terminal delivery never renews or revives ordinary lease authority. Its
   local slot is released at most once, no later than eight minutes after the
   process enters `terminal_pending`, while unresolved delivery state remains
   durable across daemon and control restarts.
