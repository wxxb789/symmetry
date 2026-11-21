package platform

import "errors"

// ErrLinuxSupervisorResponseLost identifies a committed Linux supervisor
// request whose response transport disappeared before the client decoded it.
// Recovery must replay the exact durable authority instead of converting this
// transport outcome into descendant-scan uncertainty.
var ErrLinuxSupervisorResponseLost = errors.New("Linux supervisor response was lost after request commit")

// The ErrLinuxOwner* values below are the typed identities for the repeated
// mismatch classes that the anchor and v2-helper identification sites report.
// Each message is the exact literal those sites used while the class was
// anonymous, so every produced and serialized error text is unchanged; each
// site wraps its class with %w, so errors.Is now distinguishes a mismatch from
// a failed read of the same process state.
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

	// ErrLinuxOwnerDeviceOrInodeChanged identifies a pinned nsfs or image
	// object whose device/inode pair changed after it was recorded.
	ErrLinuxOwnerDeviceOrInodeChanged = errors.New("device or inode changed")

	// ErrLinuxOwnerDeviceOrInodeDiffers identifies the same device/inode
	// mismatch as ErrLinuxOwnerDeviceOrInodeChanged at the sites that phrase
	// it as a difference from a retained pin.
	ErrLinuxOwnerDeviceOrInodeDiffers = errors.New("device or inode differs")

	// ErrLinuxOwnerDirectoryOwnershipUnsafe identifies a root-controlled
	// directory that is not owned by root or is writable by group or world.
	ErrLinuxOwnerDirectoryOwnershipUnsafe = errors.New("directory ownership or mode is unsafe")

	// ErrLinuxOwnerGateImageIdentityDiffers identifies a pinned root-owned gate
	// image whose device/inode pair no longer matches its exact recording.
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
