# rf-w4-17b run-lifecycle module — evidence record (2026-11-22)

Subject: `runningRun`'s lifecycle state moved into a new `runLifecycle` value with explicit, guarded
transitions in a new `daemon/internal/app/run_lifecycle.go`; every lifecycle write in the package's
non-test source resolves there, one derived `cleanupBlocked()` predicate replaces
`nativeCleanupBlocked`, and one `phase()` query derives the collapsed K1 phase. Contract:
`goal-rf-w4-17b-run-lifecycle.md` (GDD, 2026-11-21, with the arch-decision-2 `## Conductor
decision` sections and amendments V1-V10). Base revision `f70553bf` (the dispatch HEAD), which is
also the revision of every base coordinate below. Plan:
`docs/plans/2026-11-22-rf-w4-17b-run-lifecycle-plan.md`.

Revisions: the reviewed slice was committed as `e69f6566` (branch) and merged with `origin/main` as
`c5b4a0a9`; the fix round for that review's findings F1-F6 re-opened the working tree on top of
`c5b4a0a9`. The identity table below binds the fix-round tree: the fix round changed
`run_lifecycle_test.go` and the two records (this one included); of the identity rows,
`run_lifecycle_test.go` and the plan carry their new hashes, while every other
row is byte-identical to the committed revision (no production file changed in the fix round, so
§5-§7's write-order, lock-site and control-flow measurements still bind the committed bytes), and
§10's fix-round table names the gates re-run on the fix tree.

Subject identity (sha256 of the delivered files):

The apply revision re-hashed `run_lifecycle.go`, `run_lifecycle_test.go`, `native_run.go` and this
slice's plan (the files its accepted simplify findings changed); the fix round then re-hashed
`run_lifecycle_test.go` and the plan again.

| file | sha256 |
| --- | --- |
| `daemon/internal/app/run_lifecycle.go` | `89a15f45115710dfc63381f76e48812b320c0bf12b57e4a6273b94c86f882392` |
| `daemon/internal/app/run_lifecycle_test.go` | `293ee5ba0bc58ef8d32d0f7e1e93414320907fb1f763cd9be6da00bd003fb372` |
| `daemon/internal/app/native_run.go` | `600a367c6f1f5c97f6dfe9719342d62f8ce178add7f3c721bb19f5c538a4a7e7` |
| `daemon/internal/app/app.go` | `efc3c46887497a4f9c5dfda391585fe7316b26efcdb7f0243b8de947232fbfef` |
| `daemon/internal/app/cleanup.go` | `6f54a16546ad109a5e45c825543f4773c0c773ab14450af82b0ee147b62da388` |
| `daemon/internal/app/commands.go` | `57d2feb07937291a1863cab9fe69f1fa44dbced2a54bec84a2a2a76554040114` |
| `daemon/internal/app/events.go` | `9a10524db20e065874e512845dab247406bac4d653d35636c1f6d5e69f8cf8f1` |
| `daemon/internal/app/lease.go` | `1eea146f71663d76fba7e5965d7b211de831b7a2af97b48abaca6991c2f59370` |
| `daemon/internal/app/goal_admission.go` | `a1ebf94855bd007503704d675c939d71088e58d4a73335856f4befc03c3bff51` |
| `daemon/internal/app/recovery_glue.go` | `d4b51969697a892865319d2ba628302914b9a57f26175fbc39bb440e85992386` |
| `daemon/internal/app/linux_owner_witness_linux.go` | `c131b3ef60de447ec2189003e4d205a43115ff6285d92cb570a426dcf42050d1` |
| `docs/plans/2026-11-22-rf-w4-17b-run-lifecycle-plan.md` | `f3ed2312eba7c6c174e1a7a00300188736174c3885e73636fb92f7af32ea4209` |
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
| `daemon/internal/app/run_lifecycle.go` | new | — → 585 | `runLifecycle` (44 fields), `cleanupBlocked()`, `phase()`, 63 transition/query methods |
| `daemon/internal/app/run_lifecycle_test.go` | new | — → 1040 (816 at the apply revision) | 5 tests at the module interface, no `&daemon{}`; the committed write-site scan gate |
| `daemon/internal/app/native_run.go` | edited | 2417 → 2232 | `runningRun` embeds `runLifecycle`; 44 declarations left; 127 write statements became transition calls; `claimNativeTerminalOwner` deleted (its body is `claimSettlementOwner`) |
| `daemon/internal/app/app.go` | edited | 2221 → 2189 | startAssigned, releaseRun, releaseSlotOnce, scheduleTerminalSlotRelease, beginTerminal, releaseTerminalReservation, clearCancellationReservation, finishStarting, finishAttachedStart, the constructor literal |
| `daemon/internal/app/cleanup.go` | edited | 1047 → 1039 | finalizeExitedProcess, clearCleanupBlockedAfterStop |
| `daemon/internal/app/commands.go` | edited | 939 → 931 | dispatchQueuedCommand, handleCommand |
| `daemon/internal/app/events.go` | edited | 603 → 598 | queueEvent |
| `daemon/internal/app/lease.go` | edited | 585 → 563 | renewLeaseWithTimingAndStart, terminateForLease |
| `daemon/internal/app/goal_admission.go` | edited | 1584 → 1577 | startGoalAdmission, requireNativeSessionCloseRetry, retryBlockedNativeSessionCloses; `nativeCleanupBlocked` deleted (the free function at base lines 1290-1292) |
| `daemon/internal/app/recovery_glue.go` | edited | 894 → 880 | retryNativeUsageRecoveryPersistence |
| `daemon/internal/app/linux_owner_witness_linux.go` | edited | 3843 → 3843 | witness constructor literal |
| `daemon/internal/app/*_test.go` (19 files) | edited | — | 87 fixture literals rewritten into nested `runLifecycle: runLifecycle{…}`; 193 keys moved; assertions unchanged |
| `docs/plans/2026-11-22-rf-w4-17b-run-lifecycle-plan.md` | new | — → 387 (376 at the apply revision) | plan with decisions D1-D8 |
| `docs/evidence/rf-w4-17b-run-lifecycle-2026-11-22.md` | new | — | this record |
| `scripts/ci/omo-control-native-evidence.py` | edited | 1812 → 1813 | `SOURCE_PATHS` gains `daemon/internal/app/run_lifecycle.go` (R9) |

Line budget (contract clause 1's "no file > 1500 L"): `run_lifecycle.go` 585 and
`run_lifecycle_test.go` 1040 are the only files this slice pushes past 400 lines; every file that
already exceeded 1500 lines at base (`native_run.go`, `app.go`, `goal_admission.go`,
`linux_owner_recovery_linux.go`, `linux_owner_v2_linux.go`, `linux_owner_preflight_linux.go`,
`linux_owner_witness_linux.go`, the test files) shrinks or stays equal — `native_run.go`
2417 → 2232 as the contract requires. No file crosses 1500 lines.

Diff size (measured at the fix round, for the plan's decision D8; the apply-revision figures in
parentheses): tracked files
`+371 / -588 = 959` lines (`git diff --numstat bced8d36`, the slice's merge base: production edits
and the script 470, mechanical test-fixture rewrite 489), plus the new files `run_lifecycle.go`
585 and `run_lifecycle_test.go` 1040 (816) — a code diff of 2,584 lines (2,360) — and the two
mandated records
(plan 387 + this record 533; 849 at the apply revision). That total is over
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

63 methods are declared in `run_lifecycle.go`: 55 called from other files, 8 internal-only
(`blockRenewal`, `closeRenewal`, `exhaustUsageRetry`, `markCleanupBlocked`,
`refreshCleanupBlocked`, `releaseTerminalCancel`, `cleanupBlocked`,
`phase`). `releaseStartCancel` had a single call site (`markLeaseLostDuringStart`, same file), so
the apply stage inlined its two writes there and deleted the method (simplify S9);
`releaseTerminalCancel` keeps its two sites (`releaseSlotOnce`, `retireRun`). The full table is
`…/scratch/w4-17b/callers.txt`; the externally called set:

| transition | callers (file:function) |
| --- | --- |
| `attachClaim` | app.go:startAssigned |
| `finishStarting` | transition callers: app.go:finishStarting (wrapper, `:2111`), run_lifecycle.go:finishAttachedStart (S7 composition, `:159`); wrapper call sites: app.go:startAssigned (`:1332`), goal_admission.go:startGoalAdmission (`:335`) |
| `finishAttachedStart` | transition caller: app.go:finishAttachedStart (wrapper, `:2123`); wrapper call sites: app.go:startAssigned (`:1450`, `:1598`, `:1642`, `:1664`, `:1674`), goal_admission.go:startGoalAdmission (`:795`, `:1218`), native_run.go:settleCancelledNativeStart (`:659`) |
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
deleted `nativeCleanupBlocked`, has no writes; re-run at the apply revision in
`…/seq-report-apply.txt`: 851 compared, 0 mismatches). The touched functions and their measured field-write
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
**Result: 850 functions compared, 0 mismatches** (re-run at the apply revision in
`…/seq-report-apply.txt`: 850 compared, 0 mismatches). No new mutex exists; `run_lifecycle.go` takes no
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
**Result: 830 functions compared, 815 identical, 15 differing** (re-run at the apply revision in
`…/flow-report-apply.txt`: 830 compared, 15 differing, the same function set); every differing function's
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

Tool: `…/fixtures/main.go` (parser-only). Result in `…/fixture-proof.txt`; re-run at the apply
revision in `…/fixture-proof-apply.txt` (same totals: 87 rewritten, 193 moved, 96 untouched, 183
literals, 0 mismatches):

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

Test-side fixture **state construction by direct field writes** (25 of the 28 raw scan hits, e.g.
`fixture.daemon.running[key].stale = true`) is unchanged: the gate's scope is the package's
non-test source (plan decision D5, K1 scanner parity), and no transition expresses those writes
without inventing test-only setters. The 28 scan hits include 3 same-named fields of other test
types (`control.cancel`, `client.terminal`, `session.cancelled`); the site list is
`…/events-head4.tsv`.

## 9. Red mutants

One mutant per new test, applied alone, the target test run, then reverted. M1-M5 were recorded at
the pre-apply revision and re-run at the apply revision (the quoted line numbers are the apply
ones); M6-M12 are the apply stage's mutants for the assertions the simplify findings added (S10,
S12, S13):

