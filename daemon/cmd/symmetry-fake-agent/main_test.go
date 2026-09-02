package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	process := startFixture(t, `{"goal":"write tests"}`+"\n", nil)
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

func TestFailWritesStderrAndExitsNonZero(t *testing.T) {
	process := startFixture(t, `{"input":{"mode":"fail"}}`+"\n", nil)
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

func TestWaitInputReadsFollowUpAndCompletes(t *testing.T) {
	process := startFixture(t, `{"input":{"mode":"wait_input"}}`+"\n", nil)
	defer closeStdin(t, process)

	first := readEvent(t, process.output)
	if first != (event{Type: "progress", Message: "started"}) {
		t.Fatalf("first event = %#v", first)
	}
	waiting := readEvent(t, process.output)
	if waiting.Type != "waiting_for_input" || waiting.Question == "" {
		t.Fatalf("waiting event = %#v", waiting)
	}
	if _, err := io.WriteString(process.stdin, `{"answer":"main"}`+"\n"); err != nil {
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

func TestSlowCancellationStopsTheFixture(t *testing.T) {
	output := newEventCollector()
	process, err := execution.NewRunner().Start(
		context.Background(),
		execution.Invocation{
			Program:                fixtureBinary(t),
			InitialInput:           []byte(`{"input":{"mode":"slow"}}` + "\n"),
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
	process := startFixture(t, `{"mode":"spawn_child"}`+"\n", nil)
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
	t.Helper()
	path := fixtureBinary(t)
	command := exec.Command(path)
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
