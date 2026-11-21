package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

type commandCompletion struct {
	run  state.RunKey
	done <-chan struct{}
}

func (daemon *daemon) enqueueCommand(command protocol.Command) <-chan struct{} {
	daemon.mu.Lock()
	done, wake := daemon.enqueueCommandLocked(command)
	daemon.mu.Unlock()
	if wake != nil {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	return done
}

// enqueueCommandLocked must be called with daemon.mu held.
func (daemon *daemon) enqueueCommandLocked(command protocol.Command) (<-chan struct{}, chan<- struct{}) {
	if command.CommandID == "" || daemon.commandWake == nil || daemon.backgroundStop {
		return nil, nil
	}
	queued, created := daemon.commands.enqueue(command)
	if !created {
		return queued.done, nil
	}
	return queued.done, daemon.commandWake
}

func (daemon *daemon) runCommandExecutor(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		queued, ok := daemon.nextQueuedCommand()
		if ok {
			daemon.dispatchQueuedCommand(ctx, queued)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-daemon.commandSignal():
		}
	}
}

func (daemon *daemon) dispatchQueuedCommand(ctx context.Context, queued *queuedCommand) {
	key := state.RunKey{RunID: queued.command.RunID, Generation: queued.command.Generation}
	daemon.mu.Lock()
	if !daemon.commands.current(queued) {
		daemon.mu.Unlock()
		return
	}
	if daemon.backgroundStop || ctx.Err() != nil {
		daemon.mu.Unlock()
		daemon.finishQueuedCommand(queued, false)
		return
	}
	if queued.command.Kind == "cancel" {
		if active := daemon.running[key]; active != nil {
			if active.terminal || active.terminalizing > 0 {
				// A terminal owner has reserved this run. Do not publish a local
				// cancellation flag that could make the terminal owner fail its
				// own durable write; the command remains replayable until the
				// authoritative terminal is visible.
			} else {
				active.markCancelled()
				if !active.claimed {
					active.setCancelCommandID(queued.command.CommandID)
					daemon.mu.Unlock()
					return
				}
			}
		}
		daemon.commandWG.Add(1)
		daemon.mu.Unlock()
		go func() {
			defer daemon.commandWG.Done()
			daemon.executeQueuedCommand(ctx, queued)
		}()
		return
	}
	if !daemon.commands.lane(key, queued) {
		daemon.mu.Unlock()
		return
	}
	daemon.commandWG.Add(1)
	daemon.mu.Unlock()
	go daemon.runCommandLane(ctx, key)
}

func (daemon *daemon) runCommandLane(ctx context.Context, key state.RunKey) {
	defer daemon.commandWG.Done()
	for {
		queued, ok := daemon.nextLaneCommand(key)
		if !ok {
			return
		}
		daemon.executeQueuedCommand(ctx, queued)
	}
}

func (daemon *daemon) nextLaneCommand(key state.RunKey) (*queuedCommand, bool) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	return daemon.commands.nextInLane(key)
}

func (daemon *daemon) executeQueuedCommand(ctx context.Context, queued *queuedCommand) {
	daemon.mu.Lock()
	current := daemon.commands.current(queued)
	daemon.mu.Unlock()
	if !current {
		return
	}
	if ctx.Err() != nil {
		daemon.finishQueuedCommand(queued, false)
		return
	}
	receipt := daemon.handleCommand(ctx, queued.command)
	daemon.finishQueuedCommand(queued, receipt)
}

func (daemon *daemon) nextQueuedCommand() (*queuedCommand, bool) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	return daemon.commands.next()
}

func (daemon *daemon) finishQueuedCommand(queued *queuedCommand, receipt bool) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	daemon.commands.finish(queued.key, queued, receipt)
}

func (daemon *daemon) finishDeferredCommand(key commandKey, receipt bool) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	// A nil entry resolves whatever key currently maps to: the deferred path
	// completes the entry the command key is registered under, if any.
	daemon.commands.finish(key, nil, receipt)
}

func (daemon *daemon) commandSignal() <-chan struct{} {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	return daemon.commandWake
}

