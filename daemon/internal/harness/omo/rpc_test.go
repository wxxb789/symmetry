package omo

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func wireRecord(t *testing.T, value any) Record {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	record := decodeRecord(1, encoded)
	if record.DecodeError != nil {
		t.Fatal(record.DecodeError)
	}
	return record
}

func protocolData() map[string]any {
	return map[string]any{"protocolVersion": 1, "serverVersion": "5.1.69", "engineVersion": "5.1.69", "mode": "classic", "capabilities": []string{"durable_client_message_id", "effect_ledger"}}
}

func stateData(filename string) map[string]any {
	return map[string]any{"sessionId": "native-1", "sessionFile": filename, "isStreaming": false, "isCompacting": false, "pendingMessageCount": 0, "steering": []string{}, "followUp": []string{}, "pendingDialogs": []any{}, "permissionPreset": "workspace", "permissionPresetStatus": "applied", "effectivePermissionPreset": "workspace"}
}

func responseRecord(t *testing.T, id string, command Command, data any) Record {
	t.Helper()
	return wireRecord(t, map[string]any{"type": "response", "id": id, "command": command, "success": true, "data": data})
}

func promptData(request Request) map[string]any {
	return map[string]any{"disposition": "started", "clientMessageId": request.ClientMessageID, "clientTurnId": request.ClientTurnID,
		"admission": map[string]any{"durableSessionId": "native-1", "clientMessageId": request.ClientMessageID, "clientTurnId": request.ClientTurnID, "state": "running", "disposition": "started"}}
}

func preparedValidator(t *testing.T) (*Validator, Request) {
	t.Helper()
	validator := NewValidator(nil)
	if err := validator.Register(Request{ID: "protocol", Type: CommandProtocol}); err != nil {
		t.Fatal(err)
	}
	if err := validator.Observe(responseRecord(t, "protocol", CommandProtocol, protocolData())); err != nil {
		t.Fatal(err)
	}
	if err := validator.Register(Request{ID: "state", Type: CommandGetState}); err != nil {
		t.Fatal(err)
	}
	if err := validator.Observe(responseRecord(t, "state", CommandGetState, stateData(filepath.Join(t.TempDir(), "retained.jsonl")))); err != nil {
		t.Fatal(err)
	}
	request := Request{ID: "prompt", Type: CommandPrompt, Message: "Complete the engineering task", ClientMessageID: "admission-input", ClientTurnID: "admission-turn"}
	if err := validator.Register(request); err != nil {
		t.Fatal(err)
	}
	return validator, request
}

func identifiedEvent(kind string, request Request) map[string]any {
	return map[string]any{"type": kind, "clientMessageId": request.ClientMessageID, "clientTurnId": request.ClientTurnID}
}

func assistantMessage(t *testing.T) map[string]any {
	t.Helper()
	subject := protocol.Subject{ResourceID: "00000000-0000-4000-8000-000000000007", Commit: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", TreeDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000"}
	hash, err := subject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	result, err := json.Marshal(map[string]any{"schema_version": "symmetry.task_result.v1", "result_id": "00000000-0000-4000-8000-000000000006", "kind": "progress", "summary": "Changed the requested artifact", "subject": subject, "subject_hash": hash, "evidence_refs": []string{}, "blocker": nil, "proposed_next_action": nil, "proposal": nil, "reason": nil, "diagnostics": []any{}})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"role": "assistant", "stopReason": "stop", "content": []any{map[string]any{"type": "text", "text": string(result)}}}
}

func completionEvents(t *testing.T, request Request, message any) []map[string]any {
	t.Helper()
	end := identifiedEvent("message_end", request)
	end["message"] = message
	agentEnd := identifiedEvent("agent_end", request)
	agentEnd["messages"], agentEnd["willRetry"] = []any{message}, false
	return []map[string]any{{"type": "agent_start", "runId": "run-1"}, end, agentEnd, {"type": "agent_settled"}, {"type": "agent_idle"}}
}

