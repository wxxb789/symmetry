package codex

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestNativeTaskResultSchemaIsStrictStructuredOutputCompatible(t *testing.T) {
	schema, err := nativeTaskResultSchema()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	if _, present := schema["oneOf"]; present {
		t.Fatal("native schema retained top-level oneOf")
	}
	if _, present := schema["anyOf"]; present {
		t.Fatal("native schema retained top-level anyOf")
	}
	if _, present := schema["definitions"]; present {
		t.Fatal("native schema retained draft-07 definitions")
	}
	definitions, _ := schema["$defs"].(map[string]any)
	if len(definitions) == 0 {
		t.Fatal("native schema has no reachable definitions")
	}
	for _, keyword := range []string{"uniqueItems", "minItems", "maxItems", "pattern", "format"} {
		if strings.Contains(string(encoded), `"`+keyword+`"`) {
			t.Fatalf("native schema retained unsupported keyword %q", keyword)
		}
	}

	var visit func(any, string)
	visit = func(node any, path string) {
		object, ok := node.(map[string]any)
		if !ok {
			return
		}
		if properties, ok := object["properties"].(map[string]any); ok {
			if object["type"] != "object" {
				t.Fatalf("%s: object with properties must declare type object", path)
			}
			if object["additionalProperties"] != false {
				t.Fatalf("%s: object must declare additionalProperties false", path)
			}
			required, _ := object["required"].([]string)
			if len(required) != len(properties) {
				entries, _ := object["required"].([]any)
				required = make([]string, 0, len(entries))
				for _, entry := range entries {
					name, _ := entry.(string)
					required = append(required, name)
				}
			}
			if len(required) != len(properties) {
				t.Fatalf("%s: every property must be required in strict output", path)
			}
			for _, name := range required {
				if _, exists := properties[name]; !exists {
					t.Fatalf("%s: required property %q is not declared", path, name)
				}
			}
		}
		if ref, ok := object["$ref"].(string); ok {
			name := strings.TrimPrefix(ref, "#/$defs/")
			if !strings.HasPrefix(ref, "#/$defs/") || definitions[name] == nil {
				t.Fatalf("%s: unresolved ref %q", path, name)
			}
		}
		for keyword, child := range object {
			if keyword == "properties" || keyword == "$defs" {
				continue
			}
			switch typed := child.(type) {
			case map[string]any:
				visit(typed, path+"."+keyword)
			case []any:
				for index, entry := range typed {
					visit(entry, path+"."+keyword+"[]"+string(rune('0'+index)))
				}
			}
		}
		if child, ok := object["$defs"].(map[string]any); ok {
			for name, definition := range child {
				visit(definition, path+".$defs."+name)
			}
		}
	}
	visit(schema, "$")
}

func TestProjectNativeSchemaRejectsReachableMap(t *testing.T) {
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"entries": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}}},
		"required":   []any{"entries"},
	}
	if err := projectNativeSchema(schema); err == nil {
		t.Fatal("projection accepted a schema-valued additionalProperties map")
	}
}

func TestNormalizeNativeTaskResultTextPrunesOptionalNulls(t *testing.T) {
	subject := protocol.Subject{
		ResourceID: "11111111-1111-4111-8111-111111111111",
		Commit:     strings.Repeat("a", 40),
		TreeDigest: "sha256:" + strings.Repeat("b", 64),
	}
	subjectHash, err := subject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	strictOutput := `{
		"schema_version": "symmetry.task_result.v1",
		"result_id": "22222222-2222-4222-8222-222222222222",
		"kind": "candidate_completion",
		"summary": "created hello.txt",
		"subject": {"resource_id": "` + subject.ResourceID + `", "commit": "` + subject.Commit + `", "tree_digest": "` + subject.TreeDigest + `"},
		"subject_hash": "` + subjectHash + `",
		"evidence_refs": [],
		"blocker": null,
		"proposed_next_action": null,
		"proposal": null,
		"reason": null,
		"diagnostics": [{"code": "smoke", "message": "ok", "severity": null}]
	}`
	normalized, err := normalizeNativeTaskResultText(strictOutput)
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(normalized, &value); err != nil {
		t.Fatal(err)
	}
	diagnostics, _ := value["diagnostics"].([]any)
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostics = %v", diagnostics)
	}
	diagnostic, _ := diagnostics[0].(map[string]any)
	if _, present := diagnostic["severity"]; present {
		t.Fatal("optional severity null was not pruned")
	}
	for _, key := range []string{"blocker", "proposed_next_action", "proposal", "reason"} {
		if _, present := value[key]; !present {
			t.Fatalf("required nullable field %q was pruned", key)
		}
	}
	if _, err := protocol.ParseTaskResult(normalized); err != nil {
		t.Fatalf("normalized strict output is not a valid TaskResult: %v", err)
	}
}

func TestNormalizeNativeTaskResultTextRejectsNonCanonicalRawJSON(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "duplicate member",
			input: `{"kind":"candidate_completion","kind":"failed","diagnostics":[{"severity":null}]}`,
			want:  "duplicate",
		},
		{
			name:  "trailing value",
			input: `{"diagnostics":[{"severity":null}]} {}`,
			want:  "more than one value",
		},
		{
			name:  "invalid UTF-8",
			input: "{\"summary\":\"\xff\",\"diagnostics\":[{\"severity\":null}]}",
			want:  "UTF-8",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeNativeTaskResultText(test.input)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("normalize error = %v, want containing %q", err, test.want)
			}
		})
	}
}
