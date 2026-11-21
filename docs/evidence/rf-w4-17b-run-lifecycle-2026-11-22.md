# rf-w4-17b run-lifecycle module — evidence record (2026-11-22)

Subject: `runningRun`'s lifecycle state moved into a new `runLifecycle` value with explicit, guarded
transitions in a new `daemon/internal/app/run_lifecycle.go`; every lifecycle write in the package's
non-test source resolves there, one derived `cleanupBlocked()` predicate replaces
`nativeCleanupBlocked`, and one `phase()` query derives the collapsed K1 phase. Contract:
`goal-rf-w4-17b-run-lifecycle.md` (GDD, 2026-11-21, with the arch-decision-2 `## Conductor
decision` sections and amendments V1-V10). Base revision `f70553bf` (the dispatch HEAD), which is
also the revision of every base coordinate below. Plan:
`docs/plans/2026-11-22-rf-w4-17b-run-lifecycle-plan.md`. Nothing is committed; this record
describes the working tree the gates ran on.

Subject identity (sha256 of the delivered files):

| file | sha256 |
| --- | --- |
| `daemon/internal/app/run_lifecycle.go` | `f8ce50e8cd4a7870aefd190e2b2ba6b9dca2fdad84a86b267cb63a9ee7d8c247` |
| `daemon/internal/app/run_lifecycle_test.go` | `dffef0ea370d249f8861bb59d39e19f567c85b3d64d84f40aff1181c608edb9a` |
| `daemon/internal/app/native_run.go` | `e23b01586fb00fc603c9bd197de9e15f8251bc5c2a1ecc7b515b6d05cf7a1760` |
| `daemon/internal/app/app.go` | `efc3c46887497a4f9c5dfda391585fe7316b26efcdb7f0243b8de947232fbfef` |
| `daemon/internal/app/cleanup.go` | `6f54a16546ad109a5e45c825543f4773c0c773ab14450af82b0ee147b62da388` |
| `daemon/internal/app/commands.go` | `57d2feb07937291a1863cab9fe69f1fa44dbced2a54bec84a2a2a76554040114` |
| `daemon/internal/app/events.go` | `9a10524db20e065874e512845dab247406bac4d653d35636c1f6d5e69f8cf8f1` |
| `daemon/internal/app/lease.go` | `1eea146f71663d76fba7e5965d7b211de831b7a2af97b48abaca6991c2f59370` |
| `daemon/internal/app/goal_admission.go` | `a1ebf94855bd007503704d675c939d71088e58d4a73335856f4befc03c3bff51` |
| `daemon/internal/app/recovery_glue.go` | `d4b51969697a892865319d2ba628302914b9a57f26175fbc39bb440e85992386` |
| `daemon/internal/app/linux_owner_witness_linux.go` | `c131b3ef60de447ec2189003e4d205a43115ff6285d92cb570a426dcf42050d1` |
| `docs/plans/2026-11-22-rf-w4-17b-run-lifecycle-plan.md` | `eac83e0eee2ee8775e05903f0edc6308844af021f8305081912af24bed96a3e7` |
| `scripts/ci/omo-control-native-evidence.py` | `d55163b436a44df43817bd27623a608a5a72b782eb9445c384bdf21580159ead` |

Scratch tools and raw outputs (outside the repository, host state directory):
`…/scratch/w4-17b/scan/main.go` + `events-head4.tsv` (write/read scan), `…/seq/{main.go,
seq-report.txt}` (per-function write and lock sequences), `…/flow/{main.go, flow-report.txt}`
(control-flow points), `…/callers/{main.go, callers.txt}` (transition callers), `…/fixtures/`
(parser-only fixture rewrite and its proof), `…/fixture-proof.txt`, `…/lifecycle-fields.txt`,
`…/base/` (base-revision sources), `…/mutant-M1.txt` … `mutant-M5.txt`.

## 1. Deliverable shape (files, counts)

