package probeconformance

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/harness/codex"
	"github.com/wxxb789/symmetry/daemon/internal/harness/opencode"
	"github.com/wxxb789/symmetry/daemon/internal/harness/pi"
)

// commandRunner is the injection seam every provider probe exposes: each
// provider package accepts a runner with exactly this method set.
type commandRunner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}

// probeCall runs one provider's probe against an injected runner. The
// provider-specific result type is deliberately dropped: the contracts below
// assert the probe error and the commands the runner actually received.
type probeCall func(context.Context, commandRunner) error

// probeCase is one provider row of a cross-provider probe contract.
type probeCase struct {
	// name is the provider under test.
	name string
	// probe is the provider's exported probe entry point.
	probe probeCall
	// probeTimeout is that provider's own bounded-probe timeout, so a hung
	// runner can only pass a row by returning inside the provider's bound. It
	// mirrors the unexported per-package constants.
	probeTimeout time.Duration
	// versionOutput is what the provider's probe reads from `--version`.
	versionOutput []byte
	// otherOutput is what the provider returns for every other command. No
	// contract here reaches it, but the stub stays honest about what the probe
	// would otherwise have seen.
	otherOutput []byte
}

var probeCases = []probeCase{
	{
		name: "codex",
		probe: func(ctx context.Context, runner commandRunner) error {
			_, err := codex.Probe(ctx, "codex", runner)
			return err
		},
		probeTimeout:  time.Second,
		versionOutput: []byte("codex-cli " + codex.TestedVersion + "\n"),
		otherOutput:   []byte("app-server stdio\n"),
	},
	{
		name: "opencode",
		probe: func(ctx context.Context, runner commandRunner) error {
			_, err := opencode.NewAdapterWithRunner("opencode", runner).Probe(ctx)
			return err
		},
		probeTimeout:  time.Second,
		versionOutput: []byte(opencode.TestedVersion + "\n"),
		otherOutput:   []byte("opencode serve --hostname --port --pure"),
	},
	{
		name: "pi",
		probe: func(ctx context.Context, runner commandRunner) error {
			_, err := pi.Probe(ctx, "pi", runner)
			return err
		},
		probeTimeout:  5 * time.Second,
		versionOutput: []byte(pi.TestedVersion + "\n"),
		otherOutput:   []byte("--mode <mode> rpc"),
	},
}

// TestProbeBoundsHangingRunnerAndPreservesCancellation asserts that an
// unresponsive command runner can never outlive the probe bound: the probe
// returns the caller-visible error once its own timeout, the caller's earlier
// deadline, or the caller's cancellation fires.
func TestProbeBoundsHangingRunnerAndPreservesCancellation(t *testing.T) {
	for _, provider := range probeCases {
		t.Run(provider.name, func(t *testing.T) {
			for _, test := range []struct {
				name             string
				context          func() (context.Context, context.CancelFunc)
				want             error
				cancelAfterStart bool
				waitWithin       time.Duration
			}{
				{name: "local timeout", context: func() (context.Context, context.CancelFunc) { return context.Background(), func() {} }, want: context.DeadlineExceeded},
				{name: "caller earlier deadline", context: func() (context.Context, context.CancelFunc) {
					return newExpiredDeadlineContext()
				}, want: context.DeadlineExceeded, cancelAfterStart: true, waitWithin: time.Second},
				{name: "caller cancellation", context: func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }, want: context.Canceled, cancelAfterStart: true, waitWithin: time.Second},
			} {
				t.Run(test.name, func(t *testing.T) {
					// The local-timeout row must return inside the bound the
					// provider itself chose.
					waitWithin := test.waitWithin
					if waitWithin == 0 {
						waitWithin = provider.probeTimeout + time.Second
					}
					ctx, cancel := test.context()
					defer cancel()
					runner := hangingRunner{started: make(chan struct{})}
					done := make(chan error, 1)
					go func() {
						done <- provider.probe(ctx, runner)
					}()
					select {
					case <-runner.started:
					case <-time.After(time.Second):
						t.Fatal("the probe did not start the injected runner")
					}
					if test.cancelAfterStart {
						cancel()
					}
					var err error
					select {
					case err = <-done:
					case <-time.After(waitWithin):
						t.Fatalf("the probe did not return within %v", waitWithin)
					}
					if !errors.Is(err, test.want) {
						t.Fatalf("probe error = %v, want %v", err, test.want)
					}
				})
			}
		})
	}
}

