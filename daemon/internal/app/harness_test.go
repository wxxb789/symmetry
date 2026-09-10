package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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
	if adapter.Operations.Resume || adapter.Operations.ApprovalResponse || adapter.Operations.HardCostLimit ||
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
			value.Runtime.HarnessVersion = "unavailable"
			value.Runtime.AdapterVersion = "symmetry-daemon:unavailable"
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
		})
	}
}

func TestRegistrationWireIncludesAdapterObject(t *testing.T) {
	runtime := config.Runtime{
		RuntimeKey: "default", Name: "runtime", Capacity: 1, AgentProfile: "default", Workspace: "primary",
		HarnessKind: config.RuntimeHarnessGeneric, HarnessVersion: "legacy", AdapterVersion: "legacy", AdapterProtocolVersion: 1,
	}
	capabilities := harness.UnsupportedCapabilities(harness.KindGeneric, "test")
	capabilities.Verified = true
	capabilities.Start = true
	capabilities.Events = true
	capabilities.Cancel = true
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

func TestHandoffAdmissionFailsBeforeWorkspaceAndNativeSessionSideEffects(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	admission.SessionMode = protocol.SessionModeHandoff

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

	if got, want := session.calls, []string{"start", "details", "open", "start_turn", "wait_turn", "close", "wait"}; !sameStrings(got, want) {
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
	if err != nil || len(sessions) != 1 || sessions[0].SessionState != state.GoalSessionStateClosed || sessions[0].LaunchState != state.GoalSessionLaunchStateClosed {
		t.Fatalf("Goal session journals = %#v, error = %v", sessions, err)
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

	if got, want := session.calls, []string{"start", "details", "open", "close"}; !sameStrings(got, want) {
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
	if got, want := session.calls, []string{"start", "details", "open", "close"}; !sameStrings(got, want) {
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
	if got, want := session.calls, []string{"wait_turn", "close", "wait"}; !sameStrings(got, want) {
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
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), gate)
	defer store.Close()

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
	close(gate)
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
	adapter := &fakeNativeGoalAdapter{session: session, capabilities: verifiedCodexCapabilities()}
	registry := harness.NewRegistry()
	if err := registry.Register(harness.KindCodex, adapter); err != nil {
		t.Fatal(err)
	}
	input, err := json.Marshal(admission)
	if err != nil {
		t.Fatal(err)
	}
	controlClient := &nativeAdmissionControl{fakeControl: &fakeControl{}, work: protocol.Work{Goal: "implement the admitted change", Input: input}, admission: admission}
	value := testConfig(t)
	value.Runtime.HarnessKind = config.RuntimeHarnessCodex
	value.Runtime.HarnessVersion = "0.153.4"
	value.Runtime.AdapterVersion = "symmetry-daemon:test"
	value.Runtime.AdapterProtocolVersion = 1
	value.Runtime.RepositoryResourceID = admission.Subject.ResourceID
	app := &daemon{
		config:              value,
		store:               store,
		control:             controlClient,
		workspace:           &fakeWorkspace{},
		harnessRegistry:     registry,
		harnessCapabilities: verifiedCodexCapabilities(),
		options:             options{newID: ids(), clock: time.Now},
		runtimeID:           "runtime-1",
		runtimeEpoch:        1,
		running:             make(map[state.RunKey]*runningRun),
		slots:               make(chan struct{}, 1),
	}
	return app, store, session, controlClient
}

func verifiedCodexCapabilities() harness.Capabilities {
	return harness.Capabilities{
		Kind:                  harness.KindCodex,
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
	work           protocol.Work
	admission      protocol.Admission
	providerAccess *protocol.ProviderAccess
	calls          []string
}

func (client *nativeAdmissionControl) Claim(_ context.Context, runID string, request protocol.ClaimRequest) (protocol.ClaimResponse, error) {
	client.claimCalls++
	return protocol.ClaimResponse{RunID: runID, TaskID: "task-1", Generation: request.Generation, ClaimID: request.ClaimID, LeaseToken: "lease", LeaseExpiresAt: time.Now().Add(time.Minute), Work: client.work, ProviderAccess: client.providerAccess}, nil
}

func (client *nativeAdmissionControl) AttachHarnessSession(_ context.Context, _ string, request control.GoalSessionAttachRequest) (control.GoalSessionReceipt, error) {
	client.calls = append(client.calls, "attach")
	return control.GoalSessionReceipt{
		ID: "server-session-1", GoalID: client.admission.GoalID, TaskID: "task-1", RunID: "run-1", ActiveRunID: "run-1",
		RuntimeID: request.RuntimeID, RepositoryResourceID: client.admission.Subject.ResourceID, LocalHandleID: request.LocalHandleID,
		HarnessKind: request.HarnessKind, HarnessVersion: request.HarnessVersion, AdapterVersion: request.AdapterVersion,
		WorkspaceFingerprint: request.WorkspaceFingerprint, Workspace: request.Workspace, State: state.GoalSessionStateBusy,
	}, nil
}

func (client *nativeAdmissionControl) FetchRunContext(_ context.Context, _ string, _ protocol.Fence) (control.GoalRunContext, error) {
	client.calls = append(client.calls, "context")
	sessionID := "server-session-1"
	return control.GoalRunContext{
		GoalID: client.admission.GoalID, TaskID: "task-1", RunID: "run-1", Generation: 1, SessionID: &sessionID,
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
}

func (adapter *fakeNativeGoalAdapter) Probe(context.Context) (harness.Capabilities, error) {
	return adapter.capabilities, nil
}

func (adapter *fakeNativeGoalAdapter) Start(_ context.Context, request harness.StartRequest, sink harness.EventSink) (harness.Session, error) {
	adapter.session.calls = append(adapter.session.calls, "start")
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
	callsMu      sync.Mutex
	calls        []string
	request      harness.StartRequest
	sink         harness.EventSink
	handle       harness.NativeSessionHandle
	result       harness.TaskResult
	waitGate     <-chan struct{}
	turnStarted  chan struct{}
	startErr     error
	closeErr     error
	waitTurnDone bool
}

func (session *fakeNativeGoalSession) recordCall(call string) {
	session.callsMu.Lock()
	session.calls = append(session.calls, call)
	session.callsMu.Unlock()
}

func (session *fakeNativeGoalSession) ProcessDetails() (int, string) {
	session.recordCall("details")
	return 41, "native:41"
}

func (session *fakeNativeGoalSession) Open(context.Context) (harness.NativeSessionHandle, error) {
	session.recordCall("open")
	return session.handle, nil
}

func (session *fakeNativeGoalSession) StartTurn(ctx context.Context, _ harness.TurnRequest) error {
	session.recordCall("start_turn")
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
	return session.sink.Handle(ctx, harness.Event{Kind: harness.EventTaskResult, At: time.Now().UTC(), Payload: payload})
}

func (session *fakeNativeGoalSession) Control(_ context.Context, request harness.ControlRequest) (harness.ControlReceipt, error) {
	session.recordCall("control:" + string(request.Kind))
	return harness.ControlReceipt{CommandID: request.CommandID, Kind: request.Kind, Outcome: harness.ControlApplied, Capability: harness.CapabilityCancel}, nil
}

func (session *fakeNativeGoalSession) Wait(ctx context.Context) (harness.TaskResult, error) {
	session.recordCall("wait")
	session.callsMu.Lock()
	waitTurnDone := session.waitTurnDone
	session.callsMu.Unlock()
	if session.waitGate != nil && !waitTurnDone {
		select {
		case <-session.waitGate:
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
