package state

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestHasProcessDetailsIncludesPartialMarkers(t *testing.T) {
	for _, journal := range []RunJournal{
		{PID: 42}, {ProcessIdentity: "identity"}, {StartedAt: time.Now().UTC()},
	} {
		if !journal.HasProcessDetails() {
			t.Fatal("partial process marker was considered stopped")
		}
	}
	if (RunJournal{}).HasProcessDetails() {
		t.Fatal("empty process marker was considered unresolved")
	}
}

func TestSetProcessDetailsWithAuthorityPersistsCompletePair(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("atomic-process-authority", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	startedAt := time.Date(2026, 9, 15, 1, 2, 3, 0, time.UTC)
	authorityValue := testContainmentAuthority(99, "windows:99:created-at")

	updated, err := store.SetProcessDetailsWithAuthority(journal.Key(), authorityValue.TargetPID, authorityValue.TargetIdentity, startedAt, authorityValue)
	if err != nil {
		t.Fatalf("SetProcessDetailsWithAuthority() error = %v", err)
	}
	if updated.PID != authorityValue.TargetPID || updated.ProcessIdentity != authorityValue.TargetIdentity || !updated.StartedAt.Equal(startedAt) || updated.ContainmentAuthority == nil || !updated.ContainmentAuthority.Equal(authorityValue) {
		t.Fatalf("updated journal = %#v, want complete process-authority pair", updated)
	}

	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatalf("LoadJournal() error = %v", err)
	}
	if loaded.PID != authorityValue.TargetPID || loaded.ProcessIdentity != authorityValue.TargetIdentity || !loaded.StartedAt.Equal(startedAt) || loaded.ContainmentAuthority == nil || !loaded.ContainmentAuthority.Equal(authorityValue) {
		t.Fatalf("loaded journal = %#v, observed marker-only or incomplete authority state", loaded)
	}
}

func TestSetProcessDetailsWithAuthorityPreWriteFailurePreservesOldState(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("atomic-process-authority-prewrite", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	startedAt := time.Date(2026, 9, 15, 2, 2, 3, 0, time.UTC)
	authorityValue := testContainmentAuthority(100, "windows:100:created-at")
	restore := store.SetAtomicWriterForTesting(func(string, []byte) error { return errors.New("injected pre-write failure") })
	_, err := store.SetProcessDetailsWithAuthority(journal.Key(), authorityValue.TargetPID, authorityValue.TargetIdentity, startedAt, authorityValue)
	restore()
	if err == nil {
		t.Fatal("SetProcessDetailsWithAuthority() ignored the pre-write failure")
	}

	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatalf("LoadJournal() error = %v", err)
	}
	if loaded.PID != 0 || loaded.ProcessIdentity != "" || !loaded.StartedAt.IsZero() || loaded.ContainmentAuthority != nil {
		t.Fatalf("pre-write failure changed old journal state: %#v", loaded)
	}
}

func TestSetProcessDetailsWithAuthorityRetriesPostRenameUnknownOutcomeAsCompletePair(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("atomic-process-authority-postrename", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	startedAt := time.Date(2026, 9, 15, 3, 2, 3, 0, time.UTC)
	authorityValue := testContainmentAuthority(101, "windows:101:created-at")
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if err := writeAtomic(path, data); err != nil {
			return err
		}
		return errors.New("injected post-rename unknown outcome")
	})
	_, err := store.SetProcessDetailsWithAuthority(journal.Key(), authorityValue.TargetPID, authorityValue.TargetIdentity, startedAt, authorityValue)
	restore()
	if err == nil {
		t.Fatal("SetProcessDetailsWithAuthority() hid the post-rename unknown outcome")
	}

	unknown, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatalf("LoadJournal() after unknown outcome = %v", err)
	}
	if unknown.PID != authorityValue.TargetPID || unknown.ProcessIdentity != authorityValue.TargetIdentity || !unknown.StartedAt.Equal(startedAt) || unknown.ContainmentAuthority == nil || !unknown.ContainmentAuthority.Equal(authorityValue) {
		t.Fatalf("post-rename journal = %#v, want complete pair", unknown)
	}

	replayed, err := store.SetProcessDetailsWithAuthority(journal.Key(), authorityValue.TargetPID, authorityValue.TargetIdentity, startedAt.Add(time.Minute), authorityValue)
	if err != nil {
		t.Fatalf("replayed SetProcessDetailsWithAuthority() error = %v", err)
	}
	if replayed.PID != authorityValue.TargetPID || replayed.ProcessIdentity != authorityValue.TargetIdentity || !replayed.StartedAt.Equal(startedAt) || replayed.ContainmentAuthority == nil || !replayed.ContainmentAuthority.Equal(authorityValue) {
		t.Fatalf("replayed journal = %#v, want original complete pair", replayed)
	}
}

