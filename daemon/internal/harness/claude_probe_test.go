package harness

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
)

func TestClaudeProbeKnownVersionAndHelpFailsClosed(t *testing.T) {
	runner := &claudeFixtureRunner{responses: map[string][]byte{
		"--version": []byte(ClaudeTestedVersion + " (Claude Code)\n"),
		"--help":    []byte("claude --print --output-format <format> text, json, or stream-json\n  --json-schema <schema>  JSON Schema for structured output validation.\n"),
	}}

	capabilities, err := probeClaudeExecutable(context.Background(), `C:\Users\lhan\.local\bin\claude.exe`, runner)
	if !errors.Is(err, ErrNativeUnverified) {
		t.Fatalf("probe error = %v, want ErrNativeUnverified", err)
	}
	if capabilities.Kind != KindClaude || capabilities.NativeVersion != ClaudeTestedVersion || !capabilities.VersionKnown {
		t.Fatalf("capabilities identity = %+v, want Claude %s with known version", capabilities, ClaudeTestedVersion)
	}
	if !capabilities.TransportVerified || capabilities.Verified || capabilities.Start || capabilities.Events || capabilities.Cancel || capabilities.Resume {
		t.Fatalf("capabilities = %+v, want help-only transport evidence and no operations", capabilities)
	}
	if capabilities.Guidance != GuidanceUnsupported || capabilities.Pause != PauseUnsupported || capabilities.ApprovalResponse || capabilities.Usage != UsageUnknown || capabilities.HardCostLimit {
		t.Fatalf("unsupported controls = %+v, want all unsupported", capabilities)
	}
	if got, want := runner.calls, []string{"--version", "--help"}; !sameStrings(got, want) {
		t.Fatalf("runner calls = %#v, want %#v", got, want)
	}
}

func TestClaudeProbeHelpWithoutJSONSchemaFailsClosed(t *testing.T) {
	runner := &claudeFixtureRunner{responses: map[string][]byte{
		"--version": []byte(ClaudeTestedVersion + " (Claude Code)\n"),
		"--help":    []byte("claude --print --output-format <format> text, json, or stream-json\n"),
	}}

	capabilities, err := probeClaudeExecutable(context.Background(), "claude", runner)
	if !errors.Is(err, ErrNativeUnverified) || !strings.Contains(err.Error(), "--json-schema") {
		t.Fatalf("probe error = %v, want ErrNativeUnverified naming --json-schema", err)
	}
	if capabilities.TransportVerified || capabilities.Verified || capabilities.Start {
		t.Fatalf("capabilities = %+v, want no transport evidence without --json-schema", capabilities)
	}
}

func TestClaudeProbeUnknownVersionFailsClosed(t *testing.T) {
	runner := &claudeFixtureRunner{responses: map[string][]byte{
		"--version": []byte("2.1.259 (Claude Code)\n"),
		"--help":    []byte("should not be called"),
	}}

	capabilities, err := probeClaudeExecutable(context.Background(), "claude", runner)
	if !errors.Is(err, ErrUnsupportedVersion) {
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
	runner := &claudeFixtureRunner{errors: map[string]error{
		"--version": errors.New("executable file not found in %PATH%"),
	}}

	capabilities, err := probeClaudeExecutable(context.Background(), "claude", runner)
	if !errors.Is(err, ErrHarnessUnavailable) {
		t.Fatalf("probe error = %v, want ErrHarnessUnavailable", err)
	}
	if errors.Is(err, ErrNativeUnverified) || capabilities.VersionKnown || capabilities.NativeVersion != "" || capabilities.TransportVerified || capabilities.Verified {
		t.Fatalf("absent executable result = capabilities=%+v error=%v, want unavailable only", capabilities, err)
	}
	if got := capabilities.Unsupported[string(CapabilityStart)]; got != "Claude Code executable is unavailable" {
		t.Fatalf("start reason = %q, want unavailable reason", got)
	}
}

func TestClaudeAdapterStartRemainsFailClosedAfterProbeEvidence(t *testing.T) {
	runner := &claudeFixtureRunner{responses: map[string][]byte{
		"--version": []byte(ClaudeTestedVersion + " (Claude Code)\n"),
		"--help":    []byte("--print --output-format stream-json --json-schema"),
	}}
	adapter := NewClaudeAdapterWithRunner("claude", runner)

	session, err := adapter.Start(context.Background(), StartRequest{}, nil)
	if session != nil {
		t.Fatalf("Start() session = %T, want nil", session)
	}
	if !errors.Is(err, ErrNativeUnverified) {
		t.Fatalf("Start() error = %v, want ErrNativeUnverified", err)
	}
}

func TestClaudeProbeBoundsHangingRunnerAndPreservesCallerCancellation(t *testing.T) {
	for _, test := range []struct {
		name             string
		context          func() (context.Context, context.CancelFunc)
		want             error
		cancelAfterStart bool
		waitWithin       time.Duration
	}{
		{name: "local timeout", context: func() (context.Context, context.CancelFunc) {
			return context.Background(), func() {}
		}, want: context.DeadlineExceeded, waitWithin: claudeProbeTimeout + time.Second},
		{name: "caller cancellation", context: func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background())
		}, want: context.Canceled, cancelAfterStart: true, waitWithin: time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := test.context()
			defer cancel()
			runner := &claudeHangingRunner{started: make(chan struct{})}
			done := make(chan error, 1)
			go func() {
				_, err := probeClaudeExecutable(ctx, "claude", runner)
				done <- err
			}()

			select {
			case <-runner.started:
			case <-time.After(time.Second):
				t.Fatal("probeClaudeExecutable() did not start the runner")
			}
			if test.cancelAfterStart {
				cancel()
			}

			select {
			case err := <-done:
				if !errors.Is(err, test.want) {
					t.Fatalf("probeClaudeExecutable() error = %v, want %v", err, test.want)
				}
			case <-time.After(test.waitWithin):
				t.Fatalf("probeClaudeExecutable() did not return within %v", test.waitWithin)
			}
		})
	}
}

