package codex

import (
	"bytes"
	"encoding/json"
	"encoding/json/jsontext"
	"errors"
	"fmt"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

const defaultMaxFrameBytes = 1 << 20

var (
	ErrFrameTooLarge   = errors.New("codex app-server frame exceeds limit")
	ErrIncompleteFrame = errors.New("codex app-server stream ended with an incomplete frame")
)

// FrameKind describes only JSON-RPC-like wire framing. It does not assert that
// a method is supported by Codex.
type FrameKind string

const (
	FrameRequest      FrameKind = "request"
	FrameResponse     FrameKind = "response"
	FrameNotification FrameKind = "notification"
	FrameDiagnostic   FrameKind = "diagnostic"
)

// Frame is a lossless, conservative representation of one newline-delimited
// app-server message. Raw remains available for a later, version-specific
// decoder; unknown messages are diagnostics now.
type Frame struct {
	Kind            FrameKind
	Sequence        uint64
	Raw             json.RawMessage
	Method          string
	ID              json.RawMessage
	Diagnostic      bool
	DiagnosticCode  string
	DiagnosticError string
}

// IsFramingError reports a wire-shape failure that must invalidate any native
// terminal result observed for the same process. Unknown but well-formed
// method/response envelopes remain diagnostics until their exact semantics are
// version-verified; malformed envelopes cannot be allowed to preserve success.
func (frame Frame) IsFramingError() bool {
	switch frame.DiagnosticCode {
	case "malformed_json", "non_object_message", "invalid_jsonrpc_version", "invalid_method", "invalid_jsonrpc_shape", "unknown_native_event", "incomplete_frame":
		return true
	default:
		return false
	}
}

// Framer handles stdio JSONL boundaries across arbitrary process chunks.
type Framer struct {
	max      int
	buffer   []byte
	sequence uint64
}

// NewFramer creates a bounded framer. Non-positive limits use the default.
func NewFramer(maxBytes int) *Framer {
	if maxBytes <= 0 {
		maxBytes = defaultMaxFrameBytes
	}
	return &Framer{max: maxBytes}
}

// Feed appends one process-output chunk and returns complete frames. Malformed
// JSON is retained as a diagnostic frame so it cannot become terminal success.
func (framer *Framer) Feed(chunk []byte) ([]Frame, error) {
	if framer == nil {
		return nil, errors.New("codex framer is nil")
	}
	framer.buffer = append(framer.buffer, chunk...)
	var frames []Frame
	for {
		lineEnd := bytes.IndexByte(framer.buffer, '\n')
		if lineEnd < 0 {
			if len(framer.buffer) > framer.max {
				framer.buffer = nil
				return frames, ErrFrameTooLarge
			}
			break
		}
		line := append([]byte(nil), framer.buffer[:lineEnd]...)
		framer.buffer = framer.buffer[lineEnd+1:]
		if len(line) > framer.max {
			return frames, ErrFrameTooLarge
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		framer.sequence++
		frames = append(frames, decodeFrame(framer.sequence, line))
	}
	return frames, nil
}

// Close flushes no partial frame. A non-whitespace suffix becomes a
// diagnostic and returns ErrIncompleteFrame, making process shutdown visible.
func (framer *Framer) Close() ([]Frame, error) {
	if framer == nil {
		return nil, errors.New("codex framer is nil")
	}
	if len(bytes.TrimSpace(framer.buffer)) == 0 {
		framer.buffer = nil
		return nil, nil
	}
	framer.sequence++
	line := append([]byte(nil), framer.buffer...)
	framer.buffer = nil
	return []Frame{decodeDiagnosticFrame(framer.sequence, line, "incomplete_frame", "stream ended before newline")}, ErrIncompleteFrame
}

func decodeFrame(sequence uint64, line []byte) Frame {
	trimmed := bytes.TrimSpace(line)
	if !jsontext.Value(trimmed).IsValid() {
		return decodeDiagnosticFrame(sequence, trimmed, "malformed_json", "message is not valid JSON")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &object); err != nil || object == nil {
		return decodeDiagnosticFrame(sequence, trimmed, "non_object_message", "native message must be a JSON object")
	}
	method, hasMethod := object["method"]
	id, hasID := object["id"]
	_, hasResult := object["result"]
	_, hasError := object["error"]
	if hasMethod && (hasResult || hasError) {
		return decodeDiagnosticFrame(sequence, trimmed, "invalid_jsonrpc_shape", "native JSON-RPC request or notification cannot contain result or error")
	}
	if !hasMethod && hasID && hasResult == hasError {
		return decodeDiagnosticFrame(sequence, trimmed, "invalid_jsonrpc_shape", "native JSON-RPC response must contain exactly one of result or error")
	}
	if value, ok := object["jsonrpc"]; ok {
		var jsonrpc string
		if json.Unmarshal(value, &jsonrpc) != nil || jsonrpc != "2.0" {
			return decodeDiagnosticFrame(sequence, trimmed, "invalid_jsonrpc_version", "native message jsonrpc, when present, must be 2.0")
		}
	}
	frame := Frame{Sequence: sequence, Raw: append(json.RawMessage(nil), trimmed...)}
	if hasMethod {
		if err := json.Unmarshal(method, &frame.Method); err != nil || frame.Method == "" {
			return decodeDiagnosticFrame(sequence, trimmed, "invalid_method", "method must be a non-empty string")
		}
		if hasID {
			frame.Kind = FrameRequest
			frame.ID = append(json.RawMessage(nil), id...)
		} else {
			frame.Kind = FrameNotification
		}
		frame.Diagnostic = true
		frame.DiagnosticCode = "unverified_native_method"
		frame.DiagnosticError = "native method semantics are not verified"
		return frame
	}
	if hasID {
		if _, ok := object["result"]; ok {
			frame.Kind = FrameResponse
			frame.ID = append(json.RawMessage(nil), id...)
			frame.Diagnostic = true
			frame.DiagnosticCode = "unverified_native_response"
			frame.DiagnosticError = "native response semantics are not verified"
			return frame
		}
		if _, ok := object["error"]; ok {
			frame.Kind = FrameResponse
			frame.ID = append(json.RawMessage(nil), id...)
			frame.Diagnostic = true
			frame.DiagnosticCode = "native_error_response"
			frame.DiagnosticError = "native response contains an error"
			return frame
		}
	}
	return decodeDiagnosticFrame(sequence, trimmed, "unknown_native_event", "native event shape is not verified")
}

func decodeDiagnosticFrame(sequence uint64, raw []byte, code, message string) Frame {
	return Frame{
		Kind:            FrameDiagnostic,
		Sequence:        sequence,
		Raw:             append(json.RawMessage(nil), raw...),
		Diagnostic:      true,
		DiagnosticCode:  code,
		DiagnosticError: message,
	}
}

// Event normalizes a frame without assigning native semantic meaning.
func (frame Frame) Event() harness.Event {
	if frame.Diagnostic {
		return harness.Event{
			Kind:       harness.EventDiagnostic,
			Sequence:   frame.Sequence,
			Diagnostic: true,
			Code:       frame.DiagnosticCode,
			Message:    frame.DiagnosticError,
		}
	}
	return harness.Event{
		Kind:     harness.EventNativeFrame,
		Sequence: frame.Sequence,
	}
}

// String is useful in diagnostics without exposing any inferred protocol
// semantics.
func (frame Frame) String() string {
	return fmt.Sprintf("codex frame %d (%s)", frame.Sequence, frame.Kind)
}
