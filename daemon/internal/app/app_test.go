package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/notification"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
	"github.com/wxxb789/symmetry/daemon/internal/workspace"
)

func TestRunEnrollsOnceAndReusesPersistedIdentity(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	t.Setenv("SYMMETRY_ENROLLMENT_TOKEN", "enrollment-token")
	control := &fakeControl{}
	enrollment := &fakeEnrollment{}
	value := testConfig(t)
	for attempt := 0; attempt < 2; attempt++ {
		ctx, cancel := context.WithCancel(context.Background())
		control.cancel = cancel
		if err := Run(ctx, value, WithStore(store), WithControl(control), WithEnrollment(enrollment), WithWorkspace(&fakeWorkspace{}), WithStartProcess(failStart)); err != nil {
			t.Fatalf("Run() attempt %d: %v", attempt, err)
		}
	}
	if enrollment.calls != 1 {
		t.Fatalf("Enroll calls = %d, want 1", enrollment.calls)
	}
	if control.registerCalls != 2 {
		t.Fatalf("RegisterSession calls = %d, want 2", control.registerCalls)
	}
	identity, err := store.LoadIdentity()
	if err != nil || identity.MachineID != "machine-1" {
		t.Fatalf("LoadIdentity() = %#v, %v", identity, err)
	}
}

func TestRunPersistsClaimIntentBeforeClaimAndHandlesNotification(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveIdentity(state.MachineIdentity{MachineID: "machine-1", MachineToken: "machine-token"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	control := &fakeControl{
		assignment: protocol.Assignment{RunID: "run-1", Generation: 1, Work: protocol.Work{Goal: "work"}},
		beforeClaim: func() {
			journals, listErr := store.ListJournals()
			if listErr != nil || len(journals) != 1 || journals[0].LocalState != "claiming" {
				t.Errorf("journal before Claim = %#v, err = %v", journals, listErr)
			}
		},
		cancel: cancel,
	}
	workEnabled := make(chan struct{})
	control.workEnabled = workEnabled
	notifier := fakeNotifier{before: func() { close(workEnabled) }, hints: []notification.Hint{{Type: "work_available", RuntimeID: "runtime-1"}}}
	started := make(chan execution.Invocation, 1)
	start := func(_ context.Context, invocation execution.Invocation, sink execution.Sink) (Process, error) {
		started <- invocation
		if err := sink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, At: time.Now().UTC(), Data: []byte("agent output")}); err != nil {
			return nil, err
		}
		return fakeProcess{result: execution.Result{ExitCode: 0, FinishedAt: time.Now().UTC()}}, nil
	}
	err = Run(ctx, testConfig(t), WithStore(store), WithControl(control), WithWorkspace(&fakeWorkspace{}), WithStartProcess(start), WithNotificationClient(notifier))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	select {
	case invocation := <-started:
		if invocation.Program != "agent" || invocation.Dir == "" {
			t.Fatalf("Invocation = %#v", invocation)
		}
	default:
		t.Fatal("notification did not trigger assignment processing")
	}
	if control.claimCalls != 1 {
		t.Fatalf("Claim calls = %d, want 1", control.claimCalls)
	}
	if control.eventCalls == 0 {
		t.Fatal("expected output events to be flushed")
	}
}

