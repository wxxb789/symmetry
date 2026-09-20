package execution

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

func TestOutputTruncatedSilentTerminate(t *testing.T) {
	process, release := newOutputLifecycleProcess(t, SinkFunc(func(context.Context, Event) error {
		return nil
	}))

	result := terminateOutputLifecycle(t, process, release)
	if !result.Terminated {
		t.Fatal("Terminated = false, want true")
	}
	if result.OutputTruncated {
		t.Fatal("OutputTruncated = true, want false for silent termination")
	}
}

func TestOutputTruncatedDeliveredBeforeTerminate(t *testing.T) {
	sink := &recordingSink{notify: make(chan struct{}, 1)}
	process, release := newOutputLifecycleProcess(t, sink)
	if !process.enqueue(Stdout, []byte("delivered")) {
		t.Fatal("enqueue() = false, want true")
	}
	select {
	case <-sink.notify:
	case <-time.After(5 * time.Second):
		t.Fatal("output event was not delivered")
	}

	result := terminateOutputLifecycle(t, process, release)
	if result.OutputTruncated {
		t.Fatal("OutputTruncated = true, want false after delivered output")
	}
}

func TestOutputTruncatedLateChunk(t *testing.T) {
	process, release := newOutputLifecycleProcess(t, SinkFunc(func(context.Context, Event) error {
		return nil
	}))
	terminationDone := make(chan error, 1)
	go func() { terminationDone <- process.Terminate(context.Background(), 0) }()
	waitForOutputStop(t, process)

	if process.enqueue(Stdout, []byte("late")) {
		t.Fatal("enqueue() = true, want false after output delivery stopped")
	}
	close(release)
	result := waitForResult(t, process)
	if err := <-terminationDone; err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	if !result.OutputTruncated {
		t.Fatal("OutputTruncated = false, want true after late output")
	}
}

func TestOutputTruncatedQueuedDiscard(t *testing.T) {
	sink := newGatedOutputSink()
	process, release := newOutputLifecycleProcess(t, sink)
	if !process.enqueue(Stdout, []byte("in flight")) {
		t.Fatal("first enqueue() = false, want true")
	}
	select {
	case <-sink.started:
	case <-time.After(5 * time.Second):
		t.Fatal("sink did not start handling the first event")
	}
	if !process.enqueue(Stdout, []byte("queued")) {
		t.Fatal("second enqueue() = false, want true")
	}

	terminationDone := make(chan error, 1)
	go func() { terminationDone <- process.Terminate(context.Background(), 0) }()
	waitForOutputStop(t, process)
	close(sink.release)
	close(release)
	result := waitForResult(t, process)
	if err := <-terminationDone; err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	if result.OutputTruncated != true {
		t.Fatal("OutputTruncated = false, want true after queued discard")
	}
	if got := sink.calls(); got != 1 {
		t.Fatalf("sink calls = %d, want 1 for the in-flight event", got)
	}
}

func TestOutputTruncatedCanceledSinkError(t *testing.T) {
	sink := newCancellationOutputSink(errors.New("sink canceled"))
	process, release := newOutputLifecycleProcess(t, sink)
	if !process.enqueue(Stdout, []byte("in flight")) {
		t.Fatal("enqueue() = false, want true")
	}
	select {
	case <-sink.started:
	case <-time.After(5 * time.Second):
		t.Fatal("sink did not start handling the event")
	}

	terminationDone := make(chan error, 1)
	go func() { terminationDone <- process.Terminate(context.Background(), 0) }()
	waitForOutputStop(t, process)
	select {
	case <-sink.returned:
	case <-time.After(5 * time.Second):
		t.Fatal("sink did not return after cancellation")
	}
	close(release)
	result := waitForResult(t, process)
	if err := <-terminationDone; err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	if !result.OutputTruncated {
		t.Fatal("OutputTruncated = false, want true after canceled sink error")
	}
	if result.SinkError != nil {
		t.Fatalf("SinkError = %v, want nil for termination cancellation", result.SinkError)
	}
}

func TestOutputTruncatedCanceledSinkNil(t *testing.T) {
	sink := newCancellationOutputSink(nil)
	process, release := newOutputLifecycleProcess(t, sink)
	if !process.enqueue(Stdout, []byte("in flight")) {
		t.Fatal("enqueue() = false, want true")
	}
	select {
	case <-sink.started:
	case <-time.After(5 * time.Second):
		t.Fatal("sink did not start handling the event")
	}

	terminationDone := make(chan error, 1)
	go func() { terminationDone <- process.Terminate(context.Background(), 0) }()
	waitForOutputStop(t, process)
	select {
	case <-sink.returned:
	case <-time.After(5 * time.Second):
		t.Fatal("sink did not return after cancellation")
	}
	close(release)
	result := waitForResult(t, process)
	if err := <-terminationDone; err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	if result.OutputTruncated {
		t.Fatal("OutputTruncated = true, want false when canceled in-flight sink returns nil")
	}
}

