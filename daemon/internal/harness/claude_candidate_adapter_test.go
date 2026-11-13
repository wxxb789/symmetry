package harness

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
	claudeprotocol "github.com/wxxb789/symmetry/daemon/internal/harness/claude"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const claudeCandidateTestSessionID = "11111111-1111-4111-8111-111111111111"

func TestClaudeCandidateStagesFreshTransportAndBarriers(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	var invocation execution.Invocation
	persisted := false
	adapter := newClaudeCandidateTestAdapter(process, &invocation)
	session, err := adapter.Start(context.Background(), StartRequest{
		Workspace: t.TempDir(),
		Invocation: execution.Invocation{
			Env: []string{"PATH=test"},
		},
		PersistProcess: func(pid int, identity string) error {
			persisted = pid == 42 && identity == "test:42"
			return nil
		},
	}, &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	if !persisted {
		t.Fatal("Start() did not persist the process owner before exposing the session")
	}
	if got, want := invocation.Program, "claude-test"; got != want {
		t.Fatalf("program = %q, want %q", got, want)
	}
	encodedSchema, err := claudeCandidateStructuredOutputSchema()
	if err != nil {
		t.Fatalf("structured output schema: %v", err)
	}
	wantArgs := []string{"--print", "--verbose", "--input-format", "stream-json", "--output-format", "stream-json", "--json-schema", string(encodedSchema), "--session-id", claudeCandidateTestSessionID, "--permission-mode", "acceptEdits"}
	if !slices.Equal(invocation.Args, wantArgs) {
		t.Fatalf("fresh argv = %q, want %q", invocation.Args, wantArgs)
	}
	if invocation.InitialInput != nil || invocation.CloseInputAfterInitial {
		t.Fatalf("Start() supplied legacy input: %+v", invocation)
	}
	if pid, identity := staged.ProcessDetails(); pid != 42 || identity != "test:42" {
		t.Fatalf("ProcessDetails() = (%d, %q), want (42, test:42)", pid, identity)
	}

	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "before open", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err == nil || !strings.Contains(err.Error(), "has not opened") {
		t.Fatalf("StartTurn() before Open error = %v, want open barrier", err)
	}
	if writes := process.writeCount(); writes != 0 {
		t.Fatalf("writes before Open = %d, want 0", writes)
	}

	opened := openClaudeCandidateSession(t, staged, process)
	if opened.ID != claudeCandidateTestSessionID {
		t.Fatalf("Open() handle = %+v, want requested fresh UUID", opened)
	}
	if writes := process.writeCount(); writes != 0 {
		t.Fatalf("writes during Open = %d, want 0", writes)
	}

	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "Make bounded progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
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
	process := newClaudeCandidateFakeProcess()
	var invocation execution.Invocation
	var called bool
	var gotPID int
	var gotIdentity string
	adapter := newClaudeCandidateTestAdapter(process, &invocation)
	request := claudeCandidateStartRequest(t)
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
	session, err := adapter.Start(context.Background(), request, &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
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
		process := newClaudeCandidateFakeProcess()
		adapter := newClaudeCandidateTestAdapter(process, nil)
		session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		cleanupClaudeCandidateSession(t, session)
		staged := session.(StagedSession)
		opened, err := staged.Open(context.Background())
		if err != nil || opened.ID != claudeCandidateTestSessionID {
			t.Fatalf("Open() = %+v, %v; want requested UUID without system/init", opened, err)
		}
		if writes := process.writeCount(); writes != 0 {
			t.Fatalf("writes during Open = %d, want 0", writes)
		}
		if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "deferred init", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
			t.Fatalf("StartTurn() error = %v", err)
		}
		if err := process.emitJSON(`{"type":"system","subtype":"init","session_id":"` + claudeCandidateTestSessionID + `"}`); err != nil {
			t.Fatalf("emit matching init: %v", err)
		}
		if err := process.emitJSON(claudeCandidateResultEnvelope(t, validClaudeCandidateTaskResultJSON(t))); err != nil {
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
		process := newClaudeCandidateFakeProcess()
		adapter := newClaudeCandidateTestAdapter(process, nil)
		session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		cleanupClaudeCandidateSession(t, session)
		staged := session.(StagedSession)
		if _, err := staged.Open(context.Background()); err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "missing init", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
			t.Fatalf("StartTurn() error = %v", err)
		}
		if err := process.emitJSON(claudeCandidateResultEnvelope(t, validClaudeCandidateTaskResultJSON(t))); !errors.Is(err, errClaudeCandidateMissingInit) {
			t.Fatalf("result-before-init error = %v, want ErrMissingInit", err)
		}
		if err := staged.WaitTurn(context.Background()); !errors.Is(err, errClaudeCandidateMissingInit) {
			t.Fatalf("WaitTurn() error = %v, want ErrMissingInit", err)
		}
	})

	t.Run("wrong init identity fails closed", func(t *testing.T) {
		process := newClaudeCandidateFakeProcess()
		adapter := newClaudeCandidateTestAdapter(process, nil)
		session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		cleanupClaudeCandidateSession(t, session)
		staged := session.(StagedSession)
		if _, err := staged.Open(context.Background()); err != nil {
			t.Fatalf("Open() error = %v", err)
		}
		if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "wrong init", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
			t.Fatalf("StartTurn() error = %v", err)
		}
		if err := process.emitJSON(`{"type":"system","subtype":"init","session_id":"22222222-2222-4222-8222-222222222222"}`); !errors.Is(err, claudeprotocol.ErrConflictingIdentity) {
			t.Fatalf("wrong-init error = %v, want ErrConflictingIdentity", err)
		}
	})
}

