package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestStartAssignedStalePersistenceFailureRetainsOwnerUntilRetry(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()

	active := &runningRun{starting: true, stale: true, slotHeld: true, cleanupBlocked: true}
	daemon := &daemon{
		store:       store,
		log:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running:     map[state.RunKey]*runningRun{key: active},
		slots:       make(chan struct{}, 1),
		cleanupWake: make(chan struct{}, 1),
		background:  context.Background(),
	}
	daemon.slots <- struct{}{}

	restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
		return errors.New("injected stale persistence failure")
	})
	daemon.startAssigned(context.Background(), key, assignmentForKey(key))
	restore()

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState == "stale" {
		t.Fatal("stale state unexpectedly persisted during injected failure")
	}
	if daemon.runningRun(key) != active {
		t.Fatal("startAssigned released the owner before stale state became durable")
	}
	if cleanupQueued(daemon, key) {
		t.Fatal("cleanup was queued before stale state became durable")
	}

	daemon.startAssigned(context.Background(), key, assignmentForKey(key))
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "stale" {
		t.Fatalf("retry local state = %q, want stale", journal.LocalState)
	}
	if daemon.runningRun(key) != nil {
		t.Fatal("stale retry did not release the owner after durable persistence")
	}
	if !cleanupQueued(daemon, key) {
		t.Fatal("cleanup was not queued after stale state became durable")
	}
}

func TestTerminateForLeaseResolvesUnknownStaleWriteByReadback(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()

	active := &runningRun{stale: true, slotHeld: true, cleanupBlocked: true}
	daemon := &daemon{
		store:       store,
		log:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running:     map[state.RunKey]*runningRun{key: active},
		slots:       make(chan struct{}, 1),
		cleanupWake: make(chan struct{}, 1),
		background:  context.Background(),
	}
	daemon.slots <- struct{}{}

	attempts := 0
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		attempts++
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
		return errors.New("injected post-rename unknown stale write")
	})
	daemon.terminateForLease(journalForKey(t, store, key), "lease expired")
	restore()

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "stale" {
		t.Fatalf("unknown stale write local state = %q, want stale", journal.LocalState)
	}
	if attempts != 1 {
		t.Fatalf("unknown stale write attempts = %d, want one write resolved by readback", attempts)
	}
	if !cleanupQueued(daemon, key) {
		t.Fatal("cleanup was not queued after readback resolved stale persistence")
	}
}

func TestTerminateForLeaseNativeStartStalePersistenceFailureRetainsRecoveryOwner(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()

	active := &runningRun{nativeStartInFlight: true, slotHeld: true, cleanupBlocked: true}
	daemon := &daemon{
		store:       store,
		log:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running:     map[state.RunKey]*runningRun{key: active},
		slots:       make(chan struct{}, 1),
		cleanupWake: make(chan struct{}, 1),
		background:  context.Background(),
	}
	daemon.slots <- struct{}{}

	restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
		return errors.New("injected native-start stale persistence failure")
	})
	daemon.terminateForLease(journalForKey(t, store, key), "lease expired during native start")
	restore()

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState == "stale" {
		t.Fatal("native-start stale state unexpectedly persisted during injected failure")
	}
	if daemon.runningRun(key) != active || !active.nativeStartLeaseExpired || !active.stale {
		t.Fatalf("native-start recovery owner was released before stale persistence: active=%#v", active)
	}
	if cleanupQueued(daemon, key) {
		t.Fatal("cleanup was queued before native-start stale state became durable")
	}
}

func TestStartAssignedStaleCleanupKeepsNativeRecoveryOwner(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()

	active := &runningRun{
		starting:            true,
		stale:               true,
		nativeSession:       &closeFailureGoalSession{},
		slotHeld:            true,
		cleanupBlocked:      true,
		nativeStartInFlight: false,
	}
	daemon := &daemon{
		store:       store,
		log:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running:     map[state.RunKey]*runningRun{key: active},
		slots:       make(chan struct{}, 1),
		cleanupWake: make(chan struct{}, 1),
		background:  context.Background(),
	}
	daemon.slots <- struct{}{}

	daemon.startAssigned(context.Background(), key, assignmentForKey(key))

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "stale" {
		t.Fatalf("native recovery stale state = %q, want stale", journal.LocalState)
	}
	if daemon.runningRun(key) != active || !active.cleanupBlocked {
		t.Fatalf("stale cleanup discarded native recovery owner: active=%#v", active)
	}
	if !cleanupQueued(daemon, key) {
		t.Fatal("native recovery cleanup was not queued after stale persistence")
	}
}

func assignmentForKey(key state.RunKey) protocol.Assignment {
	return protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "g"}}
}

func journalForKey(t *testing.T, store *state.Store, key state.RunKey) state.RunJournal {
	t.Helper()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	return journal
}

func cleanupQueued(daemon *daemon, key state.RunKey) bool {
	daemon.mu.Lock()
	defer daemon.mu.Unlock()
	_, ok := daemon.cleanupQueued[key]
	return ok
}
