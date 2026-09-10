package harness

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestRegistryRejectsUnknownAdapter(t *testing.T) {
	registry := NewRegistry()
	_, err := registry.Lookup(Kind("missing"))
	if !errors.Is(err, ErrUnknownAdapter) {
		t.Fatalf("Lookup() error = %v, want ErrUnknownAdapter", err)
	}
}

func TestRegistryKeepsUnavailableCapabilitiesExplicit(t *testing.T) {
	registry := NewRegistry()
	capabilities, err := registry.Probe(context.Background(), KindClaude)
	if err == nil {
		t.Fatal("Probe() error = nil, want unavailable error")
	}
	if capabilities.Verified || capabilities.Start || capabilities.Events || capabilities.Cancel {
		t.Fatalf("unavailable capabilities = %+v, want fail-closed operations", capabilities)
	}
	if capabilities.Guidance != GuidanceUnsupported || capabilities.Pause != PauseUnsupported || capabilities.Usage != UsageUnknown {
		t.Fatalf("unavailable control capabilities = %+v, want explicit unsupported values", capabilities)
	}
	if capabilities.Unsupported[string(CapabilityPause)] == "" {
		t.Fatalf("unsupported map = %#v, want pause reason", capabilities.Unsupported)
	}
}

func TestGenericAdapterDoesNotInterpretFakeAgentJSON(t *testing.T) {
	process := newFakeProcess()
	runner := &fakeRunner{process: process}
	adapter := NewGenericAdapter(runner)
	var got []Event
	ctx := context.Background()
	_, err := adapter.Start(ctx, StartRequest{}, EventSinkFunc(func(_ context.Context, event Event) error {
		got = append(got, event)
		return nil
	}))
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	runner.emit(execution.Event{Stream: execution.Stdout, Sequence: 1, Data: []byte(`{"type":"progress"}`)})
	process.finish(execution.Result{PID: 7, ExitCode: 0, FinishedAt: time.Now().UTC()})
	if len(got) != 1 || got[0].Kind != EventOutput {
		t.Fatalf("events = %#v, want one opaque output event", got)
	}
	if got[0].Diagnostic {
		t.Fatal("generic fake-agent JSON was interpreted as a native diagnostic")
	}
}

