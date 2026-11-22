package apptest

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
)

func TestProcessDefaultsAnswerTheMajorityBehaviour(t *testing.T) {
	ctx := context.Background()

	t.Run("WriteInput", func(t *testing.T) {
		process := &Process{}
		if err := process.WriteInput([]byte("input-1")); err != nil {
			t.Fatalf("WriteInput() error = %v", err)
		}
		if got := process.Inputs(); len(got) != 1 || string(got[0]) != "input-1" {
			t.Fatalf("Inputs() = %#v, want one recorded input %q", got, "input-1")
		}
	})

	t.Run("Terminate", func(t *testing.T) {
		process := &Process{}
		if err := process.Terminate(ctx, time.Second); err != nil {
			t.Fatalf("Terminate() error = %v", err)
		}
		if got := process.Terminations(); got != 1 {
			t.Fatalf("Terminations() = %d, want 1", got)
		}
		if !process.Terminated() {
			t.Fatal("Terminated() = false after Terminate()")
		}
	})

	t.Run("Wait", func(t *testing.T) {
		result := execution.Result{ExitCode: 3, StartedAt: time.Unix(7, 0), FinishedAt: time.Unix(9, 0)}
		process := &Process{Result: result}
		if got := process.Wait(); !reflect.DeepEqual(got, result) {
			t.Fatalf("Wait() = %#v, want Result %#v", got, result)
		}
	})

	t.Run("ProcessDetails", func(t *testing.T) {
		process := &Process{}
		if pid, identity := process.ProcessDetails(); pid != DefaultProcessPID || identity != "test:42" {
			t.Fatalf("ProcessDetails() = (%d, %q), want (%d, %q)", pid, identity, DefaultProcessPID, "test:42")
		}
		if err := process.Terminate(ctx, time.Second); err != nil {
			t.Fatalf("Terminate() error = %v", err)
		}
		if pid, identity := process.ProcessDetails(); pid != 0 || identity != "" {
			t.Fatalf("ProcessDetails() after Terminate() = (%d, %q), want (0, %q)", pid, identity, "")
		}
	})

	t.Run("KeepPIDAfterTerminate", func(t *testing.T) {
		process := &Process{KeepPIDAfterTerminate: true}
		if err := process.Terminate(ctx, time.Second); err != nil {
			t.Fatalf("Terminate() error = %v", err)
		}
		if pid, identity := process.ProcessDetails(); pid != DefaultProcessPID || identity != "test:42" {
			t.Fatalf("ProcessDetails() = (%d, %q), want (%d, %q)", pid, identity, DefaultProcessPID, "test:42")
		}
	})
}

func TestProcessHooksReplaceMethods(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("sentinel")

	t.Run("WriteInput", func(t *testing.T) {
		process := &Process{}
		var received []byte
		process.OnWriteInput = func(input []byte) error {
			received = input
			return sentinel
		}
		if err := process.WriteInput([]byte("hook-input")); !errors.Is(err, sentinel) {
			t.Fatalf("WriteInput() error = %v, want the hook sentinel", err)
		}
		if string(received) != "hook-input" {
			t.Fatalf("OnWriteInput received %q, want %q", received, "hook-input")
		}
	})

	t.Run("Terminate", func(t *testing.T) {
		process := &Process{}
		hookContext, cancel := context.WithCancel(ctx)
		t.Cleanup(cancel)
		var receivedContext context.Context
		var receivedGrace time.Duration
		process.OnTerminate = func(hookCtx context.Context, grace time.Duration) error {
			receivedContext, receivedGrace = hookCtx, grace
			return sentinel
		}
		if err := process.Terminate(hookContext, 3*time.Second); !errors.Is(err, sentinel) {
			t.Fatalf("Terminate() error = %v, want the hook sentinel", err)
		}
		if receivedContext != hookContext {
			t.Fatal("Terminate() did not forward the context to the hook")
		}
		if receivedGrace != 3*time.Second {
			t.Fatalf("Terminate() forwarded grace %v, want %v", receivedGrace, 3*time.Second)
		}
		if process.Terminated() {
			t.Fatal("Terminated() = true although OnTerminate replaced the call")
		}
	})

	t.Run("Wait", func(t *testing.T) {
		process := &Process{Result: execution.Result{ExitCode: 3}}
		want := execution.Result{ExitCode: 7, FinishedAt: time.Unix(7, 0)}
		process.OnWait = func() execution.Result { return want }
		if got := process.Wait(); !reflect.DeepEqual(got, want) {
			t.Fatalf("Wait() = %#v, want the hook result %#v", got, want)
		}
	})

	t.Run("ProcessDetails", func(t *testing.T) {
		process := &Process{}
		process.OnProcessDetails = func() (int, string) { return 7, "hook:7" }
		if pid, identity := process.ProcessDetails(); pid != 7 || identity != "hook:7" {
			t.Fatalf("ProcessDetails() = (%d, %q), want (7, %q)", pid, identity, "hook:7")
		}
	})
}

func TestProcessRecordingAccessorsReturnDetachedCopies(t *testing.T) {
	ctx := context.Background()

	t.Run("Inputs", func(t *testing.T) {
		process := &Process{}
		input := []byte("input-1")
		if err := process.WriteInput(input); err != nil {
			t.Fatalf("WriteInput() error = %v", err)
		}
		input[0] = 'X'
		inputs := process.Inputs()
		if len(inputs) != 1 || string(inputs[0]) != "input-1" {
			t.Fatalf("Inputs() = %#v, want one entry with the original bytes", inputs)
		}
		inputs[0][0] = 'Y'
		inputs[0] = append(inputs[0], '!')
		if got := process.Inputs(); len(got) != 1 || string(got[0]) != "input-1" {
			t.Fatalf("mutating the returned copy changed the record: %#v", got)
		}
	})

	t.Run("Terminations", func(t *testing.T) {
		process := &Process{}
		if got := process.Terminations(); got != 0 {
			t.Fatalf("Terminations() = %d before Terminate(), want 0", got)
		}
		if process.Terminated() {
			t.Fatal("Terminated() = true before Terminate()")
		}
		if err := process.Terminate(ctx, time.Second); err != nil {
			t.Fatalf("Terminate() error = %v", err)
		}
		if got := process.Terminations(); got != 1 {
			t.Fatalf("Terminations() = %d after Terminate(), want 1", got)
		}
		if !process.Terminated() {
			t.Fatal("Terminated() = false after Terminate()")
		}
	})
}
