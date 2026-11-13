package harness

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	// CodexTestedVersion and CodexTestedSchemaHash identify the only Codex
	// transport bundle whose probe evidence is accepted by this build.
	CodexTestedVersion    = "0.168.0"
	CodexTestedSchemaHash = "sha256:893cc2379d96a7f72394b69abc2b40a5a7da039854b211c1137dcbd26f757afe"
	// CodexTestedExecutableHash is the SHA-256 of the exact Codex 0.168.0
	// executable observed on the verified Windows host. Native capability
	// promotion requires the resolved executable bytes to match this blob: a
	// matching version string or schema bundle alone must never promote a
	// capability that was proved against different binary bytes.
	CodexTestedExecutableHash = "sha256:ad8296f7d95eea56bc4bab33cef025eb000340a7252ddb83dd41041ebcc65d0b"
)

// CodexProbeEvidence is the shared conservative result of version, transport,
// and generated-schema verification. It deliberately contains no native
// lifecycle or operation claims.
type CodexProbeEvidence struct {
	Version           string
	VersionKnown      bool
	TransportVerified bool
	SchemaDigest      string
	SchemaKnown       bool
}

// ValidateCodexProbeEvidence applies the strict Codex compatibility gate used
// by both the registry probe and the concrete Codex adapter. A missing schema
// capture remains an explicit native-unverified result rather than promoting
// version/help evidence to a supported transport.
func ValidateCodexProbeEvidence(versionOutput, helpOutput, schemaDigest string, schemaCaptured bool) (CodexProbeEvidence, error) {
	version, versionErr := ValidateCodexVersionOutput(versionOutput)
	evidence := CodexProbeEvidence{Version: version, VersionKnown: version != ""}
	if versionErr != nil {
		return evidence, versionErr
	}

	if err := ValidateCodexHelpOutput(helpOutput); err != nil {
		return evidence, err
	}
	evidence.TransportVerified = true
	if !schemaCaptured {
		return evidence, fmt.Errorf("%w: exact generated app-server schema was not captured", ErrNativeUnverified)
	}

	evidence.SchemaDigest = schemaDigest
	evidence.SchemaKnown = schemaDigest == CodexTestedSchemaHash
	if !evidence.SchemaKnown {
		return evidence, fmt.Errorf("%w: generated Codex app-server schema %s is not the tested %s", ErrUnsupportedVersion, schemaDigest, CodexTestedSchemaHash)
	}
	return evidence, ErrNativeUnverified
}

// ValidateCodexVersionOutput applies the exact stable-version gate before a
// probe proceeds to help or schema inspection.
func ValidateCodexVersionOutput(output string) (string, error) {
	version := ParseCodexVersion(output)
	if version == "" {
		return "", fmt.Errorf("%w: unable to parse codex version from %q", ErrUnsupportedVersion, strings.TrimSpace(output))
	}
	if version != CodexTestedVersion {
		return version, fmt.Errorf("%w: codex %s is not in the tested version set", ErrUnsupportedVersion, version)
	}
	return version, nil
}

var codexVersionPattern = regexp.MustCompile(`^codex-cli[ \t]+([0-9]+\.[0-9]+\.[0-9]+)$`)

// codexVersionWarningPrefix is the leading stderr diagnostic Codex 0.168.0
// emits when it refuses to create its own PATH aliases (for example when the
// Codex home resolves directly under a user profile root). The bounded probe
// capture merges both streams, so the version gate tolerates a bounded number
// of these lines before the version line. It is a benign warning, not a
// compatibility signal.
const codexVersionWarningPrefix = "WARNING:"

// codexVersionWarningLimit bounds tolerated leading diagnostics so a stream of
// unrelated output cannot be mistaken for version evidence.
const codexVersionWarningLimit = 4

// ParseCodexVersion accepts only the complete stable `codex-cli X.Y.Z` line,
// optionally preceded by a bounded number of the exact Codex `WARNING:` alias
// lines. Anchoring the version expression rejects prerelease/build suffixes and
// trailing diagnostic tokens instead of silently extracting a stable-looking
// prefix, and any other line before the version still fails the parse.
func ParseCodexVersion(output string) string {
	trimmed := strings.TrimSpace(output)
	lines := strings.Split(trimmed, "\n")
	start := 0
	for start < len(lines) && start < codexVersionWarningLimit && strings.HasPrefix(strings.TrimSpace(lines[start]), codexVersionWarningPrefix) {
		start++
	}
	if start >= len(lines) {
		return ""
	}
	match := codexVersionPattern.FindStringSubmatch(strings.TrimSpace(lines[start]))
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

// HasCodexStdioAppServerHelp reports whether help text advertises the fixed
// app-server stdio transport.
func HasCodexStdioAppServerHelp(output string) bool {
	value := strings.ToLower(output)
	return strings.Contains(value, "app-server") && strings.Contains(value, "stdio")
}

// ValidateCodexHelpOutput applies the strict transport-help gate before a
// probe proceeds to generated-schema inspection.
func ValidateCodexHelpOutput(output string) error {
	if !HasCodexStdioAppServerHelp(output) {
		return fmt.Errorf("%w: app-server help did not advertise stdio transport", ErrNativeUnverified)
	}
	return nil
}
