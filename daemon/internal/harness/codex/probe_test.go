package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

func TestProbeKnownVersionAndHelpStillFailsClosed(t *testing.T) {
	help, err := os.ReadFile(filepath.Join("..", "testdata", "codex", "0.153.4", "app-server-help.txt"))
	if err != nil {
		t.Fatalf("read help fixture: %v", err)
	}
	runner := &fixtureRunner{
		responses: map[string][]byte{
			"--version":         []byte("codex-cli 0.153.4\n"),
			"app-server --help": help,
		},
		schemaDigest: TestedSchemaHash,
	}
	result, err := Probe(context.Background(), "codex", runner)
	if !errors.Is(err, harness.ErrNativeUnverified) {
		t.Fatalf("Probe() error = %v, want ErrNativeUnverified", err)
	}
	if result.Version != TestedVersion || !result.VersionKnown || !result.TransportKnown || !result.SchemaKnown || result.SchemaDigest != TestedSchemaHash {
		t.Fatalf("probe result = %+v, want exact version and stdio evidence", result)
	}
	if result.NativeSessionVerified || result.Capabilities.Verified || result.Capabilities.Start {
		t.Fatalf("probe advertised native support: %+v", result)
	}
	if result.Capabilities.Guidance != harness.GuidanceUnsupported || result.Capabilities.Pause != harness.PauseUnsupported || result.Capabilities.Usage != harness.UsageUnknown {
		t.Fatalf("probe controls = %+v, want explicit unsupported values", result.Capabilities)
	}
}

func TestProbeSchemaMismatchFailsClosedAsUnsupportedVersion(t *testing.T) {
	runner := &fixtureRunner{
		responses: map[string][]byte{
			"--version":         []byte("codex-cli 0.153.4\n"),
			"app-server --help": []byte("app-server stdio\n"),
		},
		schemaDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	result, err := Probe(context.Background(), "codex", runner)
	if !errors.Is(err, harness.ErrUnsupportedVersion) {
		t.Fatalf("Probe() error = %v, want ErrUnsupportedVersion", err)
	}
	if result.SchemaKnown || result.Capabilities.Verified || result.Capabilities.Start {
		t.Fatalf("schema-mismatched capabilities = %+v, want fail-closed", result)
	}
}

func TestProbeUnknownVersionFailsClosed(t *testing.T) {
	runner := &fixtureRunner{responses: map[string][]byte{"--version": []byte("codex-cli 0.154.0\n")}}
	result, err := Probe(context.Background(), "codex", runner)
	if !errors.Is(err, harness.ErrUnsupportedVersion) {
		t.Fatalf("Probe() error = %v, want ErrUnsupportedVersion", err)
	}
	if result.Capabilities.Verified || result.Capabilities.Start || result.Capabilities.Events {
		t.Fatalf("unknown version capabilities = %+v, want fail-closed", result.Capabilities)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %#v, want no app-server probe after version rejection", runner.calls)
	}
}

func TestAdapterStartDoesNotTreatProbeEvidenceAsAStartRequirement(t *testing.T) {
	adapter := NewAdapterWithRunner("codex", &fixtureRunner{responses: map[string][]byte{
		"--version":         []byte("codex-cli 0.153.4\n"),
		"app-server --help": []byte("app-server stdio\n"),
	}})
	process := newFakeNativeProcess()
	adapter.startProcess = func(_ context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
		process.sink = sink
		return process, nil
	}
	session, err := adapter.Start(context.Background(), harness.StartRequest{Workspace: t.TempDir()}, harness.EventSinkFunc(func(context.Context, harness.Event) error { return nil }))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, ok := session.(harness.StagedSession); !ok {
		t.Fatalf("Start() = %T, want StagedSession", session)
	}
}

type fixtureRunner struct {
	responses    map[string][]byte
	calls        []string
	schemaDigest string
}

func (runner *fixtureRunner) SchemaDigest(_ context.Context, _ string) (string, error) {
	if runner.schemaDigest == "" {
		return "", errors.New("fixture schema digest not configured")
	}
	return runner.schemaDigest, nil
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
	response, ok := runner.responses[key]
	if !ok {
		return nil, errors.New("fixture command not found")
	}
	return response, nil
}