func TestJSONLWaitingInputTransitionsAndAcknowledges(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key := state.RunKey{RunID: "run-1", Generation: 1}
	_, err = store.SaveClaimIntent(state.ClaimIntent{Key: key, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1, ClaimID: "claim-1", Work: protocol.Work{Goal: "g"}, WorkspaceBindingKey: "local"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveClaimGrant(key, protocol.ClaimResponse{RunID: "run-1", Generation: 1, ClaimID: "claim-1", LeaseToken: "lease", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"}})
	if err != nil {
		t.Fatal(err)
	}
	process := &recordingProcess{}
	daemon := &daemon{store: store, config: testConfig(t), options: options{newID: ids(), clock: time.Now}, control: &fakeControl{}, running: map[state.RunKey]*runningRun{key: {process: process}}, slots: make(chan struct{}, 1)}
	if err := daemon.queueOutput(key, config.EventFormatJSONL, &jsonlParser{}, execution.Event{Stream: execution.Stdout, At: time.Now().UTC(), Data: []byte("{\"type\":\"waiting_for_input\"}\n")}); err != nil {
		t.Fatal(err)
	}
	daemon.handleCommand(context.Background(), protocol.Command{CommandID: "command-1", RunID: "run-1", Generation: 1, Kind: "provide_input", Payload: []byte(`{"answer":"yes"}`)})
	if got := string(process.input); got != "{\"answer\":\"yes\"}\n" {
		t.Fatalf("input = %q", got)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingCommandAcknowledgements) != 1 || len(journal.PendingTransitions) < 2 {
		t.Fatalf("journal = %#v", journal)
	}
}

func TestJSONLUnknownTypeIsStoredAsAgentEvent(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	key := state.RunKey{RunID: "run-1", Generation: 1}
	_, err = store.SaveClaimIntent(state.ClaimIntent{Key: key, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1, ClaimID: "claim-1", Work: protocol.Work{Goal: "g"}, WorkspaceBindingKey: "local"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveClaimGrant(key, protocol.ClaimResponse{RunID: "run-1", Generation: 1, ClaimID: "claim-1", LeaseToken: "lease", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"}})
	if err != nil {
		t.Fatal(err)
	}
	daemon := &daemon{store: store, options: options{newID: ids()}}
	if err := daemon.queueOutput(key, config.EventFormatJSONL, &jsonlParser{}, execution.Event{Stream: execution.Stdout, At: time.Now().UTC(), Data: []byte("{\"type\":\"custom\"}\n")}); err != nil {
		t.Fatal(err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingEvents) != 1 || journal.PendingEvents[0].Kind != "agent_event" {
		t.Fatalf("events = %#v", journal.PendingEvents)
	}
}

func TestClaimRetryReusesPersistedClaimID(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	control := &fakeControl{claimErr: &control.APIError{Code: control.StateConflict}}
	daemon := &daemon{
		config:    testConfig(t),
		store:     store,
		control:   control,
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		workspace: &fakeWorkspace{},
		start: func(context.Context, execution.Invocation, execution.Sink) (Process, error) {
			return fakeProcess{result: execution.Result{}}, nil
		},
		options:      options{newID: ids(), clock: time.Now},
		runtimeID:    "runtime-1",
		runtimeEpoch: 1,
		running:      make(map[state.RunKey]*runningRun),
		slots:        make(chan struct{}, 1),
	}
	assignment := protocol.Assignment{RunID: "run-1", Generation: 1, Work: protocol.Work{Goal: "g"}}
	daemon.startAssignment(context.Background(), assignment)
	daemon.workers.Wait()
	control.claimErr = nil
	daemon.startAssignment(context.Background(), assignment)
	daemon.workers.Wait()
	if len(control.claimIDs) != 2 || control.claimIDs[0] != control.claimIDs[1] {
		t.Fatalf("claim IDs = %#v, want a stable retry ID", control.claimIDs)
	}
}

func TestCommandAcknowledgementIsIdempotentAndUsesAllowedOutcomes(t *testing.T) {
	store, key := claimedStore(t)
	process := &recordingProcess{}
	daemon := &daemon{
		store:   store,
		options: options{newID: ids()},
		running: map[state.RunKey]*runningRun{key: {process: process}},
	}
	t.Cleanup(func() { _ = store.Close() })
	command := protocol.Command{CommandID: "command-1", RunID: key.RunID, Generation: key.Generation, Kind: "provide_input", Payload: []byte(`{"answer":"yes"}`)}
	daemon.handleCommand(context.Background(), command)
	daemon.handleCommand(context.Background(), command)
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(process.input); got != "{\"answer\":\"yes\"}\n" {
		t.Fatalf("input = %q, want one write", got)
	}
	if len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].Outcome != "applied" {
		t.Fatalf("acknowledgements = %#v", journal.PendingCommandAcknowledgements)
	}

	daemon.handleCommand(context.Background(), protocol.Command{CommandID: "command-2", RunID: key.RunID, Generation: key.Generation, Kind: "unknown"})
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if got := journal.PendingCommandAcknowledgements[1].Outcome; got != "rejected" {
		t.Fatalf("unknown command outcome = %q, want rejected", got)
	}
}

func TestAssignmentQueuesRunningBeforeSynchronousAgentOutput(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	observed := make(chan state.RunJournal, 1)
	daemon := &daemon{
		config:    testConfig(t),
		store:     store,
		control:   &fakeControl{},
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		workspace: &fakeWorkspace{},
		start: func(_ context.Context, _ execution.Invocation, sink execution.Sink) (Process, error) {
			if err := sink.Handle(context.Background(), execution.Event{Stream: execution.Stdout, At: time.Now().UTC(), Data: []byte("output")}); err != nil {
				return nil, err
			}
			journal, loadErr := store.LoadJournal(state.RunKey{RunID: "run-1", Generation: 1})
			if loadErr != nil {
				return nil, loadErr
			}
			observed <- journal
			return fakeProcess{result: execution.Result{}}, nil
		},
		options:      options{newID: ids(), clock: time.Now},
		runtimeID:    "runtime-1",
		runtimeEpoch: 1,
		running:      make(map[state.RunKey]*runningRun),
		slots:        make(chan struct{}, 1),
	}
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: protocol.Work{Goal: "g"}})
	daemon.workers.Wait()
	select {
	case journal := <-observed:
		if len(journal.PendingTransitions) == 0 || journal.PendingTransitions[0].State != "running" || len(journal.PendingEvents) != 1 {
			t.Fatalf("journal = %#v", journal)
		}
	default:
		t.Fatal("agent output was not observed")
	}
}

func TestStartAssignmentDoesNotBlockReactor(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entered := make(chan struct{})
	gate := make(chan struct{})
	control := &fakeControl{claimBlock: gate, claimEntered: entered}
	daemon := &daemon{
		config:       testConfig(t),
		store:        store,
		control:      control,
		log:          slog.New(slog.NewJSONHandler(io.Discard, nil)),
		workspace:    &fakeWorkspace{},
		start:        failStart,
		options:      options{newID: ids(), clock: time.Now},
		runtimeID:    "runtime-1",
		runtimeEpoch: 1,
		running:      make(map[state.RunKey]*runningRun),
		slots:        make(chan struct{}, 1),
	}
	done := make(chan struct{})
	go func() {
		daemon.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: protocol.Work{Goal: "g"}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("startAssignment blocked the reactor")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("claim worker did not start")
	}
	close(gate)
	daemon.workers.Wait()
}

func TestPreparedWorkspaceIsCleanedAfterStartFailure(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cleaned := make(chan bool, 1)
	daemon := &daemon{
		config:       testConfig(t),
		store:        store,
		control:      &fakeControl{},
		log:          slog.New(slog.NewJSONHandler(io.Discard, nil)),
		workspace:    &trackingWorkspace{cleaned: cleaned},
		start:        failStart,
		options:      options{newID: ids(), clock: time.Now},
		runtimeID:    "runtime-1",
		runtimeEpoch: 1,
		running:      make(map[state.RunKey]*runningRun),
		slots:        make(chan struct{}, 1),
	}
	key := state.RunKey{RunID: "run-1", Generation: 1}
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "g"}})
	daemon.workers.Wait()
	select {
	case succeeded := <-cleaned:
		if succeeded {
			t.Fatal("cleanup used success policy after failed start")
		}
	default:
		t.Fatal("prepared workspace was not cleaned")
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingTransitions) != 2 || journal.PendingTransitions[0].State != "running" || journal.PendingTransitions[1].State != "failed" {
		t.Fatalf("transitions = %#v", journal.PendingTransitions)
	}
}

func TestLeaseExpiryCancelsBlockedPrepareWithoutProcess(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace := newBlockingWorkspace()
	daemon := startupTestDaemon(t, store, workspace)
	key := state.RunKey{RunID: "run-1", Generation: 1}
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "g"}})
	<-workspace.entered
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	daemon.terminateForLease(journal, "lease expired")
	select {
	case <-workspace.cancelled:
	case <-time.After(time.Second):
		t.Fatal("lease expiry did not cancel blocked Prepare")
	}
	daemon.workers.Wait()
	select {
	case succeeded := <-workspace.cleaned:
		if succeeded {
			t.Fatal("stale startup cleanup used success policy")
		}
	default:
		t.Fatal("completed Prepare was not cleaned after lease expiry")
	}
}