| # | test | mutant | observed failure |
| --- | --- | --- | --- |
| M1 | `TestRunLifecycleTransitionSequences` | delete `run.cancelCommandID = commandID` in `requestCancel` | `run_lifecycle_test.go:171: requestCancel = (cancelled=true, id=""), want (true, command-1)` |
| M2 | `TestRunLifecycleCleanupBlockedTruthTable` | drop `state.nativeCloseRetrying` from `cleanupBlocked()` | `run_lifecycle_test.go:624: cleanupBlocked() = false for {nativeCloseRetrying}, want true` |
| M3 | `TestRunLifecyclePhaseTable` | disable the `terminalizing > 0` case (`false && …`) | `run_lifecycle_test.go:655: phase() = "lease-lost", want "reserving"` (subtest `reserving`) |
| M4 | `TestRunLifecycleUnmodelledStatesStayWritableAndDetectable` | disable the orphaned-retry reset condition in `claimSettlementOwner` | `run_lifecycle_test.go:537: the orphaned retry survived the claim: pending=true inFlight=true exhausted=true ownerClaim=9` |
| M5 | `TestRunLifecycleWritesStayInTheModule` | add `active.stale = false` in `daemon.isStale` (`app.go`) | `run_lifecycle_test.go:747: lifecycle fields written outside run_lifecycle.go:` + `app.go:1852 writes stale` |
| M6 | `…TransitionSequences` (`admission/finish_starting_keeps_session_obligation`) | delete the `refreshCleanupBlocked()` call in `finishStarting` | `run_lifecycle_test.go:109: finishStarting blocked cleanup without an obligation` |
| M7 | `…TransitionSequences` (`close_retry/require_begin_end_publish`) | delete `run.goalAdmission = nil` in `publishCloseRetrySuccess` | `run_lifecycle_test.go:435: publishCloseRetrySuccess kept the session identity` |
| M8 | `…TransitionSequences` (`native_start_publication/transfer_terminal_to_settlement`) | delete `run.nativeDeadline = time.Time{}` in `transferNativeStartTerminal` | `run_lifecycle_test.go:151: transferNativeStartTerminal kept start or stale state: inFlight=false stale=false deadline=2026-11-21 18:42:36.1261409 +0000 UTC` |
| M9 | `…TransitionSequences` (`containment/stop_without_witness_stays_detectable`) | delete `run.processStopWitness = nil` in `recordProcessStop` | `run_lifecycle_test.go:494: recordProcessStop wrote a witness for a process that did not stop` |
| M10 | `…TransitionSequences` (`admission/finish_starting_keeps_session_obligation`) | `return !run.terminal` in `finishStarting` | `run_lifecycle_test.go:100: finishStarting reported a terminal run for a fresh start` |
| M11 | `…TransitionSequences` (`admission/attach_claim_and_finish_attached_start`) | `return !run.terminal \|\| run.cancelled` in `finishAttachedStart` | `run_lifecycle_test.go:76: finishAttachedStart reported a terminal run for a live attach` |
| M12 | `TestRunLifecycleWritesStayInTheModule` | add `run.cancel()` in `beginTerminalReservation` | `run_lifecycle_test.go:770: the module takes a lock or invokes a captured handle:` + `run_lifecycle.go:283 calls cancel` |

