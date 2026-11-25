# rf-acc-migration-pi-hold — evidence record

Goal `rf-acc-migration-pi-hold` (contract: the goal file under the GDD contracts root; conductor
ruling 2026-11-26, diagnosis `findings/environ-root-cause.md` §4). Branch
`refactor/rf-acc-migration-pi-hold`, base `8453da1203f4c109824545b8caee41f9e9696557` (#293).
Plan: `docs/plans/2026-11-26-rf-acc-migration-pi-hold-plan.md`. All paths are relative to the
repository or to the GDD state root. The goal's acceptance row (`migration` opens on a PR-head Bind)
is the conductor's; this record carries the local code, unit and compile rows.

## 1. Delivered files and digests

| file | sha256 at the delivered bytes |
| --- | --- |
| `daemon/internal/app/linux_owner_witness_linux.go` | `a6834897f8a30f62d6bb86f7daff1e8622a90b83780b70f53d08c5f5c77cbafd` |
| `daemon/internal/app/linux_owner_witness_linux_test.go` | `2e68d2493381860b675b4d81b0e4b7f0022fbdde228a6b93902c04ec739e78d2` |
| `docs/plans/2026-11-26-rf-acc-migration-pi-hold-plan.md` | `75455a41e31c497caa73caeca4a96afd2a1ef6f842b166f9903db01f4f8f751f` (record revision; not run-bound) |

`docs/TODO.md` and `docs/archive/CLOSED-TODO.md` are deliberately not hashed anywhere. The
`linux_owner_witness_linux_test.go` row is a recorded scope addition (plan §3.5): the contract's
owned list names the witness source, not its unit test, while the same contract demands that
"mutants pin the new wiring" — only this file can carry a runnable pin for the witness seam, so the
three new tests and their fixture live there and nowhere else.

## 2. Stage 1 — the root cause, named verbatim

Confirmed, with the source chain and the retained log rows the diagnosis cites; the inner turn
error that the diagnosis flagged as unobserved is made observable by a bounded wrap instead of
being guessed.

Retained rows (run `45696662454`, raw 3213-3221; `gdd/native/run-45696662454.log`):

```
witness output:     production_linux_v2_native_witness_test.go:728: owner-witness command exit status: 1
witness output:     production_linux_v2_native_witness_test.go:728: owner-witness stderr: -linux-v2-native-owner-witness: wait for authentic Pi owner witness turn
witness output:     production_linux_v2_native_witness_test.go:728: wait for exact native_release boundary: owner-witness command did not reach its requested durable stage
```

and the stage-marker listing of the same failure context contains no `native_release.json`
(`negative-root/migration/request` lists only directories). Chain, all cited at the base revision:

1. The migration request holds at `native_release`
   (`scripts/ci/linux-v2-native-acceptance.sh:2616-2632`; the request writer's `stage_replay`
   default hold) on the provider-free `pi-durability` vehicle.
2. `native_release` is staged only from the witness's `daemon.start` closure
   (`linux_owner_witness_linux.go:3456,3470,3488` at base), and that closure reaches a harness only
   through `harness.StartRequest.StartProcess`, which the witness wires for codex/claude alone
   (`:3509-3522`).
3. `daemon/internal/harness/pi/adapter.go:81-87,105,175-176` installs its own
   `runnerProcessStarter`, so a pi launch runs `execution.NewRunner()` directly and never calls the
   closure — the durable marker cannot be written. `daemon/internal/harness/opencode/adapter.go:77-83,134`
   is the same shape.
4. The witness therefore ran on to its pi turn and failed there
   (`linux_owner_witness_linux.go:3633-3636` at base), which is the line the retained bounded stderr
   shows. The two `errors.New` calls on that path dropped the inner cause, so the retained log could
   not name it (diagnosis §4 residual, confidence medium-high).
5. The release seam that *is* reachable on every launch path: `execution/linux_owner_v2_linux.go:1073-1077`
   calls `invocation.Callbacks.DropAckAfter(LinuxV2AckStageNativeRelease, …)` immediately after
   `owner.releaseSucceeded = true` (the helper accepted the one-use gate RELEASE), outside
   `owner.mu`. The witness already wraps that callback for its ACK-loss cases and already stages
   `stop_receipt_durable`/`leaf_removed`/`journal_cleared` from the same callback family — which is
   why the pi `stale_leaf` leg reaches its hold while the release hold did not.

Stage-1 instrument: both pi turn failures now wrap their cause (`%w`,
`linux_owner_witness_linux.go:3724-3730`), and the fixture prints a bounded tail of the App's stderr
(`linuxV2ReportOwnerWitnessFailure` → `linuxV2OwnerWitnessStderrBoundedLines`, 20 lines and a
per-line byte bound). With the fix in place the migration case is held *before* any turn, so the
inner error is moot for that case; any pi case that still reaches a turn names it verbatim on the
next guest run. No guess is recorded as a named cause.

## 3. The A-side observation of the released target (measured, WSL kernel 6.18.58)

The fix needs the released same-PID target's own `/proc` facts from the witness's A credential. The
diagnosis could not decide this (`A stat /proc/<pid>/ns/user: EACCES` in its probe), so it was
measured directly, with the gate's real order and map (`c-gate/gate.c:1404-1414` drops to A *then*
`unshare(CLONE_NEWUSER)`; `internal/launchercore/main_linux_amd64.go:961-985` writes `1 <B> 1`).

