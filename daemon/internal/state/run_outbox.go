package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

// RunOutboxState is the run-event, transition and command-acknowledgement
// outbox block of RunJournal plus the dropped-output accounting.
type RunOutboxState struct {
	LastEventSequence              int64                             `json:"last_event_sequence"`
	PendingEvents                  []protocol.RunEvent               `json:"pending_events"`
	DroppedOutputChunks            int64                             `json:"dropped_output_chunks,omitempty"`
	DroppedOutputBytes             int64                             `json:"dropped_output_bytes,omitempty"`
	PendingTransitions             []protocol.StateTransitionRequest `json:"pending_transitions"`
	AttemptedTransitionIDs         []string                          `json:"attempted_transition_ids,omitempty"`
	PendingCommandAcknowledgements []protocol.CommandAcknowledgement `json:"pending_command_acknowledgements"`
}

func (journal RunOutboxState) validatePendingEntries(runID string, fence protocol.Fence) error {
	lastSequence := int64(0)
	for _, event := range journal.PendingEvents {
		if !validEvent(event) || event.Sequence <= lastSequence || event.Sequence > journal.LastEventSequence {
			return errors.New("run journal is invalid")
		}
		lastSequence = event.Sequence
	}
	for _, transition := range journal.PendingTransitions {
		if !validTransition(transition) || !sameFence(transition.Fence, fence) {
			return errors.New("run journal is invalid")
		}
	}
	if !validAttemptedTransitions(journal.AttemptedTransitionIDs, journal.PendingTransitions) {
		return errors.New("run journal is invalid")
	}
	commandIDs := make(map[string]struct{}, len(journal.PendingCommandAcknowledgements))
	for _, acknowledgement := range journal.PendingCommandAcknowledgements {
		if !validAcknowledgement(acknowledgement, runID, fence) {
			return errors.New("run journal is invalid")
		}
		if _, exists := commandIDs[acknowledgement.CommandID]; exists {
			return errors.New("run journal is invalid")
		}
		commandIDs[acknowledgement.CommandID] = struct{}{}
	}
	return nil
}

// QueueEvent durably enqueues an idempotent event before its HTTP request.
func (store *Store) QueueEvent(key RunKey, event protocol.RunEvent) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		return appendEvent(journal, event, "event is invalid")
	})
}

// QueueNextEvent assigns sequence under the same lock as control receipts, so
// concurrent stdin failures cannot invalidate an output event's sequence.
func (store *Store) QueueNextEvent(key RunKey, event protocol.RunEvent) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		event.Sequence = journal.LastEventSequence + 1
		return appendEvent(journal, event, "event is invalid")
	})
}

// QueueOutputEvent bounds only pending raw output. Dropped chunks are recorded
// durably without consuming an event sequence so later semantic events remain
// contiguous.
func (store *Store) QueueOutputEvent(key RunKey, event protocol.RunEvent, budget int) (RunJournal, bool, error) {
	dropped := false
	journal, err := store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() {
			return errors.New("journal has no claim grant")
		}
		if event.Kind != "output" || !validRequiredString(event.EventID, 4096) || event.OccurredAt.IsZero() || !validRawMessage(event.Payload) {
			return errors.New("event is invalid")
		}
		pending := 0
		for _, pendingEvent := range journal.PendingEvents {
			if pendingEvent.Kind == "output" {
				pending += len(pendingEvent.Payload)
			}
		}
		payloadBytes := len(event.Payload)
		if budget >= 0 && (pending > budget || payloadBytes > budget-pending) {
			journal.DroppedOutputChunks++
			journal.DroppedOutputBytes += int64(payloadBytes)
			dropped = true
			return nil
		}
		event.Sequence = journal.LastEventSequence + 1
		return appendEvent(journal, event, "event is invalid")
	})
	if err != nil {
		// The mutation is only a persisted drop after the atomic journal write
		// succeeds. In particular, do not report a drop when a capacity or write
		// failure rejected the counter update.
		dropped = false
	}
	return journal, dropped, err
}

// QueueOutputTruncatedMarker turns accumulated raw-output drops into one
// durable event while clearing the counters in the same journal mutation. It
// calls newEventID only after observing drops in the authoritative journal.
func (store *Store) QueueOutputTruncatedMarker(key RunKey, at time.Time, newEventID func() (string, error)) (RunJournal, bool, error) {
	appended := false
	journal, err := store.mutateJournal(key, func(journal *RunJournal) error {
		var err error
		appended, err = appendOutputTruncatedMarker(journal, at, newEventID)
		return err
	})
	return journal, appended, err
}

