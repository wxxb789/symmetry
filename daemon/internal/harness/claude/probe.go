package claude

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

const (
	testedVersion = "2.1.323"
	// testedExecutableHash is the SHA-256 of the exact tested 2.1.323 Windows blob; version or
	// help text alone never promotes, and a PATH shim is a different blob that fails closed.
	testedExecutableHash = "sha256:5064e6e0572dca86257363ca04d48653eef20ef27f1bd04b504ed7199ed3fd13"
)

// TestedVersion is the pinned Claude Code release a Linux v2 owner witness may carry.
const TestedVersion = testedVersion

const (
	defaultExecutable = "claude"
	// probeTimeout is 5 s because the pinned 367 MB Windows client exceeds 1 s on a loaded host,
	// and a spurious deadline would fail daemon startup instead of reporting an unavailable client.
	probeTimeout          = 5 * time.Second
	probeOutputLimitBytes = 64 << 10
)

// commandRunner is the narrow command boundary used by the Claude Code
// capability probe. Keeping it injectable makes version/help evidence
// deterministic without introducing a fake native session implementation.
type commandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type osCommandRunner struct{}

func (osCommandRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	return harness.RunProbeCommand(ctx, execution.Invocation{
		Program: executable,
		Args:    args,
	}, probeOutputLimitBytes, execution.RunBoundedCommand)
}

// implementationVersion is the promoted adapter implementation identity
// reported with a verified Claude capability projection.
const implementationVersion = "claude-stream-json-v1"

// versionPattern anchors the exact `--version` line. Claude Code prints
// `<version> (Claude Code)`; a prerelease suffix, a shim banner, extra tokens,
// or a different client on the same stream fail the gate instead of being
// parsed into a stable-looking prefix.
var versionPattern = regexp.MustCompile(`^([0-9]+\.[0-9]+\.[0-9]+) \(Claude Code\)$`)

// ExecutableDigest returns the SHA-256 of the exact executable bytes. The digest
// resolver rejects a nil or expired context before this method is reached, so
// the guard lives in HashExecutableFile.
func (osCommandRunner) ExecutableDigest(ctx context.Context, executable string) (string, error) {
	return harness.HashExecutableFile(ctx, executable)
}

// probeEvidence is the shared result of Claude Code version and transport
// verification. It deliberately contains no native lifecycle or operation
// claims, and no executable digest: only the promotion gate hashes bytes.
type probeEvidence struct {
	Version           string
	VersionKnown      bool
	TransportVerified bool
}

