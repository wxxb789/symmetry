package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

type controlResult struct {
	receipt harness.ControlReceipt
	err     error
}

func interruptRejectionResponse(interruptID uint64) map[string]any {
	return map[string]any{
		"jsonrpc": "2.0",
		"id":      interruptID,
		"error":   map[string]any{"code": -32001, "message": "turn is no longer interruptible"},
	}
}

func assertRejectedNonApplied(t *testing.T, control controlResult, label string) {
	t.Helper()
	if control.err == nil || control.receipt.Outcome == harness.ControlApplied {
		t.Fatalf("%s = %+v, want failed non-applied control", label, control)
	}
}

func assertDeferredIncomplete(t *testing.T, native *nativeSession, when string) {
	t.Helper()
	native.mutex.Lock()
	deferred := native.cancelTracker.deferredTerminal != nil
	completed := native.result != nil
	native.mutex.Unlock()
	if !deferred || completed {
		t.Fatalf("terminal state %s = deferred:%t completed:%t, want deferred and incomplete", when, deferred, completed)
	}
}

func TestControlCancelAwaitsTerminalInterruptedTurn(t *testing.T) {
	process := newFakeNativeProcess()
	adapter := fakeAdapter(process)
	session := startNativeSession(t, adapter, &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Stop safely.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})

	receiptResult := make(chan harness.ControlReceipt, 1)
	errResult := make(chan error, 1)
	go func() {
		receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "command-1", Kind: harness.ControlCancel})
		receiptResult <- receipt
		errResult <- err
	}()
	interrupt := process.nextRequest(t)
	if interrupt.Method != appServerMethodTurnInterrupt || interrupt.Params.ThreadID != "thread-1" || interrupt.Params.TurnID != "turn-1" {
		t.Fatalf("interrupt request = %+v", interrupt)
	}
	process.reply(t, interrupt.ID, map[string]any{})
	if err := <-errResult; err != nil {
		t.Fatalf("Control(cancel) error = %v", err)
	}
	if receipt := <-receiptResult; receipt.Outcome != harness.ControlApplied || !strings.Contains(receipt.Message, "awaiting") {
		t.Fatalf("cancel receipt = %+v", receipt)
	}

	// Interrupt acceptance is not terminal. Only the matching completed event
	// proves that the native turn entered its interrupted terminal state.
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled {
		t.Fatalf("interrupted locally cancelled result = %+v, want cancelled", result)
	}
}

func TestControlCancelMarksCancellationBeforeInterruptResponse(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Cancel before response.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	receiptResult := make(chan harness.ControlReceipt, 1)
	errResult := make(chan error, 1)
	go func() {
		receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "command-race", Kind: harness.ControlCancel})
		receiptResult <- receipt
		errResult <- err
	}()
	interrupt := process.nextRequest(t)
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	assertDeferredIncomplete(t, session.(*nativeSession), "before interrupt response")
	process.reply(t, interrupt.ID, map[string]any{})
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled {
		t.Fatalf("interrupted result = %+v, want cancelled after interrupt response", result)
	}
	if err := <-errResult; err != nil {
		t.Fatalf("Control(cancel) error = %v", err)
	}
	if receipt := <-receiptResult; receipt.Outcome != harness.ControlApplied {
		t.Fatalf("cancel receipt = %+v, want applied", receipt)
	}
}

func TestControlCancelDefersInterruptedTerminalUntilRejection(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Defer terminal until rejection.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})

	controlDone := make(chan controlResult, 1)
	go func() {
		receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "deferred-rejection", Kind: harness.ControlCancel})
		controlDone <- controlResult{receipt: receipt, err: err}
	}()
	interrupt := process.nextRequest(t)
	if err := process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress"))); err != nil {
		t.Fatalf("emit interrupted terminal: %v", err)
	}
	assertDeferredIncomplete(t, session.(*nativeSession), "after interrupt-before-rejection")
	if err := process.emitJSON(t, interruptRejectionResponse(interrupt.ID)); err != nil {
		t.Fatalf("emit late interrupt rejection: %v", err)
	}
	control := <-controlDone
	assertRejectedNonApplied(t, control, "rejected cancel result")
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed {
		t.Fatalf("terminal result after late rejection = %+v, want failed", result)
	}
}

func TestRejectedCancelDoesNotClassifyIndependentInterruptedTurnAsCancelled(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Reject cancellation explicitly.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})

	controlDone := make(chan controlResult, 1)
	go func() {
		receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "rejected-cancel", Kind: harness.ControlCancel})
		controlDone <- controlResult{receipt: receipt, err: err}
	}()
	interrupt := process.nextRequest(t)
	if err := process.emitJSON(t, interruptRejectionResponse(interrupt.ID)); err != nil {
		t.Fatalf("emit interrupt rejection: %v", err)
	}
	control := <-controlDone
	assertRejectedNonApplied(t, control, "rejected cancel result")

	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed {
		t.Fatalf("independently interrupted result after rejection = %+v, want failed", result)
	}
}

