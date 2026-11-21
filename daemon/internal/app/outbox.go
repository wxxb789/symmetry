package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

// Preserves the first-terminal-wins barrier for a different terminal queued after another became
// durable; unwraps to errAuthoritativeTerminal so cancellation can enqueue a rejected ack.
type terminalTransitionConflictError struct {
	expectedState string
	actualState   string
}

func (err *terminalTransitionConflictError) Error() string {
	if err.actualState == "" {
		return fmt.Sprintf("%s: terminal transition %q is already durable", errAuthoritativeTerminal, err.expectedState)
	}
	return fmt.Sprintf("%s: terminal transition %q conflicts with durable %q", errAuthoritativeTerminal, err.expectedState, err.actualState)
}

func (err *terminalTransitionConflictError) Unwrap() error { return errAuthoritativeTerminal }

// restartOutboxRecoveryContextKey suppresses ordinary ownership-loss cleanup
// until restart recovery has converted the journal into a terminal fallback.
type restartOutboxRecoveryContextKey struct{}

type outboxRetry struct {
	fingerprint string
	retryAt     time.Time
	delay       time.Duration
	permanent   bool
}

type outboxFailure struct {
	journal state.RunJournal
	err     error
}

func (failure *outboxFailure) Error() string { return failure.err.Error() }

func (failure *outboxFailure) Unwrap() error { return failure.err }

func (daemon *daemon) runOutbox(ctx context.Context) {
	for {
		daemon.flushAll(ctx)
		timer := daemon.timer(minimumInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-daemon.outboxSignal():
			timer.Stop()
		case <-timer.Chan():
		}
	}
}

func (daemon *daemon) outboxSignal() <-chan struct{} {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if daemon.outboxWake == nil {
		return nil
	}
	return daemon.outboxWake
}

func (daemon *daemon) signalOutbox() {
	daemon.mu.Lock()
	wake := daemon.outboxWake
	daemon.mu.Unlock()
	if wake == nil {
		return
	}
	select {
	case wake <- struct{}{}:
	default:
	}
}

func (daemon *daemon) signalOutboxFor(key state.RunKey) {
	daemon.mu.Lock()
	active := daemon.running[key]
	starting := active != nil && active.starting
	daemon.mu.Unlock()
	if !starting {
		daemon.signalOutbox()
	}
}

func (daemon *daemon) queueTerminalTransition(key state.RunKey, stateName string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	id, err := daemon.options.newID()
	if err != nil {
		return err
	}
	enteredAt := daemon.now()
	finishTerminal := daemon.beginTerminal(key)
	journal, err := daemon.store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: id, State: stateName, Payload: encoded}, enteredAt)
	finishTerminal(err == nil)
	if err != nil {
		return err
	}
	daemon.scheduleTerminalSlotRelease(key, journal.TerminalPendingAt)
	daemon.signalOutbox()
	return nil
}

func (daemon *daemon) queueTerminalTransitionWithRetry(ctx context.Context, key state.RunKey, stateName string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	rootContext := daemon.rootContext(ctx)
	id, err := daemon.newIDWithRetry(rootContext, key, "", "terminal transition")
	if err != nil {
		return err
	}
	enteredAt := daemon.now()
	finishTerminal := daemon.beginTerminal(key)
	persisted := false
	defer func() { finishTerminal(persisted) }()
	for {
		transition := protocol.StateTransitionRequest{TransitionID: id, State: stateName, Payload: encoded}
		var (
			journal  state.RunJournal
			queueErr error
		)
		if daemon.options.queueTerminalTransition != nil {
			journal, queueErr = daemon.options.queueTerminalTransition(key, transition, enteredAt)
		} else {
			journal, queueErr = daemon.store.QueueTerminalTransitionAt(key, transition, enteredAt)
		}
		if queueErr == nil {
			persisted = true
			daemon.scheduleTerminalSlotRelease(key, journal.TerminalPendingAt)
			daemon.signalOutbox()
			return nil
		}
		if existing, loadErr := daemon.store.LoadJournal(key); loadErr == nil {
			if matches, present, actualState := terminalTransitionMatches(existing, transition); matches {
				persisted = true
				daemon.scheduleTerminalSlotRelease(key, existing.TerminalPendingAt)
				daemon.signalOutbox()
				return nil
			} else if present {
				return &terminalTransitionConflictError{expectedState: stateName, actualState: actualState}
			}
		}
		if daemon.log != nil {
			daemon.log.Warn("queue_terminal_transition_failed", "run_id", key.RunID, "generation", key.Generation, "state", stateName, "error", queueErr)
		}
		timer := daemon.timer(minimumInterval)
		select {
		case <-rootContext.Done():
			timer.Stop()
			return rootContext.Err()
		case <-timer.Chan():
		}
	}
}

func (daemon *daemon) queueCancelledTerminalAndAcknowledgement(key state.RunKey, commandID string) error {
	return daemon.queueCancelledTerminalAndAcknowledgementWithContext(context.Background(), key, commandID)
}

func (daemon *daemon) queueCancelledTerminalAndAcknowledgementWithContext(ctx context.Context, key state.RunKey, commandID string) error {
	return daemon.queueTerminalTransitionAndAcknowledgementWithContext(ctx, key, "cancelled", map[string]any{}, commandID, "applied")
}

