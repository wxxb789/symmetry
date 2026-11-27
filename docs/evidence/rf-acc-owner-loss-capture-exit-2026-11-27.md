# rf-acc-owner-loss-capture-exit — evidence record

Contract: `goal-rf-acc-owner-loss-capture-exit.md` (GDD state root, outside the repository).
Dispatch base: `d45e3298` (#305, `main`). Plan:
`docs/plans/2026-11-27-rf-acc-owner-loss-capture-exit-plan.md`. Local verification is not the
credentialed acceptance; the conductor re-Binds the PR head (§9). All Go rows below ran from
`daemon/`: the Linux-tagged rows in WSL Ubuntu-24.04 (`go1.27.0 linux/amd64`), the head-wide
Windows rows on `go1.27.4 windows/amd64`.

## 1. Subject bytes (sha256)

| File | Before (`d45e3298`) | Delivered |
| --- | --- | --- |
| `daemon/internal/app/linux_owner_witness_linux.go` | `3bf974a170e1fe237c2eebe1428719fac4467705c7c1dfe7ac9d3e1945006dcd` | `1b645c6e0120df260633346e3c2aa43d3c37264ad950a9685bfa824e96f78cfd` |
| `daemon/internal/app/linux_owner_witness_linux_test.go` | `f8cdf9b5ff54f5713a157c19e4e805fb6186e1eeb3ee8d272aea39926e42d6a1` | `4aee0ec96c054bc902f313ad7864d893f9f1c44365dc053849f48e24635c5563` |
| `daemon/e2e/production_linux_v2_owner_cli_test.go` | `1bde11fbcfda1e7175a07f4c8407d69435658426e135b52c9bcc12d350a235f4` | `e0c971dfbaf81ea66adf33c8a4e2af3cecb6692748fd1f9dc7c2b5195c728f75` |

Every command and overlay mutant in §3, §4 and §6 executed against the delivered bytes above, in
the working tree the conductor receives (no commit was made from this stage, `git status` shows
only these three Go files plus the four docs paths). The plan and this record cannot carry their own
digests; the scratch-clone receipt in §7 binds the seven-path revision it committed.

## 2. Slice 1 — the early return and why the exit is terminal

### 2.1 The classification, verbatim

`platform.LinuxOwnerAnchorClient.LookupHelper`
(`daemon/internal/platform/linux_owner_anchor_linux.go:479-565`) returns a helper whose
`Control` is non-nil only for the anchor's `attached` status (`:563-565`); for
`helper_exited` it returns `Control == nil` plus the exit proof and the two run pins (`:497-525`).
The anchor serves `helper_exited` when `run.helperExit != nil`
(`daemon/internal/platform/anchor_session.go:260,290-292`), and `run.helperExit` is written only by
`linuxOwnerAnchorRun.waitHelper` (`daemon/internal/platform/anchor_run.go:1304-1321`) after
`linuxOwnerAnchorWaitPIDFD` proved the wait and reap of the exact helper identity (proof
`Waited: true, Reaped: true`, `:1316-1320`). No other assignment to `run.helperExit` exists in
`anchor_run.go`, `anchor_session.go` or `anchor_server.go`; `waitHelper` itself refuses an
already-exited run (`:1305`), so the value is monotone for the life of the run.

### 2.2 Why the latch and the stop proof cannot appear after the exit

- The owner-loss latch has exactly one writer: `linuxOwnerHelperV2.markOwnerLost`
  (`daemon/internal/platform/helper_v2_recovery.go:191-200`, called from `watchOwnerPipe`
  `:144-176`). It is the helper process's own field, and it is served only from inside that
  process's connection handlers (`helper_v2_server.go:706`, `helper_v2_stop.go:320`).
- The stop proof the snapshot stores (`status.StopEvidence.Proof`) is created by the same
  process's `stopOwned` (`helper_v2_stop.go:714`, cached `:890`/`:1014`; field
  `helper_v2_server.go:324`) and served only from that memory (`stopEvidenceSnapshot`
  `helper_v2_stop.go:275-296`, `writeResponse` `:316`). Neither field has a durable copy the anchor
  can serve: the anchor's own run entry keeps
  pins and the exit proof, not the helper's latch or its `LinuxCgroupStopProof`.
