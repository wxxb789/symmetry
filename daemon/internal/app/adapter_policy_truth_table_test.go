package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/harness/claude"
	"github.com/wxxb789/symmetry/daemon/internal/harness/codex"
	"github.com/wxxb789/symmetry/daemon/internal/harness/omo"
	"github.com/wxxb789/symmetry/daemon/internal/harness/omp"
	"github.com/wxxb789/symmetry/daemon/internal/harness/opencode"
	"github.com/wxxb789/symmetry/daemon/internal/harness/pi"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

type truthTableOutcome struct {
	Decision  string
	Reason    string
	ErrorType string
	Sentence  string
}

type truthTableRow struct {
	Wire                     string
	Kind                     harness.Kind
	NativeGoal               bool
	UnavailableProbe         bool
	ProviderAccessMinimal    truthTableOutcome
	ProviderAccessAdvertised truthTableOutcome
	ProviderAccessProduction truthTableOutcome
	ResumeMinimal            truthTableOutcome
	ResumeAdvertised         truthTableOutcome
	ResumeProduction         truthTableOutcome
}

const (
	truthAbsentCommand = "symmetry-a3-missing-harness-binary"
	truthDefaultReason = "capability is not verified"

	// Fail-closed production projection reasons, owned and pinned by each adapter
	// package; the table binds the daemon gate's observable sentence to them.
	truthCodexStartReason    = "codex version probe failed"
	truthClaudeStartReason   = "Claude Code executable is unavailable"
	truthPiStartReason       = "pi version probe failed"
	truthOMPStartReason      = "OMP executable/version/protocol has not been verified"
	truthOmOStartReason      = "OmO executable is unavailable"
	truthOpenCodeStartReason = "opencode version probe failed"

	truthGenericResumeReason = "generic process runner has no retained native session"

	truthPiPlatformRejectReason      = "pi native repository lifecycle is verified only on Windows; Linux admission needs the Linux v2 owner path (delegated cgroup-v2 parent with cross-UID peer inspection), which this build's evidence does not provide"
	truthPiPromotedProviderReason    = "pi provider bridge transport is observed without a verified Control broker operation"
	truthCodexPlatformRejectReason   = "Codex native repository lifecycle is verified only on Windows; Linux shell tool children detach into a new session that the process-group owner cannot contain"
	truthCodexPromotedProviderReason = "Codex native provider broker bridge is not verified"
	truthCodexPromotedResumeReason   = "Codex native resume is not verified"

	truthOmOPromotedProviderReason = "native relay inference was observed; Symmetry provider broker access is not verified"
	truthOmOUnpinnedReason         = "operation is not independently verified for the pinned OmO read/write profile"

	truthOpenCodePlatformRejectReason   = "OpenCode V2 2.4.1 is promoted on Windows only: Linux admission stays fail-closed until the dispatched linux-native-v2-acceptance opencode_native_health row is green under the Linux v2 owner; V1 is not supported"
	truthOpenCodeBlobRejectReason       = "opencode native support is bound to the exact tested executable blob; the resolved executable SHA-256 is unverified or does not match"
	truthOpenCodePromotedProviderReason = "OpenCode provider broker bridge is not verified"
	truthOpenCodePromotedResumeReason   = "OpenCode native resume is not verified"
)

var truthTableWires = []struct {
	Wire string
	Kind harness.Kind
}{
	{"generic", harness.KindGeneric},
	{"codex", harness.KindCodex},
	{"claude_code", harness.KindClaude},
	{"pi", harness.KindPi},
	{"omp", harness.KindOMP},
	{"omo", harness.KindOmO},
	{"opencode", harness.KindOpenCode},
}

func truthAllowedOutcome() truthTableOutcome {
	return truthTableOutcome{Decision: "allow", ErrorType: "nil"}
}

func truthRefusedOutcome(kind harness.Kind, capability harness.Capability, reason string) truthTableOutcome {
	return truthTableOutcome{
		Decision:  "refuse",
		ErrorType: "capability_error",
		Sentence:  fmt.Sprintf("%s unsupported harness capability %s: %s", kind, capability, reason),
	}
}

func truthResumeRefusedOutcome(kind harness.Kind, reason string) truthTableOutcome {
	outcome := truthRefusedOutcome(kind, harness.CapabilityResume, reason)
	outcome.Reason = string(protocol.TaskResultReasonResumeRejected)
	return outcome
}

