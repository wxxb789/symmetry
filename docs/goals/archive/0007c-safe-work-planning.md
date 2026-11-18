# 0007c - Safe project and work planning

Let users create, edit, archive, and restore projects and create, edit, order,
and move work items without duplicate submissions, lost drafts, or silent
overwrite of concurrent changes. Scoped view preferences remain attached to the
correct project and view.

Prerequisite: the accepted 0007b read views (its list, board, detail and history
surfaces are the surfaces this goal makes writable).

Goal-managed changes retain the server's amendment and admission rules; board
movement never creates an accepted outcome. Goal-managed work items and
external-provider-owned fields stay read-only from this UI: an archived project
is read-only except for restore, provider-owned fields are never editable here,
and a Goal-managed item's run/retry/move paths route to Goal admission rather
than the legacy paths. View preferences are view preferences only: versioned
browser-local storage under a user/deployment scope, never credentials, evidence,
session handles or authoritative task data.

## Done when

Users can plan safely on real data, and the run shows each part itself:

- successful project and work-item mutations → real-API browser tests on the
  revision under test;
- duplicate submissions avoided and explicit retries reusing the original intent
  → the real-API browser tests for a double-submitted action and an explicit
  retry;
- an optimistic-lock conflict surfaces visibly and re-syncs the newest server
  state instead of silently overwriting it, and background refresh never replaces
  another selected resource's draft → the real-API browser tests for the conflict
  and refresh scenarios, run with two concurrent contexts;
- cross-project response races, refresh while editing, validation errors and
  recovery leave no corrupted visible state → the same real-API browser test
  evidence set;
- keyboard board movement and focus restoration, with Goal-managed items and
  provider-owned fields presenting no writable path → the real-API browser tests
  on both item kinds;
- the pre-existing browser acceptance coverage for these flows continues to pass
  on the new routes, and this surface's classic checks are retired in the same PR
  → the executed browser suites and the removed/changed spec list, with exact
  counts and exit codes.

No additional user-only input beyond the accepted prerequisite is required, and
missing repository or tool setup is ordinary run work, not a user-input stop. If,
at the start of the run, an item only the user can supply is missing, stop and
report at once with the exact missing item: do not start partially, do not
substitute synthetic, mock or fixture-only evidence for a required real surface,
and do not narrow the scope. After that, take and report a reversible default
for every remaining open detail; stop only for an irreversible or out-of-bounds
decision: any change to the Goal admission or
provider-ownership rules, and deleting browser acceptance coverage instead of
migrating it. End with a report that pairs each required item with the evidence
that demonstrated it, states the exact checks executed and their observed results
including counts and exit codes, and lists unresolved limitations explicitly.

Constraints that must hold: existing supported behavior stays correct and the
repository stays runnable by itself; no wire contract, gate, permission, authority
or recovery rule is weakened, removed or reinterpreted to make an implementation
pass; a fixed design change would need a documented amendment with evidence, not a
worker choice, and evidence binds the revision it was produced on — older evidence
never validates newer code (root AGENTS.md, Non-negotiable boundaries). This goal
excludes provider connections, resource synchronization, runtime control, Chat,
and Goal-specific operations.

Sized for one agent run of about 12 hours of the receiving agent's own work,
including producing this evidence: a feedback-window guide, not a deadline, not an
automatic stop. Waiting, observation and other parties' decisions neither count
toward this window nor serve as a completion argument or a reason for an extra
goal, and elapsed time never establishes success.

This goal is one independently mergeable PR (repository convention:
docs/goals/README.md).
