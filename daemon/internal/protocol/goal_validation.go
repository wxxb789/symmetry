package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func objectFields(data []byte) (map[string]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, fmt.Errorf("JSON value must be an object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil, fmt.Errorf("decode object fields: %w", err)
	}
	return fields, nil
}

func isJSONObject(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return false
	}
	var value map[string]json.RawMessage
	return json.Unmarshal(trimmed, &value) == nil
}

func validateSafePositiveInt(value int64, field string) error {
	if value <= 0 || value > MaxJSONSafeInteger {
		return fmt.Errorf("%s must be a positive JSON-safe integer", field)
	}
	return nil
}

func validateSafeNonNegativeInt(value int64, field string) error {
	if value < 0 || value > MaxJSONSafeInteger {
		return fmt.Errorf("%s must be a non-negative JSON-safe integer", field)
	}
	return nil
}

func validateShortIdentifier(value, field string) error {
	if len(value) < 1 || len(value) > 128 {
		return fmt.Errorf("%s must contain 1..128 characters", field)
	}
	for index, character := range value {
		if index == 0 {
			if !isASCIIAlphaNumeric(character) {
				return fmt.Errorf("%s has an invalid identifier", field)
			}
			continue
		}
		if !isASCIIAlphaNumeric(character) && !strings.ContainsRune("._:-", character) {
			return fmt.Errorf("%s has an invalid identifier", field)
		}
	}
	return nil
}

func validateLongText(value, field string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must contain valid UTF-8", field)
	}
	length := utf8.RuneCountInString(value)
	if length < 1 || length > 16_384 {
		return fmt.Errorf("%s must contain 1..16384 characters", field)
	}
	return nil
}

func validateCommitPath(value string) error {
	if len(value) < 1 || len(value) > 1024 || value[0] == '/' || strings.ContainsRune(value, '\x00') || strings.ContainsRune(value, '\\') || strings.Contains(value, "//") {
		return fmt.Errorf("path is not a normalized relative commit path")
	}
	parts := strings.Split(value, "/")
	for _, part := range parts {
		if part == ".." {
			return fmt.Errorf("path must not traverse parent directories")
		}
	}
	return nil
}

func isASCIIAlphaNumeric(value rune) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

// ValidateMicroUSD validates the decimal-string representation required for
// wire monetary values. PostgreSQL stores these values as signed bigint, so
// values above MaxInt64 are rejected before transport.
func ValidateMicroUSD(value string) error {
	if value == "" {
		return fmt.Errorf("microusd must be a non-empty decimal string")
	}
	if len(value) > 19 {
		return fmt.Errorf("microusd must contain at most 19 digits")
	}
	if len(value) > 1 && value[0] == '0' {
		return fmt.Errorf("microusd must not contain leading zeroes")
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return fmt.Errorf("microusd must be a non-negative decimal string")
		}
	}
	if _, err := strconv.ParseUint(value, 10, 63); err != nil {
		return fmt.Errorf("microusd exceeds signed bigint range: %w", err)
	}
	return nil
}

// ParseMicroUSD returns the exact signed-bigint value after validating its
// decimal wire representation.
func ParseMicroUSD(value string) (int64, error) {
	if err := ValidateMicroUSD(value); err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseInt(value, 10, 63)
	if err != nil {
		return 0, fmt.Errorf("parse microusd: %w", err)
	}
	return parsed, nil
}

func validateUTCTimestamp(value, field string) error {
	if strings.TrimSpace(value) == "" || !strings.HasSuffix(value, "Z") {
		return fmt.Errorf("%s must be an RFC3339 UTC timestamp", field)
	}
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return fmt.Errorf("%s must be an RFC3339 UTC timestamp: %w", field, err)
	}
	return nil
}

func validateGuidance(value GuidanceCapability, field string) error {
	switch value {
	case GuidanceNativeSteer, GuidanceNextTurn, GuidanceUnsupported:
		return nil
	default:
		return fmt.Errorf("%s %q is invalid", field, value)
	}
}

func validatePause(value PauseCapability, field string) error {
	switch value {
	case PauseSafeBoundary, PauseUnsupported:
		return nil
	default:
		return fmt.Errorf("%s %q is invalid", field, value)
	}
}

func validateUsageCapability(value UsageCapability, field string) error {
	switch value {
	case UsageReported, UsageEstimated, UsageUnknown:
		return nil
	default:
		return fmt.Errorf("%s %q is invalid", field, value)
	}
}

func validateEvidenceVerdict(value EvidenceVerdict, field string) error {
	switch value {
	case EvidencePassed, EvidenceFailed, EvidenceUnknown, EvidenceNotApplicable:
		return nil
	default:
		return fmt.Errorf("%s %q is invalid", field, value)
	}
}

func validateUniqueUUIDs(values []string, field string) error {
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		if err := validateUUID(value, fmt.Sprintf("%s[%d]", field, index)); err != nil {
			return err
		}
		if _, duplicate := seen[value]; duplicate {
			return fmt.Errorf("%s contains duplicate UUID %q", field, value)
		}
		seen[value] = struct{}{}
	}
	return nil
}
