# rf-acc-migration-pi-hold — evidence record

Goal `rf-acc-migration-pi-hold` (contract: the goal file under the GDD contracts root; conductor
ruling 2026-11-26, diagnosis `findings/environ-root-cause.md` §4). Branch
`refactor/rf-acc-migration-pi-hold`, base `8453da1203f4c109824545b8caee41f9e9696557` (#293).
Plan: `docs/plans/2026-11-26-rf-acc-migration-pi-hold-plan.md`. All paths are relative to the
repository or to the GDD state root. The goal's acceptance row (`migration` opens on a PR-head Bind)
is the conductor's; this record carries the local code, unit and compile rows.

## 1. Delivered files and digests (round-0 delivered bytes, `c665bf4f`; the round-1 fold is §13)

| file | sha256 at the delivered bytes |
| --- | --- |
| `daemon/internal/app/linux_owner_witness_linux.go` | `d73c42af80d54571d3eb3cff52448c34b22970093dc1e090764421870050af17` |
| `daemon/internal/app/linux_owner_witness_linux_test.go` | `1882d4af5fdafa1d14ef7a5611ee2dc12a5856405b7c8cbdd4fa223f82a33dfd` |
| `docs/plans/2026-11-26-rf-acc-migration-pi-hold-plan.md` | `d45e1a7c4f079d741d5deb4b286efbe3dd3e1d49f0c3000b1d2fc45738fea13b` (record revision; not run-bound) |

The rows above are the revision §10–§12 measured; the round-1 fold (§13) refreshes the two code
digests to `1fd4c17f3f54d6b616097de1eb1832ee6d2b04ec67434e5e48fb607688ae0ce3` /
`86c8f7ff5593ee23718e5cd853e04042c972e391db947e90f17d439a39d40a5d` and the plan to
`384fa88f55dfaca21b70bed1c08f0ea287fda080bf3dfe1bd0997545acea9880`.

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

All commands run in WSL `Ubuntu-24.04` (`<HOST-PATH>/daemon`, `CGO_ENABLED=0`, `GOFLAGS=-mod=mod`)
as the apply-stage snapshot: §10 records the re-run on the same apply-stage digests, §11 holds the
round-0 delivered-byte gate rows and §13 the round-1 fold's rows.

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
| Acceptance | Bind `45720019801` reached the `native_release` hold on the round-0 head and recorded the next stop (the initial-lease ACK; §12); the round-1 fold stages it (§13), and the post-fix Bind re-runs `migration` + `claude_containment` on the new head; delivered, pending that Bind |

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

## 12. Bind run (2026-11-26) — the `migration` case set on the PR head

The conductor's Bind ran the `migration` case set exactly once on the exact PR-head bytes plus the
two scratch-only files below (run head `ecfabe81a58a58c930eda366ad38a7cce8e6b876` = `c665bf4f` +
the two files), watched once; the scratch branch `scratch/rf-acc-containment-goal-admission` and
its temporary worktree are deleted; `total_count=1` right after creation, after settlement, and
after the deletion. `refs/pull/297/head` read before the push, after the push, and after the
deletion: `c665bf4f…` throughout; no push ever touched the PR branch.

**Run `45720019801`** (event `push`, head `ecfabe81…`, workflow `Scratch containment goal-admission
probe`, attempt 1): `contracts` success; `linux-native-v2-acceptance` failure (23:16:58Z–23:22:05Z);
`Case set migration exited 1` / `Failed case sets: migration`. Raw-log line cites below are from
`gh run view 45720019801 --log` (3,508 lines; auxiliary copy at the GDD state root
`scratch/bind297/run-45720019801.log`, sha256
`1df6f55ee7064fea0298c2459f5d1e58c2494c8b1e6d12011d5267a0f66d2fad`).

**The `native_release` hold is reached.** The single failing check is the migration case's denial
assertion; 0 log occurrences of `did not reach its requested durable stage`, `wait for authentic Pi
owner witness turn`, `read exact native_release marker`, `native_release migration boundary is not
exact`, `external observer did not verify exact B process`, or each migration-move and
fail-closed-recovery message. The failure line attributes to
`production_linux_v2_native_witness_test.go:728` (the `t.Helper()` call site of
`runProductionLinuxV2MigrationDenial`), whose only reachable predecessor chain is
`production_linux_v2_owner_cli_test.go:4149` (exact `native_release` boundary wait) → `:4153`
(marker) → `:4158` (RunJournal) → `:4162` (boundary) → `:4165` (external B capture) →
`:4195`/`:4220` (A-owned alternate leaf + move) → `:4226` (outside the pinned leaf) →
`:4234`/`:4238` (SIGKILL + fail-closed recovery) → `:4241` (redacted response) → `:4245`
(recovery exit status, deliberate nonzero for `unresolved`; log 3077).

**Next stop, verbatim** (log 3072):

```
    production_linux_v2_native_witness_test.go:728: migration did not fail closed through the production App: Store checkout readiness lacks the earlier initial lease ACK
```

So the migration case proceeds past the hold into its **denial assertion**
`linuxV2ValidateStageReplayResponseState(fixture, "native_release", response, "unresolved")`
(`production_linux_v2_owner_cli_test.go:4252`), which first rejects at `:1209-1210`: the response
carries `stages.checkout_ready=true` with `stages.initial_lease_acked=false` (same bits in the
sanitized `migration-denial.json`; the `stage_replay` response also shows
`native_release_state: "released"`, `outcome`/`recovery_state` `unresolved`, `failure_category`
`containment_unproven`, `journal_unproven: true`, `no_target_abort: false`, `migration_observed:
true`, `migration_path_hash`
`cee8ccf0667b2723732993cef25c0880cacb1748d8ff58988be2e504995ac226`). The same bits also violate the
two later release/checkout relations (`:1212-1213`, `:1220-1222`), which the validator does not
reach because it returns the first violation. The **retention assertion**
`linuxV2ValidateStageReplayJournal` (`:4255`) plus the `Journal.NoUnprovenContainment = false` and
pinned-leaf-identity retention checks (`:4261-4266`) were not reached — unmeasured in this run, not
failed.

Artifacts (both downloaded; zip sha256 equals the API `digest`):

| Artifact | id | size | zip sha256 | members (sha256) |
| --- | --- | --- | --- | --- |
| `linux-v2-native-evidence-45720019801-1` | `15072047105` | 3161 B | `5c4ef30e86863577f07cc445edb1710876976f44272bdd916003a0b863c32c6f` | `migration/manifest.json` `7b5355d17ec57f6a530bc95142fbf22d247ccd3d3d4b17aaa274080df1cb4236`; `migration/migration-denial.json` `9c9d94e87fc9891761434907519a4db824038f10a55c47e54223c261c43d98a3` |
| `linux-v2-host-prerequisite-45720019801-1` | `15071537762` | 673 B | `a12e8ed7f09e083970278361543d06fff78f28f92b6047a5c8c432eec04e47d5` | `linux-v2-host-prerequisite-45720019801-1.json` `b6454d08fc595a709ebe4693df03b9cb0373553855814aeb9480db5d7224d5ee` |

The evidence manifest carries `case_set: "migration"`, `source_commit` = the run head,
`status: "failed"`, `missing_allowlisted_files: []`; the host-prerequisite document carries the
same `source_commit`, `status: "passed"`, `delegated_leaf_probe_passed: true`.

### Run-input sha256 equality (run head `ecfabe81` vs PR head `c665bf4f`) (2026-11-26)

`git cat-file -p <rev>:<f> | sha256sum` at both revisions; `git diff --name-status dcfb1675
c665bf4f` (the branch's own delta over the merged main tip) is exactly the six rows below, and
`git diff --name-status c665bf4f ecfabe81` is exactly the two scratch-only rows. The two code
digests equal §1's delivered-byte digests verbatim; the record row is this file's PR-head blob (the
uncommitted §Bind edit is not part of the run inputs).

| File | Run-head bytes | PR-head bytes | Equal? |
| --- | --- | --- | --- |
| `daemon/internal/app/linux_owner_witness_linux.go` | `d73c42af80d54571d3eb3cff52448c34b22970093dc1e090764421870050af17` | `d73c42af80d54571d3eb3cff52448c34b22970093dc1e090764421870050af17` | yes |
| `daemon/internal/app/linux_owner_witness_linux_test.go` | `1882d4af5fdafa1d14ef7a5611ee2dc12a5856405b7c8cbdd4fa223f82a33dfd` | `1882d4af5fdafa1d14ef7a5611ee2dc12a5856405b7c8cbdd4fa223f82a33dfd` | yes |
| `docs/TODO.md` | `677682dca14dc721c63205a1dc88c5aaba371b9f271e06c55f345ea1f50f28a2` | `677682dca14dc721c63205a1dc88c5aaba371b9f271e06c55f345ea1f50f28a2` | yes |
| `docs/archive/CLOSED-TODO.md` | `e2e669ce78a1ce30f22b32a4b6f4958b4016858f3e0465805f2129290886a4fa` | `e2e669ce78a1ce30f22b32a4b6f4958b4016858f3e0465805f2129290886a4fa` | yes |
| `docs/evidence/rf-acc-migration-pi-hold-2026-11-26.md` | `c23b10c2507ef1ae6bb5516ad9ebf82d2cf2c9b163a020181eb6d282de492209` | `c23b10c2507ef1ae6bb5516ad9ebf82d2cf2c9b163a020181eb6d282de492209` | yes |
| `docs/plans/2026-11-26-rf-acc-migration-pi-hold-plan.md` | `d45e1a7c4f079d741d5deb4b286efbe3dd3e1d49f0c3000b1d2fc45738fea13b` | `d45e1a7c4f079d741d5deb4b286efbe3dd3e1d49f0c3000b1d2fc45738fea13b` | yes |
| `.github/workflows/scratch-rf-acc-containment-goal-admission.yml` | `7340bd28e989c2cd27ff1ada98c5d02fa3bdac7b1c8e448550823c0bd44c3da5` | absent (scratch-only) | scratch-only |
| `scripts/ci/scratch-gh-shim` | `507340af870448533257a82366718f36a010a27c6ef9c5daba94667788ed740a` | absent (scratch-only) | scratch-only |

Bind verdict: the `native_release` hold is reached; the migration case's next stop is the denial
assertion (`store checkout readiness lacks the earlier initial lease ACK`), recorded verbatim
above; the retention assertion is unmeasured because the denial assertion aborts the case first.

## 13. Round-1 fold (2026-11-26, ruling round 1: lease-ACK L1 + fail-closed arms + latent ACK-loss path)

Contract ruling round 1 (last section of `goal-rf-acc-migration-pi-hold.md`) folds
`rf-acc-initial-lease-ack` in here. Scope: the witness seam and its unit file (ruled accepted, §1),
this record, the plan, `docs/TODO.md` and `docs/archive/CLOSED-TODO.md`. No e2e validator, fixture
byte, acceptance-script line or `pi/**` file changed; the validators stay untouched.

### 13.1 The change

- **L1, the initial lease ACK.** `callbacks.DropAckAfter` (`linux_owner_witness_linux.go:4022-4059`)
  stages `lease_armed` when the execution layer delivers the initial lease ACK
  (`execution/linux_owner_v2_linux.go:837-838`, `sequence == 1`), under the pre-existing
  `ownerEffectAlreadyAckedOnEntry` replay skip plus two guards: `Barrier.HoldAfter != lease_armed`
  (the runner-held boundary's watcher owns that marker, `:3886`) and
  `!stages.InitialLeaseAcked` (a restored replay marker already owns it). The ACK fires inside
  `armInitialLease` (`execution/runner.go:823-841`) on every launch path — the pi/opencode adapters'
  own `runnerProcessStarter` (`pi/adapter.go:81,105,176`; `opencode/adapter.go:73,204,213`) and the
  codex/claude `StartProcess` closure (`codex/adapter.go:149`; `claude/adapter.go:145-146`) all run
  this same `runner.Start` — so one seam covers every adapter, the claude
  `hold_after=native_release` request the ruling names included.
- **The claude path receives the ACK.** Its delivery follows from the start ordering, not
  assumption: `armInitialLease` runs before `resumeTarget` (`execution/runner.go:603-609`), so the
  parked target cannot resume/exec until the initial renewal settled — the same settlement that
  sets `owner.leaseAck` (`execution/linux_owner_v2_linux.go:862-872`, `:871`) and then invokes the
  callback (`:837-838`). The Bind295 claude run observed the live exec'd B process
  (`native_command_verified=true`; `docs/evidence/rf-claude-schema-argv-2026-11-26.md` §7's bind
  row), so the ACK was delivered and no-oped at the delivered bytes — L1 is confirmed, not
  refuted, for the claude path. The *guest effect* of the new staging stays the post-fix Bind's row.
- **Fail-closed arms.** Identity-after-`PidfdOpen` and pidfd-liveness stay in
  `openExactLiveLinuxV2WitnessPIDFD` (`:1975-1996`); the nsfs shape moved to
  `linuxV2NativeWitnessUserNamespaceFromStat` (`:2112-2117`, called by
  `linuxV2NativeWitnessUserNamespace` `:2104-2110`) so a synthetic stat can reach it. §13.2 rows
  pin all three; §13.3 mutants redden them.
- **Latent codex/claude `drop_ack_after=native_release`.** `captureReleasedNativeTarget`
  (`:2022-2075`) retires the peer it replaces (`return previous.Close()`, `:2074`), so the
  `daemon.start`-published pidfd is closed instead of leaked; and the wrapper no longer re-writes
  the release marker when the `daemon.start` seam owns it (`:4050-4053`): the closure's marker
  (staged, and held, at `:3583`; `DropAckAfter` stamped by `witness.stage`) stays byte-identical,
  while the callback still captures the released target and returns `ErrLinuxV2MutationAckLost`.
  Both were latent — the generated ACK-loss selectors come from the pi-only `pi-durability`
  template (`scripts/ci/linux-v2-native-acceptance.sh` ACK-loss legs; record §4) — and are now
  pinned by `TestLinuxV2NativeWitnessStartProcessReleaseAckLossKeepsTheClosureStage` instead of
  left latent.

### 13.2 Unit rows

| row (file `daemon/internal/app/linux_owner_witness_linux_test.go`) | assertion |
| --- | --- |
| `TestLinuxV2NativeWitnessLeaseAckStagesThePlainNativeReleaseHold` `:2123` (`migration_pi`, `claude_hold_after_native_release`) | the initial ACK stages `lease_armed` with `hold_after=native_release`, `not_released`, no drop stamp and no B-peer proof; `InitialLeaseAcked=true`; the release marker is not staged |
| `...LeaseAckDoesNotTouchTheRunnerLeaseBoundary` `:2158` (`watcher_not_yet_staged`, `watcher_staged`) | the ACK never writes the `hold_after=lease_armed` boundary: absent before the watcher, byte-identical after it (journal moved on) |
| `...LeaseAckReplayStagesNothing` `:2207` (`restored_marker`, `acked_on_entry`) | a restored marker or an acked-on-entry ACK stages nothing and stays byte-identical |
| `TestLinuxV2NativeWitnessPIDFDIdentityArmRefusesAChangedTarget` `:2240` | an identity that changed after `PidfdOpen` refuses with the post-open arm |
| `TestLinuxV2NativeWitnessPIDFDLivenessArmRefusesAnExitedTarget` `:2255` | an exited (zombie) target whose pidfd is readable refuses |
| `TestLinuxV2NativeWitnessUserNamespaceShapeArmRefusesNonNsfsInodes` `:2296` | directory mode, zero device and zero inode refuse; an exact nsfs stat passes through |
| `TestLinuxV2NativeWitnessStartProcessReleaseAckLossKeepsTheClosureStage` `:2003` | the ACK-loss callback keeps the closure's marker bytes, closes the replaced peer, publishes the capture and returns `ErrLinuxV2MutationAckLost` |

The delivered bytes pass all rows (focused run, `ok`, exit 0; §13.5); each row reddens under its
§13.3 mutant.

### 13.3 Mutants (each an overlay of the delivered source; the tree was never written)

Each mutant ran as `go test -overlay <mutant>.json ./internal/app/ -run <row> -count=1` in WSL;
exit 1 below is the test's own status. The two source files' sha256 before and after the whole
series are `1fd4c17f…` (source) and `d58d4c05…` (unit file), byte-identical, so no mutant touched
the delivered bytes: every run was a read-only overlay.

| # | mutant delta against the delivered bytes | judged row | observed (exit 1 each) |
| --- | --- | --- | --- |
| M-L1 | the L1 staging removed from the wrapper (round-0 body restored) | `LeaseAckStagesThePlainNativeReleaseHold/{migration_pi,claude_hold_after_native_release}` | `linux_owner_witness_linux_test.go:2137: read the lease_armed stage marker: … no such file or directory` (both subtests) |
| M-L2 | the runner-held-boundary guard dropped (`ackLease = !witness.stages.InitialLeaseAcked` only) | `…LeaseAckDoesNotTouchTheRunnerLeaseBoundary/watcher_not_yet_staged` | `linux_owner_witness_linux_test.go:2173: the ACK staged over the runner-held lease boundary: <nil>` |
| M-L3 | the restored-marker guard dropped (`ackLease = HoldAfter != lease_armed` only) | `…LeaseAckReplayStagesNothing/restored_marker` | `linux_owner_witness_linux_test.go:2222: replayed lease ACK callback: existing owner witness stage marker differs from exact replay` |
| M-L4 | the `ownerEffectAlreadyAckedOnEntry` skip dropped from the early return | `…LeaseAckReplayStagesNothing/acked_on_entry` | `linux_owner_witness_linux_test.go:2235: acked-on-entry lease callback staged a marker: <nil>` |
| M-A1 | identity-after-`PidfdOpen` weakened (`current == ""` instead of `current != identity`) | `PIDFDIdentityArmRefusesAChangedTarget` | `linux_owner_witness_linux_test.go:2248: pidfd capture accepted an identity that changed after PidfdOpen` |
| M-A2 | pidfd-liveness weakened to the error branch only | `PIDFDLivenessArmRefusesAnExitedTarget` | `linux_owner_witness_linux_test.go:2289: pidfd capture accepted a target whose pidfd already signalled its exit` |
| M-A3 | nsfs shape weakened to `false` | `UserNamespaceShapeArmRefusesNonNsfsInodes` (3 subtests) | `linux_owner_witness_linux_test.go:2307: user-namespace shape arm accepted a stat outside the exact nsfs inode` (×3) |
| M-P1 | the closure-owned skip removed (unconditional release re-stage) | `StartProcessReleaseAckLossKeepsTheClosureStage` | `linux_owner_witness_linux_test.go:2032: ACK-loss release callback error = existing owner witness stage marker differs from exact replay, want the lost-ACK category` |
| M-P2 | the replaced-peer retirement removed (`return nil`) | `StartProcessReleaseAckLossKeepsTheClosureStage` | `linux_owner_witness_linux_test.go:2039: ACK-loss release callback leaked the replaced launch-transferred peer pidfd` |

M1-M4 and M-P1/M-P2 are the red rows for every new branch in §13.1 (the L1 staging and its two
guards plus the shared replay skip; the closure-owned release skip and the peer retirement); M-A1-M-A3
are the three fail-closed arms the ruling names. M-L2's `watcher_staged` subtest stays green under
its own mutant because the restored-marker guard independently catches that state (the two guards
overlap there by design; the `watcher_not_yet_staged` subtest is the discriminating one).

### 13.4 Ruling items and records

| ruling item | disposition |
| --- | --- |
| L1 stages `lease_armed` for a plain `native_release` hold | delivered (§13.1); rows §13.2; M-L1 reddens both |
| L1 also covers the claude path | delivered: no claude-specific code was needed (the shared lease ACK seam); delivery argument in §13.1; the guest effect is the post-fix Bind's row |
| the lease is not staged twice; replay stays byte-identical; codex keeps its order | two guards plus the shared replay skip (§13.1); rows `…DoesNotTouchTheRunnerLeaseBoundary` and `…ReplayStagesNothing`; M-L2/M-L3/M-L4 redden them; the codex plain-hold row `TestLinuxV2NativeWitnessStartProcessReleaseKeepsItsOwnStage` stays green (focused run, §13.5) |
| fail-closed arms each get a red-when-weakened row | rows `…PIDFDIdentityArm…`, `…PIDFDLivenessArm…`, `…UserNamespaceShapeArm…`; M-A1/M-A2/M-A3 redden them |
| latent codex/claude `drop_ack_after=native_release`: peer leak and double write fixed or proven unreachable | both fixed (§13.1) and pinned by `…AckLossKeepsTheClosureStage`; M-P1/M-P2 redden the two halves |
| scope: the unit file is accepted | §1's scope note plus §13.2's rows; plan §3.5 unchanged in intent |
| records: §5 cites the delivered-byte gate numbers | §5 recaptioned as the apply-stage snapshot, pointing at §11 (round-0) and §13 (round-1) |
| records: CLOSED-TODO says "delivered, pending the conductor's Bind" | the `rf-acc-migration-pi-hold` entry's closing sentence now reads exactly that |
| records: Bind297 kept as the hold evidence, diagnostic for the new head | §12 kept verbatim; its recorded next stop (`store checkout readiness lacks the earlier initial lease ACK`) is the stop §13 removes |
| records: claude residual updated | `docs/TODO.md` residual entry reads "addressed by L1, pending the conductor's Bind" |
| validators untouched | no e2e validator, fixture byte, acceptance-script line or `pi/**` file is in §13's delta |
| post-fix Bind | carried: `migration` and `claude_containment` once each on the new head; delivered, pending the conductor's Bind |

### 13.5 Gates (round-1 delivered bytes)

| gate | command | exit | result |
| --- | --- | --- | --- |
| format | `gofmt -l .` (daemon, Windows Go 1.27.4) | 0 | no output |
| vet (GOOS=linux) | `GOOS=linux go vet ./...` (daemon) | 0 | no output |
| vet (GOOS=windows) | `GOOS=windows go vet ./...` (daemon) | 0 | no output |
| focused witness race | WSL Ubuntu-24.04 (user `accept`), `go test -race -count=1 ./internal/app/ -run 'LinuxOwnerWitness\|Witness'` | 0 | `ok … 6.599s` |
| full unit package | WSL, `go test -count=1 ./internal/app/` | 0 | `ok … 154.782s` (unprivileged `accept`); `ok … 150.995s` as root |
| e2e compile gate | WSL, `go test -count=1 -run XXX_NONE ./e2e/` | 0 | `ok … [no tests to run]` |
| module suite (WSL) | WSL, `go test -count=1 ./...` | 1 | unprivileged `accept`: 27 of the 30 test packages pass; `internal/linuxpreflight`, `internal/platform`, `internal/workspace` fail on this host (missing AppArmor parser, world-writable temp parents) with **the same 31 failing rows at the round-0 delivered revision** (byte-identical failing-row lists). Root: 29 pass, `internal/platform` fails (14 rows, also identical at the base revision) |
| module suite (Windows) | `go test -count=1 ./...` (daemon, Windows) | 0 | all packages ok |
| mutants | the nine §13.3 overlay mutants | 1 each | 9/9 red |
| jscpd | `jscpd-changed.py --base dcfb1675 --head <round-1 scratch commit>` over the six changed files (script and `.jscpd.json` byte-identical to the scanned revision; a scratch jscpd 4.3.0, the root devDependency pin, because this host cannot reach `registry.npmjs.org` for a frozen-lockfile workspace install) | 0 | 6 changed file(s), 1762 scanned source(s), 1262 clone(s) in the corpus, none new |
| comment-cap | `comment-cap.py <round-1 scratch commit>` | 0 | no added comment block over 2 lines |
| sanitize | `model-routing-record-sanitize.py --check --root-only` per changed doc | 0/1/0/0 | TODO/evidence/plan pass; `CLOSED-TODO` refuses exactly as its round-0 bytes do (pre-existing quoted specimens; the added lines carry no host path) |
| record-cleanliness | `check-record-cleanliness.py --base dcfb1675 --head <round-1 scratch commit>` | 0 | 1 changed record passes |

Delivered, pending the conductor's Bind: the round-1 code digests are `1fd4c17f…` (witness source)
and `d58d4c05…` (unit file); the post-fix Bind runs `migration` and `claude_containment` once each
on the new head.
