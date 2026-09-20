# 0007-final - Modern workspace final review

> **Run last, after 0007a-0007i are complete.** The lettered goals deliver the
> workspace and Portal cutover; this goal reviews the integrated product against
> the full acceptance contract below.

Review the deployed workspace revision, the cutover evidence from
[0007i](0007i-portal-cutover.md), and the owner's rendered-product acceptance.
Verify that the combined workflows satisfy the original product outcome without
repeating their implementation or treating isolated component checks as proof
of the complete experience.

Completion requires a final review receipt mapping every requirement below to
current executable or rendered evidence and recording the owner's acceptance
and any unresolved findings. Reuse evidence only when valid for the reviewed
revision. Missing or stale evidence and unmet requirements keep this goal
incomplete; return findings to the responsible lettered goal and repeat affected
validation before finishing the review.

## Full-series acceptance contract

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
