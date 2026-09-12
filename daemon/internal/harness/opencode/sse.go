package opencode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Decoder incrementally parses the small SSE subset emitted by OpenCode. It
// accepts comments and CRLF framing, but rejects fields whose meaning this
// private transport has not verified.
type Decoder struct {
	max      int
	line     []byte
	data     [][]byte
	frameLen int
	sequence uint64
	failed   error
}

// NewDecoder creates a bounded SSE decoder. Non-positive limits use one MiB.
func NewDecoder(maxFrameBytes int) *Decoder {
	if maxFrameBytes <= 0 {
		maxFrameBytes = defaultMaxFrameBytes
	}
	return &Decoder{max: maxFrameBytes}
}

// Feed accepts arbitrary byte chunks and returns every complete data frame.
func (decoder *Decoder) Feed(chunk []byte) ([]Frame, error) {
	if decoder == nil {
		return nil, fmt.Errorf("opencode SSE decoder is nil")
	}
	if decoder.failed != nil {
		return nil, decoder.failed
	}
	var frames []Frame
	for len(chunk) > 0 {
		lineEnd := bytes.IndexByte(chunk, '\n')
		if lineEnd < 0 {
			if len(decoder.line)+len(chunk) > decoder.max {
				return frames, decoder.fail(ErrFrameTooLarge)
			}
			decoder.line = append(decoder.line, chunk...)
			break
		}
		if len(decoder.line)+lineEnd > decoder.max {
			return frames, decoder.fail(ErrFrameTooLarge)
		}
		decoder.line = append(decoder.line, chunk[:lineEnd]...)
		chunk = chunk[lineEnd+1:]
		line := decoder.line
		decoder.line = nil
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		decoded, err := decoder.consumeLine(line)
		if err != nil {
			return frames, decoder.fail(err)
		}
		frames = append(frames, decoded...)
	}
	return frames, nil
}

// Close rejects an unterminated line. A complete final data field is dispatched
// at EOF, matching SSE's end-of-stream behavior even when its final blank line
// was omitted by the peer.
func (decoder *Decoder) Close() ([]Frame, error) {
	if decoder == nil {
		return nil, fmt.Errorf("opencode SSE decoder is nil")
	}
	if decoder.failed != nil {
		return nil, decoder.failed
	}
	if len(decoder.line) > 0 {
		return nil, decoder.fail(ErrIncompleteFrame)
	}
	if len(decoder.data) > 0 {
		return decoder.finishFrame(), nil
	}
	return nil, nil
}

func (decoder *Decoder) consumeLine(line []byte) ([]Frame, error) {
	if len(line) == 0 {
		if len(decoder.data) == 0 {
			decoder.frameLen = 0
			return nil, nil
		}
		return decoder.finishFrame(), nil
	}
	if len(line) > decoder.max || decoder.frameLen+len(line)+1 > decoder.max {
		return nil, ErrFrameTooLarge
	}
	decoder.frameLen += len(line) + 1
	if line[0] == ':' {
		return nil, nil
	}
	field, value, found := bytes.Cut(line, []byte(":"))
	if !found || !bytes.Equal(field, []byte("data")) {
		return nil, ErrInvalidSSEField
	}
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	decoder.data = append(decoder.data, append([]byte(nil), value...))
	return nil, nil
}

func (decoder *Decoder) finishFrame() []Frame {
	data := bytes.Join(decoder.data, []byte("\n"))
	decoder.data = nil
	decoder.frameLen = 0
	decoder.sequence++
	return []Frame{{Sequence: decoder.sequence, Data: append(json.RawMessage(nil), data...)}}
}

func (decoder *Decoder) fail(err error) error {
	decoder.line = nil
	decoder.data = nil
	decoder.frameLen = 0
	decoder.failed = err
	return err
}

// DecodeGlobalEvent recognizes only OpenCode's documented connection marker.
// Other global event types have no approved Symmetry meaning yet.
func DecodeGlobalEvent(frame Frame) (Event, error) {
	event, object, err := decodeEvent(frame)
	if err != nil {
		return Event{}, err
	}
	if event.Type != "server.connected" || event.Durable != nil {
		return Event{}, fmt.Errorf("%w: global type %q", ErrUnsupportedEvent, event.Type)
	}
	data, err := strictEventObject(object["data"])
	if err != nil {
		return Event{}, err
	}
	if len(data) != 0 {
		return Event{}, fmt.Errorf("%w: server.connected data must be empty", ErrMalformedEvent)
	}
	return event, nil
}

// DecodePromptAdmittedEvent recognizes one durable session event captured from
// v1.18.30. It intentionally rejects any other event type. Use
// DecodeSessionEvent when replaying a stream that may contain later native
// event types.
func DecodePromptAdmittedEvent(frame Frame) (PromptAdmittedEvent, error) {
	event, _, err := decodeEvent(frame)
	if err != nil {
		return PromptAdmittedEvent{}, err
	}
	if event.Type != promptAdmittedEventType || event.Durable == nil {
		return PromptAdmittedEvent{}, fmt.Errorf("%w: session type %q", ErrUnsupportedEvent, event.Type)
	}
	decoded, err := DecodeSessionEvent(frame)
	if err != nil {
		return PromptAdmittedEvent{}, err
	}
	if decoded.Kind != SessionEventPromptAdmitted || decoded.PromptAdmitted == nil {
		return PromptAdmittedEvent{}, fmt.Errorf("%w: session type %q", ErrUnsupportedEvent, decoded.Type)
	}
	return *decoded.PromptAdmitted, nil
}

const (
	promptAdmittedEventType = "session.next.prompt.admitted"
	stepStartedEventType    = "session.next.step.started"
	stepEndedEventType      = "session.next.step.ended"
	stepFailedEventType     = "session.next.step.failed"
	textEndedEventType      = "session.next.text.ended"
	toolCalledEventType     = "session.next.tool.called"
	toolSuccessEventType    = "session.next.tool.success"
	toolFailedEventType     = "session.next.tool.failed"
	retriedEventType        = "session.next.retried"
)

