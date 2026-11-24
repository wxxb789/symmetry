package execution

import (
	"bytes"
	"context"
	"errors"
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

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

// testHangGuard bounds how long a test waits for a scripted runner, sink or
// child process to publish an observable. It only turns a hung harness into a
// failure; it is never a statement about how fast the product should be, so a
// loaded host cannot turn a slow settle into a verdict about the runner.
const testHangGuard = 30 * time.Second

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
	invocation := helperInvocation("streams", "196731")
	process := startHelperInvocation(t, sink, invocation)
	result := waitForResult(t, process)

	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0; wait error = %v", result.ExitCode, result.WaitError)
	}
	if got := len(sink.output(Stdout)); got != 196731 {
		t.Fatalf("stdout length = %d, want 196731", got)
	}
	if got := len(drainCoverageRuntimeDiagnostics(t, invocation, sink.output(Stderr))); got != 196731 {
		t.Fatalf("stderr length = %d, want 196731", got)
	}
	sink.requireStrictSequence(t)
}

func TestCoverageRuntimeDiagnosticStripBoundary(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("e", 196731)
	documented := []string{
		"error: coverage meta-data emit failed: rename <merge-dir>\\tmp.covmeta.5bde9a34b0182dd9672696b24ef5f92c: Access is denied.",
		"error: coverage counter data emit failed: writing <merge-dir>\\covcounters.5bde9a34b0182dd9672696b24ef5f92c: Access is denied.",
		"warning: GOCOVERDIR not set, no coverage data emitted",
	}

	testCases := []struct {
		name         string
		stream       string
		wantStripped int
		wantIntact   bool
	}{
		{"payload alone is untouched", payload, 0, true},
		{"payload plus a documented line with one newline", payload + documented[0] + "\n", 1, true},
		{"payload plus a documented line with two newlines", payload + documented[0] + "\n\n", 1, true},
		{"payload plus a documented counter line", payload + documented[1] + "\n", 1, true},
		{"payload plus the documented warning line", payload + documented[2] + "\n", 1, true},
		{"payload plus two documented lines", payload + documented[0] + "\n" + documented[2] + "\n\n", 2, true},
		{"payload plus a foreign error line still fails", payload + "error: surprising helper failure\n", 0, false},
		{"payload plus a documented line then a foreign line still fails", payload + documented[0] + "\n" + "error: surprising helper failure\n", 0, false},
		{"payload plus a documented line without a terminator still fails", payload + documented[0], 0, false},
		{"payload plus a newline then a documented line still fails", payload + "\n" + documented[0] + "\n", 1, false},
		{"a shortened payload plus a documented line still fails", payload[:len(payload)-1] + documented[0] + "\n", 1, false},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			stripped, diagnostics := stripCoverageRuntimeDiagnostics(testCase.stream)
			if len(diagnostics) != testCase.wantStripped {
				t.Fatalf("stripped lines = %d, want %d: %q", len(diagnostics), testCase.wantStripped, diagnostics)
			}
			if intact := stripped == payload; intact != testCase.wantIntact {
				t.Fatalf("stripped stream is the payload = %t, want %t; length = %d, want %d", intact, testCase.wantIntact, len(stripped), len(payload))
			}
		})
	}
}