func truthTableCheck(t *testing.T, name string, got, want truthTableOutcome) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s outcome = %+v, want %+v", name, got, want)
	}
}

func (outcome truthTableOutcome) token() string {
	reason := outcome.Reason
	if reason == "" {
		reason = "-"
	}
	return fmt.Sprintf("%s/%s/%s/%q", outcome.Decision, reason, outcome.ErrorType, outcome.Sentence)
}

func (row truthTableRow) line() string {
	return fmt.Sprintf(
		"wire=%s kind=%s native_goal=%t unavailable_probe=%t | provider_access.minimal=%s | provider_access.advertised=%s | provider_access.production=%s | resume.minimal=%s | resume.advertised=%s | resume.production=%s",
		row.Wire, row.Kind, row.NativeGoal, row.UnavailableProbe,
		row.ProviderAccessMinimal.token(), row.ProviderAccessAdvertised.token(), row.ProviderAccessProduction.token(),
		row.ResumeMinimal.token(), row.ResumeAdvertised.token(), row.ResumeProduction.token())
}

func truthTableOutcomeOf(err error) truthTableOutcome {
	if err == nil {
		return truthTableOutcome{Decision: "allow", ErrorType: "nil"}
	}
	outcome := truthTableOutcome{Decision: "refuse", ErrorType: "plain", Sentence: err.Error()}
	if reason, ok := taskResultFailureReason(err); ok {
		outcome.Reason = string(reason)
	}
	var capability *harness.CapabilityError
	if errors.As(err, &capability) {
		outcome.ErrorType = "capability_error"
	}
	return outcome
}

func truthTableAdmission(t *testing.T, mode protocol.SessionMode) protocol.Admission {
	t.Helper()
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = present %t, error %v", present, err)
	}
	admission.SessionMode = mode
	return admission
}

func truthTableCall(t *testing.T, mode protocol.SessionMode, capabilities harness.Capabilities, providerAccess *protocol.ProviderAccess) truthTableOutcome {
	t.Helper()
	return truthTableOutcomeOf(admissionLaunchFailure(truthTableAdmission(t, mode), capabilities, providerAccess))
}

func truthTableMinimalProjection(t *testing.T, kind harness.Kind) harness.Capabilities {
	t.Helper()
	if kind == harness.KindGeneric {
		return truthTableProductionProjection(t, kind)
	}
	return testVerifiedNativeCapabilities(kind)
}

// truthTableAbsentRunner answers every probe command with a fast absent-executable error,
// so the codex and pi fail-closed projections are deterministic without starting a process.
type truthTableAbsentRunner struct{}

func (truthTableAbsentRunner) Run(context.Context, string, ...string) ([]byte, error) {
	return nil, errors.New("harness executable is absent")
}

