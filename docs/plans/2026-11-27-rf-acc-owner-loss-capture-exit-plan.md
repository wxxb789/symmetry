# rf-acc-owner-loss-capture-exit — implement plan

Contract: `goal-rf-acc-owner-loss-capture-exit.md` (GDD state root, external to the repository).
Base revision: `d45e3298` (#305, `main`). Branch: `refactor/rf-acc-owner-loss-capture-exit`.
The accepted plan is the contract plus the two `docs/TODO.md` residuals it names; this file
resolves only what they leave open: the early return's exact classification, its evidence, the
router row's shape, and what is out of scope.

## 1. Base census (before any edit)

- Slice 1 site: `captureLinuxV2NativeWitnessOwnerLoss`
  (`daemon/internal/app/linux_owner_witness_linux.go:2906-2977` at `d45e3298`); the re-dial loop is
  `:2937-2975`, the terminal-classification arm `:2941-2944`, the bound
  `linuxV2NativeWitnessOwnerLossStopWait = 40 * time.Second` (`:2860`) with
  `linuxV2NativeWitnessOwnerLossPollInterval = 250 * time.Millisecond` (`:2861`).
- Terminal classification: `platform.LinuxOwnerAnchorClient.LookupHelper`
  (`daemon/internal/platform/linux_owner_anchor_linux.go:479-576`) returns `Control == nil` exactly
  on the anchor's `helper_exited` status (`:497-525`; the `attached` return carrying `Control` is
  `:575`), i.e. when `linuxOwnerAnchorRun.helperExit != nil`
  (`daemon/internal/platform/anchor_session.go:260,291`); the identity comes from the anchor's own run entry
  for the exact grant (`anchor_session.go:247-250`), so the capture's `PID`/`Identity` equality
  against `reservation.Helper` is the exact-held check the grant cannot carry (no helper field in
  `platform.LinuxOwnerAnchorGrant`).
- Slice 2 site: the router call site
  (`daemon/e2e/production_linux_v2_owner_cli_test.go:2021-2040` at `d45e3298`): the
  `ownerLossSnapshotRequired` branch with the `stage == "native_release"` containment wait and the
  `else` `linuxV2WaitPIDFDExited(helperPIDFD, linuxV2OwnerLossStopWait = 90 s)` arm; the predicate
  `linuxV2StageReplayOwnerLossSnapshotRequired` (`:1162-1170`) and its 13-row table
  (`TestLinuxV2StageReplayOwnerLossSnapshotRequired`, `:2552-2588`) are already committed.
- The reused waiters are `linuxV2WaitPIDFDExited` (`production_linux_v2_native_witness_test.go`)
  and `linuxV2RequireTargetContainedBeforeRecovery` (local pidfd subtests of
  `TestLinuxV2OwnerLossSnapshotBeforeRecovery`, `production_linux_v2_owner_cli_test.go`).

## 2. Slice 1 — the early return

### 2.1 Classification and shape (decisions the contract leaves open)

- The early return fires only when the looked-up helper is terminal *and* is the exact held helper:
  `helper.Control == nil && helper.PID == reservation.Helper.PID && helper.Identity ==
  reservation.Helper.Identity`. `Control == nil` is the same `helper_exited` notion production
  already uses; the identity terms are the pre-existing equality the arm already tests, so the
  return only shortens an arm that already classified the helper as unusable.
- A terminal exit for a *different* helper identity keeps today's behavior (re-dial to the bound,
  same error). One anchor-returned foreign exit is an anchor-invariant violation, not the exact
  held helper's death; widening the return there would be a second notion of "gone" and is
  rejected. Pinned by the second row (§4).
- The snapshot schema is unchanged. An exact terminal first lookup leaves `Captured`, `OwnerLost`
  and `ReleaseAuthorized` false, `StopProof` nil and the held helper identity echoed. If a live
  `Status` previously populated fields without complete proof, the terminal return retains those
  fields. `Error` now reports `anchor returned no live exact held helper for the owner-loss snapshot`
  immediately; the old loop could replace that diagnostic with a later lookup error at the deadline.
- Green path untouched: the arm body for a live exact helper is unchanged, so a run whose helper is
  alive at capture time (snapshot carries `helper_exit` nil, fixture requires `Error == ""`) takes
  the identical statements.

### 2.2 Why a gone helper cannot later yield the latch or the stop proof

- The latch (`linuxOwnerHelperV2.ownerLost`) is set only inside the helper process
  (`daemon/internal/platform/helper_v2_recovery.go:190-200`, `markOwnerLost`); the stop proof
  (`linuxOwnerHelperV2.stopEvidence`) is created only by the same process's `stopOwned`
  (`helper_v2_stop.go:714-720`, cached `helper_v2_server.go:324`) and is served only from that
  process's memory (`helper_v2_stop.go:275-296,316`, `helper_v2_server.go:706`).
