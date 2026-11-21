package app

import (
	"context"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

// runLifecycle owns the lifecycle state of a live runningRun and every write to
// it. runningRun embeds the value, so existing readers keep their field paths.
//
// Lock contract. Every transition below requires the caller to hold daemon.mu
// (and daemon.eventReceiptMu where the K1 inventory records it) exactly where
// the base code held it; no method here takes a lock, performs I/O, starts a
// goroutine, sends on a channel or invokes a captured cancel func. Captured
// handles are returned to the caller, which invokes them after unlocking
// because that is where the base code invoked them.
//
// Behaviour contract. Each transition performs exactly the field writes the
// base performed at the same position, in the same order, under the same
// guards; it adds no branch, no return, no error and no refusal the base did
// not have (arch-decision-2 V1). A state the base has no branch for is still
// written the same way, and the module only makes such a state *detectable*
// (cleanupBlocked(), phase()).
//
// The fields are laid out by the run-lifecycle groups the slice's inventory
// recorded: admission & attachment, native start publication, cancellation,
// lease loss, terminal reservation, native settlement ownership, usage
// accounting barrier, native close retry, process containment finalization and
// the slot.
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

// cleanupBlocked is the one derived predicate over the lifecycle state: a
// native settlement owner, an open close retry or an unfinished usage-retry
// barrier each keep release blocked. It replaces the former free function
// nativeCleanupBlocked and is the only source of the latched cleanupBlocked
// memo (runningRun.cleanupBlocked), which forced sites set directly.
//
// The memo stays declared on runningRun because this method carries the name
// the memo also needs; the two cannot share one type. The memo's writers are
// all transitions in this file (refreshCleanupBlocked, markCleanupBlocked).
func (state *runLifecycle) cleanupBlocked() bool {
	return state.nativeTerminalOwner || state.nativeCloseRetryRequired || state.nativeCloseRetrying ||
		state.nativeUsageRetryPending || state.nativeUsageRetryInFlight || state.nativeUsageRetryExhausted
}

// refreshCleanupBlocked latches the derived predicate into the memo.
func (run *runningRun) refreshCleanupBlocked() {
	run.cleanupBlocked = run.runLifecycle.cleanupBlocked()
}

// markCleanupBlocked forces the memo true where the base forced it (a process
// exit or a retained accounting barrier owes cleanup regardless of the
// predicate).
func (run *runningRun) markCleanupBlocked() {
	run.cleanupBlocked = true
}

// runPhase is the collapsed main lifecycle phase (K1 §6 P0-P13) used by
// diagnostics and tests. P4b (native cancel outcome recorded) folds into
// phaseCancelling and stays visible through the cancel-outcome fields.
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

// --- admission & attachment ---

// attachClaim records that the persisted claim was attached to the run.
func (run *runningRun) attachClaim() {
	run.claimed = true
}

// finishStarting leaves the starting window. It returns whether the run is
// already terminal; the caller checks the run is present first.
func (run *runningRun) finishStarting(cleanupReady bool) bool {
	run.starting = false
	if cleanupReady && run.process == nil && run.nativeSession == nil {
		run.refreshCleanupBlocked()
	}
	return run.terminal
}

// finishAttachedStart leaves the starting window after an execution attached.
// Cancellation accepted during native publication is replayed only after the
// start owner has settled the returned session, so the caller acts on
// terminal||cancelled.
func (run *runningRun) finishAttachedStart() bool {
	run.starting = false
	if run.process == nil && run.nativeSession == nil {
		run.refreshCleanupBlocked()
	}
	return run.terminal || run.cancelled
}

// --- native start publication ---

// beginNativeStartPublication closes the publication window from Adapter.Start
// through the returned StartTurn result. The caller has already checked the
// window, cancellation, lease, terminal and reservation guards.
func (run *runningRun) beginNativeStartPublication() {
	run.nativeStartInFlight = true
}

// publishNativeStartResult makes the adapter result visible without releasing
// start ownership.
func (run *runningRun) publishNativeStartResult(session harness.Session, sessionKey state.GoalSessionKey, admission protocol.Admission, prepared workspace.Prepared) {
	admissionCopy := admission
	run.nativeSession = session
	run.goalSession = &sessionKey
	run.goalAdmission = &admissionCopy
	run.prepared = prepared
	run.markCleanupBlocked()
}

// endNativeStartPublication releases start ownership.
func (run *runningRun) endNativeStartPublication() {
	run.nativeStartInFlight = false
}

// endNativeStartPublicationWithRenewal releases start ownership and invalidates
// the in-flight renewal request, returning its cancel func.
func (run *runningRun) endNativeStartPublicationWithRenewal() context.CancelFunc {
	run.endNativeStartPublication()
	return run.invalidateRenewal()
}

// transferNativeStartTerminal lets terminal or usage evidence durably observed
// during StartTurn retain settlement ownership after the lease watchdog fires.
func (run *runningRun) transferNativeStartTerminal(terminalObserved bool) {
	run.nativeStartInFlight = false
	if terminalObserved {
		run.stale = false
		run.nativeDeadline = time.Time{}
	}
}

// markNativeStartTerminalObserved records terminal evidence for the in-flight
// or published native start.
func (run *runningRun) markNativeStartTerminalObserved() {
	run.nativeStartTerminalObserved = true
}

// --- cancellation ---

// markCancelled cancels the run without a command reservation (daemon
// shutdown).
func (run *runningRun) markCancelled() {
	run.cancelled = true
}

// requestCancel reserves the run for a cancellation command.
func (run *runningRun) requestCancel(commandID string) {
	run.cancelled = true
	run.cancelCommandID = commandID
}

// setCancelCommandID remembers the cancel command that will settle the run
// (used when cancellation was already reserved by another command).
func (run *runningRun) setCancelCommandID(commandID string) {
	run.cancelCommandID = commandID
}

// clearCancel clears a cancellation reservation that lost its authority.
func (run *runningRun) clearCancel() {
	run.cancelled = false
	run.cancelCommandID = ""
}

// recordNativeCancelObserved records the native cancellation outcome against
// the exact cancel command.
func (run *runningRun) recordNativeCancelObserved(commandID string, terminalObserved, rejected bool) {
	run.nativeCancelCommandID = commandID
	run.nativeCancelTerminalObserved = terminalObserved
	run.nativeCancelRejected = rejected
}

// recordNativeCancelTerminateErr records the terminal diagnosis of a native
// cancellation.
func (run *runningRun) recordNativeCancelTerminateErr(err error) {
	run.nativeCancelTerminateErr = err
}

// --- lease loss ---

// markLeaseLostDuringStart records lease loss while the native start
// publication window is open and returns the start and renewal cancel funcs
// for the caller to invoke after unlocking.
func (run *runningRun) markLeaseLostDuringStart(reason string) (context.CancelFunc, context.CancelFunc) {
	if !run.nativeStartLeaseExpired {
		run.nativeStartLeaseExpired = true
		run.nativeStartLeaseReason = reason
	}
	run.stale = true
	run.leaseRenewalClosed = true
	startCancel := run.releaseStartCancel()
	renewCancel := run.invalidateRenewal()
	return startCancel, renewCancel
}

// markLeaseLostWithSettlement records lease loss for an attached run. A live
// native session keeps settlement ownership so the observed terminal is not
// replaced by the stale outcome; only that case returns a non-nil renewal
// cancel func and settlementOwned.
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

// beginRenewal starts one lease renewal request and returns its id.
func (run *runningRun) beginRenewal(cancel context.CancelFunc) uint64 {
	run.renewCancelID++
	run.renewCancel = cancel
	return run.renewCancelID
}

// clearRenewal drops the renewal handle when the request that owns it
// completed; the caller compares renewCancelID first.
func (run *runningRun) clearRenewal() {
	run.renewCancel = nil
}

// invalidateRenewal invalidates any in-flight renewal request and returns its
// cancel func for the caller to invoke after unlocking.
func (run *runningRun) invalidateRenewal() context.CancelFunc {
	renewCancel := run.renewCancel
	run.renewCancel = nil
	run.renewCancelID++
	return renewCancel
}

// releaseStartCancel returns the run's start cancel func and drops it.
func (run *runningRun) releaseStartCancel() context.CancelFunc {
	startCancel := run.cancel
	run.cancel = nil
	return startCancel
}

// blockRenewal keeps lease renewal disabled for the rest of the run; the native
// settlement owns the terminal outcome.
func (run *runningRun) blockRenewal() {
	run.nativeUsageRenewalBlocked = true
}

// closeRenewal closes the run's renewal eligibility permanently.
func (run *runningRun) closeRenewal() {
	run.leaseRenewalClosed = true
}

// --- terminal reservation ---

// beginTerminalReservation holds the transition-to-durable window.
func (run *runningRun) beginTerminalReservation() {
	run.terminalizing++
}

// reserveTerminal holds the transition-to-durable window and invalidates the
// in-flight renewal request.
func (run *runningRun) reserveTerminal() context.CancelFunc {
	run.beginTerminalReservation()
	return run.invalidateRenewal()
}

// releaseTerminalReservation drops one reservation.
func (run *runningRun) releaseTerminalReservation() {
	if run.terminalizing > 0 {
		run.terminalizing--
	}
}

// publishTerminal records the durable terminal outcome for the run.
func (run *runningRun) publishTerminal() {
	run.terminal = true
	run.leaseRenewalClosed = true
}

// reserveTerminalSlotRelease installs the timer cancel func whose expiry
// releases the run's slot.
func (run *runningRun) reserveTerminalSlotRelease(cancel context.CancelFunc) {
	run.terminalCancel = cancel
}

// releaseSlotOnce releases the run's slot exactly once and returns the pending
// terminal slot-release cancel func, if any.
func (run *runningRun) releaseSlotOnce() context.CancelFunc {
	run.slotHeld = false
	return run.releaseTerminalCancel()
}

// releaseTerminalCancel returns the pending terminal slot-release cancel func
// and drops it.
func (run *runningRun) releaseTerminalCancel() context.CancelFunc {
	terminalCancel := run.terminalCancel
	run.terminalCancel = nil
	return terminalCancel
}

// retireRun releases the lifecycle of a run that is being removed from the
// daemon registry and returns the renewal and terminal-slot cancel funcs for
// the caller to invoke after unlocking.
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

// --- native settlement ownership ---

// claimSettlementOwner takes ownership of the run's native terminal settlement.
// A retry without its originating terminal claim cannot publish or release
// anything, so a later legitimate owner supersedes that orphaned in-memory work
// while retaining the durable usage evidence.
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

// reserveSettlementOwner takes settlement ownership for a non-start owner and
// invalidates the in-flight renewal request.
func (run *runningRun) reserveSettlementOwner() (uint64, context.CancelFunc) {
	ownerClaim := run.claimSettlementOwner(false)
	return ownerClaim, run.invalidateRenewal()
}

// claimObservedSettlementOwner takes settlement ownership for a completed
// native turn, records the terminal observation and blocks renewal.
func (run *runningRun) claimObservedSettlementOwner() (uint64, context.CancelFunc) {
	ownerClaim := run.claimSettlementOwner(true)
	run.markNativeStartTerminalObserved()
	run.blockRenewal()
	return ownerClaim, run.invalidateRenewal()
}

// settleCancelledStartWithObservedTerminal takes settlement ownership for a
// cancelled native start whose terminal was observed, records the cancel
// outcome and invalidates the in-flight renewal request.
func (run *runningRun) settleCancelledStartWithObservedTerminal(commandID string) context.CancelFunc {
	run.claimSettlementOwner(true)
	run.endNativeStartPublication()
	run.recordNativeCancelObserved(commandID, true, true)
	run.blockRenewal()
	run.refreshCleanupBlocked()
	return run.invalidateRenewal()
}

// claimStartedSettlementOwner takes settlement ownership for a native start
// whose terminal was observed while publication was in flight.
func (run *runningRun) claimStartedSettlementOwner() uint64 {
	ownerClaim := run.claimSettlementOwner(true)
	run.blockRenewal()
	run.refreshCleanupBlocked()
	return ownerClaim
}

// releaseSettlementOwner releases settlement ownership idempotently because
// terminal delivery, stale recovery and run release can converge on the same
// native completion.
func (run *runningRun) releaseSettlementOwner() {
	run.nativeTerminalOwner = false
	run.nativeTerminalOwnerClaim = 0
	run.nativeStartTerminalOwner = false
	if run.terminalizing > 0 {
		run.terminalizing--
	}
	run.refreshCleanupBlocked()
}

// finalizeConclusiveSettlement finalizes the usage barrier and releases
// settlement ownership in one step.
func (run *runningRun) finalizeConclusiveSettlement() {
	run.nativeUsageFinalized = true
	run.nativeUsageRetryPending = false
	run.nativeUsageRetryInFlight = false
	run.nativeUsageRetryExhausted = false
	run.nativeUsageRetryOwnerClaim = 0
	run.releaseSettlementOwner()
}

// markUsageFinalized records that the run's usage publication is complete.
func (run *runningRun) markUsageFinalized() {
	run.nativeUsageFinalized = true
}

// --- usage accounting barrier ---

// scheduleUsageRetry arms the accounting retry barrier with its first attempt,
// capturing the completion for a later flush.
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

// startUsageRetryAttempt marks the accounting retry in flight, or exhausts the
// retry budget and reports that the caller must settle the work as exhausted.
func (run *runningRun) startUsageRetryAttempt(now time.Time) bool {
	if run.nativeUsageRetryAttempts >= nativeUsageRetryLimit || (!run.nativeUsageRetryDeadline.IsZero() && !now.Before(run.nativeUsageRetryDeadline)) {
		run.exhaustUsageRetry()
		return false
	}
	run.nativeUsageRetryInFlight = true
	return true
}

// recordUsageRetryFailure records one failed accounting attempt and returns the
// attempt number and whether the retry budget is now exhausted.
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

// finishUsageRetryInFlight leaves the in-flight accounting attempt: a finished
// settlement re-arms the retry, a completed publication drops the owner claim.
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

// clearUsageRetryBeforePublication drops the accounting barrier for a terminal
// publication; renewal stays disabled until releaseRun even if terminal
// transition persistence later needs another recovery pass.
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

// exhaustUsageRetry marks the accounting retry budget exhausted.
func (run *runningRun) exhaustUsageRetry() {
	run.nativeUsageRetryPending = false
	run.nativeUsageRetryInFlight = false
	run.nativeUsageRetryExhausted = true
	run.nativeUsageRenewalBlocked = true
}

// exhaustUsageRetryForCleanup exhausts the accounting retry and keeps the run
// blocked for stale cleanup.
func (run *runningRun) exhaustUsageRetryForCleanup() {
	run.exhaustUsageRetry()
	run.stale = true
	run.markCleanupBlocked()
}

// scheduleRecoveryUsageRetry re-arms the accounting barrier after a recovery
// persistence failure, with the recovery attempt numbering the base used.
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

// ownUsageRetry transfers the accounting retry to a new terminal owner.
func (run *runningRun) ownUsageRetry(ownerClaim uint64) {
	run.nativeUsageRetryOwnerClaim = ownerClaim
}

// rememberFinalUsage retains the exact completion a later accounting attempt
// must publish.
func (run *runningRun) rememberFinalUsage(usage protocol.Usage) {
	copy := cloneNativeUsage(usage)
	run.nativeFinalUsage = &copy
	run.nativeUsageRetryNeedsPrepare = false
}

// --- native close retry ---

// requireCloseRetry records that a failed native close left the durable session
// uncertain.
func (run *runningRun) requireCloseRetry() {
	run.nativeCloseRetryRequired = true
	run.refreshCleanupBlocked()
}

// beginCloseRetry marks the close retry in flight.
func (run *runningRun) beginCloseRetry() {
	run.nativeCloseRetrying = true
}

// endCloseRetry marks the close retry finished.
func (run *runningRun) endCloseRetry() {
	run.nativeCloseRetrying = false
}

// publishCloseRetrySuccess clears the retry barrier and the native session
// after a successful close.
func (run *runningRun) publishCloseRetrySuccess() {
	run.nativeSession = nil
	run.goalSession = nil
	run.goalAdmission = nil
	run.nativeDeadline = time.Time{}
	run.nativeCloseRetryRequired = false
	run.nativeCloseRetrying = false
	run.refreshCleanupBlocked()
}

// --- process containment finalization ---

// markProcessExited records the process exit and resets the finalization
// counters when a different process takes ownership; it reports whether the
// caller must drop the run's cleanup retry entry.
func (run *runningRun) markProcessExited(process Process) bool {
	run.markCleanupBlocked()
	run.processExited = true
	if run.processFinalizeOwner != process {
		run.processFinalizeOwner = process
		run.processFinalizeAttempts = 0
		run.processFinalizeExhausted = false
		return true
	}
	return false
}

// recordProcessStop records the stop witness for a stopped process; it reports
// whether the caller must drop the run's cleanup retry entry.
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

// takeFinalizeOwnership resets the finalization counters when a different
// process takes ownership.
func (run *runningRun) takeFinalizeOwnership(process Process) {
	if run.processFinalizeOwner != process {
		run.processFinalizeOwner = process
		run.processFinalizeAttempts = 0
		run.processFinalizeExhausted = false
	}
}

// markFinalizeExhausted records that the containment finalization retry budget
// is exhausted.
func (run *runningRun) markFinalizeExhausted() {
	run.processFinalizeExhausted = true
}

// recordFinalizeAttempt counts one containment finalization attempt.
func (run *runningRun) recordFinalizeAttempt() {
	run.processFinalizeAttempts++
}

// recordFinalizedStop records the proven stop witness of a finalized process
// and clears the finalization exhaustion for the next owner.
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
