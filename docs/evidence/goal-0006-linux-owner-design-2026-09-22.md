# Goal 0006 Linux Durable Owner Design Evidence

## Status

This is a proposed implementation contract for the active Goal 0006b. The
current proposal is revision `0006b-linux-helper-v1.1`.

It is
not a completion receipt and does not silently amend the approved Goal or the
fixed design baseline. The current implementation subject is
`fcebf721c8c47d163a765fec15c33be159328786`, tree
`8d3a145374882a06f8205ebf0b0fc36b4363c41c`.

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

The push workflow run `36044074516`, attempt 1 (2026-09-24) produced artifact
`production-linux-containment-witness-36044074516-1` (artifact ID
`10827668140`). Its provenance binds
`subject_head=fcebf721c8c47d163a765fec15c33be159328786` and
`subject_tree=8d3a145374882a06f8205ebf0b0fc36b4363c41c`, with Go 1.27.0 on
Linux kernel 6.17.0-1022-azure.

The artifact test event stream records all three required top-level witnesses
as `run=1`, `pass=1`, `skip=0`, `fail=0`:

- `TestProductionLinuxContainmentWitnessSpine`
- `TestProductionLinuxContainmentNegativeWitness`
- `TestProductionLinuxContainmentReplayWitness`

The daemon job for the same exact subject passed `gofmt`, `go vet ./...`,
`go test -count=3 -timeout 300s ./...`, `go test -race -count=1 -timeout
600s ./...`, and the Linux cross-build. The Windows daemon job passed its
native tests and build, the production Windows containment witness, and the
production pre-authority crash witness matrix. `integration`, `compose`,
`control`, `contracts`, `legacy-route-audit`, `pi-control-e2e-windows` and
`opencode-native-synthetic-windows` also passed. The pull request run
`36044082686` for PR #9 on the same subject has the same job results.

Two jobs fail on this subject, as decided under Harness Session Escapes:
`pi-control-e2e-linux` and `opencode-native-synthetic`. Their only failure
cause is `observed_escape`, which is the fail-closed containment result for
harness children that start a new session. They are not containment defects.

The earlier subject `568c9403812b18438d6ebd7f40c8e69104554276` (run
`35749712299`, artifact `10705627093`) is superseded; its evidence does not
validate the later code.

## Independent Review Receipts

Each review was a separate local Codex session (`gpt-6-luna`, `xhigh`) with
the requirement, this file and the exact diff. Reviewers could run tests on
Windows and in the WSL distro `symmetry-race` (Alpine, Go 1.27.0) and could
not edit tracked files.

| Subject | Scope | Verdict | Outcome |
| --- | --- | --- | --- |
| `3d5076f` | stale-run cancel fix | reject | Scope pointed at a docs-only commit and tests could not run. `f85d15b` added the missing recovery-path stop tests. |
| `f85d15b` | stale-run cancel fix, `bb239d2..f85d15b` | accept | No defect. Low: one guard test also passes on the base; no stale-witness and terminal interleaving test. |
| `f85d15b` | Linux containment, `568c940..f85d15b` | reject | P1: kept by owner decision, see Scan Exit-Race Boundary. P2: fixed in `c04cd47`. |
| `3f74f34` | delta `f85d15b..3f74f34` | reject | P1 worker-thread exit false-clean, fixed in `fe918d8`. Stale retention and endpoint probe had no defect. |
| `fcebf72` | delta `3f74f34..fcebf72` | accept | Original P1 reproduction fails closed 10 of 10. A post-snapshot unobserved escape remains outside the Goal boundary. |

## Remaining Acceptance

This file remains an evidence record and is not a completion receipt. The
artifact, checks and receipts above bind to `fcebf72`. A later commit that
only changes this file does not change daemon behavior. Goal acceptance
needs the owner to accept this evidence, the kept P1 boundary, and the two
fail-closed harness jobs.

## Harness Session Escapes (2026-09-24)

Pinned harness binaries start helper children in a new session, so the Linux
scan observes them outside the original group:

- OpenCode `1.18.30` runs `git` snapshot and repository probes (`rev-parse`,
  `ls-files`, `write-tree`) as session leaders. Reproduced locally on Linux at
  `d7cc419`: `TestNativeSyntheticGatewayToolIntegration` closes with
  `observed_escape` on every run. The same test passes on `main`, which had no
  descendant scan.
- pi `0.85.1` embeds `spawn(cmd, args, { stdio: "ignore", detached: true })`.
  In CI run `35981857022` (subject `4fa39d8`), the Linux Pi Control E2E handoff
  task failed with the same `observed_escape` stop-unproven error. This case
  was not reproduced locally.

Owner decision (2026-09-24): keep the fail-closed boundary. An observed session
escape remains durable unresolved containment. The `opencode-native-synthetic`
and Linux Pi Control E2E jobs stay red for this goal instead of weakening the
check. Supporting these harnesses on Linux needs a separate amendment that
gives the daemon an ownership boundary covering new sessions (for example a
delegated cgroup v2 or subreaper-tracked adoption set). It must not be a
harness opt-out from escape proof.

Two failures on the same jobs are not containment escapes:

- The Pi E2E test (`control/test/symmetry_control/pi_control_e2e_test.exs`
  around lines 4257 and 4432) still validates and rebuilds the Linux v1 identity
  `linux:<boot>:<pid>:<start>`. The daemon persists v2
  `linux:v2:<boot>:<pidns>:<pid>:<start>`, and that format already exists on
  `main`.
- The Windows Pi job's `pg_ctl start -w` hangs its step until the job is
  cancelled. It started failing only after `initdb` was fixed to create its own
  data directory.

