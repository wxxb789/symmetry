//go:build linux && amd64

// symmetry-linux-launcher-core is the static, root-owned stage entered only
// through the small setuid UID/FD/environment sanitizer in wrapper.c. It is
// intentionally transient and has no daemon or journal authority.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/wxxb789/symmetry/daemon/internal/authority"
	"github.com/wxxb789/symmetry/daemon/native/linux-gate/protocol"
	"golang.org/x/sys/unix"
)

const (
	controlFD            = 3
	leafFD               = 4
	releaseFD            = 5
	execStatusFD         = 6
	cleanupRootFD        = 4
	cleanupTargetFD      = 5
	gateImagePath        = "/usr/libexec/symmetry/symmetry-linux-gate"
	ownerConfigPath      = "/etc/symmetry/linux-owner.conf"
	cleanupWorkTimeout   = 5 * time.Minute
	cleanupMaxEntries    = 250_000
	cleanupMaxDepth      = 128
	gateReadyTimeout     = 10 * time.Second
	gateKillTimeout      = 3 * time.Second
	launchRequestTimeout = 30 * time.Second
)

// cloneCleanupLockHold must remain reachable until this one-shot root process
// exits. Releasing the flock when runCloneCleanup returns would allow a retry
// in the interval before the exact child is dead and pidfd-reaped by A.
var cloneCleanupLockHold *os.File

func main() {
	if err := run(); err != nil {
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) == 2 && os.Args[1] == "-clone-cleanup" {
		return runCloneCleanup()
	}
	if len(os.Args) != 1 {
		return errors.New("transient launcher accepts no arbitrary arguments")
	}
	return runLaunch()
}

