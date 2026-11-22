package claude

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

func TestClaudeProbeKnownVersionAndHelpFailsClosed(t *testing.T) {
	runner := &fixtureRunner{responses: map[string][]byte{
		"--version": []byte(TestedVersion + " (Claude Code)\n"),
		"--help":    []byte("claude --print --output-format <format> text, json, or stream-json\n  --json-schema <schema>  JSON Schema for structured output validation.\n"),
	}}

	capabilities, err := probeCapabilities(context.Background(), `C:\Users\runneradmin\.local\bin\claude.exe`, runner)
	assertKnownVersionProjection(t, capabilities, err, knownVersionProjection{
		provider:  "Claude",
		kind:      harness.KindClaude,
		version:   TestedVersion,
		wantError: harness.ErrNativeUnverified,
		calls:     runner.calls,
		wantCalls: []string{"--version", "--help"},
	})
	// Resume and the interactive controls are this provider's own surface: the
	// shared projection above cannot speak for them.
	if capabilities.Resume || capabilities.Guidance != harness.GuidanceUnsupported || capabilities.Pause != harness.PauseUnsupported || capabilities.ApprovalResponse || capabilities.Usage != harness.UsageUnknown || capabilities.HardCostLimit {
		t.Fatalf("unsupported controls = %+v, want resume and every interactive control unsupported", capabilities)
	}
}

func TestClaudeProbeHelpWithoutJSONSchemaFailsClosed(t *testing.T) {
	runner := &fixtureRunner{responses: map[string][]byte{
		"--version": []byte(TestedVersion + " (Claude Code)\n"),
		"--help":    []byte("claude --print --output-format <format> text, json, or stream-json\n"),
	}}

	capabilities, err := probeCapabilities(context.Background(), "claude", runner)
	if !errors.Is(err, harness.ErrNativeUnverified) || !strings.Contains(err.Error(), "--json-schema") {
		t.Fatalf("probe error = %v, want ErrNativeUnverified naming --json-schema", err)
	}
	if capabilities.TransportVerified || capabilities.Verified || capabilities.Start {
		t.Fatalf("capabilities = %+v, want no transport evidence without --json-schema", capabilities)
	}
}

func TestClaudeProbeUnknownVersionFailsClosed(t *testing.T) {
	runner := &fixtureRunner{responses: map[string][]byte{
		"--version": []byte("2.1.259 (Claude Code)\n"),
		"--help":    []byte("should not be called"),
	}}

	capabilities, err := probeCapabilities(context.Background(), "claude", runner)
	if !errors.Is(err, harness.ErrUnsupportedVersion) {
		t.Fatalf("probe error = %v, want ErrUnsupportedVersion", err)
	}
	if capabilities.NativeVersion != "2.1.259" || !capabilities.VersionKnown || capabilities.TransportVerified || capabilities.Verified {
		t.Fatalf("capabilities = %+v, want known unsupported version without transport evidence", capabilities)
	}
	if got, want := runner.calls, []string{"--version"}; !sameStrings(got, want) {
		t.Fatalf("runner calls = %#v, want %#v", got, want)
	}
}

func TestClaudeProbeAbsentExecutableIsUnavailable(t *testing.T) {
	runner := &fixtureRunner{errors: map[string]error{
		"--version": errors.New("executable file not found in %PATH%"),
	}}

	capabilities, err := probeCapabilities(context.Background(), "claude", runner)
	if !errors.Is(err, harness.ErrHarnessUnavailable) {
		t.Fatalf("probe error = %v, want ErrHarnessUnavailable", err)
	}
	if errors.Is(err, harness.ErrNativeUnverified) || capabilities.VersionKnown || capabilities.NativeVersion != "" || capabilities.TransportVerified || capabilities.Verified {
		t.Fatalf("absent executable result = capabilities=%+v error=%v, want unavailable only", capabilities, err)
	}
	if got := capabilities.Unsupported[string(harness.CapabilityStart)]; got != "Claude Code executable is unavailable" {
		t.Fatalf("start reason = %q, want unavailable reason", got)
	}
}

func TestClaudeAdapterStartRemainsFailClosedAfterProbeEvidence(t *testing.T) {
	runner := &fixtureRunner{responses: map[string][]byte{
		"--version": []byte(TestedVersion + " (Claude Code)\n"),
		"--help":    []byte("--print --output-format stream-json --json-schema"),
	}}
	adapter := NewCandidateAdapter("claude")
	adapter.probe = func(ctx context.Context) (harness.Capabilities, error) {
		return probeCapabilities(ctx, "claude", runner)
	}

	capabilities, err := adapter.Probe(context.Background())
	if !errors.Is(err, harness.ErrNativeUnverified) || capabilities.Verified || capabilities.Start || capabilities.Events || capabilities.Cancel || capabilities.ImplementationVersion != "" {
		t.Fatalf("Probe() = capabilities=%+v error=%v, want unverified no-operation projection", capabilities, err)
	}
	session, err := adapter.Start(context.Background(), harness.StartRequest{}, nil)
	if session != nil {
		t.Fatalf("Start() session = %T, want nil", session)
	}
	if err == nil {
		t.Fatalf("Start() error = nil, want a refusal from unpromoted evidence")
	}
}

