# 0008c - Evidence-based Elixir model routing

Produce an auditable, evidence-backed candidate routing policy for the tested
Symmetry Elixir task classes using the accepted 0008a workflow: a strong-only
baseline and a mixed workflow under identical contracts, permissions, budgets,
evidence, checks and review. Frozen representative tasks cover both
established-pattern edits and applicable database constraints, concurrent
writers, transactions, rollback, notifications, and authority-sensitive work,
and the frozen class list is fixed before either run.

Prerequisite: the accepted 0008a workflow and the existing configured
worker/reviewer routing and authorization it reuses. This goal reuses that
routing and authorization as they are (`reuse_existing`) and defines no new
profiles, spend caps or credentials.

First, prove the Elixir/PostgreSQL execution surface on this host: the
formatter, warning-as-error compilation, test and applicable integration gates
must actually run in the isolated checkout used for the evaluation. If that
surface is unavailable, stop and report; a class is never accepted from static
reading.

## Done when

A candidate policy exists with per-class evidence, and the run shows each part
itself:

- mixed-model and strong-only runs over the frozen Elixir classes → the
  per-class receipts with identical contracts, permissions, budgets, evidence,
  checks and review;
- actual model identities, provenance, prompt/skill identities, the credential
  route actually used (metadata, never secrets) and token/cost rows per accepted
  change → the receipt rows, with unknown billing recorded as unknown with a
  price version, and total cost covering exploration, implementation,
  verification, review, repair and failed attempts, plus verification and review
  effort, repairs, failures and limitations per accepted change;
- every accepted result passes the applicable Elixir and integration gates and
  an independent semantic review → the executed gate results and the review
  record for that revision;
- the proven execution surface is stated for each class → the environment field
  of each row;
- the candidate policy states, per tested class, promote-lightweight /
  strong-only / insufficient evidence with the evidence behind it, and carries
  every acceptance regression, not-cheaper class or evidence gap explicitly →
  the policy record;
- no claim is made for untested Elixir work, Go or TypeScript → the policy
  record's scope statement.

No new user-only input is required: the run reuses the accepted 0008a workflow
and the existing configured worker/reviewer routing and authorization. If, at
the start of the run, the accepted 0008a workflow, that routing, its
authorization, or the required Elixir/PostgreSQL execution surface is missing,
stop and report the exact missing item at once: do not start partially,
substitute synthetic, mock or fixture-only evidence for a required real surface,
or narrow scope. After that, take and report a reversible default for every
remaining open detail; stop only for an irreversible or out-of-bounds decision:
publishing a policy as accepted, removing a risk class from the frozen set,
exceeding the existing authorization, or accepting a class without its gates
actually running. End with a report pairing each required item with its
evidence, the exact checks and observed results (counts and exit codes where
applicable), and unresolved limitations.

Constraints that must hold: existing supported behavior stays correct and the
repository stays runnable by itself; no wire contract, gate, permission,
authority or recovery rule is weakened, removed or reinterpreted to make an
implementation pass. A fixed design change needs a documented amendment with
evidence, not a worker choice (root AGENTS.md). Evidence binds the revision it
was produced on; older evidence never validates newer code. This goal is
independent of 0008b: neither gates the other, both consume the accepted 0008a
workflow and the same reused routing and authorization, and each contradicts
none of the series' acceptance standards. The policy may recommend lightweight
execution, require strong-only execution, or record insufficient evidence for
each tested class; it recommends lightweight routing only where measured results
justify it, makes no claim for untested Elixir work, Go or TypeScript, claims no
general quality equality between workflows, invents no savings percentage or
cost figure, and never hardcodes model names into domain policy.

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