// DecodeSessionEvent strictly decodes the durable session-event subset pinned
// to OpenCode 1.18.30. Unknown event types are returned as Kind
// SessionEventUnknown after their envelope, durable cursor, and session
// identity have been validated. This allows a watcher to emit a bounded
// diagnostic and continue to a later terminal event.
func DecodeSessionEvent(frame Frame) (DecodedSessionEvent, error) {
	event, object, err := decodeEvent(frame)
	if err != nil {
		return DecodedSessionEvent{}, err
	}
	if event.Durable == nil {
		return DecodedSessionEvent{}, fmt.Errorf("%w: session event %q has no durable cursor", ErrInvalidDurableCursor, event.Type)
	}
	if !strings.HasPrefix(event.Type, "session.") {
		return DecodedSessionEvent{}, fmt.Errorf("%w: session type %q", ErrUnsupportedEvent, event.Type)
	}
	data, err := strictEventObject(object["data"])
	if err != nil {
		return DecodedSessionEvent{}, err
	}
	sessionID, ok := requiredString(data, "sessionID")
	if !ok || !isID(sessionID, "ses_") {
		return DecodedSessionEvent{}, fmt.Errorf("%w: event sessionID", ErrMalformedEvent)
	}
	if event.Durable.AggregateID != sessionID {
		return DecodedSessionEvent{}, fmt.Errorf("%w: durable aggregate %q, data session %q", ErrIdentityMismatch, event.Durable.AggregateID, sessionID)
	}

	decoded := DecodedSessionEvent{Event: event, Kind: SessionEventUnknown, SessionID: sessionID}
	if expected, known := knownDurableEventVersion(event.Type); known {
		if event.Durable.Version != expected {
			return DecodedSessionEvent{}, fmt.Errorf("%w: %s expects version %d, got %d", ErrInvalidEventVersion, event.Type, expected, event.Durable.Version)
		}
	}

	switch event.Type {
	case promptAdmittedEventType:
		value, err := decodePromptAdmitted(event, data)
		if err != nil {
			return DecodedSessionEvent{}, err
		}
		decoded.Kind, decoded.PromptAdmitted = SessionEventPromptAdmitted, &value
	case stepStartedEventType:
		value, err := decodeStepStarted(event, data)
		if err != nil {
			return DecodedSessionEvent{}, err
		}
		decoded.Kind, decoded.StepStarted = SessionEventStepStarted, &value
	case stepEndedEventType:
		value, err := decodeStepEnded(event, data)
		if err != nil {
			return DecodedSessionEvent{}, err
		}
		decoded.Kind, decoded.StepEnded = SessionEventStepEnded, &value
	case stepFailedEventType:
		value, err := decodeStepFailed(event, data)
		if err != nil {
			return DecodedSessionEvent{}, err
		}
		decoded.Kind, decoded.StepFailed = SessionEventStepFailed, &value
	case textEndedEventType:
		value, err := decodeTextEnded(event, data)
		if err != nil {
			return DecodedSessionEvent{}, err
		}
		decoded.Kind, decoded.TextEnded = SessionEventTextEnded, &value
	case toolCalledEventType:
		value, err := decodeToolCalled(event, data)
		if err != nil {
			return DecodedSessionEvent{}, err
		}
		decoded.Kind, decoded.ToolCalled = SessionEventToolCalled, &value
	case toolSuccessEventType:
		value, err := decodeToolSuccess(event, data)
		if err != nil {
			return DecodedSessionEvent{}, err
		}
		decoded.Kind, decoded.ToolSuccess = SessionEventToolSuccess, &value
	case toolFailedEventType:
		value, err := decodeToolFailed(event, data)
		if err != nil {
			return DecodedSessionEvent{}, err
		}
		decoded.Kind, decoded.ToolFailed = SessionEventToolFailed, &value
	case retriedEventType:
		value, err := decodeRetried(event, data)
		if err != nil {
			return DecodedSessionEvent{}, err
		}
		decoded.Kind, decoded.Retried = SessionEventRetried, &value
	default:
		// Unknown future events must still be valid JSON with no duplicate keys.
		// Their fields are intentionally not interpreted.
		if err := validateJSONValue(event.Data); err != nil {
			return DecodedSessionEvent{}, fmt.Errorf("%w: unknown event data: %v", ErrMalformedEvent, err)
		}
	}
	return decoded, nil
}

func decodeEvent(frame Frame) (Event, map[string]json.RawMessage, error) {
	object, err := strictObject(frame.Data)
	if err != nil {
		return Event{}, nil, fmt.Errorf("%w: %v", ErrMalformedEvent, err)
	}
	if err := rejectUnknownFields(object, "id", "type", "data", "durable", "location", "metadata"); err != nil {
		return Event{}, nil, err
	}
	event := Event{}
	var ok bool
	if event.ID, ok = requiredString(object, "id"); !ok || !isID(event.ID, "evt_") {
		return Event{}, nil, ErrMalformedEvent
	}
	if event.Type, ok = requiredString(object, "type"); !ok {
		return Event{}, nil, ErrMalformedEvent
	}
	if raw, exists := object["data"]; !exists || !isJSONObject(raw) {
		return Event{}, nil, ErrMalformedEvent
	}
	if raw, exists := object["metadata"]; exists {
		metadata, err := strictEventObject(raw)
		if err != nil {
			return Event{}, nil, fmt.Errorf("%w: metadata must be an object", ErrMalformedEvent)
		}
		for _, value := range metadata {
			if err := validateJSONValue(value); err != nil {
				return Event{}, nil, fmt.Errorf("%w: metadata", ErrMalformedEvent)
			}
		}
	}
	if raw, exists := object["location"]; exists {
		location, err := strictEventObject(raw)
		if err != nil {
			return Event{}, nil, err
		}
		if err := rejectUnknownFields(location, "directory", "workspaceID"); err != nil {
			return Event{}, nil, err
		}
		if directory, ok := requiredString(location, "directory"); !ok || strings.TrimSpace(directory) == "" {
			return Event{}, nil, fmt.Errorf("%w: location directory", ErrMalformedEvent)
		}
		if _, err := optionalString(location, "workspaceID"); err != nil {
			return Event{}, nil, err
		}
	}
	if raw, exists := object["durable"]; exists {
		durable, err := decodeDurableCursor(raw)
		if err != nil {
			return Event{}, nil, err
		}
		event.Durable = &durable
	}
	event.Data = append(json.RawMessage(nil), object["data"]...)
	return event, object, nil
}