func TestOutputTruncatedNaturalDrain(t *testing.T) {
	sink := &recordingSink{notify: make(chan struct{}, 1)}
	process, release := newOutputLifecycleProcess(t, sink)
	if !process.enqueue(Stdout, []byte("drained")) {
		t.Fatal("enqueue() = false, want true")
	}
	select {
	case <-sink.notify:
	case <-time.After(5 * time.Second):
		t.Fatal("output event was not delivered")
	}
	close(release)
	result := waitForResult(t, process)
	if result.OutputTruncated {
		t.Fatal("OutputTruncated = true, want false after natural drain")
	}
}

func TestFailedStartOutputMarksTruncated(t *testing.T) {
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}
	defer stdinRead.Close()
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	defer stdoutRead.Close()
	stderrRead, stderrWrite, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}
	defer stderrRead.Close()

	if _, err := stdoutWrite.Write([]byte("failed-start output")); err != nil {
		t.Fatalf("write failed-start output: %v", err)
	}
	started := &startedProcess{
		pid:      91,
		identity: "failed-start-output",
		wait:     func() (int, error) { return 1, errors.New("child stopped") },
		kill:     func() error { return nil },
		close:    func() error { return nil },
	}
	startErr := errors.New("start failed")
	process, err := cleanupFailedStartStarted(
		started,
		stdinRead, stdinWrite, stdoutRead, stdoutWrite, stderrRead, stderrWrite,
		time.Now().UTC(),
		startErr,
		nil,
	)
	if process == nil {
		t.Fatalf("cleanupFailedStartStarted() process = nil, error = %v", err)
	}
	if !errors.Is(err, startErr) {
		t.Fatalf("cleanupFailedStartStarted() error = %v, want %v", err, startErr)
	}
	result := waitForResult(t, process)
	if !result.OutputTruncated {
		t.Fatal("OutputTruncated = false, want true for discarded failed-start output")
	}
}

func newOutputLifecycleProcess(t *testing.T, sink Sink) (*Process, chan struct{}) {
	t.Helper()
	waitStarted := make(chan struct{})
	waitRelease := make(chan struct{})
	started := &startedProcess{
		pid:      7,
		identity: "output-lifecycle",
		wait: func() (int, error) {
			close(waitStarted)
			<-waitRelease
			return 0, nil
		},
		kill:  func() error { return nil },
		close: func() error { return nil },
	}
	process := newProcessFromStarted(started, sink, time.Now().UTC(), nil, nil)
	go process.deliverOutput()
	go process.waitForCompletion()
	select {
	case <-waitStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("process wait did not start")
	}
	return process, waitRelease
}

func terminateOutputLifecycle(t *testing.T, process *Process, release chan struct{}) Result {
	t.Helper()
	terminationDone := make(chan error, 1)
	go func() { terminationDone <- process.Terminate(context.Background(), 0) }()
	waitForOutputStop(t, process)
	close(release)
	result := waitForResult(t, process)
	if err := <-terminationDone; err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	return result
}

func waitForOutputStop(t *testing.T, process *Process) {
	t.Helper()
	select {
	case <-process.outputStop:
	case <-time.After(5 * time.Second):
		t.Fatal("output delivery did not stop")
	}
}

type gatedOutputSink struct {
	started     chan struct{}
	release     chan struct{}
	startedOnce sync.Once
	mutex       sync.Mutex
	count       int
}

func newGatedOutputSink() *gatedOutputSink {
	return &gatedOutputSink{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (sink *gatedOutputSink) Handle(context.Context, Event) error {
	sink.mutex.Lock()
	sink.count++
	sink.mutex.Unlock()
	sink.startedOnce.Do(func() { close(sink.started) })
	<-sink.release
	return nil
}

func (sink *gatedOutputSink) calls() int {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	return sink.count
}

type cancellationOutputSink struct {
	started   chan struct{}
	returned  chan struct{}
	returnErr error
}

func newCancellationOutputSink(returnErr error) *cancellationOutputSink {
	return &cancellationOutputSink{
		started:   make(chan struct{}),
		returned:  make(chan struct{}),
		returnErr: returnErr,
	}
}

func (sink *cancellationOutputSink) Handle(ctx context.Context, _ Event) error {
	close(sink.started)
	<-ctx.Done()
	close(sink.returned)
	return sink.returnErr
}
