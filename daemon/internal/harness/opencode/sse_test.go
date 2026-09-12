package opencode

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestDecoderReadsFixtureAcrossChunksCRLFAndHeartbeat(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "testdata", "opencode", "1.18.30", "session-events.sse"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	decoder := NewDecoder(1024)
	var frames []Frame
	for _, chunk := range [][]byte{fixture[:13], fixture[13:99], fixture[99:]} {
		decoded, err := decoder.Feed(chunk)
		if err != nil {
			t.Fatalf("Feed() error = %v", err)
		}
		frames = append(frames, decoded...)
	}
	if trailing, err := decoder.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	} else {
		frames = append(frames, trailing...)
	}
	if len(frames) != 1 || frames[0].Sequence != 1 {
		t.Fatalf("frames = %#v, want one frame", frames)
	}
	event, err := DecodePromptAdmittedEvent(frames[0])
	if err != nil || event.Durable.Seq != 1 || event.SessionID != "ses_native_1" || event.MessageID != "msg_native_1" {
		t.Fatalf("DecodePromptAdmittedEvent() = %+v, %v", event, err)
	}
}

func TestDecoderSupportsCRLFAndMultilineData(t *testing.T) {
	decoder := NewDecoder(1024)
	frames, err := decoder.Feed([]byte("data: {\r\ndata: \"id\":\"evt_1\",\"type\":\"server.connected\",\"data\":{}}\r\n\r\n"))
	if err != nil || len(frames) != 1 {
		t.Fatalf("Feed() = %#v, %v", frames, err)
	}
	if _, err := DecodeGlobalEvent(frames[0]); err != nil {
		t.Fatalf("DecodeGlobalEvent() error = %v", err)
	}
}

