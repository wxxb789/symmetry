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

func TestLoadIdentityMissingReturnsTypedNotFound(t *testing.T) {
	store := mustStore(t)

	_, err := store.LoadIdentity()
	if !IsNotFound(err) {
		t.Fatalf("LoadIdentity() error = %v, want typed not found", err)
	}
}

func TestIdentityRoundTripAndPermissions(t *testing.T) {
	store := mustStore(t)
	want := MachineIdentity{MachineID: "machine-1", MachineToken: "secret-token"}
	if err := store.SaveIdentity(want); err != nil {
		t.Fatalf("SaveIdentity() error = %v", err)
	}

	got, err := store.LoadIdentity()
	if err != nil {
		t.Fatalf("LoadIdentity() error = %v", err)
	}
	if got != want {
		t.Fatalf("LoadIdentity() = %#v, want %#v", got, want)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(store.identityPath())
		if err != nil {
			t.Fatalf("Stat(identity) error = %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("identity permissions = %o, want 600", got)
		}
		info, err = os.Stat(store.dir)
		if err != nil {
			t.Fatalf("Stat(state directory) error = %v", err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Errorf("state directory permissions = %o, want 700", got)
		}
	}
}

func TestStoreExclusiveLockAndClose(t *testing.T) {
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
	t.Run("directory", func(t *testing.T) {
		original := applyDirectorySecurity
		applyDirectorySecurity = func(string) error { return errors.New("security setup denied") }
		t.Cleanup(func() { applyDirectorySecurity = original })

		if _, err := New(t.TempDir()); err == nil {
			t.Fatal("New() succeeded when directory security setup failed")
		}
	})
	t.Run("file", func(t *testing.T) {
		store := mustStore(t)
		original := applyFileSecurity
		applyFileSecurity = func(string) error { return errors.New("security setup denied") }
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
}

func TestStatePathsHavePrivateSecurity(t *testing.T) {
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

func TestIdentityErrorsDoNotExposeToken(t *testing.T) {
	store := mustStore(t)
	const token = "never-report-this-token"
	err := store.SaveIdentity(MachineIdentity{MachineID: "machine-1", MachineToken: strings.Repeat(token, 20000)})
	if err == nil {
		t.Fatal("SaveIdentity() succeeded")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("SaveIdentity() error exposed token: %v", err)
	}
}

func TestIdentityAtomicReplace(t *testing.T) {
	store := mustStore(t)
	if err := store.SaveIdentity(MachineIdentity{MachineID: "old", MachineToken: "old-token"}); err != nil {
		t.Fatalf("save old identity: %v", err)
	}
	if err := store.SaveIdentity(MachineIdentity{MachineID: "new", MachineToken: "new-token"}); err != nil {
		t.Fatalf("save new identity: %v", err)
	}

	got, err := store.LoadIdentity()
	if err != nil {
		t.Fatalf("LoadIdentity() error = %v", err)
	}
	if got.MachineID != "new" || got.MachineToken != "new-token" {
		t.Fatalf("LoadIdentity() = %#v, want replacement", got)
	}
}

func TestIdentityRejectsCorruptTrailingAndOversizedFiles(t *testing.T) {
	store := mustStore(t)
	cases := []struct {
		name string
		body string
	}{
		{name: "corrupt", body: `{"machine_id":`},
		{name: "trailing", body: `{"machine_id":"machine-1","machine_token":"token"} {}`},
		{name: "unknown field", body: `{"machine_id":"machine-1","machine_token":"token","unexpected":true}`},
		{name: "oversized", body: strings.Repeat("x", maxStateFileBytes+1)},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := os.WriteFile(store.identityPath(), []byte(test.body), 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			if _, err := store.LoadIdentity(); err == nil {
				t.Fatal("LoadIdentity() succeeded")
			}
		})
	}
}

func TestNewDaemonInstanceIDIsVersion4AndUnique(t *testing.T) {
	seen := make(map[string]struct{})
	for range 100 {
		id, err := NewDaemonInstanceID()
		if err != nil {
			t.Fatalf("NewDaemonInstanceID() error = %v", err)
		}
		if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' || id[14] != '4' || !strings.ContainsRune("89ab", rune(id[19])) {
			t.Fatalf("NewDaemonInstanceID() = %q, want RFC 4122 v4 UUID", id)
		}
		if _, duplicate := seen[id]; duplicate {
			t.Fatalf("NewDaemonInstanceID() repeated %q", id)
		}
		seen[id] = struct{}{}
	}
}

func TestClaimIntentGrantAndPendingOutboxSurviveRestart(t *testing.T) {
	store := mustStore(t)
	key := RunKey{RunID: "run-1", Generation: 2}
	intent := ClaimIntent{
		Key: key, RuntimeKey: "local", RuntimeID: "runtime-1", RuntimeEpoch: 3,
		ClaimID: "claim-1", LocalState: "claiming", Work: protocol.Work{Goal: "implement", AgentProfile: "codex", Workspace: "isolated", Input: json.RawMessage(`{"branch":"main"}`)},
		WorkspacePath: `C:\work\run-1`, WorkspaceBindingKey: "binding-1",
	}
	if _, err := store.SaveClaimIntent(intent); err != nil {
		t.Fatalf("SaveClaimIntent() error = %v", err)
	}

	grant := protocol.ClaimResponse{RunID: key.RunID, Generation: key.Generation, ClaimID: intent.ClaimID, LeaseToken: "lease-1", LeaseExpiresAt: time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC), Work: intent.Work}
	if _, err := store.SaveClaimGrant(key, grant); err != nil {
		t.Fatalf("SaveClaimGrant() error = %v", err)
	}
	event := protocol.RunEvent{EventID: "event-1", Sequence: 1, Kind: "started", OccurredAt: time.Date(2026, 9, 3, 1, 2, 4, 0, time.UTC), Payload: json.RawMessage(`{"pid":42}`)}
	transition := protocol.StateTransitionRequest{TransitionID: "transition-1", State: "active", Payload: json.RawMessage(`{}`)}
	ack := protocol.CommandAcknowledgement{RunID: key.RunID, CommandID: "command-1", Outcome: "applied", AckID: "ack-1"}
	if _, err := store.QueueEvent(key, event); err != nil {
		t.Fatalf("QueueEvent() error = %v", err)
	}
	if _, err := store.QueueTransition(key, transition); err != nil {
		t.Fatalf("QueueTransition() error = %v", err)
	}
	if _, err := store.QueueCommandAcknowledgement(key, ack); err != nil {
		t.Fatalf("QueueCommandAcknowledgement() error = %v", err)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() before restart error = %v", err)
	}
	restarted, err := New(store.dir)
	if err != nil {
		t.Fatalf("New() after restart error = %v", err)
	}
	t.Cleanup(func() {
		if err := restarted.Close(); err != nil {
			t.Errorf("restarted Close() error = %v", err)
		}
	})
	got, err := restarted.LoadJournal(key)
	if err != nil {
		t.Fatalf("LoadJournal() error = %v", err)
	}
	if got.LeaseToken != "lease-1" || got.ClaimedRuntimeEpoch != 3 || got.ClaimID != "claim-1" || got.WorkspaceBindingKey != "binding-1" || len(got.PendingEvents) != 1 || len(got.PendingTransitions) != 1 || len(got.PendingCommandAcknowledgements) != 1 {
		t.Fatalf("journal after restart = %#v", got)
	}
}

func TestSaveClaimIntentRequiresBoundedWorkspaceBindingKey(t *testing.T) {
	store := mustStore(t)
	base := ClaimIntent{
		Key: RunKey{RunID: "run-1", Generation: 1}, RuntimeKey: "local", RuntimeID: "runtime-1", RuntimeEpoch: 3,
		ClaimID: "claim-1", Work: protocol.Work{Input: json.RawMessage(`{}`)}, WorkspacePath: `C:\work\run-1`,
	}
	for _, bindingKey := range []string{"", " ", strings.Repeat("x", 4097)} {
		intent := base
		intent.WorkspaceBindingKey = bindingKey
		if _, err := store.SaveClaimIntent(intent); err == nil {
			t.Fatalf("SaveClaimIntent(%q) succeeded", bindingKey)
		}
	}
	if _, err := store.LoadJournal(base.Key); !IsNotFound(err) {
		t.Fatalf("LoadJournal() error = %v, want no journal after rejected intent", err)
	}
}

func TestPendingOutboxRequiresPersistedClaimGrant(t *testing.T) {
	store := mustStore(t)
	key := RunKey{RunID: "run-1", Generation: 2}
	if _, err := store.SaveClaimIntent(ClaimIntent{Key: key, RuntimeKey: "local", RuntimeID: "runtime-1", RuntimeEpoch: 3, ClaimID: "claim-1", Work: protocol.Work{Input: json.RawMessage(`{}`)}, WorkspaceBindingKey: "binding-1"}); err != nil {
		t.Fatalf("SaveClaimIntent() error = %v", err)
	}
	_, err := store.QueueEvent(key, protocol.RunEvent{EventID: "event-1", Sequence: 1, Kind: "started", OccurredAt: time.Date(2026, 9, 3, 1, 2, 4, 0, time.UTC), Payload: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("QueueEvent() succeeded before claim grant")
	}
}

func TestSetProcessDetailsPersistsNonEmptyIdentity(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
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

func TestQueueTerminalTransitionIsAtomic(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	key := journal.Key()
	queued, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{TransitionID: "terminal-1", State: "completed", Payload: json.RawMessage(`{"result":"ok"}`)})
	if err != nil {
		t.Fatalf("QueueTerminalTransition() error = %v", err)
	}
	if queued.LocalState != "terminal_pending" || len(queued.PendingTransitions) != 1 || !sameFence(queued.PendingTransitions[0].Fence, queued.Fence()) {
		t.Fatalf("QueueTerminalTransition() = %#v", queued)
	}

	if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{State: "failed", Payload: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("QueueTerminalTransition() accepted an invalid transition")
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatalf("LoadJournal() error = %v", err)
	}
	if loaded.LocalState != "terminal_pending" || len(loaded.PendingTransitions) != 1 || loaded.PendingTransitions[0].TransitionID != "terminal-1" {
		t.Fatalf("invalid terminal transition partially updated journal: %#v", loaded)
	}
}

func TestQueueTerminalTransitionRejectsNonterminalStatesWithoutMutation(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	key := journal.Key()
	for _, state := range []string{"running", "waiting_for_input"} {
		if _, err := store.QueueTerminalTransition(key, protocol.StateTransitionRequest{TransitionID: "transition-" + state, State: state, Payload: json.RawMessage(`{}`)}); err == nil {
			t.Fatalf("QueueTerminalTransition(%q) succeeded", state)
		}
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatalf("LoadJournal() error = %v", err)
	}
	if loaded.LocalState != journal.LocalState || len(loaded.PendingTransitions) != 0 {
		t.Fatalf("nonterminal transition mutated journal: %#v", loaded)
	}
}

func TestQueueTerminalTransitionSelectsOneAuthoritativeTerminal(t *testing.T) {
	tests := []struct {
		name          string
		pendingStates []string
		incomingState string
		wantStates    []string
	}{
		{
			name:          "completed is replaced by cancelled",
			pendingStates: []string{"completed"},
			incomingState: "cancelled",
			wantStates:    []string{"cancelled"},
		},
		{
			name:          "cancelled keeps first terminal over completed",
			pendingStates: []string{"cancelled"},
			incomingState: "completed",
			wantStates:    []string{"cancelled"},
		},
		{
			name:          "completed keeps first terminal over failed",
			pendingStates: []string{"completed"},
			incomingState: "failed",
			wantStates:    []string{"completed"},
		},
		{
			name:          "cancelled replaces running and completed",
			pendingStates: []string{"running", "completed"},
			incomingState: "cancelled",
			wantStates:    []string{"cancelled"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := mustStore(t)
			journal := testJournal("run-1", 1)
			journal.PendingTransitions = transitionsForStates(journal, test.pendingStates)
			if hasTerminalState(journal.PendingTransitions) {
				journal.LocalState = "terminal_pending"
			}
			if err := store.SaveJournal(journal); err != nil {
				t.Fatalf("SaveJournal() error = %v", err)
			}

			queued, err := store.QueueTerminalTransition(journal.Key(), protocol.StateTransitionRequest{TransitionID: "incoming", State: test.incomingState, Payload: json.RawMessage(`{}`)})
			if err != nil {
				t.Fatalf("QueueTerminalTransition() error = %v", err)
			}
			if queued.LocalState != "terminal_pending" {
				t.Fatalf("LocalState = %q, want terminal_pending", queued.LocalState)
			}
			if got := transitionStates(queued.PendingTransitions); !equalStrings(got, test.wantStates) {
				t.Fatalf("pending states = %#v, want %#v", got, test.wantStates)
			}
		})
	}
}

func TestQueueTerminalTransitionValidatesFenceBeforeCancelledReplacement(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	journal.PendingTransitions = transitionsForStates(journal, []string{"completed"})
	journal.LocalState = "terminal_pending"
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}

	badFence := journal.Fence()
	badFence.ClaimID = "other-claim"
	_, err := store.QueueTerminalTransition(journal.Key(), protocol.StateTransitionRequest{Fence: badFence, TransitionID: "cancel", State: "cancelled", Payload: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("QueueTerminalTransition() accepted a mismatched fence")
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatalf("LoadJournal() error = %v", err)
	}
	if got := transitionStates(loaded.PendingTransitions); !equalStrings(got, []string{"completed"}) {
		t.Fatalf("mismatched fence mutated transitions: %#v", got)
	}
}

func TestValidateJournalRequiresConsistentProcessTuple(t *testing.T) {
	store := mustStore(t)
	base := testJournal("run-1", 1)
	cases := []struct {
		name   string
		mutate func(*RunJournal)
	}{
		{
			name: "zero PID with start time",
			mutate: func(journal *RunJournal) {
				journal.PID = 0
			},
		},
		{
			name: "zero PID with identity",
			mutate: func(journal *RunJournal) {
				journal.PID = 0
				journal.StartedAt = time.Time{}
				journal.ProcessIdentity = "identity"
			},
		},
		{
			name: "PID without start time",
			mutate: func(journal *RunJournal) {
				journal.StartedAt = time.Time{}
			},
		},
		{
			name: "PID without identity",
			mutate: func(journal *RunJournal) {
				journal.ProcessIdentity = ""
			},
		},
		{
			name: "whitespace identity",
			mutate: func(journal *RunJournal) {
				journal.ProcessIdentity = " "
			},
		},
		{
			name: "missing workspace binding key",
			mutate: func(journal *RunJournal) {
				journal.WorkspaceBindingKey = ""
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			journal := base
			test.mutate(&journal)
			if err := store.SaveJournal(journal); err == nil {
				t.Fatal("SaveJournal() succeeded")
			}
		})
	}
	if err := store.SaveJournal(base); err != nil {
		t.Fatalf("SaveJournal() rejected valid process tuple: %v", err)
	}
}

func TestListJournalsIncludesMultipleGenerationsAndCleansOnlyOwnedTemps(t *testing.T) {
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
	store := mustStore(t)
	first := testJournal("run-1", 1)
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
			copy.PID = 100 + index
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
}

func mustStore(t *testing.T) *Store {
	t.Helper()
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close() error = %v", err)
		}
	})
	return store
}

