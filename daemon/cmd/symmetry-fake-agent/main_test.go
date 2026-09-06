package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/protocol"
)

var (
	fixtureBuildOnce   sync.Once
	fixtureBuildPath   string
	fixtureBuildOutput []byte
	fixtureBuildError  error
	fixtureDirectory   string
)

func TestMain(mainTest *testing.M) {
	code := mainTest.Run()
	if fixtureDirectory != "" {
		_ = os.RemoveAll(fixtureDirectory)
	}
	os.Exit(code)
}

func TestSuccessEmitsProgressJSONLines(t *testing.T) {
	process := startFixture(t, taskInput(`"write tests"`, `null`), nil)
	_, stderr, err := waitForProcess(t, process, time.Second)
	if err != nil {
		t.Fatalf("fixture failed: %v; stderr: %s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}

	events := decodeEvents(t, process.output.String())
	if got, want := events, []event{{Type: "progress", Message: "started"}, {Type: "progress", Message: "completed"}}; !equalEvents(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
}

func TestInspectInputDistinguishesNullAndEmptyObject(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "null", input: `null`, want: "input:null"},
		{name: "empty object", input: `{}`, want: "input:object"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			process := startFixture(t, taskInput(`"inspect input"`, test.input), []string{modeEnvironment + "=inspect_input"})
			_, stderr, err := waitForProcess(t, process, time.Second)
			if err != nil {
				t.Fatalf("fixture failed: %v; stderr: %s", err, stderr)
			}
			if stderr != "" {
				t.Fatalf("unexpected stderr: %q", stderr)
			}
			if got, want := decodeEvents(t, process.output.String()), []event{
				{Type: "progress", Message: test.want},
				{Type: "progress", Message: "completed"},
			}; !equalEvents(got, want) {
				t.Fatalf("events = %#v, want %#v", got, want)
			}
		})
	}
}

func TestJSONValuesEmitsScalarArrayAndLargeIntegerLines(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if err := run(strings.NewReader(taskInput(`"json values"`, `{"mode":"json_values"}`)), &stdout, &stderr); err != nil {
		t.Fatalf("fixture failed: %v; stderr: %s", err, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}

	if got, want := strings.TrimSpace(stdout.String()), "42\n[\"progress\"]\n9007199254740993"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestProviderActionFlowUsesBrokerCapability(t *testing.T) {
	requests := make(chan map[string]any, 3)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("Authorization"); got != "Bearer broker-token" {
			t.Errorf("authorization = %q", got)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		requests <- body
		writer.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(writer, `{}`)
	}))
	defer server.Close()

	input := providerTaskInput(t, server.URL, []protocol.ProviderGrant{
		{
			ResourceID: "11111111-1111-1111-1111-111111111111",
			Provider:   "github",
			Kind:       "repository",
			Operations: []string{"resource.sync", "change.upsert", "change.update"},
		},
		{
			ResourceID: "22222222-2222-2222-2222-222222222222",
			Provider:   "github",
			Kind:       "ci",
			Operations: []string{"resource.sync"},
		},
	})
	process := startFixture(t, input, nil)
	_, stderr, err := waitForProcess(t, process, 2*time.Second)
	if err != nil {
		t.Fatalf("fixture failed: %v; stderr: %s", err, stderr)
	}

	first := <-requests
	second := <-requests
	third := <-requests
	if got := first["operation"]; got != "change.upsert" {
		t.Fatalf("first operation = %#v", got)
	}
	if got := first["resource_id"]; got != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("first resource_id = %#v", got)
	}
	if got := first["input"].(map[string]any)["title"]; got != "Provider broker end-to-end" {
		t.Fatalf("first title = %#v", got)
	}
	if got := second["operation"]; got != "resource.sync" {
		t.Fatalf("second operation = %#v", got)
	}
	if got := second["resource_id"]; got != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("second resource_id = %#v", got)
	}
	if got := third["operation"]; got != "resource.sync" {
		t.Fatalf("third operation = %#v", got)
	}
	if got := third["resource_id"]; got != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("third resource_id = %#v", got)
	}
	if first["action_id"] == second["action_id"] || second["action_id"] == third["action_id"] {
		t.Fatalf("action IDs must differ: %#v", []any{first["action_id"], second["action_id"], third["action_id"]})
	}

	if got, want := decodeEvents(t, process.output.String()), []event{
		{Type: "progress", Message: "provider_action_started"},
		{Type: "progress", Message: "provider_action_completed"},
	}; !equalEvents(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
}

