package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/control"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func supervisoryDaemon(t *testing.T) (*daemon, state.RunKey, *recordingProcess) {
	t.Helper()
	store, key := claimedStore(t)
	t.Cleanup(func() { _ = store.Close() })
	if _, err := store.SetLocalState(key, "running"); err != nil {
		t.Fatal(err)
	}
	process := &recordingProcess{}
	app := &daemon{
		config: config.Config{Runtime: config.Runtime{AgentProfile: "supervised"}, AgentProfiles: map[string]config.AgentProfile{"supervised": {SupervisoryControl: true, Interactive: true, InputMode: config.InputModeJSON, EventFormat: config.EventFormatJSONL}}},
		store:  store, options: options{newID: ids()}, control: &orderingControl{recordEvents: true}, workspace: &fakeWorkspace{},
		log:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		running: map[state.RunKey]*runningRun{key: {process: process, claimed: true, cleanupBlocked: true}},
	}
	return app, key, process
}

func supervisoryCommand(key state.RunKey, id, kind string) protocol.Command {
	payload := json.RawMessage(`{}`)
	if kind == "guidance" {
		payload = json.RawMessage(`{"message":"Use the existing adapter."}`)
	}
	return protocol.Command{CommandID: id, RunID: key.RunID, Generation: key.Generation, Kind: kind, Payload: payload}
}

func supervisoryReceipt(t *testing.T, app *daemon, key state.RunKey, id, kind, outcome string) {
	t.Helper()
	data, err := json.Marshal(map[string]string{"type": "command_applied", "command_id": id, "kind": kind, "outcome": outcome})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.queueOutput(key, config.EventFormatJSONL, &jsonlParser{}, execution.Event{Stream: execution.Stdout, At: time.Now().UTC(), Data: append(data, '\n')}); err != nil {
		t.Fatal(err)
	}
}

func supervisoryJournal(t *testing.T, app *daemon, key state.RunKey) state.RunJournal {
	t.Helper()
	journal, err := app.store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	return journal
}

func TestSupervisoryGuidanceWaitsForMatchingReceiptAndNeverResends(t *testing.T) {
	app, key, process := supervisoryDaemon(t)
	command := supervisoryCommand(key, "guidance-1", "guidance")
	process.beforeWrite = func() {
		journal := supervisoryJournal(t, app, key)
		if len(journal.ControlCommandIntents) != 1 || journal.ControlCommandIntents[0].CommandID != command.CommandID || journal.ControlCommandIntents[0].Outcome != "" || len(journal.PendingCommandAcknowledgements) != 0 {
			t.Fatalf("intent was not durable before stdin: %#v", journal)
		}
	}
	if !app.handleCommand(context.Background(), command) || !app.handleCommand(context.Background(), command) {
		t.Fatal("guidance not accepted")
	}
	journal := supervisoryJournal(t, app, key)
	if process.writes != 1 || journal.LocalState != "running" || len(journal.PendingTransitions) != 0 || len(journal.PendingCommandAcknowledgements) != 0 {
		t.Fatalf("stdin prematurely applied guidance: %#v, writes=%d", journal, process.writes)
	}
	var input protocol.AgentInputRecord
	if err := json.Unmarshal(process.input, &input); err != nil {
		t.Fatal(err)
	}
	if string(input.Type) != "guidance" || input.CommandID != command.CommandID || input.Goal != "g" || string(input.Input) != string(command.Payload) {
		t.Fatalf("guidance envelope = %#v", input)
	}
	supervisoryReceipt(t, app, key, "forged", "guidance", "applied")
	supervisoryReceipt(t, app, key, command.CommandID, "pause", "applied")
	journal = supervisoryJournal(t, app, key)
	if len(journal.PendingEvents) != 2 || journal.PendingEvents[0].Kind != "agent_event" || journal.PendingEvents[1].Kind != "agent_event" || len(journal.PendingCommandAcknowledgements) != 0 || journal.LocalState != "running" {
		t.Fatalf("forged receipt changed execution: %#v", journal)
	}
	supervisoryReceipt(t, app, key, command.CommandID, "guidance", "applied")
	journal = supervisoryJournal(t, app, key)
	if journal.ControlCommandIntents[0].Outcome != "applied" || len(journal.PendingCommandAcknowledgements) != 1 || len(journal.PendingTransitions) != 0 {
		t.Fatalf("receipt not durably applied: %#v", journal)
	}
	if _, err := app.store.MarkCommandAcknowledgementsDelivered(key, []string{journal.ControlCommandIntents[0].AckID}); err != nil {
		t.Fatal(err)
	}
	if !app.handleCommand(context.Background(), command) {
		t.Fatal("delivered replay rejected")
	}
	supervisoryReceipt(t, app, key, command.CommandID, "guidance", "applied")
	journal = supervisoryJournal(t, app, key)
	if process.writes != 1 || len(journal.PendingEvents) != 3 || len(journal.PendingCommandAcknowledgements) != 0 {
		t.Fatalf("replay repeated a side effect: %#v, writes=%d", journal, process.writes)
	}
}