func TestSetProcessDetailsWithAuthorityFailsClosedForMismatchesAndCleanup(t *testing.T) {
	tests := []struct {
		name      string
		journal   RunJournal
		pid       int
		identity  string
		mutate    func(*authority.Supervisor)
		wantState func(RunJournal) bool
	}{
		{
			name:     "authority target pid mismatch",
			journal:  stoppedTestJournal("atomic-process-authority-mismatch-pid", 1),
			pid:      102,
			identity: "windows:102:created-at",
			mutate:   func(value *authority.Supervisor) { value.TargetPID++ },
			wantState: func(journal RunJournal) bool {
				return journal.PID == 0 && journal.ProcessIdentity == "" && journal.StartedAt.IsZero() && journal.ContainmentAuthority == nil
			},
		},
		{
			name:     "authority target identity mismatch",
			journal:  stoppedTestJournal("atomic-process-authority-mismatch-identity", 1),
			pid:      103,
			identity: "windows:103:created-at",
			mutate:   func(value *authority.Supervisor) { value.TargetIdentity = "windows:other" },
			wantState: func(journal RunJournal) bool {
				return journal.PID == 0 && journal.ProcessIdentity == "" && journal.StartedAt.IsZero() && journal.ContainmentAuthority == nil
			},
		},
		{
			name:     "existing owner mismatch",
			journal:  testJournal("atomic-process-authority-existing-owner", 1),
			pid:      106,
			identity: "windows:106:created-at",
			wantState: func(journal RunJournal) bool {
				return journal.PID == 42 && journal.ProcessIdentity == "windows:42:created-at" && !journal.StartedAt.IsZero() && journal.ContainmentAuthority == nil
			},
		},
		{
			name: "missing claim",
			journal: func() RunJournal {
				journal := stoppedTestJournal("atomic-process-authority-no-claim", 1)
				journal.LeaseToken = ""
				journal.LeaseExpiresAt = time.Time{}
				return journal
			}(),
			pid:      104,
			identity: "windows:104:created-at",
			wantState: func(journal RunJournal) bool {
				return journal.PID == 0 && journal.ProcessIdentity == "" && journal.StartedAt.IsZero() && journal.ContainmentAuthority == nil
			},
		},
		{
			name: "cleanup pending",
			journal: func() RunJournal {
				journal := stoppedTestJournal("atomic-process-authority-cleanup", 1)
				journal.LocalState = "cleanup_pending"
				journal.TerminalState = "completed"
				journal.TerminalPendingAt = time.Date(2026, 9, 15, 4, 2, 3, 0, time.UTC)
				journal.TerminalVerdict = TerminalVerdictAccepted
				journal.TerminalResolvedAt = journal.TerminalPendingAt
				return journal
			}(),
			pid:      105,
			identity: "windows:105:created-at",
			wantState: func(journal RunJournal) bool {
				return journal.PID == 0 && journal.ProcessIdentity == "" && journal.StartedAt.IsZero() && journal.ContainmentAuthority == nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := mustStore(t)
			if err := store.SaveJournal(test.journal); err != nil {
				t.Fatal(err)
			}
			value := testContainmentAuthority(test.pid, test.identity)
			if test.mutate != nil {
				test.mutate(&value)
			}
			if _, err := store.SetProcessDetailsWithAuthority(test.journal.Key(), test.pid, test.identity, time.Date(2026, 9, 15, 5, 2, 3, 0, time.UTC), value); err == nil {
				t.Fatal("SetProcessDetailsWithAuthority() accepted an invalid mutation")
			}
			loaded, err := store.LoadJournal(test.journal.Key())
			if err != nil {
				t.Fatalf("LoadJournal() error = %v", err)
			}
			if !test.wantState(loaded) {
				t.Fatalf("failed mutation changed journal: %#v", loaded)
			}
		})
	}
}

