package pi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

func TestAdapterStagesPiRPCAndRequiresSettledExplicitTaskResult(t *testing.T) {
	process := newFakeNativeProcess()
	var invocation execution.Invocation
	adapter := &Adapter{
		executable: "pi-test",
		startProcess: func(_ context.Context, got execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			invocation = got
			process.sink = sink
			if got.PersistProcess != nil {
				pid, identity := process.ProcessDetails()
				if err := got.PersistProcess(pid, identity); err != nil {
					return nil, err
				}
			}
			return process, nil
		},
	}
	process.onWrite = func(request Request) {
		switch request.Type {
		case CommandGetState:
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "get_state", "success": true, "data": map[string]any{"sessionId": "pi-1", "sessionFile": "C:/sessions/pi-1.jsonl"}})
		case CommandPrompt:
			// Native events may arrive before prompt acceptance. Settlement alone
			// remains pending until the matching response is observed.
			process.emitJSON(t, map[string]any{"type": "agent_start"})
			process.emitJSON(t, map[string]any{"type": "message_end", "message": assistantTaskResultMessage(t)})
			process.emitJSON(t, map[string]any{"type": "agent_end", "messages": []any{}, "willRetry": false})
			process.emitJSON(t, map[string]any{"type": "agent_settled"})
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "prompt", "success": true})
		default:
			t.Fatalf("unexpected request: %+v", request)
		}
	}
	sink := &recordingHarnessSink{}
	var persistedPID int
	var persistedIdentity string
	session := startPiSession(t, adapter, harness.StartRequest{
		Workspace: t.TempDir(),
		Invocation: execution.Invocation{Args: []string{
			"--provider", "openai", "--model", "gpt-5.6", "--session-dir", "C:/sessions",
		}},
		PersistProcess: func(pid int, identity string) error {
			persistedPID, persistedIdentity = pid, identity
			return nil
		},
	}, sink)
	if got, want := strings.Join(invocation.Args, " "), "--mode rpc --provider openai --model gpt-5.6 --session-dir C:/sessions"; got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
	if invocation.InitialInput != nil || invocation.CloseInputAfterInitial {
		t.Fatalf("invocation received legacy input: %+v", invocation)
	}
	if persistedPID != 42 || persistedIdentity != "test:42" {
		t.Fatalf("persisted process = (%d, %q)", persistedPID, persistedIdentity)
	}

	handle := openPiSession(t, session)
	if handle.ID != "pi-1" || handle.Filename != "C:/sessions/pi-1.jsonl" {
		t.Fatalf("Open() handle = %+v", handle)
	}
	if sink.has(harness.EventSessionStarted) {
		t.Fatal("Open emitted session_started before handle persistence barrier")
	}
	if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make bounded progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if err := session.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v", err)
	}
	if !sink.has(harness.EventSessionStarted) || !sink.has(harness.EventTaskResult) {
		t.Fatalf("events = %#v, want session_started and task_result", sink.events)
	}
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultSucceeded || result.Semantic == nil || result.Semantic.Summary != "made bounded progress" {
		t.Fatalf("result = %+v, want semantic success", result)
	}
}

func TestControlCancelSendsClearQueueBeforeAbortAndNeedsSettlement(t *testing.T) {
	process := newFakeNativeProcess()
	adapter := fakePiAdapter(process)
	process.onWrite = func(request Request) {
		switch request.Type {
		case CommandGetState:
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "get_state", "success": true, "data": map[string]any{"sessionId": "pi-1", "sessionFile": "C:/sessions/pi-1.jsonl"}})
		case CommandPrompt:
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "prompt", "success": true})
			process.emitJSON(t, map[string]any{"type": "agent_start"})
		case CommandClearQueue:
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "clear_queue", "success": true, "data": map[string]any{"steering": []any{}, "followUp": []any{}}})
		case CommandAbort:
			// Abort's response may be delayed until after native settlement.
			process.emitJSON(t, map[string]any{"type": "agent_end", "messages": []any{}, "willRetry": false})
			process.emitJSON(t, map[string]any{"type": "agent_settled"})
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "abort", "success": true})
		}
	}
	session := startPiSession(t, adapter, harness.StartRequest{Workspace: t.TempDir()}, &recordingHarnessSink{})
	_ = openPiSession(t, session)
	if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make bounded progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-1", Kind: harness.ControlCancel})
	if err != nil || receipt.Outcome != harness.ControlApplied {
		t.Fatalf("Control(cancel) = %+v, %v", receipt, err)
	}
	if err := session.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v", err)
	}
	if writes := process.requestTypes(); strings.Join(writes, ",") != "get_state,prompt,clear_queue,abort" {
		t.Fatalf("request order = %v, want get_state,prompt,clear_queue,abort", writes)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil || result.Kind != harness.ResultCancelled {
		t.Fatalf("Wait() = %+v, %v; want cancelled", result, err)
	}
}

