# 0007d - GitHub and Azure DevOps connection management

Let users create, inspect, update, test, and remove GitHub and Azure DevOps
connections while clearly seeing authentication type, provider capabilities,
health, sync status, and actionable failure reasons. Credentials remain server
side and are never exposed through browser state, logs, or errors.

Prerequisite: the accepted 0007a shell. The evidence boundary for this goal is
fixtures_only: the real Portal API and database behavior plus controlled provider
fixtures; no real provider is contacted, and provider-side resource mutation is
out of scope; it would be a separate authorized outcome. Live provider credentials
or authorization are not a prerequisite; whatever this boundary leaves unverified
is reported as unverified and never converted into a fixture success.

The connection page is the fixed design's /portal/connections surface; this goal
owns the connection list, its health, capabilities and detail, and 0007e owns
resource bindings and synchronization on the same route.

Authentication surfaces are shown as they are: connections authenticate through
the server's provider sessions, the browser never asks for or displays a
credential, and a capability change resets the affected bound resources'
health/sync state in the same transaction instead of leaving them looking
synchronized.

## Done when

Connection management works end to end on the evidence boundary named above, and
the run shows each part itself:

- create, inspect, update, test and remove for both providers → real Portal API
  and browser flows on the revision under test, with the real-versus-fixture
  provider boundary stated per flow;
- idempotent submission, stale updates and concurrent deletion → the real Portal
  API browser flows for each, including what the UI shows when the server refuses
  a stale write;
- a failed or expired authentication, and a test that fails, produce actionable
  redacted reasons → the rendered reasons and the recorded payloads proving no
  credential or secret appears in browser state, logs or errors;
- a capability update visibly resets dependent resource health/sync state →
  observed through the real Portal API and database behavior after the update;
- whatever the boundary leaves unverified is reported as unverified, never as
  connected → the receipt's unresolved-limitations list;
- this surface's pre-existing browser acceptance coverage continues to pass on the
  new route and its classic checks are retired in the same PR → the executed
  browser suites and the removed spec list, with exact counts and exit codes.

Under this boundary, no additional user-only input beyond the accepted
prerequisite is required. Live provider credentials and authorization are never a
start prerequisite: the run starts without them, and their absence stays
unverified — never fixture success. A missing local or fixture surface is not
user-only input: it is resolved with repository tooling as part of the run, or
reported as an unresolved limitation; in that case the required evidence remains
missing and the Done when is not satisfied. If, at the start of the run, an item
only the user can supply is missing, stop and report at once with the exact
missing item: do not start partially, do not substitute synthetic, mock or
fixture-only evidence for a required real surface, and do not narrow the scope.
After that, take and report a reversible default
for every remaining open detail; stop only for an irreversible or out-of-bounds
decision: any provider-side resource mutation (a separate
authorized outcome, out of scope here), any provider API surface beyond the fixed
design, or any credential entering browser state, logs or errors. End with a report
that pairs each required item with the evidence that demonstrated it, states the
exact checks executed and their observed results including counts and exit codes,
and lists unresolved limitations explicitly.

Constraints that must hold: existing supported behavior stays correct and the
repository stays runnable by itself; no wire contract, gate, permission, authority
or recovery rule is weakened, removed or reinterpreted to make an implementation
pass; a fixed design change would need a documented amendment with evidence, not a
worker choice, and evidence binds the revision it was produced on — older evidence
never validates newer code (root AGENTS.md, Non-negotiable boundaries). This goal
excludes project resource binding, provider actions, work execution, Chat, and Goal
screens; synthetic or fixture success is never presented as live provider
connectivity.

Sized for one agent run of about 12 hours of the receiving agent's own work,
including producing this evidence: a feedback-window guide, not a deadline, not an
automatic stop. Waiting, observation and other parties' decisions neither count
toward this window nor serve as a completion argument or a reason for an extra
goal, and elapsed time never establishes success.

This goal is one independently mergeable PR (repository convention:
docs/goals/README.md).

## Owner acceptance

Owner acceptance (after the run, not part of this Done when): the owner reviews
the rendered connection flows on real data plus the evidence-boundary statement
against the fixed design's connection page and truthful-status rules. Record that
review as the owner's own verdict in the receipt; the model never rates its own
rendering, never signs for the owner and never treats owner acceptance as a
condition this run can satisfy. Until that review arrives, the receipt states the
result as produced and awaiting the owner's verdict.
