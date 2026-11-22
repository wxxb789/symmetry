package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

// Re-submits an already recorded exact receipt without invoking the external observer:
// recovery uses this barrier so a prior unknown write result is never upgraded by readback.
func (daemon *daemon) rewriteSupervisorHandoffStopReceipt(key state.RunKey, expected authority.SupervisorHandoff, receipt authority.StopReceipt) error {
	return daemon.persistSupervisorHandoffStopReceipt(key, expected, receipt, false)
}

func (daemon *daemon) persistSupervisorHandoffStopReceipt(key state.RunKey, expected authority.SupervisorHandoff, receipt authority.StopReceipt, observe bool) error {
	daemon.storeLifetimeMu.RLock()
	defer daemon.storeLifetimeMu.RUnlock()
	if daemon.storeClosing {
		return errDaemonStoreClosing
	}
	if !receipt.ValidForHandoff(expected) {
		return errors.New("supervisor handoff stop receipt does not match handoff")
	}
	record := daemon.options.recordSupervisorHandoffStopReceipt
	if record == nil {
		if daemon.store == nil {
			return errDaemonStoreClosing
		}
		record = daemon.store.RecordSupervisorHandoffStopReceipt
	}
	if _, err := record(key, expected.Clone(), receipt); err != nil {
		return err
	}
	if err := daemon.verifyDurableContainmentStopReceipt(key, expected.TargetPID, expected.TargetIdentity, receipt); err != nil {
		return err
	}
	if observe {
		return daemon.observeContainmentStop(key, expected.TargetPID, expected.TargetIdentity, receipt)
	}
	return nil
}

func (daemon *daemon) recoverUnresolvedInputIntents(ctx context.Context) error {
	journals, err := daemon.store.ListJournals()
	if err != nil {
		return err
	}
	rootContext := daemon.rootContext(ctx)
	recoveries := make([]state.RunJournal, 0, len(journals))
	var incomplete error
	for _, journal := range journals {
		if err := rootContext.Err(); err != nil {
			return err
		}
		if hasLinuxV2ContainmentState(journal) && !daemon.hasRun(journal.Key()) {
			if err := recoverLinuxV2ContainmentOwner(rootContext, daemon, journal); err != nil {
				incomplete = errors.Join(incomplete, fmt.Errorf("recover Linux v2 owner %s/%d: %w", journal.RunID, journal.Generation, err))
				continue
			}
			journal, err = daemon.store.LoadJournal(journal.Key())
			if err != nil {
				incomplete = errors.Join(incomplete, fmt.Errorf("reload Linux v2 owner journal %s/%d: %w", journal.RunID, journal.Generation, err))
				continue
			}
			if hasPendingLinuxV2Containment(journal) {
				incomplete = errors.Join(incomplete, fmt.Errorf("Linux v2 owner %s/%d remains pending after recovery", journal.RunID, journal.Generation))
				continue
			}
		}
		if !restartInputRecoveryRequired(journal) || daemon.hasRun(journal.Key()) {
			continue
		}
		// Recovery may stop a persisted process or resolve a pre-authority supervisor handoff:
		// the workspace-retention decision must be durable before either action is attempted.
		if supervisoryRecoveryRequired(journal) || journal.HasProcessDetails() {
			if journal.RetainWorkspace {
				daemon.rememberWorkspaceRetention(journal.Key())
			} else if err := daemon.retainUnknownGoalLaunchWorkspaceChecked(journal.Key()); err != nil {
				incomplete = errors.Join(incomplete, fmt.Errorf("retain recovered input workspace %s/%d: %w", journal.RunID, journal.Generation, err))
				continue
			} else {
				journal.RetainWorkspace = true
			}
		}
		var stopErr error
		handoffRecoveryPending := journal.ContainmentHandoff != nil
		if handoffRecoveryPending {
			key := journal.Key()
			stopErr = daemon.recoverPersistedSupervisorHandoff(journal.Key(), *journal.ContainmentHandoff)
			if stopErr != nil {
				incomplete = errors.Join(incomplete, fmt.Errorf("recover persisted supervisor handoff %s/%d: %w", journal.RunID, journal.Generation, stopErr))
				continue
			}
			journal, err = daemon.store.LoadJournal(key)
			if err != nil {
				incomplete = errors.Join(incomplete, fmt.Errorf("reload run after supervisor handoff recovery %s/%d: %w", key.RunID, key.Generation, err))
				continue
			}
			handoffRecoveryPending = journal.ContainmentHandoff != nil
			if handoffRecoveryPending {
				incomplete = errors.Join(incomplete, fmt.Errorf("recover persisted supervisor handoff %s/%d: supervisor handoff remains pending after recovery", journal.RunID, journal.Generation))
				continue
			}
		}
		if !handoffRecoveryPending && stopErr == nil && journal.HasProcessDetails() {
			stopErr = daemon.stopPersistedProcess(rootContext, journal)
			if stopErr == nil {
				_, stopErr = daemon.clearPersistedProcessDetails(journal.Key(), journal.PID, journal.ProcessIdentity)
			}
		}
		if stopErr != nil && daemon.log != nil {
			daemon.log.Warn("recovered_input_process_stop_pending", "run_id", journal.RunID, "generation", journal.Generation, "error", stopErr)
		}
		if !restartInputRecoveryRequired(journal) {
			continue
		}
		recoveries = append(recoveries, journal)
	}
	for _, journal := range recoveries {
		if err := rootContext.Err(); err != nil {
			return err
		}
		if daemon.hasRun(journal.Key()) {
			continue
		}
		entryContext, cancel := context.WithTimeout(rootContext, controlRequestLimit)
		if err := daemon.drainRestartInputOutbox(entryContext, journal.Key()); err != nil && !isConclusiveRestartInputFailure(err) {
			cancel()
			incomplete = errors.Join(incomplete, fmt.Errorf("drain input command outbox for %s/%d: %w", journal.RunID, journal.Generation, err))
			continue
		}
		err := daemon.queueRestartInputFailure(entryContext, journal.Key())
		cancel()
		if err != nil {
			incomplete = errors.Join(incomplete, fmt.Errorf("queue restart input failure for %s/%d: %w", journal.RunID, journal.Generation, err))
		}
	}
	if err := rootContext.Err(); err != nil {
		return err
	}
	return incomplete
}

