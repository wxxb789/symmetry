# Runbooks

Operator procedures and opt-in verification recipes for the delivered Symmetry
system. A runbook states what to run on the current tree, what the result means,
and what it does not prove; when it disagrees with the code, the code wins.

These documents are not contracts. The wire contract is
[`../protocol-v1.md`](../protocol-v1.md), the fixed architecture and boundaries
are [`../design/`](../design/README.md), goal contracts are
[`../goals/`](../goals/README.md), dated evidence records are
[`../evidence/`](../evidence/), and the checks CI actually runs are listed under
*Commands and gates* in [`../design/quality.md`](../design/quality.md).

## Operate

- [Control plane and daemon](control-plane-and-daemon.md) — release build,
  production Compose edge, configuration, operator API, terminal-delivery
  recovery.
- [Portal](portal.md) — build and start the engineering workspace, workspace
  model, run controls, Portal HTTP API, post-deploy signals.
- [Connections](connections.md) — GitHub and Azure DevOps authentication,
  resource binding, synchronization, provider-owned fields, provider broker.
- [Chat and supervisory control](chat-and-supervisory-control.md) — scoped chat,
  guidance, pause/resume/cancel, decision packets, rollout and recovery.

## Verify (opt-in recipes)

- [Daemon process stop and terminal cleanup](daemon-process-stop.md) — what
  counts as physical stop evidence and how to re-run the daemon gates.
- [OpenCode peer ownership](daemon-opencode-peer-ownership.md) — the loopback
  peer-ownership check the OpenCode adapter performs on every connection.
- [Goal session receipt recovery](session-receipt-recovery-e2e.md) — the opt-in
  committed-receipt E2E across a Go client, a real Phoenix listener and
  PostgreSQL.

## History

The four operator documents were renamed from the goal-numbered runbook names
they carried while goals 01-04 were delivered, and the three recipes from
goal-numbered testing names. Archived plans and evidence records still cite the
old names as dated records of the revision they describe; they are not
rewritten, and their links are not current tree paths. This directory is where
the current documents live.
