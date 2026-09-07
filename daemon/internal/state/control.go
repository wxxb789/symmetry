package state

import (
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

// ControlCommandIntent records a supervisory command before stdin delivery.
// Records survive acknowledgement delivery to prevent replaying side effects.
type ControlCommandIntent struct {
	CommandID                string `json:"command_id"`
	Kind                     string `json:"kind"`
	PayloadDigest            string `json:"payload_digest"`
	TransitionID             string `json:"transition_id,omitempty"`
	AckID                    string `json:"ack_id"`
	Outcome                  string `json:"outcome,omitempty"`
	AcknowledgementDelivered bool   `json:"acknowledgement_delivered,omitempty"`
}

// RetainWorkspace preserves artifacts even when the profile normally cleans up.
func (store *Store) RetainWorkspace(key RunKey) (RunJournal, error) {
	return store.mutateJournal(key, func(journal *RunJournal) error {
		journal.RetainWorkspace = true
		return nil
	})
}

// PrepareControlCommand persists at-most-once delivery intent. A replay with
// the same command ID, kind, and payload returns created=false in any state.
func (store *Store) PrepareControlCommand(key RunKey, intent ControlCommandIntent) (RunJournal, bool, error) {
	if !validControlCommandIntent(intent) || intent.Outcome != "" || intent.AcknowledgementDelivered {
		return RunJournal{}, false, errors.New("control command intent is invalid")
	}
	created := false
	journal, err := store.mutateJournal(key, func(journal *RunJournal) error {
		for _, current := range journal.ControlCommandIntents {
			if current.CommandID == intent.CommandID {
				if current.Kind != intent.Kind || current.PayloadDigest != intent.PayloadDigest {
					return errors.New("control command conflicts with journal")
				}
				return nil
			}
		}
		if !journal.hasClaimGrant() || !controlAllowed(intent.Kind, journal.LocalState) || hasPendingTerminalTransition(journal.PendingTransitions) {
			return errors.New("control command is not allowed in journal state")
		}
		if intent.Kind != "guidance" {
			for _, current := range journal.ControlCommandIntents {
				if current.Kind != "guidance" && current.Outcome == "" {
					return errors.New("a lifecycle control command is already pending")
				}
			}
		}
		journal.ControlCommandIntents = append(journal.ControlCommandIntents, intent)
		created = true
		return nil
	})
	return journal, created && err == nil, err
}

// CompleteControlCommand atomically persists a matching receipt, its lifecycle
// transition, and acknowledgement. A late receipt never overrides settlement.
// Event sequence numbers are assigned here under the journal lock.
func (store *Store) CompleteControlCommand(key RunKey, commandID, kind, outcome string, event protocol.RunEvent) (RunJournal, error) {
	if !validControlOutcome(outcome) || outcome == "" {
		return RunJournal{}, errors.New("control command outcome is invalid")
	}
	var receipt struct {
		CommandID string `json:"command_id"`
		Kind      string `json:"kind"`
		Outcome   string `json:"outcome"`
	}
	if event.Kind != "command_applied" || json.Unmarshal(event.Payload, &receipt) != nil || receipt.CommandID != commandID || receipt.Kind != kind || receipt.Outcome != outcome {
		return RunJournal{}, errors.New("control command receipt does not match command")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		var intent *ControlCommandIntent
		for index := range journal.ControlCommandIntents {
			if journal.ControlCommandIntents[index].CommandID == commandID {
				intent = &journal.ControlCommandIntents[index]
				break
			}
		}
		if intent == nil || intent.Kind != kind {
			return errors.New("control command intent does not match journal")
		}
		if intent.Outcome != "" {
			return nil
		}
		if outcome == "applied" && (!controlAllowed(kind, journal.LocalState) || hasPendingTerminalTransition(journal.PendingTransitions)) {
			outcome = "rejected"
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(event.Payload, &payload); err != nil {
				return errors.New("control command receipt is invalid")
			}
			payload["outcome"] = json.RawMessage(`"rejected"`)
			event.Payload, _ = json.Marshal(payload)
		}
		event.Sequence = journal.LastEventSequence + 1
		if err := appendEvent(journal, event, "control command receipt is invalid"); err != nil {
			return err
		}
		if outcome == "applied" && kind != "guidance" {
			localState := "paused"
			if kind == "resume" {
				localState = "running"
			}
			payload, _ := json.Marshal(map[string]string{"command_id": commandID})
			if err := queueTransition(journal, protocol.StateTransitionRequest{TransitionID: intent.TransitionID, State: localState, Payload: payload}); err != nil {
				return err
			}
			journal.LocalState = localState
		}
		intent.Outcome = outcome
		return queueCommandAcknowledgement(journal, protocol.CommandAcknowledgement{CommandID: intent.CommandID, Outcome: outcome, AckID: intent.AckID})
	})
}

