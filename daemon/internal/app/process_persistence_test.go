package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestStartAssignedUsesAtomicProcessPersistenceBeforeObserver(t *testing.T) {
	store, err := state.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	key := state.RunKey{RunID: "run-1", Generation: 1}
	now := time.Date(2026, 9, 15, 9, 10, 11, 0, time.UTC)
	events := make([]string, 0, 2)
	control := &fixedClaimControl{
		fakeControl: &fakeControl{},
		response: protocol.ClaimResponse{
			LeaseExpiresAt: now.Add(time.Minute),
			Work:           protocol.Work{Goal: "work"},
		},
	}
	daemon := &daemon{
		config:    testConfig(t),
		store:     store,
		control:   control,
		workspace: &fakeWorkspace{},
		log:       slog.New(slog.NewJSONHandler(io.Discard, nil)),
		options: options{
			newID:      ids(),
			clock:      func() time.Time { return now },
			localClock: func() time.Time { return now },
			processObserver: func(got state.RunKey, pid int, identity string, observedAt time.Time) {
				if got != key || pid != 42 || identity != "test:42" || !observedAt.Equal(now) {
					t.Errorf("process observer = (%+v, %d, %q, %s), want (%+v, 42, %q, %s)", got, pid, identity, observedAt, key, "test:42", now)
				}
				journal, loadErr := store.LoadJournal(key)
				if loadErr != nil {
					t.Errorf("load journal from process observer: %v", loadErr)
					return
				}
				if journal.PID != pid || journal.ProcessIdentity != identity || journal.ContainmentAuthority == nil {
					t.Errorf("observer saw incomplete process pair: %#v", journal)
				}
				events = append(events, "observer")
			},
		},
		runtimeID:    "runtime-1",
		runtimeEpoch: 1,
		running:      make(map[state.RunKey]*runningRun),
		slots:        make(chan struct{}, 1),
	}
	daemon.start = func(_ context.Context, invocation execution.Invocation, _ execution.Sink) (Process, error) {
		if invocation.PersistProcessWithAuthority == nil {
			t.Error("PersistProcessWithAuthority is nil")
			return nil, nil
		}
		events = append(events, "atomic")
		authorityValue := testProcessPersistenceAuthority(42, "test:42")
		if persistErr := invocation.PersistProcessWithAuthority(42, "test:42", &authorityValue); persistErr != nil {
			return nil, persistErr
		}
		return fakeProcess{result: execution.Result{ExitCode: 0, FinishedAt: now}}, nil
	}

	daemon.startAssignment(context.Background(), protocol.Assignment{
		RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "work"},
	})
	daemon.workers.Wait()

	if got, want := events, []string{"atomic", "observer"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		journal, _ := store.LoadJournal(key)
		t.Fatalf("process persistence events = %#v, want %#v; journal=%#v", got, want, journal)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.PID != 42 || journal.ProcessIdentity != "test:42" || !journal.StartedAt.Equal(now) || journal.ContainmentAuthority == nil {
		t.Fatalf("journal = %#v, want complete process/authority pair", journal)
	}
}

func TestStartAssignmentRejectsPersistedProcessMarker(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	startedAt := time.Date(2026, 9, 21, 9, 10, 11, 0, time.UTC)
	if _, err := store.SetProcessDetails(key, 42, "test:42", startedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkContainmentUnproven(key, 42, "test:42"); err != nil {
		t.Fatal(err)
	}
	starts := 0
	app := &daemon{
		store:   store,
		running: make(map[state.RunKey]*runningRun),
		slots:   make(chan struct{}, 1),
		start: func(context.Context, execution.Invocation, execution.Sink) (Process, error) {
			starts++
			return nil, nil
		},
	}
	app.startAssignment(context.Background(), protocol.Assignment{RunID: key.RunID, Generation: key.Generation, Work: protocol.Work{Goal: "must not overlap"}})
	if starts != 0 || app.hasRun(key) || len(app.slots) != 0 {
		t.Fatalf("persisted process marker admitted a new assignment: starts=%d running=%t slots=%d", starts, app.hasRun(key), len(app.slots))
	}
}

func TestPersistProcessMarkerReplaysPostRenameUnknownOutcome(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	startedAt := time.Date(2026, 9, 18, 10, 11, 12, 0, time.UTC)
	attempts := 0
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		attempts++
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
		if attempts == 1 {
			return errors.New("post-rename process marker outcome is unknown")
		}
		return nil
	})
	defer restore()

	app := &daemon{store: store}
	if err := app.persistProcessMarker(key, 42, "test:42", startedAt); err != nil {
		t.Fatalf("persistProcessMarker() error = %v", err)
	}
	if attempts != 2 {
		t.Fatalf("process marker writes = %d, want readback plus fresh replay", attempts)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.PID != 42 || journal.ProcessIdentity != "test:42" || !journal.StartedAt.Equal(startedAt) {
		t.Fatalf("journal = %#v, want exact process marker", journal)
	}
}