// MarkEventsDeliveredAndQueueOutputTruncatedMarker atomically confirms an
// accepted batch and records any preceding raw-output loss. A crash cannot
// otherwise leave loss counters with no pending event that can trigger a marker.
// newEventID is called inside the mutation only when the authoritative journal
// has drops to report, so a concurrent output drop cannot leave an accepted
// event pending because its caller used an older journal snapshot.
func (store *Store) MarkEventsDeliveredAndQueueOutputTruncatedMarker(key RunKey, eventIDs []string, at time.Time, newEventID func() (string, error)) (RunJournal, bool, error) {
	appended := false
	journal, err := store.mutateJournal(key, func(journal *RunJournal) error {
		journal.PendingEvents = removeEvents(journal.PendingEvents, eventIDs)
		var err error
		appended, err = appendOutputTruncatedMarker(journal, at, newEventID)
		return err
	})
	return journal, appended, err
}

func appendOutputTruncatedMarker(journal *RunJournal, at time.Time, newEventID func() (string, error)) (bool, error) {
	if !journal.hasClaimGrant() {
		return false, errors.New("journal has no claim grant")
	}
	if journal.DroppedOutputChunks == 0 {
		return false, nil
	}
	if newEventID == nil {
		return false, errors.New("output truncation marker ID generator is unavailable")
	}
	eventID, err := newEventID()
	if err != nil {
		return false, err
	}
	payload, err := json.Marshal(map[string]int64{
		"dropped_chunks": journal.DroppedOutputChunks,
		"dropped_bytes":  journal.DroppedOutputBytes,
	})
	if err != nil {
		return false, err
	}
	event := protocol.RunEvent{EventID: eventID, Kind: "output_truncated", Sequence: journal.LastEventSequence + 1, OccurredAt: at, Payload: payload}
	if err := appendEvent(journal, event, "event is invalid"); err != nil {
		return false, err
	}
	journal.DroppedOutputChunks = 0
	journal.DroppedOutputBytes = 0
	return true, nil
}

// QueueWaitingForInput atomically records a waiting event and the associated
// lifecycle transition. Repeated waiting records refresh an undelivered
// transition's payload without creating a second transition.
func (store *Store) QueueWaitingForInput(key RunKey, event protocol.RunEvent, transition protocol.StateTransitionRequest) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		event.Sequence = journal.LastEventSequence + 1
		if event.Kind != "waiting_for_input" {
			return errors.New("waiting event is invalid")
		}
		if journal.LocalState == "paused" {
			event.Kind = "agent_event"
			return appendEvent(journal, event, "waiting event is invalid")
		}
		if journal.LocalState == "terminal_pending" || hasPendingTerminalTransition(journal.PendingTransitions) {
			return appendEvent(journal, event, "waiting event is invalid")
		}
		prepared, err := prepareTransition(journal, transition)
		if err != nil {
			return err
		}
		if prepared.State != "waiting_for_input" {
			return errors.New("waiting transition state is invalid")
		}
		if err := appendEvent(journal, event, "waiting event is invalid"); err != nil {
			return err
		}
		if len(journal.PendingTransitions) == 0 {
			if journal.LocalState != "waiting_for_input" {
				journal.PendingTransitions = append(journal.PendingTransitions, prepared)
			}
		} else if last := len(journal.PendingTransitions) - 1; journal.PendingTransitions[last].State == "waiting_for_input" {
			if !hasAttemptedTransition(journal.AttemptedTransitionIDs, journal.PendingTransitions[last].TransitionID) {
				journal.PendingTransitions[last].Payload = prepared.Payload
			}
		} else {
			journal.PendingTransitions = append(journal.PendingTransitions, prepared)
		}
		journal.LocalState = "waiting_for_input"
		return nil
	})
}

func appendEvent(journal *RunJournal, event protocol.RunEvent, invalidMessage string) error {
	if !journal.hasClaimGrant() {
		return errors.New("journal has no claim grant")
	}
	if strings.TrimSpace(event.EventID) == "" || strings.TrimSpace(event.Kind) == "" || event.Sequence != journal.LastEventSequence+1 || event.OccurredAt.IsZero() || !validRawMessage(event.Payload) {
		return errors.New(invalidMessage)
	}
	journal.PendingEvents = append(journal.PendingEvents, event)
	journal.LastEventSequence = event.Sequence
	return nil
}