func TestCancelDuringBlockedPrepareAcknowledgesAndCleansUp(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	workspace := newBlockingWorkspace()
	daemon := startupTestDaemon(t, store, workspace)
	key := state.RunKey{RunID: "run-1", Generation: 1}
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "g"}})
	<-workspace.entered
	daemon.handleCommand(context.Background(), protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"})
	select {
	case <-workspace.cancelled:
	case <-time.After(time.Second):
		t.Fatal("cancel command did not cancel blocked Prepare")
	}
	daemon.workers.Wait()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingTransitions) != 1 || journal.PendingTransitions[0].State != "cancelled" {
		t.Fatalf("transitions = %#v", journal.PendingTransitions)
	}
	if len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].Outcome != "applied" {
		t.Fatalf("acknowledgements = %#v", journal.PendingCommandAcknowledgements)
	}
	select {
	case succeeded := <-workspace.cleaned:
		if succeeded {
			t.Fatal("cancelled startup cleanup used success policy")
		}
	default:
		t.Fatal("completed Prepare was not cleaned after cancellation")
	}
}

func TestFlushCancelledAcknowledgesBeforeTerminalTransitionAndRetries(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	api := &orderingControl{failCancelledOnce: true}
	daemon := &daemon{store: store, control: api, workspace: &fakeWorkspace{}, log: slog.New(slog.NewJSONHandler(io.Discard, nil)), options: options{newID: ids()}}
	if err := daemon.queueTransition(key, "running", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := daemon.queueTransition(key, "cancelled", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	_, _ = store.SetLocalState(key, "terminal_pending")
	daemon.queueCommandAcknowledgement(key, "cancel-1", "applied")

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	ackID := journal.PendingCommandAcknowledgements[0].AckID
	transitionID := journal.PendingTransitions[1].TransitionID
	daemon.flushRun(context.Background(), journal)

	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].AckID != ackID || len(journal.PendingTransitions) != 1 || journal.PendingTransitions[0].TransitionID != transitionID {
		t.Fatalf("failed flush did not preserve retry IDs: %#v", journal)
	}
	if got, want := api.calls, []string{"ack:cancel-1", "transition:running", "transition:cancelled"}; !sameStrings(got, want) {
		t.Fatalf("first flush calls = %#v, want %#v", got, want)
	}

	daemon.flushRun(context.Background(), journal)
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("terminal journal = %v, want deleted", err)
	}
	want := []string{"ack:cancel-1", "transition:running", "transition:cancelled", "ack:cancel-1", "transition:cancelled"}
	if !sameStrings(api.calls, want) {
		t.Fatalf("retry calls = %#v, want %#v", api.calls, want)
	}
}

