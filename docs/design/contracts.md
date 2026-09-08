# Typed policy and result structures

Normative companion to [wire protocol](protocol.md). This fixes the meaning of
JSONB fields before implementation; they are not open-ended bags of model output.
Implement the corresponding schemas under contracts/v1, not a second schema copy
under docs. All described objects reject unknown control fields and carry a v1
schema_version in their containing envelope.

## Acceptance contract

```typescript
type Subject = {
  resource_id: UUID;
  commit: string;                 // exact Git object ID, not branch name
  tree_digest: Sha256;            // canonical tracked-artifact manifest digest
};

type Predicate =
  | { id: string; kind: "check"; validator_profile: string }
  | { id: string; kind: "artifact"; resource_id: UUID; path: string }
  | { id: string; kind: "review"; reviewer_profile: string }
  | { id: string; kind: "operator_acceptance" };

type AcceptanceContract = {
  schema_version: "symmetry.acceptance.v1";
  description: string;
  predicates: Predicate[];
};
```

UUID and Sha256 are schema-defined wire strings, not runtime assertions. Predicate
IDs are unique within a contract; all predicates are required (AND). OR, arbitrary
expressions, embedded scripts and user-defined predicate plugins are excluded.
An empty contract cannot admit implementation or validate completion. Goal-level
contract may contain operator acceptance plus required work outcomes; deterministic
goal completion allows only explicitly machine-verifiable predicates.

Validator/reviewer profiles are operator-configured named profiles, distinct from
the worker's model profile. A check profile resolves an executable plus argument
array, supported platforms, timeout, env allowlist and output interpretation on
the daemon. Agent proposals name profiles; they cannot supply arbitrary shell
commands or edit the profile. A profile's digest is captured in context/evidence.
Review profiles resolve independent validation tasks; model review cannot replace
an operator_acceptance predicate. Required check-profile availability is checked
before admission. Built-in Git artifact predicates use the bound resource/commit.

Subject hash is SHA-256 of canonical Subject JSON. Validation checks out that
exact subject into an isolated validation workspace; the worker's dirty directory
is not accepted evidence. Builds may write temporary files there; they cannot
modify the proposed artifact that is subsequently published. A new candidate
commit is a new subject requiring relevant validation again.

Evidence payload maps `predicate_id` to verdict and concrete check/artifact/review
receipt. Check receipt contains profile digest, command argv digest, exit code,
subject_hash, start/finish times and bounded output reference. Artifact receipt
contains resource, commit, normalized relative path and content digest. No path
traversal or server-side arbitrary URL fetch. Review receipt contains subject,
review task ID, findings and verdict. Only an authenticated configured validator
can satisfy a validation predicate; an implementer narrative cannot.

Goal acceptance evaluates its predicates against an integration subject or
explicit operator decision, plus accepted required WorkItems. Individual commits
from divergent branches do not prove the combined result; an admitted integration
WorkItem supplies the combined pinned subject before final delivery.

## Plan proposal and admission

```typescript
type PlanProposal = {
  schema_version: "symmetry.plan.v1";
  proposal_id: UUID;
  goal_id: UUID;
  expected_revision: number;
  items: Array<{
    key: string;                  // unique within proposal
    title: string;
    description: string;
    required: boolean;
    repository_resource_id: UUID;
    acceptance: AcceptanceContract;
    depends_on_keys: string[];    // only this proposal's keys
    model_profile: string;
  }>;
};
```

Plan acceptance is an operator command binding proposal hash and goal revision.
Create all WorkItems/edges or none, under the goal lock. References must resolve,
resources/profiles must be allowed, and the graph must be acyclic. The server
assigns UUIDs and records the mapping in the replayable response. The model does
not invent server IDs. Adding a plan to an existing admitted graph first requires
a scoped plan decision; dependency edits follow the same authority rules.

Task admissions select one existing admitted WorkItem, purpose, model profile,
session mode, optional requested session and validation_of_task_id. The server
derives goal revision, resource, snapshot, subject and limits from current approved
state. A caller cannot override these derived fields in the operator API. The
server-to-daemon admission envelope in protocol.md contains the resolved values.

## Decisions, blockers and subsequent work

```typescript
type Blocker =
  | { kind: "decision"; decision_id: UUID }
  | { kind: "external"; resource_id: UUID; external_ref: string;
      next_check_at: string }
  | { kind: "dependency"; work_item_ids: UUID[] }
  | { kind: "environment"; code: string; detail: string };

type NextAction =
  | { kind: "validate"; producing_task_id: UUID }
  | { kind: "repair"; work_item_id: UUID; reason: string }
  | { kind: "observe"; resource_id: UUID; external_ref: string }
  | { kind: "replan"; reason: string }
  | { kind: "wait"; blocker: Blocker };
```

The kernel checks these against current state; a next action is a proposal. The
model cannot claim a dependency satisfied, approve a decision, choose a new
resource or extend its budget. For `progress`, a subsequent implementation task
for the same admitted item is allowed within limits; repeated progress without
new subject/evidence stops as `no_verified_progress` and requests repair/replan.
Observations do not count as delivery. No numeric progress percentage inferred
from token count, elapsed time or number of tool calls.

Decision options are `{id,label,consequence}` records; resolution names one
existing option and optional bounded comment. Action hash covers decision kind,
goal/revision, work item, options and intended effect; subject hash separately
binds artifact review. A resolved decision cannot be repurposed by editing its
question/options. An expired resolution requires a new decision/action identity.

## Goal command payloads

All commands carry the common identity/preconditions from protocol.md.

| Kind | Exact payload fields |
| --- | --- |
| activate | approved_revision |
| pause / resume / cancel | reason |
| amend | revision_contract, reason |
| accept_plan | proposal, proposal_hash, decision_id |
| resolve_decision | decision_id, expected_decision_version, option_id, comment |
| admit_task | work_item_id, purpose, model_profile, session_mode, requested_session_id nullable, validation_of_task_id nullable |
| add_dependency / remove_dependency | work_item_id, depends_on_id, decision_id |
| achieve | subject, evidence_ids, decision_id nullable |

Initial plan decision is created from the proposed plan as a server-validated
decision request; add `request_decision` with payload `kind, work_item_id nullable,
subject_hash nullable, proposal` to the command union. Only authorized operator
or a fenced goal task through its typed result may request it. Kernel derives
options/action hash, rather than letting an agent assign authority. For an
operator-authored initial plan, request_decision and resolution may be composed
by one explicitly authorized operator interaction, but both receipts are recorded.
Decisions for automatic retry/repair are unnecessary when already inside policy.

An admitted WorkItem pins acceptance and repository identity for the revision.
Editing these, changing requiredness, attaching/removing goal ownership or changing
its effective scope requires amendment and re-admission. Editing view position,
labels or display-only metadata does not. External provider description updates
remain external observations: surface drift, never overwrite approved acceptance
or silently execute changed instructions.

## Size, provenance and counters

Operational payload size limits live in server configuration and are exposed in
bootstrap; defaults must be documented and exercised at their boundary. The
model cannot raise them. Return 413 on transport-size overflow and 422 on a
schema bound violation. Large logs/artifacts are referenced, not embedded.

All v1 monetary amounts are decimal-string microusd on wire, bigint in storage.
All token/count/revision values are nonnegative JSON safe integers with explicit
schema maxima; reject overflow, never round. Native provider prices/usage are
reported with provenance; `estimated` is distinct from actual billing.