func TestGoalNativeStartUsesAtomicProcessPersistenceBeforeObserver(t *testing.T) {
	admission, present, err := parseAdmissionInput(validAdmissionInput())
	if err != nil || !present {
		t.Fatalf("parseAdmissionInput() = (%+v, %t, %v)", admission, present, err)
	}
	gate := make(chan struct{})
	app, store, session, controlClient := nativeAdmissionDaemon(t, admission, validNativeTaskResult(t, admission), gate)
	defer store.Close()
	session.persistAtomic = true
	key := state.RunKey{RunID: "run-1", Generation: 1}
	events := make([]string, 0, 1)
	app.options.processObserver = func(got state.RunKey, pid int, identity string, _ time.Time) {
		if got != key || pid != 41 || identity != "native:41" {
			t.Errorf("native process observer = (%+v, %d, %q), want (%+v, 41, %q)", got, pid, identity, key, "native:41")
		}
		journal, loadErr := store.LoadJournal(key)
		if loadErr != nil {
			t.Errorf("load native journal from process observer: %v", loadErr)
			return
		}
		if journal.PID != pid || journal.ProcessIdentity != identity || journal.ContainmentAuthority == nil {
			t.Errorf("native observer saw incomplete process pair: %#v", journal)
		}
		events = append(events, "observer")
	}

	app.startAssignment(context.Background(), protocol.Assignment{
		RunID: key.RunID, Generation: key.Generation, Work: controlClient.work,
	})
	select {
	case <-session.turnStarted:
	case <-time.After(testHangGuard):
		journal, loadErr := store.LoadJournal(key)
		if loadErr != nil {
			t.Fatalf("native turn did not start; read journal: %v", loadErr)
		}
		t.Fatalf("native turn did not start; local_state=%q terminal_state=%q process_present=%t authority_present=%t session_calls=%v",
			journal.LocalState, journal.TerminalState, journal.HasProcessDetails(), journal.ContainmentAuthority != nil, session.callsSnapshot())
	}

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.PID != 41 || journal.ProcessIdentity != "native:41" || journal.ContainmentAuthority == nil {
		t.Fatalf("native journal = %#v, want complete process/authority pair", journal)
	}
	if len(events) != 1 || events[0] != "observer" {
		t.Fatalf("native process persistence events = %#v, want [observer]", events)
	}
	calls := session.callsSnapshot()
	if atomic, opened := callIndex(calls, "atomic"), callIndex(calls, "open"); atomic < 0 || opened < 0 || atomic > opened {
		t.Fatalf("native session calls = %#v, want atomic persistence before open", calls)
	}
	close(gate)
	app.workers.Wait()
}

