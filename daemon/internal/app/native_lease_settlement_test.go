package app

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestActiveNativeLeaseExpirySettlesThroughWaiter(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedGoalDeliveryStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	waitTurnGate := make(chan struct{})
	finalWaitGate := make(chan struct{})
	close(finalWaitGate)
	waitTurnEntered := make(chan struct{}, 1)
	session := &fakeNativeGoalSession{
		result: harness.TaskResult{
			Kind:  harness.ResultCancelled,
			Usage: harness.Usage{State: harness.UsageReported, InputTokens: 13, OutputTokens: 8, CostMicrousd: "34"},
		},
		waitGate:        waitTurnGate,
		finalWaitGate:   finalWaitGate,
		turnStarted:     make(chan struct{}),
		waitTurnEntered: waitTurnEntered,
		processPID:      71,
		processIdentity: "native:71",
	}
	runContext, cancelRun := context.WithCancel(context.Background())
	active := &runningRun{
		claimed: true, cancel: cancelRun, nativeSession: session, goalSession: &sessionKey,
		goalAdmission: &admission, slotHeld: true, cleanupBlocked: true,
	}
	app := &daemon{
		config: testConfig(t), store: store, log: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{key: active}, slots: make(chan struct{}, 1),
		options: options{newID: ids(), clock: time.Now},
	}
	app.slots <- struct{}{}
	done := make(chan struct{})
	go func() {
		app.waitForNativeRun(runContext, key, active, session)
		close(done)
	}()
	awaitNativeSettlementSignal(t, waitTurnEntered, "active native waiter did not enter WaitTurn")

	app.terminateForLease(journalForKey(t, store, key), "lease expired during active native turn")
	awaitNativeSettlementSignal(t, done, "active native lease settlement did not finish")

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "stale" || journal.HasProcessDetails() || durableTerminalPresent(journal) {
		t.Fatalf("active native lease settlement journal = %#v", journal)
	}
	usageDeliveries := 0
	for _, delivery := range append(append([]state.GoalDelivery(nil), journal.PendingGoalDeliveries...), journal.DeliveredGoalDeliveries...) {
		if delivery.Kind == state.GoalDeliveryUsage && delivery.DeliveryID == nativeGoalUsageKey {
			usageDeliveries++
		}
	}
	if usageDeliveries != 1 {
		t.Fatalf("active native lease usage deliveries = %d, journal=%#v", usageDeliveries, journal)
	}
	closeCalls, waitCalls := 0, 0
	for _, call := range session.callsSnapshot() {
		switch call {
		case "close":
			closeCalls++
		case "wait":
			waitCalls++
		}
	}
	if closeCalls != 1 || waitCalls != 2 {
		t.Fatalf("active native lease calls = %#v, want one Close, one stop-proof Wait, and one final Wait", session.callsSnapshot())
	}
	if app.runningRun(key) != nil {
		t.Fatalf("active native lease retained run owner: %#v", active)
	}
}

func TestObservedNativeTerminalClaimsAfterDeadlineBeforeWatchdog(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedGoalDeliveryStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	waitTurnGate := make(chan struct{})
	close(waitTurnGate)
	finalWaitGate := make(chan struct{})
	close(finalWaitGate)
	session := &fakeNativeGoalSession{
		result:   harness.TaskResult{Kind: harness.ResultCancelled},
		waitGate: waitTurnGate, finalWaitGate: finalWaitGate, turnStarted: make(chan struct{}),
	}
	active := &runningRun{
		claimed: true, nativeSession: session, goalSession: &sessionKey, goalAdmission: &admission,
		localLeaseDeadlineAt: time.Now().Add(-time.Second), cleanupBlocked: true,
	}
	app := &daemon{
		config: testConfig(t), store: store, running: map[state.RunKey]*runningRun{key: active},
		options: options{newID: ids(), clock: time.Now},
	}

	app.waitForNativeRun(context.Background(), key, active, session)
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "terminal_pending" || journal.TerminalState != "cancelled" {
		t.Fatalf("observed native terminal lost to elapsed deadline: %#v", journal)
	}
	if active.nativeTerminalOwner || active.terminalizing != 0 {
		t.Fatalf("observed native terminal retained owner: %#v", active)
	}
}
