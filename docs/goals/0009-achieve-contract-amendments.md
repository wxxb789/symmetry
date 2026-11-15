# 0009 - Achieve-contract amendments A, B and C

Land the owner-approved amendments to the achieve/integration contract and to
the Subject computation, as one independently reviewable increment, and record
their evidence. This is a contract amendment, so the change itself is the
outcome: the amended contract, the wire schema, the code that enforces it, and
the review record that shows the amended behavior and preserved refusals.

Prerequisite: none. It does not depend on 0007 or 0008; it is a direct
prerequisite only for 0007h.

Amendment A: an operator-issued achieve is itself the acceptance act. No
separate completion decision must be raised and resolved by the same operator
before an operator achieve can succeed, and the derived readiness, allowed
actions, blockers and attention truth must not present a phantom completion
decision: a ready, eligible operator goal offers achieve directly. The existing
authority rules for every other path (non-operator acceptance, decisions the
contract requires from another authority, required-outcome and evidence
predicates, unresolved-decision and nonterminal-task refusals) are preserved
unchanged.

Amendment B: a goal whose contract has exactly one required work item binds that
item as its integration item automatically, at both plan admission and achieve;
optional items do not change the required count, and nothing other than the
contract's own rule may imply the role. A pre-marked integration designation and
an explicit integration_work_item_id payload are no longer required for the
exactly-one-required-item case, and a conflicting explicit designation or
payload that would select a different item is refused, never overriding the
automatic binding. Every contract other than the exactly-one-required-item case
keeps the existing explicit designation and explicit integration_work_item_id
requirements unchanged. The operator-authorized proposal identity/hash, plan
immutability and idempotent replay are preserved, and no accepted plan is
silently rewritten.

Amendment C: keep the sha256:<64 hex> wire format for the Subject tree digest,
and compute the canonical Subject with one git cat-file --batch stream instead
of one git process per blob. The single-stream computation must be
byte-identical to the existing independent single-stream script on the same
commit, keep the existing size/unsupported-entry/missing-object refusals, and
satisfy the existing 30-second Subject-observation deadline recorded by the
0006j acceptance witness (docs/evidence/goal-0006j-omp-native-capability-2026-11-14.md),
which is the observed existing deadline and not a new daemon timeout. It uses
one persistent batch stream regardless of blob count instead of per-blob Git
processes.

## Done when

The amended contract is enforced and its evidence exists, and the run shows each
part itself:

- the amended contract states A and B; the wire schema expresses B's conditional
  payload obligation and describes A without changing the existing nullable
  decision_id shape; both keep their preserving clauses → the amended documents
  and schema;
- an operator achieve on a real local goal completes without a
  self-raised-and-resolved completion decision, ends in the achieved state with
  its event, and replays idempotently with the same mutation identity, and the
  pre-achieve readiness projection offers achieve directly with no phantom
  completion decision in allowed_actions, blockers or attention → the observed
  run and receipt, and the observed readiness projection;
- the preserved refusals still fire: missing required outcomes, an integration
  outcome whose Subject does not match, unresolved decisions, nonterminal tasks,
  and a decision that belongs to another authority → the observed refusals after
  the amendment;
- a single-required-item goal admits and achieves without an explicit
  integration_work_item_id and without a pre-marked integration designation; a
  conflicting explicit designation or payload for that case is refused; a
  multi-required-item goal still requires both explicitly → the observed runs
  and refusal, with proposal identity/hash and idempotent replay preserved;
- the single-stream Subject computation produces the sha256: digest for a
  repository checkout with more than a thousand blobs inside the existing
  30-second Subject-observation deadline recorded by the 0006j acceptance
  witness (docs/evidence/goal-0006j-omp-native-capability-2026-11-14.md),
  byte-identical to the independent script's result on the same commit, with the
  measured wall time recorded → the recorded measurements and digest equality;
- the existing size, unsupported-entry and missing-object refusals still fire on
  the single-stream computation → the observed refusals;
- an independent review of the amendment itself is recorded, separate from the
  implementing model → the review record.

If, at the start of the run, the approved repo/resource identity needed for a
real achieve run on the local acceptance stack is missing, stop and report at
once with the exact missing item: do not start partially, do not substitute
synthetic, mock or fixture-only evidence for a required real surface, and do not
narrow the scope. After that, take and report a reversible default for every
remaining open detail; stop only for an irreversible or out-of-bounds decision:
widening A or B beyond the approved statement (any weakening of authority,
evidence or refusals), changing the sha256: wire format, or rewriting completed
0006 history or evidence. End with a report that pairs each required item with
the evidence that demonstrated it, states the exact checks executed and their
observed results, including counts and exit codes, and lists unresolved
limitations explicitly.

Constraints that must hold: existing supported behavior stays correct and the
repository stays runnable by itself; no wire contract, gate, permission,
authority or recovery rule is weakened, removed or reinterpreted to make an
implementation pass; a fixed design change would need a documented amendment
with evidence, not a worker choice (root AGENTS.md, the fixed-design amendment
rule); evidence binds the revision it was produced on and older evidence never
validates newer code (root AGENTS.md, the evidence-binds-its-revision rule).
This amendment applies exactly as approved: the three clauses exactly as stated
above; the sha256: wire format unchanged; every existing refusal path outside
the explicitly amended A and B paths preserved; completed 0006 receipts and
archived goals untouched; no evidence from the old ceremony presented as
evidence for the new contract.

Sized for one agent run of about 12 hours of the receiving agent's own work,
including producing this evidence: a feedback-window guide, not a deadline, not
an automatic stop. Waiting, observation and other parties' decisions do not
count toward this window and are neither a completion argument nor a reason for
an extra goal; elapsed time never establishes success.

This goal is one independently mergeable PR (repository convention,
docs/goals/README.md).

## Owner acceptance

After the run (not part of this Done when): the owner reviews the amended
contract text, the wire-schema change and the recorded review, refusal and
measurement evidence against the approved A, B and C statements (this file's
amendment paragraphs). Record that review as the owner's own verdict in the
receipt; the model never rates its own work, never signs for the owner and never
treats owner acceptance as a condition this run can satisfy. Until that review
arrives, the receipt states the result as produced and awaiting the owner's
verdict.