func TestSupervisoryPauseResumeKeepsProcessCapacityAndLease(t *testing.T) {
	app, key, process := supervisoryDaemon(t)
	pause := supervisoryCommand(key, "pause-1", "pause")
	if !app.handleCommand(context.Background(), pause) {
		t.Fatal("pause not accepted")
	}
	if journal := supervisoryJournal(t, app, key); journal.LocalState != "running" || len(journal.PendingCommandAcknowledgements) != 0 {
		t.Fatalf("pause applied before safe boundary: %#v", journal)
	}
	supervisoryReceipt(t, app, key, pause.CommandID, "pause", "applied")
	paused := supervisoryJournal(t, app, key)
	if paused.LocalState != "paused" || len(paused.PendingTransitions) != 1 || paused.PendingTransitions[0].State != "paused" || len(paused.PendingCommandAcknowledgements) != 1 {
		t.Fatalf("pause not atomic: %#v", paused)
	}
	if app.runningRun(key).process != process || process.terminations != 0 || !app.renewalEligible(paused) || !app.renewalStillEligible(paused) {
		t.Fatal("paused worker lost process or lease eligibility")
	}
	runs := app.activeRuns()
	if len(runs) != 1 || runs[0].State != "paused" || runs[0].ClaimID != paused.ClaimID || runs[0].LeaseToken != paused.LeaseToken {
		t.Fatalf("paused capacity heartbeat = %#v", runs)
	}
	guidance := supervisoryCommand(key, "guidance-paused", "guidance")
	if !app.handleCommand(context.Background(), guidance) {
		t.Fatal("guidance to paused worker rejected")
	}
	supervisoryReceipt(t, app, key, guidance.CommandID, "guidance", "applied")
	if journal := supervisoryJournal(t, app, key); journal.LocalState != "paused" {
		t.Fatalf("guidance resumed worker: %#v", journal)
	}
	resume := supervisoryCommand(key, "resume-1", "resume")
	if !app.handleCommand(context.Background(), resume) {
		t.Fatal("resume not accepted")
	}
	if journal := supervisoryJournal(t, app, key); journal.LocalState != "paused" {
		t.Fatalf("resume applied before receipt: %#v", journal)
	}
	supervisoryReceipt(t, app, key, resume.CommandID, "resume", "applied")
	resumed := supervisoryJournal(t, app, key)
	if resumed.LocalState != "running" || len(resumed.PendingTransitions) != 2 || resumed.PendingTransitions[1].State != "running" || app.runningRun(key).process != process || process.terminations != 0 || process.writes != 3 {
		t.Fatalf("resume restarted execution: %#v, writes=%d", resumed, process.writes)
	}
}

func TestSupervisoryCapabilityRejectionDoesNotWriteInput(t *testing.T) {
	app, key, process := supervisoryDaemon(t)
	profile := app.config.AgentProfiles["supervised"]
	profile.SupervisoryControl = false
	app.config.AgentProfiles["supervised"] = profile
	if !app.handleCommand(context.Background(), supervisoryCommand(key, "pause-1", "pause")) {
		t.Fatal("unsupported command was not settled")
	}
	journal := supervisoryJournal(t, app, key)
	if process.writes != 0 || len(journal.ControlCommandIntents) != 0 || journal.LocalState != "running" || len(journal.PendingCommandAcknowledgements) != 1 || journal.PendingCommandAcknowledgements[0].Outcome != "rejected" {
		t.Fatalf("unsupported control reached agent: %#v", journal)
	}
}

