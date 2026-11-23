package state

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestSetProcessDetailsPersistsNonEmptyIdentity(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	journal := stoppedTestJournal("run-1", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	key := journal.Key()
	startedAt := time.Date(2026, 9, 2, 1, 2, 5, 0, time.UTC)
	updated, err := store.SetProcessDetails(key, 99, "windows:99:created-at", startedAt)
	if err != nil {
		t.Fatalf("SetProcessDetails() error = %v", err)
	}
	if updated.PID != 99 || updated.ProcessIdentity != "windows:99:created-at" || !updated.StartedAt.Equal(startedAt) {
		t.Fatalf("SetProcessDetails() = %#v", updated)
	}
	restarted, err := New(store.dir)
	if !errors.Is(err, ErrStoreInUse) {
		if err == nil {
			_ = restarted.Close()
		}
		t.Fatalf("New() before Close() error = %v, want ErrStoreInUse", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	restarted, err = New(store.dir)
	if err != nil {
		t.Fatalf("New() after Close() error = %v", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	loaded, err := restarted.LoadJournal(key)
	if err != nil {
		t.Fatalf("LoadJournal() error = %v", err)
	}
	if loaded.ProcessIdentity != "windows:99:created-at" {
		t.Fatalf("ProcessIdentity after restart = %q", loaded.ProcessIdentity)
	}
}

func TestSetProcessDetailsRejectsInvalidIdentityWithoutMutation(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	key := journal.Key()
	cases := []string{"", " ", strings.Repeat("x", 4097)}
	for _, identity := range cases {
		if _, err := store.SetProcessDetails(key, 99, identity, time.Now().UTC()); err == nil {
			t.Fatalf("SetProcessDetails(%q) succeeded", identity)
		}
	}
	got, err := store.LoadJournal(key)
	if err != nil {
		t.Fatalf("LoadJournal() error = %v", err)
	}
	if got.PID != journal.PID || got.ProcessIdentity != journal.ProcessIdentity || !got.StartedAt.Equal(journal.StartedAt) {
		t.Fatalf("journal mutated by invalid process details: %#v", got)
	}
}

func TestClearProcessDetailsUsesExpectedIdentityAndIsIdempotent(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	journal := stoppedTestJournal("run-1", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	key := journal.Key()
	startedAt := time.Date(2026, 9, 9, 1, 2, 5, 0, time.UTC)
	if _, err := store.SetProcessDetails(key, 99, "linux:99:created-at", startedAt); err != nil {
		t.Fatalf("SetProcessDetails() error = %v", err)
	}
	cleared, err := store.ClearProcessDetails(key, 99, "linux:99:created-at")
	if err != nil {
		t.Fatalf("ClearProcessDetails() error = %v", err)
	}
	if cleared.PID != 0 || cleared.ProcessIdentity != "" || !cleared.StartedAt.IsZero() {
		t.Fatalf("cleared process details = %#v", cleared)
	}
	if _, err := store.ClearProcessDetails(key, 99, "linux:99:created-at"); err != nil {
		t.Fatalf("replayed ClearProcessDetails() error = %v", err)
	}
	if _, err := store.SetProcessDetails(key, 100, "linux:100:created-at", startedAt); err != nil {
		t.Fatalf("replacement SetProcessDetails() error = %v", err)
	}
	if _, err := store.ClearProcessDetails(key, 99, "linux:99:created-at"); err == nil {
		t.Fatal("ClearProcessDetails() erased a changed process identity")
	}
	current, err := store.LoadJournal(key)
	if err != nil {
		t.Fatalf("LoadJournal() error = %v", err)
	}
	if current.PID != 100 || current.ProcessIdentity != "linux:100:created-at" || !current.StartedAt.Equal(startedAt) {
		t.Fatalf("changed process details = %#v", current)
	}
}
