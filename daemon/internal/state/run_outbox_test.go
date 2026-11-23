package state

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestControlCommandReceiptRejectsDuplicateRawPayloadMembers(t *testing.T) {
	t.Parallel()
	duplicates := []struct {
		name    string
		payload json.RawMessage
	}{
		{name: "top-level", payload: json.RawMessage(`{"command_id":"pause-raw","command_id":"pause-raw","kind":"pause","outcome":"applied"}`)},
		{name: "nested", payload: json.RawMessage(`{"command_id":"pause-raw","kind":"pause","outcome":"applied","metadata":{"source":"first","source":"second"}}`)},
		{name: "escaped", payload: json.RawMessage(`{"command_id":"pause-raw","\u006bind":"pause","kind":"pause","outcome":"applied"}`)},
	}

	for _, test := range duplicates {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			store := mustStore(t)
			journal := testJournal("run-raw-command-"+test.name, 1)
			if err := store.SaveJournal(journal); err != nil {
				t.Fatal(err)
			}
			intent := ControlCommandIntent{
				CommandID: "pause-raw", Kind: "pause", PayloadDigest: strings.Repeat("a", 64),
				TransitionID: "transition-pause-raw", AckID: "ack-pause-raw",
			}
			if _, _, err := store.PrepareControlCommand(journal.Key(), intent); err != nil {
				t.Fatal(err)
			}
			_, err := store.CompleteControlCommand(journal.Key(), intent.CommandID, intent.Kind, "applied", protocol.RunEvent{
				EventID: "event-pause-raw", Kind: "command_applied", OccurredAt: time.Date(2026, 9, 16, 1, 2, 4, 0, time.UTC), Payload: test.payload,
			})
			if err == nil {
				t.Fatal("CompleteControlCommand() accepted duplicate receipt payload members")
			}
		})
	}
}

func TestQueueOutputEventBoundsPendingPayloadAndQueuesMarker(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	defer store.Close()
	journal := testJournal("run-output-budget", 1)
	journal.LastEventSequence = 0
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 1, 2, 3, 0, time.UTC)
	firstPayload := json.RawMessage(`{"chunk":"first"}`)
	queued, dropped, err := store.QueueOutputEvent(journal.Key(), protocol.RunEvent{
		EventID: "output-1", Kind: "output", OccurredAt: now, Payload: firstPayload,
	}, len(firstPayload))
	if err != nil || dropped || queued.LastEventSequence != 1 || len(queued.PendingEvents) != 1 {
		t.Fatalf("QueueOutputEvent(first) = %#v, dropped=%t, err=%v", queued, dropped, err)
	}
	secondPayload := json.RawMessage(`{"chunk":"second"}`)
	queued, dropped, err = store.QueueOutputEvent(journal.Key(), protocol.RunEvent{
		EventID: "output-2", Kind: "output", OccurredAt: now.Add(time.Second), Payload: secondPayload,
	}, len(firstPayload))
	if err != nil || !dropped || queued.LastEventSequence != 1 || len(queued.PendingEvents) != 1 || queued.DroppedOutputChunks != 1 || queued.DroppedOutputBytes != int64(len(secondPayload)) {
		t.Fatalf("QueueOutputEvent(over budget) = %#v, dropped=%t, err=%v", queued, dropped, err)
	}
	progress, err := store.QueueNextEvent(journal.Key(), protocol.RunEvent{
		EventID: "progress-1", Kind: "progress", OccurredAt: now.Add(2 * time.Second), Payload: json.RawMessage(`{}`),
	})
	if err != nil || progress.LastEventSequence != 2 {
		t.Fatalf("QueueNextEvent() = %#v, err=%v", progress, err)
	}
	marked, appended, err := store.QueueOutputTruncatedMarker(journal.Key(), now.Add(3*time.Second), func() (string, error) { return "marker-1", nil })
	if err != nil || !appended || marked.DroppedOutputChunks != 0 || marked.DroppedOutputBytes != 0 || marked.LastEventSequence != 3 || len(marked.PendingEvents) != 3 {
		t.Fatalf("QueueOutputTruncatedMarker() = %#v, appended=%t, err=%v", marked, appended, err)
	}
	var markerPayload map[string]int64
	if err := json.Unmarshal(marked.PendingEvents[2].Payload, &markerPayload); err != nil || markerPayload["dropped_chunks"] != 1 || markerPayload["dropped_bytes"] != int64(len(secondPayload)) {
		t.Fatalf("marker payload = %#v, err=%v", markerPayload, err)
	}
	unchanged, appended, err := store.QueueOutputTruncatedMarker(journal.Key(), now.Add(4*time.Second), nil)
	if err != nil || appended || unchanged.LastEventSequence != 3 {
		t.Fatalf("QueueOutputTruncatedMarker(empty) = %#v, appended=%t, err=%v", unchanged, appended, err)
	}

	directory := store.dir
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	loaded, err := restarted.LoadJournal(journal.Key())
	if err != nil || loaded.DroppedOutputChunks != 0 || loaded.DroppedOutputBytes != 0 || loaded.LastEventSequence != 3 {
		t.Fatalf("journal after restart = %#v, err=%v", loaded, err)
	}
}

