package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEvidenceBatchValidatesBatchIdentityAndItemSemantics(t *testing.T) {
	data := readEvidenceBatchFixture(t, "valid/evidence-batch.basic.json")

	batch, err := ParseEvidenceBatch(data)
	if err != nil {
		t.Fatalf("ParseEvidenceBatch() error = %v", err)
	}
	if batch.SchemaVersion != EvidenceBatchSchemaVersion {
		t.Fatalf("schema_version = %q, want %q", batch.SchemaVersion, EvidenceBatchSchemaVersion)
	}
	if len(batch.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(batch.Items))
	}
	if batch.Items[0].RunID != batch.RunID {
		t.Fatalf("item run_id = %q, want batch run_id %q", batch.Items[0].RunID, batch.RunID)
	}
}

func TestParseEvidenceBatchRejectsDuplicateKeysAndIDs(t *testing.T) {
	duplicateKey := readEvidenceBatchFixture(t, "invalid/evidence-batch.duplicate-key.json")
	if _, err := ParseEvidenceBatch(duplicateKey); err == nil || !strings.Contains(err.Error(), "duplicates evidence_key") {
		t.Fatalf("duplicate evidence key error = %v", err)
	}

	data := readEvidenceBatchFixture(t, "valid/evidence-batch.basic.json")
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	items, ok := document["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %#v, want one item", document["items"])
	}
	item, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("item = %#v, want object", items[0])
	}
	second := make(map[string]any, len(item))
	for key, value := range item {
		second[key] = value
	}
	second["evidence_key"] = "tests:secondary"
	second["observed_at"] = "2026-09-08T00:03:00Z"
	items = append(items, second)
	document["items"] = items
	mutated, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseEvidenceBatch(mutated); err == nil || !strings.Contains(err.Error(), "duplicates evidence_id") {
		t.Fatalf("duplicate evidence ID error = %v", err)
	}
}

func TestParseEvidenceBatchRejectsItemRunIDMismatch(t *testing.T) {
	data := readEvidenceBatchFixture(t, "valid/evidence-batch.basic.json")
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	items := document["items"].([]any)
	items[0].(map[string]any)["run_id"] = "88888888-8888-4888-8888-888888888888"
	mutated, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseEvidenceBatch(mutated); err == nil || !strings.Contains(err.Error(), "must match batch run_id") {
		t.Fatalf("run identity error = %v", err)
	}
}

func TestParseEvidenceBatchRejectsDuplicateJSONObjectMembers(t *testing.T) {
	for _, fixture := range []string{
		"evidence-batch.duplicate-outer.json",
		"evidence-batch.duplicate-item.json",
	} {
		t.Run(fixture, func(t *testing.T) {
			if _, err := ParseEvidenceBatch(readProtocolFixture(t, fixture)); err == nil || !strings.Contains(err.Error(), "duplicate JSON object member") {
				t.Fatalf("duplicate member error = %v", err)
			}
		})
	}
}

func readEvidenceBatchFixture(t *testing.T, relative string) []byte {
	t.Helper()
	root := contractRepositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "contracts", "fixtures", filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func readProtocolFixture(t *testing.T, name string) []byte {
	t.Helper()
	root := contractRepositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "daemon", "internal", "protocol", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}
