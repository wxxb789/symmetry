// Package claude contains private Claude Code stream-json transport helpers.
// It intentionally does not expose a harness.Adapter until credentialed native
// lifecycle evidence proves the required session and control semantics.
package claude

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"strings"
)

const defaultMaxRecordBytes = 1 << 20

var (
	ErrRecordTooLarge            = errors.New("Claude stream-json record exceeds limit")
	ErrIncompleteRecord          = errors.New("Claude stream-json stream ended with an incomplete record")
	ErrMalformedJSON             = errors.New("Claude stream-json record is not valid JSON")
	ErrNonObjectRecord           = errors.New("Claude stream-json record must be a JSON object")
	ErrInvalidEventType          = errors.New("Claude stream-json record type must be a non-empty string")
	ErrUnsupportedEvent          = errors.New("Claude stream-json event type is unsupported")
	ErrInvalidSessionID          = errors.New("Claude stream-json session_id must be a non-empty string")
	ErrInvalidResult             = errors.New("Claude stream-json result is invalid")
	ErrMissingResultPayload      = errors.New("Claude stream-json successful result must contain a non-empty result payload")
	ErrInvalidControlRequest     = errors.New("Claude stream-json control_request is invalid")
	ErrMissingResult             = errors.New("Claude stream-json stream ended without a result event")
	ErrMissingResultIdentity     = errors.New("Claude stream-json result did not establish a session identity")
	ErrConflictingIdentity       = errors.New("Claude stream-json event session identity conflicts")
	ErrRepeatedResult            = errors.New("Claude stream-json stream contains more than one result event")
	ErrEventAfterResult          = errors.New("Claude stream-json stream contains an event after the result event")
	ErrResultReportedError       = errors.New("Claude stream-json result reports an error")
	ErrUnexpectedResultSubtype   = errors.New("Claude stream-json result subtype is not a verified success")
	ErrNonSuccessTerminalReason  = errors.New("Claude stream-json result terminal_reason is not a verified success")
	ErrControlRequestUnsupported = errors.New("Claude stream-json control requests are unsupported")
)

// EventType is the small set of documented stream-json event envelopes
// recognized by this versioned private decoder.
type EventType string

const (
	EventAssistant      EventType = "assistant"
	EventUser           EventType = "user"
	EventSystem         EventType = "system"
	EventResult         EventType = "result"
	EventLog            EventType = "log"
	EventControlRequest EventType = "control_request"
)

// Event is a lossless envelope for one output record. Payload remains raw:
// field-level event semantics are intentionally not inferred before native
// lifecycle support is verified.
type Event struct {
	Sequence       uint64
	Type           EventType
	SessionID      string
	Subtype        string
	Raw            json.RawMessage
	Result         *Result
	ControlRequest *ControlRequest
	DecodeError    error
}

// Result carries only terminal fields required to reject unsafe promotion.
// Usage is deliberately raw and is not a claim of verified usage semantics.
type Result struct {
	Subtype        string
	TerminalReason string
	IsError        bool
	Output         json.RawMessage
	Usage          json.RawMessage
}

// ControlRequest makes native control prompts visible without treating them
// as approved. The validator always rejects these records for this transport.
type ControlRequest struct {
	RequestID string
	Request   json.RawMessage
}

// Decoder handles CRLF-compatible JSONL boundaries across arbitrary chunks.
type Decoder struct {
	max      int
	buffer   []byte
	sequence uint64
}

// NewDecoder creates a bounded stream-json decoder. A non-positive limit uses
// the conservative default of one MiB per raw record.
func NewDecoder(maxRecordBytes int) *Decoder {
	if maxRecordBytes <= 0 {
		maxRecordBytes = defaultMaxRecordBytes
	}
	return &Decoder{max: maxRecordBytes}
}