func TestClaudeCandidateRejectsProseTerminalAsSuccess(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "Make bounded progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	_ = process.emitJSON(`{"type":"result","subtype":"success","terminal_reason":"completed","is_error":false,"session_id":"` + claudeCandidateTestSessionID + `","result":"done"}`)
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
	if result.Kind == ResultSucceeded || result.Semantic != nil {
		t.Fatalf("prose terminal result = %+v, must not be successful semantic progress", result)
	}
	if result.Reason == nil || *result.Reason != protocol.TaskResultReasonMissingResult {
		t.Fatalf("prose terminal reason = %v, want missing_result", result.Reason)
	}
}

func TestClaudeCandidateAcceptsVerifiedTaskResultAndPublishesEvent(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	sink := &recordingClaudeCandidateSink{}
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "Make verified progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	semanticJSON := validClaudeCandidateTaskResultJSON(t)
	if err := process.emitJSON(claudeCandidateResultEnvelope(t, semanticJSON)); err != nil {
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
	if result.Kind != ResultSucceeded || result.Semantic == nil || result.Semantic.Summary != "made bounded progress" {
		t.Fatalf("result = %+v, want successful semantic TaskResult", result)
	}
	event, ok := sink.event(EventTaskResult)
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
	process := newClaudeCandidateFakeProcess()
	// Keep the watcher blocked in Wait so the in-flight terminal, not the
	// process reaper, decides the race under test.
	process.holdTermination = true
	sink := newGatedNativeResultSink()
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "Make verified progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	emitted := make(chan error, 1)
	go func() {
		emitted <- process.emitJSON(claudeCandidateResultEnvelope(t, validClaudeCandidateTaskResultJSON(t)))
	}()
	select {
	case <-sink.resultEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("native result frame never reached the sink")
	}
	// Claim the cancellation fence while the terminal is still inside the sink.
	// Close waits for the watcher, which stays blocked because this fake holds
	// termination until the test releases it.
	receipts := make(chan ControlReceipt, 1)
	receiptErrors := make(chan error, 1)
	go func() {
		receipt, controlErr := session.Control(context.Background(), ControlRequest{CommandID: "cancel-1", Kind: ControlCancel})
		receipts <- receipt
		receiptErrors <- controlErr
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if calls, _ := process.terminationDetails(); calls > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancellation never reached bounded process termination")
		}
		time.Sleep(time.Millisecond)
	}
	close(sink.releaseResult)
	if err := <-emitted; err != nil {
		t.Fatalf("emit result: %v", err)
	}
	process.finish()
	receipt := <-receipts
	if err := <-receiptErrors; err != nil || receipt.Outcome != ControlApplied {
		t.Fatalf("Control(cancel) = receipt=%+v error=%v, want applied", receipt, err)
	}
	if err := staged.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v, want the cancelled turn", err)
	}
	result, err := session.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != ResultCancelled || result.Semantic != nil {
		t.Fatalf("result = %+v, want a cancelled result without semantic success", result)
	}
	if _, ok := sink.event(EventTaskResult); ok {
		t.Fatal("an in-flight terminal after the cancel fence published a TaskResult event")
	}
}

// gatedNativeResultSink blocks the first native result frame so a test can order
// cancellation against terminal observation.
type gatedNativeResultSink struct {
	*recordingClaudeCandidateSink
	resultEntered chan struct{}
	releaseResult chan struct{}
	once          sync.Once
}

func newGatedNativeResultSink() *gatedNativeResultSink {
	return &gatedNativeResultSink{
		recordingClaudeCandidateSink: &recordingClaudeCandidateSink{},
		resultEntered:                make(chan struct{}),
		releaseResult:                make(chan struct{}),
	}
}

func (sink *gatedNativeResultSink) Handle(ctx context.Context, event Event) error {
	if event.Kind == EventNativeFrame {
		var record struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(event.Payload, &record) == nil && record.Type == "result" {
			sink.once.Do(func() { close(sink.resultEntered) })
			<-sink.releaseResult
		}
	}
	return sink.recordingClaudeCandidateSink.Handle(ctx, event)
}

