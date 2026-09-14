package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

var errRecoveryPending = errors.New("local recovery remains pending")
var errRecoveryRegistrationReady = errors.New("durable recovery is ready for registration")
var errNativePhysicalRecoveryPending = errors.New("native physical stop reconciliation remains pending")

// Windows installs the authenticated helper-release implementation from its
// build-tagged termination file. Non-Windows authority records are test-only
// compatibility data and retain the existing no-op release boundary.
var releasePersistedContainmentAuthority = func(int, string, *authority.Supervisor) error {
	return nil
}

// A successful recovered stop survives local persistence retries, not restart.
type recoveredProcessStop struct {
	pid       int
	identity  string
	startedAt time.Time
	authority *authority.Supervisor
}

func (stop recoveredProcessStop) matches(journal state.RunJournal) bool {
	if stop.pid != journal.PID || stop.identity != journal.ProcessIdentity || !stop.startedAt.Equal(journal.StartedAt) {
		return false
	}
	if stop.authority == nil || journal.ContainmentAuthority == nil {
		return stop.authority == nil && journal.ContainmentAuthority == nil
	}
	return stop.authority.Equal(*journal.ContainmentAuthority)
}

// Each entry keeps its existing ordering, while its caller owns retry scheduling.
func (daemon *daemon) recoverGoalSession(rootContext context.Context, session state.GoalSessionJournal) error {
	key := state.RunKey{RunID: session.RunID, Generation: session.Generation}
	if key.RunID == "" || key.Generation <= 0 || daemon.hasRun(key) {
		return nil
	}
	journal, loadErr := daemon.store.LoadJournal(key)
	if loadErr != nil {
		if state.IsNotFound(loadErr) {
			if retainedGoalSessionAvailable(session) {
				// A successfully released retained session deliberately outlives
				// the completed RunJournal. Its own journal owns the resume
				// identity and workspace binding from this point onward.
				return nil
			}
			if session.SessionState != state.GoalSessionStateClosed {
				if _, markErr := daemon.store.MarkGoalSessionUncertain(session.Key(), "associated run journal is unavailable during daemon recovery"); markErr != nil {
					return fmt.Errorf("mark Goal session uncertain without run %s/%d: %w", key.RunID, key.Generation, markErr)
				}
			}
			return nil
		}
		return fmt.Errorf("load associated run %s/%d: %w", key.RunID, key.Generation, loadErr)
	}
	processEvidence := journal.HasProcessDetails()
	terminalKnown := journal.TerminalState != "" || journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending" || journal.LocalState == "stale"
	resumePreStart := session.ResumeStartPending
	for _, delivery := range journal.PendingGoalDeliveries {
		if delivery.Kind != state.GoalDeliverySessionAttach || delivery.DeliveryID != session.LocalHandleID || delivery.Ready {
			continue
		}
		if resumePreStart && resumeGoalSessionPreStartAttachment(session, delivery) {
			updated, readyErr := daemon.markGoalSessionAttachDeliveryReady(key, session.LocalHandleID)
			if readyErr != nil {
				return fmt.Errorf("restore retained pi native-start boundary for %s/%d: %w", key.RunID, key.Generation, readyErr)
			}
			journal = updated
			resumePreStart = true
			break
		}
		updated, discardErr := daemon.store.DiscardUnreadyGoalSessionAttachDelivery(key, session.LocalHandleID)
		if discardErr != nil {
			return fmt.Errorf("discard unready Goal session attach for %s/%d: %w", key.RunID, key.Generation, discardErr)
		}
		journal = updated
		break
	}
	attachMappingPending := goalSessionAttachmentMappingPending(session, journal)
	resumeAttachmentPending := goalSessionResumeAttachmentPending(session, journal)
	if attachMappingPending || resumeAttachmentPending {
		for _, delivery := range journal.PendingGoalDeliveries {
			if delivery.Kind != state.GoalDeliverySessionAttach || !delivery.Ready || delivery.SessionAttach == nil || delivery.DeliveryID != session.LocalHandleID {
				continue
			}
			updated, _, deliverErr := daemon.deliverGoalDelivery(rootContext, journal, state.GoalDeliverySessionAttach, delivery.DeliveryID, nil)
			if deliverErr == nil {
				journal = updated
				if session, loadErr = daemon.store.LoadGoalSession(session.Key()); loadErr != nil {
					return fmt.Errorf("reload Goal session after attachment readback for %s/%d: %w", key.RunID, key.Generation, loadErr)
				}
			} else {
				// Definitive delivery rejection retires the durable item and
				// returns that post-retirement journal with an error. Preserve
				// it so the following recovery branch can quarantine a
				// rejected pre-start resume rather than retaining a stale copy.
				journal = updated
				if daemon.log != nil {
					daemon.log.Warn("recover_goal_session_attachment_failed", "run_id", key.RunID, "generation", key.Generation, "error", deliverErr)
				}
			}
			break
		}
	}
	if legacyAttach, ok := legacyGoalSessionAttachDelivery(journal, session); ok {
		updated, quarantineErr := daemon.quarantineLegacyGoalSessionAttach(key, session, legacyAttach)
		if quarantineErr != nil {
			return fmt.Errorf("quarantine legacy Goal session attach for %s/%d: %w", key.RunID, key.Generation, quarantineErr)
		}
		journal = updated
		if session, loadErr = daemon.store.LoadGoalSession(session.Key()); loadErr != nil {
			return fmt.Errorf("reload legacy-quarantined Goal session for %s/%d: %w", key.RunID, key.Generation, loadErr)
		}
	} else if retiredLegacyGoalSessionAttach(journal, session) {
		if _, closeErr := daemon.store.CloseGoalSession(session.Key()); closeErr != nil {
			return fmt.Errorf("close retired legacy Goal session for %s/%d: %w", key.RunID, key.Generation, closeErr)
		}
		if session, loadErr = daemon.store.LoadGoalSession(session.Key()); loadErr != nil {
			return fmt.Errorf("reload retired legacy Goal session for %s/%d: %w", key.RunID, key.Generation, loadErr)
		}
	}
	if retiredResumedGoalSessionAttachment(journal, session) && (resumePreStart || session.HasKnownRetainedResumeNativeStop()) {
		// Control definitively rejected the new attachment before a current
		// receipt existed. Its predecessor stop certificate cannot release a
		// different binding, so quarantine this local resume identity instead
		// of fabricating a stop request for the old attachment.
		//
		// A typed native rejection is durable before its terminal outbox
		// transition. Do not erase that marker by closing the session first:
		// a second crash would otherwise recover it as unknown_outcome.
		if !terminalKnown && session.ResumeTerminalReason == string(protocol.TaskResultReasonResumeRejected) && session.HasKnownRetainedResumeNativeStop() {
			if err := daemon.queueNativeGoalUsage(rootContext, key, nil); err != nil {
				return fmt.Errorf("queue rejected retained pi usage for %s/%d: %w", key.RunID, key.Generation, err)
			}
			if err := daemon.queueRecoveryTerminalTransition(rootContext, key, "failed", map[string]string{
				"stage":   "goal_session_recovery",
				"reason":  string(protocol.TaskResultReasonResumeRejected),
				"summary": "retained pi session was rejected before native work started",
				"error":   "retained pi session was rejected before native work started",
			}); err != nil {
				return fmt.Errorf("queue rejected retained pi terminal transition for %s/%d: %w", key.RunID, key.Generation, err)
			}
			if journal, loadErr = daemon.store.LoadJournal(key); loadErr != nil {
				return fmt.Errorf("reload rejected retained pi run after terminal recovery for %s/%d: %w", key.RunID, key.Generation, loadErr)
			}
			terminalKnown = journal.TerminalState != "" || journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending" || journal.LocalState == "stale"
		}
		if _, closeErr := daemon.store.CloseGoalSession(session.Key()); closeErr != nil {
			return fmt.Errorf("close definitively rejected retained pi attachment for %s/%d: %w", key.RunID, key.Generation, closeErr)
		}
		if session, loadErr = daemon.store.LoadGoalSession(session.Key()); loadErr != nil {
			return fmt.Errorf("reload rejected retained pi attachment for %s/%d: %w", key.RunID, key.Generation, loadErr)
		}
	}
	if updated, ensureErr := daemon.ensureRetainedGoalSessionStopDelivery(key, session, journal); ensureErr != nil {
		return fmt.Errorf("restore retained Goal session stop delivery for %s/%d: %w", key.RunID, key.Generation, ensureErr)
	} else {
		journal = updated
	}
	if session, loadErr = daemon.store.LoadGoalSession(session.Key()); loadErr != nil {
		return fmt.Errorf("reload retained Goal session after stop delivery recovery for %s/%d: %w", key.RunID, key.Generation, loadErr)
	}
	if resumePreStart && session.HasVerifiedControlAttachment() && session.SessionState == state.GoalSessionStateBusy {
		// Ready was not durable before the interrupted daemon reached
		// adapter.Start, so the predecessor certificate remains positive
		// evidence that this retained native session is stopped. Attach the
		// new reservation first, then release that exact binding.
		if err := daemon.recordStoppedGoalSession(key, session.Key()); err != nil {
			return fmt.Errorf("release pre-start retained pi session for %s/%d: %w", key.RunID, key.Generation, err)
		}
		if journal, loadErr = daemon.store.LoadJournal(key); loadErr != nil {
			return fmt.Errorf("reload pre-start retained pi run after stop recovery for %s/%d: %w", key.RunID, key.Generation, loadErr)
		}
		if session, loadErr = daemon.store.LoadGoalSession(session.Key()); loadErr != nil {
			return fmt.Errorf("reload pre-start retained pi session after stop recovery for %s/%d: %w", key.RunID, key.Generation, loadErr)
		}
	}
	if retiredRetainedGoalSessionStopDelivery(session, journal) {
		if _, closeErr := daemon.store.CloseGoalSession(session.Key()); closeErr != nil {
			return fmt.Errorf("close definitively rejected retained Goal session for %s/%d: %w", key.RunID, key.Generation, closeErr)
		}
		if session, loadErr = daemon.store.LoadGoalSession(session.Key()); loadErr != nil {
			return fmt.Errorf("reload rejected retained Goal session for %s/%d: %w", key.RunID, key.Generation, loadErr)
		}
	}
	// A durable stopped delivery is written only after native Close succeeds.
	// It remains a stronger native-stop fact than a stale process marker in
	// the separate RunJournal, including the crash window before that marker
	// is cleared. A generic closed session is not that witness: it still has
	// to reconcile any persisted process identity.
	nativeStopWitness := retainedGoalSessionNativeStopWitness(session, journal)
	if nativeStopWitness && processEvidence {
		if err := daemon.clearNativeProcessDetailsExact(key, journal.PID, journal.ProcessIdentity); err != nil {
			return fmt.Errorf("clear stale stopped native process record for %s/%d: %w", key.RunID, key.Generation, err)
		}
		processEvidence = false
	}
	stoppedWitness := !processEvidence && retainedGoalSessionTerminalState(session, journal)
	if terminalKnown && stoppedWitness {
		return nil
	}

	// Retention is durable before any process-control action. The native
	// session could have changed the worktree after the last Run event.
	if err := daemon.retainUnknownGoalLaunchWorkspaceChecked(key); err != nil {
		return fmt.Errorf("retain workspace for recovered Goal session %s/%d: %w", key.RunID, key.Generation, err)
	}
	legacyAttachmentBarrier := legacyGoalSessionAttachmentBarrier(session, journal)
	attachedStopRecovery := attachMappingPending || resumeAttachmentPending || resumePreStart || (session.HasVerifiedControlAttachment() && session.SessionState == state.GoalSessionStateBusy)
	goalSessionUncertainPersisted := false
	var stopErr error
	if !stoppedWitness && !attachedStopRecovery && !legacyAttachmentBarrier && session.SessionState != state.GoalSessionStateClosed && session.LaunchState != state.GoalSessionLaunchStateUncertain {
		if _, err := daemon.store.MarkGoalSessionUncertain(session.Key(), "native Goal session was left unclosed across daemon restart"); err != nil {
			return fmt.Errorf("mark Goal session uncertain for %s/%d: %w", key.RunID, key.Generation, err)
		}
		goalSessionUncertainPersisted = true
	}
	if !nativeStopWitness && journal.PID > 0 && strings.TrimSpace(journal.ProcessIdentity) != "" {
		stopErr = daemon.stopPersistedProcess(rootContext, journal)
		if stopErr != nil {
			if errors.Is(stopErr, context.Canceled) || errors.Is(stopErr, context.DeadlineExceeded) || state.IsNotFound(stopErr) {
				return fmt.Errorf("stop recovered native process %s/%d: %w", key.RunID, key.Generation, stopErr)
			}
			// Physical stop and durable execution failure are separate decisions.
			// Keep the exact marker/authority for a later reconciliation pass, but
			// do not let an unproven stop erase the terminal accounting fallback.
			if !goalSessionUncertainPersisted &&
				session.SessionState != state.GoalSessionStateClosed && session.LaunchState != state.GoalSessionLaunchStateUncertain {
				if _, uncertainErr := daemon.store.MarkGoalSessionUncertain(session.Key(), "native Goal session stop is unproven"); uncertainErr != nil {
					return fmt.Errorf("stop recovered native process %s/%d: %w", key.RunID, key.Generation, errors.Join(stopErr, fmt.Errorf("mark Goal session uncertain: %w", uncertainErr)))
				}
				goalSessionUncertainPersisted = true
			}
		}
		if stopErr == nil {
			if attachedStopRecovery || session.NeedsReconciliation() {
				if err := daemon.recordStoppedGoalSession(key, session.Key()); err != nil {
					return fmt.Errorf("record stopped recovered Goal session %s/%d: %w", key.RunID, key.Generation, err)
				}
			}
			if _, err := daemon.clearPersistedProcessDetails(key, journal.PID, journal.ProcessIdentity); err != nil {
				return fmt.Errorf("record stopped native process %s/%d: %w", key.RunID, key.Generation, err)
			}
		}
	}
	if attachedStopRecovery && !stoppedWitness && !nativeStopWitness && (journal.PID <= 0 || strings.TrimSpace(journal.ProcessIdentity) == "") {
		// A ready attach only proves that its request may have committed. With
		// no recoverable native process identity it cannot prove stopped, so
		// retain the exact request and local handle for readback/reconciliation.
		return nil
	}
	if legacyAttachmentBarrier && (journal.PID <= 0 || strings.TrimSpace(journal.ProcessIdentity) == "") {
		// An old ready attach lacks binding authority and cannot establish a
		// stopped receipt. Without a verifiable native process record, retain
		// its local handle for explicit reconciliation rather than terminalize.
		return nil
	}
	// Restart recovery has no native final-result boundary, even when the
	// persisted process can be stopped successfully. Never promote the latest
	// cumulative observation to a final total on this path.
	if err := daemon.queueNativeUnknownUsageRecovery(key); err != nil {
		return fmt.Errorf("queue unknown recovered Goal usage for %s/%d: %w", key.RunID, key.Generation, err)
	}
	if journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending" {
		if stopErr != nil {
			return errors.Join(errRecoveryRegistrationReady, errNativePhysicalRecoveryPending, stopErr)
		}
		return nil
	}
	if restartInputRecoveryRequired(journal) {
		// Input recovery owns the daemon_restart payload. A native fallback must
		// never settle that intent under a different stage/reason envelope.
		return errors.New("daemon_restart input recovery remains pending")
	}
	reason := protocol.TaskResultReasonUnknownOutcome
	summary := "native Goal session was not safely recoverable after daemon restart"
	if session.ResumeTerminalReason == string(protocol.TaskResultReasonResumeRejected) && session.HasKnownRetainedResumeNativeStop() {
		reason = protocol.TaskResultReasonResumeRejected
		summary = "retained pi session was rejected before native work started"
	}
	if err := daemon.queueRecoveryTerminalTransition(rootContext, key, "failed", map[string]string{
		"stage":   "goal_session_recovery",
		"reason":  string(reason),
		"summary": summary,
		"error":   summary,
	}); err != nil {
		return fmt.Errorf("queue unknown native outcome for %s/%d: %w", key.RunID, key.Generation, err)
	}
	if stopErr != nil {
		return errors.Join(errRecoveryRegistrationReady, errNativePhysicalRecoveryPending, stopErr)
	}
	return nil
}

