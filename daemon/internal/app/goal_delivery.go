package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

// goalSessionAttachmentReadbackAPI is an optional recovery extension. Older
// ControlAPI implementations continue to fail closed on an ambiguous attach.
type goalSessionAttachmentReadbackAPI interface {
	FetchHarnessSessionAttachment(context.Context, string, protocol.Fence) (control.GoalSessionReceipt, error)
}

type goalDeliveryRejectedError struct{ err error }

func (err *goalDeliveryRejectedError) Error() string { return err.err.Error() }

func (err *goalDeliveryRejectedError) Unwrap() error { return err.err }

func (daemon *daemon) markGoalSessionAttachDeliveryReady(key state.RunKey, localHandleID string) (state.RunJournal, error) {
	mark := daemon.options.markGoalSessionAttachDeliveryReady
	if mark == nil {
		mark = daemon.store.MarkGoalSessionAttachDeliveryReady
	}
	journal, err := mark(key, localHandleID)
	if err == nil {
		return journal, nil
	}

	// A failed directory sync can follow a visible atomic rename. Read back the
	// journal before treating the attachment as unready so we never discard a
	// receipt that is already eligible for exact replay.
	observed, readErr := daemon.store.LoadJournal(key)
	if readErr == nil {
		for _, delivery := range observed.PendingGoalDeliveries {
			if delivery.Kind == state.GoalDeliverySessionAttach && delivery.DeliveryID == localHandleID && delivery.Ready {
				return observed, nil
			}
		}
	}
	if readErr != nil && !state.IsNotFound(readErr) {
		err = errors.Join(err, fmt.Errorf("read Goal session attach delivery after ready failure: %w", readErr))
	}
	return journal, err
}

func (daemon *daemon) discardUnreadyGoalSessionAttachDelivery(key state.RunKey, localHandleID string) error {
	discard := daemon.options.discardUnreadyGoalSessionAttachDelivery
	if discard == nil {
		discard = daemon.store.DiscardUnreadyGoalSessionAttachDelivery
	}
	_, err := discard(key, localHandleID)
	return err
}

// retryUnreadyGoalSessionAttachDeliveries clears only pre-send receipts once
// their starter has finished. It covers a transient failure while compensating
// a rejected Ready barrier without ever promoting an unready receipt.
func (daemon *daemon) retryUnreadyGoalSessionAttachDeliveries() {
	journals, err := daemon.store.ListJournals()
	if err != nil {
		return
	}
	for _, journal := range journals {
		if daemon.isStarting(journal.Key()) {
			continue
		}
		for _, delivery := range journal.PendingGoalDeliveries {
			if delivery.Kind != state.GoalDeliverySessionAttach || delivery.Ready {
				continue
			}
			barrier, barrierErr := daemon.unreadyPiResumeAttachBarrier(journal, delivery)
			if barrierErr != nil {
				if daemon.log != nil {
					daemon.log.Warn("load_unready_pi_resume_barrier_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", barrierErr)
				}
				continue
			}
			if barrier {
				continue
			}
			if err := daemon.discardUnreadyGoalSessionAttachDelivery(journal.Key(), delivery.DeliveryID); err != nil && daemon.log != nil {
				daemon.log.Warn("discard_unready_goal_session_attach_failed", "run_id", journal.RunID, "generation", journal.Generation, "error", err)
			}
		}
	}
}

