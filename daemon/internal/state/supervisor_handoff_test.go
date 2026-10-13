package state

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

func TestSupervisorHandoffPrepareBindCommitIsAtomicAndIdempotent(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("handoff-commit", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	handoff := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
	prepared, err := store.PrepareSupervisorHandoff(journal.Key(), handoff)
	if err != nil {
		t.Fatalf("PrepareSupervisorHandoff() error = %v", err)
	}
	if prepared.ContainmentHandoff == nil || prepared.HasProcessDetails() || prepared.HasPendingContainment() == false {
		t.Fatalf("prepared journal = %#v, want pending handoff without process marker", prepared)
	}

	writes := 0
	previous := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAtomic(path, data)
	})
	replayed, err := store.PrepareSupervisorHandoff(journal.Key(), handoff)
	previous()
	if err != nil || replayed.ContainmentHandoff == nil {
		t.Fatalf("idempotent PrepareSupervisorHandoff() = %#v, %v", replayed, err)
	}
	if writes != 1 {
		t.Fatalf("idempotent PrepareSupervisorHandoff() writes = %d, want persistence barrier", writes)
	}

	bound, err := store.BindSupervisorHandoff(journal.Key(), handoff, 99, "windows:99:created-at")
	if err != nil {
		t.Fatalf("BindSupervisorHandoff() error = %v", err)
	}
	if bound.ContainmentHandoff == nil || bound.ContainmentHandoff.SupervisorPID != 99 || bound.ContainmentHandoff.SupervisorIdentity != "windows:99:created-at" {
		t.Fatalf("bound journal = %#v, want helper identity", bound)
	}
	boundHandoff := *bound.ContainmentHandoff

	startedAt := time.Date(2026, 9, 16, 1, 2, 3, 0, time.UTC)
	committed, err := store.CommitSupervisorHandoff(journal.Key(), boundHandoff, startedAt)
	if err != nil {
		t.Fatalf("CommitSupervisorHandoff() error = %v", err)
	}
	if committed.ContainmentHandoff != nil || committed.ContainmentAuthority == nil || committed.PID != handoff.TargetPID || committed.ProcessIdentity != handoff.TargetIdentity || !committed.StartedAt.Equal(startedAt) {
		t.Fatalf("committed journal = %#v, want complete authority pair", committed)
	}

	replayed, err = store.CommitSupervisorHandoff(journal.Key(), boundHandoff, startedAt.Add(time.Minute))
	if err != nil {
		t.Fatalf("replayed CommitSupervisorHandoff() error = %v", err)
	}
	if !replayed.StartedAt.Equal(startedAt) || replayed.ContainmentHandoff != nil || replayed.ContainmentAuthority == nil {
		t.Fatalf("replayed commit changed durable result = %#v", replayed)
	}
}

func TestSupervisorHandoffPrepareAndBindRejectCASConflicts(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("handoff-cas", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	handoff := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
	direct := stoppedTestJournal("handoff-save-bypass", 1)
	direct.ContainmentHandoff = &handoff
	if err := store.SaveJournal(direct); err == nil {
		t.Fatal("SaveJournal() created a handoff without PrepareSupervisorHandoff()")
	}
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), handoff); err != nil {
		t.Fatal(err)
	}
	conflict := handoff
	conflict.LaunchToken = strings.Repeat("e", authority.TokenBytes*2)
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), conflict); err == nil {
		t.Fatal("PrepareSupervisorHandoff() accepted a different launch")
	}
	creatorSessionID := uint32(0)
	conflict = handoff
	conflict.CreatorSessionID = &creatorSessionID
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), conflict); err == nil {
		t.Fatal("PrepareSupervisorHandoff() accepted a different creator session identity")
	}
	if _, err := store.BindSupervisorHandoff(journal.Key(), conflict, 99, "windows:99:created-at"); err == nil {
		t.Fatal("BindSupervisorHandoff() accepted a stale expected handoff")
	}
	if _, err := store.BindSupervisorHandoff(journal.Key(), handoff, 99, "windows:99:created-at"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.BindSupervisorHandoff(journal.Key(), handoff, 100, "windows:100:created-at"); err == nil {
		t.Fatal("BindSupervisorHandoff() replaced an existing helper identity")
	}
}

func TestSupervisorHandoffBindUnknownOutcomeReplaysWithPersistenceBarrier(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("handoff-bind-unknown", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	handoff := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), handoff); err != nil {
		t.Fatal(err)
	}
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if err := writeAtomic(path, data); err != nil {
			return err
		}
		return errors.New("injected post-rename bind outcome")
	})
	if _, err := store.BindSupervisorHandoff(journal.Key(), handoff, 99, "windows:99:created-at"); err == nil {
		restore()
		t.Fatal("BindSupervisorHandoff() hid post-rename unknown outcome")
	}
	restore()
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil || loaded.ContainmentHandoff == nil || loaded.ContainmentHandoff.SupervisorPID != 99 || loaded.ContainmentHandoff.SupervisorIdentity != "windows:99:created-at" {
		t.Fatalf("post-rename bind state = %#v, %v", loaded.ContainmentHandoff, err)
	}
	writes := 0
	replayWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAtomic(path, data)
	})
	if _, err := store.BindSupervisorHandoff(journal.Key(), handoff, 99, "windows:99:created-at"); err != nil {
		replayWriter()
		t.Fatalf("replayed bind after post-rename failure = %v", err)
	}
	replayWriter()
	if writes != 1 {
		t.Fatalf("replayed bind writes = %d, want persistence barrier", writes)
	}
}

func TestSupervisorHandoffCommitUnknownOutcomeReplaysAndCASMismatchFails(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("handoff-unknown", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	handoff := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), handoff); err != nil {
		t.Fatal(err)
	}
	bound, err := store.BindSupervisorHandoff(journal.Key(), handoff, 99, "windows:99:created-at")
	if err != nil {
		t.Fatal(err)
	}
	boundHandoff := *bound.ContainmentHandoff
	other := boundHandoff
	other.LaunchToken = strings.Repeat("f", authority.TokenBytes*2)
	if _, err := store.CommitSupervisorHandoff(journal.Key(), other, time.Date(2026, 9, 16, 2, 2, 3, 0, time.UTC)); err == nil {
		t.Fatal("CommitSupervisorHandoff() accepted a mismatched pending handoff")
	}
	startedAt := time.Date(2026, 9, 16, 2, 2, 3, 0, time.UTC)
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if err := writeAtomic(path, data); err != nil {
			return err
		}
		return errors.New("rename succeeded but result is unknown")
	})
	_, err = store.CommitSupervisorHandoff(journal.Key(), boundHandoff, startedAt)
	restore()
	if err == nil {
		t.Fatal("CommitSupervisorHandoff() hid an unknown write result")
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ContainmentHandoff != nil || loaded.ContainmentAuthority == nil || loaded.PID != boundHandoff.TargetPID || loaded.ProcessIdentity != boundHandoff.TargetIdentity {
		t.Fatalf("unknown commit state = %#v, want complete committed pair", loaded)
	}
	writes := 0
	replayWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAtomic(path, data)
	})
	if _, err := store.CommitSupervisorHandoff(journal.Key(), boundHandoff, startedAt.Add(time.Minute)); err != nil {
		replayWriter()
		t.Fatalf("replayed commit after unknown result = %v", err)
	}
	replayWriter()
	if writes != 1 {
		t.Fatalf("replayed commit after unknown result writes = %d, want persistence barrier", writes)
	}

}

