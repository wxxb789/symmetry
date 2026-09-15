package pi

import (
	"context"
	"errors"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/execution"
	"github.com/wxxb789/symmetry/daemon/internal/harness"
)

func TestProbeMapsBoundedVersionOutputFailureToHarnessUnavailable(t *testing.T) {
	runner := probeErrorRunner{err: execution.ErrOutputLimitExceeded}

	result, err := Probe(context.Background(), "pi", runner)

	if !errors.Is(err, harness.ErrHarnessUnavailable) {
		t.Fatalf("Probe() error = %v, want ErrHarnessUnavailable", err)
	}
	if result.Capabilities.Start {
		t.Fatal("version probe failure advertised start capability")
	}
}

func TestProbeMapsBoundedHelpOutputFailureToNativeUnverified(t *testing.T) {
	runner := probeErrorRunner{
		version: []byte("0.85.1\n"),
		err:     execution.ErrOutputLimitExceeded,
	}

	result, err := Probe(context.Background(), "pi", runner)

	if !errors.Is(err, harness.ErrNativeUnverified) {
		t.Fatalf("Probe() error = %v, want ErrNativeUnverified", err)
	}
	if !result.VersionKnown || result.Capabilities.TransportVerified {
		t.Fatalf("probe result = %+v, want known version and unverified transport", result)
	}
}

type probeErrorRunner struct {
	version []byte
	err     error
}

func (runner probeErrorRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if len(args) == 1 && args[0] == "--version" {
		if runner.version != nil {
			return runner.version, nil
		}
	}
	return nil, runner.err
}
