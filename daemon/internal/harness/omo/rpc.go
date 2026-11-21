package omo

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/wxxb789/symmetry/daemon/internal/harness/linejson"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const defaultMaxRecordBytes = 1 << 20

var (
	ErrInvalidRecord     = errors.New("invalid OmO RPC record")
	ErrCorrelation       = errors.New("OmO RPC correlation mismatch")
	ErrNotSettled        = errors.New("OmO turn has not reached agent_idle")
	ErrPromptNotAccepted = errors.New("OmO prompt admission is unconfirmed")
	ErrMissingResult     = errors.New("OmO settled without a complete normal assistant result")
	ErrUnknownOutcome    = errors.New("OmO durable input outcome is unknown")
	ErrProtocol          = errors.New("incompatible OmO classic RPC protocol")
)

type Command string

const (
	CommandProtocol     Command = "get_protocol_info"
	CommandGetState     Command = "get_state"
	CommandPrompt       Command = "prompt"
	CommandEffectStatus Command = "get_effect_status"
	CommandAbort        Command = "abort"
)

// Request contains only the audited classic-mode command surface.
type Request struct {
	ID              string  `json:"id"`
	Type            Command `json:"type"`
	Message         string  `json:"message,omitempty"`
	ClientMessageID string  `json:"clientMessageId,omitempty"`
	ClientTurnID    string  `json:"clientTurnId,omitempty"`
	EffectID        string  `json:"effectId,omitempty"`
	AwaitIdle       bool    `json:"awaitIdle,omitempty"`
	ClearQueue      bool    `json:"clearQueue,omitempty"`
	GraceMS         int     `json:"graceMs,omitempty"`
}

func validIdentity(value string) bool {
	return strings.TrimSpace(value) != "" && len(value) <= 256 && !strings.ContainsRune(value, 0) && utf8.ValidString(value)
}

func (request Request) Validate() error {
	if !validIdentity(request.ID) {
		return ErrCorrelation
	}
	switch request.Type {
	case CommandProtocol, CommandGetState:
		if request.Message != "" || request.ClientMessageID != "" || request.ClientTurnID != "" || request.EffectID != "" || request.AwaitIdle || request.ClearQueue || request.GraceMS != 0 {
			return ErrInvalidRecord
		}
	case CommandPrompt:
		if strings.TrimSpace(request.Message) == "" || utf8.RuneCountInString(request.Message) > 1000000 || !validIdentity(request.ClientMessageID) || !validIdentity(request.ClientTurnID) || request.EffectID != "" || request.AwaitIdle || request.ClearQueue || request.GraceMS != 0 {
			return ErrInvalidRecord
		}
	case CommandEffectStatus:
		if !validIdentity(request.EffectID) || request.Message != "" || request.ClientMessageID != "" || request.ClientTurnID != "" || request.AwaitIdle || request.ClearQueue || request.GraceMS != 0 {
			return ErrInvalidRecord
		}
	case CommandAbort:
		if !validIdentity(request.EffectID) || !request.AwaitIdle || !request.ClearQueue || request.GraceMS != 5000 || request.Message != "" || request.ClientMessageID != "" || request.ClientTurnID != "" {
			return ErrInvalidRecord
		}
	default:
		return ErrInvalidRecord
	}
	return nil
}

type Response struct {
	ID        string
	Command   Command
	Success   bool
	Data      json.RawMessage
	ErrorCode string
}

// Record retains payloads locally. Adapter events expose only frame metadata.
type Record struct {
	Sequence    uint64
	Type        string
	Fields      map[string]json.RawMessage
	Response    *Response
	DecodeError error
}

// Decoder accepts bounded LF-delimited UTF-8 JSON, including split records.
type Decoder struct {
	reader *linejson.Decoder[Record]
}

func NewDecoder(limit int) *Decoder {
	if limit <= 0 {
		limit = defaultMaxRecordBytes
	}
	return &Decoder{reader: linejson.New(linejson.Policy[Record]{
		MaxBytes:   limit,
		Latch:      true,
		TooLarge:   func() error { return fmt.Errorf("%w: JSONL record exceeds limit", ErrInvalidRecord) },
		Incomplete: func() error { return fmt.Errorf("%w: unterminated JSONL record", ErrInvalidRecord) },
		Decode:     decodeRecord,
		Fatal:      func(record Record) error { return record.DecodeError },
	})}
}

func (decoder *Decoder) Feed(chunk []byte) ([]Record, error) {
	return decoder.reader.Feed(chunk)
}

func (decoder *Decoder) Close() error {
	_, _, err := decoder.reader.Close()
	return err
}

func decodeRecord(sequence uint64, raw []byte) Record {
	record := Record{Sequence: sequence}
	object, err := strictObject(raw)
	if err != nil {
		record.DecodeError = err
		return record
	}
	if !nonEmptyString(object["type"], &record.Type) {
		record.DecodeError = ErrInvalidRecord
		return record
	}
	record.Fields = object
	// Classic RPC never emits routing sessionId tags. A multi-session frame
	// cannot silently join this process's one admitted turn.
	if _, exists := object["sessionId"]; exists {
		record.DecodeError = ErrCorrelation
		return record
	}
	if record.Type != "response" {
		return record
	}
	response := &Response{}
	var command string
	if !nonEmptyString(object["id"], &response.ID) || !validIdentity(response.ID) || !nonEmptyString(object["command"], &command) || requiredBool(object["success"], &response.Success) != nil {
		record.DecodeError = ErrInvalidRecord
		return record
	}
	response.Command = Command(command)
	response.Data = object["data"]
	var nativeError string
	if raw, exists := object["error"]; exists && !nonEmptyString(raw, &nativeError) {
		record.DecodeError = ErrInvalidRecord
		return record
	}
	if response.Success == (nativeError != "") {
		record.DecodeError = ErrInvalidRecord
		return record
	}
	if raw, exists := object["errorCode"]; exists && !nonEmptyString(raw, &response.ErrorCode) {
		record.DecodeError = ErrInvalidRecord
		return record
	}
	record.Response = response
	return record
}