func TestSupervisorHandoffStopReceiptAfterCommitUnknownOutcome(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("handoff-receipt-commit-unknown", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	handoff := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), handoff); err != nil {
		t.Fatal(err)
	}
	bound, err := store.BindSupervisorHandoff(journal.Key(), handoff, 99, "windows:99:created-at")
	if err != nil {
		t.Fatal(err)
	}
	expected := *bound.ContainmentHandoff
	startedAt := time.Date(2026, 9, 16, 2, 12, 3, 0, time.UTC)
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if err := writeAtomic(path, data); err != nil {
			return err
		}
		return errors.New("injected post-rename commit outcome")
	})
	if _, err := store.CommitSupervisorHandoff(journal.Key(), expected, startedAt); err == nil {
		restore()
		t.Fatal("CommitSupervisorHandoff() hid post-rename unknown outcome")
	}
	restore()
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil || loaded.ContainmentHandoff != nil || loaded.ContainmentAuthority == nil {
		t.Fatalf("commit-unknown readback = %#v, %v", loaded, err)
	}
	receipt := testStateSupervisorHandoffStopReceipt(expected)
	writes := 0
	receiptWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAtomic(path, data)
	})
	if _, err := store.RecordSupervisorHandoffStopReceipt(journal.Key(), expected, receipt); err != nil {
		receiptWriter()
		t.Fatalf("RecordSupervisorHandoffStopReceipt() after commit unknown = %v", err)
	}
	receiptWriter()
	if writes != 1 {
		t.Fatalf("stop receipt after commit unknown writes = %d, want 1", writes)
	}
	loaded, err = store.LoadJournal(journal.Key())
	if err != nil || loaded.ContainmentAuthority == nil || loaded.ContainmentAuthority.StopReceipt == nil || *loaded.ContainmentAuthority.StopReceipt != receipt {
		t.Fatalf("stop receipt after commit unknown = %#v, %v", loaded.ContainmentAuthority, err)
	}
}

func TestSupervisorHandoffReceiptAndReleaseProofControlClear(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("handoff-receipt", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	handoff := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), handoff); err != nil {
		t.Fatal(err)
	}
	bound, err := store.BindSupervisorHandoff(journal.Key(), handoff, 99, "windows:99:created-at")
	if err != nil {
		t.Fatal(err)
	}
	expected := *bound.ContainmentHandoff
	proof := expected.ReleaseProof()
	if _, err := store.ClearSupervisorHandoff(journal.Key(), expected, proof); err == nil {
		t.Fatal("ClearSupervisorHandoff() cleared a no-receipt handoff")
	}
	stillPending, err := store.LoadJournal(journal.Key())
	if err != nil || stillPending.ContainmentHandoff == nil || !stillPending.ContainmentHandoff.Equal(expected) {
		t.Fatalf("no-receipt clear changed handoff = %#v, err = %v", stillPending.ContainmentHandoff, err)
	}
	receipt := testStateSupervisorHandoffStopReceipt(expected)
	if _, err := store.RecordSupervisorHandoffStopReceipt(journal.Key(), expected, receipt); err != nil {
		t.Fatalf("RecordSupervisorHandoffStopReceipt() error = %v", err)
	}
	withReceipt, err := store.LoadJournal(journal.Key())
	if err != nil || withReceipt.ContainmentHandoff == nil || withReceipt.ContainmentHandoff.StopReceipt == nil {
		t.Fatalf("durable handoff receipt = %#v, %v", withReceipt.ContainmentHandoff, err)
	}
	expected = *withReceipt.ContainmentHandoff
	writes := 0
	restoreWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAtomic(path, data)
	})
	if _, err := store.RecordSupervisorHandoffStopReceipt(journal.Key(), expected, receipt); err != nil {
		restoreWriter()
		t.Fatalf("replayed RecordSupervisorHandoffStopReceipt() error = %v", err)
	}
	restoreWriter()
	if writes != 1 {
		t.Fatalf("replayed RecordSupervisorHandoffStopReceipt() writes = %d, want 1", writes)
	}
	proof = expected.ReleaseProof()
	cleared, err := store.ClearSupervisorHandoff(journal.Key(), expected, proof)
	if err != nil {
		t.Fatalf("ClearSupervisorHandoff() error = %v", err)
	}
	if cleared.ContainmentHandoff != nil || cleared.HasPendingContainment() || cleared.HasProcessDetails() {
		t.Fatalf("cleared journal = %#v, want no pending containment or process marker", cleared)
	}
	if _, err := store.ClearSupervisorHandoff(journal.Key(), expected, proof); err != nil {
		t.Fatalf("replayed ClearSupervisorHandoff() error = %v", err)
	}
}

func TestSupervisorHandoffReleaseClearUnknownOutcomeReplaysWithPersistenceBarrier(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("handoff-clear-unknown", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	handoff := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), handoff); err != nil {
		t.Fatal(err)
	}
	bound, err := store.BindSupervisorHandoff(journal.Key(), handoff, 99, "windows:99:created-at")
	if err != nil {
		t.Fatal(err)
	}
	expected := *bound.ContainmentHandoff
	receipt := testStateSupervisorHandoffStopReceipt(expected)
	if _, err := store.RecordSupervisorHandoffStopReceipt(journal.Key(), expected, receipt); err != nil {
		t.Fatal(err)
	}
	withReceipt, err := store.LoadJournal(journal.Key())
	if err != nil || withReceipt.ContainmentHandoff == nil {
		t.Fatalf("handoff with receipt = %#v, %v", withReceipt.ContainmentHandoff, err)
	}
	expected = *withReceipt.ContainmentHandoff
	proof := expected.ReleaseProof()
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if err := writeAtomic(path, data); err != nil {
			return err
		}
		return errors.New("injected post-rename clear outcome")
	})
	if _, err := store.ClearSupervisorHandoff(journal.Key(), expected, proof); err == nil {
		restore()
		t.Fatal("ClearSupervisorHandoff() hid post-rename unknown outcome")
	}
	restore()
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil || loaded.ContainmentHandoff != nil || loaded.ContainmentAuthority != nil || loaded.HasProcessDetails() {
		t.Fatalf("post-rename clear state = %#v, %v", loaded, err)
	}
	writes := 0
	replayWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAtomic(path, data)
	})
	if _, err := store.ClearSupervisorHandoff(journal.Key(), expected, proof); err != nil {
		replayWriter()
		t.Fatalf("replayed clear after post-rename failure = %v", err)
	}
	replayWriter()
	if writes != 1 {
		t.Fatalf("replayed clear writes = %d, want persistence barrier", writes)
	}
}

