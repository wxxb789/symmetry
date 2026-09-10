// Package opencode contains private protocol helpers for the observed
// OpenCode 1.18.30 serve API. It deliberately does not expose a harness.Adapter
// until native lifecycle behavior has independent evidence.
package opencode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	defaultMaxBodyBytes  = 1 << 20
	defaultMaxFrameBytes = 1 << 20
)

var (
	ErrInvalidConfig        = errors.New("opencode client configuration is invalid")
	ErrUnexpectedStatus     = errors.New("opencode API returned an unexpected HTTP status")
	ErrResponseTooLarge     = errors.New("opencode API response exceeds limit")
	ErrMalformedResponse    = errors.New("opencode API response is malformed")
	ErrInvalidHealth        = errors.New("opencode health response is invalid")
	ErrInvalidSession       = errors.New("opencode session response is invalid")
	ErrInvalidPrompt        = errors.New("opencode prompt response is invalid")
	ErrIdentityMismatch     = errors.New("opencode response identity conflicts with request")
	ErrFrameTooLarge        = errors.New("opencode SSE frame exceeds limit")
	ErrIncompleteFrame      = errors.New("opencode SSE stream ended with an incomplete frame")
	ErrInvalidSSEField      = errors.New("opencode SSE frame contains an unsupported field")
	ErrMalformedEvent       = errors.New("opencode SSE event is malformed")
	ErrUnsupportedEvent     = errors.New("opencode SSE event type is unsupported")
	ErrInvalidDurableCursor = errors.New("opencode SSE durable cursor is invalid")
	ErrCursorRegression     = errors.New("opencode SSE durable sequence is not strictly increasing")
	errStreamEndedUnknown   = errors.New("opencode SSE stream ended without a verified terminal event")
)

// SessionLocation is the narrow persisted identity OpenCode reports for a
// server-owned session. The daemon must retain the returned SessionInfo.ID,
// rather than assuming its requested identifier was accepted.
type SessionLocation struct {
	Directory   string `json:"directory"`
	WorkspaceID string `json:"workspaceID,omitempty"`
}

// SessionModel selects a model only when both fields are explicitly known.
type SessionModel struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

// CreateSessionRequest is the documented portion of POST /api/session.
type CreateSessionRequest struct {
	ID       string          `json:"id,omitempty"`
	Agent    string          `json:"agent,omitempty"`
	Model    *SessionModel   `json:"model,omitempty"`
	Location SessionLocation `json:"location"`
}

// SessionInfo is the identity-bearing subset of OpenCode's Session.Info.
type SessionInfo struct {
	ID        string          `json:"id"`
	ProjectID string          `json:"projectID"`
	Location  SessionLocation `json:"location"`
}

// PromptRequest models durable input admission only. A successful response is
// never a native terminal result.
type PromptRequest struct {
	ID       string
	Text     string
	Delivery string
	Resume   bool
}

// PromptAdmission is OpenCode's SessionInput.Admitted identity boundary.
type PromptAdmission struct {
	AdmittedSeq uint64
	ID          string
	SessionID   string
	Text        string
	Delivery    string
	TimeCreated int64
	PromotedSeq *uint64
}

// Frame is one complete SSE data event. Comments and empty heartbeat frames
// are not surfaced as events.
type Frame struct {
	Sequence uint64
	Data     json.RawMessage
}

// DurableCursor identifies one replayable OpenCode session event.
type DurableCursor struct {
	AggregateID string
	Seq         uint64
	Version     uint64
}

// Event is the bounded JSON envelope shared by global and session streams.
type Event struct {
	ID      string
	Type    string
	Data    json.RawMessage
	Durable *DurableCursor
}

// PromptAdmittedEvent is the only session event presently understood by this
// protocol foundation. New event types require explicit mapping and tests.
type PromptAdmittedEvent struct {
	Event
	SessionID string
	MessageID string
	Text      string
	Delivery  string
	Timestamp int64
}

func (request CreateSessionRequest) validate() error {
	if request.ID != "" && !isID(request.ID, "ses_") {
		return fmt.Errorf("%w: session id must start with ses_", ErrInvalidSession)
	}
	if strings.TrimSpace(request.Location.Directory) == "" {
		return fmt.Errorf("%w: location directory must be non-empty", ErrInvalidSession)
	}
	if request.Model != nil && (strings.TrimSpace(request.Model.ProviderID) == "" || strings.TrimSpace(request.Model.ModelID) == "") {
		return fmt.Errorf("%w: model must include providerID and modelID", ErrInvalidSession)
	}
	return nil
}