func TestFlushInputTransitionStillPrecedesAcknowledgement(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	api := &orderingControl{}
	daemon := &daemon{store: store, control: api, options: options{newID: ids()}}
	if err := daemon.queueTransition(key, "running", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	daemon.queueCommandAcknowledgement(key, "input-1", "applied")
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	daemon.flushRun(context.Background(), journal)
	if got, want := api.calls, []string{"transition:running", "ack:input-1"}; !sameStrings(got, want) {
		t.Fatalf("calls = %#v, want %#v", got, want)
	}
}

func TestJSONLNULFallsBackToRawOutputWithoutMisclassifyingLiteralEscape(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	daemon := &daemon{store: store, options: options{newID: ids()}}
	actualNUL := []byte("{\"message\":\"\\u0000\"}\n")
	if err := daemon.queueOutput(key, config.EventFormatJSONL, &jsonlParser{}, execution.Event{Stream: execution.Stdout, At: time.Now().UTC(), Data: actualNUL}); err != nil {
		t.Fatal(err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingEvents) != 1 || journal.PendingEvents[0].Kind != "output" {
		t.Fatalf("NUL event = %#v", journal.PendingEvents)
	}
	var raw map[string]string
	if err := json.Unmarshal(journal.PendingEvents[0].Payload, &raw); err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.StdEncoding.DecodeString(raw["data"])
	if err != nil || string(decoded) != string(actualNUL[:len(actualNUL)-1]) {
		t.Fatalf("raw event bytes = %q, %v", decoded, err)
	}
	if err := daemon.queueOutput(key, config.EventFormatJSONL, &jsonlParser{}, execution.Event{Stream: execution.Stdout, At: time.Now().UTC(), Data: []byte("{\"message\":\"\\\\u0000\"}\n")}); err != nil {
		t.Fatal(err)
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingEvents) != 2 || journal.PendingEvents[1].Kind != "agent_event" {
		t.Fatalf("literal escape event = %#v", journal.PendingEvents)
	}
}

func TestTerminalPendingLeaseIsRenewed(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	now := time.Now().UTC()
	if _, err := store.UpdateLeaseExpiry(key, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetLocalState(key, "terminal_pending"); err != nil {
		t.Fatal(err)
	}
	api := &fakeControl{}
	daemon := &daemon{store: store, control: api, options: options{clock: func() time.Time { return now }}, leaseDuration: 10 * time.Second}
	daemon.renewLeases(context.Background())
	if api.renewCalls != 1 {
		t.Fatalf("RenewLease calls = %d, want 1", api.renewCalls)
	}
}

func TestOwnershipLossReleasesActiveSlotAfterProcessStops(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.QueueEvent(key, protocol.RunEvent{EventID: "event-1", Sequence: 1, Kind: "output", OccurredAt: time.Now().UTC(), Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	process := newBlockingProcess()
	cleaned := make(chan bool, 1)
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	daemon := &daemon{
		store:     store,
		control:   &ownershipLossControl{},
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		workspace: &trackingWorkspace{cleaned: cleaned},
		running:   map[state.RunKey]*runningRun{key: {process: process, prepared: workspace.Prepared{Path: "C:\\workspace", Run: workspace.RunRef{RunID: key.RunID, Generation: key.Generation}}}},
		slots:     slots,
	}
	daemon.workers.Add(1)
	go func() {
		defer daemon.workers.Done()
		daemon.waitForRun(key)
	}()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	daemon.flushRun(context.Background(), journal)
	daemon.workers.Wait()
	if process.terminations != 1 {
		t.Fatalf("Terminate calls = %d, want 1", process.terminations)
	}
	if len(slots) != 0 {
		t.Fatal("slot was not released after ownership loss")
	}
	if _, exists := daemon.running[key]; exists {
		t.Fatal("run remained active after ownership loss")
	}
	select {
	case succeeded := <-cleaned:
		if succeeded {
			t.Fatal("ownership cleanup used success policy")
		}
	default:
		t.Fatal("workspace was not cleaned after ownership loss")
	}
}

func TestTerminalJournalFlushesBeforeNewSessionRegistration(t *testing.T) {
	store, key := terminalStore(t)
	defer store.Close()
	if err := store.SaveIdentity(state.MachineIdentity{MachineID: "machine-1", MachineToken: "machine-token"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	control := &terminalRecoveryControl{cancel: cancel}
	err := Run(ctx, testConfig(t), WithStore(store), WithControl(control), WithWorkspace(&fakeWorkspace{}), WithStartProcess(failStart))
	if err != nil {
		t.Fatal(err)
	}
	if !control.registeredAfterFlush {
		t.Fatal("session registered before prior terminal transition was flushed")
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("terminal journal = %v, want deleted", err)
	}
}

func TestTerminalFlushFailurePreventsNewSessionRegistration(t *testing.T) {
	store, _ := terminalStore(t)
	defer store.Close()
	if err := store.SaveIdentity(state.MachineIdentity{MachineID: "machine-1", MachineToken: "machine-token"}); err != nil {
		t.Fatal(err)
	}
	control := &terminalRecoveryControl{transitionErr: errors.New("temporary failure")}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := Run(ctx, testConfig(t), WithStore(store), WithControl(control), WithWorkspace(&fakeWorkspace{}), WithStartProcess(failStart))
	if err == nil {
		t.Fatal("Run() succeeded despite unflushed terminal journal")
	}
	if control.registerCalls != 0 {
		t.Fatalf("RegisterSession calls = %d, want 0", control.registerCalls)
	}
}

func TestTransientTerminalFlushRetriesBeforeSessionRegistration(t *testing.T) {
	store, key := terminalStore(t)
	defer store.Close()
	if err := store.SaveIdentity(state.MachineIdentity{MachineID: "machine-1", MachineToken: "machine-token"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	control := &terminalRecoveryControl{cancel: cancel, failOnce: true}
	if err := Run(ctx, testConfig(t), WithStore(store), WithControl(control), WithWorkspace(&fakeWorkspace{}), WithStartProcess(failStart)); err != nil {
		t.Fatal(err)
	}
	if !control.registeredAfterFlush || control.transitionCalls != 2 {
		t.Fatalf("registeredAfterFlush = %v, transition calls = %d", control.registeredAfterFlush, control.transitionCalls)
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("terminal journal = %v, want deleted", err)
	}
}

func TestReconcileCancelPreservesActiveExecution(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetLocalState(key, "running"); err != nil {
		t.Fatal(err)
	}
	process := newBlockingProcess()
	control := &reconcileControl{response: protocol.ReconcileResponse{Decisions: []protocol.ReconcileDecision{{RunID: key.RunID, Generation: key.Generation, Decision: protocol.ReconcileCancel}}}}
	daemon := &daemon{store: store, control: control, log: slog.New(slog.NewJSONHandler(io.Discard, nil)), running: map[state.RunKey]*runningRun{key: {process: process}}, slots: make(chan struct{}, 1)}
	daemon.reconcile(context.Background())
	if process.terminations != 0 {
		t.Fatal("ReconcileCancel terminated active execution")
	}
	if _, err := store.LoadJournal(key); err != nil {
		t.Fatalf("active journal was removed: %v", err)
	}
	close(process.done)
}

func TestRecoveredJournalCleanupDeletesOnlyAfterRecover(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	journal.WorkspacePath = "C:\\workspace"
	journal.WorkspaceBindingKey = "local"
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	cleaned := make(chan bool, 1)
	daemon := &daemon{store: store, workspace: &trackingWorkspace{cleaned: cleaned}, log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	daemon.stopRecoveredJournal(journal, "stale")
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("recovered journal = %v, want deleted", err)
	}
	select {
	case <-cleaned:
	default:
		t.Fatal("recovered workspace was not cleaned")
	}
}

func TestReconcileFiltersIneligibleJournalStates(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	journal.LocalState = "unexpected_local_state"
	journal.WorkspacePath = "C:\\workspace"
	journal.WorkspaceBindingKey = "local"
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	control := &reconcileCaptureControl{}
	daemon := &daemon{store: store, control: control, workspace: &fakeWorkspace{}, log: slog.New(slog.NewJSONHandler(io.Discard, nil)), running: make(map[state.RunKey]*runningRun)}
	daemon.reconcile(context.Background())
	if len(control.request.Runs) != 0 {
		t.Fatalf("ReconcileRequest.Runs = %#v, want no ineligible journals", control.request.Runs)
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("ineligible journal = %v, want safely removed", err)
	}
}

func TestHeartbeatMapsTerminalPendingToServerAcceptableState(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{TransitionID: "cancelled-1", State: "cancelled", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	control := &fakeControl{}
	daemon := &daemon{store: store, control: control, runtimeID: "runtime-1", runtimeEpoch: 1}
	daemon.heartbeat(context.Background())
	if len(control.heartbeat.ActiveRuns) != 1 || control.heartbeat.ActiveRuns[0].State != "cancelling" {
		t.Fatalf("heartbeat active runs = %#v", control.heartbeat.ActiveRuns)
	}
}

func TestTerminalTransitionEnqueueFailureRetainsJournalAndSlot(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	daemon := &daemon{
		store:   store,
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options: options{newID: func() (string, error) { return "", errors.New("disk failure") }},
		running: map[state.RunKey]*runningRun{key: {process: fakeProcess{result: execution.Result{}}}},
		slots:   slots,
	}
	daemon.waitForRun(key)
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState == "terminal_pending" || len(journal.PendingTransitions) != 0 {
		t.Fatalf("journal = %#v", journal)
	}
	if len(slots) != 1 {
		t.Fatal("slot was released after failed terminal enqueue")
	}
	if _, exists := daemon.running[key]; !exists {
		t.Fatal("run was removed after failed terminal enqueue")
	}
}

func TestRecoveredCleanupRulesPreserveOrRemoveJournalSafely(t *testing.T) {
	t.Run("empty path is no-op", func(t *testing.T) {
		store, key := claimedStore(t)
		defer store.Close()
		journal, err := store.LoadJournal(key)
		if err != nil {
			t.Fatal(err)
		}
		daemon := &daemon{store: store, log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
		daemon.stopRecoveredJournal(journal, "stale")
		if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
			t.Fatalf("journal = %v, want deleted", err)
		}
	})
	t.Run("missing binding is retained", func(t *testing.T) {
		daemon := &daemon{log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
		if err := daemon.cleanupRecoveredWorkspace(state.RunJournal{WorkspacePath: "C:\\workspace"}, false); err == nil {
			t.Fatal("cleanupRecoveredWorkspace accepted a missing binding key")
		}
	})
}

func TestTerminalRecoveredCleanupFailureRetainsJournal(t *testing.T) {
	store, key := terminalStore(t)
	defer store.Close()
	daemon := &daemon{store: store, control: &orderingControl{}, workspace: failingRecoverWorkspace{}, log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	daemon.flushRun(context.Background(), journal)
	if _, err := store.LoadJournal(key); err != nil {
		t.Fatalf("journal was deleted after cleanup failure: %v", err)
	}
}

func TestJSONLParserBoundsUnterminatedRecordsAndPreservesNormalJSONL(t *testing.T) {
	parser := &jsonlParser{}
	input := make([]byte, maxJSONLRecordBytes+rawOutputChunkBytes+1)
	for index := range input {
		input[index] = 'x'
	}
	records := parser.push(input)
	if len(records) < 2 || !parser.overflow || len(parser.pending) != 0 {
		t.Fatalf("overflow records = %d, overflow = %v, pending = %d", len(records), parser.overflow, len(parser.pending))
	}
	total := 0
	for _, record := range records {
		if !record.raw || len(record.data) > rawOutputChunkBytes {
			t.Fatalf("record = %#v", record)
		}
		total += len(record.data)
	}
	if total != len(input) {
		t.Fatalf("raw bytes = %d, want %d", total, len(input))
	}
	followUp := parser.push([]byte("tail\n"))
	if len(followUp) != 1 || !followUp[0].raw || string(followUp[0].data) != "tail" {
		t.Fatalf("overflow tail = %#v", followUp)
	}

	normal := (&jsonlParser{}).push([]byte(`{"type":"progress"}` + "\n"))
	if len(normal) != 1 || normal[0].raw || string(normal[0].data) != `{"type":"progress"}` {
		t.Fatalf("normal JSONL = %#v", normal)
	}
}

func TestJSONLPrimitiveAndArrayUseRawOutputAndFlushWithTerminal(t *testing.T) {
	store, key := terminalStore(t)
	defer store.Close()
	daemon := &daemon{store: store, control: &orderingControl{}, workspace: &fakeWorkspace{}, log: slog.New(slog.NewJSONHandler(io.Discard, nil)), options: options{newID: ids()}}
	parser := &jsonlParser{}
	for _, value := range [][]byte{[]byte("42\n"), []byte("[\"progress\"]\n")} {
		if err := daemon.queueOutput(key, config.EventFormatJSONL, parser, execution.Event{Stream: execution.Stdout, At: time.Now().UTC(), Data: value}); err != nil {
			t.Fatal(err)
		}
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingEvents) != 2 || journal.PendingEvents[0].Kind != "output" || journal.PendingEvents[1].Kind != "output" {
		t.Fatalf("events = %#v", journal.PendingEvents)
	}
	daemon.flushRun(context.Background(), journal)
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("terminal journal = %v, want deleted", err)
	}
}

func TestJSONLRawFallbackDoesNotDropLaterRecords(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	daemon := &daemon{store: store, options: options{newID: ids()}}
	data := []byte("42\n[\"raw\"]\n{\"type\":\"waiting_for_input\"}\n{\"type\":\"progress\"}\n")
	if err := daemon.queueOutput(key, config.EventFormatJSONL, &jsonlParser{}, execution.Event{Stream: execution.Stdout, At: time.Now().UTC(), Data: data}); err != nil {
		t.Fatal(err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingEvents) != 4 || journal.PendingEvents[0].Kind != "output" || journal.PendingEvents[1].Kind != "output" || journal.PendingEvents[2].Kind != "waiting_for_input" || journal.PendingEvents[3].Kind != "progress" {
		t.Fatalf("events = %#v", journal.PendingEvents)
	}
	if len(journal.PendingTransitions) != 1 || journal.PendingTransitions[0].State != "waiting_for_input" {
		t.Fatalf("transitions = %#v", journal.PendingTransitions)
	}
}

func TestRetryableClaimUsesHTTPStatus(t *testing.T) {
	if !retryableClaim(&control.APIError{StatusCode: http.StatusInternalServerError, Code: "internal_error"}) {
		t.Fatal("500 internal_error was not retryable")
	}
	if !retryableClaim(&control.APIError{StatusCode: http.StatusTooManyRequests, Code: "rate_limited"}) {
		t.Fatal("429 was not retryable")
	}
	if retryableClaim(&control.APIError{StatusCode: http.StatusBadRequest, Code: "internal_error"}) {
		t.Fatal("4xx was retryable")
	}
}

func TestCancelWinsCompletionAndFlushesAcknowledgement(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	process := newBlockingProcess()
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	daemon := &daemon{store: store, control: &orderingControl{}, workspace: &fakeWorkspace{}, log: slog.New(slog.NewJSONHandler(io.Discard, nil)), options: options{newID: ids()}, running: map[state.RunKey]*runningRun{key: {process: process, claimed: true}}, slots: slots}
	daemon.workers.Add(1)
	go func() { defer daemon.workers.Done(); daemon.waitForRun(key) }()
	daemon.handleCommand(context.Background(), protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"})
	daemon.workers.Wait()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingTransitions) != 1 || journal.PendingTransitions[0].State != "cancelled" || len(journal.PendingCommandAcknowledgements) != 1 {
		t.Fatalf("terminal journal = %#v", journal)
	}
	daemon.flushRun(context.Background(), journal)
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("journal = %v, want deleted", err)
	}
	if len(slots) != 0 {
		t.Fatal("slot was not released after cancelled terminal flush")
	}
}

func TestInitialInputUsesLocalModeAndPreservesGoalAndStructuredInput(t *testing.T) {
	jsonInput, err := initialInput(config.AgentProfile{InputMode: config.InputModeJSON}, protocol.Work{Goal: "implement feature", Input: []byte(`{"mode":"review"}`)})
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Goal  string          `json:"goal"`
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(jsonInput, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Goal != "implement feature" || string(payload.Input) != `{"mode":"review"}` {
		t.Fatalf("JSON input = %s", jsonInput)
	}
	goalInput, err := initialInput(config.AgentProfile{InputMode: config.InputModeGoal}, protocol.Work{Goal: "implement feature", Input: []byte(`{"ignored":true}`)})
	if err != nil || string(goalInput) != "implement feature\n" {
		t.Fatalf("goal input = %q, %v", goalInput, err)
	}
}

func TestCancelDuringClaimDefersUntilGrantAndNeverLaunchesAgent(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	control := &fakeControl{claimBlock: gate, claimEntered: entered}
	started := make(chan struct{}, 1)
	daemon := &daemon{config: testConfig(t), store: store, control: control, log: slog.New(slog.NewJSONHandler(io.Discard, nil)), workspace: &fakeWorkspace{}, start: func(context.Context, execution.Invocation, execution.Sink) (Process, error) {
		started <- struct{}{}
		return fakeProcess{}, nil
	}, options: options{newID: ids(), clock: time.Now}, runtimeID: "runtime-1", runtimeEpoch: 1, running: make(map[state.RunKey]*runningRun), slots: make(chan struct{}, 1)}
	key := state.RunKey{RunID: "run-1", Generation: 1}
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "g"}})
	<-entered
	daemon.handleCommand(context.Background(), protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"})
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingCommandAcknowledgements) != 0 {
		t.Fatalf("pre-grant acknowledgements = %#v", journal.PendingCommandAcknowledgements)
	}
	close(gate)
	daemon.workers.Wait()
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingTransitions) != 1 || journal.PendingTransitions[0].State != "cancelled" || len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].Outcome != "applied" {
		t.Fatalf("post-grant journal = %#v", journal)
	}
	select {
	case <-started:
		t.Fatal("agent launched after pending cancel")
	default:
	}
}

func TestTransientClaimRetryUsesPersistedClaimID(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	control := &fakeControl{claimErrors: []error{errors.New("network interrupted")}}
	daemon := &daemon{config: testConfig(t), store: store, control: control, log: slog.New(slog.NewJSONHandler(io.Discard, nil)), workspace: &fakeWorkspace{}, start: func(context.Context, execution.Invocation, execution.Sink) (Process, error) {
		return fakeProcess{}, nil
	}, options: options{newID: ids(), clock: time.Now}, runtimeID: "runtime-1", runtimeEpoch: 1, running: make(map[state.RunKey]*runningRun), slots: make(chan struct{}, 1)}
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: "run-1", Generation: 1, Work: protocol.Work{Goal: "g"}})
	daemon.workers.Wait()
	if len(control.claimIDs) != 2 || control.claimIDs[0] != control.claimIDs[1] {
		t.Fatalf("claim IDs = %#v", control.claimIDs)
	}
}

func TestShutdownDuringClaimDoesNotQueueCancelledTransitionOrEmptyAck(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	control := &fakeControl{claimBlock: gate, claimEntered: entered}
	daemon := &daemon{config: testConfig(t), store: store, control: control, log: slog.New(slog.NewJSONHandler(io.Discard, nil)), workspace: &fakeWorkspace{}, start: failStart, options: options{newID: ids(), clock: time.Now}, runtimeID: "runtime-1", runtimeEpoch: 1, running: make(map[state.RunKey]*runningRun), slots: make(chan struct{}, 1)}
	key := state.RunKey{RunID: "run-1", Generation: 1}
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "g"}})
	<-entered
	daemon.stopAll()
	close(gate)
	daemon.workers.Wait()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingTransitions) != 0 || len(journal.PendingCommandAcknowledgements) != 0 || journal.LocalState == "terminal_pending" {
		t.Fatalf("shutdown journal = %#v", journal)
	}
}

func TestRenewLeasesDoesNotSeriallyBlockOtherRuns(t *testing.T) {
	store, first := claimedStore(t)
	defer store.Close()
	second := state.RunKey{RunID: "run-2", Generation: 1}
	_, err := store.SaveClaimIntent(state.ClaimIntent{Key: second, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1, ClaimID: "claim-2", Work: protocol.Work{Goal: "g"}, WorkspaceBindingKey: "local"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveClaimGrant(second, protocol.ClaimResponse{RunID: second.RunID, Generation: second.Generation, ClaimID: "claim-2", LeaseToken: "lease-2", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"}})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, key := range []state.RunKey{first, second} {
		if _, err := store.UpdateLeaseExpiry(key, now.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	release := make(chan struct{})
	api := &parallelRenewControl{started: make(chan string, 2), release: release}
	daemon := &daemon{store: store, control: api, options: options{clock: func() time.Time { return now }}, leaseDuration: 10 * time.Second, slots: make(chan struct{}, 2)}
	done := make(chan struct{})
	go func() { daemon.renewLeases(context.Background()); close(done) }()
	seen := make(map[string]bool)
	for range 2 {
		select {
		case runID := <-api.started:
			seen[runID] = true
		case <-time.After(time.Second):
			t.Fatal("a hung renewal blocked another run")
		}
	}
	if !seen[first.RunID] || !seen[second.RunID] {
		t.Fatalf("renewals started = %#v", seen)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("renewal loop did not complete")
	}
}

func terminalStore(t *testing.T) (*state.Store, state.RunKey) {
	t.Helper()
	store, key := claimedStore(t)
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	journal.WorkspacePath = "C:\\workspace"
	journal.WorkspaceBindingKey = "local"
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	return store, key
}

func startupTestDaemon(t *testing.T, store *state.Store, service workspace.Service) *daemon {
	t.Helper()
	return &daemon{
		config:       testConfig(t),
		store:        store,
		control:      &fakeControl{},
		log:          slog.New(slog.NewJSONHandler(io.Discard, nil)),
		workspace:    service,
		start:        failStart,
		options:      options{newID: ids(), clock: time.Now},
		runtimeID:    "runtime-1",
		runtimeEpoch: 1,
		running:      make(map[state.RunKey]*runningRun),
		slots:        make(chan struct{}, 1),
	}
}

func claimedStore(t *testing.T) (*state.Store, state.RunKey) {
	t.Helper()
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	key := state.RunKey{RunID: "run-1", Generation: 1}
	_, err = store.SaveClaimIntent(state.ClaimIntent{Key: key, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1, ClaimID: "claim-1", Work: protocol.Work{Goal: "g"}, WorkspaceBindingKey: "local"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.SaveClaimGrant(key, protocol.ClaimResponse{RunID: key.RunID, Generation: key.Generation, ClaimID: "claim-1", LeaseToken: "lease", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"}})
	if err != nil {
		t.Fatal(err)
	}
	return store, key
}

func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{ControlPlaneURL: "https://control.example.test/api", StateDir: t.TempDir(), MachineName: "machine", AgentProfiles: map[string]config.AgentProfile{"local": {Command: "agent", InputMode: config.InputModeGoal, EventFormat: config.EventFormatRaw}}, Workspaces: map[string]config.Workspace{"local": {Policy: config.WorkspacePolicyExistingCheckout, Path: t.TempDir(), Cleanup: config.CleanupNever}}, Runtime: config.Runtime{RuntimeKey: "default", Name: "runtime", Capacity: 1, AgentProfile: "local", Workspace: "local"}}
}

func ids() func() (string, error) {
	var mutex sync.Mutex
	value := 0
	return func() (string, error) {
		mutex.Lock()
		defer mutex.Unlock()
		value++
		return "id-" + string(rune('0'+value)), nil
	}
}
func failStart(context.Context, execution.Invocation, execution.Sink) (Process, error) {
	return nil, errors.New("unexpected start")
}

type fakeEnrollment struct{ calls int }

func (client *fakeEnrollment) Enroll(context.Context, string, protocol.EnrollRequest) (protocol.EnrollResponse, error) {
	client.calls++
	return protocol.EnrollResponse{MachineID: "machine-1", MachineToken: "machine-token"}, nil
}

type fakeControl struct {
	registerCalls int
	claimCalls    int
	eventCalls    int
	assignment    protocol.Assignment
	beforeClaim   func()
	cancel        context.CancelFunc
	claimErr      error
	claimErrors   []error
	claimIDs      []string
	workEnabled   <-chan struct{}
	claimBlock    <-chan struct{}
	claimEntered  chan<- struct{}
	renewCalls    int
	heartbeat     protocol.RuntimeHeartbeatRequest
}

type orderingControl struct {
	fakeControl
	calls             []string
	terminal          bool
	failCancelledOnce bool
}

type ownershipLossControl struct{ fakeControl }

func (*ownershipLossControl) AppendEvents(context.Context, string, protocol.AppendEventsRequest) error {
	return &control.APIError{Code: control.OwnershipLost}
}

type terminalRecoveryControl struct {
	fakeControl
	cancel               context.CancelFunc
	transitionErr        error
	failOnce             bool
	transitionCalls      int
	flushed              bool
	registeredAfterFlush bool
}

func (client *terminalRecoveryControl) Transition(_ context.Context, _ string, request protocol.StateTransitionRequest) error {
	client.transitionCalls++
	if request.State == "completed" {
		client.flushed = true
	}
	if client.failOnce {
		client.failOnce = false
		return errors.New("temporary failure")
	}
	return client.transitionErr
}

func (client *terminalRecoveryControl) RegisterSession(_ context.Context, _ protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error) {
	client.registerCalls++
	client.registeredAfterFlush = client.flushed
	if client.cancel != nil {
		client.cancel()
	}
	return protocol.SessionRegistrationResponse{Runtimes: []protocol.RegisteredRuntime{{RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1}}}, nil
}

type reconcileControl struct {
	fakeControl
	response protocol.ReconcileResponse
}

func (client *reconcileControl) Reconcile(context.Context, string, protocol.ReconcileRequest) (protocol.ReconcileResponse, error) {
	return client.response, nil
}

type reconcileCaptureControl struct {
	fakeControl
	request protocol.ReconcileRequest
}

type parallelRenewControl struct {
	fakeControl
	started chan string
	release <-chan struct{}
}

func (client *parallelRenewControl) RenewLease(_ context.Context, runID string, _ protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
	client.started <- runID
	if runID == "run-1" {
		<-client.release
	}
	return protocol.LeaseHeartbeatResponse{LeaseExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (client *reconcileCaptureControl) Reconcile(_ context.Context, _ string, request protocol.ReconcileRequest) (protocol.ReconcileResponse, error) {
	client.request = request
	return protocol.ReconcileResponse{}, nil
}

func (client *orderingControl) Transition(_ context.Context, _ string, request protocol.StateTransitionRequest) error {
	client.calls = append(client.calls, "transition:"+request.State)
	if request.State == "cancelled" && client.failCancelledOnce {
		client.failCancelledOnce = false
		return errors.New("temporary transition failure")
	}
	if request.State == "cancelled" {
		client.terminal = true
	}
	return nil
}

func (client *orderingControl) AcknowledgeCommand(_ context.Context, commandID string, _ protocol.CommandAcknowledgement) error {
	client.calls = append(client.calls, "ack:"+commandID)
	if client.terminal {
		return &control.APIError{Code: control.OwnershipLost}
	}
	return nil
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (client *fakeControl) RegisterSession(_ context.Context, _ protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error) {
	client.registerCalls++
	if client.cancel != nil && client.assignment.RunID == "" {
		client.cancel()
	}
	return protocol.SessionRegistrationResponse{Runtimes: []protocol.RegisteredRuntime{{RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1}}}, nil
}
func (client *fakeControl) Heartbeat(_ context.Context, _ string, request protocol.RuntimeHeartbeatRequest) (protocol.RuntimeSnapshot, error) {
	client.heartbeat = request
	return protocol.RuntimeSnapshot{}, nil
}
func (client *fakeControl) Work(context.Context, string, int64) (protocol.RuntimeSnapshot, error) {
	if client.workEnabled != nil {
		select {
		case <-client.workEnabled:
		default:
			return protocol.RuntimeSnapshot{}, nil
		}
	}
	if client.assignment.RunID == "" {
		return protocol.RuntimeSnapshot{}, nil
	}
	assignment := client.assignment
	client.assignment = protocol.Assignment{}
	return protocol.RuntimeSnapshot{Assignments: []protocol.Assignment{assignment}}, nil
}
func (client *fakeControl) Claim(_ context.Context, runID string, request protocol.ClaimRequest) (protocol.ClaimResponse, error) {
	client.claimCalls++
	client.claimIDs = append(client.claimIDs, request.ClaimID)
	if client.claimEntered != nil {
		client.claimEntered <- struct{}{}
	}
	if client.claimBlock != nil {
		<-client.claimBlock
	}
	if client.beforeClaim != nil {
		client.beforeClaim()
	}
	if client.claimErr != nil {
		return protocol.ClaimResponse{}, client.claimErr
	}
	if len(client.claimErrors) > 0 {
		err := client.claimErrors[0]
		client.claimErrors = client.claimErrors[1:]
		return protocol.ClaimResponse{}, err
	}
	return protocol.ClaimResponse{RunID: runID, Generation: request.Generation, ClaimID: request.ClaimID, LeaseToken: "lease", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "work"}}, nil
}
func (client *fakeControl) RenewLease(context.Context, string, protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
	client.renewCalls++
	return protocol.LeaseHeartbeatResponse{LeaseExpiresAt: time.Now().Add(time.Minute)}, nil
}
func (client *fakeControl) AppendEvents(context.Context, string, protocol.AppendEventsRequest) error {
	client.eventCalls++
	return nil
}
func (client *fakeControl) Transition(_ context.Context, _ string, request protocol.StateTransitionRequest) error {
	if request.State == "completed" && client.cancel != nil {
		client.cancel()
	}
	return nil
}
func (client *fakeControl) Reconcile(context.Context, string, protocol.ReconcileRequest) (protocol.ReconcileResponse, error) {
	return protocol.ReconcileResponse{}, nil
}
func (client *fakeControl) AcknowledgeCommand(context.Context, string, protocol.CommandAcknowledgement) error {
	return nil
}

type fakeWorkspace struct{}

func (*fakeWorkspace) Prepare(_ context.Context, key string, run workspace.RunRef) (workspace.Prepared, error) {
	return workspace.Prepared{Path: "C:\\workspace", BindingKey: key, Run: run}, nil
}
func (*fakeWorkspace) Recover(_ context.Context, key string, run workspace.RunRef, path string) (workspace.Prepared, error) {
	return workspace.Prepared{Path: path, BindingKey: key, Run: run}, nil
}
func (*fakeWorkspace) Cleanup(context.Context, workspace.Prepared, bool) error { return nil }

type trackingWorkspace struct{ cleaned chan<- bool }

func (*trackingWorkspace) Prepare(_ context.Context, key string, run workspace.RunRef) (workspace.Prepared, error) {
	return workspace.Prepared{Path: "C:\\workspace", BindingKey: key, Run: run}, nil
}
func (*trackingWorkspace) Recover(_ context.Context, key string, run workspace.RunRef, path string) (workspace.Prepared, error) {
	return workspace.Prepared{Path: path, BindingKey: key, Run: run}, nil
}

func (workspace *trackingWorkspace) Cleanup(_ context.Context, _ workspace.Prepared, succeeded bool) error {
	workspace.cleaned <- succeeded
	return nil
}

type blockingWorkspace struct {
	entered   chan struct{}
	cancelled chan struct{}
	cleaned   chan bool
}

type failingRecoverWorkspace struct{}

func (failingRecoverWorkspace) Prepare(context.Context, string, workspace.RunRef) (workspace.Prepared, error) {
	return workspace.Prepared{}, errors.New("unexpected prepare")
}
func (failingRecoverWorkspace) Recover(context.Context, string, workspace.RunRef, string) (workspace.Prepared, error) {
	return workspace.Prepared{}, errors.New("recovery failed")
}
func (failingRecoverWorkspace) Cleanup(context.Context, workspace.Prepared, bool) error { return nil }

func newBlockingWorkspace() *blockingWorkspace {
	return &blockingWorkspace{entered: make(chan struct{}, 1), cancelled: make(chan struct{}, 1), cleaned: make(chan bool, 1)}
}

func (service *blockingWorkspace) Prepare(ctx context.Context, key string, run workspace.RunRef) (workspace.Prepared, error) {
	service.entered <- struct{}{}
	<-ctx.Done()
	service.cancelled <- struct{}{}
	return workspace.Prepared{Path: "C:\\workspace", BindingKey: key, Run: run}, nil
}
func (*blockingWorkspace) Recover(_ context.Context, key string, run workspace.RunRef, path string) (workspace.Prepared, error) {
	return workspace.Prepared{Path: path, BindingKey: key, Run: run}, nil
}

func (service *blockingWorkspace) Cleanup(_ context.Context, _ workspace.Prepared, succeeded bool) error {
	service.cleaned <- succeeded
	return nil
}

type fakeNotifier struct {
	before func()
	hints  []notification.Hint
}

func (client fakeNotifier) Run(ctx context.Context, hints chan<- notification.Hint) error {
	if client.before != nil {
		client.before()
	}
	for _, hint := range client.hints {
		hints <- hint
	}
	<-ctx.Done()
	return ctx.Err()
}

type fakeProcess struct{ result execution.Result }

func (process fakeProcess) WriteInput([]byte) error                        { return nil }
func (process fakeProcess) Terminate(context.Context, time.Duration) error { return nil }
func (process fakeProcess) Wait() execution.Result                         { return process.result }
func (fakeProcess) ProcessDetails() (int, string)                          { return 42, "test:42" }

type recordingProcess struct{ input []byte }

func (process *recordingProcess) WriteInput(input []byte) error {
	process.input = append(process.input, input...)
	return nil
}
func (*recordingProcess) Terminate(context.Context, time.Duration) error { return nil }
func (*recordingProcess) Wait() execution.Result                         { return execution.Result{} }
func (*recordingProcess) ProcessDetails() (int, string)                  { return 43, "test:43" }

type blockingProcess struct {
	done         chan struct{}
	terminations int
}

func newBlockingProcess() *blockingProcess       { return &blockingProcess{done: make(chan struct{})} }
func (*blockingProcess) WriteInput([]byte) error { return nil }
func (process *blockingProcess) Terminate(context.Context, time.Duration) error {
	process.terminations++
	close(process.done)
	return nil
}
func (process *blockingProcess) Wait() execution.Result {
	<-process.done
	return execution.Result{Terminated: true}
}
func (*blockingProcess) ProcessDetails() (int, string) { return 44, "test:44" }