func TestSupervisorHandoffAbortProofClearsOnlyExactPendingHandoff(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("handoff-abort", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	handoff := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
	handoff.CreatorSessionID = testStateUint32Pointer(0)
	bound, err := store.PrepareSupervisorHandoff(journal.Key(), handoff)
	if err != nil {
		t.Fatal(err)
	}
	bound, err = store.BindSupervisorHandoff(journal.Key(), *bound.ContainmentHandoff, 99, "windows:99:created-at")
	if err != nil {
		t.Fatal(err)
	}
	expected := *bound.ContainmentHandoff
	proof := testStateSupervisorHandoffAbortProofFixture(expected)
	cleared, err := store.ClearSupervisorHandoffAfterAbort(journal.Key(), expected, proof)
	if err != nil {
		t.Fatalf("ClearSupervisorHandoffAfterAbort() error = %v", err)
	}
	if cleared.ContainmentHandoff != nil || cleared.HasPendingContainment() || cleared.HasProcessDetails() {
		t.Fatalf("cleared journal = %#v, want no process or containment evidence", cleared)
	}
	if _, err := store.ClearSupervisorHandoffAfterAbort(journal.Key(), expected, proof); err == nil {
		t.Fatal("ClearSupervisorHandoffAfterAbort() accepted a missing current handoff")
	}
}

func TestSupervisorHandoffAbortProofRejectsMismatchReceiptAndConflicts(t *testing.T) {
	newPending := func(t *testing.T, runID string) (*Store, RunJournal, authority.SupervisorHandoff) {
		t.Helper()
		store := mustStore(t)
		journal := stoppedTestJournal(runID, 1)
		if err := store.SaveJournal(journal); err != nil {
			t.Fatal(err)
		}
		handoff := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
		handoff.CreatorSessionID = testStateUint32Pointer(0)
		prepared, err := store.PrepareSupervisorHandoff(journal.Key(), handoff)
		if err != nil {
			t.Fatal(err)
		}
		return store, prepared, *prepared.ContainmentHandoff
	}

	t.Run("proof mismatch", func(t *testing.T) {
		store, journal, expected := newPending(t, "handoff-abort-mismatch")
		proof := testStateSupervisorHandoffAbortProofFixture(expected)
		proof.LaunchToken = strings.Repeat("e", authority.TokenBytes*2)
		if _, err := store.ClearSupervisorHandoffAfterAbort(journal.Key(), expected, proof); err == nil {
			t.Fatal("ClearSupervisorHandoffAfterAbort() accepted a mismatched launch token")
		}
		loaded, err := store.LoadJournal(journal.Key())
		if err != nil || loaded.ContainmentHandoff == nil {
			t.Fatalf("mismatched proof changed handoff = %#v, err = %v", loaded.ContainmentHandoff, err)
		}
	})

	t.Run("durable receipt", func(t *testing.T) {
		store, journal, expected := newPending(t, "handoff-abort-receipt")
		bound, err := store.BindSupervisorHandoff(journal.Key(), expected, 99, "windows:99:created-at")
		if err != nil {
			t.Fatal(err)
		}
		expected = *bound.ContainmentHandoff
		receipt := authority.StopReceipt{
			Version: authority.SupervisorVersion, Status: "stopped", TargetPID: expected.TargetPID,
			TargetIdentity: expected.TargetIdentity, PipeToken: expected.PipeToken, JobID: expected.JobID,
			SupervisorPID: expected.SupervisorPID, SupervisorIdentity: expected.SupervisorIdentity,
		}
		if _, err := store.RecordSupervisorHandoffStopReceipt(journal.Key(), expected, receipt); err != nil {
			t.Fatal(err)
		}
		withReceipt, err := store.LoadJournal(journal.Key())
		if err != nil {
			t.Fatal(err)
		}
		proof := testStateSupervisorHandoffAbortProofFixture(*withReceipt.ContainmentHandoff)
		if _, err := store.ClearSupervisorHandoffAfterAbort(journal.Key(), *withReceipt.ContainmentHandoff, proof); err == nil {
			t.Fatal("ClearSupervisorHandoffAfterAbort() accepted a durable stop receipt")
		}
	})

	t.Run("SaveJournal process and authority conflicts", func(t *testing.T) {
		store, journal, expected := newPending(t, "handoff-abort-conflicts")
		processConflict := journal
		processConflict.PID = expected.TargetPID
		processConflict.ProcessIdentity = expected.TargetIdentity
		processConflict.StartedAt = time.Date(2026, 9, 16, 4, 2, 3, 0, time.UTC)
		if err := store.SaveJournal(processConflict); err == nil {
			t.Fatal("SaveJournal() accepted a process marker alongside pending handoff")
		}
		authorityConflict := journal
		authorityConflict.ContainmentAuthority = testSupervisorAuthority(expected.TargetPID, expected.TargetIdentity)
		if err := store.SaveJournal(authorityConflict); err == nil {
			t.Fatal("SaveJournal() accepted authority alongside pending handoff")
		}
	})
}

func TestSupervisorHandoffAbortProofUnknownWriteOutcomesFailClosed(t *testing.T) {
	t.Run("write before rename preserves pending handoff and replay succeeds", func(t *testing.T) {
		store := mustStore(t)
		journal := stoppedTestJournal("handoff-abort-prewrite", 1)
		if err := store.SaveJournal(journal); err != nil {
			t.Fatal(err)
		}
		handoffValue := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
		handoffValue.CreatorSessionID = testStateUint32Pointer(0)
		handoff, err := store.PrepareSupervisorHandoff(journal.Key(), handoffValue)
		if err != nil {
			t.Fatal(err)
		}
		expected := *handoff.ContainmentHandoff
		proof := testStateSupervisorHandoffAbortProofFixture(expected)
		restore := store.SetAtomicWriterForTesting(func(string, []byte) error { return errors.New("injected pre-write failure") })
		if _, err := store.ClearSupervisorHandoffAfterAbort(journal.Key(), expected, proof); err == nil {
			t.Fatal("ClearSupervisorHandoffAfterAbort() ignored pre-write failure")
		}
		restore()
		loaded, err := store.LoadJournal(journal.Key())
		if err != nil || loaded.ContainmentHandoff == nil {
			t.Fatalf("pre-write failure changed handoff = %#v, err = %v", loaded.ContainmentHandoff, err)
		}
		if _, err := store.ClearSupervisorHandoffAfterAbort(journal.Key(), expected, proof); err != nil {
			t.Fatalf("replayed abort clear error = %v", err)
		}
	})

	t.Run("rename success followed by error stays unknown and replay is rejected without exact current handoff", func(t *testing.T) {
		store := mustStore(t)
		journal := stoppedTestJournal("handoff-abort-postrename", 1)
		if err := store.SaveJournal(journal); err != nil {
			t.Fatal(err)
		}
		handoffValue := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
		handoffValue.CreatorSessionID = testStateUint32Pointer(0)
		handoff, err := store.PrepareSupervisorHandoff(journal.Key(), handoffValue)
		if err != nil {
			t.Fatal(err)
		}
		expected := *handoff.ContainmentHandoff
		proof := testStateSupervisorHandoffAbortProofFixture(expected)
		restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
			if err := writeAtomic(path, data); err != nil {
				return err
			}
			return errors.New("injected post-rename unknown outcome")
		})
		if _, err := store.ClearSupervisorHandoffAfterAbort(journal.Key(), expected, proof); err == nil {
			t.Fatal("ClearSupervisorHandoffAfterAbort() hid post-rename unknown outcome")
		}
		restore()
		loaded, err := store.LoadJournal(journal.Key())
		if err != nil || loaded.ContainmentHandoff != nil || loaded.HasProcessDetails() || loaded.ContainmentAuthority != nil {
			t.Fatalf("post-rename durable state = %#v, err = %v", loaded, err)
		}
		if _, err := store.ClearSupervisorHandoffAfterAbort(journal.Key(), expected, proof); err == nil {
			t.Fatal("replay cleared without an exact current handoff")
		}
	})
}

