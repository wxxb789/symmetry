package harness

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

const (
	defaultClaudeExecutable = "claude"
	testedClaudeVersion     = "2.1.259"
)

// ClaudeCommandRunner is the narrow command boundary used by the Claude Code
// capability probe. Keeping it injectable makes version/help evidence
// deterministic without introducing a fake native session implementation.
type ClaudeCommandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

type osClaudeCommandRunner struct{}

func (osClaudeCommandRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, executable, args...).CombinedOutput()
}

// Registry is a concrete, process-local adapter registry. It is intentionally
// not a dynamic plugin loader: every native kind has an explicit constructor
// and unsupported kinds remain visible as unsupported.
type Registry struct {
	mu       sync.RWMutex
	adapters map[Kind]Adapter
}

// NewRegistry creates the process-local registry for adapters whose
// implementation lives in this package. The daemon app is the composition
// root for native adapter packages, which import this package's interfaces.
// Keeping that wiring outside this package avoids an import cycle and ensures
// a configured native executable is used for both probing and execution.
func NewRegistry() *Registry {
	registry := &Registry{adapters: make(map[Kind]Adapter)}
	_ = registry.Register(KindGeneric, NewGenericAdapter())
	_ = registry.Register(KindCodex, NewCodexAdapter())
	_ = registry.Register(KindClaude, NewClaudeAdapter())
	_ = registry.Register(KindPi, NewPiAdapter())
	_ = registry.Register(KindOpenCode, NewOpenCodeAdapter())
	return registry
}

// Register adds or replaces one explicit adapter constructor result.
func (registry *Registry) Register(kind Kind, adapter Adapter) error {
	if registry == nil {
		return errors.New("harness registry is nil")
	}
	if strings.TrimSpace(string(kind)) == "" {
		return errors.New("harness adapter kind must not be empty")
	}
	if adapter == nil {
		return fmt.Errorf("harness adapter %q is nil", kind)
	}
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.adapters == nil {
		registry.adapters = make(map[Kind]Adapter)
	}
	registry.adapters[kind] = adapter
	return nil
}

// Lookup returns an explicitly registered adapter.
func (registry *Registry) Lookup(kind Kind) (Adapter, error) {
	if registry == nil {
		return nil, errors.New("harness registry is nil")
	}
	registry.mu.RLock()
	adapter, ok := registry.adapters[kind]
	registry.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownAdapter, kind)
	}
	return adapter, nil
}

// Probe invokes the selected adapter's capability probe.
func (registry *Registry) Probe(ctx context.Context, kind Kind) (Capabilities, error) {
	adapter, err := registry.Lookup(kind)
	if err != nil {
		return Capabilities{}, err
	}
	capabilities, err := adapter.Probe(ctx)
	if capabilities.Kind != kind {
		kindErr := fmt.Errorf("registered harness adapter %q reported capability kind %q", kind, capabilities.Kind)
		if err != nil {
			return capabilities, errors.Join(err, kindErr)
		}
		return capabilities, kindErr
	}
	if validateErr := capabilities.Validate(); validateErr != nil {
		if err != nil {
			return capabilities, errors.Join(err, validateErr)
		}
		return capabilities, validateErr
	}
	return capabilities, err
}

// ValidateRequired probes one adapter and checks operation requirements.
func (registry *Registry) ValidateRequired(ctx context.Context, kind Kind, required ...Capability) (Capabilities, error) {
	capabilities, err := registry.Probe(ctx, kind)
	if err != nil {
		return capabilities, err
	}
	return capabilities, capabilities.Require(required...)
}

// UnavailableAdapter represents a native harness that is not installed or
// whose protocol is not verified for this daemon build.
type UnavailableAdapter struct {
	kind         Kind
	probe        func(context.Context) (Capabilities, error)
	capabilities Capabilities
}

func newUnavailableAdapter(kind Kind, reason string) *UnavailableAdapter {
	capabilities := UnsupportedCapabilities(kind, reason)
	return &UnavailableAdapter{kind: kind, capabilities: capabilities}
}

// NewClaudeAdapter returns the conservative Claude Code adapter using the
// executable resolved from PATH. No fake JSON protocol is substituted for its
// native stream.
func NewClaudeAdapter() *UnavailableAdapter {
	return NewClaudeAdapterWithExecutable(defaultClaudeExecutable)
}

