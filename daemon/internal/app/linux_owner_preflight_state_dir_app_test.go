//go:build linux && amd64

package app

import (
	"context"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

// TestRunNormalizesLinuxV2NativeOwnerStateDir pins the app.Run wiring to
// linuxpreflight.NormalizeOwnerStateDir: the linux v2 native kinds route the
// configured StateDir through the root-working-directory normalizer before
// config validation, its refusals surface wrapped, and other kinds never enter
// the branch.
func TestRunNormalizesLinuxV2NativeOwnerStateDir(t *testing.T) {
	for _, test := range []struct {
		name     string
		kind     string
		stateDir string
		want     string
	}{
		{
			name:     "pi root-resolving relative path",
			kind:     config.RuntimeHarnessPi,
			stateDir: ".",
			want:     "normalize Linux v2 StateDir against root working directory: StateDir must not resolve to the filesystem root",
		},
		{
			name:     "opencode empty path",
			kind:     config.RuntimeHarnessOpenCode,
			stateDir: "",
			want:     "normalize Linux v2 StateDir against root working directory: StateDir is empty or contains a forbidden character",
		},
		{
			name:     "pi relative path normalizes before validation",
			kind:     config.RuntimeHarnessPi,
			stateDir: "var/lib/symmetry/state",
			want:     "validate config: runtime adapter metadata must include harness_kind, harness_version, adapter_version, and adapter_protocol_version",
		},
		{
			name:     "generic kind skips normalization",
			kind:     config.RuntimeHarnessGeneric,
			stateDir: "",
			want:     "validate config: state_dir must not be empty",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, err := state.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			value := testConfig(t)
			value.Runtime.HarnessKind = test.kind
			value.StateDir = test.stateDir
			err = Run(context.Background(), value, WithStore(store))
			if err == nil || err.Error() != test.want {
				t.Fatalf("Run() error = %v, want %q", err, test.want)
			}
		})
	}
}