func decodeDurableCursor(raw json.RawMessage) (DurableCursor, error) {
	object, err := strictObject(raw)
	if err != nil {
		return DurableCursor{}, ErrInvalidDurableCursor
	}
	if err := rejectUnknownFields(object, "aggregateID", "seq", "version"); err != nil {
		return DurableCursor{}, fmt.Errorf("%w: %v", ErrInvalidDurableCursor, err)
	}
	cursor := DurableCursor{}
	var ok bool
	if cursor.AggregateID, ok = requiredString(object, "aggregateID"); !ok || !isID(cursor.AggregateID, "ses_") {
		return DurableCursor{}, ErrInvalidDurableCursor
	}
	if cursor.Seq, ok = requiredPositiveInt(object, "seq"); !ok {
		return DurableCursor{}, ErrInvalidDurableCursor
	}
	if cursor.Version, ok = requiredPositiveInt(object, "version"); !ok {
		return DurableCursor{}, ErrInvalidDurableCursor
	}
	return cursor, nil
}

func knownDurableEventVersion(eventType string) (uint64, bool) {
	switch eventType {
	case promptAdmittedEventType, stepStartedEventType, textEndedEventType, toolCalledEventType, toolSuccessEventType, toolFailedEventType, retriedEventType:
		return 1, true
	case stepEndedEventType, stepFailedEventType:
		return 2, true
	default:
		return 0, false
	}
}

func strictEventObject(raw json.RawMessage) (map[string]json.RawMessage, error) {
	object, err := strictObject(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: event object: %v", ErrMalformedEvent, err)
	}
	return object, nil
}

