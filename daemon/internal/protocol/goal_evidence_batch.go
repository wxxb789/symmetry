package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
)

const EvidenceBatchSchemaVersion = "symmetry.evidence_batch.v1"

// EvidenceBatch is the machine-only batch transport for normalized evidence.
// Each item remains a complete symmetry.evidence.v1 envelope; the batch adds
// only the shared run identity and bounded ordered delivery.
type EvidenceBatch struct {
	SchemaVersion string     `json:"schema_version"`
	RunID         string     `json:"run_id"`
	Items         []Evidence `json:"items"`
}

// RejectDuplicateJSONMembers rejects duplicate keys at any depth before any Go
// object decoder can apply its last-wins behavior.
func RejectDuplicateJSONMembers(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := scanJSONValue(decoder, "$"); err != nil {
		return err
	}
	return ensureDecoderEOF(decoder)
}

// RejectDuplicateTopLevelJSONMembers rejects duplicate control fields at an
// otherwise extensible object boundary without imposing strictness on nested
// legacy payloads.
func RejectDuplicateTopLevelJSONMembers(data []byte, members ...string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		return fmt.Errorf("JSON value must be an object")
	}
	watched := make(map[string]struct{}, len(members))
	for _, member := range members {
		watched[member] = struct{}{}
	}
	seen := make(map[string]struct{}, len(members))
	for decoder.More() {
		member, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := member.(string)
		if !ok {
			return fmt.Errorf("JSON object member name at $ is not a string")
		}
		if _, watch := watched[name]; watch {
			if _, duplicate := seen[name]; duplicate {
				return fmt.Errorf("duplicate JSON object member %q at $", name)
			}
			seen[name] = struct{}{}
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	end, err := decoder.Token()
	if err != nil {
		return err
	}
	if end != json.Delim('}') {
		return fmt.Errorf("JSON object at $ is not terminated")
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