// unreadyPiResumeAttachBarrier prevents ordinary attach cleanup from deleting
// the target receipt after a rebind may already have committed. The explicit
// durable resume phase is the only proof that this is not an abandoned fresh
// launch intent; recovery will promote it or compensate it in fence order.
func (daemon *daemon) unreadyPiResumeAttachBarrier(journal state.RunJournal, delivery state.GoalDelivery) (bool, error) {
	if delivery.SessionAttach == nil || delivery.SessionAttach.ServerIssuedBinding || delivery.SessionAttach.BindingID == "" {
		return false, nil
	}
	session, err := daemon.store.LoadGoalSessionByHandle(delivery.SessionAttach.LocalHandleID)
	if err != nil {
		if state.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return session.SessionMode == state.GoalSessionModeResume && session.ResumeStartPending &&
		session.RunID == journal.RunID && session.Generation == journal.Generation &&
		session.BindingID == delivery.SessionAttach.BindingID && session.SessionState == state.GoalSessionStateBusy &&
		session.LaunchState == state.GoalSessionLaunchStateAttached && !session.NeedsReconciliation(), nil
}

func (daemon *daemon) closeNativeGoalSession(key state.RunKey, active *runningRun, session harness.Session) error {
	if active == nil || isNilHarnessSession(session) {
		return errors.New("native Goal session is unavailable")
	}
	active.nativeCloseMu.Lock()
	defer active.nativeCloseMu.Unlock()
	daemon.mu.Lock()
	if daemon.running[key] != active || active.nativeSession != session || active.goalSession == nil {
		daemon.mu.Unlock()
		return errors.New("native Goal session key is unavailable")
	}
	goalSession := *active.goalSession
	daemon.mu.Unlock()
	processPID, processIdentity, closeErr := closeNativeSessionWithStopProof(session)
	if closeErr != nil {
		evidenceErr := daemon.publishNativeCloseRetryAfterEvidence(key, active, session, goalSession, "native session close failed: "+closeErr.Error(), true)
		return errors.Join(closeErr, evidenceErr)
	}
	if err := daemon.recordStoppedGoalSession(key, goalSession); err != nil {
		evidenceErr := daemon.publishNativeCloseRetryAfterEvidence(key, active, session, goalSession, "durable native session stop receipt persistence failed after process stop: "+err.Error(), false)
		return errors.Join(fmt.Errorf("record durable native session stop receipt: %w", err), evidenceErr)
	}
	if err := daemon.clearNativeProcessDetailsExact(key, processPID, processIdentity); err != nil {
		evidenceErr := daemon.publishNativeCloseRetryAfterEvidence(key, active, session, goalSession, "native process marker clear failed after close: "+err.Error(), false)
		return errors.Join(err, evidenceErr)
	}
	return nil
}

// recordStoppedGoalSession turns known native termination into either a
// permanent local close (before Control attach) or a durable retained-session
// stop receipt. The latter is deliberately queued before terminal delivery and
// released only by flushGoalDeliveries after Control accepts that transition.
func (daemon *daemon) recordStoppedGoalSession(runKey state.RunKey, goalSession state.GoalSessionKey) error {
	sessionJournal, err := daemon.loadGoalSession(goalSession)
	if err != nil {
		return fmt.Errorf("load durable Goal session after native close: %w", err)
	}
	if sessionJournal.NeedsReconciliation() {
		_, err = daemon.store.ResolveGoalSessionUncertainStopped(goalSession, sessionJournal.Compatibility())
		return err
	}
	if retainedGoalSessionStopCertified(sessionJournal, runKey) {
		return nil
	}
	if sessionJournal.IsLegacyControlAttachment() {
		// Native Close has already succeeded on this path. Old attachment
		// journals lack the immutable receipt required to release Control, so
		// destroy only the local session instead of emitting an unverifiable stop.
		_, err = daemon.closeGoalSession(goalSession)
		return err
	}
	if sessionJournal.ControlSessionID == "" {
		journal, loadErr := daemon.store.LoadJournal(runKey)
		if loadErr != nil {
			return loadErr
		}
		if legacyAttach, ok := legacyGoalSessionAttachDelivery(journal, sessionJournal); ok {
			_, err = daemon.quarantineLegacyGoalSessionAttach(runKey, sessionJournal, legacyAttach)
			return err
		}
		if hasReadyGoalSessionAttach(journal, sessionJournal) {
			// The native process is known stopped, but an attach request may have
			// committed remotely before the daemon persisted its session mapping.
			// Preserve the handle and exact ready request so a later replay can
			// establish the mapping; do not close or manufacture uncertainty.
			_, err = daemon.store.SetGoalSessionState(goalSession, state.GoalSessionStateUnavailable)
			return err
		}
		// No attach receipt was durably correlated or replayable. This session
		// was never safe to retain, so preserve permanent-close behavior.
		_, err = daemon.closeGoalSession(goalSession)
		return err
	}
	if !sessionJournal.HasVerifiedControlAttachment() {
		// A resumed journal keeps the predecessor Control session ID while its
		// new binding waits for attach. That old mapping is not authority to
		// send a stop for the new binding. The caller must preserve the exact
		// ready attach barrier until it can be read back or be quarantined.
		return errGoalAttachmentPending
	}
	// The stop outbox is the durable stopped witness. Write it before changing
	// the local projection so recovery can repair either cross-file window.
	if _, err = daemon.store.QueueGoalSessionStopped(runKey, state.GoalSessionStoppedDelivery{
		SessionID:     sessionJournal.ControlSessionID,
		LocalHandleID: sessionJournal.LocalHandleID,
		BindingID:     sessionJournal.BindingID,
	}); err != nil {
		return err
	}
	if sessionJournal.SessionMode == state.GoalSessionModeResume && sessionJournal.ResumeStartPending {
		_, err = daemon.store.MarkGoalSessionResumeNativeStopped(goalSession)
	} else {
		_, err = daemon.store.MarkGoalSessionStoppedPending(goalSession)
	}
	return err
}

func retainedGoalSessionStopCertified(session state.GoalSessionJournal, key state.RunKey) bool {
	certificate := session.StopCertificate
	return session.SessionState == state.GoalSessionStateAvailable && certificate != nil &&
		certificate.RunID == key.RunID && certificate.Generation == key.Generation &&
		certificate.SessionID == session.ControlSessionID && certificate.LocalHandleID == session.LocalHandleID &&
		certificate.BindingID == session.BindingID
}

func hasReadyGoalSessionAttach(journal state.RunJournal, session state.GoalSessionJournal) bool {
	return hasReplayableGoalSessionAttach(journal, session)
}

func legacyGoalSessionAttachDelivery(journal state.RunJournal, session state.GoalSessionJournal) (state.GoalDelivery, bool) {
	closed := session.SessionState == state.GoalSessionStateClosed && session.LaunchState == state.GoalSessionLaunchStateClosed
	stoppedAwaitingMapping := session.SessionState == state.GoalSessionStateUnavailable && session.LaunchState == state.GoalSessionLaunchStateAttached && !session.NeedsReconciliation() && session.ControlSessionID == "" && session.NativeSessionID != ""
	if !closed && !stoppedAwaitingMapping {
		return state.GoalDelivery{}, false
	}
	for _, delivery := range journal.PendingGoalDeliveries {
		if delivery.Kind == state.GoalDeliverySessionAttach && delivery.Ready && delivery.SessionAttach != nil &&
			delivery.SessionAttach.LocalHandleID == session.LocalHandleID && delivery.SessionAttach.BindingID == session.BindingID &&
			delivery.SessionAttach.BindingID == "" && !delivery.SessionAttach.ServerIssuedBinding {
			return delivery, true
		}
	}
	return state.GoalDelivery{}, false
}

func (daemon *daemon) quarantineLegacyGoalSessionAttach(key state.RunKey, session state.GoalSessionJournal, delivery state.GoalDelivery) (state.RunJournal, error) {
	if session.SessionState != state.GoalSessionStateClosed || session.LaunchState != state.GoalSessionLaunchStateClosed {
		if _, err := daemon.store.SetGoalSessionState(session.Key(), state.GoalSessionStateUnavailable); err != nil {
			return state.RunJournal{}, fmt.Errorf("quarantine unreplayable legacy Goal session: %w", err)
		}
	}
	updated, err := daemon.store.RetireGoalDelivery(key, delivery.Kind, delivery.DeliveryID, delivery.PayloadDigest, http.StatusUnprocessableEntity, "legacy_attachment_unreplayable", "legacy Goal session attach has no binding authority marker", daemon.now())
	if err != nil {
		return state.RunJournal{}, fmt.Errorf("retire unreplayable legacy Goal session attach: %w", err)
	}
	if session.SessionState != state.GoalSessionStateClosed || session.LaunchState != state.GoalSessionLaunchStateClosed {
		if _, err := daemon.store.CloseGoalSession(session.Key()); err != nil {
			return updated, fmt.Errorf("close quarantined legacy Goal session: %w", err)
		}
	}
	return updated, nil
}

func retiredLegacyGoalSessionAttach(journal state.RunJournal, session state.GoalSessionJournal) bool {
	if session.SessionState == state.GoalSessionStateClosed || session.LaunchState != state.GoalSessionLaunchStateAttached || session.NeedsReconciliation() || session.ControlSessionID != "" || session.NativeSessionID == "" {
		return false
	}
	for _, retired := range journal.RetiredGoalDeliveries {
		if retired.Delivery.Kind == state.GoalDeliverySessionAttach && retired.Code == "legacy_attachment_unreplayable" && retired.Delivery.SessionAttach != nil &&
			retired.Delivery.SessionAttach.LocalHandleID == session.LocalHandleID && retired.Delivery.SessionAttach.BindingID == "" && !retired.Delivery.SessionAttach.ServerIssuedBinding {
			return true
		}
	}
	return false
}

// ensureRetainedGoalSessionStopDelivery repairs the cross-file commit window
// around a retained session stop. The outbox is the source of truth: when it
// already exists, it repairs the local unavailable projection; when the
// projection exists without it, it reconstructs the exact receipt.
func (daemon *daemon) ensureRetainedGoalSessionStopDelivery(key state.RunKey, session state.GoalSessionJournal, journal state.RunJournal) (state.RunJournal, error) {
	if !session.HasVerifiedControlAttachment() || session.NeedsReconciliation() {
		return journal, nil
	}
	if journal.RunID == "" {
		loaded, err := daemon.store.LoadJournal(key)
		if err != nil {
			return state.RunJournal{}, err
		}
		journal = loaded
	}
	matchingStop := false
	for _, delivery := range journal.PendingGoalDeliveries {
		if sameRetainedGoalSessionStopDelivery(delivery, session) {
			matchingStop = true
			break
		}
	}
	if !matchingStop {
		for _, delivery := range journal.DeliveredGoalDeliveries {
			if sameRetainedGoalSessionStopDelivery(delivery, session) {
				matchingStop = true
				break
			}
		}
	}
	if !matchingStop {
		for _, retired := range journal.RetiredGoalDeliveries {
			if sameRetainedGoalSessionStopDelivery(retired.Delivery, session) {
				matchingStop = true
				break
			}
		}
	}
	if matchingStop {
		if session.SessionState == state.GoalSessionStateBusy {
			var err error
			if session.SessionMode == state.GoalSessionModeResume && session.ResumeStartPending {
				_, err = daemon.store.MarkGoalSessionResumeNativeStopped(session.Key())
			} else {
				_, err = daemon.store.MarkGoalSessionStoppedPending(session.Key())
			}
			if err != nil {
				return journal, err
			}
		}
		return journal, nil
	}
	if session.SessionState != state.GoalSessionStateUnavailable {
		return journal, nil
	}
	return daemon.store.QueueGoalSessionStopped(key, state.GoalSessionStoppedDelivery{
		SessionID:     session.ControlSessionID,
		LocalHandleID: session.LocalHandleID,
		BindingID:     session.BindingID,
	})
}

func sameRetainedGoalSessionStopDelivery(delivery state.GoalDelivery, session state.GoalSessionJournal) bool {
	return delivery.Kind == state.GoalDeliverySessionStopped && delivery.SessionStopped != nil &&
		delivery.SessionStopped.SessionID == session.ControlSessionID &&
		delivery.SessionStopped.LocalHandleID == session.LocalHandleID &&
		delivery.SessionStopped.BindingID == session.BindingID
}

func retiredRetainedGoalSessionStopDelivery(session state.GoalSessionJournal, journal state.RunJournal) bool {
	if !session.HasVerifiedControlAttachment() || session.SessionState != state.GoalSessionStateUnavailable {
		return false
	}
	for _, retired := range journal.RetiredGoalDeliveries {
		if sameRetainedGoalSessionStopDelivery(retired.Delivery, session) {
			return true
		}
	}
	return false
}

func nativeTerminalPayload(result harness.TaskResult, admission *protocol.Admission, waitErr, closeErr error) (string, map[string]any) {
	summary := strings.TrimSpace(result.Summary)
	if summary == "" {
		summary = "native Goal execution failed"
	}
	payload := map[string]any{"summary": summary}
	if result.Process.OutputTruncated {
		payload["output_truncated"] = true
	}
	if result.Process.SinkError != nil {
		payload["sink_error"] = result.Process.SinkError.Error()
	}
	if result.Process.OutputError != nil {
		payload["output_error"] = result.Process.OutputError.Error()
	}
	if result.Semantic != nil {
		payload["task_result"] = result.Semantic
	}
	if outputErr := nativeProcessOutputFailure(result.Process); outputErr != nil {
		payload["reason"] = string(protocol.TaskResultReasonProcessFailure)
		payload["error"] = outputErr.Error()
		return "failed", payload
	}
	if waitErr != nil {
		payload["reason"] = string(canonicalTaskResultReason(waitErr, &result, protocol.TaskResultReasonUnknownOutcome))
		payload["error"] = waitErr.Error()
		return "failed", payload
	}
	if closeErr != nil {
		payload["reason"] = string(canonicalTaskResultReason(closeErr, &result, protocol.TaskResultReasonUnknownOutcome))
		payload["error"] = closeErr.Error()
		return "failed", payload
	}
	if admission == nil {
		payload["reason"] = string(canonicalTaskResultReason(nil, &result, protocol.TaskResultReasonUnknownOutcome))
		payload["error"] = "native Goal admission is unavailable"
		return "failed", payload
	}
	if result.Semantic == nil {
		switch result.Kind {
		case harness.ResultCancelled:
			payload["reason"] = string(protocol.TaskResultReasonCancelled)
			return "cancelled", payload
		case harness.ResultUnknown:
			payload["reason"] = string(canonicalTaskResultReason(nil, &result, protocol.TaskResultReasonUnknownOutcome))
			payload["error"] = "native turn outcome is unknown"
			return "failed", payload
		case harness.ResultFailed:
			payload["reason"] = string(canonicalTaskResultReason(nil, &result, protocol.TaskResultReasonProcessFailure))
			return "failed", payload
		default:
			payload["reason"] = string(protocol.TaskResultReasonMissingResult)
			payload["error"] = "native turn finished without a canonical task_result"
			return "failed", payload
		}
	}
	if err := result.Semantic.Validate(); err != nil {
		payload["reason"] = string(canonicalTaskResultReason(err, &result, protocol.TaskResultReasonValidationFailed))
		payload["error"] = "invalid native task_result: " + err.Error()
		return "failed", payload
	}
	if err := protocol.ValidateTaskResultForAdmission(*admission, *result.Semantic); err != nil {
		payload["reason"] = string(canonicalTaskResultReason(err, &result, protocol.TaskResultReasonValidationFailed))
		payload["error"] = "native task_result is incompatible with admission: " + err.Error()
		return "failed", payload
	}
	if result.Semantic.Kind != protocol.TaskResultCandidateCompletion && result.Semantic.Subject != admission.Subject {
		payload["reason"] = string(canonicalTaskResultReason(nil, &result, protocol.TaskResultReasonValidationFailed))
		payload["error"] = "non-candidate native task_result Subject does not match the admitted Subject"
		return "failed", payload
	}
	switch result.Kind {
	case harness.ResultSucceeded:
		return "completed", payload
	case harness.ResultCancelled:
		payload["reason"] = string(protocol.TaskResultReasonCancelled)
		return "cancelled", payload
	case harness.ResultFailed:
		payload["reason"] = string(canonicalTaskResultReason(nil, &result, protocol.TaskResultReasonProcessFailure))
		return "failed", payload
	default:
		payload["reason"] = string(protocol.TaskResultReasonUnknownOutcome)
		payload["error"] = "native turn outcome is unknown"
		return "failed", payload
	}
}

// flushGoalDeliveries transmits only caller-supplied typed Goal receipts. It
// never converts raw native events or TaskResult evidence references into
// evidence or usage. A missing receipt leaves the original immutable body in
// the journal for an exact retry after a timeout or daemon restart.
func (daemon *daemon) flushGoalDeliveries(ctx context.Context, journal state.RunJournal) (state.RunJournal, *control.GoalSessionReceipt, error) {
	var attachReceipt *control.GoalSessionReceipt
	for len(journal.PendingGoalDeliveries) > 0 {
		index := -1
		for candidateIndex, candidate := range journal.PendingGoalDeliveries {
			if goalDeliveryReadyForFlush(journal, candidate) {
				index = candidateIndex
				break
			}
		}
		if index < 0 {
			return journal, attachReceipt, nil
		}
		delivery := journal.PendingGoalDeliveries[index]
		updated, receipt, err := daemon.deliverGoalDelivery(ctx, journal, delivery.Kind, delivery.DeliveryID, nil)
		if err != nil {
			var rejected *goalDeliveryRejectedError
			if errors.As(err, &rejected) {
				journal = updated
				continue
			}
			return journal, attachReceipt, err
		}
		journal = updated
		if receipt != nil {
			attachReceipt = receipt
		}
	}
	return journal, attachReceipt, nil
}

func goalDeliveryReadyForFlush(journal state.RunJournal, delivery state.GoalDelivery) bool {
	if !delivery.Ready {
		return false
	}
	if delivery.Kind != state.GoalDeliverySessionStopped {
		return true
	}
	if journal.LocalState == "terminal_pending" {
		return (journal.TerminalVerdict == state.TerminalVerdictAccepted && len(journal.PendingTransitions) == 0) ||
			state.IsConclusiveTerminalVerdict(journal.TerminalVerdict)
	}
	// A lease/reconcile stale transition has no daemon-originated terminal
	// receipt. Control remains the authority and rejects this request until its
	// reaper has made the Run terminal, so an early local attempt cannot release
	// the session.
	return journal.LocalState == "stale" || journal.LocalState == "cleanup_pending"
}

// deliverGoalDelivery sends one exact durable body and deletes it only after
// the control client has verified its typed receipt.
func (daemon *daemon) deliverGoalDelivery(ctx context.Context, journal state.RunJournal, kind state.GoalDeliveryKind, deliveryID string, validateAttach func(control.GoalSessionReceipt) error) (state.RunJournal, *control.GoalSessionReceipt, error) {
	var delivery *state.GoalDelivery
	for index := range journal.PendingGoalDeliveries {
		candidate := &journal.PendingGoalDeliveries[index]
		if candidate.Kind == kind && candidate.DeliveryID == deliveryID {
			delivery = candidate
			break
		}
	}
	if delivery == nil {
		return journal, nil, errors.New("Goal delivery is not pending")
	}
	if kind == state.GoalDeliverySessionAttach && !delivery.Ready {
		return journal, nil, nil
	}
	goalAPI, ok := daemon.control.(goalControlAPI)
	if !ok {
		return journal, nil, errors.New("control API does not implement Goal delivery")
	}
	requestContext, cancel := daemon.controlContext(ctx)
	var attachReceipt *control.GoalSessionReceipt
	var uncertainAttachErr error
	var stoppedReceipt *control.GoalSessionStoppedReceipt
	var evidenceReceipt *control.GoalEvidenceReceipt
	var usageReceipt *control.GoalUsageReceipt
	var err error
	switch kind {
	case state.GoalDeliverySessionAttach:
		payload := delivery.SessionAttach
		if payload == nil {
			cancel()
			return journal, nil, errors.New("Goal session attach payload is missing")
		}
		var bindingID *string
		if strings.TrimSpace(payload.BindingID) != "" {
			bindingID = &payload.BindingID
		} else if !payload.ServerIssuedBinding {
			cancel()
			return journal, nil, errors.New("legacy Goal session attach has no binding authority marker and cannot be replayed")
		}
		// A ready request without a verified current attachment can be the
		// response-loss window, even when resume retains its predecessor's session
		// ID. Read the immutable receipt before replaying its idempotent PUT.
		readbackAPI, readbackSupported := daemon.control.(goalSessionAttachmentReadbackAPI)
		if session, loadErr := daemon.store.LoadGoalSessionByHandle(payload.LocalHandleID); loadErr == nil && !session.HasVerifiedControlAttachment() && readbackSupported {
			if receipt, readErr := readbackAPI.FetchHarnessSessionAttachment(requestContext, journal.RunID, delivery.Fence); readErr == nil {
				attachReceipt = &receipt
			} else if !goalSessionAttachmentAbsent(readErr) {
				err = fmt.Errorf("read Goal session attachment: %w", readErr)
			}
		} else if loadErr != nil {
			err = fmt.Errorf("load local Goal session for attachment readback: %w", loadErr)
		}
		if err == nil && attachReceipt == nil {
			receipt, callErr := goalAPI.AttachHarnessSession(requestContext, journal.RunID, control.GoalSessionAttachRequest{
				Fence:                delivery.Fence,
				LocalHandleID:        payload.LocalHandleID,
				BindingID:            bindingID,
				HarnessKind:          payload.HarnessKind,
				HarnessVersion:       payload.HarnessVersion,
				AdapterVersion:       payload.AdapterVersion,
				WorkspaceFingerprint: payload.WorkspaceFingerprint,
				Workspace:            payload.Workspace,
				RepositoryResourceID: payload.RepositoryResourceID,
			})
			err = callErr
			if err == nil {
				attachReceipt = &receipt
			} else if goalSessionAttachmentIdempotencyConflict(err) && readbackSupported {
				// A conflicting replay may be the exact attachment whose original
				// response was lost. Only the fenced immutable readback may resolve it.
				if receipt, readErr := readbackAPI.FetchHarnessSessionAttachment(requestContext, journal.RunID, delivery.Fence); readErr == nil {
					attachReceipt = &receipt
					err = nil
				} else {
					err = fmt.Errorf("read Goal session attachment after idempotency conflict: %w", readErr)
				}
			} else if readbackSupported && goalSessionAttachmentTransportUnknown(requestContext, err) {
				// The PUT may have committed before its transport acknowledgement
				// was lost. Resolve it once with the original fence and live request
				// budget; a historical receipt never grants new execution authority.
				if receipt, readErr := readbackAPI.FetchHarnessSessionAttachment(requestContext, journal.RunID, delivery.Fence); readErr == nil {
					attachReceipt = &receipt
					uncertainAttachErr = err
					err = nil
				} else {
					err = goalSessionAttachmentUncertainReadbackError(err, readErr)
				}
			}
		}
	case state.GoalDeliverySessionStopped:
		payload := delivery.SessionStopped
		if payload == nil {
			cancel()
			return journal, nil, errors.New("Goal session stopped payload is missing")
		}
		stopAPI, ok := daemon.control.(goalSessionStopAPI)
		if !ok {
			cancel()
			return journal, nil, errors.New("control API does not implement Goal session stop delivery")
		}
		receipt, callErr := stopAPI.MarkHarnessSessionStopped(requestContext, journal.RunID, control.GoalSessionStoppedRequest{
			Fence:         delivery.Fence,
			SessionID:     payload.SessionID,
			LocalHandleID: payload.LocalHandleID,
			BindingID:     payload.BindingID,
		})
		err = callErr
		if err == nil {
			stoppedReceipt = &receipt
		}
	case state.GoalDeliveryEvidence:
		if delivery.Evidence == nil {
			cancel()
			return journal, nil, errors.New("Goal evidence payload is missing")
		}
		receipt, callErr := goalAPI.AppendEvidence(requestContext, journal.RunID, delivery.Fence, *delivery.Evidence)
		err = callErr
		if err == nil {
			evidenceReceipt = &receipt
		}
	case state.GoalDeliveryUsage:
		if delivery.Usage == nil {
			cancel()
			return journal, nil, errors.New("Goal usage payload is missing")
		}
		receipt, callErr := goalAPI.RecordUsage(requestContext, journal.RunID, delivery.Fence, *delivery.Usage)
		err = callErr
		if err == nil {
			usageReceipt = &receipt
		}
	default:
		cancel()
		return journal, nil, errors.New("Goal delivery kind is invalid")
	}
	cancel()
	if err != nil {
		if statusCode, code, message, definitive := definitiveGoalDeliveryRejection(delivery.Kind, err); definitive {
			updated, retireErr := daemon.store.RetireGoalDelivery(journal.Key(), delivery.Kind, delivery.DeliveryID, delivery.PayloadDigest, statusCode, code, message, daemon.now())
			if retireErr != nil {
				return journal, nil, retireErr
			}
			if delivery.Kind == state.GoalDeliverySessionStopped {
				session, loadErr := daemon.store.LoadGoalSessionByHandle(delivery.SessionStopped.LocalHandleID)
				if loadErr != nil {
					return updated, nil, fmt.Errorf("load local Goal session after definitive stop rejection: %w", loadErr)
				}
				if _, closeErr := daemon.store.CloseGoalSession(session.Key()); closeErr != nil {
					return updated, nil, fmt.Errorf("close local Goal session after definitive stop rejection: %w", closeErr)
				}
			}
			return updated, nil, &goalDeliveryRejectedError{err: err}
		}
		return journal, nil, err
	}
	if err := validateGoalDeliveryReceipt(journal, *delivery, attachReceipt, stoppedReceipt, evidenceReceipt, usageReceipt); err != nil {
		return journal, nil, goalSessionAttachmentUncertainReadbackError(uncertainAttachErr, err)
	}
	if attachReceipt != nil && validateAttach != nil {
		if err := validateAttach(*attachReceipt); err != nil {
			return journal, nil, goalSessionAttachmentUncertainReadbackError(uncertainAttachErr, err)
		}
	}
	if attachReceipt != nil {
		session, loadErr := daemon.store.LoadGoalSessionByHandle(delivery.SessionAttach.LocalHandleID)
		if loadErr != nil {
			return journal, nil, fmt.Errorf("load local Goal session for attach receipt: %w", loadErr)
		}
		if attachReceipt.TaskID != session.TaskID || attachReceipt.MachineID != session.MachineID {
			return journal, nil, goalSessionAttachmentUncertainReadbackError(uncertainAttachErr, errors.New("Goal session attach receipt does not match local task or machine identity"))
		}
		persisted, persistErr := daemon.store.PersistGoalSessionControlAttachment(session.Key(), attachReceipt.ID, attachReceipt.BindingID, attachReceipt.AttachmentReceiptID, delivery.SessionAttach.ServerIssuedBinding)
		if persistErr != nil {
			if errors.Is(persistErr, state.ErrGoalSessionConflict) {
				return journal, nil, goalSessionAttachmentUncertainReadbackError(uncertainAttachErr, fmt.Errorf("persist Control Goal session attachment: %w", persistErr))
			}
			return journal, nil, fmt.Errorf("persist Control Goal session attachment: %w", persistErr)
		}
		if persisted.SessionState == state.GoalSessionStateUnavailable {
			if _, queueErr := daemon.store.QueueGoalSessionStopped(journal.Key(), state.GoalSessionStoppedDelivery{
				SessionID:     persisted.ControlSessionID,
				LocalHandleID: persisted.LocalHandleID,
				BindingID:     persisted.BindingID,
			}); queueErr != nil {
				return journal, nil, fmt.Errorf("queue retained Goal session stop after attach replay: %w", queueErr)
			}
		}
	}
	if stoppedReceipt != nil {
		session, loadErr := daemon.store.LoadGoalSessionByHandle(delivery.SessionStopped.LocalHandleID)
		if loadErr != nil {
			return journal, nil, fmt.Errorf("load local Goal session for stop receipt: %w", loadErr)
		}
		certificate := state.GoalSessionStopCertificate{
			RunID:          journal.RunID,
			Generation:     journal.Generation,
			SessionID:      stoppedReceipt.SessionID,
			LocalHandleID:  stoppedReceipt.LocalHandleID,
			BindingID:      stoppedReceipt.BindingID,
			DeliveryDigest: delivery.PayloadDigest,
			ReceiptID:      stoppedReceipt.ReceiptID,
		}
		if _, persistErr := daemon.store.MarkGoalSessionAvailable(session.Key(), certificate); persistErr != nil {
			return journal, nil, fmt.Errorf("persist released Goal session: %w", persistErr)
		}
	}
	updated, err := daemon.store.MarkGoalDeliveryDelivered(journal.Key(), delivery.Kind, delivery.DeliveryID, delivery.PayloadDigest)
	if err != nil {
		return journal, nil, err
	}
	return updated, attachReceipt, nil
}

func definitiveGoalDeliveryRejection(kind state.GoalDeliveryKind, err error) (int, string, string, bool) {
	var apiError *control.APIError
	if !errors.As(err, &apiError) {
		return 0, "", "", false
	}
	switch apiError.StatusCode {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusForbidden, http.StatusUnprocessableEntity:
		message := apiError.Message
		if message == "" {
			message = apiError.Error()
		}
		return apiError.StatusCode, string(apiError.Code), message, true
	case http.StatusConflict:
		if kind == state.GoalDeliverySessionAttach && apiError.Code == control.IdempotencyConflict {
			// Attach idempotency conflict can mean the server committed the same
			// attachment before a response was lost. Keep the exact request until
			// the attachment receipt can be read back; never retire this barrier.
			return 0, "", "", false
		}
		// A usage-key conflict could be an earlier accepted accounting record or
		// a changed body. Without a readback receipt it is an unknown external
		// effect, except for the control plane's explicit idempotency conflict.
		if kind == state.GoalDeliveryUsage && apiError.Code != control.IdempotencyConflict {
			return 0, "", "", false
		}
		if kind == state.GoalDeliverySessionStopped && apiError.Code == control.StateConflict {
			// The daemon can observe lease expiry before Control's reaper commits
			// the terminal Run. Keep this exact receipt retryable.
			return 0, "", "", false
		}
		message := apiError.Message
		if message == "" {
			message = apiError.Error()
		}
		return apiError.StatusCode, string(apiError.Code), message, true
	default:
		return 0, "", "", false
	}
}

func goalSessionAttachmentAbsent(err error) bool {
	var apiError *control.APIError
	return errors.As(err, &apiError) && apiError.StatusCode == http.StatusNotFound
}

func goalSessionAttachmentIdempotencyConflict(err error) bool {
	var apiError *control.APIError
	return errors.As(err, &apiError) && apiError.StatusCode == http.StatusConflict && apiError.Code == control.IdempotencyConflict
}

func goalSessionAttachmentTransportUnknown(ctx context.Context, err error) bool {
	// RetryDelay also admits 429/5xx, which are definite HTTP responses rather
	// than lost acknowledgements. A permanent response can wrap EOF as well.
	var apiError *control.APIError
	var responseError *control.ResponseError
	if errors.As(err, &apiError) || errors.As(err, &responseError) {
		return false
	}
	if (ctx != nil && ctx.Err() != nil) || errors.Is(err, context.Canceled) {
		return false
	}
	// http.Client can wrap a RoundTripper's url.Error in another url.Error.
	// Every such layer implements net.Error even for a permanent local cause;
	// strip them all before reusing Control's underlying transport-cause set.
	var requestError *url.Error
	for errors.As(err, &requestError) {
		err = requestError.Err
	}
	_, transportUnknown := control.RetryDelay(ctx, err, 0)
	return transportUnknown
}

func goalSessionAttachmentUncertainReadbackError(attachErr, readErr error) error {
	if attachErr == nil {
		return readErr
	}
	var apiError *control.APIError
	if errors.As(readErr, &apiError) && (apiError.StatusCode == http.StatusUnauthorized || apiError.StatusCode == http.StatusForbidden || control.IsOwnershipLost(readErr)) {
		return readErr
	}
	// Absence, another lost response or an invalid identity cannot disprove the
	// original PUT commit. Keep its transport classification and exact outbox;
	// in particular, do not retire it as a permanent GET 404 rejection.
	return fmt.Errorf("read Goal session attachment after uncertain transport (%v): %w", readErr, attachErr)
}

// flushLateGoalUsage drains the retained accounting ledger independently of
// the run lifecycle. It never renews a lease, recreates a run, or starts work.
func (daemon *daemon) flushLateGoalUsage(ctx context.Context) {
	ledgers, err := daemon.store.ListLateGoalUsage()
	if err != nil {
		return
	}
	goalAPI, ok := daemon.control.(goalControlAPI)
	if !ok {
		return
	}
	for _, ledger := range ledgers {
		for len(ledger.PendingUsageDeliveries) > 0 {
			delivery := ledger.PendingUsageDeliveries[0]
			if delivery.Usage == nil {
				return
			}
			requestContext, cancel := daemon.controlContext(ctx)
			receipt, callErr := goalAPI.RecordUsage(requestContext, ledger.RunID, ledger.Fence, *delivery.Usage)
			cancel()
			if callErr != nil {
				if statusCode, code, message, definitive := definitiveGoalDeliveryRejection(state.GoalDeliveryUsage, callErr); definitive {
					updated, retireErr := daemon.store.RetireLateGoalUsage(state.RunKey{RunID: ledger.RunID, Generation: ledger.Generation}, delivery.DeliveryID, delivery.PayloadDigest, statusCode, code, message, daemon.now())
					if retireErr == nil {
						ledger = updated
						continue
					}
				}
				break
			}
			if err := validateGoalDeliveryReceipt(state.RunJournal{RunID: ledger.RunID}, delivery, nil, nil, nil, &receipt); err != nil {
				break
			}
			updated, err := daemon.store.MarkLateGoalUsageDelivered(state.RunKey{RunID: ledger.RunID, Generation: ledger.Generation}, delivery.DeliveryID, delivery.PayloadDigest)
			if err != nil {
				break
			}
			ledger = updated
		}
	}
}

// validateGoalDeliveryReceipt is a second deletion barrier. The production
// client performs strict wire validation, but the daemon must still protect its
// durable outbox if a different ControlAPI implementation returns a malformed
// or mismatched typed receipt.
func validateGoalDeliveryReceipt(journal state.RunJournal, delivery state.GoalDelivery, attach *control.GoalSessionReceipt, stopped *control.GoalSessionStoppedReceipt, evidence *control.GoalEvidenceReceipt, usage *control.GoalUsageReceipt) error {
	switch delivery.Kind {
	case state.GoalDeliverySessionAttach:
		if delivery.SessionAttach == nil || attach == nil {
			return errors.New("Goal session attach receipt is missing")
		}
		payload := delivery.SessionAttach
		if attach.ID == "" || attach.AttachmentReceiptID == "" || attach.GoalID != payload.GoalID || attach.RunID != journal.RunID || attach.RuntimeID != delivery.Fence.RuntimeID ||
			attach.ActiveRunID != journal.RunID || attach.LocalHandleID != payload.LocalHandleID || attach.HarnessKind != payload.HarnessKind ||
			attach.HarnessVersion != payload.HarnessVersion || attach.AdapterVersion != payload.AdapterVersion || attach.WorkspaceFingerprint != payload.WorkspaceFingerprint || attach.State != state.GoalSessionStateBusy {
			return errors.New("Goal session attach receipt does not match durable request")
		}
		if (payload.BindingID != "" && attach.BindingID != payload.BindingID) || (payload.BindingID == "" && !payload.ServerIssuedBinding) {
			return errors.New("Goal session attach receipt binding does not match durable request")
		}
		if attach.Workspace != "" && attach.Workspace != payload.Workspace {
			return errors.New("Goal session attach receipt workspace does not match durable request")
		}
		if payload.RepositoryResourceID != nil && attach.RepositoryResourceID != *payload.RepositoryResourceID {
			return errors.New("Goal session attach receipt repository does not match durable request")
		}
	case state.GoalDeliverySessionStopped:
		if delivery.SessionStopped == nil || stopped == nil {
			return errors.New("Goal session stopped receipt is missing")
		}
		payload := delivery.SessionStopped
		if stopped.ReceiptID == "" || stopped.RunID != journal.RunID || stopped.SessionID != payload.SessionID || stopped.LocalHandleID != payload.LocalHandleID || stopped.BindingID != payload.BindingID || stopped.State != state.GoalSessionStateAvailable || stopped.ActiveRunID != nil || stopped.LockVersion <= 0 {
			return errors.New("Goal session stopped receipt does not match durable request")
		}
	case state.GoalDeliveryEvidence:
		if delivery.Evidence == nil || evidence == nil || evidence.ID != delivery.Evidence.EvidenceID || evidence.RunID != journal.RunID || evidence.EvidenceKey != delivery.Evidence.EvidenceKey || evidence.Kind != string(delivery.Evidence.Kind) || evidence.SubjectHash != delivery.Evidence.SubjectHash || evidence.Verdict != string(delivery.Evidence.Verdict) {
			return errors.New("Goal evidence receipt does not match durable request")
		}
	case state.GoalDeliveryUsage:
		if delivery.Usage == nil || usage == nil || usage.ID != delivery.Usage.UsageID || usage.RunID != journal.RunID || usage.UsageKey != delivery.Usage.UsageKey || usage.CostBasis != string(delivery.Usage.CostBasis) || !sameOptionalString(usage.CostMicrousd, delivery.Usage.CostMicrousd) {
			return errors.New("Goal usage receipt does not match durable request")
		}
	default:
		return errors.New("Goal delivery kind is invalid")
	}
	return nil
}

func sameOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