// MarkEventsDelivered removes successfully appended events by their stable IDs.
func (store *Store) MarkEventsDelivered(key RunKey, eventIDs []string) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		journal.PendingEvents = removeEvents(journal.PendingEvents, eventIDs)
		return nil
	})
}

// QueueTransition durably enqueues a fenced transition before its HTTP request.
func (store *Store) QueueTransition(key RunKey, transition protocol.StateTransitionRequest) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if isTerminalTransitionState(transition.State) {
			return errors.New("terminal transition requires terminal queue")
		}
		return queueTransition(journal, transition)
	})
}

// QueueRunningTransition atomically queues a running transition and records
// the corresponding local lifecycle state.
func (store *Store) QueueRunningTransition(key RunKey, transition protocol.StateTransitionRequest) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		prepared, err := prepareTransition(journal, transition)
		if err != nil {
			return err
		}
		if prepared.State != "running" {
			return errors.New("running transition state is invalid")
		}
		if journal.LocalState == "terminal_pending" || hasPendingTerminalTransition(journal.PendingTransitions) {
			return errors.New("running transition follows terminal transition")
		}
		journal.PendingTransitions = append(journal.PendingTransitions, prepared)
		journal.LocalState = "running"
		return nil
	})
}

// QueueTerminalTransition atomically makes a terminal transition durable and
// marks the local process terminal-pending before any HTTP request is sent.
func (store *Store) QueueTerminalTransition(key RunKey, transition protocol.StateTransitionRequest) (RunJournal, error) {
	return store.QueueTerminalTransitionAt(key, transition, time.Now().UTC())
}

// QueueTerminalTransitionAt atomically makes a terminal transition durable,
// records the first terminal-pending time, and marks the local process
// terminal-pending before any HTTP request is sent.
func (store *Store) QueueTerminalTransitionAt(key RunKey, transition protocol.StateTransitionRequest, pendingAt time.Time) (RunJournal, error) {
	if pendingAt.IsZero() {
		return RunJournal{}, errors.New("terminal pending time is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		return queueTerminalTransition(journal, transition, pendingAt)
	})
}

// QueueTerminalTransitionAndAcknowledgementAt atomically records an
// authoritative terminal transition and its command receipt.
func (store *Store) QueueTerminalTransitionAndAcknowledgementAt(key RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, pendingAt time.Time) (RunJournal, error) {
	return store.queueTerminalTransitionAndAcknowledgementAt(key, transition, acknowledgement, pendingAt, "")
}

// QueueCancelledTransitionAndAcknowledgementAt atomically records the
// authoritative cancellation terminal transition and its command receipt.
func (store *Store) QueueCancelledTransitionAndAcknowledgementAt(key RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, pendingAt time.Time) (RunJournal, error) {
	return store.queueTerminalTransitionAndAcknowledgementAt(key, transition, acknowledgement, pendingAt, "cancelled")
}

func (store *Store) queueTerminalTransitionAndAcknowledgementAt(key RunKey, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement, pendingAt time.Time, requiredState string) (RunJournal, error) {
	if pendingAt.IsZero() {
		return RunJournal{}, errors.New("terminal pending time is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if err := queueTerminalTransition(journal, transition, pendingAt); err != nil {
			return err
		}
		if requiredState != "" && journal.TerminalState != requiredState {
			return errors.New(requiredState + " terminal transition state is invalid")
		}
		if err := queueCommandAcknowledgement(journal, acknowledgement); err != nil {
			return err
		}
		commandDigest, err := terminalCommandDigest(*journal, acknowledgement)
		if err != nil {
			return err
		}
		if journal.TerminalCommandDigest != "" && journal.TerminalCommandDigest != commandDigest {
			return errors.New("terminal command intent conflicts with journal")
		}
		journal.TerminalCommandDigest = commandDigest
		digest, err := terminalSettlementDigest(*journal, transition, acknowledgement)
		if err != nil {
			return err
		}
		if journal.TerminalSettlementDigest != "" && journal.TerminalSettlementDigest != digest {
			return errors.New("terminal settlement conflicts with journal")
		}
		journal.TerminalSettlementDigest = digest
		return nil
	})
}

