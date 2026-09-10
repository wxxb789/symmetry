// Package pi contains private helpers for pi's documented --mode rpc transport.
// It deliberately does not expose a harness.Adapter until native lifecycle
// behavior is tested on supported platforms.
package pi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const defaultMaxRecordBytes = 1 << 20

var (
	ErrRecordTooLarge          = errors.New("pi rpc record exceeds limit")
	ErrIncompleteRecord        = errors.New("pi rpc stream ended with an incomplete record")
	ErrMalformedJSON           = errors.New("pi rpc record is not valid JSON")
	ErrInvalidUTF8             = errors.New("pi rpc record is not valid UTF-8")
	ErrNonObjectRecord         = errors.New("pi rpc record must be a JSON object")
	ErrDuplicateField          = errors.New("pi rpc record contains a duplicate field")
	ErrInvalidRecordType       = errors.New("pi rpc record type must be a non-empty string")
	ErrInvalidResponse         = errors.New("pi rpc response is invalid")
	ErrInvalidResponseID       = errors.New("pi rpc response id must be a non-empty string")
	ErrInvalidResponseCommand  = errors.New("pi rpc response command must be a non-empty string")
	ErrInvalidResponseSuccess  = errors.New("pi rpc response success must be a boolean")
	ErrResponseFailed          = errors.New("pi rpc command was rejected")
	ErrUnsupportedEvent        = errors.New("pi rpc event type is unsupported")
	ErrExtensionUIUnsupported  = errors.New("pi rpc extension UI request is unsupported")
	ErrInvalidEventPayload     = errors.New("pi rpc event payload is invalid")
	ErrInvalidRequest          = errors.New("pi rpc request is invalid")
	ErrDuplicateRequestID      = errors.New("pi rpc request id is already pending")
	ErrUnknownResponse         = errors.New("pi rpc response does not match a pending request")
	ErrResponseCommandMismatch = errors.New("pi rpc response command does not match its request")
	ErrDuplicateResponse       = errors.New("pi rpc response was already observed")
	ErrRequestTimedOut         = errors.New("pi rpc request timed out; late response is unsafe")
	ErrPendingResponse         = errors.New("pi rpc completion still has an unacknowledged request")
	ErrMissingSessionState     = errors.New("pi rpc completion has no observed session state")
	ErrConflictingSessionState = errors.New("pi rpc session state conflicts with the retained identity")
	ErrPromptNotAccepted       = errors.New("pi rpc prompt was not accepted")
	ErrUnexpectedAgentEvent    = errors.New("pi rpc agent event is not valid for the active operation")
	ErrDuplicateSettled        = errors.New("pi rpc stream contains more than one agent_settled event")
	ErrEventAfterSettled       = errors.New("pi rpc stream contains an event after agent_settled")
	ErrMissingAgentEnd         = errors.New("pi rpc stream settled without an agent_end event")
	ErrMissingAssistantMessage = errors.New("pi rpc stream settled without a complete assistant message")
	ErrUnexpectedAssistantStop = errors.New("pi rpc final assistant message did not stop normally")
	ErrNotSettled              = errors.New("pi rpc stream has not reached agent_settled")
	ErrInvalidTaskResult       = errors.New("pi task result must be one complete JSON object")
)

// Command is a documented pi RPC command name.
type Command string

const (
	CommandPrompt     Command = "prompt"
	CommandGetState   Command = "get_state"
	CommandClearQueue Command = "clear_queue"
	CommandAbort      Command = "abort"
)

// Request is a bounded command DTO. Requests produced here always include a
// non-empty id because responses otherwise cannot be safely correlated.
type Request struct {
	ID                string  `json:"id"`
	Type              Command `json:"type"`
	Message           string  `json:"message,omitempty"`
	StreamingBehavior string  `json:"streamingBehavior,omitempty"`
}

// GetStateRequest creates the documented get_state RPC command.
func GetStateRequest(id string) (Request, error) {
	return request(id, CommandGetState, "", "")
}

