# Core and loop ownership

Normative target; see [decision index](README.md). One modular monolith, one
execution daemon, one durable authority. No generic workflow engine.

## Domain boundaries

| Context/package | Owns | Does not own |
| --- | --- | --- |
| `SymmetryControl.Goals` | Goal revisions, admission, dependency eligibility, decisions, accepted outcomes, budget reservations, goal audit | Model reasoning |
| `SymmetryControl.Orchestration` | Existing Task/Run scheduling, leases, execution events and commands | Whether a goal is achieved |
| `SymmetryControl.Workspaces` | Projects, WorkItems, resource bindings, provider-owned presentation fields | A second execution lifecycle |
| `SymmetryControl.Integrations` | Existing provider broker and external readback | Goal authorization |
| `SymmetryControl.Chat` | Conversation records, typed intent routing | Unvalidated authority |
| `daemon/internal/harness` | Native protocol translation | Shared scheduling |
| `daemon/internal/{execution,state,platform}` | Process trees, journal, credentials, local session handles and workspace retention | Goal truth |

Goals may call the public orchestration submission/read APIs; orchestration does
not depend on goal policy. An application-level completion job reads a terminal
run and calls Goals. It is enqueued durably in the transition transaction;
the transition code knows the job envelope, not goal rules. Workspaces commands
for goal-managed items delegate admission/completion to Goals; imports may
update provider-owned fields but cannot assert accepted work.

Extract orchestration read models and command/lease internals only along these
ownership boundaries, retaining the public facade and characterization tests.
Do not add a generic repository layer over Ecto or a bus over function calls.

## Identity and lifetime

`Project -> Goal -> WorkItem -> Task -> Run` are containment/ownership relations.
A project has many goals; a WorkItem belongs to at most one goal. A Task is one
bounded execution request; infrastructure retries retain the Task and create a
new Run/generation as today. A new semantic action (repair, validation, planning)
creates a new Task. A Run never spans native sessions that execute concurrently.

A Goal is stable while revisions of its objective/authority change. A native
session handle can serve successive tasks only with exclusive daemon ownership.
Context snapshots and evidence are immutable. An outcome is accepted for a
specific WorkItem and goal revision, not for an entire mutable conversation.

## Goal state machine

Stored states: `draft`, `active`, `paused`, `achieved`, `cancelled`.
Derived blockers: `waiting_decision`, `waiting_dependency`, `waiting_external`,
`budget_blocked`, `validation_failed`, `stale_context`, `runtime_unavailable`.
These are read-model reasons, not additional lifecycle states.

| Command | Allowed source | Result |
| --- | --- | --- |
| activate | draft | active, with an approved revision and admitted work |
| pause | active | paused; no new admission; request safe pause of active runs |
| resume | paused | active after checking current revision, decisions, budget |
| amend | draft/active/paused | append revision; active becomes paused |
| achieve | active/paused | achieved only through the completion rule below |
| cancel | draft/active/paused | cancelled; enqueue cancellation of active runs |

Achieved/cancelled goal lifecycle and accepted contracts are immutable (late usage
and audit append remain permitted); follow-up scope is a new goal referencing
the prior goal in its context, not a silent reopen. Amendment preserves evidence
and history but invalidates old pending approvals/admissions. It blocks provider
broker actions authorized by the old revision and requests active run pause
(cancel if no safe pause is supported). Existing local tool calls may finish;
the UI must show that limitation. Old results are retained but not accepted
against the new revision. Re-admission is explicit and revalidates retained work.

Goal pause means **scheduling paused**, not necessarily process suspended.
Display per-run requested/applied control status. Native tool permissions remain
enforced by the harness; goal metadata is not an OS sandbox.

## Bounded loop

1. A goal event or due timestamp enqueues an Oban wakeup. The job performs a
   short reconciliation, not an hours-long agent run. Jobs are hints; a periodic
   scan of active goals with due `next_wake_at` repairs missed wakeups.
2. Goals checks revision, current execution, dependency acceptance, authority,
   context freshness, runtime capabilities and budget under a goal row lock.