func truthTableProductionProjection(t *testing.T, kind harness.Kind) harness.Capabilities {
	t.Helper()
	absentPath := filepath.Join(t.TempDir(), truthAbsentCommand)
	var capabilities harness.Capabilities
	switch kind {
	case harness.KindGeneric:
		probed, err := harness.NewGenericAdapter().Probe(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		capabilities = probed
	case harness.KindCodex:
		probed, _ := codex.NewAdapterWithRunner(truthAbsentCommand, truthTableAbsentRunner{}).Probe(context.Background())
		capabilities = probed
	case harness.KindClaude:
		probed, _ := claude.NewCandidateAdapter(absentPath).Probe(context.Background())
		capabilities = probed
	case harness.KindPi:
		probed, _ := pi.NewAdapterWithRunner(truthAbsentCommand, truthTableAbsentRunner{}).Probe(context.Background())
		capabilities = probed
	case harness.KindOMP:
		probed, _ := omp.NewAdapter(absentPath).Probe(context.Background())
		capabilities = probed
	case harness.KindOmO:
		probed, _ := omo.NewAdapter(truthAbsentCommand).Probe(context.Background())
		capabilities = probed
	case harness.KindOpenCode:
		// The composition root registers the native adapter on Windows and the
		// unavailable seam elsewhere; the production row models that wiring.
		if runtime.GOOS == "windows" {
			probed, _ := opencode.NewAdapterWithRunner(truthAbsentCommand, truthTableAbsentRunner{}).Probe(context.Background())
			capabilities = probed
		} else {
			probed, _ := harness.NewOpenCodeAdapter().Probe(context.Background())
			capabilities = probed
		}
	default:
		t.Fatalf("no production projection for kind %q", kind)
	}
	if capabilities.Kind != kind {
		t.Fatalf("production probe of %q returned kind %q", kind, capabilities.Kind)
	}
	return capabilities
}

func truthTableRows(t *testing.T) []truthTableRow {
	t.Helper()
	rows := make([]truthTableRow, 0, len(truthTableWires))
	for _, wire := range truthTableWires {
		kind, err := configuredHarnessKind(wire.Wire)
		if err != nil {
			t.Fatalf("configuredHarnessKind(%q) error = %v", wire.Wire, err)
		}
		if kind != wire.Kind {
			t.Fatalf("configuredHarnessKind(%q) = %q, want %q", wire.Wire, kind, wire.Kind)
		}
		minimal := truthTableMinimalProjection(t, kind)
		production := truthTableProductionProjection(t, kind)
		advertisedProvider := minimal
		advertisedProvider.ProviderAccess = true
		advertisedResume := minimal
		advertisedResume.Resume = true
		rows = append(rows, truthTableRow{
			Wire:                     wire.Wire,
			Kind:                     kind,
			NativeGoal:               kind.NativeGoal(),
			UnavailableProbe:         kind.UnavailableProbe(),
			ProviderAccessMinimal:    truthTableCall(t, protocol.SessionModeFresh, minimal, &protocol.ProviderAccess{}),
			ProviderAccessAdvertised: truthTableCall(t, protocol.SessionModeFresh, advertisedProvider, &protocol.ProviderAccess{}),
			ProviderAccessProduction: truthTableCall(t, protocol.SessionModeFresh, production, &protocol.ProviderAccess{}),
			ResumeMinimal:            truthTableCall(t, protocol.SessionModeResume, minimal, nil),
			ResumeAdvertised:         truthTableCall(t, protocol.SessionModeResume, advertisedResume, nil),
			ResumeProduction:         truthTableCall(t, protocol.SessionModeResume, production, nil),
		})
	}
	return rows
}

func truthTableExpectedRows() []truthTableRow {
	native := func(wire string, kind harness.Kind, unavailableProbe bool, startReason string) truthTableRow {
		return truthTableRow{
			Wire:                     wire,
			Kind:                     kind,
			NativeGoal:               true,
			UnavailableProbe:         unavailableProbe,
			ProviderAccessMinimal:    truthRefusedOutcome(kind, harness.CapabilityProviderAccess, truthDefaultReason),
			ProviderAccessAdvertised: truthAllowedOutcome(),
			ProviderAccessProduction: truthRefusedOutcome(kind, harness.CapabilityStart, startReason),
			ResumeMinimal:            truthResumeRefusedOutcome(kind, truthDefaultReason),
			ResumeAdvertised:         truthAllowedOutcome(),
			ResumeProduction:         truthRefusedOutcome(kind, harness.CapabilityStart, startReason),
		}
	}
	opencodeRow := native("opencode", harness.KindOpenCode, true, truthOpenCodeStartReason)
	if runtime.GOOS != "windows" {
		// Off Windows the composed seam refuses with the platform sentence, not the
		// native probe's absent-command reason.
		platformRefusal := truthRefusedOutcome(harness.KindOpenCode, harness.CapabilityStart, truthOpenCodePlatformRejectReason)
		opencodeRow.ProviderAccessProduction = platformRefusal
		opencodeRow.ResumeProduction = platformRefusal
	}
	return []truthTableRow{
		{
			Wire:                     "generic",
			Kind:                     harness.KindGeneric,
			NativeGoal:               false,
			UnavailableProbe:         false,
			ProviderAccessMinimal:    truthRefusedOutcome(harness.KindGeneric, harness.CapabilityProviderAccess, truthDefaultReason),
			ProviderAccessAdvertised: truthAllowedOutcome(),
			ProviderAccessProduction: truthRefusedOutcome(harness.KindGeneric, harness.CapabilityProviderAccess, truthDefaultReason),
			ResumeMinimal:            truthResumeRefusedOutcome(harness.KindGeneric, truthGenericResumeReason),
			ResumeAdvertised:         truthAllowedOutcome(),
			ResumeProduction:         truthResumeRefusedOutcome(harness.KindGeneric, truthGenericResumeReason),
		},
		native("codex", harness.KindCodex, false, truthCodexStartReason),
		native("claude_code", harness.KindClaude, true, truthClaudeStartReason),
		native("pi", harness.KindPi, true, truthPiStartReason),
		native("omp", harness.KindOMP, true, truthOMPStartReason),
		native("omo", harness.KindOmO, true, truthOmOStartReason),
		opencodeRow,
	}
}

func TestAdapterPolicyTruthTable(t *testing.T) {
	computed := truthTableRows(t)
	for _, row := range computed {
		t.Log(row.line())
	}
	expected := truthTableExpectedRows()
	if len(computed) != len(expected) {
		t.Fatalf("truth table has %d rows, want %d", len(computed), len(expected))
	}
	for index := range expected {
		if !reflect.DeepEqual(computed[index], expected[index]) {
			t.Errorf("truth table row %d differs\n%s\n%s", index, computed[index].line(), expected[index].line())
		}
	}
}

type truthTableCodexFixtures struct {
	responses map[string][]byte
}

func (runner truthTableCodexFixtures) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	response, ok := runner.responses[strings.Join(args, " ")]
	if !ok {
		return nil, errors.New("fixture command not found")
	}
	return response, nil
}