func TestWaitTurnRejectsMalformedOrNonStructuredFinalContent(t *testing.T) {
	for _, test := range []struct {
		name    string
		message map[string]any
	}{
		{name: "plain text", message: map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "made progress"}}, "stopReason": "stop"}},
		{name: "fenced json", message: map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "```json\n{}\n```"}}, "stopReason": "stop"}},
		{name: "multiple blocks", message: map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": validTaskResultJSON(t)}, map[string]any{"type": "text", "text": "extra"}}, "stopReason": "stop"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			process := newFakeNativeProcess()
			process.onWrite = func(request Request) {
				switch request.Type {
				case CommandGetState:
					process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "get_state", "success": true, "data": map[string]any{"sessionId": "pi-1", "sessionFile": "C:/sessions/pi-1.jsonl"}})
				case CommandPrompt:
					process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "prompt", "success": true})
					process.emitJSON(t, map[string]any{"type": "agent_start"})
					process.emitJSON(t, map[string]any{"type": "message_end", "message": test.message})
					process.emitJSON(t, map[string]any{"type": "agent_end", "messages": []any{}, "willRetry": false})
					process.emitJSON(t, map[string]any{"type": "agent_settled"})
				}
			}
			session := startPiSession(t, fakePiAdapter(process), harness.StartRequest{Workspace: t.TempDir()}, &recordingHarnessSink{})
			_ = openPiSession(t, session)
			if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make bounded progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
				t.Fatalf("StartTurn() error = %v", err)
			}
			if err := session.WaitTurn(context.Background()); !errors.Is(err, ErrInvalidTaskResult) {
				t.Fatalf("WaitTurn() error = %v, want ErrInvalidTaskResult", err)
			}
			process.finish(execution.Result{PID: 42, ExitCode: 0})
			result, err := session.Wait(context.Background())
			if err != nil || result.Kind != harness.ResultFailed || result.Reason == nil || *result.Reason != "missing_result" {
				t.Fatalf("Wait() = %+v, %v; want missing_result failure", result, err)
			}
		})
	}
}

func TestCloseRetriesAfterTerminationFailure(t *testing.T) {
	process := newFakeNativeProcess()
	process.terminateErrors = []error{errors.New("temporary termination failure"), nil}
	session := startPiSession(t, fakePiAdapter(process), harness.StartRequest{Workspace: t.TempDir()}, &recordingHarnessSink{})
	if err := session.Close(context.Background()); err == nil {
		t.Fatal("first Close() error = nil, want termination failure")
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if got := process.terminateCount(); got != 2 {
		t.Fatalf("Terminate calls = %d, want 2", got)
	}
}

func TestStartRejectsRPCTransportOverrideBeforeLaunchingProcess(t *testing.T) {
	called := false
	adapter := &Adapter{
		executable: "pi-test",
		startProcess: func(_ context.Context, _ execution.Invocation, _ execution.Sink) (nativeProcess, error) {
			called = true
			return newFakeNativeProcess(), nil
		},
	}
	_, err := adapter.Start(context.Background(), harness.StartRequest{
		Workspace:  t.TempDir(),
		Invocation: execution.Invocation{Args: []string{"--mode=text"}},
	}, &recordingHarnessSink{})
	if err == nil || !strings.Contains(err.Error(), "must not override") {
		t.Fatalf("Start() error = %v, want mode override rejection", err)
	}
	if called {
		t.Fatal("Start() launched a process despite a mode override")
	}
}

func TestStartRejectsHistoryMutatingArgumentsBeforeLaunchingProcess(t *testing.T) {
	for _, argument := range []string{
		"--continue", "--continue=latest", "-c",
		"--resume", "--resume=session-1", "-r",
		"--session", "--session=session-1",
		"--session-id", "--session-id=session-1",
		"--fork", "--fork=session-1",
		"--no-session",
	} {
		t.Run(argument, func(t *testing.T) {
			called := false
			adapter := &Adapter{
				executable: "pi-test",
				startProcess: func(_ context.Context, _ execution.Invocation, _ execution.Sink) (nativeProcess, error) {
					called = true
					return newFakeNativeProcess(), nil
				},
			}
			_, err := adapter.Start(context.Background(), harness.StartRequest{
				Workspace:  t.TempDir(),
				Invocation: execution.Invocation{Args: []string{argument}},
			}, &recordingHarnessSink{})
			if err == nil || !strings.Contains(err.Error(), "not allowed") {
				t.Fatalf("Start() error = %v, want history/session flag rejection", err)
			}
			if called {
				t.Fatal("Start() launched a process despite a prohibited history/session flag")
			}
		})
	}
}

func TestCloseBoundsMissingClearQueueAcknowledgementThenTerminates(t *testing.T) {
	process := newFakeNativeProcess()
	adapter := fakePiAdapter(process)
	adapter.cancelTimeout = 10 * time.Millisecond
	process.onWrite = func(request Request) {
		switch request.Type {
		case CommandGetState:
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "get_state", "success": true, "data": map[string]any{"sessionId": "pi-1", "sessionFile": "C:/sessions/pi-1.jsonl"}})
		case CommandPrompt:
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "prompt", "success": true})
			process.emitJSON(t, map[string]any{"type": "agent_start"})
		case CommandClearQueue:
			// The missing acknowledgement must not block Close forever or allow
			// abort to be written out of the documented clear_queue ordering.
		}
	}
	session := startPiSession(t, adapter, harness.StartRequest{Workspace: t.TempDir()}, &recordingHarnessSink{})
	_ = openPiSession(t, session)
	if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make bounded progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v, want process cleanup despite missing native ACK", err)
	}
	if writes := process.requestTypes(); strings.Join(writes, ",") != "get_state,prompt,clear_queue" {
		t.Fatalf("request order = %v, want clear_queue only before fallback", writes)
	}
	if got := process.terminateCount(); got != 1 {
		t.Fatalf("Terminate calls = %d, want one fallback cleanup", got)
	}
	result, err := session.Wait(context.Background())
	if err != nil || result.Kind != harness.ResultFailed {
		t.Fatalf("Wait() = %+v, %v; want failed unknown outcome after unacknowledged cancel", result, err)
	}
}

