package state

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

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
