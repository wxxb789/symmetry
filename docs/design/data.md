# Relational contract and migration

Normative target; PostgreSQL/Ecto, UUID primary keys, UTC microsecond timestamps.
`bytea(32)` below is shorthand for bytea plus CHECK octet_length(value)=32,
not PostgreSQL type-modifier syntax. All new mutable entities have `lock_version bigint NOT NULL DEFAULT 1`.
Enums use text plus named CHECK constraints and Ecto validation. Amounts and
counts use nonnegative bigint, never floating point. JSON below is typed,
versioned payload data; identity, ownership, state and dedup keys stay relational.

## Existing tables: additive changes

| Table | Additions and meaning |
| --- | --- |
| `work_items` | nullable `goal_id uuid`; `admitted_revision integer`; `required boolean DEFAULT true`; `integration boolean NOT NULL DEFAULT false`; `acceptance_contract jsonb`; nullable immutable `baseline_subject jsonb` or `baseline_dependency_id uuid`; nullable immutable `change_target jsonb`; goal-less items retain current behavior |
| `tasks` | nullable `work_item_id uuid`, `goal_id uuid`, `goal_revision integer`, `context_snapshot_id uuid`; `purpose text DEFAULT 'implement'`; nullable `validation_of_task_id uuid`; `admission_key uuid`; `max_run_attempts integer` for goal tasks; nullable `requested_session_id uuid`; nullable immutable `handoff_source_run_id uuid` |
| `runs` | nullable `harness_session_id uuid` paired with immutable `harness_binding_id uuid`; native result/evidence remain associated with the original execution fence |
| `runtimes` | `harness_kind text`, `harness_version text`, `adapter_version text`, `adapter_protocol_version integer`, nullable `repository_resource_id uuid`; explicit capabilities described in protocol.md |

For goal-owned WorkItems, goal_id/admitted_revision/acceptance_contract are all
non-null; enforce revision and same-project ownership via composite FKs. `integration`
may be true only for a goal-owned WorkItem and is immutable once admitted. It is an
explicit plan designation, never inferred from title, order, dependencies, or board
state. Goal-less items leave these fields null and `integration` false.

Every Goal WorkItem pins exactly one automatic baseline source: a complete
`baseline_subject` whose resource matches the WorkItem repository, or a
`baseline_dependency_id` referencing one explicit same-goal, same-revision
dependency. The fields are mutually exclusive and immutable after admission.
Automatic initial execution never infers a Subject from multiple dependencies,
provider metadata, a filesystem path or the current branch. A dependency baseline
becomes executable only when that exact dependency has a current-revision accepted
`work_outcomes` receipt; the receipt's candidate Subject is copied verbatim.

`change_target` is nullable only for a Goal WorkItem and has a strict object shape:
either exact `branches` source/target names or an exact `pull_request` URL. It is
set by the operator-approved plan, bound to the immutable repository resource, and
cannot change after admission. Goal-less WorkItems retain `NULL`; no migration
backfills this field from branch or pull-request presentation data. A non-null
target requires the bound repository's matching supported connection and its
`repositories` plus `changes` capabilities before admission can exist.

Preserve `work_items.orchestration_task_id` as the current/latest task pointer for
existing APIs. `tasks.work_item_id` is durable membership for historical queries;
do not infer history from the current pointer. `tasks.goal_id` and revision are
captured at admission; do not recompute them from a mutable WorkItem later.

`UNIQUE(tasks.goal_id, admission_key)` for non-null goal_id. Add composite unique
keys and foreign keys to enforce `(work_item_id, goal_id)` membership and
`(goal_id, goal_revision)` revision existence. Goal task fields are all present
or all absent except for operator-authorized `purpose=plan`: that branch has a
Goal, revision, context snapshot, admission key and retry limit, but a null
WorkItem and validation source. It is limited to one nonterminal Task per Goal.
Every other Goal Task requires a WorkItem. Goal-less chat remains compatible. A
validate task must reference a producing task of the same work item and revision.
Validate this under the goal lock and enforce using a composite FK including
work_item_id, goal_id, goal_revision.

