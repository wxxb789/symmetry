package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestRestartInputRecoveryDoesNotRegisterAfterTerminalIDFailure(t *testing.T) {
	store, key, intent, now := restartInputRecoveryTerminalFixture(t)
	defer store.Close()

	terminalIDError := errors.New("injected terminal transition ID failure")
	idCalls := 0
	api := &restartRecoveryControl{registeredEpoch: 2}
	restarted := &daemon{
		config: testConfig(t),
		log:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options: options{
			store:     store,
			control:   api,
			workspace: &fakeWorkspace{},
			start:     failStart,
			clock:     func() time.Time { return now },
			newID: func() (string, error) {
				idCalls++
				if idCalls == 1 {
					return "", terminalIDError
				}
				return "would-register-new-daemon-instance", nil
			},
		},
		running: make(map[state.RunKey]*runningRun),
	}

	err := restarted.initialize(context.Background())
	if err == nil || !errors.Is(err, terminalIDError) {
		t.Fatalf("initialize error = %v, want terminal ID error", err)
	}
	if errors.Is(err, errRecoveryPending) {
		t.Fatalf("initialize error = %v, must not be recovery pending", err)
	}
	assertRestartInputTerminalBarrier(t, store, key, intent, api, idCalls)
	assertRestartInputTerminalRegistersOnNextInitialize(t, store, key, intent, now, api)
}

func TestRestartInputRecoveryDoesNotRegisterAfterPermanentTerminalQueueFailure(t *testing.T) {
	store, key, intent, now := restartInputRecoveryTerminalFixture(t)
	defer store.Close()

	terminalQueueError := errors.New("injected permanent terminal queue failure")
	queueCalls := 0
	api := &restartRecoveryControl{registeredEpoch: 2}
	restarted := &daemon{
		config: testConfig(t),
		log:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options: options{
			store:     store,
			control:   api,
			workspace: &fakeWorkspace{},
			start:     failStart,
			clock:     func() time.Time { return now },
			newID:     func() (string, error) { return "terminal-transition", nil },
			queueTerminalTransition: func(runKey state.RunKey, transition protocol.StateTransitionRequest, at time.Time) (state.RunJournal, error) {
				queueCalls++
				if runKey != key || transition.State != "failed" || !at.Equal(now) {
					t.Fatalf("terminal queue request = (%#v, %#v, %s)", runKey, transition, at)
				}
				return state.RunJournal{}, terminalQueueError
			},
		},
		running: make(map[state.RunKey]*runningRun),
	}

	err := restarted.initialize(context.Background())
	if err == nil || !errors.Is(err, terminalQueueError) {
		t.Fatalf("initialize error = %v, want terminal queue error", err)
	}
	if errors.Is(err, errRecoveryPending) {
		t.Fatalf("initialize error = %v, must not be recovery pending", err)
	}
	if queueCalls != 2 {
		t.Fatalf("terminal queue calls = %d, want one immediate retry", queueCalls)
	}
	assertRestartInputTerminalBarrier(t, store, key, intent, api, 1)
	assertRestartInputTerminalRegistersOnNextInitialize(t, store, key, intent, now, api)
}

