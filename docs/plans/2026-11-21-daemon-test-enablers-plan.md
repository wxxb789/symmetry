# daemon-test-enablers plan — per-test goleak, `t.Parallel()` state tests, folded short waits

Contract: `goal-rf-w1-08-daemon-test-enablers.md` (GDD, 2026-11-21), branch
`wt-w1-08-daemon-test-enable`, baseline `origin/main` = `a3039f0` (this worktree's HEAD; re-verified
against `git log`). Source: program plan §4 W1-08 (row), findings `daemon-app.md` F16 (59 test
files, 1.4× prod LOC, 0 `t.Parallel()`, 5 ad-hoc sub-second waits, 149.1 s `internal/app` / 45.1 s
`internal/state`, goleak proposal), plan review `plan-review-ds-1.md` P3-7 (the wait-site
enumeration) and the binding `plan-review-opus-1.md` amendments (no package-level goleak TestMain;
keep process-global-seam tests serial; the wave-1 go.mod owner).

## Deliverable shape

1. `daemon/go.mod`, `daemon/go.sum`: `go.uber.org/goleak v1.3.0` (test-only, MIT; `go mod tidy`
   form: one direct require plus the checksum lines tidy records).
2. `daemon/internal/app/main_test.go` (new): `verifyNoLeakedGoroutines(t)`, a `t.Cleanup` wrapper
   around `goleak.VerifyNone(t)` with no ignore options, plus `TestLeakOracleDetectsLeakedGoroutine`,
   the self-check that fails if the oracle stops detecting a parked goroutine. No `TestMain`
   (binding amendment; see trade-off 1).
3. `daemon/internal/app/app_test.go`: the helper on 11 tests, the four short waits folded.
4. `daemon/internal/app/native_settlement_regression_test.go`: the helper on two tests, the 1 s
   cleanup wait folded.
5. `daemon/internal/state/*_test.go`: 287 `t.Parallel()` insertions (12 files): 236 top-level
   tests plus 51 own-store subtests; 7 tests and their seam subtests stay serial.
