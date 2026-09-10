package opencode

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// Close rejects an event that did not end on an SSE blank line.
func (decoder *Decoder) Close() ([]Frame, error) {
	if decoder == nil {
		return nil, fmt.Errorf("opencode SSE decoder is nil")
	}
	if decoder.failed != nil {
		return nil, decoder.failed
	}
	if len(decoder.line) > 0 || len(decoder.data) > 0 || decoder.frameLen > 0 {
		return nil, decoder.fail(ErrIncompleteFrame)
	}
	return nil, nil
}

func (decoder *Decoder) consumeLine(line []byte) ([]Frame, error) {
	if len(line) == 0 {
		if len(decoder.data) == 0 {
			decoder.frameLen = 0
			return nil, nil
		}
		data := bytes.Join(decoder.data, []byte("\n"))
		decoder.data = nil
		decoder.frameLen = 0
		decoder.sequence++
		return []Frame{{Sequence: decoder.sequence, Data: append(json.RawMessage(nil), data...)}}, nil
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
	if event.Type != "server.connected" || event.Durable != nil || !isJSONObject(object["data"]) {
		return Event{}, fmt.Errorf("%w: global type %q", ErrUnsupportedEvent, event.Type)
	}
	return event, nil
}

// DecodePromptAdmittedEvent recognizes one durable session event captured from
// v1.18.30. It intentionally rejects any unreviewed event type.
func DecodePromptAdmittedEvent(frame Frame) (PromptAdmittedEvent, error) {
	event, object, err := decodeEvent(frame)
	if err != nil {
		return PromptAdmittedEvent{}, err
	}
	if event.Type != "session.next.prompt.admitted" || event.Durable == nil {
		return PromptAdmittedEvent{}, fmt.Errorf("%w: session type %q", ErrUnsupportedEvent, event.Type)
	}
	data, err := strictObject(object["data"])
	if err != nil {
		return PromptAdmittedEvent{}, fmt.Errorf("%w: event data", ErrMalformedEvent)
	}
	decoded := PromptAdmittedEvent{Event: event}
	var ok bool
	if decoded.Timestamp, ok = requiredPositiveInt64(data, "timestamp"); !ok {
		return PromptAdmittedEvent{}, ErrMalformedEvent
	}
	if decoded.SessionID, ok = requiredString(data, "sessionID"); !ok || !isID(decoded.SessionID, "ses_") {
		return PromptAdmittedEvent{}, ErrMalformedEvent
	}
	if decoded.MessageID, ok = requiredString(data, "messageID"); !ok || !isID(decoded.MessageID, "msg_") {
		return PromptAdmittedEvent{}, ErrMalformedEvent
	}
	if decoded.Delivery, ok = requiredString(data, "delivery"); !ok || (decoded.Delivery != "steer" && decoded.Delivery != "queue") {
		return PromptAdmittedEvent{}, ErrMalformedEvent
	}
	prompt, err := strictObject(data["prompt"])
	if err != nil {
		return PromptAdmittedEvent{}, ErrMalformedEvent
	}
	if decoded.Text, ok = requiredString(prompt, "text"); !ok {
		return PromptAdmittedEvent{}, ErrMalformedEvent
	}
	return decoded, nil
}

func decodeEvent(frame Frame) (Event, map[string]json.RawMessage, error) {
	object, err := strictObject(frame.Data)
	if err != nil {
		return Event{}, nil, fmt.Errorf("%w: %v", ErrMalformedEvent, err)
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

// Observe validates one supported durable session event and returns it only
// after its identity and monotonic cursor have been proven.
func (validator *SessionEventValidator) Observe(frame Frame) (PromptAdmittedEvent, error) {
	if validator == nil {
		return PromptAdmittedEvent{}, fmt.Errorf("opencode session event validator is nil")
	}
	if validator.failed != nil {
		return PromptAdmittedEvent{}, validator.failed
	}
	event, err := DecodePromptAdmittedEvent(frame)
	if err != nil {
		return PromptAdmittedEvent{}, validator.fail(err)
	}
	if event.Durable.AggregateID != validator.sessionID || event.SessionID != validator.sessionID {
		return PromptAdmittedEvent{}, validator.fail(fmt.Errorf("%w: expected session %q", ErrIdentityMismatch, validator.sessionID))
	}
	if event.Durable.Seq <= validator.after || event.Durable.Seq <= validator.last {
		return PromptAdmittedEvent{}, validator.fail(fmt.Errorf("%w: last %d, got %d", ErrCursorRegression, validator.last, event.Durable.Seq))
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