// TestProbeRejectsSuccessfulRunnerResultAfterCancellation asserts that a runner
// which answers `--version` successfully only after the caller cancelled cannot
// settle the probe: the probe reports cancellation and never issues a second
// command.
func TestProbeRejectsSuccessfulRunnerResultAfterCancellation(t *testing.T) {
	for _, provider := range probeCases {
		t.Run(provider.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runner := &cancellationSuccessRunner{
				started:       make(chan struct{}),
				versionOutput: provider.versionOutput,
				otherOutput:   provider.otherOutput,
			}
			done := make(chan error, 1)
			go func() {
				done <- provider.probe(ctx, runner)
			}()
			select {
			case <-runner.started:
			case <-time.After(time.Second):
				t.Fatal("the probe did not start the injected runner")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("probe error = %v, want context.Canceled", err)
				}
			case <-time.After(time.Second):
				t.Fatal("the probe did not return after cancellation")
			}
			calls := runner.callsSnapshot()
			if len(calls) != 1 || calls[0] != "--version" {
				t.Fatalf("injected runner calls = %v, want only --version", calls)
			}
		})
	}
}

// hangingRunner never answers a probe command, so only the probe's own bound can
// end the call.
type hangingRunner struct {
	started chan struct{}
}

func (runner hangingRunner) Run(ctx context.Context, _ string, _ ...string) ([]byte, error) {
	if runner.started != nil {
		close(runner.started)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

// cancellationSuccessRunner answers `--version` with the provider's transcript
// only after the probe context ends, which is a result a cancelled caller must
// never see.
type cancellationSuccessRunner struct {
	started       chan struct{}
	startedOnce   sync.Once
	mutex         sync.Mutex
	calls         []string
	versionOutput []byte
	otherOutput   []byte
}

func (runner *cancellationSuccessRunner) Run(ctx context.Context, _ string, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	runner.mutex.Lock()
	runner.calls = append(runner.calls, key)
	runner.mutex.Unlock()
	if key == "--version" {
		runner.startedOnce.Do(func() { close(runner.started) })
		<-ctx.Done()
		return runner.versionOutput, nil
	}
	return runner.otherOutput, nil
}

func (runner *cancellationSuccessRunner) callsSnapshot() []string {
	runner.mutex.Lock()
	defer runner.mutex.Unlock()
	return append([]string(nil), runner.calls...)
}

// expiredDeadlineContext reports a deadline that already passed, so a probe that
// derives its bound from the caller must not wait for the caller to cancel.
type expiredDeadlineContext struct {
	done chan struct{}
	once sync.Once
	mu   sync.Mutex
	err  error
}

func newExpiredDeadlineContext() (context.Context, context.CancelFunc) {
	ctx := &expiredDeadlineContext{done: make(chan struct{})}
	return ctx, ctx.expire
}

func (ctx *expiredDeadlineContext) Deadline() (time.Time, bool) {
	return time.Unix(0, 0), true
}

func (ctx *expiredDeadlineContext) Done() <-chan struct{} {
	return ctx.done
}

func (ctx *expiredDeadlineContext) Err() error {
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	return ctx.err
}

func (*expiredDeadlineContext) Value(any) any {
	return nil
}

func (ctx *expiredDeadlineContext) expire() {
	ctx.once.Do(func() {
		ctx.mu.Lock()
		ctx.err = context.DeadlineExceeded
		ctx.mu.Unlock()
		close(ctx.done)
	})
}
