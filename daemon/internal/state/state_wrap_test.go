package state

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// TestSaveJournalWrapsAtomicWriteCause pins the contract's wrapped write path:
// the durable write error carries the concrete writer cause, its message prefix
// is unchanged, and the cause is reachable with errors.Is (a %w chain, not %v
// text). The goal-session lane pins the same seam for the GoalSession journal,
// whose write failure is classified by the app's recovery paths.
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
		// The uncertain transition also refreshes the derived lineage index, so
		// the injected failure is scoped to the journal path itself.
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

// TestLoadJournalWrapsDecodeCause pins the read/decode prefix: the base
// revision returned the bare verdict string, so the cause of an unreadable or
// malformed durable journal was invisible in the field.
func TestLoadJournalWrapsDecodeCause(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	journal := testJournal("run-wrap-decode", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	// A valid JSON document that is not a RunJournal object reaches the
	// decoder and fails there.
	if err := os.WriteFile(store.journalPath(journal.Key()), []byte("[]"), 0o600); err != nil {
		t.Fatalf("write corrupt journal: %v", err)
	}

	_, err := store.LoadJournal(journal.Key())
	if err == nil {
		t.Fatal("LoadJournal() accepted a corrupt journal")
	}
	if !strings.Contains(err.Error(), "decode run journal: ") {
		t.Fatalf("LoadJournal() error = %q, want the decode prefix plus cause", err)
	}
	if errors.Unwrap(err) == nil {
		t.Fatalf("LoadJournal() error = %q, want the decoder cause in its error chain", err)
	}
	if err.Error() == "decode run journal" {
		t.Fatalf("LoadJournal() error = %q, want the concrete decode cause", err)
	}
}
