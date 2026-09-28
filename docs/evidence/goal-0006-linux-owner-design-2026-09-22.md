# Goal 0006 Linux Durable Owner Design Evidence

## Status

The active Goal now requires a v1.2 Linux new-run boundary that also contains
detached native descendants. The owner approved a distinct unprivileged CLI
UID, operator-provisioned CLI HOME, a supported systemd host (not rootless
Compose native execution), one PR, and a shared native-UID trust domain. The
v1.2 contract near the end of this file is a **pending design**, not a
certified implementation, deployed privilege policy, positive CI gate or
completion receipt. The earlier group-only proposal, revision
`0006b-linux-helper-v1.1`, and its acceptance/evidence sections are historical
for existing v1 journal recovery, **not** the current Goal target.

The prior implementation subject is
`add532e926fac69ada5a2fbe5c7103dace596ae9`, tree
`af920dbadbfa19c9e58bfabd199dcdf8395e53f5`.

Goal completion remains blocked until the revised owner contract, its
exact-subject production evidence, and independent reviews are accepted.

## Historical v1.1 Binding Scope — existing journals only

The former v1.1 verified subject was the original pidfd-owned process group.
The current Goal **supersedes this scope for new Linux native executions**;
v1 owners still require their own group-only journal reader and fail-closed
recovery, without converting their old scan proofs into v1.2 receipts.

Observed escapes, scan failures, identity mismatches, helper loss, and unknown
write outcomes remain durable and fail closed. They never become a successful
stop receipt through absence, `ESRCH`, or a numeric PID/PGID lookup.

## Historical v1.1 Owner Contract — existing journals only

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

## Historical v1.1 Implementation Seams

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

## Historical v1.1 Acceptance Evidence

The former v1.1 subject required a Linux production-binary witness covering:

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

The push workflow run `36120037489`, attempt 1 (2026-09-25) produced artifact
`production-linux-containment-witness-36120037489-1` (artifact ID
`10856674088`). Its provenance binds
`subject_head=add532e926fac69ada5a2fbe5c7103dace596ae9` and
`subject_tree=af920dbadbfa19c9e58bfabd199dcdf8395e53f5`, with Go 1.27.0 on
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
`36120042078` for PR #9 on the same subject has the same job results.

Two jobs fail on this subject, as decided under Harness Session Escapes:
`pi-control-e2e-linux` and `opencode-native-synthetic`. Their only failure
cause is `observed_escape`, which is the fail-closed containment result for
harness children that start a new session. They are not containment defects.

The earlier subjects `568c9403812b18438d6ebd7f40c8e69104554276` (run
`35749712299`, artifact `10705627093`) and
`fcebf721c8c47d163a765fec15c33be159328786` (run `36044074516`, artifact
`10827668140`) are superseded; their evidence does not validate the later
code.

**Artifact revocation (2026-09-26):** the historical artifact identifiers above
are provenance only and no longer provide downloadable evidence. The old CI
upload glob included unredacted `state/runs/journal-*.json` files containing
machine-local helper secrets and lease tokens. After explicit owner approval,
all 69 unexpired `production-linux-containment-witness-*` artifacts were deleted
from the public repository; a subsequent artifact listing returned zero. The
emergency security-only commit `7fe362a648b79d9d583a1ac6cb9c96c0c948126a`
was pushed to PR #9: it stops printing raw Linux witness test events and
publishes only workflow/source provenance, not journals, case evidence, daemon
logs, or raw test JSON. The uncommitted full patch additionally allowlists
redacted `witness-evidence.json` and one-level daemon logs and redacts witness
error diagnostics and evidence projections. Existing CI run logs were sampled
without printing credentials;
no raw credential serialization was confirmed in the sampled failed logs. This
does not restore the deleted raw artifact evidence or establish absence of
exposure in every historical CI log.

The security-only push run `36245460137` on `7fe362a` completed its
`integration`, Linux/Windows daemon, Control, contracts, and other unaffected
jobs successfully. Artifact `production-linux-containment-witness-36245460137-1`
(`10906244752`) contains exactly three `provenance-*.json` files and the
workflow provenance text file; no journal, raw Go test JSON, daemon log, or
case-level witness evidence was uploaded. Its Linux Pi Control E2E and OpenCode
native-synthetic jobs still failed, so the PR remains unmergeable.

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
| `add532e` | delta `fcebf72..add532e` | accept | A deterministic kill-to-record window amplification fails 10 of 10 `-race` batches on `fcebf72` and passes 10 of 10 on `add532e`; the unmodified race test passed 1,200 iterations per revision. No new defect. |

## Remaining Acceptance

This file remains an evidence record and is not a completion receipt. The
artifact, checks and receipts above bind to `add532e`. A later commit that
only changes this file does not change daemon behavior. Goal acceptance
needs the owner to accept this evidence, the kept P1 boundary, and the two
fail-closed harness jobs.

The owner initially chose on 2026-09-26 to retain the original pidfd-owned
Linux process-group boundary, then on 2026-09-27 authorized a design revision
to cover descendants in new sessions with strong UID-separated isolation.
The positive Linux Pi and OpenCode jobs remain required and red on the last
completed hosted subject; recording `observed_escape` as an expected failure
does not satisfy those gates. The PR remains independently unmergeable until
the revised owner is implemented and verified. Goal 0006b remains active, and
its goal file must not be archived yet.

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

