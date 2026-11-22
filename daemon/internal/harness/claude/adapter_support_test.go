package claude

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

// testHangGuard bounds how long a test waits for a scripted, in-process double
// to publish an observable. It only turns a hung harness into a failure; it is
// never a statement about how fast the adapter should be.
const testHangGuard = 30 * time.Second

func newCandidateTestAdapter(process *candidateFakeProcess, invocation *execution.Invocation) *CandidateAdapter {
	return &CandidateAdapter{
		executable:   "claude-test",
		newSessionID: func() (string, error) { return candidateTestSessionID, nil },
		startProcess: func(_ context.Context, got execution.Invocation, sink execution.Sink) (candidateProcess, error) {
			if invocation != nil {
				*invocation = got
			}
			process.sink = sink
			if got.PersistProcess != nil {
				if err := got.PersistProcess(42, "test:42"); err != nil {
					return process, err
				}
			}
			return process, nil
		},
	}
}

func candidateStartRequest(t *testing.T) harness.StartRequest {
	t.Helper()
	return harness.StartRequest{Workspace: t.TempDir(), Invocation: execution.Invocation{Env: []string{"PATH=test"}}}
}

func openCandidateSession(t *testing.T, staged harness.StagedSession, process *candidateFakeProcess) harness.NativeSessionHandle {
	t.Helper()
	handle, err := staged.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	// Native Claude Code 2.1.323 emits init only after the first user frame.
	// These tests are not about that ordering, so establish identity here.
	if err := process.emitJSON(`{"type":"system","subtype":"init","session_id":"` + candidateTestSessionID + `"}`); err != nil {
		t.Fatalf("emit matching init: %v", err)
	}
	return handle
}

// cancelAndAwaitTermination requests a Control(cancel) for a session in the
// background and waits until the session reached bounded process termination, so
// the caller can drive the race under test while the cancel is in flight. The
// caller owns every receipt assertion.
func cancelAndAwaitTermination(t *testing.T, session harness.Session, process *candidateFakeProcess) (chan harness.ControlReceipt, chan error) {
	t.Helper()
	receipts := make(chan harness.ControlReceipt, 1)
	receiptErrors := make(chan error, 1)
	go func() {
		receipt, controlErr := session.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-1", Kind: harness.ControlCancel})
		receipts <- receipt
		receiptErrors <- controlErr
	}()
	awaitCandidateTermination(t, process, "cancellation never reached bounded process termination")
	return receipts, receiptErrors
}

// awaitCandidateTermination blocks until the fake process observed one
// bounded termination request, and fails with the caller's message otherwise.
func awaitCandidateTermination(t *testing.T, process *candidateFakeProcess, message string) {
	t.Helper()
	deadline := time.Now().Add(testHangGuard)
	for {
		if calls, _ := process.terminationDetails(); calls > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal(message)
		}
		time.Sleep(time.Millisecond)
	}
}

func cleanupCandidateSession(t *testing.T, session harness.Session) {
	cleanupCandidateSessionExpectClose(t, session, nil)
}

// candidateStdoutSink returns the exact execution sink the adapter handed
// to this process, so a test can deliver a callback through the real output
// handler the way the runner's delivery loop does.
func candidateStdoutSink(t *testing.T, process *candidateFakeProcess) execution.Sink {
	t.Helper()
	process.mu.Lock()
	sink := process.sink
	process.mu.Unlock()
	if sink == nil {
		t.Fatal("fake Claude process sink is nil")
	}
	return sink
}

// awaitCandidateCloseFence blocks until the session raised its close
// fence, so a test can deliver a callback in the window where the stop has been
// claimed but has not yet reached the runner's output shutdown. Close takes the
// session write mutex before it issues Terminate, so a test holding that mutex
// also holds the stop before the runner stops delivering output.
func awaitCandidateCloseFence(t *testing.T, session *candidateSession) {
	t.Helper()
	deadline := time.Now().Add(testHangGuard)
	for {
		session.mutex.Lock()
		closing := session.closing
		session.mutex.Unlock()
		if closing {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("close fence was never raised")
		}
		time.Sleep(time.Millisecond)
	}
}

func cleanupCandidateSessionExpectClose(t *testing.T, session harness.Session, expectedCloseErr error) {
	t.Helper()
	t.Cleanup(func() {
		if err := session.Close(context.Background()); err != nil && (expectedCloseErr == nil || !errors.Is(err, expectedCloseErr)) {
			t.Errorf("candidate cleanup Close() error = %v", err)
		}
		waitContext, cancel := context.WithTimeout(context.Background(), testHangGuard)
		_, waitErr := session.Wait(waitContext)
		cancel()
		if waitErr != nil {
			t.Errorf("candidate cleanup Wait() error = %v", waitErr)
		}
		if native, ok := session.(*candidateSession); ok {
			select {
			case <-native.watchDone:
			case <-time.After(testHangGuard):
				t.Errorf("candidate cleanup watcher did not terminate")
			}
		}
	})
}