func (daemon *daemon) handleSnapshot(ctx context.Context, snapshot protocol.RuntimeSnapshot) {
	completions, ok := daemon.enqueueSnapshotCommandsForRequest(0, snapshot.Commands)
	if !ok || !waitCommandCompletions(ctx, completions) {
		return
	}
	for _, assignment := range snapshot.Assignments {
		if ctx.Err() != nil {
			return
		}
		daemon.startAssignment(ctx, assignment)
	}
}

func (daemon *daemon) scheduleSnapshot(snapshot protocol.RuntimeSnapshot) {
	daemon.scheduleSnapshotForRequest(0, snapshot)
}

func (daemon *daemon) scheduleSnapshotForRequest(requestID uint64, snapshot protocol.RuntimeSnapshot) {
	completions, ok := daemon.enqueueSnapshotCommandsForRequest(requestID, snapshot.Commands)
	if !ok {
		return
	}
	delayed := make(map[state.RunKey][]commandCompletion)
	for _, completion := range completions {
		delayed[completion.run] = append(delayed[completion.run], completion)
	}
	daemon.mu.Lock()
	ctx := daemon.background
	if ctx == nil || daemon.backgroundStop {
		daemon.mu.Unlock()
		return
	}
	delayedCount := 0
	for _, assignment := range snapshot.Assignments {
		if len(delayed[state.RunKey{RunID: assignment.RunID, Generation: assignment.Generation}]) > 0 {
			delayedCount++
		}
	}
	daemon.snapshotWG.Add(delayedCount)
	daemon.mu.Unlock()
	for _, assignment := range snapshot.Assignments {
		assignment := assignment
		relevant := delayed[state.RunKey{RunID: assignment.RunID, Generation: assignment.Generation}]
		if len(relevant) == 0 {
			if ctx.Err() == nil {
				daemon.startAssignment(ctx, assignment)
			}
			continue
		}
		go func() {
			defer daemon.snapshotWG.Done()
			if !waitCommandCompletions(ctx, relevant) || ctx.Err() != nil {
				return
			}
			daemon.startAssignment(ctx, assignment)
		}()
	}
}

func (daemon *daemon) enqueueSnapshotCommands(commands []protocol.Command) ([]commandCompletion, bool) {
	return daemon.enqueueSnapshotCommandsForRequest(0, commands)
}