// A direct Close (the daemon closes sessions outside Control) must not turn a
// turn whose terminal already won into a cancellation: cancellation ownership
// would otherwise let a later Control(cancel) answer "applied" while the turn
// settles successfully.
func TestClaudeCandidateCloseAfterTerminalObservationKeepsCancelRejected(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "Make verified progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	// Record exactly the state a terminal observation leaves behind before its
	// semantic completion runs: the terminal is observed, the turn is not final.
	observedTerminal := claudeprotocol.Terminal{SessionID: claudeCandidateTestSessionID}
	internal := staged.(*claudeCandidateSession)
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
	receipt, err := staged.Control(context.Background(), ControlRequest{CommandID: "cancel-1", Kind: ControlCancel})
	if err != nil || receipt.Outcome != ControlRejected {
		t.Fatalf("Control(cancel) = receipt=%+v error=%v, want a rejected late cancel", receipt, err)
	}
}

// A terminal that was observed just before a direct close must keep its own
// outcome: the pending semantic completion settles the turn as successful even
// though the session was explicitly terminated, and the watcher must not
// reclassify that success as a process failure.
func TestClaudeCandidateDirectClosePreservesPendingTerminalOutcome(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	process.holdTermination = true
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "Make verified progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	internal := staged.(*claudeCandidateSession)
	terminal := claudeprotocol.Terminal{
		SessionID: claudeCandidateTestSessionID,
		Result: claudeprotocol.Result{
			Subtype:          "success",
			TerminalReason:   "completed",
			StructuredOutput: json.RawMessage(validClaudeCandidateTaskResultJSON(t)),
		},
	}
	internal.mutex.Lock()
	internal.terminal = &terminal
	internal.mutex.Unlock()
	closeErrors := make(chan error, 1)
	go func() { closeErrors <- staged.Close(context.Background()) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if calls, _ := process.terminationDetails(); calls > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("direct close never reached bounded process termination")
		}
		time.Sleep(time.Millisecond)
	}
	// The pending terminal completes while the close is still settling.
	internal.completeClaudeCandidateTerminal(terminal)
	process.finish()
	if err := <-closeErrors; err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != ResultSucceeded || result.Semantic == nil {
		t.Fatalf("result = %+v, want the pending terminal's success to survive a direct close", result)
	}
}

// Cancelling before the deferred system/init arrives is still a cancellation:
// the accepted stop explains the absent identity and the absent terminal.
func TestClaudeCandidateCancelBeforeDeferredInitSettlesCancelled(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	if _, err := staged.Open(context.Background()); err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "Make verified progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	receipt, err := staged.Control(context.Background(), ControlRequest{CommandID: "cancel-1", Kind: ControlCancel})
	if err != nil || receipt.Outcome != ControlApplied {
		t.Fatalf("Control(cancel) = receipt=%+v error=%v, want applied", receipt, err)
	}
	if err := staged.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v, want the cancelled turn", err)
	}
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != ResultCancelled {
		t.Fatalf("result kind = %s summary %q, want cancelled without a missing-init failure", result.Kind, result.Summary)
	}
}

// A terminal that fails interpretation after cancellation claimed the turn must
// not reclassify the run as failed: the accepted stop owns the turn.
func TestClaudeCandidateCancelFenceSupersedesInvalidTerminal(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	process.holdTermination = true
	sink := newGatedNativeResultSink()
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "Make verified progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	emitted := make(chan error, 1)
	go func() {
		// subtype success without structured_output: the validator rejects it.
		emitted <- process.emitJSON(`{"type":"result","subtype":"success","terminal_reason":"completed","is_error":false,"session_id":"` + claudeCandidateTestSessionID + `","result":"prose"}`)
	}()
	select {
	case <-sink.resultEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("invalid native result frame never reached the sink")
	}
	receipts := make(chan ControlReceipt, 1)
	receiptErrors := make(chan error, 1)
	go func() {
		receipt, controlErr := staged.Control(context.Background(), ControlRequest{CommandID: "cancel-1", Kind: ControlCancel})
		receipts <- receipt
		receiptErrors <- controlErr
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if calls, _ := process.terminationDetails(); calls > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancellation never reached bounded process termination")
		}
		time.Sleep(time.Millisecond)
	}
	close(sink.releaseResult)
	if err := <-emitted; err != nil {
		t.Fatalf("emit result: %v", err)
	}
	process.finish()
	receipt := <-receipts
	if err := <-receiptErrors; err != nil || receipt.Outcome != ControlApplied {
		t.Fatalf("Control(cancel) = receipt=%+v error=%v, want applied", receipt, err)
	}
	if err := staged.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v, want the cancelled turn", err)
	}
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != ResultCancelled {
		t.Fatalf("result kind = %s summary %q, want cancelled rather than a terminal-interpretation failure", result.Kind, result.Summary)
	}
}