func TestSupervisorHandoffAbortProofPreservesLegacyJournalCompatibility(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("handoff-abort-legacy", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil || loaded.ContainmentHandoff != nil {
		t.Fatalf("legacy journal load = %#v, err = %v", loaded, err)
	}
	proof := testStateSupervisorHandoffAbortProofFixture(testStateSupervisorHandoffFixture(42, "windows:42:created-at"))
	if _, err := store.ClearSupervisorHandoffAfterAbort(journal.Key(), testStateSupervisorHandoffFixture(42, "windows:42:created-at"), proof); err == nil {
		t.Fatal("ClearSupervisorHandoffAfterAbort() accepted a legacy journal without handoff")
	}
}

func TestSupervisorHandoffCreatorSessionZeroRoundTripsAndMissingStaysLegacy(t *testing.T) {
	store := mustStore(t)
	legacy := stoppedTestJournal("handoff-session-legacy", 1)
	if err := store.SaveJournal(legacy); err != nil {
		t.Fatal(err)
	}
	legacyLoaded, err := store.LoadJournal(legacy.Key())
	if err != nil || legacyLoaded.ContainmentHandoff != nil {
		t.Fatalf("legacy journal = %#v, err = %v", legacyLoaded, err)
	}

	journal := stoppedTestJournal("handoff-session-zero", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	handoff := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
	handoff.CreatorSessionID = testStateUint32Pointer(0)
	saved, err := store.PrepareSupervisorHandoff(journal.Key(), handoff)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil || loaded.ContainmentHandoff == nil || loaded.ContainmentHandoff.CreatorSessionID == nil || *loaded.ContainmentHandoff.CreatorSessionID != 0 {
		t.Fatalf("session 0 handoff = %#v, saved=%#v, err=%v", loaded.ContainmentHandoff, saved.ContainmentHandoff, err)
	}
}

func TestSupervisorHandoffWriteFailureRetainsPrepareAndSaveJournalCannotBypass(t *testing.T) {
	t.Run("pre-write failure leaves prepare absent", func(t *testing.T) {
		store := mustStore(t)
		journal := stoppedTestJournal("handoff-write-failure-pre", 1)
		if err := store.SaveJournal(journal); err != nil {
			t.Fatal(err)
		}
		handoff := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
		restore := store.SetAtomicWriterForTesting(func(string, []byte) error { return errors.New("injected pre-write failure") })
		if _, err := store.PrepareSupervisorHandoff(journal.Key(), handoff); err == nil {
			restore()
			t.Fatal("PrepareSupervisorHandoff() hid pre-write failure")
		}
		restore()
		loaded, err := store.LoadJournal(journal.Key())
		if err != nil || loaded.ContainmentHandoff != nil {
			t.Fatalf("pre-write prepare changed journal = %#v, %v", loaded, err)
		}
		if _, err := store.PrepareSupervisorHandoff(journal.Key(), handoff); err != nil {
			t.Fatalf("replayed prepare after pre-write failure = %v", err)
		}
	})

	t.Run("post-rename failure requires exact prepare replay barrier", func(t *testing.T) {
		store := mustStore(t)
		journal := stoppedTestJournal("handoff-write-failure-post", 1)
		if err := store.SaveJournal(journal); err != nil {
			t.Fatal(err)
		}
		handoff := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
		restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
			if err := writeAtomic(path, data); err != nil {
				return err
			}
			return errors.New("injected post-rename unknown outcome")
		})
		if _, err := store.PrepareSupervisorHandoff(journal.Key(), handoff); err == nil {
			restore()
			t.Fatal("PrepareSupervisorHandoff() hid post-rename unknown outcome")
		}
		restore()
		loaded, err := store.LoadJournal(journal.Key())
		if err != nil || loaded.ContainmentHandoff == nil || !loaded.ContainmentHandoff.Equal(handoff) {
			t.Fatalf("post-rename prepare state = %#v, %v", loaded, err)
		}
		writes := 0
		replayWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
			writes++
			return writeAtomic(path, data)
		})
		if _, err := store.PrepareSupervisorHandoff(journal.Key(), handoff); err != nil {
			replayWriter()
			t.Fatalf("replayed prepare after post-rename failure = %v", err)
		}
		replayWriter()
		if writes != 1 {
			t.Fatalf("replayed prepare writes = %d, want persistence barrier", writes)
		}

		copy := loaded
		copy.ContainmentHandoff = nil
		if err := store.SaveJournal(copy); err == nil {
			t.Fatal("SaveJournal() bypassed dedicated handoff clear")
		}
	})
}

func TestPendingHandoffBlocksCleanupWithoutChangingProcessMarkerMeaning(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("handoff-cleanup", 1)
	journal.LocalState = "terminal_pending"
	journal.TerminalState = "completed"
	journal.TerminalPendingAt = time.Date(2026, 9, 16, 3, 2, 3, 0, time.UTC)
	journal.TerminalVerdict = TerminalVerdictAccepted
	journal.TerminalResolvedAt = journal.TerminalPendingAt
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	handoff := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), handoff); err != nil {
		t.Fatal(err)
	}
	pending, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if pending.HasProcessDetails() {
		t.Fatal("pending handoff changed HasProcessDetails() semantics")
	}
	if !pending.HasPendingContainment() {
		t.Fatal("pending handoff was not reported by HasPendingContainment()")
	}
	if _, err := store.EnterCleanupPending(journal.Key()); err == nil {
		t.Fatal("EnterCleanupPending() ignored pending handoff")
	}
	if err := store.DeleteJournal(journal.Key()); err == nil {
		t.Fatal("DeleteJournal() erased pending handoff")
	}
}

func TestSupervisorHandoffLegacyAndMalformedJournalHandling(t *testing.T) {
	store := mustStore(t)
	legacy := stoppedTestJournal("handoff-legacy", 1)
	if err := store.SaveJournal(legacy); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadJournal(legacy.Key())
	if err != nil || loaded.ContainmentHandoff != nil {
		t.Fatalf("legacy journal load = %#v, %v", loaded, err)
	}

	partial := stoppedTestJournal("handoff-partial", 1)
	partial.ContainmentHandoff = func() *authority.SupervisorHandoff {
		value := testStateSupervisorHandoffFixture(partial.PID, partial.ProcessIdentity)
		value.SupervisorPID = 99
		return &value
	}()
	if err := store.SaveJournal(partial); err == nil {
		t.Fatal("SaveJournal() accepted partial handoff")
	}

	valid := stoppedTestJournal("handoff-conflict", 1)
	value := testStateSupervisorHandoffFixture(valid.PID, valid.ProcessIdentity)
	valid.ContainmentHandoff = &value
	valid.ContainmentAuthority = testSupervisorAuthority(valid.PID, valid.ProcessIdentity)
	if err := store.SaveJournal(valid); err == nil {
		t.Fatal("SaveJournal() accepted handoff and authority together")
	}

	partialFile := stoppedTestJournal("handoff-partial-file", 1)
	partialValue := testStateSupervisorHandoffFixture(42, "windows:42:created-at")
	partialValue.SupervisorPID = 99
	partialFile.ContainmentHandoff = &partialValue
	writeJournalForTest(t, store, partialFile)
	if _, err := store.LoadJournal(partialFile.Key()); err == nil {
		t.Fatal("LoadJournal() accepted a partial persisted handoff")
	}

	conflictingFile := testJournal("handoff-conflict-file", 1)
	conflictingValue := testStateSupervisorHandoffFixture(conflictingFile.PID, conflictingFile.ProcessIdentity)
	conflictingFile.ContainmentHandoff = &conflictingValue
	conflictingFile.ContainmentAuthority = testSupervisorAuthority(conflictingFile.PID, conflictingFile.ProcessIdentity)
	writeJournalForTest(t, store, conflictingFile)
	if _, err := store.LoadJournal(conflictingFile.Key()); err == nil {
		t.Fatal("LoadJournal() accepted a handoff and authority together")
	}

	uncertainFile := stoppedTestJournal("containment-uncertainty-without-marker", 1)
	uncertainFile.PID = 0
	uncertainFile.ProcessIdentity = ""
	uncertainFile.StartedAt = time.Time{}
	uncertainFile.ContainmentUnproven = true
	writeJournalForTest(t, store, uncertainFile)
	if _, err := store.LoadJournal(uncertainFile.Key()); err == nil {
		t.Fatal("LoadJournal() accepted containment uncertainty without a process marker")
	}

	unknown := stoppedTestJournal("handoff-unknown-field", 1)
	path := store.journalPath(unknown.Key())
	data, err := json.Marshal(unknown)
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	object["future_handoff_field"] = json.RawMessage(`true`)
	data, err = json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadJournal(unknown.Key()); err == nil {
		t.Fatal("LoadJournal() accepted an unknown handoff field")
	}
}