| file | status | lines (base → head) | note |
| --- | --- | ---: | --- |
| `daemon/internal/app/run_lifecycle.go` | new | — → 739 | `runLifecycle` (44 fields), `cleanupBlocked()`, `phase()`, 64 transition/query methods |
| `daemon/internal/app/run_lifecycle_test.go` | new | — → 838 | 5 tests at the module interface, no `&daemon{}`; the committed write-site scan gate |
| `daemon/internal/app/native_run.go` | edited | 2417 → 2242 | `runningRun` embeds `runLifecycle`; 44 declarations left; 127 write statements became transition calls; `claimNativeTerminalOwner` deleted (its body is `claimSettlementOwner`) |
| `daemon/internal/app/app.go` | edited | 2221 → 2189 | startAssigned, releaseRun, releaseSlotOnce, scheduleTerminalSlotRelease, beginTerminal, releaseTerminalReservation, clearCancellationReservation, finishStarting, finishAttachedStart, the constructor literal |
| `daemon/internal/app/cleanup.go` | edited | 1047 → 1039 | finalizeExitedProcess, clearCleanupBlockedAfterStop |
| `daemon/internal/app/commands.go` | edited | 939 → 939 | dispatchQueuedCommand, handleCommand |
| `daemon/internal/app/events.go` | edited | 603 → 603 | queueEvent |
| `daemon/internal/app/lease.go` | edited | 585 → 563 | renewLeaseWithTimingAndStart, terminateForLease |
| `daemon/internal/app/goal_admission.go` | edited | 1584 → 1577 | startGoalAdmission, requireNativeSessionCloseRetry, retryBlockedNativeSessionCloses; `nativeCleanupBlocked` deleted (the free function at base lines 1290-1292) |
| `daemon/internal/app/recovery_glue.go` | edited | 894 → 880 | retryNativeUsageRecoveryPersistence |
| `daemon/internal/app/linux_owner_witness_linux.go` | edited | 3843 → 3843 | witness constructor literal |
| `daemon/internal/app/*_test.go` (15 files) | edited | — | 87 fixture literals rewritten into nested `runLifecycle: runLifecycle{…}`; 193 keys moved; assertions unchanged |
| `docs/plans/2026-11-22-rf-w4-17b-run-lifecycle-plan.md` | new | — → 363 | plan with decisions D1-D8 |
| `docs/evidence/rf-w4-17b-run-lifecycle-2026-11-22.md` | new | — | this record |
| `scripts/ci/omo-control-native-evidence.py` | edited | 1812 → 1813 | `SOURCE_PATHS` gains `daemon/internal/app/run_lifecycle.go` (R9) |