// A superseded result record must not reach the stateful validator: otherwise its
// retained interpretation error is reported by the next record of the same chunk
// and fails a turn the accepted cancellation already owns.
func TestClaudeCandidateSupersededResultDoesNotPoisonLaterRecords(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	process.holdTermination = true
	sink := newGatedNativeResultSink()
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "Make verified progress.", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	internal := staged.(*claudeCandidateSession)
	chunk := `{"type":"result","subtype":"success","terminal_reason":"completed","is_error":false,"session_id":"` + claudeCandidateTestSessionID + `","result":"prose"}` + "\n" +
		`{"type":"log","message":"after the superseded terminal"}` + "\n"
	delivered := make(chan error, 1)
	go func() {
		delivered <- internal.handleProcessOutput(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: 7, At: time.Now().UTC(), Data: []byte(chunk)})
	}()
	select {
	case <-sink.resultEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("invalid native result frame never reached the sink")
	}
	receipts := make(chan ControlReceipt, 1)
	receiptErrors := make(chan error, 1)
	go func() {
		receipt, controlErr := staged.Control(context.Background(), ControlRequest{CommandID: "cancel-1", Kind: ControlCancel})
		receipts <- receipt
		receiptErrors <- controlErr
	}()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if calls, _ := process.terminationDetails(); calls > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancellation never reached bounded process termination")
		}
		time.Sleep(time.Millisecond)
	}
	close(sink.releaseResult)
	if err := <-delivered; err != nil {
		t.Fatalf("output handling error = %v, want the superseded terminal and the following record to stay non-failing", err)
	}
	process.finish()
	receipt := <-receipts
	if err := <-receiptErrors; err != nil || receipt.Outcome != ControlApplied {
		t.Fatalf("Control(cancel) = receipt=%+v error=%v, want applied", receipt, err)
	}
	if err := staged.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v, want the cancelled turn", err)
	}
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != ResultCancelled {
		t.Fatalf("result kind = %s summary %q, want cancelled rather than a retained validator failure", result.Kind, result.Summary)
	}
}

// One session owns at most one native turn, and a closed session accepts no
// further turn. Both negative controls fail closed instead of reusing a native
// process whose identity or lifecycle is already settled.
func TestClaudeCandidateRejectsSupersededAndClosedTurns(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "first turn", Context: json.RawMessage(`{"turn":1}`)}); err != nil {
		t.Fatalf("first StartTurn() error = %v", err)
	}
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "superseding turn", Context: json.RawMessage(`{"turn":2}`)}); err == nil || !strings.Contains(err.Error(), "already owns a turn") {
		t.Fatalf("superseding StartTurn() error = %v, want an owned-turn rejection", err)
	}
	if err := staged.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	// The closed session rejects the turn through its lifecycle fence. The exact
	// message depends on whether the process has already been reaped, so the
	// pinned contract is that the rejection surfaces and no second frame reaches
	// the native process.
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "turn after close", Context: json.RawMessage(`{"turn":3}`)}); err == nil {
		t.Fatal("closed StartTurn() error = nil, want a lifecycle rejection")
	}
	writes := process.writeCount()
	if writes != 1 {
		t.Fatalf("native prompt writes = %d, want exactly the first turn", writes)
	}
}

