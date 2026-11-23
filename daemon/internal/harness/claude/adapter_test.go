package claude

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const candidateTestSessionID = "11111111-1111-4111-8111-111111111111"

func TestCandidateAdapterStartUsesOwnerSuppliedProcessSeam(t *testing.T) {
	process := newCandidateFakeProcess()
	adapter := &CandidateAdapter{
		executable:   "claude-test",
		newSessionID: func() (string, error) { return candidateTestSessionID, nil },
		startProcess: func(context.Context, execution.Invocation, execution.Sink) (candidateProcess, error) {
			t.Fatal("adapter default process starter was used despite an owner-supplied seam")
			return nil, nil
		},
	}
	request := candidateStartRequest(t)
	called := false
	request.StartProcess = func(_ context.Context, _ execution.Invocation, sink execution.Sink) (harness.NativeProcess, error) {
		called = true
		process.sink = sink
		return process, nil
	}
	raw, err := adapter.Start(context.Background(), request, &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() with owner process seam: %v", err)
	}
	if !called {
		t.Fatal("adapter did not use the owner-supplied process seam")
	}
	session, ok := raw.(*candidateSession)
	if !ok {
		t.Fatalf("Start() session = %T, want *candidateSession", raw)
	}
	session.cancel()
}

func TestCandidateAdapterStartWithoutOwnerSeamUsesAdapterStarter(t *testing.T) {
	process := newCandidateFakeProcess()
	usedAdapterStarter := false
	adapter := &CandidateAdapter{
		executable:   "claude-test",
		newSessionID: func() (string, error) { return candidateTestSessionID, nil },
		startProcess: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (candidateProcess, error) {
			usedAdapterStarter = true
			process.sink = sink
			return process, nil
		},
	}
	raw, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() without owner process seam: %v", err)
	}
	if !usedAdapterStarter {
		t.Fatal("adapter did not use its configured default process starter")
	}
	session, ok := raw.(*candidateSession)
	if !ok {
		t.Fatalf("Start() session = %T, want *candidateSession", raw)
	}
	session.cancel()
}