func requiredStringAllowEmpty(object map[string]json.RawMessage, name string) (string, bool) {
	raw, ok := object[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func requiredNonNegativeInt(object map[string]json.RawMessage, name string) (uint64, bool) {
	raw, ok := object[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, false
	}
	var value uint64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, false
	}
	return value, true
}

func rejectUnknownFields(object map[string]json.RawMessage, allowed ...string) error {
	set := make(map[string]struct{}, len(allowed))
	for _, name := range allowed {
		set[name] = struct{}{}
	}
	for name := range object {
		if _, ok := set[name]; !ok {
			return fmt.Errorf("%w: unknown field %q", ErrMalformedEvent, name)
		}
	}
	return nil
}

func optionalString(object map[string]json.RawMessage, name string) (*string, error) {
	raw, ok := object[name]
	if !ok {
		return nil, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("%w: field %q cannot be null", ErrMalformedEvent, name)
	}
	value, ok := requiredString(object, name)
	if !ok {
		return nil, fmt.Errorf("%w: field %q must be a non-empty string", ErrMalformedEvent, name)
	}
	return &value, nil
}

func requiredBool(object map[string]json.RawMessage, name string) (bool, bool) {
	raw, ok := object[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return false, false
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		return false, false
	}
	return value, true
}

func requiredFiniteNumber(object map[string]json.RawMessage, name string) (float64, bool) {
	raw, ok := object[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return 0, false
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value json.Number
	if err := decoder.Decode(&value); err != nil {
		return 0, false
	}
	if _, err := decoder.Token(); err == nil {
		return 0, false
	}
	number, err := value.Float64()
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return number, true
}

func optionalFiniteNumber(object map[string]json.RawMessage, name string) (*float64, error) {
	if _, ok := object[name]; !ok {
		return nil, nil
	}
	value, ok := requiredFiniteNumber(object, name)
	if !ok {
		return nil, fmt.Errorf("%w: field %q must be finite", ErrMalformedEvent, name)
	}
	return &value, nil
}

func requiredStringArray(object map[string]json.RawMessage, name string) ([]string, bool) {
	raw, ok := object[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, false
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, false
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		if bytes.Equal(bytes.TrimSpace(item), []byte("null")) {
			return nil, false
		}
		var value string
		if err := json.Unmarshal(item, &value); err != nil {
			return nil, false
		}
		result = append(result, value)
	}
	return result, true
}

func optionalStringArray(object map[string]json.RawMessage, name string) ([]string, error) {
	if _, ok := object[name]; !ok {
		return nil, nil
	}
	values, ok := requiredStringArray(object, name)
	if !ok {
		return nil, fmt.Errorf("%w: field %q must be an array of strings", ErrMalformedEvent, name)
	}
	return values, nil
}

func requiredRawObject(object map[string]json.RawMessage, name string) (map[string]json.RawMessage, bool) {
	raw, ok := object[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, false
	}
	value, err := strictObject(raw)
	if err != nil {
		return nil, false
	}
	for _, child := range value {
		if err := validateJSONValue(child); err != nil {
			return nil, false
		}
	}
	return value, true
}

func requiredRawValue(object map[string]json.RawMessage, name string) (json.RawMessage, bool) {
	raw, ok := object[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, false
	}
	if err := validateJSONValue(raw); err != nil {
		return nil, false
	}
	return append(json.RawMessage(nil), raw...), true
}

func optionalRawValue(object map[string]json.RawMessage, name string) (json.RawMessage, error) {
	raw, ok := object[name]
	if !ok {
		return nil, nil
	}
	if err := validateJSONValue(raw); err != nil {
		return nil, fmt.Errorf("%w: field %q must contain valid JSON", ErrMalformedEvent, name)
	}
	return append(json.RawMessage(nil), raw...), nil
}

// validateJSONValue is used for source fields whose values are intentionally
// opaque. Unlike json.Unmarshal into a map, it still rejects duplicate object
// keys recursively.
func validateJSONValue(raw json.RawMessage) error {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || !json.Valid(trimmed) {
		return ErrMalformedEvent
	}
	switch trimmed[0] {
	case '{':
		object, err := strictObject(trimmed)
		if err != nil {
			return err
		}
		for _, child := range object {
			if err := validateJSONValue(child); err != nil {
				return err
			}
		}
	case '[':
		var values []json.RawMessage
		if err := json.Unmarshal(trimmed, &values); err != nil {
			return err
		}
		for _, child := range values {
			if err := validateJSONValue(child); err != nil {
				return err
			}
		}
	}
	return nil
}

func decodePromptAdmitted(event Event, data map[string]json.RawMessage) (PromptAdmittedEvent, error) {
	if err := rejectUnknownFields(data, "timestamp", "sessionID", "messageID", "prompt", "delivery"); err != nil {
		return PromptAdmittedEvent{}, err
	}
	decoded := PromptAdmittedEvent{Event: event}
	var ok bool
	if decoded.Timestamp, ok = requiredPositiveInt64(data, "timestamp"); !ok {
		return PromptAdmittedEvent{}, fmt.Errorf("%w: timestamp", ErrMalformedEvent)
	}
	if decoded.SessionID, ok = requiredString(data, "sessionID"); !ok || !isID(decoded.SessionID, "ses_") {
		return PromptAdmittedEvent{}, fmt.Errorf("%w: sessionID", ErrMalformedEvent)
	}
	if decoded.MessageID, ok = requiredString(data, "messageID"); !ok || !isID(decoded.MessageID, "msg_") {
		return PromptAdmittedEvent{}, fmt.Errorf("%w: messageID", ErrMalformedEvent)
	}
	if decoded.Delivery, ok = requiredString(data, "delivery"); !ok || (decoded.Delivery != "steer" && decoded.Delivery != "queue") {
		return PromptAdmittedEvent{}, fmt.Errorf("%w: delivery", ErrMalformedEvent)
	}
	prompt, err := strictEventObject(data["prompt"])
	if err != nil {
		return PromptAdmittedEvent{}, err
	}
	if err := rejectUnknownFields(prompt, "text", "files", "agents"); err != nil {
		return PromptAdmittedEvent{}, err
	}
	if decoded.Text, ok = requiredString(prompt, "text"); !ok {
		return PromptAdmittedEvent{}, fmt.Errorf("%w: prompt.text", ErrMalformedEvent)
	}
	if err := validatePromptAttachments(prompt); err != nil {
		return PromptAdmittedEvent{}, err
	}
	return decoded, nil
}

func validatePromptAttachments(prompt map[string]json.RawMessage) error {
	for _, name := range []string{"files", "agents"} {
		raw, ok := prompt[name]
		if !ok {
			continue
		}
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("%w: prompt.%s must be an array", ErrMalformedEvent, name)
		}
		var items []json.RawMessage
		if err := json.Unmarshal(raw, &items); err != nil {
			return fmt.Errorf("%w: prompt.%s", ErrMalformedEvent, name)
		}
		for _, item := range items {
			value, err := strictEventObject(item)
			if err != nil {
				return err
			}
			if name == "files" {
				if err := rejectUnknownFields(value, "uri", "mime", "name", "description", "source"); err != nil {
					return err
				}
				for _, field := range []string{"uri", "mime"} {
					if _, ok := requiredString(value, field); !ok {
						return fmt.Errorf("%w: prompt.files.%s", ErrMalformedEvent, field)
					}
				}
				for _, field := range []string{"name", "description"} {
					if _, err := optionalString(value, field); err != nil {
						return err
					}
				}
			} else {
				if err := rejectUnknownFields(value, "name", "source"); err != nil {
					return err
				}
				if _, ok := requiredString(value, "name"); !ok {
					return fmt.Errorf("%w: prompt.agents.name", ErrMalformedEvent)
				}
			}
			if source, ok := value["source"]; ok {
				sourceObject, err := strictEventObject(source)
				if err != nil {
					return err
				}
				if err := rejectUnknownFields(sourceObject, "start", "end", "text"); err != nil {
					return err
				}
				if _, ok := requiredNonNegativeInt(sourceObject, "start"); !ok {
					return fmt.Errorf("%w: source.start", ErrMalformedEvent)
				}
				if _, ok := requiredNonNegativeInt(sourceObject, "end"); !ok {
					return fmt.Errorf("%w: source.end", ErrMalformedEvent)
				}
				if _, ok := requiredString(sourceObject, "text"); !ok {
					return fmt.Errorf("%w: source.text", ErrMalformedEvent)
				}
			}
		}
	}
	return nil
}

func decodeModelRef(raw json.RawMessage) (NativeModelRef, error) {
	object, err := strictEventObject(raw)
	if err != nil {
		return NativeModelRef{}, err
	}
	if err := rejectUnknownFields(object, "id", "providerID", "variant"); err != nil {
		return NativeModelRef{}, err
	}
	id, ok := requiredString(object, "id")
	if !ok {
		return NativeModelRef{}, fmt.Errorf("%w: model.id", ErrMalformedEvent)
	}
	providerID, ok := requiredString(object, "providerID")
	if !ok {
		return NativeModelRef{}, fmt.Errorf("%w: model.providerID", ErrMalformedEvent)
	}
	variant, err := optionalString(object, "variant")
	if err != nil {
		return NativeModelRef{}, err
	}
	return NativeModelRef{ID: id, ProviderID: providerID, Variant: variant}, nil
}

func decodeStepStarted(event Event, data map[string]json.RawMessage) (NativeStepStartedEvent, error) {
	if err := rejectUnknownFields(data, "timestamp", "sessionID", "assistantMessageID", "agent", "model", "snapshot"); err != nil {
		return NativeStepStartedEvent{}, err
	}
	decoded := NativeStepStartedEvent{Event: event}
	var ok bool
	if decoded.Timestamp, ok = requiredPositiveInt64(data, "timestamp"); !ok {
		return NativeStepStartedEvent{}, fmt.Errorf("%w: timestamp", ErrMalformedEvent)
	}
	if decoded.SessionID, ok = requiredString(data, "sessionID"); !ok || !isID(decoded.SessionID, "ses_") {
		return NativeStepStartedEvent{}, fmt.Errorf("%w: sessionID", ErrMalformedEvent)
	}
	if decoded.AssistantMessageID, ok = requiredString(data, "assistantMessageID"); !ok || !isID(decoded.AssistantMessageID, "msg_") {
		return NativeStepStartedEvent{}, fmt.Errorf("%w: assistantMessageID", ErrMalformedEvent)
	}
	if decoded.Agent, ok = requiredString(data, "agent"); !ok {
		return NativeStepStartedEvent{}, fmt.Errorf("%w: agent", ErrMalformedEvent)
	}
	modelRaw, exists := data["model"]
	if !exists {
		return NativeStepStartedEvent{}, fmt.Errorf("%w: model", ErrMalformedEvent)
	}
	model, err := decodeModelRef(modelRaw)
	if err != nil {
		return NativeStepStartedEvent{}, err
	}
	decoded.Model = model
	decoded.Snapshot, err = optionalString(data, "snapshot")
	if err != nil {
		return NativeStepStartedEvent{}, err
	}
	return decoded, nil
}

func decodeTokenUsage(raw json.RawMessage) (NativeTokenUsage, error) {
	object, err := strictEventObject(raw)
	if err != nil {
		return NativeTokenUsage{}, err
	}
	if err := rejectUnknownFields(object, "input", "output", "reasoning", "cache"); err != nil {
		return NativeTokenUsage{}, err
	}
	input, ok := requiredFiniteNumber(object, "input")
	if !ok {
		return NativeTokenUsage{}, fmt.Errorf("%w: tokens.input", ErrMalformedEvent)
	}
	output, ok := requiredFiniteNumber(object, "output")
	if !ok {
		return NativeTokenUsage{}, fmt.Errorf("%w: tokens.output", ErrMalformedEvent)
	}
	reasoning, ok := requiredFiniteNumber(object, "reasoning")
	if !ok {
		return NativeTokenUsage{}, fmt.Errorf("%w: tokens.reasoning", ErrMalformedEvent)
	}
	cacheRaw, exists := object["cache"]
	if !exists {
		return NativeTokenUsage{}, fmt.Errorf("%w: tokens.cache", ErrMalformedEvent)
	}
	cacheObject, err := strictEventObject(cacheRaw)
	if err != nil {
		return NativeTokenUsage{}, err
	}
	if err := rejectUnknownFields(cacheObject, "read", "write"); err != nil {
		return NativeTokenUsage{}, err
	}
	read, ok := requiredFiniteNumber(cacheObject, "read")
	if !ok {
		return NativeTokenUsage{}, fmt.Errorf("%w: tokens.cache.read", ErrMalformedEvent)
	}
	write, ok := requiredFiniteNumber(cacheObject, "write")
	if !ok {
		return NativeTokenUsage{}, fmt.Errorf("%w: tokens.cache.write", ErrMalformedEvent)
	}
	return NativeTokenUsage{Input: input, Output: output, Reasoning: reasoning, Cache: NativeCacheUsage{Read: read, Write: write}}, nil
}

func decodeStepEnded(event Event, data map[string]json.RawMessage) (NativeStepEndedEvent, error) {
	if err := rejectUnknownFields(data, "timestamp", "sessionID", "assistantMessageID", "finish", "cost", "tokens", "snapshot", "files"); err != nil {
		return NativeStepEndedEvent{}, err
	}
	decoded := NativeStepEndedEvent{Event: event}
	var ok bool
	if decoded.Timestamp, ok = requiredPositiveInt64(data, "timestamp"); !ok {
		return NativeStepEndedEvent{}, fmt.Errorf("%w: timestamp", ErrMalformedEvent)
	}
	if decoded.SessionID, ok = requiredString(data, "sessionID"); !ok || !isID(decoded.SessionID, "ses_") {
		return NativeStepEndedEvent{}, fmt.Errorf("%w: sessionID", ErrMalformedEvent)
	}
	if decoded.AssistantMessageID, ok = requiredString(data, "assistantMessageID"); !ok || !isID(decoded.AssistantMessageID, "msg_") {
		return NativeStepEndedEvent{}, fmt.Errorf("%w: assistantMessageID", ErrMalformedEvent)
	}
	if decoded.Finish, ok = requiredString(data, "finish"); !ok {
		return NativeStepEndedEvent{}, fmt.Errorf("%w: finish", ErrMalformedEvent)
	}
	if decoded.Cost, ok = requiredFiniteNumber(data, "cost"); !ok {
		return NativeStepEndedEvent{}, fmt.Errorf("%w: cost", ErrMalformedEvent)
	}
	tokensRaw, exists := data["tokens"]
	if !exists {
		return NativeStepEndedEvent{}, fmt.Errorf("%w: tokens", ErrMalformedEvent)
	}
	tokens, err := decodeTokenUsage(tokensRaw)
	if err != nil {
		return NativeStepEndedEvent{}, err
	}
	decoded.Tokens = tokens
	decoded.Snapshot, err = optionalString(data, "snapshot")
	if err != nil {
		return NativeStepEndedEvent{}, err
	}
	decoded.Files, err = optionalStringArray(data, "files")
	if err != nil {
		return NativeStepEndedEvent{}, err
	}
	return decoded, nil
}

func decodeUnknownError(raw json.RawMessage) (NativeUnknownError, error) {
	object, err := strictEventObject(raw)
	if err != nil {
		return NativeUnknownError{}, err
	}
	if err := rejectUnknownFields(object, "type", "message"); err != nil {
		return NativeUnknownError{}, err
	}
	errorType, ok := requiredString(object, "type")
	if !ok || errorType != "unknown" {
		return NativeUnknownError{}, fmt.Errorf("%w: error.type", ErrMalformedEvent)
	}
	message, ok := requiredString(object, "message")
	if !ok {
		return NativeUnknownError{}, fmt.Errorf("%w: error.message", ErrMalformedEvent)
	}
	return NativeUnknownError{Type: errorType, Message: message}, nil
}

func decodeStepFailed(event Event, data map[string]json.RawMessage) (NativeStepFailedEvent, error) {
	if err := rejectUnknownFields(data, "timestamp", "sessionID", "assistantMessageID", "error"); err != nil {
		return NativeStepFailedEvent{}, err
	}
	decoded := NativeStepFailedEvent{Event: event}
	var ok bool
	if decoded.Timestamp, ok = requiredPositiveInt64(data, "timestamp"); !ok {
		return NativeStepFailedEvent{}, fmt.Errorf("%w: timestamp", ErrMalformedEvent)
	}
	if decoded.SessionID, ok = requiredString(data, "sessionID"); !ok || !isID(decoded.SessionID, "ses_") {
		return NativeStepFailedEvent{}, fmt.Errorf("%w: sessionID", ErrMalformedEvent)
	}
	if decoded.AssistantMessageID, ok = requiredString(data, "assistantMessageID"); !ok || !isID(decoded.AssistantMessageID, "msg_") {
		return NativeStepFailedEvent{}, fmt.Errorf("%w: assistantMessageID", ErrMalformedEvent)
	}
	errorRaw, exists := data["error"]
	if !exists {
		return NativeStepFailedEvent{}, fmt.Errorf("%w: error", ErrMalformedEvent)
	}
	nativeError, err := decodeUnknownError(errorRaw)
	if err != nil {
		return NativeStepFailedEvent{}, err
	}
	decoded.Error = nativeError
	return decoded, nil
}

func decodeTextEnded(event Event, data map[string]json.RawMessage) (NativeTextEndedEvent, error) {
	if err := rejectUnknownFields(data, "timestamp", "sessionID", "assistantMessageID", "textID", "text"); err != nil {
		return NativeTextEndedEvent{}, err
	}
	decoded := NativeTextEndedEvent{Event: event}
	var ok bool
	if decoded.Timestamp, ok = requiredPositiveInt64(data, "timestamp"); !ok {
		return NativeTextEndedEvent{}, fmt.Errorf("%w: timestamp", ErrMalformedEvent)
	}
	if decoded.SessionID, ok = requiredString(data, "sessionID"); !ok || !isID(decoded.SessionID, "ses_") {
		return NativeTextEndedEvent{}, fmt.Errorf("%w: sessionID", ErrMalformedEvent)
	}
	if decoded.AssistantMessageID, ok = requiredString(data, "assistantMessageID"); !ok || !isID(decoded.AssistantMessageID, "msg_") {
		return NativeTextEndedEvent{}, fmt.Errorf("%w: assistantMessageID", ErrMalformedEvent)
	}
	if decoded.TextID, ok = requiredString(data, "textID"); !ok {
		return NativeTextEndedEvent{}, fmt.Errorf("%w: textID", ErrMalformedEvent)
	}
	if decoded.Text, ok = requiredStringAllowEmpty(data, "text"); !ok {
		return NativeTextEndedEvent{}, fmt.Errorf("%w: text", ErrMalformedEvent)
	}
	return decoded, nil
}

func decodeProviderExecution(raw json.RawMessage) (NativeProviderExecution, error) {
	object, err := strictEventObject(raw)
	if err != nil {
		return NativeProviderExecution{}, err
	}
	if err := rejectUnknownFields(object, "executed", "metadata"); err != nil {
		return NativeProviderExecution{}, err
	}
	executed, ok := requiredBool(object, "executed")
	if !ok {
		return NativeProviderExecution{}, fmt.Errorf("%w: provider.executed", ErrMalformedEvent)
	}
	metadata, err := decodeProviderMetadata(object, "metadata")
	if err != nil {
		return NativeProviderExecution{}, err
	}
	return NativeProviderExecution{Executed: executed, Metadata: metadata}, nil
}

func decodeProviderMetadata(object map[string]json.RawMessage, name string) (map[string]map[string]json.RawMessage, error) {
	raw, exists := object[name]
	if !exists {
		return nil, nil
	}
	metadata, err := strictEventObject(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: provider.%s", ErrMalformedEvent, name)
	}
	result := make(map[string]map[string]json.RawMessage, len(metadata))
	for provider, providerRaw := range metadata {
		values, err := strictEventObject(providerRaw)
		if err != nil {
			return nil, fmt.Errorf("%w: provider.%s.%s", ErrMalformedEvent, name, provider)
		}
		for _, value := range values {
			if err := validateJSONValue(value); err != nil {
				return nil, fmt.Errorf("%w: provider metadata", ErrMalformedEvent)
			}
		}
		result[provider] = values
	}
	return result, nil
}

func decodeToolBase(data map[string]json.RawMessage) (sessionID, assistantMessageID, callID string, timestamp int64, err error) {
	var ok bool
	if timestamp, ok = requiredPositiveInt64(data, "timestamp"); !ok {
		return "", "", "", 0, fmt.Errorf("%w: timestamp", ErrMalformedEvent)
	}
	if sessionID, ok = requiredString(data, "sessionID"); !ok || !isID(sessionID, "ses_") {
		return "", "", "", 0, fmt.Errorf("%w: sessionID", ErrMalformedEvent)
	}
	if assistantMessageID, ok = requiredString(data, "assistantMessageID"); !ok || !isID(assistantMessageID, "msg_") {
		return "", "", "", 0, fmt.Errorf("%w: assistantMessageID", ErrMalformedEvent)
	}
	if callID, ok = requiredString(data, "callID"); !ok {
		return "", "", "", 0, fmt.Errorf("%w: callID", ErrMalformedEvent)
	}
	return sessionID, assistantMessageID, callID, timestamp, nil
}

func decodeToolCalled(event Event, data map[string]json.RawMessage) (NativeToolCalledEvent, error) {
	if err := rejectUnknownFields(data, "timestamp", "sessionID", "assistantMessageID", "callID", "tool", "input", "provider"); err != nil {
		return NativeToolCalledEvent{}, err
	}
	sessionID, assistantMessageID, callID, timestamp, err := decodeToolBase(data)
	if err != nil {
		return NativeToolCalledEvent{}, err
	}
	tool, ok := requiredString(data, "tool")
	if !ok {
		return NativeToolCalledEvent{}, fmt.Errorf("%w: tool", ErrMalformedEvent)
	}
	input, ok := requiredRawObject(data, "input")
	if !ok {
		return NativeToolCalledEvent{}, fmt.Errorf("%w: input", ErrMalformedEvent)
	}
	providerRaw, exists := data["provider"]
	if !exists {
		return NativeToolCalledEvent{}, fmt.Errorf("%w: provider", ErrMalformedEvent)
	}
	provider, err := decodeProviderExecution(providerRaw)
	if err != nil {
		return NativeToolCalledEvent{}, err
	}
	return NativeToolCalledEvent{Event: event, SessionID: sessionID, AssistantMessageID: assistantMessageID, CallID: callID, Tool: tool, Input: input, Provider: provider, Timestamp: timestamp}, nil
}

func decodeToolContent(raw json.RawMessage) ([]json.RawMessage, error) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, fmt.Errorf("%w: content array", ErrMalformedEvent)
	}
	var values []json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, fmt.Errorf("%w: content array", ErrMalformedEvent)
	}
	result := make([]json.RawMessage, 0, len(values))
	for _, item := range values {
		object, err := strictEventObject(item)
		if err != nil {
			return nil, err
		}
		typeName, ok := requiredString(object, "type")
		if !ok {
			return nil, fmt.Errorf("%w: content.type", ErrMalformedEvent)
		}
		switch typeName {
		case "text":
			if err := rejectUnknownFields(object, "type", "text"); err != nil {
				return nil, err
			}
			if _, ok := requiredString(object, "text"); !ok {
				return nil, fmt.Errorf("%w: content.text", ErrMalformedEvent)
			}
		case "file":
			if err := rejectUnknownFields(object, "type", "uri", "mime", "name"); err != nil {
				return nil, err
			}
			for _, field := range []string{"uri", "mime"} {
				if _, ok := requiredString(object, field); !ok {
					return nil, fmt.Errorf("%w: content.%s", ErrMalformedEvent, field)
				}
			}
			if _, err := optionalString(object, "name"); err != nil {
				return nil, err
			}
		default:
			return nil, fmt.Errorf("%w: unsupported content type %q", ErrMalformedEvent, typeName)
		}
		result = append(result, append(json.RawMessage(nil), item...))
	}
	return result, nil
}