func TestGenericSessionCancellationLifecycle(t *testing.T) {
	process := newFakeProcess()
	runner := &fakeRunner{process: process}
	adapter := NewGenericAdapter(runner)
	session, err := adapter.Start(context.Background(), StartRequest{}, nil)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if _, ok := session.(StagedSession); ok {
		t.Fatal("generic session unexpectedly implements staged native lifecycle")
	}

	receipt, err := session.Control(context.Background(), ControlRequest{CommandID: "cancel-1", Kind: ControlCancel})
	if err != nil {
		t.Fatalf("Control(cancel) error = %v", err)
	}
	if receipt.Outcome != ControlApplied || receipt.Capability != CapabilityCancel {
		t.Fatalf("receipt = %+v, want applied cancel", receipt)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != ResultCancelled {
		t.Fatalf("result kind = %q, want cancelled", result.Kind)
	}
	if !process.wasTerminated() {
		t.Fatal("process was not terminated by cancellation")
	}
}

func TestGenericSessionCloseRetriesFailedTerminationAndStabilizesSuccess(t *testing.T) {
	terminationErr := errors.New("transient termination failure")
	process := newRetryableProcess(terminationErr)
	t.Cleanup(process.releaseFirstTermination)
	runner := &retryableProcessRunner{process: process}
	session, err := NewGenericAdapter(runner).Start(context.Background(), StartRequest{}, nil)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	firstDone := make(chan error, 1)
	go func() { firstDone <- session.Close(context.Background()) }()
	select {
	case <-process.firstTerminateStarted:
	case <-time.After(time.Second):
		t.Fatal("first Close() did not start process termination")
	}

	secondContext, cancel := context.WithCancel(context.Background())
	cancel()
	if err := session.Close(secondContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("second Close() error = %v, want cancellation while first close is incomplete", err)
	}

	process.releaseFirstTermination()
	if err := <-firstDone; !errors.Is(err, terminationErr) {
		t.Fatalf("first Close() error = %v, want %v", err, terminationErr)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("retry Close() error = %v", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("repeated successful Close() error = %v", err)
	}
	if calls := process.terminationCallCount(); calls != 2 {
		t.Fatalf("Terminate calls = %d, want one failed attempt and one retry", calls)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != ResultCancelled {
		t.Fatalf("result kind = %q, want cancelled after successful retry", result.Kind)
	}
}

func TestGenericSessionRejectsUnsupportedControls(t *testing.T) {
	process := newFakeProcess()
	session, err := NewGenericAdapter(&fakeRunner{process: process}).Start(context.Background(), StartRequest{}, nil)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	_, err = session.Control(context.Background(), ControlRequest{Kind: ControlPause})
	var capabilityErr *CapabilityError
	if !errors.As(err, &capabilityErr) || !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("pause error = %v, want typed unsupported capability", err)
	}
	if capabilityErr.Capability != CapabilityPause {
		t.Fatalf("capability = %q, want pause", capabilityErr.Capability)
	}
	_ = session.Close(context.Background())
}

func TestGenericAdapterRejectsStrictCostCapBeforeProcessStart(t *testing.T) {
	process := newFakeProcess()
	runner := &fakeRunner{process: process}
	cap := "250000"
	_, err := NewGenericAdapter(runner).Start(context.Background(), StartRequest{Limits: Limits{MaxCostMicrousd: &cap}}, nil)
	var capabilityErr *CapabilityError
	if !errors.As(err, &capabilityErr) || capabilityErr.Capability != CapabilityHardCostLimit {
		t.Fatalf("Start() error = %v, want hard-cost capability rejection", err)
	}
	if runner.starts != 0 {
		t.Fatalf("generic runner starts = %d, want none for strict cap", runner.starts)
	}
}

func TestGenericAdapterRejectsProviderAccessBeforeProcessStart(t *testing.T) {
	process := newFakeProcess()
	runner := &fakeRunner{process: process}
	_, err := NewGenericAdapter(runner).Start(context.Background(), StartRequest{
		ProviderAccess: &protocol.ProviderAccess{Path: "https://control.example.test/api/v1/provider-actions", Token: "provider-token"},
	}, nil)
	var capabilityErr *CapabilityError
	if !errors.As(err, &capabilityErr) || capabilityErr.Capability != CapabilityProviderAccess {
		t.Fatalf("Start() error = %v, want provider-access capability rejection", err)
	}
	if runner.starts != 0 {
		t.Fatalf("generic runner starts = %d, want none for provider access", runner.starts)
	}
}

type fakeRunner struct {
	process *fakeProcess
	sink    execution.Sink
	starts  int
}

func (runner *fakeRunner) Start(_ context.Context, _ execution.Invocation, sink execution.Sink) (ProcessHandle, error) {
	runner.starts++
	runner.sink = sink
	return runner.process, nil
}

func (runner *fakeRunner) emit(event execution.Event) {
	if runner.sink == nil {
		return
	}
	_ = runner.sink.Handle(context.Background(), event)
}

type retryableProcessRunner struct {
	process ProcessHandle
}

func (runner *retryableProcessRunner) Start(_ context.Context, _ execution.Invocation, _ execution.Sink) (ProcessHandle, error) {
	return runner.process, nil
}

type retryableProcess struct {
	done                  chan struct{}
	firstTerminateStarted chan struct{}
	releaseFirst          chan struct{}
	firstErr              error
	startOnce             sync.Once
	releaseOnce           sync.Once
	doneOnce              sync.Once

	mu          sync.Mutex
	calls       int
	termination execution.Result
}

func newRetryableProcess(firstErr error) *retryableProcess {
	return &retryableProcess{
		done:                  make(chan struct{}),
		firstTerminateStarted: make(chan struct{}),
		releaseFirst:          make(chan struct{}),
		firstErr:              firstErr,
	}
}

func (process *retryableProcess) Wait() execution.Result {
	<-process.done
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.termination
}

func (process *retryableProcess) Terminate(ctx context.Context, _ time.Duration) error {
	process.mu.Lock()
	process.calls++
	call := process.calls
	process.mu.Unlock()
	if call == 1 {
		process.startOnce.Do(func() { close(process.firstTerminateStarted) })
		select {
		case <-process.releaseFirst:
		case <-ctx.Done():
			return ctx.Err()
		}
		return process.firstErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	process.mu.Lock()
	process.termination = execution.Result{PID: 9, ExitCode: -1, Terminated: true, FinishedAt: time.Now().UTC()}
	process.mu.Unlock()
	process.doneOnce.Do(func() { close(process.done) })
	return nil
}

func (process *retryableProcess) releaseFirstTermination() {
	process.releaseOnce.Do(func() { close(process.releaseFirst) })
}

func (process *retryableProcess) terminationCallCount() int {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.calls
}

type fakeProcess struct {
	done chan struct{}

	mu         sync.Mutex
	result     execution.Result
	terminated bool
	closeOnce  sync.Once
}

func newFakeProcess() *fakeProcess {
	return &fakeProcess{done: make(chan struct{})}
}

func (process *fakeProcess) Wait() execution.Result {
	<-process.done
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.result
}

func (process *fakeProcess) Terminate(ctx context.Context, _ time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	process.mu.Lock()
	process.terminated = true
	process.result = execution.Result{PID: 7, ExitCode: -1, Terminated: true, FinishedAt: time.Now().UTC()}
	process.mu.Unlock()
	process.closeOnce.Do(func() { close(process.done) })
	return nil
}

func (process *fakeProcess) finish(result execution.Result) {
	process.mu.Lock()
	process.result = result
	process.mu.Unlock()
	process.closeOnce.Do(func() { close(process.done) })
}

func (process *fakeProcess) wasTerminated() bool {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.terminated
}