func TestSetProcessDetailsWithAuthorityPreservesValidStopReceiptOnReplay(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("atomic-process-authority-receipt", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	authorityValue := testContainmentAuthority(journal.PID, journal.ProcessIdentity)
	if _, err := store.SetContainmentAuthority(journal.Key(), journal.PID, journal.ProcessIdentity, authorityValue); err != nil {
		t.Fatal(err)
	}
	receipt := authority.StopReceipt{
		Version:            authority.SupervisorVersion,
		Status:             "stopped",
		TargetPID:          authorityValue.TargetPID,
		TargetIdentity:     authorityValue.TargetIdentity,
		PipeToken:          authorityValue.PipeToken,
		JobID:              authorityValue.JobID,
		SupervisorPID:      authorityValue.SupervisorPID,
		SupervisorIdentity: authorityValue.SupervisorIdentity,
	}
	if _, err := store.RecordContainmentStopReceipt(journal.Key(), journal.PID, journal.ProcessIdentity, receipt); err != nil {
		t.Fatal(err)
	}

	replayed, err := store.SetProcessDetailsWithAuthority(journal.Key(), journal.PID, journal.ProcessIdentity, journal.StartedAt.Add(time.Minute), authorityValue)
	if err != nil {
		t.Fatalf("SetProcessDetailsWithAuthority() replay error = %v", err)
	}
	if replayed.ContainmentAuthority == nil || replayed.ContainmentAuthority.StopReceipt == nil || *replayed.ContainmentAuthority.StopReceipt != receipt {
		t.Fatalf("replay replaced valid stop receipt: %#v", replayed.ContainmentAuthority)
	}
}

func testContainmentAuthority(pid int, identity string) authority.Supervisor {
	return authority.Supervisor{
		Version:            authority.SupervisorVersion,
		Secret:             strings.Repeat("a", authority.SecretBytes*2),
		TargetPID:          pid,
		TargetIdentity:     identity,
		PipeToken:          strings.Repeat("b", authority.TokenBytes*2),
		JobID:              strings.Repeat("c", authority.TokenBytes*2),
		SupervisorPID:      999,
		SupervisorIdentity: "windows:999:supervisor",
	}
}

func TestContainmentAuthorityAndStopReceiptAreDurableAndCompareCleared(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("containment-authority", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	value := authority.Supervisor{
		Version:            authority.SupervisorVersion,
		Secret:             "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TargetPID:          journal.PID,
		TargetIdentity:     journal.ProcessIdentity,
		PipeToken:          "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		JobID:              "cccccccccccccccccccccccccccccccc",
		SupervisorPID:      99,
		SupervisorIdentity: "windows:99:supervisor",
	}
	if _, err := store.SetContainmentAuthority(journal.Key(), journal.PID, journal.ProcessIdentity, value); err != nil {
		t.Fatal(err)
	}
	receipt := authority.StopReceipt{
		Version:            authority.SupervisorVersion,
		Status:             "stopped",
		TargetPID:          value.TargetPID,
		TargetIdentity:     value.TargetIdentity,
		PipeToken:          value.PipeToken,
		JobID:              value.JobID,
		SupervisorPID:      value.SupervisorPID,
		SupervisorIdentity: value.SupervisorIdentity,
	}
	if _, err := store.RecordContainmentStopReceipt(journal.Key(), journal.PID, journal.ProcessIdentity, receipt); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil || loaded.ContainmentAuthority == nil || loaded.ContainmentAuthority.StopReceipt == nil {
		t.Fatalf("durable containment authority = %+v, err = %v", loaded.ContainmentAuthority, err)
	}
	if !loaded.ContainmentAuthority.StopReceipt.ValidFor(*loaded.ContainmentAuthority) {
		t.Fatal("durable stop receipt did not match its authority")
	}
	cleared, err := store.ClearProcessDetails(journal.Key(), journal.PID, journal.ProcessIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if cleared.ContainmentAuthority != nil || cleared.HasProcessDetails() {
		t.Fatalf("clear retained process authority or marker: %+v", cleared)
	}
}

func TestLegacyProcessMarkerHasNoImplicitContainmentAuthority(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("legacy-authority", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ContainmentAuthority != nil {
		t.Fatal("legacy process marker unexpectedly gained containment authority")
	}
}

func TestProcessMarkerBlocksCleanupAndJournalDeletion(t *testing.T) {
	for _, localState := range []string{"terminal_pending", "cleanup_pending"} {
		t.Run(localState, func(t *testing.T) {
			store := mustStore(t)
			journal := testJournal("process-marker", 1)
			journal.LocalState = localState
			journal.TerminalState = "completed"
			journal.TerminalPendingAt = journal.StartedAt.Add(time.Second)
			journal.TerminalVerdict = TerminalVerdictAccepted
			journal.TerminalResolvedAt = journal.TerminalPendingAt.Add(time.Second)
			if err := store.SaveJournal(journal); err != nil {
				t.Fatal(err)
			}
			if _, err := store.EnterCleanupPending(journal.Key()); err == nil {
				t.Error("cleanup accepted an unresolved process marker")
			}
			if err := store.DeleteJournal(journal.Key()); err == nil {
				t.Fatal("journal deletion erased an unresolved process marker")
			}
			if _, err := store.ClearProcessDetails(journal.Key(), journal.PID, journal.ProcessIdentity); err != nil {
				t.Fatal(err)
			}
			if _, err := store.EnterCleanupPending(journal.Key()); err != nil {
				t.Fatalf("confirmed process stop did not release cleanup: %v", err)
			}
			if err := store.DeleteJournal(journal.Key()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTerminalRejectionPreservesUnresolvedProcessMarker(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("rejected-process-marker", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	pendingAt := journal.StartedAt.Add(time.Second)
	if _, err := store.QueueTerminalTransitionAt(journal.Key(), protocol.StateTransitionRequest{TransitionID: "failed-1", State: "failed", Payload: json.RawMessage(`{}`)}, pendingAt); err != nil {
		t.Fatal(err)
	}
	resolvedAt := pendingAt.Add(time.Second)
	for _, at := range []time.Time{resolvedAt, resolvedAt.Add(time.Minute)} {
		resolved, err := store.ResolveTerminalForCleanup(journal.Key(), TerminalVerdictOwnershipLost, at)
		if err != nil {
			t.Fatal(err)
		}
		if resolved.LocalState != "terminal_pending" || resolved.PID != journal.PID || resolved.ProcessIdentity != journal.ProcessIdentity || !resolved.StartedAt.Equal(journal.StartedAt) || resolved.TerminalVerdict != TerminalVerdictOwnershipLost || !resolved.TerminalResolvedAt.Equal(resolvedAt) || len(resolved.PendingTransitions) != 0 {
			t.Fatalf("terminal verdict erased stop evidence or failed to retire delivery: %+v", resolved)
		}
	}
	if _, err := store.ClearProcessDetails(journal.Key(), journal.PID, journal.ProcessIdentity); err != nil {
		t.Fatal(err)
	}
	resolved, err := store.ResolveTerminalForCleanup(journal.Key(), TerminalVerdictOwnershipLost, resolvedAt.Add(time.Hour))
	if err != nil || resolved.LocalState != "cleanup_pending" || !resolved.TerminalResolvedAt.Equal(resolvedAt) {
		t.Fatalf("confirmed process stop did not release conclusive cleanup: %+v, %v", resolved, err)
	}
}

func TestCleanupPendingRejectsNewProcessRegistration(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("cleanup-registration", 1)
	journal.PID, journal.ProcessIdentity, journal.StartedAt = 0, "", time.Time{}
	journal.LocalState = "cleanup_pending"
	journal.TerminalState = "completed"
	journal.TerminalPendingAt = time.Now().UTC()
	journal.TerminalVerdict = TerminalVerdictAccepted
	journal.TerminalResolvedAt = journal.TerminalPendingAt
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetProcessDetails(journal.Key(), 99, "process:99", journal.TerminalResolvedAt); err == nil {
		t.Error("registered a new process after cleanup became eligible")
	}
	journal.PID, journal.ProcessIdentity, journal.StartedAt = 99, "process:99", journal.TerminalResolvedAt
	if err := store.SaveJournal(journal); err == nil {
		t.Error("complete journal replacement registered a process during cleanup")
	}
}

func TestProcessStopPersistenceFailureRetainsCleanupBarrierAcrossRestart(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	journal := testJournal("stop-write-failure", 1)
	journal.LocalState = "terminal_pending"
	journal.TerminalState = "completed"
	journal.TerminalPendingAt = journal.StartedAt.Add(time.Second)
	journal.TerminalVerdict = TerminalVerdictAccepted
	journal.TerminalResolvedAt = journal.TerminalPendingAt
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	restore := store.SetAtomicWriterForTesting(func(string, []byte) error { return errors.New("injected stop persistence failure") })
	_, err = store.ClearProcessDetails(journal.Key(), journal.PID, journal.ProcessIdentity)
	restore()
	if err == nil {
		t.Fatal("process marker clear ignored persistence failure")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	loaded, err := restarted.LoadJournal(journal.Key())
	if err != nil || loaded.PID != journal.PID || loaded.ProcessIdentity != journal.ProcessIdentity || loaded.TerminalVerdict != TerminalVerdictAccepted || loaded.LocalState != "terminal_pending" {
		t.Fatalf("restart lost uncommitted stop evidence: %+v, %v", loaded, err)
	}
	if err := restarted.DeleteJournal(journal.Key()); err == nil {
		t.Fatal("restart treated an uncommitted stop as cleanup authority")
	}
	if _, err := restarted.ClearProcessDetails(journal.Key(), journal.PID, journal.ProcessIdentity+"-different"); err == nil {
		t.Fatal("mismatched stop witness cleared the retained process")
	}
	if _, err := restarted.ClearProcessDetails(journal.Key(), journal.PID, journal.ProcessIdentity); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.EnterCleanupPending(journal.Key()); err != nil {
		t.Fatal(err)
	}
	if err := restarted.DeleteJournal(journal.Key()); err != nil {
		t.Fatal(err)
	}
}

func TestSaveJournalCannotBypassProcessMarkerMutations(t *testing.T) {
	for _, test := range []struct {
		name             string
		initiallyStopped bool
		change           func(*RunJournal)
	}{
		{name: "clear", change: func(j *RunJournal) { j.PID, j.ProcessIdentity, j.StartedAt = 0, "", time.Time{} }},
		{name: "replace", change: func(j *RunJournal) { j.PID, j.ProcessIdentity = 99, "different:99" }},
		{name: "rewrite start time", change: func(j *RunJournal) { j.StartedAt = j.StartedAt.Add(time.Second) }},
		{name: "register", initiallyStopped: true, change: func(j *RunJournal) { j.PID, j.ProcessIdentity, j.StartedAt = 99, "new:99", time.Now().UTC() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := mustStore(t)
			journal := testJournal("marker-replacement", 1)
			if test.initiallyStopped {
				journal = stoppedTestJournal("marker-replacement", 1)
			}
			if err := store.SaveJournal(journal); err != nil {
				t.Fatal(err)
			}
			test.change(&journal)
			if err := store.SaveJournal(journal); err == nil {
				t.Fatal("complete replacement bypassed process marker ownership")
			}
		})
	}
}

func TestProcessRegistrationCannotReplaceAnUnclearedOwner(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("process-registration", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetProcessDetails(journal.Key(), 99, "different:99", journal.StartedAt); err == nil {
		t.Error("process registration replaced an unresolved owner")
	}
	replayed, err := store.SetProcessDetails(journal.Key(), journal.PID, journal.ProcessIdentity, journal.StartedAt.Add(time.Minute))
	if err != nil || !replayed.StartedAt.Equal(journal.StartedAt) {
		t.Fatalf("same process registration did not preserve its original start time: %+v, %v", replayed, err)
	}
}

func TestCleanupLifecycleRequiresDedicatedTransition(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("cleanup-lifecycle", 1)
	journal.LocalState = "terminal_pending"
	journal.TerminalState = "completed"
	journal.TerminalPendingAt = time.Now().UTC()
	journal.TerminalVerdict = TerminalVerdictAccepted
	journal.TerminalResolvedAt = journal.TerminalPendingAt
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	copy := journal
	copy.LocalState = "cleanup_pending"
	if err := store.SaveJournal(copy); err == nil {
		t.Error("SaveJournal bypassed EnterCleanupPending")
	}
	if _, err := store.SetLocalState(journal.Key(), "cleanup_pending"); err == nil {
		t.Error("SetLocalState bypassed EnterCleanupPending")
	}
	if _, err := store.EnterCleanupPending(journal.Key()); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveJournal(journal); err == nil {
		t.Error("SaveJournal reopened a cleanup-pending execution")
	}
	if _, err := store.SetLocalState(journal.Key(), "terminal_pending"); err == nil {
		t.Error("SetLocalState reopened a cleanup-pending execution")
	}
}