func TestInterruptRejectionBeforeSameChunkInterruptedTerminalWinsClassification(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Order rejection before terminal.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})

	controlDone := make(chan controlResult, 1)
	go func() {
		receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "same-chunk-rejection", Kind: harness.ControlCancel})
		controlDone <- controlResult{receipt: receipt, err: err}
	}()
	interrupt := process.nextRequest(t)
	errorRaw, err := json.Marshal(interruptRejectionResponse(interrupt.ID))
	if err != nil {
		t.Fatalf("encode interrupt rejection: %v", err)
	}
	terminalRaw, err := json.Marshal(completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	if err != nil {
		t.Fatalf("encode terminal notification: %v", err)
	}
	chunk := append(append(errorRaw, '\n'), append(terminalRaw, '\n')...)
	if err := process.sink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: 1, At: time.Now().UTC(), Data: chunk}); err != nil {
		t.Fatalf("emit same-chunk rejection and terminal: %v", err)
	}
	control := <-controlDone
	assertRejectedNonApplied(t, control, "same-chunk rejected cancel result")
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed {
		t.Fatalf("same-chunk interrupted result = %+v, want failed", result)
	}
}

func TestDeferredInterruptedTerminalUsesOlderValidCancelAttempt(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Retain older cancellation evidence.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	native := session.(*nativeSession)
	olderAttempt := native.beginCancelAttempt()
	native.markCancelAttemptWritten(olderAttempt)
	native.finishCancelAttempt(olderAttempt, context.DeadlineExceeded) // written, but native response is unknown
	newerAttempt := native.beginCancelAttempt()
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	native.failCancelAttempt(newerAttempt) // the newer write failed; older evidence remains effective
	native.finishCancelAttempt(newerAttempt, context.Canceled)
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled {
		t.Fatalf("deferred terminal result = %+v, want cancelled from older attempt", result)
	}
}

func TestConcurrentControlCancelSharesOneInterruptAttempt(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Share cancellation.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})

	results := make(chan controlResult, 2)
	for _, commandID := range []string{"cancel-a", "cancel-b"} {
		go func(commandID string) {
			receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: commandID, Kind: harness.ControlCancel})
			results <- controlResult{receipt: receipt, err: err}
		}(commandID)
	}
	interrupt := process.nextRequest(t)
	if interrupt.Method != appServerMethodTurnInterrupt {
		t.Fatalf("interrupt request = %+v", interrupt)
	}
	select {
	case raw := <-process.writes:
		t.Fatalf("second interrupt request = %s", raw)
	case <-time.After(50 * time.Millisecond):
	}
	process.reply(t, interrupt.ID, map[string]any{})
	for range 2 {
		result := <-results
		if result.err != nil || result.receipt.Outcome != harness.ControlApplied {
			t.Fatalf("shared cancel result = %+v", result)
		}
	}
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	if result, err := session.Wait(context.Background()); err != nil || result.Kind != harness.ResultCancelled {
		t.Fatalf("cancelled turn = %+v, err=%v", result, err)
	}
	_ = session.Close(context.Background())
}

func TestInterruptedTerminalWaitsForBlockedCancelWrite(t *testing.T) {
	process := newBlockingFakeNativeProcess()
	process.blockAfter = 5 // initialize, initialized, thread/start, turn/start, then interrupt
	adapter := &Adapter{
		executable: "codex-test",
		startProcess: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			process.sink = sink
			return process, nil
		},
	}
	session := startNativeSession(t, adapter, &recordingHarnessSink{})
	_ = completeOpen(t, process.fakeNativeProcess, session)
	completeTurnStart(t, process.fakeNativeProcess, session, harness.TurnRequest{Goal: "Classify blocked cancel.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	controlDone := make(chan error, 1)
	go func() {
		_, err := session.Control(ctx, harness.ControlRequest{CommandID: "blocked-cancel", Kind: harness.ControlCancel})
		controlDone <- err
	}()
	select {
	case <-process.writeStarted:
	case <-time.After(testHangGuard):
		t.Fatal("cancel write did not block")
	}
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	if err := <-controlDone; err == nil {
		t.Fatal("Control(cancel) succeeded after its write failed")
	}
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed {
		t.Fatalf("blocked cancel terminal result = %+v, want failed", result)
	}
	_ = session.Close(context.Background())
}

func TestTerminalNonInterruptedTurnDoesNotSynthesizeAppliedCancel(t *testing.T) {
	for _, status := range []string{"completed", "failed"} {
		t.Run(status, func(t *testing.T) {
			process := newFakeNativeProcess()
			session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
			_ = completeOpen(t, process, session)
			completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Reject stale cancel.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
			errResult := make(chan error, 1)
			receiptResult := make(chan harness.ControlReceipt, 1)
			go func() {
				receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "command-stale", Kind: harness.ControlCancel})
				receiptResult <- receipt
				errResult <- err
			}()
			_ = process.nextRequest(t)
			process.emitJSON(t, completedNotification("thread-1", "turn-1", status, validTaskResultJSON(t, "progress")))
			if err := <-errResult; err == nil {
				t.Fatal("Control(cancel) succeeded for non-interrupted terminal turn")
			}
			if receipt := <-receiptResult; receipt.Outcome == harness.ControlApplied {
				t.Fatalf("cancel receipt = %+v, want failed", receipt)
			}
		})
	}
}

