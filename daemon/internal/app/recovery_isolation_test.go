package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestRecoverUnresolvedInputIntentsProcessesUnrelatedJournalAfterUnprovenStop(t *testing.T) {
	store, first := claimedStore(t)
	defer store.Close()

	second := state.RunKey{RunID: "run-2", Generation: 1}
	if _, err := store.SaveClaimIntent(state.ClaimIntent{
		Key: second, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1,
		ClaimID: "claim-2", Work: protocol.Work{Goal: "g"}, WorkspaceBindingKey: "local",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveClaimGrant(second, protocol.ClaimResponse{
		RunID: second.RunID, Generation: second.Generation, ClaimID: "claim-2", LeaseToken: "lease-2",
		LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetLocalState(second, "waiting_for_input"); err != nil {
		t.Fatal(err)
	}

	payload := json.RawMessage(`{"answer":"yes"}`)
	digest, err := canonicalInputDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	prepareInput := func(key state.RunKey) state.InputCommandIntent {
		t.Helper()
		intent := state.InputCommandIntent{
			CommandID:           "input-" + key.RunID,
			PayloadDigest:       digest,
			RunningTransitionID: "running-" + key.RunID,
			AckID:               "ack-" + key.RunID,
		}
		if _, created, prepareErr := store.PrepareProvideInput(key, intent); prepareErr != nil || !created {
			t.Fatalf("PrepareProvideInput(%s) created=%t error=%v", key.RunID, created, prepareErr)
		}
		return intent
	}
	firstIntent := prepareInput(first)
	secondIntent := prepareInput(second)
	if _, err := store.SetProcessDetails(first, 71, "input:first", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetProcessDetails(second, 72, "input:second", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	stopCalls := make(map[int]int)
	app := &daemon{
		store:   store,
		running: make(map[state.RunKey]*runningRun),
		options: options{newID: ids(), clock: time.Now, terminatePersist: func(pid int, identity string) error {
			stopCalls[pid]++
			switch pid {
			case 71:
				if identity != "input:first" {
					t.Fatalf("first recovered process identity = %q", identity)
				}
				return errPersistedProcessStopUnproven
			case 72:
				if identity != "input:second" {
					t.Fatalf("second recovered process identity = %q", identity)
				}
				return nil
			default:
				t.Fatalf("unexpected recovered process pid = %d", pid)
				return nil
			}
		}},
	}

	// The recovery result may carry a pending aggregate; the isolation contract
	// is established by the independent durable effects for each Run.
	_ = app.recoverUnresolvedInputIntents(context.Background())

	firstJournal, err := store.LoadJournal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJournal, err := store.LoadJournal(second)
	if err != nil {
		t.Fatal(err)
	}
	if stopCalls[71] != 1 || stopCalls[72] != 1 {
		t.Fatalf("recovery stop calls = %#v, want one attempt per Run", stopCalls)
	}
	if firstJournal.PID != 71 || firstJournal.ProcessIdentity != "input:first" || firstJournal.StartedAt.IsZero() {
		t.Fatalf("unproven first marker was cleared or replaced: %#v", firstJournal)
	}
	if firstJournal.LocalState != "terminal_pending" || firstJournal.TerminalState != "failed" || !firstJournal.RetainWorkspace {
		t.Fatalf("unproven first input was not durably failed and retained: %#v", firstJournal)
	}
	if firstJournal.InputCommandIntent == nil || firstJournal.InputCommandIntent.CommandID != firstIntent.CommandID || firstJournal.InputCommandIntent.PayloadDigest != firstIntent.PayloadDigest || firstJournal.InputCommandIntent.Outcome != "failed" {
		t.Fatalf("unproven first input intent was corrupted: %#v", firstJournal.InputCommandIntent)
	}
	if len(firstJournal.PendingCommandAcknowledgements) != 1 || firstJournal.PendingCommandAcknowledgements[0].AckID != firstIntent.AckID || firstJournal.PendingCommandAcknowledgements[0].Outcome != "failed" {
		t.Fatalf("unproven first input receipt was corrupted: %#v", firstJournal.PendingCommandAcknowledgements)
	}
	if secondJournal.HasProcessDetails() {
		t.Fatalf("independent second process marker was retained: %#v", secondJournal)
	}
	if secondJournal.InputCommandIntent == nil || secondJournal.InputCommandIntent.CommandID != secondIntent.CommandID || secondJournal.InputCommandIntent.Outcome != "failed" {
		t.Fatalf("independent second input receipt was not recovered: %#v", secondJournal.InputCommandIntent)
	}
	if secondJournal.TerminalState != "failed" || len(secondJournal.PendingTransitions) != 1 || len(secondJournal.PendingCommandAcknowledgements) != 1 || secondJournal.PendingCommandAcknowledgements[0].AckID != secondIntent.AckID || secondJournal.PendingCommandAcknowledgements[0].Outcome != "failed" {
		t.Fatalf("independent second terminal receipt was not preserved: %#v", secondJournal)
	}
}

func TestRecoverUnclosedGoalSessionsProcessesUnrelatedSessionAfterUnprovenStop(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	secondAdmission := admission
	secondAdmission.AdmissionID = "00000000-0000-4000-8000-000000000013"
	secondAdmission.GoalID = "00000000-0000-4000-8000-000000000014"
	secondWorkItemID := "00000000-0000-4000-8000-000000000015"
	secondAdmission.WorkItemID = &secondWorkItemID
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	first := state.RunKey{RunID: "goal-run-1", Generation: 1}
	second := state.RunKey{RunID: "goal-run-2", Generation: 1}
	saveClaimedGoalRun(t, store, first)
	saveClaimedGoalRun(t, store, second)
	firstSession := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	secondSession := state.GoalSessionKey{GoalID: secondAdmission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000009"}
	saveAttachedGoalSession(t, store, first, admission, firstSession)
	saveAttachedGoalSession(t, store, second, secondAdmission, secondSession)
	if _, err := store.SetProcessDetails(first, 81, "native:first", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetProcessDetails(second, 82, "native:second", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	stopCalls := make(map[int]int)
	app := &daemon{
		config:  testConfig(t),
		store:   store,
		running: make(map[state.RunKey]*runningRun),
		slots:   make(chan struct{}, 2),
		options: options{newID: ids(), clock: time.Now, terminatePersist: func(pid int, identity string) error {
			stopCalls[pid]++
			switch pid {
			case 81:
				if identity != "native:first" {
					t.Fatalf("first recovered native identity = %q", identity)
				}
				return errPersistedProcessStopUnproven
			case 82:
				if identity != "native:second" {
					t.Fatalf("second recovered native identity = %q", identity)
				}
				return nil
			default:
				t.Fatalf("unexpected recovered native pid = %d", pid)
				return nil
			}
		}},
	}

	_ = app.recoverUnclosedGoalSessions(context.Background())

	firstJournal, err := store.LoadJournal(first)
	if err != nil {
		t.Fatal(err)
	}
	secondJournal, err := store.LoadJournal(second)
	if err != nil {
		t.Fatal(err)
	}
	firstRecovered, err := store.LoadGoalSession(firstSession)
	if err != nil {
		t.Fatal(err)
	}
	secondRecovered, err := store.LoadGoalSession(secondSession)
	if err != nil {
		t.Fatal(err)
	}
	if stopCalls[81] != 1 || stopCalls[82] != 1 {
		t.Fatalf("native recovery stop calls = %#v, want one attempt per Goal", stopCalls)
	}
	if firstJournal.PID != 81 || firstJournal.ProcessIdentity != "native:first" || firstJournal.StartedAt.IsZero() {
		t.Fatalf("unproven first native marker was cleared or replaced: %#v", firstJournal)
	}
	if !firstRecovered.NeedsReconciliation() {
		t.Fatalf("unproven first Goal session lost its recovery barrier: %#v", firstRecovered)
	}
	if secondJournal.HasProcessDetails() || secondJournal.TerminalState != "failed" || len(secondJournal.PendingTransitions) != 1 {
		t.Fatalf("independent second Goal recovery did not converge: %#v", secondJournal)
	}
	if !secondRecovered.IsUncertainLaunch() || secondRecovered.SessionState != state.GoalSessionStateUnavailable {
		t.Fatalf("independent second Goal session recovery = %#v", secondRecovered)
	}
}

func TestStaleGenericWaitSchedulesCleanupAfterStaleStateWriteFailure(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetProcessDetails(key, 42, "test:42", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	process := fakeProcess{result: execution.Result{}}
	active := &runningRun{process: process, cleanupBlocked: true, slotHeld: true, stale: true}
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	app := &daemon{
		store:       store,
		running:     map[state.RunKey]*runningRun{key: active},
		slots:       slots,
		cleanupWake: make(chan struct{}, 1),
		options:     options{clock: time.Now},
	}
	restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
		return errors.New("injected stale state persistence failure")
	})
	app.waitForRunWithContext(context.Background(), key)
	restore()

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(slots) != 0 || app.runningRun(key) != active || active.processStopWitness != process {
		t.Fatalf("stale Wait did not release capacity while retaining witness: slots=%d active=%#v", len(slots), active)
	}
	if active.processStopPID != 42 || active.processStopIdentity != "test:42" || !active.cleanupBlocked {
		t.Fatalf("stale Wait witness state = %#v", active)
	}
	if journal.LocalState != "waiting_for_input" || journal.PID != 42 || journal.ProcessIdentity != "test:42" || journal.StartedAt.IsZero() {
		t.Fatalf("stale state write failure lost process marker: %#v", journal)
	}
	if got := app.cleanupWait(); got != 0 {
		t.Fatalf("stale Wait cleanup retry delay = %s, want immediate queued retry", got)
	}

	app.flushCleanups(context.Background())
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("cleanup worker did not clear marker and retire stale journal: %v", err)
	}
}

func TestRunContinuesAfterTypedRecoveryPending(t *testing.T) {
	t.Run("input_registers_after_terminal_recovery", func(t *testing.T) {
		store, err := state.New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		if err := store.SaveIdentity(state.MachineIdentity{MachineID: "machine-1", MachineToken: "machine-token"}); err != nil {
			t.Fatal(err)
		}
		key := saveRecoveryPendingInputJournal(t, store)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		registered := make(chan inputRecoveryObservation, 1)
		control := &inputRecoveryControl{store: store, key: key, registered: registered, cancel: cancel}
		startCalls := make(chan struct{}, 1)
		stopCalls := 0
		done := make(chan error, 1)
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			done <- Run(ctx, testConfig(t), WithStore(store), WithControl(control), WithWorkspace(&fakeWorkspace{}), WithStartProcess(func(context.Context, execution.Invocation, execution.Sink) (Process, error) {
				startCalls <- struct{}{}
				return nil, errors.New("input recovery test must not start an agent")
			}), WithLogWriter(io.Discard), func(settings *options) {
				settings.newID = ids()
				settings.terminatePersist = func(int, string) error {
					stopCalls++
					return errPersistedProcessStopUnproven
				}
			})
		}()
		t.Cleanup(func() {
			cancel()
			select {
			case <-finished:
			case <-time.After(3 * time.Second):
				t.Errorf("Run goroutine did not stop during cleanup")
			}
		})

		var observation inputRecoveryObservation
		select {
		case observation = <-registered:
		case <-time.After(3 * time.Second):
			t.Fatal("Run did not register after durable input failure recovery")
		}
		if observation.err != nil {
			t.Fatalf("RegisterSession observation load error = %v", observation.err)
		}
		journal := observation.journal
		if journal.LocalState != "terminal_pending" || journal.TerminalState != "failed" || !journal.RetainWorkspace || journal.PID != 1<<30 || journal.ProcessIdentity != "recovery:unproven" {
			t.Fatalf("input recovery at registration = %#v", journal)
		}
		if journal.InputCommandIntent == nil || journal.InputCommandIntent.Outcome != "failed" || len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].Outcome != "failed" {
			t.Fatalf("input failure receipt at registration = %#v", journal)
		}
		if stopCalls != 1 {
			t.Fatalf("input physical stop attempts before registration = %d, want 1", stopCalls)
		}
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("Run did not stop after registration observation")
		}
		select {
		case <-startCalls:
			t.Fatal("input recovery replayed stdin or started an agent")
		default:
		}

		now := time.Now().UTC()
		cleanupAttempts := 0
		cleanup := &daemon{store: store, running: make(map[state.RunKey]*runningRun), options: options{
			clock: func() time.Time { return now },
			terminatePersist: func(int, string) error {
				cleanupAttempts++
				if cleanupAttempts == 1 {
					return errors.New("cleanup stop remains pending")
				}
				return nil
			},
		}}
		cleanup.enqueueRecoveredCleanups()
		cleanup.flushCleanups(context.Background())
		if cleanupAttempts != 1 || cleanup.cleanupWait() != cleanupRetryMinimum {
			t.Fatalf("input cleanup retry = attempts:%d wait:%s", cleanupAttempts, cleanup.cleanupWait())
		}
		now = now.Add(cleanupRetryMinimum)
		cleanup.flushCleanups(context.Background())
		if cleanupAttempts != 2 {
			t.Fatalf("input cleanup retry attempts = %d, want 2", cleanupAttempts)
		}
		final, err := store.LoadJournal(key)
		if err != nil || final.HasProcessDetails() {
			t.Fatalf("input marker after cleanup retry = %#v, error=%v", final, err)
		}
	})

	t.Run("native_goal_retries_after_reconcile_backoff", func(t *testing.T) {
		store, err := state.New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		if err := store.SaveIdentity(state.MachineIdentity{MachineID: "machine-1", MachineToken: "machine-token"}); err != nil {
			t.Fatal(err)
		}
		key := saveRecoveryPendingNativeJournal(t, store)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		timers := newRecordingTimerFactory()
		control := &recoveryRetryControl{
			registered: make(chan struct{}, 1), calls: make(chan int, 4), requests: make(chan protocol.ReconcileRequest, 4), cancel: cancel,
		}
		stopCalls := make(chan int, 8)
		done := make(chan error, 1)
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			done <- Run(ctx, testConfig(t), WithStore(store), WithControl(control), WithWorkspace(&fakeWorkspace{}), WithStartProcess(func(context.Context, execution.Invocation, execution.Sink) (Process, error) {
				return nil, errors.New("native recovery test must not start an agent")
			}), WithLogWriter(io.Discard), func(settings *options) {
				settings.newTimer = timers.new
				settings.newID = ids()
				settings.terminatePersist = func(pid int, _ string) error {
					stopCalls <- pid
					return errPersistedProcessStopUnproven
				}
			})
		}()
		t.Cleanup(func() {
			cancel()
			select {
			case <-finished:
			case <-time.After(3 * time.Second):
				t.Errorf("Run goroutine did not stop during cleanup")
			}
		})
		select {
		case <-control.registered:
		case <-time.After(3 * time.Second):
			t.Fatal("native recovery did not register")
		}
		awaitReconcileCall(t, control.calls, 1)
		request := <-control.requests
		if !reconcileRequestContainsRun(request, key) {
			t.Fatalf("native reconcile request = %#v", request)
		}
		for range 2 {
			select {
			case <-stopCalls:
			case <-time.After(3 * time.Second):
				t.Fatal("native recovery stop attempt did not occur before backoff")
			}
		}
		timers.fireUntil(t, minimumInterval, control.calls, 2)
		select {
		case <-stopCalls:
		case <-time.After(3 * time.Second):
			t.Fatal("native recovery did not retry after controlled backoff")
		}
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Run() error = %v", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("native Run did not stop")
		}
	})
}

func TestReconcilePendingRecoverySchedulesSnapshotBeforeBackoff(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveIdentity(state.MachineIdentity{MachineID: "machine-1", MachineToken: "machine-token"}); err != nil {
		t.Fatal(err)
	}
	pendingKey := saveRecoveryPendingNativeJournal(t, store)

	assignmentKey := state.RunKey{RunID: "run-assignment", Generation: 1}
	const commandID = "snapshot-cancel"
	control := &snapshotReconcileControl{
		runRetryReconcileControl: runRetryReconcileControl{
			calls: make(chan int, 2),
			response: protocol.ReconcileResponse{
				Assignments: []protocol.Assignment{{RunID: assignmentKey.RunID, Generation: assignmentKey.Generation, Work: protocol.Work{Goal: "snapshot assignment"}}},
				Commands:    []protocol.Command{{CommandID: commandID, RunID: pendingKey.RunID, Generation: pendingKey.Generation, Kind: "cancel"}},
			},
		},
		acknowledged: make(chan protocol.CommandAcknowledgement, 1),
	}
	timers := newRecordingTimerFactory()
	started := make(chan snapshotStartObservation, 1)
	start := func(ctx context.Context, _ execution.Invocation, _ execution.Sink) (Process, error) {
		select {
		case acknowledgement := <-control.acknowledged:
			started <- snapshotStartObservation{ack: acknowledgement}
		case <-ctx.Done():
			started <- snapshotStartObservation{err: ctx.Err()}
			return nil, ctx.Err()
		}
		return newRecoveryBlockingProcess(), nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		done <- Run(ctx, testConfig(t), WithStore(store), WithControl(control), WithWorkspace(&fakeWorkspace{}), WithStartProcess(start), WithLogWriter(io.Discard), func(settings *options) {
			settings.newTimer = timers.new
			settings.newID = ids()
		})
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(3 * time.Second):
			t.Errorf("Run goroutine did not stop during cleanup")
		}
	})

	awaitReconcileCall(t, control.calls, 1)
	timers.await(t, minimumInterval)
	var observation snapshotStartObservation
	select {
	case observation = <-started:
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("Control snapshot assignment was not started after pending recovery reconcile")
	}
	if observation.err != nil {
		cancel()
		t.Fatalf("snapshot assignment start journal load error = %v", observation.err)
	}
	if observation.ack.CommandID != commandID || observation.ack.Outcome != "rejected" {
		cancel()
		t.Fatalf("snapshot command was not durably processed before assignment start: %#v", observation.ack)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not stop after snapshot scheduling assertion")
	}
}