func TestClaudeProbeBoundsHangingRunnerAndPreservesCallerCancellation(t *testing.T) {
	for _, test := range []struct {
		name             string
		context          func() (context.Context, context.CancelFunc)
		want             error
		cancelAfterStart bool
	}{
		{name: "local timeout", context: func() (context.Context, context.CancelFunc) {
			return context.Background(), func() {}
		}, want: context.DeadlineExceeded},
		{name: "caller cancellation", context: func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background())
		}, want: context.Canceled, cancelAfterStart: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := test.context()
			defer cancel()
			runner := &hangingRunner{started: make(chan struct{})}
			done := make(chan error, 1)
			go func() {
				_, err := probeCapabilities(ctx, "claude", runner)
				done <- err
			}()

			select {
			case <-runner.started:
			case <-time.After(testHangGuard):
				t.Fatal("probeCapabilities() did not start the runner")
			}
			if test.cancelAfterStart {
				cancel()
			}

			select {
			case err := <-done:
				if !errors.Is(err, test.want) {
					t.Fatalf("probeCapabilities() error = %v, want %v", err, test.want)
				}
			case <-time.After(testHangGuard):
				t.Fatalf("probeCapabilities() did not return after %s", test.name)
			}
		})
	}
}

func TestClaudeProbeRejectsSuccessAfterProbeDeadline(t *testing.T) {
	capabilities, err := probeCapabilities(context.Background(), "claude", successAfterDeadlineRunner{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("probeCapabilities() error = %v, want context.DeadlineExceeded", err)
	}
	if capabilities.VersionKnown || capabilities.NativeVersion != "" || capabilities.TransportVerified || capabilities.Verified {
		t.Fatalf("capabilities = %+v, want no capability from success-after-deadline output", capabilities)
	}
}

func TestClaudeProbeRejectsNilContextBeforeTheSharedGuard(t *testing.T) {
	runner := &fixtureRunner{}
	capabilities, err := probeCapabilities(nil, "claude", runner)
	if err == nil || err.Error() != "Claude probe context must not be nil" {
		t.Fatalf("probeCapabilities(nil) error = %v, want the entry-level Claude guard", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("runner calls = %#v, want none for a nil context", runner.calls)
	}
	if capabilities.VersionKnown || capabilities.NativeVersion != "" || capabilities.TransportVerified || capabilities.Verified {
		t.Fatalf("capabilities = %+v, want no capability from a nil context", capabilities)
	}
}

func TestClaudeProbeDoesNotParseFailedCommandOutput(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "output limit", err: execution.ErrOutputLimitExceeded},
		{name: "termination", err: errors.New("bounded process termination failed")},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := &fixtureRunner{results: map[string]fixtureResult{
				"--version": {output: []byte(TestedVersion + " (Claude Code)\n"), err: test.err},
			}}
			capabilities, err := probeCapabilities(context.Background(), "claude", runner)
			if err == nil {
				t.Fatal("probeCapabilities() error = nil, want bounded command failure")
			}
			if capabilities.VersionKnown || capabilities.NativeVersion != "" || capabilities.TransportVerified || capabilities.Verified {
				t.Fatalf("capabilities = %+v, want no capability parsed from failed output", capabilities)
			}
		})
	}
}

type fixtureRunner struct {
	responses map[string][]byte
	errors    map[string]error
	results   map[string]fixtureResult
	digest    string
	digestErr error
	calls     []string
}

// ExecutableDigest supplies a recorded blob digest so promotion-gate tests stay
// deterministic and never hash a real client.
func (runner *fixtureRunner) ExecutableDigest(_ context.Context, _ string) (string, error) {
	if runner.digestErr != nil {
		return "", runner.digestErr
	}
	return runner.digest, nil
}

type fixtureResult struct {
	output []byte
	err    error
}

type successAfterDeadlineRunner struct{}

func (successAfterDeadlineRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	<-ctx.Done()
	return []byte(TestedVersion + " (Claude Code)\n"), nil
}

