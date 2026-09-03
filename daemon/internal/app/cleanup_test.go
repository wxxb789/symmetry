package app

import (
	"context"
	"encoding/json"
	"errors"
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

func TestCleanupPendingRetriesAfterThirtySecondsAndRestartsImmediately(t *testing.T) {
	t.Run("running daemon waits thirty seconds", func(t *testing.T) {
		store, key := cleanupPendingStore(t)
		defer store.Close()
		now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
		cleaner := &retryCleanupWorkspace{remainingFailures: 1}
		daemon := &daemon{
			config:      config.Config{CleanupTimeoutMS: 10000},
			store:       store,
			workspace:   cleaner,
			log:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
			options:     options{clock: func() time.Time { return now }},
			outboxRetry: make(map[state.RunKey]outboxRetry),
		}
		daemon.enqueueCleanup(key)
		daemon.flushCleanups(context.Background())
		if len(cleaner.outcomes) != 1 {
			t.Fatalf("cleanup attempts = %d, want 1", len(cleaner.outcomes))
		}
		now = now.Add(29 * time.Second)
		daemon.flushCleanups(context.Background())
		if len(cleaner.outcomes) != 1 {
			t.Fatalf("cleanup attempts at 29s = %d, want 1", len(cleaner.outcomes))
		}
		now = now.Add(time.Second)
		daemon.flushCleanups(context.Background())
		if len(cleaner.outcomes) != 2 {
			t.Fatalf("cleanup attempts at 30s = %d, want 2", len(cleaner.outcomes))
		}
		if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
			t.Fatalf("journal = %v, want deleted after successful retry", err)
		}
	})

	t.Run("restart retries once without waiting", func(t *testing.T) {
		store, key := cleanupPendingStore(t)
		defer store.Close()
		now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
		cleaner := &retryCleanupWorkspace{remainingFailures: 1}
		first := &daemon{
			config:      config.Config{CleanupTimeoutMS: 10000},
			store:       store,
			workspace:   cleaner,
			log:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
			options:     options{clock: func() time.Time { return now }},
			outboxRetry: make(map[state.RunKey]outboxRetry),
		}
		first.enqueueCleanup(key)
		first.flushCleanups(context.Background())
		restarted := &daemon{
			config:      config.Config{CleanupTimeoutMS: 10000},
			store:       store,
			workspace:   cleaner,
			log:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
			options:     options{clock: func() time.Time { return now }},
			outboxRetry: make(map[state.RunKey]outboxRetry),
		}
		restarted.enqueueCleanup(key)
		restarted.flushCleanups(context.Background())
		if len(cleaner.outcomes) != 2 {
			t.Fatalf("cleanup attempts after restart = %d, want 2", len(cleaner.outcomes))
		}
		if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
			t.Fatalf("journal = %v, want deleted after restart retry", err)
		}
	})
}

func TestCleanupPendingUsesBoundedShutdownCancellableContext(t *testing.T) {
	store, key := cleanupPendingStore(t)
	defer store.Close()
	background, cancel := context.WithCancel(context.Background())
	cleaner := newBlockingCleanupWorkspace()
	daemon := &daemon{
		config:     config.Config{CleanupTimeoutMS: 10000},
		store:      store,
		workspace:  cleaner,
		log:        slog.New(slog.NewJSONHandler(io.Discard, nil)),
		background: background,
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- daemon.cleanupPending(context.Background(), journal) }()
	<-cleaner.entered
	if !cleaner.hasDeadline {
		t.Fatal("cleanup context had no deadline")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cleanup error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel active cleanup")
	}
	if _, err := store.LoadJournal(key); err != nil {
		t.Fatalf("journal was removed after cancelled cleanup: %v", err)
	}
}