func runLaunch() error {
	config, err := protocol.ReadInstalledOwnerConfig()
	if err != nil {
		return err
	}
	ruid, euid, suid := unix.Getresuid()
	rgid, egid, sgid := unix.Getresgid()
	if ruid != int(config.AUID) || euid != 0 || suid != 0 || rgid != int(config.AGID) || egid != int(config.AGID) || sgid != int(config.AGID) {
		return errors.New("transient launcher core is not the configured root/A caller")
	}
	controlType, err := unix.GetsockoptInt(controlFD, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || controlType != unix.SOCK_SEQPACKET {
		return fmt.Errorf("authenticate inherited A control socket type: type=%d err=%v", controlType, err)
	}
	peer, err := unix.GetsockoptUcred(controlFD, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer.Pid != int32(os.Getppid()) || peer.Uid != config.AUID || peer.Gid != config.AGID {
		return fmt.Errorf("authenticate inherited A control peer: %w", errorOr(err, errors.New("SO_PEERCRED does not match direct A parent")))
	}
	reportReadyFailure := func(stage protocol.LauncherReadyFailureStage, cause error) error {
		sendErr := sendFrame(controlFD, protocol.EncodeLauncherReadyFailure(stage))
		return errors.Join(fmt.Errorf("root launcher READY stage %s: %w", stage.String(), cause), sendErr)
	}
	if wd, err := os.Getwd(); err != nil || wd != "/" {
		return reportReadyFailure(protocol.LauncherReadyFailureEnvironment,
			fmt.Errorf("transient launcher working directory is not fixed root: %q (%v)", wd, err))
	}
	if groups, err := unix.Getgroups(); err != nil || len(groups) != 0 {
		return reportReadyFailure(protocol.LauncherReadyFailureGroups,
			fmt.Errorf("transient root phase did not clear supplementary groups: %v groups=%v", err, groups))
	}
	// Go appends GOTRACEBACK=none when the root/A image starts with AT_SECURE.
	// The C wrapper still passes only PATH and LANG to execveat.
	if env := os.Environ(); (len(env) != 2 && (len(env) != 3 || env[2] != "GOTRACEBACK=none")) ||
		env[0] != "PATH=/usr/bin:/bin" || env[1] != "LANG=C" {
		return reportReadyFailure(protocol.LauncherReadyFailureEnvironment, errors.New("transient launcher environment is not the fixed minimal environment"))
	}
	if err := setParentDeathSignal(os.Getppid()); err != nil {
		return reportReadyFailure(protocol.LauncherReadyFailureParentFence, err)
	}
	if err := verifyInheritedDescriptors(config); err != nil {
		return reportReadyFailure(protocol.LauncherReadyFailureDescriptors, err)
	}
	if err := verifySocketPeer(releaseFD, peer); err != nil {
		return reportReadyFailure(protocol.LauncherReadyFailurePeer, fmt.Errorf("authenticate inherited release endpoint: %w", err))
	}
	if err := verifySocketPeer(execStatusFD, peer); err != nil {
		return reportReadyFailure(protocol.LauncherReadyFailurePeer, fmt.Errorf("authenticate inherited exec-status endpoint: %w", err))
	}
	if err := verifyCgroupLeaf(leafFD, config.AUID); err != nil {
		return reportReadyFailure(protocol.LauncherReadyFailureLeaf, err)
	}
	launcherIdentity, err := protocol.CaptureLauncherIdentity(os.Getpid())
	if err != nil {
		return reportReadyFailure(protocol.LauncherReadyFailureIdentity,
			fmt.Errorf("capture exact root launcher identity before readiness: %w", err))
	}
	ready, err := protocol.EncodeLauncherReady(launcherIdentity)
	if err != nil {
		return reportReadyFailure(protocol.LauncherReadyFailureEncoding,
			fmt.Errorf("encode exact root launcher readiness: %w", err))
	}
	if err := sendFrame(controlFD, ready); err != nil {
		return fmt.Errorf("send group-cleared root launcher readiness to A: %w", err)
	}

	// Do not enter launchAChildGate until A sends its one launch request. A's
	// helper sends that request only after its durable BindLauncher/ArmGrant hook.
	requestPacket, fds, err := receiveControlRequest(controlFD, time.Now().Add(launchRequestTimeout))
	if err != nil {
		return err
	}
	if len(fds) != 0 {
		closeFDs(fds)
		return errors.New("A launch request carried unexpected ancillary descriptors")
	}
	launchRequest, err := protocol.DecodeLaunchRequest(requestPacket)
	clear(requestPacket)
	if err != nil {
		_ = sendResult(controlFD, protocol.LaunchResult{Status: protocol.LaunchStatusRejected, Errno: errnoNumber(err)}, nil)
		return err
	}
	defer clear(launchRequest.Secret[:])
	return launchAChildGate(controlFD, leafFD, releaseFD, execStatusFD, peer, config, launchRequest)
}

// runCloneCleanup is the sole root filesystem effect. It accepts only the
// direct configured A peer, fixed inherited O_PATH descriptors and the exact
// versioned authority request; it has no B path, argv, shell, Git or daemon
// service mode. The caller owns the Store Begin CAS; this root core validates
// the immutable readback and returns no authority until its target rewalks.
func runCloneCleanup() error {
	config, err := protocol.ReadInstalledOwnerConfig()
	if err != nil {
		return err
	}
	if config.StateDir == "" {
		return errors.New("root clone cleanup requires a versioned installed StateDir")
	}
	ruid, euid, suid := unix.Getresuid()
	rgid, egid, sgid := unix.Getresgid()
	if ruid != int(config.AUID) || euid != 0 || suid != 0 || rgid != int(config.AGID) || egid != int(config.AGID) || sgid != int(config.AGID) {
		return errors.New("root cleanup core is not the configured A caller")
	}
	controlType, err := unix.GetsockoptInt(controlFD, unix.SOL_SOCKET, unix.SO_TYPE)
	if err != nil || controlType != unix.SOCK_SEQPACKET {
		return fmt.Errorf("authenticate inherited cleanup channel type: type=%d err=%v", controlType, err)
	}
	peer, err := unix.GetsockoptUcred(controlFD, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil || peer.Pid != int32(os.Getppid()) || peer.Uid != config.AUID || peer.Gid != config.AGID {
		return fmt.Errorf("authenticate direct A cleanup peer: %w", errorOr(err, errors.New("SO_PEERCRED does not match A parent")))
	}
	if cwd, err := os.Getwd(); err != nil || cwd != "/" {
		return fmt.Errorf("root cleanup cwd is not fixed root: %q err=%v", cwd, err)
	}
	if env := os.Environ(); (len(env) != 2 && (len(env) != 3 || env[2] != "GOTRACEBACK=none")) ||
		env[0] != "PATH=/usr/bin:/bin" || env[1] != "LANG=C" {
		return errors.New("root cleanup environment is not the fixed minimal environment")
	}
	if groups, err := unix.Getgroups(); err != nil || len(groups) != 0 {
		return fmt.Errorf("root cleanup process has supplementary groups: groups=%v err=%v", groups, err)
	}
	if err := setParentDeathSignal(int(peer.Pid)); err != nil {
		return err
	}
	rootIdentity, err := verifyCleanupDirectoryFD(cleanupRootFD, config)
	if err != nil {
		return fmt.Errorf("verify inherited B root O_PATH FD: %w", err)
	}
	targetFDPresent, targetIdentity, err := cleanupTargetDescriptor(cleanupTargetFD, config, rootIdentity)
	if err != nil {
		return err
	}
	workerIdentity, err := protocol.CaptureLauncherIdentity(os.Getpid())
	if err != nil {
		return fmt.Errorf("capture exact root cleanup worker identity: %w", err)
	}
	ready, err := protocol.EncodeLauncherReady(workerIdentity)
	if err != nil {
		return err
	}
	if err := sendFrame(controlFD, ready); err != nil {
		return fmt.Errorf("send sanitized root cleanup READY: %w", err)
	}

	requestPacket, fds, err := receiveFrame(controlFD, 64<<10+1, time.Now().Add(30*time.Second))
	if err != nil {
		return fmt.Errorf("receive one authenticated cleanup request: %w", err)
	}
	defer clear(requestPacket)
	if len(fds) != 0 {
		closeFDs(fds)
		return errors.New("cleanup request carried unexpected ancillary descriptors")
	}
	request, err := protocol.DecodeCloneCleanupRequest(requestPacket)
	if err != nil {
		return err
	}
	anchor := authority.LinuxCgroupProcessIdentity{
		PID: request.Cleanup.NormalReleaseProof.AnchorPID, Identity: request.Cleanup.NormalReleaseProof.AnchorIdentity,
	}
	peerIdentity, err := protocol.CaptureLauncherIdentity(int(peer.Pid))
	if err != nil || int(peer.Pid) != anchor.PID || peerIdentity.Identity != anchor.Identity {
		return fmt.Errorf("cleanup peer is not the exact A anchor bound to normal StopReceipt: peer=%d identity=%q err=%v", peer.Pid, peerIdentity.Identity, err)
	}
	if err := verifyCloneCleanupRequest(request, config, rootIdentity, targetFDPresent, targetIdentity); err != nil {
		return err
	}
	lock, err := acquireCloneCleanupLock()
	if err != nil {
		return fmt.Errorf("serialize transient root clone cleanup: %w", err)
	}
	cloneCleanupLockHold = lock
	workContext, cancel := context.WithTimeout(context.Background(), cleanupWorkTimeout)
	defer cancel()
	result, err := removeCloneTargetTree(workContext, cleanupRootFD, cleanupTargetFD, targetFDPresent,
		request.Cleanup, rootIdentity, targetIdentity, workerIdentity, config)
	if err != nil {
		return err
	}
	result.Nonce = request.Nonce
	packet, err := protocol.EncodeCloneCleanupResult(result)
	if err != nil {
		return fmt.Errorf("encode exact root clone cleanup result: %w", err)
	}
	defer clear(packet)
	if err := sendFrame(controlFD, packet); err != nil {
		return fmt.Errorf("send exact root clone cleanup result: %w", err)
	}
	return nil
}

type cleanupDirectoryIdentity struct {
	Device uint64
	Inode  uint64
}

func verifyCleanupDirectoryFD(fd int, config protocol.OwnerConfig) (cleanupDirectoryIdentity, error) {
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_PATH == 0 {
		return cleanupDirectoryIdentity{}, fmt.Errorf("FD %d is not O_PATH: flags=%#x err=%v", fd, flags, err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return cleanupDirectoryIdentity{}, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != config.BUID || stat.Gid != config.BGID ||
		stat.Mode&07000 != 0 || stat.Mode&0022 != 0 {
		return cleanupDirectoryIdentity{}, errors.New("inherited B cleanup directory has unsafe type, owner, group, or writable mode")
	}
	return cleanupDirectoryIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}

func cleanupTargetDescriptor(fd int, config protocol.OwnerConfig, root cleanupDirectoryIdentity) (bool, cleanupDirectoryIdentity, error) {
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFL, 0)
	if err != nil || flags&unix.O_PATH == 0 {
		return false, cleanupDirectoryIdentity{}, fmt.Errorf("cleanup FD 5 is not O_PATH: flags=%#x err=%v", flags, err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return false, cleanupDirectoryIdentity{}, fmt.Errorf("stat optional B target descriptor: %w", err)
	}
	if stat.Mode&unix.S_IFMT == unix.S_IFCHR && stat.Uid == 0 && stat.Gid == 0 &&
		unix.Major(uint64(stat.Rdev)) == 1 && unix.Minor(uint64(stat.Rdev)) == 3 {
		return false, cleanupDirectoryIdentity{}, nil
	}
	identity, err := verifyCleanupDirectoryFD(fd, config)
	if err != nil {
		return false, cleanupDirectoryIdentity{}, fmt.Errorf("verify inherited B target O_PATH FD: %w", err)
	}
	if identity.Device != root.Device {
		return false, cleanupDirectoryIdentity{}, errors.New("B target O_PATH FD crosses the pinned root filesystem")
	}
	return true, identity, nil
}

func verifyCloneCleanupRequest(request protocol.CloneCleanupWireRequest, config protocol.OwnerConfig, root cleanupDirectoryIdentity,
	targetFDPresent bool, target cleanupDirectoryIdentity) error {
	if request.Cleanup.Validate() != nil || request.Grant.Validate() != nil ||
		!request.Grant.Intent.Equal(request.Cleanup.Intent) || !request.Grant.Seed.Equal(request.Cleanup.SeedReceipt) {
		return errors.New("root cleanup request does not match the exact grant and cleanup intent")
	}
	derivedRoot, err := authority.LinuxCloneWorkspaceRoot(request.Grant.Intent)
	if err != nil || derivedRoot != request.Cleanup.RootPath {
		return fmt.Errorf("cleanup RootPath differs from the immutable target layout: got=%q want=%q err=%v", request.Cleanup.RootPath, derivedRoot, err)
	}
	actualRoot, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", cleanupRootFD))
	if err != nil || actualRoot != request.Cleanup.RootPath {
		return fmt.Errorf("inherited root FD differs from Store cleanup RootPath: got=%q want=%q err=%v", actualRoot, request.Cleanup.RootPath, err)
	}
	relative, err := protocol.CloneCleanupTargetRelativePath(request.Grant.Intent)
	if err != nil {
		return err
	}
	if filepath.Join(request.Cleanup.RootPath, relative) != request.Grant.Intent.Target {
		return errors.New("root cleanup target is not the deterministic RunKey-derived child")
	}
	var rootStat unix.Stat_t
	if err := unix.Fstat(cleanupRootFD, &rootStat); err != nil || uint64(rootStat.Dev) != root.Device || rootStat.Ino != root.Inode {
		return fmt.Errorf("inherited B root identity changed: err=%v", err)
	}
	if request.Cleanup.CheckoutReceipt.TargetDevice != root.Device {
		return errors.New("checkout target device differs from its pinned B root filesystem")
	}
	if err := verifyCleanupRootNamed(request.Cleanup.RootPath, cleanupRootFD, root, config.AUID, config.BUID, config.BGID); err != nil {
		return fmt.Errorf("verify installed B root path and ancestors: %w", err)
	}
	if targetFDPresent {
		if request.PriorRemovalProof != nil {
			return errors.New("fresh target removal cannot carry a prior removal proof")
		}
		if target.Device != request.Cleanup.CheckoutReceipt.TargetDevice || target.Inode != request.Cleanup.CheckoutReceipt.TargetInode {
			return errors.New("inherited target FD differs from the durable checkout inode")
		}
		actualTarget, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", cleanupTargetFD))
		if err != nil || actualTarget != request.Grant.Intent.Target {
			return fmt.Errorf("inherited target FD path differs from the exact grant: got=%q want=%q err=%v", actualTarget, request.Grant.Intent.Target, err)
		}
	} else if target.Inode != 0 || target.Device != 0 {
		return errors.New("absent-target cleanup descriptor has unexpected identity")
	} else {
		prior := request.PriorRemovalProof
		if prior == nil || prior.Validate() != nil || !prior.Intent.Equal(request.Cleanup) ||
			prior.Disposition != authority.LinuxCloneTargetDispositionRemoved || !prior.TargetNlinkZero ||
			prior.RootDevice != root.Device || prior.RootInode != root.Inode ||
			prior.TargetDevice != request.Cleanup.CheckoutReceipt.TargetDevice ||
			prior.TargetInode != request.Cleanup.CheckoutReceipt.TargetInode {
			return errors.New("absent-target replay lacks the exact durable root removal proof for this checkout")
		}
	}
	return nil
}

// acquireCloneCleanupLock serializes the privileged effect across unknown
// response/replay windows by locking the already-installed immutable owner
// config inode. It creates no persistent root runtime files and is released by
// the kernel when this short-lived worker exits.
func acquireCloneCleanupLock() (*os.File, error) {
	for _, path := range []string{"/etc", "/etc/symmetry"} {
		fd, err := unix.Open(path, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if err != nil {
			return nil, fmt.Errorf("open fixed owner config directory %q for cleanup lock: %w", path, err)
		}
		var directoryStat unix.Stat_t
		statErr := unix.Fstat(fd, &directoryStat)
		closeErr := unix.Close(fd)
		if statErr != nil || closeErr != nil || directoryStat.Mode&unix.S_IFMT != unix.S_IFDIR ||
			directoryStat.Uid != 0 || directoryStat.Gid != 0 || directoryStat.Mode&0022 != 0 {
			return nil, fmt.Errorf("fixed owner config directory %q is unsafe: %w", path,
				errorOr(errors.Join(statErr, closeErr), errors.New("owner/mode mismatch")))
		}
	}
	lockFD, err := unix.Open(ownerConfigPath, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open immutable owner config inode for cleanup lock: %w", err)
	}
	var lockStat unix.Stat_t
	if err := unix.Fstat(lockFD, &lockStat); err != nil || lockStat.Mode&unix.S_IFMT != unix.S_IFREG ||
		lockStat.Uid != 0 || lockStat.Gid != 0 || lockStat.Mode&0022 != 0 || lockStat.Nlink != 1 ||
		lockStat.Size <= 0 || lockStat.Size > protocol.OwnerConfigMaxBytes {
		_ = unix.Close(lockFD)
		return nil, fmt.Errorf("immutable owner config lock inode is unsafe: %w", errorOr(err, errors.New("owner/mode/link/size mismatch")))
	}
	if err := unix.Flock(lockFD, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = unix.Close(lockFD)
		return nil, fmt.Errorf("another exact root cleanup worker is still active on the owner config inode: %w", err)
	}
	file := os.NewFile(uintptr(lockFD), "symmetry-linux-owner-config-cleanup-lock")
	if file == nil {
		_ = unix.Close(lockFD)
		return nil, errors.New("wrap immutable owner config cleanup lock")
	}
	return file, nil
}

func removeCloneTargetTree(ctx context.Context, rootFD, targetFD int, targetFDPresent bool,
	cleanup authority.LinuxCloneCleanupIntent, root cleanupDirectoryIdentity, target cleanupDirectoryIdentity,
	worker protocol.LauncherIdentity, config protocol.OwnerConfig) (protocol.CloneCleanupWireResult, error) {
	if ctx == nil || cleanup.Validate() != nil || worker.PID <= 1 || worker.Identity == "" ||
		uint32(os.Geteuid()) != 0 || root.Inode == 0 {
		return protocol.CloneCleanupWireResult{}, errors.New("root clone cleanup operation context or identity is invalid")
	}
	relative, err := protocol.CloneCleanupTargetRelativePath(cleanup.Intent)
	if err != nil {
		return protocol.CloneCleanupWireResult{}, err
	}
	if cleanup.Intent.Target != filepath.Join(cleanup.RootPath, relative) {
		return protocol.CloneCleanupWireResult{}, errors.New("root cleanup target differs from the immutable RunKey layout")
	}
	rootNow, err := fstatCleanupPath(rootFD)
	if err != nil || rootNow != root {
		return protocol.CloneCleanupWireResult{}, fmt.Errorf("pinned B root changed before cleanup: got=%+v want=%+v err=%v", rootNow, root, err)
	}
	if err := verifyCleanupRootNamed(cleanup.RootPath, rootFD, root, config.AUID, config.BUID, config.BGID); err != nil {
		return protocol.CloneCleanupWireResult{}, fmt.Errorf("configured B root changed before cleanup: %w", err)
	}
	targetDevice := cleanup.CheckoutReceipt.TargetDevice
	targetInode := cleanup.CheckoutReceipt.TargetInode
	disposition := authority.LinuxCloneTargetDispositionAlreadyAbsent
	if targetFDPresent {
		targetNow, err := fstatCleanupPath(targetFD)
		if err != nil || targetNow.Device != targetDevice || targetNow.Inode != targetInode || targetNow.Device != root.Device {
			return protocol.CloneCleanupWireResult{}, fmt.Errorf("pinned target FD changed before cleanup: got=%+v want=%d:%d err=%v", targetNow, targetDevice, targetInode, err)
		}
		targetFDFromRoot, targetStat, err := openCleanupRelativeDirectory(rootFD, relative,
			unix.O_PATH|unix.O_DIRECTORY, config.BUID, config.BGID, root.Device)
		if err != nil {
			return protocol.CloneCleanupWireResult{}, fmt.Errorf("reopen exact RunKey target from pinned root: %w", err)
		}
		_ = unix.Close(targetFDFromRoot)
		if uint64(targetStat.Dev) != targetDevice || targetStat.Ino != targetInode || uint64(targetStat.Dev) != root.Device ||
			targetStat.Uid != config.BUID || targetStat.Gid != config.BGID {
			return protocol.CloneCleanupWireResult{}, errors.New("RunKey target path was replaced before cleanup")
		}
		if err := removePinnedCloneTarget(ctx, rootFD, targetFD, cleanup.RootPath, relative, root, target, config, nil); err != nil {
			return protocol.CloneCleanupWireResult{}, err
		}
		disposition = authority.LinuxCloneTargetDispositionRemoved
	} else {
		if err := verifyCloneTargetAbsentUnderRoot(rootFD, relative, config, root); err != nil {
			return protocol.CloneCleanupWireResult{}, fmt.Errorf("Store-authorized absent target does not read back: %w", err)
		}
	}
	rootAfter, err := fstatCleanupPath(rootFD)
	if err != nil || rootAfter != root {
		return protocol.CloneCleanupWireResult{}, fmt.Errorf("pinned B root changed after cleanup: got=%+v want=%+v err=%v", rootAfter, root, err)
	}
	if err := verifyCleanupRootNamed(cleanup.RootPath, rootFD, root, config.AUID, config.BUID, config.BGID); err != nil {
		return protocol.CloneCleanupWireResult{}, fmt.Errorf("configured B root changed after cleanup: %w", err)
	}
	if targetFDPresent {
		targetAfter, err := fstatCleanupPath(targetFD)
		if err != nil || targetAfter.Device != targetDevice || targetAfter.Inode != targetInode {
			return protocol.CloneCleanupWireResult{}, fmt.Errorf("pinned target FD changed while cleanup was in flight: got=%+v want=%d:%d err=%v", targetAfter, targetDevice, targetInode, err)
		}
	}
	if err := verifyCloneTargetAbsentUnderRoot(rootFD, relative, config, root); err != nil {
		return protocol.CloneCleanupWireResult{}, fmt.Errorf("RunKey target is not independently absent after cleanup: %w", err)
	}
	return protocol.CloneCleanupWireResult{
		Version: 1, Kind: "cleanup-result", Cleanup: cleanup.Clone(),
		RootDevice: root.Device, RootInode: root.Inode,
		TargetDevice: targetDevice, TargetInode: targetInode,
		Disposition: disposition, PathRewalkVerified: true, BOwnedMountConfined: true,
		TargetNlinkZero: targetFDPresent,
		RootDeleterUID:  uint32(os.Geteuid()),
		RootDeleter:     authority.LinuxCgroupProcessIdentity{PID: worker.PID, Identity: worker.Identity},
	}, nil
}

func fstatCleanupPath(fd int) (cleanupDirectoryIdentity, error) {
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return cleanupDirectoryIdentity{}, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFDIR {
		return cleanupDirectoryIdentity{}, errors.New("pinned cleanup descriptor is not a directory")
	}
	return cleanupDirectoryIdentity{Device: uint64(stat.Dev), Inode: stat.Ino}, nil
}

func verifyCleanupRootNamed(path string, rootFD int, expected cleanupDirectoryIdentity, aUID, bUID, bGID uint32) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" || strings.ContainsAny(path, "\x00\r\n") {
		return errors.New("cleanup RootPath is not a canonical absolute path")
	}
	currentFD, err := unix.Open("/", unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open root for cleanup RootPath rewalk: %w", err)
	}
	var currentStat unix.Stat_t
	if err := unix.Fstat(currentFD, &currentStat); err != nil || currentStat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		currentStat.Uid != 0 || currentStat.Gid != 0 || currentStat.Mode&0022 != 0 {
		_ = unix.Close(currentFD)
		return fmt.Errorf("filesystem root is not root-owned and non-writable: %v", err)
	}
	components := strings.Split(strings.TrimPrefix(path, "/"), "/")
	for index, component := range components {
		if component == "" || component == "." || component == ".." {
			_ = unix.Close(currentFD)
			return errors.New("cleanup RootPath contains an unsafe component")
		}
		how := &unix.OpenHow{
			Flags:   uint64(unix.O_PATH | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NOFOLLOW),
			Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS),
		}
		nextFD, openErr := unix.Openat2(currentFD, component, how)
		if openErr != nil {
			_ = unix.Close(currentFD)
			return fmt.Errorf("rewalk cleanup RootPath component %q: %w", component, openErr)
		}
		_ = unix.Close(currentFD)
		currentFD = nextFD
		if err := unix.Fstat(currentFD, &currentStat); err != nil {
			_ = unix.Close(currentFD)
			return err
		}
		final := index == len(components)-1
		allowedOwner := currentStat.Uid == 0 || currentStat.Uid == aUID
		if final {
			allowedOwner = currentStat.Uid == bUID && currentStat.Gid == bGID
		}
		if currentStat.Mode&unix.S_IFMT != unix.S_IFDIR || !allowedOwner || currentStat.Mode&07000 != 0 || currentStat.Mode&0022 != 0 {
			_ = unix.Close(currentFD)
			return fmt.Errorf("cleanup RootPath ancestor %q has unsafe owner or mode", component)
		}
	}
	defer unix.Close(currentFD)
	if uint64(currentStat.Dev) != expected.Device || currentStat.Ino != expected.Inode {
		return errors.New("configured B root path no longer resolves to the pinned Root O_PATH inode")
	}
	pinned, err := fstatCleanupPath(rootFD)
	if err != nil || pinned != expected {
		return fmt.Errorf("pinned B Root FD changed during absolute path rewalk: %v", err)
	}
	return nil
}

func openCleanupRelativeDirectory(rootFD int, relative string, flags int, bUID, bGID uint32, rootDevice uint64) (int, unix.Stat_t, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != relative || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return -1, unix.Stat_t{}, errors.New("cleanup relative directory escapes pinned root")
	}
	components := strings.Split(filepath.ToSlash(relative), "/")
	currentFD := rootFD
	ownsCurrent := false
	for index, component := range components {
		if component == "" || component == "." || component == ".." || strings.ContainsRune(component, '\x00') {
			if ownsCurrent {
				_ = unix.Close(currentFD)
			}
			return -1, unix.Stat_t{}, errors.New("cleanup relative path contains an unsafe component")
		}
		openFlags := unix.O_PATH | unix.O_DIRECTORY
		if index == len(components)-1 {
			openFlags = flags
		}
		how := &unix.OpenHow{
			Flags:   uint64(openFlags | unix.O_CLOEXEC | unix.O_NOFOLLOW),
			Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV),
		}
		nextFD, err := unix.Openat2(currentFD, component, how)
		if err != nil {
			if ownsCurrent {
				_ = unix.Close(currentFD)
			}
			return -1, unix.Stat_t{}, err
		}
		if ownsCurrent {
			_ = unix.Close(currentFD)
		}
		currentFD = nextFD
		ownsCurrent = true
		var stat unix.Stat_t
		if err := unix.Fstat(currentFD, &stat); err != nil {
			_ = unix.Close(currentFD)
			return -1, unix.Stat_t{}, err
		}
		if stat.Mode&unix.S_IFMT != unix.S_IFDIR || uint64(stat.Dev) != rootDevice ||
			stat.Uid != bUID || stat.Gid != bGID || stat.Mode&07000 != 0 || stat.Mode&0022 != 0 {
			_ = unix.Close(currentFD)
			return -1, unix.Stat_t{}, errors.New("root-relative cleanup ancestor has unsafe type, owner, mode, or mount identity")
		}
		if index == len(components)-1 {
			return currentFD, stat, nil
		}
	}
	return -1, unix.Stat_t{}, errors.New("cleanup relative path has no components")
}

func verifyCloneTargetAbsentUnderRoot(rootFD int, relative string, config protocol.OwnerConfig, root cleanupDirectoryIdentity) error {
	parentRelative, name := filepath.Split(relative)
	parentRelative = strings.TrimSuffix(parentRelative, string(filepath.Separator))
	if parentRelative == "" || name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00\r\n") {
		return errors.New("RunKey target basename is invalid")
	}
	parentFD, _, err := openCleanupRelativeDirectory(rootFD, parentRelative, unix.O_RDONLY|unix.O_DIRECTORY,
		config.BUID, config.BGID, root.Device)
	if err != nil {
		return fmt.Errorf("reopen exact RunKey cleanup ancestors: %w", err)
	}
	defer unix.Close(parentFD)
	var stat unix.Stat_t
	for attempt := 0; attempt < 2; attempt++ {
		err := unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil {
			return err
		}
		return errors.New("RunKey target name is present or replaced")
	}
	if err := unix.Fsync(parentFD); err != nil {
		return fmt.Errorf("sync B target parent before absence proof: %w", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		err := unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			continue
		}
		if err != nil {
			return err
		}
		return errors.New("RunKey target name reappeared during durable absence readback")
	}
	return nil
}