func (runner *fixtureRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	key := ""
	for index, arg := range args {
		if index > 0 {
			key += " "
		}
		key += arg
	}
	runner.calls = append(runner.calls, key)
	if result, ok := runner.results[key]; ok {
		return result.output, result.err
	}
	if err, ok := runner.errors[key]; ok {
		return nil, err
	}
	if response, ok := runner.responses[key]; ok {
		return response, nil
	}
	return nil, errors.New("fixture command not found")
}

type hangingRunner struct {
	started chan struct{}
}

func (runner *hangingRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	if runner.started != nil {
		close(runner.started)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

// knownVersionProjection is one provider's expected probe projection once the
// exact tested version and transport help were recorded. The provider supplies
// its own fixture, expected constants, and error expectation, so the shared
// assertion below cannot drift between providers.
type knownVersionProjection struct {
	provider  string
	kind      harness.Kind
	version   string
	wantError error
	calls     []string
	wantCalls []string
}

// assertKnownVersionProjection asserts the projection every provider probe must
// report for that evidence: a known version, verified transport, no native
// operation, and only the commands that provider's probe issues. Control-surface
// expectations a provider does not share stay asserted in that provider's test.
func assertKnownVersionProjection(t *testing.T, capabilities harness.Capabilities, err error, want knownVersionProjection) {
	t.Helper()
	if !errors.Is(err, want.wantError) {
		t.Fatalf("%s probe error = %v, want %v", want.provider, err, want.wantError)
	}
	if capabilities.Kind != want.kind || capabilities.NativeVersion != want.version || !capabilities.VersionKnown {
		t.Fatalf("%s capabilities identity = %+v, want %s %s with known version", want.provider, capabilities, want.kind, want.version)
	}
	if !capabilities.TransportVerified || capabilities.Verified || capabilities.Start || capabilities.Events || capabilities.Cancel {
		t.Fatalf("%s capabilities = %+v, want help-only transport evidence and no operations", want.provider, capabilities)
	}
	if !sameStrings(want.calls, want.wantCalls) {
		t.Fatalf("%s runner calls = %#v, want %#v", want.provider, want.calls, want.wantCalls)
	}
}

func TestClaudeVersionGateAnchorsTheExactClientLine(t *testing.T) {
	for _, test := range []struct {
		name    string
		output  string
		version string
	}{
		{name: "exact client line", output: TestedVersion + " (Claude Code)\n", version: TestedVersion},
		{name: "shim banner first", output: "mise: installing claude\n" + TestedVersion + " (Claude Code)\n"},
		{name: "trailing token", output: TestedVersion + " (Claude Code) extra\n"},
		{name: "other client", output: "claude-code " + TestedVersion + "\n"},
		{name: "prerelease suffix", output: TestedVersion + "-beta.1 (Claude Code)\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := parseVersion(test.output); got != test.version {
				t.Fatalf("parseVersion(%q) = %q, want %q", test.output, got, test.version)
			}
		})
	}
}

func TestClaudeCapabilitiesForPlatformPromotesOnlyTheTestedBlobOnWindows(t *testing.T) {
	complete := probeEvidence{Version: TestedVersion, VersionKnown: true, TransportVerified: true}
	for _, test := range []struct {
		name          string
		platform      string
		evidence      probeEvidence
		hashMatched   bool
		wantVerified  bool
		wantReasonSub string
	}{
		{name: "windows exact blob", platform: "windows", evidence: complete, hashMatched: true, wantVerified: true},
		{name: "windows blob mismatch", platform: "windows", evidence: complete, wantReasonSub: "exact tested executable blob"},
		{name: "linux exact blob", platform: "linux", evidence: complete, hashMatched: true, wantReasonSub: "verified only on Windows"},
		{name: "darwin exact blob", platform: "darwin", evidence: complete, hashMatched: true, wantReasonSub: "verified only on Windows"},
		{
			name:          "windows without transport evidence",
			platform:      "windows",
			evidence:      probeEvidence{Version: TestedVersion, VersionKnown: true},
			hashMatched:   true,
			wantReasonSub: "probe evidence is incomplete",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			capabilities, err := capabilitiesForPlatform(test.platform, test.evidence, test.hashMatched)
			if test.wantVerified {
				if err != nil || !capabilities.Verified || !capabilities.Start || !capabilities.Events || !capabilities.Cancel {
					t.Fatalf("capabilities = %+v error = %v, want verified start/events/cancel", capabilities, err)
				}
				if capabilities.ImplementationVersion != implementationVersion || capabilities.ProtocolVersion != 1 {
					t.Fatalf("capabilities identity = %+v, want implementation %q protocol 1", capabilities, implementationVersion)
				}
				if capabilities.Resume || capabilities.Handoff || capabilities.ApprovalResponse || capabilities.ProviderAccess ||
					capabilities.HardCostLimit || capabilities.Guidance != harness.GuidanceUnsupported || capabilities.Pause != harness.PauseUnsupported || capabilities.Usage != harness.UsageUnknown {
					t.Fatalf("unproved operations advertised: %+v", capabilities)
				}
				return
			}
			if !errors.Is(err, harness.ErrNativeUnverified) {
				t.Fatalf("error = %v, want ErrNativeUnverified", err)
			}
			if capabilities.Verified || capabilities.Start || capabilities.Events || capabilities.Cancel {
				t.Fatalf("capabilities = %+v, want no promoted operation", capabilities)
			}
			// An unpromoted projection must not carry the promoted implementation
			// identity: the daemon reports `unavailable` metadata from it.
			if capabilities.ImplementationVersion != "" {
				t.Fatalf("implementation version = %q, want none without promotion", capabilities.ImplementationVersion)
			}
			for _, capability := range []harness.Capability{harness.CapabilityStart, harness.CapabilityEvents, harness.CapabilityCancel} {
				reason := capabilities.Unsupported[string(capability)]
				if !strings.Contains(reason, test.wantReasonSub) {
					t.Fatalf("%s reason = %q, want it to contain %q", capability, reason, test.wantReasonSub)
				}
			}
		})
	}
}

