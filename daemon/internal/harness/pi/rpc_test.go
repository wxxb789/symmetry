package pi

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestDecoderReadsVersionedLifecycleFixtureAcrossArbitraryBoundaries(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("..", "testdata", "pi", "0.85.1", "lifecycle.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	fixture = []byte(strings.ReplaceAll(string(fixture), "\n", "\r\n"))
	decoder := NewDecoder(4 * 1024)
	var records []Record
	for _, chunk := range [][]byte{fixture[:11], fixture[11:219], fixture[219:617], fixture[617:]} {
		decoded, err := decoder.Feed(chunk)
		if err != nil {
			t.Fatalf("Feed() error = %v", err)
		}
		records = append(records, decoded...)
	}
	if trailing, err := decoder.Close(); err != nil || len(trailing) != 0 {
		t.Fatalf("Close() = %#v, %v; want no trailing data", trailing, err)
	}
	if len(records) != 10 {
		t.Fatalf("records = %d, want 10", len(records))
	}
	validator := NewValidator(nil)
	state, err := GetStateRequest("state-1")
	if err != nil {
		t.Fatalf("GetStateRequest() error = %v", err)
	}
	prompt, err := PromptRequest("prompt-1", "perform bounded work", "")
	if err != nil {
		t.Fatalf("PromptRequest() error = %v", err)
	}
	for _, request := range []Request{state, prompt} {
		if err := validator.Register(request); err != nil {
			t.Fatalf("Register(%+v) error = %v", request, err)
		}
	}
	for _, record := range records {
		if err := validator.Observe(record); err != nil {
			t.Fatalf("Observe(record %d) error = %v", record.Sequence, err)
		}
	}
	completion, err := validator.Finish()
	if err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	if completion.State.SessionID != "pi-session-1" || completion.State.SessionFile != "C:/sessions/pi-session-1.jsonl" {
		t.Fatalf("completion state = %+v", completion.State)
	}
	if !bytes.Contains(completion.FinalAssistant, []byte(`"stopReason":"stop"`)) {
		t.Fatalf("final assistant = %s, want exact native final message", completion.FinalAssistant)
	}
	if _, err := DecodeTaskResultJSON(completion.FinalAssistant); !errors.Is(err, ErrInvalidTaskResult) {
		t.Fatalf("DecodeTaskResultJSON(message) error = %v, want explicit full-result rejection", err)
	}
}

func TestValidatorDoesNotTreatPromptAcceptanceOrAgentEndAsFinal(t *testing.T) {
	validator := registeredValidator(t)
	observeJSON(t, validator, `{"type":"response","id":"state-1","command":"get_state","success":true,"data":{"sessionId":"s","sessionFile":"C:/s.jsonl"}}`)
	observeJSON(t, validator, `{"type":"response","id":"prompt-1","command":"prompt","success":true}`)
	observeJSON(t, validator, `{"type":"agent_start"}`)
	observeJSON(t, validator, `{"type":"message_end","message":{"role":"assistant","content":"progress","stopReason":"stop"}}`)
	observeJSON(t, validator, `{"type":"agent_end","messages":[],"willRetry":true}`)
	if _, err := validator.Finish(); !errors.Is(err, ErrNotSettled) {
		t.Fatalf("Finish() after agent_end error = %v, want ErrNotSettled", err)
	}
	observeJSON(t, validator, `{"type":"auto_retry_start"}`)
	observeJSON(t, validator, `{"type":"agent_start"}`)
	observeJSON(t, validator, `{"type":"message_end","message":{"role":"assistant","content":"done","stopReason":"stop"}}`)
	observeJSON(t, validator, `{"type":"agent_end","messages":[],"willRetry":false}`)
	observeJSON(t, validator, `{"type":"agent_settled"}`)
	if _, err := validator.Finish(); err != nil {
		t.Fatalf("Finish() after agent_settled error = %v", err)
	}
}

func TestValidatorAllowsEventBeforePromptResponseButRequiresAcceptance(t *testing.T) {
	validator := registeredValidator(t)
	observeJSON(t, validator, `{"type":"response","id":"state-1","command":"get_state","success":true,"data":{"sessionId":"s","sessionFile":"C:/s.jsonl"}}`)
	observeJSON(t, validator, `{"type":"agent_start"}`)
	observeJSON(t, validator, `{"type":"message_end","message":{"role":"assistant","content":"done","stopReason":"stop"}}`)
	observeJSON(t, validator, `{"type":"agent_end","messages":[]}`)
	observeJSON(t, validator, `{"type":"agent_settled"}`)
	if _, err := validator.Finish(); !errors.Is(err, ErrPromptNotAccepted) {
		t.Fatalf("Finish() before prompt response error = %v, want ErrPromptNotAccepted", err)
	}
	observeJSON(t, validator, `{"type":"response","id":"prompt-1","command":"prompt","success":true}`)
	if _, err := validator.Finish(); err != nil {
		t.Fatalf("Finish() after prompt response error = %v", err)
	}
}

func TestValidatorRejectsCorrelationAndStateIdentityFailures(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  error
	}{
		{name: "unknown response", input: `{"type":"response","id":"other","command":"get_state","success":true,"data":{}}`, want: ErrUnknownResponse},
		{name: "mismatched command", input: `{"type":"response","id":"state-1","command":"abort","success":true}`, want: ErrResponseCommandMismatch},
		{name: "rejected command", input: `{"type":"response","id":"state-1","command":"get_state","success":false,"error":"not ready"}`, want: ErrResponseFailed},
		{name: "missing state identity", input: `{"type":"response","id":"state-1","command":"get_state","success":true,"data":{"sessionId":"s"}}`, want: ErrMissingSessionState},
		{name: "expected state conflict", input: `{"type":"response","id":"state-1","command":"get_state","success":true,"data":{"sessionId":"other","sessionFile":"C:/s.jsonl"}}`, want: ErrConflictingSessionState},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validator := registeredValidator(t)
			if test.name == "expected state conflict" {
				validator = NewValidator(&SessionState{SessionID: "s", SessionFile: "C:/s.jsonl"})
				state, err := GetStateRequest("state-1")
				if err != nil {
					t.Fatal(err)
				}
				prompt, err := PromptRequest("prompt-1", "work", "")
				if err != nil {
					t.Fatal(err)
				}
				if err := validator.Register(state); err != nil {
					t.Fatal(err)
				}
				if err := validator.Register(prompt); err != nil {
					t.Fatal(err)
				}
			}
			record := decodeOne(t, test.input)
			if err := validator.Observe(record); !errors.Is(err, test.want) {
				t.Fatalf("Observe() error = %v, want %v", err, test.want)
			}
			if _, err := validator.Finish(); !errors.Is(err, test.want) {
				t.Fatalf("Finish() error = %v, want retained failure %v", err, test.want)
			}
		})
	}

	validator := registeredValidator(t)
	observeJSON(t, validator, `{"type":"response","id":"state-1","command":"get_state","success":true,"data":{"sessionId":"s","sessionFile":"C:/s.jsonl"}}`)
	if err := validator.Observe(decodeOne(t, `{"type":"response","id":"state-1","command":"get_state","success":true,"data":{"sessionId":"s","sessionFile":"C:/s.jsonl"}}`)); !errors.Is(err, ErrDuplicateResponse) {
		t.Fatalf("duplicate response error = %v, want ErrDuplicateResponse", err)
	}
}

