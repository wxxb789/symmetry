# 0007f - Runtime visibility and trustworthy execution control

Let users inspect machines and runtimes with their verified capabilities and
control work from the detail view through the start, cancel, retry and
waiting-input flows the current Portal API exposes. Requested, queued, applied,
rejected, failed, and unknown receipts remain visibly distinct across
generations. Guidance, safe-boundary pause and resume are dispatched from Chat
(0007g); this goal displays their receipt state faithfully but does not invent a
control route for them.

Prerequisite: the accepted 0007b work detail and history views.

Runtime rows present the verified capability model as it is: harness identity
and version, repository resource affinity, and per-operation support where
guidance distinguishes native steering from next-turn delivery and where pause
is unsupported or safe-boundary only. Unsupported operations are hidden or
explained, never rendered as live controls; a queued request is never shown as
applied; and Goal-managed work items never use the legacy run/retry/move paths —
they route to Goal admission.

## Done when

Execution control is trustworthy on a live local daemon, and the run shows each
part itself:

- live-daemon and browser tests for assignment and waiting-for-input flows
  against a real daemon → the observed runs and their rendered states;
- live-daemon and browser tests for a cancellation/completion race, a
  failure-repair retry, a stale command and an unsupported capability → the
  observed outcomes, each kept distinct in the UI;
- receipts across generations stay distinguishable (requested/queued, applied,
  rejected/failed, unknown) and never collapse into one status → the rendered
  receipt history, including the guidance and pause/resume receipts dispatched
  from Chat;
- live-daemon and browser tests for history pagination, refresh, disconnect and
  reconnect → the server-snapshot reconciliation observed across a reconnect
  with in-flight state, history loaded on demand and no invented transition
  authority;
- runtime and capability rows match what the runtime actually advertises,
  including explicit unsupported entries → the rendered runtime page beside the
  runtime's own capability record;
- a Goal-managed work item presents no legacy run/retry/move path and routes to
  Goal admission instead → the rendered detail view for a Goal-managed item;
- this surface's pre-existing browser acceptance coverage continues to pass on
  the new routes and its classic checks are retired in the same PR → the
  executed browser suites and the removed spec list.

If, at the start of the run, any input that only the user can supply is
missing, stop and report at once with the exact missing item: do not start
partially, do not substitute synthetic, mock or fixture-only
evidence for a required real surface, and do not narrow the scope. After that,
take and report a reversible default for every remaining open detail; stop only
for an irreversible or out-of-bounds decision: adding a new wire control route,
promoting an unverified native capability, or deleting acceptance coverage
instead of migrating it. End with a report that pairs each required item with
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
This goal excludes native capability promotion, provider connection management,
Chat, and Goal-specific screens.

Sized for one agent run of about 12 hours of the receiving agent's own work,
including producing this evidence: a feedback-window guide, not a deadline, not
an automatic stop. Waiting, observation and other parties' decisions do not
count toward this window and are neither a completion argument nor a reason for
an extra goal; elapsed time never establishes success.

This goal is one independently mergeable PR (repository convention,
docs/goals/README.md).

## Owner acceptance

After the run (not part of this Done when): the owner reviews the rendered
runtime and capability page, the work-detail control and receipt states, and the
live-daemon and browser evidence on a real local daemon against the fixed
design's /portal/runtimes and work-detail requirements (docs/design/frontend.md).
Record that review as the owner's own verdict in the receipt; the model never
rates its own rendering, never signs for the owner and never treats owner
acceptance as a condition this run can satisfy. Until that review arrives, the
receipt states the result as produced and awaiting the owner's verdict.
