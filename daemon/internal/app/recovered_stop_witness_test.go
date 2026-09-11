package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestRecoveredInputStopWitnessSurvivesClearFailure(t *testing.T) {
	store, key, _ := newRecoveredInputFixture(t, 71, "input:71")
	defer store.Close()

	stopCalls := 0
	clearCalls := 0
	app := &daemon{
		store:   store,
		running: make(map[state.RunKey]*runningRun),
		options: options{newID: ids(), clock: time.Now},
	}
	app.options.terminatePersist = func(pid int, identity string) error {
		stopCalls++
		if pid != 71 || identity != "input:71" {
			t.Fatalf("recovered input process = (%d, %q)", pid, identity)
		}
		if stopCalls > 1 {
			return errPersistedProcessStopUnproven
		}
		return nil
	}
	app.options.clearProcessDetails = func(runKey state.RunKey, pid int, identity string) (state.RunJournal, error) {
		clearCalls++
		if clearCalls == 1 {
			return state.RunJournal{}, errors.New("injected recovered input marker clear failure")
		}
		return store.ClearProcessDetails(runKey, pid, identity)
	}

	if err := app.recoverUnresolvedInputIntents(context.Background()); err != nil {
		t.Fatalf("first input recovery error = %v, want durable terminal recovery", err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.HasProcessDetails() || journal.LocalState != "terminal_pending" || journal.TerminalState != "failed" || !journal.RetainWorkspace {
		t.Fatalf("first input recovery journal = %#v, want failed terminal with retained marker/workspace", journal)
	}
	if stopCalls != 1 || clearCalls != 1 {
		t.Fatalf("first input recovery calls = stop:%d clear:%d, want stop:1 clear:1", stopCalls, clearCalls)
	}

	// The terminal journal is no longer an input-recovery candidate. The
	// existing cleanup worker retries the physical marker clear and reuses the
	// successful recovered stop proof.
	app.enqueueRecoveredCleanups()
	app.flushCleanups(context.Background())
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.HasProcessDetails() || stopCalls != 1 || clearCalls != 2 {
		t.Fatalf("input cleanup retry = journal:%#v stop:%d clear:%d, want cleared marker and one stop", journal, stopCalls, clearCalls)
	}
}

func TestRecoveredGenericCleanupReusesStopWitnessAfterClearFailure(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	startedAt := time.Date(2026, 9, 11, 8, 0, 0, 0, time.UTC)
	if _, err := store.SetProcessDetails(key, 72, "generic:72", startedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{
		TransitionID: "generic-terminal", State: "failed", Payload: json.RawMessage(`{"reason":"process_failure"}`),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveTerminal(key, state.TerminalVerdictAccepted, startedAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}

	now := startedAt.Add(2 * time.Second)
	stopCalls := 0
	clearCalls := 0
	app := &daemon{
		store:   store,
		running: make(map[state.RunKey]*runningRun),
		options: options{clock: func() time.Time { return now }},
	}
	app.options.terminatePersist = func(pid int, identity string) error {
		stopCalls++
		if pid != 72 || identity != "generic:72" {
			t.Fatalf("recovered generic process = (%d, %q)", pid, identity)
		}
		return nil
	}
	app.options.clearProcessDetails = func(runKey state.RunKey, pid int, identity string) (state.RunJournal, error) {
		clearCalls++
		if clearCalls == 1 {
			return state.RunJournal{}, errors.New("injected recovered generic marker clear failure")
		}
		return store.ClearProcessDetails(runKey, pid, identity)
	}

	app.enqueueRecoveredCleanups()
	app.flushCleanups(context.Background())
	j, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !j.HasProcessDetails() || stopCalls != 1 || clearCalls != 1 {
		t.Fatalf("first generic cleanup = journal:%#v stop:%d clear:%d, want retained marker and one stop", j, stopCalls, clearCalls)
	}
	if got := app.cleanupWait(); got != cleanupRetryMinimum {
		t.Fatalf("generic cleanup retry delay = %s, want %s", got, cleanupRetryMinimum)
	}

	// Rediscovery must preserve the existing backoff and the in-memory stop proof.
	app.enqueueRecoveredCleanups()
	if got := app.cleanupWait(); got != cleanupRetryMinimum {
		t.Fatalf("generic cleanup retry delay after rediscovery = %s, want %s", got, cleanupRetryMinimum)
	}
	now = now.Add(cleanupRetryMinimum)
	app.flushCleanups(context.Background())
	j, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if j.HasProcessDetails() || j.LocalState != "terminal_pending" || j.TerminalVerdict != state.TerminalVerdictAccepted {
		t.Fatalf("retried generic cleanup journal = %#v, want cleared terminal marker", j)
	}
	if stopCalls != 1 || clearCalls != 2 {
		t.Fatalf("retried generic cleanup calls = stop:%d clear:%d, want stop:1 clear:2", stopCalls, clearCalls)
	}
}

func TestRecoveredNativeStopReceiptFailureReusesStopWitnessAndBinding(t *testing.T) {
	store, key := claimedGoalDeliveryStore(t)
	defer store.Close()
	sessionKey, controlSessionID, bindingID := saveRetainedGoalSession(t, store, key)
	startedAt := time.Date(2026, 9, 11, 9, 0, 0, 0, time.UTC)
	if _, err := store.SetProcessDetails(key, 73, "native:73", startedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RetainWorkspace(key); err != nil {
		t.Fatal(err)
	}

	stopCalls := 0
	app := &daemon{
		store:   store,
		running: make(map[state.RunKey]*runningRun),
		options: options{newID: ids(), clock: func() time.Time { return startedAt }},
	}
	app.options.terminatePersist = func(pid int, identity string) error {
		stopCalls++
		if pid != 73 || identity != "native:73" {
			t.Fatalf("recovered native process = (%d, %q)", pid, identity)
		}
		return nil
	}
	// Retention is already durable in this fixture. Avoid an unrelated second
	// journal write so the injected Store writer reaches the stop receipt mutation.
	app.options.retainWorkspace = func(runKey state.RunKey) (state.RunJournal, error) {
		return store.LoadJournal(runKey)
	}
	restoreWriter := store.SetAtomicWriterForTesting(func(string, []byte) error {
		return errors.New("injected native stop receipt write failure")
	})
	if err := app.recoverUnclosedGoalSessions(context.Background()); !errors.Is(err, errRecoveryPending) {
		restoreWriter()
		t.Fatalf("first native recovery error = %v, want errRecoveryPending", err)
	}
	restoreWriter()

	if stopCalls != 1 {
		t.Fatalf("first native recovery stop calls = %d, want 1", stopCalls)
	}
	if err := app.recoverUnclosedGoalSessions(context.Background()); err != nil {
		t.Fatalf("second native recovery error = %v", err)
	}
	if stopCalls != 1 {
		t.Fatalf("second native recovery stop calls = %d, want proof reuse", stopCalls)
	}

	j, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if j.HasProcessDetails() {
		t.Fatalf("native recovery retained process marker after retry: %#v", j)
	}
	var stopped *state.GoalDelivery
	for i := range j.PendingGoalDeliveries {
		if j.PendingGoalDeliveries[i].Kind == state.GoalDeliverySessionStopped {
			stopped = &j.PendingGoalDeliveries[i]
			break
		}
	}
	if stopped == nil || stopped.SessionStopped == nil || stopped.SessionStopped.SessionID != controlSessionID || stopped.SessionStopped.LocalHandleID != sessionKey.LocalHandleID || stopped.SessionStopped.BindingID != bindingID {
		t.Fatalf("native stop delivery = %#v, want original binding", stopped)
	}
	session, err := store.LoadGoalSession(sessionKey)
	if err != nil {
		t.Fatal(err)
	}
	if session.SessionState != state.GoalSessionStateUnavailable || session.ControlSessionID != controlSessionID || session.BindingID != bindingID {
		t.Fatalf("native session binding after retry = %#v", session)
	}
}

func TestRecoveredStopProofCannotApplyReplacementMarker(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	firstStartedAt := time.Date(2026, 9, 11, 10, 0, 0, 0, time.UTC)
	secondStartedAt := firstStartedAt.Add(time.Minute)
	if _, err := store.SetProcessDetails(key, 74, "same-owner", firstStartedAt); err != nil {
		t.Fatal(err)
	}
	firstJournal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	stopCalls := 0
	app := &daemon{
		store:   store,
		running: make(map[state.RunKey]*runningRun),
		options: options{terminatePersist: func(pid int, identity string) error {
			stopCalls++
			if pid != 74 || identity != "same-owner" {
				t.Fatalf("persisted replacement process = (%d, %q)", pid, identity)
			}
			return nil
		}},
	}
	if err := app.stopPersistedProcess(context.Background(), firstJournal); err != nil {
		t.Fatalf("first recovered stop = %v", err)
	}
	if stopCalls != 1 {
		t.Fatalf("first recovered stop calls = %d, want 1", stopCalls)
	}
	// Bypass the daemon clear wrapper to model a stale in-memory proof surviving
	// a marker replacement between recovery attempts.
	if _, err := store.ClearProcessDetails(key, firstJournal.PID, firstJournal.ProcessIdentity); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetProcessDetails(key, 74, "same-owner", secondStartedAt); err != nil {
		t.Fatal(err)
	}
	if err := app.stopPersistedProcess(context.Background(), firstJournal); !errors.Is(err, errPersistedProcessStopUnproven) {
		t.Fatalf("stale recovered stop error = %v, want errPersistedProcessStopUnproven", err)
	}
	if stopCalls != 1 {
		t.Fatalf("stale recovered stop re-terminated replacement: calls=%d", stopCalls)
	}
	secondJournal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.stopPersistedProcess(context.Background(), secondJournal); err != nil {
		t.Fatalf("replacement recovered stop = %v", err)
	}
	if stopCalls != 2 {
		t.Fatalf("replacement recovered stop calls = %d, want a fresh proof", stopCalls)
	}
}

func TestFreshDaemonDoesNotReuseLostRecoveredStopProof(t *testing.T) {
	store, key, _ := newRecoveredInputFixture(t, 75, "restart:75")
	defer store.Close()

	firstStops := 0
	first := &daemon{
		store:   store,
		running: make(map[state.RunKey]*runningRun),
		options: options{newID: ids(), clock: time.Now},
	}
	first.options.terminatePersist = func(pid int, identity string) error {
		firstStops++
		if pid != 75 || identity != "restart:75" {
			t.Fatalf("first daemon process = (%d, %q)", pid, identity)
		}
		return nil
	}
	first.options.clearProcessDetails = func(state.RunKey, int, string) (state.RunJournal, error) {
		return state.RunJournal{}, errors.New("injected restart-boundary clear failure")
	}
	if err := first.recoverUnresolvedInputIntents(context.Background()); err != nil {
		t.Fatalf("first daemon recovery error = %v, want durable terminal recovery", err)
	}
	if firstStops != 1 {
		t.Fatalf("first daemon stop calls = %d, want 1", firstStops)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.HasProcessDetails() || journal.LocalState != "terminal_pending" || journal.TerminalState != "failed" || !journal.RetainWorkspace {
		t.Fatalf("first daemon recovery journal = %#v, want failed terminal with retained marker/workspace", journal)
	}

	secondStops := 0
	second := &daemon{
		store:   store,
		running: make(map[state.RunKey]*runningRun),
		options: options{newID: ids(), clock: time.Now, terminatePersist: func(pid int, identity string) error {
			secondStops++
			if pid != 75 || identity != "restart:75" {
				t.Fatalf("fresh daemon process = (%d, %q)", pid, identity)
			}
			return errPersistedProcessStopUnproven
		}},
	}
	// A fresh daemon has no memory witness. Cleanup must fail closed instead of
	// treating the durable terminal marker as proof that the process stopped.
	second.enqueueRecoveredCleanups()
	second.flushCleanups(context.Background())
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if secondStops != 1 || !journal.HasProcessDetails() || len(second.running) != 0 {
		t.Fatalf("fresh daemon recovery = stops:%d journal:%#v running:%d, want fail-closed marker", secondStops, journal, len(second.running))
	}
}

func newRecoveredInputFixture(t *testing.T, pid int, identity string) (*state.Store, state.RunKey, time.Time) {
	t.Helper()
	store, key := claimedStore(t)
	if _, err := store.SetLocalState(key, "waiting_for_input"); err != nil {
		store.Close()
		t.Fatal(err)
	}
	payload := json.RawMessage(`{"answer":"yes"}`)
	digest, err := canonicalInputDigest(payload)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	if _, created, err := store.PrepareProvideInput(key, state.InputCommandIntent{
		CommandID:     "recovered-input",
		PayloadDigest: digest, RunningTransitionID: "recovered-running", AckID: "recovered-ack",
	}); err != nil || !created {
		store.Close()
		t.Fatalf("PrepareProvideInput() created=%t error=%v", created, err)
	}
	startedAt := time.Date(2026, 9, 11, 7, 0, 0, 0, time.UTC)
	if _, err := store.SetProcessDetails(key, pid, identity, startedAt); err != nil {
		store.Close()
		t.Fatal(err)
	}
	return store, key, startedAt
}
