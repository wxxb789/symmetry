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
| `daemon/internal/app/linux_owner_witness_linux.go` | `d73c42af80d54571d3eb3cff52448c34b22970093dc1e090764421870050af17` |
| `daemon/internal/app/linux_owner_witness_linux_test.go` | `1882d4af5fdafa1d14ef7a5611ee2dc12a5856405b7c8cbdd4fa223f82a33dfd` |
| `docs/plans/2026-11-26-rf-acc-migration-pi-hold-plan.md` | `d45e1a7c4f079d741d5deb4b286efbe3dd3e1d49f0c3000b1d2fc45738fea13b` (record revision; not run-bound) |

`docs/TODO.md` and `docs/archive/CLOSED-TODO.md` are deliberately not hashed anywhere. The
`linux_owner_witness_linux_test.go` row is a recorded scope addition (plan §3.5): the contract's
owned list names the witness source, not its unit test, while the same contract demands that
"mutants pin the new wiring" — only this file can carry a runnable pin for the witness seam, so the
new tests and their fixture live there and nowhere else.

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
   (`scripts/ci/linux-v2-native-acceptance.sh:2616-2626`; the request writer's `stage_replay`
   default hold) on the provider-free `pi-durability` vehicle.
2. `native_release` is staged only from the witness's `daemon.start` closure
   (`linux_owner_witness_linux.go:3456,3470,3488` at base), and that closure reaches a harness only
   through `harness.StartRequest.StartProcess`, which the witness wires for codex/claude alone
   (`:3509-3522`).
3. `daemon/internal/harness/pi/adapter.go:81-87,105,175-176` installs its own
   `runnerProcessStarter`, so a pi launch runs `execution.NewRunner()` directly and never calls the
   closure — the durable marker cannot be written. `daemon/internal/harness/opencode/adapter.go:73-79,204,213`
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
`linux_owner_witness_linux.go:3720,3724`), and the fixture prints a bounded tail of the App's stderr
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

- The release-seam decision is made inside the `DropAckAfter` wrapper, which re-derives the kind
  from `witness.request.Harness` (fix-stage refresh, STD-A02) through
  `linuxV2NativeWitnessStagesReleaseFromStartProcess` — the same predicate that wires
  `startRequest.StartProcess`, one source of truth for "this adapter hands the witness the exact
  Process", with no launch-time store to lose. The generated codex/claude rows are unchanged (their
  plain release hold stays staged from `daemon.start` after the peer pidfd/namespace checks and the
  callback no-ops); the one
  validator-permitted configuration that differs is a `native_release` ACK-loss drop on a
  `daemon.start`-wired adapter, where the ACK-loss capture now publishes the released target's
  evidence too (`TestLinuxV2NativeWitnessStartProcessReleaseKeepsItsOwnStage` pins it), and no
  generated row is affected because the ACK-loss selectors are built from the pi-only
  `pi-durability.json` template (`scripts/ci/linux-v2-native-acceptance.sh:1314-1329`).
- `callbacks.DropAckAfter` now stages the release when the request harness derives the runner seam
  (plain hold) or when the request selected that stage (ACK-loss), after
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
  `linuxV2MarkerHasNativePeerProof`, `production_linux_v2_owner_cli_test.go:1083-1089`, field by
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

All commands run in WSL `Ubuntu-24.04` (`<HOST-PATH>/daemon`, `CGO_ENABLED=0`, `GOFLAGS=-mod=mod`),
on the §1 delivered bytes (§10 records the apply-stage re-run on the same digests):

| gate | command | exit | result |
| --- | --- | --- | --- |
| format | `gofmt -l .` | 0 | no output |
| vet (unit) | `go vet ./...` | 0 | no output |
| unit suite | `go test ./internal/app/ -count=1` | 0 | `ok github.com/wxxb789/symmetry/daemon/internal/app 154.349s` |
| focused witness tests | `go test ./internal/app/ -run 'TestLinuxV2NativeWitness' -count=1` | 0 | `ok …` 1.252s (includes the five new tests: the three implement-stage pins plus the two apply-stage pins) |
| vet (e2e compile) | `go vet ./e2e/` | 0 | no output |
| e2e compile of the case | `go test ./e2e/ -run TestProductionLinuxV2MigrationDenialRetainsJournal -count=1` | 0 | `ok github.com/wxxb789/symmetry/daemon/e2e 0.021s` (the witness opt-in skip; no guest here) |

## 6. Mutant rows

Each mutant is an overlay of the delivered source (never written into the tree, never committed),
run against the final tests; the five release-seam tests are the judged surface, and the final tree
equals §1 (fix-stage refresh, §11).

