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
		LaunchToken: handoff.LaunchToken, JobID: handoff.JobID, TargetPID: handoff.TargetPID,
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
