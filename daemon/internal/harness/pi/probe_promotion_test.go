package pi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

// piShimExecutableDigest is the recorded SHA-256 of the `mise` PATH shim for
// pi 1.5.2 on the verified Windows host. The shim prints the same `1.5.2` line
// and forwards to another file, so it is real foreign bytes rather than an
// invented value.
const piShimExecutableDigest = "sha256:53ca4fc6cfaf2b9b9a9635c54f17e5f4bf6ce599693dc67b17783bf382643850"

const piRPCHelpFixture = "Usage: pi [options]\n\nOptions:\n  --mode <mode>  Output mode: text (default), json, or rpc\n"

// probeDigestRunner is the deterministic probe seam: it declares the exact bytes
// the promotion gate must observe instead of letting a fixture hash whatever
// this host resolves for the same command name.
type probeDigestRunner struct {
	outputs map[string][]byte
	digest  string
	calls   []string
}

func (runner *probeDigestRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	runner.calls = append(runner.calls, key)
	output, ok := runner.outputs[key]
	if !ok {
		return nil, fmt.Errorf("unexpected probe invocation %q", key)
	}
	return output, nil
}

func (runner *probeDigestRunner) ExecutableDigest(context.Context, string) (string, error) {
	if runner.digest == "" {
		return "", errors.New("fixture declares no executable digest")
	}
	return runner.digest, nil
}

func piProvedEvidence() piProbeEvidence {
	return piProbeEvidence{Version: TestedVersion, VersionKnown: true, TransportVerified: true}
}

func piStagedOperations() []harness.Capability {
	return []harness.Capability{
		harness.CapabilityStart, harness.CapabilityEvents, harness.CapabilityCancel, harness.CapabilityResume,
	}
}

func piUnpromotedOperations() []string {
	return []string{
		string(harness.CapabilityHandoff),
		string(harness.CapabilityGuidance),
		string(harness.CapabilityPause),
		string(harness.CapabilityApprovalResponse),
		string(harness.CapabilityUsage),
		string(harness.CapabilityHardCostLimit),
		string(harness.CapabilityProviderAccess),
	}
}

func TestPiCapabilitiesForPlatformPromotesOnlyProvedOperations(t *testing.T) {
	tests := []struct {
		name        string
		platform    string
		evidence    piProbeEvidence
		hashMatched bool
		promoted    bool
		contractErr bool
		wantReason  string
	}{
		{name: "windows with the exact tested blob", platform: "windows", evidence: piProvedEvidence(), hashMatched: true, promoted: true},
		{name: "windows with a foreign blob", platform: "windows", evidence: piProvedEvidence(), wantReason: "the resolved executable SHA-256 is unverified or does not match"},
		{name: "linux with the exact tested blob", platform: "linux", evidence: piProvedEvidence(), hashMatched: true, wantReason: "verified only on Windows"},
		{name: "darwin with the exact tested blob", platform: "darwin", evidence: piProvedEvidence(), hashMatched: true, wantReason: "verified only on Windows"},
		{name: "windows without transport evidence", platform: "windows", evidence: piProbeEvidence{Version: TestedVersion, VersionKnown: true}, hashMatched: true, wantReason: "probe evidence is incomplete"},
		// Unreachable through the probe, and deliberately a contract failure:
		// transport evidence without a known version is a malformed projection
		// and must never be returned to a caller.
		{name: "windows without a known version", platform: "windows", evidence: piProbeEvidence{TransportVerified: true}, hashMatched: true, contractErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capabilities, err := piCapabilitiesForPlatform(test.platform, test.evidence, test.hashMatched)
			if test.contractErr {
				if err == nil || !strings.Contains(err.Error(), "pi capability contract") {
					t.Fatalf("malformed evidence returned %+v with error %v, want a capability-contract failure", capabilities, err)
				}
				if !reflect.DeepEqual(capabilities, harness.Capabilities{}) {
					t.Fatalf("malformed evidence returned a projection: %+v", capabilities)
				}
				return
			}
			if err := capabilities.Validate(); err != nil {
				t.Fatalf("Capabilities.Validate() error = %v", err)
			}
			if capabilities.Kind != harness.KindPi || capabilities.ProtocolVersion != 1 {
				t.Fatalf("capabilities = %+v, want a pi protocol 1 projection", capabilities)
			}
			promoted := capabilities.Verified && capabilities.Start && capabilities.Events && capabilities.Cancel && capabilities.Resume
			if promoted != test.promoted {
				t.Fatalf("promoted = %t, want %t: %+v (error %v)", promoted, test.promoted, capabilities, err)
			}
			if test.promoted {
				if err != nil {
					t.Fatalf("promoted projection error = %v, want nil", err)
				}
				if capabilities.ImplementationVersion != piImplementationVersion {
					t.Fatalf("implementation version = %q, want %q", capabilities.ImplementationVersion, piImplementationVersion)
				}
				for _, capability := range piStagedOperations() {
					if reason := capabilities.Unsupported[string(capability)]; strings.TrimSpace(reason) != "" {
						t.Fatalf("promoted capability %s carries reason %q", capability, reason)
					}
				}
			} else {
				if !errors.Is(err, harness.ErrNativeUnverified) {
					t.Fatalf("unpromoted projection error = %v, want ErrNativeUnverified", err)
				}
				if capabilities.ImplementationVersion != "" {
					t.Fatalf("unpromoted projection claimed implementation identity %q", capabilities.ImplementationVersion)
				}
				for _, capability := range piStagedOperations() {
					reason := capabilities.Unsupported[string(capability)]
					if !strings.Contains(reason, test.wantReason) {
						t.Fatalf("capability %s reason = %q, want it to contain %q", capability, reason, test.wantReason)
					}
				}
			}
			for _, capability := range piUnpromotedOperations() {
				if strings.TrimSpace(capabilities.Unsupported[capability]) == "" {
					t.Fatalf("unsupported capability %s carries no reason: %+v", capability, capabilities.Unsupported)
				}
			}
			if capabilities.Handoff || capabilities.ApprovalResponse || capabilities.HardCostLimit || capabilities.ProviderAccess ||
				capabilities.Guidance != harness.GuidanceUnsupported || capabilities.Pause != harness.PauseUnsupported || capabilities.Usage != harness.UsageUnknown {
				t.Fatalf("capabilities advertised unproved operations: %+v", capabilities)
			}
		})
	}
}

