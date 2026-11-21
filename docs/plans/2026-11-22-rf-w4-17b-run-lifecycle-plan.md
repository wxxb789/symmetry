# rf-w4-17b run-lifecycle module plan — `runLifecycle` owns the lifecycle of a live run

Contract: `goal-rf-w4-17b-run-lifecycle.md` (GDD, 2026-11-21), binding with its
`## Conductor decision` sections (arch-decision-2 and its verification amendments V1-V10) and the
program-wide rules. Base revision for every coordinate below: `f70553bf` (the dispatch HEAD; the
contract's K1 references resolve at `79548368`, and §"Measured basis" re-measures at the dispatch
HEAD instead). Shape reference: `docs/plans/2026-11-21-rf-w4-17a-command-queue-module-plan.md` +
`docs/evidence/rf-w4-17a-command-queue-module-2026-11-21.md` (same package, same wave, plan +
`docs/evidence/<slice>-<date>.md` record).

Preconditions re-verified at dispatch: W4-17a merged (`f70553bf`, #201), #185 merged (`4b034f50`),
arch-decision-2 recorded as bound (`e936065c`, #207). No open PR touches the owned files
(`gh pr list` at dispatch: #178, #179, #181, #189, #191, #195, #197-#206 — none edits
`daemon/internal/app/*` lifecycle code except #191 (workspace walkers, `workspace` package) and
#199/#203 (platform test helpers); re-verified before finishing).

## Measured basis (dispatch HEAD `f70553bf`)

Scanner: `<HOST-PATH>/scratch/w4-17b/scan/main.go` (`go/ast`, parses
`daemon/internal/app/*.go`, collects every selector assignment / `++` / `--` whose final selector
name is a field of `runningRun`). Raw events: `.../w4-17b/events-head.tsv`.

| measure | value | command |
|---|---:|---|
| `runningRun` fields declared | 71 | struct at `native_run.go:96-181` (mutex objects `inputMu`, `linuxCloneOperationMu`, `nativeCloseMu` included) |
| non-test field-write statements (all fields) | 271 | scan, `kind in {write,incdec}`, non-test files |
| of those, lifecycle-field writes (§"Field map" list of 45 names) | 213 | `lifecycle-writes-nontest.txt` |
| non-test write *clusters* (transition sites) | 49 functions | `funcs-writes.txt` |
| non-test lifecycle-field reads | 493 | scan, `kind=read`, non-test files |
| test files whose literals set lifecycle fields | 15 of 23 | scan, `kind=literal` |
| `runningRun{` literals in tests | 267 | `grep -rho 'runningRun{' *_test.go \| wc -l` |
| of those, literals setting ≥1 lifecycle field | 87 (193 lifecycle-field keys; 346 keys counted against the full 71-field struct) | scan, `kind=literal` |
| test-side direct writes to lifecycle fields (assignments inside test bodies) | 26 (3 of which are same-named fields of other test types) | scan, test files |
| false positives of the name-based scan (non-test) | 0 with the 45-name set | scan of the 45-name set over non-test files |
| false positives of the name-based scan (test) | 3: `control.cancel` (`app_test.go:71`), `client.terminal` (`app_test.go:8777`), `session.cancelled` (`native_settlement_final_regression_test.go:593`) | scan, test files |

Confirmed by the measurement: the contract's "267 hand-built `runningRun{` fixtures" — 87 of them
carry lifecycle keys and are the ones the new state shape forces to be rewritten (O6's proof
gate applies to those 87). The other 180 literals compile unchanged.

## Deliverable shape

1. **New `daemon/internal/app/run_lifecycle.go`** — the module in one file:
   - `type runLifecycle struct`: the 44 fields of the ten K1 §6 groups (list below).
   - `cleanupBlocked() bool` on `runLifecycle`: the one derived predicate (K1 §6's six-flag OR),
     replacing the free function `nativeCleanupBlocked` (`goal_admission.go:1290-1292`).
   - `phase() runPhase` on `*runningRun`: the collapsed P0-P13 derivation for diagnostics and
     tests (design clause 3), with a documented precedence table.
   - the transition methods (table below), all declared on `*runningRun`, all documented
     "caller holds `daemon.mu`" (and `daemon.eventReceiptMu` where the base site did).
2. **`daemon/internal/app/native_run.go`** — `runningRun` embeds `runLifecycle`; the 44 field
   declarations move out; the file's 127 write statements become transition calls; all reads stay
   byte-identical (promoted fields).
3. **`daemon/internal/app/app.go`**, **`cleanup.go`**, **`commands.go`**, **`events.go`**,
   **`lease.go`**, **`goal_admission.go`**, **`recovery_glue.go`**,
   **`linux_owner_witness_linux.go`** — same mechanical replacement of lifecycle writes with
   transition calls at their base positions.
4. **`daemon/internal/app/*_test.go`** (15 files) — the 87 fixture literals rewritten into nested
   `runLifecycle: runLifecycle{…}` literals; O6 proof per literal (field→value sets equal).
   Every assertion, every test name and every other byte stays unchanged.
5. **New `daemon/internal/app/run_lifecycle_test.go`** — the module's tests at its interface (no
   `&daemon{}`): legal transition sequences per group, states the base has no branch for (shown
   detectable through the query methods, never refused), the `cleanupBlocked()` truth table, the
   `phase()` table, and the committed write-site scan gate.
6. **This plan + `docs/evidence/rf-w4-17b-run-lifecycle-2026-11-22.md`** (field→group map,
   transition list with callers, per-function write-sequence table, lock-site table, control-flow
   audit, scan output, race output, red mutants, gate exit codes).
7. **`scripts/ci/omo-control-native-evidence.py`** — `SOURCE_PATHS` gains
   `daemon/internal/app/run_lifecycle.go` (arch-decision-2 R9), mapping table in the evidence
   record, gate `python3 scripts/ci/test-omo-control-native-evidence.py` green.
8. `docs/TODO.md` / `docs/archive/CLOSED-TODO.md`: no entry closes (`grep -n "W4-17\|runLifecycle\|run
   lifecycle" docs/TODO.md` → no match at base and at dispatch HEAD), so neither file changes.

## Field map (K1 §6 groups → module)

Module fields (44) — declared in `runLifecycle`, written only by this file's transitions:

| group | fields |
|---|---|
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

One more §6 field, `cleanupBlocked` (K1 lists it under both the usage barrier and the containment
group) stays declared on `runningRun` — decision D1 below — and is also written only by this file.

Fields that keep their home on `runningRun` (data/payload per design clause 1's escape hatch;
90 declarations unchanged): `inputMu`, `linuxCloneOperationMu`, `nativeCloseMu`, `process`,
`nativeSession`, `goalSession`, `goalAdmission`, `linuxClone`, `goalUsage`, `goalUsageAt`,
`goalCachedInputTokens`, `goalUsageHasCached`, `goalUsageObservation`, `nativeDeadline`, `prepared`,
`output`, `startFailure`, `nativeFinalResult`, `nativeFinalWaitErr`, `nativeFinalCloseErr`,
`nativeFinalAdmission`, `nativeFinalUsage`, `outputDropWarned`, `stopExecution`,
`localLeaseDeadlineAt`, `leaseSequence`. Where a transition must set one of them atomically with a
flag, the transition writes it (it is declared on `*runningRun`, so it may); the per-function table
in the evidence names every such write.

## Transitions (interface; the evidence record reproduces this table with callers)

Every method below performs exactly the base writes at the base position, in the base order; no
method adds a branch, an error or a return the base did not have (V1). Guards and daemon-level
lookups stay at their base positions in the caller except the small in-cluster state guards named
in the table — those move into the transition unchanged (same condition, same order; recorded as a
`preserved` row in the control-flow audit). `mu` in the caller column means the daemon method keeps
its own lock sites unchanged.

| # | transition (receiver `*runningRun` unless noted) | writes, in base order | base sites (HEAD) |
|---|---|---|---|
| 1 | `attachClaim()` | `claimed=true` | `app.go:1417` |
| 2 | `finishStarting(cleanupReady bool) bool` | `starting=false`; if `cleanupReady && process==nil && nativeSession==nil` → `cleanupBlocked=cleanupBlocked()`; return `terminal` | `app.go:2135-2138` |
| 3 | `finishAttachedStart() bool` | `starting=false`; if `process==nil && nativeSession==nil` → `cleanupBlocked=cleanupBlocked()`; return `terminal\|\|cancelled` | `app.go:2149-2152` |
| 4 | `beginNativeStartPublication()` | `nativeStartInFlight=true` | `native_run.go:423` |
| 5 | `publishNativeStartResult(session, sessionKey, admission, prepared)` | `nativeSession`, `goalSession`, `goalAdmission`, `prepared`, `cleanupBlocked=true` | `native_run.go:441-445` |
| 6 | `endNativeStartPublication()` | `nativeStartInFlight=false` | `native_run.go:452` |
| 7 | `endNativeStartPublicationWithRenewal() context.CancelFunc` | `nativeStartInFlight=false`; `renewCancel` capture, `renewCancel=nil`, `renewCancelID++` | `native_run.go:740-743` |
| 8 | `transferNativeStartTerminal(terminalObserved bool)` | `nativeStartInFlight=false`; if `terminalObserved` → `stale=false`, `nativeDeadline=zero` | `native_run.go:473-476` |
| 9 | `claimStartedSettlementOwner() uint64` | `claimSettlementOwner(true)`; `nativeUsageRenewalBlocked=true`; `cleanupBlocked=cleanupBlocked()` | `native_run.go:478-480` |
| 9b | `settleCancelledStartWithObservedTerminal(commandID string) context.CancelFunc` | `claimSettlementOwner(true)`; `nativeStartInFlight=false`; `nativeCancelCommandID/…Observed=true/…Rejected=true`; `nativeUsageRenewalBlocked=true`; `cleanupBlocked=cleanupBlocked()`; `renewCancel` capture, `renewCancel=nil`, `renewCancelID++` | `native_run.go:721-730` |
| 10 | `markNativeStartTerminalObserved()` | `nativeStartTerminalObserved=true` | `events.go:462`, `events.go:466`, `native_run.go:393` |
| 11 | `markCancelled()` | `cancelled=true` | `native_run.go:2381` |
| 12 | `requestCancel(commandID string)` | `cancelled=true`; `cancelCommandID=commandID` | `commands.go:84-86`, `:394-395`, `:414-415`, `:419-420` |
| 13 | `setCancelCommandID(commandID string)` | `cancelCommandID=commandID` | `commands.go:408` |
| 14 | `clearCancel()` | `cancelled=false`; `cancelCommandID=""` | `app.go:2060-2061` |
| 15 | `recordNativeCancelObserved(commandID string, terminalObserved, rejected bool)` | `nativeCancelCommandID=commandID`; `nativeCancelTerminalObserved=terminalObserved`; `nativeCancelRejected=rejected` | `commands.go:371-373`, `:558-560`, `goal_admission.go:1197-1199`, `native_run.go:723-725`, `:786-788` |
| 16 | `recordNativeCancelTerminateErr(err error)` | `nativeCancelTerminateErr=err` | `commands.go:562/564/566`, `native_run.go:789` |
| 17 | `markLeaseLostDuringStart(reason string) (startCancel, renewCancel context.CancelFunc)` | if `!nativeStartLeaseExpired` → `nativeStartLeaseExpired=true`, `nativeStartLeaseReason=reason`; `stale=true`; `leaseRenewalClosed=true`; `cancel` capture, `cancel=nil`; `renewCancel` capture, `renewCancel=nil`, `renewCancelID++` | `lease.go:514-524` |
| 18 | `markLeaseLostWithSettlement() (renewCancel context.CancelFunc, settlementOwned bool)` | `cancelled=true`; `stale=true`; if `nativeSession!=nil` → `claimSettlementOwner(true)`; `nativeUsageRenewalBlocked=true`; `leaseRenewalClosed=true`; `renewCancel` capture, `renewCancel=nil`, `renewCancelID++`; `settlementOwned=true` | `lease.go:544-553` |
| 19 | `beginRenewal(cancel context.CancelFunc) uint64` | `renewCancelID++`; `renewCancel=cancel`; return the new id | `lease.go:454-456` |
| 20 | `clearRenewal()` | `renewCancel=nil` | `lease.go:467` |
| 21 | `beginTerminalReservation()` | `terminalizing++` | `commands.go:444` |
| 22 | `reserveTerminal() context.CancelFunc` | `terminalizing++`; `renewCancel` capture, `renewCancel=nil`, `renewCancelID++` | `app.go:1839-1842` |
| 23 | `releaseTerminalReservation()` | if `terminalizing>0` → `terminalizing--` | `app.go:1855`, `:2020`, `native_run.go:506`, `:527` |
| 24 | `publishTerminal()` | `terminal=true`; `leaseRenewalClosed=true` | `app.go:1858-1859` |
| 25 | `reserveTerminalSlotRelease(cancel context.CancelFunc)` | `terminalCancel=cancel` | `app.go:1795` |
| 26 | `releaseSlotOnce() context.CancelFunc` | `slotHeld=false`; `terminalCancel` capture, `terminalCancel=nil` | `app.go:1771-1775` |
| 27 | `retireRun() (renewCancel, terminalCancel context.CancelFunc)` | `slotHeld=false`; `leaseRenewalClosed=true`; if `nativeTerminalOwner` → `nativeTerminalOwner=false`, `nativeTerminalOwnerClaim=0`, `nativeStartTerminalOwner=false`, if `terminalizing>0` → `terminalizing--`; `nativeUsageRetryOwnerClaim=0`; `renewCancel` capture, `renewCancel=nil`, `renewCancelID++`; `terminalCancel` capture, `terminalCancel=nil` | `app.go:1726-1742` |
| 28 | `claimSettlementOwner(startOwner bool) uint64` | if `!nativeTerminalOwner && (nativeUsageRetryPending\|\|InFlight\|\|Exhausted\|\|nativeUsageRetryOwnerClaim!=0)` → the four orphaned-retry resets; `nativeTerminalOwnerSequence++`; if it wrapped to 0 → `++` again; `nativeTerminalOwner=true`; `nativeTerminalOwnerClaim=sequence`; `nativeStartTerminalOwner=startOwner`; `terminalizing++`; `cleanupBlocked=cleanupBlocked()` | `native_run.go:334-355` (free function `claimNativeTerminalOwner`) |
| 29 | `releaseSettlementOwner()` | `nativeTerminalOwner=false`; `nativeTerminalOwnerClaim=0`; `nativeStartTerminalOwner=false`; if `terminalizing>0` → `terminalizing--`; `cleanupBlocked=cleanupBlocked()` | `native_run.go:502-508` |
| 30 | `finalizeConclusiveSettlement()` | `nativeUsageFinalized=true`; `nativeUsageRetryPending=false`; `nativeUsageRetryInFlight=false`; `nativeUsageRetryExhausted=false`; `nativeUsageRetryOwnerClaim=0`; then `releaseSettlementOwner()` | `native_run.go:518-529` |
| 31 | `markUsageFinalized()` | `nativeUsageFinalized=true` | `native_run.go:1602`, `:1643`, `:1675` |
| 32 | `reserveSettlementOwner() (claim uint64, renewCancel context.CancelFunc)` | `claimSettlementOwner(false)`; `renewCancel` capture, `renewCancel=nil`, `renewCancelID++` | `native_run.go:369-372` |
| 33 | `claimObservedSettlementOwner() (claim uint64, renewCancel context.CancelFunc)` | `claimSettlementOwner(true)`; `nativeStartTerminalObserved=true`; `nativeUsageRenewalBlocked=true`; `renewCancel` capture, `renewCancel=nil`, `renewCancelID++` | `native_run.go:392-397` and `events.go:465-470` (identical clusters, one method) |
| 35 | `scheduleUsageRetry(ownerClaim uint64, needsPrepare bool, now time.Time)` | `nativeUsageRetryAttempts=1`; `nativeUsageRetryAt=now+minimumInterval`; `nativeUsageRetryDeadline=now+nativeUsageRetryWindow`; `nativeUsageRetryPending=true`; `nativeUsageRetryInFlight=false`; `nativeUsageRetryOwnerClaim=ownerClaim`; `nativeUsageRetryNeedsPrepare=needsPrepare`; `nativeUsageRenewalBlocked=true`; `nativeUsageRetryExhausted=false`; `cleanupBlocked=true` | `native_run.go:1716-1725` |
| 36 | `startUsageRetryAttempt(now time.Time) bool` | if budget exhausted (`attempts>=nativeUsageRetryLimit \|\| deadline passed`) → `exhaustUsageRetry()`, return false; `nativeUsageRetryInFlight=true`, return true | `native_run.go:1779-1788` |
| 37 | `recordUsageRetryFailure(now time.Time) (attempt int, exhausted bool)` | `nativeUsageRetryInFlight=false`; `nativeUsageRetryAttempts++`; if budget exhausted → `nativeUsageRetryPending=false`, `nativeUsageRetryExhausted=true`, `nativeUsageRenewalBlocked=true`, return `(attempt,true)`; else `nativeUsageRetryPending=true`, `nativeUsageRetryAt=now+nativeUsageRetryDelay(attempts)`, return `(attempt,false)` | `native_run.go:1832-1843` |
| 38 | `finishUsageRetryInFlight(settlementDone bool)` | `nativeUsageRetryInFlight=false`; if `!settlementDone && !nativeUsageFinalized` → `nativeUsageRetryPending=true`, `nativeUsageRetryAt=zero` else `nativeUsageRetryOwnerClaim=0`; `cleanupBlocked=cleanupBlocked()` | `native_run.go:1489-1496` |
| 39 | `clearUsageRetryBeforePublication(usageRetryCompletion bool)` | `nativeUsageRetryPending=false`; if `!usageRetryCompletion` → `nativeUsageRetryInFlight=false`; `nativeUsageRenewalBlocked=true`; `nativeCancelTerminalObserved=false`; `nativeCancelRejected=false`; `cleanupBlocked=cleanupBlocked()` | `native_run.go:1547-1559` |
| 40 | `exhaustUsageRetryForCleanup()` | `nativeUsageRetryPending=false`; `nativeUsageRetryInFlight=false`; `nativeUsageRetryExhausted=true`; `nativeUsageRenewalBlocked=true`; `stale=true`; `cleanupBlocked=true` | `native_run.go:1887-1892` |
| 41 | `scheduleRecoveryUsageRetry(now time.Time)` | `nativeUsageRetryPending=true`; `nativeUsageRetryInFlight=false`; `nativeUsageRetryExhausted=true`; if `attempts<nativeUsageRetryLimit` → `attempts=nativeUsageRetryLimit`; attempt := `attempts-limit+1` (min 1); `nativeUsageRetryAttempts++`; `nativeUsageRetryAt=now+nativeUsageRetryDelay(attempt)`; `nativeUsageRetryNeedsPrepare=(nativeFinalUsage==nil)`; `nativeUsageRenewalBlocked=true`; `cleanupBlocked=true` | `recovery_glue.go:720-734` |
| 42 | `ownUsageRetry(ownerClaim uint64)` | `nativeUsageRetryOwnerClaim=ownerClaim` | `native_run.go:1876` |
| 43 | `rememberFinalUsage(usage protocol.Usage)` | `nativeFinalUsage=&copy`; `nativeUsageRetryNeedsPrepare=false` | `native_run.go:2039-2040` |
| 44 | `requireCloseRetry()` | `nativeCloseRetryRequired=true`; `cleanupBlocked=cleanupBlocked()` | `goal_admission.go:1280-1281` |
| 45 | `beginCloseRetry()` | `nativeCloseRetrying=true` | `goal_admission.go:1333` |
| 46 | `endCloseRetry()` | `nativeCloseRetrying=false` | `goal_admission.go:1348` |
| 47 | `publishCloseRetrySuccess()` | `nativeSession=nil`; `goalSession=nil`; `goalAdmission=nil`; `nativeDeadline=zero`; `nativeCloseRetryRequired=false`; `nativeCloseRetrying=false`; `cleanupBlocked=cleanupBlocked()` | `native_run.go:837-843` |
| 48 | `markProcessExited(process Process) bool` | `cleanupBlocked=true`; `processExited=true`; if `processFinalizeOwner != process` → `processFinalizeOwner=process`, `processFinalizeAttempts=0`, `processFinalizeExhausted=false`, return true; return false | `native_run.go:1297-1304` |
| 49 | `recordProcessStop(witness Process, pid int, identity string, stopped bool) bool` | if `stopped` → `processStopWitness=witness`, `processStopPID=pid`, `processStopIdentity=identity`, `processFinalizeAttempts=0`, `processFinalizeExhausted=false`, return true; else `processStopWitness=nil`, `processStopPID=0`, `processStopIdentity=""`, return false | `native_run.go:1305-1316` |
| 50 | `takeFinalizeOwnership(process Process)` | if `processFinalizeOwner != process` → `processFinalizeOwner=process`, `processFinalizeAttempts=0`, `processFinalizeExhausted=false` | `cleanup.go:439-443` |
| 51 | `markFinalizeExhausted()` | `processFinalizeExhausted=true` | `cleanup.go:449`, `:461`, `:469` |
| 52 | `recordFinalizeAttempt()` | `processFinalizeAttempts++` | `cleanup.go:453` |
| 53 | `recordFinalizedStop(process Process, pid int, identity string)` | `processStopWitness=process`; `processStopPID=pid`; `processStopIdentity=identity`; `cleanupBlocked=true`; `processFinalizeExhausted=false` | `cleanup.go:484-488` |
| 54 | `clearCleanupBlock()` | `cleanupBlocked=false` | `cleanup.go:495` |
| 55 | `refreshCleanupBlocked()` | `cleanupBlocked=cleanupBlocked()` | the 12 memo sites (inside transitions 2,3,9,28,29,30,33,34,38,39,44,47) |
| 56 | `phase() runPhase` | none (query) | — |
| 57 | `cleanupBlocked() bool` on `runLifecycle` | none (query) | replaces `nativeCleanupBlocked` (`goal_admission.go:1290-1292`) |

Internal helpers (called only from this file): `blockRenewal()`, `closeRenewal()`,
`invalidateRenewal()`, `exhaustUsageRetry()`, `markCleanupBlocked()` (the forced-`true` memo
writes), `refreshCleanupBlocked()` (the derived memo write), `releaseStartCancel()` and
`releaseTerminalCancel()`; they exist so the caller-facing methods above stay semantic and so the
composed write orders match the base exactly. The delivered module declares these plus the 57 rows
above: 64 methods in total (`markStale` was not needed — `markLeaseLostWithSettlement` absorbs the
`cancelled`+`stale` pair at its single base site).

### Per-function mapping (write order preserved)

| file:function | base writes (HEAD lines) → transitions, in base order |
|---|---|
| `app.go:startAssigned` | `:1417` → 1 |
| `app.go:releaseRun` | `:1726-1742` → 27 (the two post-unlock cancel invocations and the `<-daemon.slots` receive stay in the caller at their base positions) |
| `app.go:releaseSlotOnce` | `:1771-1775` → 26 |
| `app.go:scheduleTerminalSlotRelease` | `:1795` → 25 |
| `app.go:beginTerminal` | `:1839-1842` → 22; closure `:1855` → 23; `:1858-1859` → 24 |
| `app.go:releaseTerminalReservation` | `:2020` → 23 |
| `app.go:clearCancellationReservation` | `:2060-2061` → 14 |
| `app.go:finishStarting` | `:2135-2138` → 2 |
| `app.go:finishAttachedStart` | `:2149-2152` → 3 |
| `cleanup.go:finalizeExitedProcess` | `:439-443` → 50; `:449` → 51; `:453` → 52; `:461`, `:469` → 51 (guarded by caller); `:484-488` → 53 |
| `cleanup.go:clearCleanupBlockedAfterStop` | `:495` → 54 (caller guard unchanged) |
| `commands.go:dispatchQueuedCommand` | `:84-86` → 12 |
| `commands.go:handleCommand` | `:371-373` → 15; `:394-395`, `:414-415`, `:419-420` → 12; `:408` → 13; `:441-442` → 32-part (`renewCancel`/`renewCancelID`) — the `claimNativeTerminalOwner` at `:439` → 28; `:444` → 21; `:558-560` → 15; `:562/564/566` → 16 |
| `events.go:queueEvent` | `:462`, `:466` → 10; `:465-470` → 34 |
| `goal_admission.go:startGoalAdmission` | `:1197-1199` → 15 |
| `goal_admission.go:requireNativeSessionCloseRetry` | `:1280-1281` → 44 |
| `goal_admission.go:retryBlockedNativeSessionCloses` | `:1333` → 45; `:1348` → 46 |
| `lease.go:renewLeaseWithTimingAndStart` | `:454-456` → 19; `:467` → 20 (caller guard unchanged) |
| `lease.go:terminateForLease` | `:514-524` → 17; `:544-553` → 18 (the `process` read and the `if active.nativeSession != nil` guard are subsumed by 18, which returns `settlementOwned`) |
| `linux_owner_witness_linux.go` constructor (`:2845`) | `claimed: true` moves into the nested `runLifecycle` literal |
| `native_run.go:claimNativeTerminalOwner` | `:339-352` → 28 (free function deleted; the 8 call sites become methods per rows below) |
| `native_run.go:reserveNativeTerminalClaim` | `:369-372` → 32 |
| `native_run.go:reserveNativeObservedTerminalClaim` | `:392-397` → 33 |
| `native_run.go:beginNativeStart` | `:423` → 4 |
| `native_run.go:publishNativeStartResult` | `:441-445` → 5 |
| `native_run.go:finishNativeStartPublication` | `:452` → 6 |
| `native_run.go:transferNativeStartTerminal` | `:473-476` → 8; `:478-480` → 9 |
| `native_run.go:releaseNativeTerminalOwner` | `:502-508` → 29 |
| `native_run.go:finalizeConclusiveNativeTerminalOwner` | `:518-529` → 30 |
| `native_run.go:settleCancelledNativeStart` | `:722-730` → 6,15,9 (order: 6, then 15, then 9's remaining writes — see evidence table for the exact split); `:740-743` → 7; `:786-789` → 15,16 |
| `native_run.go:publishNativeCloseRetrySuccess` | `:837-843` → 47 |
| `native_run.go:recordGenericProcessExit` | `:1297-1304` → 48 (the two `delete(daemon.cleanupRetry, key)` calls stay in the caller inside the same regions); `:1305-1316` → 49 |
| `native_run.go:completeNativeRunAfterUsageOwned` | `:1489-1496` → 38; `:1547-1559` → 39; `:1602`, `:1643`, `:1675` → 31 |
| `native_run.go:deferNativeUsageRetry` | `:1716-1725` → 35 (the five `nativeFinal*` payload writes at `:1709-1715` stay in the caller at their base positions, before the transition) |
| `native_run.go:flushPendingNativeUsage` | `:1779-1788` → 36 |
| `native_run.go:noteNativeUsageRetryFailure` | `:1832-1843` → 37 |
| `native_run.go:exhaustNativeUsageRetry` | `:1876` → 42 |
| `native_run.go:exhaustNativeUsageRetryOwned` | `:1887-1892` → 40 |
| `native_run.go:rememberNativeFinalUsage` | `:2039-2040` → 43 |
| `native_run.go:stopAll` | `:2381` → 11 |
| `recovery_glue.go:retryNativeUsageRecoveryPersistence` | `:720-734` → 41 |

Every other statement, expression, comment, branch and return in these functions keeps its base
position; the evidence record's per-function table re-derives the field-write sequence from both
revisions mechanically and shows 0 mismatches.

## Contract-preserving decisions (recorded, not guessed)

**D1 — the cleanup memo keeps its name and stays on `runningRun`.** `cleanupBlocked` is both a
stored bool (§6 lists it under the usage barrier and the containment group) and the name design
clause 3 gives the derived predicate: `cleanupBlocked()`. A field and a method cannot share a name
on one type, and moving the field into the embedded `runLifecycle` while declaring the predicate on
`*runningRun` would shadow the promoted field for every plain `active.cleanupBlocked` read. Options
measured: (a) move the field into `runLifecycle` and rename it (`cleanupOwed`) — 86 test-file lines
at base in 17 files change text (42 fixture-literal keys and 44 assertion/read lines), which
clause 3 reads as "an existing assertion changed"; (b) keep the field name and place the predicate
on `*runLifecycle`,
reached as `active.runLifecycle.cleanupBlocked()` — 0 assertion lines change. **Recommendation
adopted: (b).** Every write to the memo is still in `run_lifecycle.go` (the scan gate's field set
includes it), the module owns the predicate, and the field→group map in the evidence names this
placement with its reason.

**D2 — transitions are methods on `*runningRun` in `run_lifecycle.go`.** Design clause 1 asks the
`runLifecycle` value to own the lifecycle fields; the transitions that must set a payload field
atomically with a flag (design clause 1's escape hatch: `publishNativeStartResult`,
`publishCloseRetrySuccess`, `transferNativeStartTerminal`, `scheduleUsageRetry`'s caller,
`rememberFinalUsage`) can only do so from a receiver that sees those fields. Declaring every
transition on `*runningRun` keeps one consistent receiver, keeps the file as the single write
owner (the gate is file-based: "no code outside run_lifecycle.go writes a lifecycle field"), and
keeps `runLifecycle` a pure value with the fields, the predicate and the phase query.

**D3 — guards stay at the base position; only in-cluster state guards move into a transition**
(rows 2, 3, 8, 17, 18, 27, 28, 36, 37, 48, 49, 50 in the table). Design clause 2's
per-method precondition checks and the `lifecycleViolation` hook are superseded by V1 (no
production refusal) and by the contract's "no new production branch": the guards the base checks
are already checked by the callers, in the base order, in the same locked region. The control-flow
audit's `preserved` rows cover each moved guard by condition, effect and position.

**D4 — no effect moves across an unlock (O7 reversed).** `active.cancel()`, `renewCancel()`,
`terminalCancel()`, `startCancel()`, `<-daemon.slots`, `active.inputMu` acquire/release and every
other invocation keeps its base position; transitions only *capture* handles under the lock and
return them, exactly as the base functions did, so no new goroutine, channel or timer appears and
no cancellation is delivered earlier or later than at base.

**D5 — the write-site scan gate covers the package's non-test source.** K1's scanner excluded
`*_test.go`; the contract's design clause 4 says "reuse the K1 scanner idea", and 26 of the 29
test-side lifecycle writes are fixture state construction (e.g.
`fixture.daemon.running[key].stale = true`), which no transition expresses without inventing
test-only setters. Options: (a) non-test scope (K1 parity) + the 29 test-side writes enumerated in
the evidence; (b) all-files scope with 3 pinned false-positive exclusions, which needs 23 test
rewrites into setter calls the module should not own. **Recommendation adopted: (a)**, with the
enumerated test-side list as the visibility complement, so nothing is hidden. The gate's field set
is the 45 names of §"Field map" (the memo included), and the test asserts the field set itself, so
a field leaving the module turns the gate red.

**D6 — the fixture rewrite is the nested-literal form** `runLifecycle: runLifecycle{…}` (D1 keeps
`cleanupBlocked` outside it). 87 of the 267 fixtures change; the O6 proof compares each rewritten
literal's field→value multiset against the base revision, and the untouched 180 are counted, not
edited.

**D7 — phase collapse.** `phase()` derives the furthest phase the flags prove, first match wins, in
this documented order: `retired` (`!slotHeld`, K1 P13), `terminal` (`terminal`, P7), `settlement`
(`nativeTerminalOwner || nativeUsageFinalized`, P8/P10), `usageRetry` (P9 flags), `closeRetry`
(P11 flags), `containment` (`processExited || processFinalizeOwner != nil || cleanupBlocked`, P12),
`reserving` (`terminalizing > 0`, P6), `leaseLost` (`stale`, P5), `cancelling` (`cancelled`, P4),
`starting` (`nativeStartInFlight`, P3), `attached` (`process != nil || nativeSession != nil`, P2),
`claimed` (`claimed`, P1), `new` (default, P0). P4b (native cancel outcome recorded) folds into
`cancelling` and is visible through the query fields; the collapse is documented in the module and
pinned by one table-driven test including an illegal-state row per query method.

**D8 — size rule (arch-decision-2: "if the diff exceeds 1,500 lines, split into W4-17b-1
(authority/cancel) and W4-17b-2 (post-exit barriers)").** Options: (a) deliver the split — b-1
would still need module writes to barrier fields, because `claimSettlementOwner` (authority)
resets four usage-barrier flags, `settleCancelledNativeStart`/`transferNativeStartTerminal` set
`nativeUsageRenewalBlocked`, `releaseRun` resets `nativeUsageRetryOwnerClaim`, and
`cleanupBlocked()`'s six-flag predicate spans G6/G7/G8, so b-1's gate could only claim "0 writes
outside" for a field subset while its module already wrote the subset b-2 owns; and clause 1's
`cleanupBlocked()` replacement plus clause 3's per-function write-order control bind all 49
functions; (b) deliver the whole slice and report the measured diff. **Recommendation adopted:
(b)**: measured on the delivered tree, `git diff --numstat` over the tracked files gives
`+374 / -581 = 955` changed lines (production edits ≈ 465, the mechanical test-fixture rewrite
≈ 490) and the new files add `run_lifecycle.go` 739 + `run_lifecycle_test.go` 838 = 1,577, so the
code diff is ≈ 2,532 lines and the two mandated records add ≈ 763 (this plan 363 + the evidence
record). Under option (a) b-1 would carry the module file (~540 of its 739 lines), its tests
(~500 of 838), **all 87 fixture literals** (the fields are mixed across groups, so the rewrite is
not divisible) and its own write/lock/flow tables — measured against the same files that is
>1,800 lines, so the split does not produce a ≤1,500-line increment either; the plan and the
evidence record follow the program rule C2 (LOC is report-only). If the conductor prefers (a),
b-1 is exactly the G1-G6/G10 rows plus the G7-G9 field declarations staying on `runningRun`, and
b-2 is the declaration move plus rows 35-43, 48-54.

## Preserved semantics (byte-identical surfaces)

1. **Locks.** No new mutex; `daemon.mu` is taken only by the daemon wrappers at their base sites in
   the base order; `eventReceiptMu`, `commandReceiptMu` and `inputMu` keep their base acquisition
   points and order. Transitions take no lock (asserted by the scan test: `run_lifecycle.go`
   contains no `Lock(`).
2. **Goroutines, channels, timers.** None added, none moved: the transitions perform no invocation
   and start nothing; every captured handle is invoked by the caller at its base position (D4).
3. **Write order.** For every one of the 49 functions the sequence of fields written is identical
   to base on every path (evidence table re-derived from both revisions).
4. **Text.** No error string, log key, state name or test name changes; the only new strings are
   the phase names of the diagnostics query, which no production path logs.
5. **Behaviour on unmodelled states.** V1: no transition refuses a state; a state the base has no
   branch for (e.g. `claimSettlementOwner` with an orphaned usage-retry owner) performs exactly the
   base writes. The tests show such states are *detectable* through `cleanupBlocked()`/`phase()`;
   they do not make production refuse them.

## Evidence plan (all foreground; heavy suites serialized)

1. `gofmt -l ./internal/app/` (empty); `go vet ./...`; `GOOS=linux go build ./...`;
   `GOOS=windows go build ./...` (exit 0 each).
2. Windows `go test -count=1 -race ./internal/app/...` — this host has `gcc` absent for the
   daemon module? recorded at W4-17a: `-race` requires cgo and failed on the Windows side, so the
   attempt and its error are recorded and the run repeats without `-race`; the race oracle is WSL.
3. WSL `wsl -d Ubuntu-24.04 -u accept` → `go test -count=1 -race ./internal/app/... ./internal/execution/...`
   (contract gate). If the pre-existing `TestClaimAdmissionInputFailsClosedBeforeWorkspaceOrProcessStart`
   hang reappears (recorded at W4-17a as host-pre-existing), the run is repeated with that test
   skipped and the W4-17b touched-path subset is run with `-race`, all recorded; a full green run is
   never claimed without its output.
4. Scan gate: the committed test in `run_lifecycle_test.go` plus the standalone scratch run over the
   45-name set = 0 writes outside `run_lifecycle.go`.
5. Per-function write-sequence table: mechanical extraction (scratch tool, base `f70553bf` vs the
   working tree) for all 49 functions; 0 mismatches.
6. Lock-site table before/after for every touched function: every `daemon.mu` / `eventReceiptMu` /
   `commandReceiptMu` / `inputMu` acquisition, in order; identical.
7. Control-flow audit (lesson 32): every early return, fall-through, error short-circuit and default
   clause of the 49 functions and of `nativeCleanupBlocked`'s replacement, mapped base → HEAD with
   a `preserved`/`moved-into-transition` mark.
8. Fixture proof (O6): per rewritten literal, base field→value multiset equals HEAD's.
9. Red mutants: one temporary edit per new test, failing output recorded, then reverted.
10. `no file > 1500 L`: line counts of every changed file, base vs HEAD; `native_run.go` must not
    grow (it shrinks: 44 declarations leave it).
11. R9: `SOURCE_PATHS` + `python3 scripts/ci/test-omo-control-native-evidence.py` green; moved
    declarations mapped to their listed paths.
12. Host-path and DLP scans over the diff (`us[e]rs/`, backslash-u), `git diff --check`.
13. Diff size measurement (insertions/deletions, code vs docs) for D8's record.

## Out of scope (with reasons)

- Moving any effect from under `daemon.mu` to after unlock (O7 reversed; deferred behaviour slice).
- `daemon` registry maps (`running`, `slots`, `cleanupRetry`), the command queue and its receipts
  (W4-17a), journal callbacks (W4-12), the admission transaction (W4-01), `state.go`'s store: the
  contract names them out of scope; transitions call nothing.
- Payload fields (`process`, sessions, admission, `prepared`, `output`, `nativeFinal*`,
  `goalUsage*`, `nativeDeadline`, `localLeaseDeadlineAt`, `leaseSequence`, `stopExecution`,
  `startFailure`, the three mutex objects) keep their declarations; only their atomic writes move.
- Test-side fixture state construction via direct field writes (D5).
- `docs/design/harness.md:288`'s `startGoalAdmission` citation: the slice edits statements below
  line 263, never the function's position (O23 does not trigger; verified by line comparison).