func TestRestartInputRecoveryCompletesIndependentJournalButBlocksRegistration(t *testing.T) {
	store, blockedKey, blockedIntent, now := restartInputRecoveryTerminalFixture(t)
	defer store.Close()

	completedKey := state.RunKey{RunID: "run-2", Generation: 1}
	saveClaimedGoalRun(t, store, completedKey)
	if _, err := store.SetLocalState(completedKey, "waiting_for_input"); err != nil {
		t.Fatal(err)
	}
	completedIntent := restartInputRecoveryTerminalIntent(t, store, completedKey, now, "event-before-input-2", "input-2", "running-after-input-2", "input-ack-2")

	blockedError := errors.New("injected blocking terminal queue failure")
	queueCalls := make(map[state.RunKey]int)
	startCalls := 0
	api := &restartRecoveryControl{registeredEpoch: 2}
	restarted := &daemon{
		config: testConfig(t),
		log:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options: options{
			store:     store,
			control:   api,
			workspace: &fakeWorkspace{},
			start: func(context.Context, execution.Invocation, execution.Sink) (Process, error) {
				startCalls++
				return nil, errors.New("restart recovery must not start an agent")
			},
			clock: func() time.Time { return now },
			newID: func() (string, error) { return "terminal-transition", nil },
			queueTerminalTransition: func(key state.RunKey, transition protocol.StateTransitionRequest, at time.Time) (state.RunJournal, error) {
				queueCalls[key]++
				if transition.State != "failed" || !at.Equal(now) {
					t.Fatalf("terminal queue request = (%#v, %#v, %s)", key, transition, at)
				}
				if key == blockedKey {
					return state.RunJournal{}, blockedError
				}
				return store.QueueTerminalTransitionAt(key, transition, at)
			},
		},
		running: make(map[state.RunKey]*runningRun),
	}

	err := restarted.initialize(context.Background())
	if err == nil || !errors.Is(err, blockedError) {
		t.Fatalf("initialize error = %v, want blocking terminal queue error", err)
	}
	if errors.Is(err, errRecoveryPending) {
		t.Fatalf("initialize error = %v, must not be recovery pending", err)
	}
	if startCalls != 0 {
		t.Fatalf("agent starts = %d, want 0", startCalls)
	}
	if queueCalls[blockedKey] != 2 || queueCalls[completedKey] != 1 {
		t.Fatalf("terminal queue calls = %#v, want one immediate retry for the blocked journal", queueCalls)
	}
	if got := api.callsSnapshot(); containsString(got, "register") {
		t.Fatalf("recovery calls = %#v, must not register after a blocking entry failure", got)
	}
	assertRestartInputTerminalMarker(t, store, blockedKey, blockedIntent)
	assertRestartInputTerminalDurable(t, store, completedKey, completedIntent, protocol.Fence{
		RuntimeID: "runtime-1", RuntimeEpoch: 1, Generation: completedKey.Generation,
		ClaimID: "claim-" + completedKey.RunID, LeaseToken: "lease-" + completedKey.RunID,
	})
}

func TestRecoveryTerminalQueueRetriesExactStoreWriteAfterPostRenameError(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()

	pendingAt := time.Date(2026, time.September, 17, 5, 6, 7, 0, time.UTC)
	postRenameErr := errors.New("injected post-rename terminal write error")
	writes := make([]state.RunJournal, 0, 2)
	writeCalls := 0
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writeCalls++
		var journal state.RunJournal
		if err := json.Unmarshal(data, &journal); err != nil {
			t.Fatalf("decode journal write: %v", err)
		}
		writes = append(writes, journal)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
		if writeCalls == 1 {
			return postRenameErr
		}
		return nil
	})
	defer restore()

	idCalls := 0
	app := &daemon{
		store: store,
		options: options{
			clock: func() time.Time { return pendingAt },
			newID: func() (string, error) {
				idCalls++
				return "recovery-terminal-1", nil
			},
		},
		running: map[state.RunKey]*runningRun{key: {}},
	}
	payload := map[string]any{"z": 2, "a": "unknown"}
	if err := app.queueRecoveryTerminalTransition(context.Background(), key, "failed", payload); err != nil {
		t.Fatalf("queueRecoveryTerminalTransition() error = %v", err)
	}
	if idCalls != 1 {
		t.Fatalf("new ID calls = %d, want one ID for both exact writes", idCalls)
	}
	if writeCalls != 2 || len(writes) != 2 {
		t.Fatalf("journal writes = %d/%d, want two exact writes", writeCalls, len(writes))
	}
	first, second := writes[0], writes[1]
	if len(first.PendingTransitions) != 1 || len(second.PendingTransitions) != 1 {
		t.Fatalf("pending transitions after retry = %#v / %#v", first.PendingTransitions, second.PendingTransitions)
	}
	if first.PendingTransitions[0].TransitionID != "recovery-terminal-1" || second.PendingTransitions[0].TransitionID != "recovery-terminal-1" {
		t.Fatalf("transition IDs = %q / %q, want the same generated ID", first.PendingTransitions[0].TransitionID, second.PendingTransitions[0].TransitionID)
	}
	if !first.TerminalPendingAt.Equal(pendingAt) || !second.TerminalPendingAt.Equal(pendingAt) {
		t.Fatalf("terminal pending times = %s / %s, want %s", first.TerminalPendingAt, second.TerminalPendingAt, pendingAt)
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.PendingTransitions) != 1 || loaded.PendingTransitions[0].TransitionID != "recovery-terminal-1" || !loaded.TerminalPendingAt.Equal(pendingAt) {
		t.Fatalf("durable recovery terminal = %#v, want exact retry identity and time", loaded)
	}
}