// TerminalIntentMatches verifies the terminal state and payload independently
// of the generated transition ID used for one delivery attempt.
func TerminalIntentMatches(journal RunJournal, stateName string, payload json.RawMessage) (bool, bool) {
	if journal.TerminalIntentDigest == "" {
		for _, transition := range journal.PendingTransitions {
			if !isTerminalTransitionState(transition.State) {
				continue
			}
			return transition.State == stateName && bytes.Equal(transition.Payload, payload), true
		}
		return false, journal.TerminalState != ""
	}
	digest, err := terminalIntentDigest(journal, stateName, payload)
	if err != nil {
		return false, true
	}
	return digest == journal.TerminalIntentDigest, true
}

// TerminalCommandIntentMatches verifies the terminal command and outcome
// independently of the generated acknowledgement ID.
func TerminalCommandIntentMatches(journal RunJournal, commandID, outcome string) (bool, bool) {
	if journal.TerminalCommandDigest == "" {
		for _, acknowledgement := range journal.PendingCommandAcknowledgements {
			if acknowledgement.CommandID != commandID {
				continue
			}
			return acknowledgement.Outcome == outcome, true
		}
		return false, false
	}
	digest, err := terminalCommandIntentDigest(journal, commandID, outcome)
	if err != nil {
		return false, true
	}
	return digest == journal.TerminalCommandDigest, true
}

// TerminalSettlementMatches verifies the exact terminal transition and command
// acknowledgement that were committed atomically, even after outbox delivery
// removes their pending bodies.
func TerminalSettlementMatches(journal RunJournal, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement) (bool, bool) {
	if journal.TerminalSettlementDigest == "" {
		return false, false
	}
	digest, err := terminalSettlementDigest(journal, transition, acknowledgement)
	if err != nil {
		return false, true
	}
	return digest == journal.TerminalSettlementDigest, true
}

// ResolveTerminal durably records a terminal control-plane verdict. Repeating
// the same verdict is idempotent; conflicting terminal outcomes are rejected.
func (store *Store) ResolveTerminal(key RunKey, verdict string, resolvedAt time.Time) (RunJournal, error) {
	if !validTerminalVerdict(verdict) || resolvedAt.IsZero() {
		return RunJournal{}, errors.New("terminal verdict is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.LocalState != "terminal_pending" {
			return errors.New("journal is not terminal pending")
		}
		if journal.TerminalVerdict != "" {
			if journal.TerminalVerdict != verdict {
				return errors.New("terminal verdict conflicts with journal")
			}
			return nil
		}
		if journal.TerminalPendingAt.IsZero() || !isTerminalTransitionState(journal.TerminalState) {
			return errors.New("journal terminal state is invalid")
		}
		journal.TerminalVerdict = verdict
		journal.TerminalResolvedAt = resolvedAt
		return nil
	})
}

// ResolveTerminalForCleanup atomically records a conclusive terminal rejection,
// retires unreachable delivery state, and makes local cleanup eligible only
// after the process-ownership marker has been cleared by a verified stop.
func (store *Store) ResolveTerminalForCleanup(key RunKey, verdict string, resolvedAt time.Time) (RunJournal, error) {
	if !IsConclusiveTerminalVerdict(verdict) || resolvedAt.IsZero() {
		return RunJournal{}, errors.New("terminal cleanup verdict is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.LocalState == "cleanup_pending" {
			if journal.TerminalVerdict != verdict {
				return errors.New("terminal verdict conflicts with journal")
			}
			if journal.HasProcessDetails() || journal.HasPendingContainment() {
				journal.LocalState = "terminal_pending"
			}
			return nil
		}
		if journal.LocalState != "terminal_pending" {
			return errors.New("journal is not terminal pending")
		}
		if journal.TerminalVerdict != "" && journal.TerminalVerdict != verdict {
			return errors.New("terminal verdict conflicts with journal")
		}
		if journal.TerminalPendingAt.IsZero() || !isTerminalTransitionState(journal.TerminalState) {
			return errors.New("journal terminal state is invalid")
		}
		if journal.TerminalVerdict == "" {
			journal.TerminalVerdict = verdict
			journal.TerminalResolvedAt = resolvedAt
		}
		settleUnresolvedProviderActions(journal, providerActionFailureTerminal)
		// Retention may have failed before mandatory fencing. Preserve the
		// durable supervisory evidence before retiring unreachable intents.
		if journal.TerminalState != "completed" && len(journal.ControlCommandIntents) != 0 {
			journal.RetainWorkspace = true
		}
		journal.PendingEvents = nil
		journal.PendingTransitions = nil
		journal.AttemptedTransitionIDs = nil
		journal.PendingCommandAcknowledgements = nil
		// Goal evidence and usage remain deliverable under their original fence
		// after a terminal verdict. Do not retire them with ordinary v1 outbox
		// state: their receiver has its own durable idempotency receipts.
		journal.InputCommandIntent = nil
		journal.ControlCommandIntents = nil
		if journal.LinuxCloneReuseGrant != nil && linuxCloneReuseAliasRetirable(*journal) {
			journal.LinuxCloneReuseRetired = true
		}
		if !journal.HasProcessDetails() && !journal.HasPendingContainment() {
			journal.LocalState = "cleanup_pending"
		}
		return nil
	})
}