func (truthTableCodexFixtures) SchemaDigest(context.Context, string) (string, error) {
	return harness.CodexTestedSchemaHash, nil
}

func (truthTableCodexFixtures) ExecutableDigest(context.Context, string) (string, error) {
	return codex.TestedExecutableHash, nil
}

func TestAdapterPolicyTruthTablePromotedPi(t *testing.T) {
	registry := harness.NewRegistry()
	outputs := map[string][]byte{
		"--version": []byte(pi.TestedVersion + "\n"),
		"--help":    []byte("Options:\n  --mode <mode>  Output mode: text (default), json, or rpc\n"),
	}
	if err := registry.Register(harness.KindPi, pi.NewAdapterWithRunner("pi-fixture", nativeProbeDigestFixtures{responses: outputs, digest: pi.TestedExecutableHash})); err != nil {
		t.Fatal(err)
	}
	capabilities, _ := registry.Probe(context.Background(), harness.KindPi)
	wantPromoted := runtime.GOOS == "windows"
	if capabilities.Verified != wantPromoted || capabilities.Resume != wantPromoted {
		t.Fatalf("pi projection verified=%t resume=%t, want promoted=%t: %+v", capabilities.Verified, capabilities.Resume, wantPromoted, capabilities)
	}
	advertisedProvider := capabilities
	advertisedProvider.ProviderAccess = true
	advertisedResume := capabilities
	advertisedResume.Resume = true
	providerAccess := &protocol.ProviderAccess{}
	if wantPromoted {
		truthTableCheck(t, "promoted pi provider_access", truthTableCall(t, protocol.SessionModeFresh, capabilities, providerAccess),
			truthRefusedOutcome(harness.KindPi, harness.CapabilityProviderAccess, truthPiPromotedProviderReason))
		truthTableCheck(t, "promoted pi provider_access advertised", truthTableCall(t, protocol.SessionModeFresh, advertisedProvider, providerAccess), truthAllowedOutcome())
		truthTableCheck(t, "promoted pi resume", truthTableCall(t, protocol.SessionModeResume, capabilities, nil), truthAllowedOutcome())
		truthTableCheck(t, "promoted pi resume advertised", truthTableCall(t, protocol.SessionModeResume, advertisedResume, nil), truthAllowedOutcome())
		return
	}
	if runtime.GOOS != "linux" {
		t.Skipf("pi promotion is deterministic only on windows; the fail-closed branch is asserted on linux")
	}
	if reason := capabilities.Unsupported[string(harness.CapabilityProviderAccess)]; reason != truthPiPromotedProviderReason {
		t.Fatalf("fail-closed pi provider_access reason = %q, want the promoted reason preserved", reason)
	}
	startFailure := truthRefusedOutcome(harness.KindPi, harness.CapabilityStart, truthPiPlatformRejectReason)
	truthTableCheck(t, "fail-closed pi provider_access", truthTableCall(t, protocol.SessionModeFresh, capabilities, providerAccess), startFailure)
	truthTableCheck(t, "fail-closed pi provider_access advertised", truthTableCall(t, protocol.SessionModeFresh, advertisedProvider, providerAccess), startFailure)
	truthTableCheck(t, "fail-closed pi resume", truthTableCall(t, protocol.SessionModeResume, capabilities, nil), startFailure)
	truthTableCheck(t, "fail-closed pi resume advertised", truthTableCall(t, protocol.SessionModeResume, advertisedResume, nil), startFailure)
}

