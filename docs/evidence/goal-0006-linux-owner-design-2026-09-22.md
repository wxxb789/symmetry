# Goal 0006 Linux Durable Owner Design Evidence

## Status

This is a proposed implementation contract for the active Goal 0006b. The
current proposal is revision `0006b-linux-helper-v1.1`.

It is
not a completion receipt and does not silently amend the approved Goal or the
fixed design baseline. The current subject is `055fd1ea45c91427444fb6becfd839174efdf2c7`.

The existing Linux implementation is still fail-closed but does not provide a
daemon-external owner after daemon crash. Goal completion remains blocked until
the owner contract below is implemented and exercised by a production-binary
Linux witness.

## Binding Scope

The Linux verified subject remains the original pidfd-owned process group from
`docs/goals/0006b-crash-safe-process-containment.md:19-22`. This proposal does
not add cgroup v2, systemd delegation, cross-host transfer, a second scheduler,
or a stronger guarantee for descendants that escape the original Unix group.

Observed escapes, scan failures, identity mismatches, helper loss, and unknown
write outcomes remain durable and fail closed. They never become a successful
stop receipt through absence, `ESRCH`, or a numeric PID/PGID lookup.

## Owner Contract

1. A Linux per-run helper is the parent-side launch owner. It is started before
   the target is runnable and owns the target's process-group pidfd.
2. The helper starts the target with `Setpgid`, `Pdeathsig`, and a retained
   pidfd, then holds the target stopped at the exec gate. The target cannot run
   user code before durable prepare/bind/commit and lease arm complete.
3. The daemon and helper exchange target identity, process-group anchor, helper
   identity, and launch status over inherited local file descriptors. Secrets
   are sent only after the exact helper identity is bound; no secret enters
   argv, environment, socket names, or logs.
4. The helper watches an inherited daemon-owner pipe. EOF is an owner-loss
   event: the helper force-stops the original pidfd-owned group, performs the
   existing final descendant/escape proof, and retains its recovery endpoint
   until an exact stop receipt and release proof are durable.
5. Recovery authenticates the exact helper identity, target identity, launch
   token, and owner endpoint before stopping or releasing anything. A missing
   helper, reused PID, missing leader, or mismatched anchor is unresolved.
6. The existing journal CAS lifecycle remains authoritative:
   `prepare -> bind -> commit -> stop receipt -> release -> clear`.
   `ContainmentUnproven` remains a fail-closed diagnostic/barrier, not an owner
   substitute.
7. The helper is the only normal lease, resume, stop, and release authority.
   While the target is still exec-stopped, the daemon keeps a volatile
   emergency mirror containing the exact target pidfd, group anchor, and
   descendant monitor. The mirror cannot renew, resume, or compete with a
   healthy helper. It may hard-stop and prove the group only after exact helper
   death is observed and the helper identity is fenced.
8. The guarantee is single-fault: a daemon crash is handled by the surviving
   helper; a helper crash is handled by the surviving daemon mirror. Correlated
   daemon+helper loss or PID-namespace/host loss is outside this proposal.
9. Resume is the one `PtraceDetach`/exec-release transition and occurs only
   after durable commit and initial lease acknowledgement. No user code may run
   before that point.

## Required Implementation Seams

- `daemon/internal/execution/process_launcher_linux.go`: helper-first launch,
  stopped target, resume and wait/status plumbing.
- `daemon/internal/platform/containment_supervisor_linux.go`: inherited-FD
  protocol, owner watchdog, authenticated stop/release/lease operations, and
  production `-containment-supervisor` dispatch.
- `daemon/internal/app/recovery_containment_linux.go` and Linux termination
  recovery: exact helper/target identity validation and replayable stop/release.
- `daemon/internal/authority` and `daemon/internal/state`: only additive local
  authority fields or a platform-neutral owner proof may be introduced. The
  Windows-only `CreatorSessionID` must not be fabricated for Linux abort proofs.
- Existing Runner/App callback seams and Linux process-group proof code must be
  reused; no Control API or `contracts/v1` change is intended.

## Acceptance Evidence

The final subject must include a Linux production-binary witness covering:

- crash before durable prepare/bind;
- crash after bind and before commit;
- crash after commit and before resume;
- daemon owner `SIGKILL` while a target and immediate same-group descendant are
  live;
- helper owner loss and helper crash;
- stop-receipt, release, and clear response loss/replay;
- PID/identity reuse and anchor/session mismatch rejection;
- observed `setsid` escape and injected descendant-scan failure retaining
  unresolved state;
- successful stop proof clearing the exact marker only after durable receipt.

The helper-crash case must include a live same-group descendant and prove that
the daemon mirror stops the exact group only after helper death. A healthy
helper must remain the sole owner of normal lease/resume/release operations.

Each case must bind evidence to the exact source revision and record target,
descendant, helper PID/identity, process-group anchor, journal snapshots,
external crash exit status, and raw test output. A skipped witness is not a
passing gate.

## Current Gap

At `055fd1e`, Linux still has only `Setpgid`, direct-child `Pdeathsig`, and a
daemon-memory pidfd/monitor in `daemon/internal/platform/process_unix.go`.
The initial-scan barrier and retained-attach persistence fixes are verified,
but they do not satisfy the daemon-external owner contract above. The Goal
therefore remains active and incomplete.