// removePinnedCloneTarget performs best-effort removal inside the shared B-owned
// mount. B may race a same-UID rename/exchange between rewalk and unlink, so this
// does not prove no sibling entry was removed. It returns success only when the
// original pinned target FD is observed with nlink zero after rmdir and parent fsync.
func removePinnedCloneTarget(ctx context.Context, rootFD, targetFD int, rootPath, targetRelative string,
	rootIdentity, targetIdentity cleanupDirectoryIdentity, config protocol.OwnerConfig, beforeFinalRmdir func() error) error {
	if targetFD < 0 {
		return errors.New("pinned original B checkout descriptor is unavailable")
	}
	// beforeFinalRmdir is nil in production; tests use this narrow seam to force
	// a same-UID rename exactly between the final name check and rmdir.
	targetFlags, err := unix.FcntlInt(uintptr(targetFD), unix.F_GETFL, 0)
	if err != nil || targetFlags&unix.O_PATH == 0 {
		return fmt.Errorf("original B checkout descriptor is not O_PATH: flags=%#x err=%v", targetFlags, err)
	}
	var originalTarget unix.Stat_t
	if err := unix.Fstat(targetFD, &originalTarget); err != nil || originalTarget.Mode&unix.S_IFMT != unix.S_IFDIR ||
		uint64(originalTarget.Dev) != targetIdentity.Device || originalTarget.Ino != targetIdentity.Inode ||
		uint64(originalTarget.Dev) != rootIdentity.Device || originalTarget.Uid != config.BUID || originalTarget.Gid != config.BGID {
		return fmt.Errorf("original pinned B checkout identity differs before removal: dev=%d ino=%d uid=%d gid=%d err=%v",
			originalTarget.Dev, originalTarget.Ino, originalTarget.Uid, originalTarget.Gid, err)
	}
	parentRelative, targetName := filepath.Split(targetRelative)
	parentRelative = strings.TrimSuffix(parentRelative, string(filepath.Separator))
	if parentRelative == "" || targetName == "" || targetName == "." || targetName == ".." {
		return errors.New("RunKey target parent/name is invalid")
	}
	parentFD, parentStat, err := openCleanupRelativeDirectory(rootFD, parentRelative, unix.O_RDONLY|unix.O_DIRECTORY,
		config.BUID, config.BGID, rootIdentity.Device)
	if err != nil {
		return fmt.Errorf("open exact RunKey target parent: %w", err)
	}
	defer unix.Close(parentFD)
	pinnedTargetFD, targetStat, err := openCleanupRelativeDirectory(rootFD, targetRelative, unix.O_RDONLY|unix.O_DIRECTORY,
		config.BUID, config.BGID, rootIdentity.Device)
	if err != nil {
		return fmt.Errorf("open exact RunKey target directory: %w", err)
	}
	defer unix.Close(pinnedTargetFD)
	var rootStat unix.Stat_t
	if err := unix.Fstat(rootFD, &rootStat); err != nil {
		return err
	}
	if uint64(rootStat.Dev) != rootIdentity.Device || rootStat.Ino != rootIdentity.Inode ||
		rootStat.Uid != config.BUID || rootStat.Gid != config.BGID || rootStat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		rootStat.Mode&07000 != 0 || rootStat.Mode&0022 != 0 ||
		uint64(targetStat.Dev) != targetIdentity.Device || targetStat.Ino != targetIdentity.Inode ||
		targetStat.Uid != config.BUID || targetStat.Gid != config.BGID || targetStat.Mode&unix.S_IFMT != unix.S_IFDIR ||
		targetStat.Mode&07000 != 0 || targetStat.Mode&0022 != 0 ||
		uint64(parentStat.Dev) != rootIdentity.Device || parentStat.Uid != config.BUID || parentStat.Gid != config.BGID ||
		parentStat.Mode&unix.S_IFMT != unix.S_IFDIR || parentStat.Mode&07000 != 0 || parentStat.Mode&0022 != 0 {
		return errors.New("RunKey target or parent differs from pinned B checkout identity")
	}
	walker := &cloneCleanupWalker{
		ctx: ctx, rootFD: rootFD, rootPath: rootPath,
		root: rootIdentity, target: targetIdentity,
		rootStat: rootStat, targetStat: targetStat, aUID: config.AUID,
		targetRel: targetRelative, bUID: config.BUID, bGID: config.BGID,
		deadline: time.Now().Add(cleanupWorkTimeout),
	}
	if err := walker.clearDirectory(pinnedTargetFD, targetRelative, targetStat, 0); err != nil {
		return fmt.Errorf("recursively remove exact B checkout: %w", err)
	}
	if err := walker.verifyTargetPath(); err != nil {
		return err
	}
	currentParentFD, currentParentStat, err := openCleanupRelativeDirectory(rootFD, parentRelative, unix.O_RDONLY|unix.O_DIRECTORY,
		config.BUID, config.BGID, rootIdentity.Device)
	if err != nil {
		return fmt.Errorf("reopen exact B target parent before target rmdir: %w", err)
	}
	defer unix.Close(currentParentFD)
	if !sameCleanupDirectoryIdentity(parentStat, currentParentStat) {
		return errors.New("B target parent changed before target rmdir")
	}
	var named unix.Stat_t
	if err := unix.Fstatat(currentParentFD, targetName, &named, unix.AT_SYMLINK_NOFOLLOW); err != nil ||
		named.Mode&unix.S_IFMT != unix.S_IFDIR || uint64(named.Dev) != targetIdentity.Device || named.Ino != targetIdentity.Inode ||
		named.Uid != config.BUID || named.Gid != config.BGID || named.Mode&07777 != targetStat.Mode&07777 {
		return fmt.Errorf("exact B target name changed before final rmdir: %w", errorOr(err, errors.New("target inode mismatch")))
	}
	if beforeFinalRmdir != nil {
		if err := beforeFinalRmdir(); err != nil {
			return fmt.Errorf("complete deterministic B rename interleaving: %w", err)
		}
	}
	if err := unix.Unlinkat(currentParentFD, targetName, unix.AT_REMOVEDIR); err != nil {
		return fmt.Errorf("remove exact B target directory: %w", err)
	}
	if err := unix.Fsync(currentParentFD); err != nil {
		return fmt.Errorf("sync B target parent after rmdir: %w", err)
	}
	if err := verifyCleanupEntryAbsent(currentParentFD, targetName); err != nil {
		return err
	}
	var unlinkedTarget unix.Stat_t
	if err := unix.Fstat(targetFD, &unlinkedTarget); err != nil || unlinkedTarget.Mode&unix.S_IFMT != unix.S_IFDIR ||
		uint64(unlinkedTarget.Dev) != targetIdentity.Device || unlinkedTarget.Ino != targetIdentity.Inode ||
		unlinkedTarget.Uid != config.BUID || unlinkedTarget.Gid != config.BGID || unlinkedTarget.Nlink != 0 {
		return fmt.Errorf("original pinned B checkout inode was not unlinked after parent fsync: dev=%d ino=%d nlink=%d err=%v",
			unlinkedTarget.Dev, unlinkedTarget.Ino, unlinkedTarget.Nlink, err)
	}
	return verifyCloneTargetAbsentUnderRoot(rootFD, targetRelative, config, rootIdentity)
}

