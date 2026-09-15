package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	ProviderActionOutcomeSucceeded = "succeeded"
	ProviderActionOutcomeFailed    = "failed"
	ProviderActionOutcomeUnknown   = "unknown"
	providerActionFailureTerminal  = "run_terminal_unknown"

	// Keep worst-case retained results below half of the 4 MiB run-journal
	// limit, leaving room for work, events, transitions, and cleanup evidence.
	maxProviderActionIntents     = 32
	maxProviderActionIDBytes     = 256
	maxProviderActionKeyBytes    = 256
	maxProviderResourceIDBytes   = 256
	maxProviderFailureCodeBytes  = 256
	maxProviderActionResultBytes = 64 << 10
)

var (
	ErrProviderActionConflict = errors.New("provider action conflicts with journal")
	errProviderActionCapacity = errors.New("provider action capacity is exhausted")
)

// ProviderActionIntent is the credential-free local recovery record for one
// bridge action. RequestDigest binds the exact normalized request without
// persisting provider input in the run journal.
type ProviderActionIntent struct {
	ActionID      string          `json:"action_id"`
	ActionKey     string          `json:"action_key"`
	RequestDigest string          `json:"request_digest"`
	ResourceID    string          `json:"resource_id"`
	Operation     string          `json:"operation"`
	Outcome       string          `json:"outcome,omitempty"`
	Result        json.RawMessage `json:"result,omitempty"`
	FailureCode   string          `json:"failure_code,omitempty"`
}

// PrepareProviderAction commits action identity before dispatch. Every exact
// existing action replays without dispatch, including an unknown outcome. The
// current native bridge deliberately does not opt into Control's explicit
// unknown-action reconciliation until its lifecycle and fence are verified.
func (store *Store) PrepareProviderAction(key RunKey, intent ProviderActionIntent) (RunJournal, bool, error) {
	if !validPreparedProviderAction(intent) {
		return RunJournal{}, false, errors.New("provider action intent is invalid")
	}
	dispatch := false
	journal, err := store.mutateJournal(key, func(journal *RunJournal) error {
		if !journal.hasClaimGrant() || journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending" {
			return errors.New("provider action is not allowed in journal state")
		}
		for index := range journal.ProviderActionIntents {
			current := &journal.ProviderActionIntents[index]
			if current.ActionKey != intent.ActionKey && current.ActionID != intent.ActionID {
				continue
			}
			if !sameProviderActionIdentity(*current, intent) {
				return ErrProviderActionConflict
			}
			return nil
		}
		if len(journal.ProviderActionIntents) >= maxProviderActionIntents {
			return errProviderActionCapacity
		}
		journal.ProviderActionIntents = append(journal.ProviderActionIntents, cloneProviderActionIntent(intent))
		dispatch = true
		return nil
	})
	return journal, dispatch && err == nil, err
}

// CompleteProviderAction records the exact terminal bridge response. Conflicting
// completion cannot replace an earlier durable outcome.
func (store *Store) CompleteProviderAction(key RunKey, completed ProviderActionIntent) (RunJournal, error) {
	if !validCompletedProviderAction(completed) {
		return RunJournal{}, errors.New("provider action completion is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		for index := range journal.ProviderActionIntents {
			current := &journal.ProviderActionIntents[index]
			if current.ActionID != completed.ActionID {
				continue
			}
			if !sameProviderActionIdentity(*current, completed) {
				return ErrProviderActionConflict
			}
			if current.Outcome != "" {
				if !sameProviderActionIntent(*current, completed) {
					return ErrProviderActionConflict
				}
				return nil
			}
			*current = cloneProviderActionIntent(completed)
			return nil
		}
		return errors.New("provider action intent does not match journal")
	})
}

// RecoverProviderActions classifies every dispatch without a durable result as
// unknown. It never retries an external effect during daemon recovery.
func (store *Store) RecoverProviderActions(key RunKey, failureCode string) (RunJournal, error) {
	if !validRequiredString(failureCode, maxProviderFailureCodeBytes) {
		return RunJournal{}, errors.New("provider action recovery code is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		settleUnresolvedProviderActions(journal, failureCode)
		return nil
	})
}

// RecoverProviderAction conservatively settles one possibly committed dispatch
// after a local persistence acknowledgement failure. A terminal outcome is
// immutable and therefore survives an exact recovery retry.
func (store *Store) RecoverProviderAction(key RunKey, actionID, requestDigest, failureCode string) (RunJournal, error) {
	if !validRequiredString(actionID, maxProviderActionIDBytes) || len(requestDigest) != sha256.Size*2 || !validHex(requestDigest) || !validRequiredString(failureCode, maxProviderFailureCodeBytes) {
		return RunJournal{}, errors.New("provider action recovery identity is invalid")
	}
	return store.mutateJournal(key, func(journal *RunJournal) error {
		for index := range journal.ProviderActionIntents {
			intent := &journal.ProviderActionIntents[index]
			if intent.ActionID != actionID {
				continue
			}
			if intent.RequestDigest != requestDigest {
				return ErrProviderActionConflict
			}
			if intent.Outcome == "" {
				intent.Outcome = ProviderActionOutcomeUnknown
				intent.Result = nil
				intent.FailureCode = failureCode
			}
			return nil
		}
		return errors.New("provider action intent does not match journal")
	})
}