M1's first attempt survived the original test (the assertion only covered the post-`clearCancel`
state); the test was extended with the intermediate reservation assertion (not weakened) and the
mutant is red in the delivered test. All mutants were reverted; the tree carries none
(`grep -rn "false &&" daemon/internal/app/*.go` → no match). M6-M12 cover the assertions the apply
stage added: the four module-test rows whose fixtures left the probed field at its zero value
(M6-M9, simplify S12), the two discarded transition returns (M10-M11, S13), and the scan gate's
captured-handle scope (M12, S10; the pre-apply gate, which matched lock calls only, passed M12).

### Fix-round mutants (review fix r1)

Run in the `%TEMP%/w4fix/daemon` copy of the fix-round tree; each mutant applied alone, the module
tests or the scan gate run, then reverted. M13-M16 are the step-level assertions review F2 asked
for (the mutants the apply-stage rows let through); M17-M21 verify the widened gate's new shapes
(review F1), with M21 re-running M5's plain-field write against the rewritten scanner:

| # | test | mutant | observed failure |
| --- | --- | --- | --- |
| M13 | `…TransitionSequences` (`usage_barrier/schedule_attempt_fail_republish`) | empty `finishUsageRetryInFlight` body | `run_lifecycle_test.go:434: finishUsageRetryInFlight = (inFlight=true pending=false atZero=false ownerClaim=1), want (false, true, true, 1)` |
| M14 | `…TransitionSequences` (`close_retry/require_begin_end_publish`) | empty `requireCloseRetry` body | `run_lifecycle_test.go:526: requireCloseRetry = (required=false cleanupBlocked=false), want (true, true)` |
| M15 | same row | empty `endCloseRetry` body | `run_lifecycle_test.go:538: endCloseRetry = (retrying=true required=true), want (false, true)` |
| M16 | same row | empty `beginCloseRetry` body | `run_lifecycle_test.go:532: beginCloseRetry = (retrying=false required=true), want (true, true)` |
| M17 | `TestRunLifecycleWritesStayInTheModule` | `active.runLifecycle = runLifecycle{}` in `daemon.isStale` | `app.go:1853 replaces the whole runLifecycle value` |
| M18 | same test | `pointer := &active.stale; *pointer = false` in `daemon.isStale` | `app.go:1853 escapes &stale` |
| M19 | same test | `_ = &runningRun{runLifecycle: runLifecycle{}}` in `daemon.isStale` | `app.go:1852 constructs a runLifecycle value outside the allowlist (owner "isStale")` |
| M20 | same test | `run.stopExecution(nil)` in `runLifecycle.beginTerminalReservation` | `run_lifecycle.go:282 calls stopExecution` |
| M21 | same test | re-run of M5's `active.stale = false` in `daemon.isStale` | `app.go:1853 writes stale` |