func TestProviderActionFlowRequiresCapability(t *testing.T) {
	process := startFixture(t, taskInput(`"provider action"`, `{"mode":"provider_action_flow","work_item_id":"item-42"}`), nil)
	_, stderr, err := waitForProcess(t, process, time.Second)
	if err == nil {
		t.Fatal("fixture succeeded, want failure")
	}
	if !strings.Contains(stderr, "requires provider_access") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestSelectModeUsesExplicitInputThenGoalDirectiveThenEnvironment(t *testing.T) {
	tests := []struct {
		name        string
		goal        string
		input       string
		environment string
		want        string
	}{
		{name: "input", goal: "[symmetry-fake-agent:wait_input]", input: `{"mode":"fail"}`, environment: "slow", want: "fail"},
		{name: "goal", goal: "work\n\n[symmetry-fake-agent:wait_input]", input: `{}`, environment: "slow", want: "wait_input"},
		{name: "environment", goal: "work", input: `{}`, environment: "slow", want: "slow"},
		{name: "default", goal: "work", input: `{}`, want: "success"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := selectMode(test.goal, json.RawMessage(test.input), test.environment)
			if err != nil {
				t.Fatalf("selectMode() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("selectMode() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFailWritesStderrAndExitsNonZero(t *testing.T) {
	process := startFixture(t, taskInput(`"fail task"`, `{"mode":"fail"}`), nil)
	_, stderr, err := waitForProcess(t, process, time.Second)
	if err == nil {
		t.Fatal("fixture succeeded, want failure")
	}
	if !strings.Contains(stderr, "fake agent failure") {
		t.Fatalf("stderr = %q, want fake failure", stderr)
	}
	if len(decodeEvents(t, process.output.String())) != 0 {
		t.Fatalf("stdout = %q, want no events", process.output.String())
	}
}

func TestEvidenceSuccessEmitsOutcomeEvents(t *testing.T) {
	process := startFixture(t, taskInput(`"evidence"`, `{"mode":"evidence_success"}`), nil)
	_, stderr, err := waitForProcess(t, process, time.Second)
	if err != nil {
		t.Fatalf("fixture failed: %v; stderr: %s", err, stderr)
	}

	var kinds []string
	for _, line := range strings.Split(strings.TrimSpace(process.output.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode record: %v", err)
		}
		if record["schema_version"] != float64(semanticSchemaVersion) {
			t.Fatalf("schema_version = %#v", record["schema_version"])
		}
		kinds = append(kinds, record["type"].(string))
	}

	want := []string{"progress", "finding", "artifact", "test", "pull_request", "ci", "review", "summary", "progress"}
	if strings.Join(kinds, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("kinds = %#v, want %#v", kinds, want)
	}
}

func TestHistorySuccessEmitsPageableRawRecordsBeforeOutcome(t *testing.T) {
	process := startFixture(t, taskInput(`"history"`, `{"mode":"history_success"}`), nil)
	_, stderr, err := waitForProcess(t, process, time.Second)
	if err != nil {
		t.Fatalf("fixture failed: %v; stderr: %s", err, stderr)
	}

	lines := strings.Split(strings.TrimSpace(process.output.String()), "\n")
	if len(lines) != 119 {
		t.Fatalf("records = %d, want 119", len(lines))
	}
	if !strings.Contains(lines[0], `"type":"debug"`) || !strings.Contains(lines[0], `"message":"debug-001"`) {
		t.Fatalf("first record = %q", lines[0])
	}
	if !strings.Contains(lines[109], `"message":"debug-110"`) {
		t.Fatalf("last raw record = %q", lines[109])
	}
	if !strings.Contains(strings.Join(lines[110:], "\n"), `"type":"summary"`) {
		t.Fatal("semantic outcome records do not include a summary")
	}
}

func TestFailOnceThenEvidenceSuccessPersistsAcrossProcesses(t *testing.T) {
	directory := t.TempDir()
	input := taskInput(`"retry fixture"`, `{"mode":"fail_once_then_evidence_success","work_item_id":"item-42"}`)

	first := startFixtureInDir(t, directory, input, nil)
	_, firstStderr, firstErr := waitForProcess(t, first, time.Second)
	if firstErr == nil || !strings.Contains(firstStderr, "planned first-attempt failure") {
		t.Fatalf("first attempt error = %v, stderr = %q", firstErr, firstStderr)
	}

	second := startFixtureInDir(t, directory, input, nil)
	_, secondStderr, secondErr := waitForProcess(t, second, time.Second)
	if secondErr != nil {
		t.Fatalf("second attempt failed: %v; stderr: %s", secondErr, secondStderr)
	}
	if !strings.Contains(second.output.String(), `"type":"summary"`) {
		t.Fatalf("second attempt output = %q, want summary event", second.output.String())
	}
}

func TestWaitInputReadsFollowUpAndCompletes(t *testing.T) {
	process := startFixture(t, taskInput(`"wait for answer"`, `{"mode":"wait_input"}`), nil)
	defer closeStdin(t, process)

	first := readEvent(t, process.output)
	if first != (event{Type: "progress", Message: "started"}) {
		t.Fatalf("first event = %#v", first)
	}
	waiting := readEvent(t, process.output)
	if waiting.Type != "waiting_for_input" || waiting.Question == "" {
		t.Fatalf("waiting event = %#v", waiting)
	}
	if _, err := io.WriteString(process.stdin, provideInput(`"wait for answer"`, `{"answer":"main"}`)); err != nil {
		t.Fatalf("write follow-up input: %v", err)
	}
	closeStdin(t, process)

	_, stderr, err := waitForProcess(t, process, time.Second)
	if err != nil {
		t.Fatalf("fixture failed: %v; stderr: %s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	events := decodeEvents(t, process.output.String())
	if got, want := events, []event{{Type: "progress", Message: "started"}, {Type: "waiting_for_input", Question: "Provide the requested input."}, {Type: "progress", Message: "input_received"}, {Type: "progress", Message: "completed"}}; !equalEvents(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
}

func TestWaitTwiceEmitsDistinctQuestionsThenCompletes(t *testing.T) {
	process := startFixture(t, taskInput(`"wait twice"`, `{"mode":"wait_twice"}`), nil)
	defer closeStdin(t, process)

	if first := readEvent(t, process.output); first != (event{Type: "progress", Message: "started"}) {
		t.Fatalf("first event = %#v", first)
	}
	firstWaiting := readEvent(t, process.output)
	secondWaiting := readEvent(t, process.output)
	if firstWaiting.Type != "waiting_for_input" || firstWaiting.Question == "" {
		t.Fatalf("first waiting event = %#v", firstWaiting)
	}
	if secondWaiting.Type != "waiting_for_input" || secondWaiting.Question == "" || secondWaiting.Question == firstWaiting.Question {
		t.Fatalf("second waiting event = %#v, first waiting event = %#v", secondWaiting, firstWaiting)
	}
	if _, err := io.WriteString(process.stdin, provideInput(`"wait twice"`, `{"answer":"confirmed"}`)); err != nil {
		t.Fatalf("write follow-up input: %v", err)
	}
	closeStdin(t, process)

	_, stderr, err := waitForProcess(t, process, time.Second)
	if err != nil {
		t.Fatalf("fixture failed: %v; stderr: %s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if got, want := decodeEvents(t, process.output.String()), []event{
		{Type: "progress", Message: "started"},
		{Type: "waiting_for_input", Question: "Provide the first requested input."},
		{Type: "waiting_for_input", Question: "Confirm the requested input before continuing."},
		{Type: "progress", Message: "input_received"},
		{Type: "progress", Message: "completed"},
	}; !equalEvents(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
}

func TestExitOnFileWaitsThenReleases(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release")
	process := startFixture(t, taskInput(`"wait for file"`, fmt.Sprintf(`{"mode":"exit_on_file","path":%s}`, strconv.Quote(path))), nil)
	defer closeStdin(t, process)

	if first := readEvent(t, process.output); first != (event{Type: "progress", Message: "started"}) {
		t.Fatalf("first event = %#v", first)
	}
	if waiting := readEvent(t, process.output); waiting != (event{Type: "progress", Message: "waiting_for_file"}) {
		t.Fatalf("waiting event = %#v", waiting)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("create release file: %v", err)
	}
	closeStdin(t, process)

	_, stderr, err := waitForProcess(t, process, time.Second)
	if err != nil {
		t.Fatalf("fixture failed: %v; stderr: %s", err, stderr)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
	if got, want := decodeEvents(t, process.output.String()), []event{
		{Type: "progress", Message: "started"},
		{Type: "progress", Message: "waiting_for_file"},
		{Type: "progress", Message: "released"},
	}; !equalEvents(got, want) {
		t.Fatalf("events = %#v, want %#v", got, want)
	}
}

func TestExitOnFileCancellationStopsTheFixture(t *testing.T) {
	path := filepath.Join(t.TempDir(), "release")
	output := newEventCollector()
	process, err := execution.NewRunner().Start(
		context.Background(),
		execution.Invocation{
			Program:                fixtureBinary(t),
			InitialInput:           []byte(taskInput(`"wait for file"`, fmt.Sprintf(`{"mode":"exit_on_file","path":%s}`, strconv.Quote(path)))),
			CloseInputAfterInitial: true,
		},
		fixtureSink{output: output},
	)
	if err != nil {
		t.Fatalf("start fixture through Runner: %v", err)
	}
	if first := readEvent(t, output); first != (event{Type: "progress", Message: "started"}) {
		t.Fatalf("first event = %#v", first)
	}
	if waiting := readEvent(t, output); waiting != (event{Type: "progress", Message: "waiting_for_file"}) {
		t.Fatalf("waiting event = %#v", waiting)
	}

	terminationContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := process.Terminate(terminationContext, 100*time.Millisecond); err != nil {
		t.Fatalf("terminate fixture: %v", err)
	}
	result := process.Wait()
	if !result.Terminated {
		t.Fatal("Terminated = false, want true")
	}
	if result.Success() || result.ExitCode == 0 {
		t.Fatalf("result = %+v, want terminated failure", result)
	}
}

func TestExitOnFileRejectsInvalidPath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "missing", input: `{"mode":"exit_on_file"}`, want: "exit_on_file path is required"},
		{name: "empty", input: `{"mode":"exit_on_file","path":" "}`, want: "path must not be empty"},
		{name: "relative", input: `{"mode":"exit_on_file","path":"release"}`, want: "exit_on_file path must be absolute"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			process := startFixture(t, taskInput(`"wait for file"`, test.input), nil)
			_, stderr, err := waitForProcess(t, process, time.Second)
			if err == nil {
				t.Fatal("fixture succeeded, want failure")
			}
			if !strings.Contains(stderr, test.want) {
				t.Fatalf("stderr = %q, want %q", stderr, test.want)
			}
		})
	}
}

func TestSlowCancellationStopsTheFixture(t *testing.T) {
	output := newEventCollector()
	process, err := execution.NewRunner().Start(
		context.Background(),
		execution.Invocation{
			Program:                fixtureBinary(t),
			InitialInput:           []byte(taskInput(`"slow task"`, `{"mode":"slow"}`)),
			CloseInputAfterInitial: true,
		},
		fixtureSink{output: output},
	)
	if err != nil {
		t.Fatalf("start fixture through Runner: %v", err)
	}

	if first := readEvent(t, output); first != (event{Type: "progress", Message: "started"}) {
		t.Fatalf("first event = %#v", first)
	}
	if tick := readEvent(t, output); tick.Type != "progress" || !strings.HasPrefix(tick.Message, "tick_") {
		t.Fatalf("tick event = %#v", tick)
	}

	terminationContext, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := process.Terminate(terminationContext, 100*time.Millisecond); err != nil {
		t.Fatalf("terminate fixture: %v", err)
	}
	result := process.Wait()
	if !result.Terminated {
		t.Fatal("Terminated = false, want true")
	}
	if result.Success() {
		t.Fatalf("result = %+v, want terminated failure", result)
	}
	if result.ExitCode == 0 {
		t.Fatalf("exit code = 0, want non-zero after cancellation; result = %+v", result)
	}
}

func TestSpawnChildEmitsChildPID(t *testing.T) {
	process := startFixture(t, taskInput(`"spawn child"`, `{"mode":"spawn_child"}`), nil)
	_, stderr, err := waitForProcess(t, process, time.Second)
	if err != nil {
		t.Fatalf("fixture failed: %v; stderr: %s", err, stderr)
	}
	events := decodeEvents(t, process.output.String())
	if len(events) != 3 || events[1].Type != "child_started" || events[1].PID <= 0 {
		t.Fatalf("events = %#v, want child_started PID", events)
	}
	childPID := events[1].PID
	t.Cleanup(func() { terminatePID(t, childPID) })
}

func TestRejectsInvalidInitialEnvelope(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "plain text", input: "write tests\n", want: "decode initial input"},
		{name: "wrong type", input: `{"type":"provide_input","goal":"write tests","input":{}}` + "\n", want: "type must be task_input"},
		{name: "empty goal", input: `{"type":"task_input","goal":" ","input":null}` + "\n", want: "goal must not be empty"},
		{name: "missing input", input: `{"type":"task_input","goal":"write tests"}` + "\n", want: "input is required"},
		{name: "array input", input: `{"type":"task_input","goal":"write tests","input":[]}` + "\n", want: "input must be null or a JSON object"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			process := startFixture(t, test.input, nil)
			_, stderr, err := waitForProcess(t, process, time.Second)
			if err == nil {
				t.Fatal("fixture succeeded, want failure")
			}
			if !strings.Contains(stderr, test.want) {
				t.Fatalf("stderr = %q, want %q", stderr, test.want)
			}
		})
	}
}

func TestRejectsInitialInputWithoutTrailingNewline(t *testing.T) {
	process := startFixture(t, `{"type":"task_input","goal":"write tests","input":null}`, nil)
	closeStdin(t, process)

	_, stderr, err := waitForProcess(t, process, time.Second)
	if err == nil {
		t.Fatal("fixture succeeded, want failure")
	}
	if !strings.Contains(stderr, "input record must end with newline") {
		t.Fatalf("stderr = %q, want missing newline error", stderr)
	}
}

func TestWaitInputRejectsInvalidFollowUpEnvelope(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "wrong type", input: taskInput(`"wait for answer"`, `{}`), want: "type must be provide_input"},
		{name: "different goal", input: provideInput(`"other task"`, `{}`), want: "goal must match initial input"},
		{name: "missing input", input: `{"type":"provide_input","goal":"wait for answer"}` + "\n", want: "input is required"},
		{name: "null input", input: provideInput(`"wait for answer"`, `null`), want: "input must be a JSON object"},
		{name: "array input", input: provideInput(`"wait for answer"`, `[]`), want: "input must be a JSON object"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			process := startFixture(t, taskInput(`"wait for answer"`, `{"mode":"wait_input"}`), nil)
			defer closeStdin(t, process)

			_ = readEvent(t, process.output)
			_ = readEvent(t, process.output)
			if _, err := io.WriteString(process.stdin, test.input); err != nil {
				t.Fatalf("write follow-up input: %v", err)
			}
			closeStdin(t, process)

			_, stderr, err := waitForProcess(t, process, time.Second)
			if err == nil {
				t.Fatal("fixture succeeded, want failure")
			}
			if !strings.Contains(stderr, test.want) {
				t.Fatalf("stderr = %q, want %q", stderr, test.want)
			}
		})
	}
}

func TestWaitInputRejectsFollowUpWithoutTrailingNewline(t *testing.T) {
	process := startFixture(t, taskInput(`"wait for answer"`, `{"mode":"wait_input"}`), nil)
	defer closeStdin(t, process)

	_ = readEvent(t, process.output)
	_ = readEvent(t, process.output)
	if _, err := io.WriteString(process.stdin, `{"type":"provide_input","goal":"wait for answer","input":{}}`); err != nil {
		t.Fatalf("write follow-up input: %v", err)
	}
	closeStdin(t, process)

	_, stderr, err := waitForProcess(t, process, time.Second)
	if err == nil {
		t.Fatal("fixture succeeded, want failure")
	}
	if !strings.Contains(stderr, "input record must end with newline") {
		t.Fatalf("stderr = %q, want missing newline error", stderr)
	}
}

func taskInput(goal, input string) string {
	return `{"type":"` + string(protocol.AgentInputRecordTaskInput) + `","goal":` + goal + `,"input":` + input + "}\n"
}

func provideInput(goal, input string) string {
	return `{"type":"` + string(protocol.AgentInputRecordProvideInput) + `","goal":` + goal + `,"input":` + input + "}\n"
}

func providerTaskInput(t *testing.T, path string, grants []protocol.ProviderGrant) string {
	t.Helper()
	record := protocol.AgentInputRecord{
		Type:  protocol.AgentInputRecordTaskInput,
		Goal:  "provider action",
		Input: json.RawMessage(`{"mode":"provider_action_flow","work_item_id":"item-42"}`),
		ProviderAccess: &protocol.ProviderAccess{
			Path: path, Token: "broker-token", Grants: grants,
		},
	}
	encoded, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("encode provider task input: %v", err)
	}
	return string(encoded) + "\n"
}

type fixtureSink struct {
	output *eventCollector
}

func (sink fixtureSink) Handle(_ context.Context, value execution.Event) error {
	if value.Stream != execution.Stdout {
		return nil
	}
	_, err := sink.output.Write(value.Data)
	return err
}

type fixtureProcess struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	output  *eventCollector
	stderr  *bytes.Buffer
}

