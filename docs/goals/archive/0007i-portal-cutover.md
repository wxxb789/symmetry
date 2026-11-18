# 0007i - Single real-time Portal cutover and 0007 series release receipt

Complete the frontend replacement so the Phoenix release serves one coherent
workspace for every supported workflow, and produce the single 0007 series
release receipt. Real-time invalidation, reconnect snapshot refresh and cursor
catch-up, unified navigation, command discovery, legacy hash redirects, and
removal of duplicate legacy presentation leave one writable UI state owner.

Prerequisite: the accepted 0007a through 0007h outcomes. This is the series closing
goal; its receipt reviews the deployed revision and the cutover evidence together,
not a separate review goal or a second implementation round.

Real-time delivery follows the fixed protocol: authenticated operator topics for
invalidation only, a short-lived audience-scoped socket token from the same-origin
bootstrap (no operator bearer token in JavaScript or local storage), invalidation
payloads without transcripts, TanStack Query invalidation refetch, snapshot
refetch plus event-cursor catch-up on reconnect, and polling as fallback. Legacy
`/portal#...` hash locations redirect exactly once to their new routes. Removal
of the legacy renderer happens only after parity for every migrated surface;
the login template stays.

## Done when

One writable UI state owner serves every supported workflow, and the run shows
each part itself:

- The full frontend, contract, browser, accessibility and Phoenix release gates
  pass on a clean revision: record the commands, counts and exit codes on that
  revision.
- Real-time invalidation and reconnect snapshot/cursor catch-up lose no state:
  show the disconnected-then-reconnected flows and receipt history.
- Expired authentication, deep links, focus restoration, reduced motion and
  narrow screens work end to end: record the observed flows and rendered captures.
- Legacy hash redirects land on the new routes, and the classic routes,
  `portal.js`/CSS and EEx workspace renderer are gone after parity: record the
  removed presentation and passing migrated acceptance suites, with no second
  writable UI state model.
- One series release receipt binds one exact Subject and maps every clause of
  the full-series acceptance contract below to current executable or rendered
  evidence. Verify the integrated deployed experience, not isolated component
  checks as a substitute. Include the revision, evidence-validity column,
  rendered capture set and unresolved findings.
- Reused evidence is valid for the reviewed revision. Missing or stale evidence
  and unmet requirements leave the receipt incomplete; return findings to the
  responsible lettered goal and repeat affected validation before finishing the
  review. Preserve the contract text below verbatim in the receipt. Record
  provider evidence as real Portal API/database behavior with controlled provider
  fixtures under 0007d/0007e; do not contact real providers or promote fixture
  results into live connectivity proof.

No additional user-only input is required beyond the repository. If a required
user-only input is missing at the start, stop and report the exact missing item
at once: do not start partially, substitute synthetic, mock or fixture-only
evidence for a required real surface, or narrow scope. After that, take and
report a reversible default for every remaining open detail; stop only for an
irreversible or out-of-bounds decision: deleting legacy presentation before
parity, changing a wire contract, or declaring the series accepted. End with a
report pairing each required item with its evidence, exact checks and observed
results, counts and exit codes, and unresolved limitations.

Constraints that must hold: existing supported behavior stays correct and the
repository stays runnable by itself; no wire contract, gate, permission,
authority or recovery rule is weakened, removed or reinterpreted to make an
implementation pass. A fixed design change needs a documented amendment with
evidence, not a worker choice. Evidence binds the revision it was produced on;
older evidence never validates newer code. The full-series contract below
remains binding, including one owner per category of state, the selected stack,
real accepted Goal APIs, and no production Node server, second business-policy
engine, production mock data, workflow canvas, mobile application or visual
self-certification.

Sized for one agent run of about 12 hours of the receiving agent's own work,
including producing this evidence: a feedback-window guide, not a deadline or an
automatic stop. Waiting, observation and other parties' decisions do not count
toward that window. Waiting is neither a completion argument nor a reason for
an extra goal, and elapsed time never establishes success. This goal is one
independently mergeable PR.

## Full-series acceptance contract

The following contract is preserved unchanged from the former series review.
Its owner acceptance is the post-run verdict below, not a condition the run can
satisfy. The cutover run produces the evidence and receipt for that verdict.

Replace Symmetry's current portal frontend with a modern TypeScript and Effect
workspace integrated with Phoenix, within the fixed architecture and interaction
contract in [frontend](../design/frontend.md) and [protocol](../design/protocol.md).
Preserve the current project's useful behavior while making progress, results,
blockers, control receipts and necessary decisions clear in actual engineering work.

Completion requires working project/list/board, work detail, GitHub/ADO resource
connections, runtimes, conversation and run-control flows on real APIs; repeatable
typed build and validation; and production delivery with the Phoenix release.
The project owner accepts rendered views for clarity, consistency, information
density and interaction polish, using Linear and Attio as design references.

Verification demonstrates that repeated commands, stale responses, navigation,
expired authentication, refresh, disconnection and reconnection do not corrupt
visible state, lose drafts or misrepresent requested control as applied control.
Keyboard interaction, focus restoration, deep links, pagination and error states
remain usable. Existing acceptance behavior is preserved through the full UI
replacement, and duplicate legacy presentation code is removed after parity.

The workspace has one owner per category of state and follows the selected React,
Vite/pnpm, TanStack and Effect stack. It adds no production Node server or second
business-policy engine. Goal-specific screens use real 0006 capabilities when
available; finishing the existing-product frontend does not depend on inventing
those capabilities or implementing 0006. No production mock data, workflow canvas,
mobile application or visual self-certification is included in completion.

## Owner acceptance (after the run)

Not part of `Done when`: the owner reviews the series receipt and rendered
list/board/detail/chat/attention/connections/runtimes on real work at the reviewed
revision, against the fixed design, Linear/Attio references and the full-series
acceptance contract above, for clarity, consistency, information density and
interaction polish. Record the owner's own verdict and any separately authorized
`achieve` in that receipt when they arrive.
The model never rates its own rendering, signs for the owner, or treats owner
acceptance as a condition this run can satisfy. Until that review arrives, the
receipt states the result as produced and awaiting the owner's verdict.