func (daemon *daemon) stopPersistedProcess(ctx context.Context, journal state.RunJournal) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	var authorityCopy *authority.Supervisor
	if journal.ContainmentAuthority != nil {
		cloned := journal.ContainmentAuthority.Clone()
		authorityCopy = &cloned
	}
	stop := recoveredProcessStop{pid: journal.PID, identity: journal.ProcessIdentity, startedAt: journal.StartedAt, authority: authorityCopy}
	current, err := daemon.store.LoadJournal(journal.Key())
	if err != nil {
		if state.IsNotFound(err) {
			daemon.forgetRecoveredProcessStop(journal.Key())
		}
		return err
	}
	if journal.PID <= 0 || strings.TrimSpace(journal.ProcessIdentity) == "" || !stop.matches(current) {
		daemon.forgetRecoveredProcessStop(journal.Key())
		return errPersistedProcessStopUnproven
	}
	daemon.mu.Lock()
	previous, proven := daemon.recoveredStops[journal.Key()]
	if proven && !previous.matches(journal) {
		delete(daemon.recoveredStops, journal.Key())
		proven = false
	}
	daemon.mu.Unlock()
	if proven {
		if authorityCopy != nil && authorityCopy.StopReceipt != nil && authorityCopy.StopReceipt.ValidFor(*authorityCopy) {
			if err := releasePersistedContainmentAuthority(journal.PID, journal.ProcessIdentity, authorityCopy); err != nil {
				return err
			}
		}
		return nil
	}
	if authorityCopy != nil && authorityCopy.StopReceipt != nil && authorityCopy.StopReceipt.ValidFor(*authorityCopy) {
		if err := releasePersistedContainmentAuthority(journal.PID, journal.ProcessIdentity, authorityCopy); err != nil {
			return err
		}
		stop.authority = authorityCopy
		daemon.mu.Lock()
		if daemon.recoveredStops == nil {
			daemon.recoveredStops = make(map[state.RunKey]recoveredProcessStop)
		}
		daemon.recoveredStops[journal.Key()] = stop
		daemon.mu.Unlock()
		return nil
	}
	if authorityCopy != nil {
		if daemon.options.terminatePersistAuthority == nil {
			return errors.New("persisted containment authority termination is unavailable")
		}
		receipt, terminateErr := daemon.options.terminatePersistAuthority(journal.PID, journal.ProcessIdentity, authorityCopy)
		if terminateErr != nil {
			return terminateErr
		}
		if !receipt.ValidFor(*authorityCopy) {
			return fmt.Errorf("%w: containment supervisor returned an invalid stop receipt", errPersistedProcessStopUnproven)
		}
		if persistErr := daemon.persistContainmentStopReceipt(journal.Key(), journal.PID, journal.ProcessIdentity, receipt); persistErr != nil {
			return fmt.Errorf("persist containment stop receipt: %w", persistErr)
		}
		authorityCopy.StopReceipt = &receipt
		if err := releasePersistedContainmentAuthority(journal.PID, journal.ProcessIdentity, authorityCopy); err != nil {
			return err
		}
		stop.authority = authorityCopy
	} else {
		if daemon.options.terminatePersist == nil {
			return errors.New("persisted process termination is unavailable")
		}
		if err := daemon.options.terminatePersist(journal.PID, journal.ProcessIdentity); err != nil {
			return err
		}
	}
	daemon.mu.Lock()
	if daemon.recoveredStops == nil {
		daemon.recoveredStops = make(map[state.RunKey]recoveredProcessStop)
	}
	daemon.recoveredStops[journal.Key()] = stop
	daemon.mu.Unlock()
	return nil
}