- `helper.Control.Status` is reachable only through `dialLinuxOwnerHelperV2`
  (`helper_v2_protocol.go:337-380`), which authenticates `SO_PEERCRED` against the exact helper
  `{PID, Identity}`; a reaped process cannot accept a connection, and no code path re-creates a
  helper for the same RunKey (`handleStartHelper` refuses an existing run; `handleLookup` serves
  `attached` only while `helperExit == nil`).
- Therefore, after the first `Control == nil` response for the exact held helper, every later
  `LookupHelper` for the same grant returns the same terminal classification (or `retired` / an
  error) and the only statements that set `Captured`/`OwnerLost`/`StopProof`
  (`linux_owner_witness_linux.go:2962-2979`) are unreachable. The re-dial can only re-observe the
  exit at 250 ms cadence until the 40 s bound.

### 2.3 What changed

- `captureLinuxV2NativeWitnessOwnerLoss` ends the poll on the first exact-identity terminal exit:
  `exited := helper.Control == nil && helper.PID == reservation.Helper.PID && helper.Identity ==
  reservation.Helper.Identity` inside the existing unusable-helper arm
  (`linux_owner_witness_linux.go:2952-2961`), with the same
  `anchor returned no live exact held helper for the owner-loss snapshot` error, the same
  uncaptured snapshot shape, and no other change to the arm or to the live-helper arm
  (`:2962-2979`, byte-identical statements).
- A terminal exit for a *different* identity keeps today's poll (the identity equality is not
  dropped) because a foreign anchor-supplied exit is an anchor-invariant violation, not the held
  helper's death; widening there would be a second notion of "gone".
- New seam (production, unexported): `linuxV2NativeWitnessAnchor`
  (`Discover` + `LookupHelper`) and `linuxV2NativeWitnessOwnerLossAnchor`, whose default returns
  `owner.anchor` (`:2906-2914`); the capture resolves `Discover`/`LookupHelper` through it. The
  default body runs in every acceptance leg; the rows in §3 override and restore it. Rationale and
  the rejected alternatives are in the plan §2.3.

## 3. Slice 1 rows — red-first on the pre-change loop (WSL)

Red-first order: the seam landed first (behavior-preserving), the row was added, and the row was
run against the production bytes that still had no early return:

```
$ go test -count=1 -run '^TestLinuxV2NativeWitnessOwnerLossCaptureStopsAtExactTerminalHelperExit$' ./internal/app
--- FAIL: TestLinuxV2NativeWitnessOwnerLossCaptureStopsAtExactTerminalHelperExit (40.10s)
    linux_owner_witness_linux_test.go:2580: terminal helper_exited capture dials = 161, want the single terminal classification
FAIL	github.com/wxxb789/symmetry/daemon/internal/app	40.121s
```

exit 1. After the early return (delivered bytes):

```
$ go test -count=1 -v -run '^TestLinuxV2NativeWitnessOwnerLossCapture' ./internal/app
--- PASS: TestLinuxV2NativeWitnessOwnerLossCaptureKeepsTheRecoveryTimeout (0.26s)
--- PASS: TestLinuxV2NativeWitnessOwnerLossCaptureStopsAtExactTerminalHelperExit (0.00s)
--- PASS: TestLinuxV2NativeWitnessOwnerLossCaptureKeepsPollingForAnotherHelpersExit (0.50s)
ok  	github.com/wxxb789/symmetry/daemon/internal/app	0.785s
```

exit 0. Row facts:

