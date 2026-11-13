package harness

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strings"
)

const (
	// ClaudeTestedVersion and ClaudeTestedExecutableHash identify the only
	// Claude Code bundle whose probe evidence is accepted by this build.
	ClaudeTestedVersion = "2.1.323"
	// ClaudeTestedExecutableHash is the SHA-256 of the exact Claude Code
	// 2.1.323 executable observed on the verified Windows host. Native
	// capability promotion requires the resolved executable bytes to match this
	// blob: a matching version string or help text alone must never promote a
	// capability that was proved against different binary bytes. A PATH shim
	// that forwards to another file is a different blob and fails closed.
	ClaudeTestedExecutableHash = "sha256:5064e6e0572dca86257363ca04d48653eef20ef27f1bd04b504ed7199ed3fd13"
)

// claudeImplementationVersion is the promoted adapter implementation identity
// reported with a verified Claude capability projection.
const claudeImplementationVersion = "claude-stream-json-v1"

// claudeVersionPattern anchors the exact `--version` line. Claude Code prints
// `<version> (Claude Code)`; a prerelease suffix, a shim banner, extra tokens,
// or a different client on the same stream fail the gate instead of being
// parsed into a stable-looking prefix.
var claudeVersionPattern = regexp.MustCompile(`^([0-9]+\.[0-9]+\.[0-9]+) \(Claude Code\)$`)

// ExecutableDigest returns the SHA-256 of the exact executable bytes. The digest
// resolver rejects a nil or expired context before this method is reached, so
// the guard lives in HashExecutableFile.
func (osClaudeCommandRunner) ExecutableDigest(ctx context.Context, executable string) (string, error) {
	return HashExecutableFile(ctx, executable)
}

// claudeProbeEvidence is the shared result of Claude Code version and transport
// verification. It deliberately contains no native lifecycle or operation
// claims, and no executable digest: only the promotion gate hashes bytes.
type claudeProbeEvidence struct {
	Version           string
	VersionKnown      bool
	TransportVerified bool
}