func launchAChildGate(control, leaf, release, execStatus int, peer *unix.Ucred, config protocol.OwnerConfig, request protocol.LaunchRequest) error {
	defer clear(request.Secret[:])
	producer, identityErr := protocol.CaptureLauncherIdentity(os.Getpid())
	if identityErr != nil {
		_ = sendResult(control, protocol.LaunchResult{Status: protocol.LaunchStatusUnresolved, Errno: errnoNumber(identityErr), Token: request.ReleaseToken}, nil)
		return fmt.Errorf("capture exact root producer identity before gate clone: %w", identityErr)
	}
	gateImage, err := openTrustedGateImage()
	if err != nil {
		return sendNeverProduced(control, request, producer, err)
	}

	ownerPIDFD, err := unix.PidfdOpen(int(peer.Pid), 0)
	if err != nil {
		_ = unix.Close(gateImage)
		return sendNeverProduced(control, request, producer, fmt.Errorf("open authenticated A owner pidfd: %w", err))
	}
	if os.Getppid() != int(peer.Pid) {
		_ = unix.Close(ownerPIDFD)
		_ = unix.Close(gateImage)
		return sendNeverProduced(control, request, producer, errors.New("A owner exited before gate creation"))
	}
	if err := unix.PidfdSendSignal(ownerPIDFD, 0, nil, 0); err != nil {
		_ = unix.Close(ownerPIDFD)
		_ = unix.Close(gateImage)
		return sendNeverProduced(control, request, producer, fmt.Errorf("probe exact A owner pidfd: %w", err))
	}

	syncFDs, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		_ = unix.Close(ownerPIDFD)
		_ = unix.Close(gateImage)
		return sendNeverProduced(control, request, producer, fmt.Errorf("create gate mapping channel: %w", err))
	}
	syncParent, syncChild := syncFDs[0], syncFDs[1]
	defer unix.Close(syncParent)

	devNull, err := openNullDevice()
	if err != nil {
		_ = unix.Close(ownerPIDFD)
		_ = unix.Close(syncParent)
		_ = unix.Close(syncChild)
		_ = unix.Close(gateImage)
		return sendNeverProduced(control, request, producer, err)
	}

	gatePIDFD := -1
	gateFile := os.NewFile(uintptr(gateImage), "symmetry-linux-gate-image")
	releaseFile := os.NewFile(uintptr(release), "symmetry-linux-gate-release")
	syncFile := os.NewFile(uintptr(syncChild), "symmetry-linux-gate-map-channel")
	ownerFile := os.NewFile(uintptr(ownerPIDFD), "symmetry-linux-gate-owner-pidfd")
	statusFile := os.NewFile(uintptr(execStatus), "symmetry-linux-gate-exec-status")
	stdio := os.NewFile(uintptr(devNull), "symmetry-linux-gate-stdio")
	if gateFile == nil || releaseFile == nil || syncFile == nil || ownerFile == nil || statusFile == nil || stdio == nil {
		closeFiles(gateFile, releaseFile, syncFile, ownerFile, statusFile, stdio)
		_ = unix.Close(ownerPIDFD)
		_ = unix.Close(syncParent)
		return sendNeverProduced(control, request, producer, errors.New("wrap static gate launch descriptors"))
	}
	cmd := exec.Command("/proc/self/fd/3")
	cmd.Args = []string{"symmetry-linux-gate"}
	cmd.Env = []string{"PATH=/usr/bin:/bin", "LANG=C"}
	cmd.Dir = "/"
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdio, stdio, stdio
	cmd.ExtraFiles = []*os.File{gateFile, releaseFile, syncFile, ownerFile, statusFile}
	cmd.SysProcAttr = &syscall.SysProcAttr{UseCgroupFD: true, CgroupFD: leaf, PidFD: &gatePIDFD}
	if err := cmd.Start(); err != nil {
		clear(request.Secret[:])
		closeErr := closeFiles(gateFile, releaseFile, syncFile, ownerFile, statusFile, stdio)
		_ = sendResult(control, protocol.LaunchResult{Status: protocol.LaunchStatusUnresolved, Errno: errnoNumber(err), Token: request.ReleaseToken}, nil)
		return errors.Join(fmt.Errorf("clone static A gate result is uncertain: %w", err), closeErr)
	}
	clear(request.Secret[:])
	// Parent-side copies would defeat EOF-based owner/release detection and
	// close-on-exec status. The child has its own exact inherited descriptors.
	if err := closeFiles(gateFile, releaseFile, syncFile, ownerFile, statusFile, stdio); err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("close transient root copies of gate FDs: %w", err))
	}
	if gatePIDFD < 0 {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, errors.New("clone3 did not return the required gate pidfd"))
	}
	if os.Getppid() != int(peer.Pid) {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, errors.New("A owner exited during gate creation"))
	}

	deadline := time.Now().Add(gateReadyTimeout)
	ready, fds, err := receiveFrame(syncParent, 16, deadline)
	if err != nil || len(fds) != 0 || protocol.ValidateGateReady(ready) != nil {
		closeFDs(fds)
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("gate did not reach A user namespace setup: %w", errorOr(err, protocol.ValidateGateReady(ready))))
	}
	if err := writeOnlyBMaps(cmd.Process.Pid, config); err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("write exact B-only user namespace maps: %w", err))
	}
	if err := verifyMappedGate(cmd.Process.Pid, int(os.Getpid()), int(peer.Pid), config); err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("verify A gate identity and B-only maps: %w", err))
	}
	if err := sendFrame(syncParent, protocol.EncodeGateMap(config, request.ReleaseToken)); err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("authorize mapped A gate: %w", err))
	}
	parked, fds, err := receiveFrame(syncParent, 16, deadline)
	if err != nil || len(fds) != 0 || protocol.ValidateGateParked(parked) != nil {
		closeFDs(fds)
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("A gate did not park before RELEASE: %w", errorOr(err, protocol.ValidateGateParked(parked))))
	}
	if err := waitPeerEOF(syncParent, deadline); err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("verify gate mapping-channel retirement: %w", err))
	}
	if err := unix.PidfdSendSignal(gatePIDFD, 0, nil, 0); err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("probe parked gate pidfd: %w", err))
	}
	identity, err := processIdentity(cmd.Process.Pid, int(peer.Pid))
	if err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("capture exact A gate identity: %w", err))
	}
	if err := verifyMappedGate(cmd.Process.Pid, int(os.Getpid()), int(peer.Pid), config); err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("recheck A gate before pidfd handoff: %w", err))
	}
	if err := sendResult(control, protocol.LaunchResult{Status: protocol.LaunchStatusReady, Token: request.ReleaseToken, Identity: identity}, []int{gatePIDFD}); err != nil {
		return failAfterStart(control, cmd, gatePIDFD, request.ReleaseToken, fmt.Errorf("transfer exact gate pidfd to A helper: %w", err))
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release transient launcher child handle after pidfd transfer: %w", err)
	}
	return nil
}