- `Control == nil` implies the anchor observed `run.helperExit != nil`, and `helperExit` is written
  only by `linuxOwnerAnchorRun.waitHelper` (`anchor_run.go:1304-1321`) after `linuxOwnerAnchorWaitPIDFD`
  proved the wait and reap of the exact creation identity; the anchor never clears it
  (`anchor_run.go` writes it once, `:1320`; `waitHelper` refuses an already-exited run `:1305`), and
  `handleLookup` serves `attached` only while it is nil (`anchor_session.go:261-274,295-309`). A reaped
  process cannot serve its endpoint again, and no code path recreates a helper for the same RunKey
  (`handleStartHelper` refuses an existing run; the run's `close` clears pins, not evidence).
- Therefore, after the first `Control == nil` response, every later `LookupHelper` for the same
  grant returns the same terminal classification (or `retired`/an error), and the only statements
  that set `Captured`/`OwnerLost`/`StopProof` (the `Control.Status` arm) are unreachable. The evidence
  record names these paths with line numbers.

### 2.3 The test seam

The row must measure the real `captureLinuxV2NativeWitnessOwnerLoss`, which reaches the anchor
through `recovery.linuxV2OwnerForRecovery()`; a live anchor needs the installed A/B owner config, so
no local row can construct one. Following the file's existing capture/recovery seam convention,
production gains one unexported interface and one variable whose default is the real client:

```go
type linuxV2NativeWitnessAnchor interface {
	Discover(context.Context) (platform.LinuxOwnerAnchorIdentity, error)
	LookupHelper(context.Context, platform.LinuxOwnerAnchorGrant) (*platform.LinuxOwnerAnchorHelper, error)
}
var linuxV2NativeWitnessOwnerLossAnchor = func(owner *linuxV2OwnerResources) linuxV2NativeWitnessAnchor { return owner.anchor }
```

The default body is exercised by every acceptance run; the row overrides it and restores it with
`t.Cleanup`. Rejected alternatives: extracting only the loop (leaves the reservation/grant/Discover
prologue and the returned shape outside the row); a parameter seam on
`linuxV2NativeWitnessCaptureOwnerLoss` (changes an existing seam's signature and still leaves the
anchor resolution uncovered); testing through the anchor socket (needs the installed owner config).

## 3. Slice 2 — the router call-site pin

- After App SIGKILL and the anchor liveness check, the fixture passes stage, exact marker and
  journal, named pins and real waiters to `linuxV2WaitStageReplayPreRecovery`. The shared step
  derives the snapshot flag, guards both waits on the helper pin and returns the same flag for
  the later snapshot checks. The local table drives that step with recording waiters.
- With a helper pin, an unproven stop waits for helper exit; a proven `native_release` waits for
  target containment; a proven `stop_receipt_durable` does not wait. Both waits use the existing
  90 s fence and keep their original error text. Missing helper pin skips both waits without
  changing the snapshot decision.
- The table checks the returned flag, arm, descriptor and independent 90 s expectation. The error
  subtests still check arm exclusivity and `errors.Is`. Concrete pin arguments and waiters remain
  guest inputs, not assertions about real pidfds in the local recording tests.

## 4. Evidence plan

| Step | Command (WSL Ubuntu-24.04, from `daemon/`) | Expectation |
| --- | --- | --- |
| Red-first, slice 1 | `go test -count=1 -run '^TestLinuxV2NativeWitnessOwnerLossCaptureStopsAtExactTerminalHelperExit$' ./internal/app` on the seam-only bytes | row reddens (`dials=161`, `elapsed≈40 s`); observed before the early return is added |
| Green, slice 1 | same command after the early return | exit 0, `dials=1` |
| Narrow classification | `-run '…KeepsPollingForAnotherHelpersExit'` | stub cancels on the second lookup; widened terminal classification still gives one lookup |
| Slice 2 rows | `-run '^TestLinuxV2StageReplayPreRecoveryWait' ./e2e` | overlay the shared-step inversion, shortened bound and missing pin guard separately |
| Mutants | `go test -overlay` with a copy of the touched file, never written into the tree | each mutant exit 1 on its named row, unmodified exit 0 after |
| Fix-round gates | `gofmt -l`, `go vet ./internal/app ./e2e` on Windows and WSL; focused rows and affected packages in WSL | exit codes in the record; no new full-suite receipt |
| Committed-blob gates | scratch-clone commit of the delivered bytes: `comment-cap.py`, `scripts/ci/jscpd-changed.py`, `scripts/ci/check-record-cleanliness.py` | exit codes in the record; the tested record cannot carry its own digest |

Red-first order is therefore: seam (behavior-preserving) → row → observe RED → early return →
observe GREEN. New-row home: `daemon/internal/app/linux_owner_witness_linux_test.go` (slice 1) and
`daemon/e2e/production_linux_v2_owner_cli_test.go` (slice 2). Bind coverage: the Bind's
`claude_containment`/`codex_containment` case sets run
`TestProductionLinuxV2StageCrashReplayMatrix/<stage>` (`scripts/ci/linux-v2-native-acceptance.sh:2545`)
and exercise the live-helper capture and corresponding shared wait path, not terminal-helper
capture or the local rows. The local rows are `//go:build linux && amd64` and run in `coverage-daemon`
(`go test -count=1 -timeout 1200s -coverprofile=coverage.out ./...`, `.github/workflows/ci.yml:682`)
and the `daemon` job's race pass (`go test -race -count=1 -timeout 600s -json ./...`, `:1063`), not
in `daemon-windows` (`:1461`).

The fix-round assertions are local Go control-flow checks; the recording waiters do not poll a
pidfd or measure wall time, and the foreign-exit test cancels by lookup count. The Ubuntu 26.04
Resolute runner mirror is not required for these assertions; the local receipt names its actual
Ubuntu 24.04 Noble userland instead.

## 5. Out of scope (with reasons)

- Any `daemon/internal/platform/**`, `authority/**` or `execution/**` change: not owned; the
  anchor's terminal status and its `helperExit` semantics are used as-is.
- The `retired` lookup status and every non-terminal lookup error keep today's poll-to-the-bound
  behavior: the contract scopes the exit to the `helper_exited` classification.
- The `checkout_ready` helper-residency census and the other `docs/TODO.md` residuals of the
  stage-replay goal stay open and unmoved.
- `docs/TODO.md` entries for this goal close; anything this round learns but does not fix is filed
  as a new residual with its exact path and evidence.