func TestPersistProcessAuthorityCallbackDoesNotCallMarkerOverride(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	now := time.Date(2026, 9, 15, 9, 20, 21, 0, time.UTC)
	markerCalls := 0
	events := make([]string, 0, 1)
	app := &daemon{
		store: store,
		options: options{
			clock: func() time.Time { return now },
			recordProcess: func(got state.RunKey, pid int, identity string, startedAt time.Time) (state.RunJournal, error) {
				markerCalls++
				return state.RunJournal{}, errors.New("marker-only override must not be called")
			},
			processObserver: func(got state.RunKey, pid int, identity string, _ time.Time) {
				if got != key || pid != 43 || identity != "test:43" {
					t.Errorf("process observer = (%+v, %d, %q)", got, pid, identity)
				}
				events = append(events, "observer")
			},
		},
	}
	value := testProcessPersistenceAuthority(43, "test:43")
	if err := app.persistProcessAuthorityCallback(key)(43, "test:43", &value); err != nil {
		t.Fatalf("persistProcessAuthorityCallback() error = %v", err)
	}
	if markerCalls != 0 {
		t.Fatalf("recordProcess calls = %d, want 0 for combined authority persistence", markerCalls)
	}
	if got, want := events, []string{"observer"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("combined persistence events = %#v, want %#v", got, want)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.PID != 43 || journal.ProcessIdentity != "test:43" || journal.ContainmentAuthority == nil {
		t.Fatalf("journal = %#v, want legacy process/authority pair", journal)
	}
}

func TestCommitSupervisorHandoffCallbackObservesDurableProcessMarker(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	handoff := testAppHandoffFixture(44, "windows:44:created-at")
	if _, err := store.PrepareSupervisorHandoff(key, handoff); err != nil {
		t.Fatalf("PrepareSupervisorHandoff() error = %v", err)
	}
	bound, err := store.BindSupervisorHandoff(key, handoff, 45, "windows:45:created-at")
	if err != nil {
		t.Fatalf("BindSupervisorHandoff() error = %v", err)
	}
	expected := *bound.ContainmentHandoff
	startedAt := time.Date(2026, 9, 17, 10, 11, 12, 0, time.UTC)
	observedAt := time.Date(2026, 9, 17, 10, 11, 13, 0, time.UTC)
	observerCalls := 0
	app := &daemon{
		store: store,
		options: options{
			clock: func() time.Time { return observedAt },
			processObserver: func(got state.RunKey, pid int, identity string, gotObservedAt time.Time) {
				observerCalls++
				if got != key || pid != expected.TargetPID || identity != expected.TargetIdentity || !gotObservedAt.Equal(observedAt) {
					t.Errorf("process observer = (%+v, %d, %q, %s), want (%+v, %d, %q, %s)", got, pid, identity, gotObservedAt, key, expected.TargetPID, expected.TargetIdentity, observedAt)
				}
				journal, loadErr := store.LoadJournal(key)
				if loadErr != nil {
					t.Errorf("load journal from process observer: %v", loadErr)
					return
				}
				if journal.ContainmentHandoff != nil || journal.PID != pid || journal.ProcessIdentity != identity || !journal.StartedAt.Equal(startedAt) {
					t.Errorf("observer saw incomplete committed marker: %#v", journal)
					return
				}
				if journal.ContainmentAuthority == nil || journal.ContainmentAuthority.TargetPID != pid || journal.ContainmentAuthority.TargetIdentity != identity {
					t.Errorf("observer saw incomplete containment authority: %#v", journal.ContainmentAuthority)
				}
			},
		},
	}

	if err := app.commitSupervisorHandoffCallback(key)(expected, startedAt); err != nil {
		t.Fatalf("commitSupervisorHandoffCallback() error = %v", err)
	}
	if observerCalls != 1 {
		t.Fatalf("process observer calls = %d, want 1", observerCalls)
	}
}

func TestCommitSupervisorHandoffCallbackDoesNotObserveFailedCommit(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	handoff := testAppHandoffFixture(46, "windows:46:created-at")
	if _, err := store.PrepareSupervisorHandoff(key, handoff); err != nil {
		t.Fatalf("PrepareSupervisorHandoff() error = %v", err)
	}
	bound, err := store.BindSupervisorHandoff(key, handoff, 47, "windows:47:created-at")
	if err != nil {
		t.Fatalf("BindSupervisorHandoff() error = %v", err)
	}
	expected := *bound.ContainmentHandoff
	commitErr := errors.New("commit failed")
	observerCalls := 0
	app := &daemon{
		store: store,
		options: options{
			commitSupervisorHandoff: func(gotKey state.RunKey, got authority.SupervisorHandoff, gotStartedAt time.Time) (state.RunJournal, error) {
				if gotKey != key || !got.Equal(expected) || gotStartedAt.IsZero() {
					t.Errorf("commit arguments = (%+v, %#v, %s), want key and exact handoff", gotKey, got, gotStartedAt)
				}
				return state.RunJournal{}, commitErr
			},
			processObserver: func(state.RunKey, int, string, time.Time) {
				observerCalls++
			},
		},
	}

	if err := app.commitSupervisorHandoffCallback(key)(expected, time.Date(2026, 9, 17, 10, 12, 13, 0, time.UTC)); !errors.Is(err, commitErr) {
		t.Fatalf("commitSupervisorHandoffCallback() error = %v, want %v", err, commitErr)
	}
	if observerCalls != 0 {
		t.Fatalf("process observer calls = %d, want 0 after failed commit", observerCalls)
	}
	journ, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journ.ContainmentHandoff == nil || journ.HasProcessDetails() || journ.ContainmentAuthority != nil {
		t.Fatalf("failed commit changed durable marker: %#v", journ)
	}
}

func TestSupervisorHandoffCallbacksPersistExactFenceAndAbortProof(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	handoff := testAppHandoffFixture(48, "windows:48:created-at")
	app := &daemon{store: store}
	if err := app.prepareSupervisorHandoffCallback(key)(handoff); err != nil {
		t.Fatalf("prepareSupervisorHandoffCallback() error = %v", err)
	}
	handoff.TargetIdentity = "mutated-after-prepare"
	journ, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journ.ContainmentHandoff == nil || journ.ContainmentHandoff.TargetIdentity != "windows:48:created-at" {
		t.Fatalf("prepared handoff = %#v, want original exact identity", journ.ContainmentHandoff)
	}

	expected := *journ.ContainmentHandoff
	if err := app.bindSupervisorHandoffCallback(key)(expected, 49, "windows:49:created-at"); err != nil {
		t.Fatalf("bindSupervisorHandoffCallback() error = %v", err)
	}
	bound, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if bound.ContainmentHandoff == nil || bound.ContainmentHandoff.SupervisorPID != 49 || bound.ContainmentHandoff.SupervisorIdentity != "windows:49:created-at" {
		t.Fatalf("bound handoff = %#v, want exact helper identity", bound.ContainmentHandoff)
	}

	abortProof := authority.SupervisorHandoffAbortProof{
		Version: authority.SupervisorHandoffAbortProofVersion, Disposition: authority.SupervisorHandoffAbortDisposition,
		LaunchToken: expected.LaunchToken, JobID: expected.JobID, TargetPID: expected.TargetPID,
		TargetIdentity: expected.TargetIdentity, SupervisorPID: 49, SupervisorIdentity: "windows:49:created-at",
		CreatorSessionID: expected.CreatorSessionID,
	}
	if err := app.clearSupervisorHandoffAfterAbortCallback(key)(*bound.ContainmentHandoff, abortProof); err != nil {
		t.Fatalf("clearSupervisorHandoffAfterAbortCallback() error = %v", err)
	}
	cleared, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.ContainmentHandoff != nil || cleared.ContainmentAuthority != nil || cleared.HasProcessDetails() {
		t.Fatalf("abort clear left containment evidence: %#v", cleared)
	}
}

// TestStoreCallbackFactoriesRefuseClosingStore pins the refusal semantics of
// every factory that performs its own Store CAS: a closing daemon refuses
// before the daemon-wide override runs, a missing Store refuses when no
// override exists, and an override keeps the factory usable without a Store at
// all. Every refusal is the errDaemonStoreClosing sentinel itself.
func TestStoreCallbackFactoriesRefuseClosingStore(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()

	for _, pin := range storeCallbackPins(key) {
		t.Run(pin.name, func(t *testing.T) {
			closing := &daemon{store: store}
			closing.storeClosing = true
			var closingObserved storeCallbackObservations
			pin.installOverride(closing, &closingObserved)
			if err := pin.invoke(closing); !errors.Is(err, errDaemonStoreClosing) {
				t.Fatalf("closing store: error = %v, want %v", err, errDaemonStoreClosing)
			}
			if closingObserved.calls != 0 {
				t.Fatalf("closing store: override calls = %d, want 0", closingObserved.calls)
			}

			if err := pin.invoke(&daemon{}); !errors.Is(err, errDaemonStoreClosing) {
				t.Fatalf("no store and no override: error = %v, want %v", err, errDaemonStoreClosing)
			}

			var overrideObserved storeCallbackObservations
			overrideOnly := &daemon{}
			pin.installOverride(overrideOnly, &overrideObserved)
			if err := pin.invoke(overrideOnly); !errors.Is(err, errStoreCallbackProbe) {
				t.Fatalf("override without a store: error = %v, want %v", err, errStoreCallbackProbe)
			}
			if overrideObserved.calls != pin.wantCalls {
				t.Fatalf("override without a store: override calls = %d, want %d", overrideObserved.calls, pin.wantCalls)
			}
		})
	}
}

// TestStoreCallbackFactoriesHoldStoreLifetimeReadLock pins that the store
// lifetime read lock spans the override invocation of every factory that
// performs its own Store CAS, so daemon shutdown cannot close the Store while a
// runner still decides whether a write outcome is known.
func TestStoreCallbackFactoriesHoldStoreLifetimeReadLock(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()

	for _, pin := range storeCallbackPins(key) {
		t.Run(pin.name, func(t *testing.T) {
			app := &daemon{store: store}
			var observed storeCallbackObservations
			pin.installOverride(app, &observed)
			if err := pin.invoke(app); !errors.Is(err, errStoreCallbackProbe) {
				t.Fatalf("override error = %v, want %v", err, errStoreCallbackProbe)
			}
			if observed.calls != pin.wantCalls {
				t.Fatalf("override calls = %d, want %d", observed.calls, pin.wantCalls)
			}
			if !observed.lockHeld {
				t.Fatal("override ran without the store lifetime read lock")
			}
		})
	}
}

// TestStoreCallbackFactoriesCloneCallerArguments pins that every factory whose
// authority or handoff argument carries an optional creator session id hands
// its override an independent copy: the caller mutates its own copy after the
// call, while the override's copy keeps the original value and never aliases
// the caller's pointer.
func TestStoreCallbackFactoriesCloneCallerArguments(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()

	for _, pin := range storeCallbackClonePins(key) {
		t.Run(pin.name, func(t *testing.T) {
			app := &daemon{store: store}
			var received []*uint32
			pin.installOverride(app, &received)
			invoke, callerSessionID, mutateCaller := pin.buildCaller()
			if err := invoke(app); !errors.Is(err, errStoreCallbackProbe) {
				t.Fatalf("override error = %v, want %v", err, errStoreCallbackProbe)
			}
			if len(received) != pin.wantCalls {
				t.Fatalf("override calls = %d, want %d", len(received), pin.wantCalls)
			}
			original := sessionIDOf(callerSessionID)
			mutateCaller()
			for index, receivedSessionID := range received {
				if got := sessionIDOf(receivedSessionID); got != original {
					t.Fatalf("override call %d received creator session id %d after the caller moved to %d, want the cloned %d", index, got, sessionIDOf(callerSessionID), original)
				}
				if receivedSessionID == callerSessionID {
					t.Fatalf("override call %d aliased the caller's creator session id pointer, want a clone", index)
				}
			}
		})
	}
}

// TestStoreCallbackObserversFollowTheOverride pins the observer order of the
// two factories that notify a process observer: the successful write happens
// first and is the only source of the observed process pair, while a failed
// write notifies nobody.
func TestStoreCallbackObserversFollowTheOverride(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()

	pins := []struct {
		name string
		// wantWrites is the number of override invocations a failed call makes:
		// persistProcessAuthorityCallback retries the write once after a failed
		// attempt.
		wantWrites int
		// wantObserved is the event the observer records after a successful
		// write; commitSupervisorHandoffCallback commits a helper process that
		// differs from the handoff target, so the observer must report the
		// committed journal rather than the callback's argument.
		wantObserved string
		install      func(app *daemon, events *[]string, failWrite bool)
		invoke       func(app *daemon) error
	}{
		{
			name:         "persistProcessAuthorityCallback",
			wantWrites:   2,
			wantObserved: "observe:51:windows:51:created-at",
			install: func(app *daemon, events *[]string, failWrite bool) {
				app.options.recordProcessAuthority = func(_ state.RunKey, _ int, _ string, _ authority.Supervisor, _ time.Time) (state.RunJournal, error) {
					*events = append(*events, "write")
					if failWrite {
						return state.RunJournal{}, errStoreCallbackProbe
					}
					return state.RunJournal{}, nil
				}
				app.options.processObserver = func(_ state.RunKey, pid int, identity string, _ time.Time) {
					*events = append(*events, fmt.Sprintf("observe:%d:%s", pid, identity))
				}
			},
			invoke: func(app *daemon) error {
				value := testProcessPersistenceAuthority(51, "windows:51:created-at")
				return app.persistProcessAuthorityCallback(key)(51, "windows:51:created-at", &value)
			},
		},
		{
			name:         "commitSupervisorHandoffCallback",
			wantWrites:   1,
			wantObserved: "observe:57:windows:57:created-at",
			install: func(app *daemon, events *[]string, failWrite bool) {
				app.options.commitSupervisorHandoff = func(state.RunKey, authority.SupervisorHandoff, time.Time) (state.RunJournal, error) {
					*events = append(*events, "write")
					if failWrite {
						return state.RunJournal{}, errStoreCallbackProbe
					}
					return state.RunJournal{PID: 57, ProcessIdentity: "windows:57:created-at"}, nil
				}
				app.options.processObserver = func(_ state.RunKey, pid int, identity string, _ time.Time) {
					*events = append(*events, fmt.Sprintf("observe:%d:%s", pid, identity))
				}
			},
			invoke: func(app *daemon) error {
				return app.commitSupervisorHandoffCallback(key)(testAppHandoffFixture(56, "windows:56:created-at"), time.Date(2026, 9, 17, 10, 20, 21, 0, time.UTC))
			},
		},
	}

	for _, pin := range pins {
		for _, failWrite := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/failed_write=%t", pin.name, failWrite), func(t *testing.T) {
				app := &daemon{store: store}
				var events []string
				pin.install(app, &events, failWrite)
				err := pin.invoke(app)
				if failWrite {
					if !errors.Is(err, errStoreCallbackProbe) {
						t.Fatalf("override error = %v, want %v", err, errStoreCallbackProbe)
					}
					if want := slices.Repeat([]string{"write"}, pin.wantWrites); !slices.Equal(events, want) {
						t.Fatalf("events after a failed write = %#v, want %#v", events, want)
					}
					return
				}
				if err != nil {
					t.Fatalf("override error = %v, want nil", err)
				}
				if want := []string{"write", pin.wantObserved}; !slices.Equal(events, want) {
					t.Fatalf("events after a durable write = %#v, want %#v", events, want)
				}
			})
		}
	}
}