func TestCoverageRuntimeDiagnosticStripRequiresForwardedCoverageDirectory(t *testing.T) {
	t.Parallel()

	payload := strings.Repeat("e", 196731)
	stream := payload + "error: coverage meta-data emit failed: Access is denied.\n"

	testCases := []struct {
		name        string
		environment []string
		wantDrained bool
	}{
		{"helper environment without GOCOVERDIR keeps the line", []string{"GO_WANT_HELPER_PROCESS=1"}, false},
		{"helper environment with an empty GOCOVERDIR keeps the line", []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR="}, false},
		{"helper environment with GOCOVERDIR set drains the line", []string{"GO_WANT_HELPER_PROCESS=1", "GOCOVERDIR=<merge-dir>"}, true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			invocation := Invocation{Env: testCase.environment}
			if drained := drainCoverageRuntimeDiagnostics(t, invocation, stream) == payload; drained != testCase.wantDrained {
				t.Fatalf("drained = %t, want %t", drained, testCase.wantDrained)
			}
		})
	}
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

func TestResultDoneClosesAfterTerminationAndWaitIsReusable(t *testing.T) {
	t.Parallel()

	process := startHelper(t, &recordingSink{}, "wait")
	resultDone := process.ResultDone()
	select {
	case <-resultDone:
		t.Fatal("ResultDone() closed before process completion")
	default:
	}

	ctx, cancel := context.WithTimeout(context.Background(), testHangGuard)
	defer cancel()
	if err := process.Terminate(ctx, 100*time.Millisecond); err != nil && !strings.Contains(err.Error(), "Linux descendant containment is unproven") {
		t.Fatalf("Terminate() error = %v", err)
	}

	select {
	case <-resultDone:
	case <-time.After(testHangGuard):
		t.Fatal("ResultDone() did not close after process termination")
	}
	first := process.Wait()
	second := process.Wait()
	if first != second {
		t.Fatalf("repeated Wait() results differ: first = %#v, second = %#v", first, second)
	}
}

func TestResultDoneNilReceiver(t *testing.T) {
	var process *Process
	if resultDone := process.ResultDone(); resultDone != nil {
		t.Fatalf("ResultDone() = %v, want nil", resultDone)
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
	ctx, cancel := context.WithTimeout(context.Background(), testHangGuard)
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

func TestTerminateFallsBackToRootKillWhenForcedContainmentFails(t *testing.T) {
	forceFailure := errors.New("forced containment termination failed")
	runner := Runner{
		configureProcess: func(*exec.Cmd) error { return nil },
		attachProcess: func(*os.Process) (platform.Containment, string, error) {
			return &forceFailingContainment{forceErr: forceFailure}, "bound:process", nil
		},
	}
	process, err := runner.Start(context.Background(), helperInvocation("wait"), &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testHangGuard)
	defer cancel()
	err = process.Terminate(ctx, 0)
	if !errors.Is(err, forceFailure) {
		t.Fatalf("Terminate() error = %v, want forced containment error", err)
	}
	result := waitForResult(t, process)
	if !result.Terminated || !errors.Is(result.TerminationError, forceFailure) {
		t.Fatalf("fallback root kill result = %#v", result)
	}
}

func TestTerminateCancelsABlockingSinkAndCompletesDrain(t *testing.T) {
	t.Parallel()

	sink := newBlockingSink()
	process := startHelper(t, sink, "stdout", "3145728")
	select {
	case <-sink.started:
	case <-time.After(testHangGuard):
		t.Fatal("sink did not receive an output event")
	}

	ctx, cancel := context.WithTimeout(context.Background(), testHangGuard)
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
	case <-time.After(testHangGuard):
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

func TestProcessDetailsNilReceiverHasNoIdentity(t *testing.T) {
	var process *Process
	pid, identity := process.ProcessDetails()
	if pid != 0 || identity != "" {
		t.Fatalf("ProcessDetails() = (%d, %q), want empty identity", pid, identity)
	}
}

func TestStartPersistsProcessIdentityBeforeStartingOutputReaders(t *testing.T) {
	sink := &recordingSink{}
	invocation := helperInvocation("stdout", "64")
	called := false
	invocation.PersistProcess = func(pid int, identity string) error {
		called = true
		if pid <= 0 || identity == "" {
			t.Fatalf("process identity = (%d, %q), want non-empty identity", pid, identity)
		}
		if got := sink.output(Stdout); got != "" {
			t.Fatalf("stdout delivered before process identity persistence: %q", got)
		}
		return nil
	}
	process, err := NewRunner().Start(context.Background(), invocation, sink)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if !called {
		t.Fatal("PersistProcess callback was not called")
	}
	if result := waitForResult(t, process); result.ExitCode != 0 {
		t.Fatalf("exit code = %d, wait error = %v", result.ExitCode, result.WaitError)
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

func TestWriteInputContextCancellationClosesABlockedPipeBeforeReturning(t *testing.T) {
	reader, process := newInputPipeProcess(t)
	defer reader.Close()
	defer process.CloseInput()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	writeResult := make(chan error, 1)
	go func() {
		writeResult <- process.WriteInputContext(ctx, bytes.Repeat([]byte("x"), 8*1024*1024))
	}()
	awaitPipeByte(t, reader)

	cancel()
	err := receiveInputResult(t, writeResult)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WriteInputContext() error = %v, want context cancellation", err)
	}
	if !errors.Is(err, ErrInputClosed) {
		t.Fatalf("WriteInputContext() error = %v, want ErrInputClosed", err)
	}
	if err := process.WriteInput([]byte("later")); !errors.Is(err, ErrInputClosed) {
		t.Fatalf("WriteInput() after cancelled partial write error = %v, want ErrInputClosed", err)
	}
}

func TestCloseInputClosesABlockedPipeWithoutWaitingForWriter(t *testing.T) {
	reader, process := newInputPipeProcess(t)
	defer reader.Close()
	defer process.CloseInput()

	writeResult := make(chan error, 1)
	go func() {
		writeResult <- process.WriteInput(bytes.Repeat([]byte("x"), 8*1024*1024))
	}()
	awaitPipeByte(t, reader)

	closeResults := make(chan error, 2)
	for range 2 {
		go func() { closeResults <- process.CloseInput() }()
	}
	for range 2 {
		if err := receiveInputResult(t, closeResults); err != nil {
			t.Fatalf("CloseInput() error = %v", err)
		}
	}
	if err := receiveInputResult(t, writeResult); !errors.Is(err, ErrInputClosed) {
		t.Fatalf("WriteInput() after concurrent CloseInput error = %v, want ErrInputClosed", err)
	}
}

func TestWriteInputContextCancellationWhileQueuedDoesNotCloseAnotherWrite(t *testing.T) {
	reader, process := newInputPipeProcess(t)
	defer reader.Close()
	defer process.CloseInput()

	firstResult := make(chan error, 1)
	go func() {
		firstResult <- process.WriteInput(bytes.Repeat([]byte("x"), 8*1024*1024))
	}()
	awaitPipeByte(t, reader)

	ctx, cancel := context.WithCancel(context.Background())
	queuedResult := make(chan error, 1)
	go func() { queuedResult <- process.WriteInputContext(ctx, []byte("queued")) }()
	cancel()
	if err := receiveInputResult(t, queuedResult); !errors.Is(err, context.Canceled) {
		t.Fatalf("queued WriteInputContext() error = %v, want context cancellation", err)
	}
	process.stdinMutex.Lock()
	inputStillOpen := process.stdin != nil
	process.stdinMutex.Unlock()
	if !inputStillOpen {
		t.Fatal("queued cancellation closed the active writer's input transport")
	}

	if err := process.CloseInput(); err != nil {
		t.Fatalf("CloseInput() error = %v", err)
	}
	if err := receiveInputResult(t, firstResult); !errors.Is(err, ErrInputClosed) {
		t.Fatalf("first WriteInput() error = %v, want ErrInputClosed", err)
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
	return startHelperInvocation(t, sink, helperInvocation(mode, arguments...))
}

func startHelperInvocation(t *testing.T, sink Sink, invocation Invocation) *Process {
	t.Helper()
	process, err := NewRunner().Start(context.Background(), invocation, sink)
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

func descendantHoldingStdoutInvocation(t *testing.T) Invocation {
	t.Helper()
	invocation := helperInvocation("root-exits-child-holding-stdout")
	invocation.Env = append(invocation.Env, "GO_RUNNER_DESCENDANT_MARKER="+filepath.Join(t.TempDir(), "descendant-stdout-written"))
	return invocation
}

func completeHandoffInvocation(invocation Invocation) Invocation {
	invocation.PrepareSupervisorHandoff = func(authority.SupervisorHandoff) error { return nil }
	invocation.BindSupervisorHandoff = func(authority.SupervisorHandoff, int, string) error { return nil }
	invocation.CommitSupervisorHandoff = func(authority.SupervisorHandoff, time.Time) error { return nil }
	return invocation
}

func minimalEnvironment() []string {
	// GOCOVERDIR is forwarded because the helper child is this test binary
	// re-executed, and the coverage runtime's "not set" warning breaks assertions.
	keys := []string{"SystemRoot", "SYSTEMROOT", "WINDIR", "ComSpec", "PATHEXT", "TMP", "TEMP", "GOCOVERDIR"}
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		if value, found := os.LookupEnv(key); found {
			values = append(values, key+"="+value)
		}
	}
	return values
}

// Coverage runtime exit hooks append one of these lines to an instrumented helper child's
// stream when it loses the shared covmeta rename race; nothing else may be stripped.
var coverageRuntimeDiagnosticPrefixes = [...]string{
	"error: coverage meta-data emit failed: ",
	"error: coverage counter data emit failed: ",
	"warning: GOCOVERDIR not set, no coverage data emitted",
}

func drainCoverageRuntimeDiagnostics(t *testing.T, invocation Invocation, output string) string {
	t.Helper()
	if !helperForwardsCoverageDirectory(invocation) {
		return output
	}
	drained, diagnostics := stripCoverageRuntimeDiagnostics(output)
	for _, diagnostic := range diagnostics {
		t.Logf("stripped coverage runtime diagnostic (%d bytes): %s", len(diagnostic), diagnostic)
	}
	return drained
}

func helperForwardsCoverageDirectory(invocation Invocation) bool {
	for _, entry := range invocation.Env {
		if key, value, found := strings.Cut(entry, "="); found && key == "GOCOVERDIR" {
			return value != ""
		}
	}
	return false
}

// The runtime's detail can carry its own newline and the format adds one more, so a
// stripped block is a documented line plus the newline terminators that follow it.
func stripCoverageRuntimeDiagnostics(output string) (string, []string) {
	var stripped []string
	remaining := output
	for strings.HasSuffix(remaining, "\n") {
		index := -1
		for _, prefix := range coverageRuntimeDiagnosticPrefixes {
			if found := strings.LastIndex(remaining, prefix); found > index {
				index = found
			}
		}
		if index < 0 {
			break
		}
		if !onlyCoverageRuntimeDiagnosticLines(remaining[index:]) {
			return output, nil
		}
		stripped = append(stripped, strings.Split(strings.TrimRight(remaining[index:], "\n"), "\n")...)
		remaining = remaining[:index]
	}
	return remaining, stripped
}

func onlyCoverageRuntimeDiagnosticLines(block string) bool {
	for _, line := range strings.Split(strings.TrimRight(block, "\n"), "\n") {
		matched := false
		for _, prefix := range coverageRuntimeDiagnosticPrefixes {
			if strings.HasPrefix(line, prefix) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func newInputPipeProcess(t *testing.T) (*os.File, *Process) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	process := &Process{
		stdin:            writer,
		stdinWritePermit: make(chan struct{}, 1),
	}
	process.stdinWritePermit <- struct{}{}
	return reader, process
}

func awaitPipeByte(t *testing.T, reader *os.File) {
	t.Helper()
	readResult := make(chan error, 1)
	go func() {
		buffer := make([]byte, 1)
		count, err := reader.Read(buffer)
		if err != nil {
			readResult <- err
			return
		}
		if count != 1 {
			readResult <- fmt.Errorf("pipe read count = %d, want 1", count)
			return
		}
		readResult <- nil
	}()
	if err := receiveInputResult(t, readResult); err != nil {
		t.Fatalf("wait for input write to enter pipe: %v", err)
	}
}

func receiveInputResult(t *testing.T, results <-chan error) error {
	t.Helper()
	select {
	case err := <-results:
		return err
	case <-time.After(testHangGuard):
		t.Fatal("input operation did not return")
		return nil
	}
}

func waitForResult(t *testing.T, process *Process) Result {
	t.Helper()
	results := make(chan Result, 1)
	go func() { results <- process.Wait() }()
	select {
	case result := <-results:
		return result
	case <-time.After(testHangGuard):
		t.Fatal("Process.Wait() did not return")
		return Result{}
	}
}

type forceFailingContainment struct{ forceErr error }

func (*forceFailingContainment) Close() error { return nil }

func (containment *forceFailingContainment) Terminate(force bool) error {
	if force {
		return containment.forceErr
	}
	return nil
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
	case "stdout-then-wait":
		_, _ = io.WriteString(os.Stdout, strings.Join(values, "\x1f"))
		waitForever()
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
	case "startup-marker":
		marker := os.Getenv("GO_RUNNER_START_MARKER")
		if marker == "" {
			os.Exit(2)
		}
		if err := os.WriteFile(marker, []byte("started"), 0o600); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(4)
		}
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
		signalDescendantStdoutWritten()
		waitForever()
	case "root-exits-child-holding-stdout":
		marker := descendantStdoutMarker()
		if marker == "" {
			os.Exit(2)
		}
		child := exec.Command(os.Args[0], "-test.run=^TestHelperProcess$", "--", "tree-child")
		child.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if err := child.Start(); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(5)
		}
		// The root's exit tears the containment tree down, so wait for the
		// grandchild's marker: a fixed sleep raced its stdout write under load.
		waitForDescendantMarker(marker)
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

func descendantStdoutMarker() string {
	return os.Getenv("GO_RUNNER_DESCENDANT_MARKER")
}

// The marker must follow the grandchild's stdout write, or the root's exit
// could still tear the tree down before the write lands in the pipe.
func signalDescendantStdoutWritten() {
	marker := descendantStdoutMarker()
	if marker == "" {
		return
	}
	if err := os.WriteFile(marker, []byte("stdout-written"), 0o600); err != nil {
		fmt.Fprint(os.Stderr, err)
		os.Exit(6)
	}
}

func waitForDescendantMarker(marker string) {
	deadline := time.Now().Add(testHangGuard)
	for {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		if time.Now().After(deadline) {
			fmt.Fprintf(os.Stderr, "descendant marker %q was not created within %s", marker, testHangGuard)
			os.Exit(6)
		}
		time.Sleep(time.Millisecond)
	}
}