func TestCloseHonorsCallerCancellationWhileBoundedCleanupContinues(t *testing.T) {
	process := newFakeNativeProcess()
	adapter := fakePiAdapter(process)
	adapter.cancelTimeout = 10 * time.Millisecond
	clearWritten := make(chan struct{})
	process.onWrite = func(request Request) {
		switch request.Type {
		case CommandGetState:
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "get_state", "success": true, "data": map[string]any{"sessionId": "pi-1", "sessionFile": "C:/sessions/pi-1.jsonl"}})
		case CommandPrompt:
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "prompt", "success": true})
			process.emitJSON(t, map[string]any{"type": "agent_start"})
		case CommandClearQueue:
			close(clearWritten)
		}
	}
	session := startPiSession(t, adapter, harness.StartRequest{Workspace: t.TempDir()}, &recordingHarnessSink{})
	_ = openPiSession(t, session)
	if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make bounded progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	closeResult := make(chan error, 1)
	go func() { closeResult <- session.Close(ctx) }()
	<-clearWritten
	cancel()
	if err := <-closeResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("Close() error = %v, want caller cancellation", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil || result.Kind != harness.ResultFailed {
		t.Fatalf("Wait() = %+v, %v; want bounded cleanup failure", result, err)
	}
	if got := process.terminateCount(); got != 1 {
		t.Fatalf("Terminate calls = %d, want bounded cleanup fallback", got)
	}
}

func TestAcceptedAbortWithProcessExitBeforeSettlementRemainsCancelled(t *testing.T) {
	process := newFakeNativeProcess()
	process.onWrite = func(request Request) {
		switch request.Type {
		case CommandGetState:
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "get_state", "success": true, "data": map[string]any{"sessionId": "pi-1", "sessionFile": "C:/sessions/pi-1.jsonl"}})
		case CommandPrompt:
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "prompt", "success": true})
			process.emitJSON(t, map[string]any{"type": "agent_start"})
		case CommandClearQueue:
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "clear_queue", "success": true, "data": map[string]any{"steering": []any{}, "followUp": []any{}}})
		case CommandAbort:
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "abort", "success": true})
			process.finish(execution.Result{PID: 42, Terminated: true, OutputTruncated: true})
		}
	}
	session := startPiSession(t, fakePiAdapter(process), harness.StartRequest{Workspace: t.TempDir()}, &recordingHarnessSink{})
	_ = openPiSession(t, session)
	if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make bounded progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if receipt, err := session.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-1", Kind: harness.ControlCancel}); err != nil || receipt.Outcome != harness.ControlApplied {
		t.Fatalf("Control(cancel) = %+v, %v", receipt, err)
	}
	if err := session.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v, want accepted cancellation", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil || result.Kind != harness.ResultCancelled {
		t.Fatalf("Wait() = %+v, %v; want cancelled rather than process failure", result, err)
	}
}

