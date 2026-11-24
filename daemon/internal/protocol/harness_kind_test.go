package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// The contract schema owns the wire kind vocabulary; this binds the Go list to
// the enumerated Adapter.kind values instead of a second hand-copied set.
func TestHarnessKindVocabularyMatchesTheContractSchema(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	data, err := os.ReadFile(filepath.Join(root, "contracts", "v1", "common.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Definitions struct {
			Adapter struct {
				Properties struct {
					Kind struct {
						Enum []string `json:"enum"`
					} `json:"kind"`
				} `json:"properties"`
			} `json:"Adapter"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	enum := schema.Definitions.Adapter.Properties.Kind.Enum
	if len(enum) == 0 {
		t.Fatal("contracts/v1/common.schema.json Adapter.kind is not an enumerated kind vocabulary")
	}
	kinds := HarnessKinds()
	if len(kinds) != len(enum) {
		t.Fatalf("HarnessKinds() has %d kinds but the schema enum has %d: %v vs %v", len(kinds), len(enum), kinds, enum)
	}
	got := make([]string, 0, len(kinds))
	for _, kind := range kinds {
		got = append(got, string(kind))
	}
	want := slices.Clone(enum)
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("HarnessKinds() = %v, schema Adapter.kind enum = %v", got, want)
	}
}

func TestParseHarnessKindAcceptsExactlyTheWireVocabulary(t *testing.T) {
	for _, kind := range HarnessKinds() {
		parsed, ok := ParseHarnessKind(string(kind))
		if !ok || parsed != kind {
			t.Fatalf("ParseHarnessKind(%q) = (%q, %t), want (%q, true)", kind, parsed, ok, kind)
		}
	}
	for _, value := range []string{"", "claude", "Generic", "generic ", "future_harness", "opencode-v2"} {
		if parsed, ok := ParseHarnessKind(value); ok || parsed != "" {
			t.Fatalf("ParseHarnessKind(%q) = (%q, %t), want the zero value and false", value, parsed, ok)
		}
	}
}

func TestHarnessKindsReturnsAFreshCopy(t *testing.T) {
	kinds := HarnessKinds()
	if len(kinds) == 0 {
		t.Fatal("HarnessKinds() is empty")
	}
	kinds[0] = "mutated"
	if HarnessKinds()[0] == "mutated" {
		t.Fatal("HarnessKinds() returned shared mutable state")
	}
}
