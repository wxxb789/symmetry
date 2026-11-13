// Package codex contains conservative Codex app-server boundary helpers.
// Version/help inspection and JSON framing are not evidence that a native
// session or control method is supported.
package codex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

const (
	DefaultExecutable     = "codex"
	TestedVersion         = harness.CodexTestedVersion
	TestedSchemaHash      = harness.CodexTestedSchemaHash
	TestedExecutableHash  = harness.CodexTestedExecutableHash
	probeTimeout          = time.Second
	probeOutputLimitBytes = 64 << 10
)

// CommandRunner is injectable so version/help probing remains deterministic in
// unit tests and never needs a fake native event protocol.
type CommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

// SchemaRunner is implemented by the production command runner and by tests
// that provide a separately captured generated schema hash. Keeping schema
// generation out of CommandRunner prevents a directory-writing command from
// being reduced to misleading stdout fixture bytes.
type SchemaRunner interface {
	SchemaDigest(context.Context, string) (string, error)
}

// ExecutableHasher is implemented by runners that can supply the SHA-256 of the
// exact executable bytes. The production runner hashes the resolved file; tests
// may supply a recorded digest so the promotion gate stays deterministic.
type ExecutableHasher interface {
	ExecutableDigest(context.Context, string) (string, error)
}

// boundedCommand is optional so tests can inspect invocation construction
// without launching a native executable; the zero value uses the shared runner.
type boundedCommand func(context.Context, execution.Invocation, int) ([]byte, error)

type osCommandRunner struct {
	boundedCommand boundedCommand
}

func (runner osCommandRunner) runBoundedCommand(ctx context.Context, invocation execution.Invocation) ([]byte, error) {
	if runner.boundedCommand != nil {
		return runner.boundedCommand(ctx, invocation, probeOutputLimitBytes)
	}
	return execution.RunBoundedCommand(ctx, invocation, probeOutputLimitBytes)
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

func runSchemaProbe(ctx context.Context, run func(context.Context) (string, error)) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	probeContext, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	digest, err := run(probeContext)
	if contextErr := ctx.Err(); contextErr != nil {
		return "", contextErr
	}
	if contextErr := probeContext.Err(); contextErr != nil {
		return "", contextErr
	}
	return digest, err
}

func (runner osCommandRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	environment, err := execution.BuildEnvironment()
	if err != nil {
		return nil, err
	}
	return runner.runBoundedCommand(ctx, execution.Invocation{
		Program: executable,
		Args:    args,
		Env:     environment,
	})
}

