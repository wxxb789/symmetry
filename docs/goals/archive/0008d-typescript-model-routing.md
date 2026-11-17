# 0008d - TypeScript model routing and 0008 series release receipt

Produce an auditable, evidence-backed candidate routing policy for the tested
Symmetry TypeScript task classes using the accepted 0008a workflow, and produce
the single 0008 series release receipt. This is the series closing goal: its
receipt reviews the workflow receipts and the three language evaluations
together and is written after 0008a-0008c are accepted; it is the single series
release receipt, not a separate review round.

Prerequisite: the accepted 0008a workflow and the existing configured
worker/reviewer routing and authorization it reuses; the accepted 0007c and
0007f surfaces that carry conflict recovery and command-receipt truth; and the
accepted 0008b and 0008c results that the series receipt must map. This goal
reuses that routing and authorization as they are (`reuse_existing`), defines no
new profiles, spend caps or credentials, and never waits on the whole 0007
series.

The frozen task set covers established component work and the applicable
decoder, asynchronous-state, cancellation, conflict-recovery and receipt-truth
behavior carried by those surfaces. The class list is fixed before either run: a
class whose executable acceptance commands do not exist at the frozen revision
stops the run and is reported, and is recorded as untested; it is never silently
removed from scope by the run.

## Done when

A candidate policy and the series release receipt exist, and the run shows each
part itself:

- mixed-model and strong-only runs over the frozen TypeScript classes → the
  per-class receipts with identical contracts, permissions, budgets, evidence,
  checks and review;
- actual model identities, provenance, prompt/skill identities, the credential
  route actually used (metadata, never secrets) and token/cost rows per accepted
  change → the same field set as the Go and Elixir goals (the fixed task-level
  fields of docs/design/quality.md), with strict type, lint, test and build
  results, rendered interaction and accessibility evidence where applicable, and
  an independent contract review; unknown billing recorded as unknown with a
  price version, and total cost covering exploration, implementation,
  verification, review, repair and failed attempts, plus verification and review
  effort, repairs, failures and limitations per accepted change;
- the candidate policy states, per tested class, promote-lightweight /
  strong-only / insufficient evidence with the evidence behind it; every
  recommended rule stays within its tested class; any class without executable
  acceptance commands is reported untested rather than trimmed; and every
  acceptance regression or not-cheaper class stays visible → the policy record;
- the workflow receipts and the Go, Elixir and TypeScript evaluations are
  reviewed together → one series release receipt on one exact Subject listing
  the recommended-lightweight classes, the strong-only classes, the
  evidence-insufficient classes, the routing decisions recommended for owner
  acceptance, the known costs, limitations and any unresolved findings, with
  valid negative findings and explicitly insufficient results preserved;
- the review adds no new benchmark or implementation work and makes no claim for
  untested classes → the receipt's scope statement;
- the receipt maps every clause of the full-series acceptance contract below to
  evidence at the frozen revisions, each task binding its own base/head (the
  tasks need not share one commit) → the clause-by-clause mapping;
- missing required inputs, stale evidence, or unmet requirements keep this goal
  incomplete: the receipt is not finished, and findings return to the
  responsible lettered goal (0008a, 0008b, 0008c or the named 0007 surface)
  before the review finishes → the returned findings and the incomplete-receipt
  state.

No new user-only input is required: the run reuses the accepted 0008a workflow
and the existing configured worker/reviewer routing and authorization. If, at
the start of the run, that routing or its authorization is missing, or a frozen
class lacks its executable acceptance commands, stop and report the exact
missing item at once: do not start partially, substitute synthetic, mock or
fixture-only evidence for a required real surface, or narrow scope. After that,
take and report a reversible default for every remaining open detail; stop only
for an irreversible or out-of-bounds decision: publishing a policy as accepted,
removing a class from scope, exceeding the existing authorization, or writing a
fabricated comparison. End with a report pairing each required item with its
evidence, the exact checks and observed results (counts and exit codes where
applicable), and unresolved limitations.

Constraints that must hold: existing supported behavior stays correct and the
repository stays runnable by itself; no wire contract, gate, permission,
authority or recovery rule is weakened, removed or reinterpreted to make an
implementation pass. A fixed design change needs a documented amendment with
evidence, not a worker choice (root AGENTS.md). Evidence binds the revision it
was produced on; older evidence never validates newer code. The TypeScript
candidate policy may recommend lightweight execution, require strong-only
execution, or record insufficient evidence; it makes no claim for Go, Elixir,
untested frontend classes, or behavior absent from the accepted 0007 increments,
claims no general quality equality between workflows, and never hardcodes model
names into domain policy. No arbitrary savings target, invented model benchmark,
fabricated evaluation result or proof of absolute quality equality is required
or permitted; missing provider access, costs or independent review remain missing
evidence; and merely writing AGENTS.md and skills does not complete this goal.

Sized for one agent run of about 12 hours of the receiving agent's own work,
including producing this evidence: a feedback-window guide, not a deadline and
not an automatic stop. Waiting, observation and other parties' decisions do not
count toward it; waiting is neither a completion argument nor a reason for an
extra goal, and elapsed time never establishes success. This goal is one
independently mergeable PR (repository convention, docs/goals/README.md).

## Full-series acceptance contract

The following contract is preserved unchanged from the former series review. Its
owner acceptance is the post-run verdict below, not a condition this run can
satisfy; the run produces the evidence and receipt for that verdict.

Establish and validate a practical development workflow in which lightweight
models perform suitable Symmetry Go, Elixir and TypeScript work without lowering
the code acceptance standard, following [quality](../design/quality.md) and the
repository's language-specific contributor contracts.

Completion requires usable task instructions, scoped repository skills,
executable verification and independent review behavior that prevent an
implementing model from silently weakening tests, types, authority, recovery or
acceptance requirements. Real tasks demonstrate escalation when their design or
verification exceeds the selected worker's supported scope.

Compare mixed-model and strong-model workflows on frozen representative project
tasks with the same acceptance conditions. Record actual total cost per accepted
change, verification and review effort, repair, observed failures and limitations.
The owner can inspect the underlying evidence and accept a task-class routing
policy. New frontend-stack coverage uses a representative 0007 implementation;
Go/Elixir evaluation need not wait for it.

Recommend lightweight routing only where measured results justify it. A finding
that a task class is not cheaper or lacks adequate evidence is a valid outcome
and must remain visible. No arbitrary savings target, invented model benchmark,
fabricated evaluation result or proof of absolute quality equality is required
or permitted. Missing provider access, costs or independent review remain missing
evidence. Merely writing AGENTS.md and skills does not complete this goal.

## Owner acceptance (after the run)

Not part of this Done when: the owner reviews the TypeScript receipts, the
paired rendered capture set for the two workflows and the series release receipt
against docs/design/quality.md, records their qualitative visual judgment of the
rendered UI, and accepts or rejects the TypeScript, Go and Elixir routing
policies together. Record that review as the owner's own verdict; the model
never rates its own rendering, never signs for the owner and never treats owner
acceptance as a condition this run can satisfy. Until that review arrives, the
receipt states the result as produced and awaiting the owner's verdict.