| shape | reader A: `stat ns/user` | `stat ns/net` | read `environ` | readlink `fd/0` | `pidfd_open` |
| --- | --- | --- | --- | --- | --- |
| parked gate (host uid A, map `1:B:1`, pre-exec) | OK | OK | OK | OK | OK |
| released target (in-ns uid 1 → host B, execveat, NNP) | OK (`newfstatat`/`statx -L`/`get_link`) | OK | EACCES | EACCES | OK |

Discrimination measured on the same kernel: with the user namespace created by **root** instead of
A, A's `stat ns/user` is EACCES (`newfstatat`, `statx -L` and `get_link` alike) — the gate's own
order creates it as A, so A is the namespace owner and the product's `syscall.Stat` reader works.
The `environ`/`fd` denials are the `shared`-set defect (DAC on a 0400 B-owned inode plus the ptrace
uid mismatch), untouched here. The e2e's own capture of the same target already relies on exactly
these A-side reads (`linuxV2CaptureLiveProcess`, `production_linux_v2_native_witness_test.go:2997+`:
`platform.ProcessIdentity`, `PidfdOpen` + live poll, `ns/user`/`ns/net` stat).

Scratch probes (outside the repository, GDD state root `scratch/`):
`probe-ns-pidfd-access.py` (parked/released matrix), `probe-ns-syscall-matrix.py`
(creator-root vs creator-A × `newfstatat`/`statx`/`get_link`), `probe-gate-ns-access.py` (gate
release sequence), plus the verbatim re-run of the diagnosis probe `findings/probe-gate-sequence.py`.

## 4. The fix

`native_release` is staged from the release-ACK callback on every launch path whose adapter owns its
Runner, with the exact released target as evidence:

- `linuxV2NativeWitnessRun.startProcessStagesRelease` is set once in `launchNative` from
  `linuxV2NativeWitnessStagesReleaseFromStartProcess(kind)`, the same predicate that wires
  `startRequest.StartProcess` — one source of truth for "this adapter hands the witness the exact
  Process". Codex/claude behavior is byte-identical (their release stays staged from `daemon.start`
  after the peer pidfd/namespace checks).
- `callbacks.DropAckAfter` now stages the release when `!startProcessStagesRelease` (plain hold) or
  when the request selected that stage (ACK-loss), after
  `captureReleasedNativeTarget` publishes the target evidence; the ACK-loss drop
  (`ErrLinuxV2MutationAckLost`) and the `ownerEffectAlreadyAckedOnEntry` replay skip are unchanged.
- `captureReleasedNativeTarget` requires, fail-closed: the fenced RunJournal; the process marker,
  `ContainmentAuthority.Target*` and `ContainmentCgroup.Gate.Process` to be one and the same
  same-PID target; `platform.ProcessIdentity(pid)` equal to the journal identity before and after
  `unix.PidfdOpen(pid, 0)`; the pidfd live (`Poll` revents 0); `stat /proc/<pid>/ns/user` to be an
  exact nsfs inode (new `linuxV2NativeWitnessUserNamespace`); and `linuxV2NativeWitnessNetworkNamespace`
  (the target must retain the host network namespace). It then publishes `witness.peer` (pidfd +
  user namespace, closed by the existing `closeCapturedFDs`/defer paths) and `summary.Target`, so
  every later stage marker and the replay response inherit the evidence through
  `fillSummaryFromJournal`/`restoreStageMarkers`.
- Marker evidence at the held boundary (the e2e predicate
  `linuxV2MarkerHasNativePeerProof`, `production_linux_v2_owner_cli_test.go:1083-1090`, field by
  field): `Target.PID`/`Identity` = journal marker = authority = gate; `PIDFDVerified` =
  the verified pidfd; `HostUID` = the installed B uid; `SamePIDExec`; `UserNamespace` =
  `/proc/<pid>/ns/user`; `NetworkNamespace` = `/proc/<pid>/ns/net` (host namespace proven);
  `Owners.Gate` = the same target. No e2e validator, fixture byte or `pi/**` file changed.

Recorded behavior consequence, not hidden: the opencode health leg (frozen bytes, fixed
`native_release` hold) would now also stage a release hold if it reached its release; it aborts
earlier on its own pinned-version gate, so no observed row changes. Rejected alternatives and their
reasons are in plan §3 (a Pi-only special case would encode the defect's shape rather than the
launch-path fact; changing `pi/**` is not owned; staging without the namespace/pidfd evidence would
be rejected by the e2e marker contract).

## 5. Gate rows (commands, working directory, exit)

All commands run in WSL `Ubuntu-24.04` (`/mnt/q/repos/wt-acc-migration-hold/symmetry/daemon`,
`CGO_ENABLED=0`, `GOFLAGS=-mod=mod`), on the delivered bytes:

