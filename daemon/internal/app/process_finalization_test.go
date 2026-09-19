package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestCleanupFinalizesExitedProcessWithoutWaitingOrReterminating(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetProcessDetails(key, 42, "finalize:42", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransition(key, stateTransitionForTest("failed")); err != nil {
		t.Fatal(err)
	}
	process := &finalizingProcess{pid: 42, identity: "finalize:42"}
	daemon := &daemon{
		store:   store,
		running: map[state.RunKey]*runningRun{key: {process: process}},
		options: options{clock: func() time.Time { return time.Now().UTC() }},
	}
	active := daemon.running[key]
	if !daemon.recordGenericProcessExit(key, active, process, 42, "finalize:42", false) {
		t.Fatal("recordGenericProcessExit() rejected the live owner")
	}

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	ready, err := daemon.resolveCleanupProcessMarker(context.Background(), journal)
	if err != nil || !ready {
		t.Fatalf("resolveCleanupProcessMarker() = (%t, %v), want ready", ready, err)
	}
	if process.finalizeCalls != 1 || process.terminateCalls != 0 {
		t.Fatalf("finalization calls = %d, terminate calls = %d, want 1 and 0", process.finalizeCalls, process.terminateCalls)
	}
	cleared, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.HasProcessDetails() {
		t.Fatalf("process marker remained after successful finalization: %#v", cleared)
	}
}

func TestCleanupFinalizationRetriesAreBoundedPerOwner(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetProcessDetails(key, 43, "finalize:43", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransition(key, stateTransitionForTest("failed")); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	process := &finalizingProcess{pid: 43, identity: "finalize:43", finalizeErr: errors.New("receipt persistence failed")}
	active := &runningRun{process: process}
	daemon := &daemon{
		store:   store,
		running: map[state.RunKey]*runningRun{key: active},
		options: options{clock: func() time.Time { return now }},
	}
	if !daemon.recordGenericProcessExit(key, active, process, 43, "finalize:43", false) {
		t.Fatal("recordGenericProcessExit() rejected the live owner")
	}
	daemon.enqueueCleanup(key)
	for attempt := 0; attempt < processFinalizeRetryLimit; attempt++ {
		if attempt > 0 {
			now = now.Add(cleanupRetryMinimum * time.Duration(1<<(attempt-1)))
		}
		daemon.flushCleanups(context.Background())
	}
	if process.finalizeCalls != processFinalizeRetryLimit {
		t.Fatalf("finalization calls = %d, want %d", process.finalizeCalls, processFinalizeRetryLimit)
	}
	if active.processFinalizeExhausted == false || len(daemon.cleanupQueued) != 0 {
		t.Fatalf("retry state = exhausted:%t queued:%d, want exhausted and no queued retry", active.processFinalizeExhausted, len(daemon.cleanupQueued))
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.HasProcessDetails() {
		t.Fatal("bounded finalization retries erased the unresolved process marker")
	}
}

func TestCleanupFinalizationDoesNotAuthorizeReplacementOwner(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetProcessDetails(key, 44, "finalize:44", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	old := &finalizingProcess{pid: 44, identity: "finalize:44"}
	newOwner := &runningRun{process: &finalizingProcess{pid: 45, identity: "finalize:45"}}
	active := &runningRun{process: old, processExited: true}
	daemon := &daemon{
		store:   store,
		running: map[state.RunKey]*runningRun{key: active},
		options: options{clock: time.Now},
	}
	old.onFinalize = func() { daemon.mu.Lock(); daemon.running[key] = newOwner; daemon.mu.Unlock() }
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	finalized, err := daemon.finalizeExitedProcess(key, journal, active)
	if err != nil || finalized {
		t.Fatalf("finalizeExitedProcess() = (%t, %v), want replacement-safe no-op", finalized, err)
	}
	if active.processStopWitness != nil || newOwner.processStopWitness != nil {
		t.Fatal("finalization result crossed the Process owner replacement")
	}
}

type finalizingProcess struct {
	pid            int
	identity       string
	finalizeErr    error
	onFinalize     func()
	finalizeCalls  int
	terminateCalls int
}

func (*finalizingProcess) WriteInput([]byte) error { return nil }

func (process *finalizingProcess) Terminate(context.Context, time.Duration) error {
	process.terminateCalls++
	return nil
}

func (process *finalizingProcess) Wait() execution.Result { return execution.Result{} }

func (process *finalizingProcess) ProcessDetails() (int, string) {
	return process.pid, process.identity
}

func (process *finalizingProcess) FinalizeContainment() error {
	process.finalizeCalls++
	if process.onFinalize != nil {
		process.onFinalize()
	}
	return process.finalizeErr
}

func stateTransitionForTest(stateName string) protocol.StateTransitionRequest {
	return protocol.StateTransitionRequest{TransitionID: "finalize-" + stateName, State: stateName, Payload: []byte(`{"reason":"process_failure"}`)}
}
