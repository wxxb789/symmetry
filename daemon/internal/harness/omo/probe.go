// Package omo implements the independent OmO classic RPC transport.
package omo

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
	DefaultExecutable           = "omo"
	TestedVersion               = "5.1.69"
	ImplementationVersion       = "omo-rpc-v1"
	TestedVersionLine           = "omo 5.1.69 (engine: senpi 2026.11.10-2; scheme nodef)"
	TestedWindowsExecutableHash = "sha256:04338dd63b24c53156492e1a0777d996034416c4131a26e1ba48eb07b5429464"
	TestedLinuxExecutableHash   = "sha256:3a74aba613cf02e740073b302171c0fb9d9155f5ff6069f91bea23972ffa1444"
	probeTimeout                = 5 * time.Second
)

// CommandRunner is the bounded executable-probe seam, not a session launcher.
type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type osCommandRunner struct{}

func (osCommandRunner) ExecutableDigest(ctx context.Context, executable string) (string, error) {
	return harness.HashExecutableFile(ctx, executable)
}

func (osCommandRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	return harness.RunProbeCommand(ctx, execution.Invocation{Program: executable, Args: args}, 64*1024, execution.RunBoundedCommand)
}

// ProbeResult separates exact executable identity from behavioral promotion.
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

var rpcModeHelp = regexp.MustCompile(`(?:^|[\s,])--mode\s+<json\|rpc>(?:[\s,]|$)`)

// Probe pins the complete version line, advertised transport and platform blob.
// Behavioral promotion is limited to the independently witnessed read/write
// lifecycle recorded under testdata/omo/5.1.69 on each platform.
func Probe(ctx context.Context, executable string, runners ...CommandRunner) (ProbeResult, error) {
	var runner CommandRunner = osCommandRunner{}
	if len(runners) > 0 && runners[0] != nil {
		runner = runners[0]
	}
	return probeForPlatform(ctx, executable, runtime.GOOS, runner)
}

func probeForPlatform(ctx context.Context, executable, platform string, runner CommandRunner) (ProbeResult, error) {
	result := ProbeResult{Executable: executable, Capabilities: harness.UnsupportedCapabilities(harness.KindOmO, "operation is not independently verified for the pinned OmO read/write profile")}
	result.Capabilities.ImplementationVersion = ImplementationVersion
	if ctx == nil {
		return result, errors.New("OmO probe requires a context")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	var expectedHash string
	switch platform {
	case "windows":
		expectedHash = TestedWindowsExecutableHash
	case "linux":
		expectedHash = TestedLinuxExecutableHash
	default:
		return result, fmt.Errorf("%w: OmO platform is not pinned", harness.ErrUnsupportedVersion)
	}
	run := func(argument string) ([]byte, error) {
		return harness.RunBoundedProbe(ctx, probeTimeout, func(probeContext context.Context) ([]byte, error) {
			output, err := runner.Run(probeContext, executable, argument)
			if len(output) > 64*1024 {
				return nil, errors.New("OmO probe output exceeds limit")
			}
			return output, err
		})
	}
	version, err := run("--version")
	if contextErr := ctx.Err(); contextErr != nil {
		return result, contextErr
	}
	if err != nil {
		return result, fmt.Errorf("%w: OmO version probe: %v", harness.ErrHarnessUnavailable, err)
	}
	if strings.TrimSuffix(strings.TrimSuffix(string(version), "\n"), "\r") != TestedVersionLine {
		return result, fmt.Errorf("%w: OmO version/engine identity differs from the exact tested line", harness.ErrUnsupportedVersion)
	}
	result.Version, result.VersionKnown = TestedVersion, true
	result.Capabilities.NativeVersion, result.Capabilities.VersionKnown = TestedVersion, true
	help, err := run("--help")
	if contextErr := ctx.Err(); contextErr != nil {
		return result, contextErr
	}
	if err != nil {
		return result, fmt.Errorf("%w: OmO help probe: %v", harness.ErrNativeUnverified, err)
	}
	if !rpcModeHelp.Match(help) {
		return result, fmt.Errorf("%w: OmO does not advertise --mode <json|rpc>", harness.ErrNativeUnverified)
	}
	result.TransportKnown = true
	result.Capabilities.TransportVerified = true
	// Fixture runners must explicitly implement executable hashing: never hash a
	// coincidental host PATH entry on behalf of an injected runner.
	hasher, ok := runner.(harness.ExecutableHasher)
	if ok {
		result.ExecutableDigest, result.ExecutableHashKnown = harness.ResolveExecutableDigest(ctx, executable, hasher)
	}
	if contextErr := ctx.Err(); contextErr != nil {
		return result, contextErr
	}
	result.ExecutableHashKnown = result.ExecutableHashKnown && result.ExecutableDigest == expectedHash
	if !result.ExecutableHashKnown {
		return result, fmt.Errorf("%w: OmO executable bytes do not match the platform pin", harness.ErrNativeUnverified)
	}
	result.NativeSessionVerified = true
	result.Capabilities.Verified = true
	result.Capabilities.Start, result.Capabilities.Events, result.Capabilities.Cancel = true, true, true
	result.Capabilities.Resume, result.Capabilities.Handoff = true, true
	for _, capability := range [...]harness.Capability{harness.CapabilityStart, harness.CapabilityEvents, harness.CapabilityCancel, harness.CapabilityResume, harness.CapabilityHandoff} {
		delete(result.Capabilities.Unsupported, string(capability))
	}
	result.Capabilities.Unsupported[string(harness.CapabilityProviderAccess)] = "native relay inference was observed; Symmetry provider broker access is not verified"
	result.Capabilities.Unsupported[string(harness.CapabilityUsage)] = "native costUnknown is true; billable usage remains unknown"
	result.Capabilities.Unsupported[string(harness.CapabilityHardCostLimit)] = "native accounting does not enforce Control hard cost limits"
	return result, nil
}