func TestDecoderRejectsAmbiguousOrUnboundedJSONL(t *testing.T) {
	for name, data := range map[string][]byte{
		"duplicate response identity": []byte(`{"type":"response","id":"one","id":"two","command":"prompt","success":true}` + "\n"),
		"nested duplicate":            []byte(`{"type":"unknown","payload":{"a":1,"a":2}}` + "\n"),
		"invalid UTF8":                append([]byte(`{"type":"`), 0xff, '"', '}', '\n'),
		"non-object":                  []byte("[]\n"),
		"null success":                []byte(`{"type":"response","id":"one","command":"prompt","success":null}` + "\n"),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewDecoder(1024).Feed(data); err == nil {
				t.Fatal("malformed frame accepted")
			}
		})
	}
	decoder := NewDecoder(32)
	if _, err := decoder.Feed([]byte(`{"type":"notice"}`)); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Close(); err == nil {
		t.Fatal("truncated JSONL record accepted")
	}
	decoder = NewDecoder(8)
	if _, err := decoder.Feed([]byte("123456789")); err == nil {
		t.Fatal("unbounded frame accepted")
	}
	decoder = NewDecoder(64)
	if records, err := decoder.Feed([]byte(`{"type":"not`)); err != nil || len(records) != 0 {
		t.Fatal("split frame rejected")
	}
	if records, err := decoder.Feed([]byte("ice\"}\r\n")); err != nil || len(records) != 1 || records[0].Type != "notice" {
		t.Fatalf("split record = %+v, %v", records, err)
	}
}

const boundErrorText = "invalid OmO RPC record: JSONL record exceeds limit"

func TestDecoderCountsTrailingCRTowardTheLimit(t *testing.T) {
	record := `{"type":"notice"}`
	decoder := NewDecoder(len(record))
	records, err := decoder.Feed([]byte(record + "\r\n"))
	if !errors.Is(err, ErrInvalidRecord) || len(records) != 0 {
		t.Fatalf("CRLF record at the limit = %+v, %v; want the bound error", records, err)
	}
	if err.Error() != boundErrorText {
		t.Fatalf("CRLF record error = %q, want the bound text", err)
	}
	decoder = NewDecoder(len(record) + 1)
	if records, err = decoder.Feed([]byte(record + "\r\n")); err != nil || len(records) != 1 || records[0].Type != "notice" {
		t.Fatalf("CRLF record over the limit = %+v, %v; want one record", records, err)
	}
}

func TestDecoderTreatsBlankLinesAsRecords(t *testing.T) {
	decoder := NewDecoder(64)
	records, err := decoder.Feed([]byte("  \n"))
	if !errors.Is(err, ErrInvalidRecord) || len(records) != 1 || !errors.Is(records[0].DecodeError, ErrInvalidRecord) {
		t.Fatalf("blank line = %+v, %v; want one invalid record", records, err)
	}
}

func TestDecoderRetainsTheBoundFailureAcrossCalls(t *testing.T) {
	decoder := NewDecoder(8)
	if _, err := decoder.Feed([]byte("123456789")); err == nil || err.Error() != boundErrorText {
		t.Fatalf("oversize Feed() error = %v, want the bound error", err)
	}
	if _, err := decoder.Feed(nil); err == nil || err.Error() != boundErrorText {
		t.Fatalf("Feed after overflow error = %v, want the retained bound error", err)
	}
	if err := decoder.Close(); err == nil || err.Error() != boundErrorText {
		t.Fatalf("Close after overflow error = %v, want the retained bound error", err)
	}
}