func TestAdapterPolicyTruthTablePromotedCodex(t *testing.T) {
	registry := harness.NewRegistry()
	if err := registry.Register(harness.KindCodex, codex.NewAdapterWithRunner("codex-fixture", truthTableCodexFixtures{
		responses: map[string][]byte{
			"--version":         []byte("codex-cli " + codex.TestedVersion + "\n"),
			"app-server --help": []byte("app-server\nstdio://\n"),
		},
	})); err != nil {
		t.Fatal(err)
	}
	capabilities, _ := registry.Probe(context.Background(), harness.KindCodex)
	wantPromoted := runtime.GOOS == "windows"
	if capabilities.Verified != wantPromoted {
		t.Fatalf("codex projection verified=%t, want promoted=%t: %+v", capabilities.Verified, wantPromoted, capabilities)
	}
	advertisedProvider := capabilities
	advertisedProvider.ProviderAccess = true
	advertisedResume := capabilities
	advertisedResume.Resume = true
	providerAccess := &protocol.ProviderAccess{}
	if wantPromoted {
		truthTableCheck(t, "promoted codex provider_access", truthTableCall(t, protocol.SessionModeFresh, capabilities, providerAccess),
			truthRefusedOutcome(harness.KindCodex, harness.CapabilityProviderAccess, truthCodexPromotedProviderReason))
		truthTableCheck(t, "promoted codex provider_access advertised", truthTableCall(t, protocol.SessionModeFresh, advertisedProvider, providerAccess), truthAllowedOutcome())
		truthTableCheck(t, "promoted codex resume", truthTableCall(t, protocol.SessionModeResume, capabilities, nil),
			truthResumeRefusedOutcome(harness.KindCodex, truthCodexPromotedResumeReason))
		truthTableCheck(t, "promoted codex resume advertised", truthTableCall(t, protocol.SessionModeResume, advertisedResume, nil), truthAllowedOutcome())
		return
	}
	if runtime.GOOS != "linux" {
		t.Skipf("codex promotion is deterministic only on windows; the fail-closed branch is asserted on linux")
	}
	if reason := capabilities.Unsupported[string(harness.CapabilityResume)]; reason != truthCodexPromotedResumeReason {
		t.Fatalf("fail-closed codex resume reason = %q, want the promoted reason preserved", reason)
	}
	startFailure := truthRefusedOutcome(harness.KindCodex, harness.CapabilityStart, truthCodexPlatformRejectReason)
	truthTableCheck(t, "fail-closed codex provider_access", truthTableCall(t, protocol.SessionModeFresh, capabilities, providerAccess), startFailure)
	truthTableCheck(t, "fail-closed codex provider_access advertised", truthTableCall(t, protocol.SessionModeFresh, advertisedProvider, providerAccess), startFailure)
	truthTableCheck(t, "fail-closed codex resume", truthTableCall(t, protocol.SessionModeResume, capabilities, nil), startFailure)
	truthTableCheck(t, "fail-closed codex resume advertised", truthTableCall(t, protocol.SessionModeResume, advertisedResume, nil), startFailure)
}

// OmO is the one remaining native kind whose promotion is observable through an exported
// fixture seam; claude and omp promote only behind real pinned binaries and stay documented.
func TestAdapterPolicyTruthTablePromotedOmO(t *testing.T) {
	digest := omo.TestedWindowsExecutableHash
	if runtime.GOOS == "linux" {
		digest = omo.TestedLinuxExecutableHash
	}
	adapter := omo.NewAdapterWithRunner("omo-fixture", nativeProbeDigestFixtures{
		responses: map[string][]byte{
			"--version": []byte(omo.TestedVersionLine + "\n"),
			"--help":    []byte("Options:\n  --mode <json|rpc>  Output mode\n"),
		},
		digest: digest,
	})
	capabilities, err := adapter.Probe(context.Background())
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		if err == nil {
			t.Fatalf("omo probe on %s = %v, want the unpinned-platform refusal", runtime.GOOS, capabilities)
		}
		unpinned := truthRefusedOutcome(harness.KindOmO, harness.CapabilityStart, truthOmOUnpinnedReason)
		truthTableCheck(t, "unpinned omo provider_access", truthTableCall(t, protocol.SessionModeFresh, capabilities, &protocol.ProviderAccess{}), unpinned)
		truthTableCheck(t, "unpinned omo resume", truthTableCall(t, protocol.SessionModeResume, capabilities, nil), unpinned)
		return
	}
	if err != nil {
		t.Fatalf("promoted omo probe error = %v", err)
	}
	if !capabilities.Verified || !capabilities.Resume {
		t.Fatalf("omo projection verified=%t resume=%t, want both promoted", capabilities.Verified, capabilities.Resume)
	}
	advertisedProvider := capabilities
	advertisedProvider.ProviderAccess = true
	truthTableCheck(t, "promoted omo provider_access", truthTableCall(t, protocol.SessionModeFresh, capabilities, &protocol.ProviderAccess{}),
		truthRefusedOutcome(harness.KindOmO, harness.CapabilityProviderAccess, truthOmOPromotedProviderReason))
	truthTableCheck(t, "promoted omo resume", truthTableCall(t, protocol.SessionModeResume, capabilities, nil), truthAllowedOutcome())
	truthTableCheck(t, "promoted omo provider_access advertised", truthTableCall(t, protocol.SessionModeFresh, advertisedProvider, &protocol.ProviderAccess{}), truthAllowedOutcome())
}