func TestDeliberateCloseAfterSettledTurnDoesNotDowngradeValidResult(t *testing.T) {
	process := newFakeNativeProcess()
	process.onWrite = func(request Request) {
		switch request.Type {
		case CommandGetState:
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "get_state", "success": true, "data": map[string]any{"sessionId": "pi-1", "sessionFile": "C:/sessions/pi-1.jsonl"}})
		case CommandPrompt:
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "prompt", "success": true})
			process.emitJSON(t, map[string]any{"type": "agent_start"})
			process.emitJSON(t, map[string]any{"type": "message_end", "message": assistantTaskResultMessage(t)})
			process.emitJSON(t, map[string]any{"type": "agent_end", "messages": []any{}, "willRetry": false})
			process.emitJSON(t, map[string]any{"type": "agent_settled"})
		}
	}
	session := startPiSession(t, fakePiAdapter(process), harness.StartRequest{Workspace: t.TempDir()}, &recordingHarnessSink{})
	_ = openPiSession(t, session)
	if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make bounded progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if err := session.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v", err)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil || result.Kind != harness.ResultSucceeded || result.Semantic == nil {
		t.Fatalf("Wait() = %+v, %v; want retained valid result", result, err)
	}
}

func TestMalformedRecordAfterSemanticTurnInvalidatesFinalWaitResult(t *testing.T) {
	process := newFakeNativeProcess()
	process.onWrite = func(request Request) {
		switch request.Type {
		case CommandGetState:
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "get_state", "success": true, "data": map[string]any{"sessionId": "pi-1", "sessionFile": "C:/sessions/pi-1.jsonl"}})
		case CommandPrompt:
			process.emitJSON(t, map[string]any{"type": "response", "id": request.ID, "command": "prompt", "success": true})
			process.emitJSON(t, map[string]any{"type": "agent_start"})
			process.emitJSON(t, map[string]any{"type": "message_end", "message": assistantTaskResultMessage(t)})
			process.emitJSON(t, map[string]any{"type": "agent_end", "messages": []any{}, "willRetry": false})
			process.emitJSON(t, map[string]any{"type": "agent_settled"})
		}
	}
	session := startPiSession(t, fakePiAdapter(process), harness.StartRequest{Workspace: t.TempDir()}, &recordingHarnessSink{})
	_ = openPiSession(t, session)
	if err := session.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make bounded progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if err := session.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v", err)
	}
	process.emitRaw(t, []byte("{malformed}\n"))
	process.finish(execution.Result{PID: 42, ExitCode: 0})
	result, err := session.Wait(context.Background())
	if err != nil || result.Kind != harness.ResultFailed || result.Semantic != nil {
		t.Fatalf("Wait() = %+v, %v; want final failure after malformed record", result, err)
	}
}

func TestProbeRemainsNativeUnverifiedWithAllExecutableCapabilitiesFalse(t *testing.T) {
	runner := fakeCommandRunner{outputs: map[string][]byte{
		"--version": []byte("0.85.1\n"),
		"--help":    []byte("Options:\n  --mode <mode> Output mode: text, json, or rpc\n"),
	}}
	result, err := Probe(context.Background(), "pi-test", runner)
	if !errors.Is(err, harness.ErrNativeUnverified) {
		t.Fatalf("Probe() error = %v, want ErrNativeUnverified", err)
	}
	if !result.VersionKnown || !result.TransportKnown || result.Capabilities.Verified || result.Capabilities.Start || result.Capabilities.Events || result.Capabilities.Cancel || result.Capabilities.Resume || result.Capabilities.Usage != harness.UsageUnknown {
		t.Fatalf("capabilities = %+v, want fully executable-false projection", result.Capabilities)
	}
	if err := result.Capabilities.Validate(); err != nil {
		t.Fatalf("Capabilities.Validate() error = %v", err)
	}
}

