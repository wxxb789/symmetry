package state

import (
	"crypto/sha256"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestProvideInputIntentIsAtomicAcrossCompletionDeliveryAndEpisodes(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	store := mustStore(t)
	journal := stoppedTestJournal("run-input-terminal", 1)
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
	t.Parallel()
	store := mustStore(t)
	journal := stoppedTestJournal("run-input-cleanup", 1)
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

func TestCompletedTerminalTaskResultKindSurvivesDeliveryAndRestart(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	journal := stoppedTestJournal("run-terminal-result", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	pendingAt := time.Date(2026, 9, 10, 1, 2, 3, 0, time.UTC)
	transition := protocol.StateTransitionRequest{
		TransitionID: "completed-candidate",
		State:        "completed",
		Payload:      json.RawMessage(`{"task_result":{"kind":"candidate_completion"}}`),
	}
	queued, err := store.QueueTerminalTransitionAt(journal.Key(), transition, pendingAt)
	if err != nil {
		t.Fatal(err)
	}
	if queued.TerminalTaskResultKind != protocol.TaskResultCandidateCompletion {
		t.Fatalf("queued terminal task result kind = %q, want candidate_completion", queued.TerminalTaskResultKind)
	}
	replayed, err := store.QueueTerminalTransitionAt(journal.Key(), transition, pendingAt.Add(time.Second))
	if err != nil {
		t.Fatalf("replayed QueueTerminalTransitionAt() error = %v", err)
	}
	if replayed.TerminalTaskResultKind != protocol.TaskResultCandidateCompletion || replayed.TerminalState != "completed" {
		t.Fatalf("replayed completed terminal transition changed metadata: %#v", replayed)
	}
	if _, err := store.ResolveTerminal(journal.Key(), TerminalVerdictAccepted, pendingAt.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkTransitionsDelivered(journal.Key(), []string{transition.TransitionID}); err != nil {
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
	if loaded.TerminalTaskResultKind != protocol.TaskResultCandidateCompletion {
		t.Fatalf("restarted terminal task result kind = %q, want candidate_completion", loaded.TerminalTaskResultKind)
	}
}

func TestCancelledTerminalCannotReplaceCompletedTaskResultKind(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	journal := testJournal("run-terminal-cancel", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	pendingAt := time.Date(2026, 9, 10, 1, 2, 3, 0, time.UTC)
	if _, err := store.QueueTerminalTransitionAt(journal.Key(), protocol.StateTransitionRequest{
		TransitionID: "completed-candidate",
		State:        "completed",
		Payload:      json.RawMessage(`{"task_result":{"kind":"candidate_completion"}}`),
	}, pendingAt); err != nil {
		t.Fatal(err)
	}
	_, err := store.QueueCancelledTransitionAndAcknowledgementAt(journal.Key(), protocol.StateTransitionRequest{
		TransitionID: "cancelled-1", State: "cancelled", Payload: json.RawMessage(`{"reason":"cancelled"}`),
	}, protocol.CommandAcknowledgement{CommandID: "cancel-1", Outcome: "applied", AckID: "ack-1"}, pendingAt.Add(time.Second))
	if err == nil {
		t.Fatal("cancellation replaced an authoritative completed terminal")
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TerminalState != "completed" || loaded.TerminalTaskResultKind != protocol.TaskResultCandidateCompletion || len(loaded.PendingTransitions) != 1 || loaded.PendingTransitions[0].State != "completed" || len(loaded.PendingCommandAcknowledgements) != 0 {
		t.Fatalf("cancellation mutated authoritative completed terminal = %#v", loaded)
	}
}

func TestCompletedTerminalTaskResultKindRejectsInvalidAtomicWrite(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	journal := testJournal("run-terminal-invalid", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransition(journal.Key(), protocol.StateTransitionRequest{
		TransitionID: "completed-invalid", State: "completed", Payload: json.RawMessage(`{"task_result":{"kind":"invalid"}}`),
	}); err == nil {
		t.Fatal("QueueTerminalTransition() accepted an invalid task result kind")
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TerminalState != "" || loaded.TerminalTaskResultKind != "" || len(loaded.PendingTransitions) != 0 {
		t.Fatalf("invalid completed transition partially updated journal: %#v", loaded)
	}
}

func TestTerminalTaskResultKindNeverBackfillsAfterFirstTerminalTransition(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		first protocol.StateTransitionRequest
	}{
		{
			name:  "failed before completed candidate",
			first: protocol.StateTransitionRequest{TransitionID: "failed-first", State: "failed", Payload: json.RawMessage(`{"reason":"process_failure"}`)},
		},
		{
			name:  "completed without task result",
			first: protocol.StateTransitionRequest{TransitionID: "completed-without-result", State: "completed", Payload: json.RawMessage(`{}`)},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := mustStore(t)
			journal := testJournal("run-no-backfill", 1)
			if err := store.SaveJournal(journal); err != nil {
				t.Fatal(err)
			}
			if _, err := store.QueueTerminalTransition(journal.Key(), test.first); err != nil {
				t.Fatal(err)
			}
			_, err := store.QueueTerminalTransition(journal.Key(), protocol.StateTransitionRequest{
				TransitionID: "completed-candidate", State: "completed", Payload: json.RawMessage(`{"task_result":{"kind":"candidate_completion"}}`),
			})
			if err == nil {
				t.Fatal("later terminal transition was accepted")
			}
			updated, err := store.LoadJournal(journal.Key())
			if err != nil {
				t.Fatal(err)
			}
			if updated.TerminalTaskResultKind != "" || len(updated.PendingTransitions) != 1 || updated.PendingTransitions[0].TransitionID != test.first.TransitionID {
				t.Fatalf("later completed transition backfilled terminal metadata: %#v", updated)
			}
		})
	}
}

func TestSaveJournalCannotChangeOrClearTerminalTaskResultKind(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	journal := testJournal("run-terminal-immutable", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransition(journal.Key(), protocol.StateTransitionRequest{
		TransitionID: "completed-candidate",
		State:        "completed",
		Payload:      json.RawMessage(`{"task_result":{"kind":"candidate_completion"}}`),
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	cleared := loaded
	cleared.TerminalTaskResultKind = ""
	if err := store.SaveJournal(cleared); err == nil {
		t.Fatal("SaveJournal() cleared immutable terminal task result kind")
	}
	changed := loaded
	changed.TerminalTaskResultKind = protocol.TaskResultProgress
	if err := store.SaveJournal(changed); err == nil {
		t.Fatal("SaveJournal() changed immutable terminal task result kind")
	}
	loaded, err = store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.TerminalTaskResultKind != protocol.TaskResultCandidateCompletion {
		t.Fatalf("terminal task result kind changed after rejected saves: %#v", loaded)
	}
}

func TestEnterCleanupPendingPreservesTerminalAuditAndControlsDelivery(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	journal := stoppedTestJournal("run-1", 1)
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

	journal = stoppedTestJournal("run-2", 1)
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
	t.Parallel()
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	journal := stoppedTestJournal("run-1", 1)
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
	t.Parallel()
	store := mustStore(t)
	journal := stoppedTestJournal("run-atomic-cleanup", 1)
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
	t.Parallel()
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
	t.Parallel()
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

func TestLegacyJournalWithoutTerminalFieldsLoads(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

func TestDeleteJournalRequiresCommandAcknowledgementDelivery(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	journal := stoppedTestJournal("run-stale-ack", 1)
	journal.LocalState = "stale"
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	acknowledgement := protocol.CommandAcknowledgement{CommandID: "cancel-1", Outcome: "rejected", AckID: "ack-1"}
	queued, err := store.QueueCommandAcknowledgement(journal.Key(), acknowledgement)
	if err != nil {
		t.Fatal(err)
	}

	if err := store.DeleteJournal(journal.Key()); err == nil || !strings.Contains(err.Error(), "command acknowledgement") {
		t.Fatalf("DeleteJournal() error = %v, want pending acknowledgement guard", err)
	}
	retained, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if len(retained.PendingCommandAcknowledgements) != 1 || retained.PendingCommandAcknowledgements[0].AckID != acknowledgement.AckID {
		t.Fatalf("retained pending acknowledgements = %#v", retained.PendingCommandAcknowledgements)
	}
	if _, err := store.MarkCommandAcknowledgementsDelivered(journal.Key(), []string{queued.PendingCommandAcknowledgements[0].AckID}); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteJournal(journal.Key()); err != nil {
		t.Fatalf("DeleteJournal() after acknowledgement delivery error = %v", err)
	}
	if _, err := store.LoadJournal(journal.Key()); !IsNotFound(err) {
		t.Fatalf("LoadJournal(deleted) error = %v, want not found", err)
	}
}
