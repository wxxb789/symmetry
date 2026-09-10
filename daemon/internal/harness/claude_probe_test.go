package harness

import (
	"context"
	"errors"
	"testing"
)

func TestClaudeProbeKnownVersionAndHelpFailsClosed(t *testing.T) {
	runner := &claudeFixtureRunner{responses: map[string][]byte{
		"--version": []byte("2.1.259 (Claude Code)\n"),
		"--help":    []byte("claude --print --output-format <format> text, json, or stream-json\n"),
	}}

	capabilities, err := probeClaudeExecutable(context.Background(), `C:\Users\lhan\.local\bin\claude.exe`, runner)
	if !errors.Is(err, ErrNativeUnverified) {
		t.Fatalf("probe error = %v, want ErrNativeUnverified", err)
	}
	if capabilities.Kind != KindClaude || capabilities.NativeVersion != testedClaudeVersion || !capabilities.VersionKnown {
		t.Fatalf("capabilities identity = %+v, want Claude %s with known version", capabilities, testedClaudeVersion)
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

func TestClaudeProbeUnknownVersionFailsClosed(t *testing.T) {
	runner := &claudeFixtureRunner{responses: map[string][]byte{
		"--version": []byte("2.1.260 (Claude Code)\n"),
		"--help":    []byte("should not be called"),
	}}

	capabilities, err := probeClaudeExecutable(context.Background(), "claude", runner)
	if !errors.Is(err, ErrUnsupportedVersion) {
		t.Fatalf("probe error = %v, want ErrUnsupportedVersion", err)
	}
	if capabilities.NativeVersion != "2.1.260" || !capabilities.VersionKnown || capabilities.TransportVerified || capabilities.Verified {
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
		"--version": []byte("2.1.259 (Claude Code)\n"),
		"--help":    []byte("--print --output-format stream-json"),
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

type claudeFixtureRunner struct {
	responses map[string][]byte
	errors    map[string]error
	calls     []string
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
	if err, ok := runner.errors[key]; ok {
		return nil, err
	}
	if response, ok := runner.responses[key]; ok {
		return response, nil
	}
	return nil, errors.New("fixture command not found")
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