func TestBackgroundStopCancelsAndJoinsCleanupWorker(t *testing.T) {
	store, _ := cleanupPendingStore(t)
	defer store.Close()
	cleaner := newBlockingCleanupWorkspace()
	daemon := &daemon{
		config:    config.Config{CleanupTimeoutMS: 10000},
		store:     store,
		workspace: cleaner,
		control:   &fakeControl{},
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := daemon.startBackground(ctx)
	<-cleaner.entered
	stopped := make(chan struct{})
	go func() { done(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("background stop did not join active cleanup")
	}
}

func TestBlockedCleanupDoesNotBlockOutboxDelivery(t *testing.T) {
	store, key := cleanupPendingStore(t)
	defer store.Close()
	other := state.RunKey{RunID: "run-2", Generation: 1}
	if _, err := store.SaveClaimIntent(state.ClaimIntent{Key: other, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1, ClaimID: "claim-2", Work: protocol.Work{Goal: "g"}, WorkspaceBindingKey: "local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveClaimGrant(other, protocol.ClaimResponse{RunID: other.RunID, Generation: other.Generation, ClaimID: "claim-2", LeaseToken: "lease-2", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueEvent(other, protocol.RunEvent{EventID: "event-2", Sequence: 1, Kind: "progress", OccurredAt: time.Now(), Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cleaner := newBlockingCleanupWorkspace()
	control := &eventDeliveryControl{events: make(chan struct{}, 1)}
	daemon := &daemon{
		config:      config.Config{CleanupTimeoutMS: 10000},
		store:       store,
		control:     control,
		workspace:   cleaner,
		log:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		background:  ctx,
		cleanupWake: make(chan struct{}, 1),
		outboxRetry: make(map[state.RunKey]outboxRetry),
	}
	daemon.enqueueCleanup(key)
	cleanupDone := make(chan struct{})
	go func() { daemon.runCleanup(ctx); close(cleanupDone) }()
	<-cleaner.entered
	done := make(chan struct{})
	go func() { daemon.flushAll(ctx); close(done) }()
	select {
	case <-time.After(time.Second):
		t.Fatal("blocked cleanup prevented an unrelated outbox event from being delivered")
	case <-control.events:
	}
	cancel()
	<-done
	<-cleanupDone
	if _, err := store.LoadJournal(key); err != nil {
		t.Fatalf("cleanup journal was removed after cancellation: %v", err)
	}
}

func TestRecoveredCleanupPendingOnlyCleansWithoutControlOrSlotReacquisition(t *testing.T) {
	store, key := cleanupPendingStore(t)
	defer store.Close()
	control := &reconcileCaptureControl{}
	cleaned := make(chan bool, 1)
	slots := make(chan struct{}, 1)
	daemon := &daemon{
		config:      config.Config{CleanupTimeoutMS: 10000},
		store:       store,
		control:     control,
		workspace:   &trackingWorkspace{cleaned: cleaned},
		log:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options:     options{clock: time.Now},
		running:     make(map[state.RunKey]*runningRun),
		slots:       slots,
		outboxRetry: make(map[state.RunKey]outboxRetry),
	}
	daemon.reconcile(context.Background())
	daemon.renewLeases(context.Background())
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "must not reclaim"}})
	if len(control.request.Runs) != 0 || control.renewCalls != 0 || len(slots) != 0 {
		t.Fatalf("cleanup-pending journal used active control or capacity: reconcile=%#v renew=%d slots=%d", control.request.Runs, control.renewCalls, len(slots))
	}
	daemon.flushAll(context.Background())
	select {
	case <-cleaned:
	case <-time.After(time.Second):
		t.Fatal("recovered cleanup-pending journal was not cleaned")
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("journal = %v, want deleted after cleanup", err)
	}
}

func TestRecoveredCleanupWithEmptyWorkspacePathUsesRecoveryIntent(t *testing.T) {
	store, key := cleanupPendingStore(t)
	defer store.Close()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	journal.WorkspacePath = ""
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkWorkspaceRecoveryRequired(key); err != nil {
		t.Fatal(err)
	}
	cleaner := &emptyPathRecoveryWorkspace{}
	restarted := &daemon{
		config:    config.Config{CleanupTimeoutMS: 10000},
		store:     store,
		workspace: cleaner,
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
	}
	restarted.enqueueCleanup(key)
	restarted.flushCleanups(context.Background())
	if cleaner.recoveredPath != "" || cleaner.recoveredKey != "local" || cleaner.recoveredRun != (workspace.RunRef{RunID: key.RunID, Generation: key.Generation}) {
		t.Fatalf("empty-path recovery = key=%q run=%#v path=%q", cleaner.recoveredKey, cleaner.recoveredRun, cleaner.recoveredPath)
	}
	if !cleaner.cleaned || !cleaner.succeeded {
		t.Fatalf("cleanup state = cleaned=%v succeeded=%v", cleaner.cleaned, cleaner.succeeded)
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("journal = %v, want deleted after empty-path recovery cleanup", err)
	}
}

func TestTerminalDeliveryWaitsForStartingWorkspacePersistence(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace := newCancelHeldPrepareWorkspace()
	control := &orderingControl{}
	daemon := startupTestDaemon(t, store, workspace)
	daemon.control = control
	key := state.RunKey{RunID: "run-1", Generation: 1}
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "g"}})
	<-workspace.entered
	daemon.handleCommand(context.Background(), protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"})
	<-workspace.cancelled
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(context.Background(), journal); err != nil {
		t.Fatalf("flushRun() while workspace starts: %v", err)
	}
	if len(control.calls) != 0 || journal.WorkspacePath != "" {
		t.Fatalf("terminal delivery ran before workspace persistence: calls=%#v journal=%#v", control.calls, journal)
	}
	select {
	case <-workspace.cleaned:
		t.Fatal("workspace was cleaned before Prepare returned")
	default:
	}
	close(workspace.release)
	daemon.workers.Wait()
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.WorkspacePath == "" {
		t.Fatal("Prepare result was not persisted before terminal delivery resumed")
	}
	if err := daemon.flushRun(context.Background(), journal); err != nil {
		t.Fatalf("flushRun() after workspace persistence: %v", err)
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("journal = %v, want deleted after terminal cleanup", err)
	}
	if got, want := control.calls, []string{"transition:cancelled", "ack:cancel-1"}; !sameStrings(got, want) {
		t.Fatalf("terminal delivery calls = %#v, want %#v", got, want)
	}
	select {
	case succeeded := <-workspace.cleaned:
		if succeeded {
			t.Fatal("cancelled workspace cleanup used success policy")
		}
	default:
		t.Fatal("workspace was not cleaned after terminal delivery")
	}
}

