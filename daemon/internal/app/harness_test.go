package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/harness/codex"
	"github.com/wxxb789/symmetry/daemon/internal/harness/opencode"
	"github.com/wxxb789/symmetry/daemon/internal/harness/pi"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

func TestNewHarnessRegistryBindsConfiguredCodexNativeModel(t *testing.T) {
	profile := config.AgentProfile{Command: "codex-test", NativeModel: "gpt-6-astra", NativeModelProvider: "openai"}
	registry := newHarnessRegistry(profile)
	adapter, err := registry.Lookup(harness.KindCodex)
	if err != nil {
		t.Fatalf("Lookup() error = %v", err)
	}
	codexAdapter, ok := adapter.(*codex.Adapter)
	if !ok {
		t.Fatalf("Codex adapter type = %T, want *codex.Adapter", adapter)
	}
	configuredModel := reflect.ValueOf(codexAdapter).Elem().FieldByName("configuredNativeModel")
	if !configuredModel.IsValid() || configuredModel.String() != profile.NativeModel {
		t.Fatalf("configured native model = %q, want %q", configuredModel.String(), profile.NativeModel)
	}
	configuredProvider := reflect.ValueOf(codexAdapter).Elem().FieldByName("configuredNativeProvider")
	if !configuredProvider.IsValid() || configuredProvider.String() != profile.NativeModelProvider {
		t.Fatalf("configured native provider = %q, want %q", configuredProvider.String(), profile.NativeModelProvider)
	}
}

func TestNewHarnessRegistryBindsConcretePiAndOpenCodeAdapters(t *testing.T) {
	registry := newHarnessRegistry(config.AgentProfile{Command: "configured-agent"})
	piAdapter, err := registry.Lookup(harness.KindPi)
	if err != nil {
		t.Fatalf("Lookup(pi) error = %v", err)
	}
	if _, ok := piAdapter.(*pi.Adapter); !ok {
		t.Fatalf("Pi adapter type = %T, want *pi.Adapter", piAdapter)
	}
	openCodeAdapter, err := registry.Lookup(harness.KindOpenCode)
	if err != nil {
		t.Fatalf("Lookup(opencode) error = %v", err)
	}
	if _, ok := openCodeAdapter.(*opencode.Adapter); !ok {
		t.Fatalf("OpenCode adapter type = %T, want *opencode.Adapter", openCodeAdapter)
	}
}

func TestNativeRegistrationDoesNotAdvertiseUnverifiedProviderAccess(t *testing.T) {
	value := testConfig(t)
	value.Runtime.HarnessKind = config.RuntimeHarnessCodex
	value.Runtime.HarnessVersion = "0.153.4"
	value.Runtime.AdapterVersion = "symmetry-daemon:test"
	value.Runtime.AdapterProtocolVersion = 1
	profile := value.AgentProfiles[value.Runtime.AgentProfile]
	profile.InputMode = config.InputModeJSON
	profile.ProviderAccess = true
	value.AgentProfiles[value.Runtime.AgentProfile] = profile

	registration, _, err := buildRuntimeRegistration(value.Runtime, profile, verifiedCodexCapabilities())
	if err != nil {
		t.Fatalf("buildRuntimeRegistration() error = %v", err)
	}
	if registration.Capabilities.ProviderAccess {
		t.Fatalf("native registration advertised unverified provider access: %#v", registration.Capabilities)
	}
}

func TestLegacyConfigBuildsGenericRegistrationWithUnsupportedOperations(t *testing.T) {
	value := testConfig(t)
	daemon := &daemon{config: value}
	if err := daemon.ensureHarnessProbe(context.Background()); err != nil {
		t.Fatalf("ensureHarnessProbe() error = %v", err)
	}
	if daemon.config.Runtime.HarnessKind != config.RuntimeHarnessGeneric || daemon.config.Runtime.HarnessVersion != "legacy" ||
		daemon.config.Runtime.AdapterVersion != "legacy" || daemon.config.Runtime.AdapterProtocolVersion != 1 {
		t.Fatalf("runtime metadata = %+v, want generic legacy defaults", daemon.config.Runtime)
	}
	registration, metadata, err := buildRuntimeRegistration(daemon.config.Runtime, daemon.config.AgentProfiles[daemon.config.Runtime.AgentProfile], daemon.harnessCapabilities)
	if err != nil {
		t.Fatalf("buildRuntimeRegistration() error = %v", err)
	}
	if registration.HarnessKind != config.RuntimeHarnessGeneric || registration.HarnessVersion != "legacy" || registration.AdapterVersion != "legacy" || registration.AdapterProtocolVersion != 1 {
		t.Fatalf("registration metadata = %+v", registration)
	}
	if registration.Capabilities.Adapter == nil {
		t.Fatal("registration capabilities adapter = nil")
	}
	adapter := registration.Capabilities.Adapter
	if adapter.Kind != config.RuntimeHarnessGeneric || adapter.NativeVersion != "legacy" || adapter.ImplementationVersion != "legacy" || adapter.ProtocolVersion != 1 {
		t.Fatalf("adapter metadata = %+v", adapter)
	}
	if adapter.Operations.Start != true || adapter.Operations.Events != true || adapter.Operations.Cancel != true {
		t.Fatalf("adapter lifecycle operations = %+v", adapter.Operations)
	}
	if adapter.Operations.Resume || adapter.Operations.Handoff || adapter.Operations.ApprovalResponse || adapter.Operations.HardCostLimit ||
		adapter.Operations.Guidance != protocol.GuidanceUnsupported || adapter.Operations.Pause != protocol.PauseUnsupported || adapter.Operations.Usage != protocol.UsageUnknown {
		t.Fatalf("adapter unsupported operations = %+v", adapter.Operations)
	}
	if metadata.Adapter.Operations != adapter.Operations {
		t.Fatalf("metadata adapter operations = %+v, registration = %+v", metadata.Adapter.Operations, adapter.Operations)
	}
}

func TestStartupProbeRejectsUnknownCodexVersion(t *testing.T) {
	value := testConfig(t)
	value.Runtime.HarnessKind = config.RuntimeHarnessCodex
	value.Runtime.HarnessVersion = "0.154.0"
	value.Runtime.AdapterVersion = "symmetry-daemon:test"
	value.Runtime.AdapterProtocolVersion = 1
	registry := harness.NewRegistry()
	if err := registry.Register(harness.KindCodex, codex.NewAdapterWithRunner("codex", codexCommandFixtures{
		responses: map[string][]byte{"--version": []byte("codex-cli 0.154.0\n")},
	})); err != nil {
		t.Fatal(err)
	}
	daemon := &daemon{config: value, harnessRegistry: registry}
	err := daemon.ensureHarnessProbe(context.Background())
	if !errors.Is(err, harness.ErrUnsupportedVersion) {
		t.Fatalf("ensureHarnessProbe() error = %v, want ErrUnsupportedVersion", err)
	}
}

func TestStartupProbeRegistersUnverifiedCodexProjection(t *testing.T) {
	value := testConfig(t)
	value.Runtime.HarnessKind = config.RuntimeHarnessCodex
	value.Runtime.HarnessVersion = "0.153.4"
	value.Runtime.AdapterVersion = "symmetry-daemon:test"
	value.Runtime.AdapterProtocolVersion = 1
	registry := harness.NewRegistry()
	if err := registry.Register(harness.KindCodex, codex.NewAdapterWithRunner("codex", codexCommandFixtures{
		responses: map[string][]byte{
			"--version":         []byte("codex-cli 0.153.4\n"),
			"app-server --help": []byte("app-server\nstdio://\n"),
		},
	})); err != nil {
		t.Fatal(err)
	}
	daemon := &daemon{config: value, harnessRegistry: registry}
	err := daemon.ensureHarnessProbe(context.Background())
	if err != nil {
		t.Fatalf("ensureHarnessProbe() error = %v, want capability projection", err)
	}
	if daemon.harnessCapabilities.Verified || daemon.harnessCapabilities.Start || daemon.harnessCapabilities.Events {
		t.Fatalf("capabilities = %+v, want unverified unsupported projection", daemon.harnessCapabilities)
	}
	registration, _, err := buildRuntimeRegistration(daemon.config.Runtime, daemon.config.AgentProfiles[daemon.config.Runtime.AgentProfile], daemon.harnessCapabilities)
	if err != nil {
		t.Fatalf("buildRuntimeRegistration() error = %v", err)
	}
	if registration.Capabilities.Adapter == nil || registration.Capabilities.Adapter.Operations.Start || registration.Capabilities.Adapter.Operations.Events {
		t.Fatalf("registration adapter = %+v, want visible unsupported operations", registration.Capabilities.Adapter)
	}
}

func TestStartupProbeRegistersUnavailableNativeProjections(t *testing.T) {
	tests := []struct {
		name        string
		configKind  string
		harnessKind harness.Kind
		adapter     harness.Adapter
	}{
		{name: "claude", configKind: config.RuntimeHarnessClaudeCode, harnessKind: harness.KindClaude, adapter: harness.NewClaudeAdapter()},
		{name: "pi", configKind: config.RuntimeHarnessPi, harnessKind: harness.KindPi, adapter: harness.NewPiAdapter()},
		{name: "opencode", configKind: config.RuntimeHarnessOpenCode, harnessKind: harness.KindOpenCode, adapter: harness.NewOpenCodeAdapter()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := testConfig(t)
			value.Runtime.HarnessKind = test.configKind
			value.Runtime.HarnessVersion = "configured-native-9"
			value.Runtime.AdapterVersion = "configured-adapter-9"
			value.Runtime.AdapterProtocolVersion = 1
			registry := harness.NewRegistry()
			if err := registry.Register(test.harnessKind, test.adapter); err != nil {
				t.Fatal(err)
			}
			daemon := &daemon{config: value, harnessRegistry: registry}
			if err := daemon.ensureHarnessProbe(context.Background()); err != nil {
				t.Fatalf("ensureHarnessProbe() error = %v, want unavailable projection", err)
			}
			if daemon.harnessCapabilities.Verified || daemon.harnessCapabilities.Start || daemon.harnessCapabilities.Events {
				t.Fatalf("capabilities = %+v, want explicit unavailable operations", daemon.harnessCapabilities)
			}
			registration, _, err := buildRuntimeRegistration(daemon.config.Runtime, daemon.config.AgentProfiles[daemon.config.Runtime.AgentProfile], daemon.harnessCapabilities)
			if err != nil {
				t.Fatalf("buildRuntimeRegistration() error = %v", err)
			}
			if registration.Capabilities.Adapter == nil || registration.Capabilities.Adapter.Operations.Start || registration.Capabilities.Adapter.Operations.Events {
				t.Fatalf("registration adapter = %+v, want explicit unsupported operations", registration.Capabilities.Adapter)
			}
			if registration.HarnessVersion != unavailableNativeMetadataVersion || registration.AdapterVersion != unavailableNativeMetadataVersion ||
				registration.Capabilities.Adapter.NativeVersion != unavailableNativeMetadataVersion || registration.Capabilities.Adapter.ImplementationVersion != unavailableNativeMetadataVersion {
				t.Fatalf("unavailable registration retained configured version pins: %+v", registration)
			}
		})
	}
}

func TestStartupProbeRejectsInvalidUnverifiedProjection(t *testing.T) {
	value := testConfig(t)
	value.Runtime.HarnessKind = config.RuntimeHarnessCodex
	value.Runtime.HarnessVersion = "0.153.4"
	value.Runtime.AdapterVersion = "symmetry-daemon:test"
	value.Runtime.AdapterProtocolVersion = 1
	capabilities := verifiedCodexCapabilities()
	capabilities.ProtocolVersion = 0
	registry := harness.NewRegistry()
	if err := registry.Register(harness.KindCodex, &fakeNativeGoalAdapter{capabilities: capabilities, probeErr: harness.ErrNativeUnverified}); err != nil {
		t.Fatal(err)
	}
	daemon := &daemon{config: value, harnessRegistry: registry}
	err := daemon.ensureHarnessProbe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "protocol version must be positive") {
		t.Fatalf("ensureHarnessProbe() error = %v, want invalid capability projection", err)
	}
}

func TestStartupProbeRejectsUnexpectedJoinedProbeError(t *testing.T) {
	for _, test := range []struct {
		name     string
		harness  string
		kind     harness.Kind
		probeErr error
	}{
		{
			name:     "unverified and deadline",
			harness:  config.RuntimeHarnessCodex,
			kind:     harness.KindCodex,
			probeErr: errors.Join(harness.ErrNativeUnverified, context.DeadlineExceeded),
		},
		{
			name:     "unavailable and deadline",
			harness:  config.RuntimeHarnessPi,
			kind:     harness.KindPi,
			probeErr: errors.Join(harness.ErrHarnessUnavailable, context.DeadlineExceeded),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := testConfig(t)
			value.Runtime.HarnessKind = test.harness
			value.Runtime.HarnessVersion = "configured-native-9"
			value.Runtime.AdapterVersion = "configured-adapter-9"
			value.Runtime.AdapterProtocolVersion = 1
			registry := harness.NewRegistry()
			capabilities := harness.UnsupportedCapabilities(test.kind, "probe is unavailable")
			if err := registry.Register(test.kind, &fakeNativeGoalAdapter{capabilities: capabilities, probeErr: test.probeErr}); err != nil {
				t.Fatal(err)
			}
			daemon := &daemon{config: value, harnessRegistry: registry}
			err := daemon.ensureHarnessProbe(context.Background())
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("ensureHarnessProbe() error = %v, want unexpected joined error rejection", err)
			}
		})
	}
}

func TestStartupProbeRejectsMismatchedKnownNativeVersion(t *testing.T) {
	value := testConfig(t)
	value.Runtime.HarnessKind = config.RuntimeHarnessCodex
	value.Runtime.HarnessVersion = "0.153.5"
	value.Runtime.AdapterVersion = "symmetry-daemon:test"
	value.Runtime.AdapterProtocolVersion = 1
	registry := harness.NewRegistry()
	if err := registry.Register(harness.KindCodex, codex.NewAdapterWithRunner("codex", codexCommandFixtures{
		responses: map[string][]byte{
			"--version":         []byte("codex-cli 0.153.4\n"),
			"app-server --help": []byte("app-server\nstdio://\n"),
		},
	})); err != nil {
		t.Fatal(err)
	}
	daemon := &daemon{config: value, harnessRegistry: registry}
	err := daemon.ensureHarnessProbe(context.Background())
	if err == nil || !strings.Contains(err.Error(), "runtime.harness_version") {
		t.Fatalf("ensureHarnessProbe() error = %v, want known native version mismatch", err)
	}
}

func TestRegistrationWireIncludesAdapterObject(t *testing.T) {
	runtime := config.Runtime{
		RuntimeKey: "default", Name: "runtime", Capacity: 1, AgentProfile: "default", Workspace: "primary",
		HarnessKind: config.RuntimeHarnessGeneric, HarnessVersion: "legacy", AdapterVersion: "legacy", AdapterProtocolVersion: 1,
	}
	capabilities, err := harness.NewGenericAdapter().Probe(context.Background())
	if err != nil {
		t.Fatalf("generic Probe() error = %v", err)
	}
	profile := config.AgentProfile{InputMode: config.InputModeJSON}
	registration, _, err := buildRuntimeRegistration(runtime, profile, capabilities)
	if err != nil {
		t.Fatalf("buildRuntimeRegistration() error = %v", err)
	}
	encoded, err := json.Marshal(registration)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(encoded, &object); err != nil {
		t.Fatal(err)
	}
	if _, ok := object["harness_kind"]; !ok {
		t.Fatalf("registration JSON = %s, missing harness_kind", encoded)
	}
	capabilitiesObject, ok := object["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("registration capabilities = %#v", object["capabilities"])
	}
	if _, ok := capabilitiesObject["adapter"]; !ok {
		t.Fatalf("registration capabilities JSON = %#v, missing adapter", capabilitiesObject)
	}
}

func TestBuildRuntimeRegistrationRejectsMismatchedAdapterKind(t *testing.T) {
	runtime := config.Runtime{
		RuntimeKey: "claude", Name: "Claude", Capacity: 1, AgentProfile: "default", Workspace: "primary",
		HarnessKind: config.RuntimeHarnessClaudeCode, HarnessVersion: "2.1.259", AdapterVersion: "symmetry-daemon:test", AdapterProtocolVersion: 1,
	}
	capabilities := harness.UnsupportedCapabilities(harness.KindCodex, "test")
	_, _, err := buildRuntimeRegistration(runtime, config.AgentProfile{InputMode: config.InputModeJSON}, capabilities)
	if err == nil || !strings.Contains(err.Error(), "does not match probed adapter kind") {
		t.Fatalf("buildRuntimeRegistration() error = %v, want adapter kind mismatch", err)
	}
}