func TestLinuxSupervisorScanFencesReplayBeforeReceiptAndClear(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("linux-scan-fences", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	handoff := testStateLinuxSupervisorHandoffFixture(42, "linux:v2:01234567-89ab-cdef-0123-456789abcdef:77:42:456")
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), handoff); err != nil {
		t.Fatal(err)
	}
	bound, err := store.BindSupervisorHandoff(journal.Key(), testStateLinuxBindRequest(handoff), 99, "linux:v2:01234567-89ab-cdef-0123-456789abcdef:77:99:789")
	if err != nil {
		t.Fatal(err)
	}
	expected := *bound.ContainmentHandoff
	if bound.ContainmentScans == nil || bound.ContainmentScans.Helper.PendingSequence != 1 || bound.ContainmentScans.Mirror.PendingSequence != 1 {
		t.Fatalf("bound scan fences = %#v, want both sequence-one scans pending", bound.ContainmentScans)
	}
	fabricatedMirror := testStateLinuxBindRequest(handoff)
	fabricatedMirror.ScanUpdate.MirrorIdentity = strings.Repeat("f", authority.TokenBytes*2)
	if _, err := store.BindSupervisorHandoff(journal.Key(), fabricatedMirror, 99, "linux:v2:01234567-89ab-cdef-0123-456789abcdef:77:99:789"); err == nil {
		t.Fatal("Linux supervisor bind accepted a fabricated mirror identity")
	}

	helperBegin := testStateScanUpdate(expected, authority.SupervisorScanOwnerHelper, authority.SupervisorScanPhaseBegin, 1, "")
	fabricatedScan := testStateScanUpdate(expected, authority.SupervisorScanOwnerHelper, authority.SupervisorScanPhaseBegin, 1, "")
	fabricatedScan.ScanUpdate.MirrorIdentity = strings.Repeat("f", authority.TokenBytes*2)
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), fabricatedScan); err == nil {
		t.Fatal("Linux supervisor scan update accepted a mismatched mirror identity")
	}
	writes := 0
	unknownWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		if err := writeAtomic(path, data); err != nil {
			return err
		}
		return errors.New("scan begin result is unknown after rename")
	})
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), helperBegin); err == nil {
		unknownWriter()
		t.Fatal("scan begin hid its unknown write result")
	}
	unknownWriter()
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil || loaded.ContainmentScans == nil || loaded.ContainmentScans.Helper.PendingSequence != 1 {
		t.Fatalf("unknown scan begin state = %#v, %v", loaded.ContainmentScans, err)
	}
	writes = 0
	replayWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAtomic(path, data)
	})
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), helperBegin); err != nil {
		replayWriter()
		t.Fatalf("exact scan begin replay = %v", err)
	}
	replayWriter()
	if writes != 1 {
		t.Fatalf("exact scan begin replay writes = %d, want a fresh persistence barrier", writes)
	}
	if _, err := store.RecordSupervisorHandoffStopReceipt(journal.Key(), expected, testStateLinuxSupervisorStopReceipt(expected)); err == nil {
		t.Fatal("stop receipt accepted while helper and mirror scans were pending")
	}
	wrongSequence := testStateScanUpdate(expected, authority.SupervisorScanOwnerHelper, authority.SupervisorScanPhaseBegin, 2, "")
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), wrongSequence); err == nil {
		t.Fatal("out-of-order scan sequence replaced a pending fence")
	}

	helperClean := testStateScanUpdate(expected, authority.SupervisorScanOwnerHelper, authority.SupervisorScanPhaseClean, 1, "")
	unknownWriter = store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if err := writeAtomic(path, data); err != nil {
			return err
		}
		return errors.New("scan completion result is unknown after rename")
	})
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), helperClean); err == nil {
		unknownWriter()
		t.Fatal("scan completion hid its unknown write result")
	}
	unknownWriter()
	writes = 0
	replayWriter = store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAtomic(path, data)
	})
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), helperClean); err != nil {
		replayWriter()
		t.Fatalf("exact scan completion replay = %v", err)
	}
	replayWriter()
	if writes != 1 {
		t.Fatalf("exact scan completion replay writes = %d, want a fresh persistence barrier", writes)
	}
	loaded, err = store.LoadJournal(journal.Key())
	if err != nil || loaded.ContainmentScans == nil || loaded.ContainmentScans.Helper.PendingSequence != 1 || loaded.ContainmentScans.Helper.CleanResultSequence != 1 {
		t.Fatalf("clean result retired its pending fence before helper acknowledgement: %#v, %v", loaded.ContainmentScans, err)
	}
	if _, err := store.CommitSupervisorHandoff(journal.Key(), expected, time.Date(2026, 9, 26, 1, 2, 3, 0, time.UTC)); err == nil {
		t.Fatal("handoff committed while the mirror initial scan remained pending")
	}
	mirrorBegin := testStateScanUpdate(expected, authority.SupervisorScanOwnerMirror, authority.SupervisorScanPhaseBegin, 1, "")
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), mirrorBegin); err != nil {
		t.Fatalf("acknowledge mirror pre-scan fence = %v", err)
	}
	mirrorClean := testStateScanUpdate(expected, authority.SupervisorScanOwnerMirror, authority.SupervisorScanPhaseClean, 1, "")
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), mirrorClean); err != nil {
		t.Fatalf("complete mirror initial scan = %v", err)
	}
	helperRetire := testStateScanUpdate(expected, authority.SupervisorScanOwnerHelper, authority.SupervisorScanPhaseRetire, 1, "")
	retireWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if err := writeAtomic(path, data); err != nil {
			return err
		}
		return errors.New("scan retirement result is unknown after rename")
	})
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), helperRetire); err == nil {
		retireWriter()
		t.Fatal("scan retirement hid its unknown write result")
	}
	retireWriter()
	loaded, err = store.LoadJournal(journal.Key())
	if err != nil || loaded.ContainmentScans == nil || loaded.ContainmentScans.Helper.PendingSequence != 0 || loaded.ContainmentScans.Helper.CompletedSequence != 1 {
		t.Fatalf("unknown scan retirement state = %#v, %v", loaded.ContainmentScans, err)
	}
	writes = 0
	replayWriter = store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAtomic(path, data)
	})
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), helperRetire); err != nil {
		replayWriter()
		t.Fatalf("exact scan retirement replay = %v", err)
	}
	replayWriter()
	if writes != 1 {
		t.Fatalf("exact scan retirement replay writes = %d, want a fresh persistence barrier", writes)
	}
	mirrorRetire := testStateScanUpdate(expected, authority.SupervisorScanOwnerMirror, authority.SupervisorScanPhaseRetire, 1, "")
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), mirrorRetire); err != nil {
		t.Fatalf("retire acknowledged mirror scan fence = %v", err)
	}
	committed, err := store.CommitSupervisorHandoff(journal.Key(), expected, time.Date(2026, 9, 26, 1, 2, 3, 0, time.UTC))
	if err != nil {
		t.Fatalf("commit clean scan handoff = %v", err)
	}
	if committed.ContainmentScans == nil || !committed.ContainmentScans.clean() {
		t.Fatalf("committed scan journal = %#v, want clean helper and mirror proof", committed.ContainmentScans)
	}
	reloadedCommit, err := store.LoadJournal(journal.Key())
	if err != nil || reloadedCommit.ContainmentScans == nil || reloadedCommit.ContainmentScans.Helper.SealedSequence != 0 ||
		reloadedCommit.ContainmentScans.Mirror.SealedSequence != 0 || !reloadedCommit.ContainmentObservationUnresolved() {
		t.Fatalf("clean legacy-compatible scan round trip = %#v unresolved:%v, %v", reloadedCommit.ContainmentScans, reloadedCommit.ContainmentObservationUnresolved(), err)
	}
	receipt := testStateLinuxSupervisorStopReceipt(expected)
	if _, err := store.RecordSupervisorHandoffStopReceipt(journal.Key(), expected, receipt); err == nil {
		t.Fatal("stop receipt accepted clean retired scans before either scanner was sealed")
	}
	helperSeal := testStateScanUpdate(expected, authority.SupervisorScanOwnerHelper, authority.SupervisorScanPhaseSeal, 1, "")
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), helperSeal); err != nil {
		t.Fatalf("seal final helper scan = %v", err)
	}
	if _, err := store.RecordSupervisorHandoffStopReceipt(journal.Key(), expected, receipt); err == nil {
		t.Fatal("stop receipt accepted after only the helper scanner was sealed")
	}
	mirrorSeal := testStateScanUpdate(expected, authority.SupervisorScanOwnerMirror, authority.SupervisorScanPhaseSeal, 1, "")
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), mirrorSeal); err != nil {
		t.Fatalf("seal final mirror scan = %v", err)
	}
	if _, err := store.RecordSupervisorHandoffStopReceipt(journal.Key(), expected, receipt); err != nil {
		t.Fatalf("record stop receipt after both exact seals = %v", err)
	}
	cleared, err := store.ClearProcessDetails(journal.Key(), expected.TargetPID, expected.TargetIdentity)
	if err != nil {
		t.Fatalf("clear exact process after durable clean receipt = %v", err)
	}
	if cleared.ContainmentScans != nil || cleared.HasProcessDetails() || cleared.ContainmentAuthority != nil {
		t.Fatalf("cleared Linux containment state = %#v", cleared)
	}
}

