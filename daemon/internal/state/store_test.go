package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestStoreExclusiveLockAndClose(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	first, err := New(directory)
	if err != nil {
		t.Fatalf("first New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := first.Close(); err != nil {
			t.Errorf("first Close() error = %v", err)
		}
	})

	if _, err := New(directory); !errors.Is(err, ErrStoreInUse) {
		t.Fatalf("second New() error = %v, want ErrStoreInUse", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("second first.Close() error = %v", err)
	}
	second, err := New(directory)
	if err != nil {
		t.Fatalf("New() after Close() error = %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestStateSecuritySetupFailuresPropagateWithoutToken(t *testing.T) {
	t.Run("state root directory", func(t *testing.T) {
		original := applyDirectorySecurity
		denied := errors.New("security setup denied")
		applyDirectorySecurity = func(string) error { return denied }
		t.Cleanup(func() { applyDirectorySecurity = original })

		if _, err := New(t.TempDir()); !errors.Is(err, denied) || !strings.Contains(err.Error(), "initialize state directory") {
			t.Fatalf("New() error = %v, want wrapped state root security cause", err)
		}
	})
	t.Run("run journal directory", func(t *testing.T) {
		directory := t.TempDir()
		original := applyDirectorySecurity
		denied := errors.New("security setup denied")
		calls := 0
		applyDirectorySecurity = func(string) error {
			calls++
			if calls == 1 {
				return nil
			}
			return denied
		}
		t.Cleanup(func() { applyDirectorySecurity = original })

		store, err := New(directory)
		if store != nil {
			t.Fatal("New() returned a store when runs directory security setup failed")
		}
		if !errors.Is(err, denied) {
			t.Fatalf("New() error = %v, want wrapped security cause", err)
		}
		if !strings.Contains(err.Error(), "create run journal directory") {
			t.Fatalf("New() error = %v, want run journal context", err)
		}
		if calls != 2 {
			t.Fatalf("directory security calls = %d, want 2", calls)
		}
	})
	t.Run("file", func(t *testing.T) {
		store := mustStore(t)
		original := applyFileSecurity
		denied := errors.New("security setup denied")
		applyFileSecurity = func(string) error { return denied }
		t.Cleanup(func() { applyFileSecurity = original })

		const token = "never-report-this-token"
		err := store.SaveIdentity(MachineIdentity{MachineID: "machine-1", MachineToken: token})
		if err == nil {
			t.Fatal("SaveIdentity() succeeded when file security setup failed")
		}
		if strings.Contains(err.Error(), token) {
			t.Fatalf("SaveIdentity() error exposed token: %v", err)
		}
	})
	t.Run("lock file", func(t *testing.T) {
		original := applyFileSecurity
		denied := errors.New("security setup denied")
		applyFileSecurity = func(string) error { return denied }
		t.Cleanup(func() { applyFileSecurity = original })

		if _, err := New(t.TempDir()); !errors.Is(err, denied) {
			t.Fatalf("New() error = %v, want wrapped security cause", err)
		}
	})
}

func TestStatePathsHavePrivateSecurity(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	if err := store.SaveIdentity(MachineIdentity{MachineID: "machine-1", MachineToken: "secret-token"}); err != nil {
		t.Fatalf("SaveIdentity() error = %v", err)
	}
	journal := testJournal("run-1", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	for _, path := range []string{store.dir, store.runsDir()} {
		if err := verifyPrivateDirectory(path); err != nil {
			t.Fatalf("directory security for %q: %v", path, err)
		}
	}
	for _, path := range []string{store.identityPath(), store.journalPath(journal.Key())} {
		if err := verifyPrivateFile(path); err != nil {
			t.Fatalf("file security for %q: %v", path, err)
		}
	}
}

func TestStateRawMessagesRejectDuplicateMembersBeforePersistence(t *testing.T) {
	t.Parallel()
	duplicates := []struct {
		name    string
		payload json.RawMessage
	}{
		{name: "top-level", payload: json.RawMessage(`{"legacy":"first","legacy":"second"}`)},
		{name: "nested", payload: json.RawMessage(`{"legacy":{"value":1,"value":2}}`)},
		{name: "escaped", payload: json.RawMessage(`{"\u006cegacy":"first","legacy":"second"}`)},
	}

	for _, test := range duplicates {
		t.Run("SaveJournal Work.Input rejects "+test.name, func(t *testing.T) {
			t.Parallel()
			store := mustStore(t)
			journal := testJournal("run-raw-work-"+test.name, 1)
			journal.Work.Input = test.payload
			if err := store.SaveJournal(journal); err == nil {
				t.Fatal("SaveJournal() accepted duplicate Work.Input members")
			}
		})

		t.Run("QueueEvent rejects "+test.name, func(t *testing.T) {
			t.Parallel()
			store := mustStore(t)
			journal := testJournal("run-raw-event-"+test.name, 1)
			if err := store.SaveJournal(journal); err != nil {
				t.Fatal(err)
			}
			_, err := store.QueueEvent(journal.Key(), protocol.RunEvent{
				EventID: "event-raw-" + test.name, Sequence: journal.LastEventSequence + 1, Kind: "diagnostic",
				OccurredAt: time.Date(2026, 9, 16, 1, 2, 3, 0, time.UTC), Payload: test.payload,
			})
			if err == nil {
				t.Fatal("QueueEvent() accepted duplicate payload members")
			}
		})

		t.Run("QueueTransition rejects "+test.name, func(t *testing.T) {
			t.Parallel()
			store := mustStore(t)
			journal := testJournal("run-raw-transition-"+test.name, 1)
			if err := store.SaveJournal(journal); err != nil {
				t.Fatal(err)
			}
			_, err := store.QueueTransition(journal.Key(), protocol.StateTransitionRequest{
				TransitionID: "transition-raw-" + test.name, State: "active", Payload: test.payload,
			})
			if err == nil {
				t.Fatal("QueueTransition() accepted duplicate payload members")
			}
		})
	}

	t.Run("legacy unknown Work.Input fields remain allowed", func(t *testing.T) {
		t.Parallel()
		store := mustStore(t)
		journal := testJournal("run-raw-legacy-unknown", 1)
		journal.Work.Input = json.RawMessage(`{"legacy_unknown":{"nested":true}}`)
		if err := store.SaveJournal(journal); err != nil {
			t.Fatalf("SaveJournal() rejected ordinary unknown Work.Input field: %v", err)
		}
	})
}

func TestJournalFileLimitSupportsLargeBacklogAndRejectsOversizeFiles(t *testing.T) {
	t.Parallel()
	t.Run("round trips a backlog larger than the control response limit", func(t *testing.T) {
		t.Parallel()
		store := mustStore(t)
		journal := testJournal("run-1", 1)
		journal.PendingEvents = []protocol.RunEvent{{
			EventID:    "event-1",
			Sequence:   1,
			Kind:       "output",
			OccurredAt: time.Date(2026, time.September, 3, 1, 2, 5, 0, time.UTC),
			Payload:    json.RawMessage(`"` + strings.Repeat("x", (1<<20)+1) + `"`),
		}}
		encoded, err := json.Marshal(journal)
		if err != nil {
			t.Fatal(err)
		}
		if len(encoded) <= maxStateFileBytes || len(encoded) > maxJournalFileBytes {
			t.Fatalf("serialized journal = %d bytes, want more than %d and no more than %d", len(encoded), maxStateFileBytes, maxJournalFileBytes)
		}
		if err := store.SaveJournal(journal); err != nil {
			t.Fatalf("SaveJournal() error = %v", err)
		}
		loaded, err := store.LoadJournal(journal.Key())
		if err != nil {
			t.Fatalf("LoadJournal() error = %v", err)
		}
		if len(loaded.PendingEvents) != 1 || string(loaded.PendingEvents[0].Payload) != string(journal.PendingEvents[0].Payload) {
			t.Fatalf("loaded large backlog = %#v", loaded.PendingEvents)
		}
	})

	t.Run("rejects writes and reads beyond the journal ceiling", func(t *testing.T) {
		t.Parallel()
		store := mustStore(t)
		journal := testJournal("run-1", 1)
		journal.PendingEvents = []protocol.RunEvent{{
			EventID:    "event-1",
			Sequence:   1,
			Kind:       "output",
			OccurredAt: time.Date(2026, time.September, 3, 1, 2, 5, 0, time.UTC),
			Payload:    json.RawMessage(`"` + strings.Repeat("x", maxJournalFileBytes) + `"`),
		}}
		if err := store.SaveJournal(journal); err == nil {
			t.Fatal("SaveJournal() succeeded beyond the journal size limit")
		}
		if err := os.WriteFile(store.journalPath(journal.Key()), []byte(strings.Repeat("x", maxJournalFileBytes+1)), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := store.LoadJournal(journal.Key()); err == nil {
			t.Fatal("LoadJournal() succeeded beyond the journal size limit")
		}
	})
}

func TestListJournalsIncludesMultipleGenerationsAndCleansOnlyOwnedTemps(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	first := testJournal("run-1", 1)
	second := testJournal("run-1", 2)
	if err := store.SaveJournal(first); err != nil {
		t.Fatalf("SaveJournal(first) error = %v", err)
	}
	if err := store.SaveJournal(second); err != nil {
		t.Fatalf("SaveJournal(second) error = %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(store.journalPath(first.Key()))
		if err != nil {
			t.Fatalf("Stat(journal) error = %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("journal permissions = %o, want 600", got)
		}
	}
	ownedTemp := filepath.Join(store.runsDir(), atomicTempPrefix+strings.Repeat("a", 32)+".tmp")
	foreignTemp := filepath.Join(store.runsDir(), "foreign.tmp")
	for _, path := range []string{ownedTemp, foreignTemp} {
		if err := os.WriteFile(path, []byte("partial"), 0o600); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", path, err)
		}
	}

	got, err := store.ListJournals()
	if err != nil {
		t.Fatalf("ListJournals() error = %v", err)
	}
	if len(got) != 2 || got[0].Generation != 1 || got[1].Generation != 2 {
		t.Fatalf("ListJournals() = %#v", got)
	}
	if _, err := os.Stat(ownedTemp); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("owned temporary file was not removed: %v", err)
	}
	if _, err := os.Stat(foreignTemp); err != nil {
		t.Fatalf("foreign temporary file was removed: %v", err)
	}
}

func TestListJournalsRejectsCorruptAuthoritativeJournal(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	path := store.journalPath(RunKey{RunID: "run-1", Generation: 1})
	if err := os.WriteFile(path, []byte(`{"run_id":"run-1"`), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := store.ListJournals(); err == nil {
		t.Fatal("ListJournals() succeeded with corrupt authoritative journal")
	}
}

func TestDeleteJournalDeletesOnlyTarget(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	first := stoppedTestJournal("run-1", 1)
	second := testJournal("run-1", 2)
	for _, journal := range []RunJournal{first, second} {
		if err := store.SaveJournal(journal); err != nil {
			t.Fatalf("SaveJournal() error = %v", err)
		}
	}
	if err := store.DeleteJournal(first.Key()); err != nil {
		t.Fatalf("DeleteJournal() error = %v", err)
	}
	if _, err := store.LoadJournal(first.Key()); !IsNotFound(err) {
		t.Fatalf("LoadJournal(deleted) error = %v, want not found", err)
	}
	if _, err := store.LoadJournal(second.Key()); err != nil {
		t.Fatalf("LoadJournal(remaining) error = %v", err)
	}
}

func TestConcurrentSavesLeaveWholeJSON(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}

	var group sync.WaitGroup
	for index := range 32 {
		group.Add(1)
		go func() {
			defer group.Done()
			copy := journal
			copy.LocalState = "state"
			copy.Work.Goal = strconv.Itoa(index)
			copy.LastEventSequence = int64(index + 1)
			if err := store.SaveJournal(copy); err != nil {
				t.Errorf("SaveJournal() error = %v", err)
			}
		}()
	}
	group.Wait()

	bytes, err := os.ReadFile(store.journalPath(journal.Key()))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var got RunJournal
	if err := json.Unmarshal(bytes, &got); err != nil {
		t.Fatalf("journal JSON is not whole JSON: %v", err)
	}
	writer, err := strconv.Atoi(got.Work.Goal)
	if err != nil || writer < 0 || writer >= 32 || got.LastEventSequence != int64(writer+1) || got.PID != journal.PID || got.ProcessIdentity != journal.ProcessIdentity {
		t.Fatalf("concurrent save mixed writers or changed process ownership: %+v", got)
	}
}
