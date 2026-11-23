package app

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/app/apptest"
	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestProcessRecorderDispatchesEveryLegacyWriteToItsStoreMethod(t *testing.T) {
	now := time.Date(2026, 11, 22, 1, 2, 3, 0, time.UTC)
	calls := []string{}
	daemon, store, key := recorderTestDaemon(t, &calls, now)
	recorder := daemon.processRecorder(key, func(pid int, identity string) error {
		return daemon.persistProcessMarker(key, pid, identity, now)
	})
	want := execution.RecorderCapabilities{
		PrepareSupervisorHandoff:           true,
		BindSupervisorHandoff:              true,
		CommitSupervisorHandoff:            true,
		RecordSupervisorHandoffStopReceipt: true,
		ClearSupervisorHandoff:             true,
		PersistProcess:                     true,
		PersistProcessWithAuthority:        true,
		PersistProcessAuthority:            true,
		PersistContainmentStopReceipt:      true,
		PersistContainmentUnproven:         true,
		RecordSupervisorRecoveryScanSeal:   true,
	}
	if capabilities := recorder.Capabilities(); capabilities != want {
		t.Fatalf("process recorder capabilities = %+v, want every write", capabilities)
	}

	value := testProcessPersistenceAuthority(42, "test:42")
	receipt := testRecoveryStopReceipt(value)
	if err := recorder.PersistProcess(42, "test:42"); err != nil {
		t.Fatalf("PersistProcess() error = %v", err)
	}
	journal := recorderTestJournal(t, store, key)
	if journal.PID != 42 || journal.ProcessIdentity != "test:42" || !journal.StartedAt.Equal(now) {
		t.Fatalf("journal after PersistProcess = %#v, want the process marker", journal)
	}
	if err := recorder.PersistProcessWithAuthority(42, "test:42", &value); err != nil {
		t.Fatalf("PersistProcessWithAuthority() error = %v", err)
	}
	if err := recorder.PersistProcessAuthority(42, "test:42", &value); err != nil {
		t.Fatalf("PersistProcessAuthority() error = %v", err)
	}
	journal = recorderTestJournal(t, store, key)
	if journal.ContainmentAuthority == nil || journal.ContainmentAuthority.TargetPID != 42 {
		t.Fatalf("journal after the authority writes = %#v, want the containment authority", journal)
	}
	if err := recorder.PersistContainmentStopReceipt(42, "test:42", receipt); err != nil {
		t.Fatalf("PersistContainmentStopReceipt() error = %v", err)
	}
	journal = recorderTestJournal(t, store, key)
	if journal.ContainmentAuthority == nil || journal.ContainmentAuthority.StopReceipt == nil || !journal.ContainmentAuthority.StopReceipt.Equal(receipt) {
		t.Fatalf("journal after PersistContainmentStopReceipt = %#v, want the stop receipt", journal)
	}
	if err := recorder.PersistContainmentUnproven(42, "test:42"); err != nil {
		t.Fatalf("PersistContainmentUnproven() error = %v", err)
	}
	journal = recorderTestJournal(t, store, key)
	if !journal.ContainmentUnproven {
		t.Fatalf("journal after PersistContainmentUnproven = %#v, want the unproven marker", journal)
	}

	handoffDaemon, handoffStore, handoffKey := recorderTestDaemon(t, &calls, now)
	handoffRecorder := handoffDaemon.processRecorder(handoffKey, func(pid int, identity string) error {
		return handoffDaemon.persistProcessMarker(handoffKey, pid, identity, now)
	})
	handoff := testRecoveryHandoffFixture(46, "windows:46:created-at")
	if err := handoffRecorder.PrepareSupervisorHandoff(handoff); err != nil {
		t.Fatalf("PrepareSupervisorHandoff() error = %v", err)
	}
	if err := handoffRecorder.BindSupervisorHandoff(handoff, 47, "windows:47:bound-at"); err != nil {
		t.Fatalf("BindSupervisorHandoff() error = %v", err)
	}
	journal = recorderTestJournal(t, handoffStore, handoffKey)
	if journal.ContainmentHandoff == nil || journal.ContainmentHandoff.SupervisorPID != 47 ||
		journal.ContainmentHandoff.SupervisorIdentity != "windows:47:bound-at" ||
		journal.ContainmentHandoff.TargetPID != 46 {
		t.Fatalf("journal after BindSupervisorHandoff = %#v, want the bound handoff", journal)
	}
	bound := *journal.ContainmentHandoff
	handoffValue, err := bound.ToSupervisor()
	if err != nil {
		t.Fatalf("bound handoff has no supervisor projection: %v", err)
	}
	handoffReceipt := testRecoveryStopReceipt(handoffValue)
	if !handoffReceipt.ValidForHandoff(bound) {
		t.Fatal("handoff stop receipt fixture does not match its handoff")
	}
	if err := handoffRecorder.RecordSupervisorHandoffStopReceipt(bound, handoffReceipt); err != nil {
		t.Fatalf("RecordSupervisorHandoffStopReceipt() error = %v", err)
	}
	bound.StopReceipt = &handoffReceipt
	if err := handoffRecorder.ClearSupervisorHandoff(bound, bound.ReleaseProof()); err != nil {
		t.Fatalf("ClearSupervisorHandoff() error = %v", err)
	}
	journal = recorderTestJournal(t, handoffStore, handoffKey)
	if journal.ContainmentHandoff != nil {
		t.Fatalf("journal after ClearSupervisorHandoff = %#v, want no pending handoff", journal)
	}

	commitDaemon, commitStore, commitKey := recorderTestDaemon(t, &calls, now)
	commitRecorder := commitDaemon.processRecorder(commitKey, func(pid int, identity string) error {
		return commitDaemon.persistProcessMarker(commitKey, pid, identity, now)
	})
	commitHandoff := testRecoveryHandoffFixture(48, "windows:48:created-at")
	if err := commitRecorder.PrepareSupervisorHandoff(commitHandoff); err != nil {
		t.Fatalf("PrepareSupervisorHandoff() error = %v", err)
	}
	if err := commitRecorder.BindSupervisorHandoff(commitHandoff, 49, "windows:49:bound-at"); err != nil {
		t.Fatalf("BindSupervisorHandoff() error = %v", err)
	}
	journal = recorderTestJournal(t, commitStore, commitKey)
	if journal.ContainmentHandoff == nil {
		t.Fatalf("journal after the commit-route bind = %#v, want the bound handoff", journal)
	}
	if err := commitRecorder.CommitSupervisorHandoff(*journal.ContainmentHandoff, now); err != nil {
		t.Fatalf("CommitSupervisorHandoff() error = %v", err)
	}
	journal = recorderTestJournal(t, commitStore, commitKey)
	if journal.PID != 48 || journal.ProcessIdentity != "windows:48:created-at" ||
		journal.StartedAt.IsZero() || journal.ContainmentAuthority == nil || journal.ContainmentHandoff != nil {
		t.Fatalf("journal after CommitSupervisorHandoff = %#v, want the promoted process marker", journal)
	}

	wantCalls := []string{
		"SetProcessDetails",
		"SetProcessDetailsWithAuthority",
		"SetProcessDetailsWithAuthority",
		"RecordContainmentStopReceipt",
		"MarkContainmentUnproven",
		"PrepareSupervisorHandoff",
		"BindSupervisorHandoff",
		"RecordSupervisorHandoffStopReceipt",
		"ClearSupervisorHandoff",
		"PrepareSupervisorHandoff",
		"BindSupervisorHandoff",
		"CommitSupervisorHandoff",
	}
	if !slices.Equal(calls, wantCalls) {
		t.Fatalf("store dispatch = %#v, want %#v", calls, wantCalls)
	}
}

