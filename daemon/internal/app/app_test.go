package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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
	if control.machineIDs[0] != "machine-1" || control.machineIDs[1] != "machine-1" {
		t.Fatalf("RegisterSession machine IDs = %#v", control.machineIDs)
	}
	identity, err := store.LoadIdentity()
	if err != nil || identity.MachineID != "machine-1" {
		t.Fatalf("LoadIdentity() = %#v, %v", identity, err)
	}
}

func TestEnrollmentIntentSurvivesUnknownOutcomeAndRestart(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	t.Setenv("SYMMETRY_ENROLLMENT_TOKEN", "enrollment-token")
	enrollment := &replayEnrollment{requests: make(chan enrollmentCall, 2)}
	firstContext, cancelFirst := context.WithCancel(context.Background())
	enrollment.fail = func() error {
		cancelFirst()
		return transportError("response lost")
	}
	first := &daemon{
		config: testConfig(t),
		log:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options: options{
			store:           store,
			control:         &fakeControl{},
			enrollment:      enrollment,
			workspace:       &fakeWorkspace{},
			start:           failStart,
			clock:           time.Now,
			newID:           ids(),
			newMachineToken: func() (string, error) { return "persisted-machine-token", nil },
		},
		running: make(map[state.RunKey]*runningRun),
	}
	if err := first.initialize(firstContext); err == nil {
		t.Fatal("first initialize succeeded after unknown enrollment outcome")
	}
	firstCall := <-enrollment.requests

	enrollment.fail = nil
	second := &daemon{
		config: testConfig(t),
		log:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options: options{
			store:           store,
			control:         &fakeControl{},
			enrollment:      enrollment,
			workspace:       &fakeWorkspace{},
			start:           failStart,
			clock:           time.Now,
			newID:           ids(),
			newMachineToken: func() (string, error) { return "different-token", nil },
		},
		running: make(map[state.RunKey]*runningRun),
	}
	if err := second.initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	secondCall := <-enrollment.requests
	if firstCall.idempotencyKey != secondCall.idempotencyKey || firstCall.request != secondCall.request {
		t.Fatalf("enrollment replay changed request: first=%#v second=%#v", firstCall, secondCall)
	}
	identity, err := store.LoadIdentity()
	if err != nil || identity.MachineToken != "persisted-machine-token" {
		t.Fatalf("identity = %#v, error = %v", identity, err)
	}
	if _, err := store.LoadEnrollmentIntent(); !state.IsNotFound(err) {
		t.Fatalf("enrollment intent error = %v, want deleted", err)
	}
}

func TestEnrollmentTokenMismatchKeepsIntentAndDoesNotPersistIdentity(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	t.Setenv("SYMMETRY_ENROLLMENT_TOKEN", "enrollment-token")
	daemon := &daemon{
		config: testConfig(t),
		log:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options: options{
			store:           store,
			control:         &fakeControl{},
			enrollment:      &fakeEnrollment{responseToken: "different-token"},
			workspace:       &fakeWorkspace{},
			start:           failStart,
			clock:           time.Now,
			newID:           ids(),
			newMachineToken: func() (string, error) { return "persisted-machine-token", nil },
		},
		running: make(map[state.RunKey]*runningRun),
	}
	err = daemon.initialize(context.Background())
	if err == nil || !strings.Contains(err.Error(), "machine token does not match request") {
		t.Fatalf("initialize error = %v, want token mismatch", err)
	}
	if _, err := store.LoadIdentity(); !state.IsNotFound(err) {
		t.Fatalf("identity error = %v, want not found", err)
	}
	if _, err := store.LoadEnrollmentIntent(); err != nil {
		t.Fatalf("enrollment intent error = %v, want retained", err)
	}
}

func TestEnrollmentIntentCleanupFailureDoesNotBlockPersistedIdentity(t *testing.T) {
	dir := t.TempDir()
	store, err := state.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	t.Setenv("SYMMETRY_ENROLLMENT_TOKEN", "enrollment-token")
	enrollment := &fakeEnrollment{beforeReturn: func() {
		path := filepath.Join(dir, "enrollment.json")
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(path, "block"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}}
	daemon := &daemon{
		config: testConfig(t),
		log:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options: options{
			store:           store,
			control:         &fakeControl{},
			enrollment:      enrollment,
			workspace:       &fakeWorkspace{},
			start:           failStart,
			clock:           time.Now,
			newID:           ids(),
			newMachineToken: func() (string, error) { return "persisted-machine-token", nil },
		},
		running: make(map[state.RunKey]*runningRun),
	}
	if err := daemon.initialize(context.Background()); err != nil {
		t.Fatalf("initialize after persisted identity = %v", err)
	}
	identity, err := store.LoadIdentity()
	if err != nil || identity.MachineID != "machine-1" || identity.MachineToken != "persisted-machine-token" {
		t.Fatalf("identity = %#v, error = %v", identity, err)
	}
}

func TestRunRejectsRegistrationLeaseBelowSafetyMinimum(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.SaveIdentity(state.MachineIdentity{MachineID: "machine-1", MachineToken: "machine-token"}); err != nil {
		t.Fatal(err)
	}
	control := &shortLeaseRegistrationControl{leaseDurationMS: 29_999}
	err = Run(context.Background(), testConfig(t), WithStore(store), WithControl(control), WithWorkspace(&fakeWorkspace{}), WithStartProcess(failStart))
	if err == nil || !strings.Contains(err.Error(), "lease duration must be at least 30000ms") {
		t.Fatalf("Run() error = %v, want minimum lease rejection", err)
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
	if control.eventCalls != 0 {
		t.Fatal("terminal run sent ordinary output events")
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
	if got := string(process.input); got != "{\"type\":\"provide_input\",\"goal\":\"g\",\"input\":{\"answer\":\"yes\"}}\n" {
		t.Fatalf("input = %q", got)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "running" || len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].Outcome != "applied" || len(journal.PendingTransitions) != 2 || journal.PendingTransitions[0].State != "waiting_for_input" || journal.PendingTransitions[1].State != "running" {
		t.Fatalf("journal = %#v", journal)
	}
}

func TestWaitingInputDrainsThroughRunningAndTerminal(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	process := &recordingProcess{}
	api := &orderingControl{}
	daemon := &daemon{
		store:     store,
		control:   api,
		workspace: &fakeWorkspace{},
		options:   options{newID: ids()},
		running:   map[state.RunKey]*runningRun{key: {process: process}},
	}
	if err := daemon.queueOutput(key, config.EventFormatJSONL, &jsonlParser{}, execution.Event{Stream: execution.Stdout, At: time.Now().UTC(), Data: []byte("{\"type\":\"waiting_for_input\",\"question\":\"continue?\"}\n")}); err != nil {
		t.Fatal(err)
	}
	daemon.handleCommand(context.Background(), protocol.Command{CommandID: "input-1", RunID: key.RunID, Generation: key.Generation, Kind: "provide_input", Payload: []byte(`{"answer":"yes"}`)})
	if err := daemon.queueTerminalTransition(key, "completed", map[string]any{"exit_code": 0}); err != nil {
		t.Fatal(err)
	}

	daemon.mu.Lock()
	delete(daemon.running, key)
	daemon.mu.Unlock()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(context.Background(), journal); err != nil {
		t.Fatalf("flushRun() error = %v", err)
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("journal = %v, want deleted after terminal drain", err)
	}
	if got, want := api.calls, []string{"transition:completed", "ack:input-1"}; !sameStrings(got, want) {
		t.Fatalf("flush calls = %#v, want %#v", got, want)
	}
}

func TestQueueWaitingForInputUsesOneIDAcrossLifecycleStates(t *testing.T) {
	for _, test := range []struct {
		name        string
		prepare     func(*testing.T, *state.Store, state.RunKey)
		transitions int
		localState  string
		wantEventID string
		wantQueueID bool
	}{
		{
			name:        "running queues shared event and transition ID",
			transitions: 1,
			localState:  "waiting_for_input",
			wantEventID: "waiting-id",
			wantQueueID: true,
		},
		{
			name: "terminal keeps telemetry when a second ID would fail",
			prepare: func(t *testing.T, store *state.Store, key state.RunKey) {
				t.Helper()
				if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}); err != nil {
					t.Fatal(err)
				}
			},
			transitions: 1,
			localState:  "terminal_pending",
			wantEventID: "waiting-id",
		},
		{
			name: "delivered keeps telemetry when a second ID would fail",
			prepare: func(t *testing.T, store *state.Store, key state.RunKey) {
				t.Helper()
				if _, err := store.SetLocalState(key, "waiting_for_input"); err != nil {
					t.Fatal(err)
				}
			},
			localState:  "waiting_for_input",
			wantEventID: "waiting-id",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, key := claimedStore(t)
			defer store.Close()
			if test.prepare != nil {
				test.prepare(t, store, key)
			}
			calls := 0
			daemon := &daemon{store: store, options: options{newID: func() (string, error) {
				calls++
				if calls > 1 {
					return "", errors.New("unexpected second ID allocation")
				}
				return "waiting-id", nil
			}}}
			if err := daemon.queueWaitingForInput(key, json.RawMessage(`{"type":"waiting_for_input"}`), time.Now().UTC()); err != nil {
				t.Fatalf("queueWaitingForInput() error = %v", err)
			}
			journal, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if calls != 1 || journal.LocalState != test.localState || len(journal.PendingEvents) != 1 || journal.PendingEvents[0].EventID != test.wantEventID || len(journal.PendingTransitions) != test.transitions {
				t.Fatalf("journal = %#v, ID calls = %d", journal, calls)
			}
			if test.wantQueueID && journal.PendingTransitions[0].TransitionID != journal.PendingEvents[0].EventID {
				t.Fatalf("event ID = %q, transition ID = %q", journal.PendingEvents[0].EventID, journal.PendingTransitions[0].TransitionID)
			}
		})
	}
}