func callIndex(calls []string, want string) int {
	for index, call := range calls {
		if call == want {
			return index
		}
	}
	return -1
}

func testProcessPersistenceAuthority(pid int, identity string) authority.Supervisor {
	value := testRecoverySupervisorAuthority()
	value.TargetPID = pid
	value.TargetIdentity = identity
	return value
}

func testAppHandoffFixture(pid int, identity string) authority.SupervisorHandoff {
	sessionID := uint32(1)
	return authority.SupervisorHandoff{
		Version: authority.SupervisorHandoffVersion, LaunchToken: strings.Repeat("a", authority.TokenBytes*2),
		Secret: strings.Repeat("b", authority.SecretBytes*2), TargetPID: pid, TargetIdentity: identity,
		PipeToken: strings.Repeat("c", authority.TokenBytes*2), JobID: strings.Repeat("d", authority.TokenBytes*2), CreatorSessionID: &sessionID,
	}
}

// errStoreCallbackProbe is the error every pinned store-callback override
// returns. A pin asserts that a factory propagated exactly this error, which is
// how it proves the override ran instead of the Store method.
var errStoreCallbackProbe = errors.New("store callback override probe")

// storeCallbackObservations is what one pinned override saw while it ran.
type storeCallbackObservations struct {
	calls    int
	lockHeld bool
}

