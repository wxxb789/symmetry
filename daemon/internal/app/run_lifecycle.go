package app

import (
	"context"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

// runLifecycle owns the lifecycle state of a live runningRun and every
// assignment to it after construction. Transitions need daemon.mu at their base
// site and only capture cancel handles, which the caller invokes after unlocking.
type runLifecycle struct {
	// admission & attachment
	starting bool
	claimed  bool
	// native start publication: beginNativeStart owns Adapter.Start through the
	// returned StartTurn result.
	nativeStartInFlight         bool
	nativeStartTerminalObserved bool
	// cancellation
	cancelled                    bool
	cancelCommandID              string
	nativeCancelCommandID        string
	nativeCancelTerminalObserved bool
	nativeCancelRejected         bool
	nativeCancelTerminateErr     error
	cancel                       context.CancelFunc
	// lease loss
	stale                   bool
	nativeStartLeaseExpired bool
	nativeStartLeaseReason  string
	leaseRenewalClosed      bool
	renewCancel             context.CancelFunc
	renewCancelID           uint64
	// terminal reservation
	terminalizing  int
	terminal       bool
	terminalCancel context.CancelFunc
	// native settlement ownership
	nativeTerminalOwner         bool
	nativeTerminalOwnerClaim    uint64
	nativeTerminalOwnerSequence uint64
	nativeStartTerminalOwner    bool
	// usage accounting barrier: native completion is retained locally until
	// accounting is durably queued.
	nativeUsageRetryPending      bool
	nativeUsageRetryInFlight     bool
	nativeUsageRetryExhausted    bool
	nativeUsageRetryNeedsPrepare bool
	nativeUsageRetryOwnerClaim   uint64
	nativeUsageRetryAt           time.Time
	nativeUsageRetryDeadline     time.Time
	nativeUsageRetryAttempts     int
	nativeUsageRenewalBlocked    bool
	nativeUsageFinalized         bool
	// native close retry
	nativeCloseRetryRequired bool
	nativeCloseRetrying      bool
	// process containment finalization
	processExited            bool
	processFinalizeOwner     Process
	processFinalizeAttempts  int
	processFinalizeExhausted bool
	processStopWitness       Process
	processStopPID           int
	processStopIdentity      string
	// slot
	slotHeld bool
}

// cleanupBlocked is the one derived predicate over the lifecycle state; the
// memo stays on runningRun only because a field cannot share this method's name.
func (state *runLifecycle) cleanupBlocked() bool {
	return state.nativeTerminalOwner || state.nativeCloseRetryRequired || state.nativeCloseRetrying ||
		state.nativeUsageRetryPending || state.nativeUsageRetryInFlight || state.nativeUsageRetryExhausted
}

func (run *runningRun) refreshCleanupBlocked() {
	run.cleanupBlocked = run.runLifecycle.cleanupBlocked()
}

// markCleanupBlocked forces the memo true where the base forced it.
func (run *runningRun) markCleanupBlocked() {
	run.cleanupBlocked = true
}

// runPhase is the collapsed main lifecycle phase (K1 §6 P0-P13), queried by this
// module's tests. P4b folds into phaseCancelling and stays visible in its fields.
type runPhase string

const (
	phaseNew         runPhase = "new"         // P0
	phaseClaimed     runPhase = "claimed"     // P1
	phaseAttached    runPhase = "attached"    // P2
	phaseStarting    runPhase = "starting"    // P3
	phaseCancelling  runPhase = "cancelling"  // P4/P4b
	phaseLeaseLost   runPhase = "lease-lost"  // P5
	phaseReserving   runPhase = "reserving"   // P6
	phaseContainment runPhase = "containment" // P12
	phaseCloseRetry  runPhase = "close-retry" // P11
	phaseUsageRetry  runPhase = "usage-retry" // P9
	phaseSettlement  runPhase = "settlement"  // P8/P10
	phaseTerminal    runPhase = "terminal"    // P7
	phaseRetired     runPhase = "retired"     // P13
)

// phase derives the furthest phase the state proves. The order below is the
// documented collapse: the first matching row wins, so a terminal outcome
// dominates an open retry, and a released slot dominates everything.
func (run *runningRun) phase() runPhase {
	switch {
	case !run.slotHeld:
		return phaseRetired
	case run.terminal:
		return phaseTerminal
	case run.nativeTerminalOwner, run.nativeUsageFinalized:
		return phaseSettlement
	case run.nativeUsageRetryPending, run.nativeUsageRetryInFlight, run.nativeUsageRetryExhausted:
		return phaseUsageRetry
	case run.nativeCloseRetryRequired, run.nativeCloseRetrying:
		return phaseCloseRetry
	case run.processExited, run.processFinalizeOwner != nil, run.cleanupBlocked:
		return phaseContainment
	case run.terminalizing > 0:
		return phaseReserving
	case run.stale:
		return phaseLeaseLost
	case run.cancelled:
		return phaseCancelling
	case run.nativeStartInFlight:
		return phaseStarting
	case run.process != nil, run.nativeSession != nil:
		return phaseAttached
	case run.claimed:
		return phaseClaimed
	}
	return phaseNew
}

func (run *runningRun) attachClaim() {
	run.claimed = true
}

func (run *runningRun) finishStarting(cleanupReady bool) bool {
	run.starting = false
	if cleanupReady && run.process == nil && run.nativeSession == nil {
		run.refreshCleanupBlocked()
	}
	return run.terminal
}

// finishAttachedStart leaves the starting window: cancellation replays only
// after the start owner settles, so the caller acts on terminal||cancelled.
func (run *runningRun) finishAttachedStart() bool {
	run.finishStarting(true)
	return run.terminal || run.cancelled
}

// beginNativeStartPublication closes the publication window from Adapter.Start
// through the returned StartTurn result. The caller has already checked the
// window, cancellation, lease, terminal and reservation guards.
func (run *runningRun) beginNativeStartPublication() {
	run.nativeStartInFlight = true
}

func (run *runningRun) publishNativeStartResult(session harness.Session, sessionKey state.GoalSessionKey, admission protocol.Admission, prepared workspace.Prepared) {
	admissionCopy := admission
	run.nativeSession = session
	run.goalSession = &sessionKey
	run.goalAdmission = &admissionCopy
	run.prepared = prepared
	run.markCleanupBlocked()
}

func (run *runningRun) endNativeStartPublication() {
	run.nativeStartInFlight = false
}

func (run *runningRun) endNativeStartPublicationWithRenewal() context.CancelFunc {
	run.endNativeStartPublication()
	return run.invalidateRenewal()
}

func (run *runningRun) transferNativeStartTerminal(terminalObserved bool) {
	run.nativeStartInFlight = false
	if terminalObserved {
		run.stale = false
		run.nativeDeadline = time.Time{}
	}
}

func (run *runningRun) markNativeStartTerminalObserved() {
	run.nativeStartTerminalObserved = true
}

func (run *runningRun) markCancelled() {
	run.cancelled = true
}

func (run *runningRun) requestCancel(commandID string) {
	run.cancelled = true
	run.cancelCommandID = commandID
}

func (run *runningRun) setCancelCommandID(commandID string) {
	run.cancelCommandID = commandID
}

func (run *runningRun) clearCancel() {
	run.cancelled = false
	run.cancelCommandID = ""
}

func (run *runningRun) recordNativeCancelObserved(commandID string, terminalObserved, rejected bool) {
	run.nativeCancelCommandID = commandID
	run.nativeCancelTerminalObserved = terminalObserved
	run.nativeCancelRejected = rejected
}

func (run *runningRun) recordNativeCancelTerminateErr(err error) {
	run.nativeCancelTerminateErr = err
}

func (run *runningRun) markLeaseLostDuringStart(reason string) (context.CancelFunc, context.CancelFunc) {
	if !run.nativeStartLeaseExpired {
		run.nativeStartLeaseExpired = true
		run.nativeStartLeaseReason = reason
	}
	run.stale = true
	run.leaseRenewalClosed = true
	startCancel := run.cancel
	run.cancel = nil
	renewCancel := run.invalidateRenewal()
	return startCancel, renewCancel
}

// markLeaseLostWithSettlement records lease loss for an attached run; a live native
// session keeps settlement ownership so the stale outcome cannot replace it.
func (run *runningRun) markLeaseLostWithSettlement() (context.CancelFunc, bool) {
	run.cancelled = true
	run.stale = true
	if run.nativeSession == nil {
		return nil, false
	}
	run.claimSettlementOwner(true)
	run.blockRenewal()
	run.closeRenewal()
	return run.invalidateRenewal(), true
}

func (run *runningRun) beginRenewal(cancel context.CancelFunc) uint64 {
	run.renewCancelID++
	run.renewCancel = cancel
	return run.renewCancelID
}

// clearRenewal drops the renewal handle; the caller compares renewCancelID first.
func (run *runningRun) clearRenewal() {
	run.renewCancel = nil
}

func (run *runningRun) invalidateRenewal() context.CancelFunc {
	renewCancel := run.renewCancel
	run.renewCancel = nil
	run.renewCancelID++
	return renewCancel
}

func (run *runningRun) blockRenewal() {
	run.nativeUsageRenewalBlocked = true
}

func (run *runningRun) closeRenewal() {
	run.leaseRenewalClosed = true
}

func (run *runningRun) beginTerminalReservation() {
	run.terminalizing++
}

func (run *runningRun) reserveTerminal() context.CancelFunc {
	run.beginTerminalReservation()
	return run.invalidateRenewal()
}

func (run *runningRun) releaseTerminalReservation() {
	if run.terminalizing > 0 {
		run.terminalizing--
	}
}

func (run *runningRun) publishTerminal() {
	run.terminal = true
	run.leaseRenewalClosed = true
}

func (run *runningRun) reserveTerminalSlotRelease(cancel context.CancelFunc) {
	run.terminalCancel = cancel
}

// releaseSlotOnce releases the slot once; the caller still performs the
// <-daemon.slots receive.
func (run *runningRun) releaseSlotOnce() context.CancelFunc {
	run.slotHeld = false
	return run.releaseTerminalCancel()
}

func (run *runningRun) releaseTerminalCancel() context.CancelFunc {
	terminalCancel := run.terminalCancel
	run.terminalCancel = nil
	return terminalCancel
}

func (run *runningRun) retireRun() (context.CancelFunc, context.CancelFunc) {
	run.slotHeld = false
	run.closeRenewal()
	if run.nativeTerminalOwner {
		run.nativeTerminalOwner = false
		run.nativeTerminalOwnerClaim = 0
		run.nativeStartTerminalOwner = false
		if run.terminalizing > 0 {
			run.terminalizing--
		}
	}
	run.nativeUsageRetryOwnerClaim = 0
	return run.invalidateRenewal(), run.releaseTerminalCancel()
}

// claimSettlementOwner takes settlement ownership; a later legitimate owner
// supersedes an orphaned retry while retaining its durable usage evidence.
func (run *runningRun) claimSettlementOwner(startOwner bool) uint64 {
	if !run.nativeTerminalOwner && (run.nativeUsageRetryPending || run.nativeUsageRetryInFlight || run.nativeUsageRetryExhausted || run.nativeUsageRetryOwnerClaim != 0) {
		run.nativeUsageRetryPending = false
		run.nativeUsageRetryInFlight = false
		run.nativeUsageRetryExhausted = false
		run.nativeUsageRetryOwnerClaim = 0
	}
	run.nativeTerminalOwnerSequence++
	if run.nativeTerminalOwnerSequence == 0 {
		run.nativeTerminalOwnerSequence++
	}
	run.nativeTerminalOwner = true
	run.nativeTerminalOwnerClaim = run.nativeTerminalOwnerSequence
	run.nativeStartTerminalOwner = startOwner
	run.terminalizing++
	run.refreshCleanupBlocked()
	return run.nativeTerminalOwnerClaim
}

func (run *runningRun) reserveSettlementOwner() (uint64, context.CancelFunc) {
	ownerClaim := run.claimSettlementOwner(false)
	return ownerClaim, run.invalidateRenewal()
}

func (run *runningRun) claimObservedSettlementOwner() (uint64, context.CancelFunc) {
	ownerClaim := run.claimSettlementOwner(true)
	run.markNativeStartTerminalObserved()
	run.blockRenewal()
	return ownerClaim, run.invalidateRenewal()
}

func (run *runningRun) settleCancelledStartWithObservedTerminal(commandID string) context.CancelFunc {
	run.claimSettlementOwner(true)
	run.endNativeStartPublication()
	run.recordNativeCancelObserved(commandID, true, true)
	run.blockRenewal()
	run.refreshCleanupBlocked()
	return run.invalidateRenewal()
}

func (run *runningRun) claimStartedSettlementOwner() uint64 {
	ownerClaim := run.claimSettlementOwner(true)
	run.blockRenewal()
	run.refreshCleanupBlocked()
	return ownerClaim
}

func (run *runningRun) releaseSettlementOwner() {
	run.nativeTerminalOwner = false
	run.nativeTerminalOwnerClaim = 0
	run.nativeStartTerminalOwner = false
	if run.terminalizing > 0 {
		run.terminalizing--
	}
	run.refreshCleanupBlocked()
}

func (run *runningRun) finalizeConclusiveSettlement() {
	run.nativeUsageFinalized = true
	run.nativeUsageRetryPending = false
	run.nativeUsageRetryInFlight = false
	run.nativeUsageRetryExhausted = false
	run.nativeUsageRetryOwnerClaim = 0
	run.releaseSettlementOwner()
}

func (run *runningRun) markUsageFinalized() {
	run.nativeUsageFinalized = true
}

func (run *runningRun) scheduleUsageRetry(ownerClaim uint64, needsPrepare bool, now time.Time) {
	run.nativeUsageRetryAttempts = 1
	run.nativeUsageRetryAt = now.Add(minimumInterval)
	run.nativeUsageRetryDeadline = now.Add(nativeUsageRetryWindow)
	run.nativeUsageRetryPending = true
	run.nativeUsageRetryInFlight = false
	run.nativeUsageRetryOwnerClaim = ownerClaim
	run.nativeUsageRetryNeedsPrepare = needsPrepare
	run.nativeUsageRenewalBlocked = true
	run.nativeUsageRetryExhausted = false
	run.markCleanupBlocked()
}

func (run *runningRun) startUsageRetryAttempt(now time.Time) bool {
	if run.nativeUsageRetryAttempts >= nativeUsageRetryLimit || (!run.nativeUsageRetryDeadline.IsZero() && !now.Before(run.nativeUsageRetryDeadline)) {
		run.exhaustUsageRetry()
		return false
	}
	run.nativeUsageRetryInFlight = true
	return true
}

func (run *runningRun) recordUsageRetryFailure(now time.Time) (int, bool) {
	run.nativeUsageRetryInFlight = false
	run.nativeUsageRetryAttempts++
	attempt := run.nativeUsageRetryAttempts
	if run.nativeUsageRetryAttempts >= nativeUsageRetryLimit || (!run.nativeUsageRetryDeadline.IsZero() && !now.Before(run.nativeUsageRetryDeadline)) {
		run.nativeUsageRetryPending = false
		run.nativeUsageRetryExhausted = true
		run.nativeUsageRenewalBlocked = true
		return attempt, true
	}
	run.nativeUsageRetryPending = true
	run.nativeUsageRetryAt = now.Add(nativeUsageRetryDelay(run.nativeUsageRetryAttempts))
	return attempt, false
}

func (run *runningRun) finishUsageRetryInFlight(settlementDone bool) {
	run.nativeUsageRetryInFlight = false
	if !settlementDone && !run.nativeUsageFinalized {
		run.nativeUsageRetryPending = true
		run.nativeUsageRetryAt = time.Time{}
	} else {
		run.nativeUsageRetryOwnerClaim = 0
	}
	run.refreshCleanupBlocked()
}

// clearUsageRetryBeforePublication drops the barrier; renewal stays closed until releaseRun.
func (run *runningRun) clearUsageRetryBeforePublication(usageRetryCompletion bool) {
	run.nativeUsageRetryPending = false
	if !usageRetryCompletion {
		run.nativeUsageRetryInFlight = false
	}
	run.nativeUsageRenewalBlocked = true
	run.nativeCancelTerminalObserved = false
	run.nativeCancelRejected = false
	run.refreshCleanupBlocked()
}

func (run *runningRun) exhaustUsageRetry() {
	run.nativeUsageRetryPending = false
	run.nativeUsageRetryInFlight = false
	run.nativeUsageRetryExhausted = true
	run.nativeUsageRenewalBlocked = true
}

func (run *runningRun) exhaustUsageRetryForCleanup() {
	run.exhaustUsageRetry()
	run.stale = true
	run.markCleanupBlocked()
}

// scheduleRecoveryUsageRetry re-arms the barrier with the base's recovery attempt numbering.
func (run *runningRun) scheduleRecoveryUsageRetry(now time.Time) {
	run.nativeUsageRetryPending = true
	run.nativeUsageRetryInFlight = false
	run.nativeUsageRetryExhausted = true
	if run.nativeUsageRetryAttempts < nativeUsageRetryLimit {
		run.nativeUsageRetryAttempts = nativeUsageRetryLimit
	}
	recoveryAttempt := run.nativeUsageRetryAttempts - nativeUsageRetryLimit + 1
	if recoveryAttempt < 1 {
		recoveryAttempt = 1
	}
	run.nativeUsageRetryAttempts++
	run.nativeUsageRetryAt = now.Add(nativeUsageRetryDelay(recoveryAttempt))
	run.nativeUsageRetryNeedsPrepare = run.nativeFinalUsage == nil
	run.nativeUsageRenewalBlocked = true
	run.markCleanupBlocked()
}

func (run *runningRun) ownUsageRetry(ownerClaim uint64) {
	run.nativeUsageRetryOwnerClaim = ownerClaim
}

func (run *runningRun) rememberFinalUsage(usage protocol.Usage) {
	copy := cloneNativeUsage(usage)
	run.nativeFinalUsage = &copy
	run.nativeUsageRetryNeedsPrepare = false
}

func (run *runningRun) requireCloseRetry() {
	run.nativeCloseRetryRequired = true
	run.refreshCleanupBlocked()
}

func (run *runningRun) beginCloseRetry() {
	run.nativeCloseRetrying = true
}

func (run *runningRun) endCloseRetry() {
	run.nativeCloseRetrying = false
}

func (run *runningRun) publishCloseRetrySuccess() {
	run.nativeSession = nil
	run.goalSession = nil
	run.goalAdmission = nil
	run.nativeDeadline = time.Time{}
	run.nativeCloseRetryRequired = false
	run.nativeCloseRetrying = false
	run.refreshCleanupBlocked()
}

// markProcessExited reports whether the caller must drop the cleanup retry entry.
func (run *runningRun) markProcessExited(process Process) bool {
	run.markCleanupBlocked()
	run.processExited = true
	if run.processFinalizeOwner == process {
		return false
	}
	run.takeFinalizeOwnership(process)
	return true
}

// recordProcessStop reports whether the caller must drop the cleanup retry entry.
func (run *runningRun) recordProcessStop(witness Process, pid int, identity string, stopped bool) bool {
	if stopped {
		run.processStopWitness = witness
		run.processStopPID = pid
		run.processStopIdentity = identity
		run.processFinalizeAttempts = 0
		run.processFinalizeExhausted = false
		return true
	}
	run.processStopWitness = nil
	run.processStopPID = 0
	run.processStopIdentity = ""
	return false
}

func (run *runningRun) takeFinalizeOwnership(process Process) {
	if run.processFinalizeOwner != process {
		run.processFinalizeOwner = process
		run.processFinalizeAttempts = 0
		run.processFinalizeExhausted = false
	}
}

func (run *runningRun) markFinalizeExhausted() {
	run.processFinalizeExhausted = true
}

func (run *runningRun) recordFinalizeAttempt() {
	run.processFinalizeAttempts++
}

func (run *runningRun) recordFinalizedStop(process Process, pid int, identity string) {
	run.processStopWitness = process
	run.processStopPID = pid
	run.processStopIdentity = identity
	run.markCleanupBlocked()
	run.processFinalizeExhausted = false
}

// clearCleanupBlock clears the cleanup memo once the stop witness proves the
// run's process is contained; the caller checks the witness identity.
func (run *runningRun) clearCleanupBlock() {
	run.cleanupBlocked = false
}