func TestQueueOutputEventDropFailureDoesNotClaimOrPersistDrop(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	defer store.Close()
	journal := testJournal("run-output-drop-write-failure", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	restore := store.SetAtomicWriterForTesting(func(string, []byte) error {
		return errors.New("injected output drop write failure")
	})
	defer restore()

	queued, dropped, err := store.QueueOutputEvent(journal.Key(), protocol.RunEvent{
		EventID: "output-drop-failure", Kind: "output", OccurredAt: time.Date(2026, 9, 17, 1, 2, 3, 0, time.UTC), Payload: json.RawMessage(`{"chunk":"dropped"}`),
	}, 0)
	if err == nil || dropped || queued.RunID != "" || len(queued.PendingEvents) != 0 {
		t.Fatalf("QueueOutputEvent() = %#v, dropped=%t, err=%v; want failed atomic drop", queued, dropped, err)
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.DroppedOutputChunks != 0 || loaded.DroppedOutputBytes != 0 || len(loaded.PendingEvents) != 0 || loaded.LastEventSequence != journal.LastEventSequence {
		t.Fatalf("journal after failed drop = %#v, want unchanged counters/events", loaded)
	}
}

func TestMarkEventsDeliveredAndQueueOutputTruncatedMarkerSurvivesRestart(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	journal := testJournal("run-output-marker", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	key := journal.Key()
	now := time.Date(2026, 9, 7, 1, 2, 3, 0, time.UTC)
	payload := json.RawMessage(`{"chunk":"first"}`)

	if _, dropped, err := store.QueueOutputEvent(key, protocol.RunEvent{
		EventID: "output-1", Kind: "output", OccurredAt: now, Payload: payload,
	}, len(payload)); err != nil || dropped {
		t.Fatalf("QueueOutputEvent(first) dropped=%t, err=%v", dropped, err)
	}

	if _, dropped, err := store.QueueOutputEvent(key, protocol.RunEvent{
		EventID: "output-2", Kind: "output", OccurredAt: now.Add(time.Second), Payload: json.RawMessage(`{"chunk":"second"}`),
	}, len(payload)); err != nil || !dropped {
		t.Fatalf("QueueOutputEvent(second) dropped=%t, err=%v", dropped, err)
	}

	updated, appended, err := store.MarkEventsDeliveredAndQueueOutputTruncatedMarker(key, []string{"output-1"}, now.Add(2*time.Second), func() (string, error) { return "marker-1", nil })
	if err != nil || !appended || updated.DroppedOutputChunks != 0 || updated.DroppedOutputBytes != 0 || len(updated.PendingEvents) != 1 || updated.PendingEvents[0].Kind != "output_truncated" {
		t.Fatalf("MarkEventsDeliveredAndQueueOutputTruncatedMarker() = %#v, appended=%t, err=%v", updated, appended, err)
	}

	directory := store.dir
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	loaded, err := restarted.LoadJournal(key)
	if err != nil || len(loaded.PendingEvents) != 1 || loaded.PendingEvents[0].Kind != "output_truncated" || loaded.DroppedOutputChunks != 0 || loaded.DroppedOutputBytes != 0 {
		t.Fatalf("journal after restart = %#v, err=%v", loaded, err)
	}
}

func TestQueueCommandAcknowledgementIsIdempotentByCommandAndOutcome(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

func TestJournalRejectsDuplicatePendingCommandAcknowledgementCommandIDs(t *testing.T) {
	t.Parallel()
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

func TestPendingOutboxRequiresPersistedClaimGrant(t *testing.T) {
	t.Parallel()
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

func TestQueueTerminalTransitionIsAtomic(t *testing.T) {
	t.Parallel()
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

func TestQueueTerminalTransitionRejectsNonterminalStatesWithoutMutation(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	tests := []struct {
		name          string
		pendingStates []string
		incomingState string
		wantErr       bool
		wantStates    []string
	}{
		{
			name:          "completed rejects later cancelled",
			pendingStates: []string{"completed"},
			incomingState: "cancelled",
			wantErr:       true,
			wantStates:    []string{"completed"},
		},
		{
			name:          "cancelled rejects later completed",
			pendingStates: []string{"cancelled"},
			incomingState: "completed",
			wantErr:       true,
			wantStates:    []string{"cancelled"},
		},
		{
			name:          "completed rejects later failed",
			pendingStates: []string{"completed"},
			incomingState: "failed",
			wantErr:       true,
			wantStates:    []string{"completed"},
		},
		{
			name:          "completed rejects later cancelled after running",
			pendingStates: []string{"running", "completed"},
			incomingState: "cancelled",
			wantErr:       true,
			wantStates:    []string{"running", "completed"},
		},
		{
			name:          "cancelled replaces nonterminal running",
			pendingStates: []string{"running"},
			incomingState: "cancelled",
			wantStates:    []string{"cancelled"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
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
			if test.wantErr {
				if err == nil {
					t.Fatal("QueueTerminalTransition() accepted a later terminal")
				}
				queued, err = store.LoadJournal(journal.Key())
			}
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
	t.Parallel()
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
	t.Parallel()
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

func TestQueueTerminalTransitionAndAcknowledgementPersistsFailedRejectedPair(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	journal := testJournal("run-terminal-command", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	pendingAt := time.Date(2026, 9, 20, 1, 2, 3, 0, time.UTC)
	transition := protocol.StateTransitionRequest{TransitionID: "failed-1", State: "failed", Payload: json.RawMessage(`{"reason":"native_run_failed"}`)}
	acknowledgement := protocol.CommandAcknowledgement{RunID: journal.RunID, CommandID: "cancel-1", Outcome: "rejected", AckID: "ack-1"}

	queued, err := store.QueueTerminalTransitionAndAcknowledgementAt(journal.Key(), transition, acknowledgement, pendingAt)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, queued) {
		t.Fatalf("persisted terminal pair:\n got: %#v\nwant: %#v", loaded, queued)
	}
	if loaded.LocalState != "terminal_pending" || loaded.TerminalState != "failed" || !loaded.TerminalPendingAt.Equal(pendingAt) {
		t.Fatalf("terminal state = %#v", loaded)
	}
	if len(loaded.PendingTransitions) != 1 || loaded.PendingTransitions[0].TransitionID != transition.TransitionID || loaded.PendingTransitions[0].State != transition.State || !sameFence(loaded.PendingTransitions[0].Fence, journal.Fence()) {
		t.Fatalf("pending transitions = %#v", loaded.PendingTransitions)
	}
	if len(loaded.PendingCommandAcknowledgements) != 1 || loaded.PendingCommandAcknowledgements[0].CommandID != acknowledgement.CommandID || loaded.PendingCommandAcknowledgements[0].Outcome != acknowledgement.Outcome || loaded.PendingCommandAcknowledgements[0].AckID != acknowledgement.AckID || !sameFence(loaded.PendingCommandAcknowledgements[0].Fence, journal.Fence()) {
		t.Fatalf("pending acknowledgements = %#v", loaded.PendingCommandAcknowledgements)
	}
}

func TestQueueTerminalTransitionAndAcknowledgementInvalidAcknowledgementRollsBack(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	journal := testJournal("run-terminal-command-invalid", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	before, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	pendingAt := time.Date(2026, 9, 20, 1, 2, 3, 0, time.UTC)
	transition := protocol.StateTransitionRequest{TransitionID: "failed-1", State: "failed", Payload: json.RawMessage(`{}`)}
	acknowledgement := protocol.CommandAcknowledgement{RunID: journal.RunID, CommandID: "cancel-1", Outcome: "rejected"}

	if _, err := store.QueueTerminalTransitionAndAcknowledgementAt(journal.Key(), transition, acknowledgement, pendingAt); err == nil {
		t.Fatal("atomic terminal queue accepted an invalid acknowledgement")
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, before) {
		t.Fatalf("failed atomic terminal queue mutated journal:\n got: %#v\nwant: %#v", loaded, before)
	}
}

func TestQueueCancelledTransitionAndAcknowledgementRejectsNonCancelledState(t *testing.T) {
	t.Parallel()
	store := mustStore(t)
	journal := testJournal("run-cancel-wrapper", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	before, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	pendingAt := time.Date(2026, 9, 20, 1, 2, 3, 0, time.UTC)
	transition := protocol.StateTransitionRequest{TransitionID: "failed-1", State: "failed", Payload: json.RawMessage(`{}`)}
	acknowledgement := protocol.CommandAcknowledgement{RunID: journal.RunID, CommandID: "cancel-1", Outcome: "rejected", AckID: "ack-1"}

	if _, err := store.QueueCancelledTransitionAndAcknowledgementAt(journal.Key(), transition, acknowledgement, pendingAt); err == nil {
		t.Fatal("cancelled terminal queue accepted a non-cancelled transition")
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, before) {
		t.Fatalf("rejected cancelled wrapper mutated journal:\n got: %#v\nwant: %#v", loaded, before)
	}
}

func TestQueueWaitingForInputCoalescesEventsAndPendingTransition(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	for _, terminalState := range []string{"completed", "failed", "cancelled"} {
		t.Run(terminalState, func(t *testing.T) {
			t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestTerminalPendingTimestampSurvivesRestartAndRejectsCancelReplacement(t *testing.T) {
	t.Parallel()
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
	if _, err = store.QueueTerminalTransitionAt(journal.Key(), protocol.StateTransitionRequest{TransitionID: "cancelled-1", State: "cancelled", Payload: json.RawMessage(`{}`)}, cancelledAt); err == nil {
		t.Fatal("QueueTerminalTransitionAt(cancelled) replaced the first terminal")
	}
	queued, err = store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if queued.LocalState != "terminal_pending" || !queued.TerminalPendingAt.Equal(enteredAt) || queued.TerminalState != "completed" || queued.TerminalVerdict != "" || !queued.TerminalResolvedAt.IsZero() || !equalStrings(transitionStates(queued.PendingTransitions), []string{"completed"}) {
		t.Fatalf("terminal journal after rejected cancellation = %#v", queued)
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
	if !loaded.TerminalPendingAt.Equal(enteredAt) || loaded.TerminalState != "completed" || loaded.TerminalVerdict != "" {
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

func TestQueueRunningTransitionIsAtomic(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