// PromptRequest creates the documented prompt RPC command. A successful
// response means only accepted, queued, or immediately handled; it is never a
// task-result or completion acknowledgement.
func PromptRequest(id, message, streamingBehavior string) (Request, error) {
	if strings.TrimSpace(message) == "" {
		return Request{}, fmt.Errorf("%w: prompt message must be non-empty", ErrInvalidRequest)
	}
	if streamingBehavior != "" && streamingBehavior != "steer" && streamingBehavior != "followUp" {
		return Request{}, fmt.Errorf("%w: unknown prompt streaming behavior %q", ErrInvalidRequest, streamingBehavior)
	}
	return request(id, CommandPrompt, message, streamingBehavior)
}

// ClearThenAbortRequests returns cancellation controls in pi's documented
// order. clear_queue prevents queued messages from continuing after abort.
func ClearThenAbortRequests(clearQueueID, abortID string) ([2]Request, error) {
	if strings.TrimSpace(clearQueueID) == strings.TrimSpace(abortID) && strings.TrimSpace(clearQueueID) != "" {
		return [2]Request{}, fmt.Errorf("%w: clear_queue and abort ids must differ", ErrInvalidRequest)
	}
	clear, err := request(clearQueueID, CommandClearQueue, "", "")
	if err != nil {
		return [2]Request{}, err
	}
	abort, err := request(abortID, CommandAbort, "", "")
	if err != nil {
		return [2]Request{}, err
	}
	return [2]Request{clear, abort}, nil
}

func request(id string, command Command, message, streamingBehavior string) (Request, error) {
	request := Request{ID: strings.TrimSpace(id), Type: command, Message: message, StreamingBehavior: streamingBehavior}
	if err := request.Validate(); err != nil {
		return Request{}, err
	}
	return request, nil
}

// Validate prevents uncorrelated or undocumented control output.
func (request Request) Validate() error {
	if strings.TrimSpace(request.ID) == "" {
		return fmt.Errorf("%w: id must be non-empty", ErrInvalidRequest)
	}
	switch request.Type {
	case CommandPrompt:
		if strings.TrimSpace(request.Message) == "" {
			return fmt.Errorf("%w: prompt message must be non-empty", ErrInvalidRequest)
		}
	case CommandGetState, CommandClearQueue, CommandAbort:
		if request.Message != "" || request.StreamingBehavior != "" {
			return fmt.Errorf("%w: command %q cannot include prompt fields", ErrInvalidRequest, request.Type)
		}
	default:
		return fmt.Errorf("%w: unsupported command %q", ErrInvalidRequest, request.Type)
	}
	return nil
}

// EventType names documented pi stdout events. Unknown event types remain
// errors: a future event must not silently acquire terminal semantics.
type EventType string

const (
	EventAgentStart                     EventType = "agent_start"
	EventAgentEnd                       EventType = "agent_end"
	EventAgentSettled                   EventType = "agent_settled"
	EventTurnStart                      EventType = "turn_start"
	EventTurnEnd                        EventType = "turn_end"
	EventMessageStart                   EventType = "message_start"
	EventMessageUpdate                  EventType = "message_update"
	EventMessageEnd                     EventType = "message_end"
	EventBashExecutionUpdate            EventType = "bash_execution_update"
	EventToolExecutionStart             EventType = "tool_execution_start"
	EventToolExecutionUpdate            EventType = "tool_execution_update"
	EventToolExecutionEnd               EventType = "tool_execution_end"
	EventQueueUpdate                    EventType = "queue_update"
	EventCompactionStart                EventType = "compaction_start"
	EventCompactionEnd                  EventType = "compaction_end"
	EventAutoRetryStart                 EventType = "auto_retry_start"
	EventAutoRetryEnd                   EventType = "auto_retry_end"
	EventSummarizationRetryScheduled    EventType = "summarization_retry_scheduled"
	EventSummarizationRetryAttemptStart EventType = "summarization_retry_attempt_start"
	EventSummarizationRetryFinished     EventType = "summarization_retry_finished"
	EventExtensionError                 EventType = "extension_error"
	EventExtensionUIRequest             EventType = "extension_ui_request"
)

// Response preserves one correlated command result. Data is deliberately raw
// because this private transport only needs get_state's documented identity.
type Response struct {
	ID      string
	Command Command
	Success bool
	Data    json.RawMessage
	Error   string
}