func testJournal(runID string, generation int64) RunJournal {
	return RunJournal{
		RunID: runID, Generation: generation, RuntimeKey: "local", RuntimeID: "runtime-1", ClaimedRuntimeEpoch: 3,
		ClaimID: "claim-1", LeaseToken: "lease-1", LeaseExpiresAt: time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC),
		LocalState: "running", Work: protocol.Work{Goal: "implement", AgentProfile: "codex", Workspace: "isolated", Input: json.RawMessage(`{}`)},
		WorkspacePath: `C:\work\run-1`, WorkspaceBindingKey: "binding-1", PID: 42, ProcessIdentity: "windows:42:created-at", StartedAt: time.Date(2026, 9, 3, 1, 2, 4, 0, time.UTC), LastEventSequence: 1,
	}
}

func transitionsForStates(journal RunJournal, states []string) []protocol.StateTransitionRequest {
	transitions := make([]protocol.StateTransitionRequest, 0, len(states))
	for index, state := range states {
		transitions = append(transitions, protocol.StateTransitionRequest{
			Fence:        journal.Fence(),
			TransitionID: "pending-" + strconv.Itoa(index),
			State:        state,
			Payload:      json.RawMessage(`{}`),
		})
	}
	return transitions
}

func transitionStates(transitions []protocol.StateTransitionRequest) []string {
	states := make([]string, 0, len(transitions))
	for _, transition := range transitions {
		states = append(states, transition.State)
	}
	return states
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func hasTerminalState(transitions []protocol.StateTransitionRequest) bool {
	for _, transition := range transitions {
		switch transition.State {
		case "completed", "failed", "cancelled":
			return true
		}
	}
	return false
}
