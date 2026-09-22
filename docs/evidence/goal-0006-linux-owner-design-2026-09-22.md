# Goal 0006 Linux Durable Owner Design Evidence

## Status

This is a proposed implementation contract for the active Goal 0006b. The
current proposal is revision `0006b-linux-helper-v1.1`.

It is
not a completion receipt and does not silently amend the approved Goal or the
fixed design baseline. The current implementation subject is
`568c9403812b18438d6ebd7f40c8e69104554276`, tree
`26a402aff73d91a862ed5948c63d10797476e999`.

The owner contract below is the binding target for the Linux implementation.
Goal completion remains blocked until the contract, exact-subject production
evidence, and independent review receipts are all accepted.

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

## Exact Subject Verification

The push workflow run `35749712299`, attempt 2 (2026-09-22) produced artifact
`production-linux-containment-witness-35749712299-2` (artifact ID
`10705627093`). Its provenance binds
`subject_head=568c9403812b18438d6ebd7f40c8e69104554276` and
`subject_tree=26a402aff73d91a862ed5948c63d10797476e999`, with Go 1.27.0 on
Linux kernel 6.17.0-1022-azure.

The artifact test event stream records all three required top-level witnesses
as `run=1`, `pass=1`, `skip=0`, `fail=0`:

- `TestProductionLinuxContainmentWitnessSpine`
- `TestProductionLinuxContainmentNegativeWitness`
- `TestProductionLinuxContainmentReplayWitness`

The daemon job for the same exact subject passed `gofmt`, `go vet ./...`,
`go test -count=3 -timeout 300s ./...`, `go test -race -count=1 -timeout
600s ./...`, and the Linux cross-build. The Windows daemon job also passed its
native test/build and Windows containment witnesses. Other workflow failures
in this run were outside this containment evidence (Pi/PostgreSQL, Compose,
OpenCode synthetic, and control-restart integration jobs).

## Remaining Acceptance

This file remains an evidence record and is not a completion receipt. The
implementation-bound receipts and artifact above apply to the exact subject
named above; this evidence-only documentation update does not change daemon
behavior. Final Goal acceptance still requires the review receipts and PR
checks to bind to that subject; unrelated workflow failures must not be
represented as containment failures or silently ignored.