func recorderTestDaemon(t *testing.T, calls *[]string, now time.Time) (*daemon, *state.Store, state.RunKey) {
	t.Helper()
	store, key := claimedStore(t)
	t.Cleanup(func() { _ = store.Close() })
	fault := apptest.NewFaultStoreFrom(store)
	record := func(method string) {
		*calls = append(*calls, method)
	}
	fault.OnSetProcessDetails = func(gotKey state.RunKey, pid int, identity string, startedAt time.Time) (state.RunJournal, error) {
		if gotKey != key {
			t.Errorf("SetProcessDetails key = %+v, want %+v", gotKey, key)
		}
		record("SetProcessDetails")
		return fault.Store.SetProcessDetails(gotKey, pid, identity, startedAt)
	}
	fault.OnSetProcessDetailsWithAuthority = func(gotKey state.RunKey, pid int, identity string, startedAt time.Time, value authority.Supervisor) (state.RunJournal, error) {
		record("SetProcessDetailsWithAuthority")
		return fault.Store.SetProcessDetailsWithAuthority(gotKey, pid, identity, startedAt, value)
	}
	fault.OnMarkContainmentUnproven = func(gotKey state.RunKey, pid int, identity string) (state.RunJournal, error) {
		record("MarkContainmentUnproven")
		return fault.Store.MarkContainmentUnproven(gotKey, pid, identity)
	}
	fault.OnRecordContainmentStopReceipt = func(gotKey state.RunKey, pid int, identity string, receipt authority.StopReceipt) (state.RunJournal, error) {
		record("RecordContainmentStopReceipt")
		return fault.Store.RecordContainmentStopReceipt(gotKey, pid, identity, receipt)
	}
	fault.OnPrepareSupervisorHandoff = func(gotKey state.RunKey, handoff authority.SupervisorHandoff) (state.RunJournal, error) {
		record("PrepareSupervisorHandoff")
		return fault.Store.PrepareSupervisorHandoff(gotKey, handoff)
	}
	fault.OnBindSupervisorHandoff = func(gotKey state.RunKey, expected authority.SupervisorHandoff, pid int, identity string) (state.RunJournal, error) {
		record("BindSupervisorHandoff")
		return fault.Store.BindSupervisorHandoff(gotKey, expected, pid, identity)
	}
	fault.OnCommitSupervisorHandoff = func(gotKey state.RunKey, expected authority.SupervisorHandoff, startedAt time.Time) (state.RunJournal, error) {
		record("CommitSupervisorHandoff")
		return fault.Store.CommitSupervisorHandoff(gotKey, expected, startedAt)
	}
	fault.OnRecordSupervisorHandoffStopReceipt = func(gotKey state.RunKey, expected authority.SupervisorHandoff, receipt authority.StopReceipt) (state.RunJournal, error) {
		record("RecordSupervisorHandoffStopReceipt")
		return fault.Store.RecordSupervisorHandoffStopReceipt(gotKey, expected, receipt)
	}
	fault.OnClearSupervisorHandoff = func(gotKey state.RunKey, expected authority.SupervisorHandoff, proof authority.SupervisorHandoffReleaseProof) (state.RunJournal, error) {
		record("ClearSupervisorHandoff")
		return fault.Store.ClearSupervisorHandoff(gotKey, expected, proof)
	}
	return &daemon{store: fault, options: options{clock: func() time.Time { return now }}}, store, key
}

