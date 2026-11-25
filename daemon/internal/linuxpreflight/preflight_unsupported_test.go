//go:build !linux || !amd64

package linuxpreflight

import (
	"testing"

	"github.com/wxxb789/symmetry/daemon/internal/config"
)

func TestNormalizeOwnerStateDirIsIdentityOutsideLinuxAmd64(t *testing.T) {
	for _, path := range []string{"", ".", "var/lib/symmetry/state", "/var/lib/symmetry/state", "/"} {
		got, err := NormalizeOwnerStateDir(path)
		if err != nil || got != path {
			t.Fatalf("NormalizeOwnerStateDir(%q) = %q, %v; want identity passthrough", path, got, err)
		}
	}
}

func TestUnsupportedKindRefusalsAreStable(t *testing.T) {
	if err := VerifyLegacyLinuxOwnersDrained(config.Config{}); err == nil ||
		err.Error() != "legacy Linux owner drain verification is supported only on Linux amd64" {
		t.Fatalf("VerifyLegacyLinuxOwnersDrained() error = %v", err)
	}
	if err := VerifyLinuxOwnerInstallation(config.Config{}); err == nil ||
		err.Error() != "Linux owner installation verification is supported only on Linux amd64" {
		t.Fatalf("VerifyLinuxOwnerInstallation() error = %v", err)
	}
	if got := LinuxOwnerAppArmorUsernsDecision(); got != "not_needed" {
		t.Fatalf("LinuxOwnerAppArmorUsernsDecision() = %q, want the recorded not_needed token", got)
	}
	if LinuxAppArmorUsernsNotNeeded != "not_needed" {
		t.Fatalf("the shared vocabulary token = %q, want not_needed", LinuxAppArmorUsernsNotNeeded)
	}
}
