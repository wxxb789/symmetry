package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const (
	ProviderActionOutcomeSucceeded = "succeeded"
	ProviderActionOutcomeFailed    = "failed"
	ProviderActionOutcomeUnknown   = "unknown"
	providerActionFailureTerminal  = "run_terminal_unknown"

	// Provider actions may consume at most half of one journal's serialized
	// budget; the remaining bytes stay available for work, outbox, and cleanup
	// evidence. The exact candidate journal size remains the final guard.
	maxProviderActionBytes       = maxJournalFileBytes / 2
	maxProviderActionIDBytes     = 256
	maxProviderActionKeyBytes    = 256
	maxProviderResourceIDBytes   = 256
	maxProviderFailureCodeBytes  = 256
	maxProviderActionResultBytes = 64 << 10
)

var (
	ErrProviderActionConflict       = errors.New("provider action conflicts with journal")
	errProviderActionIntentNotFound = errors.New("provider action intent does not match journal")
	ErrProviderActionNotAllowed     = errors.New("provider action is not allowed in journal state")
	ErrProviderActionCapacity       = errors.New("provider action capacity is exhausted")
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
	if err := validateKey(key); err != nil {
		return RunJournal{}, false, err
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if err := store.ensureOpenLocked(); err != nil {
		return RunJournal{}, false, err
	}
	journal, err := store.loadJournalLocked(key)
	if err != nil {
		return RunJournal{}, false, err
	}
	for index := range journal.ProviderActionIntents {
		current := &journal.ProviderActionIntents[index]
		if current.ActionKey != intent.ActionKey && current.ActionID != intent.ActionID {
			continue
		}
		if !sameProviderActionIdentity(*current, intent) {
			return RunJournal{}, false, ErrProviderActionConflict
		}
		return journal, false, nil
	}
	if !journal.hasClaimGrant() || !providerActionDispatchAllowed(journal.LocalState) {
		return RunJournal{}, false, ErrProviderActionNotAllowed
	}
	candidate := journal
	candidate.ProviderActionIntents = append(append([]ProviderActionIntent(nil), journal.ProviderActionIntents...), cloneProviderActionIntent(intent))
	if err := validateProviderActionDispatchCapacity(candidate); err != nil {
		return RunJournal{}, false, err
	}
	if err := store.saveJournalLocked(candidate); err != nil {
		return RunJournal{}, false, err
	}
	return candidate, true, nil
}

func providerActionDispatchAllowed(localState string) bool {
	switch localState {
	case "claimed", "running", "waiting_for_input":
		return true
	default:
		return false
	}
}

// CompleteProviderAction records the exact terminal bridge response. Conflicting
// completion cannot replace an earlier durable outcome.
func (store *Store) CompleteProviderAction(key RunKey, completed ProviderActionIntent) (RunJournal, error) {
	if !validCompletedProviderAction(completed) {
		return RunJournal{}, errors.New("provider action completion is invalid")
	}
	return store.mutateJournalIfChanged(key, func(journal *RunJournal) (bool, error) {
		for index := range journal.ProviderActionIntents {
			current := &journal.ProviderActionIntents[index]
			if current.ActionID != completed.ActionID {
				continue
			}
			if !sameProviderActionIdentity(*current, completed) {
				return false, ErrProviderActionConflict
			}
			if current.Outcome != "" {
				if !sameProviderActionIntent(*current, completed) {
					return false, ErrProviderActionConflict
				}
				return false, nil
			}
			if completed.Outcome == ProviderActionOutcomeSucceeded || completed.Outcome == ProviderActionOutcomeUnknown && len(completed.Result) > 0 {
				if err := ValidateNewProviderActionResult(completed.Result); err != nil {
					return false, err
				}
			}
			*current = cloneProviderActionIntent(completed)
			if err := validateProviderActionJournalSize(*journal); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, errProviderActionIntentNotFound
	})
}

// RecoverProviderActions classifies every dispatch without a durable result as
// unknown. It never retries an external effect during daemon recovery.
func (store *Store) RecoverProviderActions(key RunKey, failureCode string) (RunJournal, error) {
	if !validRequiredString(failureCode, maxProviderFailureCodeBytes) {
		return RunJournal{}, errors.New("provider action recovery code is invalid")
	}
	return store.mutateJournalIfChanged(key, func(journal *RunJournal) (bool, error) {
		changed := settleUnresolvedProviderActions(journal, failureCode)
		if changed {
			if err := validateProviderActionJournalSize(*journal); err != nil {
				return false, err
			}
		}
		return changed, nil
	})
}

// RecoverProviderAction conservatively settles one possibly committed dispatch
// after a local persistence acknowledgement failure. A terminal outcome is
// immutable and therefore survives an exact recovery retry.
func (store *Store) RecoverProviderAction(key RunKey, actionID, requestDigest, failureCode string) (RunJournal, error) {
	if !validRequiredString(actionID, maxProviderActionIDBytes) || len(requestDigest) != sha256.Size*2 || !validHex(requestDigest) || !validRequiredString(failureCode, maxProviderFailureCodeBytes) {
		return RunJournal{}, errors.New("provider action recovery identity is invalid")
	}
	return store.mutateJournalIfChanged(key, func(journal *RunJournal) (bool, error) {
		for index := range journal.ProviderActionIntents {
			intent := &journal.ProviderActionIntents[index]
			if intent.ActionID != actionID {
				continue
			}
			if intent.RequestDigest != requestDigest {
				return false, ErrProviderActionConflict
			}
			if intent.Outcome == "" {
				intent.Outcome = ProviderActionOutcomeUnknown
				intent.Result = nil
				intent.FailureCode = failureCode
				if err := validateProviderActionJournalSize(*journal); err != nil {
					return false, err
				}
				return true, nil
			}
			return false, nil
		}
		return false, errProviderActionIntentNotFound
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
		providerActionResultsEqual(left.Result, right.Result) &&
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
	if len(result) == 0 {
		return false
	}
	normalized, err := normalizeProviderActionResult(result)
	return err == nil && len(normalized) <= maxProviderActionResultBytes
}

// ValidateNewProviderActionResult applies the admission limit for a new
// provider completion. Legacy journals are validated by semantic size above,
// but a new result must also fit in the raw representation emitted by the
// run-journal serializer. This keeps new liability bounded even when a result
// uses many equivalent JSON escape sequences.
func ValidateNewProviderActionResult(result json.RawMessage) error {
	if !validProviderActionResult(result) {
		return errors.New("provider action result is invalid")
	}
	persisted, err := serializeJSONWithoutHTMLEscaping(result)
	if err != nil || len(persisted) > maxProviderActionResultBytes {
		return ErrProviderActionCapacity
	}
	return nil
}

// normalizeProviderActionResult compacts a provider result and normalizes JSON
// string escapes without changing object member order or numeric lexical forms.
// It is used only for semantic validation and equality; the original raw
// representation remains durable so exact replay preserves legacy bytes.
func normalizeProviderActionResult(result json.RawMessage) (json.RawMessage, error) {
	if len(result) == 0 || !json.Valid(result) || protocol.RejectDuplicateJSONMembers(result) != nil {
		return nil, errors.New("provider action result is invalid JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(result))
	decoder.UseNumber()
	value, err := decodeProviderActionJSONValue(decoder)
	if err != nil || value.kind != providerActionJSONObject {
		return nil, errors.New("provider action result must be a JSON object")
	}
	if token, tokenErr := decoder.Token(); tokenErr != io.EOF {
		if tokenErr == nil || token != nil {
			return nil, errors.New("provider action result has trailing data")
		}
		return nil, errors.New("provider action result has trailing data")
	}

	var encoded bytes.Buffer
	if err := encodeProviderActionJSONValue(&encoded, value); err != nil {
		return nil, err
	}
	return json.RawMessage(encoded.Bytes()), nil
}

// NormalizeProviderActionResult validates and returns the compact semantic
// representation used by the durable provider-action boundary. Consumers that
// only need a safety/size check may discard the returned bytes and keep their
// original replay representation.
func NormalizeProviderActionResult(result json.RawMessage) (json.RawMessage, error) {
	return normalizeProviderActionResult(result)
}

const (
	providerActionJSONNull byte = iota
	providerActionJSONBool
	providerActionJSONNumber
	providerActionJSONString
	providerActionJSONArray
	providerActionJSONObject
)

type providerActionJSONValue struct {
	kind byte
	b    bool
	n    json.Number
	s    string
	a    []providerActionJSONValue
	o    []providerActionJSONObjectMember
}

type providerActionJSONObjectMember struct {
	key   string
	value providerActionJSONValue
}

func decodeProviderActionJSONValue(decoder *json.Decoder) (providerActionJSONValue, error) {
	token, err := decoder.Token()
	if err != nil {
		return providerActionJSONValue{}, err
	}
	switch value := token.(type) {
	case nil:
		return providerActionJSONValue{kind: providerActionJSONNull}, nil
	case bool:
		return providerActionJSONValue{kind: providerActionJSONBool, b: value}, nil
	case string:
		return providerActionJSONValue{kind: providerActionJSONString, s: value}, nil
	case json.Number:
		return providerActionJSONValue{kind: providerActionJSONNumber, n: value}, nil
	case json.Delim:
		switch value {
		case '[':
			var values []providerActionJSONValue
			for decoder.More() {
				item, err := decodeProviderActionJSONValue(decoder)
				if err != nil {
					return providerActionJSONValue{}, err
				}
				values = append(values, item)
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim(']') {
				return providerActionJSONValue{}, errors.New("provider action result array is invalid")
			}
			return providerActionJSONValue{kind: providerActionJSONArray, a: values}, nil
		case '{':
			var members []providerActionJSONObjectMember
			seen := make(map[string]struct{})
			for decoder.More() {
				keyToken, err := decoder.Token()
				key, ok := keyToken.(string)
				if err != nil || !ok {
					return providerActionJSONValue{}, errors.New("provider action result object key is invalid")
				}
				if _, exists := seen[key]; exists {
					return providerActionJSONValue{}, errors.New("provider action result has duplicate object member")
				}
				seen[key] = struct{}{}
				item, err := decodeProviderActionJSONValue(decoder)
				if err != nil {
					return providerActionJSONValue{}, err
				}
				members = append(members, providerActionJSONObjectMember{key: key, value: item})
			}
			end, err := decoder.Token()
			if err != nil || end != json.Delim('}') {
				return providerActionJSONValue{}, errors.New("provider action result object is invalid")
			}
			return providerActionJSONValue{kind: providerActionJSONObject, o: members}, nil
		default:
			return providerActionJSONValue{}, errors.New("provider action result delimiter is invalid")
		}
	default:
		return providerActionJSONValue{}, errors.New("provider action result value is invalid")
	}
}

func encodeProviderActionJSONValue(output *bytes.Buffer, value providerActionJSONValue) error {
	switch value.kind {
	case providerActionJSONNull:
		output.WriteString("null")
	case providerActionJSONBool:
		if value.b {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
	case providerActionJSONNumber:
		output.WriteString(value.n.String())
	case providerActionJSONString:
		encoded, err := encodeProviderActionJSONString(value.s)
		if err != nil {
			return err
		}
		output.Write(encoded)
	case providerActionJSONArray:
		output.WriteByte('[')
		for index, item := range value.a {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := encodeProviderActionJSONValue(output, item); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	case providerActionJSONObject:
		output.WriteByte('{')
		for index, member := range value.o {
			if index > 0 {
				output.WriteByte(',')
			}
			encoded, err := encodeProviderActionJSONString(member.key)
			if err != nil {
				return err
			}
			output.Write(encoded)
			output.WriteByte(':')
			if err := encodeProviderActionJSONValue(output, member.value); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	default:
		return errors.New("provider action result value kind is invalid")
	}
	return nil
}

func encodeProviderActionJSONString(value string) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return normalizeProviderActionLineSeparators(bytes.TrimSuffix(output.Bytes(), []byte{'\n'})), nil
}

// encoding/json escapes U+2028 and U+2029 even with HTML escaping disabled.
// Normalize complete escape tokens while preserving literal \u sequences.
func normalizeProviderActionLineSeparators(encoded []byte) []byte {
	output := make([]byte, 0, len(encoded))
	for index := 0; index < len(encoded); {
		if encoded[index] != '\\' {
			output = append(output, encoded[index])
			index++
			continue
		}
		if index+1 >= len(encoded) {
			output = append(output, encoded[index])
			index++
			continue
		}
		switch encoded[index+1] {
		case '\\', '"', '/', 'b', 'f', 'n', 'r', 't':
			output = append(output, encoded[index:index+2]...)
			index += 2
		case 'u':
			if index+6 > len(encoded) {
				output = append(output, encoded[index])
				index++
				continue
			}
			switch {
			case encoded[index+2] == '2' && encoded[index+3] == '0' && encoded[index+4] == '2' && encoded[index+5] == '8':
				output = append(output, 0xe2, 0x80, 0xa8)
			case encoded[index+2] == '2' && encoded[index+3] == '0' && encoded[index+4] == '2' && encoded[index+5] == '9':
				output = append(output, 0xe2, 0x80, 0xa9)
			default:
				output = append(output, encoded[index:index+6]...)
			}
			index += 6
		default:
			output = append(output, encoded[index])
			index++
		}
	}
	return output
}

func providerActionResultsEqual(left, right json.RawMessage) bool {
	if bytes.Equal(left, right) {
		return true
	}
	if len(left) == 0 || len(right) == 0 {
		return false
	}
	normalizedLeft, leftErr := normalizeProviderActionResult(left)
	normalizedRight, rightErr := normalizeProviderActionResult(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(normalizedLeft, normalizedRight)
}

func validateProviderActionIntents(journal RunJournal) error {
	if !journal.hasClaimGrant() && len(journal.ProviderActionIntents) != 0 {
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

// validateProviderActionDispatchCapacity bounds newly dispatched provider
// effects by both their serialized action bytes and the complete journal. An
// existing exact replay never reaches this check, so replay identity remains
// read-only even when the journal is already near its limit.
func validateProviderActionDispatchCapacity(journal RunJournal) error {
	providerData, err := serializeJSONWithoutHTMLEscaping(journal.ProviderActionIntents)
	if err != nil || len(providerData) > maxProviderActionBytes {
		return ErrProviderActionCapacity
	}

	// Reserve the complete serialized shape of the largest legal completion for
	// every unresolved intent. Completed provider data may already exceed the
	// provider-only budget, but a new dispatch must leave enough journal space to
	// classify every outstanding effect without making completion lossy.
	reserved := journal
	reserved.ProviderActionIntents = append([]ProviderActionIntent(nil), journal.ProviderActionIntents...)
	completionResult := maxProviderActionResult()
	completionFailureCode := maxProviderFailureCodeForCapacity()
	for index := range reserved.ProviderActionIntents {
		intent := &reserved.ProviderActionIntents[index]
		if intent.Outcome != "" {
			continue
		}
		intent.Outcome = ProviderActionOutcomeUnknown
		intent.Result = append(json.RawMessage(nil), completionResult...)
		intent.FailureCode = completionFailureCode
	}
	return validateProviderActionJournalSize(reserved)
}

// validateJournalSaveCapacity is the centralized lossless-mutation guard. A
// journal may contain completed provider data larger than the provider-only
// admission budget, but every unresolved dispatch must still have room for its
// maximum legal completion within the full 4 MiB journal.
func validateJournalSaveCapacity(journal RunJournal) error {
	data, err := serializeRunJournal(journal)
	if err != nil || len(data) > maxJournalFileBytes {
		return ErrProviderActionCapacity
	}
	if !hasUnresolvedProviderActions(journal) {
		return nil
	}

	reserved := journal
	reserved.ProviderActionIntents = append([]ProviderActionIntent(nil), journal.ProviderActionIntents...)
	completionResult := maxProviderActionResult()
	completionFailureCode := maxProviderFailureCodeForCapacity()
	for index := range reserved.ProviderActionIntents {
		intent := &reserved.ProviderActionIntents[index]
		if intent.Outcome != "" {
			continue
		}
		intent.Outcome = ProviderActionOutcomeUnknown
		intent.Result = append(json.RawMessage(nil), completionResult...)
		intent.FailureCode = completionFailureCode
	}
	reservedData, err := serializeRunJournal(reserved)
	if err != nil || len(reservedData) > maxJournalFileBytes {
		return ErrProviderActionCapacity
	}
	return nil
}

func maxProviderActionResult() json.RawMessage {
	const emptyResult = `{"payload":""}`
	return json.RawMessage(`{"payload":"` + strings.Repeat("x", maxProviderActionResultBytes-len(emptyResult)) + `"}`)
}

func maxProviderFailureCodeForCapacity() string {
	return strings.Repeat("\x00", maxProviderFailureCodeBytes)
}

// validateProviderActionJournalSize uses the same JSON representation and byte
// limit as the durable journal writer. It is also used for completion and
// recovery so an existing external effect can still be settled exactly while
// preserving the 4 MiB journal guard.
func validateProviderActionJournalSize(journal RunJournal) error {
	data, err := serializeRunJournal(journal)
	if err != nil || len(data) > maxJournalFileBytes {
		return ErrProviderActionCapacity
	}
	return nil
}

// hasUnresolvedProviderActions reports whether cleanup would discard a
// provider dispatch whose external outcome is not durably classified.
func hasUnresolvedProviderActions(journal RunJournal) bool {
	for _, intent := range journal.ProviderActionIntents {
		if intent.Outcome == "" {
			return true
		}
	}
	return false
}

func settleUnresolvedProviderActions(journal *RunJournal, failureCode string) bool {
	changed := false
	for index := range journal.ProviderActionIntents {
		intent := &journal.ProviderActionIntents[index]
		if intent.Outcome != "" {
			continue
		}
		intent.Outcome = ProviderActionOutcomeUnknown
		intent.Result = nil
		intent.FailureCode = failureCode
		changed = true
	}
	return changed
}