// Event preserves documented event payloads without treating text deltas or
// arbitrary message content as a Symmetry task result.
type Event struct {
	Type             EventType
	Message          json.RawMessage
	AgentEndMessages json.RawMessage
}

// Record is one complete bounded JSONL record. DecodeError is retained so a
// caller can drain output while the validator remains fail-closed.
type Record struct {
	Sequence    uint64
	Raw         json.RawMessage
	Response    *Response
	Event       *Event
	DecodeError error
}

// Decoder enforces pi's LF-only JSONL framing. A CR immediately before LF is
// accepted; Unicode line separators are ordinary JSON string content.
type Decoder struct {
	max      int
	buffer   []byte
	sequence uint64
	failed   error
}

// NewDecoder creates a bounded decoder. A non-positive size uses one MiB.
func NewDecoder(maxRecordBytes int) *Decoder {
	if maxRecordBytes <= 0 {
		maxRecordBytes = defaultMaxRecordBytes
	}
	return &Decoder{max: maxRecordBytes}
}

// Feed decodes complete records without ever accumulating an unbounded chunk.
func (decoder *Decoder) Feed(chunk []byte) ([]Record, error) {
	if decoder == nil {
		return nil, errors.New("pi rpc decoder is nil")
	}
	if decoder.failed != nil {
		return nil, decoder.failed
	}
	var records []Record
	for len(chunk) > 0 {
		lineEnd := bytes.IndexByte(chunk, '\n')
		if lineEnd < 0 {
			if len(decoder.buffer)+len(chunk) > decoder.max {
				return records, decoder.fail(ErrRecordTooLarge)
			}
			decoder.buffer = append(decoder.buffer, chunk...)
			break
		}
		recordBytes := len(decoder.buffer) + lineEnd
		if recordBytes > decoder.max+1 || (recordBytes > decoder.max && !hasTrailingCRBeforeLF(decoder.buffer, chunk, lineEnd)) {
			return records, decoder.fail(ErrRecordTooLarge)
		}
		decoder.buffer = append(decoder.buffer, chunk[:lineEnd]...)
		chunk = chunk[lineEnd+1:]
		line := decoder.buffer
		decoder.buffer = nil
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		decoder.sequence++
		records = append(records, decodeRecord(decoder.sequence, line))
	}
	return records, nil
}

// Close rejects a non-empty unterminated suffix, even when it is valid JSON.
func (decoder *Decoder) Close() ([]Record, error) {
	if decoder == nil {
		return nil, errors.New("pi rpc decoder is nil")
	}
	if decoder.failed != nil {
		return nil, decoder.failed
	}
	if len(bytes.TrimSpace(decoder.buffer)) == 0 {
		decoder.buffer = nil
		return nil, nil
	}
	decoder.sequence++
	raw := append([]byte(nil), decoder.buffer...)
	decoder.buffer = nil
	return []Record{{Sequence: decoder.sequence, Raw: raw, DecodeError: ErrIncompleteRecord}}, decoder.fail(ErrIncompleteRecord)
}

func (decoder *Decoder) fail(err error) error {
	decoder.buffer = nil
	decoder.failed = err
	return err
}

func hasTrailingCRBeforeLF(buffer, chunk []byte, lineEnd int) bool {
	if lineEnd > 0 {
		return chunk[lineEnd-1] == '\r'
	}
	return len(buffer) > 0 && buffer[len(buffer)-1] == '\r'
}

func decodeRecord(sequence uint64, line []byte) Record {
	raw := append(json.RawMessage(nil), bytes.TrimSpace(line)...)
	if !utf8.Valid(raw) {
		return invalidRecord(sequence, raw, ErrInvalidUTF8)
	}
	object, err := strictObject(raw)
	if err != nil {
		return invalidRecord(sequence, raw, err)
	}
	typeValue, ok := object["type"]
	if !ok {
		return invalidRecord(sequence, raw, ErrInvalidRecordType)
	}
	var typeName string
	if !nonEmptyString(typeValue, &typeName) {
		return invalidRecord(sequence, raw, ErrInvalidRecordType)
	}
	if typeName == "response" {
		response, err := decodeResponse(object)
		if err != nil {
			return invalidRecord(sequence, raw, err)
		}
		return Record{Sequence: sequence, Raw: raw, Response: response}
	}
	event, err := decodeEvent(EventType(typeName), object)
	if err != nil {
		return invalidRecord(sequence, raw, err)
	}
	return Record{Sequence: sequence, Raw: raw, Event: event}
}