func TestMarkRunningRejectsTerminalPendingJournal(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	terminal, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	daemon := &daemon{store: store, options: options{newID: ids()}}
	if err := daemon.markRunning(key); err == nil {
		t.Fatal("markRunning() succeeded after terminal transition")
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LocalState != terminal.LocalState || len(loaded.PendingTransitions) != 1 || len(terminal.PendingTransitions) != 1 || loaded.PendingTransitions[0].TransitionID != terminal.PendingTransitions[0].TransitionID || loaded.PendingTransitions[0].State != terminal.PendingTransitions[0].State || string(loaded.PendingTransitions[0].Payload) != string(terminal.PendingTransitions[0].Payload) {
		t.Fatalf("terminal journal mutated by markRunning(): %#v", loaded)
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
	if got := string(process.input); got != "{\"type\":\"provide_input\",\"goal\":\"g\",\"input\":{\"answer\":\"yes\"}}\n" {
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
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(context.Background(), journal); err != nil {
		t.Fatal(err)
	}
	select {
	case succeeded := <-cleaned:
		if succeeded {
			t.Fatal("cleanup used success policy after failed start")
		}
	default:
		t.Fatal("prepared workspace was not cleaned")
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("journal = %v, want deleted after terminal cleanup", err)
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
	if _, err := store.UpdateLeaseExpiry(key, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	daemon.renewLeases(context.Background())
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

func TestRenewLeasesStopsExpiredRecoveredJournalBeforeEligibility(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.SetProcessDetails(key, 42, "test:42", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateLeaseExpiry(key, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	terminated := make(chan struct{}, 1)
	daemon := &daemon{
		store:     store,
		control:   &fakeControl{},
		workspace: &fakeWorkspace{},
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options: options{
			clock:            time.Now,
			terminatePersist: func(int, string) error { terminated <- struct{}{}; return nil },
		},
		running: make(map[state.RunKey]*runningRun),
		slots:   make(chan struct{}, 1),
	}
	daemon.renewLeases(context.Background())
	select {
	case <-terminated:
	case <-time.After(time.Second):
		t.Fatal("expired recovered journal did not terminate persisted process")
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("expired recovered journal = %v, want removed", err)
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
	if err := daemon.flushRun(context.Background(), journal); err != nil {
		t.Fatal(err)
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

func TestFlushCancelledTransitionsBeforeAcknowledgementAndRetries(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	api := &orderingControl{failCancelledOnce: true}
	daemon := &daemon{store: store, control: api, workspace: &fakeWorkspace{}, log: slog.New(slog.NewJSONHandler(io.Discard, nil)), options: options{newID: ids()}}
	if err := daemon.queueTransition(key, "running", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := daemon.queueTerminalTransition(key, "cancelled", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	daemon.queueCommandAcknowledgement(key, "cancel-1", "applied")

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	ackID := journal.PendingCommandAcknowledgements[0].AckID
	transitionID := journal.PendingTransitions[0].TransitionID
	daemon.flushRun(context.Background(), journal)

	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].AckID != ackID || len(journal.PendingTransitions) != 1 || journal.PendingTransitions[0].TransitionID != transitionID {
		t.Fatalf("failed flush did not preserve retry IDs: %#v", journal)
	}
	if got, want := api.calls, []string{"transition:cancelled"}; !sameStrings(got, want) {
		t.Fatalf("first flush calls = %#v, want %#v", got, want)
	}

	daemon.flushRun(context.Background(), journal)
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("terminal journal = %v, want deleted", err)
	}
	want := []string{"transition:cancelled", "transition:cancelled", "ack:cancel-1"}
	if !sameStrings(api.calls, want) {
		t.Fatalf("retry calls = %#v, want %#v", api.calls, want)
	}
}

func TestPermanentCancelledAcknowledgementDoesNotBlockTerminalTransition(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	api := &permanentTerminalAcknowledgementControl{}
	daemon := &daemon{store: store, control: api, options: options{newID: ids()}}
	if err := daemon.queueTerminalTransition(key, "cancelled", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	daemon.queueCommandAcknowledgement(key, "cancel-1", "applied")
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(context.Background(), journal); err == nil {
		t.Fatal("flushRun() succeeded despite permanent acknowledgement failure")
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if api.transitionCalls != 1 || api.acknowledgementCalls != 1 || journal.TerminalVerdict != state.TerminalVerdictAccepted || len(journal.PendingTransitions) != 0 || len(journal.PendingCommandAcknowledgements) != 1 {
		t.Fatalf("permanent acknowledgement prevented terminal acceptance: %#v", journal)
	}
}

func TestCancelledCommandSignalsOutboxAfterAtomicReceipt(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	wake := make(chan struct{}, 1)
	daemon := &daemon{store: store, control: &fakeControl{}, options: options{newID: ids()}, outboxWake: wake}
	if err := daemon.queueCancelledTerminalAndAcknowledgement(key, "cancel-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-wake:
	case <-time.After(time.Second):
		t.Fatal("atomic cancellation did not wake outbox")
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "terminal_pending" || journal.TerminalState != "cancelled" || len(journal.PendingTransitions) != 1 || len(journal.PendingCommandAcknowledgements) != 1 {
		t.Fatalf("outbox wake observed partial cancellation journal: %#v", journal)
	}
}

func TestAtomicCancelledReceiptCannotBeDeletedBeforeAcknowledgement(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	timers := make(chan *manualDeadlineTimer, 1)
	control := &atomicCancelDeliveryControl{cancel: cancel}
	daemon := &daemon{
		store:   store,
		control: control,
		options: options{newID: ids(), newTimer: func(delay time.Duration) deadlineTimer {
			timer := &manualDeadlineTimer{channel: make(chan time.Time), delay: delay}
			timers <- timer
			return timer
		}},
		outboxWake:  make(chan struct{}, 1),
		outboxRetry: make(map[state.RunKey]outboxRetry),
	}
	outboxDone := make(chan struct{})
	go func() { daemon.runOutbox(ctx); close(outboxDone) }()
	<-timers
	if err := daemon.queueCancelledTerminalAndAcknowledgement(key, "cancel-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-outboxDone:
	case <-time.After(time.Second):
		t.Fatal("outbox did not drain atomic cancellation")
	}
	if got, want := control.calls, []string{"transition:cancelled", "ack:cancel-1"}; !sameStrings(got, want) {
		t.Fatalf("atomic cancellation delivery order = %#v, want %#v", got, want)
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("atomic cancellation journal = %v, want deleted after transition and acknowledgement", err)
	}
}

func TestStaleTerminalFailureDoesNotSuppressReplacementCancellation(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}, now); err != nil {
		t.Fatal(err)
	}
	api := &staleTerminalFailureControl{completedStarted: make(chan struct{}), releaseCompleted: make(chan struct{})}
	daemon := &daemon{
		store:       store,
		control:     api,
		workspace:   &fakeWorkspace{},
		log:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options:     options{newID: ids(), clock: func() time.Time { return now }},
		outboxRetry: make(map[state.RunKey]outboxRetry),
	}

	flushed := make(chan struct{})
	go func() {
		daemon.flushAll(context.Background())
		close(flushed)
	}()
	<-api.completedStarted
	if err := daemon.queueCancelledTerminalAndAcknowledgement(key, "cancel-1"); err != nil {
		t.Fatal(err)
	}
	close(api.releaseCompleted)
	select {
	case <-flushed:
	case <-time.After(time.Second):
		t.Fatal("outbox did not recover from stale terminal response")
	}

	if got, want := api.calls, []string{"transition:completed", "transition:cancelled", "ack:cancel-1"}; !sameStrings(got, want) {
		t.Fatalf("replacement cancellation delivery = %#v, want %#v", got, want)
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("replacement cancellation journal = %v, want deleted", err)
	}
}

func TestTerminalFailureAfterOrdinaryFlushUsesCurrentJournal(t *testing.T) {
	for _, test := range []struct {
		name      string
		err       error
		permanent bool
		retryAt   time.Duration
	}{
		{name: "retry after", err: &control.APIError{StatusCode: http.StatusTooManyRequests, Code: control.RateLimited, RetryAfter: 10 * time.Second}, retryAt: 10 * time.Second},
		{name: "permanent", err: &control.APIError{StatusCode: http.StatusUnprocessableEntity, Code: control.InvalidTransition}, permanent: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, key := claimedStore(t)
			defer store.Close()
			now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
			if _, err := store.QueueEvent(key, protocol.RunEvent{EventID: "event-1", Sequence: 1, Kind: "progress", OccurredAt: now, Payload: json.RawMessage(`{}`)}); err != nil {
				t.Fatal(err)
			}
			api := &terminalAfterEventControl{transitionErr: test.err}
			daemon := &daemon{
				store:       store,
				control:     api,
				workspace:   &fakeWorkspace{},
				log:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
				options:     options{newID: ids(), clock: func() time.Time { return now }},
				outboxRetry: make(map[state.RunKey]outboxRetry),
			}
			api.afterEvent = func() {
				if err := daemon.queueTerminalTransition(key, "completed", map[string]any{}); err != nil {
					t.Fatal(err)
				}
			}

			daemon.flushAll(context.Background())

			entry, exists := daemon.outboxRetry[key]
			if !exists || entry.permanent != test.permanent {
				t.Fatalf("terminal retry state = %#v, exists = %t", entry, exists)
			}
			if test.retryAt > 0 && !entry.retryAt.Equal(now.Add(test.retryAt)) {
				t.Fatalf("terminal retry at = %s, want %s", entry.retryAt, now.Add(test.retryAt))
			}
			if api.transitionCalls != 1 {
				t.Fatalf("terminal transition calls = %d, want 1", api.transitionCalls)
			}
		})
	}
}

func TestAcceptedTerminalPermanentAcknowledgementIsSuppressed(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	api := &permanentTerminalAcknowledgementControl{}
	daemon := &daemon{store: store, control: api, options: options{newID: ids(), clock: func() time.Time { return now }}, outboxRetry: make(map[state.RunKey]outboxRetry)}
	if err := daemon.queueCancelledTerminalAndAcknowledgement(key, "cancel-1"); err != nil {
		t.Fatal(err)
	}
	daemon.flushAll(context.Background())
	daemon.flushAll(context.Background())
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalVerdict != state.TerminalVerdictAccepted || len(journal.PendingCommandAcknowledgements) != 1 || api.transitionCalls != 1 || api.acknowledgementCalls != 1 || !daemon.outboxRetry[key].permanent {
		t.Fatalf("permanent terminal acknowledgement was retried or changed verdict: %#v", journal)
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

func TestFlushTransitionRetriesUnknownResultWithFrozenBody(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	api := &retryTransitionControl{failFirst: true}
	daemon := &daemon{store: store, control: api, options: options{newID: ids()}}
	if err := daemon.queueWaitingForInput(key, json.RawMessage(`{"type":"waiting_for_input","question":"first"}`), time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(context.Background(), journal); err == nil {
		t.Fatal("flushRun() succeeded after unknown transition result")
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingTransitions) != 1 || len(journal.AttemptedTransitionIDs) != 1 || journal.AttemptedTransitionIDs[0] != journal.PendingTransitions[0].TransitionID {
		t.Fatalf("journal after unknown result = %#v", journal)
	}
	daemon.flushRun(context.Background(), journal)
	if len(api.requests) != 2 || api.requests[0].TransitionID != api.requests[1].TransitionID || string(api.requests[0].Payload) != string(api.requests[1].Payload) {
		t.Fatalf("retry requests = %#v", api.requests)
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

func TestTerminalPendingLeaseIsNotRenewed(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	now := time.Now().UTC()
	if _, err := store.UpdateLeaseExpiry(key, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}, now); err != nil {
		t.Fatal(err)
	}
	api := &fakeControl{}
	daemon := &daemon{store: store, control: api, options: options{clock: func() time.Time { return now }}, leaseDuration: 10 * time.Second}
	daemon.renewLeases(context.Background())
	if api.renewCalls != 0 {
		t.Fatalf("RenewLease calls = %d, want 0", api.renewCalls)
	}
}

func TestTerminalGraceReleasesSlotExactlyOnceWithoutVerdict(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	enteredAt := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}, enteredAt); err != nil {
		t.Fatal(err)
	}
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	now := enteredAt
	timers := make(chan *manualDeadlineTimer, 1)
	daemon := &daemon{
		store:   store,
		control: &fakeControl{},
		options: options{clock: func() time.Time { return now }, newTimer: func(delay time.Duration) deadlineTimer {
			timer := &manualDeadlineTimer{channel: make(chan time.Time, 1), delay: delay}
			timers <- timer
			return timer
		}},
		background:    context.Background(),
		running:       map[state.RunKey]*runningRun{key: {slotHeld: true}},
		slots:         slots,
		leaseDuration: 10 * time.Second,
	}
	daemon.scheduleTerminalSlotRelease(key, enteredAt)
	timer := <-timers
	if timer.delay != terminalGrace {
		t.Fatalf("terminal grace delay = %s, want %s", timer.delay, terminalGrace)
	}
	now = enteredAt.Add(terminalGrace)
	timer.channel <- now
	daemon.terminalReleaseWG.Wait()
	daemon.releaseTerminalSlotAt(key, enteredAt)
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	active := daemon.running[key]
	if len(slots) != 0 || active == nil || active.slotHeld || journal.TerminalVerdict != "" || journal.LocalState != "terminal_pending" {
		t.Fatalf("terminal grace journal = %#v, slots = %d", journal, len(slots))
	}
}

func TestTerminalGraceUsesPersistedTerminalEntryTimeAfterReplacement(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	enteredAt := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}, enteredAt); err != nil {
		t.Fatal(err)
	}
	now := enteredAt.Add(time.Minute)
	timers := make(chan *manualDeadlineTimer, 1)
	daemon := &daemon{
		store: store,
		options: options{clock: func() time.Time { return now }, newID: ids(), newTimer: func(delay time.Duration) deadlineTimer {
			timer := &manualDeadlineTimer{channel: make(chan time.Time, 1), delay: delay}
			timers <- timer
			return timer
		}},
		background: context.Background(),
		running:    map[state.RunKey]*runningRun{key: {slotHeld: true}},
		slots:      make(chan struct{}, 1),
	}
	daemon.slots <- struct{}{}
	if err := daemon.queueTerminalTransition(key, "cancelled", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	timer := <-timers
	if want := terminalGrace - time.Minute; timer.delay != want {
		t.Fatalf("replacement terminal grace delay = %s, want %s", timer.delay, want)
	}
	active := daemon.running[key]
	active.terminalCancel()
	daemon.terminalReleaseWG.Wait()
}

func TestRenewalThresholdFollowsGrantedLease(t *testing.T) {
	for _, test := range []struct {
		name        string
		grant       time.Duration
		before      time.Duration
		atThreshold time.Duration
	}{
		{name: "120 second grant renews at 90 seconds", grant: 120 * time.Second, before: 91 * time.Second, atThreshold: 90 * time.Second},
		{name: "40 second grant renews at 30 seconds", grant: 40 * time.Second, before: 31 * time.Second, atThreshold: 30 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, key := claimedStore(t)
			defer store.Close()
			now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
			api := &fakeControl{}
			daemon := &daemon{
				store:         store,
				control:       api,
				options:       options{clock: func() time.Time { return now }},
				leaseDuration: test.grant,
				running:       map[state.RunKey]*runningRun{key: {process: fakeProcess{}}},
				slots:         make(chan struct{}, 1),
			}
			if _, err := store.UpdateLeaseExpiry(key, now.Add(test.before)); err != nil {
				t.Fatal(err)
			}
			daemon.renewLeases(context.Background())
			if api.renewCalls != 0 {
				t.Fatalf("RenewLease calls before threshold = %d, want 0", api.renewCalls)
			}
			if _, err := store.UpdateLeaseExpiry(key, now.Add(test.atThreshold)); err != nil {
				t.Fatal(err)
			}
			daemon.renewLeases(context.Background())
			if api.renewCalls != 1 {
				t.Fatalf("RenewLease calls at threshold = %d, want 1", api.renewCalls)
			}
		})
	}
}

func TestRenewalRequiresLiveNonterminalProcess(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*testing.T, *state.Store, state.RunKey)
		active  func(state.RunKey) map[state.RunKey]*runningRun
	}{
		{
			name:   "recovered journal has no local process",
			active: func(state.RunKey) map[state.RunKey]*runningRun { return nil },
		},
		{
			name: "terminal journal",
			prepare: func(t *testing.T, store *state.Store, key state.RunKey) {
				t.Helper()
				if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}, time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)); err != nil {
					t.Fatal(err)
				}
			},
			active: func(key state.RunKey) map[state.RunKey]*runningRun {
				return map[state.RunKey]*runningRun{key: {process: fakeProcess{}, terminal: true}}
			},
		},
		{
			name: "cleanup journal",
			prepare: func(t *testing.T, store *state.Store, key state.RunKey) {
				t.Helper()
				if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}); err != nil {
					t.Fatal(err)
				}
				if _, err := store.ResolveTerminal(key, state.TerminalVerdictAccepted, time.Date(2026, 9, 3, 1, 2, 4, 0, time.UTC)); err != nil {
					t.Fatal(err)
				}
				if _, err := store.MarkTransitionsDelivered(key, []string{"completed-1"}); err != nil {
					t.Fatal(err)
				}
				if _, err := store.EnterCleanupPending(key); err != nil {
					t.Fatal(err)
				}
			},
			active: func(key state.RunKey) map[state.RunKey]*runningRun {
				return map[state.RunKey]*runningRun{key: {process: fakeProcess{}}}
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, key := claimedStore(t)
			defer store.Close()
			now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
			if _, err := store.UpdateLeaseExpiry(key, now.Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if test.prepare != nil {
				test.prepare(t, store, key)
			}
			api := &fakeControl{}
			daemon := &daemon{store: store, control: api, options: options{clock: func() time.Time { return now }}, leaseDuration: 10 * time.Second, running: test.active(key), slots: make(chan struct{}, 1)}
			daemon.renewLeases(context.Background())
			if api.renewCalls != 0 {
				t.Fatalf("RenewLease calls = %d, want 0", api.renewCalls)
			}
		})
	}
}

func TestBlockedReactorDoesNotBlockLiveness(t *testing.T) {
	t.Run("blocked dispatch does not prevent renewal", func(t *testing.T) {
		store, key := claimedStore(t)
		defer store.Close()
		now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
		if _, err := store.UpdateLeaseExpiry(key, now.Add(leaseSafetyMargin+time.Second)); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		api := &blockingReactorControl{dispatchEntered: make(chan struct{}, 1), dispatchRelease: make(chan struct{}), renewEntered: make(chan struct{}, 1)}
		var releaseDispatch sync.Once
		release := func() { releaseDispatch.Do(func() { close(api.dispatchRelease) }) }
		defer release()
		daemon := &daemon{store: store, control: api, options: options{clock: func() time.Time { return now }, newTimer: blockedTimerFactory}, leaseDuration: 10 * time.Second, running: map[state.RunKey]*runningRun{key: {process: fakeProcess{}}}, slots: make(chan struct{}, 1), runtimeID: "runtime-1", runtimeEpoch: 1}
		dispatched := make(chan struct{})
		go func() { daemon.sync(ctx); close(dispatched) }()
		<-api.dispatchEntered
		livenessDone := make(chan struct{})
		go func() { daemon.runLiveness(ctx); close(livenessDone) }()
		select {
		case <-api.renewEntered:
		case <-time.After(time.Second):
			t.Fatal("blocked dispatch prevented renewal")
		}
		cancel()
		release()
		<-dispatched
		<-livenessDone
	})

	t.Run("blocked reconcile does not prevent terminal grace release", func(t *testing.T) {
		store, key := claimedStore(t)
		defer store.Close()
		enteredAt := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
		if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}, enteredAt); err != nil {
			t.Fatal(err)
		}
		now := enteredAt
		timers := make(chan *manualDeadlineTimer, 1)
		slots := make(chan struct{}, 1)
		slots <- struct{}{}
		api := &blockingReactorControl{reconcileEntered: make(chan struct{}, 1), reconcileRelease: make(chan struct{})}
		daemon := &daemon{store: store, control: api, options: options{clock: func() time.Time { return now }, newTimer: func(delay time.Duration) deadlineTimer {
			timer := &manualDeadlineTimer{channel: make(chan time.Time, 1), delay: delay}
			timers <- timer
			return timer
		}}, background: context.Background(), running: map[state.RunKey]*runningRun{key: {slotHeld: true}}, slots: slots, runtimeID: "runtime-1", runtimeEpoch: 1}
		reconciled := make(chan struct{})
		go func() { daemon.reconcile(context.Background()); close(reconciled) }()
		<-api.reconcileEntered
		daemon.scheduleTerminalSlotRelease(key, enteredAt)
		timer := <-timers
		now = enteredAt.Add(terminalGrace)
		timer.channel <- now
		daemon.terminalReleaseWG.Wait()
		if len(slots) != 0 {
			t.Fatal("blocked reconcile prevented terminal slot release")
		}
		close(api.reconcileRelease)
		<-reconciled
	})
}

func TestRenewalDeadlineDoesNotOutliveLeaseSafetyMargin(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	if _, err := store.UpdateLeaseExpiry(key, now.Add(leaseSafetyMargin+time.Second)); err != nil {
		t.Fatal(err)
	}
	process := newBlockingProcess()
	api := &expiringRenewalControl{advance: func() { now = now.Add(leaseSafetyMargin + 2*time.Second) }}
	daemon := &daemon{
		store:         store,
		control:       api,
		options:       options{clock: func() time.Time { return now }},
		leaseDuration: 120 * time.Second,
		running:       map[state.RunKey]*runningRun{key: {process: process}},
		slots:         make(chan struct{}, 1),
	}

	daemon.renewLeases(context.Background())

	if api.deadlineRemaining <= 0 || api.deadlineRemaining > 1500*time.Millisecond {
		t.Fatalf("renewal deadline remaining = %s, want at most 1.5s", api.deadlineRemaining)
	}
	if process.terminations != 1 {
		t.Fatalf("process terminations = %d, want 1 after lease expiry", process.terminations)
	}
}

func TestRenewalRejectsSuccessfulButUnsafeLease(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	if _, err := store.UpdateLeaseExpiry(key, now.Add(leaseSafetyMargin+time.Second)); err != nil {
		t.Fatal(err)
	}
	process := newBlockingProcess()
	daemon := &daemon{
		store:         store,
		control:       &unsafeRenewalControl{expiry: now.Add(leaseSafetyMargin)},
		options:       options{clock: func() time.Time { return now }},
		leaseDuration: 120 * time.Second,
		running:       map[state.RunKey]*runningRun{key: {process: process}},
		slots:         make(chan struct{}, 1),
	}

	daemon.renewLeases(context.Background())

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if process.terminations != 1 || journal.LocalState != "stale" {
		t.Fatalf("terminations = %d, journal = %#v", process.terminations, journal)
	}
}

func TestCancelCancelsRenewalAndIgnoresLateResponse(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	originalExpiry := now.Add(leaseSafetyMargin + time.Second)
	if _, err := store.UpdateLeaseExpiry(key, originalExpiry); err != nil {
		t.Fatal(err)
	}
	control := &lateRenewalControl{started: make(chan struct{}, 1), cancelled: make(chan struct{}, 1), release: make(chan struct{}), lateExpiry: now.Add(time.Hour)}
	daemon := &daemon{
		store:         store,
		control:       control,
		options:       options{clock: func() time.Time { return now }, newID: ids()},
		leaseDuration: 10 * time.Second,
		running:       map[state.RunKey]*runningRun{key: {slotHeld: true, claimed: true, process: fakeProcess{}}},
		slots:         make(chan struct{}, 1),
	}
	daemon.slots <- struct{}{}
	done := make(chan struct{})
	go func() { daemon.renewLeases(context.Background()); close(done) }()
	<-control.started
	daemon.handleCommand(context.Background(), protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"})
	<-control.cancelled
	close(control.release)
	<-done
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "terminal_pending" || !journal.LeaseExpiresAt.Equal(originalExpiry) {
		t.Fatalf("late renewal mutated terminal journal: %#v", journal)
	}
}

func TestTerminalAcceptanceReleasesSlotBeforeCleanup(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	daemon := &daemon{
		store:     store,
		control:   &orderingControl{},
		workspace: failingCleanupWorkspace{},
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options:   options{clock: func() time.Time { return now }, newID: ids()},
		running: map[state.RunKey]*runningRun{key: {
			slotHeld: true,
			prepared: workspace.Prepared{Path: "C:\\workspace", Run: workspace.RunRef{RunID: key.RunID, Generation: key.Generation}},
		}},
		slots: slots,
	}
	if err := daemon.queueTerminalTransition(key, "completed", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	persistWorkspacePath(t, store, key, "C:\\workspace")
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(context.Background(), journal); err == nil {
		t.Fatal("flushRun() succeeded despite cleanup failure")
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "cleanup_pending" || journal.TerminalVerdict != state.TerminalVerdictAccepted || !journal.TerminalResolvedAt.Equal(now) || len(slots) != 0 {
		t.Fatalf("accepted terminal journal = %#v, slots = %d", journal, len(slots))
	}
}

func TestTerminalAcknowledgementDoesNotMaskTransitionRejection(t *testing.T) {
	for _, test := range []struct {
		name    string
		err     error
		verdict string
	}{
		{name: "ownership lost", err: &control.APIError{StatusCode: http.StatusConflict, Code: control.OwnershipLost}, verdict: state.TerminalVerdictOwnershipLost},
		{name: "grace expired", err: &control.APIError{StatusCode: http.StatusConflict, Code: control.TerminalGraceExpired}, verdict: state.TerminalVerdictGraceExpired},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, key := claimedStore(t)
			defer store.Close()
			now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
			slots := make(chan struct{}, 1)
			slots <- struct{}{}
			daemon := &daemon{
				store:     store,
				control:   &terminalAcknowledgementControl{transitionErr: test.err},
				workspace: failingCleanupWorkspace{},
				log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
				options:   options{clock: func() time.Time { return now }, newID: ids()},
				running:   map[state.RunKey]*runningRun{key: {slotHeld: true}},
				slots:     slots,
			}
			if err := daemon.queueTerminalTransition(key, "cancelled", map[string]any{}); err != nil {
				t.Fatal(err)
			}
			persistWorkspacePath(t, store, key, "C:\\workspace")
			daemon.queueCommandAcknowledgement(key, "cancel-1", "applied")
			journal, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if err := daemon.flushRun(context.Background(), journal); err == nil {
				t.Fatal("flushRun() succeeded despite terminal transition rejection")
			}
			journal, err = store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if journal.LocalState != "cleanup_pending" || journal.TerminalVerdict != test.verdict || !journal.TerminalResolvedAt.Equal(now) || len(slots) != 0 || len(journal.PendingTransitions) != 0 || len(journal.PendingCommandAcknowledgements) != 0 {
				t.Fatalf("acknowledged terminal journal = %#v, slots = %d", journal, len(slots))
			}
			daemon.workspace = &fakeWorkspace{}
			daemon.flushAll(context.Background())
			if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
				t.Fatalf("conclusively rejected journal = %v, want deleted after cleanup", err)
			}
		})
	}
}

func TestTerminalOutboxOwnershipLossDropsOrdinaryItemsAndDeliversTerminal(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	control := &terminalOutboxOwnershipControl{}
	daemon := &daemon{
		store:     store,
		control:   control,
		workspace: failingCleanupWorkspace{},
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options:   options{newID: ids()},
		running: map[state.RunKey]*runningRun{key: {
			slotHeld: true,
			prepared: workspace.Prepared{Path: "C:\\workspace", Run: workspace.RunRef{RunID: key.RunID, Generation: key.Generation}},
		}},
		slots: slots,
	}
	if _, err := store.QueueEvent(key, protocol.RunEvent{EventID: "event-1", Sequence: 1, Kind: "output", OccurredAt: time.Now().UTC(), Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := daemon.queueTransition(key, "running", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	if err := daemon.queueTerminalTransition(key, "completed", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	persistWorkspacePath(t, store, key, "C:\\workspace")
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(context.Background(), journal); err == nil {
		t.Fatal("flushRun() succeeded despite cleanup failure")
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingEvents) != 0 || len(journal.PendingTransitions) != 0 || journal.TerminalVerdict != state.TerminalVerdictAccepted {
		t.Fatalf("ordinary terminal outbox items were not retired: %#v", journal)
	}
	if control.eventCalls != 0 || control.nonTerminalTransitionCalls != 0 {
		t.Fatalf("terminal flush called ordinary control APIs: events=%d transitions=%d", control.eventCalls, control.nonTerminalTransitionCalls)
	}
}

func TestRecoveredTerminalCancelReplacesUnconfirmedTerminal(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	enteredAt := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}, enteredAt); err != nil {
		t.Fatal(err)
	}
	daemon := &daemon{store: store, options: options{newID: ids()}, log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	daemon.handleCommand(context.Background(), protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"})
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "cancelled" || !journal.TerminalPendingAt.Equal(enteredAt) || len(journal.PendingTransitions) != 1 || journal.PendingTransitions[0].State != "cancelled" || len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].Outcome != "applied" {
		t.Fatalf("recovered cancellation did not replace terminal state: %#v", journal)
	}
}

func TestRecoveredTerminalCancelAcknowledgesFailureWhenReplacementCannotQueue(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveTerminal(key, state.TerminalVerdictAccepted, time.Date(2026, 9, 3, 1, 2, 4, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	daemon := &daemon{store: store, options: options{newID: ids()}, log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	daemon.handleCommand(context.Background(), protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"})
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "completed" || len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].Outcome != "failed" {
		t.Fatalf("recovered cancellation did not retain terminal result and failure acknowledgement: %#v", journal)
	}
}

func TestTerminalCleanupRetryPreservesSucceededOutcome(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	cleaner := &retryCleanupWorkspace{remainingFailures: 1}
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	daemon := &daemon{
		store:     store,
		control:   &fakeControl{},
		workspace: cleaner,
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options:   options{newID: ids()},
		running: map[state.RunKey]*runningRun{key: {
			slotHeld: true,
			prepared: workspace.Prepared{Path: "C:\\workspace", Run: workspace.RunRef{RunID: key.RunID, Generation: key.Generation}},
		}},
		slots: slots,
	}
	if err := daemon.queueTerminalTransition(key, "completed", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	persistWorkspacePath(t, store, key, "C:\\workspace")
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(context.Background(), journal); err == nil {
		t.Fatal("flushRun() succeeded despite first cleanup failure")
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(context.Background(), journal); err != nil {
		t.Fatalf("retry flushRun() error = %v", err)
	}
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("journal = %v, want deleted after cleanup retry", err)
	}
	if got, want := cleaner.outcomes, []bool{true, true}; !sameBools(got, want) {
		t.Fatalf("cleanup outcomes = %#v, want %#v", got, want)
	}
}

func TestTerminalQueueFailureClearsTerminalizing(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	daemon := &daemon{
		store:   store,
		options: options{newID: ids()},
		running: map[state.RunKey]*runningRun{key: {}},
	}
	if err := daemon.queueTerminalTransition(key, "running", map[string]any{}); err == nil {
		t.Fatal("queueTerminalTransition() accepted a nonterminal state")
	}
	active := daemon.running[key]
	if active == nil || active.terminal || active.terminalizing != 0 {
		t.Fatalf("failed terminal queue left active state = %#v", active)
	}
}

func TestEarlierRenewalCompletionKeepsLaterCancellationHandle(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	control := &overlappingRenewalControl{
		firstStarted:    make(chan struct{}, 1),
		firstRelease:    make(chan struct{}),
		secondStarted:   make(chan struct{}, 1),
		secondCancelled: make(chan struct{}, 1),
	}
	daemon := &daemon{
		store:   store,
		control: control,
		options: options{newID: ids()},
		running: map[state.RunKey]*runningRun{key: {process: fakeProcess{}}},
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan struct{})
	go func() {
		_, _ = daemon.renewLease(context.Background(), journal)
		close(firstDone)
	}()
	<-control.firstStarted
	if err := daemon.queueTerminalTransition(key, "running", map[string]any{}); err == nil {
		t.Fatal("queueTerminalTransition() accepted a nonterminal state")
	}
	secondDone := make(chan struct{})
	go func() {
		_, _ = daemon.renewLease(context.Background(), journal)
		close(secondDone)
	}()
	<-control.secondStarted
	close(control.firstRelease)
	<-firstDone
	finishTerminal := daemon.beginTerminal(key)
	select {
	case <-control.secondCancelled:
	case <-time.After(time.Second):
		t.Fatal("older renewal completion cleared the newer cancellation handle")
	}
	finishTerminal(false)
	<-secondDone
}

func TestAcceptedTerminalRetiresFenceRejectedAcknowledgement(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "ownership lost", err: &control.APIError{StatusCode: http.StatusConflict, Code: control.OwnershipLost}},
		{name: "grace expired", err: &control.APIError{StatusCode: http.StatusConflict, Code: control.TerminalGraceExpired}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, key := claimedStore(t)
			defer store.Close()
			cleaner := &retryCleanupWorkspace{remainingFailures: 1}
			daemon := &daemon{
				store:     store,
				control:   &terminalAcceptedAcknowledgementControl{acknowledgementErr: test.err},
				workspace: cleaner,
				log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
				options:   options{newID: ids()},
				running: map[state.RunKey]*runningRun{key: {
					prepared: workspace.Prepared{Path: "C:\\workspace", Run: workspace.RunRef{RunID: key.RunID, Generation: key.Generation}},
				}},
			}
			if err := daemon.queueTerminalTransition(key, "completed", map[string]any{}); err != nil {
				t.Fatal(err)
			}
			persistWorkspacePath(t, store, key, "C:\\workspace")
			daemon.queueCommandAcknowledgement(key, "input-1", "applied")
			journal, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if err := daemon.flushRun(context.Background(), journal); err == nil {
				t.Fatal("flushRun() succeeded despite cleanup failure")
			}
			journal, err = store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if journal.TerminalVerdict != state.TerminalVerdictAccepted || len(journal.PendingCommandAcknowledgements) != 0 {
				t.Fatalf("accepted terminal acknowledgement was not retired: %#v", journal)
			}
			if err := daemon.flushRun(context.Background(), journal); err != nil {
				t.Fatalf("cleanup retry after acknowledgement retirement: %v", err)
			}
			if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
				t.Fatalf("journal = %v, want deleted", err)
			}
		})
	}
}

func TestTerminalVerdictPersistsAndReleasesSlotBeforeCleanup(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	daemon := &daemon{
		store:     store,
		control:   &terminalVerdictControl{transitionErr: &control.APIError{StatusCode: http.StatusConflict, Code: control.TerminalGraceExpired}},
		workspace: failingCleanupWorkspace{},
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options:   options{clock: func() time.Time { return now }, newID: ids()},
		running: map[state.RunKey]*runningRun{key: {
			slotHeld: true,
			prepared: workspace.Prepared{Path: "C:\\workspace", Run: workspace.RunRef{RunID: key.RunID, Generation: key.Generation}},
		}},
		slots: slots,
	}
	if err := daemon.queueTerminalTransition(key, "completed", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	persistWorkspacePath(t, store, key, "C:\\workspace")
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(context.Background(), journal); err == nil {
		t.Fatal("flushRun() succeeded after terminal grace rejection")
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalVerdict != state.TerminalVerdictGraceExpired || !journal.TerminalResolvedAt.Equal(now) || len(slots) != 0 {
		t.Fatalf("terminal verdict journal = %#v, slots = %d", journal, len(slots))
	}
}

func TestCancelledTerminalOwnershipLossPersistsVerdictAndReleasesSlot(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	daemon := &daemon{
		store:     store,
		control:   &terminalVerdictControl{transitionErr: &control.APIError{StatusCode: http.StatusConflict, Code: control.OwnershipLost}},
		workspace: failingCleanupWorkspace{},
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options:   options{newID: ids()},
		running: map[state.RunKey]*runningRun{key: {
			slotHeld: true,
		}},
		slots: slots,
	}
	if err := daemon.queueTerminalTransition(key, "cancelled", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	persistWorkspacePath(t, store, key, "C:\\workspace")
	daemon.queueCommandAcknowledgement(key, "cancel-1", "applied")
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(context.Background(), journal); err == nil {
		t.Fatal("flushRun() succeeded after reaper ownership loss")
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.TerminalState != "cancelled" || journal.TerminalVerdict != state.TerminalVerdictOwnershipLost || len(slots) != 0 {
		t.Fatalf("cancelled terminal ownership loss = %#v, slots = %d", journal, len(slots))
	}
}

func TestReconcileRetainsTerminalPendingJournal(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveTerminal(key, state.TerminalVerdictOwnershipLost, time.Date(2026, 9, 3, 1, 2, 4, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	control := &reconcileCaptureControl{}
	daemon := &daemon{store: store, control: control, workspace: &fakeWorkspace{}, log: slog.New(slog.NewJSONHandler(io.Discard, nil)), running: make(map[state.RunKey]*runningRun)}
	daemon.reconcile(context.Background())
	if len(control.request.Runs) != 0 {
		t.Fatalf("ReconcileRequest.Runs = %#v, want none", control.request.Runs)
	}
	if _, err := store.LoadJournal(key); err != nil {
		t.Fatalf("terminal journal = %v, want retained", err)
	}
}

func TestOwnershipLossReleasesActiveSlotAfterProcessStops(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.QueueEvent(key, protocol.RunEvent{EventID: "event-1", Sequence: 1, Kind: "output", OccurredAt: time.Now().UTC(), Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	persistWorkspacePath(t, store, key, "C:\\workspace")
	process := newDeferredExitProcess()
	cleaned := make(chan bool, 1)
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	daemon := &daemon{
		store:     store,
		control:   &ownershipLossControl{},
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		workspace: &trackingWorkspace{cleaned: cleaned},
		running:   map[state.RunKey]*runningRun{key: {process: process, prepared: workspace.Prepared{Path: "C:\\workspace", Run: workspace.RunRef{RunID: key.RunID, Generation: key.Generation}}, slotHeld: true}},
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
	select {
	case <-process.terminated:
	case <-time.After(time.Second):
		t.Fatal("ownership loss did not terminate the process")
	}
	select {
	case <-cleaned:
		t.Fatal("workspace cleanup started before the process exited")
	default:
	}
	if len(slots) != 1 {
		t.Fatal("slot was released before the process exited")
	}
	close(process.exit)
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

func TestRetainedTerminalJournalDoesNotBlockNewSessionRegistration(t *testing.T) {
	store, key := terminalStore(t)
	defer store.Close()
	if err := store.SaveIdentity(state.MachineIdentity{MachineID: "machine-1", MachineToken: "machine-token"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	control := &terminalRecoveryControl{cancel: cancel, transitionErr: errors.New("temporary failure")}
	err := Run(ctx, testConfig(t), WithStore(store), WithControl(control), WithWorkspace(&fakeWorkspace{}), WithStartProcess(failStart))
	if err != nil {
		t.Fatal(err)
	}
	if control.registeredAfterFlush {
		t.Fatal("terminal journal was flushed before new session registration")
	}
	if _, err := store.LoadJournal(key); err != nil {
		t.Fatalf("terminal journal = %v, want retained", err)
	}
}

func TestTerminalFlushFailureDoesNotPreventNewSessionRegistration(t *testing.T) {
	store, key := terminalStore(t)
	defer store.Close()
	if err := store.SaveIdentity(state.MachineIdentity{MachineID: "machine-1", MachineToken: "machine-token"}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	control := &terminalRecoveryControl{cancel: cancel, transitionErr: errors.New("temporary failure")}
	err := Run(ctx, testConfig(t), WithStore(store), WithControl(control), WithWorkspace(&fakeWorkspace{}), WithStartProcess(failStart))
	if err != nil {
		t.Fatal(err)
	}
	if control.registerCalls != 1 {
		t.Fatalf("RegisterSession calls = %d, want 1", control.registerCalls)
	}
	if _, err := store.LoadJournal(key); err != nil {
		t.Fatalf("terminal journal = %v, want retained", err)
	}
}

func TestTerminalDeliveryContinuesAfterSessionRegistration(t *testing.T) {
	store, key := terminalStore(t)
	defer store.Close()
	if err := store.SaveIdentity(state.MachineIdentity{MachineID: "machine-1", MachineToken: "machine-token"}); err != nil {
		t.Fatal(err)
	}
	control := &terminalRecoveryControl{failOnce: true}
	daemon := startupTestDaemon(t, store, &fakeWorkspace{})
	daemon.control = control
	daemon.options.clock = time.Now
	if err := daemon.flushRun(context.Background(), func() state.RunJournal {
		journal, err := store.LoadJournal(key)
		if err != nil {
			t.Fatal(err)
		}
		return journal
	}()); err == nil {
		t.Fatal("flushRun() succeeded on the first transient failure")
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(context.Background(), journal); err != nil {
		t.Fatal(err)
	}
	if control.transitionCalls != 2 {
		t.Fatalf("transition calls = %d, want 2", control.transitionCalls)
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

func TestHeartbeatExcludesTerminalPendingRun(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{TransitionID: "cancelled-1", State: "cancelled", Payload: []byte(`{}`)}); err != nil {
		t.Fatal(err)
	}
	control := &fakeControl{}
	daemon := &daemon{store: store, control: control, runtimeID: "runtime-1", runtimeEpoch: 1}
	daemon.heartbeat(context.Background())
	if len(control.heartbeat.ActiveRuns) != 0 {
		t.Fatalf("heartbeat active runs = %#v, want none", control.heartbeat.ActiveRuns)
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
		running: map[state.RunKey]*runningRun{key: {process: fakeProcess{result: execution.Result{}}, slotHeld: true}},
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
		if err := daemon.cleanupRecoveredWorkspace(context.Background(), state.RunJournal{WorkspacePath: "C:\\workspace"}, false); err == nil {
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

func TestRetryStopsAfterPermanentError(t *testing.T) {
	var calls int
	want := &control.APIError{StatusCode: http.StatusBadRequest, Code: control.InvalidRequest}
	err := retry(context.Background(), func() error {
		calls++
		return want
	})
	if !errors.Is(err, want) || calls != 1 {
		t.Fatalf("retry error = %v, calls = %d", err, calls)
	}
}

func TestRetryHonorsRetryAfter(t *testing.T) {
	var calls int
	err := retry(context.Background(), func() error {
		calls++
		if calls == 1 {
			return &control.APIError{StatusCode: http.StatusTooManyRequests, Code: control.RateLimited, RetryAfter: time.Nanosecond}
		}
		return nil
	})
	if err != nil || calls != 2 {
		t.Fatalf("retry error = %v, calls = %d", err, calls)
	}
}

func TestRequestTimeoutRetriesButRootCancellationStops(t *testing.T) {
	t.Run("request deadline is retryable", func(t *testing.T) {
		var calls int
		err := retryWithWait(context.Background(), func() error {
			calls++
			if calls == 1 {
				return context.DeadlineExceeded
			}
			return nil
		}, func(context.Context, time.Duration) error { return nil })
		if err != nil || calls != 2 {
			t.Fatalf("retry error = %v, calls = %d", err, calls)
		}
	})

	t.Run("root cancellation stops retry", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var calls int
		err := retryWithWait(ctx, func() error {
			calls++
			return context.DeadlineExceeded
		}, func(context.Context, time.Duration) error {
			cancel()
			return context.Canceled
		})
		if !errors.Is(err, context.Canceled) || calls != 1 {
			t.Fatalf("retry error = %v, calls = %d", err, calls)
		}
	})
}

func TestExplicitZeroRetryAfterUsesMinimumRetryFloor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Retry-After", "0")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"error":{"code":"rate_limited","message":"retry"}}`))
	}))
	defer server.Close()
	client, err := control.NewClient(server.URL+"/api", "machine-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("generic retry", func(t *testing.T) {
		var calls int
		var observed time.Duration
		err := retryWithWait(context.Background(), func() error {
			calls++
			if calls == 1 {
				_, requestErr := client.Dispatch(context.Background(), "runtime-1", 1)
				return requestErr
			}
			return nil
		}, func(_ context.Context, delay time.Duration) error {
			observed = delay
			return nil
		})
		if err != nil || calls != 2 || observed != minimumInterval {
			t.Fatalf("retry error = %v, calls = %d, delay = %s", err, calls, observed)
		}
	})

	t.Run("claim retry", func(t *testing.T) {
		api := &zeroRetryClaimControl{client: client}
		daemon := &daemon{control: api}
		var observed time.Duration
		_, err := daemon.claimWithRetryWait(context.Background(), "run-1", protocol.ClaimRequest{RuntimeID: "runtime-1", RuntimeEpoch: 1, Generation: 1, ClaimID: "claim-1"}, func(_ context.Context, delay time.Duration) error {
			observed = delay
			return nil
		})
		if err != nil || api.calls != 2 || observed != minimumInterval {
			t.Fatalf("claim error = %v, calls = %d, delay = %s", err, api.calls, observed)
		}
	})
}

func TestRenewalCommandsUseSerialExecutorWithoutBlockingLiveness(t *testing.T) {
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
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	if _, err := store.UpdateLeaseExpiry(first, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateLeaseExpiry(second, now.Add(leaseSafetyMargin+time.Second)); err != nil {
		t.Fatal(err)
	}
	input := &blockingInputProcess{entered: make(chan struct{}, 1), release: make(chan struct{})}
	api := &blockingReactorControl{renewEntered: make(chan struct{}, 1)}
	daemon := &daemon{
		store:          store,
		control:        api,
		options:        options{clock: func() time.Time { return now }, newID: ids()},
		leaseDuration:  10 * time.Second,
		running:        map[state.RunKey]*runningRun{first: {process: input}, second: {process: fakeProcess{}}},
		slots:          make(chan struct{}, 2),
		commandWake:    make(chan struct{}, 1),
		queuedCommands: make(map[commandKey]*queuedCommand),
	}
	ctx, cancel := context.WithCancel(context.Background())
	var releaseInput sync.Once
	release := func() { releaseInput.Do(func() { close(input.release) }) }
	defer func() {
		release()
		cancel()
	}()
	executorDone := make(chan struct{})
	go func() { daemon.runCommandExecutor(ctx); close(executorDone) }()
	command := protocol.Command{CommandID: "input-1", RunID: first.RunID, Generation: first.Generation, Kind: "provide_input", Payload: json.RawMessage(`{}`)}
	daemon.enqueueCommand(command)
	daemon.enqueueCommand(command)
	<-input.entered
	renewed := make(chan struct{})
	go func() { daemon.renewLeases(ctx); close(renewed) }()
	select {
	case <-api.renewEntered:
	case <-time.After(time.Second):
		t.Fatal("blocked command executor prevented another run renewal")
	}
	<-renewed
	release()
	cancel()
	select {
	case <-executorDone:
	case <-time.After(time.Second):
		t.Fatal("command executor did not exit after shutdown")
	}
	if input.writes != 1 {
		t.Fatalf("WriteInput calls = %d, want 1 for duplicate command ID", input.writes)
	}
}

func TestRenewalAndSnapshotDoNotDuplicateCommandExecution(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	if _, err := store.UpdateLeaseExpiry(key, now.Add(leaseSafetyMargin+time.Second)); err != nil {
		t.Fatal(err)
	}
	input := &blockingInputProcess{entered: make(chan struct{}, 1), release: make(chan struct{})}
	command := protocol.Command{CommandID: "input-1", RunID: key.RunID, Generation: key.Generation, Kind: "provide_input", Payload: json.RawMessage(`{}`)}
	api := &renewalCommandControl{command: command, renewEntered: make(chan struct{}, 1)}
	daemon := &daemon{
		store:          store,
		control:        api,
		options:        options{clock: func() time.Time { return now }, newID: ids()},
		leaseDuration:  10 * time.Second,
		running:        map[state.RunKey]*runningRun{key: {process: input}},
		slots:          make(chan struct{}, 1),
		commandWake:    make(chan struct{}, 1),
		queuedCommands: make(map[commandKey]*queuedCommand),
	}
	ctx, cancel := context.WithCancel(context.Background())
	var releaseInput sync.Once
	release := func() { releaseInput.Do(func() { close(input.release) }) }
	defer func() {
		release()
		cancel()
	}()
	executorDone := make(chan struct{})
	go func() { daemon.runCommandExecutor(ctx); close(executorDone) }()
	renewed := make(chan struct{})
	go func() { daemon.renewLeases(ctx); close(renewed) }()
	<-api.renewEntered
	<-input.entered
	snapshotDone := make(chan struct{})
	go func() {
		daemon.handleSnapshot(ctx, protocol.RuntimeSnapshot{Commands: []protocol.Command{command}})
		close(snapshotDone)
	}()
	select {
	case <-snapshotDone:
		t.Fatal("snapshot bypassed in-flight command serialization")
	default:
	}
	release()
	<-renewed
	<-snapshotDone
	cancel()
	<-executorDone
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if input.writes != 1 || len(journal.PendingCommandAcknowledgements) != 1 {
		t.Fatalf("stdin writes = %d, acknowledgements = %#v", input.writes, journal.PendingCommandAcknowledgements)
	}
}

func TestBlockedInputDoesNotBlockAnotherRunCommand(t *testing.T) {
	store, first := claimedStore(t)
	defer store.Close()
	second := state.RunKey{RunID: "run-2", Generation: 1}
	if _, err := store.SaveClaimIntent(state.ClaimIntent{Key: second, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1, ClaimID: "claim-2", Work: protocol.Work{Goal: "g"}, WorkspaceBindingKey: "local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveClaimGrant(second, protocol.ClaimResponse{RunID: second.RunID, Generation: second.Generation, ClaimID: "claim-2", LeaseToken: "lease-2", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"}}); err != nil {
		t.Fatal(err)
	}
	blocked := &blockingInputProcess{entered: make(chan struct{}, 1), release: make(chan struct{})}
	other := &recordingProcess{}
	daemon := &daemon{
		store:          store,
		control:        &fakeControl{},
		options:        options{newID: ids()},
		running:        map[state.RunKey]*runningRun{first: {process: blocked}, second: {process: other}},
		slots:          make(chan struct{}, 2),
		commandWake:    make(chan struct{}, 1),
		queuedCommands: make(map[commandKey]*queuedCommand),
	}
	ctx, cancel := context.WithCancel(context.Background())
	executorDone := make(chan struct{})
	go func() { daemon.runCommandExecutor(ctx); close(executorDone) }()
	firstDone := daemon.enqueueCommand(protocol.Command{CommandID: "input-1", RunID: first.RunID, Generation: first.Generation, Kind: "provide_input", Payload: json.RawMessage(`{}`)})
	<-blocked.entered
	secondDone := daemon.enqueueCommand(protocol.Command{CommandID: "input-2", RunID: second.RunID, Generation: second.Generation, Kind: "provide_input", Payload: json.RawMessage(`{}`)})
	secondCompleted := false
	select {
	case <-secondDone:
		secondCompleted = true
	case <-time.After(500 * time.Millisecond):
	}
	close(blocked.release)
	<-firstDone
	if !secondCompleted {
		<-secondDone
	}
	cancel()
	<-executorDone
	if !secondCompleted {
		t.Fatal("blocked input delayed a command for another run")
	}
	if other.writes != 1 {
		t.Fatalf("other run writes = %d, want 1", other.writes)
	}
}

func TestSameRunCommandsPreserveFIFO(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	process := &recordingProcess{}
	daemon := &daemon{
		store:          store,
		control:        &fakeControl{},
		options:        options{newID: ids()},
		running:        map[state.RunKey]*runningRun{key: {process: process}},
		slots:          make(chan struct{}, 1),
		commandWake:    make(chan struct{}, 1),
		queuedCommands: make(map[commandKey]*queuedCommand),
	}
	ctx, cancel := context.WithCancel(context.Background())
	executorDone := make(chan struct{})
	go func() { daemon.runCommandExecutor(ctx); close(executorDone) }()

	firstDone := daemon.enqueueCommand(protocol.Command{CommandID: "input-1", RunID: key.RunID, Generation: key.Generation, Kind: "provide_input", Payload: json.RawMessage(`{"sequence":1}`)})
	secondDone := daemon.enqueueCommand(protocol.Command{CommandID: "input-2", RunID: key.RunID, Generation: key.Generation, Kind: "provide_input", Payload: json.RawMessage(`{"sequence":2}`)})
	<-firstDone
	<-secondDone
	cancel()
	<-executorDone

	firstIndex := bytes.Index(process.input, []byte(`"sequence":1`))
	secondIndex := bytes.Index(process.input, []byte(`"sequence":2`))
	if process.writes != 2 || firstIndex < 0 || secondIndex <= firstIndex {
		t.Fatalf("writes = %d, input = %q, want FIFO input records", process.writes, process.input)
	}
}

func TestSameCommandIDIsScopedToRun(t *testing.T) {
	store, first := claimedStore(t)
	defer store.Close()
	second := state.RunKey{RunID: "run-2", Generation: 1}
	if _, err := store.SaveClaimIntent(state.ClaimIntent{Key: second, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1, ClaimID: "claim-2", Work: protocol.Work{Goal: "g"}, WorkspaceBindingKey: "local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveClaimGrant(second, protocol.ClaimResponse{RunID: second.RunID, Generation: second.Generation, ClaimID: "claim-2", LeaseToken: "lease-2", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"}}); err != nil {
		t.Fatal(err)
	}
	firstProcess := &recordingProcess{}
	secondProcess := &recordingProcess{}
	daemon := &daemon{
		store:          store,
		control:        &fakeControl{},
		options:        options{newID: ids()},
		running:        map[state.RunKey]*runningRun{first: {process: firstProcess}, second: {process: secondProcess}},
		slots:          make(chan struct{}, 2),
		commandWake:    make(chan struct{}, 1),
		queuedCommands: make(map[commandKey]*queuedCommand),
	}
	ctx, cancel := context.WithCancel(context.Background())
	executorDone := make(chan struct{})
	go func() { daemon.runCommandExecutor(ctx); close(executorDone) }()

	firstDone := daemon.enqueueCommand(protocol.Command{CommandID: "shared-id", RunID: first.RunID, Generation: first.Generation, Kind: "provide_input", Payload: json.RawMessage(`{"run":1}`)})
	secondDone := daemon.enqueueCommand(protocol.Command{CommandID: "shared-id", RunID: second.RunID, Generation: second.Generation, Kind: "provide_input", Payload: json.RawMessage(`{"run":2}`)})
	<-firstDone
	<-secondDone
	cancel()
	<-executorDone

	if firstProcess.writes != 1 || secondProcess.writes != 1 {
		t.Fatalf("writes = (%d, %d), want one command per run", firstProcess.writes, secondProcess.writes)
	}
}

func TestCancelPreemptsBlockedInputInSameSnapshot(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	input := &blockingInputProcess{entered: make(chan struct{}, 1), release: make(chan struct{}), terminated: make(chan struct{})}
	daemon := &daemon{
		store:          store,
		control:        &fakeControl{},
		log:            slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options:        options{newID: ids()},
		running:        map[state.RunKey]*runningRun{key: {process: input, claimed: true}},
		slots:          make(chan struct{}, 1),
		commandWake:    make(chan struct{}, 1),
		queuedCommands: make(map[commandKey]*queuedCommand),
	}
	ctx, cancel := context.WithCancel(context.Background())
	executorDone := make(chan struct{})
	go func() { daemon.runCommandExecutor(ctx); close(executorDone) }()
	completions, ok := daemon.enqueueSnapshotCommands([]protocol.Command{
		{CommandID: "input-1", RunID: key.RunID, Generation: key.Generation, Kind: "provide_input", Payload: json.RawMessage(`{}`)},
		{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel", Payload: json.RawMessage(`{}`)},
	})
	if !ok {
		t.Fatal("snapshot commands were not queued")
	}
	snapshotDone := make(chan struct{})
	go func() {
		waitCommandCompletions(ctx, completions)
		close(snapshotDone)
	}()
	preempted := false
	select {
	case <-input.terminated:
		preempted = true
	case <-time.After(500 * time.Millisecond):
	}
	<-completions[1].done
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	terminalQueued := journal.LocalState == "terminal_pending" && journal.TerminalState == "cancelled"
	cancelAcknowledged := false
	for _, acknowledgement := range journal.PendingCommandAcknowledgements {
		if acknowledgement.CommandID == "cancel-1" && acknowledgement.Outcome == "applied" {
			cancelAcknowledged = true
		}
	}
	close(input.release)
	<-snapshotDone
	cancel()
	<-executorDone
	if !preempted || !terminalQueued || !cancelAcknowledged {
		t.Fatalf("cancel preemption = %t, terminal queued = %t, cancel acknowledged = %t", preempted, terminalQueued, cancelAcknowledged)
	}
}

func TestCancelRejectsQueuedInputAfterPreemption(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	input := &blockingInputProcess{entered: make(chan struct{}, 1), release: make(chan struct{}), terminated: make(chan struct{})}
	daemon := &daemon{
		store:          store,
		control:        &fakeControl{},
		log:            slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options:        options{newID: ids()},
		running:        map[state.RunKey]*runningRun{key: {process: input, claimed: true}},
		slots:          make(chan struct{}, 1),
		commandWake:    make(chan struct{}, 1),
		queuedCommands: make(map[commandKey]*queuedCommand),
	}
	ctx, cancel := context.WithCancel(context.Background())
	executorDone := make(chan struct{})
	go func() { daemon.runCommandExecutor(ctx); close(executorDone) }()

	firstDone := daemon.enqueueCommand(protocol.Command{CommandID: "input-1", RunID: key.RunID, Generation: key.Generation, Kind: "provide_input", Payload: json.RawMessage(`{"answer":"first"}`)})
	<-input.entered
	secondDone := daemon.enqueueCommand(protocol.Command{CommandID: "input-2", RunID: key.RunID, Generation: key.Generation, Kind: "provide_input", Payload: json.RawMessage(`{"answer":"second"}`)})
	cancelDone := daemon.enqueueCommand(protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel", Payload: json.RawMessage(`{}`)})
	<-input.terminated
	close(input.release)
	<-firstDone
	<-secondDone
	<-cancelDone
	cancel()
	<-executorDone

	if input.writes != 1 {
		t.Fatalf("WriteInput calls after cancellation = %d, want 1", input.writes)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	outcomes := make(map[string]string, len(journal.PendingCommandAcknowledgements))
	for _, acknowledgement := range journal.PendingCommandAcknowledgements {
		outcomes[acknowledgement.CommandID] = acknowledgement.Outcome
	}
	if outcomes["input-2"] != "rejected" {
		t.Fatalf("queued input outcome = %q, want rejected", outcomes["input-2"])
	}
	if outcomes["input-1"] != "applied" {
		t.Fatalf("in-flight input outcome = %q, want applied after successful write", outcomes["input-1"])
	}
}

func TestScheduledSnapshotCancelPreemptsBlockedInputAndJoins(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	if _, err := store.UpdateLeaseExpiry(key, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	input := &blockingInputProcess{entered: make(chan struct{}, 1), release: make(chan struct{}), terminated: make(chan struct{})}
	daemon := &daemon{
		store:         store,
		control:       &terminalRetryControl{transitionErr: transportError("offline")},
		log:           slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options:       options{clock: func() time.Time { return now }, newID: ids(), newTimer: blockedTimerFactory},
		leaseDuration: 120 * time.Second,
		running:       map[state.RunKey]*runningRun{key: {process: input, claimed: true}},
		slots:         make(chan struct{}, 1),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := daemon.startBackground(ctx)

	daemon.scheduleSnapshot(protocol.RuntimeSnapshot{Commands: []protocol.Command{{CommandID: "input-1", RunID: key.RunID, Generation: key.Generation, Kind: "provide_input", Payload: json.RawMessage(`{}`)}}})
	<-input.entered
	daemon.scheduleSnapshot(protocol.RuntimeSnapshot{Commands: []protocol.Command{{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel", Payload: json.RawMessage(`{}`)}}})
	select {
	case <-input.terminated:
	case <-time.After(time.Second):
		t.Fatal("scheduled cancel did not preempt blocked input")
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "terminal_pending" || len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].CommandID != "cancel-1" {
		t.Fatalf("scheduled cancellation journal = %#v", journal)
	}

	close(input.release)
	joined := make(chan struct{})
	go func() {
		done()
		close(joined)
	}()
	select {
	case <-joined:
	case <-time.After(time.Second):
		t.Fatal("background shutdown did not join command and snapshot workers")
	}
}

func TestBlockedCommandDoesNotDelayUnrelatedScheduledAssignment(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	input := &blockingInputProcess{entered: make(chan struct{}, 1), release: make(chan struct{})}
	started := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())
	daemon := &daemon{
		config:    testConfig(t),
		store:     store,
		control:   &fakeControl{},
		workspace: &fakeWorkspace{},
		start: func(context.Context, execution.Invocation, execution.Sink) (Process, error) {
			started <- struct{}{}
			return fakeProcess{}, nil
		},
		options:        options{newID: ids(), clock: time.Now},
		runtimeID:      "runtime-1",
		runtimeEpoch:   1,
		running:        map[state.RunKey]*runningRun{key: {process: input, claimed: true}},
		slots:          make(chan struct{}, 2),
		background:     ctx,
		commandWake:    make(chan struct{}, 1),
		queuedCommands: make(map[commandKey]*queuedCommand),
	}
	executorDone := make(chan struct{})
	go func() { daemon.runCommandExecutor(ctx); close(executorDone) }()
	var release sync.Once
	defer func() {
		release.Do(func() { close(input.release) })
		cancel()
		<-executorDone
		daemon.snapshotWG.Wait()
		daemon.workers.Wait()
	}()

	daemon.scheduleSnapshot(protocol.RuntimeSnapshot{
		Commands:    []protocol.Command{{CommandID: "input-1", RunID: key.RunID, Generation: key.Generation, Kind: "provide_input", Payload: json.RawMessage(`{}`)}},
		Assignments: []protocol.Assignment{{RunID: "run-2", Generation: 1, Work: protocol.Work{Goal: "g"}}},
	})
	<-input.entered
	select {
	case <-started:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("blocked command delayed an unrelated assignment")
	}
	release.Do(func() { close(input.release) })
}

func TestReleaseRunUnblocksCommandWaiters(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	input := &blockingInputProcess{entered: make(chan struct{}, 1), release: make(chan struct{})}
	daemon := &daemon{
		store:          store,
		control:        &fakeControl{},
		options:        options{newID: ids()},
		running:        map[state.RunKey]*runningRun{key: {process: input}},
		slots:          make(chan struct{}, 1),
		commandWake:    make(chan struct{}, 1),
		queuedCommands: make(map[commandKey]*queuedCommand),
	}
	ctx, cancel := context.WithCancel(context.Background())
	executorDone := make(chan struct{})
	go func() { daemon.runCommandExecutor(ctx); close(executorDone) }()
	command := protocol.Command{CommandID: "input-1", RunID: key.RunID, Generation: key.Generation, Kind: "provide_input", Payload: json.RawMessage(`{}`)}
	firstDone := daemon.enqueueCommand(command)
	<-input.entered
	duplicateDone := daemon.enqueueCommand(command)

	daemon.releaseRun(key)
	for name, completion := range map[string]<-chan struct{}{"original": firstDone, "duplicate": duplicateDone} {
		select {
		case <-completion:
		case <-time.After(time.Second):
			t.Fatalf("%s command waiter remained blocked after run release", name)
		}
	}
	close(input.release)
	cancel()
	<-executorDone
}

func TestDeliveredCommandReceiptSuppressesStaleSnapshotUntilRunEnds(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	process := &recordingProcess{}
	command := protocol.Command{CommandID: "input-1", RunID: key.RunID, Generation: key.Generation, Kind: "provide_input", Payload: json.RawMessage(`{}`)}
	daemon := &daemon{
		store:          store,
		control:        &fakeControl{},
		options:        options{newID: ids()},
		running:        map[state.RunKey]*runningRun{key: {process: process}},
		slots:          make(chan struct{}, 1),
		commandWake:    make(chan struct{}, 1),
		queuedCommands: make(map[commandKey]*queuedCommand),
	}
	ctx, cancel := context.WithCancel(context.Background())
	executorDone := make(chan struct{})
	go func() { daemon.runCommandExecutor(ctx); close(executorDone) }()
	daemon.handleSnapshot(ctx, protocol.RuntimeSnapshot{Commands: []protocol.Command{command}})
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(ctx, journal); err != nil {
		t.Fatal(err)
	}
	daemon.handleSnapshot(ctx, protocol.RuntimeSnapshot{Commands: []protocol.Command{command}})
	if process.writes != 1 {
		t.Fatalf("WriteInput calls after delivered acknowledgement = %d, want 1", process.writes)
	}
	daemon.releaseRun(key)
	if len(daemon.queuedCommands) != 0 {
		t.Fatalf("completed command receipts survived run release: %#v", daemon.queuedCommands)
	}
	cancel()
	select {
	case <-executorDone:
	case <-time.After(time.Second):
		t.Fatal("command executor did not exit")
	}
}

func TestCancelledSnapshotDoesNotLaunchAssignmentsBehindBlockedCommand(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	input := &blockingInputProcess{entered: make(chan struct{}, 1), release: make(chan struct{})}
	started := make(chan struct{}, 1)
	command := protocol.Command{CommandID: "input-1", RunID: key.RunID, Generation: key.Generation, Kind: "provide_input", Payload: json.RawMessage(`{}`)}
	daemon := &daemon{
		config:    testConfig(t),
		store:     store,
		control:   &fakeControl{},
		workspace: &fakeWorkspace{},
		start: func(context.Context, execution.Invocation, execution.Sink) (Process, error) {
			started <- struct{}{}
			return fakeProcess{}, nil
		},
		options:        options{newID: ids()},
		running:        map[state.RunKey]*runningRun{key: {process: input}},
		slots:          make(chan struct{}, 1),
		commandWake:    make(chan struct{}, 1),
		queuedCommands: make(map[commandKey]*queuedCommand),
	}
	ctx, cancel := context.WithCancel(context.Background())
	executorDone := make(chan struct{})
	go func() { daemon.runCommandExecutor(ctx); close(executorDone) }()
	snapshotDone := make(chan struct{})
	go func() {
		daemon.handleSnapshot(ctx, protocol.RuntimeSnapshot{
			Commands:    []protocol.Command{command},
			Assignments: []protocol.Assignment{{RunID: "run-2", Generation: 1, Work: protocol.Work{Goal: "g"}}},
		})
		close(snapshotDone)
	}()
	<-input.entered
	cancel()
	select {
	case <-snapshotDone:
	case <-time.After(time.Second):
		t.Fatal("cancelled snapshot stayed blocked behind command execution")
	}
	select {
	case <-started:
		t.Fatal("cancelled snapshot started an assignment")
	default:
	}
	close(input.release)
	select {
	case <-executorDone:
	case <-time.After(time.Second):
		t.Fatal("command executor did not join after blocked input released")
	}
}

func TestTerminalOutboxBackoffSuppressesPermanentFailuresAndResets(t *testing.T) {
	t.Run("retry after delays terminal delivery no later than grace", func(t *testing.T) {
		store, key := claimedStore(t)
		defer store.Close()
		now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
		if _, err := store.UpdateLeaseExpiry(key, now); err != nil {
			t.Fatal(err)
		}
		if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}, now); err != nil {
			t.Fatal(err)
		}
		api := &terminalRetryControl{transitionErr: &control.APIError{StatusCode: http.StatusTooManyRequests, Code: control.RateLimited, RetryAfter: 10 * time.Minute}}
		loop := &daemon{store: store, control: api, options: options{clock: func() time.Time { return now }}, outboxRetry: make(map[state.RunKey]outboxRetry)}
		loop.flushAll(context.Background())
		loop.flushAll(context.Background())
		if api.transitionCalls != 1 {
			t.Fatalf("terminal transition calls before Retry-After = %d, want 1", api.transitionCalls)
		}
		entry := loop.outboxRetry[key]
		if want := now.Add(terminalGrace); !entry.retryAt.Equal(want) {
			t.Fatalf("retry at = %s, want delivery deadline %s", entry.retryAt, want)
		}
		now = now.Add(terminalGrace)
		loop.flushAll(context.Background())
		if api.transitionCalls != 2 {
			t.Fatalf("terminal transition calls at grace = %d, want 2", api.transitionCalls)
		}
	})

	t.Run("delivery deadline follows lease rather than later terminal entry", func(t *testing.T) {
		store, key := claimedStore(t)
		defer store.Close()
		leaseExpiry := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
		entryAt := leaseExpiry.Add(time.Minute)
		if _, err := store.UpdateLeaseExpiry(key, leaseExpiry); err != nil {
			t.Fatal(err)
		}
		if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}, entryAt); err != nil {
			t.Fatal(err)
		}
		now := entryAt
		api := &terminalRetryControl{transitionErr: &control.APIError{StatusCode: http.StatusTooManyRequests, Code: control.RateLimited, RetryAfter: 10 * time.Minute}}
		loop := &daemon{store: store, control: api, options: options{clock: func() time.Time { return now }}, outboxRetry: make(map[state.RunKey]outboxRetry)}
		loop.flushAll(context.Background())
		if want := leaseExpiry.Add(terminalGrace); !loop.outboxRetry[key].retryAt.Equal(want) {
			t.Fatalf("retry at = %s, want lease delivery deadline %s", loop.outboxRetry[key].retryAt, want)
		}
		now = leaseExpiry.Add(terminalGrace + time.Second)
		loop.flushAll(context.Background())
		if got := loop.outboxRetry[key].retryAt; !got.Equal(now.Add(10 * time.Minute)) {
			t.Fatalf("expired delivery deadline kept retry capped at %s", got)
		}
	})

	t.Run("permanent failure is suppressed until journal changes or restart", func(t *testing.T) {
		store, key := claimedStore(t)
		defer store.Close()
		now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
		if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}, now); err != nil {
			t.Fatal(err)
		}
		api := &terminalRetryControl{transitionErr: &control.APIError{StatusCode: http.StatusBadRequest, Code: control.InvalidRequest}}
		loop := &daemon{store: store, control: api, options: options{clock: func() time.Time { return now }}, outboxRetry: make(map[state.RunKey]outboxRetry)}
		loop.flushAll(context.Background())
		loop.flushAll(context.Background())
		if api.transitionCalls != 1 {
			t.Fatalf("permanent terminal calls = %d, want 1", api.transitionCalls)
		}
		if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "cancelled-1", State: "cancelled", Payload: json.RawMessage(`{}`)}, now); err != nil {
			t.Fatal(err)
		}
		loop.flushAll(context.Background())
		if api.transitionCalls != 2 {
			t.Fatalf("changed terminal journal calls = %d, want 2", api.transitionCalls)
		}
		restarted := &daemon{store: store, control: api, options: options{clock: func() time.Time { return now }}, outboxRetry: make(map[state.RunKey]outboxRetry)}
		restarted.flushAll(context.Background())
		if api.transitionCalls != 3 {
			t.Fatalf("restarted terminal calls = %d, want 3", api.transitionCalls)
		}
	})

	t.Run("malformed terminal response is permanent", func(t *testing.T) {
		store, key := claimedStore(t)
		defer store.Close()
		now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
		if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}, now); err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			_, _ = writer.Write([]byte(`{}`))
		}))
		defer server.Close()
		client, err := control.NewClient(server.URL+"/api", "machine-token", server.Client())
		if err != nil {
			t.Fatal(err)
		}
		api := &responseErrorTerminalControl{client: client}
		loop := &daemon{store: store, control: api, options: options{clock: func() time.Time { return now }}, outboxRetry: make(map[state.RunKey]outboxRetry)}
		loop.flushAll(context.Background())
		loop.flushAll(context.Background())
		if api.transitionCalls != 1 {
			t.Fatalf("malformed terminal response calls = %d, want 1", api.transitionCalls)
		}
	})
}

func TestOrdinaryOutboxFailuresRemainCadenceDriven(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	if _, err := store.QueueEvent(key, protocol.RunEvent{EventID: "event-1", Sequence: 1, Kind: "progress", OccurredAt: time.Now(), Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	api := &ordinaryOutboxFailureControl{}
	loop := &daemon{store: store, control: api, options: options{clock: time.Now}, outboxRetry: make(map[state.RunKey]outboxRetry)}
	loop.flushAll(context.Background())
	loop.flushAll(context.Background())
	if api.eventCalls != 2 {
		t.Fatalf("ordinary event calls = %d, want 2", api.eventCalls)
	}
	if len(loop.outboxRetry) != 0 {
		t.Fatalf("ordinary outbox created terminal retry state: %#v", loop.outboxRetry)
	}
}

func TestLargePersistedEventBacklogDrainsAfterNoContentAcknowledgement(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()

	payload := append([]byte{'"'}, bytes.Repeat([]byte("x"), (1<<20)+1)...)
	payload = append(payload, '"')
	journal, err := store.QueueEvent(key, protocol.RunEvent{
		EventID:    "event-1",
		Sequence:   1,
		Kind:       "output",
		OccurredAt: time.Date(2026, time.September, 3, 1, 2, 3, 0, time.UTC),
		Payload:    json.RawMessage(payload),
	})
	if err != nil {
		t.Fatal(err)
	}
	serializedEvents, err := json.Marshal(journal.PendingEvents)
	if err != nil {
		t.Fatal(err)
	}
	if len(serializedEvents) <= 1<<20 {
		t.Fatalf("serialized event backlog = %d bytes, want more than %d", len(serializedEvents), 1<<20)
	}

	var calls int
	var requestBytes int64
	var method string
	var path string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		method = request.Method
		path = request.URL.Path
		requestBytes, _ = io.Copy(io.Discard, request.Body)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := control.NewClient(server.URL+"/api", "machine-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}

	daemon := &daemon{store: store, control: client}
	if err := daemon.flushRun(context.Background(), journal); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("append calls = %d, want 1", calls)
	}
	if method != http.MethodPost || path != "/api/v1/runs/run-1/events" {
		t.Fatalf("append request = %s %s, want POST /api/v1/runs/run-1/events", method, path)
	}
	if requestBytes <= 1<<20 {
		t.Fatalf("append request = %d bytes, want more than %d", requestBytes, 1<<20)
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingEvents) != 0 {
		t.Fatalf("pending events = %#v, want none after 204 acknowledgement", journal.PendingEvents)
	}
}

func TestAllDaemonControlRPCsUseBoundedCancelledContexts(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	if _, err := store.UpdateLeaseExpiry(key, now.Add(leaseSafetyMargin+time.Second)); err != nil {
		t.Fatal(err)
	}
	api := &deadlineRecordingControl{}
	daemon := &daemon{
		config:        testConfig(t),
		store:         store,
		control:       api,
		workspace:     &fakeWorkspace{},
		options:       options{clock: func() time.Time { return now }, newID: ids()},
		runtimeID:     "runtime-1",
		runtimeEpoch:  1,
		leaseDuration: 10 * time.Second,
		running:       map[state.RunKey]*runningRun{key: {process: fakeProcess{}}},
		slots:         make(chan struct{}, 1),
	}
	daemon.heartbeat(context.Background())
	daemon.sync(context.Background())
	daemon.reconcile(context.Background())
	if _, err := daemon.claimWithRetry(context.Background(), "run-claim", protocol.ClaimRequest{RuntimeID: "runtime-1", RuntimeEpoch: 1, Generation: 1, ClaimID: "claim-2"}); err != nil {
		t.Fatal(err)
	}
	daemon.renewLeases(context.Background())
	if err := daemon.queueEvent(key, "progress", json.RawMessage(`{}`), now); err != nil {
		t.Fatal(err)
	}
	if err := daemon.queueTransition(key, "running", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	daemon.queueCommandAcknowledgement(key, "command-1", "applied")
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := daemon.flushRun(context.Background(), journal); err != nil {
		t.Fatal(err)
	}
	api.assertBoundedAndCancelled(t)
}

func TestEnrollmentAndRegistrationUseBoundedCancelledContexts(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	t.Setenv("SYMMETRY_ENROLLMENT_TOKEN", "enrollment-token")
	enrollment := &deadlineRecordingEnrollment{}
	api := &deadlineRecordingControl{}
	daemon := &daemon{config: testConfig(t), log: slog.New(slog.NewJSONHandler(io.Discard, nil)), options: options{store: store, control: api, enrollment: enrollment, workspace: &fakeWorkspace{}, start: failStart, clock: time.Now, newID: ids()}, running: make(map[state.RunKey]*runningRun)}
	if err := daemon.initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	enrollment.assertBoundedAndCancelled(t)
	api.assertBoundedAndCancelled(t)
}

func TestLivenessExitsOnShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	timers := make(chan *manualDeadlineTimer, 1)
	daemon := &daemon{options: options{newTimer: func(delay time.Duration) deadlineTimer {
		timer := &manualDeadlineTimer{channel: make(chan time.Time), delay: delay}
		timers <- timer
		return timer
	}}, store: store, control: &fakeControl{}}
	done := make(chan struct{})
	go func() { daemon.runLiveness(ctx); close(done) }()
	<-timers
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("liveness did not exit after shutdown")
	}
}

func TestClaimWithRetryStopsAfterPermanentError(t *testing.T) {
	api := &fakeControl{claimErr: &control.APIError{StatusCode: http.StatusBadRequest, Code: control.InvalidRequest}}
	daemon := &daemon{control: api}
	_, err := daemon.claimWithRetry(context.Background(), "run-1", protocol.ClaimRequest{Generation: 1, ClaimID: "claim-1"})
	if err == nil || api.claimCalls != 1 {
		t.Fatalf("claim error = %v, calls = %d", err, api.claimCalls)
	}
}

func TestClaimWithRetryHonorsRetryAfter(t *testing.T) {
	api := &fakeControl{claimErrors: []error{&control.APIError{StatusCode: http.StatusTooManyRequests, Code: control.RateLimited, RetryAfter: time.Nanosecond}}}
	daemon := &daemon{control: api}
	claim, err := daemon.claimWithRetry(context.Background(), "run-1", protocol.ClaimRequest{Generation: 1, ClaimID: "claim-1"})
	if err != nil || api.claimCalls != 2 || claim.ClaimID != "claim-1" {
		t.Fatalf("claim = %#v, error = %v, calls = %d", claim, err, api.claimCalls)
	}
}

func TestClaimRetryStopsAtAssignmentExpiry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Retry-After", "0")
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte(`{"error":{"code":"rate_limited","message":"retry"}}`))
	}))
	defer server.Close()
	client, err := control.NewClient(server.URL+"/api", "machine-token", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	api := &zeroRetryClaimControl{client: client}
	daemon := &daemon{control: api, options: options{clock: func() time.Time { return now }}}
	expiresAt := now.Add(minimumInterval)
	var observed time.Duration
	_, err = daemon.claimWithRetryUntil(context.Background(), "run-1", protocol.ClaimRequest{Generation: 1, ClaimID: "claim-1"}, expiresAt, func(_ context.Context, delay time.Duration) error {
		observed = delay
		now = now.Add(delay)
		return nil
	})
	if !errors.Is(err, errAssignmentExpired) || api.calls != 1 || observed != minimumInterval {
		t.Fatalf("claim error = %v, calls = %d, delay = %s", err, api.calls, observed)
	}
}

func TestClaimAcceptsSuccessfulResponseAfterLocalAssignmentDeadline(t *testing.T) {
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	expiresAt := now.Add(time.Second)
	api := &lateSuccessfulClaimControl{advance: func() { now = expiresAt.Add(time.Nanosecond) }}
	daemon := &daemon{control: api, options: options{clock: func() time.Time { return now }}}

	claim, err := daemon.claimWithRetryUntil(context.Background(), "run-1", protocol.ClaimRequest{Generation: 1, ClaimID: "claim-1"}, expiresAt, waitForRetry)
	if err != nil || claim.LeaseToken != "lease" {
		t.Fatalf("claim = %#v, error = %v, want authoritative success", claim, err)
	}
}

func TestClaimSkipsRequestWhenAssignmentAlreadyExpired(t *testing.T) {
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	api := &lateSuccessfulClaimControl{advance: func() {}}
	daemon := &daemon{control: api, options: options{clock: func() time.Time { return now }}}

	_, err := daemon.claimWithRetryUntil(context.Background(), "run-1", protocol.ClaimRequest{Generation: 1, ClaimID: "claim-1"}, now, waitForRetry)
	if !errors.Is(err, errAssignmentExpired) || api.calls != 0 {
		t.Fatalf("claim error = %v, calls = %d, want expired without request", err, api.calls)
	}
}

func TestCancelWinsCompletionAndFlushesAcknowledgement(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	process := fakeProcess{result: execution.Result{ExitCode: 0}}
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
			process:  process,
			prepared: workspace.Prepared{Path: "C:\\workspace", Run: workspace.RunRef{RunID: key.RunID, Generation: key.Generation}},
			claimed:  true,
			slotHeld: true,
		}},
		slots: slots,
	}
	daemon.waitForRun(key)
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingTransitions) != 1 || journal.PendingTransitions[0].State != "completed" {
		t.Fatalf("completed journal = %#v", journal)
	}
	daemon.handleCommand(context.Background(), protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"})
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingTransitions) != 1 || journal.PendingTransitions[0].State != "cancelled" || len(journal.PendingCommandAcknowledgements) != 1 {
		t.Fatalf("terminal journal = %#v", journal)
	}
	persistWorkspacePath(t, store, key, "C:\\workspace")
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	daemon.flushRun(context.Background(), journal)
	if _, err := store.LoadJournal(key); !state.IsNotFound(err) {
		t.Fatalf("journal = %v, want deleted", err)
	}
	if len(slots) != 0 {
		t.Fatal("slot was not released after cancelled terminal flush")
	}
	select {
	case succeeded := <-cleaned:
		if succeeded {
			t.Fatal("cancelled cleanup used successful process exit instead of terminal state")
		}
	default:
		t.Fatal("cancelled workspace was not cleaned")
	}
}

func TestInitialInputUsesLocalModeAndPreservesGoalAndStructuredInput(t *testing.T) {
	jsonInput, err := initialInput(config.AgentProfile{InputMode: config.InputModeJSON}, protocol.Work{Goal: "implement feature", Input: []byte(`{"mode":"review"}`)})
	if err != nil {
		t.Fatal(err)
	}
	var payload protocol.AgentInputRecord
	if err := json.Unmarshal(jsonInput, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Type != protocol.AgentInputRecordTaskInput || payload.Goal != "implement feature" || string(payload.Input) != `{"mode":"review"}` {
		t.Fatalf("JSON input = %s", jsonInput)
	}
	omittedInput, err := initialInput(config.AgentProfile{InputMode: config.InputModeJSON}, protocol.Work{Goal: "implement feature"})
	if err != nil || string(omittedInput) != "{\"type\":\"task_input\",\"goal\":\"implement feature\",\"input\":null}\n" {
		t.Fatalf("omitted JSON input = %q, %v", omittedInput, err)
	}
	emptyInput, err := initialInput(config.AgentProfile{InputMode: config.InputModeJSON}, protocol.Work{Goal: "implement feature", Input: json.RawMessage(`{}`)})
	if err != nil || string(emptyInput) != "{\"type\":\"task_input\",\"goal\":\"implement feature\",\"input\":{}}\n" {
		t.Fatalf("empty JSON input = %q, %v", emptyInput, err)
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

func TestScheduledCancelDuringClaimCompletesAfterDurableReceipt(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	gate := make(chan struct{})
	entered := make(chan struct{}, 1)
	control := &fakeControl{claimBlock: gate, claimEntered: entered}
	daemon := &daemon{
		config:         testConfig(t),
		store:          store,
		control:        control,
		log:            slog.New(slog.NewJSONHandler(io.Discard, nil)),
		workspace:      &fakeWorkspace{},
		start:          failStart,
		options:        options{newID: ids(), clock: time.Now},
		runtimeID:      "runtime-1",
		runtimeEpoch:   1,
		running:        make(map[state.RunKey]*runningRun),
		slots:          make(chan struct{}, 1),
		commandWake:    make(chan struct{}, 1),
		queuedCommands: make(map[commandKey]*queuedCommand),
	}
	ctx := context.Background()
	key := state.RunKey{RunID: "run-1", Generation: 1}
	daemon.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "g"}})
	<-entered
	completion := daemon.enqueueCommand(protocol.Command{CommandID: "cancel-1", RunID: key.RunID, Generation: key.Generation, Kind: "cancel"})
	queued, ok := daemon.nextQueuedCommand()
	if !ok {
		t.Fatal("cancel command was not queued")
	}
	daemon.dispatchQueuedCommand(ctx, queued)
	select {
	case <-completion:
		t.Fatal("cancel command completed before its durable acknowledgement")
	case <-time.After(100 * time.Millisecond):
	}

	close(gate)
	daemon.workers.Wait()
	select {
	case <-completion:
	case <-time.After(time.Second):
		t.Fatal("cancel command did not complete after durable acknowledgement")
	}
	daemon.commandWG.Wait()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(journal.PendingTransitions) != 1 || journal.PendingTransitions[0].State != "cancelled" || len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].CommandID != "cancel-1" {
		t.Fatalf("post-grant journal = %#v", journal)
	}
}

func TestTransientClaimRetryUsesPersistedClaimID(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	control := &fakeControl{claimErrors: []error{transportError("network interrupted")}}
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
		if _, err := store.UpdateLeaseExpiry(key, now.Add(leaseSafetyMargin+time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	release := make(chan struct{})
	commandWake := make(chan struct{}, 1)
	api := &parallelRenewControl{
		started: make(chan string, 2),
		release: release,
		secondCommands: []protocol.Command{{
			CommandID:  "input-2",
			RunID:      second.RunID,
			Generation: second.Generation,
			Kind:       "provide_input",
			Payload:    json.RawMessage(`{}`),
		}},
	}
	daemon := &daemon{
		store:          store,
		control:        api,
		options:        options{clock: func() time.Time { return now }},
		leaseDuration:  10 * time.Second,
		slots:          make(chan struct{}, 2),
		running:        map[state.RunKey]*runningRun{first: {process: fakeProcess{}}, second: {process: fakeProcess{}}},
		commandWake:    commandWake,
		queuedCommands: make(map[commandKey]*queuedCommand),
	}
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
	secondProcessed := false
	select {
	case <-commandWake:
		secondProcessed = true
	case <-time.After(time.Second):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("renewal loop did not complete")
	}
	if !secondProcessed {
		t.Fatal("completed renewal waited for an unrelated stalled renewal")
	}
}

func TestRenewLeasesJoinsWorkersAfterShutdown(t *testing.T) {
	store, first := claimedStore(t)
	defer store.Close()
	second := state.RunKey{RunID: "run-2", Generation: 1}
	if _, err := store.SaveClaimIntent(state.ClaimIntent{Key: second, RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1, ClaimID: "claim-2", Work: protocol.Work{Goal: "g"}, WorkspaceBindingKey: "local"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveClaimGrant(second, protocol.ClaimResponse{RunID: second.RunID, Generation: second.Generation, ClaimID: "claim-2", LeaseToken: "lease-2", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"}}); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	for _, key := range []state.RunKey{first, second} {
		if _, err := store.UpdateLeaseExpiry(key, now.Add(leaseSafetyMargin+time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	api := &shutdownRenewControl{
		started:         make(chan string, 2),
		firstReturned:   make(chan struct{}, 1),
		secondCancelled: make(chan struct{}, 1),
		releaseSecond:   make(chan struct{}),
	}
	daemon := &daemon{
		store:         store,
		control:       api,
		options:       options{clock: func() time.Time { return now }},
		leaseDuration: 10 * time.Second,
		slots:         make(chan struct{}, 2),
		running:       map[state.RunKey]*runningRun{first: {process: fakeProcess{}}, second: {process: fakeProcess{}}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { daemon.renewLeases(ctx); close(done) }()
	for range 2 {
		select {
		case <-api.started:
		case <-time.After(time.Second):
			cancel()
			close(api.releaseSecond)
			t.Fatal("renewal worker did not start")
		}
	}
	cancel()
	<-api.firstReturned
	<-api.secondCancelled
	returnedEarly := false
	select {
	case <-done:
		returnedEarly = true
	case <-time.After(200 * time.Millisecond):
	}
	close(api.releaseSecond)
	if !returnedEarly {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("renewal loop did not join cancelled workers")
		}
	}
	if returnedEarly {
		t.Fatal("renewal loop returned before cancelled workers joined")
	}
}

func TestDelayedReconcileCannotRegressRenewedLease(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	initialExpiry := now.Add(leaseSafetyMargin + time.Second)
	if _, err := store.UpdateLeaseExpiry(key, initialExpiry); err != nil {
		t.Fatal(err)
	}
	oldReconcileExpiry := now.Add(10 * time.Second)
	renewedExpiry := now.Add(time.Minute)
	api := &reconcileRenewalRaceControl{
		reconcileStarted: make(chan struct{}, 1),
		releaseReconcile: make(chan struct{}),
		reconcileExpiry:  oldReconcileExpiry,
		renewedExpiry:    renewedExpiry,
	}
	daemon := &daemon{
		store:         store,
		control:       api,
		log:           slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options:       options{clock: func() time.Time { return now }},
		runtimeID:     "runtime-1",
		runtimeEpoch:  1,
		leaseDuration: 10 * time.Second,
		slots:         make(chan struct{}, 1),
		running:       map[state.RunKey]*runningRun{key: {process: fakeProcess{}}},
	}
	reconciled := make(chan struct{})
	go func() {
		daemon.reconcile(context.Background())
		close(reconciled)
	}()
	<-api.reconcileStarted
	daemon.renewLeases(context.Background())
	close(api.releaseReconcile)
	<-reconciled

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.LeaseExpiresAt.Equal(renewedExpiry) {
		t.Fatalf("lease expiry = %s, want renewed expiry %s", journal.LeaseExpiresAt, renewedExpiry)
	}
}

func TestDelayedReconcileCannotAdvanceTerminalLease(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	originalExpiry := now.Add(time.Minute)
	if _, err := store.UpdateLeaseExpiry(key, originalExpiry); err != nil {
		t.Fatal(err)
	}
	api := &reconcileRenewalRaceControl{
		reconcileStarted: make(chan struct{}, 1),
		releaseReconcile: make(chan struct{}),
		reconcileExpiry:  now.Add(time.Hour),
	}
	daemon := &daemon{
		store:        store,
		control:      api,
		log:          slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options:      options{clock: func() time.Time { return now }, newID: ids()},
		runtimeID:    "runtime-1",
		runtimeEpoch: 1,
		running:      map[state.RunKey]*runningRun{key: {process: fakeProcess{}}},
		slots:        make(chan struct{}, 1),
	}
	reconciled := make(chan struct{})
	go func() {
		daemon.reconcile(context.Background())
		close(reconciled)
	}()
	<-api.reconcileStarted
	if err := daemon.queueTerminalTransition(key, "completed", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	terminal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	close(api.releaseReconcile)
	<-reconciled

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.LeaseExpiresAt.Equal(originalExpiry) || !journal.TerminalPendingAt.Equal(terminal.TerminalPendingAt) {
		t.Fatalf("delayed reconcile advanced terminal lease: %#v", journal)
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

func persistWorkspacePath(t *testing.T, store *state.Store, key state.RunKey, path string) {
	t.Helper()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	journal.WorkspacePath = path
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
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

func transportError(message string) error {
	return &url.Error{Op: "POST", URL: "https://control.example.test", Err: &net.DNSError{Err: message, IsTemporary: true}}
}

type fakeEnrollment struct {
	calls         int
	beforeReturn  func()
	responseToken string
}

func (client *fakeEnrollment) Enroll(_ context.Context, _, _ string, request protocol.EnrollRequest) (protocol.EnrollResponse, error) {
	client.calls++
	if client.beforeReturn != nil {
		client.beforeReturn()
	}
	responseToken := request.MachineToken
	if client.responseToken != "" {
		responseToken = client.responseToken
	}
	return protocol.EnrollResponse{MachineID: "machine-1", MachineToken: responseToken}, nil
}

type enrollmentCall struct {
	idempotencyKey string
	request        protocol.EnrollRequest
}

type replayEnrollment struct {
	requests chan enrollmentCall
	fail     func() error
}

func (client *replayEnrollment) Enroll(_ context.Context, _ string, idempotencyKey string, request protocol.EnrollRequest) (protocol.EnrollResponse, error) {
	client.requests <- enrollmentCall{idempotencyKey: idempotencyKey, request: request}
	if client.fail != nil {
		fail := client.fail
		client.fail = nil
		return protocol.EnrollResponse{}, fail()
	}
	return protocol.EnrollResponse{MachineID: "machine-1", MachineToken: request.MachineToken}, nil
}

type contextRecorder struct {
	contexts []context.Context
	observed []time.Time
	limits   []time.Duration
}

func (recorder *contextRecorder) record(ctx context.Context, limit time.Duration) {
	recorder.contexts = append(recorder.contexts, ctx)
	recorder.observed = append(recorder.observed, time.Now())
	recorder.limits = append(recorder.limits, limit)
}

func (recorder *contextRecorder) assertBoundedAndCancelled(t *testing.T) {
	t.Helper()
	if len(recorder.contexts) == 0 {
		t.Fatal("no control request contexts were recorded")
	}
	for index, ctx := range recorder.contexts {
		deadline, ok := ctx.Deadline()
		remaining := deadline.Sub(recorder.observed[index])
		limit := recorder.limits[index]
		if !ok || remaining > limit || remaining < limit-time.Second {
			t.Fatalf("request %d deadline = %v, want a fresh deadline no later than %s", index, deadline, limit)
		}
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("request %d context error = %v, want context cancellation after call", index, ctx.Err())
		}
	}
}

type deadlineRecordingEnrollment struct{ contextRecorder }

func (client *deadlineRecordingEnrollment) Enroll(ctx context.Context, _, _ string, request protocol.EnrollRequest) (protocol.EnrollResponse, error) {
	client.record(ctx, controlRequestLimit)
	return protocol.EnrollResponse{MachineID: "machine-1", MachineToken: request.MachineToken}, nil
}

type manualDeadlineTimer struct {
	channel chan time.Time
	delay   time.Duration
}

func (timer *manualDeadlineTimer) Chan() <-chan time.Time { return timer.channel }
func (*manualDeadlineTimer) Stop()                        {}

type blockingInputProcess struct {
	entered       chan struct{}
	release       chan struct{}
	terminated    chan struct{}
	terminateOnce sync.Once
	writes        int
}

func (process *blockingInputProcess) WriteInput([]byte) error {
	process.writes++
	process.entered <- struct{}{}
	<-process.release
	return nil
}

func (process *blockingInputProcess) Terminate(context.Context, time.Duration) error {
	if process.terminated != nil {
		process.terminateOnce.Do(func() { close(process.terminated) })
	}
	return nil
}
func (*blockingInputProcess) Wait() execution.Result        { return execution.Result{} }
func (*blockingInputProcess) ProcessDetails() (int, string) { return 44, "test:44" }

func blockedTimerFactory(time.Duration) deadlineTimer {
	return &manualDeadlineTimer{channel: make(chan time.Time)}
}

type fakeControl struct {
	registerCalls     int
	machineIDs        []string
	daemonInstanceIDs []string
	claimCalls        int
	eventCalls        int
	assignment        protocol.Assignment
	beforeClaim       func()
	cancel            context.CancelFunc
	claimErr          error
	claimErrors       []error
	claimIDs          []string
	workEnabled       <-chan struct{}
	claimBlock        <-chan struct{}
	claimEntered      chan<- struct{}
	renewCalls        int
	heartbeat         protocol.RuntimeHeartbeatRequest
}

type deadlineRecordingControl struct {
	contextRecorder
}

type zeroRetryClaimControl struct {
	client *control.Client
	calls  int
}

func (client *zeroRetryClaimControl) RegisterSession(context.Context, string, string, protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error) {
	return protocol.SessionRegistrationResponse{}, errors.New("unexpected RegisterSession")
}

func (client *zeroRetryClaimControl) Heartbeat(context.Context, string, protocol.RuntimeHeartbeatRequest) (protocol.RuntimeSnapshot, error) {
	return protocol.RuntimeSnapshot{}, errors.New("unexpected Heartbeat")
}

func (client *zeroRetryClaimControl) Dispatch(context.Context, string, int64) (protocol.RuntimeSnapshot, error) {
	return protocol.RuntimeSnapshot{}, errors.New("unexpected Dispatch")
}

func (client *zeroRetryClaimControl) Claim(ctx context.Context, runID string, request protocol.ClaimRequest) (protocol.ClaimResponse, error) {
	client.calls++
	if client.calls == 1 {
		_, err := client.client.Dispatch(ctx, "runtime-1", 1)
		return protocol.ClaimResponse{}, err
	}
	return protocol.ClaimResponse{RunID: runID, Generation: request.Generation, ClaimID: request.ClaimID, LeaseToken: "lease", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"}}, nil
}

func (client *zeroRetryClaimControl) RenewLease(context.Context, string, protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
	return protocol.LeaseHeartbeatResponse{}, errors.New("unexpected RenewLease")
}

func (client *zeroRetryClaimControl) AppendEvents(context.Context, string, protocol.AppendEventsRequest) error {
	return errors.New("unexpected AppendEvents")
}

func (client *zeroRetryClaimControl) Transition(context.Context, string, protocol.StateTransitionRequest) error {
	return errors.New("unexpected Transition")
}

func (client *zeroRetryClaimControl) Reconcile(context.Context, string, protocol.ReconcileRequest) (protocol.ReconcileResponse, error) {
	return protocol.ReconcileResponse{}, errors.New("unexpected Reconcile")
}

func (client *zeroRetryClaimControl) AcknowledgeCommand(context.Context, string, protocol.CommandAcknowledgement) error {
	return errors.New("unexpected AcknowledgeCommand")
}

type terminalRetryControl struct {
	fakeControl
	transitionErr   error
	transitionCalls int
}

type shortLeaseRegistrationControl struct {
	fakeControl
	leaseDurationMS int64
}

func (client *shortLeaseRegistrationControl) RegisterSession(context.Context, string, string, protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error) {
	return protocol.SessionRegistrationResponse{
		Runtimes:        []protocol.RegisteredRuntime{{RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1}},
		LeaseDurationMS: client.leaseDurationMS,
	}, nil
}

type staleTerminalFailureControl struct {
	fakeControl
	completedStarted chan struct{}
	releaseCompleted chan struct{}
	calls            []string
}

type terminalAfterEventControl struct {
	fakeControl
	afterEvent      func()
	transitionErr   error
	transitionCalls int
}

func (client *terminalAfterEventControl) AppendEvents(context.Context, string, protocol.AppendEventsRequest) error {
	if client.afterEvent != nil {
		afterEvent := client.afterEvent
		client.afterEvent = nil
		afterEvent()
	}
	return nil
}

func (client *terminalAfterEventControl) Transition(context.Context, string, protocol.StateTransitionRequest) error {
	client.transitionCalls++
	return client.transitionErr
}

func (client *staleTerminalFailureControl) Transition(_ context.Context, _ string, request protocol.StateTransitionRequest) error {
	client.calls = append(client.calls, "transition:"+request.State)
	if request.State == "completed" {
		close(client.completedStarted)
		<-client.releaseCompleted
		return &control.APIError{StatusCode: http.StatusUnprocessableEntity, Code: control.InvalidTransition}
	}
	return nil
}

func (client *staleTerminalFailureControl) AcknowledgeCommand(_ context.Context, commandID string, _ protocol.CommandAcknowledgement) error {
	client.calls = append(client.calls, "ack:"+commandID)
	return nil
}

type renewalCommandControl struct {
	fakeControl
	command      protocol.Command
	renewEntered chan struct{}
}

func (client *renewalCommandControl) RenewLease(context.Context, string, protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
	client.renewEntered <- struct{}{}
	return protocol.LeaseHeartbeatResponse{LeaseExpiresAt: time.Now().Add(time.Minute), Commands: []protocol.Command{client.command}}, nil
}

func (client *terminalRetryControl) Transition(context.Context, string, protocol.StateTransitionRequest) error {
	client.transitionCalls++
	return client.transitionErr
}

type responseErrorTerminalControl struct {
	fakeControl
	client          *control.Client
	transitionCalls int
}

func (client *responseErrorTerminalControl) Transition(ctx context.Context, _ string, _ protocol.StateTransitionRequest) error {
	client.transitionCalls++
	_, err := client.client.Dispatch(ctx, "runtime-1", 1)
	return err
}

type ordinaryOutboxFailureControl struct{ fakeControl }

func (client *ordinaryOutboxFailureControl) AppendEvents(context.Context, string, protocol.AppendEventsRequest) error {
	client.eventCalls++
	return &control.APIError{StatusCode: http.StatusBadRequest, Code: control.InvalidRequest}
}

func (client *deadlineRecordingControl) RegisterSession(ctx context.Context, _, _ string, _ protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error) {
	client.record(ctx, controlRequestLimit)
	return protocol.SessionRegistrationResponse{Runtimes: []protocol.RegisteredRuntime{{RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1}}, LeaseDurationMS: 120_000}, nil
}

func (client *deadlineRecordingControl) Heartbeat(ctx context.Context, _ string, _ protocol.RuntimeHeartbeatRequest) (protocol.RuntimeSnapshot, error) {
	client.record(ctx, controlRequestLimit)
	return protocol.RuntimeSnapshot{}, nil
}

func (client *deadlineRecordingControl) Dispatch(ctx context.Context, _ string, _ int64) (protocol.RuntimeSnapshot, error) {
	client.record(ctx, controlRequestLimit)
	return protocol.RuntimeSnapshot{}, nil
}

func (client *deadlineRecordingControl) Claim(ctx context.Context, runID string, request protocol.ClaimRequest) (protocol.ClaimResponse, error) {
	client.record(ctx, controlRequestLimit)
	return protocol.ClaimResponse{RunID: runID, Generation: request.Generation, ClaimID: request.ClaimID, LeaseToken: "lease-claim", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "work"}}, nil
}

func (client *deadlineRecordingControl) RenewLease(ctx context.Context, _ string, _ protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
	client.record(ctx, time.Second)
	return protocol.LeaseHeartbeatResponse{LeaseExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (client *deadlineRecordingControl) AppendEvents(ctx context.Context, _ string, _ protocol.AppendEventsRequest) error {
	client.record(ctx, controlRequestLimit)
	return nil
}

func (client *deadlineRecordingControl) Transition(ctx context.Context, _ string, _ protocol.StateTransitionRequest) error {
	client.record(ctx, controlRequestLimit)
	return nil
}

func (client *deadlineRecordingControl) Reconcile(ctx context.Context, _ string, _ protocol.ReconcileRequest) (protocol.ReconcileResponse, error) {
	client.record(ctx, controlRequestLimit)
	return protocol.ReconcileResponse{}, nil
}

func (client *deadlineRecordingControl) AcknowledgeCommand(ctx context.Context, _ string, _ protocol.CommandAcknowledgement) error {
	client.record(ctx, controlRequestLimit)
	return nil
}

type orderingControl struct {
	fakeControl
	calls             []string
	terminal          bool
	failCancelledOnce bool
}

type retryTransitionControl struct {
	fakeControl
	requests  []protocol.StateTransitionRequest
	failFirst bool
}

func (client *retryTransitionControl) Transition(_ context.Context, _ string, request protocol.StateTransitionRequest) error {
	client.requests = append(client.requests, request)
	if client.failFirst {
		client.failFirst = false
		return errors.New("unknown transition result")
	}
	return nil
}

type lateRenewalControl struct {
	fakeControl
	started    chan struct{}
	cancelled  chan struct{}
	release    chan struct{}
	lateExpiry time.Time
}

func (client *lateRenewalControl) RenewLease(ctx context.Context, _ string, _ protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
	client.renewCalls++
	client.started <- struct{}{}
	<-ctx.Done()
	client.cancelled <- struct{}{}
	<-client.release
	return protocol.LeaseHeartbeatResponse{LeaseExpiresAt: client.lateExpiry}, nil
}

type terminalVerdictControl struct {
	fakeControl
	transitionErr error
}

func (client *terminalVerdictControl) Transition(context.Context, string, protocol.StateTransitionRequest) error {
	return client.transitionErr
}

type terminalAcknowledgementControl struct {
	fakeControl
	transitionErr error
}

func (client *terminalAcknowledgementControl) Transition(context.Context, string, protocol.StateTransitionRequest) error {
	return client.transitionErr
}

type permanentTerminalAcknowledgementControl struct {
	fakeControl
	transitionCalls      int
	acknowledgementCalls int
}

func (client *permanentTerminalAcknowledgementControl) Transition(context.Context, string, protocol.StateTransitionRequest) error {
	client.transitionCalls++
	return nil
}

func (client *permanentTerminalAcknowledgementControl) AcknowledgeCommand(context.Context, string, protocol.CommandAcknowledgement) error {
	client.acknowledgementCalls++
	return &control.APIError{StatusCode: http.StatusBadRequest, Code: control.InvalidRequest}
}

type atomicCancelDeliveryControl struct {
	fakeControl
	cancel context.CancelFunc
	calls  []string
}

func (client *atomicCancelDeliveryControl) Transition(_ context.Context, _ string, request protocol.StateTransitionRequest) error {
	client.calls = append(client.calls, "transition:"+request.State)
	return nil
}

func (client *atomicCancelDeliveryControl) AcknowledgeCommand(_ context.Context, commandID string, _ protocol.CommandAcknowledgement) error {
	client.calls = append(client.calls, "ack:"+commandID)
	client.cancel()
	return nil
}

type terminalOutboxOwnershipControl struct {
	fakeControl
	nonTerminalTransitionCalls int
}

func (client *terminalOutboxOwnershipControl) AppendEvents(context.Context, string, protocol.AppendEventsRequest) error {
	client.eventCalls++
	return errors.New("ordinary events must not be sent after terminal entry")
}

func (client *terminalOutboxOwnershipControl) Transition(_ context.Context, _ string, request protocol.StateTransitionRequest) error {
	if !isTerminalTransition(request.State) {
		client.nonTerminalTransitionCalls++
		return errors.New("ordinary transitions must not be sent after terminal entry")
	}
	return nil
}

type terminalAcceptedAcknowledgementControl struct {
	fakeControl
	acknowledgementErr error
}

func (*terminalAcceptedAcknowledgementControl) Transition(context.Context, string, protocol.StateTransitionRequest) error {
	return nil
}

func (client *terminalAcceptedAcknowledgementControl) AcknowledgeCommand(context.Context, string, protocol.CommandAcknowledgement) error {
	return client.acknowledgementErr
}

type overlappingRenewalControl struct {
	fakeControl
	mu              sync.Mutex
	calls           int
	firstStarted    chan struct{}
	firstRelease    chan struct{}
	secondStarted   chan struct{}
	secondCancelled chan struct{}
}

type expiringRenewalControl struct {
	fakeControl
	advance           func()
	deadlineRemaining time.Duration
}

type unsafeRenewalControl struct {
	fakeControl
	expiry time.Time
}

func (client *unsafeRenewalControl) RenewLease(context.Context, string, protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
	return protocol.LeaseHeartbeatResponse{LeaseExpiresAt: client.expiry}, nil
}

type lateSuccessfulClaimControl struct {
	fakeControl
	advance func()
	calls   int
}

func (client *lateSuccessfulClaimControl) Claim(_ context.Context, runID string, request protocol.ClaimRequest) (protocol.ClaimResponse, error) {
	client.calls++
	client.advance()
	return protocol.ClaimResponse{RunID: runID, Generation: request.Generation, ClaimID: request.ClaimID, LeaseToken: "lease", LeaseExpiresAt: time.Now().Add(time.Minute), Work: protocol.Work{Goal: "g"}}, nil
}

func (client *expiringRenewalControl) RenewLease(ctx context.Context, _ string, _ protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
	deadline, ok := ctx.Deadline()
	if ok {
		client.deadlineRemaining = time.Until(deadline)
	}
	client.advance()
	return protocol.LeaseHeartbeatResponse{}, context.DeadlineExceeded
}

func (client *overlappingRenewalControl) RenewLease(ctx context.Context, _ string, _ protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
	client.mu.Lock()
	client.calls++
	call := client.calls
	client.mu.Unlock()
	if call == 1 {
		client.firstStarted <- struct{}{}
		<-client.firstRelease
		return protocol.LeaseHeartbeatResponse{LeaseExpiresAt: time.Now().Add(time.Minute)}, nil
	}
	client.secondStarted <- struct{}{}
	<-ctx.Done()
	client.secondCancelled <- struct{}{}
	return protocol.LeaseHeartbeatResponse{}, ctx.Err()
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
		return transportError("temporary failure")
	}
	return client.transitionErr
}

func (client *terminalRecoveryControl) RegisterSession(_ context.Context, _, _ string, _ protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error) {
	client.registerCalls++
	client.registeredAfterFlush = client.flushed
	if client.cancel != nil {
		client.cancel()
	}
	return protocol.SessionRegistrationResponse{Runtimes: []protocol.RegisteredRuntime{{RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1}}, LeaseDurationMS: 120_000}, nil
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
	started        chan string
	release        <-chan struct{}
	secondCommands []protocol.Command
}

type shutdownRenewControl struct {
	fakeControl
	started         chan string
	firstReturned   chan struct{}
	secondCancelled chan struct{}
	releaseSecond   chan struct{}
}

type reconcileRenewalRaceControl struct {
	fakeControl
	reconcileStarted chan struct{}
	releaseReconcile chan struct{}
	reconcileExpiry  time.Time
	renewedExpiry    time.Time
}

func (client *reconcileRenewalRaceControl) Reconcile(context.Context, string, protocol.ReconcileRequest) (protocol.ReconcileResponse, error) {
	client.reconcileStarted <- struct{}{}
	<-client.releaseReconcile
	return protocol.ReconcileResponse{Decisions: []protocol.ReconcileDecision{{RunID: "run-1", Generation: 1, Decision: protocol.ReconcileContinue, LeaseExpiresAt: &client.reconcileExpiry}}}, nil
}

func (client *reconcileRenewalRaceControl) RenewLease(context.Context, string, protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
	return protocol.LeaseHeartbeatResponse{LeaseExpiresAt: client.renewedExpiry}, nil
}

func (client *shutdownRenewControl) RenewLease(ctx context.Context, runID string, _ protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
	client.started <- runID
	<-ctx.Done()
	if runID == "run-1" {
		client.firstReturned <- struct{}{}
		return protocol.LeaseHeartbeatResponse{}, ctx.Err()
	}
	client.secondCancelled <- struct{}{}
	<-client.releaseSecond
	return protocol.LeaseHeartbeatResponse{}, ctx.Err()
}

type blockingReactorControl struct {
	fakeControl
	dispatchEntered  chan struct{}
	dispatchRelease  chan struct{}
	reconcileEntered chan struct{}
	reconcileRelease chan struct{}
	renewEntered     chan struct{}
}

func (client *blockingReactorControl) Dispatch(context.Context, string, int64) (protocol.RuntimeSnapshot, error) {
	if client.dispatchEntered != nil {
		client.dispatchEntered <- struct{}{}
	}
	if client.dispatchRelease != nil {
		<-client.dispatchRelease
	}
	return protocol.RuntimeSnapshot{}, nil
}

func (client *blockingReactorControl) Reconcile(context.Context, string, protocol.ReconcileRequest) (protocol.ReconcileResponse, error) {
	if client.reconcileEntered != nil {
		client.reconcileEntered <- struct{}{}
	}
	if client.reconcileRelease != nil {
		<-client.reconcileRelease
	}
	return protocol.ReconcileResponse{}, nil
}

func (client *blockingReactorControl) RenewLease(context.Context, string, protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
	if client.renewEntered != nil {
		client.renewEntered <- struct{}{}
	}
	return protocol.LeaseHeartbeatResponse{LeaseExpiresAt: time.Now().Add(time.Minute)}, nil
}

func (client *parallelRenewControl) RenewLease(_ context.Context, runID string, _ protocol.LeaseHeartbeatRequest) (protocol.LeaseHeartbeatResponse, error) {
	client.started <- runID
	if runID == "run-1" {
		<-client.release
	}
	return protocol.LeaseHeartbeatResponse{LeaseExpiresAt: time.Now().Add(time.Minute), Commands: client.secondCommands}, nil
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

func sameBools(left, right []bool) bool {
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

func (client *fakeControl) RegisterSession(_ context.Context, machineID, daemonInstanceID string, _ protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error) {
	client.registerCalls++
	client.machineIDs = append(client.machineIDs, machineID)
	client.daemonInstanceIDs = append(client.daemonInstanceIDs, daemonInstanceID)
	if client.cancel != nil && client.assignment.RunID == "" {
		client.cancel()
	}
	return protocol.SessionRegistrationResponse{Runtimes: []protocol.RegisteredRuntime{{RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: 1}}, LeaseDurationMS: 120_000}, nil
}
func (client *fakeControl) Heartbeat(_ context.Context, _ string, request protocol.RuntimeHeartbeatRequest) (protocol.RuntimeSnapshot, error) {
	client.heartbeat = request
	return protocol.RuntimeSnapshot{}, nil
}
func (client *fakeControl) Dispatch(context.Context, string, int64) (protocol.RuntimeSnapshot, error) {
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

type failingCleanupWorkspace struct{}

func (failingCleanupWorkspace) Prepare(context.Context, string, workspace.RunRef) (workspace.Prepared, error) {
	return workspace.Prepared{}, errors.New("unexpected prepare")
}
func (failingCleanupWorkspace) Recover(context.Context, string, workspace.RunRef, string) (workspace.Prepared, error) {
	return workspace.Prepared{}, errors.New("unexpected recover")
}
func (failingCleanupWorkspace) Cleanup(context.Context, workspace.Prepared, bool) error {
	return errors.New("cleanup failed")
}

type retryCleanupWorkspace struct {
	remainingFailures int
	outcomes          []bool
}

func (*retryCleanupWorkspace) Prepare(context.Context, string, workspace.RunRef) (workspace.Prepared, error) {
	return workspace.Prepared{}, errors.New("unexpected prepare")
}
func (*retryCleanupWorkspace) Recover(_ context.Context, key string, run workspace.RunRef, path string) (workspace.Prepared, error) {
	return workspace.Prepared{Path: path, BindingKey: key, Run: run}, nil
}
func (workspace *retryCleanupWorkspace) Cleanup(_ context.Context, _ workspace.Prepared, succeeded bool) error {
	workspace.outcomes = append(workspace.outcomes, succeeded)
	if workspace.remainingFailures > 0 {
		workspace.remainingFailures--
		return errors.New("cleanup failed")
	}
	return nil
}

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

type recordingProcess struct {
	input  []byte
	writes int
}

func (process *recordingProcess) WriteInput(input []byte) error {
	process.writes++
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

type deferredExitProcess struct {
	terminated   chan struct{}
	exit         chan struct{}
	once         sync.Once
	terminations int
}

func newDeferredExitProcess() *deferredExitProcess {
	return &deferredExitProcess{terminated: make(chan struct{}), exit: make(chan struct{})}
}

func (*deferredExitProcess) WriteInput([]byte) error { return nil }
func (process *deferredExitProcess) Terminate(context.Context, time.Duration) error {
	process.once.Do(func() {
		process.terminations++
		close(process.terminated)
	})
	return nil
}
func (process *deferredExitProcess) Wait() execution.Result {
	<-process.exit
	return execution.Result{Terminated: true}
}
func (*deferredExitProcess) ProcessDetails() (int, string) { return 45, "test:45" }
