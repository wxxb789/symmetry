# 0008a - Auditable model-independent acceptance workflow

Establish a runnable workflow that executes a frozen Symmetry task contract in
an isolated checkout and binds implementation, verification, and independent
review to one exact Subject, following the quality policy's evaluation contract
(docs/design/quality.md). The receipt records the base and head revisions,
required checks, evidence, findings, repairs, costs when known, and unresolved
limitations, using the fixed task-level fields under
docs/evaluations/model-routing/: task_id, base/head, language, risk_class,
workflow, model_ids, prompt_hashes, input/output/cached tokens (nullable),
reported/estimated cost with provenance and price version, wall_time,
review_time, repair_count, accepted, reviewer_findings, later_defects,
environment and evidence refs. Raw sensitive transcripts stay outside Git.

Prerequisite: none. It can run in parallel with the 0007 workspace series; it
does not depend on the frontend and the frontend does not depend on it.

The workflow reuses the existing configured worker/reviewer routing and
authorization (`reuse_existing`): it defines no new profiles, spend caps or
credentials and needs no new owner answer. Each run records the actual model
identities, provenance and the credential route actually used (route metadata,
never secrets).

The workflow also produces the series' operational assets: usable task
instructions and scoped repository skills for the evaluated classes, and
executable verification plus independent review behavior that prevents an
implementing model from silently weakening tests, types, authority, recovery or
acceptance requirements. The evaluator is not a new benchmarking service and
invents no missing cost; unavailable credentials or prices leave that evidence
incomplete, never an estimated success. Missing credentials or authorization for
a required configured route at run start uses the stop-and-report rule below
instead of continuing partially; usage or prices that were not reported remain
unknown or incomplete, never fabricated.

## Done when

A frozen contract runs end to end in isolation and the workflow's guards are
demonstrated, and the run shows each part itself:

- positive and negative runs on real current Go or Elixir work → the receipts
  with base/head, the actual model identities, provenance, the credential route
  used (metadata, never secrets), checks, evidence, and the exact Subject they
  bound;
- one bounded task completed by the lightweight worker → its receipt, with the
  explanations the task required;
- one escalation triggered by a real boundary (an invariant that cannot be
  explained or tested, a diff crossing its contract, recurring failures without
  a changed diagnosis, or a proposed weakening of a guard) → the escalation
  handoff carrying failing evidence and the already-correct work preserved;
- rejection controls: a stale revision, a missing gate, a weakened test or
  guard, and an incomplete or fabricated receipt are all refused → the observed
  refusals;
- total-cost discipline: exploration, implementation, verification, review,
  repair and failed attempts are counted, unknown billing is recorded as unknown
  with token counts and a price version, never as zero → the receipt rows;
- the workflow's task instructions and review behavior demonstrably prevent
  silent weakening → the negative controls above plus the independent review
  record on the produced revision.

No new user-only input is required: the workflow reuses the existing configured
worker/reviewer routing and authorization. If, at the start of the run, that
configured routing or its authorization is missing, stop and report the exact
missing item at once: do not start partially, substitute synthetic, mock or
fixture-only evidence for a required real surface, or narrow scope. After that,
take and report a reversible default for every remaining open detail; stop only
for an irreversible or out-of-bounds decision: exceeding the existing
authorization for those routes, storing raw sensitive transcripts in Git, or
weakening a gate or acceptance rule. End with a report pairing each required
item with its evidence, the exact checks and observed results (counts and exit
codes where applicable), and unresolved limitations.

Constraints that must hold: existing supported behavior stays correct and the
repository stays runnable by itself; no wire contract, gate, permission,
authority or recovery rule is weakened, removed or reinterpreted to make an
implementation pass. A fixed design change needs a documented amendment with
evidence, not a worker choice (root AGENTS.md). Evidence binds the revision it
was produced on; older evidence never validates newer code. This goal selects
no model or routing policy, stores no sensitive transcript, invents no missing
cost, defines no new cap or savings percentage, and never weakens tests, types,
authority, recovery, permissions, or acceptance.

Sized for one agent run of about 12 hours of the receiving agent's own work,
including producing this evidence: a feedback-window guide, not a deadline and
not an automatic stop. Waiting, observation and other parties' decisions do not
count toward it; waiting is neither a completion argument nor a reason for an
extra goal, and elapsed time never establishes success. This goal is one
independently mergeable PR (repository convention, docs/goals/README.md).
