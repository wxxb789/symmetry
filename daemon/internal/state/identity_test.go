package state

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestLoadIdentityMissingReturnsTypedNotFound(t *testing.T) {
	t.Parallel()
	store := mustStore(t)

	_, err := store.LoadIdentity()
	if !IsNotFound(err) {
		t.Fatalf("LoadIdentity() error = %v, want typed not found", err)
	}
}

func TestIdentityRoundTripAndPermissions(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestIdentityErrorsDoNotExposeToken(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestSaveClaimIntentRequiresBoundedWorkspaceBindingKey(t *testing.T) {
	t.Parallel()
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