func (daemon *daemon) enqueueSnapshotCommandsForRequest(requestID uint64, commands []protocol.Command) ([]commandCompletion, bool) {
	daemon.commandReceiptMu.Lock()
	defer daemon.commandReceiptMu.Unlock()
	completions := make([]commandCompletion, 0, len(commands))
	var wake chan<- struct{}
	daemon.mu.Lock()
	for _, command := range commands {
		key := commandKey{run: state.RunKey{RunID: command.RunID, Generation: command.Generation}, id: command.CommandID}
		if requestID != 0 {
			if daemon.commands.receipts.suppressed(requestID, key) {
				continue
			}
		}
		completion, commandWake := daemon.enqueueCommandLocked(command)
		if completion == nil {
			if requestID != 0 {
				daemon.commands.receipts.finish(requestID)
			}
			daemon.mu.Unlock()
			return nil, false
		}
		if commandWake != nil {
			wake = commandWake
		}
		completions = append(completions, commandCompletion{
			run:  state.RunKey{RunID: command.RunID, Generation: command.Generation},
			done: completion,
		})
	}
	if requestID != 0 {
		daemon.commands.receipts.finish(requestID)
	}
	daemon.mu.Unlock()
	if wake != nil {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	return completions, true
}

func (daemon *daemon) beginCommandRequest() uint64 {
	daemon.commandReceiptMu.Lock()
	defer daemon.commandReceiptMu.Unlock()
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	return daemon.commands.receipts.begin()
}

func (daemon *daemon) finishCommandRequest(requestID uint64) {
	if requestID == 0 {
		return
	}
	daemon.commandReceiptMu.Lock()
	defer daemon.commandReceiptMu.Unlock()
	daemon.mu.Lock()
	daemon.commands.receipts.finish(requestID)
	daemon.mu.Unlock()
}

func waitCommandCompletions(ctx context.Context, completions []commandCompletion) bool {
	for _, completion := range completions {
		select {
		case <-ctx.Done():
			return false
		case <-completion.done:
			if ctx.Err() != nil {
				return false
			}
		}
	}
	return true
}

func (daemon *daemon) acknowledgeExpiredNativeStartCancellationCommand(ctx context.Context, key state.RunKey, commandID string) error {
	if commandID == "" {
		return nil
	}
	if !daemon.queueCommandAcknowledgementWithContext(context.WithoutCancel(ctx), key, commandID, "failed") {
		return errors.New("queue cancellation acknowledgement after native lease expiry")
	}
	daemon.finishDeferredCommand(commandKey{run: key, id: commandID}, true)
	return nil
}

func (daemon *daemon) handleCommand(ctx context.Context, command protocol.Command) bool {
	key := state.RunKey{RunID: command.RunID, Generation: command.Generation}
	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return false
	}
	for _, acknowledgement := range journal.PendingCommandAcknowledgements {
		if acknowledgement.CommandID == command.CommandID {
			return true
		}
	}
	for _, intent := range journal.ControlCommandIntents {
		if intent.CommandID == command.CommandID {
			return true
		}
	}
	if command.Kind == "cancel" {
		// A terminal transition is authoritative as soon as it is durable, even
		// before Control has returned a verdict. Re-read immediately before
		// touching the live process so a late cancellation cannot replace it.
		journal, err = daemon.store.LoadJournal(key)
		if err != nil {
			return false
		}
		if durableTerminalPresent(journal) {
			if state.CommandAcknowledgementRetired(journal) {
				return false
			}
			daemon.mu.Lock()
			active := daemon.running[key]
			terminalObserved := active != nil && active.nativeCancelCommandID == command.CommandID && active.nativeCancelTerminalObserved
			explicitlyRejected := terminalObserved && active.nativeCancelRejected
			daemon.mu.Unlock()
			if terminalObserved && !explicitlyRejected {
				return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "failed")
			}
			return daemon.replayOrRejectCancellationAcknowledgement(ctx, key, command.CommandID, journal)
		}
		daemon.mu.Lock()
		active := daemon.running[key]
		if active == nil {
			daemon.mu.Unlock()
			if journal.LocalState == "terminal_pending" {
				return daemon.queueCancellationReceipt(ctx, key, command.CommandID)
			}
			if journal.LocalState == "stale" {
				// Lease loss released the in-memory owner before this cancel
				// arrived. Recovery stops or proves the recorded process before
				// it publishes the cancellation receipt.
				return daemon.cancelRecoveredJournal(ctx, journal, command.CommandID)
			}
			return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "rejected")
		}
		if active.terminal || active.terminalizing > 0 {
			if active.nativeTerminalOwner && active.nativeStartTerminalObserved && !active.stale {
				active.recordNativeCancelObserved(command.CommandID, true, true)
			}
			daemon.mu.Unlock()
			daemon.clearCancellationReservation(key, command.CommandID)
			latest, loadErr := daemon.store.LoadJournal(key)
			if loadErr == nil && durableTerminalPresent(latest) {
				return daemon.rejectCancellationIfTerminalDurable(ctx, key, command.CommandID, latest)
			}
			// A terminal owner has a local reservation but has not published its
			// transition yet. Do not mark this run cancelled or touch native
			// state; Control will replay the command after the owner commits.
			return false
		}
		if active.nativeStartInFlight {
			// Adapter.Start/Open/StartTurn still owns publication. Record the
			// request and cancel its context; the starter settles the returned
			// session before any command may touch it.
			if active.nativeStartLeaseExpired {
				daemon.mu.Unlock()
				return false
			}
			active.requestCancel(command.CommandID)
			cancel := active.cancel
			daemon.mu.Unlock()
			if cancel != nil {
				cancel()
			}
			return false
		}
		if active.stale {
			// Lease loss already stopped this owner. Remember the cancel so the
			// exact process-exit witness can settle it; never acknowledge before
			// that physical stop is proven.
			if active.cancelCommandID == "" {
				active.setCancelCommandID(command.CommandID)
			}
			daemon.mu.Unlock()
			return daemon.settleStaleCancellation(ctx, key)
		}
		if !active.claimed {
			active.requestCancel(command.CommandID)
			daemon.mu.Unlock()
			return false
		}
		active.requestCancel(command.CommandID)
		process := active.process
		nativeSession := active.nativeSession
		nativePrepared := active.prepared
		var nativeAdmission *protocol.Admission
		if active.goalAdmission != nil {
			admissionCopy := *active.goalAdmission
			nativeAdmission = &admissionCopy
		}
		cancel := active.cancel
		terminalReserved := process == nil && nativeSession == nil && cancel != nil
		nativeTerminalReserved := nativeSession != nil
		nativeTerminalOwnerClaim := uint64(0)
		var renewCancel context.CancelFunc
		if nativeTerminalReserved {
			nativeTerminalOwnerClaim = active.claimSettlementOwner(false)
			if active.goalAdmission != nil {
				admissionCopy := *active.goalAdmission
				active.nativeFinalAdmission = &admissionCopy
			}
			renewCancel = active.invalidateRenewal()
		} else if terminalReserved {
			active.beginTerminalReservation()
		}
		daemon.mu.Unlock()
		if renewCancel != nil {
			renewCancel()
		}
		if terminalReserved {
			defer daemon.releaseTerminalReservation(key)
		}
		latest, allowed := daemon.reloadCancellationBeforeSideEffect(key, terminalReserved || nativeTerminalReserved)
		if allowed && latest.Fence() != journal.Fence() {
			allowed = false
		}
		if !allowed {
			daemon.clearCancellationReservation(key, command.CommandID)
			if nativeTerminalReserved {
				daemon.releaseNativeTerminalOwner(key, active, nativeTerminalOwnerClaim)
			}
			if durableTerminalPresent(latest) {
				return daemon.rejectCancellationIfTerminalDurable(ctx, key, command.CommandID, latest)
			}
			// The terminal owner is still responsible for completing its durable
			// write. No cancellation acknowledgement is safe yet.
			return false
		}
		daemon.rememberWorkspaceRetention(key)
		defer daemon.persistWorkspaceRetention(key)
		var terminateErr error
		var finalResult harness.TaskResult
		finalResultKnown := false
		terminalObserved := false
		cancelExplicitlyRejected := false
		var controlFailure error
		var nativeWaitTurnErr error
		var finalWaitErr error
		var nativeCloseErr error
		var terminalValidationErr error
		if nativeSession != nil {
			interruptContext, stopInterrupt := context.WithTimeout(ctx, 2*time.Second)
			receipt, controlErr := nativeSession.Control(interruptContext, harness.ControlRequest{CommandID: command.CommandID, Kind: harness.ControlCancel})
			stopInterrupt()
			controlFailure = controlErr
			if controlFailure == nil && receipt.Outcome != harness.ControlApplied {
				controlFailure = fmt.Errorf("native cancel was %s: %s", receipt.Outcome, receipt.Message)
			}
			cancelExplicitlyRejected = controlErr == nil && receipt.Outcome == harness.ControlRejected
			// A control acknowledgement only accepts the native request. The
			// staged turn barrier is the first close permission; it never returns
			// an early TaskResult and it remains bounded when acknowledgement is
			// lost.
			waitContext, stopWait := context.WithTimeout(ctx, 2*time.Second)
			var waitErr error
			if staged, ok := nativeSession.(harness.StagedSession); ok {
				waitErr = staged.WaitTurn(waitContext)
				nativeWaitTurnErr = waitErr
				// Only an explicit native rejection may be reported as rejected.
				// A lost/failed control response with an observed terminal is failed,
				// while the observed native result still owns the Run terminal.
				terminalObserved = controlFailure != nil && nativeTurnTerminalObserved(waitErr)
			} else {
				finalResult, waitErr = nativeSession.Wait(waitContext)
				finalResultKnown = waitErr == nil
			}
			stopWait()
			if waitErr != nil && !errors.Is(waitErr, context.DeadlineExceeded) && !errors.Is(waitErr, context.Canceled) {
				terminateErr = waitErr
			}
			closeErr := daemon.closeNativeGoalSession(key, active, nativeSession)
			nativeCloseErr = closeErr
			if controlFailure != nil {
				if closeErr != nil {
					terminateErr = errors.Join(terminateErr, controlFailure, closeErr)
				} else {
					// Preserve the existing cancellation contract: a bounded process
					// close is sufficient even when the native interrupt receipt was
					// unavailable.
					terminateErr = errors.Join(terminateErr)
				}
			} else {
				terminateErr = errors.Join(terminateErr, closeErr)
			}
			if _, ok := nativeSession.(harness.StagedSession); ok {
				finalContext, cancel := context.WithTimeout(daemon.failedStartFinalizationContext(), controlRequestLimit)
				result, waitErr := nativeSession.Wait(finalContext)
				cancel()
				finalWaitErr = waitErr
				if waitErr != nil {
					terminateErr = errors.Join(terminateErr, fmt.Errorf("wait native final result: %w", waitErr))
				} else {
					finalResult = result
					finalResultKnown = true
				}
			}
			if finalResultKnown && controlFailure != nil {
				terminalObserved = true
			}
			if terminalObserved && finalResultKnown {
				validationContext, stopValidation := context.WithTimeout(daemon.rootContext(ctx), controlRequestLimit)
				terminalValidationErr = daemon.verifyNativeTaskResultSubject(validationContext, key, nativePrepared, nativeAdmission, &finalResult)
				stopValidation()
			}
		} else if process != nil {
			terminateErr = process.Terminate(ctx, 2*time.Second)
		} else if cancel != nil {
			cancel()
		}
		if nativeSession != nil {
			terminalWaitErr := errors.Join(finalWaitErr, terminalValidationErr)
			waitTurnObservedFailure := nativeWaitTurnErr != nil && nativeTurnTerminalObserved(nativeWaitTurnErr)
			if terminalObserved && (waitTurnObservedFailure || !finalResultKnown) {
				terminalWaitErr = errors.Join(nativeWaitTurnErr, finalWaitErr, terminalValidationErr)
			}
			daemon.mu.Lock()
			if daemon.running[key] == active {
				rejected := terminalObserved && cancelExplicitlyRejected && !waitTurnObservedFailure && (nativeWaitTurnErr == nil || finalResultKnown)
				active.recordNativeCancelObserved(command.CommandID, terminalObserved, rejected)
				if rejected {
					active.recordNativeCancelTerminateErr(nil)
				} else if terminalObserved {
					active.recordNativeCancelTerminateErr(controlFailure)
				} else {
					active.recordNativeCancelTerminateErr(terminateErr)
				}
			}
			daemon.mu.Unlock()
			var usageResult *harness.TaskResult
			if finalResultKnown {
				usageResult = &finalResult
			}
			usage, shouldQueue, usageErr := daemon.prepareNativeGoalUsage(key, usageResult)
			var usageCopy *protocol.Usage
			if usageErr == nil && shouldQueue {
				daemon.rememberNativeFinalUsage(key, active, usage)
				usageCopy = &usage
			}
			settlementWaitErr := terminateErr
			settlementCloseErr := error(nil)
			if terminalObserved {
				settlementWaitErr = terminalWaitErr
				settlementCloseErr = nativeCloseErr
			} else if finalResultKnown && nativeProcessOutputFailure(finalResult.Process) != nil {
				settlementWaitErr = finalWaitErr
				settlementCloseErr = nativeCloseErr
			}
			if usageErr == nil {
				usageErr = daemon.queueNativeGoalUsageSettlement(key, active, nativeTerminalOwnerClaim, finalResult, settlementWaitErr, settlementCloseErr, usageCopy)
			}
			if terminalObserved {
				if usageErr != nil {
					daemon.deferNativeUsageRetry(key, active, nativeTerminalOwnerClaim, finalResult, terminalWaitErr, nativeCloseErr, usageCopy, !shouldQueue, usageErr)
					if daemon.log != nil {
						daemon.log.Warn("queue_native_goal_usage_before_terminal_cancel_failed", "run_id", key.RunID, "generation", key.Generation, "error", usageErr)
					}
					return false
				}
				daemon.completeNativeRunAfterUsageOwned(ctx, key, active, nativeTerminalOwnerClaim, finalResult, terminalWaitErr, nativeCloseErr, false)
				latest, loadErr := daemon.store.LoadJournal(key)
				return loadErr == nil && hasPendingCommandAcknowledgement(latest, command.CommandID)
			}
			if usageErr != nil {
				// Do not acknowledge cancellation or allow terminal cleanup to
				// proceed while a terminal native outcome has no durable accounting.
				daemon.deferNativeUsageRetry(key, active, nativeTerminalOwnerClaim, finalResult, terminateErr, nil, usageCopy, !shouldQueue, usageErr)
				if daemon.log != nil {
					daemon.log.Warn("queue_native_goal_usage_before_cancel_failed", "run_id", key.RunID, "generation", key.Generation, "error", usageErr)
				}
				return false
			}
			if finalResultKnown && nativeProcessOutputFailure(finalResult.Process) != nil {
				daemon.completeNativeRunAfterUsageOwned(ctx, key, active, nativeTerminalOwnerClaim, finalResult, finalWaitErr, nativeCloseErr, false)
				latest, loadErr := daemon.store.LoadJournal(key)
				return loadErr == nil && hasPendingCommandAcknowledgement(latest, command.CommandID)
			}
		}
		if terminateErr != nil {
			payload := map[string]string{
				"stage":   "cancel_native",
				"reason":  string(canonicalTaskResultReason(terminateErr, nil, protocol.TaskResultReasonUnknownOutcome)),
				"summary": "daemon failed during cancel_native",
				"error":   terminateErr.Error(),
			}
			acknowledged := daemon.queueTerminalTransitionAndAcknowledgementWithContext(ctx, key, "failed", payload, command.CommandID, "failed") == nil
			if acknowledged && nativeTerminalReserved {
				daemon.releaseNativeTerminalOwner(key, active, nativeTerminalOwnerClaim)
			}
			return acknowledged
		}
		active.inputMu.Lock()
		acknowledged := daemon.queueCancellationReceipt(ctx, key, command.CommandID)
		active.inputMu.Unlock()
		if acknowledged && nativeTerminalReserved {
			daemon.releaseNativeTerminalOwner(key, active, nativeTerminalOwnerClaim)
		}
		return acknowledged
	}

	active, nativeSessionActive := daemon.commandRunSnapshot(key)
	if active == nil {
		return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "rejected")
	}
	if nativeSessionActive {
		// This vertical slice only has a verified cancellation delivery path.
		// Guidance, pause/resume, approval, and a second native turn remain
		// visible as unsupported rather than falling back to legacy stdin.
		return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "rejected")
	}
	outcome := "rejected"
	switch command.Kind {
	case "guidance", "pause", "resume":
		return daemon.handleSupervisoryCommand(ctx, key, journal, active, command)
	case "provide_input":
		var object map[string]json.RawMessage
		if err := json.Unmarshal(command.Payload, &object); err != nil || object == nil {
			break
		}
		payloadDigest, err := canonicalInputDigest(command.Payload)
		if err != nil {
			break
		}
		input, err := inputRecord(protocol.AgentInputRecordProvideInput, journal.Work.Goal, command.Payload, nil)
		if err != nil {
			outcome = "failed"
			break
		}
		runningTransitionID, err := daemon.options.newID()
		if err != nil {
			return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "failed")
		}
		acknowledgementID, err := daemon.options.newID()
		if err != nil {
			return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "failed")
		}
		active.inputMu.Lock()
		defer active.inputMu.Unlock()
		daemon.mu.Lock()
		accepted := daemon.running[key] == active && activeRunAcceptsCommand(active)
		process := active.process
		daemon.mu.Unlock()
		if !accepted {
			current, loadErr := daemon.store.LoadJournal(key)
			if loadErr == nil && current.InputCommandIntent != nil && current.InputCommandIntent.CommandID == command.CommandID && current.InputCommandIntent.PayloadDigest == payloadDigest {
				if current.InputCommandIntent.Outcome == "" {
					return daemon.completeProvideInputWithRetry(ctx, key, command.CommandID, payloadDigest, "failed")
				}
				daemon.signalOutboxFor(key)
				return true
			}
			return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "rejected")
		}
		prepared, created, err := daemon.store.PrepareProvideInput(key, state.InputCommandIntent{
			CommandID:           command.CommandID,
			PayloadDigest:       payloadDigest,
			RunningTransitionID: runningTransitionID,
			AckID:               acknowledgementID,
		})
		if err != nil {
			return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, "rejected")
		}
		intent := prepared.InputCommandIntent
		if intent == nil {
			return false
		}
		if !created {
			if intent.Outcome == "" {
				return daemon.completeProvideInputWithRetry(ctx, key, command.CommandID, payloadDigest, "failed")
			}
			daemon.signalOutboxFor(key)
			return true
		}
		if daemon.writeInputBounded(ctx, active, process, input) != nil {
			return daemon.completeProvideInputWithRetry(ctx, key, command.CommandID, payloadDigest, "failed")
		}
		return daemon.completeProvideInputWithRetry(ctx, key, command.CommandID, payloadDigest, "applied")
	}
	return daemon.queueCommandAcknowledgementWithContext(ctx, key, command.CommandID, outcome)
}

