package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"

	contractdto "github.com/wxxb789/symmetry/daemon/internal/contracts"
)

const (
	EvidenceBatchSchemaVersion = "symmetry.evidence_batch.v1"
	evidenceBatchMaxItems      = 256
)

// EvidenceBatch is the machine-only batch transport for normalized evidence.
// Each item remains a complete symmetry.evidence.v1 envelope; the batch adds
// only the shared run identity and bounded ordered delivery.
type EvidenceBatch struct {
	SchemaVersion string     `json:"schema_version"`
	RunID         string     `json:"run_id"`
	Items         []Evidence `json:"items"`
}

type evidenceBatchWire struct {
	SchemaVersion string            `json:"schema_version"`
	RunID         string            `json:"run_id"`
	Items         []json.RawMessage `json:"items"`
}

func (batch *EvidenceBatch) UnmarshalJSON(data []byte) error {
	var decoded evidenceBatchWire
	if err := decodeStrictObject(data, &decoded, "schema_version", "run_id", "items"); err != nil {
		return err
	}

	value := EvidenceBatch{
		SchemaVersion: decoded.SchemaVersion,
		RunID:         decoded.RunID,
		Items:         make([]Evidence, 0, len(decoded.Items)),
	}
	if value.SchemaVersion != EvidenceBatchSchemaVersion {
		return fmt.Errorf("schema_version must be %q", EvidenceBatchSchemaVersion)
	}
	if err := validateUUID(value.RunID, "run_id"); err != nil {
		return err
	}
	if len(decoded.Items) == 0 {
		return fmt.Errorf("items must contain at least one evidence item")
	}
	if len(decoded.Items) > evidenceBatchMaxItems {
		return fmt.Errorf("items must contain at most %d evidence items", evidenceBatchMaxItems)
	}

	seenIDs := make(map[string]struct{}, len(decoded.Items))
	seenKeys := make(map[string]struct{}, len(decoded.Items))
	for index, rawItem := range decoded.Items {
		evidence, err := ParseEvidence(rawItem)
		if err != nil {
			return fmt.Errorf("items[%d]: %w", index, err)
		}
		if evidence.RunID != value.RunID {
			return fmt.Errorf("items[%d].run_id must match batch run_id", index)
		}
		if _, exists := seenKeys[evidence.EvidenceKey]; exists {
			return fmt.Errorf("items[%d] duplicates evidence_key %q", index, evidence.EvidenceKey)
		}
		if _, exists := seenIDs[evidence.EvidenceID]; exists {
			return fmt.Errorf("items[%d] duplicates evidence_id %q", index, evidence.EvidenceID)
		}
		seenKeys[evidence.EvidenceKey] = struct{}{}
		seenIDs[evidence.EvidenceID] = struct{}{}
		value.Items = append(value.Items, evidence)
	}

	*batch = value
	return nil
}

// ParseEvidenceBatch validates the canonical batch schema and all item-level
// evidence semantics, then applies the batch-only identity invariants.
func ParseEvidenceBatch(data []byte) (EvidenceBatch, error) {
	if err := rejectDuplicateJSONMembers(data); err != nil {
		return EvidenceBatch{}, fmt.Errorf("decode evidence-batch JSON: %w", err)
	}
	var wire contractdto.SymmetryEvidenceBatchV1
	var batch EvidenceBatch
	if err := decodeGoalEnvelope(data, contractdto.EnvelopeEvidenceBatch, &wire, &batch); err != nil {
		return EvidenceBatch{}, err
	}
	return batch, nil
}

// rejectDuplicateJSONMembers rejects duplicate keys before any Go object
// decoder can apply its last-wins behavior. The scan is recursive so it also
// covers every object nested inside a batch item.
func rejectDuplicateJSONMembers(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanJSONValue(decoder, "$"); err != nil {
		return err
	}
	return ensureDecoderEOF(decoder)
}

func scanJSONValue(decoder *json.Decoder, path string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return nil
	}

	switch delim {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			member, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := member.(string)
			if !ok {
				return fmt.Errorf("JSON object member name at %s is not a string", path)
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON object member %q at %s", key, path)
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(decoder, path+"."+key); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim('}') {
			return fmt.Errorf("JSON object at %s is not terminated", path)
		}
	case '[':
		index := 0
		for decoder.More() {
			if err := scanJSONValue(decoder, fmt.Sprintf("%s[%d]", path, index)); err != nil {
				return err
			}
			index++
		}
		end, err := decoder.Token()
		if err != nil {
			return err
		}
		if end != json.Delim(']') {
			return fmt.Errorf("JSON array at %s is not terminated", path)
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q at %s", delim, path)
	}
	return nil
}