func startFixture(t *testing.T, initial string, environment []string) *fixtureProcess {
	return startFixtureInDir(t, "", initial, environment)
}

func startFixtureInDir(t *testing.T, directory, initial string, environment []string) *fixtureProcess {
	t.Helper()
	path := fixtureBinary(t)
	command := exec.Command(path)
	if directory != "" {
		command.Dir = directory
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("open fixture stdin: %v", err)
	}
	output := newEventCollector()
	stderr := &bytes.Buffer{}
	command.Stdout = output
	command.Stderr = stderr
	command.Env = environmentWith(os.Environ(), environment)
	if err := command.Start(); err != nil {
		t.Fatalf("start fixture: %v", err)
	}
	if _, err := io.WriteString(stdin, initial); err != nil {
		t.Fatalf("write initial input: %v", err)
	}
	return &fixtureProcess{command: command, stdin: stdin, output: output, stderr: stderr}
}

func fixtureBinary(t *testing.T) string {
	t.Helper()
	fixtureBuildOnce.Do(func() {
		fixtureDirectory, fixtureBuildError = os.MkdirTemp("", "symmetry-fake-agent-test-")
		if fixtureBuildError != nil {
			return
		}

		name := "symmetry-fake-agent"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		fixtureBuildPath = filepath.Join(fixtureDirectory, name)
		command := exec.Command("go", "build", "-o", fixtureBuildPath, ".")
		command.Dir = "."
		fixtureBuildOutput, fixtureBuildError = command.CombinedOutput()
	})
	if fixtureBuildError != nil {
		t.Fatalf("build fixture: %v\n%s", fixtureBuildError, fixtureBuildOutput)
	}
	return fixtureBuildPath
}