// NewClaudeAdapterWithExecutable returns the conservative Claude Code adapter
// bound to one explicit local executable path or command name. An empty value
// preserves the default PATH lookup behavior; this constructor never discovers
// or silently falls back to another executable.
func NewClaudeAdapterWithExecutable(executable string) *UnavailableAdapter {
	return newClaudeAdapterWithRunner(executable, osClaudeCommandRunner{})
}

// NewClaudeAdapterWithRunner is the deterministic command-probe seam used by
// tests and local callers that already own executable selection.
func NewClaudeAdapterWithRunner(executable string, runner ClaudeCommandRunner) *UnavailableAdapter {
	return newClaudeAdapterWithRunner(executable, runner)
}

func newClaudeAdapterWithRunner(executable string, runner ClaudeCommandRunner) *UnavailableAdapter {
	adapter := newUnavailableAdapter(KindClaude, "Claude Code native session lifecycle is unverified")
	adapter.probe = func(ctx context.Context) (Capabilities, error) {
		return probeClaudeExecutable(ctx, executable, runner)
	}
	return adapter
}

func probeClaudeExecutable(ctx context.Context, executable string, runner ClaudeCommandRunner) (Capabilities, error) {
	if ctx == nil {
		return Capabilities{}, errors.New("harness probe context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return Capabilities{}, err
	}
	if strings.TrimSpace(executable) == "" {
		executable = defaultClaudeExecutable
	}
	if runner == nil {
		runner = osClaudeCommandRunner{}
	}

	capabilities := UnsupportedCapabilities(KindClaude, "Claude Code native session lifecycle is unverified")
	versionOutput, err := runner.Run(ctx, executable, "--version")
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return Capabilities{}, contextErr
		}
		capabilities = UnsupportedCapabilities(KindClaude, "Claude Code executable is unavailable")
		return capabilities, &AvailabilityError{
			Kind:   KindClaude,
			Reason: fmt.Sprintf("%s: %v", executable, err),
		}
	}

	version := parseClaudeVersion(string(versionOutput))
	capabilities.NativeVersion = version
	capabilities.VersionKnown = version != ""
	if version == "" {
		return capabilities, fmt.Errorf("%w: unable to parse Claude Code version from %q", ErrUnsupportedVersion, strings.TrimSpace(string(versionOutput)))
	}
	if version != testedClaudeVersion {
		return capabilities, fmt.Errorf("%w: Claude Code %s is not in the tested version set", ErrUnsupportedVersion, version)
	}

	helpOutput, err := runner.Run(ctx, executable, "--help")
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return capabilities, contextErr
		}
		return capabilities, fmt.Errorf("%w: Claude Code help probe failed: %v", ErrNativeUnverified, err)
	}
	if !hasClaudeTransportHelp(string(helpOutput)) {
		return capabilities, fmt.Errorf("%w: Claude Code help did not advertise print stream-json transport", ErrNativeUnverified)
	}
	capabilities.TransportVerified = true

	// The installed version and documented headless transport are known, but
	// no native session/control lifecycle has been behaviorally verified.
	return capabilities, ErrNativeUnverified
}

var claudeVersionPattern = regexp.MustCompile(`\b([0-9]+\.[0-9]+\.[0-9]+)\b`)