// commandRunSnapshot reads run ownership and native-session state together.
// Close recovery may clear nativeSession on a concurrent outbox worker, so
// command routing must use this protected snapshot after releasing daemon.mu.
func (daemon *daemon) commandRunSnapshot(key state.RunKey) (*runningRun, bool) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	active := daemon.running[key]
	return active, active != nil && active.nativeSession != nil
}

func activeRunAcceptsCommand(active *runningRun) bool {
	return active != nil && active.process != nil && !active.starting && !active.cancelled && !active.stale && !active.terminal && active.terminalizing == 0
}

func hasPendingCommandAcknowledgement(journal state.RunJournal, commandID string) bool {
	if commandID == "" {
		return false
	}
	for _, acknowledgement := range journal.PendingCommandAcknowledgements {
		if acknowledgement.CommandID == commandID {
			return true
		}
	}
	return false
}

func hasPendingCommandAcknowledgementOutcome(journal state.RunJournal, commandID, outcome string) bool {
	for _, acknowledgement := range journal.PendingCommandAcknowledgements {
		if acknowledgement.CommandID == commandID && acknowledgement.Outcome == outcome {
			return true
		}
	}
	return false
}

func (daemon *daemon) queueCommandAcknowledgement(key state.RunKey, commandID, outcome string) bool {
	return daemon.queueCommandAcknowledgementWithContext(context.Background(), key, commandID, outcome)
}

