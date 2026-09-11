package protocol

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEvidenceBatchResponseValidatesReceipts(t *testing.T) {
	response, err := ParseEvidenceBatchResponse(readResponseContractFixture(t, "valid/evidence-batch-response.basic.json"))
	if err != nil {
		t.Fatalf("ParseEvidenceBatchResponse() error = %v", err)
	}
	if len(response.EvidenceBatch.Receipts) != 2 {
		t.Fatalf("receipts = %d, want 2", len(response.EvidenceBatch.Receipts))
	}
	if response.EvidenceBatch.Receipts[0].Disposition != EvidenceReceiptCreated ||
		response.EvidenceBatch.Receipts[1].Disposition != EvidenceReceiptReplayed {
		t.Fatalf("dispositions = %#v, want created then replayed", response.EvidenceBatch.Receipts)
	}
}

func TestParseEvidenceBatchResponseRejectsMismatchedRunAndDuplicateIdentities(t *testing.T) {
	base := readResponseContractFixture(t, "valid/evidence-batch-response.basic.json")

	t.Run("run_id mismatch", func(t *testing.T) {
		document := decodeResponseDocument(t, base)
		receipts := responseReceipts(t, document)
		receipts[1].(map[string]any)["run_id"] = "88888888-8888-4888-8888-888888888888"
		if _, err := ParseEvidenceBatchResponse(encodeResponseDocument(t, document)); err == nil || !strings.Contains(err.Error(), "must match evidence_batch.run_id") {
			t.Fatalf("run identity error = %v", err)
		}
	})

	t.Run("duplicate receipt id", func(t *testing.T) {
		document := decodeResponseDocument(t, base)
		receipts := responseReceipts(t, document)
		first := receipts[0].(map[string]any)
		second := cloneJSONObject(t, first)
		second["evidence_key"] = "tests:tertiary"
		second["observed_at"] = "2026-09-11T12:00:02Z"
		receipts = append(receipts, second)
		document["evidence_batch"].(map[string]any)["receipts"] = receipts
		if _, err := ParseEvidenceBatchResponse(encodeResponseDocument(t, document)); err == nil || !strings.Contains(err.Error(), "duplicates receipt id") {
			t.Fatalf("duplicate receipt ID error = %v", err)
		}
	})

	t.Run("duplicate evidence key", func(t *testing.T) {
		document := decodeResponseDocument(t, base)
		receipts := responseReceipts(t, document)
		first := receipts[0].(map[string]any)
		second := receipts[1].(map[string]any)
		second["id"] = "88888888-8888-4888-8888-888888888888"
		second["evidence_key"] = first["evidence_key"]
		if _, err := ParseEvidenceBatchResponse(encodeResponseDocument(t, document)); err == nil || !strings.Contains(err.Error(), "duplicates evidence_key") {
			t.Fatalf("duplicate evidence key error = %v", err)
		}
	})
}

func TestParseEvidenceBatchConflictDetailsValidatesItems(t *testing.T) {
	details, err := ParseEvidenceBatchConflictDetails(readResponseContractFixture(t, "valid/evidence-batch-conflict-details.basic.json"))
	if err != nil {
		t.Fatalf("ParseEvidenceBatchConflictDetails() error = %v", err)
	}
	if len(details.Items) != 2 || details.Items[0].Index != 0 || details.Items[1].Index != 255 {
		t.Fatalf("conflict details = %#v, want indexes 0 and 255", details.Items)
	}
	for index, item := range details.Items {
		if item.Disposition != "conflict" {
			t.Fatalf("item %d disposition = %q, want conflict", index, item.Disposition)
		}
	}
}

func TestParseEvidenceBatchConflictDetailsRejectsDuplicateIndexesAndKeys(t *testing.T) {
	base := readResponseContractFixture(t, "valid/evidence-batch-conflict-details.basic.json")

	t.Run("duplicate index", func(t *testing.T) {
		document := decodeConflictDocument(t, base)
		items := conflictItems(t, document)
		items[1].(map[string]any)["index"] = 0
		items[1].(map[string]any)["evidence_key"] = "tests:tertiary"
		if _, err := ParseEvidenceBatchConflictDetails(encodeResponseDocument(t, document)); err == nil || !strings.Contains(err.Error(), "duplicates index") {
			t.Fatalf("duplicate index error = %v", err)
		}
	})

	t.Run("duplicate evidence key", func(t *testing.T) {
		document := decodeConflictDocument(t, base)
		items := conflictItems(t, document)
		items[1].(map[string]any)["index"] = 1
		items[1].(map[string]any)["evidence_key"] = items[0].(map[string]any)["evidence_key"]
		if _, err := ParseEvidenceBatchConflictDetails(encodeResponseDocument(t, document)); err == nil || !strings.Contains(err.Error(), "duplicates evidence_key") {
			t.Fatalf("duplicate evidence key error = %v", err)
		}
	})
}

func readResponseContractFixture(t *testing.T, relative string) []byte {
	t.Helper()
	root := contractRepositoryRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "contracts", "fixtures", filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func decodeResponseDocument(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func encodeResponseDocument(t *testing.T, document map[string]any) []byte {
	t.Helper()
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func responseReceipts(t *testing.T, document map[string]any) []any {
	t.Helper()
	evidenceBatch, ok := document["evidence_batch"].(map[string]any)
	if !ok {
		t.Fatalf("evidence_batch = %#v, want object", document["evidence_batch"])
	}
	receipts, ok := evidenceBatch["receipts"].([]any)
	if !ok || len(receipts) < 2 {
		t.Fatalf("receipts = %#v, want at least two items", evidenceBatch["receipts"])
	}
	return receipts
}

func decodeConflictDocument(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func conflictItems(t *testing.T, document map[string]any) []any {
	t.Helper()
	items, ok := document["items"].([]any)
	if !ok || len(items) < 2 {
		t.Fatalf("items = %#v, want at least two items", document["items"])
	}
	return items
}

func cloneJSONObject(t *testing.T, source map[string]any) map[string]any {
	t.Helper()
	clone := make(map[string]any, len(source))
	for key, value := range source {
		clone[key] = value
	}
	return clone
}