func TestLinuxSupervisorScanFailureStaysStickyAcrossAbortAndReceipt(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("linux-scan-failure", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	handoff := testStateLinuxSupervisorHandoffFixture(42, "linux:v2:01234567-89ab-cdef-0123-456789abcdef:77:42:456")
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), handoff); err != nil {
		t.Fatal(err)
	}
	bound, err := store.BindSupervisorHandoff(journal.Key(), testStateLinuxBindRequest(handoff), 99, "linux:v2:01234567-89ab-cdef-0123-456789abcdef:77:99:789")
	if err != nil {
		t.Fatal(err)
	}
	expected := *bound.ContainmentHandoff
	begin := testStateScanUpdate(expected, authority.SupervisorScanOwnerHelper, authority.SupervisorScanPhaseBegin, 1, "")
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), begin); err != nil {
		t.Fatal(err)
	}
	failure := testStateScanUpdate(expected, authority.SupervisorScanOwnerHelper, authority.SupervisorScanPhaseUnproven, 1, "observed_escape")
	unknownWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if err := writeAtomic(path, data); err != nil {
			return err
		}
		return errors.New("scan failure result is unknown after rename")
	})
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), failure); err == nil {
		unknownWriter()
		t.Fatal("helper scan failure hid its unknown write result")
	}
	unknownWriter()
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.ContainmentUnproven || loaded.ContainmentScans == nil || loaded.ContainmentScans.Helper.PendingSequence != 0 || loaded.ContainmentScans.Helper.UnprovenSequence != 1 || loaded.ContainmentScans.Helper.FailureKind != "observed_escape" {
		t.Fatalf("helper failure was not durably latched: %#v", loaded)
	}
	writes := 0
	replayWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAtomic(path, data)
	})
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), failure); err != nil {
		replayWriter()
		t.Fatalf("exact helper failure replay = %v", err)
	}
	replayWriter()
	if writes != 1 {
		t.Fatalf("exact helper failure replay writes = %d, want a fresh persistence barrier", writes)
	}
	if _, err := store.RecordSupervisorHandoffStopReceipt(journal.Key(), expected, testStateLinuxSupervisorStopReceipt(expected)); err == nil {
		t.Fatal("receipt accepted after helper-only scan failure")
	}
	proof := testStateSupervisorHandoffAbortProofFixture(expected)
	if _, err := store.ClearSupervisorHandoffAfterAbort(journal.Key(), expected, proof); err == nil {
		t.Fatal("abort proof cleared an observed helper scan failure")
	}
	if err := store.DeleteJournal(journal.Key()); err == nil {
		t.Fatal("journal deletion cleared a sticky helper scan failure")
	}
	if !loaded.ContainmentObservationUnresolved() {
		t.Fatal("helper scan failure was not classified as unresolved containment")
	}
}

func TestLinuxFailedPreScanBeginCannotBecomeStopReceipt(t *testing.T) {
	store, key, handoff := committedLinuxScanHandoffForTest(t, "linux-failed-next-begin")
	for _, owner := range []string{authority.SupervisorScanOwnerHelper, authority.SupervisorScanOwnerMirror} {
		for _, update := range []authority.SupervisorHandoff{
			testStateScanUpdate(handoff, owner, authority.SupervisorScanPhaseBegin, 2, ""),
			testStateScanUpdate(handoff, owner, authority.SupervisorScanPhaseClean, 2, ""),
			testStateScanUpdate(handoff, owner, authority.SupervisorScanPhaseRetire, 2, ""),
		} {
			if _, err := store.PrepareSupervisorHandoff(key, update); err != nil {
				t.Fatalf("retire periodic %s scan: %v", owner, err)
			}
		}
	}

	failedWrite := store.SetAtomicWriterForTesting(func(string, []byte) error {
		return errors.New("pre-rename scan write failure")
	})
	begin := testStateScanUpdate(handoff, authority.SupervisorScanOwnerHelper, authority.SupervisorScanPhaseBegin, 3, "")
	if _, err := store.PrepareSupervisorHandoff(key, begin); err == nil {
		failedWrite()
		t.Fatal("failed BEGIN unexpectedly persisted")
	}
	failure := testStateScanUpdate(handoff, authority.SupervisorScanOwnerHelper, authority.SupervisorScanPhaseUnproven, 3, "pre_scan_ack_failure")
	if _, err := store.PrepareSupervisorHandoff(key, failure); err == nil {
		failedWrite()
		t.Fatal("unproven update unexpectedly persisted while the journal writer was unavailable")
	}
	failedWrite()

	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if scans := loaded.ContainmentScans; scans == nil || scans.Helper.CompletedSequence != 2 ||
		scans.Helper.PendingSequence != 0 || scans.Helper.UnprovenSequence != 0 || scans.Helper.SealedSequence != 0 {
		t.Fatalf("failed BEGIN state = %#v; want prior clean sequence unsealed and no durable failed update", scans)
	}
	if !loaded.ContainmentObservationUnresolved() {
		t.Fatal("clean-but-unsealed scan state was not classified as unresolved")
	}
	if _, err := store.RecordSupervisorHandoffStopReceipt(key, handoff, testStateLinuxSupervisorStopReceipt(handoff)); err == nil {
		t.Fatal("helper hard-death state after failed BEGIN authorized a stop receipt")
	}

	if _, err := store.PrepareSupervisorHandoff(key, failure); err != nil {
		t.Fatalf("record exact pre-scan acknowledgement failure after storage recovers: %v", err)
	}
	loaded, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.ContainmentUnproven || loaded.ContainmentScans == nil ||
		loaded.ContainmentScans.Helper.UnprovenSequence != 3 || loaded.ContainmentScans.Helper.FailureKind != "pre_scan_ack_failure" {
		t.Fatalf("pre-scan failure was not made sticky at CompletedSequence+1: %#v", loaded)
	}
	if _, err := store.RecordSupervisorHandoffStopReceipt(key, handoff, testStateLinuxSupervisorStopReceipt(handoff)); err == nil {
		t.Fatal("stop receipt accepted after the exact pre-scan failure was latched")
	}
}

