package harness

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	claudeprotocol "github.com/wxxb789/symmetry/daemon/internal/harness/claude"
)

func TestClaudeCandidateProcessExitAfterTerminalBindsFinalOutcome(t *testing.T) {
	tests := []struct {
		name       string
		process    execution.Result
		wantKind   ResultKind
		wantReason string
	}{
		{
			name:     "clean exit preserves verified result",
			process:  execution.Result{PID: 42, ExitCode: 0},
			wantKind: ResultSucceeded,
		},
		{
			name:       "failed exit rejects verified result",
			process:    execution.Result{PID: 42, ExitCode: 1, WaitError: errors.New("exit status 1")},
			wantKind:   ResultFailed,
			wantReason: "process_failure",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			process := newClaudeCandidateFakeProcess()
			adapter := newClaudeCandidateTestAdapter(process, nil)
			session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			cleanupClaudeCandidateSession(t, session)
			staged := session.(StagedSession)
			openClaudeCandidateSession(t, staged, process)
			if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "bind process evidence", Context: jsonObjectForClaudeCandidateAdditionalTest()}); err != nil {
				t.Fatalf("StartTurn() error = %v", err)
			}
			if err := process.emitJSON(claudeCandidateResultEnvelope(t, validClaudeCandidateTaskResultJSON(t))); err != nil {
				t.Fatalf("emit terminal result: %v", err)
			}
			if err := staged.WaitTurn(context.Background()); err != nil {
				t.Fatalf("WaitTurn() error = %v, want terminal result before process exit", err)
			}

			process.finishResult(test.process)
			result, err := staged.Wait(context.Background())
			if err != nil {
				t.Fatalf("Wait() error = %v", err)
			}
			if result.Kind != test.wantKind {
				t.Fatalf("result kind = %q, want %q; result=%+v", result.Kind, test.wantKind, result)
			}
			if test.wantKind == ResultSucceeded && result.Semantic == nil {
				t.Fatalf("result = %+v, want retained semantic TaskResult", result)
			}
			if test.wantKind == ResultFailed {
				if result.Semantic != nil {
					t.Fatalf("result = %+v, failed process must not retain semantic success", result)
				}
				if result.Reason == nil || string(*result.Reason) != test.wantReason {
					t.Fatalf("result reason = %v, want %q", result.Reason, test.wantReason)
				}
			}
		})
	}
}

func TestClaudeCandidateRejectsDuplicateAndLateRecordsAfterTerminal(t *testing.T) {
	tests := []struct {
		name      string
		record    string
		wantError error
	}{
		{
			name:      "duplicate result",
			record:    claudeCandidateResultEnvelope(t, validClaudeCandidateTaskResultJSON(t)),
			wantError: claudeprotocol.ErrRepeatedResult,
		},
		{
			name:      "late event",
			record:    `{"type":"system","subtype":"late","session_id":"` + claudeCandidateTestSessionID + `"}`,
			wantError: claudeprotocol.ErrEventAfterResult,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			process := newClaudeCandidateFakeProcess()
			adapter := newClaudeCandidateTestAdapter(process, nil)
			session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			cleanupClaudeCandidateSession(t, session)
			staged := session.(StagedSession)
			openClaudeCandidateSession(t, staged, process)
			if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "reject late native output", Context: jsonObjectForClaudeCandidateAdditionalTest()}); err != nil {
				t.Fatalf("StartTurn() error = %v", err)
			}
			first := claudeCandidateResultEnvelope(t, validClaudeCandidateTaskResultJSON(t))
			if err := process.emitJSON(first); err != nil {
				t.Fatalf("emit first terminal result: %v", err)
			}
			if err := staged.WaitTurn(context.Background()); err != nil {
				t.Fatalf("WaitTurn() after first result = %v, want nil", err)
			}

			if err := process.emitJSON(test.record); !errors.Is(err, test.wantError) {
				t.Fatalf("emit %s = %v, want %v", test.name, err, test.wantError)
			}
			if err := staged.WaitTurn(context.Background()); !errors.Is(err, test.wantError) {
				t.Fatalf("WaitTurn() after %s = %v, want %v", test.name, err, test.wantError)
			}

			process.finishResult(execution.Result{PID: 42, ExitCode: 0})
			result, err := staged.Wait(context.Background())
			if err != nil {
				t.Fatalf("Wait() error = %v", err)
			}
			if result.Kind != ResultFailed || result.Semantic != nil {
				t.Fatalf("result = %+v, want failed non-semantic result", result)
			}
		})
	}
}

