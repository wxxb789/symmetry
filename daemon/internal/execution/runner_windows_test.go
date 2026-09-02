//go:build windows

package execution

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestTerminateStopsDescendantProcessOnWindows(t *testing.T) {
	sink := &recordingSink{notify: make(chan struct{}, 1)}
	process := startHelper(t, sink, "tree-parent")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
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
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		if output := sink.output(Stdout); strings.HasPrefix(output, "child:") {
			return output
		}
		select {
		case <-sink.notify:
		case <-timer.C:
			t.Fatal("timed out waiting for helper child PID")
			return ""
		}
	}
}

func windowsProcessExists(pid int) (bool, string) {
	output, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH").Output()
	if err != nil {
		return false, err.Error()
	}
	return strings.Contains(string(output), fmt.Sprintf("\"%d\"", pid)), string(output)
}
