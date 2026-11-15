# 0007h - Goal, attention, and decision workspace

Make /portal/attention an operational entry point for real Goals and
/portal/goals/:goalId the Goal detail surface. Users can inspect Goal state,
accepted evidence, blockers, next authorized actions, decisions, dependencies,
context, and receipts, and can issue only actions the server currently permits.

Prerequisite: the accepted 0007b work detail/evidence links, and the accepted
achieve-contract amendment (0009: amendments A, B and C). The Goal APIs are the
currently accepted projection and command surface, not a historical pull
request.

/portal/attention merges the Goal work (pending decisions, settlement attention,
derived blockers) with the project-level blocked items the shell already shows,
and stays usable when Goal rollout is off. The Goal detail surface shows the
objective, accepted progress, blocker and next authorized action, the budget
reservation and known/unknown consumption, waiting-external boundaries, and the
read-only dependency projection. Only server-allowed actions are offered,
carrying the server's expected version/revision and mutation identity;
recommendations stay visually separate from authority; the frontend never infers
Goal policy and never fabricates an unavailable capability.

## Done when

Goal work is operational from the attention entry point, and the run shows each
part itself:

- create, plan approval, activation, pause/resume, amendment, decision
  resolution and achievement on real Goal APIs → browser flows on the revision
  under test, using the amended achieve semantics;
- mutation replay, stale-revision and stale-version conflicts → the observed
  responses and the UI's recovery, never a silent overwrite;
- the dependency projection renders read-only (no editable canvas) and context
  reads respect authorization → the rendered views and the refused reads;
- attention aggregation lists Goal attention and the pre-existing project
  blockers together, and the page still works with Goal rollout unavailable →
  the rendered default landing page;
- pagination, refresh and disconnect recovery behave on the Goal surfaces → the
  observed states;
- the pre-existing browser acceptance coverage for the attention surface
  continues to pass and this surface's classic checks are retired in the same PR
  → the executed browser suites and the removed spec list.

If, at the start of the run, any input that only the user can supply is
missing, stop and report at once with the exact missing item: do not start
partially, do not substitute synthetic, mock or
fixture-only evidence for a required real surface, and do not narrow the scope.
After that, take and report a reversible default for every remaining open
detail; stop only for an irreversible or out-of-bounds decision: any action the
server does not currently allow, any client-derived Goal policy, and deleting
acceptance coverage instead of migrating it. End with a report that pairs each
required item with the evidence that demonstrated it, states the exact checks
executed and their observed results, including counts and exit codes, and lists
unresolved limitations explicitly.

Constraints that must hold: existing supported behavior stays correct and the
repository stays runnable by itself; no wire contract, gate, permission,
authority or recovery rule is weakened, removed or reinterpreted to make an
implementation pass; a fixed design change would need a documented amendment
with evidence, not a worker choice (root AGENTS.md, the fixed-design amendment
rule); evidence binds the revision it was produced on and older evidence never
validates newer code (root AGENTS.md, the evidence-binds-its-revision rule).
The frontend does not infer Goal policy or fabricate unavailable capabilities.
Goal screens use the real accepted APIs; finishing the workspace never depends
on inventing capabilities.

Sized for one agent run of about 12 hours of the receiving agent's own work,
including producing this evidence: a feedback-window guide, not a deadline, not
an automatic stop. Waiting, observation and other parties' decisions do not
count toward this window and are neither a completion argument nor a reason for
an extra goal; elapsed time never establishes success.

This goal is one independently mergeable PR (repository convention,
docs/goals/README.md).

## Owner acceptance

After the run (not part of this Done when): the owner reviews the rendered
attention and Goal detail views on real Goals, with blocker and receipt truth
legible and no raw policy internals, against the fixed design's attention and
goal routes (docs/design/frontend.md). Record that review as the owner's own
verdict in the receipt; the model never rates its own rendering, never signs for
the owner and never treats owner acceptance as a condition this run can satisfy.
Until that review arrives, the receipt states the result as produced and
awaiting the owner's verdict.
