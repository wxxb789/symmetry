package state

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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

func TestEnrollmentIntentRoundTripAndDelete(t *testing.T) {
	store := mustStore(t)
	want := EnrollmentIntent{MachineName: "builder", MachineToken: "machine-token", IdempotencyKey: "enrollment-key"}
	if err := store.SaveEnrollmentIntent(want); err != nil {
		t.Fatalf("SaveEnrollmentIntent() error = %v", err)
	}
	got, err := store.LoadEnrollmentIntent()
	if err != nil {
		t.Fatalf("LoadEnrollmentIntent() error = %v", err)
	}
	if got != want {
		t.Fatalf("LoadEnrollmentIntent() = %#v, want %#v", got, want)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(store.enrollmentPath())
		if err != nil {
			t.Fatalf("Stat(enrollment intent) error = %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("enrollment intent permissions = %o, want 600", got)
		}
	}
	if err := store.DeleteEnrollmentIntent(); err != nil {
		t.Fatalf("DeleteEnrollmentIntent() error = %v", err)
	}
	if _, err := store.LoadEnrollmentIntent(); !IsNotFound(err) {
		t.Fatalf("LoadEnrollmentIntent() error = %v, want not found", err)
	}
}

func TestNewMachineTokenIsOpaqueAndUnique(t *testing.T) {
	seen := make(map[string]struct{})
	for range 100 {
		token, err := NewMachineToken()
		if err != nil {
			t.Fatalf("NewMachineToken() error = %v", err)
		}
		decoded, err := base64.RawURLEncoding.DecodeString(token)
		if err != nil || len(decoded) != 32 {
			t.Fatalf("NewMachineToken() = %q, decoded bytes = %d, error = %v", token, len(decoded), err)
		}
		if _, duplicate := seen[token]; duplicate {
			t.Fatalf("NewMachineToken() repeated %q", token)
		}
		seen[token] = struct{}{}
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

	grant := protocol.ClaimResponse{
		RunID: key.RunID, Generation: key.Generation, ClaimID: intent.ClaimID,
		LeaseToken: "lease-1", LeaseExpiresAt: time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC), Work: intent.Work,
		ProviderAccess: &protocol.ProviderAccess{
			Path: "/api/v1/provider-actions", Token: "provider-token",
			Grants: []protocol.ProviderGrant{{ResourceID: "resource-1", Provider: "github", Kind: "repository", Operations: []string{"resource.sync"}}},
		},
	}
	if _, err := store.SaveClaimGrant(key, grant); err != nil {
		t.Fatalf("SaveClaimGrant() error = %v", err)
	}
	journalContents, err := os.ReadFile(store.journalPath(key))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if bytes.Contains(journalContents, []byte("provider-token")) || bytes.Contains(journalContents, []byte("provider_access")) {
		t.Fatalf("provider access leaked into run journal: %s", journalContents)
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

func TestQueueCommandAcknowledgementIsIdempotentByCommandAndOutcome(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key := journal.Key()
	first := protocol.CommandAcknowledgement{CommandID: "command-1", Outcome: "applied", AckID: "ack-1"}
	if _, err := store.QueueCommandAcknowledgement(key, first); err != nil {
		t.Fatalf("QueueCommandAcknowledgement(first) error = %v", err)
	}
	if _, err := store.QueueCommandAcknowledgement(key, protocol.CommandAcknowledgement{CommandID: "command-1", Outcome: "applied", AckID: "ack-2"}); err != nil {
		t.Fatalf("QueueCommandAcknowledgement(retry) error = %v", err)
	}
	if _, err := store.QueueCommandAcknowledgement(key, protocol.CommandAcknowledgement{CommandID: "command-1", Outcome: "rejected", AckID: "ack-3"}); err == nil {
		t.Fatal("QueueCommandAcknowledgement() accepted a conflicting outcome")
	}
	loaded, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.PendingCommandAcknowledgements) != 1 || loaded.PendingCommandAcknowledgements[0].AckID != "ack-1" || loaded.PendingCommandAcknowledgements[0].Outcome != "applied" {
		t.Fatalf("pending acknowledgements = %#v", loaded.PendingCommandAcknowledgements)
	}
}

func TestCommandAcknowledgementRetiredUsesConclusiveTerminalVerdict(t *testing.T) {
	tests := []struct {
		name    string
		journal RunJournal
		retired bool
	}{
		{name: "active run", journal: RunJournal{LocalState: "running"}},
		{name: "accepted terminal", journal: RunJournal{LocalState: "terminal_pending", TerminalVerdict: TerminalVerdictAccepted}},
		{name: "ownership lost", journal: RunJournal{LocalState: "terminal_pending", TerminalVerdict: TerminalVerdictOwnershipLost}, retired: true},
		{name: "terminal grace expired", journal: RunJournal{LocalState: "terminal_pending", TerminalVerdict: TerminalVerdictGraceExpired}, retired: true},
		{name: "cleanup pending", journal: RunJournal{LocalState: "cleanup_pending"}, retired: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := CommandAcknowledgementRetired(test.journal); got != test.retired {
				t.Fatalf("CommandAcknowledgementRetired(%#v) = %t, want %t", test.journal, got, test.retired)
			}
		})
	}
	if !IsConclusiveTerminalVerdict(TerminalVerdictOwnershipLost) || !IsConclusiveTerminalVerdict(TerminalVerdictGraceExpired) || IsConclusiveTerminalVerdict(TerminalVerdictAccepted) {
		t.Fatal("IsConclusiveTerminalVerdict() did not classify terminal verdicts")
	}
}

func TestProvideInputIntentIsAtomicAcrossCompletionDeliveryAndEpisodes(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-input", 1)
	journal.LocalState = "waiting_for_input"
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	first := InputCommandIntent{CommandID: "command-1", PayloadDigest: strings.Repeat("a", sha256.Size*2), RunningTransitionID: "running-1", AckID: "ack-1"}
	prepared, created, err := store.PrepareProvideInput(journal.Key(), first)
	if err != nil || !created || prepared.InputCommandIntent == nil || prepared.InputCommandIntent.CommandID != first.CommandID {
		t.Fatalf("PrepareProvideInput(first) = %#v, %t, %v", prepared, created, err)
	}
	_, created, err = store.PrepareProvideInput(journal.Key(), first)
	if err != nil || created {
		t.Fatalf("PrepareProvideInput(retry) created=%t error=%v", created, err)
	}
	completed, err := store.CompleteProvideInput(journal.Key(), first.CommandID, first.PayloadDigest, "applied")
	if err != nil || completed.LocalState != "running" || len(completed.PendingTransitions) != 1 || completed.PendingTransitions[0].TransitionID != first.RunningTransitionID || len(completed.PendingCommandAcknowledgements) != 1 || completed.PendingCommandAcknowledgements[0].AckID != first.AckID {
		t.Fatalf("CompleteProvideInput() = %#v, %v", completed, err)
	}
	completed, err = store.CompleteProvideInput(journal.Key(), first.CommandID, first.PayloadDigest, "applied")
	if err != nil || len(completed.PendingTransitions) != 1 || len(completed.PendingCommandAcknowledgements) != 1 {
		t.Fatalf("CompleteProvideInput(retry) = %#v, %v", completed, err)
	}
	if _, err := store.MarkCommandAcknowledgementsDelivered(journal.Key(), []string{first.AckID}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SetLocalState(journal.Key(), "waiting_for_input"); err != nil {
		t.Fatal(err)
	}
	second := InputCommandIntent{CommandID: "command-2", PayloadDigest: strings.Repeat("b", sha256.Size*2), RunningTransitionID: "running-2", AckID: "ack-2"}
	replaced, created, err := store.PrepareProvideInput(journal.Key(), second)
	if err != nil || !created || replaced.InputCommandIntent == nil || replaced.InputCommandIntent.CommandID != second.CommandID {
		t.Fatalf("PrepareProvideInput(next episode) = %#v, %t, %v", replaced, created, err)
	}
}

func TestPrepareProvideInputCapturesEventSequenceBarrierAndValidatesIt(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	journal := testJournal("run-input-barrier", 1)
	journal.LocalState = "waiting_for_input"
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueEvent(journal.Key(), protocol.RunEvent{EventID: "event-2", Sequence: 2, Kind: "progress", OccurredAt: time.Date(2026, 9, 3, 1, 2, 5, 0, time.UTC), Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	prepared, created, err := store.PrepareProvideInput(journal.Key(), InputCommandIntent{
		CommandID:            "command-1",
		PayloadDigest:        strings.Repeat("e", sha256.Size*2),
		RunningTransitionID:  "running-1",
		AckID:                "ack-1",
		EventSequenceBarrier: 99,
	})
	if err != nil || !created || prepared.InputCommandIntent == nil || prepared.InputCommandIntent.EventSequenceBarrier != 2 {
		t.Fatalf("PrepareProvideInput() = %#v, %t, %v", prepared, created, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	loaded, err := restarted.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.InputCommandIntent == nil || loaded.InputCommandIntent.EventSequenceBarrier != 2 {
		t.Fatalf("input barrier after restart = %#v", loaded.InputCommandIntent)
	}
	invalid := loaded
	invalid.InputCommandIntent.EventSequenceBarrier = loaded.LastEventSequence + 1
	if err := restarted.SaveJournal(invalid); err == nil {
		t.Fatal("SaveJournal() accepted an input barrier after the last event")
	}
}

func TestTerminalTransitionSettlesUnresolvedInputBeforeCleanup(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-input-terminal", 1)
	journal.LocalState = "waiting_for_input"
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	intent := InputCommandIntent{CommandID: "command-1", PayloadDigest: strings.Repeat("c", sha256.Size*2), RunningTransitionID: "running-1", AckID: "ack-1"}
	if _, _, err := store.PrepareProvideInput(journal.Key(), intent); err != nil {
		t.Fatal(err)
	}
	pendingAt := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	terminal, err := store.QueueTerminalTransitionAt(journal.Key(), protocol.StateTransitionRequest{TransitionID: "failed-1", State: "failed", Payload: json.RawMessage(`{}`)}, pendingAt)
	if err != nil || terminal.InputCommandIntent == nil || terminal.InputCommandIntent.Outcome != "failed" || len(terminal.PendingCommandAcknowledgements) != 1 || terminal.PendingCommandAcknowledgements[0].AckID != intent.AckID {
		t.Fatalf("QueueTerminalTransitionAt() = %#v, %v", terminal, err)
	}
	if _, err := store.ResolveTerminal(journal.Key(), TerminalVerdictAccepted, pendingAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkTransitionsDelivered(journal.Key(), []string{"failed-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnterCleanupPending(journal.Key()); err == nil {
		t.Fatal("EnterCleanupPending() accepted an undelivered input acknowledgement")
	}
	if _, err := store.MarkCommandAcknowledgementsDelivered(journal.Key(), []string{intent.AckID}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnterCleanupPending(journal.Key()); err != nil {
		t.Fatal(err)
	}
}

func TestResolveTerminalForCleanupRetiresUndeliveredInputIntent(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-input-cleanup", 1)
	journal.LocalState = "waiting_for_input"
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	intent := InputCommandIntent{CommandID: "command-1", PayloadDigest: strings.Repeat("d", sha256.Size*2), RunningTransitionID: "running-1", AckID: "ack-1"}
	if _, _, err := store.PrepareProvideInput(journal.Key(), intent); err != nil {
		t.Fatal(err)
	}
	pendingAt := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	if _, err := store.QueueTerminalTransitionAt(journal.Key(), protocol.StateTransitionRequest{TransitionID: "failed-1", State: "failed", Payload: json.RawMessage(`{}`)}, pendingAt); err != nil {
		t.Fatal(err)
	}
	updated, err := store.ResolveTerminalForCleanup(journal.Key(), TerminalVerdictOwnershipLost, pendingAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if updated.LocalState != "cleanup_pending" || updated.InputCommandIntent != nil || len(updated.PendingCommandAcknowledgements) != 0 {
		t.Fatalf("ResolveTerminalForCleanup() = %#v", updated)
	}
}

func TestJournalRejectsDuplicatePendingCommandAcknowledgementCommandIDs(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	journal := testJournal("run-1", 1)
	duplicate := journal
	duplicate.PendingCommandAcknowledgements = []protocol.CommandAcknowledgement{
		{Fence: journal.Fence(), RunID: journal.RunID, CommandID: "command-1", Outcome: "applied", AckID: "ack-1"},
		{Fence: journal.Fence(), RunID: journal.RunID, CommandID: "command-1", Outcome: "rejected", AckID: "ack-2"},
	}
	if err := store.SaveJournal(duplicate); err == nil {
		t.Fatal("SaveJournal() accepted duplicate command acknowledgement IDs")
	}
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(duplicate)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.journalPath(journal.Key()), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if _, err := restarted.LoadJournal(journal.Key()); err == nil {
		t.Fatal("LoadJournal() accepted duplicate command acknowledgement IDs after restart")
	}
}

func TestJournalFileLimitSupportsLargeBacklogAndRejectsOversizeFiles(t *testing.T) {
	t.Run("round trips a backlog larger than the control response limit", func(t *testing.T) {
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
	if queued.LocalState != "terminal_pending" || queued.TerminalPendingAt.IsZero() || queued.TerminalState != "completed" || len(queued.PendingTransitions) != 1 || !sameFence(queued.PendingTransitions[0].Fence, queued.Fence()) {
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

func TestEnterCleanupPendingPreservesTerminalAuditAndControlsDelivery(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key := journal.Key()
	pendingAt := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}, pendingAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveTerminal(key, TerminalVerdictAccepted, pendingAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnterCleanupPending(key); err == nil {
		t.Fatal("EnterCleanupPending() accepted an undelivered terminal transition")
	}
	if _, err := store.MarkTransitionsDelivered(key, []string{"completed-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueCommandAcknowledgement(key, protocol.CommandAcknowledgement{CommandID: "command-pending", Outcome: "applied", AckID: "ack-pending"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnterCleanupPending(key); err == nil {
		t.Fatal("EnterCleanupPending() accepted a pending command acknowledgement")
	}
	if _, err := store.MarkCommandAcknowledgementsDelivered(key, []string{"ack-pending"}); err != nil {
		t.Fatal(err)
	}
	entered, err := store.EnterCleanupPending(key)
	if err != nil {
		t.Fatal(err)
	}
	if entered.LocalState != "cleanup_pending" || entered.TerminalVerdict != TerminalVerdictAccepted || !entered.TerminalPendingAt.Equal(pendingAt) {
		t.Fatalf("cleanup-pending journal = %#v", entered)
	}
	if _, err := store.EnterCleanupPending(key); err != nil {
		t.Fatalf("EnterCleanupPending() was not idempotent: %v", err)
	}

	journal = testJournal("run-2", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key = journal.Key()
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "failed-1", State: "failed", Payload: json.RawMessage(`{}`)}, pendingAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueCommandAcknowledgement(key, protocol.CommandAcknowledgement{CommandID: "command-1", Outcome: "applied", AckID: "ack-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveTerminal(key, TerminalVerdictOwnershipLost, pendingAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	entered, err = store.EnterCleanupPending(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(entered.PendingTransitions) != 0 || len(entered.PendingCommandAcknowledgements) != 0 || entered.TerminalVerdict != TerminalVerdictOwnershipLost {
		t.Fatalf("conclusive rejection retained delivery state: %#v", entered)
	}
}

func TestCleanupPendingRoundTripsAcrossRestart(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	journal := testJournal("run-1", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	pendingAt := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	if _, err := store.QueueTerminalTransitionAt(journal.Key(), protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}, pendingAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ResolveTerminal(journal.Key(), TerminalVerdictAccepted, pendingAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkTransitionsDelivered(journal.Key(), []string{"completed-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnterCleanupPending(journal.Key()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	loaded, err := restarted.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LocalState != "cleanup_pending" || loaded.TerminalVerdict != TerminalVerdictAccepted || !loaded.TerminalPendingAt.Equal(pendingAt) {
		t.Fatalf("cleanup-pending journal after restart = %#v", loaded)
	}
}

func TestResolveTerminalForCleanupIsAtomicAndIdempotent(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-atomic-cleanup", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key := journal.Key()
	pendingAt := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "failed-1", State: "failed", Payload: json.RawMessage(`{}`)}, pendingAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueCommandAcknowledgement(key, protocol.CommandAcknowledgement{CommandID: "command-1", Outcome: "applied", AckID: "ack-1"}); err != nil {
		t.Fatal(err)
	}
	resolvedAt := pendingAt.Add(time.Second)
	updated, err := store.ResolveTerminalForCleanup(key, TerminalVerdictOwnershipLost, resolvedAt)
	if err != nil {
		t.Fatal(err)
	}
	if updated.LocalState != "cleanup_pending" || updated.TerminalVerdict != TerminalVerdictOwnershipLost || !updated.TerminalResolvedAt.Equal(resolvedAt) || len(updated.PendingTransitions) != 0 || len(updated.PendingCommandAcknowledgements) != 0 {
		t.Fatalf("atomic cleanup resolution = %#v", updated)
	}
	if _, err := store.ResolveTerminalForCleanup(key, TerminalVerdictOwnershipLost, resolvedAt.Add(time.Minute)); err != nil {
		t.Fatalf("same verdict was not idempotent: %v", err)
	}
	if _, err := store.ResolveTerminalForCleanup(key, TerminalVerdictGraceExpired, resolvedAt); err == nil {
		t.Fatal("conflicting verdict was accepted")
	}
}

func TestWorkspacePathMutationPreservesTerminalOutboxAndRecoveryIntent(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-workspace-path", 1)
	journal.WorkspacePath = ""
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key := journal.Key()
	if _, err := store.MarkWorkspaceRecoveryRequired(key); err != nil {
		t.Fatal(err)
	}
	pendingAt := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "cancelled-1", State: "cancelled", Payload: json.RawMessage(`{}`)}, pendingAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueCommandAcknowledgement(key, protocol.CommandAcknowledgement{CommandID: "command-1", Outcome: "applied", AckID: "ack-1"}); err != nil {
		t.Fatal(err)
	}
	updated, err := store.SetWorkspacePath(key, `C:\work\run-workspace-path`)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.WorkspaceRecoveryRequired || updated.WorkspacePath != `C:\work\run-workspace-path` || updated.LocalState != "terminal_pending" || len(updated.PendingTransitions) != 1 || updated.PendingTransitions[0].State != "cancelled" || len(updated.PendingCommandAcknowledgements) != 1 {
		t.Fatalf("workspace path mutation lost terminal state: %#v", updated)
	}
}

func TestAdvanceLeaseExpiryIsMonotonic(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	later := time.Date(2026, 9, 3, 2, 0, 0, 0, time.UTC)
	earlier := later.Add(-time.Minute)

	advanced, err := store.AdvanceLeaseExpiry(journal.Key(), later)
	if err != nil {
		t.Fatal(err)
	}
	if !advanced.LeaseExpiresAt.Equal(later) {
		t.Fatalf("advanced expiry = %s, want %s", advanced.LeaseExpiresAt, later)
	}

	unchanged, err := store.AdvanceLeaseExpiry(journal.Key(), earlier)
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged.LeaseExpiresAt.Equal(later) {
		t.Fatalf("regressed expiry = %s, want %s", unchanged.LeaseExpiresAt, later)
	}

	terminalAt := later.Add(-30 * time.Second)
	terminal, err := store.QueueTerminalTransitionAt(journal.Key(), protocol.StateTransitionRequest{TransitionID: "terminal-1", State: "completed", Payload: json.RawMessage(`{}`)}, terminalAt)
	if err != nil {
		t.Fatal(err)
	}
	afterTerminal, err := store.AdvanceLeaseExpiry(journal.Key(), later.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if !afterTerminal.LeaseExpiresAt.Equal(terminal.LeaseExpiresAt) || !afterTerminal.TerminalPendingAt.Equal(terminalAt) {
		t.Fatalf("terminal lease advanced: before=%#v after=%#v", terminal, afterTerminal)
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
		if _, err := store.QueueTerminalTransitionAt(key, protocol.StateTransitionRequest{TransitionID: "transition-" + state, State: state, Payload: json.RawMessage(`{}`)}, time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)); err == nil {
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
				journal.TerminalPendingAt = time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
				journal.TerminalState = pendingTerminalState(journal.PendingTransitions)
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
	journal.TerminalPendingAt = time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	journal.TerminalState = "completed"
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

func TestQueueCancelledTransitionAndAcknowledgementIsAtomic(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	pendingAt := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	transition := protocol.StateTransitionRequest{TransitionID: "cancel-1", State: "cancelled", Payload: json.RawMessage(`{}`)}
	if _, err := store.QueueCancelledTransitionAndAcknowledgementAt(journal.Key(), transition, protocol.CommandAcknowledgement{RunID: journal.RunID, CommandID: "command-1", Outcome: "applied"}, pendingAt); err == nil {
		t.Fatal("atomic cancel queue accepted an invalid acknowledgement")
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LocalState == "terminal_pending" || len(loaded.PendingTransitions) != 0 || len(loaded.PendingCommandAcknowledgements) != 0 {
		t.Fatalf("failed atomic cancel mutated journal: %#v", loaded)
	}

	queued, err := store.QueueCancelledTransitionAndAcknowledgementAt(journal.Key(), transition, protocol.CommandAcknowledgement{RunID: journal.RunID, CommandID: "command-1", Outcome: "applied", AckID: "ack-1"}, pendingAt)
	if err != nil {
		t.Fatal(err)
	}
	if queued.LocalState != "terminal_pending" || queued.TerminalState != "cancelled" || len(queued.PendingTransitions) != 1 || len(queued.PendingCommandAcknowledgements) != 1 {
		t.Fatalf("atomic cancel queue = %#v", queued)
	}
}

func TestQueueWaitingForInputCoalescesEventsAndPendingTransition(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	journal.LastEventSequence = 0
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}

	firstEvent := protocol.RunEvent{EventID: "event-1", Kind: "waiting_for_input", OccurredAt: time.Date(2026, 9, 3, 1, 2, 5, 0, time.UTC), Payload: json.RawMessage(`{"type":"waiting_for_input","question":"first"}`)}
	firstTransition := protocol.StateTransitionRequest{TransitionID: "transition-1", State: "waiting_for_input", Payload: firstEvent.Payload}
	queued, err := store.QueueWaitingForInput(journal.Key(), firstEvent, firstTransition)
	if err != nil {
		t.Fatalf("QueueWaitingForInput(first) error = %v", err)
	}
	secondEvent := protocol.RunEvent{EventID: "event-2", Kind: "waiting_for_input", OccurredAt: time.Date(2026, 9, 3, 1, 2, 6, 0, time.UTC), Payload: json.RawMessage(`{"type":"waiting_for_input","question":"second"}`)}
	secondTransition := protocol.StateTransitionRequest{TransitionID: "transition-2", State: "waiting_for_input", Payload: secondEvent.Payload}
	queued, err = store.QueueWaitingForInput(journal.Key(), secondEvent, secondTransition)
	if err != nil {
		t.Fatalf("QueueWaitingForInput(second) error = %v", err)
	}

	if queued.LocalState != "waiting_for_input" || queued.LastEventSequence != 2 || len(queued.PendingEvents) != 2 || len(queued.PendingTransitions) != 1 {
		t.Fatalf("QueueWaitingForInput() = %#v", queued)
	}
	transition := queued.PendingTransitions[0]
	if transition.TransitionID != firstTransition.TransitionID || !sameFence(transition.Fence, queued.Fence()) || string(transition.Payload) != string(secondEvent.Payload) {
		t.Fatalf("pending waiting transition = %#v", transition)
	}

	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	restarted, err := New(store.dir)
	if err != nil {
		t.Fatalf("New() after restart error = %v", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	loaded, err := restarted.LoadJournal(journal.Key())
	if err != nil {
		t.Fatalf("LoadJournal() after restart error = %v", err)
	}
	if loaded.LocalState != "waiting_for_input" || loaded.LastEventSequence != 2 || len(loaded.PendingEvents) != 2 || len(loaded.PendingTransitions) != 1 || loaded.PendingTransitions[0].TransitionID != firstTransition.TransitionID || string(loaded.PendingTransitions[0].Payload) != string(secondEvent.Payload) {
		t.Fatalf("journal after restart = %#v", loaded)
	}
}

func TestQueueWaitingForInputDoesNotRecreateDeliveredTransition(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	journal.LastEventSequence = 0
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	firstEvent := protocol.RunEvent{EventID: "event-1", Kind: "waiting_for_input", OccurredAt: time.Date(2026, 9, 3, 1, 2, 5, 0, time.UTC), Payload: json.RawMessage(`{"type":"waiting_for_input"}`)}
	queued, err := store.QueueWaitingForInput(journal.Key(), firstEvent, protocol.StateTransitionRequest{TransitionID: "transition-1", State: "waiting_for_input", Payload: firstEvent.Payload})
	if err != nil {
		t.Fatalf("QueueWaitingForInput(first) error = %v", err)
	}
	if _, err := store.MarkTransitionsDelivered(journal.Key(), []string{queued.PendingTransitions[0].TransitionID}); err != nil {
		t.Fatalf("MarkTransitionsDelivered() error = %v", err)
	}
	secondEvent := protocol.RunEvent{EventID: "event-2", Kind: "waiting_for_input", OccurredAt: time.Date(2026, 9, 3, 1, 2, 6, 0, time.UTC), Payload: json.RawMessage(`{"type":"waiting_for_input","question":"again"}`)}
	queued, err = store.QueueWaitingForInput(journal.Key(), secondEvent, protocol.StateTransitionRequest{TransitionID: "transition-2", State: "waiting_for_input", Payload: secondEvent.Payload})
	if err != nil {
		t.Fatalf("QueueWaitingForInput(after delivery) error = %v", err)
	}
	if queued.LocalState != "waiting_for_input" || queued.LastEventSequence != 2 || len(queued.PendingEvents) != 2 || len(queued.PendingTransitions) != 0 {
		t.Fatalf("QueueWaitingForInput() after delivery = %#v", queued)
	}
}

func TestQueueWaitingForInputRejectsInvalidTransitionWithoutPartialEvent(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	journal.LastEventSequence = 0
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	event := protocol.RunEvent{EventID: "event-1", Kind: "waiting_for_input", OccurredAt: time.Date(2026, 9, 3, 1, 2, 5, 0, time.UTC), Payload: json.RawMessage(`{"type":"waiting_for_input"}`)}
	if _, err := store.QueueWaitingForInput(journal.Key(), event, protocol.StateTransitionRequest{State: "waiting_for_input", Payload: event.Payload}); err == nil {
		t.Fatal("QueueWaitingForInput() accepted invalid transition")
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatalf("LoadJournal() error = %v", err)
	}
	if loaded.LocalState != journal.LocalState || loaded.LastEventSequence != 0 || len(loaded.PendingEvents) != 0 || len(loaded.PendingTransitions) != 0 {
		t.Fatalf("invalid waiting transition partially mutated journal: %#v", loaded)
	}
}

func TestQueueWaitingForInputPreservesTerminalTransitionsAndRestarts(t *testing.T) {
	for _, terminalState := range []string{"completed", "failed", "cancelled"} {
		t.Run(terminalState, func(t *testing.T) {
			store := mustStore(t)
			journal := testJournal("run-1", 1)
			journal.LastEventSequence = 0
			if err := store.SaveJournal(journal); err != nil {
				t.Fatalf("SaveJournal() error = %v", err)
			}
			terminal, err := store.QueueTerminalTransition(journal.Key(), protocol.StateTransitionRequest{TransitionID: "terminal-1", State: terminalState, Payload: json.RawMessage(`{"terminal":true}`)})
			if err != nil {
				t.Fatalf("QueueTerminalTransition() error = %v", err)
			}
			before := append([]protocol.StateTransitionRequest(nil), terminal.PendingTransitions...)
			event := protocol.RunEvent{EventID: "event-1", Kind: "waiting_for_input", OccurredAt: time.Date(2026, 9, 3, 1, 2, 5, 0, time.UTC), Payload: json.RawMessage(`{"type":"waiting_for_input","question":"late"}`)}
			queued, err := store.QueueWaitingForInput(journal.Key(), event, protocol.StateTransitionRequest{TransitionID: "waiting-1", State: "waiting_for_input", Payload: event.Payload})
			if err != nil {
				t.Fatalf("QueueWaitingForInput() error = %v", err)
			}
			if queued.LocalState != "terminal_pending" || queued.LastEventSequence != 1 || len(queued.PendingEvents) != 1 || !reflect.DeepEqual(queued.PendingTransitions, before) {
				t.Fatalf("terminal waiting race = %#v", queued)
			}

			if err := store.Close(); err != nil {
				t.Fatalf("Close() error = %v", err)
			}
			restarted, err := New(store.dir)
			if err != nil {
				t.Fatalf("New() after restart error = %v", err)
			}
			t.Cleanup(func() { _ = restarted.Close() })
			loaded, err := restarted.LoadJournal(journal.Key())
			if err != nil {
				t.Fatalf("LoadJournal() after restart error = %v", err)
			}
			if loaded.LocalState != "terminal_pending" || loaded.LastEventSequence != 1 || len(loaded.PendingEvents) != 1 || !reflect.DeepEqual(loaded.PendingTransitions, before) {
				t.Fatalf("terminal journal after restart = %#v", loaded)
			}
		})
	}
}

func TestQueueWaitingForInputPreservesPendingTerminalTransition(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	journal.LastEventSequence = 0
	journal.PendingTransitions = transitionsForStates(journal, []string{"running", "failed"})
	journal.LocalState = "terminal_pending"
	journal.TerminalPendingAt = time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	journal.TerminalState = "failed"
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	before := append([]protocol.StateTransitionRequest(nil), journal.PendingTransitions...)
	event := protocol.RunEvent{EventID: "event-1", Kind: "waiting_for_input", OccurredAt: time.Date(2026, 9, 3, 1, 2, 5, 0, time.UTC), Payload: json.RawMessage(`{"type":"waiting_for_input"}`)}
	queued, err := store.QueueWaitingForInput(journal.Key(), event, protocol.StateTransitionRequest{TransitionID: "waiting-1", State: "waiting_for_input", Payload: event.Payload})
	if err != nil {
		t.Fatalf("QueueWaitingForInput() error = %v", err)
	}
	if queued.LocalState != "terminal_pending" || queued.LastEventSequence != 1 || len(queued.PendingEvents) != 1 || !reflect.DeepEqual(queued.PendingTransitions, before) {
		t.Fatalf("pending terminal waiting race = %#v", queued)
	}
}

func TestQueueWaitingForInputStartsNewEpisodeAfterRunning(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	journal.LastEventSequence = 0
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	first := protocol.RunEvent{EventID: "waiting-1", Kind: "waiting_for_input", OccurredAt: time.Date(2026, 9, 3, 1, 2, 5, 0, time.UTC), Payload: json.RawMessage(`{"type":"waiting_for_input","question":"first"}`)}
	if _, err := store.QueueWaitingForInput(journal.Key(), first, protocol.StateTransitionRequest{TransitionID: "waiting-1", State: "waiting_for_input", Payload: first.Payload}); err != nil {
		t.Fatalf("QueueWaitingForInput(first) error = %v", err)
	}
	if _, err := store.QueueRunningTransition(journal.Key(), protocol.StateTransitionRequest{TransitionID: "running-1", State: "running", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatalf("QueueRunningTransition() error = %v", err)
	}
	second := protocol.RunEvent{EventID: "waiting-2", Kind: "waiting_for_input", OccurredAt: time.Date(2026, 9, 3, 1, 2, 6, 0, time.UTC), Payload: json.RawMessage(`{"type":"waiting_for_input","question":"second"}`)}
	queued, err := store.QueueWaitingForInput(journal.Key(), second, protocol.StateTransitionRequest{TransitionID: "waiting-2", State: "waiting_for_input", Payload: second.Payload})
	if err != nil {
		t.Fatalf("QueueWaitingForInput(second) error = %v", err)
	}
	if got, want := transitionStates(queued.PendingTransitions), []string{"waiting_for_input", "running", "waiting_for_input"}; !equalStrings(got, want) || queued.LastEventSequence != 2 || len(queued.PendingEvents) != 2 || string(queued.PendingTransitions[0].Payload) != string(first.Payload) || string(queued.PendingTransitions[2].Payload) != string(second.Payload) {
		t.Fatalf("waiting episodes = %#v", queued)
	}
}

func TestQueueWaitingForInputDoesNotUpdateAttemptedTransition(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	journal.LastEventSequence = 0
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	first := protocol.RunEvent{EventID: "waiting-1", Kind: "waiting_for_input", OccurredAt: time.Date(2026, 9, 3, 1, 2, 5, 0, time.UTC), Payload: json.RawMessage(`{"type":"waiting_for_input","question":"first"}`)}
	queued, err := store.QueueWaitingForInput(journal.Key(), first, protocol.StateTransitionRequest{TransitionID: "waiting-1", State: "waiting_for_input", Payload: first.Payload})
	if err != nil {
		t.Fatalf("QueueWaitingForInput(first) error = %v", err)
	}
	if _, err := store.MarkTransitionAttempted(journal.Key(), queued.PendingTransitions[0].TransitionID); err != nil {
		t.Fatalf("MarkTransitionAttempted() error = %v", err)
	}
	second := protocol.RunEvent{EventID: "waiting-2", Kind: "waiting_for_input", OccurredAt: time.Date(2026, 9, 3, 1, 2, 6, 0, time.UTC), Payload: json.RawMessage(`{"type":"waiting_for_input","question":"second"}`)}
	queued, err = store.QueueWaitingForInput(journal.Key(), second, protocol.StateTransitionRequest{TransitionID: "waiting-2", State: "waiting_for_input", Payload: second.Payload})
	if err != nil {
		t.Fatalf("QueueWaitingForInput(second) error = %v", err)
	}
	if len(queued.PendingEvents) != 2 || len(queued.PendingTransitions) != 1 || queued.PendingTransitions[0].TransitionID != "waiting-1" || string(queued.PendingTransitions[0].Payload) != string(first.Payload) || !equalStrings(queued.AttemptedTransitionIDs, []string{"waiting-1"}) {
		t.Fatalf("attempted waiting transition = %#v", queued)
	}
}

func TestTransitionAttemptMarkersRestartValidateAndCleanUp(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	journal.PendingTransitions = transitionsForStates(journal, []string{"running"})
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	marked, err := store.MarkTransitionAttempted(journal.Key(), "pending-0")
	if err != nil {
		t.Fatalf("MarkTransitionAttempted() error = %v", err)
	}
	if _, err := store.MarkTransitionAttempted(journal.Key(), "pending-0"); err != nil {
		t.Fatalf("MarkTransitionAttempted() idempotency error = %v", err)
	}
	if !equalStrings(marked.AttemptedTransitionIDs, []string{"pending-0"}) {
		t.Fatalf("attempted transitions = %#v", marked.AttemptedTransitionIDs)
	}
	if _, err := store.MarkTransitionAttempted(journal.Key(), "missing"); err == nil {
		t.Fatal("MarkTransitionAttempted() accepted a non-pending transition")
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	restarted, err := New(store.dir)
	if err != nil {
		t.Fatalf("New() after restart error = %v", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	loaded, err := restarted.LoadJournal(journal.Key())
	if err != nil {
		t.Fatalf("LoadJournal() after restart error = %v", err)
	}
	if !equalStrings(loaded.AttemptedTransitionIDs, []string{"pending-0"}) {
		t.Fatalf("attempted transitions after restart = %#v", loaded.AttemptedTransitionIDs)
	}
	if _, err := restarted.MarkTransitionsDelivered(journal.Key(), []string{"pending-0"}); err != nil {
		t.Fatalf("MarkTransitionsDelivered() error = %v", err)
	}
	loaded, err = restarted.LoadJournal(journal.Key())
	if err != nil {
		t.Fatalf("LoadJournal() after delivery error = %v", err)
	}
	if len(loaded.PendingTransitions) != 0 || len(loaded.AttemptedTransitionIDs) != 0 {
		t.Fatalf("delivered transition markers = %#v", loaded)
	}
}

func TestAttemptedTransitionMarkerValidationAndLegacyCompatibility(t *testing.T) {
	store := mustStore(t)
	legacy := testJournal("legacy-run", 1)
	if err := store.SaveJournal(legacy); err != nil {
		t.Fatalf("SaveJournal(legacy) error = %v", err)
	}
	encoded, err := os.ReadFile(store.journalPath(legacy.Key()))
	if err != nil {
		t.Fatalf("ReadFile(legacy) error = %v", err)
	}
	if strings.Contains(string(encoded), "attempted_transition_ids") {
		t.Fatalf("legacy journal unexpectedly serialized empty markers: %s", encoded)
	}
	if _, err := store.LoadJournal(legacy.Key()); err != nil {
		t.Fatalf("LoadJournal(legacy) error = %v", err)
	}

	for _, test := range []struct {
		name      string
		attempted []string
	}{
		{name: "unknown pending transition", attempted: []string{"missing"}},
		{name: "duplicate marker", attempted: []string{"pending-0", "pending-0"}},
		{name: "blank marker", attempted: []string{" "}},
	} {
		t.Run(test.name, func(t *testing.T) {
			journal := testJournal("run-"+strings.ReplaceAll(test.name, " ", "-"), 1)
			journal.PendingTransitions = transitionsForStates(journal, []string{"running"})
			journal.AttemptedTransitionIDs = test.attempted
			if err := store.SaveJournal(journal); err == nil {
				t.Fatal("SaveJournal() accepted invalid attempted transition markers")
			}
		})
	}
}

func TestTerminalReplacementRemovesAttemptMarkers(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	journal.PendingTransitions = transitionsForStates(journal, []string{"running"})
	journal.AttemptedTransitionIDs = []string{"pending-0"}
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	queued, err := store.QueueTerminalTransition(journal.Key(), protocol.StateTransitionRequest{TransitionID: "cancelled-1", State: "cancelled", Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("QueueTerminalTransition() error = %v", err)
	}
	if got, want := transitionStates(queued.PendingTransitions), []string{"cancelled"}; !equalStrings(got, want) || len(queued.AttemptedTransitionIDs) != 0 {
		t.Fatalf("cancel replacement = %#v", queued)
	}
}

func TestTerminalPendingTimestampSurvivesRestartAndCancelReplacement(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	enteredAt := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	queued, err := store.QueueTerminalTransitionAt(journal.Key(), protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}, enteredAt)
	if err != nil {
		t.Fatalf("QueueTerminalTransitionAt(completed) error = %v", err)
	}
	cancelledAt := enteredAt.Add(time.Minute)
	queued, err = store.QueueTerminalTransitionAt(journal.Key(), protocol.StateTransitionRequest{TransitionID: "cancelled-1", State: "cancelled", Payload: json.RawMessage(`{}`)}, cancelledAt)
	if err != nil {
		t.Fatalf("QueueTerminalTransitionAt(cancelled) error = %v", err)
	}
	if queued.LocalState != "terminal_pending" || !queued.TerminalPendingAt.Equal(enteredAt) || queued.TerminalState != "cancelled" || queued.TerminalVerdict != "" || !queued.TerminalResolvedAt.IsZero() || !equalStrings(transitionStates(queued.PendingTransitions), []string{"cancelled"}) {
		t.Fatalf("cancelled terminal journal = %#v", queued)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	restarted, err := New(store.dir)
	if err != nil {
		t.Fatalf("New() after restart error = %v", err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	loaded, err := restarted.LoadJournal(journal.Key())
	if err != nil {
		t.Fatalf("LoadJournal() after restart error = %v", err)
	}
	if !loaded.TerminalPendingAt.Equal(enteredAt) || loaded.TerminalState != "cancelled" || loaded.TerminalVerdict != "" {
		t.Fatalf("terminal journal after restart = %#v", loaded)
	}
	resolvedAt := enteredAt.Add(2 * time.Minute)
	resolved, err := restarted.ResolveTerminal(journal.Key(), TerminalVerdictOwnershipLost, resolvedAt)
	if err != nil {
		t.Fatalf("ResolveTerminal() error = %v", err)
	}
	if resolved.TerminalVerdict != TerminalVerdictOwnershipLost || !resolved.TerminalResolvedAt.Equal(resolvedAt) || !resolved.TerminalPendingAt.Equal(enteredAt) {
		t.Fatalf("resolved terminal journal = %#v", resolved)
	}
}

func TestLegacyJournalWithoutTerminalFieldsLoads(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("legacy-run", 1)
	encoded, err := json.Marshal(journal)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	delete(document, "terminal_pending_at")
	delete(document, "terminal_state")
	delete(document, "terminal_verdict")
	delete(document, "terminal_resolved_at")
	encoded, err = json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.journalPath(journal.Key()), encoded, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatalf("LoadJournal() error = %v", err)
	}
	if !loaded.TerminalPendingAt.IsZero() || loaded.TerminalState != "" || loaded.TerminalVerdict != "" || !loaded.TerminalResolvedAt.IsZero() {
		t.Fatalf("legacy terminal fields = %#v", loaded)
	}
}

func TestTerminalMetadataMatchesTerminalPendingLifecycle(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	enteredAt := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)

	journal.TerminalPendingAt = enteredAt
	if err := store.SaveJournal(journal); err == nil {
		t.Fatal("SaveJournal() accepted terminal metadata on a nonterminal journal")
	}

	journal = testJournal("run-2", 1)
	journal.LocalState = "terminal_pending"
	if err := store.SaveJournal(journal); err == nil {
		t.Fatal("SaveJournal() accepted terminal_pending without metadata")
	}

	journal.TerminalPendingAt = enteredAt
	journal.TerminalState = "running"
	if err := store.SaveJournal(journal); err == nil {
		t.Fatal("SaveJournal() accepted a nonterminal target state")
	}
}

func TestQueueRunningTransitionIsAtomic(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	journal.LocalState = "waiting_for_input"
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	queued, err := store.QueueRunningTransition(journal.Key(), protocol.StateTransitionRequest{TransitionID: "running-1", State: "running", Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("QueueRunningTransition() error = %v", err)
	}
	if queued.LocalState != "running" || len(queued.PendingTransitions) != 1 || queued.PendingTransitions[0].TransitionID != "running-1" || !sameFence(queued.PendingTransitions[0].Fence, queued.Fence()) {
		t.Fatalf("QueueRunningTransition() = %#v", queued)
	}
	if _, err := store.QueueRunningTransition(journal.Key(), protocol.StateTransitionRequest{TransitionID: "waiting-1", State: "waiting_for_input", Payload: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("QueueRunningTransition() accepted non-running state")
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatalf("LoadJournal() error = %v", err)
	}
	if loaded.LocalState != "running" || len(loaded.PendingTransitions) != 1 || loaded.PendingTransitions[0].TransitionID != "running-1" {
		t.Fatalf("invalid running transition partially mutated journal: %#v", loaded)
	}
}

func TestQueueRunningTransitionRejectsTerminalPendingJournal(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("run-1", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatalf("SaveJournal() error = %v", err)
	}
	terminal, err := store.QueueTerminalTransition(journal.Key(), protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatalf("QueueTerminalTransition() error = %v", err)
	}
	if _, err := store.QueueRunningTransition(journal.Key(), protocol.StateTransitionRequest{TransitionID: "running-1", State: "running", Payload: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("QueueRunningTransition() succeeded after terminal transition")
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatalf("LoadJournal() error = %v", err)
	}
	if !reflect.DeepEqual(loaded, terminal) {
		t.Fatalf("terminal journal mutated by running transition: %#v", loaded)
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