func invalidRecord(sequence uint64, raw []byte, err error) Record {
	return Record{Sequence: sequence, Raw: append(json.RawMessage(nil), raw...), DecodeError: err}
}

func strictObject(raw []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return nil, ErrMalformedJSON
	}
	if err := rejectDuplicateFields(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, ErrMalformedJSON
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '{' {
		return nil, ErrNonObjectRecord
	}
	object := make(map[string]json.RawMessage)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, ErrMalformedJSON
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, ErrMalformedJSON
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, ErrMalformedJSON
		}
		object[key] = append(json.RawMessage(nil), value...)
	}
	token, err = decoder.Token()
	if err != nil {
		return nil, ErrMalformedJSON
	}
	delimiter, ok = token.(json.Delim)
	if !ok || delimiter != '}' {
		return nil, ErrMalformedJSON
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrMalformedJSON
	}
	return object, nil
}

func decodeResponse(object map[string]json.RawMessage) (*Response, error) {
	response := &Response{}
	id, ok := object["id"]
	if !ok || !nonEmptyString(id, &response.ID) {
		return nil, ErrInvalidResponseID
	}
	command, ok := object["command"]
	var commandName string
	if !ok || !nonEmptyString(command, &commandName) {
		return nil, ErrInvalidResponseCommand
	}
	response.Command = Command(commandName)
	success, ok := object["success"]
	if !ok || json.Unmarshal(success, &response.Success) != nil || bytes.Equal(bytes.TrimSpace(success), []byte("null")) {
		return nil, ErrInvalidResponseSuccess
	}
	if data, ok := object["data"]; ok {
		response.Data = append(json.RawMessage(nil), data...)
	}
	if errorValue, ok := object["error"]; ok {
		if !nonEmptyString(errorValue, &response.Error) {
			return nil, ErrInvalidResponse
		}
	}
	if !response.Success && response.Error == "" {
		return nil, ErrInvalidResponse
	}
	if response.Success && response.Error != "" {
		return nil, ErrInvalidResponse
	}
	return response, nil
}

func decodeEvent(eventType EventType, object map[string]json.RawMessage) (*Event, error) {
	if !supportedEvent(eventType) {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedEvent, eventType)
	}
	event := &Event{Type: eventType}
	if eventType == EventExtensionUIRequest {
		return nil, ErrExtensionUIUnsupported
	}
	if eventType == EventMessageEnd {
		message, ok := object["message"]
		if !ok || !isJSONObject(message) {
			return nil, fmt.Errorf("%w: message_end.message must be an object", ErrInvalidEventPayload)
		}
		event.Message = append(json.RawMessage(nil), message...)
	}
	if eventType == EventAgentEnd {
		messages, ok := object["messages"]
		if !ok || !isJSONArray(messages) {
			return nil, fmt.Errorf("%w: agent_end.messages must be an array", ErrInvalidEventPayload)
		}
		event.AgentEndMessages = append(json.RawMessage(nil), messages...)
	}
	return event, nil
}

func supportedEvent(eventType EventType) bool {
	switch eventType {
	case EventAgentStart, EventAgentEnd, EventAgentSettled, EventTurnStart, EventTurnEnd,
		EventMessageStart, EventMessageUpdate, EventMessageEnd, EventBashExecutionUpdate,
		EventToolExecutionStart, EventToolExecutionUpdate, EventToolExecutionEnd, EventQueueUpdate,
		EventCompactionStart, EventCompactionEnd, EventAutoRetryStart, EventAutoRetryEnd,
		EventSummarizationRetryScheduled, EventSummarizationRetryAttemptStart,
		EventSummarizationRetryFinished, EventExtensionError, EventExtensionUIRequest:
		return true
	default:
		return false
	}
}