| Row | Input (anchor stub) | Assertions | Delivered observation |
| --- | --- | --- | --- |
| `…CaptureStopsAtExactTerminalHelperExit` | held = looked-up = `{20, helper:20:start}`, `Control == nil`, `HelperExit` = `testAppLinuxV2Exit(held, anchor)` | 1 dial; elapsed ≤ 40 s/8; `Captured`/`OwnerLost`/`ReleaseAuthorized` false; `StopProof` nil; version 1; stage + helper identity echoed; `Error` = the pre-change literal | 1 dial, 0.00 s, shape and literal as pinned |
| `…CaptureKeepsPollingForAnotherHelpersExit` | held `{20, …}` ≠ looked-up `{99, linux:v2:foreign-helper}`, `Control == nil` | ≥ 2 dials; uncaptured; `Error` contains `no live exact held helper`; held identity echoed | 3 dials in a 500 ms context, uncaptured |

The row drives the production function end to end: reservation/helper validation, grant build,
owner resolution, `Discover` equality, the poll loop and the returned shape. The stub supplies only
inputs (an anchor identity and a terminal helper); the dial count and elapsed time are the
production loop's own control flow, so no stub-side short circuit can satisfy the assertion. The
fixture asserts its own structural validity (`LinuxCgroupReservation.Validate`,
`LinuxOwnerAnchorGrant.Validate`, `LinuxCgroupExitProof.Validate`) before the run.

Pre-change comparison for the live-helper arm: `…KeepsTheRecoveryTimeout` (existing row, unchanged)
still passes, so the recovery-budget contract is untouched.

Mutants (`go test -overlay`, mutant copies in the operator's scratch root, never written into the
tree; each mutant run's output was read from the guest's own scratch log and is quoted below):

| Mutant | Overlay edit | Row | Result |
| --- | --- | --- | --- |
| `m1_no_early_return` | the whole arm reverted to the pre-change body (no `exited`) | `…StopsAtExactTerminalHelperExit` | RED, exit 1, `dials = 161`, 40.156 s — identical to the red-first observation |
| `m2_widen_terminal` | `exited := helper.Control == nil` (identity terms dropped) | `…KeepsPollingForAnotherHelpersExit` | RED, exit 1, `dials = 1`, 0.020 s |

## 4. Slice 2 — the router call-site pin

### 4.1 What moved

The call site (`daemon/e2e/production_linux_v2_owner_cli_test.go:2030-2036`) now makes one call
into `linuxV2WaitStageReplayPreRecovery` (`:1202-1227`) with the predicate's value, the two pinned
fds in a named-field `linuxV2StageReplayPreRecoveryPins`, and the two waiters
(`linuxV2RequireTargetContainedBeforeRecovery`, `linuxV2WaitPIDFDExited`) whose local rows already
exist (`TestLinuxV2OwnerLossSnapshotBeforeRecovery` local-replica subtests,
`production_linux_v2_native_witness_test.go:2721`). Arms and texts are the pre-change ones:

| `snapshotRequired` / stage | Arm | Bound | Failure text (unchanged) |
| --- | --- | --- | --- |
| false, any stage | helper-exit wait on the pinned helper pidfd | `linuxV2OwnerLossStopWait` (90 s) | `A helper did not exit with owner loss after the stage-replay kill: …` |
| true, `native_release` | target-containment wait on the pinned target pidfd | 90 s | `stage %s: the native target was not contained before recovery: …` |
| true, `stop_receipt_durable` | no wait | — | — |

The pre-change hazard comment (“Unproven owner-loss stops self-kill the helper within
linuxOwnerHelperV2StopTimeout, …”) moved with the arm. The call site keeps its
`if helperPIDFD != nil` guard and a single `t.Fatal(err)`, so both arms' messages are byte-identical
to the base texts.

### 4.2 Rows (delivered bytes, WSL)

```
$ go test -count=1 -v -run '^TestLinuxV2StageReplayPreRecoveryWait' ./e2e
--- PASS: TestLinuxV2StageReplayPreRecoveryWaitArms (0.00s)              (8 subtests)
--- PASS: TestLinuxV2StageReplayPreRecoveryWaitFailurePropagates (0.00s)  (2 subtests)
ok  	github.com/wxxb789/symmetry/daemon/e2e	0.016s
```