func TestSupervisoryCancellationWinsPendingPauseAndRetainsWorkspace(t *testing.T) {
	app, key, process := supervisoryDaemon(t)
	if !app.handleCommand(context.Background(), supervisoryCommand(key, "pause-1", "pause")) {
		t.Fatal("pause not accepted")
	}
	if !app.handleCommand(context.Background(), supervisoryCommand(key, "cancel-1", "cancel")) {
		t.Fatal("cancel not accepted")
	}
	supervisoryReceipt(t, app, key, "pause-1", "pause", "applied")
	journal := supervisoryJournal(t, app, key)
	if process.terminations != 1 || !journal.RetainWorkspace || journal.TerminalState != "cancelled" || len(journal.PendingTransitions) != 1 || journal.PendingTransitions[0].State != "cancelled" || journal.ControlCommandIntents[0].Outcome != "rejected" || len(journal.PendingCommandAcknowledgements) != 2 {
		t.Fatalf("pause overrode cancel: %#v", journal)
	}
}

func TestSupervisoryRestartFailsPausedAndPendingControlsWithoutRelaunch(t *testing.T) {
	for _, paused := range []bool{false, true} {
		name := "pending_pause"
		if paused {
			name = "paused"
		}
		t.Run(name, func(t *testing.T) {
			app, key, process := supervisoryDaemon(t)
			if !app.handleCommand(context.Background(), supervisoryCommand(key, "pause-1", "pause")) {
				t.Fatal("pause not accepted")
			}
			if paused {
				supervisoryReceipt(t, app, key, "pause-1", "pause", "applied")
			}
			if _, err := app.store.SetProcessDetails(key, 71, "agent:71", time.Now().UTC()); err != nil {
				t.Fatal(err)
			}
			app.running = make(map[state.RunKey]*runningRun)
			terminated, starts := 0, 0
			app.options.terminatePersist = func(pid int, identity string) error {
				if pid != 71 || identity != "agent:71" {
					t.Fatalf("wrong recovered process: %d %q", pid, identity)
				}
				terminated++
				return nil
			}
			app.start = func(context.Context, execution.Invocation, execution.Sink) (Process, error) {
				starts++
				return process, nil
			}
			if err := app.recoverUnresolvedInputIntents(context.Background()); err != nil {
				t.Fatal(err)
			}
			journal := supervisoryJournal(t, app, key)
			if terminated != 1 || starts != 0 || process.writes != 1 || app.hasRun(key) || journal.LocalState != "terminal_pending" || journal.TerminalState != "failed" || !journal.RetainWorkspace || len(journal.PendingTransitions) != 1 || journal.PendingTransitions[0].State != "failed" {
				t.Fatalf("unsafe recovery: %#v, terminations=%d starts=%d writes=%d", journal, terminated, starts, process.writes)
			}
			if !paused && journal.ControlCommandIntents[0].Outcome != "rejected" {
				t.Fatalf("uncertain pause not rejected: %#v", journal)
			}
		})
	}
}

func TestSupervisoryAppliedGuidanceAcknowledgementPrecedesCompletion(t *testing.T) {
	app, key, _ := supervisoryDaemon(t)
	api := app.control.(*orderingControl)
	if !app.handleCommand(context.Background(), supervisoryCommand(key, "guidance-1", "guidance")) {
		t.Fatal("guidance not accepted")
	}
	supervisoryReceipt(t, app, key, "guidance-1", "guidance", "applied")
	if _, err := app.store.QueueTerminalTransition(key, protocol.StateTransitionRequest{TransitionID: "completed-1", State: "completed", Payload: json.RawMessage(`{}`)}); err != nil {
		t.Fatal(err)
	}
	if err := app.flushRun(context.Background(), supervisoryJournal(t, app, key)); err != nil {
		t.Fatal(err)
	}
	ack, completed := -1, -1
	for index, call := range api.calls {
		if call == "ack:guidance-1" {
			ack = index
		}
		if call == "transition:completed" {
			completed = index
		}
	}
	if ack < 0 || completed <= ack {
		t.Fatalf("completion preceded applied guidance receipt: %#v", api.calls)
	}
}

type supervisoryConflictControl struct{ orderingControl }

func (api *supervisoryConflictControl) AcknowledgeCommand(_ context.Context, commandID string, _ protocol.CommandAcknowledgement) error {
	api.calls = append(api.calls, "ack:"+commandID)
	if strings.HasPrefix(commandID, "pause-") {
		return &control.APIError{Code: control.StateConflict}
	}
	return nil
}