func TestClearThenAbortRequestsAreCorrelatedAndOrdered(t *testing.T) {
	requests, err := ClearThenAbortRequests("clear-1", "abort-1")
	if err != nil {
		t.Fatalf("ClearThenAbortRequests() error = %v", err)
	}
	if requests[0].Type != CommandClearQueue || requests[1].Type != CommandAbort {
		t.Fatalf("requests = %+v, want clear_queue then abort", requests)
	}
	validator := NewValidator(nil)
	for _, request := range requests {
		if err := validator.Register(request); err != nil {
			t.Fatalf("Register(%+v) error = %v", request, err)
		}
	}
	if err := validator.Observe(decodeOne(t, `{"type":"response","id":"abort-1","command":"abort","success":true}`)); err != nil {
		t.Fatalf("out-of-order abort response error = %v", err)
	}
	if err := validator.Observe(decodeOne(t, `{"type":"response","id":"clear-1","command":"clear_queue","success":true,"data":{"steering":[],"followUp":[]}}`)); err != nil {
		t.Fatalf("clear_queue response error = %v", err)
	}
	if _, err := ClearThenAbortRequests("same", "same"); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("same IDs error = %v, want ErrInvalidRequest", err)
	}
}

func TestDecoderFailsClosedForMalformedUnknownDuplicateOversizeAndTruncatedRecords(t *testing.T) {
	tests := []struct {
		name  string
		input []byte
		want  error
	}{
		{name: "invalid utf8", input: []byte{'{', 0xff, '}', '\n'}, want: ErrInvalidUTF8},
		{name: "malformed", input: []byte("{invalid}\n"), want: ErrMalformedJSON},
		{name: "non object", input: []byte("[]\n"), want: ErrNonObjectRecord},
		{name: "duplicate type", input: []byte(`{"type":"agent_start","type":"agent_end"}` + "\n"), want: ErrDuplicateField},
		{name: "invalid response boolean", input: []byte(`{"type":"response","id":"r","command":"prompt","success":null}` + "\n"), want: ErrInvalidResponseSuccess},
		{name: "unknown event", input: []byte(`{"type":"future_event"}` + "\n"), want: ErrUnsupportedEvent},
		{name: "extension ui", input: []byte(`{"type":"extension_ui_request","id":"ui-1","method":"confirm"}` + "\n"), want: ErrExtensionUIUnsupported},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			records, err := NewDecoder(1024).Feed(test.input)
			if err != nil || len(records) != 1 {
				t.Fatalf("Feed() = %#v, %v; want one diagnostic record", records, err)
			}
			if !errors.Is(records[0].DecodeError, test.want) {
				t.Fatalf("DecodeError = %v, want %v", records[0].DecodeError, test.want)
			}
			validator := NewValidator(nil)
			if err := validator.Observe(records[0]); !errors.Is(err, test.want) {
				t.Fatalf("Observe() error = %v, want %v", err, test.want)
			}
		})
	}

	decoder := NewDecoder(8)
	if _, err := decoder.Feed(bytes.Repeat([]byte{'x'}, 9)); !errors.Is(err, ErrRecordTooLarge) {
		t.Fatalf("oversize Feed() error = %v, want ErrRecordTooLarge", err)
	}
	if _, err := decoder.Feed([]byte(`{"type":"agent_settled"}` + "\n")); !errors.Is(err, ErrRecordTooLarge) {
		t.Fatalf("Feed after overflow error = %v, want retained ErrRecordTooLarge", err)
	}
	decoder = NewDecoder(1024)
	if _, err := decoder.Feed([]byte(`{"type":"agent_settled"}`)); err != nil {
		t.Fatalf("partial Feed() error = %v", err)
	}
	records, err := decoder.Close()
	if !errors.Is(err, ErrIncompleteRecord) || len(records) != 1 || !errors.Is(records[0].DecodeError, ErrIncompleteRecord) {
		t.Fatalf("Close() = %#v, %v; want incomplete record", records, err)
	}
}