func TestClaudeCandidateStagesFreshTransportAndBarriers(t *testing.T) {
	process := newCandidateFakeProcess()
	var invocation execution.Invocation
	persisted := false
	adapter := newCandidateTestAdapter(process, &invocation)
	session, err := adapter.Start(context.Background(), harness.StartRequest{
		Workspace: t.TempDir(),
		Invocation: execution.Invocation{
			Env: []string{"PATH=test"},
		},
		PersistProcess: func(pid int, identity string) error {
			persisted = pid == 42 && identity == "test:42"
			return nil
		},
	}, &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	if !persisted {
		t.Fatal("Start() did not persist the process owner before exposing the session")
	}
	if got, want := invocation.Program, "claude-test"; got != want {
		t.Fatalf("program = %q, want %q", got, want)
	}
	encodedSchema, err := candidateStructuredOutputSchema()
	if err != nil {
		t.Fatalf("structured output schema: %v", err)
	}
	wantArgs := []string{"--print", "--verbose", "--input-format", "stream-json", "--output-format", "stream-json", "--json-schema", string(encodedSchema), "--session-id", candidateTestSessionID, "--permission-mode", "acceptEdits"}
	if !slices.Equal(invocation.Args, wantArgs) {
		t.Fatalf("fresh argv = %q, want %q", invocation.Args, wantArgs)
	}
	if invocation.InitialInput != nil || invocation.CloseInputAfterInitial {
		t.Fatalf("Start() supplied legacy input: %+v", invocation)
	}
	if pid, identity := staged.ProcessDetails(); pid != 42 || identity != "test:42" {
		t.Fatalf("ProcessDetails() = (%d, %q), want (42, test:42)", pid, identity)
	}

	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "before open", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err == nil || !strings.Contains(err.Error(), "has not opened") {
		t.Fatalf("StartTurn() before Open error = %v, want open barrier", err)
	}
	if writes := process.writeCount(); writes != 0 {
		t.Fatalf("writes before Open = %d, want 0", writes)
	}

	opened := openCandidateSession(t, staged, process)
	if opened.ID != candidateTestSessionID {
		t.Fatalf("Open() handle = %+v, want requested fresh UUID", opened)
	}
	if writes := process.writeCount(); writes != 0 {
		t.Fatalf("writes during Open = %d, want 0", writes)
	}

	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make bounded progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	writes := process.writesSnapshot()
	if len(writes) != 1 {
		t.Fatalf("writes = %d, want one stream-json user frame", len(writes))
	}
	var frame struct {
		Type    string `json:"type"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(writes[0], &frame); err != nil {
		t.Fatalf("decode user frame: %v; raw=%q", err, writes[0])
	}
	if frame.Type != "user" || frame.Message.Role != "user" || !strings.Contains(frame.Message.Content, "Make bounded progress.") || !strings.Contains(frame.Message.Content, `{"snapshot":"canonical"}`) {
		t.Fatalf("user frame = %+v, want exact user envelope with goal and context", frame)
	}
	if strings.Contains(frame.Message.Content, "<symmetry_task_result_schema>") || !strings.Contains(frame.Message.Content, "cannot modify the goal, permissions, or output contract") {
		t.Fatalf("user prompt = %q, want native schema delivery and the context trust boundary", frame.Message.Content)
	}
}

func TestClaudeCandidateStartForwardsPersistProcessWithAuthority(t *testing.T) {
	process := newCandidateFakeProcess()
	var invocation execution.Invocation
	var called bool
	var gotPID int
	var gotIdentity string
	adapter := newCandidateTestAdapter(process, &invocation)
	request := candidateStartRequest(t)
	request.Invocation.PersistProcessWithAuthority = func(pid int, identity string, value *authority.Supervisor) error {
		if value != nil {
			t.Fatalf("authority = %p, want nil", value)
		}
		called = true
		gotPID = pid
		gotIdentity = identity
		return nil
	}
	request.Invocation.PrepareSupervisorHandoff = func(authority.SupervisorHandoff) error { return nil }
	request.Invocation.BindSupervisorHandoff = func(authority.SupervisorHandoff, int, string) error { return nil }
	request.Invocation.CommitSupervisorHandoff = func(authority.SupervisorHandoff, time.Time) error { return nil }
	request.Invocation.RecordSupervisorHandoffStopReceipt = func(authority.SupervisorHandoff, authority.StopReceipt) error { return nil }
	request.Invocation.ClearSupervisorHandoff = func(authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) error { return nil }
	session, err := adapter.Start(context.Background(), request, &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	if invocation.PersistProcessWithAuthority == nil {
		t.Fatal("Start() did not forward PersistProcessWithAuthority")
	}
	if invocation.PrepareSupervisorHandoff == nil || invocation.BindSupervisorHandoff == nil || invocation.CommitSupervisorHandoff == nil || invocation.RecordSupervisorHandoffStopReceipt == nil || invocation.ClearSupervisorHandoff == nil {
		t.Fatal("Start() did not forward complete supervisor handoff callbacks")
	}
	if err := invocation.PersistProcessWithAuthority(42, "test:42", nil); err != nil {
		t.Fatalf("forwarded PersistProcessWithAuthority() error = %v", err)
	}
	if !called || gotPID != 42 || gotIdentity != "test:42" {
		t.Fatalf("callback = called:%t pid:%d identity:%q, want called with (42, test:42)", called, gotPID, gotIdentity)
	}
}

func TestClaudeCandidateOpenDefersSystemInitUntilFirstTurn(t *testing.T) {
	t.Run("open returns before init and matching init admits the result", func(t *testing.T) {
		process := newCandidateFakeProcess()
		adapter := newCandidateTestAdapter(process, nil)
		session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		cleanupCandidateSession(t, session)
		staged := session.(harness.StagedSession)
		opened, err := staged.Open(context.Background())
		if err != nil || opened.ID != candidateTestSessionID {
			t.Fatalf("Open() = %+v, %v; want requested UUID without system/init", opened, err)
		}
		if writes := process.writeCount(); writes != 0 {
			t.Fatalf("writes during Open = %d, want 0", writes)
		}
		if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "deferred init", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
			t.Fatalf("StartTurn() error = %v", err)
		}
		if err := process.emitJSON(`{"type":"system","subtype":"init","session_id":"` + candidateTestSessionID + `"}`); err != nil {
			t.Fatalf("emit matching init: %v", err)
		}
		if err := process.emitJSON(candidateResultEnvelope(t, validCandidateTaskResultJSON(t))); err != nil {
			t.Fatalf("emit verified result: %v", err)
		}
		if err := staged.WaitTurn(context.Background()); err != nil {
			t.Fatalf("WaitTurn() error = %v, want verified terminal", err)
		}
		if err := staged.Close(context.Background()); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
		if _, err := staged.Wait(context.Background()); err != nil {
			t.Fatalf("Wait() error = %v", err)
		}
	})

	t.Run("result before init is rejected", func(t *testing.T) {
		process := newCandidateFakeProcess()
		adapter := newCandidateTestAdapter(process, nil)
		session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		cleanupCandidateSession(t, session)
		staged := session.(harness.StagedSession)
		if _, err := staged.Open(context.Background()); err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "missing init", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
			t.Fatalf("StartTurn() error = %v", err)
		}
		if err := process.emitJSON(candidateResultEnvelope(t, validCandidateTaskResultJSON(t))); !errors.Is(err, errCandidateMissingInit) {
			t.Fatalf("result-before-init error = %v, want ErrMissingInit", err)
		}
		if err := staged.WaitTurn(context.Background()); !errors.Is(err, errCandidateMissingInit) {
			t.Fatalf("WaitTurn() error = %v, want ErrMissingInit", err)
		}
	})

	t.Run("wrong init identity fails closed", func(t *testing.T) {
		process := newCandidateFakeProcess()
		adapter := newCandidateTestAdapter(process, nil)
		session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		cleanupCandidateSession(t, session)
		staged := session.(harness.StagedSession)
		if _, err := staged.Open(context.Background()); err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "wrong init", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
			t.Fatalf("StartTurn() error = %v", err)
		}
		if err := process.emitJSON(`{"type":"system","subtype":"init","session_id":"22222222-2222-4222-8222-222222222222"}`); !errors.Is(err, ErrConflictingIdentity) {
			t.Fatalf("wrong-init error = %v, want ErrConflictingIdentity", err)
		}
	})
}

type blockedOpenHooks struct {
	harness.StagedHooks
	entered chan struct{}
	release chan struct{}
}

func (hooks *blockedOpenHooks) OpenNativeSession(ctx context.Context) (harness.NativeSessionHandle, error) {
	close(hooks.entered)
	<-hooks.release
	return hooks.StagedHooks.OpenNativeSession(ctx)
}

func candidateSessionWithBlockedOpen(t *testing.T) (*candidateSession, *candidateFakeProcess, *blockedOpenHooks) {
	t.Helper()
	process := newCandidateFakeProcess()
	adapter := newCandidateTestAdapter(process, nil)
	raw, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatal(err)
	}
	cleanupCandidateSession(t, raw)
	session := raw.(*candidateSession)
	hooks := &blockedOpenHooks{StagedHooks: session, entered: make(chan struct{}), release: make(chan struct{})}
	session.staged = harness.NewStagedDriver(hooks, &session.mutex)
	return session, process, hooks
}

func TestClaudeConcurrentOpenObservesTheInFlightOpenOutcome(t *testing.T) {
	t.Run("in-flight failure rejects instead of returning the UUID", func(t *testing.T) {
		session, process, hooks := candidateSessionWithBlockedOpen(t)
		opened := make(chan error, 1)
		go func() {
			_, err := session.Open(context.Background())
			opened <- err
		}()
		<-hooks.entered
		process.finishResult(execution.Result{PID: 42, ExitCode: 1})
		<-session.resultDone
		_, concurrentErr := session.Open(context.Background())
		close(hooks.release)
		firstErr := <-opened
		if concurrentErr == nil || firstErr == nil || concurrentErr.Error() != firstErr.Error() {
			t.Fatalf("concurrent Open() = %v, first Open() = %v; want the in-flight outcome", concurrentErr, firstErr)
		}
	})
	t.Run("usable in-flight open still returns the requested UUID", func(t *testing.T) {
		session, _, hooks := candidateSessionWithBlockedOpen(t)
		opened := make(chan error, 1)
		go func() {
			_, err := session.Open(context.Background())
			opened <- err
		}()
		<-hooks.entered
		handle, err := session.Open(context.Background())
		close(hooks.release)
		firstErr := <-opened
		if err != nil || handle.ID != candidateTestSessionID || firstErr != nil {
			t.Fatalf("concurrent Open() = %+v, %v; first Open() = %v", handle, err, firstErr)
		}
	})
}

func TestClaudeCandidateRejectsProseTerminalAsSuccess(t *testing.T) {
	process := newCandidateFakeProcess()
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make bounded progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	_ = process.emitJSON(`{"type":"result","subtype":"success","terminal_reason":"completed","is_error":false,"session_id":"` + candidateTestSessionID + `","result":"done"}`)
	if err := staged.WaitTurn(context.Background()); err == nil {
		t.Fatal("WaitTurn() accepted prose terminal as a verified TaskResult")
	}
	if err := staged.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind == harness.ResultSucceeded || result.Semantic != nil {
		t.Fatalf("prose terminal result = %+v, must not be successful semantic progress", result)
	}
	if result.Reason == nil || *result.Reason != protocol.TaskResultReasonMissingResult {
		t.Fatalf("prose terminal reason = %v, want missing_result", result.Reason)
	}
}

func TestClaudeCandidateAcceptsVerifiedTaskResultAndPublishesEvent(t *testing.T) {
	process := newCandidateFakeProcess()
	sink := &recordingCandidateSink{}
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make verified progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	semanticJSON := validCandidateTaskResultJSON(t)
	if err := process.emitJSON(candidateResultEnvelope(t, semanticJSON)); err != nil {
		t.Fatalf("emit verified result: %v", err)
	}
	if err := staged.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v, want verified terminal", err)
	}
	if err := staged.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultSucceeded || result.Semantic == nil || result.Semantic.Summary != "made bounded progress" {
		t.Fatalf("result = %+v, want successful semantic TaskResult", result)
	}
	event, ok := sink.event(harness.EventTaskResult)
	if !ok {
		t.Fatalf("events = %#v, want EventTaskResult", sink.eventsSnapshot())
	}
	var emitted protocol.TaskResult
	if err := json.Unmarshal(event.Payload, &emitted); err != nil {
		t.Fatalf("decode EventTaskResult payload: %v", err)
	}
	if emitted.Summary != result.Semantic.Summary || emitted.ResultID != result.Semantic.ResultID {
		t.Fatalf("EventTaskResult = %+v, want the verified semantic result", emitted)
	}
}

// A terminal record that is already inside the process-output sink when the
// cancellation fence is claimed must not be published as success: the accepted
// cancel owns the turn, and the settled result has to stay cancelled.
func TestClaudeCandidateCancellationFenceOutranksInFlightTerminal(t *testing.T) {
	process := newCandidateFakeProcess()
	// Keep the watcher blocked in Wait so the in-flight terminal, not the
	// process reaper, decides the race under test.
	process.holdTermination = true
	sink := newGatedNativeResultSink()
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make verified progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	emitted := make(chan error, 1)
	go func() {
		emitted <- process.emitJSON(candidateResultEnvelope(t, validCandidateTaskResultJSON(t)))
	}()
	select {
	case <-sink.resultEntered:
	case <-time.After(testHangGuard):
		t.Fatal("native result frame never reached the sink")
	}
	// Claim the cancellation fence while the terminal is still inside the sink.
	// Close waits for the watcher, which stays blocked because this fake holds
	// termination until the test releases it.
	receipts, receiptErrors := cancelAndAwaitTermination(t, session, process)
	close(sink.releaseResult)
	if err := <-emitted; err != nil {
		t.Fatalf("emit result: %v", err)
	}
	process.finish()
	receipt := <-receipts
	if err := <-receiptErrors; err != nil || receipt.Outcome != harness.ControlApplied {
		t.Fatalf("Control(cancel) = receipt=%+v error=%v, want applied", receipt, err)
	}
	if err := staged.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v, want the cancelled turn", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled || result.Semantic != nil {
		t.Fatalf("result = %+v, want a cancelled result without semantic success", result)
	}
	if _, ok := sink.event(harness.EventTaskResult); ok {
		t.Fatal("an in-flight terminal after the cancel fence published a TaskResult event")
	}
}

// gatedNativeResultSink blocks the first native result frame so a test can order
// cancellation against terminal observation.
type gatedNativeResultSink struct {
	*recordingCandidateSink
	resultEntered chan struct{}
	releaseResult chan struct{}
	once          sync.Once
}

func newGatedNativeResultSink() *gatedNativeResultSink {
	return &gatedNativeResultSink{
		recordingCandidateSink: &recordingCandidateSink{},
		resultEntered:          make(chan struct{}),
		releaseResult:          make(chan struct{}),
	}
}

func (sink *gatedNativeResultSink) Handle(ctx context.Context, event harness.Event) error {
	if event.Kind == harness.EventNativeFrame {
		var record struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(event.Payload, &record) == nil && record.Type == "result" {
			sink.once.Do(func() { close(sink.resultEntered) })
			<-sink.releaseResult
		}
	}
	return sink.recordingCandidateSink.Handle(ctx, event)
}

// A direct Close (the daemon closes sessions outside Control) must not turn a
// turn whose terminal already won into a cancellation: cancellation ownership
// would otherwise let a later Control(cancel) answer "applied" while the turn
// settles successfully.
func TestClaudeCandidateCloseAfterTerminalObservationKeepsCancelRejected(t *testing.T) {
	process := newCandidateFakeProcess()
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make verified progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	// Record exactly the state a terminal observation leaves behind before its
	// semantic completion runs: the terminal is observed, the turn is not final.
	observedTerminal := Terminal{SessionID: candidateTestSessionID}
	internal := staged.(*candidateSession)
	internal.mutex.Lock()
	internal.terminal = &observedTerminal
	internal.mutex.Unlock()
	if err := staged.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	internal.mutex.Lock()
	cancelRequested := internal.cancelRequested
	internal.mutex.Unlock()
	if cancelRequested {
		t.Fatal("Close() claimed cancellation for a turn whose terminal was already observed")
	}
	receipt, err := staged.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-1", Kind: harness.ControlCancel})
	if err != nil || receipt.Outcome != harness.ControlRejected {
		t.Fatalf("Control(cancel) = receipt=%+v error=%v, want a rejected late cancel", receipt, err)
	}
}

// A terminal that was observed just before a direct close must keep its own
// outcome: the pending semantic completion settles the turn as successful even
// though the session was explicitly terminated, and the watcher must not
// reclassify that success as a process failure.
func TestClaudeCandidateDirectClosePreservesPendingTerminalOutcome(t *testing.T) {
	process := newCandidateFakeProcess()
	process.holdTermination = true
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make verified progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	internal := staged.(*candidateSession)
	terminal := Terminal{
		SessionID: candidateTestSessionID,
		Result: Result{
			Subtype:          "success",
			TerminalReason:   "completed",
			StructuredOutput: json.RawMessage(validCandidateTaskResultJSON(t)),
		},
	}
	internal.mutex.Lock()
	internal.terminal = &terminal
	internal.mutex.Unlock()
	closeErrors := make(chan error, 1)
	go func() { closeErrors <- staged.Close(context.Background()) }()
	awaitCandidateTermination(t, process, "direct close never reached bounded process termination")
	// The pending terminal completes while the close is still settling.
	internal.completeRecordTerminal(terminal)
	process.finish()
	if err := <-closeErrors; err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultSucceeded || result.Semantic == nil {
		t.Fatalf("result = %+v, want the pending terminal's success to survive a direct close", result)
	}
}

// Cancelling before the deferred system/init arrives is still a cancellation:
// the accepted stop explains the absent identity and the absent terminal.
func TestClaudeCandidateCancelBeforeDeferredInitSettlesCancelled(t *testing.T) {
	process := newCandidateFakeProcess()
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	if _, err := staged.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make verified progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	receipt, err := staged.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-1", Kind: harness.ControlCancel})
	if err != nil || receipt.Outcome != harness.ControlApplied {
		t.Fatalf("Control(cancel) = receipt=%+v error=%v, want applied", receipt, err)
	}
	if err := staged.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v, want the cancelled turn", err)
	}
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled {
		t.Fatalf("result kind = %s summary %q, want cancelled without a missing-init failure", result.Kind, result.Summary)
	}
}

// A direct close before the deferred system/init arrives is the daemon's own
// stop rather than a failed lifecycle: the turn settles cancelled exactly as it
// does for an accepted Control(cancel), and no receipt is involved.
func TestClaudeCandidateDirectCloseBeforeDeferredInitSettlesCancelled(t *testing.T) {
	process := newCandidateFakeProcess()
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	if _, err := staged.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make verified progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if err := staged.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled {
		t.Fatalf("result kind = %s summary %q, want cancelled rather than a missing-init failure", result.Kind, result.Summary)
	}
}

// A terminal that fails interpretation after cancellation claimed the turn must
// not reclassify the run as failed: the accepted stop owns the turn.
func TestClaudeCandidateCancelFenceSupersedesInvalidTerminal(t *testing.T) {
	process := newCandidateFakeProcess()
	process.holdTermination = true
	sink := newGatedNativeResultSink()
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make verified progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	emitted := make(chan error, 1)
	go func() {
		// subtype success without structured_output: the validator rejects it.
		emitted <- process.emitJSON(`{"type":"result","subtype":"success","terminal_reason":"completed","is_error":false,"session_id":"` + candidateTestSessionID + `","result":"prose"}`)
	}()
	select {
	case <-sink.resultEntered:
	case <-time.After(testHangGuard):
		t.Fatal("invalid native result frame never reached the sink")
	}
	receipts, receiptErrors := cancelAndAwaitTermination(t, staged, process)
	close(sink.releaseResult)
	if err := <-emitted; err != nil {
		t.Fatalf("emit result: %v", err)
	}
	process.finish()
	receipt := <-receipts
	if err := <-receiptErrors; err != nil || receipt.Outcome != harness.ControlApplied {
		t.Fatalf("Control(cancel) = receipt=%+v error=%v, want applied", receipt, err)
	}
	if err := staged.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v, want the cancelled turn", err)
	}
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled {
		t.Fatalf("result kind = %s summary %q, want cancelled rather than a terminal-interpretation failure", result.Kind, result.Summary)
	}
}

// A superseded result record must not reach the stateful validator: otherwise its
// retained interpretation error is reported by the next record of the same chunk
// and fails a turn the accepted cancellation already owns.
func TestClaudeCandidateSupersededResultDoesNotPoisonLaterRecords(t *testing.T) {
	process := newCandidateFakeProcess()
	process.holdTermination = true
	sink := newGatedNativeResultSink()
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "Make verified progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	internal := staged.(*candidateSession)
	chunk := `{"type":"result","subtype":"success","terminal_reason":"completed","is_error":false,"session_id":"` + candidateTestSessionID + `","result":"prose"}` + "\n" +
		`{"type":"log","message":"after the superseded terminal"}` + "\n"
	delivered := make(chan error, 1)
	go func() {
		delivered <- internal.handleProcessOutput(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: 7, At: time.Now().UTC(), Data: []byte(chunk)})
	}()
	select {
	case <-sink.resultEntered:
	case <-time.After(testHangGuard):
		t.Fatal("invalid native result frame never reached the sink")
	}
	receipts, receiptErrors := cancelAndAwaitTermination(t, staged, process)
	close(sink.releaseResult)
	if err := <-delivered; err != nil {
		t.Fatalf("output handling error = %v, want the superseded terminal and the following record to stay non-failing", err)
	}
	process.finish()
	receipt := <-receipts
	if err := <-receiptErrors; err != nil || receipt.Outcome != harness.ControlApplied {
		t.Fatalf("Control(cancel) = receipt=%+v error=%v, want applied", receipt, err)
	}
	if err := staged.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v, want the cancelled turn", err)
	}
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled {
		t.Fatalf("result kind = %s summary %q, want cancelled rather than a retained validator failure", result.Kind, result.Summary)
	}
}

// One session owns at most one native turn, and a closed session accepts no
// further turn. Both negative controls fail closed instead of reusing a native
// process whose identity or lifecycle is already settled.
func TestClaudeCandidateRejectsSupersededAndClosedTurns(t *testing.T) {
	process := newCandidateFakeProcess()
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "first turn", Context: json.RawMessage(`{"turn":1}`)}); err != nil {
		t.Fatalf("first StartTurn() error = %v", err)
	}
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "superseding turn", Context: json.RawMessage(`{"turn":2}`)}); err == nil || !strings.Contains(err.Error(), "already owns a turn") {
		t.Fatalf("superseding StartTurn() error = %v, want an owned-turn rejection", err)
	}
	if err := staged.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	// The closed session rejects the turn through its lifecycle fence. The exact
	// message depends on whether the process has already been reaped, so the
	// pinned contract is that the rejection surfaces and no second frame reaches
	// the native process.
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "turn after close", Context: json.RawMessage(`{"turn":3}`)}); err == nil {
		t.Fatal("closed StartTurn() error = nil, want a lifecycle rejection")
	}
	writes := process.writeCount()
	if writes != 1 {
		t.Fatalf("native prompt writes = %d, want exactly the first turn", writes)
	}
}

func TestClaudeCandidateLateCancelAfterTerminalObservationIsRejected(t *testing.T) {
	process := newCandidateFakeProcess()
	sink := newBlockingCandidateSink()
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "observe terminal before publication", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	emitDone := make(chan error, 1)
	go func() {
		emitDone <- process.emitJSON(candidateResultEnvelope(t, validCandidateTaskResultJSON(t)))
	}()
	select {
	case <-sink.taskResultEntered:
	case <-time.After(testHangGuard):
		t.Fatal("terminal result was not held in EventTaskResult delivery")
	}

	receipt, controlErr := staged.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-late", Kind: harness.ControlCancel})
	if controlErr != nil {
		t.Fatalf("late Control(cancel) error = %v, want rejected receipt", controlErr)
	}
	if receipt.Outcome != harness.ControlRejected || receipt.Message != "Claude terminal was already observed; cancellation rejected" {
		t.Fatalf("late cancel receipt = %+v, want clear terminal-observation rejection", receipt)
	}
	if calls, _ := process.terminationDetails(); calls != 0 {
		t.Fatalf("late cancel Terminate calls = %d, want zero", calls)
	}

	close(sink.releaseTaskResult)
	if err := <-emitDone; err != nil {
		t.Fatalf("emit terminal result error = %v", err)
	}
}

func TestClaudeCandidateTerminalValidationFailureRejectsLateCancel(t *testing.T) {
	process := newCandidateFakeProcess()
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "observe terminal failure", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	invalid := `{"type":"result","subtype":"success","terminal_reason":"completed","is_error":true,"session_id":"` + candidateTestSessionID + `","result":"failed"}`
	if err := process.emitJSON(invalid); !errors.Is(err, ErrResultReportedError) {
		t.Fatalf("invalid terminal error = %v, want ErrResultReportedError", err)
	}

	receipt, controlErr := staged.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-after-failure", Kind: harness.ControlCancel})
	if controlErr != nil {
		t.Fatalf("late Control(cancel) after terminal failure error = %v", controlErr)
	}
	if receipt.Outcome != harness.ControlRejected || receipt.Message != "Claude terminal observation failed; cancellation rejected" {
		t.Fatalf("late cancel after terminal failure receipt = %+v, want clear rejection", receipt)
	}
	if calls, _ := process.terminationDetails(); calls != 0 {
		t.Fatalf("late cancel after terminal failure Terminate calls = %d, want zero", calls)
	}
}

func TestClaudeCandidateDoesNotReplayPreReadyOutputAfterStartError(t *testing.T) {
	process := newCandidateFakeProcess()
	startErr := errors.New("post-start launch error")
	sink := &recordingCandidateSink{}
	adapter := newCandidateTestAdapter(process, nil)
	adapter.startProcess = func(_ context.Context, _ execution.Invocation, output execution.Sink) (candidateProcess, error) {
		process.sink = output
		if err := output.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: 1, At: time.Now().UTC(), Data: []byte(`{"type":"system","subtype":"init","session_id":"` + candidateTestSessionID + `"}` + "\n")}); err != nil {
			t.Fatalf("queue pre-ready output: %v", err)
		}
		return process, startErr
	}
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), sink)
	if session == nil || !errors.Is(err, startErr) {
		t.Fatalf("Start() = session=%T error=%v, want retained owner and original start error", session, err)
	}
	cleanupCandidateSession(t, session)
	if events := sink.eventsSnapshot(); len(events) != 0 {
		t.Fatalf("pre-ready events after start error = %#v, want no replay", events)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() retained owner error = %v", err)
	}
}

func TestClaudeCandidateStartTurnCloseRaceCancelsPromptWrite(t *testing.T) {
	process := newCandidateFakeProcess()
	process.blockWrites = true
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	turnResult := make(chan error, 1)
	go func() {
		turnResult <- staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "race", Context: json.RawMessage(`{}`)})
	}()
	select {
	case <-process.writeStarted:
	case <-time.After(testHangGuard):
		t.Fatal("StartTurn() did not reach the fake write")
	}
	closeResult := make(chan error, 1)
	go func() { closeResult <- staged.Close(context.Background()) }()
	select {
	case err := <-turnResult:
		if err == nil {
			t.Fatal("StartTurn() succeeded after Close() fenced the prompt write")
		}
	case <-time.After(testHangGuard):
		t.Fatal("StartTurn() remained blocked after Close() cancellation")
	}
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(testHangGuard):
		t.Fatal("Close() did not complete after cancelling prompt write")
	}
	if writes := process.writeCount(); writes != 0 {
		t.Fatalf("successful prompt writes after close fence = %d, want 0", writes)
	}
}

func TestClaudeCandidateCloseFailureIsStableWithoutReterminating(t *testing.T) {
	process := newCandidateFakeProcess()
	firstErr := errors.New("transient terminate failure")
	process.terminateError = firstErr
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSessionExpectClose(t, session, firstErr)
	if err := session.Close(context.Background()); !errors.Is(err, firstErr) {
		t.Fatalf("first Close() error = %v, want %v", err, firstErr)
	}
	if err := session.Close(context.Background()); !errors.Is(err, firstErr) {
		t.Fatalf("repeated Close() error = %v, want stable %v", err, firstErr)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() after failed close error = %v", err)
	}
	if result.Process.Terminated != true {
		t.Fatalf("final process result = %+v, want terminated owner", result.Process)
	}
	if result.Kind == harness.ResultCancelled {
		t.Fatalf("final result = %+v, must not claim clean cancellation after termination failure", result)
	}
	if calls, _ := process.terminationDetails(); calls != 1 {
		t.Fatalf("Terminate calls = %d, want one actual attempt", calls)
	}
}

func TestClaudeCandidateWatcherTimeoutCanBeReconciled(t *testing.T) {
	process := newCandidateFakeProcess()
	process.holdTermination = true
	ctx, cancel := context.WithCancel(context.Background())
	session := newCandidateSession(ctx, cancel, &recordingCandidateSink{}, candidateTestSessionID)
	session.mutex.Lock()
	session.process = process
	session.processReady = true
	session.mutex.Unlock()
	go session.watchProcess()
	defer process.finish()

	watchContext, watchCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	err, retryable := session.closeOnce(watchContext)
	watchCancel()
	if !errors.Is(err, errCandidateWatcherTimeout) || !retryable {
		t.Fatalf("closeOnce() = (%v, %t), want watcher timeout and retryable=true", err, retryable)
	}

	process.mu.Lock()
	process.holdTermination = false
	process.mu.Unlock()
	err, retryable = session.closeOnce(context.Background())
	if err != nil || retryable {
		t.Fatalf("reconciled closeOnce() = (%v, %t), want nil and retryable=false", err, retryable)
	}
}

func TestCloneClaudeCandidateTaskResultDeepCopiesNestedProtocolFields(t *testing.T) {
	proposal := json.RawMessage(`{"steps":["inspect"]}`)
	reason := protocol.TaskResultReasonUnknownOutcome
	semantic := protocol.TaskResult{
		EvidenceRefs: []string{"evidence-1"},
		Blocker: &protocol.Blocker{
			Kind:        protocol.BlockerDependency,
			WorkItemIDs: []string{"00000000-0000-4000-8000-000000000001"},
		},
		ProposedNextAction: &protocol.NextAction{
			Kind: protocol.NextActionWait,
			Blocker: &protocol.Blocker{
				Kind:        protocol.BlockerDependency,
				WorkItemIDs: []string{"00000000-0000-4000-8000-000000000002"},
			},
		},
		Proposal:    &proposal,
		Reason:      &reason,
		Diagnostics: []protocol.Diagnostic{{Code: "observed", Message: "original"}},
	}
	original := harness.TaskResult{Semantic: &semantic, Reason: &reason}
	cloned := harness.CloneTaskResult(original)
	cloned.Semantic.EvidenceRefs[0] = "changed"
	cloned.Semantic.Blocker.WorkItemIDs[0] = "changed"
	cloned.Semantic.ProposedNextAction.Blocker.WorkItemIDs[0] = "changed"
	(*cloned.Semantic.Proposal)[0] = '['
	cloned.Semantic.Diagnostics[0].Message = "changed"
	*cloned.Reason = protocol.TaskResultReasonCancelled
	if original.Semantic.EvidenceRefs[0] != "evidence-1" || original.Semantic.Blocker.WorkItemIDs[0] == "changed" || original.Semantic.ProposedNextAction.Blocker.WorkItemIDs[0] == "changed" || string(*original.Semantic.Proposal) != `{"steps":["inspect"]}` || original.Semantic.Diagnostics[0].Message != "original" || *original.Reason != protocol.TaskResultReasonUnknownOutcome {
		t.Fatalf("deep clone mutated original: original=%+v", original)
	}
}

func TestClaudeCandidateCancellationUsesBoundedTerminate(t *testing.T) {
	process := newCandidateFakeProcess()
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "cancel me", Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	receipt, err := staged.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-1", Kind: harness.ControlCancel})
	if err != nil {
		t.Fatalf("Control(cancel) error = %v", err)
	}
	if receipt.Outcome != harness.ControlApplied || receipt.Capability != harness.CapabilityCancel {
		t.Fatalf("cancel receipt = %+v, want applied cancel", receipt)
	}
	if calls, grace := process.terminationDetails(); calls != 1 || grace != terminationGrace {
		t.Fatalf("Terminate() = calls=%d grace=%s, want one bounded %s call", calls, grace, terminationGrace)
	}
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != harness.ResultCancelled {
		t.Fatalf("cancel result kind = %q, want cancelled", result.Kind)
	}
}

func TestClaudeCandidateCancelledResultRequiresValidTerminationEvidence(t *testing.T) {
	tests := []struct {
		name          string
		mutate        func(*execution.Result)
		wantCancelled bool
	}{
		{name: "wait error after explicit termination", mutate: func(result *execution.Result) { result.WaitError = errors.New("wait failed") }, wantCancelled: true},
		{name: "sink error", mutate: func(result *execution.Result) { result.SinkError = errors.New("sink failed") }},
		{name: "output error", mutate: func(result *execution.Result) { result.OutputError = errors.New("output failed") }},
		{name: "termination error", mutate: func(result *execution.Result) { result.TerminationError = errors.New("termination failed") }},
		{name: "containment error", mutate: func(result *execution.Result) { result.ContainmentError = errors.New("containment failed") }},
		{name: "truncated output from requested termination", mutate: func(result *execution.Result) { result.OutputTruncated = true }, wantCancelled: true},
		{name: "not terminated", mutate: func(result *execution.Result) { result.Terminated = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			process := newCandidateFakeProcess()
			process.terminateResult = execution.Result{PID: 42, ExitCode: -1, Terminated: true, FinishedAt: time.Now().UTC()}
			test.mutate(&process.terminateResult)
			adapter := newCandidateTestAdapter(process, nil)
			session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			cleanupCandidateSession(t, session)
			staged := session.(harness.StagedSession)
			openCandidateSession(t, staged, process)
			if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "cancel with process evidence", Context: json.RawMessage(`{}`)}); err != nil {
				t.Fatalf("StartTurn() error = %v", err)
			}
			receipt, controlErr := staged.Control(context.Background(), harness.ControlRequest{Kind: harness.ControlCancel})
			if controlErr != nil || receipt.Outcome != harness.ControlApplied {
				t.Fatalf("Control(cancel) = receipt=%+v error=%v, want applied termination request", receipt, controlErr)
			}
			result, waitErr := staged.Wait(context.Background())
			if waitErr != nil {
				t.Fatalf("Wait() error = %v", waitErr)
			}
			if (result.Kind == harness.ResultCancelled) != test.wantCancelled {
				t.Fatalf("result = %+v, want cancelled=%v for process evidence", result, test.wantCancelled)
			}
		})
	}
}

func TestClaudeCandidateTruncationWithoutCancellationIsNotCancelled(t *testing.T) {
	process := newCandidateFakeProcess()
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	process.finishResult(execution.Result{PID: 42, ExitCode: 0, OutputTruncated: true, FinishedAt: time.Now().UTC()})
	result, waitErr := staged.Wait(context.Background())
	if waitErr != nil {
		t.Fatalf("Wait() error = %v", waitErr)
	}
	if result.Kind == harness.ResultCancelled {
		t.Fatalf("result = %+v, truncation without cancellation must not be ResultCancelled", result)
	}
}

func TestClaudeCandidateCancelFailureRemainsStableWithoutRepeatedTerminate(t *testing.T) {
	process := newCandidateFakeProcess()
	firstErr := errors.New("first terminate failed")
	process.terminateError = firstErr
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSessionExpectClose(t, session, firstErr)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "cancel and retry", Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	firstReceipt, firstControlErr := staged.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-1", Kind: harness.ControlCancel})
	if !errors.Is(firstControlErr, firstErr) || firstReceipt.Outcome != harness.ControlFailed {
		t.Fatalf("first cancel = receipt=%+v error=%v, want failed termination", firstReceipt, firstControlErr)
	}
	if calls, _ := process.terminationDetails(); calls != 1 {
		t.Fatalf("first cancel Terminate calls = %d, want 1", calls)
	}

	secondReceipt, secondControlErr := staged.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-2", Kind: harness.ControlCancel})
	if !errors.Is(secondControlErr, firstErr) || secondReceipt.Outcome != harness.ControlFailed {
		t.Fatalf("repeated cancel = receipt=%+v error=%v, want stable failed observation", secondReceipt, secondControlErr)
	}
	if calls, _ := process.terminationDetails(); calls != 1 {
		t.Fatalf("repeated cancel Terminate calls = %d, want no re-termination", calls)
	}
	result, waitErr := staged.Wait(context.Background())
	if waitErr != nil || result.Kind == harness.ResultCancelled {
		t.Fatalf("failed cancellation result = %+v error=%v, must not claim clean cancellation", result, waitErr)
	}
}

func TestClaudeCandidateCancelledControlContextDoesNotAcknowledgeFailure(t *testing.T) {
	process := newCandidateFakeProcess()
	firstErr := errors.New("cancel cleanup failed")
	process.terminateError = firstErr
	adapter := newCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupCandidateSessionExpectClose(t, session, firstErr)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), harness.TurnRequest{Goal: "cancel with deadline", Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	controlContext, cancel := context.WithCancel(context.Background())
	cancel()
	firstReceipt, firstControlErr := staged.Control(controlContext, harness.ControlRequest{CommandID: "cancel-cancelled", Kind: harness.ControlCancel})
	if firstControlErr == nil || firstReceipt.Outcome != harness.ControlFailed {
		t.Fatalf("cancelled control = receipt=%+v error=%v, want failed non-acknowledgement", firstReceipt, firstControlErr)
	}
	// The cancelled control still starts the session's single detached cleanup attempt;
	// Close joins it before the retry so the retry sees the settled observation.
	settleContext, cancelSettle := context.WithTimeout(context.Background(), testHangGuard)
	defer cancelSettle()
	if err := session.Close(settleContext); !errors.Is(err, firstErr) {
		t.Fatalf("settled close = %v, want the stable %v", err, firstErr)
	}
	secondReceipt, secondControlErr := staged.Control(context.Background(), harness.ControlRequest{CommandID: "cancel-retry", Kind: harness.ControlCancel})
	if !errors.Is(secondControlErr, firstErr) || secondReceipt.Outcome != harness.ControlFailed {
		t.Fatalf("retry after cancelled control = receipt=%+v error=%v, want stable failed observation", secondReceipt, secondControlErr)
	}
	if calls, _ := process.terminationDetails(); calls != 1 {
		t.Fatalf("Terminate calls after cancelled control = %d, want one actual attempt", calls)
	}
}

func TestClaudeCandidateUnsupportedOperationsAndResumeFailClosed(t *testing.T) {
	process := newCandidateFakeProcess()
	adapter := newCandidateTestAdapter(process, nil)
	// Probe through the real promotion gate with a recorded foreign blob so this
	// pins the projection rather than a hand-built capability value.
	adapter.probe = func(ctx context.Context) (harness.Capabilities, error) {
		return probeCapabilities(ctx, "claude", &fixtureRunner{responses: map[string][]byte{
			"--version": []byte(testedVersion + " (Claude Code)\n"),
			"--help":    []byte("--print --output-format stream-json --json-schema"),
		}, digest: "sha256:" + strings.Repeat("0", 64)})
	}
	resume := &harness.ResumeHandle{LocalHandleID: "local", NativeSessionID: candidateTestSessionID}
	if session, err := adapter.Start(context.Background(), harness.StartRequest{Workspace: t.TempDir(), Resume: resume}, &recordingCandidateSink{}); session != nil || !errors.Is(err, harness.ErrUnsupportedCapability) {
		t.Fatalf("resume Start() = session=%T error=%v, want no session and unsupported capability", session, err)
	}
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("fresh Start() error = %v", err)
	}
	cleanupCandidateSession(t, session)
	staged := session.(harness.StagedSession)
	openCandidateSession(t, staged, process)
	for _, kind := range []harness.ControlKind{harness.ControlGuidance, harness.ControlPause, harness.ControlResume, harness.ControlApprovalResponse} {
		receipt, controlErr := staged.Control(context.Background(), harness.ControlRequest{Kind: kind})
		if controlErr != nil || receipt.Outcome != harness.ControlUnsupported {
			t.Fatalf("Control(%q) = receipt=%+v error=%v, want explicit unsupported", kind, receipt, controlErr)
		}
	}
	capabilities, probeErr := adapter.Probe(context.Background())
	if !errors.Is(probeErr, harness.ErrNativeUnverified) || capabilities.Verified || capabilities.Start || capabilities.Events || capabilities.Cancel || capabilities.Resume {
		t.Fatalf("Probe() = capabilities=%+v error=%v, want unverified no-operation projection", capabilities, probeErr)
	}
	if err := staged.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

// The daemon validates admission.model_profile against the configured runtime
// agent profile before Start, and this adapter forwards no model selection. An
// admitted profile therefore starts instead of being rejected for a native model
// mapping the adapter never claims.
func TestClaudeCandidateAcceptsAdmittedModelProfile(t *testing.T) {
	process := newCandidateFakeProcess()
	started := false
	adapter := newCandidateTestAdapter(process, nil)
	adapter.startProcess = func(context.Context, execution.Invocation, execution.Sink) (candidateProcess, error) {
		started = true
		return process, nil
	}
	request := candidateStartRequest(t)
	request.ModelProfile = "default"

	session, err := adapter.Start(context.Background(), request, &recordingCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v, want admitted model profile accepted", err)
	}
	if !started {
		t.Fatal("Start() did not launch Claude for an admitted model profile")
	}
	cleanupCandidateSession(t, session)
}

func TestClaudeCandidateRetainsProcessOwnerWhenStartReportsError(t *testing.T) {
	process := newCandidateFakeProcess()
	want := errors.New("persist process failed")
	adapter := newCandidateTestAdapter(process, nil)
	adapter.startProcess = func(ctx context.Context, invocation execution.Invocation, sink execution.Sink) (candidateProcess, error) {
		process.sink = sink
		if invocation.PersistProcess != nil {
			if err := invocation.PersistProcess(42, "test:42"); err != nil {
				return process, err
			}
		}
		return process, want
	}
	session, err := adapter.Start(context.Background(), candidateStartRequest(t), &recordingCandidateSink{})
	if session == nil || !errors.Is(err, want) {
		t.Fatalf("Start() = session=%T error=%v, want retained owner and start error", session, err)
	}
	cleanupCandidateSession(t, session)
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() retained process error = %v", err)
	}
	if calls, _ := process.terminationDetails(); calls != 1 {
		t.Fatalf("retained process Terminate calls = %d, want 1", calls)
	}
}