func recorderTestJournal(t *testing.T, store *state.Store, key state.RunKey) state.RunJournal {
	t.Helper()
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatalf("LoadJournal() error = %v", err)
	}
	return journal
}

func TestProcessRecorderRecordsTheRecoveryScanSealThroughItsBoundKey(t *testing.T) {
	now := time.Date(2026, 11, 23, 1, 2, 3, 0, time.UTC)
	store, key := claimedStore(t)
	t.Cleanup(func() { _ = store.Close() })
	value := testProcessPersistenceAuthority(42, "test:42")
	type sealCall struct {
		key      state.RunKey
		expected authority.Supervisor
		sequence uint64
	}
	var calls []sealCall
	fault := apptest.NewFaultStoreFrom(store)
	record := func(gotKey state.RunKey, expected authority.Supervisor, sequence uint64) state.RunJournal {
		calls = append(calls, sealCall{key: gotKey, expected: expected, sequence: sequence})
		return state.RunJournal{}
	}
	fault.OnRecordSupervisorRecoveryScanSeal = func(gotKey state.RunKey, expected authority.Supervisor, sequence uint64) (state.RunJournal, error) {
		return record(gotKey, expected, sequence), errors.New("durable recovery seal write failed")
	}
	daemon := &daemon{store: fault}
	recorder := daemon.processRecorder(key, func(pid int, identity string) error {
		return daemon.persistProcessMarker(key, pid, identity, now)
	})
	err := recorder.RecordSupervisorRecoveryScanSeal(value, 2)
	if err == nil || !strings.Contains(err.Error(), "replay exact helper scan seal") {
		t.Fatalf("failing recovery seal error = %v, want the exact replay joined error", err)
	}
	if len(calls) != 2 {
		t.Fatalf("recovery seal store calls after a failure = %d, want the one exact replay", len(calls))
	}
	fault.OnRecordSupervisorRecoveryScanSeal = func(gotKey state.RunKey, expected authority.Supervisor, sequence uint64) (state.RunJournal, error) {
		return record(gotKey, expected, sequence), nil
	}
	if err := recorder.RecordSupervisorRecoveryScanSeal(value, 2); err != nil {
		t.Fatalf("recovery seal error = %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("recovery seal store calls after a success = %d, want exactly one more attempt", len(calls))
	}
	for _, call := range calls {
		if call.key != key || !call.expected.Equal(value) || call.sequence != 2 {
			t.Fatalf("recovery seal call = %+v, want the bound key, exact authority and sequence", call)
		}
	}
}
