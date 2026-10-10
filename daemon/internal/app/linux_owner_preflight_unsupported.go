//go:build !linux || !amd64

package app

import (
	"errors"

	"github.com/wxxb789/symmetry/daemon/internal/config"
)

const LinuxOwnerPreflightRuntimeBoundary = "read-only preflight does not launch B, certify a future cgroup leaf, or attest a future per-run Git metadata ACL"

func VerifyLegacyLinuxOwnersDrained(config.Config) error {
	return errors.New("legacy Linux owner drain verification is supported only on Linux amd64")
}

func VerifyLinuxOwnerInstallation(config.Config) error {
	return errors.New("Linux owner installation verification is supported only on Linux amd64")
}