func TestRecoveryTerminalQueueReusesCanonicalPendingTerminalIdentity(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()

	pendingAt := time.Date(2026, time.September, 17, 6, 7, 8, 0, time.UTC)
	initialPayload := json.RawMessage(`{ "z": 2, "a": 1 }`)
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{
		TransitionID: "existing-recovery-terminal",
		State:        "failed",
		Payload:      initialPayload,
	}, pendingAt); err != nil {
		t.Fatal(err)
	}
	before, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}

	idCalls := 0
	app := &daemon{
		store: store,
		options: options{newID: func() (string, error) {
			idCalls++
			return "unexpected-new-id", nil
		}},
		running: map[state.RunKey]*runningRun{key: {}},
	}
	if err := app.queueRecoveryTerminalTransition(context.Background(), key, "failed", map[string]int{"a": 1, "z": 2}); err != nil {
		t.Fatalf("queueRecoveryTerminalTransition() exact replay error = %v", err)
	}
	if idCalls != 0 {
		t.Fatalf("new ID calls = %d, want no new ID for an existing matching terminal", idCalls)
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, before) {
		t.Fatalf("replayed terminal = %#v, want existing identity, time, and body", loaded)
	}
}

func TestRecoveryTerminalQueueConflictsWithoutMutation(t *testing.T) {
	tests := []struct {
		name        string
		stateName   string
		payload     any
		acknowledge bool
	}{
		{name: "different payload", stateName: "failed", payload: map[string]string{"reason": "different"}},
		{name: "different state", stateName: "completed", payload: map[string]string{"reason": "original"}},
		{name: "acknowledged body unavailable", stateName: "failed", payload: map[string]string{"reason": "original"}, acknowledge: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, key := claimedStore(t)
			defer store.Close()
			pendingAt := time.Date(2026, time.September, 17, 7, 8, 9, 0, time.UTC)
			if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{
				TransitionID: "authoritative-terminal",
				State:        "failed",
				Payload:      json.RawMessage(`{"reason":"original"}`),
			}, pendingAt); err != nil {
				t.Fatal(err)
			}
			if test.acknowledge {
				if _, err := store.MarkTransitionsDelivered(key, []string{"authoritative-terminal"}); err != nil {
					t.Fatal(err)
				}
			}
			before, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			idCalls := 0
			app := &daemon{
				store: store,
				options: options{newID: func() (string, error) {
					idCalls++
					return "must-not-be-created", nil
				}},
				running: map[state.RunKey]*runningRun{key: {}},
			}
			err = app.queueRecoveryTerminalTransition(context.Background(), key, test.stateName, test.payload)
			if err == nil || !errors.Is(err, errAuthoritativeTerminal) {
				t.Fatalf("queueRecoveryTerminalTransition() error = %v, want authoritative terminal conflict", err)
			}
			if idCalls != 0 {
				t.Fatalf("new ID calls = %d, want zero on conflict", idCalls)
			}
			after, err := store.LoadJournal(key)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("conflicting recovery mutated journal:\nbefore=%#v\nafter=%#v", before, after)
			}
		})
	}
}