func settleUnresolvedControlCommands(journal *RunJournal) error {
	for index := range journal.ControlCommandIntents {
		intent := &journal.ControlCommandIntents[index]
		if intent.Outcome != "" {
			continue
		}
		intent.Outcome = "rejected"
		if err := queueCommandAcknowledgement(journal, protocol.CommandAcknowledgement{CommandID: intent.CommandID, Outcome: intent.Outcome, AckID: intent.AckID}); err != nil {
			return err
		}
	}
	return nil
}

func controlAllowed(kind, localState string) bool {
	switch kind {
	case "guidance":
		return localState == "running" || localState == "paused"
	case "pause":
		return localState == "running"
	case "resume":
		return localState == "paused"
	default:
		return false
	}
}

func validControlOutcome(outcome string) bool {
	return outcome == "" || outcome == "applied" || outcome == "rejected" || outcome == "failed"
}

func validControlCommandIntent(intent ControlCommandIntent) bool {
	return validRequiredString(intent.CommandID, 4096) && (intent.Kind == "guidance" || intent.Kind == "pause" || intent.Kind == "resume") && len(intent.PayloadDigest) == sha256.Size*2 && validHex(intent.PayloadDigest) && (intent.Kind == "guidance" || validRequiredString(intent.TransitionID, 4096)) && len(intent.TransitionID) <= 4096 && validRequiredString(intent.AckID, 4096) && validControlOutcome(intent.Outcome) && (!intent.AcknowledgementDelivered || intent.Outcome != "")
}

func validateControlCommandIntents(journal RunJournal) error {
	seenCommands := make(map[string]bool)
	seenAcks := make(map[string]bool)
	seenTransitions := make(map[string]bool)
	if input := journal.InputCommandIntent; input != nil {
		seenCommands[input.CommandID] = true
		seenAcks[input.AckID] = true
		seenTransitions[input.RunningTransitionID] = true
	}
	pendingLifecycle := false
	for _, intent := range journal.ControlCommandIntents {
		if !journal.hasClaimGrant() || !validControlCommandIntent(intent) || seenCommands[intent.CommandID] || seenAcks[intent.AckID] || (intent.TransitionID != "" && seenTransitions[intent.TransitionID]) {
			return errors.New("run journal control command intent is invalid")
		}
		seenCommands[intent.CommandID], seenAcks[intent.AckID] = true, true
		if intent.TransitionID != "" {
			seenTransitions[intent.TransitionID] = true
		}
		if intent.Outcome == "" && intent.Kind != "guidance" {
			if pendingLifecycle {
				return errors.New("multiple lifecycle control commands are pending")
			}
			pendingLifecycle = true
		}
		pending := false
		for _, ack := range journal.PendingCommandAcknowledgements {
			if ack.CommandID == intent.CommandID || ack.AckID == intent.AckID {
				if ack.CommandID != intent.CommandID || ack.AckID != intent.AckID || ack.Outcome != intent.Outcome {
					return errors.New("control command acknowledgement does not match intent")
				}
				pending = true
			}
		}
		if (intent.Outcome == "" && pending) || (intent.Outcome != "" && intent.AcknowledgementDelivered == pending) {
			return errors.New("control command acknowledgement delivery is invalid")
		}
		if intent.Outcome == "" && (journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending") {
			return errors.New("terminal journal has an unresolved control command")
		}
		for _, event := range journal.PendingEvents {
			if event.Kind != "command_applied" {
				continue
			}
			var receipt struct {
				CommandID string `json:"command_id"`
				Kind      string `json:"kind"`
				Outcome   string `json:"outcome"`
			}
			if json.Unmarshal(event.Payload, &receipt) == nil && receipt.CommandID == intent.CommandID && (receipt.Kind != intent.Kind || receipt.Outcome != intent.Outcome || intent.Outcome == "") {
				return errors.New("control command receipt does not match intent")
			}
		}
		for _, transition := range journal.PendingTransitions {
			if intent.TransitionID == "" || transition.TransitionID != intent.TransitionID {
				continue
			}
			var payload struct {
				CommandID string `json:"command_id"`
			}
			expectedState := "paused"
			if intent.Kind == "resume" {
				expectedState = "running"
			}
			if intent.Kind == "guidance" || intent.Outcome != "applied" || transition.State != expectedState || json.Unmarshal(transition.Payload, &payload) != nil || payload.CommandID != intent.CommandID {
				return errors.New("control command transition does not match intent")
			}
		}
	}
	return nil
}