| # | mutant | expected | observed (exit 1 each) |
| --- | --- | --- | --- |
| M1 | the release callback reverted to its pre-fix body: only `request.DropAckAfter == stage` selects, with no harness derivation and no `captureReleasedNativeTarget` | the runner-path hold, the ACK-loss evidence, the fail-closed rejections and the harness fail-close redden | `TestLinuxV2NativeWitnessRunnerReleaseHoldStagesExactTarget`: `release callback returned before its held stage: <nil>`; `TestLinuxV2NativeWitnessStartProcessReleaseKeepsItsOwnStage`: the ACK-loss marker carries `PIDFDVerified:false` and zero namespaces; `TestLinuxV2NativeWitnessRunnerReleaseRejectionsStayFailClosed`: all four cases `release callback accepted a journal outside the exact committed target`; `TestLinuxV2NativeWitnessReleaseSeamRequiresTheRequestHarness`: `release callback accepted a request without a configured harness` |
| M2 | `captureReleasedNativeTarget` publishes no `witness.peer` (no verified pidfd) | the unit assertion on `witness.peer` reddens; the marker itself still passes | `release witness evidence = (*execution.LinuxV2Peer)(nil)/…PIDFDVerified:true…` at `linux_owner_witness_linux_test.go:1980` — every marker assertion passed |
| M3 | `linuxV2NativeWitnessStagesReleaseFromStartProcess` returns true unconditionally | the launch-seam table and the runner-path rows redden | `witness stages the release from daemon.start for "pi" = true, want false` and the same for `"opencode"`; `TestLinuxV2NativeWitnessRunnerReleaseHoldStagesExactTarget`: `release callback returned before its held stage: <nil>`; the rejections row: all four `release callback accepted a journal outside the exact committed target` |
| M4 | the predicate inverted (pi/opencode only) | the launch-seam table and the daemon.start exclusion redden | all four seam rows (`"codex"`/`"claude"` = false, `"pi"`/`"opencode"` = true); `TestLinuxV2NativeWitnessStartProcessReleaseKeepsItsOwnStage`: the plain hold stages and blocks (`daemon.start release path callback: owner witness barrier cancelled: context deadline exceeded`); the runner-path rows (no staging; four accepted journals) |
| M5 | `request.Harness != value.Runtime.HarnessKind` deleted from request validation | the harness-equality row reddens | `a stage-replay request whose harness differs from the configured launch kind was accepted` (`linux_owner_witness_linux_test.go:113`) |

M1 also shows the ACK-loss release marker gaining the exact evidence the e2e predicate requires
(`PIDFDVerified`, `UserNamespace`/`NetworkNamespace`, `Owners.Gate`), which is the same class the
plain hold needs. M3/M4 are the red rows for the seam decision the fix moved into the callback;
M5 is the red row for the validation equality the derivation relies on.

## 7. Per-clause proof table

| contract clause | proof |
| --- | --- |
| 1 root cause named verbatim, source + log | §2 rows and line cites; the diagnosis is confirmed, not refuted; the inner turn error is instrumented (bounded wrap) rather than asserted |
| 2 the fix reaches the hold on the production path | §4; the hold engaging is pinned by `TestLinuxV2NativeWitnessRunnerReleaseHoldStagesExactTarget` (marker observed while the callback still blocks, released through the bounded release-file protocol); the fail-closed journal rejections and the replay skip are pinned by the apply-stage `TestLinuxV2NativeWitnessRunnerReleaseRejectionsStayFailClosed` and `TestLinuxV2NativeWitnessRunnerReleaseReplaySkipsWithoutStaging`; the seam decision derived from the request harness is pinned by that fixture under M3/M4 and by `TestLinuxV2NativeWitnessReleaseSeamRequiresTheRequestHarness` under M1 (§6, §11); the guest Bind row is the conductor's |
| 2 no `pi/**` change, no new provider access, no second authority | `git status`/`git diff` scope (§1 files only); the release ACK is the existing App-owned seam; no new store, scheduler or provider path |
| 3 records name the root cause verbatim | §2 |
| Invariants (denial/retention assertions, ≤2-line comments, `go vet`, host-layout independence) | no e2e validator or fixture byte changed; every added comment is ≤2 lines; §5 rows; the new test reads `/proc/self/ns/*` and the journal, never a host layout |
| Acceptance | carried: the conductor's Bind runs `migration` once on the PR head; its next stop or green is recorded there |

## 8. Residual risks and limits

- The conductor's Bind is the only guest proof; this host has no privileged Linux guest, so the
  delivered rows are local (unit, vet, compile) plus the real-kernel probes of §3.
- The release-seam decision no longer has a launch-time store (fix-stage refresh, STD-A02): the
  wrapper re-derives it from `witness.request.Harness`, which request validation pins equal to the
  configured `Runtime.HarnessKind` — the same string `runFresh` parses into the launch kind — so
  the release fixture itself pins the decision; M3/M4 (§6) and the pre-fix store-invert baseline
  (§11) are the observed rows.
