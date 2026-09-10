package codex

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

func TestFramerPreservesSplitFramesAndMarksUnknownEventsDiagnostic(t *testing.T) {
	framer := NewFramer(1024)
	frames, err := framer.Feed([]byte(`{"jsonrpc":"2.0","method":"future/event"`))
	if err != nil || len(frames) != 0 {
		t.Fatalf("first Feed() = frames %v, error %v; want buffered partial frame", frames, err)
	}
	frames, err = framer.Feed([]byte("," + `"params":{"value":1}}` + "\n" + `{"type":"future_event"}` + "\n"))
	if err != nil {
		t.Fatalf("second Feed() error = %v", err)
	}
	if len(frames) != 2 {
		t.Fatalf("frames = %d, want 2", len(frames))
	}
	if frames[0].Kind != FrameNotification || !frames[0].Diagnostic || frames[0].DiagnosticCode != "unverified_native_method" {
		t.Fatalf("method frame = %+v, want unverified diagnostic notification", frames[0])
	}
	if frames[1].Kind != FrameDiagnostic || frames[1].DiagnosticCode != "unknown_native_event" {
		t.Fatalf("unknown frame = %+v, want unknown_native_event diagnostic", frames[1])
	}
	if event := frames[1].Event(); event.Kind != harness.EventDiagnostic || !event.Diagnostic {
		t.Fatalf("unknown event = %+v, want diagnostic event", event)
	}
}

func TestFramerMarksMalformedJSONWithoutPromotingItToSuccess(t *testing.T) {
	framer := NewFramer(1024)
	frames, err := framer.Feed([]byte("{malformed}\n"))
	if err != nil {
		t.Fatalf("Feed() error = %v, want diagnostic frame without stream abort", err)
	}
	if len(frames) != 1 || !frames[0].Diagnostic || frames[0].DiagnosticCode != "malformed_json" {
		t.Fatalf("frames = %+v, want malformed_json diagnostic", frames)
	}
	if event := frames[0].Event(); event.Kind != harness.EventDiagnostic || event.Code != "malformed_json" {
		t.Fatalf("event = %+v, want malformed diagnostic", event)
	}
}

func TestFramerRejectsIncompleteAndOversizedFrames(t *testing.T) {
	framer := NewFramer(8)
	if _, err := framer.Feed([]byte("123456789")); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversized Feed() error = %v, want ErrFrameTooLarge", err)
	}
	framer = NewFramer(1024)
	if _, err := framer.Feed([]byte(`{"jsonrpc":"2.0"}`)); err != nil {
		t.Fatalf("partial Feed() error = %v", err)
	}
	frames, err := framer.Close()
	if !errors.Is(err, ErrIncompleteFrame) || len(frames) != 1 || !frames[0].Diagnostic {
		t.Fatalf("Close() = frames %+v, error %v; want incomplete diagnostic", frames, err)
	}
}

func TestFramerCountsWhitespaceTowardRawFrameLimit(t *testing.T) {
	framer := NewFramer(8)
	line := append(bytes.Repeat([]byte{' '}, 8), []byte("{}\n")...)
	if _, err := framer.Feed(line); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("whitespace-padded frame error = %v, want ErrFrameTooLarge", err)
	}
}

func TestFramerAllowsMissingAndRejectsUnsupportedJSONRPCVersion(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantKind       FrameKind
		wantCode       string
		wantFramingErr bool
	}{
		{
			name:           "missing header",
			input:          `{"method":"turn/completed"}`,
			wantKind:       FrameNotification,
			wantCode:       "unverified_native_method",
			wantFramingErr: false,
		},
		{
			name:           "unsupported version",
			input:          `{"jsonrpc":"1.0","method":"turn/completed"}`,
			wantKind:       FrameDiagnostic,
			wantCode:       "invalid_jsonrpc_version",
			wantFramingErr: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frames, err := NewFramer(1024).Feed([]byte(test.input + "\n"))
			if err != nil || len(frames) != 1 {
				t.Fatalf("Feed(%s) = (%#v, %v), want one frame without feed error", test.input, frames, err)
			}
			frame := frames[0]
			if frame.Kind != test.wantKind || frame.DiagnosticCode != test.wantCode || frame.IsFramingError() != test.wantFramingErr {
				t.Fatalf("Feed(%s) = %+v, want kind=%q code=%q framing_error=%t", test.input, frame, test.wantKind, test.wantCode, test.wantFramingErr)
			}
		})
	}
}

func TestFramerRejectsInvalidJSONRPCMessageShape(t *testing.T) {
	for _, input := range [][]byte{
		[]byte(`{"jsonrpc":"2.0","id":1}`),
		[]byte(`{"jsonrpc":"2.0","id":1,"result":{},"error":{"code":-1,"message":"both"}}`),
		[]byte(`{"jsonrpc":"2.0","method":"event","result":{}}`),
	} {
		frames, err := NewFramer(1024).Feed(append(input, '\n'))
		if err != nil || len(frames) != 1 || frames[0].DiagnosticCode != "invalid_jsonrpc_shape" || !frames[0].IsFramingError() {
			t.Fatalf("Feed(%s) = (%#v, %v), want invalid_jsonrpc_shape diagnostic", input, frames, err)
		}
	}
}

func TestFramerReadsSanitizedVersionFixture(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "testdata", "codex", "0.153.4", "frames.jsonl"))
	if err != nil {
		t.Fatalf("read framing fixture: %v", err)
	}
	frames, err := NewFramer(1024).Feed(fixture)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	if len(frames) != 4 {
		t.Fatalf("frames = %d, want four retained fixture records", len(frames))
	}
	if !frames[0].Diagnostic || frames[0].DiagnosticCode != "unverified_native_method" {
		t.Fatalf("method fixture = %+v, want unverified diagnostic", frames[0])
	}
	if !frames[1].Diagnostic || frames[1].DiagnosticCode != "unknown_native_event" {
		t.Fatalf("unknown fixture = %+v, want unknown diagnostic", frames[1])
	}
	if !frames[2].Diagnostic || frames[2].DiagnosticCode != "unverified_native_response" {
		t.Fatalf("response fixture = %+v, want unverified diagnostic", frames[2])
	}
	if !frames[3].Diagnostic || frames[3].DiagnosticCode != "malformed_json" {
		t.Fatalf("malformed fixture = %+v, want malformed diagnostic", frames[3])
	}
}

func TestFramerAcceptsRawCodexStartSmokeWithoutJSONRPCHeader(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "testdata", "codex", "0.153.4", "app-server-start-smoke.jsonl"))
	if err != nil {
		t.Fatalf("read raw start smoke fixture: %v", err)
	}
	frames, err := NewFramer(128 * 1024).Feed(fixture)
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	if len(frames) != 3 {
		t.Fatalf("frames = %d, want three raw smoke records", len(frames))
	}
	want := []struct {
		kind FrameKind
		code string
	}{
		{kind: FrameResponse, code: "unverified_native_response"},
		{kind: FrameNotification, code: "unverified_native_method"},
		{kind: FrameResponse, code: "unverified_native_response"},
	}
	for index, frame := range frames {
		if frame.Kind != want[index].kind || frame.DiagnosticCode != want[index].code || frame.IsFramingError() {
			t.Fatalf("frame %d = %+v, want accepted diagnostic kind=%q code=%q", index, frame, want[index].kind, want[index].code)
		}
	}
}