func TestResolvedRejectedTerminalSelfHealsIntoCleanupPending(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	persistWorkspacePath(t, store, key, "C:\\workspace")
	pendingAt := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "failed-1", State: "failed", Payload: json.RawMessage(`{}`)}, pendingAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveTerminal(key, state.TerminalVerdictOwnershipLost, pendingAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	daemon := &daemon{store: store, workspace: failingCleanupWorkspace{}, log: slog.New(slog.NewJSONHandler(io.Discard, nil)), options: options{clock: func() time.Time { return pendingAt.Add(2 * time.Second) }}}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(context.Background(), journal); err == nil {
		t.Fatal("flushRun() did not attempt recovered terminal cleanup")
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "cleanup_pending" || journal.TerminalVerdict != state.TerminalVerdictOwnershipLost || len(journal.PendingTransitions) != 0 {
		t.Fatalf("resolved terminal journal was not self-healed: %#v", journal)
	}
}

func TestTerminalDeliveryWaitsForWorkspacePathPersistenceRetry(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root, stop := context.WithCancel(context.Background())
	defer stop()
	workspace := newCancelHeldPrepareWorkspace()
	control := &orderingControl{}
	timers := make(chan *manualDeadlineTimer, 1)
	persistFailed := make(chan struct{}, 1)
	failFirst := true
	daemon := startupTestDaemon(t, store, workspace)
	daemon.control = control
	daemon.background = root
	daemon.options.recordWorkspace = func(key state.RunKey, path string) (state.RunJournal, error) {
		if failFirst {
			failFirst = false
			persistFailed <- struct{}{}
			return state.RunJournal{}, errors.New("injected workspace persistence failure")
		}
		return store.SetWorkspacePath(key, path)
	}
	daemon.options.newTimer = func(delay time.Duration) deadlineTimer {
		timer := &manualDeadlineTimer{channel: make(chan time.Time), delay: delay}
		if delay == minimumInterval {
			timers <- timer
		}
		return timer
	}
	key := state.RunKey{RunID: "run-1", Generation: 1}
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "g"}})
	<-workspace.entered
	daemon.handleCommand(context.Background(), protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"})
	<-workspace.cancelled
	close(workspace.release)
	<-persistFailed
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(context.Background(), journal); err != nil {
		t.Fatalf("flushRun() during workspace persistence retry: %v", err)
	}
	if len(control.calls) != 0 || journal.WorkspacePath != "" {
		t.Fatalf("terminal delivery escaped workspace persistence gate: calls=%#v journal=%#v", control.calls, journal)
	}
	select {
	case <-workspace.cleaned:
		t.Fatal("workspace was cleaned before its path was persisted")
	default:
	}
	(<-timers).channel <- time.Now()
	daemon.workers.Wait()
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.WorkspacePath == "" {
		t.Fatal("workspace path was not persisted after retry")
	}
	if err := daemon.flushRun(context.Background(), journal); err != nil {
		t.Fatalf("flushRun() after workspace persistence retry: %v", err)
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("journal = %v, want deleted after cleanup", err)
	}
}