type recordingCandidateSink struct {
	mu     sync.Mutex
	events []harness.Event
}

type blockingCandidateSink struct {
	*recordingCandidateSink
	taskResultEntered chan struct{}
	releaseTaskResult chan struct{}
	taskResultOnce    sync.Once
}

func newBlockingCandidateSink() *blockingCandidateSink {
	return &blockingCandidateSink{
		recordingCandidateSink: &recordingCandidateSink{},
		taskResultEntered:      make(chan struct{}),
		releaseTaskResult:      make(chan struct{}),
	}
}

func (sink *blockingCandidateSink) Handle(ctx context.Context, event harness.Event) error {
	if event.Kind == harness.EventTaskResult {
		sink.taskResultOnce.Do(func() { close(sink.taskResultEntered) })
		select {
		case <-sink.releaseTaskResult:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return sink.recordingCandidateSink.Handle(ctx, event)
}

func (sink *recordingCandidateSink) Handle(_ context.Context, event harness.Event) error {
	sink.mu.Lock()
	sink.events = append(sink.events, event)
	sink.mu.Unlock()
	return nil
}

func (sink *recordingCandidateSink) eventsSnapshot() []harness.Event {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	events := make([]harness.Event, len(sink.events))
	copy(events, sink.events)
	return events
}

func (sink *recordingCandidateSink) event(kind harness.EventKind) (harness.Event, bool) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for _, event := range sink.events {
		if event.Kind == kind {
			return event, true
		}
	}
	return harness.Event{}, false
}

type candidateFakeProcess struct {
	mu sync.Mutex

	sink            execution.Sink
	writes          [][]byte
	blockWrites     bool
	writeStarted    chan struct{}
	writeStartOnce  sync.Once
	terminateCalls  int
	terminateGrace  time.Duration
	terminateError  error
	terminateResult execution.Result
	holdTermination bool
	result          execution.Result
	waitDone        chan struct{}
	finishOnce      sync.Once
}

func newCandidateFakeProcess() *candidateFakeProcess {
	return &candidateFakeProcess{waitDone: make(chan struct{}), writeStarted: make(chan struct{})}
}

func (process *candidateFakeProcess) WriteInputContext(ctx context.Context, input []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	process.mu.Lock()
	blockWrites := process.blockWrites
	process.mu.Unlock()
	if blockWrites {
		process.writeStartOnce.Do(func() { close(process.writeStarted) })
		<-ctx.Done()
		return ctx.Err()
	}
	process.mu.Lock()
	process.writes = append(process.writes, append([]byte(nil), input...))
	process.mu.Unlock()
	return nil
}

func (process *candidateFakeProcess) Wait() execution.Result {
	<-process.waitDone
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.result
}

func (process *candidateFakeProcess) Terminate(_ context.Context, grace time.Duration) error {
	process.mu.Lock()
	process.terminateCalls++
	process.terminateGrace = grace
	terminateErr := process.terminateError
	if process.terminateResult != (execution.Result{}) {
		process.result = process.terminateResult
	} else {
		process.result = execution.Result{PID: 42, ExitCode: -1, Terminated: true, FinishedAt: time.Now().UTC(), TerminationError: terminateErr}
	}
	holdTermination := process.holdTermination
	process.mu.Unlock()
	if !holdTermination {
		process.finish()
	}
	return terminateErr
}

func (process *candidateFakeProcess) ProcessDetails() (int, string) {
	return 42, "test:42"
}

func (process *candidateFakeProcess) finish() {
	process.finishOnce.Do(func() { close(process.waitDone) })
}

func (process *candidateFakeProcess) finishResult(result execution.Result) {
	process.mu.Lock()
	process.result = result
	process.mu.Unlock()
	process.finish()
}

func (process *candidateFakeProcess) emitJSON(value string) error {
	process.mu.Lock()
	sink := process.sink
	process.mu.Unlock()
	if sink == nil {
		return errors.New("fake Claude process sink is nil")
	}
	return sink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: 1, At: time.Now().UTC(), Data: []byte(value + "\n")})
}

func (process *candidateFakeProcess) writeCount() int {
	process.mu.Lock()
	defer process.mu.Unlock()
	return len(process.writes)
}

func (process *candidateFakeProcess) writesSnapshot() [][]byte {
	process.mu.Lock()
	defer process.mu.Unlock()
	result := make([][]byte, len(process.writes))
	for index := range process.writes {
		result[index] = append([]byte(nil), process.writes[index]...)
	}
	return result
}