func recoveredGenericProcessMarker(journal state.RunJournal) bool {
	if !journal.HasProcessDetails() {
		return false
	}
	switch journal.LocalState {
	case "terminal_pending", "cleanup_pending", "stale":
		return true
	default:
		return false
	}
}

// Prevents a restart from silently attaching to, replacing, or cleaning up around a native
// session whose stopped state is unproven; control-plane reattachment is deliberately out of scope.
func (daemon *daemon) recoverUnclosedGoalSessions(ctx context.Context) error {
	sessions, err := daemon.store.ListGoalSessions()
	if err != nil {
		return err
	}
	rootContext := daemon.rootContext(ctx)
	var pending error
	var blocked error
	for _, session := range sessions {
		if err := rootContext.Err(); err != nil {
			return err
		}
		entryContext, cancel := context.WithTimeout(rootContext, controlRequestLimit)
		err := daemon.recoverGoalSession(entryContext, session)
		cancel()
		if err != nil {
			if errors.Is(err, errRecoveryRegistrationReady) {
				pending = errors.Join(pending, err)
			} else {
				blocked = errors.Join(blocked, err)
			}
		}
	}
	if err := rootContext.Err(); err != nil {
		return err
	}
	if blocked != nil {
		return errors.Join(errRecoveryPending, blocked)
	}
	if pending != nil {
		return errors.Join(errRecoveryPending, errRecoveryRegistrationReady, pending)
	}
	return nil
}

func restartInputRecoveryRequired(journal state.RunJournal) bool {
	// Any retained process or pre-authority handoff is a restart barrier: the daemon has no
	// in-memory owner to reattach, so recovery must stop/retain it before RunKey reuse.
	if journal.ContainmentUnproven || journal.ContainmentHandoff != nil {
		return true
	}
	if journal.InputCommandIntent == nil && !supervisoryRecoveryRequired(journal) {
		return false
	}
	switch journal.LocalState {
	case "stale", "terminal_pending", "cleanup_pending":
		return false
	default:
		return true
	}
}