func decodeToolSuccess(event Event, data map[string]json.RawMessage) (NativeToolSuccessEvent, error) {
	if err := rejectUnknownFields(data, "timestamp", "sessionID", "assistantMessageID", "callID", "structured", "content", "outputPaths", "result", "provider"); err != nil {
		return NativeToolSuccessEvent{}, err
	}
	sessionID, assistantMessageID, callID, timestamp, err := decodeToolBase(data)
	if err != nil {
		return NativeToolSuccessEvent{}, err
	}
	structured, ok := requiredRawObject(data, "structured")
	if !ok {
		return NativeToolSuccessEvent{}, fmt.Errorf("%w: structured", ErrMalformedEvent)
	}
	contentRaw, exists := data["content"]
	if !exists {
		return NativeToolSuccessEvent{}, fmt.Errorf("%w: content", ErrMalformedEvent)
	}
	content, err := decodeToolContent(contentRaw)
	if err != nil {
		return NativeToolSuccessEvent{}, err
	}
	outputPaths, err := optionalStringArray(data, "outputPaths")
	if err != nil {
		return NativeToolSuccessEvent{}, err
	}
	result, err := optionalRawValue(data, "result")
	if err != nil {
		return NativeToolSuccessEvent{}, err
	}
	providerRaw, exists := data["provider"]
	if !exists {
		return NativeToolSuccessEvent{}, fmt.Errorf("%w: provider", ErrMalformedEvent)
	}
	provider, err := decodeProviderExecution(providerRaw)
	if err != nil {
		return NativeToolSuccessEvent{}, err
	}
	return NativeToolSuccessEvent{Event: event, SessionID: sessionID, AssistantMessageID: assistantMessageID, CallID: callID, Structured: structured, Content: content, OutputPaths: outputPaths, Result: result, Provider: provider, Timestamp: timestamp}, nil
}