func TestDecoderRejectsOversizedInvalidAndTruncatedFrames(t *testing.T) {
	tests := []struct {
		name  string
		input string
		limit int
		close bool
		want  error
	}{
		{name: "oversized", input: "data: 123456789", limit: 8, want: ErrFrameTooLarge},
		{name: "unsupported field", input: "id: evt_1\n\n", limit: 1024, want: ErrInvalidSSEField},
		{name: "missing colon", input: "data\n\n", limit: 1024, want: ErrInvalidSSEField},
		{name: "truncated data", input: "data: {}", limit: 1024, close: true, want: ErrIncompleteFrame},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decoder := NewDecoder(test.limit)
			_, err := decoder.Feed([]byte(test.input))
			if test.close && err == nil {
				_, err = decoder.Close()
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestEventDecodersFailClosed(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		global bool
		want   error
	}{
		{name: "malformed JSON", input: "data: {oops}\n\n", global: true, want: ErrMalformedEvent},
		{name: "missing id", input: "data: {\"type\":\"server.connected\",\"data\":{}}\n\n", global: true, want: ErrMalformedEvent},
		{name: "unknown global", input: "data: {\"id\":\"evt_1\",\"type\":\"future\",\"data\":{}}\n\n", global: true, want: ErrUnsupportedEvent},
		{name: "server connected extra data", input: "data: {\"id\":\"evt_1\",\"type\":\"server.connected\",\"data\":{\"extra\":true}}\n\n", global: true, want: ErrMalformedEvent},
		{name: "durable global", input: "data: {\"id\":\"evt_1\",\"type\":\"server.connected\",\"durable\":{\"aggregateID\":\"ses_1\",\"seq\":1,\"version\":1},\"data\":{}}\n\n", global: true, want: ErrUnsupportedEvent},
		{name: "unknown session", input: "data: {\"id\":\"evt_1\",\"type\":\"session.future\",\"durable\":{\"aggregateID\":\"ses_1\",\"seq\":1,\"version\":1},\"data\":{}}\n\n", want: ErrUnsupportedEvent},
		{name: "missing durable", input: "data: {\"id\":\"evt_1\",\"type\":\"session.next.prompt.admitted\",\"data\":{}}\n\n", want: ErrUnsupportedEvent},
		{name: "bad cursor", input: "data: {\"id\":\"evt_1\",\"type\":\"session.next.prompt.admitted\",\"durable\":{\"aggregateID\":\"ses_1\",\"seq\":0,\"version\":1},\"data\":{}}\n\n", want: ErrInvalidDurableCursor},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			frames, err := NewDecoder(1024).Feed([]byte(test.input))
			if err != nil || len(frames) != 1 {
				t.Fatalf("Feed() = %#v, %v", frames, err)
			}
			if test.global {
				_, err = DecodeGlobalEvent(frames[0])
			} else {
				_, err = DecodePromptAdmittedEvent(frames[0])
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("decode error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestSessionEventValidatorBindsIdentityAndStrictCursor(t *testing.T) {
	validator, err := NewSessionEventValidator("ses_native_1", 0)
	if err != nil {
		t.Fatalf("NewSessionEventValidator() error = %v", err)
	}
	first := frame(t, `{"id":"evt_1","type":"session.next.prompt.admitted","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","messageID":"msg_1","prompt":{"text":"one"},"delivery":"steer"}}`)
	if event, err := validator.Observe(first); err != nil || event.Text != "one" || validator.LastCursor() != 1 {
		t.Fatalf("Observe(first) = %+v, %v, cursor=%d", event, err, validator.LastCursor())
	}
	for _, next := range []Frame{
		frame(t, `{"id":"evt_1","type":"session.next.prompt.admitted","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"timestamp":2,"sessionID":"ses_native_1","messageID":"msg_2","prompt":{"text":"duplicate"},"delivery":"steer"}}`),
		frame(t, `{"id":"evt_2","type":"session.next.prompt.admitted","durable":{"aggregateID":"ses_other","seq":2,"version":1},"data":{"timestamp":2,"sessionID":"ses_other","messageID":"msg_2","prompt":{"text":"other"},"delivery":"steer"}}`),
	} {
		if _, err := validator.Observe(next); !errors.Is(err, ErrCursorRegression) {
			// The first invalid observation is retained. It cannot safely continue.
			if !errors.Is(err, ErrIdentityMismatch) {
				t.Fatalf("Observe() error = %v, want retained fail-closed error", err)
			}
		}
	}
}

func TestSessionEventValidatorRejectsCrossSessionAndStaleAfter(t *testing.T) {
	validator, err := NewSessionEventValidator("ses_native_1", 5)
	if err != nil {
		t.Fatalf("NewSessionEventValidator() error = %v", err)
	}
	stale := frame(t, `{"id":"evt_5","type":"session.next.prompt.admitted","durable":{"aggregateID":"ses_native_1","seq":5,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","messageID":"msg_5","prompt":{"text":"stale"},"delivery":"steer"}}`)
	if _, err := validator.Observe(stale); !errors.Is(err, ErrCursorRegression) {
		t.Fatalf("Observe(stale) error = %v, want ErrCursorRegression", err)
	}
	validator, _ = NewSessionEventValidator("ses_native_1", 0)
	cross := frame(t, `{"id":"evt_1","type":"session.next.prompt.admitted","durable":{"aggregateID":"ses_other","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_other","messageID":"msg_1","prompt":{"text":"other"},"delivery":"steer"}}`)
	if _, err := validator.Observe(cross); !errors.Is(err, ErrIdentityMismatch) {
		t.Fatalf("Observe(cross) error = %v, want ErrIdentityMismatch", err)
	}
}

func TestDecodeSessionEventKnownDurableTypes(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		version  uint64
		data     string
		kind     SessionEventKind
	}{
		{
			name: "step started", typeName: "session.next.step.started", version: 1,
			data: `{"timestamp":1789052191000,"sessionID":"ses_native_1","assistantMessageID":"msg_assistant_1","agent":"build","model":{"id":"model-1","providerID":"provider-1"}}`,
			kind: SessionEventStepStarted,
		},
		{
			name: "step ended", typeName: "session.next.step.ended", version: 2,
			data: `{"timestamp":1789052192000,"sessionID":"ses_native_1","assistantMessageID":"msg_assistant_1","finish":"stop","cost":0,"tokens":{"input":1,"output":2,"reasoning":0,"cache":{"read":0,"write":0}},"files":["README.md"]}`,
			kind: SessionEventStepEnded,
		},
		{
			name: "step failed", typeName: "session.next.step.failed", version: 2,
			data: `{"timestamp":1789052193000,"sessionID":"ses_native_1","assistantMessageID":"msg_assistant_1","error":{"type":"unknown","message":"provider unavailable"}}`,
			kind: SessionEventStepFailed,
		},
		{
			name: "text ended", typeName: "session.next.text.ended", version: 1,
			data: `{"timestamp":1789052194000,"sessionID":"ses_native_1","assistantMessageID":"msg_assistant_1","textID":"part_text_1","text":"{\"schema_version\":\"symmetry.task_result.v1\"}"}`,
			kind: SessionEventTextEnded,
		},
		{
			name: "tool called", typeName: "session.next.tool.called", version: 1,
			data: `{"timestamp":1789052195000,"sessionID":"ses_native_1","assistantMessageID":"msg_assistant_1","callID":"call_1","tool":"read","input":{"path":"README.md"},"provider":{"executed":true}}`,
			kind: SessionEventToolCalled,
		},
		{
			name: "tool success", typeName: "session.next.tool.success", version: 1,
			data: `{"timestamp":1789052196000,"sessionID":"ses_native_1","assistantMessageID":"msg_assistant_1","callID":"call_1","structured":{},"content":[{"type":"text","text":"done"}],"outputPaths":["README.md"],"result":{"ok":true},"provider":{"executed":true}}`,
			kind: SessionEventToolSuccess,
		},
		{
			name: "tool failed", typeName: "session.next.tool.failed", version: 1,
			data: `{"timestamp":1789052197000,"sessionID":"ses_native_1","assistantMessageID":"msg_assistant_1","callID":"call_2","error":{"type":"unknown","message":"failed"},"result":{"ok":false},"provider":{"executed":false}}`,
			kind: SessionEventToolFailed,
		},
		{
			name: "retried", typeName: "session.next.retried", version: 1,
			data: `{"timestamp":1789052198000,"sessionID":"ses_native_1","attempt":1,"error":{"message":"retry","isRetryable":true,"statusCode":503,"responseHeaders":{"retry-after":"1"},"responseBody":"busy","metadata":{"provider":"test"}}}`,
			kind: SessionEventRetried,
		},
	}
	for index, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := frame(t, `{"id":"evt_known_`+string(rune('a'+index))+`","type":"`+test.typeName+`","durable":{"aggregateID":"ses_native_1","seq":`+strconv.Itoa(index+2)+`,"version":`+strconv.FormatUint(test.version, 10)+`},"data":`+test.data+`}`)
			decoded, err := DecodeSessionEvent(event)
			if err != nil {
				t.Fatalf("DecodeSessionEvent() error = %v", err)
			}
			if decoded.Kind != test.kind || decoded.SessionID != "ses_native_1" || decoded.Type != test.typeName {
				t.Fatalf("decoded = %+v, want kind=%q type=%q", decoded, test.kind, test.typeName)
			}
		})
	}
}

func TestSessionEventValidatorAcceptsUnknownAndContinues(t *testing.T) {
	validator, err := NewSessionEventValidator("ses_native_1", 0)
	if err != nil {
		t.Fatalf("NewSessionEventValidator() error = %v", err)
	}
	unknown := frame(t, `{"id":"evt_unknown","type":"session.next.future","durable":{"aggregateID":"ses_native_1","seq":1,"version":9},"data":{"sessionID":"ses_native_1","future":{"nested":[1,{"ok":true}]}}}`)
	decoded, err := validator.ObserveEvent(unknown)
	if err != nil || decoded.Kind != SessionEventUnknown || validator.LastCursor() != 1 {
		t.Fatalf("ObserveEvent(unknown) = %+v, %v, cursor=%d", decoded, err, validator.LastCursor())
	}
	terminalCandidate := frame(t, `{"id":"evt_step_end","type":"session.next.step.ended","durable":{"aggregateID":"ses_native_1","seq":2,"version":2},"data":{"timestamp":1789052192000,"sessionID":"ses_native_1","assistantMessageID":"msg_assistant_1","finish":"stop","cost":0,"tokens":{"input":1,"output":1,"reasoning":0,"cache":{"read":0,"write":0}}}}`)
	if decoded, err := validator.ObserveEvent(terminalCandidate); err != nil || decoded.Kind != SessionEventStepEnded || validator.LastCursor() != 2 {
		t.Fatalf("ObserveEvent(known) = %+v, %v, cursor=%d", decoded, err, validator.LastCursor())
	}
}

func TestDecodeSessionEventAcceptsEmptyTextEndedText(t *testing.T) {
	event := frame(t, `{"id":"evt_empty_text","type":"session.next.text.ended","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","assistantMessageID":"msg_assistant_1","textID":"part_text_1","text":""}}`)
	decoded, err := DecodeSessionEvent(event)
	if err != nil {
		t.Fatalf("DecodeSessionEvent() error = %v", err)
	}
	if decoded.Kind != SessionEventTextEnded || decoded.TextEnded == nil || decoded.TextEnded.Text != "" {
		t.Fatalf("decoded = %+v, want empty text-ended payload", decoded)
	}
}

func TestDecodePromptAdmittedRequiresNonNegativeIntegerSourceOffsets(t *testing.T) {
	tests := []struct {
		name  string
		start string
		end   string
		want  error
	}{
		{name: "negative start", start: "-1", end: "2", want: ErrMalformedEvent},
		{name: "fractional start", start: "1.5", end: "2", want: ErrMalformedEvent},
		{name: "negative end", start: "1", end: "-1", want: ErrMalformedEvent},
		{name: "fractional end", start: "1", end: "2.5", want: ErrMalformedEvent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := `{"id":"evt_prompt","type":"session.next.prompt.admitted","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","messageID":"msg_1","prompt":{"text":"prompt","files":[{"uri":"file:///tmp/a","mime":"text/plain","source":{"start":` + test.start + `,"end":` + test.end + `,"text":"prompt"}}]},"delivery":"steer"}}`
			if _, err := DecodeSessionEvent(frame(t, input)); !errors.Is(err, test.want) {
				t.Fatalf("DecodeSessionEvent() error = %v, want %v", err, test.want)
			}
		})
	}
	valid := frame(t, `{"id":"evt_prompt_valid","type":"session.next.prompt.admitted","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","messageID":"msg_1","prompt":{"text":"prompt","files":[{"uri":"file:///tmp/a","mime":"text/plain","source":{"start":0,"end":2,"text":"prompt"}}]},"delivery":"steer"}}`)
	if _, err := DecodeSessionEvent(valid); err != nil {
		t.Fatalf("DecodeSessionEvent(valid) error = %v", err)
	}
}

