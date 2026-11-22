package protocol

import (
	"bytes"
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
