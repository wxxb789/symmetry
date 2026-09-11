package protocol

import (
	"fmt"

	contractdto "github.com/wxxb789/symmetry/daemon/internal/contracts"
)

// EvidenceReceiptDisposition identifies how the control plane accepted one
// evidence item in a batch response.
type EvidenceReceiptDisposition string

const (
	EvidenceReceiptCreated  EvidenceReceiptDisposition = "created"
	EvidenceReceiptReplayed EvidenceReceiptDisposition = "replayed"
)

// EvidenceBatchResponse is the machine response returned after a fenced batch
// append. It is deliberately separate from Goal operator envelopes.
type EvidenceBatchResponse struct {
	EvidenceBatch EvidenceBatchReceiptSet `json:"evidence_batch"`
}

// EvidenceBatchReceiptSet groups ordered receipts under one durable run.
type EvidenceBatchReceiptSet struct {
	RunID    string                 `json:"run_id"`
	Receipts []EvidenceBatchReceipt `json:"receipts"`
}

// EvidenceBatchReceipt is the compact identity and outcome for one evidence
// item. It does not replace the submitted evidence payload.
type EvidenceBatchReceipt struct {
	ID          string                     `json:"id"`
	RunID       string                     `json:"run_id"`
	EvidenceKey string                     `json:"evidence_key"`
	Kind        EvidenceKind               `json:"kind"`
	SubjectHash string                     `json:"subject_hash"`
	Verdict     EvidenceVerdict            `json:"verdict"`
	ObservedAt  string                     `json:"observed_at"`
	Disposition EvidenceReceiptDisposition `json:"disposition"`
}

// EvidenceBatchConflictDetails describes item-level idempotency conflicts.
type EvidenceBatchConflictDetails struct {
	Items []EvidenceBatchConflictItem `json:"items"`
}

// EvidenceBatchConflictItem identifies one conflicting request item.
type EvidenceBatchConflictItem struct {
	Index       int64  `json:"index"`
	EvidenceKey string `json:"evidence_key"`
	Disposition string `json:"disposition"`
}

type evidenceBatchResponseWire struct {
	EvidenceBatch EvidenceBatchReceiptSet `json:"evidence_batch"`
}

type evidenceBatchReceiptSetWire struct {
	RunID    string                 `json:"run_id"`
	Receipts []EvidenceBatchReceipt `json:"receipts"`
}

type evidenceBatchReceiptWire EvidenceBatchReceipt

type evidenceBatchConflictDetailsWire struct {
	Items []EvidenceBatchConflictItem `json:"items"`
}

type evidenceBatchConflictItemWire EvidenceBatchConflictItem

func (response *EvidenceBatchResponse) UnmarshalJSON(data []byte) error {
	var decoded evidenceBatchResponseWire
	if err := decodeStrictObject(data, &decoded, "evidence_batch"); err != nil {
		return err
	}
	*response = EvidenceBatchResponse(decoded)
	return nil
}

func (batch *EvidenceBatchReceiptSet) UnmarshalJSON(data []byte) error {
	var decoded evidenceBatchReceiptSetWire
	if err := decodeStrictObject(data, &decoded, "run_id", "receipts"); err != nil {
		return err
	}
	if err := validateUUID(decoded.RunID, "evidence_batch.run_id"); err != nil {
		return err
	}
	if len(decoded.Receipts) == 0 {
		return fmt.Errorf("evidence_batch.receipts must contain at least one receipt")
	}
	if len(decoded.Receipts) > evidenceBatchMaxItems {
		return fmt.Errorf("evidence_batch.receipts must contain at most %d receipts", evidenceBatchMaxItems)
	}

	value := EvidenceBatchReceiptSet{
		RunID:    decoded.RunID,
		Receipts: make([]EvidenceBatchReceipt, 0, len(decoded.Receipts)),
	}
	seenIDs := make(map[string]struct{}, len(decoded.Receipts))
	seenKeys := make(map[string]struct{}, len(decoded.Receipts))
	for index, receipt := range decoded.Receipts {
		if err := receipt.Validate(); err != nil {
			return fmt.Errorf("evidence_batch.receipts[%d]: %w", index, err)
		}
		if receipt.RunID != value.RunID {
			return fmt.Errorf("evidence_batch.receipts[%d].run_id must match evidence_batch.run_id", index)
		}
		if _, duplicate := seenIDs[receipt.ID]; duplicate {
			return fmt.Errorf("evidence_batch.receipts[%d] duplicates receipt id %q", index, receipt.ID)
		}
		if _, duplicate := seenKeys[receipt.EvidenceKey]; duplicate {
			return fmt.Errorf("evidence_batch.receipts[%d] duplicates evidence_key %q", index, receipt.EvidenceKey)
		}
		seenIDs[receipt.ID] = struct{}{}
		seenKeys[receipt.EvidenceKey] = struct{}{}
		value.Receipts = append(value.Receipts, receipt)
	}

	*batch = value
	return nil
}

func (receipt *EvidenceBatchReceipt) UnmarshalJSON(data []byte) error {
	var decoded evidenceBatchReceiptWire
	if err := decodeStrictObject(data, &decoded,
		"id", "run_id", "evidence_key", "kind", "subject_hash", "verdict", "observed_at", "disposition"); err != nil {
		return err
	}
	value := EvidenceBatchReceipt(decoded)
	if err := value.Validate(); err != nil {
		return err
	}
	*receipt = value
	return nil
}