// EnterCleanupPending records that terminal delivery has reached a conclusive
// outcome and local workspace cleanup is the only remaining daemon action.
// The accepted path requires every transition and command acknowledgement to
// be delivered first, while conclusive rejections retire unreachable delivery
// work.
func (store *Store) EnterCleanupPending(key RunKey) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if journal.HasProcessDetails() || journal.HasPendingContainment() {
			return errors.New("process stop evidence remains pending")
		}
		if journal.LocalState == "cleanup_pending" {
			return nil
		}
		if journal.LocalState != "terminal_pending" || !validTerminalVerdict(journal.TerminalVerdict) {
			return errors.New("journal is not cleanup eligible")
		}
		if journal.NativeUsageRecoveryRequired {
			return errors.New("native usage recovery remains pending")
		}
		if journal.InputCommandIntent != nil && (journal.InputCommandIntent.Outcome == "" || !journal.InputCommandIntent.AcknowledgementDelivered) {
			return errors.New("input command receipt is not delivered")
		}
		if journal.TerminalVerdict == TerminalVerdictAccepted {
			for _, intent := range journal.ControlCommandIntents {
				if intent.Outcome == "" || !intent.AcknowledgementDelivered {
					return errors.New("control command receipt is not delivered")
				}
			}
			if len(journal.PendingTransitions) != 0 || len(journal.PendingCommandAcknowledgements) != 0 || len(journal.PendingGoalDeliveries) != 0 {
				return errors.New("accepted terminal delivery is incomplete")
			}
		} else {
			journal.PendingTransitions = nil
			journal.PendingCommandAcknowledgements = nil
			journal.ControlCommandIntents = nil
		}
		journal.PendingEvents = nil
		journal.AttemptedTransitionIDs = nil
		journal.LocalState = "cleanup_pending"
		return nil
	})
}

func setTerminalPending(journal *RunJournal, pendingAt time.Time, terminalState string) {
	journal.LocalState = "terminal_pending"
	if journal.TerminalPendingAt.IsZero() {
		journal.TerminalPendingAt = pendingAt
	}
	journal.TerminalState = terminalState
}

// MarkTransitionsDelivered removes successfully applied transitions by ID.
func (store *Store) MarkTransitionsDelivered(key RunKey, transitionIDs []string) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		journal.PendingTransitions = removeTransitions(journal.PendingTransitions, transitionIDs)
		journal.AttemptedTransitionIDs = retainAttemptedTransitions(journal.AttemptedTransitionIDs, journal.PendingTransitions)
		return nil
	})
}

// MarkTransitionAttempted durably freezes a pending transition before its HTTP
// request is sent. Repeating the marker for the same pending transition is a
// no-op.
func (store *Store) MarkTransitionAttempted(key RunKey, transitionID string) (RunJournal, error) {
	if !validRequiredString(transitionID, 4096) {
		return RunJournal{}, errors.New("transition ID is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.HasPendingTransition(transitionID) {
			return errors.New("transition is not pending")
		}
		if !hasAttemptedTransition(journal.AttemptedTransitionIDs, transitionID) {
			journal.AttemptedTransitionIDs = append(journal.AttemptedTransitionIDs, transitionID)
		}
		return nil
	})
}

// QueueCommandAcknowledgement durably queues an acknowledgement before its HTTP
// request. A zero fence is populated from the current journal.
func (store *Store) QueueCommandAcknowledgement(key RunKey, acknowledgement protocol.CommandAcknowledgement) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		return queueCommandAcknowledgement(journal, acknowledgement)
	})
}