exit 0. `…WaitArms` computes `snapshotRequired` with the real predicate for each stage row and
asserts the arm set, the fd and the bound each waiter receives, over the eight stage/marker/journal
combinations of the predicate table (released `native_release`, durable `stop_receipt_durable`,
not-released `native_release`, `helper_bound`, `launcher_bound`, `gate_bound`, `lease_armed`,
`checkout_ready`). `…WaitFailurePropagates` asserts both arms wrap their waiter's error
(`errors.Is` on a sentinel) and that only the selected arm runs. Because the arbitration is one
function with no branch left at the call site, a swapped, dropped or widened arm is a local failure;
the named-field pins struct makes a positional fd transposition between the arms a compile error.

### 4.3 Mutants

| Mutant | Overlay edit (`production_linux_v2_owner_cli_test.go` copy) | Result |
| --- | --- | --- |
| `m3_swap_arms` | the two arm bodies exchanged | RED, exit 1, 14 FAIL lines; `native_release` released row got `helper_exit` want `target_containment`, and every unproven row got `target_containment` |
| `m4_drop_unproven_arm` | the unproven arm's wait deleted | RED, exit 1, 12 FAIL lines, 7 failing subtests; every unproven row got no arm |
| `m5_widen_target_arm` | `case stage == "native_release"` → `case snapshotRequired` | RED, exit 1, 5 FAIL lines; the `stop_receipt_durable` row got `target_containment` want none |

## 5. Assertion inventory — nothing weakened

- Slice 1 keeps every guarantee of the changed arm: the unusable-helper classification, the exact
  error literal, the returned shape, the live-helper arm and the 40 s/250 ms budget constants are
  unchanged; the only removed work is the unobservable re-dial after a proven reap (§2.2). The
  recovery-budget row (`…KeepsTheRecoveryTimeout`) and the ordering rows
  (`…SnapshotPrecedesRecovery`, `…OwnerLossStage`, `…OwnerLossSummaryShape`) are untouched and pass.
- Slice 2 keeps both arms, both bounds, both failure texts, the `helperPIDFD != nil` guard and the
  ordering of the calls; the predicate row (`TestLinuxV2StageReplayOwnerLossSnapshotRequired`, 13
  rows) and the snapshot row (`TestLinuxV2OwnerLossSnapshotBeforeRecovery`, 16 rows + 3 local
  pidfd replicas) are unmodified. No existing test was deleted, renamed or relaxed.
- New assertions and their homes: the two app rows and the two e2e rows run in the `daemon` CI job
  (`go test -count=1 -timeout 600s ./...`, `.github/workflows/ci.yml:1461`); the Go vet job runs
  `go vet ./...` (`:992`). The conductor's Bind case sets `claude_containment` /
  `codex_containment` execute the changed production arm through
  `TestProductionLinuxV2StageCrashReplayMatrix/<stage>`
  (`scripts/ci/linux-v2-native-acceptance.sh:2545,3223-3228`) and the extracted router function on
  their stage-replay legs, but they do not run the four new local rows: a Bind never executes a
  `-run` filter that excludes them.

## 6. Local gates

