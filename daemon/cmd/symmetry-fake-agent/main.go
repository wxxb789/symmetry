// symmetry-fake-agent is a deterministic coding-agent fixture for daemon and
// end-to-end tests. Its exit status is the authoritative result signal.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

const modeEnvironment = "SYMMETRY_FAKE_AGENT_MODE"

type event struct {
	Type     string `json:"type"`
	Message  string `json:"message,omitempty"`
	Question string `json:"question,omitempty"`
	PID      int    `json:"pid,omitempty"`
}

const semanticSchemaVersion = 1

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--child" {
		os.Exit(runChild())
	}

	if err := run(os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(input io.Reader, output io.Writer, errorOutput io.Writer) error {
	reader := bufio.NewReader(input)
	initial, err := readLine(reader)
	if err != nil {
		return fmt.Errorf("read initial input: %w", err)
	}

	envelope, err := decodeTaskInput(initial)
	if err != nil {
		return fmt.Errorf("decode initial input: %w", err)
	}

	mode, err := selectMode(envelope.Goal, envelope.Input, os.Getenv(modeEnvironment))
	if err != nil {
		return err
	}

	switch mode {
	case "success":
		return runSuccess(output)
	case "evidence_success":
		return runEvidenceSuccess(output)
	case "history_success":
		return runHistorySuccess(output)
	case "fail_once_then_evidence_success":
		return runFailOnceThenEvidenceSuccess(envelope.Input, output, errorOutput)
	case "inspect_input":
		return runInspectInput(envelope.Input, output)
	case "json_values":
		return runJSONValues(output)
	case "fail":
		fmt.Fprintln(errorOutput, "fake agent failure")
		return errors.New("fake agent failed")
	case "slow":
		return runSlow(output)
	case "wait_input":
		return runWaitInput(reader, output, envelope.Goal)
	case "wait_twice":
		return runWaitTwice(reader, output, envelope.Goal)
	case "exit_on_file":
		return runExitOnFile(envelope.Input, output)
	case "spawn_child":
		return runSpawnChild(output)
	default:
		return fmt.Errorf("unsupported fake-agent mode %q", mode)
	}
}

func runSuccess(output io.Writer) error {
	if err := writeProgress(output, "started"); err != nil {
		return err
	}
	return writeProgress(output, "completed")
}

func runJSONValues(output io.Writer) error {
	for _, value := range []string{"42", `["progress"]`, "9007199254740993"} {
		if _, err := fmt.Fprintln(output, value); err != nil {
			return err
		}
	}
	return nil
}

func runEvidenceSuccess(output io.Writer) error {
	records := []map[string]any{
		{"type": "progress", "schema_version": semanticSchemaVersion, "message": "started"},
		{"type": "finding", "schema_version": semanticSchemaVersion, "message": "Verified the requested change", "severity": "info"},
		{"type": "artifact", "schema_version": semanticSchemaVersion, "path": "README.md", "kind": "file"},
		{"type": "test", "schema_version": semanticSchemaVersion, "name": "fake-agent verification", "status": "passed"},
		{"type": "pull_request", "schema_version": semanticSchemaVersion, "url": "https://example.invalid/pull/42", "state": "open"},
		{"type": "ci", "schema_version": semanticSchemaVersion, "status": "passed"},
		{"type": "review", "schema_version": semanticSchemaVersion, "status": "required"},
		{"type": "summary", "schema_version": semanticSchemaVersion, "summary": "Completed the requested engineering work"},
		{"type": "progress", "schema_version": semanticSchemaVersion, "message": "completed"},
	}

	for _, record := range records {
		if err := writeRecord(output, record); err != nil {
			return err
		}
	}
	return nil
}

func runHistorySuccess(output io.Writer) error {
	for number := 1; number <= 110; number++ {
		if err := writeRecord(output, map[string]any{
			"type":    "debug",
			"message": fmt.Sprintf("debug-%03d", number),
		}); err != nil {
			return err
		}
	}
	return runEvidenceSuccess(output)
}

func runFailOnceThenEvidenceSuccess(input json.RawMessage, output, errorOutput io.Writer) error {
	var values map[string]json.RawMessage
	if err := json.Unmarshal(input, &values); err != nil {
		return fmt.Errorf("decode fail-once input: %w", err)
	}
	workItemID, found, err := stringField(values, "work_item_id")
	if err != nil || !found || strings.TrimSpace(workItemID) == "" {
		return errors.New("fail_once_then_evidence_success requires work_item_id")
	}
	digest := sha256.Sum256([]byte(workItemID))
	marker := filepath.Join(".", fmt.Sprintf(".symmetry-fake-agent-%x.attempted", digest[:8]))
	if _, err := os.Stat(marker); errors.Is(err, os.ErrNotExist) {
		if writeErr := os.WriteFile(marker, []byte("failed\n"), 0o600); writeErr != nil {
			return fmt.Errorf("write fail-once marker: %w", writeErr)
		}
		_ = writeProgress(output, "first_attempt_failed")
		fmt.Fprintln(errorOutput, "fake agent planned first-attempt failure")
		return errors.New("fake agent failed once")
	} else if err != nil {
		return fmt.Errorf("read fail-once marker: %w", err)
	}
	return runEvidenceSuccess(output)
}

func runInspectInput(input json.RawMessage, output io.Writer) error {
	message := "input:object"
	if bytes.Equal(bytes.TrimSpace(input), []byte("null")) {
		message = "input:null"
	}
	if err := writeProgress(output, message); err != nil {
		return err
	}
	return writeProgress(output, "completed")
}

func runWaitInput(input *bufio.Reader, output io.Writer, goal string) error {
	return runWaitForInput(input, output, goal, "Provide the requested input.")
}

func runWaitTwice(input *bufio.Reader, output io.Writer, goal string) error {
	return runWaitForInput(input, output, goal,
		"Provide the first requested input.",
		"Confirm the requested input before continuing.",
	)
}

func runWaitForInput(input *bufio.Reader, output io.Writer, goal string, questions ...string) error {
	if err := writeProgress(output, "started"); err != nil {
		return err
	}
	for _, question := range questions {
		if err := writeEvent(output, event{Type: "waiting_for_input", Question: question}); err != nil {
			return err
		}
	}
	followUp, err := readLine(input)
	if err != nil {
		return fmt.Errorf("read follow-up input: %w", err)
	}
	if _, err := decodeProvideInput(followUp, goal); err != nil {
		return fmt.Errorf("decode follow-up input: %w", err)
	}
	if err := writeProgress(output, "input_received"); err != nil {
		return err
	}
	return writeProgress(output, "completed")
}

func runExitOnFile(input json.RawMessage, output io.Writer) error {
	path, err := exitOnFilePath(input)
	if err != nil {
		return err
	}
	if err := writeProgress(output, "started"); err != nil {
		return err
	}
	if err := writeProgress(output, "waiting_for_file"); err != nil {
		return err
	}

	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, terminationSignals()...)
	defer signal.Stop(interrupts)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-interrupts:
			if err := writeProgress(output, "termination_requested"); err != nil {
				return err
			}
			return errors.New("fake agent interrupted")
		case <-ticker.C:
			if _, err := os.Stat(path); err == nil {
				return writeProgress(output, "released")
			} else if !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("stat release file: %w", err)
			}
		}
	}
}

