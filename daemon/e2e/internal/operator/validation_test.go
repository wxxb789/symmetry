package operator

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

func TestSupervisoryCommandRequestValidatesKindPayloadAndGeneration(t *testing.T) {
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

func TestPausedTaskValidation(t *testing.T) {
	var task Task
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
