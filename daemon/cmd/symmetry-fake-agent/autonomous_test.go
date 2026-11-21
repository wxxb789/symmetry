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
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/protocol"
	"go.uber.org/goleak"
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

// leakCheckTB is the part of testing.TB the per-test leak oracle uses.
// *testing.T satisfies it directly; TestLeakOracleDetectsLeakedGoroutine drives
// the same oracle through a recording implementation, so the oracle's liveness
// is pinned by a test and not only by an out-of-repo mutation record.
type leakCheckTB interface {
	Cleanup(func())
	Error(args ...any)
	Helper()
}

// verifyNoLeakedGoroutines marks the test failed when a goroutine it started is
// still running after the test's own cleanups, so call it first in a test: its
// cleanup then runs last. The baseline snapshot keeps a failure attributable:
// goleak's VerifyNone scans every goroutine in the process, so without it a
// goroutine stranded by another test in this package would blame this one. Not
// for tests that call t.Parallel: VerifyNone cannot associate goroutines with a
// specific test.
func verifyNoLeakedGoroutines(tb leakCheckTB) {
	tb.Helper()
	baseline := goleak.IgnoreCurrent()
	tb.Cleanup(func() {
		goleak.VerifyNone(tb, baseline)
	})
}

// TestLeakOracleDetectsLeakedGoroutine pins the liveness of
// verifyNoLeakedGoroutines: it drives the oracle itself with a recording TB
// while a goroutine is parked in this test, and requires the recorded failure
// to name this test. Neuter the oracle (a no-op body) and this test fails while
// TestAutonomousReturnReleasesItsInputReader would stay green. It joins the
// parked goroutine before returning so no later test's check sees residue.
func TestLeakOracleDetectsLeakedGoroutine(t *testing.T) {
	recorder := &leakCheckRecorder{}
	verifyNoLeakedGoroutines(recorder)

	release := make(chan struct{})
	started := make(chan struct{})
	exited := make(chan struct{})
	go parkedLeakOracleGoroutine(started, release, exited)
	<-started
	recorder.runCleanups()
	close(release)
	<-exited

	if len(recorder.failures) == 0 {
		t.Fatal("verifyNoLeakedGoroutines reported no failure while a goroutine was parked in this test; the per-test leak oracle is inert")
	}
	if report := strings.Join(recorder.failures, "\n"); !strings.Contains(report, t.Name()) {
		t.Fatalf("verifyNoLeakedGoroutines failed without naming the goroutine parked by this test; reported:\n%s", report)
	}
}

// parkedLeakOracleGoroutine blocks until release is closed. It is a named
// function so the oracle's report reads as this test's own goroutine.
func parkedLeakOracleGoroutine(started chan<- struct{}, release <-chan struct{}, exited chan<- struct{}) {
	close(started)
	<-release
	close(exited)
}

// leakCheckRecorder records what the leak oracle reports instead of failing the
// test that expects the failure.
type leakCheckRecorder struct {
	cleanups []func()
	failures []string
}

func (*leakCheckRecorder) Helper() {}

func (recorder *leakCheckRecorder) Error(args ...any) {
	recorder.failures = append(recorder.failures, fmt.Sprintln(args...))
}

func (recorder *leakCheckRecorder) Cleanup(cleanup func()) {
	recorder.cleanups = append(recorder.cleanups, cleanup)
}

// runCleanups runs the recorded cleanups in testing.T's order: last registered
// first.
func (recorder *leakCheckRecorder) runCleanups() {
	for i := len(recorder.cleanups) - 1; i >= 0; i-- {
		recorder.cleanups[i]()
	}
}