func TestReturnedProcessBlocksCleanupUntilWaitCompletes(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cleaned := make(chan bool, 1)
	workspace := &trackingWorkspace{cleaned: cleaned}
	control := &orderingControl{}
	process := newCancelReturnedProcess()
	daemon := startupTestDaemon(t, store, workspace)
	daemon.control = control
	daemon.start = process.Start
	key := state.RunKey{RunID: "run-1", Generation: 1}
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "g"}})
	<-process.started
	daemon.handleCommand(context.Background(), protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"})
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(context.Background(), journal); err != nil {
		t.Fatalf("flushRun() before process attachment: %v", err)
	}
	if len(control.calls) != 0 {
		t.Fatalf("terminal delivery ran before process attachment: %#v", control.calls)
	}
	close(process.returnRelease)
	select {
	case <-process.terminated:
	case <-time.After(time.Second):
		t.Fatal("returned process was not terminated after cancellation")
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(context.Background(), journal); err != nil {
		t.Fatalf("flushRun() after process attachment: %v", err)
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "cleanup_pending" {
		t.Fatalf("journal state = %q, want cleanup_pending", journal.LocalState)
	}
	if len(daemon.slots) != 0 {
		t.Fatal("terminal acceptance did not release the slot while process exit was pending")
	}
	select {
	case <-cleaned:
		t.Fatal("workspace was cleaned before process exit")
	default:
	}
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "must not overlap"}})
	if process.starts != 1 {
		t.Fatalf("same-key assignment started %d processes, want 1", process.starts)
	}
	close(process.waitRelease)
	daemon.workers.Wait()
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("journal = %v, want deleted after process exit cleanup", err)
	}
	select {
	case succeeded := <-cleaned:
		if succeeded {
			t.Fatal("cancelled process cleanup used success policy")
		}
	default:
		t.Fatal("workspace was not cleaned after process exit")
	}
	daemon.flushCleanups(context.Background())
	select {
	case <-cleaned:
		t.Fatal("cleanup ran more than once after process exit")
	default:
	}
}

func TestProcessExitBeforeTerminalDeliveryReleasesCleanupOnce(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	persistWorkspacePath(t, store, key, "C:\\workspace")
	cleaned := make(chan bool, 1)
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	daemon := &daemon{
		store:     store,
		control:   &orderingControl{},
		workspace: &trackingWorkspace{cleaned: cleaned},
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options:   options{newID: ids()},
		running: map[state.RunKey]*runningRun{key: {
			process:        fakeProcess{result: execution.Result{ExitCode: 0}},
			cleanupBlocked: true,
			slotHeld:       true,
		}},
		slots: slots,
	}
	daemon.waitForRun(key)
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "terminal_pending" {
		t.Fatalf("journal state after process exit = %q, want terminal_pending", journal.LocalState)
	}
	if err := daemon.flushRun(context.Background(), journal); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("journal = %v, want deleted after terminal delivery", err)
	}
	select {
	case succeeded := <-cleaned:
		if !succeeded {
			t.Fatal("successful process cleanup used failure policy")
		}
	default:
		t.Fatal("workspace was not cleaned after terminal delivery")
	}
	daemon.flushCleanups(context.Background())
	select {
	case <-cleaned:
		t.Fatal("cleanup ran more than once after terminal delivery")
	default:
	}
}