| gate | command | exit | result |
| --- | --- | --- | --- |
| format | `gofmt -l .` | 0 | no output |
| vet (unit) | `go vet ./...` | 0 | no output |
| unit suite | `go test ./internal/app/ -count=1` | 0 | `ok github.com/wxxb789/symmetry/daemon/internal/app 135.187s`, on the §1 delivered bytes |
| focused witness tests | `go test ./internal/app/ -run 'TestLinuxV2NativeWitness' -count=1` | 0 | `ok …` (includes the three new tests) |
| vet (e2e compile) | `go vet ./e2e/` | 0 | no output |
| e2e compile of the case | `go test ./e2e/ -run TestProductionLinuxV2MigrationDenialRetainsJournal -count=1` | 0 | `ok …` (the witness opt-in skip; no guest here) |

## 6. Mutant rows

Each mutant was applied to the delivered source, run against the new tests, and reverted; the final
tree equals §1.

| # | mutant | expected | observed |
| --- | --- | --- | --- |
| M1 | the release callback reverted to its pre-fix body (`request.DropAckAfter != stage` gating only) | the runner-path hold test reddens | `TestLinuxV2NativeWitnessRunnerReleaseHoldStagesExactTarget`: `release callback returned before its held stage: <nil>`; `TestLinuxV2NativeWitnessStartProcessReleaseKeepsItsOwnStage`: the ACK-loss marker carries `PIDFDVerified:false`, zero namespaces (`FAIL`, exit 1) |
| M2 | `captureReleasedNativeTarget` publishes no `witness.peer` (no verified pidfd) | the evidence assertions redden | `release witness evidence = (*execution.LinuxV2Peer)(nil)/…` (`FAIL`, exit 1) |
| M3 | `linuxV2NativeWitnessStagesReleaseFromStartProcess` returns true for every kind | the launch-seam table reddens | `witness stages the release from daemon.start for "pi" = true, want false` and the same for `"opencode"` (`FAIL`, exit 1) |

M1 also shows the ACK-loss release marker gaining the exact evidence the e2e predicate requires
(`PIDFDVerified`, `UserNamespace`/`NetworkNamespace`, `Owners.Gate`), which is the same class the
plain hold needs.

## 7. Per-clause proof table

| contract clause | proof |
| --- | --- |
| 1 root cause named verbatim, source + log | §2 rows and line cites; the diagnosis is confirmed, not refuted; the inner turn error is instrumented (bounded wrap) rather than asserted |
| 2 the fix reaches the hold on the production path | §4; the hold engaging is pinned by `TestLinuxV2NativeWitnessRunnerReleaseHoldStagesExactTarget` (marker observed while the callback still blocks, released through the bounded release-file protocol); the guest Bind row is the conductor's |
| 2 no `pi/**` change, no new provider access, no second authority | `git status`/`git diff` scope (§1 files only); the release ACK is the existing App-owned seam; no new store, scheduler or provider path |
| 3 records name the root cause verbatim | §2 |
| Invariants (denial/retention assertions, ≤2-line comments, `go vet`, host-layout independence) | no e2e validator or fixture byte changed; every added comment is ≤2 lines; §5 rows; the new test reads `/proc/self/ns/*` and the journal, never a host layout |
| Acceptance | carried: the conductor's Bind runs `migration` once on the PR head; its next stop or green is recorded there |

## 8. Residual risks and limits

- The conductor's Bind is the only guest proof; this host has no privileged Linux guest, so the
  delivered rows are local (unit, vet, compile) plus the real-kernel probes of §3.
- The `launchNative` assignment line (`witness.startProcessStagesRelease = …`) is not pinned by a
  local test: building a `launchNative` fixture (registry, workspace, clone grant, owner) is a large
  surface no owned test provides. Its loss would be caught in the guest (codex/claude would stage
  the release twice); M3 pins the predicate it calls.
- Evidence strength versus the codex path, stated rather than hidden: codex takes the user namespace
  from a launch-transferred pidfd whose nsfs value was cross-checked with the root launcher; the
  runner path takes A's own nsfs observation of the same namespace and a `PidfdOpen` verified by
  identity before and after. Both are bound to the helper's accepted one-use RELEASE for that exact
  gate PID/identity, and the e2e independently re-verifies the target's credentials, pidfd, command
  line, leaf membership and clone at the same boundary.
- `fillSummaryFromJournal` overwrites `Target.PIDFDVerified` from `witness.peer` (the codex path has
  the same property), so the literal in `captureReleasedNativeTarget` is redundant there by design;
  M2 pins that the peer publication is what carries the evidence.
- The intermittent aborted-release measurement note and the recovery-decision residuals of
  `stale_leaf`/the stale-journal sets stay tracked in their own entries; this goal only unblocks the
  release hold they sit behind.

## 9. Retained artifacts

- Plan `docs/plans/2026-11-26-rf-acc-migration-pi-hold-plan.md`; this record.
- Scratch probes in the GDD state root `scratch/` (§3), and the verbatim re-run of
  `findings/probe-gate-sequence.py`.
- Retained run log `gdd/native/run-45696662454.log` (raw 3213-3221, 3784+) as cited by the diagnosis.
