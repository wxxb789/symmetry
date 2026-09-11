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

// DecodeGoalRevision validates and decodes the canonical goal revision envelope.
func DecodeGoalRevision(data []byte) (contractdto.SymmetryGoalRevisionV1, error) {
	var revision contractdto.SymmetryGoalRevisionV1
	if err := contractdto.DecodeSchema(contractdto.EnvelopeGoalRevision, data, &revision); err != nil {
		return contractdto.SymmetryGoalRevisionV1{}, err
	}
	value, err := decodeGoalContractObject(data)
	if err != nil {
		return contractdto.SymmetryGoalRevisionV1{}, err
	}
	if err := validateGoalRevisionContractSemantics(value); err != nil {
		return contractdto.SymmetryGoalRevisionV1{}, err
	}
	return revision, nil
}

// ValidateGoalCommand applies the portable GoalCommand schema and the
// daemon-side identity invariant that the request_plan repository resource and
// complete Subject must name the same resource. The schema deliberately keeps
// those fields independently typed, so this binding belongs at the semantic
// boundary rather than in JSON Schema alone.
func ValidateGoalCommand(data []byte) error {
	var wire contractdto.SymmetryGoalCommandV1
	if err := contractdto.DecodeSchema(contractdto.EnvelopeGoalCommand, data, &wire); err != nil {
		return err
	}
	value, err := decodeGoalContractObject(data)
	if err != nil {
		return err
	}
	if err := validateGoalCommandPredicateIDs(value); err != nil {
		return fmt.Errorf("validate goal-command contract: %w", err)
	}
	var command struct {
		Kind    string          `json:"kind"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(data, &command); err != nil {
		return fmt.Errorf("decode goal command: %w", err)
	}
	if command.Kind != "request_plan" {
		return nil
	}
	var requestPlan struct {
		RepositoryResourceID string  `json:"repository_resource_id"`
		Subject              Subject `json:"subject"`
	}
	if err := json.Unmarshal(command.Payload, &requestPlan); err != nil {
		return fmt.Errorf("decode request_plan payload: %w", err)
	}
	if requestPlan.RepositoryResourceID != requestPlan.Subject.ResourceID {
		return fmt.Errorf("request_plan repository_resource_id %q does not match subject.resource_id %q", requestPlan.RepositoryResourceID, requestPlan.Subject.ResourceID)
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

// DecodeDecision validates and decodes the canonical decision envelope.
func DecodeDecision(data []byte) (contractdto.SymmetryDecisionV1, error) {
	var decision contractdto.SymmetryDecisionV1
	if err := contractdto.DecodeSchema(contractdto.EnvelopeDecision, data, &decision); err != nil {
		return contractdto.SymmetryDecisionV1{}, err
	}
	options := make(map[string]struct{}, len(decision.Options))
	for _, option := range decision.Options {
		if _, duplicate := options[option.ID]; duplicate {
			return contractdto.SymmetryDecisionV1{}, fmt.Errorf("decision contains duplicate option id %q", option.ID)
		}
		options[option.ID] = struct{}{}
	}
	if decision.State == contractdto.Resolved {
		if decision.Resolution == nil {
			return contractdto.SymmetryDecisionV1{}, fmt.Errorf("resolved decision requires a resolution")
		}
		if _, exists := options[decision.Resolution.OptionID]; !exists {
			return contractdto.SymmetryDecisionV1{}, fmt.Errorf("decision resolution names an unknown option")
		}
	} else if decision.Resolution != nil {
		return contractdto.SymmetryDecisionV1{}, fmt.Errorf("unresolved decision must not contain a resolution")
	}
	return decision, nil
}

// ValidateGoalCreate validates the full GoalCreate envelope, including the
// initial revision's cross-field acceptance contract.
func ValidateGoalCreate(data []byte) error {
	var wire contractdto.SymmetryGoalCreateV1
	if err := contractdto.DecodeSchema(contractdto.EnvelopeGoalCreate, data, &wire); err != nil {
		return err
	}
	value, err := decodeGoalContractObject(data)
	if err != nil {
		return err
	}
	if err := validateGoalCreatePredicateIDs(value); err != nil {
		return fmt.Errorf("validate goal-create contract: %w", err)
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

// ValidateGoalEnvelope is the full semantic validation boundary for every
// Goal 0006 wire envelope. Raw schema-only callers must use
// contracts.ValidateSchema explicitly.
func ValidateGoalEnvelope(envelope contractdto.Envelope, data []byte) error {
	switch envelope {
	case contractdto.EnvelopeAdapterCapabilities:
		_, err := ParseAdapterCapabilities(data)
		return err
	case contractdto.EnvelopeAdmission:
		_, err := ParseAdmission(data)
		return err
	case contractdto.EnvelopeContextSnapshot:
		_, err := DecodeContextSnapshot(data)
		return err
	case contractdto.EnvelopeDecision:
		_, err := DecodeDecision(data)
		return err
	case contractdto.EnvelopeEvidence:
		_, err := ParseEvidence(data)
		return err
	case contractdto.EnvelopeGoalCommand:
		return ValidateGoalCommand(data)
	case contractdto.EnvelopeGoalCreate:
		return ValidateGoalCreate(data)
	case contractdto.EnvelopeGoalRevision:
		_, err := DecodeGoalRevision(data)
		return err
	case contractdto.EnvelopePlanProposal:
		return ValidatePlanProposal(data)
	case contractdto.EnvelopeTaskResult:
		_, err := ParseTaskResult(data)
		return err
	case contractdto.EnvelopeUsage:
		_, err := ParseUsage(data)
		return err
	default:
		return fmt.Errorf("unsupported Goal envelope schema %q", envelope)
	}
}
