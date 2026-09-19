package execution

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
)

func TestRunBoundedCommandCombinesOutputAndClosesStdin(t *testing.T) {
	output, err := RunBoundedCommand(context.Background(), helperInvocation("streams", "128"), 256)
	if err != nil {
		t.Fatalf("RunBoundedCommand() error = %v", err)
	}
	if len(output) != 256 || bytes.Count(output, []byte("o")) != 128 || bytes.Count(output, []byte("e")) != 128 {
		t.Fatalf("combined output = %q, want 128 stdout and 128 stderr bytes", output)
	}
}

func TestRunBoundedCommandRejectsOutputLimitWithoutReturningTruncatedOutput(t *testing.T) {
	output, err := RunBoundedCommand(context.Background(), helperInvocation("streams", "128"), 128)
	if !errors.Is(err, ErrOutputLimitExceeded) {
		t.Fatalf("RunBoundedCommand() error = %v, want ErrOutputLimitExceeded", err)
	}
	if output != nil {
		t.Fatalf("output = %q, want nil after output limit", output)
	}
}

func TestRunBoundedCommandTerminatesOnCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	marker := filepath.Join(t.TempDir(), "started")
	invocation := boundedProbeInvocation("wait", marker)

	result := make(chan error, 1)
	go func() {
		_, err := RunBoundedCommand(ctx, invocation, 1024)
		result <- err
	}()
	waitForProbeMarker(t, marker)
	cancel()

	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("RunBoundedCommand() error = %v, want context.Canceled", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("RunBoundedCommand() did not terminate after cancellation")
	}
}

func TestRunBoundedCommandTerminatesOnCallerDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	invocation := helperInvocation("wait")

	_, err := RunBoundedCommand(ctx, invocation, 1024)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RunBoundedCommand() error = %v, want context.DeadlineExceeded", err)
	}
}

func TestRunBoundedCommandStopsDescendantHoldingOutputPipe(t *testing.T) {
	output, err := RunBoundedCommand(context.Background(), helperInvocation("root-exits-child-holding-stdout"), 1024)
	if err != nil {
		t.Fatalf("RunBoundedCommand() error = %v", err)
	}
	childPIDText := strings.TrimSpace(strings.TrimPrefix(string(output), "child:"))
	childPID, err := strconv.Atoi(childPIDText)
	if err != nil {
		t.Fatalf("child PID output %q is not an integer: %v", output, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for probeProcessExists(childPID) && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if probeProcessExists(childPID) {
		t.Fatalf("descendant process %d survived bounded command", childPID)
	}
}

func TestRunBoundedCommandRejectsSuccessAfterContextDeadline(t *testing.T) {
	ctx := &postWaitDeadlineContext{deadlineAt: 4}
	invocation := helperInvocation("args", "completed")

	output, err := RunBoundedCommand(ctx, invocation, 1024)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RunBoundedCommand() error = %v, want context.DeadlineExceeded", err)
	}
	if output != nil {
		t.Fatalf("output = %q, want nil after deadline", output)
	}
}

func TestRunBoundedCommandDoesNotInstallDurableCallbacks(t *testing.T) {
	var processCallback atomic.Bool
	var authorityCallback atomic.Bool
	var receiptCallback atomic.Bool
	invocation := helperInvocation("args", "no-journal")
	invocation.PersistProcess = func(int, string) error {
		processCallback.Store(true)
		return nil
	}
	invocation.PersistProcessAuthority = func(int, string, *authority.Supervisor) error {
		authorityCallback.Store(true)
		return nil
	}
	invocation.PersistContainmentStopReceipt = func(int, string, authority.StopReceipt) error {
		receiptCallback.Store(true)
		return nil
	}

	output, err := RunBoundedCommand(context.Background(), invocation, 1024)
	if err != nil {
		t.Fatalf("RunBoundedCommand() error = %v", err)
	}
	if string(output) != "no-journal" {
		t.Fatalf("output = %q, want no-journal", output)
	}
	if processCallback.Load() || authorityCallback.Load() || receiptCallback.Load() {
		t.Fatalf("RunBoundedCommand() invoked durable callback: process=%t authority=%t receipt=%t", processCallback.Load(), authorityCallback.Load(), receiptCallback.Load())
	}
}

type postWaitDeadlineContext struct {
	calls      atomic.Int32
	deadlineAt int32
}

func (*postWaitDeadlineContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (*postWaitDeadlineContext) Done() <-chan struct{} { return nil }

func (ctx *postWaitDeadlineContext) Err() error {
	if ctx.calls.Add(1) >= ctx.deadlineAt {
		return context.DeadlineExceeded
	}
	return nil
}

func (*postWaitDeadlineContext) Value(any) any { return nil }

func TestBoundedProbeHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_BOUNDED_PROBE_HELPER") != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator == -1 || separator+1 >= len(os.Args) {
		os.Exit(2)
	}
	switch os.Args[separator+1] {
	case "wait":
		if len(os.Args) != separator+3 {
			os.Exit(2)
		}
		if err := os.WriteFile(os.Args[separator+2], []byte("started"), 0o600); err != nil {
			fmt.Fprint(os.Stderr, err)
			os.Exit(3)
		}
		waitForever()
	case "args":
		_, _ = io.WriteString(os.Stdout, strings.Join(os.Args[separator+2:], "\x1f"))
	default:
		os.Exit(2)
	}
}

func boundedProbeInvocation(mode string, arguments ...string) Invocation {
	return Invocation{
		Program: os.Args[0],
		Args:    append([]string{"-test.run=^TestBoundedProbeHelperProcess$", "--", mode}, arguments...),
		Env:     append(minimalEnvironment(), "GO_WANT_BOUNDED_PROBE_HELPER=1"),
	}
}

func waitForProbeMarker(t *testing.T, marker string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("bounded command did not start; marker %q was not created", marker)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