// TestAutonomousReturnReleasesItsInputReader pins the exit path of an
// autonomous run: the run finishes while the pipe's read side is still open and
// the reader goroutine is parked in readLine, and returning closes the input
// and joins that goroutine. The decision record only reaches the run through
// the reader goroutine, so a reader that drops an already-read record leaves
// the run blocked in boundary and this test times out instead of finishing.
// The stack scan right after the return pins the join itself: goleak's
// VerifyNone retries, so it would also pass a reader that exits shortly after
// the return instead of being joined.
func TestAutonomousReturnReleasesItsInputReader(t *testing.T) {
	verifyNoLeakedGoroutines(t)
	// The run writes progress.md and result.json into the working directory.
	t.Chdir(t.TempDir())

	input, writer := io.Pipe()
	// The write end is deliberately left open: only the run may release its
	// reader goroutine, so a run that leaves it parked is still visible to the
	// leak check at the end of this test instead of being unblocked here.
	var output bytes.Buffer
	decision := `{"type":"provide_input","goal":"goal","input":{"option_id":"staged"}}` + "\n"
	writerHook := autonomousOutputFunc(func(data []byte) (int, error) {
		if bytes.Contains(data, []byte(`"type":"waiting_for_input"`)) {
			if _, err := writer.Write([]byte(decision)); err != nil {
				return 0, err
			}
		}
		return output.Write(data)
	})
	envelope := protocol.AgentInputRecord{
		Type:  protocol.AgentInputRecordTaskInput,
		Goal:  "goal",
		Input: json.RawMessage(`{"steps":1,"step_ms":0,"decision_at":1}`),
	}
	runDone := make(chan error, 1)
	go func() { runDone <- runAutonomous(bufio.NewReader(input), input, writerHook, envelope) }()

	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("runAutonomous: %v", err)
		}
	case <-time.After(testHangGuard):
		t.Fatal("runAutonomous did not return after the decision was answered")
	}
	// At this instant the join is the only thing that can have made the reader
	// goroutine exit, so its absence shows up here. goleak alone does not pin
	// this: VerifyNone retries, and the io.ErrClosedPipe assertion below pins
	// only the Close.
	stacks := make([]byte, 1<<20)
	stacks = stacks[:runtime.Stack(stacks, true)]
	if bytes.Contains(stacks, []byte("runAutonomous.func1")) {
		t.Fatalf("reader goroutine is still alive when runAutonomous returned:\n%s", stacks)
	}
	if _, err := writer.Write([]byte("late\n")); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write after return = %v, want io.ErrClosedPipe: the input was not closed", err)
	}
	if !strings.Contains(output.String(), `"type":"summary"`) {
		t.Fatalf("run did not finish: %s", output.String())
	}
}

// TestAutonomousReturnBoundsTheJoinOnAnUnblockableCarrier pins the timeout arm
// of the exit join: on a carrier whose Close cannot unblock a parked read (the
// Linux inherited stdio pipe measured in the plan), the run must still return,
// because nothing else can release the reader goroutine before the run ends.
// Deleting the readerJoinTimeout arm parks this test's run in the join.
func TestAutonomousReturnBoundsTheJoinOnAnUnblockableCarrier(t *testing.T) {
	// The run writes progress.md and result.json into the working directory.
	t.Chdir(t.TempDir())

	// release is closed only after the run has returned, so the parked reader
	// goroutine exits with this test and cannot unblock the join before it.
	release := make(chan struct{})
	defer close(release)
	input := unblockableReader{release: release}
	var output bytes.Buffer
	envelope := protocol.AgentInputRecord{
		Type:  protocol.AgentInputRecordTaskInput,
		Goal:  "goal",
		Input: json.RawMessage(`{"steps":1,"step_ms":0}`),
	}
	runDone := make(chan error, 1)
	go func() { runDone <- runAutonomous(bufio.NewReader(input), input, &output, envelope) }()

	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("runAutonomous: %v", err)
		}
	case <-time.After(testHangGuard):
		t.Fatal("runAutonomous did not return: the input cannot be unblocked and the join was not bounded")
	}
}

// unblockableReader models the Linux inherited stdio carrier measured in the
// plan: Read parks until release is closed, and Close does not unblock it.
type unblockableReader struct{ release <-chan struct{} }

func (reader unblockableReader) Read([]byte) (int, error) {
	<-reader.release
	return 0, io.EOF
}

func (unblockableReader) Close() error { return nil }

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
