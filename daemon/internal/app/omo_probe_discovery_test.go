package app

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/harness/omo"
)

type omOReviewDiscoveryRunner func(context.Context, string) ([]byte, error)

func (runner omOReviewDiscoveryRunner) Run(ctx context.Context, _ string, args ...string) ([]byte, error) {
	return runner(ctx, args[0])
}

func TestOmOReviewRegressionDiscoveryContinuesWithUnavailableProjection(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("OmO executable pins are defined only for Windows and Linux")
	}
	for _, test := range []struct {
		name, argument string
		cause          error
	}{
		{"missing executable", "--version", exec.ErrNotFound},
		{"help nonzero", "--help", errors.New("exit status 2")},
		{"version local deadline", "--version", fmt.Errorf("probe-local timeout: %w", context.DeadlineExceeded)},
		{"help local deadline", "--help", fmt.Errorf("probe-local timeout: %w", context.DeadlineExceeded)},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := testConfig(t)
			value.Runtime.HarnessKind = config.RuntimeHarnessOmO
			value.Runtime.HarnessVersion = omo.TestedVersion
			value.Runtime.AdapterVersion = omo.ImplementationVersion
			value.Runtime.AdapterProtocolVersion = 1
			calls := 0
			runner := omOReviewDiscoveryRunner(func(ctx context.Context, argument string) ([]byte, error) {
				calls++
				if _, ok := ctx.Deadline(); !ok || ctx.Err() != nil {
					t.Fatal("probe runner did not receive a live bounded context")
				}
				if argument == test.argument {
					return nil, test.cause
				}
				if argument != "--version" {
					t.Fatalf("failed probe continued into %s", argument)
				}
				return []byte(omo.TestedVersionLine), nil
			})
			registry := newHarnessRegistry(config.AgentProfile{Command: "unused-native-executable"})
			if err := registry.Register(harness.KindOmO, omo.NewAdapterWithRunner("recorded-omo", runner)); err != nil {
				t.Fatal(err)
			}
			app := &daemon{config: value, harnessRegistry: registry}
			if err := app.ensureHarnessProbe(context.Background()); err != nil {
				t.Fatalf("expected unavailable OmO projection stopped discovery: %v", err)
			}
			wantCalls := 1
			wantVersion := unavailableNativeMetadataVersion
			if test.argument == "--help" {
				wantCalls = 2
				wantVersion = omo.TestedVersion
			}
			capabilities := app.harnessCapabilities
			if calls != wantCalls || capabilities.Kind != harness.KindOmO || capabilities.VersionKnown != (test.argument == "--help") || capabilities.TransportVerified || capabilities.Verified || capabilities.Start || capabilities.Events || capabilities.Cancel || capabilities.Resume || capabilities.Handoff {
				t.Fatalf("unavailable discovery projection = %+v, calls=%d", capabilities, calls)
			}
			registration, _, err := buildRuntimeRegistration(app.config.Runtime, app.config.AgentProfiles[app.config.Runtime.AgentProfile], capabilities)
			if err != nil {
				t.Fatalf("unavailable discovery could not build registration: %v", err)
			}
			if err := registration.Validate(); err != nil {
				t.Fatalf("unavailable discovery registration rejected by outbound validator: %v", err)
			}
			if registration.HarnessKind != "omo" || registration.HarnessVersion != wantVersion || registration.AdapterVersion != omo.ImplementationVersion || registration.Capabilities.Adapter == nil || registration.Capabilities.Adapter.Kind != "omo" || registration.Capabilities.Adapter.NativeVersion != wantVersion || registration.Capabilities.Adapter.Operations.Start || registration.Capabilities.Adapter.Operations.Events || registration.Capabilities.Adapter.Operations.Cancel || registration.Capabilities.Adapter.Operations.Resume || registration.Capabilities.Adapter.Operations.Handoff {
				t.Fatalf("unavailable runtime registration = %+v", registration)
			}
			if err := app.ensureHarnessProbe(context.Background()); err != nil || calls != wantCalls {
				t.Fatalf("accepted discovery projection was not cached: calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestOmOReviewRegressionDiscoveryKeepsCallerCancellationFatal(t *testing.T) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		t.Skip("OmO executable pins are defined only for Windows and Linux")
	}
	for _, stage := range []string{"already cancelled", "already expired", "--version", "--help"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := context.Canceled
			if stage == "already cancelled" {
				cancel()
			} else if stage == "already expired" {
				var expire context.CancelFunc
				ctx, expire = context.WithDeadline(context.Background(), time.Unix(1, 0))
				defer expire()
				want = context.DeadlineExceeded
			}
			value := testConfig(t)
			value.Runtime.HarnessKind = config.RuntimeHarnessOmO
			value.Runtime.HarnessVersion = omo.TestedVersion
			value.Runtime.AdapterVersion = omo.ImplementationVersion
			value.Runtime.AdapterProtocolVersion = 1
			calls := 0
			runner := omOReviewDiscoveryRunner(func(_ context.Context, argument string) ([]byte, error) {
				calls++
				if argument == stage {
					cancel()
				}
				if argument == "--version" {
					return []byte(omo.TestedVersionLine), nil
				}
				return []byte("--mode <json|rpc>"), nil
			})
			registry := newHarnessRegistry(config.AgentProfile{Command: "unused-native-executable"})
			if err := registry.Register(harness.KindOmO, omo.NewAdapterWithRunner("recorded-omo", runner)); err != nil {
				t.Fatal(err)
			}
			app := &daemon{config: value, harnessRegistry: registry}
			err := app.ensureHarnessProbe(ctx)
			wantCalls := 0
			if stage == "--version" {
				wantCalls = 1
			} else if stage == "--help" {
				wantCalls = 2
			}
			if !errors.Is(err, want) || errors.Is(err, harness.ErrHarnessUnavailable) || errors.Is(err, harness.ErrNativeUnverified) || calls != wantCalls || app.harnessCapabilities.Kind != "" {
				t.Fatalf("caller failure was accepted as unavailable discovery: error=%v capabilities=%+v calls=%d", err, app.harnessCapabilities, calls)
			}
		})
	}
}