// Validate applies semantic checks that the response schema cannot express.
func (receipt EvidenceBatchReceipt) Validate() error {
	if err := validateUUID(receipt.ID, "receipt.id"); err != nil {
		return err
	}
	if err := validateUUID(receipt.RunID, "receipt.run_id"); err != nil {
		return err
	}
	if err := validateShortIdentifier(receipt.EvidenceKey, "receipt.evidence_key"); err != nil {
		return err
	}
	switch receipt.Kind {
	case EvidenceCheck, EvidenceArtifact, EvidenceReview, EvidenceObservation:
	default:
		return fmt.Errorf("receipt.kind %q is invalid", receipt.Kind)
	}
	if err := validateSHA256(receipt.SubjectHash, "receipt.subject_hash"); err != nil {
		return err
	}
	if err := validateEvidenceVerdict(receipt.Verdict, "receipt.verdict"); err != nil {
		return err
	}
	if err := validateUTCTimestamp(receipt.ObservedAt, "receipt.observed_at"); err != nil {
		return err
	}
	switch receipt.Disposition {
	case EvidenceReceiptCreated, EvidenceReceiptReplayed:
		return nil
	default:
		return fmt.Errorf("receipt.disposition %q is invalid", receipt.Disposition)
	}
}

func (details *EvidenceBatchConflictDetails) UnmarshalJSON(data []byte) error {
	var decoded evidenceBatchConflictDetailsWire
	if err := decodeStrictObject(data, &decoded, "items"); err != nil {
		return err
	}
	if len(decoded.Items) == 0 {
		return fmt.Errorf("items must contain at least one conflict item")
	}
	if len(decoded.Items) > evidenceBatchMaxItems {
		return fmt.Errorf("items must contain at most %d conflict items", evidenceBatchMaxItems)
	}

	value := EvidenceBatchConflictDetails{
		Items: make([]EvidenceBatchConflictItem, 0, len(decoded.Items)),
	}
	seenIndexes := make(map[int64]struct{}, len(decoded.Items))
	seenKeys := make(map[string]struct{}, len(decoded.Items))
	for index, item := range decoded.Items {
		if err := item.Validate(); err != nil {
			return fmt.Errorf("items[%d]: %w", index, err)
		}
		if _, duplicate := seenIndexes[item.Index]; duplicate {
			return fmt.Errorf("items[%d] duplicates index %d", index, item.Index)
		}
		if _, duplicate := seenKeys[item.EvidenceKey]; duplicate {
			return fmt.Errorf("items[%d] duplicates evidence_key %q", index, item.EvidenceKey)
		}
		seenIndexes[item.Index] = struct{}{}
		seenKeys[item.EvidenceKey] = struct{}{}
		value.Items = append(value.Items, item)
	}

	*details = value
	return nil
}

func (item *EvidenceBatchConflictItem) UnmarshalJSON(data []byte) error {
	var decoded evidenceBatchConflictItemWire
	if err := decodeStrictObject(data, &decoded, "index", "evidence_key", "disposition"); err != nil {
		return err
	}
	value := EvidenceBatchConflictItem(decoded)
	if err := value.Validate(); err != nil {
		return err
	}
	*item = value
	return nil
}

// Validate applies the bounded index, key and fixed-disposition contract.
func (item EvidenceBatchConflictItem) Validate() error {
	if err := validateSafeNonNegativeInt(item.Index, "conflict.index"); err != nil {
		return err
	}
	if item.Index > evidenceBatchMaxItems-1 {
		return fmt.Errorf("conflict.index must be between 0 and %d", evidenceBatchMaxItems-1)
	}
	if err := validateShortIdentifier(item.EvidenceKey, "conflict.evidence_key"); err != nil {
		return err
	}
	if item.Disposition != "conflict" {
		return fmt.Errorf("conflict.disposition must be %q", "conflict")
	}
	return nil
}

// ParseEvidenceBatchResponse validates the canonical response schema and its
// response-only run, identity and disposition semantics.
func ParseEvidenceBatchResponse(data []byte) (EvidenceBatchResponse, error) {
	if err := rejectDuplicateJSONMembers(data); err != nil {
		return EvidenceBatchResponse{}, fmt.Errorf("decode evidence-batch-response JSON: %w", err)
	}
	var wire contractdto.SymmetryEvidenceBatchResponseV1
	var response EvidenceBatchResponse
	if err := decodeGoalEnvelope(data, contractdto.EnvelopeEvidenceBatchResponse, &wire, &response); err != nil {
		return EvidenceBatchResponse{}, err
	}
	return response, nil
}

// ParseEvidenceBatchConflictDetails validates the canonical conflict-details
// schema and its bounded index, key and disposition semantics.
func ParseEvidenceBatchConflictDetails(data []byte) (EvidenceBatchConflictDetails, error) {
	if err := rejectDuplicateJSONMembers(data); err != nil {
		return EvidenceBatchConflictDetails{}, fmt.Errorf("decode evidence-batch-conflict-details JSON: %w", err)
	}
	var wire contractdto.SymmetryEvidenceBatchConflictDetailsV1
	var details EvidenceBatchConflictDetails
	if err := decodeGoalEnvelope(data, contractdto.EnvelopeEvidenceBatchConflictDetails, &wire, &details); err != nil {
		return EvidenceBatchConflictDetails{}, err
	}
	return details, nil
}
