package harness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
)

type probeResult struct {
	output []byte
	err    error
}

func TestRunBoundedProbeRejectsNilContext(t *testing.T) {
	output, err := RunBoundedProbe(nil, time.Second, func(context.Context) ([]byte, error) {
		t.Fatal("delegate ran for a nil context")
		return []byte("unreachable"), nil
	})
	if err == nil || err.Error() != "harness probe context must not be nil" {
		t.Fatalf("RunBoundedProbe() error = %v, want the shared nil-context guard", err)
	}
	if output != nil {
		t.Fatalf("RunBoundedProbe() output = %q, want nil", output)
	}
}

func TestRunBoundedProbeRejectsFinishedContextBeforeTheDelegate(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, expire := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer expire()
	for _, test := range []struct {
		name string
		ctx  context.Context
		want error
	}{
		{name: "cancelled", ctx: cancelled, want: context.Canceled},
		{name: "deadline", ctx: expired, want: context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			output, err := RunBoundedProbe(test.ctx, time.Second, func(context.Context) ([]byte, error) {
				t.Fatal("delegate ran for an already finished caller")
				return []byte("unreachable"), nil
			})
			if err != test.want {
				t.Fatalf("RunBoundedProbe() error = %v, want %v", err, test.want)
			}
			if output != nil {
				t.Fatalf("RunBoundedProbe() output = %q, want nil", output)
			}
		})
	}
}

func TestRunBoundedProbeBoundsHangingDelegate(t *testing.T) {
	done := make(chan probeResult, 1)
	go func() {
		output, err := RunBoundedProbe(context.Background(), 20*time.Millisecond, func(probeContext context.Context) ([]byte, error) {
			<-probeContext.Done()
			return []byte("late capability output"), nil
		})
		done <- probeResult{output: output, err: err}
	}()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.DeadlineExceeded) {
			t.Fatalf("RunBoundedProbe() error = %v, want context.DeadlineExceeded", got.err)
		}
		if got.output != nil {
			t.Fatalf("RunBoundedProbe() output = %q, want nil after the bound", got.output)
		}
	case <-time.After(testHangGuard):
		t.Fatal("RunBoundedProbe() did not return after its own 20ms bound")
	}
}

func TestRunBoundedProbeRejectsSuccessAfterCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	done := make(chan probeResult, 1)
	go func() {
		output, err := RunBoundedProbe(ctx, time.Minute, func(probeContext context.Context) ([]byte, error) {
			close(started)
			<-probeContext.Done()
			return []byte("valid capability output"), nil
		})
		done <- probeResult{output: output, err: err}
	}()
	select {
	case <-started:
	case <-time.After(testHangGuard):
		t.Fatal("RunBoundedProbe() did not start the delegate")
	}
	cancel()
	select {
	case got := <-done:
		if !errors.Is(got.err, context.Canceled) {
			t.Fatalf("RunBoundedProbe() error = %v, want context.Canceled", got.err)
		}
		if got.output != nil {
			t.Fatalf("RunBoundedProbe() output = %q, want nil after cancellation", got.output)
		}
	case <-time.After(testHangGuard):
		t.Fatal("RunBoundedProbe() did not return after cancellation")
	}
}

// Scheduling slack between the requested bound and the deadline the delegate observes,
// so the window assertion pins the forwarded bound value and not only its direction.
const probeBoundSlack = 2 * time.Second

func TestRunBoundedProbePassesDelegateResultThroughUntouched(t *testing.T) {
	ctx := context.Background()
	limitErr := errors.New("caller-owned output limit exceeded")
	calls := 0
	output, err := RunBoundedProbe(ctx, 30*time.Second, func(probeContext context.Context) ([]byte, error) {
		calls++
		if probeContext == ctx {
			t.Error("delegate received the caller context instead of a derived bound")
		}
		deadline, ok := probeContext.Deadline()
		if !ok {
			t.Error("delegate context carries no deadline")
		} else if deadline.After(time.Now().Add(30 * time.Second)) {
			t.Errorf("delegate deadline = %v, want at most the caller's 30s bound", deadline)
		} else if deadline.Before(time.Now().Add(30*time.Second - probeBoundSlack)) {
			t.Errorf("delegate deadline = %v, want the caller's full 30s bound, not a narrowed one", deadline)
		}
		// The delegate owns the output bound: a limit verdict must reach the caller
		// unwrapped, and the shared probe must not impose one of its own.
		return nil, limitErr
	})
	if err != limitErr {
		t.Fatalf("RunBoundedProbe() error = %v, want the delegate's error identity", err)
	}
	if output != nil {
		t.Fatalf("RunBoundedProbe() output = %q, want nil", output)
	}
	if calls != 1 {
		t.Fatalf("delegate calls = %d, want 1", calls)
	}

	output, err = RunBoundedProbe(ctx, 30*time.Second, func(context.Context) ([]byte, error) {
		return []byte("probe output"), nil
	})
	if err != nil || string(output) != "probe output" {
		t.Fatalf("RunBoundedProbe() = (%q, %v), want the delegate's successful value", output, err)
	}
}

