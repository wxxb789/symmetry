package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

func TestWaitingDecisionRequirementIsBoundToWorkAndPrecedesPersistence(t *testing.T) {
	for _, test := range []struct {
		name     string
		required bool
		decision string
		valid    bool
	}{
		{name: "required packet missing", required: true},
		{name: "required packet malformed", required: true, decision: `,"decision":{"reason":"routine"}`},
		{name: "required packet valid", required: true, decision: decisionRequirementJSON(), valid: true},
		{name: "legacy question on supervisory runtime", valid: true},
		{name: "legacy malformed packet", decision: `,"decision":null`},
		{name: "legacy valid packet", decision: decisionRequirementJSON(), valid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, key, _ := supervisoryDaemon(t)
			journal := supervisoryJournal(t, app, key)
			journal.Work.RequiredCapabilities = map[string]bool{"supervisory_control": test.required}
			if err := app.store.SaveJournal(journal); err != nil {
				t.Fatal(err)
			}
			before := supervisoryJournal(t, app, key)
			data := `{"type":"waiting_for_input","question":"Which delivery strategy?"` + test.decision + "}\n"
			err := app.queueOutput(key, config.EventFormatJSONL, &jsonlParser{}, execution.Event{Stream: execution.Stdout, At: time.Now().UTC(), Data: []byte(data)})
			if (err == nil) != test.valid {
				t.Fatalf("queue waiting output = %v, want valid %v", err, test.valid)
			}
			after := supervisoryJournal(t, app, key)
			if !test.valid {
				if errors.Is(err, errRequiredDecisionPacket) != test.required {
					t.Fatalf("fatal classification changed for required=%t: %v", test.required, err)
				}
				if !strings.Contains(err.Error(), "decision packet") || !reflect.DeepEqual(before, after) {
					t.Fatalf("invalid packet mutated journal: error=%v, before=%#v, after=%#v", err, before, after)
				}
			} else if after.LocalState != "waiting_for_input" || len(after.PendingEvents) != 1 || len(after.PendingTransitions) != 1 {
				t.Fatalf("valid wait was not persisted: %#v", after)
			}
		})
	}
}

func TestWaitingDecisionPreservesMoreThanTenOptions(t *testing.T) {
	for _, test := range []struct {
		name     string
		required bool
	}{
		{name: "required packet", required: true},
		{name: "optional packet"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, key, process := supervisoryDaemon(t)
			journal := supervisoryJournal(t, app, key)
			journal.Work.RequiredCapabilities = map[string]bool{"supervisory_control": test.required}
			if err := app.store.SaveJournal(journal); err != nil {
				t.Fatal(err)
			}
			options := make([]any, 11)
			for i := range options {
				options[i] = map[string]any{
					"id":          fmt.Sprintf("rollout-%d", i+1),
					"label":       fmt.Sprintf("Roll out to %d regions", i+1),
					"consequence": fmt.Sprintf("Expose %d regions to the release before expanding.", i+1),
				}
			}
			packet := map[string]any{
				"type":     "waiting_for_input",
				"question": "How broadly should the release roll out?",
				"decision": map[string]any{
					"reason":                "product_change",
					"context":               "Choose the initial release footprint.",
					"options":               options,
					"recommended_option_id": "rollout-11",
				},
			}
			data, err := json.Marshal(packet)
			if err != nil {
				t.Fatal(err)
			}
			if err := app.queueOutput(key, config.EventFormatJSONL, &jsonlParser{}, execution.Event{
				Stream: execution.Stdout, At: time.Now().UTC(), Data: append(data, '\n'),
			}); err != nil {
				t.Fatalf("valid eleven-option packet was rejected: %v", err)
			}
			after := supervisoryJournal(t, app, key)
			if after.LocalState != "waiting_for_input" || after.TerminalState != "" || len(after.PendingEvents) != 1 || len(after.PendingTransitions) != 1 {
				t.Fatalf("valid decision did not persist a nonterminal wait: %#v", after)
			}
			if after.PendingEvents[0].Kind != "waiting_for_input" || after.PendingTransitions[0].State != "waiting_for_input" {
				t.Fatalf("decision did not retain its event and transition kinds: %#v", after)
			}
			for _, payload := range []json.RawMessage{after.PendingEvents[0].Payload, after.PendingTransitions[0].Payload} {
				var persisted map[string]any
				if err := json.Unmarshal(payload, &persisted); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(persisted, packet) {
					t.Fatalf("persisted decision changed options or recommendation: %#v", persisted)
				}
			}
			active := app.runningRun(key)
			if active == nil || active.process != process || active.cancelled || active.terminal || process.terminations != 0 {
				t.Fatal("valid decision terminated the active process")
			}
		})
	}
}

