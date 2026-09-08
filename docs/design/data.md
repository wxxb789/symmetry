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
| `work_items` | nullable `goal_id uuid`; `admitted_revision integer`; `required boolean DEFAULT true`; `acceptance_contract jsonb`; goal-less items retain current behavior |
| `tasks` | nullable `work_item_id uuid`, `goal_id uuid`, `goal_revision integer`, `context_snapshot_id uuid`; `purpose text DEFAULT 'implement'`; nullable `validation_of_task_id uuid`; `admission_key uuid`; `max_run_attempts integer` for goal tasks; nullable `requested_session_id uuid` |
| `runs` | nullable `harness_session_id uuid`; native result/evidence remain associated with the original execution fence |
| `runtimes` | `harness_kind text`, `harness_version text`, `adapter_version text`, `adapter_protocol_version integer`; explicit capabilities described in protocol.md |

For goal-owned WorkItems, goal_id/admitted_revision/acceptance_contract are all
non-null; enforce revision and same-project ownership via composite FKs. Goal-less
items leave these fields null.

Preserve `work_items.orchestration_task_id` as the current/latest task pointer for
existing APIs. `tasks.work_item_id` is durable membership for historical queries;
do not infer history from the current pointer. `tasks.goal_id` and revision are
captured at admission; do not recompute them from a mutable WorkItem later.

`UNIQUE(tasks.goal_id, admission_key)` for non-null goal_id. Add composite unique
keys and foreign keys to enforce `(work_item_id, goal_id)` membership and
`(goal_id, goal_revision)` revision existence. Goal task fields are all present
or all absent (context/goal/revision/admission/work item); goal-less chat remains
compatible. A validate task must reference a producing task of the same work
item and revision. Validate this under the goal lock and enforce using a
composite FK including work_item_id, goal_id, goal_revision.

One nonterminal goal Task per WorkItem: partial unique index on work_item_id
where goal_id IS NOT NULL and state IN
('queued','assigned','claimed','running','waiting_for_input','paused','cancelling').
Retain current generation/attempt identity and terminal-grace behavior. For goal
tasks, Orchestration enforces the admission-snapshotted max_run_attempts before
creating another Run (including automatic lease-expiry retry); exhaustion fails
the Task with attempt_limit. Goal-less tasks retain the existing retry policy.
Orchestration enforces this local Task field without calling Goals policy.

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
`budget_limit_microusd nullable bigint`, `budget_mode 'soft'|'strict'`,
`allowed_runtime_ids uuid[]`, `allowed_model_profiles string[]`,
`final_acceptance 'operator'|'deterministic'`, `allowed_actions string[]`,
`allowed_resource_ids uuid[]`. Default automatic_execution false, final_acceptance
operator, and no merge/publish action. No implicit unlimited auto budget.
The model profile resolves machine-local CLI/model config, not credentials.

### `work_dependencies`

`goal_id`, `work_item_id`, `depends_on_id`, inserted_at.
PK `(work_item_id,depends_on_id)`; CHECK unequal IDs; composite FKs ensure both
items belong to the same goal. Index `(depends_on_id,work_item_id)`.
Both add/remove serialize on goals and reject an active dependent Task.
Recursive CTE cycle test and insert happen in that transaction. No cross-goal
edges in v1. Read models derive graph layout and blocked reasons.

### `context_snapshots`

id, goal_id, goal_revision, work_item_id, schema_version integer,
content_hash bytea (32 bytes), payload jsonb, inserted_at.
Composite FK to revision; WorkItem ownership FK. Add composite unique keys on
referenced (id,goal_id) pairs before their FKs. Immutable. Unique
`(goal_id,goal_revision,work_item_id,content_hash)` permits safe reuse.
Payload includes exact source refs/revisions, approved task contract, repository
commit, relevant decisions, accepted evidence pointers, failed attempts,
next action, and context size accounting. See protocol.md for trust/order.
No raw transcripts, secrets or machine-local session filenames.

### `harness_sessions`

id, machine_id FK, runtime_id FK, harness_kind text, harness_version text,
adapter_version text, local_handle_id uuid, repository_resource_id FK,
workspace_fingerprint text, state text (`available|busy|unavailable|closed`),
active_run_id nullable FK, lock_version, timestamps.
Unique `(machine_id,local_handle_id)`; unique active_run_id when present.
CHECK busy iff active_run_id IS NOT NULL. Runtime ownership must match machine
using composite FK. Raw native IDs and filesystem locations live in the daemon
journal behind local_handle_id. Session claim is conditional on available and
the matching machine/workspace; atomic with Run attachment. A closed/unavailable
session does not prevent a fresh-session handoff.

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

## Transaction boundaries

Global lock order for new goal-aware operations: Goal -> WorkItems sorted UUID
-> Tasks sorted UUID -> Runs sorted UUID -> sessions -> decisions/reservations.
Existing Orchestration never acquires Goal after Task/Run: terminal completion
enqueues a settlement job and releases its transaction first. Legacy paths
operating on goal-managed items must delegate before acquiring their old locks.

- **Admit:** replay check; lock goal/item; check revision/eligibility; create or
  reuse context; insert reservation + Task + current pointer + goal event + Oban
  dispatch wakeup in one Repo transaction. No network in transaction.
- **Accept outcome:** replay; lock goal/items/tasks/runs; verify terminal subject,
  required evidence, revision and decision; insert outcome + goal event + next
  wakeup. Usage ingestion is independent and never conditional on acceptance.
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

Expose goal admission behind a disabled-by-default rollout flag until schema,
domain and adapter checks pass. Once enabled, direct legacy run/retry/move paths
for goal-managed items must use Goals admission; no bypass. Never delete or
rewrite archive goals or request-hash history. New migrations get new timestamps.

Application rollback first disables new admission; it must not roll back to an
old writer that ignores goal ownership while goal work remains active. Drain or
pause goal work and use a compatibility build that preserves fences. Destructive
down migrations are not a safe operational rollback. Backup/restore verification
and mixed goal-less/goal-managed scenarios are required implementation evidence.