func cloneProviderActionIntent(intent ProviderActionIntent) ProviderActionIntent {
	intent.Result = append(json.RawMessage(nil), intent.Result...)
	return intent
}

func sameProviderActionIdentity(left, right ProviderActionIntent) bool {
	return left.ActionID == right.ActionID &&
		left.ActionKey == right.ActionKey &&
		left.RequestDigest == right.RequestDigest &&
		left.ResourceID == right.ResourceID &&
		left.Operation == right.Operation
}

func sameProviderActionIntent(left, right ProviderActionIntent) bool {
	return sameProviderActionIdentity(left, right) &&
		left.Outcome == right.Outcome &&
		bytes.Equal(left.Result, right.Result) &&
		left.FailureCode == right.FailureCode
}

func validPreparedProviderAction(intent ProviderActionIntent) bool {
	return validProviderActionIdentity(intent) && intent.Outcome == "" && len(intent.Result) == 0 && intent.FailureCode == ""
}

func validCompletedProviderAction(intent ProviderActionIntent) bool {
	if !validProviderActionIdentity(intent) {
		return false
	}
	switch intent.Outcome {
	case ProviderActionOutcomeSucceeded:
		return validProviderActionResult(intent.Result) && intent.FailureCode == ""
	case ProviderActionOutcomeFailed:
		return len(intent.Result) == 0 && validRequiredString(intent.FailureCode, maxProviderFailureCodeBytes)
	case ProviderActionOutcomeUnknown:
		return (len(intent.Result) == 0 || validProviderActionResult(intent.Result)) && validRequiredString(intent.FailureCode, maxProviderFailureCodeBytes)
	default:
		return false
	}
}

func validProviderActionIdentity(intent ProviderActionIntent) bool {
	return validRequiredString(intent.ActionID, maxProviderActionIDBytes) &&
		validRequiredString(intent.ActionKey, maxProviderActionKeyBytes) &&
		len(intent.RequestDigest) == sha256.Size*2 && validHex(intent.RequestDigest) &&
		validRequiredString(intent.ResourceID, maxProviderResourceIDBytes) &&
		validProviderActionOperation(intent.Operation)
}

func validProviderActionOperation(operation string) bool {
	switch protocol.ProviderOperation(operation) {
	case protocol.ProviderOperationResourceSync,
		protocol.ProviderOperationChangeUpsert,
		protocol.ProviderOperationChangeUpdate:
		return true
	default:
		return false
	}
}

func validProviderActionResult(result json.RawMessage) bool {
	if len(result) == 0 || len(result) > maxProviderActionResultBytes || !json.Valid(result) {
		return false
	}
	var object map[string]json.RawMessage
	return json.Unmarshal(result, &object) == nil && object != nil
}

func validateProviderActionIntents(journal RunJournal) error {
	if len(journal.ProviderActionIntents) > maxProviderActionIntents || (!journal.hasClaimGrant() && len(journal.ProviderActionIntents) != 0) {
		return errors.New("run journal provider action intents are invalid")
	}
	seenKeys := make(map[string]struct{}, len(journal.ProviderActionIntents))
	seenIDs := make(map[string]struct{}, len(journal.ProviderActionIntents))
	for _, intent := range journal.ProviderActionIntents {
		if !(validPreparedProviderAction(intent) || validCompletedProviderAction(intent)) {
			return errors.New("run journal provider action intent is invalid")
		}
		if _, exists := seenKeys[intent.ActionKey]; exists {
			return errors.New("run journal provider action key is duplicated")
		}
		if _, exists := seenIDs[intent.ActionID]; exists {
			return errors.New("run journal provider action ID is duplicated")
		}
		seenKeys[intent.ActionKey] = struct{}{}
		seenIDs[intent.ActionID] = struct{}{}
		if (journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending") && intent.Outcome == "" {
			return errors.New("terminal run journal has an unresolved provider action")
		}
	}
	return nil
}

func settleUnresolvedProviderActions(journal *RunJournal, failureCode string) {
	for index := range journal.ProviderActionIntents {
		intent := &journal.ProviderActionIntents[index]
		if intent.Outcome != "" {
			continue
		}
		intent.Outcome = ProviderActionOutcomeUnknown
		intent.Result = nil
		intent.FailureCode = failureCode
	}
}
