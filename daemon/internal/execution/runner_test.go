package execution

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStartPassesArgumentsDirectlyWithoutShell(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	process := startHelper(t, sink, "args", "plain value", "value with spaces", "$(not-a-command)", "a&b")
	result := waitForResult(t, process)

	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0; wait error = %v", result.ExitCode, result.WaitError)
	}
	if got, want := sink.output(Stdout), "plain value\x1fvalue with spaces\x1f$(not-a-command)\x1fa&b"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestStartDrainsStdoutAndStderrBeyondSixtyFourKiB(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	process := startHelper(t, sink, "streams", "196731")
	result := waitForResult(t, process)

	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0; wait error = %v", result.ExitCode, result.WaitError)
	}
	if got := len(sink.output(Stdout)); got != 196731 {
		t.Fatalf("stdout length = %d, want 196731", got)
	}
	if got := len(sink.output(Stderr)); got != 196731 {
		t.Fatalf("stderr length = %d, want 196731", got)
	}
	sink.requireStrictSequence(t)
}

func TestSlowSinkAppliesBoundedBackpressureWithoutDeadlockingPipes(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{delay: 5 * time.Millisecond}
	process := startHelper(t, sink, "stdout", "3145728")
	result := waitForResult(t, process)

	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0; wait error = %v", result.ExitCode, result.WaitError)
	}
	if got := len(sink.output(Stdout)); got != 3145728 {
		t.Fatalf("stdout length = %d, want 3145728", got)
	}
}

func TestWriteInputAppendsAfterInitialInput(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	invocation := helperInvocation("stdin-once", "11")
	invocation.InitialInput = []byte("first")
	process, err := NewRunner().Start(context.Background(), invocation, sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := process.WriteInput([]byte("second")); err != nil {
		t.Fatalf("WriteInput() error = %v", err)
	}
	result := waitForResult(t, process)

	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0; wait error = %v", result.ExitCode, result.WaitError)
	}
	if got, want := sink.output(Stdout), "firstsecond"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestWaitReportsNonZeroExit(t *testing.T) {
	t.Parallel()

	process := startHelper(t, &recordingSink{}, "exit", "23")
	result := waitForResult(t, process)

	if result.ExitCode != 23 {
		t.Fatalf("exit code = %d, want 23", result.ExitCode)
	}
	if result.WaitError == nil {
		t.Fatal("WaitError = nil, want non-nil")
	}
}

func TestContextCancellationTerminatesTheProcessTree(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	process, err := NewRunner().Start(ctx, helperInvocation("wait"), &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	cancel()
	result := waitForResult(t, process)
	if !result.Terminated {
		t.Fatal("Terminated = false, want true")
	}
}

func TestTerminateIsIdempotent(t *testing.T) {
	t.Parallel()

	process := startHelper(t, &recordingSink{}, "wait")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var waitGroup sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			errors <- process.Terminate(ctx, 100*time.Millisecond)
		}()
	}
	waitGroup.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatalf("Terminate() error = %v", err)
		}
	}

	if result := waitForResult(t, process); !result.Terminated {
		t.Fatal("Terminated = false, want true")
	}
}

func TestTerminateCancelsABlockingSinkAndCompletesDrain(t *testing.T) {
	t.Parallel()

	sink := newBlockingSink()
	process := startHelper(t, sink, "stdout", "3145728")
	select {
	case <-sink.started:
	case <-time.After(5 * time.Second):
		t.Fatal("sink did not receive an output event")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := process.Terminate(ctx, 0); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	result := waitForResult(t, process)
	if !result.Terminated {
		t.Fatal("Terminated = false, want true")
	}
	if !result.OutputTruncated {
		t.Fatal("OutputTruncated = false, want true after termination during drain")
	}
	select {
	case <-sink.cancelled:
	case <-time.After(time.Second):
		t.Fatal("blocking sink context was not cancelled")
	}
}

func TestStartRejectsAnAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	process, err := NewRunner().Start(ctx, helperInvocation("wait"), &recordingSink{})
	if err == nil {
		t.Fatal("Start() error = nil, want context cancellation error")
	}
	if process != nil {
		t.Fatal("Start() process is non-nil for an already cancelled context")
	}
}