func startPiSession(t *testing.T, adapter *Adapter, request harness.StartRequest, sink harness.EventSink) *nativeSession {
	t.Helper()
	started, err := adapter.Start(context.Background(), request, sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	session, ok := started.(*nativeSession)
	if !ok {
		t.Fatalf("Start() session = %T, want *nativeSession", started)
	}
	return session
}

func openPiSession(t *testing.T, session *nativeSession) harness.NativeSessionHandle {
	t.Helper()
	handle, err := session.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	return handle
}

func fakePiAdapter(process *fakeNativeProcess) *Adapter {
	return &Adapter{
		executable: "pi-test",
		startProcess: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (nativeProcess, error) {
			process.sink = sink
			return process, nil
		},
	}
}

func assistantTaskResultMessage(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"role": "assistant",
		"content": []any{
			map[string]any{"type": "text", "text": validTaskResultJSON(t)},
		},
		"stopReason": "stop",
	}
}

type fakeNativeProcess struct {
	mutex           sync.Mutex
	sink            execution.Sink
	onWrite         func(Request)
	writes          []Request
	result          execution.Result
	resultDone      chan struct{}
	resultOnce      sync.Once
	sequence        uint64
	terminateErrors []error
	terminations    int
}

func newFakeNativeProcess() *fakeNativeProcess {
	return &fakeNativeProcess{resultDone: make(chan struct{})}
}

func (process *fakeNativeProcess) WriteInputContext(_ context.Context, input []byte) error {
	var request Request
	if err := json.Unmarshal(bytesTrimLine(input), &request); err != nil {
		return err
	}
	process.mutex.Lock()
	process.writes = append(process.writes, request)
	onWrite := process.onWrite
	process.mutex.Unlock()
	if onWrite != nil {
		onWrite(request)
	}
	return nil
}

func bytesTrimLine(input []byte) []byte {
	return []byte(strings.TrimSuffix(string(input), "\n"))
}

func (process *fakeNativeProcess) Wait() execution.Result {
	<-process.resultDone
	process.mutex.Lock()
	defer process.mutex.Unlock()
	return process.result
}

func (process *fakeNativeProcess) Terminate(_ context.Context, _ time.Duration) error {
	process.mutex.Lock()
	process.terminations++
	var err error
	if len(process.terminateErrors) > 0 {
		err = process.terminateErrors[0]
		process.terminateErrors = process.terminateErrors[1:]
	}
	process.mutex.Unlock()
	if err != nil {
		return err
	}
	process.finish(execution.Result{PID: 42, Terminated: true, OutputTruncated: true})
	return nil
}

func (process *fakeNativeProcess) ProcessDetails() (int, string) { return 42, "test:42" }

func (process *fakeNativeProcess) emitJSON(t *testing.T, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal native JSON: %v", err)
	}
	process.mutex.Lock()
	process.sequence++
	sequence := process.sequence
	sink := process.sink
	process.mutex.Unlock()
	if sink == nil {
		t.Fatal("native output sink is nil")
	}
	if err := sink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: sequence, At: time.Now().UTC(), Data: append(encoded, '\n')}); err != nil {
		t.Fatalf("emit native JSON: %v", err)
	}
}

func (process *fakeNativeProcess) emitRaw(t *testing.T, data []byte) {
	t.Helper()
	process.mutex.Lock()
	process.sequence++
	sequence := process.sequence
	sink := process.sink
	process.mutex.Unlock()
	if sink == nil {
		t.Fatal("native output sink is nil")
	}
	if err := sink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: sequence, At: time.Now().UTC(), Data: append([]byte(nil), data...)}); err == nil {
		t.Fatal("emit malformed native output error = nil")
	}
}

func (process *fakeNativeProcess) finish(result execution.Result) {
	process.resultOnce.Do(func() {
		process.mutex.Lock()
		process.result = result
		process.mutex.Unlock()
		close(process.resultDone)
	})
}

func (process *fakeNativeProcess) requestTypes() []string {
	process.mutex.Lock()
	defer process.mutex.Unlock()
	values := make([]string, 0, len(process.writes))
	for _, request := range process.writes {
		values = append(values, string(request.Type))
	}
	return values
}

func (process *fakeNativeProcess) terminateCount() int {
	process.mutex.Lock()
	defer process.mutex.Unlock()
	return process.terminations
}

type recordingHarnessSink struct {
	mutex  sync.Mutex
	events []harness.Event
	err    error
}

func (sink *recordingHarnessSink) Handle(_ context.Context, event harness.Event) error {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	sink.events = append(sink.events, event)
	return sink.err
}

func (sink *recordingHarnessSink) has(kind harness.EventKind) bool {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()
	for _, event := range sink.events {
		if event.Kind == kind {
			return true
		}
	}
	return false
}

type fakeCommandRunner struct {
	outputs map[string][]byte
	err     error
}

func (runner fakeCommandRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if runner.err != nil {
		return nil, runner.err
	}
	return runner.outputs[strings.Join(args, " ")], nil
}