func decodeToolFailed(event Event, data map[string]json.RawMessage) (NativeToolFailedEvent, error) {
	if err := rejectUnknownFields(data, "timestamp", "sessionID", "assistantMessageID", "callID", "error", "result", "provider"); err != nil {
		return NativeToolFailedEvent{}, err
	}
	sessionID, assistantMessageID, callID, timestamp, err := decodeToolBase(data)
	if err != nil {
		return NativeToolFailedEvent{}, err
	}
	errorRaw, exists := data["error"]
	if !exists {
		return NativeToolFailedEvent{}, fmt.Errorf("%w: error", ErrMalformedEvent)
	}
	nativeError, err := decodeUnknownError(errorRaw)
	if err != nil {
		return NativeToolFailedEvent{}, err
	}
	result, err := optionalRawValue(data, "result")
	if err != nil {
		return NativeToolFailedEvent{}, err
	}
	providerRaw, exists := data["provider"]
	if !exists {
		return NativeToolFailedEvent{}, fmt.Errorf("%w: provider", ErrMalformedEvent)
	}
	provider, err := decodeProviderExecution(providerRaw)
	if err != nil {
		return NativeToolFailedEvent{}, err
	}
	return NativeToolFailedEvent{Event: event, SessionID: sessionID, AssistantMessageID: assistantMessageID, CallID: callID, Error: nativeError, Result: result, Provider: provider, Timestamp: timestamp}, nil
}