func nonEmptyString(raw json.RawMessage, target *string) bool {
	return !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && json.Unmarshal(raw, target) == nil && strings.TrimSpace(*target) != ""
}

func isJSONObject(raw json.RawMessage) bool {
	object, err := strictObject(raw)
	return err == nil && object != nil
}

func isJSONArray(raw json.RawMessage) bool {
	if !utf8.Valid(raw) || !json.Valid(raw) || rejectDuplicateFields(raw) != nil {
		return false
	}
	var array []json.RawMessage
	return json.Unmarshal(raw, &array) == nil && array != nil
}

func rejectDuplicateFields(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := scanJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrMalformedJSON
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return ErrMalformedJSON
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		fields := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return ErrMalformedJSON
			}
			key, ok := keyToken.(string)
			if !ok {
				return ErrMalformedJSON
			}
			if _, exists := fields[key]; exists {
				return fmt.Errorf("%w: %q", ErrDuplicateField, key)
			}
			fields[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		token, err := decoder.Token()
		if err != nil {
			return ErrMalformedJSON
		}
		if closing, ok := token.(json.Delim); !ok || closing != '}' {
			return ErrMalformedJSON
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		token, err := decoder.Token()
		if err != nil {
			return ErrMalformedJSON
		}
		if closing, ok := token.(json.Delim); !ok || closing != ']' {
			return ErrMalformedJSON
		}
	default:
		return ErrMalformedJSON
	}
	return nil
}

// SessionState is the durable identity returned by get_state. This transport
// intentionally rejects --no-session state because it cannot support retained
// session recovery without both documented fields.
type SessionState struct {
	SessionID   string
	SessionFile string
}

func decodeSessionState(raw json.RawMessage) (SessionState, error) {
	object, err := strictObject(raw)
	if err != nil {
		return SessionState{}, fmt.Errorf("%w: state data: %v", ErrMissingSessionState, err)
	}
	state := SessionState{}
	if value, ok := object["sessionId"]; !ok || !nonEmptyString(value, &state.SessionID) {
		return SessionState{}, fmt.Errorf("%w: sessionId must be non-empty", ErrMissingSessionState)
	}
	if value, ok := object["sessionFile"]; !ok || !nonEmptyString(value, &state.SessionFile) {
		return SessionState{}, fmt.Errorf("%w: sessionFile must be non-empty", ErrMissingSessionState)
	}
	return state, nil
}

// NativeCompletion is a native-settlement observation, not Symmetry success.
// FinalAssistant and AgentEndMessages are the exact full native JSON boundaries
// supplied by pi; callers must explicitly select a semantic result boundary.
type NativeCompletion struct {
	State            SessionState
	FinalAssistant   json.RawMessage
	AgentEndMessages json.RawMessage
}

// Validator correlates requests and observes exactly one in-flight prompt on a
// single pi process stream. It is deliberately not safe for concurrent calls.
type Validator struct {
	expected         *SessionState
	state            *SessionState
	pending          map[string]Command
	responded        map[string]struct{}
	promptPending    bool
	promptAccepted   bool
	agentStarted     bool
	agentEnded       bool
	settled          bool
	currentAssistant json.RawMessage
	finalAssistant   json.RawMessage
	agentEndMessages json.RawMessage
	failed           error
}

// NewValidator binds expected state only when a previous durable session is
// known. An empty expected state allows a fresh observed state to establish it.
func NewValidator(expected *SessionState) *Validator {
	validator := &Validator{pending: make(map[string]Command), responded: make(map[string]struct{})}
	if expected != nil {
		copied := *expected
		validator.expected = &copied
	}
	return validator
}

