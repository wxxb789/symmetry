package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
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
