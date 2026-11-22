package protocol

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	contractdto "github.com/wxxb789/symmetry/daemon/internal/contracts"
)

// decodeGoalEnvelope keeps generated DTOs as the sole wire shape. The target
// semantic type retains cross-field checks that JSON Schema intentionally does
// not express, such as canonical subject-hash equality. It consumes the
// original JSON so a oneOf branch can distinguish an omitted optional property
// from an explicitly supplied empty array; a generated Go struct cannot retain
// that presence information after marshal due to omitempty tags.
func decodeGoalEnvelope(data []byte, envelope contractdto.Envelope, wire, semantic any) error {
	if err := contractdto.DecodeSchema(envelope, data, wire); err != nil {
		return err
	}
	if err := json.Unmarshal(data, semantic); err != nil {
		return fmt.Errorf("convert %s generated DTO: %w", envelope, err)
	}
	return nil
}

// DecodeContextSnapshot validates and decodes the canonical context envelope.
// The machine-control client owns its semantic conversion because it owns the
// authenticated run fence and context receipt.
func DecodeContextSnapshot(data []byte) (contractdto.SymmetryContextSnapshotV1, error) {
	var snapshot contractdto.SymmetryContextSnapshotV1
	if err := contractdto.DecodeSchema(contractdto.EnvelopeContextSnapshot, data, &snapshot); err != nil {
		return contractdto.SymmetryContextSnapshotV1{}, err
	}
	if err := validateContextSnapshotContentHash(data, snapshot.ContentHash); err != nil {
		return contractdto.SymmetryContextSnapshotV1{}, err
	}
	value, err := decodeGoalContractObject(data)
	if err != nil {
		return contractdto.SymmetryContextSnapshotV1{}, err
	}
	if err := validateContextSnapshotPredicateIDs(value); err != nil {
		return contractdto.SymmetryContextSnapshotV1{}, err
	}
	return snapshot, nil
}

func validateContextSnapshotContentHash(data []byte, contentHash string) error {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("decode context snapshot for content_hash: %w", err)
	}
	if _, present := envelope["content_hash"]; !present {
		return fmt.Errorf("context snapshot content_hash is missing")
	}
	delete(envelope, "content_hash")
	withoutHash, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("encode context snapshot for content_hash: %w", err)
	}
	canonical, err := CanonicalizeJSON(withoutHash)
	if err != nil {
		return fmt.Errorf("canonicalize context snapshot content_hash: %w", err)
	}
	expected := fmt.Sprintf("sha256:%x", sha256.Sum256(canonical))
	if contentHash != expected {
		return fmt.Errorf("context snapshot content_hash does not match canonical snapshot")
	}
	return nil
}

type acceptanceContract struct {
	Predicates []struct {
		ID string `json:"id"`
	} `json:"predicates"`
}

func (contract acceptanceContract) validatePredicateIDs() error {
	seen := make(map[string]struct{}, len(contract.Predicates))
	for _, predicate := range contract.Predicates {
		if _, exists := seen[predicate.ID]; exists {
			return fmt.Errorf("acceptance contract contains duplicate predicate id %q", predicate.ID)
		}
		seen[predicate.ID] = struct{}{}
	}
	return nil
}

// ValidatePlanProposal validates the full PlanProposal envelope, including
// item acceptance contracts and provider change targets.
func ValidatePlanProposal(data []byte) error {
	var wire contractdto.SymmetryPlanProposalV1
	if err := contractdto.DecodeSchema(contractdto.EnvelopePlanProposal, data, &wire); err != nil {
		return err
	}
	value, err := decodeGoalContractObject(data)
	if err != nil {
		return err
	}
	if err := validatePlanProposalPredicateIDs(value); err != nil {
		return fmt.Errorf("validate plan-proposal contract: %w", err)
	}
	return nil
}