func TestInterruptedTerminalUsesIssuedCancelIntentBeforeWriteHook(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Classify interrupt race.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	native := session.(*nativeSession)
	attemptID := native.beginCancelAttempt()
	native.markCancelAttemptWritten(attemptID)
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled {
		t.Fatalf("interrupted result = %+v, want cancellation from issued intent", result)
	}
}

func TestInterruptedTurnWithoutLocalCancelIsNotCancellation(t *testing.T) {
	process := newFakeNativeProcess()
	adapter := fakeAdapter(process)
	session := startNativeSession(t, adapter, &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Observe interruption.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed {
		t.Fatalf("externally interrupted result = %+v, want failed", result)
	}
}

func TestLateValidInterruptRejectionRetiresUnknownCancelAttempt(t *testing.T) {
	process := newFakeNativeProcess()
	sink := &recordingHarnessSink{}
	session := startNativeSession(t, fakeAdapter(process), sink)
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Reject a late cancellation.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	controlDone := make(chan error, 1)
	go func() {
		_, err := session.Control(ctx, harness.ControlRequest{CommandID: "late-rejection", Kind: harness.ControlCancel})
		controlDone <- err
	}()
	interrupt := process.nextRequest(t)
	if err := <-controlDone; err == nil {
		t.Fatal("Control(cancel) succeeded after its response timed out")
	}
	if err := process.emitJSON(t, interruptRejectionResponse(interrupt.ID)); err != nil {
		t.Fatalf("late valid rejection = %v", err)
	}
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultFailed {
		t.Fatalf("late rejection terminal result = %+v, want failed", result)
	}
	if !sink.hasDiagnostic("late_response") {
		t.Fatalf("late rejection was not retained as diagnostic: %#v", sink.events)
	}
}

func TestMalformedLateInterruptErrorDoesNotRejectUnknownCancelAttempt(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Preserve unknown cancellation.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	controlDone := make(chan error, 1)
	go func() {
		_, err := session.Control(ctx, harness.ControlRequest{CommandID: "malformed-late-error", Kind: harness.ControlCancel})
		controlDone <- err
	}()
	interrupt := process.nextRequest(t)
	if err := <-controlDone; err == nil {
		t.Fatal("Control(cancel) succeeded after its response timed out")
	}
	if err := process.emitJSON(t, map[string]any{
		"jsonrpc": "2.0",
		"id":      interrupt.ID,
		"error":   map[string]any{"message": "missing code"},
	}); err != nil {
		t.Fatalf("malformed late error = %v", err)
	}
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled {
		t.Fatalf("malformed late error terminal result = %+v, want unknown-written cancellation", result)
	}
}

func TestInterruptedTerminalReleasesWaitTurnBeforeCancelAcknowledgement(t *testing.T) {
	process := newFakeNativeProcess()
	session := startNativeSession(t, fakeAdapter(process), &recordingHarnessSink{})
	_ = completeOpen(t, process, session)
	completeTurnStart(t, process, session, harness.TurnRequest{Goal: "Resolve cancel after terminal observation.", Context: json.RawMessage(`{"snapshot":"canonical"}`)})
	controlDone := make(chan error, 1)
	go func() {
		_, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-1", Kind: harness.ControlCancel})
		controlDone <- err
	}()
	interrupt := process.nextRequest(t)
	process.emitJSON(t, completedNotification("thread-1", "turn-1", "interrupted", validTaskResultJSON(t, "progress")))
	if err := session.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v, want terminal observation before cancel acknowledgement", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("final Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled || !result.Process.Terminated {
		t.Fatalf("final interrupted result = %+v, want cancelled physical final result", result)
	}
	if err := <-controlDone; err == nil {
		t.Fatal("cancel control unexpectedly succeeded without an interrupt acknowledgement")
	}
	_ = interrupt
}