// Register records a request before its bytes are written. A failed write must
// not be retried under the same validator because an unknown partial write has
// an unsafe late-response ambiguity.
func (validator *Validator) Register(request Request) error {
	if validator == nil {
		return errors.New("pi rpc validator is nil")
	}
	if validator.failed != nil {
		return validator.failed
	}
	if err := request.Validate(); err != nil {
		return validator.fail(err)
	}
	if _, exists := validator.pending[request.ID]; exists {
		return validator.fail(fmt.Errorf("%w: %q", ErrDuplicateRequestID, request.ID))
	}
	if _, exists := validator.responded[request.ID]; exists {
		return validator.fail(fmt.Errorf("%w: %q", ErrDuplicateRequestID, request.ID))
	}
	if request.Type == CommandPrompt {
		if validator.promptPending || validator.promptAccepted || validator.agentStarted || validator.settled {
			return validator.fail(fmt.Errorf("%w: only one prompt can be in flight", ErrInvalidRequest))
		}
		validator.promptPending = true
	}
	validator.pending[request.ID] = request.Type
	return nil
}

// Expire makes a request timeout terminal. A late response cannot prove that a
// subsequent request was accepted, so this validator cannot safely continue.
func (validator *Validator) Expire(id string) error {
	if validator == nil {
		return errors.New("pi rpc validator is nil")
	}
	if validator.failed != nil {
		return validator.failed
	}
	if _, exists := validator.pending[id]; !exists {
		return validator.fail(fmt.Errorf("%w: %q", ErrUnknownResponse, id))
	}
	return validator.fail(fmt.Errorf("%w: %q", ErrRequestTimedOut, id))
}

// Observe accepts records in stream order. The first violation remains terminal
// while callers continue draining process output for diagnostics.
func (validator *Validator) Observe(record Record) error {
	if validator == nil {
		return errors.New("pi rpc validator is nil")
	}
	if validator.failed != nil {
		return validator.failed
	}
	if record.DecodeError != nil {
		return validator.fail(record.DecodeError)
	}
	if record.Response != nil {
		return validator.observeResponse(*record.Response)
	}
	if record.Event != nil {
		return validator.observeEvent(*record.Event)
	}
	return validator.fail(ErrMalformedJSON)
}

func (validator *Validator) observeResponse(response Response) error {
	command, ok := validator.pending[response.ID]
	if !ok {
		if _, responded := validator.responded[response.ID]; responded {
			return validator.fail(fmt.Errorf("%w: %q", ErrDuplicateResponse, response.ID))
		}
		return validator.fail(fmt.Errorf("%w: %q", ErrUnknownResponse, response.ID))
	}
	if command != response.Command {
		return validator.fail(fmt.Errorf("%w: request %q expected %q, got %q", ErrResponseCommandMismatch, response.ID, command, response.Command))
	}
	delete(validator.pending, response.ID)
	validator.responded[response.ID] = struct{}{}
	if !response.Success {
		return validator.fail(fmt.Errorf("%w: %s", ErrResponseFailed, response.Error))
	}
	switch command {
	case CommandGetState:
		state, err := decodeSessionState(response.Data)
		if err != nil {
			return validator.fail(err)
		}
		if err := validator.bindState(state); err != nil {
			return validator.fail(err)
		}
	case CommandPrompt:
		validator.promptPending = false
		validator.promptAccepted = true
	}
	return nil
}

func (validator *Validator) observeEvent(event Event) error {
	if validator.settled {
		if event.Type == EventAgentSettled {
			return validator.fail(ErrDuplicateSettled)
		}
		return validator.fail(fmt.Errorf("%w: %s", ErrEventAfterSettled, event.Type))
	}
	switch event.Type {
	case EventAgentStart:
		if !validator.promptPending && !validator.promptAccepted {
			return validator.fail(fmt.Errorf("%w: agent_start before prompt", ErrUnexpectedAgentEvent))
		}
		if validator.agentStarted && !validator.agentEnded {
			return validator.fail(fmt.Errorf("%w: nested agent_start", ErrUnexpectedAgentEvent))
		}
		validator.agentStarted = true
		validator.agentEnded = false
		validator.currentAssistant = nil
		validator.finalAssistant = nil
	case EventAgentEnd:
		if !validator.agentStarted {
			return validator.fail(fmt.Errorf("%w: agent_end before agent_start", ErrUnexpectedAgentEvent))
		}
		validator.agentEnded = true
		validator.agentEndMessages = append(json.RawMessage(nil), event.AgentEndMessages...)
		validator.finalAssistant = append(json.RawMessage(nil), validator.currentAssistant...)
	case EventMessageEnd:
		if !validator.agentStarted || validator.agentEnded {
			return validator.fail(fmt.Errorf("%w: message_end outside an active agent run", ErrUnexpectedAgentEvent))
		}
		if role, assistant := assistantMessage(event.Message); assistant {
			_ = role
			validator.currentAssistant = append(json.RawMessage(nil), event.Message...)
		}
	case EventAgentSettled:
		if !validator.agentStarted {
			return validator.fail(fmt.Errorf("%w: agent_settled before agent_start", ErrUnexpectedAgentEvent))
		}
		validator.settled = true
	}
	return nil
}

