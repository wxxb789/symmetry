//go:build linux

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

func TestRestartStopPersistsAuthenticatedHelperSealOnlyWithPriorMirrorSeal(t *testing.T) {
	t.Run("unsealed mirror remains unresolved", func(t *testing.T) {
		store, key, _ := committedLinuxRecoveryScanJournal(t, false)
		defer store.Close()
		terminateCalls := 0
		app := &daemon{
			store:   store,
			running: make(map[state.RunKey]*runningRun),
			options: options{
				terminatePersistAuthority: func(int, string, *authority.Supervisor) (authority.StopReceipt, error) {
					terminateCalls++
					return authority.StopReceipt{}, errors.New("unsealed mirror must not contact helper")
				},
				terminatePersistAuthorityWithScanSeal: func(int, string, *authority.Supervisor) (authority.StopReceipt, uint64, error) {
					terminateCalls++
					return authority.StopReceipt{}, 0, errors.New("unsealed mirror must not contact helper")
				},
			},
		}
		journal, err := store.LoadJournal(key)
		if err != nil {
			t.Fatal(err)
		}
		if err := app.stopPersistedProcess(context.Background(), journal); !errors.Is(err, errPersistedProcessStopUnproven) {
			t.Fatalf("stopPersistedProcess() = %v, want unresolved unsealed mirror", err)
		}
		if terminateCalls != 0 {
			t.Fatalf("helper termination calls = %d, want none with an unsealed mirror", terminateCalls)
		}
		latest, err := store.LoadJournal(key)
		if err != nil {
			t.Fatal(err)
		}
		if latest.ContainmentAuthority == nil || latest.ContainmentAuthority.StopReceipt != nil ||
			latest.ContainmentScans == nil || latest.ContainmentScans.Mirror.SealedSequence != 0 {
			t.Fatalf("unsealed mirror recovery changed durable authority: %#v", latest)
		}
	})

	t.Run("authenticated helper terminal seal precedes receipt release and clear", func(t *testing.T) {
		store, key, value := committedLinuxRecoveryScanJournal(t, true)
		defer store.Close()
		receipt := linuxRecoveryStopReceipt(value)
		previousRelease := releasePersistedContainmentAuthority
		t.Cleanup(func() { releasePersistedContainmentAuthority = previousRelease })
		releaseCalled := false
		releasePersistedContainmentAuthority = func(pid int, identity string, persisted *authority.Supervisor) error {
			releaseCalled = true
			if pid != value.TargetPID || identity != value.TargetIdentity || persisted == nil || persisted.StopReceipt == nil || *persisted.StopReceipt != receipt {
				t.Fatalf("release identity = (%d, %q, %#v), want exact durable receipt", pid, identity, persisted)
			}
			latest, err := store.LoadJournal(key)
			if err != nil {
				return err
			}
			if latest.ContainmentScans == nil ||
				latest.ContainmentScans.Helper.SealedSequence == 0 || latest.ContainmentScans.Helper.SealedSequence != latest.ContainmentScans.Helper.CompletedSequence ||
				latest.ContainmentScans.Mirror.SealedSequence == 0 || latest.ContainmentScans.Mirror.SealedSequence != latest.ContainmentScans.Mirror.CompletedSequence ||
				latest.ContainmentAuthority == nil || latest.ContainmentAuthority.StopReceipt == nil {
				t.Fatal("helper release ran before both scan seals and the receipt were durable")
			}
			return nil
		}
		app := &daemon{
			store:   store,
			running: make(map[state.RunKey]*runningRun),
			options: options{
				terminatePersistAuthority: func(int, string, *authority.Supervisor) (authority.StopReceipt, error) {
					t.Fatal("ordinary stop adapter bypassed the required helper terminal-seal proof")
					return authority.StopReceipt{}, nil
				},
				terminatePersistAuthorityWithScanSeal: func(pid int, identity string, persisted *authority.Supervisor) (authority.StopReceipt, uint64, error) {
					if pid != value.TargetPID || identity != value.TargetIdentity || persisted == nil || !persisted.Equal(value) {
						t.Fatalf("authenticated helper stop identity = (%d, %q, %#v), want exact authority", pid, identity, persisted)
					}
					return receipt, 2, nil
				},
			},
		}
		journal, err := store.LoadJournal(key)
		if err != nil {
			t.Fatal(err)
		}
		if err := app.stopPersistedProcess(context.Background(), journal); err != nil {
			t.Fatalf("stopPersistedProcess() = %v", err)
		}
		if !releaseCalled {
			t.Fatal("durable helper receipt did not reach release")
		}
		latest, err := store.LoadJournal(key)
		if err != nil {
			t.Fatal(err)
		}
		if latest.ContainmentScans == nil || latest.ContainmentScans.Helper.SealedSequence != 2 ||
			latest.ContainmentScans.Mirror.SealedSequence != latest.ContainmentScans.Mirror.CompletedSequence ||
			latest.ContainmentAuthority == nil || latest.ContainmentAuthority.StopReceipt == nil || *latest.ContainmentAuthority.StopReceipt != receipt {
			t.Fatalf("restart stop result = %#v", latest)
		}
		cleared, err := store.ClearProcessDetails(key, value.TargetPID, value.TargetIdentity)
		if err != nil {
			t.Fatalf("clear after exact receipt and release: %v", err)
		}
		if cleared.HasProcessDetails() || cleared.ContainmentScans != nil || cleared.ContainmentAuthority != nil {
			t.Fatalf("cleared authority remains: %#v", cleared)
		}
	})
}