Every mutant is red in the fix-round tree and none was left applied (`git status --short` after the
battery: only `daemon/internal/app/run_lifecycle_test.go` modified; the temp copy is
`diff -rq`-identical to the repository afterwards).

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

### Apply-revision re-run (accepted simplify findings S1-S13)

Every gate above that those findings can affect was re-run on the apply revision (the tree the
subject hashes bind); command strings are the rows above unless noted.

| gate | apply-revision result |
| --- | --- |
| gofmt | `gofmt -l internal/app/` (`daemon/`): no output, exit 0 |
| vet | `go vet ./internal/app/`, `go vet ./...` (`daemon/`): exit 0, exit 0 |
| builds | `GOOS=linux`, `GOOS=windows`, `GOOS=linux GOARCH=arm64` `go build ./...` (`daemon/`): exit 0 each |
| full package suite (windows) | `go test -count=1 ./internal/app/...`: `ok github.com/wxxb789/symmetry/daemon/internal/app 296.579s`, exit 0 |
| focused module tests | `go test -count=1 -run 'TestRunLifecycle' ./internal/app/`: `ok … 0.357s`, exit 0; 5 tests (18 transition rows, 4 unmodelled-state cases, the 64-row truth table, the 16-row phase table, the scan gate), 0 failures |
| race attempt (windows) | `go test -count=1 -race ./internal/app/`: refused, `go: -race requires cgo; enable cgo by setting CGO_ENABLED=1`, exit 2 |
| WSL race, touched-path subset | `go test -count=1 -race -timeout 20m ./internal/app/ ./internal/execution/... -run '<the 18-pattern subset of the row above>'`: `ok github.com/wxxb789/symmetry/daemon/internal/app 28.727s`; `ok github.com/wxxb789/symmetry/daemon/internal/execution 1.399s [no tests to run]`, exit 0, 0 `DATA RACE` lines. The full WSL contract gate was not re-run: its failure mode at the pre-apply revision was the host-pre-existing `TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart` loopback stall (rows above), and no apply edit touches that path |
| write-site scan (scratch, 45 names) | `…/scan/main.go` → `events-apply.tsv`: 0 non-test lifecycle writes outside `run_lifecycle.go`; the module carries 141 write statements (145 pre-apply: `finishAttachedStart`'s `starting` and `markProcessExited`'s three counters are now written by `finishStarting`/`takeFinalizeOwnership`, and the inlined `releaseStartCancel` capture nets zero) |
| per-function write sequences | `…/seq/main.go` → `seq-report-apply.txt`: 851 compared, 0 mismatches |
| lock sequences | `…/seq/main.go` → `seq-report-apply.txt`: 850 compared, 0 mismatches |
| control-flow audit | `…/flow/main.go` → `flow-report-apply.txt`: 830 compared, 815 identical, 15 differing (the same 15 functions) |
| fixture proof (O6) | `…/fixtures/main.go proof -exclude run_lifecycle_test.go` → `fixture-proof-apply.txt`: 87 rewritten literals, 193 moved keys, 96 untouched, 183 literals, 0 mismatches; nested order/set 87/87 |
| native-evidence identity (R9) | `python3 scripts/ci/test-omo-control-native-evidence.py` (repo root): `Ran 10 tests in 3.645s`, `OK`, exit 0 |
| string-literal multiset | per-file `grep -ohE '"[^"]*"'` + `sort` + `diff`, base `f70553bf` vs the apply tree: unchanged (`native_run.go` and `app.go` one difference each) |
| whitespace | `git diff --check`: exit 0 |
| host-path / DLP | `git diff bced8d36 \| grep '^+' \| grep -ci "us[e]rs/"` → 0; `… \| grep -Pc '\\[u]'` → 0 |
| line budget | every changed file that already exceeded 1500 lines at base shrinks or stays equal (`native_run.go` 2417 → 2232; `scripts/ci/omo-control-native-evidence.py` 1812 → 1813 for the R9 `SOURCE_PATHS` line); `run_lifecycle.go` 585 and `run_lifecycle_test.go` 816 are the only files this slice pushes past 400 lines, and no file crosses the 1500-line limit |
| mutant battery | §9 M1-M5 (re-run) and M6-M12 (new): each red as quoted, each reverted; the mutant edits themselves were all reverted (the tree carries none), and the apply-stage changes stayed on the three intended files (`run_lifecycle.go`, `run_lifecycle_test.go`, `native_run.go`), whose bytes the subject table binds |