func TestNativeRecoveryPersistenceFailuresBlockRegistration(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*daemon, *state.Store, state.RunKey) error
		assert    func(*testing.T, *state.Store, state.RunKey, *restartRecoveryControl)
	}{
		{
			name: "retention",
			configure: func(daemon *daemon, _ *state.Store, _ state.RunKey) error {
				failure := errors.New("injected retention persistence failure")
				daemon.options.retainWorkspace = func(state.RunKey) (state.RunJournal, error) {
					return state.RunJournal{}, failure
				}
				return failure
			},
			assert: func(t *testing.T, store *state.Store, key state.RunKey, control *restartRecoveryControl) {
				journal, err := store.LoadJournal(key)
				if err != nil {
					t.Fatal(err)
				}
				if journal.RetainWorkspace || journal.TerminalState != "" || journal.NativeUsageRecoveryRequired || !journal.HasProcessDetails() {
					t.Fatalf("retention failure changed recovery state: %#v", journal)
				}
				if containsString(control.callsSnapshot(), "register") {
					t.Fatalf("retention failure allowed registration: %#v", control.callsSnapshot())
				}
			},
		},
		{
			name: "usage",
			configure: func(daemon *daemon, _ *state.Store, _ state.RunKey) error {
				failure := errors.New("injected native usage persistence failure")
				daemon.options.queueGoalUsage = func(state.RunKey, protocol.Usage) (state.RunJournal, error) {
					return state.RunJournal{}, failure
				}
				return failure
			},
			assert: func(t *testing.T, store *state.Store, key state.RunKey, control *restartRecoveryControl) {
				journal, err := store.LoadJournal(key)
				if err != nil {
					t.Fatal(err)
				}
				if !journal.RetainWorkspace || !journal.NativeUsageRecoveryRequired || journal.TerminalState != "" || !journal.HasProcessDetails() {
					t.Fatalf("usage failure did not retain the recovery barrier: %#v", journal)
				}
				if containsString(control.callsSnapshot(), "register") {
					t.Fatalf("usage failure allowed registration: %#v", control.callsSnapshot())
				}
			},
		},
		{
			name: "terminal",
			configure: func(daemon *daemon, _ *state.Store, _ state.RunKey) error {
				failure := errors.New("injected native terminal persistence failure")
				daemon.options.queueTerminalTransition = func(state.RunKey, protocol.StateTransitionRequest, time.Time) (state.RunJournal, error) {
					return state.RunJournal{}, failure
				}
				return failure
			},
			assert: func(t *testing.T, store *state.Store, key state.RunKey, control *restartRecoveryControl) {
				journal, err := store.LoadJournal(key)
				if err != nil {
					t.Fatal(err)
				}
				if !journal.RetainWorkspace || !journal.NativeUsageRecoveryRequired || journal.TerminalState != "" || len(journal.PendingGoalDeliveries) != 1 || !journal.HasProcessDetails() {
					t.Fatalf("terminal failure did not retain usage before terminal retry: %#v", journal)
				}
				if containsString(control.callsSnapshot(), "register") {
					t.Fatalf("terminal failure allowed registration: %#v", control.callsSnapshot())
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, key, _ := nativeRecoveryRegistrationFixture(t)
			control := &restartRecoveryControl{registeredEpoch: 2}
			restarted := nativeRecoveryRegistrationDaemon(t, store, control, key)
			injected := test.configure(restarted, store, key)
			err := restarted.initialize(context.Background())
			if err == nil || !errors.Is(err, injected) {
				t.Fatalf("initialize error = %v, want injected recovery failure", err)
			}
			if errors.Is(err, errRecoveryRegistrationReady) {
				t.Fatalf("persistence failure was classified as registration-ready: %v", err)
			}
			test.assert(t, store, key, control)
		})
	}
}

