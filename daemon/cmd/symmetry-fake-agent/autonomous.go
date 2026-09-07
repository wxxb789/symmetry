package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

type autonomousConfig struct {
	Steps      int
	StepMS     int
	DecisionAt int
}

type autonomousInput struct {
	line []byte
	err  error
}

type autonomousState struct {
	goal      string
	completed int
	guidance  []string
	paused    bool
	waiting   bool
	decision  string
	commands  map[string]autonomousReceipt
}

type autonomousReceipt struct {
	line    string
	outcome string
}

func autonomousSettings(input json.RawMessage, getenv func(string) string) (autonomousConfig, error) {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(input, &values); err != nil {
		return autonomousConfig{}, fmt.Errorf("decode autonomous input: %w", err)
	}
	config := autonomousConfig{Steps: 120, StepMS: 250}
	for _, field := range []struct {
		key   string
		env   string
		value *int
		min   int
		max   int
	}{
		{"steps", "SYMMETRY_FAKE_AGENT_STEPS", &config.Steps, 1, 10000},
		{"step_ms", "SYMMETRY_FAKE_AGENT_STEP_MS", &config.StepMS, 0, 60000},
		{"decision_at", "SYMMETRY_FAKE_AGENT_DECISION_AT", &config.DecisionAt, 0, 10000},
	} {
		if raw, exists := values[field.key]; exists {
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || json.Unmarshal(raw, field.value) != nil {
				return autonomousConfig{}, fmt.Errorf("%s must be an integer", field.key)
			}
		} else if value := getenv(field.env); value != "" {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return autonomousConfig{}, fmt.Errorf("%s must be an integer", field.env)
			}
			*field.value = parsed
		}
		if *field.value < field.min || *field.value > field.max {
			return autonomousConfig{}, fmt.Errorf("%s must be between %d and %d", field.key, field.min, field.max)
		}
	}
	if config.DecisionAt > config.Steps {
		return autonomousConfig{}, errors.New("decision_at must not exceed steps")
	}
	return config, nil
}