Line budget (contract clause 1's "no file > 1500 L"): `run_lifecycle.go` 739 and
`run_lifecycle_test.go` 838 are the only files this slice pushes past 400 lines; every file that
already exceeded 1500 lines at base (`native_run.go`, `app.go`, `goal_admission.go`,
`linux_owner_recovery_linux.go`, `linux_owner_v2_linux.go`, `linux_owner_preflight_linux.go`,
`linux_owner_witness_linux.go`, the test files) shrinks or stays equal — `native_run.go`
2417 → 2242 as the contract requires. No file crosses 1500 lines.

Diff size (measured, for the plan's decision D8): tracked files `+374 / -581 = 955` lines
(`git diff --numstat f70553bf`: production edits ≈ 465, mechanical test-fixture rewrite ≈ 490),
plus the new files `run_lifecycle.go` 739 and `run_lifecycle_test.go` 838 — a code diff of
≈ 2,532 lines — and the two mandated records (plan 363 + this record ≈ 400). That total is over
the conductor's 1,500-line size rule. D8 in the plan records the measurement
and the disposition: the split into W4-17b-1/W4-17b-2 does not bound the diff either (the module
file, the fixture rewrite and the evidence tables are fixed costs of both halves: a b-1 that keeps
the barrier declarations on `runningRun` still needs the module file for the authority transitions
that write barrier flags, the same 87 mixed fixture literals, and its own write/lock/flow tables —
estimated >1,800 lines), and clause 1's field→group map plus clause 3's write-order control bind
the whole field set. Recorded for the conductor; if a split is still required, the plan names the
exact row boundary (G1-G6/G10 plus the G7-G9 declarations in b-1; the declarations move plus rows
35-43 and 48-54 in b-2).

## 2. Native-evidence source mapping (arch-decision-2 R9)

`daemon/internal/app/run_lifecycle.go` is appended to `SOURCE_PATHS` of
`scripts/ci/omo-control-native-evidence.py` (line 99), so the native-evidence identity set
(`source_files`) covers the file this slice's behaviour moved into. Moved declarations → their
listed base home:

| moved declaration (now in `run_lifecycle.go`) | base home (listed source path) |
| --- | --- |
| `runLifecycle` field declarations (44) | `native_run.go` (the `runningRun` struct) |
| `claimSettlementOwner` (was `claimNativeTerminalOwner`) | `native_run.go:334-355` |
| `cleanupBlocked()` (was `nativeCleanupBlocked`) | `goal_admission.go:1290-1292` |
| admission, cancellation, lease-loss, terminal, slot and containment transitions | `app.go`, `commands.go`, `events.go`, `lease.go`, `cleanup.go`, `recovery_glue.go`, `goal_admission.go`, `native_run.go` (the write sites listed in §5) |

## 3. Field map (K1 §6 groups → module)

| group | fields (44 declared in `runLifecycle`) |
| --- | --- |
| admission & attachment | `starting`, `claimed` |
| native start publication | `nativeStartInFlight`, `nativeStartTerminalObserved` |
| cancellation | `cancelled`, `cancelCommandID`, `nativeCancelCommandID`, `nativeCancelTerminalObserved`, `nativeCancelRejected`, `nativeCancelTerminateErr`, `cancel` |
| lease loss | `stale`, `nativeStartLeaseExpired`, `nativeStartLeaseReason`, `leaseRenewalClosed`, `renewCancel`, `renewCancelID` |
| terminal reservation | `terminalizing`, `terminal`, `terminalCancel` |
| native settlement ownership | `nativeTerminalOwner`, `nativeTerminalOwnerClaim`, `nativeTerminalOwnerSequence`, `nativeStartTerminalOwner` |
| usage accounting barrier | `nativeUsageRetryPending`, `nativeUsageRetryInFlight`, `nativeUsageRetryExhausted`, `nativeUsageRetryNeedsPrepare`, `nativeUsageRetryOwnerClaim`, `nativeUsageRetryAt`, `nativeUsageRetryDeadline`, `nativeUsageRetryAttempts`, `nativeUsageRenewalBlocked`, `nativeUsageFinalized` |
| native close retry | `nativeCloseRetryRequired`, `nativeCloseRetrying` |
| process containment finalization | `processExited`, `processFinalizeOwner`, `processFinalizeAttempts`, `processFinalizeExhausted`, `processStopWitness`, `processStopPID`, `processStopIdentity` |
| slot | `slotHeld` |

The 45th §6 field, the cleanup memo `cleanupBlocked`, stays declared on `runningRun` (decision D1:
the mandated predicate method carries the same name, and renaming the memo would textually change
86 test-file lines at base in 17 files — 42 fixture-literal keys and 44 assertion/read lines —
which clause 3 forbids). Every write to it is a
transition in `run_lifecycle.go`; the scan gate's field set includes it, so a write outside the
module fails the gate. Payload fields (90 declarations unchanged, the three mutex objects included)
stay on `runningRun`: `process`, `nativeSession`, `goalSession`, `goalAdmission`, `linuxClone`,
`goalUsage`, `goalUsageAt`, `goalCachedInputTokens`, `goalUsageHasCached`, `goalUsageObservation`,
`nativeDeadline`, `prepared`, `output`, `startFailure`, `nativeFinalResult`, `nativeFinalWaitErr`,
`nativeFinalCloseErr`, `nativeFinalAdmission`, `nativeFinalUsage`, `outputDropWarned`,
`stopExecution`, `localLeaseDeadlineAt`, `leaseSequence`.

## 4. Transitions and callers (clause 2)

64 methods are declared in `run_lifecycle.go`: 55 called from other files, 9 internal-only
(`blockRenewal`, `closeRenewal`, `exhaustUsageRetry`, `markCleanupBlocked`,
`refreshCleanupBlocked`, `releaseStartCancel`, `releaseTerminalCancel`, `cleanupBlocked`,
`phase`). The full table is `…/scratch/w4-17b/callers.txt`; the externally called set:

| transition | callers (file:function) |
| --- | --- |
| `attachClaim` | app.go:startAssigned |
| `finishStarting` | app.go:finishStarting, app.go:startAssigned, goal_admission.go:startGoalAdmission |
| `finishAttachedStart` | app.go:finishAttachedStart, native_run.go:settleCancelledNativeStart, app.go:startAssigned (5 sites), goal_admission.go:startGoalAdmission (2) |
| `beginNativeStartPublication` | native_run.go:beginNativeStart |
| `publishNativeStartResult` | native_run.go:publishNativeStartResult |
| `endNativeStartPublication` | native_run.go:finishNativeStartPublication |
| `endNativeStartPublicationWithRenewal` | native_run.go:settleCancelledNativeStart |
| `transferNativeStartTerminal` | native_run.go:transferNativeStartTerminal |
| `markNativeStartTerminalObserved` | events.go:queueEvent |
| `markCancelled` | commands.go:dispatchQueuedCommand, native_run.go:stopAll |
| `requestCancel` | commands.go:handleCommand (3 sites) |
| `setCancelCommandID` | commands.go:dispatchQueuedCommand, commands.go:handleCommand |
| `clearCancel` | app.go:clearCancellationReservation |
| `recordNativeCancelObserved` | commands.go:handleCommand (2), native_run.go:settleCancelledNativeStart, goal_admission.go:startGoalAdmission |
| `recordNativeCancelTerminateErr` | commands.go:handleCommand (3), native_run.go:settleCancelledNativeStart |
| `markLeaseLostDuringStart` | lease.go:terminateForLease |
| `markLeaseLostWithSettlement` | lease.go:terminateForLease |
| `beginRenewal` / `clearRenewal` | lease.go:renewLeaseWithTimingAndStart |
| `beginTerminalReservation` | commands.go:handleCommand |
| `reserveTerminal` | app.go:beginTerminal |
| `releaseTerminalReservation` | app.go:beginTerminal, app.go:releaseTerminalReservation |
| `publishTerminal` | app.go:beginTerminal |
| `reserveTerminalSlotRelease` | app.go:scheduleTerminalSlotRelease |
| `releaseSlotOnce` | app.go:releaseSlotOnce |
| `retireRun` | app.go:releaseRun |
| `claimSettlementOwner` | commands.go:handleCommand, native_run.go:settleCancelledNativeStart |
| `reserveSettlementOwner` | native_run.go:reserveNativeTerminalClaim |
| `claimObservedSettlementOwner` | events.go:queueEvent, native_run.go:reserveNativeObservedTerminalClaim |
| `claimStartedSettlementOwner` | native_run.go:transferNativeStartTerminal |
| `settleCancelledStartWithObservedTerminal` | native_run.go:settleCancelledNativeStart |
| `releaseSettlementOwner` | native_run.go:releaseNativeTerminalOwner |
| `finalizeConclusiveSettlement` | native_run.go:finalizeConclusiveNativeTerminalOwner |
| `markUsageFinalized` | native_run.go:completeNativeRunAfterUsageOwned (3) |
| `scheduleUsageRetry` | native_run.go:deferNativeUsageRetry |
| `startUsageRetryAttempt` | native_run.go:flushPendingNativeUsage |
| `recordUsageRetryFailure` | native_run.go:noteNativeUsageRetryFailure |
| `finishUsageRetryInFlight` | native_run.go:completeNativeRunAfterUsageOwned |
| `clearUsageRetryBeforePublication` | native_run.go:completeNativeRunAfterUsageOwned |
| `exhaustUsageRetryForCleanup` | native_run.go:exhaustNativeUsageRetryOwned |
| `scheduleRecoveryUsageRetry` | recovery_glue.go:retryNativeUsageRecoveryPersistence |
| `ownUsageRetry` | native_run.go:exhaustNativeUsageRetry |
| `rememberFinalUsage` | native_run.go:rememberNativeFinalUsage |
| `requireCloseRetry` | goal_admission.go:requireNativeSessionCloseRetry |
| `beginCloseRetry` / `endCloseRetry` | goal_admission.go:retryBlockedNativeSessionCloses |
| `publishCloseRetrySuccess` | native_run.go:publishNativeCloseRetrySuccess |
| `markProcessExited` | native_run.go:recordGenericProcessExit |
| `recordProcessStop` | native_run.go:recordGenericProcessExit |
| `takeFinalizeOwnership` | cleanup.go:finalizeExitedProcess |
| `markFinalizeExhausted` | cleanup.go:finalizeExitedProcess (3) |
| `recordFinalizeAttempt` | cleanup.go:finalizeExitedProcess |
| `recordFinalizedStop` | cleanup.go:finalizeExitedProcess |
| `clearCleanupBlock` | cleanup.go:clearCleanupBlockedAfterStop |

## 5. Per-function write-sequence table (base `f70553bf` vs working tree)

Tool: `…/seq/main.go` — per function (receiver-qualified), the ordered list of fields written,
with the base's `claimNativeTerminalOwner` call and the head's `runLifecycle` transitions expanded
inline, so a caller's sequence is compared against the exact writes its transitions perform.
Output `…/seq-report.txt`.

**Result: 851 functions compared, 0 field-write mismatches** (base has 852 functions; one, the
deleted `nativeCleanupBlocked`, has no writes). The touched functions and their measured field-write
counts (identical on both sides):

| function | writes | function | writes |
| --- | ---: | --- | ---: |
| `daemon.startAssigned` | 11 | `daemon.settleCancelledNativeStart` | 37 |
| `daemon.releaseRun` | 10 | `daemon.publishNativeCloseRetrySuccess` | 7 |
| `daemon.releaseSlotOnce` | 2 | `daemon.recordGenericProcessExit` | 13 |
| `daemon.scheduleTerminalSlotRelease` | 1 | `daemon.completeNativeRunAfterUsageOwned` | 14 |
| `daemon.beginTerminal` | 6 | `daemon.deferNativeUsageRetry` | 15 |
| `daemon.releaseTerminalReservation` | 1 | `daemon.flushPendingNativeUsage` | 5 |
| `daemon.clearCancellationReservation` | 2 | `daemon.noteNativeUsageRetryFailure` | 7 |
| `daemon.finishStarting` | 2 | `daemon.exhaustNativeUsageRetry` | 1 |
| `daemon.finishAttachedStart` | 2 | `daemon.exhaustNativeUsageRetryOwned` | 6 |
| `daemon.finalizeExitedProcess` | 12 | `daemon.rememberNativeFinalUsage` | 2 |
| `daemon.clearCleanupBlockedAfterStop` | 1 | `daemon.stopAll` | 1 |
| `daemon.dispatchQueuedCommand` | 2 | `daemon.retryNativeUsageRecoveryPersistence` | 9 |
| `daemon.handleCommand` | 31 | `daemon.attachPartialNativeSession` | 0 |
| `daemon.queueEvent` | 16 | `daemon.queueNativeEvent` | 5 (payload, unchanged) |
| `daemon.startGoalAdmission` | 8 | `daemon.beginNativeStart` | 1 |
| `daemon.requireNativeSessionCloseRetry` | 2 | `daemon.publishNativeStartResult` | 5 |
| `daemon.retryBlockedNativeSessionCloses` | 2 | `daemon.finishNativeStartPublication` | 1 |
| `daemon.renewLeaseWithTimingAndStart` | 3 | `daemon.transferNativeStartTerminal` | 16 |
| `daemon.terminateForLease` | 24 | `daemon.releaseNativeTerminalOwner` | 5 |
| `daemon.reserveNativeTerminalClaim` | 13 | `daemon.finalizeConclusiveNativeTerminalOwner` | 10 |
| `daemon.reserveNativeObservedTerminalClaim` | 15 | `daemon.claimNativeTerminalOwner` → `runningRun.claimSettlementOwner` | 11 |
| `daemon.claimObservedSettlementOwner`… (events/native_run composed sites) | see report | | |

All 49 base functions that write a `runningRun` field are in that set; the two base functions whose
bodies became transitions (`claimNativeTerminalOwner` → `claimSettlementOwner`) are compared through
the recorded move mapping in the tool.

Two recorded refinements where the plan's mapping was corrected against the base text:
1. `dispatchQueuedCommand` (base `commands.go:84-86`): the base writes `cancelled = true` and then
   writes `cancelCommandID` only on the unclaimed path (the `if !active.claimed` guard sits between
   them). The delivered code calls `markCancelled()` at the first position and
   `setCancelCommandID(...)` at the guarded position — `requestCancel(...)` (both writes) would have
   written `cancelCommandID` on the claimed path as well. Per-path write sequences: claimed
   `[cancelled]`, unclaimed `[cancelled, cancelCommandID]`, identical to base.
2. `handleCommand`'s cancel-outcome block: the base's `if active.nativeCancelRejected` chain becomes
   `rejected :=…; if rejected {…}` with `rejected` bound to the base expression immediately before;
   the branch order and the three `recordNativeCancelTerminateErr` calls are unchanged (see §7).

## 6. Lock-site table before/after

Tool: the same `…/seq/main.go`, lock mode (every `daemon.mu` / `daemon.eventReceiptMu` /
`daemon.commandReceiptMu` / `active.inputMu` `Lock`/`Unlock` call per function, in order).
**Result: 850 functions compared, 0 mismatches.** No new mutex exists; `run_lifecycle.go` takes no
lock (asserted by the committed scan test, §10). The touched functions' sequences (base and head are
identical; the head file:line coordinates are in `…/seq-report.txt`):

| function | lock sequence (both revisions) |
| --- | --- |
| `startAssigned` | mu.Lock; mu.Unlock; mu.Lock; mu.Unlock; mu.Lock; mu.Unlock; mu.Lock; mu.Unlock ×2 |
| `releaseRun` | commandReceiptMu.Lock; Unlock; mu.Lock; mu.Unlock |
| `releaseSlotOnce` / `releaseTerminalReservation` / `finishStarting` / `finishAttachedStart` / `clearCancellationReservation` / `beginNativeStart` / `publishNativeStartResult` / `finishNativeStartPublication` / `releaseNativeTerminalOwner` / `finalizeConclusiveNativeTerminalOwner` / `publishNativeCloseRetrySuccess` / `requireNativeSessionCloseRetry` / `rememberNativeFinalUsage` / `stopAll` / `clearCleanupBlockedAfterStop` | mu.Lock; mu.Unlock |
| `scheduleTerminalSlotRelease` | mu.Lock; mu.Unlock; mu.Unlock (two exit paths) |
| `beginTerminal` | mu.Lock; Unlock; Unlock; Lock; Unlock |
| `finalizeExitedProcess` | mu.Lock; Unlock ×4; Lock; Unlock ×3; Lock; Unlock |
| `dispatchQueuedCommand` | mu.Lock; Unlock ×6 |
| `handleCommand` | mu.Lock; Unlock ×9; Lock; Unlock; inputMu.Lock; Unlock ×2; mu.Lock; Unlock |
| `queueEvent` | eventReceiptMu.Lock; mu.Lock; mu.Unlock; eventReceiptMu.Unlock |
| `terminateForLease` | eventReceiptMu.Lock; mu.Lock; Unlock ×3; eventReceiptMu.Unlock ×3 (paired exits) |
| `reserveNativeObservedTerminalClaim` / `settleCancelledNativeStart` | eventReceiptMu/mu interleaving per base, unchanged |
| `renewLeaseWithTimingAndStart` | mu.Lock; Unlock ×4; Lock; Unlock |
| `completeNativeRunAfterUsageOwned` | 17 lock operations incl. `active.inputMu`, unchanged order |
| `flushPendingNativeUsage` / `noteNativeUsageRetryFailure` / `exhaustNativeUsageRetry` / `exhaustNativeUsageRetryOwned` / `deferNativeUsageRetry` / `retryNativeUsageRecoveryPersistence` / `retryBlockedNativeSessionCloses` / `recordGenericProcessExit` | unchanged (rows in the report) |

## 7. Control-flow audit (lesson 32)

Tool: `…/flow/main.go` — per function, the ordered list of `if` conditions, `case`/`default`
clauses, `return` expressions and branch statements with whitespace-normalized text.
**Result: 830 functions compared, 815 identical, 15 differing; every differing function's
difference is one of the documented guard moves or the recorded `handleCommand` binding.**

| function | base control-flow point | head equivalent | status |
| --- | --- | --- | --- |
| `beginTerminal` (closure) | `if active.terminalizing > 0`; `if entered { terminal; leaseRenewalClosed }` | `releaseTerminalReservation()` (guard inside); `if entered { publishTerminal() }` | preserved (guard + writes moved into transitions) |
| `completeNativeRunAfterUsageOwned` | `if !settlementDone && !active.nativeUsageFinalized`; `if !usageRetryCompletion` | inside `finishUsageRetryInFlight`; inside `clearUsageRetryBeforePublication` | preserved |
| `finalizeConclusiveNativeTerminalOwner` | `if active.terminalizing > 0` | inside `releaseSettlementOwner` | preserved |
| `finalizeExitedProcess` | `if active.processFinalizeOwner != process` | inside `takeFinalizeOwnership` | preserved |
| `finishAttachedStart` | `if process == nil && nativeSession == nil`; `return terminal\|\|cancelled` | inside the transition; `return active.finishAttachedStart()` | preserved |
| `finishStarting` | `if cleanupReady && process == nil && nativeSession == nil`; `return terminal` | inside the transition; `return active.finishStarting(cleanupReady)` | preserved |
| `flushPendingNativeUsage` | retry-budget condition | inside `startUsageRetryAttempt` (returns false on exhaustion) | preserved |
| `handleCommand` | `if active.nativeCancelRejected { terminateErr = nil } else if terminalObserved {…} else {…}` | `rejected := <same expression>; if rejected {…} else if terminalObserved {…} else {…}` | preserved (binding, same order) |
| `noteNativeUsageRetryFailure` | retry-budget condition | inside `recordUsageRetryFailure` | preserved |
| `recordGenericProcessExit` | `if processFinalizeOwner != process`; `if stopped` | inside `markProcessExited` (returns the reset flag); inside `recordProcessStop` (returns the stopped flag) | preserved; the two `delete(daemon.cleanupRetry, key)` calls stay in the caller at the base positions |
| `releaseNativeTerminalOwner` | `if active.terminalizing > 0` | inside `releaseSettlementOwner` | preserved |
| `releaseRun` | `if active.nativeTerminalOwner`; `if active.terminalizing > 0` | inside `retireRun` | preserved |
| `retryNativeUsageRecoveryPersistence` | `if attempts < nativeUsageRetryLimit`; `if recoveryAttempt < 1` | inside `scheduleRecoveryUsageRetry` | preserved |
| `terminateForLease` | `if !active.nativeStartLeaseExpired`; `if active.nativeSession != nil` | inside `markLeaseLostDuringStart`; inside `markLeaseLostWithSettlement` (returns `settlementOwned`) | preserved |
| `transferNativeStartTerminal` | `if terminalObserved` | inside the transition | preserved |

`handleCommand`'s other 122 decision points are identical; `releaseSlotOnce`,
`releaseTerminalSlotAt`, `resolveTerminal`, `scheduleCleanup`, `releaseCleanupIfReady`,
`deferStaleCleanupForPendingAcknowledgement`, `deliverAcknowledgement`, `waitForRunResultWithContext`
and `flushRun` are identical too (their `daemon.releaseSlotOnce(key)` calls are the daemon method,
not a transition).

## 8. Fixture-rewrite proof (O6)

Tool: `…/fixtures/main.go` (parser-only). Result in `…/fixture-proof.txt`:

- 183 `runningRun` composite literals across 49 test files (90 explicit + 93 elided inside
  `[]*runningRun` / `map[state.RunKey]*runningRun` containers).
- **87 rewritten** (one per literal that set ≥1 lifecycle field), **193 keys moved** into the nested
  `runLifecycle: runLifecycle{…}` literal, **96 untouched** (every one sets zero lifecycle fields;
  13 are empty `runningRun{}`).
- Per literal: the multiset of (field name → value source text) pairs is identical between base and
  head, nested keys expanded, value whitespace collapsed: **87/87 MATCH, 0 MISMATCH**;
  `nested_key_order_ok=87/87`, `nested_key_set_ok=87/87`,
  `lifecycle_keys_left_at_top_level=0`.
- Residual check: with the 183 literal spans replaced by a placeholder, the pre-rewrite snapshot and
  the rewritten files are **byte-identical for all 49 files** — gofmt's re-alignment never escaped a
  rewritten literal, and no other text changed (`…/residual-wt-final.txt`).
- `gofmt -l` over the 19 rewritten files: empty.

Per-file counts (rewritten literals / moved keys / untouched): app_test.go 29/43/52,
native_settlement_regression_test.go 16/54/0, harness_test.go 9/18/7,
lease_stale_persistence_test.go 6/15/0, native_settlement_final_regression_test.go 4/17/1,
goal_delivery_test.go 3/3/13, native_process_stop_test.go 3/10/1, process_cleanup_test.go 3/3/5,
deterministic_validation_test.go 2/3/1, linux_owner_v2_app_test.go 2/4/3,
native_lease_settlement_test.go 2/4/0, atomic_terminal_ack_interleaving_test.go 1/4/0,
cleanup_test.go 1/1/0, generic_process_stop_test.go 1/1/0, process_finalization_test.go 1/1/3,
recovery_isolation_test.go 1/2/0, stale_ack_settlement_test.go 1/2/0, supervisory_test.go 1/1/0,
terminal_settlement_race_test.go 1/7/1.

Test-side fixture **state construction by direct field writes** (26 sites, e.g.
`fixture.daemon.running[key].stale = true`) is unchanged: the gate's scope is the package's
non-test source (plan decision D5, K1 scanner parity), and no transition expresses those writes
without inventing test-only setters. The 29 scan hits include 3 same-named fields of other test
types (`control.cancel`, `client.terminal`, `session.cancelled`); the site list is
`…/events-head4.tsv`.

## 9. Red mutants

One mutant per new test, applied alone, the target test run, then reverted (raw outputs in
`…/mutant-M*.txt`):

| # | test | mutant | observed failure |
| --- | --- | --- | --- |
| M1 | `TestRunLifecycleTransitionSequences` | delete `run.cancelCommandID = commandID` in `requestCancel` | `run_lifecycle_test.go:175: requestCancel = (cancelled=true, id=""), want (true, command-1)` |
| M2 | `TestRunLifecycleCleanupBlockedTruthTable` | drop `state.nativeCloseRetrying` from `cleanupBlocked()` | `run_lifecycle_test.go:638: cleanupBlocked() = false for {nativeCloseRetrying}, want true` |
| M3 | `TestRunLifecyclePhaseTable` | disable the `terminalizing > 0` case (`false && …`) | `run_lifecycle_test.go:671: phase() = "lease-lost", want "reserving"` |
| M4 | `TestRunLifecycleUnmodelledStatesStayWritableAndDetectable` | disable the orphaned-retry reset condition in `claimSettlementOwner` | `run_lifecycle_test.go:544: the orphaned retry survived the claim: pending=true inFlight=true exhausted=true ownerClaim=9` |
| M5 | `TestRunLifecycleWritesStayInTheModule` | add `active.stale = false` in `daemon.isStale` (`app.go`) | `run_lifecycle_test.go:767: lifecycle fields written outside run_lifecycle.go: app.go:1853 writes stale` |

M1's first attempt survived the original test (the assertion only covered the post-`clearCancel`
state); the test was extended with the intermediate reservation assertion (not weakened) and the
mutant is red in the delivered test. All five mutants were reverted; the tree carries none
(`grep -rn "false &&" daemon/internal/app/*.go` → no match).

