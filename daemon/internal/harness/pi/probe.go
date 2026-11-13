package pi

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
	DefaultExecutable = "pi"
	TestedVersion     = "1.5.2"

	// TestedExecutableHash is the SHA-256 of the exact pi 1.5.2 Windows
	// executable whose repository lifecycle, cancellation and retained resume
	// this build promotes. Native capability promotion requires the resolved
	// executable bytes to match this blob: a matching version line and help
	// screen alone must never promote an operation proved against different
	// bytes. The mise PATH shim reports the same `1.5.2` line, forwards to
	// another file, and is therefore a different blob that promotes nothing.
	TestedExecutableHash = "sha256:40a062bedd75767e2c3c889265de7c83d64a0ba55c74b60243c2453d97cffd85"

	// testedLinuxExecutableHash is the recorded SHA-256 of the exact pi 1.5.2
	// Linux asset. It bounds the opt-in evidence tests on Linux and never
	// promotes an operation: Linux admission needs the Linux v2 owner path
	// (delegated cgroup-v2 parent with cross-UID peer inspection), which this
	// build's evidence does not provide.
	testedLinuxExecutableHash = "sha256:6a2ac1c5b0d08b91ce6095591c1960b469d0665c7fc5f32812960a4a17ca0c57"

	// piImplementationVersion is the promoted pi RPC implementation identity
	// reported with a verified capability projection, and the value a runtime
	// configuration selects through adapter_version.
	piImplementationVersion = "pi-rpc-v1"

	// probeTimeout bounds one pi probe command. The pinned 1.5.2 executable
	// answers `--version` in 0.66-0.72 s and `--help` in 1.59-1.75 s when
	// measured on an idle host (2026-11-14, Windows x64, direct install path),
	// so a one-second bound reported context.DeadlineExceeded for the documented
	// transport gate. A deadline is not a tolerated probe sentinel, so that
	// bound would fail a daemon startup instead of reporting the client's real
	// identity. Five seconds keeps the probe bounded while tolerating host
	// contention: a broken, slow or hanging executable still fails closed
	// inside this bound.
	probeTimeout     = 5 * time.Second
	probeOutputLimit = 64 * 1024
)

// CommandRunner permits deterministic version/help probe tests without a
// native agent session.
type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type osCommandRunner struct{}

// ExecutableDigest implements harness.ExecutableHasher for the production probe
// path. The digest resolver rejects a nil or expired context before this method
// is reached, so the guard lives in harness.HashExecutableFile.
func (osCommandRunner) ExecutableDigest(ctx context.Context, executable string) (string, error) {
	return harness.HashExecutableFile(ctx, executable)
}

