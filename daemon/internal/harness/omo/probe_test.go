package omo

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

type recordedProbe struct{ version, help, digest string }

func (runner recordedProbe) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if args[0] == "--version" {
		return []byte(runner.version), nil
	}
	return []byte(runner.help), nil
}

func (runner recordedProbe) ExecutableDigest(context.Context, string) (string, error) {
	return runner.digest, nil
}

type reviewProbeRunner struct {
	recordedProbe
	run    func(context.Context, string) ([]byte, error)
	digest func(context.Context) (string, error)
}

func (runner reviewProbeRunner) Run(ctx context.Context, _ string, args ...string) ([]byte, error) {
	return runner.run(ctx, args[0])
}

func (runner reviewProbeRunner) ExecutableDigest(ctx context.Context, executable string) (string, error) {
	if runner.digest != nil {
		return runner.digest(ctx)
	}
	return runner.recordedProbe.ExecutableDigest(ctx, executable)
}

func TestOmOReviewRegressionProbeFailuresDoNotWrapCommandCauses(t *testing.T) {
	for _, test := range []struct {
		name, argument string
		cause, want    error
	}{
		{"missing executable", "--version", exec.ErrNotFound, harness.ErrHarnessUnavailable},
		{"help nonzero", "--help", errors.New("exit status 2"), harness.ErrNativeUnverified},
		{"version local deadline", "--version", fmt.Errorf("probe-local timeout: %w", context.DeadlineExceeded), harness.ErrHarnessUnavailable},
		{"help local deadline", "--help", fmt.Errorf("probe-local timeout: %w", context.DeadlineExceeded), harness.ErrNativeUnverified},
		{"help local cancellation", "--help", context.Canceled, harness.ErrNativeUnverified},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			var calls []string
			recorded := recordedProbe{TestedVersionLine, "--mode <json|rpc>", TestedWindowsExecutableHash}
			runner := reviewProbeRunner{recordedProbe: recorded, run: func(bounded context.Context, argument string) ([]byte, error) {
				calls = append(calls, argument)
				if _, ok := bounded.Deadline(); !ok || bounded.Err() != nil {
					t.Fatal("probe runner did not receive a live bounded context")
				}
				if argument == test.argument {
					return []byte("not accepted despite the command failure"), test.cause
				}
				return recorded.Run(bounded, "recorded-omo", argument)
			}}
			result, err := probeForPlatform(ctx, "recorded-omo", "windows", runner)
			if !errors.Is(err, test.want) || errors.Is(err, test.cause) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				t.Fatalf("probe failure escaped availability classification: result=%+v error=%v", result, err)
			}
			if !strings.Contains(err.Error(), test.cause.Error()) {
				t.Fatalf("command diagnostic lost: %v", err)
			}
			wantCalls := 1
			if test.argument == "--help" {
				wantCalls = 2
			}
			if len(calls) != wantCalls || result.VersionKnown != (test.argument == "--help") || result.TransportKnown || result.ExecutableHashKnown || result.NativeSessionVerified || result.Capabilities.Verified || result.Capabilities.Start || result.Capabilities.Events || result.Capabilities.Cancel || result.Capabilities.Resume || result.Capabilities.Handoff {
				t.Fatalf("failed probe continued or promoted capabilities: calls=%v result=%+v", calls, result)
			}
		})
	}
}

func TestOmOReviewRegressionRegistryRetainsUnavailableProbeClassification(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("OmO executable pins are defined only for Windows and Linux")
	}
	for _, test := range []struct {
		name, argument string
		cause, want    error
	}{
		{"missing executable", "--version", exec.ErrNotFound, harness.ErrHarnessUnavailable},
		{"help local deadline", "--help", context.DeadlineExceeded, harness.ErrNativeUnverified},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorded := recordedProbe{version: TestedVersionLine, help: "--mode <json|rpc>"}
			runner := reviewProbeRunner{recordedProbe: recorded, run: func(ctx context.Context, argument string) ([]byte, error) {
				if argument == test.argument {
					return nil, test.cause
				}
				return recorded.Run(ctx, "recorded-omo", argument)
			}}
			registry := harness.NewRegistry()
			if err := registry.Register(harness.KindOmO, NewAdapterWithRunner("recorded-omo", runner)); err != nil {
				t.Fatal(err)
			}
			capabilities, err := registry.Probe(context.Background(), harness.KindOmO)
			if errors.Unwrap(err) != test.want || errors.Is(err, test.cause) || capabilities.Kind != harness.KindOmO || capabilities.Verified || capabilities.Start {
				t.Fatalf("registry exposed a fatal probe leaf or unsafe capabilities: %+v, %v", capabilities, err)
			}
			if err := capabilities.Validate(); err != nil {
				t.Fatalf("unavailable projection is not registrable: %v", err)
			}
			next, err := registry.Probe(context.Background(), harness.KindGeneric)
			if err != nil || next.Kind != harness.KindGeneric {
				t.Fatalf("subsequent registered runtime probe failed: %+v, %v", next, err)
			}
		})
	}
}

