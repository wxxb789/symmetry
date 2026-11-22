package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/harness/codex"
	"github.com/wxxb789/symmetry/daemon/internal/harness/omo"
	"github.com/wxxb789/symmetry/daemon/internal/harness/omp"
	"github.com/wxxb789/symmetry/daemon/internal/harness/pi"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func (daemon *daemon) ensureHarnessProbe(ctx context.Context) error {
	if daemon.harnessRegistry != nil && daemon.harnessCapabilities.Kind != "" {
		if err := daemon.harnessCapabilities.Validate(); err != nil {
			return fmt.Errorf("validate cached harness capability projection: %w", err)
		}
		return nil
	}
	runtime, err := normalizeRuntimeForProbe(daemon.config.Runtime)
	if err != nil {
		return err
	}
	registry := daemon.harnessRegistry
	if registry == nil {
		registry = newHarnessRegistry(daemon.config.AgentProfiles[runtime.AgentProfile])
	}
	kind, err := configuredHarnessKind(runtime.HarnessKind)
	if err != nil {
		return err
	}
	capabilities, probeErr := registry.Probe(ctx, kind)
	// Registry.Probe joins adapter failures with capability validation: validate again before
	// tolerating expected sentinels, so errors.Is on a joined error cannot hide malformation.
	if err := capabilities.Validate(); err != nil {
		return fmt.Errorf("probe %s adapter capability projection: %w", runtime.HarnessKind, err)
	}
	unverified := probeErrorContainsOnly(probeErr, harness.ErrNativeUnverified)
	unavailable := probeErrorContainsOnly(probeErr, harness.ErrHarnessUnavailable) && unavailableRuntimeKind(runtime.HarnessKind)
	if probeErr != nil && !unverified && !unavailable {
		return fmt.Errorf("probe %s adapter: %w", runtime.HarnessKind, probeErr)
	}
	if probeErr == nil && !capabilities.Verified {
		return fmt.Errorf("probe %s adapter: %w", runtime.HarnessKind, harness.ErrNativeUnverified)
	}
	if !unavailable && runtime.HarnessVersion != "legacy" && runtime.HarnessVersion != capabilities.NativeVersion {
		return fmt.Errorf("runtime.harness_version %q does not match probed %q", runtime.HarnessVersion, capabilities.NativeVersion)
	}
	if !unverified && !unavailable && runtime.AdapterVersion != "legacy" && runtime.AdapterVersion != capabilities.ImplementationVersion {
		return fmt.Errorf("runtime.adapter_version %q does not match probed %q", runtime.AdapterVersion, capabilities.ImplementationVersion)
	}
	if runtime.AdapterProtocolVersion != capabilities.ProtocolVersion {
		return fmt.Errorf("runtime.adapter_protocol_version %d does not match probed %d", runtime.AdapterProtocolVersion, capabilities.ProtocolVersion)
	}
	daemon.config.Runtime = runtime
	daemon.harnessRegistry = registry
	daemon.harnessCapabilities = capabilities
	return nil
}

// Allows a probe's expected sentinel and contextual wrappers, but never lets errors.Join
// hide an independent fatal failure.
func probeErrorContainsOnly(err, expected error) bool {
	if err == nil || expected == nil {
		return false
	}
	type unwrapMany interface{ Unwrap() []error }
	if joined, ok := err.(unwrapMany); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !probeErrorContainsOnly(child, expected) {
				return false
			}
		}
		return true
	}
	type unwrapOne interface{ Unwrap() error }
	if wrapped, ok := err.(unwrapOne); ok {
		return probeErrorContainsOnly(wrapped.Unwrap(), expected)
	}
	return errors.Is(err, expected)
}

// Daemon composition root: native adapter packages depend on harness interfaces, so they
// are wired here rather than from the harness package itself.
func newHarnessRegistry(profile config.AgentProfile) *harness.Registry {
	registry := harness.NewRegistry()
	_ = registry.Register(harness.KindCodex, codex.NewAdapterWithNativeIdentity(profile.Command, profile.NativeModel, profile.NativeModelProvider))
	_ = registry.Register(harness.KindClaude, harness.NewClaudeCandidateAdapter(profile.Command))
	_ = registry.Register(harness.KindPi, pi.NewAdapter(profile.Command))
	_ = registry.Register(harness.KindOMP, omp.NewAdapter(profile.Command))
	_ = registry.Register(harness.KindOmO, omo.NewAdapter(profile.Command))
	return registry
}

func normalizeRuntimeForProbe(runtime config.Runtime) (config.Runtime, error) {
	if runtime.HarnessKind == "" && runtime.HarnessVersion == "" && runtime.AdapterVersion == "" && runtime.AdapterProtocolVersion == 0 {
		runtime.HarnessKind = config.RuntimeHarnessGeneric
		runtime.HarnessVersion = "legacy"
		runtime.AdapterVersion = "legacy"
		runtime.AdapterProtocolVersion = 1
	}
	if runtime.HarnessKind == "" || runtime.HarnessVersion == "" || runtime.AdapterVersion == "" || runtime.AdapterProtocolVersion <= 0 {
		return config.Runtime{}, errors.New("runtime adapter metadata must include harness_kind, harness_version, adapter_version, and adapter_protocol_version")
	}
	return runtime, nil
}

