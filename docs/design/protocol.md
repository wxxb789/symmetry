# Wire contract, context and operator API

Normative target; extends [v1](../protocol-v1.md). Preserve the existing fence:
`runtime_id + runtime_epoch + run_id + generation + claim_id + lease_token`.
No native harness sees machine bearer credentials or lease tokens.

## Contract source and evolution

Implementation creates `contracts/v1/` JSON Schema Draft 7 documents for
`GoalCreate` (`goal-create.schema.json`), `GoalCommand`
(`goal-command.schema.json`), `PlanProposal` (`plan-proposal.schema.json`),
`GoalRevision`, `ContextSnapshot`, `Admission`, `TaskResult`, `Evidence`,
`Decision`, `Usage` and adapter capabilities. These are the single wire
authority, separate from Ecto persistence structs. IDs are UUID strings;
timestamps RFC3339 UTC;
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
  "subject": {
    "resource_id": "<uuid>",
    "commit": "<git-object-id>",
    "tree_digest": "sha256:<digest>"
  },
  "limits": {
    "max_turns": 1,
    "deadline_at": "<RFC3339>",
    "max_cost_microusd": null
  },
  "validation_of_task_id": null,
  "provider_scope": null
}
```

Admission carries the complete shared `Subject` from [typed structures](contracts.md):
resource_id, commit and tree_digest. The server, daemon and validator compute its
subject hash from the same canonical complete object; none may omit the digest
or substitute an implicit local value. A validation admission names the exact
candidate subject being checked. A newly produced candidate commit has its own
complete Subject and hash, not the implementation admission's starting-subject
hash. `limits.max_cost_microusd` is required on every admission. `null` is the
explicit soft-budget value; it does not grant caller-selected or unlimited
spend. A non-null decimal-string ceiling is server-derived and is emitted only
for a strict revision whose selected adapter has verified native cost-ceiling
enforcement. `validation_of_task_id` is also a required nullable key: it is
non-null only for a validation admission and binds that admission to the exact
producer Task. `provider_scope` is a required nullable key; `null` means that
the approved WorkItem has no provider-effect scope, never an implicit broad
scope. When non-null, it is a server-derived frozen map of resource IDs,
per-resource operations and an explicit change target; it contains neither
credentials nor connection grants. Contract fixtures must cover complete-subject
round trips and rejection of missing or mismatched tree_digest.

For a PlanItem, `change_target` is required and nullable. The accepted plan hash
binds its exact value before the server persists it with the WorkItem. At admission,
`null` derives only `resource.sync`; branches derive `change.upsert` and optionally
`change.update` when policy permits; a pull-request target derives only
`change.update`. The server never derives a target from mutable WorkItem branch or
pull-request URL fields. A provider scope is emitted only after the bound repository
has a matching supported connection with the capabilities required by its operations.
This mapping applies only to `implement` admissions. `validate`, `plan`, `observe`,
and `chat` admissions always receive `provider_scope: null`.

An operator `request_plan` admission is the only Goal admission with
`work_item_id: null`. It is `purpose: "plan"`, runs only while the Goal remains
draft, has `validation_of_task_id: null`, and uses an immutable planning context
for the operator-selected approved Subject. Its TaskResult must be
`plan_proposed` with a schema-valid PlanProposal. The kernel persists that
proposal as a scoped open plan Decision; it does not create WorkItems, outcomes,
dependencies, provider actions or an implicit approval.

Angle-bracket values illustrate types, not runnable fixture values. One turn
means one host invocation under an admitted contract, not one model API call.
Daemon resolves model_profile and credentials locally. `fresh|resume|handoff`
are distinct modes. `fresh` requires `requested_session_id: null`; `resume`
requires the exact retained `requested_session_id`; and `handoff` always has
`requested_session_id: null` and an exact `handoff_source_run_id` because it
creates a new native session from the immutable context snapshot and reachable
authorized artifact, never by
transferring a native handle or proprietary session format. A rejected resume
returns `resume_rejected`. A daemon without a verified cross-harness handoff
adapter rejects handoff with `handoff_unsupported` before creating any local
session journal or native process. The source is a same-Goal, same-revision,
current-generation settled producer Run and can produce at most one handoff
Task; it is immutable once consumed. Planning and external-observation Tasks
do not hand off. A fresh fallback requires a new admission against the same
preserved artifact and fresh snapshot. An in-flight Task is never silently
switched to another model/harness.

Capabilities use a versioned `adapter` object containing kind, native_version,
implementation_version,
protocol_version and `operations`:

```json
{
  "start": true,
  "events": true,
  "cancel": true,
  "resume": false,
  "handoff": false,
  "guidance": "next_turn",
  "pause": "unsupported",
  "approval_response": false,
  "usage": "unknown",
  "hard_cost_limit": false
}
```

guidance enum `native_steer|next_turn|unsupported`; pause enum
`safe_boundary|unsupported`; usage enum `reported|estimated|unknown`.
Required operations are checked at admission and claim. `handoff` is independent
of `resume` and requires verified start, events and cancel behavior for the
exact native version. Clients cannot claim new supervised work by merely
declaring generic JSON input. Advertised native operations must correspond to
the exact version's integration tests.

### Additive native runtime repository binding

Native runtime registration may include nullable `repository_resource_id`.
This is a durable repository identity, not an arbitrary daemon workspace path;
the local path remains machine-local. Existing registrations that omit it remain
wire-compatible and can continue goal-less work, but cannot receive a Goal
assignment. Scheduler selection and a new claim require it to match the WorkItem
repository resource and admitted Subject resource; the daemon rechecks before
workspace or native-process effects. Admission may queue work before a matching
runtime is online or registered. An exact replay of an already persisted claim
remains valid after a later resource binding change, but no new work is
authorized by that replay.

## Normalized results and controls

Reuse run_events for transcript metadata and normalized native events. Event
kinds: `session_started`, `message_delta`, `tool_started`, `tool_finished`,
`approval_requested`, `usage_observed`, `task_result`, `diagnostic`.
The authenticated producer and run fence determine identity, not payload fields.
Unknown native events become diagnostics, never terminal success.

An external blocker in a normalized TaskResult currently produces the explicit
`unsupported_external_check` outcome. The Control plane retains the immutable
external-wait source and exposes it to operators, but emits no scheduled check
and never sends an `observe` admission to a model. A future Integration checker
must return the exact wait/Subject-bound receipt described in `data.md`; model
output, an operator retry, or a fresh timestamp cannot stand in for that receipt.

task_result has schema_version, result_id UUID, kind (core.md), summary, a
complete Subject, and subject_hash equal to SHA-256 of that canonical Subject,
plus evidence_refs UUID[], blocker nullable, proposed_next_action nullable.
For candidate_completion, Subject identifies the newly produced candidate, not
the implementation admission's starting Subject. Proposals are schema-checked
data, not executable shell strings.
A producer cannot submit an accepted outcome directly. Successful native exit
without usable result is `failed` with reason `missing_result`, while preserving
the physical process-exit event. Run lifecycle and semantic result remain separate.
`plan_proposed` is accepted only for an admitted planning Task and must carry a
schema-valid PlanProposal; all other result kinds must carry `proposal: null`.

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
| POST `/projects/:id/goals` | `symmetry.goal_create.v1` |
| GET `/goals/:id` | current projection including allowed_actions and blocker reasons |
| POST `/goals/:id/commands` | `symmetry.goal_command.v1` discriminated command envelope |
| GET `/goals/:id/events?after=N` | ordered compact goal events, next cursor |
| GET `/goals/:id/graph` | dependency nodes/edges and blocker explanation |
| GET `/goals/:id/contexts/:snapshot_id` | authorized compact context snapshot |
| GET `/attention` | goal/work/decision projections with cursor pagination |

Command kinds are `activate`, `pause`, `resume`, `cancel`, `amend`, `request_plan`, `accept_plan`,
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
fixtures: arrays preserve order, object keys sort recursively, strings are UTF-8
without Unicode normalization, and `U+2028`/`U+2029` remain raw UTF-8 rather than
serializer-specific escapes. Integers remain exact and numeric negative zero
canonicalizes to zero. Preserve existing versioned RequestHash behavior for old
commands; do not change it opportunistically to match snapshot hashing.

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