func sendNeverProduced(control int, request protocol.LaunchRequest, producer protocol.LauncherIdentity, cause error) error {
	launcher := authority.LinuxCgroupProcessIdentity{PID: producer.PID, Identity: producer.Identity}
	authenticator, signErr := protocol.SignGateNeverProducedAuthenticatorKey(request.Secret[:], request.LaunchToken, launcher)
	if signErr != nil {
		result := protocol.LaunchResult{Status: protocol.LaunchStatusUnresolved, Errno: errnoNumber(errors.Join(cause, signErr)), Token: request.ReleaseToken}
		return errors.Join(cause, signErr, sendResult(control, result, nil))
	}
	result := protocol.LaunchResult{
		Status:        protocol.LaunchStatusNeverProduced,
		Errno:         errnoNumber(cause),
		Token:         request.ReleaseToken,
		Authenticator: authenticator,
	}
	return errors.Join(cause, sendResult(control, result, nil))
}

func failAfterStart(control int, cmd *exec.Cmd, pidfd int, token [32]byte, cause error) error {
	if pidfd < 0 {
		_ = sendResult(control, protocol.LaunchResult{Status: protocol.LaunchStatusUnresolved, Errno: errnoNumber(cause), Token: token}, nil)
		return cause
	}
	_ = unix.PidfdSendSignal(pidfd, unix.SIGKILL, nil, 0)
	if waitPIDFD(pidfd, time.Now().Add(gateKillTimeout)) == nil {
		if cmd.Process != nil {
			_ = cmd.Wait()
		}
		_ = unix.Close(pidfd)
		_ = sendResult(control, protocol.LaunchResult{Status: protocol.LaunchStatusRejected, Errno: errnoNumber(cause), Token: token}, nil)
		return cause
	}
	identity, identityErr := processIdentity(cmd.Process.Pid, os.Getppid())
	if identityErr != nil {
		identity = protocol.ProcessIdentity{PID: cmd.Process.Pid, OwnerPID: os.Getppid()}
	}
	sendErr := sendResult(control, protocol.LaunchResult{Status: protocol.LaunchStatusUnresolved, Errno: errnoNumber(cause), Token: token, Identity: identity}, []int{pidfd})
	if err := cmd.Process.Release(); err != nil {
		sendErr = errors.Join(sendErr, err)
	}
	return errors.Join(cause, sendErr)
}

func verifyInheritedDescriptors(config protocol.OwnerConfig) error {
	for _, fd := range []int{controlFD, releaseFD, execStatusFD} {
		var socketType int
		socketType, err := unix.GetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_TYPE)
		if err != nil || socketType != unix.SOCK_SEQPACKET {
			return fmt.Errorf("inherited FD %d is not SOCK_SEQPACKET: type=%d err=%v", fd, socketType, err)
		}
	}
	if err := verifyCgroupLeaf(leafFD, config.AUID); err != nil {
		return err
	}
	return nil
}