- Evidence strength versus the codex path, stated rather than hidden: codex takes the user namespace
  from a launch-transferred pidfd whose nsfs value was cross-checked with the root launcher; the
  runner path takes A's own nsfs observation of the same namespace and a `PidfdOpen` verified by
  identity before and after. Both are bound to the helper's accepted one-use RELEASE for that exact
  gate PID/identity, and the e2e independently re-verifies the target's credentials, pidfd, command
  line, leaf membership and clone at the same boundary.
- The release marker's proof fields are written by the `Target` literal in
  `captureReleasedNativeTarget` (`linux_owner_witness_linux.go:2066`): `fillSummaryFromJournal`
  copies `PIDFDVerified`/`UserNamespace`/`HostUID`/`SamePIDExec` from `witness.peer` only when it is
  non-nil (`:1699`) and never fills `NetworkNamespace`, so with the peer publication removed (M2)
  the marker still satisfies the e2e predicate and reddens only the unit assertion on
  `witness.peer` (`linux_owner_witness_linux_test.go:1980`); that assertion is what keeps the peer
  publication honest, not the peer as the source of the marker's proof fields (the literal is
  redundant only on the normal path where the peer is published).
- The intermittent aborted-release measurement note and the recovery-decision residuals of
  `stale_leaf`/the stale-journal sets stay tracked in their own entries; this goal only unblocks the
  release hold they sit behind.

## 9. Retained artifacts

- Plan `docs/plans/2026-11-26-rf-acc-migration-pi-hold-plan.md`; this record.
- Scratch probes in the GDD state root `scratch/` (§3), and the verbatim re-run of
  `findings/probe-gate-sequence.py`.
- Retained run log `gdd/native/run-45696662454.log` (raw 3213-3221, 3784+) as cited by the diagnosis.

## 10. Apply-stage refresh (2026-11-26)

Applied under the conductor's apply brief from the simplify run
(`runs/2026-11-26/symmetry-rf-acc-migration-pi-hold-simplify/answer.md`): F1, one
`openExactLiveLinuxV2WitnessPIDFD` for both witness pidfd captures (the two `unix.PidfdOpen` sites
in the witness collapse to one helper); F2, one `linuxV2NativeWitnessNamespaceStat` for the
nsfs-stat preamble; F3, the release fixture's journal is parameterized and four fail-closed
rejection cases plus the `ownerEffectAlreadyAckedOnEntry` replay skip are pinned in the unit file;
F4/F5, this record's and the plan's amended sentences. §1 digests, §2's instrument anchor, §5 rows
and §6 mutants are re-measured from these bytes.

Recorded deviations and repairs, not hidden:

- Determinism repair applied with F3: the unit fixture published the release signal with a
  non-atomic `os.WriteFile` while the pre-existing barrier (`waitRelease`, unchanged) hard-fails on
  incomplete content; one gate run caught the mid-write state (`witness release marker is not the
  exact bounded root-controlled signal`). `writeRelease` now writes `<release>.staged` and renames
  it into place; the barrier's checks are untouched and no production byte changed.
- F1 harmonizes two unasserted release-path diagnostics (the identity-change suffix and the
  pidfd-liveness wording); F2's helper keeps the `net`/`network` and `user` strings verbatim via
  the nsfs entry name. Each message template has only its definition site as a consumer (census in
  the apply report).

## 11. Fix-stage refresh (2026-11-26, review STD-A01/STD-A02)

The review found no behavior defect (`MUST_FIX`, both P3; the guest Bind row stays carried to the
conductor). Dispositions:

