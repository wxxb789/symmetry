package control

import (
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