6. `docs/TODO.md`: the F15 entry (writeInputBounded's unowned writer goroutine) that the per-test
   goleak form exists for.
7. This plan. Mutation runs and timings are recorded in the stage's answer outside the repository;
   the raw logs live under the GDD scratch directory (paths in the evidence section).

## Public/API shape and edge-case semantics

### Per-test goleak

`verifyNoLeakedGoroutines(t)` registers `goleak.VerifyNone(t)` as the test's *first* cleanup, so it
runs last: after the test's own cleanups (`stopTestDaemon`, store closes) have joined the workers
the test started. Semantics: the test fails iff a goroutine that is not testing/runtime
infrastructure is still alive after the retry window (goleak's 20 retries, ≤100 ms sleeps, ~1 s
total), so a released-but-scheduling writer does not flake. This is deliberately a *named subset*:
the package cannot carry `goleak.TestMain` while `writeInputBounded` can strand its 60 s writer
(F15, now filed in `docs/TODO.md`), because the package-wide form would need wildcard ignores.

Verified set (11 + 2 tests, chosen across input, claim/cancel, lease, outbox, and native
settlement lifecycles): `TestCancelPreemptsBlockedInputInSameSnapshot`,
`TestCancelRejectsQueuedInputAfterPreemption`,
`TestScheduledSnapshotCancelPreemptsBlockedInputAndJoins`,
`TestBlockedInputDoesNotBlockAnotherRunCommand`,
`TestTerminalOutboxBackoffSuppressesPermanentFailuresAndResets`,
`TestTerminalOutboxRetryDelayUsesMinimumCadence`, `TestCancelWinsCompletionAndFlushesAcknowledgement`,
`TestCancelDuringClaimDefersUntilGrantAndNeverLaunchesAgent`,
`TestScheduledCancelDuringClaimCompletesAfterDurableReceipt`,
`TestRenewLeasesDoesNotSeriallyBlockOtherRuns`, `TestRenewLeasesJoinsWorkersAfterShutdown`
(app_test.go); `TestPersistentTerminalJournalFailureAfterUsageRetryDoesNotBlockOtherRunOutbox`,
`TestFlushRunDefersNativeUsageRecoveryToLiveTerminalOwner`
(native_settlement_regression_test.go). The planted-leak red run (contract acceptance) is recorded
below. Tests that leave a goroutine today (F15-adjacent input-timeout fakes and any worker the test
never joins) are *not* in the set; the oracle stays honest rather than filtered.
The oracle's own liveness is pinned by `TestLeakOracleDetectsLeakedGoroutine`: it parks one
goroutine, requires `goleak.Find` to report it, and joins it before returning, so an oracle that
stops detecting leaks reddens a permanent test instead of only the mutation record.

### The five folded waits

| # | site | before | after | guarded verdict |
| --- | --- | --- | --- | --- |
| 1 | `app_test.go:5588` TestCancelPreemptsBlockedInputInSameSnapshot | `select { <-terminated / time.After(500ms) }` then a bool into the final `Fatalf` | `select { <-input.terminated / time.After(testHangGuard) }` → `t.Fatal("cancel did not preempt blocked input")` | cancel preemption of a blocked input |
| 2 | `app_test.go:5694` TestScheduledSnapshotCancelPreemptsBlockedInputAndJoins | 10 ms poll loop with a 30 s deadline for `terminal_pending` + settled input | direct assertion at the existing `cancelPersisted` fence (the receipt is persisted under the run's input lock, which the preempted input command held until it recorded outcome + acknowledgement) | the preempted input is durably settled when the cancellation receipt lands |
| 3 | `app_test.go:6911` TestScheduledCancelDuringClaimCompletesAfterDurableReceipt | `select { <-completion / time.After(100ms) }` | deterministic parked-state checks: completion channel open, command still in `daemon.queuedCommands`, no durable acknowledgement for `cancel-1` | no early completion before the durable acknowledgement |
| 4 | `app_test.go:7103` TestRenewLeasesJoinsWorkersAfterShutdown | `select { <-done / time.After(200ms) }` plus post-release join wait | positive fence: wait until run-1's in-flight request barrier is released *by the drain loop* (2 → 1) while run-2 is parked; each poll probes `done`; then the post-release join wait stays | the loop joins cancelled workers before returning |
| 5 | `native_settlement_regression_test.go:1393` cleanup | silent `time.After(time.Second)` then `backgroundWG.Wait()` | `awaitNativeSettlementSignal(t, flushDone, "outbox flush did not stop after cancellation")` | the outbox flush stops after cancellation |

Site 4's fence needs run-1's renewal to *return* rather than return an error, so that its request
barrier survives until the drain loop processes it: `shutdownRenewControl.RenewLease` returns
`protocol.LeaseHeartbeatResponse{}, nil` for run-1 (with a comment naming the fence). No production
code changes.

### `t.Parallel()` for `internal/state`

Rule applied: a test may call `t.Parallel()` iff it touches nothing outside its own `t.TempDir()`
and its own `state.Store`. Exactly the seven tests that assign the package-level seams
(`applyDirectorySecurity`/`applyFileSecurity` at `state.go:58-60`, `renameStateFileOS` at
`state_windows.go:47-49`) stay serial, with their subtests:

- `state_test.go:147` TestStateSecuritySetupFailuresPropagateWithoutToken (all four subtests)
- `goal_delivery_test.go:695` TestQueueGoalDeliveryWriteFailureLeavesNoPartialIntent
- `goal_session_test.go:563` TestGoalSessionJournalCommitSurvivesLineageIndexRefreshFailure
- `goal_session_test.go:1675` TestMarkGoalSessionResumeNativeStoppedWriteFailureLeavesPreStartState
- `goal_session_test.go:1942` TestGoalSessionAtomicWriteFailureLeavesPriorRecord
- `state_windows_test.go:43` TestWindowsWriteAtomicRetriesAfterReaderHandleReleases
- `state_windows_test.go:108` TestWindowsWriteAtomicBoundedFailurePreservesDestinationAndCleansTemp
  (also the only elapsed-time assertion in the package: ≤750 ms around the 250 ms rename window)

Go's testing package runs all sequential tests to completion before any parallel test resumes, so
the seam mutators can never overlap a parallel test; reads of the seams by parallel tests (every
journal write goes through `writeAtomic` → `applyFileSecurity`/`renameStateFileOS`) are safe for
the same reason. Everything else about the package is per-store: no package mutexes, caches, or
globals besides the seams (verified: `state.go`, `state_unix.go`, `state_windows.go`,
`goal_session.go`, `goal_delivery.go`, `provider_action.go` contain only immutable error vars and
constants at package scope); `SetAtomicWriterForTesting` swaps a *Store* field.

Subtest rule: a subtest is converted iff its closure opens its own store in its first lines
(`mustStore(t)`, `New(t.TempDir())`, or a per-subtest helper such as `newPending`); 51 sites
qualify, including the 30-case Linux v2 admission table (`linux_cgroup_v2_test.go:913`) whose
sequential cases were the package's critical path. Subtests over the parent's store stay
sequential, and the four seam-mutating subtests of `state_test.go:147` stay sequential too — the
first full run showed an unrelated subtest's `New()` failing on a sibling's injected
`applyDirectorySecurity` denial during lock setup, and the fix (removing that `t.Parallel()`, not
the seam) is what the serial rule requires.

## Evidence plan (exact commands; results recorded)

All Windows runs from `daemon/` on this worktree; all Linux runs in WSL Ubuntu-24.04 (go1.27.0,
16 vCPU) against `/mnt/q/.../daemon`. No measurement runs concurrently with the full suite.

Mutations (each applied, run, reverted; `git diff` clean for the mutated file afterwards):

1. site 1: `app.go` cancel path drops `process.Terminate` (line 9444) →
   `go test -count=1 -run '^TestCancelPreemptsBlockedInputInSameSnapshot$' -v ./internal/app/`
   → FAIL `app_test.go:5621: cancel did not preempt blocked input` (30.59 s), exit 1.
2. site 2: `completeProvideInputWithRetry` returns before its store write → green (0.50 s): the
   store's terminal transition settles the unresolved input in the same journal write
   (`settleUnresolvedInputCommand`, `state.go:4625`). `settleUnresolvedInputCommand` returns early
   → green (0.50 s): the input handler's own write covers it. Both together → FAIL
   `app_test.go:5762: scheduled cancellation journal = …LocalState:"terminal_pending"…` (0.51 s),
   exit 1 — the assertion pins the observable, and both settle paths are load-bearing.
3. site 3: dispatch completes the unclaimed cancel command (`finishQueuedCommandLocked` before the
   return) → FAIL `app_test.go:6952: cancel command completed before its durable acknowledgement`
   (0.50 s), exit 1.
4. site 4: the drain loop returns on cancellation instead of draining results → FAIL
   `app_test.go:7157: renewal loop returned before cancelled workers joined` (0.61 s), exit 1.
5. site 5: the test's flush goroutine stops signalling `flushDone` → FAIL
   `native_settlement_regression_test.go:1420` and `:1393: outbox flush did not stop after
   cancellation` (60.14 s), exit 1 — the folded cleanup wait reddens on its own path too.
6. planted goroutine leak in `TestCancelDuringClaimDefersUntilGrantAndNeverLaunchesAgent` →
   `main_test.go:22: found unexpected goroutines: [Goroutine 19 … ]`, FAIL (0.61 s), exit 1.

### Measurements (WSL Ubuntu-24.04, go1.27.0, 16 vCPU; package wall from the `ok` line)

| tree | command | wall |
| --- | --- | --- |
| baseline (pre-conversion) | `go test -count=1 -v ./internal/state/` | `ok 40.838s` |
| converted | `go test -count=1 -parallel 1 ./internal/state/` (serial-equivalent) | `ok 37.596s` |
| converted, top-level tests only | `go test -count=1 ./internal/state/` | `ok 21.953s` |
| converted (final) | `go test -count=1 ./internal/state/` (default `-parallel` 16) | `ok 17.957s` |
| converted (final) | `go test -count=1 -parallel 4 ./internal/state/` (CI-shape proxy, 4 runs) | `ok 24.487s`, `36.112s`, `37.297s`, `24.082s` |
| converted (final) | `go test -race -count=1 ./internal/state/` (2 runs) | `ok 70.618s`, `ok 79.019s` |
| converted (final) | `go test -count=1 ./internal/state/` on Windows | `ok 13.350s` |

The `-parallel 4` spread is fsync-contention variance on the WSL VM, not test flakiness (all runs
green); the acceptance number in the contract is met by the host-default run and near the bound
under the 4-way proxy — see risk 3.

### Gates run on the final tree (all foreground, one at a time)

| gate | result |
| --- | --- |
| `gofmt -l internal/app internal/state` | no output, exit 0 |
| `go vet ./...` (Windows) | exit 0 |
| `GOOS=linux go build ./...` | exit 0 |
| `GOOS=windows go build ./...` | exit 0 |
| `go test -count=1 -timeout 1200s ./internal/state/... ./internal/app/...` (Windows) | exit 0; `ok internal/state 21.629s`, `ok internal/app 265.221s` |
| `go test -count=3 -timeout 900s ./internal/state/` (Windows, repeat stability) | exit 0; `ok 25.914s` |
| `go test -race -count=1 ./internal/state/` (WSL, twice) | exit 0 |
| `go test -race -count=1 -run '<13 goleak tests>' ./internal/app/` (WSL, twice) | exit 0; 13/13 PASS, `ok 170.732s` / `ok 167.238s` |
| `git diff --check` | exit 0 |
| `go mod verify` | `all modules verified` |

The full `go test -race ./...` step stays the conductor's CI oracle (it needs the network-cached
dependency set and privileged witness jobs); the WSL race runs above cover the changed surfaces.

## Design trade-offs

1. **Per-test goleak instead of `TestMain`** (binding amendment; F15 confirmed at
   `app.go:7240-7273`: the writer goroutine is unowned on both bounded arms). A package-wide check
   today can only pass with wildcard ignores, which is the failure mode the finding names. Cost:
   coverage is a named 13-test subset; the TODO entry records the complete form.
2. **Site 2 becomes an assertion, not a shorter poll.** The input-command handler writes its
   outcome and acknowledgement while holding `inputMu`; the cancel handler queues the cancellation
   receipt while holding `inputMu`; the store additionally settles an unresolved input inside the
   terminal-transition transaction. The durable state is therefore already settled when
   `cancelPersisted` fires — waiting longer could only observe the same journal. The old loop was
   a 30 s *tolerance* for a state that is atomic with the fence; the assertion is strictly
   stronger (it fails if that atomicity regresses).
3. **Sites 3 and 4 replace negative windows with parked-state checks.** Both waits asserted "the
   bad thing has not happened yet", which no fence can prove in general; the replacement asserts
   the *state* that makes the bad thing impossible at that instant (command parked, unacknowledged;
   worker parked, loop alive) and keeps the positive waits that pin the good thing. Residual: a
   regression that returns *after* releasing the barriers but before the other worker returns is
   caught only if it closes `done` before the next poll's probe — the same scheduling-dependent
   detection the old window had, now anchored to a causal event instead of a wall clock.
4. **Subtests stay sequential** (reason above); the acceptance is about package wall time, and the
   parents are the unit that gains concurrency.
5. **`awaitTestCondition` helper** (one user, next to `testHangGuard`): site 4's fence needs a
   compound condition plus a `done` probe inside the loop; the helper keeps one poll convention
   with the hang guard as the single failure bound and a comment that its interval is never a
   performance statement.
6. **`go mod tidy` form for go.sum**: records goleak's checksum lines (plus the checksums of
   goleak's own test-only module graph). No CI tidy gate exists; the direct require must not carry
   `// indirect`.

## Out of scope (with reasons)

- The other pinned waits in `internal/app` tests: `app_test.go:5491` (3 s) and `:7067` (1 s),
  `atomic_terminal_ack_interleaving_test.go:48` (2 s), `cleanup_test.go:655` (1 ms poll),
  `harness_test.go:4780` (5 ms poll). None is in the contract's enumerated five
  (`plan-review-ds-1.md` P3-7) and the wall-clock burn-down is W4-13's slice; touching them here
  would widen a test-enabler slice into a timing-semantics change of other scenarios.
- `app_test.go` scenario split (W3-15), ExUnit `async:` conversion (W3-14), production code
  (except the `docs/TODO.md` entry), subtest parallelization in `internal/state`.
- `state_windows_test.go:107`'s ≤750 ms bound: it stays serial by the seam rule, so no threshold
  change is needed or made.

## Residual risks and limits

- The Linux-only state files (`linux_cgroup_v2_test.go`, `linux_clone_journal_test.go`) cannot
  compile on the Windows host; their parallel verdicts are exercised by the WSL `-race` runs.
- The CI acceptance number (state ≤25 s) is measured on the CI runner, not here; the WSL
  before/after numbers are the same-host comparison the contract asks for (16 vCPU vs the runner's
  4, so the local number is optimistic).
- The 4-way (`-parallel 4`) proxy, the CI runner's shape, spans 24.1–37.3 s on this host: the
  remaining wall time is fsync-heavy store setup whose cost inflates under concurrent writers, so
  on a CI runner with a slow fsync path the ≤25 s bound could be missed even though the conversion
  removes the serial `sum-of-all-tests` bound (the 30-case Linux table is no longer the critical
  path). Mitigations outside this slice: the CI run is the arbiter; if it lands high, the next lever
  is the store-setup cost (`New()` per test), not more test parallelism.
- Per-test goleak covers 13 named tests; a leak introduced in another test is not caught until the
  F15 fix enables the package-wide form.
- Two mutation classes for site 4 remain scheduling-dependent by construction (trade-off 3).

## Increment closure

- `docs/TODO.md` gains the F15 entry; nothing is moved to `docs/archive/CLOSED-TODO.md` (no TODO
  item is closed by this slice).
- No design or runbook document goes stale: `docs/design/quality.md`'s gate table (line 114) still
  describes the daemon gates exactly (`go vet`, `go test`, `-race` for concurrency changes, which
  the state package now is), and no runbook describes test parallelism or wait conventions.
  `daemon/AGENTS.md`'s test command list is unchanged.
- The contract file itself is left in place; wave-1 peers' contract files are likewise still in the
  GDD contracts directory, so the move/index step belongs to the conductor, not this stage.
