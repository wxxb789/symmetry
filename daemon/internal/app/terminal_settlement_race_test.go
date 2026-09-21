package app

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestConcurrentTerminalSettlementKeepsFirstDurableTerminal(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()

	now := time.Date(2026, time.September, 20, 1, 2, 3, 0, time.UTC)
	entered := make(chan protocol.StateTransitionRequest, 2)
	releaseCompleted := make(chan struct{})
	releaseFailed := make(chan struct{})
	app := &daemon{
		store: store,
		options: options{
			clock: func() time.Time { return now },
			newID: ids(),
			queueTerminalTransition: func(key state.RunKey, transition protocol.StateTransitionRequest, pendingAt time.Time) (state.RunJournal, error) {
				entered <- transition
				if transition.State == "completed" {
					<-releaseCompleted
				} else {
					<-releaseFailed
				}
				return store.QueueTerminalTransitionAt(key, transition, pendingAt)
			},
		},
		running: map[state.RunKey]*runningRun{key: {}},
	}

	completedDone := make(chan error, 1)
	failedDone := make(chan error, 1)
	go func() {
		completedDone <- app.queueTerminalTransitionWithRetry(context.Background(), key, "completed", map[string]string{"result": "accepted"})
	}()
	go func() {
		failedDone <- app.queueTerminalTransitionWithRetry(context.Background(), key, "failed", map[string]string{"reason": "late failure"})
	}()

	first := <-entered
	second := <-entered
	if first.TransitionID == second.TransitionID || first.State == second.State {
		t.Fatalf("terminal attempts = %#v and %#v, want different IDs and states", first, second)
	}
	winner := first
	if second.State == "completed" {
		winner = second
	}

	close(releaseCompleted)
	if err := <-completedDone; err != nil {
		t.Fatalf("completed settlement error = %v", err)
	}
	winnerJournal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if winnerJournal.LocalState != "terminal_pending" || winnerJournal.TerminalState != winner.State || len(winnerJournal.PendingTransitions) != 1 || !sameStateTransitionRequest(winnerJournal.PendingTransitions[0], transitionWithJournalFence(winner, winnerJournal)) {
		t.Fatalf("durable terminal = %#v, want only %#v", winnerJournal, winner)
	}

	close(releaseFailed)
	if err := <-failedDone; err == nil || !errors.Is(err, errAuthoritativeTerminal) {
		t.Fatalf("failed settlement error = %v, want authoritative terminal conflict", err)
	}
	afterConflict, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterConflict, winnerJournal) {
		t.Fatalf("losing terminal replaced durable winner:\nwinner=%#v\nafter=%#v", winnerJournal, afterConflict)
	}

	app.options.newID = func() (string, error) { return winner.TransitionID, nil }
	if err := app.queueTerminalTransitionWithRetry(context.Background(), key, "completed", map[string]string{"result": "accepted"}); err != nil {
		t.Fatalf("winner replay error = %v", err)
	}
	afterReplay, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterReplay, winnerJournal) {
		t.Fatalf("winner replay changed durable settlement:\nwinner=%#v\nafter=%#v", winnerJournal, afterReplay)
	}
}

func TestConclusiveTerminalFinalizesMatchingUsageOwner(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	journal, err := store.QueueTerminalTransitionAndAcknowledgementAt(
		key,
		protocol.StateTransitionRequest{TransitionID: "terminal-1", State: "failed", Payload: []byte(`{"reason":"process_failure"}`)},
		protocol.CommandAcknowledgement{CommandID: "cancel-1", Outcome: "failed", AckID: "ack-1"},
		time.Now().UTC(),
	)
	if err != nil {
		t.Fatal(err)
	}
	active := &runningRun{
		nativeTerminalOwner:         true,
		nativeTerminalOwnerClaim:    3,
		nativeTerminalOwnerSequence: 3,
		nativeUsageRetryOwnerClaim:  3,
		nativeUsageRetryPending:     true,
		nativeUsageRetryExhausted:   true,
		terminalizing:               1,
		cleanupBlocked:              true,
	}
	app := &daemon{store: store, running: map[state.RunKey]*runningRun{key: active}, options: options{clock: time.Now}}
	deliveryErr := &control.APIError{StatusCode: http.StatusConflict, Code: control.OwnershipLost, Message: "ownership lost"}
	if err := app.handleTerminalDeliveryError(journal, deliveryErr); !errors.Is(err, deliveryErr) {
		t.Fatalf("handle terminal delivery error = %v", err)
	}
	if active.nativeTerminalOwner || active.nativeTerminalOwnerClaim != 0 || active.nativeUsageRetryOwnerClaim != 0 || active.nativeUsageRetryPending || active.nativeUsageRetryInFlight || active.nativeUsageRetryExhausted || active.terminalizing != 0 || active.cleanupBlocked || !active.nativeUsageFinalized {
		t.Fatalf("conclusive terminal retained matching owner state: %#v", active)
	}
	resolved, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.TerminalVerdict != state.TerminalVerdictOwnershipLost || len(resolved.PendingTransitions) != 0 || len(resolved.PendingCommandAcknowledgements) != 0 {
		t.Fatalf("conclusive terminal journal = %#v", resolved)
	}
}
