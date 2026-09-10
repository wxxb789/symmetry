package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode/utf8"
)

// Subject identifies the exact repository artifact used by an admission or
// validation. The tree digest is intentionally part of the identity; a Git
// commit alone does not describe untracked or generated tracked-artifact state.
type Subject struct {
	ResourceID string `json:"resource_id"`
	Commit     string `json:"commit"`
	TreeDigest string `json:"tree_digest"`
}

// MaxJSONSafeInteger is the largest integer that can be represented exactly by
// every JSON implementation that uses an IEEE-754 double for numbers.
const MaxJSONSafeInteger int64 = 9_007_199_254_740_991

const (
	SubjectSchemaVersion             = "symmetry.subject.v1"
	AdmissionSchemaVersion           = "symmetry.admission.v1"
	AdapterCapabilitiesSchemaVersion = "symmetry.adapter_capabilities.v1"
	TaskResultSchemaVersion          = "symmetry.task_result.v1"
	EvidenceSchemaVersion            = "symmetry.evidence.v1"
	UsageSchemaVersion               = "symmetry.usage.v1"
)

// Validate checks the transport-level Subject invariants.
func (subject Subject) Validate() error {
	if err := validateUUID(subject.ResourceID, "resource_id"); err != nil {
		return err
	}
	if err := validateGitObjectID(subject.Commit); err != nil {
		return err
	}
	return validateSHA256(subject.TreeDigest, "tree_digest")
}

// UnmarshalJSON rejects unknown fields and validates the complete Subject at
// the JSON boundary. Legacy protocol structs do not use this type.
func (subject *Subject) UnmarshalJSON(data []byte) error {
	var decoded subjectWire
	if err := decodeStrictObject(data, &decoded, "resource_id", "commit", "tree_digest"); err != nil {
		return err
	}
	value := Subject(decoded)
	if err := value.Validate(); err != nil {
		return err
	}
	*subject = value
	return nil
}

type subjectWire Subject

// CanonicalJSON returns the deterministic JSON representation used for the
// Subject hash. Object keys are sorted recursively; arrays, if ever added to a
// compatible envelope, retain their order.
func (subject Subject) CanonicalJSON() ([]byte, error) {
	if err := subject.Validate(); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(subjectWire(subject))
	if err != nil {
		return nil, fmt.Errorf("marshal subject: %w", err)
	}
	return CanonicalizeJSON(encoded)
}