A `session_mode=handoff` Task has a non-null `handoff_source_run_id` and the
same canonical UUID in its immutable admission input; fresh and resume Tasks
have neither. A partial unique index permits only one successor per source Run.
The source Run and source Task must be same Goal, revision and WorkItem, current
generation and terminal; their identity, result and runtime binding freeze once
consumed. Handoff creates a new Task, Run, context snapshot and native session,
never transfers the source native handle or retained session.

One nonterminal goal Task per WorkItem: partial unique index on work_item_id
where goal_id IS NOT NULL and state IN
('queued','assigned','claimed','running','waiting_for_input','paused','cancelling').
Retain current generation/attempt identity and terminal-grace behavior. For goal
tasks, Orchestration enforces the admission-snapshotted max_run_attempts before
creating another Run (including automatic lease-expiry retry); exhaustion fails
the Task with attempt_limit. Goal-less tasks retain the existing retry policy.
Orchestration enforces this local Task field without calling Goals policy.

### Additive amendment: native runtime repository affinity

A native runtime may register one nullable `repository_resource_id` foreign key.
It identifies the exact repository resource whose admitted Subjects that local
runtime can materialize; it is not a display label or a daemon-selected routing
hint. The staged nullable migration preserves existing runtime rows and their
goal-less behavior. A missing binding leaves a runtime registration valid but
makes it ineligible for Goal admission, assignment and new claims.

Scheduler selection and new-claim authority require the runtime binding,
WorkItem repository resource and admitted Subject resource to match exactly,
and require the resource to remain a repository in the Goal's project. Admission
does not require a currently online runtime: it can safely queue approved work
for a matching runtime that registers later. The daemon rechecks the same
binding before creating a workspace or native session. Exact lost-ack claim
replay returns its recorded receipt before re-evaluating mutable affinity; it
never grants a new claim. A resource bound by an active runtime cannot have its
identity rewritten or be deleted until the binding is removed.

## New tables

### `goals` / `goal_revisions`

`goals`: id, project_id FK, title text, state text, current_revision integer,
event_sequence bigint DEFAULT 0, next_wake_at timestamptz nullable,
lock_version, inserted_at, updated_at. Index `(state,next_wake_at)` for scanning.
Unique `(id,project_id)` enables same-project WorkItem ownership FK.

`goal_revisions`: goal_id FK, revision integer > 0 (composite PK), objective text,
non_goals jsonb string array, acceptance_contract jsonb, authority_policy jsonb,
execution_policy jsonb, context_manifest jsonb, reason text, actor_ref text,
inserted_at. Rows immutable. `goals(id,current_revision)` references the revision
DEFERRABLE INITIALLY DEFERRED so creation/revision append is atomic.

execution_policy v1 fields: `automatic_execution boolean`,
`max_parallel_tasks positive integer` (default 1),
`max_task_admissions positive integer` (explicit at activation),
`max_run_attempts_per_task positive integer` (default 2),
`budget_limit_microusd nullable bigint`, `per_run_cost_limit_microusd nullable bigint`,
`budget_mode 'soft'|'strict'`, `hard_cost_limit_required boolean`,
`allowed_runtime_ids uuid[]`, `allowed_model_profiles string[]`,
`final_acceptance 'operator'|'deterministic'`, `allowed_actions string[]`,
`allowed_resource_ids uuid[]`. Default automatic_execution false, final_acceptance
operator, and no merge/publish action. Automatic execution requires a non-null
total budget even in soft mode. Strict mode additionally requires a non-null
per-Run ceiling and verified native/provider enforcement; a Task reserves
`max_run_attempts_per_task * per_run_cost_limit_microusd` before admission.
No implicit unlimited auto budget.
The model profile resolves machine-local CLI/model config, not credentials.

### `work_dependencies`

`goal_id`, `work_item_id`, `depends_on_id`, inserted_at.
PK `(work_item_id,depends_on_id)`; CHECK unequal IDs; composite FKs ensure both
items belong to the same goal. Index `(depends_on_id,work_item_id)`.
Both add/remove serialize on goals and reject an active dependent Task.
Recursive CTE cycle test and insert happen in that transaction. No cross-goal
edges in v1. Read models derive graph layout and blocked reasons.

### `context_snapshots`

