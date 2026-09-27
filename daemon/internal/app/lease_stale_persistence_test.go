package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

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

func expireLeaseBeforeCleanup(t *testing.T, store *state.Store, key state.RunKey) state.RunJournal {
	t.Helper()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	journal.LeaseExpiresAt = time.Now().UTC().Add(-time.Minute)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	return journal
}

func TestCleanupRetainsStaleJournalUntilLeaseExpiry(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	journal, err := store.SetLocalState(key, "stale")
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "" || journal.HasProcessDetails() || len(journal.PendingCommandAcknowledgements) != 0 {
		t.Fatalf("stale cleanup precondition = %#v", journal)
	}
	now := journal.LeaseExpiresAt.Add(-time.Second)
	app := &daemon{
		store:   store,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: make(map[state.RunKey]*runningRun),
		options: options{newID: ids(), clock: func() time.Time { return now }},
	}
	if err := app.cleanupPending(context.Background(), journal); err == nil {
		t.Fatal("cleanup removed a stale journal before lease expiry")
	}
	if _, err := store.LoadJournal(key); err != nil {
		t.Fatalf("stale journal before lease expiry = %v, want retained", err)
	}
	cancel := protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"}
	if !app.handleCommand(context.Background(), cancel) {
		t.Fatal("cancel was not acknowledged without an in-memory owner")
	}
	journal, err = store.LoadJournal(key)
	if err != nil || journal.TerminalState != "cancelled" || !hasPendingCommandAcknowledgementOutcome(journal, "cancel-1", "applied") {
		t.Fatalf("stale cancel = journal:%#v error:%v, want cancelled with applied acknowledgement", journal, err)
	}

	expiredStore, expiredKey := claimedStore(t)
	defer expiredStore.Close()
	expiredJournal, err := expiredStore.SetLocalState(expiredKey, "stale")
	if err != nil {
		t.Fatal(err)
	}
	if expiredJournal.TerminalState != "" || expiredJournal.HasProcessDetails() || len(expiredJournal.PendingCommandAcknowledgements) != 0 {
		t.Fatalf("expired cleanup precondition = %#v", expiredJournal)
	}
	expiredNow := expiredJournal.LeaseExpiresAt.Add(time.Second)
	expiredDaemon := &daemon{
		store:   expiredStore,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options: options{clock: func() time.Time { return expiredNow }},
	}
	if err := expiredDaemon.cleanupPending(context.Background(), expiredJournal); err != nil {
		t.Fatalf("cleanup after lease expiry: %v", err)
	}
	if _, err := expiredStore.LoadJournal(expiredKey); !state.IsNotFound(err) {
		t.Fatalf("expired stale journal = %v, want deleted", err)
	}
}