func TestDecoderUsesLFOnlyAndPreservesUnicodeLineSeparators(t *testing.T) {
	decoder := NewDecoder(1024)
	chunk := []byte("{\"type\":\"message_end\",\"message\":{\"role\":\"assistant\",\"content\":\"before\\u2028after\\u2029still one record\",\"stopReason\":\"stop\"}}\n")
	records, err := decoder.Feed(chunk)
	if err != nil || len(records) != 1 || records[0].DecodeError != nil {
		t.Fatalf("Feed() = %#v, %v; want one valid record", records, err)
	}
}

func TestDecoderTreatsCRLFAsTheSameBoundedRecordAndRejectsNestedDuplicateFields(t *testing.T) {
	record := `{"type":"message_end","message":{"role":"assistant","content":"x","stopReason":"stop"}}`
	decoder := NewDecoder(len(record))
	records, err := decoder.Feed([]byte(record + "\r\n"))
	if err != nil || len(records) != 1 || records[0].DecodeError != nil {
		t.Fatalf("CRLF bounded Feed() = %#v, %v; want one valid record", records, err)
	}

	decoder = NewDecoder(1024)
	records, err = decoder.Feed([]byte(`{"type":"message_end","message":{"role":"assistant","role":"user"}}` + "\n"))
	if err != nil || len(records) != 1 || !errors.Is(records[0].DecodeError, ErrDuplicateField) {
		t.Fatalf("nested duplicate Feed() = %#v, %v; want ErrDuplicateField", records, err)
	}
}