// Hash returns the wire-form SHA-256 hash of the canonical complete Subject.
func (subject Subject) Hash() (string, error) {
	canonical, err := subject.CanonicalJSON()
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// ParseSubject decodes and validates a complete Subject.
func ParseSubject(data []byte) (Subject, error) {
	var subject Subject
	if err := json.Unmarshal(data, &subject); err != nil {
		return Subject{}, err
	}
	return subject, nil
}

// CanonicalizeJSON sorts object keys recursively while preserving array order.
// It is deliberately limited to the standard JSON data model and has no schema
// or behavior semantics; callers must validate their DTO before hashing it.
func CanonicalizeJSON(data []byte) ([]byte, error) {
	if !utf8.Valid(data) {
		return nil, errors.New("JSON must contain valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	if err := ensureDecoderEOF(decoder); err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := writeCanonicalJSON(&output, value); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func writeCanonicalJSON(output *bytes.Buffer, value any) error {
	switch value := value.(type) {
	case nil:
		output.WriteString("null")
	case bool:
		if value {
			output.WriteString("true")
		} else {
			output.WriteString("false")
		}
	case string:
		encoded, err := marshalJSONString(value)
		if err != nil {
			return err
		}
		output.Write(encoded)
	case json.Number:
		number := value.String()
		if number == "" || !json.Valid([]byte(number)) {
			return fmt.Errorf("invalid JSON number %q", number)
		}
		if number == "-0" {
			number = "0"
		}
		output.WriteString(number)
	case []any:
		output.WriteByte('[')
		for index, item := range value {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := writeCanonicalJSON(output, item); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		output.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				output.WriteByte(',')
			}
			encoded, err := marshalJSONString(key)
			if err != nil {
				return err
			}
			output.Write(encoded)
			output.WriteByte(':')
			if err := writeCanonicalJSON(output, value[key]); err != nil {
				return err
			}
		}
		output.WriteByte('}')
	default:
		return fmt.Errorf("unsupported JSON value %T", value)
	}
	return nil
}

func marshalJSONString(value string) ([]byte, error) {
	var output bytes.Buffer
	encoder := json.NewEncoder(&output)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return normalizeJSONStringLineSeparators(bytes.TrimSuffix(output.Bytes(), []byte{'\n'})), nil
}

// encoding/json escapes U+2028 and U+2029 even with HTML escaping disabled.
// Scan complete escape tokens so a literal \\u2028 or \\u2029 is left unchanged.
func normalizeJSONStringLineSeparators(encoded []byte) []byte {
	output := make([]byte, 0, len(encoded))
	for index := 0; index < len(encoded); {
		if encoded[index] != '\\' {
			output = append(output, encoded[index])
			index++
			continue
		}

		if index+1 >= len(encoded) {
			output = append(output, encoded[index])
			index++
			continue
		}

		switch encoded[index+1] {
		case '\\', '"', '/', 'b', 'f', 'n', 'r', 't':
			output = append(output, encoded[index:index+2]...)
			index += 2
		case 'u':
			if index+6 > len(encoded) {
				output = append(output, encoded[index])
				index++
				continue
			}
			switch {
			case encoded[index+2] == '2' && encoded[index+3] == '0' && encoded[index+4] == '2' && encoded[index+5] == '8':
				output = append(output, 0xe2, 0x80, 0xa8)
			case encoded[index+2] == '2' && encoded[index+3] == '0' && encoded[index+4] == '2' && encoded[index+5] == '9':
				output = append(output, 0xe2, 0x80, 0xa9)
			default:
				output = append(output, encoded[index:index+6]...)
			}
			index += 6
		default:
			output = append(output, encoded[index])
			index++
		}
	}
	return output
}

func ensureDecoderEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("JSON contains more than one value")
		}
		return fmt.Errorf("trailing JSON data: %w", err)
	}
	return nil
}

func decodeStrictObject(data []byte, target any, required ...string) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("JSON value must be an object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return fmt.Errorf("decode object fields: %w", err)
	}
	for _, field := range required {
		if _, present := fields[field]; !present {
			return fmt.Errorf("missing required field %q", field)
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("decode strict object: %w", err)
	}
	return ensureDecoderEOF(decoder)
}

func validateUUID(value, field string) error {
	if len(value) != 36 {
		return fmt.Errorf("%s must be a canonical UUID", field)
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return fmt.Errorf("%s must be a canonical UUID", field)
			}
			continue
		}
		if character >= 'A' && character <= 'F' {
			return fmt.Errorf("%s must use lowercase hexadecimal UUID digits", field)
		}
		if !isHex(character) {
			return fmt.Errorf("%s must be a canonical UUID", field)
		}
	}
	if value[14] < '1' || value[14] > '5' {
		return fmt.Errorf("%s must use a supported UUID version", field)
	}
	if !strings.ContainsRune("89ab", rune(value[19])) {
		return fmt.Errorf("%s must use a supported UUID variant", field)
	}
	return nil
}

func validateGitObjectID(value string) error {
	if len(value) != 40 && len(value) != 64 {
		return errors.New("commit must be a full Git object ID")
	}
	for _, character := range value {
		if character < '0' || (character > '9' && character < 'a') || character > 'f' {
			return errors.New("commit must be a full Git object ID")
		}
	}
	return nil
}

func validateSHA256(value, field string) error {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return fmt.Errorf("%s must be sha256:<64 lowercase hex characters>", field)
	}
	for _, character := range value[len("sha256:"):] {
		if character < '0' || (character > '9' && character < 'a') || character > 'f' {
			return fmt.Errorf("%s must be sha256:<64 lowercase hex characters>", field)
		}
	}
	return nil
}

func isHex(value rune) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}