func saveRecoveryPendingInputJournal(t *testing.T, store *state.Store) state.RunKey {
	t.Helper()
	key := state.RunKey{RunID: "run-input-pending", Generation: 1}
	saveClaimedGoalRun(t, store, key)
	if _, err := store.SetLocalState(key, "waiting_for_input"); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"answer":"yes"}`)
	digest, err := canonicalInputDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, created, err := store.PrepareProvideInput(key, state.InputCommandIntent{
		CommandID: "input-pending", PayloadDigest: digest, RunningTransitionID: "running-pending", AckID: "ack-pending",
	}); err != nil || !created {
		t.Fatalf("PrepareProvideInput() created=%t error=%v", created, err)
	}
	if _, err := store.SetProcessDetails(key, 1<<30, "recovery:unproven", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return key
}

func saveRecoveryPendingNativeJournal(t *testing.T, store *state.Store) state.RunKey {
	t.Helper()
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	key := state.RunKey{RunID: "run-native-pending", Generation: 1}
	saveClaimedGoalRun(t, store, key)
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000017"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.SetProcessDetails(key, 1<<30, "recovery:unproven", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	return key
}

func reconcileRequestContainsRun(request protocol.ReconcileRequest, key state.RunKey) bool {
	for _, run := range request.Runs {
		if run.RunID == key.RunID && run.Generation == key.Generation {
			return true
		}
	}
	return false
}

type recoveryRetryControl struct {
	fakeControl
	registered     chan struct{}
	calls          chan int
	requests       chan protocol.ReconcileRequest
	cancel         context.CancelFunc
	cancelOnSecond bool
	registerOnce   sync.Once
	mutex          sync.Mutex
	count          int
}

type inputRecoveryObservation struct {
	journal state.RunJournal
	err     error
}

type inputRecoveryControl struct {
	fakeControl
	store      *state.Store
	key        state.RunKey
	registered chan<- inputRecoveryObservation
	cancel     context.CancelFunc
}

func (control *inputRecoveryControl) RegisterSession(context.Context, string, string, protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error) {
	journal, err := control.store.LoadJournal(control.key)
	control.registered <- inputRecoveryObservation{journal: journal, err: err}
	control.cancel()
	return protocol.SessionRegistrationResponse{Runtimes: []protocol.RegisteredRuntime{{RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1}}, LeaseDurationMS: protocol.MinimumLeaseDurationMS}, nil
}

func (control *recoveryRetryControl) RegisterSession(context.Context, string, string, protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error) {
	control.registerOnce.Do(func() { close(control.registered) })
	return protocol.SessionRegistrationResponse{Runtimes: []protocol.RegisteredRuntime{{RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1}}, LeaseDurationMS: protocol.MinimumLeaseDurationMS}, nil
}

func (control *recoveryRetryControl) Reconcile(_ context.Context, _ string, request protocol.ReconcileRequest) (protocol.ReconcileResponse, error) {
	control.mutex.Lock()
	control.count++
	call := control.count
	control.mutex.Unlock()
	control.calls <- call
	control.requests <- request
	if call >= 2 && control.cancelOnSecond {
		control.cancel()
	}
	return protocol.ReconcileResponse{}, nil
}

type snapshotStartObservation struct {
	journal state.RunJournal
	ack     protocol.CommandAcknowledgement
	err     error
}

type snapshotReconcileControl struct {
	runRetryReconcileControl
	acknowledged chan protocol.CommandAcknowledgement
}

func (control *snapshotReconcileControl) AcknowledgeCommand(_ context.Context, _ string, acknowledgement protocol.CommandAcknowledgement) error {
	control.acknowledged <- acknowledgement
	return nil
}

type recoveryBlockingProcess struct {
	done chan struct{}
	once sync.Once
}

func newRecoveryBlockingProcess() *recoveryBlockingProcess {
	return &recoveryBlockingProcess{done: make(chan struct{})}
}

func (*recoveryBlockingProcess) WriteInput([]byte) error { return nil }

func (process *recoveryBlockingProcess) Terminate(context.Context, time.Duration) error {
	process.once.Do(func() { close(process.done) })
	return nil
}

func (process *recoveryBlockingProcess) Wait() execution.Result {
	<-process.done
	return execution.Result{Terminated: true}
}

func (*recoveryBlockingProcess) ProcessDetails() (int, string) { return 44, "test:44" }
