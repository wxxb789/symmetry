package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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

func TestProbeBoundsHangingRunnerAndPreservesCancellation(t *testing.T) {
	for _, test := range []struct {
		name             string
		context          func() (context.Context, context.CancelFunc)
		want             error
		cancelAfterStart bool
		waitWithin       time.Duration
	}{
		{name: "local timeout", context: func() (context.Context, context.CancelFunc) { return context.Background(), func() {} }, want: context.DeadlineExceeded, waitWithin: probeTimeout + time.Second},
		{name: "caller earlier deadline", context: func() (context.Context, context.CancelFunc) {
			return newProbeDeadlineContext()
		}, want: context.DeadlineExceeded, cancelAfterStart: true, waitWithin: time.Second},
		{name: "caller cancellation", context: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }, want: context.Canceled, cancelAfterStart: true, waitWithin: time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := test.context()
			defer cancel()
			runner := hangingRunner{started: make(chan struct{})}
			done := make(chan error, 1)
			go func() {
				_, err := Probe(ctx, "codex", runner)
				done <- err
			}()
			select {
			case <-runner.started:
			case <-time.After(time.Second):
				t.Fatal("Probe() did not start the runner")
			}
			if test.cancelAfterStart {
				cancel()
			}
			var err error
			select {
			case err = <-done:
			case <-time.After(test.waitWithin):
				t.Fatalf("Probe() did not return within %v", test.waitWithin)
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("Probe() error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestProbeRejectsSuccessfulRunnerResultAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := &cancellationSuccessRunner{started: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := Probe(ctx, "codex", runner)
		done <- err
	}()
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("Probe() did not start the injected runner")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Probe() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Probe() did not return after cancellation")
	}
	calls := runner.callsSnapshot()
	if len(calls) != 1 || calls[0] != "--version" {
		t.Fatalf("injected runner calls = %v, want only --version", calls)
	}
}

func TestProbeRejectsSuccessfulSchemaResultAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner := &cancellationSuccessSchemaRunner{
		fixtureRunner: fixtureRunner{
			responses: map[string][]byte{
				"--version":         []byte("codex-cli 0.153.4\n"),
				"app-server --help": []byte("app-server stdio\n"),
			},
		},
		started: make(chan struct{}),
	}
	done := make(chan error, 1)
	go func() {
		_, err := Probe(ctx, "codex", runner)
		done <- err
	}()
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("Probe() did not start the injected schema runner")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Probe() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Probe() did not return after schema cancellation")
	}
	if len(runner.calls) != 2 || runner.calls[0] != "--version" || runner.calls[1] != "app-server --help" {
		t.Fatalf("injected runner calls = %v, want version and help only", runner.calls)
	}
}

func TestProbeBoundsHangingSchemaRunner(t *testing.T) {
	runner := &hangingSchemaRunner{fixtureRunner: fixtureRunner{
		responses: map[string][]byte{
			"--version":         []byte("codex-cli 0.153.4\n"),
			"app-server --help": []byte("app-server stdio\n"),
		},
	}, started: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := Probe(context.Background(), "codex", runner)
		done <- err
	}()
	select {
	case <-runner.started:
	case <-time.After(time.Second):
		t.Fatal("Probe() did not start the schema runner")
	}
	var err error
	select {
	case err = <-done:
	case <-time.After(probeTimeout + time.Second):
		t.Fatalf("Probe() did not return within %v", probeTimeout+time.Second)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Probe() error = %v, want context.DeadlineExceeded", err)
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

type hangingRunner struct {
	started chan struct{}
}

func (runner hangingRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	if runner.started != nil {
		close(runner.started)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

type cancellationSuccessRunner struct {
	started     chan struct{}
	startedOnce sync.Once
	mutex       sync.Mutex
	calls       []string
}

func (runner *cancellationSuccessRunner) Run(ctx context.Context, _ string, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	runner.mutex.Lock()
	runner.calls = append(runner.calls, key)
	runner.mutex.Unlock()
	if key == "--version" {
		runner.startedOnce.Do(func() { close(runner.started) })
		<-ctx.Done()
		return []byte("codex-cli 0.153.4\n"), nil
	}
	return []byte("app-server stdio\n"), nil
}

func (runner *cancellationSuccessRunner) callsSnapshot() []string {
	runner.mutex.Lock()
	defer runner.mutex.Unlock()
	return append([]string(nil), runner.calls...)
}

type cancellationSuccessSchemaRunner struct {
	fixtureRunner
	started     chan struct{}
	startedOnce sync.Once
}

func (runner *cancellationSuccessSchemaRunner) SchemaDigest(ctx context.Context, _ string) (string, error) {
	runner.startedOnce.Do(func() { close(runner.started) })
	<-ctx.Done()
	return TestedSchemaHash, nil
}

type hangingSchemaRunner struct {
	fixtureRunner
	started chan struct{}
}

func (runner hangingSchemaRunner) SchemaDigest(ctx context.Context, _ string) (string, error) {
	if runner.started != nil {
		close(runner.started)
	}
	<-ctx.Done()
	return "", ctx.Err()
}

type probeDeadlineContext struct {
	done chan struct{}
	once sync.Once
	mu   sync.Mutex
	err  error
}

func newProbeDeadlineContext() (context.Context, context.CancelFunc) {
	ctx := &probeDeadlineContext{done: make(chan struct{})}
	return ctx, ctx.expire
}

func (ctx *probeDeadlineContext) Deadline() (time.Time, bool) {
	return time.Unix(0, 0), true
}

func (ctx *probeDeadlineContext) Done() <-chan struct{} {
	return ctx.done
}

func (ctx *probeDeadlineContext) Err() error {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	return ctx.err
}

func (*probeDeadlineContext) Value(any) any {
	return nil
}

func (ctx *probeDeadlineContext) expire() {
	ctx.once.Do(func() {
		ctx.mu.Lock()
		ctx.err = context.DeadlineExceeded
		ctx.mu.Unlock()
		close(ctx.done)
	})
}
