# Wire contract, context and operator API

Normative target; extends [v1](../protocol-v1.md). Preserve the existing fence:
`runtime_id + runtime_epoch + run_id + generation + claim_id + lease_token`.
No native harness sees machine bearer credentials or lease tokens.

## Contract source and evolution

Implementation creates `contracts/v1/` JSON Schema Draft 7 documents for
GoalRevision, ContextSnapshot, Admission, TaskResult, Evidence, Decision,
Usage and adapter capabilities. These are the single wire authority, separate
from Ecto persistence structs. IDs are UUID strings; timestamps RFC3339 UTC;
hashes lowercase `sha256:<64 hex>` on wire and 32-byte bytea in storage.
All monetary amounts are decimal-string microusd on wire. Counts and revisions
are JSON safe integers with schema maxima; reject overflow, never round.

Generate TS types with json-schema-to-typescript and Go DTOs with quicktype;
check generated files in with a reproducible pnpm generation command. They are
transport types, not behavior. Elixir validates envelopes with ExJsonSchema (Draft 7). Go validates received
wire data with santhosh-tekuri/jsonschema before converting generated DTOs to
internal types. Generation is not runtime validation. Use the common Draft 7
subset: definitions/$ref, object/array/scalar, required, enum/const, oneOf, bounds
and additionalProperties; no newer dialect keywords or remote schema fetching.
Effect 3 Schema decoders are explicit
boundary adapters, tested against the same positive/negative fixture corpus.
Do not maintain a separate handwritten TS DTO interface alongside generated types.

Freeze exact generator versions in lockfiles. CI regenerates and rejects drift.
Fixtures must cover missing/extra control fields, wrong identity, unknown enum,
nullable values, safe integer limits and version mismatch, not merely happy-path
serialization. Evolution is additive within v1; reject unknown command kinds or
unsupported required capabilities. Optional diagnostics can be ignored. Strict
command payloads set additionalProperties false. A breaking semantic change gets
a new schema_version and explicit adapter negotiation.

## Admission and native execution

Server admission envelope (inside existing work.input for opted-in runtimes):

```json
{
  "schema_version": "symmetry.admission.v1",
  "admission_id": "<uuid>",
  "goal_id": "<uuid>",
  "goal_revision": 1,
  "work_item_id": "<uuid>",
  "purpose": "implement",
  "context_snapshot_id": "<uuid>",
  "context_hash": "sha256:<digest>",
  "model_profile": "implementation-default",
  "session_mode": "fresh",
  "requested_session_id": null,
  "subject": {"resource_id": "<uuid>", "commit": "<git-object-id>"},
  "limits": {"max_turns": 1, "deadline_at": "<RFC3339>"}
}
```

Angle-bracket values illustrate types, not runnable fixture values. One turn
means one host invocation under an admitted contract, not one model API call.
Daemon resolves model_profile and credentials locally. `fresh|resume|handoff`
are distinct modes. A rejected resume returns a typed reason; a fresh fallback
requires a new admission against the same preserved artifact and fresh snapshot.
An in-flight Task is never silently switched to another model/harness.

Capabilities retain existing booleans for old clients. Add a versioned
`adapter` object containing kind, native_version, implementation_version,
protocol_version and `operations`:

```json
{
  "start": true,
  "events": true,
  "cancel": true,
  "resume": false,
  "guidance": "next_turn",
  "pause": "unsupported",
  "approval_response": false,
  "usage": "unknown",
  "hard_cost_limit": false
}
```

guidance enum `native_steer|next_turn|unsupported`; pause enum
`safe_boundary|unsupported`; usage enum `reported|estimated|unknown`.
Required operations are checked at admission and claim. Old clients cannot claim
new supervised work by merely declaring generic JSON input. Advertised native
operations must correspond to the exact version's integration tests.

## Normalized results and controls

Reuse run_events for transcript metadata and normalized native events. Event
kinds: `session_started`, `message_delta`, `tool_started`, `tool_finished`,
`approval_requested`, `usage_observed`, `task_result`, `diagnostic`.
The authenticated producer and run fence determine identity, not payload fields.
Unknown native events become diagnostics, never terminal success.

task_result has schema_version, result_id UUID, kind (core.md), summary,
subject_hash, evidence_refs UUID[], blocker nullable, proposed_next_action
nullable. Proposals are schema-checked data, not executable shell strings.
A producer cannot submit an accepted outcome directly. Successful native exit
without usable result is `failed` with reason `missing_result`, while preserving
the physical process-exit event. Run lifecycle and semantic result remain separate.