func TestDecodeEventMetadataRequiresObjectAndValidNestedJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  error
	}{
		{
			name:  "scalar metadata",
			input: `{"id":"evt_metadata_scalar","type":"session.next.future","metadata":"opaque","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"sessionID":"ses_native_1"}}`,
			want:  ErrMalformedEvent,
		},
		{
			name:  "nested duplicate metadata key",
			input: `{"id":"evt_metadata_duplicate","type":"session.next.future","metadata":{"trace":{"key":1,"key":2}},"durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"sessionID":"ses_native_1"}}`,
			want:  ErrMalformedEvent,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeSessionEvent(frame(t, test.input)); !errors.Is(err, test.want) {
				t.Fatalf("DecodeSessionEvent() error = %v, want %v", err, test.want)
			}
		})
	}
	valid := frame(t, `{"id":"evt_metadata_valid","type":"session.next.future","metadata":{"trace":{"nested":[true,null,{"value":"ok"}]}},"durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"sessionID":"ses_native_1"}}`)
	decoded, err := DecodeSessionEvent(valid)
	if err != nil || decoded.Kind != SessionEventUnknown {
		t.Fatalf("DecodeSessionEvent(valid) = %+v, %v; want unknown event with valid metadata", decoded, err)
	}
}

func TestDecodeSessionEventRejectsKnownShapeVersionIdentityAndDuplicates(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  error
	}{
		{
			name:  "wrong durable version",
			input: `{"id":"evt_bad_version","type":"session.next.step.ended","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","assistantMessageID":"msg_1","finish":"stop","cost":0,"tokens":{"input":0,"output":0,"reasoning":0,"cache":{"read":0,"write":0}}}}`,
			want:  ErrInvalidEventVersion,
		},
		{
			name:  "wrong aggregate identity",
			input: `{"id":"evt_bad_identity","type":"session.next.step.started","durable":{"aggregateID":"ses_other","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","assistantMessageID":"msg_1","agent":"build","model":{"id":"model","providerID":"provider"}}}`,
			want:  ErrIdentityMismatch,
		},
		{
			name:  "unknown known-field",
			input: `{"id":"evt_bad_field","type":"session.next.text.ended","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","assistantMessageID":"msg_1","textID":"part_1","text":"done","unexpected":true}}`,
			want:  ErrMalformedEvent,
		},
		{
			name:  "duplicate nested input key",
			input: `{"id":"evt_duplicate","type":"session.next.tool.called","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","assistantMessageID":"msg_1","callID":"call_1","tool":"read","input":{"path":"a","path":"b"},"provider":{"executed":true}}}`,
			want:  ErrMalformedEvent,
		},
		{
			name:  "unknown event missing session identity",
			input: `{"id":"evt_unknown_bad","type":"session.next.future","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"future":true}}`,
			want:  ErrMalformedEvent,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeSessionEvent(frame(t, test.input[0:])); !errors.Is(err, test.want) {
				t.Fatalf("DecodeSessionEvent() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestDecodeSessionEventAcceptsExplicitNullToolResult(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantResult string
	}{
		{name: "success missing", input: `{"id":"evt_success_missing","type":"session.next.tool.success","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","assistantMessageID":"msg_1","callID":"call_1","structured":{},"content":[],"provider":{"executed":true}}}`},
		{name: "success null", input: `{"id":"evt_success_null","type":"session.next.tool.success","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","assistantMessageID":"msg_1","callID":"call_1","structured":{},"content":[],"result":null,"provider":{"executed":true}}}`, wantResult: "null"},
		{name: "failed null", input: `{"id":"evt_failed_null","type":"session.next.tool.failed","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","assistantMessageID":"msg_1","callID":"call_1","error":{"type":"unknown","message":"failed"},"result":null,"provider":{"executed":false}}}`, wantResult: "null"},
		{name: "success nested null", input: `{"id":"evt_success_nested","type":"session.next.tool.success","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","assistantMessageID":"msg_1","callID":"call_1","structured":{},"content":[],"result":{"detail":null},"provider":{"executed":true}}}`, wantResult: `{"detail":null}`},
	}
	for _, test := range tests {
		decoded, err := DecodeSessionEvent(frame(t, test.input))
		if err != nil {
			t.Fatalf("%s: DecodeSessionEvent() error = %v", test.name, err)
		}
		var result json.RawMessage
		switch {
		case decoded.ToolSuccess != nil:
			result = decoded.ToolSuccess.Result
		case decoded.ToolFailed != nil:
			result = decoded.ToolFailed.Result
		default:
			t.Fatalf("%s: decoded event has no tool result payload", test.name)
		}
		if string(result) != test.wantResult {
			t.Fatalf("%s: result = %q, want %q", test.name, result, test.wantResult)
		}
	}
}

func TestDecodeSessionEventRejectsNullArrays(t *testing.T) {
	tests := []string{
		`{"id":"evt_files","type":"session.next.prompt.admitted","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","messageID":"msg_1","prompt":{"text":"prompt","files":null},"delivery":"steer"}}`,
		`{"id":"evt_agents","type":"session.next.prompt.admitted","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","messageID":"msg_1","prompt":{"text":"prompt","agents":null},"delivery":"steer"}}`,
		`{"id":"evt_content","type":"session.next.tool.success","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","assistantMessageID":"msg_1","callID":"call_1","structured":{},"content":null,"result":{"ok":true},"provider":{"executed":true}}}`,
	}
	for _, input := range tests {
		if _, err := DecodeSessionEvent(frame(t, input)); !errors.Is(err, ErrMalformedEvent) {
			t.Fatalf("DecodeSessionEvent() error = %v, want ErrMalformedEvent", err)
		}
	}
}

func TestDecodeSessionEventRejectsNestedNulls(t *testing.T) {
	tests := []string{
		`{"id":"evt_files","type":"session.next.step.ended","durable":{"aggregateID":"ses_native_1","seq":1,"version":2},"data":{"timestamp":1,"sessionID":"ses_native_1","assistantMessageID":"msg_1","finish":"stop","cost":0,"tokens":{"input":0,"output":0,"reasoning":0,"cache":{"read":0,"write":0}},"files":[null]}}`,
		`{"id":"evt_paths","type":"session.next.tool.success","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","assistantMessageID":"msg_1","callID":"call_1","structured":{},"content":[],"result":{"ok":true},"outputPaths":[null],"provider":{"executed":true}}}`,
		`{"id":"evt_headers","type":"session.next.retried","durable":{"aggregateID":"ses_native_1","seq":1,"version":1},"data":{"timestamp":1,"sessionID":"ses_native_1","attempt":1,"error":{"message":"retry","isRetryable":true,"responseHeaders":{"x":null}}}}`,
	}
	for _, input := range tests {
		if _, err := DecodeSessionEvent(frame(t, input)); !errors.Is(err, ErrMalformedEvent) {
			t.Fatalf("DecodeSessionEvent() error = %v, want ErrMalformedEvent", err)
		}
	}
}

func TestSessionEventValidatorRejectsCursorRegressionAfterUnknown(t *testing.T) {
	validator, err := NewSessionEventValidator("ses_native_1", 4)
	if err != nil {
		t.Fatalf("NewSessionEventValidator() error = %v", err)
	}
	stale := frame(t, `{"id":"evt_stale","type":"session.next.future","durable":{"aggregateID":"ses_native_1","seq":4,"version":1},"data":{"sessionID":"ses_native_1"}}`)
	if _, err := validator.ObserveEvent(stale); !errors.Is(err, ErrCursorRegression) {
		t.Fatalf("ObserveEvent(stale) error = %v, want ErrCursorRegression", err)
	}
}

func TestSyntheticLifecycleFixturesDecode(t *testing.T) {
	fixtures := []string{"lifecycle-terminal.sse", "lifecycle-tool-retry.sse", "unknown-then-terminal.sse", "missing-result.sse"}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join("..", "testdata", "opencode", "1.18.30", name)
			contents, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			decoder := NewDecoder(16 * 1024)
			frames, err := decoder.Feed(contents)
			if err != nil {
				t.Fatalf("Feed() error = %v", err)
			}
			trailing, err := decoder.Close()
			if err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			frames = append(frames, trailing...)
			if len(frames) == 0 {
				t.Fatal("fixture produced no frames")
			}
			first, err := DecodeSessionEvent(frames[0])
			if err != nil {
				t.Fatalf("first DecodeSessionEvent() error = %v", err)
			}
			validator, err := NewSessionEventValidator(first.SessionID, 0)
			if err != nil {
				t.Fatalf("NewSessionEventValidator() error = %v", err)
			}
			for index, frame := range frames {
				decoded, err := validator.ObserveEvent(frame)
				if err != nil {
					t.Fatalf("ObserveEvent(frame %d) error = %v", index, err)
				}
				if decoded.SessionID != first.SessionID {
					t.Fatalf("frame %d session = %q, want %q", index, decoded.SessionID, first.SessionID)
				}
			}
		})
	}
}

func frame(t *testing.T, data string) Frame {
	t.Helper()
	decoder := NewDecoder(4096)
	frames, err := decoder.Feed([]byte("data: " + data + "\n\n"))
	if err != nil || len(frames) != 1 {
		t.Fatalf("frame decode = %#v, %v", frames, err)
	}
	return frames[0]
}
