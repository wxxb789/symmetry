package contracts

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAllGoalSchemasCompileAndGeneratedTimestampsStayStrings(t *testing.T) {
	if err := ensureSchemas(); err != nil {
		t.Fatalf("schemas did not compile: %v", err)
	}
	if len(schemas) != 11 {
		t.Fatalf("compiled schemas = %d, want 11", len(schemas))
	}
	if commitPath == nil || commitPath.MatchTimeout != 10*time.Millisecond {
		t.Fatalf("CommitPath regex timeout = %v, want %v", commitPath.MatchTimeout, 10*time.Millisecond)
	}

	var admission SymmetryAdmissionV1
	if err := DecodeSchema(EnvelopeAdmission, readContractFixture(t, "valid/admission.basic.json"), &admission); err != nil {
		t.Fatal(err)
	}
	if admission.Limits.DeadlineAt != "2026-09-09T00:00:00Z" {
		t.Fatalf("deadline_at = %q", admission.Limits.DeadlineAt)
	}
}

func TestDecodePreservesNullableMicrousdFields(t *testing.T) {
	var revision SymmetryGoalRevisionV1
	if err := DecodeSchema(
		EnvelopeGoalRevision,
		readContractFixture(t, "valid/goal-revision.automatic-soft-budget.json"),
		&revision,
	); err != nil {
		t.Fatal(err)
	}

	policy := revision.ExecutionPolicy
	if policy.BudgetLimitMicrousd == nil || *policy.BudgetLimitMicrousd != "1000000" {
		t.Fatalf("budget_limit_microusd = %#v, want pointer to 1000000", policy.BudgetLimitMicrousd)
	}
	if policy.PerRunCostLimitMicrousd != nil {
		t.Fatalf("per_run_cost_limit_microusd = %q, want nil", *policy.PerRunCostLimitMicrousd)
	}
}

func TestValidateRejectsTrailingJSON(t *testing.T) {
	data := append(readContractFixture(t, "valid/admission.basic.json"), []byte(" null")...)
	if err := ValidateSchema(EnvelopeAdmission, data); err == nil {
		t.Fatal("multiple JSON values were accepted")
	}
}

func TestValidateRejectsExponentNumberBeforeSchemaValidation(t *testing.T) {
	data := strings.Replace(
		string(readContractFixture(t, "valid/usage.safe-integer-limit.json")),
		`"input_tokens": 9007199254740991`,
		`"input_tokens": 1e0`,
		1,
	)
	err := ValidateSchema(EnvelopeUsage, []byte(data))
	if err == nil || !strings.Contains(err.Error(), "fractional or exponent number is not canonical: 1e0") {
		t.Fatalf("Validate accepted exponent-form safe integer: %v", err)
	}
}

func TestDecodeJSONRejectsNonCanonicalRawNumbers(t *testing.T) {
	for name, test := range map[string]struct {
		data string
		want string
	}{
		"fraction": {
			data: `{"value":1.0}`,
			want: "fractional or exponent number is not canonical: 1.0",
		},
		"exponent": {
			data: `{"value":1e0}`,
			want: "fractional or exponent number is not canonical: 1e0",
		},
		"positive unsafe integer": {
			data: `{"value":9007199254740992}`,
			want: "integer exceeds the JSON safe-integer range: 9007199254740992",
		},
		"negative unsafe integer": {
			data: `{"value":-9007199254740992}`,
			want: "integer exceeds the JSON safe-integer range: -9007199254740992",
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := decodeJSON([]byte(test.data))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("decodeJSON(%s) error = %v, want %q", test.data, err, test.want)
			}
		})
	}
}

func TestDecodeJSONTreatsNumberLookingStringsAndNegativeZeroLikeNodeChecker(t *testing.T) {
	data := []byte(`{"text":"numeric lexemes: \"1e0\", 9007199254740992","nested":["1.5","-2e3"],"value":-0}`)
	value, err := decodeJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	decoded := value.(map[string]any)
	if decoded["value"] != json.Number("-0") {
		t.Fatalf("numeric -0 = %#v, want json.Number(-0)", decoded["value"])
	}
}

func TestDecodeNormalizesNumericNegativeZeroInGeneratedIntegerDTO(t *testing.T) {
	data := strings.Replace(
		string(readContractFixture(t, "valid/usage.safe-integer-limit.json")),
		`"input_tokens": 9007199254740991`,
		`"input_tokens": -0`,
		1,
	)
	var usage SymmetryUsageV1
	if err := DecodeSchema(EnvelopeUsage, []byte(data), &usage); err != nil {
		t.Fatalf("DecodeSchema rejected numeric -0: %v", err)
	}
	if usage.InputTokens == nil || *usage.InputTokens != 0 {
		t.Fatalf("input_tokens = %#v, want 0", usage.InputTokens)
	}
}

func TestCommitPathUsesBoundedECMAScriptValidation(t *testing.T) {
	base := readContractFixture(t, "valid/evidence.artifact.json")
	for _, path := range []string{"/absolute", "dir/../escape", "dir\\windows", "dir//empty"} {
		t.Run(path, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(base, &value); err != nil {
				t.Fatal(err)
			}
			value["source_ref"].(map[string]any)["path"] = path
			value["payload"].(map[string]any)["path"] = path
			data, err := json.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			if err := ValidateSchema(EnvelopeEvidence, data); err == nil {
				t.Fatal("invalid CommitPath was accepted")
			}
		})
	}
}

func TestDecodeJSONRejectsInvalidUTF8AndUnpairedSurrogates(t *testing.T) {
	for name, data := range map[string][]byte{
		"invalid UTF-8":       {'{', '"', 'v', '"', ':', '"', 0xff, '"', '}'},
		"high surrogate":      []byte(`{"value":"\uD800"}`),
		"low surrogate":       []byte(`{"value":"\uDC00"}`),
		"unmatched high pair": []byte(`{"value":"\uD800\u0041"}`),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeJSON(data); err == nil {
				t.Fatalf("decodeJSON accepted %q", data)
			}
		})
	}
}

func TestDecodeSchemaRejectsUnpairedSurrogatesAfterSchemaValidation(t *testing.T) {
	data := readContractFixture(t, "invalid/goal-command.unpaired-surrogate.json")
	if err := ValidateSchema(EnvelopeGoalCommand, data); err != nil {
		t.Fatalf("schema validation rejected escaped surrogate before semantic validation: %v", err)
	}
	var command SymmetryGoalCommandV1
	if err := DecodeSchema(EnvelopeGoalCommand, data, &command); err == nil || !strings.Contains(err.Error(), "unpaired high surrogate") {
		t.Fatalf("DecodeSchema accepted an unpaired surrogate: %v", err)
	}
}

func TestTaskResultSchemaReturnsAnIndependentCanonicalDocument(t *testing.T) {
	schema, err := TaskResultSchema()
	if err != nil {
		t.Fatal(err)
	}
	var bundle map[string]json.RawMessage
	if err := json.Unmarshal(schemaBundle, &bundle); err != nil {
		t.Fatal(err)
	}
	var expected map[string]any
	if err := json.Unmarshal(bundle["task-result.schema.json"], &expected); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(schema, expected) {
		t.Fatalf("TaskResultSchema() = %#v, want canonical task-result schema", schema)
	}
	schema["title"] = "mutated"
	again, err := TaskResultSchema()
	if err != nil {
		t.Fatal(err)
	}
	if again["title"] != expected["title"] {
		t.Fatalf("TaskResultSchema() shared mutable state: %#v", again)
	}
}

func readContractFixture(t *testing.T, relative string) []byte {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "contracts", "fixtures", filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
