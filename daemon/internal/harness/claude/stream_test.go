package claude

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecoderReadsFixtureAcrossCRLFAndChunkBoundaries(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "testdata", "claude", "2.1.281", "stream-json.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	fixture = []byte(strings.ReplaceAll(string(fixture), "\n", "\r\n"))
	decoder := NewDecoder(1024)
	var events []Event
	for _, chunk := range [][]byte{fixture[:17], fixture[17:93], fixture[93:]} {
		decoded, feedErr := decoder.Feed(chunk)
		if feedErr != nil {
			t.Fatalf("Feed() error = %v", feedErr)
		}
		events = append(events, decoded...)
	}
	if trailing, closeErr := decoder.Close(); closeErr != nil || len(trailing) != 0 {
		t.Fatalf("Close() = %#v, %v; want no trailing record", trailing, closeErr)
	}
	if len(events) != 5 {
		t.Fatalf("events = %d, want 5", len(events))
	}
	want := []EventType{EventSystem, EventUser, EventAssistant, EventLog, EventResult}
	for index, event := range events {
		if event.DecodeError != nil || event.Type != want[index] || event.Sequence != uint64(index+1) {
			t.Fatalf("event %d = %+v, want type=%q valid sequence", index, event, want[index])
		}
		if event.SessionID != "native-session-1" {
			t.Fatalf("event %d session = %q, want native-session-1", index, event.SessionID)
		}
	}
	if events[4].Result == nil || events[4].Result.IsError || events[4].Result.Subtype != "success" || string(events[4].Result.StructuredOutput) != `{"status":"done"}` {
		t.Fatalf("result = %+v, want decoded successful result with structured_output", events[4].Result)
	}
}

func TestTerminalValidatorAcceptsOnlyObservedMatchingSuccessfulResult(t *testing.T) {
	decoder := NewDecoder(1024)
	events, err := decoder.Feed([]byte("{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"actual\"}\n{\"type\":\"result\",\"subtype\":\"success\",\"terminal_reason\":\"completed\",\"is_error\":false,\"session_id\":\"actual\",\"result\":\"done\",\"structured_output\":{\"status\":\"done\"},\"usage\":{\"input_tokens\":1}}\n"))
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	validator := NewTerminalValidator("actual")
	for _, event := range events {
		if err := validator.Observe(event); err != nil {
			t.Fatalf("Observe(%+v) error = %v", event, err)
		}
	}
	terminal, err := validator.Finish()
	if err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if terminal.SessionID != "actual" || terminal.Result.Subtype != "success" || string(terminal.Result.StructuredOutput) != `{"status":"done"}` || string(terminal.Result.Output) != "\"done\"" {
		t.Fatalf("terminal = %+v, want observed successful result", terminal)
	}
}

func TestTerminalValidatorDoesNotTreatLaunchIntentAsObservedSession(t *testing.T) {
	decoder := NewDecoder(1024)
	events, err := decoder.Feed([]byte("{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"done\"}\n"))
	if err != nil {
		t.Fatalf("Feed() error = %v", err)
	}
	validator := NewTerminalValidator("launch-intent-only")
	if err := validator.Observe(events[0]); !errors.Is(err, ErrMissingResultIdentity) {
		t.Fatalf("Observe() error = %v, want ErrMissingResultIdentity", err)
	}
	if _, err := validator.Finish(); !errors.Is(err, ErrMissingResultIdentity) {
		t.Fatalf("Finish() error = %v, want ErrMissingResultIdentity", err)
	}
}

func TestTerminalValidatorRequiresResultEvent(t *testing.T) {
	decoder := NewDecoder(1024)
	events, err := decoder.Feed([]byte("{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":\"actual\"}\n"))
	if err != nil || len(events) != 1 {
		t.Fatalf("Feed() = %#v, %v", events, err)
	}
	validator := NewTerminalValidator("actual")
	if err := validator.Observe(events[0]); err != nil {
		t.Fatalf("Observe() error = %v", err)
	}
	if _, err := validator.Finish(); !errors.Is(err, ErrMissingResult) {
		t.Fatalf("Finish() error = %v, want ErrMissingResult", err)
	}
}