func verifySocketPeer(fd int, expected *unix.Ucred) error {
	peer, err := unix.GetsockoptUcred(fd, unix.SOL_SOCKET, unix.SO_PEERCRED)
	if err != nil {
		return err
	}
	if peer.Pid != expected.Pid || peer.Uid != expected.Uid || peer.Gid != expected.Gid {
		return fmt.Errorf("socket peer differs from authenticated A process: got %#v want %#v", peer, expected)
	}
	return nil
}

func verifyCgroupLeaf(fd int, aUID uint32) error {
	var filesystem unix.Statfs_t
	if err := unix.Fstatfs(fd, &filesystem); err != nil {
		return fmt.Errorf("stat inherited cgroup leaf filesystem: %w", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		return fmt.Errorf("stat inherited cgroup leaf: %w", err)
	}
	if uint64(filesystem.Type) != uint64(unix.CGROUP2_SUPER_MAGIC) || stat.Mode&unix.S_IFMT != unix.S_IFDIR || stat.Uid != aUID {
		return errors.New("inherited leaf FD is not an A-owned cgroup-v2 directory")
	}
	return nil
}

func openTrustedGateImage() (int, error) {
	fd, err := unix.Open(gateImagePath, unix.O_PATH|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, fmt.Errorf("open pinned static gate image: %w", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = unix.Close(fd)
		return -1, fmt.Errorf("stat pinned gate image: %w", err)
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFREG || stat.Uid != 0 || stat.Gid != 0 || stat.Mode&0022 != 0 ||
		stat.Mode&0111 == 0 || stat.Nlink != 1 {
		_ = unix.Close(fd)
		return -1, errors.New("gate image is not singly-linked, root-owned, executable, and immutable to A/B")
	}
	return fd, nil
}

func openNullDevice() (int, error) {
	fd, err := unix.Open("/dev/null", unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return -1, fmt.Errorf("open fixed /dev/null: %w", err)
	}
	var stat unix.Stat_t
	if err := unix.Fstat(fd, &stat); err != nil {
		_ = unix.Close(fd)
		return -1, err
	}
	if stat.Mode&unix.S_IFMT != unix.S_IFCHR || unix.Major(uint64(stat.Rdev)) != 1 || unix.Minor(uint64(stat.Rdev)) != 3 {
		_ = unix.Close(fd)
		return -1, errors.New("fixed /dev/null is not the null character device")
	}
	return fd, nil
}

func writeOnlyBMaps(pid int, config protocol.OwnerConfig) error {
	procFD, err := unix.Open(fmt.Sprintf("/proc/%d", pid), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open exact gate proc directory: %w", err)
	}
	defer unix.Close(procFD)
	if err := writeProcAt(procFD, "uid_map", fmt.Sprintf("1 %d 1\n", config.BUID)); err != nil {
		return fmt.Errorf("write sole B uid mapping: %w", err)
	}
	// The hosted system probe established that setgroups cannot be cleared by A
	// from inside this user namespace, even before gid_map. The root wrapper has
	// already emptied groups before the gate dropped to A; root now denies
	// setgroups before writing the B-only gid_map.
	if err := writeProcAt(procFD, "setgroups", "deny\n"); err != nil {
		return fmt.Errorf("deny user-namespace setgroups before gid_map: %w", err)
	}
	if err := writeProcAt(procFD, "gid_map", fmt.Sprintf("1 %d 1\n", config.BGID)); err != nil {
		return fmt.Errorf("write sole B gid mapping: %w", err)
	}
	return nil
}

func writeProcAt(procFD int, name, value string) error {
	fd, err := unix.Openat(procFD, name, unix.O_WRONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	n, writeErr := unix.Write(fd, []byte(value))
	closeErr := unix.Close(fd)
	if writeErr != nil || n != len(value) || closeErr != nil {
		return errors.Join(writeErr, closeErr, shortWrite(n, len(value)))
	}
	return nil
}

func verifyMappedGate(pid, launcherPID, ownerPID int, config protocol.OwnerConfig) error {
	status, err := readStatus(pid)
	if err != nil {
		return err
	}
	wantedUIDs := [4]uint32{config.AUID, config.AUID, config.AUID, config.AUID}
	wantedGIDs := [4]uint32{config.AGID, config.AGID, config.AGID, config.AGID}
	if status.uids != wantedUIDs || status.gids != wantedGIDs || len(status.groups) != 0 || status.parent != launcherPID {
		return fmt.Errorf("gate did not remain A with empty groups under root launcher: uid=%v gid=%v groups=%v ppid=%d",
			status.uids, status.gids, status.groups, status.parent)
	}
	if err := verifyMap(pid, "uid_map", fmt.Sprintf("1 %d 1", config.BUID)); err != nil {
		return err
	}
	if err := verifyMap(pid, "gid_map", fmt.Sprintf("1 %d 1", config.BGID)); err != nil {
		return err
	}
	setgroups, err := readProcFile(pid, "setgroups")
	if err != nil {
		return err
	}
	if strings.TrimSpace(setgroups) != "deny" {
		return errors.New("gate user namespace does not deny setgroups")
	}
	userNS, err := namespaceStat(pid)
	if err != nil {
		return err
	}
	ownerNS, err := namespaceStat(ownerPID)
	if err != nil {
		return err
	}
	if userNS == ownerNS {
		return errors.New("gate did not create a distinct A-owned user namespace")
	}
	return nil
}

type processStatus struct {
	uids   [4]uint32
	gids   [4]uint32
	groups []uint32
	parent int
}

func readStatus(pid int) (processStatus, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return processStatus{}, fmt.Errorf("read exact gate status: %w", err)
	}
	var status processStatus
	seen := uint8(0)
	for _, line := range strings.Split(string(data), "\n") {
		key, value, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		fields := strings.Fields(value)
		parseIDs := func(destination *[4]uint32) error {
			if len(fields) != 4 {
				return errors.New("process status credential vector is malformed")
			}
			for index, field := range fields {
				parsed, err := strconv.ParseUint(field, 10, 32)
				if err != nil {
					return err
				}
				destination[index] = uint32(parsed)
			}
			return nil
		}
		switch key {
		case "Uid":
			if err := parseIDs(&status.uids); err != nil {
				return processStatus{}, err
			}
			seen |= 1
		case "Gid":
			if err := parseIDs(&status.gids); err != nil {
				return processStatus{}, err
			}
			seen |= 2
		case "Groups":
			status.groups = make([]uint32, len(fields))
			for index, field := range fields {
				parsed, err := strconv.ParseUint(field, 10, 32)
				if err != nil {
					return processStatus{}, err
				}
				status.groups[index] = uint32(parsed)
			}
			seen |= 4
		case "PPid":
			if len(fields) != 1 {
				return processStatus{}, errors.New("process PPid is malformed")
			}
			parent, err := strconv.Atoi(fields[0])
			if err != nil || parent <= 0 {
				return processStatus{}, errors.New("process PPid is invalid")
			}
			status.parent = parent
			seen |= 8
		}
	}
	if seen != 15 {
		return processStatus{}, errors.New("gate status lacks UID/GID/Groups/PPid")
	}
	return status, nil
}

type nsIdentity struct{ dev, ino uint64 }

func namespaceStat(pid int) (nsIdentity, error) {
	var stat syscall.Stat_t
	if err := syscall.Stat(fmt.Sprintf("/proc/%d/ns/user", pid), &stat); err != nil {
		return nsIdentity{}, fmt.Errorf("stat gate user namespace: %w", err)
	}
	return nsIdentity{dev: uint64(stat.Dev), ino: stat.Ino}, nil
}

func verifyMap(pid int, name, expected string) error {
	value, err := readProcFile(pid, name)
	if err != nil {
		return err
	}
	if strings.Join(strings.Fields(value), " ") != expected {
		return fmt.Errorf("gate %s is not the exact fixed-B map: %q", name, value)
	}
	return nil
}

func readProcFile(pid int, name string) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/%s", pid, name))
	if err != nil {
		return "", fmt.Errorf("read gate %s: %w", name, err)
	}
	return string(data), nil
}

func processIdentity(pid, ownerPID int) (protocol.ProcessIdentity, error) {
	statData, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return protocol.ProcessIdentity{}, fmt.Errorf("read gate process stat: %w", err)
	}
	end := bytes.LastIndexByte(statData, ')')
	if end < 0 || end+1 >= len(statData) {
		return protocol.ProcessIdentity{}, errors.New("gate process stat is malformed")
	}
	fields := strings.Fields(string(statData[end+1:]))
	if len(fields) <= 19 {
		return protocol.ProcessIdentity{}, errors.New("gate process start time is missing")
	}
	startTime, err := strconv.ParseUint(fields[19], 10, 64)
	if err != nil || startTime == 0 {
		return protocol.ProcessIdentity{}, errors.New("gate process start time is invalid")
	}
	namespace, err := namespaceStat(pid)
	if err != nil {
		return protocol.ProcessIdentity{}, err
	}
	status, err := readStatus(pid)
	if err != nil {
		return protocol.ProcessIdentity{}, err
	}
	bootID, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return protocol.ProcessIdentity{}, fmt.Errorf("read system boot identity: %w", err)
	}
	return protocol.ProcessIdentity{
		PID:          pid,
		StartTime:    startTime,
		BootID:       strings.TrimSpace(string(bootID)),
		UserNSDevice: namespace.dev,
		UserNSInode:  namespace.ino,
		HostUID:      status.uids[0],
		HostGID:      status.gids[0],
		OwnerPID:     ownerPID,
	}, nil
}

