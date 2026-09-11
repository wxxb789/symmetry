package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// decodeGoalContractObject preserves JSON number lexemes for proposal hashing.
// contracts.DecodeSchema has already enforced raw JSON and the portable schema.
func decodeGoalContractObject(data []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode Goal contract: %w", err)
	}
	if err := ensureDecoderEOF(decoder); err != nil {
		return nil, err
	}
	return value, nil
}

func validateGoalCreatePredicateIDs(value any) error {
	root, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("goal create must be an object")
	}
	revision, ok := root["initial_revision"].(map[string]any)
	if !ok {
		return fmt.Errorf("initial_revision must be an object")
	}
	return validateGoalRevisionContractSemantics(revision)
}

func validateContextSnapshotPredicateIDs(value any) error {
	root, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("context snapshot must be an object")
	}
	workContract, ok := root["work_contract"].(map[string]any)
	if !ok {
		return fmt.Errorf("work_contract must be an object")
	}
	if err := validateAcceptancePredicateIDs(workContract["acceptance"]); err != nil {
		return err
	}
	return validateProviderChangeTarget(workContract["change_target"])
}

func validateGoalCommandPredicateIDs(value any) error {
	root, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("goal command must be an object")
	}
	payload, ok := root["payload"].(map[string]any)
	if !ok {
		return fmt.Errorf("payload must be an object")
	}
	switch root["kind"] {
	case "amend":
		revision, ok := payload["revision_contract"].(map[string]any)
		if !ok {
			return fmt.Errorf("revision_contract must be an object")
		}
		return validateGoalRevisionContractSemantics(revision)
	case "accept_plan":
		if err := validatePlanProposalPredicateIDs(payload["proposal"]); err != nil {
			return err
		}
		proposalHash, ok := payload["proposal_hash"].(string)
		if !ok {
			return fmt.Errorf("proposal_hash must be a string")
		}
		expected, err := canonicalPlanProposalSHA256(payload["proposal"])
		if err != nil {
			return fmt.Errorf("canonicalize proposal: %w", err)
		}
		if proposalHash != expected {
			return fmt.Errorf("proposal_hash does not match canonical proposal")
		}
		return nil
	case "request_decision":
		if payload["kind"] == "plan" {
			return validatePlanProposalPredicateIDs(payload["proposal"])
		}
		return nil
	default:
		return nil
	}
}

func validatePlanProposalPredicateIDs(value any) error {
	proposal, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("plan proposal must be an object")
	}
	items, ok := proposal["items"].([]any)
	if !ok {
		return fmt.Errorf("plan items must be an array")
	}
	for index, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return fmt.Errorf("plan item %d must be an object", index)
		}
		if err := validateAcceptancePredicateIDs(item["acceptance"]); err != nil {
			return fmt.Errorf("plan item %d: %w", index, err)
		}
		if err := validateProviderChangeTarget(item["change_target"]); err != nil {
			return fmt.Errorf("plan item %d: %w", index, err)
		}
	}
	return nil
}

func validateProviderChangeTarget(value any) error {
	if value == nil {
		return nil
	}
	target, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("provider change target must be an object")
	}
	if target["kind"] == "branches" && target["source_branch"] == target["target_branch"] {
		return fmt.Errorf("provider change target source_branch and target_branch must differ")
	}
	return nil
}

func validateAcceptancePredicateIDs(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode acceptance contract: %w", err)
	}
	var acceptance acceptanceContract
	if err := json.Unmarshal(encoded, &acceptance); err != nil {
		return fmt.Errorf("decode acceptance contract: %w", err)
	}
	return acceptance.validatePredicateIDs()
}

func validateGoalRevisionContractSemantics(value any) error {
	revision, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("goal revision contract must be an object")
	}
	acceptance, ok := revision["acceptance_contract"].(map[string]any)
	if !ok {
		return fmt.Errorf("acceptance_contract must be an object")
	}
	if err := validateAcceptancePredicateIDs(acceptance); err != nil {
		return err
	}
	executionPolicy, ok := revision["execution_policy"].(map[string]any)
	if !ok {
		return fmt.Errorf("execution_policy must be an object")
	}
	authorityPolicy, ok := revision["authority_policy"].(map[string]any)
	if !ok {
		return fmt.Errorf("authority_policy must be an object")
	}
	if executionPolicy["final_acceptance"] != "deterministic" || authorityPolicy["operator_required_for_completion"] == true {
		return nil
	}
	predicates, ok := acceptance["predicates"].([]any)
	if !ok {
		return fmt.Errorf("acceptance predicates must be an array")
	}
	for _, rawPredicate := range predicates {
		predicate, ok := rawPredicate.(map[string]any)
		if !ok {
			return fmt.Errorf("acceptance predicate must be an object")
		}
		kind, ok := predicate["kind"].(string)
		if !ok || (kind != "check" && kind != "artifact") {
			return fmt.Errorf("deterministic acceptance includes a non-machine predicate")
		}
	}
	return nil
}

func canonicalSHA256(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	canonical, err := CanonicalizeJSON(encoded)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return fmt.Sprintf("sha256:%x", digest), nil
}

func canonicalPlanProposalSHA256(value any) (string, error) {
	normalized, err := normalizePlanProposal(value)
	if err != nil {
		return "", err
	}
	return canonicalSHA256(normalized)
}

func normalizePlanProposal(value any) (map[string]any, error) {
	proposal, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("plan proposal must be an object")
	}
	normalized := make(map[string]any, len(proposal))
	for key, item := range proposal {
		normalized[key] = item
	}
	rawItems, ok := proposal["items"].([]any)
	if !ok {
		return nil, fmt.Errorf("plan items must be an array")
	}
	items := make([]any, len(rawItems))
	for index, rawItem := range rawItems {
		item, ok := rawItem.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("plan item %d must be an object", index)
		}
		normalizedItem := make(map[string]any, len(item)+1)
		for key, child := range item {
			normalizedItem[key] = child
		}
		if _, present := normalizedItem["integration"]; !present {
			normalizedItem["integration"] = false
		}
		items[index] = normalizedItem
	}
	normalized["items"] = items
	return normalized, nil
}
