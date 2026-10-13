//go:build symmetry_pi_control_e2e

// Command pi-control-witness is a test-only daemon child. It is built only by
// the opt-in Control/Pi E2E and never participates in the normal daemon
// command or configuration path.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/app"
	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/harness/pi"
	"github.com/wxxb789/symmetry/daemon/internal/platform"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

const (
	controlE2EEnabledEnv  = "SYMMETRY_PI_CONTROL_E2E"
	witnessImplementation = "symmetry-test:pi-control-loopback-witness-v1"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "-containment-supervisor" {
		if err := platform.RunContainmentSupervisor(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "run containment supervisor: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if os.Getenv(controlE2EEnabledEnv) != "1" {
		fatalf("%s=1 is required for the test-only daemon child", controlE2EEnabledEnv)
	}

	configPath := flag.String("config", "", "path to the daemon JSON configuration")
	flag.Parse()
	if strings.TrimSpace(*configPath) == "" {
		fatalf("-config is required")
	}

	value, err := config.Load(*configPath)
	if err != nil {
		fatalf("load config: %v", err)
	}
	if value.Runtime.HarnessKind != config.RuntimeHarnessPi {
		fatalf("test-only Pi witness requires runtime.harness_kind=%q", config.RuntimeHarnessPi)
	}
	profile, ok := value.AgentProfiles[value.Runtime.AgentProfile]
	if !ok || strings.TrimSpace(profile.Command) == "" {
		fatalf("test-only Pi witness requires the configured agent profile")
	}

	registry := harness.NewRegistry()
	if err := registry.Register(harness.KindPi, newPiControlE2EWitness(profile.Command)); err != nil {
		fatalf("register test-only Pi witness: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	changes := []app.Options{
		app.WithPiControlE2EWitnessRegistry(registry),
		app.WithLogWriter(os.Stdout),
	}
	if marker := strings.TrimSpace(os.Getenv(processMarkerEnv)); marker != "" {
		changes = append(changes, app.WithPiControlE2EProcessObserver(processMarkerWriter(marker)))
	}
	if err := app.Run(ctx, value, changes...); err != nil && !errors.Is(err, context.Canceled) {
		fatalf("run test-only Pi witness daemon: %v", err)
	}
}

const processMarkerEnv = "SYMMETRY_PI_CONTROL_E2E_PROCESS_RECORD"

func processMarkerWriter(path string) func(state.RunKey, int, string, time.Time) {
	return func(key state.RunKey, pid int, identity string, startedAt time.Time) {
		marker := struct {
			RunID           string    `json:"run_id"`
			Generation      int64     `json:"generation"`
			PID             int       `json:"pid"`
			ProcessIdentity string    `json:"process_identity"`
			StartedAt       time.Time `json:"started_at"`
		}{
			RunID: key.RunID, Generation: key.Generation, PID: pid, ProcessIdentity: identity, StartedAt: startedAt,
		}
		contents, err := json.Marshal(marker)
		if err != nil {
			return
		}
		directory := filepath.Dir(path)
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return
		}
		_ = os.WriteFile(path, contents, 0o600)
	}
}

type piControlE2EWitness struct {
	delegate     *pi.Adapter
	capabilities harness.Capabilities
}

func newPiControlE2EWitness(executable string) *piControlE2EWitness {
	capabilities := harness.UnsupportedCapabilities(
		harness.KindPi,
		"test-only admission witness; native provider, accounting, and production capability support remain unverified",
	)
	capabilities.NativeVersion = pi.TestedVersion
	capabilities.ImplementationVersion = witnessImplementation
	capabilities.ProtocolVersion = 1
	capabilities.VersionKnown = true
	capabilities.TransportVerified = true
	capabilities.Verified = true
	capabilities.Start = true
	capabilities.Events = true
	capabilities.Cancel = true
	capabilities.Resume = true
	capabilities.Handoff = true
	delete(capabilities.Unsupported, string(harness.CapabilityStart))
	delete(capabilities.Unsupported, string(harness.CapabilityEvents))
	delete(capabilities.Unsupported, string(harness.CapabilityCancel))
	delete(capabilities.Unsupported, string(harness.CapabilityResume))
	delete(capabilities.Unsupported, string(harness.CapabilityHandoff))

	return &piControlE2EWitness{
		delegate:     pi.NewAdapter(executable),
		capabilities: capabilities,
	}
}

func (witness *piControlE2EWitness) Probe(ctx context.Context) (harness.Capabilities, error) {
	if witness == nil || witness.delegate == nil {
		return harness.Capabilities{}, errors.New("test-only Pi control witness is nil")
	}
	if ctx == nil {
		return harness.Capabilities{}, errors.New("test-only Pi control witness probe context must not be nil")
	}
	if err := ctx.Err(); err != nil {
		return harness.Capabilities{}, err
	}
	// Admission projection never substitutes for the real pinned CLI Probe or
	// promotes its deliberately unverified native lifecycle to production support.
	native, err := witness.delegate.Probe(ctx)
	if !errors.Is(err, harness.ErrNativeUnverified) {
		return harness.Capabilities{}, fmt.Errorf("real Pi Probe must remain native-unverified, got %v", err)
	}
	if native.Kind != harness.KindPi || native.NativeVersion != pi.TestedVersion ||
		!native.VersionKnown || !native.TransportVerified || native.Verified ||
		native.Start || native.Events || native.Cancel || native.Resume || native.Handoff || native.ProviderAccess {
		return harness.Capabilities{}, errors.New("real Pi Probe differs from the pinned conservative transport projection")
	}
	return witness.capabilities, nil
}

func (witness *piControlE2EWitness) Start(ctx context.Context, request harness.StartRequest, sink harness.EventSink) (harness.Session, error) {
	if witness == nil || witness.delegate == nil {
		return nil, errors.New("test-only Pi control witness delegate is nil")
	}
	// A failed Start may never reach App's persisted-process observer. Record
	// the attempt first so the Linux refusal cannot mistake it for no launch.
	if marker := strings.TrimSpace(os.Getenv(processMarkerEnv)); marker != "" {
		if err := os.WriteFile(marker, []byte("native_start_attempted\n"), 0o600); err != nil {
			return nil, fmt.Errorf("record test-only native Start attempt: %w", err)
		}
	}
	// The witness only affects admission Probe. Native execution remains the
	// real Pi RPC adapter and therefore still owns process/session semantics.
	return witness.delegate.Start(ctx, request, sink)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
