//go:build linux

package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestStopPersistedProcessHonorsLatestContainmentUnprovenMarker(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()

	const (
		pid      = 4242
		identity = "linux:v2:01234567-89ab-cdef-0123-456789abcdef:77:4242:456"
	)
	startedAt := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	if _, err := store.SetProcessDetails(key, pid, identity, startedAt); err != nil {
		t.Fatal(err)
	}
	stale, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if stale.ContainmentUnproven {
		t.Fatal("stale recovery snapshot unexpectedly contained containment uncertainty")
	}
	if _, err := store.MarkContainmentUnproven(key, pid, identity); err != nil {
		t.Fatal(err)
	}

	stopCalls := 0
	app := &daemon{
		store:   store,
		running: make(map[state.RunKey]*runningRun),
		options: options{terminatePersist: func(int, string) error { stopCalls++; return nil }},
	}
	if err := app.stopPersistedProcess(context.Background(), stale); !errors.Is(err, errPersistedProcessStopUnproven) {
		t.Fatalf("stopPersistedProcess() error = %v, want unresolved containment", err)
	}
	if stopCalls != 0 {
		t.Fatalf("termination calls = %d, want no PGID absence proof after restart", stopCalls)
	}

	latest, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !latest.HasProcessDetails() || !latest.ContainmentUnproven {
		t.Fatalf("latest journal = %#v, want retained process marker and uncertainty", latest)
	}
}

func TestRestartInputRecoveryRequiresPersistedProcessMarker(t *testing.T) {
	journal := state.RunJournal{
		LocalState:          "running",
		PID:                 4242,
		ProcessIdentity:     "linux:v2:01234567-89ab-cdef-0123-456789abcdef:77:4242:456",
		StartedAt:           time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC),
		ContainmentUnproven: true,
	}
	if !restartInputRecoveryRequired(journal) {
		t.Fatal("restartInputRecoveryRequired() = false, want true for persisted containment marker")
	}
}