Existing commands retain idempotency and generation. Guidance has delivery
`queued|applied|failed`; writing bytes is not proof of application. A next-turn
delivery records the admission that consumed it. A safe pause acknowledgement
means the adapter observed no running native turn and retained resumable state.
Interruption/cancellation is not pause. If unsupported, return
`unsupported_capability`, do not fake an acknowledgement or use SIGSTOP.

## Goal command API

Portal routes below use existing session + CSRF authentication. CLI/operator
equivalents use `/api/v1/goals` and existing operator bearer auth, sharing the
same context command implementation; no duplicated business logic.

| Method and path under `/portal/api` | Payload |
| --- | --- |
| POST `/projects/:id/goals` | title, initial revision contract, mutation_id |
| GET `/goals/:id` | current projection including allowed_actions and blocker reasons |
| POST `/goals/:id/commands` | mutation_id, expected_version, expected_revision, kind, payload |
| GET `/goals/:id/events?after=N` | ordered compact goal events, next cursor |
| GET `/goals/:id/graph` | dependency nodes/edges and blocker explanation |
| GET `/goals/:id/contexts/:snapshot_id` | authorized compact context snapshot |
| GET `/attention` | goal/work/decision projections with cursor pagination |

Command kinds are `activate`, `pause`, `resume`, `cancel`, `amend`, `accept_plan`,
`request_decision`, `resolve_decision`, `admit_task`, `add_dependency`, `remove_dependency`, `achieve`.
Each payload has a separate discriminated schema, fixed in [typed structures](contracts.md). Expected version and revision
are mandatory after creation. Permission checks occur on every replay. Same
mutation_id/content returns the prior receipt before evaluating changed versions.

Responses: 201 creation; 200 accepted/replayed command receipt; 401 unauthenticated;
403 unauthorized; 404 unavailable resource; 409 stale version/idempotency conflict;
422 invalid contract, unsatisfied predicate or unsupported capability.
Error envelope extends existing error code/message/fields with optional
current_version, current_revision, allowed_actions and details. Never return
secrets/raw private evidence in errors.

Machine-only additions:
`PUT /api/v1/runs/:id/session` (fenced attach),
`POST /api/v1/runs/:id/evidence` (fenced idempotent batch),
`POST /api/v1/runs/:id/usage` (normal fence or limited late-accounting rule in data.md),
`GET /api/v1/runs/:id/context` (owning machine, current claimed fence).
No machine credential can call operator goal commands. Evidence IDs derive from
daemon journal identity and are persisted before transmission.

## Context assembly and handoff

Context payload order is stable: approved goal/authority -> work contract ->
repository subject -> current decisions -> validated evidence -> relevant failed
attempts -> advisory recall -> next action. Source entries contain resource ID,
source kind, source revision/content hash, observed_at, trust classification,
required boolean, and excerpt or pointer. Trusted policy and untrusted repository
text remain visibly distinct. Retrieved text cannot become instructions merely
because it is in the snapshot.

Required source revision mismatch blocks admission; changed approved policy
requires amendment, not automatic refresh. Optional stale recall is omitted or
explicitly marked stale. Exact commit, scope, acceptance and blocking decisions
must never be truncated. Optional material uses deterministic relevance ordering
and a configured byte budget. Token estimates are labeled estimates and use
adapter-specific tokenization only when available. If mandatory context exceeds
budget, block with `context_budget_exceeded` instead of silently losing authority.

Hash canonicalized schema-valid JSON with a documented normalization shared by
fixtures; arrays preserve order, object keys sort recursively, strings are UTF-8,
integers exact. Preserve existing versioned RequestHash behavior for old commands;
do not change it opportunistically to match snapshot hashing.

Cross-harness handoff creates a new native session from this snapshot and an
authorized reachable Git commit. Never transfer raw proprietary session formats.
Native session compression/KV cache remain harness-owned. Keep stable prompt
prefixes free of fresh timestamps; observations live in the variable suffix.

## Realtime delivery

Add authenticated operator Phoenix Channel topics for invalidation only. Browser
gets a short-lived audience-scoped socket token from the authenticated same-origin
bootstrap; no operator bearer token in JS/localStorage. Authorize topic joins.
Payload: resource type/id, latest version, event cursor; no raw transcript.
On notification, TanStack Query invalidates and fetches authoritative data.
Reconnect does a snapshot refetch and goal-event cursor catchup; polling remains
a fallback. Connection loss or missed messages cannot lose state. No second
browser event store or frontend transition engine.

Draft choice reference: [ExJsonSchema supported drafts](https://github.com/jonasschmidt/ex_json_schema). Draft 7 avoids a native extension solely for a newer dialect.
