package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAutonomousSettings(t *testing.T) {
	config, err := autonomousSettings(json.RawMessage(`null`), func(string) string { return "" })
	if err != nil || config != (autonomousConfig{Steps: 120, StepMS: 250}) {
		t.Fatalf("default settings = %#v, %v", config, err)
	}
	environment := map[string]string{"SYMMETRY_FAKE_AGENT_STEPS": "30", "SYMMETRY_FAKE_AGENT_STEP_MS": "100", "SYMMETRY_FAKE_AGENT_DECISION_AT": "5"}
	config, err = autonomousSettings(json.RawMessage(`{"step_ms":0}`), func(key string) string { return environment[key] })
	if err != nil || config != (autonomousConfig{Steps: 30, StepMS: 0, DecisionAt: 5}) {
		t.Fatalf("overridden settings = %#v, %v", config, err)
	}
	for _, input := range []string{`{"steps":0}`, `{"steps":10001}`, `{"step_ms":-1}`, `{"step_ms":null}`, `{"steps":1,"decision_at":2}`, `{"steps":1.5}`} {
		if _, err := autonomousSettings(json.RawMessage(input), func(string) string { return "" }); err == nil {
			t.Errorf("accepted invalid settings %s", input)
		}
	}
}

func TestAutonomousAppliesQueuedControlsAfterOperationAndBeforeNextStep(t *testing.T) {
	inputs := make(chan autonomousInput, 4)
	var output bytes.Buffer
	operations := 0
	err := executeAutonomous(autonomousConfig{Steps: 2}, "Retain the complete goal", t.TempDir(), inputs, &output, func(time.Duration) {
		operations++
		if operations == 1 {
			inputs <- autonomousControl(1, "pause", "Retain the complete goal", `{}`)
			inputs <- autonomousControl(2, "guidance", "Retain the complete goal", `{"message":"Use the existing adapter"}`)
			inputs <- autonomousControl(3, "resume", "Retain the complete goal", `{}`)
			// A command queued inside this atomic operation cannot be acknowledged yet.
			if strings.Contains(output.String(), "command_applied") {
				t.Fatal("control applied inside the atomic operation")
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	text := output.String()
	var order []string
	for _, record := range autonomousRecords(t, text) {
		switch record["type"] {
		case "operation_started", "operation_completed":
			order = append(order, fmt.Sprintf("%s:%v", record["type"], record["step"]))
		case "command_applied":
			order = append(order, record["kind"].(string)+":"+record["outcome"].(string))
		}
	}
	want := "operation_started:1,operation_completed:1,pause:applied,guidance:applied,resume:applied,operation_started:2,operation_completed:2"
	if strings.Join(order, ",") != want {
		t.Fatalf("execution order = %v, want %s", order, want)
	}
}

func TestAutonomousPauseRetainsArtifactsAndDoesNotStartNextOperation(t *testing.T) {
	inputs := make(chan autonomousInput, 3)
	var output bytes.Buffer
	directory := t.TempDir()
	operations := 0
	err := executeAutonomous(autonomousConfig{Steps: 3}, "goal", directory, inputs, &output, func(time.Duration) {
		operations++
		if operations > 1 {
			t.Fatal("started another operation without resume")
		}
		inputs <- autonomousControl(1, "pause", "goal", `{}`)
		inputs <- autonomousControl(2, "guidance", "goal", `{"message":"Preserve progress"}`)
		close(inputs)
	})
	if err == nil || !strings.Contains(err.Error(), "paused or waiting") {
		t.Fatalf("closed paused input = %v", err)
	}
	if operations != 1 || !strings.Contains(output.String(), `"kind":"guidance","outcome":"applied"`) {
		t.Fatalf("paused execution did not process guidance: %s", output.String())
	}
	result := autonomousResult(t, directory)
	if result["completed_steps"] != float64(1) {
		t.Fatalf("paused artifacts = %#v", result)
	}
}

func TestAutonomousGuidanceAndDecisionReachVerifiedArtifacts(t *testing.T) {
	inputs := make(chan autonomousInput, 4)
	directory := t.TempDir()
	inputs <- autonomousControl(1, "guidance", "Full goal\nwith another line", `{"message":"Use staged output"}`)
	var output bytes.Buffer
	writer := autonomousOutputFunc(func(data []byte) (int, error) {
		if bytes.Contains(data, []byte(`"type":"waiting_for_input"`)) {
			inputs <- autonomousInput{line: []byte(`{"type":"provide_input","goal":"Full goal\nwith another line","input":{"option_id":"staged"}}`)}
		}
		return output.Write(data)
	})
	if err := executeAutonomous(autonomousConfig{Steps: 3, DecisionAt: 1}, "Full goal\nwith another line", directory, inputs, writer, func(time.Duration) {}); err != nil {
		t.Fatal(err)
	}
	result := autonomousResult(t, directory)
	if result["goal"] != "Full goal\nwith another line" || result["completed_steps"] != float64(3) || result["decision"] != "staged" {
		t.Fatalf("result = %#v", result)
	}
	if guidance := result["guidance"].([]any); len(guidance) != 1 || guidance[0] != "Use staged output" {
		t.Fatalf("guidance = %#v", guidance)
	}
	progress, err := os.ReadFile(filepath.Join(directory, "progress.md"))
	if err != nil || !bytes.Contains(progress, []byte("Use staged output")) {
		t.Fatalf("progress = %s, %v", progress, err)
	}
	types := make(map[string]bool)
	for _, record := range autonomousRecords(t, output.String()) {
		types[record["type"].(string)] = true
		if record["type"] == "waiting_for_input" {
			decision := record["decision"].(map[string]any)
			if decision["reason"] != "product_change" || decision["recommended_option_id"] != "staged" || len(decision["options"].([]any)) != 2 {
				t.Fatalf("decision = %#v", decision)
			}
		}
		if record["type"] == "command_applied" && record["kind"] == "provide_input" {
			t.Fatal("legacy decision reply emitted a supervisory receipt")
		}
	}
	for _, kind := range []string{"progress", "finding", "artifact", "test", "summary", "waiting_for_input"} {
		if !types[kind] {
			t.Errorf("missing semantic event %s", kind)
		}
	}
}

func TestAutonomousControlValidationAndReplay(t *testing.T) {
	state := autonomousState{goal: "goal", commands: make(map[string]autonomousReceipt)}
	var output bytes.Buffer
	for _, command := range []autonomousInput{
		autonomousControl(1, "guidance", "wrong goal", `{"message":"Do not use"}`),
		autonomousControl(2, "resume", "goal", `{}`),
		autonomousControl(3, "pause", "goal", `{"unexpected":true}`),
		autonomousControl(4, "guidance", "goal", `{"message":"Use once"}`),
		autonomousControl(4, "guidance", "goal", `{"message":"Use once"}`),
		autonomousControl(4, "guidance", "goal", `{"message":"Conflicting retry"}`),
	} {
		if err := state.apply(command.line, &output); err != nil {
			t.Fatal(err)
		}
	}
	if state.paused || len(state.guidance) != 1 || state.guidance[0] != "Use once" {
		t.Fatalf("invalid/replayed commands changed state: %#v", state)
	}
	var outcomes []string
	for _, record := range autonomousRecords(t, output.String()) {
		outcomes = append(outcomes, record["outcome"].(string))
	}
	if strings.Join(outcomes, ",") != "rejected,rejected,rejected,applied,applied,rejected" {
		t.Fatalf("outcomes = %v", outcomes)
	}
	if err := state.apply([]byte(`{"type":"pause","goal":"goal","input":{}}`), &output); err == nil {
		t.Fatal("accepted supervisory control without command ID")
	}
}

func TestAutonomousDecisionRejectsUnknownOptionAndWrongGoal(t *testing.T) {
	for _, line := range []string{
		`{"type":"provide_input","goal":"goal","input":{"option_id":"unknown"}}`,
		`{"type":"provide_input","goal":"wrong goal","input":{"option_id":"staged"}}`,
	} {
		state := autonomousState{goal: "goal", waiting: true, commands: make(map[string]autonomousReceipt)}
		var output bytes.Buffer
		if err := state.apply([]byte(line), &output); err == nil || !state.waiting {
			t.Fatalf("invalid decision released waiting state: %#v, %v", state, err)
		}
	}
}

func TestAutonomousArtifactFailureDoesNotReportSuccess(t *testing.T) {
	var output bytes.Buffer
	err := executeAutonomous(autonomousConfig{Steps: 1}, "goal", filepath.Join(t.TempDir(), "missing"), nil, &output, func(time.Duration) {})
	if err == nil || strings.Contains(output.String(), `"status":"passed"`) || strings.Contains(output.String(), `"type":"summary"`) {
		t.Fatalf("artifact failure was reported as success: %v, %s", err, output.String())
	}
}

func TestAutonomousOutputFailureStopsExecution(t *testing.T) {
	want := errors.New("output closed")
	err := executeAutonomous(autonomousConfig{Steps: 1}, "goal", t.TempDir(), nil, autonomousOutputFunc(func([]byte) (int, error) { return 0, want }), func(time.Duration) { t.Fatal("operation started after output failure") })
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
}

func autonomousControl(number int, kind, goal, payload string) autonomousInput {
	encodedGoal, _ := json.Marshal(goal)
	return autonomousInput{line: []byte(fmt.Sprintf(`{"type":%q,"command_id":"00000000-0000-4000-8000-%012d","goal":%s,"input":%s}`, kind, number, encodedGoal, payload))}
}

func autonomousRecords(t *testing.T, output string) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode event %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func autonomousResult(t *testing.T, directory string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(directory, "result.json"))
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

type autonomousOutputFunc func([]byte) (int, error)

func (write autonomousOutputFunc) Write(data []byte) (int, error) { return write(data) }