// MarkCommandAcknowledgementsDelivered removes acknowledgements confirmed by the
// control plane, keyed by their caller-generated ack IDs.
func (store *Store) MarkCommandAcknowledgementsDelivered(key RunKey, acknowledgementIDs []string) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		journal.PendingCommandAcknowledgements = removeAcknowledgements(journal.PendingCommandAcknowledgements, acknowledgementIDs)
		if journal.InputCommandIntent != nil && slices.Contains(acknowledgementIDs, journal.InputCommandIntent.AckID) {
			if journal.InputCommandIntent.Outcome == "" {
				return errors.New("input command acknowledgement is unresolved")
			}
			journal.InputCommandIntent.AcknowledgementDelivered = true
		}
		for index := range journal.ControlCommandIntents {
			intent := &journal.ControlCommandIntents[index]
			if slices.Contains(acknowledgementIDs, intent.AckID) {
				if intent.Outcome == "" {
					return errors.New("control command acknowledgement is unresolved")
				}
				intent.AcknowledgementDelivered = true
			}
		}
		return nil
	})
}

func queueTransition(journal *RunJournal, transition protocol.StateTransitionRequest) error {
	prepared, err := prepareTransition(journal, transition)
	if err != nil {
		return err
	}
	journal.PendingTransitions = append(journal.PendingTransitions, prepared)
	return nil
}

func prepareTransition(journal *RunJournal, transition protocol.StateTransitionRequest) (protocol.StateTransitionRequest, error) {
	if !journal.hasClaimGrant() {
		return protocol.StateTransitionRequest{}, errors.New("journal has no claim grant")
	}
	if strings.TrimSpace(transition.TransitionID) == "" || strings.TrimSpace(transition.State) == "" || !validRawMessage(transition.Payload) {
		return protocol.StateTransitionRequest{}, errors.New("state transition is invalid")
	}
	if isZeroFence(transition.Fence) {
		transition.Fence = journal.Fence()
	}
	if !sameFence(transition.Fence, journal.Fence()) {
		return protocol.StateTransitionRequest{}, errors.New("state transition fence does not match journal")
	}
	return transition, nil
}

func queueTerminalTransition(journal *RunJournal, transition protocol.StateTransitionRequest, pendingAt time.Time) error {
	if journal.TerminalVerdict != "" {
		return errors.New("terminal verdict is already recorded")
	}
	prepared, err := prepareTransition(journal, transition)
	if err != nil {
		return err
	}
	if !isTerminalTransitionState(prepared.State) {
		return errors.New("terminal transition state is invalid")
	}
	intentDigest, err := terminalIntentDigest(*journal, prepared.State, prepared.Payload)
	if err != nil {
		return err
	}
	if journal.TerminalIntentDigest != "" && journal.TerminalIntentDigest != intentDigest {
		return errors.New("terminal intent conflicts with journal")
	}
	if existing, present := authoritativeTerminalTransition(journal); present {
		if existing == nil || !sameStateTransition(*existing, prepared) {
			return errors.New("terminal transition conflicts with journal")
		}
		journal.TerminalIntentDigest = intentDigest
		return nil
	}
	if err := settleUnresolvedInputCommand(journal); err != nil {
		return err
	}
	if err := settleUnresolvedControlCommands(journal); err != nil {
		return err
	}
	settleUnresolvedProviderActions(journal, providerActionFailureTerminal)
	if prepared.State == "cancelled" {
		journal.RetainWorkspace = true
		journal.PendingTransitions = []protocol.StateTransitionRequest{prepared}
		journal.AttemptedTransitionIDs = retainAttemptedTransitions(journal.AttemptedTransitionIDs, journal.PendingTransitions)
		journal.TerminalIntentDigest = intentDigest
		setTerminalPending(journal, pendingAt, prepared.State)
		return nil
	}
	if terminalState := pendingTerminalState(journal.PendingTransitions); terminalState != "" {
		setTerminalPending(journal, pendingAt, terminalState)
		return nil
	}
	if prepared.State == "completed" {
		kind, err := terminalTaskResultKind(prepared.Payload)
		if err != nil {
			return err
		}
		journal.TerminalTaskResultKind = kind
	}
	journal.PendingTransitions = append(journal.PendingTransitions, prepared)
	journal.TerminalIntentDigest = intentDigest
	setTerminalPending(journal, pendingAt, prepared.State)
	return nil
}

func settleUnresolvedInputCommand(journal *RunJournal) error {
	intent := journal.InputCommandIntent
	if intent == nil || intent.Outcome != "" {
		return nil
	}
	intent.Outcome = "failed"
	return queueCommandAcknowledgement(journal, protocol.CommandAcknowledgement{RunID: journal.RunID, CommandID: intent.CommandID, Outcome: intent.Outcome, AckID: intent.AckID})
}