func TestNativeRecoveryDoesNotMixExistingDaemonRestartInputPayload(t *testing.T) {
	store, key, _ := nativeRecoveryRegistrationFixture(t)
	control := &restartRecoveryControl{registeredEpoch: 2}
	restarted := nativeRecoveryRegistrationDaemon(t, store, control, key)
	if _, err := store.SetLocalState(key, "waiting_for_input"); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"answer":"yes"}`)
	digest, err := canonicalInputDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	intent := state.InputCommandIntent{CommandID: "input-mixed", PayloadDigest: digest, RunningTransitionID: "running-mixed", AckID: "ack-mixed"}
	if _, created, err := store.PrepareProvideInput(key, intent); err != nil || !created {
		t.Fatalf("PrepareProvideInput() created=%t error=%v", created, err)
	}
	if _, err := store.CompleteProvideInput(key, intent.CommandID, intent.PayloadDigest, "applied"); err != nil {
		t.Fatal(err)
	}
	terminalPayload := json.RawMessage(`{"stage":"daemon_restart","reason":"unknown_outcome","summary":"input command recovery cannot safely replay stdin","error":"input command recovery cannot safely replay stdin"}`)
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "daemon-restart-terminal", State: "failed", Payload: terminalPayload}, time.Date(2026, time.September, 14, 2, 3, 4, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	if err := restarted.initialize(context.Background()); err != nil {
		t.Fatalf("initialize error = %v", err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	var daemonRestartTransition *protocol.StateTransitionRequest
	for index := range journal.PendingTransitions {
		if journal.PendingTransitions[index].State == "failed" {
			daemonRestartTransition = &journal.PendingTransitions[index]
			break
		}
	}
	if daemonRestartTransition == nil || !bytes.Equal(daemonRestartTransition.Payload, terminalPayload) {
		t.Fatalf("native recovery mixed the daemon_restart terminal payload: %#v", journal.PendingTransitions)
	}
	if journal.InputCommandIntent == nil || journal.InputCommandIntent.CommandID != intent.CommandID || journal.InputCommandIntent.Outcome != "applied" || journal.InputCommandIntent.AcknowledgementDelivered {
		t.Fatalf("native recovery changed InputCommandIntent: %#v", journal.InputCommandIntent)
	}
	if !journal.HasProcessDetails() || !journal.NativeUsageRecoveryRequired || !containsString(control.callsSnapshot(), "register") {
		t.Fatalf("mixed recovery registration/barrier state: journal=%#v calls=%#v", journal, control.callsSnapshot())
	}
}

func nativeRecoveryRegistrationFixture(t *testing.T) (*state.Store, state.RunKey, state.GoalSessionKey) {
	t.Helper()
	store, key := claimedGoalDeliveryStore(t)
	if err := store.SaveIdentity(state.MachineIdentity{MachineID: "machine-1", MachineToken: "machine-token"}); err != nil {
		t.Fatal(err)
	}
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parse admission = %+v, %t, %v", admission, present, err)
	}
	sessionKey := state.GoalSessionKey{GoalID: admission.GoalID, LocalHandleID: "00000000-0000-4000-8000-000000000007"}
	saveAttachedGoalSession(t, store, key, admission, sessionKey)
	if _, err := store.SetProcessDetails(key, 91, "native:91", time.Date(2026, time.September, 14, 1, 2, 3, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	return store, key, sessionKey
}

func nativeRecoveryRegistrationDaemon(t *testing.T, store *state.Store, control *restartRecoveryControl, key state.RunKey) *daemon {
	t.Helper()
	return &daemon{
		config: testConfig(t),
		log:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options: options{
			store:     store,
			control:   control,
			workspace: &fakeWorkspace{},
			start:     failStart,
			clock:     func() time.Time { return time.Date(2026, time.September, 14, 1, 2, 3, 0, time.UTC) },
			newID:     ids(),
			terminatePersist: func(pid int, identity string) error {
				if pid != 91 || identity != "native:91" {
					t.Fatalf("recovered native process = (%d, %q)", pid, identity)
				}
				return errPersistedProcessStopUnproven
			},
		},
		running: make(map[state.RunKey]*runningRun),
	}
}

func restartInputRecoveryTerminalFixture(t *testing.T) (*state.Store, state.RunKey, state.InputCommandIntent, time.Time) {
	t.Helper()
	store, key := claimedStore(t)
	if err := store.SaveIdentity(state.MachineIdentity{MachineID: "machine-1", MachineToken: "machine-token"}); err != nil {
		store.Close()
		t.Fatal(err)
	}
	now := time.Date(2026, time.September, 11, 1, 2, 3, 0, time.UTC)
	intent := restartInputRecoveryTerminalIntent(t, store, key, now, "event-before-input", "input-1", "running-after-input", "input-ack-1")
	return store, key, intent, now
}

func assertRestartInputTerminalBarrier(t *testing.T, store *state.Store, key state.RunKey, intent state.InputCommandIntent, api *restartRecoveryControl, idCalls int) {
	t.Helper()
	if idCalls != 1 {
		t.Fatalf("new ID calls = %d, want only the failed terminal preparation", idCalls)
	}
	if got, want := api.callsSnapshot(), []string{"event:event-before-input", "transition:running", "ack:input-1:applied:input-ack-1"}; !sameStrings(got, want) {
		t.Fatalf("recovery calls = %#v, want %#v without registration", got, want)
	}
	assertRestartInputTerminalMarker(t, store, key, intent)
}

func restartInputRecoveryTerminalIntent(t *testing.T, store *state.Store, key state.RunKey, now time.Time, eventID, commandID, transitionID, acknowledgementID string) state.InputCommandIntent {
	t.Helper()
	if _, err := store.QueueEvent(key, protocol.RunEvent{EventID: eventID, Sequence: 1, Kind: "waiting_for_input", OccurredAt: now, Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"answer":"yes"}`)
	digest, err := canonicalInputDigest(payload)
	if err != nil {
		t.Fatal(err)
	}
	intent := state.InputCommandIntent{CommandID: commandID, PayloadDigest: digest, RunningTransitionID: transitionID, AckID: acknowledgementID}
	if _, created, err := store.PrepareProvideInput(key, intent); err != nil || !created {
		t.Fatalf("PrepareProvideInput(%s) created=%t error=%v", key.RunID, created, err)
	}
	if _, err := store.CompleteProvideInput(key, intent.CommandID, intent.PayloadDigest, "applied"); err != nil {
		t.Fatal(err)
	}
	return intent
}

