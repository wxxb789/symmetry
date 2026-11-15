# 0008b - Evidence-based Go model routing

Produce an auditable, evidence-backed candidate routing policy for the tested
Symmetry Go task classes using the accepted 0008a workflow: a strong-only
baseline and a mixed workflow under identical contracts, permissions, budgets,
evidence, checks and review. Frozen representative tasks cover both
established-pattern edits and applicable resource ownership, cancellation,
native platform, persistence, or recovery-sensitive changes, and the frozen
class list is fixed before either run.

Prerequisite: the accepted 0008a workflow and the existing configured
worker/reviewer routing and authorization it reuses. This goal reuses that
routing and authorization as they are (`reuse_existing`) and defines no new
profiles, spend caps or credentials.

Runs execute on the named host surfaces actually used (Windows and/or the Linux
surface), and each class records the surface its gates ran on. Before the runs,
the Go toolchain gates used for acceptance are proven to execute in the isolated
checkout on this host; no cross-platform extrapolation, no gate claimed from
reading code, and unavailable checks stay pending with the missing environment
named.

## Done when

A candidate policy exists with per-class evidence, and the run shows each part
itself:

- mixed-model and strong-only runs over the frozen Go classes → the per-class
  receipts with identical contracts, permissions, budgets, evidence, checks and
  review;
- actual model identities, provenance, prompt/skill identities, the credential
  route actually used (metadata, never secrets) and token/cost rows per accepted
  change → the receipt rows, with unknown billing recorded as unknown with a
  price version, and total cost covering exploration, implementation,
  verification, review, repair and failed attempts, plus verification and review
  effort, repairs, failures and limitations per accepted change;
- every accepted result passes the applicable Go gates and an independent
  semantic review → the executed gate results and the review record for that
  revision;
- the candidate policy states, per tested class, promote-lightweight /
  strong-only / insufficient evidence with the evidence behind it → the policy
  record;
- any class whose acceptance regressed, that is not cheaper, or that lacks
  adequate evidence is carried explicitly rather than recommended → the policy
  record's negative findings;
- no claim is made for untested Go work, Elixir or TypeScript → the policy
  record's scope statement.

No new user-only input is required: the run reuses the accepted 0008a workflow
and the existing configured worker/reviewer routing and authorization. If, at
the start of the run, the accepted 0008a workflow, that routing or its
authorization is missing, stop and report the exact missing item at once: do not
start partially, substitute synthetic, mock or fixture-only evidence for a
required real surface, or narrow scope. After that, take and report a reversible
default for every remaining open detail; stop only for an irreversible or
out-of-bounds decision: publishing a policy as accepted, removing a risk class
from the frozen set, exceeding the existing authorization, or writing a
fabricated cost or savings figure. End with a report pairing each required item
with its evidence, the exact checks and observed results (counts and exit codes
where applicable), and unresolved limitations.

Constraints that must hold: existing supported behavior stays correct and the
repository stays runnable by itself; no wire contract, gate, permission,
authority or recovery rule is weakened, removed or reinterpreted to make an
implementation pass. A fixed design change needs a documented amendment with
evidence, not a worker choice (root AGENTS.md). Evidence binds the revision it
was produced on; older evidence never validates newer code. The policy may
recommend lightweight execution, require strong-only execution, or record
insufficient evidence for each tested class; it recommends lightweight routing
only where measured results justify it, makes no claim for untested Go work,
Elixir or TypeScript, claims no general quality equality between workflows,
invents no savings percentage or cost figure, and never hardcodes model names
into domain policy. This goal is independent of 0008c: neither gates the other.

Sized for one agent run of about 12 hours of the receiving agent's own work,
including producing this evidence: a feedback-window guide, not a deadline and
not an automatic stop. Waiting, observation and other parties' decisions do not
count toward it; waiting is neither a completion argument nor a reason for an
extra goal, and elapsed time never establishes success. This goal is one
independently mergeable PR (repository convention, docs/goals/README.md).

## Owner acceptance

After the run, not part of this Done when: the owner reviews the per-class
receipts, the cost tables and the candidate policy record against
docs/design/quality.md's evaluation contract, and accepts or rejects the routing
policy. Record that review as the owner's own verdict; the model never rates its
own work, never signs for the owner and never treats owner acceptance as a
condition this run can satisfy. Until that review arrives, the record states the
result as produced and awaiting the owner's verdict.