func (request PromptRequest) validate() error {
	if !isID(request.ID, "msg_") || strings.TrimSpace(request.Text) == "" {
		return fmt.Errorf("%w: prompt id and text must be non-empty", ErrInvalidPrompt)
	}
	if request.Delivery != "steer" && request.Delivery != "queue" {
		return fmt.Errorf("%w: unsupported delivery %q", ErrInvalidPrompt, request.Delivery)
	}
	return nil
}

func validateSession(info SessionInfo, request CreateSessionRequest) error {
	if !isID(info.ID, "ses_") || strings.TrimSpace(info.ProjectID) == "" || strings.TrimSpace(info.Location.Directory) == "" {
		return ErrInvalidSession
	}
	if request.ID != "" && info.ID != request.ID {
		return fmt.Errorf("%w: requested session %q, got %q", ErrIdentityMismatch, request.ID, info.ID)
	}
	if info.Location.Directory != request.Location.Directory {
		return fmt.Errorf("%w: requested directory %q, got %q", ErrIdentityMismatch, request.Location.Directory, info.Location.Directory)
	}
	if request.Location.WorkspaceID != "" && info.Location.WorkspaceID != request.Location.WorkspaceID {
		return fmt.Errorf("%w: requested workspace %q, got %q", ErrIdentityMismatch, request.Location.WorkspaceID, info.Location.WorkspaceID)
	}
	return nil
}

func validateAdmission(admission PromptAdmission, sessionID string, request PromptRequest) error {
	if admission.AdmittedSeq == 0 || !isID(admission.ID, "msg_") || !isID(admission.SessionID, "ses_") || strings.TrimSpace(admission.Text) == "" || admission.TimeCreated <= 0 {
		return ErrInvalidPrompt
	}
	if admission.ID != request.ID || admission.SessionID != sessionID || admission.Text != request.Text || admission.Delivery != request.Delivery {
		return fmt.Errorf("%w: prompt admission does not match request", ErrIdentityMismatch)
	}
	if admission.PromotedSeq != nil && *admission.PromotedSeq <= admission.AdmittedSeq {
		return ErrInvalidPrompt
	}
	return nil
}

func strictObject(raw []byte) (map[string]json.RawMessage, error) {
	if !utf8.Valid(raw) || !json.Valid(raw) {
		return nil, ErrMalformedResponse
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, ErrMalformedResponse
	}
	delimiter, ok := token.(json.Delim)
	if !ok || delimiter != '{' {
		return nil, ErrMalformedResponse
	}
	object := make(map[string]json.RawMessage)
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, ErrMalformedResponse
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, ErrMalformedResponse
		}
		if _, exists := object[key]; exists {
			return nil, fmt.Errorf("%w: duplicate field %q", ErrMalformedResponse, key)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, ErrMalformedResponse
		}
		object[key] = append(json.RawMessage(nil), value...)
	}
	token, err = decoder.Token()
	if err != nil {
		return nil, ErrMalformedResponse
	}
	delimiter, ok = token.(json.Delim)
	if !ok || delimiter != '}' {
		return nil, ErrMalformedResponse
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrMalformedResponse
	}
	return object, nil
}

func requiredString(object map[string]json.RawMessage, name string) (string, bool) {
	raw, ok := object[name]
	if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || strings.TrimSpace(value) == "" {
		return "", false
	}
	return value, true
}

func requiredPositiveInt(object map[string]json.RawMessage, name string) (uint64, bool) {
	raw, ok := object[name]
	if !ok {
		return 0, false
	}
	var value uint64
	if err := json.Unmarshal(raw, &value); err != nil || value == 0 {
		return 0, false
	}
	return value, true
}

func requiredPositiveInt64(object map[string]json.RawMessage, name string) (int64, bool) {
	raw, ok := object[name]
	if !ok {
		return 0, false
	}
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil || value <= 0 {
		return 0, false
	}
	return value, true
}

func isID(value, prefix string) bool {
	return strings.HasPrefix(value, prefix) && len(value) > len(prefix) && strings.TrimSpace(value) == value
}