3. It records a context snapshot, reserves budget and submits a Task atomically.
   Existing Orchestration selects/claims a runtime. No second dispatcher.
4. Daemon executes one bounded native turn and reports typed evidence/result.
   A clean process exit closes the Run only; it does not close the work.
5. A validator Task or authoritative external check evaluates the exact proposed
   artifact. A validator cannot be the producing Task. Test requirements are
   fixed by the work contract, not generated solely by its implementer.
6. Goals settles an accepted outcome, decision/wait, repair, or replan result.
   State, audit and next wakeup commit together. Retries read the same receipt.

Task `purpose`: `implement | validate | plan | observe | chat`.
Result `kind`: `progress | candidate_completion | blocked | repair_required |
replan_required | failed`. `candidate_completion` is never self-acceptance.

Default scheduling is serial within a goal (`max_parallel_tasks = 1`). The owner
can raise the revision's explicit cap. Distinct eligible WorkItems can then run
concurrently in isolated worktrees. Only one nonterminal Task per WorkItem is
admitted at a time, including validation. Same-session execution is exclusive.
Retries have an explicit revision-bound attempt budget; semantic repair starts
a new task and counts against it. There is no infinite continue-on-exit policy.

## Planning and useful stopping

Planning is a normal harness task that returns bounded WorkItem proposals with
acceptance predicates, dependencies, resource IDs and intended purpose. A plan
does not write goal tables. In v1, the operator accepts the proposed plan as a
revision admission. Automatic execution/repair may operate only on admitted
items and the revision's allowed resources/actions. New scope or dependencies
requires a new plan decision; no automatic free-form backlog expansion.

Dependencies only link WorkItems in the same goal. Cycle checks serialize on the
goal row. An edge is satisfied by an accepted outcome for the current revision,
not the board column. Retries are execution history, not graph cycles. For v1,
one repository per WorkItem and no concurrent merge; integration is an explicit
admitted item on a pinned combined commit with its own checks.

`observe` tasks exist only when an agent must interpret evidence. Ordinary CI/PR
polling uses integrations without an LLM. Waiting records persist an external
reference and next check time. Absence of progress produces a visible reason,
bounded repair or decision; do not spend a model turn to say "still waiting".

## Completion and governance

Each WorkItem contract enumerates required checks and a review policy. The
accepted outcome references an immutable subject hash and evidence for every
required predicate. Deterministic check execution is recorded separately from
agent narration. Subject mismatch or missing evidence prevents acceptance.

Goal completion requires all admitted required WorkItems accepted under the
current revision, goal-level predicates satisfied, no unresolved blocking
decision, and no running/queued goal Task. Default final acceptance is an
operator decision. Automatic final acceptance is allowed only when the approved
revision explicitly opts in and all final predicates are machine-verifiable;
an LLM score alone is not such a predicate. Publication/merge authority is
explicitly separate and defaults to disallowed.

A Decision is scoped to one goal revision and an immutable action hash; artifact
review also binds the subject hash. It never means "approve everything later".
Existing authorization is reused within that exact scope. Chat status questions
read projections without steering agents. Optional natural-language chat uses
a `chat` Task; proposed control intents go through the same server checks as
buttons. No model client or prompt interpreter goes inside the kernel.

## Cost and retention

Admission reserves execution slots and optional estimated money. Actual usage
is recorded even on failure/cancellation/rejected progress. Failed delivery is
not free. Unknown usage stays unknown; unresolved execution holds its reservation.
Strict monetary caps require a verified adapter/provider-enforced turn ceiling;
otherwise show the limit as a soft admission cap and never promise zero overshoot.

Do not delete a worktree/session holding the only unaccepted artifact, active
decision or pending outbox. Accepted artifacts must be retained in Git or a
configured durable artifact location before cleanup. Control stores digest and
pointer, not arbitrary raw transcripts. Default artifact transport is Git commit
plus repository resource; cross-machine work requires a reachable authorized
commit. Never silently upload local-only evidence or credentials.
