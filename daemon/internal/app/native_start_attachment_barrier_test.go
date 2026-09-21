package app

import (
	"context"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestNativeStartAttachmentBarrierDefersCancellationUntilReceipt(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), nil)
	defer store.Close()

	readyEntered := make(chan struct{}, 1)
	readyRelease := make(chan struct{})
	app.options.markGoalSessionAttachDeliveryReady = func(key state.RunKey, localHandleID string) (state.RunJournal, error) {
		readyEntered <- struct{}{}
		<-readyRelease
		return store.MarkGoalSessionAttachDeliveryReady(key, localHandleID)
	}

	app.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: controlClient.work})
	select {
	case <-readyEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("native start did not reach the durable ready marker")
	}

	key := state.RunKey{RunID: "run-1", Generation: 1}
	active := app.runningRun(key)
	if active == nil || !active.nativeStartInFlight {
		t.Fatalf("native start barrier = %#v, want in flight while ready marker is blocked", active)
	}
	if app.handleCommand(context.Background(), protocol.Command{
		RunID: key.RunID, Generation: key.Generation, CommandID: "cancel-during-attach-ready", Kind: "cancel",
	}) {
		t.Fatal("cancellation was acknowledged before attachment receipt completed")
	}
	for _, call := range session.callsSnapshot() {
		if call == "control:cancel" || call == "close" {
			t.Fatalf("cancellation crossed the attachment barrier: %#v", session.callsSnapshot())
		}
	}

	close(readyRelease)
	app.workers.Wait()

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	terminalTransitions := 0
	for _, transition := range journal.PendingTransitions {
		if isTerminalTransition(transition.State) {
			terminalTransitions++
		}
	}
	if journal.TerminalState == "" || terminalTransitions != 1 {
		t.Fatalf("attachment cancellation terminal state = %q, transitions = %#v, want exactly one terminal", journal.TerminalState, journal.PendingTransitions)
	}
	if calls := session.callsSnapshot(); countNativeStartCall(calls, "close") != 1 || countNativeStartCall(calls, "control:cancel") != 0 {
		t.Fatalf("native cancellation lifecycle = %#v, want one close and no cancel control", calls)
	}
}

func countNativeStartCall(calls []string, want string) int {
	count := 0
	for _, call := range calls {
		if call == want {
			count++
		}
	}
	return count
}