func TestBoundedProbeRejectsSuccessAfterDeadline(t *testing.T) {
	output, err := runBoundedProbe(context.Background(), time.Nanosecond, func(ctx context.Context) ([]byte, error) {
		<-ctx.Done()
		return []byte("valid capability output"), nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runBoundedProbe() error = %v, want context.DeadlineExceeded", err)
	}
	if output != nil {
		t.Fatalf("runBoundedProbe() output = %q, want nil after deadline", output)
	}
}

func TestClaudeProbeRejectsSuccessAfterProbeDeadline(t *testing.T) {
	capabilities, err := probeClaudeExecutable(context.Background(), "claude", claudeSuccessAfterDeadlineRunner{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("probeClaudeExecutable() error = %v, want context.DeadlineExceeded", err)
	}
	if capabilities.VersionKnown || capabilities.NativeVersion != "" || capabilities.TransportVerified || capabilities.Verified {
		t.Fatalf("capabilities = %+v, want no capability from success-after-deadline output", capabilities)
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
			runner := &claudeFixtureRunner{results: map[string]claudeFixtureResult{
				"--version": {output: []byte(ClaudeTestedVersion + " (Claude Code)\n"), err: test.err},
			}}
			capabilities, err := probeClaudeExecutable(context.Background(), "claude", runner)
			if err == nil {
				t.Fatal("probeClaudeExecutable() error = nil, want bounded command failure")
			}
			if capabilities.VersionKnown || capabilities.NativeVersion != "" || capabilities.TransportVerified || capabilities.Verified {
				t.Fatalf("capabilities = %+v, want no capability parsed from failed output", capabilities)
			}
		})
	}
}

type claudeFixtureRunner struct {
	responses map[string][]byte
	errors    map[string]error
	results   map[string]claudeFixtureResult
	digest    string
	digestErr error
	calls     []string
}

// ExecutableDigest supplies a recorded blob digest so promotion-gate tests stay
// deterministic and never hash a real client.
func (runner *claudeFixtureRunner) ExecutableDigest(_ context.Context, _ string) (string, error) {
	if runner.digestErr != nil {
		return "", runner.digestErr
	}
	return runner.digest, nil
}

type claudeFixtureResult struct {
	output []byte
	err    error
}

type claudeSuccessAfterDeadlineRunner struct{}

func (claudeSuccessAfterDeadlineRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	<-ctx.Done()
	return []byte(ClaudeTestedVersion + " (Claude Code)\n"), nil
}

