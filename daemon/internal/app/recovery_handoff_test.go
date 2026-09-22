package app

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestRecoverPersistedSupervisorHandoffAbortsUnboundLaunch(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	handoff := testRecoveryHandoffFixture(44, "windows:44:created-at")
	if _, err := store.PrepareSupervisorHandoff(key, handoff); err != nil {
		t.Fatalf("PrepareSupervisorHandoff() error = %v", err)
	}
	abortCalls := 0
	previousAbort := provePreparedSupervisorAbortedForRecovery
	t.Cleanup(func() { provePreparedSupervisorAbortedForRecovery = previousAbort })
	provePreparedSupervisorAbortedForRecovery = func(got authority.SupervisorHandoff) (authority.SupervisorHandoffAbortProof, error) {
		abortCalls++
		if !got.Equal(handoff) {
			t.Fatalf("abort handoff = %#v, want %#v", got, handoff)
		}
		return testRecoverySupervisorHandoffAbortProof(got), nil
	}
	app := &daemon{store: store}
	if err := app.recoverPersistedSupervisorHandoff(key, handoff); err != nil {
		t.Fatalf("recoverPersistedSupervisorHandoff() error = %v", err)
	}
	if abortCalls != 1 {
		t.Fatalf("abort proof calls = %d, want 1", abortCalls)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.ContainmentHandoff != nil || journal.HasProcessDetails() || journal.ContainmentAuthority != nil {
		t.Fatalf("aborted handoff journal = %#v, want no pending containment", journal)
	}
}

func TestRecoverPersistedSupervisorHandoffStopsAndReleasesBoundLaunch(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	handoff := testRecoveryHandoffFixture(45, "windows:45:created-at")
	if _, err := store.PrepareSupervisorHandoff(key, handoff); err != nil {
		t.Fatalf("PrepareSupervisorHandoff() error = %v", err)
	}
	bound, err := store.BindSupervisorHandoff(key, handoff, 46, "windows:46:created-at")
	if err != nil {
		t.Fatalf("BindSupervisorHandoff() error = %v", err)
	}
	expected := *bound.ContainmentHandoff
	value, err := expected.ToSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	receipt := testRecoveryStopReceipt(value)
	recoverCalls := 0
	previousRecover := recoverPreparedSupervisorForRecovery
	previousRelease := releasePreparedSupervisorForRecovery
	t.Cleanup(func() {
		recoverPreparedSupervisorForRecovery = previousRecover
		releasePreparedSupervisorForRecovery = previousRelease
	})
	recoverPreparedSupervisorForRecovery = func(got authority.SupervisorHandoff) (authority.StopReceipt, error) {
		recoverCalls++
		if !got.Equal(expected) {
			t.Fatalf("recover handoff = %#v, want %#v", got, expected)
		}
		return receipt, nil
	}
	releasePreparedSupervisorForRecovery = func(got authority.SupervisorHandoff) (authority.SupervisorHandoffReleaseProof, error) {
		if got.StopReceipt == nil || *got.StopReceipt != receipt {
			t.Fatalf("release handoff receipt = %#v, want %#v", got.StopReceipt, receipt)
		}
		return got.ReleaseProof(), nil
	}
	app := &daemon{store: store}
	if err := app.recoverPersistedSupervisorHandoff(key, expected); err != nil {
		t.Fatalf("recoverPersistedSupervisorHandoff() error = %v", err)
	}
	if recoverCalls != 1 {
		t.Fatalf("prepared supervisor recovery calls = %d, want 1", recoverCalls)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.ContainmentHandoff != nil || journal.HasProcessDetails() || journal.ContainmentAuthority != nil {
		t.Fatalf("released handoff journal = %#v, want no pending containment", journal)
	}
}

func TestRecoverPersistedSupervisorHandoffRetainsWorkspaceBeforeProcessControl(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	handoff := testRecoveryHandoffFixture(46, "windows:46:created-at")
	if _, err := store.PrepareSupervisorHandoff(key, handoff); err != nil {
		t.Fatalf("PrepareSupervisorHandoff() error = %v", err)
	}
	bound, err := store.BindSupervisorHandoff(key, handoff, 47, "windows:47:created-at")
	if err != nil {
		t.Fatalf("BindSupervisorHandoff() error = %v", err)
	}
	expected := *bound.ContainmentHandoff
	value, err := expected.ToSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	receipt := testRecoveryStopReceipt(value)
	events := make([]string, 0, 3)
	previousRecover := recoverPreparedSupervisorForRecovery
	previousRelease := releasePreparedSupervisorForRecovery
	t.Cleanup(func() {
		recoverPreparedSupervisorForRecovery = previousRecover
		releasePreparedSupervisorForRecovery = previousRelease
	})
	recoverPreparedSupervisorForRecovery = func(got authority.SupervisorHandoff) (authority.StopReceipt, error) {
		journal, loadErr := store.LoadJournal(key)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if !journal.RetainWorkspace {
			t.Fatal("prepared supervisor recovery ran before workspace retention became durable")
		}
		events = append(events, "recover")
		if !got.Equal(expected) {
			t.Fatalf("recover handoff = %#v, want %#v", got, expected)
		}
		return receipt, nil
	}
	releasePreparedSupervisorForRecovery = func(got authority.SupervisorHandoff) (authority.SupervisorHandoffReleaseProof, error) {
		journal, loadErr := store.LoadJournal(key)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if !journal.RetainWorkspace {
			t.Fatal("prepared supervisor release ran before workspace retention became durable")
		}
		events = append(events, "release")
		return got.ReleaseProof(), nil
	}
	app := &daemon{store: store, options: options{
		retainWorkspace: func(got state.RunKey) (state.RunJournal, error) {
			events = append(events, "retain")
			return store.RetainWorkspace(got)
		},
	}}
	if err := app.recoverPersistedSupervisorHandoff(key, expected); err != nil {
		t.Fatalf("recoverPersistedSupervisorHandoff() error = %v", err)
	}
	if got, want := strings.Join(events, ","), "retain,recover,release"; got != want {
		t.Fatalf("recovery event order = %q, want %q", got, want)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if !journal.RetainWorkspace {
		t.Fatalf("workspace retention was not durable after handoff recovery: %#v", journal)
	}
}

func TestRecoverPersistedSupervisorHandoffRetentionFailurePreservesPendingJournal(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	handoff := testRecoveryHandoffFixture(48, "windows:48:created-at")
	if _, err := store.PrepareSupervisorHandoff(key, handoff); err != nil {
		t.Fatalf("PrepareSupervisorHandoff() error = %v", err)
	}
	bound, err := store.BindSupervisorHandoff(key, handoff, 49, "windows:49:created-at")
	if err != nil {
		t.Fatalf("BindSupervisorHandoff() error = %v", err)
	}
	expected := *bound.ContainmentHandoff
	retentionErr := errors.New("workspace retention is unavailable")
	previousRecover := recoverPreparedSupervisorForRecovery
	previousRelease := releasePreparedSupervisorForRecovery
	t.Cleanup(func() {
		recoverPreparedSupervisorForRecovery = previousRecover
		releasePreparedSupervisorForRecovery = previousRelease
	})
	recoverPreparedSupervisorForRecovery = func(authority.SupervisorHandoff) (authority.StopReceipt, error) {
		t.Fatal("prepared supervisor recovery ran after workspace retention failed")
		return authority.StopReceipt{}, nil
	}
	releasePreparedSupervisorForRecovery = func(authority.SupervisorHandoff) (authority.SupervisorHandoffReleaseProof, error) {
		t.Fatal("prepared supervisor release ran after workspace retention failed")
		return authority.SupervisorHandoffReleaseProof{}, nil
	}
	app := &daemon{store: store, options: options{
		retainWorkspace: func(got state.RunKey) (state.RunJournal, error) {
			return state.RunJournal{}, retentionErr
		},
	}}
	err = app.recoverPersistedSupervisorHandoff(key, expected)
	if !errors.Is(err, retentionErr) {
		t.Fatalf("recoverPersistedSupervisorHandoff() error = %v, want %v", err, retentionErr)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.ContainmentHandoff == nil || !journal.ContainmentHandoff.Equal(expected) || journal.RetainWorkspace {
		t.Fatalf("retention failure changed pending handoff journal: %#v", journal)
	}
}

func TestRecoverPersistedSupervisorHandoffRetainsUnresolvedBoundLaunch(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	handoff := testRecoveryHandoffFixture(47, "windows:47:created-at")
	if _, err := store.PrepareSupervisorHandoff(key, handoff); err != nil {
		t.Fatalf("PrepareSupervisorHandoff() error = %v", err)
	}
	bound, err := store.BindSupervisorHandoff(key, handoff, 48, "windows:48:created-at")
	if err != nil {
		t.Fatalf("BindSupervisorHandoff() error = %v", err)
	}
	expected := *bound.ContainmentHandoff
	recoveryErr := errors.New("prepared supervisor stop remains unresolved")
	previousRecover := recoverPreparedSupervisorForRecovery
	previousRelease := releasePreparedSupervisorForRecovery
	t.Cleanup(func() {
		recoverPreparedSupervisorForRecovery = previousRecover
		releasePreparedSupervisorForRecovery = previousRelease
	})
	recoverPreparedSupervisorForRecovery = func(got authority.SupervisorHandoff) (authority.StopReceipt, error) {
		if !got.Equal(expected) {
			t.Fatalf("recover handoff = %#v, want %#v", got, expected)
		}
		return authority.StopReceipt{}, recoveryErr
	}
	releasePreparedSupervisorForRecovery = func(authority.SupervisorHandoff) (authority.SupervisorHandoffReleaseProof, error) {
		t.Fatal("release was called while stop remained unresolved")
		return authority.SupervisorHandoffReleaseProof{}, nil
	}
	app := &daemon{store: store}
	if err := app.recoverPersistedSupervisorHandoff(key, expected); !errors.Is(err, recoveryErr) {
		t.Fatalf("recoverPersistedSupervisorHandoff() error = %v, want %v", err, recoveryErr)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.ContainmentHandoff == nil || !journal.ContainmentHandoff.Equal(expected) {
		t.Fatalf("unresolved handoff journal = %#v, want exact pending handoff %#v", journal.ContainmentHandoff, expected)
	}
}

func TestRecoverPersistedSupervisorHandoffFallsBackToAbortProofAfterRecoverFailure(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	handoff := testRecoveryHandoffFixture(53, "windows:53:created-at")
	if _, err := store.PrepareSupervisorHandoff(key, handoff); err != nil {
		t.Fatalf("PrepareSupervisorHandoff() error = %v", err)
	}
	bound, err := store.BindSupervisorHandoff(key, handoff, 54, "windows:54:created-at")
	if err != nil {
		t.Fatalf("BindSupervisorHandoff() error = %v", err)
	}
	expected := *bound.ContainmentHandoff
	recoveryErr := errors.New("prepared supervisor recover response lost")
	previousRecover := recoverPreparedSupervisorForRecovery
	previousAbort := provePreparedSupervisorAbortedForRecovery
	previousRelease := releasePreparedSupervisorForRecovery
	t.Cleanup(func() {
		recoverPreparedSupervisorForRecovery = previousRecover
		provePreparedSupervisorAbortedForRecovery = previousAbort
		releasePreparedSupervisorForRecovery = previousRelease
	})
	recoverPreparedSupervisorForRecovery = func(got authority.SupervisorHandoff) (authority.StopReceipt, error) {
		if !got.Equal(expected) {
			t.Fatalf("recover handoff = %#v, want %#v", got, expected)
		}
		return authority.StopReceipt{}, recoveryErr
	}
	provePreparedSupervisorAbortedForRecovery = func(got authority.SupervisorHandoff) (authority.SupervisorHandoffAbortProof, error) {
		if !got.Equal(expected) {
			t.Fatalf("abort fallback handoff = %#v, want %#v", got, expected)
		}
		return testRecoverySupervisorHandoffAbortProof(got), nil
	}
	releasePreparedSupervisorForRecovery = func(authority.SupervisorHandoff) (authority.SupervisorHandoffReleaseProof, error) {
		t.Fatal("release was called after exact abort proof")
		return authority.SupervisorHandoffReleaseProof{}, nil
	}
	app := &daemon{store: store}
	if err := app.recoverPersistedSupervisorHandoff(key, expected); err != nil {
		t.Fatalf("recoverPersistedSupervisorHandoff() error = %v, want abort fallback success", err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.ContainmentHandoff != nil {
		t.Fatalf("abort fallback retained handoff: %#v", journal.ContainmentHandoff)
	}
}

func TestRecoverPersistedSupervisorHandoffRetainsBoundLaunchWhenAbortFallbackProofFails(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	handoff := testRecoveryHandoffFixture(55, "windows:55:created-at")
	if _, err := store.PrepareSupervisorHandoff(key, handoff); err != nil {
		t.Fatalf("PrepareSupervisorHandoff() error = %v", err)
	}
	bound, err := store.BindSupervisorHandoff(key, handoff, 56, "windows:56:created-at")
	if err != nil {
		t.Fatalf("BindSupervisorHandoff() error = %v", err)
	}
	expected := *bound.ContainmentHandoff
	recoveryErr := errors.New("prepared supervisor recover failed")
	abortErr := errors.New("prepared supervisor abort proof unavailable")
	previousRecover := recoverPreparedSupervisorForRecovery
	previousAbort := provePreparedSupervisorAbortedForRecovery
	t.Cleanup(func() {
		recoverPreparedSupervisorForRecovery = previousRecover
		provePreparedSupervisorAbortedForRecovery = previousAbort
	})
	recoverPreparedSupervisorForRecovery = func(authority.SupervisorHandoff) (authority.StopReceipt, error) {
		return authority.StopReceipt{}, recoveryErr
	}
	provePreparedSupervisorAbortedForRecovery = func(authority.SupervisorHandoff) (authority.SupervisorHandoffAbortProof, error) {
		return authority.SupervisorHandoffAbortProof{}, abortErr
	}
	app := &daemon{store: store}
	err = app.recoverPersistedSupervisorHandoff(key, expected)
	if !errors.Is(err, recoveryErr) || !errors.Is(err, abortErr) {
		t.Fatalf("recoverPersistedSupervisorHandoff() error = %v, want recover and abort errors", err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.ContainmentHandoff == nil || !journal.ContainmentHandoff.Equal(expected) {
		t.Fatalf("failed abort fallback changed handoff: %#v, want %#v", journal.ContainmentHandoff, expected)
	}
}

func TestRecoverPersistedSupervisorHandoffRewritesReceiptBeforeRelease(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	handoff := testRecoveryHandoffFixture(49, "windows:49:created-at")
	if _, err := store.PrepareSupervisorHandoff(key, handoff); err != nil {
		t.Fatalf("PrepareSupervisorHandoff() error = %v", err)
	}
	bound, err := store.BindSupervisorHandoff(key, handoff, 50, "windows:50:created-at")
	if err != nil {
		t.Fatalf("BindSupervisorHandoff() error = %v", err)
	}
	expected := *bound.ContainmentHandoff
	value, err := expected.ToSupervisor()
	if err != nil {
		t.Fatal(err)
	}
	receipt := testRecoveryStopReceipt(value)
	withReceipt, err := store.RecordSupervisorHandoffStopReceipt(key, expected, receipt)
	if err != nil {
		t.Fatalf("RecordSupervisorHandoffStopReceipt() error = %v", err)
	}
	expected = *withReceipt.ContainmentHandoff
	if _, err := store.RetainWorkspace(key); err != nil {
		t.Fatalf("RetainWorkspace() error = %v", err)
	}
	writes := 0
	restore := store.SetAtomicWriterForTesting(func(path string, data []byte) error {
		writes++
		return os.WriteFile(path, data, 0o600)
	})
	defer restore()
	previousRecover := recoverPreparedSupervisorForRecovery
	previousRelease := releasePreparedSupervisorForRecovery
	t.Cleanup(func() {
		recoverPreparedSupervisorForRecovery = previousRecover
		releasePreparedSupervisorForRecovery = previousRelease
	})
	recoverPreparedSupervisorForRecovery = func(authority.SupervisorHandoff) (authority.StopReceipt, error) {
		t.Fatal("recovery contacted the helper despite an existing stop receipt")
		return authority.StopReceipt{}, nil
	}
	releasePreparedSupervisorForRecovery = func(got authority.SupervisorHandoff) (authority.SupervisorHandoffReleaseProof, error) {
		if writes != 1 {
			t.Fatalf("journal writes before release = %d, want fresh receipt replay", writes)
		}
		if got.StopReceipt == nil || *got.StopReceipt != receipt {
			t.Fatalf("release receipt = %#v, want %#v", got.StopReceipt, receipt)
		}
		return got.ReleaseProof(), nil
	}
	app := &daemon{store: store}
	if err := app.recoverPersistedSupervisorHandoff(key, expected); err != nil {
		t.Fatalf("recoverPersistedSupervisorHandoff() error = %v", err)
	}
	if writes != 2 {
		t.Fatalf("journal writes = %d, want receipt replay plus clear", writes)
	}
}

func TestRecoverUnresolvedInputIntentsBlocksPendingSupervisorHandoff(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	handoff := testRecoveryHandoffFixture(51, "windows:51:created-at")
	if _, err := store.PrepareSupervisorHandoff(key, handoff); err != nil {
		t.Fatalf("PrepareSupervisorHandoff() error = %v", err)
	}
	recoveryErr := errors.New("prepared supervisor abort remains unresolved")
	previousAbort := provePreparedSupervisorAbortedForRecovery
	t.Cleanup(func() { provePreparedSupervisorAbortedForRecovery = previousAbort })
	provePreparedSupervisorAbortedForRecovery = func(authority.SupervisorHandoff) (authority.SupervisorHandoffAbortProof, error) {
		return authority.SupervisorHandoffAbortProof{}, recoveryErr
	}
	app := &daemon{store: store, running: make(map[state.RunKey]*runningRun)}
	if err := app.recoverUnresolvedInputIntents(context.Background()); !errors.Is(err, recoveryErr) {
		t.Fatalf("recoverUnresolvedInputIntents() error = %v, want %v", err, recoveryErr)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.ContainmentHandoff == nil || !journal.ContainmentHandoff.Equal(handoff) {
		t.Fatalf("pending handoff = %#v, want exact unresolved handoff %#v", journal.ContainmentHandoff, handoff)
	}
	if journal.TerminalState != "" || len(journal.PendingTransitions) != 0 || journal.InputCommandIntent != nil {
		t.Fatalf("unresolved handoff crossed restart terminal fallback: %#v", journal)
	}
}

func TestRecoverUnresolvedInputIntentsSkipsOutboxAfterSupervisorHandoffAbort(t *testing.T) {
	store, key := claimedStore(t)
	defer store.Close()
	handoff := testRecoveryHandoffFixture(52, "windows:52:created-at")
	if _, err := store.PrepareSupervisorHandoff(key, handoff); err != nil {
		t.Fatalf("PrepareSupervisorHandoff() error = %v", err)
	}
	previousAbort := provePreparedSupervisorAbortedForRecovery
	t.Cleanup(func() { provePreparedSupervisorAbortedForRecovery = previousAbort })
	provePreparedSupervisorAbortedForRecovery = func(got authority.SupervisorHandoff) (authority.SupervisorHandoffAbortProof, error) {
		return testRecoverySupervisorHandoffAbortProof(got), nil
	}
	app := &daemon{store: store, running: make(map[state.RunKey]*runningRun)}
	if err := app.recoverUnresolvedInputIntents(context.Background()); err != nil {
		t.Fatalf("recoverUnresolvedInputIntents() error = %v", err)
	}
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if journal.ContainmentHandoff != nil || journal.TerminalState != "" || len(journal.PendingTransitions) != 0 {
		t.Fatalf("successful handoff abort entered restart terminal fallback: %#v", journal)
	}
}

func TestRestartInputRecoveryRequiresPendingHandoffInTerminalStates(t *testing.T) {
	handoff := testRecoveryHandoffFixture(57, "windows:57:created-at")
	for _, localState := range []string{"terminal_pending", "stale", "cleanup_pending"} {
		localState := localState
		t.Run(localState, func(t *testing.T) {
			journal := state.RunJournal{LocalState: localState, ContainmentHandoff: &handoff}
			if !restartInputRecoveryRequired(journal) {
				t.Fatalf("restartInputRecoveryRequired(%q) = false, want true for pending handoff", localState)
			}
		})
	}
}

func testRecoverySupervisorHandoffAbortProof(value authority.SupervisorHandoff) authority.SupervisorHandoffAbortProof {
	var creatorSessionID *uint32
	if value.CreatorSessionID != nil {
		copy := *value.CreatorSessionID
		creatorSessionID = &copy
	}
	return authority.SupervisorHandoffAbortProof{
		Version:            authority.SupervisorHandoffAbortProofVersion,
		Disposition:        authority.SupervisorHandoffAbortDisposition,
		LaunchToken:        value.LaunchToken,
		JobID:              value.JobID,
		TargetPID:          value.TargetPID,
		TargetIdentity:     value.TargetIdentity,
		SupervisorPID:      value.SupervisorPID,
		SupervisorIdentity: value.SupervisorIdentity,
		CreatorSessionID:   creatorSessionID,
	}
}

func testRecoveryHandoffFixture(pid int, identity string) authority.SupervisorHandoff {
	sessionID := uint32(1)
	return authority.SupervisorHandoff{
		Version:          authority.SupervisorHandoffVersion,
		LaunchToken:      strings.Repeat("a", authority.TokenBytes*2),
		Secret:           strings.Repeat("b", authority.SecretBytes*2),
		TargetPID:        pid,
		TargetIdentity:   identity,
		PipeToken:        strings.Repeat("c", authority.TokenBytes*2),
		JobID:            strings.Repeat("d", authority.TokenBytes*2),
		CreatorSessionID: &sessionID,
	}
}
