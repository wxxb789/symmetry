package app

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestNativeStopProofStillClosesSessionWithoutIdentity(t *testing.T) {
	for _, staged := range []bool{true, false} {
		t.Run(map[bool]string{true: "partial_identity", false: "unstaged"}[staged], func(t *testing.T) {
			session := &stopProofSession{pid: 71}
			var owner harness.Session = session
			if !staged {
				owner = struct{ harness.Session }{session}
			}
			if _, _, err := closeNativeSessionWithStopProof(owner); err == nil {
				t.Fatal("missing identity was accepted as stop proof")
			}
			if !slices.Equal(session.calls, []string{"close", "wait"}) {
				t.Fatalf("cleanup calls = %v, want Close then Wait despite missing identity", session.calls)
			}
		})
	}
}

func TestNativeStopProofRejectsUnprovenFinalResult(t *testing.T) {
	for _, test := range []struct {
		name   string
		result execution.Result
	}{
		{name: "containment", result: execution.Result{ContainmentError: errors.New("owned group remained")}},
		{name: "termination", result: execution.Result{TerminationError: errors.New("hard termination failed")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			session := &stopProofSession{pid: 71, identity: "native:71", result: test.result}
			if _, _, err := closeNativeSessionWithStopProof(session); err == nil {
				t.Fatal("unproven final process result authorized stop proof")
			}
			if !slices.Equal(session.calls, []string{"close", "wait"}) {
				t.Fatalf("cleanup calls = %v", session.calls)
			}
		})
	}
}

func TestNativeStopProofDoesNotConfuseTaskFailureWithProcessStop(t *testing.T) {
	session := &stopProofSession{
		pid: 71, identity: "native:71",
		result: execution.Result{PID: 71, ExitCode: 7, WaitError: errors.New("exit status 7")},
	}
	pid, identity, err := closeNativeSessionWithStopProof(session)
	if err != nil || pid != 71 || identity != "native:71" {
		t.Fatalf("stop proof = %d %q %v, want original identity despite task failure", pid, identity, err)
	}
}

func TestNativeFinalStopErrorsCannotCreateStopReceipt(t *testing.T) {
	stopFailure := errors.New("native stop remains unproven")
	for _, test := range []struct {
		name      string
		identity  string
		result    execution.Result
		waitErr   error
		wantCause error
	}{
		{name: "containment", identity: "native:71", result: execution.Result{ContainmentError: stopFailure}, wantCause: stopFailure},
		{name: "termination", identity: "native:71", result: execution.Result{TerminationError: stopFailure}, wantCause: stopFailure},
		{name: "wait", identity: "native:71", waitErr: stopFailure, wantCause: stopFailure},
		{name: "missing_identity"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, key := claimedGoalDeliveryStore(t)
			defer store.Close()
			sessionKey, _, _ := saveRetainedGoalSession(t, store, key)
			if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			session := &stopProofSession{pid: 71, identity: test.identity, result: test.result, waitErr: test.waitErr}
			active := &runningRun{nativeSession: session, goalSession: &sessionKey}
			app := &daemon{store: store, running: map[state.RunKey]*runningRun{key: active}, options: options{clock: time.Now}}
			if err := app.closeNativeGoalSession(key, active, session); err == nil || (test.wantCause != nil && !errors.Is(err, test.wantCause)) {
				t.Fatalf("native close error = %v, want raw stop failure", err)
			}
			if !slices.Equal(session.calls, []string{"close", "wait"}) {
				t.Fatalf("unproven native identity/result skipped owned cleanup: %v", session.calls)
			}
			journal, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if !journal.HasProcessDetails() || !active.cleanupBlocked || !active.nativeCloseRetryRequired {
				t.Fatal("unproven final process result released recovery ownership")
			}
			for _, delivery := range journal.PendingGoalDeliveries {
				if delivery.Kind == state.GoalDeliverySessionStopped {
					t.Fatal("Close(nil) created a stopped receipt despite the final process error")
				}
			}
		})
	}
}

func TestNativeHandlePersistenceAndDiscardFailureStillClosesOwner(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission: %v", err)
	}
	app, store, session, client := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
	defer store.Close()
	key := state.RunKey{RunID: "run-1", Generation: 1}
	saveClaimedGoalRun(t, store, key)
	app.running[key] = &runningRun{starting: true, claimed: true}
	// The native Open reply cannot be persisted as a valid handle. Failing
	// its separate outbox compensation must still leave the Session closable.
	session.handle = harness.NativeSessionHandle{}
	discardFailure := errors.New("discard write failed")
	discardCalls := 0
	app.options.discardUnreadyGoalSessionAttachDelivery = func(state.RunKey, string) (state.RunJournal, error) {
		discardCalls++
		return state.RunJournal{}, discardFailure
	}
	claim := protocol.ClaimResponse{RunID: key.RunID, Generation: key.Generation, TaskID: "task-1", Work: client.work}
	err = app.startGoalAdmission(context.Background(), key, claim, admission, nil)
	if !errors.Is(err, discardFailure) || discardCalls != 1 {
		t.Fatalf("start error = %v, discard calls = %d", err, discardCalls)
	}
	if !slices.Contains(session.calls, "close") || !slices.Contains(session.calls, "wait") || slices.Contains(session.calls, "start_turn") {
		t.Fatalf("failed handle persistence skipped cleanup or started work: %v", session.calls)
	}
	if len(client.calls) != 0 {
		t.Fatalf("failed handle persistence sent Goal requests: %v", client.calls)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.HasProcessDetails() || !journal.RetainWorkspace {
		t.Fatalf("confirmed close lost marker-clear or retained-work evidence: %+v", journal)
	}
}

type stopProofSession struct {
	closeFailureGoalSession
	pid      int
	identity string
	result   execution.Result
	waitErr  error
	calls    []string
}

func (session *stopProofSession) ProcessDetails() (int, string) {
	return session.pid, session.identity
}

func (session *stopProofSession) Close(context.Context) error {
	session.calls = append(session.calls, "close")
	return nil
}

func (session *stopProofSession) Wait(context.Context) (harness.TaskResult, error) {
	session.calls = append(session.calls, "wait")
	return harness.TaskResult{Kind: harness.ResultFailed, Process: session.result}, session.waitErr
}
