package harness

import (
	"errors"
	"strings"
	"testing"
)

func TestCodexParseVersionToleratesBoundedLeadingWarnings(t *testing.T) {
	version := CodexTestedVersion
	warning := "WARNING: proceeding, even though we could not create PATH aliases: " +
		"Refusing to create helper binaries under temporary dir \"C:\\\\Users\\\\runneradmin\\\\\""
	for _, output := range []string{
		"codex-cli " + version + "\n",
		warning + "\ncodex-cli " + version + "\n",
		warning + "\n" + warning + "\ncodex-cli " + version + "\n",
	} {
		if got := ParseCodexVersion(output); got != version {
			t.Fatalf("ParseCodexVersion(%q) = %q, want %q", output, got, version)
		}
	}
}

func TestCodexParseVersionRejectsExcessOrForeignLeadingLines(t *testing.T) {
	version := CodexTestedVersion
	warning := "WARNING: proceeding, even though we could not create PATH aliases: reason"
	excess := strings.Repeat(warning+"\n", codexVersionWarningLimit+1) + "codex-cli " + version + "\n"
	foreign := "not a codex version line\ncodex-cli " + version + "\n"
	for _, output := range []string{excess, foreign} {
		if got := ParseCodexVersion(output); got != "" {
			t.Fatalf("ParseCodexVersion(%q) = %q, want empty", output, got)
		}
	}
	// A warning after the version line does not invalidate an already complete
	// version line.
	versionFirst := "codex-cli " + version + "\n" + warning + "\n"
	if got := ParseCodexVersion(versionFirst); got != version {
		t.Fatalf("ParseCodexVersion(%q) = %q, want %q", versionFirst, got, version)
	}
}

func TestProbeRejectsNonExactVersionOutput(t *testing.T) {
	for _, versionOutput := range []string{
		"codex-cli " + CodexTestedVersion + "-alpha.1\n",
		"codex-cli " + CodexTestedVersion + "+build.1\n",
		"codex-cli " + CodexTestedVersion + " trailing-token\n",
	} {
		t.Run(versionOutput, func(t *testing.T) {
			if version, err := ValidateCodexVersionOutput(versionOutput); !errors.Is(err, ErrUnsupportedVersion) || version != "" {
				t.Fatalf("ValidateCodexVersionOutput(%q) = (%q, %v), want ErrUnsupportedVersion with no parsed version", versionOutput, version, err)
			}
		})
	}
}
