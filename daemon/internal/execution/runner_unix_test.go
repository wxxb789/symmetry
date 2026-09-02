//go:build !windows

package execution

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestTerminatedZeroExitIsNotSuccessfulOnUnix(t *testing.T) {
	invocation := helperInvocation("unused")
	invocation.Args = []string{"-test.run=^TestUnixTerminationHelper$"}
	invocation.Env = append(minimalEnvironment(), "GO_WANT_UNIX_TERMINATION_HELPER=1")
	process, err := NewRunner().Start(context.Background(), invocation, &recordingSink{})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := process.Terminate(ctx, time.Second); err != nil {
		t.Fatalf("Terminate() error = %v", err)
	}
	result := waitForResult(t, process)
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0; wait error = %v", result.ExitCode, result.WaitError)
	}
	if !result.Terminated {
		t.Fatal("Terminated = false, want true")
	}
	if result.Success() {
		t.Fatal("terminated zero-exit process reported success")
	}
}

func TestRootExitDoesNotLeaveADescendantHoldingStdoutOnUnix(t *testing.T) {
	sink := &recordingSink{notify: make(chan struct{}, 1)}
	process := startHelper(t, sink, "root-exits-child-holding-stdout")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = process.Terminate(ctx, 0)
	})

	childPIDText := waitForUnixStdout(t, sink)
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
	if unixProcessExists(childPID) {
		t.Fatalf("descendant process %d survived root exit", childPID)
	}
}

func TestUnixTerminationHelper(t *testing.T) {
	if os.Getenv("GO_WANT_UNIX_TERMINATION_HELPER") != "1" {
		return
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM)
	defer signal.Stop(signals)
	<-signals
	os.Exit(0)
}

func waitForUnixStdout(t *testing.T, sink *recordingSink) string {
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

func unixProcessExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}