func TestClaudeAdapterProbePromotesOnlyTheResolvedTestedBlob(t *testing.T) {
	help := []byte("--print --output-format stream-json --json-schema")
	version := []byte(TestedVersion + " (Claude Code)\n")
	for _, test := range []struct {
		name            string
		runner          *fixtureRunner
		wantVerified    bool
		wantError       error
		wantDigestKnown bool
	}{
		{
			name:         "recorded tested blob",
			runner:       &fixtureRunner{responses: map[string][]byte{"--version": version, "--help": help}, digest: TestedExecutableHash},
			wantVerified: true,
		},
		{
			name:   "different blob with the same version",
			runner: &fixtureRunner{responses: map[string][]byte{"--version": version, "--help": help}, digest: "sha256:" + strings.Repeat("0", 64)},
		},
		{
			name:   "unhashable executable",
			runner: &fixtureRunner{responses: map[string][]byte{"--version": version, "--help": help}, digestErr: errors.New("access denied")},
		},
		{
			name:      "untested version",
			runner:    &fixtureRunner{responses: map[string][]byte{"--version": []byte("2.1.322 (Claude Code)\n"), "--help": help}, digest: TestedExecutableHash},
			wantError: harness.ErrUnsupportedVersion,
		},
		{
			name:      "transport without json schema",
			runner:    &fixtureRunner{responses: map[string][]byte{"--version": version, "--help": []byte("--print --output-format stream-json")}, digest: TestedExecutableHash},
			wantError: harness.ErrNativeUnverified,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			capabilities, err := probeCapabilities(context.Background(), "claude", test.runner)
			if test.wantError != nil && !errors.Is(err, test.wantError) {
				t.Fatalf("probe error = %v, want %v", err, test.wantError)
			}
			promoted := err == nil && capabilities.Verified
			wantPromoted := test.wantVerified && runtime.GOOS == "windows"
			if promoted != wantPromoted {
				t.Fatalf("promoted = %t (platform %s), want %t: capabilities=%+v error=%v", promoted, runtime.GOOS, wantPromoted, capabilities, err)
			}
			if !promoted && (capabilities.Start || capabilities.Events || capabilities.Cancel) {
				t.Fatalf("capabilities = %+v, want no operation promoted", capabilities)
			}
			if test.wantVerified && capabilities.NativeVersion != TestedVersion {
				t.Fatalf("native version = %q, want %q", capabilities.NativeVersion, TestedVersion)
			}
		})
	}
}

// The committed fixtures are promotion evidence, so their digests must still
// describe the bytes in the tree and must bind the exact tested executable blob.
func TestClaudeTestdataManifestBindsRecordedDigests(t *testing.T) {
	directory := filepath.Join("..", "testdata", "claude", TestedVersion)
	encoded, err := os.ReadFile(filepath.Join(directory, "fixture-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Executable string            `json:"executable"`
		Fixtures   map[string]string `json:"fixtures"`
	}
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		t.Fatalf("fixture manifest: %v", err)
	}
	if manifest.Executable != TestedExecutableHash {
		t.Fatalf("manifest executable = %q, want %q", manifest.Executable, TestedExecutableHash)
	}
	// The committed evidence set is part of the promotion binding, so a removed
	// fixture record must fail rather than silently shrink what the manifest
	// attests.
	required := []string{"version.txt", "help.txt", "stream-json.jsonl", "native-repository-turn.jsonl"}
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
}