func TestNativeRegistrationCarriesConfiguredRepositoryResourceID(t *testing.T) {
	repositoryResourceID := "00000000-0000-4000-8000-000000000001"
	runtime := config.Runtime{
		RuntimeKey:             "codex",
		Name:                   "Codex",
		Capacity:               1,
		AgentProfile:           "default",
		Workspace:              "primary",
		RepositoryResourceID:   repositoryResourceID,
		HarnessKind:            config.RuntimeHarnessCodex,
		HarnessVersion:         "0.153.4",
		AdapterVersion:         "symmetry-daemon:test",
		AdapterProtocolVersion: 1,
	}
	capabilities := harness.UnsupportedCapabilities(harness.KindCodex, "test")
	profile := config.AgentProfile{InputMode: config.InputModeJSON}

	registration, _, err := buildRuntimeRegistration(runtime, profile, capabilities)
	if err != nil {
		t.Fatalf("buildRuntimeRegistration() error = %v", err)
	}
	if registration.RepositoryResourceID == nil || *registration.RepositoryResourceID != repositoryResourceID {
		t.Fatalf("registration repository resource ID = %v, want %q", registration.RepositoryResourceID, repositoryResourceID)
	}

	encoded, err := json.Marshal(registration)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"repository_resource_id":"`+repositoryResourceID+`"`) {
		t.Fatalf("registration JSON = %s, missing repository resource ID", encoded)
	}
}

func TestAdmissionInputStrictParsingPreservesLegacyInputs(t *testing.T) {
	admission, present, err := parseAdmissionInput(json.RawMessage(`{"mode":"legacy"}`))
	if err != nil || present || admission.AdmissionID != "" {
		t.Fatalf("legacy parse = admission %+v, present %v, error %v", admission, present, err)
	}
	admission, present, err = parseAdmissionInput(validAdmissionInput())
	if err != nil || !present || admission.SchemaVersion != protocol.AdmissionSchemaVersion {
		t.Fatalf("admission parse = %+v, present %v, error %v", admission, present, err)
	}
	mutated := strings.Replace(string(validAdmissionInput()), `,"limits":`, `,"unexpected":true,"limits":`, 1)
	_, present, err = parseAdmissionInput(json.RawMessage(mutated))
	if !present || !errors.Is(err, errInvalidAdmission) {
		t.Fatalf("unknown admission field = present %v, error %v", present, err)
	}
	nested := json.RawMessage(`{"goal_admission":` + string(validAdmissionInput()) + `}`)
	admission, present, err = parseAdmissionInput(nested)
	if err != nil || !present || admission.SchemaVersion != protocol.AdmissionSchemaVersion {
		t.Fatalf("nested admission parse = %+v, present %v, error %v", admission, present, err)
	}
	_, present, err = parseAdmissionInput(json.RawMessage(`{"goal_admission":{"goal":"old marker"}}`))
	if present || err != nil {
		t.Fatalf("legacy nested value = present %v, error %v", present, err)
	}
	_, present, err = parseAdmissionInput(json.RawMessage(`{"schema_version":"legacy.v1","goal_admission":null}`))
	if present || err != nil {
		t.Fatalf("legacy schema version = present %v, error %v", present, err)
	}
}

func TestAdmissionLaunchRequiresFreshSingleTurnAndVerifiedOperations(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = present %v, error %v", present, err)
	}
	capabilities, err := harness.NewGenericAdapter().Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := admissionLaunchFailure(admission, capabilities, nil); err != nil {
		t.Fatalf("admission launch error = %v", err)
	}
	withoutCancel := capabilities
	withoutCancel.Cancel = false
	if err := admissionLaunchFailure(admission, withoutCancel, nil); err == nil || !errors.Is(err, harness.ErrUnsupportedCapability) {
		t.Fatalf("cancel-capability admission error = %v, want cancel capability rejection", err)
	}
	admission.SessionMode = protocol.SessionModeResume
	if err := admissionLaunchFailure(admission, capabilities, nil); err == nil || !strings.Contains(err.Error(), string(protocol.TaskResultReasonResumeRejected)) {
		t.Fatalf("resume admission error = %v, want typed resume rejection", err)
	}
	admission.SessionMode = protocol.SessionModeHandoff
	if err := admissionLaunchFailure(admission, capabilities, nil); err == nil || !strings.Contains(err.Error(), string(protocol.TaskResultReasonHandoffUnsupported)) {
		t.Fatalf("handoff admission error = %v, want typed handoff unsupported outcome", err)
	}
	admission.SessionMode = protocol.SessionModeFresh
	admission.Limits.MaxTurns = 2
	if err := admissionLaunchFailure(admission, capabilities, nil); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("multi-turn admission error = %v", err)
	}
	admission.Limits.MaxTurns = 1
	cap := "250000"
	admission.Limits.MaxCostMicrousd = &cap
	if err := admissionLaunchFailure(admission, capabilities, nil); err == nil || !errors.Is(err, harness.ErrUnsupportedCapability) {
		t.Fatalf("strict-cap admission error = %v, want hard-cost capability rejection", err)
	}
}

func TestAdmissionAssignmentFailsBeforeGenericProcessLaunch(t *testing.T) {
	journal, starts := runAdmissionAssignment(t, validAdmissionInput())
	if starts != 0 {
		t.Fatalf("generic process starts = %d, want 0", starts)
	}
	var failure map[string]string
	if err := json.Unmarshal(journal.PendingTransitions[0].Payload, &failure); err != nil {
		t.Fatal(err)
	}
	if failure["stage"] != "goal_admission" || !strings.Contains(failure["error"], "unsupported") {
		t.Fatalf("failure = %#v, want generic harness rejection", failure)
	}
}

func TestHandoffAdmissionWithoutVerifiedCapabilityFailsBeforeWorkspaceAndNativeSessionSideEffects(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	admission.SessionMode = protocol.SessionModeHandoff
	sourceRunID := "00000000-0000-4000-8000-000000000007"
	admission.HandoffSourceRunID = &sourceRunID

	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
	defer store.Close()
	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()

	if len(session.calls) != 0 {
		t.Fatalf("handoff invoked native session methods: %#v", session.calls)
	}
	if len(controlClient.calls) != 0 {
		t.Fatalf("handoff sent control session calls: %#v", controlClient.calls)
	}
	sessions, err := store.ListGoalSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("handoff created local Goal session journal: %#v", sessions)
	}

	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "failed" || len(journal.PendingTransitions) != 1 {
		t.Fatalf("handoff journal = %#v, want one failed terminal transition", journal)
	}
	var failure map[string]string
	if err := json.Unmarshal(journal.PendingTransitions[0].Payload, &failure); err != nil {
		t.Fatal(err)
	}
	if failure["reason"] != string(protocol.TaskResultReasonHandoffUnsupported) {
		t.Fatalf("handoff failure payload = %s, want canonical unsupported reason", journal.PendingTransitions[0].Payload)
	}
}

func TestVerifiedPiAndOpenCodeHandoffAdmissionsStartNewNativeSessions(t *testing.T) {
	for _, test := range []struct {
		name       string
		kind       harness.Kind
		configKind string
	}{
		{name: "pi", kind: harness.KindPi, configKind: config.RuntimeHarnessPi},
		{name: "opencode", kind: harness.KindOpenCode, configKind: config.RuntimeHarnessOpenCode},
	} {
		t.Run(test.name, func(t *testing.T) {
			admission, present, err := parseAdmissionInput(validAdmissionInput())
			if err != nil || !present {
				t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
			}
			sourceRunID := "00000000-0000-4000-8000-000000000007"
			admission.SessionMode = protocol.SessionModeHandoff
			admission.HandoffSourceRunID = &sourceRunID
			capabilities := verifiedNativeCapabilities(test.kind)
			capabilities.Handoff = true
			result := validNativeTaskResult(t, admission)
			app, store, session, controlClient := nativeAdmissionDaemonForHarness(t, admission, result, nil, test.kind, test.configKind, capabilities)
			defer store.Close()

			app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
			app.workers.Wait()
			journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
			if err != nil {
				t.Fatal(err)
			}

			if got, want := session.calls, []string{"start", "details", "open", "start_turn", "wait_turn", "details", "close", "wait", "wait"}; !sameStrings(got, want) {
				t.Fatalf("native lifecycle = %#v, want %#v; journal=%#v", got, want, journal)
			}
			if session.request.Resume != nil {
				t.Fatalf("handoff passed a resume handle to a new session: %#v", session.request.Resume)
			}
			if session.request.LocalHandleID == "" || session.request.LocalHandleID == sourceRunID {
				t.Fatalf("handoff local handle = %q, want new local native handle", session.request.LocalHandleID)
			}
			if session.turnRequest.Goal != controlClient.work.Goal || len(session.turnRequest.Context) == 0 || !strings.Contains(string(session.turnRequest.Context), admission.ContextSnapshotID) {
				t.Fatalf("handoff turn request = %#v, want canonical Goal and context", session.turnRequest)
			}
			if workspace := app.workspace.(*fakeWorkspace); workspace.subject != admission.Subject {
				t.Fatalf("handoff workspace subject = %#v, want %#v", workspace.subject, admission.Subject)
			}
			sessions, err := store.ListGoalSessions()
			if err != nil || len(sessions) != 1 {
				t.Fatalf("Goal session journals = %#v, error = %v", sessions, err)
			}
			if sessions[0].SessionMode != state.GoalSessionModeHandoff {
				t.Fatalf("handoff session mode = %q, want %q", sessions[0].SessionMode, state.GoalSessionModeHandoff)
			}
			if sessions[0].HandoffSourceRunID != sourceRunID {
				t.Fatalf("handoff source Run = %q, want %q", sessions[0].HandoffSourceRunID, sourceRunID)
			}

			firstLifecycle := append([]string(nil), session.calls...)
			app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
			app.workers.Wait()
			if !sameStrings(session.calls, firstLifecycle) {
				t.Fatalf("handoff assignment replay started another native session: before=%#v after=%#v", firstLifecycle, session.calls)
			}
		})
	}
}

func TestHandoffAdmissionRejectsMismatchedConfiguredAdapterBeforeWorkspace(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	sourceRunID := "00000000-0000-4000-8000-000000000007"
	admission.SessionMode = protocol.SessionModeHandoff
	admission.HandoffSourceRunID = &sourceRunID
	capabilities := verifiedNativeCapabilities(harness.KindCodex)
	capabilities.Handoff = true
	app, store, session, controlClient := nativeAdmissionDaemonForHarness(t, admission, validNativeTaskResult(t, admission), nil, harness.KindCodex, config.RuntimeHarnessPi, capabilities)
	defer store.Close()

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()

	if len(session.calls) != 0 || app.workspace.(*fakeWorkspace).subject != (protocol.Subject{}) || len(controlClient.calls) != 0 {
		t.Fatalf("mismatched runtime adapter crossed a side-effect boundary: native=%#v workspace=%#v control=%#v", session.calls, app.workspace.(*fakeWorkspace).subject, controlClient.calls)
	}
}

func TestObserveAdmissionFailsBeforeWorkspaceAndNativeSessionSideEffects(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	admission.Purpose = protocol.AdmissionPurposeObserve

	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
	defer store.Close()
	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()

	if len(session.calls) != 0 || len(controlClient.calls) != 0 {
		t.Fatalf("observe rejection crossed native or control boundary: native=%#v control=%#v", session.calls, controlClient.calls)
	}
	sessions, err := store.ListGoalSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("observe created local Goal session journal: %#v", sessions)
	}
	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if journal.WorkspaceRecoveryRequired || journal.WorkspacePath != "" {
		t.Fatalf("observe workspace side effects = recovery %v, path %q; want none", journal.WorkspaceRecoveryRequired, journal.WorkspacePath)
	}
	var failure map[string]string
	if err := json.Unmarshal(journal.PendingTransitions[0].Payload, &failure); err != nil {
		t.Fatal(err)
	}
	if failure["reason"] != string(protocol.TaskResultReasonUnsupportedVersion) || !strings.Contains(failure["error"], "purpose") {
		t.Fatalf("observe rejection payload = %#v", failure)
	}
}

func TestResumeAdmissionPersistsCanonicalRejectionReason(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	sessionID := "00000000-0000-4000-8000-000000000099"
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &sessionID
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
	defer store.Close()
	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()
	if len(session.calls) != 0 || len(controlClient.calls) != 0 {
		t.Fatalf("resume rejection crossed native or control boundary: native=%#v control=%#v", session.calls, controlClient.calls)
	}
	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	var failure map[string]string
	if err := json.Unmarshal(journal.PendingTransitions[0].Payload, &failure); err != nil {
		t.Fatal(err)
	}
	if failure["reason"] != string(protocol.TaskResultReasonResumeRejected) {
		t.Fatalf("resume failure payload = %#v, want canonical reason", failure)
	}
}

func TestPiResumeAdmissionRequiresVerifiedResumeCapabilityBeforeSideEffects(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000090"
	resumeBindingID := "00000000-0000-4000-8000-000000000091"
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &controlSessionID
	capabilities := verifiedNativeCapabilities(harness.KindPi)
	capabilities.NativeVersion = pi.TestedVersion
	app, store, session, controlClient := nativeAdmissionDaemonForHarness(t, admission, validNativeTaskResult(t, admission), nil, harness.KindPi, config.RuntimeHarnessPi, capabilities)
	defer store.Close()
	controlClient.harnessSessionID = &controlSessionID
	controlClient.harnessBindingID = &resumeBindingID

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-resume", Generation: 2, Work: controlClient.work})
	app.workers.Wait()
	if len(session.calls) != 0 || len(controlClient.calls) != 0 || app.workspace.(*fakeWorkspace).prepareCalls != 0 || app.workspace.(*fakeWorkspace).recoverCalls != 0 {
		t.Fatalf("unverified pi resume crossed a side-effect boundary: native=%#v control=%#v workspace=%#v", session.calls, controlClient.calls, app.workspace)
	}
	run, err := store.LoadJournal(state.RunKey{RunID: "run-resume", Generation: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.PendingTransitions) != 1 || !strings.Contains(string(run.PendingTransitions[0].Payload), string(protocol.TaskResultReasonResumeRejected)) {
		t.Fatalf("unverified pi resume terminal = %#v", run)
	}
}

func TestPiAdmissionsRejectInvalidRPCProfileBeforeNativeLaunch(t *testing.T) {
	for _, test := range []struct {
		name string
		mode protocol.SessionMode
	}{
		{name: "fresh", mode: protocol.SessionModeFresh},
		{name: "resume", mode: protocol.SessionModeResume},
		{name: "handoff", mode: protocol.SessionModeHandoff},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, profileTest := range []struct {
				name string
				args []string
			}{
				{name: "session override", args: []string{"--session", "forbidden.jsonl"}},
				{name: "export command", args: []string{"--export", "retained.jsonl", "output.html"}},
				{name: "positional input", args: []string{"--provider", "openai", "unadmitted prompt"}},
				{name: "version as value", args: []string{"--model", "--version"}},
				{name: "unknown option", args: []string{"--extension-command"}},
			} {
				t.Run(profileTest.name, func(t *testing.T) {
					admission, present, err := parseAdmissionInput(validAdmissionInput())
					if err != nil || !present {
						t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
					}
					admission.SessionMode = test.mode
					if test.mode == protocol.SessionModeHandoff {
						sourceRunID := "00000000-0000-4000-8000-000000000099"
						admission.HandoffSourceRunID = &sourceRunID
					}
					if test.mode == protocol.SessionModeResume {
						sessionID := "00000000-0000-4000-8000-000000000090"
						admission.RequestedSessionID = &sessionID
					}
					capabilities := verifiedNativeCapabilities(harness.KindPi)
					capabilities.NativeVersion = pi.TestedVersion
					capabilities.Handoff = test.mode == protocol.SessionModeHandoff
					capabilities.Resume = test.mode == protocol.SessionModeResume
					app, store, session, controlClient := nativeAdmissionDaemonForHarness(t, admission, validNativeTaskResult(t, admission), nil, harness.KindPi, config.RuntimeHarnessPi, capabilities)
					defer store.Close()
					if test.mode == protocol.SessionModeResume {
						bindingID := "00000000-0000-4000-8000-000000000091"
						controlClient.harnessSessionID = admission.RequestedSessionID
						controlClient.harnessBindingID = &bindingID
					}
					profile := app.config.AgentProfiles[app.config.Runtime.AgentProfile]
					profile.Args = profileTest.args
					app.config.AgentProfiles[app.config.Runtime.AgentProfile] = profile

					app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
					app.workers.Wait()
					if len(session.calls) != 0 || len(controlClient.calls) != 0 || app.workspace.(*fakeWorkspace).prepareCalls != 0 || app.workspace.(*fakeWorkspace).recoverCalls != 0 {
						t.Fatalf("invalid pi argv crossed a native/control/workspace boundary: native=%#v control=%#v workspace=%#v", session.calls, controlClient.calls, app.workspace)
					}
					sessions, err := store.ListGoalSessions()
					if err != nil || len(sessions) != 0 {
						t.Fatalf("invalid pi argv persisted a Goal session: %#v, error=%v", sessions, err)
					}
					journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
					if err != nil || len(journal.PendingTransitions) != 1 {
						t.Fatalf("terminal transition count=%d, error=%v", len(journal.PendingTransitions), err)
					}
					if payload := journal.PendingTransitions[0].Payload; !strings.Contains(string(payload), "validate pi RPC invocation before native launch") {
						t.Fatalf("admission did not reach the Pi argv guard: %s", payload)
					}
				})
			}
		})
	}
}

func TestPiResumeAdmissionReusesRetainedNativeSessionAndWorkspace(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000090"
	resumeBindingID := "00000000-0000-4000-8000-000000000091"
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &controlSessionID
	capabilities := verifiedNativeCapabilities(harness.KindPi)
	capabilities.NativeVersion = pi.TestedVersion
	capabilities.Resume = true
	app, store, session, controlClient := nativeAdmissionDaemonForHarness(t, admission, validNativeTaskResult(t, admission), nil, harness.KindPi, config.RuntimeHarnessPi, capabilities)
	defer store.Close()

	nativeFile := filepath.Join(t.TempDir(), "retained-session.jsonl")
	if err := os.WriteFile(nativeFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := saveAvailablePiGoalSession(t, store, admission, controlSessionID, nativeFile, capabilities)
	session.handle = harness.NativeSessionHandle{ID: source.NativeSessionID, Filename: source.NativeSessionFilename}
	controlClient.harnessSessionID = &controlSessionID
	controlClient.harnessBindingID = &resumeBindingID
	controlClient.attachSessionID = controlSessionID

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-resume", Generation: 2, Work: controlClient.work})
	app.workers.Wait()

	if session.request.Resume == nil || *session.request.Resume != (harness.ResumeHandle{
		LocalHandleID: source.LocalHandleID, NativeSessionID: source.NativeSessionID, NativeSessionFilename: source.NativeSessionFilename,
		WorkspaceFingerprint: source.WorkspaceFingerprint, NativeVersion: source.HarnessVersion,
	}) {
		t.Fatalf("resume request = %#v, want retained native handle", session.request.Resume)
	}
	if session.request.LocalHandleID != source.LocalHandleID || session.request.Workspace != source.WorkspacePath {
		t.Fatalf("resume started a different local session or workspace: %#v", session.request)
	}
	workspaceService := app.workspace.(*fakeWorkspace)
	if workspaceService.prepareCalls != 0 || workspaceService.recoverCalls != 1 || workspaceService.recoveredRun != (workspace.RunRef{RunID: "run-source", Generation: 7}) || workspaceService.recoveredPath != source.WorkspacePath {
		t.Fatalf("resume workspace recovery = %#v", workspaceService)
	}
	sessions, err := store.ListGoalSessions()
	if err != nil || len(sessions) != 1 {
		t.Fatalf("retained Goal sessions = %#v, error = %v", sessions, err)
	}
	if sessions[0].Key() != source.Key() || sessions[0].RunID != "run-resume" || sessions[0].Generation != 2 ||
		sessions[0].BindingID != resumeBindingID || sessions[0].SessionMode != state.GoalSessionModeResume ||
		sessions[0].ControlSessionID != controlSessionID || sessions[0].NativeSessionID != source.NativeSessionID ||
		sessions[0].NativeSessionFilename != source.NativeSessionFilename {
		t.Fatalf("resumed Goal session journal = %#v", sessions[0])
	}
	calls := append([]string(nil), session.calls...)
	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-resume", Generation: 2, Work: controlClient.work})
	app.workers.Wait()
	if !sameStrings(session.calls, calls) {
		t.Fatalf("resume assignment replay started another native session: before=%#v after=%#v", calls, session.calls)
	}
}

func TestPiResumeAdmissionRejectsIncompatibleOrUnavailableRetainedSessionBeforeNativeStart(t *testing.T) {
	for _, test := range []struct {
		name                string
		sourceNativeVersion string
		mutate              func(*daemon, *state.GoalSessionJournal, string)
	}{
		{name: "machine", mutate: func(app *daemon, _ *state.GoalSessionJournal, _ string) { app.machineID = "machine-2" }},
		{name: "native version", sourceNativeVersion: "0.0.0", mutate: func(_ *daemon, _ *state.GoalSessionJournal, _ string) {}},
		{name: "missing retained file", mutate: func(_ *daemon, _ *state.GoalSessionJournal, file string) { _ = os.Remove(file) }},
		{name: "workspace fingerprint", mutate: func(_ *daemon, _ *state.GoalSessionJournal, _ string) {
			workspaceFingerprint = func(context.Context, workspace.Prepared) (string, error) {
				return "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", nil
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			admission, present, err := parseAdmissionInput(validAdmissionInput())
			if err != nil || !present {
				t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
			}
			controlSessionID := "00000000-0000-4000-8000-000000000090"
			resumeBindingID := "00000000-0000-4000-8000-000000000091"
			admission.SessionMode = protocol.SessionModeResume
			admission.RequestedSessionID = &controlSessionID
			capabilities := verifiedNativeCapabilities(harness.KindPi)
			capabilities.NativeVersion = pi.TestedVersion
			capabilities.Resume = true
			app, store, session, controlClient := nativeAdmissionDaemonForHarness(t, admission, validNativeTaskResult(t, admission), nil, harness.KindPi, config.RuntimeHarnessPi, capabilities)
			defer store.Close()
			nativeFile := filepath.Join(t.TempDir(), "retained-session.jsonl")
			if err := os.WriteFile(nativeFile, []byte("{}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			sourceCapabilities := capabilities
			if test.sourceNativeVersion != "" {
				sourceCapabilities.NativeVersion = test.sourceNativeVersion
			}
			source := saveAvailablePiGoalSession(t, store, admission, controlSessionID, nativeFile, sourceCapabilities)
			controlClient.harnessSessionID = &controlSessionID
			controlClient.harnessBindingID = &resumeBindingID
			controlClient.attachSessionID = controlSessionID
			test.mutate(app, &source, nativeFile)

			app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-resume", Generation: 2, Work: controlClient.work})
			app.workers.Wait()
			if len(session.calls) != 0 || len(controlClient.calls) != 0 {
				t.Fatalf("rejected resume crossed native/control start boundary: native=%#v control=%#v", session.calls, controlClient.calls)
			}
			run, err := store.LoadJournal(state.RunKey{RunID: "run-resume", Generation: 2})
			if err != nil {
				t.Fatal(err)
			}
			if len(run.PendingTransitions) != 1 || !strings.Contains(string(run.PendingTransitions[0].Payload), string(protocol.TaskResultReasonResumeRejected)) {
				t.Fatalf("rejected resume terminal = %#v", run)
			}
		})
	}
}

func TestRecoveryCompensatesReboundPiSessionBeforeNativeStart(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000090"
	resumeBindingID := "00000000-0000-4000-8000-000000000091"
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &controlSessionID
	capabilities := verifiedNativeCapabilities(harness.KindPi)
	capabilities.NativeVersion = pi.TestedVersion
	capabilities.Resume = true
	store, key := claimedStore(t)
	defer store.Close()
	nativeFile := filepath.Join(t.TempDir(), "retained-session.jsonl")
	if err := os.WriteFile(nativeFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := saveAvailablePiGoalSession(t, store, admission, controlSessionID, nativeFile, capabilities)
	if _, err := store.QueueGoalSessionAttach(key, state.GoalSessionAttachDelivery{
		GoalID: admission.GoalID, LocalHandleID: source.LocalHandleID, BindingID: resumeBindingID,
		HarnessKind: string(harness.KindPi), HarnessVersion: capabilities.NativeVersion, AdapterVersion: capabilities.ImplementationVersion,
		WorkspaceFingerprint: source.WorkspaceFingerprint, Workspace: "local", RepositoryResourceID: &admission.Subject.ResourceID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RebindGoalSessionForResume(source.Key(), state.GoalSessionResumeRebind{
		RunID: key.RunID, Generation: key.Generation, TaskID: "task-1", AdmissionID: admission.AdmissionID, BindingID: resumeBindingID,
		ExactStopCertificate: *source.StopCertificate,
		Compatibility: state.GoalSessionCompatibility{
			MachineID: "machine-1", RuntimeID: "runtime-1", RuntimeEpoch: 1, HarnessKind: string(harness.KindPi),
			HarnessVersion: capabilities.NativeVersion, AdapterVersion: capabilities.ImplementationVersion, AdapterProtocolVersion: capabilities.ProtocolVersion,
			WorkspaceFingerprint: source.WorkspaceFingerprint, RepositoryResourceID: admission.Subject.ResourceID,
		},
	}); err != nil {
		t.Fatal(err)
	}
	client := &nativeAdmissionControl{fakeControl: &fakeControl{}, admission: admission, attachSessionID: controlSessionID}
	app := &daemon{
		config: testConfig(t), store: store, control: client, workspace: &fakeWorkspace{}, options: options{newID: ids(), clock: time.Now},
		runtimeID: "runtime-1", runtimeEpoch: 1, running: make(map[state.RunKey]*runningRun), slots: make(chan struct{}, 1),
	}
	if err := app.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got, want := client.calls, []string{"attach"}; !sameStrings(got, want) {
		t.Fatalf("pre-start resume recovery control calls = %#v, want %#v", got, want)
	}
	recovered, err := store.LoadGoalSession(source.Key())
	if err != nil {
		t.Fatal(err)
	}
	if recovered.SessionState != state.GoalSessionStateUnavailable || recovered.ControlSessionID != controlSessionID || recovered.BindingID != resumeBindingID || recovered.NativeSessionID != source.NativeSessionID || recovered.StopCertificate != nil {
		t.Fatalf("pre-start resume recovery session = %#v", recovered)
	}
	run, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.PendingGoalDeliveries) != 1 || run.PendingGoalDeliveries[0].Kind != state.GoalDeliverySessionStopped || len(run.PendingTransitions) != 1 || !strings.Contains(string(run.PendingTransitions[0].Payload), string(protocol.TaskResultReasonUnknownOutcome)) {
		t.Fatalf("pre-start resume recovery run = %#v", run)
	}
}

func TestRecoveryCompletesPreStartPiCompensationAfterSecondCrash(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000090"
	resumeBindingID := "00000000-0000-4000-8000-000000000091"
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &controlSessionID
	capabilities := verifiedNativeCapabilities(harness.KindPi)
	capabilities.NativeVersion = pi.TestedVersion
	capabilities.Resume = true
	store, key := claimedStore(t)
	defer store.Close()
	nativeFile := filepath.Join(t.TempDir(), "retained-session.jsonl")
	if err := os.WriteFile(nativeFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := saveAvailablePiGoalSession(t, store, admission, controlSessionID, nativeFile, capabilities)
	if _, err := store.QueueGoalSessionAttach(key, state.GoalSessionAttachDelivery{
		GoalID: admission.GoalID, LocalHandleID: source.LocalHandleID, BindingID: resumeBindingID,
		HarnessKind: string(harness.KindPi), HarnessVersion: capabilities.NativeVersion, AdapterVersion: capabilities.ImplementationVersion,
		WorkspaceFingerprint: source.WorkspaceFingerprint, Workspace: "local", RepositoryResourceID: &admission.Subject.ResourceID,
	}); err != nil {
		t.Fatal(err)
	}
	rebindAvailablePiGoalSession(t, store, source, key, admission, resumeBindingID, capabilities)
	if _, err := store.MarkGoalSessionAttachDeliveryReady(key, source.LocalHandleID); err != nil {
		t.Fatal(err)
	}
	client := &nativeAdmissionControl{fakeControl: &fakeControl{}, admission: admission, attachSessionID: controlSessionID}
	app := &daemon{config: testConfig(t), store: store, control: client, workspace: &fakeWorkspace{}, options: options{newID: ids(), clock: time.Now}, runtimeID: "runtime-1", runtimeEpoch: 1, running: make(map[state.RunKey]*runningRun), slots: make(chan struct{}, 1)}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	// First recovery has made the delivery ready and Control has accepted it,
	// but crashes before closing ResumeStartPending or recording the stop.
	if _, receipt, err := app.deliverGoalDelivery(context.Background(), journal, state.GoalDeliverySessionAttach, source.LocalHandleID, nil); err != nil || receipt == nil {
		t.Fatalf("first recovery attach = receipt:%#v error:%v", receipt, err)
	}
	halfway, err := store.LoadGoalSession(source.Key())
	if err != nil || !halfway.ResumeStartPending || !halfway.HasVerifiedControlAttachment() || halfway.SessionState != state.GoalSessionStateBusy {
		t.Fatalf("first recovery durable phase = %#v, error=%v", halfway, err)
	}
	if err := app.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	completed, err := store.LoadGoalSession(source.Key())
	if err != nil {
		t.Fatal(err)
	}
	if completed.ResumeStartPending || completed.SessionState != state.GoalSessionStateUnavailable || completed.StopCertificate != nil || completed.NativeSessionID != source.NativeSessionID {
		t.Fatalf("second recovery did not complete pre-start compensation: %#v", completed)
	}
	updated, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.PendingGoalDeliveries) != 1 || updated.PendingGoalDeliveries[0].Kind != state.GoalDeliverySessionStopped || len(updated.PendingTransitions) != 1 {
		t.Fatalf("second recovery did not queue exact stop and terminal: %#v", updated)
	}
}

func TestRecoveryPreservesPiResumeRejectedReasonAfterRestart(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000090"
	resumeBindingID := "00000000-0000-4000-8000-000000000091"
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &controlSessionID
	capabilities := verifiedNativeCapabilities(harness.KindPi)
	capabilities.NativeVersion = pi.TestedVersion
	capabilities.Resume = true

	directory := t.TempDir()
	store, key := claimedStoreAt(t, directory)
	nativeFile := filepath.Join(t.TempDir(), "retained-session.jsonl")
	if err := os.WriteFile(nativeFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := saveAvailablePiGoalSession(t, store, admission, controlSessionID, nativeFile, capabilities)
	rebound := rebindAvailablePiGoalSession(t, store, source, key, admission, resumeBindingID, capabilities)
	if _, err := store.MarkGoalSessionResumeRejected(rebound.Key()); err != nil {
		t.Fatalf("MarkGoalSessionResumeRejected() error = %v", err)
	}
	beforeRestart, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeRestart.PendingTransitions) != 0 {
		t.Fatalf("resume rejection wrote terminal transition before restart: %#v", beforeRestart.PendingTransitions)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = state.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	app := &daemon{
		config: testConfig(t), store: store, control: &fakeControl{}, workspace: &fakeWorkspace{},
		options: options{newID: ids(), clock: time.Now}, machineID: "machine-1", runtimeID: "runtime-1", runtimeEpoch: 1,
		running: make(map[state.RunKey]*runningRun), slots: make(chan struct{}, 1),
	}
	if err := app.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatalf("recoverUnclosedGoalSessions() error = %v", err)
	}

	recovered, err := store.LoadGoalSession(rebound.Key())
	if err != nil {
		t.Fatal(err)
	}
	if !recovered.HasKnownRetainedResumeNativeStop() || recovered.ResumeTerminalReason != string(protocol.TaskResultReasonResumeRejected) {
		t.Fatalf("recovered retained resume state = %#v", recovered)
	}
	journ, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journ.TerminalState != "failed" || len(journ.PendingTransitions) != 1 {
		t.Fatalf("recovered terminal journal = %#v, want one failed transition", journ)
	}
	var payload map[string]string
	if err := json.Unmarshal(journ.PendingTransitions[0].Payload, &payload); err != nil {
		t.Fatalf("decode recovered terminal payload: %v", err)
	}
	if payload["reason"] != string(protocol.TaskResultReasonResumeRejected) {
		t.Fatalf("recovered terminal reason = %#v, want %q", payload, protocol.TaskResultReasonResumeRejected)
	}
	if payload["reason"] == string(protocol.TaskResultReasonUnknownOutcome) {
		t.Fatalf("recovered resume rejection downgraded to unknown_outcome: %#v", payload)
	}
}

func TestRecoveryDeliversReadyReboundPiAttachmentBeforeStoppingPersistedProcess(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000090"
	resumeBindingID := "00000000-0000-4000-8000-000000000091"
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &controlSessionID
	capabilities := verifiedNativeCapabilities(harness.KindPi)
	capabilities.NativeVersion = pi.TestedVersion
	capabilities.Resume = true
	store, key := claimedStore(t)
	defer store.Close()
	nativeFile := filepath.Join(t.TempDir(), "retained-session.jsonl")
	if err := os.WriteFile(nativeFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := saveAvailablePiGoalSession(t, store, admission, controlSessionID, nativeFile, capabilities)
	if _, err := store.QueueGoalSessionAttach(key, state.GoalSessionAttachDelivery{
		GoalID: admission.GoalID, LocalHandleID: source.LocalHandleID, BindingID: resumeBindingID,
		HarnessKind: string(harness.KindPi), HarnessVersion: capabilities.NativeVersion, AdapterVersion: capabilities.ImplementationVersion,
		WorkspaceFingerprint: source.WorkspaceFingerprint, Workspace: "local", RepositoryResourceID: &admission.Subject.ResourceID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RebindGoalSessionForResume(source.Key(), state.GoalSessionResumeRebind{
		RunID: key.RunID, Generation: key.Generation, TaskID: "task-1", AdmissionID: admission.AdmissionID, BindingID: resumeBindingID,
		ExactStopCertificate: *source.StopCertificate,
		Compatibility: state.GoalSessionCompatibility{
			MachineID: "machine-1", RuntimeID: "runtime-1", RuntimeEpoch: 1, HarnessKind: string(harness.KindPi),
			HarnessVersion: capabilities.NativeVersion, AdapterVersion: capabilities.ImplementationVersion, AdapterProtocolVersion: capabilities.ProtocolVersion,
			WorkspaceFingerprint: source.WorkspaceFingerprint, RepositoryResourceID: admission.Subject.ResourceID,
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionAttachDeliveryReady(key, source.LocalHandleID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionResumeStartAttempted(source.Key()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetProcessDetails(key, 77, "pi:77", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	terminated := 0
	client := &nativeAdmissionControl{fakeControl: &fakeControl{}, admission: admission, attachSessionID: controlSessionID}
	app := &daemon{
		config: testConfig(t), store: store, control: client, workspace: &fakeWorkspace{},
		options: options{newID: ids(), clock: time.Now, terminatePersist: func(pid int, identity string) error {
			terminated++
			if pid != 77 || identity != "pi:77" {
				t.Fatalf("terminated persisted pi process = %d %q", pid, identity)
			}
			return nil
		}}, runtimeID: "runtime-1", runtimeEpoch: 1, running: make(map[state.RunKey]*runningRun), slots: make(chan struct{}, 1),
	}
	if err := app.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if terminated != 1 || !sameStrings(client.calls, []string{"attach"}) {
		t.Fatalf("ready rebound recovery did not attach then terminate: terminated=%d calls=%#v", terminated, client.calls)
	}
	recovered, err := store.LoadGoalSession(source.Key())
	if err != nil {
		t.Fatal(err)
	}
	if recovered.SessionState != state.GoalSessionStateUnavailable || recovered.ControlAttachmentReceiptID == "" || recovered.BindingID != resumeBindingID || recovered.NativeSessionID != source.NativeSessionID {
		t.Fatalf("ready rebound recovery session = %#v", recovered)
	}
}

func TestRecoveryQuarantinesPreStartPiResumeAfterDefinitiveAttachRejection(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000090"
	resumeBindingID := "00000000-0000-4000-8000-000000000091"
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &controlSessionID
	capabilities := verifiedNativeCapabilities(harness.KindPi)
	capabilities.NativeVersion = pi.TestedVersion
	capabilities.Resume = true
	store, key := claimedStore(t)
	defer store.Close()
	nativeFile := filepath.Join(t.TempDir(), "retained-session.jsonl")
	if err := os.WriteFile(nativeFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := saveAvailablePiGoalSession(t, store, admission, controlSessionID, nativeFile, capabilities)
	if _, err := store.QueueGoalSessionAttach(key, state.GoalSessionAttachDelivery{
		GoalID: admission.GoalID, LocalHandleID: source.LocalHandleID, BindingID: resumeBindingID,
		HarnessKind: string(harness.KindPi), HarnessVersion: capabilities.NativeVersion, AdapterVersion: capabilities.ImplementationVersion,
		WorkspaceFingerprint: source.WorkspaceFingerprint, Workspace: "local", RepositoryResourceID: &admission.Subject.ResourceID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RebindGoalSessionForResume(source.Key(), state.GoalSessionResumeRebind{
		RunID: key.RunID, Generation: key.Generation, TaskID: "task-1", AdmissionID: admission.AdmissionID, BindingID: resumeBindingID,
		ExactStopCertificate: *source.StopCertificate,
		Compatibility: state.GoalSessionCompatibility{
			MachineID: "machine-1", RuntimeID: "runtime-1", RuntimeEpoch: 1, HarnessKind: string(harness.KindPi),
			HarnessVersion: capabilities.NativeVersion, AdapterVersion: capabilities.ImplementationVersion, AdapterProtocolVersion: capabilities.ProtocolVersion,
			WorkspaceFingerprint: source.WorkspaceFingerprint, RepositoryResourceID: admission.Subject.ResourceID,
		},
	}); err != nil {
		t.Fatal(err)
	}
	client := &nativeAdmissionControl{
		fakeControl: &fakeControl{}, admission: admission, attachSessionID: controlSessionID,
		attachErr: &control.APIError{StatusCode: 422, Code: control.InvalidRequest, Message: "attachment is no longer admissible"},
	}
	app := &daemon{config: testConfig(t), store: store, control: client, workspace: &fakeWorkspace{}, options: options{newID: ids(), clock: time.Now}, runtimeID: "runtime-1", runtimeEpoch: 1, running: make(map[state.RunKey]*runningRun), slots: make(chan struct{}, 1)}
	if err := app.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	recovered, err := store.LoadGoalSession(source.Key())
	if err != nil {
		t.Fatal(err)
	}
	if recovered.SessionState != state.GoalSessionStateClosed || recovered.NativeSessionID != "" || recovered.ControlSessionID != "" {
		t.Fatalf("definitively rejected pre-start resume remained live: %#v", recovered)
	}
	run, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.PendingGoalDeliveries) != 0 || len(run.RetiredGoalDeliveries) != 1 || run.RetiredGoalDeliveries[0].Delivery.Kind != state.GoalDeliverySessionAttach || len(run.PendingTransitions) != 1 {
		t.Fatalf("definitively rejected pre-start resume run = %#v", run)
	}
}

func TestPiResumeRejectedBeforeNativeStartReleasesExactNewBinding(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000090"
	resumeBindingID := "00000000-0000-4000-8000-000000000091"
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &controlSessionID
	capabilities := verifiedNativeCapabilities(harness.KindPi)
	capabilities.NativeVersion = pi.TestedVersion
	capabilities.Resume = true
	app, store, session, controlClient := nativeAdmissionDaemonForHarness(t, admission, validNativeTaskResult(t, admission), nil, harness.KindPi, config.RuntimeHarnessPi, capabilities)
	defer store.Close()
	nativeFile := filepath.Join(t.TempDir(), "retained-session.jsonl")
	if err := os.WriteFile(nativeFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := saveAvailablePiGoalSession(t, store, admission, controlSessionID, nativeFile, capabilities)
	session.startErr = pi.ErrResumeRejected
	controlClient.harnessSessionID = &controlSessionID
	controlClient.harnessBindingID = &resumeBindingID
	controlClient.attachSessionID = controlSessionID

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-resume", Generation: 2, Work: controlClient.work})
	app.workers.Wait()
	if len(controlClient.calls) != 0 {
		t.Fatalf("pre-process rejected resume called Control before persisting local stop: %#v", controlClient.calls)
	}
	resumed, err := store.LoadGoalSession(source.Key())
	if err != nil {
		t.Fatal(err)
	}
	if !resumed.HasKnownRetainedResumeNativeStop() || resumed.BindingID != resumeBindingID || resumed.ControlSessionID != controlSessionID || resumed.NativeSessionID != source.NativeSessionID {
		t.Fatalf("rejected resume did not preserve release identity: %#v", resumed)
	}
	run, err := store.LoadJournal(state.RunKey{RunID: "run-resume", Generation: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.PendingGoalDeliveries) != 1 || run.PendingGoalDeliveries[0].Kind != state.GoalDeliverySessionAttach || !run.PendingGoalDeliveries[0].Ready || len(run.PendingTransitions) != 1 || !strings.Contains(string(run.PendingTransitions[0].Payload), string(protocol.TaskResultReasonResumeRejected)) {
		t.Fatalf("rejected resume run = %#v", run)
	}
}

func TestPiResumeUnknownStartRetainsReadyAttachmentBarrier(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000090"
	resumeBindingID := "00000000-0000-4000-8000-000000000091"
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &controlSessionID
	capabilities := verifiedNativeCapabilities(harness.KindPi)
	capabilities.NativeVersion = pi.TestedVersion
	capabilities.Resume = true
	app, store, session, controlClient := nativeAdmissionDaemonForHarness(t, admission, validNativeTaskResult(t, admission), nil, harness.KindPi, config.RuntimeHarnessPi, capabilities)
	defer store.Close()
	nativeFile := filepath.Join(t.TempDir(), "retained-session.jsonl")
	if err := os.WriteFile(nativeFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := saveAvailablePiGoalSession(t, store, admission, controlSessionID, nativeFile, capabilities)
	session.startErr = errors.New("pi transport disconnected after spawn")
	controlClient.harnessSessionID = &controlSessionID
	controlClient.harnessBindingID = &resumeBindingID
	controlClient.attachSessionID = controlSessionID

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-resume", Generation: 2, Work: controlClient.work})
	app.workers.Wait()
	resumed, err := store.LoadGoalSession(source.Key())
	if err != nil {
		t.Fatal(err)
	}
	if resumed.NeedsReconciliation() || resumed.SessionState != state.GoalSessionStateBusy || resumed.ControlSessionID != controlSessionID || resumed.NativeSessionID != source.NativeSessionID {
		t.Fatalf("unknown resume start discarded the retained barrier: %#v", resumed)
	}
	run, err := store.LoadJournal(state.RunKey{RunID: "run-resume", Generation: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.PendingGoalDeliveries) != 1 || run.PendingGoalDeliveries[0].Kind != state.GoalDeliverySessionAttach || !run.PendingGoalDeliveries[0].Ready || len(run.PendingTransitions) != 1 || !strings.Contains(string(run.PendingTransitions[0].Payload), string(protocol.TaskResultReasonUnknownOutcome)) {
		t.Fatalf("unknown resume start run = %#v", run)
	}
}

func TestPiResumeAttachTimeoutAfterKnownCloseRepairsThroughReadbackBarrier(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000090"
	resumeBindingID := "00000000-0000-4000-8000-000000000091"
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &controlSessionID
	capabilities := verifiedNativeCapabilities(harness.KindPi)
	capabilities.NativeVersion = pi.TestedVersion
	capabilities.Resume = true
	app, store, session, controlClient := nativeAdmissionDaemonForHarness(t, admission, validNativeTaskResult(t, admission), nil, harness.KindPi, config.RuntimeHarnessPi, capabilities)
	defer store.Close()
	nativeFile := filepath.Join(t.TempDir(), "retained-session.jsonl")
	if err := os.WriteFile(nativeFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := saveAvailablePiGoalSession(t, store, admission, controlSessionID, nativeFile, capabilities)
	session.handle = harness.NativeSessionHandle{ID: source.NativeSessionID, Filename: source.NativeSessionFilename}
	controlClient.harnessSessionID = &controlSessionID
	controlClient.harnessBindingID = &resumeBindingID
	controlClient.attachSessionID = controlSessionID
	controlClient.attachErr = transportError("attach response lost")

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-resume", Generation: 2, Work: controlClient.work})
	app.workers.Wait()
	halfBound, err := store.LoadGoalSession(source.Key())
	if err != nil {
		t.Fatal(err)
	}
	if halfBound.NeedsReconciliation() || halfBound.SessionState != state.GoalSessionStateUnavailable || halfBound.ControlAttachmentReceiptID != "" || halfBound.NativeSessionID != source.NativeSessionID {
		t.Fatalf("attach timeout discarded known stopped resume identity: %#v", halfBound)
	}
	run, err := store.LoadJournal(state.RunKey{RunID: "run-resume", Generation: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.PendingGoalDeliveries) != 1 || run.PendingGoalDeliveries[0].Kind != state.GoalDeliverySessionAttach || !run.PendingGoalDeliveries[0].Ready {
		t.Fatalf("attach timeout did not retain ready readback barrier: %#v", run)
	}
	controlClient.attachErr = nil
	updated, _, err := app.flushGoalDeliveries(context.Background(), run)
	if err != nil {
		t.Fatalf("repair retained pi attach barrier: %v", err)
	}
	repaired, err := store.LoadGoalSession(source.Key())
	if err != nil {
		t.Fatal(err)
	}
	if repaired.ControlAttachmentReceiptID == "" || repaired.SessionState != state.GoalSessionStateUnavailable || repaired.NativeSessionID != source.NativeSessionID || len(updated.PendingGoalDeliveries) != 1 || updated.PendingGoalDeliveries[0].Kind != state.GoalDeliverySessionStopped {
		t.Fatalf("repaired retained pi attachment = session:%#v journal:%#v", repaired, updated)
	}
}

func TestPiResumeDefinitiveAttachRejectionAfterKnownCloseQuarantinesSession(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000090"
	resumeBindingID := "00000000-0000-4000-8000-000000000091"
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &controlSessionID
	capabilities := verifiedNativeCapabilities(harness.KindPi)
	capabilities.NativeVersion = pi.TestedVersion
	capabilities.Resume = true
	app, store, session, controlClient := nativeAdmissionDaemonForHarness(t, admission, validNativeTaskResult(t, admission), nil, harness.KindPi, config.RuntimeHarnessPi, capabilities)
	defer store.Close()
	nativeFile := filepath.Join(t.TempDir(), "retained-session.jsonl")
	if err := os.WriteFile(nativeFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := saveAvailablePiGoalSession(t, store, admission, controlSessionID, nativeFile, capabilities)
	session.handle = harness.NativeSessionHandle{ID: source.NativeSessionID, Filename: source.NativeSessionFilename}
	controlClient.harnessSessionID = &controlSessionID
	controlClient.harnessBindingID = &resumeBindingID
	controlClient.attachSessionID = controlSessionID
	controlClient.attachErr = &control.APIError{StatusCode: 422, Code: control.InvalidRequest, Message: "attachment rejected"}

	key := state.RunKey{RunID: "run-resume", Generation: 2}
	app.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: controlClient.work})
	app.workers.Wait()
	knownStopped, err := store.LoadGoalSession(source.Key())
	if err != nil || !knownStopped.HasKnownRetainedResumeNativeStop() {
		t.Fatalf("post-start rejection did not persist known native stop: %#v, error=%v", knownStopped, err)
	}
	recoveredApp := &daemon{config: app.config, store: store, control: controlClient, workspace: &fakeWorkspace{}, options: options{newID: ids(), clock: time.Now}, runtimeID: "runtime-1", runtimeEpoch: 1, running: make(map[state.RunKey]*runningRun), slots: make(chan struct{}, 1)}
	if err := recoveredApp.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	quarantined, err := store.LoadGoalSession(source.Key())
	if err != nil {
		t.Fatal(err)
	}
	if quarantined.SessionState != state.GoalSessionStateClosed || quarantined.NativeSessionID != "" || quarantined.ControlSessionID != "" {
		t.Fatalf("post-start definitive rejection did not close retained session: %#v", quarantined)
	}
	run, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.RetiredGoalDeliveries) != 1 || run.RetiredGoalDeliveries[0].Delivery.Kind != state.GoalDeliverySessionAttach {
		t.Fatalf("post-start definitive rejection outbox = %#v", run)
	}
}

func TestRecoveryRetiresRejectedReadyPiResumeBeforeClosingWithoutPredecessorStop(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000090"
	resumeBindingID := "00000000-0000-4000-8000-000000000091"
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &controlSessionID
	capabilities := verifiedNativeCapabilities(harness.KindPi)
	capabilities.NativeVersion = pi.TestedVersion
	capabilities.Resume = true
	store, key := claimedStore(t)
	defer store.Close()
	nativeFile := filepath.Join(t.TempDir(), "retained-session.jsonl")
	if err := os.WriteFile(nativeFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := saveAvailablePiGoalSession(t, store, admission, controlSessionID, nativeFile, capabilities)
	if _, err := store.QueueGoalSessionAttach(key, state.GoalSessionAttachDelivery{
		GoalID: admission.GoalID, LocalHandleID: source.LocalHandleID, BindingID: resumeBindingID,
		HarnessKind: string(harness.KindPi), HarnessVersion: capabilities.NativeVersion, AdapterVersion: capabilities.ImplementationVersion,
		WorkspaceFingerprint: source.WorkspaceFingerprint, Workspace: "local", RepositoryResourceID: &admission.Subject.ResourceID,
	}); err != nil {
		t.Fatal(err)
	}
	rebound := rebindAvailablePiGoalSession(t, store, source, key, admission, resumeBindingID, capabilities)
	if _, err := store.MarkGoalSessionAttachDeliveryReady(key, rebound.LocalHandleID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionResumeRejected(rebound.Key()); err != nil {
		t.Fatalf("MarkGoalSessionResumeRejected() error = %v", err)
	}
	before, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.PendingTransitions) != 0 {
		t.Fatalf("resume rejection unexpectedly had terminal outbox before recovery: %#v", before.PendingTransitions)
	}

	client := &nativeAdmissionControl{
		fakeControl: &fakeControl{}, admission: admission, attachSessionID: controlSessionID,
		attachErr: &control.APIError{StatusCode: 422, Code: control.InvalidRequest, Message: "attachment is no longer admissible"},
	}
	app := &daemon{
		config: testConfig(t), store: store, control: client, workspace: &fakeWorkspace{},
		options: options{newID: ids(), clock: time.Now}, runtimeID: "runtime-1", runtimeEpoch: 1,
		running: make(map[state.RunKey]*runningRun), slots: make(chan struct{}, 1),
	}
	if err := app.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatal(err)
	}

	closed, err := store.LoadGoalSession(rebound.Key())
	if err != nil {
		t.Fatal(err)
	}
	if closed.SessionState != state.GoalSessionStateClosed || closed.LaunchState != state.GoalSessionLaunchStateClosed || closed.NativeSessionID != "" || closed.ControlSessionID != "" {
		t.Fatalf("rejected ready resume was not closed after terminalization: %#v", closed)
	}
	run, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.RetiredGoalDeliveries) != 1 || run.RetiredGoalDeliveries[0].Delivery.Kind != state.GoalDeliverySessionAttach || len(run.PendingTransitions) != 1 || run.TerminalState != "failed" {
		t.Fatalf("rejected ready resume recovery journal = %#v", run)
	}
	var payload map[string]string
	if err := json.Unmarshal(run.PendingTransitions[0].Payload, &payload); err != nil {
		t.Fatalf("decode rejected resume terminal payload: %v", err)
	}
	if payload["reason"] != string(protocol.TaskResultReasonResumeRejected) {
		t.Fatalf("rejected resume terminal payload = %#v, want resume_rejected", payload)
	}
	for _, delivery := range run.PendingGoalDeliveries {
		if delivery.Kind == state.GoalDeliverySessionStopped {
			t.Fatalf("rejected resume queued predecessor/current stop delivery: %#v", run.PendingGoalDeliveries)
		}
	}
	for _, delivery := range run.DeliveredGoalDeliveries {
		if delivery.Kind == state.GoalDeliverySessionStopped {
			t.Fatalf("rejected resume delivered predecessor/current stop delivery: %#v", run.DeliveredGoalDeliveries)
		}
	}
	for _, retired := range run.RetiredGoalDeliveries {
		if retired.Delivery.Kind == state.GoalDeliverySessionStopped {
			t.Fatalf("rejected resume retired predecessor/current stop delivery: %#v", run.RetiredGoalDeliveries)
		}
	}
	if got, want := client.calls, []string{"attach"}; !sameStrings(got, want) {
		t.Fatalf("recovery Control calls = %#v, want one rejected attach", got)
	}

	if err := app.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	again, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.PendingTransitions) != 1 || len(again.RetiredGoalDeliveries) != 1 || len(client.calls) != 1 {
		t.Fatalf("second recovery was not idempotent: journal=%#v calls=%#v", again, client.calls)
	}
}

func TestHalfBoundPiResumeNeverQueuesStopForPredecessorBinding(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000090"
	resumeBindingID := "00000000-0000-4000-8000-000000000091"
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &controlSessionID
	capabilities := verifiedNativeCapabilities(harness.KindPi)
	capabilities.NativeVersion = pi.TestedVersion
	capabilities.Resume = true
	store, key := claimedStore(t)
	defer store.Close()
	nativeFile := filepath.Join(t.TempDir(), "retained-session.jsonl")
	if err := os.WriteFile(nativeFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := saveAvailablePiGoalSession(t, store, admission, controlSessionID, nativeFile, capabilities)
	rebindAvailablePiGoalSession(t, store, source, key, admission, resumeBindingID, capabilities)
	app := &daemon{store: store, options: options{clock: time.Now}}
	if err := app.recordStoppedGoalSession(key, source.Key()); err == nil {
		t.Fatal("recordStoppedGoalSession() succeeded for a half-bound resume")
	}
	run, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	for _, delivery := range run.PendingGoalDeliveries {
		if delivery.Kind == state.GoalDeliverySessionStopped {
			t.Fatalf("half-bound resume queued a stop delivery: %#v", delivery)
		}
	}
}

func TestLatePredecessorStopReceiptAcknowledgesAfterPiRebindWithoutReleasingNewBinding(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000090"
	resumeBindingID := "00000000-0000-4000-8000-000000000091"
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &controlSessionID
	capabilities := verifiedNativeCapabilities(harness.KindPi)
	capabilities.NativeVersion = pi.TestedVersion
	capabilities.Resume = true
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	predecessorKey := state.RunKey{RunID: "run-source", Generation: 7}
	resumeKey := state.RunKey{RunID: "run-resume", Generation: 2}
	saveClaimedGoalRun(t, store, predecessorKey)
	saveClaimedGoalRun(t, store, resumeKey)
	nativeFile := filepath.Join(t.TempDir(), "retained-session.jsonl")
	if err := os.WriteFile(nativeFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := saveAvailablePiGoalSession(t, store, admission, controlSessionID, nativeFile, capabilities)
	rebindAvailablePiGoalSession(t, store, source, resumeKey, admission, resumeBindingID, capabilities)
	oldJournal, err := store.LoadJournal(predecessorKey)
	if err != nil {
		t.Fatal(err)
	}
	client := &nativeAdmissionControl{fakeControl: &fakeControl{}, admission: admission, stopReceiptID: source.StopCertificate.ReceiptID}
	app := &daemon{store: store, control: client, options: options{clock: time.Now}}
	updated, _, err := app.deliverGoalDelivery(context.Background(), oldJournal, state.GoalDeliverySessionStopped, source.StopCertificate.BindingID, nil)
	if err != nil {
		t.Fatalf("deliver late predecessor stop receipt: %v", err)
	}
	if updated.HasPendingGoalDeliveries() || len(updated.DeliveredGoalDeliveries) != 1 {
		t.Fatalf("late predecessor stop delivery remained pending: %#v", updated)
	}
	resumed, err := store.LoadGoalSession(source.Key())
	if err != nil {
		t.Fatal(err)
	}
	if resumed.SessionState != state.GoalSessionStateBusy || resumed.RunID != resumeKey.RunID || resumed.Generation != resumeKey.Generation || resumed.BindingID != resumeBindingID || resumed.StopCertificate != nil {
		t.Fatalf("late predecessor stop released new pi binding: %#v", resumed)
	}
}

func TestRetainedPiWorkspaceOwnerBlocksSourceCleanupAfterRebind(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	controlSessionID := "00000000-0000-4000-8000-000000000090"
	resumeBindingID := "00000000-0000-4000-8000-000000000091"
	admission.SessionMode = protocol.SessionModeResume
	admission.RequestedSessionID = &controlSessionID
	capabilities := verifiedNativeCapabilities(harness.KindPi)
	capabilities.NativeVersion = pi.TestedVersion
	capabilities.Resume = true
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	resumeKey := state.RunKey{RunID: "run-resume", Generation: 2}
	saveClaimedGoalRun(t, store, resumeKey)
	nativeFile := filepath.Join(t.TempDir(), "retained-session.jsonl")
	if err := os.WriteFile(nativeFile, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := saveAvailablePiGoalSession(t, store, admission, controlSessionID, nativeFile, capabilities)
	rebindAvailablePiGoalSession(t, store, source, resumeKey, admission, resumeBindingID, capabilities)
	if _, err := store.RetainWorkspace(resumeKey); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CloseGoalSession(source.Key()); err != nil {
		t.Fatal(err)
	}
	app := &daemon{store: store}
	owned, err := app.retainedGoalSessionWorkspace(state.RunKey{RunID: "run-source", Generation: 7})
	if err != nil || !owned {
		t.Fatalf("source workspace retention after rebind = %t, error = %v", owned, err)
	}
	current, err := app.retainedGoalSessionWorkspace(resumeKey)
	if err != nil || current {
		t.Fatalf("current Run incorrectly owns original retained workspace = %t, error = %v", current, err)
	}
}

func TestInvalidAdmissionAssignmentFailsDurably(t *testing.T) {
	input := json.RawMessage(strings.Replace(string(validAdmissionInput()), `,"limits":`, `,"unexpected":true,"limits":`, 1))
	journal, starts := runAdmissionAssignment(t, input)
	if starts != 0 {
		t.Fatalf("generic process starts = %d, want 0", starts)
	}
	var failure map[string]string
	if err := json.Unmarshal(journal.PendingTransitions[0].Payload, &failure); err != nil {
		t.Fatal(err)
	}
	if failure["stage"] != "invalid_admission" || !strings.Contains(failure["error"], "invalid symmetry.admission.v1") {
		t.Fatalf("failure = %#v, want invalid_admission classification", failure)
	}
}

func TestNestedGoalAdmissionAssignmentFailsClosed(t *testing.T) {
	input := json.RawMessage(`{"goal_admission":` + string(validAdmissionInput()) + `}`)
	journal, starts := runAdmissionAssignment(t, input)
	if starts != 0 {
		t.Fatalf("generic process starts = %d, want 0", starts)
	}
	var failure map[string]string
	if err := json.Unmarshal(journal.PendingTransitions[0].Payload, &failure); err != nil {
		t.Fatal(err)
	}
	if failure["stage"] != "goal_admission" || !strings.Contains(failure["error"], "unsupported") {
		t.Fatalf("failure = %#v, want generic harness rejection", failure)
	}
}

func TestFreshCodexGoalAdmissionUsesDurableStagedNativeLifecycle(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	result := validNativeTaskResult(t, admission)
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, result, nil)
	defer store.Close()

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()

	if got, want := session.calls, []string{"start", "details", "open", "start_turn", "wait_turn", "details", "close", "wait", "wait"}; !sameStrings(got, want) {
		t.Fatalf("native lifecycle = %#v, want %#v", got, want)
	}
	if got, want := controlClient.calls, []string{"attach", "context"}; !sameStrings(got, want) {
		t.Fatalf("control lifecycle = %#v, want %#v", got, want)
	}
	if session.request.Invocation.InitialInput != nil || session.request.Invocation.CloseInputAfterInitial {
		t.Fatalf("native invocation inherited legacy stdin input: %#v", session.request.Invocation)
	}
	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "terminal_pending" || journal.TerminalState != "completed" || len(journal.PendingTransitions) != 2 {
		t.Fatalf("run journal = %#v", journal)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(journal.PendingTransitions[1].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	var persisted protocol.TaskResult
	if err := json.Unmarshal(payload["task_result"], &persisted); err != nil || persisted.ResultID != result.ResultID {
		t.Fatalf("terminal task_result = %s, error = %v", payload["task_result"], err)
	}
	if len(journal.PendingEvents) != 3 || journal.PendingEvents[0].Kind != "session_started" || journal.PendingEvents[1].Kind != "message_delta" || journal.PendingEvents[2].Kind != "task_result" {
		t.Fatalf("durable native events = %#v", journal.PendingEvents)
	}
	sessions, err := store.ListGoalSessions()
	if err != nil || len(sessions) != 1 || sessions[0].SessionState != state.GoalSessionStateUnavailable || sessions[0].LaunchState != state.GoalSessionLaunchStateAttached || sessions[0].NativeSessionID != "native-thread-1" || sessions[0].MachineID != "machine-1" || sessions[0].ControlSessionID == "" || len(journal.PendingGoalDeliveries) != 1 || journal.PendingGoalDeliveries[0].Kind != state.GoalDeliverySessionStopped {
		t.Fatalf("Goal session journals = %#v, error = %v", sessions, err)
	}
}

func TestGoalSessionBindingIDUsesServerReservationOnlyForResume(t *testing.T) {
	resumeSessionID := "00000000-0000-4000-8000-000000000007"
	resumeBindingID := "00000000-0000-4000-8000-000000000008"
	tests := []struct {
		name      string
		mode      protocol.SessionMode
		claim     protocol.ClaimResponse
		want      string
		wantError bool
	}{
		{name: "fresh defers binding to Control attach", mode: protocol.SessionModeFresh},
		{name: "handoff defers binding to Control attach", mode: protocol.SessionModeHandoff},
		{name: "resume uses server binding", mode: protocol.SessionModeResume, claim: protocol.ClaimResponse{HarnessSessionID: &resumeSessionID, HarnessBindingID: &resumeBindingID}, want: resumeBindingID},
		{name: "resume requires both server values", mode: protocol.SessionModeResume, claim: protocol.ClaimResponse{HarnessSessionID: &resumeSessionID}, wantError: true},
		{name: "fresh rejects server reservation", mode: protocol.SessionModeFresh, claim: protocol.ClaimResponse{HarnessBindingID: &resumeBindingID}, wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bindingID, err := goalSessionBindingID(protocol.Admission{SessionMode: test.mode}, test.claim)
			if test.wantError {
				if err == nil {
					t.Fatal("goalSessionBindingID() succeeded")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if test.want != "" && bindingID != test.want {
				t.Fatalf("binding ID = %q, want %q", bindingID, test.want)
			}
			if test.want == "" && bindingID != "" {
				t.Fatalf("fresh binding ID = %q, want deferred empty value", bindingID)
			}
		})
	}
}

func TestFreshCodexGoalAdmissionRejectsStrictCostCapBeforeNativeStart(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	cap := "250000"
	admission.Limits.MaxCostMicrousd = &cap
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
	defer store.Close()

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()

	if len(session.calls) != 0 {
		t.Fatalf("strict cap started native transport: %#v", session.calls)
	}
	if len(controlClient.calls) != 0 {
		t.Fatalf("strict cap called Goal control: %#v", controlClient.calls)
	}
	if workspace := app.workspace.(*fakeWorkspace); workspace.subject != (protocol.Subject{}) {
		t.Fatalf("strict cap prepared a Goal workspace: %#v", workspace.subject)
	}
	sessions, err := store.ListGoalSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("strict cap persisted Goal session state: %#v", sessions)
	}
	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	var failure map[string]string
	if err := json.Unmarshal(journal.PendingTransitions[0].Payload, &failure); err != nil {
		t.Fatal(err)
	}
	if failure["stage"] != "goal_admission" || !strings.Contains(failure["error"], "hard_cost_limit") {
		t.Fatalf("strict cap failure = %#v", failure)
	}
}

func TestGoalReadyMarkerFailureDiscardsUnreadyAttachBeforeTerminalCleanup(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
	defer store.Close()
	markerFailure := errors.New("ready marker persistence failed")
	app.options.markGoalSessionAttachDeliveryReady = func(state.RunKey, string) (state.RunJournal, error) {
		return state.RunJournal{}, markerFailure
	}

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()

	if got, want := session.calls, []string{"start", "details", "open", "details", "close", "wait"}; !sameStrings(got, want) {
		t.Fatalf("native lifecycle = %#v, want %#v", got, want)
	}
	if len(controlClient.calls) != 0 {
		t.Fatalf("Goal control calls = %#v, want none", controlClient.calls)
	}
	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if journal.HasPendingGoalDeliveries() || journal.PID != 0 || journal.ProcessIdentity != "" {
		t.Fatalf("marker failure left deliverable or live process state: %#v", journal)
	}
	if active := app.runningRun(journal.Key()); active == nil || active.nativeSession != nil || active.cleanupBlocked {
		t.Fatalf("marker failure active state = %#v, want closed native session", active)
	}
}

func TestGoalReadyMarkerFailureRetriesUnreadyDiscardWithoutRestart(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
	defer store.Close()
	app.options.markGoalSessionAttachDeliveryReady = func(state.RunKey, string) (state.RunJournal, error) {
		return state.RunJournal{}, errors.New("ready marker persistence failed")
	}
	discardCalls := 0
	app.options.discardUnreadyGoalSessionAttachDelivery = func(key state.RunKey, localHandleID string) (state.RunJournal, error) {
		discardCalls++
		if discardCalls == 1 {
			return state.RunJournal{}, errors.New("discard persistence failed")
		}
		return store.DiscardUnreadyGoalSessionAttachDelivery(key, localHandleID)
	}

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()
	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingGoalDeliveries) != 1 || journal.PendingGoalDeliveries[0].Ready {
		t.Fatalf("first compensation failure did not retain only unready attach: %#v", journal.PendingGoalDeliveries)
	}
	app.retryUnreadyGoalSessionAttachDeliveries()
	journal, err = store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if journal.HasPendingGoalDeliveries() || discardCalls != 2 {
		t.Fatalf("same-daemon discard retry = deliveries:%#v calls:%d", journal.PendingGoalDeliveries, discardCalls)
	}
	if got, want := session.calls, []string{"start", "details", "open", "details", "close", "wait"}; !sameStrings(got, want) {
		t.Fatalf("native lifecycle = %#v, want %#v", got, want)
	}
	if len(controlClient.calls) != 0 {
		t.Fatalf("Goal control calls = %#v, want none", controlClient.calls)
	}
}

func TestGoalReadyMarkerFailureRetriesNativeCloseWithoutRestart(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
	defer store.Close()
	app.options.markGoalSessionAttachDeliveryReady = func(state.RunKey, string) (state.RunJournal, error) {
		return state.RunJournal{}, errors.New("ready marker persistence failed")
	}
	session.closeErr = errors.New("native close unavailable")

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()
	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if journal.HasPendingGoalDeliveries() || !journal.RetainWorkspace {
		t.Fatalf("failed close did not discard attach and retain workspace: %#v", journal)
	}
	active := app.runningRun(journal.Key())
	if active == nil || active.nativeSession != session || !active.nativeCloseRetryRequired || !active.cleanupBlocked {
		t.Fatalf("failed close was not retained for same-daemon retry: %#v", active)
	}

	session.closeErr = nil
	app.retryBlockedNativeSessionCloses()
	journal, err = store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if journal.PID != 0 || journal.ProcessIdentity != "" {
		t.Fatalf("same-daemon close retry left process ownership: %#v", journal)
	}
	if active = app.runningRun(journal.Key()); active == nil || active.nativeSession != nil || active.nativeCloseRetryRequired || active.cleanupBlocked {
		t.Fatalf("same-daemon close retry did not release native state: %#v", active)
	}
	if len(controlClient.calls) != 0 {
		t.Fatalf("Goal control calls = %#v, want none", controlClient.calls)
	}
}

func TestNativeTurnCloseFailureEntersSameDaemonRetry(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedGoalDeliveryStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	session := &fakeNativeGoalSession{closeErr: errors.New("close unavailable"), turnStarted: make(chan struct{})}
	active := &runningRun{nativeSession: session, goalSession: &sessionKey}
	daemon := &daemon{
		store:   store,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{key: active},
		slots:   make(chan struct{}, 1),
		options: options{clock: time.Now},
	}
	if err := daemon.closeNativeGoalSession(key, active, session); err == nil {
		t.Fatal("closeNativeGoalSession() succeeded despite close failure")
	}
	if !active.nativeCloseRetryRequired || !active.cleanupBlocked {
		t.Fatalf("native close failure did not enter retry: %#v", active)
	}
	session.closeErr = nil
	daemon.retryBlockedNativeSessionCloses()
	if active.nativeCloseRetryRequired || active.cleanupBlocked || active.nativeSession != nil {
		t.Fatalf("native close retry did not release active state: %#v", active)
	}
}

func TestNativeCloseDurableWriteFailuresRetryWithoutLeakingRecoveryBarrier(t *testing.T) {
	for _, test := range []struct {
		name string
		wire func(*daemon, *state.Store, state.GoalSessionKey) func()
	}{
		{
			name: "load Goal session",
			wire: func(app *daemon, store *state.Store, sessionKey state.GoalSessionKey) func() {
				calls := 0
				app.options.loadGoalSession = func(key state.GoalSessionKey) (state.GoalSessionJournal, error) {
					calls++
					if calls == 1 {
						return state.GoalSessionJournal{}, errors.New("temporary Goal session load failure")
					}
					return store.LoadGoalSession(key)
				}
				return func() {}
			},
		},
		{
			name: "close Goal session",
			wire: func(app *daemon, store *state.Store, sessionKey state.GoalSessionKey) func() {
				calls := 0
				app.options.closeGoalSession = func(key state.GoalSessionKey) (state.GoalSessionJournal, error) {
					calls++
					if calls == 1 {
						return state.GoalSessionJournal{}, errors.New("temporary Goal session close failure")
					}
					return store.CloseGoalSession(key)
				}
				return func() {}
			},
		},
		{
			name: "clear process details",
			wire: func(app *daemon, store *state.Store, _ state.GoalSessionKey) func() {
				calls := 0
				app.options.clearProcessDetails = func(key state.RunKey, pid int, identity string) (state.RunJournal, error) {
					calls++
					if calls == 1 {
						return state.RunJournal{}, errors.New("temporary process clear failure")
					}
					return store.ClearProcessDetails(key, pid, identity)
				}
				return func() {}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			admission, present, err := parseAdmissionInput(validAdmissionInput())
			if err != nil || !present {
				t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
			}
			store, key := claimedGoalDeliveryStore(t)
			sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000022"}
			saveAttachedGoalSession(t, store, key, admission, sessionKey)
			// The persisted marker must identify the same native owner captured by
			// closeNativeSessionWithStopProof; this test exercises durable-write
			// retries, not an identity mismatch.
			if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			session := &fakeNativeGoalSession{turnStarted: make(chan struct{}), processPID: 71, processIdentity: "native:71"}
			active := &runningRun{nativeSession: session, goalSession: &sessionKey, slotHeld: true}
			app := &daemon{
				store:   store,
				log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
				running: map[state.RunKey]*runningRun{key: active},
				slots:   make(chan struct{}, 1),
				options: options{clock: time.Now},
			}
			app.slots <- struct{}{}
			_ = test.wire(app, store, sessionKey)

			if err := app.closeNativeGoalSession(key, active, session); err == nil {
				t.Fatal("closeNativeGoalSession() succeeded despite durable write failure")
			}
			if active.nativeSession != session || !active.nativeCloseRetryRequired || !active.cleanupBlocked || !active.slotHeld {
				t.Fatalf("durable close failure lost active recovery barrier: %#v", active)
			}
			journal, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if journal.PID != 71 || journal.ProcessIdentity != "native:71" {
				t.Fatalf("durable close failure cleared process evidence: %#v", journal)
			}

			app.retryBlockedNativeSessionCloses()
			journal, err = store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if journal.PID != 0 || journal.ProcessIdentity != "" || active.nativeSession != nil || active.goalSession != nil || active.nativeCloseRetryRequired || active.cleanupBlocked {
				t.Fatalf("same-daemon durable close retry left recovery state: journal=%#v active=%#v", journal, active)
			}
			sessionJournal, err := store.LoadGoalSession(sessionKey)
			if err != nil || sessionJournal.SessionState != state.GoalSessionStateClosed {
				t.Fatalf("same-daemon durable close retry did not close session: %#v, %v", sessionJournal, err)
			}
			app.releaseRun(key)
			if len(app.slots) != 0 {
				t.Fatal("recovered run leaked its capacity slot")
			}
		})
	}
}

func TestNativeCloseRetrySuccessDoesNotEraseUsageRecoveryBarrier(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000023"}
	session := &fakeNativeGoalSession{turnStarted: make(chan struct{})}
	active := &runningRun{
		nativeSession:            session,
		goalSession:              &sessionKey,
		nativeCloseRetryRequired: true,
		nativeCloseRetrying:      true,
		nativeUsageRetryPending:  true,
		cleanupBlocked:           true,
	}
	app := &daemon{store: store, running: map[state.RunKey]*runningRun{key: active}}

	app.publishNativeCloseRetrySuccess(key, active, session)
	if active.nativeSession != nil || active.goalSession != nil || active.nativeCloseRetryRequired || active.nativeCloseRetrying || !active.nativeUsageRetryPending || !active.cleanupBlocked {
		t.Fatalf("native close retry erased usage recovery barrier: %#v", active)
	}
}

func TestCommandRunSnapshotRetainsNativeRouteAcrossCloseRecovery(t *testing.T) {
	key := state.RunKey{RunID: "run-1", Generation: 1}
	session := &fakeNativeGoalSession{turnStarted: make(chan struct{})}
	active := &runningRun{nativeSession: session}
	app := &daemon{running: map[state.RunKey]*runningRun{key: active}}

	snapshot, nativeSessionActive := app.commandRunSnapshot(key)
	if snapshot != active || !nativeSessionActive {
		t.Fatalf("command snapshot = (%#v, %t), want active native session", snapshot, nativeSessionActive)
	}
	// This is the close-recovery publication that races command routing in the
	// daemon. The command must use its captured boolean rather than rereading a
	// field which this path clears under daemon.mu.
	app.publishNativeCloseRetrySuccess(key, active, session)
	if !nativeSessionActive {
		t.Fatal("close recovery changed the command's native-session snapshot")
	}
	_, currentNativeSession := app.commandRunSnapshot(key)
	if currentNativeSession {
		t.Fatal("close recovery did not clear the current native session")
	}
}

func TestOriginalNativeCompletionDoesNotEraseUsageRetryBarrier(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	active := &runningRun{
		nativeUsageRetryPending:  true,
		nativeUsageRetryInFlight: true,
		cleanupBlocked:           true,
	}
	app := &daemon{store: store, running: map[state.RunKey]*runningRun{key: active}}

	app.completeNativeRunAfterUsage(context.Background(), key, active, harness.TaskResult{Kind: harness.ResultSucceeded}, nil, nil, false)
	if !active.nativeUsageRetryPending || !active.nativeUsageRetryInFlight || !active.cleanupBlocked || active.nativeUsageFinalized {
		t.Fatalf("original completion erased active usage retry barrier: %#v", active)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "" {
		t.Fatalf("original completion published alongside a newer usage retry: %#v", journal)
	}
}

func TestNativeCloseRetrySuccessDoesNotLeaveStaleCloseErrorAsCleanupBarrier(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedGoalDeliveryStore(t)
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000024"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	finalGate := make(chan struct{})
	closeEntered := make(chan struct{}, 2)
	retryPublished := make(chan struct{}, 1)
	result := validNativeTaskResult(t, admission)
	session := &fakeNativeGoalSession{
		result:          harness.TaskResult{Kind: harness.ResultSucceeded, Summary: result.Summary, Semantic: &result},
		finalWaitGate:   finalGate,
		turnStarted:     make(chan struct{}),
		waitEntered:     make(chan struct{}, 2),
		processPID:      71,
		processIdentity: "native:71",
		closeErr:        errors.New("first close failed"),
		closeEntered:    closeEntered,
	}
	active := &runningRun{
		nativeSession:  session,
		goalSession:    &sessionKey,
		goalAdmission:  &admission,
		prepared:       workspace.Prepared{Path: "C:\\workspace"},
		slotHeld:       true,
		cleanupBlocked: true,
	}
	app := &daemon{
		store:   store,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{key: active},
		slots:   make(chan struct{}, 1),
		options: options{newID: ids(), clock: time.Now, nativeCloseRetryPublished: func() { retryPublished <- struct{}{} }},
	}
	app.slots <- struct{}{}
	done := make(chan struct{})
	go func() {
		app.waitForNativeRun(context.Background(), key, active, session)
		close(done)
	}()
	select {
	case <-closeEntered:
	case <-time.After(time.Second):
		t.Fatal("initial native close did not start")
	}
	select {
	case <-retryPublished:
	case <-time.After(time.Second):
		t.Fatal("initial native close failure did not publish recovery barrier")
	}
	if !active.nativeCloseRetryRequired || !active.cleanupBlocked {
		t.Fatalf("initial native close failure did not publish recovery barrier: %#v", active)
	}
	select {
	case <-session.waitEntered:
	case <-time.After(time.Second):
		t.Fatal("initial native final wait did not start")
	}
	session.closeErr = nil
	retryDone := make(chan struct{})
	go func() {
		app.retryBlockedNativeSessionCloses()
		close(retryDone)
	}()
	select {
	case <-session.waitEntered:
	case <-time.After(time.Second):
		t.Fatal("close retry did not reach final native wait")
	}
	select {
	case <-retryDone:
		t.Fatal("close retry cleared its recovery barrier before final native wait completed")
	default:
	}
	close(finalGate)
	select {
	case <-retryDone:
	case <-time.After(time.Second):
		t.Fatal("close retry did not finish after final native wait release")
	}
	app.mu.Lock()
	sessionRetained := active.nativeSession != nil
	closeRetryPending := active.nativeCloseRetryRequired
	app.mu.Unlock()
	if sessionRetained || closeRetryPending {
		t.Fatalf("successful close retry retained native ownership: session=%t retry=%t", sessionRetained, closeRetryPending)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("original native completion did not finish after retry")
	}
	if active.cleanupBlocked || active.nativeUsageRetryPending || active.nativeUsageRetryInFlight || !active.nativeUsageFinalized {
		t.Fatalf("original completion recreated stale close or usage barrier: %#v", active)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "failed" || !strings.Contains(string(journal.PendingTransitions[0].Payload), "first close failed") {
		t.Fatalf("stale close error was not retained only as terminal diagnosis: %#v", journal)
	}
}

func TestFreshCodexGoalAdmissionRejectsMismatchedRuntimeRepositoryBeforeWorkspace(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	result := validNativeTaskResult(t, admission)
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, result, nil)
	defer store.Close()
	app.config.Runtime.RepositoryResourceID = "00000000-0000-4000-8000-000000000099"
	localWorkspace := app.workspace.(*fakeWorkspace)

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()

	if len(session.calls) != 0 {
		t.Fatalf("native lifecycle = %#v, want no launch", session.calls)
	}
	if localWorkspace.subject != (protocol.Subject{}) {
		t.Fatalf("workspace subject = %#v, want no workspace preparation", localWorkspace.subject)
	}
	sessions, err := store.ListGoalSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("Goal sessions = %#v, want no persisted launch", sessions)
	}
	if len(controlClient.calls) != 0 {
		t.Fatalf("Goal control side effects = %#v, want none", controlClient.calls)
	}
	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	var failure map[string]string
	if err := json.Unmarshal(journal.PendingTransitions[0].Payload, &failure); err != nil {
		t.Fatal(err)
	}
	if failure["stage"] != "goal_admission" || !strings.Contains(failure["error"], "repository_resource_id") {
		t.Fatalf("failure = %#v, want runtime repository affinity rejection", failure)
	}
}

func TestFreshCodexGoalAdmissionRejectsMismatchedRuntimeProfileBeforeWorkspace(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	admission.ModelProfile = "implementation-default"
	result := validNativeTaskResult(t, admission)
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, result, nil)
	defer store.Close()
	localWorkspace := app.workspace.(*fakeWorkspace)

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()

	if len(session.calls) != 0 {
		t.Fatalf("native lifecycle = %#v, want no launch", session.calls)
	}
	if localWorkspace.subject != (protocol.Subject{}) {
		t.Fatalf("workspace subject = %#v, want no workspace preparation", localWorkspace.subject)
	}
	sessions, err := store.ListGoalSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("Goal sessions = %#v, want no persisted launch", sessions)
	}
	if len(controlClient.calls) != 0 {
		t.Fatalf("Goal control side effects = %#v, want none", controlClient.calls)
	}
	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if journal.WorkspaceRecoveryRequired || journal.WorkspacePath != "" {
		t.Fatalf("workspace side effects = recovery %v, path %q; want none", journal.WorkspaceRecoveryRequired, journal.WorkspacePath)
	}
	var failure map[string]string
	if err := json.Unmarshal(journal.PendingTransitions[0].Payload, &failure); err != nil {
		t.Fatal(err)
	}
	if failure["stage"] != "goal_admission" || !strings.Contains(failure["error"], "model_profile") {
		t.Fatalf("failure = %#v, want runtime profile rejection", failure)
	}
}

func TestFreshCodexGoalAdmissionRejectsMismatchedSemanticSubject(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	result := validNativeTaskResult(t, admission)
	result.SubjectHash = "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	app, store, _, controlClient := nativeAdmissionDaemon(t, admission, result, nil)
	defer store.Close()

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()
	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "failed" || len(journal.PendingTransitions) != 2 || !strings.Contains(string(journal.PendingTransitions[1].Payload), "subject_hash") {
		t.Fatalf("mismatched result journal = %#v", journal)
	}
}

func TestFreshCodexGoalAdmissionAcceptsVerifiedCandidateSubject(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	candidate := admission.Subject
	candidate.Commit = strings.Repeat("1", 40)
	candidate.TreeDigest = "sha256:" + strings.Repeat("1", 64)
	candidateHash, err := candidate.Hash()
	if err != nil {
		t.Fatal(err)
	}
	result := validNativeTaskResult(t, admission)
	result.Kind = protocol.TaskResultCandidateCompletion
	result.Subject = candidate
	result.SubjectHash = candidateHash
	app, store, _, controlClient := nativeAdmissionDaemon(t, admission, result, nil)
	defer store.Close()
	app.workspace.(*fakeWorkspace).derivedSubject = candidate

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()

	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "completed" || len(journal.PendingTransitions) != 2 {
		t.Fatalf("candidate terminal journal = %#v", journal)
	}
	if !strings.Contains(string(journal.PendingTransitions[1].Payload), candidate.Commit) {
		t.Fatalf("candidate Subject was not retained in terminal payload: %s", journal.PendingTransitions[1].Payload)
	}
}

func TestFreshCodexGoalAdmissionRejectsAdvancedProgressSubject(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	candidate := admission.Subject
	candidate.Commit = strings.Repeat("2", 40)
	candidate.TreeDigest = "sha256:" + strings.Repeat("2", 64)
	candidateHash, err := candidate.Hash()
	if err != nil {
		t.Fatal(err)
	}
	result := validNativeTaskResult(t, admission)
	result.Subject = candidate
	result.SubjectHash = candidateHash
	app, store, _, controlClient := nativeAdmissionDaemon(t, admission, result, nil)
	defer store.Close()
	app.workspace.(*fakeWorkspace).derivedSubject = candidate

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()

	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "failed" || !strings.Contains(string(journal.PendingTransitions[1].Payload), "advanced beyond") {
		t.Fatalf("advanced progress journal = %#v", journal)
	}
}

func TestFreshCodexGoalAdmissionRejectsElapsedDeadlineBeforeLaunch(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	admission.Limits.DeadlineAt = time.Now().UTC().Add(-time.Second).Format(time.RFC3339)
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
	defer store.Close()

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()
	if len(session.calls) != 0 {
		t.Fatalf("native adapter started after elapsed deadline: %#v", session.calls)
	}
	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "failed" || !strings.Contains(string(journal.PendingTransitions[0].Payload), "deadline") {
		t.Fatalf("elapsed deadline journal = %#v", journal)
	}
}

func TestFreshCodexGoalAdmissionRetiresUnreadyAttachAfterNativeStartFailure(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
	defer store.Close()
	session.startErr = errors.New("native app server failed before session creation")

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()

	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingGoalDeliveries) != 0 {
		t.Fatalf("unready attach delivery was retained after native Start failure: %#v", journal.PendingGoalDeliveries)
	}
	if !journal.RetainWorkspace {
		t.Fatal("unknown native launch did not retain its workspace")
	}
	var failure map[string]string
	if err := json.Unmarshal(journal.PendingTransitions[0].Payload, &failure); err != nil {
		t.Fatal(err)
	}
	if failure["reason"] != string(protocol.TaskResultReasonUnknownOutcome) {
		t.Fatalf("native launch failure = %#v, want canonical unknown outcome", failure)
	}
	sessions, err := store.ListGoalSessions()
	if err != nil || len(sessions) != 1 || !sessions[0].NeedsReconciliation() {
		t.Fatalf("Goal session uncertainty = %#v, error = %v", sessions, err)
	}
}

func TestFreshCodexGoalAttachQueueFailureReportsAbortCompensationFailure(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
	defer store.Close()
	key := state.RunKey{RunID: "run-1", Generation: 1}
	saveClaimedGoalRun(t, store, key)

	abortFailure := errors.New("abort launch compensation failed")
	app.options.abortGoalSessionLaunchBeforeNativeStart = func(state.GoalSessionKey) (state.GoalSessionJournal, error) {
		return state.GoalSessionJournal{}, abortFailure
	}
	var conflictQueueErr error
	err = app.startGoalAdmission(context.Background(), key, protocol.ClaimResponse{
		RunID: key.RunID, Generation: key.Generation, TaskID: "task-1", Work: controlClient.work,
	}, admission, func(workspace.Prepared) {
		sessions, listErr := store.ListGoalSessions()
		if listErr != nil {
			t.Fatalf("ListGoalSessions() in workspace callback: %v", listErr)
		}
		if len(sessions) != 1 {
			t.Fatalf("Goal sessions in workspace callback = %#v, want one", sessions)
		}
		resourceID := admission.Subject.ResourceID
		_, conflictQueueErr = store.QueueGoalSessionAttach(key, state.GoalSessionAttachDelivery{
			GoalID:               admission.GoalID,
			LocalHandleID:        sessions[0].LocalHandleID,
			ServerIssuedBinding:  true,
			HarnessKind:          string(app.harnessCapabilities.Kind),
			HarnessVersion:       app.harnessCapabilities.NativeVersion,
			AdapterVersion:       app.harnessCapabilities.ImplementationVersion,
			WorkspaceFingerprint: sessions[0].WorkspaceFingerprint,
			Workspace:            "conflicting-workspace",
			RepositoryResourceID: &resourceID,
		})
	})
	if conflictQueueErr != nil {
		t.Fatalf("workspace callback unexpectedly failed to queue conflicting attach: %v", conflictQueueErr)
	}
	if !errors.Is(err, state.ErrGoalDeliveryConflict) || !errors.Is(err, abortFailure) {
		t.Fatalf("attach queue compensation error = %v, want both queue conflict and abort failure", err)
	}
	if len(session.calls) != 0 {
		t.Fatalf("native adapter started after attach queue failure: %v", session.calls)
	}
	sessions, err := store.ListGoalSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].LaunchState != state.GoalSessionLaunchStateLaunching || !sessions[0].NeedsReconciliation() {
		t.Fatalf("failed abort compensation erased launch barrier: %#v", sessions)
	}
}

func TestFreshCodexNativeStartFailureReportsUncertainCompensationFailure(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
	defer store.Close()
	key := state.RunKey{RunID: "run-1", Generation: 1}
	saveClaimedGoalRun(t, store, key)
	startFailure := errors.New("native start failed before session creation")
	uncertainFailure := errors.New("persist uncertain compensation failed")
	session.startErr = startFailure
	app.options.markGoalSessionUncertain = func(state.GoalSessionKey, string) (state.GoalSessionJournal, error) {
		return state.GoalSessionJournal{}, uncertainFailure
	}

	err = app.startGoalAdmission(context.Background(), key, protocol.ClaimResponse{
		RunID: key.RunID, Generation: 1, TaskID: "task-1", Work: controlClient.work,
	}, admission, nil)
	if !errors.Is(err, startFailure) || !errors.Is(err, uncertainFailure) {
		t.Fatalf("native Start compensation error = %v, want both start and uncertainty failures", err)
	}
	if len(session.calls) != 1 || session.calls[0] != "start" {
		t.Fatalf("native Start failure performed unexpected calls: %v", session.calls)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.HasPendingGoalDeliveries() || !journal.RetainWorkspace {
		t.Fatalf("Start failure lost discard or workspace retention barrier: %#v", journal)
	}
	sessions, err := store.ListGoalSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 || sessions[0].LaunchState != state.GoalSessionLaunchStateLaunching || !sessions[0].NeedsReconciliation() {
		t.Fatalf("failed uncertainty compensation claimed a resolved session: %#v", sessions)
	}
}

func TestNativeGoalDeadlineInterruptsThenClosesBeforeFailure(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	intent := state.GoalSessionLaunchIntent{
		LaunchIntentID: "00000000-0000-4000-8000-000000000008", GoalID: admission.GoalID, GoalRevision: admission.GoalRevision,
		WorkItemID: admissionWorkItemIDValue(admission.WorkItemID), TaskID: "task-1", RunID: key.RunID, Generation: key.Generation, AdmissionID: admission.AdmissionID,
		LocalHandleID: sessionKey.LocalHandleID, RuntimeID: "runtime-1", RuntimeEpoch: 1, HarnessKind: "codex", HarnessVersion: "0.153.4",
		AdapterVersion: "symmetry-daemon:test", AdapterProtocolVersion: 1,
		WorkspaceFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SessionMode: state.GoalSessionModeFresh,
	}
	if _, err := store.SaveGoalSessionLaunchIntent(intent); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(sessionKey); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PersistGoalSessionHandle(sessionKey, state.GoalSessionHandle{NativeSessionID: "native-thread-1"}); err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	session := &fakeNativeGoalSession{waitGate: gate, turnStarted: make(chan struct{})}
	active := &runningRun{nativeSession: session, goalSession: &sessionKey, goalAdmission: &admission, nativeDeadline: time.Now().Add(-time.Second)}
	app := &daemon{
		config: testConfig(t), store: store, options: options{newID: ids(), clock: time.Now},
		running: map[state.RunKey]*runningRun{key: active}, slots: make(chan struct{}, 1),
	}

	app.waitForNativeRun(context.Background(), key, active, session)
	if got, want := session.calls, []string{"wait_turn", "details", "close", "wait", "wait"}; !sameStrings(got, want) {
		t.Fatalf("deadline lifecycle = %#v, want %#v", got, want)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "failed" || !strings.Contains(string(journal.PendingTransitions[0].Payload), "deadline exceeded") {
		t.Fatalf("deadline terminal journal = %#v", journal)
	}
	sessions, err := store.ListGoalSessions()
	if err != nil || len(sessions) != 1 || sessions[0].SessionState != state.GoalSessionStateClosed {
		t.Fatalf("deadline session close = %#v, error = %v", sessions, err)
	}
}

func TestRecoverUnclosedGoalSessionStopsPersistedProcessAndFailsUnknownOutcome(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	terminated := 0
	app := &daemon{
		config: testConfig(t), store: store,
		options: options{
			newID: ids(), clock: time.Now,
			terminatePersist: func(pid int, identity string) error {
				terminated++
				if pid != 71 || identity != "native:71" {
					t.Fatalf("persisted native process = %d %q", pid, identity)
				}
				return nil
			},
		},
		running: make(map[state.RunKey]*runningRun), slots: make(chan struct{}, 1),
	}
	if err := app.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.PID != 0 || journal.ProcessIdentity != "" || !journal.StartedAt.IsZero() {
		t.Fatalf("stopped native process marker = %#v", journal)
	}
	if err := app.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if terminated != 1 {
		t.Fatalf("persisted native process stops = %d, want 1", terminated)
	}
	session, err := store.LoadGoalSession(sessionKey)
	if err != nil {
		t.Fatal(err)
	}
	if !session.IsUncertainLaunch() || session.SessionState != state.GoalSessionStateUnavailable {
		t.Fatalf("recovered Goal session = %#v", session)
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.RetainWorkspace || journal.TerminalState != "failed" || len(journal.PendingTransitions) != 1 ||
		!strings.Contains(string(journal.PendingTransitions[0].Payload), string(protocol.TaskResultReasonUnknownOutcome)) {
		t.Fatalf("recovered run journal = %#v", journal)
	}
}

func TestNativeTerminalPayloadPreservesFramingUnknownOutcome(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	reason := protocol.TaskResultReasonUnknownOutcome
	for _, resultKind := range []harness.ResultKind{harness.ResultUnknown, harness.ResultFailed} {
		stateName, payload := nativeTerminalPayload(harness.TaskResult{
			Kind:    resultKind,
			Summary: "Codex framing failed",
			Reason:  &reason,
		}, &admission, nil, nil)
		if stateName != "failed" || payload["reason"] != string(protocol.TaskResultReasonUnknownOutcome) {
			t.Fatalf("nativeTerminalPayload(%q) = (%q, %#v), want failed unknown_outcome", resultKind, stateName, payload)
		}
		if payload["reason"] == string(protocol.TaskResultReasonMissingResult) {
			t.Fatalf("nativeTerminalPayload(%q) rewrote unknown outcome as missing result: %#v", resultKind, payload)
		}
	}
}

func TestNativeTerminalPayloadPreservesMissingResultWhenWaitFails(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	reason := protocol.TaskResultReasonMissingResult
	stateName, payload := nativeTerminalPayload(harness.TaskResult{
		Kind:    harness.ResultFailed,
		Summary: "Codex app-server exited without a structured task result",
		Reason:  &reason,
	}, &admission, errors.New("Codex native turn did not reach a matching terminal event: missing_result"), nil)
	if stateName != "failed" || payload["reason"] != string(protocol.TaskResultReasonMissingResult) {
		t.Fatalf("nativeTerminalPayload() = (%q, %#v), want failed missing_result", stateName, payload)
	}
}

func TestNativeTerminalPayloadPreservesProcessFailureWhenWaitFails(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	reason := protocol.TaskResultReasonProcessFailure
	stateName, payload := nativeTerminalPayload(harness.TaskResult{
		Kind:    harness.ResultFailed,
		Summary: "native process exited after terminal result",
		Reason:  &reason,
	}, &admission, errors.New("wait native final result: process exited"), nil)
	if stateName != "failed" || payload["reason"] != string(protocol.TaskResultReasonProcessFailure) {
		t.Fatalf("nativeTerminalPayload() = (%q, %#v), want failed process_failure", stateName, payload)
	}
}

func TestNativeTerminalPayloadPrefersSemanticFailureReason(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	semantic := validNativeTaskResult(t, admission)
	semantic.Kind = protocol.TaskResultFailed
	semanticReason := protocol.TaskResultReasonQuota
	semantic.Reason = &semanticReason
	physicalReason := protocol.TaskResultReasonProcessFailure
	stateName, payload := nativeTerminalPayload(harness.TaskResult{
		Kind:     harness.ResultFailed,
		Summary:  "native process exited after reporting a quota result",
		Semantic: &semantic,
		Reason:   &physicalReason,
	}, &admission, taskResultFailure(protocol.TaskResultReasonProcessFailure, errors.New("process exited")), nil)
	if stateName != "failed" || payload["reason"] != string(protocol.TaskResultReasonQuota) {
		t.Fatalf("nativeTerminalPayload() = (%q, %#v), want failed quota", stateName, payload)
	}
}

func TestRecoverUnclosedGoalSessionStopsPersistedProcessAfterTerminalTransition(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetProcessDetails(key, 72, "native:72", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{
		TransitionID: "terminal-native-session", State: "failed", Payload: json.RawMessage(`{}`),
	}); err != nil {
		t.Fatal(err)
	}
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000009"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	terminated := 0
	app := &daemon{
		config: testConfig(t), store: store,
		options: options{
			newID: ids(), clock: time.Now,
			terminatePersist: func(pid int, identity string) error {
				terminated++
				if pid != 72 || identity != "native:72" {
					t.Fatalf("persisted native process = %d %q", pid, identity)
				}
				return nil
			},
		},
		running: make(map[state.RunKey]*runningRun), slots: make(chan struct{}, 1),
	}
	if err := app.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	if terminated != 1 {
		t.Fatalf("persisted native process stops = %d, want 1", terminated)
	}
	session, err := store.LoadGoalSession(sessionKey)
	if err != nil {
		t.Fatal(err)
	}
	if !session.IsUncertainLaunch() {
		t.Fatalf("recovered Goal session = %#v", session)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "failed" || len(journal.PendingTransitions) != 1 {
		t.Fatalf("terminal run journal = %#v", journal)
	}
}

func TestRecoverCancelledGoalSessionWithoutProcessEvidenceRetainsBarrier(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000021"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.MarkGoalSessionUncertain(sessionKey, "native stop has no persisted evidence"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{
		TransitionID: "cancelled-without-stop-proof", State: "cancelled", Payload: json.RawMessage(`{"reason":"cancelled"}`),
	}); err != nil {
		t.Fatal(err)
	}
	app := &daemon{config: testConfig(t), store: store, options: options{newID: ids(), clock: time.Now}, running: make(map[state.RunKey]*runningRun), slots: make(chan struct{}, 1)}
	if err := app.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	session, err := store.LoadGoalSession(sessionKey)
	if err != nil {
		t.Fatal(err)
	}
	if !session.NeedsReconciliation() {
		t.Fatalf("recovery inferred stopped native session without positive process evidence: %#v", session)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.RetainWorkspace || journal.TerminalState != "cancelled" {
		t.Fatalf("cancelled recovery did not retain unresolved session workspace: %#v", journal)
	}
}

func TestRecoverClosedTerminalGoalSessionRetiresUnreadyAttach(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000019"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.QueueGoalSessionAttach(key, state.GoalSessionAttachDelivery{
		GoalID:               admission.GoalID,
		LocalHandleID:        sessionKey.LocalHandleID,
		BindingID:            "00000000-0000-4000-8000-000000000009",
		HarnessKind:          "codex",
		HarnessVersion:       "0.153.4",
		AdapterVersion:       "symmetry-daemon:test",
		WorkspaceFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Workspace:            "local",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CloseGoalSession(sessionKey); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{
		TransitionID: "terminal-closed-unready",
		State:        "failed",
		Payload:      json.RawMessage(`{"reason":"unknown_outcome"}`),
	}); err != nil {
		t.Fatal(err)
	}

	app := &daemon{config: testConfig(t), store: store, options: options{newID: ids(), clock: time.Now}, running: make(map[state.RunKey]*runningRun), slots: make(chan struct{}, 1)}
	if err := app.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingGoalDeliveries) != 0 || journal.LocalState != "terminal_pending" {
		t.Fatalf("closed terminal unready attach recovery = %#v", journal)
	}
}

func TestRecoverUnclosedGoalSessionSkipsLiveOwner(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	app := &daemon{
		config: testConfig(t), store: store, options: options{newID: ids(), clock: time.Now},
		running: map[state.RunKey]*runningRun{key: {nativeSession: &fakeNativeGoalSession{turnStarted: make(chan struct{})}}}, slots: make(chan struct{}, 1),
	}
	if err := app.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatal(err)
	}
	session, err := store.LoadGoalSession(sessionKey)
	if err != nil {
		t.Fatal(err)
	}
	if session.IsUncertainLaunch() {
		t.Fatalf("live owner session was changed: %#v", session)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "" || journal.RetainWorkspace {
		t.Fatalf("live owner run was changed: %#v", journal)
	}
}

func saveAttachedGoalSession(t *testing.T, store *state.Store, key state.RunKey, admission protocol.Admission, sessionKey state.GoalSessionKey) {
	t.Helper()
	intent := state.GoalSessionLaunchIntent{
		LaunchIntentID: "00000000-0000-4000-8000-000000000008", GoalID: admission.GoalID, GoalRevision: admission.GoalRevision,
		WorkItemID: admissionWorkItemIDValue(admission.WorkItemID), TaskID: "task-1", RunID: key.RunID, Generation: key.Generation, AdmissionID: admission.AdmissionID,
		LocalHandleID: sessionKey.LocalHandleID, RuntimeID: "runtime-1", RuntimeEpoch: 1, HarnessKind: "codex", HarnessVersion: "0.153.4",
		AdapterVersion: "symmetry-daemon:test", AdapterProtocolVersion: 1,
		WorkspaceFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SessionMode: state.GoalSessionModeFresh,
	}
	if _, err := store.SaveGoalSessionLaunchIntent(intent); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(sessionKey); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PersistGoalSessionHandle(sessionKey, state.GoalSessionHandle{NativeSessionID: "native-thread-1"}); err != nil {
		t.Fatal(err)
	}
}

func TestFreshCodexGoalCancellationUsesNativeControlBeforeReceipt(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	gate := make(chan struct{})
	turnReturn := make(chan struct{})
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), gate)
	defer store.Close()
	session.turnReturnGate = turnReturn
	session.onControl = func() { close(gate) }

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	select {
	case <-session.turnStarted:
	case <-time.After(time.Second):
		t.Fatal("native turn did not start")
	}
	command := protocol.Command{RunID: "run-1", Generation: 1, CommandID: "cancel-1", Kind: "cancel"}
	if !app.handleCommand(context.Background(), command) {
		t.Fatal("native cancellation was not accepted")
	}
	if len(session.calls) < 5 || !sameStrings(session.calls[:5], []string{"start", "details", "open", "start_turn", "control:cancel"}) {
		t.Fatalf("native cancellation order = %#v", session.calls)
	}
	close(turnReturn)
	app.workers.Wait()
	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "cancelled" || len(journal.PendingTransitions) != 1 || journal.PendingTransitions[0].State != "cancelled" {
		t.Fatalf("native cancellation journal = %#v", journal)
	}
}

func TestGoalCancellationPersistsFinalUsageBeforeCancelledReceipt(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedGoalDeliveryStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	session := &fakeNativeGoalSession{
		handle:      harness.NativeSessionHandle{ID: "native-thread-1"},
		result:      harness.TaskResult{Kind: harness.ResultSucceeded, Summary: "final result won the cancel race"},
		turnStarted: make(chan struct{}),
	}
	daemon := &daemon{
		config:  testConfig(t),
		store:   store,
		control: &fakeControl{},
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{key: {
			claimed:       true,
			nativeSession: session,
			goalSession:   &sessionKey,
		}},
		slots:   make(chan struct{}, 1),
		options: options{newID: ids(), clock: time.Now},
	}
	if !daemon.handleCommand(context.Background(), protocol.Command{RunID: key.RunID, Generation: key.Generation, CommandID: "cancel-1", Kind: "cancel"}) {
		t.Fatal("native cancellation was not durably accepted")
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	var usage *protocol.Usage
	for _, delivery := range journal.PendingGoalDeliveries {
		if delivery.Kind == state.GoalDeliveryUsage && delivery.DeliveryID == nativeGoalUsageKey {
			usage = delivery.Usage
		}
	}
	if usage == nil || usage.CostBasis != protocol.CostUnknown || journal.TerminalState != "cancelled" || len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].CommandID != "cancel-1" {
		t.Fatalf("cancel/final race did not retain usage before cancellation receipt: %#v", journal)
	}
}

func TestGoalAdmissionRejectsProviderAccessBeforeNativeLaunch(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
	defer store.Close()
	profile := app.config.AgentProfiles[app.config.Runtime.AgentProfile]
	profile.ProviderAccess = true
	app.config.AgentProfiles[app.config.Runtime.AgentProfile] = profile
	controlClient.providerAccess = &protocol.ProviderAccess{
		Path:  "/api/v1/provider-actions",
		Token: "provider-token",
		Grants: []protocol.ProviderGrant{{
			ResourceID: admission.Subject.ResourceID,
			Provider:   "github",
			Kind:       "repository",
			Operations: []string{"resource.sync"},
		}},
	}

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	app.workers.Wait()
	if len(session.calls) != 0 {
		t.Fatalf("provider access started native lifecycle: %#v", session.calls)
	}
	if workspace := app.workspace.(*fakeWorkspace); workspace.subject != (protocol.Subject{}) {
		t.Fatalf("provider access prepared a Goal workspace: %#v", workspace.subject)
	}
	sessions, err := store.ListGoalSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Fatalf("provider access persisted Goal session state: %#v", sessions)
	}
	if len(controlClient.calls) != 0 {
		t.Fatalf("provider access called Goal control: %#v", controlClient.calls)
	}
	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if journal.WorkspaceRecoveryRequired || journal.WorkspacePath != "" {
		t.Fatalf("provider access caused workspace side effects: recovery %v, path %q", journal.WorkspaceRecoveryRequired, journal.WorkspacePath)
	}
	if strings.Contains(string(encoded), "provider-token") {
		t.Fatalf("provider access leaked into native run journal: %s", encoded)
	}
	var failure map[string]string
	if err := json.Unmarshal(journal.PendingTransitions[0].Payload, &failure); err != nil {
		t.Fatal(err)
	}
	if failure["stage"] != "goal_admission" || !strings.Contains(failure["error"], "provider_access") {
		t.Fatalf("failure = %#v, want provider-access admission rejection", failure)
	}
}

func TestGoalAdmissionLeavesProviderAccessNilForNativeHarness(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	gate := make(chan struct{})
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), gate)
	defer store.Close()

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	select {
	case <-session.turnStarted:
	case <-time.After(time.Second):
		t.Fatal("native turn did not start")
	}
	if session.request.ProviderAccess != nil {
		t.Fatalf("native start provider access = %#v, want nil", session.request.ProviderAccess)
	}
	close(gate)
	app.workers.Wait()
}

func runAdmissionAssignment(t *testing.T, input json.RawMessage) (state.RunJournal, int) {
	t.Helper()
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	control := &admissionClaimControl{
		fakeControl: &fakeControl{},
		work:        protocol.Work{Goal: "goal", Input: input},
	}
	capabilities, err := harness.NewGenericAdapter().Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	starts := 0
	value := testConfig(t)
	value.Runtime.RepositoryResourceID = "00000000-0000-4000-8000-000000000005"
	daemon := &daemon{
		config:              value,
		store:               store,
		control:             control,
		workspace:           &fakeWorkspace{},
		harnessCapabilities: capabilities,
		start: func(context.Context, execution.Invocation, execution.Sink) (Process, error) {
			starts++
			return nil, errors.New("generic launch must not be attempted for a Goal admission")
		},
		options:      options{newID: ids(), clock: time.Now},
		runtimeID:    "runtime-1",
		runtimeEpoch: 1,
		running:      make(map[state.RunKey]*runningRun),
		slots:        make(chan struct{}, 1),
	}
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: control.work})
	daemon.workers.Wait()
	journal, err := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "terminal_pending" || journal.TerminalState != "failed" || len(journal.PendingTransitions) != 1 {
		t.Fatalf("journal = %+v, want durable failed terminal transition", journal)
	}
	return journal, starts
}

func nativeAdmissionDaemon(t *testing.T, admission protocol.Admission, result protocol.TaskResult, waitGate <-chan struct{}) (*daemon, *state.Store, *fakeNativeGoalSession, *nativeAdmissionControl) {
	return nativeAdmissionDaemonForHarness(t, admission, result, waitGate, harness.KindCodex, config.RuntimeHarnessCodex, verifiedCodexCapabilities())
}

func saveAvailablePiGoalSession(t *testing.T, store *state.Store, admission protocol.Admission, controlSessionID, nativeFilename string, capabilities harness.Capabilities) state.GoalSessionJournal {
	t.Helper()
	localHandleID := "00000000-0000-4000-8000-000000000088"
	sourceBindingID := "00000000-0000-4000-8000-000000000089"
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: localHandleID}
	intent := state.GoalSessionLaunchIntent{
		LaunchIntentID: "00000000-0000-4000-8000-000000000087", GoalID: admission.GoalID, GoalRevision: admission.GoalRevision,
		WorkItemID: admissionWorkItemIDValue(admission.WorkItemID), TaskID: "task-source", RunID: "run-source", Generation: 7, AdmissionID: "admission-source",
		LocalHandleID: localHandleID, BindingID: sourceBindingID, MachineID: "machine-1", RuntimeID: "runtime-1", RuntimeEpoch: 1,
		HarnessKind: string(harness.KindPi), HarnessVersion: capabilities.NativeVersion, AdapterVersion: capabilities.ImplementationVersion,
		AdapterProtocolVersion: capabilities.ProtocolVersion, WorkspaceFingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		WorkspacePath: "C:\\workspace", RepositoryResourceID: admission.Subject.ResourceID, SessionMode: state.GoalSessionModeFresh,
	}
	if _, err := store.SaveGoalSessionLaunchIntent(intent); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionLaunchStarted(sessionKey); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PersistGoalSessionHandle(sessionKey, state.GoalSessionHandle{NativeSessionID: "native-retained", NativeSessionFilename: nativeFilename}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.PersistGoalSessionControlAttachment(sessionKey, controlSessionID, sourceBindingID, "00000000-0000-4000-8000-000000000092", false); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkGoalSessionStoppedPending(sessionKey); err != nil {
		t.Fatal(err)
	}
	deliveryDigest := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := store.LoadJournal(state.RunKey{RunID: "run-source", Generation: 7}); err == nil {
		queued, queueErr := store.QueueGoalSessionStopped(state.RunKey{RunID: "run-source", Generation: 7}, state.GoalSessionStoppedDelivery{
			SessionID: controlSessionID, LocalHandleID: localHandleID, BindingID: sourceBindingID,
		})
		if queueErr != nil {
			t.Fatal(queueErr)
		}
		for _, delivery := range queued.PendingGoalDeliveries {
			if delivery.Kind == state.GoalDeliverySessionStopped && delivery.SessionStopped != nil && delivery.SessionStopped.BindingID == sourceBindingID {
				deliveryDigest = delivery.PayloadDigest
				break
			}
		}
	}
	available, err := store.MarkGoalSessionAvailable(sessionKey, state.GoalSessionStopCertificate{
		RunID: "run-source", Generation: 7, SessionID: controlSessionID, LocalHandleID: localHandleID, BindingID: sourceBindingID,
		DeliveryDigest: deliveryDigest, ReceiptID: "00000000-0000-4000-8000-000000000093",
	})
	if err != nil {
		t.Fatal(err)
	}
	return available
}

func rebindAvailablePiGoalSession(t *testing.T, store *state.Store, source state.GoalSessionJournal, key state.RunKey, admission protocol.Admission, bindingID string, capabilities harness.Capabilities) state.GoalSessionJournal {
	t.Helper()
	rebound, err := store.RebindGoalSessionForResume(source.Key(), state.GoalSessionResumeRebind{
		RunID: key.RunID, Generation: key.Generation, TaskID: "task-1", AdmissionID: admission.AdmissionID, BindingID: bindingID,
		ExactStopCertificate: *source.StopCertificate,
		Compatibility: state.GoalSessionCompatibility{
			MachineID: "machine-1", RuntimeID: "runtime-1", RuntimeEpoch: 1, HarnessKind: string(harness.KindPi),
			HarnessVersion: capabilities.NativeVersion, AdapterVersion: capabilities.ImplementationVersion, AdapterProtocolVersion: capabilities.ProtocolVersion,
			WorkspaceFingerprint: source.WorkspaceFingerprint, RepositoryResourceID: admission.Subject.ResourceID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return rebound
}

func saveClaimedGoalRun(t *testing.T, store *state.Store, key state.RunKey) {
	t.Helper()
	claimID := "claim-" + key.RunID
	if _, err := store.SaveClaimIntent(state.ClaimIntent{
		Key: key, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1, ClaimID: claimID,
		Work: protocol.Work{Goal: "g"}, WorkspaceBindingKey: "local",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveClaimGrant(key, protocol.ClaimResponse{
		RunID: key.RunID, Generation: key.Generation, ClaimID: claimID, LeaseToken: "lease-" + key.RunID,
		LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"},
	}); err != nil {
		t.Fatal(err)
	}
}

func nativeAdmissionDaemonForHarness(t *testing.T, admission protocol.Admission, result protocol.TaskResult, waitGate <-chan struct{}, kind harness.Kind, configKind string, capabilities harness.Capabilities) (*daemon, *state.Store, *fakeNativeGoalSession, *nativeAdmissionControl) {
	t.Helper()
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	originalFingerprint := workspaceFingerprint
	workspaceFingerprint = func(context.Context, workspace.Prepared) (string, error) {
		return "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", nil
	}
	t.Cleanup(func() { workspaceFingerprint = originalFingerprint })
	session := &fakeNativeGoalSession{
		handle:      harness.NativeSessionHandle{ID: "native-thread-1"},
		result:      harness.TaskResult{Kind: harness.ResultSucceeded, Summary: result.Summary, Semantic: &result},
		waitGate:    waitGate,
		turnStarted: make(chan struct{}),
	}
	adapter := &fakeNativeGoalAdapter{session: session, capabilities: capabilities}
	registry := harness.NewRegistry()
	if err := registry.Register(kind, adapter); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(admission)
	if err != nil {
		t.Fatal(err)
	}
	controlClient := &nativeAdmissionControl{fakeControl: &fakeControl{}, work: protocol.Work{Goal: "implement the admitted change", Input: input}, admission: admission}
	value := testConfig(t)
	value.Runtime.HarnessKind = configKind
	value.Runtime.HarnessVersion = capabilities.NativeVersion
	value.Runtime.AdapterVersion = capabilities.ImplementationVersion
	value.Runtime.AdapterProtocolVersion = capabilities.ProtocolVersion
	value.Runtime.RepositoryResourceID = admission.Subject.ResourceID
	app := &daemon{
		config:              value,
		store:               store,
		control:             controlClient,
		workspace:           &fakeWorkspace{},
		harnessRegistry:     registry,
		harnessCapabilities: capabilities,
		options:             options{newID: ids(), clock: time.Now},
		machineID:           "machine-1",
		runtimeID:           "runtime-1",
		runtimeEpoch:        1,
		running:             make(map[state.RunKey]*runningRun),
		slots:               make(chan struct{}, 1),
	}
	return app, store, session, controlClient
}

func verifiedCodexCapabilities() harness.Capabilities {
	return verifiedNativeCapabilities(harness.KindCodex)
}

func verifiedNativeCapabilities(kind harness.Kind) harness.Capabilities {
	return harness.Capabilities{
		Kind:                  kind,
		NativeVersion:         "0.153.4",
		ImplementationVersion: "symmetry-daemon:test",
		ProtocolVersion:       1,
		VersionKnown:          true,
		TransportVerified:     true,
		Verified:              true,
		Start:                 true,
		Events:                true,
		Cancel:                true,
		Guidance:              harness.GuidanceUnsupported,
		Pause:                 harness.PauseUnsupported,
		Usage:                 harness.UsageUnknown,
	}
}

func validNativeTaskResult(t *testing.T, admission protocol.Admission) protocol.TaskResult {
	t.Helper()
	subjectHash, err := admission.Subject.Hash()
	if err != nil {
		t.Fatal(err)
	}
	return protocol.TaskResult{
		SchemaVersion: protocol.TaskResultSchemaVersion,
		ResultID:      "00000000-0000-4000-8000-000000000006",
		Kind:          protocol.TaskResultProgress,
		Summary:       "made bounded progress",
		Subject:       admission.Subject,
		SubjectHash:   subjectHash,
		EvidenceRefs:  []string{},
		Diagnostics:   []protocol.Diagnostic{},
	}
}

type nativeAdmissionControl struct {
	*fakeControl
	work             protocol.Work
	admission        protocol.Admission
	providerAccess   *protocol.ProviderAccess
	harnessSessionID *string
	harnessBindingID *string
	attachSessionID  string
	attachErr        error
	stopReceiptID    string
	calls            []string
}

func (client *nativeAdmissionControl) Claim(_ context.Context, runID string, request protocol.ClaimRequest) (protocol.ClaimResponse, error) {
	client.claimCalls++
	return protocol.ClaimResponse{RunID: runID, TaskID: "task-1", Generation: request.Generation, ClaimID: request.ClaimID, LeaseToken: "lease", LeaseExpiresAt: time.Now().Add(time.Minute), Work: client.work, ProviderAccess: client.providerAccess, HarnessSessionID: client.harnessSessionID, HarnessBindingID: client.harnessBindingID}, nil
}

func (client *nativeAdmissionControl) AttachHarnessSession(_ context.Context, runID string, request control.GoalSessionAttachRequest) (control.GoalSessionReceipt, error) {
	client.calls = append(client.calls, "attach")
	if client.attachErr != nil {
		return control.GoalSessionReceipt{}, client.attachErr
	}
	bindingID := "00000000-0000-4000-8000-000000000009"
	if request.BindingID != nil {
		bindingID = *request.BindingID
	}
	sessionID := client.attachSessionID
	if sessionID == "" {
		sessionID = "00000000-0000-4000-8000-000000000010"
	}
	receipt := control.GoalSessionReceipt{
		ID: sessionID, AttachmentReceiptID: "00000000-0000-4000-8000-000000000011", GoalID: client.admission.GoalID, TaskID: "task-1", RunID: runID, ActiveRunID: runID,
		MachineID: "machine-1", RuntimeID: request.RuntimeID, RepositoryResourceID: client.admission.Subject.ResourceID, LocalHandleID: request.LocalHandleID,
		BindingID:   bindingID,
		HarnessKind: request.HarnessKind, HarnessVersion: request.HarnessVersion, AdapterVersion: request.AdapterVersion,
		WorkspaceFingerprint: request.WorkspaceFingerprint, Workspace: request.Workspace, State: state.GoalSessionStateBusy,
	}
	return receipt, nil
}

func (client *nativeAdmissionControl) MarkHarnessSessionStopped(_ context.Context, runID string, request control.GoalSessionStoppedRequest) (control.GoalSessionStoppedReceipt, error) {
	client.calls = append(client.calls, "stopped")
	receiptID := client.stopReceiptID
	if receiptID == "" {
		receiptID = "00000000-0000-4000-8000-000000000011"
	}
	return control.GoalSessionStoppedReceipt{
		ReceiptID: receiptID, RunID: runID, SessionID: request.SessionID,
		LocalHandleID: request.LocalHandleID, BindingID: request.BindingID, State: state.GoalSessionStateAvailable,
		LockVersion: 1,
	}, nil
}

func (client *nativeAdmissionControl) FetchRunContext(_ context.Context, runID string, fence protocol.Fence) (control.GoalRunContext, error) {
	client.calls = append(client.calls, "context")
	sessionID := client.attachSessionID
	if sessionID == "" {
		sessionID = "00000000-0000-4000-8000-000000000010"
	}
	return control.GoalRunContext{
		GoalID: client.admission.GoalID, TaskID: "task-1", RunID: runID, Generation: fence.Generation, SessionID: &sessionID,
		Context: control.GoalContextSnapshot{
			SnapshotID: client.admission.ContextSnapshotID, GoalID: client.admission.GoalID, GoalRevision: client.admission.GoalRevision,
			WorkItemID: client.admission.WorkItemID, ContentHash: client.admission.ContextHash, Subject: client.admission.Subject,
			WorkContract: control.WorkContract{Purpose: string(client.admission.Purpose)},
		},
	}, nil
}

func (client *nativeAdmissionControl) AppendEvidence(_ context.Context, _ string, _ protocol.Fence, _ protocol.Evidence) (control.GoalEvidenceReceipt, error) {
	client.calls = append(client.calls, "evidence")
	return control.GoalEvidenceReceipt{}, nil
}

func (client *nativeAdmissionControl) RecordUsage(_ context.Context, _ string, _ protocol.Fence, _ protocol.Usage) (control.GoalUsageReceipt, error) {
	client.calls = append(client.calls, "usage")
	return control.GoalUsageReceipt{}, nil
}

type fakeNativeGoalAdapter struct {
	session      *fakeNativeGoalSession
	capabilities harness.Capabilities
	probeErr     error
}

func (adapter *fakeNativeGoalAdapter) Probe(context.Context) (harness.Capabilities, error) {
	return adapter.capabilities, adapter.probeErr
}

func (adapter *fakeNativeGoalAdapter) Start(_ context.Context, request harness.StartRequest, sink harness.EventSink) (harness.Session, error) {
	adapter.session.recordCall("start")
	if adapter.session.startErr != nil {
		return nil, adapter.session.startErr
	}
	if request.PersistProcess != nil {
		pid, identity := adapter.session.ProcessDetails()
		if err := request.PersistProcess(pid, identity); err != nil {
			return nil, err
		}
	}
	adapter.session.request = request
	adapter.session.sink = sink
	return adapter.session, nil
}

type fakeNativeGoalSession struct {
	callsMu         sync.Mutex
	calls           []string
	request         harness.StartRequest
	turnRequest     harness.TurnRequest
	sink            harness.EventSink
	handle          harness.NativeSessionHandle
	result          harness.TaskResult
	waitGate        <-chan struct{}
	finalWaitGate   <-chan struct{}
	turnReturnGate  <-chan struct{}
	turnStarted     chan struct{}
	waitEntered     chan struct{}
	processPID      int
	processIdentity string
	startErr        error
	closeErr        error
	closeEntered    chan struct{}
	onControl       func()
	waitTurnDone    bool
}

func (session *fakeNativeGoalSession) recordCall(call string) {
	session.callsMu.Lock()
	session.calls = append(session.calls, call)
	session.callsMu.Unlock()
}

func (session *fakeNativeGoalSession) ProcessDetails() (int, string) {
	session.recordCall("details")
	pid, identity := session.processPID, session.processIdentity
	if pid == 0 {
		pid = 41
	}
	if identity == "" {
		identity = "native:41"
	}
	return pid, identity
}

func (session *fakeNativeGoalSession) Open(context.Context) (harness.NativeSessionHandle, error) {
	session.recordCall("open")
	return session.handle, nil
}

func (session *fakeNativeGoalSession) StartTurn(ctx context.Context, request harness.TurnRequest) error {
	session.recordCall("start_turn")
	session.callsMu.Lock()
	session.turnRequest = request
	session.callsMu.Unlock()
	close(session.turnStarted)
	for _, event := range []harness.Event{
		{Kind: harness.EventSessionStarted, At: time.Now().UTC(), Payload: json.RawMessage(`{"thread_id":"native-thread-1"}`)},
		{Kind: harness.EventMessageDelta, At: time.Now().UTC(), Payload: json.RawMessage(`{"delta":"working"}`)},
	} {
		if err := session.sink.Handle(ctx, event); err != nil {
			return err
		}
	}
	payload, err := json.Marshal(session.result.Semantic)
	if err != nil {
		return err
	}
	if err := session.sink.Handle(ctx, harness.Event{Kind: harness.EventTaskResult, At: time.Now().UTC(), Payload: payload}); err != nil {
		return err
	}
	if session.turnReturnGate == nil {
		return nil
	}
	select {
	case <-session.turnReturnGate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (session *fakeNativeGoalSession) Control(_ context.Context, request harness.ControlRequest) (harness.ControlReceipt, error) {
	session.recordCall("control:" + string(request.Kind))
	if session.onControl != nil {
		session.onControl()
	}
	return harness.ControlReceipt{CommandID: request.CommandID, Kind: request.Kind, Outcome: harness.ControlApplied, Capability: harness.CapabilityCancel}, nil
}

func (session *fakeNativeGoalSession) Wait(ctx context.Context) (harness.TaskResult, error) {
	session.recordCall("wait")
	if session.waitEntered != nil {
		session.waitEntered <- struct{}{}
	}
	session.callsMu.Lock()
	waitTurnDone := session.waitTurnDone
	session.callsMu.Unlock()
	waitGate := session.waitGate
	if waitTurnDone {
		waitGate = session.finalWaitGate
	}
	if waitGate != nil {
		select {
		case <-waitGate:
		case <-ctx.Done():
			return harness.TaskResult{Kind: harness.ResultCancelled}, ctx.Err()
		}
	}
	return session.result, nil
}

func (session *fakeNativeGoalSession) WaitTurn(ctx context.Context) error {
	session.recordCall("wait_turn")
	var waitErr error
	if session.waitGate != nil {
		select {
		case <-session.waitGate:
		case <-ctx.Done():
			waitErr = ctx.Err()
		}
	}
	session.callsMu.Lock()
	session.waitTurnDone = true
	session.callsMu.Unlock()
	return waitErr
}

func (session *fakeNativeGoalSession) Close(context.Context) error {
	session.recordCall("close")
	if session.closeEntered != nil {
		session.closeEntered <- struct{}{}
	}
	return session.closeErr
}

func validAdmissionInput() json.RawMessage {
	return json.RawMessage(`{"schema_version":"symmetry.admission.v1","admission_id":"00000000-0000-4000-8000-000000000001","goal_id":"00000000-0000-4000-8000-000000000002","goal_revision":1,"work_item_id":"00000000-0000-4000-8000-000000000003","purpose":"implement","context_snapshot_id":"00000000-0000-4000-8000-000000000004","context_hash":"sha256:0000000000000000000000000000000000000000000000000000000000000000","model_profile":"local","session_mode":"fresh","requested_session_id":null,"subject":{"resource_id":"00000000-0000-4000-8000-000000000005","commit":"0000000000000000000000000000000000000000","tree_digest":"sha256:0000000000000000000000000000000000000000000000000000000000000000"},"limits":{"max_turns":1,"deadline_at":"2026-12-31T00:00:00Z","max_cost_microusd":null},"validation_of_task_id":null,"provider_scope":null}`)
}

type admissionClaimControl struct {
	*fakeControl
	work protocol.Work
}

func (client *admissionClaimControl) Claim(_ context.Context, runID string, request protocol.ClaimRequest) (protocol.ClaimResponse, error) {
	client.claimCalls++
	return protocol.ClaimResponse{
		RunID: runID, TaskID: "task-1", Generation: request.Generation, ClaimID: request.ClaimID,
		LeaseToken: "lease", LeaseExpiresAt: time.Now().Add(time.Minute), Work: client.work,
	}, nil
}

type codexCommandFixtures struct {
	responses map[string][]byte
}

func (runner codexCommandFixtures) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	key := ""
	for index, arg := range args {
		if index > 0 {
			key += " "
		}
		key += arg
	}
	response, ok := runner.responses[key]
	if !ok {
		return nil, errors.New("fixture command not found")
	}
	return response, nil
}
