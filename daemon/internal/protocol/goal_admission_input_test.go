package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseAdmissionInputClassifiesSchemaVersionsAndLegacyMarkers(t *testing.T) {
	valid := string(readGoalSemanticFixture(t, "valid/admission.basic.json"))
	withoutSchema := strings.Replace(valid, `"schema_version": "symmetry.admission.v1",`, "", 1)
	legacySchema := strings.Replace(valid, `"symmetry.admission.v1"`, `"legacy.v1"`, 1)
	unsupportedSchema := strings.Replace(valid, `"symmetry.admission.v1"`, `"symmetry.admission.v2"`, 1)
	invalidCurrent := strings.Replace(valid, `"limits":`, `"unexpected":true,"limits":`, 1)

	tests := []struct {
		name        string
		input       string
		wantPresent bool
		wantError   bool
	}{
		{name: "direct current", input: valid, wantPresent: true},
		{name: "direct current invalid envelope", input: invalidCurrent, wantPresent: true, wantError: true},
		{name: "nested current", input: `{"goal_admission":` + valid + `}`, wantPresent: true},
		{name: "unrelated outer schema with nested current", input: `{"schema_version":"legacy.v1","goal_admission":` + valid + `}`, wantPresent: true},
		{
			name:        "escaped current schema key",
			input:       strings.Replace(valid, `"schema_version":`, `"\u0073chema_version":`, 1),
			wantPresent: true,
		},
		{name: "direct unsupported version", input: unsupportedSchema, wantPresent: true, wantError: true},
		{name: "direct unsupported version without fields", input: `{"schema_version":"symmetry.admission.v2"}`, wantPresent: true, wantError: true},
		{name: "nested unsupported version", input: `{"goal_admission":{"schema_version":"symmetry.admission.v2"}}`, wantPresent: true, wantError: true},
		{
			name:        "escaped unsupported schema key",
			input:       `{"\u0073chema_version":"symmetry.admission.v2"}`,
			wantPresent: true,
			wantError:   true,
		},
		{name: "direct malformed numeric schema", input: `{"schema_version":2}`, wantPresent: true, wantError: true},
		{name: "direct malformed null schema", input: `{"schema_version":null}`, wantPresent: true, wantError: true},
		{name: "direct malformed empty schema", input: `{"schema_version":""}`, wantPresent: true, wantError: true},
		{name: "nested malformed schema", input: `{"goal_admission":{"schema_version":[]}}`, wantPresent: true, wantError: true},
		{name: "nested unrelated schema is explicit", input: `{"goal_admission":{"schema_version":"legacy.v1"}}`, wantPresent: true, wantError: true},
		{name: "direct missing schema admission shape", input: withoutSchema, wantPresent: true, wantError: true},
		{name: "nested missing schema admission shape", input: `{"goal_admission":` + withoutSchema + `}`, wantPresent: true, wantError: true},
		{name: "legacy purpose only", input: `{"purpose":"review"}`, wantPresent: false},
		{name: "legacy subject only", input: `{"subject":{}}`, wantPresent: false},
		{name: "legacy goal id only", input: `{"goal_id":"legacy"}`, wantPresent: false},
		{name: "legacy work item id only", input: `{"work_item_id":"legacy"}`, wantPresent: false},
		{name: "legacy model profile only", input: `{"model_profile":"review"}`, wantPresent: false},
		{name: "legacy overlapping admission fields", input: `{"goal_id":"legacy","work_item_id":"legacy","purpose":"review","subject":{}}`, wantPresent: false},
		{name: "direct unrelated schema", input: `{"schema_version":"legacy.v1","mode":"legacy"}`, wantPresent: false},
		{name: "escaped direct unrelated schema", input: `{"\u0073chema_version":"legacy.v1","mode":"legacy"}`, wantPresent: false},
		{name: "direct unrelated schema with ordinary input", input: `{"schema_version":"other.v7","value":1}`, wantPresent: false},
		{name: "direct unrelated schema with purpose", input: `{"schema_version":"other.v7","purpose":"review"}`, wantPresent: false},
		{name: "direct unrelated schema admission shape", input: legacySchema, wantPresent: true, wantError: true},
		{name: "schema-less nested legacy marker", input: `{"goal_admission":{"goal":"old marker"}}`, wantPresent: false},
		{name: "schema-less nested generic marker", input: `{"goal_admission":{"purpose":"review"}}`, wantPresent: false},
		{name: "null nested legacy marker", input: `{"goal_admission":null}`, wantPresent: false},
		{name: "string nested legacy marker", input: `{"goal_admission":"legacy"}`, wantPresent: false},
		{name: "number nested legacy marker", input: `{"goal_admission":7}`, wantPresent: false},
		{name: "boolean nested legacy marker", input: `{"goal_admission":true}`, wantPresent: false},
		{name: "empty array nested legacy marker", input: `{"goal_admission":[]}`, wantPresent: false},
		{name: "non-empty array nested legacy marker", input: `{"goal_admission":["legacy"]}`, wantPresent: false},
		{name: "legacy outer marker", input: `{"schema_version":"legacy.v1","goal_admission":null}`, wantPresent: false},
		{name: "legacy outer schema-less marker", input: `{"schema_version":"legacy.v1","goal_admission":{"goal":"old marker"}}`, wantPresent: false},
		{name: "distinctive outer field cannot hide behind null marker", input: `{"admission_id":"legacy","goal_admission":null}`, wantPresent: true, wantError: true},
		{name: "distinctive outer field cannot hide behind object marker", input: `{"admission_id":"legacy","goal_admission":{}}`, wantPresent: true, wantError: true},
		{name: "ordinary empty input", input: ``, wantPresent: false},
		{name: "ordinary whitespace input", input: " \t\r\n", wantPresent: false},
		{name: "ordinary null input", input: `null`, wantPresent: false},
		{name: "ordinary string input", input: `"legacy"`, wantPresent: false},
		{name: "ordinary number input", input: `7`, wantPresent: false},
		{name: "ordinary boolean input", input: `true`, wantPresent: false},
		{name: "ordinary array input", input: `[]`, wantPresent: false},
		{name: "unsupported outer marker", input: `{"schema_version":"symmetry.admission.v2","goal_admission":null}`, wantPresent: true, wantError: true},
		{name: "current outer marker", input: `{"schema_version":"symmetry.admission.v1","goal_admission":null}`, wantPresent: true, wantError: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			admission, present, err := ParseAdmissionInput(json.RawMessage(test.input))
			if present != test.wantPresent {
				t.Fatalf("present = %v, want %v; admission = %#v; error = %v", present, test.wantPresent, admission, err)
			}
			if (err != nil) != test.wantError {
				t.Fatalf("error = %v, wantError = %v", err, test.wantError)
			}
			if !test.wantError && test.wantPresent && admission.SchemaVersion != AdmissionSchemaVersion {
				t.Fatalf("admission schema_version = %q, want %q", admission.SchemaVersion, AdmissionSchemaVersion)
			}
		})
	}
}