Rechecked against the latest upstream releases on 2026-09-26: the CI pins
remain the current releases, pi `v0.87.1` and OpenCode `v1.18.32`. Pi's
[tagged built-in bash tool](https://github.com/earendil-works/pi/blob/v0.87.1/packages/coding-agent/src/core/tools/bash.ts#L90-L101)
sets `detached: process.platform !== "win32"`; its documented
[shell settings](https://github.com/earendil-works/pi/blob/v0.87.1/packages/coding-agent/docs/settings.md#L120-L126)
choose the executable or command prefix, not the spawn flag. OpenCode's
[tagged process spawner](https://github.com/anomalyco/opencode/blob/v1.18.32/packages/core/src/cross-spawn-spawner.ts)
defaults an omitted `detached` flag to true on POSIX, and its
[Git caller](https://github.com/anomalyco/opencode/blob/v1.18.32/packages/opencode/src/git/index.ts)
does not override it. There is no verified setting in either pinned binary
that preserves the same positive native tasks while keeping those children in
the original Unix group. Replacing the clients' tool executor or changing
the tests is not equivalent evidence and was not authorized.

The smallest path retaining the original group-only boundary and both positive
gates would require separately released, verified versions of **both** clients
that run the same shell/Git children without detaching; neither pinned/latest
release exposes that mode. The owner instead authorized a Linux ownership
amendment on 2026-09-27. Until that amendment's stronger boundary, crash
witnesses, and required positive gates are proven, these failures are not
permission to declare Goal 0006b complete.

Two historical failures on related jobs were not containment escapes:

- The Pi E2E test (`control/test/symmetry_control/pi_control_e2e_test.exs`
  around lines 4257 and 4440) formerly validated the Linux v1 identity
  `linux:<boot>:<pid>:<start>` while the daemon persisted v2. The current test
  now validates and reconstructs `linux:v2:<boot>:<pidns>:<pid>:<start>`;
  that earlier mismatch is not the present hosted failure.
- The Windows Pi job's `pg_ctl start -w` previously hung until the job was
  cancelled. This setup issue was fixed; the Windows Pi E2E job passed
  on the security-only `7fe362a` hosted run.

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

## Termination-Error Publication Race (2026-09-25)

The pull-request CI run `36047218329` (subject `9830fe6`, daemon job,
2026-09-24) failed `TestTerminateFallsBackToRootKillWhenForcedContainmentFails`
once: the result had `Terminated=true` with `TerminationError=nil`. The old
`terminateTree` recorded the forced containment failure only after the root
kill. The kill unblocks `command.Wait`, so `waitAndComplete` could take
`terminationMutex`, snapshot the still-nil `terminationError`, and publish the
result while `terminateTree` was between the kill and the record.

The spontaneous failure did not reproduce locally: the unmodified test passed
1,200 `-race` iterations per revision on `fcebf72` and on `add532e` (200
single runs, then 10 batches of 100). A temporary synchronization test that
makes the root kill unblock `Wait` and wait for result publication before
returning failed 10 of 10 `-race` runs on `fcebf72` with `Terminated=true` and
`TerminationError=nil` and passed 10 of 10 on `add532e` with the
forced-containment error present. The fix records the containment error
before the root kill; the kill error is recorded separately and
`os.ErrProcessDone` stays ignored. After the fix the full
`go test -race ./internal/execution/ ./internal/app/` passed in WSL and the
Windows `./...` suite passed. `recordTerminationError` joins errors
append-only, so the published result cannot lose the containment error or
duplicate the kill error.

## Goal 0006b Continuation (2026-09-26)

This is a local, **uncommitted** implementation and verification record, not
an acceptance or completion receipt. The checkout has base
`491b2222a41a25699bd8ccf7b7a678d46c2af583` and committed HEAD
`c4befecb882f0bdb7545ae1023e1399b8e81a978` (security-only followed by
the isolated CI cgroup capability probe; Linux production witness provenance
below predates these workflow-only commits).
The code and workflow diff
under `daemon/` and `.github/workflows/ci.yml` has SHA-256
`9c38624faaa6100235b10a6217f46296a54460fb26394476a6b95384b28a6a91`;
the new `scripts/cleanup-linux-pi-processes.sh` has SHA-256
`7d3849433ad0289bb288b2bfa44e205922626d2eb82e437fd706318cbf8eeb5a`.
These two hashes, not the prior Git HEAD/tree in the witness metadata,
identify the dirty implementation. They must be replaced by an immutable
commit and exact-subject hosted evidence before acceptance.

The Linux helper and daemon mirror now require a durable, identity-fenced
scan begin before reading `/proc`, a clean result before retirement, and an
irreversible clean seal before a stop receipt. A failed scan, unknown write,
or unfinished scan cannot be repaired by an absent PID or group. The helper
authenticates terminal scan proof across daemon restart only when the dead
daemon's mirror already had a durable seal. An ordinary daemon SIGKILL with
an unsealed mirror stops the original group but retains the journal without a
stop receipt or release. Positive restart recovery is demonstrated at the
distinct durable-seal boundary; neither case is called universal recovery.
The old Linux helper journal format still loads, but missing scan provenance
is not promoted to a new stop receipt.

Local production-binary witnesses ran against a PostgreSQL-backed Control
release, Go 1.27.0, and WSL2 Linux kernel 6.18.40.1. With
`SYMMETRY_PRODUCTION_LINUX_CONTAINMENT_WITNESS=1`, both negative and replay
witness flags enabled, `go test -count=1 -timeout 900s ./e2e -run
'^TestProductionLinuxContainment(WitnessSpine|NegativeWitness|ReplayWitness)$'
-v` passed all three top-level tests without skips at this worktree state.
Redacted per-case snapshots, crash statuses, provenance, and daemon logs were
copied to `/tmp/symmetry-0006b-redacted-final/` (75 files, no raw journals).
Raw test journals remain machine-local until their scratch roots are removed;
they must not be published. The production daemon binary SHA-256
was `8b03e4f161289c1bcb3de4494d1fbd3e0df3e2e527466555abb1fe9d492ab9e6`.
The spine, negative, and replay witness source SHA-256 values were respectively
`77d049d93c43f5233b2dc7d220d4c651eecf38eb371ae496e27574c2e72ceb87`,
`8d85f86dd879d150c33ade8c619367748be310d638f6e778f1339d6349b75035`,
and `4fa13282ba7fa02aebd21631a8b28f767f33c625b34ad3feb9e778ec19336bce`.

The native Linux test `TestLinuxPIDReuseContainmentRejectsReusedPID` passed in
a private user/PID namespace: the numeric PID and process-group ID were
reused with a different start time while the old pidfd remained retained;
the stale pidfd returned `ESRCH` and the replacement stayed live. A separate
production-binary case passed inside a private user/PID namespace with the
network namespace unchanged. It killed the first daemon after an exec-stopped
lease-armed target and durable mirror seal, waited for the surviving helper's
post-reap/pre-proof barrier, reused the target's numeric PID/PGID with a later
start time, and observed the helper's `identity_or_anchor_mismatch` proof
category. The restarted production daemon sent an exact authenticated request
to that helper; its journal remained unreceipted and uncleared while the
replacement stayed live. The nested case fails rather than skips if its
private PID namespace, allocator, or pidfd capability is unavailable. This
is **local** production evidence, not proof that the hosted CI runner has the
same namespace capability. The production wrong-session helper-hello case
passed: a plausible transmitted session disagreed with the live procfs
anchor, and the target never resumed; the unresolved precommit scan stayed
durable after exact physical abort proof. Lost stop and release responses
replayed the same durable receipt before helper release and journal clear.
The production spine also dropped a real Control `SubmitTask` response
after acceptance and replayed the exact same idempotency key/body. The
original Task was recovered without duplication. On Windows, the production
crash witness passed
prepare, bind, commit, lease, resume, stop,
receipt, release, and clear, and the wrong-secret first named-pipe client
was rejected before the legitimate hello on the same absolute deadline.

The local Windows `go vet ./...` and `go test -count=1 -timeout 600s ./...`
passed. Linux `go vet ./...`, `go test -count=1 -timeout 600s ./...`, and the
full `CGO_ENABLED=1 go test -race -count=1 -timeout 900s ./...` passed during
this continuation; the final witness-test-only cleanup change was separately
covered by `go test -race ./e2e` and the production witness matrix above.
The CI cleanup's extracted pidfd-only Bash script passed syntax and a real
Linux fixture: a matching original process group and child were stopped,
while a forged start time was rejected without signalling either process.
The fixture and required Pi/OpenCode positive tests remain enabled in CI.

**Open acceptance:** hosted push checks on `cfb0155`, the security-only
`7fe362a` commit and the `c4befec` capability-probe commit fail
`pi-control-e2e-linux` and `opencode-native-synthetic` on observed out-of-group
children. Pinned native clients have no verified setting that keeps these
children in the original group. The owner authorized an expanded strong Linux
boundary; silently skipping, weakening, or relabeling the required positive
gates would not make this PR independently mergeable. The uncommitted
worktree has no hosted exact-subject CI run, including for the new literal
PID-reuse production witness. The completed `7fe362a` security-only CI run
cannot validate the uncommitted containment implementation or
provide case-level evidence: that commit intentionally exports provenance
only. Clarification of Owner Contract points
4 and 8 is required if they were intended to guarantee release/clear after
*every* daemon crash, including an unsealed or failed in-flight scan: a dead
mirror cannot durably attest what it saw before its owner died. Neither the
fixed design nor the active Goal is silently amended. The Goal remains active;
do not archive its goal file until all required evidence and gates are met.

## Owner-authorized Linux v1.2 Scope and Host Probe (2026-09-27)

The owner authorized replacing the **new-run** Linux group-only verified
boundary with an ownership boundary covering detached native descendants.
The required isolation is strong: daemon and helper retain their own host UID
and exclusive delegated cgroup authority; native CLI and all descendants use
a distinct unprivileged host UID. The operator provisions that CLI UID's
HOME/credentials and access to only the selected workspace/Git metadata.
It must not receive write access to the daemon journal, recovery socket, or
manager/ancestor/sibling cgroups. Neither legacy Linux journals nor Windows
receipts may silently become new cgroup stop receipts.

The owner also authorized an isolated GitHub CI capability probe, with sudo
confined to a temporary `systemd-run` unit using `Delegate=yes` and `User=`.
Probe-only commit `c4befecb882f0bdb7545ae1023e1399b8e81a978` was pushed
to PR #9. Its push run `36260565585`, job `108455434680`, **passed** on
Ubuntu 24.04.5, kernel 6.17.0-1022-azure: the unprivileged runner service
reported UID 1001, created an exclusive leaf below its delegated systemd
unit, placed a child in that exact leaf at birth using Go 1.27
`UseCgroupFD`/`CgroupFD` plus `PidFD`, and read `cgroup.events` with
`populated 0` after child exit. The two required Pi/OpenCode Linux jobs still
failed on `observed_escape` because this commit is a probe, not the revised
containment implementation.

Local WSL checks independently created/removed an exclusive cgroup-v2 leaf
and placed a child in it with the same Go launch fields. A separate *root*
manager probe launched UID 65534 with ptrace into a root-owned leaf; the
child could not write the parent `cgroup.procs`, and the exact leaf was removed
after exit. Root's ptrace capability makes this **insufficient** evidence for
the real nonroot daemon/helper UID 65532 launching a different UID. The
hosted probe also used the same UID for parent and child; it did not prove
the owner-required cross-UID launch, strong migration denial, native CLI
credential/workspace access, or recovery after daemon/helper failure.

The owner selected a separately installed, root-owned **transient launcher**
restricted to the configured CLI UID, rather than granting UID-switching
capabilities to the long-lived daemon. A throwaway WSL probe used A UID 65532
with no effective capabilities, a test-only root-owned setuid launcher, and B
UID 65534. The launcher placed a trusted B gate in A's exclusive cgroup leaf
and transferred its pidfd; after the launcher exited, A's subreaper adopted
the same B PID. B could not open the A-owned parent `cgroup.procs` for writing,
waited before native exec, execed a trusted completion binary in the same PID
only after a one-shot release, and exited without native exec on A-gate EOF.
The exact leaf reached `populated 0`; the test-owned setuid binary, scratch
directories and cgroup were removed. This is a **local feasibility probe**,
not a safe production launcher: a Go B gate may be dumpable before it can
restrict same-UID ptrace, and no hosted cross-UID, native credential/workspace
or restart-crash proof has been run.

A Linux v1.2 implementation contract is still pending the root launcher's
fixed-UID authentication and installer policy, a reviewed strong pre-exec
gate, hosted cross-UID/migration-denial evidence, distinct-UID native
credential/workspace verification, per-run cgroup identity and journal CAS
migration, and exact stop/release/clear crash witnesses. Existing
`compose.yaml` grants no
delegated cgroup to its rootless daemon and `compose.production.yaml` has no
daemon service; production delegation and CLI-home provisioning are explicit
operator prerequisites, not silently assumed runtime capabilities. This
section records approved **scope and measured prerequisites**, not a
completed v1.2 design amendment or accepted Goal.

A separate throwaway WSL2 Linux 6.18.40.1 TCP peer probe used unprivileged A
(UID 65532, CapEff 0) and distinct nondumpable B (UID 65534). The current
`/proc/B/fd` enumeration fails `EACCES` across these host UIDs. A root-created
user namespace does not help: A also cannot use `pidfd_getfd` there. When A
itself creates a child user namespace and a short-lived privileged parent maps
**only** host B into that namespace, A remains unprivileged on the host but
can inspect B's `/proc/B/fdinfo` and duplicate a socket from the exact B
pidfd via `pidfd_getfd`; the measured duplicate matched the established
IPv4 TCP inode, tuple and network-namespace cookie. The probe's scratch
files were removed. This proves a conditional kernel permission route, **not**
a hosted native Pi/OpenCode call, secure launcher, cgroup stop receipt or
supported-host privilege policy. The existing peer check must not be replaced
by fdinfo inode alone; the retained launch pidfd, duplicated socket type,
tuple and network namespace must all be checked and an unavailable permission
must fail closed.

Probe-only commit `c4be9404ba8a3f76a979cef3bb961750a3b9bcc9` added a
transient fixed-B UID-map and exact TCP-peer capability attempt to the hosted
CI unit. The 2026-09-27 PR workflow run `36324489426`, probe job
`108634492067`, failed at its first security prerequisite on Ubuntu 24.04.5,
kernel 6.17.0-1022-azure: `fs_suid_dumpable=2`, while the provisional gate
contract required `0`. The job reported both `capability=not-proven` and
`cross_uid_capability=not-proven`; it did **not** exercise B mapping,
`pidfd_getfd`, the pinned Pi/OpenCode clients or daemon crash recovery. Mode
`2` permits privileged core dumps even though it denies an unprivileged
same-UID ptracer during a credential transition; silently accepting it for
a **B-credentialed pre-release** gate would change the security contract.
The owner instead approved a reviewed **A-credentialed** trusted gate until
durable commit and lease acknowledgement, with B credentials acquired only
after RELEASE. That avoids the pre-release B/core interval without mutating
the hosted sysctl. The cross-UID capability probe must no longer confuse
this old gate prerequisite with the separate userns/pidfd and cgroup tests.

Probe-only correction `4d70c1abdcd5c368a59be4ae3dd4ddba82310e5d`
reached farther on the 2026-09-27 push workflow run `36326222865`, probe
job `108639386946`: UID A (runner UID 1001, host `CapEff=0`) created a child
user namespace and the transient root helper wrote the exact B-only UID map;
the preexisting same-UID cgroup birth proof still passed. The child then
failed `os.setgroups([])` with `EPERM` **after** its UID map made host A
unmapped, before the B GID map or TCP peer probe. The job remained red with
`cross_uid_capability=not-proven`; this is a mapping/clear ordering bug, not
evidence that B's exact TCP peer succeeded. Clear or prove empty supplementary
groups before mapping B, then deny future `setgroups` and write only B GID.

Probe-only group-order correction `43f0dbe7e770bffbe95cdd3fcfa4067f27d12e49`
ran in the 2026-09-27 push workflow `36327391389`, job `108642665625`,
and rejected its next host precondition: runner A had **five** supplementary
groups and no unprivileged way to clear them before the new user namespace.
It reported `stage=host-supplementary-groups-not-empty` and
`cross_uid_capability=not-proven`; B's GID map and exact TCP peer were not
reached. Linux forbids `setgroups` in the new namespace before its GID map.
The owner authorized a **temporary, in-unit root group-clearing gate probe**
before A creates the namespace, with no persistent installer or global host
setting change. This extends only the disposable hosted capability check; the
production static gate and installer still need their own security proof.

In the separately authorized disposable Ubuntu 24.04 WSL2 systemd host,
`Delegate=yes`, `DelegateSubgroup=manager` and `User=A` made the delegated
service parent and `cgroup.procs` A-owned while the **parent's** `cgroup.kill`
remained root-owned mode `0200`. The first scratch leaf was created by root
and then its directory alone was chowned to A; its kill control correctly
remained root-owned. A follow-up created the leaf **as UID A** under the same
delegated parent: both the new directory and its `cgroup.kill` were UID A,
and the kill control was owner-write mode `0200`. Thus A's actual `CreateLeaf`
path needs no privileged per-leaf chown. The parent kill control must remain
root-only so A cannot kill manager/helper/sibling runs. Before binding the
created leaf, A must open the exact pinned leaf's kill control as its owner,
verify write access, then persist the same leaf identity. A root-created leaf
subsequently chowned only at the directory is **not** an equivalent fixture;
it fails closed. An earlier proposal for a transient root grant-only operation
was withdrawn after this creator-UID measurement and is not an accepted v1.2
privilege requirement.

Probe-only temporary group prep `85a7968511f0b499ca205b361d11b5d29e380e62`
ran on the 2026-09-27 push workflow `36332836155`, job `108657953228`.
The transient root stage cleared all five runner supplementary groups before
dropping back to A; A's CapEff stayed zero, the leaf birth probe passed, and
the exact B-only UID/GID maps and `setgroups=deny` were written and verified.
The **next** step failed with `fork/exec /proc/self/exe: operation not
permitted` while the mapped A namespace worker attempted to spawn a *new* B
child using Go `Credential` plus `PidFD`. The run remained red with
`cross_uid_capability=not-proven`; it never reached host-A `pidfd_getfd` or a
native Pi/OpenCode call. This may be a Go fork/credential policy rather than
the approved **same-PID A-gate-to-B** kernel path; determine the exact EPERM
source before altering the probe or asserting host support.

An uncommitted disposable Ubuntu 24.04 WSL2 scratch run used an actual
`Delegate=yes`/`DelegateSubgroup=manager` systemd service as A, an A-created
leaf, the installed root:A-GID setuid C wrapper, and the root-owned static
gate image. The wrapper cleared supplementary groups; a root-core READY
frame reached A before gate creation. Go 1.27 adds `GOTRACEBACK=none` to the
root-core environment under `AT_SECURE`, so that one fixed value must be
accepted alongside the wrapper's explicit `PATH` and `LANG`. A's procfs stat
of the root launcher's PID namespace returned the restricted proc symlink's
inode rather than the namespace inode; READY now checks the root's PID/start
against its retained pidfd and the root core's reported namespace against
A's own PID namespace before recording the producer.

This scratch test still **fails before native RELEASE**. After the root core
created the parked A gate in the leaf, A's
`stat(/proc/<gate-pid>/ns/user)` returned a proc symlink inode rather than
the gate's actual user-namespace inode reported by root. A separate cross-UID
observation on this host found root's `stat -L /proc/1/ns/user` returned
namespace inode `4026531837` with mode `8124` (hex), while A's stat returned
symlink inode `1440238` with mode `4149` (hex); A's readlink produced an
empty result. The installed-gate test therefore retained an unresolved launch
and failed closed at `verify parked A gate`; it did **not** prove B execution,
Pi/OpenCode, or post-B `pidfd_getfd`. A nonzero proc symlink inode is not a
namespace identity and cannot justify v1.2 peer acceptance. The pending
implementation rejects non-nsfs stat targets. No production or hosted positive
evidence is claimed for this dirty scratch subject.

An additional temporary C-only capability probe ran in that same disposable
WSL2 distro under a short-lived root-mapping systemd unit. Root wrote only
the B-only UID/GID maps and exited before post-B inspection; A was UID/GID
60000 with CapEff=0 and held the exact child pidfd; the child dropped in the
same PID to nondumpable UID/GID 60001, opened a live loopback TCP socket,
then A independently inspected it. A's `/proc/<B>/ns/{user,pid,net}` stats
returned proc symlink/directory inodes and empty readlinks, not nsfs targets.
`/proc/<B>/fdinfo` returned `EACCES`; exact `pidfd_getfd` for the known live
socket returned `EPERM`. A's `pidfd_send_signal(SIGKILL)` still killed the
exact child and A reaped it. This is a real capability denial for this WSL2
kernel, **not** a production B launch or native positive. The owner explicitly
amended support to require real nsfs/procfs and exact `pidfd_getfd` access as
well as systemd delegation; this kernel must reject native starts and supply
a fail-closed witness instead of a positive WSL2 claim. A separately capable
WSL2 host, if available, must pass the same native positive matrix as Linux.

### Proposed v1.2 owner contract — pending host and security proof

This proposal changes **new** Linux executions only. Existing v1 pidfd-group
journals retain their original reader, stop rules and fail-closed uncertainty;
no v1 scan seal or group receipt can authorize a v1.2 cgroup release. Windows,
Control's run fence, leases, terminal policy, and wire protocol remain
unchanged. A new versioned journal owner must be rejected, not partially
interpreted, by a binary unable to prove its cgroup stop. Rollback therefore
requires draining v1.2 owners or retaining a compatible recovery binary;
reverting the executable while live v1.2 journals exist is not a safe rollback.

The proposed **supported-host service topology** has two independent systemd
units, both unprivileged UID A: an always-running, `Delegate=yes` owner anchor
with a manager subgroup and one persistent exclusive delegated parent, and
a separate daemon unit with an ordinary control-group kill policy. The anchor
only provisions the parent and forks authenticated per-run helpers into its
manager subgroup; it does not claim work, renew leases, decide run transitions
or write a second journal. It is an ancestor subreaper for those helpers:
after exact helper death it adopts and reaps run-owned children, returning
identity-bound wait evidence to the daemon mirror. It cannot turn an `ECHILD`
or an absent endpoint into a positive wait witness. Per-run helpers remain
the normal authority under the same existing RunJournal, and B executes only
in a run leaf outside the manager subgroup. The daemon obtains its
independently pinned leaf FD and
exact helper identity through authenticated local bootstrap. `BindsTo=` with
`After=` stops the daemon on unexpected anchor loss, **not vice versa**:
daemon SIGKILL/restart must not terminate the anchor or its helpers. An
unexpected anchor failure needs a live-child stop witness; manager killing
the unit cannot be used as a journal receipt. Avoid `KillMode=process`/`none`,
which leave work running after a manager stop.
An explicit anchor stop first stops the dependent daemon; a bounded
recovery-only command must acquire the same journal lock, fence producers,
and persist only receipts it can actually prove before the anchor unit is
killed. Timeout, failed startup or a correlated owner kill may leave old
markers unresolved and require operator recovery; an empty or replaced unit
cgroup cannot manufacture a receipt. The unit definitions, stop ordering and
helper survival remain unproved until exercised on a real systemd host: the
available WSL distro runs `/init`, not systemd. Before changing the old
daemon unit layout, drain live v1 helpers through their **v1** stop receipts
and release. An unproved v1 marker blocks cutover instead of being rewritten
as v1.2; retain the v1 reader for stopped or receipt-bearing legacy owners.

1. An operator grants daemon/helper UID A one persistent, exclusive cgroup-v2
   delegated parent, with a manager leaf **outside** every run leaf. Native UID
   B has no write access to this parent, its control files, any sibling or run
   leaf, the daemon journal, or the recovery endpoint. B's HOME/session files
   and a separate B-writable per-run checkout root are operator-provisioned.
   The owner approved a new-Linux-v2 exception to the default linked-worktree
   policy: after durable handoff and initial lease acknowledgement, B creates
   its own standalone clone from an A-owned, B-readable seed. A exclusively
   controls source Git objects/refs/config/hooks, the seed, and any accepted
   object promotion; it never invokes Git against B-owned configuration or
   treats B's `.git` or checkout marker as ownership proof. B must not write
   code or Git configuration consumed as executable instructions by A. The
   checkout-ready frame comes from B **after** the authorized A-to-B RELEASE;
   it is a B-origin progress claim, not an A-certified immutable or clean
   checkout. A binds only the exact grant, seed, target inode and durable CAS
   before acknowledging the intended CLI exec; A imports and verifies a
   proposed commit independently after physical stop.
   A fixed B UID shares its HOME/credentials and
   filesystem permissions across all native runs; a cgroup leaf is **not** a
   per-run filesystem confidentiality boundary. The root-owned transient
   launcher is callable only by A, pins one operator-configured B UID/GID,
   authenticates the inherited local launch channel, and may not become a
   scheduler, lease owner, or persistent process authority. Its installed
   image and dependencies must
   be root-owned and not B-writable; its privileged phase uses a fixed safe
   cwd, minimal fixed environment and umask, an explicit FD allowlist, and
   no shell, Git hook, config or path controlled by B. A missing delegation,
   UID boundary, launcher, or CLI workspace/credential path fails closed
   **before native code runs**.
   The exact-TCP-peer design requires the trusted gate, while credentialed as
   A and before native RELEASE, to create a per-run user namespace;
   the trusted gate's root phase clears supplementary groups before dropping
   to A, then the A gate proves they remain empty **before** the B-only UID
   map can leave A unmapped. A transient root parent maps **only**
   B's host UID/GID, writes `setgroups=deny` before the B GID map, and
   verifies both maps. Neither A nor host root may be mapped. A and B retain
   the host PID and network namespaces;
   native B must have no retained namespace capabilities. A-owned namespace
   capabilities must not grant B host A credentials or access to A's cgroup
   control files. This is a **one-way** privilege boundary: A owns the child
   user namespace, but procfs/ptrace access to B must be measured on the
   actual host and cannot be inferred from that ownership. B is not trusted
   with A's journal, launcher controls or cgroup authority.
   B's credentials are not confidential from the operator's A process. The
   B-only mapping is preferred over leaving B unmapped, which obscures
   native UID-based Git trust and requires a root `setns` phase.
   The Pi provider extension is A-generated and integrity-checked in an
   A-owned directory outside the private StateDir, searchable/readable by B
   but not B-writable; the shared Git worktree and native B HOME cannot host
   its executable source. Native child HOME and XDG/session paths must resolve
   to operator-provisioned B-owned files, not inherited A HOME. Pi's retained
   session path must remain `stat`-accessible to A for resume without exposing
   the session contents or A journal. The Pi provider nonce and OpenCode HTTP
   password are existing **native application** child environment fields;
   the helper/journal/release authentication secret is never a native child
   environment field. Exact TCP peer ownership must still be checked even
   when a native application bearer is present.
2. A journal mutation keyed by the existing RunKey durably reserves one random,
   immutable run-leaf name before `mkdir`. A second exact-CAS mutation binds
   the opened leaf's boot/mount/delegated-parent/path/device/inode identity.
   Unknown `mkdir`, bind, or write outcomes use exact readback plus a fresh
   successful persistence barrier. Identity fields alone cannot prove a
   recreated cgroup is the original, even if the kernel reuses an inode:
   live-leaf operations require uninterrupted provenance through at least
   one pinned A-owned FD. If all pins are lost, a matching path/stat does not
   restore that provenance; no-receipt owners remain unresolved absent an
   independent exact proof. Pending reservations block journal deletion and
   workspace cleanup even before a target PID exists. A
   reservation with no creation/launch intent may be durably aborted, without
   fabricating a stop receipt, only after its creator is fenced. Once `mkdir`
   may have started, a pre-effect abort requires independent proof that **no
   launcher grant or gate production ever became possible**, every creator is
   fenced, and any existing exact leaf was empty and removed under the pinned
   exclusive parent. An absent path alone cannot prove the leaf never existed;
   it can finish this narrowly proven no-target abort after an unknown `rmdir`
   response and a fresh durable CAS. A launcher that may still act, uncertain
   production, or lost leaf provenance remains unresolved. Outside this
   pre-effect no-target exception, no A actor may remove or replace a reserved
   leaf before its version-bound stop receipt.
3. Helper A and daemon mirror retain separately opened FDs to the exact leaf
   before the launcher can create a gate. The transient root launcher creates
   an immutable, root-owned **static trusted gate** in the run leaf at
   **clone3 birth** (`UseCgroupFD`, `CgroupFD`, `PidFD`); that image executes
   before its credentials become UID A. The gate creates its user namespace
   as A, the transient root parent maps only B, transfers the gate pidfd to
   the helper, and exits. A verifies that launcher's exact exit, adopts the
   gate as a subreaper child, checks its PID/start identity, namespace, FD
   inventory and exact leaf membership, and durably binds the helper/gate
   pair. The journal explicitly records a pre-release **A-gate** target;
   neither the gate nor any native instruction has become UID B. This avoids
   the pre-authority B-credentialed dump/core interval on hosts with
   `fs.suid_dumpable=2`, without changing a host sysctl. The gate takes no
   B-writable code or inputs, holds no journal or A-writable cgroup/control
   FD, and reads a single-use A-owned release channel. Wrong input, EOF,
   owner loss, denied privilege drop, timeout, or stale identity exits without
   executing the CLI. This replaces v1.1's `PtraceDetach` gate **only** for
   v1.2. Every boundary needs an explicit FD allowlist: B never inherits a
   release writer, reader, A-opened journal/cgroup FD or other control FD,
   including through procfs or `SCM_RIGHTS`. A gate has A's host DAC rights;
   its trusted image must never migrate it to an ancestor or sibling cgroup.
   Both its pinned PID and exact leaf membership are rechecked before RELEASE
   and before stop proof; a possible migration, changed identity or unproven
   gate death retains unresolved state even if the leaf is empty. The C-gate
   build, root launcher installer, FD inventory and migration invariants still
   require security review and hosted proof. The launcher is a tracked
   producer from invocation until proved exit, including failed handoff and
   takeover: an empty leaf or gate EOF cannot prove it will not clone later.
   An unidentifiable producer outcome stays unresolved; no stop receipt may
   precede its proved exit or an equally strong producer fence.
4. Existing durable handoff prepare/bind/commit and the initial Control lease
   acknowledgement precede the sole RELEASE. The A gate verifies owner
   liveness, retires its release reader and all A control state, verifies the
   pre-mapping supplementary-group denial, drops real/effective/saved GID/UID,
   capabilities and A
   filesystem credentials, sets no-new-privileges, then `execve`s the approved
   native binary as unprivileged host B in the **same PID and cgroup leaf**.
   After release, verify B host credentials, B-only namespace maps and no
   capabilities, same exact PID/start and leaf membership before accepting
   Pi/OpenCode TCP peer evidence. The helper's owner-loss/lease latch
   serializes with RELEASE: it cannot release after losing authority. If a
   committed release races owner loss or the gate stalls before B exec, kill
   the pinned leaf and prove the exact gate has exited; no late production or
   absent leaf can mint a receipt. A lost release response neither creates a
   second child nor authorizes a second release. An unidentifiable outcome
   remains unresolved until readback; a ready A gate does not prove native
   exec or successful work.
5. Normal helper A stop writes `1` to the pinned leaf's `cgroup.kill`, verifies
   the same subtree's recursive `cgroup.events populated=0`, waits for the
   exact gate/native PID and reaps run-owned adopted children, including other
   sessions and nested child cgroups. If the helper died, the daemon mirror
   must obtain the anchor subreaper's authenticated, run-bound wait/reap
   evidence; the mirror is not an ancestor and `ECHILD` is not reaping proof.
   Stop must first fence both launcher and A-gate producers and the gate's
   release channel, then prove neither producer can create a late child
   before the final kill/empty observation. A daemon mirror may do so only
   after exact helper death; the helper survives ordinary daemon death and
   keeps its authenticated recovery endpoint until the same receipt is
   durable. A missing or replaced
   leaf, unknown kill/write/read result, migration, unprovable child ownership,
   zombie/reap failure, or changed owner identity retains the journal and
   workspace unresolved. `populated=0`, PID absence, or an old group scan alone
   is **not** a stop receipt. No numeric PID/PGID signal is a fallback.
6. Only an exact v1.2 physical-stop proof may be written as a version-bound
   durable stop receipt. After receipt readback, release fences the helper,
   removes only the same proven-empty leaf/subtree, and then clears the exact
   process/leaf marker. Crash before receipt reproves physical stop; crash
   after receipt replays it before release; crash during removal readbacks the
   same leaf and receipt before clear. A prior exact durable receipt plus
   verified absence under the same exclusive parent may finish an already
   authorized removal after a lost `rmdir` response without needing to act on
   a live cgroup. If a path reappears, even with the same recorded inode, or
   the parent identity changes, no unpinned leaf may be killed or removed;
   retain unresolved rather than treating it as the old leaf. Absence cannot
   mint a new receipt.

The owner approved a strictly scoped **production-binary native-owner witness**
for Goal 0006b. Pi and OpenCode currently report `ErrNativeUnverified`; a
normal Goal admission cannot claim their Start/Events/Cancel or model
completion capabilities. The isolated witness must use the real App-owned
RunJournal, helper/gate/clone and native adapters without publishing a false
capability or Goal success. Older synthetic Control integration tests remain
evidence only for their own scope, not substitutes for this A/B owner proof.
Pi's positive provider action must consume an authentic Control-issued claim
and isolated test Integration; a fabricated provider response or token is not
evidence. Acceptance must include real pinned Linux Pi and OpenCode native
owner tests running inside a proved delegated boundary on a
**Linux x86-64/amd64** systemd host. A **WSL2 x86-64/amd64** systemd host
must pass the same positive matrix only when its unprivileged A can prove
real nsfs identities and exact post-B `pidfd_getfd`; the observed denying
WSL2 kernel requires a native-start refusal witness instead. Positives need real
cross-UID broker/provider-action and OpenCode HTTP health calls proving the
exact native TCP peer without granting daemon A broad ptrace privileges or
weakening the existing peer check; the Pi extension must be readable by B
without exposing A's private StateDir or allowing B to replace trusted code.
Peer proof must pin the launch-transferred B pidfd and its user/network
namespace identity, enumerate `/proc/B/fdinfo` candidates, duplicate the
candidate via `pidfd_getfd`, and verify socket type/protocol, exact inode,
tuple and network-namespace cookie before and after the request. FD-info
inode alone, a UID, nonce or port is not exact-peer evidence. Any unsupported
host permission or lost pidfd fails closed rather than bypassing the check.
The check proves that the exact launched PID **holds** the connected socket
at inspection time; it cannot attribute every HTTP byte to that PID if B
deliberately forks or transfers the same FD. B processes share one accepted
native trust domain, and socket possession is not journal authority.
The existing direct-Go-test helper bypass and A-only `0700` fixtures cannot
count as new-owner coverage. Additional witnesses require detached
child/grandchild stop, including a live escape never seen by the old group
scan after the leader is reaped, and migration denial through normal paths,
alternate mounts and procfs. An intentionally leaked A-opened writable cgroup
FD can allow B to migrate despite its UID: prove the native FD inventory
excludes such a leak, and fail launch if that inventory cannot be established.
For the trusted A gate, verify leaf membership and no migration-capable FD
before RELEASE and before stop; an intentionally migrated or unidentified gate
must remain unresolved despite an empty leaf. Kill the helper with a live
detached child, then prove the independent anchor adopted and reaped the
exact run-owned identities before the daemon mirror records its stop receipt.
Also cover owner SIGKILL at every launch/receipt stage and lost
kill/receipt/release responses,
legacy journal replay, PID/leaf reuse, and fail-closed unavailable delegation
or CLI UID. A real native request in the existing rootless Compose daemon
must be rejected before CLI exec, with no group/same-UID fallback and no
fabricated journal receipt; its fake-agent success job is not this witness.
The current hosted same-UID cgroup probe and local transient Go gate do not
satisfy this test matrix. Deployment must also prove that the helper survives
a daemon process failure under its operator service policy;
service-manager termination of **both** owners needs its own bounded cleanup
policy and is not silently classified as a single-owner failure.