func parseVersion(output string) string {
	match := versionPattern.FindStringSubmatch(strings.TrimSpace(output))
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func hasTransportHelp(output string) bool {
	value := strings.ToLower(output)
	return strings.Contains(value, "--print") &&
		strings.Contains(value, "--output-format") &&
		strings.Contains(value, "stream-json") &&
		strings.Contains(value, "--json-schema")
}

// probeNativeEvidence inspects `claude --version` and `claude --help`. It
// never starts a session and never treats help text as lifecycle evidence. A
// failed probe returns the evidence gathered so far so the caller can report a
// truthful partial identity.
func probeNativeEvidence(ctx context.Context, executable string, runner commandRunner) (probeEvidence, error) {
	evidence := probeEvidence{}
	if ctx == nil {
		return evidence, errors.New("Claude probe context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return evidence, err
	}
	if strings.TrimSpace(executable) == "" {
		executable = defaultExecutable
	}
	if runner == nil {
		runner = osCommandRunner{}
	}

	versionOutput, err := harness.RunBoundedProbe(ctx, probeTimeout, func(probeContext context.Context) ([]byte, error) {
		return runner.Run(probeContext, executable, "--version")
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return evidence, err
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return evidence, contextErr
		}
		return evidence, &harness.AvailabilityError{
			Kind:   harness.KindClaude,
			Reason: fmt.Sprintf("%s: %v", executable, err),
		}
	}
	evidence.Version = parseVersion(string(versionOutput))
	evidence.VersionKnown = evidence.Version != ""
	if evidence.Version == "" {
		return evidence, fmt.Errorf("%w: unable to parse Claude Code version from %q", harness.ErrUnsupportedVersion, strings.TrimSpace(string(versionOutput)))
	}
	if evidence.Version != testedVersion {
		return evidence, fmt.Errorf("%w: Claude Code %s is not in the tested version set", harness.ErrUnsupportedVersion, evidence.Version)
	}

	helpOutput, err := harness.RunBoundedProbe(ctx, probeTimeout, func(probeContext context.Context) ([]byte, error) {
		return runner.Run(probeContext, executable, "--help")
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return evidence, err
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return evidence, contextErr
		}
		return evidence, fmt.Errorf("%w: Claude Code help probe failed: %v", harness.ErrNativeUnverified, err)
	}
	if !hasTransportHelp(string(helpOutput)) {
		return evidence, fmt.Errorf("%w: Claude Code help did not advertise print stream-json transport with --json-schema", harness.ErrNativeUnverified)
	}
	evidence.TransportVerified = true
	return evidence, nil
}

// executableDigest returns the SHA-256 of the exact executable bytes, or
// false when the bytes cannot be verified. Blob resolution is shared with the
// Codex probe; only the runner type is local.
func executableDigest(ctx context.Context, executable string, runner commandRunner) (string, bool) {
	return harness.ResolveExecutableDigest(ctx, executable, runner)
}

// reject marks the three native operations unsupported for reason and
// returns the fail-closed projection: no caller may observe an unvalidated
// capability map.
func reject(capabilities harness.Capabilities, reason string) (harness.Capabilities, error) {
	capabilities.Unsupported[string(harness.CapabilityStart)] = reason
	capabilities.Unsupported[string(harness.CapabilityEvents)] = reason
	capabilities.Unsupported[string(harness.CapabilityCancel)] = reason
	if err := capabilities.Validate(); err != nil {
		return harness.Capabilities{}, fmt.Errorf("Claude capability contract: %w", err)
	}
	return capabilities, harness.ErrNativeUnverified
}

// capabilitiesForPlatform projects operations only against the exact tested executable blob;
// Linux stays fail-closed because this build has no cgroup-v2 owner-path evidence for it.
func capabilitiesForPlatform(platform string, evidence probeEvidence, executableHashMatched bool) (harness.Capabilities, error) {
	capabilities := harness.Capabilities{
		Kind:              harness.KindClaude,
		NativeVersion:     evidence.Version,
		ProtocolVersion:   1,
		VersionKnown:      evidence.VersionKnown,
		TransportVerified: evidence.TransportVerified,
		Guidance:          harness.GuidanceUnsupported,
		Pause:             harness.PauseUnsupported,
		Usage:             harness.UsageUnknown,
		Unsupported: map[string]string{
			string(harness.CapabilityResume):           "Claude Code native resume is not verified",
			string(harness.CapabilityHandoff):          "Claude Code fresh-session handoff is not verified",
			string(harness.CapabilityGuidance):         "Claude Code native steer application is not verified",
			string(harness.CapabilityPause):            "Claude Code process termination is cancellation, not safe pause",
			string(harness.CapabilityApprovalResponse): "Claude Code native permission prompts have no verified response path; the promoted policy grants in-workspace file edits and denies every operation that would prompt",
			string(harness.CapabilityUsage):            "Claude Code stream usage is not promoted as billable usage",
			string(harness.CapabilityHardCostLimit):    "Claude Code has no verified provider-enforced cost limit",
			string(harness.CapabilityProviderAccess):   "Claude Code native provider broker bridge is not verified",
		},
	}
	if !evidence.VersionKnown || !evidence.TransportVerified {
		reason := "Claude Code probe evidence is incomplete; native lifecycle is unverified"
		return reject(capabilities, reason)
	}
	if platform != "windows" {
		reason := "Claude Code native repository lifecycle is verified only on Windows; Linux admission needs the Linux v2 owner path (delegated cgroup-v2 parent with cross-UID peer inspection), which this build's evidence does not provide"
		return reject(capabilities, reason)
	}
	if !executableHashMatched {
		reason := "Claude Code native support is bound to the exact tested executable blob; the resolved executable SHA-256 is unverified or does not match"
		return reject(capabilities, reason)
	}
	capabilities.Verified = true
	capabilities.ImplementationVersion = implementationVersion
	capabilities.Start = true
	capabilities.Events = true
	capabilities.Cancel = true
	if err := capabilities.Validate(); err != nil {
		return harness.Capabilities{}, fmt.Errorf("Claude capability contract: %w", err)
	}
	return capabilities, nil
}

// evidenceCapabilities is the truthful partial projection for evidence
// that has not (or cannot) promote a native lifecycle. It reports exactly the
// identity and transport facts the probe established and no operation, and it
// claims no adapter implementation identity because there is no promoted
// implementation behind it.
func evidenceCapabilities(evidence probeEvidence, probeErr error) harness.Capabilities {
	reason := "Claude Code native session lifecycle is unverified"
	if errors.Is(probeErr, harness.ErrHarnessUnavailable) {
		reason = "Claude Code executable is unavailable"
	}
	capabilities := harness.UnsupportedCapabilities(harness.KindClaude, reason)
	capabilities.NativeVersion = evidence.Version
	capabilities.VersionKnown = evidence.VersionKnown
	capabilities.TransportVerified = evidence.TransportVerified
	return capabilities
}

// probeCapabilities is the promotion gate used by the composed
// Claude adapter: probe the exact executable and project only the operations the
// evidence supports on this platform.
func probeCapabilities(ctx context.Context, executable string, runner commandRunner) (harness.Capabilities, error) {
	evidence, probeErr := probeNativeEvidence(ctx, executable, runner)
	if probeErr != nil {
		capabilities := evidenceCapabilities(evidence, probeErr)
		if err := capabilities.Validate(); err != nil {
			return harness.Capabilities{}, fmt.Errorf("Claude capability contract: %w", err)
		}
		return capabilities, probeErr
	}
	if err := ctx.Err(); err != nil {
		return evidenceCapabilities(evidence, err), err
	}
	digest, digestKnown := executableDigest(ctx, executable, runner)
	return capabilitiesForPlatform(
		runtime.GOOS,
		evidence,
		digestKnown && digest == testedExecutableHash,
	)
}
