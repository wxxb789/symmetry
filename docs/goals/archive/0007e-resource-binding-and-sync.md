# 0007e - Project resource binding and truthful synchronization

Let users bind repositories, work tracking, and CI resources from configured
GitHub or Azure DevOps connections to a project, inspect their capabilities, and
synchronize provider-owned state without presenting stale or unknown outcomes as
success. Work assignment uses only the selected authorized resources.

Prerequisite: the accepted 0007d connection management, plus the accepted 0007b
work detail view and 0007c editing surfaces that carry the assignment-eligibility
and provider-owned-field evidence. The evidence boundary is the same fixtures_only
boundary 0007d names: the real Portal API and database behavior plus controlled
provider fixtures; no real provider is contacted, and provider-side resource
creation, mutation or deletion is out of scope; it would be a separate authorized
outcome. Live provider credentials or authorization are not a prerequisite;
whatever this boundary leaves unverified is reported as unverified and never
converted into a fixture success.

On the same /portal/connections route as the connection list, resources are bound
from already-configured connections by their exact external identity; no provider
catalogue or discovery API is invented, and only the local runtime/agent resources
that the workspace already registers are selectable from a local list. Creation,
update and deletion refer to project resource bindings, not provider-side resource
mutation. Synchronization reuses the existing provider intent, unknown-outcome and
readback semantics; an unknown or failed sync is never rendered as success or
freshness.

## Done when

Binding and synchronization are truthful end to end, and the run shows each part
itself:

- binding, updating, unbinding and synchronizing repository, work-tracking and CI
  resources from both provider kinds → real-API and browser tests on the revision
  under test, covering each operation, with the real-versus-fixture provider
  boundary stated per flow;
- provider failure and unknown outcomes remain explicit and never look
  synchronized → the real-API and browser tests for an injected provider failure
  and an unknown outcome resolved by readback;
- project switching and concurrent refresh never let a late response overwrite the
  newly selected project's state → the same tests run with concurrent operations;
- synchronized external work items keep their exact external identity,
  provider-owned fields stay locked, and an externally unavailable item shows
  unavailable with no execution entry point → the tests' rendered item and the
  refused actions;
- work assignment offers only the selected authorized resources → observed through
  the same tests on the work detail and editing surfaces;
- this surface's pre-existing browser acceptance coverage continues to pass on the
  new route and its classic checks are retired in the same PR → the executed
  browser suites and the removed spec list, with exact counts and exit codes.

Under this boundary, no additional user-only input beyond the accepted
prerequisites is required. Live provider credentials and authorization are never a
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
decision: any provider-side resource creation, mutation or
deletion (a separate authorized outcome, out of scope here), any provider API
surface beyond the fixed design, or any credential entering browser state, logs or
errors. End with a report that pairs each required item with the evidence that
demonstrated it, states the exact checks executed and their observed results
including counts and exit codes, and lists unresolved limitations explicitly.

Constraints that must hold: existing supported behavior stays correct and the
repository stays runnable by itself; no wire contract, gate, permission, authority
or recovery rule is weakened, removed or reinterpreted to make an implementation
pass; a fixed design change would need a documented amendment with evidence, not a
worker choice, and evidence binds the revision it was produced on — older evidence
never validates newer code (root AGENTS.md, Non-negotiable boundaries). This goal
excludes provider policy duplication, runtime control, Chat, and Goal-specific UI.

Sized for one agent run of about 12 hours of the receiving agent's own work,
including producing this evidence: a feedback-window guide, not a deadline, not an
automatic stop. Waiting, observation and other parties' decisions neither count
toward this window nor serve as a completion argument or a reason for an extra
goal, and elapsed time never establishes success.

This goal is one independently mergeable PR (repository convention:
docs/goals/README.md).
