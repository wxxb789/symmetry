//go:build !linux || !amd64

package linuxpreflight

import (
	"errors"

	"github.com/wxxb789/symmetry/daemon/internal/config"
)

// Documents what static installation verification cannot prove: the runtime must set
// native HOME to B, verify the leaf's cgroup.kill by pinned FD, and validate per-run Git metadata.
const LinuxOwnerPreflightRuntimeBoundary = "read-only preflight does not launch B, certify a future cgroup leaf, or attest a future per-run Git metadata ACL"

// The root working directory binding is a Linux amd64 owner property.
func NormalizeOwnerStateDir(path string) (string, error) {
	return path, nil
}

func VerifyLegacyLinuxOwnersDrained(config.Config) error {
	return errors.New("legacy Linux owner drain verification is supported only on Linux amd64")
}

func VerifyLinuxOwnerInstallation(config.Config) error {
	return errors.New("Linux owner installation verification is supported only on Linux amd64")
}

// The AppArmor unprivileged-userns precondition is a Linux amd64 owner property; verification
// above always fails here, so the recorded decision is the vocabulary's not-needed token.
func LinuxOwnerAppArmorUsernsDecision() string {
	return "not_needed"
}
