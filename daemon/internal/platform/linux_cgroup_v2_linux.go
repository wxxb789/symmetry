//go:build linux

package platform

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	linuxgate "github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

var (
	// ErrLinuxCgroupV2Unresolved means the pinned cgroup identity or an operation
	// on it can no longer be established. Callers must retain the journal marker.
	ErrLinuxCgroupV2Unresolved = errors.New("Linux cgroup v2 ownership is unresolved")
	// ErrLinuxCgroupV2NotDelegated means the current effective UID cannot manage
	// the exact cgroup v2 parent using the required kernel control files.
	ErrLinuxCgroupV2NotDelegated = errors.New("Linux cgroup v2 parent is not delegated to this process")
	// ErrLinuxCgroupV2NotCgroup2 means the selected path is not on a verified
	// unified cgroup v2 mount with the required control files.
	ErrLinuxCgroupV2NotCgroup2          = errors.New("path is not on the verified cgroup v2 mount")
	errLinuxCgroupKillWriteNotAttempted = errors.New("Linux cgroup.kill write was not attempted")
)

const linuxCgroupV2EventsMax = 4096

// LinuxCgroupV2Parent is a live pin on one operator-delegated cgroup v2
// directory. Its identity is descriptive: after all live pins are lost it
// cannot be reopened safely from its saved path or inode alone.
type LinuxCgroupV2Parent struct {
	mu       sync.RWMutex
	fd       int
	identity authority.LinuxCgroupParentIdentity
	ownerUID uint32
}

// LinuxCgroupV2Leaf holds independent pins on a reserved run leaf and its
// delegated parent. Close permanently retires this instance; matching a later
// path or inode never restores its authority.
type LinuxCgroupV2Leaf struct {
	mu       sync.RWMutex
	parentFD int
	leafFD   int
	identity authority.LinuxCgroupIdentity
	ownerUID uint32
}

// LinuxCgroupV2RemovalAuthorization carries the exact owner record and the
// durable stop receipt read back by its journal owner. Exactly one of
// Supervisor and Handoff must be present. The platform validates their
// structural binding, but does not create or persist a receipt.
type LinuxCgroupV2RemovalAuthorization struct {
	Supervisor *authority.Supervisor
	Handoff    *authority.SupervisorHandoff
	Receipt    authority.StopReceipt
}

// LinuxCgroupV2PreGateRemovalAuthorization carries the exact persisted
// pre-gate receipt and per-run secret needed to verify the root launcher's
// authenticated never-cloned outcome. The caller must read both from the same
// durable RunJournal after its successful persistence barrier; this is not the
// no-target abort path and is usable only for the exact pinned leaf.
type LinuxCgroupV2PreGateRemovalAuthorization struct {
	Receipt               authority.LinuxCgroupPreGateStopReceipt
	Secret                string
	LauncherGrantPossible bool
}

// LinuxCgroupV2Release is an opaque capability showing that the platform
// verified exact receipt-backed leaf removal or receipt-backed absence under a
// still-pinned parent. Its private fields prevent callers outside this package
// from fabricating it; the zero value is unissued. Callers may snapshot the
// durable release proof only for the journal owner's exact CAS.
type LinuxCgroupV2Release struct {
	proof        authority.SupervisorHandoffReleaseProof
	preGateProof authority.LinuxCgroupPreGateReleaseProof
	kind         uint8
	issued       bool
}

const (
	linuxCgroupV2ReleaseKindOwner uint8 = iota + 1
	linuxCgroupV2ReleaseKindPreGate
)

// Snapshot returns a deep copy of the release proof and whether this value was
// issued by a successful pinned cgroup removal/absence operation. A zero value
// or malformed internal proof is never authorization.
func (release LinuxCgroupV2Release) Snapshot() (authority.SupervisorHandoffReleaseProof, bool) {
	if !release.issued || release.kind != linuxCgroupV2ReleaseKindOwner || release.proof.Validate() != nil {
		return authority.SupervisorHandoffReleaseProof{}, false
	}
	return release.proof.Clone(), true
}

// SnapshotPreGate returns the durable pre-gate removal proof only for the
// separately authorized never-cloned launcher outcome. A normal gate-bound
// release token cannot be consumed as a PID-zero reservation release.
func (release LinuxCgroupV2Release) SnapshotPreGate() (authority.LinuxCgroupPreGateReleaseProof, bool) {
	if !release.issued || release.kind != linuxCgroupV2ReleaseKindPreGate || release.preGateProof.Validate() != nil {
		return authority.LinuxCgroupPreGateReleaseProof{}, false
	}
	return release.preGateProof.Clone(), true
}

func issueLinuxCgroupV2Release(proof authority.SupervisorHandoffReleaseProof) (LinuxCgroupV2Release, error) {
	if err := proof.Validate(); err != nil {
		return LinuxCgroupV2Release{}, fmt.Errorf("verified Linux cgroup v2 removal produced invalid release proof: %w", err)
	}
	return LinuxCgroupV2Release{proof: proof.Clone(), kind: linuxCgroupV2ReleaseKindOwner, issued: true}, nil
}

func issueLinuxCgroupV2PreGateRelease(proof authority.LinuxCgroupPreGateReleaseProof) (LinuxCgroupV2Release, error) {
	if err := proof.Validate(); err != nil {
		return LinuxCgroupV2Release{}, fmt.Errorf("verified pre-gate cgroup removal produced invalid release proof: %w", err)
	}
	return LinuxCgroupV2Release{preGateProof: proof.Clone(), kind: linuxCgroupV2ReleaseKindPreGate, issued: true}, nil
}

// LinuxCgroupV2AbortAuthorization is the caller's independent pre-effect
// fencing evidence for removing a reserved leaf before any launcher grant.
// Its booleans are caller attestations; this primitive additionally verifies
// the live pinned cgroup identity, emptiness and actual rmdir outcome.
type LinuxCgroupV2AbortAuthorization struct {
	LaunchToken          string
	OwnerUID             uint32
	ReservedLeafIdentity authority.LinuxCgroupIdentity
	CreateStarted        bool
	// LauncherGrantPossible is copied from the exact durable reservation
	// readback after its persistence barrier; it is not inferred locally.
	LauncherGrantPossible bool
	Anchor                authority.LinuxCgroupProcessIdentity
	// Mirror is copied from the same exact reservation readback, including the
	// initial pre-mkdir anchor/mirror binding.
	Mirror             authority.LinuxCgroupProcessIdentity
	HelperWasCreated   bool
	Helper             authority.LinuxCgroupProcessIdentity
	HelperExit         *authority.LinuxCgroupExitProof
	LauncherWasCreated bool
	Launcher           authority.LinuxCgroupProcessIdentity
	LauncherExit       *authority.LinuxCgroupExitProof
	CreatorFenced      bool
	NoProducerCanAct   bool
}

type linuxCgroupV2Mount struct {
	id         uint64
	root       string
	mountPoint string
}

type linuxCgroupV2Stat struct {
	device uint64
	inode  uint64
	mount  uint64
	mode   uint32
	uid    uint32
}

// OpenLinuxCgroupV2Parent opens an exact visible cgroup2 mount and a delegated
// directory beneath it. delegatedPath is a canonical relative path from that
// mount's root (for example "user.slice/symmetry.slice"); it is not a host
// filesystem path. Symlinks, magic links, and nested mount traversal are
// rejected. The directory must be owned and writable by the current non-root
// effective UID, with writable cgroup.procs/cgroup.subtree_control, present
// cgroup.kill, and readable cgroup.events/cgroup.controllers controls. The
// manager cgroup.kill file may remain root-owned; each run leaf must expose an
// A-owned writable kill control before it can be bound or used. No cgroup is
// created by this call.
func OpenLinuxCgroupV2Parent(mountPoint, delegatedPath string) (*LinuxCgroupV2Parent, error) {
	if !canonicalAbsoluteLinuxPath(mountPoint) || !canonicalDelegatedCgroupPath(delegatedPath) {
		return nil, errors.New("Linux cgroup v2 mount point or delegated path is not canonical")
	}
	effectiveUID := uint32(os.Geteuid())
	if effectiveUID == 0 {
		return nil, fmt.Errorf("%w: cgroup v2 delegation must be opened by the unprivileged owner", ErrLinuxCgroupV2NotDelegated)
	}

	mountFD, err := openLinuxCgroupPath(unix.AT_FDCWD, mountPoint, false)
	if err != nil {
		return nil, fmt.Errorf("open cgroup v2 mount point: %w", err)
	}
	defer unix.Close(mountFD)
	if err := requireLinuxCgroup2FD(mountFD); err != nil {
		return nil, err
	}
	mountStat, err := statLinuxCgroupFD(mountFD)
	if err != nil {
		return nil, fmt.Errorf("stat cgroup v2 mount point: %w", err)
	}
	mounts, err := readLinuxCgroupV2MountInfo()
	if err != nil {
		return nil, fmt.Errorf("read cgroup v2 mount table: %w", err)
	}
	var mount *linuxCgroupV2Mount
	for i := range mounts {
		if mounts[i].id == mountStat.mount && mounts[i].mountPoint == mountPoint {
			if mount != nil {
				return nil, errors.New("cgroup v2 mount identity is ambiguous")
			}
			mount = &mounts[i]
		}
	}
	if mount == nil {
		return nil, fmt.Errorf("%w: visible mount ID %d is not a cgroup2 entry at %q", ErrLinuxCgroupV2NotCgroup2, mountStat.mount, mountPoint)
	}

	parentFD, err := openLinuxCgroupPath(mountFD, delegatedPath, true)
	if err != nil {
		return nil, fmt.Errorf("open delegated cgroup v2 parent: %w", err)
	}
	closeParent := true
	defer func() {
		if closeParent {
			_ = unix.Close(parentFD)
		}
	}()
	if err := requireLinuxCgroup2FD(parentFD); err != nil {
		return nil, err
	}
	parentStat, err := statLinuxCgroupFD(parentFD)
	if err != nil {
		return nil, fmt.Errorf("stat delegated cgroup v2 parent: %w", err)
	}
	if parentStat.mount != mount.id || parentStat.device == 0 || parentStat.inode == 0 {
		return nil, fmt.Errorf("%w: delegated parent crossed or changed mount", ErrLinuxCgroupV2Unresolved)
	}
	fullPath := mount.root
	if delegatedPath != "" {
		fullPath = path.Join(fullPath, delegatedPath)
	}
	identity := authority.LinuxCgroupParentIdentity{
		BootID: readLinuxCgroupBootID(), MountID: mount.id,
		Device: parentStat.device, Inode: parentStat.inode, Path: fullPath,
	}
	if identity.BootID == "" || identity.Validate() != nil {
		return nil, errors.New("cgroup v2 parent identity is invalid")
	}
	if parentStat.uid != effectiveUID || parentStat.mode&0300 != 0300 {
		return nil, fmt.Errorf("%w: delegated parent must be owned and writable/searchable by effective UID %d", ErrLinuxCgroupV2NotDelegated, effectiveUID)
	}
	if err := verifyLinuxCgroupV2Controls(parentFD, effectiveUID); err != nil {
		return nil, err
	}
	closeParent = false
	return &LinuxCgroupV2Parent{fd: parentFD, identity: identity, ownerUID: effectiveUID}, nil
}

