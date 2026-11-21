//go:build !linux || !amd64

// Package linuxpreflight verifies a Linux amd64 host installation of the
// Symmetry daemon owner: the installed A/B accounts, the delegated cgroup
// parent, the fixed Git clone/repository ownership, the systemd units, and the
// POSIX ACLs that fence A-private state from the fixed B runtime. Every
// verifier is read-only: none of them launches a process, mutates a journal, or
// certifies a future run, and none of them holds daemon state — callers pass the
// daemon configuration and the trusted root-installed owner configuration.

package linuxpreflight

import (
	"errors"

	"github.com/wxxb789/symmetry/daemon/internal/config"
)

// LinuxOwnerPreflightRuntimeBoundary documents what static installation
// verification cannot prove. The runtime must set native HOME to B, verify the
// A-created leaf's cgroup.kill through its pinned FD, and validate per-run Git
// metadata before binding or entering the native gate.
const LinuxOwnerPreflightRuntimeBoundary = "read-only preflight does not launch B, certify a future cgroup leaf, or attest a future per-run Git metadata ACL"

// NormalizeOwnerStateDir returns the configured state directory unchanged: the
// root working directory binding is a Linux amd64 owner property.
func NormalizeOwnerStateDir(path string) (string, error) {
	return path, nil
}

// VerifyLegacyLinuxOwnersDrained reports that the legacy owner drain verifier
// requires the Linux amd64 owner topology.
func VerifyLegacyLinuxOwnersDrained(config.Config) error {
	return errors.New("legacy Linux owner drain verification is supported only on Linux amd64")
}

// VerifyLinuxOwnerInstallation reports that the host installation verifier
// requires the Linux amd64 owner topology.
func VerifyLinuxOwnerInstallation(config.Config) error {
	return errors.New("Linux owner installation verification is supported only on Linux amd64")
}