func TestDecoderZeroValueKeepsTheZeroBound(t *testing.T) {
	var decoder Decoder
	records, err := decoder.Feed([]byte("\n"))
	if !errors.Is(err, ErrInvalidRecord) || err.Error() == boundErrorText || len(records) != 1 {
		t.Fatalf("blank-line Feed() = %+v, %v; want the blank line delivered as a decode error", records, err)
	}
	if err := decoder.Close(); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("Close after the decode failure error = %v, want the retained error", err)
	}

	var bounded Decoder
	if _, err := bounded.Feed([]byte("123456789")); err == nil || err.Error() != boundErrorText {
		t.Fatalf("zero-bound Feed() error = %v, want the bound error", err)
	}
	if err := bounded.Close(); err == nil || err.Error() != boundErrorText {
		t.Fatalf("zero-bound Close() error = %v, want the retained bound error", err)
	}
}

func TestProtocolGateRejectsBeforeSessionExposure(t *testing.T) {
	for _, mutation := range []struct {
		name, key string
		value     any
	}{
		{"unknown protocol", "protocolVersion", 2}, {"wrong engine", "engineVersion", "2026.11.10-2"}, {"multi mode", "mode", "multi"}, {"missing durable effects", "capabilities", []string{"durable_client_message_id"}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			validator := NewValidator(nil)
			if err := validator.Register(Request{ID: "probe", Type: CommandProtocol}); err != nil {
				t.Fatal(err)
			}
			data := protocolData()
			data[mutation.key] = mutation.value
			if err := validator.Observe(responseRecord(t, "probe", CommandProtocol, data)); err == nil {
				t.Fatal("incompatible protocol accepted")
			}
			if _, ok := validator.SessionState(); ok {
				t.Fatal("protocol failure exposed a native handle")
			}
		})
	}
}