func (daemon *daemon) queueCommandAcknowledgementWithContext(ctx context.Context, key state.RunKey, commandID, outcome string) bool {
	rootContext := daemon.rootContext(ctx)
	acknowledgement := protocol.CommandAcknowledgement{RunID: key.RunID, CommandID: commandID, Outcome: outcome}
	for {
		if daemon.commandAcknowledgementRetired(key) {
			return false
		}
		if acknowledgement.AckID == "" {
			id, err := daemon.options.newID()
			if err != nil {
				if daemon.log != nil {
					daemon.log.Warn("generate_command_acknowledgement_id_failed", "run_id", key.RunID, "generation", key.Generation, "command_id", commandID, "error", err)
				}
				timer := daemon.timer(minimumInterval)
				select {
				case <-rootContext.Done():
					timer.Stop()
					return false
				case <-timer.Chan():
				}
				continue
			}
			acknowledgement.AckID = id
		}
		_, err := daemon.store.QueueCommandAcknowledgement(key, acknowledgement)
		if err == nil {
			daemon.signalOutbox()
			return true
		}
		if daemon.commandAcknowledgementRetired(key) {
			return false
		}
		if current, loadErr := daemon.store.LoadJournal(key); loadErr == nil && hasPendingCommandAcknowledgement(current, commandID) {
			daemon.signalOutboxFor(key)
			return true
		}
		if daemon.log != nil {
			daemon.log.Warn("queue_command_acknowledgement_failed", "run_id", key.RunID, "generation", key.Generation, "command_id", commandID, "error", err)
		}
		timer := daemon.timer(minimumInterval)
		select {
		case <-rootContext.Done():
			timer.Stop()
			return false
		case <-timer.Chan():
		}
	}
}