func TestAttachedProcessFailureWaitsBeforeCleanup(t *testing.T) {
	for _, test := range []struct {
		name          string
		process       *startupFailureProcess
		recordProcess func(state.RunKey, int, string, time.Time) (state.RunJournal, error)
	}{
		{
			name:    "process details",
			process: newStartupFailureProcess(0, ""),
		},
		{
			name:    "record process",
			process: newStartupFailureProcess(42, "test:42"),
			recordProcess: func(state.RunKey, int, string, time.Time) (state.RunJournal, error) {
				return state.RunJournal{}, errors.New("injected process persistence failure")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := state.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			cleaned := make(chan bool, 1)
			daemon := startupTestDaemon(t, store, &trackingWorkspace{cleaned: cleaned})
			daemon.control = &orderingControl{}
			daemon.start = test.process.Start
			daemon.options.recordProcess = test.recordProcess
			key := state.RunKey{RunID: "run-1", Generation: 1}
			daemon.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "g"}})
			select {
			case <-test.process.terminated:
			case <-time.After(time.Second):
				t.Fatal("startup failure did not terminate returned process")
			}
			journal, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if err := daemon.flushRun(context.Background(), journal); err != nil {
				t.Fatalf("flushRun() error = %v", err)
			}
			journal, err = store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if journal.LocalState != "cleanup_pending" {
				t.Fatalf("journal state = %q, want cleanup_pending", journal.LocalState)
			}
			select {
			case <-cleaned:
				t.Fatal("workspace was cleaned before returned process exited")
			default:
			}
			close(test.process.waitRelease)
			daemon.workers.Wait()
			if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
				t.Fatalf("journal = %v, want deleted after returned process exit", err)
			}
			select {
			case <-cleaned:
			case <-time.After(time.Second):
				t.Fatal("workspace was not cleaned after returned process exit")
			}
		})
	}
}