id, goal_id, goal_revision, work_item_id nullable, schema_version integer,
content_hash bytea (32 bytes), payload jsonb, inserted_at.
Composite FK to revision; WorkItem ownership FK for non-plan snapshots. Add
composite unique keys on referenced (id,goal_id) pairs before their FKs.
Immutable. A `purpose=plan` snapshot has `work_item_id NULL`; all other Goal
snapshots require a matching WorkItem. Unique
`(goal_id,goal_revision,work_item_id,content_hash)` permits safe reuse.
Payload includes exact source refs/revisions, approved task contract, repository
commit, relevant decisions, accepted evidence pointers, failed attempts,
next action, and context size accounting. See protocol.md for trust/order.
No raw transcripts, secrets or machine-local session filenames.

### `harness_sessions`

id, machine_id FK, runtime_id FK, harness_kind text, harness_version text,
adapter_version text, local_handle_id uuid, repository_resource_id FK,
workspace_fingerprint text, attachment `binding_id uuid`, immutable
`binding_verified boolean`, state text (`available|busy|unavailable|closed`),
active_run_id nullable FK, lock_version, timestamps.
Unique `(machine_id,local_handle_id)`; unique active_run_id when present.
CHECK busy iff active_run_id IS NOT NULL. Runtime ownership must match machine
using composite FK. Raw native IDs and filesystem locations live in the daemon
journal behind local_handle_id. A verified session claim is conditional on
available and matching machine/workspace; reservation atomically rotates
binding_id and records the same immutable value in the Run before dispatch.
For `resume`, the daemon can only attach by echoing that scheduler-reserved
pair. For `fresh` and `handoff`, there is no pre-existing pair: the first
fenced attach creates both session and server-generated binding atomically,
then freezes the pair on the Run. Those modes reject an existing local handle
unless the original Run's immutable attach receipt is being replayed.
`binding_verified` is true only for a session created through the fenced
attachment protocol; migrated legacy sessions remain false and may finish
cleanup but cannot resume or release. A closed/unavailable session does not
prevent a fresh-session handoff.

### `harness_session_attach_receipts`

id, session_id FK, unique run_id FK, runtime_id FK, machine_id, original
runtime epoch/generation/claim ID/lease token, request_hash bytea(32), response
jsonb, inserted_at. Rows are append-only. The insert transaction verifies the
Run/session/binding pair while the session is busy, and snapshots the initial
attachment response. The response has historical `session` field compatibility
but is an immutable attachment receipt, not a view of the mutable
`harness_sessions` row. An owning machine may read it with the exact original
fence even after the Run is terminal; a later reservation cannot change or
rebind that history. Missing pre-receipt legacy history is not reconstructed.

### `harness_session_stop_receipts`

id, session_id FK, run_id FK, machine_id, binding_id uuid, request_hash
bytea(32), response jsonb, inserted_at. Unique `(session_id,binding_id)`.
Rows are append-only and require the exact terminal Run, machine, verified
session, local handle, binding and prior `unavailable` state. The transaction
inserts the receipt and changes that exact session to available together.
Exact replay returns its stored response; a different request for the same
binding conflicts. A stale binding cannot release a later reservation.

### `run_evidence`

id, run_id FK, evidence_key text, kind text (`check|artifact|review|observation`),
subject_hash bytea(32), source_ref jsonb, source_revision text,
validator_profile text nullable, verdict text (`passed|failed|unknown|not_applicable`),
payload jsonb, inserted_at. Unique `(run_id,evidence_key)`; index subject_hash.
Immutable; same key/different content is conflict. Source refs are typed resource
IDs plus commit/artifact/check IDs, not an unrestricted server-fetch URL.
Caller cannot self-label evidence as trusted: server derives origin/validator
identity from authenticated run and allowed validation profile.

### `work_outcomes`

id, goal_id, goal_revision, work_item_id, producing_task_id FK,
producing_run_id FK, validation_task_id nullable FK, subject_hash bytea(32),
evidence_ids jsonb UUID array, disposition text (`accepted|rejected`),
reason text, decision_id nullable FK, inserted_at.
Immutable. Partial UNIQUE `(work_item_id,goal_revision)` WHERE disposition='accepted'.
All referenced tasks/runs/evidence must match ownership and subject. Relational
FKs cover task/run identities; evidence array membership, predicates and origin
are checked within settlement under lock. No mutable "passed" column substitutes
for this receipt. UI `done` for a goal item derives from accepted outcome; a
manual board move cannot create it. Prior-revision outcomes remain history.

### `goal_decisions`