func queueCommandAcknowledgement(journal *RunJournal, acknowledgement protocol.CommandAcknowledgement) error {
	if !journal.hasClaimGrant() {
		return errors.New("journal has no claim grant")
	}
	if CommandAcknowledgementRetired(*journal) {
		return errors.New("command acknowledgement is no longer deliverable")
	}
	if acknowledgement.RunID == "" {
		acknowledgement.RunID = journal.RunID
	}
	if acknowledgement.RunID != journal.RunID || strings.TrimSpace(acknowledgement.CommandID) == "" || !validCommandAcknowledgementOutcome(acknowledgement.Outcome) || strings.TrimSpace(acknowledgement.AckID) == "" {
		return errors.New("command acknowledgement is invalid")
	}
	if isZeroFence(acknowledgement.Fence) {
		acknowledgement.Fence = journal.Fence()
	}
	if !sameFence(acknowledgement.Fence, journal.Fence()) {
		return errors.New("command acknowledgement fence does not match journal")
	}
	for _, pending := range journal.PendingCommandAcknowledgements {
		if pending.CommandID != acknowledgement.CommandID {
			continue
		}
		if pending.Outcome == acknowledgement.Outcome {
			return nil
		}
		return errors.New("command acknowledgement outcome conflicts with pending acknowledgement")
	}
	journal.PendingCommandAcknowledgements = append(journal.PendingCommandAcknowledgements, acknowledgement)
	return nil
}