func configuredHarnessKind(value string) (harness.Kind, error) {
	switch value {
	case config.RuntimeHarnessGeneric:
		return harness.KindGeneric, nil
	case config.RuntimeHarnessCodex:
		return harness.KindCodex, nil
	case config.RuntimeHarnessClaudeCode:
		return harness.KindClaude, nil
	case config.RuntimeHarnessPi:
		return harness.KindPi, nil
	case config.RuntimeHarnessOMP:
		return harness.KindOMP, nil
	case config.RuntimeHarnessOmO:
		return harness.KindOmO, nil
	case config.RuntimeHarnessOpenCode:
		return harness.KindOpenCode, nil
	default:
		return "", fmt.Errorf("runtime.harness_kind %q is unsupported", value)
	}
}

func unavailableRuntimeKind(value string) bool {
	switch value {
	case config.RuntimeHarnessClaudeCode, config.RuntimeHarnessPi, config.RuntimeHarnessOMP, config.RuntimeHarnessOmO, config.RuntimeHarnessOpenCode:
		return true
	default:
		return false
	}
}

// Identifies adapter families whose launch contract is staged (process identity, then native
// session handle, then native turn); the generic adapter deliberately remains outside.
func nativeGoalHarnessKind(kind harness.Kind) bool {
	switch kind {
	case harness.KindCodex, harness.KindClaude, harness.KindPi, harness.KindOMP, harness.KindOmO, harness.KindOpenCode:
		return true
	default:
		return false
	}
}

type runtimeRegistrationMetadata struct {
	HarnessKind            string
	HarnessVersion         string
	AdapterVersion         string
	AdapterProtocolVersion int
	Adapter                protocol.Adapter
}

const unavailableNativeMetadataVersion = "unavailable"

func buildRuntimeRegistration(runtime config.Runtime, profile config.AgentProfile, capabilities harness.Capabilities) (protocol.RuntimeRegistration, runtimeRegistrationMetadata, error) {
	if err := capabilities.Validate(); err != nil {
		return protocol.RuntimeRegistration{}, runtimeRegistrationMetadata{}, fmt.Errorf("validate adapter capabilities: %w", err)
	}
	configuredKind, err := configuredHarnessKind(runtime.HarnessKind)
	if err != nil {
		return protocol.RuntimeRegistration{}, runtimeRegistrationMetadata{}, err
	}
	if configuredKind != capabilities.Kind {
		return protocol.RuntimeRegistration{}, runtimeRegistrationMetadata{}, fmt.Errorf(
			"runtime harness kind %q does not match probed adapter kind %q",
			runtime.HarnessKind,
			capabilities.Kind,
		)
	}
	registration := protocol.RuntimeRegistration{
		RuntimeKey:           runtime.RuntimeKey,
		Name:                 runtime.Name,
		Capacity:             runtime.Capacity,
		AgentProfile:         runtime.AgentProfile,
		Workspace:            runtime.Workspace,
		RepositoryResourceID: optionalRuntimeRepositoryResourceID(runtime.RepositoryResourceID),
		Capabilities: protocol.RuntimeCapabilities{
			StructuredInput:    structuredInputForProfile(profile),
			ProviderAccess:     legacyProviderAccessForRegistration(runtime, profile) || runtime.HarnessKind == config.RuntimeHarnessPi && profile.ProviderAccess && capabilities.ProviderAccess,
			Interactive:        profile.Interactive,
			SupervisoryControl: profile.SupervisoryControl,
		},
	}
	metadata := runtimeRegistrationMetadata{
		HarnessKind:            runtime.HarnessKind,
		HarnessVersion:         runtime.HarnessVersion,
		AdapterVersion:         runtime.AdapterVersion,
		AdapterProtocolVersion: runtime.AdapterProtocolVersion,
		Adapter: protocol.Adapter{
			Kind:                  runtime.HarnessKind,
			NativeVersion:         runtime.HarnessVersion,
			ImplementationVersion: runtime.AdapterVersion,
			ProtocolVersion:       int64(runtime.AdapterProtocolVersion),
			Operations: protocol.AdapterOperations{
				Start:            capabilities.Start,
				Events:           capabilities.Events,
				Cancel:           capabilities.Cancel,
				Resume:           capabilities.Resume,
				Handoff:          capabilities.Handoff,
				Guidance:         protocol.GuidanceCapability(capabilities.Guidance),
				Pause:            protocol.PauseCapability(capabilities.Pause),
				ApprovalResponse: capabilities.ApprovalResponse,
				Usage:            protocol.UsageCapability(capabilities.Usage),
				HardCostLimit:    capabilities.HardCostLimit,
			},
		},
	}
	if runtime.HarnessKind != config.RuntimeHarnessGeneric {
		// Native identity is evidence from this probe, never an operator pin; a failed probe
		// advertises an explicit sentinel rather than stale config.
		metadata.HarnessVersion = probedMetadataVersion(capabilities.NativeVersion)
		metadata.AdapterVersion = probedMetadataVersion(capabilities.ImplementationVersion)
	}
	metadata.Adapter.NativeVersion = metadata.HarnessVersion
	metadata.Adapter.ImplementationVersion = metadata.AdapterVersion
	if err := metadata.Adapter.Validate(); err != nil {
		return protocol.RuntimeRegistration{}, runtimeRegistrationMetadata{}, fmt.Errorf("validate adapter registration: %w", err)
	}
	if err := writeRuntimeRegistrationMetadata(&registration, metadata); err != nil {
		return protocol.RuntimeRegistration{}, runtimeRegistrationMetadata{}, err
	}
	return registration, metadata, nil
}