func TestClaudeCandidateEventSinkFailureAfterTerminalIsNotSuccess(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	wantSinkError := errors.New("task result journal unavailable")
	sink := EventSinkFunc(func(_ context.Context, event Event) error {
		if event.Kind == EventTaskResult {
			return wantSinkError
		}
		return nil
	})
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "persist terminal evidence", Context: jsonObjectForClaudeCandidateAdditionalTest()}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if err := process.emitJSON(claudeCandidateResultEnvelope(t, validClaudeCandidateTaskResultJSON(t))); err != nil {
		t.Fatalf("emit terminal result = %v, want lifecycle failure retained for WaitTurn", err)
	}
	if err := staged.WaitTurn(context.Background()); !errors.Is(err, wantSinkError) {
		t.Fatalf("WaitTurn() error = %v, want sink error %v", err, wantSinkError)
	}

	process.finishResult(execution.Result{PID: 42, ExitCode: 0})
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != ResultFailed || result.Semantic != nil {
		t.Fatalf("result = %+v, sink failure must not claim semantic success", result)
	}
	if !strings.Contains(result.Summary, wantSinkError.Error()) {
		t.Fatalf("result summary = %q, want sink failure context", result.Summary)
	}
}

func TestClaudeCandidateMalformedRecordPreservesDiagnosticSinkFailure(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	wantSinkError := errors.New("diagnostic journal unavailable")
	sink := EventSinkFunc(func(_ context.Context, event Event) error {
		if event.Kind == EventDiagnostic {
			return wantSinkError
		}
		return nil
	})
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "preserve malformed record evidence", Context: jsonObjectForClaudeCandidateAdditionalTest()}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}

	if err := process.emitJSON("{malformed"); !errors.Is(err, wantSinkError) {
		t.Fatalf("emit malformed record = %v, want diagnostic sink failure %v", err, wantSinkError)
	}
	process.finishResult(execution.Result{PID: 42, ExitCode: 0})
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != ResultFailed || result.Semantic != nil || !strings.Contains(result.Summary, wantSinkError.Error()) {
		t.Fatalf("result = %+v, want failed result retaining diagnostic sink failure", result)
	}
}

func TestClaudeCandidateIncompleteTrailingRecordAfterTerminalIsNotSuccess(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	openClaudeCandidateSession(t, staged, process)
	if err := staged.StartTurn(context.Background(), TurnRequest{Goal: "reject truncated trailing output", Context: jsonObjectForClaudeCandidateAdditionalTest()}); err != nil {
		t.Fatalf("StartTurn() error = %v", err)
	}
	if err := process.emitJSON(claudeCandidateResultEnvelope(t, validClaudeCandidateTaskResultJSON(t))); err != nil {
		t.Fatalf("emit terminal result = %v", err)
	}
	if err := staged.WaitTurn(context.Background()); err != nil {
		t.Fatalf("WaitTurn() error = %v, want terminal result before process exit", err)
	}

	process.mu.Lock()
	sink := process.sink
	process.mu.Unlock()
	if sink == nil {
		t.Fatal("fake Claude process sink is nil")
	}
	if err := sink.Handle(context.Background(), execution.Event{
		Stream: execution.Stdout,
		At:     time.Now().UTC(),
		Data:   []byte(`{"type":"system","subtype":"late","session_id":"` + claudeCandidateTestSessionID + `"}`),
	}); err != nil {
		t.Fatalf("deliver incomplete trailing record = %v, want decoder to defer it until process close", err)
	}

	process.finishResult(execution.Result{PID: 42, ExitCode: 0})
	result, err := staged.Wait(context.Background())
	if err != nil {
		t.Fatalf("Wait() error = %v", err)
	}
	if result.Kind != ResultFailed || result.Semantic != nil {
		t.Fatalf("result = %+v, truncated trailing output must not claim semantic success", result)
	}
	if !strings.Contains(result.Summary, claudeprotocol.ErrIncompleteRecord.Error()) {
		t.Fatalf("result summary = %q, want %q", result.Summary, claudeprotocol.ErrIncompleteRecord.Error())
	}
}

func TestClaudeCandidateOpenAndCloseAreIdempotent(t *testing.T) {
	process := newClaudeCandidateFakeProcess()
	adapter := newClaudeCandidateTestAdapter(process, nil)
	session, err := adapter.Start(context.Background(), claudeCandidateStartRequest(t), &recordingClaudeCandidateSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	cleanupClaudeCandidateSession(t, session)
	staged := session.(StagedSession)
	first := openClaudeCandidateSession(t, staged, process)
	second, err := staged.Open(context.Background())
	if err != nil {
		t.Fatalf("second Open() error = %v", err)
	}
	if second != first {
		t.Fatalf("second Open() handle = %+v, want stable %+v", second, first)
	}

	if err := staged.Close(context.Background()); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := staged.Close(context.Background()); err != nil {
		t.Fatalf("second Close() error = %v, want nil", err)
	}
	if calls, _ := process.terminationDetails(); calls != 1 {
		t.Fatalf("Terminate() calls = %d, want one idempotent cleanup", calls)
	}
	if _, err := session.Wait(context.Background()); err != nil {
		t.Fatalf("Wait() after idempotent Close() error = %v", err)
	}
}

func jsonObjectForClaudeCandidateAdditionalTest() []byte {
	return []byte(`{"snapshot":"canonical"}`)
}