func (daemon *daemon) commandAcknowledgementRetired(key state.RunKey) bool {
	journal, err := daemon.store.LoadJournal(key)
	if state.IsNotFound(err) {
		return true
	}
	if err != nil {
		return false
	}
	return state.CommandAcknowledgementRetired(journal)
}

func (daemon *daemon) completeProvideInputWithRetry(ctx context.Context, key state.RunKey, commandID, payloadDigest, outcome string) bool {
	rootContext := daemon.rootContext(ctx)
	for {
		if _, err := daemon.store.CompleteProvideInput(key, commandID, payloadDigest, outcome); err == nil {
			daemon.signalOutbox()
			return true
		} else if daemon.log != nil {
			daemon.log.Warn("complete_provide_input_failed", "run_id", key.RunID, "generation", key.Generation, "command_id", commandID, "error", err)
		}
		timer := daemon.timer(minimumInterval)
		select {
		case <-rootContext.Done():
			timer.Stop()
			return false
		case <-timer.Chan():
		}
	}
}

func sameCommandAcknowledgement(left, right protocol.CommandAcknowledgement) bool {
	return left.Fence == right.Fence &&
		left.RunID == right.RunID &&
		left.CommandID == right.CommandID &&
		left.Outcome == right.Outcome &&
		left.AckID == right.AckID
}

