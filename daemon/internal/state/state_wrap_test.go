package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestSaveJournalWrapsAtomicWriteCause(t *testing.T) {
	injected := errors.New("injected atomic writer failure")

	t.Run("run journal", func(t *testing.T) {
		store := mustStore(t)
		journal := testJournal("run-wrap-write", 1)
		if err := store.SaveJournal(journal); err != nil {
			t.Fatalf("SaveJournal() error = %v", err)
		}
		restore := store.SetAtomicWriterForTesting(func(string, []byte) error { return injected })
		defer restore()

		err := store.SaveJournal(journal)
		if err == nil {
			t.Fatal("SaveJournal() with a failing atomic writer succeeded")
		}
		if !errors.Is(err, injected) {
			t.Fatalf("SaveJournal() error = %v, want the injected writer cause in its chain", err)
		}
		if got, want := err.Error(), "write run journal: "+injected.Error(); got != want {
			t.Fatalf("SaveJournal() error = %q, want %q", got, want)
		}
	})

	t.Run("goal session journal", func(t *testing.T) {
		store := mustStore(t)
		saved, err := store.SaveGoalSessionLaunchIntent(testGoalSessionIntent())
		if err != nil {
			t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
		}

		journalPath := store.goalSessionPath(saved.Key())
		restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
			if path == journalPath {
				return injected
			}
			return writeAtomic(path, data)
		})
		defer restore()

		_, err = store.MarkGoalSessionUncertain(saved.Key(), "native launch result was lost")
		if err == nil {
			t.Fatal("MarkGoalSessionUncertain() with a failing atomic writer succeeded")
		}
		if !errors.Is(err, injected) {
			t.Fatalf("MarkGoalSessionUncertain() error = %v, want the injected writer cause in its chain", err)
		}
		if got, want := err.Error(), "write goal session journal: "+injected.Error(); got != want {
			t.Fatalf("MarkGoalSessionUncertain() error = %q, want %q", got, want)
		}
	})
}

func TestLoadJournalWrapsDecodeCause(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	journal := testJournal("run-wrap-decode", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}

	if err := os.WriteFile(store.journalPath(journal.Key()), []byte("[]"), 0o600); err != nil {
		t.Fatalf("write corrupt journal: %v", err)
	}

	_, err := store.LoadJournal(journal.Key())
	if err == nil {
		t.Fatal("LoadJournal() accepted a corrupt journal")
	}
	if !strings.HasPrefix(err.Error(), "decode run journal: ") {
		t.Fatalf("LoadJournal() error = %q, want the decode prefix plus cause", err)
	}
	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &typeErr) || typeErr.Value != "array" {
		t.Fatalf("LoadJournal() error = %v, want the array-root *json.UnmarshalTypeError in its chain", err)
	}
}

// Pins the 4096-byte durable reason budget: the app composes "<prefix>: <cause>", so the store enforces it on write
// and read; exactly 4096 bytes is accepted and loadable again, one more is rejected before the mutation.
func TestMarkGoalSessionUncertainReasonBudget(t *testing.T) {
	const limit = 4096
	store := mustStore(t)
	saved, err := store.SaveGoalSessionLaunchIntent(testGoalSessionIntent())
	if err != nil {
		t.Fatalf("SaveGoalSessionLaunchIntent() error = %v", err)
	}
	key := saved.Key()
	if _, err := store.MarkGoalSessionLaunchStarted(key); err != nil {
		t.Fatalf("MarkGoalSessionLaunchStarted() error = %v", err)
	}
	path := store.goalSessionPath(key)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read goal session journal: %v", err)
	}

	_, err = store.MarkGoalSessionUncertain(key, strings.Repeat("r", limit+1))
	if err == nil || err.Error() != "uncertain launch reason is invalid" {
		t.Fatalf("MarkGoalSessionUncertain() with a %d-byte reason error = %v, want the budget rejection", limit+1, err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read goal session journal: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("a rejected reason changed the durable journal")
	}

	if _, err := store.MarkGoalSessionUncertain(key, strings.Repeat("r", limit)); err != nil {
		t.Fatalf("MarkGoalSessionUncertain() with a %d-byte reason error = %v, want acceptance", limit, err)
	}
	loaded, err := store.LoadGoalSession(key)
	if err != nil {
		t.Fatalf("LoadGoalSession() error = %v", err)
	}
	if len(loaded.UncertainReason) != limit {
		t.Fatalf("durable reason bytes = %d, want %d", len(loaded.UncertainReason), limit)
	}
}
