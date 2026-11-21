//go:build linux && amd64

package platform

import "errors"

// Each message is the exact literal the mismatch sites used while the class was
// anonymous, so produced and serialized error text is unchanged; sites wrap with %w so errors.Is separates a mismatch from a failed read.
var (
	ErrLinuxOwnerCgroupPathChanged = errors.New("cgroup path changed")

	ErrLinuxOwnerCreationIdentityChanged = errors.New("creation identity changed")

	// The same class as ErrLinuxOwnerCreationIdentityChanged, used where the site
	// phrases the mismatch as a difference from an expected identity.
	ErrLinuxOwnerCreationIdentityDiffers = errors.New("creation identity differs")

	ErrLinuxOwnerDeviceOrInodeChanged = errors.New("device or inode changed")

	// The phrasing variant of ErrLinuxOwnerDeviceOrInodeChanged used where the
	// comparison is against a retained pin (nsfs object or gate image).
	ErrLinuxOwnerDeviceOrInodeDiffers = errors.New("device or inode differs")

	ErrLinuxOwnerDirectoryOwnershipUnsafe = errors.New("directory ownership or mode is unsafe")

	// Also reports a failed stat; it never compares device/inode - that check
	// reports ErrLinuxOwnerDeviceOrInodeDiffers.
	ErrLinuxOwnerGateImageIdentityDiffers = errors.New("root-owned image identity differs")

	// Short-form phrasing of ErrLinuxOwnerCreationIdentityChanged.
	ErrLinuxOwnerIdentityChanged = errors.New("identity changed")

	// Phrasing variant of ErrLinuxOwnerIdentityChanged, used where the site
	// compares against an expected identity.
	ErrLinuxOwnerIdentityDiffers = errors.New("identity differs")

	// The launcher pidfd is still live; the launcher must have exited before the operation continues.
	ErrLinuxOwnerLauncherPIDFDNotReady = errors.New("launcher pidfd is not ready")

	ErrLinuxOwnerParentDiffersFromHelper = errors.New("parent differs from helper")

	ErrLinuxOwnerParentNamespaceDiffers = errors.New("parent namespace differs")

	ErrLinuxOwnerParentPIDDiffers = errors.New("parent PID differs")

	// The pidfd is still live; the operation requires the process to have exited.
	ErrLinuxOwnerPIDFDNotReady = errors.New("pidfd is not ready")

	// The per-thread snapshot changed while the census was read.
	ErrLinuxOwnerThreadSnapshotDiffers = errors.New("thread snapshot differs")
)