// AdoptLinuxCgroupV2Parent duplicates an authenticated transferred live parent
// FD and verifies its exact boot/mount/device/inode identity and delegation.
// The caller must prove the FD came from a continuously retained parent pin;
// this function does not reopen expected.Path and matching path/stat alone is
// not transfer provenance.
func AdoptLinuxCgroupV2Parent(pinnedFD int, expected authority.LinuxCgroupParentIdentity) (*LinuxCgroupV2Parent, error) {
	effectiveUID := uint32(os.Geteuid())
	if pinnedFD < 0 {
		return nil, ErrLinuxCgroupV2Unresolved
	}
	if effectiveUID == 0 || expected.Validate() != nil || expected.BootID != readLinuxCgroupBootID() {
		return nil, errors.New("Linux cgroup v2 parent adoption requires a live transferred FD and current exact identity")
	}
	fd, err := unix.FcntlInt(uintptr(pinnedFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("duplicate transferred cgroup v2 parent FD: %w", err)
	}
	closeFD := true
	defer func() {
		if closeFD {
			_ = unix.Close(fd)
		}
	}()
	if err := requireLinuxCgroup2FD(fd); err != nil {
		return nil, err
	}
	stat, err := statLinuxCgroupFD(fd)
	if err != nil || stat.mount != expected.MountID || stat.device != expected.Device || stat.inode != expected.Inode ||
		stat.mode&unix.S_IFMT != unix.S_IFDIR || stat.uid != effectiveUID || stat.mode&0300 != 0300 {
		return nil, fmt.Errorf("%w: transferred parent FD differs from the durable identity or UID", ErrLinuxCgroupV2Unresolved)
	}
	mounts, err := readLinuxCgroupV2MountInfo()
	if err != nil {
		return nil, fmt.Errorf("read cgroup v2 mount table for transferred parent: %w", err)
	}
	foundMount := false
	for _, mount := range mounts {
		if mount.id == expected.MountID && (mount.root == "/" || expected.Path == mount.root || strings.HasPrefix(expected.Path, mount.root+"/")) {
			foundMount = true
			break
		}
	}
	if !foundMount {
		return nil, fmt.Errorf("%w: transferred parent mount ID is no longer a visible cgroup2 mount", ErrLinuxCgroupV2Unresolved)
	}
	if err := verifyLinuxCgroupV2Controls(fd, effectiveUID); err != nil {
		return nil, err
	}
	closeFD = false
	return &LinuxCgroupV2Parent{fd: fd, identity: expected, ownerUID: effectiveUID}, nil
}

// Identity returns the immutable measured parent identity.
func (parent *LinuxCgroupV2Parent) Identity() authority.LinuxCgroupParentIdentity {
	if parent == nil {
		return authority.LinuxCgroupParentIdentity{}
	}
	return parent.identity
}

// OwnerUID returns the effective UID that successfully opened and verified the
// delegated parent. It is the trusted A identity, not the native B UID.
func (parent *LinuxCgroupV2Parent) OwnerUID() uint32 {
	if parent == nil {
		return 0
	}
	return parent.ownerUID
}

// DuplicateFD returns a close-on-exec duplicate for authenticated transfer to
// another trusted A owner. The receiver should use AdoptLinuxCgroupV2Parent;
// it must never substitute a path reopen if this FD is lost.
func (parent *LinuxCgroupV2Parent) DuplicateFD() (*os.File, error) {
	if parent == nil {
		return nil, ErrLinuxCgroupV2Unresolved
	}
	parent.mu.RLock()
	defer parent.mu.RUnlock()
	if err := parent.verifyPinnedParent(); err != nil {
		return nil, err
	}
	fd, err := unix.FcntlInt(uintptr(parent.fd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("duplicate pinned cgroup v2 parent: %w", err)
	}
	return os.NewFile(uintptr(fd), "symmetry-cgroup-v2-parent"), nil
}

// Close releases the live delegated-parent pin. Existing leaves keep their
// separately duplicated parent FD, but this object cannot create more leaves.
func (parent *LinuxCgroupV2Parent) Close() error {
	if parent == nil {
		return nil
	}
	parent.mu.Lock()
	defer parent.mu.Unlock()
	if parent.fd < 0 {
		return nil
	}
	err := unix.Close(parent.fd)
	parent.fd = -1
	return err
}

// CompleteRemovalAfterStopReceipt completes an already-authorized removal
// when the leaf FD was lost after a durable exact stop receipt. It acts only
// on the still-pinned delegated parent and requires the reserved name to be
// absent there. It cannot reopen, kill, or remove a leaf or mint a receipt;
// without the exact receipt and continuous parent pin it fails closed.
func (parent *LinuxCgroupV2Parent) CompleteRemovalAfterStopReceipt(ctx context.Context, identity authority.LinuxCgroupIdentity, authorization LinuxCgroupV2RemovalAuthorization) (LinuxCgroupV2Release, error) {
	if ctx == nil {
		return LinuxCgroupV2Release{}, errors.New("Linux cgroup v2 absence completion requires a context")
	}
	if parent == nil || identity.Validate() != nil || !identity.Parent.Equal(parent.identity) {
		return LinuxCgroupV2Release{}, ErrLinuxCgroupV2Unresolved
	}
	authorization = cloneLinuxCgroupV2RemovalAuthorization(authorization)
	if _, err := validateLinuxCgroupRemovalAuthorization(identity, parent.ownerUID, authorization); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	parent.mu.RLock()
	defer parent.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	if parent.fd < 0 {
		return LinuxCgroupV2Release{}, ErrLinuxCgroupV2Unresolved
	}
	if err := parent.verifyPinnedParent(); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	if err := ensureLinuxCgroupNameAbsent(parent.fd, identity.LeafName, identity.Parent.MountID, identity.Parent.Device); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	proof, err := linuxCgroupReleaseProof(authorization, authority.LinuxCgroupLeafAbsentAfterReceipt)
	if err != nil {
		return LinuxCgroupV2Release{}, err
	}
	return issueLinuxCgroupV2Release(proof)
}

// CompletePreGateRemovalAfterReceipt completes an already-authorized pre-gate
// removal when the leaf FD was lost after the durable receipt. It acts only on
// the still-pinned parent and requires the exact reserved name to be absent.
// The authenticated launcher outcome and physical-stop proof are revalidated;
// absence alone never creates a release.
func (parent *LinuxCgroupV2Parent) CompletePreGateRemovalAfterReceipt(ctx context.Context, identity authority.LinuxCgroupIdentity, authorization LinuxCgroupV2PreGateRemovalAuthorization) (LinuxCgroupV2Release, error) {
	if ctx == nil {
		return LinuxCgroupV2Release{}, errors.New("Linux cgroup pre-gate absence completion requires a context")
	}
	if parent == nil || identity.Validate() != nil || !identity.Parent.Equal(parent.identity) {
		return LinuxCgroupV2Release{}, ErrLinuxCgroupV2Unresolved
	}
	authorization = cloneLinuxCgroupV2PreGateRemovalAuthorization(authorization)
	if err := validateLinuxCgroupV2PreGateRemovalAuthorization(identity, parent.ownerUID, authorization); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	parent.mu.RLock()
	defer parent.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	if parent.fd < 0 {
		return LinuxCgroupV2Release{}, ErrLinuxCgroupV2Unresolved
	}
	if err := parent.verifyPinnedParent(); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	if err := ensureLinuxCgroupNameAbsent(parent.fd, identity.LeafName, identity.Parent.MountID, identity.Parent.Device); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	proof, err := linuxCgroupV2PreGateReleaseProof(authorization.Receipt, authority.LinuxCgroupLeafAbsentAfterReceipt)
	if err != nil {
		return LinuxCgroupV2Release{}, err
	}
	return issueLinuxCgroupV2PreGateRelease(proof)
}

// CreateLeaf reserves only the caller's immutable random leaf name beneath the
// pinned parent. On any ambiguous mkdir/open result the path is left untouched
// for journal-led recovery; this method never guesses whether creation failed.
func (parent *LinuxCgroupV2Parent) CreateLeaf(name string) (*LinuxCgroupV2Leaf, error) {
	if !validLinuxCgroupLeafName(name) {
		return nil, errors.New("Linux cgroup v2 leaf name must be 16 random bytes encoded as lowercase hex")
	}
	if parent == nil {
		return nil, ErrLinuxCgroupV2Unresolved
	}
	parent.mu.RLock()
	defer parent.mu.RUnlock()
	if parent.fd < 0 {
		return nil, ErrLinuxCgroupV2Unresolved
	}
	if err := parent.verifyPinnedParent(); err != nil {
		return nil, err
	}
	if err := unix.Mkdirat(parent.fd, name, 0755); err != nil {
		return nil, fmt.Errorf("%w: create reserved cgroup v2 leaf: %w", ErrLinuxCgroupV2Unresolved, err)
	}
	leafFD, err := openLinuxCgroupPath(parent.fd, name, true)
	if err != nil {
		return nil, fmt.Errorf("%w: open newly created cgroup v2 leaf: %w", ErrLinuxCgroupV2Unresolved, err)
	}
	leaf, err := parent.bindOpenedLeaf(name, leafFD, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: bind newly created cgroup v2 leaf: %w", ErrLinuxCgroupV2Unresolved, err)
	}
	return leaf, nil
}

// AdoptLeaf binds a caller-supplied live directory FD to the reserved path.
// The FD must have been retained or transferred through an authenticated
// ownership channel; this method never reopens a path to recreate provenance.
// Matching a saved identity after all prior pins were lost is insufficient.
func (parent *LinuxCgroupV2Parent) AdoptLeaf(name string, pinnedFD int, expected authority.LinuxCgroupIdentity) (*LinuxCgroupV2Leaf, error) {
	if pinnedFD < 0 {
		return nil, ErrLinuxCgroupV2Unresolved
	}
	if !validLinuxCgroupLeafName(name) || expected.Validate() != nil || expected.LeafName != name {
		return nil, errors.New("Linux cgroup v2 adoption requires a live FD and exact reserved identity")
	}
	if parent == nil {
		return nil, ErrLinuxCgroupV2Unresolved
	}
	parent.mu.RLock()
	defer parent.mu.RUnlock()
	if parent.fd < 0 || !expected.Parent.Equal(parent.identity) {
		return nil, ErrLinuxCgroupV2Unresolved
	}
	if err := parent.verifyPinnedParent(); err != nil {
		return nil, err
	}
	leafFD, err := unix.FcntlInt(uintptr(pinnedFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("duplicate transferred cgroup v2 leaf FD: %w", err)
	}
	return parent.bindOpenedLeaf(name, leafFD, &expected)
}

func (parent *LinuxCgroupV2Parent) bindOpenedLeaf(name string, leafFD int, expected *authority.LinuxCgroupIdentity) (*LinuxCgroupV2Leaf, error) {
	closeLeaf := true
	defer func() {
		if closeLeaf {
			_ = unix.Close(leafFD)
		}
	}()
	if err := requireLinuxCgroup2FD(leafFD); err != nil {
		return nil, err
	}
	leafStat, err := statLinuxCgroupFD(leafFD)
	if err != nil {
		return nil, err
	}
	parentStat, err := statLinuxCgroupFD(parent.fd)
	if err != nil {
		return nil, err
	}
	if leafStat.mount != parent.identity.MountID || leafStat.device != parent.identity.Device || leafStat.uid != parent.ownerUID ||
		parentStat.mount != parent.identity.MountID || parentStat.uid != parent.ownerUID || parentStat.mode&0300 != 0300 ||
		leafStat.device == 0 || leafStat.inode == 0 || leafStat.mode&unix.S_IFMT != unix.S_IFDIR || leafStat.mode&0300 != 0300 {
		return nil, fmt.Errorf("%w: cgroup v2 leaf crossed the pinned delegated mount", ErrLinuxCgroupV2Unresolved)
	}
	if err := requireLinuxCgroupNameAt(parent.fd, name, leafStat); err != nil {
		return nil, err
	}
	identity := authority.LinuxCgroupIdentity{
		Version: authority.LinuxCgroupIdentityVersion, Parent: parent.identity,
		LeafName: name, LeafDevice: leafStat.device, LeafInode: leafStat.inode,
	}
	if identity.Validate() != nil || expected != nil && !identity.Equal(*expected) {
		return nil, fmt.Errorf("%w: opened cgroup v2 leaf differs from its immutable identity", ErrLinuxCgroupV2Unresolved)
	}
	killFD, err := openLinuxCgroupControlAt(leafFD, "cgroup.kill", unix.O_WRONLY)
	if err != nil {
		return nil, fmt.Errorf("%w: A cannot open exact leaf cgroup.kill: %v", ErrLinuxCgroupV2NotDelegated, err)
	}
	killErr := verifyLinuxCgroupKillControl(killFD, parent.identity.MountID, parent.identity.Device, parent.ownerUID)
	closeKillErr := unix.Close(killFD)
	if killErr != nil || closeKillErr != nil {
		return nil, errors.Join(killErr, closeKillErr)
	}
	parentFD, err := unix.FcntlInt(uintptr(parent.fd), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("duplicate cgroup v2 parent pin: %w", err)
	}
	closeLeaf = false
	return &LinuxCgroupV2Leaf{parentFD: parentFD, leafFD: leafFD, identity: identity, ownerUID: parent.ownerUID}, nil
}

// Identity returns the immutable parent/name/device/inode identity.
func (leaf *LinuxCgroupV2Leaf) Identity() authority.LinuxCgroupIdentity {
	if leaf == nil {
		return authority.LinuxCgroupIdentity{}
	}
	return leaf.identity
}

// OwnerUID returns the effective UID that opened the delegated parent and
// created or authenticated the transferred leaf pin.
func (leaf *LinuxCgroupV2Leaf) OwnerUID() uint32 {
	if leaf == nil {
		return 0
	}
	return leaf.ownerUID
}

// DuplicateFD returns a close-on-exec duplicate suitable for authenticated
// transfer to another owner. Transfer authentication and continuous pin
// provenance remain the caller's responsibility.
func (leaf *LinuxCgroupV2Leaf) DuplicateFD() (*os.File, error) {
	if leaf == nil {
		return nil, ErrLinuxCgroupV2Unresolved
	}
	leaf.mu.RLock()
	defer leaf.mu.RUnlock()
	if err := leaf.verifyLive(); err != nil {
		return nil, err
	}
	fd, err := unix.FcntlInt(uintptr(leaf.leafFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("duplicate pinned cgroup v2 leaf: %w", err)
	}
	return os.NewFile(uintptr(fd), "symmetry-cgroup-v2-leaf"), nil
}

// BirthFD returns a close-on-exec read-only directory FD for clone3
// CLONE_INTO_CGROUP. It is opened relative to the pinned leaf and rechecked
// against the same parent/name/device/inode identity before return. The caller
// must retain it only through the trusted birth operation and must not pass it
// to native UID B.
func (leaf *LinuxCgroupV2Leaf) BirthFD() (*os.File, error) {
	if leaf == nil {
		return nil, ErrLinuxCgroupV2Unresolved
	}
	leaf.mu.RLock()
	defer leaf.mu.RUnlock()
	if err := leaf.verifyLive(); err != nil {
		return nil, err
	}
	how := &unix.OpenHow{
		Flags:   uint64(unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV),
	}
	fd, err := unix.Openat2(leaf.leafFD, ".", how)
	if err != nil {
		return nil, fmt.Errorf("%w: open clone3 cgroup birth FD: %w", ErrLinuxCgroupV2Unresolved, err)
	}
	stat, err := statLinuxCgroupFD(fd)
	if err != nil || stat.mount != leaf.identity.Parent.MountID || stat.device != leaf.identity.LeafDevice ||
		stat.inode != leaf.identity.LeafInode || stat.uid != leaf.ownerUID || stat.mode&unix.S_IFMT != unix.S_IFDIR {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("%w: clone3 cgroup birth FD differs from reserved leaf", ErrLinuxCgroupV2Unresolved)
	}
	if err := leaf.verifyLive(); err != nil {
		_ = unix.Close(fd)
		return nil, err
	}
	return os.NewFile(uintptr(fd), "symmetry-cgroup-v2-birth"), nil
}

// Close retires both live pins. A closed leaf cannot be reopened from Identity.
func (leaf *LinuxCgroupV2Leaf) Close() error {
	if leaf == nil {
		return nil
	}
	leaf.mu.Lock()
	defer leaf.mu.Unlock()
	var result error
	if leaf.leafFD >= 0 {
		result = unix.Close(leaf.leafFD)
		leaf.leafFD = -1
	}
	if leaf.parentFD >= 0 {
		if err := unix.Close(leaf.parentFD); result == nil {
			result = err
		}
		leaf.parentFD = -1
	}
	return result
}

// VerifyKillControl confirms that the kernel-created leaf cgroup.kill is
// A-owned, owner-writable and not group/world-writable. Mode 0200 is the
// normal delegated leaf mode; owner-read mode 0600 is also accepted. The
// literal control is opened through the retained leaf FD and closed without
// writing.
func (leaf *LinuxCgroupV2Leaf) VerifyKillControl() error {
	if leaf == nil {
		return ErrLinuxCgroupV2Unresolved
	}
	leaf.mu.RLock()
	defer leaf.mu.RUnlock()
	if err := leaf.verifyLive(); err != nil {
		return err
	}
	fd, err := openLinuxCgroupControlAt(leaf.leafFD, "cgroup.kill", unix.O_WRONLY)
	if err != nil {
		return fmt.Errorf("%w: open leaf cgroup.kill as A: %v", ErrLinuxCgroupV2NotDelegated, err)
	}
	defer unix.Close(fd)
	if err := verifyLinuxCgroupKillControl(fd, leaf.identity.Parent.MountID, leaf.identity.Parent.Device, leaf.ownerUID); err != nil {
		return err
	}
	return leaf.verifyLive()
}

// Kill writes cgroup.kill through the exact pinned leaf FD. Errors returned
// before the write syscall carry errLinuxCgroupKillWriteNotAttempted; any
// attempted write with an error remains an unknown result, never stop proof.
func (leaf *LinuxCgroupV2Leaf) Kill(ctx context.Context) error {
	if ctx == nil {
		return errors.Join(errLinuxCgroupKillWriteNotAttempted, errors.New("Linux cgroup v2 kill requires a context"))
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(errLinuxCgroupKillWriteNotAttempted, err)
	}
	if leaf == nil {
		return errors.Join(errLinuxCgroupKillWriteNotAttempted, ErrLinuxCgroupV2Unresolved)
	}
	leaf.mu.RLock()
	defer leaf.mu.RUnlock()
	if err := leaf.verifyLive(); err != nil {
		return errors.Join(errLinuxCgroupKillWriteNotAttempted, err)
	}
	fd, err := openLinuxCgroupControlAt(leaf.leafFD, "cgroup.kill", unix.O_WRONLY)
	if err != nil {
		return errors.Join(errLinuxCgroupKillWriteNotAttempted, fmt.Errorf("%w: open cgroup.kill: %w", ErrLinuxCgroupV2Unresolved, err))
	}
	defer unix.Close(fd)
	if err := verifyLinuxCgroupKillControl(fd, leaf.identity.Parent.MountID, leaf.identity.Parent.Device, leaf.ownerUID); err != nil {
		return errors.Join(errLinuxCgroupKillWriteNotAttempted, err)
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(errLinuxCgroupKillWriteNotAttempted, err)
	}
	if err := writeLinuxCgroupKill(fd); err != nil {
		return err
	}
	if err := leaf.verifyLive(); err != nil {
		return err
	}
	return nil
}

// writeLinuxCgroupKill returns an unclassified unresolved error once the
// syscall was attempted. Even EBADF/EINTR/short-write is not replayable as a
// no-write result because the kernel may have consumed part or all of it.
func writeLinuxCgroupKill(fd int) error {
	written, err := unix.Write(fd, []byte("1\n"))
	if err != nil {
		return fmt.Errorf("%w: cgroup.kill write result %d: %w", ErrLinuxCgroupV2Unresolved, written, err)
	}
	if written != 2 {
		return fmt.Errorf("%w: cgroup.kill write result %d: %w", ErrLinuxCgroupV2Unresolved, written, io.ErrShortWrite)
	}
	return nil
}

// WaitEmpty requires two separated recursive cgroup.events observations of
// populated=0 while the same leaf remains pinned at its reserved parent. This
// proves only an empty observation; it does not prove physical stop, producer
// fencing, identity-bound waits/reaps, or authorize journal receipt creation.
func (leaf *LinuxCgroupV2Leaf) WaitEmpty(ctx context.Context) error {
	if ctx == nil {
		return errors.New("Linux cgroup v2 empty check requires a context")
	}
	if leaf == nil {
		return ErrLinuxCgroupV2Unresolved
	}
	leaf.mu.RLock()
	defer leaf.mu.RUnlock()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		populated, err := leaf.readPopulated()
		if err != nil {
			return err
		}
		if populated {
			if err := waitLinuxCgroupV2Interval(ctx); err != nil {
				return err
			}
			continue
		}
		if err := waitLinuxCgroupV2Interval(ctx); err != nil {
			return err
		}
		stillPopulated, err := leaf.readPopulated()
		if err != nil {
			return err
		}
		if !stillPopulated {
			return nil
		}
	}
}

// RemoveAfterStopReceipt removes only the exact empty leaf and its nested
// cgroups after validating a caller-supplied durable v2 stop receipt. It
// returns an opaque release capability only after measured removal (or exact
// receipt-backed absent-after-unknown-rmdir readback). It never creates a
// StopReceipt or exposes an unissued release proof.
func (leaf *LinuxCgroupV2Leaf) RemoveAfterStopReceipt(ctx context.Context, authorization LinuxCgroupV2RemovalAuthorization) (LinuxCgroupV2Release, error) {
	if ctx == nil {
		return LinuxCgroupV2Release{}, errors.New("Linux cgroup v2 removal requires a context")
	}
	if leaf == nil {
		return LinuxCgroupV2Release{}, ErrLinuxCgroupV2Unresolved
	}
	authorization = cloneLinuxCgroupV2RemovalAuthorization(authorization)
	disposition, err := validateLinuxCgroupRemovalAuthorization(leaf.identity, leaf.ownerUID, authorization)
	if err != nil {
		return LinuxCgroupV2Release{}, err
	}
	leaf.mu.RLock()
	defer leaf.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	if err := leaf.verifyLive(); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	if err := leaf.waitEmptyLocked(ctx); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	if err := removeLinuxCgroupChildren(ctx, leaf.leafFD, leaf.identity.Parent.MountID, leaf.identity.Parent.Device, leaf.ownerUID); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	if err := leaf.verifyLive(); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	if err := requireLinuxCgroupNameAt(leaf.parentFD, leaf.identity.LeafName, linuxCgroupV2StatForIdentity(leaf.identity, leaf.identity.Parent.MountID)); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	if err := ctx.Err(); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	rmdirErr := unix.Unlinkat(leaf.parentFD, leaf.identity.LeafName, unix.AT_REMOVEDIR)
	if err := ensureLinuxCgroupNameAbsent(leaf.parentFD, leaf.identity.LeafName, leaf.identity.Parent.MountID, leaf.identity.Parent.Device); err != nil {
		if rmdirErr != nil {
			return LinuxCgroupV2Release{}, fmt.Errorf("%w: cgroup subtree removal outcome is unknown: rmdir=%v readback=%v", ErrLinuxCgroupV2Unresolved, rmdirErr, err)
		}
		return LinuxCgroupV2Release{}, err
	}
	if rmdirErr != nil {
		disposition = authority.LinuxCgroupLeafAbsentAfterReceipt
	}
	proof, err := linuxCgroupReleaseProof(authorization, disposition)
	if err != nil {
		return LinuxCgroupV2Release{}, err
	}
	return issueLinuxCgroupV2Release(proof)
}

// RemoveAfterPreGateStopReceipt removes the exact empty run leaf and nested
// cgroups only after an exact durable pre-gate receipt. It independently
// verifies the root launcher's HMAC-authenticated never-cloned outcome against
// the per-run secret, rechecks recursive emptiness and same-object pins, then
// returns a pre-gate-only opaque release token after rmdir/receipt-backed
// absence readback. LauncherGrantPossible remains true; this never invokes the
// no-target abort path.
func (leaf *LinuxCgroupV2Leaf) RemoveAfterPreGateStopReceipt(ctx context.Context, authorization LinuxCgroupV2PreGateRemovalAuthorization) (LinuxCgroupV2Release, error) {
	if ctx == nil {
		return LinuxCgroupV2Release{}, errors.New("Linux cgroup pre-gate removal requires a context")
	}
	if leaf == nil {
		return LinuxCgroupV2Release{}, ErrLinuxCgroupV2Unresolved
	}
	authorization = cloneLinuxCgroupV2PreGateRemovalAuthorization(authorization)
	if err := validateLinuxCgroupV2PreGateRemovalAuthorization(leaf.identity, leaf.ownerUID, authorization); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	leaf.mu.RLock()
	defer leaf.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	if err := leaf.verifyLive(); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	if err := leaf.waitEmptyLocked(ctx); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	if err := removeLinuxCgroupChildren(ctx, leaf.leafFD, leaf.identity.Parent.MountID, leaf.identity.Parent.Device, leaf.ownerUID); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	if err := leaf.verifyLive(); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	if err := requireLinuxCgroupNameAt(leaf.parentFD, leaf.identity.LeafName, linuxCgroupV2StatForIdentity(leaf.identity, leaf.identity.Parent.MountID)); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	if err := ctx.Err(); err != nil {
		return LinuxCgroupV2Release{}, err
	}
	rmdirErr := unix.Unlinkat(leaf.parentFD, leaf.identity.LeafName, unix.AT_REMOVEDIR)
	if err := ensureLinuxCgroupNameAbsent(leaf.parentFD, leaf.identity.LeafName, leaf.identity.Parent.MountID, leaf.identity.Parent.Device); err != nil {
		if rmdirErr != nil {
			return LinuxCgroupV2Release{}, fmt.Errorf("%w: pre-gate leaf rmdir outcome is unknown: rmdir=%v readback=%v", ErrLinuxCgroupV2Unresolved, rmdirErr, err)
		}
		return LinuxCgroupV2Release{}, err
	}
	disposition := authority.LinuxCgroupLeafRemoved
	if rmdirErr != nil {
		disposition = authority.LinuxCgroupLeafAbsentAfterReceipt
	}
	proof, err := linuxCgroupV2PreGateReleaseProof(authorization.Receipt, disposition)
	if err != nil {
		return LinuxCgroupV2Release{}, err
	}
	return issueLinuxCgroupV2PreGateRelease(proof)
}

// RemoveEmptyReservationAfterAbort is the pre-effect counterpart to
// RemoveAfterStopReceipt. It is intentionally unavailable once a launcher
// grant could have been issued. It requires a bound live FD, a started
// reservation, independent creator/producer fencing and exact helper wait
// evidence where applicable. Nested child cgroups are rejected here; their
// removal is reserved for a receipt-authorized stop.
func (leaf *LinuxCgroupV2Leaf) RemoveEmptyReservationAfterAbort(ctx context.Context, authorization LinuxCgroupV2AbortAuthorization) (authority.LinuxCgroupReservationAbortProof, error) {
	if ctx == nil {
		return authority.LinuxCgroupReservationAbortProof{}, errors.New("Linux cgroup v2 abort removal requires a context")
	}
	if authorization.HelperExit != nil {
		proof := *authorization.HelperExit
		authorization.HelperExit = &proof
	}
	if authorization.LauncherExit != nil {
		proof := *authorization.LauncherExit
		authorization.LauncherExit = &proof
	}
	if leaf == nil || authorization.OwnerUID == 0 || authorization.OwnerUID != leaf.ownerUID ||
		authorization.ReservedLeafIdentity.Validate() != nil || !authorization.ReservedLeafIdentity.Equal(leaf.identity) ||
		!authorization.CreateStarted || authorization.LauncherGrantPossible || !authorization.CreatorFenced ||
		!authorization.NoProducerCanAct || authorization.Anchor.Validate() != nil || authorization.Mirror.Validate() != nil ||
		!validLinuxCgroupLeafName(authorization.LaunchToken) ||
		authorization.HelperWasCreated != (authorization.HelperExit != nil) ||
		authorization.LauncherWasCreated != (authorization.LauncherExit != nil) ||
		authorization.LauncherWasCreated && !authorization.HelperWasCreated ||
		authorization.HelperWasCreated && authorization.Helper.Validate() != nil ||
		!authorization.HelperWasCreated && authorization.Helper != (authority.LinuxCgroupProcessIdentity{}) ||
		authorization.LauncherWasCreated && authorization.Launcher.Validate() != nil ||
		!authorization.LauncherWasCreated && authorization.Launcher != (authority.LinuxCgroupProcessIdentity{}) ||
		authorization.Mirror.Equal(authorization.Anchor) || authorization.HelperWasCreated && authorization.Mirror.Equal(authorization.Helper) ||
		authorization.LauncherWasCreated && authorization.Mirror.Equal(authorization.Launcher) ||
		authorization.HelperExit != nil && !authorization.HelperExit.ValidFor(authorization.Helper, authorization.Anchor) ||
		authorization.LauncherExit != nil && !authorization.LauncherExit.ValidForOwnedDescendant(authorization.Launcher, authorization.Helper, authorization.Anchor, authorization.HelperExit) {
		return authority.LinuxCgroupReservationAbortProof{}, errors.New("pre-effect Linux cgroup v2 removal lacks exact creator fencing")
	}
	leaf.mu.RLock()
	defer leaf.mu.RUnlock()
	if err := ctx.Err(); err != nil {
		return authority.LinuxCgroupReservationAbortProof{}, err
	}
	if err := leaf.verifyLive(); err != nil {
		return authority.LinuxCgroupReservationAbortProof{}, err
	}
	if err := leaf.waitEmptyLocked(ctx); err != nil {
		return authority.LinuxCgroupReservationAbortProof{}, err
	}
	hasChildren, err := linuxCgroupHasChildDirectories(leaf.leafFD)
	if err != nil {
		return authority.LinuxCgroupReservationAbortProof{}, fmt.Errorf("%w: inspect pre-effect leaf children: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	if hasChildren {
		return authority.LinuxCgroupReservationAbortProof{}, fmt.Errorf("%w: pre-effect leaf still contains nested cgroups", ErrLinuxCgroupV2Unresolved)
	}
	if err := leaf.verifyLive(); err != nil {
		return authority.LinuxCgroupReservationAbortProof{}, err
	}
	if err := requireLinuxCgroupNameAt(leaf.parentFD, leaf.identity.LeafName, linuxCgroupV2StatForIdentity(leaf.identity, leaf.identity.Parent.MountID)); err != nil {
		return authority.LinuxCgroupReservationAbortProof{}, err
	}
	if err := ctx.Err(); err != nil {
		return authority.LinuxCgroupReservationAbortProof{}, err
	}
	rmdirErr := unix.Unlinkat(leaf.parentFD, leaf.identity.LeafName, unix.AT_REMOVEDIR)
	if err := ensureLinuxCgroupNameAbsent(leaf.parentFD, leaf.identity.LeafName, leaf.identity.Parent.MountID, leaf.identity.Parent.Device); err != nil {
		if rmdirErr != nil {
			return authority.LinuxCgroupReservationAbortProof{}, fmt.Errorf("%w: pre-effect rmdir outcome is unknown: rmdir=%v readback=%v", ErrLinuxCgroupV2Unresolved, rmdirErr, err)
		}
		return authority.LinuxCgroupReservationAbortProof{}, err
	}
	disposition := authority.LinuxCgroupAbortLeafRemovedEmpty
	unknown := rmdirErr != nil
	if unknown {
		disposition = authority.LinuxCgroupAbortLeafAbsentAfterRmdir
	}
	var helper, launcher *authority.LinuxCgroupProcessIdentity
	if authorization.HelperWasCreated {
		copy := authorization.Helper
		helper = &copy
	}
	if authorization.LauncherWasCreated {
		copy := authorization.Launcher
		launcher = &copy
	}
	proof := authority.LinuxCgroupReservationAbortProof{
		Version: authority.LinuxCgroupAbortProofVersion, LaunchToken: authorization.LaunchToken,
		Parent: leaf.identity.Parent, LeafName: leaf.identity.LeafName, CreateStarted: true,
		LauncherGrantPossible: false, LeafIdentity: &leaf.identity, LeafDisposition: disposition,
		OwnerUID: authorization.OwnerUID, Anchor: authorization.Anchor, Mirror: authorization.Mirror,
		Witness: authorization.Anchor,
		Helper:  helper, Launcher: launcher,
		HelperExit: authorization.HelperExit, LauncherExit: authorization.LauncherExit,
		CreatorFenced: true, NoProducerCanAct: true, ParentUnchanged: true, LeafEmpty: true,
		LeafAbsent: true, RmdirSucceeded: rmdirErr == nil, RmdirOutcomeUnknown: unknown,
	}
	if !proof.ValidFor(authorization.LaunchToken, leaf.identity.Parent, leaf.identity.LeafName, true,
		authorization.OwnerUID, authorization.Anchor, authorization.Mirror, helper, launcher) {
		return authority.LinuxCgroupReservationAbortProof{}, errors.New("constructed pre-effect abort proof does not match its reservation")
	}
	return proof, nil
}

func (leaf *LinuxCgroupV2Leaf) waitEmptyLocked(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		populated, err := leaf.readPopulated()
		if err != nil {
			return err
		}
		if populated {
			if err := waitLinuxCgroupV2Interval(ctx); err != nil {
				return err
			}
			continue
		}
		if err := waitLinuxCgroupV2Interval(ctx); err != nil {
			return err
		}
		stillPopulated, err := leaf.readPopulated()
		if err != nil {
			return err
		}
		if !stillPopulated {
			return nil
		}
	}
}

func (leaf *LinuxCgroupV2Leaf) readPopulated() (bool, error) {
	if err := leaf.verifyLive(); err != nil {
		return false, err
	}
	fd, err := openLinuxCgroupControlAt(leaf.leafFD, "cgroup.events", unix.O_RDONLY)
	if err != nil {
		return false, fmt.Errorf("%w: open cgroup.events: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	defer unix.Close(fd)
	if err := verifyLinuxCgroupControlFileOnMount(fd, leaf.identity.Parent.MountID, leaf.identity.Parent.Device); err != nil {
		return false, err
	}
	contents, err := readLinuxCgroupEventsFD(fd)
	if err != nil {
		return false, fmt.Errorf("%w: read cgroup.events: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	populated, err := parseLinuxCgroupPopulated(contents)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	if err := leaf.verifyLive(); err != nil {
		return false, err
	}
	return populated, nil
}

func (leaf *LinuxCgroupV2Leaf) verifyLive() error {
	if leaf.leafFD < 0 || leaf.parentFD < 0 || leaf.identity.Validate() != nil || leaf.ownerUID == 0 || uint32(os.Geteuid()) != leaf.ownerUID {
		return ErrLinuxCgroupV2Unresolved
	}
	if err := requireLinuxCgroup2FD(leaf.parentFD); err != nil {
		return fmt.Errorf("%w: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	if err := requireLinuxCgroup2FD(leaf.leafFD); err != nil {
		return fmt.Errorf("%w: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	parentStat, err := statLinuxCgroupFD(leaf.parentFD)
	if err != nil {
		return fmt.Errorf("%w: stat pinned parent: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	leafStat, err := statLinuxCgroupFD(leaf.leafFD)
	if err != nil {
		return fmt.Errorf("%w: stat pinned leaf: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	if parentStat.mount != leaf.identity.Parent.MountID || parentStat.device != leaf.identity.Parent.Device ||
		parentStat.inode != leaf.identity.Parent.Inode || leafStat.mount != leaf.identity.Parent.MountID ||
		leafStat.device != leaf.identity.LeafDevice || leafStat.inode != leaf.identity.LeafInode {
		return fmt.Errorf("%w: live cgroup v2 FD identity changed", ErrLinuxCgroupV2Unresolved)
	}
	return requireLinuxCgroupNameAt(leaf.parentFD, leaf.identity.LeafName, leafStat)
}

func (parent *LinuxCgroupV2Parent) verifyPinnedParent() error {
	if parent.fd < 0 || parent.identity.Validate() != nil || parent.ownerUID == 0 || uint32(os.Geteuid()) != parent.ownerUID {
		return ErrLinuxCgroupV2Unresolved
	}
	if err := requireLinuxCgroup2FD(parent.fd); err != nil {
		return fmt.Errorf("%w: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	stat, err := statLinuxCgroupFD(parent.fd)
	if err != nil || stat.mount != parent.identity.MountID || stat.device != parent.identity.Device || stat.inode != parent.identity.Inode {
		return fmt.Errorf("%w: pinned delegated parent changed", ErrLinuxCgroupV2Unresolved)
	}
	return nil
}

func openLinuxCgroupPath(dirfd int, name string, beneath bool) (int, error) {
	resolve := uint64(unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS)
	if beneath {
		resolve |= unix.RESOLVE_BENEATH | unix.RESOLVE_NO_XDEV
	}
	openName := name
	if openName == "" {
		openName = "."
	}
	how := &unix.OpenHow{Flags: uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC), Resolve: resolve}
	return unix.Openat2(dirfd, openName, how)
}

func openLinuxCgroupControlAt(dirfd int, name string, flags int) (int, error) {
	openFlags := flags | unix.O_CLOEXEC | unix.O_NOFOLLOW
	if flags&unix.O_PATH == 0 {
		openFlags |= unix.O_NONBLOCK
	}
	how := &unix.OpenHow{
		Flags:   uint64(openFlags),
		Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV),
	}
	return unix.Openat2(dirfd, name, how)
}

func requireLinuxCgroup2FD(fd int) error {
	var fs unix.Statfs_t
	if err := unix.Fstatfs(fd, &fs); err != nil {
		return fmt.Errorf("stat cgroup filesystem: %w", err)
	}
	if uint64(fs.Type) != uint64(unix.CGROUP2_SUPER_MAGIC) {
		return fmt.Errorf("%w: filesystem magic %#x", ErrLinuxCgroupV2NotCgroup2, uint64(fs.Type))
	}
	return nil
}

func statLinuxCgroupFD(fd int) (linuxCgroupV2Stat, error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return linuxCgroupV2Stat{}, err
	}
	var sx unix.Statx_t
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH|unix.AT_SYMLINK_NOFOLLOW, unix.STATX_BASIC_STATS|unix.STATX_MNT_ID, &sx); err != nil {
		return linuxCgroupV2Stat{}, err
	}
	if sx.Mask&unix.STATX_MNT_ID == 0 {
		return linuxCgroupV2Stat{}, errors.New("kernel did not return a stable mount ID")
	}
	return linuxCgroupV2Stat{device: uint64(st.Dev), inode: st.Ino, mount: sx.Mnt_id, mode: st.Mode, uid: st.Uid}, nil
}

func verifyLinuxCgroupControlFileOnMount(fd int, mountID, device uint64) error {
	if err := requireLinuxCgroup2FD(fd); err != nil {
		return fmt.Errorf("%w: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	stat, err := statLinuxCgroupFD(fd)
	if err != nil || stat.mount != mountID || stat.device != device || stat.mode&unix.S_IFMT != unix.S_IFREG {
		return fmt.Errorf("%w: cgroup control file identity or type is invalid", ErrLinuxCgroupV2Unresolved)
	}
	return nil
}

func verifyLinuxCgroupControlFile(fd int, mountID, device uint64, ownerUID uint32) error {
	if err := verifyLinuxCgroupControlFileOnMount(fd, mountID, device); err != nil {
		return err
	}
	stat, err := statLinuxCgroupFD(fd)
	if err != nil || stat.uid != ownerUID {
		return fmt.Errorf("%w: cgroup control file is not owned by effective UID %d", ErrLinuxCgroupV2Unresolved, ownerUID)
	}
	return nil
}

func verifyLinuxCgroupKillControl(fd int, mountID, device uint64, ownerUID uint32) error {
	if err := verifyLinuxCgroupControlFile(fd, mountID, device, ownerUID); err != nil {
		return err
	}
	stat, err := statLinuxCgroupFD(fd)
	if err != nil {
		return fmt.Errorf("%w: stat leaf cgroup.kill: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	permissions := stat.mode & 07777
	if permissions != 0200 && permissions != 0600 {
		return fmt.Errorf("%w: leaf cgroup.kill must be owner-writable and not group/world-writable (mode 0200 or 0600)", ErrLinuxCgroupV2NotDelegated)
	}
	return nil
}

func readLinuxCgroupEventsFD(fd int) (string, error) {
	var buffer [linuxCgroupV2EventsMax]byte
	read := 0
	for read < len(buffer) {
		n, err := unix.Read(fd, buffer[read:])
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return "", err
		}
		if n == 0 {
			return string(buffer[:read]), nil
		}
		read += n
	}
	return "", errors.New("cgroup.events exceeds its bounded read")
}

func requireLinuxCgroupNameAt(parentFD int, name string, expected linuxCgroupV2Stat) error {
	var st unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return fmt.Errorf("%w: reserved cgroup path readback: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	var sx unix.Statx_t
	if err := unix.Statx(parentFD, name, unix.AT_SYMLINK_NOFOLLOW, unix.STATX_BASIC_STATS|unix.STATX_MNT_ID, &sx); err != nil {
		return fmt.Errorf("%w: reserved cgroup mount readback: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	if uint64(st.Dev) != expected.device || st.Ino != expected.inode || sx.Mnt_id != expected.mount || st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return fmt.Errorf("%w: reserved cgroup name now identifies another object", ErrLinuxCgroupV2Unresolved)
	}
	return nil
}

func linuxCgroupV2StatForIdentity(identity authority.LinuxCgroupIdentity, mountID uint64) linuxCgroupV2Stat {
	return linuxCgroupV2Stat{device: identity.LeafDevice, inode: identity.LeafInode, mount: mountID, mode: unix.S_IFDIR}
}

func verifyLinuxCgroupV2Controls(parentFD int, euid uint32) error {
	parentStat, err := statLinuxCgroupFD(parentFD)
	if err != nil {
		return fmt.Errorf("stat delegated parent controls: %w", err)
	}
	for _, control := range []struct {
		name         string
		flags        int
		requireOwner bool
		requireWrite bool
	}{
		{name: "cgroup.procs", flags: unix.O_WRONLY, requireOwner: true, requireWrite: true},
		{name: "cgroup.subtree_control", flags: unix.O_WRONLY, requireOwner: true, requireWrite: true},
		{name: "cgroup.kill", flags: unix.O_PATH},
		{name: "cgroup.events", flags: unix.O_RDONLY},
		{name: "cgroup.controllers", flags: unix.O_RDONLY},
	} {
		fd, err := openLinuxCgroupControlAt(parentFD, control.name, control.flags)
		if err != nil {
			if control.requireOwner {
				return fmt.Errorf("%w: open %s: %v", ErrLinuxCgroupV2NotDelegated, control.name, err)
			}
			return fmt.Errorf("%w: required control %s is unavailable: %v", ErrLinuxCgroupV2NotCgroup2, control.name, err)
		}
		stat, statErr := statLinuxCgroupFD(fd)
		closeErr := unix.Close(fd)
		if statErr != nil || closeErr != nil || stat.mount != parentStat.mount || stat.device != parentStat.device || stat.mode&unix.S_IFMT != unix.S_IFREG {
			return fmt.Errorf("%w: control %s is not a regular file on the pinned cgroup2 mount", ErrLinuxCgroupV2Unresolved, control.name)
		}
		if control.requireOwner && stat.uid != euid || control.requireWrite && stat.mode&0200 == 0 {
			return fmt.Errorf("%w: control %s is not owned/writable by effective UID %d", ErrLinuxCgroupV2NotDelegated, control.name, euid)
		}
	}
	return nil
}

func readLinuxCgroupV2MountInfo() ([]linuxCgroupV2Mount, error) {
	file, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return parseLinuxCgroupV2MountInfo(file)
}

func parseLinuxCgroupV2MountInfo(reader io.Reader) ([]linuxCgroupV2Mount, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var mounts []linuxCgroupV2Mount
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		separator := -1
		for i, field := range fields {
			if field == "-" {
				separator = i
				break
			}
		}
		if separator < 6 || separator+3 >= len(fields) {
			return nil, errors.New("malformed /proc/self/mountinfo entry")
		}
		if fields[separator+1] != "cgroup2" {
			continue
		}
		id, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil || id == 0 {
			return nil, errors.New("malformed cgroup2 mount ID")
		}
		root, err := unescapeLinuxMountInfoPath(fields[3])
		if err != nil || !canonicalCgroupHierarchyPath(root) {
			return nil, errors.New("malformed cgroup2 mount root")
		}
		mountPoint, err := unescapeLinuxMountInfoPath(fields[4])
		if err != nil || !canonicalAbsoluteLinuxPath(mountPoint) {
			return nil, errors.New("malformed cgroup2 mount point")
		}
		mounts = append(mounts, linuxCgroupV2Mount{id: id, root: root, mountPoint: mountPoint})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return mounts, nil
}

func unescapeLinuxMountInfoPath(value string) (string, error) {
	var result strings.Builder
	result.Grow(len(value))
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' {
			result.WriteByte(value[i])
			continue
		}
		if i+3 >= len(value) {
			return "", errors.New("short mountinfo escape")
		}
		switch value[i+1 : i+4] {
		case "040":
			result.WriteByte(' ')
		case "011":
			result.WriteByte('\t')
		case "012":
			result.WriteByte('\n')
		case "134":
			result.WriteByte('\\')
		default:
			return "", errors.New("unknown mountinfo escape")
		}
		i += 3
	}
	return result.String(), nil
}

func readLinuxCgroupBootID() string {
	value, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}
	bootID := strings.TrimSpace(string(value))
	if !isCanonicalLinuxBootID(bootID) {
		return ""
	}
	return bootID
}

func canonicalAbsoluteLinuxPath(value string) bool {
	return value != "" && path.IsAbs(value) && path.Clean(value) == value && !strings.Contains(value, `\`) && !strings.ContainsRune(value, 0)
}

func canonicalDelegatedCgroupPath(value string) bool {
	return value == "" || !path.IsAbs(value) && path.Clean(value) == value && value != "." &&
		!strings.HasPrefix(value, "../") && value != ".." && !strings.Contains(value, `\`) && !strings.ContainsRune(value, 0)
}

func canonicalCgroupHierarchyPath(value string) bool {
	return value == "/" || path.IsAbs(value) && path.Clean(value) == value && !strings.Contains(value, `\`) && !strings.ContainsRune(value, 0)
}

func validLinuxCgroupLeafName(value string) bool {
	if len(value) != 32 {
		return false
	}
	for i := range value {
		if !(value[i] >= '0' && value[i] <= '9' || value[i] >= 'a' && value[i] <= 'f') {
			return false
		}
	}
	return true
}

func parseLinuxCgroupPopulated(contents string) (bool, error) {
	seen := false
	populated := false
	for _, line := range strings.Split(strings.TrimSuffix(contents, "\n"), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return false, errors.New("malformed cgroup.events line")
		}
		if fields[0] != "populated" {
			continue
		}
		if seen || fields[1] != "0" && fields[1] != "1" {
			return false, errors.New("malformed or duplicate cgroup.events populated value")
		}
		seen = true
		populated = fields[1] == "1"
	}
	if !seen {
		return false, errors.New("cgroup.events lacks populated value")
	}
	return populated, nil
}

func waitLinuxCgroupV2Interval(ctx context.Context) error {
	timer := time.NewTimer(10 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func validateLinuxCgroupRemovalAuthorization(identity authority.LinuxCgroupIdentity, ownerUID uint32, authorization LinuxCgroupV2RemovalAuthorization) (string, error) {
	if (authorization.Supervisor == nil) == (authorization.Handoff == nil) {
		return "", errors.New("cgroup removal requires exactly one durable owner record")
	}
	if authorization.Supervisor != nil {
		owner := authorization.Supervisor
		if owner.Validate() != nil || owner.OwnerUID != ownerUID || owner.LauncherUID != 0 || owner.StopReceipt == nil || !owner.StopReceipt.Equal(authorization.Receipt) ||
			!authorization.Receipt.ValidFor(*owner) || owner.CgroupIdentity == nil || !owner.CgroupIdentity.Equal(identity) {
			return "", errors.New("cgroup removal stop receipt does not match committed owner and leaf")
		}
		return authority.LinuxCgroupLeafRemoved, nil
	}
	owner := authorization.Handoff
	if owner.Validate() != nil || owner.OwnerUID != ownerUID || owner.LauncherUID != 0 || owner.StopReceipt == nil || !owner.StopReceipt.Equal(authorization.Receipt) ||
		!authorization.Receipt.ValidForHandoff(*owner) || owner.CgroupIdentity == nil || !owner.CgroupIdentity.Equal(identity) {
		return "", errors.New("cgroup removal stop receipt does not match pending owner and leaf")
	}
	return authority.LinuxCgroupLeafRemoved, nil
}

func cloneLinuxCgroupV2RemovalAuthorization(authorization LinuxCgroupV2RemovalAuthorization) LinuxCgroupV2RemovalAuthorization {
	if authorization.Supervisor != nil {
		owner := authorization.Supervisor.Clone()
		authorization.Supervisor = &owner
	}
	if authorization.Handoff != nil {
		owner := authorization.Handoff.Clone()
		authorization.Handoff = &owner
	}
	authorization.Receipt = cloneLinuxCgroupStopReceipt(authorization.Receipt)
	return authorization
}

func cloneLinuxCgroupV2PreGateRemovalAuthorization(authorization LinuxCgroupV2PreGateRemovalAuthorization) LinuxCgroupV2PreGateRemovalAuthorization {
	authorization.Receipt = authorization.Receipt.Clone()
	return authorization
}

func validateLinuxCgroupV2PreGateRemovalAuthorization(identity authority.LinuxCgroupIdentity, ownerUID uint32, authorization LinuxCgroupV2PreGateRemovalAuthorization) error {
	receipt := authorization.Receipt
	if !authorization.LauncherGrantPossible || receipt.Validate() != nil ||
		!receipt.CgroupIdentity.Equal(identity) || receipt.OwnerUID != ownerUID || receipt.LauncherUID != 0 {
		return errors.New("pre-gate cgroup receipt does not match the exact granted leaf and owner")
	}
	if err := verifyLinuxCgroupGateProductionAuthenticator(authorization.Secret, receipt.Proof.GateOutcome); err != nil {
		return fmt.Errorf("verify authenticated no-gate launcher outcome: %w", err)
	}
	return nil
}

func verifyLinuxCgroupGateProductionAuthenticator(secret string, result authority.LinuxCgroupGateProductionResult) error {
	return linuxgate.VerifyGateNeverProducedAuthenticator(secret, result)
}

func linuxCgroupV2PreGateReleaseProof(receipt authority.LinuxCgroupPreGateStopReceipt, disposition string) (authority.LinuxCgroupPreGateReleaseProof, error) {
	proof := authority.LinuxCgroupPreGateReleaseProof{
		Version: authority.LinuxCgroupIdentityVersion, Receipt: receipt.Clone(), LeafDisposition: disposition,
	}
	if err := proof.Validate(); err != nil {
		return authority.LinuxCgroupPreGateReleaseProof{}, fmt.Errorf("platform constructed invalid pre-gate release proof: %w", err)
	}
	return proof, nil
}

func cloneLinuxCgroupStopReceipt(receipt authority.StopReceipt) authority.StopReceipt {
	if receipt.CgroupIdentity != nil {
		identity := *receipt.CgroupIdentity
		receipt.CgroupIdentity = &identity
	}
	if receipt.LinuxProof != nil {
		proof := *receipt.LinuxProof
		if proof.HelperExit != nil {
			helperExit := *proof.HelperExit
			proof.HelperExit = &helperExit
		}
		receipt.LinuxProof = &proof
	}
	return receipt
}

func linuxCgroupReleaseProof(authorization LinuxCgroupV2RemovalAuthorization, disposition string) (authority.SupervisorHandoffReleaseProof, error) {
	var proof authority.SupervisorHandoffReleaseProof
	var cgroup *authority.LinuxCgroupIdentity
	var launcherExit *authority.LinuxCgroupExitProof
	var mirror *authority.LinuxCgroupProcessIdentity
	var creatorSessionID *uint32
	var ownerUID, launcherUID uint32
	var version int
	var launchToken, ownerKind, ownerContext string
	var targetPID, supervisorPID, anchorPID, launcherPID int
	var targetIdentity, supervisorIdentity, targetBirthKind, anchorIdentity, launcherIdentity, pipeToken, jobID string
	var receipt authority.StopReceipt
	if authorization.Supervisor != nil {
		owner := authorization.Supervisor
		version, launchToken, ownerKind, ownerContext = authority.SupervisorHandoffVersionV2, owner.LaunchToken, owner.OwnerKind, owner.OwnerContext
		targetPID, targetIdentity, ownerUID, launcherUID = owner.TargetPID, owner.TargetIdentity, owner.OwnerUID, owner.LauncherUID
		pipeToken, jobID, supervisorPID, supervisorIdentity = owner.PipeToken, owner.JobID, owner.SupervisorPID, owner.SupervisorIdentity
		targetBirthKind, cgroup = owner.TargetBirthKind, owner.CgroupIdentity
		anchorPID, anchorIdentity, launcherPID, launcherIdentity = owner.AnchorPID, owner.AnchorIdentity, owner.LauncherPID, owner.LauncherIdentity
		launcherExit, mirror, creatorSessionID = owner.LauncherExit, owner.Mirror, owner.CreatorSessionID
		receipt = *owner.StopReceipt
	} else {
		owner := authorization.Handoff
		version, launchToken, ownerKind, ownerContext = authority.SupervisorHandoffVersionV2, owner.LaunchToken, owner.OwnerKind, owner.OwnerContext
		targetPID, targetIdentity, ownerUID, launcherUID = owner.TargetPID, owner.TargetIdentity, owner.OwnerUID, owner.LauncherUID
		pipeToken, jobID, supervisorPID, supervisorIdentity = owner.PipeToken, owner.JobID, owner.SupervisorPID, owner.SupervisorIdentity
		targetBirthKind, cgroup = owner.TargetBirthKind, owner.CgroupIdentity
		anchorPID, anchorIdentity, launcherPID, launcherIdentity = owner.AnchorPID, owner.AnchorIdentity, owner.LauncherPID, owner.LauncherIdentity
		launcherExit, mirror, creatorSessionID = owner.LauncherExit, owner.Mirror, owner.CreatorSessionID
		receipt = *owner.StopReceipt
	}
	if cgroup != nil {
		identity := *cgroup
		cgroup = &identity
	}
	if launcherExit != nil {
		copy := *launcherExit
		launcherExit = &copy
	}
	if mirror != nil {
		copy := *mirror
		mirror = &copy
	}
	if creatorSessionID != nil {
		copy := *creatorSessionID
		creatorSessionID = &copy
	}
	receipt = cloneLinuxCgroupStopReceipt(receipt)
	proof = authority.SupervisorHandoffReleaseProof{
		Version: version, Status: "released", LaunchToken: launchToken, OwnerKind: ownerKind, OwnerContext: ownerContext,
		TargetPID: targetPID, TargetIdentity: targetIdentity, OwnerUID: ownerUID, LauncherUID: launcherUID,
		PipeToken: pipeToken, JobID: jobID, SupervisorPID: supervisorPID, SupervisorIdentity: supervisorIdentity,
		TargetBirthKind: targetBirthKind, CgroupIdentity: cgroup, AnchorPID: anchorPID, AnchorIdentity: anchorIdentity,
		LauncherPID: launcherPID, LauncherIdentity: launcherIdentity, LauncherExit: launcherExit, Mirror: mirror,
		LeafDisposition: disposition, StopReceipt: &receipt, CreatorSessionID: creatorSessionID,
	}
	if err := proof.Validate(); err != nil {
		return authority.SupervisorHandoffReleaseProof{}, fmt.Errorf("platform constructed invalid v2 release proof: %w", err)
	}
	if authorization.Supervisor != nil {
		if !proof.ValidForSupervisor(*authorization.Supervisor) {
			return authority.SupervisorHandoffReleaseProof{}, errors.New("constructed cgroup release proof does not match owner")
		}
	} else if !proof.ValidFor(*authorization.Handoff) {
		return authority.SupervisorHandoffReleaseProof{}, errors.New("constructed cgroup release proof does not match handoff")
	}
	return proof, nil
}

func removeLinuxCgroupChildren(ctx context.Context, directoryFD int, mountID, device uint64, ownerUID uint32) error {
	names, err := readLinuxCgroupDirectoryNames(directoryFD)
	if err != nil {
		return fmt.Errorf("%w: enumerate nested cgroups: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		var st unix.Stat_t
		if err := unix.Fstatat(directoryFD, name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return fmt.Errorf("%w: stat nested cgroup %q: %v", ErrLinuxCgroupV2Unresolved, name, err)
		}
		if st.Mode&unix.S_IFMT != unix.S_IFDIR {
			continue
		}
		childFD, err := openLinuxCgroupPath(directoryFD, name, true)
		if err != nil {
			return fmt.Errorf("%w: open nested cgroup %q: %v", ErrLinuxCgroupV2Unresolved, name, err)
		}
		childStat, err := statLinuxCgroupFD(childFD)
		if err != nil || childStat.mount != mountID || childStat.device != device || childStat.uid != ownerUID ||
			childStat.mode&unix.S_IFMT != unix.S_IFDIR || childStat.mode&0300 != 0300 {
			_ = unix.Close(childFD)
			return fmt.Errorf("%w: nested directory %q is not on the pinned cgroup2 mount", ErrLinuxCgroupV2Unresolved, name)
		}
		if err := requireLinuxCgroupNameAt(directoryFD, name, childStat); err != nil {
			_ = unix.Close(childFD)
			return err
		}
		if err := waitLinuxCgroupDirectoryEmpty(ctx, childFD, mountID, device, ownerUID); err != nil {
			_ = unix.Close(childFD)
			return err
		}
		if err := removeLinuxCgroupChildren(ctx, childFD, mountID, device, ownerUID); err != nil {
			_ = unix.Close(childFD)
			return err
		}
		currentChild, err := statLinuxCgroupFD(childFD)
		if err != nil || currentChild.mount != childStat.mount || currentChild.device != childStat.device ||
			currentChild.inode != childStat.inode || currentChild.uid != ownerUID || currentChild.mode&unix.S_IFMT != unix.S_IFDIR ||
			currentChild.mode&0300 != 0300 {
			_ = unix.Close(childFD)
			return fmt.Errorf("%w: nested cgroup %q changed while pinned", ErrLinuxCgroupV2Unresolved, name)
		}
		if err := requireLinuxCgroupNameAt(directoryFD, name, childStat); err != nil {
			_ = unix.Close(childFD)
			return err
		}
		if err := ctx.Err(); err != nil {
			_ = unix.Close(childFD)
			return err
		}
		removeErr := unix.Unlinkat(directoryFD, name, unix.AT_REMOVEDIR)
		readbackErr := ensureLinuxCgroupNameAbsent(directoryFD, name, mountID, device)
		_ = unix.Close(childFD)
		if readbackErr != nil {
			return fmt.Errorf("%w: nested cgroup removal result=%v readback=%v", ErrLinuxCgroupV2Unresolved, removeErr, readbackErr)
		}
	}
	return nil
}

func waitLinuxCgroupDirectoryEmpty(ctx context.Context, fd int, mountID, device uint64, ownerUID uint32) error {
	for {
		populated, err := readLinuxCgroupDirectoryPopulated(fd, mountID, device, ownerUID)
		if err != nil {
			return err
		}
		if populated {
			if err := waitLinuxCgroupV2Interval(ctx); err != nil {
				return err
			}
			continue
		}
		if err := waitLinuxCgroupV2Interval(ctx); err != nil {
			return err
		}
		stillPopulated, err := readLinuxCgroupDirectoryPopulated(fd, mountID, device, ownerUID)
		if err != nil {
			return err
		}
		if !stillPopulated {
			return nil
		}
	}
}

func readLinuxCgroupDirectoryPopulated(directoryFD int, mountID, device uint64, ownerUID uint32) (bool, error) {
	if err := requireLinuxCgroup2FD(directoryFD); err != nil {
		return false, fmt.Errorf("%w: nested cgroup filesystem changed: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	stat, err := statLinuxCgroupFD(directoryFD)
	if err != nil || stat.mount != mountID || stat.device != device || stat.uid != ownerUID ||
		stat.mode&unix.S_IFMT != unix.S_IFDIR || stat.mode&0300 != 0300 {
		return false, fmt.Errorf("%w: nested cgroup identity changed", ErrLinuxCgroupV2Unresolved)
	}
	fd, err := openLinuxCgroupControlAt(directoryFD, "cgroup.events", unix.O_RDONLY)
	if err != nil {
		return false, fmt.Errorf("%w: open nested cgroup.events: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	defer unix.Close(fd)
	if err := verifyLinuxCgroupControlFileOnMount(fd, mountID, device); err != nil {
		return false, err
	}
	contents, err := readLinuxCgroupEventsFD(fd)
	if err != nil {
		return false, fmt.Errorf("%w: read nested cgroup.events: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	populated, err := parseLinuxCgroupPopulated(contents)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	return populated, nil
}

func readLinuxCgroupDirectoryNames(directoryFD int) ([]string, error) {
	fd, err := unix.Openat(directoryFD, ".", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(fd)
	buffer := make([]byte, 8192)
	var names []string
	for {
		n, err := unix.ReadDirent(fd, buffer)
		if err != nil {
			return nil, err
		}
		if n == 0 {
			return names, nil
		}
		remaining := buffer[:n]
		for len(remaining) != 0 {
			consumed, _, parsed := unix.ParseDirent(remaining, -1, names)
			if consumed <= 0 {
				return nil, errors.New("invalid getdents result for cgroup directory")
			}
			names = parsed
			remaining = remaining[consumed:]
		}
	}
}

func linuxCgroupHasChildDirectories(directoryFD int) (bool, error) {
	names, err := readLinuxCgroupDirectoryNames(directoryFD)
	if err != nil {
		return false, err
	}
	for _, name := range names {
		var stat unix.Stat_t
		if err := unix.Fstatat(directoryFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return false, err
		}
		if stat.Mode&unix.S_IFMT == unix.S_IFDIR {
			return true, nil
		}
	}
	return false, nil
}

func ensureLinuxCgroupNameAbsent(parentFD int, name string, mountID, device uint64) error {
	var st unix.Stat_t
	err := unix.Fstatat(parentFD, name, &st, unix.AT_SYMLINK_NOFOLLOW)
	if errors.Is(err, unix.ENOENT) {
		parentStat, parentErr := statLinuxCgroupFD(parentFD)
		if parentErr != nil || parentStat.mount != mountID || parentStat.device != device {
			return fmt.Errorf("%w: parent identity changed during absence readback", ErrLinuxCgroupV2Unresolved)
		}
		var repeated unix.Stat_t
		if repeatedErr := unix.Fstatat(parentFD, name, &repeated, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(repeatedErr, unix.ENOENT) {
			return fmt.Errorf("%w: cgroup name %q reappeared during absence readback: %v", ErrLinuxCgroupV2Unresolved, name, repeatedErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w: cgroup absence readback failed: %v", ErrLinuxCgroupV2Unresolved, err)
	}
	return fmt.Errorf("%w: cgroup name %q remains or was replaced after rmdir", ErrLinuxCgroupV2Unresolved, name)
}