func (validator *Validator) bindState(state SessionState) error {
	if validator.expected != nil && *validator.expected != state {
		return fmt.Errorf("%w: expected %q/%q, got %q/%q", ErrConflictingSessionState, validator.expected.SessionID, validator.expected.SessionFile, state.SessionID, state.SessionFile)
	}
	if validator.state != nil && *validator.state != state {
		return fmt.Errorf("%w: observed %q/%q, got %q/%q", ErrConflictingSessionState, validator.state.SessionID, validator.state.SessionFile, state.SessionID, state.SessionFile)
	}
	copied := state
	validator.state = &copied
	return nil
}

func assistantMessage(raw json.RawMessage) (string, bool) {
	object, err := strictObject(raw)
	if err != nil {
		return "", false
	}
	role, ok := object["role"]
	var roleName string
	if !ok || !nonEmptyString(role, &roleName) || roleName != "assistant" {
		return roleName, false
	}
	return roleName, true
}

// Finish returns only native settlement evidence. It never manufactures a
// Symmetry semantic result from a text delta, message text, or agent_end.
func (validator *Validator) Finish() (NativeCompletion, error) {
	if validator == nil {
		return NativeCompletion{}, errors.New("pi rpc validator is nil")
	}
	if validator.failed != nil {
		return NativeCompletion{}, validator.failed
	}
	if validator.state == nil {
		return NativeCompletion{}, ErrMissingSessionState
	}
	if !validator.promptAccepted {
		return NativeCompletion{}, ErrPromptNotAccepted
	}
	if !validator.settled {
		return NativeCompletion{}, ErrNotSettled
	}
	if len(validator.pending) != 0 {
		return NativeCompletion{}, ErrPendingResponse
	}
	if !validator.agentEnded {
		return NativeCompletion{}, ErrMissingAgentEnd
	}
	if len(validator.finalAssistant) == 0 {
		return NativeCompletion{}, ErrMissingAssistantMessage
	}
	if err := normalAssistantStop(validator.finalAssistant); err != nil {
		return NativeCompletion{}, err
	}
	return NativeCompletion{
		State:            *validator.state,
		FinalAssistant:   append(json.RawMessage(nil), validator.finalAssistant...),
		AgentEndMessages: append(json.RawMessage(nil), validator.agentEndMessages...),
	}, nil
}

func normalAssistantStop(raw json.RawMessage) error {
	object, err := strictObject(raw)
	if err != nil {
		return ErrUnexpectedAssistantStop
	}
	stopReason, ok := object["stopReason"]
	var reason string
	if !ok || !nonEmptyString(stopReason, &reason) || reason != "stop" {
		return fmt.Errorf("%w: %q", ErrUnexpectedAssistantStop, reason)
	}
	return nil
}

func (validator *Validator) fail(err error) error {
	validator.failed = err
	return err
}

// DecodeTaskResultJSON is the only semantic decoding entry point in this
// package. Callers must explicitly supply one complete raw JSON object from a
// chosen native boundary; this function never searches text, deltas, fences, or
// agent messages for embedded JSON.
func DecodeTaskResultJSON(raw json.RawMessage) (protocol.TaskResult, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || !utf8.Valid(trimmed) || !isJSONObject(trimmed) {
		return protocol.TaskResult{}, ErrInvalidTaskResult
	}
	result, err := protocol.ParseTaskResult(trimmed)
	if err != nil {
		return protocol.TaskResult{}, fmt.Errorf("%w: %v", ErrInvalidTaskResult, err)
	}
	return result, nil
}
