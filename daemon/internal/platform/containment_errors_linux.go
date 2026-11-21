//go:build linux && amd64

package platform

import "errors"

// The ErrLinuxOwner* values below are the typed identities for the repeated
// mismatch classes that the anchor and v2-helper identification sites report.
// Each message is the exact literal those sites used while the class was
// anonymous, so every produced and serialized error text is unchanged; each
// site wraps its class with %w, so errors.Is now distinguishes a mismatch from
// a failed read of the same process state.
//
// All 78 producing sites are in linux && amd64 files, so the values live in
// this linux && amd64 file rather than in an unconstrained one, where they
// would be exported dead API on windows and darwin. The cross-platform
// ErrLinuxSupervisorResponseLost stays in containment_errors.go.
var (
	// ErrLinuxOwnerCgroupPathChanged identifies a pinned Linux owner process
	// that no longer occupies its exact recorded cgroup path.
	ErrLinuxOwnerCgroupPathChanged = errors.New("cgroup path changed")

	// ErrLinuxOwnerCreationIdentityChanged identifies a Linux owner process
	// whose live creation identity (boot ID, PID namespace, PID, start time)
	// no longer equals the identity recorded for it.
	ErrLinuxOwnerCreationIdentityChanged = errors.New("creation identity changed")

	// ErrLinuxOwnerCreationIdentityDiffers identifies the same creation
	// identity mismatch as ErrLinuxOwnerCreationIdentityChanged at the sites
	// that phrase it as a difference from an expected identity.
	ErrLinuxOwnerCreationIdentityDiffers = errors.New("creation identity differs")

	// ErrLinuxOwnerDeviceOrInodeChanged identifies a pinned user-namespace
	// (nsfs) object whose device/inode identity no longer equals the identity
	// recorded for it.
	ErrLinuxOwnerDeviceOrInodeChanged = errors.New("device or inode changed")

	// ErrLinuxOwnerDeviceOrInodeDiffers identifies a pinned user-namespace
	// (nsfs) object or gate image whose device/inode pair differs from its
	// retained recording; it is the phrasing variant of
	// ErrLinuxOwnerDeviceOrInodeChanged used where the comparison is against a
	// retained pin.
	ErrLinuxOwnerDeviceOrInodeDiffers = errors.New("device or inode differs")

	// ErrLinuxOwnerDirectoryOwnershipUnsafe identifies a root-controlled
	// directory that is not owned by root or is writable by group or world.
	ErrLinuxOwnerDirectoryOwnershipUnsafe = errors.New("directory ownership or mode is unsafe")

	// ErrLinuxOwnerGateImageIdentityDiffers identifies a retained static gate
	// image FD that cannot be confirmed as a single-linked, root-owned regular
	// file with owner-execute permission and without group/world write
	// permission; a failed stat reports here too. It never compares
	// device/inode - the image device/inode check reports
	// ErrLinuxOwnerDeviceOrInodeDiffers.
	ErrLinuxOwnerGateImageIdentityDiffers = errors.New("root-owned image identity differs")

	// ErrLinuxOwnerIdentityChanged identifies a process whose live creation
	// identity no longer equals the identity recorded for it; it is the
	// short-form phrasing of ErrLinuxOwnerCreationIdentityChanged.
	ErrLinuxOwnerIdentityChanged = errors.New("identity changed")

	// ErrLinuxOwnerIdentityDiffers identifies the same creation identity
	// mismatch as ErrLinuxOwnerIdentityChanged at the sites that phrase it as
	// a difference from an expected identity.
	ErrLinuxOwnerIdentityDiffers = errors.New("identity differs")

	// ErrLinuxOwnerLauncherPIDFDNotReady identifies an exact root-launcher
	// pidfd that is still live: the launcher must have exited before the
	// operation continues.
	ErrLinuxOwnerLauncherPIDFDNotReady = errors.New("launcher pidfd is not ready")

	// ErrLinuxOwnerParentDiffersFromHelper identifies an adopted Linux owner
	// child whose parent PID is not the exact helper.
	ErrLinuxOwnerParentDiffersFromHelper = errors.New("parent differs from helper")

	// ErrLinuxOwnerParentNamespaceDiffers identifies a gate user namespace that
	// is not a direct child of the exact pinned parent namespace.
	ErrLinuxOwnerParentNamespaceDiffers = errors.New("parent namespace differs")

	// ErrLinuxOwnerParentPIDDiffers identifies a Linux owner process whose
	// parent PID is not the exact expected parent.
	ErrLinuxOwnerParentPIDDiffers = errors.New("parent PID differs")

	// ErrLinuxOwnerPIDFDNotReady identifies an exact pidfd that is still live:
	// the operation requires the process to have exited.
	ErrLinuxOwnerPIDFDNotReady = errors.New("pidfd is not ready")

	// ErrLinuxOwnerThreadSnapshotDiffers identifies an all-thread child census
	// whose per-thread snapshot changed while it was read.
	ErrLinuxOwnerThreadSnapshotDiffers = errors.New("thread snapshot differs")
)
