package pi

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

const (
	DefaultExecutable = "pi"
	TestedVersion     = "0.85.1"
)

// CommandRunner permits deterministic version/help probe tests without a
// native agent session.
type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type osCommandRunner struct{}

func (osCommandRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, executable, args...).CombinedOutput()
}

// ProbeResult records only executable and documented transport evidence. It
// never asserts that pi native lifecycle behavior has been verified.
type ProbeResult struct {
	Capabilities          harness.Capabilities
	Executable            string
	Version               string
	VersionKnown          bool
	TransportKnown        bool
	NativeSessionVerified bool
}

// Probe inspects the installed executable's version and help. It deliberately
// returns ErrNativeUnverified even for the captured version because a help
// screen and synthetic replay are not native lifecycle evidence.
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
	versionOutput, err := runner.Run(ctx, executable, "--version")
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return result, contextErr
		}
		result.Capabilities.Unsupported[string(harness.CapabilityStart)] = "pi version probe failed"
		return result, fmt.Errorf("%w: pi --version: %v", harness.ErrHarnessUnavailable, err)
	}
	result.Version = parseVersion(string(versionOutput))
	result.VersionKnown = result.Version != ""
	result.Capabilities.NativeVersion = result.Version
	result.Capabilities.VersionKnown = result.VersionKnown
	if !result.VersionKnown {
		return result, fmt.Errorf("%w: unable to parse pi version from %q", harness.ErrUnsupportedVersion, strings.TrimSpace(string(versionOutput)))
	}
	if result.Version != TestedVersion {
		return result, fmt.Errorf("%w: pi %s is not in the tested version set", harness.ErrUnsupportedVersion, result.Version)
	}
	helpOutput, err := runner.Run(ctx, executable, "--help")
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return result, contextErr
		}
		return result, fmt.Errorf("%w: pi help probe failed: %v", harness.ErrNativeUnverified, err)
	}
	result.TransportKnown = hasRPCModeHelp(string(helpOutput))
	result.Capabilities.TransportVerified = result.TransportKnown
	if !result.TransportKnown {
		return result, fmt.Errorf("%w: pi help did not advertise --mode rpc", harness.ErrNativeUnverified)
	}
	return result, harness.ErrNativeUnverified
}

var versionPattern = regexp.MustCompile(`(?m)^\s*([0-9]+\.[0-9]+\.[0-9]+)\s*$`)

func parseVersion(output string) string {
	match := versionPattern.FindStringSubmatch(output)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func hasRPCModeHelp(output string) bool {
	value := strings.ToLower(output)
	return strings.Contains(value, "--mode <mode>") && strings.Contains(value, "rpc")
}
