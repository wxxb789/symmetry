package state

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func controlIntent(commandID, kind string) ControlCommandIntent {
	return ControlCommandIntent{CommandID: commandID, Kind: kind, PayloadDigest: strings.Repeat("a", 64), TransitionID: "transition-" + commandID, AckID: "ack-" + commandID}
}

func controlReceipt(commandID, kind, outcome string) protocol.RunEvent {
	payload, _ := json.Marshal(map[string]string{"command_id": commandID, "kind": kind, "outcome": outcome})
	return protocol.RunEvent{EventID: "event-" + commandID, Kind: "command_applied", OccurredAt: time.Now().UTC(), Payload: payload}
}

func TestConclusiveCleanupRetainsArtifactsBeforeRetiringSupervisoryIntent(t *testing.T) {
	store, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	journal := testJournal("retention-fallback", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.PrepareControlCommand(journal.Key(), controlIntent("pause-1", "pause")); err != nil {
		t.Fatal(err)
	}
	failed, err := store.QueueTerminalTransition(journal.Key(), protocol.StateTransitionRequest{TransitionID: "failed-1", State: "failed", Payload: json.RawMessage(`{"error":"ownership lost"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if failed.RetainWorkspace {
		t.Fatal("fixture must model the missing retention write")
	}
	if _, err := store.ResolveTerminalForCleanup(journal.Key(), TerminalVerdictOwnershipLost, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LocalState != "cleanup_pending" || !loaded.RetainWorkspace || len(loaded.ControlCommandIntents) != 0 {
		t.Fatalf("retiring intents lost artifact retention: %#v", loaded)
	}
}

func TestControlCommandPauseResumeAndReplaySurviveRestart(t *testing.T) {
	directory := t.TempDir()
	store, err := New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	journal := testJournal("control-replay", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	pause := controlIntent("pause-1", "pause")
	prepared, created, err := store.PrepareControlCommand(journal.Key(), pause)
	if err != nil || !created || prepared.LocalState != "running" || len(prepared.PendingTransitions) != 0 || len(prepared.PendingCommandAcknowledgements) != 0 {
		t.Fatalf("prepare changed lifecycle: %#v, %t, %v", prepared, created, err)
	}
	if _, _, err := store.PrepareControlCommand(journal.Key(), controlIntent("pause-2", "pause")); err == nil {
		t.Fatal("accepted concurrent pause")
	}
	completed, err := store.CompleteControlCommand(journal.Key(), pause.CommandID, "pause", "applied", controlReceipt(pause.CommandID, "pause", "applied"))
	if err != nil || completed.LocalState != "paused" || len(completed.PendingEvents) != 1 || completed.PendingEvents[0].Sequence != journal.LastEventSequence+1 || len(completed.PendingTransitions) != 1 || completed.PendingTransitions[0].State != "paused" || len(completed.PendingCommandAcknowledgements) != 1 {
		t.Fatalf("pause incomplete: %#v, %v", completed, err)
	}
	if _, err := store.MarkCommandAcknowledgementsDelivered(journal.Key(), []string{pause.AckID}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = New(directory)
	if err != nil {
		t.Fatal(err)
	}
	loaded, created, err := store.PrepareControlCommand(journal.Key(), pause)
	if err != nil || created || !loaded.ControlCommandIntents[0].AcknowledgementDelivered {
		t.Fatalf("replay resent: %#v, %t, %v", loaded, created, err)
	}
	duplicate, err := store.CompleteControlCommand(journal.Key(), pause.CommandID, "pause", "applied", controlReceipt(pause.CommandID, "pause", "applied"))
	if err != nil || !reflect.DeepEqual(loaded, duplicate) {
		t.Fatalf("duplicate mutated: %#v, %v", duplicate, err)
	}
	changed := pause
	changed.PayloadDigest = strings.Repeat("b", 64)
	if _, _, err := store.PrepareControlCommand(journal.Key(), changed); err == nil {
		t.Fatal("accepted changed payload")
	}
	resume := controlIntent("resume-1", "resume")
	if _, created, err := store.PrepareControlCommand(journal.Key(), resume); err != nil || !created {
		t.Fatalf("prepare resume: %t, %v", created, err)
	}
	resumed, err := store.CompleteControlCommand(journal.Key(), resume.CommandID, "resume", "applied", controlReceipt(resume.CommandID, "resume", "applied"))
	if err != nil || resumed.LocalState != "running" || len(resumed.PendingTransitions) != 2 || resumed.PendingTransitions[1].State != "running" {
		t.Fatalf("resume incomplete: %#v, %v", resumed, err)
	}
}

func TestControlCommandCompletionIsAtomicAndMatchesIntent(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("control-atomic", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	intent := controlIntent("pause-1", "pause")
	prepared, _, err := store.PrepareControlCommand(journal.Key(), intent)
	if err != nil {
		t.Fatal(err)
	}
	invalid := controlReceipt(intent.CommandID, "pause", "applied")
	invalid.EventID = ""
	if _, err := store.CompleteControlCommand(journal.Key(), intent.CommandID, "pause", "applied", invalid); err == nil {
		t.Fatal("accepted invalid event")
	}
	if _, err := store.CompleteControlCommand(journal.Key(), "unknown", "pause", "applied", controlReceipt("unknown", "pause", "applied")); err == nil {
		t.Fatal("accepted unmatched command")
	}
	if _, err := store.CompleteControlCommand(journal.Key(), intent.CommandID, "guidance", "applied", controlReceipt(intent.CommandID, "guidance", "applied")); err == nil {
		t.Fatal("accepted mismatched kind")
	}
	loaded, err := store.LoadJournal(journal.Key())
	if err != nil || !reflect.DeepEqual(prepared, loaded) {
		t.Fatalf("invalid completion mutated state: %#v, %v", loaded, err)
	}
	failed, err := store.CompleteControlCommand(journal.Key(), intent.CommandID, "pause", "failed", controlReceipt(intent.CommandID, "pause", "failed"))
	if err != nil || failed.LocalState != "running" || len(failed.PendingTransitions) != 0 || failed.ControlCommandIntents[0].Outcome != "failed" || len(failed.PendingCommandAcknowledgements) != 1 {
		t.Fatalf("failure settlement: %#v, %v", failed, err)
	}
}

func TestControlCommandWaitingWinsAndPausedWaitingCannotResume(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("control-waiting", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	intent := controlIntent("pause-1", "pause")
	if _, _, err := store.PrepareControlCommand(journal.Key(), intent); err != nil {
		t.Fatal(err)
	}
	wait := protocol.RunEvent{EventID: "wait-1", Kind: "waiting_for_input", OccurredAt: time.Now().UTC(), Payload: json.RawMessage(`{"question":"Choose?"}`)}
	transition := protocol.StateTransitionRequest{TransitionID: "waiting-1", State: "waiting_for_input", Payload: wait.Payload}
	if _, err := store.QueueWaitingForInput(journal.Key(), wait, transition); err != nil {
		t.Fatal(err)
	}
	completed, err := store.CompleteControlCommand(journal.Key(), intent.CommandID, "pause", "applied", controlReceipt(intent.CommandID, "pause", "applied"))
	if err != nil || completed.LocalState != "waiting_for_input" || completed.ControlCommandIntents[0].Outcome != "rejected" || len(completed.PendingTransitions) != 1 || !strings.Contains(string(completed.PendingEvents[1].Payload), `"outcome":"rejected"`) {
		t.Fatalf("pause overrode waiting: %#v, %v", completed, err)
	}
	paused := testJournal("paused-waiting", 1)
	paused.LocalState = "paused"
	if err := store.SaveJournal(paused); err != nil {
		t.Fatal(err)
	}
	stillPaused, err := store.QueueWaitingForInput(paused.Key(), wait, transition)
	if err != nil || stillPaused.LocalState != "paused" || len(stillPaused.PendingTransitions) != 0 || len(stillPaused.PendingEvents) != 1 {
		t.Fatalf("waiting bypassed pause: %#v, %v", stillPaused, err)
	}
}

func TestControlCommandCancellationSettlesAndRetainsWorkspace(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("control-cancel", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	intent := controlIntent("pause-1", "pause")
	if _, _, err := store.PrepareControlCommand(journal.Key(), intent); err != nil {
		t.Fatal(err)
	}
	terminal, err := store.QueueTerminalTransition(journal.Key(), protocol.StateTransitionRequest{TransitionID: "cancelled-1", State: "cancelled", Payload: json.RawMessage(`{}`)})
	if err != nil || !terminal.RetainWorkspace || terminal.ControlCommandIntents[0].Outcome != "rejected" || len(terminal.PendingCommandAcknowledgements) != 1 {
		t.Fatalf("cancel settlement: %#v, %v", terminal, err)
	}
	late, err := store.CompleteControlCommand(journal.Key(), intent.CommandID, "pause", "applied", controlReceipt(intent.CommandID, "pause", "applied"))
	if err != nil || !reflect.DeepEqual(terminal, late) {
		t.Fatalf("late pause overrode cancellation: %#v, %v", late, err)
	}
	if _, err := store.ResolveTerminal(journal.Key(), TerminalVerdictAccepted, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkTransitionsDelivered(journal.Key(), []string{"cancelled-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.EnterCleanupPending(journal.Key()); err == nil {
		t.Fatal("cleanup discarded receipt")
	}
	if _, err := store.MarkCommandAcknowledgementsDelivered(journal.Key(), []string{intent.AckID}); err != nil {
		t.Fatal(err)
	}
	clean, err := store.EnterCleanupPending(journal.Key())
	if err != nil || !clean.RetainWorkspace || !clean.ControlCommandIntents[0].AcknowledgementDelivered {
		t.Fatalf("cleanup lost retained state: %#v, %v", clean, err)
	}
}

func TestControlCommandConclusiveCleanupRetiresUndeliverableIntents(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("control-retire", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.PrepareControlCommand(journal.Key(), controlIntent("guidance-1", "guidance")); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RetainWorkspace(journal.Key()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.QueueTerminalTransition(journal.Key(), protocol.StateTransitionRequest{TransitionID: "failed-1", State: "failed", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	clean, err := store.ResolveTerminalForCleanup(journal.Key(), TerminalVerdictOwnershipLost, time.Now().UTC())
	if err != nil || !clean.RetainWorkspace || len(clean.ControlCommandIntents) != 0 || len(clean.PendingCommandAcknowledgements) != 0 {
		t.Fatalf("conclusive cleanup: %#v, %v", clean, err)
	}
}

func TestControlCommandJournalRejectsForgedAcknowledgement(t *testing.T) {
	store := mustStore(t)
	journal := testJournal("control-validation", 1)
	if err := store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	intent := controlIntent("guidance-1", "guidance")
	if _, _, err := store.PrepareControlCommand(journal.Key(), intent); err != nil {
		t.Fatal(err)
	}
	completed, err := store.CompleteControlCommand(journal.Key(), intent.CommandID, "guidance", "applied", controlReceipt(intent.CommandID, "guidance", "applied"))
	if err != nil || completed.LocalState != "running" || len(completed.PendingTransitions) != 0 {
		t.Fatalf("guidance changed lifecycle: %#v, %v", completed, err)
	}
	completed.PendingCommandAcknowledgements[0].AckID = "forged"
	if err := store.SaveJournal(completed); err == nil {
		t.Fatal("accepted forged acknowledgement")
	}
}