func strictObject(raw []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return nil, ErrInvalidRecord
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := scanJSONValue(decoder); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidRecord
	}
	var object map[string]json.RawMessage
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' || json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, ErrInvalidRecord
	}
	return object, nil
}

func scanJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return ErrInvalidRecord
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		keys := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			key, ok := keyToken.(string)
			if err != nil || !ok {
				return ErrInvalidRecord
			}
			if _, exists := keys[key]; exists {
				return fmt.Errorf("%w: duplicate JSON member", ErrInvalidRecord)
			}
			keys[key] = struct{}{}
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return ErrInvalidRecord
		}
	case '[':
		for decoder.More() {
			if err := scanJSONValue(decoder); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return ErrInvalidRecord
		}
	default:
		return ErrInvalidRecord
	}
	return nil
}

func nonEmptyString(raw json.RawMessage, target *string) bool {
	return len(raw) != 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && json.Unmarshal(raw, target) == nil && strings.TrimSpace(*target) != "" && !strings.ContainsRune(*target, 0)
}

func requiredBool(raw json.RawMessage, value *bool) error {
	if len(raw) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, value) != nil {
		return ErrInvalidRecord
	}
	return nil
}

func requiredArray(raw json.RawMessage) ([]json.RawMessage, error) {
	if len(raw) == 0 || bytes.TrimSpace(raw)[0] != '[' {
		return nil, ErrInvalidRecord
	}
	var array []json.RawMessage
	if json.Unmarshal(raw, &array) != nil {
		return nil, ErrInvalidRecord
	}
	return array, nil
}

// SessionState requires explicit idle fields; absent booleans are not false.
type SessionState struct {
	SessionID           string
	SessionFile         string
	IsStreaming         bool
	IsCompacting        bool
	PendingMessageCount int
	Steering            []json.RawMessage
	FollowUp            []json.RawMessage
}

func decodeSessionState(raw json.RawMessage) (SessionState, error) {
	var state SessionState
	object, err := strictObject(raw)
	if err != nil {
		return state, err
	}
	if !nonEmptyString(object["sessionId"], &state.SessionID) || !nonEmptyString(object["sessionFile"], &state.SessionFile) || !filepath.IsAbs(state.SessionFile) {
		return state, ErrInvalidRecord
	}
	if requiredBool(object["isStreaming"], &state.IsStreaming) != nil || requiredBool(object["isCompacting"], &state.IsCompacting) != nil {
		return state, ErrInvalidRecord
	}
	count := object["pendingMessageCount"]
	if len(count) == 0 || bytes.Equal(bytes.TrimSpace(count), []byte("null")) || json.Unmarshal(count, &state.PendingMessageCount) != nil || state.PendingMessageCount < 0 {
		return state, ErrInvalidRecord
	}
	state.Steering, err = requiredArray(object["steering"])
	if err != nil {
		return state, err
	}
	state.FollowUp, err = requiredArray(object["followUp"])
	if err != nil {
		return state, err
	}
	for _, field := range []string{"pendingDialogs", "pendingQuestions"} {
		if dialogs, exists := object[field]; exists {
			array, err := requiredArray(dialogs)
			if err != nil || len(array) != 0 {
				return state, fmt.Errorf("%w: native UI requires an unavailable response bridge", ErrProtocol)
			}
		}
	}
	var permission, status, effective string
	if !nonEmptyString(object["permissionPreset"], &permission) || !nonEmptyString(object["permissionPresetStatus"], &status) || !nonEmptyString(object["effectivePermissionPreset"], &effective) || permission != "workspace" || effective != "workspace" || status != "applied" {
		return state, fmt.Errorf("%w: workspace permission preset is not applied", ErrProtocol)
	}
	return state, nil
}

func (state SessionState) ValidateIdle() error {
	if state.IsStreaming || state.IsCompacting || state.PendingMessageCount != 0 || len(state.Steering) != 0 || len(state.FollowUp) != 0 {
		return fmt.Errorf("%w: native session is not idle", ErrProtocol)
	}
	return nil
}

// DecodeAssistantTaskResult accepts only one complete final assistant text.
func DecodeAssistantTaskResult(raw json.RawMessage) (protocol.TaskResult, error) {
	var empty protocol.TaskResult
	message, err := strictObject(raw)
	if err != nil {
		return empty, err
	}
	var role, stop string
	if !nonEmptyString(message["role"], &role) || role != "assistant" || !nonEmptyString(message["stopReason"], &stop) || stop != "stop" {
		return empty, ErrMissingResult
	}
	content, err := requiredArray(message["content"])
	if err != nil || len(content) != 1 {
		return empty, ErrMissingResult
	}
	block, err := strictObject(content[0])
	if err != nil {
		return empty, err
	}
	var kind, text string
	if !nonEmptyString(block["type"], &kind) || kind != "text" || !nonEmptyString(block["text"], &text) {
		return empty, ErrMissingResult
	}
	if _, err := strictObject([]byte(text)); err != nil {
		return empty, ErrMissingResult
	}
	result, err := protocol.ParseTaskResult([]byte(text))
	if err != nil {
		return empty, fmt.Errorf("%w: final text does not satisfy TaskResult", ErrMissingResult)
	}
	return result, nil
}
