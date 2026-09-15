package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestStopPersistedProcessDurableReceiptPrecedesHelperRelease(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	startedAt := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	value := testRecoverySupervisorAuthority()
	if _, err := store.SetProcessDetails(key, value.TargetPID, value.TargetIdentity, startedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetContainmentAuthority(key, value.TargetPID, value.TargetIdentity, value); err != nil {
		t.Fatal(err)
	}
	receipt := testRecoveryStopReceipt(value)
	releaseSeen := false
	previousRelease := releasePersistedContainmentAuthority
	t.Cleanup(func() { releasePersistedContainmentAuthority = previousRelease })
	releasePersistedContainmentAuthority = func(pid int, identity string, persisted *authority.Supervisor) error {
		releaseSeen = true
		if pid != value.TargetPID || identity != value.TargetIdentity || persisted.StopReceipt == nil {
			t.Fatalf("release authority = (%d, %q, %#v), want durable receipt", pid, identity, persisted)
		}
		journal, err := store.LoadJournal(key)
		if err != nil {
			return err
		}
		if journal.ContainmentAuthority == nil || journal.ContainmentAuthority.StopReceipt == nil {
			t.Fatal("helper release ran before StopReceipt was durable")
		}
		return nil
	}
	app := &daemon{
		store:   store,
		running: make(map[state.RunKey]*runningRun),
		options: options{terminatePersistAuthority: func(pid int, identity string, persisted *authority.Supervisor) (authority.StopReceipt, error) {
			if pid != value.TargetPID || identity != value.TargetIdentity || persisted == nil {
				t.Fatalf("terminate authority = (%d, %q, %#v)", pid, identity, persisted)
			}
			return receipt, nil
		}},
	}

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.stopPersistedProcess(context.Background(), journal); err != nil {
		t.Fatalf("stopPersistedProcess() error = %v", err)
	}
	if !releaseSeen {
		t.Fatal("durable stop did not release the helper")
	}
}

func TestPersistProcessAuthorityCommitsCompleteProcessPair(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	now := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	value := testRecoverySupervisorAuthority()
	daemon := &daemon{
		store:   store,
		options: options{clock: func() time.Time { return now }},
	}

	if err := daemon.persistProcessAuthority(key, value.TargetPID, value.TargetIdentity, value); err != nil {
		t.Fatalf("persistProcessAuthority() error = %v", err)
	}
	journ, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journ.PID != value.TargetPID || journ.ProcessIdentity != value.TargetIdentity || !journ.StartedAt.Equal(now) {
		t.Fatalf("process details = (%d, %q, %s), want atomic process pair at %s", journ.PID, journ.ProcessIdentity, journ.StartedAt, now)
	}
	if journ.ContainmentAuthority == nil || !journ.ContainmentAuthority.Equal(value) {
		t.Fatalf("containment authority = %#v, want %#v", journ.ContainmentAuthority, value)
	}
}

func TestStopPersistedProcessRejectsReceiptMismatchBeforeHelperRelease(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	startedAt := time.Date(2026, 9, 14, 12, 1, 0, 0, time.UTC)
	value := testRecoverySupervisorAuthority()
	if _, err := store.SetProcessDetails(key, value.TargetPID, value.TargetIdentity, startedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetContainmentAuthority(key, value.TargetPID, value.TargetIdentity, value); err != nil {
		t.Fatal(err)
	}
	receipt := testRecoveryStopReceipt(value)
	receipt.PipeToken = strings.Repeat("d", authority.TokenBytes*2)
	releaseSeen := false
	previousRelease := releasePersistedContainmentAuthority
	t.Cleanup(func() { releasePersistedContainmentAuthority = previousRelease })
	releasePersistedContainmentAuthority = func(int, string, *authority.Supervisor) error {
		releaseSeen = true
		return errors.New("release must not run for a mismatched receipt")
	}
	app := &daemon{
		store:   store,
		running: make(map[state.RunKey]*runningRun),
		options: options{terminatePersistAuthority: func(int, string, *authority.Supervisor) (authority.StopReceipt, error) {
			return receipt, nil
		}},
	}

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.stopPersistedProcess(context.Background(), journal); !errors.Is(err, errPersistedProcessStopUnproven) {
		t.Fatalf("mismatched receipt error = %v, want unproven stop", err)
	}
	if releaseSeen {
		t.Fatal("mismatched receipt reached helper release")
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.ContainmentAuthority == nil || journal.ContainmentAuthority.StopReceipt != nil {
		t.Fatalf("mismatched receipt was persisted: %#v", journal.ContainmentAuthority)
	}
}

func TestStopPersistedProcessRetainsAuthorityWhenReceiptPersistenceFails(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	value := testRecoverySupervisorAuthority()
	startedAt := time.Date(2026, 9, 14, 12, 2, 0, 0, time.UTC)
	if _, err := store.SetProcessDetails(key, value.TargetPID, value.TargetIdentity, startedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetContainmentAuthority(key, value.TargetPID, value.TargetIdentity, value); err != nil {
		t.Fatal(err)
	}
	receipt := testRecoveryStopReceipt(value)
	terminateCalls := 0
	releaseCalls := 0
	previousRelease := releasePersistedContainmentAuthority
	t.Cleanup(func() { releasePersistedContainmentAuthority = previousRelease })
	releasePersistedContainmentAuthority = func(int, string, *authority.Supervisor) error {
		releaseCalls++
		return nil
	}
	app := &daemon{
		store:   store,
		running: make(map[state.RunKey]*runningRun),
		options: options{terminatePersistAuthority: func(int, string, *authority.Supervisor) (authority.StopReceipt, error) {
			terminateCalls++
			return receipt, nil
		}},
	}

	restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
		return errors.New("injected receipt persistence failure")
	})
	journal, err := store.LoadJournal(key)
	if err != nil {
		restore()
		t.Fatal(err)
	}
	if err := app.stopPersistedProcess(context.Background(), journal); err == nil {
		restore()
		t.Fatal("receipt persistence failure was ignored")
	}
	restore()
	if releaseCalls != 0 {
		t.Fatalf("release calls after receipt persistence failure = %d, want 0", releaseCalls)
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.ContainmentAuthority == nil || journal.ContainmentAuthority.StopReceipt != nil {
		t.Fatalf("failed receipt write changed durable authority: %#v", journal.ContainmentAuthority)
	}

	if err := app.stopPersistedProcess(context.Background(), journal); err != nil {
		t.Fatalf("retry stopPersistedProcess() error = %v", err)
	}
	if terminateCalls != 2 {
		t.Fatalf("terminate calls = %d, want one retry after the failed durable write", terminateCalls)
	}
	if releaseCalls != 1 {
		t.Fatalf("release calls after durable retry = %d, want 1", releaseCalls)
	}
}

func TestStopPersistedProcessRetriesReleaseAfterDurableReceipt(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	value := testRecoverySupervisorAuthority()
	startedAt := time.Date(2026, 9, 14, 12, 3, 0, 0, time.UTC)
	if _, err := store.SetProcessDetails(key, value.TargetPID, value.TargetIdentity, startedAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetContainmentAuthority(key, value.TargetPID, value.TargetIdentity, value); err != nil {
		t.Fatal(err)
	}
	receipt := testRecoveryStopReceipt(value)
	terminateCalls := 0
	releaseCalls := 0
	previousRelease := releasePersistedContainmentAuthority
	t.Cleanup(func() { releasePersistedContainmentAuthority = previousRelease })
	releasePersistedContainmentAuthority = func(int, string, *authority.Supervisor) error {
		releaseCalls++
		if releaseCalls == 1 {
			return errors.New("simulated release response loss")
		}
		return nil
	}
	app := &daemon{
		store:   store,
		running: make(map[state.RunKey]*runningRun),
		options: options{terminatePersistAuthority: func(int, string, *authority.Supervisor) (authority.StopReceipt, error) {
			terminateCalls++
			return receipt, nil
		}},
	}

	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.stopPersistedProcess(context.Background(), journal); err == nil {
		t.Fatal("release response loss was ignored")
	}
	journal, err = store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.ContainmentAuthority == nil || journal.ContainmentAuthority.StopReceipt == nil {
		t.Fatalf("durable receipt was lost after release response loss: %#v", journal.ContainmentAuthority)
	}
	if terminateCalls != 1 || releaseCalls != 1 {
		t.Fatalf("first attempt calls = terminate:%d release:%d, want 1 and 1", terminateCalls, releaseCalls)
	}

	if err := app.stopPersistedProcess(context.Background(), journal); err != nil {
		t.Fatalf("release retry stopPersistedProcess() error = %v", err)
	}
	if terminateCalls != 1 || releaseCalls != 2 {
		t.Fatalf("retry calls = terminate:%d release:%d, want no re-terminate and one release retry", terminateCalls, releaseCalls)
	}
}

func testRecoverySupervisorAuthority() authority.Supervisor {
	return authority.Supervisor{
		Version:            authority.SupervisorVersion,
		Secret:             strings.Repeat("a", authority.SecretBytes*2),
		TargetPID:          81,
		TargetIdentity:     "windows:81:0000000000000001",
		PipeToken:          strings.Repeat("b", authority.TokenBytes*2),
		JobID:              strings.Repeat("c", authority.TokenBytes*2),
		SupervisorPID:      82,
		SupervisorIdentity: "windows:82:0000000000000002",
	}
}

func testRecoveryStopReceipt(value authority.Supervisor) authority.StopReceipt {
	return authority.StopReceipt{
		Version:            authority.SupervisorVersion,
		Status:             "stopped",
		TargetPID:          value.TargetPID,
		TargetIdentity:     value.TargetIdentity,
		PipeToken:          value.PipeToken,
		JobID:              value.JobID,
		SupervisorPID:      value.SupervisorPID,
		SupervisorIdentity: value.SupervisorIdentity,
		ActiveProcesses:    0,
	}
}