func TestProbePromotesOnlyThroughADeclaredExactDigest(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}
	outputs := map[string][]byte{
		"--version": []byte(TestedVersion + "\n"),
		"--help":    []byte(piRPCHelpFixture),
	}
	tests := []struct {
		name            string
		executable      string
		runner          CommandRunner
		promoted        bool
		digest          string
		executableKnown bool
	}{
		{
			name:            "declared tested blob",
			executable:      "pi",
			runner:          &probeDigestRunner{outputs: outputs, digest: TestedExecutableHash},
			promoted:        runtime.GOOS == "windows",
			digest:          TestedExecutableHash,
			executableKnown: true,
		},
		{
			name:       "declared mise shim blob",
			executable: "pi",
			runner:     &probeDigestRunner{outputs: outputs, digest: piShimExecutableDigest},
			digest:     piShimExecutableDigest, executableKnown: true,
		},
		{
			// The test executable always exists, so this control
			// fails the run if the gate ever falls back to hashing the resolved
			// file for a fixture that declared nothing.
			name:       "no declared digest",
			executable: executable,
			runner:     fakeCommandRunner{outputs: outputs},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := Probe(context.Background(), test.executable, test.runner)
			if err := result.Capabilities.Validate(); err != nil {
				t.Fatalf("Capabilities.Validate() error = %v", err)
			}
			if !result.VersionKnown || !result.TransportKnown {
				t.Fatalf("probe result = %+v, want the fixture version and transport", result)
			}
			if result.ExecutableDigest != test.digest || result.ExecutableHashKnown != test.executableKnown {
				t.Fatalf("executable digest = %q known=%t, want %q known=%t", result.ExecutableDigest, result.ExecutableHashKnown, test.digest, test.executableKnown)
			}
			promoted := result.Capabilities.Verified && result.Capabilities.Start &&
				result.Capabilities.Events && result.Capabilities.Cancel && result.Capabilities.Resume
			if promoted != test.promoted || result.NativeSessionVerified != test.promoted {
				t.Fatalf("promoted = %t native_session_verified = %t, want %t: %+v (error %v)",
					promoted, result.NativeSessionVerified, test.promoted, result.Capabilities, err)
			}
			if test.promoted {
				if err != nil {
					t.Fatalf("promoted probe error = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, harness.ErrNativeUnverified) {
				t.Fatalf("probe error = %v, want ErrNativeUnverified", err)
			}
		})
	}
}