func TestLinuxScanSealReplayIsExactAndNeverReopens(t *testing.T) {
	store, key, handoff := committedLinuxScanHandoffForTest(t, "linux-seal-replay")
	for _, update := range []authority.SupervisorHandoff{
		testStateScanUpdate(handoff, authority.SupervisorScanOwnerHelper, authority.SupervisorScanPhaseBegin, 2, ""),
		testStateScanUpdate(handoff, authority.SupervisorScanOwnerHelper, authority.SupervisorScanPhaseClean, 2, ""),
		testStateScanUpdate(handoff, authority.SupervisorScanOwnerHelper, authority.SupervisorScanPhaseRetire, 2, ""),
	} {
		if _, err := store.PrepareSupervisorHandoff(key, update); err != nil {
			t.Fatalf("prepare helper final scan: %v", err)
		}
	}
	helperSeal := testStateScanUpdate(handoff, authority.SupervisorScanOwnerHelper, authority.SupervisorScanPhaseSeal, 2, "")
	unknownWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if err := writeAtomic(path, data); err != nil {
			return err
		}
		return errors.New("seal result is unknown after rename")
	})
	if _, err := store.PrepareSupervisorHandoff(key, helperSeal); err == nil {
		unknownWriter()
		t.Fatal("seal hid its unknown write result")
	}
	unknownWriter()
	loaded, err := store.LoadJournal(key)
	if err != nil || loaded.ContainmentScans == nil || loaded.ContainmentScans.Helper.SealedSequence != 2 {
		t.Fatalf("unknown seal state = %#v, %v", loaded.ContainmentScans, err)
	}
	writes := 0
	replayWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAtomic(path, data)
	})
	if _, err := store.PrepareSupervisorHandoff(key, helperSeal); err != nil {
		replayWriter()
		t.Fatalf("exact seal replay did not establish a fresh persistence barrier: %v", err)
	}
	replayWriter()
	if writes != 1 {
		t.Fatalf("exact seal replay writes = %d, want one fresh durable write", writes)
	}

	if _, err := store.PrepareSupervisorHandoff(key, testStateScanUpdate(handoff, authority.SupervisorScanOwnerHelper, authority.SupervisorScanPhaseBegin, 3, "")); err == nil {
		t.Fatal("sealed helper scanner began a later sequence")
	}
	if _, err := store.PrepareSupervisorHandoff(key, testStateScanUpdate(handoff, authority.SupervisorScanOwnerHelper, authority.SupervisorScanPhaseSeal, 1, "")); err == nil {
		t.Fatal("sealed helper scanner changed its exact seal sequence")
	}
	if _, err := store.PrepareSupervisorHandoff(key, testStateScanUpdate(handoff, authority.SupervisorScanOwnerMirror, authority.SupervisorScanPhaseSeal, 1, "")); err != nil {
		t.Fatalf("seal final mirror sequence: %v", err)
	}
	assertSealed := testStateScanUpdate(handoff, authority.SupervisorScanOwnerHelper, authority.SupervisorScanPhaseAssertAllSealed, 2, "")
	if _, err := store.PrepareSupervisorHandoff(key, assertSealed); err != nil {
		t.Fatalf("assert exact helper and mirror seals: %v", err)
	}
	if _, err := store.RecordSupervisorHandoffStopReceipt(key, handoff, testStateLinuxSupervisorStopReceipt(handoff)); err != nil {
		t.Fatalf("record receipt after exact helper+mirror seals: %v", err)
	}
}

func TestLinuxRecoverySealRequiresCleanHelperProofAndPriorMirrorSeal(t *testing.T) {
	store, key, handoff := committedLinuxScanHandoffForTest(t, "linux-recovery-seal")
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.RecoveryHelperScanSealEligible() {
		t.Fatal("recovery seal was eligible before the daemon mirror had sealed")
	}
	value := journal.ContainmentAuthority.Clone()
	if _, err := store.RecordSupervisorRecoveryScanSeal(key, value, 2); err == nil {
		t.Fatal("helper recovery proof sealed an unsealed mirror")
	}
	mirrorSeal := testStateScanUpdate(handoff, authority.SupervisorScanOwnerMirror, authority.SupervisorScanPhaseSeal, 1, "")
	if _, err := store.PrepareSupervisorHandoff(key, mirrorSeal); err != nil {
		t.Fatalf("persist durable mirror seal: %v", err)
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.RecoveryHelperScanSealEligible() {
		t.Fatal("clean unsealed helper with exact sealed mirror was not eligible for authenticated terminal proof")
	}
	unknownWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		if err := writeAtomic(path, data); err != nil {
			return err
		}
		return errors.New("helper terminal seal result is unknown after rename")
	})
	if _, err := store.RecordSupervisorRecoveryScanSeal(key, value, 2); err == nil {
		unknownWriter()
		t.Fatal("helper terminal seal hid its unknown write result")
	}
	unknownWriter()
	loaded, err := store.LoadJournal(key)
	if err != nil || loaded.ContainmentScans == nil || loaded.ContainmentScans.Helper.CompletedSequence != 2 ||
		loaded.ContainmentScans.Helper.SealedSequence != 2 || loaded.ContainmentObservationUnresolved() {
		t.Fatalf("recovered helper seal state = %#v unresolved:%v, %v", loaded.ContainmentScans, loaded.ContainmentObservationUnresolved(), err)
	}
	writes := 0
	replayWriter := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return writeAtomic(path, data)
	})
	if _, err := store.RecordSupervisorRecoveryScanSeal(key, value, 2); err != nil {
		replayWriter()
		t.Fatalf("exact recovered helper seal replay: %v", err)
	}
	replayWriter()
	if writes != 1 {
		t.Fatalf("exact recovered helper seal replay writes = %d, want one fresh barrier", writes)
	}
	if _, err := store.RecordContainmentStopReceipt(key, value.TargetPID, value.TargetIdentity, testStateLinuxSupervisorStopReceipt(handoff)); err != nil {
		t.Fatalf("record receipt after authenticated helper seal and prior mirror seal: %v", err)
	}
}

func TestLinuxInitialBoundAbortClearsOnlyWithoutReceipt(t *testing.T) {
	store := mustStore(t)
	journal := stoppedTestJournal("linux-precommit-abort", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	launch := testStateLinuxSupervisorHandoffFixture(42, "linux:v2:01234567-89ab-cdef-0123-456789abcdef:77:42:456")
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), launch); err != nil {
		t.Fatal(err)
	}
	bound, err := store.BindSupervisorHandoff(journal.Key(), testStateLinuxBindRequest(launch), 99, "linux:v2:01234567-89ab-cdef-0123-456789abcdef:77:99:789")
	if err != nil {
		t.Fatal(err)
	}
	expected := *bound.ContainmentHandoff
	for _, update := range []authority.SupervisorHandoff{
		testStateScanUpdate(expected, authority.SupervisorScanOwnerMirror, authority.SupervisorScanPhaseBegin, 1, ""),
		testStateScanUpdate(expected, authority.SupervisorScanOwnerMirror, authority.SupervisorScanPhaseClean, 1, ""),
		testStateScanUpdate(expected, authority.SupervisorScanOwnerMirror, authority.SupervisorScanPhaseRetire, 1, ""),
	} {
		if _, err := store.PrepareSupervisorHandoff(journal.Key(), update); err != nil {
			t.Fatalf("persist mirror's initial clean scan: %v", err)
		}
	}
	proof := testStateSupervisorHandoffAbortProofFixture(expected)
	cleared, err := store.ClearSupervisorHandoffAfterAbort(journal.Key(), expected, proof)
	if err != nil {
		t.Fatalf("clear exact never-committed initial handoff after abort proof: %v", err)
	}
	if cleared.ContainmentHandoff != nil || cleared.ContainmentScans != nil || cleared.ContainmentAuthority != nil || cleared.HasProcessDetails() {
		t.Fatalf("pre-commit abort left containment authority: %#v", cleared)
	}
}