### Fix-round re-run (review fix r1, findings F1-F6)

Run in the repository's `daemon/` on the fix-round tree (working tree on top of `c5b4a0a9`); the
full package suite is the conductor's gate and was not re-run here. The fix round changed no
production file, so only the gates whose subject changed (the module tests and the scan gate) plus
the cheap front-of-line gates were re-run.

| gate | fix-round result |
| --- | --- |
| gofmt | `gofmt -l internal/app/`: no output, exit 0 |
| vet | `go vet ./...`: exit 0 |
| builds | `GOOS=linux go build ./...`, `GOOS=windows go build ./...`: exit 0 each |
| focused module tests | `go test -count=1 -run 'TestRunLifecycle' ./internal/app/`: `ok … 0.584s`, exit 0; 5 tests (21 transition rows, 4 unmodelled-state cases, the 64-row truth table, the 16-row phase table, the scan gate), 0 failures |
| write-site scan (committed) | the rewritten `TestRunLifecycleWritesStayInTheModule` in the same run: 0 sites outside `run_lifecycle.go`; the two `runLifecycle:` construction keys resolve to their allowlisted owners (`app.go:startAssignment`, `linux_owner_witness_linux.go:runFresh`); the module invokes none of the function-typed handles |
| native-evidence identity (R9) | `python scripts/ci/test-omo-control-native-evidence.py` (repo root): `Ran 10 tests in 5.878s`, `OK`, exit 0 |
| fix-round mutants | §9 fix-round table M13-M21: each red as quoted, each reverted; `git status --short` afterwards shows only `daemon/internal/app/run_lifecycle_test.go` modified |
| line budget | the fix round grows `run_lifecycle_test.go` 816 → 1,040 and the plan 376 → 387 (this record 473 → 533); `run_lifecycle.go` 585 and every production file unchanged; no file crosses the 1,500-line limit |
| whitespace | `git diff --check`: exit 0 |
| host-path / DLP | `git diff c5b4a0a9 \| grep -i "us[e]rs/"` → 0; `… \| grep -P '\\[u]'` → 0 |

