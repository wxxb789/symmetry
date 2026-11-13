//go:build symmetry_pi_control_e2e

package main

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/harness"
	"github.com/wxxb789/symmetry/daemon/internal/harness/pi"
)

const witnessProbeForeignDigest = "sha256:53ca4fc6cfaf2b9b9a9635c54f17e5f4bf6ce599693dc67b17783bf382643850"

type witnessProbeFixtures struct {
	responses map[string][]byte
	digest    string
	err       error
}

func (runner witnessProbeFixtures) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if runner.err != nil {
		return nil, runner.err
	}
	response, ok := runner.responses[strings.Join(args, " ")]
	if !ok {
		return nil, errors.New("fixture command not found")
	}
	return response, nil
}

func (runner witnessProbeFixtures) ExecutableDigest(context.Context, string) (string, error) {
	if runner.digest == "" {
		return "", errors.New("fixture declares no executable digest")
	}
	return runner.digest, nil
}

// TestPiControlE2EWitnessProbeContract pins the admission witness against the
// real probe contract: the delegate must report the pinned transport, may
// promote only the operation set this build proves on the platform the evidence
// covers, and can never make the witness present anything other than its own
// admission projection.
func TestPiControlE2EWitnessProbeContract(t *testing.T) {
	outputs := map[string][]byte{
		"--version": []byte(pi.TestedVersion + "\n"),
		"--help":    []byte("Options:\n  --mode <mode>  Output mode: text (default), json, or rpc\n"),
	}
	for _, test := range []struct {
		name        string
		digest      string
		probeErr    error
		wantFailure bool
	}{
		{name: "fixture without a declared digest"},
		{name: "tested blob", digest: pi.TestedExecutableHash},
		{name: "foreign blob", digest: witnessProbeForeignDigest},
		{name: "hard probe failure", probeErr: errors.New("fixture probe failure"), wantFailure: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			witness := newPiControlE2EWitnessWithDelegate(pi.NewAdapterWithRunner("pi-fixture", witnessProbeFixtures{
				responses: outputs,
				digest:    test.digest,
				err:       test.probeErr,
			}))
			capabilities, err := witness.Probe(context.Background())
			if test.wantFailure {
				if err == nil {
					t.Fatalf("witness probe admitted a hard delegate failure: %+v", capabilities)
				}
				if !strings.Contains(err.Error(), "outside the unverified contract") {
					t.Fatalf("witness probe error = %v, want the unverified-contract failure", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("witness probe error = %v", err)
			}
			if !reflect.DeepEqual(capabilities, witness.capabilities) {
				t.Fatalf("witness projection = %+v, want its own admission projection %+v", capabilities, witness.capabilities)
			}
			if capabilities.ImplementationVersion != witnessImplementation || !capabilities.Verified ||
				!capabilities.Start || !capabilities.Events || !capabilities.Cancel || !capabilities.Resume || !capabilities.Handoff {
				t.Fatalf("witness projection = %+v, want the test-only admission identity", capabilities)
			}
			if err := capabilities.Validate(); err != nil {
				t.Fatalf("witness projection contract: %v", err)
			}
			// The witness must accept exactly the delegate states the probe can
			// produce: the fail-closed projection, and the operation set this build
			// proves for the tested blob on the platform that carries the evidence.
			native, nativeErr := witness.delegate.Probe(context.Background())
			promoted := native.Verified && native.Start && native.Events && native.Cancel && native.Resume
			wantPromoted := test.digest == pi.TestedExecutableHash && runtime.GOOS == "windows"
			if promoted != wantPromoted {
				t.Fatalf("delegate promoted = %t on %s, want %t: %+v", promoted, runtime.GOOS, wantPromoted, native)
			}
			if wantPromoted && nativeErr != nil {
				t.Fatalf("promoted delegate probe error = %v, want nil", nativeErr)
			}
			if !wantPromoted && !errors.Is(nativeErr, harness.ErrNativeUnverified) {
				t.Fatalf("unpromoted delegate probe error = %v, want ErrNativeUnverified", nativeErr)
			}
		})
	}
}