func parseClaudeVersion(output string) string {
	match := claudeVersionPattern.FindStringSubmatch(strings.TrimSpace(output))
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func hasClaudeTransportHelp(output string) bool {
	value := strings.ToLower(output)
	return strings.Contains(value, "--print") &&
		strings.Contains(value, "--output-format") &&
		strings.Contains(value, "stream-json") &&
		strings.Contains(value, "--json-schema")
}

// probeClaudeNativeEvidence inspects `claude --version` and `claude --help`. It
// never starts a session and never treats help text as lifecycle evidence. A
// failed probe returns the evidence gathered so far so the caller can report a
// truthful partial identity.
func probeClaudeNativeEvidence(ctx context.Context, executable string, runner ClaudeCommandRunner) (claudeProbeEvidence, error) {
	evidence := claudeProbeEvidence{}
	if ctx == nil {
		return evidence, errors.New("Claude probe context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return evidence, err
	}
	if strings.TrimSpace(executable) == "" {
		executable = defaultClaudeExecutable
	}
	if runner == nil {
		runner = osClaudeCommandRunner{}
	}

	versionOutput, err := runClaudeProbe(ctx, func(probeContext context.Context) ([]byte, error) {
		return runner.Run(probeContext, executable, "--version")
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return evidence, err
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return evidence, contextErr
		}
		return evidence, &AvailabilityError{
			Kind:   KindClaude,
			Reason: fmt.Sprintf("%s: %v", executable, err),
		}
	}
	evidence.Version = parseClaudeVersion(string(versionOutput))
	evidence.VersionKnown = evidence.Version != ""
	if evidence.Version == "" {
		return evidence, fmt.Errorf("%w: unable to parse Claude Code version from %q", ErrUnsupportedVersion, strings.TrimSpace(string(versionOutput)))
	}
	if evidence.Version != ClaudeTestedVersion {
		return evidence, fmt.Errorf("%w: Claude Code %s is not in the tested version set", ErrUnsupportedVersion, evidence.Version)
	}

	helpOutput, err := runClaudeProbe(ctx, func(probeContext context.Context) ([]byte, error) {
		return runner.Run(probeContext, executable, "--help")
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return evidence, err
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return evidence, contextErr
		}
		return evidence, fmt.Errorf("%w: Claude Code help probe failed: %v", ErrNativeUnverified, err)
	}
	if !hasClaudeTransportHelp(string(helpOutput)) {
		return evidence, fmt.Errorf("%w: Claude Code help did not advertise print stream-json transport with --json-schema", ErrNativeUnverified)
	}
	evidence.TransportVerified = true
	return evidence, nil
}

// claudeExecutableDigest returns the SHA-256 of the exact executable bytes, or
// false when the bytes cannot be verified. Blob resolution is shared with the
// Codex probe; only the runner type is local.
func claudeExecutableDigest(ctx context.Context, executable string, runner ClaudeCommandRunner) (string, bool) {
	return ResolveExecutableDigest(ctx, executable, runner)
}

// rejectClaude marks the three native operations unsupported for reason and
// returns the fail-closed projection: no caller may observe an unvalidated
// capability map.
func rejectClaude(capabilities Capabilities, reason string) (Capabilities, error) {
	capabilities.Unsupported[string(CapabilityStart)] = reason
	capabilities.Unsupported[string(CapabilityEvents)] = reason
	capabilities.Unsupported[string(CapabilityCancel)] = reason
	if err := capabilities.Validate(); err != nil {
		return Capabilities{}, fmt.Errorf("Claude capability contract: %w", err)
	}
	return capabilities, ErrNativeUnverified
}

// claudeCapabilitiesForPlatform projects only operations proved against the
// exact tested executable blob. Version and transport agreement are necessary
// but never sufficient: the resolved executable SHA-256 must equal
// ClaudeTestedExecutableHash, and the platform must be the one the evidence was
// taken on. Linux admission stays fail-closed because the Linux v2 owner path
// requires an installed A/B systemd runtime with a delegated cgroup-v2 parent
// and cross-UID peer inspection, which the recorded host evidence does not
// provide; Windows evidence is never promoted as Linux support.
func claudeCapabilitiesForPlatform(platform string, evidence claudeProbeEvidence, executableHashMatched bool) (Capabilities, error) {
	capabilities := Capabilities{
		Kind:              KindClaude,
		NativeVersion:     evidence.Version,
		ProtocolVersion:   1,
		VersionKnown:      evidence.VersionKnown,
		TransportVerified: evidence.TransportVerified,
		Guidance:          GuidanceUnsupported,
		Pause:             PauseUnsupported,
		Usage:             UsageUnknown,
		Unsupported: map[string]string{
			string(CapabilityResume):           "Claude Code native resume is not verified",
			string(CapabilityHandoff):          "Claude Code fresh-session handoff is not verified",
			string(CapabilityGuidance):         "Claude Code native steer application is not verified",
			string(CapabilityPause):            "Claude Code process termination is cancellation, not safe pause",
			string(CapabilityApprovalResponse): "Claude Code native permission prompts have no verified response path; the promoted policy grants in-workspace file edits and denies every operation that would prompt",
			string(CapabilityUsage):            "Claude Code stream usage is not promoted as billable usage",
			string(CapabilityHardCostLimit):    "Claude Code has no verified provider-enforced cost limit",
			string(CapabilityProviderAccess):   "Claude Code native provider broker bridge is not verified",
		},
	}
	if !evidence.VersionKnown || !evidence.TransportVerified {
		reason := "Claude Code probe evidence is incomplete; native lifecycle is unverified"
		return rejectClaude(capabilities, reason)
	}
	if platform != "windows" {
		reason := "Claude Code native repository lifecycle is verified only on Windows; Linux admission needs the Linux v2 owner path (delegated cgroup-v2 parent with cross-UID peer inspection), which this build's evidence does not provide"
		return rejectClaude(capabilities, reason)
	}
	if !executableHashMatched {
		reason := "Claude Code native support is bound to the exact tested executable blob; the resolved executable SHA-256 is unverified or does not match"
		return rejectClaude(capabilities, reason)
	}
	capabilities.Verified = true
	capabilities.ImplementationVersion = claudeImplementationVersion
	capabilities.Start = true
	capabilities.Events = true
	capabilities.Cancel = true
	if err := capabilities.Validate(); err != nil {
		return Capabilities{}, fmt.Errorf("Claude capability contract: %w", err)
	}
	return capabilities, nil
}

// claudeEvidenceCapabilities is the truthful partial projection for evidence
// that has not (or cannot) promote a native lifecycle. It reports exactly the
// identity and transport facts the probe established and no operation, and it
// claims no adapter implementation identity because there is no promoted
// implementation behind it.
func claudeEvidenceCapabilities(evidence claudeProbeEvidence, probeErr error) Capabilities {
	reason := "Claude Code native session lifecycle is unverified"
	if errors.Is(probeErr, ErrHarnessUnavailable) {
		reason = "Claude Code executable is unavailable"
	}
	capabilities := UnsupportedCapabilities(KindClaude, reason)
	capabilities.NativeVersion = evidence.Version
	capabilities.VersionKnown = evidence.VersionKnown
	capabilities.TransportVerified = evidence.TransportVerified
	return capabilities
}

// probeClaudeAdapterCapabilities is the promotion gate used by the composed
// Claude adapter: probe the exact executable and project only the operations the
// evidence supports on this platform.
func probeClaudeAdapterCapabilities(ctx context.Context, executable string, runner ClaudeCommandRunner) (Capabilities, error) {
	evidence, probeErr := probeClaudeNativeEvidence(ctx, executable, runner)
	if probeErr != nil {
		capabilities := claudeEvidenceCapabilities(evidence, probeErr)
		if err := capabilities.Validate(); err != nil {
			return Capabilities{}, fmt.Errorf("Claude capability contract: %w", err)
		}
		return capabilities, probeErr
	}
	if err := ctx.Err(); err != nil {
		return claudeEvidenceCapabilities(evidence, err), err
	}
	digest, digestKnown := claudeExecutableDigest(ctx, executable, runner)
	return claudeCapabilitiesForPlatform(
		runtime.GOOS,
		evidence,
		digestKnown && digest == ClaudeTestedExecutableHash,
	)
}