func probedMetadataVersion(version string) string {
	if strings.TrimSpace(version) == "" {
		return unavailableNativeMetadataVersion
	}
	return version
}

func optionalRuntimeRepositoryResourceID(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func structuredInputForProfile(profile config.AgentProfile) bool {
	return profile.InputMode == config.InputModeJSON
}

// Describes the existing stdin envelope contract only: native harnesses must not advertise
// provider access until their broker bridge is independently verified.
func legacyProviderAccessForRegistration(runtime config.Runtime, profile config.AgentProfile) bool {
	return runtime.HarnessKind == config.RuntimeHarnessGeneric &&
		profile.InputMode == config.InputModeJSON && profile.ProviderAccess
}

func writeRuntimeRegistrationMetadata(registration *protocol.RuntimeRegistration, metadata runtimeRegistrationMetadata) error {
	if registration == nil {
		return errors.New("runtime registration must not be nil")
	}
	registration.HarnessKind = metadata.HarnessKind
	registration.HarnessVersion = metadata.HarnessVersion
	registration.AdapterVersion = metadata.AdapterVersion
	registration.AdapterProtocolVersion = metadata.AdapterProtocolVersion
	registration.Capabilities.Adapter = &metadata.Adapter
	return nil
}

func initialInput(profile config.AgentProfile, work protocol.Work, providerAccess *protocol.ProviderAccess, controlPlaneURL string) ([]byte, error) {
	if providerAccess != nil && !profile.ProviderAccess {
		return nil, errors.New("agent profile does not allow provider access")
	}
	if profile.InputMode == config.InputModeGoal {
		return append([]byte(work.Goal), '\n'), nil
	}
	resolved, err := resolveProviderAccess(controlPlaneURL, providerAccess)
	if err != nil {
		return nil, err
	}
	if !profile.SupervisoryControl {
		return inputRecord(protocol.AgentInputRecordTaskInput, work.Goal, work.Input, resolved)
	}
	encoded, err := json.Marshal(protocol.AgentInputRecord{
		Type: protocol.AgentInputRecordTaskInput, Goal: work.Goal, Input: work.Input, ProviderAccess: resolved,
		Autonomy: &protocol.AutonomyPolicy{
			Mode:              "high",
			EscalationReasons: []string{"blocked", "consequential", "irreversible", "security", "business_policy", "expensive", "product_change"},
			RoutineDecisions:  "Proceed autonomously with implementation choices, tool selection, recoverable errors, and temporary uncertainty; do not request routine approvals.",
			ControlBoundary:   "Apply guidance, pause, and resume only at a safe boundary after the current atomic operation. Pause retains process and workspace and stops further autonomous progress until resume. Preserve the original goal.",
			Acknowledgement:   "After applying a control emit command_applied with its command_id, kind, and applied/rejected/failed outcome. Stdin receipt is not application. Consequential decisions require a concise waiting_for_input decision packet.",
		},
	})
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func resolveProviderAccess(controlPlaneURL string, access *protocol.ProviderAccess) (*protocol.ProviderAccess, error) {
	if access == nil {
		return nil, nil
	}
	if access.Path != "/api/v1/provider-actions" {
		return nil, errors.New("provider access path is invalid")
	}
	base, err := url.Parse(controlPlaneURL)
	if err != nil || base.Scheme == "" || base.Host == "" || base.User != nil {
		return nil, errors.New("control plane URL is invalid")
	}
	base.Path = strings.TrimRight(base.Path, "/")
	if base.Path == "" {
		base.Path = "/api"
	}
	base.Path += strings.TrimPrefix(access.Path, "/api")
	base.RawPath = ""
	resolved := *access
	resolved.Path = base.String()
	return &resolved, nil
}

func inputRecord(recordType protocol.AgentInputRecordType, goal string, input json.RawMessage, providerAccess *protocol.ProviderAccess) ([]byte, error) {
	encoded, err := json.Marshal(protocol.AgentInputRecord{Type: recordType, Goal: goal, Input: input, ProviderAccess: providerAccess})
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func canonicalInputDigest(payload json.RawMessage) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest), nil
}