func receiveControlRequest(fd int, deadline time.Time) ([]byte, []int, error) {
	if err := waitFD(fd, deadline); err != nil {
		return nil, nil, fmt.Errorf("wait for one authenticated A launch request: %w", err)
	}
	data := make([]byte, protocol.LaunchRequestSize)
	oob := make([]byte, unix.CmsgSpace(4*4))
	n, oobn, flags, _, err := unix.Recvmsg(fd, data, oob, unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		return nil, nil, fmt.Errorf("receive authenticated A launch request: %w", err)
	}
	var fds []int
	if oobn > 0 {
		fds, err = parseRights(oob[:oobn])
		if err != nil {
			return nil, nil, err
		}
	}
	if n != protocol.LaunchRequestSize || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		closeFDs(fds)
		return nil, nil, errors.New("A launch request has invalid size or truncation flags")
	}
	return data[:n], fds, nil
}

func waitFD(fd int, deadline time.Time) error {
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errors.New("authenticated A launch request deadline expired")
		}
		if remaining > 100*time.Millisecond {
			remaining = 100 * time.Millisecond
		}
		pollFDs := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		count, err := unix.Poll(pollFDs, int((remaining+time.Millisecond-1)/time.Millisecond))
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
	}
}

func waitPeerEOF(fd int, deadline time.Time) error {
	if err := waitFD(fd, deadline); err != nil {
		return err
	}
	var packet [1]byte
	oob := make([]byte, unix.CmsgSpace(4*4))
	n, oobn, flags, _, err := unix.Recvmsg(fd, packet[:], oob, unix.MSG_DONTWAIT|unix.MSG_CMSG_CLOEXEC)
	if oobn != 0 {
		if received, parseErr := parseRights(oob[:oobn]); parseErr == nil {
			closeFDs(received)
		} else {
			err = errors.Join(err, parseErr)
		}
	}
	if err != nil || n != 0 || flags&unix.MSG_EOR != 0 {
		return fmt.Errorf("gate mapping channel did not close cleanly: n=%d flags=%#x err=%v", n, flags, err)
	}
	return nil
}

func receiveFrame(fd, max int, deadline time.Time) ([]byte, []int, error) {
	if err := waitFD(fd, deadline); err != nil {
		return nil, nil, err
	}
	data := make([]byte, max)
	oob := make([]byte, unix.CmsgSpace(4*4))
	n, oobn, flags, _, err := unix.Recvmsg(fd, data, oob, unix.MSG_CMSG_CLOEXEC)
	if err != nil {
		return nil, nil, err
	}
	var fds []int
	if oobn != 0 {
		fds, err = parseRights(oob[:oobn])
		if err != nil {
			return nil, nil, err
		}
	}
	if n == 0 || flags&(unix.MSG_TRUNC|unix.MSG_CTRUNC) != 0 {
		closeFDs(fds)
		return nil, nil, errors.New("gate mapping frame is empty or truncated")
	}
	return data[:n], fds, nil
}

func parseRights(oob []byte) ([]int, error) {
	messages, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return nil, err
	}
	var fds []int
	for _, message := range messages {
		if message.Header.Level != unix.SOL_SOCKET || message.Header.Type != unix.SCM_RIGHTS {
			closeFDs(fds)
			return nil, errors.New("gate mapping channel carried unexpected ancillary data")
		}
		rights, err := unix.ParseUnixRights(&message)
		if err != nil {
			closeFDs(fds)
			return nil, err
		}
		fds = append(fds, rights...)
	}
	return fds, nil
}

func sendFrame(fd int, packet []byte) error {
	n, err := unix.SendmsgN(fd, packet, nil, nil, unix.MSG_NOSIGNAL)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return fmt.Errorf("gate mapping seqpacket short write: %d/%d", n, len(packet))
	}
	return nil
}

func sendResult(fd int, result protocol.LaunchResult, passedFDs []int) error {
	packet, err := protocol.EncodeLaunchResult(result)
	if err != nil {
		return err
	}
	var oob []byte
	if len(passedFDs) > 0 {
		if len(passedFDs) != 1 {
			return errors.New("root launcher may transfer exactly one gate pidfd")
		}
		oob = unix.UnixRights(passedFDs...)
	}
	n, err := unix.SendmsgN(fd, packet, oob, nil, unix.MSG_NOSIGNAL)
	if err != nil {
		return err
	}
	if n != len(packet) {
		return fmt.Errorf("root launcher result short write: %d/%d", n, len(packet))
	}
	return nil
}

func openProcDir(pid int) (int, error) {
	return unix.Open(fmt.Sprintf("/proc/%d", pid), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
}

func waitPIDFD(pidfd int, deadline time.Time) error {
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return errors.New("pidfd wait deadline exceeded")
		}
		if remaining > 100*time.Millisecond {
			remaining = 100 * time.Millisecond
		}
		millis := int((remaining + time.Millisecond - 1) / time.Millisecond)
		fds := []unix.PollFd{{Fd: int32(pidfd), Events: unix.POLLIN | unix.POLLHUP | unix.POLLERR}}
		count, err := unix.Poll(fds, millis)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		if err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
	}
}

func setParentDeathSignal(expectedParent int) error {
	if expectedParent <= 1 {
		return errors.New("transient launcher has no valid A parent")
	}
	if err := unix.Prctl(unix.PR_SET_PDEATHSIG, uintptr(unix.SIGKILL), 0, 0, 0); err != nil {
		return fmt.Errorf("arm transient launcher parent-death fence: %w", err)
	}
	if os.Getppid() != expectedParent {
		return errors.New("A owner exited while arming transient launcher parent-death fence")
	}
	return nil
}

func errnoNumber(err error) uint32 {
	var errno syscall.Errno
	if errors.As(err, &errno) && errno > 0 {
		return uint32(errno)
	}
	return uint32(unix.EPERM)
}

func errorOr(err, fallback error) error {
	if err != nil {
		return err
	}
	return fallback
}

func shortWrite(actual, want int) error {
	if actual == want {
		return nil
	}
	return fmt.Errorf("short proc control write: %d/%d", actual, want)
}

func closeFDs(fds []int) {
	for _, fd := range fds {
		_ = unix.Close(fd)
	}
}

func closeFiles(files ...*os.File) error {
	var closeErr error
	for _, file := range files {
		if file != nil {
			closeErr = errors.Join(closeErr, file.Close())
		}
	}
	return closeErr
}

type cloneCleanupWalker struct {
	ctx         context.Context
	rootFD      int
	rootPath    string
	root        cleanupDirectoryIdentity
	target      cleanupDirectoryIdentity
	rootStat    unix.Stat_t
	targetStat  unix.Stat_t
	targetRel   string
	aUID        uint32
	bUID        uint32
	bGID        uint32
	deadline    time.Time
	entriesSeen int
}

func (walker *cloneCleanupWalker) checkBudget(depth int) error {
	if err := walker.ctx.Err(); err != nil {
		return err
	}
	if time.Now().After(walker.deadline) {
		return errors.New("bounded root clone cleanup deadline expired")
	}
	if depth > cleanupMaxDepth || walker.entriesSeen > cleanupMaxEntries {
		return errors.New("root clone cleanup exceeded its fixed depth or entry bound")
	}
	return nil
}

func (walker *cloneCleanupWalker) openRelativeDirectory(path string, flags int) (int, unix.Stat_t, error) {
	return openCleanupRelativeDirectory(walker.rootFD, path, flags, walker.bUID, walker.bGID, walker.root.Device)
}

func sameCleanupDirectoryIdentity(left, right unix.Stat_t) bool {
	return left.Mode&unix.S_IFMT == unix.S_IFDIR && right.Mode&unix.S_IFMT == unix.S_IFDIR &&
		left.Dev == right.Dev && left.Ino == right.Ino && left.Uid == right.Uid && left.Gid == right.Gid &&
		left.Mode&07777 == right.Mode&07777
}

func sameCleanupEntryIdentity(left, right unix.Stat_t) bool {
	return left.Dev == right.Dev && left.Ino == right.Ino && left.Uid == right.Uid && left.Gid == right.Gid &&
		left.Mode&unix.S_IFMT == right.Mode&unix.S_IFMT && left.Mode&07777 == right.Mode&07777 &&
		left.Nlink == right.Nlink && left.Size == right.Size
}

func sameCleanupEntryForUnlink(left, right unix.Stat_t) bool {
	if left.Mode&unix.S_IFMT == unix.S_IFDIR {
		return sameCleanupDirectoryIdentity(left, right)
	}
	return sameCleanupEntryIdentity(left, right)
}