id, goal_id, goal_revision, work_item_id nullable FK, kind text
(`plan|scope|review|budget|external_action|completion`), action_hash bytea(32),
subject_hash nullable bytea(32), state text (`open|resolved|superseded`),
question text, options jsonb, resolution jsonb nullable, actor_ref text nullable,
expires_at nullable timestamptz, lock_version, timestamps.
Unique `(goal_id,goal_revision,action_hash)`. Resolution is immutable once set;
expiration is evaluated by the command, not inferred from UI time.
Amendment supersedes open old-revision decisions atomically. Operator identity
uses the existing authenticated operator class; do not invent multi-user RBAC.

### `goal_events`

id UUID (client mutation id), goal_id FK, sequence bigint, kind text,
actor_ref text, request_hash bytea(32), request_hash_version integer,
revision integer, payload jsonb, response jsonb, inserted_at.
Unique `(goal_id,sequence)`; id is globally unique. Immutable compact audit plus
idempotency receipt, not a duplicate transcript or full event-sourcing system.
Write commands and internal settlements each use a stable mutation id. On replay,
authorize access then compare request hash before checking mutable preconditions.
Same identity/content returns stored response; different content returns 409.
Never include current timestamps or regenerated random fields in retry identity.
Automatic admission failures use the same immutable event table. A candidate
that violates revision-stable policy/context records `automatic_admission_blocked`
with its source identity and no admission rows. A condition that may change
outside the Goal records `automatic_reconciliation_deferred` with a normalized
reason and bounded next wake; its mutation identity includes that reason so a
changed diagnosis cannot replay stale details. Read models omit prior-revision,
terminal, accepted, or subsequently replaced blockers.

### `goal_budget_reservations` / `run_usage`

Reservations: id, goal_id, goal_revision, task_id UNIQUE FK,
admission_key uuid, reserved_microusd nullable bigint, state text
(`held|settled|released|unknown`), lock_version, timestamps.
Unique `(goal_id,admission_key)`. All admissions, including failures, consume the
revision's max_task_admissions; releasing money does not erase task history.

Usage: id, run_id FK, usage_key text, provider text, model text,
input_tokens nullable bigint, output_tokens nullable bigint,
cached_input_tokens nullable bigint, cost_microusd nullable bigint,
cost_basis text (`reported|estimated|unknown`), price_version nullable text,
supersedes_id nullable FK, inserted_at. Unique `(run_id,usage_key)` and unique
supersedes_id when present. Immutable correction chain; latest leaves count.
Normalize native cumulative counters to one final record or explicit replacements,
never sum cumulative snapshots. Unknown values are NULL, not zero.

Admission checks under the goal lock: counted admissions < revision limit,
parallel tasks < cap, and actual effective usage plus remaining held/unknown
reservations plus proposed reservation <= money cap when configured. For a held
task with observed usage, charge max(observed cost, reservation) to avoid double
counting; settled tasks use actual cost. Unknown money blocks strict admission.
Accounting spans all revisions; increasing a revision never resets spend or
existing liabilities. Limits describe total goal spend/admissions. Late usage
is always recorded and may block subsequent admission. Do not falsify history
to fit a cap or describe an admission limit as an upstream billing guarantee.

### `goal_external_waits`

An external blocker creates one immutable wait source: Goal/revision/WorkItem,
Task/Run/generation, complete result and Subject, resource ID and external ref.
It also pins a strict `check_spec` derived by the kernel from an approved
WorkItem contract, bound resource and supported checker configuration; a model
may not widen the selector or choose a more privileged check. Only `state`,
`next_check_at`, `check_seq` and the compact reconciliation receipt change. The
active unique key is one wait per current Goal WorkItem; source identity is
unique for replay.

A due wake first invokes the matching Integration checker, outside database
locks. Its immutable receipt binds the wait identity, exact Subject, provider
object/version or content digest, and one of `pending`, `satisfied`,
`ready_for_interpretation`, `failed` or `unknown`. `pending` and `unknown`
advance a bounded schedule without admitting a model Task; `satisfied`
terminalizes the wait then wakes ordinary continuation. Only a new
`ready_for_interpretation` evidence digest may admit one bounded `observe` Task,
whose semantic admission identity is `(wait_id, evidence_digest)`. Replayed
polling or an observation without new evidence never consumes a task admission
or budget reservation. Unsupported selectors remain visibly unsupported rather
than falling back to a model turn. The wait ledger is not a duplicate provider
mutation ledger: unknown external mutations remain in `ProviderActionIntent`
readback.

