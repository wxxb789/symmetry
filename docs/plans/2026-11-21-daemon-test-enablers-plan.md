# daemon-test-enablers plan — per-test goleak, `t.Parallel()` state tests, folded short waits

Contract: `goal-rf-w1-08-daemon-test-enablers.md` (GDD, 2026-11-21), branch
`refactor/rf-w1-08-daemon-test-enablers`; merge base with `origin/main` (then at `6817693`, checked
2026-11-21) = `c8f2018`, and the branch is committed, not a dirty baseline: `aba4914` (the slice),
then `9063ab3` (the `origin/main` merge). Source: program plan §4 W1-08 (row), findings
`daemon-app.md` F16 (59 test files, 1.4× prod LOC, 0 `t.Parallel()`, 5 ad-hoc sub-second waits,
149.1 s `internal/app` / 45.1 s `internal/state`, goleak proposal), plan review
`plan-review-ds-1.md` P3-7 (the wait-site enumeration) and the binding `plan-review-opus-1.md`
amendments (no package-level goleak TestMain; keep process-global-seam tests serial; the wave-1
go.mod owner).

## Deliverable shape

1. `daemon/go.mod`, `daemon/go.sum`: `go.uber.org/goleak v1.3.0` (test-only, MIT; `go mod tidy`
   form: one direct require plus the checksum lines tidy records).
2. `daemon/internal/app/leak_check_test.go` (new; named for its content after the review, since it
   deliberately has no `TestMain`): `verifyNoLeakedGoroutines(tb)`, a `t.Cleanup` wrapper
   that snapshots `goleak.IgnoreCurrent()` at the test's entry and passes it to `goleak.VerifyNone`
   with no other options, plus `TestLeakOracleDetectsLeakedGoroutine`, the self-check that drives
   that wrapper through a recording TB and fails if the oracle stops naming the goroutine parked by
   the test. No `TestMain` (binding amendment; see trade-off 1).
3. `daemon/internal/app/app_test.go`: the helper on 11 tests, three short waits folded (site 3's
   fold was withdrawn in round 2 and its original wait restored; see the site table).
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

