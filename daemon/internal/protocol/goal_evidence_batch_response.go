package protocol

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
