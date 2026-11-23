//go:build windows

package execution

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
)

var (
	runnerTestKernel32         = syscall.NewLazyDLL("kernel32.dll")
	runnerTestGetConsoleWindow = runnerTestKernel32.NewProc("GetConsoleWindow")
)

func TestRunnerStartsWindowsChildWithoutConsoleWindow(t *testing.T) {
	const expectedInput = "headless-stdin"

	environment, err := BuildEnvironment()
	if err != nil {
		t.Fatalf("build child environment: %v", err)
	}
	sink := &recordingSink{}
	process, err := NewRunner().Start(context.Background(), Invocation{
		Program: os.Args[0],
		Args:    []string{"-test.run=^TestHeadlessRunnerChild$", "--"},
		Env: append(environment,
			"GO_WANT_HEADLESS_RUNNER_CHILD=1",
		),
		InitialInput:           []byte(expectedInput),
		CloseInputAfterInitial: true,
	}, sink)
	if err != nil {
		if process != nil {
			cleanupRunnerTestProcess(t, process)
		}
		t.Fatalf("Start() error = %v", err)
	}
	t.Cleanup(func() { cleanupRunnerTestProcess(t, process) })

	if process.Identity == "" {
		t.Fatal("process identity is empty")
	}
	result := waitForResult(t, process)
	if !result.Success() {
		t.Fatalf("headless child result = %#v, want successful completion", result)
	}

	stdout := sink.output(Stdout)
	if !strings.Contains(stdout, "stdout-console=0") {
		t.Fatalf("stdout = %q, want GetConsoleWindow() == 0", stdout)
	}
	if !strings.Contains(stdout, "stdin="+expectedInput) {
		t.Fatalf("stdout = %q, want stdin marker", stdout)
	}
	if stderr := sink.output(Stderr); !strings.Contains(stderr, "stderr-console=0") {
		t.Fatalf("stderr = %q, want GetConsoleWindow() == 0", stderr)
	}
}

func TestNativeRunnerPersistsIdentityBeforeChildResume(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-started")
	invocation := helperInvocation("startup-marker")
	invocation.Env = append(invocation.Env, "GO_RUNNER_START_MARKER="+marker)
	persisted := false
	invocation.PersistProcess = func(pid int, identity string) error {
		if pid <= 0 || identity == "" {
			t.Fatalf("process identity = (%d, %q), want non-empty identity", pid, identity)
		}
		if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("child marker before PersistProcess = %v, want os.ErrNotExist", err)
		}
		persisted = true
		return nil
	}

	process, err := NewRunner().Start(context.Background(), invocation, &recordingSink{})
	if err != nil {
		if process != nil {
			cleanupRunnerTestProcess(t, process)
		}
		t.Fatalf("Start() error = %v", err)
	}
	result := waitForResult(t, process)
	if !persisted {
		t.Fatal("PersistProcess was not called")
	}
	if !result.Success() {
		t.Fatalf("startup marker result = %#v, want successful completion", result)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("child start marker error = %v", err)
	}
}

func TestNativeRunnerCleansUpWhenProcessPersistenceFails(t *testing.T) {
	want := errors.New("persist process marker failed")
	invocation := helperInvocation("wait")
	invocation.PersistProcess = func(int, string) error { return want }

	process, err := NewRunner().Start(context.Background(), invocation, &recordingSink{})
	if process == nil {
		t.Fatalf("Start() returned nil process after post-launch failure: %v", err)
	}
	if !errors.Is(err, want) {
		t.Fatalf("Start() error = %v, want %v", err, want)
	}
	result := waitForResult(t, process)
	if !result.Terminated {
		t.Fatalf("cleanup result = %#v, want terminated process", result)
	}
}

func cleanupRunnerTestProcess(t *testing.T, process *Process) {
	t.Helper()
	if process == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), testHangGuard)
	terminationErr := process.Terminate(ctx, 0)
	cancel()
	if terminationErr == nil {
		return
	}

	// Terminate owns one shutdown operation and may outlive a caller whose
	// context expired. Rejoin that operation once with a fresh bounded context
	// before failing the test; never retry the operation indefinitely.
	retryContext, retryCancel := context.WithTimeout(context.Background(), testHangGuard)
	defer retryCancel()
	retryErr := process.Terminate(retryContext, 0)
	select {
	case <-process.resultDone:
	case <-retryContext.Done():
		select {
		case <-process.resultDone:
		default:
			t.Fatalf(
				"cleanup process did not finish before deadline: %v; termination error: %v; retry error: %v",
				retryContext.Err(), terminationErr, retryErr,
			)
		}
	}
	t.Fatalf("cleanup process termination: %v; retry error: %v", terminationErr, retryErr)
}