func TestParseAdmissionInputRequiresSchemaForDistinctiveAdmissionFields(t *testing.T) {
	for _, test := range []struct {
		name  string
		field string
	}{
		{name: "admission id", field: `"admission_id":"legacy"`},
		{name: "context snapshot id", field: `"context_snapshot_id":"legacy"`},
		{name: "context hash", field: `"context_hash":"legacy"`},
		{name: "session mode", field: `"session_mode":"fresh"`},
		{name: "requested session id", field: `"requested_session_id":null`},
		{name: "handoff source run id", field: `"handoff_source_run_id":"legacy"`},
		{name: "limits", field: `"limits":{}`},
		{name: "validation task id", field: `"validation_of_task_id":null`},
		{name: "provider scope", field: `"provider_scope":null`},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, present, err := ParseAdmissionInput(json.RawMessage(`{` + test.field + `}`))
			if !present || err == nil {
				t.Fatalf("present = %v, error = %v, want present=true and missing-schema error", present, err)
			}
		})
	}

	for _, test := range []struct {
		name  string
		field string
	}{
		{name: "admission id", field: `"admission_id":"legacy"`},
		{name: "session mode", field: `"session_mode":"fresh"`},
		{name: "limits", field: `"limits":{}`},
	} {
		t.Run("unrelated schema/"+test.name, func(t *testing.T) {
			input := json.RawMessage(`{"schema_version":"other.v7",` + test.field + `}`)
			_, present, err := ParseAdmissionInput(input)
			if !present || err == nil {
				t.Fatalf("present = %v, error = %v, want present=true and admission-shape error", present, err)
			}
		})
	}
}

func TestParseAdmissionInputRejectsDuplicateAdmissionControlMembers(t *testing.T) {
	valid := string(readGoalSemanticFixture(t, "valid/admission.basic.json"))
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "duplicate nested marker with escaped key",
			input: `{"goal_admission":` + valid + `,"\u0067oal_admission":null}`,
		},
		{
			name: "duplicate direct schema with escaped key",
			input: strings.Replace(
				valid,
				`"schema_version": "symmetry.admission.v1"`,
				`"schema_version": "symmetry.admission.v1","\u0073chema_version":"legacy.v1"`,
				1,
			),
		},
		{
			name:  "duplicate nested schema with escaped key",
			input: `{"goal_admission":{"schema_version":"symmetry.admission.v1","\u0073chema_version":"symmetry.admission.v1"}}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, present, err := ParseAdmissionInput(json.RawMessage(test.input))
			if !present || err == nil {
				t.Fatalf("present = %v, error = %v, want present=true and duplicate-member error", present, err)
			}
			if !strings.Contains(err.Error(), "duplicate") {
				t.Fatalf("error = %v, want duplicate-member classification", err)
			}
		})
	}
}