func (daemon *daemon) persistContainmentStopReceipt(key state.RunKey, pid int, identity string, receipt authority.StopReceipt) error {
	if daemon.store == nil {
		return errors.New("state store is unavailable")
	}
	if _, err := daemon.store.RecordContainmentStopReceipt(key, pid, identity, receipt); err == nil {
		return nil
	} else {
		journal, readErr := daemon.store.LoadJournal(key)
		if readErr == nil && journal.PID == pid && journal.ProcessIdentity == identity && journal.ContainmentAuthority != nil && journal.ContainmentAuthority.StopReceipt != nil && *journal.ContainmentAuthority.StopReceipt == receipt {
			return nil
		}
		if readErr != nil {
			return errors.Join(err, fmt.Errorf("read back containment stop receipt: %w", readErr))
		}
		return err
	}
}

// persistProcessAuthority treats the journal write as a small CAS protocol.
// A write error after rename is still indeterminate because the directory sync
// may not have completed. Re-submit the same owner binding and require that
// write to succeed; a readback alone cannot turn an indeterminate write into a
// durable acknowledgement. A replacement owner is never overwritten, and a
// matching existing receipt is preserved.
func (daemon *daemon) persistProcessAuthority(key state.RunKey, pid int, identity string, value authority.Supervisor) error {
	write := func() error {
		candidate := value.Clone()
		if daemon.options.recordProcessAuthority != nil {
			_, err := daemon.options.recordProcessAuthority(key, pid, identity, candidate, daemon.now())
			return err
		}
		_, err := daemon.store.SetContainmentAuthority(key, pid, identity, candidate)
		return err
	}
	if err := write(); err == nil {
		return nil
	} else {
		firstErr := err
		if retryErr := write(); retryErr == nil {
			return nil
		} else {
			return errors.Join(firstErr, retryErr)
		}
	}
}