`verifyNoLeakedGoroutines(tb)` snapshots the goroutines alive at the test's entry
(`goleak.IgnoreCurrent()`) and registers `goleak.VerifyNone(tb, baseline)` as the test's *first*
cleanup, so it runs last: after the test's own cleanups (`stopTestDaemon`, store closes) have
joined the workers the test started. The baseline is what keeps a failure attributable: `VerifyNone`
scans every goroutine in the process, so without it a goroutine stranded by an earlier test in the
package (for example `writeInputBounded`'s writer) would fail whichever test runs next and blame
the wrong test. Semantics: the test fails iff a goroutine that is not testing/runtime
infrastructure is still alive after the retry window (goleak v1.3.0's 20 retries with exponential
sleeps capped at 100 ms, ≈0.43 s total), so a released-but-scheduling writer does not flake. The
form is also incompatible with `t.Parallel` (`VerifyNone` cannot associate goroutines with a test;
goleak's parallel form is `VerifyTestMain`), so W3-15's `app_test.go` split must keep it out of
parallel tests. This is deliberately a *named subset*:
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
The oracle's own liveness is pinned by `TestLeakOracleDetectsLeakedGoroutine`: it drives
`verifyNoLeakedGoroutines` itself with a recording TB while a goroutine is parked, requires the
recorded failure to name that goroutine's test, and joins the goroutine before returning — so an
oracle that becomes a no-op or ignores everything reddens a permanent test instead of only the
mutation record.

### The four folded waits

| # | site | before | after | guarded verdict |
| --- | --- | --- | --- | --- |
| 1 | `app_test.go:5588` TestCancelPreemptsBlockedInputInSameSnapshot | `select { <-terminated / time.After(500ms) }` then a bool into the final `Fatalf` | `select { <-input.terminated / time.After(testHangGuard) }` → `t.Fatal("cancel did not preempt blocked input")`; the blocked write is then released, so a preempted handler that won the race for `inputMu` cannot sit out `inputWriteTimeout` | cancel preemption of a blocked input |
| 2 | `app_test.go:5698` TestScheduledSnapshotCancelPreemptsBlockedInputAndJoins | 10 ms poll loop with a 30 s deadline for `terminal_pending` + settled input | direct assertion at the existing `cancelPersisted` fence (the receipt is persisted under the run's input lock, which the preempted input command held until it recorded outcome + acknowledgement) | the preempted input is durably settled when the cancellation receipt lands |
| 4 | `app_test.go:7120` TestRenewLeasesJoinsWorkersAfterShutdown | `select { <-done / time.After(200ms) }` plus post-release join wait | positive fence: wait until run-1's in-flight request barrier is released *by the drain loop* (2 → 1) while run-2 is parked; each poll probes `done`; then the post-release join wait stays | the loop joins cancelled workers before returning |
| 5 | `native_settlement_regression_test.go:1393` cleanup | silent `time.After(time.Second)` then `backgroundWG.Wait()` | `awaitNativeSettlementSignal(t, flushDone, "outbox flush did not stop after cancellation")` | the outbox flush stops after cancellation |

Site 3, `app_test.go:6915` TestScheduledCancelDuringClaimCompletesAfterDurableReceipt, is **kept
as-is**: its round-1 fold — a `commandWG` join bounded by `testHangGuard` before the parked-state
checks — was withdrawn after the round-2 review (P2-1), because the join only covers workers
registered with `commandWG`: a regression that hands the unclaimed cancel to a goroutine outside
it (`go daemon.executeQueuedCommand(ctx, queued)`) completed the command after `Wait` had already
returned, and the parked-state checks then sampled before it ran — that mutation escaped the
fenced `fe2b713` tree 10/10 while the original 100 ms window caught it 9/10, so the fold lost a
verdict (contract clause 3). The wait therefore stays exactly as on `origin/main`
(`select { <-completion / time.After(100ms) }`) with the three parked-state checks after it:
completion channel open, command still in `daemon.queuedCommands`, no durable acknowledgement for
`cancel-1`. Both mutation classes re-verified red on the restored test (mutation 3 below); the
round-2 alternative (a `goleak.Find` fence around dispatch) was not adopted.

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
16 vCPU) under `/mnt/q`: this worktree's `daemon/`, plus the pre-conversion baseline extracted with
`git archive 6099f26 daemon | tar -x -C <dir>` under `/mnt/q/tmp` for the matched before/after pair.
No measurement runs concurrently with the full suite.

Mutations (each applied, run, reverted; `git diff` clean for the mutated file afterwards):

1. site 1: `app.go` cancel path drops `process.Terminate` (line 9444) →
   `go test -count=1 -run '^TestCancelPreemptsBlockedInputInSameSnapshot$' -v ./internal/app/`
   → FAIL `app_test.go:5621: cancel did not preempt blocked input` (30.59 s), exit 1.
2. site 2: `completeProvideInputWithRetry` returns before its store write → green (0.50 s): the
   store's terminal transition settles the unresolved input in the same journal write
   (`settleUnresolvedInputCommand`, `state.go:4625`). `settleUnresolvedInputCommand` returns early
   → green (0.50 s): the input handler's own write covers it. Both together → FAIL
   `app_test.go:5766: scheduled cancellation journal = …LocalState:"terminal_pending"…` (0.51 s),
   exit 1 — the assertion pins the observable, and both settle paths are load-bearing.
3. site 3 (kept as-is; fold withdrawn): the round-2 review's two mutation classes, applied to
   `dispatchQueuedCommand`'s unclaimed-cancel branch and run with
   `go test -count=10 -run '^TestScheduledCancelDuringClaimCompletesAfterDurableReceipt$' ./internal/app/`
   on the restored test:
   - registered worker (mA): the branch falls through to
     `commandWG.Add(1); go func() { defer commandWG.Done(); executeQueuedCommand }` (the
     `daemon.mu.Unlock(); return` pair removed) → 9/10 FAIL
     `app_test.go:6952: cancel command completed before its durable acknowledgement`, exit 1. The
     one green iteration is the window's scheduling dependence — the worker had not completed the
     command within 100 ms.
   - unregistered (mB): the same branch runs
     `daemon.mu.Unlock(); go daemon.executeQueuedCommand(ctx, queued); return` → 10/10 FAIL, same
     message and line, exit 1.
   Pristine restored test: 10/10 PASS, exit 0. Round-2 review on the fenced `fe2b713`: mA 10/10
   FAIL, mB 10/10 **green** — the escape that withdrew the fold (P2-1).
4. site 4: the drain loop returns on cancellation instead of draining results → FAIL
   `app_test.go:7164: renewal loop returned before cancelled workers joined` (0.61 s), exit 1.
5. site 5: the test's flush goroutine stops signalling `flushDone` → FAIL
   `native_settlement_regression_test.go:1421` and `:1393: outbox flush did not stop after
   cancellation` (60.14 s), exit 1 — the folded cleanup wait reddens on its own path too.
6. planted goroutine leak in `TestCancelDuringClaimDefersUntilGrantAndNeverLaunchesAgent` →
   `leak_check_test.go:46: found unexpected goroutines: [Goroutine 9 in state select (no cases), with
   … TestCancelDuringClaimDefersUntilGrantAndNeverLaunchesAgent.func1 on top of the stack … ]`,
   FAIL (0.51 s), exit 1 (re-run 2026-11-21 against the wrapper with its baseline snapshot).
7. wrapper without the baseline snapshot (`goleak.VerifyNone(tb)`), with a residue goroutine parked
   by an earlier test in the package (temporary probe test, removed afterwards) → FAIL
   `leak_check_test.go:46: found unexpected goroutines: … TestTempResidueLeakProbe.func1 on top of the
   stack …`, reported on
   `TestPersistentTerminalJournalFailureAfterUsageRetryDoesNotBlockOtherRunOutbox` (0.69 s), exit 1:
   the leak is attributed to a test that did not create it. With the baseline restored the same pair
   is green (both directions re-run 2026-11-21).
8. `verifyNoLeakedGoroutines` reduced to a no-op → FAIL `leak_check_test.go:69: verifyNoLeakedGoroutines
   reported no failure while a goroutine was parked in this test; the per-test leak oracle is inert`,
   exit 1: the in-tree self-test, not a mutation record, is what pins the oracle.

### Measurements (WSL Ubuntu-24.04, go1.27.0, 16 vCPU; package wall from the `ok` line)

| tree | command | wall |
| --- | --- | --- |
| baseline (pre-conversion, `6099f26`) | `go test -count=1 -v ./internal/state/` | `ok 30.440s`, `ok 38.204s` |
| converted (serial-equivalent: `-parallel 1`, same flags as the baseline row) | `go test -count=1 -v -parallel 1 ./internal/state/` | `ok 35.102s`, `ok 41.023s` |
| converted (intermediate tree: `t.Parallel()` on top-level tests only) | `go test -count=1 ./internal/state/` | `ok 21.953s` |
| converted (final tree: top-level + own-store subtests) | `go test -count=1 ./internal/state/` (default `-parallel` 16, 2 runs) | `ok 17.238s`, `ok 20.828s` |
| converted (final tree) | `go test -count=1 -parallel 4 ./internal/state/` (scheduler-width proxy, 4 runs) | `ok 24.487s`, `36.112s`, `37.297s`, `24.082s` |
| converted (final tree) | `go test -race -count=1 ./internal/state/` (2 runs) | `ok 70.618s`, `ok 79.019s` |
| converted (final tree) | `go test -count=1 ./internal/state/` on Windows | `ok 13.350s` |

The baseline/serial-equivalent pair and the acceptance row were re-measured 2026-11-21 with matched
flags (the earlier record compared a `-v` baseline against converted rows without `-v`). The pair is
the concurrency control, not the acceptance number — with every test serial the converted tree is no
faster than its baseline on this host (the remaining wall is fsync-heavy store setup either way), so
the contract's ≤25 s comes from running the tests in parallel, and the default-`-parallel` row above
is its evidence (both fresh runs inside the bound).
The `-parallel 4` spread is fsync-contention variance on the WSL VM, not test flakiness (all runs
green) — see risk 3.

### Gates run on the final tree (all foreground, one at a time)

| gate | result |
| --- | --- |
| `gofmt -l internal/app internal/state` | no output, exit 0 |
| `go vet ./...` (Windows) | exit 0 |
| `GOOS=linux go build ./...` | exit 0 |
| `GOOS=windows go build ./...` | exit 0 |
| `go test -count=1 -timeout 1200s ./internal/state/... ./internal/app/...` (Windows) | exit 0; `ok internal/state 14.214s`, `ok internal/app 225.434s` |
| `go test -count=3 -timeout 900s ./internal/state/` (Windows, repeat stability) | exit 0; `ok 28.027s` |
| `go test -race -count=1 ./internal/state/` (WSL, twice) | exit 0 |
| `go test -race -count=1 -run '<13 goleak tests + the oracle self-test>' ./internal/app/` (WSL) | exit 0; 14/14 PASS, `ok 167.690s` |
| `git diff --check` | exit 0 |
| `go mod verify` | `all modules verified` |

The full `go test -race ./...` step stays the conductor's CI oracle (it needs the network-cached
dependency set and privileged witness jobs); the WSL race runs above cover the changed surfaces.
Every row except the two WSL `internal/state` race runs (untouched files) was re-run 2026-11-21
after the review fixes (STD-01…STD-07); the WSL `-race` runs need goleak in the distro's module
cache, which was seeded from the host cache because the distro has no egress.

## Design trade-offs

1. **Per-test goleak instead of `TestMain`** (binding amendment; F15 confirmed at
   `app.go:7240-7272`: the writer goroutine is unowned on both bounded arms). A package-wide check
   today can only pass with wildcard ignores, which is the failure mode the finding names. Cost:
   coverage is a named 13-test subset; the TODO entry records the complete form.
2. **Site 2 becomes an assertion, not a shorter poll.** The input-command handler writes its
   outcome and acknowledgement while holding `inputMu`; the cancel handler queues the cancellation
   receipt while holding `inputMu`; the store additionally settles an unresolved input inside the
   terminal-transition transaction. The durable state is therefore already settled when
   `cancelPersisted` fires — waiting longer could only observe the same journal. The old loop was
   a 30 s *tolerance* for a state that is atomic with the fence; the assertion is strictly
   stronger (it fails if that atomicity regresses).
3. **Site 3's fold was withdrawn (the `commandWG` join only reached registered workers); site 4
   uses parked-state checks.** Site 3's old `time.After(100ms)` asserted "the bad thing has not
   happened yet", and the round-1 replacement joined `commandWG` before the parked-state checks so
   they ran after any *registered* worker had finished. The round-2 review showed a regression
   that hands the unclaimed cancel to a goroutine outside `commandWG` escapes the join — the
   parked-state checks sample before that goroutine runs (mB: 10/10 green on fenced `fe2b713`,
   while the original window caught the same mutation 9/10) — so the fold lost a verdict and was
   withdrawn: site 3 keeps its `origin/main` wait unchanged with the parked-state checks after it
   (mutation 3: mA 9/10 and mB 10/10 red on the restored test; pristine 10/10 green). The four
   remaining folds keep their fences. Site 4 asserts the *state* that makes the bad thing
   impossible at that instant (worker parked, loop alive) and keeps the positive waits that pin
   the good thing. Residual: a regression that returns *after* releasing the barriers but before
   the other worker returns is caught only if it closes `done` before the next poll's probe — the
   same scheduling-dependent detection the old window had, now anchored to a causal event instead
   of a wall clock.
4. **Own-store subtests are parallel; parent-store and seam subtests stay sequential.** The 51
   subtests that open their own store in their first lines are isolated by construction (own
   `t.TempDir()` store, no parent state, no process-global seam), the same rule the top-level tests
   follow; the round-1 review verified the capture sets (per-iteration loop variables and the pure
   `newPending` helper) and that the seven seam-assigning top-level tests and their subtests stay
   serial. Subtests that use the parent's store stay sequential, so the parents and their
   own-store subtests are both units of concurrency; the acceptance is about package wall time, and
   the 30-case Linux v2 admission table's subtests were its critical path.
5. **`awaitTestCondition` helper** (one user, next to `testHangGuard`): site 4's fence needs a
   compound condition plus a `done` probe inside the loop; the helper keeps one poll convention
   with the hang guard as the single failure bound and a comment that its interval is never a
   performance statement.
6. **`go mod tidy` form for go.sum**: records goleak's checksum lines (plus the checksums of
   goleak's own test-only module graph). No CI tidy gate exists; the direct require must not carry
   `// indirect`.

## Out of scope (with reasons)

- The other pinned waits in `internal/app` tests: `app_test.go:5492` (3 s) and `:7107` (1 s),
  `atomic_terminal_ack_interleaving_test.go:48` (2 s), `cleanup_test.go:655` (1 ms poll),
  `harness_test.go:4780` (5 ms poll). None is in the contract's enumerated five
  (`plan-review-ds-1.md` P3-7) and the wall-clock burn-down is W4-13's slice; touching them here
  would widen a test-enabler slice into a timing-semantics change of other scenarios.
- `app_test.go` scenario split (W3-15), ExUnit `async:` conversion (W3-14), production code
  (except the `docs/TODO.md` entry).
- `state_windows_test.go:107`'s ≤750 ms bound: it stays serial by the seam rule, so no threshold
  change is needed or made.

## Residual risks and limits

- The Linux-only state files (`linux_cgroup_v2_test.go`, `linux_clone_journal_test.go`) cannot
  compile on the Windows host; their parallel verdicts are exercised by the WSL `-race` runs.
- The state ≤25 s acceptance has no CI measurement site: the Linux daemon job's ordinary pass is
  scoped to `./internal/harness/pi ./internal/harness/opencode`
  (`.github/workflows/ci.yml:1364`), and `internal/state` runs on Linux only inside that job's race
  pass (`go test -race -count=1 -timeout 600s ./...`, `ci.yml:1374`), whose own 16-vCPU wall is
  70.6–79.0 s. The number is therefore measured on host Linux (WSL, 16 vCPU); CI's race pass
  arbitrates correctness, not the bound, and "CI race green" and "host ≤25 s" are two separate
  acceptance statements.
- The `-parallel 4` row is a scheduler-width proxy, not a CI-shape proxy: `GOMAXPROCS` stays 16 on
  this host, so it exercises 4-way test parallelism on a 16-CPU machine, not a 4-vCPU runner. Its
  spread, 24.1–37.3 s, is fsync-heavy store setup whose cost inflates under concurrent writers, so
  on a runner with a slow fsync path the ≤25 s bound could be missed even though the conversion
  removes the serial `sum-of-all-tests` bound (the 30-case Linux table is no longer the critical
  path). Mitigation outside this slice: if a future Linux job lands high, the next lever is the
  store-setup cost (`New()` per test), not more test parallelism.
- Per-test goleak covers 13 named tests; a leak introduced in another test is not caught until the
  F15 fix enables the package-wide form.
- Site 4's mutation classes stay scheduling-dependent by construction, as does site 3's restored
  wall-clock window (round-2 re-run: mA 9/10, mB 10/10; trade-off 3).

## Increment closure

- `docs/TODO.md` gains the F15 entry; nothing is moved to `docs/archive/CLOSED-TODO.md` (no TODO
  item is closed by this slice).
- No design or runbook document goes stale: `docs/design/quality.md`'s gate table (line 114) still
  describes the daemon gates exactly (`go vet`, `go test`, `-race` for concurrency changes, which
  the state package now is), and no runbook describes test parallelism or wait conventions.
  `daemon/AGENTS.md`'s test command list is unchanged.
- The contract file itself is left in place; wave-1 peers' contract files are likewise still in the
  GDD contracts directory, so the move/index step belongs to the conductor, not this stage.