func TestValidatorRejectsDuplicateSettledTrailingEventAndBadFinalAssistant(t *testing.T) {
	validator := readyToSettle(t, `{"role":"assistant","content":"done","stopReason":"aborted"}`)
	if _, err := validator.Finish(); !errors.Is(err, ErrUnexpectedAssistantStop) {
		t.Fatalf("Finish() error = %v, want ErrUnexpectedAssistantStop", err)
	}

	validator = readyToSettle(t, `{"role":"assistant","content":"done","stopReason":"stop"}`)
	if err := validator.Observe(decodeOne(t, `{"type":"agent_settled"}`)); !errors.Is(err, ErrDuplicateSettled) {
		t.Fatalf("duplicate settled error = %v, want ErrDuplicateSettled", err)
	}

	validator = readyToSettle(t, `{"role":"assistant","content":"done","stopReason":"stop"}`)
	if err := validator.Observe(decodeOne(t, `{"type":"queue_update"}`)); !errors.Is(err, ErrEventAfterSettled) {
		t.Fatalf("trailing event error = %v, want ErrEventAfterSettled", err)
	}
}

func TestValidatorBindsFinalAssistantToTheLastLowLevelRun(t *testing.T) {
	validator := registeredValidator(t)
	observeJSON(t, validator, `{"type":"response","id":"state-1","command":"get_state","success":true,"data":{"sessionId":"s","sessionFile":"C:/s.jsonl"}}`)
	observeJSON(t, validator, `{"type":"response","id":"prompt-1","command":"prompt","success":true}`)
	observeJSON(t, validator, `{"type":"agent_start"}`)
	observeJSON(t, validator, `{"type":"message_end","message":{"role":"assistant","content":"old","stopReason":"stop"}}`)
	observeJSON(t, validator, `{"type":"agent_end","messages":[]}`)
	observeJSON(t, validator, `{"type":"agent_start"}`)
	observeJSON(t, validator, `{"type":"agent_end","messages":[]}`)
	observeJSON(t, validator, `{"type":"agent_settled"}`)
	if _, err := validator.Finish(); !errors.Is(err, ErrMissingAssistantMessage) {
		t.Fatalf("Finish() error = %v, want ErrMissingAssistantMessage", err)
	}

	validator = registeredValidator(t)
	observeJSON(t, validator, `{"type":"response","id":"state-1","command":"get_state","success":true,"data":{"sessionId":"s","sessionFile":"C:/s.jsonl"}}`)
	if err := validator.Observe(decodeOne(t, `{"type":"message_end","message":{"role":"assistant","content":"orphan","stopReason":"stop"}}`)); !errors.Is(err, ErrUnexpectedAgentEvent) {
		t.Fatalf("orphan message_end error = %v, want ErrUnexpectedAgentEvent", err)
	}
}

func TestValidatorRejectsRequestTimeoutAndTaskResultDecoderRequiresExplicitFullJSON(t *testing.T) {
	validator := registeredValidator(t)
	if err := validator.Expire("state-1"); !errors.Is(err, ErrRequestTimedOut) {
		t.Fatalf("Expire() error = %v, want ErrRequestTimedOut", err)
	}
	if err := validator.Observe(decodeOne(t, `{"type":"response","id":"state-1","command":"get_state","success":true,"data":{"sessionId":"s","sessionFile":"C:/s.jsonl"}}`)); !errors.Is(err, ErrRequestTimedOut) {
		t.Fatalf("late response error = %v, want retained ErrRequestTimedOut", err)
	}

	valid := validTaskResultJSON(t)
	if result, err := DecodeTaskResultJSON(json.RawMessage(valid)); err != nil || result.Kind != protocol.TaskResultProgress {
		t.Fatalf("DecodeTaskResultJSON(valid) = %#v, %v", result, err)
	}
	for _, input := range []string{
		"```json\n" + valid + "\n```",
		"prose " + valid,
		valid + " " + valid,
		`{"role":"assistant","content":` + strconvQuote(valid) + `,"stopReason":"stop"}`,
	} {
		if _, err := DecodeTaskResultJSON(json.RawMessage(input)); !errors.Is(err, ErrInvalidTaskResult) {
			t.Fatalf("DecodeTaskResultJSON(%q) error = %v, want ErrInvalidTaskResult", input, err)
		}
	}
}

