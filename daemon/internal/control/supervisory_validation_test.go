package control

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestSupervisoryCommandRequestAndDispatchPayloads(t *testing.T) {
	for _, test := range []struct {
		name    string
		kind    string
		payload string
		valid   bool
	}{
		{"guidance", "guidance", `{"message":"Use the existing adapter"}`, true},
		{"pause", "pause", `{}`, true},
		{"resume", "resume", `{}`, true},
		{"empty guidance", "guidance", `{"message":"  "}`, false},
		{"missing guidance", "guidance", `{}`, false},
		{"null guidance", "guidance", `{"message":null}`, false},
		{"number guidance", "guidance", `{"message":42}`, false},
		{"unknown guidance field", "guidance", `{"message":"use adapter","extra":true}`, false},
		{"oversized guidance", "guidance", `{"message":"` + strings.Repeat("x", 32769) + `"}`, false},
		{"boundary guidance", "guidance", `{"message":"` + strings.Repeat("x", 32768) + `"}`, true},
		{"pause payload", "pause", `{"message":"stop"}`, false},
		{"resume payload", "resume", `{"step":1}`, false},
		{"missing pause payload", "pause", ``, false},
		{"null resume payload", "resume", `null`, false},
		{"array payload", "pause", `[]`, false},
		{"trailing object", "pause", `{} {}`, false},
		{"unknown kind", "teleport", `{}`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := protocol.TaskCommandRequest{Kind: test.kind, Payload: json.RawMessage(test.payload), Generation: 1}
			if err := validateTaskCommandRequest(request); (err == nil) != test.valid {
				t.Errorf("request validation = %v, want valid %v", err, test.valid)
			}
			command := protocol.Command{CommandID: "command-1", RunID: "run-1", Generation: 1, Kind: test.kind, Payload: request.Payload, IssuedAt: time.Now()}
			if err := validateCommands("snapshot", []protocol.Command{command}); (err == nil) != test.valid {
				t.Errorf("dispatch validation = %v, want valid %v", err, test.valid)
			}
		})
	}
	if err := validateTaskCommandRequest(protocol.TaskCommandRequest{Kind: "pause", Payload: json.RawMessage(`{}`)}); err == nil {
		t.Fatal("accepted supervisory request without current generation")
	}
	for _, request := range []protocol.TaskCommandRequest{
		{Kind: "cancel"},
		{Kind: "provide_input", Payload: json.RawMessage(`{}`)},
	} {
		if err := validateTaskCommandRequest(request); err != nil {
			t.Errorf("legacy request rejected: %v", err)
		}
	}
}

func TestSupervisoryCommandResourceStates(t *testing.T) {
	stamp := "2026-09-06T00:00:01Z"
	for _, kind := range []string{"guidance", "pause", "resume", "provide_input"} {
		for _, test := range []struct {
			name   string
			fields map[string]any
			valid  bool
		}{
			{"pending", nil, true},
			{"applied", map[string]any{"state": "applied", "applied_at": stamp}, true},
			{"acknowledged applied", map[string]any{"state": "acknowledged", "applied_at": stamp, "acknowledgement_id": "ack-1", "acknowledgement_outcome": "applied", "acknowledged_at": stamp}, true},
			{"acknowledged without retained application", map[string]any{"state": "acknowledged", "acknowledgement_id": "ack-1", "acknowledgement_outcome": "applied", "acknowledged_at": stamp}, true},
			{"server rejected", map[string]any{"state": "acknowledged", "acknowledgement_outcome": "rejected", "acknowledged_at": stamp}, true},
			{"runless pending", map[string]any{"run_id": nil, "generation": nil}, false},
			{"runless applied", map[string]any{"run_id": nil, "generation": nil, "state": "applied", "applied_at": stamp}, false},
			{"pending application", map[string]any{"applied_at": stamp}, false},
			{"applied without timestamp", map[string]any{"state": "applied"}, false},
			{"applied zero timestamp", map[string]any{"state": "applied", "applied_at": "0001-01-01T00:00:00Z"}, false},
			{"applied with ack", map[string]any{"state": "applied", "applied_at": stamp, "acknowledgement_id": "ack-1", "acknowledgement_outcome": "applied", "acknowledged_at": stamp}, false},
			{"rejected with application", map[string]any{"state": "acknowledged", "applied_at": stamp, "acknowledgement_id": "ack-1", "acknowledgement_outcome": "rejected", "acknowledged_at": stamp}, false},
			{"server applied without ack ID", map[string]any{"state": "acknowledged", "acknowledgement_outcome": "applied", "acknowledged_at": stamp}, false},
			{"server failed without ack ID", map[string]any{"state": "acknowledged", "acknowledgement_outcome": "failed", "acknowledged_at": stamp}, false},
			{"server rejected without time", map[string]any{"state": "acknowledged", "acknowledgement_outcome": "rejected"}, false},
			{"server rejected zero time", map[string]any{"state": "acknowledged", "acknowledgement_outcome": "rejected", "acknowledged_at": "0001-01-01T00:00:00Z"}, false},
			{"missing ack fields", map[string]any{"state": "acknowledged"}, false},
		} {
			t.Run(kind+"/"+test.name, func(t *testing.T) {
				command := supervisoryResource(t, kind, test.fields)
				if err := validateTaskCommandResource("task-1", command); (err == nil) != test.valid {
					t.Fatalf("resource validation = %v, want valid %v", err, test.valid)
				}
			})
		}
	}
}

