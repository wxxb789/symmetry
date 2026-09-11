package harness

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

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
	if got, want := strings.Join(invocation.Args, " "), "--print --verbose --input-format stream-json --output-format stream-json --session-id "+claudeCandidateTestSessionID; got != want {
		t.Fatalf("fresh argv = %q, want %q", got, want)
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
}

func TestClaudeCandidateOpenRequiresMatchingSystemInitIdentity(t *testing.T) {
	t.Run("non-init does not open", func(t *testing.T) {
		process := newClaudeCandidateFakeProcess()
		adapter := newClaudeCandidateTestAdapter(process, nil)
		session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
		cleanupClaudeCandidateSession(t, session)
		staged := session.(StagedSession)
		openContext, cancel := context.WithCancel(context.Background())
		defer cancel()
		result := make(chan error, 1)
		go func() {
			_, openErr := staged.Open(openContext)
			result <- openErr
		}()
		if err := process.emitJSON(`{"type":"system","subtype":"ready","session_id":"` + claudeCandidateTestSessionID + `"}`); err != nil {
			t.Fatalf("emit non-init: %v", err)
		}
		cancel()
		select {
		case err := <-result:
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("Open() error = %v, want context.Canceled after non-init event", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Open() did not return after cancellation")
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
		result := make(chan error, 1)
		go func() {
			_, openErr := staged.Open(context.Background())
			result <- openErr
		}()
		_ = process.emitJSON(`{"type":"system","subtype":"init","session_id":"22222222-2222-4222-8222-222222222222"}`)
		select {
		case err := <-result:
			if !errors.Is(err, claudeprotocol.ErrConflictingIdentity) {
				t.Fatalf("Open() error = %v, want identity conflict", err)
			}
		case <-time.After(time.Second):
			t.Fatal("Open() did not fail after mismatched init identity")
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

func TestClaudeCandidateOpenCancellationStopsOwnedProcess(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openContext, cancel := context.WithCancel(context.Background())
	openResult := make(chan error, 1)
	go func() {
		_, openErr := staged.Open(openContext)
		openResult <- openErr
	}()
	cancel()
	select {
	case openErr := <-openResult:
		if !errors.Is(openErr, context.Canceled) {
			t.Fatalf("Open() error = %v, want context.Canceled", openErr)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled Open() did not return")
	}
	if calls, _ := process.terminationDetails(); calls != 1 {
		t.Fatalf("Open() cancellation Terminate calls = %d, want one owned cleanup", calls)
	}
	select {
	case <-process.waitDone:
	default:
		t.Fatal("Open() cancellation returned while the process owner was still live")
	}
}

func TestClaudeCandidateOpenDeadlineStopsOwnedProcess(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	deadlineContext := newClaudeCandidateDoneContext(context.DeadlineExceeded)
	staged := session.(StagedSession)
	openErr := make(chan error, 1)
	go func() {
		_, err := staged.Open(deadlineContext)
		openErr <- err
	}()
	select {
	case err := <-openErr:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Open() error = %v, want context.DeadlineExceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("deadline Open() did not return")
	}
	if calls, _ := process.terminationDetails(); calls != 1 {
		t.Fatalf("Open() deadline Terminate calls = %d, want one owned cleanup", calls)
	}
	select {
	case <-process.waitDone:
	default:
		t.Fatal("Open() deadline returned while the process owner was still live")
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

func TestClaudeCandidateCancelledResultRequiresCleanProcessTermination(t *testing.T) {
	tests := []struct {
		name          string
		mutate        func(*execution.Result)
		wantCancelled bool
	}{
		{name: "wait error", mutate: func(result *execution.Result) { result.WaitError = errors.New("wait failed") }},
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
	result := make(chan struct {
		handle NativeSessionHandle
		err    error
	}, 1)
	go func() {
		handle, err := staged.Open(context.Background())
		result <- struct {
			handle NativeSessionHandle
			err    error
		}{handle: handle, err: err}
	}()
	if err := process.emitJSON(`{"type":"system","subtype":"init","session_id":"` + claudeCandidateTestSessionID + `"}`); err != nil {
		t.Fatalf("emit matching init: %v", err)
	}
	select {
	case opened := <-result:
		if opened.err != nil {
			t.Fatalf("Open() error = %v", opened.err)
		}
		return opened.handle
	case <-time.After(time.Second):
		t.Fatal("Open() did not complete after matching system/init")
		return NativeSessionHandle{}
	}
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
	result          execution.Result
	waitDone        chan struct{}
	finishOnce      sync.Once
}

type claudeCandidateDoneContext struct {
	done chan struct{}
	err  error
}

func newClaudeCandidateDoneContext(err error) *claudeCandidateDoneContext {
	done := make(chan struct{})
	close(done)
	return &claudeCandidateDoneContext{done: done, err: err}
}

func (context *claudeCandidateDoneContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (context *claudeCandidateDoneContext) Done() <-chan struct{}       { return context.done }
func (context *claudeCandidateDoneContext) Err() error                  { return context.err }
func (context *claudeCandidateDoneContext) Value(any) any               { return nil }

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
	process.mu.Unlock()
	process.finish()
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
		"type":            "result",
		"subtype":         "success",
		"terminal_reason": "completed",
		"is_error":        false,
		"session_id":      claudeCandidateTestSessionID,
		"result":          semanticJSON,
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("encode result envelope: %v", err)
	}
	return string(encoded)
}