func TestSupervisoryCancelledAcknowledgementConflictRetiresAfterTerminalAcceptance(t *testing.T) {
	app, key, _ := supervisoryDaemon(t)
	app.cleanupWake = make(chan struct{}, 1)
	app.background = context.Background()
	api := &supervisoryConflictControl{}
	app.control = api
	if !app.handleCommand(context.Background(), supervisoryCommand(key, "pause-1", "pause")) || !app.handleCommand(context.Background(), supervisoryCommand(key, "cancel-1", "cancel")) {
		t.Fatal("commands not accepted")
	}
	if err := app.flushRun(context.Background(), supervisoryJournal(t, app, key)); err != nil {
		t.Fatal(err)
	}
	journal := supervisoryJournal(t, app, key)
	if journal.TerminalVerdict != state.TerminalVerdictAccepted || !journal.RetainWorkspace || len(journal.PendingCommandAcknowledgements) != 0 || !journal.ControlCommandIntents[0].AcknowledgementDelivered {
		t.Fatalf("invalidated control acknowledgement blocked cleanup: %#v, calls=%#v", journal, api.calls)
	}
}

func TestSupervisoryInitialInputDescribesAutonomyAndPreservesLegacyEnvelope(t *testing.T) {
	work := protocol.Work{Goal: "Preserve the complete original goal.", Input: json.RawMessage(`{"scope":"existing"}`)}
	for _, supervised := range []bool{false, true} {
		profile := config.AgentProfile{InputMode: config.InputModeJSON, Interactive: true, EventFormat: config.EventFormatJSONL, SupervisoryControl: supervised}
		data, err := initialInput(profile, work, nil, "")
		if err != nil {
			t.Fatal(err)
		}
		var input protocol.AgentInputRecord
		if err := json.Unmarshal(data, &input); err != nil {
			t.Fatal(err)
		}
		if input.Goal != work.Goal || string(input.Input) != string(work.Input) {
			t.Fatalf("task context changed: %#v", input)
		}
		if !supervised {
			if input.Autonomy != nil || strings.Contains(string(data), `"autonomy"`) {
				t.Fatalf("legacy envelope changed: %s", data)
			}
			continue
		}
		if input.Autonomy == nil || input.Autonomy.Mode != "high" || len(input.Autonomy.EscalationReasons) != 7 || !strings.Contains(input.Autonomy.ControlBoundary, "safe boundary") || !strings.Contains(input.Autonomy.Acknowledgement, "command_applied") || !strings.Contains(input.Autonomy.RoutineDecisions, "do not request routine approvals") {
			t.Fatalf("supervisory policy missing execution contract: %#v", input.Autonomy)
		}
	}
}

func TestSupervisoryMalformedDecisionPacketDoesNotCreateWaitingState(t *testing.T) {
	app, key, _ := supervisoryDaemon(t)
	packet := []byte("{\"type\":\"waiting_for_input\",\"question\":\"Choose?\",\"decision\":{\"reason\":\"irreversible\",\"context\":\"Remove a column\",\"options\":[]}}\n")
	err := app.queueOutput(key, config.EventFormatJSONL, &jsonlParser{}, execution.Event{Stream: execution.Stdout, At: time.Now().UTC(), Data: packet})
	if err == nil {
		t.Fatal("malformed decision packet accepted")
	}
	journal := supervisoryJournal(t, app, key)
	if journal.LocalState != "running" || len(journal.PendingTransitions) != 0 {
		t.Fatalf("malformed decision mutated lifecycle: %#v", journal)
	}
}

func TestDecisionPacketRecommendationAllowsOmittedOrNullAndBindsKnownOption(t *testing.T) {
	for _, test := range []struct {
		name  string
		value string
		valid bool
	}{
		{"omitted", "", true},
		{"null", `,"recommended_option_id":null`, true},
		{"known", `,"recommended_option_id":"staged"`, true},
		{"unknown", `,"recommended_option_id":"unknown"`, false},
		{"empty", `,"recommended_option_id":""`, false},
		{"wrong type", `,"recommended_option_id":42`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var packet map[string]any
			data := `{"question":"Choose a migration?","decision":{"reason":"irreversible","context":"The column will be removed","options":[{"id":"staged","label":"Stage","consequence":"Preserves rollback"},{"id":"defer","label":"Defer","consequence":"Leaves work blocked"}]` + test.value + `}}`
			if err := json.Unmarshal([]byte(data), &packet); err != nil {
				t.Fatal(err)
			}
			if got := validDecisionPacket(packet); got != test.valid {
				t.Fatalf("validDecisionPacket = %t, want %t", got, test.valid)
			}
		})
	}
}