func TestServerRejectedInputHistoryDoesNotProveAcknowledgement(t *testing.T) {
	for _, kind := range []string{"guidance", "pause", "resume", "provide_input"} {
		t.Run(kind, func(t *testing.T) {
			command := supervisoryResource(t, kind, map[string]any{
				"state": "acknowledged", "acknowledgement_outcome": "rejected", "acknowledged_at": "2026-09-06T00:00:01Z",
			})
			if err := validateTaskCommandResource("task-1", command); err != nil {
				t.Fatalf("valid cancellation history rejected: %v", err)
			}
			request := protocol.CommandAcknowledgement{Fence: protocol.Fence{Generation: 1}, RunID: "run-1", AckID: "ack-1", Outcome: "rejected"}
			if err := validateAcknowledgementResponse("command-1", request, command); err == nil || !strings.Contains(err.Error(), "acknowledgement_id") {
				t.Fatalf("server rejection accepted as matching daemon ack: %v", err)
			}
		})
	}
	for _, kind := range []string{"cancel", "retry"} {
		command := supervisoryResource(t, kind, map[string]any{
			"state": "acknowledged", "acknowledgement_outcome": "rejected", "acknowledged_at": "2026-09-06T00:00:01Z",
		})
		if err := validateTaskCommandResource("task-1", command); err == nil {
			t.Errorf("legacy %s accepted partial acknowledgement", kind)
		}
	}
}

func TestSupervisoryResponseRequiresRequestedGeneration(t *testing.T) {
	request := protocol.TaskCommandRequest{Kind: "pause", Payload: json.RawMessage(`{}`), Generation: 2}
	command := supervisoryResource(t, "pause", nil)
	if err := validateTaskCommandResponse("command", "task-1", request, command); err == nil || !strings.Contains(err.Error(), "generation") {
		t.Fatalf("accepted stale command generation: %v", err)
	}
	request.Generation = 1
	if err := validateTaskCommandResponse("command", "task-1", request, command); err != nil {
		t.Fatal(err)
	}
}

func TestPausedRunAndTaskValidation(t *testing.T) {
	var run protocol.Run
	if err := json.Unmarshal([]byte(runJSON("paused", `null`, `null`)), &run); err != nil {
		t.Fatal(err)
	}
	request := protocol.StateTransitionRequest{Fence: fence(), State: "paused", TransitionID: "transition-1", Payload: json.RawMessage(`{"command_id":"command-1"}`)}
	if err := validateTransitionResponse("run-1", request, run); err != nil {
		t.Fatalf("paused transition rejected: %v", err)
	}
	var task protocol.Task
	body := strings.Replace(strings.Replace(taskJSON(), `"state":"queued"`, `"state":"paused"`, 1), `"run_id":null`, `"run_id":"run-1"`, 1)
	if err := json.Unmarshal([]byte(body), &task); err != nil {
		t.Fatal(err)
	}
	if err := validateTaskResponse("task", "task-1", task); err != nil {
		t.Fatalf("paused task rejected: %v", err)
	}
	task.RunID = nil
	if err := validateTaskResponse("task", "task-1", task); err == nil {
		t.Fatal("paused task accepted without run")
	}
}

func supervisoryResource(t *testing.T, kind string, fields map[string]any) protocol.TaskCommand {
	t.Helper()
	var values map[string]any
	if err := json.Unmarshal([]byte(commandJSON()), &values); err != nil {
		t.Fatal(err)
	}
	values["kind"] = kind
	if kind == "guidance" {
		values["payload"] = map[string]any{"message": "Use the existing adapter"}
	}
	for key, value := range fields {
		values[key] = value
	}
	body, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	var command protocol.TaskCommand
	if err := json.Unmarshal(body, &command); err != nil {
		t.Fatal(err)
	}
	return command
}