## 11. Scoped simplification review (repo AGENTS.md, before completion)

Reviewed the module against "remove an abstraction only when required behavior and clarity
survive". 63 declared methods: 55 called from other files, 8 internal steps
(`blockRenewal`, `closeRenewal`, `exhaustUsageRetry`, `markCleanupBlocked`,
`refreshCleanupBlocked`, `releaseTerminalCancel`) or queries
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
  base clusters; `releaseTerminalCancel` is used at two sites, and `releaseStartCancel` had a
  single one, so the apply stage inlined it into `markLeaseLostDuringStart` (S9);
- `exhaustUsageRetry` is the shared four-write step of two base sites;
  `markCleanupBlocked`/`refreshCleanupBlocked` split the forced and derived memo writes.

No abstraction was removed and no code changed by this review, so no check needed repeating; the
gate set above is the post-review state. The apply stage then applied the accepted simplify
findings: the comment deletions S1-S6 (179 → 41 comment lines in `run_lifecycle.go`, 33 → 5 in
`run_lifecycle_test.go`, per the binding comment policy), the three behaviour-preserving
compositions S7 (`finishAttachedStart` calls `finishStarting`), S8 (`markProcessExited` calls
`takeFinalizeOwnership`) and S9 (inlined `releaseStartCancel`), the widened scan gate S10, the
wording fix S11, and the module-test strengths S12/S13 (seeded fixtures, asserted returns). S14
(removing the two unreachable `t.Failed()` guards) was skipped at apply time; the fix round applied
it (review F6: every step and check reports through `t.Fatalf`, so the guards could never fire).
`phase()` is recorded as test-only
in §12 (S18). The write-order, lock-site and control-flow comparisons, the fixture proof and the
scan gate were re-run at the apply revision with the same results (§5-§8); the mutants are §9.

## 12. Known limits

- The Windows host cannot run `-race` (no cgo toolchain); the race evidence is the WSL run.
- The write-site scan gate covers the package's non-test source (D5, K1 parity); the 25 test-side
  fixture-state writes and the 3 same-named fields of other test types are enumerated, not gated.
  The gate's reach is syntactic (fix round, F1): field-selector assignment/inc-dec targets,
  whole-`runLifecycle` replacements, `&field` escapes, `runLifecycle:` construction keys (two
  allowlisted owners) and calls to function-typed fields; a write through a copied value
  (`v := *active` mutates the copy, not the run) and handle reads for caller-side invocation
  (legal by design, D4) stay outside it.
- Test-side rows in §8 were produced by a parser-only tool; it proves field→value equality per
  rewritten literal and byte-equality outside the rewritten spans, not type correctness (the
  compiler plus the full suite cover that).
- `phase()` has no production consumer: it is the module's state-detection query, read only by
  `run_lifecycle_test.go` (simplify S18 — recorded as test-only; the design-clause-3 wording
  "diagnostics and tests" is superseded). Its precedence treats `!slotHeld` as `retired`, so a
  future diagnostic must not read a slotless live entry as retired: the witness constructor
  (`linux_owner_witness_linux.go:2845`) registers `claimed: true` without a slot.
- This slice moves no effect across an unlock (O7 reversed); the deferred behaviour slice for the
  post-unlock moves remains out of scope, as does the K1 `daemon.mu` region inventory's
  registry-coupled work.