func TestCloseInputAfterInitialInputSupportsEOFDrivenAgent(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	invocation := helperInvocation("stdin-eof")
	invocation.InitialInput = []byte("complete request")
	invocation.CloseInputAfterInitial = true
	process, err := NewRunner().Start(context.Background(), invocation, sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	result := waitForResult(t, process)
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0; wait error = %v", result.ExitCode, result.WaitError)
	}
	if got, want := sink.output(Stdout), "complete request"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestCloseInputEndsAnInteractiveInputStream(t *testing.T) {
	t.Parallel()

	sink := &recordingSink{}
	process := startHelper(t, sink, "stdin-eof")
	if err := process.WriteInput([]byte("human follow-up")); err != nil {
		t.Fatalf("WriteInput() error = %v", err)
	}
	if err := process.CloseInput(); err != nil {
		t.Fatalf("CloseInput() error = %v", err)
	}
	result := waitForResult(t, process)
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0; wait error = %v", result.ExitCode, result.WaitError)
	}
	if got, want := sink.output(Stdout), "human follow-up"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestResultSuccessRequiresNoTermination(t *testing.T) {
	result := Result{ExitCode: 0, Terminated: true}
	if result.Success() {
		t.Fatal("terminated zero-exit result reported success")
	}
}

func TestBuildEnvironmentDoesNotLeakSymmetryVariables(t *testing.T) {
	t.Setenv("SYMMETRY_PRIVATE", "do-not-pass")
	t.Setenv("SYMMETRY_ALLOWED", "pass-me")

	environment, err := BuildEnvironment("SYMMETRY_ALLOWED")
	if err != nil {
		t.Fatalf("BuildEnvironment() error = %v", err)
	}
	values := environmentValues(environment)
	if _, found := values["SYMMETRY_PRIVATE"]; found {
		t.Fatal("SYMMETRY_PRIVATE unexpectedly present")
	}
	if got := values["SYMMETRY_ALLOWED"]; got != "pass-me" {
		t.Fatalf("SYMMETRY_ALLOWED = %q, want pass-me", got)
	}
}

func TestBuildEnvironmentRejectsControlCredentials(t *testing.T) {
	t.Setenv("SYMMETRY_CONTROL_TOKEN", "secret")

	if _, err := BuildEnvironment("SYMMETRY_CONTROL_TOKEN"); err == nil {
		t.Fatal("BuildEnvironment() error = nil, want rejected control credential")
	}
}

func TestBuildEnvironmentRejectsAllReservedSymmetryTokens(t *testing.T) {
	for _, key := range []string{
		"SYMMETRY_ENROLLMENT_TOKEN",
		"SYMMETRY_OPERATOR_TOKEN",
		"SYMMETRY_MACHINE_TOKEN",
		"SYMMETRY_ANYTHINGTOKEN",
		"SYMMETRY_DEPLOY_SECRET",
	} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, "secret")
			if _, err := BuildEnvironment(key); err == nil {
				t.Fatalf("BuildEnvironment(%q) error = nil, want reserved credential rejection", key)
			}
		})
	}
}

func TestStartResolvesABareTrustedExecutable(t *testing.T) {
	t.Setenv("PATH", filepath.Dir(os.Args[0]))

	sink := &recordingSink{}
	invocation := helperInvocation("args", "bare executable")
	invocation.Program = filepath.Base(os.Args[0])
	process, err := NewRunner().Start(context.Background(), invocation, sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	result := waitForResult(t, process)
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0; wait error = %v", result.ExitCode, result.WaitError)
	}
	if got, want := sink.output(Stdout), "bare executable"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}

func TestStartDoesNotInheritTheDaemonEnvironment(t *testing.T) {
	t.Setenv("UNEXPECTED_DAEMON_ENV", "must-not-reach-child")

	sink := &recordingSink{}
	process := startHelper(t, sink, "environment", "UNEXPECTED_DAEMON_ENV")
	result := waitForResult(t, process)
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0; wait error = %v", result.ExitCode, result.WaitError)
	}
	if got := sink.output(Stdout); got != "" {
		t.Fatalf("child inherited UNEXPECTED_DAEMON_ENV = %q", got)
	}
}

func startHelper(t *testing.T, sink Sink, mode string, arguments ...string) *Process {
	t.Helper()
	process, err := NewRunner().Start(context.Background(), helperInvocation(mode, arguments...), sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return process
}

func helperInvocation(mode string, arguments ...string) Invocation {
	return Invocation{
		Program: os.Args[0],
		Args:    append([]string{"-test.run=^TestHelperProcess$", "--", mode}, arguments...),
		Env:     append(minimalEnvironment(), "GO_WANT_HELPER_PROCESS=1"),
	}
}

func minimalEnvironment() []string {
	keys := []string{"SystemRoot", "SYSTEMROOT", "WINDIR", "ComSpec", "PATHEXT", "TMP", "TEMP"}
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		if value, found := os.LookupEnv(key); found {
			values = append(values, key+"="+value)
		}
	}
	return values
}