func (walker *cloneCleanupWalker) verifyTargetPath() error {
	if err := walker.checkBudget(0); err != nil {
		return err
	}
	rootNow, err := fstatCleanupPath(walker.rootFD)
	if err != nil || rootNow != walker.root {
		return fmt.Errorf("pinned B root identity changed: got=%+v want=%+v err=%v", rootNow, walker.root, err)
	}
	if err := verifyCleanupRootNamed(walker.rootPath, walker.rootFD, walker.root, walker.aUID, walker.bUID, walker.bGID); err != nil {
		return err
	}
	var rootStatNow unix.Stat_t
	if err := unix.Fstat(walker.rootFD, &rootStatNow); err != nil || !sameCleanupDirectoryIdentity(rootStatNow, walker.rootStat) {
		return fmt.Errorf("pinned B root metadata changed: err=%v", err)
	}
	fd, stat, err := walker.openRelativeDirectory(walker.targetRel, unix.O_PATH|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	_ = unix.Close(fd)
	if !sameCleanupDirectoryIdentity(stat, walker.targetStat) || uint64(stat.Dev) != walker.target.Device || stat.Ino != walker.target.Inode {
		return errors.New("RunKey target name no longer identifies the checkout inode")
	}
	return nil
}

func (walker *cloneCleanupWalker) verifyDirectoryPath(relative string, expected unix.Stat_t) error {
	if err := walker.verifyTargetPath(); err != nil {
		return err
	}
	fd, current, err := walker.openRelativeDirectory(relative, unix.O_PATH|unix.O_DIRECTORY)
	if err != nil {
		return err
	}
	_ = unix.Close(fd)
	if !sameCleanupDirectoryIdentity(current, expected) {
		return errors.New("root-relative directory was replaced during cleanup")
	}
	return nil
}

func (walker *cloneCleanupWalker) listDirectoryNames(directoryFD int) ([]string, error) {
	duplicate, err := unix.FcntlInt(uintptr(directoryFD), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		return nil, fmt.Errorf("duplicate B directory for bounded enumeration: %w", err)
	}
	directory := os.NewFile(uintptr(duplicate), "symmetry-linux-clone-cleanup-dir")
	if directory == nil {
		_ = unix.Close(duplicate)
		return nil, errors.New("wrap duplicated B directory for enumeration")
	}
	remaining := cleanupMaxEntries - walker.entriesSeen
	if remaining <= 0 {
		_ = directory.Close()
		return nil, errors.New("root clone cleanup exhausted its fixed entry bound")
	}
	limit := 1024
	if remaining < limit {
		limit = remaining + 1
	}
	entries := make([]os.DirEntry, 0, limit)
	var readErr error
	for {
		batch, err := directory.ReadDir(limit)
		entries = append(entries, batch...)
		if len(entries) > remaining {
			readErr = errors.New("root clone cleanup directory exceeded its fixed entry bound")
			break
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			readErr = err
			break
		}
		if len(batch) == 0 {
			break
		}
	}
	closeErr := directory.Close()
	if readErr != nil || closeErr != nil {
		return nil, errors.Join(readErr, closeErr)
	}
	if len(entries) > cleanupMaxEntries-walker.entriesSeen {
		return nil, errors.New("root clone cleanup directory exceeded its fixed entry bound")
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\x00\r\n") {
			return nil, errors.New("root clone cleanup encountered an invalid directory entry")
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (walker *cloneCleanupWalker) validateEntry(stat unix.Stat_t) error {
	if uint64(stat.Dev) != walker.root.Device || stat.Uid != walker.bUID || stat.Gid != walker.bGID || stat.Nlink == 0 || stat.Mode&06000 != 0 {
		return errors.New("B clone contains an entry outside its owner, device, link, or set-ID policy")
	}
	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		if stat.Mode&01000 != 0 || stat.Mode&0022 != 0 {
			return errors.New("B clone directory has sticky or group/world write permissions")
		}
	case unix.S_IFREG:
		if stat.Nlink != 1 || stat.Mode&0022 != 0 {
			return errors.New("B clone regular file is hard-linked or group/world writable")
		}
	case unix.S_IFLNK:
		if stat.Nlink != 1 {
			return errors.New("B clone symlink is hard-linked")
		}
	default:
		return errors.New("B clone contains an unsupported special filesystem entry")
	}
	return nil
}

func (walker *cloneCleanupWalker) unlinkEntry(parentFD int, parentRelative, name string, parentStat unix.Stat_t, depth int) error {
	if err := walker.checkBudget(depth); err != nil {
		return err
	}
	var before unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &before, unix.AT_SYMLINK_NOFOLLOW); errors.Is(err, unix.ENOENT) {
		return nil
	} else if err != nil {
		return fmt.Errorf("stat B clone entry without following symlinks: %w", err)
	}
	if err := walker.validateEntry(before); err != nil {
		return err
	}
	entryRelative := filepath.Join(parentRelative, name)
	entryType := before.Mode & unix.S_IFMT
	if entryType == unix.S_IFDIR {
		childFD, childStat, err := walker.openRelativeDirectory(entryRelative, unix.O_RDONLY|unix.O_DIRECTORY)
		if err != nil {
			return fmt.Errorf("open B clone directory with nofollow/no-xdev: %w", err)
		}
		if !sameCleanupDirectoryIdentity(before, childStat) {
			_ = unix.Close(childFD)
			return errors.New("B clone directory changed between lstat and pinned open")
		}
		if err := walker.clearDirectory(childFD, entryRelative, childStat, depth+1); err != nil {
			_ = unix.Close(childFD)
			return err
		}
		if err := unix.Close(childFD); err != nil {
			return fmt.Errorf("close emptied B clone child directory: %w", err)
		}
	} else if entryType == unix.S_IFLNK {
		how := &unix.OpenHow{
			Flags:   uint64(unix.O_PATH | unix.O_CLOEXEC | unix.O_NOFOLLOW),
			Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV),
		}
		pinned, err := unix.Openat2(parentFD, name, how)
		if err != nil {
			return fmt.Errorf("pin B clone symlink without following it: %w", err)
		}
		var pinnedStat unix.Stat_t
		statErr := unix.Fstat(pinned, &pinnedStat)
		closeErr := unix.Close(pinned)
		if statErr != nil || closeErr != nil || !sameCleanupEntryIdentity(before, pinnedStat) {
			return errors.Join(errors.New("B clone symlink changed before unlink"), statErr, closeErr)
		}
	}
	freshParentFD, freshParentStat, err := walker.openRelativeDirectory(parentRelative, unix.O_RDONLY|unix.O_DIRECTORY)
	if err != nil {
		return fmt.Errorf("reopen exact B clone parent before unlink: %w", err)
	}
	defer unix.Close(freshParentFD)
	if !sameCleanupDirectoryIdentity(parentStat, freshParentStat) {
		return errors.New("B clone parent directory changed before unlink")
	}
	if err := walker.verifyDirectoryPath(parentRelative, parentStat); err != nil {
		return err
	}
	if entryType == unix.S_IFDIR {
		childFD, childStat, err := walker.openRelativeDirectory(entryRelative, unix.O_PATH|unix.O_DIRECTORY)
		if err != nil {
			return fmt.Errorf("reopen exact empty B clone directory before unlink: %w", err)
		}
		_ = unix.Close(childFD)
		if !sameCleanupDirectoryIdentity(before, childStat) {
			return errors.New("B clone directory inode changed before unlink")
		}
	} else {
		if entryType == unix.S_IFREG {
			how := &unix.OpenHow{
				Flags:   uint64(unix.O_PATH | unix.O_CLOEXEC | unix.O_NOFOLLOW),
				Resolve: uint64(unix.RESOLVE_BENEATH | unix.RESOLVE_NO_SYMLINKS | unix.RESOLVE_NO_MAGICLINKS | unix.RESOLVE_NO_XDEV),
			}
			pinned, err := unix.Openat2(parentFD, name, how)
			if err != nil {
				return fmt.Errorf("pin B clone regular file before unlink: %w", err)
			}
			var pinnedStat unix.Stat_t
			statErr := unix.Fstat(pinned, &pinnedStat)
			closeErr := unix.Close(pinned)
			if statErr != nil || closeErr != nil || !sameCleanupEntryIdentity(before, pinnedStat) {
				return errors.Join(errors.New("B clone regular file changed before unlink"), statErr, closeErr)
			}
		}
		var current unix.Stat_t
		if err := unix.Fstatat(freshParentFD, name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil || !sameCleanupEntryIdentity(before, current) {
			return fmt.Errorf("B clone leaf entry changed before unlink: %w", errorOr(err, errors.New("inode/type/mode mismatch")))
		}
	}
	if err := walker.verifyDirectoryPath(parentRelative, parentStat); err != nil {
		return err
	}
	var current unix.Stat_t
	if err := unix.Fstatat(freshParentFD, name, &current, unix.AT_SYMLINK_NOFOLLOW); err != nil || !sameCleanupEntryForUnlink(before, current) {
		return fmt.Errorf("B clone entry changed at unlink boundary: %w", errorOr(err, errors.New("inode/type/mode mismatch")))
	}
	flags := 0
	if entryType == unix.S_IFDIR {
		flags = unix.AT_REMOVEDIR
	}
	if err := unix.Unlinkat(freshParentFD, name, flags); err != nil {
		return fmt.Errorf("unlink exact B clone entry: %w", err)
	}
	if err := unix.Fsync(freshParentFD); err != nil {
		return fmt.Errorf("sync B clone parent after unlink: %w", err)
	}
	if err := verifyCleanupEntryAbsent(freshParentFD, name); err != nil {
		return err
	}
	return walker.verifyDirectoryPath(parentRelative, parentStat)
}

func (walker *cloneCleanupWalker) clearDirectory(directoryFD int, relative string, identity unix.Stat_t, depth int) error {
	for {
		if err := walker.checkBudget(depth); err != nil {
			return err
		}
		if err := walker.verifyDirectoryPath(relative, identity); err != nil {
			return err
		}
		names, err := walker.listDirectoryNames(directoryFD)
		if err != nil {
			return err
		}
		if len(names) == 0 {
			return nil
		}
		walker.entriesSeen += len(names)
		for _, name := range names {
			if err := walker.unlinkEntry(directoryFD, relative, name, identity, depth+1); err != nil {
				return err
			}
		}
	}
}

func verifyCleanupEntryAbsent(parentFD int, name string) error {
	var stat unix.Stat_t
	if err := unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
		if err != nil {
			return err
		}
		return errors.New("B clone entry reappeared after unlink")
	}
	if err := unix.Fstatat(parentFD, name, &stat, unix.AT_SYMLINK_NOFOLLOW); !errors.Is(err, unix.ENOENT) {
		if err != nil {
			return err
		}
		return errors.New("B clone entry reappeared during absence rewalk")
	}
	return nil
}

// These local binary helpers keep the root stage's proc writes fixed-format;
// no command path, environment, cwd, or B-controlled bytes enter this phase.