func (daemon *daemon) processAuthorityMatches(key state.RunKey, pid int, identity string, value authority.Supervisor) (bool, error) {
	if daemon.store == nil {
		return false, errors.New("state store is unavailable")
	}
	journal, err := daemon.store.LoadJournal(key)
	if err != nil {
		return false, err
	}
	if journal.PID != pid || journal.ProcessIdentity != identity || journal.ContainmentAuthority == nil {
		return false, nil
	}
	existing := journal.ContainmentAuthority.Clone()
	candidate := value.Clone()
	if existing.Equal(candidate) {
		return true, nil
	}
	existing.StopReceipt = nil
	candidate.StopReceipt = nil
	return existing.Equal(candidate), nil
}

func (daemon *daemon) forgetRecoveredProcessStop(key state.RunKey) {
	daemon.mu.Lock()
	delete(daemon.recoveredStops, key)
	daemon.mu.Unlock()
}

// Recovery retries the journal on the next reconcile pass, not in an unbounded
// live-run persistence loop. The stored terminal state makes replay idempotent.
func (daemon *daemon) queueRecoveryTerminalTransition(ctx context.Context, key state.RunKey, stateName string, payload any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	id, err := daemon.options.newID()
	if err != nil {
		return err
	}
	finishTerminal := daemon.beginTerminal(key)
	persisted := false
	defer func() { finishTerminal(persisted) }()
	transition := protocol.StateTransitionRequest{TransitionID: id, State: stateName, Payload: encoded}
	var journal state.RunJournal
	if daemon.options.queueTerminalTransition != nil {
		journal, err = daemon.options.queueTerminalTransition(key, transition, daemon.now())
	} else {
		journal, err = daemon.store.QueueTerminalTransitionAt(key, transition, daemon.now())
	}
	if err != nil {
		return err
	}
	persisted = true
	daemon.scheduleTerminalSlotRelease(key, journal.TerminalPendingAt)
	if !journal.GoalDeliveryEnabled && journal.HasProcessDetails() {
		daemon.enqueueCleanup(key)
	}
	daemon.signalOutbox()
	return nil
}