func environmentWith(base, overrides []string) []string {
	result := append([]string(nil), base...)
	for _, override := range overrides {
		key, _, found := strings.Cut(override, "=")
		if !found {
			continue
		}
		result = filterEnvironment(result, key)
		result = append(result, override)
	}
	return result
}

func filterEnvironment(environment []string, key string) []string {
	result := environment[:0]
	for _, entry := range environment {
		entryKey, _, found := strings.Cut(entry, "=")
		if found && sameEnvironmentKey(entryKey, key) {
			continue
		}
		result = append(result, entry)
	}
	return result
}

func sameEnvironmentKey(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func readEvent(t *testing.T, output *eventCollector) event {
	t.Helper()
	select {
	case value := <-output.events:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for JSONL event")
		return event{}
	}
}

func waitForProcess(t *testing.T, process *fixtureProcess, timeout time.Duration) (string, string, error) {
	t.Helper()
	result := make(chan error, 1)
	go func() { result <- process.command.Wait() }()
	select {
	case err := <-result:
		return process.output.String(), process.stderr.String(), err
	case <-time.After(timeout):
		_ = process.command.Process.Kill()
		<-result
		t.Fatal("timed out waiting for fixture")
		return "", "", nil
	}
}

type eventCollector struct {
	mutex   sync.Mutex
	output  bytes.Buffer
	pending []byte
	events  chan event
}

func newEventCollector() *eventCollector {
	return &eventCollector{events: make(chan event, 1024)}
}

func (collector *eventCollector) Write(data []byte) (int, error) {
	collector.mutex.Lock()
	_, _ = collector.output.Write(data)
	collector.pending = append(collector.pending, data...)

	var parsed []event
	for {
		index := bytes.IndexByte(collector.pending, '\n')
		if index < 0 {
			break
		}
		line := collector.pending[:index]
		collector.pending = collector.pending[index+1:]
		var value event
		if err := json.Unmarshal(line, &value); err != nil {
			collector.mutex.Unlock()
			return 0, fmt.Errorf("decode fixture JSONL: %w", err)
		}
		parsed = append(parsed, value)
	}
	collector.mutex.Unlock()

	for _, value := range parsed {
		collector.events <- value
	}
	return len(data), nil
}

func (collector *eventCollector) String() string {
	collector.mutex.Lock()
	defer collector.mutex.Unlock()
	return collector.output.String()
}

func closeStdin(t *testing.T, process *fixtureProcess) {
	t.Helper()
	if process.stdin != nil {
		if err := process.stdin.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
			t.Fatalf("close fixture stdin: %v", err)
		}
		process.stdin = nil
	}
}

func decodeEvents(t *testing.T, output string) []event {
	t.Helper()
	if output == "" {
		return nil
	}
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	events := make([]event, 0, len(lines))
	for _, line := range lines {
		var value event
		if err := json.Unmarshal([]byte(line), &value); err != nil {
			t.Fatalf("invalid JSONL %q: %v", line, err)
		}
		events = append(events, value)
	}
	return events
}

func equalEvents(left, right []event) bool {
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

func terminatePID(t *testing.T, pid int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		command := exec.Command("taskkill", "/PID", strconv.Itoa(pid), "/F")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("terminate child %d: %v: %s", pid, err, output)
		}
		return
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("find child %d: %v", pid, err)
	}
	if err := process.Kill(); err != nil {
		t.Fatalf("kill child %d: %v", pid, err)
	}
}