func exitOnFilePath(input json.RawMessage) (string, error) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(input, &value); err != nil || value == nil {
		return "", errors.New("exit_on_file input must be a JSON object")
	}
	path, found, err := stringField(value, "path")
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("exit_on_file path is required")
	}
	if !filepath.IsAbs(path) {
		return "", errors.New("exit_on_file path must be absolute")
	}
	return path, nil
}

func runSlow(output io.Writer) error {
	if err := writeProgress(output, "started"); err != nil {
		return err
	}

	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, terminationSignals()...)
	defer signal.Stop(interrupts)

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for tick := 1; ; tick++ {
		select {
		case <-interrupts:
			if err := writeProgress(output, "termination_requested"); err != nil {
				return err
			}
			return errors.New("fake agent interrupted")
		case <-ticker.C:
			if err := writeProgress(output, fmt.Sprintf("tick_%d", tick)); err != nil {
				return err
			}
		}
	}
}

func runSpawnChild(output io.Writer) error {
	if err := writeProgress(output, "started"); err != nil {
		return err
	}

	child := exec.Command(os.Args[0], "--child")
	child.Stdin = nil
	child.Stdout = nil
	child.Stderr = nil
	if err := child.Start(); err != nil {
		return fmt.Errorf("start child: %w", err)
	}
	if err := writeEvent(output, event{Type: "child_started", PID: child.Process.Pid}); err != nil {
		return err
	}
	return writeProgress(output, "completed")
}