// OpenCode promotes only the exact tested Windows blob; the seam pins both the
// promoted projections and the platform refusal the Linux rows carry.
func TestAdapterPolicyTruthTablePromotedOpenCode(t *testing.T) {
	adapter := opencode.NewAdapterWithRunner("opencode-fixture", nativeProbeDigestFixtures{
		responses: openCodeExactBlobProbeResponses(),
		digest:    opencode.TestedExecutableHash,
	})
	capabilities, err := adapter.Probe(context.Background())
	wantPromoted := runtime.GOOS == "windows"
	if wantPromoted {
		if err != nil {
			t.Fatalf("promoted opencode probe error = %v", err)
		}
		if !capabilities.Verified || !capabilities.Start || !capabilities.Events || !capabilities.Cancel || capabilities.Resume {
			t.Fatalf("opencode projection = %+v, want start/events/cancel only", capabilities)
		}
		truthTableCheck(t, "promoted opencode provider_access", truthTableCall(t, protocol.SessionModeFresh, capabilities, &protocol.ProviderAccess{}),
			truthRefusedOutcome(harness.KindOpenCode, harness.CapabilityProviderAccess, truthOpenCodePromotedProviderReason))
		truthTableCheck(t, "promoted opencode resume", truthTableCall(t, protocol.SessionModeResume, capabilities, nil),
			truthResumeRefusedOutcome(harness.KindOpenCode, truthOpenCodePromotedResumeReason))
		return
	}
	if runtime.GOOS != "linux" {
		t.Skipf("opencode promotion is deterministic only on windows; the fail-closed branch is asserted on linux")
	}
	if !errors.Is(err, harness.ErrHarnessUnavailable) {
		t.Fatalf("fail-closed opencode probe error = %v, want ErrHarnessUnavailable", err)
	}
	startFailure := truthRefusedOutcome(harness.KindOpenCode, harness.CapabilityStart, truthOpenCodePlatformRejectReason)
	truthTableCheck(t, "fail-closed opencode provider_access", truthTableCall(t, protocol.SessionModeFresh, capabilities, &protocol.ProviderAccess{}), startFailure)
	truthTableCheck(t, "fail-closed opencode resume", truthTableCall(t, protocol.SessionModeResume, capabilities, nil), startFailure)
}

// A foreign blob on the promoting platform must stay fail-closed under the blob binding.
func TestAdapterPolicyTruthTableOpenCodeRejectsForeignBlob(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the blob gate is only reachable where the platform gate admits")
	}
	adapter := opencode.NewAdapterWithRunner("opencode-fixture", nativeProbeDigestFixtures{
		responses: openCodeExactBlobProbeResponses(),
		digest:    "sha256:" + strings.Repeat("0", 64),
	})
	capabilities, err := adapter.Probe(context.Background())
	if !errors.Is(err, harness.ErrNativeUnverified) {
		t.Fatalf("foreign-blob opencode probe error = %v, want ErrNativeUnverified", err)
	}
	startFailure := truthRefusedOutcome(harness.KindOpenCode, harness.CapabilityStart, truthOpenCodeBlobRejectReason)
	truthTableCheck(t, "foreign-blob opencode provider_access", truthTableCall(t, protocol.SessionModeFresh, capabilities, &protocol.ProviderAccess{}), startFailure)
	truthTableCheck(t, "foreign-blob opencode resume", truthTableCall(t, protocol.SessionModeResume, capabilities, nil), startFailure)
}
