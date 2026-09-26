package harness

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestClaudeCandidateRejectsInvalidStructuredOutputTaskResult(t *testing.T) {
	for _, test := range []struct {
		name     string
		envelope string
	}{
		{name: "schema-invalid object", envelope: `{"type":"result","subtype":"success","terminal_reason":"completed","is_error":false,"session_id":"` + claudeCandidateTestSessionID + `","result":"{}","structured_output":{"kind":"progress"}}`},
		{name: "text result without structured output", envelope: `{"type":"result","subtype":"success","terminal_reason":"completed","is_error":false,"session_id":"` + claudeCandidateTestSessionID + `","result":` + mustMarshalClaudeCandidateString(t, validClaudeCandidateTaskResultJSON(t)) + `}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			process := newClaudeCandidateFakeProcess()
			sink := &recordingClaudeCandidateSink{}
			session, err := newClaudeCandidateTestAdapter(process, nil).Start(context.Background(), claudeCandidateStartRequest(t), sink)
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			cleanupClaudeCandidateSession(t, session)
			staged := session.(StagedSession)
			openClaudeCandidateSession(t, staged, process)
			if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "Make bounded progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
				t.Fatalf("StartTurn() error = %v", err)
			}
			_ = process.emitJSON(test.envelope)
			if err := staged.WaitTurn(context.Background()); err == nil {
				t.Fatal("WaitTurn() accepted a terminal without a valid structured_output TaskResult")
			}
			if err := staged.Close(context.Background()); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			result, err := staged.Wait(context.Background())
			if err != nil {
				t.Fatalf("Wait() error = %v", err)
			}
			if result.Kind != ResultFailed || result.Semantic != nil || result.Reason == nil || *result.Reason != protocol.TaskResultReasonMissingResult {
				t.Fatalf("result = %+v, want failed missing_result without semantic progress", result)
			}
			if _, ok := sink.event(EventTaskResult); ok {
				t.Fatal("invalid structured output published EventTaskResult")
			}
		})
	}
}

func mustMarshalClaudeCandidateString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode string: %v", err)
	}
	return string(encoded)
}

// The Messages API rejects a tool input_schema with a top-level oneOf, anyOf,
// or allOf; Claude Code forwards --json-schema verbatim as that input_schema.
func TestClaudeCandidateStructuredOutputSchemaHasNoTopLevelCombinator(t *testing.T) {
	encoded, err := claudeCandidateStructuredOutputSchema()
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &root); err != nil {
		t.Fatal(err)
	}
	for _, combinator := range []string{"oneOf", "anyOf", "allOf"} {
		if _, ok := root[combinator]; ok {
			t.Fatalf("--json-schema root has %q", combinator)
		}
	}
	for _, key := range []string{"type", "properties", "required", "additionalProperties", "definitions"} {
		if _, ok := root[key]; !ok {
			t.Fatalf("--json-schema root lost %q", key)
		}
	}
}

// Dropping the root coupling cannot admit a TaskResult the full schema rejects.
func TestClaudeCandidateRejectsKindReasonMismatchOutsideToolSchema(t *testing.T) {
	var object map[string]any
	if err := json.Unmarshal([]byte(validClaudeCandidateTaskResultJSON(t)), &object); err != nil {
		t.Fatal(err)
	}
	object["reason"] = "missing_result"
	mismatch, err := json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := protocol.ParseTaskResult(mismatch); err == nil {
		t.Fatal("ParseTaskResult accepted kind=progress with a failure reason")
	}
}