// Feed appends a native stdout chunk and returns each complete JSONL record.
// Wire-shape failures remain observable as Event.DecodeError so callers can
// journal them before TerminalValidator rejects terminal success.
func (decoder *Decoder) Feed(chunk []byte) ([]Event, error) {
	if decoder == nil {
		return nil, errors.New("Claude stream-json decoder is nil")
	}
	decoder.buffer = append(decoder.buffer, chunk...)
	var events []Event
	for {
		lineEnd := bytes.IndexByte(decoder.buffer, '\n')
		if lineEnd < 0 {
			if len(decoder.buffer) > decoder.max {
				decoder.buffer = nil
				return events, ErrRecordTooLarge
			}
			return events, nil
		}
		line := append([]byte(nil), decoder.buffer[:lineEnd]...)
		decoder.buffer = decoder.buffer[lineEnd+1:]
		if len(line) > decoder.max {
			return events, ErrRecordTooLarge
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		decoder.sequence++
		events = append(events, decodeEvent(decoder.sequence, line))
	}
}

// Close reports a non-whitespace partial record as an invalid event. Native
// process shutdown cannot silently turn a truncated terminal record into a
// successful result.
func (decoder *Decoder) Close() ([]Event, error) {
	if decoder == nil {
		return nil, errors.New("Claude stream-json decoder is nil")
	}
	if len(bytes.TrimSpace(decoder.buffer)) == 0 {
		decoder.buffer = nil
		return nil, nil
	}
	decoder.sequence++
	raw := append([]byte(nil), decoder.buffer...)
	decoder.buffer = nil
	return []Event{invalidEvent(decoder.sequence, raw, ErrIncompleteRecord)}, ErrIncompleteRecord
}

func decodeEvent(sequence uint64, line []byte) Event {
	raw := append(json.RawMessage(nil), bytes.TrimSpace(line)...)
	if !jsontext.Value(raw).IsValid() {
		return invalidEvent(sequence, raw, ErrMalformedJSON)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return invalidEvent(sequence, raw, ErrNonObjectRecord)
	}

	typeValue, ok := object["type"]
	if !ok {
		return invalidEvent(sequence, raw, ErrInvalidEventType)
	}
	var eventType string
	if err := json.Unmarshal(typeValue, &eventType); err != nil || strings.TrimSpace(eventType) == "" {
		return invalidEvent(sequence, raw, ErrInvalidEventType)
	}
	event := Event{Sequence: sequence, Type: EventType(eventType), Raw: raw}
	if !supportedEventType(event.Type) {
		event.DecodeError = fmt.Errorf("%w: %q", ErrUnsupportedEvent, eventType)
		return event
	}
	if value, exists := object["session_id"]; exists {
		if err := json.Unmarshal(value, &event.SessionID); err != nil || strings.TrimSpace(event.SessionID) == "" {
			event.DecodeError = ErrInvalidSessionID
			return event
		}
	}
	switch event.Type {
	case EventResult:
		event.Result = decodeResult(object)
		if event.Result == nil {
			event.DecodeError = ErrInvalidResult
			return event
		}
		event.Subtype = event.Result.Subtype
	case EventControlRequest:
		event.ControlRequest = decodeControlRequest(object)
		if event.ControlRequest == nil {
			event.DecodeError = ErrInvalidControlRequest
		}
	default:
		if value, exists := object["subtype"]; exists {
			if err := json.Unmarshal(value, &event.Subtype); err != nil {
				event.DecodeError = fmt.Errorf("%w: subtype must be a string", ErrInvalidEventType)
				return event
			}
		}
	}
	return event
}

func invalidEvent(sequence uint64, raw []byte, decodeError error) Event {
	return Event{Sequence: sequence, Raw: append(json.RawMessage(nil), raw...), DecodeError: decodeError}
}

func supportedEventType(eventType EventType) bool {
	switch eventType {
	case EventAssistant, EventUser, EventSystem, EventResult, EventLog, EventControlRequest:
		return true
	default:
		return false
	}
}

func decodeResult(object map[string]json.RawMessage) *Result {
	isError, ok := object["is_error"]
	if !ok {
		return nil
	}
	result := &Result{}
	switch string(bytes.TrimSpace(isError)) {
	case "true":
		result.IsError = true
	case "false":
		result.IsError = false
	default:
		return nil
	}
	if value, ok := object["subtype"]; ok {
		if !decodeJSONNonNullString(value, &result.Subtype) {
			return nil
		}
	}
	if value, ok := object["terminal_reason"]; ok {
		if !decodeJSONNonNullString(value, &result.TerminalReason) {
			return nil
		}
	}
	if value, ok := object["result"]; ok {
		result.Output = append(json.RawMessage(nil), value...)
	}
	if value, ok := object["usage"]; ok {
		result.Usage = append(json.RawMessage(nil), value...)
	}
	return result
}

func decodeJSONNonNullString(raw json.RawMessage, target *string) bool {
	return !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && json.Unmarshal(raw, target) == nil
}

func decodeControlRequest(object map[string]json.RawMessage) *ControlRequest {
	requestID, ok := object["request_id"]
	if !ok {
		return nil
	}
	control := &ControlRequest{}
	if err := json.Unmarshal(requestID, &control.RequestID); err != nil || strings.TrimSpace(control.RequestID) == "" {
		return nil
	}
	request, ok := object["request"]
	if !ok || !json.Valid(request) {
		return nil
	}
	control.Request = append(json.RawMessage(nil), request...)
	return control
}

// Terminal is the observed native terminal record. SessionID is observed from
// stream output; it is never synthesized from a launch or resume intent.
type Terminal struct {
	SessionID string
	Result    Result
}

// TerminalValidator binds a finite stream to one observed session identity and
// admits only a single verified-success-shaped result envelope. It does not
// claim that this proves a native session lifecycle.
type TerminalValidator struct {
	expectedSessionID string
	observedSessionID string
	result            *Result
	failed            error
}

// NewTerminalValidator creates a validator. expectedSessionID is optional and
// only compares against an identity emitted by native output; it does not
// constitute proof that a native session was created.
func NewTerminalValidator(expectedSessionID string) *TerminalValidator {
	return &TerminalValidator{expectedSessionID: strings.TrimSpace(expectedSessionID)}
}

// Observe accepts one decoded event. The first failure is retained so callers
// may continue draining stdout without losing the terminal safety decision.
func (validator *TerminalValidator) Observe(event Event) error {
	if validator == nil {
		return errors.New("Claude stream-json terminal validator is nil")
	}
	if validator.failed != nil {
		return validator.failed
	}
	if validator.result != nil {
		if event.Type == EventResult {
			validator.failed = ErrRepeatedResult
		} else {
			validator.failed = ErrEventAfterResult
		}
		return validator.failed
	}
	if event.DecodeError != nil {
		validator.failed = event.DecodeError
		return validator.failed
	}
	if event.SessionID != "" {
		if validator.expectedSessionID != "" && event.SessionID != validator.expectedSessionID {
			validator.failed = fmt.Errorf("%w: expected %q, got %q", ErrConflictingIdentity, validator.expectedSessionID, event.SessionID)
			return validator.failed
		}
		if validator.observedSessionID != "" && event.SessionID != validator.observedSessionID {
			validator.failed = fmt.Errorf("%w: observed %q, got %q", ErrConflictingIdentity, validator.observedSessionID, event.SessionID)
			return validator.failed
		}
		validator.observedSessionID = event.SessionID
	}
	if event.Type == EventControlRequest {
		validator.failed = ErrControlRequestUnsupported
		return validator.failed
	}
	if event.Type != EventResult {
		return nil
	}
	if event.Result == nil {
		validator.failed = ErrInvalidResult
		return validator.failed
	}
	if event.SessionID == "" {
		validator.failed = ErrMissingResultIdentity
		return validator.failed
	}
	if event.Result.IsError {
		validator.failed = ErrResultReportedError
		return validator.failed
	}
	if event.Result.Subtype != "success" {
		validator.failed = fmt.Errorf("%w: %q", ErrUnexpectedResultSubtype, event.Result.Subtype)
		return validator.failed
	}
	if event.Result.TerminalReason != "" && event.Result.TerminalReason != "success" && event.Result.TerminalReason != "completed" {
		validator.failed = fmt.Errorf("%w: %q", ErrNonSuccessTerminalReason, event.Result.TerminalReason)
		return validator.failed
	}
	if !isUsableResultPayload(event.Result.Output) {
		validator.failed = ErrMissingResultPayload
		return validator.failed
	}
	stored := *event.Result
	stored.Output = append(json.RawMessage(nil), event.Result.Output...)
	stored.Usage = append(json.RawMessage(nil), event.Result.Usage...)
	validator.result = &stored
	return nil
}

func isUsableResultPayload(raw json.RawMessage) bool {
	var output string
	return len(raw) > 0 && json.Unmarshal(raw, &output) == nil && strings.TrimSpace(output) != ""
}

// Finish returns the one observed terminal result after the caller has drained
// stdout and passed every event returned by Feed and Decoder.Close to Observe.
// Missing or failed results cannot be interpreted as success.
func (validator *TerminalValidator) Finish() (Terminal, error) {
	if validator == nil {
		return Terminal{}, errors.New("Claude stream-json terminal validator is nil")
	}
	if validator.failed != nil {
		return Terminal{}, validator.failed
	}
	if validator.result == nil {
		return Terminal{}, ErrMissingResult
	}
	if validator.observedSessionID == "" {
		return Terminal{}, ErrMissingResultIdentity
	}
	return Terminal{SessionID: validator.observedSessionID, Result: *validator.result}, nil
}