func runChild() int {
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, terminationSignals()...)
	defer signal.Stop(interrupts)

	select {
	case <-interrupts:
		return 0
	case <-time.After(time.Minute):
		return 0
	}
}

func selectMode(goal string, input json.RawMessage, environment string) (string, error) {
	if mode, found, err := modeFromInput(input); err != nil {
		return "", err
	} else if found {
		return mode, nil
	}
	if mode, found := modeFromGoal(goal); found {
		return mode, nil
	}
	if environment != "" {
		return environment, nil
	}
	return "success", nil
}

func modeFromGoal(goal string) (string, bool) {
	const prefix = "[symmetry-fake-agent:"

	for _, line := range strings.Split(goal, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) && strings.HasSuffix(line, "]") {
			mode := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, prefix), "]"))
			if mode != "" {
				return mode, true
			}
		}
	}
	return "", false
}

func modeFromInput(input json.RawMessage) (string, bool, error) {
	if bytes.Equal(bytes.TrimSpace(input), []byte("null")) {
		return "", false, nil
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(input, &value); err != nil {
		return "", false, fmt.Errorf("decode input mode: %w", err)
	}
	for _, key := range []string{"mode", "fake_agent_mode"} {
		if mode, found, err := stringField(value, key); found || err != nil {
			return mode, found, err
		}
	}
	return "", false, nil
}

func decodeTaskInput(line []byte) (protocol.AgentInputRecord, error) {
	return decodeInputRecord(line, protocol.AgentInputRecordTaskInput, "", false)
}

func decodeProvideInput(line []byte, goal string) (protocol.AgentInputRecord, error) {
	return decodeInputRecord(line, protocol.AgentInputRecordProvideInput, goal, true)
}

func decodeInputRecord(line []byte, expectedType protocol.AgentInputRecordType, expectedGoal string, requireObjectInput bool) (protocol.AgentInputRecord, error) {
	var envelope protocol.AgentInputRecord
	decoder := json.NewDecoder(bytes.NewReader(line))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return protocol.AgentInputRecord{}, fmt.Errorf("decode JSON envelope: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return protocol.AgentInputRecord{}, errors.New("JSON envelope must contain one object")
		}
		return protocol.AgentInputRecord{}, fmt.Errorf("decode JSON envelope: %w", err)
	}
	if envelope.Type != expectedType {
		return protocol.AgentInputRecord{}, fmt.Errorf("type must be %s", expectedType)
	}
	if strings.TrimSpace(envelope.Goal) == "" {
		return protocol.AgentInputRecord{}, errors.New("goal must not be empty")
	}
	if expectedGoal != "" && envelope.Goal != expectedGoal {
		return protocol.AgentInputRecord{}, errors.New("goal must match initial input")
	}
	if len(envelope.Input) == 0 {
		return protocol.AgentInputRecord{}, errors.New("input is required")
	}

	input := bytes.TrimSpace(envelope.Input)
	if bytes.Equal(input, []byte("null")) {
		if requireObjectInput {
			return protocol.AgentInputRecord{}, errors.New("input must be a JSON object")
		}
		return envelope, nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(input, &object); err != nil {
		if requireObjectInput {
			return protocol.AgentInputRecord{}, errors.New("input must be a JSON object")
		}
		return protocol.AgentInputRecord{}, errors.New("input must be null or a JSON object")
	}
	return envelope, nil
}

func stringField(value map[string]json.RawMessage, key string) (string, bool, error) {
	raw, found := value[key]
	if !found {
		return "", false, nil
	}
	var mode string
	if err := json.Unmarshal(raw, &mode); err != nil {
		return "", true, fmt.Errorf("decode %s: %w", key, err)
	}
	if strings.TrimSpace(mode) == "" {
		return "", true, fmt.Errorf("%s must not be empty", key)
	}
	return mode, true, nil
}

func readLine(reader *bufio.Reader) ([]byte, error) {
	line, err := reader.ReadBytes('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("input record must end with newline")
		}
		return nil, err
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil, errors.New("input line must not be empty")
	}
	return line, nil
}

func writeProgress(output io.Writer, message string) error {
	return writeEvent(output, event{Type: "progress", Message: message})
}

func writeEvent(output io.Writer, value event) error {
	return writeRecord(output, value)
}

func writeRecord(output io.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode event: %w", err)
	}
	if _, err := output.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write event: %w", err)
	}
	return nil
}
