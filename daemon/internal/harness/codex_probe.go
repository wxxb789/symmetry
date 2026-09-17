package harness

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	// CodexTestedVersion and CodexTestedSchemaHash identify the only Codex
	// transport bundle whose probe evidence is accepted by this build.
	CodexTestedVersion    = "0.153.4"
	CodexTestedSchemaHash = "sha256:d3eace08be5dca386bfd1f1e8df650058b4113f1e10870a284d775d75517576a"
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

// ParseCodexVersion accepts only the complete stable `codex-cli X.Y.Z` line.
// Anchoring the expression rejects prerelease/build suffixes and trailing
// diagnostic tokens instead of silently extracting a stable-looking prefix.
func ParseCodexVersion(output string) string {
	match := codexVersionPattern.FindStringSubmatch(strings.TrimSpace(output))
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