func decodeRetryError(raw json.RawMessage) (NativeRetryError, error) {
	object, err := strictEventObject(raw)
	if err != nil {
		return NativeRetryError{}, err
	}
	if err := rejectUnknownFields(object, "message", "statusCode", "isRetryable", "responseHeaders", "responseBody", "metadata"); err != nil {
		return NativeRetryError{}, err
	}
	message, ok := requiredString(object, "message")
	if !ok {
		return NativeRetryError{}, fmt.Errorf("%w: error.message", ErrMalformedEvent)
	}
	statusCode, err := optionalFiniteNumber(object, "statusCode")
	if err != nil {
		return NativeRetryError{}, err
	}
	isRetryable, ok := requiredBool(object, "isRetryable")
	if !ok {
		return NativeRetryError{}, fmt.Errorf("%w: error.isRetryable", ErrMalformedEvent)
	}
	responseHeaders, err := optionalStringMap(object, "responseHeaders")
	if err != nil {
		return NativeRetryError{}, err
	}
	responseBody, err := optionalString(object, "responseBody")
	if err != nil {
		return NativeRetryError{}, err
	}
	metadata, err := optionalStringMap(object, "metadata")
	if err != nil {
		return NativeRetryError{}, err
	}
	return NativeRetryError{Message: message, StatusCode: statusCode, IsRetryable: isRetryable, ResponseHeaders: responseHeaders, ResponseBody: responseBody, Metadata: metadata}, nil
}