func runAutonomous(reader *bufio.Reader, output io.Writer, envelope protocol.AgentInputRecord) error {
	config, err := autonomousSettings(envelope.Input, os.Getenv)
	if err != nil {
		return err
	}
	inputs := make(chan autonomousInput, 64)
	done := make(chan struct{})
	defer close(done)
	go func() {
		defer close(inputs)
		for {
			line, err := readLine(reader)
			select {
			case inputs <- autonomousInput{line: line, err: err}:
			case <-done:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	return executeAutonomous(config, envelope.Goal, ".", inputs, output, time.Sleep)
}

// Only this loop writes output or artifacts. The reader can queue controls during
// an operation, but the boundary applies them after that operation completes.
func executeAutonomous(config autonomousConfig, goal, directory string, inputs <-chan autonomousInput, output io.Writer, operation func(time.Duration)) error {
	state := autonomousState{goal: goal, commands: make(map[string]autonomousReceipt)}
	if err := writeRecord(output, map[string]any{
		"type": "progress", "schema_version": semanticSchemaVersion,
		"message": "Autonomous artifact generation started", "total_steps": config.Steps,
	}); err != nil {
		return err
	}
	for step := 1; step <= config.Steps; step++ {
		if err := state.boundary(&inputs, output); err != nil {
			return err
		}
		if err := writeRecord(output, map[string]any{"type": "operation_started", "step": step}); err != nil {
			return err
		}
		operation(time.Duration(config.StepMS) * time.Millisecond)
		state.completed = step
		if err := state.writeArtifacts(directory, config.Steps); err != nil {
			return err
		}
		if err := writeRecord(output, map[string]any{"type": "operation_completed", "step": step}); err != nil {
			return err
		}
		if err := writeRecord(output, map[string]any{
			"type": "progress", "schema_version": semanticSchemaVersion,
			"message": fmt.Sprintf("Generated step %d of %d", step, config.Steps), "step": step,
		}); err != nil {
			return err
		}
		// A queued pause takes precedence over starting a decision or another step.
		if err := state.boundary(&inputs, output); err != nil {
			return err
		}
		if step == config.DecisionAt {
			state.waiting = true
			if err := writeRecord(output, autonomousDecision()); err != nil {
				return err
			}
			if err := state.boundary(&inputs, output); err != nil {
				return err
			}
		}
	}
	if err := state.writeArtifacts(directory, config.Steps); err != nil {
		return err
	}
	for _, record := range []map[string]any{
		{"type": "finding", "schema_version": semanticSchemaVersion, "message": "Generated local artifacts from the complete goal and applied guidance", "severity": "info"},
		{"type": "artifact", "schema_version": semanticSchemaVersion, "path": "progress.md", "kind": "file"},
		{"type": "artifact", "schema_version": semanticSchemaVersion, "path": "result.json", "kind": "file"},
		{"type": "test", "schema_version": semanticSchemaVersion, "name": "Autonomous artifact round-trip", "status": "passed"},
		{"type": "summary", "schema_version": semanticSchemaVersion, "summary": fmt.Sprintf("Completed %d autonomous steps and preserved progress.md and result.json", config.Steps)},
	} {
		if err := writeRecord(output, record); err != nil {
			return err
		}
	}
	return nil
}

func (state *autonomousState) boundary(inputs *<-chan autonomousInput, output io.Writer) error {
	for {
		var next autonomousInput
		var open bool
		if state.paused || state.waiting {
			if *inputs == nil {
				return errors.New("autonomous agent needs input while paused or waiting")
			}
			next, open = <-*inputs
		} else {
			select {
			case next, open = <-*inputs:
			default:
				return nil
			}
		}
		if !open || next.err != nil {
			*inputs = nil
			if state.paused || state.waiting {
				return errors.New("autonomous input closed while paused or waiting")
			}
			return nil
		}
		if err := state.apply(next.line, output); err != nil {
			return err
		}
	}
}

func (state *autonomousState) apply(line []byte, output io.Writer) error {
	var identity struct {
		Type      protocol.AgentInputRecordType `json:"type"`
		CommandID string                        `json:"command_id"`
	}
	if err := json.Unmarshal(line, &identity); err != nil {
		return fmt.Errorf("decode autonomous control: %w", err)
	}
	kind := string(identity.Type)
	if kind != "provide_input" && !validAutonomousCommandID(identity.CommandID) {
		return errors.New("autonomous command_id must be a UUID")
	}
	if receipt, exists := state.commands[identity.CommandID]; kind != "provide_input" && exists {
		outcome := receipt.outcome
		if receipt.line != string(line) {
			outcome = "rejected"
		}
		return writeAutonomousReceipt(output, identity.CommandID, kind, outcome)
	}
	envelope, err := decodeInputRecord(line, identity.Type, state.goal, true)
	outcome := "applied"
	deferred := false
	if err != nil {
		outcome = "rejected"
	} else {
		switch kind {
		case "guidance":
			var payload struct {
				Message string `json:"message"`
			}
			if json.Unmarshal(envelope.Input, &payload) != nil || strings.TrimSpace(payload.Message) == "" || state.waiting {
				outcome = "rejected"
			} else {
				state.guidance = append(state.guidance, payload.Message)
			}
		case "pause":
			if state.paused || state.waiting || !emptyAutonomousPayload(envelope.Input) {
				outcome = "rejected"
			} else {
				state.paused = true
			}
		case "resume":
			if !state.paused || !emptyAutonomousPayload(envelope.Input) {
				outcome = "rejected"
			} else {
				state.paused = false
			}
		case "provide_input":
			var payload struct {
				OptionID string `json:"option_id"`
			}
			if json.Unmarshal(envelope.Input, &payload) != nil || !state.waiting || (payload.OptionID != "staged" && payload.OptionID != "defer") {
				outcome = "rejected"
			} else {
				state.waiting = false
				state.decision = payload.OptionID
				deferred = payload.OptionID == "defer"
			}
		default:
			outcome = "rejected"
		}
	}
	if kind == "provide_input" {
		if outcome != "applied" {
			return errors.New("autonomous decision input must match the initial goal and select staged or defer while waiting")
		}
		if err := writeRecord(output, map[string]any{"type": "progress", "schema_version": semanticSchemaVersion, "message": "Decision received: " + state.decision}); err != nil {
			return err
		}
	} else {
		state.commands[identity.CommandID] = autonomousReceipt{line: string(line), outcome: outcome}
		if err := writeAutonomousReceipt(output, identity.CommandID, kind, outcome); err != nil {
			return err
		}
	}
	if deferred {
		return errors.New("autonomous work deferred by decision")
	}
	return nil
}

func writeAutonomousReceipt(output io.Writer, id, kind, outcome string) error {
	return writeRecord(output, map[string]any{"type": "command_applied", "command_id": id, "kind": kind, "outcome": outcome})
}

func emptyAutonomousPayload(input json.RawMessage) bool {
	var object map[string]json.RawMessage
	return json.Unmarshal(input, &object) == nil && object != nil && len(object) == 0
}

func validAutonomousCommandID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for index, char := range id {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if char != '-' {
				return false
			}
		} else if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F') {
			return false
		}
	}
	return true
}

func (state *autonomousState) writeArtifacts(directory string, total int) error {
	progress := fmt.Sprintf("# Autonomous progress\n\nGoal: %s\n\nCompleted: %d/%d\n", state.goal, state.completed, total)
	for _, guidance := range state.guidance {
		progress += "\nGuidance: " + guidance + "\n"
	}
	if state.decision != "" {
		progress += "\nDecision: " + state.decision + "\n"
	}
	if err := os.WriteFile(filepath.Join(directory, "progress.md"), []byte(progress), 0o600); err != nil {
		return fmt.Errorf("write autonomous progress: %w", err)
	}
	result, err := json.MarshalIndent(map[string]any{
		"goal": state.goal, "completed_steps": state.completed, "total_steps": total,
		"guidance": state.guidance, "decision": state.decision,
	}, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(directory, "result.json")
	if err := os.WriteFile(path, append(result, '\n'), 0o600); err != nil {
		return fmt.Errorf("write autonomous result: %w", err)
	}
	readback, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read autonomous result: %w", err)
	}
	if !bytes.Equal(bytes.TrimSpace(readback), result) || !json.Valid(readback) {
		return errors.New("autonomous artifact round-trip failed")
	}
	return nil
}

func autonomousDecision() map[string]any {
	return map[string]any{
		"type": "waiting_for_input", "question": "Which artifact delivery strategy should be used?",
		"decision": map[string]any{
			"reason": "product_change", "context": "Choose whether to finish the generated artifact or defer its delivery.",
			"recommended_option_id": "staged",
			"options": []map[string]string{
				{"id": "staged", "label": "Stage delivery", "consequence": "Finish the remaining local artifact steps and preserve reviewable output."},
				{"id": "defer", "label": "Defer", "consequence": "Stop this attempt and preserve the artifacts already generated."},
			},
		},
	}
}
