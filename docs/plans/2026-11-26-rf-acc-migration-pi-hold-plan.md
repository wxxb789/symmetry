# rf-acc-migration-pi-hold — implement plan

Contract: `goal-rf-acc-migration-pi-hold.md` (GDD state root, external to the repository).
Base revision: `8453da1203f4c109824545b8caee41f9e9696557` (#293).
Branch: `refactor/rf-acc-migration-pi-hold`. Diagnosis: `findings/environ-root-cause.md` §4
(EnvironRoot, confidence medium-high; raw log `gdd/native/run-45696662454.log` 3213-3221, 3784+).

The accepted plan is the contract plus the diagnosis §4; this file resolves only the
implementation choices those leave open, the per-clause baseline, and what is out of scope.

## 1. Base census (before any edit)

- The migration witness stops before its hold:
  `witness output: production_linux_v2_native_witness_test.go:728: owner-witness stderr:
  -linux-v2-native-owner-witness: wait for authentic Pi owner witness turn` and
  `wait for exact native_release boundary: owner-witness command did not reach its requested
  durable stage`; the barrier listing holds no `native_release.json` (run `45696662454`).
- `native_release` is staged only from the witness's `daemon.start` closure
  (`linux_owner_witness_linux.go:3456,3470,3488`), which is wired into a harness launch only for
  codex/claude through `harness.StartRequest.StartProcess` (`:3509-3522`). `daemon/internal/harness/pi/adapter.go`
  installs its own `runnerProcessStarter` (`:81-87,105,175-176`), so a Pi launch never runs that
  closure; the same is true of `harness/opencode/adapter.go` (`:73-79,204,213`).
- The release ACK seam fires on every launch path:
  `execution/linux_owner_v2_linux.go:1073-1077` calls
  `invocation.Callbacks.DropAckAfter(LinuxV2AckStageNativeRelease, …)` after
  `owner.releaseSucceeded = true`; the witness already wraps that callback for its ACK-loss cases
  (`linux_owner_witness_linux.go:3927-3936` at base) and the stop-receipt/leaf-removed/cleared
  stages are already staged from the same callback family (`:3886-3926` at base, record `rf-acc-pi-durability-matrices`
  §4b: `stale_leaf`'s `stop_receipt_durable` hold is reached on the pi vehicle).
- The released same-PID target is A-observable: the gate user namespace is created by the gate
  *after* it drops to A (`c-gate/gate.c:1404-1414`), so A owns it and can `stat`
  `/proc/<pid>/ns/{user,net}`, `PidfdOpen(pid)` and read `status`/`cmdline` (the e2e does exactly
  this in `linuxV2CaptureLiveProcess`, `production_linux_v2_native_witness_test.go:2997+`).
  Measured on the WSL 6.18 kernel with the gate's real order and map (evidence §3):
  parked gate → `stat ns/user`/`stat ns/net`/`environ`/`readlink fd` all OK; released target →
  `stat ns/user`/`stat ns/net` OK, `environ`/`fd` EACCES (the `shared`-set defect, not this goal's).
- The e2e marker contract the hold must satisfy:
  `linuxV2MarkerHasNativePeerProof` (`production_linux_v2_owner_cli_test.go:1083-1089`) requires
  `Target.PID/Identity` equal to `Owners.Gate`, `PIDFDVerified`, `HostUID == owner.BUID`,
  `SamePIDExec`, and nonzero user/network namespace device+inode; the replay leg restores the
  evidence from the durable marker (`restoreStageMarkers`, `linux_owner_witness_linux.go:2336`;
  `linuxV2ValidateStageReplayResponseState`, `production_linux_v2_owner_cli_test.go:1162`, released-evidence check at `:1229-1233`).

## 2. Public/API shape

All changes stay inside the two owned sources; no e2e, fixture-script or `pi/**` change is needed
(the migration arm already requests the `native_release` hold on the pi-durability vehicle, and the
e2e already validates the marker exactly).

1. `daemon/internal/app/linux_owner_witness_linux.go`
   - `linuxV2NativeWitnessRun` gains one boolean recording that the launch path observes the
     release through `daemon.start` (codex/claude) instead of the runner callback.
   - New `captureReleasedNativeTarget(store journalStore) error`: binds the committed
     RunJournal target (`ContainmentAuthority` + `ContainmentCgroup.Gate` + process marker, all
     equal), verifies the live same-PID process identity before and after `PidfdOpen`, requires
     the pidfd live, stats `/proc/<pid>/ns/user` (new small helper) and reuses
     `linuxV2NativeWitnessNetworkNamespace`, then publishes the exact target through the existing
     `witness.peer` + `summary.Target` fields.
   - `wrapLinuxV2Callbacks`' `DropAckAfter` wrapper stages `native_release` on the runner path
     (plain hold and ACK-loss alike) and leaves the generated codex/claude rows unchanged (their
     plain release hold still stages from `daemon.start` and the callback no-ops); the one
     validator-permitted configuration that differs — a `native_release` ACK-loss drop on a
     `daemon.start`-wired adapter — captures the released target, and no generated row is affected
     because the ACK-loss selectors are built from the pi-only `pi-durability.json` template.
   - The Pi turn failures keep their message and gain the inner cause (`%w`), which is the bounded
     log-only instrument Stage 1 asks for: the fixture prints a bounded stderr tail
     (`linuxV2ReportOwnerWitnessFailure` → `linuxV2OwnerWitnessStderrBoundedLines`).
2. `daemon/internal/app/linux_owner_witness_linux_test.go`
   - The release-hold test (exact marker evidence + the hold engaging, with the release file the
     test writes) and the exclusion test (codex/claude still stage only from `daemon.start`; the
     ACK-loss release keeps staging + `ErrLinuxV2MutationAckLost`).

## 3. Implementation choices the plan left open

### 3.1 The runner release stage is staged from the release-ACK callback (`:1073`), not from the Pi branch

Chosen: the callback. It is the only seam that observes a runner-launched release for *every*
adapter that owns its Runner, it fires after `owner.releaseSucceeded = true` (the helper accepted the
exact gate RELEASE), it precedes any transport/Open/turn on the Pi path, and it is already the seam
this witness uses for the ACK-loss release and for the stop/receipt stages. Rejected: staging from
the Pi branch after `adapter.Start` (Pi-only, needs a second observation after the transport is
already live, and leaves the release unobserved for any other self-runner adapter); changing
`pi/**` to consume `harness.StartRequest.StartProcess` (not owned, and the seam is absent by design
for adapters that own launch).

### 3.2 The condition is "the adapter owns its Runner", single-sourced with the wiring

`linuxV2NativeWitnessStagesReleaseFromStartProcess(kind)` is used both to wire
`startRequest.StartProcess` and to decide whether the callback must stage the release, so the two
can never drift. Consequence, recorded rather than hidden: the OpenCode health leg (frozen bytes,
fixed `native_release` hold) would now also stage a release hold if it ever reached its release; it
aborts earlier on its own pinned-version gate today, so no observed row changes, and the alternative
(a Pi-only special case) would encode the defect's shape instead of the launch-path fact.

### 3.3 The released target's namespace evidence is A's own nsfs observation

The codex path takes `UserNamespace` from `DuplicateLinuxV2Peer`, whose value is the parked gate's
A-side observation cross-checked with the authenticated root launcher
(`execution/linux_owner_v2_linux.go:2558-2572`). An adapter that owns its Runner never hands the App
that pidfd, so the equivalent A-side observation is made directly: the userns is the same ns object
across the same-PID exec, A owns it, and the release ACK already binds the helper's one-use RELEASE
to that exact gate PID/identity. The marker therefore carries the same field shape as the codex
path; the *strength* difference (no root cross-check of the ns inode, `PidfdOpen(pid)` instead of a
duplicated launch-transferred pidfd) is recorded in the evidence record as a residual, not hidden.
Rejected: reading `uid_map`/`gate.c`'s launcher frames in the witness (a second convention for
evidence the fixture-side e2e already asserts), and staging without namespace evidence (the e2e
marker contract would reject the marker — a weakened deliverable).

### 3.4 The rejected alternative (option b of the contract) stays rejected

Routing `migration` through codex/claude would change the case's vehicle (its config, its
provider-free `pi-durability` claim binding and its `native_binary` pin), and the case's intent is
"migration/leaf uncertainty retains an unresolved journal" on an independent guest
(`goal-0006b-fixture-inventory-2026-09-29.md:78`) — nothing in it is codex/claude-specific, but
nothing licenses a vehicle swap either; the harness-agnostic intent is already served by the pi
runner path once the hold is wired.

### 3.5 Scope addition: the witness unit test file carries the mutant pins

The contract's owned list names the witness source, not `daemon/internal/app/linux_owner_witness_linux_test.go`,
while the same contract requires that "mutants pin the new wiring". No e2e file can carry a runnable
pin (the guest is the conductor's), so the three new tests and their fixture join the edit set for
exactly that purpose; no other test file is touched, and the production seam grows no test hook.

## 4. Per-clause baseline and proof plan

| Contract clause | Base behavior (before) | Clause test / proof |
| --- | --- | --- |
| 1 root cause named with source + log evidence | the diagnosis's wiring claim; the inner turn error is discarded by `errors.New` at `:3633-3636` (not in the retained log) | the wrap makes the retained bounded stderr name it; the code path is pinned by the two unit tests and cited verbatim in the record |
| 2 the fix reaches the hold on the production path | no `native_release.json` on the pi path (`run-45696662454` §4) | `TestLinuxV2NativeWitnessRunnerReleaseHoldStagesExactTarget` (marker + hold + exact evidence); the guest Bind is the conductor's |
| 2 no `pi/**` change, no new provider access, no second authority | — | `git diff` scope; the tests exercise the existing seam only; the provider-free claim binding is untouched |
| 3 records name the root cause verbatim | — | evidence record §1/§4 |
| Invariants (no weakened denial/retention assertions, ≤2-line comments, `go vet`) | — | untouched e2e validators; comment audit; gate rows |
| Acceptance (conductor's Bind reaches the hold) | — | carried to the conductor; this stage records the code rows |

## 5. Local gates

`gofmt -l` (daemon), `go vet ./...` and `go test ./internal/app/` with `GOOS=linux`/in WSL
(`GOFLAGS=-mod=mod`, `CGO_ENABLED=0`), the e2e compile gate (`go vet ./e2e/`), and the mutant row
(revert the callback wiring → the new test reddens). Never run the measurement harness beside the
suite.

## 6. Out of scope (with reasons)

- `daemon/native/linux-gate/**` and the `/proc/<pid>/environ` assertions in `shared`: separate
  defect, separate goal (`rf-acc-apparmor-userns-profile` residual, diagnosis §5).
- The release-failure/recovery-decision residuals of `stale_leaf` and the stale-journal sets: their
  own tracked entries; this goal only unblocks the release hold they sit behind.
- Any `pi/**` change (not owned; contract requires `DECISION_NEEDED` instead) and any production
  daemon change outside the witness file.
- The credentialed Bind: conductor-dispatched; this stage records the local rows.
