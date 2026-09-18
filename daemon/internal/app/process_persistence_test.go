package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
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
	case <-time.After(time.Second):
		t.Fatal("native turn did not start")
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