## 10. Gate results (exact commands, counts, exit codes)

| gate | command (cwd) | result |
| --- | --- | --- |
| gofmt | `gofmt -l internal/app/` (`daemon/`) | no output, exit 0 |
| vet | `go vet ./internal/app/`, then `go vet ./...` (`daemon/`) | exit 0, exit 0 |
| build linux | `GOOS=linux go build ./...` (`daemon/`) | exit 0 |
| build windows | `GOOS=windows go build ./...` (`daemon/`) | exit 0 |
| build linux/arm64 (extra) | `GOOS=linux GOARCH=arm64 go build ./...` (`daemon/`) | exit 0 |
| full package suite (windows) | `go test -count=1 ./internal/app/...` (`daemon/`) | first run `ok … 248.807s` (exit 0) mid-stage; re-run on the frozen revision above: `ok github.com/wxxb789/symmetry/daemon/internal/app 485.816s`, exit 0 (slower because the WSL VM held host memory). Both green |
| race attempt (windows) | `go test -count=1 -race ./internal/app/` (`daemon/`) | refused: `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1` — the race oracle is the WSL run, as at W4-17a |
| focused touched-path tests (windows) | `go test -count=1 -v -run 'TestRunLifecycle\|TestReleaseRun\|TestBeginTerminal\|TestTerminateForLease\|TestRecordGenericProcessExit\|TestFinalizeExitedProcess\|TestRecordFinalizedStop\|TestNativeUsageRetry\|TestNativeUsageRecovery\|TestSettleCancelled' ./internal/app/` | `ok … 1.581s`, exit 0, 44 `--- PASS` lines, 0 failures |
| focused module tests | `go test -count=1 -run 'TestRunLifecycle' ./internal/app/` | `ok … 0.394s`, exit 0; 5 tests — 18 transition-sequence rows, 4 unmodelled-state cases, the 64-row `cleanupBlocked()` truth table, the 16-row `phase()` table and the scan gate — 0 failures |
| write-site scan (committed) | `TestRunLifecycleWritesStayInTheModule` (part of the focused run) | pass: 0 writes outside `run_lifecycle.go`, field set exact, `runningRun` embeds `runLifecycle`, module has no `Lock(` call |
| write-site scan (scratch, all 45 names) | `…/scan/main.go` over `daemon/internal/app` | 0 non-test writes outside `run_lifecycle.go` |
| per-function write sequences | `…/seq/main.go` | 851 compared, 0 mismatches |
| lock sequences | `…/seq/main.go` | 850 compared, 0 mismatches |
| control-flow audit | `…/flow/main.go` | 830 compared, 815 identical, 15 differing — all mapped in §7 |
| string-literal multiset, touched production files | per-file `grep -ohE '"[^"]*"'` + `sort` + `diff`, base vs head | `cleanup.go`, `commands.go`, `events.go`, `lease.go`, `goal_admission.go`, `recovery_glue.go`, `linux_owner_witness_linux.go`: 0 differences; `native_run.go` and `app.go`: 1 each — the removed empty-string assignments `processStopIdentity = ""` and `cancelCommandID = ""`, whose writes now live in the module. No error, log or state-name text changed |
| fixture proof (O6) | `…/fixtures/main.go` proof mode | 87/87 MATCH, 0 MISMATCH |
| native-evidence identity (R9) | `python3 scripts/ci/test-omo-control-native-evidence.py` (repo root) | `Ran 10 tests in 3.227s`, `OK`, exit 0 |
| whitespace | `git diff --check` | exit 0 |
| host-path / DLP | `git diff f70553bf \| grep '^+' \| grep -i "us[e]rs/"`; `… \| grep -P '\\[u]'`; the same two patterns over the four new files | no matches (exit 1 each, 0 hits per file). One plan line carried a scratch-tool path under the operator's profile directory while this record was drafted (`<HOST-PATH>/scratch/w4-17b/scan/main.go` today); it was respelled with the `<HOST-PATH>` placeholder, which is why the plan's hash above is its final value |