func TestRequiredCapabilitiesSurviveClaimPersistenceAndRestart(t *testing.T) {
	directory := t.TempDir()
	store, key := claimedStoreAt(t, directory)
	journal, err := store.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	var work protocol.Work
	if err := json.Unmarshal([]byte(`{"goal":"preserve the complete goal","agent_profile":"supervised","workspace":"local","input":{},"required_capabilities":{"supervisory_control":true,"future_capability":false}}`), &work); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveClaimGrant(key, protocol.ClaimResponse{RunID: key.RunID, Generation: key.Generation, ClaimID: journal.ClaimID, LeaseToken: journal.LeaseToken, LeaseExpiresAt: journal.LeaseExpiresAt, Work: work}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	restarted, err := state.New(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	loaded, err := restarted.LoadJournal(key)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Work.Goal != work.Goal || !reflect.DeepEqual(loaded.Work.RequiredCapabilities, work.RequiredCapabilities) {
		t.Fatalf("persisted work lost requirements: %#v", loaded.Work)
	}
	app := &daemon{store: restarted}
	if err := app.validateWaitingPacket(key, map[string]any{"question": "Which strategy?"}); err == nil {
		t.Fatal("restart lost the per-work decision requirement")
	}
}

func decisionRequirementJSON() string {
	return `,"decision":{"reason":"product_change","context":"Choose artifact delivery behavior.","recommended_option_id":"staged","options":[{"id":"staged","label":"Stage delivery","consequence":"Finish local artifacts."},{"id":"defer","label":"Defer","consequence":"Preserve partial artifacts."}]}`
}

func TestMissingRequiredDecisionFailsClearlyWithoutPoisoningOutput(t *testing.T) {
	app, key, _ := supervisoryDaemon(t)
	journal := supervisoryJournal(t, app, key)
	journal.Work.RequiredCapabilities = map[string]bool{"supervisory_control": true}
	if err := app.store.SaveJournal(journal); err != nil {
		t.Fatal(err)
	}
	sinkErr := app.queueOutput(key, config.EventFormatJSONL, &jsonlParser{}, execution.Event{
		Stream: execution.Stdout, At: time.Now().UTC(), Data: []byte("{\"type\":\"waiting_for_input\",\"question\":\"Choose?\"}\n"),
	})
	if sinkErr == nil {
		t.Fatal("required packet was accepted without a decision")
	}
	app.running[key].process = fakeProcess{result: execution.Result{SinkError: sinkErr}}
	app.waitForRun(key)
	failed := supervisoryJournal(t, app, key)
	if failed.TerminalState != "failed" || len(failed.PendingEvents) != 0 || len(failed.PendingTransitions) != 1 {
		t.Fatalf("invalid waiting packet poisoned failure delivery: %#v", failed)
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(failed.PendingTransitions[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload.Error, "decision packet") {
		t.Fatalf("failure omitted the packet requirement: %#v", payload)
	}
}

func TestFatalDecisionCancellationCauseSurvivesMissingRunnerSinkError(t *testing.T) {
	app, key, _ := supervisoryDaemon(t)
	process := newTerminatingInputProcess()
	app.running[key].process = process
	executionContext, stop := context.WithCancelCause(context.Background())
	defer stop(nil)
	output := newAgentOutput(config.EventFormatJSONL, "")
	output.executionContext = executionContext
	app.running[key].output = output
	finished := make(chan struct{})
	go func() {
		app.waitForRun(key)
		close(finished)
	}()
	stop(errRequiredDecisionPacket)
	if err := process.Terminate(context.Background(), 0); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("stopped process did not settle")
	}
	failed := supervisoryJournal(t, app, key)
	if failed.TerminalState != "failed" || len(failed.PendingTransitions) != 1 {
		t.Fatalf("fatal cancellation did not produce failure: %#v", failed)
	}
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(failed.PendingTransitions[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Error != errRequiredDecisionPacket.Error() {
		t.Fatalf("runner cancellation erased the packet cause: %q", payload.Error)
	}
}