func (runner osCommandRunner) SchemaDigest(ctx context.Context, executable string) (digest string, resultErr error) {
	if ctx == nil {
		return "", errors.New("codex schema probe context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	directory, err := os.MkdirTemp("", "symmetry-codex-schema-")
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return "", contextErr
		}
		return "", fmt.Errorf("create Codex schema temp directory: %w", err)
	}
	defer func() {
		if cleanupErr := os.RemoveAll(directory); cleanupErr != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove Codex schema temp directory: %w", cleanupErr))
		}
	}()
	environment, err := execution.BuildEnvironment()
	if err != nil {
		return "", err
	}
	output, err := runner.runBoundedCommand(ctx, execution.Invocation{
		Program: executable,
		Args:    []string{"app-server", "generate-json-schema", "--out", directory},
		Env:     environment,
	})
	if contextErr := ctx.Err(); contextErr != nil {
		return "", contextErr
	}
	if err != nil {
		return "", fmt.Errorf("generate Codex app-server schema: %w: %s", err, strings.TrimSpace(string(output)))
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	bundle, err := os.ReadFile(filepath.Join(directory, "codex_app_server_protocol.v2.schemas.json"))
	if contextErr := ctx.Err(); contextErr != nil {
		return "", contextErr
	}
	if err != nil {
		return "", fmt.Errorf("read generated Codex v2 schema: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	digestBytes := sha256.Sum256(bundle)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(digestBytes[:]), nil
}

// ExecutableDigest returns the SHA-256 of the exact executable bytes. The
// promotion gate requires this value to equal TestedExecutableHash before any
// native capability is claimed.
func (runner osCommandRunner) ExecutableDigest(ctx context.Context, executable string) (string, error) {
	if ctx == nil {
		return "", errors.New("codex executable digest context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return hashExecutableFile(ctx, executable)
}

// executableDigest returns the SHA-256 of the exact executable bytes, or false
// when the bytes cannot be verified. An unverifiable executable never promotes a
// native capability, so callers fail closed instead of failing the probe.
func executableDigest(ctx context.Context, executable string, runner CommandRunner) (string, bool) {
	if ctx == nil || ctx.Err() != nil {
		return "", false
	}
	if hasher, ok := runner.(ExecutableHasher); ok {
		digest, err := hasher.ExecutableDigest(ctx, executable)
		if err != nil || ctx.Err() != nil {
			return "", false
		}
		return digest, digest != ""
	}
	digest, err := hashExecutableFile(ctx, executable)
	if err != nil {
		return "", false
	}
	return digest, true
}

func hashExecutableFile(ctx context.Context, executable string) (string, error) {
	path, err := exec.LookPath(executable)
	if err != nil {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hasher := sha256.New()
	buffer := make([]byte, 1<<20)
	for {
		if contextErr := ctx.Err(); contextErr != nil {
			return "", contextErr
		}
		count, readErr := file.Read(buffer)
		if count > 0 {
			if _, writeErr := hasher.Write(buffer[:count]); writeErr != nil {
				return "", writeErr
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return "sha256:" + hex.EncodeToString(hasher.Sum(nil)), nil
}

// ProbeResult records executable/version/help, generated-schema, and the
// platform-specific native lifecycle claim. NativeSessionVerified is true only
// where credentialed repository lifecycle, cancellation, artifact and
// negative-permission evidence bind this exact version, schema, and executable
// bytes.
type ProbeResult struct {
	Capabilities          harness.Capabilities
	Executable            string
	Version               string
	VersionKnown          bool
	TransportKnown        bool
	SchemaDigest          string
	SchemaKnown           bool
	ExecutableDigest      string
	ExecutableHashKnown   bool
	NativeSessionVerified bool
}

// Probe inspects `codex --version`, `codex app-server --help`, and the exact
// generated v2 schema bundle. It does not start app-server or assume that
// help/schema output proves native lifecycle behavior. The optional runner is
// useful for deterministic tests; the first runner is used when supplied.
func Probe(ctx context.Context, executable string, runners ...CommandRunner) (ProbeResult, error) {
	if ctx == nil {
		return ProbeResult{}, errors.New("codex probe context must not be nil")
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
			harness.KindCodex,
			"Codex app-server native session behavior is unverified",
		),
	}
	versionOutput, err := runProbe(ctx, func(probeContext context.Context) ([]byte, error) {
		return runner.Run(probeContext, executable, "--version")
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return result, err
		}
		result.Capabilities.Unsupported[string(harness.CapabilityStart)] = "codex version probe failed"
		return result, fmt.Errorf("%w: codex --version: %v", harness.ErrHarnessUnavailable, err)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	version, versionErr := harness.ValidateCodexVersionOutput(string(versionOutput))
	result.Version = version
	result.VersionKnown = version != ""
	result.Capabilities.NativeVersion = version
	result.Capabilities.VersionKnown = result.VersionKnown
	if versionErr != nil {
		return result, versionErr
	}
	helpOutput, err := runProbe(ctx, func(probeContext context.Context) ([]byte, error) {
		return runner.Run(probeContext, executable, "app-server", "--help")
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return result, err
		}
		return result, fmt.Errorf("%w: codex app-server help probe failed: %v", harness.ErrNativeUnverified, err)
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := harness.ValidateCodexHelpOutput(string(helpOutput)); err != nil {
		return result, err
	}
	result.TransportKnown = true
	result.Capabilities.TransportVerified = true
	schemaRunner, schemaCaptured := runner.(SchemaRunner)
	digest := ""
	if schemaCaptured {
		digest, err = runSchemaProbe(ctx, func(probeContext context.Context) (string, error) {
			return schemaRunner.SchemaDigest(probeContext, executable)
		})
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return result, err
			}
			return result, fmt.Errorf("%w: Codex app-server schema probe failed: %v", harness.ErrNativeUnverified, err)
		}
	}
	evidence, verifyErr := harness.ValidateCodexProbeEvidence(string(versionOutput), string(helpOutput), digest, schemaCaptured)
	result.Version = evidence.Version
	result.VersionKnown = evidence.VersionKnown
	result.Capabilities.NativeVersion = evidence.Version
	result.Capabilities.VersionKnown = evidence.VersionKnown
	result.TransportKnown = evidence.TransportVerified
	result.Capabilities.TransportVerified = evidence.TransportVerified
	result.SchemaDigest = evidence.SchemaDigest
	result.SchemaKnown = evidence.SchemaKnown
	if verifyErr != nil && !errors.Is(verifyErr, harness.ErrNativeUnverified) {
		return result, verifyErr
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.ExecutableDigest, result.ExecutableHashKnown = executableDigest(ctx, executable, runner)
	if err := ctx.Err(); err != nil {
		return result, err
	}
	capabilities, nativeErr := verifiedCapabilitiesForPlatform(runtime.GOOS, evidence, result.ExecutableHashKnown && result.ExecutableDigest == TestedExecutableHash)
	result.Capabilities = capabilities
	result.NativeSessionVerified = capabilities.Verified
	return result, nativeErr
}

const ImplementationVersion = "codex-app-server-v1"

// verifiedCapabilitiesForPlatform keeps the native claim platform-specific.
// Codex 0.167.2 and 0.168.0 start every shell tool child in a detached session
// (codex-rs/core/src/spawn.rs), which the process-group owner cannot contain on
// Linux. Windows containment is verified by the Job object and the credentialed
// smoke. Linux remains explicitly unverified until the Linux v2 cgroup owner
// carries Codex evidence. The Windows claim additionally requires the resolved
// executable bytes to equal the exact tested blob: matching version output and
// schema bundle are necessary but never sufficient to promote a capability.
func verifiedCapabilitiesForPlatform(platform string, evidence harness.CodexProbeEvidence, executableHashMatched bool) (harness.Capabilities, error) {
	capabilities := harness.Capabilities{
		Kind:                  harness.KindCodex,
		NativeVersion:         evidence.Version,
		ImplementationVersion: ImplementationVersion,
		ProtocolVersion:       1,
		VersionKnown:          evidence.VersionKnown,
		TransportVerified:     evidence.TransportVerified,
		Guidance:              harness.GuidanceUnsupported,
		Pause:                 harness.PauseUnsupported,
		Usage:                 harness.UsageUnknown,
		Unsupported: map[string]string{
			string(harness.CapabilityResume):           "Codex native resume is not verified",
			string(harness.CapabilityHandoff):          "Codex fresh-session handoff is not verified",
			string(harness.CapabilityGuidance):         "Codex native steer application is not verified",
			string(harness.CapabilityPause):            "Codex interruption is cancellation, not safe pause",
			string(harness.CapabilityApprovalResponse): "Codex approval response mapping is not verified",
			string(harness.CapabilityUsage):            "Codex token counters are cumulative and not promoted as billable usage",
			string(harness.CapabilityHardCostLimit):    "Codex app-server has no verified provider-enforced cost limit",
			string(harness.CapabilityProviderAccess):   "Codex native provider broker bridge is not verified",
		},
	}
	if !evidence.VersionKnown || !evidence.TransportVerified || !evidence.SchemaKnown {
		reason := "Codex probe evidence is incomplete; native lifecycle is unverified"
		capabilities.Unsupported[string(harness.CapabilityStart)] = reason
		capabilities.Unsupported[string(harness.CapabilityEvents)] = reason
		capabilities.Unsupported[string(harness.CapabilityCancel)] = reason
		if err := capabilities.Validate(); err != nil {
			return harness.Capabilities{}, fmt.Errorf("codex capability contract: %w", err)
		}
		return capabilities, harness.ErrNativeUnverified
	}
	if platform != "windows" {
		reason := "Codex native repository lifecycle is verified only on Windows; Linux shell tool children detach into a new session that the process-group owner cannot contain"
		capabilities.Unsupported[string(harness.CapabilityStart)] = reason
		capabilities.Unsupported[string(harness.CapabilityEvents)] = reason
		capabilities.Unsupported[string(harness.CapabilityCancel)] = reason
		if err := capabilities.Validate(); err != nil {
			return harness.Capabilities{}, fmt.Errorf("codex capability contract: %w", err)
		}
		return capabilities, harness.ErrNativeUnverified
	}
	if !executableHashMatched {
		reason := "Codex native support is bound to the exact tested executable blob; the resolved executable SHA-256 is unverified or does not match"
		capabilities.Unsupported[string(harness.CapabilityStart)] = reason
		capabilities.Unsupported[string(harness.CapabilityEvents)] = reason
		capabilities.Unsupported[string(harness.CapabilityCancel)] = reason
		if err := capabilities.Validate(); err != nil {
			return harness.Capabilities{}, fmt.Errorf("codex capability contract: %w", err)
		}
		return capabilities, harness.ErrNativeUnverified
	}
	capabilities.Verified = true
	capabilities.Start = true
	capabilities.Events = true
	capabilities.Cancel = true
	if err := capabilities.Validate(); err != nil {
		return harness.Capabilities{}, fmt.Errorf("codex capability contract: %w", err)
	}
	return capabilities, nil
}

func parseVersion(output string) string {
	return harness.ParseCodexVersion(output)
}

// Adapter is a root-seam adapter that exposes probe evidence but refuses to
// create a native session. It exists so registry callers can use this package
// without mistaking transport fixtures for native support.
type Adapter struct {
	executable               string
	runner                   CommandRunner
	startProcess             processStarter
	configuredNativeModel    string
	configuredNativeProvider string
}

// NewAdapterWithNativeIdentity creates an adapter with optional exact native
// model/provider identity resolved by local configuration. Empty values do not
// claim a binding and preserve the default constructor behavior.
func NewAdapterWithNativeIdentity(executable, nativeModel, nativeProvider string) *Adapter {
	adapter := NewAdapter(executable)
	adapter.configuredNativeModel = strings.TrimSpace(nativeModel)
	adapter.configuredNativeProvider = strings.TrimSpace(nativeProvider)
	return adapter
}

func (adapter *Adapter) Probe(ctx context.Context) (harness.Capabilities, error) {
	var result ProbeResult
	var err error
	if adapter == nil {
		return harness.Capabilities{}, errors.New("codex adapter is nil")
	}
	if adapter.runner == nil {
		result, err = Probe(ctx, adapter.executable)
	} else {
		result, err = Probe(ctx, adapter.executable, adapter.runner)
	}
	return result.Capabilities, err
}