func (daemon *daemon) drainRestartInputOutbox(ctx context.Context, key state.RunKey) error {
	ctx = context.WithValue(ctx, restartOutboxRecoveryContextKey{}, true)
	delay := minimumInterval
	for {
		if daemon.hasRun(key) {
			return nil
		}
		journal, err := daemon.store.LoadJournal(key)
		if state.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if !restartInputRecoveryRequired(journal) {
			return nil
		}
		err = daemon.flushRun(ctx, journal)
		if err == nil {
			return nil
		}
		if errors.Is(err, errOutboxChanged) {
			continue
		}
		if isConclusiveRestartInputFailure(err) {
			return err
		}
		wait, retryable := restartInputRecoveryRetryDelay(ctx, err, delay)
		if !retryable {
			return err
		}
		timer := daemon.timer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.Chan():
		}
		delay = nextRetryDelay(delay)
	}
}

func restartInputRecoveryRetryDelay(ctx context.Context, err error, fallback time.Duration) (time.Duration, bool) {
	delay, retryable := control.RetryDelay(ctx, err, fallback)
	if !retryable {
		var responseError *control.ResponseError
		if !errors.As(err, &responseError) {
			return 0, false
		}
		delay, retryable = fallback, true
	}
	if delay < minimumInterval {
		delay = minimumInterval
	}
	return delay, retryable
}

func isConclusiveRestartInputFailure(err error) bool {
	return control.IsOwnershipLost(err) || control.IsTerminalGraceExpired(err)
}

func (daemon *daemon) queueRestartInputFailure(ctx context.Context, key state.RunKey) error {
	if daemon.hasRun(key) {
		return nil
	}
	journal, err := daemon.store.LoadJournal(key)
	if state.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !restartInputRecoveryRequired(journal) {
		return nil
	}
	// The terminal outbox may have settled while physical containment remains unresolved:
	// preserve that terminal and leave the process marker as the retry barrier.
	if durableTerminalPresent(journal) {
		return nil
	}
	recoveryError := "input command recovery cannot safely replay stdin"
	if supervisoryRecoveryRequired(journal) {
		recoveryError = "supervisory recovery cannot safely reattach a paused or controlled agent"
	}
	return daemon.queueRecoveryTerminalTransition(ctx, key, "failed", map[string]string{
		"stage":   "daemon_restart",
		"reason":  string(protocol.TaskResultReasonUnknownOutcome),
		"summary": recoveryError,
		"error":   recoveryError,
	})
}

func reconcileCancelCommand(commands []protocol.Command, key state.RunKey) string {
	for _, command := range commands {
		if command.Kind == "cancel" && command.RunID == key.RunID && command.Generation == key.Generation && command.CommandID != "" {
			return command.CommandID
		}
	}
	return ""
}

func isReconcileState(localState string) bool {
	switch localState {
	case "claimed", "running", "paused", "waiting_for_input", "cancelling":
		return true
	default:
		return false
	}
}

func hasFullFence(journal state.RunJournal) bool {
	fence := journal.Fence()
	return fence.RuntimeID != "" && fence.RuntimeEpoch > 0 && fence.Generation > 0 && fence.ClaimID != "" && fence.LeaseToken != ""
}

