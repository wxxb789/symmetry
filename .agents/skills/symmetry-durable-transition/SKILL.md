---
name: symmetry-durable-transition
description: Implement or review Symmetry changes to goal/task transitions, leases, retry, settlement, budget or external effects using explicit failure and recovery evidence.
---

# Durable transition

Read the relevant sections of [core](../../../docs/design/core.md),
[data](../../../docs/design/data.md) and [protocol](../../../docs/design/protocol.md).
Existing execution behavior also follows docs/protocol-v1.md.

Before editing, identify authoritative rows, transaction/lock order, mutation
identity, accepted revision/fence, and each external side effect. Draw the actual
commit/ack boundary only when needed to expose ambiguity.

For each changed boundary, identify behavior on crash before commit, after commit
before acknowledgement, and on replay. Include stale ownership/revision and
cancel/completion races when affected. An unknown external result requires
readback or explicit unresolved status, not unconditional retry.

Implement through the existing domain owner. State + durable wakeup + compact
receipt commit together; no network under DB locks. Failure usage is recorded
independently from accepted progress. Test affected invariants with fault
injection or synchronization, not timing sleeps or tests that mirror branches.

Return the enforced invariant, exact tested revision, executed evidence and
remaining failure boundaries. If a fixed contract is infeasible, report the
specific conflict and proposed amendment; continue unrelated authorized work.
