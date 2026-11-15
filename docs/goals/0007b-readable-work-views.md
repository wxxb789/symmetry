# 0007b - Readable project and work views

Let an authenticated user inspect real projects and work through dense,
consistent list, board, and work-detail views with pagination and stable URLs,
within the fixed architecture and interaction contract in docs/design/frontend.md.
Selection, filtering, navigation, refresh, and late responses must never show data
from the wrong project or replace newer state.

This is the first increment that makes real engineering work readable in the new
workspace, so the owner can judge density and clarity on actual data before any
editing flow exists.

Routes and first-screen content are the fixed design's: /portal/projects/:projectId
(saved list/board projection, filters, work summary) and /portal/work/:workItemId
(outcome, latest accepted evidence, blocker/decision, next action, execution
history; desktop drawer, full page at narrow widths; runs are drill-down detail,
the dependency graph is secondary). Work rows show title, owner, state, last
accepted result, blocking reason and cost availability; unknown usage is labelled
unknown and never rendered as $0, and stale or unknown CI is never rendered as
fresh green. Wire DTOs come from the generated contract decoders, not hand-written
parallel types.

List and history stay usable without changing a wire contract: execution history
loads on demand through the existing cursor on /portal/api/work-items/:id/timeline,
and the project work list keeps the existing /portal/api/workspace snapshot
contract. Server-side work-item pagination would be a wire-contract addition and
requires a documented design amendment first; the run reports the largest observed
snapshot as a limitation instead of inventing a second list endpoint.

Prerequisite: the accepted 0007a deployable shell (its authenticated navigation,
project selection and typed read APIs).

## Done when

Real API data renders end to end on all four read surfaces, and the run shows each
part itself:

- list, board, detail, history, empty and error states on real project data → the
  real-API browser tests covering each state, plus rendered desktop and
  narrow-screen captures for each, on the revision under test;
- stable, reloadable deep links and keyboard navigation of the list, board and
  detail surfaces → the same URLs re-entered from a fresh session, plus the
  keyboard walk recorded;
- one project's selection, filtering and navigation state after refresh, and late
  or out-of-order responses never rendered as another project's data or as newer
  state → observed in a real-API browser session that switches projects while
  earlier responses are still in flight;
- the read-only truth rules in docs/design/frontend.md (Visual and interaction
  decisions: unknown cost, stale CI, unlabeled values) → the rendered rows and
  their labels;
- the pre-existing browser acceptance coverage for these views continues to pass
  on the new routes, and this surface's classic-view checks are retired in the
  same PR (the fixed design removes the duplicate legacy presentation after
  parity) → the executed browser suites and the removed/changed spec list, with
  exact counts and exit codes.

No additional user-only input beyond the accepted prerequisite is required, and
missing repository or tool setup is ordinary run work, not a user-input stop. If,
at the start of the run, an item only the user can supply is missing, stop and
report at once with the exact missing item: do not start partially, do not
substitute synthetic, mock or fixture-only evidence for a required real surface,
and do not narrow the scope. After that, take and report a reversible default
for every remaining open detail; stop only for an irreversible or out-of-bounds
decision: deleting any browser acceptance coverage
instead of migrating it, and any wire-contract change. End with a report that
pairs each required item with the evidence that demonstrated it, states the exact
checks executed and their observed results including counts and exit codes, and
lists unresolved limitations explicitly.

Constraints that must hold: existing supported behavior stays correct and the
repository stays runnable by itself; no wire contract, gate, permission, authority
or recovery rule is weakened, removed or reinterpreted to make an implementation
pass; a fixed design change would need a documented amendment with evidence, not a
worker choice, and evidence binds the revision it was produced on — older evidence
never validates newer code (root AGENTS.md, Non-negotiable boundaries). The
read-only scope holds: this goal excludes editing, connections, resources, runtime
control, Chat, and Goal screens.

Sized for one agent run of about 12 hours of the receiving agent's own work,
including producing this evidence: a feedback-window guide, not a deadline, not an
automatic stop. Waiting, observation and other parties' decisions neither count
toward this window nor serve as a completion argument or a reason for an extra
goal, and elapsed time never establishes success.

This goal is one independently mergeable PR (repository convention:
docs/goals/README.md).

## Owner acceptance

Owner acceptance (after the run, not part of this Done when): the owner reviews
the rendered list/board/detail/history captures on real work for clarity,
information density and consistency against the fixed design references (Linear and
Attio as named in docs/design/frontend.md). Record that review as the owner's own
verdict in the receipt; the model never rates its own rendering, never signs for
the owner and never treats owner acceptance as a condition this run can satisfy.
Until that review arrives, the receipt states the result as produced and awaiting
the owner's verdict.
