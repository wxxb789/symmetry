package protocol

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateCommitPathUsesUTF8RuneLength(t *testing.T) {
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
			err := validateCommitPath(test.value)
			if (err == nil) != test.valid {
				t.Fatalf("validateCommitPath(%q) error = %v, want valid %v", test.value, err, test.valid)
			}
		})
	}
}

// TestValidateShortIdentifierPinsRejectionMessages pins the exact rejection
// message for every evidence_key rejection class. The length check precedes
// the identifier scan, so an over-length value pins the length body even when
// it also carries an invalid first rune.
func TestValidateShortIdentifierPinsRejectionMessages(t *testing.T) {
	const field = "evidence_key"
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
			err := validateShortIdentifier(test.value, field)
			if test.want == "" {
				if err != nil {
					t.Fatalf("validateShortIdentifier(%q, %q) error = %v, want accept", test.value, field, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateShortIdentifier(%q, %q) accepted, want %q", test.value, field, test.want)
			}
			if got := err.Error(); got != test.want {
				t.Fatalf("validateShortIdentifier(%q, %q) error = %q, want %q", test.value, field, got, test.want)
			}
		})
	}
}