func TestClaudeCandidateLateCancelAfterTerminalObservationIsRejected(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	sink := newBlockingClaudeCandidateSink()
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "observe terminal before publication", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	emitDone := make(chan error, 1)
	go func() {
		emitDone <- process.emitJSON(claudeCandidateResultEnvelope(t, validClaudeCandidateTaskResultJSON(t)))
	}()
	select {
	case <-sink.taskResultEntered:
	case <-time.After(time.Second):
		t.Fatal("terminal result was not held in EventTaskResult delivery")
	}

	receipt, controlErr := staged.Control(context.Background(), ControlRequest{CommandID: "cancel-late", Kind: ControlCancel})
	if controlErr != nil {
		t.Fatalf("late Control(cancel) error = %v, want rejected receipt", controlErr)
	}
	if receipt.Outcome != ControlRejected || receipt.Message != "Claude terminal was already observed; cancellation rejected" {
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
	process := newClaudeCandidateFakeProcess()
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "observe terminal failure", Context: json.RawMessage(`{"snapshot":"canonical"}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	invalid := `{"type":"result","subtype":"success","terminal_reason":"completed","is_error":true,"session_id":"` + claudeCandidateTestSessionID + `","result":"failed"}`
	if err := process.emitJSON(invalid); !errors.Is(err, claudeprotocol.ErrResultReportedError) {
		t.Fatalf("invalid terminal error = %v, want ErrResultReportedError", err)
	}

	receipt, controlErr := staged.Control(context.Background(), ControlRequest{CommandID: "cancel-after-failure", Kind: ControlCancel})
	if controlErr != nil {
		t.Fatalf("late Control(cancel) after terminal failure error = %v", controlErr)
	}
	if receipt.Outcome != ControlRejected || receipt.Message != "Claude terminal observation failed; cancellation rejected" {
		t.Fatalf("late cancel after terminal failure receipt = %+v, want clear rejection", receipt)
	}
	if calls, _ := process.terminationDetails(); calls != 0 {
		t.Fatalf("late cancel after terminal failure Terminate calls = %d, want zero", calls)
	}
}

func TestClaudeCandidateDoesNotReplayPreReadyOutputAfterStartError(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	startErr := errors.New("post-start launch error")
	sink := &recordingClaudeCandidateSink{}
	adapter := newClaudeCandidateTestAdapter(process, nil)
	adapter.startProcess = func(_ context.Context, _ execution.Invocation, output execution.Sink) (claudeCandidateProcess, error) {
		process.sink = output
		if err := output.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: 1, At: time.Now().UTC(), Data: []byte(`{"type":"system","subtype":"init","session_id":"` + claudeCandidateTestSessionID + `"}` + "\n")}); err != nil {
			t.Fatalf("queue pre-ready output: %v", err)
		}
		return process, startErr
	}
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), sink)
	if session == nil || !errors.Is(err, startErr) {
		t.Fatalf("Start() = session=%T error=%v, want retained owner and original start error", session, err)
	}
	cleanupClaudeCandidateSession(t, session)
	if events := sink.eventsSnapshot(); len(events) != 0 {
		t.Fatalf("pre-ready events after start error = %#v, want no replay", events)
	}
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() retained owner error = %v", err)
	}
}

func TestClaudeCandidateStartTurnCloseRaceCancelsPromptWrite(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	process.blockWrites = true
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	turnResult := make(chan error, 1)
	go func() {
		turnResult <- staged.StartTurn(context.Background(), TurnRequest{Goal: "race", Context: json.RawMessage(`{}`)})
	}()
	select {
	case <-process.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("StartTurn() did not reach the fake write")
	}
	closeResult := make(chan error, 1)
	go func() { closeResult <- staged.Close(context.Background()) }()
	select {
	case err := <-turnResult:
		if err == nil {
			t.Fatal("StartTurn() succeeded after Close() fenced the prompt write")
		}
	case <-time.After(time.Second):
		t.Fatal("StartTurn() remained blocked after Close() cancellation")
	}
	select {
	case err := <-closeResult:
		if err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close() did not complete after cancelling prompt write")
	}
	if writes := process.writeCount(); writes != 0 {
		t.Fatalf("successful prompt writes after close fence = %d, want 0", writes)
	}
}

func TestClaudeCandidateCloseFailureIsStableWithoutReterminating(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	firstErr := errors.New("transient terminate failure")
	process.terminateError = firstErr
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSessionExpectClose(t, session, firstErr)
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
	if result.Kind == ResultCancelled {
		t.Fatalf("final result = %+v, must not claim clean cancellation after termination failure", result)
	}
	if calls, _ := process.terminationDetails(); calls != 1 {
		t.Fatalf("Terminate calls = %d, want one actual attempt", calls)
	}
}

func TestClaudeCandidateWatcherTimeoutCanBeReconciled(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	process.holdTermination = true
	ctx, cancel := context.WithCancel(context.Background())
	session := newClaudeCandidateSession(ctx, cancel, &recordingClaudeCandidateSink{}, claudeCandidateTestSessionID)
	session.mutex.Lock()
	session.process = process
	session.processReady = true
	session.mutex.Unlock()
	go session.watchProcess()
	defer process.finish()

	watchContext, watchCancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	err, retryable := session.closeOnce(watchContext)
	watchCancel()
	if !errors.Is(err, errClaudeCandidateWatcherTimeout) || !retryable {
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
	original := TaskResult{Semantic: &semantic, Reason: &reason}
	cloned := cloneClaudeCandidateTaskResult(original)
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
	process := newClaudeCandidateFakeProcess()
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "cancel me", Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	receipt, err := staged.Control(context.Background(), ControlRequest{CommandID: "cancel-1", Kind: ControlCancel})
	if err != nil {
		t.Fatalf("Control(cancel) error = %v", err)
	}
	if receipt.Outcome != ControlApplied || receipt.Capability != CapabilityCancel {
		t.Fatalf("cancel receipt = %+v, want applied cancel", receipt)
	}
	if calls, grace := process.terminationDetails(); calls != 1 || grace != claudeCandidateTerminationGrace {
		t.Fatalf("Terminate() = calls=%d grace=%s, want one bounded %s call", calls, grace, claudeCandidateTerminationGrace)
	}
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != ResultCancelled {
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
			process := newClaudeCandidateFakeProcess()
			process.terminateResult = execution.Result{PID: 42, ExitCode: -1, Terminated: true, FinishedAt: time.Now().UTC()}
			test.mutate(&process.terminateResult)
			adapter := newClaudeCandidateTestAdapter(process, nil)
			session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			cleanupClaudeCandidateSession(t, session)
			staged := session.(StagedSession)
			openClaudeCandidateSession(t, staged, process)
			if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "cancel with process evidence", Context: json.RawMessage(`{}`)}); err != nil {
				t.Fatalf("StartTurn() error = %v", err)
			}
			receipt, controlErr := staged.Control(context.Background(), ControlRequest{Kind: ControlCancel})
			if controlErr != nil || receipt.Outcome != ControlApplied {
				t.Fatalf("Control(cancel) = receipt=%+v error=%v, want applied termination request", receipt, controlErr)
			}
			result, waitErr := staged.Wait(context.Background())
			if waitErr != nil {
				t.Fatalf("Wait() error = %v", waitErr)
			}
			if (result.Kind == ResultCancelled) != test.wantCancelled {
				t.Fatalf("result = %+v, want cancelled=%v for process evidence", result, test.wantCancelled)
			}
		})
	}
}

func TestClaudeCandidateTruncationWithoutCancellationIsNotCancelled(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	process.finishResult(execution.Result{PID: 42, ExitCode: 0, OutputTruncated: true, FinishedAt: time.Now().UTC()})
	result, waitErr := staged.Wait(context.Background())
	if waitErr != nil {
		t.Fatalf("Wait() error = %v", waitErr)
	}
	if result.Kind == ResultCancelled {
		t.Fatalf("result = %+v, truncation without cancellation must not be ResultCancelled", result)
	}
}

func TestClaudeCandidateCancelFailureRemainsStableWithoutRepeatedTerminate(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	firstErr := errors.New("first terminate failed")
	process.terminateError = firstErr
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSessionExpectClose(t, session, firstErr)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "cancel and retry", Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	firstReceipt, firstControlErr := staged.Control(context.Background(), ControlRequest{CommandID: "cancel-1", Kind: ControlCancel})
	if !errors.Is(firstControlErr, firstErr) || firstReceipt.Outcome != ControlFailed {
		t.Fatalf("first cancel = receipt=%+v error=%v, want failed termination", firstReceipt, firstControlErr)
	}
	if calls, _ := process.terminationDetails(); calls != 1 {
		t.Fatalf("first cancel Terminate calls = %d, want 1", calls)
	}

	secondReceipt, secondControlErr := staged.Control(context.Background(), ControlRequest{CommandID: "cancel-2", Kind: ControlCancel})
	if !errors.Is(secondControlErr, firstErr) || secondReceipt.Outcome != ControlFailed {
		t.Fatalf("repeated cancel = receipt=%+v error=%v, want stable failed observation", secondReceipt, secondControlErr)
	}
	if calls, _ := process.terminationDetails(); calls != 1 {
		t.Fatalf("repeated cancel Terminate calls = %d, want no re-termination", calls)
	}
	result, waitErr := staged.Wait(context.Background())
	if waitErr != nil || result.Kind == ResultCancelled {
		t.Fatalf("failed cancellation result = %+v error=%v, must not claim clean cancellation", result, waitErr)
	}
}

func TestClaudeCandidateCancelledControlContextDoesNotAcknowledgeFailure(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	firstErr := errors.New("cancel cleanup failed")
	process.terminateError = firstErr
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSessionExpectClose(t, session, firstErr)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "cancel with deadline", Context: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	controlContext, cancel := context.WithCancel(context.Background())
	cancel()
	firstReceipt, firstControlErr := staged.Control(controlContext, ControlRequest{CommandID: "cancel-cancelled", Kind: ControlCancel})
	if firstControlErr == nil || firstReceipt.Outcome != ControlFailed {
		t.Fatalf("cancelled control = receipt=%+v error=%v, want failed non-acknowledgement", firstReceipt, firstControlErr)
	}
	if native, ok := session.(*claudeCandidateSession); ok {
		native.closeMutex.Lock()
		attempt := native.closeAttempt
		native.closeMutex.Unlock()
		if attempt != nil {
			select {
			case <-attempt.done:
			case <-time.After(time.Second):
				t.Fatal("cancelled control close attempt did not settle")
			}
		}
	}
	secondReceipt, secondControlErr := staged.Control(context.Background(), ControlRequest{CommandID: "cancel-retry", Kind: ControlCancel})
	if !errors.Is(secondControlErr, firstErr) || secondReceipt.Outcome != ControlFailed {
		t.Fatalf("retry after cancelled control = receipt=%+v error=%v, want stable failed observation", secondReceipt, secondControlErr)
	}
	if calls, _ := process.terminationDetails(); calls != 1 {
		t.Fatalf("Terminate calls after cancelled control = %d, want one actual attempt", calls)
	}
}

func TestClaudeCandidateUnsupportedOperationsAndResumeFailClosed(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	adapter := newClaudeCandidateTestAdapter(process, nil)
	// Probe through the real promotion gate with a recorded foreign blob so this
	// pins the projection rather than a hand-built capability value.
	adapter.probe = func(ctx context.Context) (Capabilities, error) {
		return probeClaudeAdapterCapabilities(ctx, "claude", &claudeFixtureRunner{responses: map[string][]byte{
			"--version": []byte(ClaudeTestedVersion + " (Claude Code)\n"),
			"--help":    []byte("--print --output-format stream-json --json-schema"),
		}, digest: "sha256:" + strings.Repeat("0", 64)})
	}
	resume := &ResumeHandle{LocalHandleID: "local", NativeSessionID: claudeCandidateTestSessionID}
	if session, err := adapter.Start(context.Background(), StartRequest{Workspace: t.TempDir(), Resume: resume}, &recordingClaudeCandidateSink{}); session != nil || !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("resume Start() = session=%T error=%v, want no session and unsupported capability", session, err)
	}
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("fresh Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	for _, kind := range []ControlKind{ControlGuidance, ControlPause, ControlResume, ControlApprovalResponse} {
		receipt, controlErr := staged.Control(context.Background(), ControlRequest{Kind: kind})
		if controlErr != nil || receipt.Outcome != ControlUnsupported {
			t.Fatalf("Control(%q) = receipt=%+v error=%v, want explicit unsupported", kind, receipt, controlErr)
		}
	}
	capabilities, probeErr := adapter.Probe(context.Background())
	if !errors.Is(probeErr, ErrNativeUnverified) || capabilities.Verified || capabilities.Start || capabilities.Events || capabilities.Cancel || capabilities.Resume {
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
	process := newClaudeCandidateFakeProcess()
	started := false
	adapter := newClaudeCandidateTestAdapter(process, nil)
	adapter.startProcess = func(context.Context, execution.Invocation, execution.Sink) (claudeCandidateProcess, error) {
		started = true
		return process, nil
	}
	request := claudeCandidateStartRequest(t)
	request.ModelProfile = "default"

	session, err := adapter.Start(context.Background(), request, &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v, want admitted model profile accepted", err)
	}
	if !started {
		t.Fatal("Start() did not launch Claude for an admitted model profile")
	}
	cleanupClaudeCandidateSession(t, session)
}

func TestClaudeCandidateRetainsProcessOwnerWhenStartReportsError(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	want := errors.New("persist process failed")
	adapter := newClaudeCandidateTestAdapter(process, nil)
	adapter.startProcess = func(ctx context.Context, invocation execution.Invocation, sink execution.Sink) (claudeCandidateProcess, error) {
		process.sink = sink
		if invocation.PersistProcess != nil {
			if err := invocation.PersistProcess(42, "test:42"); err != nil {
				return process, err
			}
		}
		return process, want
	}
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if session == nil || !errors.Is(err, want) {
		t.Fatalf("Start() = session=%T error=%v, want retained owner and start error", session, err)
	}
	cleanupClaudeCandidateSession(t, session)
	if err := session.Close(context.Background()); err != nil {
		t.Fatalf("Close() retained process error = %v", err)
	}
	if calls, _ := process.terminationDetails(); calls != 1 {
		t.Fatalf("retained process Terminate calls = %d, want 1", calls)
	}
}

func newClaudeCandidateTestAdapter(process *claudeCandidateFakeProcess, invocation *execution.Invocation) *ClaudeCandidateAdapter {
	return &ClaudeCandidateAdapter{
		executable:   "claude-test",
		newSessionID: func() (string, error) { return claudeCandidateTestSessionID, nil },
		startProcess: func(_ context.Context, got execution.Invocation, sink execution.Sink) (claudeCandidateProcess, error) {
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

func claudeCandidateStartRequest(t *testing.T) StartRequest {
	t.Helper()
	return StartRequest{Workspace: t.TempDir(), Invocation: execution.Invocation{Env: []string{"PATH=test"}}}
}

func openClaudeCandidateSession(t *testing.T, staged StagedSession, process *claudeCandidateFakeProcess) NativeSessionHandle {
	t.Helper()
	handle, err := staged.Open(context.Background())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	// Native Claude Code 2.1.322 emits init only after the first user frame.
	// These tests are not about that ordering, so establish identity here.
	if err := process.emitJSON(`{"type":"system","subtype":"init","session_id":"` + claudeCandidateTestSessionID + `"}`); err != nil {
		t.Fatalf("emit matching init: %v", err)
	}
	return handle
}

func cleanupClaudeCandidateSession(t *testing.T, session Session) {
	cleanupClaudeCandidateSessionExpectClose(t, session, nil)
}

func cleanupClaudeCandidateSessionExpectClose(t *testing.T, session Session, expectedCloseErr error) {
	t.Helper()
	t.Cleanup(func() {
		if err := session.Close(context.Background()); err != nil && (expectedCloseErr == nil || !errors.Is(err, expectedCloseErr)) {
			t.Errorf("candidate cleanup Close() error = %v", err)
		}
		waitContext, cancel := context.WithTimeout(context.Background(), time.Second)
		_, waitErr := session.Wait(waitContext)
		cancel()
		if waitErr != nil {
			t.Errorf("candidate cleanup Wait() error = %v", waitErr)
		}
		if native, ok := session.(*claudeCandidateSession); ok {
			select {
			case <-native.watchDone:
			case <-time.After(time.Second):
				t.Errorf("candidate cleanup watcher did not terminate")
			}
		}
	})
}

type recordingClaudeCandidateSink struct {
	mu     sync.Mutex
	events []Event
}

type blockingClaudeCandidateSink struct {
	*recordingClaudeCandidateSink
	taskResultEntered chan struct{}
	releaseTaskResult chan struct{}
	taskResultOnce    sync.Once
}

func newBlockingClaudeCandidateSink() *blockingClaudeCandidateSink {
	return &blockingClaudeCandidateSink{
		recordingClaudeCandidateSink: &recordingClaudeCandidateSink{},
		taskResultEntered:            make(chan struct{}),
		releaseTaskResult:            make(chan struct{}),
	}
}

func (sink *blockingClaudeCandidateSink) Handle(ctx context.Context, event Event) error {
	if event.Kind == EventTaskResult {
		sink.taskResultOnce.Do(func() { close(sink.taskResultEntered) })
		select {
		case <-sink.releaseTaskResult:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return sink.recordingClaudeCandidateSink.Handle(ctx, event)
}

func (sink *recordingClaudeCandidateSink) Handle(_ context.Context, event Event) error {
	sink.mu.Lock()
	sink.events = append(sink.events, event)
	sink.mu.Unlock()
	return nil
}

func (sink *recordingClaudeCandidateSink) eventsSnapshot() []Event {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	events := make([]Event, len(sink.events))
	copy(events, sink.events)
	return events
}

func (sink *recordingClaudeCandidateSink) event(kind EventKind) (Event, bool) {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	for _, event := range sink.events {
		if event.Kind == kind {
			return event, true
		}
	}
	return Event{}, false
}

type claudeCandidateFakeProcess struct {
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

func newClaudeCandidateFakeProcess() *claudeCandidateFakeProcess {
	return &claudeCandidateFakeProcess{waitDone: make(chan struct{}), writeStarted: make(chan struct{})}
}

func (process *claudeCandidateFakeProcess) WriteInputContext(ctx context.Context, input []byte) error {
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

func (process *claudeCandidateFakeProcess) Wait() execution.Result {
	<-process.waitDone
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.result
}

func (process *claudeCandidateFakeProcess) Terminate(_ context.Context, grace time.Duration) error {
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

func (process *claudeCandidateFakeProcess) ProcessDetails() (int, string) {
	return 42, "test:42"
}

func (process *claudeCandidateFakeProcess) finish() {
	process.finishOnce.Do(func() { close(process.waitDone) })
}

func (process *claudeCandidateFakeProcess) finishResult(result execution.Result) {
	process.mu.Lock()
	process.result = result
	process.mu.Unlock()
	process.finish()
}

func (process *claudeCandidateFakeProcess) emitJSON(value string) error {
	process.mu.Lock()
	sink := process.sink
	process.mu.Unlock()
	if sink == nil {
		return errors.New("fake Claude process sink is nil")
	}
	return sink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, Sequence: 1, At: time.Now().UTC(), Data: []byte(value + "\n")})
}

func (process *claudeCandidateFakeProcess) writeCount() int {
	process.mu.Lock()
	defer process.mu.Unlock()
	return len(process.writes)
}

func (process *claudeCandidateFakeProcess) writesSnapshot() [][]byte {
	process.mu.Lock()
	defer process.mu.Unlock()
	result := make([][]byte, len(process.writes))
	for index := range process.writes {
		result[index] = append([]byte(nil), process.writes[index]...)
	}
	return result
}

func (process *claudeCandidateFakeProcess) terminationDetails() (int, time.Duration) {
	process.mu.Lock()
	defer process.mu.Unlock()
	return process.terminateCalls, process.terminateGrace
}

func validClaudeCandidateTaskResultJSON(t *testing.T) string {
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

func claudeCandidateResultEnvelope(t *testing.T, semanticJSON string) string {
	t.Helper()
	envelope := map[string]any{
		"type":              "result",
		"subtype":           "success",
		"terminal_reason":   "completed",
		"is_error":          false,
		"session_id":        claudeCandidateTestSessionID,
		"result":            semanticJSON,
		"structured_output": json.RawMessage(semanticJSON),
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("encode result envelope: %v", err)
	}
	return string(encoded)
}
