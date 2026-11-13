package pi

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

const (
	piLiveProbeEnabledEnv    = "SYMMETRY_PI_NATIVE_PROBE"
	piLiveProbeExecutableEnv = "SYMMETRY_PI_NATIVE_PROBE_EXECUTABLE"
	piLiveProbeShimEnv       = "SYMMETRY_PI_NATIVE_PROBE_SHIM"
)

// piLiveProbeExecutable resolves the absolute executable an opt-in live probe
// control launches, or skips the control.
func piLiveProbeExecutable(t *testing.T) string {
	t.Helper()
	if os.Getenv(piLiveProbeEnabledEnv) != "1" {
		t.Skip("set " + piLiveProbeEnabledEnv + "=1 to run the native Pi probe controls")
	}
	executable := strings.TrimSpace(os.Getenv(piLiveProbeExecutableEnv))
	if executable == "" || !filepath.IsAbs(executable) {
		t.Fatalf("%s must name the absolute pi %s executable", piLiveProbeExecutableEnv, TestedVersion)
	}
	info, err := os.Stat(executable)
	if err != nil {
		t.Fatalf("stat native Pi probe executable: %v", err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("%s must name a regular executable file", piLiveProbeExecutableEnv)
	}
	return executable
}

// TestPiProbeLivePromotesTheTestedBlobOnlyOnWindows is the recorded promotion
// control: the exact recorded blob for this platform must be the only bytes
// that promote, and the promoted operation set is the one the Windows evidence
// covers.
func TestPiProbeLivePromotesTheTestedBlobOnlyOnWindows(t *testing.T) {
	executable := piLiveProbeExecutable(t)
	want, recorded := testedExecutableHash(runtime.GOOS)
	if !recorded {
		t.Skipf("no pi %s executable blob is recorded for platform %s", TestedVersion, runtime.GOOS)
	}
	digest, err := harness.HashExecutableFile(context.Background(), executable)
	if err != nil {
		t.Fatalf("hash native Pi executable: %v", err)
	}
	t.Logf("resolved executable %s digest %s (recorded %s blob %s)", executable, digest, runtime.GOOS, want)
	if digest != want {
		t.Fatalf("resolved executable digest = %q, want the recorded %s blob %q", digest, runtime.GOOS, want)
	}
	result, err := Probe(context.Background(), executable)
	if err := result.Capabilities.Validate(); err != nil {
		t.Fatalf("Capabilities.Validate() error = %v", err)
	}
	if result.Version != TestedVersion || !result.VersionKnown || !result.TransportKnown || result.ExecutableDigest != want || !result.ExecutableHashKnown {
		t.Fatalf("probe result = %+v, want the tested version, transport and blob", result)
	}
	promoted := result.Capabilities.Verified && result.Capabilities.Start &&
		result.Capabilities.Events && result.Capabilities.Cancel && result.Capabilities.Resume
	wantPromoted := runtime.GOOS == "windows"
	if promoted != wantPromoted {
		t.Fatalf("promoted = %t on %s, want %t: %+v (error %v)", promoted, runtime.GOOS, wantPromoted, result.Capabilities, err)
	}
	if promoted {
		if err != nil {
			t.Fatalf("promoted probe error = %v, want nil", err)
		}
		if result.Capabilities.ImplementationVersion != piImplementationVersion {
			t.Fatalf("implementation version = %q, want %q", result.Capabilities.ImplementationVersion, piImplementationVersion)
		}
	} else {
		if !errors.Is(err, harness.ErrNativeUnverified) {
			t.Fatalf("unpromoted probe error = %v, want ErrNativeUnverified", err)
		}
	}
	if result.Capabilities.Handoff || result.Capabilities.ApprovalResponse || result.Capabilities.HardCostLimit || result.Capabilities.ProviderAccess ||
		result.Capabilities.Guidance != harness.GuidanceUnsupported || result.Capabilities.Pause != harness.PauseUnsupported || result.Capabilities.Usage != harness.UsageUnknown {
		t.Fatalf("capabilities advertised unproved operations: %+v", result.Capabilities)
	}
	t.Logf("promotion projection: %+v", result.Capabilities)
}

// TestPiProbeLiveRejectsDifferentBlob is the recorded blob-mismatch control
// against a real executable: the `mise` PATH shim reports the same `1.5.2`
// version and is different bytes, so it promotes nothing. The second half
// isolates the blob gate from shim startup cost by probing the tested executable
// with the shim's real digest.
func TestPiProbeLiveRejectsDifferentBlob(t *testing.T) {
	executable := piLiveProbeExecutable(t)
	shim := strings.TrimSpace(os.Getenv(piLiveProbeShimEnv))
	if shim == "" || !filepath.IsAbs(shim) {
		t.Fatalf("%s must name the absolute shim executable used as the blob-mismatch control", piLiveProbeShimEnv)
	}
	shimDigest, err := harness.HashExecutableFile(context.Background(), shim)
	if err != nil {
		t.Fatalf("hash shim executable: %v", err)
	}
	if shimDigest == TestedExecutableHash {
		t.Fatalf("shim digest equals the tested blob; the control needs different bytes")
	}
	t.Logf("shim %s digest %s (tested blob %s at %s)", shim, shimDigest, TestedExecutableHash, executable)

	result, probeErr := Probe(context.Background(), shim)
	if probeErr == nil {
		t.Fatal("probe of a different blob succeeded, want a fail-closed rejection")
	}
	t.Logf("shim probe rejection: %v", probeErr)
	if result.Capabilities.Verified || result.Capabilities.Start || result.Capabilities.Events ||
		result.Capabilities.Cancel || result.Capabilities.Resume {
		t.Fatalf("capabilities = %+v, want no promoted operation for a different blob", result.Capabilities)
	}
	for _, capability := range piStagedOperations() {
		if reason := result.Capabilities.Unsupported[string(capability)]; strings.TrimSpace(reason) == "" {
			t.Fatalf("%s carries no unsupported reason: %+v", capability, result.Capabilities)
		}
	}

	// Isolate the blob gate: real version and help output from the tested
	// executable, the shim's real digest. Nothing may be promoted.
	result, probeErr = Probe(context.Background(), executable, &piForeignBlobRunner{digest: shimDigest})
	if !errors.Is(probeErr, harness.ErrNativeUnverified) {
		t.Fatalf("blob-gate probe error = %v, want ErrNativeUnverified", probeErr)
	}
	if result.ExecutableDigest != shimDigest || !result.ExecutableHashKnown || !result.TransportKnown {
		t.Fatalf("blob-gate probe result = %+v, want the foreign digest with real transport evidence", result)
	}
	if result.Capabilities.Verified || result.Capabilities.Start || result.Capabilities.Events ||
		result.Capabilities.Cancel || result.Capabilities.Resume {
		t.Fatalf("capabilities = %+v, want no promoted operation for foreign bytes", result.Capabilities)
	}
	if reason := result.Capabilities.Unsupported[string(harness.CapabilityStart)]; !strings.Contains(reason, "exact tested executable blob") {
		t.Fatalf("blob-gate reason = %q, want the exact-blob rejection", reason)
	}
}

// piForeignBlobRunner probes the real executable while declaring different
// bytes, so the blob gate is exercised independently of the executable it
// launches.
type piForeignBlobRunner struct {
	digest string
	calls  []string
}

func (runner *piForeignBlobRunner) Run(ctx context.Context, executable string, args ...string) ([]byte, error) {
	runner.calls = append(runner.calls, strings.Join(args, " "))
	return (osCommandRunner{}).Run(ctx, executable, args...)
}

func (runner *piForeignBlobRunner) ExecutableDigest(context.Context, string) (string, error) {
	return runner.digest, nil
}