func (daemon *daemon) claimWithRetry(ctx context.Context, runID string, request protocol.ClaimRequest) (protocol.ClaimResponse, error) {
	return daemon.claimWithRetryUntil(ctx, runID, request, time.Time{}, waitForRetry)
}

func (daemon *daemon) claimWithRetryWait(ctx context.Context, runID string, request protocol.ClaimRequest, waitFor func(context.Context, time.Duration) error) (protocol.ClaimResponse, error) {
	return daemon.claimWithRetryUntil(ctx, runID, request, time.Time{}, waitFor)
}

func (daemon *daemon) claimWithRetryUntil(ctx context.Context, runID string, request protocol.ClaimRequest, expiresAt time.Time, waitFor func(context.Context, time.Duration) error) (protocol.ClaimResponse, error) {
	delay := minimumInterval
	for {
		if !expiresAt.IsZero() && !daemon.now().Before(expiresAt) {
			return protocol.ClaimResponse{}, errAssignmentExpired
		}
		requestLimit := controlRequestLimit
		if !expiresAt.IsZero() {
			requestLimit = min(requestLimit, expiresAt.Sub(daemon.now()))
		}
		requestContext, cancel := context.WithTimeout(ctx, requestLimit)
		response, err := daemon.control.Claim(requestContext, runID, request)
		cancel()
		if err == nil {
			return response, err
		}
		if ctx.Err() != nil {
			return protocol.ClaimResponse{}, ctx.Err()
		}
		if !expiresAt.IsZero() && !daemon.now().Before(expiresAt) {
			return protocol.ClaimResponse{}, errAssignmentExpired
		}
		wait, shouldRetry := control.RetryDelay(ctx, err, delay)
		if !shouldRetry {
			return response, err
		}
		if wait < minimumInterval {
			wait = minimumInterval
		}
		if !expiresAt.IsZero() {
			wait = min(wait, expiresAt.Sub(daemon.now()))
		}
		if waitErr := waitFor(ctx, wait); waitErr != nil {
			return protocol.ClaimResponse{}, waitErr
		}
		delay = nextRetryDelay(delay)
	}
}

func retry(ctx context.Context, operation func() error) error {
	return retryWithWait(ctx, operation, waitForRetry)
}

func retryWithWait(ctx context.Context, operation func() error, waitFor func(context.Context, time.Duration) error) error {
	delay := minimumInterval
	for {
		if err := operation(); err == nil {
			return nil
		} else {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			wait, shouldRetry := control.RetryDelay(ctx, err, delay)
			if !shouldRetry {
				return err
			}
			if wait < minimumInterval {
				wait = minimumInterval
			}
			if waitErr := waitFor(ctx, wait); waitErr != nil {
				return waitErr
			}
			delay = nextRetryDelay(delay)
		}
	}
}

func waitForRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func nextRetryDelay(delay time.Duration) time.Duration {
	if delay < retryMaximum/2 {
		return delay * 2
	}
	return retryMaximum
}