- STD-A01 (the record misattributed what pins the marker's proof fields): §8 rewritten; no code or
  test behavior changed. Baseline observation: M2 on the pre-fix bytes left every marker assertion
  green and reddened only the assertion on `witness.peer` — the marker's fields are the `Target`
  literal's, kept honest by that unit assertion.
- STD-A02 (the release-seam decision had no local pin): the launch-time store is deleted and the
  `DropAckAfter` wrapper derives the kind from `witness.request.Harness` through the same
  `linuxV2NativeWitnessStagesReleaseFromStartProcess` predicate that wires
  `startRequest.StartProcess`. Equality chain at the delivered bytes: `RunLinuxV2NativeOwnerWitness`
  validates the request against the same value it stores as `witness.config`
  (`linux_owner_witness_linux.go:404,414-415`); the validation requires
  `request.Harness == value.Runtime.HarnessKind` (`:2571`); `runFresh` derives the launch kind from
  `witness.config.Runtime.HarnessKind` (`:3201`) through `configuredHarnessKind`
  (`internal/app/probe.go:122`) = `harness.ParseKind` (`internal/harness/kind.go:21`, the
  wire→internal table at `:11`) — a pure lookup, so the request harness equals the launched kind by
  construction. Red-first evidence: inverting the removed store on the pre-fix bytes left
  `go test ./internal/app/ -run TestLinuxV2NativeWitness -count=1` green (`ok`, exit 0) — the gap —
  while the final bytes redden under M1/M3/M4 (§6). The fail-closed arm is pinned by
  `TestLinuxV2NativeWitnessReleaseSeamRequiresTheRequestHarness`
  (`linux_owner_witness_linux_test.go:2070`, M1) and the equality clause by
  `TestLinuxV2NativeWitnessRequestHarnessMustMatchTheConfiguredKind` (`:105`, M5).
- New rows: the two tests above; the release fixture stamps `Harness: config.RuntimeHarnessPi`
  (`:1858`) and the daemon.start exclusion row switches it to codex (`:1986`).
- CI surface: the pinned rows run in the `daemon` job (`daemon_race_tests`:
  `go test -race -count=1 -timeout 600s ./...` on ubuntu-latest, non-root runner user) and in
  `coverage-daemon`; the mutants are scratch overlays, so their red is observed on the WSL root
  runner — an oracle note, since what the CI jobs bind is the restored tests.

Gates re-run on the fix-stage bytes (§1; WSL, `CGO_ENABLED=0`, `GOFLAGS=-mod=mod`):

| gate | command | exit | result |
| --- | --- | --- | --- |
| format | `gofmt -l ./internal/app/` | 0 | no output |
| vet (unit) | `go vet ./...` | 0 | no output |
| vet (e2e compile) | `go vet ./e2e/` | 0 | no output |
| focused witness tests | `go test ./internal/app/ -run 'TestLinuxV2NativeWitness' -count=1` | 0 | `ok github.com/wxxb789/symmetry/daemon/internal/app 0.989s` |
| unit suite | `go test ./internal/app/ -count=1` | 0 | `ok … 156.037s` |
| e2e compile of the case | `go test ./e2e/ -run TestProductionLinuxV2MigrationDenialRetainsJournal -count=1` | 0 | `ok github.com/wxxb789/symmetry/daemon/e2e 0.015s` (witness opt-in skip) |
| mutant rows | overlays M1–M5 (§6) | 1 each | §6 |
| sanitize | `python scripts/ci/model-routing-record-sanitize.py --check --root-only` per changed doc | 0/1/0/0 | TODO/evidence/plan pass; `CLOSED-TODO` exit 1 identical at the base revision (pre-existing quoted specimens; added lines carry no host path) |
| jscpd | `python scripts/ci/jscpd-changed.py --base 8453da12 --head HEAD` (scratch clone) | 0 | 6 changed file(s), 1759 scanned source(s), 1262 clone(s) in the corpus, none new |
| record cleanliness | `python scripts/ci/check-record-cleanliness.py --base 8453da12 --head HEAD` | 0 | 1 changed record passes the root-only check |

Test counts: witness file 40 → 42 top-level tests (branch base 35); `daemon/internal/app`
direct-child test files 715 → 717; the package's `-list` count is 711 at the fix-stage bytes.

Delivered, pending the conductor's Bind: the §1 code digests are unchanged across the mutant and
gate runs above.

Appendix — mutant deltas against the fix-stage §1 bytes (each an overlay of the delivered source;
the repository tree was never modified, and its two sha256 rows above are unchanged before and
after the runs):

| # | delta | mutated sha256 | observed |
| --- | --- | --- | --- |
| M1 | the harness derivation, the runner-seam gate and the `captureReleasedNativeTarget` block in the `DropAckAfter` wrapper replaced by `if !selected || ownerEffectAlreadyAckedOnEntry {` / `return nil` / `}` | `0e096b3d6816db47e4facb8886969532e37e07fa2f3dedebc02fdde1a8af162c` | §6 M1 row |
| M2 | `witness.peer = nil` inserted after the peer literal in `captureReleasedNativeTarget` | `cf833a33f4e6992f290998fce05329d323969c69762e942b158f2146ac42ff2c` | §6 M2 row |
| M3 | `linuxV2NativeWitnessStagesReleaseFromStartProcess` body `return true` | `62b0bb07da2527c9b7dad6cdbb503ae20380da69083de2f64180d1769c96caa3` | §6 M3 row |
| M4 | the same body `return kind == harness.KindPi || kind == harness.KindOpenCode` | `6bc71f6fbb7e622be29d9e487dc5516ca537327d6469aa534bb04901e0d5972a` | §6 M4 row |
| M5 | `request.Harness != value.Runtime.HarnessKind || ` deleted from the request validation predicate | `e948b34bb903b005ffd5de5e9724e10c2cf1aa4767b61440a37f48dd3a3695eb` | §6 M5 row |
