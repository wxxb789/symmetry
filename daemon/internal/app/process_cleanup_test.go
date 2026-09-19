package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

func TestAcceptedTerminalWaitsForUnresolvedFailedStartProcessMarker(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	persistWorkspacePath(t, store, key, "C:\\workspace")
	if _, err := store.SetProcessDetails(key, 42, "test:42", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	journal, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{
		TransitionID: "failed-start", State: "failed", Payload: json.RawMessage(`{"reason":"process_failure"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	active := &runningRun{process: newStartFailureProcess(), cleanupBlocked: true, slotHeld: true}
	d := &daemon{
		config:  config.Config{CleanupTimeoutMS: 10000},
		store:   store,
		control: &fakeControl{},
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{key: active},
		slots:   make(chan struct{}, 1),
		options: options{newID: ids(), clock: time.Now},
	}
	d.slots <- struct{}{}
	if err := d.flushRun(context.Background(), journal); err != nil {
		t.Fatalf("flushRun() error = %v", err)
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalVerdict != state.TerminalVerdictAccepted || journal.LocalState != "terminal_pending" || journal.PID != 42 || journal.ProcessIdentity != "test:42" {
		t.Fatalf("accepted terminal discarded unresolved failed-start recovery ownership: %#v", journal)
	}
}

func TestConfirmedGenericProcessExitClearsMarkerBeforeTerminalCleanup(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	persistWorkspacePath(t, store, key, "C:\\workspace")
	if _, err := store.SetProcessDetails(key, 42, "test:42", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	active := &runningRun{process: fakeProcess{result: execution.Result{}}, cleanupBlocked: true, slotHeld: true}
	d := &daemon{
		config:    config.Config{CleanupTimeoutMS: 10000},
		store:     store,
		control:   &fakeControl{},
		workspace: failingCleanupWorkspace{},
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running:   map[state.RunKey]*runningRun{key: active},
		slots:     make(chan struct{}, 1),
		options:   options{newID: ids(), clock: time.Now},
	}
	d.slots <- struct{}{}
	d.waitForRunWithContext(context.Background(), key)
	d.flushCleanups(context.Background())
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.flushRun(context.Background(), journal); err == nil {
		t.Fatal("flushRun() succeeded despite deliberately failing workspace cleanup")
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "cleanup_pending" || journal.PID != 0 || journal.ProcessIdentity != "" || !journal.StartedAt.IsZero() {
		t.Fatalf("confirmed generic process exit did not clear marker before cleanup: %#v", journal)
	}
}

func TestRecoverUnownedGenericTerminalProcessMarkerStopsThenClearsMarker(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetProcessDetails(key, 71, "test:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	journal, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{
		TransitionID: "failed-start", State: "failed", Payload: json.RawMessage(`{"reason":"process_failure"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveTerminal(key, state.TerminalVerdictAccepted, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	terminated := false
	d := &daemon{
		store:   store,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: make(map[state.RunKey]*runningRun),
		options: options{terminatePersist: func(pid int, identity string) error {
			if pid != 71 || identity != "test:71" {
				t.Fatalf("recovered generic process = (%d, %q)", pid, identity)
			}
			terminated = true
			return nil
		}},
	}
	d.enqueueRecoveredCleanups()
	if terminated {
		t.Fatal("recovery discovery terminated a process outside the cleanup worker")
	}
	d.flushCleanups(context.Background())
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !terminated || journal.LocalState != "terminal_pending" || journal.TerminalVerdict != state.TerminalVerdictAccepted || journal.PID != 0 || journal.ProcessIdentity != "" || !journal.StartedAt.IsZero() {
		t.Fatalf("recovered generic terminal marker = %#v, terminated=%t", journal, terminated)
	}
}

func TestRecoverUnownedGenericProcessMarkerSkipsNonterminalRun(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetProcessDetails(key, 71, "test:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	terminated := false
	d := &daemon{
		store:   store,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: make(map[state.RunKey]*runningRun),
		options: options{terminatePersist: func(int, string) error {
			terminated = true
			return nil
		}},
	}
	d.enqueueRecoveredCleanups()
	d.flushCleanups(context.Background())
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if terminated || !journal.HasProcessDetails() || journal.LocalState == "terminal_pending" || journal.LocalState == "cleanup_pending" || journal.LocalState == "stale" {
		t.Fatalf("nonterminal generic journal was recovered: %#v, terminated=%t", journal, terminated)
	}
}

func TestRecoverUnownedGenericProcessMarkerClearsLegacyTerminalCleanupStates(t *testing.T) {
	for _, test := range []struct {
		name  string
		state string
	}{
		{name: "cleanup pending", state: "cleanup_pending"},
		{name: "stale", state: "stale"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, key := claimedStore(t)
			defer store.Close()
			if test.state == "cleanup_pending" {
				if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{
					TransitionID: "completed", State: "completed", Payload: json.RawMessage(`{}`),
				}); err != nil {
					t.Fatal(err)
				}
				if _, err := store.ResolveTerminal(key, state.TerminalVerdictAccepted, time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
				if _, err := store.MarkTransitionsDelivered(key, []string{"completed"}); err != nil {
					t.Fatal(err)
				}
				journal, err := store.LoadJournal(key)
				if err != nil {
					t.Fatal(err)
				}
				if err := store.DeleteJournal(key); err != nil {
					t.Fatal(err)
				}
				journal.LocalState = test.state
				journal.PID = 71
				journal.ProcessIdentity = "test:71"
				journal.StartedAt = time.Now().UTC()
				if err := store.SaveJournal(journal); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := store.SetProcessDetails(key, 71, "test:71", time.Now().UTC()); err != nil {
					t.Fatal(err)
				}
				if _, err := store.SetLocalState(key, test.state); err != nil {
					t.Fatal(err)
				}
			}
			terminated := false
			d := &daemon{
				store:   store,
				log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
				running: make(map[state.RunKey]*runningRun),
				options: options{terminatePersist: func(int, string) error {
					terminated = true
					return nil
				}},
			}
			d.enqueueRecoveredCleanups()
			d.flushCleanups(context.Background())
			if _, err := store.LoadJournal(key); !state.IsNotFound(err) || !terminated {
				t.Fatalf("recovered generic %s journal was not safely retired: error=%v, terminated=%t", test.state, err, terminated)
			}
		})
	}
}

func TestRecoverUnownedGenericProcessMarkersKeepsUnprovenMarkerAndContinues(t *testing.T) {
	store, first := claimedStore(t)
	defer store.Close()
	second := state.RunKey{RunID: "run-2", Generation: 1}
	if _, err := store.SaveClaimIntent(state.ClaimIntent{Key: second, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1, ClaimID: "claim-2", Work: protocol.Work{Goal: "g"}, WorkspaceBindingKey: "local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveClaimGrant(second, protocol.ClaimResponse{RunID: second.RunID, Generation: second.Generation, ClaimID: "claim-2", LeaseToken: "lease-2", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetLocalState(second, "waiting_for_input"); err != nil {
		t.Fatal(err)
	}
	prepareTerminal := func(key state.RunKey, pid int, identity string) {
		t.Helper()
		if _, err := store.SetProcessDetails(key, pid, identity, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{TransitionID: "failed-" + key.RunID, State: "failed", Payload: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.ResolveTerminal(key, state.TerminalVerdictAccepted, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	prepareTerminal(first, 71, "test:71")
	prepareTerminal(second, 72, "test:72")
	attempts := make(map[int]int)
	now := time.Now().UTC()
	d := &daemon{
		store:   store,
		control: &fakeControl{},
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: make(map[state.RunKey]*runningRun),
		options: options{clock: func() time.Time { return now }, terminatePersist: func(pid int, identity string) error {
			attempts[pid]++
			if pid == 71 && attempts[pid] < 3 {
				return errors.New("stop is unproven")
			}
			if identity != fmt.Sprintf("test:%d", pid) {
				t.Fatalf("recovered identity = %q for pid %d", identity, pid)
			}
			return nil
		}},
	}
	if !d.reconcile(context.Background()) {
		t.Fatal("unproven local marker prevented Control reconciliation")
	}
	d.enqueueRecoveredCleanups()
	d.flushCleanups(context.Background())
	firstJournal, err := store.LoadJournal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJournal, err := store.LoadJournal(second)
	if err != nil {
		t.Fatal(err)
	}
	if attempts[71] != 1 || attempts[72] != 1 || !firstJournal.HasProcessDetails() || secondJournal.HasProcessDetails() {
		t.Fatalf("recovery attempts = %#v, first=%#v, second=%#v", attempts, firstJournal, secondJournal)
	}
	d.enqueueRecoveredCleanups()
	d.flushCleanups(context.Background())
	if attempts[71] != 1 || attempts[72] != 1 {
		t.Fatalf("repeated discovery reset cleanup backoff: %#v", attempts)
	}
	for range 2 {
		now = now.Add(cleanupRetryMinimum)
		d.flushCleanups(context.Background())
	}
	firstJournal, err = store.LoadJournal(first)
	if err != nil || firstJournal.HasProcessDetails() || firstJournal.TerminalVerdict != state.TerminalVerdictAccepted || attempts[71] != 3 || attempts[72] != 1 {
		t.Fatalf("cleanup did not retry independently of reconnect: attempts=%#v marker=%t error=%v", attempts, firstJournal.HasProcessDetails(), err)
	}
}

func TestNewProcessStopEvidenceReleasesCleanupBackoff(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetProcessDetails(key, 42, "test:42", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{TransitionID: "failed", State: "failed", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	process := fakeProcess{}
	active := &runningRun{process: process, cleanupBlocked: true}
	now := time.Now().UTC()
	d := &daemon{
		store: store, running: map[state.RunKey]*runningRun{key: active},
		options: options{clock: func() time.Time { return now }},
	}
	d.enqueueCleanup(key)
	d.flushCleanups(context.Background())
	if d.cleanupWait() != cleanupRetryMinimum {
		t.Fatal("live process did not defer cleanup")
	}
	_ = process.Wait()
	if !d.recordGenericProcessExit(key, active, process, 42, "test:42", true) {
		t.Fatal("original process lost its active owner")
	}
	d.enqueueCleanup(key)
	d.flushCleanups(context.Background())
	journal, err := store.LoadJournal(key)
	if err != nil || journal.HasProcessDetails() || active.cleanupBlocked {
		t.Fatalf("fresh stop proof remained behind cleanup backoff: marker=%t blocked=%t error=%v", journal.HasProcessDetails(), active.cleanupBlocked, err)
	}
}

func TestGenericProcessMarkerClearFailureDoesNotBlockTerminalDelivery(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	persistWorkspacePath(t, store, key, "C:\\workspace")
	if _, err := store.SetProcessDetails(key, 42, "test:42", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	clearStarted := make(chan struct{})
	var clearOnce sync.Once
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	d := &daemon{
		config:  config.Config{CleanupTimeoutMS: 10000},
		store:   store,
		control: &fakeControl{},
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{key: {
			process:        fakeProcess{result: execution.Result{}},
			cleanupBlocked: true,
			slotHeld:       true,
		}},
		slots: slots,
		options: options{
			newID: ids(),
			clearProcessDetails: func(state.RunKey, int, string) (state.RunJournal, error) {
				clearOnce.Do(func() { close(clearStarted) })
				return state.RunJournal{}, errors.New("durable clear failed")
			},
		},
	}
	d.waitForRunWithContext(context.Background(), key)
	d.flushCleanups(context.Background())
	<-clearStarted
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "terminal_pending" || journal.TerminalVerdict != "" || !journal.HasProcessDetails() {
		t.Fatalf("journal before terminal delivery = %#v", journal)
	}
	if err := d.flushRun(context.Background(), journal); err != nil {
		t.Fatal(err)
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "terminal_pending" || journal.TerminalVerdict != state.TerminalVerdictAccepted || !journal.HasProcessDetails() || len(slots) != 0 {
		t.Fatalf("clear failure blocked terminal delivery: journal=%#v slots=%d", journal, len(slots))
	}
}

func TestGenericProcessMarkerClearReadbackReleasesSameActiveOwner(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetProcessDetails(key, 42, "test:42", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	active := &runningRun{process: fakeProcess{result: execution.Result{}}, cleanupBlocked: true}
	d := &daemon{
		store:   store,
		control: &fakeControl{},
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{key: active},
		options: options{
			newID: ids(),
			clearProcessDetails: func(key state.RunKey, pid int, identity string) (state.RunJournal, error) {
				if _, err := store.ClearProcessDetails(key, pid, identity); err != nil {
					return state.RunJournal{}, err
				}
				return state.RunJournal{}, errors.New("rename response was lost")
			},
		},
	}
	d.waitForRunWithContext(context.Background(), key)
	d.flushCleanups(context.Background())
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.HasProcessDetails() || active.cleanupBlocked {
		t.Fatalf("clear readback did not release the original owner: journal=%#v active=%#v", journal, active)
	}
}

func TestConclusiveTerminalWithProcessMarkerStillFlushesGoalUsage(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	usage := protocol.Usage{
		SchemaVersion: protocol.UsageSchemaVersion,
		UsageID:       "00000000-0000-4000-8000-000000000010",
		RunID:         key.RunID,
		UsageKey:      "terminal-usage",
		Provider:      "openai",
		Model:         "gpt-test",
		CostBasis:     protocol.CostUnknown,
		ObservedAt:    "2026-09-09T01:00:00Z",
	}
	if _, err := store.QueueGoalUsage(key, usage); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetProcessDetails(key, 71, "test:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{TransitionID: "failed", State: "failed", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveTerminal(key, state.TerminalVerdictOwnershipLost, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	client := &goalDeliveryControl{fakeControl: &fakeControl{}}
	d := &daemon{config: testConfig(t), store: store, control: client, workspace: &fakeWorkspace{}, options: options{clock: time.Now}}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.flushRun(context.Background(), journal); err != nil {
		t.Fatal(err)
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "terminal_pending" || !journal.HasProcessDetails() || journal.HasPendingGoalDeliveries() || len(client.usages) != 1 || client.usages[0].UsageID != usage.UsageID {
		t.Fatalf("conclusive terminal did not flush Goal usage: journal=%#v usages=%#v", journal, client.usages)
	}
}

func TestOldGenericProcessWaiterDoesNotQueueTerminalEffects(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetProcessDetails(key, 42, "test:42", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	process := newStartFailureProcess()
	active := &runningRun{process: process, cleanupBlocked: true}
	d := &daemon{
		store:   store,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{key: active},
	}
	done := make(chan struct{})
	go func() {
		d.waitForRunWithContext(context.Background(), key)
		close(done)
	}()
	<-process.waitStarted
	d.mu.Lock()
	d.running[key] = &runningRun{process: fakeProcess{}}
	d.mu.Unlock()
	close(process.releaseWait)
	<-done
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "" || len(journal.PendingTransitions) != 0 || !journal.HasProcessDetails() {
		t.Fatalf("old waiter changed current journal: %#v", journal)
	}
}

func TestPartialNativeStartSessionRetainsOwnerUntilCloseProof(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	store, key := claimedStore(t)
	defer store.Close()
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.SetProcessDetails(key, 71, "native:71", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("partial native start failed")
	session := &closeFailureGoalSession{err: failure}
	active := &runningRun{}
	d := &daemon{
		store:   store,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{key: active},
	}
	d.attachPartialNativeSession(key, session, sessionKey, admission, workspace.Prepared{Path: "C:\\workspace", BindingKey: "local"})
	if active.nativeSession != session || active.goalSession == nil || *active.goalSession != sessionKey || active.goalAdmission == nil || active.prepared.Path != "C:\\workspace" || !active.cleanupBlocked {
		t.Fatalf("partial native owner was not retained: %#v", active)
	}
	if err := d.abandonGoalSession(sessionKey, session, false, failure); !errors.Is(err, failure) {
		t.Fatalf("abandonGoalSession() error = %v, want %v", err, failure)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.HasProcessDetails() || !active.cleanupBlocked || active.nativeSession != session {
		t.Fatalf("unresolved partial native owner was discarded: journal=%#v active=%#v", journal, active)
	}
	session.err = nil
	if err := d.closeNativeGoalSession(key, active, session); err != nil {
		t.Fatalf("closeNativeGoalSession() after proof error = %v", err)
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.HasProcessDetails() {
		t.Fatalf("proven partial native close did not clear exact marker: %#v", journal)
	}
}