func TestDecoderAndValidatorFailClosedForInvalidRecords(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  error
	}{
		{name: "malformed", input: "{invalid}\n", want: ErrMalformedJSON},
		{name: "duplicate terminal flag", input: `{"type":"result","subtype":"success","is_error":true,"is_error":false,"session_id":"s","result":"done"}` + "\n", want: ErrMalformedJSON},
		{name: "escaped duplicate session", input: `{"type":"result","subtype":"success","is_error":false,"session_id":"first","session_\u0069d":"second","result":"done"}` + "\n", want: ErrMalformedJSON},
		{name: "nested duplicate usage", input: `{"type":"result","subtype":"success","is_error":false,"session_id":"s","result":"done","usage":{"input_tokens":1,"input_tokens":2}}` + "\n", want: ErrMalformedJSON},
		{name: "duplicate inside array", input: `{"type":"result","subtype":"success","is_error":false,"session_id":"s","result":"done","usage":{"tokens":[{"count":1,"count":2}]}}` + "\n", want: ErrMalformedJSON},
		{name: "invalid UTF8", input: "{\"type\":\"system\",\"session_id\":\"\xff\"}\n", want: ErrMalformedJSON},
		{name: "unpaired surrogate", input: `{"type":"system","session_id":"\ud800"}` + "\n", want: ErrMalformedJSON},
		{name: "non object", input: "[]\n", want: ErrNonObjectRecord},
		{name: "unknown type", input: "{\"type\":\"future\"}\n", want: ErrUnsupportedEvent},
		{name: "invalid result", input: "{\"type\":\"result\",\"session_id\":\"s\"}\n", want: ErrInvalidResult},
		{name: "invalid result subtype", input: "{\"type\":\"result\",\"subtype\":1,\"is_error\":false,\"session_id\":\"s\"}\n", want: ErrInvalidResult},
		{name: "null result subtype", input: "{\"type\":\"result\",\"subtype\":null,\"is_error\":false,\"session_id\":\"s\"}\n", want: ErrInvalidResult},
		{name: "null is error", input: "{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":null,\"session_id\":\"s\"}\n", want: ErrInvalidResult},
		{name: "string is error", input: "{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":\"false\",\"session_id\":\"s\"}\n", want: ErrInvalidResult},
		{name: "null terminal reason", input: "{\"type\":\"result\",\"subtype\":\"success\",\"terminal_reason\":null,\"is_error\":false,\"session_id\":\"s\"}\n", want: ErrInvalidResult},
		{name: "invalid control", input: "{\"type\":\"control_request\",\"session_id\":\"s\",\"request_id\":\"\"}\n", want: ErrInvalidControlRequest},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events, err := NewDecoder(1024).Feed([]byte(test.input))
			if err != nil || len(events) != 1 {
				t.Fatalf("Feed() = %#v, %v; want one invalid event", events, err)
			}
			validator := NewTerminalValidator("")
			if err := validator.Observe(events[0]); !errors.Is(err, test.want) {
				t.Fatalf("Observe() error = %v, want %v", err, test.want)
			}
			if _, err := validator.Finish(); !errors.Is(err, test.want) {
				t.Fatalf("Finish() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestTerminalValidatorRequiresStructuredOutputObject(t *testing.T) {
	tests := []struct {
		name   string
		fields string
	}{
		{name: "missing", fields: ""},
		{name: "null", fields: `,"structured_output":null`},
		{name: "string", fields: `,"structured_output":"{}"`},
		{name: "array", fields: `,"structured_output":[{}]`},
		{name: "success text without structured output", fields: `,"terminal_reason":"completed","result":"{\"kind\":\"progress\"}"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := `{"type":"result","subtype":"success","is_error":false,"session_id":"s"` + test.fields + "}\n"
			events, err := NewDecoder(1024).Feed([]byte(input))
			if err != nil || len(events) != 1 || events[0].DecodeError != nil {
				t.Fatalf("Feed() = %#v, %v", events, err)
			}
			validator := NewTerminalValidator("s")
			if err := validator.Observe(events[0]); !errors.Is(err, ErrMissingStructuredOutput) {
				t.Fatalf("Observe() error = %v, want ErrMissingStructuredOutput", err)
			}
			if _, err := validator.Finish(); !errors.Is(err, ErrMissingStructuredOutput) {
				t.Fatalf("Finish() error = %v, want ErrMissingStructuredOutput", err)
			}
		})
	}
}

// TestTerminalValidatorReplaysNativeStructuredOutputCaptures feeds sanitized
// Claude Code 2.1.281 --json-schema stream-json captures, recorded against a
// loopback fake Messages API, through the decoder and validator.
func TestTerminalValidatorReplaysNativeStructuredOutputCaptures(t *testing.T) {
	tests := []struct {
		file string
		want error
	}{
		{file: "structured-output-task-result-success.jsonl"},
		{file: "structured-output-retry-exhausted.jsonl", want: ErrResultReportedError},
		{file: "structured-output-missing-after-text-success.jsonl", want: ErrMissingStructuredOutput},
	}
	for _, test := range tests {
		t.Run(test.file, func(t *testing.T) {
			capture, err := os.ReadFile(filepath.Join("..", "testdata", "claude", "2.1.281", test.file))
			if err != nil {
				t.Fatalf("read capture: %v", err)
			}
			decoder := NewDecoder(0)
			events, err := decoder.Feed(capture)
			if err != nil {
				t.Fatalf("Feed() error = %v", err)
			}
			if trailing, closeErr := decoder.Close(); closeErr != nil || len(trailing) != 0 {
				t.Fatalf("Close() = %#v, %v; want no trailing record", trailing, closeErr)
			}
			if len(events) == 0 || events[len(events)-1].Type != EventResult {
				t.Fatalf("capture events = %d, want a terminal result record", len(events))
			}
			validator := NewTerminalValidator("")
			var observeErr error
			for _, event := range events {
				if observeErr = validator.Observe(event); observeErr != nil {
					break
				}
			}
			terminal, finishErr := validator.Finish()
			if test.want != nil {
				if !errors.Is(observeErr, test.want) || !errors.Is(finishErr, test.want) {
					t.Fatalf("Observe()/Finish() errors = %v / %v, want %v", observeErr, finishErr, test.want)
				}
				return
			}
			if observeErr != nil || finishErr != nil {
				t.Fatalf("Observe()/Finish() errors = %v / %v, want success", observeErr, finishErr)
			}
			var output struct {
				SchemaVersion string `json:"schema_version"`
				Kind          string `json:"kind"`
			}
			if err := json.Unmarshal(terminal.Result.StructuredOutput, &output); err != nil || output.SchemaVersion != "symmetry.task_result.v1" || output.Kind != "progress" {
				t.Fatalf("structured_output = %s (%v), want TaskResult object", terminal.Result.StructuredOutput, err)
			}
		})
	}
}

func TestDecoderExplicitlyDecodesControlRequestBeforeRejectingIt(t *testing.T) {
	events, err := NewDecoder(1024).Feed([]byte("{\"type\":\"control_request\",\"session_id\":\"s\",\"request_id\":\"r-1\",\"request\":{\"kind\":\"permission\"}}\n"))
	if err != nil || len(events) != 1 {
		t.Fatalf("Feed() = %#v, %v", events, err)
	}
	control := events[0].ControlRequest
	if events[0].DecodeError != nil || control == nil || control.RequestID != "r-1" || string(control.Request) != "{\"kind\":\"permission\"}" {
		t.Fatalf("control event = %+v, want decoded control request", events[0])
	}
	if err := NewTerminalValidator("s").Observe(events[0]); !errors.Is(err, ErrControlRequestUnsupported) {
		t.Fatalf("Observe() error = %v, want ErrControlRequestUnsupported", err)
	}
}

func TestTerminalValidatorRejectsErrorResultsControlRequestsAndConflicts(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  error
	}{
		{name: "error result", input: "{\"type\":\"result\",\"subtype\":\"error_during_execution\",\"is_error\":true,\"session_id\":\"s\"}\n", want: ErrResultReportedError},
		{name: "unexpected success subtype", input: "{\"type\":\"result\",\"subtype\":\"future_success\",\"is_error\":false,\"session_id\":\"s\"}\n", want: ErrUnexpectedResultSubtype},
		{name: "prompt too long terminal reason", input: "{\"type\":\"result\",\"subtype\":\"success\",\"terminal_reason\":\"prompt_too_long\",\"is_error\":false,\"session_id\":\"s\",\"result\":\"unexpected\"}\n", want: ErrNonSuccessTerminalReason},
		{name: "other non success terminal reason", input: "{\"type\":\"result\",\"subtype\":\"success\",\"terminal_reason\":\"max_turns\",\"is_error\":false,\"session_id\":\"s\",\"result\":\"unexpected\"}\n", want: ErrNonSuccessTerminalReason},
		{name: "control request", input: "{\"type\":\"control_request\",\"session_id\":\"s\",\"request_id\":\"r-1\",\"request\":{\"kind\":\"permission\"}}\n", want: ErrControlRequestUnsupported},
		{name: "expected conflict", input: "{\"type\":\"system\",\"session_id\":\"other\"}\n", want: ErrConflictingIdentity},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			events, err := NewDecoder(1024).Feed([]byte(test.input))
			if err != nil || len(events) != 1 {
				t.Fatalf("Feed() = %#v, %v", events, err)
			}
			validator := NewTerminalValidator("s")
			if err := validator.Observe(events[0]); !errors.Is(err, test.want) {
				t.Fatalf("Observe() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestTerminalValidatorRejectsRepeatedAndPostTerminalRecords(t *testing.T) {
	decoder := NewDecoder(1024)
	events, err := decoder.Feed([]byte("{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"session_id\":\"s\",\"structured_output\":{}}\n{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"session_id\":\"s\",\"structured_output\":{}}\n"))
	if err != nil || len(events) != 2 {
		t.Fatalf("Feed() = %#v, %v", events, err)
	}
	validator := NewTerminalValidator("s")
	if err := validator.Observe(events[0]); err != nil {
		t.Fatalf("first Observe() error = %v", err)
	}
	if err := validator.Observe(events[1]); !errors.Is(err, ErrRepeatedResult) {
		t.Fatalf("second Observe() error = %v, want ErrRepeatedResult", err)
	}
}

func TestTerminalValidatorRejectsIdentityChangesAndTrailingRecords(t *testing.T) {
	decoder := NewDecoder(1024)
	events, err := decoder.Feed([]byte("{\"type\":\"system\",\"session_id\":\"s\"}\n{\"type\":\"assistant\",\"session_id\":\"other\"}\n"))
	if err != nil || len(events) != 2 {
		t.Fatalf("Feed() = %#v, %v", events, err)
	}
	validator := NewTerminalValidator("")
	if err := validator.Observe(events[0]); err != nil {
		t.Fatalf("first Observe() error = %v", err)
	}
	if err := validator.Observe(events[1]); !errors.Is(err, ErrConflictingIdentity) {
		t.Fatalf("second Observe() error = %v, want ErrConflictingIdentity", err)
	}

	events, err = NewDecoder(1024).Feed([]byte("{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"session_id\":\"s\",\"structured_output\":{}}\n{\"type\":\"log\",\"session_id\":\"s\"}\n"))
	if err != nil || len(events) != 2 {
		t.Fatalf("trailing Feed() = %#v, %v", events, err)
	}
	validator = NewTerminalValidator("s")
	if err := validator.Observe(events[0]); err != nil {
		t.Fatalf("result Observe() error = %v", err)
	}
	if err := validator.Observe(events[1]); !errors.Is(err, ErrEventAfterResult) {
		t.Fatalf("trailing Observe() error = %v, want ErrEventAfterResult", err)
	}
}

func TestDecoderRejectsOversizeAndTruncatedRecords(t *testing.T) {
	decoder := NewDecoder(8)
	if _, err := decoder.Feed([]byte("123456789")); !errors.Is(err, ErrRecordTooLarge) {
		t.Fatalf("oversized Feed() error = %v, want ErrRecordTooLarge", err)
	}
	decoder = NewDecoder(1024)
	if _, err := decoder.Feed([]byte("{\"type\":\"system\"}")); err != nil {
		t.Fatalf("partial Feed() error = %v", err)
	}
	events, err := decoder.Close()
	if !errors.Is(err, ErrIncompleteRecord) || len(events) != 1 || !errors.Is(events[0].DecodeError, ErrIncompleteRecord) {
		t.Fatalf("Close() = %#v, %v; want incomplete invalid event", events, err)
	}
	validator := NewTerminalValidator("")
	if err := validator.Observe(events[0]); !errors.Is(err, ErrIncompleteRecord) {
		t.Fatalf("Observe() error = %v, want ErrIncompleteRecord", err)
	}
}