func optionalStringMap(object map[string]json.RawMessage, name string) (map[string]string, error) {
	if _, ok := object[name]; !ok {
		return nil, nil
	}
	value, err := strictEventObject(object[name])
	if err != nil {
		return nil, fmt.Errorf("%w: field %q must be an object", ErrMalformedEvent, name)
	}
	result := make(map[string]string, len(value))
	for key, raw := range value {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, fmt.Errorf("%w: field %q must contain strings", ErrMalformedEvent, name)
		}
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			return nil, fmt.Errorf("%w: field %q must contain strings", ErrMalformedEvent, name)
		}
		result[key] = text
	}
	return result, nil
}

func decodeRetried(event Event, data map[string]json.RawMessage) (NativeRetriedEvent, error) {
	if err := rejectUnknownFields(data, "timestamp", "sessionID", "attempt", "error"); err != nil {
		return NativeRetriedEvent{}, err
	}
	decoded := NativeRetriedEvent{Event: event}
	var ok bool
	if decoded.Timestamp, ok = requiredPositiveInt64(data, "timestamp"); !ok {
		return NativeRetriedEvent{}, fmt.Errorf("%w: timestamp", ErrMalformedEvent)
	}
	if decoded.SessionID, ok = requiredString(data, "sessionID"); !ok || !isID(decoded.SessionID, "ses_") {
		return NativeRetriedEvent{}, fmt.Errorf("%w: sessionID", ErrMalformedEvent)
	}
	if decoded.Attempt, ok = requiredFiniteNumber(data, "attempt"); !ok {
		return NativeRetriedEvent{}, fmt.Errorf("%w: attempt", ErrMalformedEvent)
	}
	errorRaw, exists := data["error"]
	if !exists {
		return NativeRetriedEvent{}, fmt.Errorf("%w: error", ErrMalformedEvent)
	}
	retryError, err := decodeRetryError(errorRaw)
	if err != nil {
		return NativeRetriedEvent{}, err
	}
	decoded.Error = retryError
	return decoded, nil
}

func isJSONObject(raw json.RawMessage) bool {
	_, err := strictObject(raw)
	return err == nil
}

// SessionEventValidator binds replay events to one retained OpenCode session
// and makes duplicate, stale, or cross-session events terminal transport
// failures. It is deliberately not concurrent-safe.
type SessionEventValidator struct {
	sessionID string
	after     uint64
	last      uint64
	failed    error
}

// NewSessionEventValidator requires the native session identity and the exact
// exclusive cursor passed in ?after=N.
func NewSessionEventValidator(sessionID string, after uint64) (*SessionEventValidator, error) {
	if !isID(sessionID, "ses_") {
		return nil, fmt.Errorf("%w: expected session id", ErrInvalidDurableCursor)
	}
	return &SessionEventValidator{sessionID: sessionID, after: after, last: after}, nil
}

// Observe preserves the original admission-only API. Call ObserveEvent when a
// watcher must retain unknown and newly-supported durable event types.
func (validator *SessionEventValidator) Observe(frame Frame) (PromptAdmittedEvent, error) {
	event, err := validator.ObserveEvent(frame)
	if err != nil {
		return PromptAdmittedEvent{}, err
	}
	if event.Kind != SessionEventPromptAdmitted || event.PromptAdmitted == nil {
		return PromptAdmittedEvent{}, validator.fail(fmt.Errorf("%w: session type %q", ErrUnsupportedEvent, event.Type))
	}
	return *event.PromptAdmitted, nil
}

// ObserveEvent validates one durable session event and returns it only after
// its identity and monotonic cursor have been proven. Unknown event types are
// valid observations when their envelope and session identity are valid.
func (validator *SessionEventValidator) ObserveEvent(frame Frame) (DecodedSessionEvent, error) {
	if validator == nil {
		return DecodedSessionEvent{}, fmt.Errorf("opencode session event validator is nil")
	}
	if validator.failed != nil {
		return DecodedSessionEvent{}, validator.failed
	}
	event, err := DecodeSessionEvent(frame)
	if err != nil {
		return DecodedSessionEvent{}, validator.fail(err)
	}
	if event.Durable.AggregateID != validator.sessionID || event.SessionID != validator.sessionID {
		return DecodedSessionEvent{}, validator.fail(fmt.Errorf("%w: expected session %q", ErrIdentityMismatch, validator.sessionID))
	}
	if event.Durable.Seq <= validator.after || event.Durable.Seq <= validator.last {
		return DecodedSessionEvent{}, validator.fail(fmt.Errorf("%w: last %d, got %d", ErrCursorRegression, validator.last, event.Durable.Seq))
	}
	validator.last = event.Durable.Seq
	return event, nil
}

func (validator *SessionEventValidator) fail(err error) error {
	validator.failed = err
	return err
}

// LastCursor returns the last accepted exclusive sequence.
func (validator *SessionEventValidator) LastCursor() uint64 {
	if validator == nil {
		return 0
	}
	return validator.last
}

func (event Event) String() string {
	return strings.TrimSpace(event.Type)
}