func (daemon *daemon) queueTerminalTransitionAndAcknowledgementWithContext(ctx context.Context, key state.RunKey, stateName string, payload any, commandID, outcome string) error {
	rootContext := daemon.rootContext(ctx)
	if daemon.commandAcknowledgementRetired(key) {
		return errors.New("command acknowledgement is no longer deliverable")
	}
	transitionID, err := daemon.newIDWithRetry(rootContext, key, commandID, "transition")
	if err != nil {
		return err
	}
	acknowledgementID, err := daemon.newIDWithRetry(rootContext, key, commandID, "acknowledgement")
	if err != nil {
		return err
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	enteredAt := daemon.now()
	finishTerminal := daemon.beginTerminal(key)
	persisted := false
	defer func() { finishTerminal(persisted) }()
	transition := protocol.StateTransitionRequest{TransitionID: transitionID, State: stateName, Payload: encoded}
	acknowledgement := protocol.CommandAcknowledgement{RunID: key.RunID, CommandID: commandID, Outcome: outcome, AckID: acknowledgementID}
	for {
		// Queue/readback and acknowledgement removal share one lock order: hold commandReceiptMu
		// across one persistence attempt only, so a stalled writer cannot block unrelated delivery.
		daemon.commandReceiptMu.Lock()
		var journal state.RunJournal
		if daemon.options.queueTerminalTransitionAndAcknowledgement != nil {
			journal, err = daemon.options.queueTerminalTransitionAndAcknowledgement(key, transition, acknowledgement, enteredAt)
		} else if stateName == "cancelled" && outcome == "applied" && daemon.options.queueCancelledTransitionAndAcknowledgement != nil {
			journal, err = daemon.options.queueCancelledTransitionAndAcknowledgement(key, transition, acknowledgement, enteredAt)
		} else {
			journal, err = daemon.store.QueueTerminalTransitionAndAcknowledgementAt(key, transition, acknowledgement, enteredAt)
		}
		if err == nil {
			daemon.commandReceiptMu.Unlock()
			persisted = true
			daemon.scheduleTerminalSlotRelease(key, journal.TerminalPendingAt)
			daemon.signalOutbox()
			return nil
		}
		if existing, loadErr := daemon.store.LoadJournal(key); loadErr == nil {
			if matches, conflict := terminalReceiptMatches(existing, transition, acknowledgement); matches {
				daemon.commandReceiptMu.Unlock()
				persisted = true
				daemon.scheduleTerminalSlotRelease(key, existing.TerminalPendingAt)
				daemon.signalOutbox()
				return nil
			} else if conflict {
				daemon.commandReceiptMu.Unlock()
				return &terminalTransitionConflictError{expectedState: stateName, actualState: existing.TerminalState}
			}
		}
		daemon.commandReceiptMu.Unlock()
		if daemon.commandAcknowledgementRetired(key) {
			return errors.New("command acknowledgement is no longer deliverable")
		}
		if daemon.log != nil {
			daemon.log.Warn("queue_terminal_transition_and_acknowledgement_failed", "run_id", key.RunID, "generation", key.Generation, "state", stateName, "command_id", commandID, "outcome", outcome, "error", err)
		}
		timer := daemon.timer(minimumInterval)
		select {
		case <-rootContext.Done():
			timer.Stop()
			return rootContext.Err()
		case <-timer.Chan():
		}
	}
}

func (daemon *daemon) newIDWithRetry(ctx context.Context, key state.RunKey, commandID, kind string) (string, error) {
	for {
		id, err := daemon.options.newID()
		if err == nil {
			return id, nil
		}
		if daemon.log != nil {
			daemon.log.Warn("generate_cancelled_receipt_id_failed", "run_id", key.RunID, "generation", key.Generation, "command_id", commandID, "kind", kind, "error", err)
		}
		timer := daemon.timer(minimumInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return "", ctx.Err()
		case <-timer.Chan():
		}
	}
}

func (daemon *daemon) flushAll(ctx context.Context) {
	daemon.retryBlockedNativeSessionCloses()
	daemon.retryUnreadyGoalSessionAttachDeliveries()
	daemon.flushPendingNativeUsage(ctx)
	journals, err := daemon.store.ListJournals()
	if err != nil {
		return
	}
	seen := make(map[state.RunKey]struct{}, len(journals))
	for _, journal := range journals {
		key := journal.Key()
		if !journal.GoalDeliveryEnabled && recoveredGenericProcessMarker(journal) {
			daemon.enqueueCleanup(key)
		}
		for {
			if ctx.Err() != nil {
				return
			}
			terminal := journal.LocalState == "terminal_pending" ||
				(journal.LocalState == "cleanup_pending" && journal.HasPendingGoalDeliveries()) ||
				(journal.LocalState == "stale" && hasPendingStaleOutbox(journal))
			if journal.LocalState == "cleanup_pending" && !journal.HasPendingGoalDeliveries() {
				_ = daemon.scheduleCleanup(ctx, journal)
				break
			}
			if terminal {
				seen[key] = struct{}{}
				if !daemon.outboxDue(journal) {
					break
				}
			}
			err := daemon.flushRun(ctx, journal)
			current, loadErr := daemon.store.LoadJournal(key)
			if state.IsNotFound(loadErr) {
				daemon.clearOutboxRetry(key)
				break
			}
			if loadErr != nil {
				if err != nil && terminal {
					daemon.recordOutboxFailure(ctx, journal, err)
				}
				break
			}
			if current.LocalState == "terminal_pending" ||
				(current.LocalState == "cleanup_pending" && current.HasPendingGoalDeliveries()) ||
				(current.LocalState == "stale" && hasPendingStaleOutbox(current)) {
				seen[key] = struct{}{}
			}
			if err == nil {
				daemon.clearOutboxRetry(key)
				break
			}

			failedJournal := journal
			var failure *outboxFailure
			if errors.As(err, &failure) {
				failedJournal = failure.journal
			}
			if errors.Is(err, errOutboxChanged) || journalFingerprint(current) != journalFingerprint(failedJournal) {
				daemon.clearOutboxRetry(key)
				journal = current
				continue
			}
			if current.LocalState == "terminal_pending" ||
				(current.LocalState == "cleanup_pending" && current.HasPendingGoalDeliveries()) ||
				(current.LocalState == "stale" && hasPendingStaleOutbox(current)) {
				daemon.recordOutboxFailure(ctx, current, err)
			}
			break
		}
	}
	daemon.flushLateGoalUsage(ctx)
	daemon.pruneOutboxRetries(seen)
}

func hasPendingStaleOutbox(journal state.RunJournal) bool {
	return journal.HasPendingGoalDeliveries() || len(journal.PendingCommandAcknowledgements) != 0
}

func (daemon *daemon) outboxDue(journal state.RunJournal) bool {
	key := journal.Key()
	fingerprint := journalFingerprint(journal)
	now := daemon.now()
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	if daemon.outboxRetry == nil {
		daemon.outboxRetry = make(map[state.RunKey]outboxRetry)
	}
	entry, exists := daemon.outboxRetry[key]
	if !exists || entry.fingerprint != fingerprint {
		daemon.outboxRetry[key] = outboxRetry{fingerprint: fingerprint, delay: minimumInterval}
		return true
	}
	return !entry.permanent && !now.Before(entry.retryAt)
}

func (daemon *daemon) recordOutboxFailure(ctx context.Context, journal state.RunJournal, err error) {
	if ctx.Err() != nil {
		return
	}
	key := journal.Key()
	fingerprint := journalFingerprint(journal)
	daemon.mu.Lock()
	entry, exists := daemon.outboxRetry[key]
	if daemon.outboxRetry == nil {
		daemon.outboxRetry = make(map[state.RunKey]outboxRetry)
	}
	if !exists || entry.fingerprint != fingerprint {
		entry = outboxRetry{fingerprint: fingerprint, delay: minimumInterval}
		daemon.outboxRetry[key] = entry
	}
	fallback := entry.delay
	if fallback <= 0 {
		fallback = minimumInterval
	}
	daemon.mu.Unlock()

	delay, retryable := control.RetryDelay(ctx, err, fallback)
	if !retryable && isAmbiguousTerminalDeliveryFailure(journal, err) {
		delay, retryable = fallback, true
	}
	if !retryable && journal.HasPendingGoalDeliveries() {
		// Goal receipts have their own durable idempotency and must continue independently of a
		// conclusive terminal verdict: never convert a pending delivery into a permanent backoff.
		delay, retryable = fallback, true
	}
	if !retryable && journal.LocalState == "stale" && len(journal.PendingCommandAcknowledgements) != 0 {
		// A stale run cannot recreate an acknowledgement after cleanup. Keep the
		// exact fenced body retryable until delivery or an explicit retirement.
		delay, retryable = fallback, true
	}
	if !retryable && isOutboxMaintenanceFailure(journal, err) {
		delay, retryable = fallback, true
	}
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	current, exists := daemon.outboxRetry[key]
	if exists && current.fingerprint != fingerprint {
		return
	}
	if !retryable {
		daemon.outboxRetry[key] = outboxRetry{fingerprint: fingerprint, delay: fallback, permanent: true}
		return
	}
	if journal.LocalState == "terminal_pending" && delay < minimumInterval {
		delay = minimumInterval
	}
	now := daemon.now()
	retryAt := now.Add(delay)
	if journal.TerminalVerdict == "" && !journal.LeaseExpiresAt.IsZero() {
		deliveryDeadline := journal.LeaseExpiresAt.Add(terminalGrace)
		if now.Before(deliveryDeadline) && retryAt.After(deliveryDeadline) {
			retryAt = deliveryDeadline
		}
	}
	daemon.outboxRetry[key] = outboxRetry{fingerprint: fingerprint, retryAt: retryAt, delay: nextRetryDelay(fallback)}
}

func isAmbiguousTerminalDeliveryFailure(journal state.RunJournal, err error) bool {
	if journal.LocalState != "terminal_pending" && journal.LocalState != "cleanup_pending" {
		return false
	}
	var responseError *control.ResponseError
	return errors.As(err, &responseError)
}

func isOutboxMaintenanceFailure(journal state.RunJournal, err error) bool {
	var apiError *control.APIError
	var responseError *control.ResponseError
	if errors.As(err, &apiError) || errors.As(err, &responseError) {
		return false
	}
	return journal.LocalState == "terminal_pending" && journal.TerminalVerdict == state.TerminalVerdictAccepted && len(journal.PendingTransitions) == 0
}

func (daemon *daemon) clearOutboxRetry(key state.RunKey) {
	daemon.mu.Lock()
	delete(daemon.outboxRetry, key)
	daemon.mu.Unlock()
}

func (daemon *daemon) pruneOutboxRetries(seen map[state.RunKey]struct{}) {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	for key := range daemon.outboxRetry {
		if _, exists := seen[key]; !exists {
			delete(daemon.outboxRetry, key)
		}
	}
}

func journalFingerprint(journal state.RunJournal) string {
	eventIDs := make([]string, len(journal.PendingEvents))
	for index, event := range journal.PendingEvents {
		eventIDs[index] = event.EventID
	}
	transitionIDs := make([]string, len(journal.PendingTransitions))
	for index, transition := range journal.PendingTransitions {
		transitionIDs[index] = transition.TransitionID
	}
	acknowledgementIDs := make([]string, len(journal.PendingCommandAcknowledgements))
	for index, acknowledgement := range journal.PendingCommandAcknowledgements {
		acknowledgementIDs[index] = acknowledgement.AckID
	}
	goalDeliveryIDs := make([]string, len(journal.PendingGoalDeliveries))
	for index, delivery := range journal.PendingGoalDeliveries {
		goalDeliveryIDs[index] = string(delivery.Kind) + ":" + delivery.DeliveryID + ":" + delivery.PayloadDigest + ":" + strconv.FormatBool(delivery.Ready)
	}
	deliveredGoalDeliveryIDs := make([]string, len(journal.DeliveredGoalDeliveries))
	for index, delivery := range journal.DeliveredGoalDeliveries {
		deliveredGoalDeliveryIDs[index] = string(delivery.Kind) + ":" + delivery.DeliveryID + ":" + delivery.PayloadDigest
	}
	value := struct {
		Key                       state.RunKey
		Fence                     protocol.Fence
		LocalState                string
		LeaseExpiresAt            time.Time
		TerminalState             string
		TerminalPendingAt         time.Time
		TerminalVerdict           string
		TerminalIntentDigest      string
		TerminalCommandDigest     string
		TerminalSettlementDigest  string
		PendingEventIDs           []string
		PendingTransitionIDs      []string
		AttemptedTransitionIDs    []string
		PendingAcknowledgementIDs []string
		PendingGoalDeliveryIDs    []string
		DeliveredGoalDeliveryIDs  []string
		NativeUsageObservation    *state.NativeUsageObservation
		NativeUsageRecovery       bool
		NativeTerminalRecovery    *state.NativeUsageTerminalRecovery
	}{
		Key:                       journal.Key(),
		Fence:                     journal.Fence(),
		LocalState:                journal.LocalState,
		LeaseExpiresAt:            journal.LeaseExpiresAt,
		TerminalState:             journal.TerminalState,
		TerminalPendingAt:         journal.TerminalPendingAt,
		TerminalVerdict:           journal.TerminalVerdict,
		TerminalIntentDigest:      journal.TerminalIntentDigest,
		TerminalCommandDigest:     journal.TerminalCommandDigest,
		TerminalSettlementDigest:  journal.TerminalSettlementDigest,
		PendingEventIDs:           eventIDs,
		PendingTransitionIDs:      transitionIDs,
		AttemptedTransitionIDs:    journal.AttemptedTransitionIDs,
		PendingAcknowledgementIDs: acknowledgementIDs,
		PendingGoalDeliveryIDs:    goalDeliveryIDs,
		DeliveredGoalDeliveryIDs:  deliveredGoalDeliveryIDs,
		NativeUsageObservation:    journal.NativeUsageObservation,
		NativeUsageRecovery:       journal.NativeUsageRecoveryRequired,
		NativeTerminalRecovery:    journal.NativeUsageTerminalRecovery,
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf("%#v", value)
	}
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

func (daemon *daemon) flushRun(ctx context.Context, journal state.RunJournal) (err error) {
	defer func() {
		if err == nil {
			return
		}
		var failure *outboxFailure
		if !errors.As(err, &failure) {
			err = &outboxFailure{journal: journal, err: err}
		}
	}()

	key := journal.Key()
	if journal.NativeUsageRecoveryRequired {
		if daemon.liveNativeUsageSettlementOwns(key) {
			return nil
		}
		updated, usage, recoveryErr := daemon.recoverNativeUsageDelivery(journal)
		if recoveryErr != nil {
			return recoveryErr
		}
		journal = updated
		updated, recoveryErr = daemon.queueNativeUsageRecoveryTerminal(ctx, journal, nil)
		if recoveryErr != nil {
			return recoveryErr
		}
		journal = updated
		updated, recoveryErr = daemon.store.ClearNativeUsageRecoveryRequired(key, usage)
		if recoveryErr != nil {
			return recoveryErr
		}
		journal = updated
	}
	if journal.LocalState == "stale" {
		// A stale run sends no ordinary lifecycle traffic, but immutable Goal receipts and
		// acknowledgements still need final delivery or retirement — cleanup can erase them.
		updated, _, err := daemon.flushGoalDeliveries(ctx, journal)
		if err != nil {
			return err
		}
		journal = updated
		for len(journal.PendingCommandAcknowledgements) > 0 {
			acknowledgement := journal.PendingCommandAcknowledgements[0]
			updated, err = daemon.deliverAcknowledgement(ctx, journal, acknowledgement)
			journal = updated
			if err != nil {
				return err
			}
		}
		if hasPendingStaleOutbox(journal) {
			return nil
		}
		return daemon.scheduleCleanup(ctx, journal)
	}
	if journal.LocalState == "cleanup_pending" {
		updated, _, err := daemon.flushGoalDeliveries(ctx, journal)
		if err != nil {
			return err
		}
		if updated.HasPendingGoalDeliveries() {
			return nil
		}
		return daemon.scheduleCleanup(ctx, updated)
	}
	if daemon.isStarting(key) {
		// A nil return here defers rather than succeeds: the outbox retries on
		// its wake signal and 1s interval; journal readers must wait for !isStarting.
		return nil
	}
	if journal.LocalState == "terminal_pending" && journal.TerminalVerdict != "" && journal.TerminalVerdict != state.TerminalVerdictAccepted {
		updated, err := daemon.store.ResolveTerminalForCleanup(key, journal.TerminalVerdict, daemon.now())
		if err != nil {
			return err
		}
		if !updated.HasProcessDetails() {
			return daemon.flushRun(ctx, updated)
		}
		daemon.releaseSlotOnce(key)
		journal = updated
	}
	updated, _, err := daemon.flushGoalDeliveries(ctx, journal)
	journal = updated
	if err != nil {
		return err
	}
	predecessorsUnavailable := false
	updated, err = daemon.deliverEventsBeforeAppliedInputAcknowledgement(ctx, journal)
	journal = updated
	if err != nil {
		if journal.LocalState != "terminal_pending" || !terminalPredecessorUnavailable(err) {
			return err
		}
		updated, retireErr := daemon.retireOrdinaryTerminalOutbox(journal)
		if retireErr != nil {
			return retireErr
		}
		journal = updated
		predecessorsUnavailable = true
	}
	for len(journal.PendingTransitions) > 0 {
		nextTransition := journal.PendingTransitions[0]
		if isTerminalTransition(nextTransition.State) {
			if !predecessorsUnavailable {
				updated, delivered, err := daemon.deliverAppliedInputAcknowledgement(ctx, journal)
				journal = updated
				if err != nil {
					return err
				}
				updated, err = daemon.deliverPendingEvents(ctx, journal)
				journal = updated
				if err != nil {
					if !terminalPredecessorUnavailable(err) {
						return err
					}
					updated, retireErr := daemon.retireOrdinaryTerminalOutbox(journal)
					if retireErr != nil {
						return retireErr
					}
					journal = updated
					predecessorsUnavailable = true
				}
				if delivered {
					continue
				}
				if len(journal.PendingEvents) > 0 {
					continue
				}
			}
		} else {
			updated, delivered, err := daemon.deliverAppliedInputAcknowledgement(ctx, journal)
			journal = updated
			if err != nil {
				return err
			}
			if delivered {
				updated, err := daemon.deliverPendingEvents(ctx, journal)
				journal = updated
				if err != nil {
					return err
				}
				continue
			}
		}
		if isTerminalTransition(journal.PendingTransitions[0].State) {
			// A validator can atomically append evidence and a terminal while earlier transitions
			// are being delivered: drain the evidence prerequisite before terminal delivery.
			updated, loadErr := daemon.store.LoadJournal(key)
			if loadErr != nil {
				return loadErr
			}
			journal = updated
			updated, _, flushErr := daemon.flushGoalDeliveries(ctx, journal)
			if flushErr != nil {
				return flushErr
			}
			journal = updated
			if len(journal.PendingTransitions) == 0 {
				continue
			}
		}
		updated, err := daemon.store.MarkTransitionAttempted(key, journal.PendingTransitions[0].TransitionID)
		if err != nil {
			return err
		}
		journal = updated
		transition := journal.PendingTransitions[0]
		wasTerminal := journal.LocalState == "terminal_pending"
		requestContext, cancel := daemon.controlContext(ctx)
		err = daemon.control.Transition(requestContext, journal.RunID, transition)
		cancel()
		current, loadErr := daemon.store.LoadJournal(key)
		if loadErr != nil {
			return loadErr
		}
		journal = current
		if !current.HasPendingTransition(transition.TransitionID) {
			return errOutboxChanged
		}
		if err != nil {
			if !wasTerminal && journal.LocalState == "terminal_pending" && !isTerminalTransition(transition.State) {
				return errOutboxChanged
			}
			if isTerminalTransition(transition.State) {
				return daemon.handleTerminalDeliveryError(journal, err)
			}
			if journal.LocalState == "terminal_pending" && terminalPredecessorUnavailable(err) {
				updated, retireErr := daemon.retireOrdinaryTerminalOutbox(journal)
				if retireErr != nil {
					return retireErr
				}
				journal = updated
				predecessorsUnavailable = true
				continue
			}
			return daemon.handleOrdinaryDeliveryError(ctx, journal, err, "transition ownership lost")
		}
		if journal.LocalState == "terminal_pending" && isTerminalTransition(transition.State) {
			updated, err := daemon.resolveTerminal(journal, state.TerminalVerdictAccepted)
			if err != nil {
				return err
			}
			journal = updated
		}
		updated, err = daemon.store.MarkTransitionsDelivered(key, []string{transition.TransitionID})
		if err != nil {
			return err
		}
		journal = updated
	}
	updated, delivered, err := daemon.deliverAppliedInputAcknowledgement(ctx, journal)
	journal = updated
	if err != nil {
		return err
	}
	if delivered {
		updated, err := daemon.deliverPendingEvents(ctx, journal)
		journal = updated
		if err != nil {
			return err
		}
	}
	for len(journal.PendingCommandAcknowledgements) > 0 {
		acknowledgement := journal.PendingCommandAcknowledgements[0]
		updated, err := daemon.deliverAcknowledgement(ctx, journal, acknowledgement)
		journal = updated
		if err != nil {
			return err
		}
	}
	// The retained-session stop receipt stays blocked until Control has acknowledged the terminal
	// transition: releasing the session earlier would let a subsequent Run overlap a terminal one.
	updated, _, err = daemon.flushGoalDeliveries(ctx, journal)
	journal = updated
	if err != nil {
		return err
	}
	if journal.LocalState == "terminal_pending" && journal.TerminalVerdict == state.TerminalVerdictAccepted && len(journal.PendingEvents) == 0 && len(journal.PendingTransitions) == 0 && len(journal.PendingCommandAcknowledgements) == 0 && !journal.HasPendingGoalDeliveries() {
		if journal.HasProcessDetails() || daemon.cleanupBlocked(key) {
			daemon.releaseSlotOnce(key)
			return nil
		}
		updated, err := daemon.enterCleanupPending(journal)
		if err != nil {
			return err
		}
		return daemon.scheduleCleanup(ctx, updated)
	}
	return nil
}

func (daemon *daemon) deliverEventsBeforeAppliedInputAcknowledgement(ctx context.Context, journal state.RunJournal) (state.RunJournal, error) {
	intent := journal.InputCommandIntent
	if intent == nil || intent.Outcome != "applied" || intent.AcknowledgementDelivered {
		return daemon.deliverPendingEvents(ctx, journal)
	}
	events := make([]protocol.RunEvent, 0, len(journal.PendingEvents))
	for _, event := range journal.PendingEvents {
		if event.Sequence <= intent.EventSequenceBarrier {
			events = append(events, event)
		}
	}
	return daemon.deliverEvents(ctx, journal, events)
}

func (daemon *daemon) deliverPendingEvents(ctx context.Context, journal state.RunJournal) (state.RunJournal, error) {
	if len(journal.PendingEvents) == 0 && journal.DroppedOutputChunks > 0 {
		return daemon.queueOutputTruncatedMarker(journal)
	}
	return daemon.deliverEvents(ctx, journal, journal.PendingEvents)
}

func (daemon *daemon) deliverEvents(ctx context.Context, journal state.RunJournal, events []protocol.RunEvent) (state.RunJournal, error) {
	if len(events) == 0 {
		if journal.DroppedOutputChunks > 0 {
			return daemon.queueOutputTruncatedMarker(journal)
		}
		return journal, nil
	}
	key := journal.Key()
	wasTerminal := journal.LocalState == "terminal_pending"
	requestContext, cancel := daemon.controlContext(ctx)
	err := daemon.control.AppendEvents(requestContext, journal.RunID, protocol.AppendEventsRequest{Fence: journal.Fence(), Events: events})
	cancel()
	if err != nil {
		current, loadErr := daemon.store.LoadJournal(key)
		if loadErr != nil {
			return journal, loadErr
		}
		journal = current
		if !wasTerminal && journal.LocalState == "terminal_pending" {
			return journal, errOutboxChanged
		}
		return journal, daemon.handleOrdinaryDeliveryError(ctx, journal, err, "event ownership lost")
	}
	ids := make([]string, len(events))
	for index, event := range events {
		ids[index] = event.EventID
	}
	daemon.eventReceiptMu.Lock()
	updated, appended, err := daemon.store.MarkEventsDeliveredAndQueueOutputTruncatedMarker(key, ids, daemon.now(), daemon.options.newID)
	daemon.eventReceiptMu.Unlock()
	if err != nil {
		return journal, err
	}
	if appended {
		daemon.signalOutboxFor(key)
	}
	return updated, nil
}

func (daemon *daemon) queueOutputTruncatedMarker(journal state.RunJournal) (state.RunJournal, error) {
	updated, appended, err := daemon.store.QueueOutputTruncatedMarker(journal.Key(), daemon.now(), daemon.options.newID)
	if err != nil {
		return journal, err
	}
	if appended {
		daemon.signalOutboxFor(journal.Key())
	}
	return updated, nil
}

func (daemon *daemon) deliverAppliedInputAcknowledgement(ctx context.Context, journal state.RunJournal) (state.RunJournal, bool, error) {
	intent := journal.InputCommandIntent
	if intent == nil || intent.Outcome != "applied" || intent.AcknowledgementDelivered || journal.HasPendingTransition(intent.RunningTransitionID) {
		return daemon.deliverAppliedControlAcknowledgement(ctx, journal)
	}
	for _, acknowledgement := range journal.PendingCommandAcknowledgements {
		if acknowledgement.AckID == intent.AckID {
			updated, err := daemon.deliverAcknowledgement(ctx, journal, acknowledgement)
			return updated, true, err
		}
	}
	return journal, false, errors.New("applied input acknowledgement is not pending")
}

func (daemon *daemon) deliverAcknowledgement(ctx context.Context, journal state.RunJournal, acknowledgement protocol.CommandAcknowledgement) (state.RunJournal, error) {
	key := journal.Key()
	wasTerminal := journal.LocalState == "terminal_pending"
	requestContext, cancel := daemon.controlContext(ctx)
	err := daemon.control.AcknowledgeCommand(requestContext, acknowledgement.CommandID, acknowledgement)
	cancel()
	current, loadErr := daemon.store.LoadJournal(key)
	if loadErr != nil {
		return journal, loadErr
	}
	journal = current
	if !hasPendingAcknowledgement(journal, acknowledgement.AckID) {
		return journal, errOutboxChanged
	}
	if err != nil {
		if !wasTerminal && journal.LocalState == "terminal_pending" {
			return journal, errOutboxChanged
		}
		if journal.LocalState == "stale" && (control.IsOwnershipLost(err) || control.IsTerminalGraceExpired(err) || invalidatedSupervisoryAcknowledgement(journal, acknowledgement, err)) {
			updated, markErr := daemon.markCommandAcknowledgementDelivered(key, acknowledgement)
			if markErr != nil {
				return journal, markErr
			}
			return updated, nil
		}
		if updated, retired, retireErr := daemon.retireAcceptedTerminalAcknowledgement(journal, acknowledgement, err); retired || retireErr != nil {
			if retireErr != nil {
				return journal, retireErr
			}
			return updated, nil
		}
		return journal, daemon.handleAcknowledgementDeliveryError(ctx, journal, err)
	}
	if journal.LocalState == "terminal_pending" {
		daemon.releaseSlotOnce(key)
	}
	updated, err := daemon.markCommandAcknowledgementDelivered(key, acknowledgement)
	if err != nil {
		return journal, err
	}
	return updated, nil
}

func hasPendingAcknowledgement(journal state.RunJournal, acknowledgementID string) bool {
	for _, acknowledgement := range journal.PendingCommandAcknowledgements {
		if acknowledgement.AckID == acknowledgementID {
			return true
		}
	}
	return false
}

func isTerminalTransition(stateName string) bool {
	switch stateName {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func durableTerminalPresent(journal state.RunJournal) bool {
	for _, transition := range journal.PendingTransitions {
		if isTerminalTransition(transition.State) {
			return true
		}
	}
	return isTerminalTransition(journal.TerminalState)
}

func durableTerminalTransition(journal state.RunJournal) (*protocol.StateTransitionRequest, bool) {
	for index := range journal.PendingTransitions {
		if isTerminalTransition(journal.PendingTransitions[index].State) {
			return &journal.PendingTransitions[index], true
		}
	}
	if isTerminalTransition(journal.TerminalState) {
		// The original transition body is intentionally not retained after the control-plane
		// acknowledgement: such a terminal cannot be proven to be the caller's exact write.
		return nil, true
	}
	return nil, false
}

func transitionWithJournalFence(transition protocol.StateTransitionRequest, journal state.RunJournal) protocol.StateTransitionRequest {
	if transition.Fence == (protocol.Fence{}) {
		transition.Fence = journal.Fence()
	}
	return transition
}

func sameStateTransitionRequest(left, right protocol.StateTransitionRequest) bool {
	return left.Fence == right.Fence &&
		left.TransitionID == right.TransitionID &&
		left.State == right.State &&
		bytes.Equal(left.Payload, right.Payload)
}

func terminalTransitionMatches(journal state.RunJournal, expected protocol.StateTransitionRequest) (bool, bool, string) {
	actual, present := durableTerminalTransition(journal)
	if !present {
		return false, false, ""
	}
	if actual == nil {
		return false, true, journal.TerminalState
	}
	expected = transitionWithJournalFence(expected, journal)
	return sameStateTransitionRequest(*actual, expected), true, actual.State
}

func terminalReceiptMatches(journal state.RunJournal, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement) (bool, bool) {
	if matches, present := state.TerminalSettlementMatches(journal, transition, acknowledgement); present {
		return matches, !matches
	}
	actualTransition, terminalPresent := durableTerminalTransition(journal)
	transition = transitionWithJournalFence(transition, journal)
	acknowledgement = acknowledgementWithJournalFence(acknowledgement, journal)
	acknowledgementMatch := false
	acknowledgementConflict := false
	for _, pending := range journal.PendingCommandAcknowledgements {
		if pending.CommandID != acknowledgement.CommandID && pending.AckID != acknowledgement.AckID {
			continue
		}
		if sameCommandAcknowledgement(pending, acknowledgement) {
			acknowledgementMatch = true
		} else {
			acknowledgementConflict = true
		}
		break
	}
	if !terminalPresent {
		return false, acknowledgementConflict
	}
	if actualTransition != nil {
		if !sameStateTransitionRequest(*actualTransition, transition) {
			return false, true
		}
	} else if journal.TerminalState != transition.State {
		return false, true
	}
	if acknowledgementMatch {
		return true, false
	}
	// A delivered or retired acknowledgement no longer carries enough local
	// data to prove an exact replay. Keep the authority barrier fail-closed.
	return false, true
}

func acknowledgementWithJournalFence(acknowledgement protocol.CommandAcknowledgement, journal state.RunJournal) protocol.CommandAcknowledgement {
	if acknowledgement.Fence == (protocol.Fence{}) {
		acknowledgement.Fence = journal.Fence()
	}
	return acknowledgement
}

func (daemon *daemon) retireOrdinaryTerminalOutbox(journal state.RunJournal) (state.RunJournal, error) {
	if len(journal.PendingEvents) > 0 {
		eventIDs := make([]string, len(journal.PendingEvents))
		for index, event := range journal.PendingEvents {
			eventIDs[index] = event.EventID
		}
		daemon.eventReceiptMu.Lock()
		updated, err := daemon.store.MarkEventsDelivered(journal.Key(), eventIDs)
		daemon.eventReceiptMu.Unlock()
		if err != nil {
			return state.RunJournal{}, err
		}
		journal = updated
	}
	transitionIDs := make([]string, 0, len(journal.PendingTransitions))
	for _, transition := range journal.PendingTransitions {
		if !isTerminalTransition(transition.State) {
			transitionIDs = append(transitionIDs, transition.TransitionID)
		}
	}
	if len(transitionIDs) == 0 {
		return journal, nil
	}
	return daemon.store.MarkTransitionsDelivered(journal.Key(), transitionIDs)
}

func terminalPredecessorUnavailable(err error) bool {
	return control.IsOwnershipLost(err) || control.IsTerminalGraceExpired(err)
}

func (daemon *daemon) resolveTerminal(journal state.RunJournal, verdict string) (state.RunJournal, error) {
	updated, err := daemon.store.ResolveTerminal(journal.Key(), verdict, daemon.now())
	if err != nil {
		return state.RunJournal{}, err
	}
	daemon.releaseSlotOnce(journal.Key())
	return updated, nil
}

func (daemon *daemon) handleTerminalDeliveryError(journal state.RunJournal, err error) error {
	if journal.LocalState == "terminal_pending" {
		verdict := ""
		switch {
		case control.IsOwnershipLost(err):
			verdict = state.TerminalVerdictOwnershipLost
		case control.IsTerminalGraceExpired(err):
			verdict = state.TerminalVerdictGraceExpired
		}
		if verdict != "" {
			updated, resolveErr := daemon.store.ResolveTerminalForCleanup(journal.Key(), verdict, daemon.now())
			if resolveErr != nil {
				return resolveErr
			}
			daemon.mu.Lock()
			active := daemon.running[journal.Key()]
			ownerCurrent := active != nil && active.nativeTerminalOwner
			ownerClaim := uint64(0)
			if ownerCurrent {
				ownerClaim = active.nativeTerminalOwnerClaim
			}
			daemon.mu.Unlock()
			if ownerCurrent {
				daemon.finalizeConclusiveNativeTerminalOwner(journal.Key(), active, ownerClaim)
			}
			if cleanupErr := daemon.scheduleCleanup(context.Background(), updated); cleanupErr != nil {
				return cleanupErr
			}
			return err
		}
	}
	return err
}

func (daemon *daemon) handleAcknowledgementDeliveryError(ctx context.Context, journal state.RunJournal, err error) error {
	if journal.LocalState == "terminal_pending" {
		return daemon.handleTerminalDeliveryError(journal, err)
	}
	return daemon.handleOrdinaryDeliveryError(ctx, journal, err, "ack ownership lost")
}

func (daemon *daemon) retireAcceptedTerminalAcknowledgement(journal state.RunJournal, acknowledgement protocol.CommandAcknowledgement, err error) (state.RunJournal, bool, error) {
	if journal.LocalState != "terminal_pending" || journal.TerminalVerdict != state.TerminalVerdictAccepted || (!control.IsOwnershipLost(err) && !control.IsTerminalGraceExpired(err) && !invalidatedSupervisoryAcknowledgement(journal, acknowledgement, err)) {
		return journal, false, nil
	}
	updated, markErr := daemon.markCommandAcknowledgementDelivered(journal.Key(), acknowledgement)
	if markErr != nil {
		return state.RunJournal{}, false, markErr
	}
	return updated, true, nil
}

func (daemon *daemon) markCommandAcknowledgementDelivered(key state.RunKey, acknowledgement protocol.CommandAcknowledgement) (state.RunJournal, error) {
	keyForCommand := commandKey{run: key, id: acknowledgement.CommandID}
	daemon.commandReceiptMu.Lock()
	daemon.mu.Lock()
	daemon.commands.receipts.suppress(keyForCommand)
	daemon.mu.Unlock()
	daemon.commandReceiptMu.Unlock()
	var (
		updated state.RunJournal
		err     error
	)
	if daemon.options.markCommandAcknowledgementsDelivered != nil {
		updated, err = daemon.options.markCommandAcknowledgementsDelivered(key, []string{acknowledgement.AckID})
	} else {
		updated, err = daemon.store.MarkCommandAcknowledgementsDelivered(key, []string{acknowledgement.AckID})
	}
	if err != nil {
		return state.RunJournal{}, err
	}
	daemon.commandReceiptMu.Lock()
	defer daemon.commandReceiptMu.Unlock()
	daemon.mu.Lock()
	daemon.commands.markDelivered(keyForCommand)
	daemon.mu.Unlock()
	return updated, nil
}

func (daemon *daemon) handleOrdinaryDeliveryError(ctx context.Context, journal state.RunJournal, err error, ownershipReason string) error {
	if control.IsOwnershipLost(err) && !restartOutboxRecovery(ctx) {
		daemon.terminateForLease(journal, ownershipReason)
	}
	return err
}

func restartOutboxRecovery(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	value, _ := ctx.Value(restartOutboxRecoveryContextKey{}).(bool)
	return value
}