func (process *candidateFakeProcess) terminationDetails() (int, time.Duration) {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.terminateCalls, process.terminateGrace
}

func validCandidateTaskResultJSON(t *testing.T) string {
	t.Helper()
	subject := protocol.Subject{
		ResourceID: "00000000-0000-4000-8000-000000000007",
		Commit:     "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TreeDigest: "sha256:0000000000000000000000000000000000000000000000000000000000000000",
	}
	subjectHash, err := subject.Hash()
	if err != nil {
		t.Fatalf("hash subject: %v", err)
	}
	result := map[string]any{
		"schema_version":       "symmetry.task_result.v1",
		"result_id":            "00000000-0000-4000-8000-000000000006",
		"kind":                 "progress",
		"summary":              "made bounded progress",
		"subject":              subject,
		"subject_hash":         subjectHash,
		"evidence_refs":        []string{},
		"blocker":              nil,
		"proposed_next_action": nil,
		"proposal":             nil,
		"reason":               nil,
		"diagnostics":          []any{},
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("encode task result: %v", err)
	}
	return string(encoded)
}

func candidateResultEnvelope(t *testing.T, semanticJSON string) string {
	t.Helper()
	envelope := map[string]any{
		"type":              "result",
		"subtype":           "success",
		"terminal_reason":   "completed",
		"is_error":          false,
		"session_id":        candidateTestSessionID,
		"result":            semanticJSON,
		"structured_output": json.RawMessage(semanticJSON),
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("encode result envelope: %v", err)
	}
	return string(encoded)
}

// A stop that truncates the model's own output must not turn an accepted
// cancellation into a failure: stdout can deliver a prefix of a record before
// the process is stopped, and the incomplete trailing record is the shutdown,
// not a decode failure. A complete malformed record keeps failing the turn.
func TestClaudeCandidateCancellationOwnsIncompleteTrailingRecord(t *testing.T) {
	process := newCandidateFakeProcess()
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "settle the cancelled turn", Context: jsonObjectForCandidateAdditionalTest()}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	// The fake keeps running after the bounded termination request so the test
	// releases the real output handler, and the watcher's decoder close, in the
	// order a stopped process produces them.
	process.holdTermination = true
	process.mu.Lock()
	stdoutSink := process.sink
	process.mu.Unlock()
	if stdoutSink == nil {
		t.Fatal("fake Claude process sink is nil")
	}
	// A record prefix, not a complete record: the decoder retains it until the
	// stream closes, which happens when the stopped process is reaped. It arrives
	// while the turn is still active, so it reaches the output handler the same
	// way live stdout does.
	if err := stdoutSink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: 1, At: time.Now().UTC(), Data: []byte(`{"type":"assistant"`)}); err != nil {
		t.Fatalf("deliver truncated trailing record = %v, want decoder to defer it until process close", err)
	}
	receipts, receiptErrors := cancelAndAwaitTermination(t, session, process)
	process.mu.Lock()
	process.holdTermination = false
	process.mu.Unlock()
	process.finish()
	receipt := <-receipts
	if err := <-receiptErrors; err != nil || receipt.Outcome != harness.ControlApplied {
		t.Fatalf("Control(cancel) = receipt=%+v error=%v, want applied", receipt, err)
	}
	if err := staged.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v, want the cancelled turn that the accepted stop owns", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled || result.Semantic != nil || result.Reason != nil {
		t.Fatalf("result = %+v, want a cancelled result without a failure reason", result)
	}
	if result.Process.OutputError != nil {
		t.Fatalf("process settlement = %+v, want the truncated tail treated as cancellation truncation", result.Process)
	}
}