func runProbe(ctx context.Context, run func(context.Context) ([]byte, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	probeContext, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	output, err := run(probeContext)
	if contextErr := ctx.Err(); contextErr != nil {
		return nil, contextErr
	}
	if contextErr := probeContext.Err(); contextErr != nil {
		return nil, contextErr
	}
	return output, err
}

func (osCommandRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	environment, err := execution.BuildEnvironment()
	if err != nil {
		return nil, err
	}
	return execution.RunBoundedCommand(ctx, execution.Invocation{
		Program: executable,
		Args:    args,
		Env:     environment,
	}, probeOutputLimit)
}

// ProbeResult records executable/version/help and exact-blob evidence together
// with the platform-specific native lifecycle claim. NativeSessionVerified is
// true only where repository lifecycle, cancellation and retained-resume
// evidence bind this exact version and executable bytes.
type ProbeResult struct {
	Capabilities          harness.Capabilities
	Executable            string
	Version               string
	VersionKnown          bool
	TransportKnown        bool
	ExecutableDigest      string
	ExecutableHashKnown   bool
	NativeSessionVerified bool
}

// piProbeEvidence is the shared result of version and transport verification.
// It deliberately contains no native lifecycle, operation or blob claim.
type piProbeEvidence struct {
	Version           string
	VersionKnown      bool
	TransportVerified bool
}

// probePiNativeEvidence inspects `pi --version` and `pi --help`. It never starts
// a session and never treats help text as lifecycle evidence. A failed probe
// returns the evidence gathered so far so the caller can report a truthful
// partial identity.
func probePiNativeEvidence(ctx context.Context, executable string, runner CommandRunner) (piProbeEvidence, error) {
	evidence := piProbeEvidence{}
	if ctx == nil {
		return evidence, errors.New("pi probe context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return evidence, err
	}
	if runner == nil {
		runner = osCommandRunner{}
	}
	versionOutput, err := runProbe(ctx, func(probeContext context.Context) ([]byte, error) {
		return runner.Run(probeContext, executable, "--version")
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return evidence, err
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return evidence, contextErr
		}
		return evidence, fmt.Errorf("%w: pi --version: %v", harness.ErrHarnessUnavailable, err)
	}
	evidence.Version = parseVersion(string(versionOutput))
	evidence.VersionKnown = evidence.Version != ""
	if !evidence.VersionKnown {
		return evidence, fmt.Errorf("%w: unable to parse pi version from %q", harness.ErrUnsupportedVersion, strings.TrimSpace(string(versionOutput)))
	}
	if evidence.Version != TestedVersion {
		return evidence, fmt.Errorf("%w: pi %s is not in the tested version set", harness.ErrUnsupportedVersion, evidence.Version)
	}
	helpOutput, err := runProbe(ctx, func(probeContext context.Context) ([]byte, error) {
		return runner.Run(probeContext, executable, "--help")
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return evidence, err
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return evidence, contextErr
		}
		return evidence, fmt.Errorf("%w: pi help probe failed: %v", harness.ErrNativeUnverified, err)
	}
	if !hasRPCModeHelp(string(helpOutput)) {
		return evidence, fmt.Errorf("%w: pi help did not advertise --mode rpc", harness.ErrNativeUnverified)
	}
	evidence.TransportVerified = true
	return evidence, nil
}

// piExecutableDigest returns the SHA-256 of the exact executable bytes, or false
// when the bytes cannot be verified. Only a runner that declares the shared
// harness.ExecutableHasher contract can supply a digest: a deterministic fixture
// without one stays unverifiable instead of hashing whatever this host happens
// to resolve for the same command name, so no fixture probe can promote by
// accident.
func piExecutableDigest(ctx context.Context, executable string, runner CommandRunner) (string, bool) {
	hasher, ok := runner.(harness.ExecutableHasher)
	if !ok {
		return "", false
	}
	return harness.ResolveExecutableDigest(ctx, executable, hasher)
}

// testedExecutableHash returns the recorded SHA-256 for a native platform, or
// false when this build records no tested blob for it. Only the Windows blob
// promotes a capability; the Linux blob bounds the opt-in evidence tests.
func testedExecutableHash(platform string) (string, bool) {
	switch platform {
	case "windows":
		return TestedExecutableHash, true
	case "linux":
		return testedLinuxExecutableHash, true
	default:
		return "", false
	}
}

// rejectPi marks the four staged operations unsupported for reason and returns
// the fail-closed projection: no caller may observe an unvalidated capability
// map.
func rejectPi(capabilities harness.Capabilities, reason string) (harness.Capabilities, error) {
	for _, capability := range []harness.Capability{
		harness.CapabilityStart, harness.CapabilityEvents, harness.CapabilityCancel, harness.CapabilityResume,
	} {
		capabilities.Unsupported[string(capability)] = reason
	}
	if err := capabilities.Validate(); err != nil {
		return harness.Capabilities{}, fmt.Errorf("pi capability contract: %w", err)
	}
	return capabilities, harness.ErrNativeUnverified
}

// piCapabilitiesForPlatform projects only operations proved against the exact
// tested executable blob. Version and transport agreement are necessary but
// never sufficient: the resolved executable SHA-256 must equal
// TestedExecutableHash and the platform must be the one the evidence was taken
// on. Linux admission stays fail-closed because the Linux v2 owner path requires
// an installed A/B systemd runtime with a delegated cgroup-v2 parent and
// cross-UID peer inspection, which the recorded host evidence does not provide;
// Windows evidence is never promoted as Linux support.
func piCapabilitiesForPlatform(platform string, evidence piProbeEvidence, executableHashMatched bool) (harness.Capabilities, error) {
	capabilities := harness.Capabilities{
		Kind:              harness.KindPi,
		NativeVersion:     evidence.Version,
		ProtocolVersion:   1,
		VersionKnown:      evidence.VersionKnown,
		TransportVerified: evidence.TransportVerified,
		Guidance:          harness.GuidanceUnsupported,
		Pause:             harness.PauseUnsupported,
		Usage:             harness.UsageUnknown,
		Unsupported: map[string]string{
			string(harness.CapabilityHandoff):          "pi fresh-session handoff is a daemon admission path with no observation bound to the exact tested blob",
			string(harness.CapabilityGuidance):         "pi native steer application is not verified as guidance",
			string(harness.CapabilityPause):            "pi interruption is cancellation, not safe pause",
			string(harness.CapabilityApprovalResponse): "pi native permission prompts have no verified response path; the fixed profile argv disables project approval",
			string(harness.CapabilityUsage):            "pi RPC token or cost counters are not promoted as billable usage",
			string(harness.CapabilityHardCostLimit):    "pi has no verified provider-enforced cost limit",
			string(harness.CapabilityProviderAccess):   "pi provider bridge transport is observed without a verified Control broker operation",
		},
	}
	if !evidence.VersionKnown || !evidence.TransportVerified {
		return rejectPi(capabilities, "pi probe evidence is incomplete; native lifecycle is unverified")
	}
	if platform != "windows" {
		reason := "pi native repository lifecycle is verified only on Windows; Linux admission needs the Linux v2 owner path (delegated cgroup-v2 parent with cross-UID peer inspection), which this build's evidence does not provide"
		return rejectPi(capabilities, reason)
	}
	if !executableHashMatched {
		reason := "pi native support is bound to the exact tested executable blob; the resolved executable SHA-256 is unverified or does not match"
		return rejectPi(capabilities, reason)
	}
	capabilities.Verified = true
	capabilities.ImplementationVersion = piImplementationVersion
	capabilities.Start = true
	capabilities.Events = true
	capabilities.Cancel = true
	capabilities.Resume = true
	if err := capabilities.Validate(); err != nil {
		return harness.Capabilities{}, fmt.Errorf("pi capability contract: %w", err)
	}
	return capabilities, nil
}

// Probe inspects the installed executable's version, help and exact bytes. It
// promotes only the operations that the Windows 1.5.2 evidence binds to this
// blob and returns ErrNativeUnverified everywhere else, so an unpromoted
// projection never advertises an executable capability.
func Probe(ctx context.Context, executable string, runners ...CommandRunner) (ProbeResult, error) {
	if ctx == nil {
		return ProbeResult{}, errors.New("pi probe context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return ProbeResult{}, err
	}
	if strings.TrimSpace(executable) == "" {
		executable = DefaultExecutable
	}
	var runner CommandRunner = osCommandRunner{}
	if len(runners) > 0 && runners[0] != nil {
		runner = runners[0]
	}
	result := ProbeResult{
		Executable: executable,
		Capabilities: harness.UnsupportedCapabilities(
			harness.KindPi,
			"pi native RPC lifecycle behavior is unverified",
		),
	}
	evidence, err := probePiNativeEvidence(ctx, executable, runner)
	result.Version = evidence.Version
	result.VersionKnown = evidence.VersionKnown
	result.TransportKnown = evidence.TransportVerified
	result.Capabilities.NativeVersion = evidence.Version
	result.Capabilities.VersionKnown = evidence.VersionKnown
	result.Capabilities.TransportVerified = evidence.TransportVerified
	if err != nil {
		if errors.Is(err, harness.ErrHarnessUnavailable) {
			result.Capabilities.Unsupported[string(harness.CapabilityStart)] = "pi version probe failed"
		}
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.ExecutableDigest, result.ExecutableHashKnown = piExecutableDigest(ctx, executable, runner)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	capabilities, nativeErr := piCapabilitiesForPlatform(
		runtime.GOOS,
		evidence,
		result.ExecutableHashKnown && result.ExecutableDigest == TestedExecutableHash,
	)
	result.Capabilities = capabilities
	result.NativeSessionVerified = capabilities.Verified
	return result, nativeErr
}

// piVersionPattern anchors the exact `pi --version` line. The installed client
// prints one bare semantic version; a prerelease suffix, a shim banner, extra
// tokens or a different client on the same stream fail the gate instead of being
// parsed into a stable-looking prefix.
var piVersionPattern = regexp.MustCompile(`^([0-9]+\.[0-9]+\.[0-9]+)$`)

func parseVersion(output string) string {
	match := piVersionPattern.FindStringSubmatch(strings.TrimSpace(output))
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func hasRPCModeHelp(output string) bool {
	value := strings.ToLower(output)
	return strings.Contains(value, "--mode <mode>") && strings.Contains(value, "rpc")
}
