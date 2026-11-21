package control

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateGoalCommitPathUsesUTF8RuneLength(t *testing.T) {
	valid := strings.Repeat("\u00e9", 1024)
	if len(valid) <= 1024 {
		t.Fatalf("test path byte length = %d, want more than 1024", len(valid))
	}

	for _, test := range []struct {
		name  string
		value string
		valid bool
	}{
		{name: "1024 UTF-8 runes", value: valid, valid: true},
		{name: "1025 UTF-8 runes", value: strings.Repeat("\u00e9", 1025), valid: false},
		{name: "invalid UTF-8", value: string([]byte{'a', 0xff}), valid: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateGoalCommitPath(test.value, "path")
			if (err == nil) != test.valid {
				t.Fatalf("validateGoalCommitPath(%q) error = %v, want valid %v", test.value, err, test.valid)
			}
		})
	}
}

// TestValidateGoalUUIDPinsRejectionMessages pins the exact rejection message
// for every run ID rejection class. Version and variant share one combined
// message at this boundary, so all three bad-nibble rows pin that string.
func TestValidateGoalUUIDPinsRejectionMessages(t *testing.T) {
	const field = "run ID"
	const uuid = "1a2b3c4d-5e6f-4a7b-8c9d-0e1f2a3b4c5d"
	canonical := fmt.Sprintf("%s must be a canonical UUID", field)
	lowerHex := fmt.Sprintf("%s must use lowercase hexadecimal UUID digits", field)
	combined := fmt.Sprintf("%s must use a supported UUID version and variant", field)

	for _, test := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "valid", value: uuid},
		{name: "too short", value: uuid[:35], want: canonical},
		{name: "36 bytes 35 runes", value: uuid[:15] + "é" + uuid[17:], want: lowerHex},
		{name: "36 runes 37 bytes", value: uuid[:35] + "\u00e9", want: canonical},
		{name: "misplaced dash", value: uuid[:8] + "0" + uuid[9:], want: canonical},
		{name: "uppercase hex digit", value: uuid[:15] + "A" + uuid[16:], want: lowerHex},
		{name: "non-hex lowercase", value: uuid[:16] + "g" + uuid[17:], want: lowerHex},
		{name: "non-hex uppercase outside A-F", value: uuid[:16] + "Z" + uuid[17:], want: lowerHex},
		{name: "unsupported version", value: uuid[:14] + "0" + uuid[15:], want: combined},
		{name: "version six above the supported range", value: uuid[:14] + "6" + uuid[15:], want: combined},
		{name: "unsupported variant", value: uuid[:19] + "0" + uuid[20:], want: combined},
		{name: "variant c above the supported range", value: uuid[:19] + "c" + uuid[20:], want: combined},
		{name: "version and variant both unsupported", value: uuid[:14] + "0" + uuid[15:19] + "0" + uuid[20:], want: combined},
		{name: "length beats uppercase", value: uuid[:15] + "A" + uuid[16:35], want: canonical},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateGoalUUID(test.value, field)
			if test.want == "" {
				if err != nil {
					t.Fatalf("validateGoalUUID(%q, %q) error = %v, want accept", test.value, field, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateGoalUUID(%q, %q) accepted, want %q", test.value, field, test.want)
			}
			if got := err.Error(); got != test.want {
				t.Fatalf("validateGoalUUID(%q, %q) error = %q, want %q", test.value, field, got, test.want)
			}
		})
	}
}

// TestValidateGoalShortIdentifierPinsRejectionMessages pins the exact
// rejection message for every control call-site rejection class, using the
// `evidence.evidence_key` field string (validation.go:962). The length check
// precedes the identifier scan, so an over-length value pins the length body
// even when it also carries an invalid first rune.
func TestValidateGoalShortIdentifierPinsRejectionMessages(t *testing.T) {
	const field = "evidence.evidence_key"
	length := fmt.Sprintf("%s must contain 1..128 characters", field)
	identifier := fmt.Sprintf("%s has an invalid identifier", field)

	for _, test := range []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty", value: "", want: length},
		{name: "one character", value: "a"},
		{name: "127 characters", value: strings.Repeat("a", 127)},
		{name: "128 characters", value: strings.Repeat("a", 128)},
		{name: "129 characters", value: strings.Repeat("a", 129), want: length},
		{name: "leading dot", value: ".goal", want: identifier},
		{name: "leading hyphen", value: "-goal", want: identifier},
		{name: "leading digit", value: "1goal"},
		{name: "slash separator", value: "goal/read", want: identifier},
		{name: "space separator", value: "goal read", want: identifier},
		{name: "non-ASCII first character", value: "\u00e9goal", want: identifier},
		{name: "non-ASCII later character", value: "goal\u00e9read", want: identifier},
		{name: "dot separator", value: "goal.read"},
		{name: "hyphen separator", value: "goal-read"},
		{name: "underscore separator", value: "goal_read"},
		{name: "colon separator", value: "goal:read"},
		{name: "mixed separators", value: "gpt-5.2-claude_code:x"},
		{name: "length beats leading dot", value: strings.Repeat("a", 128) + ".", want: length},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateGoalShortIdentifier(test.value, field)
			if test.want == "" {
				if err != nil {
					t.Fatalf("validateGoalShortIdentifier(%q, %q) error = %v, want accept", test.value, field, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateGoalShortIdentifier(%q, %q) accepted, want %q", test.value, field, test.want)
			}
			if got := err.Error(); got != test.want {
				t.Fatalf("validateGoalShortIdentifier(%q, %q) error = %q, want %q", test.value, field, got, test.want)
			}
		})
	}
}