func TestRunProbeCommandBuildsEnvironmentAndUsesInvoker(t *testing.T) {
	callerCtx := context.Background()
	calls := 0
	var gotInvocation execution.Invocation
	var gotLimit int
	output, err := RunProbeCommand(callerCtx, execution.Invocation{
		Program: "codex",
		Args:    []string{"--version"},
	}, 4096, func(ctx context.Context, invocation execution.Invocation, limit int) ([]byte, error) {
		calls++
		if ctx != callerCtx {
			t.Error("invoker received a derived context; the bounded probe owns the bound")
		}
		gotInvocation, gotLimit = invocation, limit
		return []byte("probe output"), nil
	})
	if err != nil || string(output) != "probe output" {
		t.Fatalf("RunProbeCommand() = (%q, %v), want the invoker's result", output, err)
	}
	if calls != 1 {
		t.Fatalf("invoker calls = %d, want 1", calls)
	}
	if gotInvocation.Program != "codex" || len(gotInvocation.Args) != 1 || gotInvocation.Args[0] != "--version" {
		t.Fatalf("invocation = %+v, want the caller's program and arguments", gotInvocation)
	}
	if len(gotInvocation.Env) == 0 {
		t.Fatal("invocation reached the invoker without a built environment")
	}
	if gotLimit != 4096 {
		t.Fatalf("invoker limit = %d, want the caller's 4096", gotLimit)
	}

	wantErr := errors.New("bounded command failed")
	output, err = RunProbeCommand(callerCtx, execution.Invocation{Program: "codex"}, 4096, func(context.Context, execution.Invocation, int) ([]byte, error) {
		return []byte("partial output"), wantErr
	})
	if err != wantErr {
		t.Fatalf("RunProbeCommand() error = %v, want the invoker's error identity", err)
	}
	if string(output) != "partial output" {
		t.Fatalf("RunProbeCommand() output = %q, want the invoker's partial output", output)
	}
}

func TestCodexSchemaDigestUsesInvokerAndHashesTheGeneratedBundle(t *testing.T) {
	const bundle = "{\"app_server_protocol\":\"v2\"}\n"
	digestBytes := sha256.Sum256([]byte(bundle))
	wantDigest := "sha256:" + hex.EncodeToString(digestBytes[:])
	var gotInvocation execution.Invocation
	var gotLimit int
	var directory string
	digest, err := CodexSchemaDigest(context.Background(), "codex", 4096, func(ctx context.Context, invocation execution.Invocation, limit int) ([]byte, error) {
		if err := ctx.Err(); err != nil {
			t.Errorf("invoker context = %v, want an active context", err)
		}
		gotInvocation, gotLimit = invocation, limit
		directory = invocation.Args[len(invocation.Args)-1]
		return nil, os.WriteFile(filepath.Join(directory, "codex_app_server_protocol.v2.schemas.json"), []byte(bundle), 0o600)
	})
	if err != nil {
		t.Fatalf("CodexSchemaDigest() error = %v", err)
	}
	if digest != wantDigest {
		t.Fatalf("CodexSchemaDigest() = %q, want %q", digest, wantDigest)
	}
	if gotInvocation.Program != "codex" {
		t.Fatalf("schema command program = %q, want codex", gotInvocation.Program)
	}
	wantArgs := []string{"app-server", "generate-json-schema", "--out"}
	if len(gotInvocation.Args) != len(wantArgs)+1 {
		t.Fatalf("schema command args = %#v, want %#v plus one output directory", gotInvocation.Args, wantArgs)
	}
	for index, want := range wantArgs {
		if gotInvocation.Args[index] != want {
			t.Fatalf("schema command args = %#v, want prefix %#v", gotInvocation.Args, wantArgs)
		}
	}
	if directory == "" {
		t.Fatal("schema output directory is empty")
	}
	if gotLimit != 4096 {
		t.Fatalf("schema command limit = %d, want the caller's 4096", gotLimit)
	}
	if _, statErr := os.Stat(directory); !os.IsNotExist(statErr) {
		t.Fatalf("schema temp directory %q survived the digest: %v", directory, statErr)
	}
}

func TestCodexSchemaDigestFailsClosed(t *testing.T) {
	if _, err := CodexSchemaDigest(nil, "codex", 4096, nil); err == nil || err.Error() != "codex schema probe context must not be nil" {
		t.Fatalf("CodexSchemaDigest(nil) error = %v, want the schema nil-context guard", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CodexSchemaDigest(cancelled, "codex", 4096, nil); err != context.Canceled {
		t.Fatalf("CodexSchemaDigest(finished) error = %v, want context.Canceled", err)
	}

	wantErr := errors.New("bounded schema command failed")
	_, err := CodexSchemaDigest(context.Background(), "codex", 4096, func(context.Context, execution.Invocation, int) ([]byte, error) {
		return []byte("diagnostic output"), wantErr
	})
	if !errors.Is(err, wantErr) || !strings.Contains(err.Error(), "generate Codex app-server schema: "+wantErr.Error()) {
		t.Fatalf("CodexSchemaDigest() command error = %v, want the wrapped bounded command failure", err)
	}

	_, err = CodexSchemaDigest(context.Background(), "codex", 4096, func(context.Context, execution.Invocation, int) ([]byte, error) {
		return nil, nil
	})
	if err == nil || !strings.Contains(err.Error(), "read generated Codex v2 schema:") {
		t.Fatalf("CodexSchemaDigest() missing-bundle error = %v, want the unreadable-bundle failure", err)
	}
}