// A delivery callback that the close fence rejects while an accepted stop owns
// the turn is that stop's own output shutdown, not a transport failure. Control
// claims the cancellation and the fence together, while the runner stops output
// delivery only inside the termination it has not issued yet, so a rejection in
// that window would otherwise be recorded as the process result's SinkError and
// reclassify the applied cancellation as a failed run.
func TestClaudeCandidateCancellationOwnsFenceRejectedDelivery(t *testing.T) {
	process := newCandidateFakeProcess()
	// Keep the watcher blocked in Wait so the accepted stop, not the process
	// reaper, decides the callback window under test.
	process.holdTermination = true
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "cancel under a live stdout chunk", Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	internal := staged.(*candidateSession)
	stdoutSink := candidateStdoutSink(t, process)
	// Hold the close path before the runner: Close takes this mutex before it
	// issues Terminate, and Terminate is what stops output delivery. The fence is
	// up while the mutex is held, which is exactly the window in which the
	// runner's delivery loop still records a sink rejection.
	internal.writeMutex.Lock()
	receipts := make(chan harness.ControlReceipt, 1)
	receiptErrors := make(chan error, 1)
	go func() {
		receipt, controlErr := staged.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-1", Kind: harness.ControlCancel})
		receipts <- receipt
		receiptErrors <- controlErr
	}()
	awaitCandidateCloseFence(t, internal)
	internal.mutex.Lock()
	cancelRequested := internal.cancelRequested
	internal.mutex.Unlock()
	if !cancelRequested {
		t.Fatal("Control(cancel) did not claim cancellation before the fence rejected the callback")
	}
	if calls, _ := process.terminationDetails(); calls != 0 {
		t.Fatalf("Terminate calls = %d before the callback, want the stop still in flight so output delivery is enabled", calls)
	}
	deliveryErr := stdoutSink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: 4, At: time.Now().UTC(), Data: []byte(`{"type":"log","message":"winding down"}` + "\n")})
	// execution.Process.deliverOutput keeps a sink rejection as the process
	// result's SinkError while output delivery is still enabled, so the rejected
	// callback is the process evidence the watcher validates below.
	process.terminateResult = execution.Result{PID: 42, ExitCode: -1, Terminated: true, SinkError: deliveryErr, FinishedAt: time.Now().UTC()}
	internal.writeMutex.Unlock()
	awaitCandidateTermination(t, process, "cancellation never reached bounded process termination")
	process.finish()
	receipt := <-receipts
	receiptErr := <-receiptErrors
	turnErr := staged.WaitTurn(context.Background())
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if deliveryErr != nil {
		t.Fatalf("delivery callback = %v for settled result = %+v (process = %+v), want the accepted stop's shutdown counted as delivery", deliveryErr, result, result.Process)
	}
	if receiptErr != nil || receipt.Outcome != harness.ControlApplied {
		t.Fatalf("Control(cancel) = receipt=%+v error=%v, want applied", receipt, receiptErr)
	}
	if turnErr != nil {
		t.Fatalf("WaitTurn() error = %v, want the cancelled turn that the accepted stop owns", turnErr)
	}
	if result.Kind != harness.ResultCancelled || result.Semantic != nil || result.Reason != nil {
		t.Fatalf("result = %+v, want a cancelled result without a failure reason", result)
	}
	if result.Process.SinkError != nil {
		t.Fatalf("process settlement = %+v, want no synthetic sink failure on the accepted stop", result.Process)
	}
}

// The complementary negative control: a delivery callback rejected by the close
// fence while no cancellation owns the turn is not shutdown evidence either. A
// turn whose terminal was already observed and whose session is then closed
// normally keeps failing closed, so the fence never launders a transport
// rejection into a clean settlement.
func TestClaudeCandidateNormalCloseRejectedDeliveryStillFailsClosed(t *testing.T) {
	process := newCandidateFakeProcess()
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "observe a terminal before the normal close", Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if err := process.emitJSON(candidateResultEnvelope(t, validCandidateTaskResultJSON(t))); err != nil {
		t.Fatalf("emit verified terminal: %v", err)
	}
	if err := staged.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v, want the observed terminal's own outcome", err)
	}
	internal := staged.(*candidateSession)
	stdoutSink := candidateStdoutSink(t, process)
	// Same window as the cancellation case: the fence is up, and the runner has
	// not stopped output delivery yet.
	internal.writeMutex.Lock()
	closeErrors := make(chan error, 1)
	go func() { closeErrors <- staged.Close(context.Background()) }()
	awaitCandidateCloseFence(t, internal)
	internal.mutex.Lock()
	cancelRequested := internal.cancelRequested
	internal.mutex.Unlock()
	if cancelRequested {
		t.Fatal("Close() claimed cancellation for a turn whose terminal was already observed")
	}
	if calls, _ := process.terminationDetails(); calls != 0 {
		t.Fatalf("Terminate calls = %d before the callback, want the stop still in flight so output delivery is enabled", calls)
	}
	deliveryErr := stdoutSink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: 4, At: time.Now().UTC(), Data: []byte(`{"type":"log","message":"after the terminal"}` + "\n")})
	process.terminateResult = execution.Result{PID: 42, ExitCode: -1, Terminated: true, SinkError: deliveryErr, FinishedAt: time.Now().UTC()}
	internal.writeMutex.Unlock()
	if err := <-closeErrors; err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if !errors.Is(deliveryErr, errCandidateSessionClosed) {
		t.Fatalf("delivery callback = %v, want the closed-session rejection for a delivered frame after a normal close", deliveryErr)
	}
	if result.Kind != harness.ResultFailed || result.Reason == nil || *result.Reason != protocol.TaskResultReasonProcessFailure {
		t.Fatalf("result = %+v, want a failed settlement with a process-failure reason", result)
	}
	if result.Process.SinkError == nil {
		t.Fatalf("process settlement = %+v, want the rejected delivery recorded as the process sink failure", result.Process)
	}
}