func TestHeadlessRunnerChild(t *testing.T) {
	if os.Getenv("GO_WANT_HEADLESS_RUNNER_CHILD") != "1" {
		return
	}

	const expectedInput = "headless-stdin"
	input := make([]byte, len(expectedInput))
	if _, err := io.ReadFull(os.Stdin, input); err != nil {
		fmt.Fprintf(os.Stderr, "read stdin: %v", err)
		os.Exit(2)
	}
	if string(input) != expectedInput {
		fmt.Fprintf(os.Stderr, "stdin = %q, want %q", input, expectedInput)
		os.Exit(3)
	}

	consoleWindow, _, _ := runnerTestGetConsoleWindow.Call()
	_, _ = fmt.Fprintf(os.Stdout, "stdout-console=%d stdin=%s\n", consoleWindow, input)
	_, _ = fmt.Fprintf(os.Stderr, "stderr-console=%d\n", consoleWindow)
}

func TestTerminateStopsDescendantProcessOnWindows(t *testing.T) {
	sink := &recordingSink{notify: make(chan struct{}, 1)}
	process := startHelper(t, sink, "tree-parent")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testHangGuard)
		defer cancel()
		_ = process.Terminate(ctx, 0)
	})
	childPIDText := waitForStdout(t, sink)
	childPID, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(childPIDText, "child:")))
	if err != nil {
		t.Fatalf("child PID %q is not an integer: %v", childPIDText, err)
	}
	if exists, details := windowsProcessExists(childPID); !exists {
		t.Fatalf("child process %d was not running before termination; tasklist output: %q", childPID, details)
	}

	ctx, cancel := context.WithTimeout(context.Background(), testHangGuard)
	defer cancel()
	if err := process.Terminate(ctx, 100*time.Millisecond); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	_ = waitForResult(t, process)
	if exists, details := windowsProcessExists(childPID); exists {
		t.Fatalf("descendant process %d is still running; tasklist output: %q", childPID, details)
	}
}

func TestRootExitDoesNotLeaveADescendantHoldingStdoutOnWindows(t *testing.T) {
	sink := &recordingSink{notify: make(chan struct{}, 1)}
	process := startHelper(t, sink, "root-exits-child-holding-stdout")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), testHangGuard)
		defer cancel()
		_ = process.Terminate(ctx, 0)
	})

	childPIDText := waitForStdout(t, sink)
	childPID, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(childPIDText, "child:")))
	if err != nil {
		t.Fatalf("child PID %q is not an integer: %v", childPIDText, err)
	}
	result := waitForResult(t, process)
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0; wait error = %v", result.ExitCode, result.WaitError)
	}
	if !result.Success() {
		t.Fatalf("result = %+v, want successful root completion", result)
	}
	if exists, details := windowsProcessExists(childPID); exists {
		t.Fatalf("descendant process %d survived root exit; tasklist output: %q", childPID, details)
	}
}

func waitForStdout(t *testing.T, sink *recordingSink) string {
	t.Helper()
	timer := time.NewTimer(testHangGuard)
	defer timer.Stop()
	for {
		if output := sink.output(Stdout); strings.HasPrefix(output, "child:") {
			return output
		}
		select {
		case <-sink.notify:
		case <-timer.C:
			t.Fatalf("helper child PID was not observed before the %s hang guard expired", testHangGuard)
			return ""
		}
	}
}

func windowsProcessExists(pid int) (bool, string) {
	command := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH")
	if err := platform.ConfigureProcess(command); err != nil {
		return false, err.Error()
	}
	output, err := command.Output()
	if err != nil {
		return false, err.Error()
	}
	return strings.Contains(string(output), fmt.Sprintf("\"%d\"", pid)), string(output)
}

var errDurableHandoffRouteReached = errors.New("durable handoff route reached")

func TestDefaultWindowsLauncherUsesRecorderHandoffCapability(t *testing.T) {
	handoffAttempts := 0
	previous := attachSuspendedProcessWithHandoff
	attachSuspendedProcessWithHandoff = func(context.Context, *platform.SuspendedProcess, platform.SupervisorHandoffCallbacks) (platform.Containment, authority.SupervisorHandoff, string, error) {
		handoffAttempts++
		return nil, authority.SupervisorHandoff{}, "", errDurableHandoffRouteReached
	}
	t.Cleanup(func() { attachSuspendedProcessWithHandoff = previous })

	complete := completeHandoffInvocation(helperInvocation("args"))
	process, err := NewRunner().Start(context.Background(), complete, &recordingSink{})
	if process != nil {
		t.Cleanup(func() { cleanupRunnerTestProcess(t, process) })
	}
	if !errors.Is(err, errDurableHandoffRouteReached) {
		t.Fatalf("complete handoff recorder did not reach the durable route: process=%v err=%v", process, err)
	}
	if handoffAttempts != 1 {
		t.Fatalf("durable handoff attempts = %d, want 1", handoffAttempts)
	}

	process, err = NewRunner().Start(context.Background(), helperInvocation("args"), &recordingSink{})
	if err != nil {
		if process != nil {
			cleanupRunnerTestProcess(t, process)
		}
		t.Fatalf("capability-free launch error = %v", err)
	}
	t.Cleanup(func() { cleanupRunnerTestProcess(t, process) })
	if result := waitForResult(t, process); !result.Success() {
		t.Fatalf("capability-free launch result = %#v, want success", result)
	}
	if handoffAttempts != 1 {
		t.Fatalf("capability-free invocation reached the durable route %d times, want 0", handoffAttempts-1)
	}
}