func (daemon *daemon) stopRecoveredJournal(journal state.RunJournal, reason string) {
	if journal.LocalState == "terminal_pending" {
		return
	}
	if journal.LocalState == "cleanup_pending" {
		daemon.enqueueCleanup(journal.Key())
		return
	}
	hasUnclosedGoalSession, err := daemon.recoveredGoalSessionRequiresStop(journal.Key())
	if err != nil {
		if daemon.log != nil {
			daemon.log.Warn("load_recovered_goal_session_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
		}
		return
	}
	if hasUnclosedGoalSession && !recoveredProcessRecorded(journal) {
		daemon.retainUnknownGoalLaunchWorkspace(journal.Key())
		if daemon.log != nil {
			daemon.log.Warn("stop_recovered_goal_session_unproven", "run_id", journal.RunID, "generation", journal.Generation)
		}
		return
	}
	if recoveredProcessRecorded(journal) {
		if hasUnclosedGoalSession {
			daemon.retainUnknownGoalLaunchWorkspace(journal.Key())
		}
		latest, allowed := daemon.reloadRecoveredStopBeforeSideEffect(journal)
		if !allowed {
			if daemon.log != nil {
				daemon.log.Warn("recovered_process_stop_blocked_by_terminal", "run_id", journal.RunID, "generation", journal.Generation)
			}
			return
		}
		journal = latest
		if journal.PID <= 0 || strings.TrimSpace(journal.ProcessIdentity) == "" || !daemon.canTerminatePersistedProcess(journal) {
			if daemon.log != nil {
				daemon.log.Warn("stop_recovered_process_unavailable", "run_id", journal.RunID, "generation", journal.Generation)
			}
			return
		}
		if err := daemon.stopPersistedProcess(daemon.rootContext(context.Background()), journal); err != nil {
			if daemon.log != nil {
				daemon.log.Warn("stop_recovered_process_failed", "run_id", journal.RunID, "generation", journal.Generation, "reason", reason, "error", err)
			}
			return
		}
		if _, err := daemon.clearPersistedProcessDetails(journal.Key(), journal.PID, journal.ProcessIdentity); err != nil {
			if daemon.log != nil {
				daemon.log.Warn("record_recovered_process_stop_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
			}
			return
		}
	}
	if err := daemon.resolveRecoveredGoalSessionStopped(journal.Key()); err != nil {
		if daemon.log != nil {
			daemon.log.Warn("resolve_recovered_goal_session_stop_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
		}
		return
	}
	updated, err := daemon.store.SetLocalState(journal.Key(), "stale")
	if err != nil {
		daemon.log.Warn("mark_recovered_journal_stale_failed", "run_id", journal.RunID, "error", err)
		return
	}
	if err := daemon.scheduleCleanup(context.Background(), updated); err != nil {
		daemon.log.Warn("cleanup_recovered_workspace_failed", "run_id", journal.RunID, "error", err)
		return
	}
	daemon.log.Warn("recovered_journal_stopped", "run_id", journal.RunID, "generation", journal.Generation, "reason", reason)
}

func recoveredProcessRecorded(journal state.RunJournal) bool {
	return journal.PID > 0 || strings.TrimSpace(journal.ProcessIdentity) != "" || !journal.StartedAt.IsZero()
}

func (daemon *daemon) canTerminatePersistedProcess(journal state.RunJournal) bool {
	if journal.ContainmentAuthority != nil {
		return daemon.options.terminatePersistAuthority != nil
	}
	return daemon.options.terminatePersist != nil
}

// No in-memory owner to race against: retain artifacts before process control, record the
// stopped process durably, then commit cancellation with its ack; a failed stop stays non-stale.
func (daemon *daemon) cancelRecoveredJournal(ctx context.Context, journal state.RunJournal, commandID string) bool {
	key := journal.Key()
	if journal.LocalState == "cleanup_pending" || (journal.LocalState == "terminal_pending" && journal.TerminalVerdict != "") {
		return false
	}
	if durableTerminalPresent(journal) {
		if commandID == "" || state.CommandAcknowledgementRetired(journal) {
			return false
		}
		return daemon.replayOrRejectCancellationAcknowledgement(ctx, key, commandID, journal)
	}
	if err := daemon.retainUnknownGoalLaunchWorkspaceChecked(key); err != nil {
		if daemon.log != nil {
			daemon.log.Warn("retain_recovered_cancel_workspace_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
		}
		return false
	}
	hasUnclosedGoalSession, err := daemon.recoveredGoalSessionRequiresStop(key)
	if err != nil {
		if daemon.log != nil {
			daemon.log.Warn("load_recovered_goal_session_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
		}
		return false
	}
	if recoveredProcessRecorded(journal) {
		if strings.TrimSpace(journal.ProcessIdentity) == "" || !daemon.canTerminatePersistedProcess(journal) {
			if daemon.log != nil {
				daemon.log.Warn("stop_recovered_cancel_process_unavailable", "run_id", journal.RunID, "generation", journal.Generation)
			}
			return false
		}
		latest, allowed := daemon.reloadRecoveredStopBeforeSideEffect(journal)
		if !allowed {
			if daemon.log != nil {
				daemon.log.Warn("recovered_cancel_process_stop_blocked_by_terminal", "run_id", journal.RunID, "generation", journal.Generation)
			}
			if durableTerminalPresent(latest) && commandID != "" {
				return daemon.replayOrRejectCancellationAcknowledgement(ctx, key, commandID, latest)
			}
			return false
		}
		journal = latest
		if err := daemon.stopPersistedProcess(ctx, journal); err != nil {
			if daemon.log != nil {
				daemon.log.Warn("stop_recovered_cancel_process_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
			}
			return false
		}
		if _, err := daemon.clearPersistedProcessDetails(key, journal.PID, journal.ProcessIdentity); err != nil {
			if daemon.log != nil {
				daemon.log.Warn("record_recovered_cancel_process_stop_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
			}
			return false
		}
	} else if hasUnclosedGoalSession {
		// A missing process marker is not proof that an uncertain native launch never escaped:
		// keep the lineage barrier until a bounded stop is recorded, do not acknowledge yet.
		if daemon.log != nil {
			daemon.log.Warn("stop_recovered_cancel_goal_session_unproven", "run_id", journal.RunID, "generation", journal.Generation)
		}
		return false
	}
	queued := false
	if commandID != "" {
		if !daemon.queueCancellationReceipt(ctx, key, commandID) {
			return false
		}
		queued = true
	} else if err := daemon.queueTerminalTransitionWithRetry(ctx, key, "cancelled", map[string]string{"reason": string(protocol.TaskResultReasonCancelled)}); err != nil {
		if daemon.log != nil {
			daemon.log.Warn("queue_recovered_cancel_transition_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
		}
		return false
	} else {
		queued = true
	}
	if err := daemon.resolveRecoveredGoalSessionStopped(key); err != nil {
		if daemon.log != nil {
			daemon.log.Warn("resolve_recovered_goal_session_stop_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
		}
		return queued
	}
	return true
}

func (daemon *daemon) recoveredGoalSessionRequiresStop(key state.RunKey) (bool, error) {
	sessions, err := daemon.store.ListGoalSessions()
	if err != nil {
		return false, err
	}
	for _, session := range sessions {
		if session.RunID == key.RunID && session.Generation == key.Generation && session.SessionState != state.GoalSessionStateClosed && session.SessionState != state.GoalSessionStateAvailable {
			return true, nil
		}
	}
	return false, nil
}

func (daemon *daemon) resolveRecoveredGoalSessionStopped(key state.RunKey) error {
	sessions, err := daemon.store.ListGoalSessions()
	if err != nil {
		return err
	}
	for _, session := range sessions {
		if session.RunID != key.RunID || session.Generation != key.Generation {
			continue
		}
		if session.SessionState != state.GoalSessionStateClosed && session.LaunchState == state.GoalSessionLaunchStateAttached && !session.NeedsReconciliation() && session.ControlSessionID == "" {
			journal, loadErr := daemon.store.LoadJournal(key)
			if loadErr != nil {
				return loadErr
			}
			if legacyAttach, ok := legacyGoalSessionAttachDelivery(journal, session); ok {
				if _, err := daemon.quarantineLegacyGoalSessionAttach(key, session, legacyAttach); err != nil {
					return err
				}
				continue
			}
			if legacyGoalSessionAttachmentBarrier(session, journal) {
				continue
			}
			if retiredLegacyGoalSessionAttach(journal, session) {
				if _, err := daemon.store.SetGoalSessionState(session.Key(), state.GoalSessionStateUnavailable); err != nil {
					return err
				}
				continue
			}
			if hasReadyGoalSessionAttach(journal, session) {
				// A prior attach may have committed remotely without a durable local
				// correlation: keep the exact request and native handle for replay.
				continue
			}
		}
		if session.HasVerifiedControlAttachment() && session.SessionState == state.GoalSessionStateBusy {
			if err := daemon.recordStoppedGoalSession(key, session.Key()); err != nil {
				return err
			}
			continue
		}
		if session.HasVerifiedControlAttachment() && session.SessionState == state.GoalSessionStateUnavailable {
			if _, err := daemon.ensureRetainedGoalSessionStopDelivery(key, session, state.RunJournal{}); err != nil {
				return err
			}
			continue
		}
		if session.NeedsReconciliation() {
			if _, err := daemon.store.ResolveGoalSessionUncertainStopped(session.Key(), session.Compatibility()); err != nil {
				return err
			}
		} else if session.SessionState != state.GoalSessionStateClosed && session.SessionState != state.GoalSessionStateAvailable {
			if _, err := daemon.store.CloseGoalSession(session.Key()); err != nil {
				return err
			}
		}
	}
	return nil
}

func retainedGoalSessionTerminalState(session state.GoalSessionJournal, journal state.RunJournal) bool {
	if session.SessionState == state.GoalSessionStateClosed {
		return true
	}
	return retainedGoalSessionNativeStopWitness(session, journal)
}

func retainedGoalSessionNativeStopWitness(session state.GoalSessionJournal, journal state.RunJournal) bool {
	if session.HasKnownRetainedResumeNativeStop() {
		// A resumed native process can be conclusively closed before the current attachment is
		// read back: native-stop evidence, not a release receipt — the ready attach still binds.
		return true
	}
	if !session.HasVerifiedControlAttachment() {
		return retainedGoalSessionAttachPendingStopped(session, journal)
	}
	if session.SessionState == state.GoalSessionStateAvailable {
		// A crash may occur after the local availability write and before the stop delivery is
		// moved to delivered history: the immutable certificate proves the session is released.
		return session.StopCertificate != nil
	}
	if session.SessionState != state.GoalSessionStateUnavailable {
		return false
	}
	for _, delivery := range journal.PendingGoalDeliveries {
		if sameRetainedGoalSessionStopDelivery(delivery, session) {
			return true
		}
	}
	for _, delivery := range journal.DeliveredGoalDeliveries {
		if sameRetainedGoalSessionStopDelivery(delivery, session) {
			return true
		}
	}
	for _, retired := range journal.RetiredGoalDeliveries {
		if sameRetainedGoalSessionStopDelivery(retired.Delivery, session) {
			return true
		}
	}
	return false
}

func retainedGoalSessionAvailable(session state.GoalSessionJournal) bool {
	return session.SessionState == state.GoalSessionStateAvailable &&
		!session.NeedsReconciliation() && session.HasVerifiedControlAttachment() && session.StopCertificate != nil
}

func retainedGoalSessionAttachPendingStopped(session state.GoalSessionJournal, journal state.RunJournal) bool {
	return session.SessionState == state.GoalSessionStateUnavailable &&
		goalSessionAttachmentMappingPending(session, journal)
}

func goalSessionAttachmentMappingPending(session state.GoalSessionJournal, journal state.RunJournal) bool {
	return session.LaunchState == state.GoalSessionLaunchStateAttached && session.NativeSessionID != "" &&
		!session.NeedsReconciliation() && session.ControlSessionID == "" && hasReplayableGoalSessionAttach(journal, session)
}

func hasReplayableGoalSessionAttach(journal state.RunJournal, session state.GoalSessionJournal) bool {
	for _, delivery := range journal.PendingGoalDeliveries {
		if delivery.Kind != state.GoalDeliverySessionAttach || !delivery.Ready || delivery.SessionAttach == nil ||
			delivery.SessionAttach.LocalHandleID != session.LocalHandleID || delivery.SessionAttach.BindingID != session.BindingID {
			continue
		}
		return delivery.SessionAttach.BindingID != "" || delivery.SessionAttach.ServerIssuedBinding
	}
	return false
}

// The only rebind window whose absent Ready marker proves Adapter.Start was not yet permitted:
// the predecessor stop certificate remains valid native-stop evidence for this narrow path.
func resumeGoalSessionPreStartAttachment(session state.GoalSessionJournal, delivery state.GoalDelivery) bool {
	if delivery.Kind != state.GoalDeliverySessionAttach || delivery.Ready || delivery.SessionAttach == nil ||
		session.SessionMode != state.GoalSessionModeResume || !session.ResumeStartPending || session.ResumeSourceStopCertificate == nil || session.StopCertificate != nil ||
		session.SessionState != state.GoalSessionStateBusy || session.LaunchState != state.GoalSessionLaunchStateAttached ||
		session.NeedsReconciliation() || session.ControlSessionID == "" || session.ControlAttachmentReceiptID != "" || session.NativeSessionID == "" {
		return false
	}
	payload := delivery.SessionAttach
	return !payload.ServerIssuedBinding && payload.LocalHandleID == session.LocalHandleID && payload.BindingID == session.BindingID
}

// A rebound retained session whose new Control attachment may be reconstructed from its exact
// ready delivery; unlike a fresh attach the prior Control session ID survives the rebind.
func goalSessionResumeAttachmentPending(session state.GoalSessionJournal, journal state.RunJournal) bool {
	if session.SessionMode != state.GoalSessionModeResume || session.ResumeSourceStopCertificate == nil || session.StopCertificate != nil ||
		(session.SessionState != state.GoalSessionStateBusy && session.SessionState != state.GoalSessionStateUnavailable) || session.LaunchState != state.GoalSessionLaunchStateAttached ||
		session.NeedsReconciliation() || session.ControlSessionID == "" || session.ControlAttachmentReceiptID != "" || session.NativeSessionID == "" {
		return false
	}
	for _, delivery := range journal.PendingGoalDeliveries {
		if delivery.Kind != state.GoalDeliverySessionAttach || !delivery.Ready || delivery.SessionAttach == nil {
			continue
		}
		payload := delivery.SessionAttach
		if !payload.ServerIssuedBinding && payload.LocalHandleID == session.LocalHandleID && payload.BindingID == session.BindingID {
			return true
		}
	}
	return false
}

func retiredResumedGoalSessionAttachment(journal state.RunJournal, session state.GoalSessionJournal) bool {
	if session.SessionMode != state.GoalSessionModeResume || session.ResumeSourceStopCertificate == nil ||
		session.ControlSessionID == "" || session.ControlAttachmentReceiptID != "" ||
		session.SessionState == state.GoalSessionStateClosed || session.LaunchState != state.GoalSessionLaunchStateAttached {
		return false
	}
	for _, retired := range journal.RetiredGoalDeliveries {
		if retired.Delivery.Kind != state.GoalDeliverySessionAttach || retired.Delivery.SessionAttach == nil {
			continue
		}
		payload := retired.Delivery.SessionAttach
		if !payload.ServerIssuedBinding && payload.LocalHandleID == session.LocalHandleID && payload.BindingID == session.BindingID {
			return true
		}
	}
	return false
}

func (daemon *daemon) retryNativeUsageRecoveryPersistence(key state.RunKey, active *runningRun, ownerClaim uint64, cause error) {
	now := daemon.now().UTC()
	daemon.mu.Lock()
	if daemon.running[key] != active || !nativeTerminalOwnedBy(active, ownerClaim) || active.nativeUsageRetryOwnerClaim != ownerClaim || active.nativeUsageFinalized {
		daemon.mu.Unlock()
		return
	}
	active.scheduleRecoveryUsageRetry(now)
	daemon.mu.Unlock()
	retentionErr := daemon.retainUnknownGoalLaunchWorkspaceChecked(key)
	daemon.signalOutboxFor(key)
	if daemon.log != nil {
		daemon.log.Error("persist_native_goal_usage_recovery_failed", "run_id", key.RunID, "generation", key.Generation, "error", errors.Join(cause, retentionErr))
	}
}

func (daemon *daemon) queueNativeUsageRecoveryTerminal(ctx context.Context, journal state.RunJournal, cause error) (state.RunJournal, error) {
	if !journal.NativeUsageRecoveryRequired {
		return journal, nil
	}
	if journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending" {
		if journal.TerminalState == "" {
			return journal, errors.New("native usage recovery terminal state is missing")
		}
		if settlement := journal.NativeUsageTerminalRecovery; settlement != nil {
			matches, present := state.TerminalIntentMatches(journal, settlement.State, settlement.Payload)
			if !present || !matches {
				return journal, errors.New("native usage recovery terminal conflicts with journal")
			}
			if settlement.CommandID != "" {
				matches, present = state.TerminalCommandIntentMatches(journal, settlement.CommandID, settlement.CommandOutcome)
				if !present || !matches {
					return journal, errors.New("native usage recovery acknowledgement conflicts with journal")
				}
			}
		}
		return journal, nil
	}
	if settlement := journal.NativeUsageTerminalRecovery; settlement != nil {
		var err error
		if settlement.CommandID != "" {
			err = daemon.queueTerminalTransitionAndAcknowledgementWithContext(ctx, journal.Key(), settlement.State, settlement.Payload, settlement.CommandID, settlement.CommandOutcome)
		} else {
			err = daemon.queueTerminalTransitionWithRetry(ctx, journal.Key(), settlement.State, settlement.Payload)
		}
		if err != nil {
			return journal, err
		}
		updated, loadErr := daemon.store.LoadJournal(journal.Key())
		if loadErr != nil {
			return journal, loadErr
		}
		return updated, nil
	}
	errorText := "native Goal usage persistence retry budget exhausted"
	if cause != nil {
		errorText = cause.Error()
	}
	if err := daemon.queueTerminalTransitionWithRetry(ctx, journal.Key(), "failed", map[string]string{
		"stage":   "native_goal_usage_recovery",
		"reason":  string(protocol.TaskResultReasonUnknownOutcome),
		"summary": "native Goal usage persistence retry budget exhausted",
		"error":   errorText,
	}); err != nil {
		return journal, err
	}
	updated, err := daemon.store.LoadJournal(journal.Key())
	if err != nil {
		return journal, err
	}
	return updated, nil
}

func nativeUsageRecoveryUsage(journal state.RunJournal) (protocol.Usage, bool) {
	for _, delivery := range journal.PendingGoalDeliveries {
		if delivery.Kind == state.GoalDeliveryUsage && delivery.DeliveryID == nativeGoalUsageKey && delivery.Usage != nil {
			return cloneNativeUsage(*delivery.Usage), true
		}
	}
	for _, delivery := range journal.DeliveredGoalDeliveries {
		if delivery.Kind == state.GoalDeliveryUsage && delivery.DeliveryID == nativeGoalUsageKey && delivery.Usage != nil {
			return cloneNativeUsage(*delivery.Usage), true
		}
	}
	for _, delivery := range journal.RetiredGoalDeliveries {
		if delivery.Delivery.Kind == state.GoalDeliveryUsage && delivery.Delivery.DeliveryID == nativeGoalUsageKey && delivery.Delivery.Usage != nil {
			return cloneNativeUsage(*delivery.Delivery.Usage), true
		}
	}
	return protocol.Usage{}, false
}

func (daemon *daemon) clearNativeUsageRecoveryBarrier(key state.RunKey) error {
	journal, err := daemon.store.LoadJournal(key)
	if err != nil || !journal.NativeUsageRecoveryRequired {
		return err
	}
	usage, ok := nativeUsageRecoveryUsage(journal)
	if !ok {
		return errors.New("native usage recovery body is unavailable")
	}
	_, err = daemon.store.ClearNativeUsageRecoveryRequired(key, usage)
	return err
}

// Restores a pre-existing immutable usage body; a legacy marker without one gets one explicit
// unknown accounting receipt before terminal recovery is allowed to proceed.
func (daemon *daemon) recoverNativeUsageDelivery(journal state.RunJournal) (state.RunJournal, protocol.Usage, error) {
	if usage, ok := nativeUsageRecoveryUsage(journal); ok {
		return journal, usage, nil
	}
	usage, shouldQueue, err := daemon.prepareNativeGoalUsage(journal.Key(), nil)
	if err != nil {
		return journal, protocol.Usage{}, err
	}
	if !shouldQueue {
		return journal, protocol.Usage{}, errors.New("native usage recovery body is unavailable")
	}
	updated, err := daemon.store.QueueGoalUsage(journal.Key(), usage)
	if err != nil {
		return journal, protocol.Usage{}, err
	}
	return updated, usage, nil
}

func legacyGoalSessionAttachmentBarrier(session state.GoalSessionJournal, journal state.RunJournal) bool {
	if session.SessionState == state.GoalSessionStateClosed || session.LaunchState != state.GoalSessionLaunchStateAttached || session.NeedsReconciliation() || session.ControlSessionID != "" || session.NativeSessionID == "" {
		return false
	}
	for _, delivery := range journal.PendingGoalDeliveries {
		if delivery.Kind == state.GoalDeliverySessionAttach && delivery.Ready && delivery.SessionAttach != nil &&
			delivery.SessionAttach.LocalHandleID == session.LocalHandleID && delivery.SessionAttach.BindingID == "" && !delivery.SessionAttach.ServerIssuedBinding {
			return true
		}
	}
	return retiredLegacyGoalSessionAttach(journal, session)
}

// Completes a cancel that arrived after lease loss made the run stale: Control moves the Run to
// cancelling and rejects renewal, so the receipt follows once the process owner is proven stopped.
func (daemon *daemon) settleStaleCancellation(ctx context.Context, key state.RunKey) bool {
	daemon.mu.Lock()
	active := daemon.running[key]
	if active == nil || !active.stale || active.cancelCommandID == "" || active.process == nil || active.processStopWitness != active.process {
		daemon.mu.Unlock()
		return false
	}
	commandID := active.cancelCommandID
	stopPID, stopIdentity := active.processStopPID, active.processStopIdentity
	daemon.mu.Unlock()
	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return false
	}
	if journal.HasProcessDetails() && (journal.PID != stopPID || journal.ProcessIdentity != stopIdentity) {
		return false
	}
	if durableTerminalPresent(journal) {
		return daemon.replayOrRejectCancellationAcknowledgement(ctx, key, commandID, journal)
	}
	receipt := daemon.queueCancellationReceipt(ctx, key, commandID)
	daemon.finishDeferredCommand(commandKey{run: key, id: commandID}, receipt)
	return receipt
}