func committedLinuxRecoveryScanJournal(t *testing.T, sealMirror bool) (*state.Store, state.RunKey, authority.Supervisor) {
	t.Helper()
	store, key := claimedStore(t)
	launch := authority.SupervisorHandoff{
		Version: authority.SupervisorHandoffVersion, LaunchToken: strings.Repeat("d", authority.TokenBytes*2),
		Secret: strings.Repeat("a", authority.SecretBytes*2), OwnerKind: authority.OwnerKindLinuxHelper,
		OwnerContext: "linux-helper:app-recovery|endpoint=/tmp/app-recovery.sock",
		TargetPID:    42, TargetIdentity: "linux:v2:app-recovery-target", PipeToken: strings.Repeat("b", authority.TokenBytes*2),
		JobID: strings.Repeat("c", authority.TokenBytes*2),
	}
	if _, err := store.PrepareSupervisorHandoff(key, launch); err != nil {
		t.Fatal(err)
	}
	bind := launch.Clone()
	bind.ScanUpdate = &authority.SupervisorScanUpdate{
		MirrorIdentity: strings.Repeat("e", authority.TokenBytes*2), Owner: authority.SupervisorScanOwnerMirror,
		Sequence: 1, Phase: authority.SupervisorScanPhaseInitialize,
	}
	bound, err := store.BindSupervisorHandoff(key, bind, 99, "linux:v2:app-recovery-helper")
	if err != nil {
		t.Fatal(err)
	}
	handoff := *bound.ContainmentHandoff
	for _, owner := range []string{authority.SupervisorScanOwnerHelper, authority.SupervisorScanOwnerMirror} {
		for _, phase := range []string{authority.SupervisorScanPhaseBegin, authority.SupervisorScanPhaseClean, authority.SupervisorScanPhaseRetire} {
			update := handoff.Clone()
			helperPID := update.SupervisorPID
			helperIdentity := update.SupervisorIdentity
			update.SupervisorPID = 0
			update.SupervisorIdentity = ""
			update.ScanUpdate = &authority.SupervisorScanUpdate{
				SupervisorPID: helperPID, SupervisorIdentity: helperIdentity, MirrorIdentity: strings.Repeat("e", authority.TokenBytes*2),
				Owner: owner, Sequence: 1, Phase: phase,
			}
			if _, err := store.PrepareSupervisorHandoff(key, update); err != nil {
				t.Fatalf("persist %s scan %s: %v", owner, phase, err)
			}
		}
	}
	if _, err := store.CommitSupervisorHandoff(key, handoff, time.Date(2026, 9, 26, 10, 11, 12, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	if sealMirror {
		update := handoff.Clone()
		helperPID := update.SupervisorPID
		helperIdentity := update.SupervisorIdentity
		update.SupervisorPID = 0
		update.SupervisorIdentity = ""
		update.ScanUpdate = &authority.SupervisorScanUpdate{
			SupervisorPID: helperPID, SupervisorIdentity: helperIdentity, MirrorIdentity: strings.Repeat("e", authority.TokenBytes*2),
			Owner: authority.SupervisorScanOwnerMirror, Sequence: 1, Phase: authority.SupervisorScanPhaseSeal,
		}
		if _, err := store.PrepareSupervisorHandoff(key, update); err != nil {
			t.Fatal(err)
		}
	}
	journal, err := store.LoadJournal(key)
	if err != nil || journal.ContainmentAuthority == nil {
		t.Fatalf("load committed Linux recovery authority = %#v, %v", journal, err)
	}
	return store, key, journal.ContainmentAuthority.Clone()
}

func linuxRecoveryStopReceipt(value authority.Supervisor) authority.StopReceipt {
	return authority.StopReceipt{
		Version: authority.SupervisorVersion, Status: "stopped", OwnerKind: value.OwnerKind, OwnerContext: value.OwnerContext,
		TargetPID: value.TargetPID, TargetIdentity: value.TargetIdentity, PipeToken: value.PipeToken, JobID: value.JobID,
		SupervisorPID: value.SupervisorPID, SupervisorIdentity: value.SupervisorIdentity, ActiveProcesses: 0,
	}
}