func assertRestartInputTerminalMarker(t *testing.T, store *state.Store, key state.RunKey, intent state.InputCommandIntent) {
	t.Helper()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "running" || journal.TerminalState != "" || journal.InputCommandIntent == nil || journal.InputCommandIntent.CommandID != intent.CommandID || journal.InputCommandIntent.PayloadDigest != intent.PayloadDigest || journal.InputCommandIntent.RunningTransitionID != intent.RunningTransitionID || journal.InputCommandIntent.AckID != intent.AckID || journal.InputCommandIntent.Outcome != "applied" || !journal.InputCommandIntent.AcknowledgementDelivered || len(journal.PendingEvents) != 0 || len(journal.PendingTransitions) != 0 || len(journal.PendingCommandAcknowledgements) != 0 {
		t.Fatalf("terminal preparation failure did not preserve the recoverable input marker after draining the old outbox: %#v", journal)
	}
}

func assertRestartInputTerminalRegistersOnNextInitialize(t *testing.T, store *state.Store, key state.RunKey, intent state.InputCommandIntent, now time.Time, recovered *restartRecoveryControl) {
	t.Helper()
	startCalls := 0
	registered := &restartInputRegistrationControl{restartRecoveryControl: recovered}
	registered.beforeRegister = func() {
		assertRestartInputTerminalDurable(t, store, key, intent, recovered.inputAcknowledgement.Fence)
	}
	restarted := &daemon{
		config: testConfig(t),
		log:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options: options{
			store:     store,
			control:   registered,
			workspace: &fakeWorkspace{},
			start: func(context.Context, execution.Invocation, execution.Sink) (Process, error) {
				startCalls++
				return nil, errors.New("restart recovery must not start an agent")
			},
			clock: func() time.Time { return now.Add(time.Second) },
			newID: ids(),
		},
		running: make(map[state.RunKey]*runningRun),
	}
	if err := restarted.initialize(context.Background()); err != nil {
		t.Fatalf("second initialize error = %v", err)
	}
	if !registered.observed || registered.registrations != 1 {
		t.Fatalf("registration observation = observed:%t calls:%d, want one callback", registered.observed, registered.registrations)
	}
	if startCalls != 0 {
		t.Fatalf("agent starts = %d, want 0", startCalls)
	}
	if got, want := recovered.callsSnapshot(), []string{"event:event-before-input", "transition:running", "ack:input-1:applied:input-ack-1", "register"}; !sameStrings(got, want) {
		t.Fatalf("recovery and registration calls = %#v, want %#v", got, want)
	}
}

func assertRestartInputTerminalDurable(t *testing.T, store *state.Store, key state.RunKey, intent state.InputCommandIntent, fence protocol.Fence) {
	t.Helper()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.LocalState != "terminal_pending" || journal.TerminalState != "failed" || journal.TerminalVerdict != "" || journal.InputCommandIntent == nil || journal.InputCommandIntent.CommandID != intent.CommandID || journal.InputCommandIntent.PayloadDigest != intent.PayloadDigest || journal.InputCommandIntent.RunningTransitionID != intent.RunningTransitionID || journal.InputCommandIntent.AckID != intent.AckID || journal.InputCommandIntent.Outcome != "applied" || !journal.InputCommandIntent.AcknowledgementDelivered || len(journal.PendingEvents) != 0 || len(journal.PendingCommandAcknowledgements) != 0 || len(journal.PendingTransitions) != 1 || journal.PendingTransitions[0].State != "failed" || journal.PendingTransitions[0].Fence != fence || journal.Fence() != fence {
		t.Fatalf("registration observed incomplete terminal recovery: %#v", journal)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type restartInputRegistrationControl struct {
	*restartRecoveryControl
	beforeRegister func()
	registrations  int
	observed       bool
}

func (client *restartInputRegistrationControl) RegisterSession(context.Context, string, string, protocol.SessionRegistrationRequest) (protocol.SessionRegistrationResponse, error) {
	client.record("register")
	client.registrations++
	if client.beforeRegister != nil {
		client.beforeRegister()
	}
	client.observed = true
	return protocol.SessionRegistrationResponse{Runtimes: []protocol.RegisteredRuntime{{RuntimeKey: "default", RuntimeID: "runtime-1", RuntimeEpoch: client.registeredEpoch}}, LeaseDurationMS: protocol.MinimumLeaseDurationMS}, nil
}
