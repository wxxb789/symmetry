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
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

const (
	DefaultExecutable = "codex"
	TestedVersion     = "0.153.4"
	TestedSchemaHash  = "sha256:d3eace08be5dca386bfd1f1e8df650058b4113f1e10870a284d775d75517576a"
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

type osCommandRunner struct{}

func (osCommandRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, executable, args...).CombinedOutput()
}

func (osCommandRunner) SchemaDigest(ctx context.Context, executable string) (string, error) {
	directory, err := os.MkdirTemp("", "symmetry-codex-schema-")
	if err != nil {
		return "", fmt.Errorf("create Codex schema temp directory: %w", err)
	}
	defer os.RemoveAll(directory)
	output, err := exec.CommandContext(ctx, executable, "app-server", "generate-json-schema", "--out", directory).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("generate Codex app-server schema: %w: %s", err, strings.TrimSpace(string(output)))
	}
	bundle, err := os.ReadFile(filepath.Join(directory, "codex_app_server_protocol.v2.schemas.json"))
	if err != nil {
		return "", fmt.Errorf("read generated Codex v2 schema: %w", err)
	}
	digest := sha256.Sum256(bundle)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// ProbeResult records executable/version/help and generated-schema evidence.
// NativeSessionVerified is intentionally always false until a real credentialed
// lifecycle test proves the exact version's methods and controls.
type ProbeResult struct {
	Capabilities          harness.Capabilities
	Executable            string
	Version               string
	VersionKnown          bool
	TransportKnown        bool
	SchemaDigest          string
	SchemaKnown           bool
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
	versionOutput, err := runner.Run(ctx, executable, "--version")
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return result, contextErr
		}
		result.Capabilities.Unsupported[string(harness.CapabilityStart)] = "codex version probe failed"
		return result, fmt.Errorf("%w: codex --version: %v", harness.ErrHarnessUnavailable, err)
	}
	result.Version = parseVersion(string(versionOutput))
	result.VersionKnown = result.Version != ""
	result.Capabilities.NativeVersion = result.Version
	result.Capabilities.VersionKnown = result.VersionKnown
	if !result.VersionKnown {
		return result, fmt.Errorf("%w: unable to parse codex version from %q", harness.ErrUnsupportedVersion, strings.TrimSpace(string(versionOutput)))
	}
	if result.Version != TestedVersion {
		return result, fmt.Errorf("%w: codex %s is not in the tested version set", harness.ErrUnsupportedVersion, result.Version)
	}

	helpOutput, err := runner.Run(ctx, executable, "app-server", "--help")
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return result, contextErr
		}
		return result, fmt.Errorf("%w: codex app-server help probe failed: %v", harness.ErrNativeUnverified, err)
	}
	result.TransportKnown = hasStdioAppServerHelp(string(helpOutput))
	result.Capabilities.TransportVerified = result.TransportKnown
	if !result.TransportKnown {
		return result, fmt.Errorf("%w: app-server help did not advertise stdio transport", harness.ErrNativeUnverified)
	}
	schemaRunner, ok := runner.(SchemaRunner)
	if !ok {
		return result, fmt.Errorf("%w: exact generated app-server schema was not captured", harness.ErrNativeUnverified)
	}
	digest, err := schemaRunner.SchemaDigest(ctx, executable)
	if err != nil {
		return result, fmt.Errorf("%w: Codex app-server schema probe failed: %v", harness.ErrNativeUnverified, err)
	}
	result.SchemaDigest = digest
	result.SchemaKnown = digest == TestedSchemaHash
	if !result.SchemaKnown {
		return result, fmt.Errorf("%w: generated Codex app-server schema %s is not the tested %s", harness.ErrUnsupportedVersion, digest, TestedSchemaHash)
	}
	// The exact version and stdio framing are known, but no native lifecycle
	// and control behavior is claimed. Start remains fail-closed until
	// credentialed evidence.
	return result, harness.ErrNativeUnverified
}

var versionPattern = regexp.MustCompile(`(?m)codex-cli\s+([0-9]+\.[0-9]+\.[0-9]+)\b`)

func parseVersion(output string) string {
	match := versionPattern.FindStringSubmatch(output)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func hasStdioAppServerHelp(output string) bool {
	value := strings.ToLower(output)
	return strings.Contains(value, "app-server") && strings.Contains(value, "stdio")
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

// NewAdapterWithNativeModel creates an adapter with an optional explicitly
// configured native model identity. Admission model profiles are Symmetry
// aliases and are intentionally not forwarded as Codex model IDs; this value
// is only for deployments that already resolved such an identity locally.
func NewAdapterWithNativeModel(executable, nativeModel string) *Adapter {
	return NewAdapterWithNativeIdentity(executable, nativeModel, "")
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