func TestBoundLegacyLinuxAuthorityLoadsButCannotProveCleanScans(t *testing.T) {
	store, key := claimedStoreForSupervisorScanTest(t)
	startedAt := time.Date(2026, 9, 26, 2, 2, 3, 0, time.UTC)
	handoff := testStateLinuxSupervisorHandoffFixture(42, "linux:v2:01234567-89ab-cdef-0123-456789abcdef:77:42:456")
	value, err := handoffWithHelper(handoff, 99, "linux:v2:01234567-89ab-cdef-0123-456789abcdef:77:99:789").ToSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetProcessDetailsWithAuthority(key, value.TargetPID, value.TargetIdentity, startedAt, value); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatalf("read additive legacy Linux journal: %v", err)
	}
	if loaded.ContainmentScans != nil || !loaded.ContainmentObservationUnresolved() {
		t.Fatalf("legacy Linux journal scan state = %#v unresolved:%v, want missing-but-unresolved", loaded.ContainmentScans, loaded.ContainmentObservationUnresolved())
	}
	if _, err := store.RecordContainmentStopReceipt(key, value.TargetPID, value.TargetIdentity, testStateLinuxSupervisorStopReceipt(handoffWithHelper(handoff, 99, "linux:v2:01234567-89ab-cdef-0123-456789abcdef:77:99:789"))); err == nil {
		t.Fatal("legacy Linux journal minted a receipt without scan fences")
	}
}

func committedLinuxScanHandoffForTest(t *testing.T, runID string) (*Store, RunKey, authority.SupervisorHandoff) {
	t.Helper()
	store := mustStore(t)
	journal := stoppedTestJournal(runID, 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	launch := testStateLinuxSupervisorHandoffFixture(42, "linux:v2:01234567-89ab-cdef-0123-456789abcdef:77:42:456")
	if _, err := store.PrepareSupervisorHandoff(journal.Key(), launch); err != nil {
		t.Fatal(err)
	}
	bound, err := store.BindSupervisorHandoff(journal.Key(), testStateLinuxBindRequest(launch), 99, "linux:v2:01234567-89ab-cdef-0123-456789abcdef:77:99:789")
	if err != nil {
		t.Fatal(err)
	}
	handoff := *bound.ContainmentHandoff
	for _, owner := range []string{authority.SupervisorScanOwnerHelper, authority.SupervisorScanOwnerMirror} {
		for _, phase := range []string{authority.SupervisorScanPhaseBegin, authority.SupervisorScanPhaseClean, authority.SupervisorScanPhaseRetire} {
			update := testStateScanUpdate(handoff, owner, phase, 1, "")
			if _, err := store.PrepareSupervisorHandoff(journal.Key(), update); err != nil {
				t.Fatalf("complete %s initial scan phase %s: %v", owner, phase, err)
			}
		}
	}
	if _, err := store.CommitSupervisorHandoff(journal.Key(), handoff, time.Date(2026, 9, 26, 5, 6, 7, 0, time.UTC)); err != nil {
		t.Fatalf("commit exact clean initial scans: %v", err)
	}
	return store, journal.Key(), handoff
}

func claimedStoreForSupervisorScanTest(t *testing.T) (*Store, RunKey) {
	t.Helper()
	store := mustStore(t)
	journal := stoppedTestJournal("linux-scan-legacy", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	return store, journal.Key()
}

func handoffWithHelper(handoff authority.SupervisorHandoff, pid int, identity string) authority.SupervisorHandoff {
	handoff.SupervisorPID = pid
	handoff.SupervisorIdentity = identity
	return handoff
}

func testStateLinuxSupervisorHandoffFixture(pid int, identity string) authority.SupervisorHandoff {
	handoff := testStateSupervisorHandoffFixture(pid, identity)
	handoff.OwnerKind = authority.OwnerKindLinuxHelper
	handoff.OwnerContext = "linux-helper:test|endpoint=/tmp/supervisor.sock"
	return handoff
}

func testStateLinuxBindRequest(handoff authority.SupervisorHandoff) authority.SupervisorHandoff {
	handoff.ScanUpdate = &authority.SupervisorScanUpdate{
		MirrorIdentity: strings.Repeat("e", authority.TokenBytes*2),
		Owner:          authority.SupervisorScanOwnerMirror, Sequence: 1, Phase: authority.SupervisorScanPhaseInitialize,
	}
	return handoff
}

func testStateScanUpdate(handoff authority.SupervisorHandoff, owner, phase string, sequence uint64, failureKind string) authority.SupervisorHandoff {
	helperPID := handoff.SupervisorPID
	helperIdentity := handoff.SupervisorIdentity
	handoff.SupervisorPID = 0
	handoff.SupervisorIdentity = ""
	handoff.ScanUpdate = &authority.SupervisorScanUpdate{
		SupervisorPID: helperPID, SupervisorIdentity: helperIdentity,
		MirrorIdentity: strings.Repeat("e", authority.TokenBytes*2),
		Owner:          owner, Phase: phase, Sequence: sequence, FailureKind: failureKind,
	}
	return handoff
}

func testStateLinuxSupervisorStopReceipt(handoff authority.SupervisorHandoff) authority.StopReceipt {
	receipt := testStateSupervisorHandoffStopReceipt(handoff)
	receipt.OwnerKind = handoff.OwnerKind
	receipt.OwnerContext = handoff.OwnerContext
	return receipt
}

func testStateSupervisorHandoffFixture(pid int, identity string) authority.SupervisorHandoff {
	return authority.SupervisorHandoff{
		Version: authority.SupervisorHandoffVersion, LaunchToken: strings.Repeat("d", authority.TokenBytes*2),
		Secret: strings.Repeat("a", authority.SecretBytes*2), TargetPID: pid, TargetIdentity: identity,
		PipeToken: strings.Repeat("b", authority.TokenBytes*2), JobID: strings.Repeat("c", authority.TokenBytes*2),
	}
}

func testStateSupervisorHandoffAbortProofFixture(handoff authority.SupervisorHandoff) authority.SupervisorHandoffAbortProof {
	return authority.SupervisorHandoffAbortProof{
		Version: authority.SupervisorHandoffAbortProofVersion, Disposition: authority.SupervisorHandoffAbortDisposition,
		LaunchToken: handoff.LaunchToken, JobID: handoff.JobID, OwnerKind: handoff.OwnerKind, OwnerContext: handoff.OwnerContext,
		TargetPID:      handoff.TargetPID,
		TargetIdentity: handoff.TargetIdentity, SupervisorPID: handoff.SupervisorPID, SupervisorIdentity: handoff.SupervisorIdentity,
		CreatorSessionID: cloneStateUint32Pointer(handoff.CreatorSessionID),
	}
}

func testStateSupervisorHandoffStopReceipt(handoff authority.SupervisorHandoff) authority.StopReceipt {
	return authority.StopReceipt{
		Version: authority.SupervisorVersion, Status: "stopped", TargetPID: handoff.TargetPID,
		TargetIdentity: handoff.TargetIdentity, PipeToken: handoff.PipeToken, JobID: handoff.JobID,
		SupervisorPID: handoff.SupervisorPID, SupervisorIdentity: handoff.SupervisorIdentity,
	}
}

func testStateUint32Pointer(value uint32) *uint32 {
	return &value
}

func cloneStateUint32Pointer(value *uint32) *uint32 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func testSupervisorAuthority(pid int, identity string) *authority.Supervisor {
	return &authority.Supervisor{
		Version: authority.SupervisorVersion, Secret: strings.Repeat("a", authority.SecretBytes*2), TargetPID: pid,
		TargetIdentity: identity, PipeToken: strings.Repeat("b", authority.TokenBytes*2), JobID: strings.Repeat("c", authority.TokenBytes*2),
		SupervisorPID: 99, SupervisorIdentity: "windows:99:created-at",
	}
}

func writeJournalForTest(t *testing.T, store *Store, journal RunJournal) {
	t.Helper()
	data, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.journalPath(journal.Key()), data, 0o600); err != nil {
		t.Fatal(err)
	}
}