func TestProbeRejectsUnknownOrUnanchoredVersionLines(t *testing.T) {
	tests := []struct {
		name             string
		version          string
		wantVersion      string
		wantVersionKnown bool
		wantMessage      string
	}{
		{name: "older pin", version: "0.87.1\n", wantVersion: "0.87.1", wantVersionKnown: true, wantMessage: "is not in the tested version set"},
		{name: "newer pin", version: "1.6.0\n", wantVersion: "1.6.0", wantVersionKnown: true, wantMessage: "is not in the tested version set"},
		{name: "shim banner", version: "mise: using pi 1.5.2\n", wantMessage: "unable to parse pi version"},
		{name: "suffix on the version line", version: "1.5.2 (symmetry shim)\n", wantMessage: "unable to parse pi version"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &probeDigestRunner{
				outputs: map[string][]byte{"--version": []byte(test.version), "--help": []byte("should not be called")},
				digest:  TestedExecutableHash,
			}
			result, err := Probe(context.Background(), "pi", runner)
			if !errors.Is(err, harness.ErrUnsupportedVersion) {
				t.Fatalf("Probe() error = %v, want ErrUnsupportedVersion", err)
			}
			if !strings.Contains(err.Error(), test.wantMessage) {
				t.Fatalf("Probe() error = %v, want it to contain %q", err, test.wantMessage)
			}
			if result.Version != test.wantVersion || result.VersionKnown != test.wantVersionKnown {
				t.Fatalf("probe version = %q known=%t, want %q known=%t", result.Version, result.VersionKnown, test.wantVersion, test.wantVersionKnown)
			}
			if result.TransportKnown || result.Capabilities.Verified || result.Capabilities.Start || result.Capabilities.Resume {
				t.Fatalf("probe result = %+v, want no transport or lifecycle claim", result)
			}
			if result.ExecutableHashKnown || result.ExecutableDigest != "" {
				t.Fatalf("probe resolved a digest before the version gate: %+v", result)
			}
			if got, want := runner.calls, []string{"--version"}; strings.Join(got, ",") != strings.Join(want, ",") {
				t.Fatalf("runner calls = %v, want %v", got, want)
			}
			if reason := result.Capabilities.Unsupported[string(harness.CapabilityStart)]; strings.TrimSpace(reason) == "" {
				t.Fatalf("unpromoted projection carries no start reason: %+v", result.Capabilities)
			}
		})
	}
}

func TestProbeRejectsExecutableWithoutRPCModeHelp(t *testing.T) {
	runner := &probeDigestRunner{
		outputs: map[string][]byte{
			"--version": []byte(TestedVersion + "\n"),
			"--help":    []byte("Usage: pi [options]\n\nOptions:\n  --mode <mode>  Output mode: text (default) or json\n"),
		},
		digest: TestedExecutableHash,
	}
	result, err := Probe(context.Background(), "pi", runner)
	if !errors.Is(err, harness.ErrNativeUnverified) {
		t.Fatalf("Probe() error = %v, want ErrNativeUnverified", err)
	}
	if !result.VersionKnown || result.TransportKnown || result.ExecutableHashKnown {
		t.Fatalf("probe result = %+v, want a known version without transport or digest evidence", result)
	}
	if result.Capabilities.Verified || result.Capabilities.Start || result.Capabilities.Events || result.Capabilities.Cancel {
		t.Fatalf("capabilities = %+v, want no advertised operation", result.Capabilities)
	}
	if got, want := runner.calls, []string{"--version", "--help"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("runner calls = %v, want %v", got, want)
	}
}

func TestPiTestdataManifestBindsRecordedDigests(t *testing.T) {
	directory := filepath.Join("..", "testdata", "pi", TestedVersion)
	encoded, err := os.ReadFile(filepath.Join(directory, "fixture-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Executable string            `json:"executable"`
		Version    string            `json:"version"`
		Fixtures   map[string]string `json:"fixtures"`
	}
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatalf("fixture manifest: %v", err)
	}
	if manifest.Executable != TestedExecutableHash {
		t.Fatalf("manifest executable = %q, want %q", manifest.Executable, TestedExecutableHash)
	}
	if manifest.Version != TestedVersion {
		t.Fatalf("manifest version = %q, want %q", manifest.Version, TestedVersion)
	}
	// The committed evidence set is part of the promotion binding, so a removed
	// fixture record must fail rather than silently shrink what the manifest
	// attests.
	required := []string{"version.txt", "help.txt", "lifecycle.jsonl", "compaction-continuation.jsonl"}
	if len(manifest.Fixtures) != len(required) {
		t.Fatalf("fixture manifest records %d fixtures, want exactly %d: %v", len(manifest.Fixtures), len(required), manifest.Fixtures)
	}
	for _, name := range required {
		if _, ok := manifest.Fixtures[name]; !ok {
			t.Fatalf("fixture manifest is missing the required %s record: %v", name, manifest.Fixtures)
		}
	}
	for name, want := range manifest.Fixtures {
		content, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatalf("fixture %s: %v", name, err)
		}
		sum := sha256.Sum256(content)
		if got := "sha256:" + hex.EncodeToString(sum[:]); got != want {
			t.Fatalf("fixture %s digest = %q, want %q", name, got, want)
		}
	}
	// The promoted version is derived from the committed version line through
	// the same parser the probe uses, not restated from the constant.
	version, err := os.ReadFile(filepath.Join(directory, "version.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if parsed := parseVersion(string(version)); parsed != TestedVersion {
		t.Fatalf("committed version line parses to %q, want %q", parsed, TestedVersion)
	}
	help, err := os.ReadFile(filepath.Join(directory, "help.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !hasRPCModeHelp(string(help)) {
		t.Fatal("committed help capture does not advertise the RPC transport gate")
	}
}