func cleanupPendingStore(t *testing.T) (*state.Store, state.RunKey) {
	t.Helper()
	store, key := claimedStore(t)
	persistWorkspacePath(t, store, key, "C:\\workspace")
	pendingAt := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}, pendingAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveTerminal(key, state.TerminalVerdictAccepted, pendingAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkTransitionsDelivered(key, []string{"completed-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnterCleanupPending(key); err != nil {
		t.Fatal(err)
	}
	return store, key
}

type eventDeliveryControl struct {
	fakeControl
	events chan struct{}
}

func (control *eventDeliveryControl) AppendEvents(context.Context, string, protocol.AppendEventsRequest) error {
	control.events <- struct{}{}
	return nil
}

type blockingCleanupWorkspace struct {
	entered     chan struct{}
	hasDeadline bool
}

type cancelHeldPrepareWorkspace struct {
	entered   chan struct{}
	cancelled chan struct{}
	release   chan struct{}
	cleaned   chan bool
}

type cancelReturnedProcess struct {
	started       chan struct{}
	terminated    chan struct{}
	returnRelease chan struct{}
	waitRelease   chan struct{}
	terminate     sync.Once
	starts        int
}

type startupFailureProcess struct {
	pid         int
	identity    string
	terminated  chan struct{}
	waitRelease chan struct{}
	terminate   sync.Once
}

func newStartupFailureProcess(pid int, identity string) *startupFailureProcess {
	return &startupFailureProcess{pid: pid, identity: identity, terminated: make(chan struct{}, 1), waitRelease: make(chan struct{})}
}

func (process *startupFailureProcess) Start(context.Context, execution.Invocation, execution.Sink) (Process, error) {
	return process, nil
}

func (*startupFailureProcess) WriteInput([]byte) error { return nil }

func (process *startupFailureProcess) Terminate(context.Context, time.Duration) error {
	process.terminate.Do(func() { process.terminated <- struct{}{} })
	return nil
}

func (process *startupFailureProcess) Wait() execution.Result {
	<-process.waitRelease
	return execution.Result{ExitCode: 1}
}

func (process *startupFailureProcess) ProcessDetails() (int, string) {
	return process.pid, process.identity
}

func newCancelReturnedProcess() *cancelReturnedProcess {
	return &cancelReturnedProcess{started: make(chan struct{}, 1), terminated: make(chan struct{}, 1), returnRelease: make(chan struct{}), waitRelease: make(chan struct{})}
}

func (process *cancelReturnedProcess) Start(ctx context.Context, _ execution.Invocation, _ execution.Sink) (Process, error) {
	process.starts++
	process.started <- struct{}{}
	<-ctx.Done()
	<-process.returnRelease
	return process, nil
}

func (*cancelReturnedProcess) WriteInput([]byte) error { return nil }

func (process *cancelReturnedProcess) Terminate(context.Context, time.Duration) error {
	process.terminate.Do(func() { process.terminated <- struct{}{} })
	return nil
}

func (process *cancelReturnedProcess) Wait() execution.Result {
	<-process.waitRelease
	return execution.Result{ExitCode: 1}
}

func (*cancelReturnedProcess) ProcessDetails() (int, string) { return 42, "test:42" }

func newCancelHeldPrepareWorkspace() *cancelHeldPrepareWorkspace {
	return &cancelHeldPrepareWorkspace{entered: make(chan struct{}, 1), cancelled: make(chan struct{}, 1), release: make(chan struct{}), cleaned: make(chan bool, 1)}
}

func (service *cancelHeldPrepareWorkspace) Prepare(ctx context.Context, key string, run workspace.RunRef) (workspace.Prepared, error) {
	service.entered <- struct{}{}
	<-ctx.Done()
	service.cancelled <- struct{}{}
	<-service.release
	return workspace.Prepared{Path: "C:\\workspace", BindingKey: key, Run: run}, nil
}

func (*cancelHeldPrepareWorkspace) Recover(_ context.Context, key string, run workspace.RunRef, path string) (workspace.Prepared, error) {
	return workspace.Prepared{Path: path, BindingKey: key, Run: run}, nil
}

func (service *cancelHeldPrepareWorkspace) Cleanup(_ context.Context, _ workspace.Prepared, succeeded bool) error {
	service.cleaned <- succeeded
	return nil
}

func newBlockingCleanupWorkspace() *blockingCleanupWorkspace {
	return &blockingCleanupWorkspace{entered: make(chan struct{}, 1)}
}

func (*blockingCleanupWorkspace) Prepare(context.Context, string, workspace.RunRef) (workspace.Prepared, error) {
	return workspace.Prepared{}, errors.New("unexpected prepare")
}

func (*blockingCleanupWorkspace) Recover(_ context.Context, key string, run workspace.RunRef, path string) (workspace.Prepared, error) {
	return workspace.Prepared{Path: path, BindingKey: key, Run: run}, nil
}

func (service *blockingCleanupWorkspace) Cleanup(ctx context.Context, _ workspace.Prepared, _ bool) error {
	_, service.hasDeadline = ctx.Deadline()
	service.entered <- struct{}{}
	<-ctx.Done()
	return ctx.Err()
}

type emptyPathRecoveryWorkspace struct {
	recoveredKey  string
	recoveredRun  workspace.RunRef
	recoveredPath string
	cleaned       bool
	succeeded     bool
}

func (*emptyPathRecoveryWorkspace) Prepare(context.Context, string, workspace.RunRef) (workspace.Prepared, error) {
	return workspace.Prepared{}, errors.New("unexpected prepare")
}

func (service *emptyPathRecoveryWorkspace) Recover(_ context.Context, key string, run workspace.RunRef, path string) (workspace.Prepared, error) {
	service.recoveredKey = key
	service.recoveredRun = run
	service.recoveredPath = path
	return workspace.Prepared{Path: path, BindingKey: key, Run: run}, nil
}

func (service *emptyPathRecoveryWorkspace) Cleanup(_ context.Context, _ workspace.Prepared, succeeded bool) error {
	service.cleaned = true
	service.succeeded = succeeded
	return nil
}