Current adapters expose no server-owned Integration checker that can issue this
exact receipt. Therefore every model-authored external blocker is retained as an
immutable `unsupported` wait with reason `unsupported_external_check`, no
`next_check_at`, and no model `observe` admission. Its source Task/Run/result
identities, Subject, resource and external reference remain available in the Goal
projection and compact audit identity. This is an explicit interim boundary, not a claim that polling or
manual observation can safely substitute for Integration readback.

## Transaction boundaries

Global lock order for new goal-aware operations that grant authority: Project
-> Goal -> WorkItems sorted UUID -> Tasks sorted UUID -> Runs sorted UUID ->
sessions -> decisions/reservations. The Project is locked `FOR SHARE`, which
serializes with archival's `FOR UPDATE`; exact receipt replay is read-only and
may precede this lock. Late usage, evidence and settlement retain their original
fence path but cannot authorize fresh work in an archived Project.
Existing Orchestration never acquires Goal after Task/Run: terminal completion
enqueues a settlement job and releases its transaction first. Legacy paths
operating on goal-managed items must delegate before acquiring their old locks.

- **Admit:** replay check; lock goal/item; check revision/eligibility; create or
  reuse context; insert reservation + Task + current pointer + goal event + Oban
  dispatch wakeup in one Repo transaction. No network in transaction.
- **Settle validation outcome:** replay the terminal validation Task receipt; lock
  goal/items/tasks/runs; derive accepted or rejected work only from its frozen
  candidate Subject and complete persisted required evidence, then insert the
  immutable outcome + goal event + next wakeup. There is no caller-selected
  acceptance payload. Usage ingestion is independent and never conditional on
  acceptance.
- **Achieve:** replay; lock Goal; verify current revision, required outcomes,
  goal predicates and decisions; reject if any goal Task is in the nonterminal
  set used by the partial index, including optional WorkItem tasks. Commit
  achieved state and goal event atomically. Concurrent admission uses the same
  Goal lock and rechecks active state; it cannot insert after achievement.
- **Resolve decision:** replay; lock scope; verify expected version/action hash;
  resolve + event + wakeup together. Double resolution differs -> conflict.
- **Amend:** replay; lock goal/items; append revision, pause goal, supersede open
  decisions, record cancellation/pause intents for active tasks + event + jobs.
- **Usage:** replay on run/key; validate machine ownership and original run fence
  identity, append usage/correction, reconcile reservation under Goal-first order.
  Late accounting does not reopen a terminal Run or authorize work. Accept late
  journal replay only from the original owning machine with the persisted full
  claimed identity; changed bytes for an existing usage key conflict.

DB transactions do not cover provider effects. Reuse existing provider action
intents and unknown/readback semantics; no second effects table. A result arriving
after its authorization was revoked is evidence only. Stop admitting new provider
actions on stale goal revision; an already-dispatched effect requires readback.

## Migration and rollback

Add nullable columns/tables first; no reinterpretation of legacy task.goal text
as a durable Goal. Backfill tasks.work_item_id from unambiguous current pointers;
leave unprovable historical membership NULL and label legacy history accordingly.
Introduce Goal ownership constraints and indexes after validating existing rows.
Existing goal-less tasks keep v1 routes, semantics and tests.

The baseline-source migration is additive and leaves existing Goal WorkItems with
both baseline fields NULL. Its destructive down refuses while any baseline source
exists, because removing an approved Subject or dependency identity would erase
automatic-execution authority.

Expose goal admission behind a disabled-by-default rollout flag until schema,
domain and adapter checks pass. Once enabled, direct legacy run/retry/move paths
for goal-managed items must use Goals admission; no bypass. Never delete or
rewrite archive goals or request-hash history. New migrations get new timestamps.

Application rollback first disables new admission; it must not roll back to an
old writer that ignores goal ownership while goal work remains active. Drain or
pause goal work and use a compatibility build that preserves fences. Destructive
down migrations are not a safe operational rollback. Backup/restore verification
and mixed goal-less/goal-managed scenarios are required implementation evidence.