| Platform and command | Exit | Observation |
| --- | ---: | --- |
| WSL: `gofmt -l` on the three changed Go files | 0 | no files listed |
| WSL: `go vet ./internal/app ./e2e` (base bytes, before edits) | 0 | baseline for attribution |
| WSL: `go test -count=1 -run '^TestLinuxV2NativeWitnessOwnerLoss' ./internal/app` (base bytes) | 0 | baseline for attribution |
| WSL: `go test -count=1 -run '^TestLinuxV2StageReplay\|^TestLinuxV2OwnerLossSnapshotBeforeRecovery' ./e2e` (base bytes) | 0 | baseline for attribution |
| WSL: `go test -count=1 -v -run '^TestLinuxV2NativeWitnessOwnerLossCapture' ./internal/app` | 0 | 3 rows pass (0.00 s / 0.26 s / 0.50 s) |
| WSL: `go test -count=1 -v -run '^TestLinuxV2StageReplayPreRecoveryWait' ./e2e` | 0 | 10 subtests pass |
| WSL: `go test -count=1 -run '^TestLinuxV2StageReplay\|^TestLinuxV2OwnerLossSnapshotBeforeRecovery' ./e2e` | 0 | predicate, snapshot and wait rows together |
| WSL: `go vet ./...` | 0 | full module |
| WSL as user `accept`, `umask 022`: `go test -count=1 -timeout 900s ./...` | 0 | 31 `ok`, 0 FAIL, 267 s wall |
| WSL as root: `go test -count=1 -timeout 900s ./...` | 1 | 30 `ok`, 1 FAIL package; `internal/platform`'s 13 `TestLinuxOwnerAnchorShape*`/`…Wire*` tests refuse with the documented root guard (`wire harness requires an unprivileged A process: uid=0 euid=0 gid=0 egid=0`) — the environment row of the established convention, not a code failure |
| Host (Windows): `go vet ./...` | 0 | full module |
| Host (Windows): `go test -count=1 -timeout 900s ./...` | 0 | 27 `ok`, 0 FAIL, 232 s wall |

The Windows run cannot compile the three changed files (`//go:build linux && amd64`); it is the
standing head-wide regression check for the rest of the module and is attributed to CI for the
Linux-tagged packages. Head-wide suites are CI's own gate as well; neither substitutes for the
credentialed acceptance (§9).

## 7. Committed-blob gates (scratch clone)

The three Go files, the plan, this record and the two TODO documents were committed in a scratch
clone of the dispatch base (never in the worktree), and the three committed-blob gates ran against
that commit; the PR-body receipt names the tested commit and its seven blobs, which this record
cannot carry without changing the bytes it gates:

| Command | Exit | Observation |
| --- | ---: | --- |
| `python -B scripts/ci/jscpd-changed.py --base d45e3298 --head HEAD` | 0 | 7 changed files, 0 tolerated and none new among the added lines (corpus counts in the PR-body receipt); the gate notices `daemon/e2e/production_linux_v2_owner_cli_test.go` and `docs/archive/CLOSED-TODO.md` are outside its scan scope, so clones inside them stay unchecked |
| `python -B scripts/ci/check-record-cleanliness.py --base d45e3298 --head HEAD` | 0 | the changed record passes the root-only sanitizer check; an earlier revision of this record was refused for one guest-log path mention, which the delivered prose does not carry |
| `python -B "$comment_cap" "$scratch_clone" d45e3298` | 0 | no added comment block exceeds the 2-line cap |

## 8. Residuals

- The capture's anchor seam default (`linuxV2NativeWitnessOwnerLossAnchor` returning `owner.anchor`)
  has no local row: a real `*platform.LinuxOwnerAnchorClient` needs the installed A/B owner
  configuration, so the one-line default runs only in the credentialed acceptance legs. Filed in
  `docs/TODO.md` under the goal's section; owner conductor.
- Deliberate scope, not a defect: non-terminal lookup outcomes (the anchor's `retired` status and
  transient lookup errors) keep the poll-to-the-40 s-bound behavior; only the proven
  `helper_exited` classification plus the exact held identity ends the capture early. Recorded as a
  decision note in `docs/TODO.md`.

## 9. Acceptance still pending

The conductor's Bind must run `claude_containment` and `codex_containment` once on the PR head and
record that both sets stay green with the `owner_loss` snapshot still captured (`owner_lost=true`,
`error` absent, helper-issued stop proof) plus the measured capture budget. No local gate here is
that acceptance; the delivered bytes changed the executed capture arm, so the Bind is the first
end-to-end run of the early return.