func waitForResult(t *testing.T, process *Process) Result {
	t.Helper()
	results := make(chan Result, 1)
	go func() { results <- process.Wait() }()
	select {
	case result := <-results:
		return result
	case <-time.After(10 * time.Second):
		t.Fatal("Process.Wait() did not return")
		return Result{}
	}
}

type recordingSink struct {
	delay  time.Duration
	notify chan struct{}

	mutex  sync.Mutex
	events []Event
}

type blockingSink struct {
	started       chan struct{}
	cancelled     chan struct{}
	startedOnce   sync.Once
	cancelledOnce sync.Once
}

func newBlockingSink() *blockingSink {
	return &blockingSink{
		started:   make(chan struct{}),
		cancelled: make(chan struct{}),
	}
}

func (sink *blockingSink) Handle(ctx context.Context, event Event) error {
	sink.startedOnce.Do(func() { close(sink.started) })
	<-ctx.Done()
	sink.cancelledOnce.Do(func() { close(sink.cancelled) })
	return ctx.Err()
}

func (sink *recordingSink) Handle(ctx context.Context, event Event) error {
	if sink.delay > 0 {
		select {
		case <-time.After(sink.delay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	sink.mutex.Lock()
	sink.events = append(sink.events, event)
	sink.mutex.Unlock()
	if sink.notify != nil {
		select {
		case sink.notify <- struct{}{}:
		default:
		}
	}
	return nil
}

func (sink *recordingSink) output(stream Stream) string {
	sink.mutex.Lock()
	defer sink.mutex.Unlock()

	var output strings.Builder
	for _, event := range sink.events {
		if event.Stream == stream {
			output.Write(event.Data)
		}
	}
	return output.String()
}

func (sink *recordingSink) requireStrictSequence(t *testing.T) {
	t.Helper()
	sink.mutex.Lock()
	defer sink.mutex.Unlock()

	for index, event := range sink.events {
		if want := uint64(index + 1); event.Sequence != want {
			t.Fatalf("event %d sequence = %d, want %d", index, event.Sequence, want)
		}
	}
}

func environmentValues(environment []string) map[string]string {
	values := make(map[string]string, len(environment))
	for _, entry := range environment {
		key, value, found := strings.Cut(entry, "=")
		if found {
			values[key] = value
		}
	}
	return values
}

func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	arguments := os.Args
	separator := -1
	for index, argument := range arguments {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator == -1 || separator+1 >= len(arguments) {
		os.Exit(2)
	}

	mode := arguments[separator+1]
	values := arguments[separator+2:]
	switch mode {
	case "args":
		_, _ = io.WriteString(os.Stdout, strings.Join(values, "\x1f"))
	case "streams":
		count := helperCount(values)
		writeRepeated(os.Stdout, 'o', count)
		writeRepeated(os.Stderr, 'e', count)
	case "stdout":
		writeRepeated(os.Stdout, 'o', helperCount(values))
	case "stdin-once":
		count := helperCount(values)
		input := make([]byte, count)
		if _, err := io.ReadFull(os.Stdin, input); err != nil {
			os.Exit(3)
		}
		_, _ = os.Stdout.Write(input)
	case "stdin-eof":
		input, err := io.ReadAll(os.Stdin)
		if err != nil {
			os.Exit(3)
		}
		_, _ = os.Stdout.Write(input)
	case "exit":
		os.Exit(helperCount(values))
	case "environment":
		if len(values) != 1 {
			os.Exit(2)
		}
		_, _ = io.WriteString(os.Stdout, os.Getenv(values[0]))
	case "tree-parent":
		child := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "--", "tree-child")
		child.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(5)
		}
		waitForever()
	case "tree-child":
		_, _ = fmt.Fprintf(os.Stdout, "child:%d", os.Getpid())
		waitForever()
	case "root-exits-child-holding-stdout":
		child := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "--", "tree-child")
		child.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(5)
		}
		time.Sleep(100 * time.Millisecond)
	case "wait":
		waitForever()
	default:
		fmt.Fprintf(os.Stderr, "unknown helper mode %q", mode)
		os.Exit(2)
	}

	os.Exit(0)
}

func helperCount(arguments []string) int {
	if len(arguments) != 1 {
		os.Exit(2)
	}
	count, err := strconv.Atoi(arguments[0])
	if err != nil || count < 0 {
		os.Exit(2)
	}
	return count
}

func writeRepeated(writer io.Writer, value byte, count int) {
	buffer := make([]byte, 32*1024)
	for index := range buffer {
		buffer[index] = value
	}
	for count > 0 {
		length := min(count, len(buffer))
		if _, err := writer.Write(buffer[:length]); err != nil {
			os.Exit(4)
		}
		count -= length
	}
}

func waitForever() {
	for {
		time.Sleep(time.Hour)
	}
}
