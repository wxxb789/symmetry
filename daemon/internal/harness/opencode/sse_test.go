package opencode

import (
	"errors"
	"os"
	"path/filepath"
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
	if trailing, err := decoder.Close(); err != nil || len(trailing) != 0 {
		t.Fatalf("Close() = %#v, %v", trailing, err)
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

func frame(t *testing.T, data string) Frame {
	t.Helper()
	decoder := NewDecoder(4096)
	frames, err := decoder.Feed([]byte("data: " + data + "\n\n"))
	if err != nil || len(frames) != 1 {
		t.Fatalf("frame decode = %#v, %v", frames, err)
	}
	return frames[0]
}