// Control moves a cancelled Run to cancelling and then rejects its lease
// renewal, so the daemon sees lease loss before it handles the cancel. The
// cancel must still settle once the exact process owner is proven stopped.
func TestStaleCancelSettlesAfterProvenProcessStop(t *testing.T) {
	for _, order := range []string{"cancel-before-exit", "cancel-after-exit"} {
		t.Run(order, func(t *testing.T) {
			store, key := claimedStore(t)
			defer store.Close()
			if _, err := store.SetProcessDetails(key, 44, "test:44", time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			process := newBlockingProcess()
			active := &runningRun{process: process, claimed: true, slotHeld: true, cleanupBlocked: true}
			slots := make(chan struct{}, 1)
			slots <- struct{}{}
			daemon := &daemon{
				store:       store,
				log:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
				running:     map[state.RunKey]*runningRun{key: active},
				slots:       slots,
				cleanupWake: make(chan struct{}, 1),
				background:  context.Background(),
				options:     options{newID: ids(), clock: time.Now},
			}
			cancel := protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"}

			daemon.terminateForLease(journalForKey(t, store, key), "lease renewal failed")
			if process.terminations != 1 {
				t.Fatalf("lease loss terminations = %d, want 1", process.terminations)
			}
			if order == "cancel-before-exit" {
				if daemon.handleCommand(context.Background(), cancel) {
					t.Fatal("stale cancel was acknowledged before the process stop was proven")
				}
				if journal := journalForKey(t, store, key); len(journal.PendingCommandAcknowledgements) != 0 || journal.TerminalState != "" {
					t.Fatalf("stale cancel published a receipt before the stop witness: %#v", journal)
				}
			}
			daemon.waitForRunWithContext(context.Background(), key)
			if order == "cancel-after-exit" && !daemon.handleCommand(context.Background(), cancel) {
				t.Fatal("stale cancel after the proven stop was not acknowledged")
			}

			journal := journalForKey(t, store, key)
			if journal.TerminalState != "cancelled" || !hasPendingCommandAcknowledgementOutcome(journal, "cancel-1", "applied") {
				t.Fatalf("stale cancel journal = state %q acks %#v, want cancelled with applied receipt", journal.TerminalState, journal.PendingCommandAcknowledgements)
			}
			if !daemon.handleCommand(context.Background(), cancel) {
				t.Fatal("replayed stale cancel was not treated as already acknowledged")
			}
			if again := journalForKey(t, store, key); len(again.PendingCommandAcknowledgements) != 1 {
				t.Fatalf("replayed stale cancel duplicated its receipt: %#v", again.PendingCommandAcknowledgements)
			}
		})
	}
}

func TestStaleCancelWithoutStopWitnessStaysUnacknowledged(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetProcessDetails(key, 99, "test:99", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	process := &failingTerminateProcess{}
	active := &runningRun{process: process, claimed: true, slotHeld: true, cleanupBlocked: true}
	daemon := &daemon{
		store:       store,
		log:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running:     map[state.RunKey]*runningRun{key: active},
		slots:       make(chan struct{}, 1),
		cleanupWake: make(chan struct{}, 1),
		background:  context.Background(),
		options:     options{newID: ids(), clock: time.Now},
	}
	daemon.terminateForLease(journalForKey(t, store, key), "lease renewal failed")
	if daemon.handleCommand(context.Background(), protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"}) {
		t.Fatal("stale cancel was acknowledged without a process stop witness")
	}
	journal := journalForKey(t, store, key)
	if journal.LocalState != "stale" || journal.TerminalState != "" || len(journal.PendingCommandAcknowledgements) != 0 || !journal.HasProcessDetails() {
		t.Fatalf("unproven stale cancel changed durable state: %#v", journal)
	}
}

func TestStaleCancelAfterOwnerReleaseSettlesThroughRecovery(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetLocalState(key, "stale"); err != nil {
		t.Fatal(err)
	}
	daemon := &daemon{
		store:   store,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{},
		options: options{newID: ids(), clock: time.Now},
	}
	cancel := protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"}
	if !daemon.handleCommand(context.Background(), cancel) {
		t.Fatal("stale cancel after owner release was not acknowledged")
	}
	journal := journalForKey(t, store, key)
	if journal.TerminalState != "cancelled" || !hasPendingCommandAcknowledgementOutcome(journal, "cancel-1", "applied") {
		t.Fatalf("stale cancel after owner release = state %q acks %#v, want cancelled with applied receipt", journal.TerminalState, journal.PendingCommandAcknowledgements)
	}
}

func TestStaleCancelAfterOwnerReleaseKeepsUnprovenProcess(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetProcessDetails(key, 42, "test:42", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetLocalState(key, "stale"); err != nil {
		t.Fatal(err)
	}
	daemon := &daemon{
		store:   store,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{},
		options: options{newID: ids(), clock: time.Now},
	}
	if daemon.handleCommand(context.Background(), protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"}) {
		t.Fatal("stale cancel acknowledged a recorded process that recovery cannot stop")
	}
	journal := journalForKey(t, store, key)
	if journal.TerminalState != "" || len(journal.PendingCommandAcknowledgements) != 0 || !journal.HasProcessDetails() {
		t.Fatalf("unproven stale cancel changed durable state: %#v", journal)
	}
}

func TestStaleCancelAfterOwnerReleaseStopsExactProcessBeforeReceipt(t *testing.T) {
	for _, stopErr := range []error{nil, errors.New("process identity mismatch")} {
		t.Run(fmt.Sprintf("stop error %v", stopErr), func(t *testing.T) {
			store, key := claimedStore(t)
			defer store.Close()
			if _, err := store.SetProcessDetails(key, 42, "test:42", time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			if _, err := store.SetLocalState(key, "stale"); err != nil {
				t.Fatal(err)
			}
			stops := 0
			daemon := &daemon{
				store:   store,
				log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
				running: map[state.RunKey]*runningRun{},
				options: options{newID: ids(), clock: time.Now, terminatePersist: func(pid int, identity string) error {
					if pid != 42 || identity != "test:42" {
						t.Fatalf("recovered stop target = (%d, %q), want recorded (42, test:42)", pid, identity)
					}
					if journal := journalForKey(t, store, key); journal.TerminalState != "" || len(journal.PendingCommandAcknowledgements) != 0 {
						t.Fatalf("receipt published before the recorded process stop: %#v", journal)
					}
					stops++
					return stopErr
				}},
			}
			acknowledged := daemon.handleCommand(context.Background(), protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"})
			journal := journalForKey(t, store, key)
			if stops != 1 {
				t.Fatalf("recorded process stops = %d, want 1", stops)
			}
			if stopErr != nil {
				if acknowledged || journal.TerminalState != "" || len(journal.PendingCommandAcknowledgements) != 0 || !journal.HasProcessDetails() {
					t.Fatalf("failed stop acknowledged=%v changed durable state: %#v", acknowledged, journal)
				}
				return
			}
			if !acknowledged || journal.HasProcessDetails() || journal.TerminalState != "cancelled" || !hasPendingCommandAcknowledgementOutcome(journal, "cancel-1", "applied") {
				t.Fatalf("proven stop acknowledged=%v journal = %#v, want cleared process and applied cancel receipt", acknowledged, journal)
			}
		})
	}
}