// storeCallbackPin drives one of the nine factories that perform their own
// Store CAS. installOverride sets that factory's daemon-wide option on app so
// the pinned call runs the override instead of the Store, and invoke calls the
// factory with valid arguments; the overrides never touch the Store.
type storeCallbackPin struct {
	name string
	// wantCalls is the number of override invocations one pinned call makes:
	// persistProcessAuthorityCallback retries a failed write once, every other
	// factory once.
	wantCalls       int
	installOverride func(app *daemon, observed *storeCallbackObservations)
	invoke          func(app *daemon) error
}

// storeCallbackClonePin drives one factory whose authority or handoff argument
// carries an optional creator session id. buildCaller creates the caller-owned
// fixture and returns the call that hands it to the factory, the session id
// pointer the caller keeps, and the mutation of the caller's own copy that must
// not reach the override.
type storeCallbackClonePin struct {
	name string
	// wantCalls is the number of override invocations one pinned call makes.
	wantCalls       int
	installOverride func(app *daemon, received *[]*uint32)
	buildCaller     func() (invoke func(app *daemon) error, callerSessionID *uint32, mutateCaller func())
}

// storeCallbackPins covers every factory that performs its own Store CAS.
func storeCallbackPins(key state.RunKey) []storeCallbackPin {
	return []storeCallbackPin{
		{
			name:      "persistProcessAuthorityCallback",
			wantCalls: 2,
			installOverride: func(app *daemon, observed *storeCallbackObservations) {
				app.options.recordProcessAuthority = func(state.RunKey, int, string, authority.Supervisor, time.Time) (state.RunJournal, error) {
					return state.RunJournal{}, observeStoreCallback(app, observed)
				}
			},
			invoke: func(app *daemon) error {
				value := testProcessPersistenceAuthority(51, "windows:51:created-at")
				return app.persistProcessAuthorityCallback(key)(51, "windows:51:created-at", &value)
			},
		},
		{
			name:      "markContainmentUnprovenCallback",
			wantCalls: 1,
			installOverride: func(app *daemon, observed *storeCallbackObservations) {
				app.options.markContainmentUnproven = func(state.RunKey, int, string) (state.RunJournal, error) {
					return state.RunJournal{}, observeStoreCallback(app, observed)
				}
			},
			invoke: func(app *daemon) error {
				return app.markContainmentUnprovenCallback(key)(52, "windows:52:created-at")
			},
		},
		{
			name:      "clearContainmentUnprovenCallback",
			wantCalls: 1,
			installOverride: func(app *daemon, observed *storeCallbackObservations) {
				app.options.clearContainmentUnproven = func(state.RunKey, int, string) (state.RunJournal, error) {
					return state.RunJournal{}, observeStoreCallback(app, observed)
				}
			},
			invoke: func(app *daemon) error {
				return app.clearContainmentUnprovenCallback(key)(53, "windows:53:created-at")
			},
		},
		{
			name:      "persistContainmentStopReceiptCallback",
			wantCalls: 1,
			installOverride: func(app *daemon, observed *storeCallbackObservations) {
				app.options.recordContainmentStopReceipt = func(state.RunKey, int, string, authority.StopReceipt) (state.RunJournal, error) {
					return state.RunJournal{}, observeStoreCallback(app, observed)
				}
			},
			invoke: func(app *daemon) error {
				receipt := testRecoveryStopReceipt(testRecoverySupervisorAuthority())
				return app.persistContainmentStopReceiptCallback(key)(receipt.TargetPID, receipt.TargetIdentity, receipt)
			},
		},
		{
			name:      "prepareSupervisorHandoffCallback",
			wantCalls: 1,
			installOverride: func(app *daemon, observed *storeCallbackObservations) {
				app.options.prepareSupervisorHandoff = func(state.RunKey, authority.SupervisorHandoff) (state.RunJournal, error) {
					return state.RunJournal{}, observeStoreCallback(app, observed)
				}
			},
			invoke: func(app *daemon) error {
				return app.prepareSupervisorHandoffCallback(key)(testAppHandoffFixture(54, "windows:54:created-at"))
			},
		},
		{
			name:      "bindSupervisorHandoffCallback",
			wantCalls: 1,
			installOverride: func(app *daemon, observed *storeCallbackObservations) {
				app.options.bindSupervisorHandoff = func(state.RunKey, authority.SupervisorHandoff, int, string) (state.RunJournal, error) {
					return state.RunJournal{}, observeStoreCallback(app, observed)
				}
			},
			invoke: func(app *daemon) error {
				return app.bindSupervisorHandoffCallback(key)(testAppHandoffFixture(55, "windows:55:created-at"), 55, "windows:55:created-at")
			},
		},
		{
			name:      "commitSupervisorHandoffCallback",
			wantCalls: 1,
			installOverride: func(app *daemon, observed *storeCallbackObservations) {
				app.options.commitSupervisorHandoff = func(state.RunKey, authority.SupervisorHandoff, time.Time) (state.RunJournal, error) {
					return state.RunJournal{}, observeStoreCallback(app, observed)
				}
			},
			invoke: func(app *daemon) error {
				return app.commitSupervisorHandoffCallback(key)(testAppHandoffFixture(56, "windows:56:created-at"), time.Date(2026, 9, 17, 10, 30, 31, 0, time.UTC))
			},
		},
		{
			name:      "clearSupervisorHandoffCallback",
			wantCalls: 1,
			installOverride: func(app *daemon, observed *storeCallbackObservations) {
				app.options.clearSupervisorHandoff = func(state.RunKey, authority.SupervisorHandoff, authority.SupervisorHandoffReleaseProof) (state.RunJournal, error) {
					return state.RunJournal{}, observeStoreCallback(app, observed)
				}
			},
			invoke: func(app *daemon) error {
				return app.clearSupervisorHandoffCallback(key)(testAppHandoffFixture(57, "windows:57:created-at"), authority.SupervisorHandoffReleaseProof{})
			},
		},
		{
			name:      "clearSupervisorHandoffAfterAbortCallback",
			wantCalls: 1,
			installOverride: func(app *daemon, observed *storeCallbackObservations) {
				app.options.clearSupervisorHandoffAfterAbort = func(state.RunKey, authority.SupervisorHandoff, authority.SupervisorHandoffAbortProof) (state.RunJournal, error) {
					return state.RunJournal{}, observeStoreCallback(app, observed)
				}
			},
			invoke: func(app *daemon) error {
				return app.clearSupervisorHandoffAfterAbortCallback(key)(testAppHandoffFixture(58, "windows:58:created-at"), authority.SupervisorHandoffAbortProof{})
			},
		},
	}
}