### WSL race gate (the contract's `-race` oracle)

| gate | command (WSL Ubuntu-24.04) | result |
| --- | --- | --- |
| race, full contract gate | `go test -count=1 -race -timeout 25m ./internal/app/... ./internal/execution/...` | `ok github.com/wxxb789/symmetry/daemon/internal/execution 21.908s`; `FAIL github.com/wxxb789/symmetry/daemon/internal/app 1500.147s` — `panic: test timed out after 25m0s`, goroutine dumps in `net/http.(*Transport).startDialConnForLocked` / `transport.go:1695-1715`, 0 `DATA RACE` lines. The stall is the pre-existing loopback-`httptest` hang W4-17a attributed to this host at base (`TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart`); `ps` inside WSL showed `app.test` at 0.0 % CPU after 23 minutes, i.e. a blocked dial, not slow progress. Exit status 1, caused by the untouched test set's stall |
| race, hang attribution (HEAD) | `go test -count=1 -race -timeout 3m ./internal/app/ -run '^TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart$'` | `panic: test timed out after 3m0s`, `running tests:` (the single selected test), `FAIL … 180.207s` — the same artifact W4-17a recorded at base `79548368`, so the stall is host-pre-existing and not caused by this slice |
| race, touched-path subset | `go test -count=1 -race -timeout 20m ./internal/app/ ./internal/execution/... -run 'TestRunLifecycle\|TestReleaseRun\|TestBeginTerminal\|TestTerminateForLease\|TestRecordGenericProcessExit\|TestFinalizeExitedProcess\|TestRecordFinalizedStop\|TestNativeUsageRetry\|TestNativeUsageRecovery\|TestSettleCancelled\|TestCancel\|TestStale\|TestCloseRetry\|TestNativeStart\|TestConclusive\|TestReserve\|TestLeaseExpiry\|TestLeaseOwned'` (62 selected top-level tests, `go test -list`) | `ok github.com/wxxb789/symmetry/daemon/internal/app 24.961s` and `ok github.com/wxxb789/symmetry/daemon/internal/execution 1.127s [no tests to run]`, exit 0; 0 `DATA RACE` lines — the touched lifecycle paths (all five module tests included) run clean under the race detector |