func (runner *claudeFixtureRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
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

type claudeHangingRunner struct {
	started chan struct{}
}

func (runner *claudeHangingRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
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

func TestClaudeVersionGateAnchorsTheExactClientLine(t *testing.T) {
	for _, test := range []struct {
		name    string
		output  string
		version string
	}{
		{name: "exact client line", output: ClaudeTestedVersion + " (Claude Code)\n", version: ClaudeTestedVersion},
		{name: "shim banner first", output: "mise: installing claude\n" + ClaudeTestedVersion + " (Claude Code)\n"},
		{name: "trailing token", output: ClaudeTestedVersion + " (Claude Code) extra\n"},
		{name: "other client", output: "claude-code " + ClaudeTestedVersion + "\n"},
		{name: "prerelease suffix", output: ClaudeTestedVersion + "-beta.1 (Claude Code)\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := parseClaudeVersion(test.output); got != test.version {
				t.Fatalf("parseClaudeVersion(%q) = %q, want %q", test.output, got, test.version)
			}
		})
	}
}

func TestClaudeCapabilitiesForPlatformPromotesOnlyTheTestedBlobOnWindows(t *testing.T) {
	complete := claudeProbeEvidence{Version: ClaudeTestedVersion, VersionKnown: true, TransportVerified: true}
	for _, test := range []struct {
		name          string
		platform      string
		evidence      claudeProbeEvidence
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
			evidence:      claudeProbeEvidence{Version: ClaudeTestedVersion, VersionKnown: true},
			hashMatched:   true,
			wantReasonSub: "probe evidence is incomplete",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			capabilities, err := claudeCapabilitiesForPlatform(test.platform, test.evidence, test.hashMatched)
			if test.wantVerified {
				if err != nil || !capabilities.Verified || !capabilities.Start || !capabilities.Events || !capabilities.Cancel {
					t.Fatalf("capabilities = %+v error = %v, want verified start/events/cancel", capabilities, err)
				}
				if capabilities.ImplementationVersion != claudeImplementationVersion || capabilities.ProtocolVersion != 1 {
					t.Fatalf("capabilities identity = %+v, want implementation %q protocol 1", capabilities, claudeImplementationVersion)
				}
				if capabilities.Resume || capabilities.Handoff || capabilities.ApprovalResponse || capabilities.ProviderAccess ||
					capabilities.HardCostLimit || capabilities.Guidance != GuidanceUnsupported || capabilities.Pause != PauseUnsupported || capabilities.Usage != UsageUnknown {
					t.Fatalf("unproved operations advertised: %+v", capabilities)
				}
				return
			}
			if !errors.Is(err, ErrNativeUnverified) {
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
			for _, capability := range []Capability{CapabilityStart, CapabilityEvents, CapabilityCancel} {
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
	version := []byte(ClaudeTestedVersion + " (Claude Code)\n")
	for _, test := range []struct {
		name            string
		runner          *claudeFixtureRunner
		wantVerified    bool
		wantError       error
		wantDigestKnown bool
	}{
		{
			name:         "recorded tested blob",
			runner:       &claudeFixtureRunner{responses: map[string][]byte{"--version": version, "--help": help}, digest: ClaudeTestedExecutableHash},
			wantVerified: true,
		},
		{
			name:   "different blob with the same version",
			runner: &claudeFixtureRunner{responses: map[string][]byte{"--version": version, "--help": help}, digest: "sha256:" + strings.Repeat("0", 64)},
		},
		{
			name:   "unhashable executable",
			runner: &claudeFixtureRunner{responses: map[string][]byte{"--version": version, "--help": help}, digestErr: errors.New("access denied")},
		},
		{
			name:      "untested version",
			runner:    &claudeFixtureRunner{responses: map[string][]byte{"--version": []byte("2.1.322 (Claude Code)\n"), "--help": help}, digest: ClaudeTestedExecutableHash},
			wantError: ErrUnsupportedVersion,
		},
		{
			name:      "transport without json schema",
			runner:    &claudeFixtureRunner{responses: map[string][]byte{"--version": version, "--help": []byte("--print --output-format stream-json")}, digest: ClaudeTestedExecutableHash},
			wantError: ErrNativeUnverified,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			capabilities, err := probeClaudeAdapterCapabilities(context.Background(), "claude", test.runner)
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
			if test.wantVerified && capabilities.NativeVersion != ClaudeTestedVersion {
				t.Fatalf("native version = %q, want %q", capabilities.NativeVersion, ClaudeTestedVersion)
			}
		})
	}
}

// The committed fixtures are promotion evidence, so their digests must still
// describe the bytes in the tree and must bind the exact tested executable blob.
func TestClaudeTestdataManifestBindsRecordedDigests(t *testing.T) {
	directory := filepath.Join("testdata", "claude", ClaudeTestedVersion)
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
	if manifest.Executable != ClaudeTestedExecutableHash {
		t.Fatalf("manifest executable = %q, want %q", manifest.Executable, ClaudeTestedExecutableHash)
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