// storeCallbackClonePins covers every factory that passes an authority or
// handoff argument to its override.
func storeCallbackClonePins(key state.RunKey) []storeCallbackClonePin {
	return []storeCallbackClonePin{
		{
			name:      "persistProcessAuthorityCallback",
			wantCalls: 2,
			installOverride: func(app *daemon, received *[]*uint32) {
				app.options.recordProcessAuthority = func(_ state.RunKey, _ int, _ string, got authority.Supervisor, _ time.Time) (state.RunJournal, error) {
					*received = append(*received, got.CreatorSessionID)
					return state.RunJournal{}, errStoreCallbackProbe
				}
			},
			buildCaller: func() (func(*daemon) error, *uint32, func()) {
				sessionID := uint32(1)
				value := testProcessPersistenceAuthority(51, "windows:51:created-at")
				value.CreatorSessionID = &sessionID
				return func(app *daemon) error {
						return app.persistProcessAuthorityCallback(key)(51, "windows:51:created-at", &value)
					},
					&sessionID,
					func() { sessionID = 99 }
			},
		},
		handoffClonePin(
			"prepareSupervisorHandoffCallback",
			func(app *daemon, override func(authority.SupervisorHandoff) (state.RunJournal, error)) {
				app.options.prepareSupervisorHandoff = func(_ state.RunKey, got authority.SupervisorHandoff) (state.RunJournal, error) {
					return override(got)
				}
			},
			func(app *daemon, handoff authority.SupervisorHandoff) error {
				return app.prepareSupervisorHandoffCallback(key)(handoff)
			},
		),
		handoffClonePin(
			"bindSupervisorHandoffCallback",
			func(app *daemon, override func(authority.SupervisorHandoff) (state.RunJournal, error)) {
				app.options.bindSupervisorHandoff = func(_ state.RunKey, got authority.SupervisorHandoff, _ int, _ string) (state.RunJournal, error) {
					return override(got)
				}
			},
			func(app *daemon, handoff authority.SupervisorHandoff) error {
				return app.bindSupervisorHandoffCallback(key)(handoff, 55, "windows:55:created-at")
			},
		),
		handoffClonePin(
			"commitSupervisorHandoffCallback",
			func(app *daemon, override func(authority.SupervisorHandoff) (state.RunJournal, error)) {
				app.options.commitSupervisorHandoff = func(_ state.RunKey, got authority.SupervisorHandoff, _ time.Time) (state.RunJournal, error) {
					return override(got)
				}
			},
			func(app *daemon, handoff authority.SupervisorHandoff) error {
				return app.commitSupervisorHandoffCallback(key)(handoff, time.Date(2026, 9, 17, 10, 30, 31, 0, time.UTC))
			},
		),
		handoffClonePin(
			"clearSupervisorHandoffCallback",
			func(app *daemon, override func(authority.SupervisorHandoff) (state.RunJournal, error)) {
				app.options.clearSupervisorHandoff = func(_ state.RunKey, got authority.SupervisorHandoff, _ authority.SupervisorHandoffReleaseProof) (state.RunJournal, error) {
					return override(got)
				}
			},
			func(app *daemon, handoff authority.SupervisorHandoff) error {
				return app.clearSupervisorHandoffCallback(key)(handoff, authority.SupervisorHandoffReleaseProof{})
			},
		),
		handoffClonePin(
			"clearSupervisorHandoffAfterAbortCallback",
			func(app *daemon, override func(authority.SupervisorHandoff) (state.RunJournal, error)) {
				app.options.clearSupervisorHandoffAfterAbort = func(_ state.RunKey, got authority.SupervisorHandoff, _ authority.SupervisorHandoffAbortProof) (state.RunJournal, error) {
					return override(got)
				}
			},
			func(app *daemon, handoff authority.SupervisorHandoff) error {
				return app.clearSupervisorHandoffAfterAbortCallback(key)(handoff, authority.SupervisorHandoffAbortProof{})
			},
		),
	}
}