func terminalSettlementDigest(journal RunJournal, transition protocol.StateTransitionRequest, acknowledgement protocol.CommandAcknowledgement) (string, error) {
	preparedTransition, err := prepareTransition(&journal, transition)
	if err != nil {
		return "", err
	}
	if acknowledgement.RunID == "" {
		acknowledgement.RunID = journal.RunID
	}
	if isZeroFence(acknowledgement.Fence) {
		acknowledgement.Fence = journal.Fence()
	}
	if acknowledgement.RunID != journal.RunID || strings.TrimSpace(acknowledgement.CommandID) == "" || !validCommandAcknowledgementOutcome(acknowledgement.Outcome) || strings.TrimSpace(acknowledgement.AckID) == "" || !sameFence(acknowledgement.Fence, journal.Fence()) {
		return "", errors.New("command acknowledgement is invalid")
	}
	encoded, err := serializeJSONWithoutHTMLEscaping(struct {
		Transition      protocol.StateTransitionRequest `json:"transition"`
		Acknowledgement protocol.CommandAcknowledgement `json:"acknowledgement"`
	}{Transition: preparedTransition, Acknowledgement: acknowledgement})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func terminalIntentDigest(journal RunJournal, stateName string, payload json.RawMessage) (string, error) {
	if !isTerminalTransitionState(stateName) || !validRawMessage(payload) {
		return "", errors.New("terminal intent is invalid")
	}
	encoded, err := serializeJSONWithoutHTMLEscaping(struct {
		Fence   protocol.Fence  `json:"fence"`
		State   string          `json:"state"`
		Payload json.RawMessage `json:"payload"`
	}{Fence: journal.Fence(), State: stateName, Payload: payload})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func terminalCommandDigest(journal RunJournal, acknowledgement protocol.CommandAcknowledgement) (string, error) {
	if acknowledgement.RunID == "" {
		acknowledgement.RunID = journal.RunID
	}
	if isZeroFence(acknowledgement.Fence) {
		acknowledgement.Fence = journal.Fence()
	}
	if acknowledgement.RunID != journal.RunID || !sameFence(acknowledgement.Fence, journal.Fence()) {
		return "", errors.New("terminal command acknowledgement does not match journal")
	}
	return terminalCommandIntentDigest(journal, acknowledgement.CommandID, acknowledgement.Outcome)
}

func terminalCommandIntentDigest(journal RunJournal, commandID, outcome string) (string, error) {
	if !validRequiredString(commandID, 4096) || !validCommandAcknowledgementOutcome(outcome) {
		return "", errors.New("terminal command intent is invalid")
	}
	encoded, err := serializeJSONWithoutHTMLEscaping(struct {
		Fence     protocol.Fence `json:"fence"`
		RunID     string         `json:"run_id"`
		CommandID string         `json:"command_id"`
		Outcome   string         `json:"outcome"`
	}{Fence: journal.Fence(), RunID: journal.RunID, CommandID: commandID, Outcome: outcome})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func validCommandAcknowledgementOutcome(outcome string) bool {
	switch outcome {
	case "applied", "rejected", "failed":
		return true
	default:
		return false
	}
}

func validInputCommandIntent(intent InputCommandIntent) bool {
	return validRequiredString(intent.CommandID, 4096) && len(intent.PayloadDigest) == sha256.Size*2 && validHex(intent.PayloadDigest) && validRequiredString(intent.RunningTransitionID, 4096) && validRequiredString(intent.AckID, 4096) && intent.EventSequenceBarrier >= 0 && validInputCommandOutcome(intent.Outcome) && (!intent.AcknowledgementDelivered || intent.Outcome != "")
}

func validInputCommandOutcome(outcome string) bool {
	switch outcome {
	case "", "applied", "failed":
		return true
	default:
		return false
	}
}

func isTerminalTransitionState(state string) bool {
	switch state {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func hasPendingTerminalTransition(transitions []protocol.StateTransitionRequest) bool {
	for _, transition := range transitions {
		if isTerminalTransitionState(transition.State) {
			return true
		}
	}
	return false
}

func pendingTerminalState(transitions []protocol.StateTransitionRequest) string {
	for _, transition := range transitions {
		if isTerminalTransitionState(transition.State) {
			return transition.State
		}
	}
	return ""
}

func validEvent(event protocol.RunEvent) bool {
	return validRequiredString(event.EventID, 4096) && event.Sequence > 0 && validRequiredString(event.Kind, 256) && !event.OccurredAt.IsZero() && validRawMessage(event.Payload)
}

func validTransition(transition protocol.StateTransitionRequest) bool {
	return validRequiredString(transition.TransitionID, 4096) && validRequiredString(transition.State, 256) && validRawMessage(transition.Payload)
}

func validAttemptedTransitions(attempted []string, pending []protocol.StateTransitionRequest) bool {
	pendingIDs := make(map[string]struct{}, len(pending))
	for _, transition := range pending {
		pendingIDs[transition.TransitionID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(attempted))
	for _, transitionID := range attempted {
		if !validRequiredString(transitionID, 4096) {
			return false
		}
		if _, exists := pendingIDs[transitionID]; !exists {
			return false
		}
		if _, duplicate := seen[transitionID]; duplicate {
			return false
		}
		seen[transitionID] = struct{}{}
	}
	return true
}

func validAcknowledgement(acknowledgement protocol.CommandAcknowledgement, runID string, fence protocol.Fence) bool {
	return acknowledgement.RunID == runID && validRequiredString(acknowledgement.CommandID, 4096) && validRequiredString(acknowledgement.Outcome, 256) && validRequiredString(acknowledgement.AckID, 4096) && sameFence(acknowledgement.Fence, fence)
}

func removeEvents(events []protocol.RunEvent, ids []string) []protocol.RunEvent {
	if len(ids) == 0 {
		return events
	}
	remove := makeStringSet(ids)
	result := events[:0]
	for _, event := range events {
		if !remove[event.EventID] {
			result = append(result, event)
		}
	}
	return result
}

func removeTransitions(transitions []protocol.StateTransitionRequest, ids []string) []protocol.StateTransitionRequest {
	if len(ids) == 0 {
		return transitions
	}
	remove := makeStringSet(ids)
	result := transitions[:0]
	for _, transition := range transitions {
		if !remove[transition.TransitionID] {
			result = append(result, transition)
		}
	}
	return result
}

func hasPendingTransition(transitions []protocol.StateTransitionRequest, transitionID string) bool {
	for _, transition := range transitions {
		if transition.TransitionID == transitionID {
			return true
		}
	}
	return false
}

func hasAttemptedTransition(attempted []string, transitionID string) bool {
	return slices.Contains(attempted, transitionID)
}

func retainAttemptedTransitions(attempted []string, pending []protocol.StateTransitionRequest) []string {
	if len(attempted) == 0 {
		return nil
	}
	result := attempted[:0]
	for _, transitionID := range attempted {
		if hasPendingTransition(pending, transitionID) {
			result = append(result, transitionID)
		}
	}
	return result
}

func removeAcknowledgements(acknowledgements []protocol.CommandAcknowledgement, ids []string) []protocol.CommandAcknowledgement {
	if len(ids) == 0 {
		return acknowledgements
	}
	remove := makeStringSet(ids)
	result := acknowledgements[:0]
	for _, acknowledgement := range acknowledgements {
		if !remove[acknowledgement.AckID] {
			result = append(result, acknowledgement)
		}
	}
	return result
}