## 11. Scoped simplification review (repo AGENTS.md, before completion)

Reviewed the module against "remove an abstraction only when required behavior and clarity
survive". 64 declared methods: 55 called from other files, 9 internal steps
(`blockRenewal`, `closeRenewal`, `exhaustUsageRetry`, `markCleanupBlocked`,
`refreshCleanupBlocked`, `releaseStartCancel`, `releaseTerminalCancel`) or queries
(`cleanupBlocked`, `phase`). No method is unused, no method is a wrapper without its own write
cluster, and no field is written only by a composition that another method already covers:

- the four settlement-owner claims (`claimSettlementOwner`, `reserveSettlementOwner`,
  `claimObservedSettlementOwner`, `claimStartedSettlementOwner`,
  `settleCancelledStartWithObservedTerminal`) exist because their base clusters write different
  companions in different orders (renewal invalidation, terminal observation, renewal block, memo
  refresh) — merging them would either reorder writes or drop a write on one path;
- `markCancelled` + `setCancelCommandID` are required by `dispatchQueuedCommand`, where the base
  guards the `cancelCommandID` write with `!active.claimed` (§5, refinement 1);
- `clearRenewal` (drop the handle) and `invalidateRenewal` (drop it and bump the id) are distinct
  base clusters; `releaseStartCancel`/`releaseTerminalCancel` are each used at two sites;
- `exhaustUsageRetry` is the shared four-write step of two base sites;
  `markCleanupBlocked`/`refreshCleanupBlocked` split the forced and derived memo writes.

No abstraction was removed and no code changed by this review, so no check needed repeating; the
gate set above is the post-review state.

## 12. Known limits

- The Windows host cannot run `-race` (no cgo toolchain); the race evidence is the WSL run.
- The write-site scan gate covers the package's non-test source (D5, K1 parity); the 26 test-side
  fixture-state writes and the 3 same-named fields of other test types are enumerated, not gated.
- Test-side rows in §8 were produced by a parser-only tool; it proves field→value equality per
  rewritten literal and byte-equality outside the rewritten spans, not type correctness (the
  compiler plus the full suite cover that).
- This slice moves no effect across an unlock (O7 reversed); the deferred behaviour slice for the
  post-unlock moves remains out of scope, as does the K1 `daemon.mu` region inventory's
  registry-coupled work.