func parseClaudeVersion(output string) string {
	match := claudeVersionPattern.FindStringSubmatch(output)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func hasClaudeTransportHelp(output string) bool {
	value := strings.ToLower(output)
	return strings.Contains(value, "--print") &&
		strings.Contains(value, "--output-format") &&
		strings.Contains(value, "stream-json")
}

// NewPiAdapter returns the explicit unsupported pi adapter.
func NewPiAdapter() *UnavailableAdapter {
	return newUnavailableAdapter(KindPi, "pi executable or verified native bridge is unavailable")
}

// NewOpenCodeAdapter returns the explicit unsupported OpenCode adapter.
func NewOpenCodeAdapter() *UnavailableAdapter {
	return newUnavailableAdapter(KindOpenCode, "OpenCode executable or verified native bridge is unavailable")
}

// Probe returns an unsupported projection rather than silently omitting the
// unavailable native operations.
func (adapter *UnavailableAdapter) Probe(ctx context.Context) (Capabilities, error) {
	if ctx == nil {
		return Capabilities{}, errors.New("harness probe context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return Capabilities{}, err
	}
	if adapter.probe != nil {
		return adapter.probe(ctx)
	}
	return adapter.capabilities, &AvailabilityError{Kind: adapter.kind, Reason: adapter.reason()}
}

// Start fails closed. An unavailable native adapter cannot create a session.
func (adapter *UnavailableAdapter) Start(ctx context.Context, _ StartRequest, _ EventSink) (Session, error) {
	if ctx == nil {
		return nil, errors.New("harness start context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if adapter.probe != nil {
		_, err := adapter.probe(ctx)
		if err != nil {
			return nil, err
		}
	}
	return nil, &AvailabilityError{Kind: adapter.kind, Reason: adapter.reason()}
}

func (adapter *UnavailableAdapter) reason() string {
	if len(adapter.capabilities.Unsupported) == 0 {
		return "native harness unavailable"
	}
	return adapter.capabilities.Unsupported[string(CapabilityStart)]
}

// NewCodexAdapter returns a conservative adapter for the installed Codex CLI.
// Even the known 0.153.4 version remains unverified here: app-server help and
// JSON framing do not prove native session lifecycle or control semantics.
func NewCodexAdapter() *UnavailableAdapter {
	adapter := newUnavailableAdapter(KindCodex, "Codex app-server native session behavior is unverified")
	adapter.probe = probeCodexExecutable
	return adapter
}

func probeCodexExecutable(ctx context.Context) (Capabilities, error) {
	if ctx == nil {
		return Capabilities{}, errors.New("harness probe context must not be nil")
	}
	path, err := exec.LookPath("codex")
	if err != nil {
		capabilities := UnsupportedCapabilities(KindCodex, "codex executable is unavailable")
		return capabilities, &AvailabilityError{Kind: KindCodex, Reason: err.Error()}
	}
	output, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return Capabilities{}, contextErr
		}
		capabilities := UnsupportedCapabilities(KindCodex, "codex version probe failed")
		return capabilities, fmt.Errorf("%w: codex --version: %v", ErrHarnessUnavailable, err)
	}
	version := parseCodexVersion(string(output))
	capabilities := UnsupportedCapabilities(KindCodex, "Codex app-server native session behavior is unverified")
	capabilities.NativeVersion = version
	capabilities.VersionKnown = version != ""
	if version != "0.153.4" {
		if version == "" {
			return capabilities, fmt.Errorf("%w: unable to parse codex version from %q", ErrUnsupportedVersion, strings.TrimSpace(string(output)))
		}
		return capabilities, fmt.Errorf("%w: codex %s is not in the tested version set", ErrUnsupportedVersion, version)
	}
	helpOutput, err := exec.CommandContext(ctx, path, "app-server", "--help").CombinedOutput()
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return capabilities, contextErr
		}
		return capabilities, fmt.Errorf("%w: codex app-server help probe failed: %v", ErrNativeUnverified, err)
	}
	if !strings.Contains(strings.ToLower(string(helpOutput)), "app-server") ||
		!strings.Contains(strings.ToLower(string(helpOutput)), "stdio") {
		return capabilities, fmt.Errorf("%w: app-server help did not advertise stdio transport", ErrNativeUnverified)
	}
	capabilities.TransportVerified = true
	return capabilities, ErrNativeUnverified
}

var codexVersionPattern = regexp.MustCompile(`(?m)codex-cli\s+([0-9]+\.[0-9]+\.[0-9]+)\b`)

func parseCodexVersion(output string) string {
	match := codexVersionPattern.FindStringSubmatch(output)
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

// AvailabilityError distinguishes an absent/unverified native adapter from a
// rejected operation while preserving errors.Is(ErrHarnessUnavailable).
type AvailabilityError struct {
	Kind   Kind
	Reason string
}

func (err *AvailabilityError) Error() string {
	if err == nil {
		return ""
	}
	return fmt.Sprintf("%s: %s", err.Kind, err.Reason)
}

func (err *AvailabilityError) Unwrap() error { return ErrHarnessUnavailable }