func TestOmOReviewRegressionProbePropagatesCallerCancellation(t *testing.T) {
	for _, stage := range []string{"--version", "--help", "digest"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			recorded := recordedProbe{TestedVersionLine, "--mode <json|rpc>", TestedWindowsExecutableHash}
			runner := reviewProbeRunner{recordedProbe: recorded, run: func(bounded context.Context, argument string) ([]byte, error) {
				if stage == argument {
					cancel()
				}
				return recorded.Run(bounded, "recorded-omo", argument)
			}, digest: func(context.Context) (string, error) {
				if stage != "digest" {
					t.Fatal("cancelled caller reached executable hashing")
				}
				cancel()
				return TestedWindowsExecutableHash, nil
			}}
			result, err := probeForPlatform(ctx, "recorded-omo", "windows", runner)
			if err != context.Canceled || errors.Is(err, harness.ErrHarnessUnavailable) || errors.Is(err, harness.ErrNativeUnverified) || result.Capabilities.Verified || result.Capabilities.Start {
				t.Fatalf("caller cancellation became a capability verdict: result=%+v error=%v", result, err)
			}
		})
	}
}

func TestOmOReviewRegressionProbeRejectsAlreadyFinishedCaller(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, expire := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer expire()
	for _, test := range []struct {
		name string
		ctx  context.Context
		want error
	}{
		{"cancelled", cancelled, context.Canceled},
		{"deadline", expired, context.DeadlineExceeded},
	} {
		t.Run(test.name, func(t *testing.T) {
			runner := reviewProbeRunner{run: func(context.Context, string) ([]byte, error) {
				t.Fatal("finished caller reached command runner")
				return nil, nil
			}}
			result, err := probeForPlatform(test.ctx, "recorded-omo", "windows", runner)
			if err != test.want || result.Capabilities.Verified || result.Capabilities.Start {
				t.Fatalf("finished caller probe = %+v, %v", result, err)
			}
		})
	}
}

func TestProbePinsCompleteVersionTransportAndPlatformBytes(t *testing.T) {
	for _, platform := range []string{"windows", "linux"} {
		t.Run(platform, func(t *testing.T) {
			digest := TestedWindowsExecutableHash
			if platform == "linux" {
				digest = TestedLinuxExecutableHash
			}
			runner := recordedProbe{TestedVersionLine + "\r\n", "Options: --print, --mode <json|rpc>, --session <file>", digest}
			result, err := probeForPlatform(context.Background(), "recorded-omo", platform, runner)
			if err != nil || !result.VersionKnown || !result.TransportKnown || !result.ExecutableHashKnown || !result.NativeSessionVerified {
				t.Fatalf("probe identity = %+v, %v", result, err)
			}
			if !result.Capabilities.Verified || !result.Capabilities.Start || !result.Capabilities.Events || !result.Capabilities.Cancel || !result.Capabilities.Resume || !result.Capabilities.Handoff {
				t.Fatalf("witnessed lifecycle not promoted: %+v", result.Capabilities)
			}
			if result.Capabilities.ProviderAccess || result.Capabilities.ApprovalResponse || result.Capabilities.HardCostLimit || result.Capabilities.Guidance != harness.GuidanceUnsupported || result.Capabilities.Pause != harness.PauseUnsupported || result.Capabilities.Usage != harness.UsageUnknown {
				t.Fatalf("unwitnessed operation promoted: %+v", result.Capabilities)
			}
			if err := result.Capabilities.Validate(); err != nil {
				t.Fatal(err)
			}
			for _, mutation := range []struct{ name, version, help, digest string }{
				{"banner", "banner\n" + TestedVersionLine, runner.help, digest},
				{"other engine", "omo 5.1.69 (engine: senpi 2026.11.09-1; scheme nodef)", runner.help, digest},
				{"other version", "omo 5.1.70 (engine: senpi 2026.11.10-2; scheme nodef)", runner.help, digest},
				{"help has no RPC", runner.version, "--mode <json>", digest},
				{"same version shim", runner.version, runner.help, "sha256:0000000000000000000000000000000000000000000000000000000000000000"},
			} {
				t.Run(mutation.name, func(t *testing.T) {
					got, err := probeForPlatform(context.Background(), "recorded-omo", platform, recordedProbe{mutation.version, mutation.help, mutation.digest})
					if err == nil || got.Capabilities.Verified || got.Capabilities.Start {
						t.Fatalf("unsafe executable accepted: %+v, %v", got, err)
					}
				})
			}
		})
	}
	if _, err := probeForPlatform(context.Background(), "recorded-omo", "darwin", recordedProbe{}); !errors.Is(err, harness.ErrUnsupportedVersion) {
		t.Fatalf("unknown platform = %v", err)
	}
}

func TestProductionStartDoesNotLaunchUnpinnedExecutable(t *testing.T) {
	digest := TestedWindowsExecutableHash
	if runtime.GOOS == "linux" {
		digest = TestedLinuxExecutableHash
	}
	adapter := NewAdapterWithRunner("recorded-omo", recordedProbe{TestedVersionLine, "--mode <json|rpc>", digest + "changed"})
	request := candidateRequest(t, newTestProcess())
	request.StartProcess = func(context.Context, execution.Invocation, execution.Sink) (harness.NativeProcess, error) {
		t.Fatal("unpinned executable reached the process launcher")
		return nil, errors.New("unexpected launch")
	}
	started, err := adapter.Start(context.Background(), request, harness.EventSinkFunc(func(context.Context, harness.Event) error { t.Fatal("unverified adapter exposed an event"); return nil }))
	if started != nil || err == nil {
		t.Fatalf("Start = %v, %v", started, err)
	}
}