func TestLatePromptACKCanOnlyConfirmItsOriginalAdmission(t *testing.T) {
	validator, request := preparedValidator(t)
	if err := validator.Expire(request.ID); !errors.Is(err, ErrUnknownOutcome) {
		t.Fatal(err)
	}
	for _, frame := range completionEvents(t, request, assistantMessage(t)) {
		if err := validator.Observe(wireRecord(t, frame)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := validator.Finish(); !errors.Is(err, ErrPromptNotAccepted) {
		t.Fatalf("terminal without acceptance = %v", err)
	}
	if err := validator.Observe(responseRecord(t, request.ID, CommandPrompt, promptData(request))); err != nil {
		t.Fatal(err)
	}
	message, err := validator.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if result, err := DecodeAssistantTaskResult(message); err != nil || result.Summary != "Changed the requested artifact" {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if err := validator.Observe(responseRecord(t, request.ID, CommandPrompt, promptData(request))); !errors.Is(err, ErrCorrelation) {
		t.Fatalf("duplicate ACK = %v", err)
	}
}

func TestWrongCommandIdentityAndTerminalOrderingAreRejected(t *testing.T) {
	for _, name := range []string{"wrong command", "unknown request", "other input", "terminal before start", "duplicate terminal"} {
		t.Run(name, func(t *testing.T) {
			validator, request := preparedValidator(t)
			var invalid Record
			switch name {
			case "wrong command":
				invalid = responseRecord(t, request.ID, CommandGetState, stateData(filepath.Join(t.TempDir(), "x")))
			case "unknown request":
				invalid = responseRecord(t, "different-request", CommandPrompt, promptData(request))
			case "other input":
				if err := validator.Observe(wireRecord(t, map[string]any{"type": "agent_start"})); err != nil {
					t.Fatal(err)
				}
				frame := identifiedEvent("message_end", request)
				frame["clientMessageId"], frame["message"] = "foreign-input", assistantMessage(t)
				invalid = wireRecord(t, frame)
			case "terminal before start":
				invalid = wireRecord(t, map[string]any{"type": "agent_settled"})
			case "duplicate terminal":
				for _, frame := range completionEvents(t, request, assistantMessage(t)) {
					if err := validator.Observe(wireRecord(t, frame)); err != nil {
						t.Fatal(err)
					}
				}
				invalid = wireRecord(t, map[string]any{"type": "agent_idle"})
			}
			if err := validator.Observe(invalid); err == nil {
				t.Fatal("ambiguous frame accepted")
			}
		})
	}
}

func TestSettledNeedsIdleAndCompleteStructuredOutput(t *testing.T) {
	validator, request := preparedValidator(t)
	if err := validator.Observe(responseRecord(t, request.ID, CommandPrompt, promptData(request))); err != nil {
		t.Fatal(err)
	}
	message := assistantMessage(t)
	message["content"] = []any{map[string]any{"type": "text", "text": "Here is the result: {}"}}
	frames := completionEvents(t, request, message)
	for _, frame := range frames[:len(frames)-1] {
		if err := validator.Observe(wireRecord(t, frame)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := validator.Finish(); !errors.Is(err, ErrNotSettled) {
		t.Fatalf("agent_settled alone = %v", err)
	}
	if err := validator.Observe(wireRecord(t, frames[len(frames)-1])); err != nil {
		t.Fatal(err)
	}
	raw, err := validator.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeAssistantTaskResult(raw); !errors.Is(err, ErrMissingResult) {
		t.Fatalf("prose became a result: %v", err)
	}
}

func TestMissingIdleFieldsAndActionableUIFailClosed(t *testing.T) {
	for _, field := range []string{"isStreaming", "isCompacting", "pendingMessageCount", "steering", "followUp"} {
		data := stateData(filepath.Join(t.TempDir(), "retained"))
		delete(data, field)
		raw, _ := json.Marshal(data)
		if _, err := decodeSessionState(raw); err == nil {
			t.Fatalf("omitted %s was treated as idle", field)
		}
	}
	for _, method := range []string{"confirm", "question", "unknown_ui"} {
		validator := NewValidator(nil)
		if err := validator.Observe(wireRecord(t, map[string]any{"type": "extension_ui_request", "id": "ui-1", "method": method})); err == nil {
			t.Fatalf("%s UI did not fail closed", method)
		}
	}
	validator := NewValidator(nil)
	if err := validator.Observe(wireRecord(t, map[string]any{"type": "extension_ui_request", "id": "ui-1", "method": "notify", "message": "local native notification"})); err != nil {
		t.Fatal(err)
	}
	if _, err := validator.Finish(); err == nil {
		t.Fatal("notification became success")
	}
}

func TestSettledWithoutFinalAssistantCannotBecomeResult(t *testing.T) {
	validator, request := preparedValidator(t)
	if err := validator.Observe(responseRecord(t, request.ID, CommandPrompt, promptData(request))); err != nil {
		t.Fatal(err)
	}
	end := identifiedEvent("agent_end", request)
	end["messages"], end["willRetry"] = []any{}, false
	for _, frame := range []any{map[string]any{"type": "agent_start"}, end, map[string]any{"type": "agent_settled"}, map[string]any{"type": "agent_idle"}} {
		if err := validator.Observe(wireRecord(t, frame)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := validator.Finish(); !errors.Is(err, ErrMissingResult) {
		t.Fatalf("settlement without final assistant = %v", err)
	}
}

func TestAgentEndCannotSubstituteAnotherAssistantResult(t *testing.T) {
	validator, request := preparedValidator(t)
	if err := validator.Observe(responseRecord(t, request.ID, CommandPrompt, promptData(request))); err != nil {
		t.Fatal(err)
	}
	frames := completionEvents(t, request, assistantMessage(t))
	other := assistantMessage(t)
	other["content"] = []any{map[string]any{"type": "text", "text": "A different response"}}
	frames[2]["messages"] = []any{other}
	for _, frame := range frames {
		if err := validator.Observe(wireRecord(t, frame)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := validator.Finish(); !errors.Is(err, ErrMissingResult) {
		t.Fatalf("different terminal response became a result: %v", err)
	}
}