// handoffClonePin builds the clone pin of one handoff factory: setOverride
// installs that factory's daemon-wide option so it records the handoff the
// factory hands over and returns the probe error, and call hands the
// caller-owned fixture to the factory.
func handoffClonePin(name string, setOverride func(app *daemon, override func(authority.SupervisorHandoff) (state.RunJournal, error)), call func(app *daemon, handoff authority.SupervisorHandoff) error) storeCallbackClonePin {
	handoff := testAppHandoffFixture(54, "windows:54:created-at")
	return storeCallbackClonePin{
		name:      name,
		wantCalls: 1,
		installOverride: func(app *daemon, received *[]*uint32) {
			setOverride(app, func(got authority.SupervisorHandoff) (state.RunJournal, error) {
				*received = append(*received, got.CreatorSessionID)
				return state.RunJournal{}, errStoreCallbackProbe
			})
		},
		buildCaller: func() (func(*daemon) error, *uint32, func()) {
			return func(app *daemon) error { return call(app, handoff) },
				handoff.CreatorSessionID,
				func() { *handoff.CreatorSessionID = 99 }
		},
	}
}

// observeStoreCallback records one override invocation: it counts the call,
// notes whether the daemon still held the store lifetime read lock, and returns
// the probe error.
func observeStoreCallback(app *daemon, observed *storeCallbackObservations) error {
	observed.calls++
	observed.lockHeld = observeStoreLifetimeReadLock(app)
	return errStoreCallbackProbe
}

// observeStoreLifetimeReadLock reports whether app still holds the store
// lifetime read lock: TryLock only succeeds when the caller holds no read lock
// at that moment.
func observeStoreLifetimeReadLock(app *daemon) bool {
	if !app.storeLifetimeMu.TryLock() {
		return true
	}
	app.storeLifetimeMu.Unlock()
	return false
}

// sessionIDOf reads the optional creator session id an authority or handoff
// argument carries.
func sessionIDOf(sessionID *uint32) uint32 {
	if sessionID == nil {
		return 0
	}
	return *sessionID
}
