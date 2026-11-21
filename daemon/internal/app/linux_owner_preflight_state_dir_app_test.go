//go:build linux && amd64

package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/config"
	"github.com/wxxb789/symmetry/daemon/internal/state"
)

// TestRunNormalizesLinuxV2NativeOwnerStateDir pins the app.Run wiring to
// linuxpreflight.NormalizeOwnerStateDir: the linux v2 native kinds route the
// configured StateDir through the root-working-directory normalizer before
// config validation, its refusals surface wrapped, the normalized value is the
// StateDir the Store is opened with, and other kinds never enter the branch.
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

	// The normalized value must be the StateDir app.Run uses, not only a value
	// it computed: with a complete pi adapter metadata set and no WithStore,
	// Run opens the Store itself from daemon.config.StateDir. The relative
	// StateDir is the absolute temporary path without its leading separator, so
	// the normalized path is a writable temporary directory while the
	// unnormalized path would resolve against the process working directory.
	t.Run("pi relative path reaches the Store at the normalized absolute path", func(t *testing.T) {
		workingDirectory := t.TempDir()
		t.Chdir(workingDirectory)
		storeDirectory := filepath.Join(t.TempDir(), "state")
		relativeStateDir := strings.TrimPrefix(storeDirectory, string(filepath.Separator))
		if !filepath.IsAbs(storeDirectory) || filepath.IsAbs(relativeStateDir) {
			t.Fatalf("test setup: temporary StateDir %q must be absolute with a relative separator-stripped form", storeDirectory)
		}
		t.Setenv("SYMMETRY_ENROLLMENT_TOKEN", "")
		value := testConfig(t)
		value.Runtime.HarnessKind = config.RuntimeHarnessPi
		value.Runtime.HarnessVersion = "1.0.0"
		value.Runtime.AdapterVersion = "adapter-1.0.0"
		value.Runtime.AdapterProtocolVersion = 1
		value.Runtime.RepositoryResourceID = "00000000-0000-4000-8000-000000000005"
		profile := value.AgentProfiles["local"]
		profile.Command = filepath.Join(t.TempDir(), "absent-pi-adapter")
		value.AgentProfiles["local"] = profile
		workspace := value.Workspaces["local"]
		workspace.Policy = config.WorkspacePolicyGitWorktree
		workspace.Path = ""
		workspace.Repository = t.TempDir()
		workspace.Root = t.TempDir()
		workspace.Ref = "refs/heads/main"
		value.Workspaces["local"] = workspace
		value.StateDir = relativeStateDir
		err := Run(context.Background(), value)
		if err == nil || err.Error() != "SYMMETRY_ENROLLMENT_TOKEN is required for first enrollment" {
			t.Fatalf("Run() error = %v, want the first-enrollment refusal raised after the Store opened", err)
		}
		if _, statErr := os.Stat(storeDirectory); statErr != nil {
			t.Fatalf("Store was not opened at the normalized absolute StateDir %q: %v", storeDirectory, statErr)
		}
		if _, statErr := os.Stat(filepath.Join(workingDirectory, relativeStateDir)); statErr == nil {
			t.Fatalf("Store was opened under the working directory %q instead of the normalized absolute StateDir %q", workingDirectory, storeDirectory)
		}
	})
}