## Stale-Run Cancel Race (2026-09-24)

The `integration` restart-cancel scenario was already flaky on `main`
(locally 2 of 8 passing on both `main` and this branch). A cancel that arrives
after lease loss marked the run stale was dropped: Control moves the Run to
`cancelling` and rejects the next renewal with `409 ownership_lost`, while the
daemon never published a cancellation receipt, so only Control's lease reaper
(about 30 s later) settled the Run.

The daemon now settles such a cancel without weakening the stop proof:

- While the stale owner is still in memory, the cancel is remembered and is
  acknowledged only after the exact process-exit witness matches the journal
  PID and identity. A durable terminal replays through the existing
  acknowledgement path.
- If the in-memory owner has already been released and the journal is
  `stale`, the cancel goes through `cancelRecoveredJournal`, which stops or
  proves the recorded process before it writes the receipt.
- Without a stop witness the command stays unacknowledged.

After the change the local scenario passed 5 of 6 runs. A later instrumented
repro at `f85d15b` (8 runs: 3 passed, 5 failed) showed that every failure had a
positive exact process-exit witness. The failures came from two cleanup races:

- in 3 runs, cleanup deleted the stale journal before the cancel arrived, so
  `handleCommand` could not load it;
- in 2 runs, the cancel had already loaded the stale journal when cleanup
  deleted it, and the receipt write then failed.

Owner decision (2026-09-24): keep a stale journal with no terminal state
until its lease expires. Control can send a cancel only until then; after
expiry its lease reaper settles the Run. `cleanupPending` now defers such a
journal and the cleanup retry deletes it after expiry. The cost is that the
stale journal and its workspace stay at most one lease period longer. Every
path stays fail-closed: no receipt is published without a proven process
stop.

## Scan Exit-Race Boundary (2026-09-24)

Commit `3267bd0` classifies a process that disappears during a `/proc` scan the
same way as one that disappeared between two scans. procfs reports such a
task as `ENOENT`, as `ESRCH` between open and read, or with pgrp/session `-1`
after `release_task`. A vanished descendant is skipped because its subtree is
no longer reachable from the leader. A vanished leader counts as absent only
after an identity-bound re-read (PID, pgrp, session, start time) confirms it.
Other read errors, a present leader returning `ENOENT`, identity changes, and
observed escapes still fail closed. Before this change, strict mid-scan
classification closed about a third of short runs with transient children as
unproven.

The independent Linux review (Codex `gpt-6-luna`, `xhigh`, subject
`f85d15b`) rejected this rule as a P1. Its fault-injection test lets the
leader children read return `ENOENT` while a live out-of-group process that
no scan ever observed survives; `Close()` then succeeds. A boundary probe ran
the same fixture at base `568c940` and at `f85d15b`:

- leader reaped between scans, unseen live escape: `Close()` succeeds at both
  revisions;
- leader reaped mid-scan, unseen live escape: fails closed at `568c940`,
  succeeds at `f85d15b`.

In both variants the surviving process was never observed, so it is an
unobserved escape. The Goal places that outside the verified boundary
(`docs/goals/0006b-crash-safe-process-containment.md:19-22`). The mid-scan
variant is no weaker than the between-scans variant the base already
accepted.

Owner decision (2026-09-24): keep the rule and state the boundary. The Binding
Scope sentence "They never become a successful stop receipt through absence,
`ESRCH`, or a numeric PID/PGID lookup" applies to observed escapes, scan
failures, identity mismatches, helper loss, and unknown write outcomes. An
`ENOENT` or `ESRCH` confirmed by the identity-bound re-read is an observed
exit, not a scan failure. Covering descendants that leave the group between
observations needs the separate ownership-boundary amendment named under
Harness Session Escapes.

The same review found a P2: the recovery endpoint directory was chosen by
existence only, so an existing but unwritable `TMPDIR` blocked the `/tmp`
fallback and the helper failed to bind. The directory must now accept a probe
file before it is chosen.

## Worker-Thread Children (2026-09-24)

CI run `36032576906` (subject `355f46b`) failed
`TestProcessGroupCloseRejectsLiveSetSIDDescendant` once: the monitor did not
observe the `setsid` child within 15 s. The test passed 40 of 40 local runs.
The cause was a coverage gap that already existed at base `568c940`.
`/proc/<pid>/task/<tid>/children` lists only the children forked by that
thread, and the scan read only the main thread's file. Go and Node fork from
worker threads, so their children, including observed-escape candidates, were
not visible to the scan. This weakened the observed-escape guarantee for the
original group.

The reader now lists every thread under `/proc/<pid>/task` and merges each
thread's children. It reads the main thread last. While the main thread runs,
an exiting thread hands its children to the main thread, so reading it last
sees any child moved during the pass, including from a worker that vanished
mid-read. Failure to read the main thread still follows the existing
leader-absence rules. The regression test forks from a thread other than the
main thread. It failed 5 of 5 runs with the old reader and passes with the
new one.

The independent delta review (Codex `gpt-6-luna`, `xhigh`, subject
`3f74f34`) rejected the first version, which read the main thread first. A
worker that exited after the main thread was read moved its live `setsid`
child to the main thread, so the pass missed it and `Close()` succeeded (10 of
10 reproductions). With the main thread read last, the same reproduction
fails closed in 10 of 10 runs with `descendant containment is unproven`.

Known limit: if the main thread exits while worker threads still run,
children can move to a worker that was already read. Go and Node keep the
main thread alive, so this is out of scope for now.