func TestValidatorDoesNotFinishWhileARegisteredRequestLacksItsResponse(t *testing.T) {
	validator := readyToSettle(t, `{"role":"assistant","content":"done","stopReason":"stop"}`)
	request, err := GetStateRequest("state-2")
	if err != nil {
		t.Fatalf("GetStateRequest() error = %v", err)
	}
	if err := validator.Register(request); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if _, err := validator.Finish(); !errors.Is(err, ErrPendingResponse) {
		t.Fatalf("Finish() error = %v, want ErrPendingResponse", err)
	}
	observeJSON(t, validator, `{"type":"response","id":"state-2","command":"get_state","success":true,"data":{"sessionId":"s","sessionFile":"C:/s.jsonl"}}`)
	if _, err := validator.Finish(); err != nil {
		t.Fatalf("Finish() after response error = %v", err)
	}
}

func registeredValidator(t *testing.T) *Validator {
	t.Helper()
	validator := NewValidator(nil)
	state, err := GetStateRequest("state-1")
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := PromptRequest("prompt-1", "work", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, request := range []Request{state, prompt} {
		if err := validator.Register(request); err != nil {
			t.Fatal(err)
		}
	}
	return validator
}

func readyToSettle(t *testing.T, assistant string) *Validator {
	t.Helper()
	validator := registeredValidator(t)
	observeJSON(t, validator, `{"type":"response","id":"state-1","command":"get_state","success":true,"data":{"sessionId":"s","sessionFile":"C:/s.jsonl"}}`)
	observeJSON(t, validator, `{"type":"response","id":"prompt-1","command":"prompt","success":true}`)
	observeJSON(t, validator, `{"type":"agent_start"}`)
	observeJSON(t, validator, `{"type":"message_end","message":`+assistant+`}`)
	observeJSON(t, validator, `{"type":"agent_end","messages":[]}`)
	observeJSON(t, validator, `{"type":"agent_settled"}`)
	return validator
}

func observeJSON(t *testing.T, validator *Validator, input string) {
	t.Helper()
	if err := validator.Observe(decodeOne(t, input)); err != nil {
		t.Fatalf("Observe(%s) error = %v", input, err)
	}
}

func decodeOne(t *testing.T, input string) Record {
	t.Helper()
	records, err := NewDecoder(16 * 1024).Feed([]byte(input + "\n"))
	if err != nil || len(records) != 1 {
		t.Fatalf("decode %s = %#v, %v", input, records, err)
	}
	return records[0]
}

func validTaskResultJSON(t *testing.T) string {
	t.Helper()
	subject := protocol.Subject{
		ResourceID: "00000000-0000-4000-8000-000000000007",
		Commit:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TreeDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}
	subjectHash, err := subject.Hash()
	if err != nil {
		t.Fatalf("hash subject: %v", err)
	}
	encoded, err := json.Marshal(map[string]any{
		"schema_version":       "symmetry.task_result.v1",
		"result_id":            "00000000-0000-4000-8000-000000000006",
		"kind":                 "progress",
		"summary":              "made bounded progress",
		"subject":              subject,
		"subject_hash":         subjectHash,
		"evidence_refs":        []string{},
		"blocker":              nil,
		"proposed_next_action": nil,
		"proposal":             nil,
		"reason":               nil,
		"diagnostics":          []any{},
	})
	if err != nil {
		t.Fatalf("marshal task result: %v", err)
	}
	return string(encoded)
}

func strconvQuote(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
